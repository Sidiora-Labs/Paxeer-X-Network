use tokio::sync::broadcast;

use crate::core::types::{Balance, FixedI128, Order, Position, Trade};

const CHANNEL_COUNT: usize = 7;
const DEFAULT_CAPACITY: usize = 4096;

/// Pub/sub channel identifiers matching DragonflyDB stream:* channels.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum Channel {
    Prices = 0,
    Positions = 1,
    Orders = 2,
    Trades = 3,
    Liquidations = 4,
    Funding = 5,
    Balances = 6,
}

impl Channel {
    pub const ALL: [Channel; CHANNEL_COUNT] = [
        Self::Prices,
        Self::Positions,
        Self::Orders,
        Self::Trades,
        Self::Liquidations,
        Self::Funding,
        Self::Balances,
    ];

    pub const fn as_str(&self) -> &'static str {
        match self {
            Self::Prices => "stream:prices",
            Self::Positions => "stream:positions",
            Self::Orders => "stream:orders",
            Self::Trades => "stream:trades",
            Self::Liquidations => "stream:liquidations",
            Self::Funding => "stream:funding",
            Self::Balances => "stream:balances",
        }
    }
}

/// Typed message variants for the pub/sub bus.
///
/// Subscribers receive these directly — no JSON serialization on the hot path.
/// Serialize to JSON only at the WebSocket boundary.
#[derive(Debug, Clone)]
pub enum PubSubMessage {
    PriceUpdate {
        market_id: u16,
        price: f64,
        timestamp_ns: u64,
    },
    PositionUpdate(Box<Position>),
    OrderUpdate(Box<Order>),
    TradeExecuted(Box<Trade>),
    LiquidationEvent {
        position_id: [u8; 32],
        user: [u8; 20],
        market_id: u16,
        liquidation_price: FixedI128,
        penalty: FixedI128,
    },
    FundingUpdate {
        market_id: u16,
        rate_per_second: FixedI128,
        rate_24h: FixedI128,
    },
    BalanceUpdate(Box<Balance>),
}

/// In-process broadcast pub/sub bus. Replaces DragonflyDB PUBLISH/SUBSCRIBE.
///
/// Each channel is a `tokio::broadcast` sender. Subscribers receive typed enum
/// variants directly — zero serialization overhead. The API WebSocket handler
/// subscribes via `subscribe(channel)` and serializes to JSON only at the
/// network boundary.
///
/// Backpressure: if a subscriber falls behind by more than `capacity` messages,
/// it receives a `RecvError::Lagged(n)` and skips ahead. This is acceptable
/// for real-time streaming (stale data is worse than no data).
pub struct PubSubBus {
    senders: [broadcast::Sender<PubSubMessage>; CHANNEL_COUNT],
}

impl PubSubBus {
    /// Create a bus with the default capacity (4096 messages per channel).
    pub fn new() -> Self {
        Self::with_capacity(DEFAULT_CAPACITY)
    }

    /// Create a bus with a custom per-channel capacity.
    pub fn with_capacity(capacity: usize) -> Self {
        let senders = std::array::from_fn(|_| broadcast::channel(capacity).0);
        Self { senders }
    }

    /// Publish a message to a channel. Returns the number of active receivers
    /// that will receive this message.
    ///
    /// If there are no subscribers, the message is silently dropped (no error).
    /// This matches DragonflyDB PUBLISH behavior.
    pub fn publish(&self, channel: Channel, message: PubSubMessage) -> usize {
        self.senders[channel as usize].send(message).unwrap_or(0)
    }

    /// Subscribe to a channel. Returns a receiver that yields `PubSubMessage`.
    pub fn subscribe(&self, channel: Channel) -> broadcast::Receiver<PubSubMessage> {
        self.senders[channel as usize].subscribe()
    }

    /// Number of active subscribers on a channel.
    pub fn receiver_count(&self, channel: Channel) -> usize {
        self.senders[channel as usize].receiver_count()
    }
}

impl Default for PubSubBus {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn publish_subscribe_prices() {
        let bus = PubSubBus::new();
        let mut rx = bus.subscribe(Channel::Prices);

        let sent = bus.publish(
            Channel::Prices,
            PubSubMessage::PriceUpdate {
                market_id: 0,
                price: 67_000.0,
                timestamp_ns: 1_000_000,
            },
        );
        assert_eq!(sent, 1);

        let msg = rx.recv().await.unwrap();
        match msg {
            PubSubMessage::PriceUpdate { market_id, price, .. } => {
                assert_eq!(market_id, 0);
                assert_eq!(price, 67_000.0);
            }
            _ => panic!("wrong message type"),
        }
    }

    #[tokio::test]
    async fn multiple_subscribers() {
        let bus = PubSubBus::new();
        let mut rx1 = bus.subscribe(Channel::Prices);
        let mut rx2 = bus.subscribe(Channel::Prices);

        assert_eq!(bus.receiver_count(Channel::Prices), 2);

        let sent = bus.publish(
            Channel::Prices,
            PubSubMessage::PriceUpdate {
                market_id: 1,
                price: 3_500.0,
                timestamp_ns: 2_000_000,
            },
        );
        assert_eq!(sent, 2);

        let m1 = rx1.recv().await.unwrap();
        let m2 = rx2.recv().await.unwrap();

        match (&m1, &m2) {
            (
                PubSubMessage::PriceUpdate { market_id: id1, .. },
                PubSubMessage::PriceUpdate { market_id: id2, .. },
            ) => {
                assert_eq!(*id1, 1);
                assert_eq!(*id2, 1);
            }
            _ => panic!("wrong message types"),
        }
    }

    #[tokio::test]
    async fn channel_isolation() {
        let bus = PubSubBus::new();
        let mut rx_prices = bus.subscribe(Channel::Prices);
        let mut rx_trades = bus.subscribe(Channel::Trades);

        bus.publish(
            Channel::Prices,
            PubSubMessage::PriceUpdate {
                market_id: 0,
                price: 67_000.0,
                timestamp_ns: 1_000,
            },
        );

        let msg = rx_prices.recv().await.unwrap();
        assert!(matches!(msg, PubSubMessage::PriceUpdate { .. }));

        assert!(rx_trades.try_recv().is_err());
    }

    #[test]
    fn publish_no_subscribers_returns_zero() {
        let bus = PubSubBus::new();
        let sent = bus.publish(
            Channel::Prices,
            PubSubMessage::PriceUpdate {
                market_id: 0,
                price: 67_000.0,
                timestamp_ns: 1_000,
            },
        );
        assert_eq!(sent, 0);
    }

    #[tokio::test]
    async fn position_update_message() {
        use crate::core::types::{Position, PositionStatus};

        let bus = PubSubBus::new();
        let mut rx = bus.subscribe(Channel::Positions);

        let pos = Position {
            position_id: [1u8; 32],
            user_address: [2u8; 20],
            market_id: 0,
            is_long: true,
            size_usd: 1_000_000_000_000_000_000_000,
            collateral_usd: 100_000_000_000_000_000_000,
            collateral_token: [3u8; 20],
            collateral_amount: 100_000_000,
            entry_price: 67_000_000_000_000_000_000_000,
            exit_price: None,
            realized_pnl: None,
            leverage: 10_000_000_000_000_000_000,
            status: PositionStatus::Open,
            open_tx: [4u8; 32],
            close_tx: None,
            open_block: 1000,
            close_block: None,
            opened_at: 1700000000,
            closed_at: None,
        };

        bus.publish(Channel::Positions, PubSubMessage::PositionUpdate(Box::new(pos.clone())));

        let msg = rx.recv().await.unwrap();
        match msg {
            PubSubMessage::PositionUpdate(p) => {
                assert_eq!(p.market_id, 0);
                assert!(p.is_long);
            }
            _ => panic!("wrong message type"),
        }
    }

    #[tokio::test]
    async fn lagged_subscriber_recovers() {
        let bus = PubSubBus::with_capacity(4);
        let mut rx = bus.subscribe(Channel::Prices);

        for i in 0..10u64 {
            bus.publish(
                Channel::Prices,
                PubSubMessage::PriceUpdate {
                    market_id: 0,
                    price: 60_000.0 + i as f64,
                    timestamp_ns: i * 1_000,
                },
            );
        }

        match rx.recv().await {
            Ok(_) => {}
            Err(broadcast::error::RecvError::Lagged(n)) => {
                assert!(n > 0);
                let msg = rx.recv().await.unwrap();
                assert!(matches!(msg, PubSubMessage::PriceUpdate { .. }));
            }
            Err(e) => panic!("unexpected error: {e}"),
        }
    }

    #[test]
    fn all_channels_listed() {
        assert_eq!(Channel::ALL.len(), CHANNEL_COUNT);
        for (i, ch) in Channel::ALL.iter().enumerate() {
            assert_eq!(*ch as usize, i);
        }
    }

    #[test]
    fn channel_names() {
        assert_eq!(Channel::Prices.as_str(), "stream:prices");
        assert_eq!(Channel::Balances.as_str(), "stream:balances");
    }
}
