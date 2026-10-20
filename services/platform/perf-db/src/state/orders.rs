use std::collections::{BTreeMap, HashSet};

use dashmap::DashMap;
use parking_lot::RwLock;

use crate::core::market::{MarketId, MARKET_COUNT};
use crate::core::types::{FixedI128, Order, OrderStatus};

/// Per-market orderbook: sorted bids and asks by price.
///
/// Replaces DragonflyDB `orderbook:{mid}:{bids|asks}` ZSET.
/// Bids sorted descending (best = highest price).
/// Asks sorted ascending (best = lowest price).
#[derive(Debug, Default)]
pub struct Orderbook {
    /// Buy orders: price → set of order IDs at that price level.
    bids: BTreeMap<FixedI128, Vec<[u8; 32]>>,
    /// Sell orders: price → set of order IDs at that price level.
    asks: BTreeMap<FixedI128, Vec<[u8; 32]>>,
}

/// A single price level in the orderbook depth.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct DepthLevel {
    pub price: FixedI128,
    pub order_count: usize,
}

impl Orderbook {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn add_bid(&mut self, price: FixedI128, order_id: [u8; 32]) {
        self.bids.entry(price).or_default().push(order_id);
    }

    pub fn add_ask(&mut self, price: FixedI128, order_id: [u8; 32]) {
        self.asks.entry(price).or_default().push(order_id);
    }

    pub fn remove_bid(&mut self, price: FixedI128, order_id: &[u8; 32]) {
        if let Some(ids) = self.bids.get_mut(&price) {
            ids.retain(|id| id != order_id);
            if ids.is_empty() {
                self.bids.remove(&price);
            }
        }
    }

    pub fn remove_ask(&mut self, price: FixedI128, order_id: &[u8; 32]) {
        if let Some(ids) = self.asks.get_mut(&price) {
            ids.retain(|id| id != order_id);
            if ids.is_empty() {
                self.asks.remove(&price);
            }
        }
    }

    /// Best bid price (highest). O(log n).
    pub fn best_bid(&self) -> Option<FixedI128> {
        self.bids.keys().next_back().copied()
    }

    /// Best ask price (lowest). O(log n).
    pub fn best_ask(&self) -> Option<FixedI128> {
        self.asks.keys().next().copied()
    }

    /// Bid-ask spread: best_ask - best_bid. `None` if either side is empty.
    pub fn spread(&self) -> Option<FixedI128> {
        Some(self.best_ask()? - self.best_bid()?)
    }

    /// Top N bid levels (highest price first).
    pub fn bids_depth(&self, levels: usize) -> Vec<DepthLevel> {
        self.bids
            .iter()
            .rev()
            .take(levels)
            .map(|(&price, ids)| DepthLevel {
                price,
                order_count: ids.len(),
            })
            .collect()
    }

    /// Top N ask levels (lowest price first).
    pub fn asks_depth(&self, levels: usize) -> Vec<DepthLevel> {
        self.asks
            .iter()
            .take(levels)
            .map(|(&price, ids)| DepthLevel {
                price,
                order_count: ids.len(),
            })
            .collect()
    }

    /// Total number of bid orders.
    pub fn bid_count(&self) -> usize {
        self.bids.values().map(|v| v.len()).sum()
    }

    /// Total number of ask orders.
    pub fn ask_count(&self) -> usize {
        self.asks.values().map(|v| v.len()).sum()
    }
}

/// In-memory order table with secondary indexes and per-market orderbooks.
///
/// Replaces DragonflyDB keys:
/// - `order:{id}` → `get()`
/// - `user:orders:{addr}` → `by_user()`
/// - `orderbook:{mid}:{bids|asks}` → `orderbook(mid)`
///
/// Concurrency:
/// - DashMap for primary order storage and user index.
/// - `parking_lot::RwLock<Orderbook>` per market for the sorted sets.
pub struct OrderTable {
    orders: DashMap<[u8; 32], Order>,
    by_user: DashMap<[u8; 20], HashSet<[u8; 32]>>,
    orderbooks: Vec<RwLock<Orderbook>>,
}

impl OrderTable {
    pub fn new() -> Self {
        let orderbooks = (0..MARKET_COUNT).map(|_| RwLock::new(Orderbook::new())).collect();
        Self {
            orders: DashMap::new(),
            by_user: DashMap::new(),
            orderbooks,
        }
    }

    /// Insert or replace an order. If Active, adds to orderbook.
    ///
    /// Orderbook side: `is_long` → bid, `!is_long` → ask.
    pub fn insert(&self, order: Order) {
        let oid = order.order_id;
        let user = order.user_address;
        let market = order.market_id;
        let is_long = order.is_long;
        let price = order.trigger_price;
        let is_active = matches!(order.status, OrderStatus::Active);

        self.orders.insert(oid, order);

        self.by_user
            .entry(user)
            .or_insert_with(HashSet::new)
            .insert(oid);

        if is_active {
            if let Some(book) = self.orderbooks.get(market as usize) {
                let mut book = book.write();
                if is_long {
                    book.add_bid(price, oid);
                } else {
                    book.add_ask(price, oid);
                }
            }
        }
    }

    /// Get an order by ID. O(1).
    pub fn get(&self, order_id: &[u8; 32]) -> Option<Order> {
        self.orders.get(order_id).map(|r| r.clone())
    }

    /// Cancel an order: set status to Cancelled, remove from orderbook.
    pub fn cancel(&self, order_id: &[u8; 32]) -> Option<Order> {
        let mut order = self.orders.get_mut(order_id)?;
        let prev_status = order.status;
        order.status = OrderStatus::Cancelled;

        let market = order.market_id;
        let is_long = order.is_long;
        let price = order.trigger_price;
        let result = order.clone();
        drop(order);

        if matches!(prev_status, OrderStatus::Active) {
            self.remove_from_orderbook(market, is_long, price, order_id);
        }

        Some(result)
    }

    /// Execute an order: set status to Executed, record execution details,
    /// remove from orderbook.
    pub fn execute(
        &self,
        order_id: &[u8; 32],
        execution_price: FixedI128,
        position_id: [u8; 32],
        executed_at: u64,
    ) -> Option<Order> {
        let mut order = self.orders.get_mut(order_id)?;
        let prev_status = order.status;
        order.status = OrderStatus::Executed;
        order.execution_price = Some(execution_price);
        order.position_id = Some(position_id);
        order.executed_at = Some(executed_at);

        let market = order.market_id;
        let is_long = order.is_long;
        let price = order.trigger_price;
        let result = order.clone();
        drop(order);

        if matches!(prev_status, OrderStatus::Active) {
            self.remove_from_orderbook(market, is_long, price, order_id);
        }

        Some(result)
    }

    /// Remove an order completely (from storage and all indexes).
    pub fn remove(&self, order_id: &[u8; 32]) -> Option<Order> {
        let (_, order) = self.orders.remove(order_id)?;

        if let Some(mut set) = self.by_user.get_mut(&order.user_address) {
            set.remove(order_id);
            if set.is_empty() {
                drop(set);
                self.by_user.remove(&order.user_address);
            }
        }

        if matches!(order.status, OrderStatus::Active) {
            self.remove_from_orderbook(order.market_id, order.is_long, order.trigger_price, order_id);
        }

        Some(order)
    }

    /// All orders for a user (any status).
    pub fn by_user(&self, user_address: &[u8; 20]) -> Vec<Order> {
        match self.by_user.get(user_address) {
            Some(ids) => ids
                .iter()
                .filter_map(|oid| self.orders.get(oid).map(|r| r.clone()))
                .collect(),
            None => Vec::new(),
        }
    }

    /// Active orders for a user.
    pub fn active_by_user(&self, user_address: &[u8; 20]) -> Vec<Order> {
        self.by_user(user_address)
            .into_iter()
            .filter(|o| matches!(o.status, OrderStatus::Active))
            .collect()
    }

    /// Read-only access to a market's orderbook.
    pub fn orderbook(&self, market_id: MarketId) -> Option<parking_lot::RwLockReadGuard<'_, Orderbook>> {
        self.orderbooks.get(market_id as usize).map(|b| b.read())
    }

    /// Total orders in storage (all statuses).
    pub fn count(&self) -> usize {
        self.orders.len()
    }

    /// Number of active orders.
    pub fn active_count(&self) -> usize {
        self.orders
            .iter()
            .filter(|r| matches!(r.value().status, OrderStatus::Active))
            .count()
    }

    /// Check if an order exists.
    pub fn contains(&self, order_id: &[u8; 32]) -> bool {
        self.orders.contains_key(order_id)
    }

    /// All orders (for snapshots, diagnostics).
    pub fn all(&self) -> Vec<Order> {
        self.orders.iter().map(|r| r.value().clone()).collect()
    }

    fn remove_from_orderbook(
        &self,
        market_id: u16,
        is_long: bool,
        price: FixedI128,
        order_id: &[u8; 32],
    ) {
        if let Some(book) = self.orderbooks.get(market_id as usize) {
            let mut book = book.write();
            if is_long {
                book.remove_bid(price, order_id);
            } else {
                book.remove_ask(price, order_id);
            }
        }
    }
}

impl Default for OrderTable {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::types::OrderType;

    fn make_order(id: u8, user: u8, market: u16, is_long: bool, price: i128) -> Order {
        Order {
            order_id: {
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
            is_long,
            order_type: OrderType::Limit,
            trigger_price: price,
            limit_price: None,
            size_usd: 1_000_000_000_000_000_000_000,
            leverage: 10_000_000_000_000_000_000,
            collateral_token: [0u8; 20],
            collateral_amount: 100_000_000,
            status: OrderStatus::Active,
            execution_price: None,
            position_id: None,
            tx_hash: [0u8; 32],
            block_number: 1000,
            created_at: 1700000000,
            executed_at: None,
        }
    }

    fn oid(id: u8) -> [u8; 32] {
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
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));

        let got = table.get(&oid(1)).unwrap();
        assert_eq!(got.trigger_price, 66_000);
        assert!(got.is_long);
    }

    #[test]
    fn get_nonexistent() {
        let table = OrderTable::new();
        assert!(table.get(&oid(99)).is_none());
    }

    #[test]
    fn cancel_order() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));

        let cancelled = table.cancel(&oid(1)).unwrap();
        assert!(matches!(cancelled.status, OrderStatus::Cancelled));

        let book = table.orderbook(0).unwrap();
        assert!(book.best_bid().is_none());
    }

    #[test]
    fn execute_order() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));

        let exec = table.execute(&oid(1), 66_050, [42u8; 32], 1700001000).unwrap();
        assert!(matches!(exec.status, OrderStatus::Executed));
        assert_eq!(exec.execution_price, Some(66_050));
        assert_eq!(exec.position_id, Some([42u8; 32]));

        let book = table.orderbook(0).unwrap();
        assert!(book.best_bid().is_none());
    }

    #[test]
    fn remove_order() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));

        let removed = table.remove(&oid(1)).unwrap();
        assert_eq!(removed.trigger_price, 66_000);

        assert!(table.get(&oid(1)).is_none());
        assert!(table.by_user(&user(10)).is_empty());
    }

    #[test]
    fn by_user_index() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 10, 1, false, 68_000));
        table.insert(make_order(3, 20, 0, true, 65_000));

        let user10 = table.by_user(&user(10));
        assert_eq!(user10.len(), 2);

        let user20 = table.by_user(&user(20));
        assert_eq!(user20.len(), 1);
    }

    #[test]
    fn active_by_user() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 10, 1, false, 68_000));
        table.cancel(&oid(2));

        let active = table.active_by_user(&user(10));
        assert_eq!(active.len(), 1);
        assert_eq!(active[0].order_id, oid(1));
    }

    #[test]
    fn orderbook_bids_and_asks() {
        let table = OrderTable::new();

        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 20, 0, true, 65_500));
        table.insert(make_order(3, 30, 0, true, 66_500));
        table.insert(make_order(4, 40, 0, false, 67_000));
        table.insert(make_order(5, 50, 0, false, 67_500));

        let book = table.orderbook(0).unwrap();

        assert_eq!(book.best_bid(), Some(66_500));
        assert_eq!(book.best_ask(), Some(67_000));
        assert_eq!(book.spread(), Some(500));

        assert_eq!(book.bid_count(), 3);
        assert_eq!(book.ask_count(), 2);
    }

    #[test]
    fn orderbook_depth() {
        let table = OrderTable::new();

        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 20, 0, true, 65_500));
        table.insert(make_order(3, 30, 0, true, 66_000));
        table.insert(make_order(4, 40, 0, false, 67_000));
        table.insert(make_order(5, 50, 0, false, 67_500));

        let book = table.orderbook(0).unwrap();

        let bids = book.bids_depth(10);
        assert_eq!(bids.len(), 2);
        assert_eq!(bids[0].price, 66_000);
        assert_eq!(bids[0].order_count, 2);
        assert_eq!(bids[1].price, 65_500);
        assert_eq!(bids[1].order_count, 1);

        let asks = book.asks_depth(10);
        assert_eq!(asks.len(), 2);
        assert_eq!(asks[0].price, 67_000);
        assert_eq!(asks[1].price, 67_500);
    }

    #[test]
    fn cancel_removes_from_orderbook() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 20, 0, true, 65_000));

        table.cancel(&oid(1));

        let book = table.orderbook(0).unwrap();
        assert_eq!(book.bid_count(), 1);
        assert_eq!(book.best_bid(), Some(65_000));
    }

    #[test]
    fn count_and_active_count() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 20, 0, false, 67_000));
        table.cancel(&oid(2));

        assert_eq!(table.count(), 2);
        assert_eq!(table.active_count(), 1);
    }

    #[test]
    fn multi_market_orderbooks() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.insert(make_order(2, 20, 1, true, 3_400));

        let book0 = table.orderbook(0).unwrap();
        assert_eq!(book0.best_bid(), Some(66_000));

        let book1 = table.orderbook(1).unwrap();
        assert_eq!(book1.best_bid(), Some(3_400));
    }

    #[test]
    fn orderbook_empty_spread() {
        let book = Orderbook::new();
        assert!(book.best_bid().is_none());
        assert!(book.best_ask().is_none());
        assert!(book.spread().is_none());
    }

    #[test]
    fn remove_cleans_all_indexes() {
        let table = OrderTable::new();
        table.insert(make_order(1, 10, 0, true, 66_000));
        table.remove(&oid(1));

        assert_eq!(table.count(), 0);
        assert!(table.by_user(&user(10)).is_empty());

        let book = table.orderbook(0).unwrap();
        assert_eq!(book.bid_count(), 0);
    }
}
