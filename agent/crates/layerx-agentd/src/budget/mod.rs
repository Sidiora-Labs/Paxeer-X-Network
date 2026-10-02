//! Protocol-backed and explicitly local spending limits.

#[path = "reconcile.rs"]
mod accounting;
mod create;
mod daemon;
#[path = "divergence.rs"]
mod divergence_reporting;
mod mutate;
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
    LimitId, LimitRefusal, LimitScope, ReleaseKind, ReservationRequest,
};

const ENROLMENT_PREFIX: &[u8] = b"budget/enrolment/limit/";
const ENROLMENT_INDEX_PREFIX: &[u8] = b"budget/daemon/limit-id/";
const ENROLMENT_LINEAGE_PREFIX: &[u8] = b"budget/enrolment/lineage/";
const ENROLMENT_ID_DOMAIN: &[u8] = b"layerx:budget-enrolment-limit-id:v1\0";
const ENROLMENT_MAGIC: &[u8; 4] = b"LXEL";
const ENROLMENT_VERSION: u8 = 1;
const ENROLMENT_RETIRED_VERSION: u8 = 2;
const ENROLMENT_BYTES: usize = 4 + 1 + 32 + 16 + 1 + 32 + 16 + 16 + 16;
const ENROLMENT_RETIRED_BYTES: usize = ENROLMENT_BYTES + 32;
const LINEAGE_MAGIC: &[u8; 4] = b"LXEN";
const LINEAGE_VERSION: u8 = 1;
const LINEAGE_BYTES: usize = 4 + 1 + 1 + 32 + 32;

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

fn lineage_key(
    tenant: &crate::store::TenantId,
    scope: LimitScope,
) -> Result<crate::store::TenantKey, DaemonLimitError> {
    Ok(crate::store::TenantKey::new(
        tenant.clone(),
        crate::store::ObjectKind::Configuration,
        [ENROLMENT_LINEAGE_PREFIX, &scope_bytes(scope)].concat(),
    )?)
}

fn encode_lineage(scope: LimitScope, current: [u8; 32]) -> Result<Vec<u8>, DaemonLimitError> {
    let mut encoder = layerx_wire::encode::Encoder::new(LINEAGE_BYTES);
    let corrupt = |_| DaemonLimitError::Corrupt;
    encoder.fixed(LINEAGE_MAGIC).map_err(corrupt)?;
    encoder.u8(LINEAGE_VERSION).map_err(corrupt)?;
    encoder.fixed(&scope_bytes(scope)).map_err(corrupt)?;
    encoder.fixed(&current).map_err(corrupt)?;
    Ok(encoder.finish())
}

fn stored_lineage(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    scope: LimitScope,
) -> Result<Option<[u8; 32]>, DaemonLimitError> {
    let Some(value) = store.get(&lineage_key(tenant, scope)?) else {
        return Ok(None);
    };
    let corrupt = |_| DaemonLimitError::Corrupt;
    let mut decoder = layerx_wire::decode::Decoder::new(value.bytes(), 0);
    if decoder.fixed(4).map_err(corrupt)? != LINEAGE_MAGIC
        || decoder.u8().map_err(corrupt)? != LINEAGE_VERSION
    {
        return Err(DaemonLimitError::Corrupt);
    }
    let tag = decoder.u8().map_err(corrupt)?;
    let stored_scope = scope_from(tag, enrolment_fixed::<32>(&mut decoder)?)?;
    let current = enrolment_fixed::<32>(&mut decoder)?;
    decoder.finish().map_err(corrupt)?;
    if stored_scope != scope || current == [0; 32] {
        return Err(DaemonLimitError::Corrupt);
    }
    Ok(Some(current))
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

fn scope_has_other_live(
    store: &crate::store::Store,
    tenant: &crate::store::TenantId,
    scope: LimitScope,
    stable_id: [u8; 32],
) -> Result<bool, DaemonLimitError> {
    for object_id in store.list_object_ids(tenant, crate::store::ObjectKind::Configuration) {
        let Some(other) = object_id.strip_prefix(ENROLMENT_PREFIX) else {
            continue;
        };
        let other: [u8; 32] = other.try_into().map_err(|_| DaemonLimitError::Corrupt)?;
        if other == stable_id {
            continue;
        }
        let record = stored_enrolment(store, tenant, other)?.ok_or(DaemonLimitError::Corrupt)?;
        if record.scope == scope && record.successor.is_none() {
            return Ok(true);
        }
    }
    Ok(false)
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
        Ok(_) => Ok(()),
        Err(LimitRefusal::UnknownLimit(_)) => Ok(limiter.install(record.config())?),
        Err(refusal) => Err(refusal.into()),
    }
}

fn reinstall_enrolment(
    store: &mut crate::store::Store,
    tenant: &crate::store::TenantId,
    config: &LimitConfig,
    lineaged: bool,
) -> Result<EnrolmentLimitRecord, DaemonLimitError> {
    let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
    let limit_id = daemon_limit_id(stable_id);
    let key = enrolment_key(tenant, stable_id)?;
    let lineage = if lineaged {
        None
    } else {
        if scope_has_other_live(store, tenant, config.scope, stable_id)? {
            return Err(DaemonLimitError::Conflict);
        }
        Some((
            lineage_key(tenant, config.scope)?,
            encode_lineage(config.scope, stable_id)?,
        ))
    };
    if let Some(mut existing) = stored_enrolment(store, tenant, stable_id)? {
        if existing.successor.is_some() {
            return Err(DaemonLimitError::Corrupt);
        }
        if existing.ceiling != config.ceiling {
            return Err(DaemonLimitError::Conflict);
        }
        if !enrolment_indexed(store, tenant, limit_id, stable_id)? {
            return Err(DaemonLimitError::Corrupt);
        }
        let mut updates = Vec::new();
        if config.consumed > existing.consumed {
            existing.consumed = config.consumed;
            updates.push((key, existing.encode()?));
        }
        match lineage {
            Some(companion) if !updates.is_empty() => {
                store.update_local_batch_with_companions(updates, vec![companion])?;
            }
            Some((lineage_key, bytes)) => store.put_local(lineage_key, bytes)?,
            None => store.update_local_batch(updates)?,
        }
        return Ok(existing);
    }
    let Some((lineage_key, lineage_bytes)) = lineage else {
        return Err(DaemonLimitError::Corrupt);
    };
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
    if let Err(error) = store.put_local(key.clone(), bytes) {
        store.remove_local(&index)?;
        return Err(error.into());
    }
    if let Err(error) = store.put_local(lineage_key, lineage_bytes) {
        store.remove_local(&key)?;
        store.remove_local(&index)?;
        return Err(error.into());
    }
    Ok(record)
}

fn renew_enrolment(
    store: &mut crate::store::Store,
    tenant: &crate::store::TenantId,
    config: &LimitConfig,
    current: [u8; 32],
) -> Result<(EnrolmentLimitRecord, EnrolmentLimitRecord), DaemonLimitError> {
    let mut previous =
        stored_enrolment(store, tenant, current)?.ok_or(DaemonLimitError::Corrupt)?;
    if previous.successor.is_some()
        || previous.scope != config.scope
        || !enrolment_indexed(store, tenant, previous.limit_id, current)?
    {
        return Err(DaemonLimitError::Corrupt);
    }
    let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
    let limit_id = daemon_limit_id(stable_id);
    if stored_enrolment(store, tenant, stable_id)?.is_some() {
        return Err(DaemonLimitError::Conflict);
    }
    let index = enrolment_index_key(tenant, limit_id)?;
    refuse_index_taken(store, &index, stable_id)?;
    let consumed = previous.consumed.max(config.consumed);
    if consumed > config.ceiling {
        return Err(DaemonLimitError::Conflict);
    }
    let record = EnrolmentLimitRecord {
        tenant: tenant.clone(),
        stable_id,
        limit_id,
        scope: config.scope,
        enrolment: config.id,
        ceiling: config.ceiling,
        consumed,
        successor: None,
    };
    previous.successor = Some(stable_id);
    store.update_local_batch_with_companions(
        vec![
            (enrolment_key(tenant, current)?, previous.encode()?),
            (
                lineage_key(tenant, config.scope)?,
                encode_lineage(config.scope, stable_id)?,
            ),
        ],
        vec![
            (index, stable_id.to_vec()),
            (enrolment_key(tenant, stable_id)?, record.encode()?),
        ],
    )?;
    Ok((record, previous))
}

fn declare_enrolment(
    store: &mut crate::store::Store,
    tenant: &crate::store::TenantId,
    config: &LimitConfig,
) -> Result<(EnrolmentLimitRecord, Option<EnrolmentLimitRecord>), DaemonLimitError> {
    let stable_id = enrolment_limit_id(tenant, &config.scope, &config.id);
    match stored_lineage(store, tenant, config.scope)? {
        Some(current) if current != stable_id => {
            let (record, previous) = renew_enrolment(store, tenant, config, current)?;
            Ok((record, Some(previous)))
        }
        lineage => Ok((
            reinstall_enrolment(store, tenant, config, lineage.is_some())?,
            None,
        )),
    }
}

/// Installs every verified enrolment limit and every stored one, retiring the stored ones that
/// are no longer verified.
///
/// A durable lineage per tenant and complete canonical scope names the current stable identity.
/// Re-declaring a scope under a new `LimitConfig.id` renews it: the successor is created with
/// the predecessor's consumed total (or the declared one, whichever is larger) and the
/// predecessor is marked retired with a pointer to its successor in one store batch. The
/// predecessor is retired in the limiter only after the successor is installed, and keeps its
/// held reservations so they still resolve against it.
///
/// # Errors
/// Returns `Invalid` for a zero ceiling or consumed above it, `Conflict` for a stored record whose ceiling differs, two verified limits with one scope, a renewal whose carried consumed passes the new ceiling or that re-declares an already stored identity, and a scope with another live record but no lineage, `IdCollision` for a truncated-identifier collision, `Corrupt` for a malformed record, index or lineage, and store or limiter failures.
pub fn install_enrolment_limits(
    store: &mut crate::store::Store,
    limiter: &BudgetLimiter,
    tenant: &crate::store::TenantId,
    verified: &[LimitConfig],
) -> Result<Vec<EnrolmentLimitRecord>, DaemonLimitError> {
    let mut installed = Vec::new();
    for (position, config) in verified.iter().enumerate() {
        if config.ceiling == 0 || config.consumed > config.ceiling {
            return Err(DaemonLimitError::Invalid);
        }
        if verified[..position]
            .iter()
            .any(|other| other.scope == config.scope)
        {
            return Err(DaemonLimitError::Conflict);
        }
        let (record, previous) = declare_enrolment(store, tenant, config)?;
        install_once(limiter, &record)?;
        if let Some(previous) = previous {
            install_once(limiter, &previous)?;
            limiter.retire(previous.limit_id)?;
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
    Ok(installed)
}

/// # Errors
/// Returns `Unknown` when a hit verified limit has no durable enrolment record, `Corrupt` for a record or index that disagrees with the verified limit or is retired, and store failures.
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
        limits.push(config.id);
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

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};

    use super::{
        daemon_limit_id, enrolment_index_key, enrolment_key, enrolment_limit_id,
        install_enrolment_limits, reserve, restore, scope_bytes, scope_from, stored_enrolment,
        stored_lineage, BudgetLimiter, DaemonLimitError, EnrolmentLimitRecord, LimitConfig,
        LimitId, LimitRefusal, LimitScope, ReservationRequest, ENROLMENT_BYTES,
        ENROLMENT_RETIRED_BYTES, ENROLMENT_RETIRED_VERSION, ENROLMENT_VERSION,
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
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
            ),
            "renewal",
        );
        assert_eq!(renewed.consumed, 40);
        assert_eq!(renewed.successor, None);
        assert_eq!(limiter.consumed(renewed.limit_id), Ok(40));
        assert_eq!(limiter.is_retired(first.limit_id), Ok(true));
        assert_eq!(limiter.is_retired(renewed.limit_id), Ok(false));
        assert_eq!(
            text(stored_lineage(&store, &tenant, SCOPE), "lineage"),
            Some(renewed.stable_id)
        );
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
            install(&mut store, &limiter, &tenant, lowered),
            Err(DaemonLimitError::Conflict)
        ));
        assert_eq!(
            text(stored_enrolment(&store, &tenant, lowered_id), "absent"),
            None
        );
        assert_eq!(
            text(stored_lineage(&store, &tenant, SCOPE), "lineage"),
            Some(renewed.stable_id)
        );
        assert!(matches!(
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(1, SCOPE, CEILING, 0)
            ),
            Err(DaemonLimitError::Conflict)
        ));
        assert!(matches!(
            install_enrolment_limits(
                &mut store,
                &limiter,
                &tenant,
                &[
                    declared(2, SCOPE, CEILING, 0),
                    declared(4, SCOPE, CEILING, 0)
                ],
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
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
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
            install(&mut store, &limiter, &tenant, renewal),
            Err(DaemonLimitError::IdCollision)
        ));
        assert_eq!(stored(&store, &tenant, first.stable_id).successor, None);
        assert_eq!(
            text(stored_lineage(&store, &tenant, scope), "lineage"),
            Some(first.stable_id)
        );
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
            install(
                &mut store,
                &limiter,
                &tenant,
                declared(2, SCOPE, CEILING, 0),
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
        assert_eq!(
            text(stored_lineage(&store, &tenant, SCOPE), "lineage"),
            Some(first.stable_id)
        );
        assert_eq!(limiter.is_retired(first.limit_id), Ok(false));
    }
}
