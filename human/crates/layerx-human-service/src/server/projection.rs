use serde_json::{json, Value};
use layerx_agent_api::verify::Level;

use super::agent_runtime::{AgentApprovalFacts, ManagedAgentEvidence, ManagedAgentView, ManagedReceiptExport};
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
    bytes.iter().flat_map(|byte| [char::from(DIGITS[usize::from(byte >> 4)]), char::from(DIGITS[usize::from(byte & 15)])]).collect()
}

pub(super) fn digest(text: &str) -> Result<[u8; 32], ApiFailure> {
    if text.len() != 64 { return Err(ApiFailure::upstream_degraded()); }
    let mut result = [0; 32];
    for (index, pair) in text.as_bytes().chunks_exact(2).enumerate() {
        let digit = |value| match value { b'0'..=b'9' => Some(value - b'0'), b'a'..=b'f' => Some(value - b'a' + 10), _ => None };
        result[index] = digit(pair[0]).ok_or_else(ApiFailure::upstream_degraded)? * 16 + digit(pair[1]).ok_or_else(ApiFailure::upstream_degraded)?;
    }
    if result == [0; 32] { return Err(ApiFailure::upstream_degraded()); }
    Ok(result)
}

pub(super) fn unix_time(seconds: u64) -> Result<String, ApiFailure> {
    let result = crate::time::rfc3339(seconds);
    if crate::time::seconds_from_rfc3339(&result) != Some(seconds) { return Err(ApiFailure::upstream_degraded()); }
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
    let seconds = text.parse::<u64>().map_err(|_| ApiFailure::upstream_degraded())?;
    if seconds.to_string() != text { return Err(ApiFailure::upstream_degraded()); }
    unix_time(seconds)
}

pub(super) fn managed_evidence(value: &ManagedAgentEvidence) -> Result<Value, ApiFailure> {
    let digest = digest(value.evidence_id.strip_prefix("evd_").unwrap_or(&value.evidence_id))?;
    if !matches!(value.class.as_str(), "agent-creation" | "rotate" | "recover" | "reclaim" | "archive" | "layerx-receipt" | "agent-reclaim" | "agent-retire") { return Err(ApiFailure::upstream_degraded()); }
    Ok(json!({"evidence_id":format!("evd_{}",hex(&digest)),"class":"layerx-receipt","verification":verification(value.verification)?}))
}

pub(super) fn managed_agent(value: &ManagedAgentView) -> Result<Value, ApiFailure> {
    if !value.agent_id.starts_with("agt_") || value.agent_id.len() <= 4 { return Err(ApiFailure::upstream_degraded()); }
    let state = ["creating", "active", "paused", "archiving", "archived"].get(usize::from(value.state)).ok_or_else(ApiFailure::upstream_degraded)?;
    let enforcement = ["protocol", "app"].get(usize::from(value.limit_enforcement)).ok_or_else(ApiFailure::upstream_degraded)?;
    Ok(json!({"agent_id":value.agent_id,"name":value.name,"purpose":value.purpose,"state":state,"state_copy_key":format!("agent.state.{state}"),
        "limit":{"monthly":{"amount":value.monthly_limit.to_string(),"currency":value.currency},"enforcement":enforcement,"enforcement_copy_key":if value.limit_enforcement==0 {"agent.limit.protocol-backed"} else {"agent.limit.app-enforced"}},
        "spend":{"period_start":native_time(&value.period_start)?,"period_end":native_time(&value.period_end)?,"spent":{"amount":value.spent.to_string(),"currency":value.currency},"remaining":{"amount":value.remaining.to_string(),"currency":value.currency},"verification":verification(value.spend_verification)?},
        "created_at":native_time(&value.created_at)?,"updated_at":native_time(&value.updated_at)?,"evidence":value.evidence.iter().map(managed_evidence).collect::<Result<Vec<_>,_>>()?}))
}

pub(super) fn managed_export(value: &ManagedReceiptExport) -> Result<Value, ApiFailure> {
    use base64::Engine as _;
    Ok(json!({"evidence_id":format!("evd_{}",hex(&value.digest)),"class":"layerx-receipt","verification":verification(value.verification)?,"content_type":"application/vnd.layerx.receipt","bytes_base64":base64::engine::general_purpose::STANDARD.encode(&value.canonical_bytes)}))
}

pub(super) fn approval_state(state: &AgentApprovalState) -> &'static str {
    match state { AgentApprovalState::AwaitingApproval => "pending", AgentApprovalState::Approved {..} => "approved", AgentApprovalState::Rejected => "rejected", AgentApprovalState::Expired => "expired", AgentApprovalState::Defective => "defective" }
}

pub(super) fn hold_defective() -> ApiFailure {
    ApiFailure { status:409, code:"hold-defective".to_owned(), copy_key:"error.approval.hold-defective".to_owned(), retry:"final".to_owned(), retry_after_ms:None, field:None }
}

pub(super) struct ApprovalProjection {
    pub summary: Value,
    pub detail: Value,
}

pub(super) fn native_state(state: super::agent_runtime::NativeEffectApprovalFactState) -> Result<&'static str, ApiFailure> {
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
    verification(match level { Level::Unverified => 0, Level::SequencerSigned => 1, Level::BatchIncluded => 2,
        Level::StateProven => 3, Level::CheckpointFinalised => 4, Level::SettlementAnchored => 5 })
}

pub(super) fn native_approval(facts: &super::agent_runtime::NativeEffectApprovalFacts,
    managed: &ManagedAgentView, budget: &super::agent_runtime::NativeEffectApprovalBudget,
    row: &super::agent_runtime::NativeEffectBudgetRow, fee_currency: &str, evidence: Vec<Value>) -> Result<ApprovalProjection, ApiFailure> {
    use layerx_crypto::disclosure::{AmountRole, CounterpartyRole};
    if !facts.requires_approval || facts.activity_module == 9 || facts.approval_id == [0;32] || facts.held_digest == [0;32]
        || facts.created_at_sequence >= facts.budget_expiry_sequence || facts.created_at_unix_seconds == 0
        || facts.owner != budget.owner || facts.approval_id != budget.approval_id || facts.held_digest != budget.held_digest
        || facts.asset != row.asset || facts.fee_asset != budget.fee_asset || row.evidence_digest == [0;32] {
        return Err(hold_defective());
    }
    let [amount] = facts.amounts.as_slice() else { return Err(hold_defective()); };
    if amount.role != AmountRole::Transfer { return Err(hold_defective()); }
    let recipients = facts.counterparties.iter().filter(|party|party.role == CounterpartyRole::Recipient).collect::<Vec<_>>();
    let payers = facts.counterparties.iter().filter(|party|party.role == CounterpartyRole::Payer).collect::<Vec<_>>();
    if recipients.len() > 1 || payers.len() > 1 || facts.counterparties.len() != recipients.len() + payers.len()
        || payers.first().is_some_and(|payer|payer.account != row.source_account) {
        return Err(hold_defective());
    }
    let counterparty = recipients.first().or_else(||payers.first()).ok_or_else(hold_defective)?;
    if counterparty.account == [0;32] { return Err(hold_defective()); }
    let state = native_state(facts.state)?;
    let approval_id = format!("apr_{}",hex(&facts.approval_id));
    let counterparty = format!("act_{}",hex(&counterparty.account));
    let money = json!({"amount":amount.value.to_string(),"currency":managed.currency});
    let expires_at = unix_milliseconds(facts.activity_expires_at_unix_milliseconds)?;
    let remaining = json!({"money":{"amount":row.remaining.to_string(),"currency":managed.currency},"verification":level(row.verification)?});
    let summary = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"counterparty":counterparty,"amount":money,
        "reason_copy_key":"approval.reason.policy-required","expires_at":expires_at,"state":state,"budget_remaining_after":remaining});
    let detail = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"state":state,"state_copy_key":format!("approval.state.{state}"),
        "reason_copy_key":"approval.reason.policy-required","facts":{"amount":money,"counterparty":counterparty,"asset":managed.currency,
        "fees":{"amount":facts.fee_limit.to_string(),"currency":fee_currency},"expires_at":expires_at},"budget_remaining_after":remaining,
        "created_at":unix_time(facts.created_at_unix_seconds)?,"evidence":evidence});
    Ok(ApprovalProjection {summary,detail})
}

pub(super) fn owned_material(bytes: &[u8], class: &str, content_type: &str) -> Value {
    use base64::Engine as _;
    use sha2::{Digest as _, Sha256};
    let digest:[u8;32] = Sha256::digest(bytes).into();
    json!({"evidence_id":format!("evd_{}",hex(&digest)),"class":class,"verification":"unverified", "content_type":content_type,
        "bytes_base64":base64::engine::general_purpose::STANDARD.encode(bytes)})
}

pub(super) fn approval(facts: &AgentApprovalFacts, managed: &ManagedAgentView, budget: VerifiedBudgetAfter, evidence: Vec<Value>) -> Result<ApprovalProjection, ApiFailure> {
    let hold = &facts.approval;
    if hold.held_activity.canonical_digest != hold.canonical_bytes_digest || facts.activity_expires_at_unix_seconds != hold.held_activity.expiry.0
        || facts.created_at_unix_seconds == 0 || hold.created_at_sequence >= hold.expires_at_sequence || budget.evidence_digest == [0;32] {
        return Err(hold_defective());
    }
    let [counterparty] = hold.held_activity.counterparties.values() else { return Err(hold_defective()); };
    let [amount] = hold.held_activity.amounts.values() else { return Err(hold_defective()); };
    if amount.counterparty != *counterparty || hold.held_activity.asset.as_str() != managed.currency { return Err(hold_defective()); }
    let reason = match hold.hold_reason_code.as_str() { "policy_approval_required" => "approval.reason.policy-required", _ => return Err(hold_defective()) };
    let state = approval_state(&hold.state);
    let approval_id = format!("apr_{}",hex(&hold.approval_id));
    let money = json!({"amount":amount.amount.0.to_string(),"currency":managed.currency});
    let expires_at = unix_time(facts.activity_expires_at_unix_seconds)?;
    let rank = match budget.level { Level::Unverified => 0, Level::SequencerSigned => 1, Level::BatchIncluded => 2, Level::StateProven => 3, Level::CheckpointFinalised => 4, Level::SettlementAnchored => 5 };
    let remaining = json!({"money":{"amount":budget.remaining.to_string(),"currency":managed.currency},"verification":verification(rank)?});
    let summary = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"counterparty":counterparty.as_str(),"amount":money,"reason_copy_key":reason,"expires_at":expires_at,"state":state,"budget_remaining_after":remaining});
    let detail = json!({"approval_id":approval_id,"agent_id":managed.agent_id,"agent_name":managed.name,"state":state,"state_copy_key":format!("approval.state.{state}"),"reason_copy_key":reason,
        "facts":{"amount":money,"counterparty":counterparty.as_str(),"asset":managed.currency,"fees":{"amount":hold.held_activity.fee_limit.0.to_string(),"currency":managed.currency},"expires_at":expires_at},
        "budget_remaining_after":remaining,"created_at":unix_time(facts.created_at_unix_seconds)?,"evidence":evidence});
    Ok(ApprovalProjection { summary, detail })
}
