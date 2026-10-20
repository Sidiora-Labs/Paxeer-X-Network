use criterion::{criterion_group, criterion_main, Criterion, Throughput};

use perfdb::core::types::PriceTick;
use perfdb::engine::PerfDbConfig;
use perfdb::storage::wal::{SyncPolicy, WalConfig};
use perfdb::PerfDb;

fn bench_config(dir: &std::path::Path) -> PerfDbConfig {
    PerfDbConfig {
        data_dir: dir.to_path_buf(),
        wal: WalConfig {
            dir: dir.join("wal"),
            max_segment_size: 256 * 1024 * 1024,
            sync_policy: SyncPolicy::None,
        },
        leaderboard_max: 100,
        pubsub_capacity: 64,
    }
}

fn latest_price_read(c: &mut Criterion) {
    let mut group = c.benchmark_group("latest_price");

    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(bench_config(dir.path())).unwrap();

    // Seed prices for all 18 markets
    for mid in 0..18u16 {
        db.ingest_tick(&PriceTick {
            timestamp_ns: 1_000_000_000,
            market_id: mid,
            _pad: [0; 6],
            price: 67_000.0 + mid as f64 * 100.0,
        })
        .unwrap();
    }

    group.throughput(Throughput::Elements(1));

    group.bench_function("single_market", |b| {
        b.iter(|| {
            let price = db.latest_price(0);
            criterion::black_box(price);
        });
    });

    group.bench_function("all_markets", |b| {
        b.iter(|| {
            let prices = db.all_prices();
            criterion::black_box(prices.len());
        });
    });

    group.bench_function("18_sequential_reads", |b| {
        b.iter(|| {
            for mid in 0..18u16 {
                criterion::black_box(db.latest_price(mid));
            }
        });
    });

    group.finish();
}

fn market_state_snapshot(c: &mut Criterion) {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(bench_config(dir.path())).unwrap();

    // Seed market state
    for mid in 0..18u16 {
        db.ingest_tick(&PriceTick {
            timestamp_ns: 1_000_000_000,
            market_id: mid,
            _pad: [0; 6],
            price: 67_000.0 + mid as f64 * 100.0,
        })
        .unwrap();
    }

    c.bench_function("market_snapshot", |b| {
        b.iter(|| {
            let snap = db.market_state.snapshot(0);
            criterion::black_box(snap);
        });
    });
}

fn funding_rate_read(c: &mut Criterion) {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(bench_config(dir.path())).unwrap();

    db.market_state.set_funding_rate(0, 500, 43_200_000);

    c.bench_function("funding_rate", |b| {
        b.iter(|| {
            let rate = db.market_state.funding_rate(0);
            criterion::black_box(rate);
        });
    });
}

criterion_group!(benches, latest_price_read, market_state_snapshot, funding_rate_read);
criterion_main!(benches);
