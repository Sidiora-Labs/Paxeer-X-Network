mod human;
mod protected;
mod trust;

use layerx_client::lni::handshake::{perform, Handshake, HandshakeConfig, HandshakeError};
use layerx_client::lni::refusal::decode_core_refusal;
use layerx_client::lni::schema::{decode_envelope, encode_envelope, Capability, Envelope, Version};
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_platform_authority::{
    authorized_batch_by_activity, hex, receipt_locator, EvidenceRefusal,
};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION as PROTOCOL_VERSION;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::server::WebPkiClientVerifier;
use rustls::{RootCertStore, ServerConfig, ServerConnection, StreamOwned};
use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::io::{Read, Write};
use std::net::{Ipv6Addr, SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};
use subtle::ConstantTimeEq;
use zeroize::{Zeroize, Zeroizing};

const MAX_REQUEST_BYTES: usize = 16 * 1024;
const MAX_REPLICA_RESPONSE_BYTES: usize = 256 * 1024;
const MAX_TLS_FILE_BYTES: u64 = 64 * 1024;
const IO_TIMEOUT: Duration = Duration::from_secs(8);
const REPLICA_TIMEOUT: Duration = Duration::from_secs(8);
const READINESS_TIMEOUT: Duration = Duration::from_secs(2);
const MAX_CONNECTIONS: usize = 128;
const MAX_REQUESTS_PER_CONNECTION: usize = 64;
const MAX_LNI_CONNECTIONS: usize = 16;
const LNI_FRAME_BYTES: usize = 1_212_416;
const RECEIPT_LOOKUP_REQUEST: u16 = 5;
const RECEIPT_LOOKUP_RESPONSE: u16 = 6;
const ERROR_RESPONSE: u16 = 25;
const ZERO_HEX32: &str = "0000000000000000000000000000000000000000000000000000000000000000";
static ACTIVE_CONNECTIONS: AtomicUsize = AtomicUsize::new(0);
static CORRELATION: AtomicU64 = AtomicU64::new(1);

const USAGE: &str = "layerx-receipt-authority serves verified authorised-batch facts over TLS.

Routes (GET only):
  /livez
  /readyz                                              {ready, network_id, protocol_network_id, wire_version}
  /v1/authorized-batches/by-activity/{activity_id}     bearer required
  /v1/authorized-batches/wait-by-activity/{activity_id}
                                                       bearer required; waits for a publication notification
  /internal/v1/activities/{activity_id}/authority      bearer required
  /v1/batches/{batch_id}/receipt-authority?receipt_digest={digest}
                                                       bearer required, relayed to the replica unchanged

Environment:
  LAYERX_AUTHORITY_LISTEN                    listen address, default 0.0.0.0:9445
  LAYERX_AUTHORITY_TLS_CERT_DER              server certificate (DER)
  LAYERX_AUTHORITY_TLS_KEY_DER               server private key (PKCS#8 DER)
  LAYERX_AUTHORITY_CLIENT_CA_DER             optional; when set a presented client certificate must chain to it
  LAYERX_AUTHORITY_TOKEN_FILES               colon-separated files, one bearer token each (gateway, registry, webhooks)
  LAYERX_AUTHORITY_REPLICA_URL               loopback http://127.0.0.1:PORT of the independent receipt-authority replica
  LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE bearer token the replica requires
  LAYERX_AUTHORITY_REPLICA_ID                64-hex replica identity every evidence document must carry
  LAYERX_AUTHORITY_LNI_SOCKET                LNI unix socket used as the receipt source (ReceiptLookup by activity id);
                                             this service takes receipt bytes from the LNI and never an HTTP receipt URL
  LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID       numeric protocol network id expected in the LNI handshake
  LAYERX_AUTHORITY_NETWORK_ID                deployment network identifier echoed in every answer
  LAYERX_AUTHORITY_WIRE_VERSION              wire version echoed in every answer, must be the built protocol version (default 3)
  LAYERX_AUTHORITY_SEQUENCER_ID              64-hex sequencer identity pinned for header verification
  LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY      64-hex sequencer public key pinned for header and receipt signatures
  LAYERX_AUTHORITY_GENESIS_TRUST             protected native genesis trust artifact; requires HANDOVER_FINALITY
  LAYERX_AUTHORITY_HANDOVER_FINALITY         protected independent Paxeer verification policy
  LAYERX_AUTHORITY_FIRST_BATCH               first authorised batch number
  LAYERX_AUTHORITY_LAST_BATCH                last authorised batch number
";

struct Config {
    listen: SocketAddr,
    tls: Arc<ServerConfig>,
    tokens: Vec<Zeroizing<String>>,
    human: Option<human::Human>,
    replica_address: SocketAddr,
    replica_host: String,
    replica_token: Zeroizing<String>,
    replica_id: [u8; 32],
    lni_socket: PathBuf,
    lni_gate: ConnectionGate,
    protocol_network_id: u32,
    network_id: String,
    wire_version: String,
    authorization: SequencerAuthorization,
    sequencer_public_key: [u8; 32],
    trust: Option<trust::Trust>,
}

struct Request {
    method: String,
    path: String,
    query: Option<String>,
    headers: BTreeMap<String, String>,
}

struct Response {
    status: u16,
    body: Vec<u8>,
    retry_after: Option<u64>,
    readiness_headers: Option<[&'static str; 2]>,
}

enum ReplicaAnswer {
    Status(u16, Vec<u8>),
    Unavailable,
}

fn valid_identifier(value: &str, max: usize) -> bool {
    !value.is_empty()
        && value.len() <= max
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.'))
}

fn read_secret_file(path: &str, name: &str) -> Result<Zeroizing<String>, String> {
    let mut value = fs::read_to_string(path).map_err(|error| format!("{name}: {error}"))?;
    while matches!(value.as_bytes().last(), Some(b'\n' | b'\r')) {
        value.pop();
    }
    if value.is_empty()
        || value.len() > 4096
        || value.bytes().any(|byte| !(0x21..=0x7e).contains(&byte))
    {
        value.zeroize();
        return Err(format!(
            "{name} does not contain a bounded printable secret"
        ));
    }
    Ok(Zeroizing::new(value))
}

fn read_secret(variable: &str) -> Result<Zeroizing<String>, String> {
    let path = env::var(variable).map_err(|_| format!("{variable} is required"))?;
    read_secret_file(&path, variable)
}

fn read_bounded(variable: &str) -> Result<Vec<u8>, String> {
    let path = env::var(variable).map_err(|_| format!("{variable} is required"))?;
    let metadata = fs::metadata(&path).map_err(|error| format!("{variable}: {error}"))?;
    if !metadata.is_file() || metadata.len() == 0 || metadata.len() > MAX_TLS_FILE_BYTES {
        return Err(format!("{variable} is not a bounded regular file"));
    }
    fs::read(&path).map_err(|error| format!("{variable}: {error}"))
}

fn parse_hex32(variable: &str) -> Result<[u8; 32], String> {
    let value = env::var(variable).map_err(|_| format!("{variable} is required"))?;
    hex::decode32(&value).map_err(|_| format!("{variable} must be 64 hexadecimal characters"))
}

fn parse_u64(variable: &str) -> Result<u64, String> {
    env::var(variable)
        .map_err(|_| format!("{variable} is required"))?
        .parse::<u64>()
        .map_err(|_| format!("{variable} must be an integer"))
}

fn loopback_http(endpoint: &str) -> Option<(String, SocketAddr)> {
    let authority = endpoint.strip_prefix("http://")?;
    let authority = authority.strip_suffix('/').unwrap_or(authority);
    if authority.contains(['/', '?', '#', '@', '\\']) {
        return None;
    }
    let (host, port) = authority.rsplit_once(':')?;
    let port = port.parse::<u16>().ok().filter(|port| *port != 0)?;
    let address = match host {
        "127.0.0.1" | "localhost" => SocketAddr::from(([127, 0, 0, 1], port)),
        "[::1]" => SocketAddr::from((Ipv6Addr::LOCALHOST, port)),
        _ => return None,
    };
    Some((format!("{host}:{port}"), address))
}

fn server_tls_config() -> Result<Arc<ServerConfig>, String> {
    rustls::crypto::ring::default_provider()
        .install_default()
        .map_err(|_| "failed to install the authority TLS provider".to_owned())?;
    let certificate = CertificateDer::from(read_bounded("LAYERX_AUTHORITY_TLS_CERT_DER")?);
    let private_key = PrivateKeyDer::try_from(read_bounded("LAYERX_AUTHORITY_TLS_KEY_DER")?)
        .map_err(|_| "LAYERX_AUTHORITY_TLS_KEY_DER is not a PKCS#8 private key".to_owned())?;
    let builder = ServerConfig::builder();
    let config = if env::var_os("LAYERX_AUTHORITY_CLIENT_CA_DER").is_some() {
        let client_ca = CertificateDer::from(read_bounded("LAYERX_AUTHORITY_CLIENT_CA_DER")?);
        let mut roots = RootCertStore::empty();
        roots
            .add(client_ca)
            .map_err(|_| "LAYERX_AUTHORITY_CLIENT_CA_DER is not a CA certificate".to_owned())?;
        let verifier = WebPkiClientVerifier::builder(roots.into())
            .allow_unauthenticated()
            .build()
            .map_err(|_| "authority client certificate verifier is invalid".to_owned())?;
        builder
            .with_client_cert_verifier(verifier)
            .with_single_cert(vec![certificate], private_key)
    } else {
        builder
            .with_no_client_auth()
            .with_single_cert(vec![certificate], private_key)
    }
    .map_err(|_| "authority TLS identity is invalid".to_owned())?;
    Ok(Arc::new(config))
}

fn config() -> Result<Config, String> {
    let listen = env::var("LAYERX_AUTHORITY_LISTEN")
        .unwrap_or_else(|_| "0.0.0.0:9445".to_owned())
        .parse::<SocketAddr>()
        .map_err(|_| "LAYERX_AUTHORITY_LISTEN must be a socket address".to_owned())?;
    let tls = server_tls_config()?;
    let token_files = env::var("LAYERX_AUTHORITY_TOKEN_FILES")
        .map_err(|_| "LAYERX_AUTHORITY_TOKEN_FILES is required")?;
    let mut tokens = Vec::new();
    for path in token_files.split(':').filter(|path| !path.is_empty()) {
        tokens.push(read_secret_file(path, "LAYERX_AUTHORITY_TOKEN_FILES")?);
    }
    if tokens.is_empty() {
        return Err("LAYERX_AUTHORITY_TOKEN_FILES names no token file".to_owned());
    }
    let (replica_host, replica_address) = env::var("LAYERX_AUTHORITY_REPLICA_URL")
        .ok()
        .as_deref()
        .and_then(loopback_http)
        .ok_or_else(|| {
            "LAYERX_AUTHORITY_REPLICA_URL must be a loopback http://host:port endpoint".to_owned()
        })?;
    let replica_token = read_secret("LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE")?;
    let replica_id = parse_hex32("LAYERX_AUTHORITY_REPLICA_ID")?;
    if replica_id == [0; 32] {
        return Err("LAYERX_AUTHORITY_REPLICA_ID must not be zero".to_owned());
    }
    let lni_socket = env::var("LAYERX_AUTHORITY_LNI_SOCKET")
        .map(PathBuf::from)
        .map_err(|_| "LAYERX_AUTHORITY_LNI_SOCKET is required".to_owned())?;
    if !lni_socket.is_absolute() {
        return Err("LAYERX_AUTHORITY_LNI_SOCKET must be an absolute path".to_owned());
    }
    let protocol_network_id = parse_u64("LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID")?;
    let protocol_network_id = u32::try_from(protocol_network_id)
        .ok()
        .filter(|value| *value != 0)
        .ok_or_else(|| {
            "LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID must be a non-zero 32-bit integer".to_owned()
        })?;
    let network_id = env::var("LAYERX_AUTHORITY_NETWORK_ID")
        .map_err(|_| "LAYERX_AUTHORITY_NETWORK_ID is required")?;
    let wire_version =
        env::var("LAYERX_AUTHORITY_WIRE_VERSION").unwrap_or_else(|_| PROTOCOL_VERSION.to_string());
    if !valid_identifier(&network_id, 64) || !valid_identifier(&wire_version, 32) {
        return Err(
            "LAYERX_AUTHORITY_NETWORK_ID or LAYERX_AUTHORITY_WIRE_VERSION is invalid".to_owned(),
        );
    }
    if wire_version.parse::<u16>().ok() != Some(PROTOCOL_VERSION) {
        return Err("LAYERX_AUTHORITY_WIRE_VERSION is not the built protocol version".to_owned());
    }
    let sequencer_id = parse_hex32("LAYERX_AUTHORITY_SEQUENCER_ID")?;
    let sequencer_public_key = parse_hex32("LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY")?;
    if sequencer_id == [0; 32] || sequencer_public_key == [0; 32] {
        return Err("sequencer identity and public key must not be zero".to_owned());
    }
    let first_batch = parse_u64("LAYERX_AUTHORITY_FIRST_BATCH")?;
    let last_batch = parse_u64("LAYERX_AUTHORITY_LAST_BATCH")?;
    if first_batch == 0 || last_batch < first_batch {
        return Err(
            "LAYERX_AUTHORITY_FIRST_BATCH..LAYERX_AUTHORITY_LAST_BATCH is not a batch range"
                .to_owned(),
        );
    }
    Ok(Config {
        trust: trust::Trust::load(protocol_network_id, sequencer_id, sequencer_public_key)?,
        listen,
        tls,
        human: human::Human::load(&tokens)?,
        tokens,
        replica_address,
        replica_host,
        replica_token,
        replica_id,
        lni_socket,
        lni_gate: ConnectionGate::new(MAX_LNI_CONNECTIONS),
        protocol_network_id,
        network_id,
        wire_version,
        authorization: SequencerAuthorization::new(
            sequencer_id,
            sequencer_public_key,
            first_batch,
            last_batch,
        ),
        sequencer_public_key,
    })
}

fn read_http_message(stream: &mut impl Read) -> Result<Request, String> {
    let mut bytes = Vec::with_capacity(2048);
    let mut chunk = [0_u8; 2048];
    let header_end = loop {
        let count = stream.read(&mut chunk).map_err(|error| error.to_string())?;
        if count == 0 || bytes.len().saturating_add(count) > MAX_REQUEST_BYTES {
            return Err("HTTP message is empty or exceeds its bound".to_owned());
        }
        bytes.extend_from_slice(&chunk[..count]);
        if let Some(position) = bytes.windows(4).position(|window| window == b"\r\n\r\n") {
            break position + 4;
        }
    };
    let source = std::str::from_utf8(&bytes[..header_end])
        .map_err(|_| "HTTP headers are not UTF-8".to_owned())?;
    let mut lines = source.split("\r\n");
    let start = lines
        .next()
        .ok_or_else(|| "HTTP start line is missing".to_owned())?
        .to_owned();
    let mut headers = BTreeMap::new();
    let mut content_length = 0_usize;
    for line in lines.filter(|line| !line.is_empty()) {
        let (name, value) = line
            .split_once(':')
            .ok_or_else(|| "HTTP header is malformed".to_owned())?;
        let name = name.trim().to_ascii_lowercase();
        let value = value.trim().to_owned();
        if headers.contains_key(&name) {
            return Err("duplicate HTTP header".to_owned());
        }
        if name == "transfer-encoding" {
            return Err("transfer-encoded messages are not accepted".to_owned());
        }
        if name == "content-length" {
            content_length = value
                .parse::<usize>()
                .map_err(|_| "content length is invalid".to_owned())?;
        }
        headers.insert(name, value);
    }
    if content_length != 0 {
        return Err("request bodies are not accepted".to_owned());
    }
    if bytes.len() != header_end {
        return Err("request carries unexpected bytes".to_owned());
    }
    let mut parts = start.split_whitespace();
    let method = parts
        .next()
        .ok_or_else(|| "request method is missing".to_owned())?
        .to_owned();
    let target = parts
        .next()
        .ok_or_else(|| "request target is missing".to_owned())?;
    if parts.next() != Some("HTTP/1.1") || parts.next().is_some() || !target.starts_with('/') {
        return Err("request line is invalid".to_owned());
    }
    if !headers.contains_key("host") {
        return Err("HTTP/1.1 Host header is required".to_owned());
    }
    let (path, query) = target
        .split_once('?')
        .map_or((target.to_owned(), None), |(path, query)| {
            (path.to_owned(), Some(query.to_owned()))
        });
    if path.contains('#')
        || query
            .as_deref()
            .is_some_and(|query| query.contains(['?', '#']))
    {
        return Err("request target is invalid".to_owned());
    }
    Ok(Request {
        method,
        path,
        query,
        headers,
    })
}

fn json(status: u16, value: &serde_json::Value) -> Response {
    Response {
        status,
        body: value.to_string().into_bytes(),
        retry_after: None,
        readiness_headers: None,
    }
}

fn refusal(status: u16, code: &str, retry_after: Option<u64>) -> Response {
    let body = retry_after.map_or_else(
        || serde_json::json!({ "error": { "code": code, "retry": "never" } }),
        |seconds| {
            serde_json::json!({ "error": { "code": code, "retry": "after", "retry_after_seconds": seconds } })
        },
    );
    Response {
        status,
        body: body.to_string().into_bytes(),
        retry_after,
        readiness_headers: None,
    }
}

fn write_response(
    stream: &mut impl Write,
    response: &Response,
    keep_alive: bool,
) -> Result<(), String> {
    let reason = match response.status {
        200 => "OK",
        400 => "Bad Request",
        401 => "Unauthorized",
        403 => "Forbidden",
        404 => "Not Found",
        405 => "Method Not Allowed",
        502 => "Bad Gateway",
        _ => "Service Unavailable",
    };
    let retry = response.retry_after.map_or(String::new(), |seconds| {
        format!("Retry-After: {seconds}\r\n")
    });
    let diagnostics = response
        .readiness_headers
        .map_or_else(String::new, |[replica, lni]| {
            format!("X-LayerX-Authority-Replica: {replica}\r\nX-LayerX-Authority-LNI: {lni}\r\n")
        });
    let connection = if keep_alive { "keep-alive" } else { "close" };
    let head = format!(
        "HTTP/1.1 {} {reason}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nCache-Control: no-store\r\n{retry}{diagnostics}Connection: {connection}\r\n\r\n",
        response.status,
        response.body.len()
    );
    stream
        .write_all(head.as_bytes())
        .and_then(|()| stream.write_all(&response.body))
        .and_then(|()| stream.flush())
        .map_err(|error| error.to_string())
}

fn authenticate(config: &Config, request: &Request) -> Result<(), Response> {
    let Some(presented) = request
        .headers
        .get("authorization")
        .and_then(|value| value.strip_prefix("Bearer "))
    else {
        return Err(refusal(401, "identity_required", None));
    };
    if presented.is_empty() || presented.len() > 4096 {
        return Err(refusal(401, "identity_required", None));
    }
    let accepted = config.tokens.iter().any(|token| {
        token.len() == presented.len() && bool::from(token.as_bytes().ct_eq(presented.as_bytes()))
    });
    if accepted {
        Ok(())
    } else {
        Err(refusal(401, "identity_required", None))
    }
}

fn replica_get(config: &Config, path: &str) -> ReplicaAnswer {
    match replica_exchange(config, path) {
        Ok(answer) => answer,
        Err(error) => {
            eprintln!("layerx-receipt-authority replica GET failed: {error}");
            ReplicaAnswer::Unavailable
        }
    }
}

fn replica_exchange(config: &Config, path: &str) -> Result<ReplicaAnswer, String> {
    replica_exchange_until(config, path, Instant::now() + REPLICA_TIMEOUT)
}

fn replica_exchange_until(
    config: &Config,
    path: &str,
    deadline: Instant,
) -> Result<ReplicaAnswer, String> {
    let remaining = || {
        deadline
            .checked_duration_since(Instant::now())
            .filter(|duration| !duration.is_zero())
            .ok_or_else(|| "replica deadline elapsed".to_owned())
    };
    let mut stream = TcpStream::connect_timeout(&config.replica_address, remaining()?)
        .map_err(|error| format!("connect: {error}"))?;
    stream
        .set_read_timeout(Some(remaining()?))
        .and_then(|()| {
            stream.set_write_timeout(Some(deadline.saturating_duration_since(Instant::now())))
        })
        .map_err(|error| format!("timeout: {error}"))?;
    let request = format!(
        "GET {path} HTTP/1.1\r\nHost: {}\r\nAuthorization: Bearer {}\r\nAccept: application/json\r\nConnection: close\r\n\r\n",
        config.replica_host,
        config.replica_token.as_str()
    );
    stream
        .write_all(request.as_bytes())
        .map_err(|error| format!("write: {error}"))?;
    let mut raw = Vec::new();
    let mut chunk = [0_u8; 4096];
    loop {
        stream
            .set_read_timeout(Some(remaining()?))
            .map_err(|error| format!("timeout: {error}"))?;
        let count = stream
            .read(&mut chunk)
            .map_err(|error| format!("read: {error}"))?;
        if count == 0 {
            break;
        }
        raw.extend_from_slice(&chunk[..count]);
        if raw.len() > MAX_REPLICA_RESPONSE_BYTES {
            return Err("response exceeds the replica document bound".to_owned());
        }
    }
    let head_end = raw
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or_else(|| "response lacks a header terminator".to_owned())?;
    let head = std::str::from_utf8(&raw[..head_end])
        .map_err(|_| "response head is not UTF-8".to_owned())?;
    let mut lines = head.split("\r\n");
    let status_line = lines
        .next()
        .ok_or_else(|| "response lacks a status line".to_owned())?;
    let status = status_line
        .strip_prefix("HTTP/1.1 ")
        .or_else(|| status_line.strip_prefix("HTTP/1.0 "))
        .and_then(|rest| rest.split(' ').next())
        .and_then(|code| code.parse::<u16>().ok())
        .filter(|code| (100..=599).contains(code))
        .ok_or_else(|| "response status line is malformed".to_owned())?;
    let mut content_length = None;
    for line in lines {
        let (name, value) = line
            .split_once(':')
            .ok_or_else(|| "response header is malformed".to_owned())?;
        if name.trim().eq_ignore_ascii_case("content-length") {
            let length = value
                .trim()
                .parse::<usize>()
                .map_err(|_| "response content length is malformed".to_owned())?;
            if content_length.replace(length).is_some() {
                return Err("response repeats content length".to_owned());
            }
        }
        if name.trim().eq_ignore_ascii_case("transfer-encoding") {
            return Err("response uses a transfer encoding".to_owned());
        }
    }
    let body = raw[head_end + 4..].to_vec();
    match content_length {
        Some(length) if length == body.len() => Ok(ReplicaAnswer::Status(status, body)),
        Some(_) => Err("response body length disagrees with its header".to_owned()),
        None if body.is_empty() => Ok(ReplicaAnswer::Status(status, body)),
        None => Err("response body lacks a content length".to_owned()),
    }
}

enum ReceiptSource {
    Found(Vec<u8>),
    Unknown,
    Unavailable(String),
    KeyMismatch,
}

#[derive(Clone, Copy, Debug, serde::Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
enum DependencyReason {
    Ready,
    Empty,
    Unavailable,
    Timeout,
    IdentityMismatch,
    KeyMismatch,
    ProtocolIncompatible,
    CapabilityMissing,
    ConnectionLimit,
}

#[derive(Clone, Copy, serde::Serialize)]
struct DependencyReadiness {
    ready: bool,
    reason: DependencyReason,
}

impl DependencyReadiness {
    fn from_result(result: Result<DependencyReason, DependencyReason>) -> Self {
        match result {
            Ok(reason) => Self {
                ready: true,
                reason,
            },
            Err(reason) => Self {
                ready: false,
                reason,
            },
        }
    }
}

fn validate_receipt_handshake(
    handshake: &Handshake,
    pinned_key: Option<[u8; 32]>,
    wait_publication: bool,
) -> Result<(), DependencyReason> {
    if !handshake.capabilities().contains(Capability::ReceiptLookup) {
        return Err(DependencyReason::CapabilityMissing);
    }
    if wait_publication && handshake.node().interface_version.minor < Version::V1_5.minor {
        return Err(DependencyReason::ProtocolIncompatible);
    }
    if pinned_key.is_some_and(|key| handshake.node().authorised_sequencer_key != key) {
        return Err(DependencyReason::KeyMismatch);
    }
    Ok(())
}

fn receipt_connection(
    config: &Config,
    wait_publication: bool,
    deadline: Instant,
) -> Result<(Uds, Handshake), DependencyReason> {
    use layerx_client::lni::transport::TransportError;
    let transport_error = |error| match error {
        TransportError::Deadline => DependencyReason::Timeout,
        TransportError::ConnectionLimit => DependencyReason::ConnectionLimit,
        _ => DependencyReason::Unavailable,
    };
    let limits = trust::limits(deadline).map_err(|()| DependencyReason::Timeout)?;
    let mut transport =
        Uds::connect(&config.lni_socket, &config.lni_gate, limits).map_err(transport_error)?;
    let handshake = perform(
        &mut transport,
        &HandshakeConfig {
            built_interface_version: Version::V1_5,
            expected_protocol_version: PROTOCOL_VERSION,
            expected_network_id: config.protocol_network_id,
        },
        None,
    )
    .map_err(|error| match error {
        HandshakeError::Transport(error) => transport_error(error),
        HandshakeError::Network { .. } => DependencyReason::IdentityMismatch,
        HandshakeError::ProtocolVersion { .. } | HandshakeError::InterfaceIncompatible { .. } => {
            DependencyReason::ProtocolIncompatible
        }
        _ => DependencyReason::Unavailable,
    })?;
    if Instant::now() >= deadline {
        return Err(DependencyReason::Timeout);
    }
    validate_receipt_handshake(
        &handshake,
        config
            .trust
            .is_none()
            .then_some(config.sequencer_public_key),
        wait_publication,
    )?;
    Ok((transport, handshake))
}

fn lookup_receipt(config: &Config, activity_id: [u8; 32], wait_publication: bool) -> ReceiptSource {
    let Some(deadline) = Instant::now().checked_add(IO_TIMEOUT) else {
        return ReceiptSource::Unavailable("LNI deadline unavailable".to_owned());
    };
    let (mut transport, handshake) = match receipt_connection(config, wait_publication, deadline) {
        Ok(connection) => connection,
        Err(DependencyReason::KeyMismatch) => return ReceiptSource::KeyMismatch,
        Err(reason) => return ReceiptSource::Unavailable(format!("LNI admission: {reason:?}")),
    };
    let mut selector = Vec::with_capacity(34);
    selector.push(1);
    selector.extend_from_slice(&activity_id);
    if wait_publication {
        selector.push(1);
    } else if handshake.node().interface_version.minor >= Version::V1_6.minor {
        selector.push(0);
    }
    let Ok(correlation_id) = trust::correlation(1) else {
        return ReceiptSource::Unavailable("LNI correlation exhausted".to_owned());
    };
    let request = match encode_envelope(Envelope {
        version: handshake.node().interface_version,
        message_tag: RECEIPT_LOOKUP_REQUEST,
        correlation_id,
        canonical_payload: &selector,
        proof_material: &[],
    }) {
        Ok(request) => request,
        Err(error) => return ReceiptSource::Unavailable(format!("LNI encode: {error:?}")),
    };
    if let Err(error) = transport.send(&request) {
        return ReceiptSource::Unavailable(format!("LNI send: {error:?}"));
    }
    let response = match transport.receive() {
        Ok(response) => response,
        Err(error) => return ReceiptSource::Unavailable(format!("LNI receive: {error:?}")),
    };
    let envelope = match decode_envelope(&response) {
        Ok(envelope) => envelope,
        Err(error) => return ReceiptSource::Unavailable(format!("LNI decode: {error:?}")),
    };
    if envelope.version.major != handshake.node().interface_version.major
        || envelope.correlation_id != correlation_id
    {
        return ReceiptSource::Unavailable(
            "LNI response changed version or correlation".to_owned(),
        );
    }
    match envelope.message_tag {
        RECEIPT_LOOKUP_RESPONSE if envelope.proof_material.is_empty() => {
            if envelope.canonical_payload.is_empty() {
                ReceiptSource::Unknown
            } else {
                ReceiptSource::Found(envelope.canonical_payload.to_vec())
            }
        }
        ERROR_RESPONSE => ReceiptSource::Unavailable(format!(
            "LNI refused the lookup: {:?}",
            decode_core_refusal(envelope.canonical_payload)
        )),
        other => ReceiptSource::Unavailable(format!("LNI answered message tag {other}")),
    }
}

fn checkpoint_header(
    config: &Config,
    batch: u64,
) -> Result<layerx_client::evidence::VerifiedCheckpoint, ()> {
    checkpoint_for(config, Some(batch))
}

fn checkpoint_for(
    config: &Config,
    batch: Option<u64>,
) -> Result<layerx_client::evidence::VerifiedCheckpoint, ()> {
    let deadline = Instant::now().checked_add(IO_TIMEOUT).ok_or(())?;
    let mut transport = Uds::connect(
        &config.lni_socket,
        &config.lni_gate,
        trust::limits(deadline)?,
    )
    .map_err(|_| ())?;
    let handshake = perform(
        &mut transport,
        &HandshakeConfig {
            built_interface_version: Version::V1_5,
            expected_protocol_version: PROTOCOL_VERSION,
            expected_network_id: config.protocol_network_id,
        },
        None,
    )
    .map_err(|_| ())?;
    let history = trust::snapshot(
        config,
        handshake.node().latest_sealed_batch,
        handshake.node().authorised_sequencer_key,
        deadline,
    )?;
    let verified = trust::checkpoint(
        config,
        &mut transport,
        batch.unwrap_or(handshake.node().latest_sealed_batch),
        handshake.node().interface_version,
        history.as_ref(),
    )?;
    if batch.is_none() {
        let header = layerx_wire::receipt::decode_batch_header(verified.canonical_header())
            .map_err(|_| ())?;
        if header.last_sequence() != handshake.node().chain_head_sequence
            || header.batch_number() != handshake.node().latest_sealed_batch
        {
            return Err(());
        }
    }
    Ok(verified)
}

fn evidence_refusal(refusal_kind: &EvidenceRefusal) -> Response {
    eprintln!("layerx-receipt-authority refused replica evidence: {refusal_kind:?}");
    refusal(502, "evidence_refused", None)
}

fn await_requested_receipt(
    config: &Config,
    activity_id: [u8; 32],
    wait_publication: bool,
    deadline: Instant,
) -> ReceiptSource {
    loop {
        let source = lookup_receipt(config, activity_id, wait_publication);
        if !matches!(source, ReceiptSource::Unknown)
            || !wait_publication
            || Instant::now() >= deadline
        {
            return source;
        }
        thread::sleep(Duration::from_millis(25));
    }
}

fn await_replica_evidence(
    config: &Config,
    path: &str,
    wait: bool,
    deadline: Instant,
) -> ReplicaAnswer {
    loop {
        let answer = replica_get(config, path);
        if !matches!(answer, ReplicaAnswer::Status(404, _)) || !wait || Instant::now() >= deadline {
            return answer;
        }
        thread::sleep(Duration::from_millis(25));
    }
}

fn by_activity(config: &Config, requested: &str, wait_publication: bool) -> Response {
    let Ok(activity_id) = hex::decode32(requested) else {
        return refusal(400, "invalid_activity_id", None);
    };
    if activity_id == [0; 32] {
        return refusal(400, "invalid_activity_id", None);
    }
    let deadline = Instant::now() + IO_TIMEOUT;
    let receipt = match await_requested_receipt(config, activity_id, wait_publication, deadline) {
        ReceiptSource::Found(receipt) => receipt,
        ReceiptSource::Unknown => return refusal(404, "unknown_activity", None),
        ReceiptSource::KeyMismatch => {
            eprintln!("layerx-receipt-authority: LNI sequencer key differs from the pinned key");
            return refusal(502, "sequencer_key_mismatch", None);
        }
        ReceiptSource::Unavailable(reason) => {
            eprintln!("layerx-receipt-authority receipt source unavailable: {reason}");
            return refusal(503, "receipt_source_unavailable", Some(5));
        }
    };
    let locator = match receipt_locator(&receipt) {
        Ok(locator) => locator,
        Err(error) => return evidence_refusal(&error),
    };
    if locator.activity_id != activity_id {
        return evidence_refusal(&EvidenceRefusal::ActivityMismatch);
    }
    let path = format!(
        "/v1/batches/{}/receipt-authority?receipt_digest={}",
        hex::encode(&locator.batch_id),
        hex::encode(&locator.receipt_digest)
    );
    let document = match await_replica_evidence(config, &path, wait_publication, deadline) {
        ReplicaAnswer::Status(200, body) => body,
        ReplicaAnswer::Status(404, _) => {
            return refusal(503, "replica_evidence_unavailable", Some(1));
        }
        ReplicaAnswer::Status(status, _) => {
            eprintln!("layerx-receipt-authority replica answered HTTP {status}");
            return refusal(503, "replica_unavailable", Some(5));
        }
        ReplicaAnswer::Unavailable => return refusal(503, "replica_unavailable", Some(5)),
    };
    let (evidence, authorization) = match trust::replica(config, &receipt, &document) {
        Ok(verified) => verified,
        Err(error) => return evidence_refusal(&error),
    };
    if layerx_wire::receipt::decode_batch_header(&evidence.header).map_or(true, |header| {
        header.network_id() != config.protocol_network_id
    }) {
        return refusal(503, "receipt_network_mismatch", Some(5));
    }
    match authorized_batch_by_activity(activity_id, &receipt, &evidence, &authorization) {
        Ok(facts) => {
            if verify_withdrawal_request(config, &receipt, &evidence.header, deadline).is_err() {
                return refusal(503, "withdrawal_evidence_unavailable", Some(5));
            }
            if config
                .human
                .as_ref()
                .is_some_and(|human| human.retain(&receipt, &document, config).is_err())
            {
                return refusal(503, "state_persistence_unavailable", Some(5));
            }
            let mut response = serde_json::json!({
                "activity_id": requested,
                "receipt": hex::encode(&receipt),
                "batch_id": hex::encode(&facts.batch_id),
                "asset": hex::encode(&facts.asset),
                "previous_state_root": hex::encode(&facts.previous_state_root),
                "resulting_state_root": hex::encode(&facts.resulting_state_root),
                "sequencer_public_key": hex::encode(&facts.sequencer_public_key),
                "network_id": config.network_id,
                "protocol_network_id": config.protocol_network_id,
                "wire_version": config.wire_version,
            });
            if matches!(
                evidence.batch_identity,
                layerx_platform_authority::BatchIdentityEvidence::OccupancyMaintenanceV2 { .. }
                    | layerx_platform_authority::BatchIdentityEvidence::BatchMaintenanceV1 { .. }
            ) {
                let replica: serde_json::Value = match serde_json::from_slice(&document) {
                    Ok(value) => value,
                    Err(_) => return evidence_refusal(&EvidenceRefusal::ReplicaDocument),
                };
                response["batch_evidence"] = replica["batch_evidence"].clone();
            }
            json(200, &response)
        }
        Err(error) => evidence_refusal(&error),
    }
}

fn verify_withdrawal_request(
    config: &Config,
    receipt: &[u8],
    header: &[u8],
    deadline: Instant,
) -> Result<(), String> {
    use layerx_client::evidence::{
        proof_bundle, EvidenceContext, ProofBundleSelector, VerifiedProofBundle,
    };
    use layerx_proof::receipt::{withdrawal, AuthorizedBatch};
    let decoded = layerx_wire::receipt::decode(receipt).map_err(|error| format!("{error:?}"))?;
    let protocol = decoded.protocol().ok_or("protocol receipt required")?;
    if protocol.module_id() != 1 || protocol.operation() != 9 {
        return Ok(());
    }
    let remaining = deadline
        .checked_duration_since(Instant::now())
        .ok_or("authority deadline expired")?;
    let limits = Limits {
        maximum_frame_bytes: LNI_FRAME_BYTES,
        maximum_connections: MAX_LNI_CONNECTIONS,
        maximum_streams: 1,
        maximum_queued_bytes: LNI_FRAME_BYTES,
        deadline: remaining,
    };
    let mut transport = Uds::connect(&config.lni_socket, &config.lni_gate, limits)
        .map_err(|error| format!("{error:?}"))?;
    let handshake = perform(
        &mut transport,
        &HandshakeConfig {
            built_interface_version: Version::V1_5,
            expected_protocol_version: PROTOCOL_VERSION,
            expected_network_id: config.protocol_network_id,
        },
        None,
    )
    .map_err(|error| format!("{error:?}"))?;
    let history = trust::snapshot(
        config,
        handshake.node().latest_sealed_batch,
        handshake.node().authorised_sequencer_key,
        deadline,
    )
    .map_err(|()| "withdrawal sequencer history unavailable".to_owned())?;
    let authorization = match &history {
        Some(history) => history
            .authorization_for_sequence(protocol.global_sequence())
            .map_err(|error| format!("{error:?}"))?,
        None => config.authorization,
    };
    let context = EvidenceContext {
        interface_version: handshake.node().interface_version,
        correlation_id: trust::correlation(1)
            .map_err(|()| "LNI correlation exhausted".to_owned())?,
        expected_protocol_version: PROTOCOL_VERSION,
        expected_network_id: config.protocol_network_id,
        handshake_sequencer_key: authorization.public_key(),
    };
    let registry = withdrawal::registry().map_err(|error| format!("{error:?}"))?;
    let bundle = if let Some(history) = &history {
        layerx_client::evidence::proof_bundle_with_history(
            &mut transport,
            ProofBundleSelector::Activity(protocol.activity_id()),
            context,
            &registry,
            history,
        )
    } else {
        proof_bundle(
            &mut transport,
            ProofBundleSelector::Activity(protocol.activity_id()),
            context,
            &registry,
        )
    }
    .map_err(|error| format!("{error:?}"))?;
    let VerifiedProofBundle::Activity {
        canonical_bytes,
        signed_header,
        ..
    } = bundle
    else {
        return Err("withdrawal activity proof required".to_owned());
    };
    if signed_header.canonical_bytes != header {
        return Err("withdrawal header mismatch".to_owned());
    }
    let authorized = AuthorizedBatch::new(
        protocol.batch_id(),
        protocol.asset(),
        protocol.previous_state_root(),
        protocol.resulting_state_root(),
        authorization.public_key(),
    );
    withdrawal::verify(
        receipt,
        &authorized,
        &canonical_bytes,
        config.protocol_network_id,
    )
    .map_err(|error| format!("{error:?}"))?;
    Ok(())
}

fn relay(config: &Config, batch_id: &str, query: Option<&str>) -> Response {
    let Some(digest) = query.and_then(|query| query.strip_prefix("receipt_digest=")) else {
        return refusal(400, "invalid_request", None);
    };
    if !hex::is_hex32(batch_id) || !hex::is_hex32(digest) {
        return refusal(400, "invalid_request", None);
    }
    let path = format!("/v1/batches/{batch_id}/receipt-authority?receipt_digest={digest}");
    match replica_get(config, &path) {
        ReplicaAnswer::Status(status @ (200 | 404), body) => Response {
            status,
            body,
            retry_after: None,
            readiness_headers: None,
        },
        ReplicaAnswer::Status(status, _) => {
            eprintln!("layerx-receipt-authority replica answered HTTP {status}");
            refusal(503, "replica_unavailable", Some(5))
        }
        ReplicaAnswer::Unavailable => refusal(503, "replica_unavailable", Some(5)),
    }
}

static ACTIVE_READINESS: AtomicUsize = AtomicUsize::new(0);

struct ReadinessPermit;
impl ReadinessPermit {
    fn acquire() -> Option<Arc<Self>> {
        ACTIVE_READINESS
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
                (active < MAX_LNI_CONNECTIONS).then_some(active + 1)
            })
            .ok()
            .map(|_| Arc::new(Self))
    }
}
impl Drop for ReadinessPermit {
    fn drop(&mut self) {
        ACTIVE_READINESS.fetch_sub(1, Ordering::AcqRel);
    }
}

fn replica_answers(
    config: &Config,
    deadline: Instant,
) -> Result<DependencyReason, DependencyReason> {
    let path = format!("/v1/batches/{ZERO_HEX32}/receipt-authority?receipt_digest={ZERO_HEX32}");
    match replica_exchange_until(config, &path, deadline) {
        Ok(ReplicaAnswer::Status(200, _)) => Ok(DependencyReason::Ready),
        Ok(ReplicaAnswer::Status(404, _)) => Ok(DependencyReason::Empty),
        _ if Instant::now() >= deadline => Err(DependencyReason::Timeout),
        _ => Err(DependencyReason::Unavailable),
    }
}

fn lni_answers(config: &Config, deadline: Instant) -> Result<DependencyReason, DependencyReason> {
    let (_, handshake) = receipt_connection(config, true, deadline)?;
    if config.trust.is_some() {
        let history = trust::snapshot(
            config,
            handshake.node().latest_sealed_batch,
            handshake.node().authorised_sequencer_key,
            deadline,
        )
        .map_err(|()| DependencyReason::IdentityMismatch)?;
        let next_batch = handshake
            .node()
            .latest_sealed_batch
            .checked_add(1)
            .ok_or(DependencyReason::IdentityMismatch)?;
        let key = history
            .as_ref()
            .ok_or(DependencyReason::IdentityMismatch)?
            .signed_authority()
            .intervals()
            .iter()
            .find(|interval| (interval.first_batch()..=interval.last_batch()).contains(&next_batch))
            .ok_or(DependencyReason::IdentityMismatch)?
            .public_key();
        if key != handshake.node().authorised_sequencer_key {
            return Err(DependencyReason::IdentityMismatch);
        }
    }
    if Instant::now() >= deadline {
        return Err(DependencyReason::Timeout);
    }
    Ok(DependencyReason::Ready)
}

#[derive(serde::Serialize)]
struct AuthorityDependencies {
    replica: DependencyReadiness,
    lni: DependencyReadiness,
}

#[derive(serde::Serialize)]
struct AuthorityReadinessResponse<'a> {
    ready: bool,
    network_id: &'a str,
    protocol_network_id: u32,
    wire_version: &'a str,
}

fn readiness(config: &Arc<Config>) -> Response {
    let deadline = Instant::now() + READINESS_TIMEOUT;
    let unavailable = DependencyReadiness::from_result(Err(DependencyReason::Timeout));
    let mut dependencies = AuthorityDependencies {
        replica: unavailable,
        lni: unavailable,
    };
    if let Some(permit) = ReadinessPermit::acquire() {
        let (send, receive) = std::sync::mpsc::channel();
        for is_lni in [false, true] {
            let config = Arc::clone(config);
            let send = send.clone();
            let permit = Arc::clone(&permit);
            thread::spawn(move || {
                let _permit = permit;
                let result = if is_lni {
                    lni_answers(&config, deadline)
                } else {
                    replica_answers(&config, deadline)
                };
                let _ = send.send((is_lni, DependencyReadiness::from_result(result)));
            });
        }
        drop(send);
        for _ in 0..2 {
            let Some(remaining) = deadline.checked_duration_since(Instant::now()) else {
                break;
            };
            let Ok((is_lni, result)) = receive.recv_timeout(remaining) else {
                break;
            };
            if is_lni {
                dependencies.lni = result;
            } else {
                dependencies.replica = result;
            }
        }
    } else {
        let limited = DependencyReadiness::from_result(Err(DependencyReason::ConnectionLimit));
        dependencies = AuthorityDependencies {
            replica: limited,
            lni: limited,
        };
    }
    let ready = dependencies.replica.ready && dependencies.lni.ready;
    let body = AuthorityReadinessResponse {
        ready,
        network_id: &config.network_id,
        protocol_network_id: config.protocol_network_id,
        wire_version: &config.wire_version,
    };
    let mut response = json(if ready { 200 } else { 503 }, &serde_json::json!(body));
    let category = |dependency: DependencyReadiness| {
        if dependency.ready {
            "ready"
        } else if dependency.reason == DependencyReason::ConnectionLimit {
            "unprobed"
        } else {
            "unavailable"
        }
    };
    response.readiness_headers = Some([category(dependencies.replica), category(dependencies.lni)]);
    if !ready {
        response.retry_after = Some(5);
    }
    response
}

fn route(config: &Arc<Config>, request: &Request) -> Response {
    if request.method != "GET" {
        return refusal(405, "method_not_allowed", None);
    }
    let path = request.path.as_str();
    if path == "/livez" {
        return json(200, &serde_json::json!({ "live": true }));
    }
    if path == "/readyz" {
        return readiness(config);
    }
    if path.starts_with("/v1/agent/") {
        return human::route(config, request);
    }
    if let Some(batch_id) = path
        .strip_prefix("/v1/batches/")
        .and_then(|rest| rest.strip_suffix("/receipt-authority"))
    {
        return match authenticate(config, request) {
            Ok(()) => relay(config, batch_id, request.query.as_deref()),
            Err(response) => response,
        };
    }
    if request.query.is_some() {
        return refusal(404, "not_found", None);
    }
    let activity = path
        .strip_prefix("/v1/authorized-batches/by-activity/")
        .map(|activity| (activity, false))
        .or_else(|| {
            path.strip_prefix("/v1/authorized-batches/wait-by-activity/")
                .map(|activity| (activity, true))
        })
        .or_else(|| {
            path.strip_prefix("/internal/v1/activities/")
                .and_then(|rest| rest.strip_suffix("/authority"))
                .map(|activity| (activity, false))
        });
    match activity {
        Some((activity, wait_publication)) => match authenticate(config, request) {
            Ok(()) => by_activity(config, activity, wait_publication),
            Err(response) => response,
        },
        None => refusal(404, "not_found", None),
    }
}

fn handle_connection(
    config: &Arc<Config>,
    tcp: TcpStream,
    shutdown: &AtomicBool,
) -> Result<(), String> {
    tcp.set_nodelay(true).map_err(|error| error.to_string())?;
    tcp.set_read_timeout(Some(IO_TIMEOUT))
        .map_err(|error| error.to_string())?;
    tcp.set_write_timeout(Some(IO_TIMEOUT))
        .map_err(|error| error.to_string())?;
    let connection =
        ServerConnection::new(Arc::clone(&config.tls)).map_err(|error| error.to_string())?;
    let mut stream = StreamOwned::new(connection, tcp);
    for request_number in 0..MAX_REQUESTS_PER_CONNECTION {
        if shutdown.load(Ordering::Acquire) {
            break;
        }
        let request = match read_http_message(&mut stream) {
            Ok(request) => request,
            Err(_) if request_number == 0 => {
                write_response(&mut stream, &refusal(400, "invalid_request", None), false)?;
                break;
            }
            Err(_) => break,
        };
        let keep_alive = !shutdown.load(Ordering::Acquire)
            && request_number + 1 < MAX_REQUESTS_PER_CONNECTION
            && request
                .headers
                .get("connection")
                .is_none_or(|value| !value.eq_ignore_ascii_case("close"));
        write_response(&mut stream, &route(config, &request), keep_alive)?;
        if !keep_alive {
            break;
        }
    }
    stream.conn.send_close_notify();
    let _ = stream.conn.write_tls(&mut stream.sock);
    Ok(())
}

struct ConnectionPermit;

impl ConnectionPermit {
    fn acquire() -> Option<Self> {
        ACTIVE_CONNECTIONS
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
                (active < MAX_CONNECTIONS).then_some(active + 1)
            })
            .ok()
            .map(|_| Self)
    }
}

impl Drop for ConnectionPermit {
    fn drop(&mut self) {
        ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
    }
}

fn serve(config: Config) -> Result<(), String> {
    let shutdown = Arc::new(AtomicBool::new(false));
    signal_hook::flag::register(signal_hook::consts::SIGTERM, Arc::clone(&shutdown))
        .map_err(|error| error.to_string())?;
    signal_hook::flag::register(signal_hook::consts::SIGINT, Arc::clone(&shutdown))
        .map_err(|error| error.to_string())?;
    let listener = TcpListener::bind(config.listen).map_err(|error| error.to_string())?;
    listener
        .set_nonblocking(true)
        .map_err(|error| error.to_string())?;
    let config = Arc::new(config);
    let mut workers = Vec::<thread::JoinHandle<()>>::new();
    eprintln!(
        "layerx-receipt-authority listening with TLS on {}",
        config.listen
    );
    while !shutdown.load(Ordering::Acquire) {
        workers.retain(|worker| !worker.is_finished());
        match listener.accept() {
            Ok((stream, _)) => {
                let Some(permit) = ConnectionPermit::acquire() else {
                    continue;
                };
                let shared = Arc::clone(&config);
                let stopping = Arc::clone(&shutdown);
                workers.push(thread::spawn(move || {
                    let _permit = permit;
                    if let Err(error) = handle_connection(&shared, stream, &stopping) {
                        eprintln!("layerx-receipt-authority connection failed: {error}");
                    }
                }));
            }
            Err(error) => {
                if error.kind() != std::io::ErrorKind::WouldBlock
                    && error.kind() != std::io::ErrorKind::Interrupted
                {
                    eprintln!("layerx-receipt-authority accept failed: {error}");
                }
                thread::sleep(Duration::from_millis(25));
            }
        }
    }
    drop(listener);
    for worker in workers {
        worker
            .join()
            .map_err(|_| "receipt authority connection worker panicked".to_owned())?;
    }
    Ok(())
}

fn main() {
    let arguments: Vec<String> = env::args().skip(1).collect();
    if arguments
        .iter()
        .any(|argument| argument == "--help" || argument == "-h")
    {
        print!("{USAGE}");
        return;
    }
    if !arguments.is_empty() {
        eprint!("layerx-receipt-authority accepts no arguments\n\n{USAGE}");
        std::process::exit(2);
    }
    if let Err(error) = config().and_then(serve) {
        eprintln!("layerx-receipt-authority: {error}");
        std::process::exit(2);
    }
}

#[cfg(test)]
mod lni_readiness_tests {
    use super::*;
    use layerx_client::lni::handshake::{decode_node_info, validate};

    #[test]
    fn actual_node_info_refuses_incompatible_receipt_admission() {
        let bytes =
            fs::read(env::var("PAXEER_X_LNI_CAPTURE").expect("actual LNI capture required"))
                .expect("actual capture file");
        let node = decode_node_info(&bytes).expect("production NodeInfo decoder");
        let expected = HandshakeConfig {
            built_interface_version: Version::V1_5,
            expected_protocol_version: PROTOCOL_VERSION,
            expected_network_id: node.network_id,
        };
        let key = node.authorised_sequencer_key;
        let accepted = validate(node.clone(), &expected, None).expect("actual handshake identity");
        assert_eq!(
            validate_receipt_handshake(&accepted, Some(key), true),
            Ok(())
        );
        let mut count = 1;
        let mut wrong = node.clone();
        wrong.protocol_version += 1;
        assert!(matches!(
            validate(wrong, &expected, None),
            Err(HandshakeError::ProtocolVersion { .. })
        ));
        count += 1;
        let mut wrong = node.clone();
        wrong.network_id += 1;
        assert!(matches!(
            validate(wrong, &expected, None),
            Err(HandshakeError::Network { .. })
        ));
        count += 1;
        let mut wrong = node.clone();
        wrong.interface_version.major += 1;
        assert!(matches!(
            validate(wrong, &expected, None),
            Err(HandshakeError::InterfaceIncompatible { .. })
        ));
        count += 1;
        let mut wrong = node.clone();
        wrong.advertised_capabilities.clear();
        let handshake = validate(wrong, &expected, None)
            .expect("same actual identity without receipt capability");
        assert_eq!(
            validate_receipt_handshake(&handshake, Some(key), true),
            Err(DependencyReason::CapabilityMissing)
        );
        count += 1;
        let mut wrong = node.clone();
        wrong.interface_version.minor = 4;
        let handshake = validate(wrong, &expected, None).expect("compatible major");
        assert_eq!(
            validate_receipt_handshake(&handshake, Some(key), true),
            Err(DependencyReason::ProtocolIncompatible)
        );
        count += 1;
        let mut wrong_key = key;
        wrong_key[0] ^= 1;
        assert_eq!(
            validate_receipt_handshake(&accepted, Some(wrong_key), true),
            Err(DependencyReason::KeyMismatch)
        );
        count += 1;
        println!("PAXEER_X_LNI_CASES={count}");
    }
}
