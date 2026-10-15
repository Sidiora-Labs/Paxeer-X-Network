//! Oracle feeder for the kernel perps markets: median of public REST quotes,
//! signed oracle observations pushed to layerxexchange `pushOracle` as EVM
//! transactions, and an on-disk journal.

use std::collections::BTreeMap;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use ed25519_dalek::{Signer, SigningKey};
use layerx_platform_internal::evm_sender::{Eip1559Call, EvmSender};
use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{
    ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload, PerpsPayload,
};
use layerx_wire::activity::encode_signed_envelope;
use layerx_wire::hash::{payload_hash_for, Domain};
use layerx_wire::sign::preimage_unsigned;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use zeroize::Zeroizing;

/// Status line of the plain health listener: `GET /healthz` answers 200
/// while the process runs, `GET /readyz` answers 200 only while the last
/// complete round is fresh, anything else 404.
#[must_use]
pub fn health_status(request: &[u8], ready: bool) -> &'static str {
    if request.starts_with(b"GET /healthz ") {
        "200 OK"
    } else if !request.starts_with(b"GET /readyz ") {
        "404 Not Found"
    } else if ready {
        "200 OK"
    } else {
        "503 Service Unavailable"
    }
}

pub mod chainflow;
pub mod cosmos_tx;

/// Perps oracle push ordinal (`0x00060003`).
pub const ORACLE_PUSH_ORDINAL: u16 = 3;
/// `LX_ORACLE_OBSERVATION_BYTES`.
pub const OBSERVATION_BYTES: usize = 72;
/// `LX_ORACLE_TRANSPORT_BYTES`.
pub const TRANSPORT_BYTES: usize = 137;
/// `LX_ORACLE_TRANSPORT_VERSION`.
pub const TRANSPORT_VERSION: u8 = 1;
/// `LXP_PROTOCOL_VERSION_STATE_COMMITMENT`: the kernel decodes the transport form.
pub const PROTOCOL_VERSION_STATE_COMMITMENT: u16 = 3;
const OBSERVATION_TAG: &[u8] = b"LXP:ORACLE:OBSERVATION:v1";
const MAX_VALIDITY_MS: u64 = 300_000;
/// Environment prefix of the shared EVM sender loader: the feeder reads
/// `LAYERX_FEEDER_EVM_SENDER_KEY_FILE` or `LAYERX_FEEDER_EVM_SENDER_KMS_KEY_ID`.
pub const EVM_SENDER_PREFIX: &str = "LAYERX_FEEDER_EVM_SENDER";
/// layerxexchange precompile.
pub const LAYERX_EXCHANGE: [u8; 20] = precompile(0x10, 0x15);
/// addr precompile.
pub const ADDR_PRECOMPILE: [u8; 20] = precompile(0x10, 0x04);
/// `pushOracle(bytes,bytes)`.
pub const PUSH_ORACLE_SELECTOR: [u8; 4] = [0x0e, 0x4d, 0x93, 0x82];
/// `getEvmAddrByLayerX(bytes32)`.
pub const GET_EVM_ADDR_BY_LAYERX_SELECTOR: [u8; 4] = [0xc4, 0x13, 0xba, 0xd3];
/// Readiness lasts this many cadences after the last accepted round.
pub const HEALTH_WINDOW_CADENCES: u64 = 3;
const RECEIPT_POLL: Duration = Duration::from_millis(50);

const fn precompile(high: u8, low: u8) -> [u8; 20] {
    let mut address = [0; 20];
    address[18] = high;
    address[19] = low;
    address
}

#[derive(Clone, Debug, Deserialize)]
pub struct MarketConfig {
    pub symbol: String,
    pub market_id: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct SourceConfig {
    pub name: String,
    /// URL with a `{pair}` placeholder.
    pub url: String,
    /// JSON pointer to the price; `{pair}` is substituted.
    pub price_pointer: String,
    /// Optional JSON pointer to the quote time (integer or decimal string).
    #[serde(default)]
    pub time_pointer: Option<String>,
    /// `ms` (default) or `s`.
    #[serde(default)]
    pub time_unit: Option<String>,
    pub pairs: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Config {
    pub network_id: u32,
    pub protocol_version: u16,
    pub actor_did: String,
    pub fee_limit: String,
    pub cadence_ms: u64,
    pub validity_ms: u64,
    pub max_quote_age_ms: u64,
    pub max_divergence_bps: u64,
    pub min_sources: usize,
    pub price_decimals: u32,
    pub source_identifier: u64,
    pub initial_account_sequence: u64,
    pub markets: Vec<MarketConfig>,
    pub sources: Vec<SourceConfig>,
}

impl Config {
    /// # Errors
    /// Refuses unreadable, malformed or out-of-bound configuration.
    pub fn load(path: &Path) -> Result<Self, String> {
        let text =
            fs::read_to_string(path).map_err(|e| format!("config {}: {e}", path.display()))?;
        let config: Self = serde_json::from_str(&text).map_err(|e| format!("config: {e}"))?;
        config.validate()?;
        Ok(config)
    }

    /// # Errors
    /// Refuses zero or unbounded fields and malformed market ids.
    pub fn validate(&self) -> Result<(), String> {
        if self.network_id == 0
            || self.cadence_ms == 0
            || self.validity_ms == 0
            || self.validity_ms > MAX_VALIDITY_MS
            || self.max_quote_age_ms == 0
            || self.min_sources == 0
            || self.min_sources > self.sources.len()
            || self.source_identifier == 0
            || self.initial_account_sequence == 0
            || self.markets.is_empty()
            || self.actor_did.is_empty()
            || self.fee_limit()? == 0
        {
            return Err("config has a zero or out-of-bound field".to_owned());
        }
        for market in &self.markets {
            market_id(&market.market_id)?;
        }
        Ok(())
    }

    /// # Errors
    /// Refuses a non-decimal fee limit.
    pub fn fee_limit(&self) -> Result<u128, String> {
        self.fee_limit
            .parse()
            .map_err(|_| "fee_limit must be a decimal integer".to_owned())
    }
}

/// # Errors
/// Refuses anything other than 64 hex characters.
pub fn market_id(text: &str) -> Result<[u8; 32], String> {
    let bytes = hex_decode(text.trim())?;
    bytes
        .try_into()
        .map_err(|_| "market id must be 32 bytes".to_owned())
}

/// # Errors
/// Refuses odd length or non-hex characters.
pub fn hex_decode(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) {
        return Err("odd hex length".to_owned());
    }
    (0..text.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&text[i..i + 2], 16).map_err(|_| "invalid hex".to_owned()))
        .collect()
}

#[must_use]
pub fn hex_encode(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(String::new(), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}

/// Parses a non-negative decimal into fixed point with `decimals` places,
/// truncating extra precision. Exponent forms are refused.
///
/// # Errors
/// Refuses malformed, negative, zero or overflowing values.
pub fn parse_fixed(text: &str, decimals: u32) -> Result<u128, String> {
    let (whole, frac) = text.split_once('.').unwrap_or((text, ""));
    if whole.is_empty() && frac.is_empty()
        || !whole.bytes().all(|b| b.is_ascii_digit())
        || !frac.bytes().all(|b| b.is_ascii_digit())
    {
        return Err(format!("malformed price {text:?}"));
    }
    let scale = 10u128.checked_pow(decimals).ok_or("decimals overflow")?;
    let whole: u128 = if whole.is_empty() {
        0
    } else {
        whole.parse().map_err(|_| "price overflow")?
    };
    let mut fraction = 0u128;
    let mut digits = 0u32;
    for byte in frac.bytes().take(decimals as usize) {
        fraction = fraction * 10 + u128::from(byte - b'0');
        digits += 1;
    }
    fraction *= 10u128.pow(decimals - digits);
    let value = whole
        .checked_mul(scale)
        .and_then(|v| v.checked_add(fraction))
        .ok_or("price overflow")?;
    if value == 0 {
        return Err("zero price".to_owned());
    }
    Ok(value)
}

/// One quote from one source.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Quote {
    pub source: String,
    pub price: u128,
    pub observed_at_ms: u64,
}

fn scalar_text(value: &Value) -> Option<String> {
    match value {
        Value::String(text) => Some(text.clone()),
        Value::Number(number) => Some(number.to_string()),
        _ => None,
    }
}

/// Extracts a quote from a recorded or live source response.
///
/// # Errors
/// Refuses a missing or malformed price or time.
pub fn parse_quote(
    source: &SourceConfig,
    pair: &str,
    body: &Value,
    fetched_at_ms: u64,
    decimals: u32,
) -> Result<Quote, String> {
    let pointer = source.price_pointer.replace("{pair}", pair);
    let price = body
        .pointer(&pointer)
        .and_then(scalar_text)
        .ok_or_else(|| format!("{}: no price at {pointer}", source.name))?;
    let price = parse_fixed(&price, decimals).map_err(|e| format!("{}: {e}", source.name))?;
    let observed_at_ms = match &source.time_pointer {
        None => fetched_at_ms,
        Some(pointer) => {
            let text = body
                .pointer(pointer)
                .and_then(scalar_text)
                .ok_or_else(|| format!("{}: no time at {pointer}", source.name))?;
            let raw: u64 = text
                .parse()
                .map_err(|_| format!("{}: malformed time", source.name))?;
            match source.time_unit.as_deref() {
                Some("s") => raw.checked_mul(1000).ok_or("time overflow")?,
                None | Some("ms") => raw,
                Some(other) => return Err(format!("{}: unknown time unit {other}", source.name)),
            }
        }
    };
    Ok(Quote {
        source: source.name.clone(),
        price,
        observed_at_ms,
    })
}

/// Median of a non-empty slice; even counts average the middle pair (floor).
#[must_use]
pub fn median(prices: &[u128]) -> Option<u128> {
    let mut sorted = prices.to_vec();
    sorted.sort_unstable();
    let n = sorted.len();
    match n {
        0 => None,
        _ if n % 2 == 1 => Some(sorted[n / 2]),
        _ => {
            let (a, b) = (sorted[n / 2 - 1], sorted[n / 2]);
            Some(a.midpoint(b))
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AggregateError {
    TooFewFresh { fresh: usize, required: usize },
    TooFewAgreeing { agreeing: usize, required: usize },
}

/// Drops stale quotes, then quotes diverging from the median by more than
/// `max_divergence_bps`, and returns the median of the rest with its
/// observation time (the oldest accepted quote).
///
/// # Errors
/// Refuses when fewer than `min_sources` quotes survive either filter.
pub fn aggregate(
    quotes: &[Quote],
    now_ms: u64,
    max_age_ms: u64,
    max_divergence_bps: u64,
    min_sources: usize,
) -> Result<(u128, u64), AggregateError> {
    let fresh: Vec<&Quote> = quotes
        .iter()
        .filter(|q| q.observed_at_ms <= now_ms && now_ms - q.observed_at_ms <= max_age_ms)
        .collect();
    if fresh.len() < min_sources {
        return Err(AggregateError::TooFewFresh {
            fresh: fresh.len(),
            required: min_sources,
        });
    }
    let first = median(&fresh.iter().map(|q| q.price).collect::<Vec<_>>()).ok_or(
        AggregateError::TooFewFresh {
            fresh: 0,
            required: min_sources,
        },
    )?;
    let agreeing: Vec<&&Quote> = fresh
        .iter()
        .filter(|q| {
            q.price.abs_diff(first).saturating_mul(10_000)
                <= first.saturating_mul(u128::from(max_divergence_bps))
        })
        .collect();
    if agreeing.len() < min_sources {
        return Err(AggregateError::TooFewAgreeing {
            agreeing: agreeing.len(),
            required: min_sources,
        });
    }
    let price = median(&agreeing.iter().map(|q| q.price).collect::<Vec<_>>()).ok_or(
        AggregateError::TooFewAgreeing {
            agreeing: 0,
            required: min_sources,
        },
    )?;
    let observed_at = agreeing
        .iter()
        .map(|q| q.observed_at_ms)
        .min()
        .unwrap_or(now_ms);
    Ok((price, observed_at))
}

/// One oracle observation in the kernel `lx_oracle_observation` layout.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Observation {
    pub market_id: [u8; 32],
    pub observation_sequence: u64,
    pub price: u128,
    pub observed_at: u64,
    pub source_identifier: u64,
}

impl Observation {
    /// The 72-byte canonical payload, encoded by the typed perps payload.
    ///
    /// # Errors
    /// Refuses zero fields as the kernel does.
    pub fn encode(&self) -> Result<Vec<u8>, String> {
        PerpsPayload::OraclePush {
            market_id: self.market_id,
            observation_sequence: self.observation_sequence,
            price: self.price,
            observed_at: self.observed_at,
            source_identifier: self.source_identifier,
        }
        .encode()
        .map_err(|e| format!("observation: {e:?}"))
    }

    /// `lx_oracle_observation_sign`: Ed25519 over
    /// `SHA256(signature-preimage tag || "LXP:ORACLE:OBSERVATION:v1" || payload)`.
    ///
    /// # Errors
    /// Refuses a non-canonical observation.
    pub fn signing_digest(&self) -> Result<[u8; 32], String> {
        let mut hasher = Sha256::new();
        hasher.update(Domain::SignaturePreimage.tag());
        hasher.update(OBSERVATION_TAG);
        hasher.update(self.encode()?);
        Ok(hasher.finalize().into())
    }

    /// `lx_oracle_transport_encode`: version, observation, observation signature.
    ///
    /// # Errors
    /// Refuses a non-canonical observation.
    pub fn transport(&self, key: &SigningKey) -> Result<Vec<u8>, String> {
        let signature = key.sign(&self.signing_digest()?).to_bytes();
        let mut out = Vec::with_capacity(TRANSPORT_BYTES);
        out.push(TRANSPORT_VERSION);
        out.extend_from_slice(&self.encode()?);
        out.extend_from_slice(&signature);
        Ok(out)
    }
}

/// Registry declaring only the perps oracle push.
///
/// # Errors
/// Never in practice; surfaces a registry construction refusal.
pub fn oracle_registry() -> Result<ModuleRegistry, String> {
    let kind =
        ActivityType::new(ModuleId::Perps, ORACLE_PUSH_ORDINAL).map_err(|e| format!("{e:?}"))?;
    let registration =
        ModuleRegistration::new(ModuleId::Perps, &[kind]).map_err(|e| format!("{e:?}"))?;
    ModuleRegistry::new(&[registration]).map_err(|e| format!("{e:?}"))
}

/// Envelope scope for one signed oracle activity.
#[derive(Clone, Debug)]
pub struct ActivityScope<'a> {
    pub protocol_version: u16,
    pub network_id: u32,
    pub actor_did: &'a str,
    pub account_sequence: u64,
    pub validity_ms: u64,
    pub fee_limit: u128,
}

/// Builds and signs the oracle push activity (`lx_oracle_activity_encode_signed`):
/// transport payload at the state-commitment protocol, the bare observation with
/// the observation signature otherwise. Returns the signed bytes.
///
/// # Errors
/// Refuses non-canonical fields or an envelope the wire encoder rejects.
pub fn signed_activity(
    observation: &Observation,
    scope: &ActivityScope<'_>,
    key: &SigningKey,
    registry: &ModuleRegistry,
) -> Result<Vec<u8>, String> {
    let kind =
        ActivityType::new(ModuleId::Perps, ORACLE_PUSH_ORDINAL).map_err(|e| format!("{e:?}"))?;
    let transport = scope.protocol_version >= PROTOCOL_VERSION_STATE_COMMITMENT;
    let bytes = if transport {
        observation.transport(key)?
    } else {
        observation.encode()?
    };
    let payload = Payload::new(registry, kind, &bytes).map_err(|e| format!("payload: {e:?}"))?;
    let payload_hash = payload_hash_for(&payload).map_err(|e| format!("{e:?}"))?;
    let mut idempotency = Sha256::new();
    idempotency.update(Domain::ContextHash.tag());
    idempotency.update(&bytes);
    let not_after = observation
        .observed_at
        .checked_add(scope.validity_ms)
        .ok_or("validity overflow")?;
    let public = key.verifying_key().to_bytes();
    let actor = Did::new(scope.actor_did.as_bytes()).map_err(|e| format!("actor did: {e:?}"))?;
    let mut builder = EnvelopeBuilder::new();
    builder
        .protocol_version(scope.protocol_version)
        .and_then(|b| b.network_id(scope.network_id))
        .and_then(|b| b.activity_type(kind))
        .and_then(|b| b.actor_did(actor))
        .and_then(|b| b.authority(Authority::owner(&public)?))
        .and_then(|b| b.account_sequence(scope.account_sequence))
        .and_then(|b| b.timestamp_bound(TimestampBound::new(observation.observed_at, not_after)?))
        .and_then(|b| b.idempotency_key(IdempotencyKey::new(idempotency.finalize().into())))
        .and_then(|b| b.fee_limit(Amount::from_u128(scope.fee_limit)))
        .and_then(|b| b.payload_hash(payload_hash))
        .and_then(|b| b.payload(payload))
        .map_err(|e| format!("envelope: {e:?}"))?;
    let unsigned = builder.build().map_err(|e| format!("envelope: {e:?}"))?;
    let preimage = preimage_unsigned(&unsigned).map_err(|e| format!("{e:?}"))?;
    let signature = key.sign(preimage.as_bytes()).to_bytes();
    let envelope =
        unsigned.attach_signature(Signature::new(&signature).map_err(|e| format!("{e:?}"))?);
    encode_signed_envelope(&envelope).map_err(|e| format!("{e:?}"))
}

/// Reads the oracle key file: 32 raw bytes or 64 hex characters.
///
/// # Errors
/// Refuses an unreadable or malformed key.
pub fn load_key(path: &Path) -> Result<SigningKey, String> {
    let raw =
        Zeroizing::new(fs::read(path).map_err(|e| format!("oracle key {}: {e}", path.display()))?);
    let seed: Zeroizing<Vec<u8>> = if raw.len() == 32 {
        Zeroizing::new(raw.to_vec())
    } else {
        let text = std::str::from_utf8(&raw).map_err(|_| "oracle key is not hex")?;
        Zeroizing::new(hex_decode(text.trim())?)
    };
    let seed: [u8; 32] = seed
        .as_slice()
        .try_into()
        .map_err(|_| "oracle key must be 32 bytes")?;
    let seed = Zeroizing::new(seed);
    Ok(SigningKey::from_bytes(&seed))
}

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct MarketRecord {
    pub observation_sequence: u64,
    pub price: String,
    pub observed_at: u64,
    pub activity_id: String,
}

/// The last submission per market and the next account sequence.
#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct Journal {
    pub next_account_sequence: u64,
    pub markets: BTreeMap<String, MarketRecord>,
}

impl Journal {
    fn path(dir: &Path) -> PathBuf {
        dir.join("journal.json")
    }

    /// Loads the journal, or starts one at `initial_account_sequence`.
    ///
    /// # Errors
    /// Refuses an unreadable or malformed journal.
    pub fn load(dir: &Path, initial_account_sequence: u64) -> Result<Self, String> {
        match fs::read(Self::path(dir)) {
            Ok(bytes) => serde_json::from_slice(&bytes).map_err(|e| format!("journal: {e}")),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self {
                next_account_sequence: initial_account_sequence,
                markets: BTreeMap::new(),
            }),
            Err(e) => Err(format!("journal: {e}")),
        }
    }

    /// Durably replaces the journal (write, fsync, rename).
    ///
    /// # Errors
    /// Refuses when the state directory cannot be written.
    pub fn store(&self, dir: &Path) -> Result<(), String> {
        fs::create_dir_all(dir).map_err(|e| format!("state dir: {e}"))?;
        let temporary = dir.join("journal.json.tmp");
        let bytes = serde_json::to_vec_pretty(self).map_err(|e| format!("journal: {e}"))?;
        let mut file = fs::File::create(&temporary).map_err(|e| format!("journal: {e}"))?;
        file.write_all(&bytes)
            .and_then(|()| file.sync_all())
            .map_err(|e| format!("journal: {e}"))?;
        fs::rename(&temporary, Self::path(dir)).map_err(|e| format!("journal: {e}"))?;
        fs::File::open(dir)
            .and_then(|d| d.sync_all())
            .map_err(|e| format!("journal: {e}"))
    }

    #[must_use]
    pub fn next_observation_sequence(&self, symbol: &str) -> u64 {
        self.markets
            .get(symbol)
            .map_or(1, |m| m.observation_sequence + 1)
    }
}

/// HTTPS client for public quote sources, trusting the platform roots.
///
/// # Errors
/// Refuses when no platform root certificate can be loaded.
pub fn http_agent() -> Result<ureq::Agent, String> {
    let roots: Vec<_> = rustls_native_certs::load_native_certs()
        .certs
        .iter()
        .map(|c| ureq::tls::Certificate::from_der(c.as_ref()).to_owned())
        .collect();
    if roots.is_empty() {
        return Err("no platform root certificates".to_owned());
    }
    let tls = ureq::tls::TlsConfig::builder()
        .provider(ureq::tls::TlsProvider::Rustls)
        .root_certs(ureq::tls::RootCerts::new_with_certs(&roots))
        .build();
    Ok(ureq::Agent::config_builder()
        .timeout_global(Some(Duration::from_secs(5)))
        .max_redirects(0)
        .tls_config(tls)
        .build()
        .into())
}

/// Fetches one live quote.
///
/// # Errors
/// Refuses transport failures, non-2xx responses and unparseable bodies.
pub fn fetch_quote(
    agent: &ureq::Agent,
    source: &SourceConfig,
    symbol: &str,
    now_ms: u64,
    decimals: u32,
) -> Result<Quote, String> {
    let pair = source
        .pairs
        .get(symbol)
        .ok_or_else(|| format!("{}: no pair for {symbol}", source.name))?;
    let url = source.url.replace("{pair}", pair);
    let body = agent
        .get(&url)
        .call()
        .map_err(|e| format!("{}: {e}", source.name))?
        .body_mut()
        .read_to_string()
        .map_err(|e| format!("{}: {e}", source.name))?;
    let body: Value = serde_json::from_str(&body).map_err(|e| format!("{}: {e}", source.name))?;
    parse_quote(source, pair, &body, now_ms, decimals)
}

/// Whether the feeder is ready: a round was accepted no longer than
/// [`HEALTH_WINDOW_CADENCES`] cadences ago. `last_accepted_ms` 0 means never.
#[must_use]
pub fn ready(last_accepted_ms: u64, now_ms: u64, cadence_ms: u64) -> bool {
    last_accepted_ms != 0
        && now_ms.saturating_sub(last_accepted_ms)
            <= cadence_ms.saturating_mul(HEALTH_WINDOW_CADENCES)
}

fn abi_word(value: usize) -> [u8; 32] {
    let mut word = [0; 32];
    word[32 - std::mem::size_of::<usize>()..].copy_from_slice(&value.to_be_bytes());
    word
}

fn abi_bytes(bytes: &[u8], out: &mut Vec<u8>) {
    out.extend_from_slice(&abi_word(bytes.len()));
    out.extend_from_slice(bytes);
    out.resize(out.len() + (32 - bytes.len() % 32) % 32, 0);
}

/// layerxexchange `pushOracle(bytes observation, bytes signature)` calldata:
/// the 72-byte observation and its 64-byte oracle-key signature.
///
/// # Errors
/// Refuses a non-canonical observation.
pub fn push_oracle(observation: &Observation, key: &SigningKey) -> Result<Vec<u8>, String> {
    let transport = observation.transport(key)?;
    let (observation, signature) = transport[1..].split_at(OBSERVATION_BYTES);
    let mut data = Vec::with_capacity(292);
    data.extend_from_slice(&PUSH_ORACLE_SELECTOR);
    data.extend_from_slice(&abi_word(0x40));
    data.extend_from_slice(&abi_word(0x40 + 32 + observation.len().div_ceil(32) * 32));
    abi_bytes(observation, &mut data);
    abi_bytes(signature, &mut data);
    Ok(data)
}

/// Decodes an ABI `address` return word.
///
/// # Errors
/// Refuses anything other than 32 bytes with 12 leading zero bytes.
pub fn address_word(text: &str) -> Result<[u8; 20], String> {
    let word = hex_decode(text.strip_prefix("0x").unwrap_or(text))?;
    if word.len() != 32 || word[..12].iter().any(|byte| *byte != 0) {
        return Err("malformed address word".to_owned());
    }
    word[12..]
        .try_into()
        .map_err(|_| "malformed address word".to_owned())
}

/// Refuses an EVM sender other than the 0x1004-bound address of the oracle key.
///
/// # Errors
/// Names both addresses when they differ.
pub fn require_bound_sender(bound: [u8; 20], sender: [u8; 20]) -> Result<(), String> {
    if bound == sender {
        Ok(())
    } else {
        Err(format!(
            "EVM sender 0x{} is not 0x{}, the 0x1004-bound address of the oracle key",
            hex_encode(&sender),
            hex_encode(&bound)
        ))
    }
}

/// Reads a `pax_sequencerReceipt` answer: `Ok(true)` once receipted (or
/// committed) with status 1, `Ok(false)` while unknown or only submitted.
///
/// # Errors
/// Refuses a reverted push and an unknown rung.
pub fn receipt_accepted(answer: &Value) -> Result<bool, String> {
    if answer.is_null() {
        return Ok(false);
    }
    match answer.get("rung").and_then(Value::as_str) {
        Some("submitted") => Ok(false),
        Some("receipted" | "committed") => match answer.get("status").and_then(Value::as_str) {
            Some("0x1") => Ok(true),
            Some("0x0") => Err(format!(
                "pushOracle reverted: {}",
                answer
                    .get("returnData")
                    .and_then(Value::as_str)
                    .unwrap_or("")
            )),
            _ => Err("receipt without a status".to_owned()),
        },
        _ => Err("receipt with an unknown rung".to_owned()),
    }
}

fn quantity(value: &Value) -> Result<u128, String> {
    value
        .as_str()
        .and_then(|text| text.strip_prefix("0x"))
        .and_then(|text| u128::from_str_radix(text, 16).ok())
        .ok_or_else(|| format!("malformed quantity {value}"))
}

/// JSON-RPC client of the sequencer endpoint.
pub struct Sequencer {
    agent: ureq::Agent,
    url: String,
}

/// Where a sent push stands.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Push {
    /// `pax_sequencerReceipt` answered receipted with status 1.
    Accepted(String),
    /// Sent, but not receipted before the deadline.
    Pending(String),
}

impl Sequencer {
    /// # Errors
    /// Refuses an endpoint that is not an http or https URL.
    pub fn new(agent: ureq::Agent, url: String) -> Result<Self, String> {
        if url.starts_with("https://") || url.starts_with("http://") {
            Ok(Self { agent, url })
        } else {
            Err("sequencer endpoint must be an http or https URL".to_owned())
        }
    }

    /// One JSON-RPC call.
    ///
    /// # Errors
    /// Refuses transport failures, malformed answers and JSON-RPC errors.
    pub fn call(&self, method: &str, params: &Value) -> Result<Value, String> {
        let request = json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params});
        let body = self
            .agent
            .post(&self.url)
            .send_json(&request)
            .map_err(|e| format!("{method}: {e}"))?
            .body_mut()
            .read_to_string()
            .map_err(|e| format!("{method}: {e}"))?;
        let mut answer: Value =
            serde_json::from_str(&body).map_err(|e| format!("{method}: {e}"))?;
        if let Some(error) = answer.get("error") {
            return Err(format!("{method}: {error}"));
        }
        answer
            .get_mut("result")
            .map(Value::take)
            .ok_or_else(|| format!("{method}: no result"))
    }

    /// Refuses `sender` unless addr 0x1004 binds the oracle key to it.
    ///
    /// # Errors
    /// Refuses an unbound oracle key or a different sender.
    pub fn check_sender(&self, key: &SigningKey, sender: [u8; 20]) -> Result<(), String> {
        let mut data = GET_EVM_ADDR_BY_LAYERX_SELECTOR.to_vec();
        data.extend_from_slice(&key.verifying_key().to_bytes());
        let bound = self.call(
            "eth_call",
            &json!([{
                "to": format!("0x{}", hex_encode(&ADDR_PRECOMPILE)),
                "data": format!("0x{}", hex_encode(&data)),
            }, "latest"]),
        )?;
        let bound = address_word(bound.as_str().ok_or("eth_call: malformed result")?)?;
        require_bound_sender(bound, sender)
    }

    /// Signs `data` to layerxexchange with `sender`, sends it and waits for
    /// `pax_sequencerReceipt` until `deadline`.
    ///
    /// # Errors
    /// Refuses estimation or send failures, a reverted push and malformed answers.
    pub fn push(
        &self,
        sender: &EvmSender,
        chain_id: u64,
        data: Vec<u8>,
        deadline: Instant,
    ) -> Result<Push, String> {
        let from = format!("0x{}", hex_encode(&sender.address()));
        let to = format!("0x{}", hex_encode(&LAYERX_EXCHANGE));
        let input = format!("0x{}", hex_encode(&data));
        let nonce = quantity(&self.call("eth_getTransactionCount", &json!([from, "pending"]))?)?;
        let gas_limit = quantity(&self.call(
            "eth_estimateGas",
            &json!([{"from": from, "to": to, "data": input}]),
        )?)?;
        let priority = quantity(&self.call("eth_maxPriorityFeePerGas", &json!([]))?)?;
        let gas_price = quantity(&self.call("eth_gasPrice", &json!([]))?)?;
        let call = Eip1559Call {
            chain_id,
            nonce: u64::try_from(nonce).map_err(|_| "nonce overflow")?,
            max_priority_fee_per_gas: priority,
            max_fee_per_gas: gas_price.max(priority),
            gas_limit: u64::try_from(gas_limit).map_err(|_| "gas overflow")?,
            to: LAYERX_EXCHANGE,
            data,
        };
        let raw = sender.sign_transaction(&call).map_err(|e| e.to_string())?;
        let hash = self.call(
            "eth_sendRawTransaction",
            &json!([format!("0x{}", hex_encode(&raw))]),
        )?;
        let hash = hash
            .as_str()
            .ok_or("eth_sendRawTransaction: malformed hash")?
            .to_owned();
        loop {
            if receipt_accepted(&self.call("pax_sequencerReceipt", &json!([hash]))?)? {
                return Ok(Push::Accepted(hash));
            }
            if Instant::now() >= deadline {
                return Ok(Push::Pending(hash));
            }
            std::thread::sleep(RECEIPT_POLL);
        }
    }
}

/// The observation one round pushes for `market` from `quotes`.
///
/// # Errors
/// Refuses quotes that do not aggregate and a malformed market id.
pub fn round_observation(
    config: &Config,
    market: &MarketConfig,
    quotes: &[Quote],
    journal: &Journal,
    now_ms: u64,
) -> Result<Observation, String> {
    let (price, observed_at) = aggregate(
        quotes,
        now_ms,
        config.max_quote_age_ms,
        config.max_divergence_bps,
        config.min_sources,
    )
    .map_err(|e| format!("rejected: {e:?}"))?;
    Ok(Observation {
        market_id: market_id(&market.market_id)?,
        observation_sequence: journal.next_observation_sequence(&market.symbol),
        price,
        observed_at,
        source_identifier: config.source_identifier,
    })
}

/// Runs one feed round over every market. Returns the markets whose push
/// `pax_sequencerReceipt` answered receipted.
///
/// # Errors
/// Refuses only journal write failures; per-market failures are logged.
#[allow(clippy::too_many_arguments)]
pub fn run_round(
    config: &Config,
    agent: &ureq::Agent,
    key: &SigningKey,
    sender: &EvmSender,
    sequencer: &Sequencer,
    journal: &mut Journal,
    state_dir: &Path,
    now_ms: u64,
) -> Result<usize, String> {
    let mut accepted = 0;
    for market in &config.markets {
        let started = Instant::now();
        let quotes: Vec<Quote> = config
            .sources
            .iter()
            .filter_map(|source| {
                match fetch_quote(agent, source, &market.symbol, now_ms, config.price_decimals) {
                    Ok(quote) => Some(quote),
                    Err(error) => {
                        eprintln!("feeder: {} quote: {error}", market.symbol);
                        None
                    }
                }
            })
            .collect();
        let observation = match round_observation(config, market, &quotes, journal, now_ms) {
            Ok(observation) => observation,
            Err(error) => {
                eprintln!("feeder: {} {error}", market.symbol);
                continue;
            }
        };
        let deadline = started + Duration::from_millis(config.cadence_ms);
        let push = push_oracle(&observation, key)
            .and_then(|data| sequencer.push(sender, u64::from(config.network_id), data, deadline));
        let hash = match push {
            Ok(Push::Accepted(hash)) => {
                accepted += 1;
                hash
            }
            Ok(Push::Pending(hash)) => {
                eprintln!("feeder: {} {hash} not receipted yet", market.symbol);
                hash
            }
            Err(error) => {
                eprintln!("feeder: {} {error}", market.symbol);
                continue;
            }
        };
        // A sent observation sequence is spent either way: the chain only needs it to increase.
        journal.markets.insert(
            market.symbol.clone(),
            MarketRecord {
                observation_sequence: observation.observation_sequence,
                price: observation.price.to_string(),
                observed_at: observation.observed_at,
                activity_id: hash.trim_start_matches("0x").to_owned(),
            },
        );
        journal.store(state_dir)?;
        eprintln!(
            "feeder: {} {} seq {} in {:?}",
            market.symbol,
            observation.price,
            observation.observation_sequence,
            started.elapsed()
        );
    }
    Ok(accepted)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn word(value: usize) -> [u8; 32] {
        abi_word(value)
    }

    fn config() -> Result<Config, String> {
        Config::load(&Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/feeder.json"))
    }

    #[test]
    fn one_round_pushes_one_observation_and_signature() -> Result<(), String> {
        let config = config()?;
        let market = &config.markets[0];
        assert_eq!(market.symbol, "BTC-USD");
        let now = 1_759_735_805_000;
        let price = parse_fixed("65000.00", config.price_decimals)?;
        let quotes: Vec<Quote> = ["coinbase", "kraken", "bitstamp"]
            .iter()
            .map(|source| Quote {
                source: (*source).to_owned(),
                price,
                observed_at_ms: now,
            })
            .collect();
        let journal = Journal::load(&std::env::temp_dir().join("layerx-feeder-absent"), 1)?;
        let observation = round_observation(&config, market, &quotes, &journal, now)?;
        assert_eq!(
            observation,
            Observation {
                market_id: [0x11; 32],
                observation_sequence: 1,
                price: 6_500_000_000_000,
                observed_at: now,
                source_identifier: 1,
            }
        );

        let key = SigningKey::from_bytes(&[0x42; 32]);
        let data = push_oracle(&observation, &key)?;
        assert_eq!(data.len(), 292);
        assert_eq!(data[..4], PUSH_ORACLE_SELECTOR);
        assert_eq!(data[4..36], word(0x40));
        assert_eq!(data[36..68], word(0xc0));
        assert_eq!(data[68..100], word(72));
        assert_eq!(data[100..172], observation.encode()?);
        assert_eq!(data[172..196], [0; 24]);
        assert_eq!(data[196..228], word(64));
        let signature: [u8; 64] = data[228..292].try_into().map_err(|_| "signature length")?;
        layerx_crypto::ed25519::verify_digest(
            &key.verifying_key().to_bytes(),
            &signature,
            &observation.signing_digest()?,
        )
        .map_err(|e| format!("observation signature: {e:?}"))?;

        let directory =
            std::env::temp_dir().join(format!("layerx-feeder-sender-{}", std::process::id()));
        fs::create_dir_all(&directory).map_err(|e| e.to_string())?;
        let file = directory.join("sender.key");
        fs::write(&file, format!("{:064x}", 1)).map_err(|e| e.to_string())?;
        std::env::set_var("LAYERX_FEEDER_TEST_SENDER_KEY_FILE", &file);
        let sender = EvmSender::from_env("LAYERX_FEEDER_TEST_SENDER").map_err(|e| e.to_string())?;
        fs::remove_dir_all(&directory).map_err(|e| e.to_string())?;
        let call = Eip1559Call {
            chain_id: u64::from(config.network_id),
            nonce: 0,
            max_priority_fee_per_gas: 1,
            max_fee_per_gas: 2,
            gas_limit: 120_000,
            to: LAYERX_EXCHANGE,
            data: data.clone(),
        };
        let raw = sender.sign_transaction(&call).map_err(|e| e.to_string())?;
        assert_eq!(raw[0], 2);
        assert!(raw.windows(data.len()).any(|window| window == data));
        Ok(())
    }

    #[test]
    fn unbound_sender_is_refused_at_start() -> Result<(), String> {
        let bound =
            address_word("0x0000000000000000000000007e5f4552091a69125d5dfcb7b8c2659029395bdf")?;
        assert_eq!(
            bound,
            *b"\x7e\x5f\x45\x52\x09\x1a\x69\x12\x5d\x5d\xfc\xb7\xb8\xc2\x65\x90\x29\x39\x5b\xdf"
        );
        assert_eq!(require_bound_sender(bound, bound), Ok(()));
        assert_eq!(
            require_bound_sender(bound, [0x22; 20]),
            Err(format!(
                "EVM sender 0x{} is not 0x7e5f4552091a69125d5dfcb7b8c2659029395bdf, the 0x1004-bound address of the oracle key",
                "22".repeat(20)
            ))
        );
        assert!(address_word(&format!("0x01{}", "00".repeat(31))).is_err());
        assert!(address_word("0x00").is_err());
        Ok(())
    }

    #[test]
    fn push_counts_only_when_receipted() {
        assert_eq!(receipt_accepted(&Value::Null), Ok(false));
        assert_eq!(
            receipt_accepted(&json!({"rung": "submitted", "txHash": "0x01", "nonce": "0x0"})),
            Ok(false)
        );
        assert_eq!(
            receipt_accepted(&json!({"rung": "receipted", "status": "0x1"})),
            Ok(true)
        );
        assert_eq!(
            receipt_accepted(&json!({"rung": "committed", "status": "0x1"})),
            Ok(true)
        );
        assert_eq!(
            receipt_accepted(
                &json!({"rung": "receipted", "status": "0x0", "returnData": "0x08c379a0"})
            ),
            Err("pushOracle reverted: 0x08c379a0".to_owned())
        );
        assert!(receipt_accepted(&json!({"rung": "receipted"})).is_err());
        assert!(receipt_accepted(&json!({"rung": "pending"})).is_err());
    }

    #[test]
    fn health_turns_not_ready_after_three_cadences() {
        assert!(!ready(0, 1_000, 1_000));
        assert!(ready(1_000, 1_000, 1_000));
        assert!(ready(1_000, 4_000, 1_000));
        assert!(!ready(1_000, 4_001, 1_000));
    }
}
