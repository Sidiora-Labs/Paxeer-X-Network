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
    NativeActivity, NativeApprovalDecisionV1, NativeApprovalGetV1, NativeApprovalListV1,
    NativeApprovalListResultV1, NativeApprovalResultV1, NativePreparationPurposeV1,
    NativeLocalGrantConsentV1, NativePrepareRequestV1, NativePrepareResultV1,
    SignedNativePreparationPurposeV1,
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
pub(crate) struct NativeActivityV1Wire {
    version: String,
    module: String,
    ordinal: String,
}

impl NativeActivityV1Wire {
    pub(crate) fn into_activity(self, id: RequestId) -> Result<NativeActivity, Rejection> {
        if decimal_u64(&self.version, id)? != u64::from(NativeActivity::VERSION) {
            return Err(malformed(id));
        }
        let module = u16::try_from(decimal_u64(&self.module, id)?)
            .map_err(|_| noncanonical(id))?;
        let ordinal = u16::try_from(decimal_u64(&self.ordinal, id)?)
            .map_err(|_| noncanonical(id))?;
        NativeActivity::new(module, ordinal).map_err(contract(id))
    }
}

impl Canonical for NativeActivity {
    fn canonical(&self) -> Value {
        json!({
            "version": NativeActivity::VERSION.to_string(),
            "module": self.module.to_string(),
            "ordinal": self.ordinal.to_string(),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativePreparationPurposeV1Wire {
    version: String,
    tenant: String,
    agent_did: String,
    session_id: String,
    generation: String,
    expires_at_ms: String,
    capability_id: String,
    preparation_id: String,
    canonical_digest: String,
    commitment: String,
}

impl NativePreparationPurposeV1Wire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<NativePreparationPurposeV1, Rejection> {
        if decimal_u64(&self.version, id)? != u64::from(NativePreparationPurposeV1::VERSION) {
            return Err(malformed(id));
        }
        NativePreparationPurposeV1 {
            tenant: text(self.tenant, id, TenantId::new)?,
            agent_did: text(self.agent_did, id, AgentDid::new)?,
            session_id: text(self.session_id, id, SessionId::new)?,
            generation: decimal_u64(&self.generation, id)?,
            expires_at_ms: decimal_u64(&self.expires_at_ms, id)?,
            capability_id: text(self.capability_id, id, CapabilityId::new)?,
            preparation_id: hex32(&self.preparation_id, id)?,
            canonical_digest: hex32(&self.canonical_digest, id)?,
            commitment: hex32(&self.commitment, id)?,
        }
        .validate()
        .map_err(contract(id))
    }
}

impl Canonical for NativePreparationPurposeV1 {
    fn canonical(&self) -> Value {
        json!({
            "version": NativePreparationPurposeV1::VERSION.to_string(),
            "tenant": self.tenant.as_str(),
            "agent_did": self.agent_did.as_str(),
            "session_id": self.session_id.as_str(),
            "generation": self.generation.to_string(),
            "expires_at_ms": self.expires_at_ms.to_string(),
            "capability_id": self.capability_id.as_str(),
            "preparation_id": lower_hex(&self.preparation_id),
            "canonical_digest": lower_hex(&self.canonical_digest),
            "commitment": lower_hex(&self.commitment),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct SignedNativePreparationPurposeV1Wire {
    purpose: NativePreparationPurposeV1Wire,
    owner_public_key: String,
    signature: String,
}

impl SignedNativePreparationPurposeV1Wire {
    pub(crate) fn into_request(
        self,
        id: RequestId,
    ) -> Result<SignedNativePreparationPurposeV1, Rejection> {
        if self.signature.len() != 128 {
            return Err(malformed(id));
        }
        let signature: [u8; 64] = hex_bytes(&self.signature, id)?
            .try_into()
            .map_err(|_| malformed(id))?;
        SignedNativePreparationPurposeV1 {
            purpose: self.purpose.into_request(id)?,
            owner_public_key: hex32(&self.owner_public_key, id)?,
            signature,
        }
        .validate()
        .map_err(contract(id))
    }
}

impl Canonical for SignedNativePreparationPurposeV1 {
    fn canonical(&self) -> Value {
        json!({
            "purpose": self.purpose.canonical(),
            "owner_public_key": lower_hex(&self.owner_public_key),
            "signature": lower_hex(&self.signature),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeLocalGrantConsentV1Wire {
    version: String,
    capability: String,
    session_scope: String,
    expires_at_ms: String,
    owner_public_key: String,
    signature: String,
}

impl NativeLocalGrantConsentV1Wire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<NativeLocalGrantConsentV1, Rejection> {
        native_result_version(&self.version, id)?;
        for record in [&self.capability, &self.session_scope] {
            if record.is_empty() || record.len() > layerx_wire::limits::MAX_MESSAGE_BYTES * 2 {
                return Err(malformed(id));
            }
        }
        if self.signature.len() != 128 {
            return Err(malformed(id));
        }
        let consent = NativeLocalGrantConsentV1 {
            capability: hex_bytes(&self.capability, id)?,
            session_scope: hex_bytes(&self.session_scope, id)?,
            expires_at_ms: decimal_u64(&self.expires_at_ms, id)?,
            owner_public_key: hex32(&self.owner_public_key, id)?,
            signature: hex_bytes(&self.signature, id)?.try_into().map_err(|_| malformed(id))?,
        };
        consent.validate().map_err(contract(id))?;
        Ok(consent)
    }
}

impl Canonical for NativeLocalGrantConsentV1 {
    fn canonical(&self) -> Value {
        json!({
            "version": "1",
            "capability": lower_hex(&self.capability),
            "session_scope": lower_hex(&self.session_scope),
            "expires_at_ms": self.expires_at_ms.to_string(),
            "owner_public_key": lower_hex(&self.owner_public_key),
            "signature": lower_hex(&self.signature),
        })
    }
}

pub(crate) fn native_local_grant(
    consent: &NativeLocalGrantConsentV1,
    id: RequestId,
) -> Result<crate::capability::binding::SignedNativeLocalGrantV1, Rejection> {
    consent.validate().map_err(contract(id))?;
    if consent.capability.len() > layerx_wire::limits::MAX_MESSAGE_BYTES
        || consent.session_scope.len() > layerx_wire::limits::MAX_MESSAGE_BYTES
    {
        return Err(malformed(id));
    }
    Ok(crate::capability::binding::SignedNativeLocalGrantV1 {
        capability: crate::capability::timed::NativeTimedCapabilityV1::decode(&consent.capability)
            .map_err(|_| malformed(id))?,
        session: crate::session::NativeSessionScopeV1::decode(&consent.session_scope)
            .map_err(|_| malformed(id))?,
        expires_at_ms: consent.expires_at_ms,
        owner_public_key: consent.owner_public_key,
        signature: consent.signature,
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativePrepareV1Wire {
    variant: String,
    activity: NativeActivityV1Wire,
    actor: String,
    authority: String,
    account_sequence: String,
    not_before: String,
    not_after: String,
    idempotency_key: String,
    fee_limit: String,
    payload: String,
    payload_hash: String,
    capability_id: String,
    purpose: SignedNativePreparationPurposeV1Wire,
    local_grant: Option<NativeLocalGrantConsentV1Wire>,
}

impl NativePrepareV1Wire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<NativePrepareRequestV1, Rejection> {
        if self.variant != "native_v1" {
            return Err(malformed(id));
        }
        if self.payload.len() > layerx_types::limits::MAX_PAYLOAD_BYTES * 2 {
            return Err(malformed(id));
        }
        NativePrepareRequestV1 {
            activity: self.activity.into_activity(id)?,
            actor: text(self.actor, id, AgentDid::new)?,
            authority: self.authority,
            account_sequence: decimal_u64(&self.account_sequence, id)?,
            not_before: decimal_u64(&self.not_before, id)?,
            not_after: decimal_u64(&self.not_after, id)?,
            idempotency_key: hex32(&self.idempotency_key, id)?,
            fee_limit: decimal_u128(&self.fee_limit, id)?,
            payload: hex_bytes(&self.payload, id)?,
            payload_hash: hex32(&self.payload_hash, id)?,
            capability_id: text(self.capability_id, id, CapabilityId::new)?,
            purpose: self.purpose.into_request(id)?,
            local_grant: self.local_grant.map(|grant| grant.into_request(id)).transpose()?,
        }
        .validate()
        .map_err(contract(id))
    }
}

impl Canonical for NativePrepareRequestV1 {
    fn canonical(&self) -> Value {
        json!({
            "variant": "native_v1",
            "activity": self.activity.canonical(),
            "actor": self.actor.as_str(),
            "authority": self.authority,
            "account_sequence": self.account_sequence.to_string(),
            "not_before": self.not_before.to_string(),
            "not_after": self.not_after.to_string(),
            "idempotency_key": lower_hex(&self.idempotency_key),
            "fee_limit": self.fee_limit.to_string(),
            "payload": lower_hex(&self.payload),
            "payload_hash": lower_hex(&self.payload_hash),
            "capability_id": self.capability_id.as_str(),
            "purpose": self.purpose.canonical(),
            "local_grant": self.local_grant.as_ref().map(Canonical::canonical),
        })
    }
}

pub(crate) fn native_human_prepare(
    request: &NativePrepareRequestV1,
    id: RequestId,
) -> Result<crate::human::HumanPrepare, Rejection> {
    let request = request.clone().validate().map_err(contract(id))?;
    Ok(crate::human::HumanPrepare {
        activity_type: request.activity.activity_type().map_err(contract(id))?.value(),
        actor: request.actor.as_str().to_owned(),
        authority: request.authority,
        account_sequence: request.account_sequence,
        not_before: request.not_before,
        not_after: request.not_after,
        idempotency_key: lower_hex(&request.idempotency_key),
        fee_limit: request.fee_limit,
        payload: request.payload,
        payload_hash: request.payload_hash,
        capability_id: Some(request.capability_id.to_bytes().map_err(contract(id))?),
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalListV1Wire {
    variant: String,
}

impl NativeApprovalListV1Wire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<NativeApprovalListV1, Rejection> {
        if self.variant != "native_v1" {
            return Err(malformed(id));
        }
        Ok(NativeApprovalListV1)
    }
}

impl Canonical for NativeApprovalListV1 {
    fn canonical(&self) -> Value {
        json!({"variant": "native_v1"})
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalGetV1Wire {
    variant: String,
    approval_id: String,
}

impl NativeApprovalGetV1Wire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<NativeApprovalGetV1, Rejection> {
        if self.variant != "native_v1" {
            return Err(malformed(id));
        }
        Ok(NativeApprovalGetV1 { approval_id: hex32(&self.approval_id, id)? })
    }
}

impl Canonical for NativeApprovalGetV1 {
    fn canonical(&self) -> Value {
        json!({"variant": "native_v1", "approval_id": lower_hex(&self.approval_id)})
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalDecisionV1Wire {
    variant: String,
    approval_id: String,
    held_digest: String,
    current_sequence: String,
}

impl NativeApprovalDecisionV1Wire {
    pub(crate) fn into_request(self, id: RequestId) -> Result<NativeApprovalDecisionV1, Rejection> {
        if self.variant != "native_v1" {
            return Err(malformed(id));
        }
        Ok(NativeApprovalDecisionV1 {
            approval_id: hex32(&self.approval_id, id)?,
            held_digest: hex32(&self.held_digest, id)?,
            current_sequence: decimal_u64(&self.current_sequence, id)?,
        })
    }
}

impl Canonical for NativeApprovalDecisionV1 {
    fn canonical(&self) -> Value {
        json!({
            "variant": "native_v1",
            "approval_id": lower_hex(&self.approval_id),
            "held_digest": lower_hex(&self.held_digest),
            "current_sequence": self.current_sequence.to_string(),
        })
    }
}

fn native_result_version(version: &str, id: RequestId) -> Result<(), Rejection> {
    if decimal_u64(version, id)? != 1 {
        return Err(malformed(id));
    }
    Ok(())
}

fn native_optional_hex32(value: Value, id: RequestId) -> Result<Option<[u8; 32]>, Rejection> {
    match value {
        Value::Null => Ok(None),
        Value::String(value) => hex32(&value, id).map(Some),
        _ => Err(malformed(id)),
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativePrepareResultV1Wire {
    version: String,
    preparation_id: String,
    canonical_bytes: String,
    signing_preimage: String,
    activity: NativeActivityV1Wire,
    approval_required: bool,
    approval_id: Value,
}

impl NativePrepareResultV1Wire {
    pub(crate) fn into_result(self, id: RequestId) -> Result<NativePrepareResultV1, Rejection> {
        native_result_version(&self.version, id)?;
        if self.canonical_bytes.is_empty()
            || self.canonical_bytes.len() > layerx_wire::limits::MAX_MESSAGE_BYTES * 2
        {
            return Err(malformed(id));
        }
        let approval_id = native_optional_hex32(self.approval_id, id)?;
        if self.approval_required != approval_id.is_some() {
            return Err(malformed(id));
        }
        Ok(NativePrepareResultV1 {
            preparation_id: hex32(&self.preparation_id, id)?,
            canonical_bytes: hex_bytes(&self.canonical_bytes, id)?,
            signing_preimage: hex32(&self.signing_preimage, id)?,
            activity: self.activity.into_activity(id)?,
            approval_required: self.approval_required,
            approval_id,
        })
    }
}

impl Canonical for NativePrepareResultV1 {
    fn canonical(&self) -> Value {
        json!({
            "version": "1",
            "preparation_id": lower_hex(&self.preparation_id),
            "canonical_bytes": lower_hex(&self.canonical_bytes),
            "signing_preimage": lower_hex(&self.signing_preimage),
            "activity": self.activity.canonical(),
            "approval_required": self.approval_required,
            "approval_id": self.approval_id.as_ref().map(|value| lower_hex(value)),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalResultV1Wire {
    version: String,
    approval_id: String,
    held_digest: String,
    activity: NativeActivityV1Wire,
    state: String,
    submission_ref: Value,
}

impl NativeApprovalResultV1Wire {
    pub(crate) fn into_result(self, id: RequestId) -> Result<NativeApprovalResultV1, Rejection> {
        native_result_version(&self.version, id)?;
        if !matches!(self.state.as_str(),
            "Awaiting" | "Granted" | "Rejected" | "Expired" | "Defective" | "NotRequired")
        {
            return Err(malformed(id));
        }
        Ok(NativeApprovalResultV1 {
            approval_id: hex32(&self.approval_id, id)?,
            held_digest: hex32(&self.held_digest, id)?,
            activity: self.activity.into_activity(id)?,
            state: self.state,
            submission_ref: native_optional_hex32(self.submission_ref, id)?,
        })
    }
}

impl Canonical for NativeApprovalResultV1 {
    fn canonical(&self) -> Value {
        json!({
            "version": "1",
            "approval_id": lower_hex(&self.approval_id),
            "held_digest": lower_hex(&self.held_digest),
            "activity": self.activity.canonical(),
            "state": self.state,
            "submission_ref": self.submission_ref.as_ref().map(|value| lower_hex(value)),
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct NativeApprovalListResultV1Wire {
    version: String,
    approvals: Vec<NativeApprovalResultV1Wire>,
}

impl NativeApprovalListResultV1Wire {
    pub(crate) fn into_result(self, id: RequestId) -> Result<NativeApprovalListResultV1, Rejection> {
        native_result_version(&self.version, id)?;
        if self.approvals.len() > 100 {
            return Err(malformed(id));
        }
        Ok(NativeApprovalListResultV1 {
            approvals: self.approvals.into_iter()
                .map(|approval| approval.into_result(id))
                .collect::<Result<_, _>>()?,
        })
    }
}

impl Canonical for NativeApprovalListResultV1 {
    fn canonical(&self) -> Value {
        json!({
            "version": "1",
            "approvals": self.approvals.iter().map(Canonical::canonical).collect::<Vec<_>>(),
        })
    }
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
    #[serde(default, skip_serializing_if = "Option::is_none")]
    purpose: Option<String>,
}

impl BudgetCreateWire {
    /// The optional `TextV1` purpose label. It is outside the canonical body and is bound
    /// into the request digests only through [`budget_create_purpose_suffix`].
    pub(crate) fn purpose(&self) -> Option<&str> {
        self.purpose.as_deref()
    }

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

/// Domain tag of the `TextV1` purpose label suffix of budget.create request digests.
pub(crate) const BUDGET_CREATE_PURPOSE_TEXT_V1: &[u8] = b"LayerX/budget/create-purpose/text-v1\0";

/// Bytes that bind an optional `TextV1` purpose label into the budget.create body digest and
/// idempotency request bytes: empty when the label is absent, so both stay byte-identical to
/// an unlabelled create; otherwise the domain tag, the text length as a big-endian `u64` and
/// the exact UTF-8 text.
pub(crate) fn budget_create_purpose_suffix(purpose: Option<&str>) -> Vec<u8> {
    let Some(purpose) = purpose else {
        return Vec::new();
    };
    let mut suffix = BUDGET_CREATE_PURPOSE_TEXT_V1.to_vec();
    suffix.extend_from_slice(&(purpose.len() as u64).to_be_bytes());
    suffix.extend_from_slice(purpose.as_bytes());
    suffix
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

pub(crate) enum ProgramSimulationRequest {
    Legacy(ProgramCallRequest),
    Native {
        program_id: [u8; 32],
        payload: Vec<u8>,
        fee_limit: u128,
        signed_activity: Vec<u8>,
    },
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NativeProgramSimulationWire {
    payload_encoding: String,
    program_id: String,
    calldata: String,
    budget: ProgramBudgetWire,
    signed_activity: String,
    native_call: NativeProgramCallWire,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NativeProgramCallWire {
    guest_abi: u16,
    entrypoint: String,
    capabilities_hex: String,
    access_declaration_hex: String,
    response_capacity: u32,
    resources: [String; 7],
}

pub(crate) fn program_simulation_request(
    request: &Map<String, Value>,
    id: RequestId,
) -> Result<ProgramSimulationRequest, Rejection> {
    if !request.contains_key("payload_encoding") && !request.contains_key("native_call") {
        return Ok(ProgramSimulationRequest::Legacy(
            decode_wire::<ProgramSimulateWire>(request, id)?.into_request(id)?,
        ));
    }
    let wire = decode_wire::<NativeProgramSimulationWire>(request, id)?;
    if wire.payload_encoding != "native-v1" {
        return Err(malformed(id));
    }
    let program_id = hex32(&wire.program_id, id)?;
    let calldata = hex_bytes(&wire.calldata, id)?;
    let capabilities = hex_bytes(&wire.native_call.capabilities_hex, id)?;
    let access_declaration = hex_bytes(&wire.native_call.access_declaration_hex, id)?;
    let mut resources = [0_u64; 7];
    for (value, text) in resources.iter_mut().zip(&wire.native_call.resources) {
        *value = decimal_u64(text, id)?;
    }
    if decimal_u64(&wire.budget.fuel, id)? != resources[0] {
        return Err(malformed(id));
    }
    let payload = layerx_types::program_call::NativeProgramCall {
        program_id: layerx_types::intent::ProgramId::new(program_id),
        guest_abi: wire.native_call.guest_abi,
        entrypoint: wire.native_call.entrypoint.as_bytes(),
        calldata: &calldata,
        capabilities: &capabilities,
        access_declaration: &access_declaration,
        response_capacity: wire.native_call.response_capacity,
        resources: layerx_types::program_call::Resources(resources),
    }
    .encode()
    .map_err(|_| malformed(id))?;
    Ok(ProgramSimulationRequest::Native {
        program_id,
        payload,
        fee_limit: decimal_u128(&wire.budget.fee_limit, id)?,
        signed_activity: signed_activity(&wire.signed_activity, id)?,
    })
}

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

#[cfg(test)]
mod budget_create_wire_tests {
    use super::{
        budget_create_purpose_suffix, BudgetCreateWire, Canonical, BUDGET_CREATE_PURPOSE_TEXT_V1,
    };
    use crate::agent_rpc_dispatch::canonical_request_bytes;
    use crate::tenant::Operation;
    use layerx_agent_api::error::RequestId;
    use serde_json::{json, Map, Value};
    use sha2::{Digest, Sha256};

    fn body(purpose: Option<&str>) -> Value {
        let mut body = json!({
            "tenant": "tenant",
            "agent_did": "did:layerx:alice",
            "asset": "asset",
            "limit": "10",
            "enforcement": "DaemonLimit",
            "expiry": "100",
        });
        if let (Some(purpose), Some(object)) = (purpose, body.as_object_mut()) {
            object.insert("purpose".into(), Value::String(purpose.to_owned()));
        }
        body
    }

    #[test]
    fn purpose_label_is_optional_and_outside_the_canonical_body() {
        let hex_label = "ab".repeat(32);
        let absent = serde_json::from_value::<BudgetCreateWire>(body(None));
        let present = serde_json::from_value::<BudgetCreateWire>(body(Some(&hex_label)));
        assert!(matches!(&absent, Ok(wire) if wire.purpose().is_none()));
        assert!(matches!(&present, Ok(wire) if wire.purpose() == Some(hex_label.as_str())));
        let (Ok(absent), Ok(present)) = (absent, present) else {
            panic!("budget create wire did not decode");
        };
        let (Ok(absent), Ok(present)) = (
            absent.into_request(RequestId(1)),
            present.into_request(RequestId(1)),
        ) else {
            panic!("budget create wire did not convert");
        };
        assert_eq!(absent, present);
        assert_eq!(absent.request.canonical(), present.request.canonical());
    }

    #[test]
    fn unknown_budget_create_field_is_refused() {
        let mut unknown = body(Some("rent"));
        if let Some(object) = unknown.as_object_mut() {
            object.insert("label".into(), Value::String("rent".into()));
        }
        assert!(serde_json::from_value::<BudgetCreateWire>(unknown).is_err());
    }

    fn object(purpose: Option<&str>) -> Map<String, Value> {
        match body(purpose) {
            Value::Object(object) => object,
            _ => panic!("budget create body is not an object"),
        }
    }

    fn request_bytes(purpose: Option<&str>) -> Vec<u8> {
        match canonical_request_bytes(Operation::BudgetCreate, &object(purpose), RequestId(1)) {
            Ok(Some(bytes)) => bytes,
            _ => panic!("budget create request bytes were not derived"),
        }
    }

    fn body_digest(purpose: Option<&str>) -> [u8; 32] {
        let Ok(wire) = serde_json::from_value::<BudgetCreateWire>(body(purpose)) else {
            panic!("budget create wire did not decode");
        };
        let label = wire.purpose().map(str::to_owned);
        let Ok(typed) = wire.into_request(RequestId(1)) else {
            panic!("budget create wire did not convert");
        };
        crate::agent_rpc_adapters::budget_create_body_digest(&typed, label.as_deref())
    }

    fn unlabelled_vectors() -> (Vec<u8>, [u8; 32]) {
        let Ok(wire) = serde_json::from_value::<BudgetCreateWire>(body(None)) else {
            panic!("budget create wire did not decode");
        };
        let Ok(typed) = wire.into_request(RequestId(1)) else {
            panic!("budget create wire did not convert");
        };
        let mut bytes = Operation::BudgetCreate.name().as_bytes().to_vec();
        bytes.push(0);
        let Ok(json) = serde_json::to_vec(&typed.canonical()) else {
            panic!("canonical body did not serialize");
        };
        bytes.extend(json);
        let mut digest = Sha256::new();
        digest.update(b"LayerX/budget/create-body/v1\0");
        digest.update(typed.canonical().to_string().as_bytes());
        (bytes, digest.finalize().into())
    }

    #[test]
    fn absent_purpose_label_keeps_the_unlabelled_digests() {
        let (bytes, digest) = unlabelled_vectors();
        assert!(budget_create_purpose_suffix(None).is_empty());
        assert_eq!(request_bytes(None), bytes);
        assert_eq!(body_digest(None), digest);
    }

    #[test]
    fn present_purpose_label_is_bound_with_tag_length_and_text() {
        let (bytes, digest) = unlabelled_vectors();
        let mut suffix = BUDGET_CREATE_PURPOSE_TEXT_V1.to_vec();
        suffix.extend_from_slice(&4_u64.to_be_bytes());
        suffix.extend_from_slice(b"rent");
        assert_eq!(budget_create_purpose_suffix(Some("rent")), suffix);
        let mut labelled = bytes;
        labelled.extend_from_slice(&suffix);
        assert_eq!(request_bytes(Some("rent")), labelled);
        assert_ne!(body_digest(Some("rent")), digest);
    }

    #[test]
    fn different_purpose_labels_give_different_digests() {
        assert_ne!(request_bytes(Some("rent")), request_bytes(Some("food")));
        assert_ne!(body_digest(Some("rent")), body_digest(Some("food")));
        assert_ne!(request_bytes(Some("ab")), request_bytes(Some("abab")));
        assert_ne!(body_digest(Some("")), body_digest(None));
    }

    #[test]
    fn same_purpose_label_gives_equal_digests() {
        let hex_label = "ab".repeat(32);
        assert_eq!(request_bytes(Some(&hex_label)), request_bytes(Some(&hex_label)));
        assert_eq!(body_digest(Some(&hex_label)), body_digest(Some(&hex_label)));
    }

    #[test]
    fn replay_with_a_different_purpose_label_misses_the_cached_success() {
        use crate::idempotency::{EconomicResult, IdempotencyError, Outcome, RetentionPolicy, Store};
        let root = std::env::temp_dir().join(format!("lxp-budget-purpose-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&root);
        let (Ok(tenant), Ok(retention)) = (
            crate::store::TenantId::new("tenant-a"),
            RetentionPolicy::new(1_000, 500),
        ) else {
            panic!("tenant or retention refused");
        };
        let Ok(store) = Store::open(&root, tenant, retention) else {
            panic!("idempotency store did not open");
        };
        let key = [7; 32];
        let success = || {
            Ok(EconomicResult {
                response_bytes: b"created".to_vec(),
                receipt_ref: None,
            })
        };
        let first = store.execute(key, &request_bytes(Some("rent")), 1, |_| success());
        assert!(matches!(first, Ok(Outcome::First(_))));
        let same = store.execute(key, &request_bytes(Some("rent")), 1, |_| success());
        assert!(matches!(same, Ok(Outcome::RepeatedOriginal(_))));
        let other = store.execute(key, &request_bytes(Some("food")), 1, |_| success());
        assert!(matches!(other, Err(IdempotencyError::Conflict(_))));
        let unlabelled = store.execute(key, &request_bytes(None), 1, |_| success());
        assert!(matches!(unlabelled, Err(IdempotencyError::Conflict(_))));
        let _ = std::fs::remove_dir_all(&root);
    }
}


#[cfg(test)]
mod native_contract_tests {
    use super::{
        native_human_prepare, Canonical, NativeActivityV1Wire, NativePreparationPurposeV1Wire,
        NativeApprovalDecisionV1Wire, NativeApprovalGetV1Wire, NativeApprovalListV1Wire,
        NativeApprovalListResultV1Wire, NativeApprovalResultV1Wire,
        NativeLocalGrantConsentV1Wire, NativePrepareResultV1Wire, NativePrepareV1Wire,
        SignedNativePreparationPurposeV1Wire,
    };
    use layerx_agent_api::error::RequestId;
    use layerx_agent_api::identity::NativePreparationPurposeV1;
    use serde_json::{json, Value};

    fn purpose_body() -> Value {
        json!({
            "version": "1",
            "tenant": "tenant-a",
            "agent_did": "did:layerx:alice",
            "session_id": "11".repeat(32),
            "generation": "1",
            "expires_at_ms": "1000",
            "capability_id": "22".repeat(32),
            "preparation_id": "33".repeat(32),
            "canonical_digest": "44".repeat(32),
            "commitment": "55".repeat(32),
        })
    }

    fn purpose(value: Value) -> NativePreparationPurposeV1 {
        let Ok(wire) = serde_json::from_value::<NativePreparationPurposeV1Wire>(value) else {
            panic!("native preparation purpose did not decode");
        };
        let Ok(value) = wire.into_request(RequestId(1)) else {
            panic!("native preparation purpose did not validate");
        };
        value
    }

    #[test]
    fn native_activity_wire_preserves_modules_and_refuses_noncanonical_values() {
        let mut values = Vec::new();
        for module in ["1", "3"] {
            let body = json!({"version": "1", "module": module, "ordinal": "1"});
            let Ok(wire) = serde_json::from_value::<NativeActivityV1Wire>(body.clone()) else {
                panic!("native activity did not decode");
            };
            let Ok(activity) = wire.into_activity(RequestId(1)) else {
                panic!("native activity did not validate");
            };
            assert_eq!(activity.canonical(), body);
            values.push(activity);
        }
        assert_ne!(values[0], values[1]);
        assert_ne!(values[0].canonical(), values[1].canonical());
        for (field, value) in [
            ("version", "0"),
            ("version", "2"),
            ("version", "01"),
            ("module", "0"),
            ("module", "12"),
            ("module", "65536"),
            ("module", "01"),
            ("ordinal", "0"),
            ("ordinal", "65536"),
            ("ordinal", "+1"),
            ("ordinal", " 1"),
            ("ordinal", "1 "),
        ] {
            let mut body = json!({"version": "1", "module": "1", "ordinal": "1"});
            body[field] = Value::String(value.into());
            let Ok(wire) = serde_json::from_value::<NativeActivityV1Wire>(body) else {
                panic!("string wire shape did not decode");
            };
            assert!(wire.into_activity(RequestId(1)).is_err());
        }
        for body in [
            json!({"version": 1, "module": "1", "ordinal": "1"}),
            json!({"version": "1", "module": 1, "ordinal": "1"}),
            json!({"version": "1", "module": "1", "ordinal": 1}),
            json!({"version": "1", "module": "1", "ordinal": "1", "extra": "1"}),
        ] {
            assert!(serde_json::from_value::<NativeActivityV1Wire>(body).is_err());
        }
        assert!(serde_json::from_str::<NativeActivityV1Wire>(
            r#"{"version":"1","module":"1","ordinal":"1"} {}"#,
        ).is_err());
    }

    #[test]
    fn native_purpose_wire_binds_every_coordinate_and_explicit_commitment() {
        let body = purpose_body();
        let original = purpose(body.clone());
        assert_eq!(original.canonical(), body);
        assert_eq!(original.commitment, [0x55; 32]);
        for (field, value) in [
            ("tenant", "tenant-b".into()),
            ("agent_did", "did:layerx:bob".into()),
            ("session_id", "66".repeat(32)),
            ("generation", "2".into()),
            ("expires_at_ms", "1001".into()),
            ("capability_id", "77".repeat(32)),
            ("preparation_id", "88".repeat(32)),
            ("canonical_digest", "99".repeat(32)),
            ("commitment", "aa".repeat(32)),
        ] {
            let mut changed = body.clone();
            changed[field] = Value::String(value);
            let changed = purpose(changed);
            assert_ne!(changed.canonical(), original.canonical());
            assert_ne!(changed.canonical_bytes(), original.canonical_bytes());
        }
    }

    fn signed_purpose_body() -> Value {
        json!({
            "purpose": purpose_body(),
            "owner_public_key": "66".repeat(32),
            "signature": "77".repeat(64),
        })
    }

    #[test]
    fn signed_native_purpose_wire_preserves_structural_signature_bytes() {
        let body = signed_purpose_body();
        let Ok(wire) = serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(body.clone()) else {
            panic!("signed purpose wire did not decode");
        };
        let Ok(signed) = wire.into_request(RequestId(1)) else {
            panic!("signed purpose contract did not validate");
        };
        assert_eq!(signed.canonical(), body);
        assert_eq!(signed.owner_public_key, [0x66; 32]);
        assert_eq!(signed.signature, [0x77; 64]);
    }

    #[test]
    fn signed_native_purpose_wire_refuses_key_and_signature_encoding_errors() {
        for (field, value) in [
            ("owner_public_key", "66".repeat(31)),
            ("owner_public_key", "66".repeat(33)),
            ("owner_public_key", "AA".repeat(32)),
            ("owner_public_key", "zz".repeat(32)),
            ("signature", "77".repeat(63)),
            ("signature", "77".repeat(65)),
            ("signature", "AA".repeat(64)),
            ("signature", "zz".repeat(64)),
            ("signature", "7".repeat(127)),
            ("signature", String::new()),
        ] {
            let mut body = signed_purpose_body();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(body) else {
                panic!("string signed purpose shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        let mut zero_expiry = signed_purpose_body();
        zero_expiry["purpose"]["expires_at_ms"] = Value::String("0".into());
        let Ok(wire) = serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(zero_expiry) else {
            panic!("zero expiry signed purpose shape did not decode");
        };
        assert!(wire.into_request(RequestId(1)).is_err());
    }

    #[test]
    fn signed_native_purpose_wire_refuses_unknown_missing_and_nonstring_fields() {
        let mut outer_unknown = signed_purpose_body();
        outer_unknown["extra"] = json!(1);
        let mut inner_unknown = signed_purpose_body();
        inner_unknown["purpose"]["extra"] = json!(1);
        for body in [outer_unknown, inner_unknown] {
            assert!(serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(body).is_err());
        }
        for field in ["purpose", "owner_public_key", "signature"] {
            let Value::Object(mut missing) = signed_purpose_body() else {
                panic!("signed purpose body is not an object");
            };
            missing.remove(field);
            assert!(serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(
                Value::Object(missing),
            ).is_err());
        }
        for field in ["owner_public_key", "signature"] {
            let mut numeric = signed_purpose_body();
            numeric[field] = json!(1);
            assert!(serde_json::from_value::<SignedNativePreparationPurposeV1Wire>(numeric).is_err());
        }
    }

    fn native_prepare_body() -> Value {
        json!({
            "variant": "native_v1",
            "activity": {"version": "1", "module": "9", "ordinal": "1"},
            "actor": "did:layerx:alice",
            "authority": "owner-primary",
            "account_sequence": "4",
            "not_before": "10",
            "not_after": "20",
            "idempotency_key": "88".repeat(32),
            "fee_limit": "123",
            "payload": "0102",
            "payload_hash": "99".repeat(32),
            "capability_id": "22".repeat(32),
            "purpose": signed_purpose_body(),
            "local_grant": null,
        })
    }

    #[test]
    fn native_prepare_wire_preserves_native_module_in_real_human_request() {
        let body = native_prepare_body();
        let Ok(wire) = serde_json::from_value::<NativePrepareV1Wire>(body.clone()) else {
            panic!("native prepare wire did not decode");
        };
        let Ok(request) = wire.into_request(RequestId(1)) else {
            panic!("native prepare contract did not validate");
        };
        assert_eq!(request.canonical(), body);
        let Ok(human) = native_human_prepare(&request, RequestId(1)) else {
            panic!("native prepare did not convert to HumanPrepare");
        };
        assert_eq!(human.activity_type, 0x0009_0001);
        assert_eq!(human.actor, "did:layerx:alice");
        assert_eq!(human.authority, "owner-primary");
        assert_eq!(human.account_sequence, 4);
        assert_eq!((human.not_before, human.not_after), (10, 20));
        assert_eq!(human.idempotency_key, "88".repeat(32));
        assert_eq!(human.fee_limit, 123);
        assert_eq!(human.payload, [1, 2]);
        assert_eq!(human.payload_hash, [0x99; 32]);
        assert_eq!(human.capability_id, Some([0x22; 32]));
        let mut invalid = request;
        invalid.actor = match layerx_agent_api::identity::AgentDid::new("did:layerx:bob") {
            Ok(actor) => actor,
            Err(_) => panic!("bounded actor did not construct"),
        };
        assert!(native_human_prepare(&invalid, RequestId(1)).is_err());
    }

    #[test]
    fn native_prepare_wire_refuses_bounds_mismatches_and_noncanonical_values() {
        for (field, value) in [
            ("variant", "native_v2".into()),
            ("actor", "did:layerx:bob".into()),
            ("authority", String::new()),
            ("authority", "a".repeat(layerx_types::limits::MAX_AUTHORITY_BYTES + 1)),
            ("account_sequence", "04".into()),
            ("not_before", "21".into()),
            ("not_after", " 20".into()),
            ("idempotency_key", "88".repeat(31)),
            ("fee_limit", "340282366920938463463374607431768211456".into()),
            ("payload", String::new()),
            ("payload", "AA".into()),
            ("payload", "0".into()),
            ("payload", "00".repeat(layerx_types::limits::MAX_PAYLOAD_BYTES + 1)),
            ("payload_hash", "99".repeat(33)),
            ("capability_id", "33".repeat(32)),
            ("capability_id", "AA".repeat(32)),
        ] {
            let mut body = native_prepare_body();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativePrepareV1Wire>(body) else {
                panic!("string native prepare shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        for module in ["1", "12"] {
            let mut body = native_prepare_body();
            body["activity"]["module"] = Value::String(module.into());
            let Ok(wire) = serde_json::from_value::<NativePrepareV1Wire>(body) else {
                panic!("native module shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        let mut unknown = native_prepare_body();
        unknown["activity_type"] = json!("1");
        assert!(serde_json::from_value::<NativePrepareV1Wire>(unknown).is_err());
        let mut numeric = native_prepare_body();
        numeric["account_sequence"] = json!(4);
        assert!(serde_json::from_value::<NativePrepareV1Wire>(numeric).is_err());
        let Value::Object(mut missing) = native_prepare_body() else {
            panic!("native prepare body is not an object");
        };
        missing.remove("variant");
        assert!(serde_json::from_value::<NativePrepareV1Wire>(Value::Object(missing)).is_err());
    }

    fn prepare_result_body() -> Value {
        json!({
            "version": "1",
            "preparation_id": "11".repeat(32),
            "canonical_bytes": "0102",
            "signing_preimage": "22".repeat(32),
            "activity": {"version": "1", "module": "9", "ordinal": "1"},
            "approval_required": true,
            "approval_id": "33".repeat(32),
        })
    }

    fn approval_result_body() -> Value {
        json!({
            "version": "1",
            "approval_id": "33".repeat(32),
            "held_digest": "22".repeat(32),
            "activity": {"version": "1", "module": "9", "ordinal": "1"},
            "state": "Awaiting",
            "submission_ref": null,
        })
    }

    #[test]
    fn native_approval_requests_preserve_all_fields_and_refuse_bad_discriminants() {
        let list = json!({"variant": "native_v1"});
        let Ok(wire) = serde_json::from_value::<NativeApprovalListV1Wire>(list.clone()) else {
            panic!("approval list did not decode");
        };
        let Ok(request) = wire.into_request(RequestId(1)) else { panic!("approval list refused"); };
        assert_eq!(request.canonical(), list);
        let get = json!({"variant": "native_v1", "approval_id": "11".repeat(32)});
        let Ok(wire) = serde_json::from_value::<NativeApprovalGetV1Wire>(get.clone()) else {
            panic!("approval get did not decode");
        };
        let Ok(request) = wire.into_request(RequestId(1)) else { panic!("approval get refused"); };
        assert_eq!(request.canonical(), get);
        let decision = json!({
            "variant": "native_v1", "approval_id": "11".repeat(32),
            "held_digest": "22".repeat(32), "current_sequence": "3",
        });
        let Ok(wire) = serde_json::from_value::<NativeApprovalDecisionV1Wire>(decision.clone()) else {
            panic!("approval decision did not decode");
        };
        let Ok(request) = wire.into_request(RequestId(1)) else { panic!("approval decision refused"); };
        assert_eq!(request.canonical(), decision);
        for (field, value) in [
            ("variant", "native_v2".into()), ("approval_id", "AA".repeat(32)),
            ("held_digest", "22".repeat(31)), ("current_sequence", "03".into()),
        ] {
            let mut body = decision.clone();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativeApprovalDecisionV1Wire>(body) else {
                panic!("decision string shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        assert!(serde_json::from_value::<NativeApprovalListV1Wire>(json!({"variant":"native_v1","extra":1})).is_err());
        assert!(serde_json::from_value::<NativeApprovalGetV1Wire>(json!({"variant":"native_v1"})).is_err());
    }

    #[test]
    fn native_results_round_trip_typed_values_and_exact_owner_states() {
        for required in [true, false] {
            let mut body = prepare_result_body();
            body["approval_required"] = json!(required);
            if !required { body["approval_id"] = Value::Null; }
            let Ok(wire) = serde_json::from_value::<NativePrepareResultV1Wire>(body.clone()) else {
                panic!("prepare result did not decode");
            };
            let Ok(result) = wire.into_result(RequestId(1)) else { panic!("prepare result refused"); };
            assert_eq!(result.canonical(), body);
            assert_eq!(result.activity.module, 9);
        }
        for state in ["Awaiting", "Granted", "Rejected", "Expired", "Defective", "NotRequired"] {
            let mut body = approval_result_body();
            body["state"] = json!(state);
            let Ok(wire) = serde_json::from_value::<NativeApprovalResultV1Wire>(body.clone()) else {
                panic!("approval result did not decode");
            };
            let Ok(result) = wire.into_result(RequestId(1)) else { panic!("approval result refused"); };
            assert_eq!(result.canonical(), body);
        }
        let list = json!({"version":"1", "approvals":[approval_result_body()]});
        let Ok(wire) = serde_json::from_value::<NativeApprovalListResultV1Wire>(list.clone()) else {
            panic!("approval results did not decode");
        };
        let Ok(result) = wire.into_result(RequestId(1)) else { panic!("approval results refused"); };
        assert_eq!(result.canonical(), list);
    }

    #[test]
    fn native_prepare_result_refuses_version_hex_bounds_and_approval_inconsistency() {
        for (field, value) in [
            ("version", "2".into()), ("version", "01".into()),
            ("preparation_id", "11".repeat(31)), ("signing_preimage", "AA".repeat(32)),
            ("canonical_bytes", String::new()), ("canonical_bytes", "a".into()),
            ("canonical_bytes", "00".repeat(layerx_wire::limits::MAX_MESSAGE_BYTES + 1)),
            ("approval_id", "33".repeat(33)),
        ] {
            let mut body = prepare_result_body();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativePrepareResultV1Wire>(body) else {
                panic!("prepare result string shape did not decode");
            };
            assert!(wire.into_result(RequestId(1)).is_err());
        }
        for (required, approval) in [(true, Value::Null), (false, json!("33".repeat(32)))] {
            let mut body = prepare_result_body();
            body["approval_required"] = json!(required);
            body["approval_id"] = approval;
            let Ok(wire) = serde_json::from_value::<NativePrepareResultV1Wire>(body) else {
                panic!("prepare result boolean shape did not decode");
            };
            assert!(wire.into_result(RequestId(1)).is_err());
        }
        for (field, value) in [("module", "12"), ("ordinal", "0"), ("version", "2")] {
            let mut body = prepare_result_body();
            body["activity"][field] = json!(value);
            let Ok(wire) = serde_json::from_value::<NativePrepareResultV1Wire>(body) else {
                panic!("prepare result activity shape did not decode");
            };
            assert!(wire.into_result(RequestId(1)).is_err());
        }
        let mut invalid_bool = prepare_result_body();
        invalid_bool["approval_required"] = json!("true");
        assert!(serde_json::from_value::<NativePrepareResultV1Wire>(invalid_bool).is_err());
        let mut extra = prepare_result_body();
        extra["extra"] = json!(1);
        assert!(serde_json::from_value::<NativePrepareResultV1Wire>(extra).is_err());
    }

    #[test]
    fn native_approval_result_refuses_unknown_state_bad_refs_and_oversized_list() {
        for (field, value) in [
            ("state", "granted".into()), ("version", "0".into()),
            ("held_digest", "AA".repeat(32)), ("submission_ref", "33".repeat(31)),
        ] {
            let mut body = approval_result_body();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativeApprovalResultV1Wire>(body) else {
                panic!("approval result string shape did not decode");
            };
            assert!(wire.into_result(RequestId(1)).is_err());
        }
        let too_many = json!({"version":"1", "approvals":vec![approval_result_body();101]});
        let Ok(wire) = serde_json::from_value::<NativeApprovalListResultV1Wire>(too_many) else {
            panic!("approval list shape did not decode");
        };
        assert!(wire.into_result(RequestId(1)).is_err());
        let mut unknown = approval_result_body();
        unknown["extra"] = json!(1);
        assert!(serde_json::from_value::<NativeApprovalResultV1Wire>(unknown).is_err());
    }

    #[test]
    fn native_local_grant_wire_refuses_expiry_encoding_and_record_bounds() {
        let original = json!({
            "version":"1", "capability":"01", "session_scope":"02",
            "expires_at_ms":"1000", "owner_public_key":"11".repeat(32), "signature":"22".repeat(64),
        });
        let Ok(wire) = serde_json::from_value::<NativeLocalGrantConsentV1Wire>(original.clone()) else {
            panic!("local consent shape did not decode");
        };
        let Ok(consent) = wire.into_request(RequestId(1)) else { panic!("local consent shape refused"); };
        assert_eq!(consent.canonical(), original);
        assert!(super::native_local_grant(&consent, RequestId(1)).is_err());
        for (field, value) in [
            ("version","2".into()), ("capability",String::new()), ("session_scope",String::new()),
            ("capability","00".repeat(layerx_wire::limits::MAX_MESSAGE_BYTES + 1)),
            ("session_scope","AA".into()), ("expires_at_ms","0".into()),
            ("expires_at_ms","01000".into()), ("owner_public_key","AA".repeat(32)),
            ("signature","22".repeat(63)),
        ] {
            let mut body = original.clone();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativeLocalGrantConsentV1Wire>(body) else {
                panic!("local consent string shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        let mut unknown = original;
        unknown["extra"] = json!(1);
        assert!(serde_json::from_value::<NativeLocalGrantConsentV1Wire>(unknown).is_err());
    }

    #[test]
    fn native_purpose_wire_refuses_unknown_missing_and_malformed_fields() {
        for (field, value) in [
            ("version", "2".into()),
            ("version", "01".into()),
            ("generation", "0".into()),
            ("generation", "01".into()),
            ("generation", "18446744073709551616".into()),
            ("expires_at_ms", "0".into()),
            ("expires_at_ms", "01000".into()),
            ("expires_at_ms", "+1000".into()),
            ("expires_at_ms", " 1000".into()),
            ("expires_at_ms", "1000 ".into()),
            ("expires_at_ms", "18446744073709551616".into()),
            ("tenant", "bad\0tenant".into()),
            ("session_id", "AA".repeat(32)),
            ("capability_id", "ab".into()),
            ("preparation_id", "AA".repeat(32)),
            ("canonical_digest", "44".repeat(31)),
            ("commitment", "rent".into()),
        ] {
            let mut body = purpose_body();
            body[field] = Value::String(value);
            let Ok(wire) = serde_json::from_value::<NativePreparationPurposeV1Wire>(body) else {
                panic!("string purpose shape did not decode");
            };
            assert!(wire.into_request(RequestId(1)).is_err());
        }
        let mut labelled = purpose_body();
        labelled["purpose"] = Value::String("rent".into());
        assert!(serde_json::from_value::<NativePreparationPurposeV1Wire>(labelled).is_err());
        let Value::Object(mut missing) = purpose_body() else {
            panic!("purpose body is not an object");
        };
        missing.remove("commitment");
        assert!(serde_json::from_value::<NativePreparationPurposeV1Wire>(Value::Object(missing)).is_err());
        for field in ["generation", "expires_at_ms"] {
            let mut numeric = purpose_body();
            numeric[field] = json!(1);
            assert!(serde_json::from_value::<NativePreparationPurposeV1Wire>(numeric).is_err());
        }
        let Value::Object(mut missing_expiry) = purpose_body() else {
            panic!("purpose body is not an object");
        };
        missing_expiry.remove("expires_at_ms");
        assert!(serde_json::from_value::<NativePreparationPurposeV1Wire>(
            Value::Object(missing_expiry),
        ).is_err());
    }
}
