//! Concurrent, digest-bound approval holds with deterministic expiry.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
use layerx_agent_api::prepare::{
    CanonicalBytes, DisclosedAmount, Disclosure, IdempotencyRef, PreparationRef, Prepared,
    SigningPreimage,
};
use layerx_agent_api::{Amount, TimestampSeconds};
use layerx_types::ids::Did;
use layerx_types::verify::VerificationLevel;
use sha2::{Digest as _, Sha256};

use crate::budget::{
    self, BudgetLimiter, BudgetReservation, DurableBudgetReservation, LimitId, LimitScope,
};
use crate::capability::CapabilityId;
use crate::session::SessionId;
use crate::store::{ObjectKind, Store, TenantId, TenantKey};
use layerx_types::payload::ModuleRegistry;

const HOLD_KEY_PREFIX: &[u8] = b"approval-hold-v1:";
const RELEASED_KEY_PREFIX: &[u8] = b"approval-released-v1:";
const HOLD_MAGIC: &[u8; 8] = b"LXAPHLD2";
const RELEASED_MAGIC: &[u8; 8] = b"LXAPREL1";
const MAX_HOLD_RECORD_BYTES: usize = 2 * 1024 * 1024;
const MAX_DISCLOSURE_ITEMS: u32 = 64;

/// Complete audit context captured before an approval hold is created.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalContext {
    pub tenant: TenantId,
    pub agent: Did,
    pub session: SessionId,
    pub capability: CapabilityId,
    pub policy_version: String,
    pub request_id: [u8; 32],
}

/// Identity of the human or external approver.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApproverId(String);

impl ApproverId {
    /// Creates a non-empty approver identity.
    ///
    /// # Errors
    ///
    /// Returns `InvalidApprover` for an empty identifier.
    pub fn new(value: impl Into<String>) -> Result<Self, ApprovalError> {
        let value = value.into();
        if value.is_empty() {
            return Err(ApprovalError::InvalidApprover);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// Terminal choice submitted by an approver.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalChoice {
    Approve,
    Reject,
}

/// Explicit lifecycle for a held request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalState {
    AwaitingApproval,
    Approved,
    Rejected,
    Expired,
    Defective,
}

/// Approval audit entry carrying every required request dimension.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalAuditEntry {
    pub tenant: TenantId,
    pub agent: Did,
    pub session: SessionId,
    pub capability: CapabilityId,
    pub policy_version: String,
    pub request_id: [u8; 32],
    pub idempotency_key: String,
    pub decision: ApprovalState,
    pub reason: &'static str,
    pub resulting_activity_id: Option<[u8; 32]>,
    pub verification_level: VerificationLevel,
    pub approver: Option<ApproverId>,
    pub disclosure_digest: [u8; 32],
}

/// Disclosure-only view presented to an approver.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalTicket {
    pub hold_id: [u8; 32],
    pub disclosure: Disclosure,
    pub disclosure_digest: [u8; 32],
    pub expires_at_sequence: u64,
    pub state: ApprovalState,
}

#[derive(Clone, Debug)]
struct HeldApproval {
    context: ApprovalContext,
    prepared: Prepared,
    created_at_sequence: u64,
    expires_at_sequence: u64,
    state: ApprovalState,
    audit: Option<ApprovalAuditEntry>,
    decision_claimed: bool,
    budget_reservations: Vec<DurableBudgetReservation>,
}

/// Complete immutable snapshot used by the authenticated approval service.
#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct ApprovalSnapshot {
    pub context: ApprovalContext,
    pub prepared: Prepared,
    pub created_at_sequence: u64,
    pub expires_at_sequence: u64,
    pub state: ApprovalState,
    pub submission_ref: Option<[u8; 32]>,
}

pub(crate) struct ReleasedApproval {
    pub tenant: TenantId,
    pub approval_id: [u8; 32],
    pub submission_ref: [u8; 32],
    pub prepared: Prepared,
}

/// Thread-safe approval registry; state transitions serialize per hold.
pub struct ApprovalRegistry {
    holds: Mutex<BTreeMap<[u8; 32], HeldApproval>>,
    store: Option<Arc<Mutex<Store>>>,
}

impl Default for ApprovalRegistry {
    fn default() -> Self {
        Self {
            holds: Mutex::new(BTreeMap::new()),
            store: None,
        }
    }
}

impl ApprovalRegistry {
    #[must_use]
    pub fn with_store(store: Arc<Mutex<Store>>) -> Self {
        Self {
            holds: Mutex::new(BTreeMap::new()),
            store: Some(store),
        }
    }

    pub(crate) fn has_durable_store(&self) -> bool {
        self.store.is_some()
    }

    /// Reconstructs all durable holds belonging to `tenant`, refusing any malformed record.
    ///
    /// # Errors
    ///
    /// Returns an error if approval state, reservations, registry bindings, or durable storage are invalid.
    pub fn replay_tenant(
        &self,
        tenant: &TenantId,
        limiter: &BudgetLimiter,
    ) -> Result<usize, ApprovalError> {
        let store = self.store.as_ref().ok_or(ApprovalError::Unavailable)?;
        let store = store.lock().map_err(|_| ApprovalError::Unavailable)?;
        let ids = store.list_object_ids(tenant, ObjectKind::PreparedActivity);
        let mut decoded = Vec::new();
        for id in ids.into_iter().filter(|id| id.starts_with(HOLD_KEY_PREFIX)) {
            if id.len() != HOLD_KEY_PREFIX.len() + 32 {
                return Err(ApprovalError::CorruptRecord);
            }
            let key = TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, id)
                .map_err(|_| ApprovalError::CorruptRecord)?;
            let value = store.get(&key).ok_or(ApprovalError::CorruptRecord)?;
            let held = decode_hold(value.bytes())?;
            validate_replayed(&held, tenant)?;
            if key.object_id()[HOLD_KEY_PREFIX.len()..] != held.context.request_id {
                return Err(ApprovalError::CorruptRecord);
            }
            decoded.push((held.context.request_id, held));
        }
        drop(store);
        let reservations = decoded
            .iter()
            .flat_map(|(_, held)| held.budget_reservations.iter().cloned())
            .collect::<Vec<_>>();
        budget::restore(limiter, &reservations).map_err(|_| ApprovalError::CorruptRecord)?;
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        for (id, held) in decoded {
            if holds.insert(id, held).is_some() {
                return Err(ApprovalError::DuplicateHold);
            }
        }
        Ok(holds.len())
    }

    pub(crate) fn replay_released(
        &self,
        tenant: &TenantId,
        limiter: &BudgetLimiter,
        registry: &ModuleRegistry,
    ) -> Result<Vec<ReleasedApproval>, ApprovalError> {
        let store = self.store.as_ref().ok_or(ApprovalError::Unavailable)?;
        let store = store.lock().map_err(|_| ApprovalError::Unavailable)?;
        let mut released = Vec::new();
        let mut reservations = Vec::new();
        for id in store
            .list_object_ids(tenant, ObjectKind::Outbox)
            .into_iter()
            .filter(|id| id.starts_with(RELEASED_KEY_PREFIX))
        {
            if id.len() != RELEASED_KEY_PREFIX.len() + 32 {
                return Err(ApprovalError::CorruptRecord);
            }
            let key = TenantKey::new(tenant.clone(), ObjectKind::Outbox, id)
                .map_err(|_| ApprovalError::CorruptRecord)?;
            let value = store.get(&key).ok_or(ApprovalError::CorruptRecord)?;
            let (submission_ref, held) = decode_released(value.bytes())?;
            validate_replayed(&held, tenant)?;
            let canonical = held.prepared.unsigned_canonical_bytes.as_bytes();
            let disclosure = layerx_crypto::disclosure::bind(canonical, registry)
                .map_err(|_| ApprovalError::CorruptRecord)?;
            if disclosure
                .reencode()
                .map_err(|_| ApprovalError::CorruptRecord)?
                != canonical
            {
                return Err(ApprovalError::CorruptRecord);
            }
            if key.object_id()[RELEASED_KEY_PREFIX.len()..] != held.context.request_id {
                return Err(ApprovalError::CorruptRecord);
            }
            reservations.extend(held.budget_reservations.iter().cloned());
            released.push(ReleasedApproval {
                tenant: tenant.clone(),
                approval_id: held.context.request_id,
                submission_ref,
                prepared: held.prepared,
            });
        }
        drop(store);
        budget::restore(limiter, &reservations).map_err(|_| ApprovalError::CorruptRecord)?;
        Ok(released)
    }

    /// Rebinds every replayed canonical preparation to the live negotiated registry.
    ///
    /// # Errors
    ///
    /// Returns an error if approval state, reservations, registry bindings, or durable storage are invalid.
    pub fn validate_registry(
        &self,
        tenant: &TenantId,
        registry: &ModuleRegistry,
    ) -> Result<(), ApprovalError> {
        let holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        for held in holds.values().filter(|held| &held.context.tenant == tenant) {
            let bytes = held.prepared.unsigned_canonical_bytes.as_bytes();
            let disclosure = layerx_crypto::disclosure::bind(bytes, registry)
                .map_err(|_| ApprovalError::CorruptRecord)?;
            if disclosure
                .reencode()
                .map_err(|_| ApprovalError::CorruptRecord)?
                != bytes
            {
                return Err(ApprovalError::CorruptRecord);
            }
        }
        Ok(())
    }

    ///
    /// # Errors
    ///
    /// Returns an error if approval state, reservations, registry bindings, or durable storage are invalid.
    pub fn hold_ids(&self) -> Result<Vec<[u8; 32]>, ApprovalError> {
        Ok(self
            .holds
            .lock()
            .map_err(|_| ApprovalError::Unavailable)?
            .keys()
            .copied()
            .collect())
    }
    /// Returns the disclosure-only ticket for one hold when it exists.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the hold registry lock is poisoned.
    pub fn ticket(&self, hold_id: [u8; 32]) -> Result<Option<ApprovalTicket>, ApprovalError> {
        let holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        Ok(holds.get(&hold_id).map(ticket_from_hold))
    }

    /// Returns the terminal audit entry recorded for one hold when it exists.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the hold registry lock is poisoned.
    pub fn audit_entry(
        &self,
        hold_id: [u8; 32],
    ) -> Result<Option<ApprovalAuditEntry>, ApprovalError> {
        let holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        Ok(holds.get(&hold_id).and_then(|held| held.audit.clone()))
    }

    pub(crate) fn list_scoped(
        &self,
        tenant: &TenantId,
        current_sequence: u64,
    ) -> Result<Vec<ApprovalSnapshot>, ApprovalError> {
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        let mut records = Vec::new();
        for held in holds.values_mut() {
            if &held.context.tenant == tenant {
                validate_for_read(held, current_sequence);
                records.push(snapshot(held));
            }
        }
        Ok(records)
    }

    pub(crate) fn get_scoped(
        &self,
        tenant: &TenantId,
        hold_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<ApprovalSnapshot, ApprovalError> {
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        let held = holds.get_mut(&hold_id).ok_or(ApprovalError::NotFound)?;
        if &held.context.tenant != tenant {
            return Err(ApprovalError::NotFound);
        }
        validate_for_read(held, current_sequence);
        Ok(snapshot(held))
    }

    pub(crate) fn claim_scoped(
        &self,
        tenant: &TenantId,
        hold_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<ApprovalSnapshot, ApprovalError> {
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        let held = holds.get_mut(&hold_id).ok_or(ApprovalError::NotFound)?;
        if &held.context.tenant != tenant {
            return Err(ApprovalError::NotFound);
        }
        validate_for_read(held, current_sequence);
        if held.state == ApprovalState::Defective {
            return Err(ApprovalError::Defective);
        }
        if held.state != ApprovalState::AwaitingApproval {
            return Err(ApprovalError::AlreadyDecided(held.state));
        }
        if held.decision_claimed {
            return Err(ApprovalError::DecisionConflict);
        }
        held.decision_claimed = true;
        Ok(snapshot(held))
    }

    pub(crate) fn complete_claim(
        &self,
        hold_id: [u8; 32],
        state: ApprovalState,
        approver: ApproverId,
        reason: &'static str,
        resulting_activity_id: Option<[u8; 32]>,
        decision_record: Option<crate::approval::PreparedDecision>,
    ) -> Result<ApprovalAuditEntry, ApprovalError> {
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        let held = holds.get_mut(&hold_id).ok_or(ApprovalError::NotFound)?;
        if !held.decision_claimed || held.state != ApprovalState::AwaitingApproval {
            return Err(ApprovalError::DecisionConflict);
        }
        let mut audit = terminal_audit(held, state, Some(approver), reason);
        audit.resulting_activity_id = resulting_activity_id;
        if let Some(submission_ref) = resulting_activity_id {
            replace_durable_with_released(
                self,
                held,
                submission_ref,
                decision_record.ok_or(ApprovalError::CorruptRecord)?,
            )?;
        } else {
            if decision_record.is_some() {
                return Err(ApprovalError::CorruptRecord);
            }
            remove_durable(self, held)?;
        }
        held.state = state;
        held.decision_claimed = false;
        held.audit = Some(audit.clone());
        Ok(audit)
    }

    pub(crate) fn abort_claim(&self, hold_id: [u8; 32]) -> Result<(), ApprovalError> {
        let mut holds = self.holds.lock().map_err(|_| ApprovalError::Unavailable)?;
        let held = holds.get_mut(&hold_id).ok_or(ApprovalError::NotFound)?;
        held.decision_claimed = false;
        Ok(())
    }

    pub(crate) fn reject_claim_releasing(
        &self,
        hold_id: [u8; 32],
        approver: ApproverId,
        limiter: &BudgetLimiter,
        current_sequence: u64,
        decision_record: crate::approval::PreparedDecision,
        decision_store: &Mutex<Store>,
    ) -> Result<ApprovalAuditEntry, crate::approval::ApprovalOperationError> {
        self.finish_claim_releasing(
            hold_id,
            ApprovalState::Rejected,
            approver,
            "approver_rejected_and_released_reservation",
            limiter,
            current_sequence,
            decision_record,
            decision_store,
        )
    }

    pub(crate) fn check_reserved_limits(
        &self,
        hold_id: [u8; 32],
        limiter: &BudgetLimiter,
    ) -> Result<(), crate::approval::ApprovalOperationError> {
        use crate::approval::ApprovalOperationError;
        let holds = self
            .holds
            .lock()
            .map_err(|_| ApprovalOperationError::Registry(ApprovalError::Unavailable))?;
        let held = holds
            .get(&hold_id)
            .ok_or(ApprovalOperationError::Registry(ApprovalError::NotFound))?;
        if held.state != ApprovalState::AwaitingApproval {
            return Ok(());
        }
        for reservation in &held.budget_reservations {
            limiter
                .consumed(reservation.limit_id)
                .map_err(ApprovalOperationError::Reservation)?;
        }
        Ok(())
    }

    #[allow(clippy::too_many_arguments)]
    pub(crate) fn finish_claim_releasing(
        &self,
        hold_id: [u8; 32],
        state: ApprovalState,
        approver: ApproverId,
        reason: &'static str,
        limiter: &BudgetLimiter,
        current_sequence: u64,
        decision_record: crate::approval::PreparedDecision,
        decision_store: &Mutex<Store>,
    ) -> Result<ApprovalAuditEntry, crate::approval::ApprovalOperationError> {
        use crate::approval::ApprovalOperationError;
        let mut holds = self
            .holds
            .lock()
            .map_err(|_| ApprovalOperationError::Registry(ApprovalError::Unavailable))?;
        let held = holds
            .get_mut(&hold_id)
            .ok_or(ApprovalOperationError::Registry(ApprovalError::NotFound))?;
        if !held.decision_claimed || held.state != ApprovalState::AwaitingApproval {
            return Err(ApprovalOperationError::Registry(
                ApprovalError::DecisionConflict,
            ));
        }
        if let Err(error) = release_durable(
            self,
            held,
            limiter,
            current_sequence,
            decision_record,
            decision_store,
        ) {
            held.decision_claimed = false;
            return Err(error);
        }
        let audit = terminal_audit(held, state, Some(approver), reason);
        held.state = state;
        held.decision_claimed = false;
        held.audit = Some(audit.clone());
        Ok(audit)
    }
}

/// Approval hold refusal taxonomy.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalError {
    InvalidApprover,
    InvalidWindow,
    InvalidDisclosureDigest,
    ActorMismatch,
    DuplicateHold,
    NotFound,
    AlreadyDecided(ApprovalState),
    DisclosureChanged,
    Defective,
    DecisionConflict,
    Unavailable,
    CorruptRecord,
}

/// Holds only prepared bytes and their decoded disclosure, never the caller request.
///
/// # Errors
///
/// Refuses a window that does not close before the preparation expires, a digest that does not
/// match the canonical bytes, an actor other than the disclosed one, a duplicate request
/// identifier, and a poisoned registry.
pub fn hold(
    registry: &ApprovalRegistry,
    context: ApprovalContext,
    prepared: Prepared,
    current_sequence: u64,
    expires_at_sequence: u64,
) -> Result<ApprovalTicket, ApprovalError> {
    hold_inner(
        registry,
        context,
        prepared,
        current_sequence,
        expires_at_sequence,
        Vec::new(),
    )
}

/// Persists an approval and its exact already-acquired budget reservation in one record.
///
/// # Errors
///
/// Returns an error if approval state, reservations, registry bindings, or durable storage are invalid.
pub fn hold_reserved(
    registry: &ApprovalRegistry,
    context: ApprovalContext,
    prepared: Prepared,
    current_sequence: u64,
    expires_at_sequence: u64,
    reservation: &BudgetReservation,
) -> Result<ApprovalTicket, ApprovalError> {
    let mut durable_limits = reservation
        .durable
        .iter()
        .map(|item| item.limit_id)
        .collect::<Vec<_>>();
    durable_limits.sort_unstable();
    let mut applied_limits = reservation.applied_limits.clone();
    applied_limits.sort_unstable();
    if reservation.id != context.request_id
        || reservation.amount == 0
        || reservation.durable.is_empty()
        || durable_limits.windows(2).any(|pair| pair[0] == pair[1])
        || durable_limits != applied_limits
        || reservation.durable.iter().any(|item| {
            item.reservation_id != reservation.id
                || item.amount != reservation.amount
                || item.digest != item.canonical_digest()
        })
    {
        return Err(ApprovalError::CorruptRecord);
    }
    hold_inner(
        registry,
        context,
        prepared,
        current_sequence,
        expires_at_sequence,
        reservation.durable.clone(),
    )
}

fn hold_inner(
    registry: &ApprovalRegistry,
    context: ApprovalContext,
    prepared: Prepared,
    current_sequence: u64,
    expires_at_sequence: u64,
    budget_reservations: Vec<DurableBudgetReservation>,
) -> Result<ApprovalTicket, ApprovalError> {
    if expires_at_sequence <= current_sequence || expires_at_sequence > prepared.expiry.0 {
        return Err(ApprovalError::InvalidWindow);
    }
    let digest = canonical_digest(prepared.unsigned_canonical_bytes.as_bytes());
    if digest != prepared.disclosure.canonical_digest {
        return Err(ApprovalError::InvalidDisclosureDigest);
    }
    if context.agent.as_bytes() != prepared.disclosure.actor.as_str().as_bytes() {
        return Err(ApprovalError::ActorMismatch);
    }
    let hold_id = context.request_id;
    let mut holds = registry
        .holds
        .lock()
        .map_err(|_| ApprovalError::Unavailable)?;
    if holds.contains_key(&hold_id) {
        return Err(ApprovalError::DuplicateHold);
    }
    let held = HeldApproval {
        context,
        prepared,
        created_at_sequence: current_sequence,
        expires_at_sequence,
        state: ApprovalState::AwaitingApproval,
        audit: None,
        decision_claimed: false,
        budget_reservations,
    };
    if registry.store.is_some() && held.budget_reservations.is_empty() {
        return Err(ApprovalError::CorruptRecord);
    }
    let ticket = ticket_from_hold(&held);
    if let Some(store) = &registry.store {
        let key = hold_storage_key(&held.context.tenant, hold_id)?;
        let bytes = encode_hold(&held)?;
        store
            .lock()
            .map_err(|_| ApprovalError::Unavailable)?
            .put_local(key, bytes)
            .map_err(|_| ApprovalError::Unavailable)?;
    }
    holds.insert(hold_id, held);
    Ok(ticket)
}

/// Applies exactly one decision to the disclosure digest the approver saw.
///
/// # Errors
///
/// Refuses an unknown hold, an already decided or newly expired hold, a presented digest that
/// differs from the held disclosure, and a poisoned registry.
pub fn decide(
    registry: &ApprovalRegistry,
    hold_id: [u8; 32],
    approver: ApproverId,
    choice: ApprovalChoice,
    presented_disclosure_digest: [u8; 32],
    current_sequence: u64,
) -> Result<ApprovalAuditEntry, ApprovalError> {
    let mut holds = registry
        .holds
        .lock()
        .map_err(|_| ApprovalError::Unavailable)?;
    let held = holds.get_mut(&hold_id).ok_or(ApprovalError::NotFound)?;
    if held.state != ApprovalState::AwaitingApproval {
        return Err(ApprovalError::AlreadyDecided(held.state));
    }
    if current_sequence >= held.expires_at_sequence {
        let audit = terminal_audit(
            held,
            ApprovalState::Expired,
            None,
            "approval_window_expired",
        );
        remove_durable(registry, held)?;
        held.state = ApprovalState::Expired;
        held.audit = Some(audit);
        return Err(ApprovalError::AlreadyDecided(ApprovalState::Expired));
    }
    if presented_disclosure_digest != held.prepared.disclosure.canonical_digest {
        return Err(ApprovalError::DisclosureChanged);
    }
    let (state, reason) = match choice {
        ApprovalChoice::Approve => (ApprovalState::Approved, "approver_approved_disclosure"),
        ApprovalChoice::Reject => (ApprovalState::Rejected, "approver_rejected_disclosure"),
    };
    let audit = terminal_audit(held, state, Some(approver), reason);
    remove_durable(registry, held)?;
    held.state = state;
    held.audit = Some(audit.clone());
    Ok(audit)
}

fn remove_durable(registry: &ApprovalRegistry, held: &HeldApproval) -> Result<(), ApprovalError> {
    let Some(store) = &registry.store else {
        return Ok(());
    };
    let key = hold_storage_key(&held.context.tenant, held.context.request_id)?;
    if !store
        .lock()
        .map_err(|_| ApprovalError::Unavailable)?
        .remove_local(&key)
        .map_err(|_| ApprovalError::Unavailable)?
    {
        return Err(ApprovalError::CorruptRecord);
    }
    Ok(())
}

fn release_durable(
    registry: &ApprovalRegistry,
    held: &HeldApproval,
    limiter: &BudgetLimiter,
    current_sequence: u64,
    decision_record: crate::approval::PreparedDecision,
    decision_store: &Mutex<Store>,
) -> Result<(), crate::approval::ApprovalOperationError> {
    use crate::approval::ApprovalOperationError;
    let mut store = registry
        .store
        .as_deref()
        .unwrap_or(decision_store)
        .lock()
        .map_err(|_| ApprovalOperationError::Registry(ApprovalError::Unavailable))?;
    for reservation in &held.budget_reservations {
        limiter
            .consumed(reservation.limit_id)
            .map_err(ApprovalOperationError::Reservation)?;
    }
    let staged = budget::stage_release(
        limiter,
        held.context.request_id,
        budget::ReleaseKind::Failed,
        current_sequence,
    )
    .map_err(ApprovalOperationError::Reservation)?;
    let persisted = if registry.store.is_some() {
        let key = hold_storage_key(&held.context.tenant, held.context.request_id)
            .map_err(ApprovalOperationError::Registry)?;
        if store.get(&key).is_none() {
            return Err(ApprovalOperationError::Registry(
                ApprovalError::CorruptRecord,
            ));
        }
        decision_record.remove_hold(&mut store, key)
    } else {
        decision_record.persist(&mut store)
    };
    persisted.map_err(|error| match error {
        crate::approval::ApprovalExpiryError::DecisionConflict => {
            ApprovalOperationError::Registry(ApprovalError::DecisionConflict)
        }
        _ => ApprovalOperationError::Registry(ApprovalError::Unavailable),
    })?;
    let _ = staged.publish();
    Ok(())
}

fn replace_durable_with_released(
    registry: &ApprovalRegistry,
    held: &HeldApproval,
    submission_ref: [u8; 32],
    decision_record: crate::approval::PreparedDecision,
) -> Result<(), ApprovalError> {
    let Some(store) = &registry.store else {
        return Ok(());
    };
    let hold_key = hold_storage_key(&held.context.tenant, held.context.request_id)?;
    let released_key = released_storage_key(&held.context.tenant, held.context.request_id)?;
    let bytes = encode_released(held, submission_ref)?;
    let mut store = store.lock().map_err(|_| ApprovalError::Unavailable)?;
    decision_record
        .replace_hold(&mut store, &hold_key, released_key, bytes)
        .map_err(|error| match error {
            crate::approval::ApprovalExpiryError::DecisionConflict => {
                ApprovalError::DecisionConflict
            }
            _ => ApprovalError::Unavailable,
        })
}

fn validate_for_read(held: &mut HeldApproval, current_sequence: u64) {
    if held.state != ApprovalState::AwaitingApproval {
        return;
    }
    if current_sequence >= held.expires_at_sequence {
        let audit = terminal_audit(
            held,
            ApprovalState::Expired,
            None,
            "approval_window_expired",
        );
        held.state = ApprovalState::Expired;
        held.audit = Some(audit);
        return;
    }
    let observed = canonical_digest(held.prepared.unsigned_canonical_bytes.as_bytes());
    if observed != held.prepared.disclosure.canonical_digest {
        let audit = terminal_audit(
            held,
            ApprovalState::Defective,
            None,
            "held_disclosure_digest_mismatch",
        );
        held.state = ApprovalState::Defective;
        held.audit = Some(audit);
    }
}

fn snapshot(held: &HeldApproval) -> ApprovalSnapshot {
    ApprovalSnapshot {
        context: held.context.clone(),
        prepared: held.prepared.clone(),
        created_at_sequence: held.created_at_sequence,
        expires_at_sequence: held.expires_at_sequence,
        state: held.state,
        submission_ref: held
            .audit
            .as_ref()
            .and_then(|audit| audit.resulting_activity_id),
    }
}

/// Expires every elapsed hold against an explicit protocol-relative sequence.
///
/// # Errors
///
/// Returns `Unavailable` when the hold registry lock is poisoned.
pub fn expire(
    registry: &ApprovalRegistry,
    current_sequence: u64,
) -> Result<Vec<ApprovalAuditEntry>, ApprovalError> {
    let mut holds = registry
        .holds
        .lock()
        .map_err(|_| ApprovalError::Unavailable)?;
    let mut expired = Vec::new();
    for held in holds.values_mut() {
        if held.state == ApprovalState::AwaitingApproval
            && current_sequence >= held.expires_at_sequence
        {
            let audit = terminal_audit(
                held,
                ApprovalState::Expired,
                None,
                "approval_window_expired",
            );
            held.state = ApprovalState::Expired;
            held.audit = Some(audit.clone());
            expired.push(audit);
        }
    }
    Ok(expired)
}

fn ticket_from_hold(held: &HeldApproval) -> ApprovalTicket {
    ApprovalTicket {
        hold_id: held.context.request_id,
        disclosure: held.prepared.disclosure.clone(),
        disclosure_digest: held.prepared.disclosure.canonical_digest,
        expires_at_sequence: held.expires_at_sequence,
        state: held.state,
    }
}

fn terminal_audit(
    held: &HeldApproval,
    decision: ApprovalState,
    approver: Option<ApproverId>,
    reason: &'static str,
) -> ApprovalAuditEntry {
    ApprovalAuditEntry {
        tenant: held.context.tenant.clone(),
        agent: held.context.agent.clone(),
        session: held.context.session,
        capability: held.context.capability,
        policy_version: held.context.policy_version.clone(),
        request_id: held.context.request_id,
        idempotency_key: held.prepared.disclosure.idempotency_key.as_str().to_owned(),
        decision,
        reason,
        resulting_activity_id: None,
        verification_level: VerificationLevel::UNVERIFIED,
        approver,
        disclosure_digest: held.prepared.disclosure.canonical_digest,
    }
}

fn canonical_digest(bytes: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hasher.finalize().into()
}

fn hold_storage_key(tenant: &TenantId, id: [u8; 32]) -> Result<TenantKey, ApprovalError> {
    let mut object = HOLD_KEY_PREFIX.to_vec();
    object.extend_from_slice(&id);
    TenantKey::new(tenant.clone(), ObjectKind::PreparedActivity, object)
        .map_err(|_| ApprovalError::CorruptRecord)
}

pub(crate) fn released_storage_key(
    tenant: &TenantId,
    id: [u8; 32],
) -> Result<TenantKey, ApprovalError> {
    let mut object = RELEASED_KEY_PREFIX.to_vec();
    object.extend_from_slice(&id);
    TenantKey::new(tenant.clone(), ObjectKind::Outbox, object)
        .map_err(|_| ApprovalError::CorruptRecord)
}

/// Decodes one released approval record and returns its exact durable budget reservations
/// after the same validation replay applies.
///
/// # Errors
///
/// Returns `CorruptRecord` for a record that does not decode or does not validate for `tenant`.
pub(crate) fn released_reservations(
    tenant: &TenantId,
    bytes: &[u8],
) -> Result<Vec<DurableBudgetReservation>, ApprovalError> {
    let (_, held) = decode_released(bytes)?;
    validate_replayed(&held, tenant)?;
    Ok(held.budget_reservations)
}

fn encode_released(
    held: &HeldApproval,
    submission_ref: [u8; 32],
) -> Result<Vec<u8>, ApprovalError> {
    let encoded_hold = encode_hold(held)?;
    let mut bytes = Vec::with_capacity(RELEASED_MAGIC.len() + 32 + encoded_hold.len());
    bytes.extend_from_slice(RELEASED_MAGIC);
    bytes.extend_from_slice(&submission_ref);
    bytes.extend_from_slice(&encoded_hold);
    if bytes.len() > MAX_HOLD_RECORD_BYTES {
        return Err(ApprovalError::CorruptRecord);
    }
    Ok(bytes)
}

fn decode_released(bytes: &[u8]) -> Result<([u8; 32], HeldApproval), ApprovalError> {
    if bytes.len() < RELEASED_MAGIC.len() + 32 || &bytes[..RELEASED_MAGIC.len()] != RELEASED_MAGIC {
        return Err(ApprovalError::CorruptRecord);
    }
    let submission_ref = bytes[RELEASED_MAGIC.len()..RELEASED_MAGIC.len() + 32]
        .try_into()
        .map_err(|_| ApprovalError::CorruptRecord)?;
    let held = decode_hold(&bytes[RELEASED_MAGIC.len() + 32..])?;
    Ok((submission_ref, held))
}

fn validate_replayed(held: &HeldApproval, tenant: &TenantId) -> Result<(), ApprovalError> {
    if &held.context.tenant != tenant
        || held.context.request_id == [0; 32]
        || held.created_at_sequence >= held.expires_at_sequence
        || held.expires_at_sequence > held.prepared.expiry.get()
        || held.context.agent.as_bytes() != held.prepared.disclosure.actor.as_str().as_bytes()
        || canonical_digest(held.prepared.unsigned_canonical_bytes.as_bytes())
            != held.prepared.disclosure.canonical_digest
        || held.state != ApprovalState::AwaitingApproval
        || held.audit.is_some()
        || held.decision_claimed
        || held.budget_reservations.is_empty()
        || held.budget_reservations.iter().any(|reservation| {
            reservation.reservation_id != held.context.request_id
                || reservation.amount == 0
                || reservation.expiry_sequence < held.expires_at_sequence
                || reservation.digest != reservation.canonical_digest()
        })
    {
        return Err(ApprovalError::CorruptRecord);
    }
    Ok(())
}

fn put_bytes(out: &mut Vec<u8>, bytes: &[u8]) -> Result<(), ApprovalError> {
    let len = u32::try_from(bytes.len()).map_err(|_| ApprovalError::CorruptRecord)?;
    out.extend_from_slice(&len.to_be_bytes());
    out.extend_from_slice(bytes);
    Ok(())
}
fn put_text(out: &mut Vec<u8>, text: &str) -> Result<(), ApprovalError> {
    put_bytes(out, text.as_bytes())
}

fn encode_hold(h: &HeldApproval) -> Result<Vec<u8>, ApprovalError> {
    let mut o = HOLD_MAGIC.to_vec();
    put_text(&mut o, h.context.tenant.as_str())?;
    put_bytes(&mut o, h.context.agent.as_bytes())?;
    o.extend_from_slice(&h.context.session.0);
    o.extend_from_slice(&h.context.capability.0);
    put_text(&mut o, &h.context.policy_version)?;
    o.extend_from_slice(&h.context.request_id);
    o.extend_from_slice(&h.created_at_sequence.to_be_bytes());
    o.extend_from_slice(&h.expires_at_sequence.to_be_bytes());
    o.extend_from_slice(
        &(u32::try_from(h.budget_reservations.len()).map_err(|_| ApprovalError::CorruptRecord)?)
            .to_be_bytes(),
    );
    for reservation in &h.budget_reservations {
        o.extend_from_slice(&reservation.reservation_id);
        o.extend_from_slice(&reservation.limit_id.0);
        let (scope_tag, scope_id) = encode_scope(reservation.scope);
        o.push(scope_tag);
        o.extend_from_slice(&scope_id);
        o.extend_from_slice(&reservation.amount.to_be_bytes());
        o.extend_from_slice(&reservation.ceiling.to_be_bytes());
        o.extend_from_slice(&reservation.expiry_sequence.to_be_bytes());
        o.extend_from_slice(&reservation.digest);
    }
    put_text(&mut o, h.prepared.preparation_ref.as_str())?;
    put_bytes(&mut o, h.prepared.unsigned_canonical_bytes.as_bytes())?;
    put_bytes(&mut o, h.prepared.signing_preimage.as_bytes())?;
    o.extend_from_slice(&h.prepared.disclosure.canonical_digest);
    o.extend_from_slice(&h.prepared.disclosure.activity_type.0.to_be_bytes());
    put_text(&mut o, h.prepared.disclosure.actor.as_str())?;
    put_text(&mut o, h.prepared.disclosure.authority.as_str())?;
    let cps = h.prepared.disclosure.counterparties.values();
    o.extend_from_slice(
        &(u32::try_from(cps.len()).map_err(|_| ApprovalError::CorruptRecord)?).to_be_bytes(),
    );
    for v in cps {
        put_text(&mut o, v.as_str())?;
    }
    let amts = h.prepared.disclosure.amounts.values();
    o.extend_from_slice(
        &(u32::try_from(amts.len()).map_err(|_| ApprovalError::CorruptRecord)?).to_be_bytes(),
    );
    for v in amts {
        put_text(&mut o, v.counterparty.as_str())?;
        o.extend_from_slice(&v.amount.0.to_be_bytes());
    }
    put_text(&mut o, h.prepared.disclosure.asset.as_str())?;
    o.extend_from_slice(&h.prepared.disclosure.fee_limit.0.to_be_bytes());
    o.extend_from_slice(&h.prepared.disclosure.expiry.get().to_be_bytes());
    put_text(&mut o, h.prepared.disclosure.idempotency_key.as_str())?;
    o.extend_from_slice(&h.prepared.expiry.get().to_be_bytes());
    if o.len() > MAX_HOLD_RECORD_BYTES {
        return Err(ApprovalError::CorruptRecord);
    }
    Ok(o)
}

struct Reader<'a> {
    b: &'a [u8],
    p: usize,
}
impl<'a> Reader<'a> {
    fn take(&mut self, n: usize) -> Result<&'a [u8], ApprovalError> {
        let e = self.p.checked_add(n).ok_or(ApprovalError::CorruptRecord)?;
        let v = self.b.get(self.p..e).ok_or(ApprovalError::CorruptRecord)?;
        self.p = e;
        Ok(v)
    }
    fn u16(&mut self) -> Result<u16, ApprovalError> {
        Ok(u16::from_be_bytes(
            self.take(2)?
                .try_into()
                .map_err(|_| ApprovalError::CorruptRecord)?,
        ))
    }
    fn u32(&mut self) -> Result<u32, ApprovalError> {
        Ok(u32::from_be_bytes(
            self.take(4)?
                .try_into()
                .map_err(|_| ApprovalError::CorruptRecord)?,
        ))
    }
    fn u64(&mut self) -> Result<u64, ApprovalError> {
        Ok(u64::from_be_bytes(
            self.take(8)?
                .try_into()
                .map_err(|_| ApprovalError::CorruptRecord)?,
        ))
    }
    fn u128(&mut self) -> Result<u128, ApprovalError> {
        Ok(u128::from_be_bytes(
            self.take(16)?
                .try_into()
                .map_err(|_| ApprovalError::CorruptRecord)?,
        ))
    }
    fn a16(&mut self) -> Result<[u8; 16], ApprovalError> {
        self.take(16)?
            .try_into()
            .map_err(|_| ApprovalError::CorruptRecord)
    }
    fn bytes(&mut self) -> Result<Vec<u8>, ApprovalError> {
        let n = usize::try_from(self.u32()?).map_err(|_| ApprovalError::CorruptRecord)?;
        Ok(self.take(n)?.to_vec())
    }
    fn text(&mut self) -> Result<String, ApprovalError> {
        String::from_utf8(self.bytes()?).map_err(|_| ApprovalError::CorruptRecord)
    }
    fn a32(&mut self) -> Result<[u8; 32], ApprovalError> {
        self.take(32)?
            .try_into()
            .map_err(|_| ApprovalError::CorruptRecord)
    }
}
fn decode_hold(bytes: &[u8]) -> Result<HeldApproval, ApprovalError> {
    if bytes.len() > MAX_HOLD_RECORD_BYTES {
        return Err(ApprovalError::CorruptRecord);
    }
    let mut r = Reader { b: bytes, p: 0 };
    if r.take(8)? != HOLD_MAGIC {
        return Err(ApprovalError::CorruptRecord);
    }
    let tenant = TenantId::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let agent = Did::new(&r.bytes()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let context = ApprovalContext {
        tenant,
        agent,
        session: SessionId(r.a32()?),
        capability: CapabilityId(r.a32()?),
        policy_version: r.text()?,
        request_id: r.a32()?,
    };
    let created_at_sequence = r.u64()?;
    let expires_at_sequence = r.u64()?;
    let budget_reservations = decode_reservations(&mut r)?;
    let preparation_ref =
        PreparationRef::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let unsigned_canonical_bytes =
        CanonicalBytes::new(r.bytes()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let signing_preimage =
        SigningPreimage::new(r.bytes()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let canonical_digest = r.a32()?;
    let activity_type = ActivityType(r.u16()?);
    let actor = AgentDid::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let authority = AuthorityRef::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let cp_count = r.u32()?;
    if cp_count > MAX_DISCLOSURE_ITEMS {
        return Err(ApprovalError::CorruptRecord);
    }
    let mut cps = Vec::new();
    for _ in 0..cp_count {
        cps.push(AgentDid::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?);
    }
    let amount_count = r.u32()?;
    if amount_count > MAX_DISCLOSURE_ITEMS {
        return Err(ApprovalError::CorruptRecord);
    }
    let mut amounts = Vec::new();
    for _ in 0..amount_count {
        let counterparty = AgentDid::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
        let amount = Amount(u128::from_be_bytes(
            r.take(16)?
                .try_into()
                .map_err(|_| ApprovalError::CorruptRecord)?,
        ));
        amounts.push(DisclosedAmount {
            counterparty,
            amount,
        });
    }
    let asset = Asset::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let fee_limit = Amount(u128::from_be_bytes(
        r.take(16)?
            .try_into()
            .map_err(|_| ApprovalError::CorruptRecord)?,
    ));
    let expiry = TimestampSeconds(r.u64()?);
    let idempotency_key =
        IdempotencyRef::new(r.text()?).map_err(|_| ApprovalError::CorruptRecord)?;
    let prepared_expiry = TimestampSeconds(r.u64()?);
    if r.p != bytes.len() {
        return Err(ApprovalError::CorruptRecord);
    }
    let disclosure = Disclosure {
        canonical_digest,
        activity_type,
        actor,
        authority,
        counterparties: ExplicitSet::allow(cps),
        amounts: ExplicitSet::allow(amounts),
        asset,
        fee_limit,
        expiry,
        idempotency_key,
    };
    Ok(HeldApproval {
        context,
        prepared: Prepared {
            preparation_ref,
            unsigned_canonical_bytes,
            signing_preimage,
            disclosure,
            expiry: prepared_expiry,
        },
        created_at_sequence,
        expires_at_sequence,
        state: ApprovalState::AwaitingApproval,
        audit: None,
        decision_claimed: false,
        budget_reservations,
    })
}

fn encode_scope(scope: LimitScope) -> (u8, [u8; 32]) {
    match scope {
        LimitScope::Tenant(id) => (0, id),
        LimitScope::Agent(id) => (1, id),
        LimitScope::Session(id) => (2, id),
        LimitScope::Capability(id) => (3, id),
        LimitScope::Counterparty(id) => (4, id),
    }
}

fn decode_scope(tag: u8, id: [u8; 32]) -> Result<LimitScope, ApprovalError> {
    match tag {
        0 => Ok(LimitScope::Tenant(id)),
        1 => Ok(LimitScope::Agent(id)),
        2 => Ok(LimitScope::Session(id)),
        3 => Ok(LimitScope::Capability(id)),
        4 => Ok(LimitScope::Counterparty(id)),
        _ => Err(ApprovalError::CorruptRecord),
    }
}

fn decode_reservations(r: &mut Reader<'_>) -> Result<Vec<DurableBudgetReservation>, ApprovalError> {
    let reservation_count = r.u32()?;
    if reservation_count == 0 || reservation_count > MAX_DISCLOSURE_ITEMS {
        return Err(ApprovalError::CorruptRecord);
    }
    let mut budget_reservations = Vec::new();
    for _ in 0..reservation_count {
        let reservation_id = r.a32()?;
        let limit_id = LimitId(r.a16()?);
        let scope = decode_scope(r.take(1)?[0], r.a32()?)?;
        let amount = r.u128()?;
        let ceiling = r.u128()?;
        let expiry_sequence = r.u64()?;
        let digest = r.a32()?;
        budget_reservations.push(DurableBudgetReservation {
            reservation_id,
            limit_id,
            scope,
            amount,
            ceiling,
            expiry_sequence,
            digest,
        });
    }
    Ok(budget_reservations)
}

#[cfg(test)]
mod released_replay_tests {
    use std::sync::{Arc, Mutex};

    use ed25519_dalek::{Signer as _, SigningKey};
    use layerx_agent_api::identity::{ActivityType, AgentDid, Asset, AuthorityRef, ExplicitSet};
    use layerx_agent_api::prepare::{
        CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
    };
    use layerx_agent_api::{Amount, TimestampSeconds};
    use layerx_types::activity::{Authority, TimestampBound};
    use layerx_types::ids::{Did, IdempotencyKey};
    use layerx_types::payload::{ModuleId, ModuleRegistration, ModuleRegistry};
    use layerx_wire::encode::Encoder;
    use sha2::{Digest as _, Sha256};

    use super::{
        canonical_digest, encode_released, hold_reserved, released_storage_key, ApprovalContext,
        ApprovalError, ApprovalRegistry,
    };
    use crate::budget::{
        reserve, BudgetLimiter, LimitConfig, LimitId, LimitScope, ReservationRequest,
    };
    use crate::capability::CapabilityId;
    use crate::prepare::{
        prepare_activity, CorePreparationBoundary, CorePreparationState, CoreStateError,
        PreparationDefaults, PrepareRequest,
    };
    use crate::session::SessionId;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    const ACTOR: &str = "did:layerx:expiry";

    struct RecordedCore(CorePreparationState);

    impl CorePreparationBoundary for RecordedCore {
        fn preparation_state(
            &mut self,
            _actor: &Did,
        ) -> Result<CorePreparationState, CoreStateError> {
            Ok(self.0.clone())
        }
    }

    struct Root(std::path::PathBuf);

    impl Drop for Root {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn tenant() -> TenantId {
        TenantId::new("tenant-released-replay").unwrap_or_else(|error| panic!("{error}"))
    }

    fn activity_type() -> layerx_types::payload::ActivityType {
        layerx_types::payload::ActivityType::new(ModuleId::Asset, 5)
            .unwrap_or_else(|error| panic!("activity: {error:?}"))
    }

    fn modules() -> ModuleRegistry {
        ModuleRegistry::new(
            &[ModuleRegistration::new(ModuleId::Asset, &[activity_type()])
                .unwrap_or_else(|error| panic!("registration: {error:?}"))],
        )
        .unwrap_or_else(|error| panic!("registry: {error:?}"))
    }

    fn sign(mut payload: Vec<u8>) -> Vec<u8> {
        let offset = payload.len() - 167;
        let key = SigningKey::from_bytes(&[0x66; 32]);
        payload[offset + 33..offset + 65].copy_from_slice(&key.verifying_key().to_bytes());
        let mut hasher = Sha256::new();
        hasher.update(layerx_wire::hash::Domain::SignaturePreimage.tag());
        hasher.update(&payload[..2]);
        hasher.update(&payload[4..offset + 33]);
        hasher.update(&payload[offset + 129..]);
        let digest: [u8; 32] = hasher.finalize().into();
        payload[offset + 65..offset + 129].copy_from_slice(&key.sign(&digest).to_bytes());
        payload
    }

    fn send_payload() -> Vec<u8> {
        let mut encoder = Encoder::new(512);
        encoder
            .u16(0x5301)
            .unwrap_or_else(|error| panic!("tag: {error:?}"));
        encoder
            .u16(10)
            .unwrap_or_else(|error| panic!("fields: {error:?}"));
        encoder
            .fixed(&[0x11; 32])
            .unwrap_or_else(|error| panic!("from: {error:?}"));
        encoder
            .fixed(&[0x22; 32])
            .unwrap_or_else(|error| panic!("to: {error:?}"));
        encoder
            .fixed(&[0x33; 32])
            .unwrap_or_else(|error| panic!("asset: {error:?}"));
        encoder
            .u128(25)
            .unwrap_or_else(|error| panic!("amount: {error:?}"));
        encoder
            .u64(5)
            .unwrap_or_else(|error| panic!("sequence: {error:?}"));
        encoder
            .fixed(&[4; 32])
            .unwrap_or_else(|error| panic!("idempotency: {error:?}"));
        encoder
            .u64(1_010)
            .unwrap_or_else(|error| panic!("expiry: {error:?}"));
        encoder
            .fixed(&[0x55; 32])
            .unwrap_or_else(|error| panic!("context: {error:?}"));
        encoder
            .u8(0)
            .unwrap_or_else(|error| panic!("conditions: {error:?}"));
        encoder
            .u8(1)
            .unwrap_or_else(|error| panic!("authority kind: {error:?}"));
        encoder
            .fixed(&[0x11; 32])
            .unwrap_or_else(|error| panic!("controller: {error:?}"));
        encoder
            .fixed(&[0x66; 32])
            .unwrap_or_else(|error| panic!("payload key: {error:?}"));
        encoder
            .fixed(&[0x77; 64])
            .unwrap_or_else(|error| panic!("payload signature: {error:?}"));
        encoder
            .fixed(&[0x55; 32])
            .unwrap_or_else(|error| panic!("signed context: {error:?}"));
        encoder
            .u32(17)
            .unwrap_or_else(|error| panic!("network: {error:?}"));
        encoder
            .u16(layerx_wire::limits::PROTOCOL_VERSION)
            .unwrap_or_else(|error| panic!("version: {error:?}"));
        sign(encoder.finish())
    }

    fn prepared() -> Prepared {
        let mut core = RecordedCore(CorePreparationState {
            network_id: 17,
            account_sequence: 5,
            protocol_timestamp: 1_000,
            observed_head_sequence: 88,
            module_registry: modules(),
        });
        let activity = prepare_activity(
            &mut core,
            PreparationDefaults {
                timestamp_span: 30,
                fee_limit: layerx_types::amount::Amount::from_u128(12),
                maximum_payload_bytes: 1_024,
            },
            PrepareRequest {
                actor: Did::new(ACTOR.as_bytes()).unwrap_or_else(|error| panic!("DID: {error:?}")),
                authority: Authority::owner(b"external-authority")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                activity_type: activity_type(),
                expected_account_sequence: Some(5),
                timestamp_bound: Some(
                    TimestampBound::new(995, 1_010)
                        .unwrap_or_else(|error| panic!("timestamp: {error:?}")),
                ),
                fee_limit: Some(layerx_types::amount::Amount::from_u128(7)),
                idempotency_key: IdempotencyKey::new([4; 32]),
                payload: send_payload(),
                declared_payload_limit: 1_024,
            },
        )
        .unwrap_or_else(|error| panic!("prepare: {error:?}"));
        let bytes = activity.canonical_bytes;
        Prepared {
            preparation_ref: PreparationRef::new("released-replay")
                .unwrap_or_else(|error| panic!("preparation: {error:?}")),
            disclosure: Disclosure {
                canonical_digest: canonical_digest(&bytes),
                activity_type: ActivityType(activity_type().value()),
                actor: AgentDid::new(ACTOR).unwrap_or_else(|error| panic!("actor: {error:?}")),
                authority: AuthorityRef::new("session-key")
                    .unwrap_or_else(|error| panic!("authority: {error:?}")),
                counterparties: ExplicitSet::deny_all(),
                amounts: ExplicitSet::deny_all(),
                asset: Asset::new("LXP").unwrap_or_else(|error| panic!("asset: {error:?}")),
                fee_limit: Amount(7),
                expiry: TimestampSeconds(1_010),
                idempotency_key: IdempotencyRef::new("04".repeat(32))
                    .unwrap_or_else(|error| panic!("activity key: {error:?}")),
            },
            unsigned_canonical_bytes: CanonicalBytes::new(bytes)
                .unwrap_or_else(|error| panic!("bytes: {error:?}")),
            signing_preimage: SigningPreimage::new(activity.signing_preimage.to_vec())
                .unwrap_or_else(|error| panic!("preimage: {error:?}")),
            expiry: TimestampSeconds(1_010),
        }
    }

    fn limit(id: u8) -> LimitConfig {
        LimitConfig {
            id: LimitId([id; 16]),
            name: format!("released replay limit {id}"),
            scope: LimitScope::Tenant([id; 32]),
            ceiling: 1_000,
            consumed: 0,
        }
    }

    fn limiter(ids: &[u8]) -> BudgetLimiter {
        BudgetLimiter::new(ids.iter().map(|id| limit(*id)).collect())
            .unwrap_or_else(|error| panic!("limiter: {error:?}"))
    }

    fn entries(store: &Store) -> Vec<(TenantKey, Option<Vec<u8>>)> {
        [ObjectKind::PreparedActivity, ObjectKind::Outbox]
            .into_iter()
            .flat_map(|kind| {
                store
                    .list_object_ids(&tenant(), kind)
                    .into_iter()
                    .map(move |id| {
                        TenantKey::new(tenant(), kind, id)
                            .unwrap_or_else(|error| panic!("key: {error:?}"))
                    })
            })
            .map(|key| {
                let bytes = store.get(&key).map(|value| value.bytes().to_vec());
                (key, bytes)
            })
            .collect()
    }

    /// Persists one released record through the real hold encoding, reserved against limits 1
    /// and 2, and returns its store with the record's bytes.
    fn persisted(label: &str, id: u8) -> (Root, Arc<Mutex<Store>>) {
        let root = Root(std::env::temp_dir().join(format!(
            "layerx-released-replay-{label}-{}",
            std::process::id()
        )));
        let _ = std::fs::remove_dir_all(&root.0);
        let store = Arc::new(Mutex::new(
            Store::open(&root.0).unwrap_or_else(|error| panic!("store: {error}")),
        ));
        let original = limiter(&[1, 2]);
        let reservation = reserve(
            &original,
            &ReservationRequest {
                id: [id; 32],
                amount: 10,
                expiry_sequence: 40,
                current_sequence: 10,
                applicable_limits: vec![LimitId([1; 16]), LimitId([2; 16])],
            },
        )
        .unwrap_or_else(|error| panic!("reserve: {error:?}"));
        let registry = ApprovalRegistry::with_store(Arc::clone(&store));
        hold_reserved(
            &registry,
            ApprovalContext {
                tenant: tenant(),
                agent: Did::new(ACTOR.as_bytes())
                    .unwrap_or_else(|error| panic!("agent: {error:?}")),
                session: SessionId([2; 32]),
                capability: CapabilityId([3; 32]),
                policy_version: "policy-v3".to_owned(),
                request_id: [id; 32],
            },
            prepared(),
            10,
            40,
            &reservation,
        )
        .unwrap_or_else(|error| panic!("hold: {error:?}"));
        let held = registry
            .holds
            .lock()
            .unwrap_or_else(|_| panic!("holds lock"))
            .get(&[id; 32])
            .cloned()
            .unwrap_or_else(|| panic!("held approval"));
        let bytes = encode_released(&held, [9; 32]).unwrap_or_else(|error| panic!("{error:?}"));
        let key = released_storage_key(&tenant(), [id; 32])
            .unwrap_or_else(|error| panic!("key: {error:?}"));
        store
            .lock()
            .unwrap_or_else(|_| panic!("store lock"))
            .put_local(key, bytes)
            .unwrap_or_else(|error| panic!("released: {error}"));
        (root, store)
    }

    #[test]
    fn released_replay_under_a_missing_limit_is_refused_without_change() {
        let (_root, store) = persisted("missing", 41);
        let before = entries(&store.lock().unwrap_or_else(|_| panic!("store lock")));
        let partial = limiter(&[1]);
        let registry = ApprovalRegistry::with_store(Arc::clone(&store));
        assert!(matches!(
            registry.replay_released(&tenant(), &partial, &modules()),
            Err(ApprovalError::CorruptRecord)
        ));
        assert_eq!(
            entries(&store.lock().unwrap_or_else(|_| panic!("store lock"))),
            before
        );
        assert_eq!(partial.held_reservations(), Ok(0));
        assert_eq!(partial.held_exposure(LimitId([1; 16])), Ok(0));
        assert_eq!(partial.consumed(LimitId([1; 16])), Ok(0));
    }

    #[test]
    fn released_replay_with_every_limit_known_restores_the_exact_holds() {
        let (_root, store) = persisted("known", 42);
        let before = entries(&store.lock().unwrap_or_else(|_| panic!("store lock")));
        let restarted = limiter(&[1, 2]);
        let registry = ApprovalRegistry::with_store(Arc::clone(&store));
        let released = registry
            .replay_released(&tenant(), &restarted, &modules())
            .unwrap_or_else(|error| panic!("replay: {error:?}"));
        assert_eq!(released.len(), 1);
        assert_eq!(released[0].approval_id, [42; 32]);
        assert_eq!(released[0].submission_ref, [9; 32]);
        assert_eq!(
            restarted.held_limits([42; 32]),
            Ok(vec![LimitId([1; 16]), LimitId([2; 16])])
        );
        assert_eq!(restarted.held_reservations(), Ok(2));
        for id in [1, 2] {
            assert_eq!(restarted.held_exposure(LimitId([id; 16])), Ok(10));
            assert_eq!(restarted.consumed(LimitId([id; 16])), Ok(0));
        }
        assert_eq!(
            entries(&store.lock().unwrap_or_else(|_| panic!("store lock"))),
            before
        );
    }
}
