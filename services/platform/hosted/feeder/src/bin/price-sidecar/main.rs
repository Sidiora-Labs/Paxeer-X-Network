//! Validator price sidecar: reads the Chainflow feed for every market once
//! per cadence and submits one signed `MsgSubmitPrices` Cosmos transaction.

mod health;
mod round;

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use layerx_oracle_feeder::chainflow::ChainflowConfig;
use layerx_oracle_feeder::cosmos_tx::{compressed_public_key, MAX_OBSERVATIONS};
use layerx_oracle_feeder::{hex_decode, http_agent};
use serde::Deserialize;
use zeroize::Zeroizing;

const DEFAULT_CADENCE_MS: u64 = 1000;
const DEFAULT_HEALTH_ADDR: &str = "[::]:8080";

fn default_cadence_ms() -> u64 {
    DEFAULT_CADENCE_MS
}

/// One pricefeed market: its on-chain id and its Chainflow request symbol.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct MarketConfig {
    pub market_id: u64,
    pub symbol: String,
}

/// The sidecar configuration file named by `PRICE_SIDECAR_CONFIG`.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub chain_id: String,
    /// 20-byte operator address of the validator, hex.
    pub validator: String,
    /// 20-byte feed account of the registered feed key, hex.
    pub feed_account: String,
    pub gas_limit: u64,
    #[serde(default = "default_cadence_ms")]
    pub cadence_ms: u64,
    pub retry_backoff_ms: u64,
    pub retry_max_attempts: u32,
    pub chainflow: ChainflowConfig,
    pub markets: Vec<MarketConfig>,
}

impl Config {
    /// Reads and validates the configuration file.
    ///
    /// # Errors
    /// Refuses an unreadable or malformed file and the refusals of [`Config::validate`].
    pub fn load(path: &Path) -> Result<Self, String> {
        let text =
            fs::read_to_string(path).map_err(|e| format!("config {}: {e}", path.display()))?;
        let config: Self = serde_json::from_str(&text).map_err(|e| format!("config: {e}"))?;
        config.validate()?;
        Ok(config)
    }

    /// # Errors
    /// Names the first empty, zero or malformed field.
    pub fn validate(&self) -> Result<(), String> {
        for (field, value) in [
            ("chain_id", &self.chain_id),
            ("validator", &self.validator),
            ("feed_account", &self.feed_account),
            ("chainflow.base_url", &self.chainflow.base_url),
            ("chainflow.api_key_env", &self.chainflow.api_key_env),
        ] {
            if value.trim().is_empty() {
                return Err(format!("config field {field} is empty"));
            }
        }
        for (field, value) in [
            ("gas_limit", self.gas_limit),
            ("cadence_ms", self.cadence_ms),
            ("retry_backoff_ms", self.retry_backoff_ms),
            ("retry_max_attempts", u64::from(self.retry_max_attempts)),
            ("chainflow.timeout_ms", self.chainflow.timeout_ms),
            (
                "chainflow.max_quote_age_ms",
                self.chainflow.max_quote_age_ms,
            ),
        ] {
            if value == 0 {
                return Err(format!("config field {field} is zero"));
            }
        }
        address("validator", &self.validator)?;
        address("feed_account", &self.feed_account)?;
        if !self.chainflow.base_url.starts_with("https://")
            && !self.chainflow.base_url.starts_with("http://")
        {
            return Err("config field chainflow.base_url must be an http or https URL".to_owned());
        }
        if self.markets.is_empty() {
            return Err("config field markets is empty".to_owned());
        }
        if self.markets.len() > MAX_OBSERVATIONS {
            return Err(format!(
                "config field markets lists {} markets, at most {MAX_OBSERVATIONS}",
                self.markets.len()
            ));
        }
        let mut ids = BTreeSet::new();
        let mut symbols = BTreeSet::new();
        for (index, market) in self.markets.iter().enumerate() {
            if market.symbol.is_empty()
                || !market
                    .symbol
                    .bytes()
                    .all(|b| b.is_ascii_uppercase() || b.is_ascii_digit())
            {
                return Err(format!(
                    "config field markets[{index}].symbol must be upper-case letters and digits"
                ));
            }
            if !ids.insert(market.market_id) || !symbols.insert(market.symbol.as_str()) {
                return Err(format!(
                    "config field markets[{index}] repeats a market id or symbol"
                ));
            }
        }
        Ok(())
    }

    /// # Errors
    /// Refuses a malformed validator address.
    pub fn validator(&self) -> Result<[u8; 20], String> {
        address("validator", &self.validator)
    }

    /// # Errors
    /// Refuses a malformed feed account address.
    pub fn feed_account(&self) -> Result<[u8; 20], String> {
        address("feed_account", &self.feed_account)
    }
}

fn address(field: &str, text: &str) -> Result<[u8; 20], String> {
    let text = text.trim();
    hex_decode(text.strip_prefix("0x").unwrap_or(text))
        .ok()
        .and_then(|bytes| <[u8; 20]>::try_from(bytes).ok())
        .filter(|bytes| bytes.iter().any(|byte| *byte != 0))
        .ok_or_else(|| format!("config field {field} must be a non-zero 20-byte hex address"))
}

fn env(name: &str) -> Result<String, String> {
    std::env::var(name).map_err(|_| format!("{name} is required"))
}

/// Wall-clock milliseconds since the Unix epoch.
#[must_use]
pub fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| u64::try_from(d.as_millis()).unwrap_or(u64::MAX))
}

/// Reads the feed key file: 32 raw bytes or 64 hex characters.
fn load_secret(path: &Path) -> Result<Zeroizing<[u8; 32]>, String> {
    let raw =
        Zeroizing::new(fs::read(path).map_err(|e| format!("feed key {}: {e}", path.display()))?);
    let bytes: Zeroizing<Vec<u8>> = if raw.len() == 32 {
        Zeroizing::new(raw.to_vec())
    } else {
        let text = std::str::from_utf8(&raw).map_err(|_| "feed key is not hex")?;
        Zeroizing::new(hex_decode(text.trim())?)
    };
    if bytes.len() != 32 {
        return Err("feed key must be 32 bytes".to_owned());
    }
    let mut secret = Zeroizing::new([0u8; 32]);
    secret.copy_from_slice(&bytes);
    compressed_public_key(&secret)?;
    Ok(secret)
}

fn run(check_config: bool) -> Result<(), String> {
    let config = Config::load(&PathBuf::from(env("PRICE_SIDECAR_CONFIG")?))?;
    if check_config {
        println!("price-sidecar: config ok");
        return Ok(());
    }
    let secret = load_secret(&PathBuf::from(env("PRICE_SIDECAR_FEED_KEY_FILE")?))?;
    let api_key = config.chainflow.api_key()?;
    let state_dir = PathBuf::from(env("PRICE_SIDECAR_STATE_DIR")?);
    let health = std::env::var("PRICE_SIDECAR_HEALTH_ADDR")
        .unwrap_or_else(|_| DEFAULT_HEALTH_ADDR.to_owned());
    let agent = http_agent()?;
    let rpc = round::Rpc::new(
        agent.clone(),
        env("PRICE_SIDECAR_CONSENSUS_RPC_URL")?,
        env("PRICE_SIDECAR_EVM_RPC_URL")?,
    )?;
    let mut sidecar = round::start(&config, &rpc, secret, &state_dir)?;
    let metrics = Arc::new(Mutex::new(health::Metrics::default()));
    health::serve(&health, Arc::clone(&metrics), config.cadence_ms)?;
    let cadence = Duration::from_millis(config.cadence_ms);
    loop {
        let started = Instant::now();
        round::run_round(
            &config,
            &agent,
            &api_key,
            &rpc,
            &mut sidecar,
            &metrics,
            &state_dir,
        )?;
        thread::sleep(cadence.saturating_sub(started.elapsed()));
    }
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let check_config = match args.as_slice() {
        [] => false,
        [flag] if flag == "--check-config" => true,
        [flag] if flag == "--version" => {
            println!("price-sidecar {}", env!("CARGO_PKG_VERSION"));
            return;
        }
        _ => {
            eprintln!("usage: price-sidecar [--check-config | --version]");
            std::process::exit(2);
        }
    };
    if let Err(error) = run(check_config) {
        eprintln!("price-sidecar: {error}");
        std::process::exit(1);
    }
}
