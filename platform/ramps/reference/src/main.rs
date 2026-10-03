#![forbid(unsafe_code)]

use std::collections::BTreeMap;
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use layerx_paxeer_client::{
    ChainSignal, EndpointConfig, EndpointSignal, EndpointTransport, FinalityTracker, TrackerConfig,
    TransactionHash,
};
use layerx_ramp_toolkit::clients::{
    ComplianceClient, Endpoint, IdentityClient, LayerxClient, LayerxConfig, MutualTlsClient,
    MutualTlsFiles, PaxeerCustodyClient, ProviderCallback, ProviderClient, SecretFile, parse_hex32,
};
use layerx_ramp_toolkit::engine::{InventoryRebalancer, RampEngine};
use layerx_ramp_toolkit::journal::{Journal, WorkflowStage};
use layerx_ramp_toolkit::{
    CreateOrder, EXTERNAL_CUSTODY_LABEL, OperatorIdentity, QuoteTerms, RampDirection, RampError,
    RampOrder, platform_ramp_toolkit,
};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use native_tls::{Identity, TlsAcceptor, TlsStream};
use serde::{Deserialize, Serialize};
use serde_json::json;
use subtle::ConstantTimeEq as _;

const MAX_REQUEST_BYTES: usize = 1024 * 1024;
const MAX_HEADERS: usize = 32 * 1024;
const MAX_CONNECTIONS: usize = 128;

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct Config {
    listen: String,
    listener: Option<String>,
    journal_path: PathBuf,
    worker_id: String,
    lease_seconds: u64,
    reconcile_seconds: u64,
    operator: OperatorIdentity,
    quotes: Vec<QuoteTerms>,
    server_identity_pkcs12: Option<PathBuf>,
    server_identity_password_file: Option<PathBuf>,
    client_tls: ClientTls,
    identity: IdentityConfig,
    compliance: ComplianceConfig,
    provider: ProviderConfig,
    layerx: LayerxConfig,
    paxeer: PaxeerConfig,
    provider_callback_public_key: String,
    operator_control_token_file: PathBuf,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct ClientTls {
    ca_pem: PathBuf,
    identity_pkcs12: PathBuf,
    identity_password_file: PathBuf,
    timeout_seconds: u64,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct IdentityConfig {
    endpoint: String,
    service_token_file: PathBuf,
    audience: String,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct ComplianceConfig {
    endpoint: String,
    service_token_file: PathBuf,
    public_key: String,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct ProviderConfig {
    endpoint: String,
    credential_file: PathBuf,
    settlement_path: String,
    status_path: String,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct PaxeerConfig {
    custody_endpoint: String,
    custody_credential_file: PathBuf,
    broadcast_path: String,
    status_path: String,
    operator_account: String,
    wallet_address: String,
    vault_id: String,
    signer_key_handle: String,
    rpc_endpoints: Vec<String>,
    rpc_trust_anchor_der: PathBuf,
    rpc_chain_id: u64,
    rpc_minimum_agreement: usize,
    required_confirmations: u64,
    poll_cadence_seconds: u64,
    delayed_after_polls: u64,
}

enum Listener {
    Tls(TlsAcceptor),
    Plain,
}

trait Transport: Read + Write {
    fn tcp(&self) -> &TcpStream;
}

impl Transport for TlsStream<TcpStream> {
    fn tcp(&self) -> &TcpStream {
        self.get_ref()
    }
}

impl Transport for TcpStream {
    fn tcp(&self) -> &TcpStream {
        self
    }
}

struct State {
    journal: Mutex<Journal>,
    quotes: BTreeMap<String, QuoteTerms>,
    operator: OperatorIdentity,
    identity: IdentityClient,
    compliance: ComplianceClient,
    provider: ProviderClient,
    layerx: LayerxClient,
    paxeer: PaxeerCustodyClient,
    paxeer_tracker_config: TrackerConfig,
    paxeer_trackers: Mutex<BTreeMap<[u8; 32], FinalityTracker>>,
    registry: ModuleRegistry,
    worker_id: String,
    lease_seconds: u64,
    provider_callback_public_key: [u8; 32],
    operator_control_token: String,
}

struct ConnectionGate {
    active: AtomicUsize,
    maximum: usize,
}

impl ConnectionGate {
    const fn new(maximum: usize) -> Self {
        Self {
            active: AtomicUsize::new(0),
            maximum,
        }
    }

    fn try_acquire(self: &Arc<Self>) -> Option<ConnectionPermit> {
        self.active
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
                (active < self.maximum).then_some(active + 1)
            })
            .ok()
            .map(|_| ConnectionPermit(Arc::clone(self)))
    }
}

struct ConnectionPermit(Arc<ConnectionGate>);

impl Drop for ConnectionPermit {
    fn drop(&mut self) {
        self.0.active.fetch_sub(1, Ordering::AcqRel);
    }
}

fn main() {
    if std::env::args_os().nth(1).as_deref() == Some(std::ffi::OsStr::new("--verify-receipt")) {
        if let Err(error) = verify_receipt_command() {
            eprintln!("ramp receipt verification refused: {error}");
            std::process::exit(1);
        }
        return;
    }
    if let Err(error) = run() {
        eprintln!("layerx-reference-ramp refused startup: {error}");
        std::process::exit(1);
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ReceiptVerifierConfig {
    client_tls: ClientTls,
    layerx: LayerxConfig,
}

fn verify_receipt_command() -> Result<(), String> {
    let arguments: Vec<_> = std::env::args_os().skip(2).collect();
    if arguments.len() != 3 {
        return Err("usage: --verify-receipt CONFIG.json ORDER.json ACTIVITY_HEX".to_owned());
    }
    let read = |path: &std::ffi::OsString| {
        SecretFile::new(PathBuf::from(path))
            .and_then(|file| file.read())
            .map_err(|_| "protected verifier input unavailable".to_owned())
    };
    let config: ReceiptVerifierConfig = serde_json::from_slice(&read(&arguments[0])?)
        .map_err(|_| "invalid verifier configuration".to_owned())?;
    let order: RampOrder = serde_json::from_slice(&read(&arguments[1])?)
        .map_err(|_| "invalid bound order".to_owned())?;
    order
        .validate_bound()
        .map_err(|_| "invalid order identity".to_owned())?;
    let activity = arguments[2].to_str().ok_or("invalid activity encoding")?;
    let activity = parse_hex32(activity).map_err(|_| "invalid activity identity")?;
    if !(1..=120).contains(&config.client_tls.timeout_seconds) {
        return Err("invalid verifier timeout".to_owned());
    }
    let tls = MutualTlsFiles {
        ca_pem: config.client_tls.ca_pem,
        identity_pkcs12: SecretFile::new(config.client_tls.identity_pkcs12)
            .map_err(|_| "invalid verifier identity")?,
        identity_password: SecretFile::new(config.client_tls.identity_password_file)
            .map_err(|_| "invalid verifier password")?,
    };
    let http = MutualTlsClient::new(&tls, Duration::from_secs(config.client_tls.timeout_seconds))
        .map_err(|_| "invalid verifier TLS configuration")?;
    let client = build_layerx(&config.layerx, http)?;
    let layerx_ramp_toolkit::clients::LayerxSubmission::Verified { leg, .. } = client
        .resolve(&order, activity)
        .map_err(|_| "receipt proof refused")?
    else {
        return Err("receipt not verified".to_owned());
    };
    println!(
        "{}",
        json!({
            "verified": true,
            "order_digest": order.order_digest,
            "activity_id": leg.activity_id,
            "receipt_digest": leg.receipt_digest,
            "batch_id": leg.batch_id,
            "network_id": config.layerx.network_id,
            "wire_version": config.layerx.protocol_version,
            "external_custody_label": EXTERNAL_CUSTODY_LABEL
        })
    );
    Ok(())
}

fn run() -> Result<(), String> {
    let config_path = std::env::args_os()
        .nth(1)
        .ok_or_else(|| "usage: layerx-reference-ramp CONFIG.json".to_owned())?;
    let config: Config = serde_json::from_slice(
        &SecretFile::new(PathBuf::from(config_path))
            .and_then(|file| file.read())
            .map_err(|_| "protected config unavailable".to_owned())?,
    )
    .map_err(|error| format!("parse config: {error}"))?;
    validate_config(&config)?;
    let listener_mode = Arc::new(listener_config(&config)?);
    let state = Arc::new(build_state(&config)?);
    let reconcile_state = Arc::clone(&state);
    let cadence = Duration::from_secs(config.reconcile_seconds);
    thread::Builder::new()
        .name("ramp-reconciler".to_owned())
        .spawn(move || reconcile_loop(&reconcile_state, cadence))
        .map_err(|error| format!("start reconciler: {error}"))?;
    let listener = TcpListener::bind(&config.listen)
        .map_err(|error| format!("bind {}: {error}", config.listen))?;
    let connection_gate = Arc::new(ConnectionGate::new(MAX_CONNECTIONS));
    println!("{}", platform_ramp_toolkit());
    println!("{EXTERNAL_CUSTODY_LABEL}");
    for stream in listener.incoming() {
        let stream = match stream {
            Ok(stream) => stream,
            Err(error) => {
                eprintln!("ramp accept failed: {error}");
                continue;
            }
        };
        let Some(permit) = connection_gate.try_acquire() else {
            eprintln!("ramp connection limit reached");
            continue;
        };
        let state = Arc::clone(&state);
        let listener_mode = Arc::clone(&listener_mode);
        if thread::Builder::new()
            .name("ramp-request".to_owned())
            .spawn(move || {
                let _permit = permit;
                serve(stream, &listener_mode, &state);
            })
            .is_err()
        {
            eprintln!("ramp request worker unavailable");
        }
    }
    Ok(())
}

fn validate_config(config: &Config) -> Result<(), String> {
    if config.worker_id.is_empty()
        || config.worker_id.len() > 128
        || config.lease_seconds == 0
        || config.reconcile_seconds == 0
        || config.client_tls.timeout_seconds == 0
        || config.lease_seconds <= config.client_tls.timeout_seconds.saturating_mul(5)
        || config.quotes.is_empty()
        || config.paxeer.rpc_endpoints.is_empty()
        || config.paxeer.rpc_chain_id == 0
        || config.paxeer.rpc_minimum_agreement < 2
        || config.paxeer.rpc_minimum_agreement > config.paxeer.rpc_endpoints.len()
        || config.paxeer.required_confirmations == 0
        || config.paxeer.poll_cadence_seconds == 0
        || config.paxeer.delayed_after_polls == 0
        || config.paxeer.operator_account != config.operator.account
        || config.layerx.protocol_version != layerx_wire::limits::PROTOCOL_VERSION
        || config.layerx.network_id == 0
        || config.layerx.fee_limit == 0
        || config.identity.audience.is_empty()
        || !valid_service_path(&config.provider.settlement_path)
        || !valid_service_path(&config.provider.status_path)
        || !valid_service_path(&config.paxeer.broadcast_path)
        || !valid_service_path(&config.paxeer.status_path)
        || !valid_opaque(&config.paxeer.wallet_address)
        || !valid_opaque(&config.paxeer.vault_id)
        || !valid_opaque(&config.paxeer.signer_key_handle)
        || config
            .operator
            .account
            .strip_prefix("agent:")
            .and_then(|account| account.strip_suffix(":main"))
            != Some(config.layerx.actor_did.as_str())
    {
        return Err("invalid worker, timing or quote configuration".to_owned());
    }
    config
        .operator
        .validate()
        .map_err(|_| "operator identity rejected".to_owned())?;
    for quote in &config.quotes {
        quote
            .validate(now())
            .map_err(|_| format!("quote {} rejected", quote.quote_id))?;
    }
    Ok(())
}

fn valid_service_path(path: &str) -> bool {
    path.starts_with('/')
        && path.len() <= 512
        && !path.contains(['?', '#', '\\'])
        && !path.split('/').any(|segment| matches!(segment, "." | ".."))
}

fn valid_opaque(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 512
        && !value
            .bytes()
            .any(|byte| byte.is_ascii_control() || matches!(byte, b'"' | b'\'' | b'\\'))
}

fn listener_config(config: &Config) -> Result<Listener, String> {
    match config.listener.as_deref() {
        None | Some("tls") => server_acceptor(config).map(Listener::Tls),
        Some("plain") => match (
            &config.server_identity_pkcs12,
            &config.server_identity_password_file,
        ) {
            (Some(_), _) => Err("server_identity_pkcs12 is set with listener plain".to_owned()),
            (_, Some(_)) => {
                Err("server_identity_password_file is set with listener plain".to_owned())
            }
            (None, None) => Ok(Listener::Plain),
        },
        Some(_) => Err("listener must be tls or plain".to_owned()),
    }
}

fn server_acceptor(config: &Config) -> Result<TlsAcceptor, String> {
    let identity_path = config
        .server_identity_pkcs12
        .as_ref()
        .ok_or_else(|| "server_identity_pkcs12 is required".to_owned())?;
    let password_path = config
        .server_identity_password_file
        .as_ref()
        .ok_or_else(|| "server_identity_password_file is required".to_owned())?;
    let identity = SecretFile::new(identity_path)
        .map_err(|_| "invalid server identity file".to_owned())?
        .read()
        .map_err(|_| "server identity unavailable".to_owned())?;
    let password = secret_text(password_path)?;
    let identity = Identity::from_pkcs12(&identity, &password)
        .map_err(|_| "server identity rejected".to_owned())?;
    TlsAcceptor::builder(identity)
        .min_protocol_version(Some(native_tls::Protocol::Tlsv12))
        .build()
        .map_err(|_| "server TLS configuration rejected".to_owned())
}

fn build_layerx(config: &LayerxConfig, http: MutualTlsClient) -> Result<LayerxClient, String> {
    config.build(http)
}

fn build_state(config: &Config) -> Result<State, String> {
    let tls = MutualTlsFiles {
        ca_pem: config.client_tls.ca_pem.clone(),
        identity_pkcs12: SecretFile::new(&config.client_tls.identity_pkcs12)
            .map_err(|_| "invalid client identity".to_owned())?,
        identity_password: SecretFile::new(&config.client_tls.identity_password_file)
            .map_err(|_| "invalid client identity password".to_owned())?,
    };
    let timeout = Duration::from_secs(config.client_tls.timeout_seconds);
    let client =
        || MutualTlsClient::new(&tls, timeout).map_err(|_| "mTLS client rejected".to_owned());
    let identity = IdentityClient {
        http: client()?,
        endpoint: Endpoint::parse(&config.identity.endpoint)
            .map_err(|_| "identity endpoint rejected".to_owned())?,
        service_token: secret_text(&config.identity.service_token_file)?,
        audience: config.identity.audience.clone(),
    };
    let compliance = ComplianceClient {
        http: client()?,
        endpoint: Endpoint::parse(&config.compliance.endpoint)
            .map_err(|_| "compliance endpoint rejected".to_owned())?,
        service_token: secret_text(&config.compliance.service_token_file)?,
        verifying_key: configured_key(&config.compliance.public_key, "compliance")?,
    };
    let provider = ProviderClient {
        http: client()?,
        endpoint: Endpoint::parse(&config.provider.endpoint)
            .map_err(|_| "provider endpoint rejected".to_owned())?,
        credential: secret_text(&config.provider.credential_file)?,
        settlement_path: config.provider.settlement_path.clone(),
        status_path: config.provider.status_path.clone(),
    };
    let layerx = build_layerx(&config.layerx, client()?)?;
    let paxeer = PaxeerCustodyClient {
        http: client()?,
        endpoint: Endpoint::parse(&config.paxeer.custody_endpoint)
            .map_err(|_| "Paxeer custody endpoint rejected".to_owned())?,
        credential: secret_text(&config.paxeer.custody_credential_file)?,
        broadcast_path: config.paxeer.broadcast_path.clone(),
        status_path: config.paxeer.status_path.clone(),
        operator_account: config.paxeer.operator_account.clone(),
        wallet_address: config.paxeer.wallet_address.clone(),
        vault_id: config.paxeer.vault_id.clone(),
        signer_key_handle: config.paxeer.signer_key_handle.clone(),
    };
    let paxeer_tracker_config = tracker_config(config, timeout)?;
    let registry = asset_registry()?;
    let mut quotes = BTreeMap::new();
    for quote in &config.quotes {
        if quotes
            .insert(quote.quote_id.clone(), quote.clone())
            .is_some()
        {
            return Err("duplicate quote id".to_owned());
        }
    }
    let state = State {
        journal: Mutex::new(
            Journal::open(&config.journal_path).map_err(|_| "journal rejected".to_owned())?,
        ),
        quotes,
        operator: config.operator.clone(),
        identity,
        compliance,
        provider,
        layerx,
        paxeer,
        paxeer_tracker_config,
        paxeer_trackers: Mutex::new(BTreeMap::new()),
        registry,
        worker_id: config.worker_id.clone(),
        lease_seconds: config.lease_seconds,
        provider_callback_public_key: configured_key(
            &config.provider_callback_public_key,
            "provider callback",
        )?,
        operator_control_token: secret_text(&config.operator_control_token_file)?,
    };
    if verified_recovery(&state).is_err() {
        eprintln!("ramp journal recovery requires verified external settlement");
    }
    Ok(state)
}

fn asset_registry() -> Result<ModuleRegistry, String> {
    let send = ActivityType::new(ModuleId::Asset, 5)
        .map_err(|_| "asset send activity rejected".to_owned())?;
    let receive = ActivityType::new(ModuleId::Asset, 6)
        .map_err(|_| "asset receive activity rejected".to_owned())?;
    let registration = ModuleRegistration::new(ModuleId::Asset, &[send, receive])
        .map_err(|_| "asset registry rejected".to_owned())?;
    let registry =
        ModuleRegistry::new(&[registration]).map_err(|_| "module registry rejected".to_owned())?;
    Ok(registry)
}

fn tracker_config(config: &Config, timeout: Duration) -> Result<TrackerConfig, String> {
    let rpc_trust_anchor_der = fs::read(&config.paxeer.rpc_trust_anchor_der)
        .map_err(|_| "Paxeer RPC trust anchor is unavailable".to_owned())?;
    if rpc_trust_anchor_der.is_empty() {
        return Err("Paxeer RPC trust anchor is empty".to_owned());
    }
    let paxeer_tracker_config = TrackerConfig {
        endpoints: config
            .paxeer
            .rpc_endpoints
            .iter()
            .map(|url| EndpointConfig {
                url: url.clone(),
                request_timeout: timeout,
                transport: EndpointTransport::PinnedTls {
                    trust_anchor_der: rpc_trust_anchor_der.clone(),
                },
                expected_chain_id: config.paxeer.rpc_chain_id,
            })
            .collect(),
        minimum_endpoint_agreement: config.paxeer.rpc_minimum_agreement,
        required_confirmations: config.paxeer.required_confirmations,
        poll_cadence: Duration::from_secs(config.paxeer.poll_cadence_seconds),
        delayed_after_polls: config.paxeer.delayed_after_polls,
    };
    Ok(paxeer_tracker_config)
}

fn configured_key(value: &str, label: &str) -> Result<[u8; 32], String> {
    let key = parse_hex32(value).map_err(|_| format!("{label} key rejected"))?;
    if key == [0; 32] {
        return Err(format!("{label} key rejected"));
    }
    Ok(key)
}

fn secret_text(path: &PathBuf) -> Result<String, String> {
    let bytes = SecretFile::new(path)
        .map_err(|_| format!("secret file {} rejected", path.display()))?
        .read()
        .map_err(|_| format!("secret file {} unavailable", path.display()))?;
    let value = std::str::from_utf8(&bytes)
        .map_err(|_| format!("secret file {} is not text", path.display()))?
        .trim_end_matches(['\r', '\n'])
        .to_owned();
    if value.is_empty() {
        return Err(format!("secret file {} is empty", path.display()));
    }
    if value.len() > 4096 || value.bytes().any(|byte| matches!(byte, 0 | b'\r' | b'\n')) {
        return Err(format!(
            "secret file {} is not a bounded credential",
            path.display()
        ));
    }
    Ok(value)
}

fn serve(stream: TcpStream, listener: &Listener, state: &State) {
    match listener {
        Listener::Tls(acceptor) => {
            let Ok(mut tls) = acceptor.accept(stream) else {
                return;
            };
            exchange(&mut tls, state);
        }
        Listener::Plain => {
            let mut stream = stream;
            exchange(&mut stream, state);
        }
    }
}

fn exchange<S: Transport>(stream: &mut S, state: &State) {
    let response = read_request(stream)
        .and_then(|request| route(state, &request))
        .unwrap_or_else(|response| response);
    let _ = stream.write_all(&response.encode());
    let _ = stream.flush();
}

struct Request {
    method: String,
    path: String,
    headers: BTreeMap<String, String>,
    body: Vec<u8>,
}

struct Response {
    status: u16,
    body: Vec<u8>,
}

impl Response {
    fn encode(&self) -> Vec<u8> {
        let reason = match self.status {
            200 => "OK",
            201 => "Created",
            202 => "Accepted",
            400 => "Bad Request",
            401 => "Unauthorized",
            403 => "Forbidden",
            404 => "Not Found",
            409 => "Conflict",
            _ => "Service Unavailable",
        };
        let mut output = format!(
            "HTTP/1.1 {} {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
            self.status,
            reason,
            self.body.len()
        )
        .into_bytes();
        output.extend_from_slice(&self.body);
        output
    }
}

fn read_request<S: Transport>(stream: &mut S) -> Result<Request, Response> {
    stream
        .tcp()
        .set_read_timeout(Some(Duration::from_secs(10)))
        .map_err(|_| error(503, "request_timeout"))?;
    let mut bytes = Vec::new();
    let mut chunk = [0_u8; 4096];
    let boundary;
    loop {
        let read = stream
            .read(&mut chunk)
            .map_err(|_| error(400, "request_read_failed"))?;
        if read == 0 {
            return Err(error(400, "request_incomplete"));
        }
        bytes.extend_from_slice(&chunk[..read]);
        if bytes.len() > MAX_HEADERS {
            return Err(error(400, "headers_too_large"));
        }
        if let Some(found) = bytes.windows(4).position(|window| window == b"\r\n\r\n") {
            boundary = found;
            break;
        }
    }
    let headers =
        std::str::from_utf8(&bytes[..boundary]).map_err(|_| error(400, "headers_invalid"))?;
    let mut lines = headers.split("\r\n");
    let request_line = lines.next().ok_or_else(|| error(400, "request_invalid"))?;
    let mut parts = request_line.split_ascii_whitespace();
    let method = parts.next().ok_or_else(|| error(400, "request_invalid"))?;
    let path = parts.next().ok_or_else(|| error(400, "request_invalid"))?;
    if parts.next() != Some("HTTP/1.1") || parts.next().is_some() || !path.starts_with('/') {
        return Err(error(400, "request_invalid"));
    }
    let mut map = BTreeMap::new();
    for line in lines {
        let (name, value) = line
            .split_once(':')
            .ok_or_else(|| error(400, "headers_invalid"))?;
        let name = name.to_ascii_lowercase();
        if map.insert(name, value.trim().to_owned()).is_some() {
            return Err(error(400, "duplicate_header"));
        }
    }
    let length = map
        .get("content-length")
        .map_or(Ok(0), |value| value.parse::<usize>())
        .map_err(|_| error(400, "content_length_invalid"))?;
    if length > MAX_REQUEST_BYTES {
        return Err(error(400, "body_too_large"));
    }
    let method = method.to_owned();
    let path = path.to_owned();
    let body_offset = boundary.saturating_add(4);
    while bytes.len().saturating_sub(body_offset) < length {
        let read = stream
            .read(&mut chunk)
            .map_err(|_| error(400, "request_read_failed"))?;
        if read == 0 || bytes.len().saturating_add(read) > MAX_HEADERS + MAX_REQUEST_BYTES {
            return Err(error(400, "request_incomplete"));
        }
        bytes.extend_from_slice(&chunk[..read]);
    }
    Ok(Request {
        method,
        path,
        headers: map,
        body: bytes[body_offset..body_offset + length].to_vec(),
    })
}

fn route(state: &State, request: &Request) -> Result<Response, Response> {
    if request.method == "GET" && request.path == "/livez" {
        return Ok(ok(json!({ "live": true })));
    }
    if request.method == "GET" && request.path == "/readyz" {
        let health = state
            .journal
            .try_lock()
            .ok()
            .map(|journal| journal.health());
        let ready = health.as_ref().is_some_and(|health| health.ready);
        let body = json!({
            "ready": ready,
            "journal": health,
            "network_id": state.layerx.activity.network_id,
            "wire_version": state.layerx.activity.protocol_version,
            "external_custody": true,
            "external_custody_label": EXTERNAL_CUSTODY_LABEL,
            "readiness_scope": "local-journal",
            "build_revision": option_env!("LAYERX_RAMP_BUILD_REVISION"),
            "build_source_digest": option_env!("LAYERX_RAMP_BUILD_SOURCE_DIGEST"),
            "provider_contract": layerx_ramp_toolkit::PROVIDER_CONTRACT_VERSION,
            "compliance_contract": layerx_ramp_toolkit::COMPLIANCE_CONTRACT_VERSION,
            "paxeer_contract": layerx_ramp_toolkit::PAXEER_CONTRACT_VERSION
        });
        return Ok(if ready {
            ok(body)
        } else {
            json_response(503, body)
        });
    }
    if request.path == "/internal/v1/journal" && request.method == "GET" {
        require_operator(state, request)?;
        let journal = state
            .journal
            .try_lock()
            .map_err(|_| error(503, "journal_busy"))?;
        return Ok(ok(json!(journal.health())));
    }
    if request.path == "/internal/v1/journal/recover" && request.method == "POST" {
        require_operator(state, request)?;
        verified_recovery(state).map_err(|error| map_error(&error))?;
        return Ok(ok(json!({ "recovered": true })));
    }
    if request.method == "POST" {
        let journal = state
            .journal
            .try_lock()
            .map_err(|_| error(503, "journal_busy"))?;
        if !journal.health().ready {
            return Err(error(503, "journal_recovery_required"));
        }
    }
    if request.method == "POST" && request.path == "/v1/orders" {
        return create_order(state, request);
    }
    if request.method == "POST" && request.path == "/v1/provider-callbacks" {
        let callback: ProviderCallback =
            serde_json::from_slice(&request.body).map_err(|_| error(400, "callback_invalid"))?;
        let mut journal = state
            .journal
            .lock()
            .map_err(|_| error(503, "journal_unavailable"))?;
        if !journal.health().ready {
            return Err(error(503, "journal_recovery_required"));
        }
        let mut engine = engine(state, &mut journal);
        engine
            .provider_callback(&callback, &state.provider_callback_public_key, now())
            .map_err(|error| map_error(&error))?;
        return Ok(ok(json!({ "accepted": true })));
    }
    if let Some(digest) = request.path.strip_prefix("/v1/orders/") {
        if request.method != "GET" || digest.contains('/') {
            return Err(error(404, "not_found"));
        }
        let principal = authenticate(state, request)?;
        let digest = parse_hex32(digest).map_err(|_| error(400, "order_digest_invalid"))?;
        let journal = state
            .journal
            .lock()
            .map_err(|_| error(503, "journal_unavailable"))?;
        let snapshot = journal
            .order(&digest)
            .ok_or_else(|| error(404, "order_not_found"))?;
        if snapshot.order.customer != principal {
            return Err(error(404, "order_not_found"));
        }
        return Ok(ok(json!({
            "order_id": snapshot.order.order_id,
            "order": snapshot.order,
            "stage": snapshot.stage,
            "presentation": snapshot.presentation()
        })));
    }
    if request.method == "POST" && request.path == "/internal/v1/work" {
        return perform_work(state, request);
    }
    if request.method == "POST" && request.path == "/internal/v1/rebalances" {
        return rebalance(state, request);
    }
    if let Some(idempotency) = request.path.strip_prefix("/internal/v1/rebalances/") {
        if request.method != "GET" || idempotency.contains('/') {
            return Err(error(404, "not_found"));
        }
        require_operator(state, request)?;
        let idempotency =
            parse_hex32(idempotency).map_err(|_| error(400, "idempotency_key_invalid"))?;
        let journal = state
            .journal
            .lock()
            .map_err(|_| error(503, "journal_unavailable"))?;
        let snapshot = journal
            .paxeer(&idempotency)
            .ok_or_else(|| error(404, "rebalance_not_found"))?;
        return Ok(ok(rebalance_presentation(state, snapshot)));
    }
    Err(error(404, "not_found"))
}

fn create_order(state: &State, request: &Request) -> Result<Response, Response> {
    let principal = authenticate(state, request)?;
    let create: CreateOrder =
        serde_json::from_slice(&request.body).map_err(|_| error(400, "order_invalid"))?;
    let mut journal = state
        .journal
        .lock()
        .map_err(|_| error(503, "journal_unavailable"))?;
    if !journal.health().ready {
        return Err(error(503, "journal_recovery_required"));
    }
    if let Some(existing) = journal.order_by_id(&create.order_id) {
        if existing.order.customer != principal
            || existing.order.quote.quote_id != create.quote_id
            || existing.order.payer_grant != create.payer_grant
        {
            return Err(error(409, "order_id_conflict"));
        }
        return Ok(created(existing.presentation()));
    }
    let quote = state
        .quotes
        .get(&create.quote_id)
        .cloned()
        .ok_or_else(|| error(404, "quote_not_found"))?;
    let order = RampOrder::bind(create, quote, principal, state.operator.clone(), now())
        .map_err(|error| map_error(&error))?;
    let snapshot = journal
        .create_order(order, now())
        .map_err(|error| map_error(&error))?;
    Ok(created(snapshot.presentation()))
}

fn perform_work(state: &State, request: &Request) -> Result<Response, Response> {
    require_operator(state, request)?;
    let work: Work =
        serde_json::from_slice(&request.body).map_err(|_| error(400, "work_invalid"))?;
    let mut journal = state
        .journal
        .lock()
        .map_err(|_| error(503, "journal_unavailable"))?;
    if !journal.health().ready {
        return Err(error(503, "journal_recovery_required"));
    }
    if work.canonical_receive_payload.is_some() && !matches!(work.action, WorkAction::SubmitLayerx)
    {
        return Err(error(400, "work_invalid"));
    }
    if matches!(work.action, WorkAction::SubmitLayerx)
        && state.layerx.activity.protocol_version
            == layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION
        && journal
            .order(&work.order_digest)
            .is_some_and(|snapshot| snapshot.order.direction() == RampDirection::OffRamp)
        && work.canonical_receive_payload.is_none()
    {
        return Err(error(400, "native_payer_grant_authorization_required"));
    }
    let mut engine = engine(state, &mut journal);
    match work.action {
        WorkAction::Compliance => engine.evaluate_compliance(work.order_digest, now()),
        WorkAction::SubmitProvider => engine.submit_provider(work.order_digest, now()),
        WorkAction::ReconcileProvider => engine.reconcile_provider(work.order_digest, now()),
        WorkAction::SubmitLayerx => match work.account_sequence {
            Some(sequence) => match work.canonical_receive_payload.as_deref() {
                Some(payload) => {
                    engine.submit_native_receive(work.order_digest, payload, sequence, now())
                }
                None => engine.submit_layerx(work.order_digest, sequence, now()),
            },
            None => Err(RampError::InvalidOrder),
        },
        WorkAction::ResolveLayerx => engine.resolve_layerx(work.order_digest, now()),
    }
    .map_err(|error| map_error(&error))?;
    let snapshot = journal
        .order(&work.order_digest)
        .ok_or_else(|| error(404, "order_not_found"))?;
    Ok(ok(json!({
        "stage": snapshot.stage,
        "presentation": snapshot.presentation()
    })))
}

fn rebalance_presentation(
    state: &State,
    snapshot: &layerx_ramp_toolkit::journal::PaxeerSnapshot,
) -> serde_json::Value {
    json!({
        "settlement_domain": "paxeer",
        "external_custody_label": EXTERNAL_CUSTODY_LABEL,
        "operator_account": state.paxeer.operator_account,
        "idempotency_key": snapshot.idempotency_key,
        "asset": snapshot.asset,
        "amount": snapshot.amount,
        "operation_id": snapshot.operation_id.as_deref(),
        "transaction_hash": snapshot.transaction_hash.map(|hash| format!("0x{}", layerx_ramp_toolkit::clients::hex(&hash))),
        "status": snapshot.stage,
        "block_hash": snapshot.block_hash.map(|hash| format!("0x{}", layerx_ramp_toolkit::clients::hex(&hash))),
        "confirmations": snapshot.confirmations,
        "required_confirmations": state.paxeer_tracker_config.required_confirmations
    })
}

fn rebalance(state: &State, request: &Request) -> Result<Response, Response> {
    require_operator(state, request)?;
    let action: Rebalance =
        serde_json::from_slice(&request.body).map_err(|_| error(400, "rebalance_invalid"))?;
    let mut journal = state
        .journal
        .lock()
        .map_err(|_| error(503, "journal_unavailable"))?;
    if !journal.health().ready {
        return Err(error(503, "journal_recovery_required"));
    }
    let mut rebalancer = InventoryRebalancer {
        journal: &mut journal,
        custody: &state.paxeer,
    };
    match action {
        Rebalance::Submit {
            asset,
            amount,
            idempotency_key,
        } => {
            rebalancer
                .submit(asset, amount, idempotency_key, now())
                .map_err(|error| map_error(&error))?;
            let persisted = journal
                .paxeer(&idempotency_key)
                .ok_or_else(|| error(404, "rebalance_not_found"))?;
            Ok(accepted(rebalance_presentation(state, persisted)))
        }
        Rebalance::Poll {
            idempotency_key,
            operation_id,
            transaction_hash,
        } => {
            let transaction = TransactionHash::from_hex(&transaction_hash)
                .map_err(|_| error(400, "transaction_hash_invalid"))?;
            let mut trackers = state
                .paxeer_trackers
                .lock()
                .map_err(|_| error(503, "paxeer_tracker_unavailable"))?;
            if let std::collections::btree_map::Entry::Vacant(entry) =
                trackers.entry(idempotency_key)
            {
                let tracker =
                    FinalityTracker::new(state.paxeer_tracker_config.clone(), transaction)
                        .map_err(|_| error(503, "paxeer_tracker_unavailable"))?;
                entry.insert(tracker);
            }
            let tracker = trackers
                .get_mut(&idempotency_key)
                .ok_or_else(|| error(503, "paxeer_tracker_unavailable"))?;
            if tracker.transaction() != transaction {
                return Err(error(409, "rebalance_transaction_conflict"));
            }
            let report = rebalancer
                .poll(idempotency_key, &operation_id, tracker, now())
                .map_err(|error| map_error(&error))?;
            let persisted = journal
                .paxeer(&idempotency_key)
                .ok_or_else(|| error(404, "rebalance_not_found"))?;
            let mut presentation = rebalance_presentation(state, persisted);
            presentation["chain"] = json!(chain_signal(&report.signal()));
            presentation["endpoints"] = json!(endpoint_signal(&report.endpoint()));
            Ok(ok(presentation))
        }
        Rebalance::Reconcile { idempotency_key } => {
            rebalancer
                .reconcile(idempotency_key, now())
                .map_err(|error| map_error(&error))?;
            let persisted = journal
                .paxeer(&idempotency_key)
                .ok_or_else(|| error(404, "rebalance_not_found"))?;
            Ok(accepted(rebalance_presentation(state, persisted)))
        }
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Work {
    order_digest: [u8; 32],
    action: WorkAction,
    account_sequence: Option<u64>,
    canonical_receive_payload: Option<Vec<u8>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "snake_case")]
enum WorkAction {
    Compliance,
    SubmitProvider,
    ReconcileProvider,
    SubmitLayerx,
    ResolveLayerx,
}

#[derive(Deserialize)]
#[serde(tag = "action", rename_all = "snake_case", deny_unknown_fields)]
enum Rebalance {
    Submit {
        asset: [u8; 32],
        amount: u128,
        idempotency_key: [u8; 32],
    },
    Reconcile {
        idempotency_key: [u8; 32],
    },
    Poll {
        idempotency_key: [u8; 32],
        operation_id: String,
        transaction_hash: String,
    },
}

fn authenticate(
    state: &State,
    request: &Request,
) -> Result<layerx_ramp_toolkit::AuthenticatedPrincipal, Response> {
    state
        .identity
        .authenticate(
            request
                .headers
                .get("authorization")
                .ok_or_else(|| error(401, "authentication_required"))?,
            now(),
        )
        .map_err(|_| error(401, "authentication_refused"))
}

fn require_operator(state: &State, request: &Request) -> Result<(), Response> {
    let presented = request
        .headers
        .get("authorization")
        .and_then(|value| value.strip_prefix("Bearer "))
        .ok_or_else(|| error(401, "operator_authentication_required"))?;
    if presented
        .as_bytes()
        .ct_eq(state.operator_control_token.as_bytes())
        .unwrap_u8()
        != 1
    {
        return Err(error(403, "operator_authentication_refused"));
    }
    Ok(())
}

fn verified_recovery(state: &State) -> Result<(), RampError> {
    let mut journal = state.journal.lock().map_err(|_| RampError::Journal)?;
    journal.recover_verified(|projection| {
        layerx_ramp_toolkit::clients::verify_recovery_settlement(
            projection,
            &state.operator,
            &state.provider,
            &state.layerx,
            &state.paxeer,
            &state.paxeer_tracker_config,
        )
    })
}

fn engine<'a>(state: &'a State, journal: &'a mut Journal) -> RampEngine<'a> {
    RampEngine {
        journal,
        compliance: &state.compliance,
        provider: &state.provider,
        layerx: &state.layerx,
        registry: &state.registry,
        worker_id: &state.worker_id,
        lease_seconds: state.lease_seconds,
    }
}

fn reconcile_loop(state: &State, cadence: Duration) {
    loop {
        thread::sleep(cadence);
        let Ok(mut journal) = state.journal.lock() else {
            continue;
        };
        if !journal.health().ready {
            continue;
        }
        let due = journal.orders();
        for snapshot in due {
            let digest = snapshot.order.order_digest;
            let observed_at = now();
            if matches!(
                snapshot.stage,
                WorkflowStage::ProviderSubmissionPlanned
                    | WorkflowStage::ProviderSubmittedUnknown
                    | WorkflowStage::ProviderPending
                    | WorkflowStage::LayerxSubmissionPlanned
                    | WorkflowStage::LayerxSubmittedUnknown
                    | WorkflowStage::LayerxPending
            ) && snapshot
                .evidence
                .retry_at
                .is_some_and(|retry_at| retry_at > observed_at)
            {
                continue;
            }
            let mut worker = engine(state, &mut journal);
            let result = match snapshot.stage {
                WorkflowStage::ProviderSubmissionPlanned
                | WorkflowStage::ProviderSubmittedUnknown
                | WorkflowStage::ProviderPending => worker.reconcile_provider(digest, observed_at),
                WorkflowStage::LayerxSubmissionPlanned
                | WorkflowStage::LayerxSubmittedUnknown
                | WorkflowStage::LayerxPending => worker.resolve_layerx(digest, observed_at),
                WorkflowStage::ProviderSettled | WorkflowStage::LayerxVerified => {
                    worker.finish_if_complete(digest, observed_at)
                }
                _ => Ok(()),
            };
            if result.is_err() {
                eprintln!("ramp reconciliation retained unresolved state");
            }
        }
    }
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |duration| duration.as_secs())
}

fn chain_signal(signal: &ChainSignal) -> serde_json::Value {
    match signal {
        ChainSignal::Progressing => json!({ "state": "progressing" }),
        ChainSignal::Delayed {
            stalled_polls,
            threshold,
            stalled_for,
            delayed_after,
        } => json!({
            "state": "delayed",
            "stalled_polls": stalled_polls,
            "threshold": threshold,
            "stalled_for_seconds": stalled_for.as_secs(),
            "delayed_after_seconds": delayed_after.as_secs()
        }),
        ChainSignal::Unreachable { .. } => json!({ "state": "unreachable" }),
    }
}

fn endpoint_signal(signal: &EndpointSignal) -> serde_json::Value {
    match signal {
        EndpointSignal::Serving => json!({ "state": "serving" }),
        EndpointSignal::Degraded { failovers } => {
            json!({ "state": "degraded", "failover_count": failovers.len() })
        }
        EndpointSignal::Unreachable { .. } => json!({ "state": "unreachable" }),
    }
}

fn ok(value: impl Serialize) -> Response {
    json_response(200, value)
}

fn created(value: impl Serialize) -> Response {
    json_response(201, value)
}

fn accepted(value: impl Serialize) -> Response {
    json_response(202, value)
}

fn error(status: u16, code: &str) -> Response {
    json_response(status, json!({ "error": code }))
}

fn json_response(status: u16, value: impl Serialize) -> Response {
    match serde_json::to_vec(&value) {
        Ok(body) => Response { status, body },
        Err(_) => Response {
            status: 503,
            body: b"{\"error\":\"encoding_failed\"}".to_vec(),
        },
    }
}

fn map_error(error_value: &RampError) -> Response {
    match error_value {
        RampError::InvalidOrder | RampError::InvalidPrincipal | RampError::OrderBinding => {
            error(400, "request_refused")
        }
        RampError::Conflict | RampError::IllegalTransition | RampError::LeaseHeld => {
            error(409, "operation_conflict")
        }
        RampError::PayerGrantRequired | RampError::Intent | RampError::ReceiptMismatch => {
            error(400, "layerx_binding_refused")
        }
        RampError::Compliance => error(503, "compliance_unavailable"),
        RampError::Provider => error(503, "provider_unavailable"),
        RampError::Layerx | RampError::Receipt(_) => error(503, "layerx_unavailable"),
        RampError::Paxeer => error(503, "paxeer_unavailable"),
        RampError::Journal | RampError::Configuration => error(503, "service_unavailable"),
    }
}

#[must_use]
pub const fn platform_reference_ramp() -> &'static str {
    "receipt-backed-reference-market-maker"
}

#[cfg(test)]
mod boundary_tests {
    use std::sync::Arc;
    use std::sync::atomic::Ordering;

    use super::ConnectionGate;

    #[test]
    fn connection_gate_refuses_work_above_the_bound_and_releases_capacity() {
        let gate = Arc::new(ConnectionGate::new(2));
        let Some(first) = gate.try_acquire() else {
            panic!("first permit was refused");
        };
        let Some(second) = gate.try_acquire() else {
            panic!("second permit was refused");
        };
        assert!(gate.try_acquire().is_none());
        drop(first);
        let Some(replacement) = gate.try_acquire() else {
            panic!("released capacity was not reusable");
        };
        assert_eq!(gate.active.load(Ordering::Acquire), 2);
        drop(second);
        drop(replacement);
        assert_eq!(gate.active.load(Ordering::Acquire), 0);
    }
}
