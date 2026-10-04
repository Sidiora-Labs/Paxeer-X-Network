//! Typed MCP write outcomes over the ordinary daemon write path.

use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::track::{ReceiptRef, SubmissionRef, SubmissionState, TrackedSubmission};
use layerx_agent_api::verify::Level;
use layerx_types::result::ResultCode;

use crate::server::{DaemonInvocation, InvocationOutcome, Server, ServerError};

/// Mandatory client stages. No MCP-only write stage exists.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WriteStage {
    Prepare,
    Disclose,
    Policy,
    Sign,
    Submit,
    Track,
}

pub const ORDINARY_WRITE_STAGES: [WriteStage; 6] = [
    WriteStage::Prepare,
    WriteStage::Disclose,
    WriteStage::Policy,
    WriteStage::Sign,
    WriteStage::Submit,
    WriteStage::Track,
];

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FailureClass {
    Refused,
    Unavailable,
    InvalidEvidence,
    Protocol,
}

/// Machine-readable failure. There is no success-like prose field.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StageFailure {
    pub stage: WriteStage,
    pub class: FailureClass,
    pub protocol_result_code: Option<ResultCode>,
}

/// Receipt evidence required before an executed outcome can exist.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedReceipt {
    pub receipt_ref: ReceiptRef,
    pub canonical_receipt: CanonicalBytes,
    pub verification_level: Level,
    pub evidence_ids: Vec<[u8; 32]>,
}

/// Complete daemon transcript consumed by an MCP write tool.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WriteTranscript {
    pub stages: Vec<WriteStage>,
    pub submission: Result<TrackedSubmission, StageFailure>,
    pub receipt: Option<VerifiedReceipt>,
}

/// The only non-error write outcomes exposed to a model.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum WriteOutcome {
    Executed {
        submission_ref: SubmissionRef,
        receipt: VerifiedReceipt,
    },
    Unknown {
        submission_ref: SubmissionRef,
        age_ms: u64,
    },
    Pending {
        submission_ref: SubmissionRef,
        state: SubmissionState,
    },
}

#[derive(Debug)]
pub enum WriteToolError {
    Server(ServerError),
    Stage(StageFailure),
    InvalidTranscript,
    SuccessWithoutVerifiedReceipt,
    ReceiptMismatch,
}

/// Named payment write tools that reuse the ordinary submit path.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PaymentTool {
    Send,
    Create,
    Mint,
    Transfer,
    IssueGrant,
    DrawGrant,
}

impl PaymentTool {
    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            Self::Send => "wallet.send",
            Self::Create => "token.create",
            Self::Mint => "token.mint",
            Self::Transfer => "token.transfer",
            Self::IssueGrant => "grant.issue",
            Self::DrawGrant => "grant.draw",
        }
    }
}

/// # Errors
/// Refuses missing scope, incomplete daemon stages and unverifiable receipt outcomes.
pub fn execute_payment<F>(
    server: &mut Server,
    core_sequence: u64,
    tool: PaymentTool,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    execute_named(
        server,
        core_sequence,
        tool.name(),
        validated_arguments,
        executor,
        unknown_age_ms,
    )
}

/// Executes a new write invocation through the scoped server's daemon-only route.
///
/// # Errors
///
/// Returns typed stage, transcript, evidence, or daemon-routing failures.
pub fn execute<F>(
    server: &mut Server,
    core_sequence: u64,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    execute_named(
        server,
        core_sequence,
        "activity.submit",
        validated_arguments,
        executor,
        unknown_age_ms,
    )
}

fn execute_named<F>(
    server: &mut Server,
    core_sequence: u64,
    name: &str,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    server
        .execute_committed(core_sequence, name, validated_arguments, |invocation| {
            let transcript = executor(invocation);
            let result = if transcript.stages == ORDINARY_WRITE_STAGES
                || (transcript_matches(&transcript.stages, &ORDINARY_WRITE_STAGES)
                    && transcript.submission.as_ref().is_err_and(|failure| {
                        matches!(
                            failure.class,
                            FailureClass::Refused | FailureClass::Protocol
                        )
                    })
                    && transcript.receipt.is_none())
            {
                classify_transcript(transcript, unknown_age_ms)
            } else {
                Err(WriteToolError::InvalidTranscript)
            };
            let outcome = invocation_outcome(&result);
            (result, outcome)
        })
        .map_err(WriteToolError::Server)?
}

/// Resolves a prior honest non-terminal result through the same daemon tracking path.
///
/// # Errors
///
/// Returns typed evidence or daemon-routing failures and never manufactures completion.
pub fn track<F>(
    server: &mut Server,
    core_sequence: u64,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    track_named(
        server,
        core_sequence,
        "activity.track",
        validated_arguments,
        executor,
        unknown_age_ms,
    )
}

/// # Errors
/// Refuses missing scope, invalid tracking evidence and success without a verified receipt.
pub fn wait<F>(
    server: &mut Server,
    core_sequence: u64,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    track_named(
        server,
        core_sequence,
        "activity.wait",
        validated_arguments,
        executor,
        unknown_age_ms,
    )
}

fn track_named<F>(
    server: &mut Server,
    core_sequence: u64,
    name: &str,
    validated_arguments: Vec<u8>,
    executor: F,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError>
where
    F: FnOnce(&DaemonInvocation) -> WriteTranscript,
{
    server
        .execute_committed(core_sequence, name, validated_arguments, |invocation| {
            let mut transcript = executor(invocation);
            let result = if transcript.stages == [WriteStage::Track] {
                transcript.stages = ORDINARY_WRITE_STAGES.to_vec();
                classify_transcript(transcript, unknown_age_ms)
            } else {
                Err(WriteToolError::InvalidTranscript)
            };
            let outcome = invocation_outcome(&result);
            (result, outcome)
        })
        .map_err(WriteToolError::Server)?
}

fn classify_transcript(
    transcript: WriteTranscript,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError> {
    let submission = match transcript.submission {
        Ok(submission) => submission,
        Err(failure) => return Err(WriteToolError::Stage(failure)),
    };
    classify_submission(submission, transcript.receipt, unknown_age_ms)
}

fn invocation_outcome(result: &Result<WriteOutcome, WriteToolError>) -> InvocationOutcome {
    match result {
        Ok(WriteOutcome::Executed { .. }) => InvocationOutcome::Completed,
        Ok(WriteOutcome::Unknown { .. } | WriteOutcome::Pending { .. }) => {
            InvocationOutcome::Unknown
        }
        Err(WriteToolError::Stage(failure))
            if matches!(
                failure.class,
                FailureClass::Refused | FailureClass::Protocol
            ) =>
        {
            InvocationOutcome::Refused
        }
        Err(_) => InvocationOutcome::Failed,
    }
}

fn classify_submission(
    submission: TrackedSubmission,
    receipt: Option<VerifiedReceipt>,
    unknown_age_ms: u64,
) -> Result<WriteOutcome, WriteToolError> {
    let submission_ref = submission.submission_ref.clone();
    match submission.state {
        SubmissionState::Executed { receipt_ref } => {
            let receipt = receipt.ok_or(WriteToolError::SuccessWithoutVerifiedReceipt)?;
            if receipt.receipt_ref != receipt_ref {
                return Err(WriteToolError::ReceiptMismatch);
            }
            if receipt.verification_level == Level::Unverified
                || submission.verification_level == Level::Unverified
                || receipt.verification_level != submission.verification_level
                || receipt.evidence_ids.is_empty()
            {
                return Err(WriteToolError::SuccessWithoutVerifiedReceipt);
            }
            Ok(WriteOutcome::Executed {
                submission_ref,
                receipt,
            })
        }
        SubmissionState::Failed { result } => Err(WriteToolError::Stage(StageFailure {
            stage: WriteStage::Track,
            class: FailureClass::Protocol,
            protocol_result_code: Some(result),
        })),
        SubmissionState::Unknown => Ok(WriteOutcome::Unknown {
            submission_ref,
            age_ms: unknown_age_ms,
        }),
        state => {
            if receipt.is_some() {
                return Err(WriteToolError::InvalidTranscript);
            }
            Ok(WriteOutcome::Pending {
                submission_ref,
                state,
            })
        }
    }
}

fn transcript_matches(actual: &[WriteStage], required: &[WriteStage]) -> bool {
    if actual == required {
        return true;
    }
    let Some(WriteStage::Policy) = actual.last() else {
        return false;
    };
    actual == &required[..3]
}

pub(crate) fn preparation_id(arguments: &serde_json::Value) -> Option<[u8; 32]> {
    let value=arguments.get("preparation")?.get("purpose")?.get("purpose")?.get("preparation_id")?.as_str()?;
    identifier(value)
}

fn identifier(value: &str) -> Option<[u8; 32]> {
    if value.len()!=64 || !value.bytes().all(|b|b.is_ascii_digit() || (b'a'..=b'f').contains(&b)) {return None;}
    let mut out=[0u8;32];
    for (i,b) in out.iter_mut().enumerate() {*b=u8::from_str_radix(&value[i*2..i*2+2],16).ok()?;}
    Some(out)
}

fn lower_hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b|format!("{b:02x}")).collect()
}

pub(crate) fn execute_native_alias<F>(
    name: &str, arguments: &serde_json::Value,
    registry: &layerx_types::payload::ModuleRegistry, key:[u8;32], mut owner:F,
) -> Result<serde_json::Value,crate::boundary::BoundaryRefusal>
where F:FnMut(layerx_agentd::tenant::Operation,&serde_json::Value)->Result<serde_json::Value,crate::boundary::BoundaryRefusal> {
    use crate::boundary::BoundaryRefusal;
    use layerx_agentd::tenant::Operation;
    use serde_json::json;
    let refuse=||BoundaryRefusal::Malformed("mcp.native_write_binding".into());
    if arguments.get("variant").and_then(serde_json::Value::as_str)!=Some("native_write_v1") {return Err(BoundaryRefusal::NotServed("native_write_v1"));}
    let intent=arguments.get("intent").ok_or_else(refuse)?;
    let preparation=arguments.get("preparation").ok_or_else(refuse)?;
    if intent.get("idempotency_key").and_then(serde_json::Value::as_str).and_then(identifier)!=Some(key) {return Err(refuse());}
    let expected_id=preparation_id(arguments).ok_or_else(refuse)?;
    let canonical_text=arguments.get("canonical_bytes").and_then(serde_json::Value::as_str).ok_or_else(refuse)?;
    if canonical_text.is_empty() || canonical_text.len()%2!=0 || !canonical_text.bytes().all(|b|b.is_ascii_digit() || (b'a'..=b'f').contains(&b)) {return Err(refuse());}
    let canonical=(0..canonical_text.len()).step_by(2).map(|i|u8::from_str_radix(&canonical_text[i..i+2],16).map_err(|_|refuse())).collect::<Result<Vec<_>,_>>()?;
    let preview=layerx_crypto::disclosure::bind(&canonical,registry).map_err(|_|refuse())?;
    use sha2::{Digest, Sha256};
    let signed=preparation.get("purpose").ok_or_else(refuse)?;
    let purpose=signed.get("purpose").ok_or_else(refuse)?;
    let same_decimal=|field:&str,n:u128|preparation.get(field).and_then(serde_json::Value::as_str).and_then(|v|v.parse::<u128>().ok())==Some(n);
    let supplied_activity=preparation.get("activity").ok_or_else(refuse)?;
    if <[u8;32]>::from(Sha256::digest(&canonical))!=expected_id
        || purpose.get("canonical_digest").and_then(serde_json::Value::as_str).and_then(identifier)!=Some(expected_id)
        || supplied_activity.get("module").and_then(serde_json::Value::as_str).and_then(|s|s.parse::<u16>().ok())!=Some(preview.activity_type.module() as u16)
        || supplied_activity.get("ordinal").and_then(serde_json::Value::as_str).and_then(|s|s.parse::<u16>().ok())!=Some(preview.activity_type.ordinal())
        || preparation.get("actor").and_then(serde_json::Value::as_str).map(str::as_bytes)!=Some(preview.actor.as_slice())
        || preparation.get("authority").and_then(serde_json::Value::as_str).map(str::as_bytes)!=Some(preview.authority.as_slice())
        || !same_decimal("account_sequence",u128::from(preview.envelope_sequence()))
        || !same_decimal("fee_limit",preview.fee_limit)
        || !same_decimal("not_before",u128::from(preview.expiry.not_before))
        || !same_decimal("not_after",u128::from(preview.expiry.not_after))
        || preparation.get("payload").and_then(serde_json::Value::as_str)!=Some(lower_hex(preview.canonical_payload()).as_str())
        || preview.idempotency_key!=key
        || arguments.get("signer_public_key")!=signed.get("owner_public_key") {return Err(refuse());}
    verify_native_intent(name,intent,&preview)?;
    let prepared_value=owner(Operation::Prepare,preparation)?;
    let prepared=layerx_sdk::agent_envelope::decode_native_preparation(&prepared_value).ok_or_else(refuse)?;
    if prepared.preparation_id!=expected_id || prepared.canonical_bytes!=canonical {return Err(refuse());}
    let disclosed_value=owner(Operation::Prepare,&json!({"variant":"native_disclosure_v1","canonical_bytes":lower_hex(&prepared.canonical_bytes)}))?;
    let exported=layerx_sdk::agent_envelope::decode_native_disclosure(&disclosed_value).ok_or_else(refuse)?;
    if exported.preparation_id!=prepared.preparation_id || exported.canonical_bytes!=prepared.canonical_bytes {return Err(refuse());}
    let disclosure=layerx_crypto::disclosure::bind(&prepared.canonical_bytes,registry).map_err(|_|refuse())?;
    if <[u8;32]>::from(Sha256::digest(&prepared.canonical_bytes))!=prepared.preparation_id
        || disclosure.audit_digest().map_err(|_|refuse())?!=exported.disclosure_digest
        || disclosure.activity_type.value()!=exported.activity_type || disclosure.actor!=exported.actor
        || disclosure.authority!=exported.authority || disclosure.asset!=exported.asset || disclosure.fee_limit!=exported.fee_limit
        || disclosure.expiry.not_before!=exported.not_before || disclosure.expiry.not_after!=exported.not_after
        || disclosure.expiry.payload_expires_at!=exported.payload_expires_at || disclosure.idempotency_key!=key
        || disclosure.idempotency_key!=exported.idempotency_key {
        return Err(refuse());
    }
    verify_native_intent(name,intent,&disclosure)?;
    if prepared.approval_required {return Ok(prepared_value);}
    let reference=lower_hex(&prepared.preparation_id);
    let signature=arguments.get("signature").ok_or_else(refuse)?;
    let signer=arguments.get("signer_public_key").ok_or_else(refuse)?;
    owner(Operation::Sign,&json!({"preparation_ref":reference,"signature":signature}))?;
    let mut submit=json!({"preparation_ref":reference,"signature":signature,"signer_public_key":signer});
    if matches!(name,"wallet.send"|"token.transfer") {submit["variant"]=json!("native_send_submit_v1");}
    let observation=owner(Operation::Submit,&submit)?;
    let submission=observation.get("submission").and_then(|value|value.get("submission_ref")).and_then(serde_json::Value::as_str).ok_or_else(||BoundaryRefusal::Unavailable("mcp.native_submit_observation".into()))?;
    owner(Operation::Track,&json!({"submission_ref":submission}))
}

fn verify_native_intent(name:&str,intent:&serde_json::Value,d:&layerx_crypto::disclosure::Disclosure)->Result<(),crate::boundary::BoundaryRefusal> {
    use layerx_crypto::disclosure::{CounterpartyRole,AmountRole};
    use layerx_crypto::payments::Payment;
    let refused=||crate::boundary::BoundaryRefusal::Malformed("mcp.native_intent_mismatch".into());
    let id=|field:&str|intent.get(field).and_then(serde_json::Value::as_str).and_then(identifier).ok_or_else(refused);
    let amount=|field:&str|intent.get(field).and_then(serde_json::Value::as_str).and_then(|v|v.parse::<u128>().ok()).ok_or_else(refused);
    let ordinal=crate::catalogue::native_alias_ordinal(name).ok_or_else(refused)?;
    if d.activity_type.module()!=layerx_types::payload::ModuleId::Asset || d.activity_type.value()!=(u32::from(1u16)<<16|u32::from(ordinal)) {return Err(refused());}
    let matches=match name {
        "wallet.send"|"token.transfer"=>{
            let recipients=d.counterparties.iter().filter(|c|c.role==CounterpartyRole::Recipient).collect::<Vec<_>>();
            let amounts=d.amounts.iter().filter(|a|a.role==AmountRole::Transfer).collect::<Vec<_>>();
            recipients.len()==1 && amounts.len()==1 && recipients[0].account==id("destination")? && amounts[0].value==amount("amount")? && d.asset==id("asset")?
        },
        "token.mint"=>matches!(&d.payment,Some(Payment::Mint{asset,to,amount:value}) if *asset==id("asset")? && *to==id("destination")? && *value==amount("amount")?),
        "grant.issue"=>matches!(&d.payment,Some(Payment::IssueGrant(g)) if g.recipient==id("beneficiary")? && g.asset==id("asset")? && g.allowance==amount("amount")? && u128::from(g.expiration)==amount("expires_at_ms")?),
        "grant.draw"=>matches!(&d.payment,Some(Payment::Receive{grant,amount:value,..}) if *grant==id("grant_id")? && *value==amount("amount")?),
        "token.create"=>matches!(&d.payment,Some(Payment::Register(r)) if intent.get("symbol").and_then(serde_json::Value::as_str)==Some(r.symbol.as_str()) && amount("decimals")?==u128::from(r.decimals) && amount("supply_cap")?==r.supply_cap),
        _=>false,
    };
    if matches {Ok(())} else {Err(refused())}
}
