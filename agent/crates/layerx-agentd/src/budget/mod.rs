//! Protocol-backed and explicitly local spending limits.

#[path = "reconcile.rs"]
mod accounting;
mod create;
mod daemon;
#[path = "divergence.rs"]
mod divergence_reporting;
#[path = "hold.rs"]
mod recovery;
mod mutate;
#[path = "reserve.rs"]
mod reservations;

pub use accounting::{
    budget_state_key, LocalAccounting, ProtocolBudgetRecord, ProtocolBudgetState, ReconcileError,
    ReconciliationState, SpendReceiptEvidence, BUDGET_MODULE_ID,
};
pub use create::{
    budget_create_identity, core_expiry_ms, create_protocol_budget, BudgetCreateIdentity,
    BudgetCreationError, BudgetKind, BudgetPipeline, BudgetRequest, CoreBudgetReceipt, LocalLimit,
    ProtocolBudget,
};
pub use daemon::{
    applicable_daemon_limits, create_daemon_limit, daemon_limit_id,
    daemon_limits, load_daemon_limits, revoke_daemon_limit, DaemonLimitError, DaemonLimitRecord,
};
pub use mutate::{
    budget_mutation_identity, budget_state_context, confirm_budget_mutation, BudgetMutation,
    BudgetMutationPipeline, ConfirmedBudgetMutation,
};
pub use divergence_reporting::{BudgetDivergenceAlert, BudgetHealth, DivergenceAuditRecord};
pub use recovery::{
    PersistedReceipt, RestartAccounting, RestartError, UnknownOutcome, UnknownReservation,
};
pub use reservations::{
    BudgetLimiter, BudgetReservation, CoreTimestampMs, DurableBudgetReservation, LimitConfig,
    LimitId, LimitRefusal, LimitScope, ReleaseKind, ReservationRequest,
};

const ENROLMENT_PREFIX: &[u8] = b"budget/enrolment/limit/";
const ENROLMENT_INDEX_PREFIX: &[u8] = b"budget/daemon/limit-id/";
const ENROLMENT_ID_DOMAIN: &[u8] = b"layerx:budget-enrolment-limit-id:v1\0";
const ENROLMENT_MAGIC: &[u8; 4] = b"LXEL";
const ENROLMENT_VERSION: u8 = 1;
const ENROLMENT_BYTES: usize = 4 + 1 + 32 + 16 + 1 + 32 + 16 + 16 + 16;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EnrolmentLimitRecord {
    pub tenant: crate::store::TenantId,
    pub stable_id: [u8; 32],
    pub limit_id: LimitId,
    pub scope: LimitScope,
    pub enrolment: LimitId,
    pub ceiling: u128,
    pub consumed: u128,
}

fn scope_bytes(scope: LimitScope) -> [u8; 33] {
    let (tag, id) = match scope {
        LimitScope::Tenant(id) => (0_u8, id),
        LimitScope::Agent(id) => (1, id),
        LimitScope::Session(id) => (2, id),
        LimitScope::Capability(id) => (3, id),
        LimitScope::Counterparty(id) => (4, id),
    };
    let mut bytes = [0; 33];
    bytes[0] = tag;
    bytes[1..].copy_from_slice(&id);
    bytes
}

fn scope_from(tag: u8, id: [u8; 32]) -> Result<LimitScope, DaemonLimitError> {
    match tag {
        0 => Ok(LimitScope::Tenant(id)),
        1 => Ok(LimitScope::Agent(id)),
        2 => Ok(LimitScope::Session(id)),
        3 => Ok(LimitScope::Capability(id)),
        4 => Ok(LimitScope::Counterparty(id)),
        _ => Err(DaemonLimitError::Corrupt),
    }
}

#[must_use]
pub fn enrolment_limit_id(
    tenant: &crate::store::TenantId,
    scope: &LimitScope,
    enrolment: &LimitId,
) -> [u8; 32] {
    use sha2::{Digest as _, Sha256};
    let tenant: [u8; 32] = Sha256::digest(tenant.as_str().as_bytes()).into();
    Sha256::new()
        .chain_update(ENROLMENT_ID_DOMAIN)
        .chain_update(tenant)
        .chain_update(scope_bytes(*scope))
        .chain_update(enrolment.0)
        .finalize()
        .into()
}

fn enrolment_fixed<const N: usize>(
    decoder: &mut layerx_wire::decode::Decoder<'_>,
) -> Result<[u8; N], DaemonLimitError> {
    decoder
        .fixed(N)
        .map_err(|_| DaemonLimitError::Corrupt)?
        .try_into()
        .map_err(|_| DaemonLimitError::Corrupt)
}

impl EnrolmentLimitRecord {
    fn encode(&self) -> Result<Vec<u8>, DaemonLimitError> {
        let mut encoder = layerx_wire::encode::Encoder::new(ENROLMENT_BYTES);
        let corrupt = |_| DaemonLimitError::Corrupt;
        encoder.fixed(ENROLMENT_MAGIC).map_err(corrupt)?;
        encoder.u8(ENROLMENT_VERSION).map_err(corrupt)?;
        encoder.fixed(&self.stable_id).map_err(corrupt)?;
        encoder.fixed(&self.limit_id.0).map_err(corrupt)?;
        encoder.fixed(&scope_bytes(self.scope)).map_err(corrupt)?;
        encoder.fixed(&self.enrolment.0).map_err(corrupt)?;
        encoder.u128(self.ceiling).map_err(corrupt)?;
        encoder.u128(self.consumed).map_err(corrupt)?;
        Ok(encoder.finish())
    }

    fn decode(tenant: crate::store::TenantId, bytes: &[u8]) -> Result<Self, DaemonLimitError> {
        let corrupt = |_| DaemonLimitError::Corrupt;
        let mut decoder = layerx_wire::decode::Decoder::new(bytes, 0);
        if decoder.fixed(4).map_err(corrupt)? != ENROLMENT_MAGIC
            || decoder.u8().map_err(corrupt)? != ENROLMENT_VERSION
        {
            return Err(DaemonLimitError::Corrupt);
        }
        let stable_id = enrolment_fixed::<32>(&mut decoder)?;
        let limit_id = LimitId(enrolment_fixed::<16>(&mut decoder)?);
        let tag = decoder.u8().map_err(corrupt)?;
        let scope = scope_from(tag, enrolment_fixed::<32>(&mut decoder)?)?;
        let enrolment = LimitId(enrolment_fixed::<16>(&mut decoder)?);
        let ceiling = decoder.u128().map_err(corrupt)?;
        let consumed = decoder.u128().map_err(corrupt)?;
        decoder.finish().map_err(corrupt)?;
        if stable_id != enrolment_limit_id(&tenant, &scope, &enrolment)
            || limit_id != daemon_limit_id(stable_id)
            || ceiling == 0
            || consumed > ceiling
        {
            return Err(DaemonLimitError::Corrupt);
        }
        Ok(Self {
            tenant,
            stable_id,
            limit_id,
            scope,
            enrolment,
            ceiling,
            consumed,
        })
    }

    fn config(&self) -> LimitConfig {
        LimitConfig {
            id: self.limit_id,
            name: format!(
                "enrolment limit {}",
                self.stable_id
                    .iter()
                    .map(|byte| format!("{byte:02x}"))
                    .collect::<String>()
            ),
            scope: self.scope,
            ceiling: self.ceiling,
            consumed: self.consumed,
        }
    }
}

fn enrolment_key(
    tenant: &crate::store::TenantId,
    stable_id: [u8; 32],
) -> Result<crate::store::TenantKey, DaemonLimitError> {
    Ok(crate::store::TenantKey::new(
        tenant.clone(),
        crate::store::ObjectKind::Configuration,
        [ENROLMENT_PREFIX, &stable_id].concat(),
    )?)
}

fn enrolment_index_key(
    tenant: &crate::store::TenantId,
    limit_id: LimitId,
) -> Result<crate::store::TenantKey, DaemonLimitError> {
    Ok(crate::store::TenantKey::new(
        tenant.clone(),
        crate::store::ObjectKind::Configuration,
        [ENROLMENT_INDEX_PREFIX, &limit_id.0].concat(),
    )?)
}

fn stored_enrolment(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    stable_id: [u8; 32],
) -> Result<Option<EnrolmentLimitRecord>, DaemonLimitError> {
    store
        .get(&enrolment_key(tenant, stable_id)?)
        .map(|value| EnrolmentLimitRecord::decode(tenant.clone(), value.bytes()))
        .transpose()
}

fn enrolment_indexed(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    limit_id: LimitId,
    stable_id: [u8; 32],
) -> Result<bool, DaemonLimitError> {
    Ok(store
        .get(&enrolment_index_key(tenant, limit_id)?)
        .is_some_and(|value| value.bytes() == stable_id.as_slice()))
}

fn install_once(
    limiter: &BudgetLimiter,
    record: &EnrolmentLimitRecord,
) -> Result<(), DaemonLimitError> {
    match limiter.consumed(record.limit_id) {
        Ok(_) => Ok(()),
        Err(LimitRefusal::UnknownLimit(_)) => Ok(limiter.install(record.config())?),
        Err(refusal) => Err(refusal.into()),
    }
}

/// # Errors
/// Returns `Invalid` for a zero ceiling or consumed above it, `Conflict` for a stored record whose ceiling differs, `IdCollision` for a truncated-identifier collision, `Corrupt` for a malformed record or index, and store or limiter failures.
pub fn install_enrolment_limits(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
) -> Result<Vec<EnrolmentLimitRecord>, DaemonLimitError> {
    let mut installed = Vec::new();
    for config in verified {
        if config.ceiling == 0 || config.consumed > config.ceiling {
            return Err(DaemonLimitError::Invalid);
        }
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let limit_id = daemon_limit_id(stable_id);
        let key = enrolment_key(tenant, stable_id)?;
        let record = if let Some(mut existing) = stored_enrolment(store, tenant, stable_id)? {
            if existing.ceiling != config.ceiling {
                return Err(DaemonLimitError::Conflict);
            }
            if !enrolment_indexed(store, tenant, limit_id, stable_id)? {
                return Err(DaemonLimitError::Corrupt);
            }
            if config.consumed > existing.consumed {
                existing.consumed = config.consumed;
                store.update_local_batch(vec![(key, existing.encode()?)])?;
            }
            existing
        } else {
            let index = enrolment_index_key(tenant, limit_id)?;
            if let Some(value) = store.get(&index) {
                if value.bytes() != stable_id.as_slice() {
                    return Err(DaemonLimitError::IdCollision);
                }
                return Err(DaemonLimitError::Corrupt);
            }
            let record = EnrolmentLimitRecord {
                tenant: tenant.clone(),
                stable_id,
                limit_id,
                scope: config.scope,
                enrolment: config.id,
                ceiling: config.ceiling,
                consumed: config.consumed,
            };
            let bytes = record.encode()?;
            store.put_local(index.clone(), stable_id.to_vec())?;
            if let Err(error) = store.put_local(key, bytes) {
                store.remove_local(&index)?;
                return Err(error.into());
            }
            record
        };
        install_once(limiter, &record)?;
        installed.push(record);
    }
    for object_id in store.list_object_ids(tenant, crate::store::ObjectKind::Configuration) {
        let Some(stable_id) = object_id.strip_prefix(ENROLMENT_PREFIX) else {
            continue;
        };
        let stable_id: [u8; 32] = stable_id
            .try_into()
            .map_err(|_| DaemonLimitError::Corrupt)?;
        if installed.iter().any(|record| record.stable_id == stable_id) {
            continue;
        }
        let record =
            stored_enrolment(store, tenant, stable_id)?.ok_or(DaemonLimitError::Corrupt)?;
        if !enrolment_indexed(store, tenant, record.limit_id, stable_id)? {
            return Err(DaemonLimitError::Corrupt);
        }
        install_once(limiter, &record)?;
        limiter.retire(record.limit_id)?;
        installed.push(record);
    }
    Ok(installed)
}

/// # Errors
/// Returns `Unknown` when a hit verified limit has no durable enrolment record, `Corrupt` for a record or index that disagrees with the verified limit, and store failures.
pub fn enrolment_charge_limits(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
    disclosure: &layerx_crypto::disclosure::Disclosure,
    presented: &[LimitScope],
) -> Result<Vec<LimitId>, DaemonLimitError> {
    let mut limits = Vec::new();
    for config in verified {
        let hit = presented.contains(&config.scope)
            || disclosure.counterparties.iter().any(|counterparty| {
                config.scope == LimitScope::Counterparty(counterparty.account)
            });
        if !hit {
            continue;
        }
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let record =
            stored_enrolment(store, tenant, stable_id)?.ok_or(DaemonLimitError::Unknown)?;
        if record.ceiling != config.ceiling
            || !enrolment_indexed(store, tenant, record.limit_id, stable_id)?
        {
            return Err(DaemonLimitError::Corrupt);
        }
        limits.push(config.id);
        limits.push(record.limit_id);
    }
    Ok(limits)
}

/// # Errors
/// Returns `Corrupt` for an enrolment hold whose record, scope or ceiling disagrees, `Arithmetic` on overflow or a consumed total above the ceiling, and every daemon-limit consumption failure.
pub fn consumption_updates(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    holds: &[DurableBudgetReservation],
) -> Result<Vec<(crate::store::TenantKey, Vec<u8>)>, DaemonLimitError> {
    let mut enrolments: Vec<EnrolmentLimitRecord> = Vec::new();
    let mut daemon_holds = Vec::new();
    for hold in holds {
        let enrolment = match store.get(&enrolment_index_key(tenant, hold.limit_id)?) {
            Some(value) => {
                let stable_id: [u8; 32] = value
                    .bytes()
                    .try_into()
                    .map_err(|_| DaemonLimitError::Corrupt)?;
                stored_enrolment(store, tenant, stable_id)?
            }
            None => None,
        };
        let Some(enrolment) = enrolment else {
            daemon_holds.push(hold.clone());
            continue;
        };
        if enrolment.limit_id != hold.limit_id {
            return Err(DaemonLimitError::Corrupt);
        }
        let position = match enrolments
            .iter()
            .position(|record| record.stable_id == enrolment.stable_id)
        {
            Some(position) => position,
            None => {
                enrolments.push(enrolment);
                enrolments.len() - 1
            }
        };
        let record = &mut enrolments[position];
        if hold.scope != record.scope || hold.ceiling != record.ceiling {
            return Err(DaemonLimitError::Corrupt);
        }
        record.consumed = record
            .consumed
            .checked_add(hold.amount)
            .filter(|consumed| *consumed <= record.ceiling)
            .ok_or(DaemonLimitError::Arithmetic)?;
    }
    let mut updates = daemon::consumption_updates(store, tenant, &daemon_holds)?;
    for record in &enrolments {
        updates.push((enrolment_key(tenant, record.stable_id)?, record.encode()?));
    }
    Ok(updates)
}

/// Reconciles local budget cache state against verified protocol evidence.
///
/// # Errors
///
/// Returns `ProtocolStateSchemaUnavailable` after authenticating the included
/// candidate state if it does not follow the canonical budget record schema.
/// Receipt verification, activity binding, and replay rejection happen before
/// that fail-closed result.
pub fn reconcile(
    local: &mut LocalAccounting,
    protocol: &ProtocolBudgetState,
    receipts: &[SpendReceiptEvidence],
    verifier: &crate::protocol_evidence::EvidenceAuthority,
) -> Result<ReconciliationState, ReconcileError> {
    accounting::reconcile_state(local, protocol, receipts, verifier)
}

/// Atomically reserves against every applicable scope.
///
/// # Errors
///
/// Returns `InvalidRequest` for a zero amount, an expiry at or before the current sequence or
/// an empty limit list, `UnknownLimit` for an unconfigured scope, `Exceeded` when consumed plus
/// held plus the request passes a ceiling, `Arithmetic` on overflow, or `Poisoned`.
pub fn reserve(
    limiter: &BudgetLimiter,
    request: &ReservationRequest,
) -> Result<BudgetReservation, LimitRefusal> {
    reservations::reserve_all(limiter, request)
}

/// Atomically reserves against every applicable scope with a core-clock deadline in addition to
/// the head-sequence bound.
///
/// # Errors
///
/// Returns `InvalidRequest` for a deadline at or before verified core time (equality is
/// expired), plus every refusal of `reserve`, including `Retired` for a revoked limit.
pub fn reserve_until_core_time(
    limiter: &BudgetLimiter,
    request: &ReservationRequest,
    deadline: CoreTimestampMs,
    core_now: CoreTimestampMs,
) -> Result<BudgetReservation, LimitRefusal> {
    reservations::reserve_until(limiter, request, deadline, core_now)
}

/// Releases a time-bounded hold whose core deadline was reached.
///
/// # Errors
///
/// Returns `Poisoned` when the limiter lock is poisoned.
pub fn release_expired_core_time(
    limiter: &BudgetLimiter,
    reservation_id: [u8; 32],
    core_now: CoreTimestampMs,
) -> Result<bool, LimitRefusal> {
    reservations::release_core_expired(limiter, reservation_id, core_now)
}

/// Restores durable reservations with their core-clock deadlines before writes are admitted.
///
/// # Errors
///
/// Returns a limit refusal if a durable reservation cannot be restored.
pub fn restore_bounded(
    limiter: &BudgetLimiter,
    reservations: &[(DurableBudgetReservation, Option<CoreTimestampMs>)],
) -> Result<(), LimitRefusal> {
    reservations::restore_bounded(limiter, reservations)
}

/// Deterministically releases or consumes one reservation.
///
/// # Errors
///
/// Returns `Arithmetic` when an `Executed` release overflows a limit's consumed total, or
/// `Poisoned` when the limiter lock is poisoned; a reservation no limit holds is `Ok(false)`.
pub fn release(
    limiter: &BudgetLimiter,
    reservation_id: [u8; 32],
    kind: ReleaseKind,
    current_sequence: u64,
) -> Result<bool, LimitRefusal> {
    reservations::release_all(limiter, reservation_id, kind, current_sequence)
}

/// Restores canonical durable reservations before the limiter is made ready.
///
/// # Errors
///
/// Returns a limit refusal if a durable reservation cannot be restored.
pub fn restore(
    limiter: &BudgetLimiter,
    reservations: &[DurableBudgetReservation],
) -> Result<(), LimitRefusal> {
    reservations::restore_all(limiter, reservations)
}

/// Persists a reservation whose submission outcome is unknown.
///
/// # Errors
///
/// Returns `RestartError::Store` for the I/O or `SizeOverflow` failure raised while the store
/// is written to disk; the in-memory entry is rolled back so nothing is half-persisted.
pub fn hold_unknown(
    store: &mut crate::store::Store,
    reservation: &UnknownReservation,
) -> Result<(), RestartError> {
    recovery::persist_unknown(store, reservation)
}

/// Rebuilds held and consumed accounting before writes are admitted.
///
/// # Errors
///
/// Returns typed verification, activity-identity, and replay failures before
/// rebuilding receipt consumption. The resulting accounting remains blocked by
/// `ProtocolStateSchemaUnavailable` because core defines no canonical budget
/// record/key schema. Returns `Corrupt` for malformed reservations, `Arithmetic`
/// on overflow, and `Store` for storage failures.
pub fn rebuild(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    unknown_ids: &[[u8; 32]],
    receipts: &[PersistedReceipt],
    protocol: &ProtocolBudgetState,
    verifier: &crate::protocol_evidence::EvidenceAuthority,
) -> Result<RestartAccounting, RestartError> {
    recovery::rebuild_accounting(store, tenant, unknown_ids, receipts, protocol, verifier)
}

/// Raises an explicit alert for a local/protocol mismatch.
#[must_use]
pub fn divergence_alert(
    state: &ReconciliationState,
    local_ceiling: u128,
) -> Option<BudgetDivergenceAlert> {
    divergence_reporting::build_alert(state, local_ceiling)
}
