use std::path::PathBuf;
use std::process;

use clap::{Parser, Subcommand};

use perfdb::core::market::{market_by_id, market_by_symbol, MarketId, MARKETS, MARKET_COUNT};
use perfdb::core::timeframe::Timeframe;
use perfdb::core::types::FixedI128;
use perfdb::engine::PerfDbConfig;
use perfdb::PerfDb;

#[derive(Parser)]
#[command(
    name = "perfdb",
    version,
    about = "PerfDB — embedded high-performance database",
    long_about = None,
)]
struct Cli {
    /// Data directory path
    #[arg(long, env = "PERFDB_DATA_DIR", default_value = "./data")]
    data_dir: PathBuf,

    /// Output as JSON instead of human-readable tables
    #[arg(long, global = true)]
    json: bool,

    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Database overview: record counts, store sizes
    Info,

    /// List all markets with status
    Markets,

    /// Latest prices (all markets or specific)
    Prices {
        /// Market symbol or ID (e.g. "BTC" or "0"). Omit for all.
        market: Option<String>,
    },

    /// Tick data for a market
    Ticks {
        /// Market symbol or ID
        market: String,
        /// Show last N ticks
        #[arg(long, default_value = "10")]
        last: usize,
    },

    /// Candle data for a market + timeframe
    Candles {
        /// Market symbol or ID
        market: String,
        /// Timeframe (1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1D, 1W, etc.)
        timeframe: String,
        /// Show last N candles
        #[arg(long, default_value = "10")]
        last: usize,
    },

    /// Query positions
    Positions {
        /// Filter by user address (hex, 0x-prefixed)
        #[arg(long)]
        user: Option<String>,
        /// Filter by market symbol or ID
        #[arg(long)]
        market: Option<String>,
        /// Look up a specific position by ID (hex)
        #[arg(long)]
        id: Option<String>,
        /// Show only open positions
        #[arg(long)]
        open: bool,
    },

    /// Query orders
    Orders {
        /// Filter by user address
        #[arg(long)]
        user: Option<String>,
        /// Look up a specific order by ID (hex)
        #[arg(long)]
        id: Option<String>,
        /// Show only active orders
        #[arg(long)]
        active: bool,
    },

    /// Order book depth for a market
    Orderbook {
        /// Market symbol or ID
        market: String,
        /// Number of price levels to show
        #[arg(long, default_value = "10")]
        levels: usize,
    },

    /// Balances for a user address
    Balances {
        /// User address (hex, 0x-prefixed)
        user: String,
    },

    /// User profile and referral info
    User {
        /// User address (hex, 0x-prefixed)
        address: String,
    },

    /// Trader statistics
    Stats {
        /// Trader address (hex, 0x-prefixed)
        address: String,
    },

    /// Daily PnL history for a trader
    Daily {
        /// Trader address (hex, 0x-prefixed)
        address: String,
        /// Max days to show
        #[arg(long, default_value = "30")]
        limit: usize,
    },

    /// Leaderboard (pnl or volume)
    Leaderboard {
        /// Metric: "pnl" or "volume"
        #[arg(default_value = "pnl")]
        metric: String,
        /// Number of entries
        #[arg(long, default_value = "20")]
        limit: usize,
    },

    /// Query events
    Events {
        /// Filter by market symbol or ID
        #[arg(long)]
        market: Option<String>,
        /// Filter by trader address
        #[arg(long)]
        trader: Option<String>,
        /// Show last N events
        #[arg(long, default_value = "20")]
        last: usize,
    },

    /// Current indexer checkpoint
    Checkpoint,

    /// Create a state snapshot
    Snapshot,
}

fn main() {
    let cli = Cli::parse();

    let config = PerfDbConfig::new(&cli.data_dir);
    let db = match PerfDb::open(config) {
        Ok(db) => db,
        Err(e) => {
            eprintln!("error: failed to open PerfDB at {}: {e}", cli.data_dir.display());
            process::exit(1);
        }
    };

    let result = match cli.command {
        Command::Info => cmd_info(&db, cli.json),
        Command::Markets => cmd_markets(&db, cli.json),
        Command::Prices { market } => cmd_prices(&db, market.as_deref(), cli.json),
        Command::Ticks { market, last } => cmd_ticks(&db, &market, last, cli.json),
        Command::Candles { market, timeframe, last } => {
            cmd_candles(&db, &market, &timeframe, last, cli.json)
        }
        Command::Positions { user, market, id, open } => {
            cmd_positions(&db, user.as_deref(), market.as_deref(), id.as_deref(), open, cli.json)
        }
        Command::Orders { user, id, active } => {
            cmd_orders(&db, user.as_deref(), id.as_deref(), active, cli.json)
        }
        Command::Orderbook { market, levels } => cmd_orderbook(&db, &market, levels, cli.json),
        Command::Balances { user } => cmd_balances(&db, &user, cli.json),
        Command::User { address } => cmd_user(&db, &address, cli.json),
        Command::Stats { address } => cmd_stats(&db, &address, cli.json),
        Command::Daily { address, limit } => cmd_daily(&db, &address, limit, cli.json),
        Command::Leaderboard { metric, limit } => cmd_leaderboard(&db, &metric, limit, cli.json),
        Command::Events { market, trader, last } => {
            cmd_events(&db, market.as_deref(), trader.as_deref(), last, cli.json)
        }
        Command::Checkpoint => cmd_checkpoint(&db, cli.json),
        Command::Snapshot => cmd_snapshot(&db),
    };

    if let Err(e) = result {
        eprintln!("error: {e}");
        process::exit(1);
    }
}

fn resolve_market(input: &str) -> Result<MarketId, String> {
    if let Ok(id) = input.parse::<u16>() {
        if market_by_id(id).is_some() {
            return Ok(id);
        }
        return Err(format!("unknown market ID: {id}"));
    }
    if let Some(m) = market_by_symbol(input) {
        return Ok(m.id);
    }
    let upper = input.to_uppercase();
    if let Some(m) = market_by_symbol(&upper) {
        return Ok(m.id);
    }
    Err(format!("unknown market: {input}"))
}

fn resolve_timeframe(input: &str) -> Result<Timeframe, String> {
    Timeframe::from_str(input).ok_or_else(|| format!("unknown timeframe: {input}"))
}

fn parse_addr(input: &str) -> [u8; 20] {
    perfdb::core::types::parse_address(input)
}

fn parse_hash(input: &str) -> [u8; 32] {
    perfdb::core::types::parse_hash(input)
}

fn hex_addr(addr: &[u8; 20]) -> String {
    format!("0x{}", addr.iter().map(|b| format!("{b:02x}")).collect::<String>())
}

fn hex_hash(hash: &[u8; 32]) -> String {
    format!("0x{}", hash.iter().map(|b| format!("{b:02x}")).collect::<String>())
}

fn format_fixed(val: FixedI128) -> String {
    if val == 0 {
        return "0".to_string();
    }
    let precision: i128 = 1_000_000_000_000_000_000;
    let whole = val / precision;
    let frac = (val % precision).unsigned_abs();
    if frac == 0 {
        format!("{whole}")
    } else {
        let frac_str = format!("{frac:018}");
        let trimmed = frac_str.trim_end_matches('0');
        format!("{whole}.{trimmed}")
    }
}

fn short_addr(addr: &[u8; 20]) -> String {
    let full = hex_addr(addr);
    if full.len() > 12 {
        format!("{}...{}", &full[..6], &full[full.len() - 4..])
    } else {
        full
    }
}

fn cmd_info(db: &PerfDb, json: bool) -> Result<(), String> {
    let pos_count = db.positions.count();
    let open_pos = db.positions.open_count();
    let order_count = db.orders.count();
    let active_orders = db.orders.active_count();
    let user_count = db.users.count();
    let balance_count = db.balances.count();
    let event_count = db.events.total_count();
    let trader_count = db.trader_stats.count();
    let cp = db.checkpoint.snapshot();

    let mut tick_total = 0u64;
    for mid in 0..MARKET_COUNT as MarketId {
        tick_total += db.tick_count(mid);
    }

    if json {
        let obj = serde_json::json!({
            "ticks_total": tick_total,
            "positions": pos_count,
            "positions_open": open_pos,
            "orders": order_count,
            "orders_active": active_orders,
            "users": user_count,
            "balances": balance_count,
            "events": event_count,
            "traders_with_stats": trader_count,
            "checkpoint_block": cp.last_block,
            "checkpoint_hash": cp.last_block_hash,
        });
        println!("{}", serde_json::to_string_pretty(&obj).unwrap());
    } else {
        println!("PerfDB Status");
        println!("{}", "=".repeat(40));
        println!("{:<24} {}", "Ticks (total):", tick_total);
        println!("{:<24} {} ({} open)", "Positions:", pos_count, open_pos);
        println!("{:<24} {} ({} active)", "Orders:", order_count, active_orders);
        println!("{:<24} {}", "Users:", user_count);
        println!("{:<24} {}", "Balance entries:", balance_count);
        println!("{:<24} {}", "Events:", event_count);
        println!("{:<24} {}", "Traders with stats:", trader_count);
        println!("{:<24} {}", "Checkpoint block:", cp.last_block);
        if !cp.last_block_hash.is_empty() {
            println!("{:<24} {}", "Checkpoint hash:", cp.last_block_hash);
        }
    }
    Ok(())
}

fn cmd_markets(db: &PerfDb, json: bool) -> Result<(), String> {
    if json {
        let markets: Vec<_> = MARKETS
            .iter()
            .map(|m| {
                let snap = db.market_state.snapshot(m.id);
                serde_json::json!({
                    "id": m.id,
                    "symbol": m.symbol,
                    "name": m.name,
                    "price": snap.as_ref().map(|s| s.latest_price).unwrap_or(0.0),
                    "enabled": snap.as_ref().map(|s| s.enabled).unwrap_or(false),
                    "paused": snap.as_ref().map(|s| s.paused).unwrap_or(false),
                    "ticks": db.tick_count(m.id),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&markets).unwrap());
    } else {
        println!(
            "{:<4} {:<8} {:<14} {:>14} {:>8} {:>8} {:>10}",
            "ID", "Symbol", "Name", "Price", "Status", "Paused", "Ticks"
        );
        println!("{}", "-".repeat(72));
        for m in &MARKETS {
            let snap = db.market_state.snapshot(m.id);
            let price = snap.as_ref().map(|s| s.latest_price).unwrap_or(0.0);
            let enabled = snap.as_ref().map(|s| s.enabled).unwrap_or(false);
            let paused = snap.as_ref().map(|s| s.paused).unwrap_or(false);
            let status = if enabled { "active" } else { "disabled" };
            let pause_str = if paused { "yes" } else { "-" };
            let ticks = db.tick_count(m.id);

            println!(
                "{:<4} {:<8} {:<14} {:>14.2} {:>8} {:>8} {:>10}",
                m.id, m.symbol, m.name, price, status, pause_str, ticks
            );
        }
    }
    Ok(())
}

fn cmd_prices(db: &PerfDb, market: Option<&str>, json: bool) -> Result<(), String> {
    if let Some(m) = market {
        let mid = resolve_market(m)?;
        let meta = market_by_id(mid).unwrap();
        let price = db.latest_price(mid);
        let ns = db.market_state.last_update_ns(mid);

        if json {
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "market_id": mid,
                    "symbol": meta.symbol,
                    "price": price,
                    "last_update_ns": ns,
                }))
                .unwrap()
            );
        } else {
            println!("{} ({}): {:.6}", meta.symbol, mid, price);
            if ns > 0 {
                println!("Last update: {} ns", ns);
            }
        }
    } else {
        let prices = db.all_prices();
        if json {
            let arr: Vec<_> = prices
                .iter()
                .map(|(mid, p)| {
                    let meta = market_by_id(*mid).unwrap();
                    serde_json::json!({
                        "market_id": mid,
                        "symbol": meta.symbol,
                        "price": p,
                    })
                })
                .collect();
            println!("{}", serde_json::to_string_pretty(&arr).unwrap());
        } else {
            if prices.is_empty() {
                println!("No prices available.");
                return Ok(());
            }
            println!("{:<4} {:<8} {:>16}", "ID", "Symbol", "Price");
            println!("{}", "-".repeat(30));
            for (mid, price) in &prices {
                let meta = market_by_id(*mid).unwrap();
                println!("{:<4} {:<8} {:>16.6}", mid, meta.symbol, price);
            }
        }
    }
    Ok(())
}

fn cmd_ticks(db: &PerfDb, market: &str, last: usize, json: bool) -> Result<(), String> {
    let mid = resolve_market(market)?;
    let meta = market_by_id(mid).unwrap();
    let all_ticks = db.ticks(mid);
    let start = all_ticks.len().saturating_sub(last);
    let ticks = &all_ticks[start..];

    if json {
        let arr: Vec<_> = ticks
            .iter()
            .map(|t| {
                serde_json::json!({
                    "timestamp_ns": t.timestamp_ns,
                    "market_id": t.market_id,
                    "price": t.price,
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!(
            "{} — last {} of {} ticks",
            meta.symbol,
            ticks.len(),
            all_ticks.len()
        );
        if ticks.is_empty() {
            println!("No ticks.");
            return Ok(());
        }
        println!("{:<22} {:>16}", "Timestamp (ns)", "Price");
        println!("{}", "-".repeat(40));
        for t in ticks {
            println!("{:<22} {:>16.6}", t.timestamp_ns, t.price);
        }
    }
    Ok(())
}

fn cmd_candles(
    db: &PerfDb,
    market: &str,
    timeframe: &str,
    last: usize,
    json: bool,
) -> Result<(), String> {
    let mid = resolve_market(market)?;
    let tf = resolve_timeframe(timeframe)?;
    let meta = market_by_id(mid).unwrap();
    let all = db.candles(mid, tf);
    let start = all.len().saturating_sub(last);
    let candles = &all[start..];

    if json {
        let arr: Vec<_> = candles
            .iter()
            .map(|c| {
                serde_json::json!({
                    "timestamp_ns": c.timestamp_ns,
                    "open": c.open,
                    "high": c.high,
                    "low": c.low,
                    "close": c.close,
                    "volume": c.volume,
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!(
            "{} {} — last {} of {} candles",
            meta.symbol,
            tf.as_str(),
            candles.len(),
            all.len()
        );
        if candles.is_empty() {
            println!("No candles.");
            return Ok(());
        }
        println!(
            "{:<22} {:>12} {:>12} {:>12} {:>12} {:>10}",
            "Timestamp (ns)", "Open", "High", "Low", "Close", "Volume"
        );
        println!("{}", "-".repeat(84));
        for c in candles {
            println!(
                "{:<22} {:>12.2} {:>12.2} {:>12.2} {:>12.2} {:>10.1}",
                c.timestamp_ns, c.open, c.high, c.low, c.close, c.volume
            );
        }

        if let Some(live) = db.live_candle(mid, tf) {
            println!();
            println!(
                "Live:  O={:.2} H={:.2} L={:.2} C={:.2}",
                live.open, live.high, live.low, live.close
            );
        }
    }
    Ok(())
}

fn cmd_positions(
    db: &PerfDb,
    user: Option<&str>,
    market: Option<&str>,
    id: Option<&str>,
    open_only: bool,
    json: bool,
) -> Result<(), String> {
    let positions = if let Some(id_str) = id {
        let pid = parse_hash(id_str);
        db.positions.get(&pid).into_iter().collect::<Vec<_>>()
    } else if let Some(u) = user {
        let addr = parse_addr(u);
        if open_only {
            db.positions.open_by_user(&addr)
        } else {
            db.positions.by_user(&addr)
        }
    } else if let Some(m) = market {
        let mid = resolve_market(m)?;
        if open_only {
            db.positions.open_by_market(mid)
        } else {
            db.positions.by_market(mid)
        }
    } else {
        let all = db.positions.all();
        if open_only {
            all.into_iter()
                .filter(|p| matches!(p.status, perfdb::core::types::PositionStatus::Open))
                .collect()
        } else {
            all
        }
    };

    if json {
        let arr: Vec<_> = positions
            .iter()
            .map(|p| {
                serde_json::json!({
                    "position_id": hex_hash(&p.position_id),
                    "user": hex_addr(&p.user_address),
                    "market_id": p.market_id,
                    "is_long": p.is_long,
                    "size_usd": format_fixed(p.size_usd),
                    "entry_price": format_fixed(p.entry_price),
                    "leverage": format_fixed(p.leverage),
                    "status": format!("{:?}", p.status),
                    "opened_at": p.opened_at,
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!("{} position(s)", positions.len());
        if positions.is_empty() {
            return Ok(());
        }
        println!(
            "{:<14} {:<14} {:<5} {:<5} {:>14} {:>14} {:>6} {:<10}",
            "ID", "User", "Mkt", "Side", "Size", "Entry", "Lev", "Status"
        );
        println!("{}", "-".repeat(90));
        for p in &positions {
            let market_sym = market_by_id(p.market_id)
                .map(|m| m.symbol)
                .unwrap_or("???");
            let side = if p.is_long { "LONG" } else { "SHORT" };
            let status = format!("{:?}", p.status);
            println!(
                "{:<14} {:<14} {:<5} {:<5} {:>14} {:>14} {:>6} {:<10}",
                &hex_hash(&p.position_id)[..14],
                short_addr(&p.user_address),
                market_sym,
                side,
                format_fixed(p.size_usd),
                format_fixed(p.entry_price),
                format_fixed(p.leverage),
                status,
            );
        }
    }
    Ok(())
}

fn cmd_orders(
    db: &PerfDb,
    user: Option<&str>,
    id: Option<&str>,
    active_only: bool,
    json: bool,
) -> Result<(), String> {
    let orders = if let Some(id_str) = id {
        let oid = parse_hash(id_str);
        db.orders.get(&oid).into_iter().collect::<Vec<_>>()
    } else if let Some(u) = user {
        let addr = parse_addr(u);
        if active_only {
            db.orders.active_by_user(&addr)
        } else {
            db.orders.by_user(&addr)
        }
    } else {
        let all = db.orders.all();
        if active_only {
            all.into_iter()
                .filter(|o| matches!(o.status, perfdb::core::types::OrderStatus::Active))
                .collect()
        } else {
            all
        }
    };

    if json {
        let arr: Vec<_> = orders
            .iter()
            .map(|o| {
                serde_json::json!({
                    "order_id": hex_hash(&o.order_id),
                    "user": hex_addr(&o.user_address),
                    "market_id": o.market_id,
                    "is_long": o.is_long,
                    "type": format!("{:?}", o.order_type),
                    "trigger_price": format_fixed(o.trigger_price),
                    "size_usd": format_fixed(o.size_usd),
                    "status": format!("{:?}", o.status),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!("{} order(s)", orders.len());
        if orders.is_empty() {
            return Ok(());
        }
        println!(
            "{:<14} {:<14} {:<5} {:<5} {:<10} {:>14} {:>14} {:<10}",
            "ID", "User", "Mkt", "Side", "Type", "Trigger", "Size", "Status"
        );
        println!("{}", "-".repeat(94));
        for o in &orders {
            let market_sym = market_by_id(o.market_id)
                .map(|m| m.symbol)
                .unwrap_or("???");
            let side = if o.is_long { "LONG" } else { "SHORT" };
            println!(
                "{:<14} {:<14} {:<5} {:<5} {:<10} {:>14} {:>14} {:<10}",
                &hex_hash(&o.order_id)[..14],
                short_addr(&o.user_address),
                market_sym,
                side,
                format!("{:?}", o.order_type),
                format_fixed(o.trigger_price),
                format_fixed(o.size_usd),
                format!("{:?}", o.status),
            );
        }
    }
    Ok(())
}

fn cmd_orderbook(db: &PerfDb, market: &str, levels: usize, json: bool) -> Result<(), String> {
    let mid = resolve_market(market)?;
    let meta = market_by_id(mid).unwrap();

    let book = db
        .orders
        .orderbook(mid)
        .ok_or_else(|| format!("no orderbook for market {}", mid))?;

    let bids = book.bids_depth(levels);
    let asks = book.asks_depth(levels);
    let best_bid = book.best_bid();
    let best_ask = book.best_ask();
    let spread = book.spread();

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "market": meta.symbol,
                "best_bid": best_bid.map(format_fixed),
                "best_ask": best_ask.map(format_fixed),
                "spread": spread.map(format_fixed),
                "bids": bids.iter().map(|l| serde_json::json!({
                    "price": format_fixed(l.price),
                    "orders": l.order_count,
                })).collect::<Vec<_>>(),
                "asks": asks.iter().map(|l| serde_json::json!({
                    "price": format_fixed(l.price),
                    "orders": l.order_count,
                })).collect::<Vec<_>>(),
            }))
            .unwrap()
        );
    } else {
        println!("{} Orderbook", meta.symbol);
        println!("{}", "=".repeat(40));

        if let (Some(bb), Some(ba)) = (best_bid, best_ask) {
            println!(
                "Spread: {} (bid: {} / ask: {})",
                spread.map(format_fixed).unwrap_or_default(),
                format_fixed(bb),
                format_fixed(ba)
            );
            println!();
        }

        println!("ASKS (lowest first):");
        println!("{:>20} {:>8}", "Price", "Orders");
        println!("{}", "-".repeat(30));
        for level in asks.iter().rev() {
            println!("{:>20} {:>8}", format_fixed(level.price), level.order_count);
        }

        println!("{}", "=".repeat(30));

        println!("BIDS (highest first):");
        println!("{:>20} {:>8}", "Price", "Orders");
        println!("{}", "-".repeat(30));
        for level in &bids {
            println!("{:>20} {:>8}", format_fixed(level.price), level.order_count);
        }
    }
    Ok(())
}

fn cmd_balances(db: &PerfDb, user: &str, json: bool) -> Result<(), String> {
    let addr = parse_addr(user);
    let balances = db.balances.user_balances(&addr);

    if json {
        let arr: Vec<_> = balances
            .iter()
            .map(|b| {
                serde_json::json!({
                    "token": hex_addr(&b.token),
                    "amount": format_fixed(b.amount),
                    "locked": format_fixed(b.locked),
                    "available": format_fixed(b.available),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!("Balances for {}", hex_addr(&addr));
        if balances.is_empty() {
            println!("  (none)");
            return Ok(());
        }
        println!(
            "{:<44} {:>20} {:>20} {:>20}",
            "Token", "Amount", "Locked", "Available"
        );
        println!("{}", "-".repeat(106));
        for b in &balances {
            println!(
                "{:<44} {:>20} {:>20} {:>20}",
                hex_addr(&b.token),
                format_fixed(b.amount),
                format_fixed(b.locked),
                format_fixed(b.available),
            );
        }
    }
    Ok(())
}

fn cmd_user(db: &PerfDb, address: &str, json: bool) -> Result<(), String> {
    let addr = parse_addr(address);
    let user = db.users.get(&addr).ok_or("user not found")?;

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "address": hex_addr(&user.address),
                "vault": user.vault_address.as_ref().map(hex_addr),
                "referral_code": user.referral_code,
                "referred_by": user.referred_by.as_ref().map(hex_addr),
                "tier": user.tier,
                "fee_discount_bps": user.fee_discount_bps,
                "created_at": user.created_at,
                "first_trade_at": user.first_trade_at,
                "last_active_at": user.last_active_at,
                "referral_count": db.users.referral_count(&addr),
            }))
            .unwrap()
        );
    } else {
        println!("User: {}", hex_addr(&user.address));
        println!("{}", "=".repeat(50));
        println!("{:<20} {}", "Tier:", user.tier);
        println!("{:<20} {} bps", "Fee discount:", user.fee_discount_bps);
        if let Some(ref code) = user.referral_code {
            println!("{:<20} {}", "Referral code:", code);
        }
        if let Some(ref referrer) = user.referred_by {
            println!("{:<20} {}", "Referred by:", hex_addr(referrer));
        }
        println!(
            "{:<20} {}",
            "Referrals:",
            db.users.referral_count(&addr)
        );
        if let Some(ref vault) = user.vault_address {
            println!("{:<20} {}", "Vault:", hex_addr(vault));
        }
        println!("{:<20} {}", "Created:", user.created_at);
        if let Some(ft) = user.first_trade_at {
            println!("{:<20} {}", "First trade:", ft);
        }
        if let Some(la) = user.last_active_at {
            println!("{:<20} {}", "Last active:", la);
        }
    }
    Ok(())
}

fn cmd_stats(db: &PerfDb, address: &str, json: bool) -> Result<(), String> {
    let addr = parse_addr(address);
    let stats = db
        .trader_stats
        .get(&addr)
        .ok_or("no stats for this trader")?;

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "address": hex_addr(&stats.address),
                "total_pnl": format_fixed(stats.total_pnl),
                "total_volume": format_fixed(stats.total_volume),
                "total_fees_paid": format_fixed(stats.total_fees_paid),
                "total_trades": stats.total_trades,
                "total_positions": stats.total_positions,
                "open_positions": stats.open_positions,
                "win_count": stats.win_count,
                "loss_count": stats.loss_count,
                "liquidation_count": stats.liquidation_count,
                "best_trade_pnl": format_fixed(stats.best_trade_pnl),
                "worst_trade_pnl": format_fixed(stats.worst_trade_pnl),
                "win_rate": if stats.total_trades > 0 {
                    format!("{:.1}%", stats.win_count as f64 / stats.total_trades as f64 * 100.0)
                } else {
                    "N/A".to_string()
                },
            }))
            .unwrap()
        );
    } else {
        println!("Trader Stats: {}", short_addr(&stats.address));
        println!("{}", "=".repeat(50));
        println!("{:<24} {}", "Total PnL:", format_fixed(stats.total_pnl));
        println!("{:<24} {}", "Total volume:", format_fixed(stats.total_volume));
        println!("{:<24} {}", "Total fees:", format_fixed(stats.total_fees_paid));
        println!("{:<24} {}", "Trades:", stats.total_trades);
        println!("{:<24} {}", "Positions (total):", stats.total_positions);
        println!("{:<24} {}", "Positions (open):", stats.open_positions);
        println!(
            "{:<24} {} / {} / {}",
            "W / L / Liq:",
            stats.win_count,
            stats.loss_count,
            stats.liquidation_count
        );
        if stats.total_trades > 0 {
            let wr = stats.win_count as f64 / stats.total_trades as f64 * 100.0;
            println!("{:<24} {:.1}%", "Win rate:", wr);
        }
        println!("{:<24} {}", "Best trade:", format_fixed(stats.best_trade_pnl));
        println!("{:<24} {}", "Worst trade:", format_fixed(stats.worst_trade_pnl));
    }
    Ok(())
}

fn cmd_daily(db: &PerfDb, address: &str, limit: usize, json: bool) -> Result<(), String> {
    let addr = parse_addr(address);
    let history = db.daily_stats.trader_daily_history(&addr, limit);

    if json {
        let arr: Vec<_> = history
            .iter()
            .map(|d| {
                serde_json::json!({
                    "date": d.date,
                    "pnl": format_fixed(d.pnl),
                    "volume": format_fixed(d.volume),
                    "trades": d.trades,
                    "fees": format_fixed(d.fees),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!("Daily PnL for {}", short_addr(&addr));
        if history.is_empty() {
            println!("  (no data)");
            return Ok(());
        }
        println!(
            "{:<12} {:>20} {:>20} {:>8} {:>20}",
            "Date", "PnL", "Volume", "Trades", "Fees"
        );
        println!("{}", "-".repeat(84));
        for d in &history {
            println!(
                "{:<12} {:>20} {:>20} {:>8} {:>20}",
                d.date,
                format_fixed(d.pnl),
                format_fixed(d.volume),
                d.trades,
                format_fixed(d.fees),
            );
        }
    }
    Ok(())
}

fn cmd_leaderboard(db: &PerfDb, metric: &str, limit: usize, json: bool) -> Result<(), String> {
    db.rebuild_leaderboard(0);

    let entries = match metric.to_lowercase().as_str() {
        "pnl" => db.pnl_leaderboard(limit),
        "volume" | "vol" => db.volume_leaderboard(limit),
        _ => return Err(format!("unknown metric: {metric} (use 'pnl' or 'volume')")),
    };

    if json {
        let arr: Vec<_> = entries
            .iter()
            .map(|e| {
                serde_json::json!({
                    "rank": e.rank,
                    "address": hex_addr(&e.address),
                    "value": format_fixed(e.value),
                    "trades": e.total_trades,
                    "wins": e.win_count,
                    "losses": e.loss_count,
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        let title = match metric.to_lowercase().as_str() {
            "pnl" => "PnL Leaderboard",
            _ => "Volume Leaderboard",
        };
        println!("{}", title);
        if entries.is_empty() {
            println!("  (empty)");
            return Ok(());
        }
        println!(
            "{:<6} {:<14} {:>20} {:>8} {:>6} {:>6}",
            "Rank", "Address", "Value", "Trades", "Wins", "Loss"
        );
        println!("{}", "-".repeat(64));
        for e in &entries {
            println!(
                "{:<6} {:<14} {:>20} {:>8} {:>6} {:>6}",
                e.rank,
                short_addr(&e.address),
                format_fixed(e.value),
                e.total_trades,
                e.win_count,
                e.loss_count,
            );
        }
    }
    Ok(())
}

fn cmd_events(
    db: &PerfDb,
    market: Option<&str>,
    trader: Option<&str>,
    last: usize,
    json: bool,
) -> Result<(), String> {
    let events = if let Some(m) = market {
        let mid = resolve_market(m)?;
        db.events.latest_by_market(mid, last)
    } else if let Some(t) = trader {
        let addr = parse_addr(t);
        db.events.latest_by_trader(&addr, last)
    } else {
        return Err("specify --market or --trader".to_string());
    };

    if json {
        let arr: Vec<_> = events
            .iter()
            .map(|e| {
                serde_json::json!({
                    "block": e.block_number,
                    "timestamp": e.block_timestamp,
                    "tx": hex_hash(&e.tx_hash),
                    "event": format!("{:?}", e.event),
                })
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&arr).unwrap());
    } else {
        println!("{} event(s)", events.len());
        if events.is_empty() {
            return Ok(());
        }
        println!(
            "{:<10} {:<14} {:<14} {}",
            "Block", "Timestamp", "Tx", "Event"
        );
        println!("{}", "-".repeat(80));
        for e in &events {
            let event_name = event_short_name(&e.event);
            println!(
                "{:<10} {:<14} {:<14} {}",
                e.block_number,
                e.block_timestamp,
                &hex_hash(&e.tx_hash)[..14],
                event_name,
            );
        }
    }
    Ok(())
}

fn event_short_name(event: &perfdb::core::types::Event) -> &'static str {
    match event {
        perfdb::core::types::Event::PositionOpened { .. } => "PositionOpened",
        perfdb::core::types::Event::PositionClosed { .. } => "PositionClosed",
        perfdb::core::types::Event::PositionModified { .. } => "PositionModified",
        perfdb::core::types::Event::OrderPlaced { .. } => "OrderPlaced",
        perfdb::core::types::Event::OrderExecuted { .. } => "OrderExecuted",
        perfdb::core::types::Event::OrderCancelled { .. } => "OrderCancelled",
        perfdb::core::types::Event::Liquidation { .. } => "Liquidation",
        perfdb::core::types::Event::ADLExecuted { .. } => "ADLExecuted",
        perfdb::core::types::Event::PriceUpdated { .. } => "PriceUpdated",
        perfdb::core::types::Event::FundingSettled { .. } => "FundingSettled",
        perfdb::core::types::Event::FundingRateUpdated { .. } => "FundingRateUpdated",
        perfdb::core::types::Event::MarketCreated { .. } => "MarketCreated",
        perfdb::core::types::Event::MarketPaused { .. } => "MarketPaused",
        perfdb::core::types::Event::CollateralDeposited { .. } => "CollateralDeposited",
        perfdb::core::types::Event::CollateralWithdrawn { .. } => "CollateralWithdrawn",
        perfdb::core::types::Event::VaultCreated { .. } => "VaultCreated",
        perfdb::core::types::Event::VaultDeficit { .. } => "VaultDeficit",
    }
}

fn cmd_checkpoint(db: &PerfDb, json: bool) -> Result<(), String> {
    let cp = db.checkpoint.snapshot();

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "last_block": cp.last_block,
                "last_block_hash": cp.last_block_hash,
                "last_block_timestamp": cp.last_block_timestamp,
            }))
            .unwrap()
        );
    } else {
        println!("Indexer Checkpoint");
        println!("{}", "=".repeat(40));
        println!("{:<24} {}", "Block:", cp.last_block);
        if !cp.last_block_hash.is_empty() {
            println!("{:<24} {}", "Hash:", cp.last_block_hash);
        }
        println!("{:<24} {}", "Timestamp:", cp.last_block_timestamp);
    }
    Ok(())
}

fn cmd_snapshot(db: &PerfDb) -> Result<(), String> {
    match db.snapshot() {
        Ok(path) => {
            println!("Snapshot created: {}", path.display());
            Ok(())
        }
        Err(e) => Err(format!("snapshot failed: {e}")),
    }
}
