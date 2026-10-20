use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use perfdb::core::timeframe::Timeframe;
use perfdb::core::types::{CandleRecord, PriceTick};
use perfdb::engine::PerfDbConfig;
use perfdb::query::time_range;
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

fn make_candles(count: usize, interval_ns: u64) -> Vec<CandleRecord> {
    (0..count)
        .map(|i| CandleRecord {
            timestamp_ns: i as u64 * interval_ns,
            open: 60_000.0 + i as f64,
            high: 60_001.0 + i as f64,
            low: 59_999.0 + i as f64,
            close: 60_000.5 + i as f64,
            volume: 100.0,
            market_id: 0,
            timeframe: Timeframe::Min1 as u8,
            _pad: 0,
        })
        .collect()
}

fn candle_range_query(c: &mut Criterion) {
    let mut group = c.benchmark_group("candle_range_query");

    for dataset_size in [1_000usize, 10_000, 100_000] {
        let candles = make_candles(dataset_size, 60_000_000_000);
        let mid = dataset_size / 2;
        let start_ns = mid as u64 * 60_000_000_000;
        let end_ns = (mid + 1000).min(dataset_size) as u64 * 60_000_000_000;

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::new("range_1000", dataset_size),
            &candles,
            |b, candles| {
                b.iter(|| {
                    let result = time_range::query_candle_range(candles, start_ns, end_ns);
                    criterion::black_box(result.len());
                });
            },
        );
    }
    group.finish();
}

fn candle_last_n(c: &mut Criterion) {
    let mut group = c.benchmark_group("candle_last_n");
    let candles = make_candles(100_000, 60_000_000_000);

    for n in [100usize, 500, 1000] {
        group.bench_with_input(
            BenchmarkId::from_parameter(n),
            &n,
            |b, &n| {
                b.iter(|| {
                    let result = time_range::query_last_candles(&candles, n);
                    criterion::black_box(result.len());
                });
            },
        );
    }
    group.finish();
}

fn candle_find_at(c: &mut Criterion) {
    let candles = make_candles(100_000, 60_000_000_000);
    let target = 50_000u64 * 60_000_000_000 + 30_000_000_000; // mid-bucket

    c.bench_function("candle_find_at_100k", |b| {
        b.iter(|| {
            let result = time_range::find_candle_at(&candles, target);
            criterion::black_box(result);
        });
    });
}

fn candle_query_through_engine(c: &mut Criterion) {
    let mut group = c.benchmark_group("candle_query_engine");

    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(bench_config(dir.path())).unwrap();

    // Generate enough ticks to produce ~500 1-second candles
    for i in 0..1_500u64 {
        let ts_ns = i * 1_000_000_000;
        db.ingest_tick(&PriceTick {
            timestamp_ns: ts_ns,
            market_id: 0,
            _pad: [0; 6],
            price: 67_000.0 + (i as f64 * 0.1),
        })
        .unwrap();
    }

    let candle_count = db.candle_count(0, Timeframe::Sec1);
    group.throughput(Throughput::Elements(candle_count.min(100)));

    group.bench_function("candles_sec1", |b| {
        b.iter(|| {
            let candles = db.candles(0, Timeframe::Sec1);
            criterion::black_box(candles.len());
        });
    });

    group.bench_function("live_candle", |b| {
        b.iter(|| {
            let candle = db.live_candle(0, Timeframe::Sec1);
            criterion::black_box(candle);
        });
    });

    group.finish();
}

criterion_group!(
    benches,
    candle_range_query,
    candle_last_n,
    candle_find_at,
    candle_query_through_engine,
);
criterion_main!(benches);
