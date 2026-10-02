//! Durable record of completed source verifications.
//!
//! Rebuilding is expensive, so a completed verification is persisted and
//! replayed at start-up. A replayed record never asserts a verdict on its own:
//! it carries the digest the rebuild produced, and the registry compares that
//! digest with registered protocol state again, so a record for a program that
//! has since been upgraded resurfaces as a visible mismatch.

use std::fs;
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use std::path::PathBuf;
use std::time::{Duration, Instant};

use layerx_programs::{hex, BuildPlan, ProgramId};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::routes::Response;
use crate::write_atomic;

const RECORD_SUFFIX: &str = "verified";

/// One completed rebuild of one registered program version.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedSource {
    pub program: ProgramId,
    pub version: u32,
    pub source_uri: String,
    pub source_digest: [u8; 32],
    pub artifact_digest: [u8; 32],
    pub plan: BuildPlan,
}

/// Store of completed rebuilds, one file per program version.
#[derive(Clone, Debug)]
pub struct VerifiedSourceStore {
    root: PathBuf,
}

impl VerifiedSourceStore {
    /// Opens, creating the store directory when it is absent.
    ///
    /// # Errors
    ///
    /// Returns the filesystem error that prevented the directory from opening.
    pub fn open(root: PathBuf) -> Result<Self, String> {
        fs::create_dir_all(&root)
            .map_err(|error| format!("could not open the verified source store: {error}"))?;
        Ok(Self { root })
    }

    /// Persists one completed rebuild.
    ///
    /// # Errors
    ///
    /// Returns the filesystem error that prevented durable persistence.
    pub fn record(&self, entry: &VerifiedSource) -> Result<(), String> {
        let document = encode_source(entry);
        let path = self.root.join(format!(
            "{}-{}.{RECORD_SUFFIX}",
            hex::encode(&entry.program.bytes()),
            entry.version
        ));
        write_atomic(&path, document.as_bytes())
            .map_err(|error| format!("could not persist {}: {error}", path.display()))
    }

    pub(crate) fn decode_record(bytes: &str) -> Result<VerifiedSource, String> {
        decode(bytes.as_bytes()).ok_or_else(|| "prepared source verification is corrupt".to_owned())
    }

    pub(crate) fn encode_record(entry: &VerifiedSource) -> String {
        encode_source(entry)
    }

    /// Reads every completed rebuild in program order.
    ///
    /// # Errors
    ///
    /// Returns unreadable directories and refuses corrupt records rather than
    /// dropping a verification silently.
    pub fn records(&self) -> Result<Vec<VerifiedSource>, String> {
        let mut paths = Vec::new();
        let entries = fs::read_dir(&self.root)
            .map_err(|error| format!("could not read the verified source store: {error}"))?;
        for entry in entries {
            let path = entry
                .map_err(|error| format!("could not read the verified source store: {error}"))?
                .path();
            if path.extension().is_some_and(|value| value == RECORD_SUFFIX) {
                paths.push(path);
            }
        }
        paths.sort();
        let mut records = Vec::with_capacity(paths.len());
        for path in paths {
            let bytes = fs::read(&path)
                .map_err(|error| format!("could not read {}: {error}", path.display()))?;
            let record = decode(&bytes).ok_or_else(|| format!("{} is corrupt", path.display()))?;
            records.push(record);
        }
        Ok(records)
    }
}

fn encode_source(entry: &VerifiedSource) -> String {
    json!({
        "program": hex::encode(&entry.program.bytes()),
        "version": entry.version,
        "source_uri": entry.source_uri,
        "source_digest": hex::encode(&entry.source_digest),
        "artifact_digest": hex::encode(&entry.artifact_digest),
        "plan": entry.plan.encode(),
    }).to_string()
}

fn decode(bytes: &[u8]) -> Option<VerifiedSource> {
    let document: Value = serde_json::from_slice(bytes).ok()?;
    Some(VerifiedSource {
        program: ProgramId::new(hex::decode_digest(document["program"].as_str()?).ok()?).ok()?,
        version: u32::try_from(document["version"].as_u64()?).ok()?,
        source_uri: document["source_uri"].as_str()?.to_owned(),
        source_digest: hex::decode_digest(document["source_digest"].as_str()?).ok()?,
        artifact_digest: hex::decode_digest(document["artifact_digest"].as_str()?).ok()?,
        plan: BuildPlan::parse(document["plan"].as_str()?).ok()?,
    })
}

/// Version of the durable verification request record encoding.
pub const VERIFICATION_RECORD_VERSION: u32 = 2;
/// Most request identities retained; only settled identities are evicted.
pub const MAX_VERIFICATION_RECORDS: usize = 4_096;
const REQUEST_SUFFIX: &str = "request";
const OWNER_LOCK: &str = "owner.lock";
/// Longest a request waits for another worker that owns the request journal.
const LEASE_WAIT: Duration = Duration::from_secs(5);
const LEASE_RETRY: Duration = Duration::from_millis(10);

/// The verified publication a completed source verification commits to the
/// event outbox; a recovery re-enqueues exactly these bytes.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Publication {
    pub body: String,
    pub principal: String,
    pub occurred_at: u64,
}

/// Lifecycle of one request identity.
///
/// `Building` is held by the worker that owns the journal lease; found without
/// an owner it is a rebuild that crashed and is recovered by the next request.
/// `Retryable` records a 503 outcome that committed no effect. `Persisted` is
/// a completed verification whose publication may not yet be queued.
/// `Completed` carries the acknowledged terminal response.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(tag = "phase", rename_all = "snake_case", deny_unknown_fields)]
pub enum VerificationState {
    Building,
    Retryable,
    Artifact {
        response: Response,
        source: String,
    },
    Persisted {
        response: Response,
        publication: Publication,
    },
    Completed {
        response: Response,
    },
}

impl VerificationState {
    const fn settled(&self) -> bool {
        matches!(self, Self::Retryable | Self::Completed { .. })
    }
}

/// One durable principal/program/idempotency-key scope.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct VerificationRecord {
    pub version: u32,
    pub scope: String,
    pub principal: String,
    pub program: String,
    pub request_digest: String,
    pub integrity_digest: String,
    pub attempt: u32,
    pub state: VerificationState,
    pub created_at: u64,
    pub updated_at: u64,
}

/// What a worker that owns the lease must do for one request.
#[derive(Debug, Eq, PartialEq)]
pub enum Admission {
    /// Run the rebuild; `record.attempt` > 1 marks a recovered build.
    Build(VerificationRecord),
    /// Re-enqueue the committed publication, then acknowledge the response.
    Publish(VerificationRecord),
    Artifact(VerificationRecord),
    /// Return the recorded terminal response.
    Replay(Response),
    /// The key is bound to a different request.
    Conflict,
    /// Every retained identity is live or ambiguous.
    QuotaExhausted,
}

/// Why the request journal refused an operation. Both fail closed.
#[derive(Debug, Eq, PartialEq)]
pub enum JournalRefusal {
    Corrupt(String),
    Unavailable(String),
}

/// Why a lease was not granted.
#[derive(Debug, Eq, PartialEq)]
pub enum LeaseRefusal {
    Busy,
    Unavailable(String),
}

/// Result of reconciling the request journal when a worker opens.
#[derive(Debug, Default, Eq, PartialEq)]
pub struct Reconciled {
    pub live: usize,
    pub settled: usize,
    pub defects: Vec<String>,
    pub preserved: Vec<PathBuf>,
}

/// Durable journal of source verification requests, one atomically replaced
/// file per scope, serialized across worker processes by an advisory lock.
#[derive(Clone, Debug)]
pub struct VerificationJournal {
    root: PathBuf,
    capacity: usize,
}

/// Exclusive ownership of the request journal; dropping it releases the lock,
/// and a crashed owner releases it with its process.
#[derive(Debug)]
pub struct VerificationLease {
    journal: VerificationJournal,
    _lock: fs::File,
}

impl VerificationJournal {
    /// Opens, creating the private journal directory when it is absent.
    ///
    /// # Errors
    /// Returns the filesystem error that prevented the directory from opening.
    pub fn open(root: PathBuf) -> Result<Self, String> {
        Self::with_capacity(root, MAX_VERIFICATION_RECORDS)
    }

    pub(crate) fn with_capacity(root: PathBuf, capacity: usize) -> Result<Self, String> {
        fs::create_dir_all(&root)
            .and_then(|()| fs::set_permissions(&root, fs::Permissions::from_mode(0o700)))
            .map_err(|error| format!("could not open the verification request journal: {error}"))?;
        Ok(Self { root, capacity })
    }

    /// Takes the journal lease, waiting at most until `deadline`.
    ///
    /// # Errors
    /// `Busy` while another worker owns the journal; `Unavailable` when the
    /// lock cannot be opened.
    pub fn lease(&self, deadline: Instant) -> Result<VerificationLease, LeaseRefusal> {
        let limit = Instant::now()
            .checked_add(LEASE_WAIT)
            .map_or(deadline, |wait| wait.min(deadline));
        loop {
            match self.try_lease() {
                Err(LeaseRefusal::Busy) if Instant::now() < limit => {
                    std::thread::sleep(LEASE_RETRY);
                }
                result => return result,
            }
        }
    }

    fn try_lease(&self) -> Result<VerificationLease, LeaseRefusal> {
        let lock = fs::OpenOptions::new()
            .create(true)
            .truncate(false)
            .write(true)
            .mode(0o600)
            .open(self.root.join(OWNER_LOCK))
            .map_err(|error| LeaseRefusal::Unavailable(format!("journal lock: {error}")))?;
        match lock.try_lock() {
            Ok(()) => Ok(VerificationLease {
                journal: self.clone(),
                _lock: lock,
            }),
            Err(fs::TryLockError::WouldBlock) => Err(LeaseRefusal::Busy),
            Err(fs::TryLockError::Error(error)) => {
                Err(LeaseRefusal::Unavailable(format!("journal lock: {error}")))
            }
        }
    }

    /// Reconciles the journal when a worker opens: preserves interrupted
    /// writes as evidence and validates every record. A journal another
    /// worker owns is left to that owner.
    ///
    /// # Errors
    /// Refuses an unreadable journal.
    pub fn reconcile(&self, now: u64) -> Result<Reconciled, String> {
        let lease = match self.try_lease() {
            Ok(lease) => lease,
            Err(LeaseRefusal::Busy) => return Ok(Reconciled::default()),
            Err(LeaseRefusal::Unavailable(error)) => return Err(error),
        };
        let mut reconciled = Reconciled::default();
        for path in lease.entries()? {
            let name = file_name(&path);
            if let Some(scope) = name.strip_suffix(&format!(".{REQUEST_SUFFIX}.tmp")) {
                reconciled
                    .preserved
                    .extend(lease.preserve_uncertain(scope, now)?);
            }
        }
        for (scope, record) in lease.records()? {
            match record {
                Ok(record) if record.state.settled() => reconciled.settled += 1,
                Ok(_) => reconciled.live += 1,
                Err(defect) => {
                    reconciled.live += 1;
                    reconciled.defects.push(format!("{scope}: {defect}"));
                }
            }
        }
        Ok(reconciled)
    }
}

impl VerificationLease {
    fn path(&self, scope: &str) -> PathBuf {
        self.journal.root.join(format!("{scope}.{REQUEST_SUFFIX}"))
    }

    fn entries(&self) -> Result<Vec<PathBuf>, String> {
        let mut paths = Vec::new();
        for entry in fs::read_dir(&self.journal.root)
            .map_err(|error| format!("could not read the verification request journal: {error}"))?
        {
            paths.push(
                entry
                    .map_err(|error| {
                        format!("could not read the verification request journal: {error}")
                    })?
                    .path(),
            );
        }
        paths.sort();
        Ok(paths)
    }

    /// Moves an interrupted replacement aside, keeping it as evidence; the
    /// committed record stays authoritative because replacement is atomic.
    fn preserve_uncertain(&self, scope: &str, now: u64) -> Result<Option<PathBuf>, String> {
        let temporary = self
            .journal
            .root
            .join(format!("{scope}.{REQUEST_SUFFIX}.tmp"));
        if !temporary.exists() {
            return Ok(None);
        }
        let mut evidence = self
            .journal
            .root
            .join(format!("{scope}.{REQUEST_SUFFIX}.uncertain-{now}"));
        let mut suffix = 0_u32;
        while evidence.exists() {
            suffix = suffix
                .checked_add(1)
                .ok_or("uncertain evidence exhausted")?;
            evidence = self
                .journal
                .root
                .join(format!("{scope}.{REQUEST_SUFFIX}.uncertain-{now}-{suffix}"));
        }
        fs::rename(&temporary, &evidence)
            .and_then(|()| fs::File::open(&self.journal.root)?.sync_all())
            .map_err(|error| format!("could not preserve an uncertain write: {error}"))?;
        Ok(Some(evidence))
    }

    fn read(&self, scope: &str) -> Result<Option<VerificationRecord>, JournalRefusal> {
        let path = self.path(scope);
        let bytes = match fs::read(&path) {
            Ok(bytes) => bytes,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(error) => {
                return Err(JournalRefusal::Unavailable(format!(
                    "could not read {}: {error}",
                    path.display()
                )))
            }
        };
        decode_request(scope, &bytes)
            .map(Some)
            .map_err(|defect| JournalRefusal::Corrupt(format!("{}: {defect}", path.display())))
    }

    fn records(&self) -> Result<Vec<(String, Result<VerificationRecord, String>)>, String> {
        let mut records = Vec::new();
        for path in self.entries()? {
            let name = file_name(&path);
            let Some(scope) = name.strip_suffix(&format!(".{REQUEST_SUFFIX}")) else {
                continue;
            };
            let record = fs::read(&path)
                .map_err(|error| format!("could not read {}: {error}", path.display()))
                .and_then(|bytes| decode_request(scope, &bytes));
            records.push((scope.to_owned(), record));
        }
        Ok(records)
    }

    /// Admits one request against the durable record for its scope.
    ///
    /// # Errors
    /// Fails closed on a corrupt record, a record bound to another identity,
    /// an unreadable journal and an uncertain write.
    pub fn admit(
        &self,
        scope: &str,
        principal: &str,
        program: ProgramId,
        request_digest: [u8; 32],
        now: u64,
    ) -> Result<Admission, JournalRefusal> {
        self.preserve_uncertain(scope, now)
            .map_err(JournalRefusal::Unavailable)?;
        let program = hex::encode(&program.bytes());
        let digest = hex::encode(&request_digest);
        if let Some(mut record) = self.read(scope)? {
            if record.principal != principal || record.program != program {
                return Err(JournalRefusal::Corrupt(format!(
                    "{scope}: record is bound to another principal or program"
                )));
            }
            if record.request_digest != digest {
                return Ok(Admission::Conflict);
            }
            return match record.state {
                VerificationState::Completed { response } => Ok(Admission::Replay(response)),
                VerificationState::Persisted { .. } => Ok(Admission::Publish(record)),
                VerificationState::Artifact { .. } => Ok(Admission::Artifact(record)),
                VerificationState::Building | VerificationState::Retryable => {
                    record.attempt = record
                        .attempt
                        .checked_add(1)
                        .ok_or_else(|| JournalRefusal::Corrupt("attempts exhausted".to_owned()))?;
                    self.settle(&mut record, VerificationState::Building, now)?;
                    Ok(Admission::Build(record))
                }
            };
        }
        if !self.retain_one().map_err(JournalRefusal::Unavailable)? {
            return Ok(Admission::QuotaExhausted);
        }
        let mut record = VerificationRecord {
            version: VERIFICATION_RECORD_VERSION,
            scope: scope.to_owned(),
            principal: principal.to_owned(),
            program,
            request_digest: digest,
            integrity_digest: String::new(),
            attempt: 1,
            state: VerificationState::Building,
            created_at: now,
            updated_at: now,
        };
        self.settle(&mut record, VerificationState::Building, now)?;
        Ok(Admission::Build(record))
    }

    pub(crate) fn pending_publications(&self) -> Result<Vec<VerificationRecord>, String> {
        let mut pending = Vec::new();
        for (_, record) in self.records()? {
            let record = record?;
            if matches!(record.state, VerificationState::Persisted { .. }) {
                pending.push(record);
            }
        }
        Ok(pending)
    }

    /// Makes room for one new identity by evicting the oldest settled one.
    /// Live, ambiguous and corrupt identities are never evicted.
    fn retain_one(&self) -> Result<bool, String> {
        let records = self.records()?;
        if records.len() < self.journal.capacity {
            return Ok(true);
        }
        let oldest = records
            .iter()
            .filter_map(|(scope, record)| match record {
                Ok(record) if record.state.settled() => Some((record.updated_at, scope)),
                _ => None,
            })
            .min();
        let Some((_, scope)) = oldest else {
            return Ok(false);
        };
        fs::remove_file(self.path(scope))
            .and_then(|()| fs::File::open(&self.journal.root)?.sync_all())
            .map_err(|error| format!("could not evict a settled request: {error}"))?;
        Ok(true)
    }

    /// Durably replaces the record with its next state.
    ///
    /// # Errors
    /// Returns `Unavailable` when the replacement is uncertain; the caller
    /// must not act on the new state.
    pub fn settle(
        &self,
        record: &mut VerificationRecord,
        state: VerificationState,
        now: u64,
    ) -> Result<(), JournalRefusal> {
        record.state = state;
        record.updated_at = now;
        record.integrity_digest = record_integrity(record).map_err(JournalRefusal::Unavailable)?;
        let bytes = serde_json::to_vec(record)
            .map_err(|error| JournalRefusal::Unavailable(error.to_string()))?;
        write_atomic(&self.path(&record.scope), &bytes).map_err(|error| {
            JournalRefusal::Unavailable(format!("verification request write is uncertain: {error}"))
        })
    }
}

fn file_name(path: &std::path::Path) -> String {
    path.file_name()
        .and_then(|name| name.to_str())
        .unwrap_or_default()
        .to_owned()
}

fn record_integrity(record: &VerificationRecord) -> Result<String, String> {
    let mut canonical = record.clone();
    canonical.integrity_digest.clear();
    let encoded = serde_json::to_vec(&canonical).map_err(|error| error.to_string())?;
    let mut digest = Sha256::new();
    digest.update(b"LayerX/platform/registry/verification-record/v2\0");
    digest.update(encoded);
    Ok(hex::encode(&digest.finalize()))
}

fn decode_request(scope: &str, bytes: &[u8]) -> Result<VerificationRecord, String> {
    let record: VerificationRecord =
        serde_json::from_slice(bytes).map_err(|error| format!("undecodable record: {error}"))?;
    if record.version != VERIFICATION_RECORD_VERSION {
        return Err(format!("unsupported record version {}", record.version));
    }
    if record.integrity_digest != record_integrity(&record)? {
        return Err("record integrity digest differs".to_owned());
    }
    if record.scope != scope {
        return Err("record scope does not match its name".to_owned());
    }
    let digests = [&record.scope, &record.program, &record.request_digest];
    if digests
        .iter()
        .any(|value| hex::decode_digest(value).is_err())
        || record.attempt == 0
    {
        return Err("record identity is malformed".to_owned());
    }
    if let VerificationState::Artifact { response, source } = &record.state {
        let verified = VerifiedSourceStore::decode_record(source)?;
        if hex::encode(&verified.program.bytes()) != record.program || !matches!(response.status, 200 | 409) {
            return Err("prepared artifact identity is invalid".to_owned());
        }
    }
    if let VerificationState::Artifact { response, .. }
    | VerificationState::Persisted { response, .. }
    | VerificationState::Completed { response } = &record.state
    {
        if !(100..=599).contains(&response.status) {
            return Err("record response status is invalid".to_owned());
        }
    }
    Ok(record)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::event_producer::ProgramOutbox;
    use layerx_platform_internal::producer::Outbox as _;

    const HOLDER: &str = "LAYERX_TEST_VERIFICATION_LEASE_HOLDER";

    fn directory(name: &str) -> PathBuf {
        let root = std::env::temp_dir().join(format!(
            "registry-verification-{name}-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        root
    }

    fn program(byte: u8) -> ProgramId {
        ProgramId::new([byte; 32]).unwrap_or_else(|error| panic!("{error}"))
    }

    fn scope(byte: u8) -> String {
        hex::encode(&[byte; 32])
    }

    fn deadline() -> Instant {
        Instant::now() + Duration::from_secs(5)
    }

    fn lease(journal: &VerificationJournal) -> VerificationLease {
        journal
            .lease(deadline())
            .unwrap_or_else(|refused| panic!("{refused:?}"))
    }

    fn ok(status: u16) -> Response {
        Response {
            status,
            body: format!("{{\"status\":{status}}}"),
        }
    }

    fn build(admission: Result<Admission, JournalRefusal>) -> VerificationRecord {
        match admission {
            Ok(Admission::Build(record)) => record,
            other => panic!("expected a build, got {other:?}"),
        }
    }

    #[test]
    fn durable_verification_replays_after_restart_and_refuses_changed_requests() {
        let root = directory("replay");
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        let mut record = build(held.admit(&scope(1), "alice", program(7), [1; 32], 10));
        assert_eq!(record.attempt, 1);
        held.settle(
            &mut record,
            VerificationState::Completed { response: ok(200) },
            11,
        )
        .unwrap_or_else(|e| panic!("{e:?}"));
        drop(held);
        drop(journal);
        let restarted = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let reconciled = restarted.reconcile(12).unwrap_or_else(|e| panic!("{e}"));
        assert_eq!((reconciled.live, reconciled.settled), (0, 1));
        let held = lease(&restarted);
        assert_eq!(
            held.admit(&scope(1), "alice", program(7), [1; 32], 13),
            Ok(Admission::Replay(ok(200)))
        );
        let before = fs::read(root.join(format!("{}.request", scope(1)))).unwrap_or_default();
        assert_eq!(
            held.admit(&scope(1), "alice", program(7), [2; 32], 14),
            Ok(Admission::Conflict)
        );
        assert_eq!(
            fs::read(root.join(format!("{}.request", scope(1)))).unwrap_or_default(),
            before
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_isolates_principals_and_refuses_foreign_records() {
        let root = directory("principals");
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        build(held.admit(&scope(1), "alice", program(7), [1; 32], 10));
        build(held.admit(&scope(2), "bob", program(7), [1; 32], 10));
        assert!(matches!(
            held.admit(&scope(1), "bob", program(7), [1; 32], 11),
            Err(JournalRefusal::Corrupt(_))
        ));
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_recovers_a_crashed_rebuild_as_a_new_attempt() {
        let root = directory("crash");
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        build(held.admit(&scope(3), "alice", program(7), [1; 32], 10));
        drop(held);
        let reconciled = journal.reconcile(11).unwrap_or_else(|e| panic!("{e}"));
        assert_eq!((reconciled.live, reconciled.settled), (1, 0));
        let held = lease(&journal);
        assert_eq!(
            held.admit(&scope(3), "alice", program(7), [9; 32], 12),
            Ok(Admission::Conflict)
        );
        let mut record = build(held.admit(&scope(3), "alice", program(7), [1; 32], 12));
        assert_eq!(record.attempt, 2);
        held.settle(&mut record, VerificationState::Retryable, 13)
            .unwrap_or_else(|e| panic!("{e:?}"));
        assert_eq!(
            build(held.admit(&scope(3), "alice", program(7), [1; 32], 14)).attempt,
            3
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_recovers_a_persisted_publication_without_duplicate_events() {
        let root = directory("persisted");
        let journal =
            VerificationJournal::open(root.join("requests")).unwrap_or_else(|e| panic!("{e}"));
        let outbox = ProgramOutbox::new(&root);
        let publication = Publication {
            body: json!({
                "program_id": "cd".repeat(32),
                "lifecycle": "active",
                "versions": [{
                    "version": 1,
                    "code_hash": "ee".repeat(32),
                    "deployment_receipt_digest": "ff".repeat(32),
                }],
            })
            .to_string(),
            principal: "ab".repeat(32),
            occurred_at: 20,
        };
        let held = lease(&journal);
        let mut record = build(held.admit(&scope(4), "alice", program(7), [1; 32], 20));
        held.settle(
            &mut record,
            VerificationState::Persisted {
                response: ok(200),
                publication: publication.clone(),
            },
            20,
        )
        .unwrap_or_else(|e| panic!("{e:?}"));
        outbox
            .enqueue_publication(
                &publication.body,
                &publication.principal,
                publication.occurred_at,
            )
            .unwrap_or_else(|e| panic!("{e}"));
        drop(held);
        let held = lease(&journal);
        let Ok(Admission::Publish(mut record)) =
            held.admit(&scope(4), "alice", program(7), [1; 32], 30)
        else {
            panic!("a persisted verification must recover its publication");
        };
        let VerificationState::Persisted {
            response,
            publication: recovered,
        } = record.state.clone()
        else {
            panic!("recovered state is not persisted");
        };
        assert_eq!(recovered, publication);
        outbox
            .enqueue_publication(&recovered.body, &recovered.principal, recovered.occurred_at)
            .unwrap_or_else(|e| panic!("{e}"));
        held.settle(&mut record, VerificationState::Completed { response }, 31)
            .unwrap_or_else(|e| panic!("{e:?}"));
        let pending = outbox
            .pending()
            .unwrap_or_else(|e| panic!("{e}"))
            .unwrap_or_else(|| panic!("publication missing"));
        assert_eq!(pending.observation.sequence, 1);
        assert_eq!(pending.observation.occurred_at, 20);
        outbox
            .acknowledge(&pending.observation.id, true)
            .unwrap_or_else(|e| panic!("{e}"));
        let observed = outbox
            .pending()
            .unwrap_or_else(|e| panic!("{e}"))
            .unwrap_or_else(|| panic!("observed publication missing"));
        assert_eq!(observed.observation.id, pending.observation.id);
        outbox
            .acknowledge(&pending.observation.id, false)
            .unwrap_or_else(|e| panic!("{e}"));
        assert!(outbox.pending().unwrap_or_else(|e| panic!("{e}")).is_none());
        outbox.enqueue_publication(&publication.body, &"bc".repeat(32), 40)
            .unwrap_or_else(|e| panic!("{e}"));
        let other = outbox.pending().unwrap_or_else(|e| panic!("{e}"))
            .unwrap_or_else(|| panic!("other principal publication missing"));
        assert_eq!(other.observation.sequence, 2);
        assert_eq!(other.observation.principal_digest, Some("bc".repeat(32)));
        outbox.acknowledge(&other.observation.id, true).unwrap_or_else(|e| panic!("{e}"));
        outbox.acknowledge(&other.observation.id, false).unwrap_or_else(|e| panic!("{e}"));
        outbox.enqueue_publication(&publication.body, &publication.principal, publication.occurred_at)
            .unwrap_or_else(|e| panic!("{e}"));
        assert!(outbox.pending().unwrap_or_else(|e| panic!("{e}")).is_none());

        let legacy = ProgramOutbox::new(&root.join("legacy"));
        legacy.enqueue(
            &format!("{}:{}", pending.observation.resource, pending.observation.source_sequence),
            pending.observation.clone(),
        ).unwrap_or_else(|e| panic!("{e}"));
        legacy.enqueue_publication(&publication.body, &publication.principal, publication.occurred_at)
            .unwrap_or_else(|e| panic!("{e}"));
        let legacy_pending = legacy.pending().unwrap_or_else(|e| panic!("{e}"))
            .unwrap_or_else(|| panic!("legacy publication missing"));
        assert_eq!(legacy_pending.observation.sequence, 1);
        legacy.acknowledge(&legacy_pending.observation.id, true).unwrap_or_else(|e| panic!("{e}"));
        legacy.acknowledge(&legacy_pending.observation.id, false).unwrap_or_else(|e| panic!("{e}"));
        assert!(legacy.pending().unwrap_or_else(|e| panic!("{e}")).is_none());
        assert_eq!(
            held.admit(&scope(4), "alice", program(7), [1; 32], 32),
            Ok(Admission::Replay(ok(200)))
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_recovers_artifact_before_publication() {
        let root = directory("artifact-recovery");
        let journal = VerificationJournal::with_capacity(root.join("requests"), 1).unwrap_or_else(|e| panic!("{e}"));
        let store = VerifiedSourceStore::open(root.join("verified")).unwrap_or_else(|e| panic!("{e}"));
        let source = VerifiedSource {
            program: program(7), version: 1,
            source_uri: "https://source.example/program".to_owned(),
            source_digest: [1; 32], artifact_digest: [2; 32],
            plan: BuildPlan {
                environment: layerx_programs::BuildEnvironment {
                    builder_image_digest: [3; 32], toolchain_digest: [4; 32],
                    dependency_lock_digest: [5; 32], source_date_epoch: 1,
                    command: vec!["/usr/bin/cc".to_owned(), "source.c".to_owned()],
                },
                artifact_path: "program.wasm".to_owned(),
                toolchain_manifest: "toolchain.lock".to_owned(),
                dependency_lock: "dependencies.lock".to_owned(),
            },
        };
        let held = lease(&journal);
        let mut record = build(held.admit(&scope(10), "alice", program(7), [1; 32], 10));
        held.settle(&mut record, VerificationState::Artifact {
            response: ok(200), source: VerifiedSourceStore::encode_record(&source),
        }, 11).unwrap_or_else(|e| panic!("{e:?}"));
        assert_eq!(held.admit(&scope(11), "alice", program(7), [1; 32], 11), Ok(Admission::QuotaExhausted));
        drop(held);
        assert!(store.records().unwrap_or_else(|e| panic!("{e}")).is_empty());
        for now in [12, 13] {
            let reopened = VerificationJournal::open(root.join("requests")).unwrap_or_else(|e| panic!("{e}"));
            assert_eq!(reopened.reconcile(now).unwrap_or_else(|e| panic!("{e}")).live, 1);
            let held = lease(&reopened);
            let Ok(Admission::Artifact(recovered)) = held.admit(&scope(10), "alice", program(7), [1; 32], now) else {
                panic!("artifact recovery must not rebuild");
            };
            assert_eq!(recovered.attempt, 1);
            let VerificationState::Artifact { response, source: encoded } = recovered.state else {
                panic!("artifact recovery phase missing");
            };
            assert_eq!(response, ok(200));
            let restored = VerifiedSourceStore::decode_record(&encoded).unwrap_or_else(|e| panic!("{e}"));
            assert_eq!(restored, source);
            store.record(&restored).unwrap_or_else(|e| panic!("{e}"));
            assert_eq!(store.records().unwrap_or_else(|e| panic!("{e}")), vec![source.clone()]);
        }
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_retention_preserves_live_identities() {
        let root = directory("retention");
        let journal =
            VerificationJournal::with_capacity(root.clone(), 2).unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        build(held.admit(&scope(1), "alice", program(7), [1; 32], 10));
        let mut settled = build(held.admit(&scope(2), "alice", program(7), [1; 32], 11));
        held.settle(
            &mut settled,
            VerificationState::Completed { response: ok(422) },
            12,
        )
        .unwrap_or_else(|e| panic!("{e:?}"));
        build(held.admit(&scope(3), "alice", program(7), [1; 32], 13));
        assert!(!root.join(format!("{}.request", scope(2))).exists());
        assert_eq!(
            held.admit(&scope(4), "alice", program(7), [1; 32], 14),
            Ok(Admission::QuotaExhausted)
        );
        assert_eq!(
            build(held.admit(&scope(1), "alice", program(7), [1; 32], 15)).attempt,
            2
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_fails_closed_on_corrupt_uncertain_and_unavailable_storage() {
        let root = directory("corrupt");
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let corrupt = root.join(format!("{}.request", scope(5)));
        fs::write(&corrupt, b"{\"version\":1,").unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        assert!(matches!(
            held.admit(&scope(5), "alice", program(7), [1; 32], 10),
            Err(JournalRefusal::Corrupt(_))
        ));
        assert_eq!(fs::read(&corrupt).unwrap_or_default(), b"{\"version\":1,");
        let mut future = build(held.admit(&scope(6), "alice", program(7), [1; 32], 10));
        future.version = VERIFICATION_RECORD_VERSION + 1;
        fs::write(
            root.join(format!("{}.request", scope(6))),
            serde_json::to_vec(&future).unwrap_or_default(),
        )
        .unwrap_or_else(|e| panic!("{e}"));
        assert!(matches!(
            held.admit(&scope(6), "alice", program(7), [1; 32], 11),
            Err(JournalRefusal::Corrupt(_))
        ));
        let mut committed = build(held.admit(&scope(8), "alice", program(7), [1; 32], 12));
        held.settle(
            &mut committed,
            VerificationState::Completed { response: ok(200) },
            12,
        )
        .unwrap_or_else(|e| panic!("{e:?}"));
        let terminal_path = root.join(format!("{}.request", scope(8)));
        let terminal_bytes = fs::read(&terminal_path).unwrap_or_else(|e| panic!("{e}"));
        let mut altered: Value = serde_json::from_slice(&terminal_bytes).unwrap_or_else(|e| panic!("{e}"));
        altered["state"]["response"]["body"] = Value::String("changed response".to_owned());
        fs::write(&terminal_path, altered.to_string()).unwrap_or_else(|e| panic!("{e}"));
        assert!(matches!(held.admit(&scope(8), "alice", program(7), [1; 32], 13), Err(JournalRefusal::Corrupt(_))));
        fs::write(&terminal_path, &terminal_bytes).unwrap_or_else(|e| panic!("{e}"));
        let interrupted = root.join(format!("{}.request.tmp", scope(8)));
        fs::write(&interrupted, b"partial").unwrap_or_else(|e| panic!("{e}"));
        drop(held);
        let reconciled = journal.reconcile(13).unwrap_or_else(|e| panic!("{e}"));
        assert_eq!(reconciled.preserved.len(), 1);
        assert_eq!(reconciled.defects.len(), 2);
        assert_eq!(
            fs::read(&reconciled.preserved[0]).unwrap_or_default(),
            b"partial"
        );
        assert!(!interrupted.exists());
        let held = lease(&journal);
        assert_eq!(
            held.admit(&scope(8), "alice", program(7), [1; 32], 14),
            Ok(Admission::Replay(ok(200)))
        );
        drop(held);
        let blocked = directory("unavailable");
        fs::write(&blocked, b"not a directory").unwrap_or_else(|e| panic!("{e}"));
        assert!(VerificationJournal::open(blocked.clone()).is_err());
        let unavailable = VerificationJournal {
            root: blocked.clone(),
            capacity: 1,
        };
        assert!(matches!(
            unavailable.lease(deadline()),
            Err(LeaseRefusal::Unavailable(_))
        ));
        let _ = fs::remove_file(blocked);
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn durable_verification_concurrent_worker_process_has_one_build_owner() {
        let root = directory("owner");
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let mut child =
            std::process::Command::new(std::env::current_exe().unwrap_or_else(|e| panic!("{e}")))
                .args([
                    "--exact",
                    "verified::tests::lease_holder_process",
                    "--nocapture",
                ])
                .env(HOLDER, &root)
                .stdout(std::process::Stdio::null())
                .spawn()
                .unwrap_or_else(|e| panic!("{e}"));
        let ready = root.join("holder.ready");
        let limit = deadline();
        while !ready.exists() {
            assert!(Instant::now() < limit, "lease holder did not start");
            std::thread::sleep(Duration::from_millis(5));
        }
        let short = Instant::now() + Duration::from_millis(100);
        assert!(matches!(journal.lease(short), Err(LeaseRefusal::Busy)));
        fs::write(root.join("holder.release"), b"").unwrap_or_else(|e| panic!("{e}"));
        assert!(child.wait().unwrap_or_else(|e| panic!("{e}")).success());
        let held = lease(&journal);
        assert_eq!(
            held.admit(&scope(9), "alice", program(7), [1; 32], 50),
            Ok(Admission::Replay(ok(200)))
        );
        let _ = fs::remove_dir_all(root);
    }

    /// Runs only as the second worker process spawned by the ownership test.
    #[test]
    fn lease_holder_process() {
        let Some(root) = std::env::var_os(HOLDER) else {
            return;
        };
        let root = PathBuf::from(root);
        let journal = VerificationJournal::open(root.clone()).unwrap_or_else(|e| panic!("{e}"));
        let held = lease(&journal);
        let mut record = build(held.admit(&scope(9), "alice", program(7), [1; 32], 40));
        fs::write(root.join("holder.ready"), b"").unwrap_or_else(|e| panic!("{e}"));
        let limit = Instant::now() + Duration::from_secs(30);
        while !root.join("holder.release").exists() {
            assert!(Instant::now() < limit, "release never arrived");
            std::thread::sleep(Duration::from_millis(5));
        }
        held.settle(
            &mut record,
            VerificationState::Completed { response: ok(200) },
            41,
        )
        .unwrap_or_else(|e| panic!("{e:?}"));
    }
}
