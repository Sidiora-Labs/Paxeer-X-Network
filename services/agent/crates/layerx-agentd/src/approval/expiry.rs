//! Durable approval expiry, idempotency and concurrency arbitration.

use std::path::Path;
use std::sync::{Arc, Mutex, MutexGuard};

use crate::budget::{self, BudgetLimiter, ReleaseKind};
use crate::policy::approval::{ApprovalSnapshot, ApprovalState};
use crate::store::{ObjectKind, Store, TenantId, TenantKey};

use super::{decision, ApprovalDecision, ApprovalOutcome};

const MAGIC: &[u8; 5] = b"LXAP1";
const KEY_PREFIX: &[u8] = b"approval-v1:";

/// Required idempotency key for one approve or reject operation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecisionKey(String);

impl DecisionKey {
    /// Constructs a non-empty bounded idempotency key.
    ///
    /// # Errors
    ///
    /// Refuses empty values, values longer than 255 bytes, and embedded NUL bytes.
    pub fn new(value: impl Into<String>) -> Result<Self, ApprovalExpiryError> {
        let value = value.into();
        if value.is_empty() || value.len() > 255 || value.as_bytes().contains(&0) {
            return Err(ApprovalExpiryError::InvalidIdempotencyKey);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// File-backed approval state machine used across process restarts.
pub struct ApprovalExpiry {
    store: Arc<Mutex<Store>>,
    decisions: Mutex<()>,
}

impl ApprovalExpiry {
    #[cfg(test)]
    pub(super) fn decision_is_locked(&self) -> bool {
        matches!(
            self.decisions.try_lock(),
            Err(std::sync::TryLockError::WouldBlock)
        )
    }

    pub(crate) fn lock_decisions(&self) -> Result<MutexGuard<'_, ()>, ApprovalExpiryError> {
        self.decisions
            .lock()
            .map_err(|_| ApprovalExpiryError::Store)
    }

    pub(crate) fn repeated(
        &self,
        tenant: &TenantId,
        approval_id: [u8; 32],
        idempotency_key: &DecisionKey,
    ) -> Result<Option<ApprovalDecision>, ApprovalExpiryError> {
        let store = self.store.lock().map_err(|_| ApprovalExpiryError::Store)?;
        let key = storage_key(tenant, approval_id)?;
        let Some(value) = store.get(&key) else {
            return Ok(None);
        };
        let persisted = decode(value.bytes())?;
        match (persisted.outcome, persisted.idempotency_key.as_deref()) {
            (Some(outcome), Some(key)) if key == idempotency_key.as_str() => {
                Ok(Some(ApprovalDecision {
                    outcome,
                    submission_ref: persisted.submission_ref,
                    winning_outcome: None,
                    enforcement: super::ApprovalEnforcement::DaemonOnly,
                    authority_notice: super::APPROVAL_ENFORCEMENT_NOTICE,
                }))
            }
            (Some(outcome), _) => Ok(Some(super::conflict(outcome))),
            (None, _) => Ok(None),
        }
    }

    pub(crate) fn persist_prepared_decision(
        &self,
        prepared: PreparedDecision,
    ) -> Result<(), ApprovalExpiryError> {
        let mut store = self.store.lock().map_err(|_| ApprovalExpiryError::Store)?;
        prepared.check_pending(&store)?;
        store
            .put_local(prepared.key, prepared.bytes)
            .map_err(|_| ApprovalExpiryError::Store)
    }

    /// Opens the real daemon store at `root`.
    ///
    /// # Errors
    ///
    /// Returns a storage failure when the store cannot be opened or migrated.
    pub fn open(root: impl AsRef<Path>) -> Result<Self, ApprovalExpiryError> {
        Ok(Self {
            decisions: Mutex::new(()),
            store: Arc::new(Mutex::new(
                Store::open(root).map_err(|_| ApprovalExpiryError::Store)?,
            )),
        })
    }

    /// Uses the daemon's sole durable store owner.
    #[must_use]
    pub fn from_shared_store(store: Arc<Mutex<Store>>) -> Self {
        Self {
            store,
            decisions: Mutex::new(()),
        }
    }

    pub(crate) fn observe(
        &self,
        snapshot: &ApprovalSnapshot,
        current_sequence: u64,
        limiter: &BudgetLimiter,
    ) -> Result<ApprovalState, ApprovalExpiryError> {
        let mut store = self.store.lock().map_err(|_| ApprovalExpiryError::Store)?;
        if super::program_requirement::has_bound_approval(&store, snapshot)
            .map_err(|_| ApprovalExpiryError::Corrupt)? {
            return observe_program(&mut store, snapshot, current_sequence, limiter);
        }
        let key = storage_key(&snapshot.context.tenant, snapshot.context.request_id)?;
        let mut persisted = match store.get(&key) {
            Some(value) => decode(value.bytes())?,
            None => PersistedApproval::pending(snapshot.expires_at_sequence),
        };
        if persisted.expires_at_sequence != snapshot.expires_at_sequence {
            return Err(ApprovalExpiryError::ExpiryMismatch);
        }
        if persisted.outcome.is_none() && current_sequence >= persisted.expires_at_sequence {
            persisted.outcome = Some(ApprovalOutcome::Expired);
            let bytes = encode(&persisted)?;
            let staged = budget::stage_release(
                limiter,
                snapshot.context.request_id,
                ReleaseKind::Expired,
                current_sequence,
            )
            .map_err(|_| ApprovalExpiryError::Reservation)?;
            store
                .put_local(key, bytes)
                .map_err(|_| ApprovalExpiryError::Store)?;
            let _ = staged.publish();
            return Ok(ApprovalState::Expired);
        }
        if store.get(&key).is_none() {
            store
                .put_local(key, encode(&persisted)?)
                .map_err(|_| ApprovalExpiryError::Store)?;
        }
        Ok(state_for(persisted.outcome, snapshot.state))
    }

    pub(crate) fn decide(
        &self,
        snapshot: &ApprovalSnapshot,
        current_sequence: u64,
        idempotency_key: &DecisionKey,
        intended: ApprovalOutcome,
        submission_ref: Option<[u8; 32]>,
        limiter: &BudgetLimiter,
    ) -> Result<DecisionResolution, ApprovalExpiryError> {
        let observed = self.observe(snapshot, current_sequence, limiter)?;
        if observed == ApprovalState::Expired {
            return Ok(DecisionResolution::Expired);
        }
        let mut store = self.store.lock().map_err(|_| ApprovalExpiryError::Store)?;
        let key = storage_key(&snapshot.context.tenant, snapshot.context.request_id)?;
        let mut persisted = store
            .get(&key)
            .map(|value| decode(value.bytes()))
            .transpose()?
            .unwrap_or_else(|| PersistedApproval::pending(snapshot.expires_at_sequence));
        if let Some(winner) = persisted.outcome {
            if persisted.idempotency_key.as_deref() == Some(idempotency_key.as_str()) {
                return Ok(DecisionResolution::Repeat(ApprovalDecision {
                    outcome: winner,
                    submission_ref: persisted.submission_ref,
                    winning_outcome: None,
                    enforcement: super::ApprovalEnforcement::DaemonOnly,
                    authority_notice: super::APPROVAL_ENFORCEMENT_NOTICE,
                }));
            }
            return Ok(DecisionResolution::Conflict(winner));
        }
        let expected = store.get(&key).map(|value| value.bytes().to_vec());
        persisted.idempotency_key = Some(idempotency_key.as_str().to_owned());
        persisted.outcome = Some(intended);
        persisted.submission_ref = submission_ref;
        let bytes = encode(&persisted)?;
        if matches!(
            intended,
            ApprovalOutcome::Granted | ApprovalOutcome::Rejected | ApprovalOutcome::Defective
        ) {
            return Ok(DecisionResolution::WinnerPrepared(PreparedDecision {
                key,
                expected,
                bytes,
            }));
        }
        store
            .put_local(key, bytes)
            .map_err(|_| ApprovalExpiryError::Store)?;
        Ok(DecisionResolution::Winner)
    }

    /// Recovers a known hold and expires it if its deadline elapsed while offline.
    ///
    /// # Errors
    ///
    /// Returns storage, encoding, expiry-consistency, or reservation-release failures.
    pub fn recover(
        &self,
        tenant: &TenantId,
        approval_id: [u8; 32],
        expires_at_sequence: u64,
        current_sequence: u64,
        limiter: &BudgetLimiter,
    ) -> Result<ApprovalDecision, ApprovalExpiryError> {
        let _decision = self.lock_decisions()?;
        let mut store = self.store.lock().map_err(|_| ApprovalExpiryError::Store)?;
        if let Some(snapshot) = crate::policy::approval::program_recovery_snapshot(&store, tenant, approval_id)
            .map_err(|_| ApprovalExpiryError::Corrupt)? {
            if snapshot.expires_at_sequence != expires_at_sequence {
                return Err(ApprovalExpiryError::ExpiryMismatch);
            }
            let state = observe_program(&mut store, &snapshot, current_sequence, limiter)?;
            return if state == ApprovalState::AwaitingApproval {
                Ok(decision(ApprovalOutcome::AlreadyDecided, None))
            } else {
                Self::validate_program_terminal(&store, &snapshot)
            };
        }
        let key = storage_key(tenant, approval_id)?;
        let Some(value) = store.get(&key) else {
            return Err(ApprovalExpiryError::NotFound);
        };
        let mut persisted = decode(value.bytes())?;
        if persisted.expires_at_sequence != expires_at_sequence {
            return Err(ApprovalExpiryError::ExpiryMismatch);
        }
        if persisted.outcome.is_none() && current_sequence >= expires_at_sequence {
            persisted.outcome = Some(ApprovalOutcome::Expired);
            let bytes = encode(&persisted)?;
            let staged =
                budget::stage_release(limiter, approval_id, ReleaseKind::Expired, current_sequence)
                    .map_err(|_| ApprovalExpiryError::Reservation)?;
            store
                .put_local(key, bytes)
                .map_err(|_| ApprovalExpiryError::Store)?;
            let _ = staged.publish();
            return Ok(decision(ApprovalOutcome::Expired, None));
        }
        let outcome = persisted.outcome.unwrap_or(ApprovalOutcome::AlreadyDecided);
        Ok(decision(outcome, persisted.submission_ref))
    }

    pub(crate) fn decision_store(&self) -> &Mutex<Store> {
        &self.store
    }
}

#[derive(Clone)]
pub(crate) struct PreparedDecision {
    key: TenantKey,
    expected: Option<Vec<u8>>,
    bytes: Vec<u8>,
}

impl PreparedDecision {
    fn check_pending(&self, store: &Store) -> Result<(), ApprovalExpiryError> {
        let current = store.get(&self.key).map(crate::store::StoredValue::bytes);
        if current != self.expected.as_deref() {
            return Err(ApprovalExpiryError::DecisionConflict);
        }
        if let Some(bytes) = current {
            if decode(bytes)?.outcome.is_some() {
                return Err(ApprovalExpiryError::DecisionConflict);
            }
        }
        Ok(())
    }

    pub(crate) fn replace_hold(
        self,
        store: &mut Store,
        hold_key: &TenantKey,
        released_key: TenantKey,
        released_bytes: Vec<u8>,
    ) -> Result<(), ApprovalExpiryError> {
        self.check_pending(store)?;
        store
            .replace_local_with_companion(
                hold_key,
                released_key,
                released_bytes,
                self.key,
                self.bytes,
            )
            .map_err(|_| ApprovalExpiryError::Store)
    }

    pub(crate) fn remove_hold(
        self,
        store: &mut Store,
        hold_key: TenantKey,
    ) -> Result<(), ApprovalExpiryError> {
        self.check_pending(store)?;
        store
            .update_local_batch_removing(vec![(self.key, self.bytes)], vec![hold_key])
            .map_err(|_| ApprovalExpiryError::Store)
    }

    pub(crate) fn persist(self, store: &mut Store) -> Result<(), ApprovalExpiryError> {
        self.check_pending(store)?;
        store
            .put_local(self.key, self.bytes)
            .map_err(|_| ApprovalExpiryError::Store)
    }
}

pub(crate) enum DecisionResolution {
    Winner,
    WinnerPrepared(PreparedDecision),
    Repeat(ApprovalDecision),
    Conflict(ApprovalOutcome),
    Expired,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalExpiryError {
    InvalidIdempotencyKey,
    Store,
    Corrupt,
    NotFound,
    ExpiryMismatch,
    Reservation,
    DecisionConflict,
}

struct PersistedApproval {
    expires_at_sequence: u64,
    idempotency_key: Option<String>,
    outcome: Option<ApprovalOutcome>,
    submission_ref: Option<[u8; 32]>,
}

impl PersistedApproval {
    const fn pending(expires_at_sequence: u64) -> Self {
        Self {
            expires_at_sequence,
            idempotency_key: None,
            outcome: None,
            submission_ref: None,
        }
    }
}

fn storage_key(tenant: &TenantId, approval_id: [u8; 32]) -> Result<TenantKey, ApprovalExpiryError> {
    let mut object_id = Vec::with_capacity(KEY_PREFIX.len() + approval_id.len());
    object_id.extend_from_slice(KEY_PREFIX);
    object_id.extend_from_slice(&approval_id);
    TenantKey::new(tenant.clone(), ObjectKind::Idempotency, object_id)
        .map_err(|_| ApprovalExpiryError::Store)
}

fn encode(record: &PersistedApproval) -> Result<Vec<u8>, ApprovalExpiryError> {
    let key = record.idempotency_key.as_deref().unwrap_or_default();
    let key_length = u8::try_from(key.len()).map_err(|_| ApprovalExpiryError::Corrupt)?;
    let mut bytes = Vec::with_capacity(MAGIC.len() + 43 + key.len());
    bytes.extend_from_slice(MAGIC);
    bytes.extend_from_slice(&record.expires_at_sequence.to_be_bytes());
    bytes.push(outcome_code(record.outcome));
    bytes.push(key_length);
    bytes.extend_from_slice(key.as_bytes());
    match record.submission_ref {
        Some(reference) => {
            bytes.push(1);
            bytes.extend_from_slice(&reference);
        }
        None => bytes.push(0),
    }
    Ok(bytes)
}

fn decode(bytes: &[u8]) -> Result<PersistedApproval, ApprovalExpiryError> {
    if bytes.len() < MAGIC.len() + 11 || &bytes[..MAGIC.len()] != MAGIC {
        return Err(ApprovalExpiryError::Corrupt);
    }
    let mut offset = MAGIC.len();
    let expires_at_sequence = u64::from_be_bytes(
        bytes[offset..offset + 8]
            .try_into()
            .map_err(|_| ApprovalExpiryError::Corrupt)?,
    );
    offset += 8;
    let outcome = decode_outcome(bytes[offset])?;
    offset += 1;
    let key_length = usize::from(bytes[offset]);
    offset += 1;
    if bytes.len() < offset + key_length + 1 {
        return Err(ApprovalExpiryError::Corrupt);
    }
    let idempotency_key = if key_length == 0 {
        None
    } else {
        let key = std::str::from_utf8(&bytes[offset..offset + key_length])
            .map_err(|_| ApprovalExpiryError::Corrupt)?;
        Some(key.to_owned())
    };
    offset += key_length;
    let submission_ref = match bytes[offset] {
        0 if bytes.len() == offset + 1 => None,
        1 if bytes.len() == offset + 33 => Some(
            bytes[offset + 1..]
                .try_into()
                .map_err(|_| ApprovalExpiryError::Corrupt)?,
        ),
        _ => return Err(ApprovalExpiryError::Corrupt),
    };
    Ok(PersistedApproval {
        expires_at_sequence,
        idempotency_key,
        outcome,
        submission_ref,
    })
}

const fn outcome_code(outcome: Option<ApprovalOutcome>) -> u8 {
    match outcome {
        None => 0,
        Some(ApprovalOutcome::Granted) => 1,
        Some(ApprovalOutcome::Rejected) => 2,
        Some(ApprovalOutcome::Expired) => 3,
        Some(ApprovalOutcome::Defective) => 4,
        Some(ApprovalOutcome::AlreadyDecided) => 5,
        Some(ApprovalOutcome::Conflict) => 6,
    }
}

const fn decode_outcome(code: u8) -> Result<Option<ApprovalOutcome>, ApprovalExpiryError> {
    match code {
        0 => Ok(None),
        1 => Ok(Some(ApprovalOutcome::Granted)),
        2 => Ok(Some(ApprovalOutcome::Rejected)),
        3 => Ok(Some(ApprovalOutcome::Expired)),
        4 => Ok(Some(ApprovalOutcome::Defective)),
        5 => Ok(Some(ApprovalOutcome::AlreadyDecided)),
        6 => Ok(Some(ApprovalOutcome::Conflict)),
        _ => Err(ApprovalExpiryError::Corrupt),
    }
}

const fn state_for(outcome: Option<ApprovalOutcome>, fallback: ApprovalState) -> ApprovalState {
    match outcome {
        Some(ApprovalOutcome::Granted) => ApprovalState::Approved,
        Some(ApprovalOutcome::Rejected) => ApprovalState::Rejected,
        Some(ApprovalOutcome::Expired) => ApprovalState::Expired,
        Some(ApprovalOutcome::Defective) => ApprovalState::Defective,
        _ => fallback,
    }
}

#[cfg(test)]
mod tests {
    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::ids::Did;
    use sha2::{Digest as _, Sha256};

    use super::{encode, storage_key, ApprovalExpiry, ApprovalExpiryError, PersistedApproval};
    use crate::budget::{
        self, BudgetLimiter, LimitConfig, LimitId, LimitScope, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::policy::approval::{
        hold_reserved, ApprovalContext, ApprovalRegistry, ApprovalSnapshot, ApprovalState,
    };
    use crate::session::SessionId;
    use crate::store::TenantId;

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("approval expiry: {error:?}"))
    }

    #[test]
    fn offline_expiry_publishes_the_staged_release_after_the_durable_write() {
        let root = std::env::temp_dir().join(format!("lxp-expiry-stage-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let expiry = must(ApprovalExpiry::open(&root));
        let tenant = must(TenantId::new("tenant-a"));
        let limiter = must(BudgetLimiter::new(vec![LimitConfig {
            id: LimitId([1; 16]),
            name: "tenant-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 100,
            consumed: 0,
        }]));
        must(budget::reserve(
            &limiter,
            &ReservationRequest {
                id: [7; 32],
                amount: 40,
                expiry_sequence: 5,
                current_sequence: 1,
                applicable_limits: vec![LimitId([1; 16])],
            },
        ));
        {
            let mut store = must(expiry.store.lock());
            must(store.put_local(
                must(storage_key(&tenant, [7; 32])),
                must(encode(&PersistedApproval::pending(5))),
            ));
        }
        assert!(expiry.recover(&tenant, [7; 32], 5, 4, &limiter).is_ok());
        assert_eq!(limiter.held_limits([7; 32]), Ok(vec![LimitId([1; 16])]));
        assert!(expiry.recover(&tenant, [7; 32], 5, 5, &limiter).is_ok());
        assert_eq!(limiter.held_reservations(), Ok(0));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        let _ = std::fs::remove_dir_all(root);
    }

    fn fixture(name: &str) -> (std::path::PathBuf, ApprovalExpiry, TenantId, BudgetLimiter) {
        let root = std::env::temp_dir().join(format!("lxp-expiry-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let expiry = must(ApprovalExpiry::open(&root));
        let tenant = must(TenantId::new("tenant-a"));
        let limiter = must(BudgetLimiter::new(vec![LimitConfig {
            id: LimitId([1; 16]),
            name: "tenant-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 100,
            consumed: 0,
        }]));
        must(budget::reserve(
            &limiter,
            &ReservationRequest {
                id: [7; 32],
                amount: 40,
                expiry_sequence: 5,
                current_sequence: 1,
                applicable_limits: vec![LimitId([1; 16])],
            },
        ));
        {
            let mut store = must(expiry.store.lock());
            must(store.put_local(
                must(storage_key(&tenant, [7; 32])),
                must(encode(&PersistedApproval::pending(5))),
            ));
        }
        (root, expiry, tenant, limiter)
    }

    fn persisted_bytes(expiry: &ApprovalExpiry, tenant: &TenantId) -> Option<Vec<u8>> {
        must(expiry.store.lock())
            .get(&must(storage_key(tenant, [7; 32])))
            .map(|value| value.bytes().to_vec())
    }

    #[test]
    fn offline_expiry_with_a_failed_durable_write_keeps_the_hold() {
        let (root, expiry, tenant, limiter) = fixture("recover-io-failure");
        let before = persisted_bytes(&expiry, &tenant);
        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            expiry.recover(&tenant, [7; 32], 5, 5, &limiter),
            Err(ApprovalExpiryError::Store)
        ));
        assert_eq!(limiter.held_limits([7; 32]), Ok(vec![LimitId([1; 16])]));
        assert_eq!(limiter.held_reservations(), Ok(1));
        assert_eq!(limiter.held_exposure(LimitId([1; 16])), Ok(40));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        assert_eq!(persisted_bytes(&expiry, &tenant), before);
        assert!(!root.exists());

        must(std::fs::create_dir_all(&root));
        assert!(expiry.recover(&tenant, [7; 32], 5, 5, &limiter).is_ok());
        assert_eq!(limiter.held_reservations(), Ok(0));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        let expired = persisted_bytes(&expiry, &tenant);
        assert_ne!(expired, before);
        assert!(expiry.recover(&tenant, [7; 32], 5, 6, &limiter).is_ok());
        assert_eq!(persisted_bytes(&expiry, &tenant), expired);
        assert_eq!(limiter.held_reservations(), Ok(0));
        let _ = std::fs::remove_dir_all(root);
    }

    fn snapshot(tenant: &TenantId) -> ApprovalSnapshot {
        let bytes = b"approval-expiry-observe".to_vec();
        let digest: [u8; 32] = Sha256::digest(&bytes).into();
        let actor = "did:layerx:approval-expiry";
        let prepared = Prepared {
            preparation_ref: must(PreparationRef::new("approval-expiry-observe")),
            unsigned_canonical_bytes: must(CanonicalBytes::new(bytes)),
            signing_preimage: must(SigningPreimage::new(vec![7; 32])),
            disclosure: Disclosure {
                canonical_digest: digest,
                activity_type: ActivityType(7),
                actor: must(AgentDid::new(actor)),
                authority: must(AuthorityRef::new("session-key")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: must(Asset::new("LXP")),
                fee_limit: Amount(2),
                expiry: TimestampSeconds(40),
                idempotency_key: must(IdempotencyRef::new("07".repeat(32))),
            },
            expiry: TimestampSeconds(40),
        };
        let registry = ApprovalRegistry::default();
        let limiter = must(BudgetLimiter::new(vec![LimitConfig {
            id: LimitId([1; 16]),
            name: "tenant-limit".to_owned(),
            scope: LimitScope::Tenant([1; 32]),
            ceiling: 100,
            consumed: 0,
        }]));
        let reservation = must(budget::reserve(
            &limiter,
            &ReservationRequest {
                id: [7; 32],
                amount: 40,
                expiry_sequence: 5,
                current_sequence: 1,
                applicable_limits: vec![LimitId([1; 16])],
            },
        ));
        must(hold_reserved(
            &registry,
            ApprovalContext {
                tenant: tenant.clone(),
                agent: must(Did::new(actor.as_bytes())),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [7; 32],
            },
            prepared,
            1,
            5,
            &reservation,
        ));
        must(registry.get_scoped(tenant, [7; 32], 1))
    }

    #[test]
    fn observed_expiry_with_a_failed_durable_write_keeps_the_hold() {
        let (root, expiry, tenant, limiter) = fixture("observe-io-failure");
        let snapshot = snapshot(&tenant);
        let before = persisted_bytes(&expiry, &tenant);
        must(std::fs::remove_dir_all(&root));
        assert!(matches!(
            expiry.observe(&snapshot, 5, &limiter),
            Err(ApprovalExpiryError::Store)
        ));
        assert_eq!(limiter.held_limits([7; 32]), Ok(vec![LimitId([1; 16])]));
        assert_eq!(limiter.held_reservations(), Ok(1));
        assert_eq!(limiter.held_exposure(LimitId([1; 16])), Ok(40));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        assert_eq!(persisted_bytes(&expiry, &tenant), before);
        assert!(!root.exists());

        must(std::fs::create_dir_all(&root));
        assert!(matches!(
            expiry.observe(&snapshot, 5, &limiter),
            Ok(ApprovalState::Expired)
        ));
        assert_eq!(limiter.held_reservations(), Ok(0));
        assert_eq!(limiter.consumed(LimitId([1; 16])), Ok(0));
        let expired = persisted_bytes(&expiry, &tenant);
        assert_ne!(expired, before);
        assert!(matches!(
            expiry.observe(&snapshot, 6, &limiter),
            Ok(ApprovalState::Expired)
        ));
        assert_eq!(persisted_bytes(&expiry, &tenant), expired);
        let _ = std::fs::remove_dir_all(root);
    }
}

impl PreparedDecision {
    pub(crate) fn program_terminal(
        store: &Store,
        snapshot: &ApprovalSnapshot,
        idempotency_key: &DecisionKey,
        outcome: ApprovalOutcome,
        submission_ref: Option<[u8; 32]>,
    ) -> Result<Self, ApprovalExpiryError> {
        super::program_requirement::read_approval(store, snapshot)
            .map_err(|_| ApprovalExpiryError::Corrupt)?;
        let key = storage_key(&snapshot.context.tenant, snapshot.context.request_id)?;
        let expected = store.get(&key).map(|value| value.bytes().to_vec());
        if let Some(bytes) = &expected {
            let existing = decode(bytes)?;
            if existing.expires_at_sequence != snapshot.expires_at_sequence {
                return Err(ApprovalExpiryError::ExpiryMismatch);
            }
            if existing.outcome.is_some() {
                return Err(ApprovalExpiryError::DecisionConflict);
            }
        }
        if !matches!(outcome, ApprovalOutcome::Granted | ApprovalOutcome::Rejected
            | ApprovalOutcome::Expired | ApprovalOutcome::Defective)
            || (outcome == ApprovalOutcome::Granted) != submission_ref.is_some()
        {
            return Err(ApprovalExpiryError::Corrupt);
        }
        let bytes = encode(&PersistedApproval {
            expires_at_sequence: snapshot.expires_at_sequence,
            idempotency_key: Some(idempotency_key.as_str().to_owned()),
            outcome: Some(outcome),
            submission_ref,
        })?;
        Ok(Self { key, expected, bytes })
    }

    pub(crate) fn apply_program_terminal(
        self,
        store: &mut Store,
        snapshot: &ApprovalSnapshot,
        context: Option<&crate::agent_rpc_peer::RpcOwnerContext<'_>>,
        sequence: u64,
        expected_hold: &[u8],
        released: Option<(TenantKey, Vec<u8>)>,
    ) -> Result<(), ApprovalExpiryError> {
        use super::program_requirement::{read_approval, Terminal};
        self.check_pending(store)?;
        let intended = decode(&self.bytes)?;
        if self.key != storage_key(&snapshot.context.tenant, snapshot.context.request_id)?
            || intended.expires_at_sequence != snapshot.expires_at_sequence {
            return Err(ApprovalExpiryError::Corrupt);
        }
        let outcome = match intended.outcome {
            Some(ApprovalOutcome::Granted) => Terminal::Granted,
            Some(ApprovalOutcome::Rejected) => Terminal::Rejected,
            Some(ApprovalOutcome::Expired) => Terminal::Expired,
            Some(ApprovalOutcome::Defective) => Terminal::Defective,
            _ => return Err(ApprovalExpiryError::Corrupt),
        };
        if (outcome == Terminal::Granted) != released.is_some() {
            return Err(ApprovalExpiryError::Corrupt);
        }
        let (record, durable) = read_approval(store, snapshot)
            .map_err(|_| ApprovalExpiryError::Corrupt)?;
        if durable.terminal() && outcome == Terminal::Granted {
            return Err(ApprovalExpiryError::DecisionConflict);
        }
        let key_text = intended.idempotency_key.as_deref().ok_or(ApprovalExpiryError::Corrupt)?;
        let updated = record.stage_terminal(
            snapshot.context.request_id, key_text, context, sequence, outcome, intended.submission_ref,
        ).map_err(|_| ApprovalExpiryError::DecisionConflict)?;
        let hold_key = crate::policy::approval::hold_storage_key(
            &snapshot.context.tenant, snapshot.context.request_id,
        ).map_err(|_| ApprovalExpiryError::Corrupt)?;
        let held = store.get(&hold_key).ok_or(ApprovalExpiryError::NotFound)?;
        if held.class() != crate::store::StorageClass::LocalOnly || held.bytes() != expected_hold {
            return Err(ApprovalExpiryError::DecisionConflict);
        }
        crate::policy::approval::validate_hold_snapshot(expected_hold, snapshot)
            .map_err(|_| ApprovalExpiryError::Corrupt)?;
        let mut updates = vec![updated.companion().map_err(|_| ApprovalExpiryError::Corrupt)?];
        let mut inserts = Vec::new();
        if self.expected.is_some() {
            updates.push((self.key, self.bytes));
        } else {
            inserts.push((self.key, self.bytes));
        }
        if let Some((key, bytes)) = released {
            if key != crate::policy::approval::released_storage_key(
                &snapshot.context.tenant, snapshot.context.request_id,
            ).map_err(|_| ApprovalExpiryError::Corrupt)? {
                return Err(ApprovalExpiryError::Corrupt);
            }
            crate::policy::approval::validate_released_snapshot(
                &bytes, snapshot, intended.submission_ref.ok_or(ApprovalExpiryError::Corrupt)?,
            ).map_err(|_| ApprovalExpiryError::Corrupt)?;
            inserts.push((key, bytes));
        } else {
            let terminal_key = crate::policy::approval::program_terminal_storage_key(
                &snapshot.context.tenant, snapshot.context.request_id,
            ).map_err(|_| ApprovalExpiryError::Corrupt)?;
            inserts.push((terminal_key, expected_hold.to_vec()));
        }
        store.apply_program_approval_batch(updates, inserts, vec![hold_key])
            .map_err(|_| ApprovalExpiryError::Store)
    }
}

fn observe_program(
    store: &mut Store, snapshot: &ApprovalSnapshot, sequence: u64, limiter: &BudgetLimiter,
) -> Result<ApprovalState, ApprovalExpiryError> {
    use super::program_requirement::{read_approval, Terminal};
    let (requirement, _) = read_approval(store, snapshot).map_err(|_| ApprovalExpiryError::Corrupt)?;
    if let Some((outcome, decision_key, reference)) = requirement.terminal_decision()
        .map_err(|_| ApprovalExpiryError::Corrupt)? {
        let key = storage_key(&snapshot.context.tenant, snapshot.context.request_id)?;
        let persisted = decode(store.get(&key).ok_or(ApprovalExpiryError::NotFound)?.bytes())?;
        let (expected, state) = match outcome {
            Terminal::Granted => (ApprovalOutcome::Granted, ApprovalState::Approved),
            Terminal::Rejected => (ApprovalOutcome::Rejected, ApprovalState::Rejected),
            Terminal::Expired => (ApprovalOutcome::Expired, ApprovalState::Expired),
            Terminal::Defective => (ApprovalOutcome::Defective, ApprovalState::Defective),
        };
        if persisted.outcome != Some(expected) || persisted.idempotency_key.as_deref() != Some(decision_key)
            || persisted.submission_ref != reference || persisted.expires_at_sequence != snapshot.expires_at_sequence {
            return Err(ApprovalExpiryError::Corrupt);
        }
        return Ok(state);
    }
    if sequence < snapshot.expires_at_sequence { return Ok(ApprovalState::AwaitingApproval); }
    let hold_key = crate::policy::approval::hold_storage_key(&snapshot.context.tenant, snapshot.context.request_id)
        .map_err(|_| ApprovalExpiryError::Corrupt)?;
    let expected_hold = store.get(&hold_key).ok_or(ApprovalExpiryError::NotFound)?.bytes().to_vec();
    let reservations = crate::policy::approval::validate_hold_snapshot(&expected_hold, snapshot)
        .map_err(|_| ApprovalExpiryError::Corrupt)?;
    for reservation in reservations {
        limiter.consumed(reservation.limit_id).map_err(|_| ApprovalExpiryError::Reservation)?;
    }
    let decision_key = DecisionKey::new("program-expiry-v1")?;
    let terminal = PreparedDecision::program_terminal(store, snapshot, &decision_key, ApprovalOutcome::Expired, None)?;
    let staged = budget::stage_release(limiter, snapshot.context.request_id, ReleaseKind::Expired, sequence)
        .map_err(|_| ApprovalExpiryError::Reservation)?;
    terminal.apply_program_terminal(store, snapshot, None, sequence, &expected_hold, None)?;
    let _ = staged.publish();
    Ok(ApprovalState::Expired)
}

impl ApprovalExpiry {
    pub(crate) fn validate_program_terminal(
        store: &Store, snapshot: &ApprovalSnapshot,
    ) -> Result<ApprovalDecision, ApprovalExpiryError> {
        use super::program_requirement::{read_approval, Terminal};
        let (record, _) = read_approval(store, snapshot).map_err(|_| ApprovalExpiryError::Corrupt)?;
        let (terminal, decision_key, reference) = record.terminal_decision()
            .map_err(|_| ApprovalExpiryError::Corrupt)?.ok_or(ApprovalExpiryError::Corrupt)?;
        let outcome = match terminal {
            Terminal::Granted => ApprovalOutcome::Granted,
            Terminal::Rejected => ApprovalOutcome::Rejected,
            Terminal::Expired => ApprovalOutcome::Expired,
            Terminal::Defective => ApprovalOutcome::Defective,
        };
        let stored = store.get(&storage_key(&snapshot.context.tenant, snapshot.context.request_id)?)
            .ok_or(ApprovalExpiryError::NotFound)?;
        if stored.class() != crate::store::StorageClass::LocalOnly { return Err(ApprovalExpiryError::Corrupt); }
        let persisted = decode(stored.bytes())?;
        if persisted.expires_at_sequence != snapshot.expires_at_sequence
            || persisted.outcome != Some(outcome) || persisted.idempotency_key.as_deref() != Some(decision_key)
            || persisted.submission_ref != reference || encode(&persisted)?.as_slice() != stored.bytes() {
            return Err(ApprovalExpiryError::Corrupt);
        }
        Ok(decision(outcome, reference))
    }
}
