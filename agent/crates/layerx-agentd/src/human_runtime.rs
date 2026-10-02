//! Production operation owner for the authenticated Human-to-agent boundary.

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Arc, Mutex, MutexGuard, RwLock};

use layerx_client::evidence::{
    CheckpointSelector, EvidenceError, ProofBundleSelector, RootSelector,
};
use layerx_client::receipt::{Lookup, ReceiptSelector};
use layerx_client::submit::Submission;
use layerx_client::Client;
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::AuthorizedBatch;
use layerx_types::activity::{Authority, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleRegistry};
use layerx_types::result::Retriability;
use layerx_types::verify::VerificationLevel;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::time::Duration;

use crate::admin::{
    ActionPlan, AdminError, OperatorCommand, OperatorContext, Surface, ORDINARY_CLIENT_WRITE,
};
use crate::approval::{
    ApprovalDecision, ApprovalExpiry, ApprovalOutcome, ApprovalRecord, ApprovalService,
    ApprovalSubmissionQueue, DecisionKey, DecisionRequest,
};
use crate::budget::{
    budget_create_identity, budget_state_key, create_protocol_budget, BudgetCreationError,
    BudgetKind, BudgetLimiter, BudgetPipeline, BudgetRequest, CoreBudgetReceipt, LimitConfig,
    PersistedReceipt, ProtocolBudget, ProtocolBudgetRecord, ProtocolBudgetState, RestartAccounting,
    BUDGET_MODULE_ID,
};
use crate::capability::{
    assert_narrowing, Capability, CapabilityDimensions, CapabilityId, RateCeiling,
};
use crate::degraded::Controller;
use crate::human::{
    HumanCapabilityInstall, HumanFinalizationEvidence, HumanOperationError, HumanOperations,
    HumanOwnerInstall, HumanPeer, HumanPrepare, HumanResponse, HumanSubmit, MutationEnvelope,
};
use crate::identity::{self, CoreIdentity, IdentityError, IdentityResolver, ProtocolAuthority};
use crate::managed_agent::{self, ManagedAgent};
use crate::outbox::{
    Outbox, RecoveredOutbox, RecoveryError, RecoveryInputs, SubmissionState, SubmissionStatus,
};
use crate::policy::approval::{ApprovalRegistry, ApprovalState, ApproverId};
use crate::prepare::PreparationLifecycle;
use crate::prepare::{
    prepare_activity_for_protocol, CoreStateError, PreparationDefaults, PrepareRequest, Prepared,
    ProductionCorePreparationBoundary,
};
use crate::protocol_evidence::{EvidenceAuthority, RawStateEvidence};
use crate::receipt::{ReceiptEvidenceRecord, ReceiptMetadata};
use crate::session::{self, OpenRequest, SessionId, SessionRegistry};
use crate::session_control::SessionControl;

impl<A: HumanAuthorityBoundary> UnifiedAgentOwner<A> {
    pub(crate) fn rpc_sign(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::submit::SignRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        use crate::session_control::{AdmissionStage, WriteAdmission};
        let control = self.session_control.clone();
        let peer = context.peer();
        if peer.subject.is_none() {
            return Err(HumanOperationError::Refused);
        }
        let (tenant, agent) = binding_coordinates(context)?;
        let reference = request.preparation_ref.as_str().to_owned();
        let preparation_id = digest_from_hex(&reference).ok_or(HumanOperationError::Refused)?;
        let mut operations = self.lock_operations()?;
        let cached = operations
            .prepared
            .get(&(peer.tenant.clone(), peer.principal.clone(), reference))
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        let snapshot = core_preparation_snapshot(
            &mut operations.node,
            peer,
            cached.prepared.envelope.actor_did(),
        )?;
        drop(operations);
        let prepared = cached.prepared;
        let signature: [u8; 64] = request
            .signature
            .as_bytes()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let Authority::Owner(owner_key) = prepared.envelope.authority() else {
            return Err(HumanOperationError::Refused);
        };
        let signer_public_key: [u8; 32] = owner_key
            .as_ref()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let signed = attach_external_signature(&prepared, signature)
            .map_err(|_| HumanOperationError::Refused)?;
        let verified =
            verify_before_submit(&signed, &prepared, &signer_public_key, &cached.registry)
                .map_err(|_| HumanOperationError::Refused)?;
        let activity_id = verified.activity_id();
        let permit = context.permit();
        let record = permit
            .admit_write(
                &control,
                WriteAdmission {
                    stage: AdmissionStage::Sign,
                    preparation_id,
                    charge: None,
                    extensions: Vec::new(),
                    current_sequence: snapshot.observed_head_sequence,
                    core_time_ms: snapshot.protocol_timestamp,
                    planner: None,
                },
            )
            .map_err(rpc_commit_error)?;
        recheck_submit_binding(
            &prepared.disclosure,
            &control,
            &tenant,
            &agent,
            &record,
            snapshot.protocol_timestamp,
        )?;
        permit
            .transition_preparation(
                &control,
                preparation_id,
                crate::prepare::LifecycleState::Signing,
                snapshot.observed_head_sequence,
            )
            .map_err(rpc_commit_error)?;
        control
            .mark_signing(
                &permit.preparation_authorization().session.tenant,
                preparation_id,
            )
            .map_err(rpc_commit_error)?;
        permit
            .retain_signed_bytes(
                &control,
                preparation_id,
                verified.into_exact_bytes(),
                activity_id,
            )
            .map_err(rpc_commit_error)?;
        control
            .mark_signed(
                &permit.preparation_authorization().session.tenant,
                preparation_id,
            )
            .map_err(rpc_commit_error)?;
        Self::signed_projection(prepared.envelope.idempotency_key().bytes(), activity_id)
    }

    fn signed_projection(
        submission_id: [u8; 32],
        activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::<A>::observation(&SubmissionStatus {
            submission_id,
            state: SubmissionState::Signed,
            activity_id,
            evidence: None,
            transitions: Vec::new(),
        })
    }
}

/// Restores every persisted subscription tenant from the shared durable store at startup.
///
/// Restoration is all-or-nothing: a poisoned store lock or any tenant that fails to open
/// (corrupt or unmigratable records) fails construction instead of serving an empty view.
///
/// # Errors
/// Returns `HumanOperationError::Unavailable` when the store lock is poisoned or a persisted
/// subscription tenant cannot be restored.
fn restored_subscriptions(
    store: &Arc<Mutex<Store>>,
) -> Result<BTreeMap<TenantId, crate::events::subscription::Store>, HumanOperationError> {
    let tenants = store
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?
        .tenant_ids_for_kind(ObjectKind::Subscription);
    let mut restored = BTreeMap::new();
    for tenant in tenants {
        let subscriptions =
            crate::events::subscription::Store::open_shared(Arc::clone(store), tenant.clone())
                .map_err(|_| HumanOperationError::Unavailable)?;
        restored.insert(tenant, subscriptions);
    }
    Ok(restored)
}
use crate::session_keys::SessionKeyRegistry;
use crate::sign::{
    attach_external_signature, validate_issued_session, verify_before_submit,
    ProvisionedSessionKey, VerifiedSubmission,
};
use crate::store::{key, ObjectKind, StorageClass, Store, TenantId, TenantKey};

impl<A: HumanAuthorityBoundary> UnifiedAgentOwner<A> {
    /// The tenant capability derivation graph, restored under the owner lock from
    /// the shared durable store on first use; a tenant with no persisted graph
    /// starts empty.
    fn capability_graph(
        &mut self,
        tenant: &TenantId,
    ) -> Result<&mut crate::capability::CapabilityGraph, HumanOperationError> {
        if !self.capabilities.contains_key(tenant) {
            let restored = {
                let store = self
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                crate::capability::CapabilityGraph::restore(&store, tenant.clone())
                    .map_err(|_| HumanOperationError::Unavailable)?
            };
            self.capabilities.insert(
                tenant.clone(),
                restored.unwrap_or_else(|| crate::capability::CapabilityGraph::new(tenant.clone())),
            );
        }
        self.capabilities
            .get_mut(tenant)
            .ok_or(HumanOperationError::Unavailable)
    }
}
mod native_receipt;
mod subject;
use native_receipt::RetainedNativeOwner;

const MAX_RESPONSE: usize = 1_048_576;
const BUDGET_RECEIPT_POLL: Duration = Duration::from_millis(50);
const BUDGET_RECEIPT_ATTEMPTS: u32 = 200;

pub type BalanceContext = (
    [u8; 32],
    [u8; 32],
    String,
    String,
    u64,
    u64,
    SequencerAuthorization,
);

/// Independently authenticated authority data. Implementations must obtain
/// these values from the configured authority peer; no local/static authority
/// is accepted by this operation owner.
pub trait HumanAuthorityBoundary {
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn registry(&self, peer: &HumanPeer) -> Result<ModuleRegistry, CoreStateError>;
    /// # Errors
    /// Refuses subjects not bound by the configured provider and current native checkpoint.
    fn authorize_subject(&mut self, peer: &HumanPeer) -> Result<(), HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn authorized_batch(
        &mut self,
        peer: &HumanPeer,
        expected_activity: [u8; 32],
    ) -> Result<AuthorizedBatch, HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn balance_context(&mut self, peer: &HumanPeer) -> Result<BalanceContext, HumanOperationError>;
    /// # Errors
    /// Refuses receipt authority not bound to the caller's original signed activity.
    fn authorized_activity(
        &mut self,
        peer: &HumanPeer,
        activity: &[u8],
        expected: [u8; 32],
    ) -> Result<AuthorizedBatch, HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn core_identity(
        &mut self,
        peer: &HumanPeer,
        agent: &Did,
    ) -> Result<CoreIdentity, IdentityError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn lease_attestation(
        &mut self,
        peer: &HumanPeer,
    ) -> Result<CoreLeaseAttestation, HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn capability_scope(
        &mut self,
        peer: &HumanPeer,
        agent: &Did,
        authority_id: [u8; 32],
        action_key: [u8; 32],
        capability_id: [u8; 32],
    ) -> Result<CoreCapabilityScope, HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn budget_state(
        &mut self,
        peer: &HumanPeer,
        active_budget_id: [u8; 32],
    ) -> Result<CoreBudgetState, HumanOperationError>;
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    fn key_rotation_policy(
        &mut self,
        peer: &HumanPeer,
        did: &Did,
        recovery: bool,
    ) -> Result<CoreKeyPolicy, HumanOperationError>;
}
pub(crate) fn lxgs2_grant_window(
    summary: &[u8],
    capability_id: &[u8; 32],
    authority_id: &[u8; 32],
    action_key: &[u8; 32],
    expiry_sequence: u64,
) -> Result<(u64, u64), HumanOperationError> {
    if summary.len() != 209
        || summary[..5] != *b"LXGS2"
        || summary[5..37] != capability_id[..]
        || summary[69..101] != authority_id[..]
        || summary[101..133] != action_key[..]
    {
        return Err(HumanOperationError::Refused);
    }
    let u64_at = |offset: usize| {
        let mut bytes = [0_u8; 8];
        bytes.copy_from_slice(&summary[offset..offset + 8]);
        u64::from_be_bytes(bytes)
    };
    if u64_at(165) != expiry_sequence {
        return Err(HumanOperationError::Refused);
    }
    Ok((u64_at(185), u64_at(193)))
}

#[cfg(test)]
mod lxgs2_window_tests {
    use super::{lxgs2_grant_window, HumanOperationError};

    const CAPABILITY: [u8; 32] = [0x11; 32];
    const AUTHORITY: [u8; 32] = [0x22; 32];
    const ACTION: [u8; 32] = [0x33; 32];
    const EXPIRY: u64 = 0x0102_0304_0506_0708;
    const NOT_BEFORE: u64 = 1_790_000_000_000;
    const NOT_AFTER: u64 = 1_790_000_600_000;

    fn summary() -> Vec<u8> {
        let mut bytes = vec![0xA5_u8; 209];
        bytes[..5].copy_from_slice(b"LXGS2");
        bytes[5..37].copy_from_slice(&CAPABILITY);
        bytes[69..101].copy_from_slice(&AUTHORITY);
        bytes[101..133].copy_from_slice(&ACTION);
        bytes[165..173].copy_from_slice(&EXPIRY.to_be_bytes());
        bytes[185..193].copy_from_slice(&NOT_BEFORE.to_be_bytes());
        bytes[193..201].copy_from_slice(&NOT_AFTER.to_be_bytes());
        bytes
    }

    fn decode(bytes: &[u8]) -> Result<(u64, u64), HumanOperationError> {
        lxgs2_grant_window(bytes, &CAPABILITY, &AUTHORITY, &ACTION, EXPIRY)
    }

    #[test]
    fn lxgs2_window_decodes_documented_offsets() {
        assert_eq!(decode(&summary()), Ok((NOT_BEFORE, NOT_AFTER)));
    }

    #[test]
    fn lxgs2_window_refuses_wrong_length() {
        let mut short = summary();
        short.pop();
        assert_eq!(decode(&short), Err(HumanOperationError::Refused));
        let mut long = summary();
        long.push(0);
        assert_eq!(decode(&long), Err(HumanOperationError::Refused));
    }

    #[test]
    fn lxgs2_window_refuses_wrong_tag() {
        let mut bytes = summary();
        bytes[4] = b'1';
        assert_eq!(decode(&bytes), Err(HumanOperationError::Refused));
    }

    #[test]
    fn lxgs2_window_refuses_mismatched_capability_id() {
        let mut bytes = summary();
        bytes[36] ^= 1;
        assert_eq!(decode(&bytes), Err(HumanOperationError::Refused));
    }

    #[test]
    fn lxgs2_window_refuses_mismatched_authority_id() {
        let mut bytes = summary();
        bytes[69] ^= 1;
        assert_eq!(decode(&bytes), Err(HumanOperationError::Refused));
    }

    #[test]
    fn lxgs2_window_refuses_mismatched_action_key() {
        let mut bytes = summary();
        bytes[132] ^= 1;
        assert_eq!(decode(&bytes), Err(HumanOperationError::Refused));
    }

    #[test]
    fn lxgs2_window_refuses_mismatched_expiry_sequence() {
        assert_eq!(
            lxgs2_grant_window(&summary(), &CAPABILITY, &AUTHORITY, &ACTION, EXPIRY + 1),
            Err(HumanOperationError::Refused)
        );
    }
}

pub struct CoreCapabilityScope {
    pub scope: crate::capability::ProtocolScope,

    pub not_before_ms: u64,
    pub not_after_ms: u64,

    pub module_mask: u64,
    pub observed_sequence: u64,
    pub verification: u8,
    pub evidence_digest: [u8; 32],
}
pub struct CoreBudgetState {
    pub revocation_sequence: u64,
    pub observed_head_sequence: u64,
    pub verification: u8,
    pub evidence_digest: [u8; 32],
    pub receipt_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
    pub remaining: u128,
    pub asset: [u8; 32],
}
pub struct CoreKeyPolicy {
    pub policy_revision: u64,
    pub required_delay_seconds: u64,
    pub maximum_delay_seconds: u64,
    pub effective_sequence: u64,
    pub observed_head_sequence: u64,
    pub verification: u8,
    pub evidence_digest: [u8; 32],
    pub checkpoint_digest: [u8; 32],
    pub age_sequences: u64,
    pub maximum_age_sequences: u64,
}

/// Authenticated authority observation relating wall clock to the core clock.
/// Both anchors are required so conversion never assumes seconds equal blocks.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CoreLeaseAttestation {
    pub lower_unix_ms: u64,
    pub lower_sequence: u64,
    pub upper_unix_ms: u64,
    pub upper_sequence: u64,
    pub observed_head_sequence: u64,
    pub canonical_bytes: Vec<u8>,
}

impl CoreLeaseAttestation {
    fn map(&self, not_before: u64, not_after: u64) -> Result<(u64, u64), HumanOperationError> {
        let wall_span = self
            .upper_unix_ms
            .checked_sub(self.lower_unix_ms)
            .ok_or(HumanOperationError::Refused)?;
        let sequence_span = self
            .upper_sequence
            .checked_sub(self.lower_sequence)
            .ok_or(HumanOperationError::Refused)?;
        if wall_span == 0
            || sequence_span == 0
            || self.canonical_bytes.is_empty()
            || self.lower_sequence == 0
            || self.observed_head_sequence < self.lower_sequence
            || self.observed_head_sequence > self.upper_sequence
            || not_before < self.lower_unix_ms
            || not_after <= not_before
            || not_after > self.upper_unix_ms
        {
            return Err(HumanOperationError::Refused);
        }
        let lower_delta = not_before - self.lower_unix_ms;
        let upper_delta = not_after - self.lower_unix_ms;
        let first = self
            .lower_sequence
            .checked_add(
                lower_delta
                    .checked_mul(sequence_span)
                    .ok_or(HumanOperationError::Refused)?
                    / wall_span,
            )
            .ok_or(HumanOperationError::Refused)?;
        let upper_product = upper_delta
            .checked_mul(sequence_span)
            .ok_or(HumanOperationError::Refused)?;
        let last = self
            .lower_sequence
            .checked_add(
                upper_product
                    .checked_add(wall_span - 1)
                    .ok_or(HumanOperationError::Refused)?
                    / wall_span,
            )
            .ok_or(HumanOperationError::Refused)?;
        if last <= self.observed_head_sequence || last <= first {
            return Err(HumanOperationError::Refused);
        }
        Ok((first, last))
    }
}

/// TLS-authenticated independent authority replica. The endpoint is distinct
/// from the node LNI socket and every response is decoded into closed protocol
/// types before it can influence preparation or receipt verification.
pub struct RemoteHumanAuthority {
    agent: ureq::Agent,
    endpoint: String,
    bearer: String,
    maximum_response_bytes: usize,
}

impl RemoteHumanAuthority {
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    pub fn connect(
        endpoint: &str,
        bearer: String,
        deadline: Duration,
        maximum_response_bytes: usize,
        ca_der: &[u8],
    ) -> Result<Self, HumanOperationError> {
        let endpoint = endpoint.trim_end_matches('/');
        if !endpoint.starts_with("https://")
            || bearer.len() < 32
            || deadline.is_zero()
            || maximum_response_bytes == 0
            || maximum_response_bytes > MAX_RESPONSE
        {
            return Err(HumanOperationError::Refused);
        }
        let tls = crate::outbound_tls::private_ca(ca_der).ok_or(HumanOperationError::Refused)?;
        let config = ureq::Agent::config_builder()
            .tls_config(tls)
            .timeout_global(Some(deadline))
            .http_status_as_error(false)
            .build();
        Ok(Self {
            agent: config.into(),
            endpoint: endpoint.to_owned(),
            bearer,
            maximum_response_bytes,
        })
    }

    fn get(&self, path: &str) -> Result<Value, HumanOperationError> {
        let mut response = self
            .agent
            .get(format!("{}{path}", self.endpoint))
            .header("Authorization", &format!("Bearer {}", self.bearer))
            .call()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if !response.status().is_success() {
            return Err(HumanOperationError::Refused);
        }
        if response
            .body()
            .content_length()
            .is_some_and(|length| length > self.maximum_response_bytes as u64)
        {
            return Err(HumanOperationError::Refused);
        }
        let body = response
            .body_mut()
            .with_config()
            .limit(self.maximum_response_bytes.saturating_add(1) as u64)
            .read_to_string()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if body.len() > self.maximum_response_bytes {
            return Err(HumanOperationError::Refused);
        }
        serde_json::from_str(&body).map_err(|_| HumanOperationError::Refused)
    }

    fn registry_from(value: &Value) -> Result<ModuleRegistry, HumanOperationError> {
        let modules = value
            .get("modules")
            .and_then(Value::as_array)
            .ok_or(HumanOperationError::Refused)?;
        if modules.is_empty() || modules.len() > 32 {
            return Err(HumanOperationError::Refused);
        }
        let mut registrations = Vec::with_capacity(modules.len());
        for module in modules {
            let id = u16::try_from(
                module
                    .get("module_id")
                    .and_then(Value::as_u64)
                    .ok_or(HumanOperationError::Refused)?,
            )
            .map_err(|_| HumanOperationError::Refused)?;
            let module_id = layerx_types::payload::ModuleId::from_u16(id)
                .map_err(|_| HumanOperationError::Refused)?;
            let values = module
                .get("activity_types")
                .and_then(Value::as_array)
                .ok_or(HumanOperationError::Refused)?;
            let mut activities = Vec::with_capacity(values.len());
            for value in values {
                activities.push(
                    ActivityType::from_u32(
                        u32::try_from(value.as_u64().ok_or(HumanOperationError::Refused)?)
                            .map_err(|_| HumanOperationError::Refused)?,
                    )
                    .map_err(|_| HumanOperationError::Refused)?,
                );
            }
            registrations.push(
                layerx_types::payload::ModuleRegistration::new(module_id, &activities)
                    .map_err(|_| HumanOperationError::Refused)?,
            );
        }
        ModuleRegistry::new(&registrations).map_err(|_| HumanOperationError::Refused)
    }
}

impl HumanAuthorityBoundary for RemoteHumanAuthority {
    fn authorize_subject(&mut self, peer: &HumanPeer) -> Result<(), HumanOperationError> {
        subject::verify_context(
            peer,
            &self.get(&format!(
                "/v1/agent/subject-context?tenant={}&principal={}",
                query(peer.transport_tenant()),
                subject::principal_query(peer)
            ))?,
        )
    }
    fn authorized_activity(
        &mut self,
        peer: &HumanPeer,
        activity: &[u8],
        expected: [u8; 32],
    ) -> Result<AuthorizedBatch, HumanOperationError> {
        if peer.subject.is_none() {
            return self.authorized_batch(peer, expected);
        }
        let value = self.get(&format!(
            "/v1/agent/authorized-batch?tenant={}&principal={}&activity_id={}&signed_activity={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer),
            hex(&expected),
            hex(activity)
        ))?;
        Ok(AuthorizedBatch::new(
            hex_field(&value, "batch_id")?,
            hex_field(&value, "asset")?,
            hex_field(&value, "previous_state_root")?,
            hex_field(&value, "resulting_state_root")?,
            hex_field(&value, "sequencer_public_key")?,
        ))
    }
    fn registry(&self, peer: &HumanPeer) -> Result<ModuleRegistry, CoreStateError> {
        self.get(&format!(
            "/v1/agent/registry?tenant={}&principal={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer)
        ))
        .and_then(|value| Self::registry_from(&value))
        .map_err(|error| match error {
            HumanOperationError::Unavailable => CoreStateError::Unavailable,
            HumanOperationError::Refused
            | HumanOperationError::CapabilityRefused(_)
            | HumanOperationError::Typed(_) => {
                CoreStateError::Unverified
            }
        })
    }
    fn authorized_batch(
        &mut self,
        peer: &HumanPeer,
        expected_activity: [u8; 32],
    ) -> Result<AuthorizedBatch, HumanOperationError> {
        let value = self.get(&format!(
            "/v1/agent/authorized-batch?tenant={}&principal={}&activity_id={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer),
            hex(&expected_activity)
        ))?;
        Ok(AuthorizedBatch::new(
            hex_field(&value, "batch_id")?,
            hex_field(&value, "asset")?,
            hex_field(&value, "previous_state_root")?,
            hex_field(&value, "resulting_state_root")?,
            hex_field(&value, "sequencer_public_key")?,
        ))
    }
    fn balance_context(
        &mut self,
        peer: &HumanPeer,
    ) -> Result<
        (
            [u8; 32],
            [u8; 32],
            String,
            String,
            u64,
            u64,
            SequencerAuthorization,
        ),
        HumanOperationError,
    > {
        let value = self.get(&format!(
            "/v1/agent/balance-context?tenant={}&principal={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer)
        ))?;
        let currency = value
            .get("currency")
            .and_then(Value::as_str)
            .filter(|value| !value.is_empty() && value.len() <= 32)
            .ok_or(HumanOperationError::Refused)?
            .to_owned();
        let observed_at = value
            .get("observed_at")
            .and_then(Value::as_str)
            .filter(|value| !value.is_empty() && value.len() <= 64)
            .ok_or(HumanOperationError::Refused)?
            .to_owned();
        Ok((
            hex_field(&value, "account_id")?,
            hex_field(&value, "asset_id")?,
            currency,
            observed_at,
            u64_field(&value, "age_seconds")?,
            u64_field(&value, "maximum_age_seconds")?,
            SequencerAuthorization::new(
                hex_field(&value, "sequencer_id")?,
                hex_field(&value, "sequencer_public_key")?,
                u64_field(&value, "first_batch_number")?,
                u64_field(&value, "last_batch_number")?,
            ),
        ))
    }
    fn core_identity(
        &mut self,
        peer: &HumanPeer,
        agent: &Did,
    ) -> Result<CoreIdentity, IdentityError> {
        let did = std::str::from_utf8(agent.as_bytes()).map_err(|_| IdentityError::Unverified)?;
        let value = self
            .get(&format!(
                "/v1/agent/identity?tenant={}&principal={}&did={}",
                query(peer.transport_tenant()),
                subject::principal_query(peer),
                query(did)
            ))
            .map_err(map_identity)?;
        let authorities = value
            .get("authorities")
            .and_then(Value::as_array)
            .ok_or(IdentityError::Unverified)?
            .iter()
            .map(|entry| {
                let id = entry
                    .get("id")
                    .and_then(Value::as_str)
                    .and_then(digest_from_hex)
                    .ok_or(IdentityError::Unverified)?;
                match entry.get("kind").and_then(Value::as_str) {
                    Some("primary_key") => Ok(ProtocolAuthority::PrimaryKey(id)),
                    Some("session_key") => Ok(ProtocolAuthority::SessionKey(id)),
                    Some("capability_grant") => Ok(ProtocolAuthority::CapabilityGrant(id)),
                    _ => Err(IdentityError::Unverified),
                }
            })
            .collect::<Result<Vec<_>, _>>()?;
        if authorities.is_empty() {
            return Err(IdentityError::Unverified);
        }
        let canonical = value
            .get("canonical_core_bytes")
            .and_then(Value::as_str)
            .and_then(decode_hex)
            .ok_or(IdentityError::Unverified)?;
        Ok(CoreIdentity {
            canonical_bytes: canonical,
            head_sequence: u64_field(&value, "head_sequence")
                .map_err(|_| IdentityError::Unverified)?,
            revocation_sequence: u64_field(&value, "revocation_sequence")
                .map_err(|_| IdentityError::Unverified)?,
            verification_level: verification_level(&value)?,
            frozen: value
                .get("frozen")
                .and_then(Value::as_bool)
                .ok_or(IdentityError::Unverified)?,
            authorities,
        })
    }
    fn lease_attestation(
        &mut self,
        peer: &HumanPeer,
    ) -> Result<CoreLeaseAttestation, HumanOperationError> {
        let value = self.get(&format!(
            "/v1/agent/core-clock?tenant={}&principal={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer)
        ))?;
        Ok(CoreLeaseAttestation {
            lower_unix_ms: u64_field(&value, "lower_unix_ms")?,
            lower_sequence: u64_field(&value, "lower_sequence")?,
            upper_unix_ms: u64_field(&value, "upper_unix_ms")?,
            upper_sequence: u64_field(&value, "upper_sequence")?,
            observed_head_sequence: u64_field(&value, "observed_head_sequence")?,
            canonical_bytes: value
                .get("canonical_attestation")
                .and_then(Value::as_str)
                .and_then(decode_hex)
                .filter(|v| !v.is_empty())
                .ok_or(HumanOperationError::Refused)?,
        })
    }
    fn capability_scope(
        &mut self,
        peer: &HumanPeer,
        agent: &Did,
        authority_id: [u8; 32],
        action_key: [u8; 32],
        capability_id: [u8; 32],
    ) -> Result<CoreCapabilityScope, HumanOperationError> {
        let did =
            std::str::from_utf8(agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let value = self.get(&format!("/v1/agent/capability-scope?tenant={}&principal={}&did={}&authority={}&action_key={}&capability_id={}", query(peer.transport_tenant()), subject::principal_query(peer), query(did), hex(&authority_id), hex(&action_key), hex(&capability_id)))?;
        let set16 = |name| -> Result<std::collections::BTreeSet<u16>, HumanOperationError> {
            value
                .get(name)
                .and_then(Value::as_array)
                .ok_or(HumanOperationError::Refused)?
                .iter()
                .map(|v| {
                    u16::try_from(v.as_u64().ok_or(HumanOperationError::Refused)?)
                        .map_err(|_| HumanOperationError::Refused)
                })
                .collect()
        };
        let set32 = |name| -> Result<std::collections::BTreeSet<[u8; 32]>, HumanOperationError> {
            value
                .get(name)
                .and_then(Value::as_array)
                .ok_or(HumanOperationError::Refused)?
                .iter()
                .map(|v| {
                    v.as_str()
                        .and_then(digest_from_hex)
                        .ok_or(HumanOperationError::Refused)
                })
                .collect()
        };
        let summary = value
            .get("canonical_core_bytes")
            .and_then(Value::as_str)
            .and_then(decode_hex)
            .ok_or(HumanOperationError::Refused)?;
        let (not_before_ms, not_after_ms) = lxgs2_grant_window(
            &summary,
            &capability_id,
            &authority_id,
            &action_key,
            u64_field(&value, "expiry_sequence")?,
        )?;

        let enforceable_dimensions = value
            .get("enforceable_dimensions")
            .and_then(Value::as_array)
            .ok_or(HumanOperationError::Refused)?
            .iter()
            .map(|v| match v.as_str() {
                Some("activity_type") => Ok(crate::capability::Dimension::ActivityType),
                Some("counterparty") => Ok(crate::capability::Dimension::Counterparty),
                Some("asset") => Ok(crate::capability::Dimension::Asset),
                Some("amount") => Ok(crate::capability::Dimension::Amount),
                Some("rate") => Ok(crate::capability::Dimension::Rate),
                Some("purpose") => Ok(crate::capability::Dimension::Purpose),
                Some("expiry") => Ok(crate::capability::Dimension::Expiry),
                _ => Err(HumanOperationError::Refused),
            })
            .collect::<Result<_, _>>()?;
        Ok(CoreCapabilityScope {
            scope: crate::capability::ProtocolScope {
                activity_types: set16("activity_types")?,
                counterparties: set32("counterparties")?,
                assets: set32("assets")?,
                amount_ceiling: value
                    .get("amount_ceiling")
                    .and_then(Value::as_str)
                    .ok_or(HumanOperationError::Refused)?
                    .parse()
                    .map_err(|_| HumanOperationError::Refused)?,
                expires_at_sequence: u64_field(&value, "expiry_sequence")?,

                not_before_ms,
                not_after_ms,
                enforceable_dimensions,
            },
            observed_sequence: u64_field(&value, "observed_sequence")?,
            module_mask: crate::capability::binding::lxgs2_module_mask(&summary)
                .map_err(|error| binding_refusal(&error))?,

            not_before_ms,
            not_after_ms,
            verification: u8::try_from(u64_field(&value, "verification")?)
                .map_err(|_| HumanOperationError::Refused)?,
            evidence_digest: hex_field(&value, "evidence_digest")?,
        })
    }
    fn budget_state(
        &mut self,
        peer: &HumanPeer,
        active_budget_id: [u8; 32],
    ) -> Result<CoreBudgetState, HumanOperationError> {
        let value = self.get(&format!(
            "/v1/agent/budget-state?tenant={}&principal={}&budget_id={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer),
            hex(&active_budget_id)
        ))?;
        let state = CoreBudgetState {
            revocation_sequence: u64_field(&value, "revocation_sequence")?,
            observed_head_sequence: u64_field(&value, "observed_head_sequence")?,
            verification: u8::try_from(u64_field(&value, "verification")?)
                .map_err(|_| HumanOperationError::Refused)?,
            evidence_digest: hex_field(&value, "evidence_digest")?,
            receipt_digest: hex_field(&value, "receipt_digest")?,
            checkpoint_digest: hex_field(&value, "checkpoint_digest")?,
            age_sequences: u64_field(&value, "age_sequences")?,
            maximum_age_sequences: u64_field(&value, "maximum_age_sequences")?,
            remaining: value
                .get("remaining")
                .and_then(Value::as_str)
                .ok_or(HumanOperationError::Refused)?
                .parse()
                .map_err(|_| HumanOperationError::Refused)?,
            asset: hex_field(&value, "asset")?,
        };
        if state.revocation_sequence == 0
            || state.observed_head_sequence < state.revocation_sequence
            || state.verification < 4
            || state.verification > 5
            || state.evidence_digest == [0; 32]
            || state.receipt_digest == [0; 32]
            || state.checkpoint_digest == [0; 32]
            || state.maximum_age_sequences == 0
            || state.age_sequences > state.maximum_age_sequences
            || state.asset == [0; 32]
        {
            return Err(HumanOperationError::Refused);
        }
        Ok(state)
    }
    fn key_rotation_policy(
        &mut self,
        peer: &HumanPeer,
        did: &Did,
        recovery: bool,
    ) -> Result<CoreKeyPolicy, HumanOperationError> {
        let did = std::str::from_utf8(did.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let value = self.get(&format!(
            "/v1/agent/key-policy?tenant={}&principal={}&did={}&recovery={}",
            query(peer.transport_tenant()),
            subject::principal_query(peer),
            query(did),
            recovery
        ))?;
        let state = CoreKeyPolicy {
            policy_revision: u64_field(&value, "policy_revision")?,
            required_delay_seconds: u64_field(&value, "required_delay_seconds")?,
            maximum_delay_seconds: u64_field(&value, "maximum_delay_seconds")?,
            effective_sequence: u64_field(&value, "effective_sequence")?,
            observed_head_sequence: u64_field(&value, "observed_head_sequence")?,
            verification: u8::try_from(u64_field(&value, "verification")?)
                .map_err(|_| HumanOperationError::Refused)?,
            evidence_digest: hex_field(&value, "evidence_digest")?,
            checkpoint_digest: hex_field(&value, "checkpoint_digest")?,
            age_sequences: u64_field(&value, "age_sequences")?,
            maximum_age_sequences: u64_field(&value, "maximum_age_sequences")?,
        };
        if state.policy_revision == 0
            || state.required_delay_seconds == 0
            || state.maximum_delay_seconds < state.required_delay_seconds
            || state.effective_sequence == 0
            || state.observed_head_sequence < state.effective_sequence
            || state.verification < 4
            || state.verification > 5
            || state.evidence_digest == [0; 32]
            || state.checkpoint_digest == [0; 32]
            || state.maximum_age_sequences == 0
            || state.age_sequences > state.maximum_age_sequences
        {
            return Err(HumanOperationError::Refused);
        }
        Ok(state)
    }
}

/// Concrete production path. Prepared bytes remain key-free; externally
/// supplied signatures are attached, reverified, durably queued, and only then
/// submitted through the sole frozen LNI client.
pub struct ProductionHumanOperations<A> {
    authority: A,
    node: Client,
    store: Arc<Mutex<Store>>,
    outboxes: BTreeMap<String, Outbox>,
    prepared: BTreeMap<(String, String, String), CachedPreparation>,
    submissions: BTreeMap<(String, String, String), [u8; 32]>,
    maximum_payload_bytes: usize,
    timestamp_span: u64,
    last_verified_receipt: Option<([u8; 32], i32, u64)>,
    unified_owner_active: bool,
    write_admission: BTreeMap<String, Result<(), RecoveryRefusal>>,

    clock: Arc<dyn layerx_types::clock::Clock>,
    subscriptions: BTreeMap<TenantId, crate::events::subscription::Store>,
    session_control: Option<SessionControl>,

    budget_limiter: Option<Arc<BudgetLimiter>>,

    export_trust: Option<(crate::export::ExportTrustSource, Duration)>,

    policies: Option<crate::policy::TenantPolicyRegistries>,
}

/// Why one tenant stays read-only after startup recovery.
#[derive(Debug)]
pub enum RecoveryRefusal {
    /// The tenant's peers could not be resolved or the store refused.
    Store(HumanOperationError),
    /// The protocol budget record could not be read or verified.
    BudgetState { budget_id: [u8; 32], reason: String },
    /// Served receipts predate evidence persistence and cannot be re-verified.
    EvidenceMissing { budget_id: [u8; 32], count: usize },
    /// Durable recovery itself failed.
    Recovery { budget_id: [u8; 32], reason: String },
    /// Recovery completed but spend accounting is not reconciled.
    WritesBlocked {
        budget_id: [u8; 32],
        accounting: RestartAccounting,
    },
}

/// One tenant budget's startup recovery outcome: the recovered durable state
/// together with the write-admission decision derived from it.
pub struct TenantBudgetRecovery {
    pub recovered: RecoveredOutbox,
    pub admission: Result<(), RecoveryRefusal>,
}

/// Inputs for recovering one tenant budget from the durable store.
pub struct BudgetRecoveryRequest<'a> {
    pub budget_id: [u8; 32],
    pub protocol_budget: ProtocolBudgetState,
    pub verifier: EvidenceAuthority,
    pub receipts_with_evidence: &'a [ReceiptEvidenceRecord],
    pub receipts_without_evidence: &'a [ReceiptMetadata],
    pub ceiling_maximum: u128,
    pub current_sequence: u64,
}

/// Recovers one tenant budget: verifies the protocol budget state, replays the
/// persisted receipt evidence that falls in its period, restores every durable
/// submission and unknown reservation, and decides write admission.
///
/// Receipts without persisted evidence are held, never skipped: recovery still
/// completes so operators can inspect it, but admission is refused naming the
/// budget and the count.
///
/// # Errors
///
/// Returns the refusal when the budget state does not verify or decode, or
/// when durable recovery fails.
pub fn recover_tenant_budget(
    store: &mut Store,
    tenant: &TenantId,
    request: &BudgetRecoveryRequest<'_>,
) -> Result<TenantBudgetRecovery, RecoveryRefusal> {
    let budget_id = request.budget_id;
    let verified = request
        .verifier
        .verify_state(&request.protocol_budget.evidence)
        .map_err(|error| RecoveryRefusal::BudgetState {
            budget_id,
            reason: format!("{error:?}"),
        })?;
    let record = ProtocolBudgetRecord::decode(verified.canonical_state()).map_err(|error| {
        RecoveryRefusal::BudgetState {
            budget_id,
            reason: format!("{error:?}"),
        }
    })?;
    if record.budget_id != budget_id {
        return Err(RecoveryRefusal::BudgetState {
            budget_id,
            reason: "protocol budget record names another budget".to_owned(),
        });
    }
    let window = record.period_start..record.window_end_sequence();
    let budget_receipts: Vec<PersistedReceipt> = request
        .receipts_with_evidence
        .iter()
        .filter(|receipt| window.contains(&receipt.global_sequence))
        .map(|receipt| PersistedReceipt {
            expected_activity_id: receipt.activity_id,
            evidence: receipt.evidence.clone(),
        })
        .collect();
    let evidence_missing = request
        .receipts_without_evidence
        .iter()
        .filter(|receipt| window.contains(&receipt.global_sequence))
        .count();
    let mut unknown_budget_ids = Vec::new();
    for object_id in store.list_object_ids(tenant, ObjectKind::Budget) {
        let Some(id) = object_id.strip_prefix(b"unknown-budget:".as_slice()) else {
            continue;
        };
        let id: [u8; 32] = id.try_into().map_err(|_| RecoveryRefusal::Recovery {
            budget_id,
            reason: "unknown reservation identifier is not 32 bytes".to_owned(),
        })?;
        unknown_budget_ids.push(id);
    }
    let inputs = RecoveryInputs {
        verifier: request.verifier.clone(),
        unknown_budget_ids: &unknown_budget_ids,
        budget_receipts: &budget_receipts,
        protocol_budget: request.protocol_budget.clone(),
        ceiling_maximum: request.ceiling_maximum,
        ceiling_receipts: &[],
        unknown_ceiling_reservations: &[],
        current_sequence: request.current_sequence,
    };
    let recovered = crate::outbox::recover(store, tenant, &inputs).map_err(|error| {
        RecoveryRefusal::Recovery {
            budget_id,
            reason: format!("{error:?}"),
        }
    })?;
    let admission = if evidence_missing > 0 {
        Err(RecoveryRefusal::EvidenceMissing {
            budget_id,
            count: evidence_missing,
        })
    } else {
        recovered
            .require_write_ready()
            .map_err(|error| match error {
                RecoveryError::WritesBlocked => RecoveryRefusal::WritesBlocked {
                    budget_id,
                    accounting: recovered.budget_accounting,
                },
                other => RecoveryRefusal::Recovery {
                    budget_id,
                    reason: format!("{other:?}"),
                },
            })
    };
    Ok(TenantBudgetRecovery {
        recovered,
        admission,
    })
}

#[derive(Clone)]
struct CachedPreparation {
    prepared: Prepared,
    registry: ModuleRegistry,
}

/// One daemon-owned composition for every mutable agent authority. Services
/// construct short-lived adapters borrowing these owners; none opens a second
/// store or maintains an independent approval/budget/session universe.
pub struct UnifiedAgentOwner<A> {
    operations: Arc<Mutex<ProductionHumanOperations<A>>>,
    store: Arc<Mutex<Store>>,
    peers: Vec<HumanPeer>,

    pub programs: Option<crate::ops::program::ProgramOperations>,
    pub approvals: Arc<ApprovalRegistry>,
    pub approval_queue: Arc<ApprovalSubmissionQueue>,
    pub approval_expiry: Arc<ApprovalExpiry>,
    pub budgets: Arc<BudgetLimiter>,
    verified_limits: Vec<LimitConfig>,
    pub preparation_lifecycle: Arc<PreparationLifecycle>,
    pub sessions: Arc<RwLock<SessionRegistry>>,
    pub session_control: SessionControl,
    pub session_keys: SessionKeyRegistry,
    pub degraded: Controller,

    capabilities: BTreeMap<TenantId, crate::capability::CapabilityGraph>,
}

fn session_id_hex(text: &str) -> Option<[u8; 32]> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 {
        return None;
    }
    let mut output = [0_u8; 32];
    for (slot, pair) in output.iter_mut().zip(bytes.chunks_exact(2)) {
        *slot = u8::from_str_radix(std::str::from_utf8(pair).ok()?, 16).ok()?;
    }
    Some(output)
}

/// The one process-wide agent owner shared by the Human Unix listener and the
/// agent RPC listener, so sessions, budgets, approvals and the outbox keep a
/// single authority.
pub struct SharedAgentOwner<A> {
    owner: Arc<Mutex<UnifiedAgentOwner<A>>>,
    idempotency: Option<Arc<RpcIdempotency>>,
}

/// Durable per-tenant idempotency records for agent RPC mutations.
struct RpcIdempotency {
    root: std::path::PathBuf,
    daemon_sequences: u64,
    protocol_sequences: u64,
    stores: Mutex<BTreeMap<String, Arc<crate::idempotency::Store>>>,
}

impl<A> Clone for SharedAgentOwner<A> {
    fn clone(&self) -> Self {
        Self {
            owner: Arc::clone(&self.owner),
            idempotency: self.idempotency.clone(),
        }
    }
}

impl<A: HumanAuthorityBoundary> SharedAgentOwner<A> {
    /// Shares one owner between the Human Unix listener and the agent RPC listener.
    #[must_use]
    pub fn new(owner: UnifiedAgentOwner<A>) -> Self {
        Self {
            owner: Arc::new(Mutex::new(owner)),
            idempotency: None,
        }
    }

    /// Attaches the durable agent RPC idempotency root and retention windows.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Refused` for a relative root or an invalid retention
    /// window.
    pub fn with_idempotency(
        mut self,
        root: std::path::PathBuf,
        daemon_sequences: u64,
        protocol_sequences: u64,
    ) -> Result<Self, HumanOperationError> {
        if !root.is_absolute()
            || crate::idempotency::RetentionPolicy::new(daemon_sequences, protocol_sequences)
                .is_err()
        {
            return Err(HumanOperationError::Refused);
        }
        self.idempotency = Some(Arc::new(RpcIdempotency {
            root,
            daemon_sequences,
            protocol_sequences,
            stores: Mutex::new(BTreeMap::new()),
        }));
        Ok(self)
    }

    /// # Errors
    /// Returns `HumanOperationError::Unavailable` when a previous holder panicked while
    /// holding the owner.
    pub fn lock(&self) -> Result<MutexGuard<'_, UnifiedAgentOwner<A>>, HumanOperationError> {
        self.owner.lock().map_err(|_| HumanOperationError::Unavailable)
    }

    /// Returns the tenant's durable idempotency store, opening it on first use.
    ///
    /// # Errors
    /// Returns `Unavailable` when no idempotency root is configured, a lock is poisoned or
    /// the store cannot be opened.
    pub(crate) fn idempotency(
        &self,
        tenant: &TenantId,
    ) -> Result<Arc<crate::idempotency::Store>, HumanOperationError> {
        let config = self
            .idempotency
            .as_ref()
            .ok_or(HumanOperationError::Unavailable)?;
        let mut stores = config
            .stores
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if let Some(store) = stores.get(tenant.as_str()) {
            return Ok(Arc::clone(store));
        }
        let directory: String = tenant
            .as_str()
            .bytes()
            .map(|byte| format!("{byte:02x}"))
            .collect();
        let retention =
            crate::idempotency::RetentionPolicy::new(config.daemon_sequences, config.protocol_sequences)
                .map_err(|_| HumanOperationError::Unavailable)?;
        let store = Arc::new(
            crate::idempotency::Store::open(config.root.join(directory), tenant.clone(), retention)
                .map_err(|_| HumanOperationError::Unavailable)?,
        );
        stores.insert(tenant.as_str().to_owned(), Arc::clone(&store));
        Ok(store)
    }

    /// Returns the live core chain sequence from the shared owner's node head.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Unavailable` when an owner lock is poisoned.
    pub(crate) fn current_core_sequence(&self) -> Result<u64, HumanOperationError> {
        let owner = self.lock()?;
        let operations = owner.lock_operations()?;
        Ok(operations.node.head().chain_sequence)
    }

    /// Maps a session-authenticated agent principal to a Human peer.
    ///
    /// A Human peer carries Unix-authenticated uid semantics and verified subject bindings;
    /// no session-authorized adapter over the retained subject bindings exists yet, so every
    /// principal is refused rather than admitted without a subject-owner check.
    ///
    /// # Errors
    /// Always returns `HumanOperationError::Refused`.
    pub(crate) fn rpc_peer(
        &self,
        _principal: &crate::tenant::ResolvedPrincipal,
    ) -> Result<HumanPeer, HumanOperationError> {
        Err(HumanOperationError::Refused)
    }
}

impl<A: HumanAuthorityBoundary> HumanOperations for SharedAgentOwner<A> {
    fn session_refresh(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::identity::SessionRefresh) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.session_refresh(context, control, request)
    }

    fn session_close(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::identity::SessionClose) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.session_close(context, control, request)
    }

    fn session_list(&mut self, peer: &HumanPeer, request: layerx_agent_api::identity::SessionList) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.session_list(peer, request)
    }

    fn read_module_state(&mut self, peer: &HumanPeer, request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::ModuleStateSelector>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.read_module_state(peer, request)
    }

    fn read_history(&mut self, peer: &HumanPeer, request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::HistorySelector>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.read_history(peer, request)
    }

    fn read_batch(&mut self, peer: &HumanPeer, request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::BatchRef>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.read_batch(peer, request)
    }

    fn wait(&mut self, peer: &HumanPeer, request: layerx_agent_api::track::WaitRequest) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.wait(peer, request)
    }

    fn budget_create(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetCreate>>) -> Result<layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>, HumanOperationError> {
        self.lock()?.budget_create(context, control, request)
    }

    fn budget_fund(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetFund>>) -> Result<layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>, HumanOperationError> {
        self.lock()?.budget_fund(context, control, request)
    }

    fn budget_list(&mut self, peer: &HumanPeer, request: layerx_agent_api::budget::BudgetList) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.budget_list(peer, request)
    }

    fn budget_revoke(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetTarget>>) -> Result<layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>, HumanOperationError> {
        self.lock()?.budget_revoke(context, control, request)
    }

    fn budget_state(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::budget::BudgetTarget) -> Result<layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>, HumanOperationError> {
        self.lock()?.budget_state(context, control, request)
    }

    fn budget_reconciliation(&mut self, peer: &HumanPeer, request: layerx_agent_api::budget::BudgetTarget) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.budget_reconciliation(peer, request)
    }

    fn capability_create(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::capability::CapabilityCreate>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.capability_create(context, control, request)
    }

    fn capability_attenuate(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::capability::CapabilityAttenuate>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.capability_attenuate(context, control, request)
    }

    fn capability_list(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::capability::CapabilityList) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.capability_list(context, control, request)
    }

    fn capability_revoke(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::capability::CapabilityRevoke>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.capability_revoke(context, control, request)
    }

    fn authorize_subject(&mut self, peer: &HumanPeer) -> Result<(), HumanOperationError> {
        self.lock()?.authorize_subject(peer)
    }

    fn account_state(&mut self, peer: &HumanPeer, account_id: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.account_state(peer, account_id)
    }

    fn registry(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.registry(peer)
    }

    fn prepare(&mut self, peer: &HumanPeer, request: MutationEnvelope<HumanPrepare>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.prepare(peer, request)
    }

    fn submit_external(&mut self, peer: &HumanPeer, request: MutationEnvelope<HumanSubmit>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.submit_external(peer, request)
    }

    fn track(&mut self, peer: &HumanPeer, submission_ref: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.track(peer, submission_ref)
    }

    fn receipt_by_idempotency_key(&mut self, peer: &HumanPeer, idempotency_key: [u8; 32], expected_activity_id: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.receipt_by_idempotency_key(peer, idempotency_key, expected_activity_id)
    }

    fn approval_list(&mut self, peer: &HumanPeer, current_sequence: u64, cursor: Option<[u8; 32]>, limit: u8) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.approval_list(peer, current_sequence, cursor, limit)
    }

    fn approval_get(&mut self, peer: &HumanPeer, approval_id: [u8; 32], current_sequence: u64) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.approval_get(peer, approval_id, current_sequence)
    }

    fn approval_approve(&mut self, peer: &HumanPeer, approval_id: [u8; 32], held_digest: [u8; 32], idempotency_key: &str, current_sequence: u64) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.approval_approve(peer, approval_id, held_digest, idempotency_key, current_sequence)
    }

    fn approval_reject(&mut self, peer: &HumanPeer, approval_id: [u8; 32], held_digest: [u8; 32], idempotency_key: &str, current_sequence: u64) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.approval_reject(peer, approval_id, held_digest, idempotency_key, current_sequence)
    }

    fn balance(&mut self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.balance(peer)
    }

    fn native_fee_policy(&mut self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.native_fee_policy(peer)
    }

    fn session_fee_state(&mut self, peer: &HumanPeer, grant_id: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.session_fee_state(peer, grant_id)
    }

    fn session_seed_prepare(&mut self, peer: &HumanPeer, agent: &str, action_key: [u8; 32], request_digest: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.session_seed_prepare(peer, agent, action_key, request_digest)
    }

    fn head(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.head(peer)
    }

    fn evidence(&mut self, peer: &HumanPeer, idempotency_key: [u8; 32], expected_activity_id: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.evidence(peer, idempotency_key, expected_activity_id)
    }

    fn identity_resolve(&mut self, peer: &HumanPeer, agent: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.identity_resolve(peer, agent)
    }

    fn lease_map(&mut self, peer: &HumanPeer, not_before_unix_ms: u64, not_after_unix_ms: u64) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.lease_map(peer, not_before_unix_ms, not_after_unix_ms)
    }

    fn owner_validate(&mut self, peer: &HumanPeer, request: HumanOwnerInstall) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.owner_validate(peer, request)
    }

    fn owner_install(&mut self, peer: &HumanPeer, request: MutationEnvelope<HumanOwnerInstall>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.owner_install(peer, request)
    }

    fn account_sequence(&mut self, peer: &HumanPeer, actor: &str, authority: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.account_sequence(peer, actor, authority)
    }

    fn agent_list(&mut self, peer: &HumanPeer, cursor: Option<[u8; 32]>, limit: u8) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_list(peer, cursor, limit)
    }

    fn agent_get(&mut self, peer: &HumanPeer, agent_id: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_get(peer, agent_id)
    }

    fn agent_control(&mut self, peer: &HumanPeer, agent_id: &str, resume: bool, session_observation: [u8; 32], evidence: HumanFinalizationEvidence) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_control(peer, agent_id, resume, session_observation, evidence)
    }

    fn agent_limit(&mut self, peer: &HumanPeer, agent_id: &str, monthly_limit: u128, currency: &str, replacement_budget_id: [u8; 32], evidence: HumanFinalizationEvidence) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_limit(peer, agent_id, monthly_limit, currency, replacement_budget_id, evidence)
    }

    fn agent_journey(&mut self, peer: &HumanPeer, agent_id: &str, kind: crate::human::HumanAgentJourneyKind, pre_observation: [u8; 32], post_observation: [u8; 32], evidence: HumanFinalizationEvidence) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_journey(peer, agent_id, kind, pre_observation, post_observation, evidence)
    }

    fn agent_archive(&mut self, peer: &HumanPeer, agent_id: &str, confirm_name: &str, observations: ([u8; 32], [u8; 32], [u8; 32]), evidence: HumanFinalizationEvidence) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_archive(peer, agent_id, confirm_name, observations, evidence)
    }

    fn capability_install(&mut self, peer: &HumanPeer, request: HumanCapabilityInstall) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.capability_install(peer, request)
    }

    fn agent_lifecycle_publish(&mut self, peer: &HumanPeer, request: MutationEnvelope<crate::human::HumanAgentLifecycleSeed>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_lifecycle_publish(peer, request)
    }

    fn agent_context(&mut self, peer: &HumanPeer, agent_id: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_context(peer, agent_id)
    }

    fn agent_budget_state(&mut self, peer: &HumanPeer, active_budget_id: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_budget_state(peer, active_budget_id)
    }

    fn agent_key_policy(&mut self, peer: &HumanPeer, agent_did: &str, recovery: bool) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_key_policy(peer, agent_did, recovery)
    }

    fn agent_session_snapshot(&mut self, peer: &HumanPeer, agent_id: &str) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_session_snapshot(peer, agent_id)
    }

    fn agent_session_suspend(&mut self, peer: &HumanPeer, agent_id: &str, action_key: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_session_suspend(peer, agent_id, action_key)
    }

    fn agent_session_bind(&mut self, peer: &HumanPeer, agent_id: &str, session_id: [u8; 32], token_id: [u8; 32], action_key: [u8; 32]) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_session_bind(peer, agent_id, session_id, token_id, action_key)
    }

    fn agent_session_restrict(&mut self, peer: &HumanPeer, agent_id: &str, current_sequence: u64, action_key: [u8; 32], permitted_activity_types: Vec<u16>, scopes: Vec<String>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.agent_session_restrict(peer, agent_id, current_sequence, action_key, permitted_activity_types, scopes)
    }

    fn operator_command(&mut self, peer: &HumanPeer, operator_id: &str, request_id: [u8; 32], command: OperatorCommand) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.operator_command(peer, operator_id, request_id, command)
    }

    fn subscription_create(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: MutationEnvelope<layerx_agent_api::subscription::SubscriptionCreate>) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_create(context, control, request)
    }

    fn subscription_list(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::SubscriptionList) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_list(context, control, request)
    }

    fn subscription_pause(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::SubscriptionTarget) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_pause(context, control, request)
    }

    fn subscription_resume(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::SubscriptionTarget) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_resume(context, control, request)
    }

    fn subscription_delete(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::SubscriptionTarget) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_delete(context, control, request)
    }

    fn subscription_health(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::SubscriptionTarget) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_health(context, control, request)
    }

    fn subscription_acknowledge(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::subscription::CursorAcknowledgement) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.subscription_acknowledge(context, control, request)
    }

    fn availability_fetch(&mut self, peer: &HumanPeer, request: layerx_agent_api::availability::AvailabilityRequest) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.availability_fetch(peer, request)
    }

    fn export_offline(&mut self, peer: &HumanPeer, request: layerx_agent_api::read::ReadRequest<Vec<layerx_agent_api::export::FactRef>>) -> Result<layerx_agent_api::read::VerifiedRead<layerx_agent_api::export::OfflineExport>, HumanOperationError> {
        self.lock()?.export_offline(peer, request)
    }

    fn fee_projection(&mut self, peer: &HumanPeer, request: layerx_agent_api::read::FeeProjectionRequest) -> Result<layerx_agent_api::read::ProjectionResult<layerx_agent_api::read::FeeProjection>, HumanOperationError> {
        self.lock()?.fee_projection(peer, request)
    }

    fn policy_dry_run(&mut self, context: &crate::agent_rpc_peer::RpcOwnerContext<'_>, control: &crate::session_control::SessionControl, request: layerx_agent_api::policy::PolicyDryRunRequest) -> Result<HumanResponse, HumanOperationError> {
        self.lock()?.policy_dry_run(context, control, request)
    }

    fn policy_dry_run_legacy(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::LegacyPolicyDryRun,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<layerx_agent_api::policy::PolicyDryRunResult>,
        HumanOperationError,
    > {
        self.lock()?
            .policy_dry_run_legacy(context, control, request)
    }
}

fn require_held_reservations(
    approvals: &ApprovalRegistry,
    budgets: &BudgetLimiter,
) -> Result<(), HumanOperationError> {
    for id in approvals
        .hold_ids()
        .map_err(|_| HumanOperationError::Refused)?
    {
        if !budgets
            .has_reservation(id)
            .map_err(|_| HumanOperationError::Refused)?
        {
            return Err(HumanOperationError::Refused);
        }
    }
    Ok(())
}

fn restored_sessions(
    shared_store: &Arc<Mutex<Store>>,
    replayed: &std::collections::BTreeSet<String>,
) -> Result<SessionRegistry, HumanOperationError> {
    let mut sessions = SessionRegistry::default();
    let store = shared_store
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?;
    for tenant in replayed {
        let tenant_id = TenantId::new(tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        sessions
            .restore_tenant(&store, &tenant_id)
            .map_err(|_| HumanOperationError::Refused)?;
        managed_agent::validate_session_coordinates(&store, &sessions, &tenant_id)?;
    }
    Ok(sessions)
}

struct TenantRecoveryContext<'a> {
    peer: &'a HumanPeer,
    tenant_id: &'a TenantId,
    registry: &'a ModuleRegistry,
    inventory: &'a crate::receipt::ReceiptEvidenceInventory,
    verifier: &'a EvidenceAuthority,
    authorization: SequencerAuthorization,
    sequencer_key: [u8; 32],
    ceiling_maximum: u128,
    current_sequence: u64,
}

fn settle_terminal_submission(
    outbox: &mut Outbox,
    store: &mut Store,
    idempotency_key: [u8; 32],
    terminal_state: SubmissionState,
    terminal: crate::protocol_evidence::VerifiedReceiptEvidence,
) -> Result<(), HumanOperationError> {
    let status = outbox
        .status(idempotency_key)
        .ok_or(HumanOperationError::Refused)?;
    if status.state.terminal() {
        if status.state != terminal_state
            || status
                .evidence
                .is_none_or(|evidence| evidence.receipt_ref() != terminal.receipt_ref())
        {
            return Err(HumanOperationError::Refused);
        }
    } else {
        outbox
            .transition(
                store,
                idempotency_key,
                terminal_state,
                "canonical receipt and signed inclusion verified",
                Some(terminal),
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
    }
    Ok(())
}

fn budget_creation_response(
    plan: ActionPlan,
    budget: &ProtocolBudget,
    audit_entries: u64,
) -> Result<HumanResponse, HumanOperationError> {
    let mut out = Encoder::new();
    out.u8(plan_code(plan));
    out.fixed(&budget.object_id());
    out.u64(budget.observed_head_sequence());
    out.u128(budget.record().per_period_limit);
    out.u64(budget.record().expiry);
    out.text(budget.enforcement())?;
    out.u64(audit_entries);
    out.finish()
}

impl<A: HumanAuthorityBoundary> UnifiedAgentOwner<A> {
    pub(crate) fn rpc_session_refresh(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::identity::SessionRefresh,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.context.tenant.as_str() != context.peer().tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let target =
            session_id_hex(request.session_id.as_str()).ok_or(HumanOperationError::Refused)?;
        let current_sequence = self.lock_operations()?.node.head().chain_sequence;
        let (replacement, _) = self
            .session_control
            .refresh_session_authorized(context.permit(), SessionId(target), current_sequence)
            .map_err(Self::session_writer_error)?;
        let credential = replacement.credential();
        if credential.tenant().as_str() != context.peer().tenant.as_str()
            || credential.session_id() != SessionId(target)
        {
            return Err(HumanOperationError::Unavailable);
        }
        let tenant =
            TenantId::new(context.peer().tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let record = sessions
            .get(&tenant, SessionId(target))
            .ok_or(HumanOperationError::Unavailable)?;
        if !record.open
            || record.generation != credential.generation()
            || record.request.token_id != credential.token_id()
        {
            return Err(HumanOperationError::Unavailable);
        }
        let mut out = Encoder::new();
        out.text(credential.tenant().as_str())?;
        out.fixed(&credential.session_id().0);
        out.fixed(&credential.token_id());
        out.u64(credential.generation());
        out.u64(record.request.expiry_sequence);
        Self::encode_session_record(&mut out, record)?;
        out.finish()
    }
    pub(crate) fn rpc_subscription_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<layerx_agent_api::subscription::SubscriptionCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_create(self, context, &control, request)
    }
    pub(crate) fn rpc_capability_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::capability_create(self, context, &control, request)
    }
    pub(crate) fn rpc_capability_attenuate(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityAttenuate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::capability_attenuate(self, context, &control, request)
    }
    pub(crate) fn rpc_capability_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::capability::CapabilityList,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::capability_list(self, context, &control, request)
    }
    pub(crate) fn rpc_capability_revoke(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityRevoke>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::capability_revoke(self, context, &control, request)
    }

    /// Discovers one program bound to the single authenticated chain head pinned for this read.
    ///
    /// # Errors
    /// Returns `Typed(StalePinnedHead)` when the pinned head advanced or aged out, `Unavailable`
    /// when no program reader or kind-5 transport exists, and `Refused` for every other refusal.
    pub(crate) fn rpc_program_discover(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        program: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let program =
            layerx_programs::ProgramId::new(program).map_err(|_| HumanOperationError::Refused)?;
        let mut operations = self
            .operations
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let programs = self
            .programs
            .as_mut()
            .ok_or(HumanOperationError::Unavailable)?;
        let (now, bound) = current_program_bundle(programs, &mut operations.node, context, program)?;
        let discovery = programs
            .discover(program, now, &bound)
            .map_err(program_operation_error)?;
        let mut out = Encoder::new();
        encode_program_discovery(&mut out, &discovery)?;
        out.finish()
    }

    /// Reads one program interface from the same bound kind-5 result as its discovery.
    ///
    /// # Errors
    /// As [`Self::rpc_program_discover`].
    pub(crate) fn rpc_program_interface(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        program: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let program =
            layerx_programs::ProgramId::new(program).map_err(|_| HumanOperationError::Refused)?;
        let mut operations = self
            .operations
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let programs = self
            .programs
            .as_mut()
            .ok_or(HumanOperationError::Unavailable)?;
        let (now, bound) = current_program_bundle(programs, &mut operations.node, context, program)?;
        let read = programs
            .interface(program, now, &bound)
            .map_err(program_operation_error)?;
        let mut out = Encoder::new();
        encode_program_discovery(&mut out, &read.discovery)?;
        out.u32(read.version);
        out.bytes(read.interface.canonical_encoding())?;
        out.finish()
    }

    fn settle_budget_write(
        &self,
        tenant: &TenantId,
        idempotency_key: [u8; 32],
        executed: bool,
        sequence: u64,
    ) -> Result<(), HumanOperationError> {
        let Some(preparation_id) = self
            .session_control
            .preparation_for_idempotency_key(tenant, idempotency_key)
            .map_err(rpc_commit_error)?
        else {
            return Ok(());
        };
        let outcome = if executed {
            crate::budget::ReleaseKind::Executed
        } else {
            crate::budget::ReleaseKind::Failed
        };
        self.session_control
            .settle_write(tenant, preparation_id, outcome, sequence)
            .map_err(rpc_commit_error)?;
        Ok(())
    }

    pub(crate) fn rpc_subscription_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::SubscriptionList,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_list(self, context, &control, request)
    }

    pub(crate) fn rpc_subscription_pause(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_pause(self, context, &control, request)
    }
    pub(crate) fn rpc_subscription_resume(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_resume(self, context, &control, request)
    }
    pub(crate) fn rpc_subscription_delete(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_delete(self, context, &control, request)
    }
    pub(crate) fn rpc_subscription_health(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_health(self, context, &control, request)
    }

    pub(crate) fn rpc_subscription_acknowledge(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::subscription::CursorAcknowledgement,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        HumanOperations::subscription_acknowledge(self, context, &control, request)
    }

    pub(crate) fn rpc_session_close(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::identity::SessionClose,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.context.tenant.as_str() != context.peer().tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let target =
            session_id_hex(request.session_id.as_str()).ok_or(HumanOperationError::Refused)?;
        let current_sequence = self.lock_operations()?.node.head().chain_sequence;
        self.session_control
            .close_session_authorized(context.permit(), SessionId(target), current_sequence)
            .map_err(Self::session_writer_error)?;
        self.session_record_response(context.peer(), target)
    }
    fn session_writer_error(
        error: crate::session_control::SessionControlError,
    ) -> HumanOperationError {
        use crate::session_control::SessionControlError;
        match error {
            SessionControlError::Human(error) => error,
            SessionControlError::Authorization(_) | SessionControlError::Session(_) => {
                HumanOperationError::Refused
            }
            SessionControlError::Lifecycle(_) | SessionControlError::Unavailable => {
                HumanOperationError::Unavailable
            }
        }
    }
    fn session_record_response(
        &self,
        peer: &HumanPeer,
        target: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let record = sessions
            .get(&tenant, SessionId(target))
            .ok_or(HumanOperationError::Unavailable)?;
        let mut out = Encoder::new();
        Self::encode_session_record(&mut out, record)?;
        out.finish()
    }
    fn encode_session_record(
        out: &mut Encoder,
        record: &session::SessionRecord,
    ) -> Result<(), HumanOperationError> {
        out.fixed(&record.request.session_id.0);
        out.fixed(&record.request.token_id);
        out.text(
            std::str::from_utf8(record.request.agent.as_bytes())
                .map_err(|_| HumanOperationError::Refused)?,
        )?;
        out.u64(record.generation);
        out.u8(u8::from(record.open));
        out.u64(record.request.expiry_sequence);
        out.u64(record.sequence);
        Ok(())
    }
    pub(crate) fn rpc_budget_reconciliation(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: layerx_agent_api::budget::BudgetTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        if request.tenant.as_str() != context.peer().tenant {
            return Err(HumanOperationError::Refused);
        }
        let budget_id =
            digest_from_hex(request.budget_id.as_str()).ok_or(HumanOperationError::Refused)?;
        context
            .commit(&control, |peer| {
                self.agent_budget_state(peer, budget_id)
                    .map_err(crate::session_control::SessionControlError::Human)
            })
            .map_err(|error| match error {
                crate::session_control::SessionControlError::Human(error) => error,
                crate::session_control::SessionControlError::Unavailable => {
                    HumanOperationError::Unavailable
                }
                _ => HumanOperationError::Refused,
            })
    }
    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    pub fn new(
        mut operations: ProductionHumanOperations<A>,
        shared_store: Arc<Mutex<Store>>,
        peers: &BTreeMap<u32, (String, String)>,
        verified_limits: Vec<LimitConfig>,
        session_keys: SessionKeyRegistry,
    ) -> Result<Self, HumanOperationError> {
        if verified_limits.is_empty() {
            return Err(HumanOperationError::Refused);
        }
        let ceiling_maximum = verified_limits
            .iter()
            .map(|limit| limit.ceiling)
            .min()
            .ok_or(HumanOperationError::Refused)?;
        let verified = verified_limits.clone();
        let approvals = Arc::new(ApprovalRegistry::with_store(Arc::clone(&shared_store)));
        let budgets = Arc::new(
            BudgetLimiter::new(verified_limits).map_err(|_| HumanOperationError::Refused)?,
        );
        let approval_queue = Arc::new(ApprovalSubmissionQueue::default());

        {
            let store = shared_store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            crate::budget::load_daemon_limits(&store, &budgets)
                .map_err(daemon_limit_refusal)?;
        }
        let mut replayed = std::collections::BTreeSet::new();
        let restore_peers = {
            let store = shared_store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            subject::restore_peers(&store, peers)?
        };
        for peer in &restore_peers {
            let tenant = &peer.tenant;
            if replayed.insert(tenant.clone()) {
                let tenant_id =
                    TenantId::new(tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
                crate::budget::install_enrolment_limits(
                    &mut *shared_store
                        .lock()
                        .map_err(|_| HumanOperationError::Unavailable)?,
                    &budgets,
                    &tenant_id,
                    &verified,
                )
                .map_err(|_| HumanOperationError::Refused)?;
                let registry = operations.authority.registry(peer).map_err(map_core)?;
                let released = approvals
                    .replay_released(&tenant_id, &budgets, &registry)
                    .map_err(|_| HumanOperationError::Refused)?;
                approval_queue
                    .restore(released)
                    .map_err(|_| HumanOperationError::Refused)?;
                approvals
                    .replay_tenant(&tenant_id, &budgets)
                    .map_err(|_| HumanOperationError::Refused)?;
                approvals
                    .validate_registry(&tenant_id, &registry)
                    .map_err(|_| HumanOperationError::Refused)?;
                {
                    let store = shared_store
                        .lock()
                        .map_err(|_| HumanOperationError::Unavailable)?;
                    managed_agent::validate_tenant(&store, &tenant_id)?;
                }
            }
        }
        require_held_reservations(&approvals, &budgets)?;
        operations.recover_tenants(&restore_peers, ceiling_maximum);
        operations.unified_owner_active = true;
        {
            let store = shared_store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            for tenant in store.tenant_ids_for_kind(ObjectKind::Session) {
                replayed.insert(tenant.as_str().to_owned());
            }
        }
        let sessions = restored_sessions(&shared_store, &replayed)?;
        session_keys
            .probe()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let preparation_lifecycle = Arc::new(PreparationLifecycle::default());
        let session_control = SessionControl::new(
            Arc::clone(&shared_store),
            sessions,
            Arc::clone(&preparation_lifecycle),
            Arc::clone(&budgets),
        );
        let sessions = session_control.registry();

        session_control
            .restore_writes()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let cleanup_tenants = shared_store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?
            .tenant_ids_for_kind(ObjectKind::Capability);
        for tenant in &cleanup_tenants {
            sweep_capability_cleanups(&session_control, &preparation_lifecycle, tenant)?;

            let store = shared_store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            crate::capability::restore_chain_reservations(&store, tenant)
                .map_err(|error| consume_refusal(&error))?;
        }

        operations.attach_session_control(session_control.clone());

        operations.attach_budget_limiter(Arc::clone(&budgets));
        Ok(Self {
            operations: Arc::new(Mutex::new(operations)),
            store: Arc::clone(&shared_store),
            peers: restore_peers,

            programs: None,
            approvals,
            approval_queue,
            approval_expiry: Arc::new(ApprovalExpiry::from_shared_store(shared_store)),
            verified_limits: verified,
            budgets,
            preparation_lifecycle,
            sessions,
            session_control,
            session_keys,
            degraded: Controller::default(),

            capabilities: BTreeMap::new(),
        })
    }

    fn lock_operations(
        &self,
    ) -> Result<MutexGuard<'_, ProductionHumanOperations<A>>, HumanOperationError> {
        self.operations
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)
    }

    /// The Human peer bindings restored by the server at construction.
    #[must_use]
    pub(crate) fn retained_peers(&self) -> &[HumanPeer] {
        &self.peers
    }

    /// Revalidates a retained peer through the authority and binds `agent` to its subject.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Refused` when the authority refuses the subject or the
    /// agent is not bound to it, and `Unavailable` when an owner lock is poisoned.
    pub(crate) fn bind_rpc_subject(
        &mut self,
        peer: &HumanPeer,
        agent: &Did,
    ) -> Result<HumanPeer, HumanOperationError> {
        let mut operations = self.lock_operations()?;
        operations.authorize_subject(peer)?;
        let registry = operations.authority.registry(peer).map_err(map_core)?;
        subject::for_did(&operations.store, peer, agent, &registry)
    }

    /// Resolves the owner of a session held by the shared session registry.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Refused` when the tenant holds no such session, and
    /// `Unavailable` when the registry lock is poisoned.
    pub(crate) fn session_owner(
        &self,
        tenant: &TenantId,
        id: SessionId,
    ) -> Result<crate::tenant::ObjectOwner, HumanOperationError> {
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let record = sessions.get(tenant, id).ok_or(HumanOperationError::Refused)?;
        Ok(crate::tenant::ObjectOwner {
            tenant: record.request.tenant.clone(),
            agent: Some(record.request.agent.clone()),
        })
    }

    /// Resolves the stored owner of the object an operation addresses.
    ///
    /// `Ok(None)` only for an operation that addresses no stored object.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Refused` for a missing, malformed or unknown reference and
    /// for every referenced object whose owner record is not resolvable here, and `Unavailable`
    /// when an owner lock is poisoned.
    pub(crate) fn target_object_owner(
        &self,
        operation: crate::tenant::Operation,
        request: &serde_json::Map<String, serde_json::Value>,
        tenant: &TenantId,
    ) -> Result<Option<crate::tenant::ObjectOwner>, HumanOperationError> {
        use crate::tenant::Operation;
        match operation {
            Operation::ApprovalList
            | Operation::SubscriptionList
            | Operation::BudgetList
            | Operation::CapabilityList
            | Operation::SessionList
            | Operation::AvailabilityFetch
            | Operation::ProgramDiscover
            | Operation::ProgramInterface
            | Operation::ProgramSimulate
            | Operation::Project
            | Operation::ReadAccount
            | Operation::ReadBalance
            | Operation::ReadBatch
            | Operation::ReadCheckpoint
            | Operation::ReadHistory
            | Operation::ReadModuleState
            | Operation::BudgetCreate
            | Operation::CapabilityCreate
            | Operation::SubscriptionCreate
            | Operation::Prepare
            | Operation::ProgramCall
            | Operation::ProgramDeploy
            | Operation::FaucetClaim => Ok(None),
            Operation::SessionClose | Operation::SessionRefresh => {
                let text = request
                    .get("session_id")
                    .and_then(serde_json::Value::as_str)
                    .ok_or(HumanOperationError::Refused)?;
                let id = session_id_hex(text).ok_or(HumanOperationError::Refused)?;
                self.session_owner(tenant, SessionId(id)).map(Some)
            }
            Operation::AgentRegister
            | Operation::SessionOpen
            | Operation::ApprovalApprove
            | Operation::ApprovalGet
            | Operation::ApprovalReject
            | Operation::ExportOffline
            | Operation::SubscriptionAcknowledge
            | Operation::SubscriptionDelete
            | Operation::SubscriptionHealth
            | Operation::SubscriptionPause
            | Operation::SubscriptionResume
            | Operation::BudgetReconciliation
            | Operation::BudgetFund
            | Operation::BudgetRevoke
            | Operation::CapabilityAttenuate
            | Operation::CapabilityRevoke
            | Operation::ProgramActivity
            | Operation::ProgramReceipt
            | Operation::ProgramUpgrade
            | Operation::ProgramWindDown
            | Operation::ReadProofBundle
            | Operation::Sign
            | Operation::Submit
            | Operation::Track
            | Operation::Wait => Err(HumanOperationError::Refused),
        }
    }

    /// Reports whether the owner is healthy, with the network id and protocol version the
    /// node negotiated at handshake.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Unavailable` when the operation owner is poisoned.
    pub fn rpc_health(&self) -> Result<(bool, u32, u16), HumanOperationError> {
        let operations = self.lock_operations()?;
        let node = operations.node.handshake().node();
        Ok((
            self.degraded.status().mode == crate::degraded::Mode::Healthy,
            node.network_id,
            node.protocol_version,
        ))
    }
}

fn rpc_commit_error(error: crate::session_control::SessionControlError) -> HumanOperationError {
    match error {
        crate::session_control::SessionControlError::Human(error) => error,
        crate::session_control::SessionControlError::Unavailable => HumanOperationError::Unavailable,
        _ => HumanOperationError::Refused,
    }
}

impl<A: HumanAuthorityBoundary> UnifiedAgentOwner<A> {
    pub(crate) fn rpc_approval_approve(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        self.approval_decide(
            context.peer(),
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
            true,
            Some((context, &control)),
        )
    }

    pub(crate) fn rpc_approval_reject(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        let control = self.session_control.clone();
        self.approval_decide(
            context.peer(),
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
            false,
            Some((context, &control)),
        )
    }

    /// Admits one preparation through the single write seam: the capability charge (or the
    /// unrestricted legacy check), the idempotent outcome and the preparation record are one
    /// durable store write. A retry of an already recorded preparation rechecks its binding and
    /// replays the recorded outcome; it is never charged again.
    pub(crate) fn rpc_prepare(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<HumanPrepare>,
    ) -> Result<HumanResponse, HumanOperationError> {
        use crate::capability::binding::{self, Admission, AdmissionOutcome, BindingError};
        use crate::prepare::{
            DurablePreparation, PreparationExtension, EXTENSION_CAPABILITY, EXTENSION_OUTCOME,
        };
        use crate::session_control::{
            AdmissionPlan, AdmissionPlanner, AdmissionStage, SessionControlError, WriteAdmission,
        };
        let control = self.session_control.clone();
        let peer = context.peer();
        let (tenant, agent) = binding_coordinates(context)?;
        let body_digest = request.body_digest;
        let capability_id = request.operation.capability_id;
        let mut operations = self.lock_operations()?;
        let before: std::collections::BTreeSet<String> = operations
            .prepared
            .keys()
            .filter(|(tenant, principal, _)| *tenant == peer.tenant && *principal == peer.principal)
            .map(|(_, _, reference)| reference.clone())
            .collect();
        let response = operations.prepare_gated(peer, request, Some((context, &control)))?;
        let (key, prepared) = {
            let mut inserted = operations.prepared.iter().filter(|((tenant, principal, reference), _)| {
                *tenant == peer.tenant
                    && *principal == peer.principal
                    && !before.contains(reference)
            });
            let (Some((key, cached)), None) = (inserted.next(), inserted.next()) else {
                return Err(HumanOperationError::Unavailable);
            };
            (key.clone(), cached.prepared.clone())
        };
        let admitted = (|| -> Result<(), HumanOperationError> {
            let preparation_id = digest_from_hex(&key.2).ok_or(HumanOperationError::Refused)?;
            let snapshot = core_preparation_snapshot(
                &mut operations.node,
                peer,
                prepared.envelope.actor_did(),
            )?;
            let now_ms = snapshot.protocol_timestamp;
            let existing = {
                let shared = control.store();
                let store = shared
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                let durable_key = DurablePreparation::store_key(&tenant, preparation_id)
                    .map_err(|_| HumanOperationError::Unavailable)?;
                store.get(&durable_key).map(|value| value.bytes().to_vec())
            };
            if let Some(bytes) = existing {
                let record = DurablePreparation::decode(tenant.clone(), &bytes)
                    .map_err(|_| HumanOperationError::Unavailable)?;
                return recheck_stored_binding(&control, &tenant, &agent, &record, now_ms)?
                    .replay(&body_digest, capability_id.as_ref())
                    .map_err(|error| binding_refusal(&error));
            }
            let planner_tenant = tenant.clone();
            let planner_agent = agent.clone();
            let disclosure = &prepared.disclosure;
            let observed_head_sequence = snapshot.observed_head_sequence;
            let planner: AdmissionPlanner<'_> = Box::new(move |store: &Store| {
                let refuse =
                    |error: BindingError| SessionControlError::Human(binding_refusal(&error));
                let (extension, updates, companions) = match capability_id {
                    Some(selected) => {
                        let semantic = crate::capability::derive_effects(
                            disclosure,
                            &crate::capability::VerifiedInputs {
                                revoke_balance: verified_revoke_balance(disclosure),
                            },
                        )
                        .map_err(|_| SessionControlError::Human(HumanOperationError::Refused))?;
                        let record =
                            crate::capability::timed::restore(store, &planner_tenant, &selected)
                                .map_err(|error| {
                                    SessionControlError::Human(capability_refusal(&error))
                                })?
                                .filter(|record| record.agent == planner_agent)
                                .ok_or_else(|| refuse(BindingError::Unbound))?;
                        let intent = binding::PlanIntent::new(
                            disclosure.activity_type.value(),
                            &semantic,
                            capability_purpose(&record, disclosure).map_err(refuse)?,
                        );
                        let disclosure_digest = disclosure
                            .audit_digest()
                            .map_err(|_| SessionControlError::Unavailable)?;
                        let Admission::Bound {
                            extension,
                            mut plan,
                        } = binding::admit(
                            store,
                            &planner_tenant,
                            &planner_agent,
                            capability_id,
                            preparation_id,
                            &intent,
                            disclosure_digest,
                            now_ms,
                        )
                        .map_err(refuse)?
                        else {
                            return Err(SessionControlError::Unavailable);
                        };
                        let chain = extension
                            .chain
                            .iter()
                            .map(|id| {
                                crate::capability::timed::restore(store, &planner_tenant, id)
                                    .map_err(|error| {
                                        SessionControlError::Human(capability_refusal(&error))
                                    })?
                                    .ok_or(SessionControlError::Unavailable)
                            })
                            .collect::<Result<Vec<_>, _>>()?;
                        let reservation = crate::capability::plan_chain(
                            store,
                            &planner_tenant,
                            preparation_id,
                            &chain,
                            &semantic,
                            observed_head_sequence,
                        )
                        .map_err(|error| SessionControlError::Human(consume_refusal(&error)))?;
                        if let Some(companion) = reservation.companion {
                            plan.companions.push(companion);
                        }
                        (Some(extension), plan.updates, plan.companions)
                    }
                    None => {
                        if binding::is_restricted(store, &planner_tenant, &planner_agent)
                            .map_err(refuse)?
                        {
                            return Err(refuse(BindingError::Restricted));
                        }
                        (None, Vec::new(), Vec::new())
                    }
                };
                let mut extensions = Vec::new();
                if let Some(extension) = &extension {
                    extensions.push(PreparationExtension {
                        tag: EXTENSION_CAPABILITY,
                        bytes: extension.encode().map_err(refuse)?,
                    });
                }
                extensions.push(PreparationExtension {
                    tag: EXTENSION_OUTCOME,
                    bytes: AdmissionOutcome::admitted(body_digest, extension.as_ref()).encode(),
                });
                Ok(AdmissionPlan {
                    updates,
                    companions,
                    extensions,
                })
            });
            let charge = {
                let mut store = operations
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                crate::budget::install_enrolment_limits(
                    &mut store,
                    &self.budgets,
                    &tenant,
                    &self.verified_limits,
                )
                .map_err(|_| HumanOperationError::Refused)?;
                let mut amount: u128 = 0;
                for disclosed in &prepared.disclosure.amounts {
                    if matches!(
                        disclosed.role,
                        layerx_crypto::disclosure::AmountRole::Transfer
                    ) {
                        amount = amount
                            .checked_add(disclosed.value)
                            .ok_or(HumanOperationError::Refused)?;
                    }
                }
                if amount == 0 {
                    None
                } else {
                    let actor = std::str::from_utf8(prepared.envelope.actor_did().as_bytes())
                        .map_err(|_| HumanOperationError::Refused)?;
                    let agent_digest = daemon_limit_agent(actor);
                    let session_id = context
                        .permit()
                        .preparation_authorization()
                        .session
                        .session_id;
                    let mut applicable_limits = crate::budget::enrolment_charge_limits(
                        &store,
                        &tenant,
                        &self.verified_limits,
                        &prepared.disclosure,
                        &[
                            crate::budget::LimitScope::Agent(agent_digest),
                            crate::budget::LimitScope::Session(session_id.0),
                        ],
                    )
                    .map_err(|_| HumanOperationError::Refused)?;
                    applicable_limits.extend(
                        crate::budget::applicable_daemon_limits(
                            &store,
                            &tenant,
                            agent_digest,
                            prepared.disclosure.asset,
                            crate::budget::CoreTimestampMs(now_ms),
                        )
                        .map_err(daemon_limit_refusal)?,
                    );
                    if applicable_limits.is_empty() {
                        None
                    } else {
                        Some(crate::session_control::WriteCharge {
                            amount,
                            applicable_limits,
                            head_sequence_bound: snapshot.observed_head_sequence,
                            core_deadline_ms: Some(crate::budget::CoreTimestampMs(
                                prepared.disclosure.expiry.not_after,
                            )),
                        })
                    }
                }
            };
            context
                .permit()
                .admit_write(
                    &control,
                    WriteAdmission {
                        stage: AdmissionStage::Prepare {
                            prepared: &prepared,
                        },
                        preparation_id,
                        charge,
                        extensions: Vec::new(),
                        current_sequence: snapshot.observed_head_sequence,
                        core_time_ms: now_ms,
                        planner: Some(planner),
                    },
                )
                .map_err(rpc_commit_error)?;
            Ok(())
        })();
        if let Err(error) = admitted {
            operations.prepared.remove(&key);
            return Err(error);
        }
        Ok(response)
    }

    /// Enqueue and transmit run inside one permit read interval, so a session close cannot
    /// land between the durable outbox enqueue and node submission.
    pub(crate) fn rpc_submit_external(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        request: MutationEnvelope<HumanSubmit>,
    ) -> Result<HumanResponse, HumanOperationError> {
        use crate::session_control::{AdmissionStage, WriteAdmission};
        let control = self.session_control.clone();
        self.authorize_external_submit(context.peer(), &request)?;

        if context.peer().subject.is_none() {
            return Err(HumanOperationError::Refused);
        }
        let (tenant, agent) = binding_coordinates(context)?;
        let permit = context.permit();
        let origin = permit.preparation_authorization();
        let registered = match digest_from_hex(&request.operation.preparation_ref) {
            Some(id) => match self.preparation_lifecycle.state(id) {
                Ok(_) => Some(id),
                Err(crate::prepare::LifecycleError::NotFound) => None,
                Err(_) => return Err(HumanOperationError::Unavailable),
            },
            None => None,
        };
        let mut operations = self.lock_operations()?;
        if let Some(preparation_id) = registered {
            let peer = context.peer();
            let cached = operations
                .prepared
                .get(&(
                    peer.tenant.clone(),
                    peer.principal.clone(),
                    request.operation.preparation_ref.clone(),
                ))
                .cloned()
                .ok_or(HumanOperationError::Refused)?;
            let signature: [u8; 64] = request
                .operation
                .signature
                .as_slice()
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?;
            let signed = attach_external_signature(&cached.prepared, signature)
                .map_err(|_| HumanOperationError::Refused)?;
            let verified = verify_before_submit(
                &signed,
                &cached.prepared,
                &request.operation.signer_public_key,
                &cached.registry,
            )
            .map_err(|_| HumanOperationError::Refused)?;
            let snapshot = core_preparation_snapshot(
                &mut operations.node,
                peer,
                cached.prepared.envelope.actor_did(),
            )?;
            let charge = {
                let store = operations
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                budget_write_charge(
                    &store,
                    &tenant,
                    &cached.prepared,
                    snapshot.protocol_timestamp,
                    snapshot.observed_head_sequence,
                )?
            };
            let record = permit
                .admit_write(
                    &control,
                    WriteAdmission {
                        stage: AdmissionStage::Submit,
                        preparation_id,
                        charge,
                        extensions: Vec::new(),
                        current_sequence: snapshot.observed_head_sequence,
                        core_time_ms: snapshot.protocol_timestamp,
                        planner: None,
                    },
                )
                .map_err(rpc_commit_error)?;
            recheck_submit_binding(
                &cached.prepared.disclosure,
                &control,
                &tenant,
                &agent,
                &record,
                snapshot.protocol_timestamp,
            )?;
            permit
                .submit_with_external_signature(
                    &control,
                    preparation_id,
                    verified.exact_bytes().to_vec(),
                    verified.activity_id(),
                    snapshot.observed_head_sequence,
                    snapshot.protocol_timestamp,
                )
                .map_err(rpc_commit_error)?;
            control
                .mark_signed(&origin.session.tenant, preparation_id)
                .map_err(rpc_commit_error)?;
            permit
                .transition_preparation(
                    &control,
                    preparation_id,
                    crate::prepare::LifecycleState::Submitted,
                    snapshot.observed_head_sequence,
                )
                .map_err(rpc_commit_error)?;
            control
                .mark_submitted(&tenant, preparation_id)
                .map_err(rpc_commit_error)?;
        } else {
            let shared = control.store();
            let restricted = crate::capability::binding::is_restricted(
                &*shared
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?,
                &tenant,
                &agent,
            )
            .map_err(|error| binding_refusal(&error))?;
            if restricted {
                return Err(binding_refusal(
                    &crate::capability::binding::BindingError::Restricted,
                ));
            }
        }
        context
            .commit(&control, |peer| {
                operations
                    .submit_external_with_origin(peer, request, Some(origin))
                    .map_err(crate::session_control::SessionControlError::Human)
            })
            .map_err(rpc_commit_error)
    }

    fn authorize_external_submit(
        &self,
        peer: &HumanPeer,
        request: &MutationEnvelope<HumanSubmit>,
    ) -> Result<(), HumanOperationError> {
        let prepared_key = (
            peer.tenant.clone(),
            peer.principal.clone(),
            request.operation.preparation_ref.clone(),
        );
        let prepared = self
            .lock_operations()?
            .prepared
            .get(&prepared_key)
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        self.approval_queue
            .authorize_submit(
                &tenant,
                &request.operation.preparation_ref,
                &prepared.prepared.canonical_bytes,
                request.operation.approval_release_ref,
            )
            .map_err(|_| HumanOperationError::Refused)
    }

    #[allow(clippy::too_many_arguments)]
    fn approval_decide(
        &mut self,
        peer: &HumanPeer,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
        approve: bool,
        gate: Option<(
            &crate::agent_rpc_peer::RpcOwnerContext<'_>,
            &crate::session_control::SessionControl,
        )>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let key = DecisionKey::new(idempotency_key).map_err(|_| HumanOperationError::Refused)?;
        if approve {
            if let Some(decision) = self
                .approval_expiry
                .repeated(&tenant, approval_id, &key)
                .map_err(|_| HumanOperationError::Unavailable)?
            {
                if decision.outcome == ApprovalOutcome::Granted {
                    let reference = decision
                        .submission_ref
                        .ok_or(HumanOperationError::Refused)?;
                    if !self
                        .approval_queue
                        .matches_released_decision(&tenant, approval_id, reference, held_digest)
                        .map_err(|_| HumanOperationError::Unavailable)?
                    {
                        return Err(HumanOperationError::Refused);
                    }
                }
                return encode_decision(&decision);
            }
        }
        let snapshot = self
            .approvals
            .get_scoped(&tenant, approval_id, current_sequence)
            .map_err(|_| HumanOperationError::Refused)?;
        if snapshot.prepared.disclosure.canonical_digest != held_digest {
            return Err(HumanOperationError::Refused);
        }
        let request = DecisionRequest {
            tenant: &tenant,
            approval_id,
            idempotency_key: &key,
            approver: ApproverId::new(peer.principal.clone())
                .map_err(|_| HumanOperationError::Refused)?,
            current_sequence,
        };
        let service = ApprovalService::new(&self.approvals, &self.budgets, &self.approval_expiry);
        let queue = &self.approval_queue;
        let effect = move || {
            let decision = if approve {
                service.approve(request, &snapshot.prepared, queue)
            } else {
                service.reject(request)
            };
            decision.map_err(|_| HumanOperationError::Unavailable)
        };
        let decision = match gate {
            None => effect()?,
            Some((context, control)) => context
                .commit(control, |_| {
                    effect().map_err(crate::session_control::SessionControlError::Human)
                })
                .map_err(rpc_commit_error)?,
        };
        encode_decision(&decision)
    }
}

impl<A: HumanAuthorityBoundary> HumanOperations for UnifiedAgentOwner<A> {
    fn session_list(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::identity::SessionList,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.0.tenant.as_str() != peer.tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut records: Vec<&session::SessionRecord> = sessions.tenant_sessions(&tenant).collect();
        records.sort_by_key(|record| record.request.session_id.0);
        let mut out = Encoder::new();
        out.u16(records.len())?;
        for record in records {
            Self::encode_session_record(&mut out, record)?;
        }
        out.finish()
    }
    fn read_module_state(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::ModuleStateSelector>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.read_module_state(peer, request)
    }
    fn read_history(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::HistorySelector>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.read_history(peer, request)
    }
    fn read_batch(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::BatchRef>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.read_batch(peer, request)
    }
    fn wait(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::track::WaitRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.wait(peer, request)
    }
    fn budget_reconciliation(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::budget::BudgetTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.tenant.as_str() != peer.tenant {
            return Err(HumanOperationError::Refused);
        }
        let budget_id =
            digest_from_hex(request.budget_id.as_str()).ok_or(HumanOperationError::Refused)?;
        self.agent_budget_state(peer, budget_id)
    }
    fn authorize_subject(&mut self, peer: &HumanPeer) -> Result<(), HumanOperationError> {
        self.lock_operations()?.authorize_subject(peer)
    }
    fn operator_command(
        &mut self,
        peer: &HumanPeer,
        operator_id: &str,
        request_id: [u8; 32],
        command: OperatorCommand,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .operator_command(peer, operator_id, request_id, command)
    }
    fn account_state(
        &mut self,
        peer: &HumanPeer,
        account_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.account_state(peer, account_id)
    }
    fn registry(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.registry(peer)
    }
    fn prepare(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanPrepare>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.prepare(peer, request)
    }
    fn submit_external(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanSubmit>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.authorize_external_submit(peer, &request)?;
        self.lock_operations()?.submit_external(peer, request)
    }
    fn track(
        &mut self,
        peer: &HumanPeer,
        submission_ref: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        let mut operations = self.lock_operations()?;
        let response = operations.track(peer, submission_ref)?;
        if let Some((key, result_code, sequence)) = operations.last_verified_receipt.take() {
            let tenant =
                TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
            {
                let mut store = self
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                self.approval_queue
                    .settle_verified(
                        &tenant,
                        key,
                        result_code,
                        sequence,
                        &mut store,
                        &self.budgets,
                    )
                    .map_err(|_| HumanOperationError::Unavailable)?;
            }
            self.settle_budget_write(&tenant, key, result_code == 0, sequence)?;
        }
        Ok(response)
    }
    fn receipt_by_idempotency_key(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let mut operations = self.lock_operations()?;
        let response =
            operations.receipt_by_idempotency_key(peer, idempotency_key, expected_activity_id)?;
        if let Some((key, result_code, sequence)) = operations.last_verified_receipt.take() {
            let tenant =
                TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
            {
                let mut store = self
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                self.approval_queue
                    .settle_verified(
                        &tenant,
                        key,
                        result_code,
                        sequence,
                        &mut store,
                        &self.budgets,
                    )
                    .map_err(|_| HumanOperationError::Unavailable)?;
            }
            self.settle_budget_write(&tenant, key, result_code == 0, sequence)?;
        }
        Ok(response)
    }
    fn approval_list(
        &mut self,
        peer: &HumanPeer,
        current_sequence: u64,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let page = ApprovalService::new(&self.approvals, &self.budgets, &self.approval_expiry)
            .list(&tenant, cursor, usize::from(limit), current_sequence)
            .map_err(|_| HumanOperationError::Refused)?;
        let mut out = Encoder::new();
        out.u8(u8::try_from(page.approvals.len()).map_err(|_| HumanOperationError::Refused)?);
        for record in &page.approvals {
            encode_approval(&mut out, record)?;
        }
        match page.next_cursor {
            Some(value) => {
                out.u8(1);
                out.fixed(&value);
            }
            None => out.u8(0),
        }
        out.finish()
    }
    fn approval_get(
        &mut self,
        peer: &HumanPeer,
        approval_id: [u8; 32],
        current_sequence: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let record = ApprovalService::new(&self.approvals, &self.budgets, &self.approval_expiry)
            .get(&tenant, approval_id, current_sequence)
            .map_err(|_| HumanOperationError::Refused)?;
        let mut out = Encoder::new();
        encode_approval(&mut out, &record)?;
        out.finish()
    }
    fn approval_approve(
        &mut self,
        peer: &HumanPeer,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.approval_decide(
            peer,
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
            true,
            None,
        )
    }
    fn approval_reject(
        &mut self,
        peer: &HumanPeer,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.approval_decide(
            peer,
            approval_id,
            held_digest,
            idempotency_key,
            current_sequence,
            false,
            None,
        )
    }
    fn balance(&mut self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.balance(peer)
    }
    fn native_fee_policy(
        &mut self,
        _peer: &HumanPeer,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.native_fee_policy()
    }
    fn session_seed_prepare(
        &mut self,
        peer: &HumanPeer,
        agent: &str,
        action_key: [u8; 32],
        request_digest: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        if action_key == [0; 32] || request_digest == [0; 32] || Did::new(agent.as_bytes()).is_err()
        {
            return Err(HumanOperationError::Refused);
        }
        let mut namespace = Vec::new();
        for part in [
            peer.tenant.as_bytes(),
            peer.principal.as_bytes(),
            agent.as_bytes(),
        ] {
            namespace.extend(
                u32::try_from(part.len())
                    .map_err(|_| HumanOperationError::Refused)?
                    .to_be_bytes(),
            );
            namespace.extend(part);
        }
        namespace.extend(action_key);
        let seed = self
            .session_keys
            .prepare_seed(&namespace, request_digest)
            .map_err(|_| HumanOperationError::Refused)?;
        let mut out = Encoder::new();
        out.fixed(seed.as_ref());
        out.finish()
    }
    fn session_fee_state(
        &mut self,
        _peer: &HumanPeer,
        grant_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.session_fee_state(grant_id)
    }
    fn head(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.head(peer)
    }
    fn evidence(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .evidence(peer, idempotency_key, expected_activity_id)
    }
    fn account_sequence(
        &mut self,
        peer: &HumanPeer,
        actor: &str,
        authority: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .account_sequence(peer, actor, authority)
    }
    fn identity_resolve(
        &mut self,
        peer: &HumanPeer,
        agent: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        let did = Did::new(agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let identity = self
            .lock_operations()?
            .subject_identity(peer, &did)
            .map_err(|error| map_identity_operation(&error))?;
        encode_identity(&identity)
    }
    fn lease_map(
        &mut self,
        peer: &HumanPeer,
        not_before_unix_ms: u64,
        not_after_unix_ms: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        let attestation = self.lock_operations()?.authority.lease_attestation(peer)?;
        let (not_before_sequence, expiry_sequence) =
            attestation.map(not_before_unix_ms, not_after_unix_ms)?;
        let mut out = Encoder::new();
        out.u64(not_before_sequence);
        out.u64(expiry_sequence);
        out.u64(attestation.observed_head_sequence);
        out.bytes(&attestation.canonical_bytes)?;
        out.finish()
    }
    fn owner_validate(
        &mut self,
        peer: &HumanPeer,
        request: HumanOwnerInstall,
    ) -> Result<HumanResponse, HumanOperationError> {
        let validated = self.validate_owner(peer, &request)?;
        encode_owner_validation(&validated)
    }
    fn owner_install(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanOwnerInstall>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if owner_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let action = owner_action_key(&tenant, request.key)?;
        let replay = self.owner_replay(&action, request.body_digest)?;
        let (validated, allocated_token_id) = match replay {
            Some(OwnerAction::Completed(response)) => return Ok(response),
            Some(OwnerAction::Validated(value, Some(token_id))) => (value, token_id),
            Some(OwnerAction::Validated(value, None)) => match self.persist_owner_validation(
                &tenant,
                &action,
                request.body_digest,
                request.operation.session_id,
                value,
            )? {
                OwnerAction::Completed(response) => return Ok(response),
                OwnerAction::Validated(value, Some(token_id)) => (value, token_id),
                _ => return Err(HumanOperationError::Unavailable),
            },
            Some(OwnerAction::Pending) | None => {
                let value = self.validate_owner(peer, &request.operation)?;
                match self.persist_owner_validation(
                    &tenant,
                    &action,
                    request.body_digest,
                    request.operation.session_id,
                    value,
                )? {
                    OwnerAction::Completed(response) => return Ok(response),
                    OwnerAction::Validated(value, Some(token_id)) => (value, token_id),
                    _ => return Err(HumanOperationError::Unavailable),
                }
            }
        };
        self.install_owner_session(&tenant, action, request, &validated, allocated_token_id)
    }
    fn agent_list(
        &mut self,
        peer: &HumanPeer,
        cursor: Option<[u8; 32]>,
        limit: u8,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::list(&store, &tenant, cursor, limit)
    }
    fn agent_get(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::get(&store, &tenant, agent_id)
    }
    fn agent_control(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        resume: bool,
        session_observation: [u8; 32],
        evidence: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::finalize_control(
            &mut store,
            &tenant,
            agent_id,
            resume,
            session_observation,
            evidence.into(),
        )
    }
    fn agent_limit(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        monthly_limit: u128,
        currency: &str,
        replacement_budget_id: [u8; 32],
        evidence: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::finalize_limit(
            &mut store,
            &tenant,
            agent_id,
            monthly_limit,
            currency,
            replacement_budget_id,
            evidence.into(),
        )
    }
    fn agent_journey(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        kind: crate::human::HumanAgentJourneyKind,
        pre_observation: [u8; 32],
        post_observation: [u8; 32],
        evidence: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let (kind, amount, currency, delay, ready) = match kind {
            crate::human::HumanAgentJourneyKind::OwnerRotated {
                custody_key,
                signed_activity,
            } => {
                if pre_observation != [0; 32] || post_observation != [0; 32] {
                    return Err(HumanOperationError::Refused);
                }
                let (identity, authority, registry) = {
                    let mut operations = self.lock_operations()?;
                    let registry = operations.authority.registry(peer).map_err(map_core)?;
                    let activity =
                        layerx_wire::activity::decode_signed(&signed_activity, &registry)
                            .map_err(|_| HumanOperationError::Refused)?;
                    let did =
                        Did::new(activity.actor_did()).map_err(|_| HumanOperationError::Refused)?;
                    let identity = operations
                        .subject_identity(peer, &did)
                        .map_err(|_| HumanOperationError::Refused)?;
                    let bound = subject::for_activity(
                        &operations.store,
                        peer,
                        &signed_activity,
                        &registry,
                    )?;
                    let authority = operations.authority.authorized_activity(
                        &bound,
                        &signed_activity,
                        evidence.activity_id,
                    )?;
                    (identity, authority, registry)
                };
                return self.session_control.commit_owner_rotation(
                    &tenant,
                    agent_id,
                    &managed_agent::rotation::Projection {
                        custody_key: &custody_key,
                        signed_activity: &signed_activity,
                        evidence: evidence.into(),
                        identity: &identity,
                        authority: &authority,
                        registry: &registry,
                    },
                );
            }

            crate::human::HumanAgentJourneyKind::Reclaim { amount, currency } => {
                (0, amount, currency, 0, 0)
            }
            crate::human::HumanAgentJourneyKind::Rotate {
                challenge_delay_seconds,
                ready_at,
            } => (1, 0, String::new(), challenge_delay_seconds, ready_at),
            crate::human::HumanAgentJourneyKind::Recover {
                challenge_delay_seconds,
                ready_at,
            } => (2, 0, String::new(), challenge_delay_seconds, ready_at),
        };
        let evidence: managed_agent::FinalizationEvidence = evidence.into();
        if matches!(kind, 1 | 2) {
            let finalized = {
                let store = self
                    .store
                    .lock()
                    .map_err(|_| HumanOperationError::Unavailable)?;
                managed_agent::validate_authority_revocation(
                    &store,
                    &tenant,
                    agent_id,
                    (kind, delay, ready),
                    pre_observation,
                    post_observation,
                    evidence,
                )?
            };
            self.session_control
                .invalidate_finalized(&finalized)
                .map_err(|error| match error {
                    crate::session_control::SessionControlError::Unavailable => {
                        HumanOperationError::Unavailable
                    }
                    _ => HumanOperationError::Refused,
                })?;
        }
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::finalize_journey(
            &mut store,
            &tenant,
            agent_id,
            (kind, amount, &currency, delay, ready),
            pre_observation,
            post_observation,
            evidence,
        )
    }
    fn agent_archive(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        confirm_name: &str,
        (pre_observation, post_observation, session_observation): ([u8; 32], [u8; 32], [u8; 32]),
        evidence: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::finalize_archive(
            &mut store,
            &tenant,
            agent_id,
            confirm_name,
            (pre_observation, post_observation, session_observation),
            evidence.into(),
        )
    }
    fn capability_install(
        &mut self,
        peer: &HumanPeer,
        request: HumanCapabilityInstall,
    ) -> Result<HumanResponse, HumanOperationError> {
        install_capability(
            &mut self.lock_operations()?.authority,
            &self.store,
            peer,
            &request,
        )
    }
    fn agent_lifecycle_publish(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<crate::human::HumanAgentLifecycleSeed>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        if managed_agent::lifecycle_publish_digest(&request.operation)
            .map_err(|_| HumanOperationError::Refused)?
            != request.body_digest
        {
            return Err(HumanOperationError::Refused);
        }
        let record = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?
            .get(&tenant, SessionId(request.key))
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        if !record.open
            || record.request.tenant != tenant
            || record.request.authority
                != ProtocolAuthority::SessionKey(request.operation.protocol_grant_id)
        {
            return Err(HumanOperationError::Refused);
        }
        let agent = ManagedAgent::from_creation(
            &request.operation,
            std::str::from_utf8(record.request.agent.as_bytes())
                .map_err(|_| HumanOperationError::Refused)?,
            request.key,
            record.request.token_id,
            record.generation,
        )?;
        let companion = lifecycle_action_key(&tenant, request.key)?;
        let mut completed = Vec::with_capacity(34);
        completed.push(2);
        completed.extend(request.body_digest);
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if let Some(existing) = store.get(&companion) {
            if existing.class() != StorageClass::LocalOnly || existing.bytes() != completed {
                return Err(HumanOperationError::Refused);
            }
            return HumanResponse::new(vec![1]).map_err(|_| HumanOperationError::Refused);
        }
        managed_agent::publish_creation_with_companion(
            &mut store, &tenant, &agent, companion, completed,
        )?;
        HumanResponse::new(vec![1]).map_err(|_| HumanOperationError::Refused)
    }
    fn agent_context(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::context(&store, &tenant, agent_id)
    }
    fn agent_budget_state(
        &mut self,
        peer: &HumanPeer,
        active_budget_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        if active_budget_id == [0; 32] {
            return Err(HumanOperationError::Refused);
        }
        let state = self
            .lock_operations()?
            .authority
            .budget_state(peer, active_budget_id)?;
        let mut out = Encoder::new();
        out.fixed(&active_budget_id);
        out.u64(state.revocation_sequence);
        out.u64(state.observed_head_sequence);
        out.u8(state.verification);
        out.fixed(&state.evidence_digest);
        out.fixed(&state.receipt_digest);
        out.fixed(&state.checkpoint_digest);
        out.u64(state.age_sequences);
        out.u64(state.maximum_age_sequences);
        out.u128(state.remaining);
        out.fixed(&state.asset);
        let response = out.finish()?;
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::record_observation(
            &mut store,
            &tenant,
            1,
            state.evidence_digest,
            response.bytes().to_vec(),
        )?;
        Ok(response)
    }
    fn agent_key_policy(
        &mut self,
        peer: &HumanPeer,
        agent_did: &str,
        recovery: bool,
    ) -> Result<HumanResponse, HumanOperationError> {
        let did = Did::new(agent_did.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let state = {
            let mut operations = self.lock_operations()?;
            let registry = operations.authority.registry(peer).map_err(map_core)?;
            let bound = subject::for_did(&operations.store, peer, &did, &registry)?;
            operations
                .authority
                .key_rotation_policy(&bound, &did, recovery)?
        };
        let mut out = Encoder::new();
        out.text(agent_did)?;
        out.u8(u8::from(recovery));
        out.u64(state.policy_revision);
        out.u64(state.required_delay_seconds);
        out.u64(state.maximum_delay_seconds);
        out.u64(state.effective_sequence);
        out.u64(state.observed_head_sequence);
        out.u8(state.verification);
        out.fixed(&state.evidence_digest);
        out.fixed(&state.checkpoint_digest);
        out.u64(state.age_sequences);
        out.u64(state.maximum_age_sequences);
        let response = out.finish()?;
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        managed_agent::record_observation(
            &mut store,
            &tenant,
            2,
            state.evidence_digest,
            response.bytes().to_vec(),
        )?;
        Ok(response)
    }
    fn agent_session_snapshot(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let (did, session_id, token, generation) =
            managed_agent::session_coordinates(&store, &tenant, agent_id)?;
        let record = sessions
            .get(&tenant, SessionId(session_id))
            .ok_or(HumanOperationError::Refused)?;
        if record.request.tenant != tenant
            || record.request.token_id != token
            || record.generation != generation
        {
            return Err(HumanOperationError::Refused);
        }
        let mut out = Encoder::new();
        out.text(agent_id)?;
        out.text(&did)?;
        out.fixed(&session_id);
        out.fixed(&token);
        out.u64(generation);
        out.u8(u8::from(record.open));
        out.u64(record.request.expiry_sequence);
        out.u64(record.sequence);
        out.finish()
    }
    fn agent_session_suspend(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        action_key: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut sessions = self
            .sessions
            .write()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let (_, session_id, _, _) = managed_agent::session_coordinates(&store, &tenant, agent_id)?;
        let grant = managed_agent::protocol_grant(&store, &tenant, agent_id)?;
        let was_open = sessions
            .get(&tenant, SessionId(session_id))
            .map(|record| record.open)
            .ok_or(HumanOperationError::Refused)?;
        if !was_open {
            return managed_agent::record_session_observation(
                &mut store, &tenant, agent_id, action_key, false, false,
            );
        }
        let (response, key, bytes) = managed_agent::prepare_session_observation(
            &store, &tenant, agent_id, action_key, false,
        )?;
        session::close_with_companion(
            &mut store,
            &mut sessions,
            &tenant,
            SessionId(session_id),
            key,
            bytes,
        )
        .map_err(|_| HumanOperationError::Unavailable)?;
        drop(store);
        drop(sessions);
        self.session_keys
            .revoke(grant)
            .map_err(|_| HumanOperationError::Unavailable)?;
        Ok(response)
    }
    fn agent_session_bind(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        session_id: [u8; 32],
        token_id: [u8; 32],
        action_key: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let (did, _, _, _) = managed_agent::session_coordinates(&store, &tenant, agent_id)?;
        let agent = Did::new(did.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let token = sessions
            .authenticate_bearer(&tenant, SessionId(session_id), token_id)
            .map_err(|_| HumanOperationError::Refused)?;
        token
            .boundary(&sessions)
            .map_err(|_| HumanOperationError::Refused)?;
        if token.tenant() != &tenant || token.agent() != &agent {
            return Err(HumanOperationError::Refused);
        }
        let Some(ProtocolAuthority::SessionKey(authority)) = sessions
            .get(&tenant, SessionId(session_id))
            .map(|record| record.request.authority.clone())
        else {
            return Err(HumanOperationError::Refused);
        };
        managed_agent::bind_session(
            &mut store,
            &tenant,
            agent_id,
            (session_id, token_id, token.generation(), authority),
            action_key,
        )
    }
    fn agent_session_restrict(
        &mut self,
        peer: &HumanPeer,
        agent_id: &str,
        current_sequence: u64,
        action_key: [u8; 32],
        permitted_activity_types: Vec<u16>,
        scopes: Vec<String>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        self.session_control
            .restrict_managed_agent(
                &tenant,
                agent_id,
                scopes.into_iter().collect(),
                permitted_activity_types.into_iter().collect(),
                current_sequence,
                action_key,
            )
            .map(|(_, _, response)| response)
            .map_err(|error| match error {
                crate::session_control::SessionControlError::Unavailable => {
                    HumanOperationError::Unavailable
                }
                _ => HumanOperationError::Refused,
            })
    }

    fn budget_list(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::budget::BudgetList,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.tenant.as_str() != peer.tenant {
            return Err(HumanOperationError::Refused);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let owners = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            managed_agent::budget_owners(&store, &tenant)?
        };
        let owners: Vec<&managed_agent::BudgetOwner> = owners
            .iter()
            .filter(|owner| owner.agent_did == request.agent_did.as_str())
            .collect();
        let mut out = Encoder::new();
        out.u16(owners.len())?;
        for owner in owners {
            out.fixed(&owner.active_budget_id);
            out.text(&owner.agent_id)?;
        }
        out.finish()
    }

    fn subscription_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::subscription::SubscriptionCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_create(context, control, request)
    }

    fn subscription_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionList,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_list(context, control, request)
    }

    fn subscription_pause(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_pause(context, control, request)
    }

    fn subscription_resume(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_resume(context, control, request)
    }

    fn subscription_delete(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_delete(context, control, request)
    }

    fn subscription_health(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_health(context, control, request)
    }

    fn subscription_acknowledge(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::CursorAcknowledgement,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .subscription_acknowledge(context, control, request)
    }

    fn session_refresh(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        _control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::SessionRefresh,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.rpc_session_refresh(context, request)
    }
    fn session_close(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        _control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::SessionClose,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.rpc_session_close(context, request)
    }

    fn availability_fetch(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::availability::AvailabilityRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?.availability_fetch(peer, request)
    }

    fn capability_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .capability_create(context, control, request)
    }

    fn capability_attenuate(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityAttenuate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .capability_attenuate(context, control, request)
    }

    fn capability_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::capability::CapabilityList,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .capability_list(context, control, request)
    }

    fn capability_revoke(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityRevoke>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let mut operations = self.lock_operations()?;
        let response = operations.capability_revoke(context, control, request)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        sweep_capability_cleanups(control, &self.preparation_lifecycle, &tenant)?;
        drop(operations);
        Ok(response)
    }

    fn budget_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetCreate>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        self.lock_operations()?
            .budget_create(context, control, request)
    }

    fn budget_fund(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetFund>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        self.lock_operations()?
            .budget_fund(context, control, request)
    }

    fn budget_revoke(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetTarget>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        let target = &request.operation.request;
        let budget_id = digest_from_hex(target.budget_id.as_str())
            .ok_or(HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
        let tenant = TenantId::new(target.tenant.as_str().to_owned())
            .map_err(|_| HumanOperationError::Refused)?;
        let actor =
            Did::new(target.agent_did.as_str().as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let mut operations = self.lock_operations()?;
        let response = operations.budget_revoke(context, control, request)?;
        let snapshot = core_preparation_snapshot(&mut operations.node, context.peer(), &actor)?;
        drop(operations);
        sweep_budget_revocation(
            control,
            &self.preparation_lifecycle,
            &tenant,
            crate::budget::daemon_limit_id(budget_id),
            snapshot.protocol_timestamp,
            snapshot.observed_head_sequence,
        )?;
        Ok(response)
    }

    fn budget_state(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::budget::BudgetTarget,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        self.lock_operations()?
            .budget_state(context, control, request)
    }

    fn export_offline(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<Vec<layerx_agent_api::export::FactRef>>,
    ) -> Result<
        layerx_agent_api::read::VerifiedRead<layerx_agent_api::export::OfflineExport>,
        HumanOperationError,
    > {
        self.lock_operations()?.export_offline(peer, request)
    }

    fn fee_projection(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::FeeProjectionRequest,
    ) -> Result<
        layerx_agent_api::read::ProjectionResult<layerx_agent_api::read::FeeProjection>,
        HumanOperationError,
    > {
        self.lock_operations()?.fee_projection(peer, request)
    }

    fn policy_dry_run(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::policy::PolicyDryRunRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.lock_operations()?
            .policy_dry_run(context, control, request)
    }

    fn policy_dry_run_legacy(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::LegacyPolicyDryRun,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<layerx_agent_api::policy::PolicyDryRunResult>,
        HumanOperationError,
    > {
        self.lock_operations()?
            .policy_dry_run_legacy(context, control, request)
    }
}

struct FixedIdentity(CoreIdentity);
impl IdentityResolver for FixedIdentity {
    fn resolve(&mut self, _did: &Did) -> Result<Option<CoreIdentity>, IdentityError> {
        Ok(Some(self.0.clone()))
    }
}
enum OwnerAction {
    Pending,
    Validated((CoreIdentity, u64, u64), Option<[u8; 32]>),
    Completed(HumanResponse),
}
fn pending_owner_action(bytes: &[u8], body: [u8; 32]) -> bool {
    bytes.len() == 34 && bytes[0] == 1 && bytes[1] == 0 && bytes[2..] == body
}
fn owner_action_key(
    tenant: &TenantId,
    action: [u8; 32],
) -> Result<crate::store::TenantKey, HumanOperationError> {
    let mut id = b"human-owner-action-v1:".to_vec();
    id.extend_from_slice(&action);
    key(tenant.clone(), ObjectKind::Idempotency, id).map_err(|_| HumanOperationError::Refused)
}
fn lifecycle_action_key(
    tenant: &TenantId,
    action: [u8; 32],
) -> Result<crate::store::TenantKey, HumanOperationError> {
    let mut id = b"human-lifecycle-publish-v1:".to_vec();
    id.extend_from_slice(&action);
    key(tenant.clone(), ObjectKind::Idempotency, id).map_err(|_| HumanOperationError::Refused)
}
fn encode_owner_validated(
    body: [u8; 32],
    value: &(CoreIdentity, u64, u64),
    token_id: [u8; 32],
) -> Result<Vec<u8>, HumanOperationError> {
    if token_id == [0; 32] {
        return Err(HumanOperationError::Refused);
    }
    let mut out = Encoder::new();
    out.u8(1);
    out.u8(1);
    out.fixed(&body);
    out.u64(value.0.head_sequence);
    out.u64(value.0.revocation_sequence);
    out.u8(value.0.verification_level.wire_rank());
    out.u8(u8::from(value.0.frozen));
    out.u16(value.0.authorities.len())?;
    for authority in &value.0.authorities {
        match authority {
            ProtocolAuthority::PrimaryKey(id) => {
                out.u8(1);
                out.fixed(id);
            }
            ProtocolAuthority::SessionKey(id) => {
                out.u8(2);
                out.fixed(id);
            }
            ProtocolAuthority::CapabilityGrant(id) => {
                out.u8(3);
                out.fixed(id);
            }
        }
    }
    out.bytes(&value.0.canonical_bytes)?;
    out.u64(value.1);
    out.u64(value.2);
    out.fixed(&token_id);
    Ok(out.0)
}
fn decode_owner_action(bytes: &[u8], body: [u8; 32]) -> Result<OwnerAction, HumanOperationError> {
    let mut input = OwnerInput { bytes, at: 0 };
    if input.u8()? != 1 {
        return Err(HumanOperationError::Refused);
    }
    let state = input.u8()?;
    if input.fixed() != Some(body) {
        return Err(HumanOperationError::Refused);
    }
    if state == 2 {
        return Ok(OwnerAction::Completed(
            HumanResponse::new(input.remaining().to_vec())
                .map_err(|_| HumanOperationError::Refused)?,
        ));
    }
    if state != 1 {
        return Err(HumanOperationError::Refused);
    }
    let head_sequence = input.u64()?;
    let revocation_sequence = input.u64()?;
    let verification_level = rank(input.u8()?)?;
    let frozen = match input.u8()? {
        0 => false,
        1 => true,
        _ => return Err(HumanOperationError::Refused),
    };
    let count = usize::from(input.u16()?);
    if count == 0 || count > 256 {
        return Err(HumanOperationError::Refused);
    }
    let mut authorities = Vec::with_capacity(count);
    for _ in 0..count {
        let tag = input.u8()?;
        let id = input.fixed().ok_or(HumanOperationError::Refused)?;
        authorities.push(match tag {
            1 => ProtocolAuthority::PrimaryKey(id),
            2 => ProtocolAuthority::SessionKey(id),
            3 => ProtocolAuthority::CapabilityGrant(id),
            _ => return Err(HumanOperationError::Refused),
        });
    }
    let canonical_bytes = input.bytes()?;
    let expiry = input.u64()?;
    let observed = input.u64()?;
    let allocated_token_id = if input.remaining().is_empty() {
        None
    } else {
        let token_id = input.fixed().ok_or(HumanOperationError::Refused)?;
        if token_id == [0; 32] || !input.remaining().is_empty() {
            return Err(HumanOperationError::Refused);
        }
        Some(token_id)
    };
    Ok(OwnerAction::Validated(
        (
            CoreIdentity {
                canonical_bytes,
                head_sequence,
                revocation_sequence,
                verification_level,
                frozen,
                authorities,
            },
            expiry,
            observed,
        ),
        allocated_token_id,
    ))
}
struct OwnerInput<'a> {
    bytes: &'a [u8],
    at: usize,
}
impl OwnerInput<'_> {
    fn take(&mut self, n: usize) -> Result<&[u8], HumanOperationError> {
        let end = self.at.checked_add(n).ok_or(HumanOperationError::Refused)?;
        let out = self
            .bytes
            .get(self.at..end)
            .ok_or(HumanOperationError::Refused)?;
        self.at = end;
        Ok(out)
    }
    fn u8(&mut self) -> Result<u8, HumanOperationError> {
        Ok(self.take(1)?[0])
    }
    fn u16(&mut self) -> Result<u16, HumanOperationError> {
        Ok(u16::from_be_bytes(
            self.take(2)?
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        ))
    }
    fn u64(&mut self) -> Result<u64, HumanOperationError> {
        Ok(u64::from_be_bytes(
            self.take(8)?
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        ))
    }
    fn fixed(&mut self) -> Option<[u8; 32]> {
        self.take(32).ok()?.try_into().ok()
    }
    fn bytes(&mut self) -> Result<Vec<u8>, HumanOperationError> {
        let n = u32::from_be_bytes(
            self.take(4)?
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        ) as usize;
        Ok(self.take(n)?.to_vec())
    }
    fn remaining(&self) -> &[u8] {
        &self.bytes[self.at..]
    }
}
fn rank(value: u8) -> Result<VerificationLevel, HumanOperationError> {
    match value {
        1 => Ok(VerificationLevel::SEQUENCER_SIGNED),
        2 => Ok(VerificationLevel::BATCH_INCLUDED),
        3 => Ok(VerificationLevel::STATE_PROVEN),
        4 => Ok(VerificationLevel::CHECKPOINT_FINALISED),
        5 => Ok(VerificationLevel::SETTLEMENT_ANCHORED),
        _ => Err(HumanOperationError::Refused),
    }
}

impl<A: HumanAuthorityBoundary> UnifiedAgentOwner<A> {
    fn owner_replay(
        &self,
        action: &TenantKey,
        body_digest: [u8; 32],
    ) -> Result<Option<OwnerAction>, HumanOperationError> {
        Ok({
            let mut store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            if let Some(value) = store.get(action) {
                Some(if pending_owner_action(value.bytes(), body_digest) {
                    OwnerAction::Pending
                } else {
                    decode_owner_action(value.bytes(), body_digest)?
                })
            } else {
                let mut pending = Vec::with_capacity(34);
                pending.push(1);
                pending.push(0);
                pending.extend_from_slice(&body_digest);
                store
                    .put_local(action.clone(), pending)
                    .map_err(|_| HumanOperationError::Unavailable)?;
                None
            }
        })
    }
    fn provision_owner_session(
        &self,
        request: &HumanOwnerInstall,
    ) -> Result<(), HumanOperationError> {
        let issued = validate_issued_session(
            &request.registration_payload,
            request.grantor,
            request.session_public_key,
            request.grant_not_before,
            request.grant_expires_at,
            request.grant_revocation_sequence,
            &request.permitted_activity_types,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        match self.session_keys.provision(
            request.authority_id,
            request
                .session_secret
                .as_ref()
                .ok_or(HumanOperationError::Refused)?
                .as_seed(),
            &issued,
        ) {
            Ok(()) => {}
            Err(crate::session_keys::SessionKeyRegistryError::Exists) => {
                let signer = self
                    .session_keys
                    .load(request.authority_id, issued)
                    .map_err(|_| HumanOperationError::Refused)?;
                drop(signer);
            }
            Err(_) => return Err(HumanOperationError::Unavailable),
        }
        Ok(())
    }
    fn install_owner_session(
        &mut self,
        tenant: &TenantId,
        action: TenantKey,
        request: MutationEnvelope<HumanOwnerInstall>,
        validated: &(CoreIdentity, u64, u64),
        allocated_token_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let did = Did::new(request.operation.agent.as_bytes())
            .map_err(|_| HumanOperationError::Refused)?;
        let installed_agent = request.operation.agent.clone();
        let lifecycle = request.operation.lifecycle.clone();
        let mut resolver = FixedIdentity(validated.0.clone());
        let mut sessions = self
            .sessions
            .write()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let identity = identity::register(&mut store, tenant.clone(), did.clone(), &mut resolver)
            .map_err(|error| map_identity_operation(&error))?;
        self.provision_owner_session(&request.operation)?;
        let open_request = OpenRequest {
            session_id: SessionId(request.operation.session_id),
            token_id: allocated_token_id,
            tenant: tenant.clone(),
            agent: did,
            authority: owner_authority(&request.operation)?,
            permitted_activity_types: request
                .operation
                .permitted_activity_types
                .iter()
                .copied()
                .collect(),
            scopes: request.operation.scopes.iter().cloned().collect(),
            expiry_sequence: validated.1,
            expiry_seconds: None,
            opening_client: request.operation.opening_client,
            policy_version: request.operation.policy_version,
        };
        if let Some(existing) = sessions.get(tenant, open_request.session_id) {
            if !existing.open || existing.request != open_request {
                return Err(HumanOperationError::Refused);
            }
        } else {
            session::open(
                &mut store,
                &mut sessions,
                &identity,
                open_request,
                validated.2,
            )
            .map_err(|_| HumanOperationError::Refused)?;
        }
        let installed_generation = sessions
            .generation(tenant, SessionId(request.operation.session_id))
            .ok_or(HumanOperationError::Unavailable)?;
        if let Some(seed) = lifecycle.as_ref() {
            managed_agent::publish_creation(
                &mut store,
                tenant,
                &ManagedAgent::from_creation(
                    seed,
                    &installed_agent,
                    request.operation.session_id,
                    allocated_token_id,
                    installed_generation,
                )?,
            )?;
        }
        let mut out = Encoder::new();
        out.fixed(&allocated_token_id);
        out.fixed(&request.operation.session_id);
        out.u64(installed_generation);
        out.u64(validated.1);
        out.u64(validated.2);
        let response = out.finish()?;
        let mut completed = Vec::with_capacity(34 + response.bytes().len());
        completed.push(1);
        completed.push(2);
        completed.extend_from_slice(&request.body_digest);
        completed.extend_from_slice(response.bytes());
        store
            .put_local(action, completed)
            .map_err(|_| HumanOperationError::Unavailable)?;
        Ok(response)
    }
    fn persist_owner_validation(
        &self,
        tenant: &TenantId,
        action: &TenantKey,
        body_digest: [u8; 32],
        session_id: [u8; 32],
        validated: (CoreIdentity, u64, u64),
    ) -> Result<OwnerAction, HumanOperationError> {
        let sessions = self
            .sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let current = store.get(action).ok_or(HumanOperationError::Unavailable)?;
        let decoded = if pending_owner_action(current.bytes(), body_digest) {
            OwnerAction::Pending
        } else {
            decode_owner_action(current.bytes(), body_digest)?
        };
        match decoded {
            OwnerAction::Completed(response) => return Ok(OwnerAction::Completed(response)),
            OwnerAction::Validated(value, Some(token_id)) => {
                return Ok(OwnerAction::Validated(value, Some(token_id)))
            }
            OwnerAction::Pending | OwnerAction::Validated(_, None) => {}
        }
        let token_id = match sessions.get(tenant, SessionId(session_id)) {
            Some(existing) if existing.request.token_id != [0; 32] => existing.request.token_id,
            Some(_) => return Err(HumanOperationError::Refused),
            None => {
                let mut generated = None;
                for _ in 0..8 {
                    let mut token_id = [0_u8; 32];
                    getrandom::fill(&mut token_id).map_err(|_| HumanOperationError::Unavailable)?;
                    if token_id != [0; 32] {
                        generated = Some(token_id);
                        break;
                    }
                }
                generated.ok_or(HumanOperationError::Unavailable)?
            }
        };
        store
            .put_local(
                action.clone(),
                encode_owner_validated(body_digest, &validated, token_id)?,
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
        Ok(OwnerAction::Validated(validated, Some(token_id)))
    }

    fn validate_owner(
        &mut self,
        peer: &HumanPeer,
        request: &HumanOwnerInstall,
    ) -> Result<(CoreIdentity, u64, u64), HumanOperationError> {
        if request.authority_kind != 2
            || request.session_id == [0; 32]
            || request.token_id != [0; 32]
            || request.session_public_key == [0; 32]
            || request.grantor == [0; 32]
            || request.registration_payload.is_empty()
            || request.registration_payload.len() > 1024
            || request.grant_not_before == 0
            || request.grant_expires_at <= request.grant_not_before
            || request.grant_revocation_sequence == 0
            || request.session_secret.is_none()
            || request.permitted_activity_types.is_empty()
            || request.scopes.is_empty()
            || request.opening_client.is_empty()
            || request.policy_version.is_empty()
        {
            return Err(HumanOperationError::Refused);
        }
        let did = Did::new(request.agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let grantor = layerx_wire::hash::did_id_for_protocol(&did, 3)
            .map_err(|_| HumanOperationError::Refused)?;
        let (_, _, _, _, account_age, maximum_account_age, _) =
            self.lock_operations()?.authority.balance_context(peer)?;
        if request.grantor != grantor
            || maximum_account_age == 0
            || account_age > maximum_account_age
        {
            return Err(HumanOperationError::Refused);
        }
        if request
            .lifecycle
            .as_ref()
            .is_some_and(|lifecycle| lifecycle.protocol_grant_id != request.authority_id)
        {
            return Err(HumanOperationError::Refused);
        }
        let issued = validate_issued_session(
            &request.registration_payload,
            request.grantor,
            request.session_public_key,
            request.grant_not_before,
            request.grant_expires_at,
            request.grant_revocation_sequence,
            &request.permitted_activity_types,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        if issued.grant_id != request.authority_id {
            return Err(HumanOperationError::Refused);
        }
        let provisioned = ProvisionedSessionKey::from_seed(
            request
                .session_secret
                .as_ref()
                .ok_or(HumanOperationError::Refused)?
                .as_seed(),
            issued,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        drop(provisioned);
        let identity = self
            .lock_operations()?
            .subject_identity(peer, &did)
            .map_err(|error| map_identity_operation(&error))?;
        if identity.frozen
            || identity.head_sequence == 0
            || identity.revocation_sequence != request.grant_revocation_sequence
            || identity.canonical_bytes.len() != 223
            || !identity.canonical_bytes.starts_with(b"LXGI1")
            || identity.canonical_bytes[5..37] != grantor
            || identity.verification_level < VerificationLevel::CHECKPOINT_FINALISED
            || !identity.authorities.contains(&owner_authority(request)?)
        {
            return Err(HumanOperationError::Refused);
        }
        let attestation = self.lock_operations()?.authority.lease_attestation(peer)?;
        let (_, expiry) = attestation.map(
            request.lease_not_before_unix_ms,
            request.lease_not_after_unix_ms,
        )?;
        if request.lease_not_before_unix_ms < request.grant_not_before
            || request.lease_not_after_unix_ms > request.grant_expires_at
        {
            return Err(HumanOperationError::Refused);
        }
        Ok((identity, expiry, attestation.observed_head_sequence))
    }
}

fn install_capability<A: HumanAuthorityBoundary>(
    authority: &mut A,
    shared_store: &Arc<Mutex<Store>>,
    peer: &HumanPeer,
    request: &HumanCapabilityInstall,
) -> Result<HumanResponse, HumanOperationError> {
    if request.action_key == [0; 32]
        || request.capability_id == [0; 32]
        || request.authority_id == [0; 32]
        || request.activity_types.is_empty()
        || request.counterparties.is_empty()
        || request.assets.is_empty()
        || request.purposes.is_empty()
        || request.amount_ceiling == 0
        || request.rate_maximum_uses == 0
        || request.rate_window_sequences == 0
        || request.expiry_sequence == 0
    {
        return Err(HumanOperationError::Refused);
    }
    let tenant = TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
    let did = Did::new(request.agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
    let replay_key = capability_action_key(&tenant, request.action_key)?;
    let request_digest = capability_request_digest(request);
    {
        let store = shared_store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if let Some(existing) = store.get(&replay_key) {
            if existing.class() != StorageClass::LocalOnly {
                return Err(HumanOperationError::Refused);
            }
            return decode_capability_action(existing.bytes(), request_digest);
        }
    }
    {
        let mut store = shared_store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut pending = Vec::with_capacity(34);
        pending.push(1);
        pending.extend_from_slice(&request_digest);
        pending.push(0);
        store
            .put_local(replay_key.clone(), pending)
            .map_err(|_| HumanOperationError::Unavailable)?;
    }
    let registry = authority.registry(peer).map_err(map_core)?;
    let bound = subject::for_did(shared_store, peer, &did, &registry)?;
    let (capability, observed) = validate_capability(authority, &bound, &tenant, &did, request)?;
    let mut store = shared_store
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?;
    let existing_capability =
        Capability::restore(&store, tenant.clone(), CapabilityId(request.capability_id))
            .map_err(|_| HumanOperationError::Unavailable)?;
    if let Some(existing) = existing_capability.as_ref() {
        if *existing != capability {
            return Err(HumanOperationError::Refused);
        }
    }
    let mut out = Encoder::new();
    out.fixed(&request.capability_id);
    out.u64(observed.observed_sequence);
    out.u8(observed.verification);
    out.fixed(&observed.evidence_digest);
    let response = out.finish()?;
    let mut completed = Vec::with_capacity(34 + response.bytes().len());
    completed.push(1);
    completed.extend_from_slice(&request_digest);
    completed.push(1);
    completed.extend_from_slice(response.bytes());
    if existing_capability.is_some() {
        store
            .put_local(replay_key, completed)
            .map_err(|_| HumanOperationError::Unavailable)?;
    } else {
        let capability_key = key(
            tenant,
            ObjectKind::Capability,
            request.capability_id.to_vec(),
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let capability_bytes =
            crate::capability::encode(&capability).map_err(|_| HumanOperationError::Refused)?;
        store
            .update_local_with_companion(replay_key, completed, capability_key, capability_bytes)
            .map_err(|_| HumanOperationError::Unavailable)?;
    }
    Ok(response)
}

fn capability_action_key(
    tenant: &TenantId,
    action_key: [u8; 32],
) -> Result<crate::store::TenantKey, HumanOperationError> {
    let mut id = b"human-capability-action-v1:".to_vec();
    id.extend_from_slice(&action_key);
    key(tenant.clone(), ObjectKind::Idempotency, id).map_err(|_| HumanOperationError::Refused)
}

fn capability_request_digest(request: &HumanCapabilityInstall) -> [u8; 32] {
    fn field(digest: &mut Sha256, value: &[u8]) {
        digest.update(u32::try_from(value.len()).unwrap_or(u32::MAX).to_be_bytes());
        digest.update(value);
    }
    let mut digest = Sha256::new();
    digest.update(b"layerx-agentd/human-capability-action/v1\0");
    digest.update(request.action_key);
    field(&mut digest, request.agent.as_bytes());
    digest.update(request.authority_id);
    digest.update(request.capability_id);
    digest.update(
        u32::try_from(request.activity_types.len())
            .unwrap_or(u32::MAX)
            .to_be_bytes(),
    );
    for value in &request.activity_types {
        digest.update(value.to_be_bytes());
    }
    digest.update(
        u32::try_from(request.counterparties.len())
            .unwrap_or(u32::MAX)
            .to_be_bytes(),
    );
    for value in &request.counterparties {
        digest.update(value);
    }
    digest.update(
        u32::try_from(request.assets.len())
            .unwrap_or(u32::MAX)
            .to_be_bytes(),
    );
    for value in &request.assets {
        digest.update(value);
    }
    digest.update(request.amount_ceiling.to_be_bytes());
    digest.update(request.rate_maximum_uses.to_be_bytes());
    digest.update(request.rate_window_sequences.to_be_bytes());
    digest.update(
        u32::try_from(request.purposes.len())
            .unwrap_or(u32::MAX)
            .to_be_bytes(),
    );
    for value in &request.purposes {
        field(&mut digest, value.as_bytes());
    }
    digest.update(request.expiry_sequence.to_be_bytes());
    digest.finalize().into()
}

fn decode_capability_action(
    bytes: &[u8],
    expected: [u8; 32],
) -> Result<HumanResponse, HumanOperationError> {
    if bytes.len() < 34 || bytes[0] != 1 || bytes[1..33] != expected {
        return Err(HumanOperationError::Refused);
    }
    match bytes[33] {
        0 => Err(HumanOperationError::Unavailable),
        1 => HumanResponse::new(bytes[34..].to_vec()).map_err(|_| HumanOperationError::Refused),
        _ => Err(HumanOperationError::Refused),
    }
}

fn persisted_terminal_receipt_matches(canonical: &[u8], expected: [u8; 32]) -> bool {
    Sha256::digest(canonical)[..] == expected
}

fn agent_evidence_digest(
    action_key: [u8; 32],
    object_id: [u8; 32],
    observed_sequence: u64,
    verification: u8,
) -> [u8; 32] {
    let mut digest = Sha256::new();
    let stage = [5_u8];
    let sequence = observed_sequence.to_be_bytes();
    let rank = [verification];
    for part in [
        b"layerx-human/agent-create/agent-evidence/v1".as_slice(),
        stage.as_slice(),
        action_key.as_slice(),
        object_id.as_slice(),
        sequence.as_slice(),
        rank.as_slice(),
    ] {
        digest.update(u32::try_from(part.len()).unwrap_or(u32::MAX).to_be_bytes());
        digest.update(part);
    }
    digest.finalize().into()
}

impl<A: HumanAuthorityBoundary> ProductionHumanOperations<A> {
    /// Creates one managed agent's protocol budget through the ordinary
    /// prepare, external-sign, verify and submit path, then confirms the
    /// record from proven core state before it is recorded as active.
    ///
    /// The command is audited through the admin surface first. The signed
    /// activity must be the cached preparation named by `preparation`, must
    /// decode as a canonical budget-create payload whose `budget_id` is the
    /// managed agent's digest, and the tenant must not already hold a live
    /// protocol budget for that agent.
    fn create_tenant_budget(
        &mut self,
        peer: &HumanPeer,
        operator_id: &str,
        request_id: [u8; 32],
        command: OperatorCommand,
    ) -> Result<HumanResponse, HumanOperationError> {
        let OperatorCommand::CreateBudget {
            agent,
            asset,
            ceiling,
            expiry_sequence,
            preparation,
            signer_public_key,
            signature,
        } = command
        else {
            return Err(HumanOperationError::Refused);
        };
        if !self.unified_owner_active {
            return Err(HumanOperationError::Unavailable);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let (surface, plan) =
            self.admitted_client_write(&tenant, operator_id, request_id, command)?;
        let prepared_key = (
            peer.tenant.clone(),
            peer.principal.clone(),
            hex(&preparation),
        );
        let (operator_cached, operator_submission) =
            self.verified_budget_preparation(&prepared_key, signature, &signer_public_key, agent)?;
        let operator_expiry_ms =
            budget_create_identity(operator_submission.exact_bytes(), &operator_cached.registry)
                .map_err(budget_refusal)?
                .expiry_ms;
        let observed_head_sequence = core_preparation_snapshot(
            &mut self.node,
            peer,
            operator_cached.prepared.envelope.actor_did(),
        )?
        .observed_head_sequence;
        if expiry_sequence <= observed_head_sequence {
            return Err(HumanOperationError::Typed(
                crate::human::HumanRefusal::BudgetExpired,
            ));
        }

        let budget = self
            .confirm_protocol_budget(
                peer,
                &tenant,
                &prepared_key,
                signature,
                signer_public_key,
                agent,
                asset,
                ceiling,
                operator_expiry_ms,
            )?
            .map_err(budget_refusal)?;
        budget_creation_response(plan, &budget, surface.audit_entries())
    }

    #[allow(clippy::too_many_arguments)]
    fn confirm_protocol_budget(
        &mut self,
        peer: &HumanPeer,
        tenant: &TenantId,
        prepared_key: &(String, String, String),
        signature: [u8; 64],
        signer_public_key: [u8; 32],
        agent: [u8; 32],
        asset: [u8; 32],
        ceiling: u128,
        expiry_ms: u64,
    ) -> Result<Result<ProtocolBudget, BudgetCreationError>, HumanOperationError> {
        let (cached, submission) =
            self.verified_budget_preparation(prepared_key, signature, &signer_public_key, agent)?;
        let candidate = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            managed_agent::budget_candidate(&store, tenant, agent)?
        };
        if candidate.state == 4 {
            return Err(HumanOperationError::Refused);
        }
        let core_time_ms =
            core_preparation_snapshot(&mut self.node, peer, cached.prepared.envelope.actor_did())?
                .protocol_timestamp;
        let (verifier, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let correlation = boundary_correlation(peer, &agent, b"budget-create");
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &cached.registry,
            signer: signer_public_key,
            correlation,
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        if candidate.active_budget_id != [0; 32]
            && live_protocol_budget(&mut pipeline, &verifier, candidate.active_budget_id)
        {
            return Err(HumanOperationError::Refused);
        }
        let request = BudgetRequest {
            tenant: tenant.clone(),
            request_id: submission.idempotency_key(),
            kind: BudgetKind::ProtocolBudget,
            asset,
            ceiling,
            expiry_ms,
            core_time_ms,
            canonical_activity: submission.exact_bytes().to_vec(),
            verified_submission: Some(submission),
        };
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let budget = match create_protocol_budget(
            &mut store,
            &request,
            &cached.registry,
            &verifier,
            &mut pipeline,
        ) {
            Ok(budget) => budget,
            Err(error) => return Ok(Err(error)),
        };
        managed_agent::assign_budget(&mut store, tenant, &candidate.agent_id, budget.object_id())?;
        drop(store);
        self.prepared.remove(prepared_key);
        Ok(Ok(budget))
    }

    fn budget_read_parts(
        &mut self,
        peer: &HumanPeer,
    ) -> Result<(EvidenceAuthority, [u8; 32], SequencerAuthorization), HumanOperationError> {
        let node = self.node.handshake().node().clone();
        let verifier = EvidenceAuthority::pinned_to_handshake(
            node.protocol_version,
            node.network_id,
            node.authorised_sequencer_key,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let (_, _, _, _, _, _, authorization) = self.authority.balance_context(peer)?;
        Ok((verifier, node.authorised_sequencer_key, authorization))
    }

    fn budget_mutation_context(
        &mut self,
        peer: &HumanPeer,
        activity_type: ActivityType,
        payload: &[u8],
        actor: &Did,
        authority: &Authority,
    ) -> Result<Option<layerx_crypto::disclosure::BudgetStateContext>, HumanOperationError> {
        if activity_type.module() != layerx_types::payload::ModuleId::Budget
            || !matches!(activity_type.ordinal(), 2 | 8 | 9)
        {
            return Ok(None);
        }
        let budget_id: [u8; 32] = payload
            .get(2..34)
            .and_then(|bytes| bytes.try_into().ok())
            .ok_or(HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
        let Authority::Owner(owner) = authority else {
            return Err(HumanOperationError::Refused);
        };
        let signer: [u8; 32] = owner
            .as_ref()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let head = core_preparation_snapshot(&mut self.node, peer, actor)?.observed_head_sequence;
        let (verifier, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &registry,
            signer,
            correlation: boundary_correlation(peer, actor.as_bytes(), b"budget-context"),
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        let state = BudgetPipeline::budget_state(&mut pipeline, budget_id).map_err(budget_refusal)?;
        let proven = verifier
            .verify_state(&state.evidence)
            .map_err(|_| HumanOperationError::Refused)?;
        let record = ProtocolBudgetRecord::decode(proven.canonical_state())
            .map_err(|_| HumanOperationError::Refused)?;
        if record.budget_id != budget_id {
            return Err(HumanOperationError::Refused);
        }
        let balance = crate::budget::BudgetMutationPipeline::budget_balance(
            &mut pipeline,
            record.budget_account,
            record.asset_id,
        )
        .map_err(budget_refusal)?;
        crate::budget::budget_state_context(&record, proven.canonical_state(), head, balance)
            .map(Some)
            .map_err(budget_refusal)
    }

    fn require_budget_owner(
        &self,
        tenant: &TenantId,
        agent_did: &str,
        budget_id: [u8; 32],
    ) -> Result<(), HumanOperationError> {
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        if managed_agent::budget_owners(&store, tenant)?
            .iter()
            .any(|owner| owner.active_budget_id == budget_id && owner.agent_did == agent_did)
        {
            Ok(())
        } else {
            Err(HumanOperationError::Refused)
        }
    }

    fn daemon_limit_record(
        &self,
        tenant: &TenantId,
        budget_id: [u8; 32],
    ) -> Result<Option<crate::budget::DaemonLimitRecord>, HumanOperationError> {
        let store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        Ok(crate::budget::daemon_limits(&store, tenant)
            .map_err(daemon_limit_refusal)?
            .into_iter()
            .find(|record| record.budget_id == budget_id))
    }

    fn admit_budget_submit(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        tenant: &TenantId,
        cached: &CachedPreparation,
        preparation_ref: &str,
        submission: &VerifiedSubmission,
    ) -> Result<([u8; 32], u64), HumanOperationError> {
        let preparation_id =
            digest_from_hex(preparation_ref).ok_or(HumanOperationError::Refused)?;
        let snapshot = core_preparation_snapshot(
            &mut self.node,
            context.peer(),
            cached.prepared.envelope.actor_did(),
        )?;
        let charge = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            budget_write_charge(
                &store,
                tenant,
                &cached.prepared,
                snapshot.protocol_timestamp,
                snapshot.observed_head_sequence,
            )?
        };
        let permit = context.permit();
        permit
            .admit_write(
                control,
                crate::session_control::WriteAdmission {
                    stage: crate::session_control::AdmissionStage::Submit,
                    preparation_id,
                    charge,
                    extensions: Vec::new(),
                    current_sequence: snapshot.observed_head_sequence,
                    core_time_ms: snapshot.protocol_timestamp,
                    planner: None,
                },
            )
            .map_err(rpc_commit_error)?;
        permit
            .submit_with_external_signature(
                control,
                preparation_id,
                submission.exact_bytes().to_vec(),
                submission.activity_id(),
                snapshot.observed_head_sequence,
                snapshot.protocol_timestamp,
            )
            .map_err(rpc_commit_error)?;
        Ok((preparation_id, snapshot.observed_head_sequence))
    }

    fn owned_budget_mutation(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        tenant: &TenantId,
        carrier: &layerx_agent_api::budget::BudgetAuthorization,
        mutation_key: [u8; 32],
        expected: impl Fn(&crate::budget::BudgetMutation) -> bool,
    ) -> Result<crate::budget::ConfirmedBudgetMutation, HumanOperationError> {
        let peer = context.peer();
        let prepared_key = (
            peer.tenant.clone(),
            peer.principal.clone(),
            carrier.preparation_ref.as_str().to_owned(),
        );
        let cached = self
            .prepared
            .get(&prepared_key)
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        let budget_context = prepared_budget_context(&cached.prepared).ok_or(
            HumanOperationError::Typed(crate::human::HumanRefusal::BudgetContextMismatch),
        )?;
        let (signature, signer) = owner_signature(&cached.prepared, carrier)?;
        let signed = attach_external_signature(&cached.prepared, signature)
            .map_err(|_| HumanOperationError::Refused)?;
        let submission = verify_before_submit(&signed, &cached.prepared, &signer, &cached.registry)
            .map_err(|_| HumanOperationError::Refused)?;
        let mutation = crate::budget::budget_mutation_identity(
            submission.exact_bytes(),
            &cached.registry,
            &budget_context,
        )
        .map_err(budget_refusal)?;
        if !expected(&mutation) {
            return Err(HumanOperationError::Refused);
        }
        let (preparation_id, head) = self.admit_budget_submit(
            context,
            control,
            tenant,
            &cached,
            carrier.preparation_ref.as_str(),
            &submission,
        )?;
        let (verifier, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &cached.registry,
            signer,
            correlation: boundary_correlation(
                peer,
                cached.prepared.envelope.actor_did().as_bytes(),
                b"budget-mutation",
            ),
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        let outcome = context
            .commit(control, |_| {
                Ok(crate::budget::confirm_budget_mutation(
                    &mut pipeline,
                    &verifier,
                    &submission,
                    &cached.registry,
                    &budget_context,
                    mutation_key,
                ))
            })
            .map_err(rpc_commit_error)?;
        let (release, sequence) = match &outcome {
            Ok(confirmed) => (
                crate::budget::ReleaseKind::Executed,
                confirmed.observed_head_sequence(),
            ),
            Err(BudgetCreationError::CoreRejected) => (crate::budget::ReleaseKind::Failed, head),
            Err(_) => (crate::budget::ReleaseKind::Unknown, head),
        };
        control
            .settle_write(tenant, preparation_id, release, sequence)
            .map_err(rpc_commit_error)?;
        let confirmed = outcome.map_err(budget_refusal)?;
        self.prepared.remove(&prepared_key);
        Ok(confirmed)
    }

    fn admitted_client_write(
        &self,
        tenant: &TenantId,
        operator_id: &str,
        request_id: [u8; 32],
        command: OperatorCommand,
    ) -> Result<(Surface, ActionPlan), HumanOperationError> {
        let root = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?
            .root()
            .to_path_buf();
        let context =
            OperatorContext::new(operator_id, request_id).map_err(HumanOperationError::from)?;
        let mut surface = Surface::open(&root, tenant).map_err(HumanOperationError::from)?;
        let plan = surface
            .dispatch(&context, command)
            .map_err(HumanOperationError::from)?;
        if plan != ActionPlan::OrdinaryClientWrite(ORDINARY_CLIENT_WRITE) {
            return Err(HumanOperationError::Refused);
        }
        Ok((surface, plan))
    }

    fn verified_budget_preparation(
        &self,
        prepared_key: &(String, String, String),
        signature: [u8; 64],
        signer_public_key: &[u8; 32],
        agent: [u8; 32],
    ) -> Result<(CachedPreparation, VerifiedSubmission), HumanOperationError> {
        let cached = self
            .prepared
            .get(prepared_key)
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        let signed = attach_external_signature(&cached.prepared, signature)
            .map_err(|_| HumanOperationError::Refused)?;
        let verified_submission = verify_before_submit(
            &signed,
            &cached.prepared,
            signer_public_key,
            &cached.registry,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let identity = budget_create_identity(verified_submission.exact_bytes(), &cached.registry)
            .map_err(|_| HumanOperationError::Refused)?;
        if identity.budget_id != agent {
            return Err(HumanOperationError::Refused);
        }
        Ok((cached, verified_submission))
    }

    fn dispatch_queued(
        &mut self,
        peer: &HumanPeer,
        submission_id: [u8; 32],
        registry: &ModuleRegistry,
        signer: [u8; 32],
        correlation: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.require_write_admission(&peer.tenant)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let bytes = subject::begin_transmission(
            self.outboxes.entry(peer.tenant.clone()).or_default(),
            &mut store,
            submission_id,
        )?;
        let (state, reason) =
            match self
                .node
                .submit_signed(registry, signer, correlation, 0, &bytes)
            {
                Ok(Submission::Acknowledged(_)) => {
                    (SubmissionState::Acknowledged, "core admission acknowledged")
                }
                Ok(Submission::Unknown(_)) => {
                    (SubmissionState::Unknown, "submission outcome indeterminate")
                }
                Err(_) => (
                    SubmissionState::Unknown,
                    "node submission boundary unavailable after durable dispatch",
                ),
            };
        self.outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .transition(&mut store, submission_id, state, reason, None)
            .map_err(|_| HumanOperationError::Unavailable)?;
        Self::observation(
            self.outboxes
                .entry(peer.tenant.clone())
                .or_default()
                .status(submission_id)
                .ok_or(HumanOperationError::Unavailable)?,
        )
    }

    /// The tenant's subscription store, opened from the shared durable store on first use.
    ///
    /// Callers invoke this only inside the one authorized mutation scope (subscription
    /// create/pause/resume/delete/acknowledge after the session permit has been checked).
    /// Peer reads must use `self.subscriptions.get(&tenant)` directly: an absent tenant is
    /// genuinely absent state and is never lazily opened on a read.
    ///
    /// # Errors
    /// Returns `HumanOperationError::Unavailable` when the store lock is poisoned or the
    /// tenant's persisted subscriptions cannot be restored.
    fn subscription_store_for(
        &mut self,
        tenant: TenantId,
    ) -> Result<&mut crate::events::subscription::Store, HumanOperationError> {
        match self.subscriptions.entry(tenant) {
            std::collections::btree_map::Entry::Occupied(entry) => Ok(entry.into_mut()),
            std::collections::btree_map::Entry::Vacant(entry) => {
                let opened = crate::events::subscription::Store::open_shared(
                    Arc::clone(&self.store),
                    entry.key().clone(),
                )
                .map_err(|_| HumanOperationError::Unavailable)?;
                Ok(entry.insert(opened))
            }
        }
    }

    fn resume_queued(
        &mut self,
        peer: &HumanPeer,
        identifier: [u8; 32],
        expected_activity: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let bytes = self
            .outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .bytes_for_transmission(identifier)
            .map_err(|_| HumanOperationError::Refused)?
            .to_vec();
        let activity = layerx_wire::activity::decode_signed(&bytes, &registry)
            .map_err(|_| HumanOperationError::Refused)?;
        if activity.idempotency_key() != identifier
            || layerx_wire::hash::activity_id(&activity)
                .map_err(|_| HumanOperationError::Refused)?
                != expected_activity
            || activity.network_id() != self.node.handshake().node().network_id
            || activity.protocol_version() != self.node.handshake().node().protocol_version
        {
            return Err(HumanOperationError::Refused);
        }
        let signer = activity
            .authority()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let actor = Did::new(activity.actor_did()).map_err(|_| HumanOperationError::Refused)?;
        let correlation = u64::from_be_bytes(
            identifier[..8]
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        ) | 1;
        let origin = self
            .outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .origin(identifier)
            .map_err(|_| HumanOperationError::Unavailable)?
            .ok_or(HumanOperationError::Refused)?;
        let sessions = self
            .session_control
            .as_ref()
            .ok_or(HumanOperationError::Unavailable)?
            .registry();
        let held = sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let core_sequence = self.node.head().chain_sequence;
        let record = held
            .get(&origin.session.tenant, origin.session.session_id)
            .ok_or(HumanOperationError::Refused)?;
        let submit_scopes =
            crate::tenant::OperationClass::authorized_scopes(crate::tenant::Operation::Submit);
        if origin.session.tenant.as_str() != peer.tenant
            || record.request.tenant != origin.session.tenant
            || !record.open
            || held
                .generation(&origin.session.tenant, origin.session.session_id)
                != Some(origin.generation)
            || core_sequence >= record.request.expiry_sequence
            || record.request.agent != actor
            || !submit_scopes
                .iter()
                .any(|scope| record.request.scopes.contains(*scope))
        {
            return Err(HumanOperationError::Refused);
        }
        let dispatched = self.dispatch_queued(peer, identifier, &registry, signer, correlation);
        drop(held);
        dispatched
    }

    pub(crate) fn submit_external_with_origin(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanSubmit>,
        origin: Option<crate::prepare::PreparationAuthorization>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if !self.unified_owner_active {
            return Err(HumanOperationError::Unavailable);
        }
        if submit_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let prepared_key = (
            peer.tenant.clone(),
            peer.principal.clone(),
            request.operation.preparation_ref.clone(),
        );
        let cached = self
            .prepared
            .get(&prepared_key)
            .cloned()
            .ok_or(HumanOperationError::Refused)?;
        let prepared = cached.prepared;
        let signature: [u8; 64] = request
            .operation
            .signature
            .as_slice()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let signed = attach_external_signature(&prepared, signature)
            .map_err(|_| HumanOperationError::Refused)?;
        let verified = verify_before_submit(
            &signed,
            &prepared,
            &request.operation.signer_public_key,
            &cached.registry,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let submission_id = prepared.envelope.idempotency_key().bytes();
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        self.outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .enqueue_with_origin(&mut store, tenant, submission_id, verified, origin)
            .map_err(|_| HumanOperationError::Unavailable)?;
        self.prepared.remove(&prepared_key);
        self.submissions.insert(
            (
                peer.tenant.clone(),
                peer.principal.clone(),
                hex(&submission_id),
            ),
            submission_id,
        );
        drop(store);
        self.dispatch_queued(
            peer,
            submission_id,
            &cached.registry,
            request.operation.signer_public_key,
            request.request_id,
        )
    }

    fn subject_owner(
        &mut self,
        peer: &HumanPeer,
        did: &Did,
        authority: &Authority,
    ) -> Result<(), HumanOperationError> {
        if peer.subject.is_none() {
            return Ok(());
        }
        let Authority::Owner(bytes) = authority else {
            return Err(HumanOperationError::Refused);
        };
        let key = bytes
            .as_ref()
            .try_into()
            .map_err(|_| HumanOperationError::Refused)?;
        let identity = self
            .subject_identity(peer, did)
            .map_err(|_| HumanOperationError::Refused)?;
        if identity.frozen
            || identity.verification_level < VerificationLevel::CHECKPOINT_FINALISED
            || !identity
                .authorities
                .contains(&ProtocolAuthority::PrimaryKey(key))
        {
            return Err(HumanOperationError::Refused);
        }
        Ok(())
    }
    fn subject_identity(
        &mut self,
        peer: &HumanPeer,
        did: &Did,
    ) -> Result<CoreIdentity, IdentityError> {
        let registry = self
            .authority
            .registry(peer)
            .map_err(|_| IdentityError::Unverified)?;
        let bound = subject::for_did(&self.store, peer, did, &registry)
            .map_err(|_| IdentityError::Unverified)?;
        self.authority.core_identity(&bound, did)
    }

    fn terminal_receipt_evidence(
        &mut self,
        receipt_evidence: &layerx_client::evidence::VerifiedProofBundle,
        authority: &AuthorizedBatch,
        native: Option<&layerx_proof::receipt::NativeOwnerOutcomeContext<'_>>,
    ) -> Result<crate::protocol_evidence::VerifiedReceiptEvidence, HumanOperationError> {
        let layerx_client::evidence::VerifiedProofBundle::Receipt {
            canonical_bytes,
            proof,
            signed_header,
            ..
        } = receipt_evidence
        else {
            return Err(HumanOperationError::Refused);
        };
        let raw = crate::protocol_evidence::RawReceiptEvidence::new(
            canonical_bytes.clone(),
            proof.clone(),
            signed_header.canonical_bytes.clone(),
            signed_header.signature,
        );
        let header = layerx_wire::receipt::decode_batch_header(&signed_header.canonical_bytes)
            .map_err(|_| HumanOperationError::Refused)?;
        if header.protocol_version() == 3 && header.first_sequence() < header.last_sequence() {
            return self.maintained_terminal_receipt(&raw, authority, &header, native);
        }
        if let Some(expected) = native {
            return crate::protocol_evidence::VerifiedReceiptEvidence::verify_authorized_native_owner(
                &raw, authority, expected, None,
            ).map_err(|_| HumanOperationError::Refused);
        }
        let node = self.node.handshake().node();
        let terminal = crate::protocol_evidence::VerifiedReceiptEvidence::verify_authorized(
            &raw,
            authority,
            node.protocol_version,
            node.network_id,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        Ok(terminal)
    }

    fn maintained_terminal_receipt(
        &mut self,
        raw: &crate::protocol_evidence::RawReceiptEvidence,
        authority: &AuthorizedBatch,
        header: &layerx_wire::receipt::BatchHeader,
        native: Option<&layerx_proof::receipt::NativeOwnerOutcomeContext<'_>>,
    ) -> Result<crate::protocol_evidence::VerifiedReceiptEvidence, HumanOperationError> {
        let node = self.node.handshake().node().clone();
        let authorization = layerx_proof::inclusion::SequencerAuthorization::new(
            header.sequencer_id(),
            authority.sequencer_public_key(),
            header.batch_number(),
            header.batch_number(),
        );
        let items = self.maintained_receipt_history(raw, header, &authorization)?;
        let item = items.last().ok_or(HumanOperationError::Refused)?;
        layerx_wire::batch_maintenance::decode_maintenance(item.canonical_bytes())
            .map_err(|_| HumanOperationError::Refused)?;
        let receipts = items[..items.len() - 1]
            .iter()
            .map(|item| item.canonical_bytes().to_vec())
            .collect::<Vec<_>>();
        let proof = layerx_client::read::HistoryProof::decode(item.proof_material())
            .map_err(|_| HumanOperationError::Refused)?;
        let header_signature = raw.header_signature();
        if proof.header != raw.canonical_header() || proof.header_signature != header_signature {
            return Err(HumanOperationError::Refused);
        }
        let evidence = layerx_proof::receipt::MaintainedOutcomeEvidence {
            header: raw.canonical_header(),
            header_signature: &header_signature,
            activity_proof: raw.proof(),
            maintenance: item.canonical_bytes(),
            maintenance_proof: &proof.proof,
            authorization: &authorization,
        };
        if let Some(expected) = native {
            return crate::protocol_evidence::VerifiedReceiptEvidence::verify_authorized_native_owner(
                raw, authority, expected, Some((&evidence, &receipts)),
            ).map_err(|_| HumanOperationError::Refused);
        }
        crate::protocol_evidence::VerifiedReceiptEvidence::verify_authorized_maintained(
            raw,
            authority,
            &evidence,
            &receipts,
            node.protocol_version,
            node.network_id,
        )
        .map_err(|_| HumanOperationError::Refused)
    }

    fn maintained_receipt_history(
        &mut self,
        raw: &crate::protocol_evidence::RawReceiptEvidence,
        header: &layerx_wire::receipt::BatchHeader,
        authorization: &layerx_proof::inclusion::SequencerAuthorization,
    ) -> Result<Vec<layerx_client::read::HistoryItem>, HumanOperationError> {
        let count = header
            .last_sequence()
            .checked_sub(header.first_sequence())
            .filter(|count| *count > 0 && *count <= 64)
            .ok_or(HumanOperationError::Refused)?;
        let mut items = Vec::new();
        let mut next = header.first_sequence();
        let mut total = 0_usize;
        while next <= header.last_sequence() {
            let expected = (header.last_sequence() - next + 1).min(256);
            let page = self
                .node
                .history(
                    next,
                    header.last_sequence(),
                    256,
                    None,
                    layerx_types::verify::VerificationLevel::BATCH_INCLUDED,
                    next.checked_add(10_000)
                        .ok_or(HumanOperationError::Refused)?,
                    *authorization,
                )
                .map_err(|_| HumanOperationError::Unavailable)?;
            if u64::try_from(page.items.len()).ok() != Some(expected)
                || page.cursor.is_some() != (next + expected <= header.last_sequence())
            {
                return Err(HumanOperationError::Refused);
            }
            for item in page.items {
                if item.kind != layerx_client::read::HistoryKind::Receipt
                    || item.global_sequence != next
                {
                    return Err(HumanOperationError::Refused);
                }
                let proof = layerx_client::read::HistoryProof::decode(item.proof_material())
                    .map_err(|_| HumanOperationError::Refused)?;
                if proof.header != raw.canonical_header()
                    || proof.header_signature != raw.header_signature()
                {
                    return Err(HumanOperationError::Refused);
                }
                total = total
                    .checked_add(item.canonical_bytes().len())
                    .filter(|total| *total <= 16_777_216)
                    .ok_or(HumanOperationError::Refused)?;
                items.push(item);
                next = next.checked_add(1).ok_or(HumanOperationError::Refused)?;
            }
        }
        if u64::try_from(items.len()).ok() != count.checked_add(1) {
            return Err(HumanOperationError::Refused);
        }
        Ok(items)
    }

    fn retained_activity(
        &self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
    ) -> Result<Vec<u8>, HumanOperationError> {
        self.outboxes
            .get(&peer.tenant)
            .ok_or(HumanOperationError::Refused)?
            .exact_signed_bytes(idempotency_key)
            .map(<[u8]>::to_vec)
            .map_err(|_| HumanOperationError::Refused)
    }

    fn augment_receipt_evidence(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        tenant: TenantId,
        mut served: crate::receipt::ServedReceipt,
        authority: &AuthorizedBatch,
        native: Option<&layerx_proof::receipt::NativeOwnerOutcomeContext<'_>>,
    ) -> Result<crate::receipt::ServedReceipt, HumanOperationError> {
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let correlation = u64::from_be_bytes(
            idempotency_key[..8]
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        ) | 1;
        let activity_evidence = self.node.proof_bundle(
            ProofBundleSelector::Activity(served.metadata.activity_id),
            correlation,
            &registry,
        );
        let receipt_evidence = self.node.proof_bundle(
            ProofBundleSelector::Receipt(served.metadata.activity_id),
            correlation
                .checked_add(1)
                .ok_or(HumanOperationError::Refused)?,
            &registry,
        );
        match (activity_evidence, receipt_evidence) {
            (Ok(activity_evidence), Ok(receipt_evidence)) => {
                if activity_evidence.canonical_bytes()
                    != self.retained_activity(peer, idempotency_key)?
                    || receipt_evidence.canonical_bytes() != served.canonical_bytes
                {
                    return Err(HumanOperationError::Refused);
                }
                let terminal =
                    self.terminal_receipt_evidence(&receipt_evidence, authority, native)?;
                let terminal_state = if terminal.result_code() == 0 {
                    SubmissionState::Executed
                } else {
                    SubmissionState::Failed
                };
                let evidence_batch = activity_evidence
                    .signed_header()
                    .batch_number()
                    .map_err(|_| HumanOperationError::Refused)?;
                let capability_preparation = self
                    .session_control
                    .as_ref()
                    .map(|control| {
                        control.preparation_for_idempotency_key(&tenant, idempotency_key)
                    })
                    .transpose()
                    .map_err(rpc_commit_error)?
                    .flatten();

                let checkpoint = match self.node.checkpoint_evidence(
                    CheckpointSelector::Batch(evidence_batch),
                    correlation
                        .checked_add(2)
                        .ok_or(HumanOperationError::Refused)?,
                ) {
                    Ok(checkpoint) => Some(checkpoint),
                    Err(error) if evidence_unavailable(&error) => None,
                    Err(_) => return Err(HumanOperationError::Refused),
                };
                {
                    let mut store = self
                        .store
                        .lock()
                        .map_err(|_| HumanOperationError::Unavailable)?;
                    crate::finality::augment_verified(
                        &mut store,
                        tenant.clone(),
                        idempotency_key,
                        &activity_evidence,
                        &receipt_evidence,
                        checkpoint.as_ref(),
                    )
                    .map_err(|_| HumanOperationError::Refused)?;
                    let raw = raw_receipt_evidence(&receipt_evidence)?;
                    crate::receipt::persist_evidence(
                        &mut store,
                        tenant.clone(),
                        idempotency_key,
                        &raw,
                    )
                    .map_err(|error| match error {
                        crate::receipt::ReceiptStoreError::Store(_) => {
                            HumanOperationError::Unavailable
                        }
                        _ => HumanOperationError::Refused,
                    })?;
                    if let Some(preparation_id) = capability_preparation {
                        settle_capability_chain(&mut store, &tenant, preparation_id, &terminal)?;
                    }

                    served = crate::receipt::serve(
                        &store,
                        tenant,
                        crate::receipt::ReceiptLookupKey::Idempotency(idempotency_key),
                    )
                    .map_err(|_| HumanOperationError::Unavailable)?;
                    settle_terminal_submission(
                        self.outboxes.entry(peer.tenant.clone()).or_default(),
                        &mut store,
                        idempotency_key,
                        terminal_state,
                        terminal,
                    )?;
                }
            }
            (Err(error), _) | (_, Err(error)) if evidence_unavailable(&error) => {}
            (Err(_), _) | (_, Err(_)) => return Err(HumanOperationError::Refused),
        }
        Ok(served)
    }

    /// # Errors
    /// Returns an error when the request is invalid, authority is refused, or required state is unavailable.
    pub fn new(
        authority: A,
        node: Client,
        store: Arc<Mutex<Store>>,
        peers: &BTreeMap<u32, (String, String)>,
        maximum_payload_bytes: usize,
        timestamp_span: u64,

        clock: Arc<dyn layerx_types::clock::Clock>,
    ) -> Result<Self, HumanOperationError> {
        if maximum_payload_bytes == 0 || timestamp_span == 0 {
            return Err(HumanOperationError::Refused);
        }
        let mut outboxes = BTreeMap::<String, Outbox>::new();
        let mut submissions = BTreeMap::new();
        let mut tenant_principals = BTreeMap::<String, String>::new();
        let restore_peers = {
            let durable = store.lock().map_err(|_| HumanOperationError::Unavailable)?;
            subject::restore_peers(&durable, peers)?
        };
        for peer in restore_peers {
            let (principal, tenant) = (&peer.principal, &peer.tenant);
            if tenant_principals
                .insert(tenant.clone(), principal.clone())
                .is_some_and(|previous| previous != *principal)
            {
                return Err(HumanOperationError::Refused);
            }
        }
        let durable = store.lock().map_err(|_| HumanOperationError::Unavailable)?;
        for (tenant, principal) in tenant_principals {
            let tenant_id =
                TenantId::new(tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
            let outbox = outboxes.entry(tenant.clone()).or_default();
            for object in durable.list_object_ids(&tenant_id, ObjectKind::Outbox) {
                if object.starts_with(b"approval-released-v1:") {
                    continue;
                }
                let id: [u8; 32] = object
                    .try_into()
                    .map_err(|_| HumanOperationError::Refused)?;
                outbox
                    .restore(&durable, tenant_id.clone(), id)
                    .map_err(|_| HumanOperationError::Refused)?;
                submissions.insert((tenant.clone(), principal.clone(), hex(&id)), id);
            }
        }
        drop(durable);

        let subscriptions = restored_subscriptions(&store)?;
        Ok(Self {
            authority,
            node,
            store,
            outboxes,
            prepared: BTreeMap::new(),
            submissions,
            maximum_payload_bytes,
            timestamp_span,
            last_verified_receipt: None,
            unified_owner_active: false,
            write_admission: BTreeMap::new(),

            clock,
            subscriptions,
            session_control: None,

            budget_limiter: None,

            export_trust: None,

            policies: None,
        })
    }

    /// Loads every configured tenant policy source into the registries `policy_dry_run`
    /// evaluates.
    ///
    /// # Errors
    /// Returns the first tenant whose source cannot be loaded; nothing is attached then.
    pub fn attach_policies(
        &mut self,
        sources: &BTreeMap<TenantId, std::path::PathBuf>,
    ) -> Result<(), crate::policy::PolicyLoadError> {
        self.policies = Some(crate::policy::load_tenant_registries(sources)?);
        Ok(())
    }

    /// The subject agent's protocol budget reconciliation, rebuilt exactly as startup
    /// recovery rebuilds it: STATE_PROVEN budget state, the persisted receipt evidence in
    /// the budget window, restart accounting, then `budget::reconcile`. `None` only when
    /// the agent holds no protocol budget, a window receipt has no persisted evidence, or
    /// restart accounting does not reconcile.
    fn verified_policy_budget(
        &mut self,
        peer: &HumanPeer,
        tenant: &TenantId,
        actor: &Did,
    ) -> Result<Option<crate::budget::ReconciliationState>, HumanOperationError> {
        let (owner, inventory) = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            let Some(owner) = managed_agent::budget_owners(&store, tenant)?
                .into_iter()
                .find(|owner| owner.agent_did.as_bytes() == actor.as_bytes())
            else {
                return Ok(None);
            };
            let inventory =
                crate::receipt::evidence_inventory(&store, tenant).map_err(|error| match error {
                    crate::receipt::ReceiptStoreError::Store(_) => HumanOperationError::Unavailable,
                    _ => HumanOperationError::Refused,
                })?;
            (owner, inventory)
        };
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let (_, _, _, _, _, _, authorization) = self.authority.balance_context(peer)?;
        let node = self.node.handshake().node().clone();
        let verifier = EvidenceAuthority::pinned_to_handshake(
            node.protocol_version,
            node.network_id,
            node.authorised_sequencer_key,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let budget_id = owner.active_budget_id;
        let (with_evidence, without_evidence) = self.attribute_receipts(
            &peer.tenant,
            &registry,
            owner.agent_did.as_bytes(),
            &inventory,
        );
        let key = budget_state_key(budget_id);
        let value = self
            .node
            .module_state(
                BUDGET_MODULE_ID,
                &key,
                VerificationLevel::STATE_PROVEN,
                boundary_correlation(peer, &budget_id, b"policy-budget"),
                authorization,
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
        let protocol = ProtocolBudgetState {
            evidence: RawStateEvidence::module_witness(
                value.canonical_bytes().to_vec(),
                BUDGET_MODULE_ID,
                key,
                value.proof_material().to_vec(),
                RootSelector::Latest,
                node.authorised_sequencer_key,
            ),
        };
        let verified = verifier
            .verify_state(&protocol.evidence)
            .map_err(|_| HumanOperationError::Refused)?;
        let record = ProtocolBudgetRecord::decode(verified.canonical_state())
            .map_err(|_| HumanOperationError::Refused)?;
        if record.budget_id != budget_id {
            return Err(HumanOperationError::Refused);
        }
        let window = record.period_start..record.window_end_sequence();
        if without_evidence
            .iter()
            .any(|receipt| window.contains(&receipt.global_sequence))
        {
            return Ok(None);
        }
        let receipts: Vec<PersistedReceipt> = with_evidence
            .iter()
            .filter(|receipt| window.contains(&receipt.global_sequence))
            .map(|receipt| PersistedReceipt {
                expected_activity_id: receipt.activity_id,
                evidence: receipt.evidence.clone(),
            })
            .collect();
        let accounting = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            let mut unknown_budget_ids = Vec::new();
            for object_id in store.list_object_ids(tenant, ObjectKind::Budget) {
                let Some(id) = object_id.strip_prefix(b"unknown-budget:".as_slice()) else {
                    continue;
                };
                let id: [u8; 32] = id.try_into().map_err(|_| HumanOperationError::Refused)?;
                unknown_budget_ids.push(id);
            }
            crate::budget::rebuild(
                &store,
                tenant,
                &unknown_budget_ids,
                &receipts,
                &protocol,
                &verifier,
            )
            .map_err(|_| HumanOperationError::Refused)?
        };
        if !accounting.reconciled {
            return Ok(None);
        }
        let spend_receipts: Vec<crate::budget::SpendReceiptEvidence> = receipts
            .iter()
            .map(|receipt| crate::budget::SpendReceiptEvidence {
                expected_activity_id: receipt.expected_activity_id,
                evidence: receipt.evidence.clone(),
            })
            .collect();
        let mut local = crate::budget::LocalAccounting {
            consumed: accounting.receipt_consumed,
            window_start_sequence: 0,
            last_receipt: None,
        };
        crate::budget::reconcile(&mut local, &protocol, &spend_receipts, &verifier)
            .map(Some)
            .map_err(|_| HumanOperationError::Refused)
    }

    /// Installs the independently configured deployment export trust and the
    /// operator deadline bounding availability retrieval; no request field
    /// selects either.
    pub fn install_export_trust(
        &mut self,
        source: crate::export::ExportTrustSource,
        availability_deadline: Duration,
    ) {
        self.export_trust = Some((source, availability_deadline));
    }

    /// The account bound to the authenticated caller: the authority's fresh
    /// balance context account, which an authenticated subject must derive to.
    fn export_bound_account(&mut self, peer: &HumanPeer) -> Result<[u8; 32], HumanOperationError> {
        let (account, asset, _, _, age, maximum_age, _) = self.authority.balance_context(peer)?;
        if maximum_age == 0 || age > maximum_age {
            return Err(HumanOperationError::Unavailable);
        }
        if account == [0; 32] {
            return Err(HumanOperationError::Refused);
        }
        if let Some(scope) = &peer.subject {
            let canonical = layerx_types::account::AccountId::parse(&scope.account)
                .map_err(|_| HumanOperationError::Refused)?;
            if layerx_wire::hash::account_id_for_protocol(&canonical, 3)
                .map_err(|_| HumanOperationError::Refused)?
                != account
                || scope.asset != asset
            {
                return Err(HumanOperationError::Refused);
            }
        }
        Ok(account)
    }

    pub(crate) fn attach_budget_limiter(&mut self, limiter: Arc<BudgetLimiter>) {
        self.budget_limiter = Some(limiter);
    }

    pub(crate) fn attach_session_control(&mut self, control: SessionControl) {
        self.session_control = Some(control);
    }

    /// Returns the startup write-admission decision for `tenant`.
    #[must_use]
    pub fn write_admission(&self, tenant: &str) -> Option<&Result<(), RecoveryRefusal>> {
        self.write_admission.get(tenant)
    }

    fn require_write_admission(&self, tenant: &str) -> Result<(), HumanOperationError> {
        match self.write_admission.get(tenant) {
            Some(Ok(())) => Ok(()),
            Some(Err(_)) | None => Err(HumanOperationError::Refused),
        }
    }

    /// Runs startup recovery once per tenant budget and records write admission
    /// per tenant. A tenant whose recovery fails stays read-only with the reason
    /// logged; other tenants proceed independently.
    pub fn recover_tenants(&mut self, peers: &[HumanPeer], ceiling_maximum: u128) {
        let mut first_peer_by_tenant = BTreeMap::<String, HumanPeer>::new();
        for peer in peers {
            first_peer_by_tenant
                .entry(peer.tenant.clone())
                .or_insert_with(|| peer.clone());
        }
        for (tenant, peer) in first_peer_by_tenant {
            let admission = self.recover_tenant(&peer, ceiling_maximum);
            match &admission {
                Ok(()) => eprintln!("layerx-agentd: recovery tenant={tenant} writes admitted"),
                Err(refusal) => {
                    eprintln!("layerx-agentd: recovery tenant={tenant} read-only: {refusal:?}");
                }
            }
            self.write_admission.insert(tenant, admission);
        }
    }

    fn recover_tenant(
        &mut self,
        peer: &HumanPeer,
        ceiling_maximum: u128,
    ) -> Result<(), RecoveryRefusal> {
        let tenant_id = TenantId::new(peer.tenant.clone())
            .map_err(|_| RecoveryRefusal::Store(HumanOperationError::Refused))?;
        let owners = {
            let store = self
                .store
                .lock()
                .map_err(|_| RecoveryRefusal::Store(HumanOperationError::Unavailable))?;
            managed_agent::budget_owners(&store, &tenant_id).map_err(RecoveryRefusal::Store)?
        };
        if owners.is_empty() {
            eprintln!(
                "layerx-agentd: recovery tenant={} no protocol budget to recover",
                peer.tenant
            );
            return Ok(());
        }
        let inventory = {
            let store = self
                .store
                .lock()
                .map_err(|_| RecoveryRefusal::Store(HumanOperationError::Unavailable))?;
            crate::receipt::evidence_inventory(&store, &tenant_id).map_err(|error| {
                RecoveryRefusal::Store(match error {
                    crate::receipt::ReceiptStoreError::Store(_) => HumanOperationError::Unavailable,
                    _ => HumanOperationError::Refused,
                })
            })?
        };
        let registry = self
            .authority
            .registry(peer)
            .map_err(|error| RecoveryRefusal::Store(map_core(error)))?;
        let (_, _, _, _, _, _, authorization) = self
            .authority
            .balance_context(peer)
            .map_err(RecoveryRefusal::Store)?;
        let node = self.node.handshake().node().clone();
        let verifier = EvidenceAuthority::pinned_to_handshake(
            node.protocol_version,
            node.network_id,
            node.authorised_sequencer_key,
        )
        .map_err(|error| {
            eprintln!("layerx-agentd: recovery handshake pin refused: {error:?}");
            RecoveryRefusal::Store(HumanOperationError::Refused)
        })?;
        let context = TenantRecoveryContext {
            peer,
            tenant_id: &tenant_id,
            registry: &registry,
            inventory: &inventory,
            verifier: &verifier,
            authorization,
            sequencer_key: node.authorised_sequencer_key,
            ceiling_maximum,
            current_sequence: self.node.head().chain_sequence,
        };
        let mut admission = Ok(());
        for owner in owners {
            let outcome = self.recover_owner_budget(&context, &owner);
            if admission.is_ok() {
                admission = outcome;
            }
        }
        admission
    }

    fn recover_owner_budget(
        &mut self,
        context: &TenantRecoveryContext<'_>,
        owner: &managed_agent::BudgetOwner,
    ) -> Result<(), RecoveryRefusal> {
        let peer = context.peer;
        let budget_id = owner.active_budget_id;
        let (with_evidence, without_evidence) = self.attribute_receipts(
            &peer.tenant,
            context.registry,
            owner.agent_did.as_bytes(),
            context.inventory,
        );
        let correlation = boundary_correlation(peer, &budget_id, b"budget-recovery");
        let key = budget_state_key(budget_id);
        let outcome = match self.node.module_state(
            BUDGET_MODULE_ID,
            &key,
            VerificationLevel::STATE_PROVEN,
            correlation,
            context.authorization,
        ) {
            Ok(value) => {
                let evidence = RawStateEvidence::module_witness(
                    value.canonical_bytes().to_vec(),
                    BUDGET_MODULE_ID,
                    key,
                    value.proof_material().to_vec(),
                    RootSelector::Latest,
                    context.sequencer_key,
                );
                let request = BudgetRecoveryRequest {
                    budget_id,
                    protocol_budget: ProtocolBudgetState { evidence },
                    verifier: context.verifier.clone(),
                    receipts_with_evidence: &with_evidence,
                    receipts_without_evidence: &without_evidence,
                    ceiling_maximum: context.ceiling_maximum,
                    current_sequence: context.current_sequence,
                };
                let mut store = self
                    .store
                    .lock()
                    .map_err(|_| RecoveryRefusal::Store(HumanOperationError::Unavailable))?;
                recover_tenant_budget(&mut store, context.tenant_id, &request)
            }
            Err(error) => Err(RecoveryRefusal::BudgetState {
                budget_id,
                reason: format!("{error:?}"),
            }),
        };
        match outcome {
            Ok(recovery) => {
                let accounting = recovery.recovered.budget_accounting;
                let ceiling_reconciled = recovery
                    .recovered
                    .ceiling
                    .snapshot()
                    .is_ok_and(|snapshot| snapshot.reconciled);
                eprintln!(
                    "layerx-agentd: recovery tenant={} agent={} budget={} queued={} awaiting={} receipts_with_evidence={} receipts_without_evidence={} protocol_consumed={:?} receipt_consumed={} held_unresolved={} unresolved_count={} reconciled={} ceiling_reconciled={ceiling_reconciled} admitted={}",
                    peer.tenant,
                    owner.agent_id,
                    hex(&budget_id),
                    recovery.recovered.queued_for_transmission.len(),
                    recovery.recovered.awaiting_receipt_resolution.len(),
                    with_evidence.len(),
                    without_evidence.len(),
                    accounting.protocol_consumed,
                    accounting.receipt_consumed,
                    accounting.held_unresolved,
                    accounting.unresolved_count,
                    accounting.reconciled,
                    recovery.admission.is_ok(),
                );
                self.outboxes
                    .insert(peer.tenant.clone(), recovery.recovered.outbox);
                recovery.admission
            }
            Err(refusal) => {
                eprintln!(
                    "layerx-agentd: recovery tenant={} agent={} budget={} failed: {refusal:?}",
                    peer.tenant,
                    owner.agent_id,
                    hex(&budget_id),
                );
                Err(refusal)
            }
        }
    }

    /// Splits a tenant's receipt inventory into the receipts whose signed
    /// activity names `agent_did` as actor. Receipts that cannot be attributed
    /// to the agent are not that agent's budget spend.
    fn attribute_receipts(
        &self,
        tenant: &str,
        registry: &ModuleRegistry,
        agent_did: &[u8],
        inventory: &crate::receipt::ReceiptEvidenceInventory,
    ) -> (Vec<ReceiptEvidenceRecord>, Vec<ReceiptMetadata>) {
        let Some(outbox) = self.outboxes.get(tenant) else {
            return (Vec::new(), Vec::new());
        };
        let attributed = |idempotency_key: [u8; 32]| {
            outbox
                .exact_signed_bytes(idempotency_key)
                .ok()
                .and_then(|bytes| layerx_wire::activity::decode_signed(bytes, registry).ok())
                .is_some_and(|activity| activity.actor_did() == agent_did)
        };
        let with_evidence = inventory
            .with_evidence
            .iter()
            .filter(|record| attributed(record.idempotency_key))
            .cloned()
            .collect();
        let without_evidence = inventory
            .without_evidence
            .iter()
            .filter(|metadata| attributed(metadata.idempotency_key))
            .copied()
            .collect();
        (with_evidence, without_evidence)
    }

    fn registry_response(registry: &ModuleRegistry) -> Result<HumanResponse, HumanOperationError> {
        let mut out = Encoder::new();
        out.u16(registry.registrations().len())?;
        for registration in registry.registrations() {
            out.u16(usize::from(registration.module() as u16))?;
            out.u16(registration.activity_types().len())?;
            for activity in registration.activity_types() {
                out.u32(activity.value());
            }
        }
        out.finish()
    }

    fn observation(status: &SubmissionStatus) -> Result<HumanResponse, HumanOperationError> {
        let mut out = Encoder::new();
        out.fixed(&status.activity_id);
        out.text(&hex(&status.submission_id))?;
        out.u8(state_code(status.state));
        if status.state == SubmissionState::Executed {
            out.text(
                &status
                    .evidence
                    .map(|value| hex(&value.receipt_ref()))
                    .ok_or(HumanOperationError::Refused)?,
            )?;
        }
        out.u8(0); // verification remains unverified until a receipt is returned
        out.u8(0); // evidence
                   // The current durable outbox schema has no transition timestamp. Do
                   // not fabricate one for the Human API; the state itself is durable.
        out.u8(0);
        out.u8(0); // no receipt
        out.finish()
    }

    fn session_fee_state(
        &mut self,
        grant_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let snapshot = self
            .node
            .session_fee_state(40, grant_id)
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut out = Encoder::new();
        out.bytes(&snapshot.value)?;
        out.u64(snapshot.observed_sequence);
        out.fixed(&snapshot.state_root);
        out.finish()
    }

    fn native_fee_policy(&mut self) -> Result<HumanResponse, HumanOperationError> {
        let snapshot = self
            .node
            .native_fee_policy(39)
            .map_err(|_| HumanOperationError::Unavailable)?;
        let policy = snapshot.value;
        let currency =
            std::str::from_utf8(&policy.asset.symbol).map_err(|_| HumanOperationError::Refused)?;
        let mut out = Encoder::new();
        out.u8(policy.version);
        out.fixed(&policy.asset.asset_id);
        out.text(currency)?;
        out.u8(policy.asset.decimals);
        out.u64(snapshot.observed_sequence);
        out.fixed(&snapshot.state_root);
        out.finish()
    }

    fn balance(&mut self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        let (
            account,
            asset,
            currency,
            observed_at,
            age_seconds,
            maximum_age_seconds,
            authorization,
        ) = self.authority.balance_context(peer)?;
        if maximum_age_seconds == 0 || age_seconds > maximum_age_seconds {
            return Err(HumanOperationError::Unavailable);
        }
        if let Some(scope) = &peer.subject {
            let canonical = layerx_types::account::AccountId::parse(&scope.account)
                .map_err(|_| HumanOperationError::Refused)?;
            if layerx_wire::hash::account_id_for_protocol(&canonical, 3)
                .map_err(|_| HumanOperationError::Refused)?
                != account
                || scope.asset != asset
            {
                return Err(HumanOperationError::Refused);
            }
        }
        let identity = Sha256::digest([peer.tenant.as_bytes(), peer.principal.as_bytes()].concat());
        let correlation = u64::from_be_bytes(
            identity[..8]
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        );
        let balance = self
            .node
            .balance(
                account,
                asset,
                VerificationLevel::CHECKPOINT_FINALISED,
                correlation,
                authorization,
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
        let freshness = balance.freshness();
        let mut out = Encoder::new();
        out.fixed(&balance.account);
        out.fixed(&balance.asset);
        out.text(&currency)?;
        out.text(&observed_at)?;
        out.u64(age_seconds);
        out.u128(balance.amount.value());
        out.u8(verification_code(balance.achieved()));
        out.u64(freshness.global_sequence);
        out.u64(freshness.batch_number);
        out.u64(freshness.observed_head_sequence);
        out.fixed(&freshness.observed_checkpoint);
        out.bytes(balance.canonical_bytes())?;
        out.bytes(balance.proof_material())?;
        out.finish()
    }

    fn head(&self, _peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        let head = self.node.head();
        let mut out = Encoder::new();
        out.u64(head.chain_sequence);
        out.u64(head.sealed_batch);
        out.fixed(&head.finalised_checkpoint);
        out.finish()
    }

    fn evidence(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        self.receipt_by_idempotency_key(peer, idempotency_key, expected_activity_id)
    }
}

impl<A: HumanAuthorityBoundary> ProductionHumanOperations<A> {
    fn authenticated_subscription_scope(
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        scope: &layerx_agent_api::subscription::SubscriptionScope,
    ) -> Result<(), HumanOperationError> {
        if scope.tenant.as_str() != context.peer().tenant
            || scope.agent.as_str().as_bytes() != context.principal().agent.as_bytes()
        {
            return Err(HumanOperationError::Refused);
        }
        Ok(())
    }
    fn subscription_operation_error(
        error: crate::events::subscription::SubscriptionError,
    ) -> HumanOperationError {
        use crate::events::subscription::SubscriptionError;
        match error {
            SubscriptionError::Corrupt
            | SubscriptionError::Durable(_)
            | SubscriptionError::SharedUnavailable => HumanOperationError::Unavailable,
            _ => HumanOperationError::Refused,
        }
    }

    fn prepare_gated(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanPrepare>,
        gate: Option<(
            &crate::agent_rpc_peer::RpcOwnerContext<'_>,
            &crate::session_control::SessionControl,
        )>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if !self.unified_owner_active {
            return Err(HumanOperationError::Unavailable);
        }
        self.require_write_admission(&peer.tenant)?;
        if prepare_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let activity_type = ActivityType::from_u32(request.operation.activity_type)
            .map_err(|_| HumanOperationError::Refused)?;
        let actor = Did::new(request.operation.actor.as_bytes())
            .map_err(|_| HumanOperationError::Refused)?;
        let authority = decode_owner_authority(&request.operation.authority)
            .map_err(|_| HumanOperationError::Refused)?;
        self.subject_owner(peer, &actor, &authority)?;
        let timestamp =
            TimestampBound::new(request.operation.not_before, request.operation.not_after)
                .map_err(|_| HumanOperationError::Refused)?;
        let budget_context = self.budget_mutation_context(
            peer,
            activity_type,
            &request.operation.payload,
            &actor,
            &authority,
        )?;

        let protocol_version = self.node.handshake().node().protocol_version;
        let mut boundary =
            ProductionCorePreparationBoundary::new(&mut self.node, request.request_id)
                .map_err(map_core)?;
        let prepared = budget_aware_prepare(
            budget_context.as_ref(),
            &mut boundary,
            PreparationDefaults {
                timestamp_span: self.timestamp_span,
                fee_limit: Amount::from_u128(request.operation.fee_limit),
                maximum_payload_bytes: self.maximum_payload_bytes,
            },
            PrepareRequest {
                actor,
                authority,
                activity_type,
                expected_account_sequence: Some(request.operation.account_sequence),
                timestamp_bound: Some(timestamp),
                fee_limit: Some(Amount::from_u128(request.operation.fee_limit)),
                idempotency_key: IdempotencyKey::new(
                    digest_from_hex(&request.operation.idempotency_key)
                        .ok_or(HumanOperationError::Refused)?,
                ),
                payload: request.operation.payload,
                declared_payload_limit: self.maximum_payload_bytes,
            },
            protocol_version,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let registry = boundary
            .last_state()
            .ok_or(HumanOperationError::Unavailable)?
            .module_registry
            .clone();
        if prepared.envelope.payload_hash() != request.operation.payload_hash {
            return Err(HumanOperationError::Refused);
        }
        let reference = hex(&Sha256::digest(&prepared.canonical_bytes));
        let cached_key = (
            peer.tenant.clone(),
            peer.principal.clone(),
            reference.clone(),
        );
        let cached = CachedPreparation {
            prepared: prepared.clone(),
            registry,
        };
        let cache = &mut self.prepared;
        let effect = move || {
            if cache.insert(cached_key, cached).is_some() {
                Err(HumanOperationError::Refused)
            } else {
                Ok(())
            }
        };
        match gate {
            None => effect()?,
            Some((context, control)) => context
                .commit(control, |_| {
                    effect().map_err(crate::session_control::SessionControlError::Human)
                })
                .map_err(rpc_commit_error)?,
        }
        let mut out = Encoder::new();
        out.text(&reference)?;
        out.bytes(&prepared.canonical_bytes)?;
        out.bytes(&prepared.signing_preimage)?;
        out.u32(prepared.envelope.activity_type().value());
        out.text(
            std::str::from_utf8(prepared.envelope.actor_did().as_bytes())
                .map_err(|_| HumanOperationError::Refused)?,
        )?;
        out.text(&request.operation.authority)?;
        out.u64(prepared.envelope.account_sequence());
        out.u64(prepared.envelope.timestamp_bound().not_before());
        out.u64(prepared.envelope.timestamp_bound().not_after());
        out.u128(prepared.envelope.fee_limit().value());
        out.bytes(prepared.envelope.payload().as_bytes())?;
        out.fixed(&prepared.envelope.payload_hash());
        out.fixed(&prepared.envelope.idempotency_key().bytes());
        out.finish()
    }
}

impl<A: HumanAuthorityBoundary> HumanOperations for ProductionHumanOperations<A> {
    fn budget_reconciliation(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::budget::BudgetTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.tenant.as_str() != peer.tenant {
            return Err(HumanOperationError::Refused);
        }
        let budget_id =
            digest_from_hex(request.budget_id.as_str()).ok_or(HumanOperationError::Refused)?;
        self.agent_budget_state(peer, budget_id)
    }
    fn operator_command(
        &mut self,
        peer: &HumanPeer,
        operator_id: &str,
        request_id: [u8; 32],
        command: OperatorCommand,
    ) -> Result<HumanResponse, HumanOperationError> {
        if matches!(command, OperatorCommand::CreateBudget { .. }) {
            return self.create_tenant_budget(peer, operator_id, request_id, command);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let root = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?
            .root()
            .to_path_buf();
        let restored = Outbox::default();
        let outbox = self.outboxes.get(&peer.tenant).unwrap_or(&restored);
        route_operator_command(&root, &tenant, outbox, operator_id, request_id, command)
    }
    fn authorize_subject(&mut self, peer: &HumanPeer) -> Result<(), HumanOperationError> {
        self.authority.authorize_subject(peer)?;
        let mut store = self
            .store
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        subject::retain(&mut store, peer)
    }
    fn account_state(
        &mut self,
        peer: &HumanPeer,
        account_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        let (_, _, _, _, age, maximum_age, authorization) = self.authority.balance_context(peer)?;
        if account_id == [0; 32] || maximum_age == 0 || age > maximum_age {
            return Err(HumanOperationError::Unavailable);
        }
        let correlation = boundary_correlation(peer, &account_id, b"account-state");
        let value = self
            .node
            .account(
                account_id,
                VerificationLevel::STATE_PROVEN,
                correlation,
                authorization,
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
        let decoded =
            layerx_proof::state::decode_account_value(account_id, value.canonical_bytes())
                .map_err(|_| HumanOperationError::Refused)?;
        let mut out = Encoder::new();
        out.fixed(&decoded.account_id);
        out.u8(value.achieved().wire_rank());
        out.bytes(value.canonical_bytes())?;
        out.bytes(value.proof_material())?;
        out.u64(value.freshness().observed_head_sequence);
        out.finish()
    }
    fn registry(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        // Mutable authenticated authority access is intentionally required; the
        // listener calls prepare first in normal operation. A readiness owner
        // must probe authority before accepting the socket.
        Self::registry_response(&self.authority.registry(peer).map_err(map_core)?)
    }

    fn account_sequence(
        &mut self,
        peer: &HumanPeer,
        actor: &str,
        authority: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        if actor.is_empty() || authority.is_empty() {
            return Err(HumanOperationError::Refused);
        }
        let actor = Did::new(actor.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
        let owner = decode_owner_authority(authority).map_err(|_| HumanOperationError::Refused)?;
        self.subject_owner(peer, &actor, &owner)?;
        let correlation = boundary_correlation(peer, actor.as_bytes(), b"account-sequence");
        let mut boundary = ProductionCorePreparationBoundary::new(&mut self.node, correlation)
            .map_err(map_core)?;
        let state =
            crate::prepare::CorePreparationBoundary::preparation_state(&mut boundary, &actor)
                .map_err(map_core)?;
        let mut out = Encoder::new();
        out.u64(state.account_sequence);
        out.finish()
    }

    fn prepare(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanPrepare>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.prepare_gated(peer, request, None)
    }

    fn submit_external(
        &mut self,
        peer: &HumanPeer,
        request: MutationEnvelope<HumanSubmit>,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.submit_external_with_origin(peer, request, None)
    }

    fn track(
        &mut self,
        peer: &HumanPeer,
        submission_ref: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        self.last_verified_receipt = None;
        let id = *self
            .submissions
            .get(&(
                peer.tenant.clone(),
                peer.principal.clone(),
                submission_ref.to_owned(),
            ))
            .ok_or(HumanOperationError::Refused)?;
        let status = self
            .outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .status(id)
            .ok_or(HumanOperationError::Refused)?
            .clone();
        if status.state == SubmissionState::Queued {
            return self.resume_queued(peer, id, status.activity_id);
        }
        if status.state == SubmissionState::Submitted {
            let mut store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            self.outboxes
                .entry(peer.tenant.clone())
                .or_default()
                .transition(
                    &mut store,
                    id,
                    SubmissionState::Unknown,
                    "recover durable submission before receipt resolution",
                    None,
                )
                .map_err(|_| HumanOperationError::Unavailable)?;
        }
        if matches!(
            status.state,
            SubmissionState::Submitted | SubmissionState::Acknowledged | SubmissionState::Unknown
        ) {
            match self.receipt_by_idempotency_key(peer, id, status.activity_id) {
                Ok(_) | Err(HumanOperationError::Unavailable) => {}
                Err(error) => return Err(error),
            }
        }
        let current = self
            .outboxes
            .entry(peer.tenant.clone())
            .or_default()
            .status(id)
            .ok_or(HumanOperationError::Refused)?;
        if let Some(evidence) = current.evidence {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            let served = crate::receipt::serve(
                &store,
                TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?,
                crate::receipt::ReceiptLookupKey::Idempotency(id),
            )
            .map_err(|_| HumanOperationError::Unavailable)?;
            if !persisted_terminal_receipt_matches(&served.canonical_bytes, evidence.receipt_ref())
                || served.metadata.activity_id != current.activity_id
                || served.metadata.idempotency_key != id
                || (served.metadata.result.code.raw() == 0)
                    != (current.state == SubmissionState::Executed)
            {
                return Err(HumanOperationError::Refused);
            }
            self.last_verified_receipt = Some((
                id,
                served.metadata.result.code.raw(),
                served.metadata.global_sequence,
            ));
        }
        Self::observation(current)
    }

    fn receipt_by_idempotency_key(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        self.last_verified_receipt = None;
        if !self
            .submissions
            .values()
            .any(|value| *value == idempotency_key)
            || !self.submissions.contains_key(&(
                peer.tenant.clone(),
                peer.principal.clone(),
                hex(&idempotency_key),
            ))
        {
            return Err(HumanOperationError::Refused);
        }
        let original = self.retained_activity(peer, idempotency_key)?;
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let proof_peer = subject::for_activity(&self.store, peer, &original, &registry)?;
        let authority =
            self.authority
                .authorized_activity(&proof_peer, &original, expected_activity_id)?;
        let retained = RetainedNativeOwner::decode(
            original,
            &registry,
            self.node.handshake().node(),
            idempotency_key,
            expected_activity_id,
        )?;
        let native = retained.as_ref().map(RetainedNativeOwner::context);
        let selector = ReceiptSelector::IdempotencyKey {
            idempotency_key,
            expected_activity_id,
        };
        let correlation = u64::from_be_bytes(
            idempotency_key[..8]
                .try_into()
                .map_err(|_| HumanOperationError::Refused)?,
        );
        let lookup = if let Some(expected) = &native {
            self.node
                .lookup_native_owner_receipt(selector, correlation, authority, expected)
        } else {
            self.node.lookup_receipt(selector, correlation, authority)
        }
        .map_err(native_receipt::map_lookup_error)?;
        let mut out = Encoder::new();
        match lookup {
            Lookup::Absent => out.u8(0),
            Lookup::Verified(receipt) => {
                let protocol = receipt
                    .receipt()
                    .protocol()
                    .ok_or(HumanOperationError::Refused)?;
                let tenant =
                    TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
                let mut served = native_receipt::persist(
                    &self.store,
                    tenant.clone(),
                    idempotency_key,
                    &receipt,
                    &authority,
                    native.as_ref(),
                )?;
                if served.canonical_bytes != receipt.canonical_bytes()
                    || served.metadata.idempotency_key != idempotency_key
                    || served.metadata.activity_id != expected_activity_id
                    || served.metadata.activity_id != protocol.activity_id()
                    || served.metadata.global_sequence != protocol.global_sequence()
                    || served.metadata.result.code.raw() != protocol.result_code()
                    || served.metadata.verification_level < receipt.level()
                {
                    return Err(HumanOperationError::Refused);
                }
                served = self.augment_receipt_evidence(
                    peer,
                    idempotency_key,
                    tenant,
                    served,
                    &authority,
                    native.as_ref(),
                )?;
                self.last_verified_receipt = Some((
                    idempotency_key,
                    served.metadata.result.code.raw(),
                    served.metadata.global_sequence,
                ));
                out.u8(1);
                out.bytes(receipt.canonical_bytes())?;
                out.fixed(&authority.batch_id());
                out.fixed(&authority.asset());
                out.fixed(&authority.previous_state_root());
                out.fixed(&authority.resulting_state_root());
                out.fixed(&authority.sequencer_public_key());
                out.u8(served.metadata.verification_level.wire_rank());
            }
        }
        out.finish()
    }
    fn balance(&mut self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::balance(self, peer)
    }
    fn native_fee_policy(
        &mut self,
        _peer: &HumanPeer,
    ) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::native_fee_policy(self)
    }
    fn session_seed_prepare(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: [u8; 32],
        _: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn session_fee_state(
        &mut self,
        _peer: &HumanPeer,
        grant_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::session_fee_state(self, grant_id)
    }
    fn head(&self, peer: &HumanPeer) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::head(self, peer)
    }
    fn evidence(
        &mut self,
        peer: &HumanPeer,
        idempotency_key: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        ProductionHumanOperations::evidence(self, peer, idempotency_key, expected_activity_id)
    }
    fn approval_list(
        &mut self,
        _: &HumanPeer,
        _: u64,
        _: Option<[u8; 32]>,
        _: u8,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn approval_get(
        &mut self,
        _: &HumanPeer,
        _: [u8; 32],
        _: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn approval_approve(
        &mut self,
        _: &HumanPeer,
        _: [u8; 32],
        _: [u8; 32],
        _: &str,
        _: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn approval_reject(
        &mut self,
        _: &HumanPeer,
        _: [u8; 32],
        _: [u8; 32],
        _: &str,
        _: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn identity_resolve(
        &mut self,
        _: &HumanPeer,
        _: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn lease_map(
        &mut self,
        _: &HumanPeer,
        _: u64,
        _: u64,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn owner_validate(
        &mut self,
        _: &HumanPeer,
        _: HumanOwnerInstall,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn owner_install(
        &mut self,
        _: &HumanPeer,
        _: MutationEnvelope<HumanOwnerInstall>,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn capability_install(
        &mut self,
        peer: &HumanPeer,
        request: HumanCapabilityInstall,
    ) -> Result<HumanResponse, HumanOperationError> {
        install_capability(&mut self.authority, &self.store, peer, &request)
    }
    fn agent_lifecycle_publish(
        &mut self,
        _: &HumanPeer,
        _: MutationEnvelope<crate::human::HumanAgentLifecycleSeed>,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_list(
        &mut self,
        _: &HumanPeer,
        _: Option<[u8; 32]>,
        _: u8,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_get(&mut self, _: &HumanPeer, _: &str) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_control(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: bool,
        _: [u8; 32],
        _: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_limit(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: u128,
        _: &str,
        _: [u8; 32],
        _: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_journey(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: crate::human::HumanAgentJourneyKind,
        _: [u8; 32],
        _: [u8; 32],
        _: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_archive(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: &str,
        (_, _, _): ([u8; 32], [u8; 32], [u8; 32]),
        _: HumanFinalizationEvidence,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_context(
        &mut self,
        _: &HumanPeer,
        _: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_budget_state(
        &mut self,
        _: &HumanPeer,
        _: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_key_policy(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: bool,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_session_snapshot(
        &mut self,
        _: &HumanPeer,
        _: &str,
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_session_suspend(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }
    fn agent_session_bind(
        &mut self,
        _: &HumanPeer,
        _: &str,
        _: [u8; 32],
        _: [u8; 32],
        _: [u8; 32],
    ) -> Result<HumanResponse, HumanOperationError> {
        Err(HumanOperationError::Unavailable)
    }

    fn budget_list(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::budget::BudgetList,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.tenant.as_str() != peer.tenant {
            return Err(HumanOperationError::Refused);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let owners = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            managed_agent::budget_owners(&store, &tenant)?
        };
        let owners: Vec<&managed_agent::BudgetOwner> = owners
            .iter()
            .filter(|owner| owner.agent_did == request.agent_did.as_str())
            .collect();
        let mut out = Encoder::new();
        out.u16(owners.len())?;
        for owner in owners {
            out.fixed(&owner.active_budget_id);
            out.text(&owner.agent_id)?;
        }
        out.finish()
    }

    fn read_module_state(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::ModuleStateSelector>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let module_id = production_module_id(&request.selector.module)?;
        let requested = production_verification_level(request.requested_verification_level);
        let (_, _, _, _, age, maximum_age, authorization) = self.authority.balance_context(peer)?;
        if maximum_age == 0 || age > maximum_age {
            return Err(HumanOperationError::Unavailable);
        }
        let key = request.selector.key.as_bytes();
        let correlation = boundary_correlation(peer, key, b"module-state");
        let value = self
            .node
            .module_state(module_id, key, requested, correlation, authorization)
            .map_err(|_| HumanOperationError::Unavailable)?;
        if value.achieved() < requested {
            return Err(HumanOperationError::Refused);
        }
        let proof = value.proof_material();
        if proof.len() > MAX_RESPONSE {
            return Err(HumanOperationError::Refused);
        }
        let mut out = Encoder::new();
        out.u8(value.achieved().wire_rank());
        out.bytes(value.canonical_bytes())?;
        out.u32(u32::try_from(proof.len()).map_err(|_| HumanOperationError::Refused)?);
        out.fixed(proof);
        out.finish()
    }
    fn read_batch(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::BatchRef>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let batch_number = production_batch_number(&request.selector)?;
        let requested = production_verification_level(request.requested_verification_level);
        let achieved = VerificationLevel::SEQUENCER_SIGNED;
        if achieved < requested {
            return Err(HumanOperationError::Refused);
        }
        let correlation = boundary_correlation(peer, &batch_number.to_be_bytes(), b"batch-header");
        let signed = self
            .node
            .batch_header(batch_number, correlation)
            .map_err(production_batch_header_error)?;
        if signed.header.batch_number() != batch_number
            || signed.header.first_sequence() > signed.header.last_sequence()
        {
            return Err(HumanOperationError::Refused);
        }
        let mut out = Encoder::new();
        out.u64(signed.header.batch_number());
        out.u64(signed.header.first_sequence());
        out.u64(signed.header.last_sequence());
        out.u8(achieved.wire_rank());
        out.bytes(signed.canonical_bytes())?;
        out.finish()
    }
    fn wait(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::track::WaitRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        let requested = production_verification_level(request.requested_verification_level);
        let deadline_ms = request
            .deadline
            .get()
            .checked_mul(1000)
            .ok_or(HumanOperationError::Refused)?;
        let submission_ref = request.submission_ref.as_str();
        let tracked = HumanOperations::track(self, peer, submission_ref)?;
        let id = *self
            .submissions
            .get(&(
                peer.tenant.clone(),
                peer.principal.clone(),
                submission_ref.to_owned(),
            ))
            .ok_or(HumanOperationError::Refused)?;
        let achieved = if self
            .last_verified_receipt
            .is_some_and(|(verified, _, _)| verified == id)
        {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            crate::receipt::serve(
                &store,
                TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?,
                crate::receipt::ReceiptLookupKey::Idempotency(id),
            )
            .map_err(|_| HumanOperationError::Unavailable)?
            .metadata
            .verification_level
        } else {
            VerificationLevel::UNVERIFIED
        };
        let deadline_elapsed = if achieved >= requested {
            false
        } else {
            let reading = self
                .clock
                .sample(Duration::from_secs(1))
                .map_err(|_| HumanOperationError::Unavailable)?;
            reading.unix_milliseconds >= deadline_ms
        };
        let mut out = Encoder::new();
        out.u8(u8::from(deadline_elapsed));
        out.u8(achieved.wire_rank());
        out.fixed(tracked.bytes());
        out.finish()
    }

    fn subscription_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionList,
    ) -> Result<HumanResponse, HumanOperationError> {
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        let records = context
            .permit()
            .with_subscription_authority(
                control,
                crate::tenant::Operation::SubscriptionList,
                |sessions, token, observability, core_sequence| {
                    store
                        .list_authorized(
                            sessions,
                            token,
                            observability,
                            core_sequence,
                            &request.scope,
                        )
                        .map(|mut records| {
                            records.extend(store.list(&request.scope));
                            records
                        })
                },
            )
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let mut out = Encoder::new();
        out.u32(u32::try_from(records.len()).map_err(|_| HumanOperationError::Unavailable)?);
        for record in &records {
            encode_subscription_record(&mut out, record)?;
        }
        out.finish()
    }

    fn subscription_pause(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        let record = store
            .pause_permitted(control, context.permit(), request)
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let mut out = Encoder::new();
        encode_subscription_record(&mut out, &record)?;
        out.finish()
    }
    fn subscription_resume(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        let record = store
            .resume_permitted(control, context.permit(), request)
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let mut out = Encoder::new();
        encode_subscription_record(&mut out, &record)?;
        out.finish()
    }
    fn subscription_delete(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        store
            .delete_permitted(control, context.permit(), request)
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let mut out = Encoder::new();
        out.u8(0);
        out.finish()
    }
    fn subscription_acknowledge(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::CursorAcknowledgement,
    ) -> Result<HumanResponse, HumanOperationError> {
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let cursor = request.cursor;
        let store = self.subscription_store_for(tenant)?;
        let record = store
            .acknowledge_permitted(control, context.permit(), request)
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        if record.last_acknowledged != cursor {
            return Err(HumanOperationError::Unavailable);
        }
        let mut out = Encoder::new();
        encode_subscription_record(&mut out, &record)?;
        out.finish()
    }

    fn session_refresh(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::SessionRefresh,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.context.tenant.as_str() != context.peer().tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let target =
            session_id_hex(request.session_id.as_str()).ok_or(HumanOperationError::Refused)?;
        let current_sequence = self.node.head().chain_sequence;
        let (replacement, _) = control
            .refresh_session_authorized(context.permit(), SessionId(target), current_sequence)
            .map_err(UnifiedAgentOwner::<A>::session_writer_error)?;
        let credential = replacement.credential();
        if credential.tenant().as_str() != context.peer().tenant.as_str()
            || credential.session_id() != SessionId(target)
        {
            return Err(HumanOperationError::Unavailable);
        }
        let tenant =
            TenantId::new(context.peer().tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let registry = control.registry();
        let sessions = registry
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let record = sessions
            .get(&tenant, SessionId(target))
            .ok_or(HumanOperationError::Unavailable)?;
        if !record.open
            || record.generation != credential.generation()
            || record.request.token_id != credential.token_id()
        {
            return Err(HumanOperationError::Unavailable);
        }
        let mut out = Encoder::new();
        out.text(credential.tenant().as_str())?;
        out.fixed(&credential.session_id().0);
        out.fixed(&credential.token_id());
        out.u64(credential.generation());
        out.u64(record.request.expiry_sequence);
        UnifiedAgentOwner::<A>::encode_session_record(&mut out, record)?;
        out.finish()
    }
    fn session_close(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::SessionClose,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.context.tenant.as_str() != context.peer().tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let target =
            session_id_hex(request.session_id.as_str()).ok_or(HumanOperationError::Refused)?;
        let current_sequence = self.node.head().chain_sequence;
        control
            .close_session_authorized(context.permit(), SessionId(target), current_sequence)
            .map_err(UnifiedAgentOwner::<A>::session_writer_error)?;
        let tenant =
            TenantId::new(context.peer().tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let registry = control.registry();
        let sessions = registry
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let record = sessions
            .get(&tenant, SessionId(target))
            .ok_or(HumanOperationError::Unavailable)?;
        let mut out = Encoder::new();
        UnifiedAgentOwner::<A>::encode_session_record(&mut out, record)?;
        out.finish()
    }

    fn read_history(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<layerx_agent_api::read::HistorySelector>,
    ) -> Result<HumanResponse, HumanOperationError> {
        let selector = request.selector;
        let first = selector.first.0;
        let last = selector.last.0;
        let page_bound =
            u16::try_from(selector.page_limit).map_err(|_| HumanOperationError::Refused)?;
        let cursor = selector
            .cursor
            .as_ref()
            .map(|text| {
                crate::read::Cursor::from_hex(text.as_str())
                    .map(|cursor| {
                        layerx_client::read::HistoryCursor::from_coordinates(
                            cursor.next_sequence,
                            cursor.end_sequence,
                            cursor.observed_head_sequence,
                            cursor.observed_checkpoint,
                        )
                    })
                    .map_err(|_| HumanOperationError::Refused)
            })
            .transpose()?;
        let requested = production_verification_level(request.requested_verification_level);
        let (_, _, _, _, age, maximum_age, authorization) = self.authority.balance_context(peer)?;
        if maximum_age == 0 || age > maximum_age {
            return Err(HumanOperationError::Unavailable);
        }
        let mut range = [0_u8; 16];
        range[..8].copy_from_slice(&first.to_be_bytes());
        range[8..].copy_from_slice(&last.to_be_bytes());
        let correlation = boundary_correlation(peer, &range, b"history");
        let page = self
            .node
            .history(first, last, page_bound, cursor, requested, correlation, authorization)
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut out = Encoder::new();
        out.u16(page.items.len())?;
        for item in &page.items {
            if item.achieved() < requested {
                return Err(HumanOperationError::Refused);
            }
            let kind = match item.kind {
                layerx_client::read::HistoryKind::Activity => 1,
                layerx_client::read::HistoryKind::Receipt => 2,
                layerx_client::read::HistoryKind::Event => 3,
            };
            let proof = item.proof_material();
            if proof.len() > MAX_RESPONSE {
                return Err(HumanOperationError::Refused);
            }
            out.u64(item.global_sequence);
            out.u8(kind);
            out.u8(item.achieved().wire_rank());
            out.bytes(item.canonical_bytes())?;
            out.u32(u32::try_from(proof.len()).map_err(|_| HumanOperationError::Refused)?);
            out.fixed(proof);
            if out.0.len() > MAX_RESPONSE {
                return Err(HumanOperationError::Refused);
            }
        }
        match page.cursor {
            None => out.u8(0),
            Some(next) => {
                out.u8(1);
                out.text(
                    &crate::read::Cursor {
                        next_sequence: next.next_sequence(),
                        end_sequence: next.end_sequence(),
                        observed_head_sequence: next.head_sequence(),
                        observed_checkpoint: next.checkpoint(),
                    }
                    .to_hex(),
                )?;
            }
        }
        if out.0.len() > MAX_RESPONSE {
            return Err(HumanOperationError::Refused);
        }
        out.finish()
    }

    fn subscription_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::subscription::SubscriptionCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if subscription_create_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        Self::authenticated_subscription_scope(context, &request.operation.scope)?;
        let subscription_id = subscription_create_identity(
            context.peer().tenant.as_bytes(),
            context.principal().agent.as_bytes(),
            request.operation.scope.capability.as_str().as_bytes(),
            &request.key,
        )?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        let record = store
            .create_permitted(control, context.permit(), subscription_id, request.operation)
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let mut out = Encoder::new();
        encode_subscription_record(&mut out, &record)?;
        out.finish()
    }

    fn session_list(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::identity::SessionList,
    ) -> Result<HumanResponse, HumanOperationError> {
        if request.0.tenant.as_str() != peer.tenant.as_str() {
            return Err(HumanOperationError::Refused);
        }
        let tenant =
            TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let registry = self
            .session_control
            .as_ref()
            .ok_or(HumanOperationError::Unavailable)?
            .registry();
        let sessions = registry
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let mut records: Vec<&session::SessionRecord> = sessions.tenant_sessions(&tenant).collect();
        records.sort_by_key(|record| record.request.session_id.0);
        let mut out = Encoder::new();
        out.u16(records.len())?;
        for record in records {
            UnifiedAgentOwner::<A>::encode_session_record(&mut out, record)?;
        }
        out.finish()
    }

    fn subscription_health(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::subscription::SubscriptionTarget,
    ) -> Result<HumanResponse, HumanOperationError> {
        use crate::events::subscription::Continuity;
        Self::authenticated_subscription_scope(context, &request.scope)?;
        let tenant = TenantId::new(context.peer().tenant.clone())
            .map_err(|_| HumanOperationError::Refused)?;
        let store = self.subscription_store_for(tenant)?;
        let (record, continuity) = context
            .permit()
            .with_subscription_authority(
                control,
                crate::tenant::Operation::SubscriptionHealth,
                |sessions, token, observability, core_sequence| {
                    store.health_continuity_authorized(
                        sessions,
                        token,
                        observability,
                        core_sequence,
                        &request,
                    )
                },
            )
            .map_err(rpc_commit_error)?
            .map_err(Self::subscription_operation_error)?;
        let pending_backfill = match continuity {
            Continuity::Healthy => None,
            Continuity::GapBlocked {
                missing_first,
                missing_last,
                backfill_attempted,
                recovered_through,
            } => {
                let backfill_cursor = match recovered_through {
                    Some(recovered) => recovered
                        .checked_add(1)
                        .ok_or(HumanOperationError::Unavailable)?,
                    None => missing_first,
                };
                Some((missing_first, missing_last, backfill_cursor, backfill_attempted))
            }
            Continuity::Truncated {
                requested_from,
                oldest_available,
                lost_through,
            } => Some((requested_from, lost_through, oldest_available, false)),
        };
        let mut out = Encoder::new();
        out.text(record.scope.tenant.as_str())?;
        out.text(record.scope.agent.as_str())?;
        out.text(record.scope.capability.as_str())?;
        out.text(record.subscription_id.as_str())?;
        out.text(&record.last_acknowledged.0 .0.to_string())?;
        out.u8(0);
        match pending_backfill {
            None => out.u8(0),
            Some((missing_first, missing_last, backfill_cursor, backfill_attempted)) => {
                out.u8(1);
                out.text(&missing_first.to_string())?;
                out.text(&missing_last.to_string())?;
                out.text(&backfill_cursor.to_string())?;
                out.u8(u8::from(backfill_attempted));
            }
        }
        out.finish()
    }

    fn availability_fetch(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::availability::AvailabilityRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        fn classes(
            out: &mut Encoder,
            tallies: &BTreeMap<(String, u8), (u32, u64)>,
            provider: &str,
            report: &layerx_proof::availability::ClassReport,
        ) -> Result<(), HumanOperationError> {
            out.u16(report.obtained.len() + report.missing.len())?;
            for (class, complete) in report
                .obtained
                .iter()
                .map(|class| (*class, true))
                .chain(report.missing.iter().map(|class| (*class, false)))
            {
                let (chunks, bytes) = tallies
                    .get(&(provider.to_owned(), class as u8))
                    .copied()
                    .unwrap_or((0, 0));
                out.u8(class as u8);
                out.u8(u8::from(complete));
                out.u32(chunks);
                out.u64(bytes);
                let failure: &[u8] = if complete { b"" } else { b"missing_class" };
                out.u32(u32::try_from(failure.len()).map_err(|_| HumanOperationError::Refused)?);
                out.fixed(failure);
            }
            Ok(())
        }
        let request = request.validate().map_err(|_| HumanOperationError::Refused)?;
        let layerx_agent_api::availability::AvailabilityRequest {
            selector,
            requested_verification_level,
            maximum_bytes,
            maximum_chunks,
            deadline,
        } = request;
        let batch_number = production_batch_number(
            &layerx_agent_api::read::BatchRef::new(selector)
                .map_err(|_| HumanOperationError::Refused)?,
        )?;
        let requested = production_verification_level(requested_verification_level);
        if VerificationLevel::BATCH_INCLUDED < requested {
            return Err(HumanOperationError::Refused);
        }
        let deadline_ms = deadline
            .get()
            .checked_mul(1000)
            .ok_or(HumanOperationError::Refused)?;
        let now_ms = self
            .clock
            .sample(Duration::from_secs(1))
            .map_err(|_| HumanOperationError::Unavailable)?
            .unix_milliseconds;
        let remaining_ms = deadline_ms
            .checked_sub(now_ms)
            .filter(|remaining| *remaining > 0)
            .ok_or(HumanOperationError::Refused)?;
        let limits = layerx_client::availability::RetrievalLimits {
            maximum_bytes: usize::try_from(maximum_bytes).map_err(|_| HumanOperationError::Refused)?,
            maximum_chunks: usize::try_from(maximum_chunks)
                .map_err(|_| HumanOperationError::Refused)?,
            deadline: Duration::from_millis(remaining_ms),
        };
        let correlation = boundary_correlation(peer, &batch_number.to_be_bytes(), b"batch-header");
        let signed = self
            .node
            .batch_header(batch_number, correlation)
            .map_err(production_batch_header_error)?;
        let header = &signed.header;
        if header.batch_number() != batch_number {
            return Err(HumanOperationError::Refused);
        }
        let context = layerx_client::availability::FetchContext {
            interface_version: self.node.handshake().node().interface_version,
            correlation_id: boundary_correlation(
                peer,
                &batch_number.to_be_bytes(),
                b"availability-fetch",
            ),
            expected_batch_number: batch_number,
            data_availability_root: header.data_availability_root(),
            record_roots: layerx_proof::availability::RootCommitments {
                activity: header.activity_merkle_root(),
                receipt: header.receipt_merkle_root(),
                event: header.event_merkle_root(),
                oracle: header.oracle_root(),
            },
            limits,
        };
        let mut tallies: BTreeMap<(String, u8), (u32, u64)> = BTreeMap::new();
        let mut overflow = false;
        let outcome = self
            .node
            .fetch_availability(
                layerx_client::availability::AvailabilitySelector::Batch(batch_number),
                context,
                |progress| {
                    let chunk = progress.chunk.chunk();
                    let entry = tallies
                        .entry((progress.provider.to_owned(), chunk.class as u8))
                        .or_insert((0, 0));
                    match (
                        entry.0.checked_add(1),
                        u64::try_from(chunk.bytes.len())
                            .ok()
                            .and_then(|length| entry.1.checked_add(length)),
                    ) {
                        (Some(chunks), Some(bytes)) => *entry = (chunks, bytes),
                        _ => overflow = true,
                    }
                },
            )
            .map_err(|error| match error {
                layerx_client::availability::FetchError::UnavailableCapability
                | layerx_client::availability::FetchError::DisconnectedClient
                | layerx_client::availability::FetchError::NoProviders => {
                    HumanOperationError::Unavailable
                }
                layerx_client::availability::FetchError::InvalidLimits
                | layerx_client::availability::FetchError::InvalidSelector
                | layerx_client::availability::FetchError::CorrelationOverflow => {
                    HumanOperationError::Refused
                }
            })?;
        if overflow {
            return Err(HumanOperationError::Refused);
        }
        let mut out = Encoder::new();
        match outcome {
            layerx_client::availability::FetchOutcome::Complete(result) => {
                out.u8(VerificationLevel::BATCH_INCLUDED.wire_rank());
                out.u8(1);
                out.text(&result.provider)?;
                classes(&mut out, &tallies, &result.provider, &result.report.classes)?;
                out.u16(1)?;
                out.text(&result.provider)?;
                classes(&mut out, &tallies, &result.provider, &result.report.classes)?;
                out.u32(0);
            }
            layerx_client::availability::FetchOutcome::Partial(reports) => {
                if VerificationLevel::UNVERIFIED < requested {
                    return Err(HumanOperationError::Refused);
                }
                out.u8(VerificationLevel::UNVERIFIED.wire_rank());
                out.u8(0);
                out.u16(0)?;
                out.u16(reports.len())?;
                for report in &reports {
                    out.text(&report.provider)?;
                    classes(&mut out, &tallies, &report.provider, &report.classes)?;
                    let failure = format!("{:?}", report.failure);
                    out.bytes(failure.as_bytes())?;
                }
            }
        }
        out.finish()
    }

    fn capability_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityCreate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if capability_create_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let (tenant, actor, agent) = capability_coordinates(
            context,
            request.operation.tenant.as_str(),
            request.operation.agent_did.as_str(),
        )?;
        let snapshot = core_preparation_snapshot(&mut self.node, context.peer(), &actor)?;
        let now_ms = snapshot.protocol_timestamp;
        let head = snapshot.observed_head_sequence;
        let authority = capability_session_authority(context, control)?;
        let observed = capability_grant_scope(
            &mut self.authority,
            &self.store,
            context,
            &tenant,
            &actor,
            &authority,
        )?;
        let id = crate::capability::timed::create_id(tenant.as_str(), &agent, &request.key)
            .map_err(|error| capability_refusal(&error))?;
        let record = crate::capability::timed::TimedCapability::from_public(
            id,
            None,
            tenant.clone(),
            &agent,
            authority.clone(),
            &request.operation.dimensions,
            observed.not_after_ms,
            now_ms,
            head,
            layerx_agent_api::error::RequestId(request.request_id),
        )
        .map_err(|error| capability_refusal(&error))?;
        if record.is_expired(now_ms) {
            return Err(HumanOperationError::CapabilityRefused(
                crate::capability::Dimension::Expiry,
            ));
        }
        crate::capability::timed::check_scope(&record, &observed.scope, now_ms)
            .map_err(|error| capability_refusal(&error))?;
        let shared = Arc::clone(&self.store);
        let stored = context
            .commit(control, |_| {
                let mut store = shared
                    .lock()
                    .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                crate::capability::binding::issue(
                    &mut store,
                    record,
                    now_ms,
                    head,
                    observed.module_mask,
                )
                .map_err(|error| {
                    crate::session_control::SessionControlError::Human(binding_refusal(&error))
                })
            })
            .map_err(rpc_commit_error)?;
        let stored = match stored {
            crate::capability::timed::Insert::Created(record)
            | crate::capability::timed::Insert::Replayed(record) => record,
        };
        let mut out = Encoder::new();
        encode_capability_authority(&mut out, &tenant, &agent, &authority)?;
        encode_capability_record(&mut out, &stored, now_ms)?;
        out.finish()
    }

    fn capability_attenuate(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityAttenuate>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if capability_attenuate_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let (tenant, actor, agent) = capability_coordinates(
            context,
            request.operation.tenant.as_str(),
            request.operation.agent_did.as_str(),
        )?;
        let request_id = layerx_agent_api::error::RequestId(request.request_id);
        let parent_id =
            crate::capability::timed::parse_id(request.operation.parent_id.as_str(), request_id)
                .map_err(|error| capability_refusal(&error))?;
        let snapshot = core_preparation_snapshot(&mut self.node, context.peer(), &actor)?;
        let now_ms = snapshot.protocol_timestamp;
        let authority = capability_session_authority(context, control)?;
        let observed = capability_grant_scope(
            &mut self.authority,
            &self.store,
            context,
            &tenant,
            &actor,
            &authority,
        )?;
        let id = crate::capability::timed::attenuate_id(
            tenant.as_str(),
            &agent,
            &parent_id,
            &request.key,
        )
        .map_err(|error| capability_refusal(&error))?;
        let child = crate::capability::timed::TimedCapability::from_public(
            id,
            Some(parent_id),
            tenant.clone(),
            &agent,
            authority.clone(),
            &request.operation.dimensions,
            observed.not_after_ms,
            now_ms,
            snapshot.observed_head_sequence,
            request_id,
        )
        .map_err(|error| capability_refusal(&error))?;
        if child.is_expired(now_ms) {
            return Err(HumanOperationError::CapabilityRefused(
                crate::capability::Dimension::Expiry,
            ));
        }
        crate::capability::timed::check_scope(&child, &observed.scope, now_ms)
            .map_err(|error| capability_refusal(&error))?;
        let shared = Arc::clone(&self.store);
        let stored = context
            .commit(control, |_| {
                let refuse = |error: crate::capability::timed::TimedError| {
                    crate::session_control::SessionControlError::Human(capability_refusal(&error))
                };
                let mut store = shared
                    .lock()
                    .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                let parent = crate::capability::timed::restore(&store, &tenant, &parent_id)
                    .map_err(refuse)?
                    .ok_or(crate::capability::timed::TimedError::UnknownParent)
                    .map_err(refuse)?;
                if parent.agent != agent || parent.tenant != tenant {
                    return Err(refuse(crate::capability::timed::TimedError::UnknownParent));
                }
                crate::capability::timed::require_active_chain(&store, &parent, now_ms)
                    .map_err(refuse)?;
                crate::capability::timed::require_subset(&child, &parent).map_err(refuse)?;
                crate::capability::binding::issue(
                    &mut store,
                    child,
                    now_ms,
                    snapshot.observed_head_sequence,
                    observed.module_mask,
                )
                .map_err(|error| {
                    crate::session_control::SessionControlError::Human(binding_refusal(&error))
                })
            })
            .map_err(rpc_commit_error)?;
        let stored = match stored {
            crate::capability::timed::Insert::Created(record)
            | crate::capability::timed::Insert::Replayed(record) => record,
        };
        let mut out = Encoder::new();
        encode_capability_authority(&mut out, &tenant, &agent, &authority)?;
        encode_capability_record(&mut out, &stored, now_ms)?;
        out.finish()
    }

    fn capability_list(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::capability::CapabilityList,
    ) -> Result<HumanResponse, HumanOperationError> {
        let (tenant, actor, agent) =
            capability_coordinates(context, request.tenant.as_str(), request.agent_did.as_str())?;
        let snapshot = core_preparation_snapshot(&mut self.node, context.peer(), &actor)?;
        let now_ms = snapshot.protocol_timestamp;
        let authority = capability_session_authority(context, control)?;
        let shared = Arc::clone(&self.store);
        let records = context
            .commit(control, |_| {
                let store = shared
                    .lock()
                    .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                crate::capability::timed::list(&store, &tenant, &agent).map_err(|error| {
                    crate::session_control::SessionControlError::Human(capability_refusal(&error))
                })
            })
            .map_err(rpc_commit_error)?;
        let mut out = Encoder::new();
        encode_capability_authority(&mut out, &tenant, &agent, &authority)?;
        out.u16(records.len())?;
        let mut previous: Option<[u8; 32]> = None;
        for record in &records {
            if previous.is_some_and(|last| last >= record.id) {
                return Err(HumanOperationError::Unavailable);
            }
            previous = Some(record.id);
            encode_capability_record(&mut out, record, now_ms)?;
        }
        out.finish()
    }

    fn capability_revoke(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<layerx_agent_api::capability::CapabilityRevoke>,
    ) -> Result<HumanResponse, HumanOperationError> {
        if capability_revoke_digest(&request.operation) != request.body_digest {
            return Err(HumanOperationError::Refused);
        }
        let (tenant, actor, agent) = capability_coordinates(
            context,
            request.operation.tenant.as_str(),
            request.operation.agent_did.as_str(),
        )?;
        let target = crate::capability::timed::parse_id(
            request.operation.capability_id.as_str(),
            layerx_agent_api::error::RequestId(request.request_id),
        )
        .map_err(|error| capability_refusal(&error))?;
        let snapshot = core_preparation_snapshot(&mut self.node, context.peer(), &actor)?;
        let now_ms = snapshot.protocol_timestamp;
        let head = snapshot.observed_head_sequence;
        let authority = capability_session_authority(context, control)?;
        let shared = Arc::clone(&self.store);
        let (record, _subtree) = context
            .commit(control, |_| {
                let mut store = shared
                    .lock()
                    .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                crate::capability::binding::revoke(
                    &mut store, &tenant, &agent, &target, now_ms, head,
                )
                .map_err(|error| {
                    crate::session_control::SessionControlError::Human(binding_refusal(&error))
                })
            })
            .map_err(rpc_commit_error)?;
        let mut out = Encoder::new();
        encode_capability_authority(&mut out, &tenant, &agent, &authority)?;
        encode_capability_record(&mut out, &record, now_ms)?;
        out.finish()
    }

    fn fee_projection(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::FeeProjectionRequest,
    ) -> Result<
        layerx_agent_api::read::ProjectionResult<layerx_agent_api::read::FeeProjection>,
        HumanOperationError,
    > {
        self.authorize_subject(peer)?;
        let request = request
            .validate()
            .map_err(|_| HumanOperationError::Refused)?;
        let correlation = boundary_correlation(
            peer,
            &request.protocol_activity_type.to_be_bytes(),
            b"fee-projection",
        );
        let observation = self
            .node
            .estimate_fee(request.meter(), correlation)
            .map_err(fee_estimate_error)?;
        let head = observation.head();
        let projection = layerx_agent_api::read::FeeProjection::from_observation(request, observation)
            .map_err(fee_projection_error)?;
        let batch = layerx_agent_api::read::BatchRef::new(head.sealed_batch.to_string())
            .map_err(|_| HumanOperationError::Refused)?;
        let freshness = layerx_agent_api::read::Freshness {
            chain_head: layerx_agent_api::Sequence(head.chain_sequence),
            latest_sealed_batch: batch.clone(),
            latest_finalised_checkpoint: layerx_agent_api::read::CheckpointRef::new(hex(
                &head.finalised_checkpoint,
            ))
            .map_err(|_| HumanOperationError::Refused)?,
            value_sequence: layerx_agent_api::Sequence(head.chain_sequence),
            relative_to: layerx_agent_api::read::RelativeTo::Batch(batch),
        };
        projection
            .into_projection(FEE_PROJECTION_RATIONALE, freshness)
            .map_err(fee_projection_error)
    }

    fn policy_dry_run(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::policy::PolicyDryRunRequest,
    ) -> Result<HumanResponse, HumanOperationError> {
        let request = request
            .validate()
            .map_err(|_| HumanOperationError::Refused)?;
        let (tenant, actor, _) =
            capability_coordinates(context, request.tenant.as_str(), request.agent_did.as_str())?;
        capability_session_authority(context, control)?;
        let session_id = SessionId(
            request
                .session_id
                .to_bytes()
                .map_err(|_| HumanOperationError::Refused)?,
        );
        let capability_id = CapabilityId(
            request
                .capability_id
                .to_bytes()
                .map_err(|_| HumanOperationError::Refused)?,
        );
        let policy_request = crate::policy::PolicyRequest {
            activity_type: request.activity_type.0,
            counterparty: request.counterparty,
            asset: request.asset,
            amount: request.amount.get(),
            purpose: request.purpose,
            core_sequence: request.core_sequence.get(),
        };
        let capability = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            match Capability::restore(&store, tenant.clone(), capability_id) {
                Ok(Some(capability)) => capability,
                Ok(None) => {
                    return Err(policy_refusal(
                        crate::policy::PolicyDryRunRefusal::UnknownCapability,
                    ))
                }
                Err(_) => {
                    return Err(policy_refusal(
                        crate::policy::PolicyDryRunRefusal::CapabilityStore,
                    ))
                }
            }
        };
        let sessions = control.registry();
        let sessions = sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let session = sessions
            .get(&tenant, session_id)
            .filter(|record| record.request.agent == actor)
            .ok_or_else(|| policy_refusal(
                crate::policy::PolicyDryRunRefusal::UnknownSession,
            ))?;
        let budget = self.verified_policy_budget(context.peer(), &tenant, &actor)?;
        let verified_context = match &budget {
            Some(reconciliation) => crate::policy::VerifiedPolicyContext::ProtocolBudget(reconciliation),
            None => crate::policy::VerifiedPolicyContext::Unavailable,
        };
        let registry = self
            .policies
            .as_mut()
            .ok_or(HumanOperationError::Unavailable)?
            .get_mut(&tenant)
            .ok_or_else(|| policy_refusal(
                crate::policy::PolicyDryRunRefusal::NoPolicyForTenant,
            ))?;
        let request_id = crate::policy::dry_run_request_id(
            &tenant,
            session_id,
            capability_id,
            registry.begin_request().generation(),
            &policy_request,
        );
        let result = crate::policy::dry_run_with_context(
            registry,
            request_id,
            &policy_request,
            session,
            &capability,
            verified_context,
        );
        HumanResponse::new(result.explanation.machine_bytes())
            .map_err(|_| HumanOperationError::Refused)
    }

    fn export_offline(
        &mut self,
        peer: &HumanPeer,
        request: layerx_agent_api::read::ReadRequest<Vec<layerx_agent_api::export::FactRef>>,
    ) -> Result<
        layerx_agent_api::read::VerifiedRead<layerx_agent_api::export::OfflineExport>,
        HumanOperationError,
    > {
        let texts: Vec<&str> = request
            .selector
            .iter()
            .map(layerx_agent_api::export::FactRef::as_str)
            .collect();
        let selectors = layerx_agent_api::export::parse_fact_set(&texts)
            .map_err(|_| HumanOperationError::Refused)?;
        let requested = production_verification_level(request.requested_verification_level);
        let bound_account = if selectors
            .iter()
            .any(|selector| matches!(selector, layerx_agent_api::export::FactSelector::State { .. }))
        {
            Some(self.export_bound_account(peer)?)
        } else {
            None
        };
        let tenant = TenantId::new(peer.tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let (source, availability_deadline) = self
            .export_trust
            .as_ref()
            .ok_or(HumanOperationError::Unavailable)?;
        let trust = source
            .trust(registry.clone())
            .map_err(|_| HumanOperationError::Unavailable)?;
        let node = self.node.handshake().node().clone();
        let authority = EvidenceAuthority::pinned_to_handshake(
            node.protocol_version,
            node.network_id,
            node.authorised_sequencer_key,
        )
        .map_err(|_| HumanOperationError::Refused)?;
        let anchors = texts
            .iter()
            .map(|text| text.as_bytes())
            .collect::<Vec<_>>()
            .join(&b'\n');
        let context = crate::export::ExportOwnerContext {
            store: &self.store,
            tenant: &tenant,
            bound_account,
            registry: &registry,
            authority: &authority,
            trust: &trust,
            first_correlation: boundary_correlation(peer, &anchors, b"export-offline"),
            availability_deadline: *availability_deadline,
        };
        let produced = crate::export::produce(&mut self.node, &context, &request.selector, requested)
            .map_err(export_refusal)?;
        layerx_agent_api::export::check_export_response(&request, &produced.response)
            .map_err(|_| HumanOperationError::Refused)?;
        Ok(produced.response)
    }

    fn budget_create(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetCreate>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        use layerx_agent_api::budget::{BudgetEnforcement, BudgetRecord, ProtocolBudgetView};
        let MutationEnvelope {
            key,
            body_digest,
            operation,
            ..
        } = request;
        let mutation = operation.validate().map_err(budget_contract_refusal)?;
        let create = &mutation.request;
        let (tenant, authority) = budget_authority(
            context,
            control,
            create.tenant.as_str(),
            create.agent_did.as_str(),
        )?;
        let expiry_ms = crate::budget::core_expiry_ms(create.expiry.0).map_err(budget_refusal)?;
        let asset = digest_from_hex(create.asset.as_str())
            .ok_or(HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
        let value = match create.enforcement {
            BudgetEnforcement::DaemonLimit => {
                let limiter = self
                    .budget_limiter
                    .clone()
                    .ok_or(HumanOperationError::Unavailable)?;
                let snapshot = core_preparation_snapshot(
                    &mut self.node,
                    context.peer(),
                    &context.principal().agent,
                )?;
                let now = snapshot.protocol_timestamp;
                let budget_id = daemon_limit_budget_id(&tenant, create.agent_did.as_str(), &key);
                let record = crate::budget::DaemonLimitRecord {
                    tenant: tenant.clone(),
                    budget_id,
                    limit_id: crate::budget::daemon_limit_id(budget_id),
                    agent_digest: daemon_limit_agent(create.agent_did.as_str()),
                    asset,
                    ceiling: create.limit.0,
                    consumed: 0,
                    expiry_ms,
                    revoked: false,
                    mutation_key: key,
                    body_digest,
                    revoke_key: [0; 32],
                };
                let store = &self.store;
                let created = context
                    .commit(control, |_| {
                        let mut store = store
                            .lock()
                            .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                        crate::budget::create_daemon_limit(
                            &mut store,
                            &limiter,
                            record,
                            crate::budget::CoreTimestampMs(now),
                        )
                        .map_err(|error| {
                            crate::session_control::SessionControlError::Human(
                                daemon_limit_refusal(error),
                            )
                        })
                    })
                    .map_err(rpc_commit_error)?;
                crate::human::BudgetState {
                    record: BudgetRecord::Daemon(daemon_limit_view(&created)),
                    balance: created
                        .ceiling
                        .checked_sub(created.consumed)
                        .ok_or(HumanOperationError::Unavailable)?,
                    proven_head: snapshot.observed_head_sequence,
                    activity_id: None,
                }
            }
            BudgetEnforcement::ProtocolBudget => {
                let carrier = mutation.authorization.as_ref().ok_or(HumanOperationError::Typed(
                    crate::human::HumanRefusal::BudgetAuthorizationRequired,
                ))?;
                let peer = context.peer();
                let prepared_key = (
                    peer.tenant.clone(),
                    peer.principal.clone(),
                    carrier.preparation_ref.as_str().to_owned(),
                );
                let cached = self
                    .prepared
                    .get(&prepared_key)
                    .cloned()
                    .ok_or(HumanOperationError::Refused)?;
                let Some(layerx_crypto::disclosure::DisclosedNativeOperation::BudgetCreate(
                    disclosed,
                )) = cached.prepared.disclosure.native_operation.as_ref()
                else {
                    return Err(HumanOperationError::Typed(
                        crate::human::HumanRefusal::BudgetCodec,
                    ));
                };
                let agent = disclosed.budget_id;
                let candidate = {
                    let store = self
                        .store
                        .lock()
                        .map_err(|_| HumanOperationError::Unavailable)?;
                    managed_agent::budget_candidate(&store, &tenant, agent)?
                };
                if candidate.agent_did != create.agent_did.as_str() {
                    return Err(HumanOperationError::Refused);
                }
                let (signature, signer) = owner_signature(&cached.prepared, carrier)?;
                let signed = attach_external_signature(&cached.prepared, signature)
                    .map_err(|_| HumanOperationError::Refused)?;
                let submission =
                    verify_before_submit(&signed, &cached.prepared, &signer, &cached.registry)
                        .map_err(|_| HumanOperationError::Refused)?;
                if submission.idempotency_key() != key {
                    return Err(HumanOperationError::Refused);
                }
                let identity =
                    crate::budget::budget_create_identity(submission.exact_bytes(), &cached.registry)
                        .map_err(budget_refusal)?;
                if identity.expiry_ms != expiry_ms
                    || identity.asset != asset
                    || identity.per_period_limit != create.limit.0
                {
                    return Err(HumanOperationError::Typed(
                        crate::human::HumanRefusal::BudgetContextMismatch,
                    ));
                }
                let (preparation_id, head) = self.admit_budget_submit(
                    context,
                    control,
                    &tenant,
                    &cached,
                    carrier.preparation_ref.as_str(),
                    &submission,
                )?;
                let ceiling = create.limit.0;
                let outcome = context
                    .commit(control, |peer| {
                        self.confirm_protocol_budget(
                            peer,
                            &tenant,
                            &prepared_key,
                            signature,
                            signer,
                            agent,
                            asset,
                            ceiling,
                            expiry_ms,
                        )
                        .map_err(crate::session_control::SessionControlError::Human)
                    })
                    .map_err(rpc_commit_error)?;
                let (release, sequence) = match &outcome {
                    Ok(budget) => (
                        crate::budget::ReleaseKind::Executed,
                        budget.observed_head_sequence(),
                    ),
                    Err(BudgetCreationError::CoreRejected) => {
                        (crate::budget::ReleaseKind::Failed, head)
                    }
                    Err(_) => (crate::budget::ReleaseKind::Unknown, head),
                };
                control
                    .settle_write(&tenant, preparation_id, release, sequence)
                    .map_err(rpc_commit_error)?;
                let budget = outcome.map_err(budget_refusal)?;
                let created = budget.record();
                let peer = context.peer();
                let registry = self.authority.registry(peer).map_err(map_core)?;
                let (_, sequencer_key, authorization) = self.budget_read_parts(peer)?;
                let mut pipeline = NodeBudgetPipeline {
                    node: &mut self.node,
                    registry: &registry,
                    signer,
                    correlation: boundary_correlation(
                        peer,
                        create.agent_did.as_str().as_bytes(),
                        b"budget-create-balance",
                    ),
                    authorization,
                    sequencer_key,
                    receipt_poll: BUDGET_RECEIPT_POLL,
                    receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
                };
                let balance = crate::budget::BudgetMutationPipeline::budget_balance(
                    &mut pipeline,
                    created.budget_account,
                    created.asset_id,
                )
                .map_err(budget_refusal)?;
                crate::human::BudgetState {
                    record: BudgetRecord::Protocol(
                        ProtocolBudgetView::from_proven(
                            created,
                            layerx_agent_api::verify::Level::StateProven,
                        )
                        .map_err(budget_contract_refusal)?,
                    ),
                    balance,
                    proven_head: budget.observed_head_sequence(),
                    activity_id: Some(submission.activity_id()),
                }
            }
        };
        Ok(layerx_agent_api::budget::AuthorityResponse { authority, value })
    }

    fn budget_fund(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetFund>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        let MutationEnvelope { key, operation, .. } = request;
        let mutation = operation.validate().map_err(budget_contract_refusal)?;
        let fund = &mutation.request;
        let (tenant, authority) = budget_authority(
            context,
            control,
            fund.tenant.as_str(),
            fund.agent_did.as_str(),
        )?;
        let budget_id = digest_from_hex(fund.budget_id.as_str())
            .ok_or(HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
        self.require_budget_owner(&tenant, fund.agent_did.as_str(), budget_id)?;
        let carrier = mutation.authorization.as_ref().ok_or(HumanOperationError::Typed(
            crate::human::HumanRefusal::BudgetAuthorizationRequired,
        ))?;
        let amount = fund.amount.0;

        let fund_signer = {
            let peer = context.peer();
            let cached = self
                .prepared
                .get(&(
                    peer.tenant.clone(),
                    peer.principal.clone(),
                    carrier.preparation_ref.as_str().to_owned(),
                ))
                .ok_or(HumanOperationError::Refused)?;
            owner_signature(&cached.prepared, carrier)?.1
        };
        let confirmed = self.owned_budget_mutation(context, control, &tenant, carrier, key, |m| {
            matches!(
                m,
                crate::budget::BudgetMutation::Fund { budget_id: id, amount: value, .. }
                    if *id == budget_id && *value == amount
            )
        })?;
        let funded = confirmed.record();
        let peer = context.peer();
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let (_, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &registry,
            signer: fund_signer,
            correlation: boundary_correlation(
                peer,
                fund.agent_did.as_str().as_bytes(),
                b"budget-fund-balance",
            ),
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        let balance = crate::budget::BudgetMutationPipeline::budget_balance(
            &mut pipeline,
            funded.budget_account,
            funded.asset_id,
        )
        .map_err(budget_refusal)?;
        if balance < funded.per_period_limit {
            return Err(HumanOperationError::Typed(
                crate::human::HumanRefusal::BudgetLimitExceeded,
            ));
        }

        let view = layerx_agent_api::budget::ProtocolBudgetView::from_proven(
            confirmed.record(),
            layerx_agent_api::verify::Level::StateProven,
        )
        .map_err(budget_contract_refusal)?;
        Ok(layerx_agent_api::budget::AuthorityResponse {
            authority,
            value: crate::human::BudgetState {
                record: layerx_agent_api::budget::BudgetRecord::Protocol(view),
                balance,
                proven_head: confirmed.observed_head_sequence(),
                activity_id: Some(confirmed.activity_id()),
            },
        })
    }

    fn budget_revoke(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: MutationEnvelope<
            layerx_agent_api::budget::SignedBudgetMutation<layerx_agent_api::budget::BudgetTarget>,
        >,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        use layerx_agent_api::budget::{BudgetEnforcement, BudgetRecord, ProtocolBudgetView};
        let MutationEnvelope {
            key,
            operation: mutation,
            ..
        } = request;
        let target = &mutation.request;
        let (tenant, authority) = budget_authority(
            context,
            control,
            target.tenant.as_str(),
            target.agent_did.as_str(),
        )?;
        let budget_id = digest_from_hex(target.budget_id.as_str())
            .ok_or(HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
        if let Some(record) = self.daemon_limit_record(&tenant, budget_id)? {
            mutation
                .require_for(BudgetEnforcement::DaemonLimit)
                .map_err(budget_contract_refusal)?;
            if record.agent_digest != daemon_limit_agent(target.agent_did.as_str()) {
                return Err(HumanOperationError::Refused);
            }
            let limiter = self
                .budget_limiter
                .clone()
                .ok_or(HumanOperationError::Unavailable)?;
            let snapshot = core_preparation_snapshot(
                &mut self.node,
                context.peer(),
                &context.principal().agent,
            )?;
            let store = &self.store;
            let revoked = context
                .commit(control, |_| {
                    let mut store = store
                        .lock()
                        .map_err(|_| crate::session_control::SessionControlError::Unavailable)?;
                    crate::budget::revoke_daemon_limit(&mut store, &limiter, &tenant, budget_id, key)
                        .map_err(|error| {
                            crate::session_control::SessionControlError::Human(
                                daemon_limit_refusal(error),
                            )
                        })
                })
                .map_err(rpc_commit_error)?;
            return Ok(layerx_agent_api::budget::AuthorityResponse {
                authority,
                value: crate::human::BudgetState {
                    record: BudgetRecord::Daemon(daemon_limit_view(&revoked)),
                    balance: revoked
                        .ceiling
                        .checked_sub(revoked.consumed)
                        .ok_or(HumanOperationError::Unavailable)?,
                    proven_head: snapshot.observed_head_sequence,
                    activity_id: None,
                },
            });
        }
        mutation
            .require_for(BudgetEnforcement::ProtocolBudget)
            .map_err(budget_contract_refusal)?;
        self.require_budget_owner(&tenant, target.agent_did.as_str(), budget_id)?;
        let carrier = mutation.authorization.as_ref().ok_or(HumanOperationError::Typed(
            crate::human::HumanRefusal::BudgetAuthorizationRequired,
        ))?;
        let revoke_signer = {
            let peer = context.peer();
            let cached = self
                .prepared
                .get(&(
                    peer.tenant.clone(),
                    peer.principal.clone(),
                    carrier.preparation_ref.as_str().to_owned(),
                ))
                .ok_or(HumanOperationError::Refused)?;
            owner_signature(&cached.prepared, carrier)?.1
        };
        let confirmed = self.owned_budget_mutation(context, control, &tenant, carrier, key, |m| {
            matches!(
                m,
                crate::budget::BudgetMutation::Revoke { budget_id: id, .. } if *id == budget_id
            )
        })?;
        if let Some(limiter) = self.budget_limiter.clone() {
            let mut store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            match crate::budget::revoke_daemon_limit(&mut store, &limiter, &tenant, budget_id, key) {
                Ok(_) | Err(crate::budget::DaemonLimitError::Unknown) => {}
                Err(error) => return Err(daemon_limit_refusal(error)),
            }
        }
        self.prepared.retain(|_, cached| {
            prepared_budget_context(&cached.prepared)
                .map_or(true, |context| context.budget_id != budget_id)
        });
        let revoked = confirmed.record();
        let peer = context.peer();
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let (_, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &registry,
            signer: revoke_signer,
            correlation: boundary_correlation(
                peer,
                target.agent_did.as_str().as_bytes(),
                b"budget-revoke-balance",
            ),
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        let balance = crate::budget::BudgetMutationPipeline::budget_balance(
            &mut pipeline,
            revoked.budget_account,
            revoked.asset_id,
        )
        .map_err(budget_refusal)?;
        let view =
            ProtocolBudgetView::from_proven(revoked, layerx_agent_api::verify::Level::StateProven)
                .map_err(budget_contract_refusal)?;
        Ok(layerx_agent_api::budget::AuthorityResponse {
            authority,
            value: crate::human::BudgetState {
                record: BudgetRecord::Protocol(view),
                balance,
                proven_head: confirmed.observed_head_sequence(),
                activity_id: Some(confirmed.activity_id()),
            },
        })
    }

    fn budget_state(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::budget::BudgetTarget,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<crate::human::BudgetState>,
        HumanOperationError,
    > {
        use layerx_agent_api::budget::{BudgetRecord, ProtocolBudgetView};
        let (tenant, authority) = budget_authority(
            context,
            control,
            request.tenant.as_str(),
            request.agent_did.as_str(),
        )?;
        let budget_id = digest_from_hex(request.budget_id.as_str()).ok_or(
            HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec),
        )?;
        let peer = context.peer();
        if let Some(record) = self.daemon_limit_record(&tenant, budget_id)? {
            if record.agent_digest != daemon_limit_agent(request.agent_did.as_str()) {
                return Err(HumanOperationError::Refused);
            }
            if record.revoked {
                return Err(daemon_limit_refusal(
                    crate::budget::DaemonLimitError::Revoked,
                ));
            }
            let balance = record
                .ceiling
                .checked_sub(record.consumed)
                .ok_or(HumanOperationError::Unavailable)?;
            let proven_head =
                core_preparation_snapshot(&mut self.node, peer, &context.principal().agent)?
                    .observed_head_sequence;
            return Ok(layerx_agent_api::budget::AuthorityResponse {
                authority,
                value: crate::human::BudgetState {
                    record: BudgetRecord::Daemon(daemon_limit_view(&record)),
                    balance,
                    proven_head,
                    activity_id: None,
                },
            });
        }
        self.require_budget_owner(&tenant, request.agent_did.as_str(), budget_id)
            .map_err(|error| match error {
                HumanOperationError::Refused => {
                    HumanOperationError::Typed(crate::human::HumanRefusal::BudgetNotFound)
                }
                other => other,
            })?;
        let registry = self.authority.registry(peer).map_err(map_core)?;
        let (_, signer) =
            capability_authority_parts(&capability_session_authority(context, control)?);
        let head = core_preparation_snapshot(&mut self.node, peer, &context.principal().agent)?
            .observed_head_sequence;
        let (verifier, sequencer_key, authorization) = self.budget_read_parts(peer)?;
        let mut pipeline = NodeBudgetPipeline {
            node: &mut self.node,
            registry: &registry,
            signer,
            correlation: boundary_correlation(
                peer,
                request.agent_did.as_str().as_bytes(),
                b"budget-state",
            ),
            authorization,
            sequencer_key,
            receipt_poll: BUDGET_RECEIPT_POLL,
            receipt_attempts: BUDGET_RECEIPT_ATTEMPTS,
        };
        let state = crate::budget::BudgetMutationPipeline::budget_state(&mut pipeline, budget_id)
            .map_err(budget_refusal)?;
        let proven = verifier
            .verify_state(&state.evidence)
            .map_err(|_| HumanOperationError::Refused)?;
        let record = ProtocolBudgetRecord::decode(proven.canonical_state())
            .map_err(|_| HumanOperationError::Refused)?;
        if record.budget_id != budget_id {
            return Err(HumanOperationError::Refused);
        }
        let balance = crate::budget::BudgetMutationPipeline::budget_balance(
            &mut pipeline,
            record.budget_account,
            record.asset_id,
        )
        .map_err(budget_refusal)?;
        let proven_context =
            crate::budget::budget_state_context(&record, proven.canonical_state(), head, balance)
                .map_err(budget_refusal)?;
        let view =
            ProtocolBudgetView::from_proven(&record, layerx_agent_api::verify::Level::StateProven)
                .map_err(budget_contract_refusal)?;
        Ok(layerx_agent_api::budget::AuthorityResponse {
            authority,
            value: crate::human::BudgetState {
                record: BudgetRecord::Protocol(view),
                balance: proven_context.balance,
                proven_head: proven_context.observed_head_sequence,
                activity_id: None,
            },
        })
    }

    fn policy_dry_run_legacy(
        &mut self,
        context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
        control: &crate::session_control::SessionControl,
        request: layerx_agent_api::identity::LegacyPolicyDryRun,
    ) -> Result<
        layerx_agent_api::budget::AuthorityResponse<layerx_agent_api::policy::PolicyDryRunResult>,
        HumanOperationError,
    > {
        use crate::human::PolicyContextField;
        let layerx_agent_api::identity::LegacyPolicyDryRun {
            context: asserted,
            canonical_intent,
        } = request;
        let authority = capability_session_authority(context, control)?;
        let credential = context.permit().credential();
        let principal = context.principal();
        if credential.tenant() != &principal.tenant
            || credential.session_id() != principal.session_id
        {
            return Err(HumanOperationError::Refused);
        }
        let principal_agent = std::str::from_utf8(principal.agent.as_bytes())
            .map_err(|_| HumanOperationError::Refused)?;
        if asserted.tenant.as_str() != principal.tenant.as_str() {
            return Err(policy_context_mismatch(PolicyContextField::Tenant));
        }
        if asserted.agent_did.as_str() != principal_agent {
            return Err(policy_context_mismatch(PolicyContextField::AgentDid));
        }
        let (tenant, actor, agent) = capability_coordinates(
            context,
            asserted.tenant.as_str(),
            asserted.agent_did.as_str(),
        )?;
        let (_, authority_id) = capability_authority_parts(&authority);
        if asserted.authority_ref.as_str() != crate::agent_rpc_dispatch::lower_hex(&authority_id) {
            return Err(policy_context_mismatch(PolicyContextField::AuthorityRef));
        }
        let snapshot = core_preparation_snapshot(&mut self.node, context.peer(), &actor)?;
        let binding = control
            .capability_binding(&tenant, credential.session_id())
            .map_err(legacy_binding_error)?
            .ok_or(HumanOperationError::Typed(
                crate::human::HumanRefusal::SessionBindingMissing,
            ))?;
        if binding.tenant != tenant
            || binding.session_id != credential.session_id()
            || binding.generation != credential.generation()
        {
            return Err(HumanOperationError::Refused);
        }
        if let ProtocolAuthority::CapabilityGrant(grant) = &authority {
            if *grant != binding.capability_id {
                return Err(HumanOperationError::Refused);
            }
        }
        let capability = {
            let store = self
                .store
                .lock()
                .map_err(|_| HumanOperationError::Unavailable)?;
            let capability =
                crate::capability::timed::restore(&store, &tenant, &binding.capability_id)
                    .map_err(|error| error.owner_error())?
                    .ok_or_else(|| {
                        policy_refusal(crate::policy::PolicyDryRunRefusal::UnknownCapability)
                    })?;
            if capability.tenant != tenant
                || capability.agent != agent
                || capability.authority != authority
                || capability.revoked.is_some()
            {
                return Err(HumanOperationError::Refused);
            }
            crate::capability::timed::require_active_chain(
                &store,
                &capability,
                snapshot.protocol_timestamp,
            )
            .map_err(|error| error.owner_error())?;
            capability
        };
        let view = crate::policy::CapabilityView::try_from(&capability)
            .map_err(|_| HumanOperationError::Refused)?;
        let (label, activity) =
            match layerx_agent_api::identity::IntentCarrier::classify(&canonical_intent).map_err(
                |_| HumanOperationError::Typed(crate::human::HumanRefusal::IntentMalformed),
            )? {
                layerx_agent_api::identity::IntentCarrier::Explicit(intent) => (
                    Some(intent.purpose().to_owned()),
                    intent.canonical_activity().to_vec(),
                ),
                layerx_agent_api::identity::IntentCarrier::Raw(bytes) => (None, bytes.to_vec()),
            };
        let modules = self.authority.registry(context.peer()).map_err(map_core)?;
        let disclosure = layerx_crypto::disclosure::bind(&activity, &modules)
            .map_err(|_| HumanOperationError::Typed(crate::human::HumanRefusal::IntentMalformed))?;
        if disclosure.actor.as_slice() != actor.as_bytes() {
            return Err(HumanOperationError::Refused);
        }
        let raw = label.is_none();
        let purpose = legacy_policy_purpose(&disclosure, label)?;
        let effects = legacy_policy_effects(&disclosure, raw)?;
        let budget = self.verified_policy_budget(context.peer(), &tenant, &actor)?;
        let verified_context = match &budget {
            Some(reconciliation) => {
                crate::policy::VerifiedPolicyContext::ProtocolBudget(reconciliation)
            }
            None => crate::policy::VerifiedPolicyContext::Unavailable,
        };
        let sessions = control.registry();
        let sessions = sessions
            .read()
            .map_err(|_| HumanOperationError::Unavailable)?;
        let session = sessions
            .get(credential.tenant(), credential.session_id())
            .filter(|record| {
                record.open
                    && record.generation == credential.generation()
                    && record.request.agent == actor
                    && record.request.authority == authority
            })
            .ok_or_else(|| policy_refusal(crate::policy::PolicyDryRunRefusal::UnknownSession))?;
        let asserted_values = asserted.permitted_activity_types.values();
        let asserted_types = asserted_values
            .iter()
            .map(|activity_type| activity_type.0)
            .collect::<std::collections::BTreeSet<u16>>();
        if asserted_values.is_empty()
            || asserted_types.len() != asserted_values.len()
            || session.request.permitted_activity_types.is_empty()
            || asserted_types != session.request.permitted_activity_types
        {
            return Err(policy_context_mismatch(
                PolicyContextField::PermittedActivityTypes,
            ));
        }
        let Some(expiry_seconds) = session.request.expiry_seconds else {
            return Err(HumanOperationError::Typed(
                crate::human::HumanRefusal::SessionExpiryBindingMissing,
            ));
        };
        if asserted.expiry.get() != expiry_seconds {
            return Err(policy_context_mismatch(PolicyContextField::Expiry));
        }
        if !session
            .public_expiry_within(snapshot.protocol_timestamp)
            .map_err(|_| HumanOperationError::Refused)?
            || session.request.expiry_sequence <= snapshot.observed_head_sequence
        {
            return Err(HumanOperationError::Typed(
                crate::human::HumanRefusal::SessionExpired,
            ));
        }
        if asserted.client.as_str() != session.request.opening_client {
            return Err(policy_context_mismatch(PolicyContextField::Client));
        }
        if asserted.policy_version.as_str() != session.request.policy_version {
            return Err(policy_context_mismatch(PolicyContextField::PolicyVersion));
        }
        let intent = crate::policy::PolicyIntentRequest {
            effects,
            purpose,
            core_sequence: snapshot.observed_head_sequence,
        };
        let registry = self
            .policies
            .as_mut()
            .ok_or(HumanOperationError::Unavailable)?
            .get_mut(&tenant)
            .ok_or_else(|| policy_refusal(crate::policy::PolicyDryRunRefusal::NoPolicyForTenant))?;
        let request_id = crate::policy::dry_run_intent_request_id(
            &tenant,
            credential.session_id(),
            CapabilityId(binding.capability_id),
            registry.begin_request().generation(),
            &intent,
        );
        let result = crate::policy::dry_run_intent_with_context(
            registry,
            request_id,
            &intent,
            session,
            view,
            verified_context,
        );
        drop(sessions);
        let value = legacy_policy_result(result.explanation)?;
        let (_, description) = budget_authority(context, control, tenant.as_str(), &agent)?;
        Ok(layerx_agent_api::budget::AuthorityResponse {
            authority: description,
            value,
        })
    }
}

/// Production budget pipeline over the sole frozen node client: the exact
/// verified bytes are submitted through `submit_signed`, the receipt is fetched
/// as a raw proof bundle, and the created record is read as proof-gated module
/// state so the caller can verify both against the pinned handshake authority.
struct NodeBudgetPipeline<'a> {
    node: &'a mut Client,
    registry: &'a ModuleRegistry,
    signer: [u8; 32],
    correlation: u64,
    authorization: SequencerAuthorization,
    sequencer_key: [u8; 32],
    receipt_poll: Duration,
    receipt_attempts: u32,
}

impl BudgetPipeline for NodeBudgetPipeline<'_> {
    fn submit_budget(
        &mut self,
        request: &BudgetRequest,
    ) -> Result<CoreBudgetReceipt, BudgetCreationError> {
        let submission = request
            .verified_submission
            .as_ref()
            .ok_or(BudgetCreationError::ActivityBindingUnavailable)?;
        crate::budget::BudgetMutationPipeline::submit_verified(self, submission)
    }

    fn budget_state(
        &mut self,
        budget_id: [u8; 32],
    ) -> Result<ProtocolBudgetState, BudgetCreationError> {
        let key = budget_state_key(budget_id);
        let value = self
            .node
            .module_state(
                BUDGET_MODULE_ID,
                &key,
                VerificationLevel::STATE_PROVEN,
                self.correlation.wrapping_add(2).max(1),
                self.authorization,
            )
            .map_err(|_| BudgetCreationError::CreatedBudgetUnconfirmed)?;
        Ok(ProtocolBudgetState {
            evidence: RawStateEvidence::module_witness(
                value.canonical_bytes().to_vec(),
                BUDGET_MODULE_ID,
                key,
                value.proof_material().to_vec(),
                RootSelector::Latest,
                self.sequencer_key,
            ),
        })
    }
}

/// Returns whether proven core state currently holds a live (not closed, not
/// revoked) budget record named `budget_id`. Any missing, unverifiable or
/// mismatched state is reported as not live.
fn live_protocol_budget(
    pipeline: &mut dyn BudgetPipeline,
    verifier: &EvidenceAuthority,
    budget_id: [u8; 32],
) -> bool {
    let Ok(state) = pipeline.budget_state(budget_id) else {
        return false;
    };
    let Ok(proven) = verifier.verify_state(&state.evidence) else {
        return false;
    };
    let Ok(record) = ProtocolBudgetRecord::decode(proven.canonical_state()) else {
        return false;
    };
    record.budget_id == budget_id && !record.closed && !record.revoked
}

fn raw_receipt_evidence(
    bundle: &layerx_client::evidence::VerifiedProofBundle,
) -> Result<crate::protocol_evidence::RawReceiptEvidence, HumanOperationError> {
    let layerx_client::evidence::VerifiedProofBundle::Receipt {
        canonical_bytes,
        proof,
        signed_header,
        ..
    } = bundle
    else {
        return Err(HumanOperationError::Refused);
    };
    Ok(crate::protocol_evidence::RawReceiptEvidence::new(
        canonical_bytes.clone(),
        proof.clone(),
        signed_header.canonical_bytes.clone(),
        signed_header.signature,
    ))
}

fn evidence_unavailable(error: &EvidenceError) -> bool {
    match error {
        EvidenceError::Unavailable | EvidenceError::Transport(_) => true,
        EvidenceError::CoreRefusal { result, .. } => {
            result.retriability() == Retriability::Retriable
        }
        _ => false,
    }
}

fn owner_authority(request: &HumanOwnerInstall) -> Result<ProtocolAuthority, HumanOperationError> {
    match request.authority_kind {
        1 => Ok(ProtocolAuthority::PrimaryKey(request.authority_id)),
        2 => Ok(ProtocolAuthority::SessionKey(request.authority_id)),
        3 => Ok(ProtocolAuthority::CapabilityGrant(request.authority_id)),
        _ => Err(HumanOperationError::Refused),
    }
}
fn encode_identity(identity: &CoreIdentity) -> Result<HumanResponse, HumanOperationError> {
    let mut out = Encoder::new();
    out.u64(identity.head_sequence);
    out.u64(identity.revocation_sequence);
    out.u8(verification_code(identity.verification_level));
    out.u8(u8::from(identity.frozen));
    out.u16(identity.authorities.len())?;
    for authority in &identity.authorities {
        let (kind, id) = match authority {
            ProtocolAuthority::PrimaryKey(id) => (1, id),
            ProtocolAuthority::SessionKey(id) => (2, id),
            ProtocolAuthority::CapabilityGrant(id) => (3, id),
        };
        out.u8(kind);
        out.fixed(id);
    }
    out.bytes(&identity.canonical_bytes)?;
    out.finish()
}
fn encode_owner_validation(
    validated: &(CoreIdentity, u64, u64),
) -> Result<HumanResponse, HumanOperationError> {
    let mut out = Encoder::new();
    out.u64(validated.0.head_sequence);
    out.u64(validated.1);
    out.u64(validated.2);
    out.bytes(&validated.0.canonical_bytes)?;
    out.finish()
}
fn owner_digest(request: &HumanOwnerInstall) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-human-owner-install/v2");
    hash_text(&mut digest, request.agent.as_bytes());
    digest.update([request.authority_kind]);
    digest.update(request.authority_id);
    digest.update(request.session_id);
    digest.update(request.token_id);
    digest.update(request.session_public_key);
    hash_text(&mut digest, &request.registration_payload);
    digest.update(request.grantor);
    digest.update(request.grant_not_before.to_be_bytes());
    digest.update(request.grant_expires_at.to_be_bytes());
    digest.update(request.grant_revocation_sequence.to_be_bytes());
    count(&mut digest, request.permitted_activity_types.len());
    for x in &request.permitted_activity_types {
        digest.update(x.to_be_bytes());
    }
    count(&mut digest, request.scopes.len());
    for x in &request.scopes {
        hash_text(&mut digest, x.as_bytes());
    }
    digest.update(request.lease_not_before_unix_ms.to_be_bytes());
    digest.update(request.lease_not_after_unix_ms.to_be_bytes());
    hash_text(&mut digest, request.opening_client.as_bytes());
    hash_text(&mut digest, request.policy_version.as_bytes());
    match &request.lifecycle {
        None => digest.update([0]),
        Some(v) => {
            digest.update([1]);
            hash_text(&mut digest, v.agent_id.as_bytes());
            hash_text(&mut digest, v.name.as_bytes());
            hash_text(&mut digest, v.purpose.as_bytes());
            hash_text(&mut digest, v.currency.as_bytes());
            digest.update(v.monthly_limit.to_be_bytes());
            digest.update(v.period_start.to_be_bytes());
            digest.update(v.period_end.to_be_bytes());
            digest.update(v.created_at.to_be_bytes());
            digest.update(v.updated_at.to_be_bytes());
            count(&mut digest, v.verified_evidence.len());
            for x in &v.verified_evidence {
                digest.update(x);
            }
            hash_text(&mut digest, v.actor.as_bytes());
            hash_text(&mut digest, v.primary_authority.as_bytes());
            hash_text(&mut digest, v.custody_key.as_bytes());
            digest.update(v.custody_public_key);
            hash_text(&mut digest, v.owner_account.as_bytes());
            hash_text(&mut digest, v.budget_account.as_bytes());
            digest.update(v.budget_asset);
            digest.update(v.purpose_hash);
            digest.update(v.recovery_root);
            digest.update(v.recovery_threshold.to_be_bytes());
            digest.update(v.capability_id);
            count(&mut digest, v.activity_types.len());
            for x in &v.activity_types {
                digest.update(x.to_be_bytes());
            }
            count(&mut digest, v.counterparties.len());
            for x in &v.counterparties {
                digest.update(x);
            }
            count(&mut digest, v.assets.len());
            for x in &v.assets {
                digest.update(x);
            }
            digest.update(v.amount_ceiling.to_be_bytes());
            digest.update(v.rate_maximum_uses.to_be_bytes());
            digest.update(v.rate_window_sequences.to_be_bytes());
            count(&mut digest, v.purposes.len());
            for x in &v.purposes {
                hash_text(&mut digest, x.as_bytes());
            }
            digest.update(v.capability_expiry_sequence.to_be_bytes());
            count(&mut digest, v.session_scopes.len());
            for x in &v.session_scopes {
                hash_text(&mut digest, x.as_bytes());
            }
            digest.update(v.session_expiry_unix_seconds.to_be_bytes());
            digest.update(v.protocol_grant_id);
            digest.update(v.budget_period_seconds.to_be_bytes());
            digest.update(v.budget_expiry_seconds.to_be_bytes());
            digest.update(v.initial_funding.to_be_bytes());
            digest.update(v.network_id.to_be_bytes());
            count(&mut digest, v.creation_receipt_roots.len());
            for x in &v.creation_receipt_roots {
                digest.update(x);
            }
        }
    }
    digest.finalize().into()
}
fn count(digest: &mut Sha256, value: usize) {
    digest.update(u16::try_from(value).unwrap_or(u16::MAX).to_be_bytes());
}
fn map_identity(error: HumanOperationError) -> IdentityError {
    match error {
        HumanOperationError::Unavailable => IdentityError::BoundaryUnavailable,
        HumanOperationError::Refused
        | HumanOperationError::CapabilityRefused(_)
        | HumanOperationError::Typed(_) => {
            IdentityError::Unverified
        }
    }
}
fn map_identity_operation(error: &IdentityError) -> HumanOperationError {
    match error {
        IdentityError::BoundaryUnavailable => HumanOperationError::Unavailable,
        _ => HumanOperationError::Refused,
    }
}
fn decode_hex(value: &str) -> Option<Vec<u8>> {
    if value.is_empty()
        || !value.len().is_multiple_of(2)
        || value.len() > MAX_RESPONSE.saturating_mul(2)
    {
        return None;
    }
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).ok())
        .collect()
}
fn verification_level(value: &Value) -> Result<VerificationLevel, IdentityError> {
    match value.get("verification_level").and_then(Value::as_str) {
        Some("sequencer_signed") => Ok(VerificationLevel::SEQUENCER_SIGNED),
        Some("batch_included") => Ok(VerificationLevel::BATCH_INCLUDED),
        Some("state_proven") => Ok(VerificationLevel::STATE_PROVEN),
        Some("checkpoint_finalised") => Ok(VerificationLevel::CHECKPOINT_FINALISED),
        Some("settlement_anchored") => Ok(VerificationLevel::SETTLEMENT_ANCHORED),
        _ => Err(IdentityError::Unverified),
    }
}

fn encode_approval(out: &mut Encoder, record: &ApprovalRecord) -> Result<(), HumanOperationError> {
    out.fixed(&record.approval_id);
    encode_disclosure(out, &record.held_activity)?;
    out.fixed(&record.canonical_bytes_digest);
    out.text(record.hold_reason.code)?;
    out.text(record.hold_reason.message)?;
    out.u64(record.created_at_sequence);
    out.u64(record.expires_at_sequence);
    out.u8(match record.state {
        ApprovalState::AwaitingApproval => 0,
        ApprovalState::Approved => 1,
        ApprovalState::Rejected => 2,
        ApprovalState::Expired => 3,
        ApprovalState::Defective => 4,
    });
    if record.state == ApprovalState::Approved {
        out.fixed(
            &record
                .submission_ref
                .ok_or(HumanOperationError::Unavailable)?,
        );
    }
    Ok(())
}
fn encode_disclosure(
    out: &mut Encoder,
    value: &layerx_agent_api::prepare::Disclosure,
) -> Result<(), HumanOperationError> {
    out.fixed(&value.canonical_digest);
    out.u16(usize::from(value.activity_type.0))?;
    out.text(value.actor.as_str())?;
    out.text(value.authority.as_str())?;
    out.u16(value.counterparties.values().len())?;
    for item in value.counterparties.values() {
        out.text(item.as_str())?;
    }
    out.u16(value.amounts.values().len())?;
    for item in value.amounts.values() {
        out.text(item.counterparty.as_str())?;
        out.u128(item.amount.0);
    }
    out.text(value.asset.as_str())?;
    out.u128(value.fee_limit.0);
    out.u64(value.expiry.0);
    out.text(value.idempotency_key.as_str())?;
    Ok(())
}
fn encode_decision(decision: &ApprovalDecision) -> Result<HumanResponse, HumanOperationError> {
    let mut out = Encoder::new();
    out.u8(outcome_code(decision.outcome));
    match decision.submission_ref {
        Some(value) => {
            out.u8(1);
            out.fixed(&value);
        }
        None => out.u8(0),
    }
    match decision.winning_outcome {
        Some(value) => {
            out.u8(1);
            out.u8(outcome_code(value));
        }
        None => out.u8(0),
    }
    out.finish()
}
fn outcome_code(value: ApprovalOutcome) -> u8 {
    match value {
        ApprovalOutcome::Granted => 0,
        ApprovalOutcome::Rejected => 1,
        ApprovalOutcome::Expired => 2,
        ApprovalOutcome::Defective => 3,
        ApprovalOutcome::AlreadyDecided => 4,
        ApprovalOutcome::Conflict => 5,
    }
}
fn map_core(error: CoreStateError) -> HumanOperationError {
    match error {
        CoreStateError::Unavailable => HumanOperationError::Unavailable,
        CoreStateError::Unverified | CoreStateError::Refused { .. } => HumanOperationError::Refused,
    }
}
fn core_preparation_snapshot(
    node: &mut Client,
    peer: &HumanPeer,
    actor: &Did,
) -> Result<crate::prepare::CorePreparationState, HumanOperationError> {
    let correlation = boundary_correlation(peer, actor.as_bytes(), b"core-batch-time");
    let mut boundary =
        ProductionCorePreparationBoundary::new(node, correlation).map_err(map_core)?;
    crate::prepare::CorePreparationBoundary::preparation_state(&mut boundary, actor)
        .map_err(map_core)
}

fn boundary_correlation(peer: &HumanPeer, actor: &[u8], purpose: &[u8]) -> u64 {
    let mut digest = Sha256::new();
    digest.update(b"layerx-agentd/human-lni-correlation/v1\0");
    hash_text(&mut digest, peer.tenant.as_bytes());
    hash_text(&mut digest, peer.principal.as_bytes());
    hash_text(&mut digest, actor);
    hash_text(&mut digest, purpose);
    let bytes = digest.finalize();
    let mut correlation = [0_u8; 8];
    correlation.copy_from_slice(&bytes[..8]);
    let value = u64::from_be_bytes(correlation);
    if value == 0 {
        1
    } else {
        value
    }
}
/// Audits one operator command through the admin surface and executes the
/// daemon-local inspection it names against the tenant's restored outbox.
///
/// The response carries the selected action plan code, the inspected unknown
/// submission when the command was `InspectUnknown`, and the audit entry count.
///
/// # Errors
///
/// Returns `Refused` for an invalid operator, a protected mutation, or a target
/// that is not in the state the command requires, and `Unavailable` when the
/// tenant audit log cannot be opened or appended.
pub fn route_operator_command(
    store_root: &Path,
    tenant: &TenantId,
    outbox: &Outbox,
    operator_id: &str,
    request_id: [u8; 32],
    command: OperatorCommand,
) -> Result<HumanResponse, HumanOperationError> {
    let context =
        OperatorContext::new(operator_id, request_id).map_err(HumanOperationError::from)?;
    let mut surface = Surface::open(store_root, tenant).map_err(HumanOperationError::from)?;
    let mut out = Encoder::new();
    match command {
        OperatorCommand::InspectUnknown(submission_id) => {
            let status = surface
                .inspect_unknown(&context, outbox, submission_id)
                .map_err(HumanOperationError::from)?;
            out.u8(plan_code(ActionPlan::InspectOnly));
            out.fixed(&status.activity_id);
            out.text(&hex(&status.submission_id))?;
            out.u8(state_code(status.state));
        }
        other => {
            let plan = surface
                .dispatch(&context, other)
                .map_err(HumanOperationError::from)?;
            out.u8(plan_code(plan));
        }
    }
    out.u64(surface.audit_entries());
    out.finish()
}

const fn plan_code(plan: ActionPlan) -> u8 {
    match plan {
        ActionPlan::InspectOnly => 1,
        ActionPlan::ReceiptLookupAndExactResend => 2,
        ActionPlan::ResumeDaemonLocalSubscription => 3,
        ActionPlan::ReconcileFromVerifiedCoreEvidence => 4,
        ActionPlan::RetryEvidenceVerification => 5,
        ActionPlan::OrdinaryClientWrite(_) => 6,
    }
}

impl From<AdminError> for HumanOperationError {
    fn from(error: AdminError) -> Self {
        match error {
            AdminError::Audit(_) => Self::Unavailable,
            AdminError::InvalidOperator
            | AdminError::ProtectedMutation(_)
            | AdminError::NotUnknown
            | AdminError::NotStalled
            | AdminError::NotBacklogged
            | AdminError::UnknownResolution(_)
            | AdminError::Subscription(_)
            | AdminError::BudgetReconciliation(_)
            | AdminError::Verification(_)
            | AdminError::RouteInvariant
            | AdminError::Arithmetic => Self::Refused,
        }
    }
}

fn state_code(state: SubmissionState) -> u8 {
    match state {
        SubmissionState::Prepared => 0,
        SubmissionState::Signed => 1,
        SubmissionState::Queued => 2,
        SubmissionState::Submitted => 3,
        SubmissionState::Acknowledged => 4,
        SubmissionState::Unknown => 5,
        SubmissionState::Executed => 6,
        SubmissionState::Failed => 7,
        SubmissionState::Expired | SubmissionState::Superseded => 8,
    }
}
fn verification_code(level: VerificationLevel) -> u8 {
    if level == VerificationLevel::UNVERIFIED {
        0
    } else if level == VerificationLevel::SEQUENCER_SIGNED {
        1
    } else if level == VerificationLevel::BATCH_INCLUDED {
        2
    } else if level == VerificationLevel::STATE_PROVEN {
        3
    } else if level == VerificationLevel::CHECKPOINT_FINALISED {
        4
    } else {
        5
    }
}
fn hex(bytes: &[u8]) -> String {
    const H: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|b| [H[(b >> 4) as usize] as char, H[(b & 15) as usize] as char])
        .collect()
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum OwnerAuthorityError {
    NonCanonical,
}

fn decode_owner_authority(reference: &str) -> Result<Authority, OwnerAuthorityError> {
    let key = reference.strip_prefix("did:layerx:").unwrap_or(reference);
    if key.len() != 64
        || !key
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
    {
        return Err(OwnerAuthorityError::NonCanonical);
    }
    let bytes = digest_from_hex(key).ok_or(OwnerAuthorityError::NonCanonical)?;
    Authority::owner(&bytes).map_err(|_| OwnerAuthorityError::NonCanonical)
}

fn digest_from_hex(value: &str) -> Option<[u8; 32]> {
    if value.len() != 64 {
        return None;
    }
    let mut out = [0; 32];
    for (index, slot) in out.iter_mut().enumerate() {
        *slot = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16).ok()?;
    }
    Some(out)
}
fn u64_field(value: &Value, field: &str) -> Result<u64, HumanOperationError> {
    value
        .get(field)
        .and_then(Value::as_u64)
        .ok_or(HumanOperationError::Refused)
}
fn hex_field(value: &Value, field: &str) -> Result<[u8; 32], HumanOperationError> {
    value
        .get(field)
        .and_then(Value::as_str)
        .and_then(digest_from_hex)
        .ok_or(HumanOperationError::Refused)
}
fn query(value: &str) -> String {
    value
        .bytes()
        .flat_map(|byte| {
            if byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b'~') {
                vec![char::from(byte)]
            } else {
                format!("%{byte:02X}").chars().collect()
            }
        })
        .collect()
}
pub(crate) fn prepare_digest(request: &HumanPrepare) -> [u8; 32] {
    crate::capability::binding::prepare_body_digest(
        legacy_prepare_digest(request),
        request.capability_id.as_ref(),
    )
}
fn legacy_prepare_digest(request: &HumanPrepare) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-human-journey-prepare/v1");
    digest.update(request.activity_type.to_be_bytes());
    hash_text(&mut digest, request.actor.as_bytes());
    hash_text(&mut digest, request.authority.as_bytes());
    digest.update(request.account_sequence.to_be_bytes());
    digest.update(request.not_before.to_be_bytes());
    digest.update(request.not_after.to_be_bytes());
    hash_text(&mut digest, request.idempotency_key.as_bytes());
    digest.update(request.fee_limit.to_be_bytes());
    digest.update(request.payload_hash);
    digest.update(Sha256::digest(&request.payload));
    digest.finalize().into()
}
pub(crate) fn submit_digest(request: &HumanSubmit) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-human-journey-submit/v1");
    hash_text(&mut digest, request.preparation_ref.as_bytes());
    digest.update(Sha256::digest(&request.signature));
    digest.update(request.signer_public_key);
    match request.approval_release_ref {
        Some(reference) => {
            digest.update([1]);
            digest.update(reference);
        }
        None => digest.update([0]),
    }
    digest.finalize().into()
}
pub(crate) fn subscription_create_digest(
    request: &layerx_agent_api::subscription::SubscriptionCreate,
) -> [u8; 32] {
    fn text(digest: &mut Sha256, value: &[u8]) {
        digest.update(u64::try_from(value.len()).unwrap_or(u64::MAX).to_be_bytes());
        digest.update(value);
    }
    let mut digest = Sha256::new();
    digest.update(b"LayerX/subscription/create-body/v1\0");
    text(&mut digest, request.scope.tenant.as_str().as_bytes());
    text(&mut digest, request.scope.agent.as_str().as_bytes());
    text(&mut digest, request.scope.capability.as_str().as_bytes());
    let filter = &request.filter;
    for set in [
        filter.agents.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect::<Vec<_>>(),
        filter.accounts.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.modules.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.assets.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.counterparties.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
    ] {
        digest.update(u64::try_from(set.len()).unwrap_or(u64::MAX).to_be_bytes());
        for (tenant, value) in set {
            text(&mut digest, tenant.as_bytes());
            text(&mut digest, value.as_bytes());
        }
    }
    digest.update(u64::try_from(filter.activity_types.values().len()).unwrap_or(u64::MAX).to_be_bytes());
    for activity in filter.activity_types.values() {
        text(&mut digest, activity.0.to_string().as_bytes());
    }
    digest.update(u64::try_from(filter.result_classes.values().len()).unwrap_or(u64::MAX).to_be_bytes());
    for class in filter.result_classes.values() {
        digest.update(class.raw().to_be_bytes());
    }
    digest.update(request.start.0 .0.to_be_bytes());
    text(&mut digest, request.delivery_target.as_str().as_bytes());
    digest.finalize().into()
}
fn capability_digest_text(digest: &mut Sha256, value: &[u8]) {
    digest.update(u64::try_from(value.len()).unwrap_or(u64::MAX).to_be_bytes());
    digest.update(value);
}
fn capability_digest_count(digest: &mut Sha256, count: usize) {
    digest.update(u64::try_from(count).unwrap_or(u64::MAX).to_be_bytes());
}
fn capability_digest_dimensions(
    digest: &mut Sha256,
    dimensions: &layerx_agent_api::capability::CapabilityDimensions,
) {
    capability_digest_count(digest, dimensions.activity_types.values().len());
    for activity in dimensions.activity_types.values() {
        digest.update(activity.0.to_be_bytes());
    }
    capability_digest_count(digest, dimensions.counterparties.values().len());
    for counterparty in dimensions.counterparties.values() {
        capability_digest_text(digest, counterparty.as_str().as_bytes());
    }
    capability_digest_count(digest, dimensions.assets.values().len());
    for asset in dimensions.assets.values() {
        capability_digest_text(digest, asset.as_str().as_bytes());
    }
    capability_digest_count(digest, dimensions.amount_ceilings.values().len());
    for ceiling in dimensions.amount_ceilings.values() {
        capability_digest_text(digest, ceiling.asset.as_str().as_bytes());
        digest.update(ceiling.amount.0.to_be_bytes());
    }
    capability_digest_count(digest, dimensions.rate_ceilings.values().len());
    for ceiling in dimensions.rate_ceilings.values() {
        digest.update(ceiling.window_seconds.0.to_be_bytes());
        digest.update(ceiling.maximum_actions.to_be_bytes());
    }
    capability_digest_count(digest, dimensions.purpose_constraints.values().len());
    for purpose in dimensions.purpose_constraints.values() {
        capability_digest_text(digest, purpose.as_str().as_bytes());
    }
    digest.update(dimensions.expiry.0.to_be_bytes());
}
pub(crate) fn capability_create_digest(
    request: &layerx_agent_api::capability::CapabilityCreate,
) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/capability/create-body/v1\0");
    capability_digest_text(&mut digest, request.tenant.as_str().as_bytes());
    capability_digest_text(&mut digest, request.agent_did.as_str().as_bytes());
    capability_digest_dimensions(&mut digest, &request.dimensions);
    digest.finalize().into()
}
pub(crate) fn capability_attenuate_digest(
    request: &layerx_agent_api::capability::CapabilityAttenuate,
) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/capability/attenuate-body/v1\0");
    capability_digest_text(&mut digest, request.tenant.as_str().as_bytes());
    capability_digest_text(&mut digest, request.agent_did.as_str().as_bytes());
    capability_digest_text(&mut digest, request.parent_id.as_str().as_bytes());
    capability_digest_dimensions(&mut digest, &request.dimensions);
    digest.finalize().into()
}
pub(crate) fn capability_revoke_digest(
    request: &layerx_agent_api::capability::CapabilityRevoke,
) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/capability/revoke-body/v1\0");
    capability_digest_text(&mut digest, request.tenant.as_str().as_bytes());
    capability_digest_text(&mut digest, request.agent_did.as_str().as_bytes());
    capability_digest_text(&mut digest, request.capability_id.as_str().as_bytes());
    digest.finalize().into()
}
/// Maps a timed capability error to the owner error. Every refusal (duplicate entry,
/// zero window, ceiling outside assets, expiry, wider than parent or authority) carries its
/// capability dimension; store faults stay Unavailable and every other refusal stays Refused.
fn capability_refusal(error: &crate::capability::timed::TimedError) -> HumanOperationError {
    use crate::capability::timed::TimedError;
    use crate::capability::Dimension;
    match error {
        TimedError::Duplicate(dimension) | TimedError::Wider(dimension) => {
            HumanOperationError::CapabilityRefused(*dimension)
        }
        TimedError::ZeroWindow => HumanOperationError::CapabilityRefused(Dimension::Rate),
        TimedError::CeilingOutsideAssets => {
            HumanOperationError::CapabilityRefused(Dimension::Amount)
        }
        TimedError::Expired | TimedError::NotYetValid => {
            HumanOperationError::CapabilityRefused(Dimension::Expiry)
        }
        _ => error.owner_error(),
    }
}
/// The authenticated coordinates of one capability request. The request fields never select
/// the actor: tenant and agent DID must equal the permit's resolved principal and peer.
fn capability_coordinates(
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    tenant: &str,
    agent: &str,
) -> Result<(TenantId, Did, String), HumanOperationError> {
    let principal = context.principal();
    let principal_agent =
        std::str::from_utf8(principal.agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
    if tenant != context.peer().tenant.as_str()
        || tenant != principal.tenant.as_str()
        || agent != principal_agent
    {
        return Err(HumanOperationError::Refused);
    }
    let tenant_id =
        TenantId::new(context.peer().tenant.clone()).map_err(|_| HumanOperationError::Refused)?;
    Ok((tenant_id, principal.agent.clone(), principal_agent.to_owned()))
}
/// The protocol authority the permit's open session actually uses (session.rs OpenRequest).
fn capability_session_authority(
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    control: &crate::session_control::SessionControl,
) -> Result<ProtocolAuthority, HumanOperationError> {
    let principal = context.principal();
    let registry = control.registry();
    let sessions = registry
        .read()
        .map_err(|_| HumanOperationError::Unavailable)?;
    let record = sessions
        .get(&principal.tenant, principal.session_id)
        .ok_or(HumanOperationError::Refused)?;
    if !record.open || record.request.agent != principal.agent {
        return Err(HumanOperationError::Refused);
    }
    Ok(record.request.authority.clone())
}
fn capability_authority_parts(authority: &ProtocolAuthority) -> (u8, [u8; 32]) {
    match authority {
        ProtocolAuthority::PrimaryKey(id) => (1, *id),
        ProtocolAuthority::SessionKey(id) => (2, *id),
        ProtocolAuthority::CapabilityGrant(id) => (3, *id),
    }
}
/// Authority block: text tenant, text agent_did, text authority_ref (lowercase hex of the
/// session authority id), bytes protocol_authority (kind byte then the 32-byte id, the
/// encoding encode_identity uses).
fn encode_capability_authority(
    out: &mut Encoder,
    tenant: &TenantId,
    agent: &str,
    authority: &ProtocolAuthority,
) -> Result<(), HumanOperationError> {
    let (kind, id) = capability_authority_parts(authority);
    out.text(tenant.as_str())?;
    out.text(agent)?;
    out.text(&crate::agent_rpc_dispatch::lower_hex(&id))?;
    let mut protocol_authority = Vec::with_capacity(33);
    protocol_authority.push(kind);
    protocol_authority.extend_from_slice(&id);
    out.bytes(&protocol_authority)
}
/// The verified native grant scope the permit's session authority narrows. Only a session
/// opened under `ProtocolAuthority::CapabilityGrant` carries a verified grant window
/// (LXGS2 not_after ms); every other authority is refused. The grant's action key is the one
/// its completed Human capability install recorded; the authority id is the identity's single
/// primary key (the LXGS2 authority the native producer checks against identity state).
fn capability_grant_scope<A: HumanAuthorityBoundary>(
    authority: &mut A,
    shared_store: &Arc<Mutex<Store>>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    tenant: &TenantId,
    actor: &Did,
    session_authority: &ProtocolAuthority,
) -> Result<CoreCapabilityScope, HumanOperationError> {
    let ProtocolAuthority::CapabilityGrant(grant_id) = session_authority else {
        return Err(HumanOperationError::Refused);
    };
    let identity = authority
        .core_identity(context.peer(), actor)
        .map_err(|error| map_identity_operation(&error))?;
    if identity.frozen || identity.verification_level == VerificationLevel::UNVERIFIED {
        return Err(HumanOperationError::Refused);
    }
    let mut primary = identity.authorities.iter().filter_map(|candidate| match candidate {
        ProtocolAuthority::PrimaryKey(id) => Some(*id),
        _ => None,
    });
    let (Some(authority_id), None) = (primary.next(), primary.next()) else {
        return Err(HumanOperationError::Refused);
    };
    let action_key = capability_grant_action_key(shared_store, tenant, grant_id)?;
    let observed =
        authority.capability_scope(context.peer(), actor, authority_id, action_key, *grant_id)?;
    if observed.observed_sequence == 0
        || observed.verification < 4
        || observed.verification > 5
        || observed.evidence_digest == [0; 32]
        || observed.evidence_digest
            != agent_evidence_digest(
                action_key,
                *grant_id,
                observed.observed_sequence,
                observed.verification,
            )
    {
        return Err(HumanOperationError::Refused);
    }
    Ok(observed)
}
/// The action key of the one completed Human capability install of `grant_id`
/// (`human-capability-action-v1:` records, layout of `install_capability`). None or more than
/// one is refused; a malformed record is Unavailable.
fn capability_grant_action_key(
    shared_store: &Arc<Mutex<Store>>,
    tenant: &TenantId,
    grant_id: &[u8; 32],
) -> Result<[u8; 32], HumanOperationError> {
    const PREFIX: &[u8] = b"human-capability-action-v1:";
    let store = shared_store
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?;
    let mut found = None;
    for object_id in store.list_object_ids(tenant, ObjectKind::Idempotency) {
        let Some(action) = object_id.strip_prefix(PREFIX) else {
            continue;
        };
        let action_key: [u8; 32] =
            action.try_into().map_err(|_| HumanOperationError::Unavailable)?;
        let Some(value) = store.get(&capability_action_key(tenant, action_key)?) else {
            continue;
        };
        let bytes = value.bytes();
        if value.class() != StorageClass::LocalOnly || bytes.len() < 34 || bytes[0] != 1 {
            return Err(HumanOperationError::Unavailable);
        }
        match bytes[33] {
            0 => continue,
            1 if bytes.len() >= 66 => {
                if bytes[34..66] == grant_id[..] && found.replace(action_key).is_some() {
                    return Err(HumanOperationError::Refused);
                }
            }
            _ => return Err(HumanOperationError::Unavailable),
        }
    }
    found.ok_or(HumanOperationError::Refused)
}
/// One record in the layout the agent RPC decoders accept. Asset and counterparty core ids
/// are emitted as strict lowercase 64-hex text; state is revoked whenever the revoked tag is
/// set, else expired at `now_ms`, else active.
fn encode_capability_record(
    out: &mut Encoder,
    record: &crate::capability::timed::TimedCapability,
    now_ms: u64,
) -> Result<(), HumanOperationError> {
    use crate::agent_rpc_dispatch::lower_hex;
    out.text(&lower_hex(&record.id))?;
    match &record.parent {
        Some(parent) => {
            out.u8(1);
            out.text(&lower_hex(parent))?;
        }
        None => out.u8(0),
    }
    out.u16(record.activity_types.len())?;
    for activity in &record.activity_types {
        out.fixed(&activity.to_be_bytes());
    }
    out.u16(record.counterparties.len())?;
    for counterparty in &record.counterparties {
        out.text(&lower_hex(counterparty))?;
    }
    out.u16(record.assets.len())?;
    for asset in &record.assets {
        out.text(&lower_hex(asset))?;
    }
    out.u16(record.amount_ceilings.len())?;
    for (asset, amount) in &record.amount_ceilings {
        out.text(&lower_hex(asset))?;
        out.u128(*amount);
    }
    out.u16(record.rate_ceilings.len())?;
    for (window_seconds, maximum_actions) in &record.rate_ceilings {
        out.u64(*window_seconds);
        out.u64(*maximum_actions);
    }
    out.u16(record.purposes.len())?;
    for purpose in &record.purposes {
        out.text(purpose)?;
    }
    out.u64(record.expiry_seconds);
    out.u64(record.created_at_ms);
    out.u64(record.created_at_sequence);
    let state = if record.revoked.is_some() {
        1
    } else if record.is_expired(now_ms) {
        2
    } else {
        0
    };
    out.u8(state);
    match record.revoked {
        Some((at_ms, at_sequence)) => {
            out.u8(1);
            out.u64(at_ms);
            out.u64(at_sequence);
        }
        None => out.u8(0),
    }
    Ok(())
}

const FEE_PROJECTION_RATIONALE: &str = "native committed fee schedule evaluated for the stated hypothetical meter at the captured authenticated head snapshot; not an executed fee and not verified";

fn fee_estimate_error(error: layerx_client::client::FeeEstimateError) -> HumanOperationError {
    match error {
        layerx_client::client::FeeEstimateError::SnapshotSkew { .. } => {
            HumanOperationError::Typed(crate::human::HumanRefusal::FeeSnapshotSkew)
        }
        layerx_client::client::FeeEstimateError::Read(
            layerx_client::read::ReadError::UnavailableCapability
            | layerx_client::read::ReadError::Disconnected
            | layerx_client::read::ReadError::Transport(_),
        ) => HumanOperationError::Unavailable,
        layerx_client::client::FeeEstimateError::MeterOutOfRange { .. }
        | layerx_client::client::FeeEstimateError::Read(_) => HumanOperationError::Refused,
    }
}

fn fee_projection_error(error: layerx_agent_api::identity::ContractError) -> HumanOperationError {
    match error {
        layerx_agent_api::identity::ContractError::Mismatch(_) => {
            HumanOperationError::Typed(crate::human::HumanRefusal::FeeSnapshotSkew)
        }
        _ => HumanOperationError::Refused,
    }
}

fn policy_context_mismatch(field: crate::human::PolicyContextField) -> HumanOperationError {
    HumanOperationError::Typed(crate::human::HumanRefusal::PolicyContextMismatch(field))
}

fn legacy_binding_error(error: crate::session_control::SessionControlError) -> HumanOperationError {
    match error {
        crate::session_control::SessionControlError::Unavailable => {
            HumanOperationError::Unavailable
        }
        _ => HumanOperationError::Refused,
    }
}

fn legacy_purpose_commitment(
    disclosure: &layerx_crypto::disclosure::Disclosure,
) -> Option<[u8; 32]> {
    use layerx_crypto::disclosure::DisclosedNativeOperation;
    use layerx_crypto::payments::Payment;
    if let Some(DisclosedNativeOperation::BudgetCreate(create)) = &disclosure.native_operation {
        return Some(create.purpose);
    }
    match disclosure.payment.as_ref()? {
        Payment::Receive { payer_grant, .. } => Some(payer_grant.purpose_hash),
        Payment::IssueGrant(grant) => Some(grant.purpose_hash),
        _ => None,
    }
}

fn legacy_policy_purpose(
    disclosure: &layerx_crypto::disclosure::Disclosure,
    label: Option<String>,
) -> Result<crate::policy::Purpose, HumanOperationError> {
    let undisclosed = HumanOperationError::Typed(crate::human::HumanRefusal::PurposeUndisclosed);
    match (legacy_purpose_commitment(disclosure), label) {
        (None, None) => Ok(crate::policy::Purpose::None),
        (Some(_), None) => Err(HumanOperationError::Typed(
            crate::human::HumanRefusal::IntentBindingMissing,
        )),
        (Some(commitment), Some(label))
            if label == crate::agent_rpc_dispatch::lower_hex(&commitment) =>
        {
            crate::policy::PurposeText::try_from(label)
                .map(crate::policy::Purpose::Text)
                .map_err(|_| undisclosed)
        }
        _ => Err(undisclosed),
    }
}

fn legacy_policy_effects(
    disclosure: &layerx_crypto::disclosure::Disclosure,
    raw: bool,
) -> Result<Vec<crate::policy::Effect>, HumanOperationError> {
    use crate::capability::Effect as Semantic;
    let underivable = HumanOperationError::Typed(if raw {
        crate::human::HumanRefusal::IntentBindingMissing
    } else {
        crate::human::HumanRefusal::IntentEffectUnderivable
    });
    let activity_type = u16::try_from(disclosure.activity_type.value()).map_err(|_| underivable)?;
    let plan = crate::capability::derive_effects(
        disclosure,
        &crate::capability::VerifiedInputs {
            revoke_balance: None,
        },
    )
    .map_err(|_| underivable)?;
    Ok(plan
        .effects()
        .iter()
        .filter_map(|effect| match *effect {
            Semantic::Transfer {
                to, asset, amount, ..
            } => Some((to, asset, amount)),
            Semantic::Issuance {
                account,
                asset,
                amount,
            }
            | Semantic::Destruction {
                account,
                asset,
                amount,
            } => Some((account, asset, amount)),
            Semantic::Authorization { .. } => None,
        })
        .map(|(counterparty, asset, amount)| crate::policy::Effect {
            activity_type,
            counterparty,
            asset,
            amount,
        })
        .collect())
}

fn legacy_policy_result(
    explanation: crate::policy::Explanation,
) -> Result<layerx_agent_api::policy::PolicyDryRunResult, HumanOperationError> {
    use layerx_agent_api::policy::{PolicyDecisionReason, PolicyOutcome};
    let outcome = match explanation.outcome {
        crate::policy::Outcome::Allow => PolicyOutcome::Allow,
        crate::policy::Outcome::Deny => PolicyOutcome::Deny,
    };
    let reason = match explanation.reason {
        crate::policy::DecisionReason::PermittedByRule => PolicyDecisionReason::PermittedByRule,
        crate::policy::DecisionReason::ExplicitDeny => PolicyDecisionReason::ExplicitDeny,
        crate::policy::DecisionReason::ApprovalRequired => PolicyDecisionReason::ApprovalRequired,
        crate::policy::DecisionReason::NoPermittingRule => PolicyDecisionReason::NoPermittingRule,
        crate::policy::DecisionReason::InvalidContext => PolicyDecisionReason::InvalidContext,
        crate::policy::DecisionReason::EvaluationFailure => PolicyDecisionReason::EvaluationFailure,
    };
    layerx_agent_api::policy::PolicyDryRunResult {
        outcome,
        policy_version: layerx_agent_api::identity::PolicyVersion::new(explanation.policy_version)
            .map_err(|_| HumanOperationError::Refused)?,
        matched_rules: explanation.matched_rules,
        deciding_rule: explanation.deciding_rule,
        reason,
        authority_statement: explanation.authority_statement.to_owned(),
    }
    .validate()
    .map_err(|_| HumanOperationError::Refused)
}

fn policy_refusal(refusal: crate::policy::PolicyDryRunRefusal) -> HumanOperationError {
    HumanOperationError::Typed(crate::human::HumanRefusal::Policy(refusal))
}

/// Pins the one authenticated chain head and binds the kind-5 program state to it, at the
/// authenticated current core time.
fn current_program_bundle(
    programs: &mut crate::ops::program::ProgramOperations,
    node: &mut Client,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    program: layerx_programs::ProgramId,
) -> Result<(u64, layerx_programs::VerifiedProgramBundle), HumanOperationError> {
    let now = core_preparation_snapshot(node, context.peer(), &context.principal().agent)?
        .protocol_timestamp;
    let correlation = boundary_correlation(context.peer(), &program.bytes(), b"program-state");
    let bound = programs
        .current_program(node, program, now, correlation)
        .map_err(program_operation_error)?;
    Ok((now, bound))
}

fn program_operation_error(error: crate::ops::program::ProgramOperationError) -> HumanOperationError {
    match error {
        crate::ops::program::ProgramOperationError::Stale
        | crate::ops::program::ProgramOperationError::HeadAdvanced => {
            HumanOperationError::Typed(crate::human::HumanRefusal::StalePinnedHead)
        }
        crate::ops::program::ProgramOperationError::Unavailable => HumanOperationError::Unavailable,
        crate::ops::program::ProgramOperationError::InvalidRequest
        | crate::ops::program::ProgramOperationError::UnknownProgram
        | crate::ops::program::ProgramOperationError::InactiveProgram
        | crate::ops::program::ProgramOperationError::UnverifiedReceipt
        | crate::ops::program::ProgramOperationError::Submit(_)
        | crate::ops::program::ProgramOperationError::ProgramStateAbsent
        | crate::ops::program::ProgramOperationError::CoreRefusal { .. } => {
            HumanOperationError::Refused
        }
    }
}

fn encode_program_discovery(
    out: &mut Encoder,
    discovery: &crate::ops::program::ProgramDiscovery,
) -> Result<(), HumanOperationError> {
    out.fixed(&discovery.program.bytes());
    out.u8(match discovery.lifecycle {
        layerx_programs::ProgramLifecycle::Active => 0,
        layerx_programs::ProgramLifecycle::Deprecated => 1,
        layerx_programs::ProgramLifecycle::Tombstoned => 2,
    });
    out.u64(discovery.observed_sequence);
    out.u64(discovery.observed_at);
    out.u64(discovery.valid_through);
    out.fixed(&discovery.receipt_digest);
    out.fixed(&discovery.state_root);
    out.u32(discovery.version);
    out.u16(usize::from(discovery.abi_version))?;
    out.fixed(&discovery.code_hash);
    Ok(())
}

fn export_refusal(error: crate::export::ExportProduceError) -> HumanOperationError {
    use crate::export::ExportProduceError as Produce;
    use crate::human::HumanRefusal;
    use layerx_proof::export::CompleteExportError as Complete;
    match error {
        Produce::SettlementAnchoringUnavailable
        | Produce::Verification(Complete::SettlementAnchoringUnavailable) => {
            HumanOperationError::Typed(HumanRefusal::ExportSettlementAnchoringUnavailable)
        }
        Produce::Verification(Complete::LevelNotAchieved { .. }) => {
            HumanOperationError::Typed(HumanRefusal::ExportLevelUnattainable)
        }
        Produce::Oversize => HumanOperationError::Typed(HumanRefusal::ExportResponseTooLarge),
        Produce::Store
        | Produce::StaleHead
        | Produce::Availability { .. }
        | Produce::AvailabilityIncomplete { .. }
        | Produce::Evidence {
            error: layerx_client::evidence::EvidenceError::Unavailable,
            ..
        } => HumanOperationError::Unavailable,
        Produce::Facts(_)
        | Produce::NotOwned { .. }
        | Produce::AccountNotBound { .. }
        | Produce::Correlation
        | Produce::Evidence { .. }
        | Produce::ReceiptEvidence { .. }
        | Produce::Binding { .. }
        | Produce::MaintenanceLink { .. }
        | Produce::Codec(_)
        | Produce::Artifact(_)
        | Produce::ConflictingRecord
        | Produce::MixedSnapshot
        | Produce::Verification(_)
        | Produce::Contract => HumanOperationError::Refused,
    }
}

/// The permit's resolved tenant and agent DID text that capability bindings are keyed by.
fn binding_coordinates(
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
) -> Result<(TenantId, String), HumanOperationError> {
    let principal = context.principal();
    let agent =
        std::str::from_utf8(principal.agent.as_bytes()).map_err(|_| HumanOperationError::Refused)?;
    let (tenant, _, agent) = capability_coordinates(context, principal.tenant.as_str(), agent)?;
    Ok((tenant, agent))
}

fn verified_revoke_balance(disclosure: &layerx_crypto::disclosure::Disclosure) -> Option<u128> {
    match &disclosure.native_operation {
        Some(layerx_crypto::disclosure::DisclosedNativeOperation::BudgetRevoke(revoke)) => {
            Some(revoke.context.balance)
        }
        _ => None,
    }
}

fn capability_purpose(
    record: &crate::capability::timed::TimedCapability,
    disclosure: &layerx_crypto::disclosure::Disclosure,
) -> Result<crate::capability::binding::PurposeBinding, crate::capability::binding::BindingError> {
    use crate::capability::binding;
    use layerx_crypto::disclosure::DisclosedNativeOperation;
    let purposes = record_purposes(record)?;
    let context_commitment = match &disclosure.native_operation {
        Some(DisclosedNativeOperation::BudgetFund(fund)) => Some(fund.context.purpose_hash),
        Some(DisclosedNativeOperation::BudgetDefund(defund)) => Some(defund.context.purpose_hash),
        Some(DisclosedNativeOperation::BudgetRevoke(revoke)) => Some(revoke.context.purpose_hash),
        _ => None,
    };
    match context_commitment {
        Some(commitment) => binding::purpose_from_set(&purposes, Some(commitment)),
        None => binding::bind_purpose_from_set(
            &layerx_agent_api::identity::ExplicitSet::allow(purposes),
            disclosure,
        ),
    }
}

fn record_purposes(
    record: &crate::capability::timed::TimedCapability,
) -> Result<Vec<layerx_agent_api::identity::Purpose>, crate::capability::binding::BindingError> {
    record
        .purposes
        .iter()
        .map(|value| {
            layerx_agent_api::identity::Purpose::new(value.as_str())
                .map_err(|_| crate::capability::binding::BindingError::Corrupt)
        })
        .collect()
}

fn consume_refusal(error: &crate::capability::ConsumeError) -> HumanOperationError {
    use crate::capability::ConsumeError;
    match error {
        ConsumeError::Refused { dimension, .. } => {
            HumanOperationError::CapabilityRefused(*dimension)
        }
        ConsumeError::MissingReservation
        | ConsumeError::Corrupt
        | ConsumeError::SizeOverflow
        | ConsumeError::Store(_) => HumanOperationError::Unavailable,
        ConsumeError::InvalidIdentity
        | ConsumeError::EmptyChain
        | ConsumeError::Conflict
        | ConsumeError::Indeterminate
        | ConsumeError::Overflow => HumanOperationError::Refused,
    }
}

fn settle_capability_chain(
    store: &mut Store,
    tenant: &TenantId,
    preparation_id: [u8; 32],
    terminal: &crate::protocol_evidence::VerifiedReceiptEvidence,
) -> Result<(), HumanOperationError> {
    if crate::capability::binding::preparation_binding(store, tenant, &preparation_id)
        .map_err(|error| binding_refusal(&error))?
        .is_none()
    {
        return Ok(());
    }
    crate::capability::settle_chain(
        store,
        tenant,
        preparation_id,
        crate::capability::SettleOutcome::Verified(terminal),
    )
    .map_err(|error| consume_refusal(&error))
}

fn recheck_submit_binding(
    disclosure: &layerx_crypto::disclosure::Disclosure,
    control: &SessionControl,
    tenant: &TenantId,
    agent: &str,
    record: &crate::prepare::DurablePreparation,
    now_ms: u64,
) -> Result<(), HumanOperationError> {
    recheck_stored_binding(control, tenant, agent, record, now_ms)?;
    let (Some(extension), _) = stored_binding(record)? else {
        return Ok(());
    };
    let digest = disclosure
        .audit_digest()
        .map_err(|_| HumanOperationError::Unavailable)?;
    let shared = control.store();
    let store = shared
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?;
    crate::capability::binding::recheck_on_submit(
        &store,
        tenant,
        &record.preparation_id,
        &digest,
        &extension.capability_id,
    )
    .map_err(|error| binding_refusal(&error))
}

/// Owner surface of a binding failure; a chain failure keeps the timed dimension mapping.
fn binding_refusal(error: &crate::capability::binding::BindingError) -> HumanOperationError {
    match error {
        crate::capability::binding::BindingError::Chain(error) => capability_refusal(error),
        other => other.owner_error(),
    }
}

/// The capability binding (tag 1, bound preparations only) and admission outcome (tag 2)
/// recorded with one durable preparation; they must name the same capability.
fn stored_binding(
    record: &crate::prepare::DurablePreparation,
) -> Result<
    (
        Option<crate::capability::binding::CapabilityExtension>,
        crate::capability::binding::AdmissionOutcome,
    ),
    HumanOperationError,
> {
    use crate::capability::binding::{AdmissionOutcome, CapabilityExtension};
    let extension = record
        .extensions
        .get(&crate::prepare::EXTENSION_CAPABILITY)
        .map(|bytes| CapabilityExtension::decode(bytes))
        .transpose()
        .map_err(|error| binding_refusal(&error))?;
    let outcome = record
        .extensions
        .get(&crate::prepare::EXTENSION_OUTCOME)
        .ok_or(HumanOperationError::Unavailable)
        .and_then(|bytes| {
            AdmissionOutcome::decode(bytes).map_err(|error| binding_refusal(&error))
        })?;
    if outcome.capability_id != extension.as_ref().map(|value| value.capability_id) {
        return Err(HumanOperationError::Unavailable);
    }
    Ok((extension, outcome))
}

/// Rechecks a recorded preparation's binding at core time `now_ms`: a cancelled outcome,
/// a restricted agent on the legacy path, or a revoked, expired or changed chain refuses.
fn recheck_stored_binding(
    control: &SessionControl,
    tenant: &TenantId,
    agent: &str,
    record: &crate::prepare::DurablePreparation,
    now_ms: u64,
) -> Result<crate::capability::binding::AdmissionOutcome, HumanOperationError> {
    if record.tenant != *tenant {
        return Err(HumanOperationError::Refused);
    }
    let (extension, outcome) = stored_binding(record)?;
    if outcome.state != crate::capability::binding::OutcomeState::Admitted {
        return Err(binding_refusal(
            &crate::capability::binding::BindingError::Cancelled,
        ));
    }
    let shared = control.store();
    let store = shared
        .lock()
        .map_err(|_| HumanOperationError::Unavailable)?;
    crate::capability::binding::recheck(&store, tenant, agent, extension.as_ref(), now_ms)
        .map_err(|error| binding_refusal(&error))?;
    Ok(outcome)
}

fn sweep_budget_revocation(
    control: &SessionControl,
    lifecycle: &PreparationLifecycle,
    tenant: &TenantId,
    limit_id: crate::budget::LimitId,
    at_ms: u64,
    at_sequence: u64,
) -> Result<(), HumanOperationError> {
    use crate::prepare::{
        DurablePreparation, LifecycleState, PreparationExtension, EXTENSION_OUTCOME,
    };
    let mut unsent = Vec::new();
    {
        let shared = control.store();
        let store = shared
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)?;
        for preparation_id in DurablePreparation::recorded_ids(&store, tenant)
            .map_err(|_| HumanOperationError::Unavailable)?
        {
            let key = DurablePreparation::store_key(tenant, preparation_id)
                .map_err(|_| HumanOperationError::Unavailable)?;
            let value = store.get(&key).ok_or(HumanOperationError::Unavailable)?;
            let record = DurablePreparation::decode(tenant.clone(), value.bytes())
                .map_err(|_| HumanOperationError::Unavailable)?;
            if record.terminal()
                || !record.holds.iter().any(|(hold, _)| hold.limit_id == limit_id)
            {
                continue;
            }
            match lifecycle.state(preparation_id) {
                Ok(LifecycleState::Prepared | LifecycleState::Signing | LifecycleState::Signed) => {}
                Ok(_) => continue,
                Err(_) => return Err(HumanOperationError::Unavailable),
            }
            let (_, outcome) = stored_binding(&record)?;
            unsent.push((preparation_id, outcome.cancelled(at_ms, at_sequence)));
        }
    }
    for (preparation_id, outcome) in &unsent {
        control
            .cancel_write(
                tenant,
                *preparation_id,
                PreparationExtension {
                    tag: EXTENSION_OUTCOME,
                    bytes: outcome.encode(),
                },
                at_sequence,
            )
            .map_err(rpc_commit_error)?;
    }
    let ids: std::collections::BTreeSet<[u8; 32]> =
        unsent.iter().map(|(preparation_id, _)| *preparation_id).collect();
    control
        .invalidate_preparation_ids(&ids, at_sequence)
        .map_err(rpc_commit_error)?;
    Ok(())
}

/// Completes every owed revocation cleanup of one tenant: each unsent (Prepared, Signing or
/// Signed) preparation whose chain holds a revoked record is durably cancelled, then the
/// in-memory preparations are invalidated, then the cleanup record is removed. Submitted,
/// Acknowledged and Unknown work, and records the in-memory lifecycle does not hold, keep
/// their holds. A failure leaves the cleanup record for the next revoke or restart.
fn sweep_capability_cleanups(
    control: &SessionControl,
    lifecycle: &PreparationLifecycle,
    tenant: &TenantId,
) -> Result<(), HumanOperationError> {
    use crate::capability::binding;
    use crate::prepare::{
        DurablePreparation, LifecycleError, LifecycleState, PreparationExtension,
        EXTENSION_CAPABILITY, EXTENSION_OUTCOME,
    };
    let shared = control.store();
    let lock = || {
        shared
            .lock()
            .map_err(|_| HumanOperationError::Unavailable)
    };
    let cleanups =
        binding::pending_cleanups(&*lock()?, tenant).map_err(|error| binding_refusal(&error))?;
    for cleanup in cleanups {
        let mut unsent = Vec::new();
        {
            let store = lock()?;
            let revoked = binding::revoked_ids(&store, tenant, &cleanup.agent)
                .map_err(|error| binding_refusal(&error))?;
            for preparation_id in DurablePreparation::recorded_ids(&store, tenant)
                .map_err(|_| HumanOperationError::Unavailable)?
            {
                let key = DurablePreparation::store_key(tenant, preparation_id)
                    .map_err(|_| HumanOperationError::Unavailable)?;
                let value = store.get(&key).ok_or(HumanOperationError::Unavailable)?;
                let record = DurablePreparation::decode(tenant.clone(), value.bytes())
                    .map_err(|_| HumanOperationError::Unavailable)?;
                if record.terminal() || !record.extensions.contains_key(&EXTENSION_CAPABILITY) {
                    continue;
                }
                let (Some(extension), outcome) = stored_binding(&record)? else {
                    return Err(HumanOperationError::Unavailable);
                };
                if !binding::is_invalidated(&extension, &revoked) {
                    continue;
                }
                match lifecycle.state(preparation_id) {
                    Ok(LifecycleState::Prepared | LifecycleState::Signing | LifecycleState::Signed) => {}
                    Ok(_) => continue,
                    Err(_) => return Err(HumanOperationError::Unavailable),
                }
                unsent.push((
                    preparation_id,
                    outcome.cancelled(cleanup.at_ms, cleanup.at_sequence),
                ));
            }
        }
        for (preparation_id, outcome) in &unsent {
            control
                .cancel_write(
                    tenant,
                    *preparation_id,
                    PreparationExtension {
                        tag: EXTENSION_OUTCOME,
                        bytes: outcome.encode(),
                    },
                    cleanup.at_sequence,
                )
                .map_err(rpc_commit_error)?;
        }
        let ids: std::collections::BTreeSet<[u8; 32]> =
            unsent.iter().map(|(preparation_id, _)| *preparation_id).collect();
        control
            .invalidate_preparation_ids(&ids, cleanup.at_sequence)
            .map_err(rpc_commit_error)?;
        binding::complete_cleanup(&mut *lock()?, &cleanup)
            .map_err(|error| binding_refusal(&error))?;
    }
    Ok(())
}

impl crate::budget::BudgetMutationPipeline for NodeBudgetPipeline<'_> {
    fn submit_verified(
        &mut self,
        submission: &VerifiedSubmission,
    ) -> Result<CoreBudgetReceipt, BudgetCreationError> {
        let activity_id = match self.node.submit_signed(
            self.registry,
            self.signer,
            self.correlation,
            0,
            submission.exact_bytes(),
        ) {
            Ok(Submission::Acknowledged(acknowledgement)) => acknowledgement.activity_id(),
            Ok(Submission::Unknown(_)) | Err(_) => return Err(BudgetCreationError::Submission),
        };
        if activity_id != submission.activity_id() {
            return Err(BudgetCreationError::Submission);
        }
        let receipt_correlation = self.correlation.wrapping_add(1).max(1);
        let mut attempt = 0;
        loop {
            match self.node.proof_bundle(
                ProofBundleSelector::Receipt(activity_id),
                receipt_correlation,
                self.registry,
            ) {
                Ok(bundle) => {
                    let evidence = raw_receipt_evidence(&bundle)
                        .map_err(|_| BudgetCreationError::Submission)?;
                    return Ok(CoreBudgetReceipt { evidence });
                }
                Err(error) if evidence_unavailable(&error) && attempt < self.receipt_attempts => {
                    attempt += 1;
                    std::thread::sleep(self.receipt_poll);
                }
                Err(_) => return Err(BudgetCreationError::Submission),
            }
        }
    }

    fn budget_state(
        &mut self,
        budget_id: [u8; 32],
    ) -> Result<ProtocolBudgetState, BudgetCreationError> {
        BudgetPipeline::budget_state(self, budget_id)
    }

    fn budget_balance(
        &mut self,
        account: [u8; 32],
        asset: [u8; 32],
    ) -> Result<u128, BudgetCreationError> {
        let balance = self
            .node
            .balance(
                account,
                asset,
                VerificationLevel::STATE_PROVEN,
                self.correlation.wrapping_add(3).max(1),
                self.authorization,
            )
            .map_err(|_| BudgetCreationError::CreatedBudgetUnconfirmed)?;
        if balance.account != account || balance.asset != asset {
            return Err(BudgetCreationError::ContextMismatch);
        }
        Ok(balance.amount.value())
    }
}

fn budget_aware_prepare(
    context: Option<&layerx_crypto::disclosure::BudgetStateContext>,
    boundary: &mut dyn crate::prepare::CorePreparationBoundary,
    defaults: PreparationDefaults,
    request: PrepareRequest,
    protocol_version: u16,
) -> Result<crate::prepare::Prepared, crate::prepare::PrepareError> {
    match context {
        Some(context) => crate::prepare::prepare_budget_mutation_for_protocol(
            boundary,
            defaults,
            request,
            protocol_version,
            context,
        ),
        None => prepare_activity_for_protocol(boundary, defaults, request, protocol_version),
    }
}

fn budget_refusal(error: BudgetCreationError) -> HumanOperationError {
    use crate::human::HumanRefusal;
    match error {
        BudgetCreationError::Submission => HumanOperationError::Unavailable,
        BudgetCreationError::Expired => HumanOperationError::Typed(HumanRefusal::BudgetExpired),
        BudgetCreationError::StaleRevocation => {
            HumanOperationError::Typed(HumanRefusal::BudgetStaleRevocation)
        }
        BudgetCreationError::BudgetNotLive => {
            HumanOperationError::Typed(HumanRefusal::BudgetNotLive)
        }
        BudgetCreationError::ContextMismatch => {
            HumanOperationError::Typed(HumanRefusal::BudgetContextMismatch)
        }
        BudgetCreationError::NotBudgetCreation | BudgetCreationError::InvalidLimit => {
            HumanOperationError::Typed(HumanRefusal::BudgetCodec)
        }
        _ => HumanOperationError::Refused,
    }
}

fn daemon_limit_refusal(error: crate::budget::DaemonLimitError) -> HumanOperationError {
    use crate::budget::DaemonLimitError;
    use crate::human::HumanRefusal;
    match error {
        DaemonLimitError::Store(_) | DaemonLimitError::Corrupt => HumanOperationError::Unavailable,
        DaemonLimitError::Expired => HumanOperationError::Typed(HumanRefusal::BudgetExpired),
        DaemonLimitError::IdCollision => {
            HumanOperationError::Typed(HumanRefusal::BudgetLimitCollision)
        }
        DaemonLimitError::Revoked => HumanOperationError::Typed(HumanRefusal::BudgetLimitRevoked),
        DaemonLimitError::Conflict => HumanOperationError::Typed(HumanRefusal::BudgetLimitConflict),
        DaemonLimitError::Limit(_) => HumanOperationError::Typed(HumanRefusal::BudgetLimitExceeded),
        DaemonLimitError::Invalid | DaemonLimitError::Unknown | DaemonLimitError::Arithmetic => {
            HumanOperationError::Refused
        }
    }
}

fn budget_contract_refusal(error: layerx_agent_api::identity::ContractError) -> HumanOperationError {
    use crate::human::HumanRefusal;
    use layerx_agent_api::identity::ContractError;
    match error {
        ContractError::Empty(_) => HumanOperationError::Typed(HumanRefusal::BudgetAuthorizationRequired),
        ContractError::Mismatch(_) => {
            HumanOperationError::Typed(HumanRefusal::BudgetAuthorizationUnexpected)
        }
        ContractError::DaemonLimitFunding => {
            HumanOperationError::Typed(HumanRefusal::BudgetDaemonLimitFunding)
        }
        _ => HumanOperationError::Refused,
    }
}

fn daemon_limit_agent(agent_did: &str) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-agentd/daemon-limit-agent/v1\0");
    hash_text(&mut digest, agent_did.as_bytes());
    digest.finalize().into()
}

fn daemon_limit_budget_id(tenant: &TenantId, agent_did: &str, key: &[u8; 32]) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-agentd/daemon-limit-id/v1\0");
    hash_text(&mut digest, tenant.as_str().as_bytes());
    hash_text(&mut digest, agent_did.as_bytes());
    hash_text(&mut digest, key);
    digest.finalize().into()
}

fn daemon_limit_view(
    record: &crate::budget::DaemonLimitRecord,
) -> layerx_agent_api::budget::DaemonLimitView {
    layerx_agent_api::budget::DaemonLimitView {
        budget_id: record.budget_id,
        asset: record.asset,
        ceiling: layerx_agent_api::BudgetLimit(record.ceiling),
        consumed: layerx_agent_api::Amount(record.consumed),
        expiry_ms: record.expiry_ms,
        revoked: record.revoked,
    }
}

fn budget_authority(
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    control: &crate::session_control::SessionControl,
    tenant: &str,
    agent: &str,
) -> Result<(TenantId, layerx_agent_api::budget::AuthorityDescription), HumanOperationError> {
    use layerx_agent_api::identity::{AgentDid, AuthorityRef};
    let (tenant_id, _, agent_text) = capability_coordinates(context, tenant, agent)?;
    let authority = capability_session_authority(context, control)?;
    let (kind, id) = capability_authority_parts(&authority);
    let mut protocol_authority = Vec::with_capacity(33);
    protocol_authority.push(kind);
    protocol_authority.extend_from_slice(&id);
    let description = layerx_agent_api::budget::AuthorityDescription::new(
        layerx_agent_api::identity::TenantId::new(tenant_id.as_str())
            .map_err(budget_contract_refusal)?,
        AgentDid::new(agent_text).map_err(budget_contract_refusal)?,
        AuthorityRef::new(crate::agent_rpc_dispatch::lower_hex(&id))
            .map_err(budget_contract_refusal)?,
        protocol_authority,
    )
    .map_err(budget_contract_refusal)?;
    Ok((tenant_id, description))
}

fn prepared_budget_context(
    prepared: &crate::prepare::Prepared,
) -> Option<layerx_crypto::disclosure::BudgetStateContext> {
    use layerx_crypto::disclosure::DisclosedNativeOperation;
    match prepared.disclosure.native_operation.as_ref()? {
        DisclosedNativeOperation::BudgetFund(value) => Some(value.context),
        DisclosedNativeOperation::BudgetDefund(value) => Some(value.context),
        DisclosedNativeOperation::BudgetRevoke(value) => Some(value.context),
        _ => None,
    }
}

fn owner_signature(
    prepared: &crate::prepare::Prepared,
    carrier: &layerx_agent_api::budget::BudgetAuthorization,
) -> Result<([u8; 64], [u8; 32]), HumanOperationError> {
    let Authority::Owner(owner) = prepared.envelope.authority() else {
        return Err(HumanOperationError::Refused);
    };
    let owner: [u8; 32] = owner
        .as_ref()
        .try_into()
        .map_err(|_| HumanOperationError::Refused)?;
    if carrier
        .signer_public_key
        .is_some_and(|signer| signer != owner)
    {
        return Err(HumanOperationError::Refused);
    }
    let signature: [u8; 64] = carrier
        .signature
        .as_bytes()
        .try_into()
        .map_err(|_| HumanOperationError::Typed(crate::human::HumanRefusal::BudgetCodec))?;
    Ok((signature, owner))
}

pub(crate) fn budget_write_charge(
    store: &Store,
    tenant: &TenantId,
    prepared: &crate::prepare::Prepared,
    core_time_ms: u64,
    head_sequence_bound: u64,
) -> Result<Option<crate::session_control::WriteCharge>, HumanOperationError> {
    let mut amount: u128 = 0;
    for disclosed in &prepared.disclosure.amounts {
        if matches!(disclosed.role, layerx_crypto::disclosure::AmountRole::Transfer) {
            amount = amount
                .checked_add(disclosed.value)
                .ok_or(HumanOperationError::Refused)?;
        }
    }
    if amount == 0 {
        return Ok(None);
    }
    let actor = std::str::from_utf8(prepared.envelope.actor_did().as_bytes())
        .map_err(|_| HumanOperationError::Refused)?;
    let applicable_limits = crate::budget::applicable_daemon_limits(
        store,
        tenant,
        daemon_limit_agent(actor),
        prepared.disclosure.asset,
        crate::budget::CoreTimestampMs(core_time_ms),
    )
    .map_err(daemon_limit_refusal)?;
    if applicable_limits.is_empty() {
        return Ok(None);
    }
    Ok(Some(crate::session_control::WriteCharge {
        amount,
        applicable_limits,
        head_sequence_bound,
        core_deadline_ms: Some(crate::budget::CoreTimestampMs(
            prepared.disclosure.expiry.not_after,
        )),
    }))
}

fn subscription_create_identity(
    tenant: &[u8],
    agent: &[u8],
    capability: &[u8],
    key: &[u8; 32],
) -> Result<layerx_agent_api::subscription::SubscriptionId, HumanOperationError> {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/subscription/create/v1\0");
    for value in [tenant, agent, capability] {
        let length = u32::try_from(value.len()).map_err(|_| HumanOperationError::Refused)?;
        digest.update(length.to_be_bytes());
        digest.update(value);
    }
    digest.update(key);
    let identity: [u8; 32] = digest.finalize().into();
    layerx_agent_api::subscription::SubscriptionId::new(crate::agent_rpc_dispatch::lower_hex(&identity))
        .map_err(|_| HumanOperationError::Refused)
}

fn hash_text(digest: &mut Sha256, value: &[u8]) {
    digest.update(u32::try_from(value.len()).unwrap_or(u32::MAX).to_be_bytes());
    digest.update(value);
}

struct Encoder(Vec<u8>);

fn production_verification_level(level: layerx_agent_api::verify::Level) -> VerificationLevel {
    match level {
        layerx_agent_api::verify::Level::Unverified => VerificationLevel::UNVERIFIED,
        layerx_agent_api::verify::Level::SequencerSigned => VerificationLevel::SEQUENCER_SIGNED,
        layerx_agent_api::verify::Level::BatchIncluded => VerificationLevel::BATCH_INCLUDED,
        layerx_agent_api::verify::Level::StateProven => VerificationLevel::STATE_PROVEN,
        layerx_agent_api::verify::Level::CheckpointFinalised => {
            VerificationLevel::CHECKPOINT_FINALISED
        }
        layerx_agent_api::verify::Level::SettlementAnchored => {
            VerificationLevel::SETTLEMENT_ANCHORED
        }
    }
}

fn production_module_id(
    module: &layerx_agent_api::read::ModuleRef,
) -> Result<u16, HumanOperationError> {
    let text = module.as_str();
    let module_id = text
        .parse::<u16>()
        .map_err(|_| HumanOperationError::Refused)?;
    if module_id.to_string() != text {
        return Err(HumanOperationError::Refused);
    }
    Ok(module_id)
}

fn production_batch_number(
    batch: &layerx_agent_api::read::BatchRef,
) -> Result<u64, HumanOperationError> {
    let text = batch.as_str();
    let batch_number = text
        .parse::<u64>()
        .map_err(|_| HumanOperationError::Refused)?;
    if batch_number == 0 || batch_number.to_string() != text {
        return Err(HumanOperationError::Refused);
    }
    Ok(batch_number)
}

fn production_batch_header_error(
    error: layerx_client::batch::BatchHeaderError,
) -> HumanOperationError {
    match error {
        layerx_client::batch::BatchHeaderError::Transport(_)
        | layerx_client::batch::BatchHeaderError::UnavailableCapability
        | layerx_client::batch::BatchHeaderError::Disconnected => HumanOperationError::Unavailable,
        layerx_client::batch::BatchHeaderError::Envelope(_)
        | layerx_client::batch::BatchHeaderError::UnexpectedResponse
        | layerx_client::batch::BatchHeaderError::Malformed
        | layerx_client::batch::BatchHeaderError::Missing
        | layerx_client::batch::BatchHeaderError::SelectorMismatch
        | layerx_client::batch::BatchHeaderError::AuthorityMismatch
        | layerx_client::batch::BatchHeaderError::Signature => HumanOperationError::Refused,
    }
}
fn encode_subscription_record(
    out: &mut Encoder,
    record: &layerx_agent_api::subscription::SubscriptionRecord,
) -> Result<(), HumanOperationError> {
    out.text(record.subscription_id.as_str())?;
    out.text(record.scope.tenant.as_str())?;
    out.text(record.scope.agent.as_str())?;
    out.text(record.scope.capability.as_str())?;
    let filter = &record.filter;
    for set in [
        filter.agents.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect::<Vec<_>>(),
        filter.accounts.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.modules.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.assets.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
        filter.counterparties.values().iter().map(|item| (item.tenant.as_str(), item.value.as_str())).collect(),
    ] {
        out.u16(set.len())?;
        for (tenant, value) in set {
            out.text(tenant)?;
            out.text(value)?;
        }
    }
    out.u16(filter.activity_types.values().len())?;
    for activity in filter.activity_types.values() {
        out.text(&activity.0.to_string())?;
    }
    out.u16(filter.result_classes.values().len())?;
    for class in filter.result_classes.values() {
        out.fixed(&class.raw().to_be_bytes());
    }
    out.text(&record.start.0 .0.to_string())?;
    out.text(&record.last_acknowledged.0 .0.to_string())?;
    out.text(record.delivery_target.as_str())?;
    out.u8(u8::from(record.paused));
    Ok(())
}

impl Encoder {
    fn new() -> Self {
        Self(Vec::with_capacity(256))
    }
    fn u8(&mut self, value: u8) {
        self.0.push(value);
    }
    fn u16(&mut self, value: usize) -> Result<(), HumanOperationError> {
        self.0.extend_from_slice(
            &u16::try_from(value)
                .map_err(|_| HumanOperationError::Refused)?
                .to_be_bytes(),
        );
        Ok(())
    }
    fn u32(&mut self, value: u32) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn u64(&mut self, value: u64) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn u128(&mut self, value: u128) {
        self.0.extend_from_slice(&value.to_be_bytes());
    }
    fn fixed(&mut self, value: &[u8]) {
        self.0.extend_from_slice(value);
    }
    fn bytes(&mut self, value: &[u8]) -> Result<(), HumanOperationError> {
        if value.is_empty() || value.len() > MAX_RESPONSE {
            return Err(HumanOperationError::Refused);
        }
        self.u32(u32::try_from(value.len()).map_err(|_| HumanOperationError::Refused)?);
        self.fixed(value);
        Ok(())
    }
    fn text(&mut self, value: &str) -> Result<(), HumanOperationError> {
        self.bytes(value.as_bytes())
    }
    fn finish(self) -> Result<HumanResponse, HumanOperationError> {
        HumanResponse::new(self.0).map_err(|_| HumanOperationError::Refused)
    }
}

fn validate_capability<A: HumanAuthorityBoundary>(
    authority: &mut A,
    peer: &HumanPeer,
    tenant: &TenantId,
    did: &Did,
    request: &HumanCapabilityInstall,
) -> Result<(Capability, CoreCapabilityScope), HumanOperationError> {
    let identity = authority
        .core_identity(peer, did)
        .map_err(|error| map_identity_operation(&error))?;
    if identity.frozen
        || identity.verification_level == VerificationLevel::UNVERIFIED
        || !identity
            .authorities
            .contains(&ProtocolAuthority::PrimaryKey(request.authority_id))
    {
        return Err(HumanOperationError::Refused);
    }
    let observed = authority.capability_scope(
        peer,
        did,
        request.authority_id,
        request.action_key,
        request.capability_id,
    )?;
    if observed.observed_sequence == 0
        || observed.verification < 4
        || observed.verification > 5
        || observed.evidence_digest == [0; 32]
        || request.expiry_sequence <= observed.observed_sequence
    {
        return Err(HumanOperationError::Refused);
    }
    let capability = Capability::new(
        CapabilityId(request.capability_id),
        tenant.clone(),
        CapabilityDimensions {
            activity_types: request.activity_types.iter().copied().collect(),
            counterparties: request.counterparties.iter().copied().collect(),
            assets: request.assets.iter().copied().collect(),
            amount_ceiling: request.amount_ceiling,
            rate_ceiling: RateCeiling {
                maximum_uses: request.rate_maximum_uses,
                window_sequences: request.rate_window_sequences,
            },
            purposes: request.purposes.iter().cloned().collect(),
            expiry_sequence: request.expiry_sequence,
        },
    )
    .map_err(|_| HumanOperationError::Refused)?;
    assert_narrowing(
        &capability,
        ProtocolAuthority::PrimaryKey(request.authority_id),
        &observed.scope,
    )
    .map_err(|_| HumanOperationError::Refused)?;
    let expected = agent_evidence_digest(
        request.action_key,
        request.capability_id,
        observed.observed_sequence,
        observed.verification,
    );
    if observed.evidence_digest != expected {
        return Err(HumanOperationError::Refused);
    }
    Ok((capability, observed))
}

#[cfg(test)]
#[path = "outbound_tls/tests.rs"]
mod outbound_tls_tests;

#[cfg(test)]
mod owner_authority_tests {
    use super::{decode_owner_authority, hex, OwnerAuthorityError};

    fn native_send(
        actor: &str,
        key: &ed25519_dalek::SigningKey,
        asset: [u8; 32],
        expiry: u64,
    ) -> Result<Vec<u8>, String> {
        use ed25519_dalek::Signer;
        use sha2::{Digest, Sha256};
        let name = format!("agent:{actor}:main");
        let mut account = Sha256::new();
        account.update(b"LX:ACCOUNT:v1");
        account.update(
            u32::try_from(name.len())
                .map_err(|error| format!("account length: {error:?}"))?
                .to_be_bytes(),
        );
        account.update(name.as_bytes());
        let source: [u8; 32] = account.finalize().into();
        let idempotency = [0x83; 32];
        let mut context = Sha256::new();
        context.update(b"LXP/v1/context-hash\0");
        context.update(source);
        context.update(source);
        context.update(asset);
        context.update(1_u128.to_be_bytes());
        context.update(idempotency);
        let context: [u8; 32] = context.finalize().into();
        let mut common = Vec::new();
        common.extend_from_slice(&source);
        common.extend_from_slice(&source);
        common.extend_from_slice(&asset);
        common.extend_from_slice(&1_u128.to_be_bytes());
        common.extend_from_slice(&0_u64.to_be_bytes());
        common.extend_from_slice(&idempotency);
        common.extend_from_slice(&expiry.to_be_bytes());
        common.extend_from_slice(&context);
        common.push(0);
        let mut tail = context.to_vec();
        tail.extend_from_slice(&77_u32.to_be_bytes());
        tail.extend_from_slice(&3_u16.to_be_bytes());
        let mut authorization = b"LXP/v1/signature-preimage\0".to_vec();
        authorization.extend_from_slice(&0x5301_u16.to_be_bytes());
        authorization.extend_from_slice(&common);
        authorization.push(1);
        authorization.extend_from_slice(&source);
        authorization.extend_from_slice(&tail);
        let signature = key.sign(&Sha256::digest(&authorization));
        let mut payload = vec![0x53, 1, 0, 10];
        payload.extend_from_slice(&common);
        payload.push(1);
        payload.extend_from_slice(&source);
        payload.extend_from_slice(&key.verifying_key().to_bytes());
        payload.extend_from_slice(&signature.to_bytes());
        payload.extend_from_slice(&tail);
        Ok(payload)
    }

    fn native_client(socket: String) -> Result<layerx_client::Client, String> {
        use layerx_client::client::{Client, ClientConfig, ReconnectPolicy};
        use layerx_client::lni::handshake::HandshakeConfig;
        use layerx_client::lni::schema::Version;
        use layerx_client::lni::transport::Limits;
        use std::path::PathBuf;
        use std::time::Duration;
        Client::connect(ClientConfig {
            endpoint: PathBuf::from(socket),
            handshake: HandshakeConfig {
                built_interface_version: Version::V1_3,
                expected_protocol_version: 3,
                expected_network_id: 77,
            },
            limits: Limits {
                maximum_frame_bytes: 1_212_416,
                maximum_connections: 1,
                maximum_streams: 4,
                maximum_queued_bytes: 4_849_664,
                deadline: Duration::from_secs(10),
            },
            reconnect: ReconnectPolicy {
                maximum_attempts: 1,
                base_delay: Duration::from_millis(50),
                maximum_delay: Duration::from_millis(50),
                jitter_percent: 0,
            },
        })
        .map_err(|error| format!("authenticated native client: {error:?}"))
    }

    #[test]
    fn real_owner_authority_prepares_over_temp_socket() -> Result<(), String> {
        use super::{
            prepare_activity_for_protocol, ActivityType, Amount, Did, IdempotencyKey,
            PreparationDefaults, PrepareRequest, ProductionCorePreparationBoundary,
        };
        let Ok(socket) = std::env::var("LAYERX_TEST_OWNER_AUTHORITY_SOCKET") else {
            return Ok(());
        };
        let public = std::env::var("LAYERX_TEST_OWNER_AUTHORITY_PUBLIC")
            .map_err(|error| format!("native public key: {error:?}"))?;
        let actor = std::env::var("LAYERX_TEST_OWNER_AUTHORITY_DID")
            .map_err(|error| format!("native DID: {error:?}"))?;
        let mut client = native_client(socket)?;
        let key = super::digest_from_hex(&public)
            .ok_or_else(|| "native public key encoding".to_owned())?;
        let key_file = std::env::var("LAYERX_TEST_OWNER_AUTHORITY_KEY_FILE")
            .map_err(|error| format!("native signing key path: {error:?}"))?;
        let seed: [u8; 32] = std::fs::read(key_file)
            .map_err(|error| format!("native signing key file: {error:?}"))?
            .try_into()
            .map_err(|_| "native signing key length".to_owned())?;
        let signing_key = ed25519_dalek::SigningKey::from_bytes(&seed);
        assert_eq!(signing_key.verifying_key().to_bytes(), key);
        let asset = std::env::var("LAYERX_TEST_OWNER_AUTHORITY_ASSET")
            .map_err(|error| format!("native asset: {error:?}"))?;
        let asset =
            super::digest_from_hex(&asset).ok_or_else(|| "native asset encoding".to_owned())?;
        let native_actor =
            Did::new(actor.as_bytes()).map_err(|error| format!("native actor: {error:?}"))?;
        let now = client
            .preparation_state(&native_actor, 100)
            .map_err(|error| format!("native clock: {error:?}"))?
            .protocol_timestamp;
        let expires = now
            .checked_add(30_000)
            .ok_or_else(|| "test clock overflow".to_owned())?;
        let payload = native_send(&actor, &signing_key, asset, expires)?;
        for (index, reference) in [public.clone(), format!("did:layerx:{public}")]
            .iter()
            .enumerate()
        {
            let mut boundary = ProductionCorePreparationBoundary::new(
                &mut client,
                u64::try_from(index + 1).map_err(|error| format!("correlation: {error:?}"))?,
            )
            .map_err(|error| format!("production adapter: {error:?}"))?;
            let authority = decode_owner_authority(reference)
                .map_err(|error| format!("canonical authority: {error:?}"))?;
            let prepared = prepare_activity_for_protocol(
                &mut boundary,
                PreparationDefaults {
                    timestamp_span: 60_000,
                    fee_limit: Amount::ZERO,
                    maximum_payload_bytes: 1024,
                },
                PrepareRequest {
                    actor: Did::new(actor.as_bytes())
                        .map_err(|error| format!("native actor: {error:?}"))?,
                    authority,
                    activity_type: ActivityType::from_u32(0x0001_0005)
                        .map_err(|error| format!("Asset SEND: {error:?}"))?,
                    expected_account_sequence: Some(0),
                    timestamp_bound: Some(
                        super::TimestampBound::new(now.saturating_sub(1000), expires)
                            .map_err(|error| format!("native timestamp bound: {error:?}"))?,
                    ),
                    fee_limit: Some(Amount::ZERO),
                    idempotency_key: IdempotencyKey::new([0x83; 32]),
                    payload: payload.clone(),
                    declared_payload_limit: 1024,
                },
                3,
            )
            .map_err(|error| format!("real node preparation: {error:?}"))?;
            assert_eq!(prepared.envelope.authority().as_bytes(), key);
            assert_eq!(prepared.envelope.authority().as_bytes().len(), 32);
            assert!(!prepared
                .canonical_bytes
                .windows(public.len())
                .any(|window| window == public.as_bytes()));
        }
        Ok(())
    }

    #[test]
    fn ed25519_authority_decodes_from_hex_and_did() {
        let signer = ed25519_dalek::SigningKey::from_bytes(&[0x83; 32]);
        let public_key = signer.verifying_key().to_bytes();
        let canonical = hex(&public_key);
        for reference in [canonical.clone(), format!("did:layerx:{canonical}")] {
            let authority = decode_owner_authority(&reference)
                .unwrap_or_else(|error| panic!("canonical authority: {error:?}"));
            assert_eq!(authority.as_bytes(), public_key);
            assert_eq!(hex(authority.as_bytes()), canonical);
        }
    }

    #[test]
    fn malformed_authority_is_refused_without_unicode_slicing() {
        let canonical = "ab".repeat(32);
        for reference in [
            String::new(),
            "a".repeat(63),
            "a".repeat(65),
            "g".repeat(64),
            canonical.to_uppercase(),
            format!("0x{canonical}"),
            format!(" {canonical}"),
            format!("{canonical}\n"),
            format!("did:other:{canonical}"),
            format!("did:layerx:did:layerx:{canonical}"),
            format!("did:layerx:{canonical}#key-1"),
            format!("a{}", "€".repeat(21)),
        ] {
            assert_eq!(
                decode_owner_authority(&reference),
                Err(OwnerAuthorityError::NonCanonical),
                "accepted {reference:?}"
            );
        }
    }
}

#[cfg(test)]
mod terminal_recovery_tests {
    use super::persisted_terminal_receipt_matches;
    use crate::protocol_evidence::{RawReceiptEvidence, VerifiedReceiptEvidence};
    use layerx_proof::merkle::Proof;
    use layerx_proof::receipt::AuthorizedBatch;

    fn decode(value: &str) -> Vec<u8> {
        value
            .as_bytes()
            .chunks_exact(2)
            .map(|bytes| {
                u8::from_str_radix(
                    std::str::from_utf8(bytes)
                        .unwrap_or_else(|error| panic!("fixture hex: {error}")),
                    16,
                )
                .unwrap_or_else(|error| panic!("fixture hex: {error}"))
            })
            .collect()
    }

    #[test]
    fn terminal_recovery_checks_the_authenticated_signed_receipt_reference() {
        let value: serde_json::Value = serde_json::from_str(include_str!(
            "../../../../platform/hosted/authority/tests/fixtures/real-program-deploy-receipt.json"
        ))
        .unwrap_or_else(|error| panic!("committed receipt: {error}"));
        let field = |name: &str| {
            decode(
                value[name]
                    .as_str()
                    .unwrap_or_else(|| panic!("fixture field {name}")),
            )
        };
        let canonical = field("receipt_hex");
        let header_bytes = field("header_hex");
        let header = layerx_wire::receipt::decode_batch_header(&header_bytes)
            .unwrap_or_else(|error| panic!("header: {error:?}"));
        let receipt = layerx_wire::receipt::decode(&canonical)
            .unwrap_or_else(|error| panic!("receipt: {error:?}"));
        let protocol = receipt
            .protocol()
            .unwrap_or_else(|| panic!("protocol receipt"));
        let authority = AuthorizedBatch::new(
            protocol.batch_id(),
            protocol.asset(),
            header.previous_state_root(),
            header.resulting_state_root(),
            field("sequencer_public_key_hex")
                .try_into()
                .unwrap_or_else(|_| panic!("public key width")),
        );
        assert_eq!(value["proof_count"], 1);
        assert_eq!(value["proof_index"], 0);
        let raw = RawReceiptEvidence::new(
            canonical.clone(),
            Proof::new(0, 1, Vec::new()).unwrap_or_else(|error| panic!("proof: {error:?}")),
            header_bytes,
            field("header_signature_hex")
                .try_into()
                .unwrap_or_else(|_| panic!("signature width")),
        );
        let terminal = VerifiedReceiptEvidence::verify_authorized(
            &raw,
            &authority,
            header.protocol_version(),
            header.network_id(),
        )
        .unwrap_or_else(|error| panic!("real terminal receipt: {error:?}"));
        assert!(persisted_terminal_receipt_matches(
            &canonical,
            terminal.receipt_ref()
        ));
        let unsigned = layerx_wire::receipt::encode_unsigned(&receipt)
            .unwrap_or_else(|error| panic!("unsigned receipt: {error:?}"));
        let protocol_digest = layerx_wire::hash::receipt_digest(&unsigned)
            .unwrap_or_else(|error| panic!("protocol digest: {error:?}"));
        assert!(!persisted_terminal_receipt_matches(
            &canonical,
            protocol_digest
        ));
        let mut altered = canonical;
        altered[10] ^= 1;
        assert!(!persisted_terminal_receipt_matches(
            &altered,
            terminal.receipt_ref()
        ));
    }
}
