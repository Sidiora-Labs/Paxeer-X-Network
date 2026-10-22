//! Shared pieces of the price sidecar: the health listener, the on-disk
//! journal, the JSON-RPC client and the 0x1004 sender binding check.

use std::collections::BTreeMap;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::Duration;

use ed25519_dalek::SigningKey;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

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

/// addr precompile.
pub const ADDR_PRECOMPILE: [u8; 20] = precompile(0x10, 0x04);
/// `getEvmAddrByLayerX(bytes32)`.
pub const GET_EVM_ADDR_BY_LAYERX_SELECTOR: [u8; 4] = [0xc4, 0x13, 0xba, 0xd3];
/// Readiness lasts this many cadences after the last accepted round.
pub const HEALTH_WINDOW_CADENCES: u64 = 3;

const fn precompile(high: u8, low: u8) -> [u8; 20] {
    let mut address = [0; 20];
    address[18] = high;
    address[19] = low;
    address
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

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct MarketRecord {
    #[serde(rename = "observation_sequence")]
    pub sequence: u64,
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
        self.markets.get(symbol).map_or(1, |m| m.sequence + 1)
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

/// Whether the feeder is ready: a round was accepted no longer than
/// [`HEALTH_WINDOW_CADENCES`] cadences ago. `last_accepted_ms` 0 means never.
#[must_use]
pub fn ready(last_accepted_ms: u64, now_ms: u64, cadence_ms: u64) -> bool {
    last_accepted_ms != 0
        && now_ms.saturating_sub(last_accepted_ms)
            <= cadence_ms.saturating_mul(HEALTH_WINDOW_CADENCES)
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

/// JSON-RPC client of the sequencer endpoint.
pub struct Sequencer {
    agent: ureq::Agent,
    url: String,
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
}

#[cfg(test)]
mod tests {
    use super::*;

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
    fn health_turns_not_ready_after_three_cadences() {
        assert!(!ready(0, 1_000, 1_000));
        assert!(ready(1_000, 1_000, 1_000));
        assert!(ready(1_000, 4_000, 1_000));
        assert!(!ready(1_000, 4_001, 1_000));
    }
}
