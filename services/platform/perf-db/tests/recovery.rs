use std::fs::{self, OpenOptions};
use std::io::Write;

use perfdb::core::types::*;
use perfdb::engine::PerfDbConfig;
use perfdb::storage::wal::{SyncPolicy, WalConfig};
use perfdb::PerfDb;


fn test_config(dir: &std::path::Path) -> PerfDbConfig {
    PerfDbConfig {
        data_dir: dir.to_path_buf(),
        wal: WalConfig {
            dir: dir.join("wal"),
            max_segment_size: 64 * 1024 * 1024,
            sync_policy: SyncPolicy::EveryWrite,
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

fn addr(id: u8) -> [u8; 20] {
    let mut buf = [0u8; 20];
    buf[19] = id;
    buf
}

fn pid(id: u8) -> [u8; 32] {
    let mut buf = [0u8; 32];
    buf[31] = id;
    buf
}

fn token(id: u8) -> [u8; 20] {
    let mut buf = [0u8; 20];
    buf[0] = 0xAA;
    buf[19] = id;
    buf
}

fn make_event(block: u64, event: Event) -> IndexedEvent {
    IndexedEvent {
        block_number: block,
        block_timestamp: block * 12,
        tx_hash: {
            let mut buf = [0u8; 32];
            buf[31] = block as u8;
            buf[30] = (block >> 8) as u8;
            buf
        },
        tx_index: 0,
        log_index: 0,
        event,
    }
}


#[test]
fn wal_only_recovery_restores_events() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();

        db.process_event(&make_event(
            1,
            Event::CollateralDeposited {
                user: addr(1),
                token: token(1),
                amount: 5_000_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            2,
            Event::PositionOpened {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                is_long: true,
                size_usd: 1_000_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            3,
            Event::FundingRateUpdated {
                market_id: 0,
                new_rate_per_second: 500,
                funding_rate_24h: 43_200_000,
            },
        ))
        .unwrap();

        db.sync_wal().unwrap();

    }


    {
        let db = PerfDb::open(config).unwrap();




        let bal = db.balances.get(&addr(1), &token(1));
        assert!(bal.is_some(), "balance not recovered from WAL");
        assert_eq!(bal.unwrap().amount, 5_000_000);


        assert!(db.positions.get(&pid(1)).is_some());
        let pos = db.positions.get(&pid(1)).unwrap();
        assert!(matches!(pos.status, PositionStatus::Open));
        assert_eq!(pos.size_usd, 1_000_000);


        assert!(db.users.contains(&addr(1)));


        assert_eq!(db.market_state.funding_rate(0), 500);


        assert_eq!(db.checkpoint.last_block(), 3);
    }
}


#[test]
fn wal_recovery_restores_market_prices() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();

        db.ingest_tick(&make_tick(1_000_000_000, 0, 67_000.0)).unwrap();
        db.ingest_tick(&make_tick(2_000_000_000, 0, 67_100.0)).unwrap();
        db.ingest_tick(&make_tick(1_000_000_000, 1, 3_500.0)).unwrap();

        db.sync_wal().unwrap();
    }


    {
        let db = PerfDb::open(config).unwrap();


        assert_eq!(db.latest_price(0), 67_100.0);
        assert_eq!(db.latest_price(1), 3_500.0);


        assert_eq!(db.tick_count(0), 2);
        assert_eq!(db.tick_count(1), 1);
    }
}


#[test]
fn snapshot_plus_wal_recovery() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();


        db.process_event(&make_event(
            100,
            Event::PositionOpened {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                is_long: true,
                size_usd: 1_000_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.save_checkpoint(100, "0xblock100".to_string(), 1200);
        db.snapshot().unwrap();


        db.process_event(&make_event(
            101,
            Event::PositionOpened {
                position_id: pid(2),
                user: addr(2),
                market_id: 1,
                is_long: false,
                size_usd: 500_000,
                leverage: 5,
                entry_price: 3_500,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            102,
            Event::CollateralDeposited {
                user: addr(3),
                token: token(1),
                amount: 2_000_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            103,
            Event::PositionClosed {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                closed_size_usd: 1_000_000,
                exit_price: 68_000,
                realized_pnl: 15_000,
                is_full_close: true,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            104,
            Event::FundingRateUpdated {
                market_id: 0,
                new_rate_per_second: 300,
                funding_rate_24h: 25_920_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            105,
            Event::VaultCreated {
                user: addr(2),
                vault: addr(50),
            },
        ))
        .unwrap();

        db.sync_wal().unwrap();

    }


    {
        let db = PerfDb::open(config).unwrap();


        let pos1 = db.positions.get(&pid(1)).unwrap();
        assert!(
            matches!(pos1.status, PositionStatus::Closed),
            "position 1 should be closed after WAL replay"
        );
        assert_eq!(pos1.exit_price, Some(68_000));


        let pos2 = db.positions.get(&pid(2)).unwrap();
        assert!(matches!(pos2.status, PositionStatus::Open));
        assert_eq!(pos2.market_id, 1);


        let bal = db.balances.get(&addr(3), &token(1)).unwrap();
        assert_eq!(bal.amount, 2_000_000);


        assert_eq!(db.market_state.funding_rate(0), 300);


        let user2 = db.users.get(&addr(2)).unwrap();
        assert_eq!(user2.vault_address, Some(addr(50)));


        let stats1 = db.trader_stats.get(&addr(1)).unwrap();
        assert_eq!(stats1.total_pnl, 15_000);
        assert_eq!(stats1.open_positions, 0);
    }
}


#[test]
fn truncated_wal_recovers_valid_entries() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();

        db.process_event(&make_event(
            1,
            Event::PositionOpened {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                is_long: true,
                size_usd: 1_000_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            2,
            Event::CollateralDeposited {
                user: addr(2),
                token: token(1),
                amount: 3_000_000,
            },
        ))
        .unwrap();

        db.sync_wal().unwrap();
    }


    let wal_dir = dir.path().join("wal");
    let entries: Vec<_> = fs::read_dir(&wal_dir)
        .unwrap()
        .filter_map(|e| e.ok())
        .filter(|e| {
            e.file_name()
                .to_string_lossy()
                .starts_with("wal_")
        })
        .collect();


    let mut seg_files: Vec<_> = entries.iter().map(|e| e.path()).collect();
    seg_files.sort();
    let last_seg = seg_files.last().unwrap();

    {
        let mut f = OpenOptions::new().append(true).open(last_seg).unwrap();

        f.write_all(&[0xFF; 37]).unwrap();
        f.flush().unwrap();
    }


    {
        let db = PerfDb::open(config).unwrap();


        assert!(db.positions.get(&pid(1)).is_some());


        let bal = db.balances.get(&addr(2), &token(1)).unwrap();
        assert_eq!(bal.amount, 3_000_000);
    }
}


#[test]
fn corrupted_crc_stops_replay_at_corruption() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();

        db.process_event(&make_event(
            1,
            Event::PositionOpened {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                is_long: true,
                size_usd: 1_000_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            2,
            Event::PositionOpened {
                position_id: pid(2),
                user: addr(2),
                market_id: 1,
                is_long: false,
                size_usd: 500_000,
                leverage: 5,
                entry_price: 3_500,
                collateral_token: token(1),
                collateral_amount: 100_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            3,
            Event::CollateralDeposited {
                user: addr(3),
                token: token(1),
                amount: 999_999,
            },
        ))
        .unwrap();

        db.sync_wal().unwrap();
    }


    let wal_dir = dir.path().join("wal");
    let mut seg_files: Vec<_> = fs::read_dir(&wal_dir)
        .unwrap()
        .filter_map(|e| e.ok())
        .filter(|e| {
            e.file_name()
                .to_string_lossy()
                .starts_with("wal_")
        })
        .map(|e| e.path())
        .collect();
    seg_files.sort();
    let seg_path = seg_files.last().unwrap();

    {
        let data = fs::read(seg_path).unwrap();
        let mut modified = data.clone();


        let first_len = u32::from_le_bytes(data[0..4].try_into().unwrap()) as usize;
        let second_offset = 8 + first_len;
        if second_offset + 8 < modified.len() {
            modified[second_offset + 4] ^= 0xFF;
            modified[second_offset + 5] ^= 0xFF;
        }

        fs::write(seg_path, &modified).unwrap();
    }


    {
        let db = PerfDb::open(config).unwrap();


        assert!(
            db.positions.get(&pid(1)).is_some(),
            "position 1 should be recovered (before corruption)"
        );


        assert!(
            db.positions.get(&pid(2)).is_none(),
            "position 2 should NOT be recovered (corrupted CRC)"
        );


        let bal = db.balances.get(&addr(3), &token(1));
        assert!(
            bal.is_none(),
            "balance should NOT be recovered (after corruption point)"
        );
    }
}


#[test]
fn empty_wal_recovery() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());

    {
        let _db = PerfDb::open(config.clone()).unwrap();

    }

    {
        let db = PerfDb::open(config).unwrap();
        assert_eq!(db.tick_count(0), 0);
        assert_eq!(db.latest_price(0), 0.0);
        assert_eq!(db.checkpoint.last_block(), 0);
    }
}


#[test]
fn multiple_reopens_accumulate_state() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();
        db.process_event(&make_event(
            1,
            Event::PositionOpened {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                is_long: true,
                size_usd: 100_000,
                leverage: 10,
                entry_price: 67_000,
                collateral_token: token(1),
                collateral_amount: 10_000,
            },
        ))
        .unwrap();
        db.save_checkpoint(1, "0x1".to_string(), 12);
        db.shutdown().unwrap();
    }


    {
        let db = PerfDb::open(config.clone()).unwrap();


        assert!(db.positions.get(&pid(1)).is_some());

        db.process_event(&make_event(
            2,
            Event::PositionOpened {
                position_id: pid(2),
                user: addr(2),
                market_id: 1,
                is_long: false,
                size_usd: 200_000,
                leverage: 5,
                entry_price: 3_500,
                collateral_token: token(1),
                collateral_amount: 40_000,
            },
        ))
        .unwrap();
        db.save_checkpoint(2, "0x2".to_string(), 24);
        db.shutdown().unwrap();
    }


    {
        let db = PerfDb::open(config.clone()).unwrap();
        assert!(db.positions.get(&pid(1)).is_some());
        assert!(db.positions.get(&pid(2)).is_some());
        assert_eq!(db.checkpoint.last_block(), 2);


        db.process_event(&make_event(
            3,
            Event::PositionOpened {
                position_id: pid(3),
                user: addr(3),
                market_id: 2,
                is_long: true,
                size_usd: 300_000,
                leverage: 20,
                entry_price: 150,
                collateral_token: token(1),
                collateral_amount: 15_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            4,
            Event::PositionClosed {
                position_id: pid(1),
                user: addr(1),
                market_id: 0,
                closed_size_usd: 100_000,
                exit_price: 68_000,
                realized_pnl: 1_000,
                is_full_close: true,
            },
        ))
        .unwrap();

        db.save_checkpoint(4, "0x4".to_string(), 48);
        db.shutdown().unwrap();
    }


    {
        let db = PerfDb::open(config).unwrap();
        assert!(matches!(
            db.positions.get(&pid(1)).unwrap().status,
            PositionStatus::Closed
        ));
        assert!(matches!(
            db.positions.get(&pid(2)).unwrap().status,
            PositionStatus::Open
        ));
        assert!(matches!(
            db.positions.get(&pid(3)).unwrap().status,
            PositionStatus::Open
        ));
        assert_eq!(db.checkpoint.last_block(), 4);
    }
}


#[test]
fn wal_replay_deduplicates_with_snapshot() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());


    {
        let db = PerfDb::open(config.clone()).unwrap();

        for i in 1..=5u64 {
            db.process_event(&make_event(
                i,
                Event::CollateralDeposited {
                    user: addr(i as u8),
                    token: token(1),
                    amount: i as i128 * 1_000_000,
                },
            ))
            .unwrap();
        }

        db.save_checkpoint(5, "0x5".to_string(), 60);
        db.snapshot().unwrap();


        for i in 6..=10u64 {
            db.process_event(&make_event(
                i,
                Event::CollateralDeposited {
                    user: addr(i as u8),
                    token: token(1),
                    amount: i as i128 * 1_000_000,
                },
            ))
            .unwrap();
        }

        db.sync_wal().unwrap();
    }



    {
        let db = PerfDb::open(config).unwrap();


        for i in 1..=10u8 {
            let bal = db.balances.get(&addr(i), &token(1));
            assert!(
                bal.is_some(),
                "balance for user {} not found",
                i
            );

            assert_eq!(
                bal.unwrap().amount,
                i as i128 * 1_000_000,
                "user {} balance wrong — possible duplicate replay",
                i
            );
        }
    }
}


#[test]
fn recovery_after_heavy_workload() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());

    let num_ticks = 10_000u64;
    let num_events = 100u64;


    {
        let db = PerfDb::open(config.clone()).unwrap();

        for i in 0..num_ticks {
            let mid = (i % 5) as u16;
            db.ingest_tick(&make_tick(i * 1_000_000, mid, 50_000.0 + i as f64 * 0.01))
                .unwrap();
        }

        for i in 0..num_events {
            db.process_event(&make_event(
                i + 1,
                Event::PositionOpened {
                    position_id: pid((i % 200) as u8),
                    user: addr((i % 50) as u8 + 1),
                    market_id: (i % 5) as u16,
                    is_long: i % 2 == 0,
                    size_usd: 100_000 + i as i128 * 1_000,
                    leverage: 10,
                    entry_price: 67_000,
                    collateral_token: token(1),
                    collateral_amount: 10_000,
                },
            ))
            .unwrap();
        }

        db.save_checkpoint(num_events, "0xheavy".to_string(), num_events * 12);
        db.snapshot().unwrap();
        db.sync_wal().unwrap();
    }


    {
        let db = PerfDb::open(config).unwrap();


        let total_ticks: u64 = (0..5).map(|mid| db.tick_count(mid)).sum();
        assert_eq!(total_ticks, num_ticks);


        assert_eq!(db.checkpoint.last_block(), num_events);


        for mid in 0..5u16 {
            assert!(db.latest_price(mid) > 0.0);
        }
    }
}
