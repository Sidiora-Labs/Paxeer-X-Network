//! Process probe for the version 1 Agent operation envelope through a real unified
//! gateway and full-mode daemon. Inputs are provisioned by
//! `tools/qualification/paxeer-x/agent_operation_envelope.py`; every input is required and
//! the probe fails when any is absent. It never starts a local server.

use std::path::{Path, PathBuf};

use layerx_agent_api::error::{ErrorClass, Level, RequestId, Retriability, VerificationStatus};
use layerx_sdk::agent_envelope::{
    canonical_u64, AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError,
};
use layerx_sdk::production::SecretBytes;
use layerx_sdk::programs::LayerXKeyCredential;
use layerx_sdk::Operation;
use serde_json::Value;

const GATEWAY: &str = "LAYERX_AGENT_ENVELOPE_GATEWAY";
const TRUST_ANCHORS: &str = "LAYERX_AGENT_ENVELOPE_TRUST_ANCHORS";
const CASES: &str = "LAYERX_AGENT_ENVELOPE_CASES";

type Probe = Result<(), Box<dyn std::error::Error>>;

fn required_env(name: &str) -> Result<String, Box<dyn std::error::Error>> {
    match std::env::var(name) {
        Ok(value) if !value.is_empty() => Ok(value),
        _ => Err(format!("required probe input {name} is absent").into()),
    }
}

fn field<'a>(value: &'a Value, name: &str) -> Result<&'a Value, Box<dyn std::error::Error>> {
    value
        .get(name)
        .ok_or_else(|| format!("cases file is missing {name}").into())
}

fn text<'a>(value: &'a Value, name: &str) -> Result<&'a str, Box<dyn std::error::Error>> {
    field(value, name)?
        .as_str()
        .ok_or_else(|| format!("cases field {name} is not a string").into())
}

fn bytes32(value: &str) -> Result<[u8; 32], Box<dyn std::error::Error>> {
    if value.len() != 64
        || !value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
    {
        return Err("identifier is not 64 lowercase hex characters".into());
    }
    let mut out = [0; 32];
    for (index, slot) in out.iter_mut().enumerate() {
        *slot = u8::from_str_radix(&value[index * 2..index * 2 + 2], 16)?;
    }
    Ok(out)
}

struct Inputs {
    transport: AgentEnvelopeTransport,
    credential: EnvelopeCredential,
    cases: Value,
}

fn inputs() -> Result<Inputs, Box<dyn std::error::Error>> {
    let gateway = required_env(GATEWAY)?;
    let anchors = PathBuf::from(required_env(TRUST_ANCHORS)?);
    let cases_path = PathBuf::from(required_env(CASES)?);
    let cases: Value = serde_json::from_slice(&std::fs::read(&cases_path)?)?;
    let coordinates = field(&cases, "credential")?;
    let credential = EnvelopeCredential::new(
        text(coordinates, "tenant")?,
        bytes32(text(coordinates, "session_id")?)?,
        bytes32(text(coordinates, "token_id")?)?,
        canonical_u64(text(coordinates, "generation")?)
            .ok_or("generation is not a canonical decimal u64")?,
    )
    .map_err(|error| format!("credential refused: {error:?}"))?;
    let key = field(&cases, "gateway_key")?;
    let secret = std::fs::read(Path::new(text(key, "secret_file")?))?;
    let secret = SecretBytes::new(&secret).map_err(|_| "gateway key secret is empty")?;
    let gateway_key = LayerXKeyCredential::new(text(key, "key_id")?, secret)
        .map_err(|_| "gateway key identifier refused")?;
    let transport = AgentEnvelopeTransport::connect(&gateway, Some(gateway_key), Some(&anchors))
        .map_err(|error| format!("gateway transport refused: {error:?}"))?;
    Ok(Inputs {
        transport,
        credential,
        cases,
    })
}

fn catalogued(name: &str) -> Result<Operation, Box<dyn std::error::Error>> {
    Operation::ALL
        .iter()
        .copied()
        .find(|operation| operation.name() == name)
        .ok_or_else(|| format!("operation {name} is not catalogued").into())
}

fn send_case(
    inputs: &Inputs,
    case: &str,
    expected_operation: Option<&str>,
) -> Result<
    (
        Operation,
        Result<layerx_agent_api::error::ApiSuccess<Value>, EnvelopeError>,
    ),
    Box<dyn std::error::Error>,
> {
    let entry = field(field(&inputs.cases, "cases")?, case)?;
    let name = text(entry, "operation")?;
    if let Some(expected) = expected_operation {
        if name != expected {
            return Err(format!("case {case} must exercise {expected}, not {name}").into());
        }
    }
    let operation = catalogued(name)?;
    if operation.mutating() {
        return Err(format!("case {case} must be a non-mutating operation").into());
    }
    let request_id = RequestId(
        canonical_u64(text(entry, "request_id")?).ok_or("request_id is not canonical")?,
    );
    let result = inputs.transport.send_operation(
        operation,
        request_id,
        field(entry, "request")?,
        Some(&inputs.credential),
        None,
    );
    Ok((operation, result))
}

fn report(case: &str, operation: Operation) {
    println!(
        "{}",
        serde_json::json!({"case": case, "language": "rust", "operation": operation.name(), "result": "pass"})
    );
}

#[test]
#[ignore = "process probe: run only by tools/qualification/paxeer-x/agent_operation_envelope.py"]
fn sdk_rust_read() -> Probe {
    let inputs = inputs()?;

    let (operation, result) = send_case(&inputs, "read_account", Some("read.account"))?;
    let success = result.map_err(|error| format!("read_account failed: {error:?}"))?;
    match success.verification_status {
        VerificationStatus::Achieved(level) if level > Level::Unverified => {}
        status => return Err(format!("read_account verification not achieved: {status:?}").into()),
    }
    report("read_account", operation);

    let (operation, result) = send_case(&inputs, "program_read", None)?;
    if !operation.name().starts_with("program.") {
        return Err("program_read must exercise a program read operation".into());
    }
    let success = result.map_err(|error| format!("program_read failed: {error:?}"))?;
    match &success.verification_status {
        VerificationStatus::Achieved(level) if *level > Level::Unverified => {}
        VerificationStatus::Unverified { reason, .. }
            if reason.as_str() == "server_side_receipt_verification_only" => {}
        status => return Err(format!("program_read verification invalid: {status:?}").into()),
    }
    report("program_read", operation);

    let (operation, result) = send_case(&inputs, "approval_list", Some("approval.list"))?;
    result.map_err(|error| format!("approval_list failed: {error:?}"))?;
    report("approval_list", operation);

    let (operation, result) = send_case(&inputs, "faucet_retired", Some("faucet.claim"))?;
    match result {
        Err(EnvelopeError::Refused(error))
            if error.class == ErrorClass::UnavailableCapability
                && error.retriability == Retriability::Terminal
                && error.protocol_result_code.is_none()
                && error.reason.as_str() == "unavailable_capability.faucet.claim" => {}
        other => return Err(format!("faucet.claim was not retired: {other:?}").into()),
    }
    report("faucet_retired", operation);

    let read = operation_by_case(&inputs, "read_account")?;
    match inputs
        .transport
        .send_operation(read, RequestId(1), &serde_json::json!({}), None, None)
    {
        Err(EnvelopeError::CredentialPresence { .. }) => {}
        other => return Err(format!("missing credential was not refused: {other:?}").into()),
    }
    let mutation = catalogued("session.close")?;
    match inputs.transport.send_operation(
        mutation,
        RequestId(1),
        &serde_json::json!({}),
        Some(&inputs.credential),
        None,
    ) {
        Err(EnvelopeError::IdempotencyKeyPresence { .. }) => {}
        other => return Err(format!("mutation without key was not refused: {other:?}").into()),
    }
    Ok(())
}

fn operation_by_case(inputs: &Inputs, case: &str) -> Result<Operation, Box<dyn std::error::Error>> {
    catalogued(text(field(field(&inputs.cases, "cases")?, case)?, "operation")?)
}
