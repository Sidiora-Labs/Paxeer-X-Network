use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};

use perfdb::core::types::*;
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

fn addr(id: u16) -> [u8; 20] {
    let mut buf = [0u8; 20];
    buf[18] = (id >> 8) as u8;
    buf[19] = id as u8;
    buf
}

fn pid(id: u16) -> [u8; 32] {
    let mut buf = [0u8; 32];
    buf[30] = (id >> 8) as u8;
    buf[31] = id as u8;
    buf
}

fn token() -> [u8; 20] {
    let mut buf = [0u8; 20];
    buf[0] = 0xAA;
    buf
}

fn setup_db(num_positions: u16) -> (tempfile::TempDir, PerfDb) {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(bench_config(dir.path())).unwrap();

    for i in 0..num_positions {
        let user = addr(i / 10); // 10 positions per user
        db.process_event(&IndexedEvent {
            block_number: i as u64 + 1,
            block_timestamp: (i as u64 + 1) * 12,
            tx_hash: {
                let mut buf = [0u8; 32];
                buf[31] = i as u8;
                buf[30] = (i >> 8) as u8;
                buf
            },
            tx_index: 0,
            log_index: 0,
            event: Event::PositionOpened {
                position_id: pid(i),
                user,
                market_id: (i % 18) as u16,
                is_long: i % 2 == 0,
                size_usd: 100_000 + i as i128 * 1_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(),
                collateral_amount: 10_000,
            },
        })
        .unwrap();
    }

    (dir, db)
}

fn position_get_by_id(c: &mut Criterion) {
    let mut group = c.benchmark_group("position_get_by_id");

    for count in [100u16, 1_000, 5_000] {
        let (_dir, db) = setup_db(count);
        let target_id = pid(count / 2);

        group.throughput(Throughput::Elements(1));
        group.bench_with_input(
            BenchmarkId::from_parameter(count),
            &count,
            |b, _| {
                b.iter(|| {
                    let pos = db.positions.get(&target_id);
                    criterion::black_box(pos);
                });
            },
        );
    }
    group.finish();
}

fn position_by_user(c: &mut Criterion) {
    let mut group = c.benchmark_group("position_by_user");

    for count in [100u16, 1_000, 5_000] {
        let (_dir, db) = setup_db(count);
        let target_user = addr(count / 20); // mid-range user

        group.bench_with_input(
            BenchmarkId::from_parameter(count),
            &count,
            |b, _| {
                b.iter(|| {
                    let positions = db.positions.by_user(&target_user);
                    criterion::black_box(positions.len());
                });
            },
        );
    }
    group.finish();
}

fn position_by_market(c: &mut Criterion) {
    let mut group = c.benchmark_group("position_by_market");

    let (_dir, db) = setup_db(5_000);

    group.bench_function("market_0", |b| {
        b.iter(|| {
            let positions = db.positions.by_market(0);
            criterion::black_box(positions.len());
        });
    });

    group.finish();
}

fn position_open_by_user(c: &mut Criterion) {
    let mut group = c.benchmark_group("position_open_by_user");

    let (_dir, db) = setup_db(5_000);
    let target_user = addr(50);

    group.bench_function("5000_positions", |b| {
        b.iter(|| {
            let positions = db.positions.open_by_user(&target_user);
            criterion::black_box(positions.len());
        });
    });

    group.finish();
}

criterion_group!(
    benches,
    position_get_by_id,
    position_by_user,
    position_by_market,
    position_open_by_user,
);
criterion_main!(benches);
