//! Durable dynamic daemon-enforced limits kept in the existing store.

use layerx_wire::decode::Decoder;
use layerx_wire::encode::Encoder;
use sha2::{Digest as _, Sha256};

use super::reservations::{
    BudgetLimiter, CoreTimestampMs, DurableBudgetReservation, LimitConfig, LimitId, LimitRefusal,
    LimitScope,
};
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

const LIMIT_PREFIX: &[u8] = b"budget/daemon/limit/";
const INDEX_PREFIX: &[u8] = b"budget/daemon/limit-id/";
const LIMIT_ID_DOMAIN: &[u8] = b"layerx:budget-daemon-limit-id:v1\0";
const RECORD_MAGIC: &[u8; 4] = b"LXDL";
const RECORD_VERSION: u8 = 1;
const RECORD_BYTES: usize = 4 + 1 + 32 + 16 + 32 + 32 + 16 + 16 + 8 + 1 + 32 + 32 + 32;

/// One durable daemon-enforced limit. Bypassing the daemon bypasses it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DaemonLimitRecord {
    pub tenant: TenantId,
    pub budget_id: [u8; 32],
    pub limit_id: LimitId,
    pub agent_digest: [u8; 32],
    pub asset: [u8; 32],
    pub ceiling: u128,
    pub consumed: u128,
    pub expiry_ms: u64,
    pub revoked: bool,
    pub mutation_key: [u8; 32],
    pub body_digest: [u8; 32],
    /// Mutation key of the revocation; all zero while the limit is live.
    pub revoke_key: [u8; 32],
}

#[derive(Debug)]
pub enum DaemonLimitError {
    Store(StoreError),
    Corrupt,
    Invalid,
    Expired,
    IdCollision,
    Unknown,
    Revoked,
    Conflict,
    Arithmetic,
    Limit(LimitRefusal),
}

impl From<StoreError> for DaemonLimitError {
    fn from(error: StoreError) -> Self {
        Self::Store(error)
    }
}

impl From<LimitRefusal> for DaemonLimitError {
    fn from(error: LimitRefusal) -> Self {
        Self::Limit(error)
    }
}

/// Derives the limiter identifier of one daemon budget. The full 32-byte identifier is kept
/// in a durable index so two budgets whose truncated identifiers collide are refused.
#[must_use]
pub fn daemon_limit_id(budget_id: [u8; 32]) -> LimitId {
    let digest: [u8; 32] = Sha256::new()
        .chain_update(LIMIT_ID_DOMAIN)
        .chain_update(budget_id)
        .finalize()
        .into();
    let mut id = [0; 16];
    id.copy_from_slice(&digest[..16]);
    LimitId(id)
}

impl DaemonLimitRecord {
    fn encode(&self) -> Result<Vec<u8>, DaemonLimitError> {
        let mut encoder = Encoder::new(RECORD_BYTES);
        let corrupt = |_| DaemonLimitError::Corrupt;
        encoder.fixed(RECORD_MAGIC).map_err(corrupt)?;
        encoder.u8(RECORD_VERSION).map_err(corrupt)?;
        encoder.fixed(&self.budget_id).map_err(corrupt)?;
        encoder.fixed(&self.limit_id.0).map_err(corrupt)?;
        encoder.fixed(&self.agent_digest).map_err(corrupt)?;
        encoder.fixed(&self.asset).map_err(corrupt)?;
        encoder.u128(self.ceiling).map_err(corrupt)?;
        encoder.u128(self.consumed).map_err(corrupt)?;
        encoder.u64(self.expiry_ms).map_err(corrupt)?;
        encoder.u8(u8::from(self.revoked)).map_err(corrupt)?;
        encoder.fixed(&self.mutation_key).map_err(corrupt)?;
        encoder.fixed(&self.body_digest).map_err(corrupt)?;
        encoder.fixed(&self.revoke_key).map_err(corrupt)?;
        Ok(encoder.finish())
    }

    fn decode(tenant: TenantId, bytes: &[u8]) -> Result<Self, DaemonLimitError> {
        let corrupt = |_| DaemonLimitError::Corrupt;
        let mut decoder = Decoder::new(bytes, 0);
        if decoder.fixed(4).map_err(corrupt)? != RECORD_MAGIC
            || decoder.u8().map_err(corrupt)? != RECORD_VERSION
        {
            return Err(DaemonLimitError::Corrupt);
        }
        let budget_id = fixed::<32>(&mut decoder)?;
        let limit_id = LimitId(fixed::<16>(&mut decoder)?);
        let agent_digest = fixed::<32>(&mut decoder)?;
        let asset = fixed::<32>(&mut decoder)?;
        let ceiling = decoder.u128().map_err(corrupt)?;
        let consumed = decoder.u128().map_err(corrupt)?;
        let expiry_ms = decoder.u64().map_err(corrupt)?;
        let revoked = match decoder.u8().map_err(corrupt)? {
            0 => false,
            1 => true,
            _ => return Err(DaemonLimitError::Corrupt),
        };
        let mutation_key = fixed::<32>(&mut decoder)?;
        let body_digest = fixed::<32>(&mut decoder)?;
        let revoke_key = fixed::<32>(&mut decoder)?;
        decoder.finish().map_err(corrupt)?;
        let record = Self {
            tenant,
            budget_id,
            limit_id,
            agent_digest,
            asset,
            ceiling,
            consumed,
            expiry_ms,
            revoked,
            mutation_key,
            body_digest,
            revoke_key,
        };
        if record.limit_id != daemon_limit_id(record.budget_id) || !record.well_formed() {
            return Err(DaemonLimitError::Corrupt);
        }
        Ok(record)
    }

    fn well_formed(&self) -> bool {
        self.budget_id != [0; 32]
            && self.agent_digest != [0; 32]
            && self.asset != [0; 32]
            && self.ceiling != 0
            && self.consumed <= self.ceiling
            && self.expiry_ms != 0
            && self.mutation_key != [0; 32]
            && self.revoked == (self.revoke_key != [0; 32])
    }

    fn config(&self) -> LimitConfig {
        LimitConfig {
            id: self.limit_id,
            name: format!("daemon budget {}", hex(&self.budget_id)),
            scope: LimitScope::Agent(self.agent_digest),
            ceiling: self.ceiling,
            consumed: self.consumed,
        }
    }
}

fn fixed<const N: usize>(decoder: &mut Decoder<'_>) -> Result<[u8; N], DaemonLimitError> {
    decoder
        .fixed(N)
        .map_err(|_| DaemonLimitError::Corrupt)?
        .try_into()
        .map_err(|_| DaemonLimitError::Corrupt)
}

fn hex(value: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(value.len() * 2);
    for byte in value {
        encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
        encoded.push(char::from(DIGITS[usize::from(byte & 15)]));
    }
    encoded
}

fn limit_key(tenant: &TenantId, budget_id: [u8; 32]) -> Result<TenantKey, DaemonLimitError> {
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Configuration,
        [LIMIT_PREFIX, &budget_id].concat(),
    )?)
}

fn index_key(tenant: &TenantId, limit_id: LimitId) -> Result<TenantKey, DaemonLimitError> {
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Configuration,
        [INDEX_PREFIX, &limit_id.0].concat(),
    )?)
}

fn stored(
    store: &Store,
    tenant: &TenantId,
    budget_id: [u8; 32],
) -> Result<Option<DaemonLimitRecord>, DaemonLimitError> {
    store
        .get(&limit_key(tenant, budget_id)?)
        .map(|value| DaemonLimitRecord::decode(tenant.clone(), value.bytes()))
        .transpose()
}

/// Durably publishes and installs one daemon limit. The same mutation key and body digest
/// replays the stored record; any other create for the same budget identifier conflicts.
///
/// # Errors
///
/// Refuses a malformed record, an expiry at or before verified core time (equality is
/// expired), a truncated-identifier collision with a different budget, and store or limiter
/// failures.
pub fn create_daemon_limit(
    store: &mut Store,
    limiter: &BudgetLimiter,
    record: DaemonLimitRecord,
    core_now: CoreTimestampMs,
) -> Result<DaemonLimitRecord, DaemonLimitError> {
    if record.limit_id != daemon_limit_id(record.budget_id)
        || record.consumed != 0
        || record.revoked
        || !record.well_formed()
    {
        return Err(DaemonLimitError::Invalid);
    }
    if let Some(existing) = stored(store, &record.tenant, record.budget_id)? {
        if existing.mutation_key == record.mutation_key
            && existing.body_digest == record.body_digest
        {
            return Ok(existing);
        }
        return Err(DaemonLimitError::Conflict);
    }
    if record.expiry_ms <= core_now.0 {
        return Err(DaemonLimitError::Expired);
    }
    let index = index_key(&record.tenant, record.limit_id)?;
    if let Some(value) = store.get(&index) {
        if value.bytes() != record.budget_id.as_slice() {
            return Err(DaemonLimitError::IdCollision);
        }
        return Err(DaemonLimitError::Corrupt);
    }
    let bytes = record.encode()?;
    store.put_local(index.clone(), record.budget_id.to_vec())?;
    if let Err(error) = store.put_local(limit_key(&record.tenant, record.budget_id)?, bytes) {
        store.remove_local(&index)?;
        return Err(error.into());
    }
    limiter.install(record.config())?;
    Ok(record)
}

/// Durably revokes one daemon limit and retires it in the limiter. Holds and consumed totals
/// are kept for the outcomes that still resolve against them. The same mutation key replays.
///
/// # Errors
///
/// Returns `Unknown` for an absent limit, `Revoked` when another mutation already revoked it,
/// and store or limiter failures.
pub fn revoke_daemon_limit(
    store: &mut Store,
    limiter: &BudgetLimiter,
    tenant: &TenantId,
    budget_id: [u8; 32],
    mutation_key: [u8; 32],
) -> Result<DaemonLimitRecord, DaemonLimitError> {
    let mut record = stored(store, tenant, budget_id)?.ok_or(DaemonLimitError::Unknown)?;
    if record.revoked {
        return if record.revoke_key == mutation_key {
            Ok(record)
        } else {
            Err(DaemonLimitError::Revoked)
        };
    }
    if mutation_key == [0; 32] {
        return Err(DaemonLimitError::Invalid);
    }
    record.revoked = true;
    record.revoke_key = mutation_key;
    store.update_local_batch(vec![(limit_key(tenant, budget_id)?, record.encode()?)])?;
    limiter.retire(record.limit_id)?;
    Ok(record)
}

/// Lists one tenant's durable daemon limits in key order.
///
/// # Errors
///
/// Returns `Corrupt` for a malformed record or index, and store failures.
pub fn daemon_limits(
    store: &Store,
    tenant: &TenantId,
) -> Result<Vec<DaemonLimitRecord>, DaemonLimitError> {
    let mut records = Vec::new();
    for object_id in store.list_object_ids(tenant, ObjectKind::Configuration) {
        let Some(budget_id) = object_id.strip_prefix(LIMIT_PREFIX) else {
            continue;
        };
        let budget_id: [u8; 32] = budget_id
            .try_into()
            .map_err(|_| DaemonLimitError::Corrupt)?;
        let record = stored(store, tenant, budget_id)?.ok_or(DaemonLimitError::Corrupt)?;
        let index = store
            .get(&index_key(tenant, record.limit_id)?)
            .ok_or(DaemonLimitError::Corrupt)?;
        if index.bytes() != budget_id.as_slice() {
            return Err(DaemonLimitError::Corrupt);
        }
        records.push(record);
    }
    Ok(records)
}

/// Installs every durable daemon limit, retiring revoked ones, before any write is admitted.
///
/// # Errors
///
/// Returns `Corrupt` for malformed records and limiter failures for a duplicate identifier.
pub fn load_daemon_limits(
    store: &Store,
    limiter: &BudgetLimiter,
) -> Result<Vec<DaemonLimitRecord>, DaemonLimitError> {
    let mut loaded = Vec::new();
    for tenant in store.tenant_ids_for_kind(ObjectKind::Configuration) {
        for record in daemon_limits(store, &tenant)? {
            limiter.install(record.config())?;
            if record.revoked {
                limiter.retire(record.limit_id)?;
            }
            loaded.push(record);
        }
    }
    Ok(loaded)
}

/// Selects the live daemon limits that apply to one agent and asset at verified core time.
///
/// # Errors
///
/// Returns `Corrupt` for malformed records and store failures.
pub fn applicable_daemon_limits(
    store: &Store,
    tenant: &TenantId,
    agent_digest: [u8; 32],
    asset: [u8; 32],
    core_now: CoreTimestampMs,
) -> Result<Vec<LimitId>, DaemonLimitError> {
    Ok(daemon_limits(store, tenant)?
        .into_iter()
        .filter(|record| {
            !record.revoked
                && record.agent_digest == agent_digest
                && record.asset == asset
                && record.expiry_ms > core_now.0
        })
        .map(|record| record.limit_id)
        .collect())
}

/// Builds the durable consumed-total updates for the daemon limits named by executed holds.
/// The caller writes them in the same store batch as the preparation's terminal transition,
/// so a verified execution is consumed exactly once across restarts.
///
/// # Errors
///
/// Returns `Corrupt` for a hold whose daemon record is missing or whose scope or ceiling
/// differs, `Arithmetic` on overflow or a consumed total above the ceiling.
pub fn consumption_updates(
    store: &Store,
    tenant: &TenantId,
    holds: &[DurableBudgetReservation],
) -> Result<Vec<(TenantKey, Vec<u8>)>, DaemonLimitError> {
    let mut records: Vec<DaemonLimitRecord> = Vec::new();
    for hold in holds {
        let index = index_key(tenant, hold.limit_id)?;
        let Some(budget_id) = store.get(&index) else {
            continue;
        };
        let budget_id: [u8; 32] = budget_id
            .bytes()
            .try_into()
            .map_err(|_| DaemonLimitError::Corrupt)?;
        let position = match records.iter().position(|r| r.budget_id == budget_id) {
            Some(position) => position,
            None => {
                records.push(stored(store, tenant, budget_id)?.ok_or(DaemonLimitError::Corrupt)?);
                records.len() - 1
            }
        };
        let record = &mut records[position];
        if hold.scope != LimitScope::Agent(record.agent_digest) || hold.ceiling != record.ceiling {
            return Err(DaemonLimitError::Corrupt);
        }
        record.consumed = record
            .consumed
            .checked_add(hold.amount)
            .filter(|consumed| *consumed <= record.ceiling)
            .ok_or(DaemonLimitError::Arithmetic)?;
    }
    records
        .iter()
        .map(|record| Ok((limit_key(tenant, record.budget_id)?, record.encode()?)))
        .collect()
}
