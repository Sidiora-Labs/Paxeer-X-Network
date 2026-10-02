use layerx_platform_webhooks::error::WebhookError;
use layerx_platform_webhooks::events::{
    DeliveryId, EndpointId, EventKind, Principal, Verification,
};
use layerx_platform_webhooks::hosted::HostedService;
use layerx_platform_webhooks::http::{self, Reply, Request};
use layerx_platform_webhooks::scheme;
use layerx_platform_webhooks::trusted::{
    DeveloperIdentity, IngressRole, SourceTrigger, TrustedSources,
};
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
use rustls::server::WebPkiClientVerifier;
use rustls::{RootCertStore, ServerConfig, ServerConnection, StreamOwned};
use serde::{Deserialize, Serialize};
use std::env;
use std::fs;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const MAX_CONNECTIONS: usize = 256;
const DEFAULT_PAGE: usize = 50;
static ACTIVE_CONNECTIONS: AtomicUsize = AtomicUsize::new(0);

enum Listener {
    Tls(Arc<ServerConfig>),
    Plain,
}

/// The public process serves only the developer API; the ingress process
/// serves only the internal producer and operator routes, over TLS with a
/// verified internal client certificate.
enum Role {
    Public {
        identity: DeveloperIdentity,
    },
    Ingress {
        sources: TrustedSources,
        source_trigger: SourceTrigger,
        operator_trigger: SourceTrigger,
    },
}

struct Config {
    listen: SocketAddr,
    health_listen: Option<SocketAddr>,
    listener: Listener,
    service: Arc<HostedService>,
    role: Role,
    dispatch_interval: Duration,
    dispatch_budget: u32,
    retention_events: usize,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RegisterBody {
    url: String,
    #[serde(default)]
    kinds: Vec<String>,
    #[serde(default)]
    minimum_verification: Option<String>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SuspendBody {
    reason: String,
}

#[derive(Serialize)]
struct SchemeDocument {
    scheme: &'static str,
    algorithm: &'static str,
    signed_message: &'static str,
    receiver_obligation: &'static str,
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |duration| duration.as_secs())
}

fn number(name: &str, default: u64) -> Result<u64, String> {
    env::var(name).map_or(Ok(default), |value| {
        value
            .parse::<u64>()
            .map_err(|_| format!("{name} is not an integer"))
    })
}

const LISTENER_CERTIFICATE_VARIABLES: [&str; 2] = [
    "LAYERX_WEBHOOKS_TLS_CERT_DER",
    "LAYERX_WEBHOOKS_TLS_KEY_DER",
];

const PUBLIC_FOREIGN_VARIABLES: [&str; 3] = [
    "LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER",
    "LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE",
    "LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE",
];
const INGRESS_FOREIGN_VARIABLES: [&str; 1] = ["LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE"];

fn ingress_role() -> Result<bool, String> {
    let ingress = match env::var("LAYERX_WEBHOOKS_ROLE").as_deref() {
        Ok("public") => false,
        Ok("ingress") => true,
        _ => return Err("LAYERX_WEBHOOKS_ROLE must be public or ingress".to_owned()),
    };
    let (role, foreign) = if ingress {
        ("ingress", INGRESS_FOREIGN_VARIABLES.as_slice())
    } else {
        ("public", PUBLIC_FOREIGN_VARIABLES.as_slice())
    };
    foreign
        .iter()
        .find(|variable| env::var_os(variable).is_some())
        .map_or(Ok(ingress), |variable| {
            Err(format!(
                "{variable} is set with LAYERX_WEBHOOKS_ROLE {role}"
            ))
        })
}

fn listener_config(ingress: bool) -> Result<Listener, String> {
    rustls::crypto::ring::default_provider()
        .install_default()
        .map_err(|_| "failed to install TLS provider".to_owned())?;
    if ingress {
        return match env::var("LAYERX_WEBHOOKS_LISTENER").as_deref() {
            Err(env::VarError::NotPresent) | Ok("tls") => {
                let client_ca = env::var("LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER")
                    .map_err(|_| "LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER is required")?;
                tls_config(Some(&client_ca)).map(Listener::Tls)
            }
            _ => {
                Err("LAYERX_WEBHOOKS_ROLE ingress requires LAYERX_WEBHOOKS_LISTENER tls".to_owned())
            }
        };
    }
    match env::var("LAYERX_WEBHOOKS_LISTENER") {
        Err(env::VarError::NotPresent) => tls_config(None).map(Listener::Tls),
        Ok(mode) if mode == "tls" => tls_config(None).map(Listener::Tls),
        Ok(mode) if mode == "plain" => LISTENER_CERTIFICATE_VARIABLES
            .iter()
            .find(|variable| env::var_os(variable).is_some())
            .map_or(Ok(Listener::Plain), |variable| {
                Err(format!(
                    "{variable} is set with LAYERX_WEBHOOKS_LISTENER plain"
                ))
            }),
        _ => Err("LAYERX_WEBHOOKS_LISTENER must be tls or plain".to_owned()),
    }
}

fn tls_config(client_ca: Option<&str>) -> Result<Arc<ServerConfig>, String> {
    let cert = CertificateDer::from(
        fs::read(
            env::var("LAYERX_WEBHOOKS_TLS_CERT_DER")
                .map_err(|_| "webhook TLS certificate is required")?,
        )
        .map_err(|error| error.to_string())?,
    );
    let key = PrivateKeyDer::from(PrivatePkcs8KeyDer::from(
        fs::read(
            env::var("LAYERX_WEBHOOKS_TLS_KEY_DER").map_err(|_| "webhook TLS key is required")?,
        )
        .map_err(|error| error.to_string())?,
    ));
    let builder = ServerConfig::builder();
    let builder = match client_ca {
        None => builder.with_no_client_auth(),
        Some(path) => {
            let mut roots = RootCertStore::empty();
            roots
                .add(CertificateDer::from(
                    fs::read(path).map_err(|error| error.to_string())?,
                ))
                .map_err(|_| "ingress client CA is invalid".to_owned())?;
            builder.with_client_cert_verifier(
                WebPkiClientVerifier::builder(Arc::new(roots))
                    .build()
                    .map_err(|_| "ingress client CA is invalid".to_owned())?,
            )
        }
    };
    builder
        .with_single_cert(vec![cert], key)
        .map(Arc::new)
        .map_err(|error| error.to_string())
}

fn config() -> Result<Config, String> {
    let ingress = ingress_role()?;
    let listener = listener_config(ingress)?;
    let role = if ingress {
        Role::Ingress {
            sources: TrustedSources::from_environment()?,
            source_trigger: SourceTrigger::from_environment()?,
            operator_trigger: SourceTrigger::operator_from_environment()?,
        }
    } else {
        Role::Public {
            identity: DeveloperIdentity::from_environment()?,
        }
    };
    Ok(Config {
        listen: env::var("LAYERX_WEBHOOKS_LISTEN")
            .unwrap_or_else(|_| "0.0.0.0:9444".to_owned())
            .parse::<SocketAddr>()
            .map_err(|_| "webhook listen address is invalid".to_owned())?,
        health_listen: match env::var("LAYERX_WEBHOOKS_HEALTH_LISTEN") {
            Ok(value) if !ingress => Some(value.parse::<SocketAddr>()
                .map_err(|_| "webhook health listen address is invalid".to_owned())?),
            Err(env::VarError::NotPresent) => None,
            _ => return Err("webhook health listener is only available to the public role".to_owned()),
        },
        listener,
        service: Arc::new(HostedService::from_environment()?),
        role,
        dispatch_interval: Duration::from_secs(number(
            "LAYERX_WEBHOOKS_DISPATCH_INTERVAL_SECONDS",
            1,
        )?),
        dispatch_budget: u32::try_from(number("LAYERX_WEBHOOKS_DISPATCH_BUDGET", 64)?)
            .map_err(|_| "webhook dispatch budget is invalid".to_owned())?,
        retention_events: usize::try_from(number("LAYERX_WEBHOOKS_RETENTION_EVENTS", 10_000)?)
            .map_err(|_| "webhook retention bound is invalid".to_owned())?
            .clamp(1, 20_000),
    })
}

fn refusal(error: &WebhookError) -> Reply {
    let (status, code, retry) = match error {
        WebhookError::InvalidRequest => (400, "invalid_request", None),
        WebhookError::UnknownEndpoint => (404, "unknown_endpoint", None),
        WebhookError::UnknownDelivery => (404, "unknown_delivery", None),
        WebhookError::NotDeadLettered => (409, "not_dead_lettered", None),
        WebhookError::EndpointSuspended => (409, "endpoint_suspended", None),
        WebhookError::EventConflict => (409, "conflict", None),
        WebhookError::OrderViolation => (409, "order_violation", None),
        WebhookError::InvalidCursor => (400, "invalid_cursor", None),
        WebhookError::CursorExpired => (410, "cursor_expired", None),
        WebhookError::VerificationRequired => (422, "verification_required", None),
        WebhookError::SignatureRejected => (401, "signature_rejected", None),
        WebhookError::ReplayRejected => (409, "replay_rejected", None),
        WebhookError::StaleTimestamp => (400, "stale_timestamp", None),
        WebhookError::ReplayCapacity => (503, "replay_capacity", Some(10)),
        WebhookError::Entropy => (503, "entropy_unavailable", Some(5)),
        WebhookError::CorruptStore | WebhookError::Unavailable | WebhookError::Io(_) => {
            (503, "dependency_unavailable", Some(5))
        }
        WebhookError::Gateway(_) => (422, "verification_refused", None),
    };
    Reply::refusal(status, code, retry)
}

fn encoded<T: Serialize>(status: u16, value: &T) -> Reply {
    serde_json::to_string(value).map_or_else(
        |_| Reply::refusal(503, "encoding_failed", Some(5)),
        |body| Reply::json(status, body),
    )
}

fn body<T: for<'de> Deserialize<'de>>(request: &Request) -> Result<T, WebhookError> {
    serde_json::from_slice(&request.body).map_err(|_| WebhookError::InvalidRequest)
}

fn page(request: &Request) -> usize {
    request
        .parameter("limit")
        .and_then(|value| value.parse::<usize>().ok())
        .unwrap_or(DEFAULT_PAGE)
        .clamp(1, 200)
}

fn principal(identity: &DeveloperIdentity, request: &Request) -> Result<Principal, WebhookError> {
    identity.authenticate(
        request.header("authorization"),
        request.header("cookie"),
        request.header("x-layerx-csrf"),
        matches!(request.method.as_str(), "POST" | "DELETE"),
    )
}

fn endpoints(config: &Config, request: &Request, principal: &Principal, at: u64) -> Reply {
    if request.method == "GET" {
        return config
            .service
            .snapshot(principal, at, page(request))
            .map_or_else(
                |error| refusal(&error),
                |value| encoded(200, &value.endpoints),
            );
    }
    if request.method != "POST" {
        return Reply::refusal(404, "not_found", None);
    }
    let Some(idempotency) = request.header("idempotency-key") else {
        return Reply::refusal(400, "idempotency_key_required", None);
    };
    let body = match body::<RegisterBody>(request) {
        Ok(value) => value,
        Err(error) => return refusal(&error),
    };
    let kinds = match body
        .kinds
        .iter()
        .map(|value| EventKind::parse(value))
        .collect::<Result<Vec<_>, _>>()
    {
        Ok(value) => value,
        Err(error) => return refusal(&error),
    };
    let minimum = match body.minimum_verification.as_deref() {
        Some(value) => match Verification::parse(value) {
            Ok(value) => value,
            Err(error) => return refusal(&error),
        },
        None => Verification::Unverified,
    };
    config
        .service
        .register(principal, &body.url, &kinds, minimum, idempotency, at)
        .map_or_else(|error| refusal(&error), |value| encoded(201, &value))
}

fn endpoint_route(
    config: &Config,
    request: &Request,
    principal: &Principal,
    endpoint: &str,
    action: &str,
    at: u64,
) -> Reply {
    let endpoint = match EndpointId::new(endpoint) {
        Ok(value) => value,
        Err(error) => return refusal(&error),
    };
    match (request.method.as_str(), action) {
        ("GET", "events") => config
            .service
            .events_since(
                principal,
                &endpoint,
                request.parameter("cursor"),
                page(request),
            )
            .map_or_else(|error| refusal(&error), |value| encoded(200, &value)),
        ("GET", "keys") => config
            .service
            .signing_keys(principal, &endpoint, at)
            .map_or_else(|error| refusal(&error), |value| encoded(200, &value)),
        ("POST", "keys") => match request.header("idempotency-key") {
            Some(idempotency) => config
                .service
                .rotate_key(principal, &endpoint, idempotency, at)
                .map_or_else(|error| refusal(&error), |value| encoded(201, &value)),
            None => Reply::refusal(400, "idempotency_key_required", None),
        },
        ("POST", "redeliveries") => {
            let Some(idempotency) = request.header("idempotency-key") else {
                return Reply::refusal(400, "idempotency_key_required", None);
            };
            config
                .service
                .redeliver(
                    principal,
                    &endpoint,
                    request.parameter("cursor"),
                    page(request),
                    idempotency,
                    at,
                )
                .map_or_else(|error| refusal(&error), |value| encoded(202, &value))
        }
        ("POST", "suspensions") => body::<SuspendBody>(request)
            .and_then(|body| {
                config
                    .service
                    .suspend(principal, &endpoint, &body.reason, at)
            })
            .map_or_else(
                |error| refusal(&error),
                |()| Reply::json(200, "{\"suspended\":true}".to_owned()),
            ),
        ("POST", "resumptions") => config.service.resume(principal, &endpoint).map_or_else(
            |error| refusal(&error),
            |()| Reply::json(200, "{\"suspended\":false}".to_owned()),
        ),
        _ => Reply::refusal(404, "not_found", None),
    }
}

fn owned_route(config: &Config, request: &Request, principal: &Principal, at: u64) -> Reply {
    let segments = request.segments();
    match (request.method.as_str(), segments.as_slice()) {
        (_, ["v1", "webhooks", "endpoints"]) => endpoints(config, request, principal, at),
        (_, ["v1", "webhooks", "endpoints", endpoint, action]) => {
            endpoint_route(config, request, principal, endpoint, action, at)
        }
        ("GET", ["v1", "webhooks", "events"]) => config
            .service
            .snapshot(principal, at, page(request))
            .map_or_else(|error| refusal(&error), |value| encoded(200, &value.events)),
        ("GET", ["v1", "webhooks", "deliveries"]) => config
            .service
            .snapshot(principal, at, page(request))
            .map_or_else(
                |error| refusal(&error),
                |value| encoded(200, &value.deliveries),
            ),
        ("GET", ["v1", "webhooks", "dead-letters"]) => config
            .service
            .snapshot(principal, at, page(request))
            .map_or_else(
                |error| refusal(&error),
                |value| encoded(200, &value.dead_letters),
            ),
        ("POST", ["v1", "webhooks", "dead-letters", delivery, "replay"]) => {
            let Some(idempotency) = request.header("idempotency-key") else {
                return Reply::refusal(400, "idempotency_key_required", None);
            };
            DeliveryId::new(*delivery)
                .and_then(|delivery| {
                    config
                        .service
                        .replay_dead_letter(principal, &delivery, idempotency, at)
                })
                .map_or_else(|error| refusal(&error), |value| encoded(202, &value))
        }
        _ => Reply::refusal(404, "not_found", None),
    }
}

fn internal_route(
    config: &Config,
    sources: &TrustedSources,
    source_trigger: &SourceTrigger,
    operator_trigger: &SourceTrigger,
    request: &Request,
    peer: Option<IngressRole>,
    at: u64,
) -> Reply {
    let segments = request.segments();
    match (request.method.as_str(), segments.as_slice()) {
        ("POST", ["internal", "v1", "events", kind, source_event]) => {
            if peer != Some(IngressRole::Producer) {
                return Reply::refusal(403, "producer_role_required", None);
            }
            if !source_trigger.authorizes(request.header("authorization")) {
                return Reply::refusal(401, "source_authentication_required", None);
            }
            EventKind::parse(kind)
                .and_then(|kind| sources.fetch(kind, source_event))
                .and_then(|event| config.service.publish(&event, at))
                .map_or_else(|error| refusal(&error), |value| encoded(202, &value))
        }
        ("POST", ["internal", "v1", "dispatch"]) => {
            if peer != Some(IngressRole::Operator) {
                return Reply::refusal(403, "operator_role_required", None);
            }
            if !operator_trigger.authorizes(request.header("authorization")) {
                return Reply::refusal(401, "operator_authentication_required", None);
            }
            config
                .service
                .dispatch(at, config.dispatch_budget)
                .map_or_else(|error| refusal(&error), |value| encoded(200, &value))
        }
        _ => Reply::refusal(404, "not_found", None),
    }
}

fn internal_path(request: &Request) -> bool {
    request
        .segments()
        .first()
        .is_some_and(|segment| segment.eq_ignore_ascii_case("internal"))
}

fn readiness(config: &Config) -> Reply {
    let delivery = config.service.ready();
    match &config.role {
        Role::Public { .. } => encoded(
            if delivery { 200 } else { 503 },
            &serde_json::json!({
                "ready": delivery,
                "role": "public",
                "components": { "delivery_state_and_signer": delivery }
            }),
        ),
        Role::Ingress { sources, .. } => {
            let sources = sources.ready();
            encoded(
                if delivery && sources { 200 } else { 503 },
                &serde_json::json!({
                    "ready": delivery && sources,
                    "role": "ingress",
                    "components": {
                        "delivery_state_and_signer": delivery,
                        "canonical_sources_and_receipt_authority": sources
                    }
                }),
            )
        }
    }
}

fn route(
    config: &Config,
    request: &Request,
    peer: Option<Result<IngressRole, WebhookError>>,
) -> Reply {
    let at = now();
    if request.method == "GET" && request.path == "/healthz" {
        return readiness(config);
    }
    match &config.role {
        Role::Public { identity } => {
            if internal_path(request) {
                return Reply::refusal(404, "not_found", None);
            }
            if request.method == "GET" && request.path == "/v1/webhooks/scheme" {
                return encoded(
                    200,
                    &SchemeDocument {
                        scheme: scheme::SCHEME_VERSION,
                        algorithm: "ed25519",
                        signed_message: "<event-id>.<timestamp>. followed by exact body bytes",
                        receiver_obligation: scheme::RECEIVER_OBLIGATION,
                    },
                );
            }
            if !request.path.starts_with("/v1/webhooks/") {
                return Reply::refusal(404, "not_found", None);
            }
            match principal(identity, request) {
                Ok(principal) => owned_route(config, request, &principal, at),
                Err(_) => Reply::refusal(401, "session_required", None),
            }
        }
        Role::Ingress {
            sources,
            source_trigger,
            operator_trigger,
        } => {
            if !request.path.starts_with("/internal/") {
                return Reply::refusal(404, "not_found", None);
            }
            let Some(Ok(peer)) = peer else {
                return Reply::refusal(403, "peer_role_refused", None);
            };
            internal_route(
                config,
                sources,
                source_trigger,
                operator_trigger,
                request,
                Some(peer),
                at,
            )
        }
    }
}

fn serve_health(listener: TcpListener, config: &Arc<Config>) {
    for accepted in listener.incoming() {
        let Ok(mut stream) = accepted else {
            continue;
        };
        if ACTIVE_CONNECTIONS.fetch_add(1, Ordering::AcqRel) >= MAX_CONNECTIONS {
            ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
            continue;
        }
        let config = Arc::clone(config);
        thread::spawn(move || {
            let _guard = ConnectionGuard;
            if stream.set_read_timeout(Some(Duration::from_secs(15))).is_err()
                || stream.set_write_timeout(Some(Duration::from_secs(15))).is_err()
            {
                return;
            }
            let reply = http::read_request(&mut stream).map_or_else(
                |_| Reply::refusal(400, "invalid_request", None),
                |request| {
                    if request.method == "GET" && request.path == "/healthz" {
                        readiness(&config)
                    } else {
                        Reply::refusal(404, "not_found", None)
                    }
                },
            );
            let _ = http::write_reply(&mut stream, &reply);
        });
    }
}

fn serve(config: Config) -> Result<(), String> {
    let health = config.health_listen.map(TcpListener::bind).transpose()
        .map_err(|error| format!("webhook health listen: {error}"))?;
    let config = Arc::new(config);
    if let Some(listener) = health {
        let health_config = Arc::clone(&config);
        thread::Builder::new().name("webhook-readiness".to_owned())
            .spawn(move || serve_health(listener, &health_config))
            .map_err(|error| format!("webhook health thread: {error}"))?;
    }
    if config.dispatch_interval.is_zero() {
        return Err("webhook dispatch interval must be positive".to_owned());
    }
    let worker_config = Arc::clone(&config);
    thread::spawn(move || loop {
        thread::sleep(worker_config.dispatch_interval);
        let _ = worker_config
            .service
            .dispatch(now(), worker_config.dispatch_budget);
        let _ = worker_config
            .service
            .prune_all(worker_config.retention_events);
    });
    let listener = TcpListener::bind(config.listen).map_err(|error| error.to_string())?;
    for accepted in listener.incoming() {
        let Ok(tcp) = accepted else {
            continue;
        };
        if ACTIVE_CONNECTIONS.fetch_add(1, Ordering::AcqRel) >= MAX_CONNECTIONS {
            ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
            continue;
        }
        let request_config = Arc::clone(&config);
        thread::spawn(move || {
            let _guard = ConnectionGuard;
            let _ = handle(tcp, &request_config);
        });
    }
    Ok(())
}

struct ConnectionGuard;

impl Drop for ConnectionGuard {
    fn drop(&mut self) {
        ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
    }
}

fn handle(tcp: TcpStream, config: &Config) -> Result<(), String> {
    tcp.set_read_timeout(Some(Duration::from_secs(15)))
        .map_err(|error| error.to_string())?;
    tcp.set_write_timeout(Some(Duration::from_secs(15)))
        .map_err(|error| error.to_string())?;
    match &config.listener {
        Listener::Tls(tls) => {
            let connection =
                ServerConnection::new(Arc::clone(tls)).map_err(|error| error.to_string())?;
            let mut stream = StreamOwned::new(connection, tcp);
            let request = http::read_request(&mut stream);
            let peer = stream
                .conn
                .peer_certificates()
                .and_then(|chain| chain.first())
                .map(|leaf| IngressRole::from_certificate(leaf.as_ref()));
            reply(config, &mut stream, request, peer)
        }
        Listener::Plain => {
            let mut stream = tcp;
            let request = http::read_request(&mut stream);
            reply(config, &mut stream, request, None)
        }
    }
}

fn reply<S: Read + Write, E>(
    config: &Config,
    stream: &mut S,
    request: Result<Request, E>,
    peer: Option<Result<IngressRole, WebhookError>>,
) -> Result<(), String> {
    let reply = request.map_or_else(
        |_| Reply::refusal(400, "invalid_request", None),
        |request| route(config, &request, peer),
    );
    http::write_reply(stream, &reply).map_err(|error| error.to_string())
}

fn main() {
    if let Err(error) = config().and_then(serve) {
        eprintln!("layerx-webhooks: {error}");
        std::process::exit(2);
    }
}
