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

pub(crate) fn dispatch_extended<A: HumanAuthorityBoundary>(
    owner: &SharedAgentOwner<A>,
    permit: &OperationPermit,
    peer: &RpcOwnerContext<'_>,
    operation: Operation,
    request: &Map<String, Value>,
    ctx: &DispatchContext,
) -> Option<Result<Dispatched, Rejection>> {
    let _ = permit;
    let id = ctx.request_id;
    let mut owner = owner.clone();
    match operation {
        Operation::BudgetReconciliation => Some(budget_reconciliation(&mut owner, peer, request, id)),
        _ => None,
    }
}

fn budget_reconciliation<A: HumanAuthorityBoundary>(
    owner: &mut SharedAgentOwner<A>,
    peer: &RpcOwnerContext<'_>,
    request: &Map<String, Value>,
    id: RequestId,
) -> Result<Dispatched, Rejection> {
    let request: BudgetSelector = decode(request, id)?;
    let _ = (request.tenant, request.agent_did);
    let budget_id = hex32(&request.budget_id, id)?;
    let response = owner
        .agent_budget_state(peer.peer(), budget_id)
        .map_err(|error| owner_error(id, error))?;
    decode_budget_state(&response, budget_id).ok_or_else(|| {
        rejection(ErrorClass::InternalFault, id, "owner.response_malformed")
    })
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BudgetSelector {
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
