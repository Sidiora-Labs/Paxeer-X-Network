use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use serde_json::Value;
use zeroize::{Zeroize, Zeroizing};

use crate::http::{json, ok, refusal, Request, Response};
use crate::journal::Journal;
use crate::secret::{
    hex, sha256_hex, unhex, unix_seconds, valid_hex, valid_identifier, valid_principal,
};
use crate::tls::Upstream;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Journey,
    Approval,
    Payment,
    Program,
}

impl Kind {
    /// Parses the four source families.
    /// # Errors
    /// Refuses any undeclared family.
    pub fn parse(value: &str) -> Result<Self, String> {
        match value {
            "journeys" => Ok(Self::Journey),
            "approvals" => Ok(Self::Approval),
            "payments" => Ok(Self::Payment),
            "programs" => Ok(Self::Program),
            _ => Err("invalid source kind".to_owned()),
        }
    }
    #[must_use]
    pub const fn singular(self) -> &'static str {
        match self {
            Self::Journey => "journey",
            Self::Approval => "approval",
            Self::Payment => "payment",
            Self::Program => "program",
        }
    }
    fn route(self, resource: &str) -> String {
        let prefix = match self {
            Self::Journey => "/v1/journeys",
            Self::Approval => "/v1/approvals",
            Self::Payment => "/v1/receipts",
            Self::Program => "/v1/programs/registry",
        };
        format!("{prefix}/{resource}")
    }
    fn credential(self, value: &str) -> (&'static str, Zeroizing<String>) {
        if matches!(self, Self::Journey | Self::Approval) {
            (
                "Cookie",
                Zeroizing::new(format!("__Host-layerx_access={value}")),
            )
        } else {
            (
                "Authorization",
                Zeroizing::new(format!("LayerX-Key {value}")),
            )
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Fact {
    pub name: String,
    pub value: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Record {
    pub id: String,
    pub principal: String,
    pub subject: String,
    pub subject_sequence: u64,
    pub occurred_at: u64,
    pub facts: Vec<Fact>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub activity_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub amount: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub asset: Option<String>,
}

#[derive(Serialize, Deserialize)]
#[serde(untagged)]
enum Stored {
    Producer { record: Record, observation: String },
    Legacy(Record),
}

pub struct ProducerCredential {
    pub token: Zeroizing<String>,
    pub allow_principal_digest: bool,
}

struct Store {
    journal: Journal,
    records: BTreeMap<String, Record>,
    sequences: BTreeMap<(String, String), u64>,
    observations: BTreeMap<String, Vec<u8>>,
}
impl Store {
    fn open(directory: &Path) -> Result<Self, String> {
        let mut records = Vec::new();
        let journal = Journal::open::<Stored>(directory, |record| records.push(record))?;
        let mut store = Self {
            journal,
            records: BTreeMap::new(),
            sequences: BTreeMap::new(),
            observations: BTreeMap::new(),
        };
        for stored in records {
            let record = match stored {
                Stored::Legacy(record) => record,
                Stored::Producer {
                    record,
                    observation,
                } => {
                    let decoded: crate::producer::Observation = serde_json::from_str(&observation)
                        .map_err(|_| "invalid producer journal observation".to_owned())?;
                    decoded.validate()?;
                    if decoded.record(record.principal.clone()) != record
                        || decoded
                            .principal
                            .as_ref()
                            .is_some_and(|principal| principal != &record.principal)
                        || decoded
                            .principal_digest
                            .as_ref()
                            .is_some_and(|digest| principal_digest(&record.principal) != *digest)
                    {
                        return Err("producer journal observation mismatch".to_owned());
                    }
                    store
                        .observations
                        .insert(record.id.clone(), observation.into_bytes());
                    record
                }
            };
            if !valid_hex(&record.id, 32)
                || !valid_principal(&record.principal)
                || !valid_identifier(&record.subject, 128)
                || record.facts.len() > 32
                || record.subject_sequence != store.next(&record)?
                || store.records.contains_key(&record.id)
            {
                return Err("invalid event journal ordering or identity".to_owned());
            }
            store.index(record);
        }
        Ok(store)
    }
    fn next(&self, record: &Record) -> Result<u64, String> {
        self.sequences
            .get(&(record.principal.clone(), record.subject.clone()))
            .copied()
            .unwrap_or(0)
            .checked_add(1)
            .ok_or_else(|| "event sequence exhausted".to_owned())
    }
    fn index(&mut self, record: Record) {
        self.sequences.insert(
            (record.principal.clone(), record.subject.clone()),
            record.subject_sequence,
        );
        self.records.insert(record.id.clone(), record);
    }
    fn append_produced(&mut self, record: Record, body: &[u8]) -> Result<Record, u16> {
        if let Some(previous) = self.records.get(&record.id) {
            return if self
                .observations
                .get(&record.id)
                .is_some_and(|bytes| bytes == body)
            {
                Ok(previous.clone())
            } else {
                Err(409)
            };
        }
        if record.subject_sequence != self.next(&record).map_err(|_| 503_u16)? {
            return Err(409);
        }
        self.journal
            .append(&Stored::Producer {
                record: record.clone(),
                observation: std::str::from_utf8(body).map_err(|_| 400_u16)?.to_owned(),
            })
            .map_err(|_| 503_u16)?;
        self.observations.insert(record.id.clone(), body.to_vec());
        self.index(record.clone());
        Ok(record)
    }
    fn append(&mut self, mut record: Record) -> Result<Record, String> {
        if let Some(previous) = self.records.get(&record.id) {
            return Ok(previous.clone());
        }
        record.subject_sequence = self.next(&record)?;
        self.journal.append(&record)?;
        self.index(record.clone());
        Ok(record)
    }
}

/// Interval between reads of the enrollment snapshot.
pub const PRINCIPAL_POLL: Duration = Duration::from_secs(3);
/// Version of the enrollment snapshot format.
pub const ENROLLMENT_VERSION: u64 = 1;
/// File under the state directory that holds the adopted enrollment state.
pub const ENROLLMENT_STATE_FILE: &str = "enrollment.json";
const MAX_ENROLLMENT_BYTES: usize = 1_048_576;
const MAX_PRINCIPALS: usize = 10_000;
const MAX_FINGERPRINTS: usize = 100_000;
const MAX_STATE_BYTES: usize = 33_554_432;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SnapshotFile {
    version: u64,
    generation: u64,
    principals: Vec<SnapshotEntry>,
    mac: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SnapshotEntry {
    principal: String,
    credential_file: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct Enrolled {
    since: u64,
    fingerprint: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct EnrollmentState {
    generation: u64,
    principals: BTreeMap<String, Enrolled>,
    owners: BTreeMap<String, String>,
    #[serde(default)]
    issuers: BTreeMap<String, IssuerBinding>,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
struct IssuerBinding {
    tenant: String,
    revision: u64,
    session_id: String,
    fingerprint: String,
    expires_at: u64,
    complete: bool,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct IssuedEnrollment {
    principal: String,
    tenant: String,
    revision: u64,
    session_id: String,
    credential: String,
    expires_at: u64,
}

impl Drop for IssuedEnrollment {
    fn drop(&mut self) {
        self.credential.zeroize();
    }
}

impl EnrollmentState {
    fn valid(&self) -> bool {
        self.principals.len() <= MAX_PRINCIPALS
            && self.issuers.len() <= MAX_PRINCIPALS
            && self.issuers.iter().all(|(principal, binding)| {
                valid_principal(principal)
                    && valid_identifier(&binding.tenant, 256)
                    && binding.revision > 0
                    && valid_identifier(&binding.session_id, 256)
                    && binding.expires_at > 0
                    && valid_hex(&binding.fingerprint, 32)
            })
            && self.owners.len() <= MAX_FINGERPRINTS
            && self.principals.iter().all(|(principal, enrolled)| {
                valid_principal(principal)
                    && enrolled.since <= self.generation
                    && self.owners.get(&enrolled.fingerprint) == Some(principal)
            })
            && self.owners.iter().all(|(fingerprint, principal)| {
                valid_hex(fingerprint, 32) && valid_principal(principal)
            })
    }
}

/// One adopted enrollment generation. Binding, readiness, observation and
/// event reads of one request all use the generation captured at its start.
pub struct Generation {
    number: u64,
    adopted: bool,
    credentials: BTreeMap<String, Zeroizing<String>>,
}

impl Generation {
    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.number
    }
    #[must_use]
    pub fn principals(&self) -> usize {
        self.credentials.len()
    }
}

/// A fully validated and authenticated snapshot that has not been adopted yet.
pub struct Candidate {
    prepared_from: EnrollmentState,
    generation: u64,
    credentials: BTreeMap<String, Zeroizing<String>>,
    fingerprints: BTreeMap<String, String>,
    changed: Vec<String>,
}

fn protected_open(path: &Path) -> std::io::Result<File> {
    let owner = fs::metadata("/proc/self")?.uid();
    let before = fs::symlink_metadata(path)?;
    if !path.is_absolute()
        || !before.is_file()
        || before.uid() != owner
        || before.mode() & 0o077 != 0
        || before.nlink() != 1
    {
        return Err(ErrorKind::PermissionDenied.into());
    }
    let mut options = OpenOptions::new();
    options.read(true);
    #[cfg(target_os = "linux")]
    options.custom_flags(0x20000 | 0x800);
    let file = options.open(path)?;
    let metadata = file.metadata()?;
    if !metadata.is_file()
        || metadata.uid() != owner
        || metadata.mode() & 0o077 != 0
        || metadata.nlink() != 1
        || metadata.dev() != before.dev()
        || metadata.ino() != before.ino()
    {
        return Err(ErrorKind::PermissionDenied.into());
    }
    Ok(file)
}

/// Requires an owner-only regular file belonging to the effective user.
/// # Errors
/// Refuses links, foreign ownership and group or other access.
pub fn require_protected(path: &Path) -> Result<(), String> {
    protected_open(path)
        .map(|_| ())
        .map_err(|_| "protected file required".to_owned())
}

fn read_protected_secret(path: &Path) -> Result<Zeroizing<String>, &'static str> {
    let file = protected_open(path).map_err(|error| {
        if error.kind() == ErrorKind::PermissionDenied {
            "enrollment_unprotected"
        } else {
            "enrollment_credential_unreadable"
        }
    })?;
    let mut value = Zeroizing::new(String::new());
    file.take(crate::secret::MAX_SECRET_BYTES as u64 + 3)
        .read_to_string(&mut value)
        .map_err(|_| "enrollment_credential_unreadable")?;
    if value.len() > crate::secret::MAX_SECRET_BYTES + 2 {
        return Err("enrollment_credential_unreadable");
    }
    while matches!(value.as_bytes().last(), Some(b'\n' | b'\r')) {
        value.pop();
    }
    if value.is_empty()
        || value.len() > crate::secret::MAX_SECRET_BYTES
        || value.bytes().any(|byte| byte.is_ascii_control())
    {
        return Err("enrollment_credential_unreadable");
    }
    Ok(value)
}

/// Reads the enrollment key through its checked descriptor.
/// # Errors
/// Refuses an unprotected, unreadable or short key.
pub fn enrollment_key(path: &Path) -> Result<Zeroizing<String>, String> {
    let key = read_protected_secret(path).map_err(str::to_owned)?;
    if key.len() < crate::secret::MIN_TOKEN_BYTES {
        return Err("enrollment key is too short".to_owned());
    }
    Ok(key)
}

/// Signs an empty generation zero without exposing the key in process arguments.
#[must_use]
pub fn empty_enrollment_mac(kind: Kind, key: &str) -> String {
    enrollment_snapshot_mac(kind, 0, &[], key)
}

/// Signs the ordered principal credentials of an enrollment snapshot.
#[must_use]
pub fn enrollment_snapshot_mac(
    kind: Kind,
    generation: u64,
    entries: &[(&str, &str)],
    key: &str,
) -> String {
    let mut message = format!("layerx-enrollment-v1\n{}s\n{generation}\n", kind.singular());
    for (principal, credential) in entries {
        message.push_str(principal);
        message.push('\n');
        message.push_str(&sha256_hex(credential.as_bytes()));
        message.push('\n');
    }
    let key = ring::hmac::Key::new(ring::hmac::HMAC_SHA256, key.as_bytes());
    hex(ring::hmac::sign(&key, message.as_bytes()).as_ref())
}

/// The versioned principal enrollment of one source: the adopted generation,
/// its persisted state and the authenticated snapshot it refreshes from.
pub struct Enrollments {
    kind: Kind,
    snapshot: PathBuf,
    state_path: PathBuf,
    key: ring::hmac::Key,
    state: Mutex<EnrollmentState>,
    current: RwLock<Arc<Generation>>,
    last_refusal: Mutex<Option<&'static str>>,
}

impl Enrollments {
    /// Opens the enrollment of a source, replaying the persisted generation.
    /// Nothing is enrolled until a snapshot is adopted.
    /// # Errors
    /// Refuses an unreadable or invalid persisted enrollment state.
    pub fn open(kind: Kind, snapshot: &Path, key: &str, directory: &Path) -> Result<Self, String> {
        fs::create_dir_all(directory).map_err(|error| format!("state directory: {error}"))?;
        let state_path = directory.join(ENROLLMENT_STATE_FILE);
        let state = match protected_open(&state_path) {
            Ok(file) => {
                let mut bytes = Vec::new();
                file.take(MAX_STATE_BYTES as u64 + 1)
                    .read_to_end(&mut bytes)
                    .map_err(|error| error.to_string())?;
                serde_json::from_slice(&bytes)
                    .ok()
                    .filter(|state: &EnrollmentState| {
                        bytes.len() <= MAX_STATE_BYTES && state.valid()
                    })
                    .ok_or_else(|| "invalid persisted enrollment state".to_owned())?
            }
            Err(error) if error.kind() == ErrorKind::NotFound => EnrollmentState::default(),
            Err(error) => return Err(error.to_string()),
        };
        let current = Generation {
            number: state.generation,
            adopted: false,
            credentials: BTreeMap::new(),
        };
        Ok(Self {
            kind,
            snapshot: snapshot.to_path_buf(),
            state_path,
            key: ring::hmac::Key::new(ring::hmac::HMAC_SHA256, key.as_bytes()),
            state: Mutex::new(state),
            current: RwLock::new(Arc::new(current)),
            last_refusal: Mutex::new(None),
        })
    }

    /// The adopted generation, captured once per request.
    #[must_use]
    pub fn current(&self) -> Arc<Generation> {
        self.current.read().map_or_else(
            |_| {
                Arc::new(Generation {
                    number: 0,
                    adopted: false,
                    credentials: BTreeMap::new(),
                })
            },
            |current| Arc::clone(&current),
        )
    }

    /// The code of the most recent refused snapshot, cleared on adoption.
    #[must_use]
    pub fn last_refusal(&self) -> Option<&'static str> {
        self.last_refusal.lock().ok().and_then(|refusal| *refusal)
    }

    fn fingerprint(&self, credential: &str) -> String {
        let mut message = Zeroizing::new(b"layerx-credential-v1\n".to_vec());
        message.extend_from_slice(credential.as_bytes());
        hex(ring::hmac::sign(&self.key, &message).as_ref())
    }

    /// Reads, bounds, authenticates and validates the snapshot against the
    /// adopted state without changing anything.
    /// # Errors
    /// Returns the refusal code of the first violated rule.
    pub fn prepare(&self) -> Result<Option<Candidate>, &'static str> {
        self.prepare_path(&self.snapshot)
    }

    fn prepare_path(&self, snapshot: &Path) -> Result<Option<Candidate>, &'static str> {
        let file = match protected_open(snapshot) {
            Ok(file) => file,
            Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
            Err(error) if error.kind() == ErrorKind::PermissionDenied => {
                return Err("enrollment_unprotected")
            }
            Err(_) => return Err("enrollment_malformed"),
        };
        let mut bytes = Zeroizing::new(Vec::new());
        file.take(MAX_ENROLLMENT_BYTES as u64 + 1)
            .read_to_end(&mut bytes)
            .map_err(|_| "enrollment_malformed")?;
        if bytes.len() > MAX_ENROLLMENT_BYTES {
            return Err("enrollment_oversized");
        }
        let file: SnapshotFile =
            serde_json::from_slice(&bytes).map_err(|_| "enrollment_malformed")?;
        if file.version != ENROLLMENT_VERSION {
            return Err("enrollment_malformed");
        }
        if file.principals.len() > MAX_PRINCIPALS {
            return Err("enrollment_oversized");
        }
        let mac = unhex(&file.mac)
            .filter(|mac| mac.len() == 32)
            .ok_or("enrollment_malformed")?;
        let mut message = format!(
            "layerx-enrollment-v1\n{}s\n{}\n",
            self.kind.singular(),
            file.generation
        );
        let mut credentials = BTreeMap::new();
        let mut fingerprints = BTreeMap::new();
        let mut owners = BTreeMap::new();
        for entry in &file.principals {
            if !valid_principal(&entry.principal) {
                return Err("enrollment_malformed");
            }
            if credentials.contains_key(&entry.principal) {
                return Err("enrollment_duplicate");
            }
            let path = Path::new(&entry.credential_file);
            let credential = read_protected_secret(path)?;
            let fingerprint = self.fingerprint(&credential);
            if owners
                .insert(fingerprint.clone(), entry.principal.clone())
                .is_some()
            {
                return Err("enrollment_credential_reused");
            }
            message.push_str(&entry.principal);
            message.push('\n');
            message.push_str(&sha256_hex(credential.as_bytes()));
            message.push('\n');
            fingerprints.insert(entry.principal.clone(), fingerprint);
            credentials.insert(entry.principal.clone(), credential);
        }
        ring::hmac::verify(&self.key, message.as_bytes(), &mac)
            .map_err(|_| "enrollment_unauthenticated")?;
        let state = self.state.lock().map_err(|_| "enrollment_unavailable")?;
        let adopted: BTreeMap<&String, &String> = state
            .principals
            .iter()
            .map(|(principal, enrolled)| (principal, &enrolled.fingerprint))
            .collect();
        let proposed: BTreeMap<&String, &String> = fingerprints.iter().collect();
        if file.generation < state.generation {
            return Err("enrollment_stale_generation");
        }
        if file.generation == state.generation && adopted != proposed {
            return Err("enrollment_generation_conflict");
        }
        if owners.iter().any(|(fingerprint, principal)| {
            state
                .owners
                .get(fingerprint)
                .is_some_and(|owner| owner != principal)
        }) {
            return Err("enrollment_credential_reused");
        }
        let replay = !self
            .current
            .read()
            .map_err(|_| "enrollment_unavailable")?
            .adopted;
        let changed = fingerprints
            .iter()
            .filter(|(principal, fingerprint)| {
                replay || adopted.get(principal) != Some(fingerprint)
            })
            .map(|(principal, _)| principal.clone())
            .collect();
        Ok(Some(Candidate {
            prepared_from: state.clone(),
            generation: file.generation,
            credentials,
            fingerprints,
            changed,
        }))
    }

    /// Persists and atomically installs a candidate whose changed principals
    /// were verified upstream. Credentials of removed or rotated principals
    /// lose authority the moment the new generation is installed.
    /// Returns the generation when it differs from the one served before.
    /// # Errors
    /// Refuses a candidate that no longer follows the adopted state, an
    /// exhausted fingerprint history or an unwritable state file.
    pub fn adopt(&self, candidate: Candidate) -> Result<Option<u64>, &'static str> {
        let mut state = self.state.lock().map_err(|_| "enrollment_unavailable")?;
        if candidate.generation < state.generation {
            return Err("enrollment_stale_generation");
        }
        if candidate.generation == state.generation
            && (candidate.fingerprints.len() != state.principals.len()
                || candidate
                    .fingerprints
                    .iter()
                    .any(|(principal, fingerprint)| {
                        state
                            .principals
                            .get(principal)
                            .map(|enrolled| &enrolled.fingerprint)
                            != Some(fingerprint)
                    }))
        {
            return Err("enrollment_generation_conflict");
        }
        let mut current = self.current.write().map_err(|_| "enrollment_unavailable")?;
        if current.adopted && candidate.generation == state.generation {
            if let Ok(mut refusal) = self.last_refusal.lock() {
                *refusal = None;
            }
            return Ok(None);
        }
        if candidate.prepared_from != *state {
            return Err("enrollment_generation_conflict");
        }
        let mut next = EnrollmentState {
            generation: candidate.generation,
            principals: BTreeMap::new(),
            owners: state.owners.clone(),
            issuers: state.issuers.clone(),
        };
        for (principal, fingerprint) in candidate.fingerprints {
            let since = state
                .principals
                .get(&principal)
                .filter(|enrolled| enrolled.fingerprint == fingerprint)
                .map_or(candidate.generation, |enrolled| enrolled.since);
            if next
                .owners
                .insert(fingerprint.clone(), principal.clone())
                .is_some_and(|owner| owner != principal)
            {
                return Err("enrollment_credential_reused");
            }
            next.principals
                .insert(principal, Enrolled { since, fingerprint });
        }
        if next.owners.len() > MAX_FINGERPRINTS {
            return Err("enrollment_oversized");
        }
        if next != *state {
            persist(&self.state_path, &next).map_err(|_| "enrollment_unavailable")?;
            *state = next;
        }
        let changed = !current.adopted || current.number != candidate.generation;
        *current = Arc::new(Generation {
            number: candidate.generation,
            adopted: true,
            credentials: candidate.credentials,
        });
        drop(current);
        if let Ok(mut refusal) = self.last_refusal.lock() {
            *refusal = None;
        }
        Ok(changed.then_some(candidate.generation))
    }

    fn refused(&self, code: &'static str) -> bool {
        self.last_refusal.lock().is_ok_and(|mut refusal| {
            let fresh = *refusal != Some(code);
            *refusal = Some(code);
            fresh
        })
    }
}

impl Candidate {
    /// Principals whose credential is new or changed in this snapshot.
    #[must_use]
    pub fn changed(&self) -> &[String] {
        &self.changed
    }
}

fn persist<T: Serialize>(path: &Path, state: &T) -> Result<(), String> {
    let bytes =
        Zeroizing::new(serde_json::to_vec(state).map_err(|_| "enrollment encoding".to_owned())?);
    persist_bytes(path, &bytes)
}

fn persist_bytes(path: &Path, bytes: &[u8]) -> Result<(), String> {
    if bytes.len() > MAX_STATE_BYTES {
        return Err("enrollment state exceeds bound".to_owned());
    }
    let directory = path.parent().ok_or("enrollment directory missing")?;
    let metadata =
        fs::symlink_metadata(directory).map_err(|_| "enrollment directory unavailable")?;
    if !path.is_absolute()
        || !metadata.is_dir()
        || metadata.mode() & 0o022 != 0
        || metadata.uid()
            != fs::metadata("/proc/self")
                .map_err(|_| "enrollment owner unavailable")?
                .uid()
        || fs::canonicalize(directory).map_err(|_| "enrollment directory unavailable")? != directory
    {
        return Err("enrollment directory unprotected".to_owned());
    }
    match protected_open(path) {
        Ok(_) => {}
        Err(error) if error.kind() == ErrorKind::NotFound => {}
        Err(_) => return Err("enrollment target unprotected".to_owned()),
    }
    let suffix = crate::secret::random_hex(16)?;
    let temporary = path.with_extension(format!("json.{}.new", suffix.as_str()));
    let mut file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .mode(0o600)
        .open(&temporary)
        .map_err(|error| error.to_string())?;
    let result = (|| {
        file.write_all(bytes)
            .and_then(|()| file.sync_all())
            .map_err(|error| error.to_string())?;
        drop(file);
        fs::rename(&temporary, path).map_err(|error| error.to_string())?;
        if let Some(directory) = path.parent() {
            File::open(directory)
                .and_then(|directory| directory.sync_all())
                .map_err(|error| error.to_string())?;
        }
        Ok(())
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

/// Answers a source that is still waiting for its principal set: alive, not
/// ready, and refusing every other route until the set arrives.
#[must_use]
pub fn waiting_principals(request: &Request) -> Response {
    if request.method == "GET" && request.path == "/livez" {
        return ok("{\"alive\":true}".to_owned());
    }
    if request.method == "GET" && request.path == "/readyz" {
        return json(
            503,
            &serde_json::json!({"ready":false,"state":"waiting-principals"}),
        );
    }
    refusal(503, "waiting_principals", Some(PRINCIPAL_POLL.as_secs()))
}

pub struct Service {
    kind: Kind,
    upstream: Upstream,
    enrollments: Enrollments,
    token: Zeroizing<String>,
    producers: Vec<ProducerCredential>,
    store: Mutex<Store>,
    enrollment_writer: Mutex<()>,
    bootstrap: Option<PathBuf>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Observe {
    principal: String,
    resource: String,
}

impl Service {
    fn enroll_issued(&self, request: &Request) -> Response {
        if !matches!(self.kind, Kind::Journey | Kind::Approval)
            || !request.peer_verified
            || !self.producers.iter().any(|producer| {
                !producer.allow_principal_digest && request.bearer_matches(&producer.token)
            })
        {
            return refusal(401, "unauthorized", None);
        }
        let Ok(issued) = serde_json::from_slice::<IssuedEnrollment>(&request.body) else {
            return refusal(400, "enrollment_malformed", None);
        };
        let result = self.install_issued(&issued);
        match result {
            Ok(generation) => json(
                200,
                &serde_json::json!({"principal":issued.principal,
                "tenant":issued.tenant,"revision":issued.revision,"generation":generation,"bound":true}),
            ),
            Err((status, code)) => refusal(status, code, (status == 503).then_some(3)),
        }
    }

    fn install_issued(&self, issued: &IssuedEnrollment) -> Result<u64, (u16, &'static str)> {
        let _writer = self
            .enrollment_writer
            .lock()
            .map_err(|_| (503, "enrollment_unavailable"))?;
        match self.refresh_locked() {
            Ok(_) => {}
            Err(code @ ("enrollment_upstream_unavailable" | "enrollment_principal_mismatch")) => {
                self.enrollments.refused(code);
            }
            Err(code) => {
                self.enrollments.refused(code);
                return Err((503, code));
            }
        }
        let now = unix_seconds().map_err(|_| (503, "enrollment_unavailable"))?;
        if !valid_principal(&issued.principal)
            || !valid_identifier(&issued.tenant, 256)
            || !valid_identifier(&issued.session_id, 256)
            || issued.revision == 0
            || issued.expires_at <= now
            || issued.credential.is_empty()
            || issued.credential.len() > crate::secret::MAX_SECRET_BYTES
            || issued
                .credential
                .bytes()
                .any(|byte| byte.is_ascii_control() || byte == b';')
        {
            return Err((400, "enrollment_malformed"));
        }
        let identity = self
            .fetch_with(&issued.credential, "/internal/v1/principal")
            .map_err(|_| (503, "enrollment_upstream_unavailable"))?;
        if !principal_matches(self.kind, &identity, &issued.principal)
            || identity.get("tenant_id").and_then(Value::as_str) != Some(issued.tenant.as_str())
            || identity.get("session_id").and_then(Value::as_str)
                != Some(issued.session_id.as_str())
        {
            return Err((403, "enrollment_principal_mismatch"));
        }
        let fingerprint = self.enrollments.fingerprint(&issued.credential);
        let current = self.enrollments.current();
        let mut proposed = IssuerBinding {
            tenant: issued.tenant.clone(),
            revision: issued.revision,
            session_id: issued.session_id.clone(),
            fingerprint,
            expires_at: issued.expires_at,
            complete: false,
        };
        {
            let mut state = self
                .enrollments
                .state
                .lock()
                .map_err(|_| (503, "enrollment_unavailable"))?;
            if let Some(previous) = state.issuers.get(&issued.principal) {
                if previous.tenant != proposed.tenant || previous.revision > proposed.revision {
                    return Err((409, "enrollment_issuer_conflict"));
                }
                if previous.revision == proposed.revision {
                    proposed.complete = previous.complete;
                    if previous != &proposed {
                        return Err((409, "enrollment_issuer_conflict"));
                    }
                    if previous.complete
                        && current.adopted
                        && !current.credentials.contains_key(&issued.principal)
                    {
                        return Err((409, "enrollment_removed"));
                    }
                    if previous.complete
                        && current.adopted
                        && current
                            .credentials
                            .get(&issued.principal)
                            .is_some_and(|value| value.as_str() == issued.credential)
                    {
                        return Ok(current.number);
                    }
                }
            }
            if state
                .owners
                .get(&proposed.fingerprint)
                .is_some_and(|owner| owner != &issued.principal)
            {
                return Err((409, "enrollment_credential_reused"));
            }
            let mut next = state.clone();
            next.issuers
                .insert(issued.principal.clone(), proposed.clone());
            if !next.valid() {
                return Err((413, "enrollment_oversized"));
            }
            persist(&self.enrollments.state_path, &next)
                .map_err(|_| (503, "enrollment_unavailable"))?;
            *state = next;
        }
        let mut credentials = if current.adopted {
            current.credentials.clone()
        } else {
            self.enrollments
                .prepare()
                .map_err(|code| (503, code))?
                .ok_or((503, "enrollment_unavailable"))?
                .credentials
        };
        let unchanged = credentials
            .get(&issued.principal)
            .is_some_and(|value| value.as_str() == issued.credential);
        credentials.insert(
            issued.principal.clone(),
            Zeroizing::new(issued.credential.clone()),
        );
        if !unchanged || !current.adopted {
            self.write_issued_snapshot(&credentials)?;
            self.refresh_locked().map_err(|code| (503, code))?;
        }
        let adopted = self.enrollments.current();
        if !adopted.adopted
            || adopted
                .credentials
                .get(&issued.principal)
                .is_none_or(|value| value.as_str() != issued.credential)
        {
            return Err((503, "enrollment_not_adopted"));
        }
        let mut state = self
            .enrollments
            .state
            .lock()
            .map_err(|_| (503, "enrollment_unavailable"))?;
        let mut next = state.clone();
        let binding = next
            .issuers
            .get_mut(&issued.principal)
            .ok_or((503, "enrollment_unavailable"))?;
        if binding.revision != issued.revision {
            return Err((409, "enrollment_issuer_conflict"));
        }
        binding.complete = true;
        persist(&self.enrollments.state_path, &next)
            .map_err(|_| (503, "enrollment_unavailable"))?;
        *state = next;
        Ok(adopted.number)
    }

    fn write_issued_snapshot(
        &self,
        credentials: &BTreeMap<String, Zeroizing<String>>,
    ) -> Result<(), (u16, &'static str)> {
        let state = self
            .enrollments
            .state
            .lock()
            .map_err(|_| (503, "enrollment_unavailable"))?;
        let generation = state
            .generation
            .checked_add(1)
            .ok_or((503, "enrollment_generation_exhausted"))?;
        drop(state);
        self.write_snapshot(credentials, generation)
    }

    fn write_snapshot(
        &self,
        credentials: &BTreeMap<String, Zeroizing<String>>,
        generation: u64,
    ) -> Result<(), (u16, &'static str)> {
        if credentials.len() > MAX_PRINCIPALS {
            return Err((413, "enrollment_oversized"));
        }
        let directory = self
            .enrollments
            .state_path
            .parent()
            .ok_or((503, "enrollment_unavailable"))?;
        let mut principals = Vec::new();
        let mut message = format!(
            "layerx-enrollment-v1\n{}s\n{generation}\n",
            self.kind.singular()
        );
        for (principal, credential) in credentials {
            let file = directory.join(format!(
                "credential-{}",
                self.enrollments.fingerprint(credential)
            ));
            persist_bytes(&file, credential.as_bytes())
                .map_err(|_| (503, "enrollment_unavailable"))?;
            principals.push(serde_json::json!({"principal":principal,"credential_file":file}));
            message.push_str(principal);
            message.push('\n');
            message.push_str(&sha256_hex(credential.as_bytes()));
            message.push('\n');
        }
        let mac = hex(ring::hmac::sign(&self.enrollments.key, message.as_bytes()).as_ref());
        let bytes = Zeroizing::new(
            serde_json::to_vec(&serde_json::json!({"version":ENROLLMENT_VERSION,
            "generation":generation,"principals":principals,"mac":mac}))
            .map_err(|_| (503, "enrollment_unavailable"))?,
        );
        if bytes.len() > MAX_ENROLLMENT_BYTES {
            return Err((413, "enrollment_oversized"));
        }
        persist_bytes(&self.enrollments.snapshot, &bytes)
            .map_err(|_| (503, "enrollment_unavailable"))
    }
    /// Opens a durable source whose principals come from the authenticated
    /// enrollment snapshot at `snapshot`; nothing is enrolled until
    /// [`Service::refresh`] adopts one.
    /// # Errors
    /// Refuses an invalid journal or persisted enrollment state.
    pub fn open(
        kind: Kind,
        upstream: Upstream,
        snapshot: &Path,
        enrollment_key: &str,
        token: Zeroizing<String>,
        directory: &Path,
    ) -> Result<Self, String> {
        let store = Store::open(directory)?;
        Ok(Self {
            kind,
            upstream,
            enrollments: Enrollments::open(kind, snapshot, enrollment_key, directory)?,
            token,
            producers: Vec::new(),
            store: Mutex::new(store),
            enrollment_writer: Mutex::new(()),
            bootstrap: std::env::var_os("LAYERX_EVENTS_BOOTSTRAP_CREDENTIALS_FILE")
                .map(PathBuf::from),
        })
    }
    /// Re-reads the enrollment snapshot. A newer authenticated snapshot whose
    /// new or rotated credentials all bind upstream replaces the served
    /// generation atomically; any refusal leaves the last valid one intact.
    /// Returns the adopted generation when it changed, and on refusal its code
    /// together with whether it differs from the previous refusal.
    /// # Errors
    /// Returns the refusal code; it never names credential contents.
    pub fn refresh(&self) -> Result<Option<u64>, (&'static str, bool)> {
        let result = self
            .enrollment_writer
            .lock()
            .map_err(|_| "enrollment_unavailable")
            .and_then(|_writer| self.refresh_locked());
        result.map_err(|code| (code, self.enrollments.refused(code)))
    }

    fn refresh_locked(&self) -> Result<Option<u64>, &'static str> {
        if let Some(bootstrap) = &self.bootstrap {
            match self.enrollments.prepare_path(bootstrap) {
                Ok(Some(candidate)) => {
                    let current = self.enrollments.current();
                    if candidate.generation > current.number || !self.enrollments.snapshot.exists()
                    {
                        for principal in candidate.changed() {
                            let credential = candidate
                                .credentials
                                .get(principal)
                                .ok_or("enrollment_unavailable")?;
                            if !self.bind_with(principal, credential)? {
                                return Err("enrollment_principal_mismatch");
                            }
                        }
                        self.write_snapshot(&candidate.credentials, candidate.generation)
                            .map_err(|(_, code)| code)?;
                    }
                }
                Err("enrollment_stale_generation") => {}
                Err(code) => return Err(code),
                Ok(None) => return Err("enrollment_bootstrap_missing"),
            }
        }
        self.enrollments.prepare().and_then(|candidate| {
            let Some(candidate) = candidate else {
                return Ok(None);
            };
            for principal in candidate.changed() {
                let credential = candidate
                    .credentials
                    .get(principal)
                    .ok_or("enrollment_unavailable")?;
                if !self.bind_with(principal, credential)? {
                    return Err("enrollment_principal_mismatch");
                }
            }
            self.enrollments.adopt(candidate)
        })
    }
    /// The served enrollment generation.
    #[must_use]
    pub fn generation(&self) -> Arc<Generation> {
        self.enrollments.current()
    }
    /// # Errors
    /// Refuses duplicate credentials and digest authority outside the payment source.
    pub fn with_producers(mut self, producers: Vec<ProducerCredential>) -> Result<Self, String> {
        if producers.len() > 3
            || producers.iter().enumerate().any(|(index, credential)| {
                credential.token.is_empty()
                    || credential.token.as_str() == self.token.as_str()
                    || (credential.allow_principal_digest
                        && !matches!(self.kind, Kind::Payment | Kind::Program))
                    || producers[..index]
                        .iter()
                        .any(|other| other.token == credential.token)
            })
        {
            return Err("invalid producer credentials".to_owned());
        }
        self.producers = producers;
        Ok(self)
    }

    fn observe_produced(
        &self,
        enrolled: &Generation,
        body: &[u8],
        credential: &ProducerCredential,
    ) -> Result<Record, u16> {
        let observation: crate::producer::Observation =
            serde_json::from_slice(body).map_err(|_| 400_u16)?;
        observation.validate().map_err(|_| 400_u16)?;
        if body.len() > crate::producer::MAX_OBSERVATION_BYTES {
            return Err(400);
        }
        if observation.kind != self.kind.singular() {
            return Err(403);
        }
        let principal = match (&observation.principal, &observation.principal_digest) {
            (Some(principal), None) => principal.clone(),
            (None, Some(digest)) if credential.allow_principal_digest => {
                let mut matches = enrolled
                    .credentials
                    .keys()
                    .filter(|principal| principal_digest(principal) == *digest);
                let principal = matches.next().ok_or(403_u16)?.clone();
                if matches.next().is_some() {
                    return Err(403);
                }
                principal
            }
            _ => return Err(403),
        };
        self.bind(enrolled, &principal).map_err(|_| 403_u16)?;
        let record = observation.record(principal);
        self.store
            .lock()
            .map_err(|_| 503_u16)?
            .append_produced(record, body)
    }

    fn fetch_with(&self, credential: &str, path: &str) -> Result<Value, &'static str> {
        self.fetch_with_deadline(credential, path, None)
    }

    fn fetch_with_deadline(
        &self,
        credential: &str,
        path: &str,
        deadline: Option<Instant>,
    ) -> Result<Value, &'static str> {
        let (header, value) = self.kind.credential(credential);
        let response = match deadline {
            Some(deadline) => self.upstream.get_as_until(path, header, &value, deadline),
            None => self.upstream.get_as(path, header, &value),
        }
        .map_err(|_| "enrollment_upstream_unavailable")?;
        if response.status != 200 || !response.content_type.starts_with("application/json") {
            return Err("upstream refused request");
        }
        let envelope: Value =
            serde_json::from_slice(&response.body).map_err(|_| "invalid upstream JSON")?;
        if envelope.get("ok") != Some(&Value::Bool(true)) {
            return Err("upstream outcome unavailable");
        }
        envelope
            .get("result")
            .cloned()
            .ok_or("upstream result missing")
    }
    /// Verifies upstream that `credential` is the credential of `principal`.
    /// Ok(false) is an explicit identity mismatch or refusal; Err is an
    /// unreachable upstream.
    fn bind_with(&self, principal: &str, credential: &str) -> Result<bool, &'static str> {
        self.bind_with_deadline(principal, credential, None)
    }

    fn bind_with_deadline(
        &self,
        principal: &str,
        credential: &str,
        deadline: Option<Instant>,
    ) -> Result<bool, &'static str> {
        match self.fetch_with_deadline(credential, "/internal/v1/principal", deadline) {
            Ok(identity) => {
                let state = match deadline {
                    Some(_) => self
                        .enrollments
                        .state
                        .try_lock()
                        .map_err(|_| "enrollment_unavailable")?,
                    None => self
                        .enrollments
                        .state
                        .lock()
                        .map_err(|_| "enrollment_unavailable")?,
                };
                Ok(principal_matches(self.kind, &identity, principal)
                    && state.issuers.get(principal).is_none_or(|binding| {
                        identity.get("tenant_id").and_then(Value::as_str)
                            == Some(binding.tenant.as_str())
                    }))
            }
            Err("enrollment_upstream_unavailable") => Err("enrollment_upstream_unavailable"),
            Err(_) => Ok(false),
        }
    }
    fn fetch(&self, enrolled: &Generation, principal: &str, path: &str) -> Result<Value, String> {
        let credential = enrolled
            .credentials
            .get(principal)
            .ok_or_else(|| "unknown principal".to_owned())?;
        self.fetch_with(credential, path).map_err(str::to_owned)
    }
    fn bind(&self, enrolled: &Generation, principal: &str) -> Result<(), String> {
        let credential = enrolled
            .credentials
            .get(principal)
            .ok_or_else(|| "unknown principal".to_owned())?;
        if self.bind_with(principal, credential)? {
            Ok(())
        } else {
            Err("credential principal mismatch".to_owned())
        }
    }
    fn ready(&self, enrolled: &Generation, deadline: Instant) -> bool {
        self.upstream
            .get_until("/readyz", deadline)
            .is_ok_and(|response| response.status == 200)
            && enrolled.credentials.iter().all(|(principal, credential)| {
                self.bind_with_deadline(principal, credential, Some(deadline)) == Ok(true)
            })
            && self
                .store
                .try_lock()
                .is_ok_and(|store| store.journal.probe_writable().is_ok())
            && Instant::now() < deadline
    }
    fn observe(&self, enrolled: &Generation, body: &[u8]) -> Result<Record, String> {
        let request: Observe =
            serde_json::from_slice(body).map_err(|_| "invalid observation".to_owned())?;
        if !valid_identifier(&request.resource, 128) {
            return Err("invalid resource".to_owned());
        }
        self.bind(enrolled, &request.principal)?;
        let snapshot = self.fetch(
            enrolled,
            &request.principal,
            &self.kind.route(&request.resource),
        )?;
        let record = derive(self.kind, &request.principal, &request.resource, &snapshot)?;
        self.store
            .lock()
            .map_err(|_| "event store unavailable".to_owned())?
            .append(record)
    }
    /// Routes authenticated observations and immutable event reads.
    #[must_use]
    pub fn route(&self, request: &Request) -> Response {
        if request.method == "GET" && request.path == "/livez" {
            return ok("{\"alive\":true}".to_owned());
        }
        if request.method == "POST" && request.path == "/internal/v1/enrollments" {
            return self.enroll_issued(request);
        }
        let admission_route = request.method == "GET"
            && matches!(
                request.path.as_str(),
                "/internal/v1/producer-readiness" | "/internal/v1/reader-readiness"
            );
        let readiness_route =
            admission_route || (request.method == "GET" && request.path == "/readyz");
        let enrolled = if readiness_route {
            match self.enrollments.current.try_read() {
                Ok(current) => Arc::clone(&current),
                Err(_) => return refusal(503, "enrollment_unavailable", None),
            }
        } else {
            self.enrollments.current()
        };
        let last_refusal = if readiness_route {
            self.enrollments
                .last_refusal
                .try_lock()
                .ok()
                .and_then(|refusal| *refusal)
        } else {
            self.enrollments.last_refusal()
        };
        if admission_route {
            if !request.peer_verified || !request.body.is_empty() {
                return refusal(401, "unauthorized", None);
            }
            let producer = request.path == "/internal/v1/producer-readiness";
            let credential = self
                .producers
                .iter()
                .find(|credential| request.bearer_matches(&credential.token));
            if (producer && credential.is_none())
                || (!producer && !request.bearer_matches(&self.token))
            {
                return refusal(401, "unauthorized", None);
            }
            let deadline =
                match crate::tls::admission_deadline(&request.headers, Duration::from_secs(5)) {
                    Ok(deadline) => deadline,
                    Err(_) => return refusal(503, "admission_deadline_exceeded", None),
                };
            let ready = enrolled.adopted
                && !enrolled.credentials.is_empty()
                && enrolled.credentials.iter().all(|(principal, credential)| {
                    self.bind_with_deadline(principal, credential, Some(deadline)) == Ok(true)
                })
                && self
                    .store
                    .try_lock()
                    .is_ok_and(|store| store.journal.probe_writable().is_ok())
                && Instant::now() < deadline;
            return json(
                if ready { 200 } else { 503 },
                &serde_json::json!({
                    "schema": "layerx.event-admission.v1",
                    "role": if producer { "source-producer" } else { "source-reader" },
                    "kind": self.kind.singular(), "ready": ready,
                    "generation": enrolled.number, "principals": enrolled.credentials.len(),
                    "principal_digest": credential.is_some_and(|value| value.allow_principal_digest),
                    "fresh_for_ms": 10_000
                }),
            );
        }
        if request.method == "GET" {
            if let Some(principal) = request
                .path
                .strip_prefix("/internal/v1/principals/")
                .and_then(|path| path.strip_suffix("/issued-enrollment"))
                .filter(|principal| valid_principal(principal))
            {
                if !request.peer_verified
                    || !self
                        .producers
                        .iter()
                        .any(|credential| request.bearer_matches(&credential.token))
                {
                    return refusal(401, "unauthorized", None);
                }
                let issuer = self
                    .enrollments
                    .state
                    .lock()
                    .ok()
                    .and_then(|state| state.issuers.get(principal).cloned());
                let bound = issuer.as_ref().is_some_and(|binding| {
                    binding.complete
                        && enrolled
                            .credentials
                            .get(principal)
                            .is_some_and(|credential| {
                                self.enrollments.fingerprint(credential) == binding.fingerprint
                            })
                }) && enrolled.adopted
                    && self.bind(&enrolled, principal).is_ok();
                return json(
                    200,
                    &serde_json::json!({"principal":principal,
                    "generation":enrolled.number,
                    "bound":bound,
                    "tenant":issuer.as_ref().map(|binding| binding.tenant.as_str()),
                    "session_id":issuer.as_ref().map(|binding| binding.session_id.as_str()),
                    "revision":issuer.as_ref().map(|binding| binding.revision)}),
                );
            }
            if let Some(principal) = request
                .path
                .strip_prefix("/internal/v1/principals/")
                .and_then(|path| path.strip_suffix("/enrollment"))
                .filter(|principal| valid_principal(principal))
            {
                if !request.peer_verified
                    || !self
                        .producers
                        .iter()
                        .any(|credential| request.bearer_matches(&credential.token))
                {
                    return refusal(401, "unauthorized", None);
                }
                let bound = enrolled.adopted && self.bind(&enrolled, principal).is_ok();
                return json(
                    200,
                    &serde_json::json!({"principal":principal,
                    "generation":enrolled.number, "bound":bound}),
                );
            }
        }
        if enrolled.credentials.is_empty() {
            if request.method == "GET" && request.path == "/readyz" {
                return json(
                    503,
                    &serde_json::json!({"ready":false,"state":"waiting-principals","generation":enrolled.number,"principals":0,"last_refusal":last_refusal}),
                );
            }
            return waiting_principals(request);
        }
        if request.method == "GET" && request.path == "/readyz" {
            let ready = crate::tls::admission_deadline(&request.headers, Duration::from_secs(5))
                .is_ok_and(|deadline| self.ready(&enrolled, deadline));
            return json(
                if ready { 200 } else { 503 },
                &serde_json::json!({"ready":ready,"generation":enrolled.number,"principals":enrolled.credentials.len(),"last_refusal":last_refusal}),
            );
        }
        if request.method == "POST"
            && request.path == "/internal/v1/observe"
            && request.json_body()
            && request.peer_verified
        {
            if let Some(credential) = self
                .producers
                .iter()
                .find(|credential| request.bearer_matches(&credential.token))
            {
                return self
                    .observe_produced(&enrolled, &request.body, credential)
                    .map_or_else(
                        |status| {
                            refusal(status, "observation_refused", (status == 503).then_some(5))
                        },
                        |record| json(200, &record),
                    );
            }
        }
        if !request.peer_verified || !request.bearer_matches(&self.token) {
            return refusal(401, "unauthorized", None);
        }
        if request.method == "POST" && request.path == "/internal/v1/observe" && request.json_body()
        {
            return self.observe(&enrolled, &request.body).map_or_else(
                |_| refusal(503, "source_unavailable", Some(5)),
                |record| json(200, &record),
            );
        }
        if request.method == "GET" {
            if let Some(id) = request
                .path
                .strip_prefix("/internal/v1/events/")
                .filter(|id| valid_hex(id, 32))
            {
                let record = self
                    .store
                    .lock()
                    .ok()
                    .and_then(|store| store.records.get(id).cloned());
                return record
                    .filter(|record| enrolled.credentials.contains_key(&record.principal))
                    .map_or_else(
                        || refusal(404, "event_not_found", None),
                        |record| {
                            if self.bind(&enrolled, &record.principal).is_err() {
                                refusal(503, "source_unavailable", Some(5))
                            } else {
                                json(200, &record)
                            }
                        },
                    );
            }
        }
        refusal(404, "not_found", None)
    }
}

fn principal_matches(kind: Kind, identity: &Value, principal: &str) -> bool {
    if matches!(kind, Kind::Journey | Kind::Approval) {
        identity.get("active") == Some(&Value::Bool(true))
            && identity.get("sub").and_then(Value::as_str) == Some(principal)
    } else {
        identity.get("principal_digest").and_then(Value::as_str)
            == Some(principal_digest(principal).as_str())
    }
}

fn principal_digest(principal: &str) -> String {
    sha256_hex(principal.as_bytes())
}

fn derive(kind: Kind, principal: &str, resource: &str, snapshot: &Value) -> Result<Record, String> {
    let committed =
        serde_json::to_vec(&(principal, resource, snapshot)).map_err(|error| error.to_string())?;
    let mut record = Record {
        id: sha256_hex(&committed),
        principal: principal.to_owned(),
        subject: resource.to_owned(),
        subject_sequence: 0,
        occurred_at: unix_seconds()?,
        facts: Vec::new(),
        activity_id: None,
        amount: None,
        asset: None,
    };
    let (identity, fields): (&str, &[&str]) = match kind {
        Kind::Journey => ("journey_id", &["kind", "state", "updated_at"]),
        Kind::Approval => ("approval_id", &["agent_id", "state", "created_at"]),
        Kind::Program => (
            "program_id",
            &["lifecycle", "version", "code_hash", "receipt_digest"],
        ),
        Kind::Payment => ("activity_id", &[]),
    };
    if snapshot.get(identity).and_then(Value::as_str) != Some(resource) {
        return Err("source identity mismatch".to_owned());
    }
    for field in fields {
        let value = snapshot
            .get(*field)
            .ok_or_else(|| "source field missing".to_owned())?;
        record.facts.push(Fact {
            name: (*field).to_owned(),
            value: value
                .as_str()
                .map_or_else(|| value.to_string(), str::to_owned),
        });
    }
    if matches!(kind, Kind::Payment) {
        let bytes = snapshot
            .get("receipt")
            .and_then(Value::as_str)
            .and_then(unhex)
            .ok_or_else(|| "receipt missing".to_owned())?;
        let receipt =
            layerx_wire::receipt::decode(&bytes).map_err(|_| "receipt malformed".to_owned())?;
        let receipt = receipt
            .protocol()
            .ok_or_else(|| "protocol receipt required".to_owned())?;
        if hex(&receipt.activity_id()) != resource {
            return Err("receipt identity mismatch".to_owned());
        }
        record.activity_id = Some(resource.to_owned());
        record.amount = Some(receipt.amount().to_string());
        record.asset = Some(hex(&receipt.asset()));
        record.occurred_at = receipt.timestamp();
    }
    Ok(record)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn producer_observation(sequence: u64) -> crate::producer::Observation {
        crate::producer::Observation {
            kind: "journey".to_owned(),
            id: crate::producer::event_id("journey", "journey-one", sequence),
            principal: Some("principal-one".to_owned()),
            principal_digest: None,
            resource: "journey-one".to_owned(),
            sequence,
            source_sequence: 17,
            occurred_at: 123,
            facts: vec![Fact {
                name: "state".to_owned(),
                value: "processing".to_owned(),
            }],
            activity_id: None,
            amount: None,
            asset: None,
        }
    }

    #[test]
    fn producer_retries_require_identical_bytes_after_restart() {
        let directory =
            std::env::temp_dir().join(format!("layerx-produced-events-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&directory);
        let first = producer_observation(1);
        let bytes = first.encode().unwrap_or_else(|error| panic!("{error}"));
        let record = first.record("principal-one".to_owned());
        let mut store = Store::open(&directory).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(
            store.append_produced(record.clone(), &bytes),
            Ok(record.clone())
        );
        assert_eq!(
            store.append_produced(record.clone(), &bytes),
            Ok(record.clone())
        );
        assert_eq!(store.journal.len(), 1);
        let mut changed = record.clone();
        changed.facts[0].value = "refused".to_owned();
        let mut changed_body = first.clone();
        changed_body.facts[0].value = "refused".to_owned();
        assert_eq!(
            store.append_produced(
                changed,
                &changed_body
                    .encode()
                    .unwrap_or_else(|error| panic!("{error}"))
            ),
            Err(409)
        );
        let spaced = serde_json::to_vec_pretty(&first).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(store.append_produced(record.clone(), &spaced), Err(409));
        let third = producer_observation(3);
        assert_eq!(
            store.append_produced(
                third.record("principal-one".to_owned()),
                &third.encode().unwrap_or_else(|error| panic!("{error}"))
            ),
            Err(409)
        );
        drop(store);
        let mut store = Store::open(&directory).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(store.append_produced(record.clone(), &bytes), Ok(record));
        assert_eq!(store.journal.len(), 1);
        let second = producer_observation(2);
        assert!(store
            .append_produced(
                second.record("principal-one".to_owned()),
                &second.encode().unwrap_or_else(|error| panic!("{error}"))
            )
            .is_ok());
        assert_eq!(store.journal.len(), 2);
        drop(store);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }

    #[test]
    fn source_credentials_cannot_be_reassigned_to_a_foreign_principal() {
        let human = serde_json::json!({"active":true,"sub":"principal-one"});
        assert!(principal_matches(Kind::Journey, &human, "principal-one"));
        assert!(!principal_matches(Kind::Approval, &human, "principal-two"));
        assert!(!principal_matches(
            Kind::Journey,
            &serde_json::json!({"active":false,"sub":"principal-one"}),
            "principal-one"
        ));
        let gateway = serde_json::json!({"principal_digest": principal_digest("principal-one")});
        assert!(principal_matches(Kind::Payment, &gateway, "principal-one"));
        assert!(!principal_matches(Kind::Program, &gateway, "principal-two"));
        assert!(!principal_matches(
            Kind::Payment,
            &serde_json::json!({}),
            "principal-one"
        ));
    }

    #[test]
    fn journal_preserves_immutable_event_order_and_deduplicates_observations() {
        let directory = std::env::temp_dir().join(format!("layerx-events-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&directory);
        let first = derive(Kind::Journey, "principal-one", "journey-one", &serde_json::json!({"journey_id":"journey-one","kind":"move","state":"processing","updated_at":123})).unwrap_or_else(|error| panic!("{error}"));
        let mut store = Store::open(&directory).unwrap_or_else(|error| panic!("{error}"));
        let accepted = store
            .append(first.clone())
            .unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(accepted.subject_sequence, 1);
        assert_eq!(
            store
                .append(first)
                .unwrap_or_else(|error| panic!("{error}"))
                .subject_sequence,
            1
        );
        let second = derive(Kind::Journey, "principal-one", "journey-one", &serde_json::json!({"journey_id":"journey-one","kind":"move","state":"refused","updated_at":124})).unwrap_or_else(|error| panic!("{error}"));
        assert_ne!(accepted.id, second.id);
        assert_eq!(
            store
                .append(second)
                .unwrap_or_else(|error| panic!("{error}"))
                .subject_sequence,
            2
        );
        drop(store);
        let store = Store::open(&directory).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(store.records.len(), 2);
        assert_eq!(store.records[&accepted.id].subject_sequence, 1);
        assert!(derive(
            Kind::Journey,
            "principal-one",
            "foreign",
            &serde_json::json!({"journey_id":"journey-one"})
        )
        .is_err());
        assert!(derive(
            Kind::Payment,
            "principal-one",
            "a",
            &serde_json::json!({"activity_id":"a","receipt":"00"})
        )
        .is_err());
        drop(store);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }

    const KEY: &str = "0123456789abcdef0123456789abcdef";

    fn owner_only(path: &Path, contents: &str) {
        use std::os::unix::fs::PermissionsExt;
        std::fs::write(path, contents).unwrap_or_else(|error| panic!("{error}"));
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))
            .unwrap_or_else(|error| panic!("{error}"));
    }

    fn sign(kind: Kind, generation: u64, entries: &[(&str, &str)]) -> String {
        let mut message = format!("layerx-enrollment-v1\n{}s\n{generation}\n", kind.singular());
        for (principal, credential) in entries {
            message.push_str(principal);
            message.push('\n');
            message.push_str(&sha256_hex(credential.as_bytes()));
            message.push('\n');
        }
        let key = ring::hmac::Key::new(ring::hmac::HMAC_SHA256, KEY.as_bytes());
        hex(ring::hmac::sign(&key, message.as_bytes()).as_ref())
    }

    /// Writes an owner-only snapshot naming one owner-only credential file per
    /// principal, signed with `mac` or the enrollment key.
    fn snapshot(
        directory: &Path,
        kind: Kind,
        generation: u64,
        entries: &[(&str, &str)],
        mac: Option<String>,
    ) {
        let mut principals = Vec::new();
        for (principal, credential) in entries {
            let path = directory.join(format!("{principal}-{generation}.credential"));
            owner_only(&path, &format!("{credential}\n"));
            principals.push(serde_json::json!({
                "principal": principal,
                "credential_file": path.display().to_string(),
            }));
        }
        let body = serde_json::json!({
            "version": ENROLLMENT_VERSION,
            "generation": generation,
            "principals": principals,
            "mac": mac.unwrap_or_else(|| sign(kind, generation, entries)),
        });
        owner_only(
            &directory.join("enrollment-snapshot.json"),
            &body.to_string(),
        );
    }

    fn scratch(name: &str) -> PathBuf {
        let directory =
            std::env::temp_dir().join(format!("layerx-enrollment-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&directory);
        std::fs::create_dir_all(&directory).unwrap_or_else(|error| panic!("{error}"));
        directory
    }

    fn open(kind: Kind, directory: &Path) -> Enrollments {
        Enrollments::open(
            kind,
            &directory.join("enrollment-snapshot.json"),
            KEY,
            &directory.join("state"),
        )
        .unwrap_or_else(|error| panic!("{error}"))
    }

    fn refresh(enrollments: &Enrollments) -> Result<Option<u64>, &'static str> {
        enrollments
            .prepare()
            .and_then(|candidate| {
                candidate.map_or(Ok(None), |candidate| enrollments.adopt(candidate))
            })
            .inspect_err(|code| {
                enrollments.refused(code);
            })
    }

    fn credential(enrollments: &Enrollments, principal: &str) -> Option<String> {
        enrollments
            .current()
            .credentials
            .get(principal)
            .map(|credential| credential.as_str().to_owned())
    }

    #[test]
    fn empty_enrollment_is_live_but_waits_for_principals() {
        let directory = scratch("empty");
        let enrollments = open(Kind::Journey, &directory);
        assert_eq!(refresh(&enrollments), Ok(None));
        assert!(!enrollments.current().adopted);
        snapshot(&directory, Kind::Journey, 0, &[], None);
        assert_eq!(refresh(&enrollments), Ok(Some(0)));
        assert_eq!(enrollments.current().principals(), 0);
        assert_eq!(refresh(&enrollments), Ok(None));
        let request = Request {
            method: "GET".to_owned(),
            path: "/readyz".to_owned(),
            headers: BTreeMap::new(),
            body: Vec::new(),
            peer_verified: true,
        };
        assert_eq!(waiting_principals(&request).status, 503);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }

    #[test]
    fn every_source_role_rotates_one_principal_while_another_keeps_its_credential() {
        for kind in [Kind::Journey, Kind::Approval, Kind::Payment, Kind::Program] {
            let directory = scratch(kind.singular());
            let enrollments = open(kind, &directory);
            snapshot(
                &directory,
                kind,
                1,
                &[
                    ("principal-one", "credential-one-a"),
                    ("principal-two", "credential-two"),
                ],
                None,
            );
            let candidate = enrollments
                .prepare()
                .unwrap_or_else(|code| panic!("{code}"))
                .unwrap_or_else(|| panic!("snapshot absent"));
            assert_eq!(candidate.changed(), ["principal-one", "principal-two"]);
            assert_eq!(enrollments.adopt(candidate), Ok(Some(1)));
            let before = enrollments.current();
            snapshot(
                &directory,
                kind,
                2,
                &[
                    ("principal-one", "credential-one-b"),
                    ("principal-two", "credential-two"),
                ],
                None,
            );
            let candidate = enrollments
                .prepare()
                .unwrap_or_else(|code| panic!("{code}"))
                .unwrap_or_else(|| panic!("snapshot absent"));
            assert_eq!(candidate.changed(), ["principal-one"]);
            assert_eq!(enrollments.adopt(candidate), Ok(Some(2)));
            assert_eq!(before.generation(), 1);
            assert_eq!(
                before.credentials["principal-one"].as_str(),
                "credential-one-a"
            );
            assert_eq!(
                credential(&enrollments, "principal-one").as_deref(),
                Some("credential-one-b")
            );
            assert_eq!(
                credential(&enrollments, "principal-two").as_deref(),
                Some("credential-two")
            );
            let state = enrollments
                .state
                .lock()
                .unwrap_or_else(|error| panic!("{error}"))
                .clone();
            assert_eq!(state.principals["principal-one"].since, 2);
            assert_eq!(state.principals["principal-two"].since, 1);
            let persisted =
                std::fs::read_to_string(directory.join("state").join(ENROLLMENT_STATE_FILE))
                    .unwrap_or_else(|error| panic!("{error}"));
            assert!(!persisted.contains("credential-one"));
            assert!(!persisted.contains("credential-two"));
            std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
        }
    }

    fn adopted_generation_three(name: &str) -> (PathBuf, Enrollments) {
        let directory = scratch(name);
        let enrollments = open(Kind::Payment, &directory);
        snapshot(
            &directory,
            Kind::Payment,
            3,
            &[("principal-one", "credential-one")],
            None,
        );
        assert_eq!(refresh(&enrollments), Ok(Some(3)));
        (directory, enrollments)
    }

    fn unchanged(enrollments: &Enrollments) {
        assert_eq!(enrollments.current().generation(), 3);
        assert_eq!(
            credential(enrollments, "principal-one").as_deref(),
            Some("credential-one")
        );
    }

    #[test]
    fn refused_snapshots_leave_the_last_valid_generation_intact() {
        let (directory, enrollments) = adopted_generation_three("refusals");
        let path = directory.join("enrollment-snapshot.json");
        owner_only(&path, "{\"version\":1,");
        assert_eq!(refresh(&enrollments), Err("enrollment_malformed"));
        unchanged(&enrollments);
        owner_only(&path, "{}");
        assert_eq!(refresh(&enrollments), Err("enrollment_malformed"));
        owner_only(&path, &" ".repeat(MAX_ENROLLMENT_BYTES + 1));
        assert_eq!(refresh(&enrollments), Err("enrollment_oversized"));
        unchanged(&enrollments);
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-one", "credential-one-b")],
            Some("00".repeat(32)),
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_unauthenticated"));
        unchanged(&enrollments);
        snapshot(
            &directory,
            Kind::Journey,
            4,
            &[("principal-one", "credential-one-b")],
            Some(sign(
                Kind::Journey,
                4,
                &[("principal-one", "credential-one-b")],
            )),
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_unauthenticated"));
        let duplicate = serde_json::json!({
            "version": 1,
            "generation": 4,
            "principals": [
                {"principal": "principal-one", "credential_file": directory.join("principal-one-4.credential").display().to_string()},
                {"principal": "principal-one", "credential_file": directory.join("principal-one-4.credential").display().to_string()},
            ],
            "mac": sign(Kind::Payment, 4, &[("principal-one", "credential-one-b"), ("principal-one", "credential-one-b")]),
        });
        owner_only(&path, &duplicate.to_string());
        assert_eq!(refresh(&enrollments), Err("enrollment_duplicate"));
        unchanged(&enrollments);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }

    #[test]
    fn credential_reuse_and_generation_rules_leave_the_last_valid_generation_intact() {
        let (directory, enrollments) = adopted_generation_three("generations");
        let path = directory.join("enrollment-snapshot.json");
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-one", "shared"), ("principal-two", "shared")],
            None,
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_credential_reused"));
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-two", "credential-one")],
            None,
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_credential_reused"));
        snapshot(
            &directory,
            Kind::Payment,
            2,
            &[("principal-one", "credential-one")],
            None,
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_stale_generation"));
        snapshot(
            &directory,
            Kind::Payment,
            3,
            &[("principal-one", "credential-one-b")],
            None,
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_generation_conflict"));
        unchanged(&enrollments);
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-one", "credential-one-b")],
            None,
        );
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o644))
                .unwrap_or_else(|error| panic!("{error}"));
        }
        assert_eq!(refresh(&enrollments), Err("enrollment_unprotected"));
        assert_eq!(enrollments.last_refusal(), Some("enrollment_unprotected"));
        assert!(!enrollments.refused("enrollment_unprotected"));
        unchanged(&enrollments);
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }

    #[test]
    fn removal_revokes_authority_and_generations_replay_after_restart() {
        let directory = scratch("restart");
        let enrollments = open(Kind::Program, &directory);
        snapshot(
            &directory,
            Kind::Program,
            1,
            &[
                ("principal-one", "credential-one"),
                ("principal-two", "credential-two"),
            ],
            None,
        );
        assert_eq!(refresh(&enrollments), Ok(Some(1)));
        snapshot(
            &directory,
            Kind::Program,
            2,
            &[("principal-two", "credential-two")],
            None,
        );
        assert_eq!(refresh(&enrollments), Ok(Some(2)));
        assert_eq!(credential(&enrollments, "principal-one"), None);
        drop(enrollments);
        let enrollments = open(Kind::Program, &directory);
        assert!(!enrollments.current().adopted);
        assert_eq!(enrollments.current().generation(), 2);
        assert_eq!(refresh(&enrollments), Ok(Some(2)));
        assert_eq!(
            credential(&enrollments, "principal-two").as_deref(),
            Some("credential-two")
        );
        assert_eq!(credential(&enrollments, "principal-one"), None);
        snapshot(
            &directory,
            Kind::Program,
            3,
            &[("principal-two", "credential-one")],
            None,
        );
        assert_eq!(refresh(&enrollments), Err("enrollment_credential_reused"));
        drop(enrollments);
        snapshot(
            &directory,
            Kind::Program,
            1,
            &[
                ("principal-one", "credential-one"),
                ("principal-two", "credential-two"),
            ],
            None,
        );
        let enrollments = open(Kind::Program, &directory);
        assert_eq!(refresh(&enrollments), Err("enrollment_stale_generation"));
        assert_eq!(enrollments.current().principals(), 0);
        std::fs::write(directory.join("state").join(ENROLLMENT_STATE_FILE), "{")
            .unwrap_or_else(|error| panic!("{error}"));
        assert!(Enrollments::open(
            Kind::Program,
            &directory.join("enrollment-snapshot.json"),
            KEY,
            &directory.join("state")
        )
        .is_err());
        std::fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error}"));
    }
    #[test]
    fn conflicting_prepared_candidates_cannot_replace_an_adopted_generation() {
        let (directory, enrollments) = adopted_generation_three("prepared-conflict");
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-one", "credential-one-a")],
            None,
        );
        let first = enrollments
            .prepare()
            .unwrap_or_else(|error| panic!("{error:?}"))
            .unwrap_or_else(|| panic!("snapshot absent"));
        let identical = enrollments
            .prepare()
            .unwrap_or_else(|error| panic!("{error:?}"))
            .unwrap_or_else(|| panic!("snapshot absent"));
        snapshot(
            &directory,
            Kind::Payment,
            4,
            &[("principal-one", "credential-one-b")],
            None,
        );
        let second = enrollments
            .prepare()
            .unwrap_or_else(|error| panic!("{error:?}"))
            .unwrap_or_else(|| panic!("snapshot absent"));
        assert_eq!(enrollments.adopt(first), Ok(Some(4)));
        assert_eq!(enrollments.adopt(identical), Ok(None));
        let persisted = fs::read(directory.join("state").join(ENROLLMENT_STATE_FILE))
            .unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(
            enrollments.adopt(second),
            Err("enrollment_generation_conflict")
        );
        assert_eq!(
            credential(&enrollments, "principal-one").as_deref(),
            Some("credential-one-a")
        );
        assert_eq!(
            fs::read(directory.join("state").join(ENROLLMENT_STATE_FILE))
                .unwrap_or_else(|error| panic!("{error:?}")),
            persisted
        );
        fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error:?}"));
    }

    #[test]
    fn restart_revalidates_every_persisted_principal_before_adoption() {
        for kind in [Kind::Journey, Kind::Approval, Kind::Payment, Kind::Program] {
            let directory = scratch(&format!("revalidate-{}", kind.singular()));
            let enrollments = open(kind, &directory);
            snapshot(
                &directory,
                kind,
                1,
                &[
                    ("principal-one", "credential-one"),
                    ("principal-two", "credential-two"),
                ],
                None,
            );
            assert_eq!(refresh(&enrollments), Ok(Some(1)));
            drop(enrollments);
            let enrollments = open(kind, &directory);
            let replay = enrollments
                .prepare()
                .unwrap_or_else(|error| panic!("{error:?}"))
                .unwrap_or_else(|| panic!("snapshot absent"));
            assert_eq!(replay.changed(), ["principal-one", "principal-two"]);
            assert!(!enrollments.current().adopted);
            assert_eq!(enrollments.current().principals(), 0);
            assert_eq!(enrollments.adopt(replay), Ok(Some(1)));
            assert!(enrollments
                .prepare()
                .unwrap_or_else(|error| panic!("{error:?}"))
                .unwrap_or_else(|| panic!("snapshot absent"))
                .changed()
                .is_empty());
            fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error:?}"));
        }
    }

    #[test]
    fn protected_reads_refuse_links_and_preserve_the_opened_file_identity() {
        let directory = scratch("protected-reader");
        let path = directory.join("credential");
        owner_only(&path, "credential-one");
        let opened = protected_open(&path).unwrap_or_else(|error| panic!("{error:?}"));
        let next = directory.join("replacement");
        owner_only(&next, "credential-two");
        fs::rename(&next, &path).unwrap_or_else(|error| panic!("{error:?}"));
        let mut contents = String::new();
        opened
            .take(4096)
            .read_to_string(&mut contents)
            .unwrap_or_else(|error| panic!("{error:?}"));
        assert_eq!(contents, "credential-one");
        let link = directory.join("symbolic");
        std::os::unix::fs::symlink(&path, &link).unwrap_or_else(|error| panic!("{error:?}"));
        assert!(protected_open(&link).is_err());
        let hard = directory.join("hard");
        fs::hard_link(&path, &hard).unwrap_or_else(|error| panic!("{error:?}"));
        assert!(protected_open(&path).is_err());
        assert!(protected_open(&hard).is_err());
        fs::remove_dir_all(directory).unwrap_or_else(|error| panic!("{error:?}"));
    }
}
