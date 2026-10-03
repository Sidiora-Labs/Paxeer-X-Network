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
use crate::agent_rpc_wire::Canonical;
use crate::human::HumanRefusal;
use layerx_agent_api::budget::{AuthorityResponse, BudgetRecord};
use layerx_agent_api::read::Freshness;
use sha2::{Digest, Sha256};

pub(crate) fn sign<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &crate::agent_rpc_peer::RpcOwnerContext<'_>,
    request: &serde_json::Map<String, serde_json::Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::{decode_observation, dispatched, mutation_key};
    use crate::agent_rpc_wire::{decode_wire, SignRequestWire};
    let id = ctx.request_id;
    let typed = decode_wire::<SignRequestWire>(request, id)?.into_request(id)?;
    mutation_key(ctx)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_sign(context, typed));
    dispatched(id, response, decode_observation)
}

pub(crate) fn subscription_create<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionCreateWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::subscription_create_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_create(context, envelope))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_record_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_list<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionListWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_list(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_list_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_pause<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionTargetWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_pause(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_record_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_resume<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionTargetWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_resume(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_record_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_delete<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionTargetWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_delete(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_null_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_health<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::SubscriptionTargetWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_health(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_health_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn subscription_acknowledge<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::CursorAcknowledgementWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_subscription_acknowledge(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: subscription_record_value(id, response.bytes())?,
        verification: None,
    })
}

fn subscription_payload(
    id: RequestId,
    payload: &[u8],
    decoder: impl FnOnce(&mut Reader<'_>) -> Option<Value>,
) -> Result<Value, Rejection> {
    let mut reader = Reader {
        bytes: payload,
        offset: 0,
    };
    decoder(&mut reader)
        .filter(|_| reader.finish().is_some())
        .ok_or_else(|| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))
}

fn subscription_decimal(reader: &mut Reader<'_>, id: RequestId) -> Option<u64> {
    crate::agent_rpc_dispatch::decimal_u64(&reader.text()?, id).ok()
}

fn subscription_flag(reader: &mut Reader<'_>) -> Option<bool> {
    match reader.u8()? {
        0 => Some(false),
        1 => Some(true),
        _ => None,
    }
}

fn subscription_optional_decimal(reader: &mut Reader<'_>, id: RequestId) -> Option<Value> {
    match reader.u8()? {
        0 => Some(Value::Null),
        1 => Some(dec(subscription_decimal(reader, id)?)),
        _ => None,
    }
}

fn subscription_scope(reader: &mut Reader<'_>) -> Option<Value> {
    let tenant = reader.text()?;
    let agent = reader.text()?;
    let capability = reader.text()?;
    Some(serde_json::json!({
        "tenant": tenant,
        "agent": agent,
        "capability": capability,
    }))
}

fn subscription_tenant_objects(reader: &mut Reader<'_>) -> Option<Value> {
    let count = reader.u16()?;
    let mut items = Vec::with_capacity(usize::from(count));
    for _ in 0..count {
        let tenant = reader.text()?;
        let value = reader.text()?;
        items.push(serde_json::json!({"tenant": tenant, "value": value}));
    }
    Some(Value::Array(items))
}

fn subscription_activity_types(reader: &mut Reader<'_>, id: RequestId) -> Option<Value> {
    let count = reader.u16()?;
    let mut items = Vec::with_capacity(usize::from(count));
    for _ in 0..count {
        items.push(dec(u16::try_from(subscription_decimal(reader, id)?).ok()?));
    }
    Some(Value::Array(items))
}

fn subscription_result_classes(reader: &mut Reader<'_>) -> Option<Value> {
    let count = reader.u16()?;
    let mut items = Vec::with_capacity(usize::from(count));
    for _ in 0..count {
        items.push(Value::from(i32::from_be_bytes(reader.fixed()?)));
    }
    Some(Value::Array(items))
}

fn subscription_record(reader: &mut Reader<'_>, id: RequestId) -> Option<Value> {
    let subscription_id = reader.text()?;
    let scope = subscription_scope(reader)?;
    let agents = subscription_tenant_objects(reader)?;
    let accounts = subscription_tenant_objects(reader)?;
    let modules = subscription_tenant_objects(reader)?;
    let assets = subscription_tenant_objects(reader)?;
    let counterparties = subscription_tenant_objects(reader)?;
    let activity_types = subscription_activity_types(reader, id)?;
    let result_classes = subscription_result_classes(reader)?;
    let start = subscription_decimal(reader, id)?;
    let last_acknowledged = subscription_decimal(reader, id)?;
    let delivery_target = reader.text()?;
    let paused = subscription_flag(reader)?;
    Some(serde_json::json!({
        "subscription_id": subscription_id,
        "scope": scope,
        "filter": {
            "agents": agents,
            "accounts": accounts,
            "modules": modules,
            "assets": assets,
            "counterparties": counterparties,
            "activity_types": activity_types,
            "result_classes": result_classes,
        },
        "start": dec(start),
        "last_acknowledged": dec(last_acknowledged),
        "delivery_target": delivery_target,
        "paused": paused,
    }))
}

fn subscription_record_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    subscription_payload(id, payload, |reader| subscription_record(reader, id))
}

fn subscription_list_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    subscription_payload(id, payload, |reader| {
        let count = reader.u32()?;
        let mut records = Vec::new();
        for _ in 0..count {
            records.push(subscription_record(reader, id)?);
        }
        Some(Value::Array(records))
    })
}

fn subscription_health_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    subscription_payload(id, payload, |reader| {
        let scope = subscription_scope(reader)?;
        let subscription_id = reader.text()?;
        let last_acknowledged = subscription_decimal(reader, id)?;
        let last_delivery_at = subscription_optional_decimal(reader, id)?;
        let pending_backfill = match reader.u8()? {
            0 => Value::Null,
            1 => {
                let missing_first = subscription_decimal(reader, id)?;
                let missing_last = subscription_decimal(reader, id)?;
                let backfill_cursor = subscription_decimal(reader, id)?;
                let backfill_attempted = subscription_flag(reader)?;
                serde_json::json!({
                    "missing_first": dec(missing_first),
                    "missing_last": dec(missing_last),
                    "backfill_cursor": dec(backfill_cursor),
                    "backfill_attempted": backfill_attempted,
                })
            }
            _ => return None,
        };
        Some(serde_json::json!({
            "target": {"scope": scope, "subscription_id": subscription_id},
            "last_acknowledged": dec(last_acknowledged),
            "last_delivery_at": last_delivery_at,
            "pending_backfill": pending_backfill,
        }))
    })
}

fn subscription_null_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    subscription_payload(id, payload, |reader| (reader.u8()? == 0).then_some(Value::Null))
}

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
        HumanOperationError::CapabilityRefused(dimension) => rejection(
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

        HumanOperationError::Typed(refusal) => {
            rejection(refusal.class(), request_id, refusal.reason())
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

pub(crate) fn approval_list_native<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::dispatched_native;
    use crate::agent_rpc_wire::{decode_wire, NativeApprovalListV1Wire, NativeApprovalListResultV1Wire};
    let id = ctx.request_id;
    decode_wire::<NativeApprovalListV1Wire>(request, id)?.into_request(id)?;
    let response = owner.lock().and_then(|mut guard| guard.rpc_approval_list_native(context));
    dispatched_native(id, response, NativeApprovalListResultV1Wire::into_result)
}

pub(crate) fn approval_get_native<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::dispatched_native;
    use crate::agent_rpc_wire::{decode_wire, NativeApprovalGetV1Wire, NativeApprovalResultV1Wire};
    let id = ctx.request_id;
    let typed = decode_wire::<NativeApprovalGetV1Wire>(request, id)?.into_request(id)?;
    let response = owner.lock().and_then(|mut guard| guard.rpc_approval_get_native(context, typed));
    dispatched_native(id, response, NativeApprovalResultV1Wire::into_result)
}

fn approval_decision_native<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext, grant: bool,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::{dispatched_native, malformed, mutation_key, native_approval_digest};
    use crate::agent_rpc_wire::{decode_wire, NativeApprovalDecisionV1Wire, NativeApprovalResultV1Wire};
    let id = ctx.request_id;
    let typed = decode_wire::<NativeApprovalDecisionV1Wire>(request, id)?.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: native_approval_digest(&typed, grant).map_err(|_| malformed(id))?,
        operation: typed,
    };
    let response = owner.lock().and_then(|mut guard| guard.rpc_approval_decide_native(context, envelope, grant));
    dispatched_native(id, response, NativeApprovalResultV1Wire::into_result)
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
    if crate::agent_rpc_dispatch::native_variant(request, id)? {
        return approval_decision_native(owner, context, request, ctx, approve);
    }
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
    if crate::agent_rpc_dispatch::native_variant(request, id)? {
        use crate::agent_rpc_dispatch::{dispatched_native, malformed, native_prepare_digest};
        use crate::agent_rpc_wire::{decode_wire, NativePrepareV1Wire, NativePrepareResultV1Wire};
        let typed = decode_wire::<NativePrepareV1Wire>(request, id)?.into_request(id)?;
        let envelope = crate::human::MutationEnvelope {
            request_id: id.0,
            key: mutation_key(ctx)?,
            body_digest: native_prepare_digest(&typed).map_err(|_| malformed(id))?,
            operation: typed,
        };
        let response = owner.lock().and_then(|mut guard| guard.rpc_prepare_native(context, envelope));
        return dispatched_native(id, response, NativePrepareResultV1Wire::into_result);
    }
    let typed = human_prepare(decode(request, id)?, id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::capability::binding::prepare_body_digest(
            crate::human_runtime::prepare_digest(&typed),
            typed.capability_id.as_ref(),
        ),
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
            .map(|text| {
                crate::read::Cursor::from_hex(&text).map_err(|_| malformed(id))?;
                layerx_agent_api::read::HistoryCursor::new(text).map_err(|_| malformed(id))
            })
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
                let text = std::str::from_utf8(reader.bytes()?).ok()?;
                crate::read::Cursor::from_hex(text).ok()?;
                Value::String(text.to_owned())
            }
            _ => return None,
        };
        let mut out = Map::new();
        out.insert("items".into(), Value::Array(items));
        out.insert("cursor".into(), cursor);
        Some((Value::Object(out), lowest))
    })
}

pub(crate) fn availability_fetch<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    fn classes(reader: &mut Reader<'_>) -> Option<Value> {
        let count = reader.u16()?;
        let mut items = Vec::with_capacity(usize::from(count));
        for _ in 0..count {
            let class = match reader.u8()? {
                1 => "activities",
                2 => "receipts",
                3 => "oracle",
                4 => "state_diff",
                5 => "recovery",
                _ => return None,
            };
            let complete = match reader.u8()? {
                0 => false,
                1 => true,
                _ => return None,
            };
            let verified_chunks = reader.u32()?;
            let verified_bytes = reader.u64()?;
            let failure = reader.opt_bytes()?;
            if complete != failure.is_empty() {
                return None;
            }
            let mut item = Map::new();
            item.insert("class".into(), Value::String(class.into()));
            item.insert("complete".into(), Value::Bool(complete));
            item.insert("verified_chunks".into(), dec(verified_chunks));
            item.insert("verified_bytes".into(), dec(verified_bytes));
            item.insert(
                "failure".into(),
                if failure.is_empty() {
                    Value::Null
                } else {
                    Value::String(std::str::from_utf8(failure).ok()?.to_owned())
                },
            );
            items.push(Value::Object(item));
        }
        Some(Value::Array(items))
    }
    let id = ctx.request_id;
    let request: AvailabilityWire = decode(request, id)?;
    let _ = (request.tenant, request.agent);
    let requested = requested_level(&request.requested_verification_level, id)?;
    let typed = layerx_agent_api::availability::AvailabilityRequest {
        selector: request.selector,
        requested_verification_level: requested,
        maximum_bytes: request
            .maximum_bytes
            .parse::<u64>()
            .ok()
            .filter(|value| value.to_string() == request.maximum_bytes)
            .ok_or_else(|| malformed(id))?,
        maximum_chunks: request
            .maximum_chunks
            .parse::<u32>()
            .ok()
            .filter(|value| value.to_string() == request.maximum_chunks)
            .ok_or_else(|| malformed(id))?,
        deadline: layerx_agent_api::generated::TimestampSeconds::parse_decimal(&request.deadline)
            .ok()
            .filter(|value| value.get().to_string() == request.deadline)
            .ok_or_else(|| malformed(id))?,
    }
    .validate()
    .map_err(|_| malformed(id))?;
    let mut guard = owner.lock().map_err(|error| owner_error(id, error))?;
    let response = guard.availability_fetch(context.peer(), typed);
    owner_payload(id, response, |reader| {
        let achieved = level(reader.u8()?)?;
        if achieved < requested {
            return None;
        }
        let mut completion = Map::new();
        match reader.u8()? {
            0 => {
                completion.insert("state".into(), Value::String("partial".into()));
            }
            1 => {
                completion.insert("state".into(), Value::String("complete".into()));
                completion.insert("provider".into(), Value::String(reader.text()?));
            }
            _ => return None,
        }
        let top = classes(reader)?;
        let count = reader.u16()?;
        let mut providers = Vec::with_capacity(usize::from(count));
        for _ in 0..count {
            let provider = reader.text()?;
            let provider_classes = classes(reader)?;
            let failure = reader.opt_bytes()?;
            let mut item = Map::new();
            item.insert("provider".into(), Value::String(provider));
            item.insert("classes".into(), provider_classes);
            item.insert(
                "failure".into(),
                if failure.is_empty() {
                    Value::Null
                } else {
                    Value::String(std::str::from_utf8(failure).ok()?.to_owned())
                },
            );
            providers.push(Value::Object(item));
        }
        let mut out = Map::new();
        out.insert("completion".into(), Value::Object(completion));
        out.insert("classes".into(), top);
        out.insert("providers".into(), Value::Array(providers));
        Some((Value::Object(out), Some(achieved)))
    })
}

pub(crate) fn capability_create<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::CapabilityCreateWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::capability_create_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_capability_create(context, envelope))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: capability_record_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn capability_attenuate<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::CapabilityAttenuateWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::capability_attenuate_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_capability_attenuate(context, envelope))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: capability_record_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn capability_list<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::CapabilityListWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_capability_list(context, typed))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: capability_records_value(id, response.bytes())?,
        verification: None,
    })
}

pub(crate) fn capability_revoke<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    let id = ctx.request_id;
    let wire: crate::agent_rpc_wire::CapabilityRevokeWire = decode(request, id)?;
    let typed = wire.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: crate::human_runtime::capability_revoke_digest(&typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_capability_revoke(context, envelope))
        .map_err(|error| owner_error(id, error))?;
    Ok(Dispatched {
        value: capability_record_value(id, response.bytes())?,
        verification: None,
    })
}

/// Owner capability payloads are bounded by `HumanResponse::new` (human.rs `MAX_BYTES`,
/// 1 MiB); every count is a u16 and every element is read from the remaining bytes, so a
/// record list holds at most 65 535 records and never more than the payload encodes.
fn capability_payload(
    id: RequestId,
    payload: &[u8],
    decoder: impl FnOnce(&mut Reader<'_>) -> Option<Value>,
) -> Result<Value, Rejection> {
    let mut reader = Reader {
        bytes: payload,
        offset: 0,
    };
    decoder(&mut reader)
        .filter(|_| reader.finish().is_some())
        .ok_or_else(|| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))
}

/// Authority block: text tenant, text agent_did, text authority_ref, non-empty bytes
/// protocol_authority. Returns the JSON authority and its tenant and agent_did.
fn capability_authority(reader: &mut Reader<'_>) -> Option<(Value, String, String)> {
    let tenant = reader.text()?;
    let agent_did = reader.text()?;
    let authority_ref = reader.text()?;
    let protocol_authority = reader.bytes()?;
    let mut out = Map::new();
    out.insert("tenant".into(), Value::String(tenant.clone()));
    out.insert("agent_did".into(), Value::String(agent_did.clone()));
    out.insert("authority_ref".into(), Value::String(authority_ref));
    out.insert("protocol_authority".into(), hexv(protocol_authority));
    Some((Value::Object(out), tenant, agent_did))
}

fn capability_hex_id(reader: &mut Reader<'_>, id: RequestId) -> Option<(String, [u8; 32])> {
    let text = reader.text()?;
    let bytes = hex32(&text, id).ok()?;
    Some((text, bytes))
}

fn capability_unique<T: Ord + Clone>(values: &[T]) -> Option<()> {
    let set: std::collections::BTreeSet<T> = values.iter().cloned().collect();
    (set.len() == values.len()).then_some(())
}

fn capability_texts(reader: &mut Reader<'_>) -> Option<Vec<String>> {
    let count = reader.u16()?;
    let mut items = Vec::new();
    for _ in 0..count {
        items.push(reader.text()?);
    }
    capability_unique(&items)?;
    Some(items)
}

fn capability_strings(items: &[String]) -> Value {
    Value::Array(items.iter().map(|item| Value::String(item.clone())).collect())
}

/// One record: text capability_id (64 lowercase hex), u8 parent tag + text parent_id,
/// u16 count + u16 activity types, u16 count + text counterparties, u16 count + text assets,
/// u16 count + (text asset, u128 amount), u16 count + (u64 window_seconds,
/// u64 maximum_actions), u16 count + text purposes, u64 expiry_seconds, u64 created_at_ms,
/// u64 created_at_sequence, u8 state (0 active, 1 revoked, 2 expired), u8 revoked tag +
/// (u64 revoked_at_ms, u64 revoked_at_sequence). Duplicates, a zero window, a ceiling for
/// an asset outside `assets`, and a state that disagrees with the revoked tag are malformed.
fn capability_record(
    reader: &mut Reader<'_>,
    id: RequestId,
    tenant: &str,
    agent_did: &str,
) -> Option<(Value, [u8; 32])> {
    let (capability_id, capability_bytes) = capability_hex_id(reader, id)?;
    let parent_id = match reader.u8()? {
        0 => Value::Null,
        1 => Value::String(capability_hex_id(reader, id)?.0),
        _ => return None,
    };
    let activity_count = reader.u16()?;
    let mut activity_types = Vec::new();
    for _ in 0..activity_count {
        activity_types.push(reader.u16()?);
    }
    capability_unique(&activity_types)?;
    let counterparties = capability_texts(reader)?;
    let assets = capability_texts(reader)?;
    let amount_count = reader.u16()?;
    let mut amount_assets = Vec::new();
    let mut amount_ceilings = Vec::new();
    for _ in 0..amount_count {
        let asset = reader.text()?;
        let amount = reader.u128()?;
        if !assets.contains(&asset) {
            return None;
        }
        amount_assets.push(asset.clone());
        let mut ceiling = Map::new();
        ceiling.insert("asset".into(), Value::String(asset));
        ceiling.insert("amount".into(), dec(amount));
        amount_ceilings.push(Value::Object(ceiling));
    }
    capability_unique(&amount_assets)?;
    let rate_count = reader.u16()?;
    let mut windows = Vec::new();
    let mut rate_ceilings = Vec::new();
    for _ in 0..rate_count {
        let window_seconds = reader.u64()?;
        let maximum_actions = reader.u64()?;
        if window_seconds == 0 {
            return None;
        }
        windows.push(window_seconds);
        let mut ceiling = Map::new();
        ceiling.insert("window_seconds".into(), dec(window_seconds));
        ceiling.insert("maximum_actions".into(), dec(maximum_actions));
        rate_ceilings.push(Value::Object(ceiling));
    }
    capability_unique(&windows)?;
    let purposes = capability_texts(reader)?;
    let expiry = reader.u64()?;
    let created_at_ms = reader.u64()?;
    let created_at_sequence = reader.u64()?;
    let state = match reader.u8()? {
        0 => "active",
        1 => "revoked",
        2 => "expired",
        _ => return None,
    };
    let (revoked_at_ms, revoked_at_sequence) = match reader.u8()? {
        0 => (Value::Null, Value::Null),
        1 => (dec(reader.u64()?), dec(reader.u64()?)),
        _ => return None,
    };
    if (state == "revoked") == revoked_at_ms.is_null() {
        return None;
    }
    let mut dimensions = Map::new();
    dimensions.insert(
        "activity_types".into(),
        Value::Array(activity_types.into_iter().map(dec).collect()),
    );
    dimensions.insert("counterparties".into(), capability_strings(&counterparties));
    dimensions.insert("assets".into(), capability_strings(&assets));
    dimensions.insert("amount_ceilings".into(), Value::Array(amount_ceilings));
    dimensions.insert("rate_ceilings".into(), Value::Array(rate_ceilings));
    dimensions.insert("purpose_constraints".into(), capability_strings(&purposes));
    dimensions.insert("expiry".into(), dec(expiry));
    let mut out = Map::new();
    out.insert("capability_id".into(), Value::String(capability_id));
    out.insert("parent_id".into(), parent_id);
    out.insert("tenant".into(), Value::String(tenant.to_owned()));
    out.insert("agent_did".into(), Value::String(agent_did.to_owned()));
    out.insert("dimensions".into(), Value::Object(dimensions));
    out.insert("state".into(), Value::String(state.into()));
    out.insert("created_at_ms".into(), dec(created_at_ms));
    out.insert("created_at_sequence".into(), dec(created_at_sequence));
    out.insert("revoked_at_ms".into(), revoked_at_ms);
    out.insert("revoked_at_sequence".into(), revoked_at_sequence);
    Some((Value::Object(out), capability_bytes))
}

/// `AuthorityResponse<CapabilityRecord>`: authority block, then one record.
fn capability_record_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    capability_payload(id, payload, |reader| {
        let (authority, tenant, agent_did) = capability_authority(reader)?;
        let (record, _) = capability_record(reader, id, &tenant, &agent_did)?;
        let mut out = Map::new();
        out.insert("authority".into(), authority);
        out.insert("value".into(), record);
        Some(Value::Object(out))
    })
}

/// `AuthorityResponse<CapabilityRecords>`: authority block, then u16 count + records in
/// strictly ascending capability id order.
fn capability_records_value(id: RequestId, payload: &[u8]) -> Result<Value, Rejection> {
    capability_payload(id, payload, |reader| {
        let (authority, tenant, agent_did) = capability_authority(reader)?;
        let count = reader.u16()?;
        let mut records = Vec::new();
        let mut previous: Option<[u8; 32]> = None;
        for _ in 0..count {
            let (record, capability_id) = capability_record(reader, id, &tenant, &agent_did)?;
            if previous.is_some_and(|last| last >= capability_id) {
                return None;
            }
            previous = Some(capability_id);
            records.push(record);
        }
        let mut value = Map::new();
        value.insert("capabilities".into(), Value::Array(records));
        let mut out = Map::new();
        out.insert("authority".into(), authority);
        out.insert("value".into(), Value::Object(value));
        Some(Value::Object(out))
    })
}

/// Body digest of a budget mutation: a per-operation domain over the canonical JSON of the
/// converted request, which binds every signed-authorization carrier field when present.
fn budget_body_digest<T: Canonical>(domain: &[u8], typed: &T) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(domain);
    digest.update(typed.canonical().to_string().as_bytes());
    digest.finalize().into()
}

/// Body digest of a budget create: [`budget_body_digest`] under the create domain, followed by
/// the optional `TextV1` purpose label suffix, which is empty when the label is absent.
pub(crate) fn budget_create_body_digest<T: Canonical>(typed: &T, purpose: Option<&str>) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"LayerX/budget/create-body/v1\0");
    digest.update(typed.canonical().to_string().as_bytes());
    digest.update(crate::agent_rpc_wire::budget_create_purpose_suffix(purpose));
    digest.finalize().into()
}

pub(crate) fn budget_create<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    use crate::agent_rpc_wire::{decode_wire, BudgetCreateWire};
    let id = ctx.request_id;
    let wire = decode_wire::<BudgetCreateWire>(request, id)?;
    let purpose = wire.purpose().map(str::to_owned);
    let typed = wire.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: budget_create_body_digest(&typed, purpose.as_deref()),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| {
            let control = guard.session_control.clone();
            match purpose.as_deref() {
                None => guard.budget_create(context, &control, envelope),
                Some(purpose) => {
                    guard.budget_create_with_purpose(context, &control, envelope, purpose)
                }
            }
        })
        .map_err(|error| owner_error(id, error))?;
    Ok(budget_record_value(&response))
}

pub(crate) fn budget_fund<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    use crate::agent_rpc_wire::{decode_wire, BudgetFundWire};
    let id = ctx.request_id;
    let typed = decode_wire::<BudgetFundWire>(request, id)?.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: budget_body_digest(b"LayerX/budget/fund-body/v1\0", &typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| {
            let control = guard.session_control.clone();
            guard.budget_fund(context, &control, envelope)
        })
        .map_err(|error| owner_error(id, error))?;
    Ok(budget_record_value(&response))
}

pub(crate) fn budget_revoke<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_dispatch::mutation_key;
    use crate::agent_rpc_wire::{decode_wire, BudgetTargetWire};
    let id = ctx.request_id;
    let typed = decode_wire::<BudgetTargetWire>(request, id)?.into_request(id)?;
    let envelope = crate::human::MutationEnvelope {
        request_id: id.0,
        key: mutation_key(ctx)?,
        body_digest: budget_body_digest(b"LayerX/budget/revoke-body/v1\0", &typed),
        operation: typed,
    };
    let response = owner
        .lock()
        .and_then(|mut guard| {
            let control = guard.session_control.clone();
            guard.budget_revoke(context, &control, envelope)
        })
        .map_err(|error| owner_error(id, error))?;
    Ok(budget_record_value(&response))
}

/// `AuthorityResponse<BudgetRecord>`: a protocol budget reports the level its state was proven
/// at; a daemon limit reports no level and always carries its bypass notice.
fn budget_record_value(response: &AuthorityResponse<crate::human::BudgetState>) -> Dispatched {
    let state = &response.value;
    let (value, verification) =
        budget_record_wire(&state.record, state.proven_head, state.activity_id.as_ref());
    Dispatched {
        value: serde_json::json!({
            "authority": authority_wire(&response.authority),
            "value": value,
        }),
        verification,
    }
}

fn authority_wire(authority: &layerx_agent_api::budget::AuthorityDescription) -> Value {
    serde_json::json!({
        "tenant": authority.tenant.as_str(),
        "agent_did": authority.agent_did.as_str(),
        "authority_ref": authority.authority_ref.as_str(),
        "protocol_authority": hexv(&authority.protocol_authority),
    })
}

/// The one `BudgetRecord` wire encoding and the verification it states.
fn budget_record_wire(
    record: &BudgetRecord,
    head: u64,
    activity_id: Option<&[u8; 32]>,
) -> (Value, Option<VerificationStatus>) {
    let level = crate::agent_rpc_dispatch::level_name;
    let (mut value, verification) = match record {
        BudgetRecord::Protocol(view) => (
            serde_json::json!({
                "enforcement": "ProtocolBudget",
                "budget_id": hexv(&view.budget_id),
                "owner": hexv(&view.owner),
                "budget_account": hexv(&view.budget_account),
                "asset_id": hexv(&view.asset_id),
                "purpose_hash": hexv(&view.purpose_hash),
                "per_period_limit": dec(view.per_period_limit.0),
                "configured_period_limit": dec(view.configured_period_limit.0),
                "carry_cap": dec(view.carry_cap.0),
                "spent_this_period": dec(view.spent_this_period.0),
                "carried": dec(view.carried.0),
                "period_length_ms": dec(view.period_length_ms),
                "period_start_ms": dec(view.period_start_ms),
                "expiry_ms": dec(view.expiry_ms),
                "revocation_counter": dec(view.revocation_counter),
                "rollover_policy": dec(view.rollover_policy),
                "closed": view.closed,
                "revoked": view.revoked,
                "delegates": view.delegates.iter().map(|delegate| hexv(delegate)).collect::<Vec<_>>(),
                "source_account": view.source_account.as_ref().map(|account| hexv(account)),
                "achieved_verification_level": level(view.achieved_verification_level),
            }),
            Some(VerificationStatus::Achieved(
                view.achieved_verification_level,
            )),
        ),
        BudgetRecord::Daemon(view) => (
            serde_json::json!({
                "enforcement": "DaemonLimit",
                "budget_id": hexv(&view.budget_id),
                "asset": hexv(&view.asset),
                "ceiling": dec(view.ceiling.0),
                "consumed": dec(view.consumed.0),
                "expiry_ms": dec(view.expiry_ms),
                "revoked": view.revoked,
                "notice": view.notice(),
            }),
            None,
        ),
    };
    if let Value::Object(fields) = &mut value {
        fields.insert("head".into(), dec(head));
        fields.insert(
            "activity_id".into(),
            activity_id.map_or(Value::Null, |id| hexv(id)),
        );
    }
    (value, verification)
}

/// `budget.state`: the owner reads the budget through the authenticated session; the record
/// keeps its own verification, and the state-proven balance, head and activity id are passed
/// through as the owner proved them.
pub(crate) fn budget_state<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let typed = crate::agent_rpc_wire::budget_state_request(request, id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| {
            let control = guard.session_control.clone();
            guard.budget_state(context, &control, typed)
        })
        .map_err(|error| owner_error(id, error))?;
    let state = &response.value;
    let (record, verification) =
        budget_record_wire(&state.record, state.proven_head, state.activity_id.as_ref());
    Ok(Dispatched {
        value: serde_json::json!({
            "authority": authority_wire(&response.authority),
            "value": {
                "record": record,
                "balance": dec(state.balance),
                "proven_head": dec(state.proven_head),
                "activity_id": state.activity_id.as_ref().map_or(Value::Null, |id| hexv(id)),
            },
        }),
        verification,
    })
}

fn freshness_value(freshness: &Freshness) -> Value {
    serde_json::json!({
        "chain_head": dec(freshness.chain_head.0),
        "latest_sealed_batch": freshness.latest_sealed_batch.as_str(),
        "latest_finalised_checkpoint": freshness.latest_finalised_checkpoint.as_str(),
        "value_sequence": dec(freshness.value_sequence.0),
        "relative_to": match &freshness.relative_to {
            layerx_agent_api::read::RelativeTo::Batch(batch) => serde_json::json!({"batch": batch.as_str()}),
            layerx_agent_api::read::RelativeTo::Checkpoint(checkpoint) => {
                serde_json::json!({"checkpoint": checkpoint.as_str()})
            }
        },
    })
}

pub(crate) fn read_proof_bundle<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ProofBundleWire};
    let id = ctx.request_id;
    let typed = decode_wire::<ProofBundleWire>(request, id)?.into_request(id)?;
    let response = owner.lock()
        .and_then(|mut guard| guard.rpc_read_proof_bundle(context, typed.clone()))
        .map_err(|error| owner_error(id, error))?;
    layerx_agent_api::proof::ProofBundle::check_response(&typed, &response)
        .map_err(|_| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?;
    let dispatched = Dispatched {
        value: serde_json::json!({
            "value": {
                "target": hexv(response.value.target.as_bytes()),
                "proofs": response.value.proofs.iter().map(|proof| hexv(proof.as_bytes())).collect::<Vec<_>>(),
            },
            "achieved_verification_level": crate::agent_rpc_dispatch::level_name(response.achieved_verification_level),
            "freshness": freshness_value(&response.freshness),
        }),
        verification: Some(VerificationStatus::Achieved(response.achieved_verification_level)),
    };
    if crate::agent_rpc::success_body(id, &dispatched, Some(Some(typed.requested_verification_level)))?
        .body.len() > crate::agent_rpc::MAX_BODY_BYTES {
        return Err(rejection(ErrorClass::InternalFault, id, "owner.response_malformed"));
    }
    Ok(dispatched)
}

/// `export.offline`: the fact set is decoded through the shared fact grammar, the requested
/// level is passed to the owner unchanged, and the response is accepted only when it states
/// exactly the requested facts at or above the requested level.
pub(crate) fn export_offline<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ExportOfflineWire};
    let id = ctx.request_id;
    let typed = decode_wire::<ExportOfflineWire>(request, id)?.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.export_offline(context.peer(), typed.clone()))
        .map_err(|error| owner_error(id, error))?;
    layerx_agent_api::export::check_export_response(&typed, &response)
        .map_err(|_| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?;
    let export = &response.value;
    let buckets = |items: &[layerx_agent_api::prepare::CanonicalBytes]| {
        items.iter().map(|item| hexv(item.as_bytes())).collect::<Vec<_>>()
    };
    Ok(Dispatched {
        value: serde_json::json!({
            "value": {
                "facts": export.facts.iter().map(|fact| fact.as_str()).collect::<Vec<_>>(),
                "receipts": buckets(&export.receipts),
                "proofs": buckets(&export.proofs),
                "certificates": buckets(&export.certificates),
                "headers": buckets(&export.headers),
            },
            "achieved_verification_level": crate::agent_rpc_dispatch::level_name(
                response.achieved_verification_level,
            ),
            "freshness": freshness_value(&response.freshness),
        }),
        verification: Some(VerificationStatus::Achieved(response.achieved_verification_level)),
    })
}

/// `project` is the fee projection. The legacy policy payload (`context` +
/// `canonical_intent`) is refused with its typed reason and never reinterpreted.
pub(crate) fn project<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, FeeProjectionWire, LegacyProjectWire};
    let id = ctx.request_id;
    if decode_wire::<LegacyProjectWire>(request, id).is_ok() {
        return Err(owner_error(
            id,
            HumanOperationError::Typed(HumanRefusal::Policy(
                crate::policy::PolicyDryRunRefusal::LegacyProjectPayload,
            )),
        ));
    }
    let typed = decode_wire::<FeeProjectionWire>(request, id)?.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.fee_projection(context.peer(), typed))
        .map_err(|error| owner_error(id, error))?;
    let fee = &response.projected;
    Ok(Dispatched {
        value: serde_json::json!({
            "projected": {
                "request": {
                    "protocol_activity_type": dec(fee.request.protocol_activity_type),
                    "canonical_bytes": dec(fee.request.canonical_bytes),
                    "execution_units": dec(fee.request.execution_units),
                    "storage_units": dec(fee.request.storage_units),
                },
                "parameter_version": dec(fee.parameter_version),
                "fee": dec(fee.fee.0),
                "canonical_schedule": hexv(fee.canonical_schedule.as_bytes()),
                "snapshot_sequence": dec(fee.snapshot_sequence.0),
                "snapshot_state_root": hexv(&fee.snapshot_state_root),
            },
            "rationale": response.rationale,
            "observed_freshness": freshness_value(&response.observed_freshness),
        }),
        verification: None,
    })
}

/// `policy.dry_run`: a local restriction evaluated by the owner; the owner bytes are the
/// policy explanation record, decoded strictly. Never a verification level.
pub(crate) fn policy_dry_run<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::PolicyDryRunShape;
    use crate::policy::{DecisionReason, EvaluationMode, Explanation, Outcome};
    use layerx_agent_api::policy::{PolicyDecisionReason, PolicyOutcome};
    let id = ctx.request_id;
    let malformed_response =
        || rejection(ErrorClass::InternalFault, id, "owner.response_malformed");
    let value = match crate::agent_rpc_wire::policy_dry_run_request(request, id)? {
        PolicyDryRunShape::Typed(typed) => {
            let response = owner
                .lock()
                .and_then(|mut guard| {
                    let control = guard.session_control.clone();
                    guard.policy_dry_run(context, &control, typed)
                })
                .map_err(|error| owner_error(id, error))?;
            let explanation = Explanation::from_machine_bytes(response.bytes())
                .map_err(|_| malformed_response())?;
            if explanation.mode != EvaluationMode::DryRun {
                return Err(malformed_response());
            }
            policy_dry_run_value(
                match explanation.outcome {
                    Outcome::Allow => "allow",
                    Outcome::Deny => "deny",
                },
                &explanation.policy_version,
                &explanation.matched_rules,
                explanation.deciding_rule.as_deref(),
                match explanation.reason {
                    DecisionReason::PermittedByRule => "permitted_by_rule",
                    DecisionReason::ExplicitDeny => "explicit_deny",
                    DecisionReason::ApprovalRequired => "approval_required",
                    DecisionReason::NoPermittingRule => "no_permitting_rule",
                    DecisionReason::InvalidContext => "invalid_context",
                    DecisionReason::EvaluationFailure => "evaluation_failure",
                },
                explanation.authority_statement,
            )
        }
        PolicyDryRunShape::Legacy(legacy) => {
            let response = owner
                .lock()
                .and_then(|mut guard| {
                    let control = guard.session_control.clone();
                    guard.policy_dry_run_legacy(context, &control, legacy)
                })
                .map_err(|error| owner_error(id, error))?;
            let result = response
                .value
                .validate()
                .map_err(|_| malformed_response())?;
            policy_dry_run_value(
                match result.outcome {
                    PolicyOutcome::Allow => "allow",
                    PolicyOutcome::Deny => "deny",
                },
                result.policy_version.as_str(),
                &result.matched_rules,
                result.deciding_rule.as_deref(),
                match result.reason {
                    PolicyDecisionReason::PermittedByRule => "permitted_by_rule",
                    PolicyDecisionReason::ExplicitDeny => "explicit_deny",
                    PolicyDecisionReason::ApprovalRequired => "approval_required",
                    PolicyDecisionReason::NoPermittingRule => "no_permitting_rule",
                    PolicyDecisionReason::InvalidContext => "invalid_context",
                    PolicyDecisionReason::EvaluationFailure => "evaluation_failure",
                },
                &result.authority_statement,
            )
        }
    };
    Ok(Dispatched {
        value,
        verification: None,
    })
}

/// The one seven-key `PolicyDryRunResult` wire encoding.
fn policy_dry_run_value(
    outcome: &str,
    policy_version: &str,
    matched_rules: &[String],
    deciding_rule: Option<&str>,
    reason: &str,
    authority_statement: &str,
) -> Value {
    serde_json::json!({
        "outcome": outcome,
        "policy_version": policy_version,
        "matched_rules": matched_rules,
        "deciding_rule": deciding_rule,
        "reason": reason,
        "mode": "dry_run",
        "authority_statement": authority_statement,
    })
}

/// `program.discover`: the owner reads the registry entry against the authenticated current
/// core time and returns the fixed big-endian discovery record.
pub(crate) fn program_discover<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ProgramDiscoverWire};
    let id = ctx.request_id;
    let program = decode_wire::<ProgramDiscoverWire>(request, id)?.into_request(id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_program_discover(context, program));
    owner_payload(id, response, |reader| {
        if reader.fixed::<32>()? != program {
            return None;
        }
        let lifecycle = match reader.u8()? {
            0 => "active",
            1 => "deprecated",
            2 => "tombstoned",
            _ => return None,
        };
        let observed_sequence = reader.u64()?;
        let observed_at = reader.u64()?;
        let valid_through = reader.u64()?;
        let receipt_digest: [u8; 32] = reader.fixed()?;
        let state_root: [u8; 32] = reader.fixed()?;
        let version = reader.u32()?;
        let abi_version = reader.u16()?;
        let code_hash: [u8; 32] = reader.fixed()?;
        Some((
            serde_json::json!({
                "program_id": hexv(&program),
                "lifecycle": lifecycle,
                "version": version,
                "code_hash": hexv(&code_hash),
                "abi_version": abi_version,
                "receipt_digest": hexv(&receipt_digest),
                "state_root": hexv(&state_root),
                "observed_sequence": dec(observed_sequence),
                "observed_at": dec(observed_at),
                "valid_through": dec(valid_through),
                "verification": "registry-receipt-and-current-head-verified",
            }),
            None,
        ))
    })
}

pub(crate) fn program_interface<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ProgramDiscoverWire};
    use layerx_programs::SourceStatus;
    let id = ctx.request_id;
    let program = decode_wire::<ProgramDiscoverWire>(request, id)?.into_request(id)?;
    let response = owner.lock()
        .and_then(|mut guard| guard.rpc_program_interface_with_source(context, program))
        .map_err(|error| owner_error(id, error))?;
    let native = &response.interface;
    let head = &native.discovery;
    let source = &response.source;
    if head.program.bytes() != program || source.program() != program
        || source.version() != native.version || native.version != head.version
        || source.code_hash() != head.code_hash || source.state_root() != head.state_root
        || source.observed_sequence() != head.observed_sequence || source.observed_at() != head.observed_at
        || source.current_head_receipt_digest() != head.receipt_digest
        || source.deployment_receipt_digest() == [0; 32] || source.valid_through() > head.valid_through
    {
        return Err(rejection(ErrorClass::InternalFault, id, "owner.response_malformed"));
    }
    let status = match source.source() {
        SourceStatus::Unpublished => serde_json::json!({"status":"unpublished"}),
        SourceStatus::Verified { source_digest, environment_digest } => serde_json::json!({
            "status":"verified", "source_digest":hexv(source_digest),
            "environment_digest":hexv(environment_digest),
            "pipeline":source.pipeline().ok_or_else(|| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?,
        }),
        SourceStatus::Mismatch { expected, reproduced } => serde_json::json!({
            "status":"mismatch", "expected_code_hash":hexv(expected), "reproduced_artifact_digest":hexv(reproduced),
        }),
    };
    let interface = native.interface.canonical_encoding();
    let interface_digest: [u8; 32] = Sha256::digest(interface).into();
    Ok(Dispatched {
        value: serde_json::json!({
            "program_id":hexv(&program), "version":native.version, "code_hash":hexv(&head.code_hash),
            "abi_version":head.abi_version, "interface":hexv(interface), "interface_digest":hexv(&interface_digest),
            "receipt_digest":hexv(&source.current_head_receipt_digest()), "state_root":hexv(&head.state_root),
            "observed_sequence":dec(head.observed_sequence), "observed_at":dec(head.observed_at),
            "valid_through":dec(source.valid_through()), "source":status,
            "verification":"deployment-interface-and-current-head-verified",
        }),
        verification: None,
    })
}

pub(crate) fn program_activity<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ProgramActivityWire};
    let id = ctx.request_id;
    let activity_id = decode_wire::<ProgramActivityWire>(request, id)?.into_request(id)?;
    let response = owner.lock()
        .and_then(|mut guard| guard.rpc_program_activity(context, activity_id))
        .map_err(|error| owner_error(id, error))?;
    program_activity_response(id, activity_id, response)
}

fn program_activity_response(
    id: RequestId,
    activity_id: [u8; 32],
    response: crate::human_runtime::RpcProgramActivity,
) -> Result<Dispatched, Rejection> {
    use crate::human_runtime::RpcProgramActivity;
    match response {
        RpcProgramActivity::Unknown { idempotency_key, signed_activity } => Ok(Dispatched {
            value: serde_json::json!({
                "state": "unknown",
                "activity_id": hexv(&activity_id),
                "idempotency_key": hexv(&idempotency_key),
                "retained_signed_activity": hexv(&signed_activity),
            }),
            verification: Some(VerificationStatus::Unverified {
                requested: Level::SequencerSigned,
                achieved: Level::Unverified,
                reason: layerx_agent_api::error::ReasonCode::new("receipt_pending")
                    .map_err(|_| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?,
            }),
        }),
        RpcProgramActivity::Verified { idempotency_key, signed_activity, authority, execution: result } => {
            let malformed_response = || rejection(ErrorClass::InternalFault, id, "owner.response_malformed");
            let execution = &result.execution;
            let receipt = layerx_wire::receipt::decode(execution.receipt()).map_err(|_| malformed_response())?;
            let protocol = receipt.protocol().ok_or_else(malformed_response)?;
            let unsigned = layerx_wire::receipt::encode_unsigned(&receipt).map_err(|_| malformed_response())?;
            let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| malformed_response())?;
            let outcome = execution.outcome().ok_or_else(malformed_response)?;
            if !execution.committed() || protocol.activity_id() != activity_id {
                return Err(malformed_response());
            }
            Ok(Dispatched {
                value: serde_json::json!({
                    "state": if outcome.is_completed() { "executed" } else { "refused" },
                    "activity_id": hexv(&activity_id),
                    "idempotency_key": hexv(&idempotency_key),
                    "retained_signed_activity": hexv(&signed_activity),
                    "program_id": hexv(&result.program_id),
                    "guest_abi_version": result.guest_abi_version,
                    "module_version": protocol.module_version(),
                    "batch_id": hexv(&protocol.batch_id()),
                    "global_sequence": dec(protocol.global_sequence()),
                    "result_code": execution.result_code(),
                    "state_root": hexv(&protocol.resulting_state_root()),
                    "receipt": hexv(execution.receipt()),
                    "receipt_digest": hexv(&receipt_digest),
                    "terminal_payload": hexv(&result.terminal_payload),
                    "call_graph": hexv(execution.call_graph()),
                    "authority": {
                        "batch_id": hexv(&authority.batch_id()),
                        "asset": hexv(&authority.asset()),
                        "previous_state_root": hexv(&authority.previous_state_root()),
                        "resulting_state_root": hexv(&authority.resulting_state_root()),
                        "sequencer_public_key": hexv(&authority.sequencer_public_key()),
                    },
                    "usage": {
                        "cpu_fuel": dec(execution.cpu_fuel()),
                        "memory_bytes": dec(execution.memory_bytes()),
                        "storage_read_bytes": dec(execution.storage_read_bytes()),
                        "storage_write_bytes": dec(execution.storage_write_bytes()),
                        "output_values": execution.output_values(),
                        "output_bytes": dec(execution.output_bytes()),
                        "fee_units": dec(execution.fee_units()),
                    },
                    "outcome": program_outcome_value(outcome),
                    "verification": "receipt-terminal-and-call-graph-verified",
                }),
                verification: Some(VerificationStatus::Achieved(Level::SequencerSigned)),
            })
        }
    }
}

pub(crate) fn program_simulate<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let typed = crate::agent_rpc_wire::program_simulation_request(request, id)?;
    let response = owner
        .lock()
        .and_then(|mut guard| guard.rpc_program_simulate(context, typed))
        .map_err(|error| owner_error(id, error))?;
    let malformed_response = || rejection(ErrorClass::InternalFault, id, "owner.response_malformed");
    let execution = &response.execution;
    let receipt = layerx_wire::receipt::decode(execution.receipt()).map_err(|_| malformed_response())?;
    let protocol = receipt.protocol().ok_or_else(malformed_response)?;
    let unsigned = layerx_wire::receipt::encode_unsigned(&receipt).map_err(|_| malformed_response())?;
    let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| malformed_response())?;
    let outcome = execution.outcome().ok_or_else(malformed_response)?;
    if execution.committed() || response.evidence.committed {
        return Err(malformed_response());
    }
    Ok(Dispatched {
        value: serde_json::json!({
            "committed": false,
            "execution": {
                "state": "simulated",
                "activity_id": hexv(&protocol.activity_id()),
                "program_id": hexv(&response.program_id),
                "guest_abi_version": response.guest_abi_version,
                "module_version": protocol.module_version(),
                "batch_id": hexv(&protocol.batch_id()),
                "global_sequence": dec(protocol.global_sequence()),
                "result_code": execution.result_code(),
                "state_root": hexv(&protocol.resulting_state_root()),
                "receipt": hexv(execution.receipt()),
                "receipt_digest": hexv(&receipt_digest),
                "terminal_payload": hexv(&response.terminal_payload),
                "call_graph": hexv(execution.call_graph()),
                "authority": {
                    "batch_id": hexv(&protocol.batch_id()),
                    "asset": hexv(&protocol.asset()),
                    "previous_state_root": hexv(&protocol.previous_state_root()),
                    "resulting_state_root": hexv(&protocol.resulting_state_root()),
                    "sequencer_public_key": hexv(&response.sequencer_public_key),
                },
                "usage": {
                    "cpu_fuel": dec(execution.cpu_fuel()),
                    "memory_bytes": dec(execution.memory_bytes()),
                    "storage_read_bytes": dec(execution.storage_read_bytes()),
                    "storage_write_bytes": dec(execution.storage_write_bytes()),
                    "output_values": execution.output_values(),
                    "output_bytes": dec(execution.output_bytes()),
                    "fee_units": dec(execution.fee_units()),
                },
                "outcome": program_outcome_value(outcome),
                "verification": "receipt-terminal-and-call-graph-verified",
            },
            "simulation_evidence": {
                "boundary_id": hexv(&response.evidence.boundary_id),
                "activity_id": hexv(&response.evidence.activity_id),
                "previous_state_root": hexv(&response.evidence.previous_state_root),
                "hypothetical_state_root": hexv(&response.evidence.hypothetical_state_root),
                "observed_sequence": dec(response.evidence.observed_sequence),
                "observed_at": dec(response.evidence.observed_at),
                "public_key": hexv(&response.sequencer_public_key),
                "signature": hexv(&response.evidence_signature),
                "committed": false,
            },
        }),
        verification: Some(VerificationStatus::Achieved(Level::SequencerSigned)),
    })
}

fn program_outcome_value(outcome: &layerx_types::intent::ProgramCallOutcome) -> Value {
    use layerx_types::intent::{ProgramCallFailure, ProgramCallOutcome, ProgramLegacyValue};
    match outcome {
        ProgramCallOutcome::Completed(response) => serde_json::json!({
            "kind": "completed", "code": response.code(), "response": hexv(response.body()),
        }),
        ProgramCallOutcome::LegacyCompleted(response) => serde_json::json!({
            "kind": "legacy_completed", "code": response.code(),
            "values": response.values().iter().map(|value| match value {
                ProgramLegacyValue::I32(value) => serde_json::json!({"type": "i32", "value": value}),
                ProgramLegacyValue::I64(value) => serde_json::json!({"type": "i64", "value": value.to_string()}),
            }).collect::<Vec<_>>(),
        }),
        ProgramCallOutcome::Refused(failure) => {
            let failure = match *failure {
                ProgramCallFailure::UnknownProgram => serde_json::json!({"kind": "unknown_program"}),
                ProgramCallFailure::Reentrancy => serde_json::json!({"kind": "reentrancy"}),
                ProgramCallFailure::DepthExceeded { limit, attempted } => {
                    serde_json::json!({"kind": "depth_exceeded", "limit": limit, "attempted": attempted})
                }
                ProgramCallFailure::FanoutExceeded { limit, attempted } => {
                    serde_json::json!({"kind": "fanout_exceeded", "limit": limit, "attempted": attempted})
                }
                ProgramCallFailure::GuestRefused { code } => serde_json::json!({"kind": "guest_refused", "code": code}),
                ProgramCallFailure::Authority => serde_json::json!({"kind": "authority"}),
                ProgramCallFailure::Resource => serde_json::json!({"kind": "resource"}),
                ProgramCallFailure::Response => serde_json::json!({"kind": "response"}),
                ProgramCallFailure::Fault => serde_json::json!({"kind": "fault"}),
            };
            serde_json::json!({"kind": "refused", "failure": failure})
        }
    }
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
struct AvailabilityWire {
    #[serde(default)]
    tenant: Option<String>,
    #[serde(default)]
    agent: Option<String>,
    selector: String,
    requested_verification_level: String,
    maximum_bytes: String,
    maximum_chunks: String,
    deadline: String,
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

pub(crate) fn program_call<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    let id = ctx.request_id;
    let typed = crate::agent_rpc_wire::program_simulation_request(request, id)?;
    let key = crate::agent_rpc_dispatch::mutation_key(ctx)?;
    let (activity_id, response) = owner.lock()
        .and_then(|mut guard| guard.rpc_program_call(context, typed, id.0, key))
        .map_err(|error| owner_error(id, error))?;
    program_activity_response(id, activity_id, response)
}

pub(crate) fn program_deploy<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    program_lifecycle(owner, context, request, ctx, Operation::ProgramDeploy)
}

pub(crate) fn program_upgrade<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    program_lifecycle(owner, context, request, ctx, Operation::ProgramUpgrade)
}

pub(crate) fn program_wind_down<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext,
) -> Result<Dispatched, Rejection> {
    program_lifecycle(owner, context, request, ctx, Operation::ProgramWindDown)
}

fn program_lifecycle<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>, context: &RpcOwnerContext<'_>,
    request: &Map<String, Value>, ctx: &DispatchContext, operation: Operation,
) -> Result<Dispatched, Rejection> {
    use crate::agent_rpc_wire::{decode_wire, ProgramLifecycleWire};
    let id = ctx.request_id;
    let typed = decode_wire::<ProgramLifecycleWire>(request, id)?.into_request(id)?;
    let key = crate::agent_rpc_dispatch::mutation_key(ctx)?;
    let (activity_id, receipt) = owner.lock()
        .and_then(|mut guard| guard.rpc_program_lifecycle(context, typed, id.0, key, operation))
        .map_err(|error| owner_error(id, error))?;
    let Some(receipt) = receipt else {
        return Ok(Dispatched {
            value: serde_json::json!({"state": "unknown", "activity_id": hexv(&activity_id),
                "retry": "after", "retry_after_seconds": 2}),
            verification: Some(VerificationStatus::Unverified {
                requested: Level::SequencerSigned, achieved: Level::Unverified,
                reason: layerx_agent_api::error::ReasonCode::new("receipt_pending")
                    .map_err(|_| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?,
            }),
        });
    };
    let decoded = layerx_wire::receipt::decode(&receipt)
        .map_err(|_| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?;
    let protocol = decoded.protocol()
        .filter(|protocol| protocol.activity_id() == activity_id)
        .ok_or_else(|| rejection(ErrorClass::InternalFault, id, "owner.response_malformed"))?;
    Ok(Dispatched {
        value: serde_json::json!({"state": if protocol.result_code() == 0 { "executed" } else { "refused" },
            "activity_id": hexv(&activity_id), "receipt": hexv(&receipt),
            "terminal_payload": "", "call_graph": ""}),
        verification: Some(VerificationStatus::Achieved(Level::SequencerSigned)),
    })
}
