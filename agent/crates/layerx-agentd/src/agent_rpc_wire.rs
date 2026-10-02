//! Strict JSON wire forms of the mutating catalogue requests whose agent-api types carry no serde.
//!
//! Every wire struct refuses unknown fields, decodes canonical decimal strings and lowercase hex
//! with the dispatcher helpers, and converts into the agent-api request through its real
//! constructor or validator. The dispatcher digest and the adapters decode through these same
//! functions; [`Canonical`] gives the fixed JSON of the converted request for the digest.

use layerx_agent_api::budget::{BudgetCreate, BudgetEnforcement, BudgetFund, BudgetId, BudgetTarget};
use layerx_agent_api::capability::{
    AmountCeiling, CapabilityAttenuate, CapabilityCreate, CapabilityDimensions, CapabilityId,
    CapabilityRevoke, ExplicitSet, RateCeiling,
};
use layerx_agent_api::error::RequestId;
use layerx_agent_api::identity::{
    ActivityType, AgentDid, Asset, AuthorityRef, ClientId, ContractError, Counterparty,
    PolicyVersion, Purpose, SessionClose, SessionContext, SessionId, SessionRefresh, TenantId,
};
use layerx_agent_api::read::{AccountRef, ModuleRef};
use layerx_agent_api::submit::{PreparationRef, SignRequest, SignatureBytes};
use layerx_agent_api::subscription::{
    Cursor, CursorAcknowledgement, DeliveryTarget, SubscriptionCreate, SubscriptionFilter,
    SubscriptionId, SubscriptionScope, SubscriptionTarget, TenantObject,
};
use layerx_agent_api::{Amount, BudgetLimit, Sequence, TimestampSeconds};
use layerx_types::intent::CapabilityRequest;
use layerx_types::result::ResultCode;
use serde::de::DeserializeOwned;
use serde::Deserialize;
use serde_json::{json, Map, Value};

use crate::agent_rpc::Rejection;
use crate::agent_rpc_dispatch::{
    decimal_u128, decimal_u64, decode, hex32, hex_bytes, lower_hex, malformed, noncanonical,
};
use crate::human::HumanRefusal;
use layerx_agent_api::budget::{BudgetAuthorization, SignedBudgetMutation};
use layerx_agent_api::error::Retriability;
use layerx_agent_api::export::{validate_export_request, FactRef};
use layerx_agent_api::policy::PolicyDryRunRequest;
use layerx_agent_api::read::{FeeProjectionRequest, ReadRequest};
use layerx_agent_api::identity::{LegacyPolicyDryRun, MAX_POLICY_INTENT_BYTES};

/// Decodes the operation `request` object into a wire struct with the dispatcher's strict
/// decode error mapping (`envelope.unknown_field`, otherwise `envelope.malformed`).
///
/// # Errors
/// Returns the dispatcher decode [`Rejection`].
pub(crate) fn decode_wire<W: DeserializeOwned>(
    request: &Map<String, Value>,
    id: RequestId,
) -> Result<W, Rejection> {
    decode(request, id)
}

/// Fixed-order JSON of a converted request, used as the canonical digest bytes.
pub(crate) trait Canonical {
    fn canonical(&self) -> Value;
}

fn contract(id: RequestId) -> impl Fn(ContractError) -> Rejection {
    move |_| malformed(id)
}

fn text<T>(
    value: String,
    id: RequestId,
    new: fn(String) -> Result<T, ContractError>,
) -> Result<T, Rejection> {
    new(value).map_err(contract(id))
}

fn timestamp(value: &str, id: RequestId) -> Result<TimestampSeconds, Rejection> {
    Ok(TimestampSeconds(decimal_u64(value, id)?))
}

fn activity_type(value: &str, id: RequestId) -> Result<ActivityType, Rejection> {
    Ok(ActivityType(
        u16::try_from(decimal_u64(value, id)?).map_err(|_| noncanonical(id))?,
    ))
}

fn texts<T>(
    values: Vec<String>,
    id: RequestId,
    new: fn(String) -> Result<T, ContractError>,
) -> Result<ExplicitSet<T>, Rejection> {
    Ok(ExplicitSet::allow(
        values
            .into_iter()
            .map(|value| text(value, id, new))
            .collect::<Result<_, _>>()?,
    ))
}

fn activity_types(values: &[String], id: RequestId) -> Result<ExplicitSet<ActivityType>, Rejection> {
    Ok(ExplicitSet::allow(
        values
            .iter()
            .map(|value| activity_type(value, id))
            .collect::<Result<_, _>>()?,
    ))
}

fn enforcement(value: &str, id: RequestId) -> Result<BudgetEnforcement, Rejection> {
    match value {
        "ProtocolBudget" => Ok(BudgetEnforcement::ProtocolBudget),
        "DaemonLimit" => Ok(BudgetEnforcement::DaemonLimit),
        _ => Err(malformed(id)),
    }
}

const fn enforcement_name(value: BudgetEnforcement) -> &'static str {
    match value {
        BudgetEnforcement::ProtocolBudget => "ProtocolBudget",
        BudgetEnforcement::DaemonLimit => "DaemonLimit",
    }
}

fn strs<T>(set: &ExplicitSet<T>, as_str: fn(&T) -> &str) -> Value {
    Value::Array(set.values().iter().map(|item| Value::String(as_str(item).into())).collect())
}

fn activity_values(set: &ExplicitSet<ActivityType>) -> Value {
    Value::Array(set.values().iter().map(|item| Value::String(item.0.to_string())).collect())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SessionContextWire {
    tenant: String,
    agent_did: String,
    authority_ref: String,
    permitted_activity_types: Vec<String>,
    expiry: String,
    client: String,
    policy_version: String,
}

impl SessionContextWire {
    fn into_context(self, id: RequestId) -> Result<SessionContext, Rejection> {
        SessionContext::new(
            text(self.tenant, id, TenantId::new)?,
            text(self.agent_did, id, AgentDid::new)?,
            text(self.authority_ref, id, AuthorityRef::new)?,
            activity_types(&self.permitted_activity_types, id)?,
            timestamp(&self.expiry, id)?,
            text(self.client, id, ClientId::new)?,
            text(self.policy_version, id, PolicyVersion::new)?,
        )
        .map_err(contract(id))
    }
}

fn context_value(context: &SessionContext) -> Value {
    json!({
        "tenant": context.tenant.as_str(),
        "agent_did": context.agent_did.as_str(),
        "authority_ref": context.authority_ref.as_str(),
        "permitted_activity_types": activity_values(&context.permitted_activity_types),
        "expiry": context.expiry.0.to_string(),
        "client": context.client.as_str(),
        "policy_version": context.policy_version.as_str(),
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BudgetCreateWire {
    tenant: String,
    agent_did: String,
    asset: String,
    limit: String,
    enforcement: String,
    expiry: String,
    #[serde(default)]
    authorization: Option<BudgetAuthorizationWire>,
}

impl BudgetCreateWire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<SignedBudgetMutation<BudgetCreate>, Rejection> {
        SignedBudgetMutation {
            request: BudgetCreate {
                tenant: text(self.tenant, id, TenantId::new)?,
                agent_did: text(self.agent_did, id, AgentDid::new)?,
                asset: text(self.asset, id, Asset::new)?,
                limit: BudgetLimit(decimal_u128(&self.limit, id)?),
                enforcement: enforcement(&self.enforcement, id)?,
                expiry: timestamp(&self.expiry, id)?,
            },
            authorization: budget_authorization(self.authorization, id)?,
        }
        .validate()
        .map_err(budget_contract(id))
    }
}

impl Canonical for BudgetCreate {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "asset": self.asset.as_str(),
            "limit": self.limit.0.to_string(),
            "enforcement": enforcement_name(self.enforcement),
            "expiry": self.expiry.0.to_string(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BudgetFundWire {
    tenant: String,
    agent_did: String,
    budget_id: String,
    amount: String,
    enforcement: String,
    #[serde(default)]
    authorization: Option<BudgetAuthorizationWire>,
}

impl BudgetFundWire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<SignedBudgetMutation<BudgetFund>, Rejection> {
        SignedBudgetMutation {
            request: BudgetFund {
                tenant: text(self.tenant, id, TenantId::new)?,
                agent_did: text(self.agent_did, id, AgentDid::new)?,
                budget_id: text(self.budget_id, id, BudgetId::new)?,
                amount: Amount(decimal_u128(&self.amount, id)?),
                enforcement: enforcement(&self.enforcement, id)?,
            },
            authorization: budget_authorization(self.authorization, id)?,
        }
        .validate()
        .map_err(budget_contract(id))
    }
}

impl Canonical for BudgetFund {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "budget_id": self.budget_id.as_str(),
            "amount": self.amount.0.to_string(),
            "enforcement": enforcement_name(self.enforcement),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BudgetTargetWire {
    tenant: String,
    agent_did: String,
    budget_id: String,
    #[serde(default)]
    authorization: Option<BudgetAuthorizationWire>,
}

impl BudgetTargetWire {
    /// `budget.revoke`: the owner checks the carrier against the enforcement of the
    /// authenticated budget it looks up (`SignedBudgetMutation::require_for`).
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<SignedBudgetMutation<BudgetTarget>, Rejection> {
        Ok(SignedBudgetMutation {
            request: BudgetTarget {
                tenant: text(self.tenant, id, TenantId::new)?,
                agent_did: text(self.agent_did, id, AgentDid::new)?,
                budget_id: text(self.budget_id, id, BudgetId::new)?,
            },
            authorization: budget_authorization(self.authorization, id)?,
        })
    }
}

/// `budget.state`: the `BudgetTargetWire` request of a read; a signed-authorization carrier is
/// never part of a read and is refused.
pub(crate) fn budget_state_request(
    request: &Map<String, Value>,
    id: RequestId,
) -> Result<BudgetTarget, Rejection> {
    let target = decode_wire::<BudgetTargetWire>(request, id)?.into_request(id)?;
    if target.authorization.is_some() {
        return Err(malformed(id));
    }
    Ok(target.request)
}

impl Canonical for BudgetTarget {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "budget_id": self.budget_id.as_str(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct AmountCeilingWire {
    asset: String,
    amount: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct RateCeilingWire {
    window_seconds: String,
    maximum_actions: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CapabilityDimensionsWire {
    activity_types: Vec<String>,
    counterparties: Vec<String>,
    assets: Vec<String>,
    amount_ceilings: Vec<AmountCeilingWire>,
    rate_ceilings: Vec<RateCeilingWire>,
    purpose_constraints: Vec<String>,
    expiry: String,
}

impl CapabilityDimensionsWire {
    fn into_dimensions(self, id: RequestId) -> Result<CapabilityDimensions, Rejection> {
        let amount_ceilings = self
            .amount_ceilings
            .into_iter()
            .map(|ceiling| {
                Ok(AmountCeiling {
                    asset: text(ceiling.asset, id, Asset::new)?,
                    amount: Amount(decimal_u128(&ceiling.amount, id)?),
                })
            })
            .collect::<Result<_, Rejection>>()?;
        let rate_ceilings = self
            .rate_ceilings
            .into_iter()
            .map(|ceiling| {
                Ok(RateCeiling {
                    window_seconds: timestamp(&ceiling.window_seconds, id)?,
                    maximum_actions: decimal_u64(&ceiling.maximum_actions, id)?,
                })
            })
            .collect::<Result<_, Rejection>>()?;
        CapabilityDimensions {
            activity_types: activity_types(&self.activity_types, id)?,
            counterparties: texts(self.counterparties, id, Counterparty::new)?,
            assets: texts(self.assets, id, Asset::new)?,
            amount_ceilings: ExplicitSet::allow(amount_ceilings),
            rate_ceilings: ExplicitSet::allow(rate_ceilings),
            purpose_constraints: texts(self.purpose_constraints, id, Purpose::new)?,
            expiry: timestamp(&self.expiry, id)?,
        }
        .validate()
        .map_err(contract(id))
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CapabilityListWire {
    tenant: String,
    agent_did: String,
}

impl CapabilityListWire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<layerx_agent_api::capability::CapabilityList, Rejection> {
        Ok(layerx_agent_api::capability::CapabilityList {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
        })
    }
}

/// Signed-authorization carrier of a budget mutation. The signature reuses the
/// `sign` decoding (64 bytes, lowercase hex); the optional legacy signer key is only an
/// equality assertion and is strict 64-digit lowercase hex when present.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct BudgetAuthorizationWire {
    preparation_ref: String,
    signature: String,
    #[serde(default)]
    signer_public_key: Option<String>,
}

fn budget_authorization(
    wire: Option<BudgetAuthorizationWire>,
    id: RequestId,
) -> Result<Option<BudgetAuthorization>, Rejection> {
    wire.map(|wire| {
        let signed = SignRequestWire {
            preparation_ref: wire.preparation_ref,
            signature: wire.signature,
        }
        .into_request(id)?;
        Ok(BudgetAuthorization {
            preparation_ref: signed.preparation_ref,
            signature: signed.signature,
            signer_public_key: wire
                .signer_public_key
                .map(|key| hex32(&key, id))
                .transpose()?,
        })
    })
    .transpose()
}

/// Carrier refusals of `SignedBudgetMutation::validate` keep their typed budget reasons;
/// every other contract failure is the malformed request it always was.
fn budget_contract(id: RequestId) -> impl Fn(ContractError) -> Rejection {
    move |error| {
        let refusal = match error {
            ContractError::Empty("budget_authorization") => {
                HumanRefusal::BudgetAuthorizationRequired
            }
            ContractError::Mismatch("budget_authorization") => {
                HumanRefusal::BudgetAuthorizationUnexpected
            }
            ContractError::DaemonLimitFunding => HumanRefusal::BudgetDaemonLimitFunding,
            _ => return malformed(id),
        };
        Rejection {
            class: refusal.class(),
            retriability: Retriability::Terminal,
            request_id: id,
            reason: refusal.reason(),
        }
    }
}

/// Without a carrier the canonical bytes are exactly the unsigned request's, so existing
/// daemon-limit idempotency records keep their digests; with one, every carrier field is bound.
impl<T: Canonical> Canonical for SignedBudgetMutation<T> {
    fn canonical(&self) -> Value {
        match &self.authorization {
            None => self.request.canonical(),
            Some(authorization) => json!({
                "request": self.request.canonical(),
                "authorization": {
                    "preparation_ref": authorization.preparation_ref.as_str(),
                    "signature": lower_hex(authorization.signature.as_bytes()),
                    "signer_public_key": authorization.signer_public_key.map(|key| lower_hex(&key)),
                },
            }),
        }
    }
}

/// `project` (fee projection): the four meter inputs as canonical decimals.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct FeeProjectionWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    protocol_activity_type: String,
    canonical_bytes: String,
    execution_units: String,
    storage_units: String,
}

impl FeeProjectionWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<FeeProjectionRequest, Rejection> {
        let _ = (self.tenant, self.agent);
        FeeProjectionRequest {
            protocol_activity_type: u32::try_from(decimal_u64(&self.protocol_activity_type, id)?)
                .map_err(|_| noncanonical(id))?,
            canonical_bytes: decimal_u64(&self.canonical_bytes, id)?,
            execution_units: decimal_u64(&self.execution_units, id)?,
            storage_units: decimal_u64(&self.storage_units, id)?,
        }
        .validate()
        .map_err(contract(id))
    }
}

/// The legacy `project` policy payload (`context` + `canonical_intent`). It is recognised
/// only to refuse it with its typed reason; it is never reinterpreted.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct LegacyProjectWire {
    #[serde(rename = "context")]
    _context: Value,
    #[serde(rename = "canonical_intent")]
    _canonical_intent: Value,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct PolicyDryRunWire {
    tenant: String,
    agent_did: String,
    session_id: String,
    capability_id: String,
    activity_type: String,
    counterparty: String,
    asset: String,
    amount: String,
    purpose: String,
    core_sequence: String,
}

impl PolicyDryRunWire {
    fn into_request(self, id: RequestId) -> Result<PolicyDryRunRequest, Rejection> {
        PolicyDryRunRequest {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
            session_id: text(self.session_id, id, SessionId::new)?,
            capability_id: text(self.capability_id, id, CapabilityId::new)?,
            activity_type: activity_type(&self.activity_type, id)?,
            counterparty: hex32(&self.counterparty, id)?,
            asset: hex32(&self.asset, id)?,
            amount: Amount(decimal_u128(&self.amount, id)?),
            purpose: self.purpose,
            core_sequence: Sequence(decimal_u64(&self.core_sequence, id)?),
        }
        .validate()
        .map_err(contract(id))
    }
}

/// The two disjoint `policy.dry_run` request shapes.
pub(crate) enum PolicyDryRunShape {
    Typed(PolicyDryRunRequest),
    Legacy(LegacyPolicyDryRun),
}

const POLICY_DRY_RUN_TYPED_KEYS: [&str; 10] = [
    "tenant",
    "agent_did",
    "session_id",
    "capability_id",
    "activity_type",
    "counterparty",
    "asset",
    "amount",
    "purpose",
    "core_sequence",
];

const POLICY_DRY_RUN_LEGACY_KEYS: [&str; 2] = ["context", "canonical_intent"];

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct LegacyPolicyDryRunWire {
    context: SessionContextWire,
    canonical_intent: String,
}

impl LegacyPolicyDryRunWire {
    fn into_request(self, id: RequestId) -> Result<LegacyPolicyDryRun, Rejection> {
        if self.canonical_intent.is_empty()
            || self.canonical_intent.len() > MAX_POLICY_INTENT_BYTES * 2
        {
            return Err(malformed(id));
        }
        Ok(LegacyPolicyDryRun {
            context: self.context.into_context(id)?,
            canonical_intent: hex_bytes(&self.canonical_intent, id)?,
        })
    }
}

/// The single decoder of the `policy.dry_run` request.
pub(crate) fn policy_dry_run_request(
    request: &Map<String, Value>,
    id: RequestId,
) -> Result<PolicyDryRunShape, Rejection> {
    let within = |keys: &[&str]| request.keys().all(|key| keys.contains(&key.as_str()));
    if within(&POLICY_DRY_RUN_TYPED_KEYS) {
        decode_wire::<PolicyDryRunWire>(request, id)?
            .into_request(id)
            .map(PolicyDryRunShape::Typed)
    } else if within(&POLICY_DRY_RUN_LEGACY_KEYS) {
        decode_wire::<LegacyPolicyDryRunWire>(request, id)?
            .into_request(id)
            .map(PolicyDryRunShape::Legacy)
    } else {
        Err(malformed(id))
    }
}

/// `export.offline`: the fact set is checked by the one shared fact grammar
/// (`parse_fact_set`: 1..=16, unique, strict) and the requested level is kept as sent.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ExportOfflineWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    fact_set: Vec<String>,
    requested_verification_level: String,
}

impl ExportOfflineWire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<ReadRequest<Vec<FactRef>>, Rejection> {
        let _ = (self.tenant, self.agent);
        validate_export_request(ReadRequest {
            selector: self
                .fact_set
                .into_iter()
                .map(|fact| text(fact, id, FactRef::new))
                .collect::<Result<_, _>>()?,
            requested_verification_level: export_level(&self.requested_verification_level, id)?,
        })
        .map_err(|_| malformed(id))
    }
}

/// The export level spelling is the envelope's level name; any other text is malformed.
fn export_level(text: &str, id: RequestId) -> Result<layerx_agent_api::error::Level, Rejection> {
    use layerx_agent_api::error::Level;
    [
        Level::Unverified,
        Level::SequencerSigned,
        Level::BatchIncluded,
        Level::StateProven,
        Level::CheckpointFinalised,
        Level::SettlementAnchored,
    ]
    .into_iter()
    .find(|level| crate::agent_rpc_dispatch::level_name(*level) == text)
    .ok_or_else(|| malformed(id))
}

fn dimensions_value(dimensions: &CapabilityDimensions) -> Value {
    json!({
        "activity_types": activity_values(&dimensions.activity_types),
        "counterparties": strs(&dimensions.counterparties, Counterparty::as_str),
        "assets": strs(&dimensions.assets, Asset::as_str),
        "amount_ceilings": dimensions.amount_ceilings.values().iter().map(|ceiling| json!({
            "asset": ceiling.asset.as_str(),
            "amount": ceiling.amount.0.to_string(),
        })).collect::<Vec<_>>(),
        "rate_ceilings": dimensions.rate_ceilings.values().iter().map(|ceiling| json!({
            "window_seconds": ceiling.window_seconds.0.to_string(),
            "maximum_actions": ceiling.maximum_actions.to_string(),
        })).collect::<Vec<_>>(),
        "purpose_constraints": strs(&dimensions.purpose_constraints, Purpose::as_str),
        "expiry": dimensions.expiry.0.to_string(),
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CapabilityCreateWire {
    tenant: String,
    agent_did: String,
    dimensions: CapabilityDimensionsWire,
}

impl CapabilityCreateWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<CapabilityCreate, Rejection> {
        Ok(CapabilityCreate {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
            dimensions: self.dimensions.into_dimensions(id)?,
        })
    }
}

impl Canonical for CapabilityCreate {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "dimensions": dimensions_value(&self.dimensions),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CapabilityAttenuateWire {
    tenant: String,
    agent_did: String,
    parent_id: String,
    dimensions: CapabilityDimensionsWire,
}

impl CapabilityAttenuateWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<CapabilityAttenuate, Rejection> {
        Ok(CapabilityAttenuate {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
            parent_id: text(self.parent_id, id, CapabilityId::new)?,
            dimensions: self.dimensions.into_dimensions(id)?,
        })
    }
}

impl Canonical for CapabilityAttenuate {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "parent_id": self.parent_id.as_str(),
            "dimensions": dimensions_value(&self.dimensions),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CapabilityRevokeWire {
    tenant: String,
    agent_did: String,
    capability_id: String,
}

impl CapabilityRevokeWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<CapabilityRevoke, Rejection> {
        Ok(CapabilityRevoke {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
            capability_id: text(self.capability_id, id, CapabilityId::new)?,
        })
    }
}

impl Canonical for CapabilityRevoke {
    fn canonical(&self) -> Value {
        json!({
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "capability_id": self.capability_id.as_str(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SessionRefreshWire {
    session_id: String,
    context: SessionContextWire,
}

impl SessionRefreshWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<SessionRefresh, Rejection> {
        Ok(SessionRefresh {
            session_id: text(self.session_id, id, SessionId::new)?,
            context: self.context.into_context(id)?,
        })
    }
}

impl Canonical for SessionRefresh {
    fn canonical(&self) -> Value {
        json!({
            "session_id": self.session_id.as_str(),
            "context": context_value(&self.context),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SessionCloseWire {
    session_id: String,
    context: SessionContextWire,
}

impl SessionCloseWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<SessionClose, Rejection> {
        Ok(SessionClose {
            session_id: text(self.session_id, id, SessionId::new)?,
            context: self.context.into_context(id)?,
        })
    }
}

impl Canonical for SessionClose {
    fn canonical(&self) -> Value {
        json!({
            "session_id": self.session_id.as_str(),
            "context": context_value(&self.context),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubscriptionScopeWire {
    tenant: String,
    agent: String,
    capability: String,
}

impl SubscriptionScopeWire {
    fn into_scope(self, id: RequestId) -> Result<SubscriptionScope, Rejection> {
        Ok(SubscriptionScope {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent: text(self.agent, id, AgentDid::new)?,
            capability: text(self.capability, id, CapabilityId::new)?,
        })
    }
}

fn scope_value(scope: &SubscriptionScope) -> Value {
    json!({
        "tenant": scope.tenant.as_str(),
        "agent": scope.agent.as_str(),
        "capability": scope.capability.as_str(),
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct TenantObjectWire {
    tenant: String,
    value: String,
}

fn tenant_objects<T>(
    values: Vec<TenantObjectWire>,
    id: RequestId,
    new: fn(String) -> Result<T, ContractError>,
) -> Result<ExplicitSet<TenantObject<T>>, Rejection> {
    Ok(ExplicitSet::allow(
        values
            .into_iter()
            .map(|item| {
                Ok(TenantObject {
                    tenant: text(item.tenant, id, TenantId::new)?,
                    value: text(item.value, id, new)?,
                })
            })
            .collect::<Result<_, Rejection>>()?,
    ))
}

fn tenant_object_values<T>(set: &ExplicitSet<TenantObject<T>>, as_str: fn(&T) -> &str) -> Value {
    Value::Array(
        set.values()
            .iter()
            .map(|item| json!({"tenant": item.tenant.as_str(), "value": as_str(&item.value)}))
            .collect(),
    )
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubscriptionFilterWire {
    agents: Vec<TenantObjectWire>,
    accounts: Vec<TenantObjectWire>,
    activity_types: Vec<String>,
    modules: Vec<TenantObjectWire>,
    assets: Vec<TenantObjectWire>,
    counterparties: Vec<TenantObjectWire>,
    result_classes: Vec<i32>,
}

impl SubscriptionFilterWire {
    fn into_filter(self, id: RequestId) -> Result<SubscriptionFilter, Rejection> {
        Ok(SubscriptionFilter {
            agents: tenant_objects(self.agents, id, AgentDid::new)?,
            accounts: tenant_objects(self.accounts, id, AccountRef::new)?,
            activity_types: activity_types(&self.activity_types, id)?,
            modules: tenant_objects(self.modules, id, ModuleRef::new)?,
            assets: tenant_objects(self.assets, id, Asset::new)?,
            counterparties: tenant_objects(self.counterparties, id, Counterparty::new)?,
            result_classes: ExplicitSet::allow(
                self.result_classes.into_iter().map(ResultCode::from_raw).collect(),
            ),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubscriptionCreateWire {
    scope: SubscriptionScopeWire,
    filter: SubscriptionFilterWire,
    start: String,
    delivery_target: String,
}

impl SubscriptionCreateWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<SubscriptionCreate, Rejection> {
        SubscriptionCreate {
            scope: self.scope.into_scope(id)?,
            filter: self.filter.into_filter(id)?,
            start: Cursor(Sequence(decimal_u64(&self.start, id)?)),
            delivery_target: text(self.delivery_target, id, DeliveryTarget::new)?,
        }
        .validate()
        .map_err(contract(id))
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubscriptionListWire {
    pub(crate) scope: SubscriptionScopeWire,
}

impl SubscriptionListWire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<layerx_agent_api::subscription::SubscriptionList, Rejection> {
        Ok(layerx_agent_api::subscription::SubscriptionList {
            scope: self.scope.into_scope(id)?,
        })
    }
}

impl Canonical for layerx_agent_api::subscription::SubscriptionList {
    fn canonical(&self) -> Value {
        json!({
            "scope": scope_value(&self.scope),
        })
    }
}

impl Canonical for SubscriptionCreate {
    fn canonical(&self) -> Value {
        let filter = &self.filter;
        json!({
            "scope": scope_value(&self.scope),
            "filter": {
                "agents": tenant_object_values(&filter.agents, AgentDid::as_str),
                "accounts": tenant_object_values(&filter.accounts, AccountRef::as_str),
                "activity_types": activity_values(&filter.activity_types),
                "modules": tenant_object_values(&filter.modules, ModuleRef::as_str),
                "assets": tenant_object_values(&filter.assets, Asset::as_str),
                "counterparties": tenant_object_values(&filter.counterparties, Counterparty::as_str),
                "result_classes": filter.result_classes.values().iter().map(|code| code.raw()).collect::<Vec<_>>(),
            },
            "start": self.start.0 .0.to_string(),
            "delivery_target": self.delivery_target.as_str(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SubscriptionTargetWire {
    scope: SubscriptionScopeWire,
    subscription_id: String,
}

impl SubscriptionTargetWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<SubscriptionTarget, Rejection> {
        Ok(SubscriptionTarget {
            scope: self.scope.into_scope(id)?,
            subscription_id: text(self.subscription_id, id, SubscriptionId::new)?,
        })
    }
}

impl Canonical for SubscriptionTarget {
    fn canonical(&self) -> Value {
        json!({
            "scope": scope_value(&self.scope),
            "subscription_id": self.subscription_id.as_str(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct CursorAcknowledgementWire {
    scope: SubscriptionScopeWire,
    subscription_id: String,
    cursor: String,
}

impl CursorAcknowledgementWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<CursorAcknowledgement, Rejection> {
        Ok(CursorAcknowledgement {
            scope: self.scope.into_scope(id)?,
            subscription_id: text(self.subscription_id, id, SubscriptionId::new)?,
            cursor: Cursor(Sequence(decimal_u64(&self.cursor, id)?)),
        })
    }
}

impl Canonical for CursorAcknowledgement {
    fn canonical(&self) -> Value {
        json!({
            "scope": scope_value(&self.scope),
            "subscription_id": self.subscription_id.as_str(),
            "cursor": self.cursor.0 .0.to_string(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SignRequestWire {
    preparation_ref: String,
    signature: String,
}

impl SignRequestWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<SignRequest, Rejection> {
        let signature = hex_bytes(&self.signature, id)?;
        if signature.len() != 64 {
            return Err(malformed(id));
        }
        Ok(SignRequest {
            preparation_ref: text(self.preparation_ref, id, PreparationRef::new)?,
            signature: SignatureBytes::new(signature).map_err(contract(id))?,
        })
    }
}

impl Canonical for SignRequest {
    fn canonical(&self) -> Value {
        json!({
            "preparation_ref": self.preparation_ref.as_str(),
            "signature": lower_hex(self.signature.as_bytes()),
        })
    }
}

/// Converted `program.call` / `program.simulate` request (SDK `wire_call` shape).
pub(crate) struct ProgramCallRequest {
    pub(crate) program_id: [u8; 32],
    pub(crate) calldata: Vec<u8>,
    pub(crate) fuel: u64,
    pub(crate) fee_limit: u128,
    pub(crate) capabilities: Vec<CapabilityRequest>,
    pub(crate) signed_activity: Vec<u8>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ProgramBudgetWire {
    fuel: String,
    fee_limit: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ProgramCallWire {
    program_id: String,
    calldata: String,
    budget: ProgramBudgetWire,
    capabilities: Vec<String>,
    signed_activity: String,
}

fn capability(value: &str, id: RequestId) -> Result<CapabilityRequest, Rejection> {
    Ok(match value {
        "storage_read" => CapabilityRequest::StorageRead,
        "storage_write" => CapabilityRequest::StorageWrite,
        "transfer" => CapabilityRequest::Transfer,
        "emit_event" => CapabilityRequest::EmitEvent,
        "compose" => CapabilityRequest::Compose,
        _ => return Err(malformed(id)),
    })
}

const fn capability_name(value: CapabilityRequest) -> &'static str {
    match value {
        CapabilityRequest::StorageRead => "storage_read",
        CapabilityRequest::StorageWrite => "storage_write",
        CapabilityRequest::Transfer => "transfer",
        CapabilityRequest::EmitEvent => "emit_event",
        CapabilityRequest::Compose => "compose",
    }
}

fn signed_activity(value: &str, id: RequestId) -> Result<Vec<u8>, Rejection> {
    let bytes = hex_bytes(value, id)?;
    if bytes.is_empty() {
        return Err(malformed(id));
    }
    Ok(bytes)
}

impl ProgramCallWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<ProgramCallRequest, Rejection> {
        Ok(ProgramCallRequest {
            program_id: hex32(&self.program_id, id)?,
            calldata: hex_bytes(&self.calldata, id)?,
            fuel: decimal_u64(&self.budget.fuel, id)?,
            fee_limit: decimal_u128(&self.budget.fee_limit, id)?,
            capabilities: self
                .capabilities
                .iter()
                .map(|value| capability(value, id))
                .collect::<Result<_, _>>()?,
            signed_activity: signed_activity(&self.signed_activity, id)?,
        })
    }
}

impl Canonical for ProgramCallRequest {
    fn canonical(&self) -> Value {
        json!({
            "program_id": lower_hex(&self.program_id),
            "calldata": lower_hex(&self.calldata),
            "budget": {"fuel": self.fuel.to_string(), "fee_limit": self.fee_limit.to_string()},
            "capabilities": self.capabilities.iter().map(|value| capability_name(*value)).collect::<Vec<_>>(),
            "signed_activity": lower_hex(&self.signed_activity),
        })
    }
}

/// `program.simulate` carries the same SDK `wire_call` body as `program.call`.
pub(crate) type ProgramSimulateWire = ProgramCallWire;

/// Converted lifecycle request (`program.deploy`, `program.upgrade`, `program.wind-down`):
/// the SDK sends only the signed lifecycle activity; ordinal and payload checks run in
/// `ops::program::validate_*_activity` against the module registry.
pub(crate) struct ProgramLifecycleRequest {
    pub(crate) signed_activity: Vec<u8>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ProgramLifecycleWire {
    signed_activity: String,
}

impl ProgramLifecycleWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<ProgramLifecycleRequest, Rejection> {
        Ok(ProgramLifecycleRequest {
            signed_activity: signed_activity(&self.signed_activity, id)?,
        })
    }
}

impl Canonical for ProgramLifecycleRequest {
    fn canonical(&self) -> Value {
        json!({"signed_activity": lower_hex(&self.signed_activity)})
    }
}

pub(crate) type ProgramDeployWire = ProgramLifecycleWire;
pub(crate) type ProgramUpgradeWire = ProgramLifecycleWire;
pub(crate) type ProgramWindDownWire = ProgramLifecycleWire;

const REQUESTED_VERIFICATION: &str = "sequencer-signed";

fn requested_verification(value: &str, id: RequestId) -> Result<(), Rejection> {
    if value == REQUESTED_VERIFICATION {
        Ok(())
    } else {
        Err(malformed(id))
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ProgramDiscoverWire {
    program_id: String,
    requested_verification_level: String,
}

impl ProgramDiscoverWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<[u8; 32], Rejection> {
        requested_verification(&self.requested_verification_level, id)?;
        hex32(&self.program_id, id)
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ProgramActivityWire {
    activity_id: String,
    requested_verification_level: String,
}

impl ProgramActivityWire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<[u8; 32], Rejection> {
        requested_verification(&self.requested_verification_level, id)?;
        hex32(&self.activity_id, id)
    }
}
