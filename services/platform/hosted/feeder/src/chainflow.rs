//! Chainflow feed source of the price sidecar: the authenticated ticker
//! request and the exact conversion of its JSON number prices.

use std::collections::BTreeMap;
use std::path::Path;
use std::time::Duration;

use serde::Deserialize;
use serde_json::value::RawValue;
use ureq::typestate::WithoutBody;
use zeroize::Zeroizing;

/// Price decimals of every pricefeed market.
pub const PRICE_DECIMALS: u32 = 18;
/// Header carrying the Chainflow API key.
pub const API_KEY_HEADER: &str = "X-API-Key";
const TICKER_PATH: &str = "/api/markets/ticker/";
/// `2^256 - 1`, the largest representable price.
const MAX_PRICE: &str =
    "115792089237316195423570985008687907853269984665640564039457584007913129639935";
const EXPONENT_CAP: i64 = 1_000_000;

/// Chainflow feed settings; the API key itself stays outside the file.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq)]
pub struct ChainflowConfig {
    pub base_url: String,
    /// Name of the variable holding the key; `<name>_FILE` may name a file instead.
    pub api_key_env: String,
    pub timeout_ms: u64,
    pub max_quote_age_ms: u64,
}

/// Why a market yields no usable ticker this round.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum FetchError {
    Stale,
    RateLimited,
    Symbol,
    NoPrice,
    Transport(String),
}

impl FetchError {
    /// Metric label of the failure.
    #[must_use]
    pub fn kind(&self) -> &'static str {
        match self {
            Self::Stale => "stale",
            Self::RateLimited => "rate_limited",
            Self::Symbol => "symbol",
            Self::NoPrice => "no_price",
            Self::Transport(_) => "transport",
        }
    }
}

/// One accepted ticker, prices as 18-decimal integer strings.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Ticker {
    pub index_price: String,
    pub mark_price: String,
    pub observed_at_ms: u64,
}

#[derive(Deserialize)]
struct RawTicker<'a> {
    symbol: Option<String>,
    #[serde(borrow)]
    index_price: Option<&'a RawValue>,
    #[serde(borrow)]
    mark_price: Option<&'a RawValue>,
    updated_at: Option<i64>,
}

/// The ticker inside a 200 body: `data` of a `{"success": true, "data": {..}}`
/// envelope, or the body itself when it is a bare ticker with `symbol` at the
/// top level.
fn ticker_object(body: &str) -> Result<&str, FetchError> {
    let top: BTreeMap<String, &RawValue> =
        serde_json::from_str(body).map_err(|e| FetchError::Transport(e.to_string()))?;
    let Some(success) = top.get("success") else {
        if top.contains_key("symbol") {
            return Ok(body);
        }
        let keys: Vec<&str> = top.keys().map(String::as_str).collect();
        return Err(FetchError::Transport(format!(
            "unknown ticker body, top-level keys {keys:?}"
        )));
    };
    match serde_json::from_str::<bool>(success.get()) {
        Ok(true) => top
            .get("data")
            .map(|data| data.get())
            .ok_or_else(|| FetchError::Transport("success envelope without data".to_owned())),
        Ok(false) => {
            let message = ["error", "message"]
                .iter()
                .find_map(|key| top.get(*key))
                .map_or("no message", |m| m.get());
            Err(FetchError::Transport(format!(
                "chainflow success=false: {message}"
            )))
        }
        Err(_) => Err(FetchError::Transport(format!(
            "envelope success is not a boolean: {}",
            success.get()
        ))),
    }
}

impl ChainflowConfig {
    /// Reads the API key from the variable `api_key_env`, or from the file
    /// named by `<api_key_env>_FILE`.
    ///
    /// # Errors
    /// Refuses when neither is set, the file is unreadable, or the key is empty.
    pub fn api_key(&self) -> Result<Zeroizing<String>, String> {
        let file_var = format!("{}_FILE", self.api_key_env);
        let key = if let Some(value) = std::env::var_os(&self.api_key_env) {
            Zeroizing::new(
                value
                    .into_string()
                    .map_err(|_| format!("{} is not UTF-8", self.api_key_env))?,
            )
        } else if let Some(path) = std::env::var_os(&file_var) {
            let path = Path::new(&path);
            Zeroizing::new(
                std::fs::read_to_string(path)
                    .map_err(|e| format!("{file_var} {}: {e}", path.display()))?,
            )
        } else {
            return Err(format!(
                "neither {} nor {file_var} is set",
                self.api_key_env
            ));
        };
        let trimmed = key.trim();
        if trimmed.is_empty() {
            return Err("Chainflow API key is empty".to_owned());
        }
        Ok(Zeroizing::new(trimmed.to_owned()))
    }

    /// `GET /api/markets/ticker/{symbol}` with the API key on `agent`; HTTP
    /// statuses are returned, not raised, so [`status_error`] classifies them.
    ///
    /// # Errors
    /// Refuses a request symbol other than upper-case ASCII letters and digits.
    pub fn ticker_request(
        &self,
        agent: &ureq::Agent,
        symbol: &str,
        api_key: &str,
    ) -> Result<ureq::RequestBuilder<WithoutBody>, FetchError> {
        if symbol.is_empty()
            || !symbol
                .bytes()
                .all(|b| b.is_ascii_uppercase() || b.is_ascii_digit())
        {
            return Err(FetchError::Symbol);
        }
        let url = format!(
            "{}{TICKER_PATH}{symbol}",
            self.base_url.trim_end_matches('/')
        );
        Ok(agent
            .get(&url)
            .header(API_KEY_HEADER, api_key)
            .config()
            .http_status_as_error(false)
            .timeout_global(Some(Duration::from_millis(self.timeout_ms)))
            .build())
    }

    /// Fetches and checks one market's ticker; `now_ms` is read once the
    /// response body has arrived, so the quote age is taken at receipt.
    ///
    /// # Errors
    /// Refuses as [`status_error`] and [`ChainflowConfig::parse_ticker`] do,
    /// and transport failures as [`FetchError::Transport`].
    pub fn fetch_ticker(
        &self,
        agent: &ureq::Agent,
        symbol: &str,
        api_key: &str,
        now_ms: impl FnOnce() -> u64,
    ) -> Result<Ticker, FetchError> {
        let mut response = self
            .ticker_request(agent, symbol, api_key)?
            .call()
            .map_err(|e| FetchError::Transport(e.to_string()))?;
        status_error(response.status().as_u16())?;
        let body = response
            .body_mut()
            .read_to_string()
            .map_err(|e| FetchError::Transport(e.to_string()))?;
        self.parse_ticker(&body, symbol, now_ms())
    }

    /// Checks a 200 ticker body, enveloped or bare, for the request `symbol`:
    /// a `success: false` envelope is refused with its message; the response symbol
    /// must be `PERP_<SYMBOL>_USDC`, both prices JSON numbers, and `updated_at`
    /// (the observation time) no older than `max_quote_age_ms`.
    ///
    /// # Errors
    /// [`FetchError::Symbol`], [`FetchError::NoPrice`], [`FetchError::Stale`],
    /// or [`FetchError::Transport`] for a refused envelope or a body that is
    /// not a ticker object, naming the body's top-level keys.
    pub fn parse_ticker(
        &self,
        body: &str,
        symbol: &str,
        now_ms: u64,
    ) -> Result<Ticker, FetchError> {
        let raw: RawTicker<'_> = serde_json::from_str(ticker_object(body)?)
            .map_err(|e| FetchError::Transport(e.to_string()))?;
        if raw.symbol.as_deref() != Some(format!("PERP_{symbol}_USDC").as_str()) {
            return Err(FetchError::Symbol);
        }
        let price = |token: Option<&RawValue>| {
            token
                .and_then(|t| parse_json_number_fixed(t.get(), PRICE_DECIMALS).ok())
                .ok_or(FetchError::NoPrice)
        };
        let index_price = price(raw.index_price)?;
        let mark_price = price(raw.mark_price)?;
        let observed_at_ms = raw
            .updated_at
            .and_then(|at| u64::try_from(at).ok())
            .filter(|at| *at <= now_ms && now_ms - at <= self.max_quote_age_ms)
            .ok_or(FetchError::Stale)?;
        Ok(Ticker {
            index_price,
            mark_price,
            observed_at_ms,
        })
    }
}

/// Classifies a ticker response status: 200 passes, 503 is stale or
/// unknown, 429 is rate limited.
///
/// # Errors
/// Every status other than 200.
pub fn status_error(status: u16) -> Result<(), FetchError> {
    match status {
        200 => Ok(()),
        503 => Err(FetchError::Stale),
        429 => Err(FetchError::RateLimited),
        other => Err(FetchError::Transport(format!("status {other}"))),
    }
}

fn digits(text: &str) -> bool {
    !text.is_empty() && text.bytes().all(|b| b.is_ascii_digit())
}

/// Converts a JSON number token to an unsigned integer with `decimals`
/// places, exactly, truncating toward zero, as a decimal string.
///
/// # Errors
/// Refuses a token outside the JSON number grammar, a negative value, a zero
/// result and a result over `2^256 - 1`.
pub fn parse_json_number_fixed(token: &str, decimals: u32) -> Result<String, String> {
    let malformed = || format!("not a JSON number: {token:?}");
    if token.starts_with('-') {
        return Err(format!("negative price {token:?}"));
    }
    let (mantissa, exponent) = match token.find(['e', 'E']) {
        Some(at) => (&token[..at], Some(&token[at + 1..])),
        None => (token, None),
    };
    let (whole, fraction) = match mantissa.split_once('.') {
        Some((whole, fraction)) if digits(fraction) => (whole, fraction),
        Some(_) => return Err(malformed()),
        None => (mantissa, ""),
    };
    if !digits(whole) || (whole.len() > 1 && whole.starts_with('0')) {
        return Err(malformed());
    }
    let exponent: i64 = match exponent {
        None => 0,
        Some(text) => {
            let (negative, magnitude) = match text.as_bytes().first() {
                Some(b'-') => (true, &text[1..]),
                Some(b'+') => (false, &text[1..]),
                _ => (false, text),
            };
            if !digits(magnitude) {
                return Err(malformed());
            }
            let value = magnitude.bytes().fold(0i64, |acc, b| {
                (acc * 10 + i64::from(b - b'0')).min(EXPONENT_CAP)
            });
            if negative {
                -value
            } else {
                value
            }
        }
    };
    let significand = format!("{whole}{fraction}");
    let significand = significand.trim_start_matches('0');
    let fraction_len = i64::try_from(fraction.len()).map_err(|_| malformed())?;
    let shift = exponent + i64::from(decimals) - fraction_len;
    let max_len = i64::try_from(MAX_PRICE.len()).map_err(|_| malformed())?;
    let kept = i64::try_from(significand.len()).map_err(|_| malformed())? + shift;
    if significand.is_empty() || kept <= 0 {
        return Err(format!("zero price {token:?}"));
    }
    if kept > max_len {
        return Err(format!("price overflow {token:?}"));
    }
    let kept = usize::try_from(kept).map_err(|_| malformed())?;
    let mut value: String = significand.chars().take(kept).collect();
    value.extend(std::iter::repeat_n('0', kept - value.len()));
    if value.len() == MAX_PRICE.len() && value.as_str() > MAX_PRICE {
        return Err(format!("price overflow {token:?}"));
    }
    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::{
        parse_json_number_fixed, status_error, ChainflowConfig, FetchError, Ticker, API_KEY_HEADER,
        MAX_PRICE,
    };
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::Arc;
    use std::thread;

    const BTC: &str = include_str!("../tests/fixtures/chainflow_ticker_btc.json");
    const BTC_ENVELOPE: &str = include_str!("../tests/fixtures/chainflow_ticker_btc_envelope.json");

    fn config(api_key_env: &str) -> ChainflowConfig {
        ChainflowConfig {
            base_url: "https://feed.example/".to_owned(),
            api_key_env: api_key_env.to_owned(),
            timeout_ms: 2_000,
            max_quote_age_ms: 10_000,
        }
    }

    #[test]
    fn json_number_fixed_vectors() {
        let accepted = [
            ("65000.5", "65000500000000000000000"),
            ("1.234e-5", "12340000000000"),
            ("2.5e16", "25000000000000000000000000000000000"),
            ("1E2", "100000000000000000000"),
            ("65010", "65010000000000000000000"),
            ("0.000000000000000001", "1"),
            ("1.9999999999999999999", "1999999999999999999"),
            ("12e+1", "120000000000000000000"),
            ("0.5E-17", "5"),
            (
                "115792089237316195423570985008687907853269984665640564039457.584007913129639935",
                MAX_PRICE,
            ),
        ];
        for (token, expected) in accepted {
            assert_eq!(
                parse_json_number_fixed(token, 18).as_deref(),
                Ok(expected),
                "{token}"
            );
        }
        let refused = [
            "1e-19",
            "-1",
            "0",
            "\"5\"",
            "0.0e5",
            "-0",
            "01",
            "1.",
            ".5",
            "1e",
            "1e+",
            "+1",
            "1.5.0",
            " 1",
            "NaN",
            "",
            "1e999999999999999999999",
            "1e-999999999999999999999",
            "115792089237316195423570985008687907853269984665640564039457.584007913129639936",
        ];
        for token in refused {
            assert!(parse_json_number_fixed(token, 18).is_err(), "{token}");
        }
    }

    #[test]
    fn chainflow_recorded_ticker_parses() {
        assert_eq!(
            config("UNUSED").parse_ticker(BTC, "BTC", 1_700_000_000_500),
            Ok(Ticker {
                index_price: "65000500000000000000000".to_owned(),
                mark_price: "65010000000000000000000".to_owned(),
                observed_at_ms: 1_700_000_000_000,
            })
        );
    }

    #[test]
    fn chainflow_ticker_refusals() {
        let config = config("UNUSED");
        assert_eq!(
            config.parse_ticker(BTC, "ETH", 1_700_000_000_000),
            Err(FetchError::Symbol)
        );
        let no_price =
            r#"{"symbol":"PERP_BTC_USDC","mark_price":65010,"updated_at":1700000000000}"#;
        assert_eq!(
            config.parse_ticker(no_price, "BTC", 1_700_000_000_000),
            Err(FetchError::NoPrice)
        );
        let null_mark = r#"{"symbol":"PERP_BTC_USDC","index_price":1,"mark_price":null,"updated_at":1700000000000}"#;
        assert_eq!(
            config.parse_ticker(null_mark, "BTC", 1_700_000_000_000),
            Err(FetchError::NoPrice)
        );
        let string_price = r#"{"symbol":"PERP_BTC_USDC","index_price":"1","mark_price":1,"updated_at":1700000000000}"#;
        assert_eq!(
            config.parse_ticker(string_price, "BTC", 1_700_000_000_000),
            Err(FetchError::NoPrice)
        );
        assert_eq!(
            config.parse_ticker(BTC, "BTC", 1_700_000_010_001),
            Err(FetchError::Stale)
        );
        assert_eq!(
            config
                .parse_ticker(BTC, "BTC", 1_700_000_010_000)
                .map(|t| t.observed_at_ms),
            Ok(1_700_000_000_000)
        );
        assert_eq!(
            config.parse_ticker(BTC, "BTC", 1_699_999_999_999),
            Err(FetchError::Stale)
        );
        let no_time =
            r#"{"symbol":"PERP_BTC_USDC","index_price":1,"mark_price":1,"updated_at":null}"#;
        assert_eq!(
            config.parse_ticker(no_time, "BTC", 1_700_000_000_000),
            Err(FetchError::Stale)
        );
        assert_eq!(
            config
                .parse_ticker("[]", "BTC", 1_700_000_000_000)
                .map_err(|e| e.kind()),
            Err("transport")
        );
    }

    #[test]
    fn chainflow_enveloped_ticker_parses() {
        assert_eq!(
            config("UNUSED").parse_ticker(BTC_ENVELOPE, "BTC", 1_700_000_000_500),
            Ok(Ticker {
                index_price: "65000500000000000000000".to_owned(),
                mark_price: "65010000000000000000000".to_owned(),
                observed_at_ms: 1_700_000_000_000,
            })
        );
        assert_eq!(
            config("UNUSED").parse_ticker(BTC_ENVELOPE, "ETH", 1_700_000_000_500),
            Err(FetchError::Symbol)
        );
    }

    #[test]
    fn chainflow_envelope_refusals() {
        let config = config("UNUSED");
        assert_eq!(
            config.parse_ticker(
                r#"{"success":false,"error":"Price unavailable or stale"}"#,
                "BTC",
                1_700_000_000_000
            ),
            Err(FetchError::Transport(
                r#"chainflow success=false: "Price unavailable or stale""#.to_owned()
            ))
        );
        assert_eq!(
            config.parse_ticker(r#"{"success":true}"#, "BTC", 1_700_000_000_000),
            Err(FetchError::Transport(
                "success envelope without data".to_owned()
            ))
        );
        assert_eq!(
            config.parse_ticker(r#"{"success":"yes","data":{}}"#, "BTC", 1_700_000_000_000),
            Err(FetchError::Transport(
                r#"envelope success is not a boolean: "yes""#.to_owned()
            ))
        );
        assert_eq!(
            config.parse_ticker(r#"{"result":{},"ok":1}"#, "BTC", 1_700_000_000_000),
            Err(FetchError::Transport(
                r#"unknown ticker body, top-level keys ["ok", "result"]"#.to_owned()
            ))
        );
    }

    #[test]
    fn chainflow_ticker_uses_updated_at() {
        let body = r#"{"symbol":"PERP_BTC_USDC","index_price":65000.5,"mark_price":65010,"timestamp":1699999980000,"updated_at":1700000000000}"#;
        assert_eq!(
            config("UNUSED")
                .parse_ticker(body, "BTC", 1_700_000_000_000)
                .map(|t| t.observed_at_ms),
            Ok(1_700_000_000_000)
        );
    }

    /// Serves one ticker response whose `updated_at` is `updated_at`; the
    /// returned flag turns true once the request has arrived, before the
    /// response is written.
    fn serve_ticker_once(
        updated_at: u64,
    ) -> Result<(String, Arc<AtomicBool>, thread::JoinHandle<()>), String> {
        let listener = TcpListener::bind("127.0.0.1:0").map_err(|e| e.to_string())?;
        let base_url = format!(
            "http://{}",
            listener.local_addr().map_err(|e| e.to_string())?
        );
        let received = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&received);
        let server = thread::spawn(move || {
            let Ok((mut stream, _)) = listener.accept() else {
                return;
            };
            let mut request = Vec::new();
            let mut buf = [0_u8; 1024];
            while !request.windows(4).any(|w| w == b"\r\n\r\n") {
                match stream.read(&mut buf) {
                    Ok(0) | Err(_) => return,
                    Ok(n) => request.extend_from_slice(&buf[..n]),
                }
            }
            flag.store(true, Ordering::SeqCst);
            let body = format!(
                r#"{{"symbol":"PERP_BTC_USDC","index_price":65000.5,"mark_price":65010,"updated_at":{updated_at}}}"#
            );
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = stream.write_all(response.as_bytes());
        });
        Ok((base_url, received, server))
    }

    /// Fetches through the real HTTP path with a clock that reads `START`
    /// until the server has received the request and `START + 100` after.
    fn fetch_with_request_in_flight(updated_at: u64) -> Result<Result<Ticker, FetchError>, String> {
        const START: u64 = 1_700_000_000_000;
        let (base_url, received, server) = serve_ticker_once(updated_at)?;
        let mut config = config("UNUSED");
        config.base_url = base_url;
        let agent = ureq::Agent::new_with_defaults();
        let result = config.fetch_ticker(&agent, "BTC", "k1", || {
            if received.load(Ordering::SeqCst) {
                START + 100
            } else {
                START
            }
        });
        server.join().map_err(|_| "server panicked".to_owned())?;
        Ok(result)
    }

    #[test]
    fn chainflow_fetch_takes_quote_age_at_receipt() -> Result<(), String> {
        assert_eq!(
            fetch_with_request_in_flight(1_700_000_000_050)?,
            Ok(Ticker {
                index_price: "65000500000000000000000".to_owned(),
                mark_price: "65010000000000000000000".to_owned(),
                observed_at_ms: 1_700_000_000_050,
            })
        );
        assert_eq!(
            fetch_with_request_in_flight(1_700_000_000_101)?,
            Err(FetchError::Stale)
        );
        assert_eq!(
            fetch_with_request_in_flight(1_699_999_990_050)?,
            Err(FetchError::Stale)
        );
        assert_eq!(
            fetch_with_request_in_flight(1_699_999_990_100)?.map(|t| t.observed_at_ms),
            Ok(1_699_999_990_100)
        );
        Ok(())
    }

    #[test]
    fn chainflow_status_mapping() {
        assert_eq!(status_error(200), Ok(()));
        assert_eq!(status_error(503), Err(FetchError::Stale));
        assert_eq!(status_error(429), Err(FetchError::RateLimited));
        assert_eq!(
            status_error(401),
            Err(FetchError::Transport("status 401".to_owned()))
        );
        let kinds = [
            FetchError::Stale,
            FetchError::RateLimited,
            FetchError::Symbol,
            FetchError::NoPrice,
            FetchError::Transport(String::new()),
        ]
        .map(|e| e.kind());
        assert_eq!(
            kinds,
            ["stale", "rate_limited", "symbol", "no_price", "transport"]
        );
    }

    #[test]
    fn chainflow_ticker_request_carries_api_key() -> Result<(), String> {
        let dir = std::env::temp_dir().join(format!("chainflow-key-{}", std::process::id()));
        std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
        let path = dir.join("api.key");
        std::fs::write(&path, "k1\n").map_err(|e| e.to_string())?;
        let config = config("CHAINFLOW_UNIT_TEST_API_KEY");
        assert!(config.api_key().is_err());
        std::env::set_var("CHAINFLOW_UNIT_TEST_API_KEY_FILE", &path);
        let key = config.api_key();
        std::env::remove_var("CHAINFLOW_UNIT_TEST_API_KEY_FILE");
        std::fs::remove_dir_all(&dir).map_err(|e| e.to_string())?;
        let key = key?;
        assert_eq!(key.as_str(), "k1");

        let agent = ureq::Agent::new_with_defaults();
        let request = config
            .ticker_request(&agent, "BTC", &key)
            .map_err(|e| format!("{e:?}"))?;
        assert_eq!(
            request.uri_ref().map(ToString::to_string).as_deref(),
            Some("https://feed.example/api/markets/ticker/BTC")
        );
        assert_eq!(
            request
                .headers_ref()
                .and_then(|h| h.get(API_KEY_HEADER))
                .map(ureq::http::HeaderValue::as_bytes),
            Some(&b"k1"[..])
        );
        for symbol in ["", "btc", "BTC/USD", "../x"] {
            assert_eq!(
                config.ticker_request(&agent, symbol, &key).err(),
                Some(FetchError::Symbol),
                "{symbol}"
            );
        }
        Ok(())
    }
}
