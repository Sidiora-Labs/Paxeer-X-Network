//! Version 1 Agent HTTP operation envelope: bounded decode, exact credential coordinates and
//! session authorization ordering ahead of any owner effect.

use layerx_agent_api::error::{ApiError, ErrorClass, ReasonCode, RequestId, Retriability};
use layerx_agent_api::verify::{Level, VerificationStatus};
use serde::Deserialize;

use crate::agent_rpc_peer;
use crate::agent_rpc_dispatch::{
    canonical_request_bytes, dispatch_operation, DispatchContext, Dispatched,
};
use crate::degraded::Mode;
use crate::idempotency::{EconomicResult, IdempotencyError, Outcome};
use crate::human_runtime::{HumanAuthorityBoundary, SharedAgentOwner};

use crate::session::{SessionCredential, SessionId};
use crate::session_control::{OperationPermit, SessionControl, SessionControlError};
use crate::store::TenantId;
use crate::tenant::{self, surface_for, AuthorizationError, ObjectOwner, Operation, Surface};

pub const ENVELOPE_VERSION: u8 = 1;
pub const MAX_BODY_BYTES: usize = 1_048_576;
const BOOTSTRAP_OPERATIONS: &[Operation] = &[Operation::AgentRegister, Operation::SessionOpen];
const RETIRED_OPERATIONS: &[&str] = &["faucet.claim"];

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WireEnvelope {
    version: u8,
    request_id: String,
    operation: String,
    #[serde(default)]
    idempotency_key: Option<String>,
    request: serde_json::Map<String, serde_json::Value>,
    credential: Option<WireCredential>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WireCredential {
    tenant: String,
    session_id: String,
    token_id: String,
    generation: String,
}

/// A decoded envelope whose coordinates passed every bound but carry no authority yet.
pub struct Envelope {
    pub request_id: RequestId,
    pub operation: Operation,
    pub idempotency_key: Option<[u8; 32]>,
    pub request: serde_json::Map<String, serde_json::Value>,
    /// `None` only for the bootstrap operations, whose existing bootstrap authority decides.
    pub credential: Option<SessionCredential>,
}

/// Rejection raised before or during authorization, rendered as the established error envelope.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Rejection {
    pub class: ErrorClass,
    pub retriability: Retriability,
    pub request_id: RequestId,
    pub reason: &'static str,
}

impl Rejection {
    pub(crate) const fn new(class: ErrorClass, request_id: RequestId, reason: &'static str) -> Self {
        Self {
            class,
            retriability: Retriability::Terminal,
            request_id,
            reason,
        }
    }

    /// Returns the established API error, or `None` if the reason is not machine readable.
    #[must_use]
    pub fn into_api_error(self) -> Option<ApiError> {
        Some(ApiError {
            class: self.class,
            protocol_result_code: None,
            retriability: self.retriability,
            request_id: self.request_id,
            reason: ReasonCode::new(self.reason).ok()?,
        })
    }
}

fn parse_decimal_u64(value: &str) -> Option<u64> {
    let canonical = value == "0"
        || (!value.is_empty()
            && value.len() <= 20
            && value.bytes().all(|byte| byte.is_ascii_digit())
            && !value.starts_with('0'));
    if !canonical {
        return None;
    }
    value.parse().ok()
}

fn parse_hex32(value: &str) -> Option<[u8; 32]> {
    if value.len() != 64
        || !value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
    {
        return None;
    }
    let mut out = [0u8; 32];
    for (index, slot) in out.iter_mut().enumerate() {
        *slot = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16).ok()?;
    }
    Some(out)
}

fn lookup(name: &str) -> Option<Operation> {
    Operation::ALL
        .iter()
        .copied()
        .find(|operation| operation.name() == name)
}

/// Decodes one bounded version 1 envelope.
///
/// # Errors
/// Returns a [`Rejection`] for an oversized, malformed, non-UTF-8 or truncated body, unknown
/// version, field or operation, non-canonical integer, malformed credential coordinate, a
/// missing idempotency key on a mutating operation, one supplied on a non-mutating operation,
/// and `UnavailableCapability` for a retired operation.
pub fn decode(body: &[u8]) -> Result<Envelope, Rejection> {
    let unknown = RequestId(0);
    if body.len() > MAX_BODY_BYTES {
        return Err(Rejection::new(ErrorClass::ProtocolIncompatibility, unknown, "envelope.oversized"));
    }
    let wire: WireEnvelope = serde_json::from_slice(body).map_err(|error| {
        let reason = if error.is_data() && error.to_string().starts_with("unknown field") {
            "envelope.unknown_field"
        } else {
            "envelope.malformed"
        };
        Rejection::new(ErrorClass::ProtocolIncompatibility, unknown, reason)
    })?;
    let request_id = RequestId(parse_decimal_u64(&wire.request_id).ok_or_else(|| {
        Rejection::new(ErrorClass::ProtocolIncompatibility, unknown, "envelope.noncanonical_integer")
    })?);
    if wire.version != ENVELOPE_VERSION {
        return Err(Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.version"));
    }
    if RETIRED_OPERATIONS.contains(&wire.operation.as_str()) {
        return Err(Rejection::new(
            ErrorClass::UnavailableCapability,
            request_id,
            "unavailable_capability.faucet.claim",
        ));
    }
    let operation = lookup(&wire.operation).ok_or_else(|| {
        Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.unknown_operation")
    })?;
    let idempotency_key = match (operation.mutating(), wire.idempotency_key.as_deref()) {
        (true, Some(text)) => Some(
            parse_hex32(text)
                .filter(|key| *key != [0; 32])
                .ok_or_else(|| Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.credential"))?,
        ),
        (true, None) | (false, Some(_)) => {
            return Err(Rejection::new(ErrorClass::IdempotencyConflict, request_id, "envelope.idempotency_key"));
        }
        (false, None) => None,
    };
    let malformed_credential =
        || Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.credential");
    let bootstrap = BOOTSTRAP_OPERATIONS.contains(&operation);
    let credential = match (bootstrap, wire.credential) {
        (true, None) => None,
        (false, Some(credential)) => {
            let tenant = TenantId::new(credential.tenant).map_err(|_| malformed_credential())?;
            let session_id =
                SessionId(parse_hex32(&credential.session_id).ok_or_else(malformed_credential)?);
            let token_id = parse_hex32(&credential.token_id).ok_or_else(malformed_credential)?;
            let generation = parse_decimal_u64(&credential.generation).ok_or_else(|| {
                Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.noncanonical_integer")
            })?;
            Some(SessionCredential::new(tenant, session_id, token_id, generation))
        }
        _ => return Err(malformed_credential()),
    };
    Ok(Envelope {
        request_id,
        operation,
        idempotency_key,
        request: wire.request,
        credential,
    })
}

fn request_text<'a>(
    envelope: &'a Envelope,
    field: &str,
) -> Result<Option<&'a str>, Rejection> {
    match envelope.request.get(field) {
        None => Ok(None),
        Some(serde_json::Value::String(text)) => Ok(Some(text)),
        Some(_) => Err(Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            envelope.request_id,
            "envelope.malformed",
        )),
    }
}

/// Runs the common session authorization for a non-bootstrap envelope, then rejects any
/// caller tenant or agent coordinate inside the operation request that differs from the
/// authenticated principal. The returned permit must be held through every owner effect.
///
/// # Errors
/// Returns a [`Rejection`] for a bootstrap envelope, a caller-coordinate mismatch or any
/// authorization failure.
pub fn authorize(
    control: &SessionControl,
    envelope: &Envelope,
    surface: Surface,
    core_sequence: u64,
    target_owner: Option<ObjectOwner>,
) -> Result<OperationPermit, Rejection> {
    let request_id = envelope.request_id;
    let credential = envelope.credential.as_ref().ok_or_else(|| {
        Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "envelope.credential")
    })?;
    let mismatch =
        || Rejection::new(ErrorClass::PolicyRefusal, request_id, "envelope.coordinate_mismatch");
    let supplied_tenant = request_text(envelope, "tenant")?
        .map(|text| TenantId::new(text).map_err(|_| mismatch()))
        .transpose()?;
    tenant::require_caller_coordinates(credential.tenant(), None, supplied_tenant.as_ref())
        .map_err(|_| mismatch())?;
    tenant::require_session_credential(envelope.operation, envelope.credential.as_ref())
        .map_err(|error| authorization_rejection(request_id, &SessionControlError::Authorization(error)))?;
    let permit = control
        .authorize(credential, envelope.operation, surface, core_sequence, target_owner)
        .map_err(|error| authorization_rejection(request_id, &error))?;
    if let Some(agent) = request_text(envelope, "agent")? {
        if permit.principal().agent.as_bytes() != agent.as_bytes() {
            return Err(mismatch());
        }
    }
    Ok(permit)
}

/// Rejects any principal-bearing header reaching the daemon; credentials travel only in the
/// bounded body credential object.
///
/// # Errors
/// Returns `PolicyRefusal` `envelope.header_principal` when such a header is present.
pub fn reject_header_principal<'a>(
    header_names: impl IntoIterator<Item = &'a str>,
) -> Result<(), Rejection> {
    for name in header_names {
        if ["layerx-tenant", "layerx-agent", "authorization"]
            .iter()
            .any(|forbidden| name.eq_ignore_ascii_case(forbidden))
        {
            return Err(Rejection::new(ErrorClass::PolicyRefusal, RequestId(0), "envelope.header_principal"));
        }
    }
    Ok(())
}

fn authorization_rejection(request_id: RequestId, error: &SessionControlError) -> Rejection {
    match error {
        SessionControlError::Authorization(AuthorizationError::ScopeDenied) => {
            Rejection::new(ErrorClass::CapabilityRefusal, request_id, "session.scope_denied")
        }
        SessionControlError::Authorization(AuthorizationError::Expired) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.expired")
        }
        SessionControlError::Authorization(AuthorizationError::Revoked) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.revoked")
        }
        SessionControlError::Authorization(AuthorizationError::InvalidRequest) => {
            Rejection::new(ErrorClass::ProtocolIncompatibility, request_id, "session.invalid_request")
        }
        SessionControlError::Unavailable => Rejection {
            class: ErrorClass::InternalFault,
            retriability: Retriability::Retriable,
            request_id,
            reason: "unavailable",
        },
        SessionControlError::Session(_) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.not_authorized")
        }
        _ => Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.not_authorized"),
    }
}

const fn class_name(class: ErrorClass) -> &'static str {
    match class {
        ErrorClass::TransportFailure => "TransportFailure",
        ErrorClass::Deadline => "Deadline",
        ErrorClass::ProtocolIncompatibility => "ProtocolIncompatibility",
        ErrorClass::UnavailableCapability => "UnavailableCapability",
        ErrorClass::CoreRejection => "CoreRejection",
        ErrorClass::VerificationFailure => "VerificationFailure",
        ErrorClass::PolicyRefusal => "PolicyRefusal",
        ErrorClass::CapabilityRefusal => "CapabilityRefusal",
        ErrorClass::BudgetRefusal => "BudgetRefusal",
        ErrorClass::RateLimit => "RateLimit",
        ErrorClass::IdempotencyConflict => "IdempotencyConflict",
        ErrorClass::InternalFault => "InternalFault",
    }
}

/// HTTP status for a rejection; credential authentication failures are 401.
#[must_use]
pub fn http_status(rejection: &Rejection) -> u16 {
    match rejection.class {
        ErrorClass::ProtocolIncompatibility => match rejection.reason {
            "envelope.oversized" => 413,
            "envelope.unknown_operation" => 404,
            _ => 400,
        },
        ErrorClass::PolicyRefusal if rejection.reason == "session.not_authorized" => 401,
        ErrorClass::PolicyRefusal | ErrorClass::CapabilityRefusal | ErrorClass::BudgetRefusal => 403,
        ErrorClass::IdempotencyConflict => 409,
        ErrorClass::UnavailableCapability => 503,
        _ => 500,
    }
}

/// Renders a rejection as the established JSON error envelope.
#[must_use]
pub fn error_body(rejection: &Rejection) -> Vec<u8> {
    serde_json::json!({
        "class": class_name(rejection.class),
        "protocol_result_code": serde_json::Value::Null,
        "retriability": match rejection.retriability {
            Retriability::Terminal => "Terminal",
            Retriability::Retriable => "Retriable",
        },
        "request_id": rejection.request_id.0.to_string(),
        "reason": rejection.reason,
    })
    .to_string()
    .into_bytes()
}

/// One rendered daemon response: the HTTP status and the exact JSON body.
pub struct AgentRpcResponse {
    pub status: u16,
    pub body: Vec<u8>,
}

/// Renders an HTTP framing refusal raised before a body reaches the dispatcher.
#[must_use]
pub fn refusal(status: u16, class: ErrorClass, reason: &'static str) -> AgentRpcResponse {
    AgentRpcResponse {
        status,
        body: error_body(&Rejection::new(class, RequestId(0), reason)),
    }
}

const fn level_name(level: Level) -> &'static str {
    match level {
        Level::Unverified => "Unverified",
        Level::SequencerSigned => "SequencerSigned",
        Level::BatchIncluded => "BatchIncluded",
        Level::StateProven => "StateProven",
        Level::CheckpointFinalised => "CheckpointFinalised",
        Level::SettlementAnchored => "SettlementAnchored",
    }
}

const LEVELS: [Level; 6] = [
    Level::Unverified,
    Level::SequencerSigned,
    Level::BatchIncluded,
    Level::StateProven,
    Level::CheckpointFinalised,
    Level::SettlementAnchored,
];

/// `None` when the request declares no level; `Some(None)` when the declared level is not a
/// level name.
fn requested_level(
    request: &serde_json::Map<String, serde_json::Value>,
) -> Option<Option<Level>> {
    let value = request.get("requested_verification_level")?;
    Some(value.as_str().and_then(|name| {
        LEVELS.into_iter().find(|level| level_name(*level) == name)
    }))
}

fn verification_json(
    request_id: RequestId,
    status: Option<&VerificationStatus>,
    requested: Option<Option<Level>>,
) -> Result<serde_json::Value, Rejection> {
    let Some(status) = status else {
        return match requested {
            None => Ok(serde_json::json!({
                "state": "achieved",
                "level": level_name(Level::Unverified),
            })),
            Some(Some(requested)) if requested > Level::Unverified => Ok(serde_json::json!({
                "state": "unverified",
                "requested": level_name(requested),
                "achieved": level_name(Level::Unverified),
                "reason": "owner_payload_carries_no_level",
            })),
            Some(_) => Err(Rejection::new(
                ErrorClass::VerificationFailure,
                request_id,
                "verification.inconsistent",
            )),
        };
    };
    match status {
        VerificationStatus::Achieved(level) => Ok(serde_json::json!({
            "state": "achieved",
            "level": level_name(*level),
        })),
        VerificationStatus::Unverified {
            requested,
            achieved,
            reason,
        } => {
            if achieved >= requested {
                return Err(Rejection::new(
                    ErrorClass::VerificationFailure,
                    request_id,
                    "verification.inconsistent",
                ));
            }
            Ok(serde_json::json!({
                "state": "unverified",
                "requested": level_name(*requested),
                "achieved": level_name(*achieved),
                "reason": reason.as_str(),
            }))
        }
    }
}

fn rejected(rejection: &Rejection) -> AgentRpcResponse {
    AgentRpcResponse {
        status: http_status(rejection),
        body: error_body(rejection),
    }
}

/// Renders the section 4 success envelope with its exact fields.
///
/// # Errors
/// Returns `VerificationFailure` when an unverified status does not satisfy achieved < requested.
pub fn success_body(
    request_id: RequestId,
    dispatched: &Dispatched,
    requested: Option<Option<Level>>,
) -> Result<AgentRpcResponse, Rejection> {
    let verification_status =
        verification_json(request_id, dispatched.verification.as_ref(), requested)?;
    Ok(AgentRpcResponse {
        status: 200,
        body: serde_json::json!({
            "request_id": request_id.0.to_string(),
            "value": dispatched.value,
            "verification_status": verification_status,
        })
        .to_string()
        .into_bytes(),
    })
}

fn owner_unavailable(request_id: RequestId) -> Rejection {
    Rejection {
        class: ErrorClass::InternalFault,
        retriability: Retriability::Retriable,
        request_id,
        reason: "owner.unavailable",
    }
}

fn authorized<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    envelope: &Envelope,
) -> Result<(OperationPermit, u64), Rejection> {
    let request_id = envelope.request_id;
    let surface = surface_for(envelope.operation);
    let core_sequence = owner
        .current_core_sequence()
        .map_err(|_| owner_unavailable(request_id))?;
    let guard = owner.lock().map_err(|_| owner_unavailable(request_id))?;
    if guard.degraded.status().mode != Mode::Healthy {
        return Err(Rejection {
            class: ErrorClass::UnavailableCapability,
            retriability: Retriability::Retriable,
            request_id,
            reason: "owner.degraded",
        });
    }
    let target = tenant::load_target_owner(&*guard, envelope.operation, &envelope.request)
        .map_err(|_| Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.not_authorized"))?;
    let permit = authorize(&guard.session_control, envelope, surface, core_sequence, target)?;
    drop(guard);
    Ok((permit, core_sequence))
}

fn respond(
    request_id: RequestId,
    requested: Option<Option<Level>>,
    result: Result<Dispatched, Rejection>,
) -> AgentRpcResponse {
    match result.and_then(|dispatched| success_body(request_id, &dispatched, requested)) {
        Ok(response) => response,
        Err(rejection) => rejected(&rejection),
    }
}

const fn is_fault(class: ErrorClass) -> bool {
    matches!(
        class,
        ErrorClass::InternalFault | ErrorClass::TransportFailure | ErrorClass::Deadline
    )
}

fn settled_bytes(response: &AgentRpcResponse) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(2 + response.body.len());
    bytes.extend_from_slice(&response.status.to_be_bytes());
    bytes.extend_from_slice(&response.body);
    bytes
}

fn replayed(request_id: RequestId, bytes: &[u8]) -> AgentRpcResponse {
    match bytes {
        [high, low, body @ ..] => AgentRpcResponse {
            status: u16::from_be_bytes([*high, *low]),
            body: body.to_vec(),
        },
        _ => rejected(&owner_unavailable(request_id)),
    }
}

fn idempotency_refusal(request_id: RequestId, error: IdempotencyError) -> Rejection {
    match error {
        IdempotencyError::Conflict(_) => Rejection::new(
            ErrorClass::IdempotencyConflict,
            request_id,
            "idempotency.body_changed",
        ),
        _ => owner_unavailable(request_id),
    }
}

/// Serves one agent RPC request body through the shared owner: decode (retired operations are
/// refused there), degraded refusal, session authorization with the permit held through the
/// owner effect, durable idempotency for every dispatched mutation outside the prepare and
/// submit journeys, and the success or error envelope. Typed owner refusals settle like
/// successes and replay identically; a pending record found on retry is an unknown outcome and
/// the effect is never re-run.
#[must_use]
pub fn handle_rpc<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    body: &[u8],
) -> AgentRpcResponse {
    let envelope = match decode(body) {
        Ok(envelope) => envelope,
        Err(rejection) => return rejected(&rejection),
    };
    let request_id = envelope.request_id;
    let (permit, core_sequence) = match envelope.credential {
        Some(_) => match authorized(owner, &envelope) {
            Ok(authorized) => authorized,
            Err(rejection) => return rejected(&rejection),
        },
        None => match envelope.operation {},
    };
    let bound = match owner.lock() {
        Ok(mut guard) => agent_rpc_peer::bind(&mut *guard, &permit),
        Err(_) => return rejected(&owner_unavailable(request_id)),
    };
    let context_peer = match bound {
        Ok(context_peer) => context_peer,
        Err(_) => {
            return rejected(&Rejection::new(
                ErrorClass::PolicyRefusal,
                request_id,
                "envelope.peer_unmapped",
            ));
        }
    };
    let context = DispatchContext {
        request_id,
        idempotency_key: envelope.idempotency_key,
        peer: context_peer.peer().clone(),
    };
    let journey = matches!(envelope.operation, Operation::Prepare | Operation::Submit);
    let Some(key) = envelope.idempotency_key.filter(|_| !journey) else {
        let result = dispatch_operation(owner, &permit, &context_peer, envelope.operation, &envelope.request, &context);
        let response = respond(request_id, requested_level(&envelope.request), result);
        drop(context_peer);
        drop(permit);
        return response;
    };
    let request_bytes = match canonical_request_bytes(envelope.operation, &envelope.request, request_id) {
        Ok(Some(bytes)) => bytes,
        Ok(None) => {
            return rejected(&Rejection::new(
                ErrorClass::UnavailableCapability,
                request_id,
                "idempotency.unsupported_operation",
            ));
        }
        Err(rejection) => return rejected(&rejection),
    };
    let store = match owner.idempotency(&permit.principal().tenant) {
        Ok(store) => store,
        Err(_) => return rejected(&owner_unavailable(request_id)),
    };
    let mut fault = None;
    let outcome = store.execute(key, &request_bytes, core_sequence, |attempt| {
        if attempt.retry {
            fault = Some(Rejection::new(ErrorClass::TransportFailure, request_id, "outcome.unknown"));
            return Err(String::from("outcome.unknown"));
        }
        let result = dispatch_operation(owner, &permit, &context_peer, envelope.operation, &envelope.request, &context)
            .and_then(|dispatched| {
                success_body(request_id, &dispatched, requested_level(&envelope.request))
            });
        let response = match result {
            Ok(response) => response,
            Err(rejection) if is_fault(rejection.class) => {
                fault = Some(rejection);
                return Err(String::from("fault"));
            }
            Err(rejection) => rejected(&rejection),
        };
        Ok(EconomicResult {
            response_bytes: settled_bytes(&response),
            receipt_ref: None,
        })
    });
    drop(context_peer);
    drop(permit);
    match outcome {
        Ok(Outcome::First(result) | Outcome::RepeatedOriginal(result)) => {
            replayed(request_id, &result.response_bytes)
        }
        Err(IdempotencyError::Operation(_)) => {
            rejected(&fault.unwrap_or_else(|| owner_unavailable(request_id)))
        }
        Err(error) => rejected(&idempotency_refusal(request_id, error)),
    }
}
