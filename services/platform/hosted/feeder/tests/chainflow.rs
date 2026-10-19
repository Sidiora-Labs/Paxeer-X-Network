use std::path::Path;

use layerx_oracle_feeder::chainflow::{
    status_error, ChainflowConfig, FetchError, Ticker, API_KEY_HEADER,
};

type Result = std::result::Result<(), String>;

fn fixture(name: &str) -> std::result::Result<String, String> {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name);
    std::fs::read_to_string(&path).map_err(|e| e.to_string())
}

fn config() -> ChainflowConfig {
    ChainflowConfig {
        base_url: "https://feed.example".to_owned(),
        api_key_env: "PRICE_FEED_REST_API_KEY".to_owned(),
        timeout_ms: 2_000,
        max_quote_age_ms: 10_000,
    }
}

#[test]
fn ticker_request_carries_api_key() -> Result {
    let dir = std::env::temp_dir().join(format!("chainflow-it-key-{}", std::process::id()));
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let path = dir.join("chainflow.key");
    std::fs::write(&path, "k1\n").map_err(|e| e.to_string())?;
    std::env::remove_var("PRICE_FEED_REST_API_KEY");
    std::env::set_var("PRICE_FEED_REST_API_KEY_FILE", &path);
    let config = config();
    let key = config.api_key();
    std::env::remove_var("PRICE_FEED_REST_API_KEY_FILE");
    std::fs::remove_dir_all(&dir).map_err(|e| e.to_string())?;
    let key = key?;
    assert_eq!(key.as_str(), "k1");

    let agent = ureq::Agent::new_with_defaults();
    let request = config
        .ticker_request(&agent, "BTC", &key)
        .map_err(|e| format!("{e:?}"))?;
    let uri = request.uri_ref().ok_or("request has no uri")?;
    assert_eq!(uri.path(), "/api/markets/ticker/BTC");
    assert_eq!(
        uri.to_string(),
        "https://feed.example/api/markets/ticker/BTC"
    );
    assert_eq!(
        request
            .headers_ref()
            .and_then(|h| h.get(API_KEY_HEADER))
            .map(ureq::http::HeaderValue::as_bytes),
        Some(&b"k1"[..])
    );
    Ok(())
}

#[test]
fn recorded_ticker_parses() -> Result {
    let body = fixture("chainflow_ticker_btc.json")?;
    assert_eq!(
        config().parse_ticker(&body, "BTC", 1_700_000_000_000),
        Ok(Ticker {
            index_price: "65000500000000000000000".to_owned(),
            mark_price: "65010000000000000000000".to_owned(),
            observed_at_ms: 1_700_000_000_000,
        })
    );
    Ok(())
}

#[test]
fn ticker_uses_updated_at() {
    let body = r#"{"symbol":"PERP_BTC_USDC","index_price":65000.5,"mark_price":65010,"timestamp":1699999980000,"updated_at":1700000000000}"#;
    assert_eq!(
        config()
            .parse_ticker(body, "BTC", 1_700_000_000_000)
            .map(|t| t.observed_at_ms),
        Ok(1_700_000_000_000)
    );
}

#[test]
fn ticker_symbol_and_price_required() -> Result {
    let eth = r#"{"symbol":"PERP_ETH_USDC","index_price":3500.25,"mark_price":3501,"updated_at":1700000000000}"#;
    assert_eq!(
        config().parse_ticker(eth, "BTC", 1_700_000_000_000),
        Err(FetchError::Symbol)
    );
    let no_price = fixture("chainflow_ticker_no_price.json")?;
    let refused = config().parse_ticker(&no_price, "BTC", 1_700_000_000_000);
    assert_eq!(refused, Err(FetchError::NoPrice));
    assert_eq!(refused.map_err(|e| e.kind()), Err("no_price"));
    Ok(())
}

#[test]
fn ticker_staleness_and_status() -> Result {
    let stale = fixture("chainflow_ticker_stale.json")?;
    let refused = config().parse_ticker(&stale, "BTC", 1_700_000_010_001);
    assert_eq!(refused, Err(FetchError::Stale));
    assert_eq!(refused.map_err(|e| e.kind()), Err("stale"));
    assert_eq!(
        config()
            .parse_ticker(&stale, "BTC", 1_700_000_010_000)
            .map(|t| t.observed_at_ms),
        Ok(1_700_000_000_000)
    );
    assert_eq!(status_error(503), Err(FetchError::Stale));
    assert_eq!(status_error(429), Err(FetchError::RateLimited));
    assert_eq!(status_error(429).map_err(|e| e.kind()), Err("rate_limited"));
    assert_eq!(status_error(200), Ok(()));
    Ok(())
}
