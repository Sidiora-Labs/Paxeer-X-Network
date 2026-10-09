//! Retention ledger beside the durable store: verified terminal-finality
//! timers, dispute/evidence holds, the binding projection, immutable tombstone
//! records, pending-purge accounting and restart reconciliation of deletion
//! intents. Terminal finality and bindings are read only from a
//! `SnapshotBinding` over the exact market state bytes it names; a missing
//! producer, record or authority fails closed and keeps the bytes. The ledger
//! has its own live high-water mark outside the restorable data directory.
use crate::store::{ObjectRecord, ObjectState, Store, RETENTION_SECS};
use layerx_programs_ai_market::codec;
use layerx_programs_ai_market::errors::ApplicationError;
use layerx_programs_ai_market::evidence::{decode_manifest, ArtifactError, ArtifactKind};
use layerx_programs_ai_market::queries::SnapshotBinding;
use layerx_programs_ai_market::registry_ops::PolicySection;
use layerx_programs_ai_market::state::{decode_shared_state, Section};
use layerx_programs_ai_market::tasks::{TaskBinding, TaskSet, TaskStatus};
use layerx_programs_ai_market::types::TaskId;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::ffi::OsString;
use std::fs::{self, File};
use std::io::Write;
use std::path::{Path, PathBuf};

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
    /// The snapshot is below verified finality (rank 4).
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

/// First verified terminal observation; it fixes day 0 and is never moved.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct Terminal {
    pub task: String,
    pub snapshot: String,
    pub observed_at: u64,
    pub retain_until: u64,
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
    pub purge_failures: u32,
    pub history: Vec<Event>,
}

impl Entry {
    /// Physical deletion deadline: the terminal floor raised by every hold;
    /// none while terminal finality is unverified or a binding is ambiguous.
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
    /// binding, after verified terminal finality plus 30 days, with no active hold.
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
    let bytes = hex::decode(&record.manifest).map_err(|_| ArtifactError::Malformed)?;
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

    /// Starts the 30-day terminal timer from a rank-4 snapshot in which the
    /// artifact's subject task is cancelled. Admitted or accepted tasks are
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
        snapshot
            .require_finalized()
            .map_err(|_| Refusal::FinalityUnavailable)?;
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

    /// The root bound by its subject task record (INPUT by the input digest,
    /// RESULT by the result digest) is FINALIZED only at rank 4; below it the
    /// binding stays pending. A finalized binding is never downgraded.
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
        let next = if snapshot.require_finalized().is_ok() {
            Binding::Finalized
        } else {
            Binding::Pending
        };
        let entry = self.entry_mut(root);
        if entry.binding == Binding::Finalized || entry.binding == next {
            return Ok(entry.binding);
        }
        entry.binding = next;
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
    /// their live marks; a tombstone intent the store never applied is
    /// completed; deletion deadlines are re-derived. Returns completed intents.
    pub fn recover(&mut self, store: &mut Store) -> Result<usize, Refusal> {
        if !store.restore_ready() || !self.ready() {
            return Err(Refusal::RestoreNotReady);
        }
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
        let availability = if record.state == ObjectState::Available {
            "AVAILABLE"
        } else if restricted {
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
