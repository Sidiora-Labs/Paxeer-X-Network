//! Protocol-backed and explicitly local spending limits.

#[path = "reconcile.rs"]
mod accounting;
mod create;
mod daemon;
#[path = "divergence.rs"]
mod divergence_reporting;
mod mutate;
pub mod budget_proof;
mod program_sources;
pub mod program_settlement;
pub use program_sources::{
    read_program_budget_sources, read_native_effect_budget_sources, ProgramSourceError, ResolvedProgramCharge,
    ResolvedProgramSource, VerifiedResolvedProgramCharges, VerifiedProgramBudgetAllocations,
};
#[path = "hold.rs"]
mod recovery;
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
    applicable_daemon_limits, create_daemon_limit, daemon_limit_id, daemon_limits,
    load_daemon_limits, revoke_daemon_limit, DaemonLimitError, DaemonLimitRecord,
};
pub use divergence_reporting::{BudgetDivergenceAlert, BudgetHealth, DivergenceAuditRecord};
pub use mutate::{
    budget_mutation_identity, budget_state_context, confirm_budget_mutation, BudgetMutation,
    BudgetMutationPipeline, ConfirmedBudgetMutation,
};
pub use recovery::{
    PersistedReceipt, RestartAccounting, RestartError, UnknownOutcome, UnknownReservation,
};
pub use reservations::{
    BudgetLimiter, BudgetReservation, CoreTimestampMs, DurableBudgetReservation, LimitConfig,
    LimitId, LimitRefusal, LimitScope, ReleaseKind, ReservationRequest, StagedRelease,
    ProgramLimitDenomination, ProgramChargeKind, ProgramBudgetCharge, ProgramReservationRequest,
    ProgramBudgetHold, ProgramBudgetReservation, StagedProgramReservation,
    ProgramBudgetAllocation, StagedProgramSettlement,
};

const ENROLMENT_PREFIX: &[u8] = b"budget/enrolment/limit/";
const ENROLMENT_INDEX_PREFIX: &[u8] = b"budget/daemon/limit-id/";
const ENROLMENT_ID_DOMAIN: &[u8] = b"layerx:budget-enrolment-limit-id:v1\0";
const ENROLMENT_MAGIC: &[u8; 4] = b"LXEL";
const ENROLMENT_VERSION: u8 = 1;
const ENROLMENT_RETIRED_VERSION: u8 = 2;
const ENROLMENT_BYTES: usize = 4 + 1 + 32 + 16 + 1 + 32 + 16 + 16 + 16;
const ENROLMENT_RETIRED_BYTES: usize = ENROLMENT_BYTES + 32;

/// One durable enrolment limit. A live record has no successor; a record retired by a renewal
/// names the stable identity that carried its consumed total forward.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EnrolmentLimitRecord {
    pub tenant: crate::store::TenantId,
    pub stable_id: [u8; 32],
    pub limit_id: LimitId,
    pub scope: LimitScope,
    pub enrolment: LimitId,
    pub ceiling: u128,
    pub consumed: u128,
    pub successor: Option<[u8; 32]>,
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

/// Derives the durable stable identity of one enrolment limit from the tenant, the complete
/// canonical scope (variant tag and full 32-byte value) and the authenticated operator-declared
/// `LimitConfig.id`. The declared identifier is the configuration identity; it is not a policy
/// digest.
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
        let (version, capacity) = match self.successor {
            None => (ENROLMENT_VERSION, ENROLMENT_BYTES),
            Some(_) => (ENROLMENT_RETIRED_VERSION, ENROLMENT_RETIRED_BYTES),
        };
        let mut encoder = layerx_wire::encode::Encoder::new(capacity);
        let corrupt = |_| DaemonLimitError::Corrupt;
        encoder.fixed(ENROLMENT_MAGIC).map_err(corrupt)?;
        encoder.u8(version).map_err(corrupt)?;
        encoder.fixed(&self.stable_id).map_err(corrupt)?;
        encoder.fixed(&self.limit_id.0).map_err(corrupt)?;
        encoder.fixed(&scope_bytes(self.scope)).map_err(corrupt)?;
        encoder.fixed(&self.enrolment.0).map_err(corrupt)?;
        encoder.u128(self.ceiling).map_err(corrupt)?;
        encoder.u128(self.consumed).map_err(corrupt)?;
        if let Some(successor) = &self.successor {
            encoder.fixed(successor).map_err(corrupt)?;
        }
        Ok(encoder.finish())
    }

    fn decode(tenant: crate::store::TenantId, bytes: &[u8]) -> Result<Self, DaemonLimitError> {
        let corrupt = |_| DaemonLimitError::Corrupt;
        let mut decoder = layerx_wire::decode::Decoder::new(bytes, 0);
        if decoder.fixed(4).map_err(corrupt)? != ENROLMENT_MAGIC {
            return Err(DaemonLimitError::Corrupt);
        }
        let version = decoder.u8().map_err(corrupt)?;
        if version != ENROLMENT_VERSION && version != ENROLMENT_RETIRED_VERSION {
            return Err(DaemonLimitError::Corrupt);
        }
        let stable_id = enrolment_fixed::<32>(&mut decoder)?;
        let limit_id = LimitId(enrolment_fixed::<16>(&mut decoder)?);
        let tag = decoder.u8().map_err(corrupt)?;
        let scope = scope_from(tag, enrolment_fixed::<32>(&mut decoder)?)?;
        let enrolment = LimitId(enrolment_fixed::<16>(&mut decoder)?);
        let ceiling = decoder.u128().map_err(corrupt)?;
        let consumed = decoder.u128().map_err(corrupt)?;
        let successor = if version == ENROLMENT_RETIRED_VERSION {
            Some(enrolment_fixed::<32>(&mut decoder)?)
        } else {
            None
        };
        decoder.finish().map_err(corrupt)?;
        if stable_id != enrolment_limit_id(&tenant, &scope, &enrolment)
            || limit_id != daemon_limit_id(stable_id)
            || ceiling == 0
            || consumed > ceiling
            || successor.is_some_and(|successor| successor == [0; 32] || successor == stable_id)
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
            successor,
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

fn refuse_index_taken(
    store: &crate::store::Store,
    index: &crate::store::TenantKey,
    stable_id: [u8; 32],
) -> Result<(), DaemonLimitError> {
    match store.get(index) {
        Some(value) if value.bytes() != stable_id.as_slice() => Err(DaemonLimitError::IdCollision),
        Some(_) => Err(DaemonLimitError::Corrupt),
        None => Ok(()),
    }
}

fn install_once(
    limiter: &BudgetLimiter,
    record: &EnrolmentLimitRecord,
) -> Result<(), DaemonLimitError> {
    match limiter.consumed(record.limit_id) {
        Ok(_) => Ok(limiter.refresh_consumed(record.limit_id, record.consumed)?),
        Err(LimitRefusal::UnknownLimit(_)) => Ok(limiter.install(record.config())?),
        Err(refusal) => Err(refusal.into()),
    }
}

fn reinstall_enrolment(
    store: &mut crate::store::Store,
    tenant: &crate::store::TenantId,
    config: &LimitConfig,
) -> Result<EnrolmentLimitRecord, DaemonLimitError> {
    let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
    let limit_id = daemon_limit_id(stable_id);
    let key = enrolment_key(tenant, stable_id)?;
    if let Some(mut existing) = stored_enrolment(store, tenant, stable_id)? {
        if existing.successor.is_some() || existing.ceiling != config.ceiling {
            return Err(DaemonLimitError::Conflict);
        }
        if !enrolment_indexed(store, tenant, limit_id, stable_id)? {
            return Err(DaemonLimitError::Corrupt);
        }
        if config.consumed > existing.consumed {
            existing.consumed = config.consumed;
            store.update_local_batch(vec![(key, existing.encode()?)])?;
        }
        return Ok(existing);
    }
    let index = enrolment_index_key(tenant, limit_id)?;
    refuse_index_taken(store, &index, stable_id)?;
    let record = EnrolmentLimitRecord {
        tenant: tenant.clone(),
        stable_id,
        limit_id,
        scope: config.scope,
        enrolment: config.id,
        ceiling: config.ceiling,
        consumed: config.consumed,
        successor: None,
    };
    let bytes = record.encode()?;
    store.put_local(index.clone(), stable_id.to_vec())?;
    if let Err(error) = store.put_local(key, bytes) {
        store.remove_local(&index)?;
        return Err(error.into());
    }
    Ok(record)
}

fn renew_enrolment(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    tenant: &crate::store::TenantId,
    config: &LimitConfig,
    predecessor: LimitId,
) -> Result<(EnrolmentLimitRecord, Option<EnrolmentLimitRecord>), DaemonLimitError> {
    let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
    let current = enrolment_limit_id(tenant, &config.scope, &predecessor);
    let mut previous =
        stored_enrolment(store, tenant, current)?.ok_or(DaemonLimitError::Conflict)?;
    if stored_enrolment(store, tenant, stable_id)?.is_some() {
        if previous.successor != Some(stable_id) {
            return Err(DaemonLimitError::Conflict);
        }
        return Ok((reinstall_enrolment(store, tenant, config)?, None));
    }
    if previous.successor.is_some() {
        return Err(DaemonLimitError::Conflict);
    }
    if !enrolment_indexed(store, tenant, previous.limit_id, current)? {
        return Err(DaemonLimitError::Corrupt);
    }
    let limit_id = daemon_limit_id(stable_id);
    let index = enrolment_index_key(tenant, limit_id)?;
    refuse_index_taken(store, &index, stable_id)?;
    let mut record = EnrolmentLimitRecord {
        tenant: tenant.clone(),
        stable_id,
        limit_id,
        scope: config.scope,
        enrolment: config.id,
        ceiling: config.ceiling,
        consumed: previous.consumed.max(config.consumed),
        successor: None,
    };
    let renewed = limiter.renew_locked(
        &previous.config(),
        &record.config(),
        |consumed| -> Result<(), DaemonLimitError> {
            record.consumed = consumed;
            previous.successor = Some(stable_id);
            let mut companions = vec![
                (index, stable_id.to_vec()),
                (enrolment_key(tenant, stable_id)?, record.encode()?),
            ];
            if let Some(denomination) = stored_program_denomination(store, tenant, current)? {
                companions.push((program_denomination_key(tenant, stable_id)?,
                    encode_program_denomination(stable_id, denomination)));
            }
            store.update_local_batch_with_companions(
                vec![(enrolment_key(tenant, current)?, previous.encode()?)], companions,
            )?;
            Ok(())
        },
    );
    match renewed {
        Err(DaemonLimitError::Limit(LimitRefusal::Exceeded { .. })) => {
            return Err(DaemonLimitError::Conflict)
        }
        other => other?,
    }
    Ok((record, Some(previous)))
}

/// An authenticated renewal of one verified enrolment limit: the verified limit declared as
/// `successor` replaces the live limit declared as `predecessor` for the same tenant and scope.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EnrolmentPredecessor {
    pub successor: LimitId,
    pub predecessor: LimitId,
}

/// Installs every verified enrolment limit as an independent limit and every stored one,
/// retiring the stored ones that are no longer verified.
///
/// # Errors
/// Returns every error of `install_enrolment_renewals`.
pub fn install_enrolment_limits(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
) -> Result<Vec<EnrolmentLimitRecord>, DaemonLimitError> {
    install_enrolment_renewals(store, limiter, tenant, verified, &[])
}

/// Installs every verified enrolment limit and every stored one, retiring the stored ones that
/// are no longer verified.
///
/// Limits sharing one scope are independent constraints. A renewal exists only when
/// `predecessors` names, for a verified limit, the declared identifier of the live limit it
/// replaces. The successor is created with the predecessor's consumed total (or the declared
/// one, whichever is larger) after the full exposure, consumed plus every outstanding hold of
/// the predecessor lineage, is checked against the new ceiling. The caller holds the store lock
/// and the limiter lock is held from that check through the one store batch, which marks the
/// predecessor retired with a pointer to its successor, until the successor, the link and the
/// retirement are published together, so no admission interleaves; its holds keep their
/// identifiers and count against the successor. A renewal needs its predecessor installed with
/// its preserved holds restored first, so at startup the base install and every hold restore
/// precede it. An identity already in the limiter takes the refreshed persisted consumed total.
///
/// # Errors
/// Returns `Invalid` for a zero ceiling, consumed above it, a self renewal or two renewals of one verified limit, `Conflict` for a stored record whose ceiling differs, a retired limit declared again, a renewal whose predecessor is not a live limit of the same scope or whose full exposure passes the new ceiling, `Limit(UnknownLimit)` for a renewal whose predecessor is not installed in the limiter, `IdCollision` for a truncated-identifier collision, `Corrupt` for a malformed record or index, and store or limiter failures.
pub fn install_enrolment_renewals(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
    predecessors: &[EnrolmentPredecessor],
) -> Result<Vec<EnrolmentLimitRecord>, DaemonLimitError> {
    let mut installed = Vec::new();
    for config in verified {
        if config.ceiling == 0 || config.consumed > config.ceiling {
            return Err(DaemonLimitError::Invalid);
        }
        let mut named = predecessors
            .iter()
            .filter(|renewal| renewal.successor == config.id);
        let renewal = named.next();
        if named.next().is_some()
            || renewal.is_some_and(|renewal| renewal.predecessor == renewal.successor)
        {
            return Err(DaemonLimitError::Invalid);
        }
        let (record, previous) = match renewal {
            Some(renewal) => renew_enrolment(store, limiter, tenant, config, renewal.predecessor)?,
            None => (reinstall_enrolment(store, tenant, config)?, None),
        };
        if previous.is_none() {
            install_once(limiter, &record)?;
        }
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
    let mut program_bindings = Vec::new();
    for record in &installed {
        if let Some(denomination) = stored_program_denomination(store, tenant, record.stable_id)? {
            program_bindings.push((record.limit_id, denomination, true, false));
        }
    }
    limiter.bind_program_denominations(&program_bindings, || Ok::<(), DaemonLimitError>(()))?;
    for record in &installed {
        if let Some(successor) = record.successor {
            limiter.link_successor(record.limit_id, daemon_limit_id(successor))?;
        }
    }
    Ok(installed)
}

/// # Errors
/// Returns `Unknown` when a hit verified limit has no durable enrolment record, `Corrupt` for a record or index that disagrees with the verified limit or is retired, and store failures. Each hit verified limit yields exactly its one canonical enrolment identity.
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
            || disclosure
                .counterparties
                .iter()
                .any(|counterparty| config.scope == LimitScope::Counterparty(counterparty.account));
        if !hit {
            continue;
        }
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let record =
            stored_enrolment(store, tenant, stable_id)?.ok_or(DaemonLimitError::Unknown)?;
        if record.ceiling != config.ceiling
            || record.successor.is_some()
            || !enrolment_indexed(store, tenant, record.limit_id, stable_id)?
        {
            return Err(DaemonLimitError::Corrupt);
        }
        limits.push(record.limit_id);
    }
    Ok(limits)
}

fn enrolment_position(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    enrolments: &mut Vec<EnrolmentLimitRecord>,
    stable_id: [u8; 32],
) -> Result<Option<usize>, DaemonLimitError> {
    if let Some(position) = enrolments
        .iter()
        .position(|record| record.stable_id == stable_id)
    {
        return Ok(Some(position));
    }
    let Some(record) = stored_enrolment(store, tenant, stable_id)? else {
        return Ok(None);
    };
    enrolments.push(record);
    Ok(Some(enrolments.len() - 1))
}

fn charge_enrolment(
    record: &mut EnrolmentLimitRecord,
    amount: u128,
) -> Result<(), DaemonLimitError> {
    record.consumed = record
        .consumed
        .checked_add(amount)
        .filter(|consumed| *consumed <= record.ceiling)
        .ok_or(DaemonLimitError::Arithmetic)?;
    Ok(())
}

/// Builds the durable consumed-total updates for executed holds. A hold on a retired enrolment
/// identity charges that record and the live successor its lineage chain names.
///
/// # Errors
/// Returns `Corrupt` for an enrolment hold whose record, scope or ceiling disagrees or whose successor chain is missing, cyclic or crosses scopes, `Arithmetic` on overflow or a consumed total above the ceiling, and every daemon-limit consumption failure.
pub fn consumption_updates(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    holds: &[DurableBudgetReservation],
) -> Result<Vec<(crate::store::TenantKey, Vec<u8>)>, DaemonLimitError> {
    let mut enrolments: Vec<EnrolmentLimitRecord> = Vec::new();
    let mut daemon_holds = Vec::new();
    for hold in holds {
        let position = match store.get(&enrolment_index_key(tenant, hold.limit_id)?) {
            Some(value) => {
                let stable_id: [u8; 32] = value
                    .bytes()
                    .try_into()
                    .map_err(|_| DaemonLimitError::Corrupt)?;
                enrolment_position(store, tenant, &mut enrolments, stable_id)?
            }
            None => None,
        };
        let Some(position) = position else {
            daemon_holds.push(hold.clone());
            continue;
        };
        let record = &mut enrolments[position];
        if record.limit_id != hold.limit_id
            || hold.scope != record.scope
            || hold.ceiling != record.ceiling
        {
            return Err(DaemonLimitError::Corrupt);
        }
        charge_enrolment(record, hold.amount)?;
        let scope = record.scope;
        let mut visited = vec![record.stable_id];
        let mut successor = record.successor;
        while let Some(next) = successor {
            if visited.contains(&next) {
                return Err(DaemonLimitError::Corrupt);
            }
            visited.push(next);
            let position = enrolment_position(store, tenant, &mut enrolments, next)?
                .ok_or(DaemonLimitError::Corrupt)?;
            let record = &mut enrolments[position];
            if record.scope != scope {
                return Err(DaemonLimitError::Corrupt);
            }
            if record.successor.is_none() {
                charge_enrolment(record, hold.amount)?;
            }
            successor = record.successor;
        }
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

/// Stages the limiter side of one settlement before the store write. The returned stage holds
/// the limiter lock, so admission is excluded until it is published or dropped; dropping it
/// after a failed store write leaves the cache unchanged.
///
/// # Errors
///
/// Returns `Arithmetic` when an `Executed` release overflows a consumed total, or `Poisoned`.
pub fn stage_release(
    limiter: &BudgetLimiter,
    reservation_id: [u8; 32],
    kind: ReleaseKind,
    current_sequence: u64,
) -> Result<StagedRelease<'_>, LimitRefusal> {
    limiter.stage_release(reservation_id, kind, current_sequence)
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

const PROGRAM_DENOMINATION_PREFIX: &[u8] = b"budget/enrolment/denomination/";

fn program_denomination_key(tenant: &crate::store::TenantId, stable_id: [u8; 32])
    -> Result<crate::store::TenantKey, DaemonLimitError>
{
    Ok(crate::store::TenantKey::new(tenant.clone(), crate::store::ObjectKind::Configuration,
        [PROGRAM_DENOMINATION_PREFIX, &stable_id].concat())?)
}

fn encode_program_denomination(stable_id: [u8; 32], denomination: ProgramLimitDenomination) -> Vec<u8> {
    let mut bytes = b"LXPD\x01".to_vec();
    bytes.extend(stable_id); bytes.extend(denomination.asset);
    bytes.push(u8::from(denomination.source.is_some()));
    bytes.extend(denomination.source.unwrap_or([0; 32]));
    bytes
}

fn stored_program_denomination(store: &crate::store::Store, tenant: &crate::store::TenantId, stable_id: [u8; 32])
    -> Result<Option<ProgramLimitDenomination>, DaemonLimitError>
{
    let Some(value) = store.get(&program_denomination_key(tenant, stable_id)?) else { return Ok(None); };
    let bytes = value.bytes();
    if bytes.len() != 102 || &bytes[..5] != b"LXPD\x01" || bytes[5..37] != stable_id {
        return Err(DaemonLimitError::Corrupt);
    }
    let asset = bytes[37..69].try_into().map_err(|_| DaemonLimitError::Corrupt)?;
    let source: [u8; 32] = bytes[70..102].try_into().map_err(|_| DaemonLimitError::Corrupt)?;
    let source = match bytes[69] {
        0 if source == [0; 32] => None,
        1 if source != [0; 32] => Some(source),
        _ => return Err(DaemonLimitError::Corrupt),
    };
    let denomination = ProgramLimitDenomination { asset, source };
    if !denomination.valid() { return Err(DaemonLimitError::Corrupt); }
    Ok(Some(denomination))
}

pub fn install_program_enrolment_denominations(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    verified: &crate::enrolment::VerifiedProgramBudgetDenominations,
) -> Result<(), DaemonLimitError> {
    let tenant = verified.tenant();
    let mut updates = Vec::new();
    let mut companions = Vec::new();
    let mut bindings = Vec::new();
    for (stable_id, denomination) in verified.bindings() {
        let record = stored_enrolment(store, tenant, *stable_id)?.ok_or(DaemonLimitError::Unknown)?;
        if record.successor.is_some() || !enrolment_indexed(store, tenant, record.limit_id, *stable_id)? {
            return Err(DaemonLimitError::Conflict);
        }
        let stored = stored_program_denomination(store, tenant, *stable_id)?;
        if stored.is_some_and(|previous| previous != *denomination)
            || (stored.is_none() && record.consumed != 0)
        { return Err(DaemonLimitError::Conflict); }
        if stored.is_none() {
            updates.push((enrolment_key(tenant, *stable_id)?, record.encode()?));
            companions.push((program_denomination_key(tenant, *stable_id)?, encode_program_denomination(*stable_id, *denomination)));
        }
        bindings.push((record.limit_id, *denomination, stored.is_some(), false));
    }
    limiter.bind_program_denominations(&bindings, || -> Result<(), DaemonLimitError> {
        if !companions.is_empty() { store.update_local_batch_with_companions(updates, companions)?; }
        Ok(())
    })
}

pub fn program_enrolment_charge_limits(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
    disclosure: &layerx_crypto::disclosure::Disclosure,
    presented: &[LimitScope],
    source: [u8; 32],
    asset: [u8; 32],
) -> Result<Vec<LimitId>, DaemonLimitError> {
    if source == [0; 32] || asset == [0; 32] { return Err(DaemonLimitError::Invalid); }
    let applicable = enrolment_charge_limits(store, tenant, verified, disclosure, presented)?;
    let mut selected = Vec::new();
    for id in applicable {
        let index = store.get(&enrolment_index_key(tenant, id)?).ok_or(DaemonLimitError::Unknown)?;
        let stable_id = index.bytes().try_into().map_err(|_| DaemonLimitError::Corrupt)?;
        let denomination = stored_program_denomination(store, tenant, stable_id)?
            .ok_or(DaemonLimitError::AmbiguousDenomination)?;
        if denomination.matches(source, asset) { selected.push(id); }
    }
    selected.sort_unstable();
    selected.dedup();
    Ok(selected)
}

pub fn program_consumption_updates(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    record: &ProgramBudgetReservation,
) -> Result<Vec<(crate::store::TenantKey, Vec<u8>)>, DaemonLimitError> {
    record.validate()?;
    if record.allocations().is_some() { return Err(DaemonLimitError::Invalid); }
    let mut holds = Vec::new();
    for hold in &record.holds {
        let denomination = match store.get(&enrolment_index_key(tenant, hold.reservation.limit_id)?) {
            Some(index) => {
                let stable_id = index.bytes().try_into().map_err(|_| DaemonLimitError::Corrupt)?;
                if let Some(stored) = stored_enrolment(store, tenant, stable_id)? {
                    if stored.limit_id != hold.reservation.limit_id { return Err(DaemonLimitError::Corrupt); }
                    stored_program_denomination(store, tenant, stable_id)?.ok_or(DaemonLimitError::AmbiguousDenomination)?
                } else {
                    daemon::program_denomination(store, tenant, hold.reservation.limit_id)?
                }
            }
            None => daemon::program_denomination(store, tenant, hold.reservation.limit_id)?,
        };
        if denomination != hold.denomination { return Err(DaemonLimitError::Conflict); }
        holds.push(hold.reservation.clone());
    }
    consumption_updates(store, tenant, &holds)
}

pub fn program_allocation_charge_limits(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
    presented: &[LimitScope],
    charge: &ResolvedProgramCharge,
) -> Result<Vec<LimitId>, DaemonLimitError> {
    if charge.source.account() == [0; 32] || charge.asset == [0; 32] {
        return Err(DaemonLimitError::Invalid);
    }
    let mut limits = Vec::new();
    for config in verified {
        let hit = match config.scope {
            LimitScope::Tenant(_) => true,
            LimitScope::Counterparty(account) => charge.destination == Some(account),
            _ => presented.contains(&config.scope),
        };
        if !hit { continue; }
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let record = stored_enrolment(store, tenant, stable_id)?.ok_or(DaemonLimitError::Unknown)?;
        if record.ceiling != config.ceiling || record.scope != config.scope || record.enrolment != config.id
            || record.successor.is_some() || !enrolment_indexed(store, tenant, record.limit_id, stable_id)?
        { return Err(DaemonLimitError::Corrupt); }
        let denomination = stored_program_denomination(store, tenant, stable_id)?
            .ok_or(DaemonLimitError::AmbiguousDenomination)?;
        if denomination.matches(charge.source.account(), charge.asset) { limits.push(record.limit_id); }
    }
    limits.sort_unstable(); limits.dedup();
    Ok(limits)
}

fn program_settlement_key(tenant: &crate::store::TenantId, reservation_id: [u8; 32])
    -> Result<crate::store::TenantKey, DaemonLimitError>
{
    let mut id = b"budget/program/settlement/v1/".to_vec(); id.extend(reservation_id);
    Ok(crate::store::TenantKey::new(tenant.clone(), crate::store::ObjectKind::Configuration, id)?)
}

pub(crate) fn program_settlement_recorded(
    store: &crate::store::Store, tenant: &crate::store::TenantId, id: [u8; 32],
) -> Result<bool, DaemonLimitError> {
    let key = program_settlement_key(tenant, id)?;
    match store.get(&key) {
        None => Ok(false),
        Some(value) if value.class() == crate::store::StorageClass::LocalOnly => {
            reservations::validate_program_settlement_marker(value.bytes(), id)
                .map_err(|_| DaemonLimitError::Corrupt)?;
            Ok(true)
        }
        Some(_) => Err(DaemonLimitError::Corrupt),
    }
}

fn validate_program_settlement_limits(
    store: &crate::store::Store, tenant: &crate::store::TenantId, stage: &StagedProgramSettlement<'_>,
) -> Result<(), DaemonLimitError> {
    let mut dynamic = None;
    for (id, scope, ceiling, consumed, denomination) in stage.expected_limits() {
        if let Some(index) = store.get(&enrolment_index_key(tenant, *id)?) {
            let stable_id = index.bytes().try_into().map_err(|_| DaemonLimitError::Corrupt)?;
            if let Some(record) = stored_enrolment(store, tenant, stable_id)? {
                if record.limit_id != *id || record.scope != *scope || record.ceiling != *ceiling
                    || record.consumed != *consumed
                    || stored_program_denomination(store, tenant, stable_id)? != Some(*denomination)
                { return Err(DaemonLimitError::Conflict); }
                continue;
            }
        }
        if dynamic.is_none() { dynamic = Some(daemon_limits(store, tenant)?); }
        let record = dynamic.as_ref().and_then(|records| records.iter().find(|record| record.limit_id == *id))
            .ok_or(DaemonLimitError::Unknown)?;
        if LimitScope::Agent(record.agent_digest) != *scope || record.ceiling != *ceiling
            || record.consumed != *consumed || daemon::program_denomination(store, tenant, *id)? != *denomination
        { return Err(DaemonLimitError::Conflict); }
    }
    Ok(())
}

impl BudgetLimiter {
    pub fn stage_program_settlement(
        &self, store: &crate::store::Store, tenant: &crate::store::TenantId,
        record: &ProgramBudgetReservation,
        witness: &program_settlement::VerifiedProgramDebitSettlement,
    ) -> Result<StagedProgramSettlement<'_>, DaemonLimitError> {
        let key = program_settlement_key(tenant, record.id)?;
        let previous = store.get(&key);
        if previous.is_some_and(|value| value.class() != crate::store::StorageClass::LocalOnly) {
            return Err(DaemonLimitError::Corrupt);
        }
        let staged = self.stage_program_settlement_inner(record, witness, previous.map(|value| value.bytes()))?;
        finish_program_settlement_stage(store, tenant, key, staged)
    }
}

fn finish_program_settlement_stage<'a>(
    store: &crate::store::Store, tenant: &crate::store::TenantId, key: crate::store::TenantKey,
    mut staged: StagedProgramSettlement<'a>,
) -> Result<StagedProgramSettlement<'a>, DaemonLimitError> {
    if staged.replayed() { return Ok(staged); }
    validate_program_settlement_limits(store, tenant, &staged)?;
    let updates = consumption_updates(store, tenant, staged.actual_holds())?;
    let inserts = vec![(key, staged.marker().to_vec())];
    staged.set_writes(updates, inserts);
    Ok(staged)
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};

    use super::{
        daemon_limit_id, enrolment_index_key, enrolment_key, enrolment_limit_id,
        install_enrolment_limits, install_enrolment_renewals, release, reserve, restore,
        scope_bytes, scope_from, stage_release, stored_enrolment, BudgetLimiter, DaemonLimitError,
        EnrolmentLimitRecord, EnrolmentPredecessor, LimitConfig, LimitId, LimitRefusal, LimitScope,
        ReleaseKind, ReservationRequest, ENROLMENT_BYTES, ENROLMENT_RETIRED_BYTES,
        ENROLMENT_RETIRED_VERSION, ENROLMENT_VERSION,
    };
    use crate::store::{Store, TenantId};

    const SCOPE: LimitScope = LimitScope::Agent([0x21; 32]);
    const CEILING: u128 = 100;

    static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

    fn text<T, E: std::fmt::Debug>(result: Result<T, E>, label: &str) -> T {
        match result {
            Ok(value) => value,
            Err(error) => panic!("{label} must be valid: {error:?}"),
        }
    }

    struct Root(PathBuf);

    impl Root {
        fn new(name: &str) -> Self {
            let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
            Self(std::env::temp_dir().join(format!(
                "layerx-agentd-enrolment-{name}-{}-{sequence}",
                std::process::id()
            )))
        }
    }

    impl Drop for Root {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn tenant_id(name: &str) -> TenantId {
        text(TenantId::new(name), "tenant")
    }

    fn declared(id: u8, scope: LimitScope, ceiling: u128, consumed: u128) -> LimitConfig {
        LimitConfig {
            id: LimitId([id; 16]),
            name: format!("declared limit {id}"),
            scope,
            ceiling,
            consumed,
        }
    }

    fn install(
        store: &mut Store,
        limiter: &BudgetLimiter,
        tenant: &TenantId,
        config: LimitConfig,
    ) -> Result<EnrolmentLimitRecord, DaemonLimitError> {
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let installed = install_enrolment_limits(store, limiter, tenant, &[config])?;
        installed
            .into_iter()
            .find(|record| record.stable_id == stable_id)
            .ok_or(DaemonLimitError::Unknown)
    }

    fn renew(
        store: &mut Store,
        limiter: &BudgetLimiter,
        tenant: &TenantId,
        config: LimitConfig,
        predecessor: u8,
    ) -> Result<EnrolmentLimitRecord, DaemonLimitError> {
        let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
        let renewal = EnrolmentPredecessor {
            successor: config.id,
            predecessor: LimitId([predecessor; 16]),
        };
        let installed = install_enrolment_renewals(store, limiter, tenant, &[config], &[renewal])?;
        installed
            .into_iter()
            .find(|record| record.stable_id == stable_id)
            .ok_or(DaemonLimitError::Unknown)
    }

    fn stored(store: &Store, tenant: &TenantId, stable_id: [u8; 32]) -> EnrolmentLimitRecord {
        let Some(record) = text(stored_enrolment(store, tenant, stable_id), "stored record") else {
            panic!("stored record must exist");
        };
        record
    }

    fn raw(store: &Store, tenant: &TenantId, stable_id: [u8; 32]) -> Vec<u8> {
        let Some(value) = store.get(&text(enrolment_key(tenant, stable_id), "key")) else {
            panic!("stored bytes must exist");
        };
        value.bytes().to_vec()
    }

    fn request(id: u8, amount: u128, limit: LimitId) -> ReservationRequest {
        ReservationRequest {
            id: [id; 32],
            amount,
            expiry_sequence: 10,
            current_sequence: 1,
            applicable_limits: vec![limit],
        }
    }

    #[test]
    fn enrolment_identity_is_namespaced_by_tenant_and_every_scope_variant() {
        let value = [0x09; 32];
        let scopes = [
            LimitScope::Tenant(value),
            LimitScope::Agent(value),
            LimitScope::Session(value),
            LimitScope::Capability(value),
            LimitScope::Counterparty(value),
        ];
        let declared = LimitId([0x07; 16]);
        let mut identities = BTreeSet::new();
        for tenant in [tenant_id("tenant-a"), tenant_id("tenant-b")] {
            for scope in scopes {
                identities.insert(enrolment_limit_id(&tenant, &scope, &declared));
                let bytes = scope_bytes(scope);
                let mut id = [0; 32];
                id.copy_from_slice(&bytes[1..]);
                assert_eq!(text(scope_from(bytes[0], id), "scope"), scope);
            }
        }
        assert_eq!(identities.len(), 10);
        let tenant = tenant_id("tenant-a");
        assert_ne!(
            enrolment_limit_id(&tenant, &LimitScope::Agent(value), &declared),
            enrolment_limit_id(&tenant, &LimitScope::Agent([0x0a; 32]), &declared)
        );
        assert_ne!(
            enrolment_limit_id(&tenant, &SCOPE, &declared),
            enrolment_limit_id(&tenant, &SCOPE, &LimitId([0x08; 16]))
        );
        assert!(matches!(
            scope_from(5, value),
            Err(DaemonLimitError::Corrupt)
        ));
    }

    #[test]
    fn renewal_under_a_new_identity_carries_consumed_and_keeps_the_ceiling() {
        let root = Root::new("renewal");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 40),
            ),
            "first",
        );
        let renewed = text(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1,
            ),
            "renewal",
        );
        assert_eq!(renewed.consumed, 40);
        assert_eq!(renewed.successor, None);
        assert_eq!(limiter.consumed(renewed.limit_id), Ok(40));
        assert_eq!(limiter.is_retired(first.limit_id), Ok(true));
        assert_eq!(limiter.is_retired(renewed.limit_id), Ok(false));
        assert_eq!(
            stored(&store, &tenant, first.stable_id).successor,
            Some(renewed.stable_id)
        );
        assert!(matches!(
            reserve(&limiter, &request(3, 61, renewed.limit_id)),
            Err(LimitRefusal::Exceeded { .. })
        ));
        assert!(matches!(
            reserve(&limiter, &request(3, 1, first.limit_id)),
            Err(LimitRefusal::Retired(_))
        ));
        assert!(reserve(&limiter, &request(4, 60, renewed.limit_id)).is_ok());
        let again = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
            ),
            "reinstall",
        );
        assert_eq!(again.consumed, 40);
        let lowered = declared(3, SCOPE, 30, 0);
        let lowered_id = enrolment_limit_id(&tenant, &SCOPE, &lowered.id);
        assert!(matches!(
            renew(&mut store, &limiter, &tenant, lowered, 2),
            Err(DaemonLimitError::Conflict)
        ));
        assert_eq!(
            text(stored_enrolment(&store, &tenant, lowered_id), "absent"),
            None
        );
        assert_eq!(limiter.is_retired(renewed.limit_id), Ok(false));
        assert!(matches!(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 0)
            ),
            Err(DaemonLimitError::Conflict)
        ));
    }

    #[test]
    fn renewal_keeps_a_held_reservation_on_the_retired_identity() {
        let root = Root::new("held");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        let reservation = text(reserve(&limiter, &request(5, 20, first.limit_id)), "hold");
        let renewed = text(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1,
            ),
            "renewal",
        );
        assert_eq!(limiter.held_limits([5; 32]), Ok(vec![first.limit_id]));
        assert_eq!(limiter.held_reservations(), Ok(1));
        assert_eq!(renewed.consumed, 10);

        let restarted = text(BudgetLimiter::new(Vec::new()), "restarted limiter");
        text(
            install_enrolment_limits(
                &mut store,
                &restarted,
                &tenant,
                &[declared(2, SCOPE, CEILING, 0)],
            ),
            "restart install",
        );
        assert_eq!(restarted.is_retired(first.limit_id), Ok(true));
        text(restore(&restarted, &reservation.durable), "restore");
        assert_eq!(restarted.held_limits([5; 32]), Ok(vec![first.limit_id]));

        let updates = text(
            super::consumption_updates(&store, &tenant, &reservation.durable),
            "consumption",
        );
        assert_eq!(updates.len(), 2);
        text(store.update_local_batch(updates), "apply consumption");
        let retired = stored(&store, &tenant, first.stable_id);
        assert_eq!(retired.consumed, 30);
        assert_eq!(retired.successor, Some(renewed.stable_id));
        assert_eq!(stored(&store, &tenant, renewed.stable_id).consumed, 30);
    }

    #[test]
    fn truncated_identifier_collision_is_still_refused() {
        let root = Root::new("collision");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let fresh = declared(1, SCOPE, CEILING, 0);
        let fresh_id = enrolment_limit_id(&tenant, &SCOPE, &fresh.id);
        text(
            store.put_local(
                text(
                    enrolment_index_key(&tenant, daemon_limit_id(fresh_id)),
                    "index",
                ),
                vec![0xee; 32],
            ),
            "foreign index",
        );
        assert!(matches!(
            install(&mut store, &limiter, &tenant, fresh),
            Err(DaemonLimitError::IdCollision)
        ));

        let scope = LimitScope::Session([0x31; 32]);
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(2, scope, CEILING, 0),
            ),
            "first",
        );
        let renewal = declared(3, scope, CEILING, 0);
        let renewal_id = enrolment_limit_id(&tenant, &scope, &renewal.id);
        text(
            store.put_local(
                text(
                    enrolment_index_key(&tenant, daemon_limit_id(renewal_id)),
                    "index",
                ),
                vec![0xee; 32],
            ),
            "foreign index",
        );
        assert!(matches!(
            renew(&mut store, &limiter, &tenant, renewal, 2),
            Err(DaemonLimitError::IdCollision)
        ));
        assert_eq!(stored(&store, &tenant, first.stable_id).successor, None);
        assert_eq!(limiter.is_retired(first.limit_id), Ok(false));
    }

    #[test]
    fn retired_record_decodes_strictly_and_points_at_its_successor() {
        let root = Root::new("retired");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 5),
            ),
            "first",
        );
        let live = raw(&store, &tenant, first.stable_id);
        assert_eq!(live.len(), ENROLMENT_BYTES);
        assert_eq!(live[4], ENROLMENT_VERSION);
        let renewed = text(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1,
            ),
            "renewal",
        );
        let retired = raw(&store, &tenant, first.stable_id);
        assert_eq!(retired.len(), ENROLMENT_RETIRED_BYTES);
        assert_eq!(retired[4], ENROLMENT_RETIRED_VERSION);
        let mut expected = live.clone();
        expected[4] = ENROLMENT_RETIRED_VERSION;
        assert_eq!(retired[..ENROLMENT_BYTES], expected[..]);
        assert_eq!(retired[ENROLMENT_BYTES..], renewed.stable_id[..]);
        let decoded = text(
            EnrolmentLimitRecord::decode(tenant.clone(), &retired),
            "retired decode",
        );
        assert_eq!(decoded.successor, Some(renewed.stable_id));
        assert_eq!(decoded.consumed, 5);

        let mut unknown = retired.clone();
        unknown[4] = 3;
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant.clone(), &unknown),
            Err(DaemonLimitError::Corrupt)
        ));
        let mut truncated = retired[..ENROLMENT_BYTES].to_vec();
        truncated[4] = ENROLMENT_RETIRED_VERSION;
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant.clone(), &truncated),
            Err(DaemonLimitError::Corrupt)
        ));
        let mut trailing = retired.clone();
        trailing[4] = ENROLMENT_VERSION;
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant.clone(), &trailing),
            Err(DaemonLimitError::Corrupt)
        ));
        let mut own = retired[..ENROLMENT_BYTES].to_vec();
        own.extend_from_slice(&first.stable_id);
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant.clone(), &own),
            Err(DaemonLimitError::Corrupt)
        ));
        let mut zero = retired[..ENROLMENT_BYTES].to_vec();
        zero.extend_from_slice(&[0; 32]);
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant.clone(), &zero),
            Err(DaemonLimitError::Corrupt)
        ));
        assert!(matches!(
            EnrolmentLimitRecord::decode(tenant_id("tenant-b"), &retired),
            Err(DaemonLimitError::Corrupt)
        ));
    }

    #[test]
    fn same_identity_reinstall_conflicts_on_ceiling_change_and_keeps_consumed_monotone() {
        let root = Root::new("reinstall");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 5),
            ),
            "first",
        );
        assert!(matches!(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING + 1, 5)
            ),
            Err(DaemonLimitError::Conflict)
        ));
        text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 8),
            ),
            "raise",
        );
        let lowered = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 3),
            ),
            "lower",
        );
        assert_eq!(lowered.consumed, 8);
        assert_eq!(stored(&store, &tenant, first.stable_id).consumed, 8);
        assert_eq!(limiter.is_retired(first.limit_id), Ok(false));
    }

    #[test]
    fn predecessor_holds_count_against_the_successor_ceiling() {
        let root = Root::new("lineage-held");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        let hold = text(
            reserve(&limiter, &request(5, 20, first.limit_id)),
            "old hold",
        );
        let renewed = text(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1,
            ),
            "renewal",
        );
        assert!(matches!(
            reserve(&limiter, &request(6, 71, renewed.limit_id)),
            Err(LimitRefusal::Exceeded {
                ceiling: 100,
                consumed: 10,
                held: 20,
                requested: 71,
                ..
            })
        ));
        assert!(reserve(&limiter, &request(7, 70, renewed.limit_id)).is_ok());

        let restarted = text(BudgetLimiter::new(Vec::new()), "restarted limiter");
        text(
            install_enrolment_limits(
                &mut store,
                &restarted,
                &tenant,
                &[declared(2, SCOPE, CEILING, 0)],
            ),
            "restart install",
        );
        text(restore(&restarted, &hold.durable), "restore");
        assert!(matches!(
            reserve(&restarted, &request(6, 71, renewed.limit_id)),
            Err(LimitRefusal::Exceeded { held: 20, .. })
        ));
        assert!(reserve(&restarted, &request(7, 70, renewed.limit_id)).is_ok());
    }

    #[test]
    fn settling_a_predecessor_hold_charges_the_successor_in_store_and_cache() {
        let root = Root::new("lineage-settle");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        let hold = text(
            reserve(&limiter, &request(5, 20, first.limit_id)),
            "old hold",
        );
        let held_bytes = hold.durable.clone();
        let renewed = text(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1,
            ),
            "renewal",
        );
        let stale = text(BudgetLimiter::new(Vec::new()), "stale limiter");
        text(
            install_enrolment_limits(
                &mut store,
                &stale,
                &tenant,
                &[declared(2, SCOPE, CEILING, 0)],
            ),
            "stale install",
        );
        assert_eq!(stale.consumed(renewed.limit_id), Ok(10));

        let updates = text(
            super::consumption_updates(&store, &tenant, &hold.durable),
            "consumption",
        );
        text(store.update_local_batch(updates), "settlement write");
        assert_eq!(
            release(&limiter, [5; 32], ReleaseKind::Executed, 2),
            Ok(true)
        );
        assert_eq!(stored(&store, &tenant, renewed.stable_id).consumed, 30);
        assert_eq!(limiter.consumed(renewed.limit_id), Ok(30));
        assert_eq!(stored(&store, &tenant, first.stable_id).consumed, 30);
        assert_eq!(limiter.consumed(first.limit_id), Ok(30));
        assert_eq!(limiter.held_reservations(), Ok(0));
        assert!(matches!(
            reserve(&limiter, &request(6, 71, renewed.limit_id)),
            Err(LimitRefusal::Exceeded {
                consumed: 30,
                held: 0,
                ..
            })
        ));
        assert!(reserve(&limiter, &request(7, 70, renewed.limit_id)).is_ok());

        assert_eq!(hold.durable, held_bytes);
        assert_eq!(hold.durable.len(), 1);
        assert_eq!(hold.durable[0].limit_id, first.limit_id);
        assert_eq!(hold.durable[0].digest, hold.durable[0].canonical_digest());

        text(
            install_enrolment_limits(
                &mut store,
                &stale,
                &tenant,
                &[declared(2, SCOPE, CEILING, 0)],
            ),
            "refresh install",
        );
        assert_eq!(stale.consumed(renewed.limit_id), Ok(30));
        assert_eq!(stale.consumed(first.limit_id), Ok(30));
    }

    #[test]
    fn renewal_above_full_exposure_is_refused() {
        let root = Root::new("exposure");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        text(reserve(&limiter, &request(5, 20, first.limit_id)), "hold");
        let tight = declared(2, SCOPE, 29, 0);
        let tight_id = enrolment_limit_id(&tenant, &SCOPE, &tight.id);
        assert!(matches!(
            renew(&mut store, &limiter, &tenant, tight, 1),
            Err(DaemonLimitError::Conflict)
        ));
        assert_eq!(
            text(stored_enrolment(&store, &tenant, tight_id), "absent"),
            None
        );
        assert_eq!(stored(&store, &tenant, first.stable_id).successor, None);
        assert_eq!(limiter.is_retired(first.limit_id), Ok(false));
        let renewed = text(
            renew(&mut store, &limiter, &tenant, declared(3, SCOPE, 30, 0), 1),
            "exact exposure",
        );
        assert_eq!(renewed.consumed, 10);
        assert_eq!(limiter.held_exposure(renewed.limit_id), Ok(20));
        assert!(matches!(
            reserve(&limiter, &request(6, 1, renewed.limit_id)),
            Err(LimitRefusal::Exceeded { .. })
        ));
    }

    #[test]
    fn concurrent_admission_during_renewal_cannot_pass_the_successor_ceiling() {
        for round in 0..20 {
            let root = Root::new(&format!("race-{round}"));
            let mut store = text(Store::open(&root.0), "store");
            let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
            let tenant = tenant_id("tenant-a");
            let first = text(
                install(
                    &mut store,
                    &limiter,
                    &tenant,
                    declared(1, SCOPE, CEILING, 10),
                ),
                "first",
            );
            text(reserve(&limiter, &request(5, 20, first.limit_id)), "hold");
            let barrier = std::sync::Barrier::new(2);
            let (renewal, admission) = std::thread::scope(|scope| {
                let renewing = scope.spawn(|| {
                    barrier.wait();
                    renew(&mut store, &limiter, &tenant, declared(2, SCOPE, 30, 0), 1)
                });
                let admitting = scope.spawn(|| {
                    barrier.wait();
                    reserve(&limiter, &request(6, 5, first.limit_id))
                });
                let Ok(renewal) = renewing.join() else {
                    panic!("renewal thread must finish");
                };
                let Ok(admission) = admitting.join() else {
                    panic!("admission thread must finish");
                };
                (renewal, admission)
            });
            match (renewal, admission) {
                (Ok(renewed), Err(LimitRefusal::Retired(limit))) => {
                    assert_eq!(limit, first.limit_id);
                    let consumed = text(limiter.consumed(renewed.limit_id), "consumed");
                    let held = text(limiter.held_exposure(renewed.limit_id), "held");
                    assert_eq!((consumed, held), (10, 20));
                    assert_eq!(
                        stored(&store, &tenant, first.stable_id).successor,
                        Some(renewed.stable_id)
                    );
                }
                (Err(DaemonLimitError::Conflict), Ok(_)) => {
                    assert_eq!(limiter.is_retired(first.limit_id), Ok(false));
                    assert_eq!(limiter.held_exposure(first.limit_id), Ok(25));
                    assert_eq!(stored(&store, &tenant, first.stable_id).successor, None);
                    let successor = enrolment_limit_id(&tenant, &SCOPE, &LimitId([2; 16]));
                    assert_eq!(
                        text(stored_enrolment(&store, &tenant, successor), "absent"),
                        None
                    );
                }
                (renewal, admission) => {
                    panic!("renewal and admission interleaved: {renewal:?} {admission:?}")
                }
            }
        }
    }

    #[test]
    fn startup_restores_preserved_holds_before_a_renewal_exposure_check() {
        let root = Root::new("startup-exposure");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        let hold = text(reserve(&limiter, &request(5, 20, first.limit_id)), "hold");
        drop(limiter);

        let restarted = text(BudgetLimiter::new(Vec::new()), "restarted limiter");
        assert!(matches!(
            renew(&mut store, &restarted, &tenant, declared(2, SCOPE, 29, 0), 1),
            Err(DaemonLimitError::Limit(LimitRefusal::UnknownLimit(limit))) if limit == first.limit_id
        ));
        text(
            install_enrolment_limits(
                &mut store,
                &restarted,
                &tenant,
                &[declared(1, SCOPE, CEILING, 0)],
            ),
            "base install",
        );
        text(restore(&restarted, &hold.durable), "restore");
        let tight = enrolment_limit_id(&tenant, &SCOPE, &LimitId([2; 16]));
        assert!(matches!(
            renew(
                &mut store,
                &restarted,
                &tenant,
                declared(2, SCOPE, 29, 0),
                1
            ),
            Err(DaemonLimitError::Conflict)
        ));
        assert_eq!(
            text(stored_enrolment(&store, &tenant, tight), "absent"),
            None
        );
        assert_eq!(stored(&store, &tenant, first.stable_id).successor, None);
        assert_eq!(restarted.is_retired(first.limit_id), Ok(false));
        let renewed = text(
            renew(
                &mut store,
                &restarted,
                &tenant,
                declared(3, SCOPE, 30, 0),
                1,
            ),
            "exact exposure",
        );
        assert_eq!(renewed.consumed, 10);
        assert_eq!(restarted.held_exposure(renewed.limit_id), Ok(20));
        assert_eq!(restarted.is_retired(first.limit_id), Ok(true));
    }

    #[test]
    fn renewal_requires_a_live_predecessor_of_the_same_scope() {
        let root = Root::new("predecessor");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        assert!(matches!(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                9
            ),
            Err(DaemonLimitError::Conflict)
        ));
        let other = LimitScope::Session([0x31; 32]);
        text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, other, CEILING, 0),
            ),
            "other scope",
        );
        assert!(matches!(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                1
            ),
            Err(DaemonLimitError::Conflict)
        ));
        assert!(matches!(
            renew(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
                2
            ),
            Err(DaemonLimitError::Invalid)
        ));
    }

    #[test]
    fn same_scope_limits_without_a_predecessor_are_independent_and_both_charged() {
        let root = Root::new("independent");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let installed = text(
            install_enrolment_limits(
                &mut store,
                &limiter,
                &tenant,
                &[declared(1, SCOPE, CEILING, 0), declared(2, SCOPE, 50, 0)],
            ),
            "both",
        );
        assert_eq!(installed.len(), 2);
        let wide = installed[0].clone();
        let narrow = installed[1].clone();
        assert_ne!(wide.limit_id, narrow.limit_id);
        assert_eq!(wide.successor, None);
        assert_eq!(narrow.successor, None);
        assert_eq!(limiter.is_retired(wide.limit_id), Ok(false));
        assert_eq!(limiter.is_retired(narrow.limit_id), Ok(false));
        let both = |id: u8, amount: u128| ReservationRequest {
            applicable_limits: vec![wide.limit_id, narrow.limit_id],
            ..request(id, amount, wide.limit_id)
        };
        assert!(matches!(
            reserve(&limiter, &both(5, 51)),
            Err(LimitRefusal::Exceeded { .. })
        ));
        let hold = text(reserve(&limiter, &both(6, 50)), "hold");
        assert_eq!(hold.durable.len(), 2);
        let updates = text(
            super::consumption_updates(&store, &tenant, &hold.durable),
            "consumption",
        );
        assert_eq!(updates.len(), 2);
        text(store.update_local_batch(updates), "settlement write");
        assert_eq!(
            release(&limiter, [6; 32], ReleaseKind::Executed, 2),
            Ok(true)
        );
        assert_eq!(stored(&store, &tenant, wide.stable_id).consumed, 50);
        assert_eq!(stored(&store, &tenant, narrow.stable_id).consumed, 50);
        assert_eq!(limiter.consumed(wide.limit_id), Ok(50));
        assert_eq!(limiter.consumed(narrow.limit_id), Ok(50));
    }

    #[test]
    fn failed_store_write_leaves_the_cache_unchanged() {
        let root = Root::new("failed-write");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let first = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 10),
            ),
            "first",
        );
        let hold = text(reserve(&limiter, &request(5, 20, first.limit_id)), "hold");
        let absent = text(enrolment_key(&tenant, [0x5a; 32]), "absent key");
        let staged = text(
            stage_release(&limiter, [5; 32], ReleaseKind::Executed, 2),
            "stage",
        );
        assert!(staged.found());
        assert!(store.update_local_batch(vec![(absent, vec![1])]).is_err());
        drop(staged);
        assert_eq!(limiter.consumed(first.limit_id), Ok(10));
        assert_eq!(limiter.held_limits([5; 32]), Ok(vec![first.limit_id]));
        assert_eq!(stored(&store, &tenant, first.stable_id).consumed, 10);

        let staged = text(
            stage_release(&limiter, [5; 32], ReleaseKind::Executed, 2),
            "stage again",
        );
        let updates = text(
            super::consumption_updates(&store, &tenant, &hold.durable),
            "consumption",
        );
        text(store.update_local_batch(updates), "settlement write");
        assert!(staged.publish());
        assert_eq!(limiter.consumed(first.limit_id), Ok(30));
        assert_eq!(limiter.held_reservations(), Ok(0));
        assert_eq!(stored(&store, &tenant, first.stable_id).consumed, 30);
        let replay = text(
            stage_release(&limiter, [5; 32], ReleaseKind::Executed, 3),
            "replay",
        );
        assert!(!replay.publish());
        assert_eq!(limiter.consumed(first.limit_id), Ok(30));
    }

    fn store_files(root: &std::path::Path) -> Vec<(PathBuf, Vec<u8>)> {
        let mut files = text(std::fs::read_dir(root), "store directory")
            .map(|entry| text(entry, "store entry").path())
            .filter(|path| path.is_file())
            .map(|path| {
                let bytes = text(std::fs::read(&path), "store file");
                (path, bytes)
            })
            .collect::<Vec<_>>();
        files.sort();
        files
    }

    fn daemon(
        store: &mut Store,
        limiter: &BudgetLimiter,
        tenant: &TenantId,
    ) -> super::DaemonLimitRecord {
        text(
            super::create_daemon_limit(
                store,
                limiter,
                super::DaemonLimitRecord {
                    tenant: tenant.clone(),
                    budget_id: [0x31; 32],
                    limit_id: daemon_limit_id([0x31; 32]),
                    agent_digest: [0x21; 32],
                    asset: [0x32; 32],
                    ceiling: CEILING,
                    consumed: 0,
                    expiry_ms: 1_000_000,
                    revoked: false,
                    mutation_key: [0x33; 32],
                    body_digest: [0x34; 32],
                    revoke_key: [0; 32],
                },
                super::CoreTimestampMs(1),
            ),
            "daemon limit",
        )
    }

    fn daemon_consumed(store: &Store, tenant: &TenantId) -> u128 {
        let records = text(super::daemon_limits(store, tenant), "daemon limits");
        assert_eq!(records.len(), 1);
        records[0].consumed
    }

    #[test]
    fn consumption_for_a_hold_in_neither_index_is_refused_and_every_record_is_identical() {
        let root = Root::new("unclassified");
        let mut store = text(Store::open(&root.0), "store");
        let unclassified = declared(9, SCOPE, CEILING, 0);
        let unclassified_id = unclassified.id;
        let limiter = text(BudgetLimiter::new(vec![unclassified]), "limiter");
        let tenant = tenant_id("tenant-a");
        let enrolment = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 0),
            ),
            "enrolment",
        );
        let daemon = daemon(&mut store, &limiter, &tenant);
        let hold = text(
            reserve(
                &limiter,
                &ReservationRequest {
                    applicable_limits: vec![enrolment.limit_id, daemon.limit_id, unclassified_id],
                    ..request(7, 20, enrolment.limit_id)
                },
            ),
            "hold",
        );
        assert_eq!(hold.durable.len(), 3);
        let only_unclassified = hold
            .durable
            .iter()
            .filter(|durable| durable.limit_id == unclassified_id)
            .cloned()
            .collect::<Vec<_>>();
        assert_eq!(only_unclassified.len(), 1);
        let before = store_files(&root.0);
        assert!(!before.is_empty());
        assert!(matches!(
            super::consumption_updates(&store, &tenant, &hold.durable),
            Err(DaemonLimitError::Unknown)
        ));
        assert!(matches!(
            super::consumption_updates(&store, &tenant, &only_unclassified),
            Err(DaemonLimitError::Unknown)
        ));
        assert_eq!(store_files(&root.0), before);
        assert_eq!(stored(&store, &tenant, enrolment.stable_id).consumed, 0);
        assert_eq!(daemon_consumed(&store, &tenant), 0);
        assert_eq!(limiter.held_reservations(), Ok(3));
        assert_eq!(limiter.consumed(enrolment.limit_id), Ok(0));
        assert_eq!(limiter.consumed(daemon.limit_id), Ok(0));
        assert_eq!(limiter.consumed(unclassified_id), Ok(0));
    }

    #[test]
    fn a_refused_unclassified_hold_stays_held_and_claimable_across_a_store_restart() {
        let root = Root::new("unclassified-restart");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(
            BudgetLimiter::new(vec![declared(9, SCOPE, CEILING, 0)]),
            "limiter",
        );
        let tenant = tenant_id("tenant-a");
        let enrolment = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 0),
            ),
            "enrolment",
        );
        let hold = text(
            reserve(
                &limiter,
                &ReservationRequest {
                    applicable_limits: vec![enrolment.limit_id, LimitId([9; 16])],
                    ..request(8, 20, enrolment.limit_id)
                },
            ),
            "hold",
        );
        assert!(matches!(
            super::consumption_updates(&store, &tenant, &hold.durable),
            Err(DaemonLimitError::Unknown)
        ));
        let before = store_files(&root.0);
        drop(store);

        let mut store = text(Store::open(&root.0), "reopened store");
        assert_eq!(store_files(&root.0), before);
        let restarted = text(
            BudgetLimiter::new(vec![declared(9, SCOPE, CEILING, 0)]),
            "restarted limiter",
        );
        text(
            install_enrolment_limits(
                &mut store,
                &restarted,
                &tenant,
                &[declared(1, SCOPE, CEILING, 0)],
            ),
            "restart install",
        );
        text(restore(&restarted, &hold.durable), "restore");
        assert_eq!(restarted.held_reservations(), Ok(2));
        assert_eq!(stored(&store, &tenant, enrolment.stable_id).consumed, 0);
        assert!(matches!(
            super::consumption_updates(&store, &tenant, &hold.durable),
            Err(DaemonLimitError::Unknown)
        ));
        assert_eq!(
            release(&restarted, [8; 32], ReleaseKind::Failed, 2),
            Ok(true)
        );
        assert_eq!(restarted.held_reservations(), Ok(0));
        assert_eq!(restarted.consumed(enrolment.limit_id), Ok(0));
    }

    #[test]
    fn enrolment_and_daemon_indexed_holds_still_persist_their_charges() {
        let root = Root::new("classified");
        let mut store = text(Store::open(&root.0), "store");
        let limiter = text(BudgetLimiter::new(Vec::new()), "limiter");
        let tenant = tenant_id("tenant-a");
        let enrolment = text(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 0),
            ),
            "enrolment",
        );
        let daemon = daemon(&mut store, &limiter, &tenant);
        let hold = text(
            reserve(
                &limiter,
                &ReservationRequest {
                    applicable_limits: vec![enrolment.limit_id, daemon.limit_id],
                    ..request(9, 20, enrolment.limit_id)
                },
            ),
            "hold",
        );
        let updates = text(
            super::consumption_updates(&store, &tenant, &hold.durable),
            "consumption",
        );
        assert_eq!(updates.len(), 2);
        text(store.update_local_batch(updates), "settlement write");
        assert_eq!(
            release(&limiter, [9; 32], ReleaseKind::Executed, 2),
            Ok(true)
        );
        assert_eq!(stored(&store, &tenant, enrolment.stable_id).consumed, 20);
        assert_eq!(daemon_consumed(&store, &tenant), 20);
        assert_eq!(limiter.consumed(enrolment.limit_id), Ok(20));
        assert_eq!(limiter.consumed(daemon.limit_id), Ok(20));
    }
}

#[cfg(test)]
mod program_denomination_tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt as _;
    use crate::store::{ObjectKind, Store, TenantId, TenantKey};

    fn must<T, E: std::fmt::Debug>(result: Result<T, E>) -> T {
        result.unwrap_or_else(|error| panic!("program denomination: {error:?}"))
    }

    #[test]
    fn protected_binding_is_immutable_and_preserves_consumption_across_restart() {
        let suffix = must(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH)).as_nanos();
        let root = std::env::temp_dir().join(format!("program-denomination-{}-{suffix}", std::process::id()));
        must(std::fs::create_dir_all(&root));
        let tenant = must(TenantId::new("denominated"));
        let config = LimitConfig { id: LimitId([2; 16]), name: "all sources in one asset".to_owned(),
            scope: LimitScope::Tenant([3; 32]), ceiling: 100, consumed: 0 };
        let stable_id = enrolment_limit_id(&tenant, &config.scope, &config.id);
        let source = root.join("denominations.json");
        let document = serde_json::json!({"version":1,"tenant":tenant.as_str(),"limits":[{
            "stable_id":hex::encode(stable_id),"asset":hex::encode([4_u8;32]),"source":"*"}]});
        must(std::fs::write(&source, must(serde_json::to_vec(&document))));
        must(std::fs::set_permissions(&source, std::fs::Permissions::from_mode(0o600)));
        let verified = must(crate::config::load_program_budget_denominations(&source, &tenant));
        let mut store = must(Store::open(root.join("store")));
        let limiter = must(BudgetLimiter::new(Vec::new()));
        let installed = must(install_enrolment_limits(&mut store, &limiter, &tenant, &[config.clone()]));
        must(install_program_enrolment_denominations(&mut store, &limiter, &verified));
        let limit_id = installed[0].limit_id;
        let request = ProgramReservationRequest { id:[5;32], expiry_sequence:100,current_sequence:1,core_deadline:Some(CoreTimestampMs(500)),
            charges:vec![
                ProgramBudgetCharge {kind:ProgramChargeKind::Principal,source:[6;32],asset:[4;32],amount:20,applicable_limits:vec![limit_id]},
                ProgramBudgetCharge {kind:ProgramChargeKind::ProgramSpend,source:[7;32],asset:[4;32],amount:30,applicable_limits:vec![limit_id]},
            ]};
        let stage = must(limiter.stage_program_reservation(&request,CoreTimestampMs(1)));
        let key = must(TenantKey::new(tenant.clone(),ObjectKind::Configuration,b"program-hold".to_vec()));
        must(store.put_local(key.clone(),must(stage.record().encode())));
        let held = stage.publish();
        assert_eq!(limiter.held_exposure(limit_id),Ok(50));
        drop(limiter);drop(store);
        let mut store = must(Store::open(root.join("store")));
        let restarted = must(BudgetLimiter::new(Vec::new()));
        must(install_enrolment_limits(&mut store,&restarted,&tenant,&[config.clone()]));
        let persisted = store.get(&key).unwrap_or_else(|| panic!("missing typed Program reservation"));
        let restored = must(ProgramBudgetReservation::decode(persisted.bytes()));
        assert_eq!(restored,held);
        must(restarted.restore_program_reservation(&restored));
        let staged = must(restarted.stage_release(restored.id,ReleaseKind::Executed,2));
        let mut updates = must(program_consumption_updates(&store,&tenant,&restored));
        updates.push((key,b"executed".to_vec()));
        must(store.update_local_batch(updates));
        assert!(staged.publish());
        assert_eq!(restarted.consumed(limit_id),Ok(50));
        let mut changed=document;
        changed["limits"][0]["asset"]=serde_json::Value::String(hex::encode([8_u8;32]));
        must(std::fs::write(&source,must(serde_json::to_vec(&changed))));
        let changed=must(crate::config::load_program_budget_denominations(&source,&tenant));
        assert!(matches!(install_program_enrolment_denominations(&mut store,&restarted,&changed),Err(DaemonLimitError::Conflict)));
        assert_eq!(restarted.consumed(limit_id),Ok(50));
        drop(store);drop(restarted);
        let mut store=must(Store::open(root.join("store")));
        let final_limiter=must(BudgetLimiter::new(Vec::new()));
        must(install_enrolment_limits(&mut store,&final_limiter,&tenant,&[config]));
        assert_eq!(final_limiter.consumed(limit_id),Ok(50));
        assert_eq!(final_limiter.held_reservations(),Ok(0));
        drop(store);must(std::fs::remove_dir_all(root));
    }

    #[test]
    fn legacy_consumption_refuses_binding_and_daemon_asset_recovers_without_inference() {
        let suffix = must(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH)).as_nanos();
        let root = std::env::temp_dir().join(format!("program-budget-history-{}-{suffix}",std::process::id()));
        must(std::fs::create_dir_all(&root));
        let mut store = must(Store::open(root.join("store")));
        let tenant = must(TenantId::new("legacy-history"));
        let config = LimitConfig { id:LimitId([1;16]),name:"legacy".to_owned(),
            scope:LimitScope::Tenant([2;32]),ceiling:100,consumed:7 };
        let stable_id = enrolment_limit_id(&tenant,&config.scope,&config.id);
        let limiter = must(BudgetLimiter::new(Vec::new()));
        let installed = must(install_enrolment_limits(&mut store,&limiter,&tenant,&[config]));
        let source = root.join("source.json");
        must(std::fs::write(&source,must(serde_json::to_vec(&serde_json::json!({
            "version":1,"tenant":tenant.as_str(),"limits":[{"stable_id":hex::encode(stable_id),
            "asset":hex::encode([3_u8;32]),"source":"*"}]})))));
        must(std::fs::set_permissions(&source,std::fs::Permissions::from_mode(0o600)));
        let verified = must(crate::config::load_program_budget_denominations(&source,&tenant));
        assert!(matches!(install_program_enrolment_denominations(&mut store,&limiter,&verified),Err(DaemonLimitError::Conflict)));
        assert_eq!(must(stored_program_denomination(&store,&tenant,stable_id)),None);
        assert_eq!(limiter.consumed(installed[0].limit_id),Ok(7));
        let dynamic = must(create_daemon_limit(&mut store,&limiter,DaemonLimitRecord {
            tenant:tenant.clone(),budget_id:[4;32],limit_id:daemon_limit_id([4;32]),
            agent_digest:[5;32],asset:[6;32],ceiling:100,consumed:0,expiry_ms:1000,
            revoked:false,mutation_key:[7;32],body_digest:[8;32],revoke_key:[0;32],
        },CoreTimestampMs(1)));
        let request = ProgramReservationRequest { id:[9;32],charges:vec![ProgramBudgetCharge {
            kind:ProgramChargeKind::ProgramSpend,source:[10;32],asset:dynamic.asset,amount:30,
            applicable_limits:vec![dynamic.limit_id],
        }],expiry_sequence:100,current_sequence:1,core_deadline:Some(CoreTimestampMs(500)) };
        let stage = must(limiter.stage_program_reservation(&request,CoreTimestampMs(1)));
        let key = must(TenantKey::new(tenant.clone(),ObjectKind::Configuration,b"typed-hold".to_vec()));
        must(store.put_local(key.clone(),must(stage.record().encode())));
        let record = stage.publish();
        drop(store); drop(limiter);
        let mut store = must(Store::open(root.join("store")));
        let limiter = must(BudgetLimiter::new(Vec::new()));
        must(load_daemon_limits(&store,&limiter));
        let stored = store.get(&key).unwrap_or_else(|| panic!("missing typed hold"));
        let restored = must(ProgramBudgetReservation::decode(stored.bytes()));
        assert_eq!(restored,record);
        must(limiter.restore_program_reservation(&restored));
        let stage = must(limiter.stage_release(restored.id,ReleaseKind::Executed,2));
        let mut updates = must(program_consumption_updates(&store,&tenant,&restored));
        updates.push((key,b"executed".to_vec()));
        must(store.update_local_batch(updates)); assert!(stage.publish());
        assert_eq!(limiter.consumed(dynamic.limit_id),Ok(30));
        assert_eq!(must(daemon_limits(&store,&tenant))[0].consumed,30);
        drop(store); must(std::fs::remove_dir_all(root));
    }

}
