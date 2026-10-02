//! Typed owner adapters for catalogue operations outside the base envelope dispatcher.
//!
//! Each arm decodes the operation `request` strictly into the exact arguments of an existing
//! `HumanOperations` method and calls it on the shared daemon owner. Operations without an
//! existing owner method return `None`, so the dispatcher keeps them unmatched.

use layerx_agent_api::error::{ErrorClass, Level, RequestId, Retriability, VerificationStatus};
use serde::Deserialize;
use serde_json::{Map, Value};

use crate::agent_rpc::Rejection;
use crate::agent_rpc_dispatch::{DispatchContext, Dispatched};
use crate::agent_rpc_peer::RpcOwnerContext;
use crate::human::{HumanOperationError, HumanOperations, HumanResponse};
use crate::human_runtime::{HumanAuthorityBoundary, SharedAgentOwner};
use crate::session_control::OperationPermit;
use crate::tenant::Operation;

pub(crate) fn budget_list<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: BudgetListRequestWire = decode(request, id)?;
    let principal = context.principal();
    if principal.tenant.as_str() != wire.tenant
        || principal.agent.as_bytes() != wire.agent_did.as_bytes()
    {
        return Err(rejection(
            ErrorClass::PolicyRefusal,
            id,
            "envelope.coordinate_mismatch",
        ));
    }
    let typed = layerx_agent_api::budget::BudgetList {
        tenant: layerx_agent_api::identity::TenantId::new(wire.tenant)
            .map_err(|_| malformed(id))?,
        agent_did: layerx_agent_api::identity::AgentDid::new(wire.agent_did)
            .map_err(|_| malformed(id))?,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.budget_list(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let count = reader.u16()?;
        let mut budgets = Vec::with_capacity(usize::from(count));
        for _ in 0..count {
            let budget_id: [u8; 32] = reader.fixed()?;
            let agent_id = reader.text()?;
            let mut budget = Map::new();
            budget.insert("budget_id".into(), hexv(&budget_id));
            budget.insert("agent_id".into(), Value::String(agent_id));
            budgets.push(Value::Object(budget));
        }
        let mut out = Map::new();
        out.insert("budgets".into(), Value::Array(budgets));
        Some((Value::Object(out), None))
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BudgetListRequestWire {
    tenant: String,
    agent_did: String,
}

pub(crate) fn budget_reconciliation<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: WireBudgetTarget = decode(request, id)?;
    let principal = context.principal();
    if principal.tenant.as_str() != wire.tenant
        || principal.agent.as_bytes() != wire.agent_did.as_bytes()
    {
        return Err(rejection(
            ErrorClass::PolicyRefusal,
            id,
            "envelope.coordinate_mismatch",
        ));
    }
    let budget_id = hex32(&wire.budget_id, id)?;
    let typed = layerx_agent_api::budget::BudgetTarget {
        tenant: layerx_agent_api::identity::TenantId::new(wire.tenant)
            .map_err(|_| malformed(id))?,
        agent_did: layerx_agent_api::identity::AgentDid::new(wire.agent_did)
            .map_err(|_| malformed(id))?,
        budget_id: layerx_agent_api::budget::BudgetId::new(wire.budget_id)
            .map_err(|_| malformed(id))?,
    };
    let response = {
        let mut guard = owner
            .lock()
            .map_err(|_| owner_error(id, HumanOperationError::Unavailable))?;
        guard.rpc_budget_reconciliation(context, typed)
    }
    .map_err(|error| owner_error(id, error))?;
    decode_budget_state(&response, budget_id).ok_or_else(|| {
        rejection(ErrorClass::InternalFault, id, "owner.response_malformed")
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WireBudgetTarget {
    tenant: String,
    agent_did: String,
    budget_id: String,
}

const fn rejection(class: ErrorClass, request_id: RequestId, reason: &'static str) -> Rejection {
    Rejection {
        class,
        retriability: Retriability::Terminal,
        request_id,
        reason,
    }
}

fn malformed(request_id: RequestId) -> Rejection {
    rejection(ErrorClass::ProtocolIncompatibility, request_id, "envelope.malformed")
}

fn decode<T: for<'de> Deserialize<'de>>(
    request: &Map<String, Value>,
    request_id: RequestId,
) -> Result<T, Rejection> {
    serde_json::from_value(Value::Object(request.clone())).map_err(|error| {
        if error.to_string().starts_with("unknown field") {
            rejection(
                ErrorClass::ProtocolIncompatibility,
                request_id,
                "envelope.unknown_field",
            )
        } else {
            malformed(request_id)
        }
    })
}

fn hex32(text: &str, request_id: RequestId) -> Result<[u8; 32], Rejection> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 || !bytes.iter().all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(malformed(request_id));
    }
    let mut out = [0_u8; 32];
    for (index, pair) in bytes.chunks_exact(2).enumerate() {
        let text = std::str::from_utf8(pair).map_err(|_| malformed(request_id))?;
        out[index] = u8::from_str_radix(text, 16).map_err(|_| malformed(request_id))?;
    }
    Ok(out)
}

fn owner_error(request_id: RequestId, error: HumanOperationError) -> Rejection {
    match error {
        HumanOperationError::Refused => {
            rejection(ErrorClass::PolicyRefusal, request_id, "owner.refused")
        }
        HumanOperationError::Unavailable => Rejection {
            class: ErrorClass::UnavailableCapability,
            retriability: Retriability::Retriable,
            request_id,
            reason: "owner.unavailable",
        },
    }
}

fn level(value: u8) -> Option<Level> {
    Some(match value {
        0 => Level::Unverified,
        1 => Level::SequencerSigned,
        2 => Level::BatchIncluded,
        3 => Level::StateProven,
        4 => Level::CheckpointFinalised,
        5 => Level::SettlementAnchored,
        _ => return None,
    })
}

fn hexv(bytes: &[u8]) -> Value {
    Value::String(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

fn dec(value: impl ToString) -> Value {
    Value::String(value.to_string())
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    fn take(&mut self, length: usize) -> Option<&'a [u8]> {
        let end = self.offset.checked_add(length)?;
        let value = self.bytes.get(self.offset..end)?;
        self.offset = end;
        Some(value)
    }
    fn fixed<const N: usize>(&mut self) -> Option<[u8; N]> {
        self.take(N)?.try_into().ok()
    }
    fn u8(&mut self) -> Option<u8> {
        Some(self.take(1)?[0])
    }
    fn u64(&mut self) -> Option<u64> {
        Some(u64::from_be_bytes(self.fixed()?))
    }
    fn u128(&mut self) -> Option<u128> {
        Some(u128::from_be_bytes(self.fixed()?))
    }
    fn finish(&self) -> Option<()> {
        (self.offset == self.bytes.len()).then_some(())
    }
}

fn decode_budget_state(response: &HumanResponse, budget_id: [u8; 32]) -> Option<Dispatched> {
    let mut reader = Reader {
        bytes: response.bytes(),
        offset: 0,
    };
    let returned: [u8; 32] = reader.fixed()?;
    if returned != budget_id {
        return None;
    }
    let revocation_sequence = reader.u64()?;
    let observed_head_sequence = reader.u64()?;
    let achieved = level(reader.u8()?)?;
    let evidence_digest: [u8; 32] = reader.fixed()?;
    let receipt_digest: [u8; 32] = reader.fixed()?;
    let checkpoint_digest: [u8; 32] = reader.fixed()?;
    let age_sequences = reader.u64()?;
    let maximum_age_sequences = reader.u64()?;
    let remaining = reader.u128()?;
    let asset: [u8; 32] = reader.fixed()?;
    reader.finish()?;
    let mut value = Map::new();
    value.insert("budget_id".into(), hexv(&returned));
    value.insert("revocation_sequence".into(), dec(revocation_sequence));
    value.insert("observed_head_sequence".into(), dec(observed_head_sequence));
    value.insert("evidence_digest".into(), hexv(&evidence_digest));
    value.insert("receipt_digest".into(), hexv(&receipt_digest));
    value.insert("checkpoint_digest".into(), hexv(&checkpoint_digest));
    value.insert("age_sequences".into(), dec(age_sequences));
    value.insert("maximum_age_sequences".into(), dec(maximum_age_sequences));
    value.insert("remaining".into(), dec(remaining));
    value.insert("asset".into(), hexv(&asset));
    Some(Dispatched {
        value: Value::Object(value),
        verification: Some(VerificationStatus::Achieved(achieved)),
    })
}

pub(crate) fn approval_approve<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    approval_decision(owner, context, request, ctx, true)
}

pub(crate) fn approval_reject<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    approval_decision(owner, context, request, ctx, false)
}

fn approval_decision<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
    approve: bool,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::{
        decimal_u64, decode_decision, dispatched, lower_hex, mutation_key, ApprovalDecisionRequest,
    };
    let id = ctx.request_id;
    let request: ApprovalDecisionRequest = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let approval_id = hex32(&request.approval_id, id)?;
    let held_digest = hex32(&request.held_digest, id)?;
    let current_sequence = decimal_u64(&request.current_sequence, id)?;
    let key = lower_hex(&mutation_key(ctx)?);
    let response = owner.lock().and_then(|mut guard| {
        if approve {
            guard.rpc_approval_approve(context, approval_id, held_digest, &key, current_sequence)
        } else {
            guard.rpc_approval_reject(context, approval_id, held_digest, &key, current_sequence)
        }
    });
    dispatched(id, response, decode_decision)
}

pub(crate) fn prepare<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::{decode_preparation, dispatched, human_prepare, mutation_key};
    let id = ctx.request_id;
    let typed = human_prepare(decode(request, id)?, id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::prepare_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_prepare(context, envelope));
    dispatched(id, response, decode_preparation)
}

pub(crate) fn submit<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::{decode_observation, dispatched, human_submit, mutation_key};
    let id = ctx.request_id;
    let typed = human_submit(decode(request, id)?, id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::submit_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_submit_external(context, envelope));
    dispatched(id, response, decode_observation)
}

pub(crate) fn session_refresh<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: SessionTargetWire = decode(request, id)?;
    let typed = layerx_agent_api::identity::SessionRefresh {
        session_id: layerx_agent_api::identity::SessionId::new(request.session_id)
            .map_err(|_| malformed(id))?,
        context: session_context(request.context, context, id)?,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.rpc_session_refresh(context, typed);
    owner_payload(id, response, |reader| {
        let tenant = reader.text()?;
        let session_id: [u8; 32] = reader.fixed()?;
        let token_id: [u8; 32] = reader.fixed()?;
        let generation = reader.u64()?;
        let expires_at = reader.u64()?;
        let (session, open, record_session_id) = decode_session_record(reader)?;
        let record = session.as_object()?;
        if !open
            || record_session_id != session_id
            || record.get("token_id")? != &hexv(&token_id)
            || record.get("generation")? != &dec(generation)
        {
            return None;
        }
        let mut credential = Map::new();
        credential.insert("tenant".into(), Value::String(tenant));
        credential.insert("session_id".into(), hexv(&session_id));
        credential.insert("token_id".into(), hexv(&token_id));
        credential.insert("generation".into(), dec(generation));
        credential.insert("expires_at".into(), dec(expires_at));
        let mut out = Map::new();
        out.insert("credential".into(), Value::Object(credential));
        out.insert("session".into(), session);
        Some((Value::Object(out), None))
    })
}

pub(crate) fn session_close<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: SessionTargetWire = decode(request, id)?;
    let typed = layerx_agent_api::identity::SessionClose {
        session_id: layerx_agent_api::identity::SessionId::new(request.session_id)
            .map_err(|_| malformed(id))?,
        context: session_context(request.context, context, id)?,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.rpc_session_close(context, typed);
    owner_payload(id, response, |reader| {
        let record = decode_session_record(reader)?;
        (!record.1).then_some((record.0, None))
    })
}

pub(crate) fn session_list<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: SessionListWire = decode(request, id)?;
    let typed =
        layerx_agent_api::identity::SessionList(session_context(request.context, context, id)?);
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.session_list(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let count = reader.u16()?;
        let mut sessions = Vec::with_capacity(usize::from(count));
        let mut previous: Option<[u8; 32]> = None;
        for _ in 0..count {
            let (record, _, session_id) = decode_session_record(reader)?;
            if previous.is_some_and(|last| last >= session_id) {
                return None;
            }
            previous = Some(session_id);
            sessions.push(record);
        }
        let mut out = Map::new();
        out.insert("sessions".into(), Value::Array(sessions));
        Some((Value::Object(out), None))
    })
}

pub(crate) fn wait<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: WaitWire = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let typed = layerx_agent_api::track::WaitRequest {
        submission_ref: layerx_agent_api::track::SubmissionRef::new(request.submission_ref)
            .map_err(|_| malformed(id))?,
        requested_verification_level: requested_level(&request.requested_verification_level, id)?,
        deadline: layerx_agent_api::generated::TimestampSeconds::parse_decimal(&request.deadline)
            .ok()
            .filter(|value| value.get().to_string() == request.deadline)
            .ok_or_else(|| malformed(id))?,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.wait(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let deadline_elapsed = match reader.u8()? {
            0 => false,
            1 => true,
            _ => return None,
        };
        let actual = level(reader.u8()?)?;
        let remaining = reader.bytes.len().checked_sub(reader.offset)?;
        let mut track = crate::agent_rpc_dispatch::Reader {
            bytes: reader.take(remaining)?,
            offset: 0,
        };
        let (submission, _) = crate::agent_rpc_dispatch::decode_observation(&mut track)?;
        track.finish()?;
        let mut out = Map::new();
        out.insert("submission".into(), submission);
        out.insert(
            "actual_verification_level".into(),
            Value::String(level_wire(actual).into()),
        );
        out.insert("deadline_elapsed".into(), Value::Bool(deadline_elapsed));
        Some((Value::Object(out), Some(actual)))
    })
}

pub(crate) fn read_module_state<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: ModuleStateWire = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let requested = requested_level(&request.requested_verification_level, id)?;
    let typed = layerx_agent_api::read::ReadRequest {
        selector: layerx_agent_api::read::ModuleStateSelector {
            module: layerx_agent_api::read::ModuleRef::new(request.module)
                .map_err(|_| malformed(id))?,
            key: layerx_agent_api::prepare::CanonicalBytes::new(lower_hex_bytes(&request.key, id)?)
                .map_err(|_| malformed(id))?,
        },
        requested_verification_level: requested,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.read_module_state(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let achieved = level(reader.u8()?)?;
        let canonical = reader.bytes()?;
        let proof = reader.opt_bytes()?;
        if achieved < requested {
            return None;
        }
        let mut out = Map::new();
        out.insert("canonical_value".into(), hexv(canonical));
        out.insert("proof".into(), if proof.is_empty() { Value::Null } else { hexv(proof) });
        Some((Value::Object(out), Some(achieved)))
    })
}

pub(crate) fn read_history<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: HistoryWire = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let requested = requested_level(&request.requested_verification_level, id)?;
    let selector = layerx_agent_api::read::HistorySelector {
        first: canonical_sequence(&request.range.first, id)?,
        last: canonical_sequence(&request.range.last, id)?,
        cursor: request
            .cursor
            .map(|text| layerx_agent_api::read::HistoryCursor::new(text).map_err(|_| malformed(id)))
            .transpose()?,
        page_limit: request
            .page_limit
            .parse::<u32>()
            .ok()
            .filter(|value| value.to_string() == request.page_limit)
            .ok_or_else(|| malformed(id))?,
    }
    .validate()
    .map_err(|_| malformed(id))?;
    let typed = layerx_agent_api::read::ReadRequest {
        selector,
        requested_verification_level: requested,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.read_history(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let count = reader.u16()?;
        let mut items = Vec::with_capacity(usize::from(count));
        let mut lowest: Option<Level> = None;
        for _ in 0..count {
            let global_sequence = reader.u64()?;
            let kind = reader.u8()?;
            let achieved = level(reader.u8()?)?;
            let canonical = reader.bytes()?;
            let proof = reader.opt_bytes()?;
            if achieved < requested {
                return None;
            }
            lowest = Some(lowest.map_or(achieved, |current| current.min(achieved)));
            let mut item = Map::new();
            item.insert("global_sequence".into(), dec(global_sequence));
            item.insert("kind".into(), dec(kind));
            item.insert(
                "achieved_verification_level".into(),
                Value::String(level_wire(achieved).into()),
            );
            item.insert("canonical".into(), hexv(canonical));
            item.insert("proof".into(), if proof.is_empty() { Value::Null } else { hexv(proof) });
            items.push(Value::Object(item));
        }
        let cursor = match reader.u8()? {
            0 => Value::Null,
            1 => {
                let mut cursor = Map::new();
                cursor.insert("next_sequence".into(), dec(reader.u64()?));
                cursor.insert("end_sequence".into(), dec(reader.u64()?));
                cursor.insert("head_sequence".into(), dec(reader.u64()?));
                cursor.insert("checkpoint".into(), hexv(&reader.fixed::<32>()?));
                Value::Object(cursor)
            }
            _ => return None,
        };
        let mut out = Map::new();
        out.insert("items".into(), Value::Array(items));
        out.insert("cursor".into(), cursor);
        Some((Value::Object(out), lowest))
    })
}

pub(crate) fn read_batch<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let request: BatchWire = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let requested = requested_level(&request.requested_verification_level, id)?;
    let typed = layerx_agent_api::read::ReadRequest {
        selector: layerx_agent_api::read::BatchRef::new(request.batch).map_err(|_| malformed(id))?,
        requested_verification_level: requested,
    };
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.read_batch(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let batch_number = reader.u64()?;
        let first_sequence = reader.u64()?;
        let last_sequence = reader.u64()?;
        let achieved = level(reader.u8()?)?;
        let header = reader.bytes()?;
        if first_sequence > last_sequence || achieved < requested {
            return None;
        }
        let mut out = Map::new();
        out.insert("batch_number".into(), dec(batch_number));
        out.insert("first_sequence".into(), dec(first_sequence));
        out.insert("last_sequence".into(), dec(last_sequence));
        out.insert("canonical_header".into(), hexv(header));
        Some((Value::Object(out), Some(achieved)))
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SessionContextWire {
    tenant: String,
    agent_did: String,
    authority_ref: String,
    permitted_activity_types: Vec<String>,
    expiry: String,
    client: String,
    policy_version: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SessionTargetWire {
    session_id: String,
    context: SessionContextWire,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SessionListWire {
    context: SessionContextWire,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WaitWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    submission_ref: String,
    requested_verification_level: String,
    deadline: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ModuleStateWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    module: String,
    key: String,
    requested_verification_level: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct HistoryRangeWire {
    first: String,
    last: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct HistoryWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    range: HistoryRangeWire,
    cursor: Option<String>,
    page_limit: String,
    requested_verification_level: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BatchWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    batch: String,
    requested_verification_level: String,
}

/// Builds the typed session context and refuses coordinates that differ from the bound
/// principal (tenant.rs `CoordinateMismatch`, mapped to `session.not_authorized` by AMEND 18).
fn session_context(
    wire: SessionContextWire,
    context: &RpcOwnerContext<'_>,
    id: RequestId,
) -> Result<layerx_agent_api::identity::SessionContext, Rejection> {
    use layerx_agent_api::identity::{
        ActivityType, AgentDid, AuthorityRef, ClientId, ExplicitSet, PolicyVersion,
        SessionContext, TenantId,
    };
    let principal = context.principal();
    if wire.tenant != principal.tenant.as_str()
        || wire.agent_did.as_bytes() != principal.agent.as_bytes()
    {
        return Err(rejection(ErrorClass::PolicyRefusal, id, "session.not_authorized"));
    }
    let activity_types = wire
        .permitted_activity_types
        .iter()
        .map(|text| {
            text.parse::<u16>()
                .ok()
                .filter(|value| value.to_string() == *text)
                .map(ActivityType)
                .ok_or_else(|| malformed(id))
        })
        .collect::<Result<Vec<_>, _>>()?;
    SessionContext::new(
        TenantId::new(wire.tenant).map_err(|_| malformed(id))?,
        AgentDid::new(wire.agent_did).map_err(|_| malformed(id))?,
        AuthorityRef::new(wire.authority_ref).map_err(|_| malformed(id))?,
        ExplicitSet::allow(activity_types),
        layerx_agent_api::generated::TimestampSeconds::parse_decimal(&wire.expiry)
            .ok()
            .filter(|value| value.get().to_string() == wire.expiry)
            .ok_or_else(|| malformed(id))?,
        ClientId::new(wire.client).map_err(|_| malformed(id))?,
        PolicyVersion::new(wire.policy_version).map_err(|_| malformed(id))?,
    )
    .map_err(|_| malformed(id))
}

fn canonical_sequence(text: &str, id: RequestId) -> Result<layerx_agent_api::generated::Sequence, Rejection> {
    layerx_agent_api::generated::Sequence::parse_decimal(text)
        .ok()
        .filter(|value| value.get().to_string() == text)
        .ok_or_else(|| malformed(id))
}

fn lower_hex_bytes(text: &str, id: RequestId) -> Result<Vec<u8>, Rejection> {
    let bytes = text.as_bytes();
    if bytes.is_empty()
        || bytes.len() % 2 != 0
        || !bytes.iter().all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
    {
        return Err(malformed(id));
    }
    bytes
        .chunks_exact(2)
        .map(|pair| {
            std::str::from_utf8(pair)
                .ok()
                .and_then(|pair| u8::from_str_radix(pair, 16).ok())
                .ok_or_else(|| malformed(id))
        })
        .collect()
}

/// Wire names as the SDKs send them (`program_http.rs` REQUESTED_VERIFICATION, config.rs).
fn requested_level(text: &str, id: RequestId) -> Result<Level, Rejection> {
    Ok(match text {
        "unverified" => Level::Unverified,
        "sequencer-signed" => Level::SequencerSigned,
        "batch-included" => Level::BatchIncluded,
        "state-proven" => Level::StateProven,
        "checkpoint-finalised" => Level::CheckpointFinalised,
        "settlement-anchored" => Level::SettlementAnchored,
        _ => return Err(malformed(id)),
    })
}

const fn level_wire(value: Level) -> &'static str {
    match value {
        Level::Unverified => "unverified",
        Level::SequencerSigned => "sequencer-signed",
        Level::BatchIncluded => "batch-included",
        Level::StateProven => "state-proven",
        Level::CheckpointFinalised => "checkpoint-finalised",
        Level::SettlementAnchored => "settlement-anchored",
    }
}

/// AMEND 28b SESSION RECORD; returns the JSON record, its `open` flag and its session id.
fn decode_session_record(reader: &mut Reader<'_>) -> Option<(Value, bool, [u8; 32])> {
    let session_id: [u8; 32] = reader.fixed()?;
    let token_id: [u8; 32] = reader.fixed()?;
    let agent_did = reader.text()?;
    let generation = reader.u64()?;
    let open = match reader.u8()? {
        0 => false,
        1 => true,
        _ => return None,
    };
    let expiry_sequence = reader.u64()?;
    let sequence = reader.u64()?;
    let mut out = Map::new();
    out.insert("session_id".into(), hexv(&session_id));
    out.insert("token_id".into(), hexv(&token_id));
    out.insert("agent_did".into(), Value::String(agent_did));
    out.insert("generation".into(), dec(generation));
    out.insert("open".into(), Value::Bool(open));
    out.insert("expiry_sequence".into(), dec(expiry_sequence));
    out.insert("sequence".into(), dec(sequence));
    Some((Value::Object(out), open, session_id))
}

fn owner_payload(
    id: RequestId,
    response: Result<HumanResponse, HumanOperationError>,
    decoder: impl FnOnce(&mut Reader<'_>) -> Option<(Value, Option<Level>)>,
) -> Result<Dispatched, Rejection> {
    let response = response.map_err(|error| owner_error(id, error))?;
    let mut reader = Reader {
        bytes: response.bytes(),
        offset: 0,
    };
    let (value, achieved) = decoder(&mut reader)
        .filter(|_| reader.finish().is_some())
        .ok_or_else(|| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?;
    Ok(Dispatched {
        value,
        verification: achieved.map(VerificationStatus::Achieved),
    })
}

impl<'a> Reader<'a> {
    fn u16(&mut self) -> Option<u16> {
        Some(u16::from_be_bytes(self.fixed()?))
    }
    fn u32(&mut self) -> Option<u32> {
        Some(u32::from_be_bytes(self.fixed()?))
    }
    fn bytes(&mut self) -> Option<&'a [u8]> {
        let length = usize::try_from(self.u32()?).ok()?;
        (length != 0).then_some(())?;
        self.take(length)
    }
    fn text(&mut self) -> Option<String> {
        String::from_utf8(self.bytes()?.to_vec()).ok()
    }
    fn opt_bytes(&mut self) -> Option<&'a [u8]> {
        let length = usize::try_from(self.u32()?).ok()?;
        self.take(length)
    }
}
