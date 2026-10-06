use std::path::Path;

use ed25519_dalek::SigningKey;
use layerx_oracle_feeder::{
    aggregate, hex_decode, median, oracle_registry, parse_fixed, parse_quote, signed_activity,
    ActivityScope, AggregateError, Config, Journal, MarketRecord, Observation, Quote,
};
use layerx_types::payload::{ActivityType, ModuleId, PerpsPayload};
use serde_json::Value;

type Result = std::result::Result<(), String>;

fn fixture(name: &str) -> std::result::Result<Value, String> {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name);
    let text = std::fs::read_to_string(&path).map_err(|e| e.to_string())?;
    serde_json::from_str(&text).map_err(|e| e.to_string())
}

fn config() -> std::result::Result<Config, String> {
    Config::load(&Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/feeder.json"))
}

fn quote(source: &str, price: u128, at: u64) -> Quote {
    Quote {
        source: source.to_owned(),
        price,
        observed_at_ms: at,
    }
}

/// Kernel `lx_oracle_observation_encode` layout: market id, sequence,
/// u128 big-endian price, observed_at, source identifier.
const KERNEL_VECTOR: &str = concat!(
    "1111111111111111111111111111111111111111111111111111111111111111",
    "0000000000000001",
    "00000000000000000000000005f5e100",
    "0000018bcfe56800",
    "0000000000000007",
);

fn vector_observation() -> Observation {
    Observation {
        market_id: [0x11; 32],
        observation_sequence: 1,
        price: 100_000_000,
        observed_at: 1_700_000_000_000,
        source_identifier: 7,
    }
}

#[test]
fn median_odd_even_and_empty() {
    assert_eq!(median(&[]), None);
    assert_eq!(median(&[5]), Some(5));
    assert_eq!(median(&[9, 1, 5]), Some(5));
    assert_eq!(median(&[1, 4, 2, 3]), Some(2));
    assert_eq!(median(&[3, 4]), Some(3));
    assert_eq!(median(&[u128::MAX, u128::MAX]), Some(u128::MAX));
}

#[test]
fn stale_quotes_are_rejected() {
    let now = 100_000;
    let quotes = [
        quote("a", 100, now - 20_000),
        quote("b", 101, now - 1_000),
        quote("c", 99, now + 5),
    ];
    assert_eq!(
        aggregate(&quotes, now, 10_000, 50, 2),
        Err(AggregateError::TooFewFresh {
            fresh: 1,
            required: 2
        })
    );
    let quotes = [quote("a", 100, now - 9_000), quote("b", 102, now - 1_000)];
    assert_eq!(
        aggregate(&quotes, now, 10_000, 500, 2),
        Ok((101, now - 9_000))
    );
}

#[test]
fn divergent_quotes_are_rejected() {
    let now = 50_000;
    let quotes = [
        quote("a", 10_000, now),
        quote("b", 10_004, now),
        quote("c", 11_000, now),
    ];
    // 50 bps of 10_004 is ~50, so 11_000 is dropped and the median of the rest is taken.
    assert_eq!(aggregate(&quotes, now, 10_000, 50, 2), Ok((10_002, now)));
    assert_eq!(
        aggregate(&quotes, now, 10_000, 50, 3),
        Err(AggregateError::TooFewAgreeing {
            agreeing: 2,
            required: 3
        })
    );
}

#[test]
fn fixed_point_parsing() -> Result {
    assert_eq!(parse_fixed("62431.26", 8)?, 6_243_126_000_000);
    assert_eq!(parse_fixed("62433.1000000099", 8)?, 6_243_310_000_000);
    assert_eq!(parse_fixed("7", 2)?, 700);
    assert!(parse_fixed("0.000", 8).is_err());
    assert!(parse_fixed("-1", 8).is_err());
    assert!(parse_fixed("1e5", 8).is_err());
    Ok(())
}

#[test]
fn recorded_source_responses_parse() -> Result {
    let config = config()?;
    let now = 1_759_735_805_000;
    let mut quotes = Vec::new();
    for (source, file) in
        config
            .sources
            .iter()
            .zip(["coinbase_btc.json", "kraken_btc.json", "bitstamp_btc.json"])
    {
        let pair = &source.pairs["BTC-USD"];
        quotes.push(parse_quote(
            source,
            pair,
            &fixture(file)?,
            now,
            config.price_decimals,
        )?);
    }
    assert_eq!(quotes[0].price, 6_243_126_000_000);
    assert_eq!(quotes[1].price, 6_243_310_000_000);
    assert_eq!(quotes[2].price, 6_242_800_000_000);
    assert_eq!(quotes[2].observed_at_ms, 1_759_735_801_000);
    assert_eq!(
        aggregate(
            &quotes,
            now,
            config.max_quote_age_ms,
            config.max_divergence_bps,
            config.min_sources
        ),
        Ok((6_243_126_000_000, 1_759_735_801_000))
    );
    Ok(())
}

#[test]
fn observation_matches_kernel_vector() -> Result {
    let observation = vector_observation();
    let bytes = observation.encode()?;
    assert_eq!(bytes, hex_decode(KERNEL_VECTOR)?);
    let kind = ActivityType::new(ModuleId::Perps, 3).map_err(|e| format!("{e:?}"))?;
    assert_eq!(
        PerpsPayload::decode(kind, &bytes).map_err(|e| format!("{e:?}"))?,
        PerpsPayload::OraclePush {
            market_id: [0x11; 32],
            observation_sequence: 1,
            price: 100_000_000,
            observed_at: 1_700_000_000_000,
            source_identifier: 7,
        }
    );
    let zero = Observation {
        price: 0,
        ..observation
    };
    assert!(zero.encode().is_err());
    Ok(())
}

#[test]
fn transport_and_activity_are_signed_by_the_oracle_key() -> Result {
    let key = SigningKey::from_bytes(&[0x42; 32]);
    let public = key.verifying_key().to_bytes();
    let observation = vector_observation();
    let transport = observation.transport(&key)?;
    assert_eq!(transport.len(), 137);
    assert_eq!(transport[0], 1);
    assert_eq!(&transport[1..73], hex_decode(KERNEL_VECTOR)?.as_slice());
    let signature: [u8; 64] = transport[73..].try_into().map_err(|_| "signature length")?;
    layerx_crypto::ed25519::verify_digest(&public, &signature, &observation.signing_digest()?)
        .map_err(|e| format!("observation signature: {e:?}"))?;

    let registry = oracle_registry()?;
    let scope = ActivityScope {
        protocol_version: 3,
        network_id: 125,
        actor_did: "did:layerx:oracle-feeder",
        account_sequence: 9,
        validity_ms: 60_000,
        fee_limit: 1_000_000,
    };
    let signed = signed_activity(&observation, &scope, &key, &registry)?;
    let activity =
        layerx_wire::activity::decode_signed(&signed, &registry).map_err(|e| format!("{e:?}"))?;
    assert_eq!(activity.payload(), transport.as_slice());
    assert_eq!(activity.authority(), public.as_slice());
    assert_eq!(activity.account_sequence(), 9);
    assert_eq!(activity.timestamp_bound().not_before, 1_700_000_000_000);
    assert_eq!(activity.timestamp_bound().not_after, 1_700_000_060_000);
    let preimage = layerx_wire::sign::preimage(&activity).map_err(|e| format!("{e:?}"))?;
    let signature: [u8; 64] = activity
        .signature()
        .ok_or("unsigned")?
        .try_into()
        .map_err(|_| "signature length")?;
    layerx_crypto::ed25519::verify_digest(&public, &signature, preimage.as_bytes())
        .map_err(|e| format!("activity signature: {e:?}"))?;
    Ok(())
}

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
            observation_sequence: 12,
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
