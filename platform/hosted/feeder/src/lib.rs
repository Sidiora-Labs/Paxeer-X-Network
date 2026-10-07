//! Oracle feeder for the kernel perps markets: median of public REST quotes,
//! signed oracle observations, LNI submission and an on-disk journal.

use std::collections::BTreeMap;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use ed25519_dalek::{Signer, SigningKey};
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{ConnectionGate, Limits, Uds};
use layerx_client::submit::{submit_signed, Submission, SubmissionContext};
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
use serde_json::Value;
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
const FRAME_BYTES: usize = 1_212_416;

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
    pub lni_socket: PathBuf,
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
    if text.len() % 2 != 0 {
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
            Some(a / 2 + b / 2 + (a % 2 + b % 2) / 2)
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

/// Submits signed bytes over a fresh LNI connection. `Ok(true)` means the core
/// acknowledged admission; `Ok(false)` means the outcome is unknown.
///
/// # Errors
/// Refuses connection, handshake or core admission failures.
pub fn submit(
    config: &Config,
    registry: &ModuleRegistry,
    key: &SigningKey,
    signed: &[u8],
    correlation_id: u64,
) -> Result<bool, String> {
    let limits = Limits {
        maximum_frame_bytes: FRAME_BYTES,
        maximum_connections: 1,
        maximum_streams: 1,
        maximum_queued_bytes: FRAME_BYTES,
        deadline: Duration::from_millis(config.cadence_ms.max(1000)),
    };
    let mut transport = Uds::connect(&config.lni_socket, &ConnectionGate::new(1), limits)
        .map_err(|e| format!("LNI unavailable: {e:?}"))?;
    let handshake = perform(
        &mut transport,
        &HandshakeConfig {
            built_interface_version: Version::V1_4,
            expected_protocol_version: config.protocol_version,
            expected_network_id: config.network_id,
        },
        None,
    )
    .map_err(|e| format!("LNI handshake: {e:?}"))?;
    let context = SubmissionContext {
        interface_version: handshake.node().interface_version,
        protocol_version: config.protocol_version,
        network_id: config.network_id,
        correlation_id,
        signer_public_key: key.verifying_key().to_bytes(),
        attempt: 1,
    };
    match submit_signed(&mut transport, registry, context, signed)
        .map_err(|e| format!("submit: {e:?}"))?
    {
        Submission::Acknowledged(_) => Ok(true),
        Submission::Unknown(_) => Ok(false),
    }
}

/// Runs one feed round over every market. Returns the markets submitted.
///
/// # Errors
/// Refuses only journal write failures; per-market failures are logged.
pub fn run_round(
    config: &Config,
    agent: &ureq::Agent,
    key: &SigningKey,
    registry: &ModuleRegistry,
    journal: &mut Journal,
    state_dir: &Path,
    now_ms: u64,
) -> Result<usize, String> {
    let fee_limit = config.fee_limit()?;
    let mut submitted = 0;
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
        let (price, observed_at) = match aggregate(
            &quotes,
            now_ms,
            config.max_quote_age_ms,
            config.max_divergence_bps,
            config.min_sources,
        ) {
            Ok(value) => value,
            Err(error) => {
                eprintln!("feeder: {} rejected: {error:?}", market.symbol);
                continue;
            }
        };
        let observation = Observation {
            market_id: market_id(&market.market_id)?,
            observation_sequence: journal.next_observation_sequence(&market.symbol),
            price,
            observed_at,
            source_identifier: config.source_identifier,
        };
        let scope = ActivityScope {
            protocol_version: config.protocol_version,
            network_id: config.network_id,
            actor_did: &config.actor_did,
            account_sequence: journal.next_account_sequence,
            validity_ms: config.validity_ms,
            fee_limit,
        };
        let signed = signed_activity(&observation, &scope, key, registry)?;
        match submit(
            config,
            registry,
            key,
            &signed,
            journal.next_account_sequence,
        ) {
            Ok(true) => {
                let activity = layerx_wire::activity::decode_signed(&signed, registry)
                    .map_err(|e| format!("{e:?}"))?;
                let id = layerx_wire::hash::activity_id(&activity).map_err(|e| format!("{e:?}"))?;
                journal.next_account_sequence += 1;
                journal.markets.insert(
                    market.symbol.clone(),
                    MarketRecord {
                        observation_sequence: observation.observation_sequence,
                        price: price.to_string(),
                        observed_at,
                        activity_id: hex_encode(&id),
                    },
                );
                journal.store(state_dir)?;
                submitted += 1;
                eprintln!(
                    "feeder: {} {price} seq {} in {:?}",
                    market.symbol,
                    observation.observation_sequence,
                    started.elapsed()
                );
            }
            // ponytail: an unknown outcome keeps the sequences; resolve by receipt lookup if the core starts refusing them as replays
            Ok(false) => eprintln!("feeder: {} outcome unknown", market.symbol),
            Err(error) => eprintln!("feeder: {} {error}", market.symbol),
        }
    }
    Ok(submitted)
}
