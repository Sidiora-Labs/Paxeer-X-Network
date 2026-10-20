use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

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

fn make_tick(ts_ns: u64, market_id: u16, price: f64) -> PriceTick {
    PriceTick {
        timestamp_ns: ts_ns,
        market_id,
        _pad: [0; 6],
        price,
    }
}

fn tick_ingestion(c: &mut Criterion) {
    let mut group = c.benchmark_group("tick_ingestion");

    for count in [1_000u64, 10_000, 100_000] {
        group.throughput(Throughput::Elements(count));
        group.bench_with_input(
            BenchmarkId::from_parameter(count),
            &count,
            |b, &count| {
                b.iter_with_setup(
                    || {
                        let dir = tempfile::tempdir().unwrap();
                        let db = PerfDb::open(bench_config(dir.path())).unwrap();
                        (dir, db)
                    },
                    |(_dir, db)| {
                        for i in 0..count {
                            let mid = (i % 18) as u16;
                            db.ingest_tick(&make_tick(
                                i * 1_000_000,
                                mid,
                                67_000.0 + i as f64 * 0.01,
                            ))
                            .unwrap();
                        }
                    },
                );
            },
        );
    }
    group.finish();
}

fn tick_ingestion_single_market(c: &mut Criterion) {
    let mut group = c.benchmark_group("tick_ingestion_single_market");
    let count = 100_000u64;
    group.throughput(Throughput::Elements(count));

    group.bench_function("100k_btc", |b| {
        b.iter_with_setup(
            || {
                let dir = tempfile::tempdir().unwrap();
                let db = PerfDb::open(bench_config(dir.path())).unwrap();
                (dir, db)
            },
            |(_dir, db)| {
                for i in 0..count {
                    db.ingest_tick(&make_tick(
                        i * 1_000_000,
                        0,
                        67_000.0 + i as f64 * 0.01,
                    ))
                    .unwrap();
                }
            },
        );
    });
    group.finish();
}

criterion_group!(benches, tick_ingestion, tick_ingestion_single_market);
criterion_main!(benches);
