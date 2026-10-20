# PerfDB Architecture

## What This Replaces

The Sidiora off-chain infra currently uses **4 separate databases**, each with distinct responsibilities:

| DB | Role | Pain Points |
|---|---|---|
| **PostgreSQL** | Users, trader stats, positions (historical), orders (historical), referrals, competitions, alerts, API keys, daily stats, leaderboard materialized views, indexer checkpoint | Slow for high-frequency writes; materialized views need periodic REFRESH; no time-series optimization |
| **QuestDB** | Price ticks (`price_ticks`), pre-computed candles (`candles_history`), trade volume, funding ticks | Separate process; ILP ingestion + PG wire for reads; extra infra to maintain |
| **ScyllaDB** | Events by market, events by trader, trades by user, trades by market, funding history | Cassandra-style wide rows; complex deployment; overkill for this data volume |
| **DragonflyDB** (Redis) | Real-time state: open positions, active orders, orderbook sorted sets, latest prices, funding rates, user balances, pub/sub streaming | JSON serialization overhead; no durability guarantees; data lost on restart |

### Current Data Flow

```
Chain Events (Indexer)
  ├──> PostgreSQL  (user/stats/positions/orders — persistent)
  ├──> ScyllaDB    (event log + trade history — append-only)
  ├──> QuestDB     (price ticks + volume — time-series)
  └──> DragonflyDB (live state + pub/sub — cache)

Price Feeds (Pyth/HyperLiquid/Custom)
  ├──> QuestDB     (raw ticks + candle aggregation)
  └──> DragonflyDB (latest price + pub/sub)

API reads from all 4, WebSocket proxies DragonflyDB pub/sub.
```

---

## PerfDB: Unified Architecture

PerfDB replaces all four with a single embedded Rust database engine optimized for this exact workload.

### Design Principles

1. **Single process, zero network hops** — embedded library, not a separate server
2. **Hot/warm/cold tiering** — in-memory for real-time, mmap for recent, disk for archive
3. **Append-only time-series core** — ticks and events are immutable once written
4. **Pre-computed aggregations** — candles built in-process, not via external SAMPLE BY
5. **Lock-free hot path** — concurrent readers never block writers
6. **WAL for durability** — survives crashes without losing acknowledged writes
7. **Built-in pub/sub** — no external Redis needed for streaming
8. **Native Rust types** — no JSON serialization on the hot path

### Storage Engines (by access pattern)

```
PerfDB
├── TimeSeriesEngine        ← replaces QuestDB
│   ├── TickStore           (mmap append-only, per-market files)
│   ├── CandleStore         (fixed-record, per-market-per-timeframe)
│   └── FundingStore        (append-only funding rate history)
│
├── StateEngine             ← replaces DragonflyDB
│   ├── PositionTable       (in-memory HashMap, WAL-backed)
│   ├── OrderTable          (in-memory HashMap + sorted sets for orderbook)
│   ├── MarketState         (prices, funding rates, OI — atomic updates)
│   ├── BalanceCache        (per-user-per-token balances)
│   └── PubSubBus           (in-process broadcast channels)
│
├── RelationalEngine        ← replaces PostgreSQL
│   ├── UserStore           (B-tree indexed by address)
│   ├── TraderStatsStore    (in-memory with periodic flush)
│   ├── DailyStatsStore     (date-partitioned)
│   ├── ReferralStore       (relational lookups)
│   ├── CompetitionStore    (small dataset, in-memory)
│   └── CheckpointStore     (singleton, WAL-backed)
│
└── EventLogEngine          ← replaces ScyllaDB
    ├── EventsByMarket      (append-only, partitioned by market_id)
    ├── EventsByTrader      (append-only, partitioned by trader address)
    └── TradeLog            (append-only, dual-indexed)
```

### Data Type Mapping

| Current DB | Current Table/Key | PerfDB Engine | Storage |
|---|---|---|---|
| QuestDB | `price_ticks` | TimeSeriesEngine::TickStore | mmap append-only, 24B/tick |
| QuestDB | `candles_history` | TimeSeriesEngine::CandleStore | mmap fixed-record, 48B/candle |
| QuestDB | `funding_ticks` | TimeSeriesEngine::FundingStore | mmap append-only |
| QuestDB | `trade_volume` | TimeSeriesEngine (computed) | aggregated from EventLog |
| Dragonfly | `position:{id}` | StateEngine::PositionTable | in-memory + WAL |
| Dragonfly | `order:{id}` | StateEngine::OrderTable | in-memory + WAL |
| Dragonfly | `orderbook:{mid}:{side}` | StateEngine::OrderTable (sorted index) | in-memory |
| Dragonfly | `price:latest:{mid}` | StateEngine::MarketState | atomic in-memory |
| Dragonfly | `funding:rate:{mid}` | StateEngine::MarketState | atomic in-memory |
| Dragonfly | `user:balance:*` | StateEngine::BalanceCache | in-memory + WAL |
| Dragonfly | `user:positions:*` | StateEngine::PositionTable (index) | in-memory |
| Dragonfly | `user:orders:*` | StateEngine::OrderTable (index) | in-memory |
| Dragonfly | `markets:active` | StateEngine::MarketState | in-memory |
| Dragonfly | pub/sub `stream:*` | StateEngine::PubSubBus | tokio::broadcast |
| PostgreSQL | `users` | RelationalEngine::UserStore | mmap B-tree |
| PostgreSQL | `trader_stats` | RelationalEngine::TraderStatsStore | in-memory + periodic flush |
| PostgreSQL | `trader_daily_stats` | RelationalEngine::DailyStatsStore | date-partitioned files |
| PostgreSQL | `market_daily_stats` | RelationalEngine::DailyStatsStore | date-partitioned files |
| PostgreSQL | `protocol_daily_stats` | RelationalEngine::DailyStatsStore | date-partitioned files |
| PostgreSQL | `positions` | RelationalEngine (+ StateEngine) | WAL-backed + index files |
| PostgreSQL | `orders` | RelationalEngine (+ StateEngine) | WAL-backed + index files |
| PostgreSQL | `markets` | StateEngine::MarketState | in-memory + WAL |
| PostgreSQL | `referral_*` | RelationalEngine::ReferralStore | file-backed |
| PostgreSQL | `competitions` | RelationalEngine::CompetitionStore | in-memory |
| PostgreSQL | `indexer_state` | RelationalEngine::CheckpointStore | WAL singleton |
| PostgreSQL | `mv_leaderboard_*` | Computed in-memory (no mat views) | sorted vec, rebuilt on schedule |
| ScyllaDB | `events_by_market` | EventLogEngine::EventsByMarket | append-only partitioned files |
| ScyllaDB | `events_by_trader` | EventLogEngine::EventsByTrader | append-only partitioned files |
| ScyllaDB | `trades_by_user` | EventLogEngine::TradeLog | append-only, user-indexed |
| ScyllaDB | `trades_by_market` | EventLogEngine::TradeLog | append-only, market-indexed |
| ScyllaDB | `funding_history` | TimeSeriesEngine::FundingStore | append-only |

### Record Layouts (binary, no serde overhead on hot path)

```
PriceTick (24 bytes):
  timestamp_ns: u64    // nanosecond precision
  market_id:    u16    // up to 65k markets
  price:        f64    // IEEE 754
  _pad:         [u8;6] // alignment

Candle (48 bytes):
  timestamp_ns: u64
  market_id:    u16
  timeframe:    u8     // enum index
  _pad:         [u8;5]
  open:         f64
  high:         f64
  low:          f64
  close:        f64
  volume:       f64    // not available on all sources; 0.0 if unknown

Position (heap, WAL-serialized):
  position_id:       [u8; 32]   // hex string as bytes
  user_address:      [u8; 20]   // raw address bytes
  market_id:         u16
  is_long:           bool
  size_usd:          i128       // fixed-point 18 decimals
  collateral_usd:    i128
  collateral_token:  [u8; 20]
  collateral_amount: i128
  entry_price:       i128
  exit_price:        Option<i128>
  realized_pnl:      Option<i128>
  leverage:          i128
  status:            u8         // 0=open, 1=closed, 2=liquidated
  open_tx:           [u8; 32]
  close_tx:          Option<[u8; 32]>
  open_block:        u64
  close_block:       Option<u64>
  opened_at:         u64        // unix seconds
  closed_at:         Option<u64>
```

### Timeframes (15 total, expanded from current 11)

```
1s, 5s, 15s, 30s       ← new sub-minute (for scalpers)
1m, 2m, 3m, 5m, 15m, 30m
1h, 2h, 4h, 8h         ← 2h is new
1D, 1W
```

### Memory Architecture

```
┌─────────────────────────────────────────────────┐
│                   HOT TIER (RAM)                │
│                                                 │
│  MarketState[18]     ~2 KB   (prices, OI, etc) │
│  PositionTable       ~50 KB  (open positions)   │
│  OrderTable          ~30 KB  (active orders)    │
│  BalanceCache        ~20 KB  (user balances)    │
│  TraderStats         ~500 KB (all traders)      │
│  CandleBuffers       ~150 KB (18 mkts x 15 tf) │
│  LeaderboardCache    ~50 KB  (top 500)          │
│  PubSubBus           ~0      (channels only)    │
├─────────────────────────────────────────────────┤
│                  WARM TIER (mmap)               │
│                                                 │
│  TickStore           OS page cache handles it   │
│  CandleStore         recent candles stay mapped │
│  EventLog            recent events              │
│  WAL                 always mmap'd              │
├─────────────────────────────────────────────────┤
│                  COLD TIER (disk)               │
│                                                 │
│  Archived ticks      (older than 30 days)       │
│  Archived events     (older than 90 days)       │
│  Daily stats files   (all time)                 │
│  Historical positions/orders (closed)           │
└─────────────────────────────────────────────────┘
```

Total hot RAM: **< 1 MB** for current dataset. Scales linearly with active users/markets.

### Concurrency Model

```
Writers (single logical writer per engine):
  Indexer   ──> WAL ──> StateEngine + RelationalEngine + EventLog
  PriceFeed ──> WAL ──> TimeSeriesEngine (lock-free append)

Readers (unlimited concurrent, lock-free):
  API routes  ──> read snapshots from any engine
  WebSocket   ──> PubSubBus (tokio::broadcast, zero-copy)
  Background  ──> DailyStats aggregation, leaderboard rebuild
```

- **TickStore**: lock-free append via atomic file length counter + mmap
- **StateEngine**: `DashMap` for concurrent read/write (sharded RwLock)
- **CandleStore**: single writer (price feed), readers use mmap snapshots
- **WAL**: single writer, readers never touch it (only for crash recovery)

### Durability & Recovery

```
Write Path:
  1. Write to WAL (fsync configurable: every N ms or every write)
  2. Apply to in-memory state
  3. Acknowledge to caller
  4. Background: compact WAL segments periodically

Recovery:
  1. Load base snapshots from disk
  2. Replay WAL from last checkpoint
  3. Rebuild in-memory indexes
  4. Resume normal operation
```

### Pub/Sub (replaces DragonflyDB pub/sub)

```rust
// Channels (matching current DragonflyDB stream:* channels):
pub enum Channel {
    Prices,        // stream:prices
    Positions,     // stream:positions
    Orders,        // stream:orders
    Trades,        // stream:trades
    Liquidations,  // stream:liquidations
    Funding,       // stream:funding
    Balances,      // stream:balances
}
```

Uses `tokio::broadcast` channels internally. The API WebSocket handler subscribes
directly — no Redis client needed. Zero serialization on publish (subscribers get
typed enum variants, serialize to JSON only at the WS boundary).

---

## Project Structure

```
perf-db/
├── Cargo.toml
├── ARCHITECTURE.md          ← this file
├── src/
│   ├── lib.rs               # PerfDB public API (the crate interface)
│   ├── main.rs              # Optional standalone server binary
│   │
│   ├── core/
│   │   ├── mod.rs
│   │   ├── types.rs          # PriceTick, Candle, Position, Order, Trade, Event, User, etc.
│   │   ├── market.rs         # MarketId, MarketInfo, constants
│   │   └── timeframe.rs      # Timeframe enum (15 variants) + duration math
│   │
│   ├── storage/
│   │   ├── mod.rs
│   │   ├── wal.rs            # Write-ahead log (append-only, segment-based)
│   │   ├── mmap.rs           # Memory-mapped file abstraction (read/append/truncate)
│   │   ├── tick_store.rs     # Per-market append-only tick files
│   │   ├── candle_store.rs   # Per-market-per-timeframe fixed-record candle files
│   │   ├── event_store.rs    # Partitioned append-only event log
│   │   └── snapshot.rs       # Periodic state snapshots for fast recovery
│   │
│   ├── state/
│   │   ├── mod.rs
│   │   ├── positions.rs      # In-memory position table with indexes
│   │   ├── orders.rs         # In-memory order table + orderbook sorted sets
│   │   ├── market_state.rs   # Latest prices, funding rates, OI (atomic)
│   │   ├── balances.rs       # Per-user-per-token balance cache
│   │   └── pubsub.rs         # In-process broadcast pub/sub bus
│   │
│   ├── aggregation/
│   │   ├── mod.rs
│   │   ├── candle_builder.rs # Real-time tick → candle for all 15 timeframes
│   │   ├── stats.rs          # Trader stats, daily stats, protocol stats
│   │   └── leaderboard.rs    # Periodic leaderboard computation (replaces mat views)
│   │
│   ├── query/
│   │   ├── mod.rs
│   │   ├── time_range.rs     # Tick/candle range queries with binary search
│   │   ├── position_query.rs # Filter by user/market/status with pagination
│   │   ├── trade_query.rs    # Trade history by user or market
│   │   └── event_query.rs    # Event log queries
│   │
│   ├── relational/
│   │   ├── mod.rs
│   │   ├── users.rs          # User store (address → User)
│   │   ├── referrals.rs      # Referral code lookups, tier computation
│   │   ├── competitions.rs   # Competition entries
│   │   ├── alerts.rs         # User alerts
│   │   ├── api_keys.rs       # API key store
│   │   └── checkpoint.rs     # Indexer state (singleton)
│   │
│   └── engine.rs             # Top-level PerfDB engine (coordinates all sub-engines)
│
├── benches/
│   ├── tick_ingestion.rs     # Benchmark: tick write throughput
│   ├── candle_query.rs       # Benchmark: candle range queries
│   └── position_lookup.rs    # Benchmark: position by user
│
└── tests/
    ├── integration.rs        # Full engine lifecycle tests
    └── recovery.rs           # WAL crash recovery tests
```

---

## Implementation Plan

### Phase 1: Foundation (core + storage primitives)
1. `Cargo.toml` with dependencies
2. `core/types.rs` — all data types (binary-serializable)
3. `core/timeframe.rs` — 15 timeframes with duration math
4. `core/market.rs` — market ID constants and metadata
5. `storage/mmap.rs` — memory-mapped file abstraction
6. `storage/wal.rs` — write-ahead log with segment rotation

### Phase 2: Time-Series Engine
7. `storage/tick_store.rs` — per-market append-only tick storage
8. `storage/candle_store.rs` — pre-computed candle storage
9. `aggregation/candle_builder.rs` — real-time tick → candle for 15 TFs
10. `query/time_range.rs` — binary search range queries on ticks and candles

### Phase 3: State Engine (replaces DragonflyDB)
11. `state/market_state.rs` — atomic price/funding/OI state
12. `state/positions.rs` — in-memory position table with user/market indexes
13. `state/orders.rs` — order table + sorted orderbook sets
14. `state/balances.rs` — user balance cache
15. `state/pubsub.rs` — broadcast channel bus

### Phase 4: Relational + Event Log (replaces PostgreSQL + ScyllaDB)
16. `relational/users.rs` — user store with B-tree index
17. `relational/referrals.rs` — referral logic
18. `relational/checkpoint.rs` — indexer state
19. `storage/event_store.rs` — dual-partitioned event log
20. `aggregation/stats.rs` — trader stats, daily stats
21. `aggregation/leaderboard.rs` — in-memory leaderboard

### Phase 5: Engine Coordinator + Public API
22. `engine.rs` — top-level coordinator wiring all engines
23. `lib.rs` — public `PerfDB` struct with clean API surface
24. `storage/snapshot.rs` — periodic snapshots for fast recovery

### Phase 6: Integration + Benchmarks
25. Integration tests: full write/read lifecycle
26. Recovery tests: WAL replay after simulated crash
27. Benchmarks: tick ingestion, candle queries, position lookups
28. Migration guide: how to swap PerfDB into the existing services

---

## Performance Targets

| Operation | Target | Current (estimated) |
|---|---|---|
| Tick ingestion | > 2M ticks/sec | ~10K/sec (QuestDB ILP over TCP) |
| Latest price read | < 100ns | ~500us (Redis GET over TCP) |
| Candle query (1000 bars) | < 50us | ~5ms (QuestDB PG wire) |
| Position lookup by ID | < 200ns | ~300us (Redis GET over TCP) |
| Position list by user | < 1us | ~2ms (Redis SMEMBERS + N GETs) |
| Orderbook snapshot | < 500ns | ~1ms (Redis ZRANGEBYSCORE) |
| Pub/sub publish | < 100ns | ~200us (Redis PUBLISH over TCP) |
| WAL write + fsync | < 10us | N/A (no unified WAL currently) |
| Full recovery (100M ticks) | < 5s | N/A |

---

## Migration Strategy

PerfDB will be an **embedded library crate**. The existing services link against it:

```rust
// In services/shared/Cargo.toml:
[dependencies]
perfdb = { path = "../perfdb" }

// In indexer/main.rs — replaces 4 separate DB connections:
let db = PerfDB::open("/data/perfdb")?;
```

The migration is incremental:
1. **Phase A**: Add PerfDB as a write-through mirror (writes go to PerfDB + existing DBs)
2. **Phase B**: Switch reads from existing DBs to PerfDB, one endpoint at a time
3. **Phase C**: Remove old DB writes once PerfDB reads are validated
4. **Phase D**: Remove old DB dependencies entirely
