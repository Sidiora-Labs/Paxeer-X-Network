//! Version 1 Agent operation envelope transport over the unified gateway route.
//!
//! The envelope reaches the agent daemon through `POST /v1/agent/rpc`. Gateway API-key
//! admission (`Authorization`) and the tenant session credential carried inside the
//! envelope are two distinct authorities; neither substitutes for the other. A mutating
//! operation whose outcome cannot be established after the request may have left the
//! client is reported as [`EnvelopeError::Unknown`] and is never resent automatically.

use std::fmt;
use std::path::Path;
use std::time::Duration;

use layerx_agent_api::error::{ApiError, ErrorClass, ReasonCode, RequestId, Retriability};
use layerx_agent_api::idempotency::{IdempotentMutation, Key};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::proof::{ProofBundle, ProofBundleTarget, MAX_PROOF_BUNDLE_BYTES};
use layerx_agent_api::read::{
    BatchRef, CheckpointRef, Freshness, ReadRequest, RelativeTo, VerifiedRead,
};
use layerx_agent_api::verify::{ApiSuccess, Level, VerificationStatus};
use layerx_agent_api::Sequence;
use layerx_types::result::ResultCode;
use serde_json::{json, Map, Value};
use url::Url;

use crate::programs::LayerXKeyCredential;
use crate::{Call, Deployment, Operation};

/// External unified-gateway route rewritten by the gateway to the daemon's `POST /rpc`.
pub const AGENT_RPC_ROUTE: &str = "/v1/agent/rpc";
/// The only envelope version this client produces and accepts.
pub const ENVELOPE_VERSION: u64 = 1;
/// Daemon body bound for one envelope.
pub const MAX_ENVELOPE_BYTES: usize = 1_048_576;
/// Operations admitted by their existing bootstrap authority with a null credential.
pub const BOOTSTRAP_OPERATIONS: &[Operation] = &[Operation::AgentRegister, Operation::SessionOpen];
const MAX_HTTP_RESPONSE_BYTES: u64 = 9 * 1_048_576;
const MAX_TENANT_BYTES: usize = 255;
const MAX_REASON_BYTES: usize = 128;
const MAX_TRUST_ANCHOR_BYTES: u64 = 1_048_576;
const SUCCESS_FIELDS: &[&str] = &["request_id", "value", "verification_status"];
const ERROR_FIELDS: &[&str] = &[
    "class",
    "protocol_result_code",
    "retriability",
    "request_id",
    "reason",
];

/// Typed refusal or outcome of one envelope exchange.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum EnvelopeError {
    InvalidEndpoint,
    InvalidTrustAnchors,
    InvalidCredential,
    InvalidRequest,
    /// A session credential is required for every non-bootstrap operation and forbidden
    /// for the bootstrap set.
    CredentialPresence {
        operation: Operation,
    },
    /// An idempotency key is required exactly for mutating operations.
    IdempotencyKeyPresence {
        operation: Operation,
    },
    DeploymentMismatch {
        deployment: Deployment,
    },
    Bounds,
    GatewayAuthentication,
    /// The daemon or gateway answered with the established error envelope.
    Refused(ApiError),
    /// A read failed in transport (no response, non-JSON body, or an edge/gateway
    /// answer); it had no effect and may be re-issued by the caller.
    Transport {
        operation: Operation,
    },
    /// A read returned a well-formed JSON body that violates the exact version 1
    /// success/error schema or names another request. Never re-issued.
    Decode {
        operation: Operation,
    },
    /// A mutation may or may not have taken effect. Reconcile through `track` with the
    /// same idempotency key; never resend automatically.
    Unknown {
        operation: Operation,
    },
}

/// Whether the caller may re-issue an exchange that ended in an [`EnvelopeError`].
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ClientRetriability {
    Safe,
    Never,
}

impl EnvelopeError {
    /// `Safe` only for a read transport failure; every other outcome, including
    /// [`EnvelopeError::Decode`] and [`EnvelopeError::Unknown`], is `Never`.
    #[must_use]
    pub const fn client_retriability(&self) -> ClientRetriability {
        match self {
            Self::Transport { .. } => ClientRetriability::Safe,
            _ => ClientRetriability::Never,
        }
    }
}

/// Session coordinates carried in the envelope `credential` object.
///
/// Bounds match the daemon's `SessionCredential`: tenant is 1..=255 bytes without NUL,
/// session and token identifiers are 32 bytes, generation is a `u64` sent as a canonical
/// decimal string.
#[derive(Clone, Eq, PartialEq)]
pub struct EnvelopeCredential {
    tenant: String,
    session_id: [u8; 32],
    token_id: [u8; 32],
    generation: u64,
}

impl Drop for EnvelopeCredential {
    fn drop(&mut self) {
        zeroize::Zeroize::zeroize(&mut self.token_id);
    }
}
impl fmt::Debug for EnvelopeCredential {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("EnvelopeCredential")
            .field("tenant", &self.tenant)
            .field("session_id", &hex(&self.session_id))
            .field("token_id", &"<redacted>")
            .field("generation", &self.generation)
            .finish()
    }
}

impl EnvelopeCredential {
    /// Binds the full tenant session coordinate.
    ///
    /// # Errors
    ///
    /// Refuses a tenant outside the daemon bounds and all-zero identifiers.
    pub fn new(
        tenant: impl Into<String>,
        session_id: [u8; 32],
        token_id: [u8; 32],
        generation: u64,
    ) -> Result<Self, EnvelopeError> {
        let tenant = tenant.into();
        if tenant.is_empty() || tenant.len() > MAX_TENANT_BYTES || tenant.as_bytes().contains(&0) {
            return Err(EnvelopeError::InvalidCredential);
        }
        if session_id == [0; 32] || token_id == [0; 32] {
            return Err(EnvelopeError::InvalidCredential);
        }
        Ok(Self {
            tenant,
            session_id,
            token_id,
            generation,
        })
    }

    #[must_use]
    pub fn tenant(&self) -> &str {
        &self.tenant
    }

    #[must_use]
    pub const fn session_id(&self) -> [u8; 32] {
        self.session_id
    }

    #[must_use]
    pub const fn generation(&self) -> u64 {
        self.generation
    }

    fn encode(&self) -> Value {
        json!({
            "tenant": self.tenant,
            "session_id": hex(&self.session_id),
            "token_id": hex(&self.token_id),
            "generation": self.generation.to_string(),
        })
    }
}

/// Encodes the exact version 1 envelope body.
///
/// # Errors
///
/// Refuses a non-object request, a credential present for a bootstrap operation or absent
/// for any other operation, and an idempotency key that does not match the operation's
/// generated mutating classification.
pub fn encode_envelope(
    operation: Operation,
    request_id: RequestId,
    request: &Value,
    credential: Option<&EnvelopeCredential>,
    idempotency_key: Option<Key>,
) -> Result<Value, EnvelopeError> {
    if !request.is_object() {
        return Err(EnvelopeError::InvalidRequest);
    }
    if BOOTSTRAP_OPERATIONS.contains(&operation) == credential.is_some() {
        return Err(EnvelopeError::CredentialPresence { operation });
    }
    if operation.mutating() != idempotency_key.is_some() {
        return Err(EnvelopeError::IdempotencyKeyPresence { operation });
    }
    Ok(json!({
        "version": ENVELOPE_VERSION,
        "request_id": request_id.0.to_string(),
        "operation": operation.name(),
        "request": request,
        "credential": credential.map_or(Value::Null, EnvelopeCredential::encode),
        "idempotency_key": idempotency_key.map_or(Value::Null, |key| Value::String(hex(&key.bytes()))),
    }))
}

/// HTTPS envelope client for the unified gateway Agent route.
pub struct AgentEnvelopeTransport {
    agent: ureq::Agent,
    endpoint: Url,
    gateway_key: Option<LayerXKeyCredential>,
    route: &'static str,
}

/// Internal route of the agent daemon RPC mTLS listener.
pub const DAEMON_RPC_ROUTE: &str = "/rpc";

impl AgentEnvelopeTransport {
    pub fn tenant_readiness(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::TenantReadiness>, EnvelopeError> {
        use layerx_agent_api::identity::{TenantReadiness, TenantRecoveryReason};
        let response = self.send_operation(
            Operation::TenantReadiness,
            request_id,
            &json!({}),
            Some(credential),
            None,
        )?;
        let invalid = || EnvelopeError::Decode {
            operation: Operation::TenantReadiness,
        };
        let fields = response.value.as_object().ok_or_else(invalid)?;
        if !exact_fields(
            fields,
            &[
                "transport_ready",
                "verified_reads_ready",
                "writes_admitted",
                "recovery_reason",
            ],
        ) {
            return Err(invalid());
        }
        let recovery_reason = match fields.get("recovery_reason") {
            Some(Value::Null) => None,
            Some(Value::String(reason)) => {
                Some(TenantRecoveryReason::parse(reason).ok_or_else(invalid)?)
            }
            _ => return Err(invalid()),
        };
        let value = TenantReadiness {
            transport_ready: fields
                .get("transport_ready")
                .and_then(Value::as_bool)
                .ok_or_else(invalid)?,
            verified_reads_ready: fields
                .get("verified_reads_ready")
                .and_then(Value::as_bool)
                .ok_or_else(invalid)?,
            writes_admitted: fields
                .get("writes_admitted")
                .and_then(Value::as_bool)
                .ok_or_else(invalid)?,
            recovery_reason,
        };
        value.validate().map_err(|_| invalid())?;
        if response.verification_status != VerificationStatus::Achieved(Level::Unverified) {
            return Err(invalid());
        }
        Ok(native_success(response, value))
    }
    /// Connects to a gateway origin. Only `https` is accepted; redirects are refused.
    ///
    /// `trust_anchors` names an explicit PEM file of trusted roots; when absent the
    /// platform roots are used. An empty or unreadable anchor file is refused.
    ///
    /// # Errors
    ///
    /// Refuses a non-HTTPS endpoint, query/fragment/credentials in the URL, and invalid
    /// or empty trust anchors.
    pub fn connect(
        endpoint: &str,
        gateway_key: Option<LayerXKeyCredential>,
        trust_anchors: Option<&Path>,
    ) -> Result<Self, EnvelopeError> {
        Self::build(endpoint, gateway_key, trust_anchors, None, AGENT_RPC_ROUTE)
    }

    /// Connects directly to the agent daemon RPC listener (`POST /rpc`). Only `https` is
    /// accepted; the client identity comes from `client_cert` and no `Authorization`
    /// header is ever sent, because the daemon refuses principal headers.
    ///
    /// # Errors
    ///
    /// Same as [`Self::connect`].
    pub fn connect_daemon(
        endpoint: &str,
        client_cert: ureq::tls::ClientCert,
        trust_anchors: &Path,
    ) -> Result<Self, EnvelopeError> {
        Self::build(
            endpoint,
            None,
            Some(trust_anchors),
            Some(client_cert),
            DAEMON_RPC_ROUTE,
        )
    }

    fn build(
        endpoint: &str,
        gateway_key: Option<LayerXKeyCredential>,
        trust_anchors: Option<&Path>,
        client_cert: Option<ureq::tls::ClientCert>,
        route: &'static str,
    ) -> Result<Self, EnvelopeError> {
        let endpoint = Url::parse(endpoint).map_err(|_| EnvelopeError::InvalidEndpoint)?;
        if endpoint.scheme() != "https"
            || endpoint.host_str().is_none_or(str::is_empty)
            || !endpoint.username().is_empty()
            || endpoint.password().is_some()
            || endpoint.query().is_some()
            || endpoint.fragment().is_some()
        {
            return Err(EnvelopeError::InvalidEndpoint);
        }
        let roots = match trust_anchors {
            Some(path) => explicit_roots(path)?,
            None => crate::tls::system_roots(endpoint.as_str())
                .map_err(|_| EnvelopeError::InvalidTrustAnchors)?,
        };
        let config = ureq::Agent::config_builder()
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .provider(ureq::tls::TlsProvider::Rustls)
                    .root_certs(roots)
                    .client_cert(client_cert)
                    .build(),
            )
            .timeout_global(Some(Duration::from_secs(30)))
            .http_status_as_error(false)
            .max_redirects(0)
            .build();
        Ok(Self {
            agent: config.into(),
            endpoint,
            gateway_key,
            route,
        })
    }

    fn route(&self) -> Url {
        let mut endpoint = self.endpoint.clone();
        let base = endpoint.path().trim_end_matches('/').to_owned();
        endpoint.set_path(&format!("{base}{}", self.route));
        endpoint
    }

    /// Sends one typed non-mutating catalogue call. `request` is the operation request in
    /// its schema JSON form.
    ///
    /// # Errors
    ///
    /// Refuses a mutating operation (use [`Self::send_mutation`]), a non-daemon call,
    /// returns the established error envelope, or `Transport`.
    pub fn send_query<T>(
        &self,
        call: &Call<T>,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
    ) -> Result<ApiSuccess<Value>, EnvelopeError> {
        daemon_call(call)?;
        if call.operation().mutating() {
            return Err(EnvelopeError::IdempotencyKeyPresence {
                operation: call.operation(),
            });
        }
        self.send_operation(call.operation(), request_id, request, credential, None)
    }

    /// Sends one typed mutating catalogue call. The envelope `request_id` and
    /// `idempotency_key` are taken from the typed [`IdempotentMutation`]; `request` is the
    /// inner operation request in its schema JSON form.
    ///
    /// # Errors
    ///
    /// Returns the established error envelope, or [`EnvelopeError::Unknown`] whenever the
    /// outcome cannot be established after sending. Reconcile with `track`; never resend
    /// automatically.
    pub fn send_mutation<T>(
        &self,
        call: &Call<IdempotentMutation<T>>,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
    ) -> Result<ApiSuccess<Value>, EnvelopeError> {
        daemon_call(call)?;
        let mutation = call.request();
        self.send_operation(
            call.operation(),
            mutation.request_id,
            request,
            credential,
            Some(mutation.key),
        )
    }

    /// Sends one catalogue operation by its generated identity.
    ///
    /// # Errors
    ///
    /// See [`Self::send_query`] and [`Self::send_mutation`].
    pub fn send_operation(
        &self,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
        idempotency_key: Option<Key>,
    ) -> Result<ApiSuccess<Value>, EnvelopeError> {
        let mut received = None;
        self.exchange(
            operation,
            request_id,
            request,
            credential,
            idempotency_key,
            &mut received,
        )
    }

    /// Same as [`Self::send_operation`], also returning the HTTP status and parsed JSON
    /// body when a JSON response was received, for evidence retention.
    pub fn send_operation_recorded(
        &self,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
        idempotency_key: Option<Key>,
    ) -> (
        Option<(u16, Value)>,
        Result<ApiSuccess<Value>, EnvelopeError>,
    ) {
        let mut received = None;
        let outcome = self.exchange(
            operation,
            request_id,
            request,
            credential,
            idempotency_key,
            &mut received,
        );
        (received, outcome)
    }

    pub fn prepare_native(
        &self,
        request_id: RequestId,
        key: Key,
        request: &layerx_agent_api::identity::NativePrepareRequestV1,
        credential: &EnvelopeCredential,
        registry: &layerx_types::payload::ModuleRegistry,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativePrepareResultV1>, EnvelopeError> {
        use sha2::{Digest, Sha256};
        let body = encode_native_prepare(request)?;
        let purpose = &request.purpose.purpose;
        if purpose.tenant.as_str() != credential.tenant()
            || purpose.session_id.to_bytes().ok() != Some(credential.session_id())
            || purpose.generation != credential.generation()
        {
            return Err(EnvelopeError::InvalidCredential);
        }
        let response = self.send_operation(
            Operation::Prepare,
            request_id,
            &body,
            Some(credential),
            Some(key),
        )?;
        let unknown = || EnvelopeError::Unknown {
            operation: Operation::Prepare,
        };
        let value = decode_native_preparation(&response.value).ok_or_else(unknown)?;
        let canonical_digest: [u8; 32] = Sha256::digest(&value.canonical_bytes).into();
        let activity = layerx_wire::activity::decode_unsigned(&value.canonical_bytes, registry)
            .map_err(|_| unknown())?;
        let preimage = layerx_wire::sign::preimage(&activity).map_err(|_| unknown())?;
        if value.preparation_id != purpose.preparation_id
            || canonical_digest != purpose.canonical_digest
            || value.preparation_id != canonical_digest
            || value.activity != request.activity
            || activity.activity_type().value()
                != request
                    .activity
                    .activity_type()
                    .map_err(|_| unknown())?
                    .value()
            || activity.actor_did() != request.actor.as_str().as_bytes()
            || activity.account_sequence() != request.account_sequence
            || activity.timestamp_bound().not_before != request.not_before
            || activity.timestamp_bound().not_after != request.not_after
            || activity.idempotency_key() != request.idempotency_key
            || activity.fee_limit() != request.fee_limit
            || activity.payload() != request.payload.as_slice()
            || activity.payload_hash() != request.payload_hash
            || preimage.as_bytes() != &value.signing_preimage
            || value
                .approval_id
                .is_some_and(|id| id != value.preparation_id)
        {
            return Err(unknown());
        }
        Ok(native_success(response, value))
    }

    pub fn prepare_native_effect(
        &self,
        request_id: RequestId,
        key: Key,
        request: &layerx_agent_api::identity::NativeEffectPrepareRequestV1,
        credential: &EnvelopeCredential,
        registry: &layerx_types::payload::ModuleRegistry,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativePrepareResultV1>, EnvelopeError> {
        use sha2::{Digest, Sha256};
        let body = crate::native_effect::encode_native_effect_prepare(request)?;
        let purpose = &request.purpose.purpose;
        crate::native_effect::validate_request_binding(request, credential, key)?;
        let response = self.send_operation(
            Operation::Prepare,
            request_id,
            &body,
            Some(credential),
            Some(key),
        )?;
        let unknown = || EnvelopeError::Unknown {
            operation: Operation::Prepare,
        };
        let value = decode_native_preparation(&response.value).ok_or_else(unknown)?;
        let canonical_digest: [u8; 32] = Sha256::digest(&value.canonical_bytes).into();
        let activity = layerx_wire::activity::decode_unsigned(&value.canonical_bytes, registry)
            .map_err(|_| unknown())?;
        let preimage = layerx_wire::sign::preimage(&activity).map_err(|_| unknown())?;
        if value.preparation_id != purpose.preparation_id
            || canonical_digest != purpose.canonical_digest
            || value.preparation_id != canonical_digest
            || value.activity != request.activity
            || activity.activity_type().value()
                != request
                    .activity
                    .activity_type()
                    .map_err(|_| unknown())?
                    .value()
            || activity.actor_did() != request.actor.as_str().as_bytes()
            || activity.account_sequence() != request.account_sequence
            || activity.timestamp_bound().not_before != request.not_before
            || activity.timestamp_bound().not_after != request.not_after
            || activity.idempotency_key() != request.idempotency_key
            || activity.fee_limit() != request.fee_limit
            || activity.payload() != request.payload.as_slice()
            || activity.payload_hash() != request.payload_hash
            || preimage.as_bytes() != &value.signing_preimage
            || value
                .approval_id
                .is_some_and(|id| id != value.preparation_id)
        {
            return Err(unknown());
        }
        Ok(native_success(response, value))
    }

    pub fn prepare_native_send(
        &self,
        request_id: RequestId,
        key: Key,
        request: &layerx_agent_api::identity::NativeSendPrepareRequestV1,
        credential: &EnvelopeCredential,
        registry: &layerx_types::payload::ModuleRegistry,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativePrepareResultV1>, EnvelopeError> {
        use sha2::{Digest, Sha256};
        let body = crate::native_effect::encode_native_send_prepare(request)?;
        let purpose = &request.purpose.purpose;
        crate::native_effect::validate_send_request_binding(request, credential, key)?;
        let response = self.send_operation(
            Operation::Prepare,
            request_id,
            &body,
            Some(credential),
            Some(key),
        )?;
        let unknown = || EnvelopeError::Unknown {
            operation: Operation::Prepare,
        };
        let value = decode_native_preparation(&response.value).ok_or_else(unknown)?;
        let canonical_digest: [u8; 32] = Sha256::digest(&value.canonical_bytes).into();
        let activity = layerx_wire::activity::decode_unsigned(&value.canonical_bytes, registry)
            .map_err(|_| unknown())?;
        let preimage = layerx_wire::sign::preimage(&activity).map_err(|_| unknown())?;
        let reencoded = layerx_wire::activity::encode_unsigned(&activity).map_err(|_| unknown())?;
        if value.preparation_id != purpose.preparation_id
            || canonical_digest != purpose.canonical_digest
            || value.preparation_id != canonical_digest
            || value.activity != request.activity
            || activity.protocol_version() != purpose.protocol_version
            || activity.network_id() != purpose.network_id
            || activity.authority() != request.authority.as_bytes()
            || reencoded != value.canonical_bytes
            || activity.activity_type().value()
                != request
                    .activity
                    .activity_type()
                    .map_err(|_| unknown())?
                    .value()
            || activity.actor_did() != request.actor.as_str().as_bytes()
            || activity.account_sequence() != request.account_sequence
            || activity.timestamp_bound().not_before != request.not_before
            || activity.timestamp_bound().not_after != request.not_after
            || activity.idempotency_key() != request.idempotency_key
            || activity.fee_limit() != request.fee_limit
            || activity.payload() != request.payload.as_slice()
            || activity.payload_hash() != request.payload_hash
            || preimage.as_bytes() != &value.signing_preimage
            || value
                .approval_id
                .is_some_and(|id| id != value.preparation_id)
        {
            return Err(unknown());
        }
        Ok(native_success(response, value))
    }

    pub fn approval_list_native(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativeApprovalListResultV1>, EnvelopeError>
    {
        let response = self.send_operation(
            Operation::ApprovalList,
            request_id,
            &json!({"variant":"native_v1"}),
            Some(credential),
            None,
        )?;
        let value = decode_native_approval_list(&response.value).ok_or(EnvelopeError::Decode {
            operation: Operation::ApprovalList,
        })?;
        Ok(native_success(response, value))
    }

    pub fn approval_get_native(
        &self,
        request_id: RequestId,
        request: &layerx_agent_api::identity::NativeApprovalGetV1,
        credential: &EnvelopeCredential,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativeApprovalResultV1>, EnvelopeError> {
        let response = self.send_operation(
            Operation::ApprovalGet,
            request_id,
            &json!({"variant":"native_v1", "approval_id":hex(&request.approval_id)}),
            Some(credential),
            None,
        )?;
        let value = decode_native_approval(&response.value)
            .filter(|value| value.approval_id == request.approval_id)
            .ok_or(EnvelopeError::Decode {
                operation: Operation::ApprovalGet,
            })?;
        Ok(native_success(response, value))
    }

    pub fn approval_decide_native(
        &self,
        request_id: RequestId,
        key: Key,
        request: &layerx_agent_api::identity::NativeApprovalDecisionV1,
        credential: &EnvelopeCredential,
        grant: bool,
    ) -> Result<ApiSuccess<layerx_agent_api::identity::NativeApprovalResultV1>, EnvelopeError> {
        let operation = if grant {
            Operation::ApprovalApprove
        } else {
            Operation::ApprovalReject
        };
        let response = self.send_operation(operation, request_id,
            &json!({"variant":"native_v1", "approval_id":hex(&request.approval_id),
                    "held_digest":hex(&request.held_digest), "current_sequence":request.current_sequence.to_string()}),
            Some(credential), Some(key))?;
        let value = decode_native_approval(&response.value)
            .filter(|value| {
                value.approval_id == request.approval_id && value.held_digest == request.held_digest
            })
            .ok_or(EnvelopeError::Unknown { operation })?;
        Ok(native_success(response, value))
    }

    pub fn read_proof_bundle(
        &self,
        request_id: RequestId,
        request: &ReadRequest<CanonicalBytes>,
        credential: &EnvelopeCredential,
    ) -> Result<ApiSuccess<VerifiedRead<ProofBundle>>, EnvelopeError> {
        let body = encode_proof_bundle_request(request)?;
        let response = self.send_operation(
            Operation::ReadProofBundle,
            request_id,
            &body,
            Some(credential),
            None,
        )?;
        let value =
            decode_proof_bundle_response(request, &response).ok_or(EnvelopeError::Decode {
                operation: Operation::ReadProofBundle,
            })?;
        Ok(native_success(response, value))
    }

    fn exchange(
        &self,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
        idempotency_key: Option<Key>,
        received: &mut Option<(u16, Value)>,
    ) -> Result<ApiSuccess<Value>, EnvelopeError> {
        let proof_request = if operation == Operation::ReadProofBundle {
            Some(decode_proof_bundle_request(request).ok_or(EnvelopeError::InvalidRequest)?)
        } else {
            None
        };
        let envelope =
            encode_envelope(operation, request_id, request, credential, idempotency_key)?;
        let body = serde_json::to_vec(&envelope).map_err(|_| EnvelopeError::InvalidRequest)?;
        if body.len() > MAX_ENVELOPE_BYTES {
            return Err(EnvelopeError::Bounds);
        }
        let authorization = self
            .gateway_key
            .as_ref()
            .map(LayerXKeyCredential::authorization)
            .transpose()
            .map_err(|_| EnvelopeError::GatewayAuthentication)?;
        let ambiguous = || {
            if operation.mutating() {
                EnvelopeError::Unknown { operation }
            } else {
                EnvelopeError::Transport { operation }
            }
        };
        let route = self.route();
        let mut request_builder = self
            .agent
            .post(route.as_str())
            .header("Accept", "application/json")
            .header("Content-Type", "application/json")
            .header(
                "User-Agent",
                concat!("layerx-rust/", env!("CARGO_PKG_VERSION")),
            );
        if let Some(value) = authorization.as_deref() {
            request_builder = request_builder.header("Authorization", value);
        }
        let mut response = request_builder
            .send(body.as_slice())
            .map_err(|_| ambiguous())?;
        let malformed = || {
            if operation.mutating() {
                EnvelopeError::Unknown { operation }
            } else {
                EnvelopeError::Transport { operation }
            }
        };
        let status = response.status().as_u16();
        let json_body = response
            .headers()
            .get("content-type")
            .and_then(|value| value.to_str().ok())
            .and_then(|value| value.split(';').next())
            .is_some_and(|value| value.trim() == "application/json");
        if !json_body {
            return Err(malformed());
        }
        let response_limit = if proof_request.is_some() {
            MAX_ENVELOPE_BYTES as u64 + 1
        } else {
            MAX_HTTP_RESPONSE_BYTES
        };
        let encoded = response
            .body_mut()
            .with_config()
            .limit(response_limit)
            .read_to_vec()
            .map_err(|_| ambiguous())?;
        if proof_request.is_some() && encoded.len() > MAX_ENVELOPE_BYTES {
            return Err(EnvelopeError::Decode { operation });
        }
        let document: Value = serde_json::from_slice(&encoded).map_err(|_| malformed())?;
        *received = Some((status, document.clone()));
        let edge = matches!(status, 502..=504)
            || document
                .as_object()
                .is_some_and(|object| object.contains_key("ok"));
        let schema_violation = || {
            if operation.mutating() {
                EnvelopeError::Unknown { operation }
            } else if edge {
                EnvelopeError::Transport { operation }
            } else {
                EnvelopeError::Decode { operation }
            }
        };
        let decoded = decode_response(status, &document).ok_or_else(schema_violation)?;
        let received = match &decoded {
            Ok(success) => success.request_id,
            // "0" is the daemon's request_id for an envelope it could not parse.
            Err(error) if error.request_id == RequestId(0) => request_id,
            Err(error) => error.request_id,
        };
        if received != request_id {
            return Err(if operation.mutating() {
                EnvelopeError::Unknown { operation }
            } else {
                EnvelopeError::Decode { operation }
            });
        }
        let success = decoded.map_err(EnvelopeError::Refused)?;
        if let Some(request) = proof_request.as_ref() {
            decode_proof_bundle_response(request, &success)
                .ok_or(EnvelopeError::Decode { operation })?;
        }
        Ok(success)
    }
}

fn daemon_call<T>(call: &Call<T>) -> Result<(), EnvelopeError> {
    if call.deployment() == Deployment::Daemon {
        Ok(())
    } else {
        Err(EnvelopeError::DeploymentMismatch {
            deployment: call.deployment(),
        })
    }
}

fn explicit_roots(path: &Path) -> Result<ureq::tls::RootCerts, EnvelopeError> {
    let metadata = std::fs::metadata(path).map_err(|_| EnvelopeError::InvalidTrustAnchors)?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > MAX_TRUST_ANCHOR_BYTES {
        return Err(EnvelopeError::InvalidTrustAnchors);
    }
    let pem = std::fs::read(path).map_err(|_| EnvelopeError::InvalidTrustAnchors)?;
    let mut certificates = Vec::new();
    for item in ureq::tls::parse_pem(&pem) {
        match item.map_err(|_| EnvelopeError::InvalidTrustAnchors)? {
            ureq::tls::PemItem::Certificate(certificate) => {
                certificates.push(certificate.to_owned());
            }
            _ => return Err(EnvelopeError::InvalidTrustAnchors),
        }
    }
    if certificates.is_empty() {
        return Err(EnvelopeError::InvalidTrustAnchors);
    }
    Ok(ureq::tls::RootCerts::new_with_certs(&certificates))
}

/// Decodes a version 1 response document. `None` means the document is not a valid
/// success or error envelope for the given status.
#[must_use]
pub fn decode_response(
    status: u16,
    document: &Value,
) -> Option<Result<ApiSuccess<Value>, ApiError>> {
    let envelope = document.as_object()?;
    if status == 200 {
        if !exact_fields(envelope, SUCCESS_FIELDS) {
            return None;
        }
        let request_id = request_id(envelope.get("request_id")?)?;
        let verification_status = verification_status(envelope.get("verification_status")?)?;
        return Some(Ok(ApiSuccess {
            request_id,
            value: envelope.get("value")?.clone(),
            verification_status,
        }));
    }
    if !exact_fields(envelope, ERROR_FIELDS) {
        return None;
    }
    let class = match envelope.get("class")?.as_str()? {
        "TransportFailure" => ErrorClass::TransportFailure,
        "Deadline" => ErrorClass::Deadline,
        "ProtocolIncompatibility" => ErrorClass::ProtocolIncompatibility,
        "UnavailableCapability" => ErrorClass::UnavailableCapability,
        "CoreRejection" => ErrorClass::CoreRejection,
        "VerificationFailure" => ErrorClass::VerificationFailure,
        "PolicyRefusal" => ErrorClass::PolicyRefusal,
        "CapabilityRefusal" => ErrorClass::CapabilityRefusal,
        "BudgetRefusal" => ErrorClass::BudgetRefusal,
        "RateLimit" => ErrorClass::RateLimit,
        "IdempotencyConflict" => ErrorClass::IdempotencyConflict,
        "InternalFault" => ErrorClass::InternalFault,
        _ => return None,
    };
    let retriability = match envelope.get("retriability")?.as_str()? {
        "Terminal" => Retriability::Terminal,
        "Retriable" => Retriability::Retriable,
        _ => return None,
    };
    let protocol_result_code = match envelope.get("protocol_result_code")? {
        Value::Null => None,
        Value::Number(number) => Some(ResultCode::from_raw(i32::try_from(number.as_i64()?).ok()?)),
        _ => return None,
    };
    Some(Err(ApiError {
        class,
        protocol_result_code,
        retriability,
        request_id: request_id(envelope.get("request_id")?)?,
        reason: reason(envelope.get("reason")?)?,
    }))
}

fn verification_status(value: &Value) -> Option<VerificationStatus> {
    let status = value.as_object()?;
    match status.get("state")?.as_str()? {
        "achieved" if exact_fields(status, &["state", "level"]) => {
            Some(VerificationStatus::Achieved(level(status.get("level")?)?))
        }
        "unverified" if exact_fields(status, &["state", "requested", "achieved", "reason"]) => {
            let requested = level(status.get("requested")?)?;
            let achieved = level(status.get("achieved")?)?;
            if achieved >= requested {
                return None;
            }
            Some(VerificationStatus::Unverified {
                requested,
                achieved,
                reason: reason(status.get("reason")?)?,
            })
        }
        _ => None,
    }
}

fn level(value: &Value) -> Option<Level> {
    Some(match value.as_str()? {
        "Unverified" => Level::Unverified,
        "SequencerSigned" => Level::SequencerSigned,
        "BatchIncluded" => Level::BatchIncluded,
        "StateProven" => Level::StateProven,
        "CheckpointFinalised" => Level::CheckpointFinalised,
        "SettlementAnchored" => Level::SettlementAnchored,
        _ => return None,
    })
}

fn request_id(value: &Value) -> Option<RequestId> {
    canonical_u64(value.as_str()?).map(RequestId)
}

fn reason(value: &Value) -> Option<ReasonCode> {
    let text = value.as_str()?;
    if text.len() > MAX_REASON_BYTES {
        return None;
    }
    ReasonCode::new(text).ok()
}

/// Parses a canonical decimal `u64`: digits only, no sign, no leading zero except `0`.
#[must_use]
pub fn canonical_u64(text: &str) -> Option<u64> {
    if text.is_empty()
        || !text.bytes().all(|byte| byte.is_ascii_digit())
        || (text.len() > 1 && text.starts_with('0'))
    {
        return None;
    }
    text.parse().ok()
}

fn exact_fields(value: &Map<String, Value>, fields: &[&str]) -> bool {
    value.len() == fields.len() && fields.iter().all(|field| value.contains_key(*field))
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes
        .iter()
        .fold(String::with_capacity(bytes.len() * 2), |mut out, byte| {
            let _ = write!(out, "{byte:02x}");
            out
        })
}

pub fn encode_native_prepare(
    request: &layerx_agent_api::identity::NativePrepareRequestV1,
) -> Result<Value, EnvelopeError> {
    request
        .clone()
        .validate()
        .map_err(|_| EnvelopeError::InvalidRequest)?;
    let signed = &request.purpose;
    let purpose = &signed.purpose;
    Ok(json!({
        "variant": "native_v1",
        "activity": native_activity_json(request.activity),
        "actor": request.actor.as_str(), "authority": request.authority,
        "account_sequence": request.account_sequence.to_string(),
        "not_before": request.not_before.to_string(), "not_after": request.not_after.to_string(),
        "idempotency_key": hex(&request.idempotency_key), "fee_limit": request.fee_limit.to_string(),
        "payload": hex(&request.payload), "payload_hash": hex(&request.payload_hash),
        "capability_id": request.capability_id.as_str(),
        "purpose": {
            "purpose": {"version":"1", "tenant":purpose.tenant.as_str(),
                "agent_did":purpose.agent_did.as_str(), "session_id":purpose.session_id.as_str(),
                "generation":purpose.generation.to_string(), "expires_at_ms":purpose.expires_at_ms.to_string(),
                "capability_id":purpose.capability_id.as_str(), "preparation_id":hex(&purpose.preparation_id),
                "canonical_digest":hex(&purpose.canonical_digest), "commitment":hex(&purpose.commitment)},
            "owner_public_key":hex(&signed.owner_public_key), "signature":hex(&signed.signature)
        },
        "local_grant": request.local_grant.as_ref().map(|grant| json!({
            "version":"1", "capability":hex(&grant.capability), "session_scope":hex(&grant.session_scope),
            "expires_at_ms":grant.expires_at_ms.to_string(), "owner_public_key":hex(&grant.owner_public_key),
            "signature":hex(&grant.signature)
        }))
    }))
}

fn native_activity_json(activity: layerx_agent_api::identity::NativeActivity) -> Value {
    json!({"version":"1", "module":activity.module.to_string(), "ordinal":activity.ordinal.to_string()})
}

fn native_activity(value: &Value) -> Option<layerx_agent_api::identity::NativeActivity> {
    let value = value.as_object()?;
    if !exact_fields(value, &["version", "module", "ordinal"])
        || value.get("version")?.as_str()? != "1"
    {
        return None;
    }
    layerx_agent_api::identity::NativeActivity::new(
        u16::try_from(canonical_u64(value.get("module")?.as_str()?)?).ok()?,
        u16::try_from(canonical_u64(value.get("ordinal")?.as_str()?)?).ok()?,
    )
    .ok()
}

fn native_hex(value: &Value, maximum: usize) -> Option<Vec<u8>> {
    let text = value.as_str()?;
    if text.is_empty()
        || text.len() % 2 != 0
        || text.len() / 2 > maximum
        || !text
            .bytes()
            .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
    {
        return None;
    }
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| u8::from_str_radix(std::str::from_utf8(pair).ok()?, 16).ok())
        .collect()
}

fn native_id(value: &Value) -> Option<[u8; 32]> {
    native_hex(value, 32)?.try_into().ok()
}

fn native_optional_id(value: &Value) -> Option<Option<[u8; 32]>> {
    if value.is_null() {
        Some(None)
    } else {
        native_id(value).map(Some)
    }
}

pub fn decode_native_preparation(
    value: &Value,
) -> Option<layerx_agent_api::identity::NativePrepareResultV1> {
    let value = value.as_object()?;
    if !exact_fields(
        value,
        &[
            "version",
            "preparation_id",
            "canonical_bytes",
            "signing_preimage",
            "activity",
            "approval_required",
            "approval_id",
        ],
    ) || value.get("version")?.as_str()? != "1"
    {
        return None;
    }
    let approval_required = value.get("approval_required")?.as_bool()?;
    let approval_id = native_optional_id(value.get("approval_id")?)?;
    if approval_required != approval_id.is_some() {
        return None;
    }
    Some(layerx_agent_api::identity::NativePrepareResultV1 {
        preparation_id: native_id(value.get("preparation_id")?)?,
        canonical_bytes: native_hex(
            value.get("canonical_bytes")?,
            layerx_wire::limits::MAX_MESSAGE_BYTES,
        )?,
        signing_preimage: native_id(value.get("signing_preimage")?)?,
        activity: native_activity(value.get("activity")?)?,
        approval_required,
        approval_id,
    })
}

pub fn decode_native_approval(
    value: &Value,
) -> Option<layerx_agent_api::identity::NativeApprovalResultV1> {
    let value = value.as_object()?;
    if !exact_fields(
        value,
        &[
            "version",
            "approval_id",
            "held_digest",
            "activity",
            "state",
            "submission_ref",
        ],
    ) || value.get("version")?.as_str()? != "1"
    {
        return None;
    }
    let state = value.get("state")?.as_str()?;
    if !matches!(
        state,
        "Awaiting" | "Granted" | "Rejected" | "Expired" | "Defective" | "NotRequired"
    ) {
        return None;
    }
    Some(layerx_agent_api::identity::NativeApprovalResultV1 {
        approval_id: native_id(value.get("approval_id")?)?,
        held_digest: native_id(value.get("held_digest")?)?,
        activity: native_activity(value.get("activity")?)?,
        state: state.to_owned(),
        submission_ref: native_optional_id(value.get("submission_ref")?)?,
    })
}

pub fn decode_native_approval_list(
    value: &Value,
) -> Option<layerx_agent_api::identity::NativeApprovalListResultV1> {
    let value = value.as_object()?;
    if !exact_fields(value, &["version", "approvals"]) || value.get("version")?.as_str()? != "1" {
        return None;
    }
    let approvals = value.get("approvals")?.as_array()?;
    if approvals.len() > 100 {
        return None;
    }
    Some(layerx_agent_api::identity::NativeApprovalListResultV1 {
        approvals: approvals
            .iter()
            .map(decode_native_approval)
            .collect::<Option<Vec<_>>>()?,
    })
}

fn native_success<T>(response: ApiSuccess<Value>, value: T) -> ApiSuccess<T> {
    ApiSuccess {
        request_id: response.request_id,
        value,
        verification_status: response.verification_status,
    }
}

pub fn encode_proof_bundle_request(
    request: &ReadRequest<CanonicalBytes>,
) -> Result<Value, EnvelopeError> {
    ProofBundleTarget::decode(request.selector.as_bytes())
        .map_err(|_| EnvelopeError::InvalidRequest)?;
    Ok(json!({
        "target": hex(request.selector.as_bytes()),
        "requested_verification_level": format!("{:?}", request.requested_verification_level),
    }))
}

pub fn decode_proof_bundle_request(value: &Value) -> Option<ReadRequest<CanonicalBytes>> {
    let request = value.as_object()?;
    if !exact_fields(request, &["target", "requested_verification_level"]) {
        return None;
    }
    let bytes = native_hex(request.get("target")?, 67)?;
    ProofBundleTarget::decode(&bytes).ok()?;
    Some(ReadRequest {
        selector: CanonicalBytes::new(bytes).ok()?,
        requested_verification_level: level(request.get("requested_verification_level")?)?,
    })
}

pub fn decode_proof_bundle_response(
    request: &ReadRequest<CanonicalBytes>,
    response: &ApiSuccess<Value>,
) -> Option<VerifiedRead<ProofBundle>> {
    let read = response.value.as_object()?;
    if !exact_fields(read, &["value", "achieved_verification_level", "freshness"]) {
        return None;
    }
    let achieved = level(read.get("achieved_verification_level")?)?;
    if response.verification_status != VerificationStatus::Achieved(achieved) {
        return None;
    }
    let bundle = read.get("value")?.as_object()?;
    if !exact_fields(bundle, &["target", "proofs"]) {
        return None;
    }
    let target = CanonicalBytes::new(native_hex(bundle.get("target")?, 67)?).ok()?;
    let proofs = bundle.get("proofs")?.as_array()?;
    if proofs.len() != 1 {
        return None;
    }
    let proof = CanonicalBytes::new(native_hex(&proofs[0], MAX_PROOF_BUNDLE_BYTES)?).ok()?;
    let freshness = read.get("freshness")?.as_object()?;
    if !exact_fields(
        freshness,
        &[
            "chain_head",
            "latest_sealed_batch",
            "latest_finalised_checkpoint",
            "value_sequence",
            "relative_to",
        ],
    ) {
        return None;
    }
    let chain_head = canonical_u64(freshness.get("chain_head")?.as_str()?)?;
    let value_sequence = canonical_u64(freshness.get("value_sequence")?.as_str()?)?;
    let sealed = freshness.get("latest_sealed_batch")?.as_str()?;
    let sealed_number = canonical_u64(sealed)?;
    let checkpoint = freshness.get("latest_finalised_checkpoint")?;
    native_id(checkpoint)?;
    let relative = freshness.get("relative_to")?.as_object()?;
    if !exact_fields(relative, &["batch"]) {
        return None;
    }
    let batch = relative.get("batch")?.as_str()?;
    let batch_number = canonical_u64(batch)?;
    if chain_head == 0
        || value_sequence == 0
        || sealed_number == 0
        || batch_number == 0
        || value_sequence > chain_head
        || batch_number > sealed_number
    {
        return None;
    }
    let result = VerifiedRead::new(
        ProofBundle {
            target,
            proofs: vec![proof],
        },
        achieved,
        Freshness {
            chain_head: Sequence(chain_head),
            latest_sealed_batch: BatchRef::new(sealed).ok()?,
            latest_finalised_checkpoint: CheckpointRef::new(checkpoint.as_str()?).ok()?,
            value_sequence: Sequence(value_sequence),
            relative_to: RelativeTo::Batch(BatchRef::new(batch).ok()?),
        },
    );
    ProofBundle::check_response(request, &result).ok()?;
    Some(result)
}
