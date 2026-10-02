//! `policy.dry_run` over the version 1 Agent operation envelope.

use layerx_agent_api::error::RequestId;
use layerx_agent_api::identity::PolicyVersion;
use layerx_agent_api::policy::{
    PolicyDecisionReason, PolicyDryRunRequest, PolicyDryRunResult, PolicyOutcome,
};
use serde_json::{json, Value};

use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use crate::rpc::encode_hex;
use crate::rpc_projection::require_unverified_status;
use crate::rpc_subscription::{object, text, violation};
use crate::Operation;

fn request_value(request: &PolicyDryRunRequest) -> Value {
    json!({
        "tenant": request.tenant.as_str(),
        "agent_did": request.agent_did.as_str(),
        "session_id": request.session_id.as_str(),
        "capability_id": request.capability_id.as_str(),
        "activity_type": request.activity_type.0.to_string(),
        "counterparty": encode_hex(&request.counterparty),
        "asset": encode_hex(&request.asset),
        "amount": request.amount.0.to_string(),
        "purpose": request.purpose,
        "core_sequence": request.core_sequence.0.to_string(),
    })
}

fn outcome(value: &Value, operation: Operation) -> Result<PolicyOutcome, EnvelopeError> {
    match value.as_str() {
        Some("allow") => Ok(PolicyOutcome::Allow),
        Some("deny") => Ok(PolicyOutcome::Deny),
        _ => Err(violation(operation)),
    }
}

fn reason(value: &Value, operation: Operation) -> Result<PolicyDecisionReason, EnvelopeError> {
    match value.as_str() {
        Some("permitted_by_rule") => Ok(PolicyDecisionReason::PermittedByRule),
        Some("explicit_deny") => Ok(PolicyDecisionReason::ExplicitDeny),
        Some("approval_required") => Ok(PolicyDecisionReason::ApprovalRequired),
        Some("no_permitting_rule") => Ok(PolicyDecisionReason::NoPermittingRule),
        Some("invalid_context") => Ok(PolicyDecisionReason::InvalidContext),
        Some("evaluation_failure") => Ok(PolicyDecisionReason::EvaluationFailure),
        _ => Err(violation(operation)),
    }
}

fn decode_result(value: &Value, operation: Operation) -> Result<PolicyDryRunResult, EnvelopeError> {
    let result = object(
        value,
        &[
            "outcome",
            "policy_version",
            "matched_rules",
            "deciding_rule",
            "reason",
            "mode",
            "authority_statement",
        ],
        operation,
    )?;
    if result["mode"].as_str() != Some("dry_run") {
        return Err(violation(operation));
    }
    let matched_rules = result["matched_rules"]
        .as_array()
        .ok_or_else(|| violation(operation))?
        .iter()
        .map(|rule| text(rule, operation))
        .collect::<Result<Vec<_>, _>>()?;
    let deciding_rule = match &result["deciding_rule"] {
        Value::Null => None,
        rule => Some(text(rule, operation)?),
    };
    PolicyDryRunResult {
        outcome: outcome(&result["outcome"], operation)?,
        policy_version: PolicyVersion::new(text(&result["policy_version"], operation)?)
            .map_err(|_| violation(operation))?,
        matched_rules,
        deciding_rule,
        reason: reason(&result["reason"], operation)?,
        authority_statement: text(&result["authority_statement"], operation)?,
    }
    .validate()
    .map_err(|_| violation(operation))
}

impl AgentEnvelopeTransport {
    /// Evaluates the tenant policy against one hypothetical activity under a real session and
    /// capability. The result is a local restriction, never protocol authorisation, and carries
    /// no freshness.
    ///
    /// # Errors
    ///
    /// Returns `InvalidRequest` for a non-canonical session or capability identifier before
    /// sending, the established error envelope, `Transport`, or `Decode` when the answer is not
    /// exactly one dry-run result claiming no verification level.
    pub fn policy_dry_run(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        request: &PolicyDryRunRequest,
    ) -> Result<PolicyDryRunResult, EnvelopeError> {
        let operation = Operation::PolicyDryRun;
        let request = request
            .clone()
            .validate()
            .map_err(|_| EnvelopeError::InvalidRequest)?;
        let success = self.send_operation(
            operation,
            request_id,
            &request_value(&request),
            Some(credential),
            None,
        )?;
        require_unverified_status(&success.verification_status, operation)?;
        decode_result(&success.value, operation)
    }
}
