use perfdb::core::timeframe::Timeframe;
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

fn oid(id: u8) -> [u8; 32] {
    let mut buf = [0u8; 32];
    buf[30] = 0xFF;
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
fn full_lifecycle() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());

    let snap_path;
    {
        let db = PerfDb::open(config.clone()).unwrap();

        for i in 0..200u64 {
            let mid = (i % 3) as u16;
            let base_price = match mid {
                0 => 67_000.0,
                1 => 3_500.0,
                _ => 150.0,
            };
            db.ingest_tick(&make_tick(i * 1_000_000_000, mid, base_price + i as f64 * 0.1))
                .unwrap();
        }

        db.process_event(&make_event(
            100,
            Event::CollateralDeposited {
                user: addr(1),
                token: token(1),
                amount: 10_000_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            101,
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
            102,
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
            103,
            Event::OrderPlaced {
                order_id: oid(1),
                user: addr(3),
                market_id: 0,
                order_type: 0,
                is_long: true,
                trigger_price: 66_500,
                size_usd: 200_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            104,
            Event::OrderExecuted {
                order_id: oid(1),
                position_id: pid(10),
                execution_price: 66_550,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            200,
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
            201,
            Event::FundingRateUpdated {
                market_id: 0,
                new_rate_per_second: 500,
                funding_rate_24h: 43_200_000,
            },
        ))
        .unwrap();

        db.process_event(&make_event(
            202,
            Event::VaultCreated {
                user: addr(1),
                vault: addr(50),
            },
        ))
        .unwrap();

        db.save_checkpoint(202, "0xblock202".to_string(), 2424);
        snap_path = db.snapshot().unwrap();

        assert_eq!(db.tick_count(0), 67);
        assert_eq!(db.tick_count(1), 67);
        assert_eq!(db.tick_count(2), 66);

        assert!(db.positions.get(&pid(1)).is_some());
        assert!(db.positions.get(&pid(2)).is_some());
        assert!(matches!(
            db.positions.get(&pid(1)).unwrap().status,
            PositionStatus::Closed
        ));
        assert!(matches!(
            db.positions.get(&pid(2)).unwrap().status,
            PositionStatus::Open
        ));

        assert!(matches!(
            db.orders.get(&oid(1)).unwrap().status,
            OrderStatus::Executed
        ));

        let bal = db.balances.get(&addr(1), &token(1)).unwrap();
        assert_eq!(bal.amount, 10_000_000);

        let user1 = db.users.get(&addr(1)).unwrap();
        assert_eq!(user1.vault_address, Some(addr(50)));

        let stats1 = db.trader_stats.get(&addr(1)).unwrap();
        assert_eq!(stats1.total_trades, 1);
        assert_eq!(stats1.total_pnl, 15_000);
        assert_eq!(stats1.win_count, 1);
        assert_eq!(stats1.open_positions, 0);

        assert_eq!(db.market_state.funding_rate(0), 500);
        assert_eq!(db.checkpoint.last_block(), 202);
        assert_eq!(db.events.total_count(), 8);
    }

    assert!(snap_path.exists());

    {
        let db = PerfDb::open(config).unwrap();

        assert_eq!(db.tick_count(0), 67);
        assert_eq!(db.tick_count(1), 67);

        assert!(db.positions.get(&pid(1)).is_some());
        assert!(db.positions.get(&pid(2)).is_some());
        assert!(matches!(
            db.positions.get(&pid(1)).unwrap().status,
            PositionStatus::Closed
        ));

        assert_eq!(db.users.get(&addr(1)).unwrap().vault_address, Some(addr(50)));
        assert_eq!(db.checkpoint.last_block(), 202);

        db.ingest_tick(&make_tick(999_000_000_000, 0, 68_500.0))
            .unwrap();
        assert_eq!(db.latest_price(0), 68_500.0);
        assert_eq!(db.tick_count(0), 68);
    }
}

#[test]
fn multi_market_workload() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    for i in 0..1_800u64 {
        let mid = (i % 18) as u16;
        db.ingest_tick(&make_tick(i * 1_000_000_000, mid, 50_000.0 + i as f64))
            .unwrap();
    }

    for mid in 0..18u16 {
        assert_eq!(db.tick_count(mid), 100, "market {mid} tick count mismatch");
    }

    let prices = db.all_prices();
    assert_eq!(prices.len(), 18);

    for mid in 0..18u16 {
        assert!(db.latest_price(mid) > 0.0, "market {mid} has no price");
    }

    for mid in 0..18u16 {
        db.process_event(&make_event(
            mid as u64 + 1000,
            Event::PositionOpened {
                position_id: pid(mid as u8 + 100),
                user: addr(mid as u8 + 1),
                market_id: mid,
                is_long: mid % 2 == 0,
                size_usd: 100_000,
                leverage: 10,
                entry_price: db.latest_price(mid) as i128,
                collateral_token: token(1),
                collateral_amount: 10_000,
            },
        ))
        .unwrap();
    }

    for mid in 0..18u16 {
        assert!(
            db.positions.get(&pid(mid as u8 + 100)).is_some(),
            "position for market {mid} not found"
        );
    }

    assert!(db.users.contains(&addr(1)));
    assert!(db.users.contains(&addr(18)));

    db.shutdown().unwrap();
}

#[test]
fn candle_generation_across_boundaries() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    for i in 0..240u64 {
        let ts_ns = i * 500_000_000;
        db.ingest_tick(&make_tick(ts_ns, 0, 67_000.0 + (i as f64 * 0.5)))
            .unwrap();
    }

    let sec1_count = db.candle_count(0, Timeframe::Sec1);
    assert!(
        sec1_count >= 118,
        "expected >=118 1s candles, got {sec1_count}"
    );

    assert!(db.live_candle(0, Timeframe::Sec1).is_some());
    assert!(db.live_candle(0, Timeframe::Min1).is_some());

    let candles = db.candles(0, Timeframe::Sec1);
    for w in candles.windows(2) {
        assert!(
            w[0].timestamp_ns < w[1].timestamp_ns,
            "candles not ordered"
        );
    }
}

#[test]
fn position_full_lifecycle() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

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

    let pos = db.positions.get(&pid(1)).unwrap();
    assert!(matches!(pos.status, PositionStatus::Open));
    assert_eq!(pos.size_usd, 1_000_000);
    assert_eq!(db.trader_stats.get(&addr(1)).unwrap().open_positions, 1);

    db.process_event(&make_event(
        150,
        Event::PositionModified {
            position_id: pid(1),
            new_size_usd: 1_500_000,
            new_collateral_usd: 150_000,
            new_collateral_amount: 150_000,
        },
    ))
    .unwrap();

    let pos = db.positions.get(&pid(1)).unwrap();
    assert_eq!(pos.size_usd, 1_500_000);
    assert_eq!(pos.collateral_usd, 150_000);

    db.process_event(&make_event(
        200,
        Event::PositionClosed {
            position_id: pid(1),
            user: addr(1),
            market_id: 0,
            closed_size_usd: 500_000,
            exit_price: 68_000,
            realized_pnl: 5_000,
            is_full_close: false,
        },
    ))
    .unwrap();

    let pos = db.positions.get(&pid(1)).unwrap();
    assert!(matches!(pos.status, PositionStatus::Open));
    assert_eq!(pos.size_usd, 1_000_000);
    assert_eq!(db.trader_stats.get(&addr(1)).unwrap().open_positions, 1);
    assert_eq!(db.trader_stats.get(&addr(1)).unwrap().total_trades, 1);

    db.process_event(&make_event(
        300,
        Event::PositionClosed {
            position_id: pid(1),
            user: addr(1),
            market_id: 0,
            closed_size_usd: 1_000_000,
            exit_price: 69_000,
            realized_pnl: 20_000,
            is_full_close: true,
        },
    ))
    .unwrap();

    let pos = db.positions.get(&pid(1)).unwrap();
    assert!(matches!(pos.status, PositionStatus::Closed));
    let stats = db.trader_stats.get(&addr(1)).unwrap();
    assert_eq!(stats.open_positions, 0);
    assert_eq!(stats.total_trades, 2);
    assert_eq!(stats.total_pnl, 25_000);
    assert_eq!(stats.win_count, 2);
}

#[test]
fn liquidation_flow() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    db.process_event(&make_event(
        100,
        Event::PositionOpened {
            position_id: pid(1),
            user: addr(1),
            market_id: 0,
            is_long: true,
            size_usd: 5_000_000,
            leverage: 100,
            entry_price: 67_000,
            collateral_token: token(1),
            collateral_amount: 50_000,
        },
    ))
    .unwrap();

    db.process_event(&make_event(
        500,
        Event::Liquidation {
            position_id: pid(1),
            user: addr(1),
            market_id: 0,
            liquidation_price: 65_000,
            penalty: 25_000,
            keeper: addr(99),
        },
    ))
    .unwrap();

    let pos = db.positions.get(&pid(1)).unwrap();
    assert!(matches!(pos.status, PositionStatus::Liquidated));
    assert_eq!(pos.exit_price, Some(65_000));
    assert_eq!(pos.close_block, Some(500));

    let stats = db.trader_stats.get(&addr(1)).unwrap();
    assert_eq!(stats.liquidation_count, 1);
    assert_eq!(stats.open_positions, 0);
    assert_eq!(stats.total_pnl, -25_000);
}

#[test]
fn event_log_partitioning() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

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

    db.process_event(&make_event(
        3,
        Event::FundingRateUpdated {
            market_id: 0,
            new_rate_per_second: 100,
            funding_rate_24h: 8_640_000,
        },
    ))
    .unwrap();

    assert_eq!(db.events.total_count(), 3);

    let m0_events = db.events.by_market(0, None, None);
    assert_eq!(m0_events.len(), 2);

    let m1_events = db.events.by_market(1, None, None);
    assert_eq!(m1_events.len(), 1);

    let t1_events = db.events.by_trader(&addr(1), None, None);
    assert_eq!(t1_events.len(), 1);
}

#[test]
fn leaderboard_after_trading() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

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
        10,
        Event::PositionClosed {
            position_id: pid(1),
            user: addr(1),
            market_id: 0,
            closed_size_usd: 1_000_000,
            exit_price: 70_000,
            realized_pnl: 50_000,
            is_full_close: true,
        },
    ))
    .unwrap();

    db.process_event(&make_event(
        2,
        Event::PositionOpened {
            position_id: pid(2),
            user: addr(2),
            market_id: 0,
            is_long: false,
            size_usd: 5_000_000,
            leverage: 50,
            entry_price: 67_000,
            collateral_token: token(1),
            collateral_amount: 100_000,
        },
    ))
    .unwrap();
    db.process_event(&make_event(
        20,
        Event::PositionClosed {
            position_id: pid(2),
            user: addr(2),
            market_id: 0,
            closed_size_usd: 5_000_000,
            exit_price: 68_000,
            realized_pnl: -10_000,
            is_full_close: true,
        },
    ))
    .unwrap();

    db.rebuild_leaderboard(1000);

    let pnl_board = db.pnl_leaderboard(10);
    assert_eq!(pnl_board.len(), 2);
    assert_eq!(pnl_board[0].address, addr(1));

    let vol_board = db.volume_leaderboard(10);
    assert_eq!(vol_board.len(), 2);
    assert_eq!(vol_board[0].address, addr(2));
}

#[test]
fn balance_deposit_withdraw_flow() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    let tok = token(1);

    db.process_event(&make_event(
        1,
        Event::CollateralDeposited {
            user: addr(1),
            token: tok,
            amount: 1_000_000,
        },
    ))
    .unwrap();

    let bal = db.balances.get(&addr(1), &tok).unwrap();
    assert_eq!(bal.amount, 1_000_000);
    assert_eq!(bal.available, 1_000_000);

    db.process_event(&make_event(
        2,
        Event::CollateralDeposited {
            user: addr(1),
            token: tok,
            amount: 500_000,
        },
    ))
    .unwrap();
    assert_eq!(db.balances.get(&addr(1), &tok).unwrap().amount, 1_500_000);

    db.process_event(&make_event(
        3,
        Event::CollateralWithdrawn {
            user: addr(1),
            token: tok,
            amount: 300_000,
        },
    ))
    .unwrap();
    assert_eq!(db.balances.get(&addr(1), &tok).unwrap().amount, 1_200_000);
    assert_eq!(
        db.balances.get(&addr(1), &tok).unwrap().available,
        1_200_000
    );
}

#[test]
fn all_event_variants_process_cleanly() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    let events = vec![
        make_event(1, Event::PositionOpened {
            position_id: pid(1), user: addr(1), market_id: 0, is_long: true,
            size_usd: 100_000, leverage: 10, entry_price: 67_000,
            collateral_token: token(1), collateral_amount: 10_000,
        }),
        make_event(2, Event::PositionModified {
            position_id: pid(1), new_size_usd: 200_000,
            new_collateral_usd: 20_000, new_collateral_amount: 20_000,
        }),
        make_event(3, Event::PositionClosed {
            position_id: pid(1), user: addr(1), market_id: 0,
            closed_size_usd: 200_000, exit_price: 68_000,
            realized_pnl: 5_000, is_full_close: true,
        }),
        make_event(4, Event::OrderPlaced {
            order_id: oid(1), user: addr(2), market_id: 0,
            order_type: 0, is_long: true, trigger_price: 66_000, size_usd: 100_000,
        }),
        make_event(5, Event::OrderExecuted {
            order_id: oid(1), position_id: pid(5), execution_price: 66_100,
        }),
        make_event(6, Event::OrderPlaced {
            order_id: oid(2), user: addr(3), market_id: 1,
            order_type: 2, is_long: false, trigger_price: 70_000, size_usd: 50_000,
        }),
        make_event(7, Event::OrderCancelled {
            order_id: oid(2), user: addr(3),
        }),
        make_event(8, Event::PositionOpened {
            position_id: pid(10), user: addr(4), market_id: 0, is_long: true,
            size_usd: 500_000, leverage: 50, entry_price: 67_000,
            collateral_token: token(1), collateral_amount: 10_000,
        }),
        make_event(9, Event::Liquidation {
            position_id: pid(10), user: addr(4), market_id: 0,
            liquidation_price: 65_000, penalty: 5_000, keeper: addr(99),
        }),
        make_event(10, Event::ADLExecuted {
            position_id: pid(99), deleveraged_size_usd: 50_000,
        }),
        make_event(11, Event::PriceUpdated {
            market_id: 0, price: 67_500, timestamp: 132,
        }),
        make_event(12, Event::FundingSettled {
            market_id: 0, funding_rate: 100,
            long_payment: 5_000, short_payment: -5_000,
        }),
        make_event(13, Event::FundingRateUpdated {
            market_id: 0, new_rate_per_second: 200, funding_rate_24h: 17_280_000,
        }),
        make_event(14, Event::MarketCreated {
            market_id: 0, name: "Bitcoin".into(), symbol: "BTC".into(),
        }),
        make_event(15, Event::MarketPaused { market_id: 0 }),
        make_event(16, Event::CollateralDeposited {
            user: addr(5), token: token(2), amount: 5_000_000,
        }),
        make_event(17, Event::CollateralWithdrawn {
            user: addr(5), token: token(2), amount: 1_000_000,
        }),
        make_event(18, Event::VaultCreated {
            user: addr(6), vault: addr(60),
        }),
        make_event(19, Event::VaultDeficit {
            token: token(2), deficit: 500,
        }),
    ];

    for e in &events {
        db.process_event(e).unwrap();
    }

    assert_eq!(db.events.total_count(), events.len() as u64);
    assert_eq!(db.checkpoint.last_block(), 19);
}

#[test]
fn snapshot_preserves_all_state() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());

    {
        let db = PerfDb::open(config.clone()).unwrap();

        for i in 1..=5u8 {
            db.process_event(&make_event(
                i as u64 * 10,
                Event::CollateralDeposited {
                    user: addr(i),
                    token: token(1),
                    amount: i as i128 * 1_000_000,
                },
            ))
            .unwrap();

            db.process_event(&make_event(
                i as u64 * 10 + 1,
                Event::PositionOpened {
                    position_id: pid(i),
                    user: addr(i),
                    market_id: (i % 3) as u16,
                    is_long: i % 2 == 0,
                    size_usd: i as i128 * 100_000,
                    leverage: 10,
                    entry_price: 67_000,
                    collateral_token: token(1),
                    collateral_amount: i as i128 * 10_000,
                },
            ))
            .unwrap();
        }

        for i in [1u8, 3, 5] {
            db.process_event(&make_event(
                200 + i as u64,
                Event::PositionClosed {
                    position_id: pid(i),
                    user: addr(i),
                    market_id: (i % 3) as u16,
                    closed_size_usd: i as i128 * 100_000,
                    exit_price: 68_000,
                    realized_pnl: i as i128 * 1_000,
                    is_full_close: true,
                },
            ))
            .unwrap();
        }

        db.save_checkpoint(205, "0xblock205".to_string(), 2460);
        db.snapshot().unwrap();
    }

    {
        let db = PerfDb::open(config).unwrap();

        for i in 1..=5u8 {
            assert!(db.users.contains(&addr(i)), "user {} not found", i);
        }

        for i in 1..=5u8 {
            let pos = db.positions.get(&pid(i)).unwrap();
            if [1, 3, 5].contains(&i) {
                assert!(matches!(pos.status, PositionStatus::Closed));
            } else {
                assert!(matches!(pos.status, PositionStatus::Open));
            }
        }

        for i in 1..=5u8 {
            let bal = db.balances.get(&addr(i), &token(1)).unwrap();
            assert_eq!(bal.amount, i as i128 * 1_000_000);
        }

        assert_eq!(db.checkpoint.last_block(), 205);
    }
}

#[test]
fn tick_range_queries() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    for i in 0..100u64 {
        db.ingest_tick(&make_tick(i * 1_000_000_000, 0, 67_000.0 + i as f64))
            .unwrap();
    }

    let all_ticks = db.ticks(0);
    assert_eq!(all_ticks.len(), 100);

    let range = perfdb::query::time_range::query_tick_range(
        &all_ticks,
        10_000_000_000,
        20_000_000_000,
    );
    assert_eq!(range.len(), 10);

    let last5 = perfdb::query::time_range::query_last_ticks(&all_ticks, 5);
    assert_eq!(last5.len(), 5);
    assert_eq!(last5[0].timestamp_ns, 95_000_000_000);
}

#[test]
fn wal_sync_and_continue() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    for i in 0..50u64 {
        db.ingest_tick(&make_tick(i * 1_000_000_000, 0, 67_000.0 + i as f64))
            .unwrap();
    }

    db.sync_wal().unwrap();

    for i in 50..100u64 {
        db.ingest_tick(&make_tick(i * 1_000_000_000, 0, 67_000.0 + i as f64))
            .unwrap();
    }

    assert_eq!(db.tick_count(0), 100);
    db.shutdown().unwrap();
}

#[test]
fn shutdown_creates_snapshot() {
    let dir = tempfile::tempdir().unwrap();
    let config = test_config(dir.path());

    {
        let db = PerfDb::open(config.clone()).unwrap();

        db.ingest_tick(&make_tick(1_000, 0, 67_000.0)).unwrap();
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

        db.save_checkpoint(1, "0xhash".to_string(), 12);
        db.shutdown().unwrap();
    }

    let snap_dir = config.snapshots_dir();
    let snaps = perfdb::storage::snapshot::list_snapshots(&snap_dir).unwrap();
    assert!(!snaps.is_empty());
}

#[test]
fn large_tick_ingestion() {
    let dir = tempfile::tempdir().unwrap();
    let db = PerfDb::open(test_config(dir.path())).unwrap();

    let count = 50_000u64;
    for i in 0..count {
        let mid = (i % 5) as u16;
        db.ingest_tick(&make_tick(i * 1_000_000, mid, 50_000.0 + (i as f64 * 0.01)))
            .unwrap();
    }

    let total: u64 = (0..5).map(|mid| db.tick_count(mid)).sum();
    assert_eq!(total, count);

    for mid in 0..5u16 {
        let ticks = db.ticks(mid);
        for w in ticks.windows(2) {
            assert!(w[0].timestamp_ns < w[1].timestamp_ns);
        }
    }
}
