//! Paxeer X Network AI-market evaluator service. Each epoch it reads the committed market state
//! through the 0x101D programs precompile, sends one `CommitScore` in the commit window and one
//! `RevealScore` in the reveal window for its granted evaluator, and keeps the commitment secret
//! in a local journal so a restart can still reveal.
use std::collections::BTreeMap;
use std::fs;
use std::io::ErrorKind;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use ed25519_dalek::{Signer, SigningKey};
use layerx_oracle_feeder::{http_agent, Sequencer};
use layerx_platform_internal::evm_sender::{Eip1559Call, EvmSender};
use layerx_platform_internal::http::{
    health_listen, ok, parse_client_request, refusal, write_response, Response, IO_TIMEOUT,
};
use layerx_platform_internal::journal::Journal;
use layerx_platform_internal::secret::{hex, read_secret_file, required_env, unhex};
use layerx_programs_ai_market::{
    admission::AdmissionTable,
    aggregation::QUORUM,
    codec::{
        self, decode_envelope, derive_market, encode_envelope, ChunkRequest, CommitScorePayload,
        Envelope, ReportBody, ResultStatus, RevealScorePayload, ScoreVector,
    },
    commit_reveal::{
        self as cr,
        commitment::{COMMIT_SCORE_BYTES, REVEAL_MAX_BYTES},
        Status,
    },
    dispatch::{self, Operation},
    evaluators::{
        authority::{evaluator_region, FrozenEvaluator, Snapshot},
        model::GrantStatus,
    },
    queries::{ReadProof, StateCapture},
    registry_ops::PolicySection,
    state::{
        decode_shared_state, settlement_rewards, ActorSlot, HeightWindow, ReplayTable, Section,
    },
    ApplicationError, Authentication, ChainDomain, Digest32, EvaluatorBinding, EvaluatorId,
    EvidenceRoot, FrozenBinding, Presence, PrincipalId, ProgramId, PublicKey32, ReportDigest,
    RequestId, Salt32, Signature64, Version, MAX_CHUNK_BYTES, MAX_ENVELOPE_BYTES, MAX_EVALUATORS,
    MAX_STATE_BYTES,
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use zeroize::Zeroizing;

/// F04 windows inside the 128-height epoch: commit `[T+64, T+80)`, reveal `[T+80, T+96)`.
const COMMIT_OPENS: u64 = 64;
const COMMIT_CLOSES: u64 = 80;
const REVEAL_OPENS: u64 = 80;
const REVEAL_CLOSES: u64 = 96;
const PROGRAMS_PRECOMPILE: [u8; 20] = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x1d,
];
/// `call(bytes32,uint16,string,bytes,bytes,bytes,uint32,uint64[7],uint128)`.
const CALL_SELECTOR: [u8; 4] = [0x54, 0xbd, 0x2b, 0x48];
/// `simulateCall` with the `call` arguments.
const SIMULATE_SELECTOR: [u8; 4] = [0x30, 0x44, 0xbd, 0x56];
/// `getStateHead(bytes32)`.
const STATE_HEAD_SELECTOR: [u8; 4] = [0x08, 0x33, 0x65, 0x0b];
const GUEST_ABI: u16 = 5;
const ENTRYPOINT: &str = "layerx_call";
const CALL_HEAD_BYTES: usize = 15 * 32;
const TERMINAL_SUCCESS: u8 = 1;
const LIFECYCLE_ACTIVE: u8 = 1;
const CHUNK_REQUEST_BYTES: usize = 46;
const CONFIG_VARIABLE: &str = "LAYERX_AI_EVALUATOR_CONFIG";
const KEY_VARIABLE: &str = "LAYERX_AI_EVALUATOR_KEY_FILE";
/// The shared EVM sender loader reads `<prefix>_KEY_FILE` or `<prefix>_KMS_KEY_ID`.
const EVM_SENDER_PREFIX: &str = "LAYERX_AI_EVALUATOR_EVM_SENDER";
const RECEIPT_POLL: Duration = Duration::from_millis(50);
const READY_POLLS: u64 = 3;

fn app(error: ApplicationError) -> String {
    format!("application error 0x{:04x}", error.code())
}

fn bytes32(text: &str, name: &str) -> Result<[u8; 32], String> {
    unhex(text)
        .and_then(|bytes| <[u8; 32]>::try_from(bytes).ok())
        .ok_or_else(|| format!("{name} is not 32 bytes of lowercase hex"))
}

fn random32() -> Result<[u8; 32], String> {
    let mut bytes = [0; 32];
    getrandom::fill(&mut bytes).map_err(|_| "entropy is unavailable".to_owned())?;
    Ok(bytes)
}

/// The granted ed25519 assessment key; `public_key` and `sign(digest)` mirror the SDK signer.
pub struct AssessmentKey(SigningKey);

impl AssessmentKey {
    /// Loads a 32-byte ed25519 seed written as lowercase hex.
    ///
    /// # Errors
    /// Refuses an unreadable file or anything other than a 32-byte hex seed.
    pub fn load(path: &Path) -> Result<Self, String> {
        let text = read_secret_file(path)?;
        let seed = Zeroizing::new(unhex(&text).ok_or("assessment key is not lowercase hex")?);
        let seed: &[u8; 32] = seed
            .as_slice()
            .try_into()
            .map_err(|_| "assessment key is not a 32-byte ed25519 seed".to_owned())?;
        Ok(Self(SigningKey::from_bytes(seed)))
    }

    #[must_use]
    pub fn public_key(&self) -> PublicKey32 {
        PublicKey32(self.0.verifying_key().to_bytes())
    }

    #[must_use]
    pub fn sign(&self, digest: &[u8; 32]) -> Signature64 {
        Signature64(self.0.sign(digest).to_bytes())
    }
}

/// The market, evaluator and read principal this service acts for.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Identity {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub evaluator: EvaluatorId,
    /// Evaluator owner: the envelope actor bound to the evaluator replay slot.
    pub owner: PrincipalId,
    /// Native principal of the EVM sender, the actor of state reads.
    pub reader: PrincipalId,
}

/// The opened epoch of one committed state, seen from this evaluator at `height`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EpochView {
    pub height: u64,
    pub frozen: FrozenBinding,
    /// This evaluator's frozen roster entry, absent when it is not frozen into the epoch.
    pub entry: Option<FrozenEvaluator>,
    pub slot: cr::Slot,
    pub commit: HeightWindow,
    pub reveal: HeightWindow,
    /// Next evaluator role sequence of the owner, absent without a bound replay slot.
    pub sequence: Option<u64>,
    /// Frozen evaluators whose live grant is still active.
    pub active: usize,
    pub revealed: usize,
    /// Accepted commitments without an admitted report.
    pub unrevealed: usize,
}

impl EpochView {
    /// # Errors
    /// Refuses a state of another market, a state without an opened epoch and any guest decode
    /// refusal.
    pub fn read(current: &[u8], identity: &Identity, height: u64) -> Result<Self, String> {
        let state = decode_shared_state(current).map_err(app)?;
        let header =
            PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])
                .map_err(app)?
                .header;
        if header.deployment_chain_domain != identity.chain || header.program_id != identity.program
        {
            return Err("captured state belongs to another market".to_owned());
        }
        let epoch =
            AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])
                .map_err(app)?
                .current_epoch()
                .ok_or("no epoch is open")?;
        let roster = settlement_rewards(&state, None)
            .and_then(|rewards| rewards.row(epoch))
            .map_err(app)?
            .roster;
        let frozen = FrozenBinding {
            chain: header.deployment_chain_domain,
            program: header.program_id,
            market: header.market_id,
            epoch,
            config: Version::new(header.active_config_version).map_err(app)?,
            roster,
        };
        let window = |opens, closes| {
            HeightWindow::epoch(header.origin_height, epoch, opens, closes).map_err(app)
        };
        let region = evaluator_region(state.feature_sections[Section::IdentityRoster.index()])
            .map_err(app)?;
        let (mut active, mut revealed, mut unrevealed, mut entry) = (0, 0, 0, None);
        for frozen_entry in region
            .snapshot()
            .filter(|snapshot| snapshot.epoch == epoch)
            .into_iter()
            .flat_map(Snapshot::entries)
        {
            let id = frozen_entry.entry.evaluator;
            if region
                .get(id)
                .is_some_and(|record| record.grant.status == GrantStatus::Active)
                && !region.excluded(id)
                && epoch < frozen_entry.expiry_epoch_exclusive
            {
                active += 1;
            }
            match cr::slot(current, id, height).map_err(app)?.status {
                Status::Revealed => revealed += 1,
                Status::Committed | Status::Expired => unrevealed += 1,
                Status::Absent => {}
            }
            if id == identity.evaluator {
                entry = Some(*frozen_entry);
            }
        }
        Ok(Self {
            height,
            frozen,
            entry,
            slot: cr::slot(current, identity.evaluator, height).map_err(app)?,
            commit: window(COMMIT_OPENS, COMMIT_CLOSES)?,
            reveal: window(REVEAL_OPENS, REVEAL_CLOSES)?,
            sequence: next_sequence(&state.control.replay, identity.owner)?,
            active,
            revealed,
            unrevealed,
        })
    }

    /// True inside the reveal window once fewer than `margin` heights remain.
    #[must_use]
    pub fn near_end(&self, margin: u64) -> bool {
        self.height >= self.reveal.start
            && self.height < self.reveal.end
            && self.height.saturating_add(margin) >= self.reveal.end
    }
}

fn next_sequence(replay: &ReplayTable, owner: PrincipalId) -> Result<Option<u64>, String> {
    for index in 0..MAX_EVALUATORS {
        let slot = ActorSlot::evaluator(index).map_err(app)?;
        if let Some(actor) = replay.actor(slot).filter(|actor| actor.principal == owner) {
            return actor
                .last
                .map_or(0, |last| last.sequence)
                .checked_add(1)
                .map(Some)
                .ok_or_else(|| "evaluator sequence overflow".to_owned());
        }
    }
    Ok(None)
}

/// The commitment secret of one epoch: everything a restart needs to reveal.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct Secret {
    pub epoch: u64,
    pub evidence: String,
    pub scores: String,
    pub salt: String,
    pub commitment: String,
    /// The signed `CommitScore` envelope, resent unchanged until it lands or its window closes.
    pub commit: String,
    pub reveal_request: String,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(tag = "entry", rename_all = "snake_case")]
enum Entry {
    Prepared(Secret),
    Applied {
        epoch: u64,
        operation: u16,
    },
    Refused {
        epoch: u64,
        operation: u16,
        code: u16,
    },
    Forfeited {
        epoch: u64,
    },
}

/// What the journal holds for one epoch.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct EpochRecord {
    pub secret: Option<Secret>,
    /// Selectors of the calls the chain applied, in order.
    pub applied: Vec<u16>,
    /// The refused selector and its application error code (0 when undecoded).
    pub refused: Option<(u16, u16)>,
    pub forfeited: bool,
}

fn fold(epochs: &mut BTreeMap<u64, EpochRecord>, entry: Entry) {
    match entry {
        Entry::Prepared(secret) => {
            let epoch = secret.epoch;
            epochs.entry(epoch).or_default().secret = Some(secret);
        }
        Entry::Applied { epoch, operation } => {
            epochs.entry(epoch).or_default().applied.push(operation);
        }
        Entry::Refused {
            epoch,
            operation,
            code,
        } => epochs.entry(epoch).or_default().refused = Some((operation, code)),
        Entry::Forfeited { epoch } => epochs.entry(epoch).or_default().forfeited = true,
    }
}

/// Append-only, fsynced journal of commitment secrets and call outcomes per epoch.
pub struct ScoreJournal {
    journal: Journal,
    epochs: BTreeMap<u64, EpochRecord>,
}

impl ScoreJournal {
    /// Opens the journal under `directory` and replays it.
    ///
    /// # Errors
    /// Refuses an unusable, locked or undecodable journal.
    pub fn open(directory: &Path) -> Result<Self, String> {
        let mut epochs = BTreeMap::new();
        let journal = Journal::open(directory, |entry: Entry| fold(&mut epochs, entry))?;
        Ok(Self { journal, epochs })
    }

    #[must_use]
    pub fn epoch(&self, epoch: u64) -> Option<&EpochRecord> {
        self.epochs.get(&epoch)
    }

    /// Applied calls, refused epochs and forfeited epochs over the whole journal.
    #[must_use]
    pub fn totals(&self) -> (usize, usize, usize) {
        self.epochs
            .values()
            .fold((0, 0, 0), |(applied, refused, forfeited), record| {
                (
                    applied + record.applied.len(),
                    refused + usize::from(record.refused.is_some()),
                    forfeited + usize::from(record.forfeited),
                )
            })
    }

    fn record(&mut self, entry: Entry) -> Result<(), String> {
        self.journal.append(&entry)?;
        fold(&mut self.epochs, entry);
        Ok(())
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct AssessedScore {
    worker: String,
    score: u32,
}

/// `<assessments>/<epoch>.json`: the sealed evidence root and one score per assessed worker.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Assessment {
    evidence_root: String,
    scores: Vec<AssessedScore>,
}

impl Assessment {
    /// Canonical `worker32 || score:u32` entries in ascending worker order.
    fn encode(&self) -> Result<Vec<u8>, String> {
        let mut entries = self
            .scores
            .iter()
            .map(|entry| Ok((bytes32(&entry.worker, "worker")?, entry.score)))
            .collect::<Result<Vec<_>, String>>()?;
        entries.sort_unstable_by_key(|(worker, _)| *worker);
        let mut out = Vec::with_capacity(entries.len() * 36);
        for (worker, score) in entries {
            out.extend_from_slice(&worker);
            out.extend_from_slice(&score.to_be_bytes());
        }
        ScoreVector::Encoded(&out).validate().map_err(app)?;
        Ok(out)
    }
}

/// One envelope for the programs precompile.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Call {
    pub operation: Operation,
    pub epoch: u64,
    pub envelope: Vec<u8>,
}

/// The guest's answer to a sent call.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CallOutcome {
    Applied,
    Refused(Option<ApplicationError>),
}

/// Reads a guest result frame: `Ok` and `AlreadyApplied` are applied, `Error` is refused.
///
/// # Errors
/// Refuses a malformed result frame.
pub fn outcome_of(response: &[u8]) -> Result<CallOutcome, String> {
    let result = codec::decode_result(response).map_err(app)?;
    Ok(match result.status {
        ResultStatus::Ok | ResultStatus::AlreadyApplied => CallOutcome::Applied,
        ResultStatus::Error => CallOutcome::Refused(result.error),
    })
}

/// The commit-then-reveal state machine of one evaluator.
pub struct Evaluator {
    identity: Identity,
    key: AssessmentKey,
    journal: ScoreJournal,
    assessments: PathBuf,
}

impl Evaluator {
    /// # Errors
    /// Refuses an unusable journal.
    pub fn open(
        identity: Identity,
        key: AssessmentKey,
        state_dir: &Path,
        assessments: &Path,
    ) -> Result<Self, String> {
        Ok(Self {
            identity,
            key,
            journal: ScoreJournal::open(state_dir)?,
            assessments: assessments.to_path_buf(),
        })
    }

    #[must_use]
    pub const fn journal(&self) -> &ScoreJournal {
        &self.journal
    }

    /// The call this evaluator sends for `view`, if any. A refused call or a forfeited epoch is
    /// never retried; a prepared commitment is journaled before its envelope is returned.
    ///
    /// # Errors
    /// Refuses an unreadable assessment, a journal write failure and guest encoding refusals.
    pub fn step(&mut self, view: &EpochView) -> Result<Option<Call>, String> {
        let Some(entry) = view.entry else {
            return Ok(None);
        };
        let epoch = view.frozen.epoch;
        let record = self.journal.epoch(epoch).cloned().unwrap_or_default();
        if record.refused.is_some() || record.forfeited {
            return Ok(None);
        }
        let height = view.height;
        match view.slot.status {
            Status::Revealed => Ok(None),
            Status::Expired => self.forfeit(epoch, "the reveal window closed without a reveal"),
            Status::Committed if height < view.reveal.start => Ok(None),
            Status::Committed => match (record.secret, view.sequence) {
                (None, _) => self.forfeit(epoch, "the accepted commitment has no journaled secret"),
                (Some(_), None) => Ok(None),
                (Some(secret), Some(sequence)) => self.reveal(view, &entry, &secret, sequence),
            },
            Status::Absent if height >= view.commit.end => match record.secret {
                Some(_) => self.forfeit(epoch, "the commitment was not accepted in its window"),
                None => Ok(None),
            },
            Status::Absent if height < view.commit.start => Ok(None),
            Status::Absent => match (record.secret, view.sequence) {
                (Some(secret), _) => Ok(Some(Call {
                    operation: dispatch::CommitScore,
                    epoch,
                    envelope: unhex(&secret.commit).ok_or("journaled commit is not hex")?,
                })),
                (None, Some(sequence)) => self.prepare(view, &entry, sequence),
                (None, None) => Ok(None),
            },
        }
    }

    /// Journals the guest's answer to `call`; a refusal is logged and never retried.
    ///
    /// # Errors
    /// Refuses a journal write failure.
    pub fn settle(&mut self, call: &Call, outcome: CallOutcome) -> Result<(), String> {
        let (epoch, operation) = (call.epoch, call.operation.selector());
        match outcome {
            CallOutcome::Applied => self.journal.record(Entry::Applied { epoch, operation }),
            CallOutcome::Refused(error) => {
                let code = error.map_or(0, ApplicationError::code);
                eprintln!(
                    "ai-evaluator: epoch {epoch} operation 0x{operation:04x} refused with 0x{code:04x}; not retried"
                );
                self.journal.record(Entry::Refused {
                    epoch,
                    operation,
                    code,
                })
            }
        }
    }

    fn forfeit(&mut self, epoch: u64, reason: &str) -> Result<Option<Call>, String> {
        eprintln!("ai-evaluator: epoch {epoch} forfeited: {reason}");
        self.journal.record(Entry::Forfeited { epoch })?;
        Ok(None)
    }

    fn prepare(
        &mut self,
        view: &EpochView,
        entry: &FrozenEvaluator,
        sequence: u64,
    ) -> Result<Option<Call>, String> {
        let epoch = view.frozen.epoch;
        let path = self.assessments.join(format!("{epoch}.json"));
        let text = match fs::read_to_string(&path) {
            Ok(text) => text,
            Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(format!("{}: {error}", path.display())),
        };
        let assessment: Assessment =
            serde_json::from_str(&text).map_err(|error| format!("{}: {error}", path.display()))?;
        let evidence = bytes32(&assessment.evidence_root, "evidence_root")?;
        let scores = assessment.encode()?;
        let binding = binding(view, entry);
        let salt = random32()?;
        let commitment = codec::commitment_digest(
            &binding,
            report_digest(binding, evidence, &scores)?,
            Salt32::new(salt).map_err(app)?,
        )
        .map_err(app)?;
        let mut payload = vec![0; COMMIT_SCORE_BYTES];
        codec::encode_commit_score(
            &CommitScorePayload {
                binding,
                commitment,
            },
            &mut payload,
        )
        .map_err(app)?;
        let envelope = self.envelope(
            dispatch::CommitScore,
            view,
            sequence,
            view.commit.end,
            random32()?,
            &payload,
        )?;
        self.journal.record(Entry::Prepared(Secret {
            epoch,
            evidence: hex(&evidence),
            scores: hex(&scores),
            salt: hex(&salt),
            commitment: hex(commitment.as_bytes()),
            commit: hex(&envelope),
            reveal_request: hex(&random32()?),
        }))?;
        Ok(Some(Call {
            operation: dispatch::CommitScore,
            epoch,
            envelope,
        }))
    }

    fn reveal(
        &mut self,
        view: &EpochView,
        entry: &FrozenEvaluator,
        secret: &Secret,
        sequence: u64,
    ) -> Result<Option<Call>, String> {
        let epoch = view.frozen.epoch;
        let binding = binding(view, entry);
        let scores = unhex(&secret.scores).ok_or("journaled scores are not hex")?;
        let body = ReportBody {
            binding,
            evidence: EvidenceRoot::new(bytes32(&secret.evidence, "journaled evidence")?)
                .map_err(app)?,
            scores: ScoreVector::Encoded(&scores),
        };
        let report = codec::report_digest(&body).map_err(app)?;
        let salt = Salt32::new(bytes32(&secret.salt, "journaled salt")?).map_err(app)?;
        let commitment = codec::commitment_digest(&binding, report, salt).map_err(app)?;
        if hex(commitment.as_bytes()) != secret.commitment
            || view.slot.commit.map(|record| record.commitment) != Some(commitment)
        {
            return self.forfeit(epoch, "the journaled secret does not open the commitment");
        }
        let signature = self
            .key
            .sign(codec::attestation_digest(report).map_err(app)?.as_bytes());
        let mut payload = vec![0; REVEAL_MAX_BYTES];
        let len = codec::encode_reveal_score(
            &RevealScorePayload {
                report: body,
                signature,
                salt,
            },
            &mut payload,
        )
        .map_err(app)?;
        payload.truncate(len);
        let envelope = self.envelope(
            dispatch::RevealScore,
            view,
            sequence,
            view.reveal.end,
            bytes32(&secret.reveal_request, "journaled reveal request")?,
            &payload,
        )?;
        Ok(Some(Call {
            operation: dispatch::RevealScore,
            epoch,
            envelope,
        }))
    }

    /// The owner-actor envelope bound to the frozen epoch, signed by the assessment key over its
    /// request digest.
    fn envelope(
        &self,
        operation: Operation,
        view: &EpochView,
        sequence: u64,
        expiry: u64,
        request: [u8; 32],
        payload: &[u8],
    ) -> Result<Vec<u8>, String> {
        let frozen = view.frozen;
        let key = self.key.public_key();
        let mut envelope = Envelope {
            operation,
            chain: frozen.chain,
            program: frozen.program,
            market: frozen.market,
            actor: self.identity.owner,
            epoch: frozen.epoch,
            config: frozen.config.get(),
            roster: Presence::Present(frozen.roster),
            sequence,
            expiry,
            request: RequestId::new(request).map_err(app)?,
            payload,
            authentication: Authentication::Delegate {
                key,
                signature: Signature64([0; 64]),
            },
        };
        let mut out = vec![0; MAX_ENVELOPE_BYTES];
        let unsigned = encode_envelope(&envelope, &mut out).map_err(app)?;
        let digest = decode_envelope(&out[..unsigned])
            .and_then(|validated| validated.request_digest())
            .map_err(app)?;
        envelope.authentication = Authentication::Delegate {
            key,
            signature: self.key.sign(digest.as_bytes()),
        };
        let len = encode_envelope(&envelope, &mut out).map_err(app)?;
        out.truncate(len);
        Ok(out)
    }
}

const fn binding(view: &EpochView, entry: &FrozenEvaluator) -> EvaluatorBinding {
    EvaluatorBinding {
        frozen: view.frozen,
        evaluator: entry.entry.evaluator,
        grant: entry.entry.grant,
        key_version: entry.entry.key_version,
    }
}

fn report_digest(
    binding: EvaluatorBinding,
    evidence: [u8; 32],
    scores: &[u8],
) -> Result<ReportDigest, String> {
    codec::report_digest(&ReportBody {
        binding,
        evidence: EvidenceRoot::new(evidence).map_err(app)?,
        scores: ScoreVector::Encoded(scores),
    })
    .map_err(app)
}

fn read_envelope(identity: &Identity, payload: &[u8]) -> Result<Vec<u8>, String> {
    let envelope = Envelope {
        operation: dispatch::READ_STATE_CHUNK,
        chain: identity.chain,
        program: identity.program,
        market: derive_market(identity.chain, identity.program).map_err(app)?,
        actor: identity.reader,
        epoch: 0,
        config: 0,
        roster: Presence::Absent,
        sequence: 0,
        expiry: u64::MAX,
        request: RequestId::new(random32()?).map_err(app)?,
        payload,
        authentication: Authentication::Native,
    };
    let mut out = vec![0; MAX_ENVELOPE_BYTES];
    let len = encode_envelope(&envelope, &mut out).map_err(app)?;
    out.truncate(len);
    Ok(out)
}

/// Captures the complete committed state through `READ_STATE_CHUNK` program reads: `fetch`
/// answers one read envelope with its result frame, every chunk is pinned to the revision and
/// digest of the first and proved by `proof`.
///
/// # Errors
/// Refuses a refused read, a malformed frame and any capture integrity failure.
pub fn capture_state(
    identity: &Identity,
    proof: &ReadProof,
    mut fetch: impl FnMut(&[u8]) -> Result<Vec<u8>, String>,
) -> Result<Vec<u8>, String> {
    let requested =
        u16::try_from(MAX_CHUNK_BYTES).map_err(|_| "chunk bound overflow".to_owned())?;
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut capture = StateCapture::new(&mut buffer);
    let mut pinned = (0, Presence::Absent);
    let mut offset = 0_u32;
    loop {
        let mut request = [0; CHUNK_REQUEST_BYTES];
        let len = codec::encode_chunk_request(
            &ChunkRequest {
                revision: pinned.0,
                digest: pinned.1,
                offset,
                requested,
            },
            &mut request,
        )
        .map_err(app)?;
        let response = fetch(&read_envelope(identity, &request[..len])?)?;
        let result = codec::decode_result(&response).map_err(app)?;
        if result.status != ResultStatus::Ok {
            return Err(format!(
                "state read refused with {:?}",
                result.error.map(ApplicationError::code)
            ));
        }
        capture
            .accept(proof, result.payload)
            .map_err(|error| format!("state capture: {error:?}"))?;
        let chunk = codec::decode_chunk_response(result.payload).map_err(app)?;
        pinned = (chunk.revision, Presence::Present(chunk.digest));
        offset = u32::try_from(chunk.bytes.len())
            .ok()
            .and_then(|read| chunk.offset.checked_add(read))
            .ok_or("chunk offset overflow")?;
        if offset >= chunk.total_bytes {
            break;
        }
    }
    let (bytes, _) = capture
        .finish()
        .map_err(|error| format!("state capture: {error:?}"))?;
    Ok(bytes.to_vec())
}

/// Gauges of the last observed epoch and totals of the journal.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Metrics {
    pub epoch: u64,
    pub active: usize,
    pub revealed: usize,
    /// Unrevealed commitments once fewer than the configured margin of heights remain.
    pub unrevealed_near_end: usize,
    pub applied: usize,
    pub refused: usize,
    pub forfeited: usize,
}

impl Metrics {
    #[must_use]
    pub fn observe(view: &EpochView, journal: &ScoreJournal, margin: u64) -> Self {
        let (applied, refused, forfeited) = journal.totals();
        Self {
            epoch: view.frozen.epoch,
            active: view.active,
            revealed: view.revealed,
            unrevealed_near_end: if view.near_end(margin) {
                view.unrevealed
            } else {
                0
            },
            applied,
            refused,
            forfeited,
        }
    }

    /// Prometheus text exposition.
    #[must_use]
    pub fn render(&self) -> String {
        let gauges = [
            ("epoch", "gauge", self.epoch),
            ("active", "gauge", count(self.active)),
            ("quorum", "gauge", count(QUORUM)),
            ("below_quorum", "gauge", u64::from(self.active < QUORUM)),
            ("revealed", "gauge", count(self.revealed)),
            (
                "unrevealed_near_end",
                "gauge",
                count(self.unrevealed_near_end),
            ),
            ("applied_total", "counter", count(self.applied)),
            ("refused_total", "counter", count(self.refused)),
            ("forfeited_total", "counter", count(self.forfeited)),
        ];
        let mut out = String::new();
        for (name, kind, value) in gauges {
            for part in [
                "# TYPE paxai_evaluator_",
                name,
                " ",
                kind,
                "\npaxai_evaluator_",
                name,
                " ",
                &value.to_string(),
                "\n",
            ] {
                out.push_str(part);
            }
        }
        out
    }
}

fn count(value: usize) -> u64 {
    u64::try_from(value).unwrap_or(u64::MAX)
}

fn health_response(method: &str, path: &str, ready: bool, metrics: &Metrics) -> Response {
    match (method, path) {
        ("GET", "/healthz") => ok(r#"{"status":"ok"}"#.to_owned()),
        ("GET", "/readyz") if ready => ok(r#"{"status":"ready"}"#.to_owned()),
        ("GET", "/readyz") => refusal(503, "not_ready", None),
        ("GET", "/metrics") => ok(metrics.render()),
        _ => refusal(404, "not_found", None),
    }
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| {
            u64::try_from(since.as_millis()).unwrap_or(u64::MAX)
        })
}

fn serve_health(
    metrics: Arc<Mutex<Metrics>>,
    last_ok: Arc<AtomicU64>,
    window_ms: u64,
) -> Result<(), String> {
    let listen = health_listen()?;
    let listener =
        TcpListener::bind(listen).map_err(|error| format!("health bind {listen}: {error}"))?;
    thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            if stream.set_read_timeout(Some(IO_TIMEOUT)).is_err()
                || stream.set_write_timeout(Some(IO_TIMEOUT)).is_err()
            {
                continue;
            }
            let last = last_ok.load(Ordering::Relaxed);
            let ready = last != 0 && now_ms().saturating_sub(last) <= window_ms;
            let observed = metrics.lock().map(|shared| *shared).unwrap_or_default();
            let response = parse_client_request(&mut stream).map_or_else(
                |_| refusal(400, "invalid_request", None),
                |request| health_response(&request.method, &request.path, ready, &observed),
            );
            if let Err(error) = write_response(&mut stream, &response) {
                eprintln!("ai-evaluator: health response: {error}");
            }
        }
    });
    Ok(())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Config {
    rpc_url: String,
    evm_chain_id: u64,
    gas_limit: u64,
    /// Decimal `uint128` program fee limit.
    fee_limit: String,
    response_capacity: u32,
    budget: [u64; 7],
    capabilities: String,
    access_declaration: String,
    chain: String,
    program: String,
    evaluator: String,
    owner: String,
    reader: String,
    state_dir: PathBuf,
    assessments_dir: PathBuf,
    poll_ms: u64,
    near_end_heights: u64,
    receipt_timeout_ms: u64,
}

impl Config {
    fn load(path: &Path) -> Result<Self, String> {
        let bytes = fs::read(path).map_err(|error| format!("{}: {error}", path.display()))?;
        let config: Self = serde_json::from_slice(&bytes)
            .map_err(|error| format!("{}: {error}", path.display()))?;
        if config.poll_ms == 0 || config.gas_limit == 0 || config.receipt_timeout_ms == 0 {
            return Err("poll_ms, gas_limit and receipt_timeout_ms must be positive".to_owned());
        }
        Ok(config)
    }

    fn identity(&self) -> Result<Identity, String> {
        Ok(Identity {
            chain: ChainDomain::new(bytes32(&self.chain, "chain")?).map_err(app)?,
            program: ProgramId::new(bytes32(&self.program, "program")?).map_err(app)?,
            evaluator: EvaluatorId::new(bytes32(&self.evaluator, "evaluator")?).map_err(app)?,
            owner: PrincipalId::new(bytes32(&self.owner, "owner")?).map_err(app)?,
            reader: PrincipalId::new(bytes32(&self.reader, "reader")?).map_err(app)?,
        })
    }
}

fn word_usize(value: usize) -> [u8; 32] {
    let mut word = [0; 32];
    word[32 - std::mem::size_of::<usize>()..].copy_from_slice(&value.to_be_bytes());
    word
}

fn word_u128(value: u128) -> [u8; 32] {
    let mut word = [0; 32];
    word[16..].copy_from_slice(&value.to_be_bytes());
    word
}

fn abi_word(data: &[u8], index: usize) -> Result<&[u8], String> {
    index
        .checked_mul(32)
        .and_then(|start| data.get(start..start.checked_add(32)?))
        .ok_or_else(|| "short ABI return".to_owned())
}

fn abi_usize(word: &[u8]) -> Result<usize, String> {
    let (high, low) = word.split_at_checked(24).ok_or("short ABI word")?;
    if high.iter().any(|byte| *byte != 0) {
        return Err("ABI word out of range".to_owned());
    }
    let low: [u8; 8] = low.try_into().map_err(|_| "short ABI word".to_owned())?;
    usize::try_from(u64::from_be_bytes(low)).map_err(|_| "ABI word out of range".to_owned())
}

/// The dynamic `bytes` whose offset is head word `index`.
fn abi_bytes(data: &[u8], index: usize) -> Result<Vec<u8>, String> {
    let offset = abi_usize(abi_word(data, index)?)?;
    let len = abi_usize(
        data.get(offset..offset.saturating_add(32))
            .ok_or("short ABI return")?,
    )?;
    offset
        .checked_add(32)
        .and_then(|start| data.get(start..start.checked_add(len)?))
        .map(<[u8]>::to_vec)
        .ok_or_else(|| "short ABI return".to_owned())
}

/// `(int32 terminalResult, uint8 terminalKind, bytes response)`.
fn decode_terminal(data: &[u8]) -> Result<(i32, u8, Vec<u8>), String> {
    let result: [u8; 4] = abi_word(data, 0)?[28..]
        .try_into()
        .map_err(|_| "short ABI word".to_owned())?;
    let kind = abi_word(data, 1)?[31];
    Ok((i32::from_be_bytes(result), kind, abi_bytes(data, 2)?))
}

/// `getStateHead` record: `LXPS2 || len:u32 || LXPS1 || program32 || lifecycle || ...`, or the
/// bare `LXPS1` record.
fn require_active(record: &[u8], program: &[u8; 32]) -> Result<(), String> {
    let inner = match record.strip_prefix(b"LXPS2") {
        Some(wrapped) => {
            let (len, rest) = wrapped.split_at_checked(4).ok_or("truncated state head")?;
            let len = usize::try_from(u32::from_be_bytes(
                len.try_into()
                    .map_err(|_| "truncated state head".to_owned())?,
            ))
            .map_err(|_| "state head length overflow".to_owned())?;
            rest.get(..len).ok_or("truncated state head")?
        }
        None => record,
    };
    let body = inner
        .strip_prefix(b"LXPS1")
        .ok_or("state head is not a program record")?;
    if body.get(..32) != Some(program.as_slice()) {
        return Err("state head names another program".to_owned());
    }
    match body.get(32) {
        Some(&LIFECYCLE_ACTIVE) => Ok(()),
        Some(lifecycle) => Err(format!("program lifecycle {lifecycle} is not active")),
        None => Err("truncated state head".to_owned()),
    }
}

fn data_bytes(value: &Value) -> Result<Vec<u8>, String> {
    value
        .as_str()
        .and_then(|text| text.strip_prefix("0x"))
        .and_then(|text| unhex(&text.to_ascii_lowercase()))
        .ok_or_else(|| format!("malformed data {value}"))
}

fn quantity(value: &Value) -> Result<u128, String> {
    value
        .as_str()
        .and_then(|text| text.strip_prefix("0x"))
        .and_then(|text| u128::from_str_radix(text, 16).ok())
        .ok_or_else(|| format!("malformed quantity {value}"))
}

fn quantity_u64(value: &Value) -> Result<u64, String> {
    u64::try_from(quantity(value)?).map_err(|_| format!("quantity {value} overflows"))
}

fn digest(value: &Value) -> Result<Digest32, String> {
    let bytes = data_bytes(value)?;
    Digest32::new(<[u8; 32]>::try_from(bytes).map_err(|_| format!("malformed digest {value}"))?)
        .map_err(app)
}

/// A `pax_sequencerReceipt` answer: `None` while unknown or only submitted.
fn receipt_outcome(receipt: &Value) -> Result<Option<CallOutcome>, String> {
    if receipt.is_null() {
        return Ok(None);
    }
    match receipt.get("rung").and_then(Value::as_str) {
        Some("submitted") => Ok(None),
        Some("receipted" | "committed") => {
            let response = receipt
                .get("returnData")
                .map_or_else(|| Ok(Vec::new()), data_bytes)
                .and_then(|data| decode_terminal(&data))
                .map(|(_, _, response)| response);
            match receipt.get("status").and_then(Value::as_str) {
                Some("0x1") => outcome_of(&response?).map(Some),
                Some("0x0") => Ok(Some(
                    match response.and_then(|response| outcome_of(&response)) {
                        Ok(CallOutcome::Refused(error)) => CallOutcome::Refused(error),
                        _ => CallOutcome::Refused(None),
                    },
                )),
                _ => Err("receipt without a status".to_owned()),
            }
        }
        _ => Err("receipt with an unknown rung".to_owned()),
    }
}

struct Head {
    number: u64,
    hash: Digest32,
    state_root: Digest32,
}

/// The sequencer endpoint and the fixed native-call terms of the programs precompile.
struct Chain {
    rpc: Sequencer,
    program: [u8; 32],
    capabilities: Vec<u8>,
    access_declaration: Vec<u8>,
    response_capacity: u32,
    budget: [u64; 7],
    fee_limit: u128,
    evm_chain_id: u64,
    gas_limit: u64,
    receipt_timeout: Duration,
}

impl Chain {
    fn new(config: &Config, program: ProgramId) -> Result<Self, String> {
        Ok(Self {
            rpc: Sequencer::new(http_agent()?, config.rpc_url.clone())?,
            program: program.bytes(),
            capabilities: unhex(&config.capabilities).ok_or("capabilities are not hex")?,
            access_declaration: unhex(&config.access_declaration)
                .ok_or("access_declaration is not hex")?,
            response_capacity: config.response_capacity,
            budget: config.budget,
            fee_limit: config
                .fee_limit
                .parse()
                .map_err(|_| "fee_limit is not a decimal uint128".to_owned())?,
            evm_chain_id: config.evm_chain_id,
            gas_limit: config.gas_limit,
            receipt_timeout: Duration::from_millis(config.receipt_timeout_ms),
        })
    }

    /// `call` or `simulateCall` calldata for one envelope.
    fn calldata(&self, selector: [u8; 4], envelope: &[u8]) -> Vec<u8> {
        let parts: [&[u8]; 4] = [
            ENTRYPOINT.as_bytes(),
            envelope,
            &self.capabilities,
            &self.access_declaration,
        ];
        let mut head = selector.to_vec();
        head.extend_from_slice(&self.program);
        head.extend_from_slice(&word_u128(u128::from(GUEST_ABI)));
        let mut tail = Vec::new();
        for part in parts {
            head.extend_from_slice(&word_usize(CALL_HEAD_BYTES + tail.len()));
            tail.extend_from_slice(&word_usize(part.len()));
            tail.extend_from_slice(part);
            tail.resize(tail.len().next_multiple_of(32), 0);
        }
        head.extend_from_slice(&word_u128(u128::from(self.response_capacity)));
        for limit in self.budget {
            head.extend_from_slice(&word_u128(u128::from(limit)));
        }
        head.extend_from_slice(&word_u128(self.fee_limit));
        head.extend_from_slice(&tail);
        head
    }

    /// The finalized head: the committed height every read is pinned to.
    fn head(&self) -> Result<Head, String> {
        let block = self
            .rpc
            .call("eth_getBlockByNumber", &json!(["finalized", false]))?;
        Ok(Head {
            number: quantity_u64(&block["number"])?,
            hash: digest(&block["hash"])?,
            state_root: digest(&block["stateRoot"])?,
        })
    }

    fn view_call(
        &self,
        from: Option<[u8; 20]>,
        data: &[u8],
        block: u64,
    ) -> Result<Vec<u8>, String> {
        let mut request = json!({
            "to": format!("0x{}", hex(&PROGRAMS_PRECOMPILE)),
            "data": format!("0x{}", hex(data)),
        });
        if let Some(from) = from {
            request["from"] = json!(format!("0x{}", hex(&from)));
        }
        data_bytes(
            &self
                .rpc
                .call("eth_call", &json!([request, format!("0x{block:x}")]))?,
        )
    }

    /// Refuses unless 0x101D `getStateHead` reports the program ACTIVE at `block`.
    fn require_active(&self, block: u64) -> Result<(), String> {
        let mut data = STATE_HEAD_SELECTOR.to_vec();
        data.extend_from_slice(&self.program);
        require_active(
            &abi_bytes(&self.view_call(None, &data, block)?, 0)?,
            &self.program,
        )
    }

    /// One `simulateCall` read from `from` at `block`, answering the guest result frame.
    fn simulate(&self, from: [u8; 20], block: u64, envelope: &[u8]) -> Result<Vec<u8>, String> {
        let data = self.calldata(SIMULATE_SELECTOR, envelope);
        let (result, kind, response) =
            decode_terminal(&self.view_call(Some(from), &data, block)?)?;
        if response.is_empty() {
            return Err(format!(
                "simulateCall ended with kind {kind} result {result} and no response"
            ));
        }
        if kind != TERMINAL_SUCCESS {
            eprintln!("ai-evaluator: simulateCall ended with kind {kind} result {result}");
        }
        Ok(response)
    }

    /// Signs and sends one `call` with the configured gas limit, so a guest refusal lands as a
    /// reverted receipt; `None` while the receipt is still pending at the deadline.
    fn send(&self, sender: &EvmSender, envelope: &[u8]) -> Result<Option<CallOutcome>, String> {
        let from = format!("0x{}", hex(&sender.address()));
        let nonce = quantity_u64(
            &self
                .rpc
                .call("eth_getTransactionCount", &json!([from, "pending"]))?,
        )?;
        let priority = quantity(&self.rpc.call("eth_maxPriorityFeePerGas", &json!([]))?)?;
        let gas_price = quantity(&self.rpc.call("eth_gasPrice", &json!([]))?)?;
        let raw = sender
            .sign_transaction(&Eip1559Call {
                chain_id: self.evm_chain_id,
                nonce,
                max_priority_fee_per_gas: priority,
                max_fee_per_gas: gas_price.max(priority),
                gas_limit: self.gas_limit,
                to: PROGRAMS_PRECOMPILE,
                data: self.calldata(CALL_SELECTOR, envelope),
            })
            .map_err(|error| error.to_string())?;
        let hash = self.rpc.call(
            "eth_sendRawTransaction",
            &json!([format!("0x{}", hex(&raw))]),
        )?;
        let hash = hash
            .as_str()
            .ok_or("eth_sendRawTransaction: malformed hash")?
            .to_owned();
        let deadline = Instant::now() + self.receipt_timeout;
        loop {
            if let Some(outcome) =
                receipt_outcome(&self.rpc.call("pax_sequencerReceipt", &json!([hash]))?)?
            {
                return Ok(Some(outcome));
            }
            if Instant::now() >= deadline {
                return Ok(None);
            }
            thread::sleep(RECEIPT_POLL);
        }
    }
}

/// One poll: committed head, program lifecycle, full state capture, then at most one call.
fn tick(chain: &Chain, sender: &EvmSender, evaluator: &mut Evaluator) -> Result<EpochView, String> {
    let head = chain.head()?;
    chain.require_active(head.number)?;
    let identity = evaluator.identity;
    let proof = ReadProof {
        chain: identity.chain,
        program: identity.program,
        native_state_root: head.state_root,
        observed_sequence: head.number,
        execution_height: head.number,
        batch_id: head.hash,
    };
    let state = capture_state(&identity, &proof, |envelope| {
        chain.simulate(sender.address(), head.number, envelope)
    })?;
    let view = EpochView::read(&state, &identity, head.number.saturating_add(1))?;
    if let Some(call) = evaluator.step(&view)? {
        if let Some(outcome) = chain.send(sender, &call.envelope)? {
            evaluator.settle(&call, outcome)?;
        }
    }
    evaluator.journal.journal.probe_writable()?;
    Ok(view)
}

fn run() -> Result<(), String> {
    let config = Config::load(Path::new(&required_env(CONFIG_VARIABLE)?))?;
    let identity = config.identity()?;
    let key = AssessmentKey::load(Path::new(&required_env(KEY_VARIABLE)?))?;
    let sender =
        EvmSender::from_env(EVM_SENDER_PREFIX).map_err(|error| format!("EVM sender: {error}"))?;
    let chain = Chain::new(&config, identity.program)?;
    chain.rpc.check_sender(&key.0, sender.address())?;
    let mut evaluator = Evaluator::open(identity, key, &config.state_dir, &config.assessments_dir)?;
    let metrics = Arc::new(Mutex::new(Metrics::default()));
    let last_ok = Arc::new(AtomicU64::new(0));
    serve_health(
        Arc::clone(&metrics),
        Arc::clone(&last_ok),
        config.poll_ms.saturating_mul(READY_POLLS),
    )?;
    let poll = Duration::from_millis(config.poll_ms);
    loop {
        let started = Instant::now();
        match tick(&chain, &sender, &mut evaluator) {
            Ok(view) => {
                let observed =
                    Metrics::observe(&view, evaluator.journal(), config.near_end_heights);
                if let Ok(mut shared) = metrics.lock() {
                    *shared = observed;
                }
                last_ok.store(now_ms(), Ordering::Relaxed);
            }
            Err(error) => eprintln!("ai-evaluator: {error}"),
        }
        thread::sleep(poll.saturating_sub(started.elapsed()));
    }
}

#[must_use]
pub fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("ai-evaluator: {error}");
            ExitCode::FAILURE
        }
    }
}
