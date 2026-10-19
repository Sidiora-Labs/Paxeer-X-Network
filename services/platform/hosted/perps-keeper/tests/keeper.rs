#[path = "../src/main.rs"]
mod keeper;

pub use keeper::main;

use keeper::{
    decode_funding, decode_market, decode_oracle, decode_positions, refusal_code, Action, Keeper,
    MarketState, Outcome, PositionState, DUPLICATE_TICK, FUNDING_TICK, LIQUIDATE,
};
use serde_json::json;

type TestResult = Result<(), String>;

const MARKET: [u8; 32] = [0x11; 32];
const QUOTE: [u8; 32] = [0x22; 32];
const INSURANCE: [u8; 32] = [0x33; 32];
const LIQUIDATOR: [u8; 32] = [0x44; 32];
const INTERVAL_MS: u64 = 3_600_000;
const PRICE_SCALE: u128 = 1_000_000;
const MAINTENANCE_BPS: u128 = 500;

fn num(value: u128) -> [u8; 32] {
    let mut out = [0; 32];
    out[16..].copy_from_slice(&value.to_be_bytes());
    out
}

fn concat(words: &[[u8; 32]]) -> Vec<u8> {
    words.iter().flatten().copied().collect()
}

fn market_answer() -> Vec<u8> {
    let mut words = vec![num(32), MARKET, QUOTE, [0x55; 32], [0x56; 32], [0x57; 32]];
    words.extend([[0x58; 32], INSURANCE]);
    words.extend([num(1), num(1), num(1), num(PRICE_SCALE)]);
    words.extend([
        num(1_000),
        num(MAINTENANCE_BPS),
        num(100),
        num(5_000),
        num(50),
        num(1_000),
    ]);
    words.extend([
        num(u128::from(INTERVAL_MS)),
        num(60_000),
        num(1),
        num(u128::MAX),
    ]);
    words.extend([num(24 * 32), num(1), num(0), num(1), [0x66; 32]]);
    concat(&words)
}

fn position_words(id: u8, size: u128, entry_notional: u128) -> Vec<[u8; 32]> {
    vec![
        [id; 32],
        MARKET,
        [0x70 + id; 32],
        [0x80 + id; 32],
        QUOTE,
        num(1),
        num(size),
        num(entry_notional),
        num(0),
        num(1),
    ]
}

fn state(last_tick_ms: u64, positions: &[(u8, u128)]) -> Result<MarketState, String> {
    let market = decode_market(&market_answer()).map_err(|e| e.to_string())?;
    let funding = decode_funding(&concat(&[num(0), num(u128::from(last_tick_ms))]))
        .map_err(|e| e.to_string())?;
    let oracle = decode_oracle(&concat(&[num(7), num(2 * PRICE_SCALE), num(1)]))
        .map_err(|e| e.to_string())?;
    let mut array = vec![num(32), num(positions.len() as u128)];
    for (id, _) in positions {
        array.extend(position_words(*id, 10, 20));
    }
    let decoded = decode_positions(&concat(&array)).map_err(|e| e.to_string())?;
    Ok(MarketState {
        market,
        funding,
        oracle: Some(oracle),
        deficit: 0,
        insurance_balance: 9_000,
        positions: decoded
            .into_iter()
            .zip(positions)
            .map(|(position, (_, margin_balance))| PositionState {
                position,
                margin_balance: *margin_balance,
            })
            .collect(),
    })
}

#[test]
fn funding_tick_due() -> TestResult {
    let now = 10_000_000_000;
    let mut keeper = Keeper::new(LIQUIDATOR);
    let plan = keeper.plan(&state(now - INTERVAL_MS - 1, &[])?, now);
    let expected = Action::FundingTick {
        market: MARKET,
        last_tick_ms: now - INTERVAL_MS - 1,
    };
    assert_eq!(plan.actions, vec![expected.clone()]);
    assert_eq!(
        expected.calldata(),
        [FUNDING_TICK.as_slice(), &MARKET].concat()
    );
    let metrics = keeper.metrics_of(&MARKET).ok_or("no metrics")?;
    assert_eq!(metrics.last_funding_tick_ms, now - INTERVAL_MS - 1);
    assert_eq!(metrics.insurance_fund_balance, 9_000);

    let early = keeper.plan(&state(now - INTERVAL_MS + 1, &[])?, now);
    assert!(early.actions.is_empty());
    Ok(())
}

#[test]
fn liquidates_below_maintenance() -> TestResult {
    let now = 10_000_000_000;
    // Notional = 2 * 10 = 20, maintenance = ceil(20 * 500 / 10000) = 1.
    let mut keeper = Keeper::new(LIQUIDATOR);
    let plan = keeper.plan(&state(now, &[(1, 0), (2, 1)])?, now);
    let expected = Action::Liquidate {
        market: MARKET,
        position: [1; 32],
        liquidator: LIQUIDATOR,
    };
    assert_eq!(plan.actions, vec![expected.clone()]);
    assert_eq!(
        expected.calldata(),
        [LIQUIDATE.as_slice(), &MARKET, &[1; 32], &LIQUIDATOR].concat()
    );
    let metrics = keeper.metrics_of(&MARKET).ok_or("no metrics")?;
    assert_eq!(metrics.below_maintenance_not_liquidated, 1);
    keeper.record(&expected, &Outcome::Accepted("0xab".to_owned()));
    let metrics = keeper.metrics_of(&MARKET).ok_or("no metrics")?;
    assert_eq!(metrics.below_maintenance_not_liquidated, 0);
    assert!(keeper.render_metrics().contains(&format!(
        "layerx_perps_keeper_insurance_fund_balance{{market=\"0x{}\"}} 9000",
        "11".repeat(32)
    )));
    Ok(())
}

#[test]
fn duplicate_tick_not_retried() -> TestResult {
    let now = 10_000_000_000;
    let last = now - INTERVAL_MS - 1;
    let mut keeper = Keeper::new(LIQUIDATOR);
    let plan = keeper.plan(&state(last, &[])?, now);
    let [tick] = plan.actions.as_slice() else {
        return Err(format!("expected one tick, planned {:?}", plan.actions));
    };

    let mut revert = vec![0x08, 0xc3, 0x79, 0xa0];
    revert.extend(num(32));
    revert.extend(num(DUPLICATE_TICK.len() as u128));
    let mut text = [0; 32];
    text[..DUPLICATE_TICK.len()].copy_from_slice(DUPLICATE_TICK.as_bytes());
    revert.extend(text);
    let error = json!({"code": 3, "message": "execution reverted", "data": format!("0x{}", keeper::hex_encode(&revert))});
    let code = refusal_code(&error).ok_or("no refusal code")?;
    assert_eq!(code, DUPLICATE_TICK);

    let line = keeper.record(tick, &Outcome::Refused(code));
    assert!(line.contains("refused LXP_NOT_YET_VALID"), "{line}");
    assert!(line.contains("not retried"), "{line}");
    assert!(keeper.plan(&state(last, &[])?, now).actions.is_empty());
    assert!(keeper
        .plan(&state(last, &[])?, now + INTERVAL_MS)
        .actions
        .is_empty());

    let next = keeper.plan(&state(now, &[])?, now + INTERVAL_MS);
    assert_eq!(
        next.actions,
        vec![Action::FundingTick {
            market: MARKET,
            last_tick_ms: now
        }]
    );
    Ok(())
}
