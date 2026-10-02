use super::{
    authenticate_key, http, json_response, response, Config, IncomingRequest, OutgoingResponse,
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::os::unix::fs::MetadataExt;
use std::sync::OnceLock;
use zeroize::Zeroizing;

const CATALOGUE: &str = include_str!("../../../../tools/paxeer-x/route-catalogue.json");
const MAX_BINDINGS_BYTES: u64 = 1024 * 1024;
const AGENT_RPC_PATH: &str = "/v1/agent/rpc";
const AGENT_RPC_SERVICE: &str = "agentd";
const AGENT_RPC_MAX_BODY: usize = 1_048_576;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct BindingFile {
    schema: String,
    chain_id: u64,
    network_id: String,
    wire_version: String,
    allowed_origins: Vec<String>,
    services: BTreeMap<String, Binding>,
    #[serde(default)]
    upstreams: BTreeMap<String, RouteBinding>,
    passthrough: Vec<Passthrough>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Binding {
    url: String,
    discovery_url: Option<String>,
    health_path: String,
    identity_path: String,
    health_authorization_file: Option<String>,
    ready_pointer: String,
    ready_value: Value,
    network_pointer: String,
    network_value: Value,
    version_pointer: String,
    version_value: Value,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RouteBinding {
    service: String,
    binding: Binding,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Passthrough {
    service: String,
    listen: std::net::SocketAddr,
    upstream: String,
}

pub(super) struct Registry {
    bindings: BTreeMap<String, Binding>,
    upstreams: BTreeMap<String, RouteBinding>,
    allowed_origins: BTreeSet<String>,
    passthrough: Vec<Passthrough>,
}

fn catalogue() -> &'static Value {
    static VALUE: OnceLock<Value> = OnceLock::new();
    VALUE.get_or_init(|| serde_json::from_str(CATALOGUE).expect("compiled route catalogue"))
}

fn service(id: &str) -> Option<&'static Value> {
    catalogue()["services"]
        .as_array()?
        .iter()
        .find(|s| s["id"] == id)
}

fn private(id: &str) -> bool {
    service(id).is_none_or(|s| s["exposure"] != "product")
}

fn safe_path(path: &str) -> bool {
    path.starts_with('/')
        && path.len() <= 2048
        && !path.contains(['?', '#', '\\', '%'])
        && path.split('/').all(|part| part != "." && part != "..")
        && path.bytes().all(|b| b.is_ascii_graphic())
}

fn protected(path: &str) -> Result<Vec<u8>, String> {
    let metadata = fs::symlink_metadata(path).map_err(|_| "route binding file unavailable")?;
    if !metadata.is_file()
        || metadata.nlink() != 1
        || metadata.mode() & 0o077 != 0
        || metadata.len() > MAX_BINDINGS_BYTES
    {
        return Err("route binding file is not protected".into());
    }
    fs::read(path).map_err(|_| "route binding file unreadable".into())
}

impl Registry {
    pub(super) fn configured(network: &str, wire: &str) -> Result<Self, String> {
        let Some(path) = std::env::var_os("LAYERX_GATEWAY_ROUTE_BINDINGS_FILE") else {
            return Ok(Self {
                bindings: BTreeMap::new(),
                upstreams: BTreeMap::new(),
                allowed_origins: BTreeSet::new(),
                passthrough: Vec::new(),
            });
        };
        let path = path.to_str().ok_or("route binding path invalid")?;
        let file: BindingFile =
            serde_json::from_slice(&protected(path)?).map_err(|_| "route bindings malformed")?;
        if file.schema != "paxeer-x.route-bindings.v1"
            || file.chain_id != 125
            || file.network_id != network
            || file.wire_version != wire
            || file.services.len() > 31
            || file.upstreams.len() > 64
        {
            return Err("route bindings network or version mismatch".into());
        }
        let mut allowed_origins = BTreeSet::new();
        for origin in file.allowed_origins {
            let endpoint = http::Endpoint::parse(&origin)?;
            if !endpoint.base_path.is_empty()
                || origin.contains('*')
                || !allowed_origins.insert(origin)
            {
                return Err("route CORS origin invalid or duplicate".into());
            }
        }
        for (name, upstream) in &file.upstreams {
            if name.is_empty()
                || name.len() > 64
                || !name.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
                || private(&upstream.service)
                || !catalogue()["routes"].as_array().is_some_and(|routes| {
                    routes.iter().any(|route| {
                        route["proxy"] == true
                            && route["service"] == upstream.service
                            && route["upstream"] == *name
                    })
                })
            {
                return Err("unknown route upstream or service binding".into());
            }
        }
        for (id, binding) in file.services.iter().chain(
            file.upstreams
                .values()
                .map(|value| (&value.service, &value.binding)),
        ) {
            if service(id).is_none() {
                return Err("unknown route service".into());
            }
            let endpoint = http::Endpoint::parse(&binding.url)?;
            if !endpoint.base_path.is_empty()
                || (!safe_path(&binding.health_path) || !safe_path(&binding.identity_path))
            {
                return Err("route binding endpoint or health path invalid".into());
            }
            if let Some(url) = &binding.discovery_url {
                http::Endpoint::parse(url)?;
                if private(id) {
                    return Err("private service cannot advertise an endpoint".into());
                }
            }
            if ![
                &binding.ready_pointer,
                &binding.network_pointer,
                &binding.version_pointer,
            ]
            .iter()
            .all(|p| p.starts_with('/') && p.len() <= 128)
                || binding.ready_value.is_null()
                || ![json!(125), json!(network)].contains(&binding.network_value)
                || !binding
                    .version_value
                    .as_str()
                    .is_some_and(|v| !v.is_empty() && v.len() <= 64)
            {
                return Err("route health identity predicate invalid".into());
            }
            if id == AGENT_RPC_SERVICE {
                if binding.network_value != json!(network) {
                    return Err(format!(
                        "route binding {id} network_value must equal the gateway network {network}"
                    ));
                }
                if binding.version_value != json!(wire) {
                    return Err(format!(
                        "route binding {id} version_value differs from the gateway wire version {wire}"
                    ));
                }
            }
            if let Some(path) = &binding.health_authorization_file {
                let _ = protected(path)?;
            }
        }
        let mut listeners = BTreeSet::new();
        for tunnel in &file.passthrough {
            if !matches!(tunnel.service.as_str(), "program-registry" | "agentd")
                || !listeners.insert(tunnel.listen)
                || tunnel.listen.port() == 0
                || !file.services.contains_key(&tunnel.service)
            {
                return Err("TLS passthrough service or listener invalid".into());
            }
            let target = http::Endpoint::parse(&tunnel.upstream)?;
            let binding = file
                .services
                .get(&tunnel.service)
                .ok_or("TLS service absent")?;
            let bound = http::Endpoint::parse(&binding.url)?;
            if target.host != bound.host || target.port != bound.port {
                return Err("TLS passthrough differs from authenticated service identity".into());
            }
            if !target.base_path.is_empty() {
                return Err("TLS passthrough cannot rewrite paths".into());
            }
        }
        Ok(Self {
            bindings: file.services,
            upstreams: file.upstreams,
            allowed_origins,
            passthrough: file.passthrough,
        })
    }

    pub(super) fn start_passthrough(&self, primary: std::net::SocketAddr) -> Result<(), String> {
        use std::net::{TcpListener, TcpStream, ToSocketAddrs};
        use std::sync::atomic::{AtomicUsize, Ordering};
        use std::sync::Arc;
        use std::time::Duration;
        let mut listeners = Vec::new();
        for tunnel in &self.passthrough {
            if tunnel.listen == primary {
                return Err("TLS passthrough conflicts with HTTP listener".into());
            }
            let listener =
                TcpListener::bind(tunnel.listen).map_err(|_| "TLS passthrough bind failed")?;
            let endpoint = http::Endpoint::parse(&tunnel.upstream)?;
            listeners.push((listener, endpoint));
        }
        for (listener, endpoint) in listeners {
            let active = Arc::new(AtomicUsize::new(0));
            std::thread::spawn(move || {
                for incoming in listener.incoming() {
                    let Ok(client) = incoming else { break };
                    if active
                        .fetch_update(Ordering::AcqRel, Ordering::Acquire, |n| {
                            (n < 32).then_some(n + 1)
                        })
                        .is_err()
                    {
                        continue;
                    }
                    let active = Arc::clone(&active);
                    let endpoint = endpoint.clone();
                    std::thread::spawn(move || {
                        let run = || -> Result<(), std::io::Error> {
                            let address = (endpoint.host.as_str(), endpoint.port)
                                .to_socket_addrs()?
                                .next()
                                .ok_or_else(|| std::io::Error::other("TLS upstream unresolved"))?;
                            let upstream =
                                TcpStream::connect_timeout(&address, Duration::from_secs(3))?;
                            for socket in [&client, &upstream] {
                                socket.set_read_timeout(Some(Duration::from_secs(8)))?;
                                socket.set_write_timeout(Some(Duration::from_secs(8)))?;
                            }
                            let client_read = client.try_clone()?;
                            let upstream_write = upstream.try_clone()?;
                            let outbound = std::thread::spawn(move || {
                                tunnel_bytes(client_read, upstream_write)
                            });
                            let _ = tunnel_bytes(upstream, client);
                            let _ = outbound.join();
                            Ok(())
                        };
                        let _ = run();
                        active.fetch_sub(1, Ordering::AcqRel);
                    });
                }
            });
        }
        Ok(())
    }

    pub(super) fn origin<'a>(&self, request: &'a IncomingRequest) -> Option<&'a str> {
        request
            .headers
            .get("origin")
            .filter(|origin| self.allowed_origins.contains(*origin))
            .map(String::as_str)
    }

    fn binding(&self, service: &str, upstream: &str) -> Option<&Binding> {
        if upstream == service {
            self.bindings.get(service)
        } else {
            self.upstreams
                .get(upstream)
                .filter(|value| value.service == service)
                .map(|value| &value.binding)
        }
    }

    fn health(&self, config: &Config, id: &str) -> Result<(), &'static str> {
        let upstreams: BTreeSet<_> = catalogue()["routes"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|route| route["service"] == id && route["proxy"] == true)
            .filter_map(|route| route["upstream"].as_str())
            .collect();
        if upstreams.is_empty() {
            return self.health_binding(config, self.bindings.get(id).ok_or("not_configured")?);
        }
        for upstream in upstreams {
            self.health_binding(config, self.binding(id, upstream).ok_or("not_configured")?)?;
        }
        Ok(())
    }

    fn health_binding(&self, config: &Config, binding: &Binding) -> Result<(), &'static str> {
        let endpoint = http::Endpoint::parse(&binding.url).map_err(|_| "invalid_upstream")?;
        let auth = match &binding.health_authorization_file {
            Some(path) => Zeroizing::new(
                String::from_utf8(protected(path).map_err(|_| "authority_unavailable")?)
                    .map_err(|_| "authority_unavailable")?
                    .trim()
                    .to_owned(),
            ),
            None => Zeroizing::new(String::new()),
        };
        let result = config
            .client
            .request_authorized(
                &endpoint,
                &auth,
                &http::OutboundRequest {
                    method: "GET",
                    path: &binding.health_path,
                    idempotency: None,
                    content_type: "application/json",
                    body: &[],
                },
            )
            .map_err(|_| "unreachable")?;
        if result.status != 200 || result.content_type != "application/json" {
            return Err("dependency_not_ready");
        }
        let body: Value =
            serde_json::from_slice(&result.body).map_err(|_| "invalid_health_response")?;
        let identity = if binding.identity_path == binding.health_path {
            body.clone()
        } else {
            let reply = config
                .client
                .request_authorized(
                    &endpoint,
                    &auth,
                    &http::OutboundRequest {
                        method: "GET",
                        path: &binding.identity_path,
                        idempotency: None,
                        content_type: "application/json",
                        body: &[],
                    },
                )
                .map_err(|_| "identity_unreachable")?;
            if reply.status != 200 || reply.content_type != "application/json" {
                return Err("identity_unavailable");
            }
            serde_json::from_slice::<Value>(&reply.body).map_err(|_| "invalid_identity_response")?
        };
        if identity.pointer(&binding.network_pointer) != Some(&binding.network_value) {
            return Err("wrong_network");
        }
        if identity.pointer(&binding.version_pointer) != Some(&binding.version_value) {
            return Err("wrong_version");
        }
        if body.pointer(&binding.ready_pointer) != Some(&binding.ready_value) {
            return Err("dependency_not_ready");
        }
        Ok(())
    }

    pub(super) fn readiness(&self, config: &Config) -> Value {
        let Some(services) = catalogue()["services"].as_array() else {
            return json!({"complete": false});
        };
        let states = std::thread::scope(|scope| {
            let handles: Vec<_> = services
                .iter()
                .filter_map(|s| s["id"].as_str())
                .map(|id| (id, scope.spawn(move || self.health(config, id))))
                .collect();
            handles
                .into_iter()
                .map(|(id, handle)| {
                    let result = handle.join().unwrap_or(Err("probe_failed"));
                    (
                        id.to_owned(),
                        json!({"ready": result.is_ok(), "reason": result.err().unwrap_or("ready")}),
                    )
                })
                .collect::<BTreeMap<_, _>>()
        });
        json!({"complete": states.values().all(|v| v["ready"] == true), "services": states})
    }
}

fn matches_path(template: &str, path: &str) -> bool {
    let path = path.split_once('?').map_or(path, |(path, _)| path);
    if !safe_path(path) {
        return false;
    }
    let parts: Vec<_> = path.split('/').collect();
    let expected: Vec<_> = template.split('/').collect();
    parts.len() == expected.len()
        && expected.iter().zip(parts).all(|(left, right)| {
            if left.starts_with(':') || (left.starts_with('{') && left.ends_with('}')) {
                !right.is_empty()
                    && right.len() <= 512
                    && right
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"-._:".contains(&b))
            } else {
                *left == right
            }
        })
}

pub(super) fn route(config: &Config, request: &IncomingRequest) -> Option<OutgoingResponse> {
    if request.method == "OPTIONS" {
        return Some(if config.routes.origin(request).is_some() {
            OutgoingResponse {
                content_type: "application/json".to_owned(),
                headers: Vec::new(),
                status: 204,
                body: Vec::new(),
                retry_after: None,
            }
        } else {
            response(403, "origin_not_allowed", None)
        });
    }
    if request.headers.contains_key("origin") && config.routes.origin(request).is_none() {
        return Some(response(403, "origin_not_allowed", None));
    }
    if request.path == "/v1/routes" || request.path.starts_with("/v1/routes/") {
        if request.method != "GET" {
            return Some(response(405, "method_not_allowed", None));
        }
        if let Err(refusal) = authenticate_key(config, request) {
            return Some(refusal);
        }
        if request.path == "/v1/routes" {
            let mut document = catalogue().clone();
            document["readiness"] = config.routes.readiness(config);
            return Some(json_response(200, &document));
        }
        let id = request.path.trim_start_matches("/v1/routes/");
        let Some(entry) = service(id) else {
            return Some(response(404, "not_found", None));
        };
        if private(id) {
            return Some(response(403, "private_service", None));
        }
        let mut document = entry.clone();
        let result = config.routes.health(config, id);
        document["ready"] = json!(result.is_ok());
        document["reason"] = json!(result.err().unwrap_or("ready"));
        document["routes"] = json!(catalogue()["routes"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|r| r["service"] == id)
            .collect::<Vec<_>>());
        if result.is_ok() {
            if let Some(url) = config
                .routes
                .bindings
                .get(id)
                .and_then(|b| b.discovery_url.as_ref())
            {
                document["endpoint"] = json!(url);
            }
        }
        return Some(json_response(
            if result.is_ok() { 200 } else { 503 },
            &document,
        ));
    }
    if request.path.split_once('?').map_or(request.path.as_str(), |(path, _)| path)
        == AGENT_RPC_PATH
    {
        return Some(agent_rpc(config, request));
    }
    let entry = catalogue()["routes"].as_array()?.iter().find(|r| {
        r["proxy"] == true
            && r["method"] == request.method
            && r["path"]
                .as_str()
                .is_some_and(|p| matches_path(p, &request.path))
    })?;
    let id = entry["service"].as_str()?;
    if private(id) {
        return Some(response(403, "private_service", None));
    }
    if entry["authentication"] == "gateway-api-key"
        && !request.headers.contains_key("authorization")
    {
        return Some(response(401, "authentication_required", None));
    }
    if id == "agentd" {
        if request.body.len() > 1024 * 1024 {
            return Some(response(413, "request_too_large", None));
        }
        if let Err(refusal) = authenticate_key(config, request) {
            return Some(refusal);
        }
    }
    let upstream = entry["upstream"].as_str().unwrap_or(id);
    let Some(binding) = config.routes.binding(id, upstream) else {
        return Some(json_response(
            503,
            &json!({"error":{"code":"route_unavailable","service":id,"reason":"not_configured"}}),
        ));
    };
    if let Err(reason) = config.routes.health_binding(config, binding) {
        return Some(json_response(
            503,
            &json!({"error":{"code":"route_unavailable","service":id,"reason":reason}}),
        ));
    }
    let endpoint = match http::Endpoint::parse(&binding.url) {
        Ok(endpoint) => endpoint,
        Err(_) => return Some(response(503, "invalid_upstream", None)),
    };
    let authorization = if matches!(
        id,
        "human" | "wallet-gateway" | "interop" | "ramp" | "webhooks-dashboard"
    ) {
        request
            .headers
            .get("authorization")
            .map_or("", String::as_str)
    } else {
        ""
    };
    let forwarded: Vec<_> = request
        .headers
        .iter()
        .filter(|(name, _)| forward_header(id, name))
        .map(|(name, value)| (name.as_str(), value.as_str()))
        .collect();
    let upstream_path = match routed_path(entry, &request.path) {
        Some(path) => path,
        None => return Some(response(400, "invalid_route_target", None)),
    };
    Some(
        match config.client.request_forwarded(
            &endpoint,
            authorization,
            &http::OutboundRequest {
                method: &request.method,
                path: &upstream_path,
                idempotency: request.headers.get("idempotency-key").map(String::as_str),
                content_type: request
                    .headers
                    .get("content-type")
                    .map_or("application/json", String::as_str),
                body: &request.body,
            },
            &forwarded,
        ) {
            Ok(reply) => OutgoingResponse {
                status: reply.status,
                content_type: reply.content_type,
                headers: reply
                    .headers
                    .into_iter()
                    .filter(|(name, _)| response_header(id, name))
                    .collect(),
                body: reply.body,
                retry_after: None,
            },
            Err(_) => response(503, "upstream_unavailable", None),
        },
    )
}

fn agent_rpc_refusal(status: u16, reason: &str) -> OutgoingResponse {
    json_response(
        status,
        &json!({
            "class": "ProtocolIncompatibility",
            "protocol_result_code": null,
            "retriability": "Terminal",
            "request_id": "0",
            "reason": reason,
        }),
    )
}

fn agent_rpc(config: &Config, request: &IncomingRequest) -> OutgoingResponse {
    if request.path != AGENT_RPC_PATH {
        return response(400, "query_string_not_allowed", None);
    }
    let Some(entry) = catalogue()["routes"].as_array().and_then(|routes| {
        routes.iter().find(|r| {
            r["service"] == AGENT_RPC_SERVICE
                && r["path"] == AGENT_RPC_PATH
                && r["method"] == "POST"
                && r["proxy"] == true
                && r["authentication"] == "gateway-api-key"
                && r["surface"] == "agent"
                && r["tls"] == "mtls"
        })
    }) else {
        return response(503, "route_unavailable", None);
    };
    let Some(upstream_path) = entry["upstream_path"]
        .as_str()
        .filter(|path| safe_path(path))
    else {
        return response(503, "invalid_upstream", None);
    };
    if request.method != "POST" {
        return response(405, "method_not_allowed", None);
    }
    if private(AGENT_RPC_SERVICE) {
        return response(403, "private_service", None);
    }
    if ["layerx-tenant", "layerx-agent"]
        .iter()
        .any(|name| request.headers.contains_key(*name))
    {
        return response(400, "untrusted_identity_header", None);
    }
    if let Err(refusal) = authenticate_key(config, request) {
        return refusal;
    }
    if request.headers.get("content-type").map(String::as_str) != Some("application/json") {
        return response(415, "unsupported_media_type", None);
    }
    if request.body.len() > AGENT_RPC_MAX_BODY {
        return agent_rpc_refusal(413, "envelope.oversized");
    }
    if !config.client_identity {
        return response(503, "client_identity_required", None);
    }
    if let Err(reason) = config.routes.health(config, AGENT_RPC_SERVICE) {
        return json_response(
            503,
            &json!({"error":{"code":"route_unavailable","service":AGENT_RPC_SERVICE,"reason":reason}}),
        );
    }
    let Some(binding) = config.routes.bindings.get(AGENT_RPC_SERVICE) else {
        return response(503, "route_unavailable", None);
    };
    let endpoint = match http::Endpoint::parse(&binding.url) {
        Ok(endpoint) if endpoint.base_path.is_empty() => endpoint,
        _ => return response(503, "invalid_upstream", None),
    };
    match config.client.request_forwarded(
        &endpoint,
        "",
        &http::OutboundRequest {
            method: "POST",
            path: upstream_path,
            idempotency: None,
            content_type: "application/json",
            body: &request.body,
        },
        &[],
    ) {
        Ok(reply) if reply.content_type == "application/json" => OutgoingResponse {
            status: reply.status,
            content_type: "application/json".to_owned(),
            headers: reply
                .headers
                .into_iter()
                .filter(|(name, _)| response_header(AGENT_RPC_SERVICE, name))
                .collect(),
            body: reply.body,
            retry_after: None,
        },
        Ok(_) => response(502, "invalid_upstream_content_type", None),
        Err(_) => response(503, "upstream_unavailable", None),
    }
}


pub(super) fn stream(
    config: &Config,
    request: &IncomingRequest,
    downstream: &mut impl std::io::Write,
) -> Option<Result<(), String>> {
    if request.method != "GET" {
        return None;
    }
    let entry = catalogue()["routes"].as_array()?.iter().find(|entry| {
        entry["proxy"] == true
            && entry["service"] == "human"
            && entry["method"] == "GET"
            && entry.get("upstream_path").unwrap_or(&entry["path"]) == "/v1/stream/{cursor}"
            && entry["path"]
                .as_str()
                .is_some_and(|path| matches_path(path, &request.path))
    })?;
    let refusal = if request.headers.contains_key("x-layerx-principal")
        || request.headers.contains_key("x-layerx-api-key")
    {
        Some(response(400, "untrusted_identity_header", None))
    } else if request.headers.contains_key("origin") && config.routes.origin(request).is_none() {
        Some(response(403, "origin_not_allowed", None))
    } else if let Err(reason) = config.routes.health(config, "human") {
        Some(json_response(
            503,
            &json!({"error":{"code":"route_unavailable","service":"human","reason":reason}}),
        ))
    } else {
        None
    };
    if let Some(refusal) = refusal {
        return Some(http::write_response_connection_with_origin(
            downstream,
            &refusal,
            false,
            config.routes.origin(request),
        ));
    }
    let binding = config.routes.bindings.get("human")?;
    let endpoint = match http::Endpoint::parse(&binding.url) {
        Ok(endpoint) => endpoint,
        Err(error) => return Some(Err(error)),
    };
    let Some(path) = routed_path(entry, &request.path) else {
        return Some(Err("invalid stream route target".into()));
    };
    let authorization = request
        .headers
        .get("authorization")
        .map_or("", String::as_str);
    let forwarded: Vec<_> = request
        .headers
        .iter()
        .filter(|(name, _)| forward_header("human", name))
        .map(|(name, value)| (name.as_str(), value.as_str()))
        .collect();
    Some(
        config.client.stream_forwarded(
            &endpoint,
            authorization,
            &http::OutboundRequest {
                method: "GET",
                path: &path,
                idempotency: None,
                content_type: request
                    .headers
                    .get("content-type")
                    .map_or("application/json", String::as_str),
                body: &request.body,
            },
            &forwarded,
            downstream,
            config.routes.origin(request),
        ),
    )
}

fn routed_path(entry: &Value, target: &str) -> Option<String> {
    let (path, query) = target
        .split_once('?')
        .map_or((target, None), |(p, q)| (p, Some(q)));
    let template = entry["path"].as_str()?;
    let upstream = entry
        .get("upstream_path")
        .and_then(Value::as_str)
        .unwrap_or(template);
    let expected: Vec<_> = template.split('/').collect();
    let actual: Vec<_> = path.split('/').collect();
    if expected.len() != actual.len() {
        return None;
    }
    let parameters: BTreeMap<_, _> = expected
        .iter()
        .zip(actual)
        .filter(|(part, _)| part.starts_with(':') || part.starts_with('{'))
        .map(|(key, value)| (*key, value))
        .collect();
    let rewritten = upstream
        .split('/')
        .map(|part| parameters.get(part).copied().unwrap_or(part))
        .collect::<Vec<_>>()
        .join("/");
    if !safe_path(&rewritten) {
        return None;
    }
    Some(match query {
        Some(query) => format!("{rewritten}?{query}"),
        None => rewritten,
    })
}

fn forward_header(service: &str, name: &str) -> bool {
    if matches!(name, "accept" | "x-trace-id" | "x-layerx-trace") {
        return true;
    }
    match service {
        "human" => matches!(
            name,
            "cookie" | "x-layerx-csrf" | "x-layerx-wallet-binding" | "origin" | "last-event-id"
        ),
        "wallet-gateway" => matches!(
            name,
            "x-agent-key" | "x-agent-nonce" | "x-agent-expires" | "x-agent-signature" | "origin"
        ),
        "search-web" => matches!(
            name,
            "payment-signature" | "layerx-payer-did" | "x-payment" | "origin"
        ),
        "interop" => matches!(name, "payment-signature" | "x-payment" | "origin"),
        "webhooks-dashboard" => {
            matches!(name, "cookie" | "x-csrf-token" | "x-layerx-csrf" | "origin")
        }
        "gas" | "ramp" => name == "origin",
        _ => false,
    }
}

fn response_header(service: &str, name: &str) -> bool {
    match name {
        "set-cookie" => matches!(service, "human" | "webhooks-dashboard"),
        "payment-required" | "payment-response" | "x-payment-response" => {
            matches!(service, "search-web" | "interop")
        }
        "last-event-id" => service == "human",
        "content-disposition" => matches!(service, "human" | "search-web"),
        "etag" | "x-content-sha256" | "x-layerx-batch" => service == "archive",
        "retry-after" | "www-authenticate" => true,
        _ => false,
    }
}

fn tunnel_bytes(
    mut source: std::net::TcpStream,
    mut destination: std::net::TcpStream,
) -> std::io::Result<()> {
    use std::io::{Read, Write};
    let started = std::time::Instant::now();
    let mut buffer = [0_u8; 16 * 1024];
    let result = (|| {
        while started.elapsed() < std::time::Duration::from_secs(300) {
            let count = source.read(&mut buffer)?;
            if count == 0 {
                break;
            }
            destination.write_all(&buffer[..count])?;
        }
        Ok(())
    })();
    let _ = destination.shutdown(std::net::Shutdown::Both);
    let _ = source.shutdown(std::net::Shutdown::Both);
    result
}

pub(super) fn rpc_catalogue(
    config: &Config,
    request: &IncomingRequest,
    id: &Value,
    params: Option<&Value>,
) -> Value {
    if params.is_some_and(|p| p != &json!([])) {
        return super::rpc::error(id, -32602, "Invalid params");
    }
    if authenticate_key(config, request).is_err() {
        return super::rpc::error(id, -32002, "Authentication required");
    }
    let mut document = catalogue().clone();
    document["readiness"] = config.routes.readiness(config);
    json!({"jsonrpc":"2.0", "id":id, "result":document})
}
