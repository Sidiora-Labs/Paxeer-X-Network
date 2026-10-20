use std::sync::atomic::{AtomicU64, Ordering};

use parking_lot::RwLock;

use crate::core::market::{MarketId, MARKET_COUNT};
use crate::core::types::FixedI128;

/// Per-market real-time state. Replaces DragonflyDB keys:
/// - `price:latest:{mid}` → `latest_price()`
/// - `funding:rate:{mid}` → `funding_rate()`
/// - `market:{mid}` → `snapshot()`
/// - `markets:active` → `active_markets()`
///
/// Hot path (price reads): lock-free via `AtomicU64` (~5ns).
/// Warm path (funding, OI, flags): `parking_lot::RwLock` (~25ns uncontended).
pub struct MarketStateTable {
    slots: Vec<MarketSlot>,
}

struct MarketSlot {
    /// Latest price as f64 bits. Lock-free hot-path reads.
    latest_price_bits: AtomicU64,
    /// Nanosecond timestamp of last price update.
    last_update_ns: AtomicU64,
    /// Warm-path fields behind RwLock.
    inner: RwLock<MarketInner>,
}

#[derive(Debug, Clone)]
struct MarketInner {
    mark_price: Option<FixedI128>,
    index_price: Option<FixedI128>,
    funding_rate_per_second: FixedI128,
    funding_rate_24h: FixedI128,
    long_oi: FixedI128,
    short_oi: FixedI128,
    enabled: bool,
    paused: bool,
    max_leverage: Option<FixedI128>,
    maintenance_margin_bps: Option<u32>,
    volume_24h: Option<FixedI128>,
    trades_24h: Option<u32>,
    price_change_24h: Option<FixedI128>,
    price_change_pct_24h: Option<FixedI128>,
}

impl Default for MarketInner {
    fn default() -> Self {
        Self {
            mark_price: None,
            index_price: None,
            funding_rate_per_second: 0,
            funding_rate_24h: 0,
            long_oi: 0,
            short_oi: 0,
            enabled: true,
            paused: false,
            max_leverage: None,
            maintenance_margin_bps: None,
            volume_24h: None,
            trades_24h: None,
            price_change_24h: None,
            price_change_pct_24h: None,
        }
    }
}

/// Read-only snapshot of a market's state. Returned by `snapshot()`.
#[derive(Debug, Clone)]
pub struct MarketSnapshot {
    pub market_id: MarketId,
    pub latest_price: f64,
    pub last_update_ns: u64,
    pub mark_price: Option<FixedI128>,
    pub index_price: Option<FixedI128>,
    pub funding_rate_per_second: FixedI128,
    pub funding_rate_24h: FixedI128,
    pub long_oi: FixedI128,
    pub short_oi: FixedI128,
    pub enabled: bool,
    pub paused: bool,
    pub max_leverage: Option<FixedI128>,
    pub maintenance_margin_bps: Option<u32>,
    pub volume_24h: Option<FixedI128>,
    pub trades_24h: Option<u32>,
    pub price_change_24h: Option<FixedI128>,
    pub price_change_pct_24h: Option<FixedI128>,
}

impl MarketStateTable {
    /// Create a table for `MARKET_COUNT` markets, all initialized to defaults.
    pub fn new() -> Self {
        let slots = (0..MARKET_COUNT)
            .map(|_| MarketSlot {
                latest_price_bits: AtomicU64::new(0),
                last_update_ns: AtomicU64::new(0),
                inner: RwLock::new(MarketInner::default()),
            })
            .collect();
        Self { slots }
    }

    /// Latest price for a market. Lock-free, < 10ns.
    /// Returns 0.0 if never updated.
    pub fn latest_price(&self, market_id: MarketId) -> f64 {
        match self.slots.get(market_id as usize) {
            Some(slot) => f64::from_bits(slot.latest_price_bits.load(Ordering::Acquire)),
            None => 0.0,
        }
    }

    /// Nanosecond timestamp of last price update.
    pub fn last_update_ns(&self, market_id: MarketId) -> u64 {
        match self.slots.get(market_id as usize) {
            Some(slot) => slot.last_update_ns.load(Ordering::Acquire),
            None => 0,
        }
    }

    /// Update the latest price atomically. Called on every price tick.
    pub fn set_price(&self, market_id: MarketId, price: f64, timestamp_ns: u64) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            slot.latest_price_bits.store(price.to_bits(), Ordering::Release);
            slot.last_update_ns.store(timestamp_ns, Ordering::Release);
        }
    }

    /// Update funding rate fields. Called on FundingRateUpdated events.
    pub fn set_funding_rate(
        &self,
        market_id: MarketId,
        rate_per_second: FixedI128,
        rate_24h: FixedI128,
    ) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            let mut inner = slot.inner.write();
            inner.funding_rate_per_second = rate_per_second;
            inner.funding_rate_24h = rate_24h;
        }
    }

    /// Read funding rate per second.
    pub fn funding_rate(&self, market_id: MarketId) -> FixedI128 {
        match self.slots.get(market_id as usize) {
            Some(slot) => slot.inner.read().funding_rate_per_second,
            None => 0,
        }
    }

    /// Read funding rate 24h.
    pub fn funding_rate_24h(&self, market_id: MarketId) -> FixedI128 {
        match self.slots.get(market_id as usize) {
            Some(slot) => slot.inner.read().funding_rate_24h,
            None => 0,
        }
    }

    /// Update open interest.
    pub fn set_open_interest(
        &self,
        market_id: MarketId,
        long_oi: FixedI128,
        short_oi: FixedI128,
    ) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            let mut inner = slot.inner.write();
            inner.long_oi = long_oi;
            inner.short_oi = short_oi;
        }
    }

    /// Add to long OI (atomic increment under write lock).
    pub fn add_long_oi(&self, market_id: MarketId, delta: FixedI128) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            slot.inner.write().long_oi += delta;
        }
    }

    /// Add to short OI.
    pub fn add_short_oi(&self, market_id: MarketId, delta: FixedI128) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            slot.inner.write().short_oi += delta;
        }
    }

    /// Read (long_oi, short_oi).
    pub fn open_interest(&self, market_id: MarketId) -> (FixedI128, FixedI128) {
        match self.slots.get(market_id as usize) {
            Some(slot) => {
                let inner = slot.inner.read();
                (inner.long_oi, inner.short_oi)
            }
            None => (0, 0),
        }
    }

    /// Set mark/index prices (from oracle events, less frequent than price feed).
    pub fn set_oracle_prices(
        &self,
        market_id: MarketId,
        mark: Option<FixedI128>,
        index: Option<FixedI128>,
    ) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            let mut inner = slot.inner.write();
            if mark.is_some() {
                inner.mark_price = mark;
            }
            if index.is_some() {
                inner.index_price = index;
            }
        }
    }

    /// Enable or disable a market.
    pub fn set_enabled(&self, market_id: MarketId, enabled: bool) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            slot.inner.write().enabled = enabled;
        }
    }

    /// Pause or unpause a market.
    pub fn set_paused(&self, market_id: MarketId, paused: bool) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            slot.inner.write().paused = paused;
        }
    }

    pub fn is_enabled(&self, market_id: MarketId) -> bool {
        self.slots
            .get(market_id as usize)
            .map(|s| s.inner.read().enabled)
            .unwrap_or(false)
    }

    pub fn is_paused(&self, market_id: MarketId) -> bool {
        self.slots
            .get(market_id as usize)
            .map(|s| s.inner.read().paused)
            .unwrap_or(false)
    }

    /// Set market configuration parameters.
    pub fn set_config(
        &self,
        market_id: MarketId,
        max_leverage: Option<FixedI128>,
        maintenance_margin_bps: Option<u32>,
    ) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            let mut inner = slot.inner.write();
            inner.max_leverage = max_leverage;
            inner.maintenance_margin_bps = maintenance_margin_bps;
        }
    }

    /// Update 24h rolling stats.
    pub fn set_24h_stats(
        &self,
        market_id: MarketId,
        volume: Option<FixedI128>,
        trades: Option<u32>,
        price_change: Option<FixedI128>,
        price_change_pct: Option<FixedI128>,
    ) {
        if let Some(slot) = self.slots.get(market_id as usize) {
            let mut inner = slot.inner.write();
            inner.volume_24h = volume;
            inner.trades_24h = trades;
            inner.price_change_24h = price_change;
            inner.price_change_pct_24h = price_change_pct;
        }
    }

    /// Full read-only snapshot of a market's state.
    pub fn snapshot(&self, market_id: MarketId) -> Option<MarketSnapshot> {
        let slot = self.slots.get(market_id as usize)?;
        let inner = slot.inner.read();
        Some(MarketSnapshot {
            market_id,
            latest_price: f64::from_bits(slot.latest_price_bits.load(Ordering::Acquire)),
            last_update_ns: slot.last_update_ns.load(Ordering::Acquire),
            mark_price: inner.mark_price,
            index_price: inner.index_price,
            funding_rate_per_second: inner.funding_rate_per_second,
            funding_rate_24h: inner.funding_rate_24h,
            long_oi: inner.long_oi,
            short_oi: inner.short_oi,
            enabled: inner.enabled,
            paused: inner.paused,
            max_leverage: inner.max_leverage,
            maintenance_margin_bps: inner.maintenance_margin_bps,
            volume_24h: inner.volume_24h,
            trades_24h: inner.trades_24h,
            price_change_24h: inner.price_change_24h,
            price_change_pct_24h: inner.price_change_pct_24h,
        })
    }

    /// List of all active (enabled + not paused) market IDs.
    pub fn active_markets(&self) -> Vec<MarketId> {
        self.slots
            .iter()
            .enumerate()
            .filter_map(|(i, slot)| {
                let inner = slot.inner.read();
                if inner.enabled && !inner.paused {
                    Some(i as MarketId)
                } else {
                    None
                }
            })
            .collect()
    }

    /// Latest prices for all markets as (market_id, price) pairs.
    /// Skips markets with price 0.0 (never updated).
    pub fn all_prices(&self) -> Vec<(MarketId, f64)> {
        self.slots
            .iter()
            .enumerate()
            .filter_map(|(i, slot)| {
                let price = f64::from_bits(slot.latest_price_bits.load(Ordering::Acquire));
                if price != 0.0 {
                    Some((i as MarketId, price))
                } else {
                    None
                }
            })
            .collect()
    }
}

impl Default for MarketStateTable {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn price_read_write() {
        let table = MarketStateTable::new();

        assert_eq!(table.latest_price(0), 0.0);
        assert_eq!(table.last_update_ns(0), 0);

        table.set_price(0, 67_000.50, 1_000_000_000);

        assert_eq!(table.latest_price(0), 67_000.50);
        assert_eq!(table.last_update_ns(0), 1_000_000_000);
    }

    #[test]
    fn price_update_overwrites() {
        let table = MarketStateTable::new();
        table.set_price(0, 67_000.0, 1_000);
        table.set_price(0, 67_100.0, 2_000);
        assert_eq!(table.latest_price(0), 67_100.0);
        assert_eq!(table.last_update_ns(0), 2_000);
    }

    #[test]
    fn multi_market_prices() {
        let table = MarketStateTable::new();
        table.set_price(0, 67_000.0, 1_000);
        table.set_price(1, 3_500.0, 1_000);
        table.set_price(2, 150.0, 1_000);

        assert_eq!(table.latest_price(0), 67_000.0);
        assert_eq!(table.latest_price(1), 3_500.0);
        assert_eq!(table.latest_price(2), 150.0);
        assert_eq!(table.latest_price(3), 0.0);
    }

    #[test]
    fn out_of_range_market_returns_defaults() {
        let table = MarketStateTable::new();
        assert_eq!(table.latest_price(99), 0.0);
        assert_eq!(table.funding_rate(99), 0);
        assert_eq!(table.open_interest(99), (0, 0));
        assert!(table.snapshot(99).is_none());
    }

    #[test]
    fn funding_rate() {
        let table = MarketStateTable::new();
        table.set_funding_rate(0, 1_000_000, 86_400_000_000);

        assert_eq!(table.funding_rate(0), 1_000_000);
        assert_eq!(table.funding_rate_24h(0), 86_400_000_000);
    }

    #[test]
    fn open_interest() {
        let table = MarketStateTable::new();
        table.set_open_interest(0, 1_000_000, 800_000);
        assert_eq!(table.open_interest(0), (1_000_000, 800_000));

        table.add_long_oi(0, 200_000);
        assert_eq!(table.open_interest(0), (1_200_000, 800_000));

        table.add_short_oi(0, 100_000);
        assert_eq!(table.open_interest(0), (1_200_000, 900_000));
    }

    #[test]
    fn enable_disable_pause() {
        let table = MarketStateTable::new();

        assert!(table.is_enabled(0));
        assert!(!table.is_paused(0));

        table.set_paused(0, true);
        assert!(table.is_paused(0));
        assert!(table.is_enabled(0));

        table.set_enabled(0, false);
        assert!(!table.is_enabled(0));
    }

    #[test]
    fn active_markets() {
        let table = MarketStateTable::new();

        let active = table.active_markets();
        assert_eq!(active.len(), MARKET_COUNT);

        table.set_paused(5, true);
        table.set_enabled(10, false);

        let active = table.active_markets();
        assert_eq!(active.len(), MARKET_COUNT - 2);
        assert!(!active.contains(&5));
        assert!(!active.contains(&10));
    }

    #[test]
    fn snapshot() {
        let table = MarketStateTable::new();
        table.set_price(0, 67_000.0, 1_000_000);
        table.set_funding_rate(0, 500, 43_200_000);
        table.set_open_interest(0, 10_000, 8_000);
        table.set_oracle_prices(0, Some(67_000_000), None);
        table.set_config(0, Some(100_000_000_000_000_000_000), Some(50));
        table.set_24h_stats(0, Some(5_000_000), Some(1200), None, None);

        let snap = table.snapshot(0).unwrap();
        assert_eq!(snap.market_id, 0);
        assert_eq!(snap.latest_price, 67_000.0);
        assert_eq!(snap.last_update_ns, 1_000_000);
        assert_eq!(snap.funding_rate_per_second, 500);
        assert_eq!(snap.funding_rate_24h, 43_200_000);
        assert_eq!(snap.long_oi, 10_000);
        assert_eq!(snap.short_oi, 8_000);
        assert_eq!(snap.mark_price, Some(67_000_000));
        assert!(snap.index_price.is_none());
        assert!(snap.enabled);
        assert!(!snap.paused);
        assert_eq!(snap.max_leverage, Some(100_000_000_000_000_000_000));
        assert_eq!(snap.maintenance_margin_bps, Some(50));
        assert_eq!(snap.volume_24h, Some(5_000_000));
        assert_eq!(snap.trades_24h, Some(1200));
    }

    #[test]
    fn all_prices() {
        let table = MarketStateTable::new();
        table.set_price(0, 67_000.0, 1_000);
        table.set_price(1, 3_500.0, 1_000);

        let prices = table.all_prices();
        assert_eq!(prices.len(), 2);
        assert!(prices.contains(&(0, 67_000.0)));
        assert!(prices.contains(&(1, 3_500.0)));
    }

    #[test]
    fn oracle_prices_partial_update() {
        let table = MarketStateTable::new();
        table.set_oracle_prices(0, Some(67_000_000), Some(66_990_000));

        let snap = table.snapshot(0).unwrap();
        assert_eq!(snap.mark_price, Some(67_000_000));
        assert_eq!(snap.index_price, Some(66_990_000));

        table.set_oracle_prices(0, Some(67_100_000), None);
        let snap = table.snapshot(0).unwrap();
        assert_eq!(snap.mark_price, Some(67_100_000));
        assert_eq!(snap.index_price, Some(66_990_000));
    }

    #[test]
    fn set_price_out_of_range_no_panic() {
        let table = MarketStateTable::new();
        table.set_price(99, 1.0, 1_000);
        table.set_funding_rate(99, 1, 1);
    }
}
