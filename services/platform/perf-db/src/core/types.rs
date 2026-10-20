use bytemuck::{Pod, Zeroable};
use serde::{Deserialize, Serialize};

/// Raw price tick from feed. 24 bytes, mmap-appendable.
#[derive(Debug, Clone, Copy, PartialEq, Pod, Zeroable)]
#[repr(C)]
pub struct PriceTick {
    pub timestamp_ns: u64,
    pub market_id: u16,
    pub _pad: [u8; 6],
    pub price: f64,
}

const _: () = assert!(size_of::<PriceTick>() == 24);

/// Pre-computed OHLCV candle. 48 bytes, mmap-appendable. Volume is stored as
/// `f32` (rather than `f64`) to keep the record at 48 bytes.
#[derive(Debug, Clone, Copy, PartialEq, Pod, Zeroable)]
#[repr(C)]
pub struct CandleRecord {
    pub timestamp_ns: u64,
    pub open: f64,
    pub high: f64,
    pub low: f64,
    pub close: f64,
    pub volume: f32,
    pub market_id: u16,
    pub timeframe: u8,
    pub _pad: u8,
}

const _: () = assert!(size_of::<CandleRecord>() == 48);

/// 18-decimal fixed-point integer (`1_000_000_000_000_000_000` == 1.0 USD).
/// Range of ±1.7×10^38 covers up to ±1.7×10^20 whole units, which is
/// sufficient for DeFi-scale USD amounts.
pub type FixedI128 = i128;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[repr(u8)]
pub enum PositionStatus {
    Open = 0,
    Closed = 1,
    Liquidated = 2,
}

impl From<u8> for PositionStatus {
    fn from(v: u8) -> Self {
        match v {
            0 => Self::Open,
            1 => Self::Closed,
            2 => Self::Liquidated,
            _ => Self::Open,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Position {
    pub position_id: [u8; 32],
    pub user_address: [u8; 20],
    pub market_id: u16,
    pub is_long: bool,
    pub size_usd: FixedI128,
    pub collateral_usd: FixedI128,
    pub collateral_token: [u8; 20],
    pub collateral_amount: FixedI128,
    pub entry_price: FixedI128,
    pub exit_price: Option<FixedI128>,
    pub realized_pnl: Option<FixedI128>,
    pub leverage: FixedI128,
    pub status: PositionStatus,
    pub open_tx: [u8; 32],
    pub close_tx: Option<[u8; 32]>,
    pub open_block: u64,
    pub close_block: Option<u64>,
    pub opened_at: u64,
    pub closed_at: Option<u64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[repr(u8)]
pub enum OrderType {
    Limit = 0,
    StopLimit = 1,
    TakeProfit = 2,
    StopLoss = 3,
}

impl From<u8> for OrderType {
    fn from(v: u8) -> Self {
        match v {
            0 => Self::Limit,
            1 => Self::StopLimit,
            2 => Self::TakeProfit,
            3 => Self::StopLoss,
            _ => Self::Limit,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[repr(u8)]
pub enum OrderStatus {
    Active = 0,
    Executed = 1,
    Cancelled = 2,
}

impl From<u8> for OrderStatus {
    fn from(v: u8) -> Self {
        match v {
            0 => Self::Active,
            1 => Self::Executed,
            2 => Self::Cancelled,
            _ => Self::Active,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Order {
    pub order_id: [u8; 32],
    pub user_address: [u8; 20],
    pub market_id: u16,
    pub is_long: bool,
    pub order_type: OrderType,
    pub trigger_price: FixedI128,
    pub limit_price: Option<FixedI128>,
    pub size_usd: FixedI128,
    pub leverage: FixedI128,
    pub collateral_token: [u8; 20],
    pub collateral_amount: FixedI128,
    pub status: OrderStatus,
    pub execution_price: Option<FixedI128>,
    pub position_id: Option<[u8; 32]>,
    pub tx_hash: [u8; 32],
    pub block_number: u64,
    pub created_at: u64,
    pub executed_at: Option<u64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[repr(u8)]
pub enum TradeType {
    Open = 0,
    Close = 1,
    PartialClose = 2,
    Liquidation = 3,
    Increase = 4,
}

impl From<u8> for TradeType {
    fn from(v: u8) -> Self {
        match v {
            0 => Self::Open,
            1 => Self::Close,
            2 => Self::PartialClose,
            3 => Self::Liquidation,
            4 => Self::Increase,
            _ => Self::Open,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Trade {
    pub trade_id: [u8; 32],
    pub user_address: [u8; 20],
    pub market_id: u16,
    pub is_long: bool,
    pub size_usd: FixedI128,
    pub price: FixedI128,
    pub pnl: Option<FixedI128>,
    pub fee_usd: FixedI128,
    pub trade_type: TradeType,
    pub tx_hash: [u8; 32],
    pub block_number: u64,
    pub timestamp: u64,
}

/// Mirrors the `DecodedEvent` variants produced by the chain indexer.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Event {
    PositionOpened {
        position_id: [u8; 32],
        user: [u8; 20],
        market_id: u16,
        is_long: bool,
        size_usd: FixedI128,
        leverage: FixedI128,
        entry_price: FixedI128,
        collateral_token: [u8; 20],
        collateral_amount: FixedI128,
    },
    PositionClosed {
        position_id: [u8; 32],
        user: [u8; 20],
        market_id: u16,
        closed_size_usd: FixedI128,
        exit_price: FixedI128,
        realized_pnl: FixedI128,
        is_full_close: bool,
    },
    PositionModified {
        position_id: [u8; 32],
        new_size_usd: FixedI128,
        new_collateral_usd: FixedI128,
        new_collateral_amount: FixedI128,
    },
    OrderPlaced {
        order_id: [u8; 32],
        user: [u8; 20],
        market_id: u16,
        order_type: u8,
        is_long: bool,
        trigger_price: FixedI128,
        size_usd: FixedI128,
    },
    OrderExecuted {
        order_id: [u8; 32],
        position_id: [u8; 32],
        execution_price: FixedI128,
    },
    OrderCancelled {
        order_id: [u8; 32],
        user: [u8; 20],
    },
    Liquidation {
        position_id: [u8; 32],
        user: [u8; 20],
        market_id: u16,
        liquidation_price: FixedI128,
        penalty: FixedI128,
        keeper: [u8; 20],
    },
    ADLExecuted {
        position_id: [u8; 32],
        deleveraged_size_usd: FixedI128,
    },
    PriceUpdated {
        market_id: u16,
        price: FixedI128,
        timestamp: u64,
    },
    FundingSettled {
        market_id: u16,
        funding_rate: FixedI128,
        long_payment: FixedI128,
        short_payment: FixedI128,
    },
    FundingRateUpdated {
        market_id: u16,
        new_rate_per_second: FixedI128,
        funding_rate_24h: FixedI128,
    },
    MarketCreated {
        market_id: u16,
        name: String,
        symbol: String,
    },
    MarketPaused {
        market_id: u16,
    },
    CollateralDeposited {
        user: [u8; 20],
        token: [u8; 20],
        amount: FixedI128,
    },
    CollateralWithdrawn {
        user: [u8; 20],
        token: [u8; 20],
        amount: FixedI128,
    },
    VaultCreated {
        user: [u8; 20],
        vault: [u8; 20],
    },
    VaultDeficit {
        token: [u8; 20],
        deficit: FixedI128,
    },
}

/// Wrapper that attaches block/tx context to any Event.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IndexedEvent {
    pub block_number: u64,
    pub block_timestamp: u64,
    pub tx_hash: [u8; 32],
    pub tx_index: u32,
    pub log_index: u32,
    pub event: Event,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct User {
    pub address: [u8; 20],
    pub vault_address: Option<[u8; 20]>,
    pub referral_code: Option<String>,
    pub referred_by: Option<[u8; 20]>,
    pub tier: String,
    pub fee_discount_bps: u32,
    pub created_at: u64,
    pub first_trade_at: Option<u64>,
    pub last_active_at: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TraderStats {
    pub address: [u8; 20],
    pub total_pnl: FixedI128,
    pub total_volume: FixedI128,
    pub total_fees_paid: FixedI128,
    pub total_trades: u32,
    pub total_positions: u32,
    pub open_positions: u32,
    pub win_count: u32,
    pub loss_count: u32,
    pub liquidation_count: u32,
    pub best_trade_pnl: FixedI128,
    pub worst_trade_pnl: FixedI128,
    pub avg_leverage: FixedI128,
    pub max_leverage: FixedI128,
    pub total_funding_paid: FixedI128,
    pub total_funding_received: FixedI128,
    pub first_trade_at: Option<u64>,
    pub last_trade_at: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DailyStats {
    pub date: u32,
    pub pnl: FixedI128,
    pub volume: FixedI128,
    pub trades: u32,
    pub fees: FixedI128,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MarketInfo {
    pub id: u16,
    pub name: String,
    pub symbol: String,
    pub enabled: bool,
    pub mark_price: Option<FixedI128>,
    pub index_price: Option<FixedI128>,
    pub long_oi: Option<FixedI128>,
    pub short_oi: Option<FixedI128>,
    pub funding_rate: Option<FixedI128>,
    pub max_leverage: Option<FixedI128>,
    pub maintenance_margin_bps: Option<u32>,
    pub volume_24h: Option<FixedI128>,
    pub trades_24h: Option<u32>,
    pub price_change_24h: Option<FixedI128>,
    pub price_change_pct_24h: Option<FixedI128>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Balance {
    pub user_address: [u8; 20],
    pub token: [u8; 20],
    pub amount: FixedI128,
    pub locked: FixedI128,
    pub available: FixedI128,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReferralInfo {
    pub referral_code: Option<String>,
    pub referral_count: u32,
    pub total_rewards: FixedI128,
    pub tier: String,
}

/// Parse a decimal string (representing a wei-scale i256/u256) into i128.
/// Returns 0 on parse failure (caller should log).
pub fn parse_fixed(s: &str) -> FixedI128 {
    s.parse::<i128>().unwrap_or(0)
}

/// Parse a hex address string "0x..." into [u8; 20].
pub fn parse_address(s: &str) -> [u8; 20] {
    let s = s.strip_prefix("0x").unwrap_or(s);
    let mut buf = [0u8; 20];
    if let Ok(bytes) = hex_decode(s) {
        let len = bytes.len().min(20);
        buf[20 - len..].copy_from_slice(&bytes[..len]);
    }
    buf
}

/// Parse a hex hash string "0x..." into [u8; 32].
pub fn parse_hash(s: &str) -> [u8; 32] {
    let s = s.strip_prefix("0x").unwrap_or(s);
    let mut buf = [0u8; 32];
    if let Ok(bytes) = hex_decode(s) {
        let len = bytes.len().min(32);
        buf[32 - len..].copy_from_slice(&bytes[..len]);
    }
    buf
}

fn hex_decode(s: &str) -> Result<Vec<u8>, ()> {
    if s.len() % 2 != 0 {
        return Err(());
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).map_err(|_| ()))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn price_tick_size() {
        assert_eq!(size_of::<PriceTick>(), 24);
    }

    #[test]
    fn candle_record_size() {
        assert_eq!(size_of::<CandleRecord>(), 48);
    }

    #[test]
    fn parse_fixed_basic() {
        assert_eq!(parse_fixed("1000000000000000000"), 1_000_000_000_000_000_000);
        assert_eq!(parse_fixed("-500"), -500);
        assert_eq!(parse_fixed("invalid"), 0);
    }

    #[test]
    fn parse_address_basic() {
        let addr = parse_address("0xdead000000000000000000000000000000001234");
        assert_eq!(addr[0], 0xde);
        assert_eq!(addr[1], 0xad);
        assert_eq!(addr[18], 0x12);
        assert_eq!(addr[19], 0x34);
    }

    #[test]
    fn parse_hash_basic() {
        let h = parse_hash("0x0000000000000000000000000000000000000000000000000000000000000001");
        assert_eq!(h[31], 1);
        assert_eq!(h[0], 0);
    }
}
