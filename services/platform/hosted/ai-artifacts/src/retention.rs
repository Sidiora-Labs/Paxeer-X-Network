//! Retention ledger beside the durable store: block-commit terminal timers,
//! dispute/evidence holds, the binding projection, immutable tombstone
//! records, pending-purge accounting, restart reconciliation of deletion
//! intents and payload restoration from replicas. Evidence is promoted only on
//! `CommitFinality`: the block commit plus state proof at the committed height
//! of a `SnapshotBinding` over the exact market state bytes it names. A missing
//! producer, record, authority or payload fails closed, keeps the bytes and is
//! never reported present. The ledger has its own live high-water mark outside
//! the restorable data directory.
use crate::store::{ObjectRecord, ObjectState, SessionState, Store, RETENTION_SECS};
use layerx_programs_ai_market::codec;
use layerx_programs_ai_market::errors::ApplicationError;
use layerx_programs_ai_market::evidence::{decode_manifest, ArtifactError, ArtifactKind};
use layerx_programs_ai_market::queries::SnapshotBinding;
use layerx_programs_ai_market::registry_ops::PolicySection;
use layerx_programs_ai_market::state::{decode_shared_state, Section};
use layerx_programs_ai_market::tasks::{TaskBinding, TaskSet, TaskStatus};
use layerx_programs_ai_market::types::Digest32;
use layerx_programs_ai_market::types::TaskId;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::ffi::OsString;
use std::fs::{self, File};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

/// v1 maximum ordinary lease; a longer hold is refused by storage admission.
pub const MAX_HOLD_SECS: u64 = 365 * 86_400;
pub const EXPIRED: u8 = 1;
pub const OWNER_WITHDRAWN: u8 = 2;
pub const POLICY_RESTRICTED: u8 = 3;
pub const INTEGRITY_FAILURE: u8 = 4;

const FAIL: ArtifactError = ArtifactError::StorageFailure;

pub fn reason_name(reason: u8) -> Option<&'static str> {
    match reason {
        EXPIRED => Some("EXPIRED"),
        OWNER_WITHDRAWN => Some("OWNER_WITHDRAWN"),
        POLICY_RESTRICTED => Some("POLICY_RESTRICTED"),
        INTEGRITY_FAILURE => Some("INTEGRITY_FAILURE"),
        _ => None,
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Refusal {
    Artifact(ArtifactError),
    /// The state head's block is not committed, or names no committed height.
    FinalityUnavailable,
    /// A required record, producer or reference is absent from the evidence.
    EvidenceUnavailable,
    /// The task is admitted or accepted: execution is unresolved, no timer runs.
    UnresolvedExecution,
    /// A binding transaction is unknown or unfinalized.
    BindingPending,
    RetentionActive {
        until: u64,
    },
    HoldActive {
        until: u64,
    },
    /// The store or ledger is below its live high-water mark.
    RestoreNotReady,
}

impl Refusal {
    pub fn name(self) -> &'static str {
        match self {
            Self::Artifact(e) => e.name(),
            Self::FinalityUnavailable => "FINALITY_UNAVAILABLE",
            Self::EvidenceUnavailable => "EVIDENCE_UNAVAILABLE",
            Self::UnresolvedExecution => "UNRESOLVED_EXECUTION",
            Self::BindingPending => "BINDING_PENDING",
            Self::RetentionActive { .. } => "RETENTION_ACTIVE",
            Self::HoldActive { .. } => "HOLD_ACTIVE",
            Self::RestoreNotReady => "RESTORE_NOT_READY",
        }
    }
}

impl From<ArtifactError> for Refusal {
    fn from(error: ArtifactError) -> Self {
        Self::Artifact(error)
    }
}

impl From<ApplicationError> for Refusal {
    fn from(error: ApplicationError) -> Self {
        Self::Artifact(error.into())
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub enum Binding {
    #[default]
    Unbound,
    Pending,
    Finalized,
}

impl Binding {
    pub fn name(self) -> &'static str {
        match self {
            Self::Unbound => "UPLOADED_UNBOUND",
            Self::Pending => "BINDING_PENDING",
            Self::Finalized => "FINALIZED",
        }
    }
}

/// The block commit plus state proof that finalizes one state head: the
/// commit at the head's own execution height, bound by `bind_snapshot` to the
/// head's native state root; the market state bytes are proven against the
/// head by their state digest before anything is promoted.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CommitFinality {
    pub height: u64,
    pub commit: Digest32,
}

impl CommitFinality {
    /// The committed head only: a block below commit rank or a head with no
    /// committed height is `FinalityUnavailable`.
    pub fn of(snapshot: &SnapshotBinding) -> Result<Self, Refusal> {
        snapshot
            .require_finalized()
            .map_err(|_| Refusal::FinalityUnavailable)?;
        if snapshot.execution_height == 0 {
            return Err(Refusal::FinalityUnavailable);
        }
        Ok(Self {
            height: snapshot.execution_height,
            commit: snapshot.checkpoint,
        })
    }
}

/// First committed terminal observation; it fixes day 0 and is never moved.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct Terminal {
    pub task: String,
    pub snapshot: String,
    pub observed_at: u64,
    pub retain_until: u64,
    /// Committed height and block commit of the terminal head.
    #[serde(default)]
    pub height: u64,
    #[serde(default)]
    pub commit: String,
}

/// Append-only; a reinstatement never removes an earlier tombstone.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum Event {
    Tombstoned {
        version: u32,
        reason: u8,
        at: u64,
    },
    Reinstated {
        grant: String,
        decision: String,
        at: u64,
    },
    PurgeFailed {
        attempt: u32,
        at: u64,
    },
    Purged {
        at: u64,
    },
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Entry {
    pub terminal: Option<Terminal>,
    pub holds: BTreeMap<String, u64>,
    pub binding: Binding,
    /// Commit height at which the binding was promoted to `FINALIZED`.
    #[serde(default)]
    pub committed_height: Option<u64>,
    pub purge_failures: u32,
    pub history: Vec<Event>,
}

impl Entry {
    /// Physical deletion deadline: the terminal floor raised by every hold;
    /// none while the terminal head is uncommitted or a binding is ambiguous.
    pub fn purge_after(&self) -> Option<u64> {
        if self.binding == Binding::Pending {
            return None;
        }
        let floor = self.terminal.as_ref()?.retain_until;
        Some(self.holds.values().fold(floor, |a, b| a.max(*b)))
    }

    pub fn tombstone_version(&self) -> u32 {
        let count = self
            .history
            .iter()
            .filter(|e| matches!(e, Event::Tombstoned { .. }))
            .count();
        u32::try_from(count).unwrap_or(u32::MAX)
    }

    /// The tombstone intent still in force, if the latest lifecycle event is one.
    fn standing_tombstone(&self) -> Option<(u8, u64)> {
        self.history.iter().rev().find_map(|e| match e {
            Event::Tombstoned { reason, at, .. } => Some(Some((*reason, *at))),
            Event::Reinstated { .. } => Some(None),
            _ => None,
        })?
    }

    /// Ordinary deletion (`EXPIRED`, `OWNER_WITHDRAWN`): only with no ambiguous
    /// binding, after the committed terminal head plus 30 days, with no active hold.
    pub fn admit_deletion(&self, at: u64) -> Result<(), Refusal> {
        if self.binding == Binding::Pending {
            return Err(Refusal::BindingPending);
        }
        let floor = self
            .terminal
            .as_ref()
            .ok_or(Refusal::EvidenceUnavailable)?
            .retain_until;
        if at < floor {
            return Err(Refusal::RetentionActive { until: floor });
        }
        match self.holds.values().copied().filter(|h| *h > at).max() {
            Some(until) => Err(Refusal::HoldActive { until }),
            None => Ok(()),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Tombstone {
    pub version: u32,
    pub reason: u8,
    pub at: u64,
    pub purge_after: Option<u64>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct PurgeReport {
    pub purged: Vec<String>,
    pub pending: Vec<String>,
}

/// Public lifecycle view of one root; it carries no economic effect.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Projection {
    pub state: &'static str,
    pub availability: &'static str,
    pub binding: &'static str,
    pub tombstone_reason: Option<&'static str>,
    pub tombstone_version: u32,
    pub retain_until: Option<u64>,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
struct LedgerState {
    mark: u64,
    /// Highest service time seen; new deadlines start no earlier.
    clock: u64,
    entries: BTreeMap<String, Entry>,
}

pub struct Ledger {
    path: PathBuf,
    mark_path: PathBuf,
    live_mark: u64,
    state: LedgerState,
}

fn sync_dir(path: &Path) -> Result<(), ArtifactError> {
    File::open(path)
        .and_then(|d| d.sync_all())
        .map_err(|_| FAIL)
}

fn write_durable(path: &Path, bytes: &[u8]) -> Result<(), ArtifactError> {
    let mut tmp = OsString::from(path);
    tmp.push(".tmp");
    let tmp = PathBuf::from(tmp);
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

fn load(path: &Path) -> Result<LedgerState, ArtifactError> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(|_| FAIL),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(LedgerState::default()),
        Err(_) => Err(FAIL),
    }
}

fn root_bytes(root: &str) -> Result<[u8; 32], ArtifactError> {
    let bytes = hex::decode(root).map_err(|_| ArtifactError::Malformed)?;
    bytes.try_into().map_err(|_| ArtifactError::Malformed)
}

fn owned<'a>(store: &'a Store, tenant: &str, root: &str) -> Result<&'a ObjectRecord, Refusal> {
    store
        .state
        .objects
        .get(root)
        .filter(|o| o.tenant == tenant)
        .ok_or(Refusal::Artifact(ArtifactError::Unauthorized))
}

fn known(store: &Store, root: &str) -> Result<(), Refusal> {
    match store.state.objects.get(root).map(|o| o.state) {
        Some(ObjectState::Purged) | None => Err(ArtifactError::ContentUnavailable.into()),
        Some(_) => Ok(()),
    }
}

fn manifest_bytes(record: &ObjectRecord) -> Result<Vec<u8>, ArtifactError> {
    hex::decode(&record.manifest).map_err(|_| ArtifactError::Malformed)
}

/// Present only when every durable chunk of `session` re-verifies against the
/// record's manifest; a missing, short or altered chunk is absent.
fn payload_present(store: &Store, record: &ObjectRecord, session: &str) -> bool {
    manifest_bytes(record)
        .and_then(|bytes| store.verify_content(session, &decode_manifest(&bytes)?))
        .is_ok()
}

/// Every retained root whose primary payload fails verification is served from
/// a published replica session of the same root that re-verifies in full; a
/// root with no such replica stays unrestored and is never reported present.
/// A restored quarantined root returns to available; tombstoned roots keep
/// their state.
// ponytail: re-reads every retained payload at startup; track per-root
// verification marks if stores grow large enough to slow restart.
fn restore_payloads(store: &mut Store) -> Result<(), ArtifactError> {
    let mut restored = Vec::new();
    let mut missing = Vec::new();
    for (root, record) in &store.state.objects {
        if !matches!(
            record.state,
            ObjectState::Available | ObjectState::Quarantined | ObjectState::Tombstoned
        ) || payload_present(store, record, &record.session)
        {
            continue;
        }
        let replica = store
            .state
            .sessions
            .iter()
            .filter(|(id, s)| {
                **id != record.session
                    && s.state == SessionState::Published
                    && s.root.as_deref() == Some(root.as_str())
            })
            .map(|(id, _)| id)
            .find(|id| payload_present(store, record, id));
        match replica {
            Some(id) => restored.push((root.clone(), id.clone())),
            None => missing.push(root.clone()),
        }
    }
    let at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| FAIL)?
        .as_secs();
    for (root, session) in &restored {
        if let Some(record) = store.state.objects.get_mut(root) {
            record.session = session.clone();
            if record.state == ObjectState::Quarantined {
                record.transition(ObjectState::Available, at);
            }
        }
    }
    if !restored.is_empty() {
        store.persist()?;
    }
    for (root, session) in &restored {
        store.event(&format!("restored root={root} replica={session}"))?;
    }
    for root in &missing {
        store.event(&format!("payload-missing root={root}"))?;
    }
    Ok(())
}

/// The artifact's kind and its subject task record in the exact snapshot
/// state: the state digest, chain, program and market must all match.
fn subject_task(
    store: &Store,
    root: &str,
    snapshot: &SnapshotBinding,
    state: &[u8],
) -> Result<(ArtifactKind, Option<TaskBinding>), Refusal> {
    let record = store
        .state
        .objects
        .get(root)
        .ok_or(ArtifactError::ContentUnavailable)?;
    let bytes = manifest_bytes(record)?;
    let manifest = decode_manifest(&bytes)?;
    let context = manifest.context;
    if context.chain != snapshot.chain
        || context.program != snapshot.program
        || context.market != snapshot.market
    {
        return Err(ArtifactError::InvalidContext.into());
    }
    if codec::state_digest(state)? != snapshot.state_digest {
        return Err(ArtifactError::IntegrityConflict.into());
    }
    if !matches!(
        manifest.kind,
        ArtifactKind::Input | ArtifactKind::Result | ArtifactKind::ExecutionEvidence
    ) {
        return Err(Refusal::EvidenceUnavailable);
    }
    let task = TaskId::new(manifest.subject)?;
    let shared = decode_shared_state(state)?;
    let section = PolicySection::decode(shared.section(Section::PolicyLifecycle)?)?;
    let (_, binding) = TaskSet::decode(section.task_region)?.position(task)?;
    Ok((manifest.kind, binding))
}

impl Ledger {
    /// `mark_path` holds the live ledger high-water mark; it lives outside the
    /// data directory so restoring a backup never rolls it back.
    pub fn open(dir: &Path, mark_path: &Path) -> Result<Self, ArtifactError> {
        let path = dir.join("retention.json");
        Ok(Self {
            state: load(&path)?,
            path,
            mark_path: mark_path.to_path_buf(),
            live_mark: read_mark(mark_path)?,
        })
    }

    /// A restored ledger below the live mark may lack holds or terminal
    /// observations and must not drive deletion.
    pub fn ready(&self) -> bool {
        self.state.mark >= self.live_mark
    }

    pub fn entry(&self, root: &str) -> Option<&Entry> {
        self.state.entries.get(root)
    }

    fn entry_mut(&mut self, root: &str) -> &mut Entry {
        self.state.entries.entry(root.to_string()).or_default()
    }

    /// Deadlines start at the highest service time seen, so a clock that
    /// moves backward never shortens one.
    fn anchor(&mut self, at: u64) -> u64 {
        self.state.clock = self.state.clock.max(at);
        self.state.clock
    }

    /// Every transition raises the mark; the live mark follows the durable
    /// file. A refused or failed write drops the uncommitted change.
    fn commit(&mut self) -> Result<(), Refusal> {
        let written = self.write();
        if written.is_err() {
            if let Ok(state) = load(&self.path) {
                self.state = state;
            }
        }
        written
    }

    fn write(&mut self) -> Result<(), Refusal> {
        if !self.ready() {
            return Err(Refusal::RestoreNotReady);
        }
        let mark = self.state.mark.checked_add(1).ok_or(FAIL)?;
        self.state.mark = mark;
        let bytes = serde_json::to_vec(&self.state).map_err(|_| FAIL)?;
        write_durable(&self.path, &bytes)?;
        write_durable(&self.mark_path, format!("{mark}\n").as_bytes())?;
        self.live_mark = mark;
        Ok(())
    }

    /// Tombstoned records carry the ledger's deletion deadline, so any purge
    /// pass over the store honours holds, ambiguity and missing finality.
    fn sync(&self, store: &mut Store) -> Result<(), ArtifactError> {
        let mut changed = false;
        for (root, record) in &mut store.state.objects {
            if record.state != ObjectState::Tombstoned {
                continue;
            }
            let after = self.state.entries.get(root).and_then(Entry::purge_after);
            if record.retention_until != after {
                record.retention_until = after;
                changed = true;
            }
        }
        if changed {
            store.persist()?;
        }
        Ok(())
    }

    /// Starts the 30-day terminal timer from a committed state head in which
    /// the artifact's subject task is cancelled. Admitted or accepted tasks are
    /// unresolved; a committed result needs the worker's terminal outcome
    /// evidence, which this ledger never assumes.
    pub fn observe_terminal(
        &mut self,
        store: &mut Store,
        root: &str,
        snapshot: &SnapshotBinding,
        state: &[u8],
        at: u64,
    ) -> Result<u64, Refusal> {
        known(store, root)?;
        let commit = CommitFinality::of(snapshot)?;
        let (_, record) = subject_task(store, root, snapshot, state)?;
        let record = record.ok_or(Refusal::EvidenceUnavailable)?;
        match record.status {
            TaskStatus::Cancelled => {}
            TaskStatus::Admitted | TaskStatus::Accepted => {
                return Err(Refusal::UnresolvedExecution)
            }
            TaskStatus::ResultCommitted => return Err(Refusal::EvidenceUnavailable),
        }
        if let Some(terminal) = self.entry(root).and_then(|e| e.terminal.as_ref()) {
            return Ok(terminal.retain_until);
        }
        let observed_at = self.anchor(at);
        let retain_until = observed_at
            .checked_add(RETENTION_SECS)
            .ok_or(ArtifactError::Malformed)?;
        let snapshot_id = hex::encode(snapshot.snapshot_id()?.bytes());
        self.entry_mut(root).terminal = Some(Terminal {
            task: hex::encode(record.task.bytes()),
            snapshot: snapshot_id,
            observed_at,
            retain_until,
            height: commit.height,
            commit: hex::encode(commit.commit.bytes()),
        });
        self.commit()?;
        self.sync(store)?;
        store.event(&format!(
            "retention-terminal root={root} until={retain_until}"
        ))?;
        Ok(retain_until)
    }

    /// A binding transaction was submitted; its outcome is unknown.
    pub fn binding_intent(&mut self, store: &mut Store, root: &str) -> Result<Binding, Refusal> {
        known(store, root)?;
        let entry = self.entry_mut(root);
        if entry.binding != Binding::Unbound {
            return Ok(entry.binding);
        }
        entry.binding = Binding::Pending;
        self.commit()?;
        self.sync(store)?;
        store.event(&format!("binding-pending root={root}"))?;
        Ok(Binding::Pending)
    }

    /// The sealed digest bound by its subject task record (INPUT by the input
    /// digest, RESULT by the result digest) is FINALIZED at its commit height
    /// only on `CommitFinality`; an uncommitted head leaves the binding
    /// pending. A finalized binding is never downgraded.
    pub fn observe_binding(
        &mut self,
        store: &mut Store,
        root: &str,
        snapshot: &SnapshotBinding,
        state: &[u8],
    ) -> Result<Binding, Refusal> {
        known(store, root)?;
        let (kind, record) = subject_task(store, root, snapshot, state)?;
        let record = record.ok_or(Refusal::EvidenceUnavailable)?;
        let bytes = root_bytes(root)?;
        let referenced = match kind {
            ArtifactKind::Input => record.input.bytes() == bytes,
            ArtifactKind::Result => record.result.is_some_and(|r| r.bytes() == bytes),
            _ => false,
        };
        if !referenced {
            return Err(Refusal::EvidenceUnavailable);
        }
        let commit = CommitFinality::of(snapshot).ok();
        let next = if commit.is_some() {
            Binding::Finalized
        } else {
            Binding::Pending
        };
        let entry = self.entry_mut(root);
        if entry.binding == Binding::Finalized || entry.binding == next {
            return Ok(entry.binding);
        }
        entry.binding = next;
        entry.committed_height = commit.map(|c| c.height);
        self.commit()?;
        self.sync(store)?;
        store.event(&format!("binding root={root} status={}", next.name()))?;
        Ok(next)
    }

    /// A reorg removes only an unfinalized binding; the bytes stay.
    pub fn orphan_binding(&mut self, store: &mut Store, root: &str) -> Result<Binding, Refusal> {
        known(store, root)?;
        match self.entry(root).map_or(Binding::Unbound, |e| e.binding) {
            Binding::Finalized => return Err(ArtifactError::IntegrityConflict.into()),
            Binding::Unbound => return Ok(Binding::Unbound),
            Binding::Pending => self.entry_mut(root).binding = Binding::Unbound,
        }
        self.commit()?;
        self.sync(store)?;
        store.event(&format!("binding-orphaned root={root}"))?;
        Ok(Binding::Unbound)
    }

    /// Places or extends a dispute/evidence hold; it is never shortened by a
    /// repeat placement and is capped at the ordinary lease.
    pub fn place_hold(
        &mut self,
        store: &mut Store,
        tenant: &str,
        root: &str,
        hold: [u8; 32],
        until: u64,
        at: u64,
    ) -> Result<u64, Refusal> {
        owned(store, tenant, root)?;
        known(store, root)?;
        if hold == [0; 32] {
            return Err(ArtifactError::Malformed.into());
        }
        let anchor = self.anchor(at);
        if until <= anchor {
            return Err(ArtifactError::Malformed.into());
        }
        if until - anchor > MAX_HOLD_SECS {
            return Err(ArtifactError::CapacityUnavailable.into());
        }
        let id = hex::encode(hold);
        let slot = self.entry_mut(root).holds.entry(id.clone()).or_insert(0);
        *slot = (*slot).max(until);
        let extended = *slot;
        self.commit()?;
        self.sync(store)?;
        store.event(&format!("hold root={root} hold={id} until={extended}"))?;
        Ok(extended)
    }

    pub fn release_hold(
        &mut self,
        store: &mut Store,
        tenant: &str,
        root: &str,
        hold: [u8; 32],
    ) -> Result<(), Refusal> {
        owned(store, tenant, root)?;
        let id = hex::encode(hold);
        self.state
            .entries
            .get_mut(root)
            .and_then(|e| e.holds.remove(&id))
            .ok_or(ArtifactError::ContentUnavailable)?;
        self.commit()?;
        self.sync(store)?;
        store
            .event(&format!("hold-released root={root} hold={id}"))
            .map_err(Refusal::from)
    }

    /// `TombstoneArtifact`. `POLICY_RESTRICTED` and `INTEGRITY_FAILURE` deny
    /// serving at once; `EXPIRED` and `OWNER_WITHDRAWN` need `admit_deletion`. The intent
    /// is durable in the ledger before the store changes; bytes stay until
    /// the ledger's deletion deadline. No protocol record is touched.
    pub fn tombstone(
        &mut self,
        store: &mut Store,
        tenant: &str,
        root: &str,
        reason: u8,
        at: u64,
    ) -> Result<Tombstone, Refusal> {
        reason_name(reason).ok_or(ArtifactError::Malformed)?;
        let record = owned(store, tenant, root)?;
        if !matches!(
            record.state,
            ObjectState::Available | ObjectState::Quarantined
        ) {
            return Err(ArtifactError::Tombstoned.into());
        }
        if matches!(reason, EXPIRED | OWNER_WITHDRAWN) {
            self.entry(root)
                .ok_or(Refusal::EvidenceUnavailable)?
                .admit_deletion(at)?;
        }
        let entry = self.entry_mut(root);
        let version = entry.tombstone_version().checked_add(1).ok_or(FAIL)?;
        entry.history.push(Event::Tombstoned {
            version,
            reason,
            at,
        });
        entry.purge_failures = 0;
        let purge_after = entry.purge_after();
        self.commit()?;
        apply_tombstone(store, root, reason, at)?;
        self.sync(store)?;
        Ok(Tombstone {
            version,
            reason,
            at,
            purge_after,
        })
    }

    /// Reinstates a tombstoned root on an explicit current grant and policy
    /// decision as a new availability event; tombstone history stays.
    pub fn reinstate(
        &mut self,
        store: &mut Store,
        tenant: &str,
        root: &str,
        grant_id: &str,
        decision: [u8; 32],
        at: u64,
    ) -> Result<(), Refusal> {
        let record = owned(store, tenant, root)?;
        match record.state {
            ObjectState::Tombstoned => {}
            ObjectState::Purged => return Err(ArtifactError::ContentUnavailable.into()),
            _ => return Err(ArtifactError::Malformed.into()),
        }
        if decision == [0; 32] {
            return Err(ArtifactError::Malformed.into());
        }
        let grant = store
            .state
            .grants
            .get(grant_id)
            .filter(|g| g.issuer == tenant && g.root == root)
            .ok_or(ArtifactError::Unauthorized)?;
        if grant.revoked || grant.generation != record.access_generation {
            return Err(ArtifactError::AuthorityRevoked.into());
        }
        if at >= grant.expires_at {
            return Err(ArtifactError::Expired.into());
        }
        let manifest = hex::decode(&record.manifest).map_err(|_| ArtifactError::Malformed)?;
        store.verify_content(&record.session, &decode_manifest(&manifest)?)?;
        let record = store
            .state
            .objects
            .get_mut(root)
            .ok_or(ArtifactError::ContentUnavailable)?;
        record.transition(ObjectState::Available, at);
        record.retention_until = None;
        store.persist()?;
        self.entry_mut(root).history.push(Event::Reinstated {
            grant: grant_id.to_string(),
            decision: hex::encode(decision),
            at,
        });
        self.commit()?;
        store
            .event(&format!("reinstated root={root}"))
            .map_err(Refusal::from)
    }

    /// Retention purge on the caller's clock. A root whose removal fails stays
    /// `TOMBSTONED_PENDING_PURGE`, raises an alert and is retried next pass.
    pub fn purge(&mut self, store: &mut Store, at: u64) -> Result<PurgeReport, Refusal> {
        self.sync(store)?;
        let due: Vec<String> = store
            .state
            .objects
            .iter()
            .filter(|(_, o)| {
                o.state == ObjectState::Tombstoned && o.retention_until.is_some_and(|d| at >= d)
            })
            .map(|(root, _)| root.clone())
            .collect();
        if due.is_empty() {
            return Ok(PurgeReport::default());
        }
        let purged = store.purge_due(at)?;
        let mut pending = Vec::new();
        for root in due {
            let entry = self.entry_mut(&root);
            if purged.contains(&root) {
                entry.history.push(Event::Purged { at });
                continue;
            }
            entry.purge_failures = entry.purge_failures.saturating_add(1);
            let attempt = entry.purge_failures;
            entry.history.push(Event::PurgeFailed { attempt, at });
            pending.push((root, attempt));
        }
        self.commit()?;
        for (root, attempt) in &pending {
            store.event(&format!("purge-alert root={root} attempts={attempt}"))?;
        }
        Ok(PurgeReport {
            purged,
            pending: pending.into_iter().map(|(root, _)| root).collect(),
        })
    }

    /// Restart pass, before serving: both the store and the ledger must be at
    /// their live marks; a retained payload missing from its primary copy is
    /// restored from a verified replica; a tombstone intent the store never
    /// applied is completed; deletion deadlines are re-derived. Returns
    /// completed intents.
    pub fn recover(&mut self, store: &mut Store) -> Result<usize, Refusal> {
        if !store.restore_ready() || !self.ready() {
            return Err(Refusal::RestoreNotReady);
        }
        restore_payloads(store)?;
        let standing: Vec<(String, u8, u64)> = self
            .state
            .entries
            .iter()
            .filter_map(|(root, e)| e.standing_tombstone().map(|(r, at)| (root.clone(), r, at)))
            .filter(|(root, _, _)| {
                store.state.objects.get(root).is_some_and(|o| {
                    matches!(o.state, ObjectState::Available | ObjectState::Quarantined)
                })
            })
            .collect();
        for (root, reason, at) in &standing {
            apply_tombstone(store, root, *reason, *at)?;
            store.event(&format!("reconcile tombstone root={root}"))?;
        }
        self.sync(store)?;
        Ok(standing.len())
    }

    pub fn projection(&self, store: &Store, root: &str) -> Result<Projection, Refusal> {
        let record = store
            .state
            .objects
            .get(root)
            .ok_or(ArtifactError::ContentUnavailable)?;
        let entry = self.entry(root);
        let state = match record.state {
            ObjectState::Tombstoned if entry.is_some_and(|e| e.purge_failures > 0) => {
                "TOMBSTONED_PENDING_PURGE"
            }
            other => other.name(),
        };
        let removed = matches!(record.state, ObjectState::Tombstoned | ObjectState::Purged);
        let restricted = record.state == ObjectState::Purged
            || (removed && record.tombstone_reason == Some(POLICY_RESTRICTED));
        let present = record.state == ObjectState::Available
            && payload_present(store, record, &record.session);
        let availability = if present {
            "AVAILABLE"
        } else if restricted || record.state == ObjectState::Available {
            "EVIDENCE_UNAVAILABLE"
        } else {
            "UNAVAILABLE"
        };
        Ok(Projection {
            state,
            availability,
            binding: entry.map_or(Binding::Unbound, |e| e.binding).name(),
            tombstone_reason: record
                .tombstone_reason
                .filter(|_| removed)
                .and_then(reason_name),
            tombstone_version: entry.map_or(0, Entry::tombstone_version),
            retain_until: entry.and_then(Entry::purge_after),
        })
    }
}

/// Denies serving durably: the record is TOMBSTONED with its reason and the
/// revocation high-water mark rises, so a stale restore cannot serve it.
fn apply_tombstone(
    store: &mut Store,
    root: &str,
    reason: u8,
    at: u64,
) -> Result<(), ArtifactError> {
    let record = store
        .state
        .objects
        .get_mut(root)
        .ok_or(ArtifactError::ContentUnavailable)?;
    record.transition(ObjectState::Tombstoned, at);
    record.tombstone_reason = Some(reason);
    record.retention_until = None;
    store.state.revocation_mark = store.state.revocation_mark.checked_add(1).ok_or(FAIL)?;
    store.persist()?;
    store.event(&format!("tombstone root={root} reason={reason}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::{ObjectRecord, Session, Transition};
    use layerx_programs_ai_market::codec::{
        decode_envelope, derive_market, encode_envelope, Envelope,
    };
    use layerx_programs_ai_market::dispatch;
    use layerx_programs_ai_market::evidence::{
        chunk_count, encode_manifest, manifest_root, object_content_root, ArtifactContext,
        ArtifactManifest, Items, Privacy, CHUNK_BYTES, MAX_MANIFEST_BYTES,
    };
    use layerx_programs_ai_market::policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES};
    use layerx_programs_ai_market::queries::{
        bind_snapshot, CaptureFacts, FinalityEvidence, ReadProof,
    };
    use layerx_programs_ai_market::registry::{derive_rewards_account, F01_SECTION_CAP};
    use layerx_programs_ai_market::registry_ops::{self, CallContext, Outcome};
    use layerx_programs_ai_market::state::{self, SharedState};
    use layerx_programs_ai_market::types::{
        AssetId, Authentication, ChainDomain, PolicyDigest, Presence, PrincipalId, ProgramId,
        RequestId, RubricDigest, WorkerId,
    };
    use layerx_programs_ai_market::{MAX_CHUNK_BYTES, MAX_EVENT_BYTES};
    use std::time::{SystemTime, UNIX_EPOCH};

    const TENANT: &str = "tenant-a";
    const CHAIN: [u8; 32] = [0x11; 32];
    const PROGRAM: [u8; 32] = [0x22; 32];
    const DAY: u64 = 86_400;

    fn chain() -> ChainDomain {
        ChainDomain::new(CHAIN).unwrap()
    }
    fn program() -> ProgramId {
        ProgramId::new(PROGRAM).unwrap()
    }
    fn digest(byte: u8) -> Digest32 {
        Digest32::new([byte; 32]).unwrap()
    }
    fn ctx() -> ArtifactContext {
        ArtifactContext {
            chain: chain(),
            program: program(),
            market: derive_market(chain(), program()).unwrap(),
            policy: PolicyDigest::new([0x44; 32]).unwrap(),
        }
    }
    fn dir(name: &str) -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let dir = std::env::temp_dir().join(format!(
            "ai-artifacts-retention-{name}-{}-{nanos}",
            std::process::id()
        ));
        fs::create_dir_all(&dir).unwrap();
        dir
    }
    fn open(dir: &Path) -> (Store, Ledger) {
        let store = Store::open(&dir.join("store"), &dir.join("revocation.mark")).unwrap();
        let ledger = Ledger::open(&dir.join("store"), &dir.join("retention.mark")).unwrap();
        (store, ledger)
    }

    /// A public INPUT payload of two chunks whose manifest subject is `task`.
    fn payload(fill: u8) -> Vec<u8> {
        vec![fill; CHUNK_BYTES as usize + 4_096]
    }
    fn manifest(bytes: &[u8], task: u8) -> Vec<u8> {
        let count = chunk_count(bytes.len() as u64).unwrap();
        let mut scratch = vec![[0u8; 32]; count as usize];
        let manifest = ArtifactManifest {
            kind: ArtifactKind::Input,
            privacy: Privacy::Public,
            context: ctx(),
            epoch: 9,
            publisher: PrincipalId::new([0x55; 32]).unwrap(),
            subject: [task; 32],
            byte_length: bytes.len() as u64,
            chunk_count: count,
            content_root: object_content_root(bytes, &mut scratch).unwrap(),
            parents: Items::Typed(&[]),
            declaration_root: [0; 32],
            reproduction_root: [0; 32],
            access_policy_root: [0; 32],
            not_after_height: 0,
        };
        let mut out = vec![0; MAX_MANIFEST_BYTES];
        let n = encode_manifest(&manifest, &mut out).unwrap();
        out.truncate(n);
        out
    }
    /// Durable chunks of one published session bound to `root`.
    fn session(store: &mut Store, id: u8, root: &str, bytes: &[u8]) -> String {
        let id = hex::encode([id; 32]);
        store.create_session_dir(&id).unwrap();
        for (i, chunk) in bytes.chunks(CHUNK_BYTES as usize).enumerate() {
            store.write_chunk(&id, i as u32, chunk).unwrap();
        }
        store.state.sessions.insert(
            id.clone(),
            Session {
                tenant: TENANT.to_string(),
                request_id: id.clone(),
                kind: ArtifactKind::Input as u8,
                privacy: Privacy::Public as u8,
                context: hex::encode(ctx().bytes()),
                byte_length: bytes.len() as u64,
                chunk_count: chunk_count(bytes.len() as u64).unwrap(),
                expected_root: None,
                created_at: 1,
                state: SessionState::Published,
                root: Some(root.to_string()),
                wrapped_key: None,
            },
        );
        id
    }
    /// An AVAILABLE object over its primary session; returns its root.
    fn publish(store: &mut Store, task: u8, bytes: &[u8]) -> String {
        let manifest = manifest(bytes, task);
        let root = hex::encode(manifest_root(&manifest).unwrap().bytes());
        let primary = session(store, task, &root, bytes);
        store.state.objects.insert(
            root.clone(),
            ObjectRecord {
                tenant: TENANT.to_string(),
                session: primary,
                manifest: hex::encode(&manifest),
                envelope: String::new(),
                declaration: None,
                declared_purposes: None,
                rights: None,
                record_digest: hex::encode([task; 32]),
                state: ObjectState::Available,
                privacy: Privacy::Public as u8,
                byte_length: bytes.len() as u64,
                chunk_count: chunk_count(bytes.len() as u64).unwrap(),
                publisher: hex::encode([0x55; 32]),
                generation: 1,
                wrapped_key: None,
                access_generation: 1,
                published_at: 1,
                tombstone_reason: None,
                retention_until: None,
                history: vec![Transition {
                    state: ObjectState::Available,
                    at: 1,
                }],
            },
        );
        store.persist().unwrap();
        root
    }

    fn encode_shared(shared: &SharedState<'_>) -> Vec<u8> {
        let mut out = vec![0; shared.encoded_len().unwrap()];
        let mut scratch = vec![0; Section::Control.payload_cap()];
        let n = state::encode_shared_state(shared, &mut out, &mut scratch).unwrap();
        out.truncate(n);
        out
    }
    /// Real F01 CREATE through `registry_ops::apply`.
    fn created_state() -> Vec<u8> {
        let policy = TaskPolicyV1::bounded_default(
            1,
            1,
            PolicyCommitments {
                model_artifact: digest(1),
                dataset_artifact: [2; 32],
                benchmark_suite: digest(3),
                rubric: RubricDigest::new([4; 32]).unwrap(),
                task_schema: digest(5),
                result_schema: digest(6),
                service_terms: digest(7),
            },
            100,
            1,
        )
        .unwrap();
        let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
        policy.encode(&mut encoded_policy).unwrap();
        let asset = [13; 32];
        let rewards = derive_rewards_account(program(), AssetId::new(asset).unwrap()).unwrap();
        let mut payload = [12; 32].to_vec();
        payload.extend_from_slice(&asset);
        payload.extend_from_slice(rewards.as_bytes());
        payload.extend_from_slice(&[14; 32]);
        payload.push(0);
        payload.extend_from_slice(&encoded_policy);
        payload.extend_from_slice(&[16; 32]);
        let owner = PrincipalId::new([12; 32]).unwrap();
        let envelope = Envelope {
            operation: dispatch::CREATE,
            chain: chain(),
            program: program(),
            market: derive_market(chain(), program()).unwrap(),
            actor: owner,
            epoch: 0,
            config: 1,
            roster: Presence::Absent,
            sequence: 1,
            expiry: 1_000_000,
            request: RequestId::new([1; 32]).unwrap(),
            payload: &payload,
            authentication: Authentication::Native,
        };
        let mut encoded = vec![0; 16_384];
        let n = encode_envelope(&envelope, &mut encoded).unwrap();
        let validated = decode_envelope(&encoded[..n]).unwrap();
        let call = CallContext {
            chain: chain(),
            program: program(),
            principal: owner,
            height: 600,
        };
        let mut section = vec![0; F01_SECTION_CAP];
        let mut event = vec![0; MAX_EVENT_BYTES];
        match registry_ops::apply(&call, None, &validated, &mut section, &mut event).unwrap() {
            Outcome::Applied { state, .. } => encode_shared(&state),
            Outcome::AlreadyApplied(_) => panic!("fresh create retried"),
        }
    }
    /// The created market whose task region holds `tasks` (id, status, input).
    fn market(tasks: &[(u8, TaskStatus, &str)]) -> Vec<u8> {
        let mut sorted: Vec<TaskBinding> = tasks
            .iter()
            .map(|(id, status, root)| TaskBinding {
                task: TaskId::new([*id; 32]).unwrap(),
                requester: PrincipalId::new([0x61; 32]).unwrap(),
                worker: WorkerId::new([0x62; 32]).unwrap(),
                input: Digest32::new(root_bytes(root).unwrap()).unwrap(),
                deadline: 5_000,
                status: *status,
                acknowledgement: (*status != TaskStatus::Cancelled).then(|| digest(0x63)),
                result: None,
                admission: digest(0x65),
            })
            .collect();
        sorted.sort_by_key(|t| t.task);
        let mut region = u16::try_from(sorted.len()).unwrap().to_be_bytes().to_vec();
        region.push(0);
        for binding in &sorted {
            region.extend_from_slice(&binding.encode().unwrap());
        }
        let base = created_state();
        let shared = decode_shared_state(&base).unwrap();
        let mut section =
            PolicySection::decode(shared.section(Section::PolicyLifecycle).unwrap()).unwrap();
        section.task_region = &region;
        let mut bytes = vec![0; Section::PolicyLifecycle.payload_cap()];
        let n = section.encode(&mut bytes).unwrap();
        encode_shared(
            &shared
                .replace_section(Section::PolicyLifecycle, &bytes[..n])
                .unwrap(),
        )
    }
    /// The state head read at `height` with the block commit `commit` at that
    /// height over its native state root, bound as the service binds it.
    fn head(state: &[u8], height: u64, commit: u8, rank: u8) -> SnapshotBinding {
        let proof = ReadProof {
            chain: chain(),
            program: program(),
            native_state_root: digest(0x5a),
            observed_sequence: height + 7,
            execution_height: height,
            batch_id: digest(0xbb),
        };
        let facts = CaptureFacts {
            proof,
            revision: decode_shared_state(state).unwrap().revision,
            digest: codec::state_digest(state).unwrap(),
            total_bytes: u32::try_from(state.len()).unwrap(),
            chunks: state.len().div_ceil(MAX_CHUNK_BYTES),
        };
        let finality = FinalityEvidence {
            native_state_root: proof.native_state_root,
            checkpoint: digest(commit),
            settlement: Presence::Absent,
            rank,
        };
        bind_snapshot(state, &facts, &finality, 1_700_000_000_000).unwrap()
    }

    #[test]
    fn state_head_finality_is_the_commit_at_its_height() {
        let state = market(&[]);
        assert_eq!(
            CommitFinality::of(&head(&state, 700, 0xc7, 4)),
            Ok(CommitFinality {
                height: 700,
                commit: digest(0xc7),
            })
        );
        assert_eq!(
            CommitFinality::of(&head(&state, 700, 0xc7, 3)),
            Err(Refusal::FinalityUnavailable)
        );
        assert_eq!(
            CommitFinality::of(&head(&state, 0, 0xc7, 4)),
            Err(Refusal::FinalityUnavailable)
        );
    }

    #[test]
    fn sealed_digest_promoted_at_its_commit_height() {
        let dir = dir("commit-height");
        let (mut store, mut ledger) = open(&dir);
        let bound = publish(&mut store, 0x31, &payload(0x31));
        let ended = publish(&mut store, 0x32, &payload(0x32));
        let state = market(&[
            (0x31, TaskStatus::Accepted, bound.as_str()),
            (0x32, TaskStatus::Cancelled, ended.as_str()),
        ]);

        assert_eq!(
            ledger.observe_binding(&mut store, &bound, &head(&state, 640, 0xc6, 3), &state),
            Ok(Binding::Pending)
        );
        assert_eq!(ledger.entry(&bound).unwrap().committed_height, None);
        assert_eq!(
            ledger.observe_binding(&mut store, &bound, &head(&state, 640, 0xc6, 4), &state),
            Ok(Binding::Finalized)
        );
        assert_eq!(ledger.entry(&bound).unwrap().committed_height, Some(640));
        assert_eq!(
            ledger.observe_binding(&mut store, &bound, &head(&state, 700, 0xc7, 4), &state),
            Ok(Binding::Finalized)
        );
        assert_eq!(ledger.entry(&bound).unwrap().committed_height, Some(640));
        assert_eq!(
            ledger.orphan_binding(&mut store, &bound),
            Err(Refusal::Artifact(ArtifactError::IntegrityConflict))
        );
        assert_eq!(
            ledger.projection(&store, &bound),
            Ok(Projection {
                state: "AVAILABLE",
                availability: "AVAILABLE",
                binding: "FINALIZED",
                tombstone_reason: None,
                tombstone_version: 0,
                retain_until: None,
            })
        );

        let at = 1_000 * DAY;
        assert_eq!(
            ledger.observe_terminal(&mut store, &ended, &head(&state, 640, 0xc6, 3), &state, at),
            Err(Refusal::FinalityUnavailable)
        );
        assert_eq!(
            ledger.observe_terminal(&mut store, &ended, &head(&state, 640, 0xc6, 4), &state, at),
            Ok(at + RETENTION_SECS)
        );
        let terminal = ledger.entry(&ended).unwrap().terminal.clone().unwrap();
        assert_eq!(
            (terminal.height, terminal.commit, terminal.retain_until),
            (640, hex::encode([0xc6; 32]), at + RETENTION_SECS)
        );

        drop((store, ledger));
        let (store, ledger) = open(&dir);
        assert_eq!(ledger.entry(&bound).unwrap().committed_height, Some(640));
        assert_eq!(
            ledger
                .entry(&ended)
                .unwrap()
                .terminal
                .as_ref()
                .map(|t| t.height),
            Some(640)
        );
        assert_eq!(
            ledger.projection(&store, &bound).unwrap().binding,
            "FINALIZED"
        );
    }

    #[test]
    fn missing_primary_payload_is_restored_from_a_replica() {
        let dir = dir("replica");
        let (mut store, mut ledger) = open(&dir);
        let bytes = payload(0x41);
        let root = publish(&mut store, 0x41, &bytes);
        let replica = session(&mut store, 0x42, &root, &bytes);
        let lonely = publish(&mut store, 0x43, &payload(0x43));
        store.persist().unwrap();
        let primary = store.state.objects[&root].session.clone();
        let staging = dir.join("store").join("staging");
        fs::remove_file(staging.join(&primary).join("1")).unwrap();
        fs::remove_file(
            staging
                .join(store.state.objects[&lonely].session.clone())
                .join("0"),
        )
        .unwrap();

        for missing in [&root, &lonely] {
            assert_eq!(
                ledger.projection(&store, missing).unwrap().availability,
                "EVIDENCE_UNAVAILABLE"
            );
        }
        assert_eq!(ledger.recover(&mut store), Ok(0));
        let record = &store.state.objects[&root];
        assert_eq!(record.session, replica);
        let manifest = hex::decode(&record.manifest).unwrap();
        let manifest = decode_manifest(&manifest).unwrap();
        assert_eq!(store.verify_content(&record.session, &manifest), Ok(()));
        let mut scratch = vec![[0u8; 32]; manifest.chunk_count as usize];
        assert_eq!(
            object_content_root(&bytes, &mut scratch).unwrap(),
            manifest.content_root
        );
        assert_eq!(
            ledger.projection(&store, &root).unwrap().availability,
            "AVAILABLE"
        );
        assert_eq!(
            ledger.projection(&store, &lonely).unwrap().availability,
            "EVIDENCE_UNAVAILABLE"
        );

        let (store, _) = open(&dir);
        assert_eq!(store.state.objects[&root].session, replica);
        let log = fs::read_to_string(dir.join("store").join("events.log")).unwrap();
        assert!(log.contains(&format!("restored root={root} replica={replica}")));
        assert!(log.contains(&format!("payload-missing root={lonely}")));
    }
}
