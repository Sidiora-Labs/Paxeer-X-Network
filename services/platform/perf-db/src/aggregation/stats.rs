use dashmap::DashMap;

use crate::core::types::{DailyStats, FixedI128, TraderStats};

/// In-memory trader stats store. Replaces PostgreSQL `trader_stats` table.
///
/// Keyed by trader address. Each entry holds cumulative lifetime stats.
/// Updated by the indexer on PositionClosed, Liquidation, etc.
///
/// Concurrency: DashMap provides sharded RwLock.
pub struct TraderStatsStore {
    stats: DashMap<[u8; 20], TraderStats>,
}

impl TraderStatsStore {
    pub fn new() -> Self {
        Self {
            stats: DashMap::new(),
        }
    }

    /// Ensure a trader has a stats entry. Creates with zeroes if missing.
    pub fn ensure(&self, address: [u8; 20]) {
        self.stats.entry(address).or_insert_with(|| TraderStats {
            address,
            total_pnl: 0,
            total_volume: 0,
            total_fees_paid: 0,
            total_trades: 0,
            total_positions: 0,
            open_positions: 0,
            win_count: 0,
            loss_count: 0,
            liquidation_count: 0,
            best_trade_pnl: 0,
            worst_trade_pnl: 0,
            avg_leverage: 0,
            max_leverage: 0,
            total_funding_paid: 0,
            total_funding_received: 0,
            first_trade_at: None,
            last_trade_at: None,
        });
    }

    /// Get stats for a trader. O(1).
    pub fn get(&self, address: &[u8; 20]) -> Option<TraderStats> {
        self.stats.get(address).map(|r| r.clone())
    }

    /// Update stats in place. Returns `true` if the trader exists.
    pub fn update(&self, address: &[u8; 20], f: impl FnOnce(&mut TraderStats)) -> bool {
        match self.stats.get_mut(address) {
            Some(mut entry) => {
                f(entry.value_mut());
                true
            }
            None => false,
        }
    }

    /// Increment trade stats atomically. Matches PG `increment_trade_stats`.
    pub fn increment_trade(
        &self,
        address: &[u8; 20],
        volume: FixedI128,
        pnl: FixedI128,
        fee: FixedI128,
        is_win: bool,
        is_liquidation: bool,
        timestamp: u64,
    ) -> bool {
        match self.stats.get_mut(address) {
            Some(mut entry) => {
                let s = entry.value_mut();
                s.total_trades += 1;
                s.total_volume += volume;
                s.total_pnl += pnl;
                s.total_fees_paid += fee;

                if is_win {
                    s.win_count += 1;
                } else if !is_liquidation {
                    s.loss_count += 1;
                }
                if is_liquidation {
                    s.liquidation_count += 1;
                }

                if pnl > s.best_trade_pnl {
                    s.best_trade_pnl = pnl;
                }
                if pnl < s.worst_trade_pnl {
                    s.worst_trade_pnl = pnl;
                }

                if s.first_trade_at.is_none() {
                    s.first_trade_at = Some(timestamp);
                }
                s.last_trade_at = Some(timestamp);

                true
            }
            None => false,
        }
    }

    /// Increment open_positions count.
    pub fn increment_open_positions(&self, address: &[u8; 20]) {
        if let Some(mut entry) = self.stats.get_mut(address) {
            entry.total_positions += 1;
            entry.open_positions += 1;
        }
    }

    /// Decrement open_positions count (clamped to 0).
    pub fn decrement_open_positions(&self, address: &[u8; 20]) {
        if let Some(mut entry) = self.stats.get_mut(address) {
            entry.open_positions = entry.open_positions.saturating_sub(1);
        }
    }

    /// Total number of traders with stats.
    pub fn count(&self) -> usize {
        self.stats.len()
    }

    /// All stats (for snapshots, leaderboard computation).
    pub fn all(&self) -> Vec<TraderStats> {
        self.stats.iter().map(|r| r.value().clone()).collect()
    }

    /// Remove a trader's stats.
    pub fn remove(&self, address: &[u8; 20]) -> Option<TraderStats> {
        self.stats.remove(address).map(|(_, v)| v)
    }
}

impl Default for TraderStatsStore {
    fn default() -> Self {
        Self::new()
    }
}

/// Key for daily stats: (address, date_u32).
/// date_u32 = YYYYMMDD as u32, e.g. 20241115.
type DailyKey = ([u8; 20], u32);

/// In-memory daily stats store. Replaces PostgreSQL
/// `trader_daily_stats`, `market_daily_stats`, `protocol_daily_stats`.
///
/// Trader daily stats: keyed by (address, date).
/// Protocol daily stats: aggregated from trader daily stats on demand.
pub struct DailyStatsStore {
    trader_daily: DashMap<DailyKey, DailyStats>,
}

impl DailyStatsStore {
    pub fn new() -> Self {
        Self {
            trader_daily: DashMap::new(),
        }
    }

    /// Set/overwrite daily stats for a trader on a date.
    pub fn set_trader_daily(&self, address: [u8; 20], date: u32, stats: DailyStats) {
        self.trader_daily.insert((address, date), stats);
    }

    /// Get daily stats for a trader on a date.
    pub fn get_trader_daily(&self, address: &[u8; 20], date: u32) -> Option<DailyStats> {
        self.trader_daily.get(&(*address, date)).map(|r| r.clone())
    }

    /// Update daily stats for a trader in place.
    pub fn update_trader_daily(
        &self,
        address: &[u8; 20],
        date: u32,
        f: impl FnOnce(&mut DailyStats),
    ) -> bool {
        match self.trader_daily.get_mut(&(*address, date)) {
            Some(mut entry) => {
                f(entry.value_mut());
                true
            }
            None => false,
        }
    }

    /// Ensure a trader daily entry exists. Creates with zeroes if missing.
    pub fn ensure_trader_daily(&self, address: [u8; 20], date: u32) {
        self.trader_daily.entry((address, date)).or_insert_with(|| {
            DailyStats {
                date,
                pnl: 0,
                volume: 0,
                trades: 0,
                fees: 0,
            }
        });
    }

    /// Accumulate a trade into a trader's daily stats.
    pub fn accumulate_trade(
        &self,
        address: [u8; 20],
        date: u32,
        volume: FixedI128,
        pnl: FixedI128,
        fee: FixedI128,
    ) {
        self.trader_daily
            .entry((address, date))
            .and_modify(|s| {
                s.volume += volume;
                s.pnl += pnl;
                s.trades += 1;
                s.fees += fee;
            })
            .or_insert_with(|| DailyStats {
                date,
                pnl,
                volume,
                trades: 1,
                fees: fee,
            });
    }

    /// All daily stats for a trader, sorted by date descending.
    pub fn trader_daily_history(
        &self,
        address: &[u8; 20],
        limit: usize,
    ) -> Vec<DailyStats> {
        let mut entries: Vec<DailyStats> = self
            .trader_daily
            .iter()
            .filter(|r| &r.key().0 == address)
            .map(|r| r.value().clone())
            .collect();
        entries.sort_by(|a, b| b.date.cmp(&a.date));
        entries.truncate(limit);
        entries
    }

    /// Aggregate protocol-wide daily stats for a given date.
    /// Sums across all traders.
    pub fn protocol_daily(&self, date: u32) -> DailyStats {
        let mut agg = DailyStats {
            date,
            pnl: 0,
            volume: 0,
            trades: 0,
            fees: 0,
        };

        for entry in self.trader_daily.iter() {
            if entry.key().1 == date {
                let s = entry.value();
                agg.volume += s.volume;
                agg.pnl += s.pnl;
                agg.trades += s.trades;
                agg.fees += s.fees;
            }
        }

        agg
    }

    /// Number of unique traders active on a given date.
    pub fn unique_traders_on_date(&self, date: u32) -> usize {
        self.trader_daily
            .iter()
            .filter(|r| r.key().1 == date && r.value().trades > 0)
            .count()
    }

    /// Total daily stats entries.
    pub fn count(&self) -> usize {
        self.trader_daily.len()
    }
}

impl Default for DailyStatsStore {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[19] = id;
        buf
    }

    #[test]
    fn ensure_and_get() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        let stats = store.get(&addr(1)).unwrap();
        assert_eq!(stats.total_trades, 0);
        assert_eq!(stats.total_pnl, 0);
    }

    #[test]
    fn get_nonexistent() {
        let store = TraderStatsStore::new();
        assert!(store.get(&addr(99)).is_none());
    }

    #[test]
    fn increment_trade() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        assert!(store.increment_trade(&addr(1), 1_000_000, 500_000, 10_000, true, false, 1700000000));

        let s = store.get(&addr(1)).unwrap();
        assert_eq!(s.total_trades, 1);
        assert_eq!(s.total_volume, 1_000_000);
        assert_eq!(s.total_pnl, 500_000);
        assert_eq!(s.total_fees_paid, 10_000);
        assert_eq!(s.win_count, 1);
        assert_eq!(s.loss_count, 0);
        assert_eq!(s.best_trade_pnl, 500_000);
        assert_eq!(s.first_trade_at, Some(1700000000));
        assert_eq!(s.last_trade_at, Some(1700000000));
    }

    #[test]
    fn increment_trade_loss() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        store.increment_trade(&addr(1), 1_000, -300, 10, false, false, 100);

        let s = store.get(&addr(1)).unwrap();
        assert_eq!(s.win_count, 0);
        assert_eq!(s.loss_count, 1);
        assert_eq!(s.worst_trade_pnl, -300);
    }

    #[test]
    fn increment_trade_liquidation() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        store.increment_trade(&addr(1), 0, 0, 0, false, true, 100);

        let s = store.get(&addr(1)).unwrap();
        assert_eq!(s.liquidation_count, 1);
        assert_eq!(s.loss_count, 0);
    }

    #[test]
    fn increment_nonexistent() {
        let store = TraderStatsStore::new();
        assert!(!store.increment_trade(&addr(99), 0, 0, 0, false, false, 0));
    }

    #[test]
    fn open_positions_tracking() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        store.increment_open_positions(&addr(1));
        store.increment_open_positions(&addr(1));
        assert_eq!(store.get(&addr(1)).unwrap().open_positions, 2);
        assert_eq!(store.get(&addr(1)).unwrap().total_positions, 2);

        store.decrement_open_positions(&addr(1));
        assert_eq!(store.get(&addr(1)).unwrap().open_positions, 1);

        store.decrement_open_positions(&addr(1));
        store.decrement_open_positions(&addr(1));
        assert_eq!(store.get(&addr(1)).unwrap().open_positions, 0);
    }

    #[test]
    fn multiple_trades_accumulate() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));

        store.increment_trade(&addr(1), 1_000, 200, 10, true, false, 100);
        store.increment_trade(&addr(1), 2_000, -500, 20, false, false, 200);
        store.increment_trade(&addr(1), 3_000, 1_000, 30, true, false, 300);

        let s = store.get(&addr(1)).unwrap();
        assert_eq!(s.total_trades, 3);
        assert_eq!(s.total_volume, 6_000);
        assert_eq!(s.total_pnl, 700);
        assert_eq!(s.total_fees_paid, 60);
        assert_eq!(s.win_count, 2);
        assert_eq!(s.loss_count, 1);
        assert_eq!(s.best_trade_pnl, 1_000);
        assert_eq!(s.worst_trade_pnl, -500);
        assert_eq!(s.first_trade_at, Some(100));
        assert_eq!(s.last_trade_at, Some(300));
    }

    #[test]
    fn count_and_all() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));
        store.ensure(addr(2));

        assert_eq!(store.count(), 2);
        assert_eq!(store.all().len(), 2);
    }

    #[test]
    fn remove_stats() {
        let store = TraderStatsStore::new();
        store.ensure(addr(1));
        assert!(store.remove(&addr(1)).is_some());
        assert!(store.get(&addr(1)).is_none());
    }

    #[test]
    fn set_and_get_trader_daily() {
        let store = DailyStatsStore::new();
        store.set_trader_daily(addr(1), 20241115, DailyStats {
            date: 20241115,
            pnl: 500,
            volume: 10_000,
            trades: 5,
            fees: 100,
        });

        let daily = store.get_trader_daily(&addr(1), 20241115).unwrap();
        assert_eq!(daily.pnl, 500);
        assert_eq!(daily.volume, 10_000);
    }

    #[test]
    fn get_nonexistent_daily() {
        let store = DailyStatsStore::new();
        assert!(store.get_trader_daily(&addr(1), 20241115).is_none());
    }

    #[test]
    fn accumulate_trade() {
        let store = DailyStatsStore::new();

        store.accumulate_trade(addr(1), 20241115, 1_000, 200, 10);
        store.accumulate_trade(addr(1), 20241115, 2_000, -100, 20);

        let daily = store.get_trader_daily(&addr(1), 20241115).unwrap();
        assert_eq!(daily.volume, 3_000);
        assert_eq!(daily.pnl, 100);
        assert_eq!(daily.trades, 2);
        assert_eq!(daily.fees, 30);
    }

    #[test]
    fn trader_daily_history() {
        let store = DailyStatsStore::new();
        store.accumulate_trade(addr(1), 20241113, 1_000, 100, 10);
        store.accumulate_trade(addr(1), 20241114, 2_000, 200, 20);
        store.accumulate_trade(addr(1), 20241115, 3_000, 300, 30);

        let history = store.trader_daily_history(&addr(1), 2);
        assert_eq!(history.len(), 2);
        assert_eq!(history[0].date, 20241115);
        assert_eq!(history[1].date, 20241114);
    }

    #[test]
    fn protocol_daily() {
        let store = DailyStatsStore::new();
        store.accumulate_trade(addr(1), 20241115, 1_000, 200, 10);
        store.accumulate_trade(addr(2), 20241115, 2_000, 300, 20);
        store.accumulate_trade(addr(1), 20241114, 500, 50, 5);

        let proto = store.protocol_daily(20241115);
        assert_eq!(proto.volume, 3_000);
        assert_eq!(proto.pnl, 500);
        assert_eq!(proto.trades, 2);
        assert_eq!(proto.fees, 30);
    }

    #[test]
    fn unique_traders_on_date() {
        let store = DailyStatsStore::new();
        store.accumulate_trade(addr(1), 20241115, 1_000, 200, 10);
        store.accumulate_trade(addr(2), 20241115, 2_000, 300, 20);
        store.accumulate_trade(addr(3), 20241114, 500, 50, 5);

        assert_eq!(store.unique_traders_on_date(20241115), 2);
        assert_eq!(store.unique_traders_on_date(20241114), 1);
        assert_eq!(store.unique_traders_on_date(20241116), 0);
    }

    #[test]
    fn ensure_trader_daily() {
        let store = DailyStatsStore::new();
        store.ensure_trader_daily(addr(1), 20241115);

        let daily = store.get_trader_daily(&addr(1), 20241115).unwrap();
        assert_eq!(daily.trades, 0);
        assert_eq!(daily.pnl, 0);
    }

    #[test]
    fn daily_count() {
        let store = DailyStatsStore::new();
        store.accumulate_trade(addr(1), 20241115, 1_000, 200, 10);
        store.accumulate_trade(addr(1), 20241114, 500, 50, 5);
        store.accumulate_trade(addr(2), 20241115, 2_000, 300, 20);

        assert_eq!(store.count(), 3);
    }
}
