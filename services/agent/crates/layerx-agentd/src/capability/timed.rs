//! Owner-issued capability records measured in core protocol time.

use std::collections::{BTreeMap, BTreeSet};

use layerx_agent_api::capability::CapabilityDimensions as PublicDimensions;
use layerx_agent_api::error::RequestId;
use sha2::{Digest, Sha256};

use crate::human::HumanOperationError;
use crate::identity::ProtocolAuthority;
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

use super::{Dimension, ProtocolScope};

const RECORD_PREFIX: &[u8] = b"timed-v1:";
const RECORD_VERSION: u8 = 1;

/// One owner-issued capability; every set is explicit and an empty set denies its dimension.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TimedCapability {
    pub id: [u8; 32],
    pub parent: Option<[u8; 32]>,
    pub tenant: TenantId,
    pub agent: String,
    pub authority: ProtocolAuthority,
    pub activity_types: BTreeSet<u16>,
    pub counterparties: BTreeSet<[u8; 32]>,
    pub assets: BTreeSet<[u8; 32]>,
    pub amount_ceilings: BTreeMap<[u8; 32], u128>,
    pub rate_ceilings: BTreeMap<u64, u64>,
    pub purposes: BTreeSet<String>,
    pub expiry_seconds: u64,
    pub grant_not_after_ms: u64,
    pub created_at_ms: u64,
    pub created_at_sequence: u64,
    pub revoked: Option<(u64, u64)>,
}

/// Observed state of a record at one core protocol instant.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TimedState {
    Active,
    Revoked,
    Expired,
}

/// Construction, narrowing, storage, or decoding failure.
#[derive(Debug)]
pub enum TimedError {
    Malformed,
    Duplicate(Dimension),
    ZeroWindow,
    CeilingOutsideAssets,
    Wider(Dimension),
    Expired,
    NotYetValid,
    UnknownParent,
    ParentInactive,
    NotFound,
    Conflict,
    Corrupt,
    SizeOverflow,
    Store(StoreError),
}

impl From<StoreError> for TimedError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

impl TimedError {
    /// Maps a failure to the owner refusal surface; only a wider dimension carries its dimension.
    #[must_use]
    pub fn owner_error(&self) -> HumanOperationError {
        match self {
            Self::Wider(dimension) => HumanOperationError::CapabilityRefused(*dimension),
            Self::Corrupt | Self::SizeOverflow | Self::Store(_) => HumanOperationError::Unavailable,
            _ => HumanOperationError::Refused,
        }
    }
}

/// Outcome of an idempotent insert.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Insert {
    Created(TimedCapability),
    Replayed(TimedCapability),
}

impl TimedCapability {
    /// Converts the public dimensions losslessly, refusing every collapsing or ambiguous entry.
    ///
    /// # Errors
    ///
    /// Refuses a non-canonical asset or counterparty reference, a duplicate entry, a zero rate
    /// window, an amount ceiling for an asset outside `assets`, and a zero expiry.
    #[allow(clippy::too_many_arguments)]
    pub fn from_public(
        id: [u8; 32],
        parent: Option<[u8; 32]>,
        tenant: TenantId,
        agent: &str,
        authority: ProtocolAuthority,
        dimensions: &PublicDimensions,
        grant_not_after_ms: u64,
        created_at_ms: u64,
        created_at_sequence: u64,
        request_id: RequestId,
    ) -> Result<Self, TimedError> {
        if agent.is_empty() || dimensions.expiry.get() == 0 {
            return Err(TimedError::Malformed);
        }
        let mut activity_types = BTreeSet::new();
        for value in dimensions.activity_types.values() {
            if !activity_types.insert(value.0) {
                return Err(TimedError::Duplicate(Dimension::ActivityType));
            }
        }
        let mut counterparties = BTreeSet::new();
        for value in dimensions.counterparties.values() {
            if !counterparties.insert(parse_id(value.as_str(), request_id)?) {
                return Err(TimedError::Duplicate(Dimension::Counterparty));
            }
        }
        let mut assets = BTreeSet::new();
        for value in dimensions.assets.values() {
            if !assets.insert(parse_id(value.as_str(), request_id)?) {
                return Err(TimedError::Duplicate(Dimension::Asset));
            }
        }
        let mut amount_ceilings = BTreeMap::new();
        for ceiling in dimensions.amount_ceilings.values() {
            let asset = parse_id(ceiling.asset.as_str(), request_id)?;
            if !assets.contains(&asset) {
                return Err(TimedError::CeilingOutsideAssets);
            }
            if amount_ceilings.insert(asset, ceiling.amount.get()).is_some() {
                return Err(TimedError::Duplicate(Dimension::Amount));
            }
        }
        let mut rate_ceilings = BTreeMap::new();
        for ceiling in dimensions.rate_ceilings.values() {
            let window = ceiling.window_seconds.get();
            if window == 0 {
                return Err(TimedError::ZeroWindow);
            }
            if rate_ceilings.insert(window, ceiling.maximum_actions).is_some() {
                return Err(TimedError::Duplicate(Dimension::Rate));
            }
        }
        let mut purposes = BTreeSet::new();
        for value in dimensions.purpose_constraints.values() {
            if !purposes.insert(value.as_str().to_owned()) {
                return Err(TimedError::Duplicate(Dimension::Purpose));
            }
        }
        Ok(Self {
            id,
            parent,
            tenant,
            agent: agent.to_owned(),
            authority,
            activity_types,
            counterparties,
            assets,
            amount_ceilings,
            rate_ceilings,
            purposes,
            expiry_seconds: dimensions.expiry.get(),
            grant_not_after_ms,
            created_at_ms,
            created_at_sequence,
            revoked: None,
        })
    }

    /// Exact millisecond instant at which the record stops admitting anything.
    #[must_use]
    pub fn not_after_ms(&self) -> u128 {
        (u128::from(self.expiry_seconds) * 1000).min(u128::from(self.grant_not_after_ms))
    }

    /// Expired when the core protocol time has reached the record's not-after instant.
    #[must_use]
    pub fn is_expired(&self, now_ms: u64) -> bool {
        u128::from(now_ms) >= self.not_after_ms()
    }

    #[must_use]
    pub fn state(&self, now_ms: u64) -> TimedState {
        if self.revoked.is_some() {
            TimedState::Revoked
        } else if self.is_expired(now_ms) {
            TimedState::Expired
        } else {
            TimedState::Active
        }
    }

    /// Compares the requested content, ignoring creation time, revocation and grant carriage.
    #[must_use]
    pub fn same_request(&self, other: &Self) -> bool {
        self.id == other.id
            && self.parent == other.parent
            && self.tenant == other.tenant
            && self.agent == other.agent
            && self.authority == other.authority
            && self.activity_types == other.activity_types
            && self.counterparties == other.counterparties
            && self.assets == other.assets
            && self.amount_ceilings == other.amount_ceilings
            && self.rate_ceilings == other.rate_ceilings
            && self.purposes == other.purposes
            && self.expiry_seconds == other.expiry_seconds
    }

    /// Encodes the versioned record layout.
    ///
    /// # Errors
    ///
    /// Returns `SizeOverflow` when a set or text exceeds its u16 length prefix.
    pub fn encode(&self) -> Result<Vec<u8>, TimedError> {
        let mut out = vec![RECORD_VERSION];
        out.extend_from_slice(&self.id);
        match &self.parent {
            Some(parent) => {
                out.push(1);
                out.extend_from_slice(parent);
            }
            None => out.push(0),
        }
        push_text(&mut out, self.tenant.as_str())?;
        push_text(&mut out, &self.agent)?;
        let (kind, authority) = match self.authority {
            ProtocolAuthority::PrimaryKey(value) => (1_u8, value),
            ProtocolAuthority::SessionKey(value) => (2, value),
            ProtocolAuthority::CapabilityGrant(value) => (3, value),
        };
        out.push(kind);
        out.extend_from_slice(&authority);
        push_len(&mut out, self.activity_types.len())?;
        for value in &self.activity_types {
            out.extend_from_slice(&value.to_be_bytes());
        }
        for set in [&self.counterparties, &self.assets] {
            push_len(&mut out, set.len())?;
            for value in set {
                out.extend_from_slice(value);
            }
        }
        push_len(&mut out, self.amount_ceilings.len())?;
        for (asset, amount) in &self.amount_ceilings {
            out.extend_from_slice(asset);
            out.extend_from_slice(&amount.to_be_bytes());
        }
        push_len(&mut out, self.rate_ceilings.len())?;
        for (window, maximum) in &self.rate_ceilings {
            out.extend_from_slice(&window.to_be_bytes());
            out.extend_from_slice(&maximum.to_be_bytes());
        }
        push_len(&mut out, self.purposes.len())?;
        for purpose in &self.purposes {
            push_text(&mut out, purpose)?;
        }
        for value in [
            self.expiry_seconds,
            self.grant_not_after_ms,
            self.created_at_ms,
            self.created_at_sequence,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        match self.revoked {
            Some((at_ms, at_sequence)) => {
                out.push(1);
                out.extend_from_slice(&at_ms.to_be_bytes());
                out.extend_from_slice(&at_sequence.to_be_bytes());
            }
            None => out.push(0),
        }
        Ok(out)
    }

    /// Decodes the versioned record layout exactly.
    ///
    /// # Errors
    ///
    /// Returns `Corrupt` for an unknown version or tag, truncated or trailing bytes, non-UTF-8
    /// text, an unordered or duplicate entry, or a zero rate window.
    pub fn decode(bytes: &[u8]) -> Result<Self, TimedError> {
        let mut r = Reader { bytes, offset: 0 };
        if r.u8()? != RECORD_VERSION {
            return Err(TimedError::Corrupt);
        }
        let id = r.id()?;
        let parent = match r.u8()? {
            0 => None,
            1 => Some(r.id()?),
            _ => return Err(TimedError::Corrupt),
        };
        let tenant = TenantId::new(r.text()?).map_err(|_| TimedError::Corrupt)?;
        let agent = r.text()?;
        let authority = match (r.u8()?, r.id()?) {
            (1, value) => ProtocolAuthority::PrimaryKey(value),
            (2, value) => ProtocolAuthority::SessionKey(value),
            (3, value) => ProtocolAuthority::CapabilityGrant(value),
            _ => return Err(TimedError::Corrupt),
        };
        let mut activity_types = BTreeSet::new();
        for _ in 0..r.len()? {
            ordered_insert(&mut activity_types, r.u16()?)?;
        }
        let mut counterparties = BTreeSet::new();
        for _ in 0..r.len()? {
            ordered_insert(&mut counterparties, r.id()?)?;
        }
        let mut assets = BTreeSet::new();
        for _ in 0..r.len()? {
            ordered_insert(&mut assets, r.id()?)?;
        }
        let mut amount_ceilings = BTreeMap::new();
        for _ in 0..r.len()? {
            let asset = r.id()?;
            let amount = r.u128()?;
            if amount_ceilings.last_key_value().is_some_and(|(last, _)| *last >= asset) {
                return Err(TimedError::Corrupt);
            }
            amount_ceilings.insert(asset, amount);
        }
        let mut rate_ceilings = BTreeMap::new();
        for _ in 0..r.len()? {
            let window = r.u64()?;
            let maximum = r.u64()?;
            if window == 0 || rate_ceilings.last_key_value().is_some_and(|(last, _)| *last >= window) {
                return Err(TimedError::Corrupt);
            }
            rate_ceilings.insert(window, maximum);
        }
        let mut purposes = BTreeSet::new();
        for _ in 0..r.len()? {
            ordered_insert(&mut purposes, r.text()?)?;
        }
        let expiry_seconds = r.u64()?;
        let grant_not_after_ms = r.u64()?;
        let created_at_ms = r.u64()?;
        let created_at_sequence = r.u64()?;
        let revoked = match r.u8()? {
            0 => None,
            1 => Some((r.u64()?, r.u64()?)),
            _ => return Err(TimedError::Corrupt),
        };
        if r.offset != bytes.len() || agent.is_empty() || expiry_seconds == 0 {
            return Err(TimedError::Corrupt);
        }
        Ok(Self {
            id,
            parent,
            tenant,
            agent,
            authority,
            activity_types,
            counterparties,
            assets,
            amount_ceilings,
            rate_ceilings,
            purposes,
            expiry_seconds,
            grant_not_after_ms,
            created_at_ms,
            created_at_sequence,
            revoked,
        })
    }
}

/// Identifier of a root record: SHA-256 over the create domain, tenant, agent and request key.
///
/// # Errors
///
/// Returns `SizeOverflow` when a text exceeds its u32 length prefix.
pub fn create_id(tenant: &str, agent: &str, key: &[u8; 32]) -> Result<[u8; 32], TimedError> {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/capability/create/v1\0");
    for value in [tenant.as_bytes(), agent.as_bytes()] {
        length_prefixed(&mut digest, value)?;
    }
    digest.update(key);
    Ok(digest.finalize().into())
}

/// Identifier of an attenuated record: the attenuate domain also binds the parent identifier.
///
/// # Errors
///
/// Returns `SizeOverflow` when a text exceeds its u32 length prefix.
pub fn attenuate_id(
    tenant: &str,
    agent: &str,
    parent: &[u8; 32],
    key: &[u8; 32],
) -> Result<[u8; 32], TimedError> {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/capability/attenuate/v1\0");
    for value in [tenant.as_bytes(), agent.as_bytes()] {
        length_prefixed(&mut digest, value)?;
    }
    digest.update(parent);
    digest.update(key);
    Ok(digest.finalize().into())
}

/// Decodes a capability id, core asset id or core account id through the strict `hex32` rule.
///
/// # Errors
///
/// Returns `Malformed` for anything other than exactly 64 lowercase hex characters.
pub fn parse_id(text: &str, request_id: RequestId) -> Result<[u8; 32], TimedError> {
    crate::agent_rpc_dispatch::hex32(text, request_id).map_err(|_| TimedError::Malformed)
}

/// Proves a record is no wider than the verified protocol scope at core time `now_ms`.
///
/// # Errors
///
/// Returns `NotYetValid` before the grant window opens and `Wider` naming the first dimension
/// that exceeds the scope. Rate and purpose have no protocol bound and are daemon-only.
pub fn check_scope(
    capability: &TimedCapability,
    scope: &ProtocolScope,
    now_ms: u64,
) -> Result<(), TimedError> {
    if now_ms < scope.not_before_ms {
        return Err(TimedError::NotYetValid);
    }
    if u128::from(capability.expiry_seconds) * 1000 > u128::from(scope.not_after_ms)
        || capability.grant_not_after_ms != scope.not_after_ms
    {
        return Err(TimedError::Wider(Dimension::Expiry));
    }
    if !capability.activity_types.is_subset(&scope.activity_types) {
        return Err(TimedError::Wider(Dimension::ActivityType));
    }
    if !capability.counterparties.is_subset(&scope.counterparties) {
        return Err(TimedError::Wider(Dimension::Counterparty));
    }
    if !capability.assets.is_subset(&scope.assets) {
        return Err(TimedError::Wider(Dimension::Asset));
    }
    if capability
        .amount_ceilings
        .values()
        .any(|amount| *amount > scope.amount_ceiling)
    {
        return Err(TimedError::Wider(Dimension::Amount));
    }
    Ok(())
}

/// Proves a child never widens any dimension of its parent.
///
/// # Errors
///
/// Returns `Wider` naming the first widened dimension in the stable dimension order.
pub fn require_subset(child: &TimedCapability, parent: &TimedCapability) -> Result<(), TimedError> {
    if child.not_after_ms() > parent.not_after_ms() || child.expiry_seconds > parent.expiry_seconds {
        return Err(TimedError::Wider(Dimension::Expiry));
    }
    if !child.activity_types.is_subset(&parent.activity_types) {
        return Err(TimedError::Wider(Dimension::ActivityType));
    }
    if !child.counterparties.is_subset(&parent.counterparties) {
        return Err(TimedError::Wider(Dimension::Counterparty));
    }
    if !child.assets.is_subset(&parent.assets) {
        return Err(TimedError::Wider(Dimension::Asset));
    }
    if child.amount_ceilings.iter().any(|(asset, amount)| {
        parent
            .amount_ceilings
            .get(asset)
            .is_none_or(|limit| amount > limit)
    }) {
        return Err(TimedError::Wider(Dimension::Amount));
    }
    if parent.rate_ceilings.iter().any(|(parent_window, parent_maximum)| {
        !child.rate_ceilings.iter().any(|(window, maximum)| {
            window >= parent_window && maximum <= parent_maximum
        })
    }) || (parent.rate_ceilings.is_empty() && !child.rate_ceilings.is_empty())
    {
        return Err(TimedError::Wider(Dimension::Rate));
    }
    if !child.purposes.is_subset(&parent.purposes) {
        return Err(TimedError::Wider(Dimension::Purpose));
    }
    Ok(())
}

/// Tenant key of one timed record; the prefix keeps it apart from legacy 32-byte ids.
///
/// # Errors
///
/// Returns the store error for an invalid tenant key.
pub fn record_key(tenant: &TenantId, id: &[u8; 32]) -> Result<TenantKey, TimedError> {
    let mut object = RECORD_PREFIX.to_vec();
    object.extend_from_slice(id);
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Capability, object)?)
}

/// Restores one record by identifier.
///
/// # Errors
///
/// Returns `Corrupt` when the stored bytes do not decode or belong to another tenant or id.
pub fn restore(
    store: &Store,
    tenant: &TenantId,
    id: &[u8; 32],
) -> Result<Option<TimedCapability>, TimedError> {
    let Some(value) = store.get(&record_key(tenant, id)?) else {
        return Ok(None);
    };
    let record = TimedCapability::decode(value.bytes())?;
    if record.id != *id || record.tenant != *tenant {
        return Err(TimedError::Corrupt);
    }
    Ok(Some(record))
}

/// Lists every timed record of one agent in identifier order, revoked records included.
///
/// # Errors
///
/// Returns `Corrupt` when any stored timed record does not decode.
pub fn list(store: &Store, tenant: &TenantId, agent: &str) -> Result<Vec<TimedCapability>, TimedError> {
    let mut out = Vec::new();
    for object in store.list_object_ids(tenant, ObjectKind::Capability) {
        let Some(id) = object.strip_prefix(RECORD_PREFIX) else {
            continue;
        };
        let id: [u8; 32] = id.try_into().map_err(|_| TimedError::Corrupt)?;
        let record = restore(store, tenant, &id)?.ok_or(TimedError::Corrupt)?;
        if record.agent == agent {
            out.push(record);
        }
    }
    Ok(out)
}

/// Requires the record and every ancestor to be present, unrevoked and unexpired at `now_ms`.
///
/// # Errors
///
/// Returns `UnknownParent` for a missing ancestor and `ParentInactive` for a revoked or expired
/// record anywhere in the chain.
pub fn require_active_chain(
    store: &Store,
    capability: &TimedCapability,
    now_ms: u64,
) -> Result<(), TimedError> {
    let mut current = capability.clone();
    let mut seen = BTreeSet::new();
    loop {
        if !seen.insert(current.id) {
            return Err(TimedError::Corrupt);
        }
        if current.state(now_ms) != TimedState::Active {
            return Err(TimedError::ParentInactive);
        }
        let Some(parent) = current.parent else {
            return Ok(());
        };
        current = restore(store, &capability.tenant, &parent)?.ok_or(TimedError::UnknownParent)?;
        if current.agent != capability.agent {
            return Err(TimedError::Corrupt);
        }
    }
}

/// Inserts a record durably, replaying an identical request and refusing a changed one.
///
/// # Errors
///
/// Returns `Conflict` when the identifier already holds different content and the store error
/// from the durable write.
pub fn insert(store: &mut Store, record: TimedCapability) -> Result<Insert, TimedError> {
    if let Some(existing) = restore(store, &record.tenant, &record.id)? {
        return if existing.same_request(&record) {
            Ok(Insert::Replayed(existing))
        } else {
            Err(TimedError::Conflict)
        };
    }
    store.put_local(record_key(&record.tenant, &record.id)?, record.encode()?)?;
    Ok(Insert::Created(record))
}

/// Revokes a record and every descendant of the same agent in one durable store transaction.
///
/// Records already revoked keep their original revocation. A target that is already revoked
/// returns its stored record and writes nothing.
///
/// # Errors
///
/// Returns `NotFound` for an unknown target or one owned by another agent, and the store error
/// when the single atomic batch cannot be persisted (nothing is changed then).
pub fn revoke_subtree(
    store: &mut Store,
    tenant: &TenantId,
    agent: &str,
    root: &[u8; 32],
    at_ms: u64,
    at_sequence: u64,
) -> Result<(TimedCapability, Vec<[u8; 32]>), TimedError> {
    let target = restore(store, tenant, root)?.ok_or(TimedError::NotFound)?;
    if target.agent != agent {
        return Err(TimedError::NotFound);
    }
    if target.revoked.is_some() {
        return Ok((target, Vec::new()));
    }
    let records = list(store, tenant, agent)?;
    let mut subtree = BTreeSet::from([*root]);
    loop {
        let before = subtree.len();
        for record in &records {
            if record.parent.is_some_and(|parent| subtree.contains(&parent)) {
                subtree.insert(record.id);
            }
        }
        if subtree.len() == before {
            break;
        }
    }
    let mut updates = Vec::new();
    let mut revoked = Vec::new();
    let mut result = target;
    for mut record in records {
        if !subtree.contains(&record.id) || record.revoked.is_some() {
            continue;
        }
        record.revoked = Some((at_ms, at_sequence));
        updates.push((record_key(tenant, &record.id)?, record.encode()?));
        revoked.push(record.id);
        if record.id == *root {
            result = record;
        }
    }
    store.update_local_batch(updates)?;
    Ok((result, revoked))
}

fn ordered_insert<T: Ord>(set: &mut BTreeSet<T>, value: T) -> Result<(), TimedError> {
    if set.last().is_some_and(|last| *last >= value) {
        return Err(TimedError::Corrupt);
    }
    set.insert(value);
    Ok(())
}

fn length_prefixed(digest: &mut Sha256, value: &[u8]) -> Result<(), TimedError> {
    let length = u32::try_from(value.len()).map_err(|_| TimedError::SizeOverflow)?;
    digest.update(length.to_be_bytes());
    digest.update(value);
    Ok(())
}

fn push_len(out: &mut Vec<u8>, value: usize) -> Result<(), TimedError> {
    let value = u16::try_from(value).map_err(|_| TimedError::SizeOverflow)?;
    out.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

fn push_text(out: &mut Vec<u8>, value: &str) -> Result<(), TimedError> {
    push_len(out, value.len())?;
    out.extend_from_slice(value.as_bytes());
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], TimedError> {
        let end = self.offset.checked_add(length).ok_or(TimedError::Corrupt)?;
        let value = self.bytes.get(self.offset..end).ok_or(TimedError::Corrupt)?;
        self.offset = end;
        Ok(value)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], TimedError> {
        self.take(N)?.try_into().map_err(|_| TimedError::Corrupt)
    }

    fn u8(&mut self) -> Result<u8, TimedError> {
        Ok(self.array::<1>()?[0])
    }

    fn u16(&mut self) -> Result<u16, TimedError> {
        Ok(u16::from_be_bytes(self.array()?))
    }

    fn u64(&mut self) -> Result<u64, TimedError> {
        Ok(u64::from_be_bytes(self.array()?))
    }

    fn u128(&mut self) -> Result<u128, TimedError> {
        Ok(u128::from_be_bytes(self.array()?))
    }

    fn id(&mut self) -> Result<[u8; 32], TimedError> {
        self.array()
    }

    fn len(&mut self) -> Result<usize, TimedError> {
        Ok(usize::from(self.u16()?))
    }

    fn text(&mut self) -> Result<String, TimedError> {
        let length = self.len()?;
        std::str::from_utf8(self.take(length)?)
            .map(str::to_owned)
            .map_err(|_| TimedError::Corrupt)
    }
}

#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum NativeSpendSourceV1 {
    Principal,
    Program { owner_program: [u8; 32], seed: Vec<u8>, source_account: [u8; 32] },
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NativeTimedCapabilityV1 {
    pub record: TimedCapability,
    pub activities: BTreeSet<layerx_agent_api::identity::NativeActivity>,
    pub purpose_commitments: BTreeSet<[u8; 32]>,
    pub spend_ceilings: BTreeMap<(NativeSpendSourceV1, [u8; 32]), u128>,
}

impl NativeTimedCapabilityV1 {
    pub const VERSION: u8 = 1;

    pub fn validate(&self) -> Result<(), TimedError> {
        if !self.record.activity_types.is_empty() || !self.record.purposes.is_empty()
            || self.record.agent.is_empty() || self.record.expiry_seconds == 0
            || self.record.grant_not_after_ms == 0
            || self.record.parent == Some(self.record.id)
        {
            return Err(TimedError::Malformed);
        }
        for activity in &self.activities {
            activity.validate().map_err(|_| TimedError::Malformed)?;
        }
        if self.record.rate_ceilings.keys().any(|window| *window == 0) {
            return Err(TimedError::ZeroWindow);
        }
        if self.record.amount_ceilings.keys().any(|asset| !self.record.assets.contains(asset))
            || self.spend_ceilings.iter().any(|((_, asset), amount)| {
                !self.record.assets.contains(asset)
                    || self.record.amount_ceilings.get(asset).is_none_or(|limit| amount > limit)
            })
        {
            return Err(TimedError::CeilingOutsideAssets);
        }
        Ok(())
    }

    pub fn encode(&self) -> Result<Vec<u8>, TimedError> {
        self.validate()?;
        let record = self.record.encode()?;
        let mut out = b"LXNC".to_vec();
        out.push(Self::VERSION);
        let length = u32::try_from(record.len()).map_err(|_| TimedError::SizeOverflow)?;
        out.extend_from_slice(&length.to_be_bytes());
        out.extend_from_slice(&record);
        push_len(&mut out, self.activities.len())?;
        for activity in &self.activities {
            out.extend_from_slice(&activity.encode().map_err(|_| TimedError::Malformed)?);
        }
        push_len(&mut out, self.purpose_commitments.len())?;
        for commitment in &self.purpose_commitments {
            out.extend_from_slice(commitment);
        }
        push_len(&mut out, self.spend_ceilings.len())?;
        for ((source, asset), amount) in &self.spend_ceilings {
            match source {
                NativeSpendSourceV1::Principal => out.push(0),
                NativeSpendSourceV1::Program { owner_program, seed, source_account } => {
                    out.push(1);
                    out.extend_from_slice(owner_program);
                    push_len(&mut out, seed.len())?;
                    out.extend_from_slice(seed);
                    out.extend_from_slice(source_account);
                }
            }
            out.extend_from_slice(asset);
            out.extend_from_slice(&amount.to_be_bytes());
        }
        Ok(out)
    }

    pub fn decode(bytes: &[u8]) -> Result<Self, TimedError> {
        let mut reader = Reader { bytes, offset: 0 };
        if reader.take(4)? != b"LXNC" || reader.u8()? != Self::VERSION {
            return Err(TimedError::Corrupt);
        }
        let length = usize::try_from(u32::from_be_bytes(reader.array()?))
            .map_err(|_| TimedError::SizeOverflow)?;
        let record = TimedCapability::decode(reader.take(length)?)?;
        let mut activities = BTreeSet::new();
        for _ in 0..reader.len()? {
            let activity = layerx_agent_api::identity::NativeActivity::decode(reader.take(5)?)
                .map_err(|_| TimedError::Corrupt)?;
            ordered_insert(&mut activities, activity)?;
        }
        let mut purpose_commitments = BTreeSet::new();
        for _ in 0..reader.len()? {
            ordered_insert(&mut purpose_commitments, reader.id()?)?;
        }
        let mut spend_ceilings = BTreeMap::new();
        for _ in 0..reader.len()? {
            let source = match reader.u8()? {
                0 => NativeSpendSourceV1::Principal,
                1 => {
                    let owner_program = reader.id()?;
                    let length = reader.len()?;
                    let seed = reader.take(length)?.to_vec();
                    let source_account = reader.id()?;
                    NativeSpendSourceV1::Program { owner_program, seed, source_account }
                }
                _ => return Err(TimedError::Corrupt),
            };
            let key = (source, reader.id()?);
            let amount = reader.u128()?;
            if spend_ceilings.last_key_value().is_some_and(|(last, _)| *last >= key) {
                return Err(TimedError::Corrupt);
            }
            spend_ceilings.insert(key, amount);
        }
        if reader.offset != bytes.len() {
            return Err(TimedError::Corrupt);
        }
        let value = Self { record, activities, purpose_commitments, spend_ceilings };
        value.validate().map_err(|_| TimedError::Corrupt)?;
        Ok(value)
    }
}

pub fn native_record_key(tenant: &TenantId, id: &[u8; 32]) -> Result<TenantKey, TimedError> {
    let mut object = b"native-timed-v1:".to_vec();
    object.extend_from_slice(id);
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Capability, object)?)
}

pub fn restore_native(
    store: &Store,
    tenant: &TenantId,
    id: &[u8; 32],
) -> Result<Option<NativeTimedCapabilityV1>, TimedError> {
    let Some(value) = store.get(&native_record_key(tenant, id)?) else {
        return Ok(None);
    };
    if value.class() != crate::store::StorageClass::LocalOnly {
        return Err(TimedError::Corrupt);
    }
    let record = NativeTimedCapabilityV1::decode(value.bytes())?;
    if record.record.id != *id || record.record.tenant != *tenant {
        return Err(TimedError::Corrupt);
    }
    Ok(Some(record))
}

pub fn native_active_chain(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
    id: &[u8; 32],
    now_ms: u64,
) -> Result<Vec<NativeTimedCapabilityV1>, TimedError> {
    let mut chain = Vec::new();
    let mut seen = BTreeSet::new();
    let mut next = *id;
    loop {
        if chain.len() >= 64 || !seen.insert(next) {
            return Err(TimedError::Corrupt);
        }
        let record = restore_native(store, tenant, &next)?.ok_or(TimedError::UnknownParent)?;
        if record.record.agent != agent || record.record.state(now_ms) != TimedState::Active {
            return Err(TimedError::ParentInactive);
        }
        if now_ms < record.record.created_at_ms {
            return Err(TimedError::NotYetValid);
        }
        if let Some(child) = chain.last() {
            super::require_native_subset(child, &record)?;
        }
        let parent = record.record.parent;
        chain.push(record);
        match parent {
            Some(id) => next = id,
            None => return Ok(chain),
        }
    }
}

#[cfg(test)]
mod native_tests {
    use super::*;
    use layerx_agent_api::identity::NativeActivity;

    fn must<T, E: core::fmt::Debug>(result: Result<T, E>) -> T {
        result.unwrap_or_else(|error| panic!("native capability: {error:?}"))
    }

    fn record() -> NativeTimedCapabilityV1 {
        NativeTimedCapabilityV1 {
            record: TimedCapability {
                id: [1; 32], parent: None,
                tenant: must(TenantId::new("native-tenant")), agent: "native-agent".into(),
                authority: ProtocolAuthority::PrimaryKey([2; 32]),
                activity_types: BTreeSet::new(), counterparties: BTreeSet::from([[3; 32]]),
                assets: BTreeSet::from([[4; 32]]),
                amount_ceilings: BTreeMap::from([([4; 32], 100)]),
                rate_ceilings: BTreeMap::from([(10, 3)]), purposes: BTreeSet::new(),
                expiry_seconds: 100, grant_not_after_ms: 100_000,
                created_at_ms: 1, created_at_sequence: 1, revoked: None,
            },
            activities: BTreeSet::from([must(NativeActivity::new(9, 3))]),
            purpose_commitments: BTreeSet::from([[5; 32]]),
            spend_ceilings: BTreeMap::from([
                ((NativeSpendSourceV1::Principal, [4; 32]), 50),
                ((NativeSpendSourceV1::Program {
                    owner_program: [6; 32], seed: vec![7, 8], source_account: [9; 32],
                }, [4; 32]), 100),
            ]),
        }
    }

    #[test]
    fn native_record_exact_codec_and_legacy_codec_remain_disjoint() {
        let record = record();
        let bytes = must(record.encode());
        assert_eq!(must(NativeTimedCapabilityV1::decode(&bytes)), record);
        assert!(TimedCapability::decode(&bytes).is_err());
        let legacy = must(record.record.encode());
        assert!(NativeTimedCapabilityV1::decode(&legacy).is_err());
        for end in 0..bytes.len() {
            assert!(NativeTimedCapabilityV1::decode(&bytes[..end]).is_err());
        }
        let mut trailing = bytes.clone();
        trailing.push(0);
        assert!(NativeTimedCapabilityV1::decode(&trailing).is_err());
        let mut wrong_version = bytes;
        wrong_version[4] = 2;
        assert!(NativeTimedCapabilityV1::decode(&wrong_version).is_err());
        let mut legacy_identity = record.clone();
        legacy_identity.record.activity_types.insert(3);
        assert!(legacy_identity.encode().is_err());
        let mut legacy_purpose = record;
        legacy_purpose.record.purposes.insert("label".into());
        assert!(legacy_purpose.encode().is_err());
    }

    #[test]
    fn native_attenuation_preserves_full_identity_source_and_every_bound() {
        let parent = record();
        let mut child = parent.clone();
        child.record.id = [10; 32];
        child.record.parent = Some(parent.record.id);
        child.record.amount_ceilings.insert([4; 32], 50);
        child.spend_ceilings.retain(|(source, _), _| *source == NativeSpendSourceV1::Principal);
        must(super::super::require_native_subset(&child, &parent));
        let mut wider = child.clone();
        wider.activities = BTreeSet::from([must(NativeActivity::new(1, 3))]);
        assert!(matches!(super::super::require_native_subset(&wider, &parent),
            Err(TimedError::Wider(Dimension::ActivityType))));
        let mut wider = child.clone();
        wider.purpose_commitments.insert([11; 32]);
        assert!(matches!(super::super::require_native_subset(&wider, &parent),
            Err(TimedError::Wider(Dimension::Purpose))));
        let mut wider = child.clone();
        wider.spend_ceilings.insert((NativeSpendSourceV1::Program {
            owner_program: [6; 32], seed: vec![7, 9], source_account: [9; 32],
        }, [4; 32]), 1);
        assert!(matches!(super::super::require_native_subset(&wider, &parent),
            Err(TimedError::Wider(Dimension::Amount))));
        let mut wider = child.clone();
        wider.record.expiry_seconds += 1;
        assert!(matches!(super::super::require_native_subset(&wider, &parent),
            Err(TimedError::Wider(Dimension::Expiry))));
        let mut wider = child;
        wider.record.rate_ceilings = BTreeMap::from([(1, 3)]);
        assert!(matches!(super::super::require_native_subset(&wider, &parent),
            Err(TimedError::Wider(Dimension::Rate))));
    }

    #[test]
    fn native_restore_chain_keeps_tenant_time_parent_and_revocation_binding() {
        let root = std::env::temp_dir().join(format!("native-capability-chain-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let parent = record();
        let mut child = parent.clone();
        child.record.id = [12; 32];
        child.record.parent = Some(parent.record.id);
        {
            let mut store = must(Store::open(root.join("store")));
            for record in [&parent, &child] {
                must(store.put_local(must(native_record_key(&record.record.tenant, &record.record.id)),
                    must(record.encode())));
            }
        }
        let mut store = must(Store::open(root.join("store")));
        let tenant = &parent.record.tenant;
        assert_eq!(must(native_active_chain(&store, tenant, "native-agent", &child.record.id, 1)).len(), 2);
        assert!(native_active_chain(&store, tenant, "other-agent", &child.record.id, 1).is_err());
        assert!(native_active_chain(&store, tenant, "native-agent", &child.record.id, 0).is_err());
        assert!(native_active_chain(&store, tenant, "native-agent", &child.record.id, 100_000).is_err());
        assert!(native_active_chain(&store, &must(TenantId::new("other")), "native-agent", &child.record.id, 1).is_err());
        let mut revoked = parent.clone();
        revoked.record.revoked = Some((2, 2));
        must(store.put_local(must(native_record_key(tenant, &parent.record.id)), must(revoked.encode())));
        assert!(native_active_chain(&store, tenant, "native-agent", &child.record.id, 2).is_err());
        drop(store);
        let _ = std::fs::remove_dir_all(root);
    }
}
