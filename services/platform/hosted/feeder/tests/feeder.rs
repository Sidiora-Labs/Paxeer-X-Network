use std::path::Path;

use layerx_oracle_feeder::{health_status, Journal, MarketRecord};

type Result = std::result::Result<(), String>;

#[test]
fn journal_round_trip() -> Result {
    let dir = std::env::temp_dir().join(format!("layerx-feeder-journal-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    let fresh = Journal::load(&dir, 4)?;
    assert_eq!(fresh.next_account_sequence, 4);
    assert_eq!(fresh.next_observation_sequence("BTC-USD"), 1);
    let mut journal = fresh;
    journal.next_account_sequence = 5;
    journal.markets.insert(
        "BTC-USD".to_owned(),
        MarketRecord {
            sequence: 12,
            price: "6243126000000".to_owned(),
            observed_at: 1_759_735_801_000,
            activity_id: "ab".repeat(32),
        },
    );
    journal.store(&dir)?;
    let loaded = Journal::load(&dir, 1)?;
    assert_eq!(loaded, journal);
    assert_eq!(loaded.next_observation_sequence("BTC-USD"), 13);
    std::fs::remove_dir_all(&dir).map_err(|e| e.to_string())?;
    Ok(())
}

#[test]
fn health_listener_routes() {
    let healthz = b"GET /healthz HTTP/1.1\r\nHost: feeder\r\n\r\n";
    let readyz = b"GET /readyz HTTP/1.1\r\nHost: feeder\r\n\r\n";
    assert_eq!(health_status(healthz, false), "200 OK");
    assert_eq!(health_status(healthz, true), "200 OK");
    assert_eq!(health_status(readyz, true), "200 OK");
    assert_eq!(health_status(readyz, false), "503 Service Unavailable");
    assert_eq!(
        health_status(b"POST /healthz HTTP/1.1\r\n\r\n", true),
        "404 Not Found"
    );
    assert_eq!(
        health_status(b"GET /healthzz HTTP/1.1\r\n\r\n", true),
        "404 Not Found"
    );
    assert_eq!(health_status(b"", true), "404 Not Found");
}

/// The legacy feeder pushed `pushOracle(bytes,bytes)` to 0x1015, which the
/// perps precompile refuses; the price sidecar is the only binary the crate
/// builds and the only one its image runs.
#[test]
fn only_the_price_sidecar_ships() -> Result {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR"));
    let manifest = std::fs::read_to_string(dir.join("Cargo.toml")).map_err(|e| e.to_string())?;
    let bins: Vec<&str> = manifest
        .lines()
        .skip_while(|line| *line != "[[bin]]")
        .filter(|line| line.starts_with("name = "))
        .collect();
    assert_eq!(bins, ["name = \"price-sidecar\""]);
    let dockerfile = std::fs::read_to_string(dir.join("Dockerfile")).map_err(|e| e.to_string())?;
    assert!(dockerfile.contains("--bin price-sidecar"));
    assert!(dockerfile.contains("ENTRYPOINT [\"/usr/local/bin/price-sidecar\"]"));
    assert!(!dockerfile.contains("layerx-oracle-feeder /usr/local/bin"));
    Ok(())
}
