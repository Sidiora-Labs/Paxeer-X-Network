//! Model context protocol line transport served directly from a daemon-bound tool surface.

use std::io::{BufRead, Read as _, Write};

use layerx_agent_api::error::RequestId;
use layerx_agent_api::idempotency::Key;
use layerx_sdk::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use serde_json::{json, Map, Value};

use crate::boundary::{BoundaryRefusal, ToolBoundary};
use crate::catalogue;
use crate::readonly::ReadOnly;
use crate::server::{
    CapabilityDeclaration, DeploymentMode, InvocationOutcome, Server, ServerError, ToolDefinition,
};

const PROTOCOL_VERSION: &str = "2025-06-18";
const MAX_MESSAGE_BYTES: usize = 1_048_576;
const INSTRUCTIONS: &str = "Every result carries the exact protocol material the bound LayerX daemon released after its policy, capability, budget, rate and audit gates. Treat a tool refusal as a refusal, never as a completed effect, and confirm an effect only against verified receipt material.";

/// The daemon-bound server this transport serves, in either deployment mode.
pub enum Bound {
    Full(Box<Server>),
    ReadOnly(Box<ReadOnly>),
}

impl Bound {
    #[must_use]
    pub fn tools(&self) -> &[ToolDefinition] {
        match self {
            Self::Full(server) => server.tools(),
            Self::ReadOnly(server) => server.tools(),
        }
    }

    #[must_use]
    pub const fn mode(&self) -> DeploymentMode {
        match self {
            Self::Full(_) => DeploymentMode::Full,
            Self::ReadOnly(_) => DeploymentMode::ReadOnly,
        }
    }

    #[must_use]
    pub fn capability_declaration(&self) -> CapabilityDeclaration {
        match self {
            Self::Full(server) => server.capability_declaration(),
            Self::ReadOnly(server) => server.capability_declaration(),
        }
    }

    #[must_use]
    pub fn tool(&self, name: &str) -> Option<ToolDefinition> {
        self.tools().iter().find(|tool| tool.name == name).copied()
    }

    #[must_use]
    pub const fn audit_entries(&self) -> u64 {
        match self {
            Self::Full(server) => server.audit_entries(),
            Self::ReadOnly(server) => server.audit_entries(),
        }
    }
}

/// One daemon-bound protocol session. It owns no key material and holds no gateway credential.
pub struct Session<B: ToolBoundary> {
    bound: Bound,
    boundary: B,
}

impl<B: ToolBoundary> Session<B> {
    #[must_use]
    pub const fn new(bound: Bound, boundary: B) -> Self {
        Self { bound, boundary }
    }

    #[must_use]
    pub const fn bound(&self) -> &Bound {
        &self.bound
    }

    /// Serves protocol messages until the reader ends.
    ///
    /// # Errors
    ///
    /// Returns a transport failure or an oversized protocol message; a refused tool is a
    /// protocol result, never a transport error.
    pub fn serve<R: BufRead, W: Write>(
        &mut self,
        reader: &mut R,
        writer: &mut W,
    ) -> Result<(), String> {
        let limit = u64::try_from(MAX_MESSAGE_BYTES).unwrap_or(u64::MAX);
        let mut line = String::new();
        loop {
            line.clear();
            let read = reader
                .by_ref()
                .take(limit)
                .read_line(&mut line)
                .map_err(|error| format!("could not read a protocol message: {error}"))?;
            if read == 0 {
                return Ok(());
            }
            if read >= MAX_MESSAGE_BYTES && !line.ends_with('\n') {
                return Err("a protocol message exceeded the transport limit".into());
            }
            let message = line.trim();
            if message.is_empty() {
                continue;
            }
            let Some(response) = self.handle(message) else {
                continue;
            };
            let encoded = serde_json::to_string(&response)
                .map_err(|error| format!("could not encode a protocol message: {error}"))?;
            writeln!(writer, "{encoded}")
                .and_then(|()| writer.flush())
                .map_err(|error| format!("could not write a protocol message: {error}"))?;
        }
    }

    /// Answers one protocol message, or nothing for a notification.
    #[must_use]
    pub fn handle(&mut self, message: &str) -> Option<Value> {
        let Ok(request) = serde_json::from_str::<Value>(message) else {
            return Some(failure(
                Value::Null,
                -32700,
                "the message is not valid JSON",
            ));
        };
        let identifier = request.get("id").cloned().unwrap_or(Value::Null);
        let Some(method) = request.get("method").and_then(Value::as_str) else {
            return Some(failure(
                identifier,
                -32600,
                "the message did not name a method",
            ));
        };
        if identifier.is_null() {
            return None;
        }
        let parameters = request.get("params").cloned().unwrap_or(Value::Null);
        Some(match method {
            "initialize" => success(identifier, self.initialize()),
            "ping" => success(identifier, json!({})),
            "tools/list" => success(identifier, json!({"tools": self.listing()})),
            "tools/call" => self.call(identifier, &parameters),
            _ => failure(
                identifier,
                -32601,
                &format!("method {method} is not implemented"),
            ),
        })
    }

    fn initialize(&self) -> Value {
        let loaded_binding = match &self.bound {
            Bound::Full(server) => server.binding().transport_binding(self.bound.mode()),
            Bound::ReadOnly(server) => server.binding().transport_binding(self.bound.mode()),
        };
        let declaration = self.bound.capability_declaration();
        json!({
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {"tools": {"listChanged": false}},
            "serverInfo": {
                "name": "layerx",
                "title": "LayerX",
                "version": env!("CARGO_PKG_VERSION"),
            },
            "instructions": INSTRUCTIONS,
            "_meta": {
                "layerx/deployment_mode": mode_name(self.bound.mode()),
                "layerx/binding": "agent-daemon",
                "layerx/loaded_binding_v1": loaded_binding,
                "layerx/read_tools": declaration.read_tools,
                "layerx/write_tools": declaration.write_tools,
                "layerx/mutations_reachable": declaration.mutations_reachable,
            },
        })
    }

    fn listing(&self) -> Vec<Value> {
        self.bound
            .tools()
            .iter()
            .filter_map(|tool| catalogue::listing(*tool))
            .collect()
    }

    fn call(&mut self, identifier: Value, parameters: &Value) -> Value {
        let Some(name) = parameters.get("name").and_then(Value::as_str) else {
            return failure(identifier, -32602, "the call did not name a tool");
        };
        let Some(tool) = self.bound.tool(name) else {
            return failure(
                identifier,
                -32602,
                &format!("tool {name} is not served by this deployment"),
            );
        };
        let arguments = parameters.get("arguments").cloned().unwrap_or(Value::Null);
        if let Err(error) = catalogue::validate(tool.name, &arguments) {
            return success(
                identifier,
                content(
                    &json!({"refusal": error.detail(), "tool": tool.name, "stage": "arguments"}),
                    true,
                ),
            );
        }
        match self.invoke(tool, &arguments) {
            Ok(value) => success(
                identifier,
                content(&json!({"tool": tool.name, "result": value}), false),
            ),
            Err(refusal) => success(identifier, content(&refusal, true)),
        }
    }

    fn invoke(&mut self, tool: ToolDefinition, arguments: &Value) -> Result<Value, Value> {
        let core_sequence = self.boundary.observed_sequence().map_err(
            |refusal| json!({"refusal": refusal.detail(), "tool": tool.name, "stage": "freshness"}),
        )?;
        let encoded = serde_json::to_vec(arguments).map_err(
            |error| json!({"refusal": error.to_string(), "tool": tool.name, "stage": "arguments"}),
        )?;
        let Self { bound, boundary } = self;
        let executor = |_: &crate::server::DaemonInvocation| {
            let outcome = boundary.execute(tool, arguments);
            let state = match &outcome {
                Ok(_) => InvocationOutcome::Completed,
                Err(refusal) if refusal.unknown() => InvocationOutcome::Unknown,
                Err(_) => InvocationOutcome::Refused,
            };
            (outcome, state)
        };
        let routed = match bound {
            Bound::Full(server) => match tool.kind {
                crate::server::ToolKind::Read => {
                    server.execute_read(core_sequence, tool.name, encoded, executor)
                }
                crate::server::ToolKind::Write => {
                    server.execute_committed(core_sequence, tool.name, encoded, executor)
                }
            },
            Bound::ReadOnly(server) => {
                server.execute_authorized(core_sequence, tool.name, encoded, executor)
            }
        };
        match routed {
            Ok(Ok(value)) => Ok(value),
            Ok(Err(refusal)) => Err(refusal_value(tool, &refusal)),
            Err(error) => Err(json!({
                "refusal": authority_detail(&error),
                "tool": tool.name,
                "stage": "authority",
            })),
        }
    }
}

fn refusal_value(tool: ToolDefinition, refusal: &BoundaryRefusal) -> Value {
    json!({
        "refusal": refusal.detail(),
        "tool": tool.name,
        "stage": "daemon",
        "state": if refusal.unknown() { "unknown" } else { "refused" },
    })
}

fn authority_detail(error: &ServerError) -> String {
    match error {
        ServerError::MissingSession => "the bound daemon session is absent",
        ServerError::MissingCapability => "the bound capability is absent",
        ServerError::ClosedSession => "the bound daemon session is closed",
        ServerError::RevokedSession => "the bound daemon session is revoked",
        ServerError::TenantMismatch => "the bound session and capability are cross-tenant",
        ServerError::CapabilityMismatch => "the bound capability does not authorize this session",
        ServerError::ExpiredAuthority => "the bound authority has expired",
        ServerError::NoScope => "the bound session carries no scope for this catalogue",
        ServerError::InvalidInvocation => "the invocation is outside the daemon's bounds",
        ServerError::ToolAbsent => "the daemon does not authorize this tool for this session",
        ServerError::WrongServer => "the invocation belongs to another server binding",
        ServerError::AuthorizationUnavailable => "the daemon authorization state is unavailable",
        ServerError::DurableInvocation(_) => "the durable daemon invocation was refused",
        ServerError::InvocationUnknown => "the durable daemon invocation outcome is unknown",
        ServerError::Arithmetic => "the daemon invocation counter is exhausted",
        ServerError::Capability(_) => "the bound capability record is unusable",
        ServerError::Audit(_) => "the daemon audit log could not record this invocation",
    }
    .to_owned()
}

/// Names one deployment mode exactly as the installed manifest declares it.
#[must_use]
pub const fn mode_name(mode: DeploymentMode) -> &'static str {
    match mode {
        DeploymentMode::Full => "full",
        DeploymentMode::ReadOnly => "read-only",
    }
}

fn content(value: &Value, refused: bool) -> Value {
    let rendered = serde_json::to_string_pretty(value)
        .unwrap_or_else(|_| "{\"refusal\":\"result encoding failed\"}".to_owned());
    json!({
        "content": [{"type": "text", "text": rendered}],
        "structuredContent": value,
        "isError": refused,
    })
}

fn success(identifier: Value, result: Value) -> Value {
    let mut envelope = Map::new();
    envelope.insert("jsonrpc".to_owned(), Value::String("2.0".to_owned()));
    envelope.insert("id".to_owned(), identifier);
    envelope.insert("result".to_owned(), result);
    Value::Object(envelope)
}

fn failure(identifier: Value, code: i32, detail: &str) -> Value {
    let mut error = Map::new();
    error.insert("code".to_owned(), Value::from(code));
    error.insert("message".to_owned(), Value::String(detail.to_owned()));
    let mut envelope = Map::new();
    envelope.insert("jsonrpc".to_owned(), Value::String("2.0".to_owned()));
    envelope.insert("id".to_owned(), identifier);
    envelope.insert("error".to_owned(), Value::Object(error));
    Value::Object(envelope)
}

pub struct DaemonClientSession {
    transport: AgentEnvelopeTransport,
    credential: EnvelopeCredential,
    mode: DeploymentMode,
    next_request_id: u64,
    loaded_binding: String,
    tools: Vec<ToolDefinition>,
    listings: Vec<Value>,
}

impl DaemonClientSession {
    pub fn connect(
        transport: AgentEnvelopeTransport,
        credential: EnvelopeCredential,
        mode: DeploymentMode,
    ) -> Result<Self, Value> {
        let mut session = Self {
            transport,
            credential,
            mode,
            next_request_id: 1,
            loaded_binding: String::new(),
            tools: Vec::new(),
            listings: Vec::new(),
        };
        session.refresh_description()?;
        Ok(session)
    }

    fn request_id(&mut self) -> Result<RequestId, Value> {
        let id = self.next_request_id;
        self.next_request_id = id.checked_add(1).ok_or_else(
            || json!({"reason": "mcp.request_counter_exhausted", "state": "refused"}),
        )?;
        Ok(RequestId(id))
    }

    fn invoke(&mut self, tool: &str, arguments: &Value, key: Key) -> Result<Value, Value> {
        let id = self.request_id()?;
        self.transport
            .mcp_invoke(id, &self.credential, tool, arguments, key)
            .map(|response| response.value)
            .map_err(daemon_client_refusal)
    }

    fn refresh_description(&mut self) -> Result<(), Value> {
        let key = request_key()?;
        let value = self.invoke("mcp.describe", &json!({}), key)?;
        let fields = value.as_object().ok_or_else(description_refusal)?;
        if fields.len() != 3
            || fields.get("mode").and_then(Value::as_str) != Some(mode_name(self.mode))
        {
            return Err(description_refusal());
        }
        let binding = fields
            .get("loaded_binding_v1")
            .and_then(Value::as_str)
            .filter(|value| {
                value.len() == 64
                    && value
                        .bytes()
                        .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
            })
            .ok_or_else(description_refusal)?;
        let listings = fields
            .get("tools")
            .and_then(Value::as_array)
            .ok_or_else(description_refusal)?;
        let definitions = catalogue::surface(self.mode)
            .into_iter()
            .chain(catalogue::web_surface())
            .filter(|tool| {
                self.mode == DeploymentMode::Full || tool.kind == crate::server::ToolKind::Read
            })
            .collect::<Vec<_>>();
        let mut tools = Vec::new();
        for listing in listings {
            let name = listing
                .get("name")
                .and_then(Value::as_str)
                .ok_or_else(description_refusal)?;
            let tool = definitions
                .iter()
                .find(|tool| tool.name == name)
                .copied()
                .ok_or_else(description_refusal)?;
            if tools
                .iter()
                .any(|prior: &ToolDefinition| prior.name == name)
                || catalogue::listing(tool).as_ref() != Some(listing)
            {
                return Err(description_refusal());
            }
            tools.push(tool);
        }
        if tools.is_empty() {
            return Err(description_refusal());
        }
        self.loaded_binding = binding.to_owned();
        self.tools = tools;
        self.listings = listings.clone();
        Ok(())
    }

    pub fn handle(&mut self, message: &str) -> Option<Value> {
        let Ok(request) = serde_json::from_str::<Value>(message) else {
            return Some(failure(
                Value::Null,
                -32700,
                "the message is not valid JSON",
            ));
        };
        let identifier = request.get("id").cloned().unwrap_or(Value::Null);
        let Some(method) = request.get("method").and_then(Value::as_str) else {
            return Some(failure(
                identifier,
                -32600,
                "the message did not name a method",
            ));
        };
        if identifier.is_null() {
            return None;
        }
        let parameters = request.get("params").cloned().unwrap_or(Value::Null);
        Some(match method {
            "ping" => success(identifier, json!({})),
            "initialize" | "tools/list" => match self.refresh_description() {
                Err(refusal) => daemon_protocol_failure(identifier, refusal),
                Ok(()) if method == "tools/list" => {
                    success(identifier, json!({"tools": self.listings}))
                }
                Ok(()) => {
                    let read_tools = self
                        .tools
                        .iter()
                        .filter(|tool| tool.kind == crate::server::ToolKind::Read)
                        .count();
                    let write_tools = self.tools.len().saturating_sub(read_tools);
                    success(
                        identifier,
                        json!({
                            "protocolVersion": PROTOCOL_VERSION,
                            "capabilities": {"tools": {"listChanged": false}},
                            "serverInfo": {"name": "layerx", "title": "LayerX", "version": env!("CARGO_PKG_VERSION")},
                            "instructions": INSTRUCTIONS,
                            "_meta": {
                                "layerx/deployment_mode": mode_name(self.mode),
                                "layerx/binding": "agent-daemon",
                                "layerx/loaded_binding_v1": self.loaded_binding,
                                "layerx/read_tools": read_tools,
                                "layerx/write_tools": write_tools,
                                "layerx/mutations_reachable": write_tools != 0,
                            },
                        }),
                    )
                }
            },
            "tools/call" => self.call(identifier, &parameters),
            _ => failure(identifier, -32601, "the method is not implemented"),
        })
    }

    fn call(&mut self, identifier: Value, parameters: &Value) -> Value {
        let Some(name) = parameters.get("name").and_then(Value::as_str) else {
            return failure(identifier, -32602, "the call did not name a tool");
        };
        let Some(tool) = self.tools.iter().find(|tool| tool.name == name).copied() else {
            return failure(
                identifier,
                -32602,
                "the tool is not served by this deployment",
            );
        };
        let arguments = parameters.get("arguments").cloned().unwrap_or(Value::Null);
        if let Err(error) = catalogue::validate(tool.name, &arguments) {
            return success(
                identifier,
                content(
                    &json!({
                        "refusal": error.detail(), "tool": tool.name, "stage": "arguments", "state": "refused",
                    }),
                    true,
                ),
            );
        }
        let explicit_key = arguments.get("idempotency_key").is_some()
            || parameters
                .get("_meta")
                .and_then(|meta| meta.get("layerx/idempotency_key"))
                .is_some();
        let key = if tool.kind == crate::server::ToolKind::Read && !explicit_key {
            request_key()
        } else {
            invocation_key(parameters, &arguments)
        };
        let result = key.and_then(|key| self.invoke(tool.name, &arguments, key));
        match result {
            Ok(value) => {
                let refused = value
                    .get("state")
                    .and_then(Value::as_str)
                    .is_some_and(|state| matches!(state, "refused" | "unknown"));
                success(
                    identifier,
                    content(&json!({"tool": tool.name, "result": value}), refused),
                )
            }
            Err(refusal) => success(
                identifier,
                content(
                    &json!({
                        "tool": tool.name, "stage": "daemon", "refusal": refusal,
                    }),
                    true,
                ),
            ),
        }
    }

    pub fn serve<R: BufRead, W: Write>(
        &mut self,
        reader: &mut R,
        writer: &mut W,
    ) -> Result<(), String> {
        let mut line = String::new();
        let limit = u64::try_from(MAX_MESSAGE_BYTES).unwrap_or(u64::MAX);
        loop {
            line.clear();
            let read = reader
                .by_ref()
                .take(limit)
                .read_line(&mut line)
                .map_err(|error| format!("could not read a protocol message: {error}"))?;
            if read == 0 {
                return Ok(());
            }
            if read >= MAX_MESSAGE_BYTES && !line.ends_with('\n') {
                return Err("a protocol message exceeded the transport limit".to_owned());
            }
            let message = line.trim();
            if message.is_empty() {
                continue;
            }
            if let Some(response) = self.handle(message) {
                serde_json::to_writer(&mut *writer, &response)
                    .map_err(|_| "could not encode a protocol message".to_owned())?;
                writeln!(writer)
                    .and_then(|()| writer.flush())
                    .map_err(|error| format!("could not write a protocol message: {error}"))?;
            }
        }
    }
}

fn description_refusal() -> Value {
    json!({"reason": "mcp.description_invalid", "state": "refused"})
}

fn request_key() -> Result<Key, Value> {
    let mut bytes = [0_u8; 32];
    std::fs::File::open("/dev/urandom")
        .and_then(|mut file| file.read_exact(&mut bytes))
        .map_err(|_| json!({"reason": "mcp.request_entropy_unavailable", "state": "refused"}))?;
    Key::new(bytes)
        .map_err(|_| json!({"reason": "mcp.request_entropy_unavailable", "state": "refused"}))
}

fn invocation_key(parameters: &Value, arguments: &Value) -> Result<Key, Value> {
    let argument_key = arguments.get("idempotency_key").or_else(|| {
        (arguments.get("variant").and_then(Value::as_str) == Some("native_write_v1"))
            .then(|| arguments.get("intent").and_then(|intent| intent.get("idempotency_key"))).flatten()
    }).and_then(Value::as_str);
    let metadata_key = parameters
        .get("_meta")
        .and_then(|meta| meta.get("layerx/idempotency_key"))
        .and_then(Value::as_str);
    if argument_key
        .zip(metadata_key)
        .is_some_and(|(left, right)| left != right)
    {
        return Err(json!({"reason": "mcp.idempotency_key_conflict", "state": "refused"}));
    }
    let encoded = argument_key
        .or(metadata_key)
        .filter(|key| {
            key.len() == 64
                && key
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        })
        .ok_or_else(|| json!({"reason": "mcp.idempotency_key_required", "state": "refused"}))?;
    let mut bytes = [0_u8; 32];
    for (index, byte) in bytes.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&encoded[index * 2..index * 2 + 2], 16)
            .map_err(|_| json!({"reason": "mcp.idempotency_key_required", "state": "refused"}))?;
    }
    Key::new(bytes)
        .map_err(|_| json!({"reason": "mcp.idempotency_key_required", "state": "refused"}))
}

fn daemon_client_refusal(error: EnvelopeError) -> Value {
    match error {
        EnvelopeError::Refused(error) => json!({
            "class": format!("{:?}", error.class),
            "protocol_result_code": error.protocol_result_code.map(|code| code.raw()),
            "retriability": format!("{:?}", error.retriability),
            "request_id": error.request_id.0.to_string(),
            "reason": error.reason.as_str(),
            "state": if error.reason.as_str() == "outcome.unknown" { "unknown" } else { "refused" },
        }),
        EnvelopeError::Unknown { .. } => {
            json!({"reason": "outcome.unknown", "state": "unknown", "retriability": "Never"})
        }
        EnvelopeError::GatewayAuthentication => {
            json!({"reason": "gateway.authentication", "state": "refused"})
        }
        EnvelopeError::Transport { .. } => {
            json!({"reason": "mcp.transport_unavailable", "state": "refused"})
        }
        _ => json!({"reason": "mcp.exchange_invalid", "state": "refused"}),
    }
}

fn daemon_protocol_failure(identifier: Value, refusal: Value) -> Value {
    let mut response = failure(
        identifier,
        -32001,
        "the live daemon refused the MCP authority",
    );
    if let Some(error) = response.get_mut("error").and_then(Value::as_object_mut) {
        error.insert("data".to_owned(), refusal);
    }
    response
}
