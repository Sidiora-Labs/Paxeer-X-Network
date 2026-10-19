//! Perps keeper of the layerxexchange precompile at 0x1015: the only driver
//! of `fundingTick` and `adl`. Each round reads every configured market at
//! pending state from the sequencer (`getMarket`, `getFunding`, `getOracle`,
//! `getDeficit`, `getPerpsPositions`, and the layerxledger `balanceOf` of the
//! insurance and margin accounts), sends `fundingTick` once the funding
//! interval has elapsed, `liquidate` for every position below maintenance
//! with the caller DID main account as liquidator, and `adl` over at most 16
//! ascending profitable positions while a deficit is recorded. Writes are
//! type-2 transactions signed by the shared EVM sender, whose address is
//! sponsored as a gas station registered sender.

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::io::{Read as _, Write as _};
use std::net::TcpListener;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use layerx_platform_internal::evm_sender::{Eip1559Call, EvmSender};
use serde::Deserialize;
use serde_json::{json, Value};

/// Environment prefix of the shared EVM sender loader: the keeper reads
/// `LAYERX_PERPS_KEEPER_KEY_FILE` or `LAYERX_PERPS_KEEPER_KMS_KEY_ID`.
pub const EVM_SENDER_PREFIX: &str = "LAYERX_PERPS_KEEPER";
/// layerxexchange precompile.
pub const LAYERX_EXCHANGE: [u8; 20] = precompile(0x10, 0x15);
/// layerxledger precompile.
pub const LAYERX_LEDGER: [u8; 20] = precompile(0x10, 0x1a);
/// addr precompile.
pub const ADDR_PRECOMPILE: [u8; 20] = precompile(0x10, 0x04);
/// `getMarket(bytes32)`.
pub const GET_MARKET: [u8; 4] = [0xc3, 0xc9, 0x5c, 0x7b];
/// `getFunding(bytes32)`.
pub const GET_FUNDING: [u8; 4] = [0xf7, 0xf9, 0x43, 0xe0];
/// `getOracle(bytes32)`.
pub const GET_ORACLE: [u8; 4] = [0xda, 0xfa, 0xf9, 0x4a];
/// `getDeficit(bytes32)`.
pub const GET_DEFICIT: [u8; 4] = [0x5f, 0xc3, 0x5f, 0xfc];
/// `getPerpsPositions(bytes32)`.
pub const GET_PERPS_POSITIONS: [u8; 4] = [0xc0, 0x45, 0x17, 0x74];
/// `fundingTick(bytes32)`.
pub const FUNDING_TICK: [u8; 4] = [0x2b, 0x6e, 0xdb, 0x1e];
/// `liquidate(bytes32,bytes32,bytes32)`.
pub const LIQUIDATE: [u8; 4] = [0xd5, 0xb3, 0xe4, 0xd9];
/// `adl(bytes32,bytes32[])`.
pub const ADL: [u8; 4] = [0xc2, 0x93, 0xc1, 0x09];
/// layerxledger `balanceOf(bytes32,bytes32)`.
pub const BALANCE_OF: [u8; 4] = [0xb1, 0xff, 0xc0, 0x3e];
/// addr `getUnifiedAccount(address)`.
pub const GET_UNIFIED_ACCOUNT: [u8; 4] = [0x35, 0x7f, 0xee, 0xd6];
/// `Error(string)`.
pub const ERROR_STRING: [u8; 4] = [0x08, 0xc3, 0x79, 0xa0];
/// The refusal `validate_funding_tick` gives a tick whose interval has not
/// elapsed since the last accepted tick: a duplicate tick.
pub const DUPLICATE_TICK: &str = "LXP_NOT_YET_VALID";
/// The refusal of a view over an absent record.
pub const UNKNOWN_FIELD: &str = "LXP_UNKNOWN_FIELD";
/// `LX_PERPS_DISPATCH_MAX_ADL`.
pub const MAX_ADL_POSITIONS: usize = 16;
/// `LX_PERPS_POSITION_CAPACITY`.
pub const MAX_POSITIONS: usize = 128;
/// Most markets one keeper drives.
pub const MAX_MARKETS: usize = 64;
const MAX_ORACLE_KEYS: usize = 8;
const MARKET_HEAD_WORDS: usize = 24;
const POSITION_WORDS: usize = 10;
const BASIS_POINTS_ONE: u128 = 10_000;
const LOW_HALF: u128 = (1 << 64) - 1;
const RECEIPT_POLL: Duration = Duration::from_millis(50);
const MAX_CODE_BYTES: usize = 64;

const fn precompile(high: u8, low: u8) -> [u8; 20] {
    let mut address = [0; 20];
    address[18] = high;
    address[19] = low;
    address
}

#[must_use]
pub fn hex_encode(bytes: &[u8]) -> String {
    bytes.iter().fold(String::new(), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}

/// Decodes hexadecimal digits without a `0x` prefix.
///
/// # Errors
/// Refuses an odd length and non-hex digits.
pub fn hex_decode(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) || !text.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return Err("malformed hex".to_owned());
    }
    (0..text.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&text[i..i + 2], 16).map_err(|_| "malformed hex".to_owned()))
        .collect()
}

fn hex_prefixed(text: &str) -> Result<Vec<u8>, String> {
    hex_decode(text.strip_prefix("0x").ok_or("hex without 0x prefix")?)
}

/// Parses a `0x`-prefixed 32-byte identifier.
///
/// # Errors
/// Refuses anything other than 64 hex digits after `0x`.
pub fn id32(text: &str) -> Result<[u8; 32], String> {
    hex_prefixed(text)?
        .try_into()
        .map_err(|_| format!("{text} is not a 32-byte id"))
}

/// A canonical ABI decoding failure, naming the field.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AbiError(pub &'static str);

impl std::fmt::Display for AbiError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "non-canonical ABI {}", self.0)
    }
}

fn word(data: &[u8], index: usize) -> Result<&[u8; 32], AbiError> {
    let start = index.checked_mul(32).ok_or(AbiError("offset"))?;
    let end = start.checked_add(32).ok_or(AbiError("offset"))?;
    data.get(start..end)
        .and_then(|bytes| bytes.try_into().ok())
        .ok_or(AbiError("length"))
}

fn uint(word: &[u8; 32], bits: u32, field: &'static str) -> Result<u128, AbiError> {
    if word[..16].iter().any(|byte| *byte != 0) {
        return Err(AbiError(field));
    }
    let mut low = [0; 16];
    low.copy_from_slice(&word[16..]);
    let value = u128::from_be_bytes(low);
    if bits < 128 && value >> bits != 0 {
        return Err(AbiError(field));
    }
    Ok(value)
}

fn u64_word(word: &[u8; 32], field: &'static str) -> Result<u64, AbiError> {
    u64::try_from(uint(word, 64, field)?).map_err(|_| AbiError(field))
}

fn u32_word(word: &[u8; 32], field: &'static str) -> Result<u32, AbiError> {
    u32::try_from(uint(word, 32, field)?).map_err(|_| AbiError(field))
}

fn usize_word(word: &[u8; 32], field: &'static str) -> Result<usize, AbiError> {
    usize::try_from(uint(word, 64, field)?).map_err(|_| AbiError(field))
}

fn bool_word(word: &[u8; 32], field: &'static str) -> Result<bool, AbiError> {
    match uint(word, 8, field)? {
        0 => Ok(false),
        1 => Ok(true),
        _ => Err(AbiError(field)),
    }
}

fn i128_word(word: &[u8; 32], field: &'static str) -> Result<i128, AbiError> {
    let mut low = [0; 16];
    low.copy_from_slice(&word[16..]);
    let value = i128::from_be_bytes(low);
    let extension = if value < 0 { 0xff } else { 0 };
    if word[..16].iter().all(|byte| *byte == extension) {
        Ok(value)
    } else {
        Err(AbiError(field))
    }
}

fn exact_words(data: &[u8], words: usize) -> Result<(), AbiError> {
    if words.checked_mul(32) == Some(data.len()) {
        Ok(())
    } else {
        Err(AbiError("length"))
    }
}

/// The `PerpsMarket` fields the keeper drives a market by.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Market {
    pub id: [u8; 32],
    pub quote_asset: [u8; 32],
    pub insurance_account: [u8; 32],
    pub price_scale: u128,
    pub maintenance_margin_ratio_bps: u32,
    pub funding_interval_ms: u64,
    pub halted: bool,
}

/// Decodes a `getMarket` return: one dynamic `PerpsMarket` tuple in kernel
/// order (seven ids, four u128 sizes, six bps, two ms, two prices, the
/// oracle keys, the parameter version) followed by `halted`.
///
/// # Errors
/// Refuses any non-canonical or out-of-bound field.
pub fn decode_market(data: &[u8]) -> Result<Market, AbiError> {
    if usize_word(word(data, 0)?, "market offset")? != 32 {
        return Err(AbiError("market offset"));
    }
    let tuple = &data[32..];
    let field = |index| word(tuple, index);
    for index in [7, 8, 9, 10, 19, 20] {
        uint(field(index)?, 128, "market amount")?;
    }
    for index in 11..=16 {
        if u128::from(u32_word(field(index)?, "market bps")?) > BASIS_POINTS_ONE {
            return Err(AbiError("market bps"));
        }
    }
    let keys_offset = usize_word(field(21)?, "oracle keys offset")?;
    if keys_offset != MARKET_HEAD_WORDS * 32 {
        return Err(AbiError("oracle keys offset"));
    }
    let keys = usize_word(field(MARKET_HEAD_WORDS)?, "oracle keys")?;
    if keys == 0 || keys > MAX_ORACLE_KEYS {
        return Err(AbiError("oracle keys"));
    }
    exact_words(tuple, MARKET_HEAD_WORDS + 1 + keys)?;
    u32_word(field(22)?, "parameter version")?;
    u64_word(field(18)?, "oracle staleness")?;
    let market = Market {
        id: *field(0)?,
        quote_asset: *field(1)?,
        insurance_account: *field(6)?,
        price_scale: uint(field(10)?, 128, "price scale")?,
        maintenance_margin_ratio_bps: u32_word(field(12)?, "maintenance margin")?,
        funding_interval_ms: u64_word(field(17)?, "funding interval")?,
        halted: bool_word(field(23)?, "halted")?,
    };
    if market.funding_interval_ms == 0 || market.price_scale == 0 {
        return Err(AbiError("market parameters"));
    }
    Ok(market)
}

/// A `getFunding` return.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Funding {
    pub index: i128,
    pub last_tick_ms: u64,
}

/// Decodes `(int256 index, uint64 lastTickMs)`.
///
/// # Errors
/// Refuses a wrong length or out-of-bound field.
pub fn decode_funding(data: &[u8]) -> Result<Funding, AbiError> {
    exact_words(data, 2)?;
    Ok(Funding {
        index: i128_word(word(data, 0)?, "funding index")?,
        last_tick_ms: u64_word(word(data, 1)?, "last tick")?,
    })
}

/// A `getOracle` return.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Oracle {
    pub sequence: u64,
    pub price: u128,
    pub observed_at_ms: u64,
}

/// Decodes `(uint64 sequence, uint256 price, uint64 observedAtMs)`.
///
/// # Errors
/// Refuses a wrong length or out-of-bound field.
pub fn decode_oracle(data: &[u8]) -> Result<Oracle, AbiError> {
    exact_words(data, 3)?;
    Ok(Oracle {
        sequence: u64_word(word(data, 0)?, "oracle sequence")?,
        price: uint(word(data, 1)?, 128, "oracle price")?,
        observed_at_ms: u64_word(word(data, 2)?, "oracle time")?,
    })
}

/// Decodes one amount word: `getDeficit` (uint256 within u128) and
/// layerxledger `balanceOf` (uint128).
///
/// # Errors
/// Refuses a wrong length or a value above u128.
pub fn decode_amount(data: &[u8]) -> Result<u128, AbiError> {
    exact_words(data, 1)?;
    uint(word(data, 0)?, 128, "amount")
}

/// `lx_perps_side`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Side {
    Buy,
    Sell,
}

/// The kernel position record of `getPosition` and `getPerpsPositions`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Position {
    pub id: [u8; 32],
    pub market: [u8; 32],
    pub owner_main_account: [u8; 32],
    pub margin_account: [u8; 32],
    pub asset: [u8; 32],
    pub side: Side,
    pub size: u128,
    pub entry_notional: u128,
    pub funding_index_at_entry: i128,
    pub open: bool,
}

fn decode_position(tuple: &[u8]) -> Result<Position, AbiError> {
    let side = match uint(word(tuple, 5)?, 8, "side")? {
        1 => Side::Buy,
        2 => Side::Sell,
        _ => return Err(AbiError("side")),
    };
    Ok(Position {
        id: *word(tuple, 0)?,
        market: *word(tuple, 1)?,
        owner_main_account: *word(tuple, 2)?,
        margin_account: *word(tuple, 3)?,
        asset: *word(tuple, 4)?,
        side,
        size: uint(word(tuple, 6)?, 128, "size")?,
        entry_notional: uint(word(tuple, 7)?, 128, "entry notional")?,
        funding_index_at_entry: i128_word(word(tuple, 8)?, "entry funding index")?,
        open: bool_word(word(tuple, 9)?, "open")?,
    })
}

/// Decodes a `getPerpsPositions` return: `Position[]` of at most 128
/// static tuples in strictly ascending position id order.
///
/// # Errors
/// Refuses a non-canonical array, an oversized list or unordered ids.
pub fn decode_positions(data: &[u8]) -> Result<Vec<Position>, AbiError> {
    if usize_word(word(data, 0)?, "positions offset")? != 32 {
        return Err(AbiError("positions offset"));
    }
    let count = usize_word(word(data, 1)?, "positions length")?;
    if count > MAX_POSITIONS {
        return Err(AbiError("positions length"));
    }
    exact_words(data, 2 + count * POSITION_WORDS)?;
    let positions = data[64..]
        .chunks_exact(POSITION_WORDS * 32)
        .map(decode_position)
        .collect::<Result<Vec<_>, _>>()?;
    if positions.windows(2).any(|pair| pair[0].id >= pair[1].id) {
        return Err(AbiError("positions order"));
    }
    Ok(positions)
}

/// Reads the `layerxMainAccountId` of a `getUnifiedAccount` return
/// `(address, string, bytes32, bytes32)`.
///
/// # Errors
/// Refuses a non-canonical head.
pub fn decode_main_account(data: &[u8]) -> Result<[u8; 32], AbiError> {
    if word(data, 0)?[..12].iter().any(|byte| *byte != 0) {
        return Err(AbiError("unified account address"));
    }
    if usize_word(word(data, 1)?, "unified account string")? != 128 {
        return Err(AbiError("unified account string"));
    }
    Ok(*word(data, 3)?)
}

/// Reads an `Error(string)` revert payload.
#[must_use]
pub fn decode_error_string(data: &[u8]) -> Option<String> {
    let body = data.strip_prefix(&ERROR_STRING)?;
    if usize_word(word(body, 0).ok()?, "error").ok()? != 32 {
        return None;
    }
    let length = usize_word(word(body, 1).ok()?, "error").ok()?;
    let text = body.get(64..64usize.checked_add(length)?)?;
    String::from_utf8(text.to_vec()).ok()
}

fn lxp_code(text: &str) -> Option<String> {
    let code = text.trim();
    (code.starts_with("LXP_")
        && code.len() <= MAX_CODE_BYTES
        && code
            .bytes()
            .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit() || byte == b'_'))
    .then(|| code.to_owned())
}

/// The `LXP_<code>` of a JSON-RPC error object: from its `Error(string)`
/// revert data, else from an `execution reverted: LXP_<code>` message.
#[must_use]
pub fn refusal_code(error: &Value) -> Option<String> {
    let from_data = error
        .get("data")
        .and_then(Value::as_str)
        .and_then(|data| hex_prefixed(data).ok())
        .and_then(|data| decode_error_string(&data))
        .and_then(|text| lxp_code(&text));
    from_data.or_else(|| {
        error
            .get("message")
            .and_then(Value::as_str)
            .and_then(|message| message.strip_prefix("execution reverted: "))
            .and_then(lxp_code)
    })
}

fn calldata(selector: [u8; 4], words: &[[u8; 32]]) -> Vec<u8> {
    let mut data = Vec::with_capacity(4 + 32 * words.len());
    data.extend_from_slice(&selector);
    for item in words {
        data.extend_from_slice(item);
    }
    data
}

fn length_word(length: usize) -> [u8; 32] {
    let mut out = [0; 32];
    out[24..].copy_from_slice(&(length as u64).to_be_bytes());
    out
}

/// Calldata of a one-market 0x1015 view: `getMarket`, `getFunding`,
/// `getOracle`, `getDeficit` or `getPerpsPositions`.
#[must_use]
pub fn market_view(selector: [u8; 4], market: &[u8; 32]) -> Vec<u8> {
    calldata(selector, &[*market])
}

/// layerxledger `balanceOf(accountId, assetId)` calldata.
#[must_use]
pub fn balance_of(account: &[u8; 32], asset: &[u8; 32]) -> Vec<u8> {
    calldata(BALANCE_OF, &[*account, *asset])
}

/// addr `getUnifiedAccount(address)` calldata.
#[must_use]
pub fn unified_account(address: &[u8; 20]) -> Vec<u8> {
    let mut padded = [0; 32];
    padded[12..].copy_from_slice(address);
    calldata(GET_UNIFIED_ACCOUNT, &[padded])
}

/// One keeper write to 0x1015.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Action {
    /// `fundingTick(market)` for the interval that started at `last_tick_ms`.
    FundingTick { market: [u8; 32], last_tick_ms: u64 },
    /// `liquidate(market, position, liquidator)`.
    Liquidate {
        market: [u8; 32],
        position: [u8; 32],
        liquidator: [u8; 32],
    },
    /// `adl(market, positions)` with 1 to 16 strictly ascending ids.
    Adl {
        market: [u8; 32],
        positions: Vec<[u8; 32]>,
    },
}

impl Action {
    /// The transaction input of this write.
    #[must_use]
    pub fn calldata(&self) -> Vec<u8> {
        match self {
            Self::FundingTick { market, .. } => calldata(FUNDING_TICK, &[*market]),
            Self::Liquidate {
                market,
                position,
                liquidator,
            } => calldata(LIQUIDATE, &[*market, *position, *liquidator]),
            Self::Adl { market, positions } => {
                let mut words = vec![*market, length_word(64), length_word(positions.len())];
                words.extend_from_slice(positions);
                calldata(ADL, &words)
            }
        }
    }

    fn describe(&self) -> String {
        match self {
            Self::FundingTick {
                market,
                last_tick_ms,
            } => format!(
                "fundingTick market 0x{} interval from {last_tick_ms}",
                hex_encode(market)
            ),
            Self::Liquidate {
                market, position, ..
            } => format!(
                "liquidate market 0x{} position 0x{}",
                hex_encode(market),
                hex_encode(position)
            ),
            Self::Adl { market, positions } => format!(
                "adl market 0x{} over {} positions",
                hex_encode(market),
                positions.len()
            ),
        }
    }
}

/// The type-2 transaction carrying `action` to 0x1015.
#[must_use]
pub fn transaction(
    chain_id: u64,
    nonce: u64,
    gas_limit: u64,
    max_priority_fee_per_gas: u128,
    max_fee_per_gas: u128,
    action: &Action,
) -> Eip1559Call {
    Eip1559Call {
        chain_id,
        nonce,
        max_priority_fee_per_gas,
        max_fee_per_gas,
        gas_limit,
        to: LAYERX_EXCHANGE,
        data: action.calldata(),
    }
}

/// Kernel u128 arithmetic failures, as `LXP_ERR_NON_CANONICAL` and
/// `LXP_ERR_OVERFLOW`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MathError {
    NonCanonical,
    Overflow,
}

fn mul_wide(left: u128, right: u128) -> (u128, u128) {
    let (left_high, left_low) = (left >> 64, left & LOW_HALF);
    let (right_high, right_low) = (right >> 64, right & LOW_HALF);
    let low_product = left_low * right_low;
    let first_cross = left_high * right_low;
    let second_cross = left_low * right_high;
    let high_product = left_high * right_high;
    let middle = (low_product >> 64) + (first_cross & LOW_HALF) + (second_cross & LOW_HALF);
    let low = (middle << 64) | (low_product & LOW_HALF);
    let high = high_product + (first_cross >> 64) + (second_cross >> 64) + (middle >> 64);
    (high, low)
}

/// `floor(left * right / divisor)` and its remainder over a 256-bit
/// product, refused when the quotient leaves u128
/// (`lxp_u128_mul_div_floor`).
///
/// # Errors
/// `NonCanonical` for a zero divisor, `Overflow` for a quotient above u128.
pub fn mul_div_floor(left: u128, right: u128, divisor: u128) -> Result<(u128, u128), MathError> {
    if divisor == 0 {
        return Err(MathError::NonCanonical);
    }
    let (high, low) = mul_wide(left, right);
    if high >= divisor {
        return Err(MathError::Overflow);
    }
    let mut remainder = high;
    let mut quotient = 0_u128;
    for bit in (0..128).rev() {
        let carry = remainder >> 127;
        remainder = (remainder << 1) | ((low >> bit) & 1);
        quotient <<= 1;
        if carry == 1 || remainder >= divisor {
            remainder = remainder.wrapping_sub(divisor);
            quotient |= 1;
        }
    }
    Ok((quotient, remainder))
}

/// `ceil(value * bps / 10000)` (`lxp_u128_mul_bps_ceil`).
///
/// # Errors
/// `Overflow` when the result leaves u128.
pub fn mul_bps_ceil(value: u128, bps: u32) -> Result<u128, MathError> {
    let (quotient, remainder) = mul_div_floor(value, u128::from(bps), BASIS_POINTS_ONE)?;
    if remainder == 0 {
        Ok(quotient)
    } else {
        quotient.checked_add(1).ok_or(MathError::Overflow)
    }
}

/// `floor(mark * size / price_scale)` (`position_notional`).
///
/// # Errors
/// `NonCanonical` for a zero mark, scale or size; `Overflow` above u128.
pub fn position_notional(
    position: &Position,
    mark_price: u128,
    price_scale: u128,
) -> Result<u128, MathError> {
    if mark_price == 0 || price_scale == 0 || position.size == 0 {
        return Err(MathError::NonCanonical);
    }
    Ok(mul_div_floor(mark_price, position.size, price_scale)?.0)
}

/// Whether the margin balance is below the maintenance requirement
/// `ceil(notional * maintenance_bps / 10000)` (`lx_perps_maintenance_check`).
///
/// # Errors
/// `NonCanonical` for a maintenance ratio outside 1..=10000 or a zero
/// mark, scale or size; `Overflow` above u128.
pub fn below_maintenance(
    market: &Market,
    position: &Position,
    mark_price: u128,
    margin_balance: u128,
) -> Result<bool, MathError> {
    if market.maintenance_margin_ratio_bps == 0
        || u128::from(market.maintenance_margin_ratio_bps) > BASIS_POINTS_ONE
    {
        return Err(MathError::NonCanonical);
    }
    let notional = position_notional(position, mark_price, market.price_scale)?;
    let required = mul_bps_ceil(notional, market.maintenance_margin_ratio_bps)?;
    Ok(margin_balance < required)
}

/// The average entry price, rounded against the holder (`entry_price_of`).
///
/// # Errors
/// `NonCanonical` for a zero size; `Overflow` above u128.
pub fn entry_price(market: &Market, position: &Position) -> Result<u128, MathError> {
    if position.size == 0 {
        return Err(MathError::NonCanonical);
    }
    let (quotient, remainder) =
        mul_div_floor(position.entry_notional, market.price_scale, position.size)?;
    if position.side == Side::Buy && remainder != 0 {
        quotient.checked_add(1).ok_or(MathError::Overflow)
    } else {
        Ok(quotient)
    }
}

/// The profit of `position` at `mark_price`, `None` for a loss or zero
/// (`lx_perps_pnl_compute`, a loss rounded up).
///
/// # Errors
/// `NonCanonical` for a zero size or scale; `Overflow` above u128.
pub fn profit(
    market: &Market,
    position: &Position,
    mark_price: u128,
) -> Result<Option<u128>, MathError> {
    let entry = entry_price(market, position)?;
    let mark_below = mark_price < entry;
    let difference = mark_price.abs_diff(entry);
    let (quotient, _) = mul_div_floor(difference, position.size, market.price_scale)?;
    let loss = difference != 0 && (position.side == Side::Buy) == mark_below;
    Ok((!loss && quotient != 0).then_some(quotient))
}

/// The `adl` ids: the open positions in profit at `mark_price`, the 16 with
/// the largest profit (ties by lower id), in strictly ascending id order.
#[must_use]
pub fn adl_positions(market: &Market, positions: &[Position], mark_price: u128) -> Vec<[u8; 32]> {
    let mut ranked: Vec<(u128, [u8; 32])> = positions
        .iter()
        .filter(|position| position.open)
        .filter_map(|position| {
            profit(market, position, mark_price)
                .ok()
                .flatten()
                .map(|amount| (amount, position.id))
        })
        .collect();
    ranked.sort_by(|left, right| right.0.cmp(&left.0).then(left.1.cmp(&right.1)));
    let mut ids: Vec<[u8; 32]> = ranked
        .into_iter()
        .take(MAX_ADL_POSITIONS)
        .map(|(_, id)| id)
        .collect();
    ids.sort_unstable();
    ids
}

/// Whether a whole funding interval has elapsed since the last tick, the
/// condition `lx_perps_tick_execute` accepts a tick under.
#[must_use]
pub fn funding_tick_due(market: &Market, funding: &Funding, now_ms: u64) -> bool {
    !market.halted
        && market.funding_interval_ms != 0
        && now_ms
            .checked_sub(funding.last_tick_ms)
            .is_some_and(|elapsed| elapsed >= market.funding_interval_ms)
}

/// One open position and the balance of its margin account.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PositionState {
    pub position: Position,
    pub margin_balance: u128,
}

/// Everything one round reads of a market at pending state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MarketState {
    pub market: Market,
    pub funding: Funding,
    /// `None` while no observation is recorded.
    pub oracle: Option<Oracle>,
    pub deficit: u128,
    pub insurance_balance: u128,
    pub positions: Vec<PositionState>,
}

/// The monitoring signals of one market.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct MarketMetrics {
    pub last_funding_tick_ms: u64,
    pub below_maintenance_not_liquidated: u64,
    pub insurance_fund_balance: u128,
}

/// Where a sent write stands.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Outcome {
    /// `pax_sequencerReceipt` answered receipted with status 1.
    Accepted(String),
    /// Sent, not receipted before the deadline.
    Pending(String),
    /// The chain refused it with this `LXP_<code>`.
    Refused(String),
    /// Not sent: transport, signing or malformed answer.
    Failed(String),
}

/// The writes one round sends for a market and what it logs.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Plan {
    pub actions: Vec<Action>,
    pub notes: Vec<String>,
}

type MetricValue = fn(&MarketMetrics) -> u128;

/// Keeper memory across rounds: the funding interval already ticked or
/// refused as a duplicate per market, and the metrics per market id.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Keeper {
    liquidator: [u8; 32],
    spent_ticks: BTreeMap<[u8; 32], u64>,
    metrics: BTreeMap<[u8; 32], MarketMetrics>,
}

impl Keeper {
    /// `liquidator` is the caller DID main account id.
    #[must_use]
    pub const fn new(liquidator: [u8; 32]) -> Self {
        Self {
            liquidator,
            spent_ticks: BTreeMap::new(),
            metrics: BTreeMap::new(),
        }
    }

    #[must_use]
    pub fn metrics_of(&self, market: &[u8; 32]) -> Option<MarketMetrics> {
        self.metrics.get(market).copied()
    }

    /// Plans the round for `state` at `now_ms` and refreshes its metrics.
    pub fn plan(&mut self, state: &MarketState, now_ms: u64) -> Plan {
        let market = &state.market;
        let mut plan = Plan::default();
        if funding_tick_due(market, &state.funding, now_ms)
            && self.spent_ticks.get(&market.id) != Some(&state.funding.last_tick_ms)
        {
            plan.actions.push(Action::FundingTick {
                market: market.id,
                last_tick_ms: state.funding.last_tick_ms,
            });
        }
        let mut below = 0_u64;
        if let Some(oracle) = state.oracle {
            for entry in state.positions.iter().filter(|entry| entry.position.open) {
                match below_maintenance(market, &entry.position, oracle.price, entry.margin_balance)
                {
                    Ok(true) => {
                        below = below.saturating_add(1);
                        if !market.halted {
                            plan.actions.push(Action::Liquidate {
                                market: market.id,
                                position: entry.position.id,
                                liquidator: self.liquidator,
                            });
                        }
                    }
                    Ok(false) => {}
                    Err(error) => plan.notes.push(format!(
                        "maintenance check of position 0x{}: {error:?}",
                        hex_encode(&entry.position.id)
                    )),
                }
            }
            if state.deficit != 0 {
                let positions: Vec<Position> = state
                    .positions
                    .iter()
                    .map(|entry| entry.position.clone())
                    .collect();
                let ids = adl_positions(market, &positions, oracle.price);
                if ids.is_empty() {
                    plan.notes.push(format!(
                        "market 0x{} deficit {} has no position in profit",
                        hex_encode(&market.id),
                        state.deficit
                    ));
                } else {
                    plan.actions.push(Action::Adl {
                        market: market.id,
                        positions: ids,
                    });
                }
            }
        } else {
            plan.notes.push(format!(
                "market 0x{} has no oracle observation",
                hex_encode(&market.id)
            ));
        }
        self.metrics.insert(
            market.id,
            MarketMetrics {
                last_funding_tick_ms: state.funding.last_tick_ms,
                below_maintenance_not_liquidated: below,
                insurance_fund_balance: state.insurance_balance,
            },
        );
        plan
    }

    /// Records the outcome of a planned write and returns its log line. A
    /// sent tick spends its interval; a tick refused as a duplicate spends it
    /// too and is not retried for that interval.
    pub fn record(&mut self, action: &Action, outcome: &Outcome) -> String {
        if let Action::FundingTick {
            market,
            last_tick_ms,
        } = action
        {
            let spent = match outcome {
                Outcome::Accepted(_) | Outcome::Pending(_) => true,
                Outcome::Refused(code) => code == DUPLICATE_TICK,
                Outcome::Failed(_) => false,
            };
            if spent {
                self.spent_ticks.insert(*market, *last_tick_ms);
            }
        }
        if let (Action::Liquidate { market, .. }, Outcome::Accepted(_)) = (action, outcome) {
            if let Some(metrics) = self.metrics.get_mut(market) {
                metrics.below_maintenance_not_liquidated =
                    metrics.below_maintenance_not_liquidated.saturating_sub(1);
            }
        }
        let what = action.describe();
        match outcome {
            Outcome::Accepted(hash) => format!("{what} accepted {hash}"),
            Outcome::Pending(hash) => format!("{what} sent {hash}, not receipted yet"),
            Outcome::Refused(code) if code == DUPLICATE_TICK => {
                format!("{what} refused {code}: duplicate tick, not retried for this interval")
            }
            Outcome::Refused(code) => format!("{what} refused {code}"),
            Outcome::Failed(error) => format!("{what} not sent: {error}"),
        }
    }

    /// The metrics in the Prometheus text format, keyed by the market id
    /// `getMarket` returned.
    #[must_use]
    pub fn render_metrics(&self) -> String {
        let mut out = String::new();
        let series: [(&str, &str, MetricValue); 3] = [
            (
                "layerx_perps_keeper_last_funding_tick_ms",
                "Last accepted funding tick per market, unix milliseconds.",
                |metrics| u128::from(metrics.last_funding_tick_ms),
            ),
            (
                "layerx_perps_keeper_positions_below_maintenance_not_liquidated",
                "Open positions below maintenance not liquidated in the last round.",
                |metrics| u128::from(metrics.below_maintenance_not_liquidated),
            ),
            (
                "layerx_perps_keeper_insurance_fund_balance",
                "Insurance account balance of the market quote asset.",
                |metrics| metrics.insurance_fund_balance,
            ),
        ];
        for (name, help, value) in series {
            let _ = writeln!(out, "# HELP {name} {help}\n# TYPE {name} gauge");
            for (market, metrics) in &self.metrics {
                let _ = writeln!(
                    out,
                    "{name}{{market=\"0x{}\"}} {}",
                    hex_encode(market),
                    value(metrics)
                );
            }
        }
        out
    }
}

/// The answer of the health and metrics listener: `GET /healthz` 200,
/// `GET /metrics` 200 with the metrics, anything else 404.
#[must_use]
pub fn http_response(request: &[u8], metrics: &str) -> String {
    let (status, content_type, body) = if request.starts_with(b"GET /healthz ") {
        ("200 OK", "text/plain", "ok\n")
    } else if request.starts_with(b"GET /metrics ") {
        ("200 OK", "text/plain; version=0.0.4", metrics)
    } else {
        ("404 Not Found", "text/plain", "")
    };
    format!(
        "HTTP/1.1 {status}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    )
}

/// Keeper configuration file.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Config {
    pub chain_id: u64,
    pub round_ms: u64,
    pub receipt_wait_ms: u64,
    pub markets: Vec<[u8; 32]>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RawConfig {
    chain_id: u64,
    round_ms: u64,
    receipt_wait_ms: u64,
    markets: Vec<String>,
}

impl Config {
    /// # Errors
    /// Refuses unknown fields, zero values, a receipt wait above the round,
    /// no markets, more than 64, malformed or repeated market ids.
    pub fn parse(text: &str) -> Result<Self, String> {
        let raw: RawConfig = serde_json::from_str(text).map_err(|e| format!("config: {e}"))?;
        if raw.chain_id == 0
            || raw.round_ms == 0
            || raw.receipt_wait_ms == 0
            || raw.receipt_wait_ms > raw.round_ms
        {
            return Err("config: chain_id, round_ms and receipt_wait_ms must be positive, receipt_wait_ms at most round_ms".to_owned());
        }
        if raw.markets.is_empty() || raw.markets.len() > MAX_MARKETS {
            return Err(format!("config: 1 to {MAX_MARKETS} markets"));
        }
        let markets = raw
            .markets
            .iter()
            .map(|market| id32(market))
            .collect::<Result<Vec<_>, _>>()?;
        let mut seen = std::collections::BTreeSet::new();
        if !markets.iter().all(|market| seen.insert(*market)) {
            return Err("config: repeated market".to_owned());
        }
        Ok(Self {
            chain_id: raw.chain_id,
            round_ms: raw.round_ms,
            receipt_wait_ms: raw.receipt_wait_ms,
            markets,
        })
    }

    /// # Errors
    /// Refuses an unreadable or invalid file.
    pub fn load(path: &Path) -> Result<Self, String> {
        Self::parse(
            &std::fs::read_to_string(path)
                .map_err(|e| format!("config {}: {e}", path.display()))?,
        )
    }
}

/// A JSON-RPC failure.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RpcError {
    Transport(String),
    /// A JSON-RPC error object, with its `LXP_<code>` when it carries one.
    Refused {
        code: Option<String>,
        detail: String,
    },
    Malformed(String),
}

impl std::fmt::Display for RpcError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Transport(detail) | Self::Malformed(detail) | Self::Refused { detail, .. } => {
                formatter.write_str(detail)
            }
        }
    }
}

impl From<AbiError> for RpcError {
    fn from(error: AbiError) -> Self {
        Self::Malformed(error.to_string())
    }
}

/// Reads a `pax_sequencerReceipt` answer: `None` while unknown or only
/// submitted, then accepted for status 1 or refused with its revert code.
#[must_use]
pub fn receipt_outcome(answer: &Value, hash: &str) -> Option<Outcome> {
    if answer.is_null() {
        return None;
    }
    match answer.get("rung").and_then(Value::as_str) {
        Some("submitted") => None,
        Some("receipted" | "committed") => match answer.get("status").and_then(Value::as_str) {
            Some("0x1") => Some(Outcome::Accepted(hash.to_owned())),
            Some("0x0") => Some(Outcome::Refused(
                answer
                    .get("returnData")
                    .and_then(Value::as_str)
                    .and_then(|data| hex_prefixed(data).ok())
                    .and_then(|data| decode_error_string(&data))
                    .and_then(|text| lxp_code(&text))
                    .unwrap_or_else(|| "reverted".to_owned()),
            )),
            _ => Some(Outcome::Failed(format!("{hash}: receipt without a status"))),
        },
        _ => Some(Outcome::Failed(format!(
            "{hash}: receipt with an unknown rung"
        ))),
    }
}

fn quantity(value: &Value) -> Result<u128, RpcError> {
    value
        .as_str()
        .and_then(|text| text.strip_prefix("0x"))
        .and_then(|text| u128::from_str_radix(text, 16).ok())
        .ok_or_else(|| RpcError::Malformed(format!("malformed quantity {value}")))
}

fn quantity_u64(value: &Value) -> Result<u64, RpcError> {
    u64::try_from(quantity(value)?)
        .map_err(|_| RpcError::Malformed(format!("quantity {value} above u64")))
}

/// JSON-RPC client of the sequencer endpoint.
pub struct Rpc {
    agent: ureq::Agent,
    url: String,
}

impl Rpc {
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
    /// Transport failures, malformed answers and JSON-RPC error objects.
    pub fn call(&self, method: &str, params: &Value) -> Result<Value, RpcError> {
        let request = json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params});
        let body = self
            .agent
            .post(&self.url)
            .send_json(&request)
            .map_err(|e| RpcError::Transport(format!("{method}: {e}")))?
            .body_mut()
            .read_to_string()
            .map_err(|e| RpcError::Transport(format!("{method}: {e}")))?;
        let mut answer: Value = serde_json::from_str(&body)
            .map_err(|e| RpcError::Malformed(format!("{method}: {e}")))?;
        if let Some(error) = answer.get("error") {
            return Err(RpcError::Refused {
                code: refusal_code(error),
                detail: format!("{method}: {error}"),
            });
        }
        answer
            .get_mut("result")
            .map(Value::take)
            .ok_or_else(|| RpcError::Malformed(format!("{method}: no result")))
    }

    /// `eth_call` of a view at pending state.
    ///
    /// # Errors
    /// As [`Rpc::call`], plus a result that is not hex.
    pub fn view(&self, to: &[u8; 20], data: &[u8]) -> Result<Vec<u8>, RpcError> {
        let answer = self.call(
            "eth_call",
            &json!([{"to": format!("0x{}", hex_encode(to)), "data": format!("0x{}", hex_encode(data))}, "pending"]),
        )?;
        answer
            .as_str()
            .ok_or_else(|| RpcError::Malformed("eth_call: result is not a string".to_owned()))
            .and_then(|text| hex_prefixed(text).map_err(RpcError::Malformed))
    }

    fn view_or_absent(&self, data: &[u8]) -> Result<Option<Vec<u8>>, RpcError> {
        match self.view(&LAYERX_EXCHANGE, data) {
            Ok(answer) => Ok(Some(answer)),
            Err(RpcError::Refused {
                code: Some(code), ..
            }) if code == UNKNOWN_FIELD => Ok(None),
            Err(error) => Err(error),
        }
    }

    /// Reads one market at pending state.
    ///
    /// # Errors
    /// Any failed or non-canonical read; an absent oracle or deficit is not
    /// a failure.
    pub fn read_market(&self, id: &[u8; 32]) -> Result<MarketState, RpcError> {
        let market = decode_market(&self.view(&LAYERX_EXCHANGE, &market_view(GET_MARKET, id))?)?;
        if market.id != *id {
            return Err(RpcError::Malformed(
                "getMarket answered another market".to_owned(),
            ));
        }
        let funding = decode_funding(&self.view(&LAYERX_EXCHANGE, &market_view(GET_FUNDING, id))?)?;
        let oracle = self
            .view_or_absent(&market_view(GET_ORACLE, id))?
            .map(|data| decode_oracle(&data))
            .transpose()?;
        let deficit = self
            .view_or_absent(&market_view(GET_DEFICIT, id))?
            .map_or(Ok(0), |data| decode_amount(&data))?;
        let insurance_balance = decode_amount(&self.view(
            &LAYERX_LEDGER,
            &balance_of(&market.insurance_account, &market.quote_asset),
        )?)?;
        let mut positions = Vec::new();
        for position in
            decode_positions(&self.view(&LAYERX_EXCHANGE, &market_view(GET_PERPS_POSITIONS, id))?)?
        {
            if position.market != *id {
                return Err(RpcError::Malformed(
                    "getPerpsPositions answered another market".to_owned(),
                ));
            }
            if !position.open {
                continue;
            }
            let margin_balance = decode_amount(&self.view(
                &LAYERX_LEDGER,
                &balance_of(&position.margin_account, &position.asset),
            )?)?;
            positions.push(PositionState {
                position,
                margin_balance,
            });
        }
        Ok(MarketState {
            market,
            funding,
            oracle,
            deficit,
            insurance_balance,
            positions,
        })
    }

    /// The DID main account id addr 0x1004 binds `address` to.
    ///
    /// # Errors
    /// Refuses a failed read and an unbound address.
    pub fn main_account(&self, address: &[u8; 20]) -> Result<[u8; 32], String> {
        let data = self
            .view(&ADDR_PRECOMPILE, &unified_account(address))
            .map_err(|e| e.to_string())?;
        let account = decode_main_account(&data).map_err(|e| e.to_string())?;
        if account == [0; 32] {
            return Err(format!(
                "EVM sender 0x{} is not bound to a DID",
                hex_encode(address)
            ));
        }
        Ok(account)
    }

    /// `eth_chainId`.
    ///
    /// # Errors
    /// Refuses a failed or malformed answer.
    pub fn chain_id(&self) -> Result<u64, String> {
        quantity_u64(
            &self
                .call("eth_chainId", &json!([]))
                .map_err(|e| e.to_string())?,
        )
        .map_err(|e| e.to_string())
    }

    fn signed(
        &self,
        sender: &EvmSender,
        chain_id: u64,
        action: &Action,
    ) -> Result<Vec<u8>, RpcError> {
        let from = format!("0x{}", hex_encode(&sender.address()));
        let data = format!("0x{}", hex_encode(&action.calldata()));
        let nonce =
            quantity_u64(&self.call("eth_getTransactionCount", &json!([from, "pending"]))?)?;
        let gas_limit = quantity_u64(&self.call(
            "eth_estimateGas",
            &json!([{"from": from, "to": format!("0x{}", hex_encode(&LAYERX_EXCHANGE)), "data": data}, "pending"]),
        )?)?;
        let priority = quantity(&self.call("eth_maxPriorityFeePerGas", &json!([]))?)?;
        let gas_price = quantity(&self.call("eth_gasPrice", &json!([]))?)?;
        let call = transaction(
            chain_id,
            nonce,
            gas_limit,
            priority,
            gas_price.max(priority),
            action,
        );
        sender
            .sign_transaction(&call)
            .map_err(|e| RpcError::Malformed(e.to_string()))
    }

    /// Signs `action` with `sender`, sends it and waits for
    /// `pax_sequencerReceipt` until `deadline`.
    #[must_use]
    pub fn submit(
        &self,
        sender: &EvmSender,
        chain_id: u64,
        action: &Action,
        deadline: Instant,
    ) -> Outcome {
        let sent = self.signed(sender, chain_id, action).and_then(|raw| {
            self.call(
                "eth_sendRawTransaction",
                &json!([format!("0x{}", hex_encode(&raw))]),
            )
        });
        let hash = match sent {
            Ok(Value::String(hash)) => hash,
            Ok(other) => {
                return Outcome::Failed(format!("eth_sendRawTransaction answered {other}"))
            }
            Err(RpcError::Refused {
                code: Some(code), ..
            }) => return Outcome::Refused(code),
            Err(error) => return Outcome::Failed(error.to_string()),
        };
        loop {
            match self.call("pax_sequencerReceipt", &json!([hash])) {
                Ok(answer) => {
                    if let Some(outcome) = receipt_outcome(&answer, &hash) {
                        return outcome;
                    }
                }
                Err(error) => return Outcome::Failed(format!("{hash}: {error}")),
            }
            if Instant::now() >= deadline {
                return Outcome::Pending(hash);
            }
            thread::sleep(RECEIPT_POLL);
        }
    }
}

/// HTTP agent trusting the platform roots.
///
/// # Errors
/// Refuses a host without root certificates.
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

fn env(name: &str) -> Result<String, String> {
    std::env::var(name).map_err(|_| format!("{name} is required"))
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| u64::try_from(d.as_millis()).unwrap_or(u64::MAX))
}

fn serve_health(address: &str, metrics: Arc<Mutex<String>>) -> Result<(), String> {
    let listener = TcpListener::bind(address).map_err(|e| format!("health bind {address}: {e}"))?;
    thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            let mut request = [0_u8; 1024];
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let read = stream.read(&mut request).unwrap_or(0);
            let body = metrics.lock().map_or_else(|_| String::new(), |m| m.clone());
            let _ = stream.write_all(http_response(&request[..read], &body).as_bytes());
        }
    });
    Ok(())
}

/// Reads, plans and sends one round over every configured market.
pub fn run_round(config: &Config, rpc: &Rpc, sender: &EvmSender, keeper: &mut Keeper, now_ms: u64) {
    for id in &config.markets {
        let state = match rpc.read_market(id) {
            Ok(state) => state,
            Err(error) => {
                eprintln!("perps-keeper: market 0x{}: {error}", hex_encode(id));
                continue;
            }
        };
        let plan = keeper.plan(&state, now_ms);
        for note in &plan.notes {
            eprintln!("perps-keeper: {note}");
        }
        for action in &plan.actions {
            let deadline = Instant::now() + Duration::from_millis(config.receipt_wait_ms);
            let outcome = rpc.submit(sender, config.chain_id, action, deadline);
            eprintln!("perps-keeper: {}", keeper.record(action, &outcome));
        }
    }
}

fn run() -> Result<(), String> {
    let config = Config::load(Path::new(&env("LAYERX_PERPS_KEEPER_CONFIG")?))?;
    let rpc = Rpc::new(http_agent()?, env("LAYERX_PERPS_KEEPER_SEQUENCER_URL")?)?;
    let chain_id = rpc.chain_id()?;
    if chain_id != config.chain_id {
        return Err(format!(
            "sequencer chain id {chain_id} is not the configured {}",
            config.chain_id
        ));
    }
    let sender = EvmSender::from_env(EVM_SENDER_PREFIX).map_err(|e| format!("EVM sender: {e}"))?;
    let liquidator = rpc.main_account(&sender.address())?;
    eprintln!(
        "perps-keeper: sender 0x{} (a gas station registered sender), liquidator account 0x{}",
        hex_encode(&sender.address()),
        hex_encode(&liquidator)
    );
    let health = std::env::var("LAYERX_HEALTH_ADDR").unwrap_or_else(|_| "[::]:8080".to_owned());
    let metrics = Arc::new(Mutex::new(String::new()));
    serve_health(&health, Arc::clone(&metrics))?;
    let mut keeper = Keeper::new(liquidator);
    let round = Duration::from_millis(config.round_ms);
    loop {
        let started = Instant::now();
        run_round(&config, &rpc, &sender, &mut keeper, now_ms());
        if let Ok(mut shared) = metrics.lock() {
            *shared = keeper.render_metrics();
        }
        thread::sleep(round.saturating_sub(started.elapsed()));
    }
}

pub fn main() {
    if let Err(error) = run() {
        eprintln!("perps-keeper: {error}");
        std::process::exit(1);
    }
}
