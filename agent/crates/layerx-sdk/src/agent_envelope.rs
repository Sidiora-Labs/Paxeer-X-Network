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

use layerx_agent_api::error::{
    ApiError, ApiSuccess, ErrorClass, IdempotentMutation, Key, Level, ReasonCode, RequestId,
    Retriability, VerificationStatus,
};
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
    CredentialPresence { operation: Operation },
    /// An idempotency key is required exactly for mutating operations.
    IdempotencyKeyPresence { operation: Operation },
    DeploymentMismatch { deployment: Deployment },
    Bounds,
    GatewayAuthentication,
    /// The daemon or gateway answered with the established error envelope.
    Refused(ApiError),
    /// A read failed in transport; it had no effect and may be re-issued by the caller.
    Transport { operation: Operation },
    /// A read returned a body that is not a valid version 1 response.
    Decode { operation: Operation },
    /// A mutation may or may not have taken effect. Reconcile through `track` with the
    /// same idempotency key; never resend automatically.
    Unknown { operation: Operation },
    /// The response is bound to a different request than the one sent.
    RequestIdMismatch { sent: RequestId, received: RequestId },
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
        if tenant.is_empty() || tenant.len() > MAX_TENANT_BYTES || tenant.as_bytes().contains(&0)
        {
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
}

impl AgentEnvelopeTransport {
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
        })
    }

    fn route(&self) -> Url {
        let mut endpoint = self.endpoint.clone();
        let base = endpoint.path().trim_end_matches('/').to_owned();
        endpoint.set_path(&format!("{base}{AGENT_RPC_ROUTE}"));
        endpoint
    }

    /// Sends one typed non-mutating catalogue call. `request` is the operation request in
    /// its schema JSON form.
    ///
    /// # Errors
    ///
    /// Refuses a mutating operation (use [`Self::send_mutation`]), a non-daemon call,
    /// returns the established error envelope, or `Transport`/`Decode`.
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

    fn exchange(
        &self,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
        idempotency_key: Option<Key>,
        received: &mut Option<(u16, Value)>,
    ) -> Result<ApiSuccess<Value>, EnvelopeError> {
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
                EnvelopeError::Decode { operation }
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
        let encoded = response
            .body_mut()
            .with_config()
            .limit(MAX_HTTP_RESPONSE_BYTES)
            .read_to_vec()
            .map_err(|_| ambiguous())?;
        let document: Value = serde_json::from_slice(&encoded).map_err(|_| malformed())?;
        *received = Some((status, document.clone()));
        let decoded = decode_response(status, &document).ok_or_else(malformed)?;
        let received = match &decoded {
            Ok(success) => success.request_id,
            // "0" is the daemon's request_id for an envelope it could not parse.
            Err(error) if error.request_id == RequestId(0) => request_id,
            Err(error) => error.request_id,
        };
        if received != request_id {
            return Err(EnvelopeError::RequestIdMismatch {
                sent: request_id,
                received,
            });
        }
        decoded.map_err(EnvelopeError::Refused)
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
pub fn decode_response(status: u16, document: &Value) -> Option<Result<ApiSuccess<Value>, ApiError>> {
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
        Value::Number(number) => Some(ResultCode::from_raw(
            i32::try_from(number.as_i64()?).ok()?,
        )),
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
    bytes.iter().fold(String::with_capacity(bytes.len() * 2), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}
