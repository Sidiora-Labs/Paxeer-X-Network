use std::collections::HashSet;

use dashmap::DashMap;

use crate::core::types::{Position, PositionStatus};

/// In-memory position table with secondary indexes.
///
/// Replaces DragonflyDB keys:
/// - `position:{id}` → `get()`
/// - `user:positions:{addr}` → `by_user()`
/// - `market:positions:{mid}` → `by_market()`
///
/// Concurrency: DashMap provides sharded RwLock — concurrent reads/writes
/// without external synchronization. Secondary indexes are eventually consistent
/// (nanosecond delay between primary insert and index update).
pub struct PositionTable {
    positions: DashMap<[u8; 32], Position>,
    by_user: DashMap<[u8; 20], HashSet<[u8; 32]>>,
    by_market: DashMap<u16, HashSet<[u8; 32]>>,
}

impl PositionTable {
    pub fn new() -> Self {
        Self {
            positions: DashMap::new(),
            by_user: DashMap::new(),
            by_market: DashMap::new(),
        }
    }

    /// Insert or replace a position. Updates all indexes.
    pub fn insert(&self, position: Position) {
        let pid = position.position_id;
        let user = position.user_address;
        let market = position.market_id;

        self.positions.insert(pid, position);

        self.by_user
            .entry(user)
            .or_insert_with(HashSet::new)
            .insert(pid);

        self.by_market
            .entry(market)
            .or_insert_with(HashSet::new)
            .insert(pid);
    }

    /// Get a position by ID. O(1).
    pub fn get(&self, position_id: &[u8; 32]) -> Option<Position> {
        self.positions.get(position_id).map(|r| r.clone())
    }

    /// Update a position in place. Returns `true` if the position exists.
    ///
    /// The callback receives a mutable reference under the DashMap shard lock.
    /// Keep the callback fast to avoid contention.
    pub fn update(&self, position_id: &[u8; 32], f: impl FnOnce(&mut Position)) -> bool {
        match self.positions.get_mut(position_id) {
            Some(mut entry) => {
                f(entry.value_mut());
                true
            }
            None => false,
        }
    }

    /// Remove a position and clean up all indexes.
    pub fn remove(&self, position_id: &[u8; 32]) -> Option<Position> {
        let (_, pos) = self.positions.remove(position_id)?;

        if let Some(mut set) = self.by_user.get_mut(&pos.user_address) {
            set.remove(position_id);
            if set.is_empty() {
                drop(set);
                self.by_user.remove(&pos.user_address);
            }
        }

        if let Some(mut set) = self.by_market.get_mut(&pos.market_id) {
            set.remove(position_id);
            if set.is_empty() {
                drop(set);
                self.by_market.remove(&pos.market_id);
            }
        }

        Some(pos)
    }

    /// All positions for a user (any status).
    pub fn by_user(&self, user_address: &[u8; 20]) -> Vec<Position> {
        self.collect_from_index(&self.by_user, user_address)
    }

    /// All positions for a market (any status).
    pub fn by_market(&self, market_id: u16) -> Vec<Position> {
        self.collect_from_index(&self.by_market, &market_id)
    }

    /// Open positions for a user.
    pub fn open_by_user(&self, user_address: &[u8; 20]) -> Vec<Position> {
        self.by_user(user_address)
            .into_iter()
            .filter(|p| matches!(p.status, PositionStatus::Open))
            .collect()
    }

    /// Open positions for a market.
    pub fn open_by_market(&self, market_id: u16) -> Vec<Position> {
        self.by_market(market_id)
            .into_iter()
            .filter(|p| matches!(p.status, PositionStatus::Open))
            .collect()
    }

    /// Total number of positions (all statuses).
    pub fn count(&self) -> usize {
        self.positions.len()
    }

    /// Number of open positions.
    pub fn open_count(&self) -> usize {
        self.positions
            .iter()
            .filter(|r| matches!(r.value().status, PositionStatus::Open))
            .count()
    }

    /// All positions (for iteration, diagnostics, snapshots).
    pub fn all(&self) -> Vec<Position> {
        self.positions.iter().map(|r| r.value().clone()).collect()
    }

    /// Check if a position exists.
    pub fn contains(&self, position_id: &[u8; 32]) -> bool {
        self.positions.contains_key(position_id)
    }

    fn collect_from_index<K: Eq + std::hash::Hash>(
        &self,
        index: &DashMap<K, HashSet<[u8; 32]>>,
        key: &K,
    ) -> Vec<Position> {
        match index.get(key) {
            Some(ids) => ids
                .iter()
                .filter_map(|pid| self.positions.get(pid).map(|r| r.clone()))
                .collect(),
            None => Vec::new(),
        }
    }
}

impl Default for PositionTable {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_position(id: u8, user: u8, market: u16, status: PositionStatus) -> Position {
        Position {
            position_id: {
                let mut buf = [0u8; 32];
                buf[31] = id;
                buf
            },
            user_address: {
                let mut buf = [0u8; 20];
                buf[19] = user;
                buf
            },
            market_id: market,
            is_long: true,
            size_usd: 1_000_000_000_000_000_000_000,
            collateral_usd: 100_000_000_000_000_000_000,
            collateral_token: [0u8; 20],
            collateral_amount: 100_000_000,
            entry_price: 67_000_000_000_000_000_000_000,
            exit_price: None,
            realized_pnl: None,
            leverage: 10_000_000_000_000_000_000,
            status,
            open_tx: [0u8; 32],
            close_tx: None,
            open_block: 1000,
            close_block: None,
            opened_at: 1700000000,
            closed_at: None,
        }
    }

    fn pid(id: u8) -> [u8; 32] {
        let mut buf = [0u8; 32];
        buf[31] = id;
        buf
    }

    fn user(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[19] = id;
        buf
    }

    #[test]
    fn insert_and_get() {
        let table = PositionTable::new();
        let pos = make_position(1, 10, 0, PositionStatus::Open);
        table.insert(pos.clone());

        let got = table.get(&pid(1)).unwrap();
        assert_eq!(got.position_id, pid(1));
        assert_eq!(got.market_id, 0);
        assert!(got.is_long);
    }

    #[test]
    fn get_nonexistent() {
        let table = PositionTable::new();
        assert!(table.get(&pid(99)).is_none());
    }

    #[test]
    fn update_position() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));

        let updated = table.update(&pid(1), |p| {
            p.status = PositionStatus::Closed;
            p.exit_price = Some(68_000_000_000_000_000_000_000);
            p.closed_at = Some(1700001000);
        });
        assert!(updated);

        let got = table.get(&pid(1)).unwrap();
        assert!(matches!(got.status, PositionStatus::Closed));
        assert_eq!(got.exit_price, Some(68_000_000_000_000_000_000_000));
    }

    #[test]
    fn update_nonexistent() {
        let table = PositionTable::new();
        let updated = table.update(&pid(99), |_| {});
        assert!(!updated);
    }

    #[test]
    fn remove_position() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));

        let removed = table.remove(&pid(1)).unwrap();
        assert_eq!(removed.position_id, pid(1));

        assert!(table.get(&pid(1)).is_none());
        assert!(table.by_user(&user(10)).is_empty());
        assert!(table.by_market(0).is_empty());
    }

    #[test]
    fn remove_nonexistent() {
        let table = PositionTable::new();
        assert!(table.remove(&pid(99)).is_none());
    }

    #[test]
    fn by_user_index() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 10, 1, PositionStatus::Open));
        table.insert(make_position(3, 20, 0, PositionStatus::Open));

        let user10 = table.by_user(&user(10));
        assert_eq!(user10.len(), 2);

        let user20 = table.by_user(&user(20));
        assert_eq!(user20.len(), 1);

        let user99 = table.by_user(&user(99));
        assert!(user99.is_empty());
    }

    #[test]
    fn by_market_index() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 20, 0, PositionStatus::Open));
        table.insert(make_position(3, 10, 1, PositionStatus::Open));

        let mkt0 = table.by_market(0);
        assert_eq!(mkt0.len(), 2);

        let mkt1 = table.by_market(1);
        assert_eq!(mkt1.len(), 1);
    }

    #[test]
    fn open_by_user_filters_status() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 10, 1, PositionStatus::Closed));
        table.insert(make_position(3, 10, 2, PositionStatus::Liquidated));

        let open = table.open_by_user(&user(10));
        assert_eq!(open.len(), 1);
        assert_eq!(open[0].position_id, pid(1));
    }

    #[test]
    fn open_by_market_filters_status() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 20, 0, PositionStatus::Closed));

        let open = table.open_by_market(0);
        assert_eq!(open.len(), 1);
        assert_eq!(open[0].position_id, pid(1));
    }

    #[test]
    fn count_and_open_count() {
        let table = PositionTable::new();
        assert_eq!(table.count(), 0);
        assert_eq!(table.open_count(), 0);

        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 20, 0, PositionStatus::Closed));
        table.insert(make_position(3, 30, 1, PositionStatus::Open));

        assert_eq!(table.count(), 3);
        assert_eq!(table.open_count(), 2);
    }

    #[test]
    fn insert_replaces_existing() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));

        let mut updated = make_position(1, 10, 0, PositionStatus::Closed);
        updated.exit_price = Some(68_000);
        table.insert(updated);

        assert_eq!(table.count(), 1);
        let got = table.get(&pid(1)).unwrap();
        assert!(matches!(got.status, PositionStatus::Closed));
    }

    #[test]
    fn all_positions() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.insert(make_position(2, 20, 1, PositionStatus::Open));

        let all = table.all();
        assert_eq!(all.len(), 2);
    }

    #[test]
    fn contains() {
        let table = PositionTable::new();
        assert!(!table.contains(&pid(1)));

        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        assert!(table.contains(&pid(1)));
    }

    #[test]
    fn remove_cleans_indexes_fully() {
        let table = PositionTable::new();
        table.insert(make_position(1, 10, 0, PositionStatus::Open));
        table.remove(&pid(1));

        assert!(table.by_user(&user(10)).is_empty());
        assert!(table.by_market(0).is_empty());
        assert_eq!(table.count(), 0);
    }
}
