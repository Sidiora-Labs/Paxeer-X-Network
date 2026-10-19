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
use crate::{Call, Operation, PolicyDryRun};

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

fn legacy_request_value(request: &PolicyDryRun) -> Value {
    let context = &request.context;
    let permitted_activity_types = context
        .permitted_activity_types
        .values()
        .iter()
        .map(|activity_type| activity_type.0.to_string())
        .collect::<Vec<_>>();
    json!({
        "context": {
            "tenant": context.tenant.as_str(),
            "agent_did": context.agent_did.as_str(),
            "authority_ref": context.authority_ref.as_str(),
            "permitted_activity_types": permitted_activity_types,
            "expiry": context.expiry.0.to_string(),
            "client": context.client.as_str(),
            "policy_version": context.policy_version.as_str(),
        },
        "canonical_intent": encode_hex(&request.canonical_intent),
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

    /// # Errors
    ///
    /// Returns the established error envelope, `Transport`, or `Decode` for any other answer.
    pub fn policy_dry_run_call(
        &self,
        request_id: RequestId,
        credential: &EnvelopeCredential,
        call: &Call<PolicyDryRun>,
    ) -> Result<PolicyDryRunResult, EnvelopeError> {
        let operation = call.operation();
        let success = self.send_operation(
            operation,
            request_id,
            &legacy_request_value(call.request()),
            Some(credential),
            None,
        )?;
        require_unverified_status(&success.verification_status, operation)?;
        decode_result(&success.value, operation)
    }
}

#[cfg(test)]
mod tests {
    use layerx_agent_api::identity::{
        ActivityType, AgentDid, AuthorityRef, ClientId, ExplicitSet, PolicyVersion, SessionContext,
        TenantId,
    };
    use layerx_agent_api::policy::{PolicyDecisionReason, PolicyDryRunResult, PolicyOutcome};
    use layerx_agent_api::TimestampSeconds;
    use serde_json::json;

    use super::{decode_result, legacy_request_value};
    use crate::{Operation, PolicyDryRun};

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("sdk policy dry run: {error:?}"))
    }

    #[test]
    fn legacy_dry_run_body_and_result_are_exact() {
        let request = PolicyDryRun {
            context: must(SessionContext::new(
                must(TenantId::new("tenant-a")),
                must(AgentDid::new("did:layerx:alice")),
                must(AuthorityRef::new("authority-a")),
                ExplicitSet::allow(vec![ActivityType(1), ActivityType(513)]),
                TimestampSeconds(1_900_000_000),
                must(ClientId::new("client-a")),
                must(PolicyVersion::new("policy-1")),
            )),
            canonical_intent: vec![0x00, 0xab, 0xcd, 0xef],
        };
        let body = legacy_request_value(&request);
        assert_eq!(
            body,
            json!({
                "context": {
                    "tenant": "tenant-a",
                    "agent_did": "did:layerx:alice",
                    "authority_ref": "authority-a",
                    "permitted_activity_types": ["1", "513"],
                    "expiry": "1900000000",
                    "client": "client-a",
                    "policy_version": "policy-1",
                },
                "canonical_intent": "00abcdef",
            })
        );
        assert_eq!(body["canonical_intent"].as_str(), Some("00abcdef"));

        let operation = Operation::PolicyDryRun;
        let mut answer = json!({
            "outcome": "deny",
            "policy_version": "policy-1",
            "matched_rules": ["rule-a"],
            "deciding_rule": "rule-a",
            "reason": "explicit_deny",
            "mode": "dry_run",
            "authority_statement": "local restriction only",
        });
        assert_eq!(
            must(decode_result(&answer, operation)),
            PolicyDryRunResult {
                outcome: PolicyOutcome::Deny,
                policy_version: must(PolicyVersion::new("policy-1")),
                matched_rules: vec!["rule-a".to_owned()],
                deciding_rule: Some("rule-a".to_owned()),
                reason: PolicyDecisionReason::ExplicitDeny,
                authority_statement: "local restriction only".to_owned(),
            }
        );
        answer["mode"] = json!("live");
        assert!(decode_result(&answer, operation).is_err());
        answer["mode"] = json!("dry_run");
        answer["explanation"] = json!("unexpected");
        assert!(decode_result(&answer, operation).is_err());
    }
}
