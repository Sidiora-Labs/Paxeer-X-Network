//! Version 1 Agent HTTP operation envelope: bounded decode, exact credential coordinates and
//! session authorization ordering ahead of any owner effect.

use layerx_agent_api::error::{ApiError, ErrorClass, ReasonCode, RequestId, Retriability};
use layerx_agent_api::verify::{Level, VerificationStatus};
use serde::Deserialize;

use crate::agent_rpc_dispatch::{
    canonical_request_bytes, dispatch_operation, DispatchContext, Dispatched,
};
use crate::agent_rpc_peer;
use crate::degraded::Mode;
use crate::human_runtime::{HumanAuthorityBoundary, SharedAgentOwner};
use crate::idempotency::{EconomicResult, IdempotencyError, Outcome};

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
    pub(crate) const fn new(
        class: ErrorClass,
        request_id: RequestId,
        reason: &'static str,
    ) -> Self {
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
        return Err(Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            unknown,
            "envelope.oversized",
        ));
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
        Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            unknown,
            "envelope.noncanonical_integer",
        )
    })?);
    if wire.version != ENVELOPE_VERSION {
        return Err(Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "envelope.version",
        ));
    }
    if RETIRED_OPERATIONS.contains(&wire.operation.as_str()) {
        return Err(Rejection::new(
            ErrorClass::UnavailableCapability,
            request_id,
            "unavailable_capability.faucet.claim",
        ));
    }
    let operation = lookup(&wire.operation).ok_or_else(|| {
        Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "envelope.unknown_operation",
        )
    })?;
    let idempotency_key = match (operation.mutating(), wire.idempotency_key.as_deref()) {
        (true, Some(text)) => Some(parse_hex32(text).filter(|key| *key != [0; 32]).ok_or_else(
            || {
                Rejection::new(
                    ErrorClass::ProtocolIncompatibility,
                    request_id,
                    "envelope.credential",
                )
            },
        )?),
        (true, None) | (false, Some(_)) => {
            return Err(Rejection::new(
                ErrorClass::IdempotencyConflict,
                request_id,
                "envelope.idempotency_key",
            ));
        }
        (false, None) => None,
    };
    let malformed_credential = || {
        Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "envelope.credential",
        )
    };
    let bootstrap = BOOTSTRAP_OPERATIONS.contains(&operation);
    let credential = match (bootstrap, wire.credential) {
        (true, None) => None,
        (false, Some(credential)) => {
            let tenant = TenantId::new(credential.tenant).map_err(|_| malformed_credential())?;
            let session_id =
                SessionId(parse_hex32(&credential.session_id).ok_or_else(malformed_credential)?);
            let token_id = parse_hex32(&credential.token_id).ok_or_else(malformed_credential)?;
            let generation = parse_decimal_u64(&credential.generation).ok_or_else(|| {
                Rejection::new(
                    ErrorClass::ProtocolIncompatibility,
                    request_id,
                    "envelope.noncanonical_integer",
                )
            })?;
            Some(SessionCredential::new(
                tenant, session_id, token_id, generation,
            ))
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

fn request_text<'a>(envelope: &'a Envelope, field: &str) -> Result<Option<&'a str>, Rejection> {
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
        Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "envelope.credential",
        )
    })?;
    let mismatch = || {
        Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "envelope.coordinate_mismatch",
        )
    };
    let supplied_tenant = request_text(envelope, "tenant")?
        .map(|text| TenantId::new(text).map_err(|_| mismatch()))
        .transpose()?;
    tenant::require_caller_coordinates(credential.tenant(), None, supplied_tenant.as_ref())
        .map_err(|_| mismatch())?;
    tenant::require_session_credential(envelope.operation, envelope.credential.as_ref()).map_err(
        |error| authorization_rejection(request_id, &SessionControlError::Authorization(error)),
    )?;
    let permit = control
        .authorize(
            credential,
            envelope.operation,
            surface,
            core_sequence,
            target_owner,
        )
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
            return Err(Rejection::new(
                ErrorClass::PolicyRefusal,
                RequestId(0),
                "envelope.header_principal",
            ));
        }
    }
    Ok(())
}

fn authorization_rejection(request_id: RequestId, error: &SessionControlError) -> Rejection {
    match error {
        SessionControlError::Authorization(AuthorizationError::ScopeDenied) => Rejection::new(
            ErrorClass::CapabilityRefusal,
            request_id,
            "session.scope_denied",
        ),
        SessionControlError::Authorization(AuthorizationError::Expired) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.expired")
        }
        SessionControlError::Authorization(AuthorizationError::Revoked) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.revoked")
        }
        SessionControlError::Authorization(AuthorizationError::InvalidRequest) => Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "session.invalid_request",
        ),
        SessionControlError::Unavailable => Rejection {
            class: ErrorClass::InternalFault,
            retriability: Retriability::Retriable,
            request_id,
            reason: "unavailable",
        },
        SessionControlError::Session(crate::session::SessionError::Revoked) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.revoked")
        }
        SessionControlError::Session(crate::session::SessionError::Expired) => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "session.expired")
        }
        SessionControlError::Session(_) => Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "session.not_authorized",
        ),
        _ => Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "session.not_authorized",
        ),
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
        ErrorClass::PolicyRefusal | ErrorClass::CapabilityRefusal | ErrorClass::BudgetRefusal => {
            403
        }
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

const LEVELS: [(&str, Level); 12] = [
    ("Unverified", Level::Unverified),
    ("SequencerSigned", Level::SequencerSigned),
    ("BatchIncluded", Level::BatchIncluded),
    ("StateProven", Level::StateProven),
    ("CheckpointFinalised", Level::CheckpointFinalised),
    ("SettlementAnchored", Level::SettlementAnchored),
    ("unverified", Level::Unverified),
    ("sequencer-signed", Level::SequencerSigned),
    ("batch-included", Level::BatchIncluded),
    ("state-proven", Level::StateProven),
    ("checkpoint-finalised", Level::CheckpointFinalised),
    ("settlement-anchored", Level::SettlementAnchored),
];

/// `None` when the request declares no level; `Some(None)` when the declared level is not a
/// level name.
fn requested_level(request: &serde_json::Map<String, serde_json::Value>) -> Option<Option<Level>> {
    let value = request.get("requested_verification_level")?;
    Some(value.as_str().and_then(|name| {
        LEVELS
            .into_iter()
            .find_map(|(spelling, level)| (spelling == name).then_some(level))
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

fn lookup_owner_error(
    request_id: RequestId,
    error: crate::human::HumanOperationError,
) -> Rejection {
    match error {
        crate::human::HumanOperationError::Refused => {
            Rejection::new(ErrorClass::PolicyRefusal, request_id, "owner.refused")
        }
        crate::human::HumanOperationError::Unavailable => Rejection {
            class: ErrorClass::UnavailableCapability,
            retriability: Retriability::Retriable,
            request_id,
            reason: "owner.unavailable",
        },
        crate::human::HumanOperationError::Typed(refusal) => {
            Rejection::new(refusal.class(), request_id, refusal.reason())
        }
        crate::human::HumanOperationError::CapabilityRefused(dimension) => Rejection::new(
            ErrorClass::CapabilityRefusal,
            request_id,
            match dimension {
                crate::capability::Dimension::Expiry => "capability.expiry",
                crate::capability::Dimension::ActivityType => "capability.activity_type",
                crate::capability::Dimension::Counterparty => "capability.counterparty",
                crate::capability::Dimension::Asset => "capability.asset",
                crate::capability::Dimension::Amount => "capability.amount",
                crate::capability::Dimension::Rate => "capability.rate",
                crate::capability::Dimension::Purpose => "capability.purpose",
            },
        ),
    }
}

fn authorized<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    envelope: &Envelope,
) -> Result<
    (
        OperationPermit,
        u64,
        agent_rpc_peer::BoundRpcPeer,
        SessionControl,
    ),
    Rejection,
> {
    authorized_on_surface(owner, envelope, surface_for(envelope.operation))
}

fn authorized_on_surface<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    envelope: &Envelope,
    surface: Surface,
) -> Result<
    (
        OperationPermit,
        u64,
        agent_rpc_peer::BoundRpcPeer,
        SessionControl,
    ),
    Rejection,
> {
    let request_id = envelope.request_id;
    let credential = envelope.credential.as_ref().ok_or_else(|| {
        Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            request_id,
            "envelope.credential",
        )
    })?;
    let mismatch = || {
        Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "envelope.coordinate_mismatch",
        )
    };
    let supplied_tenant = request_text(envelope, "tenant")?
        .map(|text| TenantId::new(text).map_err(|_| mismatch()))
        .transpose()?;
    let supplied_agent = request_text(envelope, "agent")?;
    tenant::require_caller_coordinates(credential.tenant(), None, supplied_tenant.as_ref())
        .map_err(|_| mismatch())?;
    tenant::require_session_credential(envelope.operation, Some(credential)).map_err(|error| {
        authorization_rejection(request_id, &SessionControlError::Authorization(error))
    })?;
    let core_sequence = owner
        .current_core_sequence()
        .map_err(|_| owner_unavailable(request_id))?;
    let control = {
        let guard = owner.lock().map_err(|_| owner_unavailable(request_id))?;
        if guard.degraded.status().mode != Mode::Healthy {
            return Err(Rejection {
                class: ErrorClass::UnavailableCapability,
                retriability: Retriability::Retriable,
                request_id,
                reason: "owner.degraded",
            });
        }
        guard.session_control.clone()
    };
    let lookup = control
        .authenticate_lookup(credential, envelope.operation, surface, core_sequence)
        .map_err(|error| authorization_rejection(request_id, &error))?;
    if supplied_agent.is_some_and(|agent| lookup.principal().agent.as_bytes() != agent.as_bytes()) {
        return Err(mismatch());
    }
    let (bound, target) = {
        let mut guard = owner.lock().map_err(|_| owner_unavailable(request_id))?;
        let bound =
            agent_rpc_peer::bind_lookup(&mut *guard, &lookup).map_err(|error| match error {
                crate::human::HumanOperationError::Refused => Rejection::new(
                    ErrorClass::PolicyRefusal,
                    request_id,
                    "envelope.peer_unmapped",
                ),
                error => lookup_owner_error(request_id, error),
            })?;
        let target = guard
            .target_object_owner_authenticated(&lookup, &bound, &envelope.request)
            .map_err(|error| lookup_owner_error(request_id, error))?;
        (bound, target)
    };
    let core_sequence = owner
        .current_core_sequence()
        .map_err(|_| owner_unavailable(request_id))?;
    let permit = control
        .authorize_resolved(lookup, target, core_sequence)
        .map_err(|error| authorization_rejection(request_id, &error))?;
    Ok((permit, core_sequence, bound, control))
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
    if BOOTSTRAP_OPERATIONS.contains(&envelope.operation) {
        return rejected(&Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "refused_pending_bootstrap_artifact",
        ));
    }
    let (permit, core_sequence, bound, control) = match envelope.credential {
        Some(_) => match authorized(owner, &envelope) {
            Ok(authorized) => authorized,
            Err(rejection) => return rejected(&rejection),
        },
        None => {
            return rejected(&Rejection::new(
                ErrorClass::ProtocolIncompatibility,
                request_id,
                "envelope.credential",
            ));
        }
    };
    let context_peer = match agent_rpc_peer::from_resolved(&control, &permit, bound) {
        Ok(context_peer) => context_peer,
        Err(error) => return rejected(&authorization_rejection(request_id, &error)),
    };
    let context = DispatchContext {
        request_id,
        idempotency_key: envelope.idempotency_key,
        peer: context_peer.peer().clone(),
    };
    let journey = matches!(envelope.operation, Operation::Prepare | Operation::Submit);
    let Some(key) = envelope.idempotency_key.filter(|_| !journey) else {
        let result = dispatch_operation(
            owner,
            &permit,
            &context_peer,
            envelope.operation,
            &envelope.request,
            &context,
        );
        let response = respond(request_id, requested_level(&envelope.request), result);
        drop(context_peer);
        drop(permit);
        return response;
    };
    let request_bytes =
        match canonical_request_bytes(envelope.operation, &envelope.request, request_id) {
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
            fault = Some(Rejection::new(
                ErrorClass::TransportFailure,
                request_id,
                "outcome.unknown",
            ));
            return Err(String::from("outcome.unknown"));
        }
        let result = dispatch_operation(
            owner,
            &permit,
            &context_peer,
            envelope.operation,
            &envelope.request,
            &context,
        )
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

pub fn handle_human_native_prepare<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    peer: &crate::human::HumanPeer,
    body: &[u8],
) -> Result<AgentRpcResponse, crate::human::HumanOperationError> {
    use crate::human::HumanOperationError;

    let subject = peer.subject.as_ref().ok_or(HumanOperationError::Refused)?;
    if peer.uid == 0 || peer.principal.is_empty() || peer.tenant.is_empty() {
        return Err(HumanOperationError::Refused);
    }
    let envelope = decode(body).map_err(|_| HumanOperationError::Refused)?;
    if envelope.operation != Operation::Prepare
        || !crate::agent_rpc_dispatch::native_effect_variant(&envelope.request)
        || envelope
            .credential
            .as_ref()
            .is_none_or(|credential| credential.tenant().as_str() != peer.tenant)
    {
        return Err(HumanOperationError::Refused);
    }
    let (permit, _, bound, control) = authorized(owner, &envelope).map_err(|error| {
        if matches!(
            error.class,
            ErrorClass::InternalFault
                | ErrorClass::TransportFailure
                | ErrorClass::Deadline
                | ErrorClass::UnavailableCapability
        ) {
            HumanOperationError::Unavailable
        } else {
            HumanOperationError::Refused
        }
    })?;
    let context = agent_rpc_peer::from_resolved(&control, &permit, bound)
        .map_err(|_| HumanOperationError::Refused)?;
    if context.peer() != peer
        || context.principal().tenant.as_str() != peer.tenant
        || context.principal().agent.as_bytes() != subject.owner.as_bytes()
    {
        return Err(HumanOperationError::Refused);
    }
    permit
        .boundary(&control)
        .map_err(|_| HumanOperationError::Refused)?;
    drop(context);
    drop(permit);
    drop(control);
    drop(envelope);
    let response = handle_rpc(owner, body);
    if response.body.is_empty() || response.body.len() > MAX_BODY_BYTES - 64 {
        return Err(HumanOperationError::Refused);
    }
    Ok(response)
}

pub fn handle_human_native_send_prepare<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    peer: &crate::human::HumanPeer,
    body: &[u8],
) -> Result<AgentRpcResponse, crate::human::HumanOperationError> {
    use crate::human::HumanOperationError;

    let subject = peer.subject.as_ref().ok_or(HumanOperationError::Refused)?;
    if peer.uid == 0 || peer.principal.is_empty() || peer.tenant.is_empty() {
        return Err(HumanOperationError::Refused);
    }
    let envelope = decode(body).map_err(|_| HumanOperationError::Refused)?;
    if !matches!(envelope.operation, Operation::Prepare | Operation::Submit)
        || surface_for(envelope.operation) != Surface::Contract
        || envelope
            .credential
            .as_ref()
            .is_none_or(|credential| credential.tenant().as_str() != peer.tenant)
    {
        return Err(HumanOperationError::Refused);
    }
    let preparation_id = match envelope.operation {
        Operation::Prepare if crate::agent_rpc_dispatch::native_send_variant(&envelope.request) => {
            None
        }
        Operation::Submit
            if crate::agent_rpc_dispatch::native_send_submit_variant(&envelope.request) =>
        {
            let request = crate::agent_rpc_wire::decode_wire::<
                crate::agent_rpc_wire::NativeSendSubmitV1Wire,
            >(&envelope.request, envelope.request_id)
            .and_then(|request| request.into_request(envelope.request_id))
            .map_err(|_| HumanOperationError::Refused)?;
            Some(
                crate::agent_rpc_dispatch::hex32(&request.preparation_ref, envelope.request_id)
                    .map_err(|_| HumanOperationError::Refused)?,
            )
        }
        _ => return Err(HumanOperationError::Refused),
    };
    let (permit, _, bound, control) = authorized(owner, &envelope).map_err(|error| {
        if matches!(
            error.class,
            ErrorClass::InternalFault
                | ErrorClass::TransportFailure
                | ErrorClass::Deadline
                | ErrorClass::UnavailableCapability
        ) {
            HumanOperationError::Unavailable
        } else {
            HumanOperationError::Refused
        }
    })?;
    let context = agent_rpc_peer::from_resolved(&control, &permit, bound)
        .map_err(|_| HumanOperationError::Refused)?;
    if context.peer() != peer
        || context.principal().tenant.as_str() != peer.tenant
        || context.principal().agent.as_bytes() != subject.owner.as_bytes()
    {
        return Err(HumanOperationError::Refused);
    }
    permit
        .boundary(&control)
        .map_err(|_| HumanOperationError::Refused)?;
    if let Some(preparation_id) = preparation_id {
        owner
            .lock()?
            .require_native_send_preparation(&context, preparation_id)?;
    }
    drop(context);
    drop(permit);
    drop(control);
    drop(envelope);
    let response = handle_rpc(owner, body);
    if response.body.is_empty() || response.body.len() > MAX_BODY_BYTES - 64 {
        return Err(HumanOperationError::Refused);
    }
    Ok(response)
}

#[cfg(test)]
fn level_request(name: &str) -> serde_json::Map<String, serde_json::Value> {
    let mut request = serde_json::Map::new();
    request.insert(
        String::from("requested_verification_level"),
        serde_json::Value::String(String::from(name)),
    );
    request
}

#[test]
fn requested_level_accepts_each_declared_kebab_case_spelling() {
    for (name, level) in [
        ("unverified", Level::Unverified),
        ("sequencer-signed", Level::SequencerSigned),
        ("batch-included", Level::BatchIncluded),
        ("state-proven", Level::StateProven),
        ("checkpoint-finalised", Level::CheckpointFinalised),
        ("settlement-anchored", Level::SettlementAnchored),
    ] {
        assert_eq!(requested_level(&level_request(name)), Some(Some(level)));
    }
}

#[test]
fn requested_level_keeps_each_camel_case_spelling() {
    for (name, level) in [
        ("Unverified", Level::Unverified),
        ("SequencerSigned", Level::SequencerSigned),
        ("BatchIncluded", Level::BatchIncluded),
        ("StateProven", Level::StateProven),
        ("CheckpointFinalised", Level::CheckpointFinalised),
        ("SettlementAnchored", Level::SettlementAnchored),
    ] {
        assert_eq!(requested_level(&level_request(name)), Some(Some(level)));
    }
}

#[test]
fn requested_level_refuses_an_unknown_spelling() {
    for name in [
        "Sequencer-Signed",
        "SEQUENCER-SIGNED",
        "sequencer_signed",
        "sequencersigned",
        " sequencer-signed",
        "sequencer-signed ",
        "checkpoint-finalized",
        "",
    ] {
        let requested = requested_level(&level_request(name));
        assert_eq!(requested, Some(None));
        let refusal = verification_json(RequestId(3), None, requested)
            .expect_err("an unknown requested level is refused");
        assert_eq!(refusal.class, ErrorClass::VerificationFailure);
        assert_eq!(refusal.reason, "verification.inconsistent");
    }
}

#[test]
fn requested_level_parses_the_program_activity_default() {
    assert_eq!(
        requested_level(&level_request("sequencer-signed")),
        Some(Some(Level::SequencerSigned))
    );
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct McpToolRequest {
    tool: String,
    arguments: serde_json::Value,
}

pub struct McpInvocationRequest {
    pub request_id: RequestId,
    pub credential: SessionCredential,
    pub tool: String,
    pub arguments: serde_json::Value,
    pub idempotency_key: [u8; 32],
}

pub fn decode_mcp_invocation(
    body: &[u8],
) -> Result<Option<McpInvocationRequest>, AgentRpcResponse> {
    if body.len() > MAX_BODY_BYTES {
        return Err(refusal(
            413,
            ErrorClass::ProtocolIncompatibility,
            "envelope.oversized",
        ));
    }
    let wire: WireEnvelope = match serde_json::from_slice(body) {
        Ok(wire) => wire,
        Err(_) => {
            return Err(refusal(
                400,
                ErrorClass::ProtocolIncompatibility,
                "envelope.malformed",
            ))
        }
    };
    if wire.operation != "mcp.invoke" {
        return Ok(None);
    }
    let envelope = decode(body).map_err(|error| rejected(&error))?;
    let id = envelope.request_id;
    let request: McpToolRequest =
        serde_json::from_value(serde_json::Value::Object(envelope.request)).map_err(|_| {
            rejected(&Rejection::new(
                ErrorClass::ProtocolIncompatibility,
                id,
                "envelope.unknown_field",
            ))
        })?;
    if request.tool.is_empty()
        || request.tool.len() > 128
        || request.tool.as_bytes().contains(&0)
        || !request.arguments.is_object()
    {
        return Err(rejected(&Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            id,
            "envelope.malformed",
        )));
    }
    let credential = envelope.credential.ok_or_else(|| {
        rejected(&Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            id,
            "envelope.credential",
        ))
    })?;
    let idempotency_key = envelope.idempotency_key.ok_or_else(|| {
        rejected(&Rejection::new(
            ErrorClass::ProtocolIncompatibility,
            id,
            "envelope.idempotency_key",
        ))
    })?;
    Ok(Some(McpInvocationRequest {
        request_id: id,
        credential,
        tool: request.tool,
        arguments: request.arguments,
        idempotency_key,
    }))
}

pub fn mcp_result_response(id: RequestId, value: serde_json::Value) -> AgentRpcResponse {
    respond(
        id,
        None,
        Ok(Dispatched {
            value,
            verification: None,
        }),
    )
}

pub fn mcp_result_refusal(
    id: RequestId,
    class: ErrorClass,
    reason: &'static str,
) -> AgentRpcResponse {
    rejected(&Rejection::new(class, id, reason))
}

pub fn validate_mcp_native_request(
    operation: Operation,
    arguments: &serde_json::Value,
) -> Result<(), Rejection> {
    let id = RequestId(0);
    let request = arguments
        .as_object()
        .ok_or_else(|| crate::agent_rpc_dispatch::malformed(id))?;
    let variant = request.get("variant").and_then(serde_json::Value::as_str);
    let admitted = match operation {
        Operation::Prepare => matches!(
            variant,
            Some("native_v1" | "native_effect_v1" | "native_send_v1" | "native_disclosure_v1")
        ),
        Operation::Submit => variant == Some("native_send_submit_v1"),
        _ => false,
    };
    if !admitted || canonical_request_bytes(operation, request, id)?.is_none() {
        return Err(crate::agent_rpc_dispatch::malformed(id));
    }
    Ok(())
}

pub struct McpOwnerEnvironment {
    pub registry: layerx_types::payload::ModuleRegistry,
    pub core_time_ms: u64,
    pub head_sequence: u64,
    pub native_effect_profile: bool,
    pub native_send_profile: bool,
}

pub fn mcp_owner_environment<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, request_id: RequestId,
    credential: &SessionCredential,
) -> Result<McpOwnerEnvironment, Rejection> {
    let envelope = Envelope {
        request_id, operation:Operation::ReadAuthority, idempotency_key:None,
        request:serde_json::Map::new(), credential:Some(credential.clone()),
    };
    let (permit, _, bound, control) = authorized_on_surface(owner, &envelope, Surface::Mcp)?;
    let context = agent_rpc_peer::from_resolved(&control, &permit, bound)
        .map_err(|error| authorization_rejection(request_id, &error))?;
    owner.lock().and_then(|mut guard| guard.rpc_mcp_owner_environment(&context))
        .map_err(|error| crate::agent_rpc_dispatch::owner_error(request_id, error))
}

pub fn dispatch_mcp_owner<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    request_id: RequestId,
    credential: &SessionCredential,
    operation: Operation,
    arguments: &serde_json::Value,
    idempotency_key: [u8; 32],
) -> Result<serde_json::Value, Rejection> {
    if operation == Operation::McpInvoke
        || BOOTSTRAP_OPERATIONS.contains(&operation)
        || idempotency_key == [0; 32]
    {
        return Err(Rejection::new(
            ErrorClass::PolicyRefusal,
            request_id,
            "mcp.operation_refused",
        ));
    }
    let request = arguments
        .as_object()
        .ok_or_else(|| crate::agent_rpc_dispatch::malformed(request_id))?;
    if let Some(value) = request.get("idempotency_key") {
        if value.as_str().and_then(parse_hex32) != Some(idempotency_key) {
            return Err(Rejection::new(
                ErrorClass::IdempotencyConflict,
                request_id,
                "idempotency.body_changed",
            ));
        }
    }
    let envelope = Envelope {
        request_id,
        operation,
        idempotency_key: operation.mutating().then_some(idempotency_key),
        request: request.clone(),
        credential: Some(credential.clone()),
    };
    let (permit, _, bound, control) = authorized_on_surface(owner, &envelope, Surface::Mcp)?;
    let context_peer = agent_rpc_peer::from_resolved(&control, &permit, bound)
        .map_err(|error| authorization_rejection(request_id, &error))?;
    let context = DispatchContext {
        request_id,
        idempotency_key: envelope.idempotency_key,
        peer: context_peer.peer().clone(),
    };
    dispatch_operation(owner, &permit, &context_peer, operation, request, &context)
        .map(|dispatched| dispatched.value)
}

pub fn mcp_api_refusal(error: &ApiError) -> AgentRpcResponse {
    let reason = error.reason.as_str();
    let status = match error.class {
        ErrorClass::ProtocolIncompatibility => 400,
        ErrorClass::PolicyRefusal if reason == "session.not_authorized" => 401,
        ErrorClass::PolicyRefusal | ErrorClass::CapabilityRefusal | ErrorClass::BudgetRefusal => {
            403
        }
        ErrorClass::IdempotencyConflict => 409,
        ErrorClass::UnavailableCapability => 503,
        _ => 500,
    };
    AgentRpcResponse {
        status,
        body: serde_json::json!({
            "class": class_name(error.class),
            "protocol_result_code": error.protocol_result_code.map(|code| code.raw()),
            "retriability": match error.retriability {
                Retriability::Terminal => "Terminal", Retriability::Retriable => "Retriable",
            },
            "request_id": error.request_id.0.to_string(),
            "reason": reason,
        })
        .to_string()
        .into_bytes(),
    }
}

pub fn decode_mcp_local_grant_config(
    value: &serde_json::Value,
) -> Result<layerx_agent_api::identity::NativeLocalGrantConsentV1, Rejection> {
    let id = RequestId(0);
    serde_json::from_value::<crate::agent_rpc_wire::NativeLocalGrantConsentV1Wire>(value.clone())
        .map_err(|_| crate::agent_rpc_dispatch::malformed(id))?
        .into_request(id)
}

pub fn mcp_control_refusal(request_id: RequestId, error: &SessionControlError) -> AgentRpcResponse {
    rejected(&authorization_rejection(request_id, error))
}
