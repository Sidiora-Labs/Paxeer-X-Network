use std::collections::HashMap;

use parking_lot::RwLock;

use crate::core::types::IndexedEvent;

/// Dual-partitioned append-only event log.
///
/// Replaces ScyllaDB tables:
/// - `events_by_market` (partition: market_id)
/// - `events_by_trader` (partition: trader_address)
///
/// Events are stored in two parallel indexes. Each event is appended to both
/// the market partition and the trader partition. Within each partition,
/// events are sorted by (block_number, log_index) — natural insertion order.
///
/// Concurrency: `parking_lot::RwLock` on each partition. Single writer,
/// multiple concurrent readers.
pub struct EventStore {
    by_market: RwLock<HashMap<u16, Vec<IndexedEvent>>>,
    by_trader: RwLock<HashMap<[u8; 20], Vec<IndexedEvent>>>,
    total_count: std::sync::atomic::AtomicU64,
}

impl EventStore {
    pub fn new() -> Self {
        Self {
            by_market: RwLock::new(HashMap::new()),
            by_trader: RwLock::new(HashMap::new()),
            total_count: std::sync::atomic::AtomicU64::new(0),
        }
    }

    /// Append an event to both market and trader partitions.
    ///
    /// `market_id` and `trader_address` are extracted by the caller from the
    /// event variant (not all events have both — pass `None` to skip).
    pub fn append(
        &self,
        event: IndexedEvent,
        market_id: Option<u16>,
        trader_address: Option<[u8; 20]>,
    ) {
        if let Some(mid) = market_id {
            self.by_market
                .write()
                .entry(mid)
                .or_default()
                .push(event.clone());
        }

        if let Some(addr) = trader_address {
            self.by_trader
                .write()
                .entry(addr)
                .or_default()
                .push(event);
        }

        self.total_count
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }

    /// Query events for a market, optionally filtered by block range [from, to).
    pub fn by_market(
        &self,
        market_id: u16,
        from_block: Option<u64>,
        to_block: Option<u64>,
    ) -> Vec<IndexedEvent> {
        let guard = self.by_market.read();
        match guard.get(&market_id) {
            Some(events) => filter_by_block_range(events, from_block, to_block),
            None => Vec::new(),
        }
    }

    /// Query events for a trader address, optionally filtered by block range.
    pub fn by_trader(
        &self,
        trader: &[u8; 20],
        from_block: Option<u64>,
        to_block: Option<u64>,
    ) -> Vec<IndexedEvent> {
        let guard = self.by_trader.read();
        match guard.get(trader) {
            Some(events) => filter_by_block_range(events, from_block, to_block),
            None => Vec::new(),
        }
    }

    /// Latest N events for a market (most recent first).
    pub fn latest_by_market(&self, market_id: u16, count: usize) -> Vec<IndexedEvent> {
        let guard = self.by_market.read();
        match guard.get(&market_id) {
            Some(events) => {
                let start = events.len().saturating_sub(count);
                events[start..].iter().rev().cloned().collect()
            }
            None => Vec::new(),
        }
    }

    /// Latest N events for a trader (most recent first).
    pub fn latest_by_trader(&self, trader: &[u8; 20], count: usize) -> Vec<IndexedEvent> {
        let guard = self.by_trader.read();
        match guard.get(trader) {
            Some(events) => {
                let start = events.len().saturating_sub(count);
                events[start..].iter().rev().cloned().collect()
            }
            None => Vec::new(),
        }
    }

    /// Count events for a market.
    pub fn market_event_count(&self, market_id: u16) -> usize {
        self.by_market
            .read()
            .get(&market_id)
            .map_or(0, |v| v.len())
    }

    /// Count events for a trader.
    pub fn trader_event_count(&self, trader: &[u8; 20]) -> usize {
        self.by_trader.read().get(trader).map_or(0, |v| v.len())
    }

    /// Total events across all partitions (may double-count if both market and trader indexed).
    pub fn total_count(&self) -> u64 {
        self.total_count
            .load(std::sync::atomic::Ordering::Relaxed)
    }

    /// Number of unique markets with events.
    pub fn market_partition_count(&self) -> usize {
        self.by_market.read().len()
    }

    /// Number of unique traders with events.
    pub fn trader_partition_count(&self) -> usize {
        self.by_trader.read().len()
    }

    /// Query events for a market filtered by event type string.
    /// `event_type_fn` extracts a type name from the event for filtering.
    pub fn by_market_filtered(
        &self,
        market_id: u16,
        filter: impl Fn(&IndexedEvent) -> bool,
    ) -> Vec<IndexedEvent> {
        let guard = self.by_market.read();
        match guard.get(&market_id) {
            Some(events) => events.iter().filter(|e| filter(e)).cloned().collect(),
            None => Vec::new(),
        }
    }

    /// Query events for a trader filtered.
    pub fn by_trader_filtered(
        &self,
        trader: &[u8; 20],
        filter: impl Fn(&IndexedEvent) -> bool,
    ) -> Vec<IndexedEvent> {
        let guard = self.by_trader.read();
        match guard.get(trader) {
            Some(events) => events.iter().filter(|e| filter(e)).cloned().collect(),
            None => Vec::new(),
        }
    }
}

impl Default for EventStore {
    fn default() -> Self {
        Self::new()
    }
}

fn filter_by_block_range(
    events: &[IndexedEvent],
    from_block: Option<u64>,
    to_block: Option<u64>,
) -> Vec<IndexedEvent> {
    events
        .iter()
        .filter(|e| {
            if let Some(from) = from_block {
                if e.block_number < from {
                    return false;
                }
            }
            if let Some(to) = to_block {
                if e.block_number >= to {
                    return false;
                }
            }
            true
        })
        .cloned()
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::types::Event;

    fn addr(id: u8) -> [u8; 20] {
        let mut buf = [0u8; 20];
        buf[19] = id;
        buf
    }

    fn make_event(block: u64, market_id: u16, user_id: u8) -> IndexedEvent {
        IndexedEvent {
            block_number: block,
            block_timestamp: block * 12,
            tx_hash: {
                let mut buf = [0u8; 32];
                buf[31] = block as u8;
                buf
            },
            tx_index: 0,
            log_index: 0,
            event: Event::PositionOpened {
                position_id: [0u8; 32],
                user: addr(user_id),
                market_id,
                is_long: true,
                size_usd: 1_000_000_000_000_000_000_000,
                leverage: 10_000_000_000_000_000_000,
                entry_price: 67_000_000_000_000_000_000_000,
                collateral_token: [0u8; 20],
                collateral_amount: 100_000_000,
            },
        }
    }

    fn make_close_event(block: u64, market_id: u16, user_id: u8) -> IndexedEvent {
        IndexedEvent {
            block_number: block,
            block_timestamp: block * 12,
            tx_hash: {
                let mut buf = [0u8; 32];
                buf[31] = block as u8;
                buf
            },
            tx_index: 0,
            log_index: 0,
            event: Event::PositionClosed {
                position_id: [0u8; 32],
                user: addr(user_id),
                market_id,
                closed_size_usd: 500_000,
                exit_price: 68_000,
                realized_pnl: 1_000,
                is_full_close: true,
            },
        }
    }

    #[test]
    fn append_and_query_by_market() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 0, 2), Some(0), Some(addr(2)));
        store.append(make_event(102, 1, 1), Some(1), Some(addr(1)));

        let mkt0 = store.by_market(0, None, None);
        assert_eq!(mkt0.len(), 2);

        let mkt1 = store.by_market(1, None, None);
        assert_eq!(mkt1.len(), 1);

        let mkt99 = store.by_market(99, None, None);
        assert!(mkt99.is_empty());
    }

    #[test]
    fn append_and_query_by_trader() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 1, 1), Some(1), Some(addr(1)));
        store.append(make_event(102, 0, 2), Some(0), Some(addr(2)));

        let trader1 = store.by_trader(&addr(1), None, None);
        assert_eq!(trader1.len(), 2);

        let trader2 = store.by_trader(&addr(2), None, None);
        assert_eq!(trader2.len(), 1);
    }

    #[test]
    fn block_range_filter() {
        let store = EventStore::new();
        for block in 100..110 {
            store.append(make_event(block, 0, 1), Some(0), Some(addr(1)));
        }

        let filtered = store.by_market(0, Some(103), Some(107));
        assert_eq!(filtered.len(), 4);
        assert_eq!(filtered[0].block_number, 103);
        assert_eq!(filtered[3].block_number, 106);
    }

    #[test]
    fn block_range_from_only() {
        let store = EventStore::new();
        for block in 100..105 {
            store.append(make_event(block, 0, 1), Some(0), Some(addr(1)));
        }

        let filtered = store.by_market(0, Some(103), None);
        assert_eq!(filtered.len(), 2);
    }

    #[test]
    fn block_range_to_only() {
        let store = EventStore::new();
        for block in 100..105 {
            store.append(make_event(block, 0, 1), Some(0), Some(addr(1)));
        }

        let filtered = store.by_market(0, None, Some(103));
        assert_eq!(filtered.len(), 3);
    }

    #[test]
    fn latest_by_market() {
        let store = EventStore::new();
        for block in 100..110 {
            store.append(make_event(block, 0, 1), Some(0), Some(addr(1)));
        }

        let latest = store.latest_by_market(0, 3);
        assert_eq!(latest.len(), 3);
        assert_eq!(latest[0].block_number, 109);
        assert_eq!(latest[2].block_number, 107);
    }

    #[test]
    fn latest_by_trader() {
        let store = EventStore::new();
        for block in 100..105 {
            store.append(make_event(block, 0, 1), Some(0), Some(addr(1)));
        }

        let latest = store.latest_by_trader(&addr(1), 2);
        assert_eq!(latest.len(), 2);
        assert_eq!(latest[0].block_number, 104);
    }

    #[test]
    fn market_event_count() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 0, 2), Some(0), Some(addr(2)));

        assert_eq!(store.market_event_count(0), 2);
        assert_eq!(store.market_event_count(1), 0);
    }

    #[test]
    fn trader_event_count() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 1, 1), Some(1), Some(addr(1)));

        assert_eq!(store.trader_event_count(&addr(1)), 2);
        assert_eq!(store.trader_event_count(&addr(99)), 0);
    }

    #[test]
    fn total_count() {
        let store = EventStore::new();
        assert_eq!(store.total_count(), 0);

        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 0, 2), Some(0), Some(addr(2)));

        assert_eq!(store.total_count(), 2);
    }

    #[test]
    fn partition_counts() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_event(101, 1, 2), Some(1), Some(addr(2)));
        store.append(make_event(102, 0, 1), Some(0), Some(addr(1)));

        assert_eq!(store.market_partition_count(), 2);
        assert_eq!(store.trader_partition_count(), 2);
    }

    #[test]
    fn market_only_append() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), None);

        assert_eq!(store.market_event_count(0), 1);
        assert_eq!(store.trader_event_count(&addr(1)), 0);
    }

    #[test]
    fn trader_only_append() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), None, Some(addr(1)));

        assert_eq!(store.market_event_count(0), 0);
        assert_eq!(store.trader_event_count(&addr(1)), 1);
    }

    #[test]
    fn filtered_query() {
        let store = EventStore::new();
        store.append(make_event(100, 0, 1), Some(0), Some(addr(1)));
        store.append(make_close_event(101, 0, 1), Some(0), Some(addr(1)));

        let opens_only = store.by_market_filtered(0, |e| {
            matches!(e.event, Event::PositionOpened { .. })
        });
        assert_eq!(opens_only.len(), 1);

        let closes_only = store.by_trader_filtered(&addr(1), |e| {
            matches!(e.event, Event::PositionClosed { .. })
        });
        assert_eq!(closes_only.len(), 1);
    }

    #[test]
    fn empty_store_queries() {
        let store = EventStore::new();
        assert!(store.by_market(0, None, None).is_empty());
        assert!(store.by_trader(&addr(1), None, None).is_empty());
        assert!(store.latest_by_market(0, 10).is_empty());
        assert!(store.latest_by_trader(&addr(1), 10).is_empty());
    }
}
