use crate::ramp_v2::{SourceSettlementRequestV2, SourceSettlementResponseV2};
use crate::{MigrationError, SourceEvidence};
use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use layerx_interop_gateway::trace::TraceId;
use rustls::pki_types::{CertificateDer, ServerName};
use rustls::{ClientConfig, ClientConnection, RootCertStore, StreamOwned};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::fs::{self, File};
use std::io::{Read, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};
use zeroize::Zeroizing;

const MAX_HEADER: usize = 32 * 1024;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct GatewayClientConfig {
    pub endpoint: String,
    pub ca_certificate_der: PathBuf,
    pub customer_authorization_file: PathBuf,
    pub connect_timeout_ms: u64,
    pub request_timeout_ms: u64,
    pub maximum_response_bytes: usize,
}

pub struct GatewayClient {
    host: String,
    port: u16,
    tls: Arc<ClientConfig>,
    authorization: Zeroizing<String>,
    connect_timeout: Duration,
    request_timeout: Duration,
    maximum_response_bytes: usize,
}

#[derive(Serialize)]
pub struct FundedMigrationObservation {
    state: &'static str,
    producer_state: String,
    order_digest: [u8; 32],
    operation: String,
    source_evidence_digest: [u8; 32],
    source_claim_id: Option<[u8; 32]>,
    provenance: &'static str,
    layerx_receipt: bool,
    layerx_credit_verified: bool,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Success {
    ok: bool,
    operation: String,
    result: Settlement,
    trace: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Settlement {
    source_settlement: SourceSettlementResponseV2,
    provenance: String,
    custody_label: String,
    layerx_receipt: bool,
}

impl GatewayClient {
    pub fn new(config: &GatewayClientConfig) -> Result<Self, MigrationError> {
        if !(100..=30_000).contains(&config.connect_timeout_ms)
            || !(100..=120_000).contains(&config.request_timeout_ms)
            || !(1024..=2 * 1024 * 1024).contains(&config.maximum_response_bytes)
        {
            return Err(MigrationError::Configuration);
        }
        let (host, port) = endpoint(&config.endpoint)?;
        let secret = Zeroizing::new(private(&config.customer_authorization_file, 8192)?);
        let authorization = Zeroizing::new(
            std::str::from_utf8(&secret)
                .map_err(|_| MigrationError::Configuration)?
                .trim_end_matches(['\r', '\n'])
                .to_owned(),
        );
        let token = authorization
            .strip_prefix("Bearer ")
            .ok_or(MigrationError::Configuration)?;
        if token.is_empty() || token.bytes().any(|byte| !byte.is_ascii_graphic()) {
            return Err(MigrationError::Configuration);
        }
        let mut roots = RootCertStore::empty();
        roots
            .add(CertificateDer::from(private(
                &config.ca_certificate_der,
                64 * 1024,
            )?))
            .map_err(|_| MigrationError::Configuration)?;
        Ok(Self {
            host,
            port,
            tls: Arc::new(
                ClientConfig::builder()
                    .with_root_certificates(roots)
                    .with_no_client_auth(),
            ),
            authorization,
            connect_timeout: Duration::from_millis(config.connect_timeout_ms),
            request_timeout: Duration::from_millis(config.request_timeout_ms),
            maximum_response_bytes: config.maximum_response_bytes,
        })
    }

    pub fn migrate_asset(
        &self,
        order_digest: [u8; 32],
        chain: &str,
        evidence: &SourceEvidence,
        idempotency: &str,
        trace: &TraceId,
    ) -> Result<FundedMigrationObservation, MigrationError> {
        let binding = SourceSettlementRequestV2::new(order_digest, chain, evidence)?;
        if idempotency.is_empty()
            || idempotency.len() > 128
            || !idempotency
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.'))
        {
            return Err(MigrationError::Configuration);
        }
        let body = serde_json::to_vec(&json!({"order_digest": order_digest, "chain": chain,
            "source_evidence": STANDARD.encode(evidence.canonical())}))
        .map_err(|_| MigrationError::Configuration)?;
        let addresses = (self.host.as_str(), self.port)
            .to_socket_addrs()
            .map_err(|_| MigrationError::RpcUnavailable)?;
        let mut connected = None;
        for address in addresses.take(8) {
            if let Ok(stream) = TcpStream::connect_timeout(&address, self.connect_timeout) {
                connected = Some(stream);
                break;
            }
        }
        let tcp = connected.ok_or(MigrationError::RpcUnavailable)?;
        tcp.set_read_timeout(Some(self.request_timeout))
            .map_err(|_| MigrationError::RpcUnavailable)?;
        tcp.set_write_timeout(Some(self.request_timeout))
            .map_err(|_| MigrationError::RpcUnavailable)?;
        let server =
            ServerName::try_from(self.host.clone()).map_err(|_| MigrationError::Configuration)?;
        let connection = ClientConnection::new(Arc::clone(&self.tls), server)
            .map_err(|_| MigrationError::RpcUnavailable)?;
        let mut stream = StreamOwned::new(connection, tcp);
        let deadline = Instant::now() + self.request_timeout;
        write!(stream,
            "POST /v2/migration/assets HTTP/1.1\r\nHost: {}:{}\r\nAuthorization: {}\r\nIdempotency-Key: {}\r\nX-LayerX-Trace-Id: {}\r\nContent-Type: application/json\r\nAccept: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
            self.host, self.port, self.authorization.as_str(), idempotency, trace.as_str(), body.len())
            .and_then(|()| stream.write_all(&body)).and_then(|()| stream.flush())
            .map_err(|_| MigrationError::GatewaySubmissionUnknown)?;
        let (status, bytes) = response(&mut stream, self.maximum_response_bytes, deadline)?;
        if status == 429 {
            return Err(MigrationError::RpcRateLimited {
                retry_after_seconds: 5,
            });
        }
        if status >= 500 {
            return Err(MigrationError::GatewaySubmissionUnknown);
        }
        if !matches!(status, 200 | 202) {
            return Err(MigrationError::PlaneRefused);
        }
        let value: Value =
            serde_json::from_slice(&bytes).map_err(|_| MigrationError::RpcResponseMismatch)?;
        if value.get("ok") == Some(&Value::Bool(false)) {
            return Err(MigrationError::PlaneRefused);
        }
        let success: Success =
            serde_json::from_value(value).map_err(|_| MigrationError::RpcResponseMismatch)?;
        if !success.ok || !valid_operation(&success.operation) || TraceId::parse(&success.trace).is_err()
            || success.result.provenance != "external-custody" || success.result.layerx_receipt
            || success.result.custody_label != "External custody: this independent market maker controls the off-platform funds and payout."
        { return Err(MigrationError::RpcResponseMismatch); }
        success
            .result
            .source_settlement
            .validate(&binding, evidence)?;
        let settled = success.result.source_settlement;
        Ok(FundedMigrationObservation {
            state: "producer-observed",
            producer_state: settled.state,
            order_digest,
            operation: success.operation,
            source_evidence_digest: settled.source_evidence_digest,
            source_claim_id: settled.source_claim_id,
            provenance: "external-custody",
            layerx_receipt: false,
            layerx_credit_verified: false,
        })
    }
}

fn valid_operation(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 256
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b':'))
}

fn endpoint(value: &str) -> Result<(String, u16), MigrationError> {
    if value.len() > 2048 || value.bytes().any(|byte| !byte.is_ascii_graphic()) {
        return Err(MigrationError::Configuration);
    }
    let rest = value
        .strip_prefix("https://")
        .ok_or(MigrationError::Configuration)?;
    let authority = rest.strip_suffix('/').unwrap_or(rest);
    if authority.contains(['/', '@', '?', '#', '\\']) {
        return Err(MigrationError::Configuration);
    }
    let (host, port) = match authority.rsplit_once(':') {
        Some((host, port)) => (
            host.to_owned(),
            port.parse::<u16>()
                .map_err(|_| MigrationError::Configuration)?,
        ),
        None => (authority.to_owned(), 443),
    };
    if host.len() > 253
        || port == 0
        || !host.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && !label.starts_with('-')
                && !label.ends_with('-')
                && label
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        })
    {
        return Err(MigrationError::Configuration);
    }
    Ok((host, port))
}

fn private(path: &Path, maximum: usize) -> Result<Vec<u8>, MigrationError> {
    let uid = fs::read_to_string("/proc/self/status")
        .ok()
        .and_then(|value| {
            value.lines().find_map(|line| {
                line.strip_prefix("Uid:")
                    .and_then(|uid| uid.split_whitespace().nth(1))
                    .and_then(|uid| uid.parse::<u32>().ok())
            })
        })
        .ok_or(MigrationError::Configuration)?;
    let before = fs::symlink_metadata(path).map_err(|_| MigrationError::Configuration)?;
    if !path.is_absolute()
        || fs::canonicalize(path).ok().as_deref() != Some(path)
        || !before.is_file()
        || before.uid() != uid
        || before.nlink() != 1
        || before.permissions().mode() & 0o077 != 0
        || before.len() == 0
        || before.len() > maximum as u64
    {
        return Err(MigrationError::Configuration);
    }
    let mut file = File::open(path).map_err(|_| MigrationError::Configuration)?;
    let after = file.metadata().map_err(|_| MigrationError::Configuration)?;
    if before.dev() != after.dev()
        || before.ino() != after.ino()
        || before.uid() != after.uid()
        || before.mode() != after.mode()
        || after.nlink() != 1
    {
        return Err(MigrationError::Configuration);
    }
    let mut bytes = Vec::new();
    Read::by_ref(&mut file)
        .take(maximum as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| MigrationError::Configuration)?;
    if bytes.is_empty() || bytes.len() > maximum {
        return Err(MigrationError::Configuration);
    }
    Ok(bytes)
}

fn response(
    stream: &mut impl Read,
    maximum: usize,
    deadline: Instant,
) -> Result<(u16, Vec<u8>), MigrationError> {
    let mut bytes = Vec::new();
    let mut buffer = [0_u8; 4096];
    let end = loop {
        if Instant::now() >= deadline {
            return Err(MigrationError::GatewaySubmissionUnknown);
        }
        let count = stream
            .read(&mut buffer)
            .map_err(|_| MigrationError::GatewaySubmissionUnknown)?;
        if count == 0 {
            return Err(MigrationError::GatewaySubmissionUnknown);
        }
        bytes.extend_from_slice(&buffer[..count]);
        if bytes.len() > maximum.saturating_add(MAX_HEADER) {
            return Err(MigrationError::RpcResponseMismatch);
        }
        if let Some(position) = bytes.windows(4).position(|part| part == b"\r\n\r\n") {
            if position + 4 > MAX_HEADER {
                return Err(MigrationError::RpcResponseMismatch);
            }
            break position + 4;
        }
        if bytes.len() > MAX_HEADER {
            return Err(MigrationError::RpcResponseMismatch);
        }
    };
    let text =
        std::str::from_utf8(&bytes[..end]).map_err(|_| MigrationError::RpcResponseMismatch)?;
    let mut lines = text.split("\r\n");
    let mut status = lines
        .next()
        .ok_or(MigrationError::RpcResponseMismatch)?
        .split_whitespace();
    if !matches!(status.next(), Some("HTTP/1.1" | "HTTP/1.0")) {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let status = status
        .next()
        .and_then(|value| value.parse::<u16>().ok())
        .filter(|value| (100..=599).contains(value))
        .ok_or(MigrationError::RpcResponseMismatch)?;
    let mut headers = BTreeMap::new();
    for line in lines.filter(|line| !line.is_empty()) {
        let (name, value) = line
            .split_once(':')
            .ok_or(MigrationError::RpcResponseMismatch)?;
        if name.is_empty()
            || !name
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
            || headers
                .insert(name.to_ascii_lowercase(), value.trim().to_owned())
                .is_some()
        {
            return Err(MigrationError::RpcResponseMismatch);
        }
    }
    if !headers
        .get("content-type")
        .is_some_and(|value| value.split(';').next() == Some("application/json"))
        || (headers.contains_key("transfer-encoding") && headers.contains_key("content-length"))
    {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let length = headers
        .get("content-length")
        .map(|value| {
            value
                .parse::<usize>()
                .map_err(|_| MigrationError::RpcResponseMismatch)
        })
        .transpose()?;
    if length.is_some_and(|value| value > maximum)
        || (length.is_none()
            && headers.get("transfer-encoding").map(String::as_str) != Some("chunked"))
    {
        return Err(MigrationError::RpcResponseMismatch);
    }
    let mut body = bytes[end..].to_vec();
    loop {
        if body.len() > maximum {
            return Err(MigrationError::RpcResponseMismatch);
        }
        if let Some(length) = length {
            if body.len() > length {
                return Err(MigrationError::RpcResponseMismatch);
            }
            if body.len() == length {
                return Ok((status, body));
            }
        } else if let Some(decoded) = chunked(&body)? {
            return Ok((status, decoded));
        }
        if Instant::now() >= deadline {
            return Err(MigrationError::GatewaySubmissionUnknown);
        }
        let count = stream
            .read(&mut buffer)
            .map_err(|_| MigrationError::GatewaySubmissionUnknown)?;
        if count == 0 {
            return Err(MigrationError::GatewaySubmissionUnknown);
        }
        body.extend_from_slice(&buffer[..count]);
    }
}

fn chunked(wire: &[u8]) -> Result<Option<Vec<u8>>, MigrationError> {
    let mut offset = 0;
    let mut result = Vec::new();
    loop {
        let Some(relative) = wire[offset..].windows(2).position(|part| part == b"\r\n") else {
            return Ok(None);
        };
        let line = std::str::from_utf8(&wire[offset..offset + relative])
            .map_err(|_| MigrationError::RpcResponseMismatch)?;
        if line.is_empty() || line.len() > 16 || !line.bytes().all(|byte| byte.is_ascii_hexdigit())
        {
            return Err(MigrationError::RpcResponseMismatch);
        }
        let length =
            usize::from_str_radix(line, 16).map_err(|_| MigrationError::RpcResponseMismatch)?;
        offset += relative + 2;
        let end = offset
            .checked_add(length)
            .and_then(|value| value.checked_add(2))
            .ok_or(MigrationError::RpcResponseMismatch)?;
        if end > wire.len() {
            return Ok(None);
        }
        if wire.get(end - 2..end) != Some(b"\r\n") {
            return Err(MigrationError::RpcResponseMismatch);
        }
        if length == 0 {
            if end != wire.len() {
                return Err(MigrationError::RpcResponseMismatch);
            }
            return Ok(Some(result));
        }
        result.extend_from_slice(&wire[offset..end - 2]);
        offset = end;
    }
}
