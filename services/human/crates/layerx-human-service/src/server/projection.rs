use layerx_agent_api::verify::Level;
use serde_json::{json, Value};

use super::agent_runtime::{
    AgentApprovalFacts, ManagedAgentEvidence, ManagedAgentView, ManagedReceiptExport,
};
use super::backend::ApiFailure;
use crate::approvals::{AgentApprovalState, VerifiedBudgetAfter};

pub(super) fn verification(rank: u8) -> Result<&'static str, ApiFailure> {
    match rank {
        0 => Ok("unverified"),
        1..=3 => Ok("receipt-verified"),
        4 => Ok("checkpoint-finalised"),
        5 => Ok("settlement-anchored"),
        _ => Err(ApiFailure::upstream_degraded()),
    }
}

pub(super) fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|byte| {
            [
                char::from(DIGITS[usize::from(byte >> 4)]),
                char::from(DIGITS[usize::from(byte & 15)]),
            ]
        })
        .collect()
}

pub(super) fn digest(text: &str) -> Result<[u8; 32], ApiFailure> {
    if text.len() != 64 {
        return Err(ApiFailure::upstream_degraded());
    }
    let mut result = [0; 32];
    for (index, pair) in text.as_bytes().chunks_exact(2).enumerate() {
        let digit = |value| match value {
            b'0'..=b'9' => Some(value - b'0'),
            b'a'..=b'f' => Some(value - b'a' + 10),
            _ => None,
        };
        result[index] = digit(pair[0]).ok_or_else(ApiFailure::upstream_degraded)? * 16
            + digit(pair[1]).ok_or_else(ApiFailure::upstream_degraded)?;
    }
    if result == [0; 32] {
        return Err(ApiFailure::upstream_degraded());
    }
    Ok(result)
}

pub(super) fn unix_time(seconds: u64) -> Result<String, ApiFailure> {
    let result = crate::time::rfc3339(seconds);
    if crate::time::seconds_from_rfc3339(&result) != Some(seconds) {
        return Err(ApiFailure::upstream_degraded());
    }
    Ok(result)
}

pub(super) fn unix_milliseconds(milliseconds: u64) -> Result<String, ApiFailure> {
    let mut result = unix_time(milliseconds / 1000)?;
    let remainder = milliseconds % 1000;
    if remainder != 0 {
        result.pop();
        result.push_str(&format!(".{remainder:03}Z"));
    }
    Ok(result)
}

fn native_time(text: &str) -> Result<String, ApiFailure> {
    let seconds = text
        .parse::<u64>()
        .map_err(|_| ApiFailure::upstream_degraded())?;
    if seconds.to_string() != text {
        return Err(ApiFailure::upstream_degraded());
    }
    unix_time(seconds)
}

pub(super) fn managed_evidence(value: &ManagedAgentEvidence) -> Result<Value, ApiFailure> {
    let digest = digest(
        value
            .evidence_id
            .strip_prefix("evd_")
            .unwrap_or(&value.evidence_id),
    )?;
    if !matches!(
        value.class.as_str(),
        "agent-creation"
            | "rotate"
            | "recover"
            | "reclaim"
            | "archive"
            | "layerx-receipt"
            | "agent-reclaim"
            | "agent-retire"
    ) {
        return Err(ApiFailure::upstream_degraded());
    }
    Ok(
        json!({"evidence_id":format!("evd_{}",hex(&digest)),"class":"layerx-receipt","verification":verification(value.verification)?}),
    )
}

pub(super) fn managed_agent(value: &ManagedAgentView) -> Result<Value, ApiFailure> {
    if !value.agent_id.starts_with("agt_") || value.agent_id.len() <= 4 {
        return Err(ApiFailure::upstream_degraded());
    }
    let state = ["creating", "active", "paused", "archiving", "archived"]
        .get(usize::from(value.state))
        .ok_or_else(ApiFailure::upstream_degraded)?;
    let enforcement = ["protocol", "app"]
        .get(usize::from(value.limit_enforcement))
        .ok_or_else(ApiFailure::upstream_degraded)?;
    Ok(
        json!({"agent_id":value.agent_id,"name":value.name,"purpose":value.purpose,"state":state,"state_copy_key":format!("agent.state.{state}"),
        "limit":{"monthly":{"amount":value.monthly_limit.to_string(),"currency":value.currency},"enforcement":enforcement,"enforcement_copy_key":if value.limit_enforcement==0 {"agent.limit.protocol-backed"} else {"agent.limit.app-enforced"}},
        "spend":{"period_start":native_time(&value.period_start)?,"period_end":native_time(&value.period_end)?,"spent":{"amount":value.spent.to_string(),"currency":value.currency},"remaining":{"amount":value.remaining.to_string(),"currency":value.currency},"verification":verification(value.spend_verification)?},
        "created_at":native_time(&value.created_at)?,"updated_at":native_time(&value.updated_at)?,"evidence":value.evidence.iter().map(managed_evidence).collect::<Result<Vec<_>,_>>()?}),
    )
}

pub(super) fn managed_export(value: &ManagedReceiptExport) -> Result<Value, ApiFailure> {
    use base64::Engine as _;
    Ok(
        json!({"evidence_id":format!("evd_{}",hex(&value.digest)),"class":"layerx-receipt","verification":verification(value.verification)?,"content_type":"application/vnd.layerx.receipt","bytes_base64":base64::engine::general_purpose::STANDARD.encode(&value.canonical_bytes)}),
    )
}

pub(super) fn approval_state(state: &AgentApprovalState) -> &'static str {
    match state {
        AgentApprovalState::AwaitingApproval => "pending",
        AgentApprovalState::Approved { .. } => "approved",
        AgentApprovalState::Rejected => "rejected",
        AgentApprovalState::Expired => "expired",
        AgentApprovalState::Defective => "defective",
    }
}

pub(super) fn hold_defective() -> ApiFailure {
    ApiFailure {
        status: 409,
        code: "hold-defective".to_owned(),
        copy_key: "error.approval.hold-defective".to_owned(),
        retry: "final".to_owned(),
        retry_after_ms: None,
        field: None,
    }
}

pub(super) struct ApprovalProjection {
    pub summary: Value,
    pub detail: Value,
}

pub(super) fn native_state(
    state: super::agent_runtime::NativeEffectApprovalFactState,
) -> Result<&'static str, ApiFailure> {
    use super::agent_runtime::NativeEffectApprovalFactState;
    match state {
        NativeEffectApprovalFactState::Awaiting => Ok("pending"),
        NativeEffectApprovalFactState::Granted => Ok("approved"),
        NativeEffectApprovalFactState::Rejected => Ok("rejected"),
        NativeEffectApprovalFactState::Expired => Ok("expired"),
        NativeEffectApprovalFactState::NotRequired => Err(ApiFailure::not_found()),
    }
}

pub(super) fn level(level: Level) -> Result<&'static str, ApiFailure> {
    verification(match level {
        Level::Unverified => 0,
        Level::SequencerSigned => 1,
        Level::BatchIncluded => 2,
        Level::StateProven => 3,
        Level::CheckpointFinalised => 4,
        Level::SettlementAnchored => 5,
    })
}

pub(super) fn native_approval(
    facts: &super::agent_runtime::NativeEffectApprovalFacts,
    managed: &ManagedAgentView,
    budget: &super::agent_runtime::NativeEffectApprovalBudget,
    row: &super::agent_runtime::NativeEffectBudgetRow,
    fee_currency: &str,
    evidence: Vec<Value>,
) -> Result<ApprovalProjection, ApiFailure> {
    use layerx_crypto::disclosure::{AmountRole, CounterpartyRole};
    if !facts.requires_approval
        || facts.activity_module == 9
        || facts.approval_id == [0; 32]
        || facts.held_digest == [0; 32]
        || facts.created_at_sequence >= facts.budget_expiry_sequence
        || facts.created_at_unix_seconds == 0
        || facts.owner != budget.owner
        || facts.approval_id != budget.approval_id
        || facts.held_digest != budget.held_digest
        || facts.asset != row.asset
        || facts.fee_asset != budget.fee_asset
        || row.evidence_digest == [0; 32]
    {
        return Err(hold_defective());
    }
    let [amount] = facts.amounts.as_slice() else {
        return Err(hold_defective());
    };
    if amount.role != AmountRole::Transfer {
        return Err(hold_defective());
    }
    let recipients = facts
        .counterparties
        .iter()
        .filter(|party| party.role == CounterpartyRole::Recipient)
        .collect::<Vec<_>>();
    let payers = facts
        .counterparties
        .iter()
        .filter(|party| party.role == CounterpartyRole::Payer)
        .collect::<Vec<_>>();
    if recipients.len() > 1
        || payers.len() > 1
        || facts.counterparties.len() != recipients.len() + payers.len()
        || payers
            .first()
            .is_some_and(|payer| payer.account != row.source_account)
    {
        return Err(hold_defective());
    }
    let counterparty = recipients
        .first()
        .or_else(|| payers.first())
        .ok_or_else(hold_defective)?;
    if counterparty.account == [0; 32] {
        return Err(hold_defective());
    }
    let state = native_state(facts.state)?;
    let approval_id = format!("apr_{}", hex(&facts.approval_id));
    let counterparty = format!("act_{}", hex(&counterparty.account));
    let money = json!({"amount":amount.value.to_string(),"currency":managed.currency});
    let expires_at = unix_milliseconds(facts.activity_expires_at_unix_milliseconds)?;
    let remaining = json!({"money":{"amount":row.remaining.to_string(),"currency":managed.currency},"verification":level(row.verification)?});
    let summary = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"counterparty":counterparty,"amount":money,
        "reason_copy_key":"approval.reason.policy-required","expires_at":expires_at,"state":state,"budget_remaining_after":remaining});
    let detail = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"state":state,"state_copy_key":format!("approval.state.{state}"),
        "reason_copy_key":"approval.reason.policy-required","facts":{"amount":money,"counterparty":counterparty,"asset":managed.currency,
        "fees":{"amount":facts.fee_limit.to_string(),"currency":fee_currency},"expires_at":expires_at},"budget_remaining_after":remaining,
        "created_at":unix_time(facts.created_at_unix_seconds)?,"evidence":evidence});
    Ok(ApprovalProjection { summary, detail })
}

pub(super) fn owned_material(bytes: &[u8], class: &str, content_type: &str) -> Value {
    use base64::Engine as _;
    use sha2::{Digest as _, Sha256};
    let digest: [u8; 32] = Sha256::digest(bytes).into();
    json!({"evidence_id":format!("evd_{}",hex(&digest)),"class":class,"verification":"unverified", "content_type":content_type,
        "bytes_base64":base64::engine::general_purpose::STANDARD.encode(bytes)})
}

pub(super) fn approval(
    facts: &AgentApprovalFacts,
    managed: &ManagedAgentView,
    budget: VerifiedBudgetAfter,
    evidence: Vec<Value>,
) -> Result<ApprovalProjection, ApiFailure> {
    let hold = &facts.approval;
    if hold.held_activity.canonical_digest != hold.canonical_bytes_digest
        || facts.activity_expires_at_unix_seconds != hold.held_activity.expiry.0
        || facts.created_at_unix_seconds == 0
        || hold.created_at_sequence >= hold.expires_at_sequence
        || budget.evidence_digest == [0; 32]
    {
        return Err(hold_defective());
    }
    let [counterparty] = hold.held_activity.counterparties.values() else {
        return Err(hold_defective());
    };
    let [amount] = hold.held_activity.amounts.values() else {
        return Err(hold_defective());
    };
    if amount.counterparty != *counterparty || hold.held_activity.asset.as_str() != managed.currency
    {
        return Err(hold_defective());
    }
    let reason = match hold.hold_reason_code.as_str() {
        "policy_approval_required" => "approval.reason.policy-required",
        _ => return Err(hold_defective()),
    };
    let state = approval_state(&hold.state);
    let approval_id = format!("apr_{}", hex(&hold.approval_id));
    let money = json!({"amount":amount.amount.0.to_string(),"currency":managed.currency});
    let expires_at = unix_time(facts.activity_expires_at_unix_seconds)?;
    let rank = match budget.level {
        Level::Unverified => 0,
        Level::SequencerSigned => 1,
        Level::BatchIncluded => 2,
        Level::StateProven => 3,
        Level::CheckpointFinalised => 4,
        Level::SettlementAnchored => 5,
    };
    let remaining = json!({"money":{"amount":budget.remaining.to_string(),"currency":managed.currency},"verification":verification(rank)?});
    let summary = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"counterparty":counterparty.as_str(),"amount":money,"reason_copy_key":reason,"expires_at":expires_at,"state":state,"budget_remaining_after":remaining});
    let detail = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"state":state,"state_copy_key":format!("approval.state.{state}"),"reason_copy_key":reason,
        "facts":{"amount":money,"counterparty":counterparty.as_str(),"asset":managed.currency,"fees":{"amount":hold.held_activity.fee_limit.0.to_string(),"currency":managed.currency},"expires_at":expires_at},
        "budget_remaining_after":remaining,"created_at":unix_time(facts.created_at_unix_seconds)?,"evidence":evidence});
    Ok(ApprovalProjection { summary, detail })
}

pub(super) fn program_state(
    state: super::agent_runtime::NativeApprovalFactState,
) -> Result<&'static str, ApiFailure> {
    use super::agent_runtime::NativeApprovalFactState;
    Ok(match state {
        NativeApprovalFactState::Awaiting => "pending",
        NativeApprovalFactState::Granted => "approved",
        NativeApprovalFactState::Rejected => "rejected",
        NativeApprovalFactState::Expired => "expired",
        NativeApprovalFactState::Defective => "defective",
        NativeApprovalFactState::NotRequired => "not-required",
    })
}

fn program_bytes(bytes: &[u8]) -> Value {
    use sha2::{Digest as _, Sha256};
    json!({"sha256":hex(&Sha256::digest(bytes)),"byte_length":bytes.len().to_string()})
}

fn program_operation(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
) -> Result<Value, ApiFailure> {
    use layerx_types::program_call::NativeProgramCall;
    use layerx_types::program_lifecycle::{
        NativeProgramDeploy, NativeProgramUpgrade, NativeProgramWindDown, ProgramUpgradePolicy,
        ProgramWindDownOperation,
    };
    let bytes = &facts.canonical_payload_bytes;
    match facts.activity_ordinal {
        1 => {
            let value = NativeProgramDeploy::decode(bytes).map_err(|_| hold_defective())?;
            if value.encode().map_err(|_| hold_defective())? != *bytes {
                return Err(hold_defective());
            }
            let policy = match value.policy {
                ProgramUpgradePolicy::Immutable => json!({"kind":"immutable"}),
                ProgramUpgradePolicy::Authority(authority) => {
                    json!({"kind":"authority","authority":hex(&authority)})
                }
            };
            let mut deploy = json!({"guest_abi":value.guest_abi,"policy":policy,"new_hash":hex(&value.new_hash),"wasm":program_bytes(value.wasm)});
            if let Some(interface) = value.interface {
                deploy["interface"] = program_bytes(interface);
            }
            Ok(json!({"kind":"deploy","program_id":hex(&value.program_id.bytes()),"deploy":deploy}))
        }
        2 => {
            let value = NativeProgramUpgrade::decode(bytes).map_err(|_| hold_defective())?;
            if value.encode().map_err(|_| hold_defective())? != *bytes {
                return Err(hold_defective());
            }
            let mut upgrade = json!({"guest_abi":value.guest_abi,"old_hash":hex(&value.old_hash),"new_hash":hex(&value.new_hash),
                "migration_hook_hex":hex(value.migration_hook),"clear_interface":value.clear_interface,"wasm":program_bytes(value.wasm)});
            if let Some(interface) = value.interface {
                upgrade["interface"] = program_bytes(interface);
            }
            Ok(
                json!({"kind":"upgrade","program_id":hex(&value.program_id.bytes()),"upgrade":upgrade}),
            )
        }
        3 => {
            let value = NativeProgramCall::decode(bytes).map_err(|_| hold_defective())?;
            if value.encode().map_err(|_| hold_defective())? != *bytes {
                return Err(hold_defective());
            }
            Ok(
                json!({"kind":"call","program_id":hex(&value.program_id.bytes()),"call":{
                "guest_abi":value.guest_abi,"entrypoint_hex":hex(value.entrypoint),"calldata":program_bytes(value.calldata),
                "capabilities":program_bytes(value.capabilities),"access_declaration":program_bytes(value.access_declaration),
                "response_capacity":value.response_capacity,"resources":value.resources.0.iter().map(u64::to_string).collect::<Vec<_>>()}}),
            )
        }
        7 => {
            let value = NativeProgramWindDown::decode(bytes).map_err(|_| hold_defective())?;
            if value.encode().map_err(|_| hold_defective())? != *bytes {
                return Err(hold_defective());
            }
            let operation = match value.operation {
                ProgramWindDownOperation::Route {
                    account,
                    asset,
                    destination,
                    seed,
                } => json!({"kind":"route","account":format!("act_{}",hex(&account)),
                    "asset_id":hex(&asset),"destination":format!("act_{}",hex(&destination)),"seed_hex":hex(seed)}),
                ProgramWindDownOperation::Deprecate {
                    exit_program,
                    deadline_batch,
                } => {
                    json!({"kind":"deprecate","exit_program":hex(&exit_program),"deadline_batch":deadline_batch.to_string()})
                }
                ProgramWindDownOperation::Tombstone => json!({"kind":"tombstone"}),
                ProgramWindDownOperation::Exit { account } => {
                    json!({"kind":"exit","account":format!("act_{}",hex(&account))})
                }
                ProgramWindDownOperation::BoundedExit {
                    account,
                    maximum_exit_amount,
                } => json!({"kind":"bounded-exit","account":format!("act_{}",hex(&account)),
                    "maximum_exit_amount":maximum_exit_amount.to_string()}),
            };
            Ok(
                json!({"kind":"wind-down","program_id":hex(&value.program_id.bytes()),"wind_down":operation}),
            )
        }
        _ => Err(hold_defective()),
    }
}

fn program_limits(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
) -> Result<(&'static str, Vec<Value>), ApiFailure> {
    use super::agent_runtime::{NativeProgramApprovalSemantics, NativeProgramChargeKind};
    match &facts.semantics {
        NativeProgramApprovalSemantics::OperationOnly => Ok(("operation-only", Vec::new())),
        NativeProgramApprovalSemantics::AuthorizedLimits(rows) => {
            if rows.is_empty() {
                return Err(hold_defective());
            }
            let mut seen = std::collections::BTreeSet::new();
            let mut limits = Vec::with_capacity(rows.len());
            for row in rows {
                let kind = match row.kind {
                    NativeProgramChargeKind::Principal => "principal",
                    NativeProgramChargeKind::ProgramSpend => "program-spend",
                };
                if row.source == [0; 32]
                    || row.asset == [0; 32]
                    || row.destination.is_none_or(|value| value == [0; 32])
                    || !seen.insert((kind, row.source, row.asset, row.destination))
                {
                    return Err(hold_defective());
                }
                let mut limit = json!({"kind":kind,"source_account":format!("act_{}",hex(&row.source)),"asset_id":hex(&row.asset),"maximum_amount":row.maximum_amount.to_string()});
                if let Some(destination) = row.destination {
                    limit["destination"] = json!(format!("act_{}", hex(&destination)));
                }
                limits.push(limit);
            }
            Ok(("authorized-limits", limits))
        }
    }
}

fn validate_program_facts(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
) -> Result<(), ApiFailure> {
    use super::agent_runtime::NativeApprovalFactState;
    if facts.approval_id == [0; 32]
        || facts.held_digest == [0; 32]
        || facts.owner.is_empty()
        || facts.actor.is_empty()
        || facts.activity_module != 9
        || facts.created_at_sequence >= facts.budget_expiry_sequence
        || facts.created_at_unix_seconds == 0
        || facts
            .created_at_unix_seconds
            .checked_mul(1000)
            .is_none_or(|created| created >= facts.activity_expires_at_unix_milliseconds)
        || facts.release_ref.is_some_and(|value| value == [0; 32])
        || facts.fee_asset.is_some_and(|value| value == [0; 32])
        || facts.release_ref.is_some() != (facts.state == NativeApprovalFactState::Granted)
    {
        return Err(hold_defective());
    }
    Ok(())
}

pub(super) fn program_summary(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
    managed: &ManagedAgentView,
    evidence: &[Value],
) -> Result<Value, ApiFailure> {
    validate_program_facts(facts)?;
    if !managed.agent_id.starts_with("agt_") || managed.agent_id.len() <= 4 {
        return Err(hold_defective());
    }
    let (semantics, limits) = program_limits(facts)?;
    let reason = if facts.state == super::agent_runtime::NativeApprovalFactState::NotRequired {
        "approval.reason.program-policy-not-required"
    } else {
        "approval.reason.program-policy-required"
    };
    Ok(
        json!({"approval_id":format!("apr_{}",hex(&facts.approval_id)),"agent_id":managed.agent_id,"agent_name":managed.name,
        "state":program_state(facts.state)?,"reason_copy_key":reason,
        "created_at":unix_time(facts.created_at_unix_seconds)?,"expires_at":unix_milliseconds(facts.activity_expires_at_unix_milliseconds)?,
        "held_digest":hex(&facts.held_digest),"operation":program_operation(facts)?,"semantics":semantics,"authorized_limits":limits,"evidence":evidence}),
    )
}

pub(super) fn program_detail(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
    managed: &ManagedAgentView,
    evidence: &[Value],
    budget: Option<&Value>,
) -> Result<Value, ApiFailure> {
    let mut detail = program_summary(facts, managed, evidence)?;
    detail["state_copy_key"] = json!(format!("approval.state.{}", program_state(facts.state)?));
    detail["created_at_sequence"] = json!(facts.created_at_sequence.to_string());
    detail["budget_expiry_sequence"] = json!(facts.budget_expiry_sequence.to_string());
    if let Some(reference) = facts.release_ref {
        detail["release_ref"] = json!(hex(&reference));
    }
    if let Some(budget) = budget {
        if budget["approval_id"] != detail["approval_id"]
            || budget["held_digest"] != detail["held_digest"]
        {
            return Err(hold_defective());
        }
        detail["budget"] = budget.clone();
    }
    Ok(detail)
}

pub(super) fn program_budget(
    budget: &super::agent_runtime::NativeProgramApprovalBudget,
    proof_reference: Value,
) -> Result<Value, ApiFailure> {
    use sha2::{Digest as _, Sha256};
    if budget.approval_id == [0; 32]
        || budget.held_digest == [0; 32]
        || budget.owner.is_empty()
        || budget.budget_id == [0; 32]
        || budget.asset == [0; 32]
        || budget.source_account == [0; 32]
        || budget.evidence_digest == [0; 32]
        || budget.receipt_digest == [0; 32]
        || budget.checkpoint_digest == [0; 32]
        || budget.proof_digest == [0; 32]
        || budget.verified_proof_bytes.is_empty()
        || !matches!(
            budget.verification,
            Level::CheckpointFinalised | Level::SettlementAnchored
        )
        || budget.age_sequences > budget.maximum_age_sequences
        || <[u8; 32]>::from(Sha256::digest(&budget.verified_proof_bytes)) != budget.proof_digest
        || proof_reference["evidence_id"] != json!(format!("evd_{}", hex(&budget.proof_digest)))
        || proof_reference["class"] != json!("checkpoint-proof")
        || proof_reference["verification"] != json!(level(budget.verification)?)
    {
        return Err(ApiFailure::upstream_degraded());
    }
    Ok(
        json!({"approval_id":format!("apr_{}",hex(&budget.approval_id)),"held_digest":hex(&budget.held_digest),
        "budget_id":hex(&budget.budget_id),"asset_id":hex(&budget.asset),"source_account":format!("act_{}",hex(&budget.source_account)),
        "remaining":budget.remaining.to_string(),"observed_head_sequence":budget.observed_at_sequence.to_string(),
        "age_sequences":budget.age_sequences.to_string(),"maximum_age_sequences":budget.maximum_age_sequences.to_string(),
        "within_bound":budget.age_sequences <= budget.maximum_age_sequences,"terminal":budget.terminal,"verification":level(budget.verification)?,
        "proof_digest":hex(&budget.proof_digest),"receipt_digest":hex(&budget.receipt_digest),"checkpoint_digest":hex(&budget.checkpoint_digest),
        "evidence":[proof_reference]}),
    )
}

pub(super) fn program_material(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
    material: &super::agent_runtime::NativeProgramApprovalMaterial,
) -> Result<Value, ApiFailure> {
    use sha2::{Digest as _, Sha256};
    validate_program_facts(facts)?;
    if material.owner != facts.owner
        || material.approval_id != facts.approval_id
        || material.held_digest != facts.held_digest
        || material.actor != facts.actor
        || material.activity_ordinal != facts.activity_ordinal
        || material.canonical_payload_bytes != facts.canonical_payload_bytes
        || material.activity_expires_at_unix_milliseconds
            != facts.activity_expires_at_unix_milliseconds
        || material.canonical_unsigned_bytes.is_empty()
        || material.immutable_carrier_bytes.is_empty()
        || material.canonical_budget_bytes.is_empty()
        || <[u8; 32]>::from(Sha256::digest(&material.canonical_unsigned_bytes)) != facts.approval_id
        || <[u8; 32]>::from(Sha256::digest(&material.immutable_carrier_bytes)) != facts.held_digest
    {
        return Err(hold_defective());
    }
    let reference = |bytes: &[u8], class: &str| json!({"evidence_id":format!("evd_{}",hex(&Sha256::digest(bytes))),"class":class,"verification":"unverified"});
    let unsigned = reference(&material.canonical_unsigned_bytes, "approval-hold");
    let carrier = reference(&material.immutable_carrier_bytes, "approval-hold");
    let reservation = reference(&material.canonical_budget_bytes, "local-journey-state");
    Ok(
        json!({"approval_id":format!("apr_{}",hex(&facts.approval_id)),"held_digest":hex(&facts.held_digest),"provenance":"local-owned",
        "canonical_unsigned":unsigned,"immutable_carrier":carrier,"budget_reservation":reservation,"evidence":[unsigned,carrier,reservation]}),
    )
}

pub(super) fn program_decision(
    facts: &super::agent_runtime::NativeProgramApprovalFacts,
    evidence: &[Value],
) -> Result<Value, ApiFailure> {
    use super::agent_runtime::NativeApprovalFactState;
    validate_program_facts(facts)?;
    if !matches!(
        facts.state,
        NativeApprovalFactState::Granted | NativeApprovalFactState::Rejected
    ) {
        return Err(hold_defective());
    }
    let state = program_state(facts.state)?;
    let mut decision = json!({"approval_id":format!("apr_{}",hex(&facts.approval_id)),"held_digest":hex(&facts.held_digest),"state":state,
        "state_copy_key":format!("approval.state.{state}"),"money_moved":false,"moved_copy_key":"approval.program.decision.money-not-moved","evidence":evidence});
    if let Some(reference) = facts.release_ref {
        decision["release_ref"] = json!(hex(&reference));
    }
    Ok(decision)
}
