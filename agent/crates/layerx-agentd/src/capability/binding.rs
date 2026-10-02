//! Durable binding of owner-issued timed capabilities to preparations.

use std::collections::BTreeSet;

use sha2::{Digest, Sha256};

use crate::human::HumanOperationError;
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

use super::timed::{self, TimedCapability, TimedError, TimedState};
use super::{Decision, Dimension};

/// Separate domain for a capability-bound prepare body; the legacy prepare digest is unchanged.
pub const PREPARE_CAPABILITY_DOMAIN: &[u8] = b"layerx-human-journey-prepare-capability/v1\0";
const CHAIN_DOMAIN: &[u8] = b"layerx-capability-chain/v1\0";
const INTENT_DOMAIN: &[u8] = b"layerx-capability-intent/v1\0";
const BINDING_PREFIX: &[u8] = b"timed-binding-v1:";
const USES_PREFIX: &[u8] = b"timed-uses-v1:";
const CLEANUP_PREFIX: &[u8] = b"timed-cleanup-v1:";
const VERSION: u8 = 1;
const MAX_CHAIN: usize = 64;

/// Binding, admission, decoding or storage failure.
#[derive(Debug)]
pub enum BindingError {
    Restricted,
    Unbound,
    Cancelled,
    Refused(Dimension),
    Chain(TimedError),
    Conflict,
    Corrupt,
    SizeOverflow,
    Store(StoreError),
}

impl From<StoreError> for BindingError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

impl From<TimedError> for BindingError {
    fn from(value: TimedError) -> Self {
        Self::Chain(value)
    }
}

impl BindingError {
    /// Maps a failure to the owner refusal surface; a dimension refusal keeps its dimension.
    #[must_use]
    pub fn owner_error(&self) -> HumanOperationError {
        match self {
            Self::Refused(dimension) => HumanOperationError::CapabilityRefused(*dimension),
            Self::Chain(error) => error.owner_error(),
            Self::Corrupt | Self::SizeOverflow | Self::Store(_) => HumanOperationError::Unavailable,
            Self::Restricted | Self::Unbound | Self::Cancelled | Self::Conflict => {
                HumanOperationError::Refused
            }
        }
    }
}

/// Capability-bound body digest: SHA-256 over the separate domain, the legacy digest and the id.
#[must_use]
pub fn bound_prepare_digest(legacy: &[u8; 32], capability_id: &[u8; 32]) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(PREPARE_CAPABILITY_DOMAIN);
    digest.update(legacy);
    digest.update(capability_id);
    digest.finalize().into()
}

/// Prepare body digest: exactly the legacy bytes when no capability id is present.
#[must_use]
pub fn prepare_body_digest(legacy: [u8; 32], capability_id: Option<&[u8; 32]>) -> [u8; 32] {
    match capability_id {
        Some(id) => bound_prepare_digest(&legacy, id),
        None => legacy,
    }
}

/// Canonical digest of an ancestor chain, leaf first; fixed-width ids need no count.
#[must_use]
pub fn chain_digest(chain: &[[u8; 32]]) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(CHAIN_DOMAIN);
    for id in chain {
        digest.update(id);
    }
    digest.finalize().into()
}

/// Prepared activity fields evaluated against every record of the selected chain.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TimedIntent {
    pub activity_type: u16,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub amount: u128,
    pub purpose: String,
}

impl TimedIntent {
    /// Canonical digest of the evaluated intent.
    ///
    /// # Errors
    ///
    /// Returns `SizeOverflow` when the purpose exceeds its u32 length prefix.
    pub fn digest(&self) -> Result<[u8; 32], BindingError> {
        let mut digest = Sha256::new();
        digest.update(INTENT_DOMAIN);
        digest.update(self.activity_type.to_be_bytes());
        digest.update(self.counterparty);
        digest.update(self.asset);
        digest.update(self.amount.to_be_bytes());
        let length = u32::try_from(self.purpose.len()).map_err(|_| BindingError::SizeOverflow)?;
        digest.update(length.to_be_bytes());
        digest.update(self.purpose.as_bytes());
        Ok(digest.finalize().into())
    }
}

/// Capability binding retained with a preparation; `chain[0]` is the selected id, the last is the root.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapabilityExtension {
    pub capability_id: [u8; 32],
    pub chain: Vec<[u8; 32]>,
    pub intent_digest: [u8; 32],
    pub admitted_at_ms: u64,
}

impl CapabilityExtension {
    /// Encodes the versioned extension bytes.
    ///
    /// # Errors
    ///
    /// Returns `Corrupt` for an empty, oversized, duplicated or mis-rooted chain.
    pub fn encode(&self) -> Result<Vec<u8>, BindingError> {
        check_chain(self.capability_id, &self.chain)?;
        let mut out = vec![VERSION];
        out.extend_from_slice(&self.capability_id);
        push_u16(&mut out, self.chain.len())?;
        for id in &self.chain {
            out.extend_from_slice(id);
        }
        out.extend_from_slice(&self.intent_digest);
        out.extend_from_slice(&self.admitted_at_ms.to_be_bytes());
        Ok(out)
    }

    /// Decodes the extension bytes exactly.
    ///
    /// # Errors
    ///
    /// Returns `Corrupt` for an unknown version, a malformed chain, or truncated or trailing bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        r.version()?;
        let capability_id = r.id()?;
        let count = usize::from(r.u16()?);
        let mut chain = Vec::with_capacity(count.min(MAX_CHAIN));
        for _ in 0..count {
            chain.push(r.id()?);
        }
        let intent_digest = r.id()?;
        let admitted_at_ms = r.u64()?;
        r.finish()?;
        check_chain(capability_id, &chain)?;
        Ok(Self {
            capability_id,
            chain,
            intent_digest,
            admitted_at_ms,
        })
    }
}

/// Recorded admission state of one preparation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum OutcomeState {
    Admitted,
    Cancelled { at_ms: u64, at_sequence: u64 },
}

/// Idempotent admission outcome retained with a preparation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmissionOutcome {
    pub body_digest: [u8; 32],
    pub capability_id: Option<[u8; 32]>,
    pub chain_digest: [u8; 32],
    pub state: OutcomeState,
}

impl AdmissionOutcome {
    #[must_use]
    pub fn admitted(body_digest: [u8; 32], extension: Option<&CapabilityExtension>) -> Self {
        Self {
            body_digest,
            capability_id: extension.map(|value| value.capability_id),
            chain_digest: chain_digest(extension.map_or(&[], |value| value.chain.as_slice())),
            state: OutcomeState::Admitted,
        }
    }

    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = vec![VERSION];
        out.extend_from_slice(&self.body_digest);
        match &self.capability_id {
            Some(id) => {
                out.push(1);
                out.extend_from_slice(id);
            }
            None => out.push(0),
        }
        out.extend_from_slice(&self.chain_digest);
        match self.state {
            OutcomeState::Admitted => out.push(0),
            OutcomeState::Cancelled { at_ms, at_sequence } => {
                out.push(1);
                out.extend_from_slice(&at_ms.to_be_bytes());
                out.extend_from_slice(&at_sequence.to_be_bytes());
            }
        }
        out
    }

    /// Decodes the outcome bytes exactly.
    ///
    /// # Errors
    ///
    /// Returns `Corrupt` for an unknown version or tag, or truncated or trailing bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        r.version()?;
        let body_digest = r.id()?;
        let capability_id = match r.u8()? {
            0 => None,
            1 => Some(r.id()?),
            _ => return Err(BindingError::Corrupt),
        };
        let chain_digest = r.id()?;
        let state = match r.u8()? {
            0 => OutcomeState::Admitted,
            1 => OutcomeState::Cancelled {
                at_ms: r.u64()?,
                at_sequence: r.u64()?,
            },
            _ => return Err(BindingError::Corrupt),
        };
        r.finish()?;
        Ok(Self {
            body_digest,
            capability_id,
            chain_digest,
            state,
        })
    }

    /// Replays a retry only for the identical body and capability; never re-admits.
    ///
    /// # Errors
    ///
    /// Returns `Conflict` for a different body or capability and `Cancelled` once revoked.
    pub fn replay(
        &self,
        body_digest: &[u8; 32],
        capability_id: Option<&[u8; 32]>,
    ) -> Result<(), BindingError> {
        if self.body_digest != *body_digest || self.capability_id.as_ref() != capability_id {
            return Err(BindingError::Conflict);
        }
        match self.state {
            OutcomeState::Admitted => Ok(()),
            OutcomeState::Cancelled { .. } => Err(BindingError::Cancelled),
        }
    }

    /// The same outcome durably cancelled; an earlier cancellation is kept.
    #[must_use]
    pub fn cancelled(&self, at_ms: u64, at_sequence: u64) -> Self {
        let mut out = self.clone();
        if out.state == OutcomeState::Admitted {
            out.state = OutcomeState::Cancelled { at_ms, at_sequence };
        }
        out
    }
}

/// Durable owner-controlled restriction of one agent; never removed once written.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AgentBinding {
    pub tenant: TenantId,
    pub agent: String,
    pub restricted_at_ms: u64,
    pub restricted_at_sequence: u64,
}

impl AgentBinding {
    fn encode(&self) -> Result<Vec<u8>, BindingError> {
        let mut out = vec![VERSION];
        push_text(&mut out, self.tenant.as_str())?;
        push_text(&mut out, &self.agent)?;
        out.extend_from_slice(&self.restricted_at_ms.to_be_bytes());
        out.extend_from_slice(&self.restricted_at_sequence.to_be_bytes());
        Ok(out)
    }

    fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        r.version()?;
        let tenant = TenantId::new(r.text()?).map_err(|_| BindingError::Corrupt)?;
        let agent = r.text()?;
        let restricted_at_ms = r.u64()?;
        let restricted_at_sequence = r.u64()?;
        r.finish()?;
        if agent.is_empty() {
            return Err(BindingError::Corrupt);
        }
        Ok(Self {
            tenant,
            agent,
            restricted_at_ms,
            restricted_at_sequence,
        })
    }
}

/// Store key of an agent restriction.
///
/// # Errors
///
/// Returns the store error for an invalid tenant key.
pub fn binding_key(tenant: &TenantId, agent: &str) -> Result<TenantKey, BindingError> {
    let mut object = BINDING_PREFIX.to_vec();
    object.extend_from_slice(&Sha256::digest(agent.as_bytes()));
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Capability, object)?)
}

/// Restores the restriction of one agent.
///
/// # Errors
///
/// Returns `Corrupt` when the stored bytes do not decode or name another tenant or agent.
pub fn restore_binding(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
) -> Result<Option<AgentBinding>, BindingError> {
    let Some(value) = store.get(&binding_key(tenant, agent)?) else {
        return Ok(None);
    };
    let binding = AgentBinding::decode(value.bytes())?;
    if binding.tenant != *tenant || binding.agent != agent {
        return Err(BindingError::Corrupt);
    }
    Ok(Some(binding))
}

/// Durably restricts an agent to capability-bound preparation; an existing restriction is kept.
///
/// # Errors
///
/// Returns `Corrupt` for an undecodable stored restriction and the durable write failure.
pub fn restrict(
    store: &mut Store,
    tenant: &TenantId,
    agent: &str,
    at_ms: u64,
    at_sequence: u64,
) -> Result<AgentBinding, BindingError> {
    if agent.is_empty() {
        return Err(BindingError::Corrupt);
    }
    if let Some(existing) = restore_binding(store, tenant, agent)? {
        return Ok(existing);
    }
    let binding = AgentBinding {
        tenant: tenant.clone(),
        agent: agent.to_owned(),
        restricted_at_ms: at_ms,
        restricted_at_sequence: at_sequence,
    };
    store.put_local(binding_key(tenant, agent)?, binding.encode()?)?;
    Ok(binding)
}

/// Restricted when the durable restriction exists or any timed record, revoked included, exists.
///
/// # Errors
///
/// Returns `Corrupt` for undecodable stored records.
pub fn is_restricted(store: &Store, tenant: &TenantId, agent: &str) -> Result<bool, BindingError> {
    Ok(restore_binding(store, tenant, agent)?.is_some()
        || !timed::list(store, tenant, agent)?.is_empty())
}

/// Issues an owner record: the restriction is written first, so a failure leaves the agent restricted.
///
/// # Errors
///
/// Returns the restriction failure, or the timed insert refusal or store failure.
pub fn issue(
    store: &mut Store,
    record: TimedCapability,
    at_ms: u64,
    at_sequence: u64,
) -> Result<timed::Insert, BindingError> {
    let tenant = record.tenant.clone();
    let agent = record.agent.clone();
    restrict(store, &tenant, &agent, at_ms, at_sequence)?;
    Ok(timed::insert(store, record)?)
}

/// Atomic store plan: `updates` name existing local keys and `companions` absent ones.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ChargePlan {
    pub updates: Vec<(TenantKey, Vec<u8>)>,
    pub companions: Vec<(TenantKey, Vec<u8>)>,
}

/// Prepare admission decision.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Admission {
    Legacy,
    Bound {
        extension: CapabilityExtension,
        plan: ChargePlan,
    },
}

/// Admits one preparation. Without an id the legacy path is taken only for an unrestricted agent;
/// with an id every record of the active chain must admit the intent, and the plan charges exactly
/// those records once per preparation.
///
/// # Errors
///
/// Returns `Restricted`, `Unbound`, `Refused` naming the first failing dimension, the chain
/// failure, or `Corrupt` for undecodable stored state.
pub fn admit(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
    capability_id: Option<[u8; 32]>,
    preparation_id: [u8; 32],
    intent: &TimedIntent,
    now_ms: u64,
) -> Result<Admission, BindingError> {
    let Some(capability_id) = capability_id else {
        return if is_restricted(store, tenant, agent)? {
            Err(BindingError::Restricted)
        } else {
            Ok(Admission::Legacy)
        };
    };
    let binding_key = binding_key(tenant, agent)?;
    let binding = store.get(&binding_key).ok_or(BindingError::Unbound)?;
    let binding_bytes = binding.bytes().to_vec();
    restore_binding(store, tenant, agent)?;
    let chain = active_chain(store, tenant, agent, &capability_id, now_ms)?;
    let mut logs = Vec::with_capacity(chain.len());
    for record in &chain {
        let key = uses_key(tenant, &record.id)?;
        let log = match store.get(&key) {
            Some(value) => Some(UseLog::decode(value.bytes())?),
            None => None,
        };
        if let Decision::Refuse(dimension) = evaluate(
            record,
            intent,
            log.as_ref().map_or(&[][..], |value| value.0.as_slice()),
            &preparation_id,
            now_ms,
        ) {
            return Err(BindingError::Refused(dimension));
        }
        logs.push((key, log));
    }
    let mut plan = ChargePlan {
        updates: vec![(binding_key, binding_bytes)],
        companions: Vec::new(),
    };
    for (record, (key, log)) in chain.iter().zip(logs) {
        let existed = log.is_some();
        let mut log = log.unwrap_or_default();
        log.charge(preparation_id, record, now_ms);
        let bytes = log.encode()?;
        if existed {
            plan.updates.push((key, bytes));
        } else {
            plan.companions.push((key, bytes));
        }
    }
    Ok(Admission::Bound {
        extension: CapabilityExtension {
            capability_id,
            chain: chain.iter().map(|record| record.id).collect(),
            intent_digest: intent.digest()?,
            admitted_at_ms: now_ms,
        },
        plan,
    })
}

/// Rechecks the same bindings at sign and at direct external submit.
///
/// # Errors
///
/// Returns `Restricted` for a legacy preparation of a restricted agent, `Unbound`, the chain
/// failure for a revoked, expired or missing record, and `Corrupt` for a changed chain.
pub fn recheck(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
    extension: Option<&CapabilityExtension>,
    now_ms: u64,
) -> Result<(), BindingError> {
    let Some(extension) = extension else {
        return if is_restricted(store, tenant, agent)? {
            Err(BindingError::Restricted)
        } else {
            Ok(())
        };
    };
    if restore_binding(store, tenant, agent)?.is_none() {
        return Err(BindingError::Unbound);
    }
    let chain = active_chain(store, tenant, agent, &extension.capability_id, now_ms)?;
    if chain
        .iter()
        .map(|record| record.id)
        .ne(extension.chain.iter().copied())
    {
        return Err(BindingError::Corrupt);
    }
    Ok(())
}

/// Evaluates one record of the chain in the stable dimension order; an empty set denies.
#[must_use]
pub fn evaluate(
    record: &TimedCapability,
    intent: &TimedIntent,
    uses: &[([u8; 32], u64)],
    preparation_id: &[u8; 32],
    now_ms: u64,
) -> Decision {
    if record.is_expired(now_ms) {
        return Decision::Refuse(Dimension::Expiry);
    }
    if !record.activity_types.contains(&intent.activity_type) {
        return Decision::Refuse(Dimension::ActivityType);
    }
    if !record.counterparties.contains(&intent.counterparty) {
        return Decision::Refuse(Dimension::Counterparty);
    }
    if !record.assets.contains(&intent.asset) {
        return Decision::Refuse(Dimension::Asset);
    }
    if record
        .amount_ceilings
        .get(&intent.asset)
        .is_none_or(|ceiling| intent.amount > *ceiling)
    {
        return Decision::Refuse(Dimension::Amount);
    }
    if record.rate_ceilings.is_empty()
        || record.rate_ceilings.iter().any(|(window, maximum)| {
            let used = uses
                .iter()
                .filter(|(id, at_ms)| {
                    id != preparation_id
                        && u128::from(*at_ms) + u128::from(*window) * 1000 > u128::from(now_ms)
                })
                .count();
            u64::try_from(used).map_or(true, |used| used >= *maximum)
        })
    {
        return Decision::Refuse(Dimension::Rate);
    }
    if !record.purposes.contains(&intent.purpose) {
        return Decision::Refuse(Dimension::Purpose);
    }
    Decision::Allow
}

/// Durable record that revocation cleanup of unsent preparations is still owed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RevocationCleanup {
    pub tenant: TenantId,
    pub agent: String,
    pub root: [u8; 32],
    pub at_ms: u64,
    pub at_sequence: u64,
}

impl RevocationCleanup {
    fn encode(&self) -> Result<Vec<u8>, BindingError> {
        let mut out = vec![VERSION];
        push_text(&mut out, self.tenant.as_str())?;
        push_text(&mut out, &self.agent)?;
        out.extend_from_slice(&self.root);
        out.extend_from_slice(&self.at_ms.to_be_bytes());
        out.extend_from_slice(&self.at_sequence.to_be_bytes());
        Ok(out)
    }

    fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        r.version()?;
        let tenant = TenantId::new(r.text()?).map_err(|_| BindingError::Corrupt)?;
        let agent = r.text()?;
        let root = r.id()?;
        let at_ms = r.u64()?;
        let at_sequence = r.u64()?;
        r.finish()?;
        Ok(Self {
            tenant,
            agent,
            root,
            at_ms,
            at_sequence,
        })
    }
}

fn cleanup_key(tenant: &TenantId, root: &[u8; 32]) -> Result<TenantKey, BindingError> {
    let mut object = CLEANUP_PREFIX.to_vec();
    object.extend_from_slice(root);
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Capability, object)?)
}

/// Revokes a subtree after durably recording the cleanup it owes.
///
/// # Errors
///
/// Returns `NotFound` (as a chain failure) for an unknown or foreign target, `Corrupt` for a
/// conflicting cleanup record, and the store failure.
pub fn revoke(
    store: &mut Store,
    tenant: &TenantId,
    agent: &str,
    root: &[u8; 32],
    at_ms: u64,
    at_sequence: u64,
) -> Result<(TimedCapability, Vec<[u8; 32]>), BindingError> {
    let target = timed::restore(store, tenant, root)?.ok_or(TimedError::NotFound)?;
    if target.agent != agent {
        return Err(TimedError::NotFound.into());
    }
    let key = cleanup_key(tenant, root)?;
    match store.get(&key) {
        Some(value) => {
            let existing = RevocationCleanup::decode(value.bytes())?;
            if existing.tenant != *tenant || existing.agent != agent || existing.root != *root {
                return Err(BindingError::Corrupt);
            }
        }
        None => {
            let cleanup = RevocationCleanup {
                tenant: tenant.clone(),
                agent: agent.to_owned(),
                root: *root,
                at_ms,
                at_sequence,
            };
            store.put_local(key, cleanup.encode()?)?;
        }
    }
    Ok(timed::revoke_subtree(
        store,
        tenant,
        agent,
        root,
        at_ms,
        at_sequence,
    )?)
}

/// Every cleanup still owed for one tenant, in root order.
///
/// # Errors
///
/// Returns `Corrupt` for an undecodable or mis-keyed record.
pub fn pending_cleanups(
    store: &Store,
    tenant: &TenantId,
) -> Result<Vec<RevocationCleanup>, BindingError> {
    let mut out = Vec::new();
    for object in store.list_object_ids(tenant, ObjectKind::Capability) {
        let Some(root) = object.strip_prefix(CLEANUP_PREFIX) else {
            continue;
        };
        let root: [u8; 32] = root.try_into().map_err(|_| BindingError::Corrupt)?;
        let value = store
            .get(&cleanup_key(tenant, &root)?)
            .ok_or(BindingError::Corrupt)?;
        let cleanup = RevocationCleanup::decode(value.bytes())?;
        if cleanup.tenant != *tenant || cleanup.root != root {
            return Err(BindingError::Corrupt);
        }
        out.push(cleanup);
    }
    Ok(out)
}

/// Every revoked record of one agent.
///
/// # Errors
///
/// Returns `Corrupt` for undecodable stored records.
pub fn revoked_ids(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
) -> Result<BTreeSet<[u8; 32]>, BindingError> {
    Ok(timed::list(store, tenant, agent)?
        .into_iter()
        .filter(|record| record.revoked.is_some())
        .map(|record| record.id)
        .collect())
}

/// A preparation is invalidated when any record of its chain is revoked.
#[must_use]
pub fn is_invalidated(extension: &CapabilityExtension, revoked: &BTreeSet<[u8; 32]>) -> bool {
    extension.chain.iter().any(|id| revoked.contains(id))
}

/// Removes a cleanup record only once its root is durably revoked.
///
/// # Errors
///
/// Returns the store failure or `Corrupt` for undecodable stored records.
pub fn complete_cleanup(
    store: &mut Store,
    cleanup: &RevocationCleanup,
) -> Result<bool, BindingError> {
    let revoked = timed::restore(store, &cleanup.tenant, &cleanup.root)?
        .is_some_and(|record| record.revoked.is_some());
    if !revoked {
        return Ok(false);
    }
    Ok(store.remove_local(&cleanup_key(&cleanup.tenant, &cleanup.root)?)?)
}

fn active_chain(
    store: &Store,
    tenant: &TenantId,
    agent: &str,
    capability_id: &[u8; 32],
    now_ms: u64,
) -> Result<Vec<TimedCapability>, BindingError> {
    let leaf = timed::restore(store, tenant, capability_id)?.ok_or(TimedError::NotFound)?;
    if leaf.agent != agent {
        return Err(TimedError::NotFound.into());
    }
    timed::require_active_chain(store, &leaf, now_ms)?;
    let mut chain = vec![leaf];
    while let Some(parent) = chain.last().and_then(|record| record.parent) {
        if chain.len() >= MAX_CHAIN {
            return Err(BindingError::Corrupt);
        }
        let record = timed::restore(store, tenant, &parent)?.ok_or(TimedError::UnknownParent)?;
        if record.agent != agent || record.state(now_ms) != TimedState::Active {
            return Err(TimedError::ParentInactive.into());
        }
        chain.push(record);
    }
    Ok(chain)
}

fn check_chain(capability_id: [u8; 32], chain: &[[u8; 32]]) -> Result<(), BindingError> {
    let unique: BTreeSet<_> = chain.iter().collect();
    if chain.first() != Some(&capability_id) || chain.len() > MAX_CHAIN || unique.len() != chain.len()
    {
        return Err(BindingError::Corrupt);
    }
    Ok(())
}

fn uses_key(tenant: &TenantId, id: &[u8; 32]) -> Result<TenantKey, BindingError> {
    let mut object = USES_PREFIX.to_vec();
    object.extend_from_slice(id);
    Ok(TenantKey::new(tenant.clone(), ObjectKind::Capability, object)?)
}

/// Durable per-record use log: (preparation id, core ms), strictly ordered by time then id.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
struct UseLog(Vec<([u8; 32], u64)>);

impl UseLog {
    fn charge(&mut self, preparation_id: [u8; 32], record: &TimedCapability, now_ms: u64) {
        let longest = record.rate_ceilings.keys().max().copied().unwrap_or(0);
        self.0
            .retain(|(_, used)| u128::from(*used) + u128::from(longest) * 1000 > u128::from(now_ms));
        if self.0.iter().all(|(id, _)| *id != preparation_id) {
            self.0.push((preparation_id, now_ms));
            self.0.sort_by(|a, b| (a.1, a.0).cmp(&(b.1, b.0)));
        }
    }

    fn encode(&self) -> Result<Vec<u8>, BindingError> {
        let mut out = vec![VERSION];
        let count = u32::try_from(self.0.len()).map_err(|_| BindingError::SizeOverflow)?;
        out.extend_from_slice(&count.to_be_bytes());
        for (id, at_ms) in &self.0 {
            out.extend_from_slice(id);
            out.extend_from_slice(&at_ms.to_be_bytes());
        }
        Ok(out)
    }

    fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        r.version()?;
        let count = r.u32()?;
        let mut entries: Vec<([u8; 32], u64)> = Vec::new();
        let mut ids = BTreeSet::new();
        for _ in 0..count {
            let id = r.id()?;
            let at_ms = r.u64()?;
            if entries.last().is_some_and(|(last, used)| (*used, *last) >= (at_ms, id))
                || !ids.insert(id)
            {
                return Err(BindingError::Corrupt);
            }
            entries.push((id, at_ms));
        }
        r.finish()?;
        Ok(Self(entries))
    }
}

fn push_u16(out: &mut Vec<u8>, value: usize) -> Result<(), BindingError> {
    let value = u16::try_from(value).map_err(|_| BindingError::SizeOverflow)?;
    out.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

fn push_text(out: &mut Vec<u8>, value: &str) -> Result<(), BindingError> {
    push_u16(out, value.len())?;
    out.extend_from_slice(value.as_bytes());
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], BindingError> {
        let end = self.offset.checked_add(length).ok_or(BindingError::Corrupt)?;
        let value = self.bytes.get(self.offset..end).ok_or(BindingError::Corrupt)?;
        self.offset = end;
        Ok(value)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], BindingError> {
        self.take(N)?.try_into().map_err(|_| BindingError::Corrupt)
    }

    fn version(&mut self) -> Result<(), BindingError> {
        if self.u8()? == VERSION {
            Ok(())
        } else {
            Err(BindingError::Corrupt)
        }
    }

    fn u8(&mut self) -> Result<u8, BindingError> {
        Ok(self.array::<1>()?[0])
    }

    fn u16(&mut self) -> Result<u16, BindingError> {
        Ok(u16::from_be_bytes(self.array()?))
    }

    fn u32(&mut self) -> Result<u32, BindingError> {
        Ok(u32::from_be_bytes(self.array()?))
    }

    fn u64(&mut self) -> Result<u64, BindingError> {
        Ok(u64::from_be_bytes(self.array()?))
    }

    fn id(&mut self) -> Result<[u8; 32], BindingError> {
        self.array()
    }

    fn text(&mut self) -> Result<String, BindingError> {
        let length = usize::from(self.u16()?);
        std::str::from_utf8(self.take(length)?)
            .map(str::to_owned)
            .map_err(|_| BindingError::Corrupt)
    }

    fn finish(&self) -> Result<(), BindingError> {
        if self.offset == self.bytes.len() {
            Ok(())
        } else {
            Err(BindingError::Corrupt)
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::{BTreeMap, BTreeSet};

    use super::*;
    use crate::identity::ProtocolAuthority;

    const AGENT: &str = "did:layerx:agent";
    const ASSET: [u8; 32] = [7; 32];
    const PARTY: [u8; 32] = [8; 32];
    const NOW: u64 = 1_000_000;

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("capability binding: {error:?}"))
    }

    fn open(name: &str) -> (std::path::PathBuf, Store) {
        let root = std::env::temp_dir().join(format!("lxp-cb1-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let store = must(Store::open(&root));
        (root, store)
    }

    fn tenant() -> TenantId {
        must(TenantId::new("tenant-a"))
    }

    fn record(id: u8, parent: Option<u8>, maximum: u64) -> TimedCapability {
        TimedCapability {
            id: [id; 32],
            parent: parent.map(|value| [value; 32]),
            tenant: tenant(),
            agent: AGENT.to_owned(),
            authority: ProtocolAuthority::CapabilityGrant([9; 32]),
            activity_types: BTreeSet::from([5]),
            counterparties: BTreeSet::from([PARTY]),
            assets: BTreeSet::from([ASSET]),
            amount_ceilings: BTreeMap::from([(ASSET, 100)]),
            rate_ceilings: BTreeMap::from([(60, maximum)]),
            purposes: BTreeSet::from(["pay".to_owned()]),
            expiry_seconds: 10_000,
            grant_not_after_ms: 10_000_000,
            created_at_ms: 1,
            created_at_sequence: 1,
            revoked: None,
        }
    }

    fn intent() -> TimedIntent {
        TimedIntent {
            activity_type: 5,
            counterparty: PARTY,
            asset: ASSET,
            amount: 50,
            purpose: "pay".to_owned(),
        }
    }

    fn uses(store: &Store, id: u8) -> usize {
        store
            .get(&must(uses_key(&tenant(), &[id; 32])))
            .map_or(0, |value| must(UseLog::decode(value.bytes())).0.len())
    }

    fn commit(store: &mut Store, preparation: u8, plan: ChargePlan) {
        let mut companions = plan.companions;
        let mut object = b"prepare/record/".to_vec();
        object.extend_from_slice(&[preparation; 32]);
        companions.push((
            must(TenantKey::new(tenant(), ObjectKind::Configuration, object)),
            vec![preparation],
        ));
        must(store.update_local_batch_with_companions(plan.updates, companions));
    }

    fn bound(admission: Admission) -> (CapabilityExtension, ChargePlan) {
        match admission {
            Admission::Bound { extension, plan } => (extension, plan),
            Admission::Legacy => panic!("expected a bound admission"),
        }
    }

    #[test]
    fn legacy_digest_is_exact_and_bound_digest_uses_a_separate_domain() {
        let legacy = [3; 32];
        assert_eq!(prepare_body_digest(legacy, None), legacy);
        let bound = prepare_body_digest(legacy, Some(&[4; 32]));
        assert_ne!(bound, legacy);
        assert_eq!(bound, bound_prepare_digest(&legacy, &[4; 32]));
        assert_ne!(bound, bound_prepare_digest(&legacy, &[5; 32]));
    }

    #[test]
    fn codecs_round_trip_and_refuse_trailing_or_malformed_bytes() {
        let extension = CapabilityExtension {
            capability_id: [1; 32],
            chain: vec![[1; 32], [2; 32]],
            intent_digest: [3; 32],
            admitted_at_ms: 9,
        };
        let bytes = must(extension.encode());
        assert_eq!(must(CapabilityExtension::decode(&bytes)), extension);
        let mut trailing = bytes.clone();
        trailing.push(0);
        assert!(CapabilityExtension::decode(&trailing).is_err());
        let mut version = bytes.clone();
        version[0] = 2;
        assert!(CapabilityExtension::decode(&version).is_err());
        let rooted = CapabilityExtension {
            chain: vec![[2; 32], [1; 32]],
            ..extension.clone()
        };
        assert!(rooted.encode().is_err());
        let repeated = CapabilityExtension {
            chain: vec![[1; 32], [1; 32]],
            ..extension.clone()
        };
        assert!(repeated.encode().is_err());

        let outcome = AdmissionOutcome::admitted([4; 32], Some(&extension));
        let bytes = outcome.encode();
        assert_eq!(must(AdmissionOutcome::decode(&bytes)), outcome);
        assert!(AdmissionOutcome::decode(&bytes[..bytes.len() - 1]).is_err());
        let cancelled = outcome.cancelled(5, 6);
        assert_eq!(must(AdmissionOutcome::decode(&cancelled.encode())), cancelled);
        assert_eq!(cancelled.cancelled(7, 8), cancelled);
        must(outcome.replay(&[4; 32], Some(&[1; 32])));
        assert!(matches!(outcome.replay(&[5; 32], Some(&[1; 32])), Err(BindingError::Conflict)));
        assert!(matches!(outcome.replay(&[4; 32], None), Err(BindingError::Conflict)));
        assert!(matches!(cancelled.replay(&[4; 32], Some(&[1; 32])), Err(BindingError::Cancelled)));

        let log = UseLog(vec![([1; 32], 5), ([0; 32], 6)]);
        assert_eq!(must(UseLog::decode(&must(log.encode()))), log);
        let unordered = UseLog(vec![([1; 32], 6), ([0; 32], 5)]);
        assert!(UseLog::decode(&must(unordered.encode())).is_err());
    }

    #[test]
    fn restriction_is_durable_and_never_removed_by_revocation_or_restart() {
        let (root, mut store) = open("restrict");
        assert_eq!(
            must(admit(&store, &tenant(), AGENT, None, [1; 32], &intent(), NOW)),
            Admission::Legacy
        );
        must(issue(&mut store, record(1, None, 3), NOW, 4));
        assert!(matches!(
            admit(&store, &tenant(), AGENT, None, [1; 32], &intent(), NOW),
            Err(BindingError::Restricted)
        ));
        must(revoke(&mut store, &tenant(), AGENT, &[1; 32], NOW, 5));
        drop(store);
        let store = must(Store::open(&root));
        assert!(matches!(
            admit(&store, &tenant(), AGENT, None, [1; 32], &intent(), NOW),
            Err(BindingError::Restricted)
        ));
        assert!(matches!(
            recheck(&store, &tenant(), AGENT, None, NOW),
            Err(BindingError::Restricted)
        ));
        assert!(matches!(
            admit(&store, &tenant(), AGENT, Some([1; 32]), [1; 32], &intent(), NOW),
            Err(BindingError::Chain(TimedError::ParentInactive))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn chain_is_charged_once_per_preparation_without_siblings() {
        let (root, mut store) = open("charge");
        must(issue(&mut store, record(1, None, 3), NOW, 1));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1));
        must(issue(&mut store, record(3, Some(1), 3), NOW, 1));
        let (extension, plan) =
            bound(must(admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &intent(), NOW)));
        assert_eq!(extension.chain, vec![[2; 32], [1; 32]]);
        commit(&mut store, 10, plan);
        assert_eq!((uses(&store, 1), uses(&store, 2), uses(&store, 3)), (1, 1, 0));
        let (_, plan) =
            bound(must(admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &intent(), NOW)));
        must(store.update_local_batch(plan.updates));
        assert_eq!((uses(&store, 1), uses(&store, 2)), (1, 1));
        must(recheck(&store, &tenant(), AGENT, Some(&extension), NOW));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn every_dimension_and_ancestor_rate_is_enforced() {
        let (root, mut store) = open("dimensions");
        must(issue(&mut store, record(1, None, 1), NOW, 1));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1));
        let wrong = TimedIntent {
            asset: [6; 32],
            ..intent()
        };
        assert!(matches!(
            admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &wrong, NOW),
            Err(BindingError::Refused(Dimension::Asset))
        ));
        let large = TimedIntent {
            amount: 101,
            ..intent()
        };
        assert!(matches!(
            admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &large, NOW),
            Err(BindingError::Refused(Dimension::Amount))
        ));
        let (_, plan) =
            bound(must(admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &intent(), NOW)));
        commit(&mut store, 10, plan);
        assert!(matches!(
            admit(&store, &tenant(), AGENT, Some([2; 32]), [11; 32], &intent(), NOW + 1),
            Err(BindingError::Refused(Dimension::Rate))
        ));
        must(admit(&store, &tenant(), AGENT, Some([2; 32]), [11; 32], &intent(), NOW + 60_000));
        assert!(matches!(
            admit(&store, &tenant(), AGENT, Some([2; 32]), [11; 32], &intent(), 10_000_000),
            Err(BindingError::Chain(TimedError::ParentInactive))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn revocation_records_recoverable_cleanup_and_invalidates_bound_preparations() {
        let (root, mut store) = open("revoke");
        must(issue(&mut store, record(1, None, 3), NOW, 1));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1));
        let (extension, plan) =
            bound(must(admit(&store, &tenant(), AGENT, Some([2; 32]), [10; 32], &intent(), NOW)));
        commit(&mut store, 10, plan);
        let (_, revoked) = must(revoke(&mut store, &tenant(), AGENT, &[1; 32], NOW + 1, 7));
        assert_eq!(revoked.len(), 2);
        let pending = must(pending_cleanups(&store, &tenant()));
        assert_eq!(pending.len(), 1);
        let ids = must(revoked_ids(&store, &tenant(), AGENT));
        assert!(is_invalidated(&extension, &ids));
        assert!(recheck(&store, &tenant(), AGENT, Some(&extension), NOW + 2).is_err());
        assert!(must(complete_cleanup(&mut store, &pending[0])));
        assert!(must(pending_cleanups(&store, &tenant())).is_empty());
        assert!(matches!(
            revoke(&mut store, &tenant(), "did:layerx:other", &[1; 32], NOW, 8),
            Err(BindingError::Chain(TimedError::NotFound))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }
}
