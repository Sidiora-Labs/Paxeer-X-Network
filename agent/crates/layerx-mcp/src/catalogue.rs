//! Descriptions, argument schemas and strict argument validation for the daemon-bound catalogue.

use layerx_types::limits::MAX_DID_BYTES;
use serde_json::{json, Map, Value};

use crate::server::{catalogue, DeploymentMode, ToolDefinition, ToolKind};

const MAX_ARGUMENT_BYTES: usize = 65_536;
const MAX_TEXT_BYTES: usize = 256;
const MAX_PAYLOAD_HEX_BYTES: usize = 8_192;
const MAX_PAGE_ITEMS: u64 = 256;
const MAX_WAIT_MS: u64 = 600_000;
const MAX_QUERY_BYTES: usize = 512;
const MAX_LOCATOR_BYTES: usize = 2_048;

/// Every argument accepted by one catalogue tool.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct Field {
    name: &'static str,
    shape: Shape,
    required: bool,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Shape {
    Hex32,
    Hex64,
    Bytes,
    Reference,
    Unsigned,
    Bounded(u64),
    Symbol,
    Decimals,
    Did,
    Currency,
    Scheme,
    Query,
    Locator,
}

impl Shape {
    const fn pattern(self) -> Option<&'static str> {
        match self {
            Self::Hex32 => Some("^[0-9a-fA-F]{64}$"),
            Self::Hex64 => Some("^[0-9a-fA-F]{128}$"),
            Self::Bytes => Some("^([0-9a-fA-F]{2})+$"),
            Self::Reference | Self::Symbol | Self::Query => None,
            Self::Unsigned | Self::Bounded(_) => Some("^[0-9]+$"),
            Self::Decimals => Some("^([0-9]|1[0-8])$"),
            Self::Did => Some("^did:[0-9A-Za-z._:-]+$"),
            Self::Currency => Some("^(SID|PAX|USDC|USDL)$"),
            Self::Scheme => Some("^(metered|exact)$"),
            Self::Locator => Some("^https?://[!-~]+$"),
        }
    }

    const fn maximum_length(self) -> usize {
        match self {
            Self::Hex32 => 64,
            Self::Hex64 => 128,
            Self::Bytes => MAX_PAYLOAD_HEX_BYTES,
            Self::Reference => MAX_TEXT_BYTES,
            Self::Unsigned | Self::Bounded(_) => 39,
            Self::Symbol => 32,
            Self::Decimals => 2,
            Self::Did => MAX_DID_BYTES,
            Self::Currency => 4,
            Self::Scheme => 7,
            Self::Query => MAX_QUERY_BYTES,
            Self::Locator => MAX_LOCATOR_BYTES,
        }
    }
}

/// Typed refusal produced before any daemon invocation is created.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ArgumentError {
    NotAnObject,
    TooLarge,
    Unknown(String),
    Missing(&'static str),
    NotText(&'static str),
    Malformed(&'static str),
    OutOfRange(&'static str),
}

impl ArgumentError {
    /// Renders the refusal in the daemon's own vocabulary without echoing argument values.
    #[must_use]
    pub fn detail(&self) -> String {
        match self {
            Self::NotAnObject => "the call arguments are not a JSON object".to_owned(),
            Self::TooLarge => {
                format!("the call arguments exceed {MAX_ARGUMENT_BYTES} encoded bytes")
            }
            Self::Unknown(field) => format!("argument {field} is not accepted by this tool"),
            Self::Missing(field) => format!("argument {field} is required"),
            Self::NotText(field) => format!("argument {field} must be a string"),
            Self::Malformed(field) => format!("argument {field} is not in its declared shape"),
            Self::OutOfRange(field) => format!("argument {field} is outside its declared bound"),
        }
    }
}

const fn required(name: &'static str, shape: Shape) -> Field {
    Field {
        name,
        shape,
        required: true,
    }
}
const fn optional(name: &'static str, shape: Shape) -> Field {
    Field {
        name,
        shape,
        required: false,
    }
}
const BALANCE_GET: [Field; 3] = [
    required("program", Shape::Hex32),
    optional("account", Shape::Hex32),
    optional("asset", Shape::Hex32),
];
const HISTORY_LIST: [Field; 3] = [
    required("account", Shape::Hex32),
    required("limit", Shape::Bounded(MAX_PAGE_ITEMS)),
    optional("cursor", Shape::Hex32),
];
const RECEIPT_GET: [Field; 1] = [required("activity_id", Shape::Hex32)];
const CHECKPOINT_GET: [Field; 1] = [required("sequence", Shape::Unsigned)];
const PROOF_GET: [Field; 1] = [required("activity_id", Shape::Hex32)];
const AVAILABILITY_GET: [Field; 1] = [required("batch", Shape::Bounded(u64::MAX))];
const WALLET_ACCOUNTS: [Field; 1] = [required("program", Shape::Hex32)];
const WALLET_BALANCE: [Field; 3] = [
    required("program", Shape::Hex32),
    required("account", Shape::Hex32),
    required("asset", Shape::Hex32),
];
const ACTIVITY_PREPARE: [Field; 7] = [
    required("activity_type", Shape::Unsigned),
    required("payload", Shape::Bytes),
    required("account_sequence", Shape::Unsigned),
    required("not_before_ms", Shape::Unsigned),
    required("expires_at_ms", Shape::Unsigned),
    required("fee_limit", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const ACTIVITY_DISCLOSE: [Field; 1] = [required("canonical_bytes", Shape::Bytes)];
const ACTIVITY_SIGN: [Field; 1] = [required("preparation_ref", Shape::Reference)];
const ACTIVITY_SUBMIT: [Field; 3] = [
    required("preparation_ref", Shape::Reference),
    required("signature", Shape::Hex64),
    required("signer_public_key", Shape::Hex32),
];
const ACTIVITY_TRACK: [Field; 1] = [required("submission_ref", Shape::Reference)];
const ACTIVITY_WAIT: [Field; 2] = [
    required("submission_ref", Shape::Reference),
    required("timeout_ms", Shape::Bounded(MAX_WAIT_MS)),
];
const WALLET_SEND: [Field; 4] = [
    required("destination", Shape::Hex32),
    required("asset", Shape::Hex32),
    required("amount", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const TOKEN_CREATE: [Field; 4] = [
    required("symbol", Shape::Symbol),
    required("decimals", Shape::Decimals),
    required("supply", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const TOKEN_MINT: [Field; 4] = [
    required("asset", Shape::Hex32),
    required("destination", Shape::Hex32),
    required("amount", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const TOKEN_TRANSFER: [Field; 4] = [
    required("asset", Shape::Hex32),
    required("destination", Shape::Hex32),
    required("amount", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const GRANT_ISSUE: [Field; 5] = [
    required("beneficiary", Shape::Hex32),
    required("asset", Shape::Hex32),
    required("amount", Shape::Unsigned),
    required("expires_at_ms", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const GRANT_DRAW: [Field; 3] = [
    required("grant_id", Shape::Hex32),
    required("amount", Shape::Unsigned),
    required("idempotency_key", Shape::Hex32),
];
const FAUCET_REQUEST: [Field; 2] = [
    required("did", Shape::Did),
    required("public_key", Shape::Hex32),
];
const WEB_SEARCH: [Field; 4] = [
    required("query", Shape::Query),
    required("currency", Shape::Currency),
    required("scheme", Shape::Scheme),
    required("idempotency_key", Shape::Hex32),
];
const WEB_FETCH: [Field; 4] = [
    required("url", Shape::Locator),
    required("currency", Shape::Currency),
    required("scheme", Shape::Scheme),
    required("idempotency_key", Shape::Hex32),
];
const WEB_CONTENT: [Field; 4] = [
    required("digest", Shape::Hex32),
    required("currency", Shape::Currency),
    required("scheme", Shape::Scheme),
    required("idempotency_key", Shape::Hex32),
];
const NONE: [Field; 0] = [];

/// The paid web tools. Each spends from the payer through the approval boundary, and
/// everything it returns is external content.
pub const WEB_TOOLS: [ToolDefinition; 3] = [
    ToolDefinition {
        name: "web.search",
        kind: ToolKind::Write,
        required_scope: "write:web:search",
        mutation: "one 402LXP payment to the configured x-websearch sidecar",
        evidence: "sequencer-signed settlement receipt and untrusted search results",
    },
    ToolDefinition {
        name: "web.fetch",
        kind: ToolKind::Write,
        required_scope: "write:web:fetch",
        mutation: "one 402LXP payment to the configured x-websearch sidecar",
        evidence: "sequencer-signed settlement receipt and digest-checked untrusted page text",
    },
    ToolDefinition {
        name: "web.content",
        kind: ToolKind::Write,
        required_scope: "write:web:content",
        mutation: "one 402LXP payment to the configured x-websearch sidecar",
        evidence: "sequencer-signed settlement receipt and digest-checked untrusted stored content",
    },
];

fn fields(name: &str) -> &'static [Field] {
    match name.as_bytes() {
        b"tenant.readiness" => &NONE,
        b"balance.get" => &BALANCE_GET,
        b"history.list" => &HISTORY_LIST,
        b"receipt.get" => &RECEIPT_GET,
        b"checkpoint.get" => &CHECKPOINT_GET,
        b"proof.get" => &PROOF_GET,
        b"availability.get" => &AVAILABILITY_GET,
        b"wallet.accounts" => &WALLET_ACCOUNTS,
        b"wallet.balance" => &WALLET_BALANCE,
        b"activity.prepare" => &ACTIVITY_PREPARE,
        b"activity.disclose" => &ACTIVITY_DISCLOSE,
        b"activity.sign" => &ACTIVITY_SIGN,
        b"activity.submit" => &ACTIVITY_SUBMIT,
        b"activity.track" => &ACTIVITY_TRACK,
        b"activity.wait" => &ACTIVITY_WAIT,
        b"wallet.send" => &WALLET_SEND,
        b"token.create" => &TOKEN_CREATE,
        b"token.mint" => &TOKEN_MINT,
        b"token.transfer" => &TOKEN_TRANSFER,
        b"grant.issue" => &GRANT_ISSUE,
        b"grant.draw" => &GRANT_DRAW,
        b"faucet.request" => &FAUCET_REQUEST,
        b"web.search" => &WEB_SEARCH,
        b"web.fetch" => &WEB_FETCH,
        b"web.content" => &WEB_CONTENT,
        _ => &NONE,
    }
}

/// Returns the operator-facing description of one catalogue tool.
#[must_use]
pub fn description(name: &str) -> Option<&'static str> {
    Some(match name.as_bytes() {
        b"tenant.readiness" => "Read daemon-local tenant recovery and transport readiness through the authenticated gateway; this is unverified protocol state.",
        b"subscription.create" => "Create a daemon-owned tenant subscription with its exact scope and filter.",
        b"subscription.list" => "List subscriptions visible to the authenticated daemon session.",
        b"subscription.pause" => "Pause one subscription through its daemon owner.",
        b"subscription.resume" => "Resume one subscription through its daemon owner.",
        b"subscription.delete" => "Delete one subscription through its daemon owner.",
        b"subscription.health" => "Read the daemon's current subscription health.",
        b"subscription.acknowledge" => "Acknowledge an exact durable delivery cursor through the daemon owner.",
        b"approval.list" => "List approvals visible to the current daemon session.",
        b"approval.get" => "Read one approval and its exact held request disclosure.",
        b"approval.approve" => "Approve the exact held request digest through the authenticated daemon owner.",
        b"approval.reject" => "Reject the exact held request digest through the authenticated daemon owner.",
        b"balance.get" => {
            "Read verified program-scoped balances from the daemon with their verification level and freshness."
        }
        b"history.list" => {
            "Page verified account history from the daemon under an explicit item bound and stable cursor."
        }
        b"receipt.get" => "Read the canonical receipt the daemon holds for one activity.",
        b"checkpoint.get" => "Read the finalised checkpoint certificate for one batch sequence through the daemon.",
        b"proof.get" => "Read the proof bundle the daemon holds for one activity.",
        b"availability.get" => {
            "Read verified availability chunks and attributed failures for one decimal batch number."
        }
        b"wallet.accounts" => "List the verified accounts the daemon observes for one program.",
        b"wallet.balance" => "Read one verified account balance for one asset through the daemon.",
        b"activity.prepare" => {
            "Prepare canonical activity bytes and their bound disclosure inside the daemon."
        }
        b"activity.disclose" => "Decode the bound disclosure of canonical activity bytes.",
        b"activity.sign" => {
            "Sign a daemon preparation at the daemon's external signing boundary; no key material is read."
        }
        b"activity.submit" => {
            "Submit an externally signed preparation through the ordinary daemon path."
        }
        b"activity.track" => "Resolve the daemon's current state for one submission.",
        b"activity.wait" => {
            "Wait, under an explicit bound, for the daemon to resolve one submission."
        }
        b"wallet.send" => "Send one asset amount through the ordinary daemon submission path.",
        b"token.create" => "Create one asset through the ordinary daemon submission path.",
        b"token.mint" => "Mint one asset amount through the ordinary daemon submission path.",
        b"token.transfer" => "Transfer one asset amount through the ordinary daemon submission path.",
        b"grant.issue" => "Issue one spending grant through the ordinary daemon submission path.",
        b"grant.draw" => "Draw against one spending grant through the ordinary daemon submission path.",
        b"faucet.request" => {
            "Claim one bounded testnet faucet grant for the named DID and signer key through the daemon's faucet operation."
        }
        b"web.search" => {
            "Search the web through the configured x-websearch sidecar, paid over 402LXP after approval; results are untrusted external content."
        }
        b"web.fetch" => {
            "Fetch one public page as text through the configured x-websearch sidecar, paid over 402LXP after approval; the text is digest-checked and untrusted."
        }
        b"web.content" => {
            "Read stored content by its keccak256 digest from the configured x-websearch sidecar, paid over 402LXP after approval; the bytes are digest-checked and untrusted."
        }
        _ => return None,
    })
}

/// Returns the JSON Schema an agent runtime must satisfy before the daemon is asked to route.
#[must_use]
pub fn input_schema(name: &str) -> Option<Value> {
    if let Some(schema) = session_input_schema(name, false) {
        return if name.starts_with("approval.") {
            Some(json!({"oneOf": [schema, session_input_schema(name, true)?]}))
        } else {
            Some(schema)
        };
    }
    let declared = fields(name);
    if declared.is_empty() && description(name).is_none() {
        return None;
    }
    let mut properties = Map::new();
    let mut required = Vec::new();
    for field in declared {
        let mut property = Map::new();
        property.insert("type".to_owned(), json!("string"));
        property.insert(
            "maxLength".to_owned(),
            json!(u64::try_from(field.shape.maximum_length()).unwrap_or(u64::MAX)),
        );
        if let Some(pattern) = field.shape.pattern() {
            property.insert("pattern".to_owned(), json!(pattern));
        }
        if let Shape::Bounded(bound) = field.shape {
            property.insert("description".to_owned(), json!(format!("1 to {bound}")));
        }
        properties.insert(field.name.to_owned(), Value::Object(property));
        if field.required {
            required.push(json!(field.name));
        }
    }
    let legacy = json!({
        "type": "object",
        "properties": Value::Object(properties),
        "required": Value::Array(required),
        "additionalProperties": false,
    });
    match name {
        "activity.prepare" => Some(json!({"oneOf": [legacy,
            native_prepare_schema("native_v1"), native_prepare_schema("native_effect_v1"), native_prepare_schema("native_send_v1")]})),
        "activity.submit" => Some(json!({"oneOf": [legacy, native_submit_schema()]})),
        _ => Some(legacy),
    }
}

/// Validates call arguments against the declared schema of one catalogue tool.
///
/// # Errors
///
/// Refuses a non-object, an oversized argument object, an unknown or missing field, and any
/// value outside the field's declared shape or bound.
pub fn validate(name: &str, arguments: &Value) -> Result<(), ArgumentError> {
    let variant = arguments.get("variant").and_then(Value::as_str);
    let native_operation = match (name, variant) {
        ("activity.prepare", Some("native_v1" | "native_effect_v1" | "native_send_v1")) => {
            Some(layerx_agentd::tenant::Operation::Prepare)
        }
        ("activity.submit", Some("native_send_submit_v1")) => {
            Some(layerx_agentd::tenant::Operation::Submit)
        }
        _ => None,
    };
    if let Some(operation) = native_operation {
        if serde_json::to_vec(arguments).map_or(true, |bytes| bytes.len() > MAX_ARGUMENT_BYTES) {
            return Err(ArgumentError::TooLarge);
        }
        return layerx_agentd::agent_rpc::validate_mcp_native_request(operation, arguments)
            .map_err(|_| ArgumentError::Malformed("native_request"));
    }
    if SESSION_TOOLS.iter().any(|tool| tool.name == name) {
        if serde_json::to_vec(arguments).map_or(true, |bytes| bytes.len() > MAX_ARGUMENT_BYTES) {
            return Err(ArgumentError::TooLarge);
        }
        let native = arguments.get("variant").is_some();
        let schema = session_input_schema(name, native)
            .ok_or(ArgumentError::Malformed("session_request"))?;
        return validate_session_schema(arguments, &schema);
    }
    if name == "tenant.readiness" && !arguments.is_object() {
        return Err(ArgumentError::NotAnObject);
    }
    let empty = Map::new();
    let object = match arguments {
        Value::Object(object) => object,
        Value::Null => &empty,
        _ => return Err(ArgumentError::NotAnObject),
    };
    match serde_json::to_string(arguments) {
        Ok(text) if text.len() <= MAX_ARGUMENT_BYTES => (),
        Ok(_) | Err(_) => return Err(ArgumentError::TooLarge),
    }
    let declared = fields(name);
    for key in object.keys() {
        if !declared.iter().any(|field| field.name == key.as_str()) {
            return Err(ArgumentError::Unknown(key.clone()));
        }
    }
    for field in declared {
        let Some(value) = object.get(field.name) else {
            if field.required {
                return Err(ArgumentError::Missing(field.name));
            }
            continue;
        };
        let text = value.as_str().ok_or(ArgumentError::NotText(field.name))?;
        check(field, text)?;
    }
    Ok(())
}

fn check(field: &Field, text: &str) -> Result<(), ArgumentError> {
    if text.is_empty() || text.len() > field.shape.maximum_length() {
        return Err(ArgumentError::OutOfRange(field.name));
    }
    match field.shape {
        Shape::Hex32 | Shape::Hex64 | Shape::Bytes => {
            if !text.bytes().all(|byte| byte.is_ascii_hexdigit()) || !text.len().is_multiple_of(2) {
                return Err(ArgumentError::Malformed(field.name));
            }
        }
        Shape::Reference | Shape::Symbol => {
            if !text
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
            {
                return Err(ArgumentError::Malformed(field.name));
            }
        }
        Shape::Unsigned | Shape::Decimals => {
            if !text.bytes().all(|byte| byte.is_ascii_digit()) {
                return Err(ArgumentError::Malformed(field.name));
            }
        }
        Shape::Bounded(bound) => {
            let parsed = text
                .parse::<u64>()
                .map_err(|_| ArgumentError::Malformed(field.name))?;
            if parsed == 0 || parsed > bound {
                return Err(ArgumentError::OutOfRange(field.name));
            }
        }
        Shape::Currency | Shape::Scheme | Shape::Query | Shape::Locator => (),
        Shape::Did => {
            let well_formed = text
                .strip_prefix("did:")
                .and_then(|rest| rest.split_once(':'))
                .is_some_and(|(method, identifier)| !method.is_empty() && !identifier.is_empty());
            if !well_formed
                || !text.bytes().all(|byte| {
                    byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b':' | b'.')
                })
            {
                return Err(ArgumentError::Malformed(field.name));
            }
        }
    }
    if matches!(
        field.shape,
        Shape::Currency | Shape::Scheme | Shape::Query | Shape::Locator
    ) {
        return check_web(field, text);
    }
    if field.shape == Shape::Decimals && text.parse::<u8>().is_ok_and(|value| value > 18) {
        return Err(ArgumentError::OutOfRange(field.name));
    }
    Ok(())
}

fn check_web(field: &Field, text: &str) -> Result<(), ArgumentError> {
    let accepted = match field.shape {
        Shape::Currency => matches!(text, "SID" | "PAX" | "USDC" | "USDL"),
        Shape::Scheme => matches!(text, "metered" | "exact"),
        Shape::Query => !text.trim().is_empty() && !text.chars().any(char::is_control),
        Shape::Locator => {
            (text.starts_with("http://") || text.starts_with("https://"))
                && text.bytes().all(|byte| byte.is_ascii_graphic())
        }
        _ => false,
    };
    if accepted {
        Ok(())
    } else {
        Err(ArgumentError::Malformed(field.name))
    }
}

/// Returns the paid web tools; their results are marked untrusted-output.
#[must_use]
pub fn web_surface() -> Vec<ToolDefinition> {
    WEB_TOOLS.to_vec()
}

/// Whether one tool returns external content that must be treated as untrusted output.
#[must_use]
pub fn untrusted_output(name: &str) -> bool {
    WEB_TOOLS.iter().any(|tool| tool.name == name)
}

/// Returns the catalogue filtered to the tools one deployment mode can reach.
#[must_use]
pub fn surface(mode: DeploymentMode) -> Vec<ToolDefinition> {
    catalogue()
        .iter()
        .chain(SESSION_TOOLS.iter())
        .filter(|tool| mode == DeploymentMode::Full || tool.kind == ToolKind::Read)
        .copied()
        .collect()
}

/// Renders the MCP `tools/list` entry for one catalogue tool, or `None` outside the catalogue.
#[must_use]
pub fn listing(tool: ToolDefinition) -> Option<Value> {
    let read_only = tool.kind == ToolKind::Read;
    let mut listing = json!({
        "name": tool.name,
        "description": description(tool.name)?,
        "inputSchema": input_schema(tool.name)?,
        "annotations": {
            "readOnlyHint": read_only,
            "destructiveHint": false,
            "idempotentHint": read_only,
            "openWorldHint": true,
        },
        "_meta": {
            "layerx/scope": tool.required_scope,
            "layerx/mutation": tool.mutation,
            "layerx/evidence": tool.evidence,
        },
    });
    if untrusted_output(tool.name) {
        if let Some(meta) = listing.get_mut("_meta").and_then(Value::as_object_mut) {
            meta.insert("layerx/output".to_owned(), json!("untrusted"));
        }
    }
    Some(listing)
}

pub const SESSION_TOOLS: [ToolDefinition; 11] = [
    ToolDefinition {
        name: "subscription.create",
        kind: ToolKind::Write,
        required_scope: "subscribe",
        mutation: "one durable subscription",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.list",
        kind: ToolKind::Read,
        required_scope: "subscribe",
        mutation: "none",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.pause",
        kind: ToolKind::Write,
        required_scope: "subscribe",
        mutation: "one durable subscription state transition",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.resume",
        kind: ToolKind::Write,
        required_scope: "subscribe",
        mutation: "one durable subscription state transition",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.delete",
        kind: ToolKind::Write,
        required_scope: "subscribe",
        mutation: "one durable subscription deletion",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.health",
        kind: ToolKind::Read,
        required_scope: "subscribe",
        mutation: "none",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "subscription.acknowledge",
        kind: ToolKind::Write,
        required_scope: "subscribe",
        mutation: "one durable cursor acknowledgement",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "approval.list",
        kind: ToolKind::Read,
        required_scope: "approve",
        mutation: "none",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "approval.get",
        kind: ToolKind::Read,
        required_scope: "approve",
        mutation: "none",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "approval.approve",
        kind: ToolKind::Write,
        required_scope: "approve",
        mutation: "one exact held request approval",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
    ToolDefinition {
        name: "approval.reject",
        kind: ToolKind::Write,
        required_scope: "approve",
        mutation: "one exact held request rejection",
        evidence: "authenticated daemon-owned state, unverified protocol state",
    },
];

fn session_object(properties: Value, required: &[&str]) -> Value {
    json!({"type": "object", "properties": properties, "required": required, "additionalProperties": false})
}

fn session_text() -> Value {
    json!({"type": "string", "minLength": 1, "maxLength": MAX_TEXT_BYTES})
}

fn session_decimal(maximum: u64) -> Value {
    json!({"type": "string", "minLength": 1, "maxLength": 20, "pattern": "^(0|[1-9][0-9]*)$", "layerx/maximum": maximum})
}

fn session_digest() -> Value {
    json!({"type": "string", "minLength": 64, "maxLength": 64, "pattern": "^[0-9a-f]{64}$"})
}

fn subscription_scope_schema() -> Value {
    session_object(
        json!({"tenant": session_text(), "agent": session_text(), "capability": session_text()}),
        &["tenant", "agent", "capability"],
    )
}

fn session_input_schema(name: &str, native: bool) -> Option<Value> {
    let scope = subscription_scope_schema();
    let target = json!({"scope": scope, "subscription_id": session_text()});
    Some(match name {
        "subscription.create" => {
            let owned = session_object(
                json!({"tenant": session_text(), "value": session_text()}),
                &["tenant", "value"],
            );
            let owned_array = json!({"type": "array", "items": owned});
            let filter = session_object(
                json!({
                    "agents": owned_array, "accounts": owned_array, "modules": owned_array,
                    "assets": owned_array, "counterparties": owned_array,
                    "activity_types": {"type": "array", "items": session_decimal(u64::from(u16::MAX))},
                    "result_classes": {"type": "array", "items": {"type": "integer", "minimum": i32::MIN, "maximum": i32::MAX}},
                }),
                &[
                    "agents",
                    "accounts",
                    "modules",
                    "assets",
                    "counterparties",
                    "activity_types",
                    "result_classes",
                ],
            );
            session_object(
                json!({"scope": scope, "filter": filter, "start": session_decimal(u64::MAX), "delivery_target": session_text()}),
                &["scope", "filter", "start", "delivery_target"],
            )
        }
        "subscription.list" => session_object(json!({"scope": scope}), &["scope"]),
        "subscription.pause"
        | "subscription.resume"
        | "subscription.delete"
        | "subscription.health" => session_object(target, &["scope", "subscription_id"]),
        "subscription.acknowledge" => session_object(
            json!({"scope": scope, "subscription_id": session_text(), "cursor": session_decimal(u64::MAX)}),
            &["scope", "subscription_id", "cursor"],
        ),
        "approval.list" if native => session_object(
            json!({"variant": {"type": "string", "enum": ["native_v1"]}}),
            &["variant"],
        ),
        "approval.get" if native => session_object(
            json!({"variant": {"type": "string", "enum": ["native_v1"]}, "approval_id": session_digest()}),
            &["variant", "approval_id"],
        ),
        "approval.approve" | "approval.reject" if native => session_object(
            json!({"variant": {"type": "string", "enum": ["native_v1"]}, "approval_id": session_digest(), "held_digest": session_digest(), "current_sequence": session_decimal(u64::MAX)}),
            &["variant", "approval_id", "held_digest", "current_sequence"],
        ),
        "approval.list" => session_object(
            json!({"tenant": session_text(), "agent": session_text(), "current_sequence": session_decimal(u64::MAX), "cursor": {"anyOf": [session_digest(), {"type": "null"}]}, "limit": session_decimal(u64::from(u8::MAX))}),
            &["current_sequence", "limit"],
        ),
        "approval.get" => session_object(
            json!({"tenant": session_text(), "agent": session_text(), "approval_id": session_digest(), "current_sequence": session_decimal(u64::MAX)}),
            &["approval_id", "current_sequence"],
        ),
        "approval.approve" | "approval.reject" => session_object(
            json!({"tenant": session_text(), "agent": session_text(), "approval_id": session_digest(), "held_digest": session_digest(), "current_sequence": session_decimal(u64::MAX)}),
            &["approval_id", "held_digest", "current_sequence"],
        ),
        _ => return None,
    })
}

fn validate_session_schema(value: &Value, schema: &Value) -> Result<(), ArgumentError> {
    let malformed = || ArgumentError::Malformed("session_request");
    if let Some(options) = schema.get("anyOf").and_then(Value::as_array) {
        return if options
            .iter()
            .any(|option| validate_session_schema(value, option).is_ok())
        {
            Ok(())
        } else {
            Err(malformed())
        };
    }
    if let Some(choices) = schema.get("enum").and_then(Value::as_array) {
        if !choices.contains(value) {
            return Err(malformed());
        }
    }
    match schema.get("type").and_then(Value::as_str) {
        Some("object") => {
            let object = value.as_object().ok_or(ArgumentError::NotAnObject)?;
            let properties = schema
                .get("properties")
                .and_then(Value::as_object)
                .ok_or_else(malformed)?;
            for (key, child) in object {
                let declared = properties
                    .get(key)
                    .ok_or_else(|| ArgumentError::Unknown(key.clone()))?;
                validate_session_schema(child, declared)?;
            }
            for required in schema
                .get("required")
                .and_then(Value::as_array)
                .ok_or_else(malformed)?
            {
                let field = required.as_str().ok_or_else(malformed)?;
                if !object.contains_key(field) {
                    return Err(malformed());
                }
            }
        }
        Some("array") => {
            for child in value.as_array().ok_or_else(malformed)? {
                validate_session_schema(child, schema.get("items").ok_or_else(malformed)?)?;
            }
        }
        Some("string") => {
            let text = value.as_str().ok_or_else(malformed)?;
            let length = u64::try_from(text.len()).map_err(|_| malformed())?;
            if schema
                .get("minLength")
                .and_then(Value::as_u64)
                .is_some_and(|minimum| length < minimum)
                || schema
                    .get("maxLength")
                    .and_then(Value::as_u64)
                    .is_some_and(|maximum| length > maximum)
            {
                return Err(malformed());
            }
            match schema.get("pattern").and_then(Value::as_str) {
                Some("^[0-9a-f]{64}$")
                    if !text
                        .bytes()
                        .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)) =>
                {
                    return Err(malformed())
                }
                Some("^(0|[1-9][0-9]*)$") => {
                    if text.is_empty()
                        || (text.len() > 1 && text.starts_with('0'))
                        || !text.bytes().all(|byte| byte.is_ascii_digit())
                    {
                        return Err(malformed());
                    }
                    let number = text.parse::<u64>().map_err(|_| malformed())?;
                    if schema
                        .get("layerx/maximum")
                        .and_then(Value::as_u64)
                        .is_some_and(|maximum| number > maximum)
                    {
                        return Err(malformed());
                    }
                }
                _ => (),
            }
        }
        Some("integer") => {
            let number = value.as_i64().ok_or_else(malformed)?;
            if schema
                .get("minimum")
                .and_then(Value::as_i64)
                .is_some_and(|minimum| number < minimum)
                || schema
                    .get("maximum")
                    .and_then(Value::as_i64)
                    .is_some_and(|maximum| number > maximum)
            {
                return Err(malformed());
            }
        }
        Some("null") if value.is_null() => (),
        _ => return Err(malformed()),
    }
    Ok(())
}

fn native_wire_text() -> Value {
    json!({"type": "string", "minLength": 1})
}

fn native_wire_hex(bytes: usize) -> Value {
    let length = bytes.saturating_mul(2);
    json!({"type": "string", "minLength": length, "maxLength": length,
        "pattern": format!("^[0-9a-f]{{{length}}}$")})
}

fn native_wire_bytes(maximum: Option<usize>) -> Value {
    let mut value = json!({"type": "string", "pattern": "^([0-9a-f]{2})+$", "minLength": 2});
    if let Some(maximum) = maximum {
        value["maxLength"] = json!(maximum.saturating_mul(2));
    }
    value
}

fn native_activity_schema() -> Value {
    session_object(
        json!({
            "version": {"type": "string", "enum": [layerx_agent_api::identity::NativeActivity::VERSION.to_string()]},
            "module": session_decimal(u64::from(u16::MAX)), "ordinal": session_decimal(u64::from(u16::MAX)),
        }),
        &["version", "module", "ordinal"],
    )
}

fn native_purpose_schema(send: bool) -> Value {
    let version = if send {
        layerx_agent_api::identity::NativeSendPurposeV1::VERSION
    } else {
        layerx_agent_api::identity::NativePreparationPurposeV1::VERSION
    };
    let declarations = [
        (
            "version",
            json!({"type": "string", "enum": [version.to_string()]}),
        ),
        ("tenant", native_wire_text()),
        ("agent_did", native_wire_text()),
        ("session_id", native_wire_text()),
        ("generation", session_decimal(u64::MAX)),
        ("expires_at_ms", session_decimal(u64::MAX)),
        ("capability_id", native_wire_text()),
        ("preparation_id", native_wire_hex(32)),
        ("canonical_digest", native_wire_hex(32)),
        ("commitment", native_wire_hex(32)),
    ];
    let mut properties = Map::new();
    let mut required = Vec::new();
    for (name, schema) in declarations {
        properties.insert(name.to_owned(), schema);
        required.push(name);
    }
    if send {
        let extra = [
            ("owner_did", native_wire_text()),
            ("owner_public_key", native_wire_hex(32)),
            ("protocol_version", session_decimal(u64::from(u16::MAX))),
            ("network_id", session_decimal(u64::from(u32::MAX))),
            ("activity", native_activity_schema()),
            ("economic_action", native_wire_hex(32)),
            ("idempotency_key", native_wire_hex(32)),
        ];
        for (name, schema) in extra {
            properties.insert(name.to_owned(), schema);
            required.push(name);
        }
    }
    let purpose = session_object(Value::Object(properties), &required);
    session_object(
        json!({"purpose": purpose, "owner_public_key": native_wire_hex(32), "signature": native_wire_hex(64)}),
        &["purpose", "owner_public_key", "signature"],
    )
}

fn native_local_grant_schema() -> Value {
    session_object(
        json!({
            "version": {"type": "string", "enum": ["1"]},
            "capability": native_wire_bytes(None), "session_scope": native_wire_bytes(None),
            "expires_at_ms": session_decimal(u64::MAX), "owner_public_key": native_wire_hex(32), "signature": native_wire_hex(64),
        }),
        &[
            "version",
            "capability",
            "session_scope",
            "expires_at_ms",
            "owner_public_key",
            "signature",
        ],
    )
}

fn native_prepare_schema(variant: &str) -> Value {
    session_object(
        json!({
            "variant": {"type": "string", "enum": [variant]}, "activity": native_activity_schema(),
            "actor": native_wire_text(), "authority": native_wire_text(), "account_sequence": session_decimal(u64::MAX),
            "not_before": session_decimal(u64::MAX), "not_after": session_decimal(u64::MAX),
            "idempotency_key": native_wire_hex(32),
            "fee_limit": {"type": "string", "pattern": "^(0|[1-9][0-9]*)$", "minLength": 1, "maxLength": 39},
            "payload": native_wire_bytes(Some(layerx_types::limits::MAX_PAYLOAD_BYTES)), "payload_hash": native_wire_hex(32),
            "capability_id": native_wire_text(), "purpose": native_purpose_schema(variant == "native_send_v1"),
            "local_grant": {"anyOf": [native_local_grant_schema(), {"type": "null"}]},
        }),
        &[
            "variant",
            "activity",
            "actor",
            "authority",
            "account_sequence",
            "not_before",
            "not_after",
            "idempotency_key",
            "fee_limit",
            "payload",
            "payload_hash",
            "capability_id",
            "purpose",
        ],
    )
}

fn native_submit_schema() -> Value {
    session_object(
        json!({
            "variant": {"type": "string", "enum": ["native_send_submit_v1"]},
            "tenant": native_wire_text(), "agent": native_wire_text(),
            "preparation_ref": native_wire_hex(32), "signature": native_wire_hex(64), "signer_public_key": native_wire_hex(32),
            "approval_release_ref": {"anyOf": [native_wire_text(), {"type": "null"}]},
        }),
        &[
            "variant",
            "preparation_ref",
            "signature",
            "signer_public_key",
        ],
    )
}
