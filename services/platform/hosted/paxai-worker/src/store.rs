//! F02-R017/R023/R024 durable job store shared by every replica of one worker.
//!
//! One directory on a POSIX filesystem holds the admissions of every replica. Each transaction
//! holds an exclusive `flock` on `lock`; every file is written to a temporary name, fsynced and
//! renamed or linked into place, then its directory is fsynced. Layout:
//! `counters` = `service_job_sequence:u64 || fencing_token:u64`;
//! `jobs/<key>/` = immutable `admission` and `payload`, replace-by-rename `state`, create-once
//! `result`, replace-until-result `output`, append-only `evidence-<n>` and `conflict-<n>`;
//! `active/<key>` marks ACCEPTED, RUNNING and `UNKNOWN_EXECUTION` jobs and is checked against
//! `state` on every scan; `requests/<H(market||request)>/<customer>` holds the job key for
//! queries by request id; `markers/<marker>` = `envelope_digest32 || expiry:u64 ||
//! recorded_ms:u64`; `tombstones/<key>` = `H(key || request_commitment)`.
//! A job directory appears through one atomic rename of a fully written staging directory.
use crate::auth::{decode_service, Admission, ServiceContext, ServiceError, ServiceRequest};
use crate::jobs::Outcome;
use layerx_programs_ai_market::{
    codec::{self, Reader},
    errors::{ApplicationError, CodecResult, ARITHMETIC, NON_CANONICAL},
    types::{
        ChainDomain, Digest32, MarketId, MetadataDigest, Presence, PrincipalId, ProgramId,
        PublicKey32, RequestId, RosterDigest, TaskId, WorkerId,
    },
    MAX_ENVELOPE_BYTES, SCHEMA_VERSION,
};
use std::fs::{self, File, OpenOptions};
use std::io::{self, ErrorKind, Write};
use std::path::{Path, PathBuf};

pub const MAX_REFERENCE_BYTES: usize = 4_096;
pub const MAX_ENCRYPTED_BYTES: usize = 1_048_576;
pub const MAX_RUNNING: usize = 32;
pub const MAX_QUEUED: usize = 128;
pub const MAX_QUEUED_BYTES: u64 = 134_217_728;
pub const RETENTION_MS: u64 = 2_592_000_000;
pub const KEY_DOMAIN: &str = "PAXAI/worker-job-key/v1";
pub const REQUEST_DOMAIN: &str = "PAXAI/worker-job-request/v1";
pub const TOMBSTONE_DOMAIN: &str = "PAXAI/worker-job-tombstone/v1";
const MAX_ACKNOWLEDGMENT_BYTES: usize = 1_024;
const STATE_BYTES: usize = 33;
const MARKER_BYTES: usize = 48;
const DIRECTORIES: [&str; 6] = [
    "jobs",
    "active",
    "requests",
    "markers",
    "tombstones",
    "staging",
];

trait Durable<T> {
    fn durable(self) -> Result<T, ServiceError>;
}
impl<T> Durable<T> for io::Result<T> {
    fn durable(self) -> Result<T, ServiceError> {
        self.map_err(|_| ServiceError::ExecutionUnavailable)
    }
}
impl<T> Durable<T> for Result<T, ApplicationError> {
    fn durable(self) -> Result<T, ServiceError> {
        self.map_err(|_| ServiceError::ExecutionUnavailable)
    }
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(char::from(DIGITS[usize::from(byte >> 4)]));
        out.push(char::from(DIGITS[usize::from(byte & 0x0F)]));
    }
    out
}

fn unhex(text: &str) -> Option<[u8; 32]> {
    let digits = text.as_bytes();
    if digits.len() != 64 {
        return None;
    }
    let mut out = [0; 32];
    for (byte, pair) in out.iter_mut().zip(digits.chunks_exact(2)) {
        let high = char::from(pair[0]).to_digit(16)?;
        let low = char::from(pair[1]).to_digit(16)?;
        *byte = u8::try_from(high * 16 + low).ok()?;
    }
    Some(out)
}

/// Uniqueness key `H("PAXAI/worker-job-key/v1", market32 || customer32 || request32)`; it is
/// also the admission identity the runner sees.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct JobKey(Digest32);
impl JobKey {
    /// # Errors
    /// `NonCanonical` when the digest is all zero.
    pub fn derive(
        market: MarketId,
        customer: PrincipalId,
        request: RequestId,
    ) -> Result<Self, ServiceError> {
        let mut bytes = [0; 96];
        bytes[..32].copy_from_slice(market.as_bytes());
        bytes[32..64].copy_from_slice(customer.as_bytes());
        bytes[64..].copy_from_slice(request.as_bytes());
        Ok(Self(codec::domain_hash(KEY_DOMAIN, &bytes)?))
    }
    #[must_use]
    pub const fn from_digest(digest: Digest32) -> Self {
        Self(digest)
    }
    #[must_use]
    pub const fn digest(self) -> Digest32 {
        self.0
    }
    fn name(self) -> String {
        hex(self.0.as_bytes())
    }
    fn parse(name: &str) -> Option<Self> {
        Digest32::new(unhex(name)?).ok().map(Self)
    }
}

/// Local lifecycle: `Ended` carries the R009 outcome; `UNKNOWN_EXECUTION` is not final.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum JobState {
    Accepted,
    Running,
    Ended(Outcome),
}
impl JobState {
    const ACCEPTED: u8 = 6;
    const RUNNING: u8 = 7;
    #[must_use]
    pub const fn code(self) -> u8 {
        match self {
            Self::Accepted => Self::ACCEPTED,
            Self::Running => Self::RUNNING,
            Self::Ended(outcome) => outcome.code(),
        }
    }
    fn from_code(code: u8) -> CodecResult<Self> {
        match code {
            Self::ACCEPTED => Ok(Self::Accepted),
            Self::RUNNING => Ok(Self::Running),
            other => Outcome::from_code(other)
                .map(Self::Ended)
                .map_err(|_| NON_CANONICAL),
        }
    }
    /// Terminal and immutable: every outcome except `UNKNOWN_EXECUTION`.
    #[must_use]
    pub const fn is_final(self) -> bool {
        matches!(self, Self::Ended(outcome) if !matches!(outcome, Outcome::UnknownExecution))
    }
    /// Holds a queue slot and its encrypted bytes.
    #[must_use]
    pub const fn is_queued(self) -> bool {
        matches!(self, Self::Accepted | Self::Running)
    }
}

/// Mutable part of one job, replaced as a whole under the store lock.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StateRecord {
    pub state: JobState,
    pub fence: u64,
    pub lease_until_ms: u64,
    pub cancel_requested: bool,
    pub late: bool,
    pub changed_ms: u64,
    pub payload_bytes: u32,
}
impl StateRecord {
    fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(STATE_BYTES);
        out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
        out.push(self.state.code());
        out.extend_from_slice(&self.fence.to_be_bytes());
        out.extend_from_slice(&self.lease_until_ms.to_be_bytes());
        out.push(u8::from(self.cancel_requested));
        out.push(u8::from(self.late));
        out.extend_from_slice(&self.changed_ms.to_be_bytes());
        out.extend_from_slice(&self.payload_bytes.to_be_bytes());
        out
    }
    fn decode(bytes: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(bytes);
        if r.u16()? != SCHEMA_VERSION {
            return Err(NON_CANONICAL);
        }
        let value = Self {
            state: JobState::from_code(r.u8()?)?,
            fence: r.u64()?,
            lease_until_ms: r.u64()?,
            cancel_requested: r.boolean()?,
            late: r.boolean()?,
            changed_ms: r.u64()?,
            payload_bytes: r.u32()?,
        };
        r.finish()?;
        Ok(value)
    }
}

/// Immutable admission transaction: request bytes and commitment, the caller authorization
/// context, model/deployment, frozen key version, deadline and the signed acknowledgment.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct JobRecord {
    pub key: JobKey,
    pub admission: Admission,
    pub unit_kind: u8,
    pub max_input_units: u32,
    pub accepted_ms: u64,
    pub sequence: u64,
    pub request: Vec<u8>,
    pub input_manifest: Vec<u8>,
    pub acknowledgment: Vec<u8>,
}
impl JobRecord {
    /// The admitted `ServiceRequest`, decoded from the saved signed envelope.
    ///
    /// # Errors
    /// `ExecutionUnavailable` when the saved bytes no longer decode.
    pub fn request(&self) -> Result<ServiceRequest, ServiceError> {
        decode_service(&self.request)
            .and_then(|envelope| ServiceRequest::decode(&envelope.payload))
            .map_err(|_| ServiceError::ExecutionUnavailable)
    }
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let mut out = Vec::new();
        out.extend_from_slice(&SCHEMA_VERSION.to_be_bytes());
        out.extend_from_slice(self.key.0.as_bytes());
        write_admission(&mut out, &self.admission);
        out.push(self.unit_kind);
        out.extend_from_slice(&self.max_input_units.to_be_bytes());
        out.extend_from_slice(&self.accepted_ms.to_be_bytes());
        out.extend_from_slice(&self.sequence.to_be_bytes());
        for bytes in [&self.request, &self.input_manifest, &self.acknowledgment] {
            let length = u32::try_from(bytes.len()).map_err(|_| ARITHMETIC)?;
            out.extend_from_slice(&length.to_be_bytes());
            out.extend_from_slice(bytes);
        }
        Ok(out)
    }
    fn decode(bytes: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(bytes);
        if r.u16()? != SCHEMA_VERSION {
            return Err(NON_CANONICAL);
        }
        let value = Self {
            key: JobKey(Digest32::new(r.fixed()?)?),
            admission: read_admission(&mut r)?,
            unit_kind: r.u8()?,
            max_input_units: r.u32()?,
            accepted_ms: r.u64()?,
            sequence: r.u64()?,
            request: r.bytes(MAX_ENVELOPE_BYTES)?.to_vec(),
            input_manifest: r.bytes(MAX_REFERENCE_BYTES)?.to_vec(),
            acknowledgment: r.bytes(MAX_ACKNOWLEDGMENT_BYTES)?.to_vec(),
        };
        r.finish()?;
        Ok(value)
    }
}

fn write_admission(out: &mut Vec<u8>, a: &Admission) {
    let c = &a.context;
    for digest in [
        c.chain.as_bytes(),
        c.program.as_bytes(),
        c.market.as_bytes(),
        c.actor.as_bytes(),
    ] {
        out.extend_from_slice(digest);
    }
    out.extend_from_slice(&c.epoch.to_be_bytes());
    out.extend_from_slice(&c.config.to_be_bytes());
    match c.roster {
        Presence::Absent => out.extend_from_slice(&[0; 32]),
        Presence::Present(roster) => out.extend_from_slice(roster.as_bytes()),
    }
    out.extend_from_slice(&c.sequence.to_be_bytes());
    out.extend_from_slice(&c.expiry.to_be_bytes());
    out.extend_from_slice(c.request.as_bytes());
    for digest in [
        a.worker.as_bytes(),
        a.owner.as_bytes(),
        &a.delegate.0,
        a.task.as_bytes(),
        a.request_commitment.as_bytes(),
        a.metadata.as_bytes(),
    ] {
        out.extend_from_slice(digest);
    }
    out.extend_from_slice(&a.metadata_revision.to_be_bytes());
    for digest in [
        a.capability.as_bytes(),
        a.model.as_bytes(),
        a.deployment.as_bytes(),
    ] {
        out.extend_from_slice(digest);
    }
    out.extend_from_slice(&a.generation.to_be_bytes());
    out.extend_from_slice(&a.key_version.to_be_bytes());
    out.extend_from_slice(&a.deadline_ms.to_be_bytes());
    out.extend_from_slice(&a.task_deadline.to_be_bytes());
    out.extend_from_slice(&a.admitted_height.to_be_bytes());
}

fn read_admission(r: &mut Reader<'_>) -> CodecResult<Admission> {
    let context = ServiceContext {
        chain: ChainDomain::new(r.fixed()?)?,
        program: ProgramId::new(r.fixed()?)?,
        market: MarketId::new(r.fixed()?)?,
        actor: PrincipalId::new(r.fixed()?)?,
        epoch: r.u64()?,
        config: r.u64()?,
        roster: match r.fixed::<32>()? {
            bytes if bytes == [0; 32] => Presence::Absent,
            bytes => Presence::Present(RosterDigest::new(bytes)?),
        },
        sequence: r.u64()?,
        expiry: r.u64()?,
        request: RequestId::new(r.fixed()?)?,
    };
    Ok(Admission {
        context,
        worker: WorkerId::new(r.fixed()?)?,
        owner: PrincipalId::new(r.fixed()?)?,
        delegate: PublicKey32(r.fixed()?),
        task: TaskId::new(r.fixed()?)?,
        request_commitment: Digest32::new(r.fixed()?)?,
        metadata: MetadataDigest::new(r.fixed()?)?,
        metadata_revision: r.u64()?,
        capability: Digest32::new(r.fixed()?)?,
        model: Digest32::new(r.fixed()?)?,
        deployment: Digest32::new(r.fixed()?)?,
        generation: r.u64()?,
        key_version: r.u64()?,
        deadline_ms: r.u32()?,
        task_deadline: r.u64()?,
        admitted_height: r.u64()?,
    })
}

/// One job as the store holds it; `result` is the complete signed result manifest.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Job {
    pub record: JobRecord,
    pub state: StateRecord,
    pub result: Option<Vec<u8>>,
}

/// A new admission, inserted only when its key is neither present nor tombstoned.
#[derive(Clone, Copy, Debug)]
pub struct NewJob<'a> {
    pub key: JobKey,
    pub admission: Admission,
    pub request: &'a [u8],
    pub input_manifest: &'a [u8],
    pub payload: &'a [u8],
    pub unit_kind: u8,
    pub max_input_units: u32,
    pub accepted_ms: u64,
}

/// A signed terminal record and, for a success, the retained output ciphertext.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Terminal {
    pub outcome: Outcome,
    pub result: Vec<u8>,
    pub output: Option<Vec<u8>>,
}

/// Effects of one transaction, applied in order: evidence, conflict, terminal record, state.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Change {
    pub state: Option<StateRecord>,
    pub terminal: Option<Terminal>,
    pub evidence: Option<Vec<u8>>,
    pub conflict: Option<Vec<u8>>,
}

/// Queue occupancy over ACCEPTED and RUNNING jobs.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Usage {
    pub queued: usize,
    pub running: usize,
    pub queued_bytes: u64,
}

/// The counters of one open transaction; they persist before any other effect.
#[derive(Debug)]
pub struct Txn<'s> {
    store: &'s JobStore,
    counters: [u64; 2],
    dirty: bool,
}
impl Txn<'_> {
    /// Next `service_job_sequence`, also the actor sequence of signed results.
    ///
    /// # Errors
    /// `Overflow` at `u64::MAX`.
    pub fn next_sequence(&mut self) -> Result<u64, ServiceError> {
        self.next(0)
    }
    /// Next lease fencing token.
    ///
    /// # Errors
    /// `Overflow` at `u64::MAX`.
    pub fn next_fence(&mut self) -> Result<u64, ServiceError> {
        self.next(1)
    }
    fn next(&mut self, index: usize) -> Result<u64, ServiceError> {
        let value = self.counters[index]
            .checked_add(1)
            .ok_or(ServiceError::Overflow)?;
        self.counters[index] = value;
        self.dirty = true;
        Ok(value)
    }
    /// # Errors
    /// `ExecutionUnavailable` when the store cannot be read.
    pub fn usage(&self) -> Result<Usage, ServiceError> {
        self.store.scan().map(|(usage, _)| usage)
    }
}

/// The shared durable store of one worker's jobs.
#[derive(Clone, Debug)]
pub struct JobStore {
    root: PathBuf,
}

impl JobStore {
    /// Opens or initializes the store under `root`.
    ///
    /// # Errors
    /// `ExecutionUnavailable` when the directory cannot be created, locked or read.
    pub fn open(root: &Path) -> Result<Self, ServiceError> {
        for directory in DIRECTORIES {
            fs::create_dir_all(root.join(directory)).durable()?;
        }
        let store = Self {
            root: root.to_path_buf(),
        };
        let _lock = store.lock()?;
        if !present(&store.root.join("counters"))? {
            store.write_counters([0, 0])?;
        }
        sync_directory(root)?;
        Ok(store)
    }

    fn lock(&self) -> Result<File, ServiceError> {
        // ponytail: one store-wide lock serializes every replica; shard by key prefix if
        // admission throughput ever needs it.
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(self.root.join("lock"))
            .durable()?;
        file.lock().durable()?;
        Ok(file)
    }

    fn job_dir(&self, key: JobKey) -> PathBuf {
        self.root.join("jobs").join(key.name())
    }

    fn read_counters(&self) -> Result<[u64; 2], ServiceError> {
        let bytes = fs::read(self.root.join("counters")).durable()?;
        let mut r = Reader::new(&bytes);
        let counters = [r.u64().durable()?, r.u64().durable()?];
        r.finish().durable()?;
        Ok(counters)
    }

    fn write_counters(&self, counters: [u64; 2]) -> Result<(), ServiceError> {
        let mut bytes = counters[0].to_be_bytes().to_vec();
        bytes.extend_from_slice(&counters[1].to_be_bytes());
        write_atomic(&self.root.join("counters"), &bytes)
    }

    fn tombstoned(&self, key: JobKey) -> Result<bool, ServiceError> {
        present(&self.root.join("tombstones").join(key.name()))
    }

    fn load(&self, key: JobKey) -> Result<Job, ServiceError> {
        let dir = self.job_dir(key);
        if !present(&dir)? {
            return Err(ServiceError::NotFound);
        }
        let record = JobRecord::decode(&fs::read(dir.join("admission")).durable()?).durable()?;
        let mut state = StateRecord::decode(&fs::read(dir.join("state")).durable()?).durable()?;
        let result = match fs::read(dir.join("result")) {
            Ok(bytes) => {
                let (code, signed) = bytes
                    .split_first()
                    .ok_or(ServiceError::ExecutionUnavailable)?;
                state.state = JobState::Ended(
                    Outcome::from_code(*code).map_err(|_| ServiceError::ExecutionUnavailable)?,
                );
                Some(signed.to_vec())
            }
            Err(error) if error.kind() == ErrorKind::NotFound => None,
            Err(_) => return Err(ServiceError::ExecutionUnavailable),
        };
        Ok(Job {
            record,
            state,
            result,
        })
    }

    /// Usage over the active markers, dropping markers whose job is gone or no longer active.
    fn scan(&self) -> Result<(Usage, Vec<(JobKey, StateRecord)>), ServiceError> {
        let mut usage = Usage::default();
        let mut active = Vec::new();
        let marks = self.root.join("active");
        for entry in fs::read_dir(&marks).durable()? {
            let entry = entry.durable()?;
            let name = entry.file_name();
            let Some(key) = name.to_str().and_then(JobKey::parse) else {
                remove(&entry.path())?;
                continue;
            };
            let state = match self.load(key) {
                Ok(job) => job.state,
                Err(ServiceError::NotFound) => {
                    remove(&entry.path())?;
                    continue;
                }
                Err(error) => return Err(error),
            };
            match state.state {
                JobState::Accepted => usage.queued += 1,
                JobState::Running => usage.running += 1,
                JobState::Ended(Outcome::UnknownExecution) => {}
                JobState::Ended(_) => {
                    remove(&entry.path())?;
                    continue;
                }
            }
            if state.state.is_queued() {
                usage.queued_bytes = usage
                    .queued_bytes
                    .checked_add(u64::from(state.payload_bytes))
                    .ok_or(ServiceError::Overflow)?;
            }
            active.push((key, state));
        }
        sync_directory(&marks)?;
        Ok((usage, active))
    }

    fn request_dir(&self, market: MarketId, request: RequestId) -> Result<PathBuf, ServiceError> {
        let mut bytes = [0; 64];
        bytes[..32].copy_from_slice(market.as_bytes());
        bytes[32..].copy_from_slice(request.as_bytes());
        let digest = codec::domain_hash(REQUEST_DOMAIN, &bytes)?;
        Ok(self.root.join("requests").join(hex(digest.as_bytes())))
    }

    /// The saved job under `key`, if any.
    ///
    /// # Errors
    /// `IdempotencyConflict` when the key is tombstoned; `ExecutionUnavailable` on store failure.
    pub fn find(&self, key: JobKey) -> Result<Option<Job>, ServiceError> {
        let _lock = self.lock()?;
        if self.tombstoned(key)? {
            return Err(ServiceError::IdempotencyConflict);
        }
        match self.load(key) {
            Ok(job) => Ok(Some(job)),
            Err(ServiceError::NotFound) => Ok(None),
            Err(error) => Err(error),
        }
    }

    /// # Errors
    /// `NotFound` for an unknown key; `ExecutionUnavailable` on store failure.
    pub fn job(&self, key: JobKey) -> Result<Job, ServiceError> {
        let _lock = self.lock()?;
        self.load(key)
    }

    /// The immutable encrypted request payload.
    ///
    /// # Errors
    /// `NotFound` for an unknown key; `ExecutionUnavailable` on store failure.
    pub fn payload(&self, key: JobKey) -> Result<Vec<u8>, ServiceError> {
        let _lock = self.lock()?;
        self.load(key)?;
        fs::read(self.job_dir(key).join("payload")).durable()
    }

    /// The retained output ciphertext of a success.
    ///
    /// # Errors
    /// `NotFound` for an unknown key; `ExecutionUnavailable` on store failure.
    pub fn output(&self, key: JobKey) -> Result<Option<Vec<u8>>, ServiceError> {
        let _lock = self.lock()?;
        let job = self.load(key)?;
        if job.result.is_none() {
            return Ok(None);
        }
        match fs::read(self.job_dir(key).join("output")) {
            Ok(bytes) => Ok(Some(bytes)),
            Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
            Err(_) => Err(ServiceError::ExecutionUnavailable),
        }
    }

    /// Retained runner evidence (`evidence`) or contradictory evidence (`conflict`), in order.
    ///
    /// # Errors
    /// `NotFound` for an unknown key; `ExecutionUnavailable` on store failure.
    pub fn retained(&self, key: JobKey, conflicts: bool) -> Result<Vec<Vec<u8>>, ServiceError> {
        let _lock = self.lock()?;
        self.load(key)?;
        let prefix = if conflicts { "conflict" } else { "evidence" };
        let dir = self.job_dir(key);
        let mut out = Vec::new();
        for index in 0usize.. {
            match fs::read(dir.join(format!("{prefix}-{index}"))) {
                Ok(bytes) => out.push(bytes),
                Err(error) if error.kind() == ErrorKind::NotFound => break,
                Err(_) => return Err(ServiceError::ExecutionUnavailable),
            }
        }
        Ok(out)
    }

    /// Jobs indexed under `request` in `market`, whichever customer admitted them.
    ///
    /// # Errors
    /// `ExecutionUnavailable` on store failure.
    pub fn by_request(
        &self,
        market: MarketId,
        request: RequestId,
    ) -> Result<Vec<Job>, ServiceError> {
        let _lock = self.lock()?;
        let dir = self.request_dir(market, request)?;
        let entries = match fs::read_dir(&dir) {
            Ok(entries) => entries,
            Err(error) if error.kind() == ErrorKind::NotFound => return Ok(Vec::new()),
            Err(_) => return Err(ServiceError::ExecutionUnavailable),
        };
        let mut jobs = Vec::new();
        for entry in entries {
            let path = entry.durable()?.path();
            if !hex_named(&path) {
                continue;
            }
            let bytes = fs::read(&path).durable()?;
            let digest: [u8; 32] = bytes
                .try_into()
                .map_err(|_| ServiceError::ExecutionUnavailable)?;
            let key = JobKey(Digest32::new(digest).durable()?);
            match self.load(key) {
                Ok(job) => jobs.push(job),
                Err(ServiceError::NotFound) => {}
                Err(error) => return Err(error),
            }
        }
        Ok(jobs)
    }

    /// Queue occupancy.
    ///
    /// # Errors
    /// `ExecutionUnavailable` on store failure.
    pub fn usage(&self) -> Result<Usage, ServiceError> {
        let _lock = self.lock()?;
        self.scan().map(|(usage, _)| usage)
    }

    /// ACCEPTED, RUNNING and `UNKNOWN_EXECUTION` jobs with their state.
    ///
    /// # Errors
    /// `ExecutionUnavailable` on store failure.
    pub fn active(&self) -> Result<Vec<(JobKey, StateRecord)>, ServiceError> {
        let _lock = self.lock()?;
        self.scan().map(|(_, active)| active)
    }

    /// Atomic unique admission: under the lock, refuses a tombstoned key, returns the saved job
    /// for the same commitment, refuses another commitment, checks the queue bounds, allocates
    /// the next `service_job_sequence`, signs the acknowledgment with it and publishes the job
    /// directory by one rename.
    ///
    /// # Errors
    /// `InputTooLarge` above the payload or reference bound; `IdempotencyConflict` for a
    /// tombstoned key or another commitment; `CapacityExceeded` at the queue count or byte
    /// bound; `Overflow`; signing refusals; `ExecutionUnavailable` when the store cannot commit,
    /// in which case no acknowledgment exists.
    pub fn insert(
        &self,
        job: &NewJob<'_>,
        sign: impl FnOnce(u64) -> Result<Vec<u8>, ServiceError>,
    ) -> Result<Job, ServiceError> {
        if job.payload.len() > MAX_ENCRYPTED_BYTES || job.input_manifest.len() > MAX_REFERENCE_BYTES
        {
            return Err(ServiceError::InputTooLarge);
        }
        let payload_bytes =
            u32::try_from(job.payload.len()).map_err(|_| ServiceError::InputTooLarge)?;
        let _lock = self.lock()?;
        if self.tombstoned(job.key)? {
            return Err(ServiceError::IdempotencyConflict);
        }
        match self.load(job.key) {
            Ok(saved)
                if saved.record.admission.request_commitment
                    == job.admission.request_commitment =>
            {
                return Ok(saved);
            }
            Ok(_) => return Err(ServiceError::IdempotencyConflict),
            Err(ServiceError::NotFound) => {}
            Err(error) => return Err(error),
        }
        let (usage, _) = self.scan()?;
        if usage.queued >= MAX_QUEUED {
            return Err(ServiceError::CapacityExceeded);
        }
        let bytes = usage
            .queued_bytes
            .checked_add(u64::from(payload_bytes))
            .ok_or(ServiceError::Overflow)?;
        if bytes > MAX_QUEUED_BYTES {
            return Err(ServiceError::CapacityExceeded);
        }
        let mut counters = self.read_counters()?;
        let sequence = counters[0].checked_add(1).ok_or(ServiceError::Overflow)?;
        counters[0] = sequence;
        self.write_counters(counters)?;
        let saved = Job {
            record: JobRecord {
                key: job.key,
                admission: job.admission,
                unit_kind: job.unit_kind,
                max_input_units: job.max_input_units,
                accepted_ms: job.accepted_ms,
                sequence,
                request: job.request.to_vec(),
                input_manifest: job.input_manifest.to_vec(),
                acknowledgment: sign(sequence)?,
            },
            state: StateRecord {
                state: JobState::Accepted,
                fence: 0,
                lease_until_ms: 0,
                cancel_requested: false,
                late: false,
                changed_ms: job.accepted_ms,
                payload_bytes,
            },
            result: None,
        };
        self.publish(
            &self.root.join("staging").join(job.key.name()),
            &saved,
            job.payload,
        )?;
        Ok(saved)
    }

    fn publish(&self, staging: &Path, job: &Job, payload: &[u8]) -> Result<(), ServiceError> {
        if present(staging)? {
            fs::remove_dir_all(staging).durable()?;
        }
        fs::create_dir(staging).durable()?;
        write_atomic(&staging.join("admission"), &job.record.encode().durable()?)?;
        write_atomic(&staging.join("payload"), payload)?;
        write_atomic(&staging.join("state"), &job.state.encode())?;
        let admission = &job.record.admission;
        let index = self.request_dir(admission.context.market, admission.context.request)?;
        fs::create_dir_all(&index).durable()?;
        write_atomic(
            &index.join(hex(admission.context.actor.as_bytes())),
            job.record.key.0.as_bytes(),
        )?;
        write_atomic(&self.root.join("active").join(job.record.key.name()), &[])?;
        fs::rename(staging, self.job_dir(job.record.key)).durable()?;
        sync_directory(&self.root.join("jobs"))?;
        sync_directory(&self.root.join("staging"))
    }

    /// One read-modify-write of `key` under the lock. `change` sees the current job and may
    /// draw sequences and fences; its effects apply only when it returns `Ok`.
    ///
    /// # Errors
    /// `NotFound` for an unknown key; refusals of `change`; `ExecutionUnavailable` on store
    /// failure, in which case the state on disk is the last fully written one.
    pub fn update<T>(
        &self,
        key: JobKey,
        change: impl FnOnce(&Job, &mut Txn<'_>) -> Result<(Change, T), ServiceError>,
    ) -> Result<T, ServiceError> {
        let _lock = self.lock()?;
        let job = self.load(key)?;
        let mut txn = Txn {
            store: self,
            counters: self.read_counters()?,
            dirty: false,
        };
        let (effects, value) = change(&job, &mut txn)?;
        if txn.dirty {
            self.write_counters(txn.counters)?;
        }
        self.apply(key, effects)?;
        Ok(value)
    }

    fn apply(&self, key: JobKey, change: Change) -> Result<(), ServiceError> {
        let dir = self.job_dir(key);
        if let Some(bytes) = change.evidence {
            append(&dir, "evidence", &bytes)?;
        }
        if let Some(bytes) = change.conflict {
            append(&dir, "conflict", &bytes)?;
        }
        if let Some(terminal) = change.terminal {
            if let Some(output) = terminal.output {
                write_atomic(&dir.join("output"), &output)?;
            }
            let mut bytes = vec![terminal.outcome.code()];
            bytes.extend_from_slice(&terminal.result);
            if !write_once(&dir.join("result"), &bytes)? {
                return Err(ServiceError::IdempotencyConflict);
            }
        }
        if let Some(state) = change.state {
            write_atomic(&dir.join("state"), &state.encode())?;
            if state.state.is_final() {
                remove(&self.root.join("active").join(key.name()))?;
            }
        }
        Ok(())
    }

    /// Records a transport replay marker keyed by `marker`; the same envelope digest again is a
    /// redelivery, another digest under the same marker refuses.
    ///
    /// # Errors
    /// `IdempotencyConflict` for another digest; `ExecutionUnavailable` on store failure.
    pub fn mark(
        &self,
        marker: Digest32,
        envelope: Digest32,
        expiry: u64,
        now_ms: u64,
    ) -> Result<(), ServiceError> {
        let _lock = self.lock()?;
        let path = self.root.join("markers").join(hex(marker.as_bytes()));
        match fs::read(&path) {
            Ok(bytes) => {
                if bytes.get(..32) == Some(envelope.as_bytes().as_slice()) {
                    Ok(())
                } else {
                    Err(ServiceError::IdempotencyConflict)
                }
            }
            Err(error) if error.kind() == ErrorKind::NotFound => {
                let mut bytes = Vec::with_capacity(MARKER_BYTES);
                bytes.extend_from_slice(envelope.as_bytes());
                bytes.extend_from_slice(&expiry.to_be_bytes());
                bytes.extend_from_slice(&now_ms.to_be_bytes());
                write_atomic(&path, &bytes)
            }
            Err(_) => Err(ServiceError::ExecutionUnavailable),
        }
    }

    /// Retention: final jobs older than `RETENTION_MS` become tombstones holding only
    /// `H(key || request commitment)`; `UNKNOWN_EXECUTION` and unfinished jobs stay. Markers past
    /// both their expiry height and the retention window are dropped. Returns the number of
    /// jobs tombstoned.
    ///
    /// # Errors
    /// `ExecutionUnavailable` on store failure.
    pub fn purge(&self, now_ms: u64, height: u64) -> Result<usize, ServiceError> {
        let _lock = self.lock()?;
        let mut purged = 0;
        for entry in fs::read_dir(self.root.join("jobs")).durable()? {
            let name = entry.durable()?.file_name();
            let Some(key) = name.to_str().and_then(JobKey::parse) else {
                continue;
            };
            let job = self.load(key)?;
            if !job.state.state.is_final() || !retention_over(job.state.changed_ms, now_ms) {
                continue;
            }
            let mut bytes = [0; 64];
            bytes[..32].copy_from_slice(key.0.as_bytes());
            bytes[32..].copy_from_slice(job.record.admission.request_commitment.as_bytes());
            let tombstone = codec::domain_hash(TOMBSTONE_DOMAIN, &bytes)?;
            write_atomic(
                &self.root.join("tombstones").join(key.name()),
                tombstone.as_bytes(),
            )?;
            let admission = &job.record.admission;
            let index = self.request_dir(admission.context.market, admission.context.request)?;
            remove(&index.join(hex(admission.context.actor.as_bytes())))?;
            remove(&self.root.join("active").join(key.name()))?;
            fs::remove_dir_all(self.job_dir(key)).durable()?;
            purged += 1;
        }
        sync_directory(&self.root.join("jobs"))?;
        for entry in fs::read_dir(self.root.join("markers")).durable()? {
            let path = entry.durable()?.path();
            if !hex_named(&path) {
                remove(&path)?;
                continue;
            }
            let bytes = fs::read(&path).durable()?;
            let mut r = Reader::new(&bytes);
            r.take(32).durable()?;
            let (expiry, recorded) = (r.u64().durable()?, r.u64().durable()?);
            if expiry < height && retention_over(recorded, now_ms) {
                remove(&path)?;
            }
        }
        sync_directory(&self.root.join("markers"))?;
        Ok(purged)
    }

    /// The tombstone digest kept for a purged key.
    ///
    /// # Errors
    /// `ExecutionUnavailable` on store failure.
    pub fn tombstone(&self, key: JobKey) -> Result<Option<Vec<u8>>, ServiceError> {
        let _lock = self.lock()?;
        match fs::read(self.root.join("tombstones").join(key.name())) {
            Ok(bytes) => Ok(Some(bytes)),
            Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
            Err(_) => Err(ServiceError::ExecutionUnavailable),
        }
    }
}

/// Complete records carry a bare 64-digit hex name; anything else is an interrupted write.
fn hex_named(path: &Path) -> bool {
    path.file_name()
        .and_then(|name| name.to_str())
        .and_then(unhex)
        .is_some()
}

fn retention_over(since_ms: u64, now_ms: u64) -> bool {
    since_ms
        .checked_add(RETENTION_MS)
        .is_some_and(|until| until <= now_ms)
}

fn present(path: &Path) -> Result<bool, ServiceError> {
    match fs::symlink_metadata(path) {
        Ok(_) => Ok(true),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(false),
        Err(_) => Err(ServiceError::ExecutionUnavailable),
    }
}

fn remove(path: &Path) -> Result<(), ServiceError> {
    match fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(()),
        Err(_) => Err(ServiceError::ExecutionUnavailable),
    }
}

fn sync_directory(path: &Path) -> Result<(), ServiceError> {
    File::open(path).and_then(|dir| dir.sync_all()).durable()
}

fn sync_parent(path: &Path) -> Result<(), ServiceError> {
    sync_directory(path.parent().ok_or(ServiceError::ExecutionUnavailable)?)
}

fn write_temp(path: &Path, bytes: &[u8]) -> Result<PathBuf, ServiceError> {
    let temp = path.with_extension("tmp");
    let mut file = File::create(&temp).durable()?;
    file.write_all(bytes).durable()?;
    file.sync_all().durable()?;
    Ok(temp)
}

fn write_atomic(path: &Path, bytes: &[u8]) -> Result<(), ServiceError> {
    let temp = write_temp(path, bytes)?;
    fs::rename(&temp, path).durable()?;
    sync_parent(path)
}

/// Creates `path` with the complete `bytes` exactly once; `false` when it already exists.
fn write_once(path: &Path, bytes: &[u8]) -> Result<bool, ServiceError> {
    let temp = write_temp(path, bytes)?;
    let created = match fs::hard_link(&temp, path) {
        Ok(()) => true,
        Err(error) if error.kind() == ErrorKind::AlreadyExists => false,
        Err(_) => return Err(ServiceError::ExecutionUnavailable),
    };
    fs::remove_file(&temp).durable()?;
    sync_parent(path)?;
    Ok(created)
}

fn append(dir: &Path, prefix: &str, bytes: &[u8]) -> Result<(), ServiceError> {
    for index in 0usize.. {
        if write_once(&dir.join(format!("{prefix}-{index}")), bytes)? {
            return Ok(());
        }
    }
    Err(ServiceError::Overflow)
}
