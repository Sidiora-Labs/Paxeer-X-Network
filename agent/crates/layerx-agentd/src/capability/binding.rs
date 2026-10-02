//! Durable binding of owner-issued timed capabilities to preparations.

use std::collections::{BTreeMap, BTreeSet};

use layerx_agent_api::identity::{ExplicitSet, Purpose};
use layerx_crypto::disclosure::{DisclosedNativeOperation, Disclosure};
use layerx_crypto::payments::Payment;
use layerx_crypto::purpose::purpose_commitment_v1;
use layerx_types::payload::ActivityType;
use sha2::{Digest, Sha256};

use crate::human::HumanOperationError;
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

use super::timed::{self, TimedCapability, TimedError, TimedState};
use super::{AuthorizationKind, Decision, Dimension, Effect, SemanticPlan};

/// Separate domain for a capability-bound prepare body; the legacy prepare digest is unchanged.
pub const PREPARE_CAPABILITY_DOMAIN: &[u8] = b"layerx-human-journey-prepare-capability/v1\0";
const CHAIN_DOMAIN: &[u8] = b"layerx-capability-chain/v1\0";
const INTENT_DOMAIN: &[u8] = b"layerx-capability-intent/v1\0";
const BINDING_PREFIX: &[u8] = b"timed-binding-v1:";
const USES_PREFIX: &[u8] = b"timed-uses-v1:";
const CLEANUP_PREFIX: &[u8] = b"timed-cleanup-v1:";
const SCOPE_PREFIX: &[u8] = b"timed-scope-v1:";
const PREPARATION_PREFIX: &[u8] = b"timed-prep-v1:";
const PURPOSE_COMMITMENT: u8 = 1;
const PURPOSE_TEXT_V1: u8 = 2;
const LXGS2_LENGTH: usize = 209;
const LXGS2_MODULE_MASK: usize = 173;
const VERSION: u8 = 1;
const PREPARATION_TEXT_VERSION: u8 = 2;
const MAX_CHAIN: usize = 64;

/// Binding, admission, decoding or storage failure.
#[derive(Debug)]
pub enum BindingError {
    Restricted,
    Unbound,
    Cancelled,
    Refused(Dimension),
    PurposeCommitmentMissing,
    ModuleScopeUnproven,
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
            Self::PurposeCommitmentMissing => {
                HumanOperationError::CapabilityRefused(Dimension::Purpose)
            }
            Self::ModuleScopeUnproven => {
                HumanOperationError::CapabilityRefused(Dimension::ActivityType)
            }
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
pub struct PlanIntent {
    activity: u32,
    effects: Vec<Effect>,
    gross: BTreeMap<[u8; 32], u128>,
    purpose: PurposeBinding,
}

/// Purpose dimension bound to an activity commitment; each variant has its own codec tag.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PurposeBinding {
    /// An explicit 32-byte reference supplied as bytes, never derived from text (tag 1).
    Commitment([u8; 32]),
    /// A textual label, of any content, whose v1 purpose commitment equals the commitment (tag 2).
    TextV1 { commitment: [u8; 32] },
}

impl PurposeBinding {
    const fn parts(self) -> (u8, [u8; 32]) {
        match self {
            Self::Commitment(value) => (PURPOSE_COMMITMENT, value),
            Self::TextV1 { commitment } => (PURPOSE_TEXT_V1, commitment),
        }
    }

    const fn commitment(self) -> [u8; 32] {
        self.parts().1
    }

    /// Preparation record version for this variant; `Commitment` keeps the version-1 encoding.
    const fn preparation_version(self) -> u8 {
        match self {
            Self::Commitment(_) => VERSION,
            Self::TextV1 { .. } => PREPARATION_TEXT_VERSION,
        }
    }

    fn encode(self, out: &mut Vec<u8>) {
        let (tag, value) = self.parts();
        out.push(tag);
        out.extend_from_slice(&value);
    }

    fn decode(r: &mut Reader<'_>) -> Result<Self, BindingError> {
        match r.u8()? {
            PURPOSE_COMMITMENT => Ok(Self::Commitment(r.array()?)),
            PURPOSE_TEXT_V1 => Ok(Self::TextV1 {
                commitment: r.array()?,
            }),
            _ => Err(BindingError::Corrupt),
        }
    }
}

#[must_use]
pub fn purpose_commitment(disclosure: &Disclosure) -> Option<[u8; 32]> {
    if let Some(DisclosedNativeOperation::BudgetCreate(create)) = &disclosure.native_operation {
        return Some(create.purpose);
    }
    match &disclosure.payment {
        Some(Payment::IssueGrant(grant)) => Some(grant.purpose_hash),
        Some(Payment::Receive { payer_grant, .. }) => Some(payer_grant.purpose_hash),
        _ => None,
    }
}

/// Binds one asserted purpose to the activity's commitment with the rules of [`purpose_from_commitment`].
///
/// # Errors
/// Returns `PurposeCommitmentMissing` for an activity without a commitment and `Refused(Purpose)`
/// when the asserted purpose does not match it.
pub fn bind_purpose(
    asserted: &str,
    disclosure: &Disclosure,
) -> Result<PurposeBinding, BindingError> {
    purpose_from_commitment(asserted, purpose_commitment(disclosure))
}

/// Binds one asserted purpose text to a commitment with the label rule of [`purpose_from_set`].
///
/// - An absent commitment returns `PurposeCommitmentMissing`.
/// - Every text, 64-character hex included, is a textual label: it matches only when
///   `purpose_commitment_v1(text)` equals the commitment and yields `TextV1`; text is never
///   interpreted as a 32-byte reference, so this never yields `Commitment`.
/// - A mismatch or an empty text refuses on the Purpose dimension.
///
/// # Errors
/// Returns `PurposeCommitmentMissing` for an absent commitment and `Refused(Purpose)` for a
/// mismatch or an empty text.
pub fn purpose_from_commitment(
    asserted: &str,
    commitment: Option<[u8; 32]>,
) -> Result<PurposeBinding, BindingError> {
    let commitment = commitment.ok_or(BindingError::PurposeCommitmentMissing)?;
    member_binding(asserted, commitment).ok_or(BindingError::Refused(Dimension::Purpose))
}

/// Binds the purpose set to the activity's commitment with the rules of [`purpose_from_set`].
///
/// # Errors
/// Returns `Refused(Purpose)` for an empty set or when no member matches the activity commitment,
/// and `PurposeCommitmentMissing` when the activity carries no commitment.
pub fn bind_purpose_from_set(
    purposes: &ExplicitSet<Purpose>,
    disclosure: &Disclosure,
) -> Result<PurposeBinding, BindingError> {
    purpose_from_set(purposes.values(), purpose_commitment(disclosure))
}

/// Binds a purpose set to a commitment; the first matching member in set order wins.
///
/// - An empty set refuses on the Purpose dimension.
/// - An absent commitment returns `PurposeCommitmentMissing`.
/// - Every member, 64-character hex included, is a textual label: it matches only when
///   `purpose_commitment_v1(member)` equals the commitment and yields `TextV1`; a member is never
///   interpreted as a 32-byte reference, so this never yields `Commitment`.
/// - A present commitment that no member matches refuses on the Purpose dimension.
///
/// # Errors
/// Returns `Refused(Purpose)` for an empty set or an unmatched commitment and
/// `PurposeCommitmentMissing` for an absent commitment.
pub fn purpose_from_set(
    purposes: &[Purpose],
    commitment: Option<[u8; 32]>,
) -> Result<PurposeBinding, BindingError> {
    if purposes.is_empty() {
        return Err(BindingError::Refused(Dimension::Purpose));
    }
    let commitment = commitment.ok_or(BindingError::PurposeCommitmentMissing)?;
    purposes
        .iter()
        .find_map(|purpose| member_binding(purpose.as_str(), commitment))
        .ok_or(BindingError::Refused(Dimension::Purpose))
}

/// `TextV1` binding of one purpose text when its v1 commitment equals the commitment.
fn member_binding(member: &str, commitment: [u8; 32]) -> Option<PurposeBinding> {
    purpose_commitment_v1(member)
        .is_ok_and(|value| value == commitment)
        .then_some(PurposeBinding::TextV1 { commitment })
}

/// # Errors
/// Returns `ModuleScopeUnproven` for a summary that is not a 209-byte LXGS2 record or carries no module.
pub fn lxgs2_module_mask(summary: &[u8]) -> Result<u64, BindingError> {
    if summary.len() != LXGS2_LENGTH || summary[..5] != *b"LXGS2" {
        return Err(BindingError::ModuleScopeUnproven);
    }
    let mut bytes = [0_u8; 8];
    bytes.copy_from_slice(&summary[LXGS2_MODULE_MASK..LXGS2_MODULE_MASK + 8]);
    match u64::from_be_bytes(bytes) {
        0 => Err(BindingError::ModuleScopeUnproven),
        mask => Ok(mask),
    }
}

fn scope_key(tenant: &TenantId, capability_id: &[u8; 32]) -> Result<TenantKey, BindingError> {
    let mut object = SCOPE_PREFIX.to_vec();
    object.extend_from_slice(capability_id);
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
}

/// # Errors
/// Returns `Corrupt` for an undecodable or zero stored module scope.
pub fn module_scope(
    store: &Store,
    tenant: &TenantId,
    capability_id: &[u8; 32],
) -> Result<Option<u64>, BindingError> {
    let Some(value) = store.get(&scope_key(tenant, capability_id)?) else {
        return Ok(None);
    };
    let mut r = Reader::new(value.bytes());
    r.version()?;
    let mask = r.u64()?;
    r.finish()?;
    if mask == 0 {
        return Err(BindingError::Corrupt);
    }
    Ok(Some(mask))
}

fn module_admits(mask: u64, activity: u32) -> bool {
    ActivityType::from_u32(activity).is_ok_and(|activity| {
        1_u64
            .checked_shl(activity.value() >> 16)
            .is_some_and(|bit| mask & bit != 0)
    })
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PreparationBinding {
    pub capability_id: [u8; 32],
    pub activity: u32,
    pub disclosure_digest: [u8; 32],
    pub purpose: PurposeBinding,
}

impl PreparationBinding {
    fn encode(&self) -> Vec<u8> {
        let mut out = vec![self.purpose.preparation_version()];
        out.extend_from_slice(&self.capability_id);
        out.extend_from_slice(&self.activity.to_be_bytes());
        out.extend_from_slice(&self.disclosure_digest);
        self.purpose.encode(&mut out);
        out
    }

    /// Version 1 carries only `Commitment`; version 2 carries only `TextV1`; anything else is `Corrupt`.
    fn decode(bytes: &[u8]) -> Result<Self, BindingError> {
        let mut r = Reader::new(bytes);
        let version = r.u8()?;
        let capability_id = r.array()?;
        let activity = r.u32()?;
        let disclosure_digest = r.array()?;
        let purpose = PurposeBinding::decode(&mut r)?;
        r.finish()?;
        if version != purpose.preparation_version() {
            return Err(BindingError::Corrupt);
        }
        Ok(Self {
            capability_id,
            activity,
            disclosure_digest,
            purpose,
        })
    }
}

fn preparation_key(
    tenant: &TenantId,
    preparation_id: &[u8; 32],
) -> Result<TenantKey, BindingError> {
    let mut object = PREPARATION_PREFIX.to_vec();
    object.extend_from_slice(preparation_id);
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
}

/// # Errors
/// Returns `Corrupt` for an undecodable stored preparation binding.
pub fn preparation_binding(
    store: &Store,
    tenant: &TenantId,
    preparation_id: &[u8; 32],
) -> Result<Option<PreparationBinding>, BindingError> {
    store
        .get(&preparation_key(tenant, preparation_id)?)
        .map(|value| PreparationBinding::decode(value.bytes()))
        .transpose()
}

/// # Errors
/// Returns `Unbound` without a durable binding and `Conflict` when the capability id or digest differs.
pub fn recheck_on_submit(
    store: &Store,
    tenant: &TenantId,
    preparation_id: &[u8; 32],
    digest: &[u8; 32],
    capability_id: &[u8; 32],
) -> Result<(), BindingError> {
    let binding =
        preparation_binding(store, tenant, preparation_id)?.ok_or(BindingError::Unbound)?;
    if binding.capability_id != *capability_id || binding.disclosure_digest != *digest {
        return Err(BindingError::Conflict);
    }
    Ok(())
}

impl PlanIntent {
    #[must_use]
    pub fn new(activity: u32, plan: &SemanticPlan, purpose: PurposeBinding) -> Self {
        Self {
            activity,
            effects: plan.effects().to_vec(),
            gross: plan.gross_per_asset().clone(),
            purpose,
        }
    }

    #[must_use]
    pub const fn activity(&self) -> u32 {
        self.activity
    }

    #[must_use]
    pub const fn purpose(&self) -> PurposeBinding {
        self.purpose
    }

    /// Canonical digest of the evaluated intent.
    #[must_use]
    pub fn digest(&self) -> [u8; 32] {
        let mut digest = Sha256::new();
        digest.update(INTENT_DOMAIN);
        digest.update(self.activity.to_be_bytes());
        for effect in &self.effects {
            let (tag, kind, from, account, asset, amount) = match *effect {
                Effect::Transfer {
                    from,
                    to,
                    asset,
                    amount,
                } => (1_u8, 0_u8, from, to, Some(asset), amount),
                Effect::Issuance {
                    account,
                    asset,
                    amount,
                } => (2, 0, [0; 32], account, Some(asset), amount),
                Effect::Destruction {
                    account,
                    asset,
                    amount,
                } => (3, 0, [0; 32], account, Some(asset), amount),
                Effect::Authorization {
                    kind,
                    account,
                    asset,
                    amount,
                } => (
                    4,
                    match kind {
                        AuthorizationKind::SpendingLimit => 1,
                        AuthorizationKind::SupplyCap => 2,
                        AuthorizationKind::PerDrawMaximum => 3,
                        AuthorizationKind::GrantAllowance => 4,
                    },
                    [0; 32],
                    account,
                    asset,
                    amount,
                ),
            };
            digest.update([tag, kind]);
            digest.update(from);
            digest.update(account);
            match asset {
                Some(asset) => {
                    digest.update([1]);
                    digest.update(asset);
                }
                None => digest.update([0]),
            }
            digest.update(amount.to_be_bytes());
        }
        digest.update([0xff]);
        for (asset, amount) in &self.gross {
            digest.update(asset);
            digest.update(amount.to_be_bytes());
        }
        let mut purpose = Vec::new();
        self.purpose.encode(&mut purpose);
        digest.update(purpose);
        digest.finalize().into()
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
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
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
    module_mask: u64,
) -> Result<timed::Insert, BindingError> {
    let tenant = record.tenant.clone();
    let agent = record.agent.clone();
    if module_mask == 0 {
        return Err(BindingError::ModuleScopeUnproven);
    }
    if let Some(parent) = record.parent {
        let parent_mask =
            module_scope(store, &tenant, &parent)?.ok_or(BindingError::ModuleScopeUnproven)?;
        if module_mask & !parent_mask != 0 {
            return Err(BindingError::Refused(Dimension::ActivityType));
        }
    }
    restrict(store, &tenant, &agent, at_ms, at_sequence)?;
    match module_scope(store, &tenant, &record.id)? {
        Some(existing) if existing != module_mask => return Err(BindingError::Conflict),
        Some(_) => {}
        None => {
            let mut bytes = vec![VERSION];
            bytes.extend_from_slice(&module_mask.to_be_bytes());
            store.put_local(scope_key(&tenant, &record.id)?, bytes)?;
        }
    }
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
    intent: &PlanIntent,
    disclosure_digest: [u8; 32],
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
        let mask =
            module_scope(store, tenant, &record.id)?.ok_or(BindingError::ModuleScopeUnproven)?;
        let key = uses_key(tenant, &record.id)?;
        let log = match store.get(&key) {
            Some(value) => Some(UseLog::decode(value.bytes())?),
            None => None,
        };
        if let Decision::Refuse(dimension) = evaluate(
            record,
            mask,
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
    let binding = PreparationBinding {
        capability_id,
        activity: intent.activity,
        disclosure_digest,
        purpose: intent.purpose,
    }
    .encode();
    let key = preparation_key(tenant, &preparation_id)?;
    match store.get(&key) {
        Some(existing) if existing.bytes() != binding.as_slice() => {
            return Err(BindingError::Conflict)
        }
        Some(_) => plan.updates.push((key, binding)),
        None => plan.companions.push((key, binding)),
    }
    Ok(Admission::Bound {
        extension: CapabilityExtension {
            capability_id,
            chain: chain.iter().map(|record| record.id).collect(),
            intent_digest: intent.digest(),
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
    for record in &chain {
        module_scope(store, tenant, &record.id)?.ok_or(BindingError::ModuleScopeUnproven)?;
    }
    Ok(())
}

/// Evaluates one record of the chain in the stable dimension order; an empty set denies.
#[must_use]
pub fn evaluate(
    record: &TimedCapability,
    module_mask: u64,
    intent: &PlanIntent,
    uses: &[([u8; 32], u64)],
    preparation_id: &[u8; 32],
    now_ms: u64,
) -> Decision {
    if record.is_expired(now_ms) {
        return Decision::Refuse(Dimension::Expiry);
    }
    if !module_admits(module_mask, intent.activity) {
        return Decision::Refuse(Dimension::ActivityType);
    }
    if !ActivityType::from_u32(intent.activity)
        .is_ok_and(|activity| record.activity_types.contains(&activity.ordinal()))
    {
        return Decision::Refuse(Dimension::ActivityType);
    }
    for effect in &intent.effects {
        let (account, asset) = match *effect {
            Effect::Transfer { to, asset, .. } => (to, Some(asset)),
            Effect::Issuance { account, asset, .. }
            | Effect::Destruction { account, asset, .. } => (account, Some(asset)),
            Effect::Authorization { account, asset, .. } => (account, asset),
        };
        if !record.counterparties.contains(&account) {
            return Decision::Refuse(Dimension::Counterparty);
        }
        if asset.is_some_and(|asset| !record.assets.contains(&asset)) {
            return Decision::Refuse(Dimension::Asset);
        }
    }
    if intent.gross.iter().any(|(asset, amount)| {
        record
            .amount_ceilings
            .get(asset)
            .is_none_or(|ceiling| amount > ceiling)
    }) {
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
    let commitment = intent.purpose.commitment();
    if !record
        .purposes
        .iter()
        .any(|purpose| member_binding(purpose, commitment) == Some(intent.purpose))
    {
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
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
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
    if chain.first() != Some(&capability_id)
        || chain.len() > MAX_CHAIN
        || unique.len() != chain.len()
    {
        return Err(BindingError::Corrupt);
    }
    Ok(())
}

fn uses_key(tenant: &TenantId, id: &[u8; 32]) -> Result<TenantKey, BindingError> {
    let mut object = USES_PREFIX.to_vec();
    object.extend_from_slice(id);
    Ok(TenantKey::new(
        tenant.clone(),
        ObjectKind::Capability,
        object,
    )?)
}

/// Durable per-record use log: (preparation id, core ms), strictly ordered by time then id.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
struct UseLog(Vec<([u8; 32], u64)>);

impl UseLog {
    fn charge(&mut self, preparation_id: [u8; 32], record: &TimedCapability, now_ms: u64) {
        let longest = record.rate_ceilings.keys().max().copied().unwrap_or(0);
        self.0.retain(|(_, used)| {
            u128::from(*used) + u128::from(longest) * 1000 > u128::from(now_ms)
        });
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
            if entries
                .last()
                .is_some_and(|(last, used)| (*used, *last) >= (at_ms, id))
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
        let end = self
            .offset
            .checked_add(length)
            .ok_or(BindingError::Corrupt)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(BindingError::Corrupt)?;
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

    use layerx_types::payload::ModuleId;

    use super::*;
    use crate::identity::ProtocolAuthority;

    const AGENT: &str = "did:layerx:agent";
    const ASSET: [u8; 32] = [7; 32];
    const PARTY: [u8; 32] = [8; 32];
    const NOW: u64 = 1_000_000;
    const PURPOSE: [u8; 32] = [6; 32];
    const DIGEST: [u8; 32] = [12; 32];
    const ASSET_MASK: u64 = 1 << 1;

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
            purposes: BTreeSet::from([hex(&PURPOSE)]),
            expiry_seconds: 10_000,
            grant_not_after_ms: 10_000_000,
            created_at_ms: 1,
            created_at_sequence: 1,
            revoked: None,
        }
    }

    fn plan(effects: Vec<Effect>) -> PlanIntent {
        let mut gross = BTreeMap::new();
        for effect in &effects {
            if let Effect::Transfer { asset, amount, .. }
            | Effect::Issuance { asset, amount, .. }
            | Effect::Destruction { asset, amount, .. } = *effect
            {
                let total: &mut u128 = gross.entry(asset).or_default();
                *total = must(total.checked_add(amount).ok_or("gross overflow"));
            }
        }
        PlanIntent {
            activity: activity(ModuleId::Asset),
            effects,
            gross,
            purpose: PurposeBinding::TextV1 {
                commitment: must(purpose_commitment_v1(&hex(&PURPOSE))),
            },
        }
    }

    fn transfer(asset: [u8; 32], amount: u128) -> Effect {
        Effect::Transfer {
            from: [1; 32],
            to: PARTY,
            asset,
            amount,
        }
    }

    fn intent() -> PlanIntent {
        plan(vec![transfer(ASSET, 50)])
    }

    fn activity(module: ModuleId) -> u32 {
        must(ActivityType::new(module, 5)).value()
    }

    fn hex(bytes: &[u8]) -> String {
        bytes.iter().map(|byte| format!("{byte:02x}")).collect()
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
        assert_eq!(
            must(AdmissionOutcome::decode(&cancelled.encode())),
            cancelled
        );
        assert_eq!(cancelled.cancelled(7, 8), cancelled);
        must(outcome.replay(&[4; 32], Some(&[1; 32])));
        assert!(matches!(
            outcome.replay(&[5; 32], Some(&[1; 32])),
            Err(BindingError::Conflict)
        ));
        assert!(matches!(
            outcome.replay(&[4; 32], None),
            Err(BindingError::Conflict)
        ));
        assert!(matches!(
            cancelled.replay(&[4; 32], Some(&[1; 32])),
            Err(BindingError::Cancelled)
        ));

        let log = UseLog(vec![([1; 32], 5), ([0; 32], 6)]);
        assert_eq!(must(UseLog::decode(&must(log.encode()))), log);
        let unordered = UseLog(vec![([1; 32], 6), ([0; 32], 5)]);
        assert!(UseLog::decode(&must(unordered.encode())).is_err());
    }

    #[test]
    fn restriction_is_durable_and_never_removed_by_revocation_or_restart() {
        let (root, mut store) = open("restrict");
        assert_eq!(
            must(admit(
                &store,
                &tenant(),
                AGENT,
                None,
                [1; 32],
                &intent(),
                DIGEST,
                NOW
            )),
            Admission::Legacy
        );
        must(issue(&mut store, record(1, None, 3), NOW, 4, ASSET_MASK));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                None,
                [1; 32],
                &intent(),
                DIGEST,
                NOW
            ),
            Err(BindingError::Restricted)
        ));
        must(revoke(&mut store, &tenant(), AGENT, &[1; 32], NOW, 5));
        drop(store);
        let store = must(Store::open(&root));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                None,
                [1; 32],
                &intent(),
                DIGEST,
                NOW
            ),
            Err(BindingError::Restricted)
        ));
        assert!(matches!(
            recheck(&store, &tenant(), AGENT, None, NOW),
            Err(BindingError::Restricted)
        ));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [1; 32],
                &intent(),
                DIGEST,
                NOW
            ),
            Err(BindingError::Chain(TimedError::ParentInactive))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn chain_is_charged_once_per_preparation_without_siblings() {
        let (root, mut store) = open("charge");
        must(issue(&mut store, record(1, None, 3), NOW, 1, ASSET_MASK));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1, ASSET_MASK));
        must(issue(&mut store, record(3, Some(1), 3), NOW, 1, ASSET_MASK));
        let (extension, plan) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([2; 32]),
            [10; 32],
            &intent(),
            DIGEST,
            NOW,
        )));
        assert_eq!(extension.chain, vec![[2; 32], [1; 32]]);
        commit(&mut store, 10, plan);
        assert_eq!(
            (uses(&store, 1), uses(&store, 2), uses(&store, 3)),
            (1, 1, 0)
        );
        let (_, plan) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([2; 32]),
            [10; 32],
            &intent(),
            DIGEST,
            NOW,
        )));
        must(store.update_local_batch(plan.updates));
        assert_eq!((uses(&store, 1), uses(&store, 2)), (1, 1));
        must(recheck(&store, &tenant(), AGENT, Some(&extension), NOW));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn every_dimension_and_ancestor_rate_is_enforced() {
        let (root, mut store) = open("dimensions");
        must(issue(&mut store, record(1, None, 1), NOW, 1, ASSET_MASK));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1, ASSET_MASK));
        let wrong = plan(vec![transfer([6; 32], 50)]);
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([2; 32]),
                [10; 32],
                &wrong,
                DIGEST,
                NOW
            ),
            Err(BindingError::Refused(Dimension::Asset))
        ));
        let large = plan(vec![transfer(ASSET, 101)]);
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([2; 32]),
                [10; 32],
                &large,
                DIGEST,
                NOW
            ),
            Err(BindingError::Refused(Dimension::Amount))
        ));
        let (_, plan) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([2; 32]),
            [10; 32],
            &intent(),
            DIGEST,
            NOW,
        )));
        commit(&mut store, 10, plan);
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([2; 32]),
                [11; 32],
                &intent(),
                DIGEST,
                NOW + 1
            ),
            Err(BindingError::Refused(Dimension::Rate))
        ));
        must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([2; 32]),
            [11; 32],
            &intent(),
            DIGEST,
            NOW + 60_000,
        ));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([2; 32]),
                [11; 32],
                &intent(),
                DIGEST,
                10_000_000
            ),
            Err(BindingError::Chain(TimedError::ParentInactive))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn revocation_records_recoverable_cleanup_and_invalidates_bound_preparations() {
        let (root, mut store) = open("revoke");
        must(issue(&mut store, record(1, None, 3), NOW, 1, ASSET_MASK));
        must(issue(&mut store, record(2, Some(1), 3), NOW, 1, ASSET_MASK));
        let (extension, plan) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([2; 32]),
            [10; 32],
            &intent(),
            DIGEST,
            NOW,
        )));
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

    #[test]
    fn empty_purpose_set_denies_every_purpose() {
        let (root, mut store) = open("purpose-empty");
        let mut empty = record(1, None, 3);
        empty.purposes.clear();
        must(issue(&mut store, empty, NOW, 1, ASSET_MASK));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [10; 32],
                &intent(),
                DIGEST,
                NOW
            ),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn hex_text_is_never_accepted_as_a_literal_reference() {
        assert!(matches!(
            purpose_from_commitment(&hex(&PURPOSE), Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        assert!(matches!(
            purpose_from_commitment(&hex(&[5; 32]), Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        assert!(matches!(
            purpose_from_commitment("", Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
    }

    #[test]
    fn dry_run_textual_label_is_admitted_through_the_v1_producer() {
        let commitment = must(purpose_commitment_v1("rent"));
        assert_eq!(
            must(purpose_from_commitment("rent", Some(commitment))),
            PurposeBinding::TextV1 { commitment }
        );
        assert!(matches!(
            purpose_from_commitment("rent", Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
    }

    #[test]
    fn dry_run_lowercase_hex_text_is_hashed_like_any_label() {
        let text = hex(&PURPOSE);
        let commitment = must(purpose_commitment_v1(&text));
        assert_eq!(
            must(purpose_from_commitment(&text, Some(commitment))),
            PurposeBinding::TextV1 { commitment }
        );
        assert!(matches!(
            purpose_from_commitment(&text, Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
    }

    #[test]
    fn dry_run_uppercase_hex_is_a_label() {
        let upper = hex(&PURPOSE).to_uppercase();
        assert!(matches!(
            purpose_from_commitment(&upper, Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        let commitment = must(purpose_commitment_v1(&upper));
        assert_eq!(
            must(purpose_from_commitment(&upper, Some(commitment))),
            PurposeBinding::TextV1 { commitment }
        );
    }

    #[test]
    fn dry_run_missing_commitment_is_reported() {
        assert!(matches!(
            purpose_from_commitment("rent", None),
            Err(BindingError::PurposeCommitmentMissing)
        ));
        assert!(matches!(
            purpose_from_commitment(&hex(&PURPOSE), None),
            Err(BindingError::PurposeCommitmentMissing)
        ));
    }

    #[test]
    fn submit_recheck_refuses_a_changed_binding() {
        let (root, mut store) = open("submit");
        must(issue(&mut store, record(1, None, 3), NOW, 1, ASSET_MASK));
        let (_, plan) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([1; 32]),
            [10; 32],
            &intent(),
            DIGEST,
            NOW,
        )));
        commit(&mut store, 10, plan);
        must(recheck_on_submit(
            &store,
            &tenant(),
            &[10; 32],
            &DIGEST,
            &[1; 32],
        ));
        assert!(matches!(
            recheck_on_submit(&store, &tenant(), &[10; 32], &[13; 32], &[1; 32]),
            Err(BindingError::Conflict)
        ));
        assert!(matches!(
            recheck_on_submit(&store, &tenant(), &[10; 32], &DIGEST, &[2; 32]),
            Err(BindingError::Conflict)
        ));
        assert!(matches!(
            recheck_on_submit(&store, &tenant(), &[11; 32], &DIGEST, &[1; 32]),
            Err(BindingError::Unbound)
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn record_without_proven_module_scope_refuses() {
        let (root, mut store) = open("scope");
        must(restrict(&mut store, &tenant(), AGENT, NOW, 1));
        must(timed::insert(&mut store, record(1, None, 3)));
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [10; 32],
                &intent(),
                DIGEST,
                NOW
            ),
            Err(BindingError::ModuleScopeUnproven)
        ));
        assert!(matches!(
            issue(&mut store, record(2, Some(1), 3), NOW, 1, ASSET_MASK),
            Err(BindingError::ModuleScopeUnproven)
        ));
        assert!(matches!(
            issue(&mut store, record(3, None, 3), NOW, 1, 0),
            Err(BindingError::ModuleScopeUnproven)
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn full_activity_identity_is_checked_before_the_ordinal() {
        let (root, mut store) = open("identity");
        must(issue(&mut store, record(1, None, 3), NOW, 1, ASSET_MASK));
        let escrow = PlanIntent {
            activity: activity(ModuleId::Escrow),
            ..intent()
        };
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [10; 32],
                &escrow,
                DIGEST,
                NOW
            ),
            Err(BindingError::Refused(Dimension::ActivityType))
        ));
        assert!(matches!(
            issue(
                &mut store,
                record(2, Some(1), 3),
                NOW,
                1,
                ASSET_MASK | (1 << 2)
            ),
            Err(BindingError::Refused(Dimension::ActivityType))
        ));
        assert_eq!(
            evaluate(
                &record(1, None, 3),
                ASSET_MASK,
                &intent(),
                &[],
                &[10; 32],
                NOW
            ),
            Decision::Allow
        );
        assert_eq!(
            evaluate(&record(1, None, 3), 1 << 2, &intent(), &[], &[10; 32], NOW),
            Decision::Refuse(Dimension::ActivityType)
        );
        assert_ne!(intent().digest(), escrow.digest());
        let mut summary = vec![0_u8; 209];
        summary[..5].copy_from_slice(b"LXGS2");
        summary[173..181].copy_from_slice(&ASSET_MASK.to_be_bytes());
        assert_eq!(must(lxgs2_module_mask(&summary)), ASSET_MASK);
        summary[173..181].copy_from_slice(&0_u64.to_be_bytes());
        assert!(matches!(
            lxgs2_module_mask(&summary),
            Err(BindingError::ModuleScopeUnproven)
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn zero_effect_write_is_admitted_and_rate_counted_once() {
        let (root, mut store) = open("zero-effect");
        must(issue(&mut store, record(1, None, 1), NOW, 1, ASSET_MASK));
        let (_, charge) = bound(must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([1; 32]),
            [10; 32],
            &plan(Vec::new()),
            DIGEST,
            NOW,
        )));
        commit(&mut store, 10, charge);
        assert_eq!(uses(&store, 1), 1);
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [11; 32],
                &plan(Vec::new()),
                DIGEST,
                NOW + 1
            ),
            Err(BindingError::Refused(Dimension::Rate))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn two_leg_plan_sum_over_one_asset_ceiling_is_refused() {
        let one_leg = plan(vec![transfer(ASSET, 60)]);
        let two_legs = plan(vec![transfer(ASSET, 60), transfer(ASSET, 60)]);
        assert_eq!(
            evaluate(
                &record(1, None, 3),
                ASSET_MASK,
                &one_leg,
                &[],
                &[10; 32],
                NOW
            ),
            Decision::Allow
        );
        assert_eq!(
            evaluate(
                &record(1, None, 3),
                ASSET_MASK,
                &two_legs,
                &[],
                &[10; 32],
                NOW
            ),
            Decision::Refuse(Dimension::Amount)
        );
    }

    #[test]
    fn receive_plan_with_authorizations_is_admitted() {
        let (root, mut store) = open("receive");
        must(issue(&mut store, record(1, None, 3), NOW, 1, ASSET_MASK));
        let receive = plan(vec![
            transfer(ASSET, 40),
            Effect::Authorization {
                kind: AuthorizationKind::PerDrawMaximum,
                account: PARTY,
                asset: Some(ASSET),
                amount: 50,
            },
            Effect::Authorization {
                kind: AuthorizationKind::GrantAllowance,
                account: PARTY,
                asset: None,
                amount: 500,
            },
        ]);
        must(admit(
            &store,
            &tenant(),
            AGENT,
            Some([1; 32]),
            [10; 32],
            &receive,
            DIGEST,
            NOW,
        ));
        let stranger = plan(vec![Effect::Authorization {
            kind: AuthorizationKind::GrantAllowance,
            account: [9; 32],
            asset: None,
            amount: 1,
        }]);
        assert!(matches!(
            admit(
                &store,
                &tenant(),
                AGENT,
                Some([1; 32]),
                [10; 32],
                &stranger,
                DIGEST,
                NOW
            ),
            Err(BindingError::Refused(Dimension::Counterparty))
        ));
        let _ = std::fs::remove_dir_all(&root);
    }

    #[test]
    fn purpose_from_set_hashes_hex_members_and_refuses_unmatched_labels() {
        let text = hex(&PURPOSE);
        let commitment = must(purpose_commitment_v1(&text));
        let member = must(Purpose::new(text.as_str()));
        let label = must(Purpose::new("rent"));
        assert_eq!(
            must(purpose_from_set(&[label.clone(), member], Some(commitment))),
            PurposeBinding::TextV1 { commitment }
        );
        assert!(matches!(
            purpose_from_set(&[label.clone()], Some(commitment)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        assert!(matches!(
            purpose_from_set(&[label], None),
            Err(BindingError::PurposeCommitmentMissing)
        ));
        assert!(matches!(
            purpose_from_set(&[], Some(commitment)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
    }

    #[test]
    fn textual_label_is_admitted_through_the_v1_producer() {
        let commitment = must(purpose_commitment_v1("rent"));
        let binding = must(purpose_from_set(
            &[must(Purpose::new("food")), must(Purpose::new("rent"))],
            Some(commitment),
        ));
        assert_eq!(binding, PurposeBinding::TextV1 { commitment });
        let mut labelled = record(1, None, 3);
        labelled.purposes = BTreeSet::from(["rent".to_owned()]);
        let text_intent = PlanIntent {
            purpose: binding,
            ..intent()
        };
        assert_eq!(
            evaluate(&labelled, ASSET_MASK, &text_intent, &[], &[10; 32], NOW),
            Decision::Allow
        );
        let legacy_intent = PlanIntent {
            purpose: PurposeBinding::Commitment(commitment),
            ..intent()
        };
        assert_eq!(
            evaluate(&labelled, ASSET_MASK, &legacy_intent, &[], &[10; 32], NOW),
            Decision::Refuse(Dimension::Purpose)
        );
        assert_ne!(text_intent.digest(), legacy_intent.digest());
    }

    #[test]
    fn hex_member_is_hashed_and_never_matches_as_a_literal() {
        let text = hex(&PURPOSE);
        let member = must(Purpose::new(text.as_str()));
        assert!(matches!(
            purpose_from_set(&[member.clone()], Some(PURPOSE)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        let hashed = must(purpose_commitment_v1(&text));
        assert_eq!(
            must(purpose_from_set(&[member], Some(hashed))),
            PurposeBinding::TextV1 { commitment: hashed }
        );
        let mut referenced = record(1, None, 3);
        referenced.purposes = BTreeSet::from([text]);
        let hashed_intent = PlanIntent {
            purpose: PurposeBinding::TextV1 { commitment: hashed },
            ..intent()
        };
        assert_eq!(
            evaluate(&referenced, ASSET_MASK, &hashed_intent, &[], &[10; 32], NOW),
            Decision::Allow
        );
        let literal_intent = PlanIntent {
            purpose: PurposeBinding::Commitment(PURPOSE),
            ..intent()
        };
        assert_eq!(
            evaluate(
                &referenced,
                ASSET_MASK,
                &literal_intent,
                &[],
                &[10; 32],
                NOW
            ),
            Decision::Refuse(Dimension::Purpose)
        );
    }

    #[test]
    fn mismatching_label_is_refused_on_purpose() {
        let commitment = must(purpose_commitment_v1("rent"));
        assert!(matches!(
            purpose_from_set(&[must(Purpose::new("Rent"))], Some(commitment)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
        assert!(matches!(
            purpose_from_set(&[must(Purpose::new("rent "))], Some(commitment)),
            Err(BindingError::Refused(Dimension::Purpose))
        ));
    }

    #[test]
    fn text_binding_round_trips_under_the_new_version() {
        let binding = PreparationBinding {
            capability_id: [1; 32],
            activity: activity(ModuleId::Asset),
            disclosure_digest: DIGEST,
            purpose: PurposeBinding::TextV1 {
                commitment: must(purpose_commitment_v1("rent")),
            },
        };
        let bytes = binding.encode();
        assert_eq!(bytes[0], PREPARATION_TEXT_VERSION);
        assert_eq!(bytes[bytes.len() - 33], PURPOSE_TEXT_V1);
        assert_eq!(must(PreparationBinding::decode(&bytes)), binding);
        let mut downgraded = bytes.clone();
        downgraded[0] = VERSION;
        assert!(matches!(
            PreparationBinding::decode(&downgraded),
            Err(BindingError::Corrupt)
        ));
        let mut unknown_version = bytes.clone();
        unknown_version[0] = 3;
        assert!(matches!(
            PreparationBinding::decode(&unknown_version),
            Err(BindingError::Corrupt)
        ));
        let mut unknown_tag = bytes.clone();
        let tag = bytes.len() - 33;
        unknown_tag[tag] = 3;
        assert!(matches!(
            PreparationBinding::decode(&unknown_tag),
            Err(BindingError::Corrupt)
        ));
        let mut trailing = bytes;
        trailing.push(0);
        assert!(PreparationBinding::decode(&trailing).is_err());
    }

    #[test]
    fn previous_version_commitment_record_decodes_unchanged() {
        let activity = activity(ModuleId::Asset);
        let mut previous = vec![1_u8];
        previous.extend_from_slice(&[1; 32]);
        previous.extend_from_slice(&activity.to_be_bytes());
        previous.extend_from_slice(&DIGEST);
        previous.push(1);
        previous.extend_from_slice(&PURPOSE);
        let binding = PreparationBinding {
            capability_id: [1; 32],
            activity,
            disclosure_digest: DIGEST,
            purpose: PurposeBinding::Commitment(PURPOSE),
        };
        assert_eq!(must(PreparationBinding::decode(&previous)), binding);
        assert_eq!(binding.encode(), previous);
        let mut retagged = previous;
        retagged[0] = PREPARATION_TEXT_VERSION;
        assert!(matches!(
            PreparationBinding::decode(&retagged),
            Err(BindingError::Corrupt)
        ));
    }
}
