//! Durable local store: one fsynced metadata snapshot written by atomic rename,
//! fsynced chunk files, an append-only event log holding identifiers only, the
//! live revocation high-water mark kept outside the restorable snapshot,
//! delivery intents, and the retention purge pass.
use layerx_programs_ai_market::evidence::{ArtifactError, ArtifactManifest, ContentAssembler};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, File, OpenOptions};
use std::io::{Cursor, Read, Write};
use std::path::{Path, PathBuf};
use std::sync::mpsc;
use std::time::Duration;

/// Incomplete staging expires after 24 hours of service UTC time.
pub const STAGING_SECS: u64 = 86_400;
/// Terminal retention: payload bytes of a tombstoned object are kept 30 days.
pub const RETENTION_SECS: u64 = 30 * 86_400;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum SessionState {
    Staging,
    Published,
    Quarantined,
    Failed,
    Expired,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum ObjectState {
    Publishing,
    Available,
    Tombstoned,
    Purged,
    Quarantined,
}

impl ObjectState {
    pub fn name(self) -> &'static str {
        match self {
            Self::Publishing => "PUBLISHING",
            Self::Available => "AVAILABLE",
            Self::Tombstoned => "TOMBSTONED",
            Self::Purged => "PURGED",
            Self::Quarantined => "QUARANTINED",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct Transition {
    pub state: ObjectState,
    pub at: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum DeliveryState {
    Pending,
    Delivered,
    Unknown,
}

impl DeliveryState {
    pub fn name(self) -> &'static str {
        match self {
            Self::Pending => "PENDING",
            Self::Delivered => "DELIVERED",
            Self::Unknown => "UNKNOWN_DELIVERY",
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Disposition {
    pub digest: String,
    pub status: u16,
    pub body: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Session {
    pub tenant: String,
    pub request_id: String,
    pub kind: u8,
    pub privacy: u8,
    pub context: String,
    pub byte_length: u64,
    pub chunk_count: u32,
    pub expected_root: Option<String>,
    pub created_at: u64,
    pub state: SessionState,
    pub root: Option<String>,
    pub wrapped_key: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ObjectRecord {
    pub tenant: String,
    pub session: String,
    pub manifest: String,
    pub envelope: String,
    pub declaration: Option<String>,
    pub declared_purposes: Option<u16>,
    pub rights: Option<u8>,
    pub record_digest: String,
    pub state: ObjectState,
    pub privacy: u8,
    pub byte_length: u64,
    pub chunk_count: u32,
    pub publisher: String,
    pub generation: u64,
    pub wrapped_key: Option<String>,
    pub access_generation: u64,
    pub published_at: u64,
    pub tombstone_reason: Option<u8>,
    pub retention_until: Option<u64>,
    pub history: Vec<Transition>,
}

impl ObjectRecord {
    pub fn transition(&mut self, state: ObjectState, at: u64) {
        self.state = state;
        self.history.push(Transition { state, at });
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Grant {
    pub issuer: String,
    pub grantee: String,
    pub root: String,
    pub task: String,
    pub purpose_mask: u16,
    pub generation: u64,
    pub expires_at: u64,
    pub revoked: bool,
    pub revocation_sequence: u64,
    pub revocation_digest: Option<String>,
}

/// One key-release delivery per tenant request identity. The intent is durable
/// before any response byte is sent; only a completed write within the deadline
/// acknowledges it.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Delivery {
    pub digest: String,
    pub root: String,
    pub grant: String,
    pub generation: u64,
    pub attempts: u32,
    pub state: DeliveryState,
    pub updated_at: u64,
}

/// Binds a delivery identity to exactly what it releases.
pub fn delivery_digest(root: &str, grant: &str, task: &str, purpose: &str) -> String {
    let mut h = Sha256::new();
    for part in [root, grant, task, purpose] {
        h.update(part.as_bytes());
        h.update([0]);
    }
    hex::encode(h.finalize())
}

/// Hands a response to its sink on a worker thread and waits at most
/// `deadline`. A completed write returns the still-open sink, so the caller
/// acknowledges durably before the peer sees the end of the response; `None`
/// means transmission may or may not have happened.
pub fn deliver<W: Write + Send + 'static>(
    mut sink: W,
    bytes: Vec<u8>,
    deadline: Duration,
) -> Option<W> {
    let (done, outcome) = mpsc::channel();
    std::thread::spawn(move || {
        let written = sink.write_all(&bytes).and_then(|()| sink.flush());
        let _ = done.send(written.map(|()| sink));
    });
    outcome.recv_timeout(deadline).ok()?.ok()
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct State {
    pub requests: BTreeMap<String, Disposition>,
    pub sessions: BTreeMap<String, Session>,
    pub objects: BTreeMap<String, ObjectRecord>,
    pub quota_used: BTreeMap<String, u64>,
    pub grants: BTreeMap<String, Grant>,
    pub publisher_revocations: BTreeMap<String, u64>,
    pub deliveries: BTreeMap<String, Delivery>,
    /// Monotonic count of deny transitions (grant and publisher revocation,
    /// rotation, tombstone); restored state below the live mark is stale.
    pub revocation_mark: u64,
}

pub struct Store {
    dir: PathBuf,
    mark_path: PathBuf,
    live_mark: u64,
    pub state: State,
}

const FAIL: ArtifactError = ArtifactError::StorageFailure;

fn sync_dir(path: &Path) -> Result<(), ArtifactError> {
    File::open(path)
        .and_then(|d| d.sync_all())
        .map_err(|_| FAIL)
}

fn write_durable(path: &Path, bytes: &[u8]) -> Result<(), ArtifactError> {
    let tmp = path.with_extension("tmp");
    let mut file = File::create(&tmp).map_err(|_| FAIL)?;
    file.write_all(bytes).map_err(|_| FAIL)?;
    file.sync_all().map_err(|_| FAIL)?;
    fs::rename(&tmp, path).map_err(|_| FAIL)?;
    sync_dir(path.parent().ok_or(FAIL)?)
}

fn read_mark(path: &Path) -> Result<u64, ArtifactError> {
    match fs::read_to_string(path) {
        Ok(text) => text.trim().parse().map_err(|_| FAIL),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(0),
        Err(_) => Err(FAIL),
    }
}

impl Store {
    /// `mark_path` holds the live revocation high-water mark; it lives outside
    /// the data directory so restoring a backup never rolls it back.
    pub fn open(dir: &Path, mark_path: &Path) -> Result<Self, ArtifactError> {
        fs::create_dir_all(dir.join("staging")).map_err(|_| FAIL)?;
        sync_dir(dir)?;
        let mut store = Self {
            dir: dir.to_path_buf(),
            mark_path: mark_path.to_path_buf(),
            live_mark: read_mark(mark_path)?,
            state: State::default(),
        };
        store.reload()?;
        Ok(store)
    }

    pub fn live_mark(&self) -> u64 {
        self.live_mark
    }

    /// A restore is ready only when its revocation high-water mark is at or
    /// above the live mark; a stale grant store never serves with newer files.
    pub fn restore_ready(&self) -> bool {
        self.state.revocation_mark >= self.live_mark
    }

    pub fn reload(&mut self) -> Result<(), ArtifactError> {
        let path = self.dir.join("state.json");
        self.state = match fs::read(&path) {
            Ok(bytes) => serde_json::from_slice(&bytes).map_err(|_| FAIL)?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => State::default(),
            Err(_) => return Err(FAIL),
        };
        Ok(())
    }

    // ponytail: whole-snapshot rewrite per acknowledged operation; move to an
    // append-only journal or database when metadata volume makes this slow.
    pub fn persist(&mut self) -> Result<(), ArtifactError> {
        let bytes = serde_json::to_vec(&self.state).map_err(|_| FAIL)?;
        write_durable(&self.dir.join("state.json"), &bytes)?;
        if self.state.revocation_mark > self.live_mark {
            let mark = format!("{}\n", self.state.revocation_mark);
            write_durable(&self.mark_path, mark.as_bytes())?;
            self.live_mark = self.state.revocation_mark;
        }
        Ok(())
    }

    pub fn release_quota(&mut self, tenant: &str, bytes: u64) {
        if let Some(used) = self.state.quota_used.get_mut(tenant) {
            *used = used.saturating_sub(bytes);
        }
    }

    /// Terminal staging dispositions release their quota reservation exactly once.
    pub fn expire_staging(&mut self, at: u64) -> usize {
        let expired: Vec<(String, String, u64)> = self
            .state
            .sessions
            .iter()
            .filter(|(_, s)| s.state == SessionState::Staging && at >= s.created_at + STAGING_SECS)
            .map(|(id, s)| (id.clone(), s.tenant.clone(), s.byte_length))
            .collect();
        for (id, tenant, bytes) in &expired {
            if let Some(s) = self.state.sessions.get_mut(id) {
                s.state = SessionState::Expired;
            }
            self.release_quota(tenant, *bytes);
        }
        expired.len()
    }

    /// Records the intent durably before any byte is sent. The same identity
    /// with a different release is IdempotencyConflict; one still in flight is
    /// UnknownDelivery. Returns the state this attempt reconciles, if any.
    pub fn begin_delivery(
        &mut self,
        id: &str,
        intent: Delivery,
    ) -> Result<Option<DeliveryState>, ArtifactError> {
        let prior = self
            .state
            .deliveries
            .get(id)
            .map(|d| (d.digest == intent.digest, d.state, d.attempts));
        let attempts = match prior {
            Some((false, _, _)) => return Err(ArtifactError::IdempotencyConflict),
            Some((true, DeliveryState::Pending, _)) => return Err(ArtifactError::UnknownDelivery),
            Some((true, _, n)) => n.saturating_add(1),
            None => 1,
        };
        self.state.deliveries.insert(
            id.to_string(),
            Delivery {
                attempts,
                state: DeliveryState::Pending,
                ..intent
            },
        );
        self.persist()?;
        self.event(&format!("delivery-intent id={id} attempt={attempts}"))?;
        Ok(prior.map(|p| p.1))
    }

    /// Acknowledges a completed write, or records that the outcome is unknown.
    pub fn settle_delivery(
        &mut self,
        id: &str,
        delivered: bool,
        at: u64,
    ) -> Result<(), ArtifactError> {
        let delivery = self
            .state
            .deliveries
            .get_mut(id)
            .filter(|d| d.state == DeliveryState::Pending)
            .ok_or(FAIL)?;
        delivery.state = if delivered {
            DeliveryState::Delivered
        } else {
            DeliveryState::Unknown
        };
        delivery.updated_at = at;
        let state = delivery.state.name();
        self.persist()?;
        self.event(&format!("delivery-settled id={id} state={state}"))
    }

    /// Restart pass: an intent never acknowledged may already have been
    /// transmitted, so it reports UnknownDelivery until a retry reconciles it.
    pub fn interrupted_deliveries(&mut self, at: u64) -> usize {
        let mut count = 0;
        for delivery in self.state.deliveries.values_mut() {
            if delivery.state == DeliveryState::Pending {
                delivery.state = DeliveryState::Unknown;
                delivery.updated_at = at;
                count += 1;
            }
        }
        count
    }

    /// Retention pass driven by the caller's clock: every tombstoned object at
    /// or past its retention deadline loses its payload replicas and key
    /// envelope; root, manifest, reason and history stay. A removal failure
    /// leaves it TOMBSTONED (pending purge) for the next pass.
    pub fn purge_due(&mut self, at: u64) -> Result<Vec<String>, ArtifactError> {
        let due: Vec<(String, String)> = self
            .state
            .objects
            .iter()
            .filter(|(_, o)| {
                o.state == ObjectState::Tombstoned && o.retention_until.is_some_and(|d| at >= d)
            })
            .map(|(root, o)| (root.clone(), o.session.clone()))
            .collect();
        let mut purged = Vec::new();
        for (root, session) in due {
            let replicas: BTreeSet<String> = self
                .state
                .sessions
                .iter()
                .filter(|(_, s)| s.root.as_deref() == Some(root.as_str()))
                .map(|(id, _)| id.clone())
                .chain([session])
                .collect();
            if replicas
                .iter()
                .try_for_each(|s| self.remove_session_dir(s))
                .is_err()
            {
                self.event(&format!("purge-pending root={root}"))?;
                continue;
            }
            let Some(record) = self.state.objects.get_mut(&root) else {
                continue;
            };
            record.wrapped_key = None;
            record.transition(ObjectState::Purged, at);
            let (tenant, bytes) = (record.tenant.clone(), record.byte_length);
            self.release_quota(&tenant, bytes);
            self.event(&format!("purged root={root}"))?;
            purged.push(root);
        }
        if !purged.is_empty() {
            self.persist()?;
        }
        Ok(purged)
    }

    pub fn event(&self, line: &str) -> Result<(), ArtifactError> {
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(self.dir.join("events.log"))
            .map_err(|_| FAIL)?;
        writeln!(file, "{line}").map_err(|_| FAIL)?;
        file.sync_data().map_err(|_| FAIL)
    }

    fn session_dir(&self, session: &str) -> PathBuf {
        self.dir.join("staging").join(session)
    }

    pub fn create_session_dir(&self, session: &str) -> Result<(), ArtifactError> {
        fs::create_dir_all(self.session_dir(session)).map_err(|_| FAIL)?;
        sync_dir(&self.dir.join("staging"))
    }

    fn remove_session_dir(&self, session: &str) -> Result<(), ArtifactError> {
        match fs::remove_dir_all(self.session_dir(session)) {
            Ok(()) => sync_dir(&self.dir.join("staging")),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(_) => Err(FAIL),
        }
    }

    pub fn read_chunk(&self, session: &str, index: u32) -> Result<Option<Vec<u8>>, ArtifactError> {
        match fs::read(self.session_dir(session).join(index.to_string())) {
            Ok(bytes) => Ok(Some(bytes)),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(_) => Err(FAIL),
        }
    }

    /// An already stored chunk with different bytes is an integrity conflict.
    pub fn write_chunk(
        &self,
        session: &str,
        index: u32,
        bytes: &[u8],
    ) -> Result<(), ArtifactError> {
        if let Some(existing) = self.read_chunk(session, index)? {
            return if existing == bytes {
                Ok(())
            } else {
                Err(ArtifactError::IntegrityConflict)
            };
        }
        write_durable(&self.session_dir(session).join(index.to_string()), bytes)
    }

    /// Re-reads every durable chunk and recomputes the manifest content root;
    /// a file's existence alone never counts as a completed upload.
    pub fn verify_content(
        &self,
        session: &str,
        manifest: &ArtifactManifest<'_>,
    ) -> Result<(), ArtifactError> {
        let mut scratch = vec![[0u8; 32]; manifest.chunk_count as usize];
        let mut assembler = ContentAssembler::new(manifest, &mut scratch)?;
        for index in 0..manifest.chunk_count {
            let chunk = self
                .read_chunk(session, index)?
                .ok_or(ArtifactError::MissingChunk)?;
            assembler.deliver(index, &chunk)?;
        }
        assembler.finish()
    }
}

/// Sequential reader over a session's durable chunk files; a missing chunk is
/// an error, never an early end of stream.
pub struct ChunkReader<'a> {
    store: &'a Store,
    session: &'a str,
    count: u32,
    next: u32,
    current: Cursor<Vec<u8>>,
}

impl Store {
    pub fn chunk_reader<'a>(&'a self, session: &'a str, count: u32) -> ChunkReader<'a> {
        ChunkReader {
            store: self,
            session,
            count,
            next: 0,
            current: Cursor::new(Vec::new()),
        }
    }
}

impl Read for ChunkReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        loop {
            let n = self.current.read(buf)?;
            if n > 0 || buf.is_empty() || self.next >= self.count {
                return Ok(n);
            }
            let chunk = self
                .store
                .read_chunk(self.session, self.next)
                .map_err(|_| std::io::Error::other("chunk read failed"))?
                .ok_or_else(|| std::io::Error::other("chunk missing"))?;
            self.current = Cursor::new(chunk);
            self.next += 1;
        }
    }
}
