use super::agent_runtime::AgentRuntime;
use crate::agents::AgentFailure;
use crate::auth::{OperationDigest, Passkeys, StepUpEvidence};
use crate::custody::{
    CustodySigner, KeyId, NativeConsent, NativeConsentRequest, Operation, SignAuthorization,
};
use crate::store::{PrincipalScope, RowKey, Table};
use crate::trace::TraceId;
use layerx_agent_api::identity::{
    NativeActivity, NativeLocalGrantConsentV1, NativePreparationPurposeV1,
};
use layerx_agent_api::prepare::PrepareRequest;
use layerx_agentd::capability::timed::{
    NativeSpendSourceV1, NativeTimedCapabilityV1, TimedCapability,
};
use layerx_agentd::identity::ProtocolAuthority;
use layerx_agentd::session::{NativeSessionScopeV1, SessionId};
use layerx_agentd::store::TenantId;
use layerx_crypto::authority_grant::NativeFeeBudget;
use layerx_types::ids::Did;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};

pub const NATIVE_SEND: u32 = 0x0001_0005;

pub fn access_operation(
    scope: &PrincipalScope<'_>,
    request: &NativeSendAccessRequest,
) -> Result<OperationDigest, AgentFailure> {
    access_operation_coordinates(scope.principal().as_str(), scope.tenant().as_str(), request)
}

pub fn access_operation_coordinates(
    principal: &str,
    tenant: &str,
    request: &NativeSendAccessRequest,
) -> Result<OperationDigest, AgentFailure> {
    use sha2::{Digest as _, Sha256};
    request.validate()?;
    let mut digest = Sha256::new();
    digest.update(b"LayerX/Human/SecuritySettings/native-Send-access/v1\0");
    for text in [
        tenant.as_bytes(),
        principal.as_bytes(),
        request.owner.as_bytes(),
    ] {
        let length = u32::try_from(text.len())
            .map_err(|_| AgentFailure::Refused("native access text exceeds bound"))?;
        digest.update(length.to_be_bytes());
        digest.update(text);
    }
    for bytes in [
        request.action_key,
        request.owner_public_key,
        request.counterparty,
        request.asset,
        request.commitment,
    ] {
        digest.update(bytes);
    }
    for value in [
        request.not_before,
        request.expires_at,
        request.rate_window_ms,
        request.maximum_uses,
    ] {
        digest.update(value.to_be_bytes());
    }
    digest.update(request.maximum_amount.to_be_bytes());
    match request.native_fee_budget {
        None => digest.update([0]),
        Some(fee) => {
            digest.update([1]);
            digest.update(fee.asset);
            for value in [
                fee.maximum_per_activity,
                fee.maximum_total,
                fee.maximum_per_period,
            ] {
                digest.update(value.to_be_bytes());
            }
            digest.update(fee.period_length.to_be_bytes());
            digest.update(fee.period_start.to_be_bytes());
        }
    }
    Ok(OperationDigest::new(digest.finalize().into()))
}

pub struct NativeSendAccessRequest {
    pub action_key: [u8; 32],
    pub owner: Did,
    pub owner_public_key: [u8; 32],
    pub not_before: u64,
    pub expires_at: u64,
    pub native_fee_budget: Option<NativeFeeBudget>,
    pub counterparty: [u8; 32],
    pub asset: [u8; 32],
    pub maximum_amount: u128,
    pub rate_window_ms: u64,
    pub maximum_uses: u64,
    pub commitment: [u8; 32],
}

impl NativeSendAccessRequest {
    pub fn validate(&self) -> Result<(), AgentFailure> {
        if self.action_key[..8] == [0; 8]
            || self.owner_public_key == [0; 32]
            || self.not_before >= self.expires_at
            || self.counterparty == [0; 32]
            || self.asset == [0; 32]
            || self.maximum_amount == 0
            || self.rate_window_ms == 0
            || self.maximum_uses == 0
            || self.commitment == [0; 32]
        {
            return Err(AgentFailure::Refused("invalid native Send access request"));
        }
        Ok(())
    }
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct HumanOwnerNativeSessionV1 {
    pub version: u8,
    pub tenant: String,
    pub principal: String,
    pub owner: String,
    pub owner_public_key: [u8; 32],
    pub session_id: [u8; 32],
    pub generation: u64,
    pub grant_id: [u8; 32],
    pub grant_receipt_digest: [u8; 32],
    pub grant_activity_id: [u8; 32],
    pub grant_sequence: u64,
    pub expires_at_ms: u64,
    token: [u8; 32],
}

impl HumanOwnerNativeSessionV1 {
    pub(crate) fn from_installed(
        scope: &PrincipalScope<'_>,
        request: &NativeSendAccessRequest,
        token: [u8; 32],
        generation: u64,
        grant_id: [u8; 32],
        evidence: super::agent_runtime::AgentFinalizationEvidence,
    ) -> Result<Self, AgentFailure> {
        if token == [0; 32]
            || generation == 0
            || grant_id == [0; 32]
            || evidence.activity_id == [0; 32]
            || evidence.receipt_digest == [0; 32]
            || evidence.observed_sequence == 0
            || !matches!(evidence.verification, 4 | 5)
            || evidence.action_key != request.action_key
        {
            return Err(AgentFailure::Refused("Human owner session proof differs"));
        }
        Ok(Self {
            version: 1,
            tenant: scope.tenant().as_str().to_owned(),
            principal: scope.principal().as_str().to_owned(),
            owner: std::str::from_utf8(request.owner.as_bytes())
                .map_err(|_| AgentFailure::Refused("invalid Human owner"))?
                .to_owned(),
            owner_public_key: request.owner_public_key,
            session_id: request.action_key,
            generation,
            grant_id,
            grant_receipt_digest: evidence.receipt_digest,
            grant_activity_id: evidence.activity_id,
            grant_sequence: evidence.observed_sequence,
            expires_at_ms: request
                .expires_at
                .checked_mul(1000)
                .ok_or(AgentFailure::Refused("session expiry overflow"))?,
            token,
        })
    }
    pub(crate) const fn credential(&self) -> &[u8; 32] {
        &self.token
    }
    pub fn validate(&self, scope: &PrincipalScope<'_>, now: u64) -> Result<(), AgentFailure> {
        if self.version != 1
            || self.principal != scope.principal().as_str()
            || self.tenant != scope.tenant().as_str()
            || self.token == [0; 32]
            || self.session_id == [0; 32]
            || self.generation == 0
            || self.grant_id == [0; 32]
            || self.grant_sequence == 0
            || self.grant_receipt_digest == [0; 32]
            || self.grant_activity_id == [0; 32]
            || self.owner_public_key == [0; 32]
            || now
                .checked_mul(1000)
                .is_none_or(|value| value >= self.expires_at_ms)
        {
            return Err(AgentFailure::Refused("Human owner session unavailable"));
        }
        Did::new(self.owner.as_bytes())
            .map_err(|_| AgentFailure::Refused("invalid Human owner"))?;
        Ok(())
    }
}
impl Drop for HumanOwnerNativeSessionV1 {
    fn drop(&mut self) {
        zeroize::Zeroize::zeroize(&mut self.token);
    }
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct HumanOwnerNativeContextV1 {
    pub version: u8,
    pub session: HumanOwnerNativeSessionV1,
    pub capability_id: [u8; 32],
    pub capability: Vec<u8>,
    pub session_scope: Vec<u8>,
    pub grant_signature: Vec<u8>,
    pub expires_at_ms: u64,
    pub consent_request_digest: [u8; 32],
    pub commitment: [u8; 32],
}
impl HumanOwnerNativeContextV1 {
    pub fn signed_grant(&self) -> Result<NativeLocalGrantConsentV1, AgentFailure> {
        let signature: [u8; 64] = self
            .grant_signature
            .as_slice()
            .try_into()
            .map_err(|_| AgentFailure::Refused("invalid retained grant signature"))?;
        if self.version != 1
            || signature == [0; 64]
            || self.capability_id == [0; 32]
            || self.expires_at_ms > self.session.expires_at_ms
        {
            return Err(AgentFailure::Refused("invalid retained native access"));
        }
        Ok(NativeLocalGrantConsentV1 {
            capability: self.capability.clone(),
            session_scope: self.session_scope.clone(),
            expires_at_ms: self.expires_at_ms,
            owner_public_key: self.session.owner_public_key,
            signature,
        })
    }
}

pub struct NativeSendOwnerCoordinatesV1 {
    pub actor: String,
    pub authority: String,
    pub generation: u64,
    pub head_sequence: u64,
    pub protocol_time_ms: u64,
    pub account_sequence: u64,
    pub expiry_sequence: u64,
    pub expiry_ms: u64,
    pub owner_public_key: [u8; 32],
    pub revocation_sequence: u64,
    pub native_fee_asset: [u8; 32],
}

pub struct NativeSendPreviewV1 {
    pub canonical_bytes: Vec<u8>,
    pub signing_preimage: Vec<u8>,
    pub purpose: NativePreparationPurposeV1,
    pub head_sequence: u64,
    pub protocol_time_ms: u64,
    pub owner_public_key: [u8; 32],
    pub revocation_sequence: u64,
}

fn key(id: [u8; 32]) -> Result<RowKey, AgentFailure> {
    let suffix: String = id.iter().map(|value| format!("{value:02x}")).collect();
    RowKey::new(format!("human-native-send-access-{suffix}"))
        .map_err(|_| AgentFailure::Refused("invalid native access key"))
}

pub fn unsigned_access(
    request: &NativeSendAccessRequest,
    session: &HumanOwnerNativeSessionV1,
    coordinates: &NativeSendOwnerCoordinatesV1,
) -> Result<NativeLocalGrantConsentV1, AgentFailure> {
    if request.action_key != session.session_id
        || request.owner.as_bytes() != session.owner.as_bytes()
        || request.owner_public_key != session.owner_public_key
        || coordinates.actor != session.owner
        || coordinates.owner_public_key != session.owner_public_key
        || coordinates.generation != session.generation
        || coordinates.authority != format!("session:{}", hex(session.grant_id))
        || coordinates.head_sequence < session.grant_sequence
        || coordinates.revocation_sequence == 0
        || coordinates.revocation_sequence > coordinates.head_sequence
        || coordinates.expiry_sequence <= coordinates.head_sequence
        || coordinates.protocol_time_ms >= session.expires_at_ms
        || coordinates.expiry_ms < session.expires_at_ms
        || request.counterparty == [0; 32]
        || request.asset == [0; 32]
        || request.maximum_amount == 0
        || request.rate_window_ms == 0
        || request.maximum_uses == 0
        || request.commitment == [0; 32]
    {
        return Err(AgentFailure::Refused("native access coordinates differ"));
    }
    let mut ceilings = BTreeMap::from([(request.asset, request.maximum_amount)]);
    if let Some(fee) = request.native_fee_budget {
        if fee.asset != coordinates.native_fee_asset {
            return Err(AgentFailure::Refused("native fee asset differs"));
        }
        let maximum = ceilings.entry(fee.asset).or_default();
        *maximum = maximum
            .checked_add(fee.maximum_per_activity)
            .ok_or(AgentFailure::Refused("native access amount overflow"))?;
    }
    let tenant = TenantId::new(session.tenant.clone())
        .map_err(|_| AgentFailure::Refused("invalid native tenant"))?;
    let activity = NativeActivity::new(1, 5)
        .map_err(|_| AgentFailure::Refused("invalid native Send activity"))?;
    let activities = BTreeSet::from([activity]);
    let capability = NativeTimedCapabilityV1 {
        record: TimedCapability {
            id: request.action_key,
            parent: None,
            tenant: tenant.clone(),
            agent: session.owner.clone(),
            authority: ProtocolAuthority::SessionKey(session.grant_id),
            activity_types: BTreeSet::new(),
            counterparties: BTreeSet::from([request.counterparty]),
            assets: ceilings.keys().copied().collect(),
            amount_ceilings: ceilings.clone(),
            rate_ceilings: BTreeMap::from([(request.rate_window_ms, request.maximum_uses)]),
            purposes: BTreeSet::new(),
            expiry_seconds: request.expires_at,
            grant_not_after_ms: session.expires_at_ms,
            created_at_ms: coordinates.protocol_time_ms,
            created_at_sequence: coordinates.head_sequence,
            revoked: None,
        },
        activities: activities.clone(),
        purpose_commitments: BTreeSet::from([request.commitment]),
        spend_ceilings: ceilings
            .into_iter()
            .map(|(asset, amount)| ((NativeSpendSourceV1::Principal, asset), amount))
            .collect(),
    }
    .encode()
    .map_err(|_| AgentFailure::Refused("invalid native capability"))?;
    let session_scope = NativeSessionScopeV1 {
        tenant,
        agent: Did::new(session.owner.as_bytes())
            .map_err(|_| AgentFailure::Refused("invalid native owner"))?,
        session_id: SessionId(session.session_id),
        generation: session.generation,
        permitted_activities: activities,
    }
    .encode()
    .map_err(|_| AgentFailure::Refused("invalid native session scope"))?;
    let grant = NativeLocalGrantConsentV1 {
        capability,
        session_scope,
        expires_at_ms: session.expires_at_ms,
        owner_public_key: session.owner_public_key,
        signature: [0; 64],
    };
    layerx_human_kms::attestor::validate_native_local_grant(&grant)
        .map_err(|_| AgentFailure::Refused("native grant encoding differs"))?;
    Ok(grant)
}

fn hex(value: [u8; 32]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}

#[allow(clippy::too_many_arguments)]
pub async fn authorize_access(
    scope: &mut PrincipalScope<'_>,
    runtime: &mut AgentRuntime,
    custody: &CustodySigner,
    passkeys: &Passkeys,
    authenticated: &StepUpEvidence,
    expected_auth_operation: OperationDigest,
    request_digest: [u8; 32],
    trace: &TraceId,
    request: &NativeSendAccessRequest,
    session: HumanOwnerNativeSessionV1,
    now: u64,
) -> Result<HumanOwnerNativeContextV1, AgentFailure> {
    request.validate()?;
    session.validate(scope, now)?;
    if expected_auth_operation != access_operation(scope, request)? {
        return Err(AgentFailure::Refused("native access operation differs"));
    }
    passkeys
        .revalidate_step_up(scope, authenticated, expected_auth_operation, now)
        .map_err(|_| {
            AgentFailure::Refused("native Send access needs fresh SecuritySettings consent")
        })?;
    let coordinates = runtime
        .native_send_owner_context(
            &session,
            u64::from_be_bytes(
                request.action_key[..8]
                    .try_into()
                    .map_err(|_| AgentFailure::Refused("invalid native action"))?,
            ),
        )
        .map_err(|_| AgentFailure::Unavailable)?;
    let mut grant = unsigned_access(request, &session, &coordinates)?;
    let disclosure = NativeConsent::LocalGrant(&grant)
        .digest()
        .map_err(|_| AgentFailure::Refused("invalid native consent"))?;
    let evidence = CustodySigner::bind_authenticated_step_up(
        passkeys,
        scope,
        authenticated,
        expected_auth_operation,
        Operation::SecuritySettings,
        disclosure,
        request_digest,
        now,
    )
    .map_err(|_| AgentFailure::Refused("native access consent differs"))?;
    let principal = scope.principal().clone();
    let custody_key = KeyId::new("human-primary")
        .map_err(|_| AgentFailure::Refused("invalid Human custody key"))?;
    let signed = custody
        .sign_native_consent_in_scope(
            scope,
            NativeConsentRequest::new(
                &principal,
                &custody_key,
                NativeConsent::LocalGrant(&grant),
                SignAuthorization::new(Operation::SecuritySettings, Some(&evidence)),
                now,
                trace.clone(),
            ),
        )
        .await
        .map_err(|_| AgentFailure::Refused("native access custody refused"))?;
    if signed.signer_public_key() != session.owner_public_key
        || signed.disclosure_digest() != disclosure
    {
        return Err(AgentFailure::Refused(
            "native access signature owner differs",
        ));
    }
    grant.signature = *signed.signature();
    let context = HumanOwnerNativeContextV1 {
        version: 1,
        capability_id: request.action_key,
        capability: grant.capability,
        session_scope: grant.session_scope,
        grant_signature: grant.signature.to_vec(),
        expires_at_ms: grant.expires_at_ms,
        consent_request_digest: request_digest,
        commitment: request.commitment,
        session,
    };
    let bytes = serde_json::to_vec(&context)
        .map_err(|_| AgentFailure::Refused("native access cannot persist"))?;
    let row_key = key(context.capability_id)?;
    if let Some(row) = scope.get(Table::Journeys, &row_key) {
        if row.bytes() != bytes.as_slice() {
            return Err(AgentFailure::Refused("native access changed on retry"));
        }
    } else {
        scope
            .put(Table::Journeys, row_key, now, bytes)
            .map_err(|_| AgentFailure::Unavailable)?;
    }
    Ok(context)
}

pub fn load_access(
    scope: &PrincipalScope<'_>,
    capability_id: [u8; 32],
    now: u64,
) -> Result<HumanOwnerNativeContextV1, AgentFailure> {
    let row_key = key(capability_id)?;
    let row = scope
        .get(Table::Journeys, &row_key)
        .ok_or(AgentFailure::Refused("native Send access not consented"))?;
    let context: HumanOwnerNativeContextV1 = serde_json::from_slice(row.bytes())
        .map_err(|_| AgentFailure::Refused("invalid retained native Send access"))?;
    context.session.validate(scope, now)?;
    if context.capability_id != capability_id {
        return Err(AgentFailure::Refused("native access binding differs"));
    }
    context.signed_grant()?;
    Ok(context)
}

pub fn preview_send(
    scope: &PrincipalScope<'_>,
    runtime: &mut AgentRuntime,
    request: &PrepareRequest,
    capability_id: [u8; 32],
    purpose_expires_ms: u64,
    commitment: [u8; 32],
    now: u64,
) -> Result<NativeSendPreviewV1, AgentFailure> {
    let context = load_access(scope, capability_id, now)?;
    if request.protocol_activity_type != NATIVE_SEND
        || request.actor.as_str() != context.session.owner
        || request.authority.as_str() != hex(context.session.owner_public_key)
        || commitment != context.commitment
        || purpose_expires_ms > context.expires_at_ms
    {
        return Err(AgentFailure::Refused("native Send preview binding differs"));
    }
    runtime
        .native_send_preview(&context, request, purpose_expires_ms, commitment)
        .map_err(|_| AgentFailure::Refused("native Send preview refused"))
}

pub struct NativeSendPreviewV2 {
    pub canonical_bytes: Vec<u8>,
    pub signing_preimage: Vec<u8>,
    pub purpose: layerx_agent_api::identity::NativeSendPurposeV1,
    pub head_sequence: u64,
    pub protocol_time_ms: u64,
    pub owner_public_key: [u8; 32],
    pub revocation_sequence: u64,
}

pub fn preview_send_v2(
    scope: &PrincipalScope<'_>,
    runtime: &mut AgentRuntime,
    request: &PrepareRequest,
    capability_id: [u8; 32],
    purpose_expires_ms: u64,
    now: u64,
) -> Result<NativeSendPreviewV2, AgentFailure> {
    let context = load_access(scope, capability_id, now)?;
    if request.protocol_activity_type != NATIVE_SEND
        || request.actor.as_str() != context.session.owner
        || request.authority.as_str() != hex(context.session.owner_public_key)
        || purpose_expires_ms > context.expires_at_ms
    {
        return Err(AgentFailure::Refused(
            "native Send statement binding differs",
        ));
    }
    runtime
        .native_send_preview_v2(&context, request, purpose_expires_ms, context.commitment)
        .map_err(|_| AgentFailure::Refused("native Send statement preview refused"))
}
