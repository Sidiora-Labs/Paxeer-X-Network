//! Pay per request over 402LXP. Every paid route offers SID, PAX, USDC and
//! USDL, each as a metered draw against the payer's grant and as an exact
//! alternative, in `PAYMENT-REQUIRED`. PAX is paid into the receiver's main
//! account and drawn from the payer's; SID, USDC and USDL move between the
//! per-asset accounts. A `PAYMENT-SIGNATURE` is settled
//! through the `layerx-x402` seller: the receiver key signs the ordinal-6
//! draw, the gateway executes it through `lx_sendActivity`, a pending result
//! is recovered with `lx_getActivityStatus` or `lx_getReceipt`, and the
//! receipt's sequencer signature is verified against the configured trust
//! before the route releases anything. The `payment` configuration names
//! the payer every draw is taken from, the fee limit each draw is signed
//! with and may be charged, and the directory the pinned conformance suite
//! is checked against. The association of payer, request,
//! offer, activity, idempotency key and receipt is kept in the data
//! directory so a retry reuses the same signed activity and a receipt never
//! releases a second request.

use std::collections::BTreeMap;
use std::fs;
use std::io::{self, Read as _, Write as _};
use std::net::{TcpStream, ToSocketAddrs as _};
use std::os::unix::fs::{DirBuilderExt as _, OpenOptionsExt as _};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, PoisonError};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use ed25519_dalek::{Signer as _, SigningKey};
use layerx_crypto::payments::{Grant, Payment, ReceiverAuthorization};
use layerx_crypto::send::{encode_payment_envelope, EnvelopeOptions};
use layerx_crypto::signer::{sign_disclosed, LocalSigner};
use layerx_interop_gateway::adapter::ConformanceSuite;
use layerx_interop_gateway::principal::PrincipalId;
use layerx_interop_gateway::trace::TraceId;
use layerx_interop_gateway::GatewayCore;
use layerx_proof::merkle::leaf_hash;
use layerx_proof::receipt::{verify, verify_sequencer_signature, AuthorizedBatch};
use layerx_types::activity::Signature;
use layerx_types::payload::ModuleId;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::Domain;
use layerx_x402::model::{
    account_identifiers, AtomicAmount, PaymentRequired, PaymentRequirements, ResourceInfo,
    X402_VERSION,
};
use layerx_x402::seller::{
    ExecutedPayment, LayerXPaymentRequest, PaymentPlane, PlanePaymentOutcome, Seller, SellerOutcome,
};
use layerx_x402::x402_adapter_descriptor;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};
use zeroize::Zeroizing;

use crate::assets::{AcceptedAsset, AcceptedAssets, AssetRefusal};
use crate::config::{payer_did_valid, AssetSymbol, Config, SequencerTrust};
use crate::server::{Request, Response, Route, RouteError, RouteTable};

pub const PAYMENT_REQUIRED: &str = "PAYMENT-REQUIRED";
pub const PAYMENT_SIGNATURE: &str = "PAYMENT-SIGNATURE";
pub const PAYMENT_RESPONSE: &str = "PAYMENT-RESPONSE";
/// Names the buyer's DID so metered offers can be bound to its accounts.
pub const PAYER_DID: &str = "LAYERX-PAYER-DID";
pub const COMMITMENT: &str = "executed";
pub const METERED: &str = "metered";
pub const EXACT: &str = "exact";
/// The protocol the draw, its receiver authorization and the payee accounts
/// are derived under.
pub const PROTOCOL_VERSION: u16 = layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION;
pub const OFFER_TIMEOUT_SECONDS: u32 = 60;
pub const DRAW_VALIDITY_MS: u64 = 60_000;
pub const GRANT_BYTES: usize = 346;

const PURPOSE_DOMAIN: &[u8] = b"LayerX/x-websearch/v1/purpose\0";
const PRINCIPAL_DOMAIN: &[u8] = b"LayerX/x-websearch/v1/principal\0";
const TRACE_DOMAIN: &[u8] = b"LayerX/x-websearch/v1/trace\0";
const ANONYMOUS_PRINCIPAL: &str = "anonymous-buyer";
const RPC_TIMEOUT: Duration = Duration::from_secs(10);
const RPC_RESPONSE_LIMIT: usize = 4_194_304;
const RECEIPT_HEX_LIMIT: usize = 2_097_152;
const PENDING_CODE: i64 = -32001;
const ASSET_MODULE: u16 = 1;
const SEND_OPERATION: u8 = 5;
const RECEIVE_OPERATION: u8 = 6;
const MAX_HOST_BYTES: usize = 255;

/// Milliseconds since the Unix epoch, as the gate reads its clock.
pub type Clock = fn() -> u64;

#[must_use]
pub fn system_clock() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| {
            u64::try_from(elapsed.as_millis()).unwrap_or(u64::MAX)
        })
}

/// Lowercase hexadecimal.
#[must_use]
pub fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut text = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        text.push(char::from(DIGITS[usize::from(byte >> 4)]));
        text.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    text
}

/// Decodes lowercase hexadecimal only.
#[must_use]
pub fn unhex(text: &str) -> Option<Vec<u8>> {
    const fn nibble(byte: u8) -> Option<u8> {
        match byte {
            b'0'..=b'9' => Some(byte - b'0'),
            b'a'..=b'f' => Some(byte - b'a' + 10),
            _ => None,
        }
    }
    if !text.len().is_multiple_of(2) {
        return None;
    }
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| Some((nibble(pair[0])? << 4) | nibble(pair[1])?))
        .collect()
}

fn unhex32(text: &str) -> Option<[u8; 32]> {
    unhex(text)?.try_into().ok()
}

fn sha256(parts: &[&[u8]]) -> [u8; 32] {
    let mut hash = Sha256::new();
    for part in parts {
        hash.update(part);
    }
    hash.finalize().into()
}

fn domain_hash(domain: Domain, bytes: &[u8]) -> [u8; 32] {
    sha256(&[domain.tag(), bytes])
}

/// The receiver's DID, derived from its Ed25519 public key.
#[must_use]
pub fn receiver_did(public_key: &[u8; 32]) -> String {
    format!("did:layerx:{}", hex(public_key))
}

/// The per-asset account `agent:<did>:asset:<asset id>`.
#[must_use]
pub fn asset_account(did: &str, asset_id: &[u8; 32]) -> String {
    format!("agent:{did}:asset:{}", hex(asset_id))
}

/// The account `did` pays or is paid in `asset`: the main account
/// `agent:<did>:main` for PAX, the kernel's native coin, and the per-asset
/// account for SID, USDC and USDL.
#[must_use]
pub fn wallet_account(did: &str, asset: &AcceptedAsset) -> String {
    if asset.symbol == AssetSymbol::Pax {
        format!("agent:{did}:main")
    } else {
        asset_account(did, &asset.asset_id)
    }
}

/// The account id of a reference under [`PROTOCOL_VERSION`].
#[must_use]
pub fn account_id(account: &str) -> Option<[u8; 32]> {
    account_identifiers(account).ok().map(|ids| ids[1])
}

/// The purpose every grant for this receiver's paid routes carries.
#[must_use]
pub fn purpose_hash(receiver_public_key: &[u8; 32]) -> [u8; 32] {
    sha256(&[PURPOSE_DOMAIN, receiver_public_key])
}

/// Decodes a payer-signed canonical grant, verifying its id and signature.
#[must_use]
pub fn decode_grant(bytes: &[u8], actor: &str) -> Option<Grant> {
    if bytes.len() != GRANT_BYTES {
        return None;
    }
    match Payment::decode(ModuleId::Asset, 7, bytes, actor.as_bytes()).ok()? {
        Payment::IssueGrant(grant) => Some(grant),
        _ => None,
    }
}

/// Everything one receiver-signed draw binds.
#[derive(Clone, Debug)]
pub struct DrawRequest<'a> {
    pub grant: &'a Grant,
    pub amount: u128,
    pub receiver_sequence: u64,
    pub identity_sequence: u64,
    pub idempotency_key: [u8; 32],
    pub network_id: u32,
    pub now_ms: u64,
    /// The fee limit the enclosing activity binds.
    pub fee_limit: u128,
}

/// A signed ordinal-6 Asset activity and its id.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedDraw {
    pub canonical: Vec<u8>,
    pub activity_id: [u8; 32],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DrawError {
    Authorization,
    Payload,
    Envelope,
    Signature,
    Identity,
}

impl std::fmt::Display for DrawError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Self::Authorization => "the receive authorization cannot be encoded",
            Self::Payload => "the receive payload is refused by the codec",
            Self::Envelope => "the draw envelope is refused by the codec",
            Self::Signature => "the draw cannot be signed",
            Self::Identity => "the signed draw has no activity id",
        })
    }
}

impl std::error::Error for DrawError {}

fn receive_authorization(
    receiver: &SigningKey,
    draw: &DrawRequest<'_>,
    context_hash: &[u8; 32],
) -> Result<[u8; 64], DrawError> {
    let grant = draw.grant;
    let mut message = Encoder::new(512);
    message
        .fixed(b"LXP:RECEIVE:v1")
        .and_then(|()| message.fixed(&grant.from))
        .and_then(|()| message.fixed(&grant.recipient))
        .and_then(|()| message.fixed(&grant.asset))
        .and_then(|()| message.u128(draw.amount))
        .and_then(|()| message.fixed(&grant.id))
        .and_then(|()| message.u64(draw.receiver_sequence))
        .and_then(|()| message.fixed(&draw.idempotency_key))
        .and_then(|()| message.fixed(context_hash))
        .and_then(|()| message.u8(1))
        .and_then(|()| message.fixed(&grant.recipient))
        .and_then(|()| message.fixed(context_hash))
        .and_then(|()| message.u32(draw.network_id))
        .and_then(|()| message.u16(PROTOCOL_VERSION))
        .map_err(|_| DrawError::Authorization)?;
    Ok(receiver
        .sign(&domain_hash(Domain::SignaturePreimage, &message.finish()))
        .to_bytes())
}

/// Builds and signs the ordinal-6 draw through the existing codecs: the
/// receiver authorizes the receive, and the receiver identity signs the
/// enclosing activity at its next identity sequence.
///
/// # Errors
/// Refuses a draw the receive or envelope codec rejects.
pub fn sign_draw(receiver: &SigningKey, draw: &DrawRequest<'_>) -> Result<SignedDraw, DrawError> {
    let grant = draw.grant;
    let public_key = receiver.verifying_key().to_bytes();
    let did = receiver_did(&public_key);
    let mut purpose = Vec::with_capacity(64);
    purpose.extend_from_slice(&grant.purpose_hash);
    if grant.has_reference {
        purpose.extend_from_slice(&grant.reference_hash);
    }
    let context_hash = domain_hash(Domain::ContextHash, &purpose);
    let signature = receive_authorization(receiver, draw, &context_hash)?;
    let payment = Payment::Receive {
        from: grant.from,
        to: grant.recipient,
        asset: grant.asset,
        amount: draw.amount,
        grant: grant.id,
        sequence: draw.receiver_sequence,
        idempotency_key: draw.idempotency_key,
        context_hash,
        receiver_authorization: ReceiverAuthorization {
            kind: 1,
            controller: grant.recipient,
            public_key,
            signature,
            signed_context_hash: context_hash,
            network_id: draw.network_id,
            protocol_version: PROTOCOL_VERSION,
        },
        payer_grant: Box::new(grant.clone()),
    };
    let (module, ordinal) = payment.activity_type();
    let payload = payment
        .encode(did.as_bytes())
        .map_err(|_| DrawError::Payload)?;
    let encoded = encode_payment_envelope(
        module,
        ordinal,
        &payload,
        &EnvelopeOptions {
            actor: &did,
            public_key,
            protocol_version: PROTOCOL_VERSION,
            network_id: draw.network_id,
            identity_sequence: draw.identity_sequence,
            idempotency_key: draw.idempotency_key,
            fee_limit: draw.fee_limit,
            not_before: draw.now_ms.saturating_sub(1_000),
            not_after: draw.now_ms.saturating_add(DRAW_VALIDITY_MS),
        },
    )
    .map_err(|_| DrawError::Envelope)?;
    let seed = Zeroizing::new(receiver.to_bytes());
    let local = LocalSigner::new(*seed);
    let mut future = sign_disclosed(
        &local,
        &encoded.canonical,
        &encoded.disclosure,
        &encoded.registry,
    );
    let mut context = std::task::Context::from_waker(std::task::Waker::noop());
    let std::task::Poll::Ready(outcome) = future.as_mut().poll(&mut context) else {
        return Err(DrawError::Signature);
    };
    let signed = outcome.map_err(|_| DrawError::Signature)?;
    let signature = Signature::new(signed.as_bytes()).map_err(|_| DrawError::Signature)?;
    let envelope = encoded.envelope.attach_signature(signature);
    let canonical = layerx_wire::activity::encode_signed_envelope(&envelope)
        .map_err(|_| DrawError::Envelope)?;
    let decoded = layerx_wire::activity::decode_signed(&canonical, &encoded.registry)
        .map_err(|_| DrawError::Envelope)?;
    let activity_id = layerx_wire::hash::activity_id(&decoded).map_err(|_| DrawError::Identity)?;
    Ok(SignedDraw {
        canonical,
        activity_id,
    })
}

/// One JSON-RPC answer from the gateway.
#[derive(Clone, Debug, PartialEq)]
pub enum RpcAnswer {
    Result(Value),
    Error { code: i64, data: Value },
}

impl RpcAnswer {
    fn pending(&self) -> bool {
        match self {
            Self::Error { code, data } => {
                *code == PENDING_CODE
                    && data.get("state").and_then(Value::as_str) == Some("pending")
            }
            Self::Result(value) => value.get("state").and_then(Value::as_str) == Some("pending"),
        }
    }
}

/// A JSON-RPC client for the configured gateway endpoint: TLS for https and
/// plain HTTP for the loopback endpoints the configuration allows.
#[derive(Clone, Debug)]
pub struct GatewayRpc {
    tls: bool,
    host: String,
    port: u16,
    authority: String,
    path: String,
    authorization_file: Option<PathBuf>,
}

impl GatewayRpc {
    /// # Errors
    /// Refuses an endpoint that is not an http or https URL with a host.
    pub fn new(endpoint: &str) -> Result<Self, GateError> {
        let (tls, rest) = if let Some(rest) = endpoint.strip_prefix("https://") {
            (true, rest)
        } else if let Some(rest) = endpoint.strip_prefix("http://") {
            (false, rest)
        } else {
            return Err(GateError::Endpoint);
        };
        if endpoint.chars().any(char::is_whitespace) || endpoint.contains(['@', '#', '?']) {
            return Err(GateError::Endpoint);
        }
        let (authority, path) = rest
            .split_once('/')
            .map_or((rest, "/".to_owned()), |(authority, path)| {
                (authority, format!("/{path}"))
            });
        let default_port = if tls { 443 } else { 80 };
        let (host, port) = if let Some(bracketed) = authority.strip_prefix('[') {
            let (host, tail) = bracketed.split_once(']').ok_or(GateError::Endpoint)?;
            let port = match tail.strip_prefix(':') {
                Some(port) => port.parse().map_err(|_| GateError::Endpoint)?,
                None if tail.is_empty() => default_port,
                None => return Err(GateError::Endpoint),
            };
            (host, port)
        } else if let Some((host, port)) = authority.rsplit_once(':') {
            (host, port.parse().map_err(|_| GateError::Endpoint)?)
        } else {
            (authority, default_port)
        };
        if host.is_empty() {
            return Err(GateError::Endpoint);
        }
        Ok(Self {
            tls,
            host: host.to_owned(),
            port,
            authority: authority.to_owned(),
            path,
            authorization_file: None,
        })
    }

    pub fn with_authorization_file(mut self, path: &Path) -> Result<Self, GateError> {
        if !self.tls && self.host.parse::<std::net::IpAddr>().is_ok_and(|ip| !ip.is_loopback()) {
            return Err(GateError::Authorization);
        }
        if !self.tls && self.host.parse::<std::net::IpAddr>().is_err() && self.host != "localhost" {
            return Err(GateError::Authorization);
        }
        let _ = gateway_authorization(path)?;
        self.authorization_file = Some(path.to_owned());
        Ok(self)
    }

    /// Calls one method. `None` means the answer is unknown: the gateway was
    /// unreachable or its response was not a well-formed JSON-RPC answer.
    #[must_use]
    pub fn call(&self, method: &str, params: &Value) -> Option<RpcAnswer> {
        let body =
            json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).to_string();
        let authorization = match &self.authorization_file {
            Some(path) => Some(gateway_authorization(path).ok()?),
            None => None,
        };
        let header = Zeroizing::new(authorization.as_ref().map_or_else(String::new,
            |value| format!("Authorization: {}\r\n", value.as_str())));
        let authorization_header = header.as_str();
        let request = Zeroizing::new(format!(
            "POST {} HTTP/1.1\r\nHost: {}\r\n{authorization_header}Content-Type: application/json\r\nAccept: application/json\r\nConnection: close\r\nContent-Length: {}\r\n\r\n{body}",
            self.path,
            self.authority,
            body.len()
        ));
        let address = (self.host.as_str(), self.port)
            .to_socket_addrs()
            .ok()?
            .next()?;
        if self.authorization_file.is_some() && !self.tls && !address.ip().is_loopback() {
            return None;
        }
        let stream = TcpStream::connect_timeout(&address, RPC_TIMEOUT).ok()?;
        stream.set_read_timeout(Some(RPC_TIMEOUT)).ok()?;
        stream.set_write_timeout(Some(RPC_TIMEOUT)).ok()?;
        let raw = if self.tls {
            let connector = native_tls::TlsConnector::new().ok()?;
            let mut stream = connector.connect(&self.host, stream).ok()?;
            exchange(&mut stream, request.as_bytes())?
        } else {
            let mut stream = stream;
            exchange(&mut stream, request.as_bytes())?
        };
        decode_answer(&decode_http(&raw)?)
    }
}

fn gateway_authorization(path: &Path) -> Result<Zeroizing<String>, GateError> {
    use std::os::unix::fs::MetadataExt as _;
    if !path.is_absolute() || path.components().any(|part| part.as_os_str().to_str()
        .is_some_and(|part| part == ".env" || part.starts_with(".env."))) {
        return Err(GateError::Authorization);
    }
    if fs::canonicalize(path).map_err(|_| GateError::Authorization)? != path {
        return Err(GateError::Authorization);
    }
    let file = fs::OpenOptions::new().read(true).custom_flags(0x20000 | 0x800)
        .open(path).map_err(|_| GateError::Authorization)?;
    let metadata = file.metadata().map_err(|_| GateError::Authorization)?;
    let owner = fs::metadata("/proc/self").map_err(|_| GateError::Authorization)?.uid();
    if !metadata.is_file() || metadata.nlink() != 1 || metadata.uid() != owner
        || metadata.mode() & 0o077 != 0 || metadata.len() > 256 {
        return Err(GateError::Authorization);
    }
    let mut bytes = Zeroizing::new(Vec::new());
    file.take(257).read_to_end(&mut bytes).map_err(|_| GateError::Authorization)?;
    if bytes.len() > 256 { return Err(GateError::Authorization); }
    let text = std::str::from_utf8(&bytes).map_err(|_| GateError::Authorization)?;
    let text = text.strip_suffix("\r\n").or_else(|| text.strip_suffix('\n')).unwrap_or(text);
    let (id, secret) = text.strip_prefix("LayerX-Key ").and_then(|value| value.split_once(':'))
        .ok_or(GateError::Authorization)?;
    if id.is_empty() || id.len() > 64 || !id.bytes().all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
        || !secret.strip_prefix("lxp_live_").is_some_and(|value| value.len() == 64
            && value.bytes().all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))) {
        return Err(GateError::Authorization);
    }
    Ok(Zeroizing::new(text.to_owned()))
}

fn exchange(stream: &mut (impl io::Read + io::Write), request: &[u8]) -> Option<Vec<u8>> {
    stream.write_all(request).ok()?;
    stream.flush().ok()?;
    let mut raw = Vec::new();
    let limit = u64::try_from(RPC_RESPONSE_LIMIT).ok()?.saturating_add(1);
    stream.take(limit).read_to_end(&mut raw).ok()?;
    (raw.len() <= RPC_RESPONSE_LIMIT).then_some(raw)
}

fn decode_http(raw: &[u8]) -> Option<Value> {
    let split = raw.windows(4).position(|window| window == b"\r\n\r\n")?;
    let head = std::str::from_utf8(&raw[..split]).ok()?;
    let mut lines = head.split("\r\n");
    let status = lines.next()?;
    if !status.starts_with("HTTP/1.") || status.split_whitespace().nth(1) != Some("200") {
        return None;
    }
    let mut length = None;
    let mut chunked = false;
    for line in lines {
        let (name, value) = line.split_once(':')?;
        if name.eq_ignore_ascii_case("transfer-encoding") {
            if chunked || !value.trim().eq_ignore_ascii_case("chunked") {
                return None;
            }
            chunked = true;
        }
        if name.eq_ignore_ascii_case("content-length") {
            if length.is_some() {
                return None;
            }
            length = Some(value.trim().parse::<usize>().ok()?);
        }
    }
    let body = &raw[split + 4..];
    if chunked {
        if length.is_some() {
            return None;
        }
        return serde_json::from_slice(&dechunk(body)?).ok();
    }
    if length.is_some_and(|length| length != body.len()) {
        return None;
    }
    serde_json::from_slice(body).ok()
}

/// Joins the chunks of a chunked transfer encoding body, refusing a body
/// that is cut short, carries anything after its last chunk, or names a
/// chunk size that is not hexadecimal.
fn dechunk(mut body: &[u8]) -> Option<Vec<u8>> {
    let mut joined = Vec::new();
    loop {
        let line_end = body.windows(2).position(|window| window == b"\r\n")?;
        let size_line = std::str::from_utf8(&body[..line_end]).ok()?;
        let size_text = size_line.split(';').next()?.trim();
        let size = usize::from_str_radix(size_text, 16).ok()?;
        body = &body[line_end + 2..];
        if size == 0 {
            loop {
                let trailer_end = body.windows(2).position(|window| window == b"\r\n")?;
                let trailer = &body[..trailer_end];
                body = &body[trailer_end + 2..];
                if trailer.is_empty() {
                    break;
                }
            }
            return body.is_empty().then_some(joined);
        }
        joined.extend_from_slice(body.get(..size)?);
        body = body.get(size..)?.strip_prefix(b"\r\n")?;
    }
}

fn decode_answer(value: &Value) -> Option<RpcAnswer> {
    if value.get("jsonrpc") != Some(&json!("2.0"))
        || value.get("id") != Some(&json!(1))
        || value.get("result").is_some() == value.get("error").is_some()
    {
        return None;
    }
    if let Some(error) = value.get("error") {
        return Some(RpcAnswer::Error {
            code: error.get("code")?.as_i64()?,
            data: error.get("data").cloned().unwrap_or(Value::Null),
        });
    }
    value.get("result").cloned().map(RpcAnswer::Result)
}

/// What the data directory keeps for one payment, keyed by the seller's
/// idempotency key.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PaymentRecord {
    pub principal: String,
    pub request_digest: String,
    pub resource: String,
    pub offer: Value,
    pub payer: Option<String>,
    pub receive_key: Option<String>,
    pub activity: Option<String>,
    pub activity_id: Option<String>,
    pub attempted: bool,
    pub receipt: Option<String>,
    pub receipt_digest: Option<String>,
    pub released: bool,
}

/// File-backed payment associations under `<data_dir>/payments`.
#[derive(Clone, Debug)]
pub struct PaymentStore {
    root: PathBuf,
}

const REQUESTS: &str = "requests";
const RECEIVE_KEYS: &str = "receive-keys";
const RECEIPTS: &str = "receipts";
const DELIVERIES: &str = "deliveries";

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum DeliveryState {
    Pending,
    Computing,
    Failed,
    Ready,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct DeliveryResponse {
    status: u16,
    content_type: String,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
    body_digest: String,
    canonical_digest: Option<String>,
    canonical_content: Option<Vec<u8>>,
}

impl DeliveryResponse {
    fn from_response(response: Response, canonical_content: Option<Vec<u8>>) -> Self {
        Self {
            canonical_digest: canonical_content
                .as_ref()
                .map(|bytes| hex(&crate::canonical::content_digest(bytes))),
            canonical_content,
            body_digest: hex(&sha256(&[&response.body])),
            status: response.status,
            content_type: response.content_type,
            headers: response.headers,
            body: response.body,
        }
    }

    fn response(&self) -> io::Result<Response> {
        if self.body_digest != hex(&sha256(&[&self.body])) || !(100..=599).contains(&self.status) {
            return Err(io::ErrorKind::InvalidData.into());
        }
        let digests: Vec<_> = self.headers.iter()
            .filter(|(name, _)| name.eq_ignore_ascii_case("Content-Digest")).collect();
        if digests.len() != 1
            || digests[0].1 != format!("sha-256=:{}:", STANDARD.encode(sha256(&[&self.body])))
            || self.content_type != "application/json"
        {
            return Err(io::ErrorKind::InvalidData.into());
        }
        match (&self.canonical_content, &self.canonical_digest) {
            (Some(bytes), Some(digest)) => {
                if *digest != hex(&crate::canonical::content_digest(bytes)) {
                    return Err(io::ErrorKind::InvalidData.into());
                }
                validate_canonical_response(&self.body, bytes)?;
            }
            (None, None) if self.status >= 400 => {}
            _ => return Err(io::ErrorKind::InvalidData.into()),
        }
        Ok(Response {
            status: self.status,
            content_type: self.content_type.clone(),
            headers: self.headers.clone(),
            body: self.body.clone(),
        })
    }
}

fn validate_canonical_response(body: &[u8], bytes: &[u8]) -> io::Result<()> {
    let content = crate::canonical::CanonicalContent::parse(bytes)
        .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
    let value: Value =
        serde_json::from_slice(body).map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
    if value.get("digest").and_then(Value::as_str)
        != Some(hex(&crate::canonical::content_digest(bytes)).as_str())
        || value.get("media_type").and_then(Value::as_str) != Some(content.media_type.as_str())
    {
        return Err(io::ErrorKind::InvalidData.into());
    }
    let matches = match content.kind {
        crate::canonical::ContentKind::Search => {
            value
                .get("query")
                .and_then(Value::as_str)
                .map(str::as_bytes)
                == Some(content.payload.as_slice())
                && serde_json::from_str::<Value>(&content.text).ok().as_ref()
                    == value.get("results")
        }
        crate::canonical::ContentKind::Fetch => {
            value.get("url").and_then(Value::as_str).map(str::as_bytes)
                == Some(content.payload.as_slice())
                && value.get("text").and_then(Value::as_str) == Some(content.text.as_str())
                && value.get("length").and_then(Value::as_u64) == Some(content.text.len() as u64)
        }
        crate::canonical::ContentKind::Api => false,
    };
    if matches {
        Ok(())
    } else {
        Err(io::ErrorKind::InvalidData.into())
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct DeliveryRecord {
    version: u8,
    principal: String,
    request_digest: String,
    resource: String,
    receipt_digest: String,
    state: DeliveryState,
    response: Option<DeliveryResponse>,
    #[serde(default)]
    failures: u32,
    #[serde(default)]
    retry_after_ms: u64,
}

impl DeliveryRecord {
    fn pending(payment: &PaymentRecord, receipt_digest: &[u8; 32]) -> Self {
        Self {
            version: 1,
            principal: payment.principal.clone(),
            request_digest: payment.request_digest.clone(),
            resource: payment.resource.clone(),
            receipt_digest: hex(receipt_digest),
            state: DeliveryState::Pending,
            response: None,
            failures: 0,
            retry_after_ms: 0,
        }
    }

    fn validate(&self, payment: &PaymentRecord) -> io::Result<()> {
        if self.version != 1
            || self.principal != payment.principal
            || self.request_digest != payment.request_digest
            || self.resource != payment.resource
            || payment.receipt.is_none()
            || payment.receipt_digest.as_deref() != Some(self.receipt_digest.as_str())
            || unhex32(&self.receipt_digest).is_none()
        {
            return Err(io::ErrorKind::InvalidData.into());
        }
        if let Some(response) = &self.response {
            let headers: Vec<_> = response
                .headers
                .iter()
                .filter(|(name, _)| name.eq_ignore_ascii_case(PAYMENT_RESPONSE))
                .collect();
            if headers.len() != 1 {
                return Err(io::ErrorKind::InvalidData.into());
            }
            let settlement: layerx_x402::model::SettlementResponse = serde_json::from_slice(
                &STANDARD
                    .decode(&headers[0].1)
                    .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?,
            )
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
            settlement
                .validate_wire()
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
            let binding = settlement
                .extensions
                .get("x-websearch")
                .ok_or(io::ErrorKind::InvalidData)?;
            let evidence = settlement
                .extensions
                .get("layerx")
                .ok_or(io::ErrorKind::InvalidData)?;
            let receipt = evidence
                .get("receipt")
                .and_then(Value::as_str)
                .and_then(|receipt| STANDARD.decode(receipt).ok())
                .ok_or(io::ErrorKind::InvalidData)?;
            if !settlement.success
                || settlement.transaction != format!("lxp:{}", self.receipt_digest)
                || settlement.payer.as_deref() != payment.payer.as_deref()
                || payment.offer.get("network").and_then(Value::as_str)
                    != Some(settlement.network.as_str())
                || settlement
                    .amount
                    .map(|amount| amount.value().to_string())
                    .as_deref()
                    != payment.offer.get("amount").and_then(Value::as_str)
                || evidence.get("receiptDigest").and_then(Value::as_str)
                    != Some(self.receipt_digest.as_str())
                || evidence.get("verificationLevel").and_then(Value::as_str)
                    != Some("sequencer-signed")
                || payment.receipt.as_deref() != Some(hex(&receipt).as_str())
                || binding.get("resource").and_then(Value::as_str) != Some(self.resource.as_str())
                || binding.get("receiptDigest").and_then(Value::as_str)
                    != Some(self.receipt_digest.as_str())
                || binding.get("responseDigest").and_then(Value::as_str)
                    != Some(response.body_digest.as_str())
                || binding.get("contentDigest").and_then(Value::as_str)
                    != response.canonical_digest.as_deref()
                || binding.get("delivery").and_then(Value::as_str)
                    != Some(if self.state == DeliveryState::Ready {
                        "ready"
                    } else {
                        "failed"
                    })
            {
                return Err(io::ErrorKind::InvalidData.into());
            }
            if let Some(bytes) = &response.canonical_content {
                let content = crate::canonical::CanonicalContent::parse(bytes)
                    .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
                let prefix = match content.kind {
                    crate::canonical::ContentKind::Search => "GET /search?q=",
                    crate::canonical::ContentKind::Fetch => "GET /fetch?url=",
                    crate::canonical::ContentKind::Api => {
                        return Err(io::ErrorKind::InvalidData.into())
                    }
                };
                let payload = std::str::from_utf8(&content.payload)
                    .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
                if self.resource != format!("{prefix}{}", encode_query_value(payload)) {
                    return Err(io::ErrorKind::InvalidData.into());
                }
            }
        }
        match (&self.state, &self.response) {
            (DeliveryState::Pending | DeliveryState::Computing, None) => Ok(()),
            (DeliveryState::Failed, Some(response)) if response.status >= 400 => {
                response.response().map(|_| ())
            }
            (DeliveryState::Ready, Some(response)) if (200..300).contains(&response.status) => {
                response.response().map(|_| ())
            }
            _ => Err(io::ErrorKind::InvalidData.into()),
        }
    }
}

impl PaymentStore {
    /// # Errors
    /// Returns the I/O error that prevented creating the store directories.
    pub fn open(data_dir: &Path) -> io::Result<Self> {
        let root = data_dir.join("payments");
        for index in [REQUESTS, RECEIVE_KEYS, RECEIPTS, DELIVERIES] {
            fs::DirBuilder::new()
                .recursive(true)
                .mode(0o700)
                .create(root.join(index))?;
        }
        fs::File::open(&root)?.sync_all()?;
        fs::File::open(data_dir)?.sync_all()?;
        Ok(Self { root })
    }

    fn path(&self, index: &str, name: &str) -> PathBuf {
        self.root.join(index).join(name)
    }

    /// # Errors
    /// Returns an I/O error, or `InvalidData` for a record that does not parse.
    pub fn load(&self, key: &[u8; 32]) -> io::Result<Option<PaymentRecord>> {
        match fs::read(self.path(REQUESTS, &hex(key))) {
            Ok(bytes) => serde_json::from_slice(&bytes)
                .map(Some)
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData)),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error),
        }
    }

    fn save(&self, key: &[u8; 32], record: &PaymentRecord) -> io::Result<()> {
        self.save_json(REQUESTS, key, record)
    }

    fn save_json(&self, index: &str, key: &[u8; 32], record: &impl Serialize) -> io::Result<()> {
        let bytes =
            serde_json::to_vec(record).map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        let path = self.path(index, &hex(key));
        let staging = self.path(index, &format!("{}.staging", hex(key)));
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(0o600)
            .open(&staging)?;
        file.write_all(&bytes)?;
        file.sync_all()?;
        fs::rename(&staging, &path)?;
        fs::File::open(self.root.join(index))?.sync_all()
    }

    fn delivery(&self, key: &[u8; 32]) -> io::Result<Option<DeliveryRecord>> {
        match fs::read(self.path(DELIVERIES, &hex(key))) {
            Ok(bytes) => serde_json::from_slice(&bytes)
                .map(Some)
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData)),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error),
        }
    }

    /// Binds `name` in `index` to `owner` once. Returns whether `owner` holds it.
    fn claim(&self, index: &str, name: &str, owner: &[u8; 32]) -> io::Result<bool> {
        let path = self.path(index, name);
        let staging = self.path(index, &format!("{name}.{}.staging", hex(owner)));
        match fs::remove_file(&staging) {
            Ok(()) => {}
            Err(error) if error.kind() == io::ErrorKind::NotFound => {}
            Err(error) => return Err(error),
        }
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&staging)?;
        file.write_all(hex(owner).as_bytes())?;
        file.sync_all()?;
        let claimed = match fs::hard_link(&staging, &path) {
            Ok(()) => {
                fs::File::open(self.root.join(index))?.sync_all()?;
                true
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => {
                fs::read(&path)? == hex(owner).as_bytes()
            }
            Err(error) => return Err(error),
        };
        fs::remove_file(staging)?;
        Ok(claimed)
    }

    fn holder(&self, index: &str, name: &str) -> io::Result<Option<Vec<u8>>> {
        match fs::read(self.path(index, name)) {
            Ok(bytes) => Ok(Some(bytes)),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error),
        }
    }

    /// Marks the payment under `key` released by `receipt_digest`. Returns
    /// `false`, releasing nothing, when the receipt already released a
    /// request or this payment was already released.
    ///
    /// # Errors
    /// Returns the I/O error that prevented recording the release.
    pub fn release(&self, key: &[u8; 32], receipt_digest: &[u8; 32]) -> io::Result<bool> {
        let Some(mut record) = self.load(key)? else {
            return Ok(false);
        };
        if record.released
            || record.receipt_digest.as_deref() != Some(hex(receipt_digest).as_str())
            || !self.claim(RECEIPTS, &hex(receipt_digest), key)?
        {
            return Ok(false);
        }
        record.released = true;
        self.save(key, &record)?;
        Ok(true)
    }
}

#[derive(Debug)]
pub enum GateError {
    Assets(AssetRefusal),
    Endpoint,
    Authorization,
    Adapter,
    Store(io::Error),
    Payer,
    Conformance,
}

impl std::fmt::Display for GateError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Assets(refusal) => write!(f, "payment assets refused: {refusal}"),
            Self::Endpoint => f.write_str("the gateway endpoint is not an http or https URL"),
            Self::Authorization => f.write_str("gateway authorization file or transport refused"),
            Self::Adapter => f.write_str("the 402LXP adapter cannot be registered"),
            Self::Store(error) => write!(f, "the payment store cannot be opened: {error}"),
            Self::Payer => f.write_str("payment.payer_did does not derive a payer account"),
            Self::Conformance => {
                f.write_str("payment.conformance_suite does not hold the pinned conformance suite")
            }
        }
    }
}

impl std::error::Error for GateError {}

struct Receiver {
    key: SigningKey,
    did: String,
    purpose: [u8; 32],
    accounts: [[u8; 32]; 4],
}

/// The payment hook every paid route calls.
pub struct PaymentGate {
    assets: AcceptedAssets,
    receiver: Receiver,
    network: String,
    network_id: u32,
    trust: SequencerTrust,
    payer: Option<String>,
    draw_fee_limit: u128,
    rpc: GatewayRpc,
    store: PaymentStore,
    clock: Clock,
    gateway: Mutex<GatewayCore>,
    delivery_lock: Mutex<()>,
    _lease: fs::File,
}

impl PaymentGate {
    /// `conformance` is the caller's pinned conformance suite for the x402
    /// adapter the seller settles through.
    ///
    /// # Errors
    /// Refuses the configured assets, an unusable gateway endpoint, a
    /// payment store that cannot be opened, an adapter that cannot be
    /// registered, a configured payer that derives no account, and a
    /// configured conformance suite directory whose recorded exchanges are
    /// not the ones `conformance` pins.
    pub fn new(
        config: &Config,
        receiver: &SigningKey,
        conformance: ConformanceSuite,
        clock: Clock,
    ) -> Result<Self, GateError> {
        let assets = AcceptedAssets::new(&config.assets).map_err(GateError::Assets)?;
        let payment = &config.payment;
        if let Some(payer) = payment.payer_did.as_deref() {
            if !payer_did_valid(payer)
                || assets
                    .all()
                    .iter()
                    .any(|asset| account_id(&wallet_account(payer, asset)).is_none())
            {
                return Err(GateError::Payer);
            }
        }
        if let Some(directory) = payment.conformance_suite.as_deref() {
            if recorded_suite(directory).ok()
                != Some((conformance.suite_digest(), conformance.vector_count()))
            {
                return Err(GateError::Conformance);
            }
        }
        let rpc = GatewayRpc::new(&config.gateway.endpoint)?;
        let rpc = match &config.gateway.authorization_file {
            Some(path) => rpc.with_authorization_file(path)?,
            None => rpc,
        };
        let store = PaymentStore::open(&config.data_dir).map_err(GateError::Store)?;
        let lease = fs::OpenOptions::new().read(true).write(true).create(true)
            .truncate(false).mode(0o600).custom_flags(0x20000)
            .open(store.root.join("writer.lock")).map_err(GateError::Store)?;
        lease.try_lock().map_err(|_| GateError::Store(io::Error::other("payment journal already open")))?;
        let public_key = receiver.verifying_key().to_bytes();
        let did = receiver_did(&public_key);
        let mut accounts = [[0; 32]; 4];
        for (slot, asset) in accounts.iter_mut().zip(assets.all()) {
            *slot = account_id(&wallet_account(&did, asset)).ok_or(GateError::Adapter)?;
        }
        let trace = TraceId::mint([0; 16]);
        let mut gateway = GatewayCore::new();
        let descriptor = x402_adapter_descriptor(conformance).map_err(|_| GateError::Adapter)?;
        gateway
            .register_adapter(descriptor, &trace, clock() / 1_000)
            .map_err(|_| GateError::Adapter)?;
        Ok(Self {
            assets,
            receiver: Receiver {
                key: receiver.clone(),
                purpose: purpose_hash(&public_key),
                did,
                accounts,
            },
            network: format!("layerx:{}", config.kernel_network_id),
            network_id: config.kernel_network_id,
            trust: config.gateway.sequencer,
            payer: payment.payer_did.clone(),
            draw_fee_limit: payment.draw_fee_limit,
            rpc,
            store,
            clock,
            gateway: Mutex::new(gateway),
            delivery_lock: Mutex::new(()),
            _lease: lease,
        })
    }

    #[must_use]
    pub const fn assets(&self) -> &AcceptedAssets {
        &self.assets
    }

    #[must_use]
    pub fn receiver_did(&self) -> &str {
        &self.receiver.did
    }

    #[must_use]
    pub const fn purpose(&self) -> [u8; 32] {
        self.receiver.purpose
    }

    /// The configured payer every draw is taken from, if any.
    #[must_use]
    pub fn payer(&self) -> Option<&str> {
        self.payer.as_deref()
    }

    /// The fee limit each draw is signed with and may be charged.
    #[must_use]
    pub const fn draw_fee_limit(&self) -> u128 {
        self.draw_fee_limit
    }

    /// The account id the receiver is paid into for `asset`.
    #[must_use]
    pub fn payee(&self, asset: &AcceptedAsset) -> [u8; 32] {
        let index = self
            .assets
            .all()
            .iter()
            .position(|candidate| candidate.asset_id == asset.asset_id)
            .unwrap_or_default();
        self.receiver.accounts[index]
    }

    #[must_use]
    pub const fn store(&self) -> &PaymentStore {
        &self.store
    }

    fn offer(
        &self,
        asset: &AcceptedAsset,
        scheme: &str,
        payer: Option<&str>,
    ) -> PaymentRequirements {
        let mut layerx = json!({
            "commitment": COMMITMENT,
            "account": wallet_account(&self.receiver.did, asset),
            "currency": asset.symbol.code(),
        });
        if let (Some(payer), Some(terms)) = (payer, layerx.as_object_mut()) {
            let payer = account_id(&wallet_account(payer, asset)).map(|id| hex(&id));
            terms.insert("payer".to_owned(), json!(payer));
            terms.insert("purposeHash".to_owned(), json!(hex(&self.receiver.purpose)));
        }
        PaymentRequirements {
            scheme: scheme.to_owned(),
            network: self.network.clone(),
            amount: AtomicAmount::from_u128(asset.price),
            asset: asset.id_hex(),
            pay_to: hex(&self.payee(asset)),
            max_timeout_seconds: OFFER_TIMEOUT_SECONDS,
            extra: Some(json!({ "layerx": layerx })),
        }
    }

    /// The offers for one request: per asset a metered draw bound to the
    /// named payer's grant, then the exact alternative.
    ///
    /// # Errors
    /// Answers 400 for a request without a usable `Host` header.
    pub fn payment_required(
        &self,
        request: &Request,
        payer: Option<&str>,
    ) -> Result<PaymentRequired, Response> {
        let host = request
            .header("Host")
            .filter(|host| {
                !host.is_empty()
                    && host.len() <= MAX_HOST_BYTES
                    && host
                        .bytes()
                        .all(|byte| byte.is_ascii_alphanumeric() || b".-:[]".contains(&byte))
            })
            .ok_or_else(|| Response::error(400, "missing_host"))?;
        let target = canonical_target(request)?;
        let mut accepts = Vec::with_capacity(8);
        for asset in self.assets.all() {
            if payer.is_some() {
                accepts.push(self.offer(asset, METERED, payer));
            }
            accepts.push(self.offer(asset, EXACT, None));
        }
        Ok(PaymentRequired {
            x402_version: X402_VERSION,
            error: None,
            resource: ResourceInfo {
                url: format!("http://{host}{target}"),
                description: Some(describe(request.route).to_owned()),
                mime_type: Some("application/json".to_owned()),
                service_name: Some("x-websearch".to_owned()),
                tags: Vec::new(),
                icon_url: None,
            },
            accepts,
            extensions: BTreeMap::new(),
        })
    }

    fn challenge(seller: &Seller, error: &str) -> Response {
        match seller.payment_required() {
            Ok(signal) => {
                let body =
                    serde_json::to_vec(&json!({"error": error, "paymentRequired": signal.body}))
                        .unwrap_or_default();
                Response::json(signal.status, body).with_header(PAYMENT_REQUIRED, &signal.header)
            }
            Err(_) => Response::error(503, "payment_unavailable"),
        }
    }

    /// Settles the request's `PAYMENT-SIGNATURE` and calls `release` only
    /// after a verified receipt that has released no other request. Without
    /// a payment it answers 402 with `PAYMENT-REQUIRED`.
    pub fn settle(&self, request: &Request, release: &dyn Fn(&Request) -> Response) -> Response {
        self.settle_request(request, release, false)
    }

    fn settle_request(
        &self,
        request: &Request,
        release: &dyn Fn(&Request) -> Response,
        resumable: bool,
    ) -> Response {
        let _delivery = self
            .delivery_lock
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
        let resource = match resource_binding(request) {
            Ok(resource) => resource,
            Err(response) => return response,
        };
        let payer = match payer_did(request, self.payer.as_deref()) {
            Ok(payer) => payer,
            Err(response) => return response,
        };
        let required = match self.payment_required(request, payer) {
            Ok(required) => required,
            Err(response) => return response,
        };
        let Ok(seller) = Seller::new(required) else {
            return Response::error(503, "payment_unavailable");
        };
        let Some(header) = request.header(PAYMENT_SIGNATURE) else {
            return Self::challenge(&seller, "payment_required");
        };
        let principal = principal(payer);
        let trace = TraceId::mint(trace_entropy(header));
        let now_ms = (self.clock)();
        let mut plane = Plane {
            gate: self,
            resource: &resource,
            payer,
            now_ms,
            key: None,
            refusal: None,
        };
        let outcome = {
            let mut gateway = self.gateway.lock().unwrap_or_else(PoisonError::into_inner);
            seller.settle(
                &mut gateway,
                &principal,
                header,
                &mut plane,
                &trace,
                now_ms / 1_000,
            )
        };
        match outcome {
            Err(_) => Self::challenge(&seller, plane.refusal.unwrap_or("payment_invalid")),
            Ok(SellerOutcome::Pending) => {
                Response::error(503, "payment_pending").with_header("Retry-After", "1")
            }
            Ok(SellerOutcome::Refused { header, response }) => {
                let reason = response.error_reason.unwrap_or_default();
                Self::challenge(&seller, &reason).with_header(PAYMENT_RESPONSE, &header)
            }
            Ok(SellerOutcome::Settled {
                header,
                receipt_digest,
                ..
            }) => {
                let Some(key) = plane.key else {
                    return Self::challenge(&seller, "payment_invalid");
                };
                if resumable {
                    self.deliver(&key, &receipt_digest, &header, request, release)
                        .unwrap_or_else(|_| Response::error(503, "payment_store_unavailable"))
                } else {
                    match self.store.release(&key, &receipt_digest) {
                        Ok(true) => release(request).with_header(PAYMENT_RESPONSE, &header),
                        Ok(false) => Self::challenge(&seller, "receipt_consumed"),
                        Err(_) => Response::error(503, "payment_store_unavailable"),
                    }
                }
            }
        }
    }

    fn deliver(
        &self,
        key: &[u8; 32],
        receipt_digest: &[u8; 32],
        payment_header: &str,
        request: &Request,
        handler: &dyn Fn(&Request) -> Response,
    ) -> io::Result<Response> {
        let payment = self.store.load(key)?.ok_or(io::ErrorKind::InvalidData)?;
        if payment.receipt.is_none()
            || payment.receipt_digest.as_deref() != Some(hex(receipt_digest).as_str())
            || !self.store.claim(RECEIPTS, &hex(receipt_digest), key)?
        {
            return Err(io::ErrorKind::InvalidData.into());
        }
        let mut delivery = match self.store.delivery(key)? {
            Some(delivery) => delivery,
            None => {
                if payment.released {
                    return Ok(Response::error(409, "receipt_consumed"));
                }
                let delivery = DeliveryRecord::pending(&payment, receipt_digest);
                self.store.save_json(DELIVERIES, key, &delivery)?;
                delivery
            }
        };
        delivery.validate(&payment)?;
        if delivery.state == DeliveryState::Ready {
            return delivery
                .response
                .as_ref()
                .ok_or(io::ErrorKind::InvalidData)?
                .response();
        }
        if delivery.state == DeliveryState::Failed && (self.clock)() < delivery.retry_after_ms {
            let remaining = delivery.retry_after_ms.saturating_sub((self.clock)()).div_ceil(1_000);
            let mut response = delivery.response.as_ref().ok_or(io::ErrorKind::InvalidData)?.response()?;
            response.headers.retain(|(name, _)| !name.eq_ignore_ascii_case("Retry-After"));
            return Ok(response.with_header("Retry-After", &remaining.max(1).to_string()));
        }
        delivery.state = DeliveryState::Computing;
        delivery.response = None;
        delivery.retry_after_ms = 0;
        self.store.save_json(DELIVERIES, key, &delivery)?;
        let mut response =
            std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| handler(request)))
                .unwrap_or_else(|_| Response::error(500, "delivery_failed"));
        if !(200..300).contains(&response.status) && response.status < 400 {
            response = Response::error(502, "delivery_failed");
        }
        let canonical_content = if (200..300).contains(&response.status) {
            match self.canonical_result(request, &response) {
                Ok(bytes) => Some(bytes),
                Err(_) => {
                    response = Response::error(500, "delivery_result_invalid");
                    None
                }
            }
        } else {
            None
        };
        let mut settlement: layerx_x402::model::SettlementResponse = serde_json::from_slice(
            &STANDARD
                .decode(payment_header)
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?,
        )
        .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        settlement.extensions.insert("x-websearch".to_owned(), json!({
            "resource": payment.resource,
            "receiptDigest": hex(receipt_digest),
            "delivery": if canonical_content.is_some() { "ready" } else { "failed" },
            "contentDigest": canonical_content.as_ref().map(|bytes| hex(&crate::canonical::content_digest(bytes))),
            "responseDigest": hex(&sha256(&[&response.body])),
        }));
        settlement
            .validate_wire()
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        let payment_header = STANDARD.encode(
            serde_json::to_vec(&settlement)
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?,
        );
        response.headers.retain(|(name, _)| {
            !name.eq_ignore_ascii_case(PAYMENT_RESPONSE)
                && !name.eq_ignore_ascii_case("Content-Digest")
        });
        let digest = STANDARD.encode(sha256(&[&response.body]));
        response = response
            .with_header(PAYMENT_RESPONSE, &payment_header)
            .with_header("Content-Digest", &format!("sha-256=:{digest}:"));
        delivery.state = if (200..300).contains(&response.status) {
            delivery.failures = 0;
            delivery.retry_after_ms = 0;
            DeliveryState::Ready
        } else {
            delivery.failures = delivery.failures.saturating_add(1);
            let delay = (1_u64 << delivery.failures.saturating_sub(1).min(6)).min(60);
            delivery.retry_after_ms = (self.clock)().saturating_add(delay * 1_000);
            response.headers.retain(|(name, _)| !name.eq_ignore_ascii_case("Retry-After"));
            response = response.with_header("Retry-After", &delay.to_string());
            DeliveryState::Failed
        };
        delivery.response = Some(DeliveryResponse::from_response(response, canonical_content));
        delivery.validate(&payment)?;
        self.store.save_json(DELIVERIES, key, &delivery)?;
        delivery
            .response
            .as_ref()
            .ok_or(io::ErrorKind::InvalidData)?
            .response()
    }

    fn canonical_result(&self, request: &Request, response: &Response) -> io::Result<Vec<u8>> {
        let value: Value = serde_json::from_slice(&response.body)
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        let digest = value
            .get("digest")
            .and_then(Value::as_str)
            .and_then(unhex32)
            .ok_or(io::ErrorKind::InvalidData)?;
        let path = self
            .store
            .root
            .parent()
            .ok_or(io::ErrorKind::InvalidData)?
            .join("content")
            .join(hex(&digest));
        let mut bytes = Vec::new();
        fs::File::open(path)?
            .take((crate::content::MAX_CONTENT_BYTES as u64) + 1)
            .read_to_end(&mut bytes)?;
        if bytes.len() > crate::content::MAX_CONTENT_BYTES
            || crate::canonical::content_digest(&bytes) != digest
        {
            return Err(io::ErrorKind::InvalidData.into());
        }
        validate_canonical_response(&response.body, &bytes)?;
        let content = crate::canonical::CanonicalContent::parse(&bytes)
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        let parameter = match request.route {
            Route::Search => "q",
            Route::Fetch => "url",
            _ => return Err(io::ErrorKind::InvalidData.into()),
        };
        if request
            .query_param(parameter)
            .ok()
            .flatten()
            .as_deref()
            .map(str::as_bytes)
            != Some(content.payload.as_slice())
        {
            return Err(io::ErrorKind::InvalidData.into());
        }
        Ok(bytes)
    }

    pub fn recover_pending_deliveries(&self) -> io::Result<()> {
        let _delivery = self
            .delivery_lock
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
        for entry in fs::read_dir(self.store.root.join(REQUESTS))? {
            let entry = entry?;
            let name = entry.file_name();
            let name = name.to_str().ok_or(io::ErrorKind::InvalidData)?;
            if name.ends_with(".staging") {
                continue;
            }
            let key = unhex32(name).ok_or(io::ErrorKind::InvalidData)?;
            let payment = self.store.load(&key)?.ok_or(io::ErrorKind::InvalidData)?;
            if unhex32(&payment.request_digest).is_none() || payment.resource.is_empty() {
                return Err(io::ErrorKind::InvalidData.into());
            }
            let Some(receipt) = payment.receipt.as_deref() else {
                if payment.receipt_digest.is_some() || self.store.delivery(&key)?.is_some() {
                    return Err(io::ErrorKind::InvalidData.into());
                }
                continue;
            };
            let bytes = unhex(receipt).ok_or(io::ErrorKind::InvalidData)?;
            let asset = payment
                .offer
                .get("asset")
                .and_then(Value::as_str)
                .and_then(unhex32)
                .and_then(|id| self.assets.by_id(&id))
                .ok_or(io::ErrorKind::InvalidData)?;
            let metered = payment.receive_key.is_some();
            let scheme = if metered { METERED } else { EXACT };
            if payment.offer.get("scheme").and_then(Value::as_str) != Some(scheme)
                || payment.offer.get("network").and_then(Value::as_str)
                    != Some(self.network.as_str())
                || payment.offer.get("amount").and_then(Value::as_str)
                    != Some(asset.price.to_string().as_str())
                || payment.offer.get("payTo").and_then(Value::as_str)
                    != Some(hex(&self.payee(asset)).as_str())
            {
                return Err(io::ErrorKind::InvalidData.into());
            }
            let expected = Expected {
                operation: if metered {
                    RECEIVE_OPERATION
                } else {
                    SEND_OPERATION
                },
                fee_limit: metered.then_some(self.draw_fee_limit),
                activity_id: Some(
                    payment
                        .activity_id
                        .as_deref()
                        .and_then(unhex32)
                        .ok_or(io::ErrorKind::InvalidData)?,
                ),
                from: Some(
                    payment
                        .payer
                        .as_deref()
                        .and_then(unhex32)
                        .ok_or(io::ErrorKind::InvalidData)?,
                ),
                to: self.payee(asset),
                asset: asset.asset_id,
                amount: asset.price,
            };
            let plane = Plane {
                gate: self,
                resource: &payment.resource,
                payer: self.payer.as_deref(),
                now_ms: (self.clock)(),
                key: None,
                refusal: None,
            };
            let (_, digest) = plane
                .verify_receipt(&bytes, &expected)
                .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
            if payment.receipt_digest.as_deref() != Some(hex(&digest).as_str())
                || !self.store.claim(RECEIPTS, &hex(&digest), &key)?
            {
                return Err(io::ErrorKind::InvalidData.into());
            }
            let mut delivery = match self.store.delivery(&key)? {
                Some(delivery) => delivery,
                None if payment.released => continue,
                None => DeliveryRecord::pending(&payment, &digest),
            };
            delivery.validate(&payment)?;
            if delivery.state == DeliveryState::Computing {
                delivery.state = DeliveryState::Pending;
            }
            self.store.save_json(DELIVERIES, &key, &delivery)?;
        }
        for entry in fs::read_dir(self.store.root.join(DELIVERIES))? {
            let entry = entry?;
            let name = entry.file_name();
            let name = name.to_str().ok_or(io::ErrorKind::InvalidData)?;
            if name.ends_with(".staging") {
                continue;
            }
            let key = unhex32(name).ok_or(io::ErrorKind::InvalidData)?;
            let payment = self.store.load(&key)?.ok_or(io::ErrorKind::InvalidData)?;
            self.store
                .delivery(&key)?
                .ok_or(io::ErrorKind::InvalidData)?
                .validate(&payment)?;
        }
        Ok(())
    }

    pub fn install_validated(
        gate: &Arc<Self>,
        routes: &mut RouteTable,
        route: Route,
        validate: impl Fn(&Request) -> Result<(), Response> + Send + Sync + 'static,
        handler: impl Fn(&Request) -> Response + Send + Sync + 'static,
    ) -> Result<(), RouteError> {
        let gate = Arc::clone(gate);
        routes.set(route, move |request: &Request| {
            if let Err(response) = validate(request) {
                return response;
            }
            if request.route.is_paid() {
                gate.settle_request(request, &handler, true)
            } else {
                handler(request)
            }
        })
    }

    /// Wraps a paid route's handler so it runs only after settlement.
    pub fn paid(
        gate: &Arc<Self>,
        handler: impl Fn(&Request) -> Response + Send + Sync + 'static,
    ) -> impl Fn(&Request) -> Response + Send + Sync + 'static {
        let gate = Arc::clone(gate);
        move |request: &Request| {
            if request.route.is_paid() {
                gate.settle(request, &handler)
            } else {
                handler(request)
            }
        }
    }

    /// Installs a paid route's handler behind the payment hook.
    ///
    /// # Errors
    /// Refuses the built-in health route and a route that already has a handler.
    pub fn install(
        gate: &Arc<Self>,
        routes: &mut RouteTable,
        route: Route,
        handler: impl Fn(&Request) -> Response + Send + Sync + 'static,
    ) -> Result<(), RouteError> {
        routes.set(route, Self::paid(gate, handler))
    }
}

const fn describe(route: Route) -> &'static str {
    match route {
        Route::Health => "Sidecar health",
        Route::Search => "Web search results",
        Route::Fetch => "A fetched page and its digest",
        Route::Content => "Stored content by digest",
    }
}

/// The payer a request's draws are taken from: the configured payer when
/// there is one, which a `LAYERX-PAYER-DID` header may repeat but not
/// contradict, and otherwise the DID the header names.
fn payer_did<'a>(
    request: &'a Request,
    configured: Option<&'a str>,
) -> Result<Option<&'a str>, Response> {
    let named = match request.header(PAYER_DID) {
        None => None,
        Some(did) if payer_did_valid(did) => Some(did),
        Some(_) => return Err(Response::error(400, "malformed_payer")),
    };
    match (configured, named) {
        (Some(configured), Some(named)) if configured != named => {
            Err(Response::error(400, "payer_mismatch"))
        }
        (Some(configured), _) => Ok(Some(configured)),
        (None, named) => Ok(named),
    }
}

/// The digest and rule count of the recorded gateway exchanges in
/// `directory`: SHA-256 over every file's bytes in file name order, and the
/// number of rules under `/endpoints/gateway/*` across them.
fn recorded_suite(directory: &Path) -> io::Result<([u8; 32], u64)> {
    let mut names = fs::read_dir(directory)?
        .map(|entry| entry.map(|entry| entry.path()))
        .collect::<io::Result<Vec<_>>>()?;
    names.sort();
    let mut digest = Sha256::new();
    let mut rules = 0_u64;
    for name in names {
        let bytes = fs::read(name)?;
        digest.update(&bytes);
        let recording: Value = serde_json::from_slice(&bytes)
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidData))?;
        if let Some(list) = recording
            .pointer("/endpoints/gateway/*")
            .and_then(Value::as_array)
        {
            rules = u64::try_from(list.len())
                .ok()
                .and_then(|count| rules.checked_add(count))
                .ok_or_else(|| io::Error::from(io::ErrorKind::InvalidData))?;
        }
    }
    Ok((digest.finalize().into(), rules))
}

fn principal(payer: Option<&str>) -> PrincipalId {
    let name = payer.map_or_else(
        || ANONYMOUS_PRINCIPAL.to_owned(),
        |did| {
            format!(
                "payer-{}",
                hex(&sha256(&[PRINCIPAL_DOMAIN, did.as_bytes()]))
            )
        },
    );
    PrincipalId::new(name).unwrap_or_else(|_| {
        PrincipalId::new(ANONYMOUS_PRINCIPAL).unwrap_or_else(|_| unreachable!("fixed principal"))
    })
}

fn trace_entropy(header: &str) -> [u8; 16] {
    let digest = sha256(&[TRACE_DOMAIN, header.as_bytes()]);
    let mut entropy = [0; 16];
    entropy.copy_from_slice(&digest[..16]);
    entropy
}

fn canonical_target(request: &Request) -> Result<String, Response> {
    let (path, parameter) = match request.route {
        Route::Search => ("/search", Some("q")),
        Route::Fetch => ("/fetch", Some("url")),
        Route::Content => {
            let digest = request
                .digest
                .ok_or_else(|| Response::error(400, "malformed_digest"))?;
            return Ok(format!("/content/{}", hex(&digest)));
        }
        Route::Health => ("/health", None),
    };
    let Some(parameter) = parameter else {
        return Ok(path.to_owned());
    };
    let value = request
        .query_param(parameter)
        .map_err(|_| Response::error(400, "malformed_query"))?
        .filter(|value| !value.is_empty())
        .ok_or_else(|| Response::error(400, "missing_query"))?;
    Ok(format!("{path}?{parameter}={}", encode_query_value(&value)))
}

fn encode_query_value(value: &str) -> String {
    let mut encoded = String::new();
    const DIGITS: &[u8; 16] = b"0123456789ABCDEF";
    for byte in value.bytes() {
        if byte.is_ascii_alphanumeric() || b"-._~".contains(&byte) {
            encoded.push(char::from(byte));
        } else {
            encoded.push('%');
            encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
            encoded.push(char::from(DIGITS[usize::from(byte & 15)]));
        }
    }
    encoded
}

fn resource_binding(request: &Request) -> Result<String, Response> {
    Ok(format!("{} {}", request.method, canonical_target(request)?))
}

type Settled = Result<PlanePaymentOutcome, &'static str>;

/// The plane the seller settles through: it signs, submits and recovers the
/// draw, confirms an exact receipt, and verifies every receipt against the
/// configured sequencer trust.
struct Plane<'a> {
    gate: &'a PaymentGate,
    resource: &'a str,
    payer: Option<&'a str>,
    now_ms: u64,
    key: Option<[u8; 32]>,
    refusal: Option<&'static str>,
}

impl PaymentPlane for Plane<'_> {
    fn execute(
        &mut self,
        request: LayerXPaymentRequest,
        _trace: &TraceId,
    ) -> Result<PlanePaymentOutcome, layerx_x402::model::X402Error> {
        self.key = Some(request.idempotency_key);
        match self.run(&request) {
            Ok(outcome) => Ok(outcome),
            Err(reason) => {
                self.refusal = Some(reason);
                if reason == "request_mismatch" {
                    Err(layerx_x402::model::X402Error::RequirementsMismatch)
                } else {
                    Ok(PlanePaymentOutcome::Refused { reason })
                }
            }
        }
    }
}

fn offer_record(request: &LayerXPaymentRequest) -> Value {
    json!({
        "scheme": request.scheme,
        "network": request.network,
        "asset": request.asset,
        "amount": request.amount.value().to_string(),
        "payTo": request.pay_to,
    })
}

struct Expected {
    operation: u8,
    fee_limit: Option<u128>,
    activity_id: Option<[u8; 32]>,
    from: Option<[u8; 32]>,
    to: [u8; 32],
    asset: [u8; 32],
    amount: u128,
}

impl Plane<'_> {
    fn run(&self, request: &LayerXPaymentRequest) -> Settled {
        let asset = unhex32(&request.asset)
            .and_then(|id| self.gate.assets.by_id(&id).copied())
            .ok_or("unsupported_asset")?;
        let pay_to = unhex32(&request.pay_to).ok_or("payee_mismatch")?;
        if pay_to != self.gate.payee(&asset) {
            return Err("payee_mismatch");
        }
        if request.amount.value() != asset.price {
            return Err("amount_mismatch");
        }
        let key = request.idempotency_key;
        let store = &self.gate.store;
        let Ok(existing) = store.load(&key) else {
            return Ok(PlanePaymentOutcome::Pending);
        };
        if let Some(record) = existing {
            if record.principal != request.principal.as_str()
                || record.request_digest != hex(&request.request_digest)
                || record.resource != self.resource
                || record.offer != offer_record(request)
            {
                return Err("request_mismatch");
            }
            return match request.scheme.as_str() {
                METERED => self.recover(&key, record, &asset, pay_to),
                _ => self.exact(request, &asset, pay_to, Some(record)),
            };
        }
        match request.scheme.as_str() {
            METERED => self.draw(request, &asset, pay_to),
            EXACT => self.exact(request, &asset, pay_to, None),
            _ => Err("unsupported_scheme"),
        }
    }

    fn fresh_record(&self, request: &LayerXPaymentRequest) -> PaymentRecord {
        PaymentRecord {
            principal: request.principal.as_str().to_owned(),
            request_digest: hex(&request.request_digest),
            resource: self.resource.to_owned(),
            offer: offer_record(request),
            payer: None,
            receive_key: None,
            activity: None,
            activity_id: None,
            attempted: false,
            receipt: None,
            receipt_digest: None,
            released: false,
        }
    }

    fn draw(
        &self,
        request: &LayerXPaymentRequest,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
    ) -> Settled {
        let payer = self.payer.ok_or("payer_required")?;
        let (grant_bytes, receive_key) = metered_payload(&request.scheme_payload)?;
        let grant = decode_grant(&grant_bytes, &self.gate.receiver.did).ok_or("invalid_grant")?;
        let payer_account = account_id(&wallet_account(payer, asset)).ok_or("payer_mismatch")?;
        check_grant(
            &grant,
            &payer_account,
            &pay_to,
            asset,
            &self.gate.receiver.purpose,
        )?;
        if self.now_ms / 1_000 >= grant.expiration {
            return Err("grant_expired");
        }
        let key = request.idempotency_key;
        match self
            .gate
            .store
            .claim(RECEIVE_KEYS, &hex(&receive_key), &key)
        {
            Ok(true) => {}
            Ok(false) => return Err("idempotency_conflict"),
            Err(_) => return Ok(PlanePaymentOutcome::Pending),
        }
        let identity = self.sequence(&json!([self.gate.receiver.did, "identity"]));
        let account = self.sequence(&json!([hex(&pay_to)]));
        let (Some(identity_sequence), Some(receiver_sequence)) = (identity, account) else {
            return Ok(PlanePaymentOutcome::Pending);
        };
        let signed = sign_draw(
            &self.gate.receiver.key,
            &DrawRequest {
                grant: &grant,
                amount: asset.price,
                receiver_sequence,
                identity_sequence,
                idempotency_key: receive_key,
                network_id: self.gate.network_id,
                now_ms: self.now_ms,
                fee_limit: self.gate.draw_fee_limit,
            },
        )
        .map_err(|_| "draw_refused")?;
        let record = PaymentRecord {
            payer: Some(hex(&payer_account)),
            receive_key: Some(hex(&receive_key)),
            activity: Some(hex(&signed.canonical)),
            activity_id: Some(hex(&signed.activity_id)),
            ..self.fresh_record(request)
        };
        if self.gate.store.save(&key, &record).is_err() {
            return Ok(PlanePaymentOutcome::Pending);
        }
        self.submit(&key, record, asset, pay_to)
    }

    fn sequence(&self, params: &Value) -> Option<u64> {
        match self.gate.rpc.call("lx_getSequence", params)? {
            RpcAnswer::Result(value) => {
                let text = value.get("next_sequence")?.as_str()?;
                if text.is_empty()
                    || (text.len() > 1 && text.starts_with('0'))
                    || !text.bytes().all(|byte| byte.is_ascii_digit())
                {
                    return None;
                }
                text.parse().ok()
            }
            RpcAnswer::Error { .. } => None,
        }
    }

    fn submit(
        &self,
        key: &[u8; 32],
        mut record: PaymentRecord,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
    ) -> Settled {
        let activity = record.activity.clone().ok_or("request_mismatch")?;
        record.attempted = true;
        if self.gate.store.save(key, &record).is_err() {
            return Ok(PlanePaymentOutcome::Pending);
        }
        let answer = self
            .gate
            .rpc
            .call("lx_sendActivity", &json!([activity, COMMITMENT]));
        self.conclude(key, record, answer, asset, pay_to)
    }

    fn recover(
        &self,
        key: &[u8; 32],
        record: PaymentRecord,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
    ) -> Settled {
        if let Some(receipt) = record.receipt.as_deref() {
            let bytes = unhex(receipt).ok_or("receipt_unverified")?;
            return self.finish(key, record, bytes, asset, pay_to);
        }
        if !record.attempted {
            return self.submit(key, record, asset, pay_to);
        }
        let activity_id = record.activity_id.clone().ok_or("request_mismatch")?;
        let status = self
            .gate
            .rpc
            .call("lx_getActivityStatus", &json!([activity_id]));
        let needs_receipt = matches!(
            &status,
            Some(RpcAnswer::Result(value))
                if value.get("receipt").is_none()
                    && value.get("state").and_then(Value::as_str) != Some("pending")
        );
        let answer = if needs_receipt {
            self.gate.rpc.call("lx_getReceipt", &json!([activity_id]))
        } else {
            status
        };
        self.conclude(key, record, answer, asset, pay_to)
    }

    fn conclude(
        &self,
        key: &[u8; 32],
        record: PaymentRecord,
        answer: Option<RpcAnswer>,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
    ) -> Settled {
        let Some(answer) = answer else {
            return Ok(PlanePaymentOutcome::Pending);
        };
        if answer.pending() {
            return Ok(PlanePaymentOutcome::Pending);
        }
        let RpcAnswer::Result(value) = answer else {
            return Err("gateway_refused");
        };
        if value.get("activity_id").and_then(Value::as_str) != record.activity_id.as_deref() {
            return Err("receipt_mismatch");
        }
        if value
            .get("state")
            .is_some_and(|state| state.as_str() != Some("completed"))
        {
            return Err("payment_failed");
        }
        let Some(receipt) = value.get("receipt") else {
            return Ok(PlanePaymentOutcome::Pending);
        };
        let bytes = receipt
            .as_str()
            .filter(|text| text.len() <= RECEIPT_HEX_LIMIT)
            .and_then(unhex)
            .ok_or("receipt_unverified")?;
        self.finish(key, record, bytes, asset, pay_to)
    }

    fn finish(
        &self,
        key: &[u8; 32],
        mut record: PaymentRecord,
        bytes: Vec<u8>,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
    ) -> Settled {
        let metered = record.receive_key.is_some();
        let expected = Expected {
            operation: if metered {
                RECEIVE_OPERATION
            } else {
                SEND_OPERATION
            },
            fee_limit: metered.then_some(self.gate.draw_fee_limit),
            activity_id: record.activity_id.as_deref().and_then(unhex32),
            from: record.payer.as_deref().and_then(unhex32),
            to: pay_to,
            asset: asset.asset_id,
            amount: asset.price,
        };
        let (batch, digest) = self.verify_receipt(&bytes, &expected)?;
        match self.gate.store.claim(RECEIPTS, &hex(&digest), key) {
            Ok(true) => {}
            Ok(false) => return Err("receipt_consumed"),
            Err(_) => return Ok(PlanePaymentOutcome::Pending),
        }
        if record.receipt.is_none() {
            record.receipt = Some(hex(&bytes));
            record.receipt_digest = Some(hex(&digest));
            if self.gate.store.save(key, &record).is_err() {
                return Ok(PlanePaymentOutcome::Pending);
            }
        } else if record.receipt_digest.as_deref() != Some(hex(&digest).as_str()) {
            return Err("receipt_mismatch");
        }
        Ok(PlanePaymentOutcome::Executed(ExecutedPayment {
            canonical_receipt: bytes,
            authorised_batch: batch,
        }))
    }

    fn verify_receipt(
        &self,
        bytes: &[u8],
        expected: &Expected,
    ) -> Result<(AuthorizedBatch, [u8; 32]), &'static str> {
        let trusted = self.gate.trust.public_key;
        let receipt =
            verify_sequencer_signature(bytes, trusted).map_err(|_| "receipt_unverified")?;
        let facts = receipt.protocol().ok_or("receipt_unverified")?;
        if facts.module_id() != ASSET_MODULE
            || facts.operation() != expected.operation
            || facts.result_code() != 0
            || expected
                .activity_id
                .is_some_and(|activity| activity != facts.activity_id())
            || expected.from.is_some_and(|from| from != facts.from())
            || facts.to() != expected.to
            || facts.asset() != expected.asset
            || facts.amount() != expected.amount
        {
            return Err("receipt_mismatch");
        }
        if expected
            .fee_limit
            .is_some_and(|limit| facts.fee_charged() > limit)
        {
            return Err("draw_fee_limit_exceeded");
        }
        let batch = AuthorizedBatch::new(
            facts.batch_id(),
            facts.asset(),
            facts.previous_state_root(),
            facts.resulting_state_root(),
            trusted,
        );
        let verified = verify(bytes, &batch).map_err(|_| "receipt_unverified")?;
        let digest = leaf_hash(verified.canonical_bytes()).map_err(|_| "receipt_unverified")?;
        Ok((batch, digest))
    }

    fn exact(
        &self,
        request: &LayerXPaymentRequest,
        asset: &AcceptedAsset,
        pay_to: [u8; 32],
        existing: Option<PaymentRecord>,
    ) -> Settled {
        let (bytes, claimed) = exact_payload(&request.scheme_payload)?;
        let key = request.idempotency_key;
        let expected = Expected {
            operation: SEND_OPERATION,
            fee_limit: None,
            activity_id: None,
            from: self
                .payer
                .map(|payer| account_id(&wallet_account(payer, asset)).ok_or("payer_mismatch"))
                .transpose()?,
            to: pay_to,
            asset: asset.asset_id,
            amount: asset.price,
        };
        let (_, digest) = self.verify_receipt(&bytes, &expected)?;
        if digest != claimed {
            return Err("receipt_digest_mismatch");
        }
        let record = if let Some(record) = existing {
            record
        } else {
            match self.gate.store.holder(RECEIPTS, &hex(&digest)) {
                Ok(None) => {}
                Ok(Some(_)) => return Err("receipt_consumed"),
                Err(_) => return Ok(PlanePaymentOutcome::Pending),
            }
            let facts = verify_sequencer_signature(&bytes, self.gate.trust.public_key)
                .ok()
                .and_then(|receipt| {
                    receipt
                        .protocol()
                        .map(|facts| (facts.activity_id(), facts.from()))
                })
                .ok_or("receipt_unverified")?;
            let record = PaymentRecord {
                payer: Some(hex(&facts.1)),
                activity_id: Some(hex(&facts.0)),
                attempted: true,
                ..self.fresh_record(request)
            };
            if self.gate.store.save(&key, &record).is_err() {
                return Ok(PlanePaymentOutcome::Pending);
            }
            record
        };
        if record.receipt.is_some() {
            return self.finish(&key, record, bytes, asset, pay_to);
        }
        let activity_id = record.activity_id.clone().ok_or("request_mismatch")?;
        let answer = self.gate.rpc.call("lx_getReceipt", &json!([activity_id]));
        let confirmed = match &answer {
            Some(RpcAnswer::Result(value)) => value
                .get("receipt")
                .and_then(Value::as_str)
                .is_none_or(|receipt| unhex(receipt).as_deref() == Some(bytes.as_slice())),
            Some(error @ RpcAnswer::Error { .. }) if error.pending() => true,
            Some(RpcAnswer::Error { .. }) => return Err("receipt_unknown"),
            None => true,
        };
        if !confirmed {
            return Err("receipt_mismatch");
        }
        self.conclude(&key, record, answer, asset, pay_to)
    }
}

fn check_grant(
    grant: &Grant,
    payer: &[u8; 32],
    pay_to: &[u8; 32],
    asset: &AcceptedAsset,
    purpose: &[u8; 32],
) -> Result<(), &'static str> {
    if grant.from != *payer {
        return Err("grant_payer_mismatch");
    }
    if grant.recipient != *pay_to {
        return Err("grant_recipient_mismatch");
    }
    if grant.asset != asset.asset_id {
        return Err("grant_asset_mismatch");
    }
    if grant.purpose_hash != *purpose {
        return Err("grant_purpose_mismatch");
    }
    if grant.recurring || grant.window_length != 0 || grant.has_reference {
        return Err("grant_not_metered");
    }
    if grant.per_draw_maximum < asset.price {
        return Err("grant_limit");
    }
    Ok(())
}

/// The metered payload: `grant`, the payer-signed canonical grant as
/// lowercase hexadecimal, and `idempotencyKey`, the draw's 32-byte key.
fn metered_payload(payload: &Value) -> Result<(Vec<u8>, [u8; 32]), &'static str> {
    let fields = payload.as_object().ok_or("invalid_payment_payload")?;
    if fields.len() != 2 {
        return Err("invalid_payment_payload");
    }
    let grant = fields
        .get("grant")
        .and_then(Value::as_str)
        .filter(|grant| grant.len() == GRANT_BYTES * 2)
        .and_then(unhex)
        .ok_or("invalid_payment_payload")?;
    let key = fields
        .get("idempotencyKey")
        .and_then(Value::as_str)
        .and_then(unhex32)
        .ok_or("invalid_payment_payload")?;
    Ok((grant, key))
}

/// The exact payload: `receipt` in base64, `receiptDigest`, the
/// `sequencer-signed` verification level and an optional `idempotencyKey`.
fn exact_payload(payload: &Value) -> Result<(Vec<u8>, [u8; 32]), &'static str> {
    let fields = payload.as_object().ok_or("invalid_payment_payload")?;
    let known = [
        "receipt",
        "receiptDigest",
        "verificationLevel",
        "idempotencyKey",
    ];
    if fields.keys().any(|name| !known.contains(&name.as_str()))
        || fields.get("verificationLevel").and_then(Value::as_str) != Some("sequencer-signed")
        || fields
            .get("idempotencyKey")
            .is_some_and(|key| key.as_str().and_then(unhex32).is_none())
    {
        return Err("invalid_payment_payload");
    }
    let receipt = fields
        .get("receipt")
        .and_then(Value::as_str)
        .filter(|receipt| receipt.len() <= RECEIPT_HEX_LIMIT)
        .and_then(|receipt| STANDARD.decode(receipt).ok())
        .ok_or("invalid_payment_payload")?;
    let digest = fields
        .get("receiptDigest")
        .and_then(Value::as_str)
        .and_then(unhex32)
        .ok_or("invalid_payment_payload")?;
    Ok((receipt, digest))
}

#[cfg(test)]
mod chunked_tests {
    use std::io::{Read as _, Write as _};
    use std::net::TcpListener;
    use std::thread;

    use serde_json::json;

    use super::{decode_http, GatewayRpc, RpcAnswer};

    const HEAD: &str = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\nTransfer-Encoding: chunked\r\n\r\n";

    #[test]
    fn a_chunked_answer_is_joined_across_its_chunks_and_trailers() {
        let raw = format!(
            "{HEAD}10;ext=1\r\n{{\"jsonrpc\":\"2.0\"\r\nC\r\n,\"id\":1,\"res\r\nB\r\nult\":\"0x1\"}}\r\n0\r\nExpires: never\r\n\r\n"
        );
        let value = decode_http(raw.as_bytes()).expect("chunked answer decodes");
        assert_eq!(value, json!({"jsonrpc": "2.0", "id": 1, "result": "0x1"}));
    }

    #[test]
    fn a_chunked_answer_cut_short_or_followed_by_more_bytes_is_refused() {
        let cut = format!("{HEAD}20\r\n{{\"jsonrpc\":\"2.0\"}}\r\n");
        assert!(decode_http(cut.as_bytes()).is_none());
        let trailing = format!("{HEAD}2\r\n{{}}\r\n0\r\n\r\nx");
        assert!(decode_http(trailing.as_bytes()).is_none());
        let both = format!(
            "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nTransfer-Encoding: chunked\r\n\r\n2\r\n{{}}\r\n0\r\n\r\n"
        );
        assert!(decode_http(both.as_bytes()).is_none());
        let gzip = format!("HTTP/1.1 200 OK\r\nTransfer-Encoding: gzip\r\n\r\n{{}}");
        assert!(decode_http(gzip.as_bytes()).is_none());
    }

    #[test]
    fn the_client_reads_a_chunked_answer_from_a_loopback_endpoint() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("loopback listener");
        let address = listener.local_addr().expect("listener address");
        let body =
            json!({"jsonrpc": "2.0", "id": 1, "result": "0x".to_owned() + &"00".repeat(2048)})
                .to_string();
        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("one connection");
            let mut request = [0u8; 4096];
            let read = stream.read(&mut request).expect("request bytes");
            assert!(std::str::from_utf8(&request[..read])
                .expect("utf-8 request")
                .starts_with("POST / HTTP/1.1\r\n"));
            let (first, second) = body.split_at(1000);
            let answer = format!(
                "{HEAD}{:x}\r\n{first}\r\n{:x}\r\n{second}\r\n0\r\n\r\n",
                first.len(),
                second.len()
            );
            stream.write_all(answer.as_bytes()).expect("answer written");
        });
        let rpc = GatewayRpc::new(&format!("http://{address}")).expect("loopback endpoint");
        let answer = rpc.call("eth_call", &json!([]));
        server.join().expect("server thread");
        match answer {
            Some(RpcAnswer::Result(value)) => {
                assert_eq!(value, json!("0x".to_owned() + &"00".repeat(2048)));
            }
            other => panic!("chunked answer not read: {other:?}"),
        }
    }
}
