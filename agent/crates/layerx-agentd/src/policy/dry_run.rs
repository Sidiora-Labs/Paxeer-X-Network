//! Side-effect-bounded dry-run evaluation and stable explanations.

use std::str;

use super::{
    evaluate, Decision, DecisionReason, EvaluationInput, Explanation, Outcome, PolicyRegistry,
    PolicySet,
};

pub const LOCAL_ALLOW_NOTICE: &str =
    "local policy has no objection; this is a local restriction result, not protocol authorisation";
pub const LOCAL_DENY_NOTICE: &str =
    "local restriction refused the request; this is not protocol authorisation and the protocol was not consulted";

/// Whether an explanation was produced for live or dry-run evaluation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvaluationMode {
    Live,
    DryRun,
}

/// Refusal while decoding an explanation record.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExplanationDecodeError {
    Malformed,
    UnknownValue,
    NonCanonical,
}

/// Dry-run output; the audit insertion is its only mutable effect.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DryRunResult {
    pub decision: Decision,
    pub explanation: Explanation,
}

pub(crate) fn evaluate_dry_run(
    registry: &mut PolicyRegistry,
    request_id: [u8; 32],
    policy: &PolicySet,
    input: &EvaluationInput<'_>,
) -> DryRunResult {
    let decision = evaluate(policy, input);
    let explanation = explain_decision(&decision, EvaluationMode::DryRun);
    registry.record_decision(request_id, decision.clone());
    DryRunResult {
        decision,
        explanation,
    }
}

pub(crate) fn explain_decision(decision: &Decision, mode: EvaluationMode) -> Explanation {
    Explanation {
        schema_version: 1,
        mode,
        outcome: decision.outcome,
        policy_version: decision.policy_version.clone(),
        matched_rules: decision.matched_rules.clone(),
        deciding_rule: decision.deciding_rule.clone(),
        reason: decision.reason,
        authority_statement: match decision.outcome {
            Outcome::Allow => LOCAL_ALLOW_NOTICE,
            Outcome::Deny => LOCAL_DENY_NOTICE,
        },
    }
}

pub(crate) fn encode_explanation(explanation: &Explanation) -> Vec<u8> {
    let mut output = Vec::new();
    push_line(
        &mut output,
        "schema",
        &explanation.schema_version.to_string(),
    );
    push_line(
        &mut output,
        "mode",
        match explanation.mode {
            EvaluationMode::Live => "live",
            EvaluationMode::DryRun => "dry_run",
        },
    );
    push_line(
        &mut output,
        "outcome",
        match explanation.outcome {
            Outcome::Allow => "allow",
            Outcome::Deny => "deny",
        },
    );
    push_line(&mut output, "policy_version", &explanation.policy_version);
    push_line(
        &mut output,
        "matched_count",
        &explanation.matched_rules.len().to_string(),
    );
    for (index, rule) in explanation.matched_rules.iter().enumerate() {
        push_line(&mut output, &format!("matched_{index}"), rule);
    }
    push_line(
        &mut output,
        "deciding_rule",
        explanation.deciding_rule.as_deref().unwrap_or(""),
    );
    push_line(&mut output, "reason", reason_name(explanation.reason));
    push_line(
        &mut output,
        "authority_statement",
        explanation.authority_statement,
    );
    output
}

pub(crate) fn decode_explanation(bytes: &[u8]) -> Result<Explanation, ExplanationDecodeError> {
    let mut cursor = bytes;
    let schema_version = match read_line(&mut cursor, "schema")? {
        "1" => 1,
        _ => return Err(ExplanationDecodeError::UnknownValue),
    };
    let mode = match read_line(&mut cursor, "mode")? {
        "live" => EvaluationMode::Live,
        "dry_run" => EvaluationMode::DryRun,
        _ => return Err(ExplanationDecodeError::UnknownValue),
    };
    let outcome = match read_line(&mut cursor, "outcome")? {
        "allow" => Outcome::Allow,
        "deny" => Outcome::Deny,
        _ => return Err(ExplanationDecodeError::UnknownValue),
    };
    let policy_version = read_line(&mut cursor, "policy_version")?.to_owned();
    let matched_count: usize = parse_decimal(read_line(&mut cursor, "matched_count")?)?;
    let mut matched_rules = Vec::new();
    for index in 0..matched_count {
        matched_rules.push(read_line(&mut cursor, &format!("matched_{index}"))?.to_owned());
    }
    let deciding_rule = match read_line(&mut cursor, "deciding_rule")? {
        "" => None,
        rule => Some(rule.to_owned()),
    };
    let reason = match read_line(&mut cursor, "reason")? {
        "permitted_by_rule" => DecisionReason::PermittedByRule,
        "explicit_deny" => DecisionReason::ExplicitDeny,
        "approval_required" => DecisionReason::ApprovalRequired,
        "no_permitting_rule" => DecisionReason::NoPermittingRule,
        "invalid_context" => DecisionReason::InvalidContext,
        "evaluation_failure" => DecisionReason::EvaluationFailure,
        _ => return Err(ExplanationDecodeError::UnknownValue),
    };
    let authority_statement = match outcome {
        Outcome::Allow => LOCAL_ALLOW_NOTICE,
        Outcome::Deny => LOCAL_DENY_NOTICE,
    };
    if read_line(&mut cursor, "authority_statement")? != authority_statement {
        return Err(ExplanationDecodeError::UnknownValue);
    }
    if !cursor.is_empty() {
        return Err(ExplanationDecodeError::Malformed);
    }
    let explanation = Explanation {
        schema_version,
        mode,
        outcome,
        policy_version,
        matched_rules,
        deciding_rule,
        reason,
        authority_statement,
    };
    if encode_explanation(&explanation) != bytes {
        return Err(ExplanationDecodeError::NonCanonical);
    }
    Ok(explanation)
}

fn read_line<'a>(cursor: &mut &'a [u8], key: &str) -> Result<&'a str, ExplanationDecodeError> {
    let rest = cursor
        .strip_prefix(key.as_bytes())
        .and_then(|rest| rest.strip_prefix(b"="))
        .ok_or(ExplanationDecodeError::Malformed)?;
    let colon = rest
        .iter()
        .position(|byte| *byte == b':')
        .ok_or(ExplanationDecodeError::Malformed)?;
    let length_text =
        str::from_utf8(&rest[..colon]).map_err(|_| ExplanationDecodeError::Malformed)?;
    let length: usize = parse_decimal(length_text)?;
    let value_start = colon + 1;
    let value_end = value_start
        .checked_add(length)
        .ok_or(ExplanationDecodeError::Malformed)?;
    if rest.get(value_end) != Some(&b'\n') {
        return Err(ExplanationDecodeError::Malformed);
    }
    let value = str::from_utf8(&rest[value_start..value_end])
        .map_err(|_| ExplanationDecodeError::Malformed)?;
    *cursor = &rest[value_end + 1..];
    Ok(value)
}

fn parse_decimal<T: str::FromStr>(text: &str) -> Result<T, ExplanationDecodeError> {
    if text.is_empty()
        || !text.bytes().all(|byte| byte.is_ascii_digit())
        || (text.len() > 1 && text.starts_with('0'))
    {
        return Err(ExplanationDecodeError::Malformed);
    }
    text.parse().map_err(|_| ExplanationDecodeError::Malformed)
}

fn push_line(output: &mut Vec<u8>, key: &str, value: &str) {
    output.extend_from_slice(key.as_bytes());
    output.push(b'=');
    output.extend_from_slice(value.len().to_string().as_bytes());
    output.push(b':');
    output.extend_from_slice(value.as_bytes());
    output.push(b'\n');
}

const fn reason_name(reason: DecisionReason) -> &'static str {
    match reason {
        DecisionReason::PermittedByRule => "permitted_by_rule",
        DecisionReason::ExplicitDeny => "explicit_deny",
        DecisionReason::ApprovalRequired => "approval_required",
        DecisionReason::NoPermittingRule => "no_permitting_rule",
        DecisionReason::InvalidContext => "invalid_context",
        DecisionReason::EvaluationFailure => "evaluation_failure",
    }
}
