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
