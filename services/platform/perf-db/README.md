<div align="center">

<img src="https://cdn.redixusercontent.ocfstudio.com/perf_db.png" alt="PerfDB" width="500" />

<h3>Embedded Rust database engine for high-frequency trading systems</h3>

<p>
Time series storage · real-time state · relational data · event sourcing unified into a single in process library, replacing a four database stack with<br/>
<strong>zero network hops</strong> and <strong>native Rust types on the hot path</strong>.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Project-PerfDB-FFFFFF?style=for-the-badge&amp;labelColor=004CED" alt="Project: PerfDB" />
  <img src="https://img.shields.io/badge/Built_by-Sidiora_Labs-004CED?style=for-the-badge&amp;labelColor=000000" alt="Built by Sidiora Labs" />
  <a href="#license"><img src="https://img.shields.io/badge/License-MIT_OR_Apache--2.0-004CED?style=for-the-badge&amp;labelColor=000000" alt="License: MIT OR Apache-2.0" /></a>
  <img src="https://img.shields.io/badge/Status-Pre--release-FF6B35?style=for-the-badge&amp;labelColor=000000" alt="Status: Pre-release" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Rust-1.75%2B-004CED?style=for-the-badge&amp;labelColor=000000&amp;logo=rust&amp;logoColor=white" alt="Rust 1.75+" />
  <img src="https://img.shields.io/badge/Engine-Zero_Network_Hops-00C896?style=for-the-badge&amp;labelColor=000000" alt="Zero network hops" />
  <img src="https://img.shields.io/badge/Concurrency-Lock--free-004CED?style=for-the-badge&amp;labelColor=000000" alt="Lock-free reads" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Latest_Read-1.1ns-00C896?style=for-the-badge&amp;labelColor=000000" alt="Latest read: 1.1ns" />
  <img src="https://img.shields.io/badge/Candle_Query-54--95ns-00C896?style=for-the-badge&amp;labelColor=000000" alt="Candle range query: 54-95ns" />
  <img src="https://img.shields.io/badge/Tick_Ingest-~1.6M%2Fsec-00C896?style=for-the-badge&amp;labelColor=000000" alt="Tick ingestion: ~1.6M/sec" />
</p>

</div>

---

## Table of Contents

- [Overview](#overview)
- [Performance](#performance)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Usage](#usage)
- [Supported Timeframes](#supported-timeframes)
- [Durability and Recovery](#durability-and-recovery)
- [Benchmarks](#benchmarks)
- [Project Status](#project-status)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)
- [Related](#related)

## Overview

PerfDB is a purpose-built, embedded Rust database engine for high-frequency
trading (HFT) and DeFi infrastructure. It consolidates four separate
database systems into a single, zero-copy, lock-free engine optimized for
the access patterns of a trading platform: append-only price ticks,
pre-aggregated candles, hot in-memory state, and an immutable event log.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              BEFORE: 4 DATABASES                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  PostgreSQL     QuestDB        ScyllaDB         DragonflyDB                 │
│  (relational)   (time-series)  (event log)      (real-time cache)           │
│       ↓              ↓              ↓                 ↓                     │
│   Network hop    Network hop    Network hop      Network hop                │
│   + JSON ser.    + ILP proto    + CQL driver     + RESP proto               │
└─────────────────────────────────────────────────────────────────────────────┘
                                    ↓
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AFTER: PerfDB                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                     Single embedded library                                 │
│                     Zero network hops                                       │
│                     Native Rust types (no serialization)                    │
│                     Lock-free concurrent reads                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

| Challenge | Traditional approach | PerfDB |
|---|---|---|
| Network latency | ~500µs per Redis `GET` over TCP | <100ns direct memory access |
| Serialization overhead | JSON encode/decode per operation | Zero-copy native Rust types |
| Operational complexity | Four databases to deploy and scale | Single embedded library |
| Data durability | In-memory cache lost on restart | WAL-backed with crash recovery |
| Time-series performance | External `SAMPLE BY` queries | Pre-computed candles in-process |
| Pub/sub | Separate Redis connection | Built-in `tokio::broadcast` channels |

## Performance

| Operation | Measured | Target | Legacy stack (typical) |
|---|---|---|---|
| Latest price read | 1.1ns | <100ns | ~500µs (Redis `GET`) |
| Candle range query | 54–95ns | <50µs | ~5ms (QuestDB PG wire) |
| Position lookup by ID | 97–102ns | <200ns | ~300µs (Redis `GET`) |
| Position list by user | ~1.6µs | <1µs | ~2ms (Redis `SMEMBERS`) |
| Tick ingestion, multi-market | ~1M/sec | >2M/sec | ~10K/sec (QuestDB ILP) |
| Tick ingestion, single-market | ~1.6M/sec | >2M/sec | ~10K/sec (QuestDB ILP) |

Run `cargo bench` to reproduce these numbers on your own hardware; see
[Benchmarks](#benchmarks) for details. Figures above were captured on a
commodity VPS without NVMe storage — expect better throughput on
dedicated hardware.

## Architecture

PerfDB is organized into four specialized storage engines, each replacing a component of the legacy stack:

```
PerfDB
├── TimeSeriesEngine        ← replaces QuestDB
│   ├── TickStore           (mmap append-only, per-market files)
│   ├── CandleStore         (fixed-record, per-market-per-timeframe)
│   └── FundingStore        (append-only funding rate history)
│
├── StateEngine             ← replaces DragonflyDB/Redis
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

## Quick Start

```rust
use perfdb::{PerfDb, PerfDbConfig};
use perfdb::core::types::PriceTick;

fn main() -> Result<(), perfdb::PerfDbError> {
    let config = PerfDbConfig::new("/data/perfdb");
    let db = PerfDb::open(config)?;

    db.ingest_tick(&PriceTick {
        timestamp_ns: 1_699_900_000_000_000_000,
        market_id: 1, // BTC
        price: 43_250.50,
    })?;

    let price = db.latest_price(1);
    let recent_ticks = db.ticks(1);

    db.shutdown()?;
    Ok(())
}
```

## Installation

PerfDB is not yet published to crates.io. Use it as a path or git dependency:

```toml
[dependencies]
perfdb = { git = "https://github.com/Sidiora-Labs/perf-db" }
```

### From source

```bash
git clone https://github.com/Sidiora-Labs/perf-db.git
cd perf-db
cargo build --release
```

### System requirements

- Rust 1.75 or newer (stable)
- Linux (kernel 4.15+) or macOS 10.15+
- SSD recommended for WAL and mmap performance

## Usage

### Opening a database

```rust
use perfdb::{PerfDb, PerfDbConfig};

let config = PerfDbConfig::new("/data/perfdb");
let db = PerfDb::open(config)?;
```

`PerfDb::open` loads the latest snapshot (if any), replays WAL entries
written after that snapshot's checkpoint, and opens the mmap-backed tick
and candle stores. See [Durability and Recovery](#durability-and-recovery).

### Time-series

```rust
db.ingest_tick(&tick)?;

let candles = db.candles(market_id, Timeframe::Min15);
let live = db.live_candle(market_id, Timeframe::Min15);
let price = db.latest_price(market_id);
```

### Real-time state

```rust
let position = db.positions.get(&position_id);
let user_positions = db.positions.by_user(&address);
let orderbook = db.orders.orderbook(market_id);
let balance = db.balances.get(&address, &token);
```

### Events

```rust
use perfdb::core::types::IndexedEvent;

db.process_event(&indexed_event)?;
let by_market = db.events.by_market(market_id, None, None);
let by_trader = db.events.by_trader(&address, None, None);
```

`process_event` writes to the WAL, appends to the event log, dispatches
the event to update positions/orders/balances/market state, and advances
the checkpoint.

### Pub/sub

```rust
use perfdb::state::pubsub::Channel;

let mut rx = db.pubsub.subscribe(Channel::Prices);
while let Ok(msg) = rx.recv().await {
    // handle PubSubMessage
}
```

### Leaderboards and stats

```rust
db.rebuild_leaderboard(now_ts);
let pnl_board = db.pnl_leaderboard(100);
let volume_board = db.volume_leaderboard(100);
```

### Shutdown

```rust
db.shutdown()?; // flushes candle builder, syncs WAL, writes final snapshot
```

## Supported Timeframes

PerfDB supports 16 candle timeframes, from sub-minute to weekly:

| Category | Variants |
|---|---|
| Sub-minute | `Sec1`, `Sec5`, `Sec15`, `Sec30` |
| Minute | `Min1`, `Min2`, `Min3`, `Min5`, `Min15`, `Min30` |
| Hourly | `Hour1`, `Hour2`, `Hour4`, `Hour8` |
| Daily+ | `Day1`, `Week1` |

```rust
use perfdb::core::timeframe::Timeframe;

let candles = db.candles(market_id, Timeframe::Min15);
```

## Durability and Recovery

**Write path**: every mutation is appended to the write-ahead log before it
is applied to in-memory state, so an acknowledged write survives a crash.

**Recovery on `PerfDb::open`**:

1. Load the most recent snapshot, if one exists.
2. Restore all stores from that snapshot.
3. Replay WAL entries written after the snapshot's checkpoint.
4. Seed the candle builder from the latest stored candles.

`PerfDb::snapshot()` can be called periodically to bound WAL replay time;
`PerfDb::shutdown()` flushes the candle builder, syncs the WAL, and writes
a final snapshot. Recovery behavior is covered by `tests/recovery.rs`,
including truncated and CRC-corrupted WAL segments.

## Benchmarks

```bash
cargo bench                        # full suite
cargo bench --bench tick_ingestion
cargo bench --bench candle_query
cargo bench --bench position_lookup
cargo bench --bench price_read
```

Results are written to `target/criterion/` with HTML reports. See
[Performance](#performance) for the latest recorded numbers.

## Project Status

PerfDB is pre-release. The on-disk WAL and snapshot formats, and the public
API, may change without a deprecation period before 1.0. It is being
developed as the storage backend for the Sidiora perpetual futures
platform's off-chain infrastructure; see `ARCHITECTURE.md` for the full
design rationale and the systems it replaces.

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for
setup instructions, code standards, and the PR process.

```bash
git clone https://github.com/Sidiora-Labs/perf-db.git
cd perf-db
rustup component add clippy rustfmt
cargo test
cargo clippy --all-targets --all-features -- -D warnings
cargo fmt
```

## Security

See [`SECURITY.md`](SECURITY.md) for the vulnerability disclosure process.

## License

Licensed under either of:

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE) or <http://www.apache.org/licenses/LICENSE-2.0>)
- MIT License ([LICENSE-MIT](LICENSE-MIT) or <http://opensource.org/licenses/MIT>)

at your option.

### Acknowledgments

- [`memmap2`](https://crates.io/crates/memmap2) — memory-mapped file I/O
- [`tokio`](https://tokio.rs/) — async runtime and broadcast channels
- [`dashmap`](https://crates.io/crates/dashmap) / [`parking_lot`](https://crates.io/crates/parking_lot) — concurrent data structures
- [`criterion`](https://crates.io/crates/criterion) — benchmarking framework

## Related

- [Paxeer Network](https://paxeer.app) — Sovereign L1 (Chain ID 125), 400ms blocks and finality, purpose-built for high-frequency and agentic workloads.

---

<p align="center">
  <em>Four databases. One library. Zero network hops.</em>
</p>

<p align="center">
  Built by <strong>Sidiora Labs</strong>
</p>

<p align="center">
  <sub>SPDX-License-Identifier: MIT OR Apache-2.0</sub>
</p>