//! Process probe for the version 1 Agent operation envelope through a real unified
//! gateway and full-mode daemon, driven by
//! `tools/qualification/paxeer-x/agent_operation_envelope.py`. The case file named by
//! `PAXEER_X_AGENT_ENVELOPE_CASE` is required; every listed case runs and is reported, and
//! any absent input or failed case fails the probe. It never starts a local server.

use std::cell::RefCell;
use std::collections::BTreeSet;
use std::io::Write as _;
use std::net::TcpStream;
use std::sync::Arc;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use layerx_agent_api::error::{
    ApiSuccess, ErrorClass, Key, Level, RequestId, Retriability, VerificationStatus,
};
use layerx_sdk::agent_envelope::{
    canonical_u64, decode_response, decode_native_preparation, decode_native_approval, decode_native_approval_list,
    decode_proof_bundle_request, decode_proof_bundle_response, encode_proof_bundle_request,
    AgentEnvelopeTransport, ClientRetriability,
    EnvelopeCredential, EnvelopeError, AGENT_RPC_ROUTE,
};
use layerx_sdk::production::SecretBytes;
use layerx_sdk::programs::LayerXKeyCredential;
use layerx_sdk::Operation;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

const CASE_FILE: &str = "PAXEER_X_AGENT_ENVELOPE_CASE";
const PRE_RESTART: &str = "pre-restart";
const POST_RESTART: &str = "post-restart";
const READ_PHASE: &str = "read";
const LANGUAGE: &str = "rust";
const KEY_DOMAIN: &[u8] = b"paxeer-x/agent-envelope/idempotency\0";

type Failure = Box<dyn std::error::Error>;
type Outcome = Result<ApiSuccess<Value>, EnvelopeError>;

fn field<'a>(value: &'a Value, name: &str) -> Result<&'a Value, Failure> {
    value
        .get(name)
        .ok_or_else(|| format!("case file is missing {name}").into())
}

fn text<'a>(value: &'a Value, name: &str) -> Result<&'a str, Failure> {
    field(value, name)?
        .as_str()
        .filter(|text| !text.is_empty())
        .ok_or_else(|| format!("case field {name} is not a non-empty string").into())
}

fn bytes32(value: &str) -> Result<[u8; 32], Failure> {
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

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn catalogued(name: &str) -> Result<Operation, Failure> {
    Operation::ALL
        .iter()
        .copied()
        .find(|operation| operation.name() == name)
        .ok_or_else(|| format!("operation {name} is not catalogued").into())
}

struct Probe {
    transport: AgentEnvelopeTransport,
    route: String,
    ca_pem: PathBuf,
    authorization: String,
    gateway_key: (String, String),
    coordinates: Coordinates,
    credential: EnvelopeCredential,
    requests: Value,
    operations: Vec<String>,
    phase: String,
    state_file: PathBuf,
    response_dir: PathBuf,
    server_name: String,
    last: RefCell<Option<(u16, Value)>>,
    next_request_id: u64,
}

#[derive(Clone)]
struct Coordinates {
    tenant: String,
    session_id: [u8; 32],
    token_id: [u8; 32],
    generation: u64,
}

impl Coordinates {
    fn credential(&self) -> Result<EnvelopeCredential, Failure> {
        EnvelopeCredential::new(
            self.tenant.clone(),
            self.session_id,
            self.token_id,
            self.generation,
        )
        .map_err(|error| format!("credential refused: {error:?}").into())
    }
}

fn load() -> Result<(Probe, Vec<String>), Failure> {
    let path = match std::env::var(CASE_FILE) {
        Ok(value) if !value.is_empty() => PathBuf::from(value),
        _ => return Err(format!("required probe input {CASE_FILE} is absent").into()),
    };
    let case: Value = serde_json::from_slice(&std::fs::read(&path)?)?;
    let endpoint = text(&case, "endpoint")?;
    let base = endpoint
        .strip_suffix(AGENT_RPC_ROUTE)
        .ok_or("endpoint does not name the agent RPC route")?;
    let ca_pem = PathBuf::from(text(&case, "ca_pem")?);
    let server_name = text(&case, "server_name")?.to_owned();
    if url::Url::parse(endpoint)?.host_str() != Some(server_name.as_str()) {
        return Err("endpoint host differs from server_name; the SDK verifies the URL host".into());
    }
    let credential_document: Value =
        serde_json::from_slice(&std::fs::read(text(&case, "credential_file")?)?)?;
    let document = credential_document
        .as_object()
        .ok_or("credential file is not an object")?;
    let expected: BTreeSet<&str> = ["tenant", "session_id", "token_id", "generation"].into();
    if document.keys().map(String::as_str).collect::<BTreeSet<_>>() != expected {
        return Err("credential file does not carry exactly the session coordinates".into());
    }
    let coordinates = Coordinates {
        tenant: text(&credential_document, "tenant")?.to_owned(),
        session_id: bytes32(text(&credential_document, "session_id")?)?,
        token_id: bytes32(text(&credential_document, "token_id")?)?,
        generation: canonical_u64(text(&credential_document, "generation")?)
            .ok_or("generation is not a canonical decimal u64")?,
    };
    let credential = coordinates.credential()?;
    let key_text = std::fs::read_to_string(text(&case, "gateway_api_key_file")?)?;
    let (key_id, secret) = key_text
        .trim()
        .split_once(':')
        .ok_or("gateway API key file is not <id>:<secret>")?;
    let authorization = format!("LayerX-Key {key_id}:{secret}");
    let secret_text = secret.to_owned();
    let secret = SecretBytes::new(secret.as_bytes()).map_err(|_| "gateway key secret is empty")?;
    let gateway_key =
        LayerXKeyCredential::new(key_id, secret).map_err(|_| "gateway key identifier refused")?;
    if !Path::new(text(&case, "program_bearer_file")?).is_file() {
        return Err("program bearer file is absent".into());
    }
    let transport = AgentEnvelopeTransport::connect(base, Some(gateway_key), Some(&ca_pem))
        .map_err(|error| format!("gateway transport refused: {error:?}"))?;
    let operations = field(&case, "operations")?
        .as_array()
        .ok_or("operations is not an array")?
        .iter()
        .map(|name| {
            name.as_str()
                .map(str::to_owned)
                .ok_or_else(|| Failure::from("operation name is not a string"))
        })
        .collect::<Result<Vec<_>, _>>()?;
    let catalogue: Vec<&str> = Operation::ALL.iter().map(|operation| operation.name()).collect();
    if operations.len() != catalogue.len()
        || operations
            .iter()
            .collect::<BTreeSet<_>>()
            .into_iter()
            .map(String::as_str)
            .ne(catalogue.iter().copied().collect::<BTreeSet<_>>())
    {
        return Err("operations do not equal the generated catalogue".into());
    }
    let cases = field(&case, "cases")?
        .as_array()
        .ok_or("cases is not an array")?
        .iter()
        .map(|id| {
            id.as_str()
                .map(str::to_owned)
                .ok_or_else(|| Failure::from("case id is not a string"))
        })
        .collect::<Result<Vec<_>, _>>()?;
    if cases.is_empty() || cases.iter().collect::<BTreeSet<_>>().len() != cases.len() {
        return Err("cases must be a non-empty list of distinct ids".into());
    }
    let phase = text(&case, "phase")?.to_owned();
    if ![READ_PHASE, PRE_RESTART, POST_RESTART].contains(&phase.as_str()) {
        return Err(format!("unknown phase {phase}").into());
    }
    let response_dir = PathBuf::from(text(&case, "response_dir")?);
    if !response_dir.is_dir() {
        return Err("response_dir is absent".into());
    }
    let first_request_id = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or(Duration::ZERO)
        .as_micros();
    Ok((
        Probe {
            transport,
            route: endpoint.to_owned(),
            ca_pem,
            authorization,
            gateway_key: (key_id.to_owned(), secret_text),
            coordinates,
            credential,
            requests: field(&case, "requests")?.clone(),
            operations,
            phase,
            state_file: PathBuf::from(text(&case, "state_file")?),
            response_dir,
            server_name,
            last: RefCell::new(None),
            next_request_id: u64::try_from(first_request_id % u128::from(u32::MAX))? + 1,
        },
        cases,
    ))
}

fn verification_json(status: &VerificationStatus) -> Value {
    match status {
        VerificationStatus::Achieved(level) => {
            json!({"state": "achieved", "level": format!("{level:?}")})
        }
        VerificationStatus::Unverified {
            requested,
            achieved,
            reason,
        } => json!({
            "state": "unverified",
            "requested": format!("{requested:?}"),
            "achieved": format!("{achieved:?}"),
            "reason": reason.as_str(),
        }),
    }
}

fn outcome_json(outcome: &Outcome) -> Value {
    match outcome {
        Ok(success) => json!({
            "result": "success",
            "request_id": success.request_id.0.to_string(),
            "value": success.value,
            "verification_status": verification_json(&success.verification_status),
        }),
        Err(EnvelopeError::Refused(error)) => json!({
            "result": "error",
            "class": format!("{:?}", error.class),
            "protocol_result_code": error.protocol_result_code.map(|code| code.raw()),
            "retriability": format!("{:?}", error.retriability),
            "request_id": error.request_id.0.to_string(),
            "reason": error.reason.as_str(),
        }),
        Err(EnvelopeError::Unknown { operation }) => {
            json!({"result": "unknown", "operation": operation.name()})
        }
        Err(other) => json!({"result": "client_refusal", "error": format!("{other:?}")}),
    }
}

impl Probe {
    fn request_id(&mut self) -> RequestId {
        let id = RequestId(self.next_request_id);
        self.next_request_id += 1;
        id
    }

    /// SHA-256(KEY_DOMAIN || run nonce (32 bytes of requests.allowed_mutation.idempotency_key)
    /// || "rust" || NUL || case id): distinct per language and case, verifiable by the harness.
    fn fresh_key(&self, case: &str, _request_id: RequestId) -> Result<Key, Failure> {
        let nonce = self.provisioned_key("allowed_mutation")?;
        let digest = Sha256::new()
            .chain_update(KEY_DOMAIN)
            .chain_update(nonce.bytes())
            .chain_update(LANGUAGE.as_bytes())
            .chain_update([0_u8])
            .chain_update(case.as_bytes())
            .finalize();
        let mut bytes = [0_u8; 32];
        bytes.copy_from_slice(&digest);
        Key::new(bytes).map_err(|_| "derived idempotency key is reserved".into())
    }

    fn provisioned_key(&self, case: &str) -> Result<Key, Failure> {
        match field(&self.requests, case)?.get("idempotency_key") {
            Some(Value::String(text)) => {
                Key::new(bytes32(text)?).map_err(|_| "provisioned idempotency key is reserved".into())
            }
            _ => Err(format!("requests.{case} lacks its idempotency_key").into()),
        }
    }

    fn mutation(&self, name: &str) -> Result<(Operation, Value), Failure> {
        let entry = field(&self.requests, name)?;
        let operation = catalogued(text(entry, "operation")?)?;
        if !operation.mutating() {
            return Err(format!("provisioned {name} is not a mutating operation").into());
        }
        let request = field(entry, "request")?.clone();
        if !request.is_object() {
            return Err(format!("provisioned {name} request is not an object").into());
        }
        Ok((operation, request))
    }

    fn send(
        &self,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        credential: Option<&EnvelopeCredential>,
        key: Option<Key>,
    ) -> Outcome {
        let (received, outcome) = self
            .transport
            .send_operation_recorded(operation, request_id, request, credential, key);
        *self.last.borrow_mut() = received;
        outcome
    }

    fn record(&self, case: &str, operation: Operation, outcome: &Outcome) -> Result<(), Failure> {
        let _ = (operation, outcome_json(outcome));
        let document = match self.last.borrow_mut().take() {
            Some((status, body)) => json!({"status": status, "body": body}),
            None => json!({"status": null, "body": null}),
        };
        std::fs::write(
            self.response_dir.join(format!("{case}.json")),
            serde_json::to_vec_pretty(&document)?,
        )?;
        Ok(())
    }

    /// Posts an envelope that the SDK refuses to build, to prove the daemon refuses it too.
    fn raw(&self, envelope: &Value) -> Result<(u16, Value), Failure> {
        let pem = std::fs::read(&self.ca_pem)?;
        let mut roots = Vec::new();
        for item in ureq::tls::parse_pem(&pem) {
            if let ureq::tls::PemItem::Certificate(certificate) = item? {
                roots.push(certificate.to_owned());
            }
        }
        if roots.is_empty() {
            return Err("ca_pem carries no certificate".into());
        }
        let agent: ureq::Agent = ureq::Agent::config_builder()
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .provider(ureq::tls::TlsProvider::Rustls)
                    .root_certs(ureq::tls::RootCerts::new_with_certs(&roots))
                    .build(),
            )
            .timeout_global(Some(Duration::from_secs(30)))
            .http_status_as_error(false)
            .max_redirects(0)
            .build()
            .into();
        let mut response = agent
            .post(self.route.as_str())
            .header("Accept", "application/json")
            .header("Content-Type", "application/json")
            .header("Authorization", self.authorization.as_str())
            .send(serde_json::to_vec(envelope)?.as_slice())?;
        let status = response.status().as_u16();
        let body = response
            .body_mut()
            .with_config()
            .limit(1_048_576)
            .read_to_vec()?;
        Ok((status, serde_json::from_slice(&body)?))
    }

    fn query_case(
        &mut self,
        case: &str,
        operation_name: &str,
    ) -> Result<(Operation, Outcome), Failure> {
        let entry = field(&self.requests, case)?;
        if text(entry, "operation")? != operation_name {
            return Err(format!("requests.{case} must name {operation_name}").into());
        }
        let operation = catalogued(operation_name)?;
        let request = field(entry, "request")?.clone();
        if !request.is_object() {
            return Err(format!("requests.{case} request is not an object").into());
        }
        let request_id = self.request_id();
        let outcome = self.send(operation, request_id, &request, Some(&self.credential), None);
        self.record(case, operation, &outcome)?;
        Ok((operation, outcome))
    }

    fn run(&mut self, case: &str) -> Result<(), Failure> {
        let phase = self.phase.clone();
        match (phase.as_str(), case) {
            (READ_PHASE, "read") => self.read(),
            (READ_PHASE, "program_read") => self.program_read(),
            (READ_PHASE, "approval_list") => {
                let (_, outcome) = self.query_case(case, "approval.list")?;
                outcome.map_err(|error| format!("approval_list failed: {error:?}"))?;
                Ok(())
            }
            (_, "native_prepare" | "native_approval_list" | "native_approval_get" | "native_approval_approve" | "native_approval_reject") => self.native_case(case),
            (_, refusal) if refusal.starts_with("native_refusal.") => self.native_case(case),
            (_, proof_case) if proof_case.starts_with("proof_bundle.") => self.proof_bundle_case(case),
            (_, "read_decode_failure") => self.read_decode_failure(),
            (_, "mutation_decode_unknown") => self.mutation_decode_unknown(),
            (PRE_RESTART, "allowed_mutation") => self.allowed_mutation(),
            (PRE_RESTART, "mutation_duplicate_same_result") => self.duplicate(),
            (PRE_RESTART, "changed_body_same_key") => self.changed_body(),
            (PRE_RESTART, "missing_idempotency_key") => self.missing_key(),
            (PRE_RESTART, "wrong_scope") => self.wrong_scope(),
            (PRE_RESTART, "wrong_tenant") => {
                let mut coordinates = self.coordinates.clone();
                if coordinates.tenant.len() < 255 {
                    coordinates.tenant.push('x');
                } else {
                    coordinates.tenant.pop();
                }
                self.refused_credential(case, &coordinates)
            }
            (PRE_RESTART, "wrong_generation") => {
                let mut coordinates = self.coordinates.clone();
                coordinates.generation = coordinates
                    .generation
                    .checked_add(1)
                    .unwrap_or(coordinates.generation - 1);
                self.refused_credential(case, &coordinates)
            }
            (PRE_RESTART, "wrong_session") => {
                let mut coordinates = self.coordinates.clone();
                coordinates.session_id[31] ^= 0x01;
                self.refused_credential(case, &coordinates)
            }
            (PRE_RESTART, "wrong_token") => {
                let mut coordinates = self.coordinates.clone();
                coordinates.token_id[31] ^= 0x01;
                self.refused_credential(case, &coordinates)
            }
            (PRE_RESTART, "revoked_session") => self.revoked_session(),
            (PRE_RESTART, "restart_unknown_pending") => self.restart_pending(),
            (POST_RESTART, "restart_unknown_reconcile") => self.restart_reconcile(),
            (PRE_RESTART, operation_case) if operation_case.starts_with("operation.") => {
                self.operation_case(operation_case)
            }
            _ => Err(format!("case {case} is not defined for phase {phase}").into()),
        }
    }

    fn read(&mut self) -> Result<(), Failure> {
        let (_, outcome) = self.query_case("read", "read.account")?;
        let success = outcome.map_err(|error| format!("read failed: {error:?}"))?;
        match success.verification_status {
            VerificationStatus::Achieved(level) if level > Level::Unverified => {}
            status => return Err(format!("read verification not achieved: {status:?}").into()),
        }
        let read = catalogued("read.account")?;
        match self.send(read, RequestId(1), &json!({}), None, None) {
            Err(EnvelopeError::CredentialPresence { .. }) => {}
            other => return Err(format!("missing credential was not refused: {other:?}").into()),
        }
        let mutation = catalogued("session.close")?;
        match self.send(mutation, RequestId(1), &json!({}), Some(&self.credential), None) {
            Err(EnvelopeError::IdempotencyKeyPresence { .. }) => Ok(()),
            other => Err(format!("mutation without key was not refused: {other:?}").into()),
        }
    }

    fn program_read(&mut self) -> Result<(), Failure> {
        let (_, outcome) = self.query_case("program_read", "program.interface")?;
        let success = outcome.map_err(|error| format!("program_read failed: {error:?}"))?;
        match &success.verification_status {
            VerificationStatus::Achieved(level) if *level > Level::Unverified => Ok(()),
            VerificationStatus::Unverified { reason, .. }
                if reason.as_str() == "server_side_receipt_verification_only" =>
            {
                Ok(())
            }
            status => Err(format!("program_read verification invalid: {status:?}").into()),
        }
    }

    /// Transport for a decode case: `requests.<case>.endpoint` (the harness origin that
    /// serves the schema-violating body) when present, else the gateway endpoint.
    fn case_transport(&self, case: &str) -> Result<Option<AgentEnvelopeTransport>, Failure> {
        let Some(endpoint) = field(&self.requests, case)?.get("endpoint") else {
            return Ok(None);
        };
        let endpoint = endpoint.as_str().ok_or("case endpoint is not a string")?;
        let base = endpoint
            .strip_suffix(AGENT_RPC_ROUTE)
            .ok_or("case endpoint does not name the agent RPC route")?;
        let secret = SecretBytes::new(self.gateway_key.1.as_bytes())
            .map_err(|_| "gateway key secret is empty")?;
        let key = LayerXKeyCredential::new(&self.gateway_key.0, secret)
            .map_err(|_| "gateway key identifier refused")?;
        AgentEnvelopeTransport::connect(base, Some(key), Some(&self.ca_pem))
            .map(Some)
            .map_err(|error| format!("{case} transport refused: {error:?}").into())
    }

    fn send_case(
        &self,
        case: &str,
        operation: Operation,
        request_id: RequestId,
        request: &Value,
        key: Option<Key>,
    ) -> Result<Outcome, Failure> {
        let transport = self.case_transport(case)?;
        let (received, outcome) = transport.as_ref().unwrap_or(&self.transport).send_operation_recorded(
            operation,
            request_id,
            request,
            Some(&self.credential),
            key,
        );
        *self.last.borrow_mut() = received;
        Ok(outcome)
    }

    fn read_decode_failure(&mut self) -> Result<(), Failure> {
        let entry = field(&self.requests, "read_decode_failure")?;
        let operation = catalogued(text(entry, "operation")?)?;
        if operation.mutating() {
            return Err("requests.read_decode_failure must name a read".into());
        }
        let request = field(entry, "request")?.clone();
        let request_id = self.request_id();
        let outcome = self.send_case("read_decode_failure", operation, request_id, &request, None)?;
        let received = self.last.borrow().clone();
        self.record("read_decode_failure", operation, &outcome)?;
        match (&received, &outcome) {
            (Some((_, body)), Err(error @ EnvelopeError::Decode { operation: decoded }))
                if body.is_object()
                    && *decoded == operation
                    && error.client_retriability() == ClientRetriability::Never =>
            {
                Ok(())
            }
            _ => Err(format!(
                "schema-violating read was not a non-retriable DecodeFailure: {outcome:?}"
            )
            .into()),
        }
    }

    fn mutation_decode_unknown(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("mutation_decode_unknown")?;
        let request_id = self.request_id();
        let key = match field(&self.requests, "mutation_decode_unknown")?.get("idempotency_key") {
            Some(Value::String(text)) => {
                Key::new(bytes32(text)?).map_err(|_| "provisioned idempotency key is reserved")?
            }
            None | Some(Value::Null) => self.fresh_key("mutation_decode_unknown", request_id)?,
            Some(_) => return Err("provisioned idempotency key is not a string".into()),
        };
        let outcome =
            self.send_case("mutation_decode_unknown", operation, request_id, &request, Some(key))?;
        let received = self.last.borrow().clone();
        self.record("mutation_decode_unknown", operation, &outcome)?;
        match (&received, &outcome) {
            (Some((_, body)), Err(error @ EnvelopeError::Unknown { operation: unknown }))
                if body.is_object()
                    && *unknown == operation
                    && error.client_retriability() == ClientRetriability::Never =>
            {
                Ok(())
            }
            _ => Err(format!("schema-violating mutation reply was not Unknown: {outcome:?}").into()),
        }
    }

    fn allowed_mutation(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("allowed_mutation")?;
        let request_id = self.request_id();
        let key = match field(&self.requests, "allowed_mutation")?.get("idempotency_key") {
            Some(Value::String(text)) => {
                Key::new(bytes32(text)?).map_err(|_| "provisioned idempotency key is reserved")?
            }
            Some(_) => return Err("provisioned idempotency key is not a string".into()),
            None => return Err("requests.allowed_mutation lacks its idempotency_key".into()),
        };
        let outcome = self.send(operation, request_id, &request, Some(&self.credential), Some(key));
        self.record("allowed_mutation", operation, &outcome)?;
        outcome.map_err(|error| format!("allowed_mutation failed: {error:?}"))?;
        Ok(())
    }

    fn duplicate(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("allowed_mutation")?;
        let request_id = self.request_id();
        let key = self.fresh_key("mutation_duplicate_same_result", request_id)?;
        let first = self
            .send(operation, request_id, &request, Some(&self.credential), Some(key))
            .map_err(|error| format!("duplicate first send failed: {error:?}"))?;
        let repeated = self.send(operation, request_id, &request, Some(&self.credential), Some(key));
        self.record("mutation_duplicate_same_result", operation, &repeated)?;
        let repeated = repeated.map_err(|error| format!("duplicate repeat failed: {error:?}"))?;
        if repeated != first {
            return Err("same key and same body did not return the identical outcome".into());
        }
        Ok(())
    }

    fn changed_body(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("allowed_mutation")?;
        let (changed_operation, changed) = self.mutation("changed_body")?;
        if changed_operation != operation || changed == request {
            return Err("requests.changed_body must be the same operation with a changed body".into());
        }
        let request_id = self.request_id();
        let key = self.fresh_key("changed_body_same_key", request_id)?;
        self.send(operation, request_id, &request, Some(&self.credential), Some(key))
            .map_err(|error| format!("changed_body first send failed: {error:?}"))?;
        let second_id = self.request_id();
        let outcome = self.send(operation, second_id, &changed, Some(&self.credential), Some(key));
        self.record("changed_body_same_key", operation, &outcome)?;
        match outcome {
            Err(EnvelopeError::Refused(error))
                if error.class == ErrorClass::IdempotencyConflict
                    && error.reason.as_str() == "idempotency.body_changed" =>
            {
                Ok(())
            }
            other => Err(format!("changed body under the same key was not refused: {other:?}").into()),
        }
    }

    fn missing_key(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("allowed_mutation")?;
        let request_id = self.request_id();
        let envelope = json!({
            "version": 1,
            "request_id": request_id.0.to_string(),
            "operation": operation.name(),
            "request": request,
            "credential": {
                "tenant": self.coordinates.tenant,
                "session_id": hex(&self.coordinates.session_id),
                "token_id": hex(&self.coordinates.token_id),
                "generation": self.coordinates.generation.to_string(),
            },
            "idempotency_key": null,
        });
        let (status, document) = self.raw(&envelope)?;
        let decoded = decode_response(status, &document)
            .ok_or("missing_idempotency_key response is not a valid envelope")?;
        let outcome: Outcome = decoded.map_err(EnvelopeError::Refused);
        self.record("missing_idempotency_key", operation, &outcome)?;
        match outcome {
            Err(EnvelopeError::Refused(error))
                if status == 409
                    && error.class == ErrorClass::IdempotencyConflict
                    && error.reason.as_str() == "envelope.idempotency_key"
                    && error.request_id == request_id =>
            {
                Ok(())
            }
            other => Err(format!("mutation without key was not refused by the daemon: {other:?}").into()),
        }
    }

    fn wrong_scope(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("wrong_scope")?;
        let request_id = self.request_id();
        let key = self.provisioned_key("wrong_scope")?;
        let outcome = self.send(operation, request_id, &request, Some(&self.credential), Some(key));
        self.record("wrong_scope", operation, &outcome)?;
        match outcome {
            Err(EnvelopeError::Refused(error))
                if error.class == ErrorClass::PolicyRefusal =>
            {
                Ok(())
            }
            other => Err(format!("out-of-scope mutation was not refused: {other:?}").into()),
        }
    }

    fn refused_credential(&mut self, case: &str, coordinates: &Coordinates) -> Result<(), Failure> {
        let credential = coordinates.credential()?;
        let (operation, request) = self.mutation("allowed_mutation")?;
        let request_id = self.request_id();
        let key = self.fresh_key(case, request_id)?;
        let outcome = self.send(operation, request_id, &request, Some(&credential), Some(key));
        self.record(case, operation, &outcome)?;
        match outcome {
            Err(EnvelopeError::Refused(error)) if error.class == ErrorClass::PolicyRefusal => Ok(()),
            other => Err(format!("{case} credential was not refused: {other:?}").into()),
        }
    }

    fn revoked_session(&mut self) -> Result<(), Failure> {
        let entry = field(&self.requests, "revoked_session")?;
        let document: Value =
            serde_json::from_slice(&std::fs::read(text(entry, "credential_file")?)?)?;
        let coordinates = Coordinates {
            tenant: text(&document, "tenant")?.to_owned(),
            session_id: bytes32(text(&document, "session_id")?)?,
            token_id: bytes32(text(&document, "token_id")?)?,
            generation: canonical_u64(text(&document, "generation")?)
                .ok_or("revoked generation is not canonical")?,
        };
        let credential = coordinates.credential()?;
        let entry = field(&self.requests, "read")?;
        let operation = catalogued(text(entry, "operation")?)?;
        if operation != Operation::ReadAccount {
            return Err("requests.read must name read.account".into());
        }
        let request = field(entry, "request")?.clone();
        let request_id = self.request_id();
        let outcome = self.send(operation, request_id, &request, Some(&credential), None);
        let status = self.last.borrow().as_ref().map(|(status, _)| *status);
        self.record("revoked_session", operation, &outcome)?;
        match outcome {
            Err(EnvelopeError::Refused(error))
                if error.class == ErrorClass::PolicyRefusal && status == Some(403) =>
            {
                Ok(())
            }
            other => Err(format!("revoked session was not refused with 403: {other:?}").into()),
        }
    }

    fn restart_pending(&mut self) -> Result<(), Failure> {
        let (operation, request) = self.mutation("allowed_mutation")?;
        let request_id = self.request_id();
        let key = self.fresh_key("restart_unknown_pending", request_id)?;
        let envelope = json!({
            "version": 1,
            "request_id": request_id.0.to_string(),
            "operation": operation.name(),
            "request": request,
            "credential": self.wire_credential(),
            "idempotency_key": hex(&key.bytes()),
        });
        let state = json!({
            "request_id": request_id.0.to_string(),
            "idempotency_key": hex(&key.bytes()),
            "operation": operation.name(),
            "request": request,
        });
        std::fs::write(&self.state_file, serde_json::to_vec_pretty(&state)?)?;
        self.send_dropping_acknowledgement(&envelope)?;
        let outcome: Outcome = Err(EnvelopeError::Unknown { operation });
        self.record("restart_unknown_pending", operation, &outcome)
    }

    fn wire_credential(&self) -> Value {
        json!({
            "tenant": self.coordinates.tenant,
            "session_id": hex(&self.coordinates.session_id),
            "token_id": hex(&self.coordinates.token_id),
            "generation": self.coordinates.generation.to_string(),
        })
    }

    /// Writes one complete envelope over verified TLS and closes the connection before
    /// reading any response, so the acknowledgement is lost by construction.
    fn send_dropping_acknowledgement(&self, envelope: &Value) -> Result<(), Failure> {
        let pem = std::fs::read(&self.ca_pem)?;
        let mut roots = rustls::RootCertStore::empty();
        for item in ureq::tls::parse_pem(&pem) {
            if let ureq::tls::PemItem::Certificate(certificate) = item? {
                roots.add(rustls::pki_types::CertificateDer::from(certificate.der().to_vec()))?;
            }
        }
        if roots.is_empty() {
            return Err("ca_pem carries no certificate".into());
        }
        let config = rustls::ClientConfig::builder_with_provider(Arc::new(
            rustls::crypto::ring::default_provider(),
        ))
        .with_safe_default_protocol_versions()?
        .with_root_certificates(roots)
        .with_no_client_auth();
        let name = rustls::pki_types::ServerName::try_from(self.server_name.clone())?;
        let connection = rustls::ClientConnection::new(Arc::new(config), name)?;
        let url = url::Url::parse(&self.route)?;
        let port = url.port_or_known_default().ok_or("endpoint has no port")?;
        let socket = TcpStream::connect((self.server_name.as_str(), port))?;
        let mut stream = rustls::StreamOwned::new(connection, socket);
        let body = serde_json::to_vec(envelope)?;
        let head = format!(
            "POST {} HTTP/1.1\r\nHost: {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nAuthorization: {}\r\nConnection: close\r\n\r\n",
            url.path(),
            self.server_name,
            body.len(),
            self.authorization
        );
        stream.write_all(head.as_bytes())?;
        stream.write_all(&body)?;
        stream.flush()?;
        stream.conn.send_close_notify();
        stream.flush()?;
        Ok(())
    }

    fn restart_reconcile(&mut self) -> Result<(), Failure> {
        let state: Value = serde_json::from_slice(&std::fs::read(&self.state_file)?)?;
        let operation = catalogued(text(&state, "operation")?)?;
        let request_id = RequestId(
            canonical_u64(text(&state, "request_id")?).ok_or("recorded request_id is not canonical")?,
        );
        let key = Key::new(bytes32(text(&state, "idempotency_key")?)?)
            .map_err(|_| "recorded idempotency key is reserved")?;
        let request = field(&state, "request")?.clone();
        let outcome = self.send(operation, request_id, &request, Some(&self.credential), Some(key));
        self.record("restart_unknown_reconcile", operation, &outcome)?;
        match outcome {
            Ok(_) => Ok(()),
            Err(EnvelopeError::Refused(error))
                if !matches!(
                    error.class,
                    ErrorClass::IdempotencyConflict
                        | ErrorClass::ProtocolIncompatibility
                        | ErrorClass::InternalFault
                ) =>
            {
                Ok(())
            }
            other => Err(format!("restart did not report the stored outcome: {other:?}").into()),
        }
    }

    fn native_case(&mut self, case: &str) -> Result<(), Failure> {
        let entry = field(&self.requests, case)?.clone();
        let operation = catalogued(text(&entry, "operation")?)?;
        let request = field(&entry, "request")?.clone();
        let expected = match case {
            "native_prepare" => Some(Operation::Prepare),
            "native_approval_list" => Some(Operation::ApprovalList),
            "native_approval_get" => Some(Operation::ApprovalGet),
            "native_approval_approve" => Some(Operation::ApprovalApprove),
            "native_approval_reject" => Some(Operation::ApprovalReject),
            _ => None,
        };
        if expected.is_some_and(|expected| expected != operation)
            || !matches!(operation, Operation::Prepare | Operation::ApprovalList | Operation::ApprovalGet | Operation::ApprovalApprove | Operation::ApprovalReject) {
            return Err("native case operation mismatch".into());
        }
        let request_id = self.request_id();
        let key = if operation.mutating() { Some(self.provisioned_key(case)?) } else { None };
        let outcome = self.send(operation, request_id, &request, Some(&self.credential), key);
        self.record(case, operation, &outcome)?;
        if case.starts_with("native_refusal.") {
            return match outcome {
                Err(EnvelopeError::Refused(error))
                    if error.class != ErrorClass::InternalFault && error.class != ErrorClass::UnavailableCapability
                        && format!("{:?}", error.class) == text(&entry, "expected_class")?
                        && error.reason.as_str() == text(&entry, "expected_reason")? => Ok(()),
                other => Err(format!("native refusal mismatch: {other:?}").into()),
            };
        }
        if text(&request, "variant")? != "native_v1" {
            return Err("native case lacks explicit variant".into());
        }
        let response = outcome.map_err(|error| format!("native operation failed: {error:?}"))?;
        match operation {
            Operation::Prepare => {
                let value = decode_native_preparation(&response.value).ok_or("native preparation decoder refused")?;
                let purpose = field(field(&request, "purpose")?, "purpose")?;
                let activity = field(&request, "activity")?;
                let digest: [u8; 32] = Sha256::digest(&value.canonical_bytes).into();
                let signing: [u8; 32] = Sha256::new().chain_update(b"LXP/v1/signature-preimage\0")
                    .chain_update(&value.canonical_bytes).finalize().into();
                if value.preparation_id != digest || digest != bytes32(text(purpose, "canonical_digest")?)?
                    || value.preparation_id != bytes32(text(purpose, "preparation_id")?)?
                    || value.signing_preimage != signing
                    || u64::from(value.activity.module) != canonical_u64(text(activity, "module")?).ok_or("native module")?
                    || u64::from(value.activity.ordinal) != canonical_u64(text(activity, "ordinal")?).ok_or("native ordinal")?
                    || text(purpose, "tenant")? != self.coordinates.tenant
                    || bytes32(text(purpose, "session_id")?)? != self.coordinates.session_id
                    || canonical_u64(text(purpose, "generation")?) != Some(self.coordinates.generation)
                    || value.approval_required != field(&entry, "expected_approval_required")?.as_bool().ok_or("expected approval flag")?
                    || value.approval_id.is_some_and(|id| id != value.preparation_id) {
                    return Err("native preparation lost signed purpose or full identity binding".into());
                }
                let mut extra = response.value.clone();
                extra.as_object_mut().ok_or("native response object")?.insert("legacy_activity_type".into(), json!(value.activity.ordinal));
                if decode_native_preparation(&extra).is_some() { return Err("native decoder accepted unknown field".into()); }
                let mut truncated = response.value.clone();
                truncated["activity"]["module"] = json!(value.activity.module);
                if decode_native_preparation(&truncated).is_some() { return Err("native decoder accepted numeric module".into()); }
            }
            Operation::ApprovalList => {
                let value = decode_native_approval_list(&response.value).ok_or("native approval list decoder refused")?;
                let expected_id = bytes32(text(&entry, "expected_approval_id")?)?;
                if !value.approvals.iter().any(|approval| approval.approval_id == expected_id) {
                    return Err("native approval list omitted the durable preparation".into());
                }
            }
            Operation::ApprovalGet | Operation::ApprovalApprove | Operation::ApprovalReject => {
                let value = decode_native_approval(&response.value).ok_or("native approval decoder refused")?;
                if value.approval_id != bytes32(text(&request, "approval_id")?)?
                    || value.state != text(&entry, "expected_state")?
                    || value.held_digest != bytes32(text(&entry, "expected_held_digest")?)?
                    || operation.mutating() && value.held_digest != bytes32(text(&request, "held_digest")?)? {
                    return Err("native approval lost exact held consent or durable state".into());
                }
            }
            _ => return Err("unsupported native operation".into()),
        }
        Ok(())
    }

    fn proof_bundle_case(&mut self, case: &str) -> Result<(), Failure> {
        let entry = field(&self.requests, case)?.clone();
        let operation = catalogued(text(&entry, "operation")?)?;
        if operation != Operation::ReadProofBundle {
            return Err("proof bundle case operation mismatch".into());
        }
        let request = field(&entry, "request")?.clone();
        let request_id = self.request_id();
        let outcome = self.send_case(case, operation, request_id, &request, None)?;
        let received_status = self.last.borrow().as_ref().map(|(status, _)| *status);
        self.record(case, operation, &outcome)?;
        if case.starts_with("proof_bundle.refusal.") {
            let expected_status = match entry.get("expected_status") {
                Some(value) => u16::try_from(value.as_u64().ok_or("expected_status is not an integer")?)?,
                None => 403,
            };
            if received_status != Some(expected_status) {
                return Err("proof bundle refusal HTTP status mismatch".into());
            }
            return match outcome {
                Err(EnvelopeError::Refused(error))
                    if format!("{:?}", error.class) == text(&entry, "expected_class")?
                        && error.reason.as_str() == text(&entry, "expected_reason")? => Ok(()),
                other => Err(format!("proof bundle refusal mismatch: {other:?}").into()),
            };
        }
        let response = outcome.map_err(|error| format!("proof bundle failed: {error:?}"))?;
        let typed_request = decode_proof_bundle_request(&request)
            .ok_or("proof bundle request decoder refused real request")?;
        if encode_proof_bundle_request(&typed_request).map_err(|error| format!("proof bundle encoder refused: {error:?}"))? != request {
            return Err("proof bundle request changed in SDK round trip".into());
        }
        let read = decode_proof_bundle_response(&typed_request, &response)
            .ok_or("proof bundle response decoder refused real owner response")?;
        let record = read.value.record().map_err(|error| format!("proof bundle record refused: {error:?}"))?;
        let expected_variant = field(&entry, "expected_variant")?.as_u64()
            .ok_or("proof bundle expected_variant is not an integer")?;
        if u64::from(record.variant as u8) != expected_variant {
            return Err("proof bundle returned another native variant".into());
        }
        if record.encode().map_err(|error| format!("proof bundle encoding refused: {error:?}"))? != read.value.proofs[0] {
            return Err("SDK altered native proof, canonical value, or maintenance fields".into());
        }
        layerx_sdk::Client::accept_proof_bundle(&typed_request, read.clone())
            .map_err(|error| format!("typed SDK refused proof bundle: {error:?}"))?;
        proof_bundle_decoder_negatives(&request, &response)?;
        Ok(())
    }

    fn operation_case(&mut self, case: &str) -> Result<(), Failure> {
        let name = &case["operation.".len()..];
        if !self.operations.iter().any(|operation| operation == name) {
            return Err(format!("{case} is not in the provisioned operations").into());
        }
        let operation = catalogued(name)?;
        let entry = field(&self.requests, case)?;
        if text(entry, "operation")? != name {
            return Err(format!("requests.{case} names a different operation").into());
        }
        let request = field(entry, "request")?.clone();
        if !request.is_object() {
            return Err(format!("requests.{case} request is not an object").into());
        }
        let provisioned_key = match entry.get("idempotency_key") {
            Some(Value::String(text)) => Some(
                Key::new(bytes32(text)?).map_err(|_| "provisioned idempotency key is reserved")?,
            ),
            None | Some(Value::Null) => None,
            Some(_) => return Err("provisioned idempotency key is not a string".into()),
        };
        let request_id = self.request_id();
        let key = match (operation.mutating(), provisioned_key) {
            (true, Some(key)) => Some(key),
            (true, None) => Some(self.fresh_key(case, request_id)?),
            (false, None) => None,
            (false, Some(_)) => return Err(format!("{case} is not mutating but carries a key").into()),
        };
        let bootstrap = matches!(operation, Operation::AgentRegister | Operation::SessionOpen);
        let credential = (!bootstrap).then_some(&self.credential);
        let outcome = self.send(operation, request_id, &request, credential, key);
        self.record(case, operation, &outcome)?;
        if operation == Operation::FaucetClaim {
            return match outcome {
                Err(EnvelopeError::Refused(error))
                    if error.class == ErrorClass::UnavailableCapability
                        && error.retriability == Retriability::Terminal
                        && error.protocol_result_code.is_none()
                        && error.reason.as_str() == "unavailable_capability.faucet.claim" =>
                {
                    Ok(())
                }
                other => Err(format!("faucet.claim was not retired: {other:?}").into()),
            };
        }
        match outcome {
            Ok(_) => Ok(()),
            Err(EnvelopeError::Refused(error))
                if matches!(
                    error.class,
                    ErrorClass::UnavailableCapability
                        | ErrorClass::ProtocolIncompatibility
                        | ErrorClass::InternalFault
                ) =>
            {
                Err(format!(
                    "{case} was not dispatched by its owner: {:?} {}",
                    error.class,
                    error.reason.as_str()
                )
                .into())
            }
            Err(EnvelopeError::Refused(_)) => Ok(()),
            Err(other) => Err(format!("{case} failed: {other:?}").into()),
        }
    }
}

fn proof_bundle_decoder_negatives(request: &Value, response: &ApiSuccess<Value>) -> Result<(), Failure> {
    let typed_request = decode_proof_bundle_request(request).ok_or("valid proof request missing")?;
    let read = decode_proof_bundle_response(&typed_request, response).ok_or("valid proof response missing")?;
    let mut request_cases = Vec::new();
    for target in [String::new(), "00".to_owned(), "00".repeat(68)] {
        let mut changed = request.clone();
        changed["target"] = json!(target);
        request_cases.push(changed);
    }
    let target = typed_request.selector.as_bytes();
    for offset in [0_usize, 2] {
        let mut bytes = target.to_vec();
        bytes[offset] = 255;
        let mut changed = request.clone();
        changed["target"] = json!(hex(&bytes));
        request_cases.push(changed);
    }
    let mut zero_activity = target.to_vec();
    zero_activity[3..35].fill(0);
    let mut changed = request.clone();
    changed["target"] = json!(hex(&zero_activity));
    request_cases.push(changed);
    if target.len() == 67 {
        let mut zero_account = target.to_vec();
        zero_account[35..].fill(0);
        let mut changed = request.clone();
        changed["target"] = json!(hex(&zero_account));
        request_cases.push(changed);
    }
    let mut changed = request.clone();
    changed["target"] = json!(format!("{}A", hex(target)));
    request_cases.push(changed);
    let mut changed = request.clone();
    changed["requested_verification_level"] = json!("Finalised");
    request_cases.push(changed);
    let mut changed = request.clone();
    changed["sequencer_key"] = json!("00".repeat(32));
    request_cases.push(changed);
    for changed in request_cases {
        if decode_proof_bundle_request(&changed).is_some() {
            return Err("SDK accepted a malformed PB1 request".into());
        }
    }
    let mut response_cases = Vec::new();
    let mut changed = response.value.clone();
    let mut wrong_target = target.to_vec();
    wrong_target[34] ^= 1;
    changed["value"]["target"] = json!(hex(&wrong_target));
    response_cases.push(changed);
    for proofs in [json!([]), json!([hex(read.value.proofs[0].as_bytes()), hex(read.value.proofs[0].as_bytes())]),
        json!(["00".repeat(layerx_agent_api::proof::MAX_PROOF_BUNDLE_BYTES + 1)])] {
        let mut changed = response.value.clone();
        changed["value"]["proofs"] = proofs;
        response_cases.push(changed);
    }
    let original = read.value.proofs[0].as_bytes();
    let mut frames = Vec::new();
    let mut changed = original.to_vec();
    changed[0] ^= 1;
    frames.push(changed);
    let mut changed = original.to_vec();
    changed[5] = 0;
    frames.push(changed);
    let mut changed = original.to_vec();
    changed[6..10].fill(0);
    frames.push(changed);
    let mut changed = original.to_vec();
    changed.pop();
    frames.push(changed);
    let mut changed = original.to_vec();
    changed.push(0);
    frames.push(changed);
    if read.value.record().map_err(|error| format!("native record refused: {error:?}"))?.activity_receipt.is_some() {
        let mut offset = 6_usize;
        for _ in 0..2 {
            let length = u32::from_be_bytes(original[offset..offset + 4].try_into()?) as usize;
            offset += 4 + length;
        }
        frames.push(original[..offset].to_vec());
    }
    for bytes in frames {
        let mut changed = response.value.clone();
        changed["value"]["proofs"] = json!([hex(&bytes)]);
        response_cases.push(changed);
    }
    for level in ["Unverified", "CheckpointFinalised", "SettlementAnchored"] {
        let mut changed = response.value.clone();
        changed["achieved_verification_level"] = json!(level);
        response_cases.push(changed);
    }
    for (field, value) in [("chain_head", json!("01")), ("value_sequence", json!("18446744073709551616")),
        ("latest_sealed_batch", json!(1)), ("latest_finalised_checkpoint", json!("00")),
        ("relative_to", json!({"checkpoint":"00".repeat(32)}))] {
        let mut changed = response.value.clone();
        changed["freshness"][field] = value;
        response_cases.push(changed);
    }
    let mut changed = response.value.clone();
    changed["freshness"].as_object_mut().ok_or("freshness not object")?.remove("chain_head");
    response_cases.push(changed);
    for value in response_cases {
        let changed = ApiSuccess {
            request_id: response.request_id,
            value,
            verification_status: response.verification_status.clone(),
        };
        if decode_proof_bundle_response(&typed_request, &changed).is_some() {
            return Err("SDK accepted a malformed owner response envelope or record frame".into());
        }
    }
    let changed = ApiSuccess {
        request_id: response.request_id,
        value: response.value.clone(),
        verification_status: VerificationStatus::Achieved(Level::Unverified),
    };
    if decode_proof_bundle_response(&typed_request, &changed).is_some() {
        return Err("SDK accepted disagreement between outer and inner verification levels".into());
    }
    let mut higher_request = typed_request;
    higher_request.requested_verification_level = Level::CheckpointFinalised;
    if decode_proof_bundle_response(&higher_request, response).is_some() {
        return Err("SDK promoted a native proof above its achieved verification level".into());
    }
    Ok(())
}

#[test]
#[ignore = "process probe: run only by tools/qualification/paxeer-x/agent_operation_envelope.py with --ignored"]
fn agent_operation_envelope_process_cases() -> Result<(), Failure> {
    let (mut probe, cases) = load()?;
    let mut passed = 0_usize;
    let mut failures = Vec::new();
    for case in &cases {
        match probe.run(case) {
            Ok(()) => {
                println!("PAXEER_X_AGENT_ENVELOPE_CASE {case} passed");
                passed += 1;
            }
            Err(error) => failures.push(format!("{case}: {error}")),
        }
    }
    println!("PAXEER_X_AGENT_ENVELOPE_CASES={passed}");
    if failures.is_empty() {
        Ok(())
    } else {
        Err(failures.join("; ").into())
    }
}
