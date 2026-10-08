//! Private keeper for root-bound storage capacity reservations.
//!
//! The keeper persists every exact request with the signed observation it
//! binds before sending it, persists every signed answer it receives, and on
//! restart resends unanswered requests byte for byte so the node returns the
//! one identity it already assigned.

mod native;
mod store;

use std::collections::BTreeMap;
use std::path::Path;
use std::process::ExitCode;

use layerx_platform_authority::ai_storage_admission::{
    decode_reserve_answer, observe_body, request_id_body, Demand, Kind, Observation, Profile,
    Request, Reservation, CANCEL_REQUEST, CANCEL_RESPONSE, INSTALL_REQUEST, OBSERVE_REQUEST,
    RECONCILE_REQUEST, RECONCILE_RESPONSE, RESERVE_REQUEST, RESERVE_RESPONSE,
};
use layerx_platform_authority::hex;

use native::{Client, NativeError};
use store::{Store, StoreError};

const RESULT_NON_CANONICAL: i32 = -3;
const RESULT_IDEMPOTENT_REPLAY: i32 = -302;
const RESULT_PARAMETER_BOUNDS: i32 = -734;
const RESULT_PROJECTION_STALE: i32 = -903;
const CLASS_MALFORMED: u8 = 1;
const CLASS_SEMANTIC: u8 = 4;

const USAGE: &str =
    "usage: paxai-storage-keeper <install|observe|stage|reserve|recover|reconcile|cancel|status> \
--state DIR --network-id N --sequencer HEX [--socket PATH] [command options]";

enum Failure {
    Usage(String),
    Refused { class: u8, result: i32 },
    Advisory,
    Verify(String),
    Io(String),
}

impl From<NativeError> for Failure {
    fn from(error: NativeError) -> Self {
        match error {
            NativeError::Refused { class, result } => Self::Refused { class, result },
            NativeError::Io(error) => Self::Io(error.to_string()),
            other @ (NativeError::Protocol(_) | NativeError::Answer(_)) => {
                Self::Verify(other.to_string())
            }
        }
    }
}

impl From<StoreError> for Failure {
    fn from(error: StoreError) -> Self {
        match error {
            StoreError::Io(error) => Self::Io(error.to_string()),
            other @ (StoreError::Corrupt(_) | StoreError::Answer(_)) => {
                Self::Verify(other.to_string())
            }
        }
    }
}

struct Options {
    command: String,
    values: BTreeMap<String, String>,
}

impl Options {
    fn parse(arguments: &[String]) -> Result<Self, Failure> {
        let (command, rest) = arguments
            .split_first()
            .ok_or_else(|| Failure::Usage(USAGE.to_owned()))?;
        let mut values = BTreeMap::new();
        let mut cursor = rest.iter();
        while let Some(flag) = cursor.next() {
            let name = flag
                .strip_prefix("--")
                .ok_or_else(|| Failure::Usage(format!("unexpected argument {flag}")))?;
            let value = cursor
                .next()
                .ok_or_else(|| Failure::Usage(format!("missing value for --{name}")))?;
            if values.insert(name.to_owned(), value.clone()).is_some() {
                return Err(Failure::Usage(format!("repeated --{name}")));
            }
        }
        Ok(Self {
            command: command.clone(),
            values,
        })
    }

    fn text(&self, name: &str) -> Result<&str, Failure> {
        self.values
            .get(name)
            .map(String::as_str)
            .ok_or_else(|| Failure::Usage(format!("missing --{name}")))
    }

    fn number<T: std::str::FromStr>(&self, name: &str) -> Result<T, Failure> {
        self.text(name)?
            .parse()
            .map_err(|_| Failure::Usage(format!("--{name} is not a number")))
    }

    fn number_or<T: std::str::FromStr>(&self, name: &str, absent: T) -> Result<T, Failure> {
        if self.values.contains_key(name) {
            self.number(name)
        } else {
            Ok(absent)
        }
    }

    fn bytes32(&self, name: &str) -> Result<[u8; 32], Failure> {
        hex::decode32(self.text(name)?)
            .map_err(|_| Failure::Usage(format!("--{name} must be 64 hex characters")))
    }
}

struct Keeper {
    options: Options,
    store: Store,
    network_id: u32,
    sequencer: [u8; 32],
}

impl Keeper {
    fn client(&self) -> Result<Client, Failure> {
        Ok(Client::connect(
            Path::new(self.options.text("socket")?),
            self.network_id,
            self.sequencer,
        )?)
    }

    fn observe(client: &mut Client) -> Result<(Observation, Vec<u8>, Vec<u8>), Failure> {
        let answer = client.call(OBSERVE_REQUEST, &observe_body())?;
        let observation = Observation::decode(&answer.payload)
            .map_err(|error| Failure::Verify(error.to_string()))?;
        Ok((observation, answer.payload, answer.proof))
    }

    fn install(&mut self) -> Result<(), Failure> {
        let options = &self.options;
        let profile = Profile {
            version: options.number("profile-version")?,
            digest: options.bytes32("profile-digest")?,
            floor: Demand {
                blobs: options.number("floor-blobs")?,
                bytes: options.number("floor-bytes")?,
                kv: options.number("floor-kv")?,
            },
            maximum_work_lifetime: options.number("max-work-lifetime")?,
        };
        let body = profile
            .encode()
            .map_err(|error| Failure::Usage(error.to_string()))?;
        let mut client = self.client()?;
        let answer = client.call(INSTALL_REQUEST, &body)?;
        let installed = self.store.record_profile(&answer.payload, &answer.proof)?;
        if installed != profile {
            return Err(Failure::Verify("node installed another profile".to_owned()));
        }
        println!(
            "profile version={} digest={} floor_blobs={} floor_bytes={} floor_kv={} max_work_lifetime={}",
            installed.version,
            hex::encode(&installed.digest),
            installed.floor.blobs,
            installed.floor.bytes,
            installed.floor.kv,
            installed.maximum_work_lifetime
        );
        Ok(())
    }

    fn observe_command(&self) -> Result<(), Failure> {
        let mut client = self.client()?;
        let (observation, _, _) = Self::observe(&mut client)?;
        let profile_match = self
            .store
            .profile
            .is_some_and(|profile| observation.require_profile(&profile).is_ok());
        println!(
            "observation next_sequence={} root={} available_blobs={} available_bytes={} available_kv={} active={} next_request_id={} profile_match={}",
            observation.next_sequence,
            hex::encode(&observation.state_root),
            observation.available.blobs,
            observation.available.bytes,
            observation.available.kv,
            observation.active_reservations,
            observation.next_request_id,
            u8::from(profile_match)
        );
        Ok(())
    }

    fn stage(&mut self, client: &mut Client) -> Result<[u8; 32], Failure> {
        let profile = self
            .store
            .profile
            .ok_or_else(|| Failure::Usage("no installed profile in the keeper store".to_owned()))?;
        let options = &self.options;
        let kind = match options.text("kind")? {
            "work" => Kind::Work,
            "obligation" => Kind::Obligation,
            other => return Err(Failure::Usage(format!("unknown kind {other}"))),
        };
        let mut request = Request {
            kind,
            activity_id: options.bytes32("activity")?,
            idempotency_key: options.bytes32("idempotency")?,
            actor_did: options.text("actor")?.as_bytes().to_vec(),
            demand: Demand {
                blobs: options.number_or("blobs", 0)?,
                bytes: options.number_or("bytes", 0)?,
                kv: options.number_or("kv", 0)?,
            },
            expected_sequence: 0,
            expected_root: [0; 32],
            lifetime: options.number_or("lifetime", 0)?,
            supersedes: options.number_or("supersedes", 0)?,
        };
        let (observation, payload, proof) = Self::observe(client)?;
        observation
            .require_profile(&profile)
            .map_err(|error| Failure::Verify(error.to_string()))?;
        request.expected_sequence = observation.next_sequence;
        request.expected_root = observation.state_root;
        let bytes = request
            .encode()
            .map_err(|error| Failure::Usage(error.to_string()))?;
        // A supersession frees its predecessor's reserve at the node, which
        // this observation still counts; the node's own check is authoritative.
        if request.supersedes == 0 && observation.admits(kind, &request.demand).is_err() {
            println!(
                "advisory_refused available_blobs={} available_bytes={} available_kv={}",
                observation.available.blobs, observation.available.bytes, observation.available.kv
            );
            return Err(Failure::Advisory);
        }
        let digest = self.store.stage(&bytes, &payload, &proof)?;
        println!(
            "staged digest={} request={} sequence={}",
            hex::encode(&digest),
            hex::encode(&bytes),
            request.expected_sequence
        );
        Ok(digest)
    }

    fn submit(&mut self, client: &mut Client, digest: &[u8; 32]) -> Result<(), Failure> {
        let bytes = self
            .store
            .staged_by_digest(digest)
            .map(|staged| staged.bytes.clone())
            .ok_or_else(|| Failure::Verify("request not staged".to_owned()))?;
        match client.call(RESERVE_REQUEST, &bytes) {
            Ok(answer) => {
                let (replayed, _) = decode_reserve_answer(&answer.payload)
                    .map_err(|error| Failure::Verify(error.to_string()))?;
                let record =
                    self.store
                        .record_answer(RESERVE_RESPONSE, &answer.payload, &answer.proof)?;
                print_record(&record, Some(replayed));
                Ok(())
            }
            Err(NativeError::Refused { class, result }) => {
                if self.definitive(client, digest, class, result)? {
                    self.store.record_abandoned(digest, class, result)?;
                    println!(
                        "abandoned digest={} class={class} result={result}",
                        hex::encode(digest)
                    );
                }
                Err(Failure::Refused { class, result })
            }
            Err(other) => Err(other.into()),
        }
    }

    fn definitive(
        &self,
        client: &mut Client,
        digest: &[u8; 32],
        class: u8,
        result: i32,
    ) -> Result<bool, Failure> {
        if class == CLASS_MALFORMED {
            return Ok(true);
        }
        if class != CLASS_SEMANTIC {
            return Ok(false);
        }
        match result {
            RESULT_NON_CANONICAL | RESULT_PARAMETER_BOUNDS | RESULT_IDEMPOTENT_REPLAY => Ok(true),
            RESULT_PROJECTION_STALE => {
                let staged = self
                    .store
                    .staged_by_digest(digest)
                    .ok_or_else(|| Failure::Verify("request not staged".to_owned()))?;
                let (expected_sequence, expected_root) = (
                    staged.request.expected_sequence,
                    staged.request.expected_root,
                );
                let (observation, _, _) = Self::observe(client)?;
                Ok(observation
                    .require_head(expected_sequence, &expected_root)
                    .is_err())
            }
            _ => Ok(false),
        }
    }

    fn by_id(&mut self, request_tag: u16, response_tag: u16) -> Result<(), Failure> {
        let request_id: u64 = self.options.number("request-id")?;
        let mut client = self.client()?;
        let answer = client.call(request_tag, &request_id_body(request_id))?;
        let record = self
            .store
            .record_answer(response_tag, &answer.payload, &answer.proof)?;
        if record.request_id != request_id {
            return Err(Failure::Verify("answer names another request".to_owned()));
        }
        print_record(&record, None);
        Ok(())
    }

    fn recover(&mut self) -> Result<(), Failure> {
        let mut client = self.client()?;
        let pending: Vec<[u8; 32]> = self
            .store
            .staged
            .iter()
            .map(|staged| staged.digest)
            .filter(|digest| {
                self.store.record_for(digest).is_none()
                    && !self.store.abandoned.contains_key(digest)
            })
            .collect();
        let (mut resent, mut held, mut dropped, mut reconciled) =
            (0_usize, 0_usize, 0_usize, 0_usize);
        for digest in &pending {
            match self.submit(&mut client, digest) {
                Ok(()) => resent += 1,
                Err(Failure::Refused { .. }) if self.store.abandoned.contains_key(digest) => {
                    dropped += 1;
                }
                Err(Failure::Refused { class, result }) => {
                    held += 1;
                    println!(
                        "pending digest={} class={class} result={result}",
                        hex::encode(digest)
                    );
                }
                Err(other) => return Err(other),
            }
        }
        let outstanding: Vec<u64> = self
            .store
            .records
            .values()
            .filter(|record| record.holds_capacity())
            .map(|record| record.request_id)
            .collect();
        for request_id in outstanding {
            let answer = client.call(RECONCILE_REQUEST, &request_id_body(request_id))?;
            let record =
                self.store
                    .record_answer(RECONCILE_RESPONSE, &answer.payload, &answer.proof)?;
            reconciled += 1;
            print_record(&record, None);
        }
        println!(
            "recovered resent={resent} pending={held} abandoned={dropped} reconciled={reconciled} torn_tail_dropped={}",
            u8::from(self.store.truncated_tail)
        );
        Ok(())
    }

    fn status(&self) {
        if let Some(profile) = self.store.profile {
            println!(
                "profile version={} digest={}",
                profile.version,
                hex::encode(&profile.digest)
            );
        }
        for staged in &self.store.staged {
            if let Some(abandoned) = self.store.abandoned.get(&staged.digest) {
                println!(
                    "abandoned digest={} class={} result={}",
                    hex::encode(&staged.digest),
                    abandoned.class,
                    abandoned.result
                );
            } else if let Some(record) = self.store.record_for(&staged.digest) {
                print_record(record, None);
            } else {
                println!(
                    "unanswered digest={} sequence={}",
                    hex::encode(&staged.digest),
                    staged.observation.next_sequence
                );
            }
        }
    }

    fn run(&mut self) -> Result<(), Failure> {
        match self.options.command.as_str() {
            "install" => self.install(),
            "observe" => self.observe_command(),
            "stage" => {
                let mut client = self.client()?;
                self.stage(&mut client).map(|_| ())
            }
            "reserve" => {
                let mut client = self.client()?;
                let digest = self.stage(&mut client)?;
                self.submit(&mut client, &digest)
            }
            "recover" => self.recover(),
            "reconcile" => self.by_id(RECONCILE_REQUEST, RECONCILE_RESPONSE),
            "cancel" => self.by_id(CANCEL_REQUEST, CANCEL_RESPONSE),
            "status" => {
                self.status();
                Ok(())
            }
            other => Err(Failure::Usage(format!("unknown command {other}\n{USAGE}"))),
        }
    }
}

fn print_record(record: &Reservation, replayed: Option<bool>) {
    let kind = match record.kind {
        Kind::Work => "work",
        Kind::Obligation => "obligation",
    };
    let replayed = replayed.map_or(String::new(), |flag| {
        format!(" replayed={}", u8::from(flag))
    });
    println!(
        "reservation id={} kind={kind} state={}{replayed} digest={} bound_sequence={} outcome_sequence={} outcome_result={}",
        record.request_id,
        record.state.name(),
        hex::encode(&record.request_digest),
        record.bound_sequence,
        record.outcome_sequence,
        record.outcome_result
    );
}

fn start(arguments: &[String]) -> Result<(), Failure> {
    let options = Options::parse(arguments)?;
    let network_id: u32 = options.number("network-id")?;
    let sequencer = options.bytes32("sequencer")?;
    let store = Store::open(Path::new(options.text("state")?), network_id, sequencer)?;
    Keeper {
        options,
        store,
        network_id,
        sequencer,
    }
    .run()
}

fn main() -> ExitCode {
    let arguments: Vec<String> = std::env::args().skip(1).collect();
    match start(&arguments) {
        Ok(()) => ExitCode::SUCCESS,
        Err(Failure::Usage(message)) => {
            eprintln!("{message}");
            ExitCode::from(2)
        }
        Err(Failure::Refused { class, result }) => {
            println!("refused class={class} result={result}");
            ExitCode::from(3)
        }
        Err(Failure::Advisory) => ExitCode::from(3),
        Err(Failure::Verify(message)) => {
            eprintln!("verification failed: {message}");
            ExitCode::from(4)
        }
        Err(Failure::Io(message)) => {
            eprintln!("io failed: {message}");
            ExitCode::from(5)
        }
    }
}
