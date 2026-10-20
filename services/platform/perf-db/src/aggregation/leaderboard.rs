use parking_lot::RwLock;

use crate::core::types::{FixedI128, TraderStats};

/// A single entry on the leaderboard.
#[derive(Debug, Clone)]
pub struct LeaderboardEntry {
    pub rank: u32,
    pub address: [u8; 20],
    pub value: FixedI128,
    pub total_trades: u32,
    pub win_count: u32,
    pub loss_count: u32,
}

/// Leaderboard ranking metric.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum LeaderboardMetric {
    Pnl,
    Volume,
}

/// In-memory leaderboard cache. Replaces PostgreSQL materialized views:
/// - `mv_leaderboard_pnl_24h`
/// - `mv_leaderboard_volume_24h`
///
/// Rebuilt periodically (every 5 min) from `TraderStatsStore`.
/// API reads from the cached sorted Vec — zero computation on read.
///
/// Concurrency: `parking_lot::RwLock` — single writer (background task),
/// multiple concurrent readers (API).
pub struct Leaderboard {
    pnl_board: RwLock<LeaderboardData>,
    volume_board: RwLock<LeaderboardData>,
    max_entries: usize,
}

#[derive(Debug, Clone)]
struct LeaderboardData {
    entries: Vec<LeaderboardEntry>,
    last_rebuilt_at: u64,
}

impl Default for LeaderboardData {
    fn default() -> Self {
        Self {
            entries: Vec::new(),
            last_rebuilt_at: 0,
        }
    }
}

impl Leaderboard {
    /// Create a leaderboard with a maximum number of entries per board.
    pub fn new(max_entries: usize) -> Self {
        Self {
            pnl_board: RwLock::new(LeaderboardData::default()),
            volume_board: RwLock::new(LeaderboardData::default()),
            max_entries,
        }
    }

    /// Rebuild both leaderboards from a snapshot of all trader stats.
    ///
    /// Called by a background task every N minutes.
    /// `now` is the current unix timestamp for recording when the rebuild happened.
    pub fn rebuild(&self, all_stats: &[TraderStats], now: u64) {
        self.rebuild_pnl(all_stats, now);
        self.rebuild_volume(all_stats, now);
    }

    /// Get the PnL leaderboard (top N, highest PnL first).
    pub fn pnl_board(&self, limit: usize) -> Vec<LeaderboardEntry> {
        let guard = self.pnl_board.read();
        guard.entries.iter().take(limit).cloned().collect()
    }

    /// Get the volume leaderboard (top N, highest volume first).
    pub fn volume_board(&self, limit: usize) -> Vec<LeaderboardEntry> {
        let guard = self.volume_board.read();
        guard.entries.iter().take(limit).cloned().collect()
    }

    /// When the PnL leaderboard was last rebuilt (unix timestamp).
    pub fn pnl_last_rebuilt_at(&self) -> u64 {
        self.pnl_board.read().last_rebuilt_at
    }

    /// When the volume leaderboard was last rebuilt.
    pub fn volume_last_rebuilt_at(&self) -> u64 {
        self.volume_board.read().last_rebuilt_at
    }

    /// Find a trader's rank on the PnL leaderboard. `None` if not in top N.
    pub fn pnl_rank(&self, address: &[u8; 20]) -> Option<u32> {
        let guard = self.pnl_board.read();
        guard
            .entries
            .iter()
            .find(|e| &e.address == address)
            .map(|e| e.rank)
    }

    /// Find a trader's rank on the volume leaderboard.
    pub fn volume_rank(&self, address: &[u8; 20]) -> Option<u32> {
        let guard = self.volume_board.read();
        guard
            .entries
            .iter()
            .find(|e| &e.address == address)
            .map(|e| e.rank)
    }

    /// Maximum entries per board.
    pub fn max_entries(&self) -> usize {
        self.max_entries
    }

    fn rebuild_pnl(&self, all_stats: &[TraderStats], now: u64) {
        let mut sorted: Vec<_> = all_stats
            .iter()
            .filter(|s| s.total_trades > 0)
            .collect();
        sorted.sort_by(|a, b| b.total_pnl.cmp(&a.total_pnl));
        sorted.truncate(self.max_entries);

        let entries: Vec<LeaderboardEntry> = sorted
            .into_iter()
            .enumerate()
            .map(|(i, s)| LeaderboardEntry {
                rank: (i + 1) as u32,
                address: s.address,
                value: s.total_pnl,
                total_trades: s.total_trades,
                win_count: s.win_count,
                loss_count: s.loss_count,
            })
            .collect();

        *self.pnl_board.write() = LeaderboardData {
            entries,
            last_rebuilt_at: now,
        };
    }

    fn rebuild_volume(&self, all_stats: &[TraderStats], now: u64) {
        let mut sorted: Vec<_> = all_stats
            .iter()
            .filter(|s| s.total_trades > 0)
            .collect();
        sorted.sort_by(|a, b| b.total_volume.cmp(&a.total_volume));
        sorted.truncate(self.max_entries);

        let entries: Vec<LeaderboardEntry> = sorted
            .into_iter()
            .enumerate()
            .map(|(i, s)| LeaderboardEntry {
                rank: (i + 1) as u32,
                address: s.address,
                value: s.total_volume,
                total_trades: s.total_trades,
                win_count: s.win_count,
                loss_count: s.loss_count,
            })
            .collect();

        *self.volume_board.write() = LeaderboardData {
            entries,
            last_rebuilt_at: now,
        };
    }
}

impl Default for Leaderboard {
    fn default() -> Self {
        Self::new(500)
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

    fn make_stats(id: u8, pnl: FixedI128, volume: FixedI128, trades: u32) -> TraderStats {
        TraderStats {
            address: addr(id),
            total_pnl: pnl,
            total_volume: volume,
            total_fees_paid: 0,
            total_trades: trades,
            total_positions: trades,
            open_positions: 0,
            win_count: trades / 2,
            loss_count: trades - trades / 2,
            liquidation_count: 0,
            best_trade_pnl: pnl,
            worst_trade_pnl: 0,
            avg_leverage: 0,
            max_leverage: 0,
            total_funding_paid: 0,
            total_funding_received: 0,
            first_trade_at: Some(1700000000),
            last_trade_at: Some(1700001000),
        }
    }

    #[test]
    fn empty_leaderboard() {
        let lb = Leaderboard::new(100);
        assert!(lb.pnl_board(10).is_empty());
        assert!(lb.volume_board(10).is_empty());
        assert_eq!(lb.pnl_last_rebuilt_at(), 0);
    }

    #[test]
    fn rebuild_pnl_ordering() {
        let lb = Leaderboard::new(100);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 1_000, 5_000, 3),
            make_stats(3, -200, 20_000, 10),
        ];

        lb.rebuild(&stats, 1700000000);

        let board = lb.pnl_board(10);
        assert_eq!(board.len(), 3);
        assert_eq!(board[0].address, addr(2));
        assert_eq!(board[0].rank, 1);
        assert_eq!(board[0].value, 1_000);
        assert_eq!(board[1].address, addr(1));
        assert_eq!(board[1].rank, 2);
        assert_eq!(board[2].address, addr(3));
        assert_eq!(board[2].rank, 3);
    }

    #[test]
    fn rebuild_volume_ordering() {
        let lb = Leaderboard::new(100);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 1_000, 5_000, 3),
            make_stats(3, -200, 20_000, 10),
        ];

        lb.rebuild(&stats, 1700000000);

        let board = lb.volume_board(10);
        assert_eq!(board[0].address, addr(3));
        assert_eq!(board[0].value, 20_000);
        assert_eq!(board[1].address, addr(1));
        assert_eq!(board[2].address, addr(2));
    }

    #[test]
    fn limit_entries() {
        let lb = Leaderboard::new(2);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 1_000, 5_000, 3),
            make_stats(3, -200, 20_000, 10),
        ];

        lb.rebuild(&stats, 100);

        let board = lb.pnl_board(10);
        assert_eq!(board.len(), 2);
    }

    #[test]
    fn limit_read() {
        let lb = Leaderboard::new(100);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 1_000, 5_000, 3),
            make_stats(3, -200, 20_000, 10),
        ];

        lb.rebuild(&stats, 100);

        let board = lb.pnl_board(1);
        assert_eq!(board.len(), 1);
        assert_eq!(board[0].address, addr(2));
    }

    #[test]
    fn filters_zero_trade_traders() {
        let lb = Leaderboard::new(100);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 0, 0, 0),
        ];

        lb.rebuild(&stats, 100);

        let board = lb.pnl_board(10);
        assert_eq!(board.len(), 1);
    }

    #[test]
    fn rank_lookup() {
        let lb = Leaderboard::new(100);
        let stats = vec![
            make_stats(1, 500, 10_000, 5),
            make_stats(2, 1_000, 5_000, 3),
        ];

        lb.rebuild(&stats, 100);

        assert_eq!(lb.pnl_rank(&addr(2)), Some(1));
        assert_eq!(lb.pnl_rank(&addr(1)), Some(2));
        assert!(lb.pnl_rank(&addr(99)).is_none());

        assert_eq!(lb.volume_rank(&addr(1)), Some(1));
        assert_eq!(lb.volume_rank(&addr(2)), Some(2));
    }

    #[test]
    fn last_rebuilt_at() {
        let lb = Leaderboard::new(100);
        lb.rebuild(&[make_stats(1, 100, 100, 1)], 42);
        assert_eq!(lb.pnl_last_rebuilt_at(), 42);
        assert_eq!(lb.volume_last_rebuilt_at(), 42);
    }

    #[test]
    fn rebuild_replaces_old_data() {
        let lb = Leaderboard::new(100);
        lb.rebuild(&[make_stats(1, 100, 100, 1)], 10);
        assert_eq!(lb.pnl_board(10).len(), 1);

        lb.rebuild(
            &[
                make_stats(2, 200, 200, 2),
                make_stats(3, 300, 300, 3),
            ],
            20,
        );

        let board = lb.pnl_board(10);
        assert_eq!(board.len(), 2);
        assert_eq!(board[0].address, addr(3));
        assert_eq!(lb.pnl_last_rebuilt_at(), 20);
    }

    #[test]
    fn max_entries_accessor() {
        let lb = Leaderboard::new(250);
        assert_eq!(lb.max_entries(), 250);
    }

    #[test]
    fn pnl_board_includes_trade_stats() {
        let lb = Leaderboard::new(100);
        lb.rebuild(&[make_stats(1, 500, 10_000, 8)], 100);

        let entry = &lb.pnl_board(1)[0];
        assert_eq!(entry.total_trades, 8);
        assert_eq!(entry.win_count, 4);
        assert_eq!(entry.loss_count, 4);
    }
}
