use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::core::types::{
    Balance, DailyStats, Order, Position, TraderStats, User,
};
use crate::error::PerfDbError;
use crate::relational::checkpoint::Checkpoint;
use crate::state::market_state::MarketSnapshot;

/// Complete serializable snapshot of all in-memory state.
///
/// Used for fast recovery: load last snapshot, then replay WAL entries
/// written after the snapshot's checkpoint block.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StateSnapshot {
    pub checkpoint: SnapshotCheckpoint,
    pub positions: Vec<Position>,
    pub orders: Vec<Order>,
    pub balances: Vec<Balance>,
    pub users: Vec<User>,
    pub trader_stats: Vec<TraderStats>,
    pub daily_stats: Vec<DailyStatsEntry>,
    pub market_states: Vec<SnapshotMarketState>,
    pub snapshot_timestamp: u64,
}

/// Checkpoint info embedded in the snapshot.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SnapshotCheckpoint {
    pub last_block: u64,
    pub last_block_hash: String,
    pub last_block_timestamp: u64,
}

impl From<Checkpoint> for SnapshotCheckpoint {
    fn from(cp: Checkpoint) -> Self {
        Self {
            last_block: cp.last_block,
            last_block_hash: cp.last_block_hash,
            last_block_timestamp: cp.last_block_timestamp,
        }
    }
}

/// Daily stats entry with the address included (the key is stripped
/// from DashMap on snapshot).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DailyStatsEntry {
    pub address: [u8; 20],
    pub stats: DailyStats,
}

/// Serializable version of MarketSnapshot (the original isn't Serialize).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SnapshotMarketState {
    pub market_id: u16,
    pub latest_price: f64,
    pub last_update_ns: u64,
    pub mark_price: Option<i128>,
    pub index_price: Option<i128>,
    pub funding_rate_per_second: i128,
    pub funding_rate_24h: i128,
    pub long_oi: i128,
    pub short_oi: i128,
    pub enabled: bool,
    pub paused: bool,
    pub max_leverage: Option<i128>,
    pub maintenance_margin_bps: Option<u32>,
    pub volume_24h: Option<i128>,
    pub trades_24h: Option<u32>,
    pub price_change_24h: Option<i128>,
    pub price_change_pct_24h: Option<i128>,
}

impl From<MarketSnapshot> for SnapshotMarketState {
    fn from(ms: MarketSnapshot) -> Self {
        Self {
            market_id: ms.market_id,
            latest_price: ms.latest_price,
            last_update_ns: ms.last_update_ns,
            mark_price: ms.mark_price,
            index_price: ms.index_price,
            funding_rate_per_second: ms.funding_rate_per_second,
            funding_rate_24h: ms.funding_rate_24h,
            long_oi: ms.long_oi,
            short_oi: ms.short_oi,
            enabled: ms.enabled,
            paused: ms.paused,
            max_leverage: ms.max_leverage,
            maintenance_margin_bps: ms.maintenance_margin_bps,
            volume_24h: ms.volume_24h,
            trades_24h: ms.trades_24h,
            price_change_24h: ms.price_change_24h,
            price_change_pct_24h: ms.price_change_pct_24h,
        }
    }
}

/// Write a snapshot to disk as `snapshot_{block}.bin` in the given directory.
pub fn write_snapshot(
    dir: &Path,
    snapshot: &StateSnapshot,
) -> Result<PathBuf, PerfDbError> {
    fs::create_dir_all(dir).map_err(|e| PerfDbError::Io {
        context: format!("create snapshot dir: {}", dir.display()),
        source: e,
    })?;

    let filename = format!("snapshot_{:012}.bin", snapshot.checkpoint.last_block);
    let path = dir.join(&filename);

    let data = bincode::serialize(snapshot).map_err(|e| PerfDbError::Serialization {
        context: "snapshot serialize".into(),
        detail: e.to_string(),
    })?;

    fs::write(&path, &data).map_err(|e| PerfDbError::Io {
        context: format!("write snapshot: {}", path.display()),
        source: e,
    })?;

    tracing::info!(
        block = snapshot.checkpoint.last_block,
        size_bytes = data.len(),
        path = %path.display(),
        "snapshot written"
    );

    Ok(path)
}

/// Load the latest snapshot from a directory. Returns `None` if no snapshots exist.
pub fn load_latest_snapshot(dir: &Path) -> Result<Option<StateSnapshot>, PerfDbError> {
    if !dir.exists() {
        return Ok(None);
    }

    let mut snapshots = list_snapshots(dir)?;
    if snapshots.is_empty() {
        return Ok(None);
    }

    snapshots.sort_by(|a, b| b.0.cmp(&a.0));

    let (block, path) = &snapshots[0];
    tracing::info!(block, path = %path.display(), "loading snapshot");

    let data = fs::read(path).map_err(|e| PerfDbError::Io {
        context: format!("read snapshot: {}", path.display()),
        source: e,
    })?;

    let snapshot: StateSnapshot =
        bincode::deserialize(&data).map_err(|e| PerfDbError::Serialization {
            context: "snapshot deserialize".into(),
            detail: e.to_string(),
        })?;

    Ok(Some(snapshot))
}

/// Load a specific snapshot by block number. Returns `None` if not found.
pub fn load_snapshot(dir: &Path, block: u64) -> Result<Option<StateSnapshot>, PerfDbError> {
    let filename = format!("snapshot_{:012}.bin", block);
    let path = dir.join(&filename);

    if !path.exists() {
        return Ok(None);
    }

    let data = fs::read(&path).map_err(|e| PerfDbError::Io {
        context: format!("read snapshot: {}", path.display()),
        source: e,
    })?;

    let snapshot: StateSnapshot =
        bincode::deserialize(&data).map_err(|e| PerfDbError::Serialization {
            context: "snapshot deserialize".into(),
            detail: e.to_string(),
        })?;

    Ok(Some(snapshot))
}

/// List all snapshot files in a directory as (block_number, path) pairs.
pub fn list_snapshots(dir: &Path) -> Result<Vec<(u64, PathBuf)>, PerfDbError> {
    if !dir.exists() {
        return Ok(Vec::new());
    }

    let mut results = Vec::new();

    for entry in fs::read_dir(dir).map_err(|e| PerfDbError::Io {
        context: format!("read snapshot dir: {}", dir.display()),
        source: e,
    })? {
        let entry = entry.map_err(|e| PerfDbError::Io {
            context: "read snapshot dir entry".into(),
            source: e,
        })?;

        let name = entry.file_name();
        let name = name.to_string_lossy();

        if let Some(num_str) = name
            .strip_prefix("snapshot_")
            .and_then(|s| s.strip_suffix(".bin"))
        {
            if let Ok(block) = num_str.parse::<u64>() {
                results.push((block, entry.path()));
            }
        }
    }

    Ok(results)
}

/// Remove all snapshots older than `keep_from_block`.
pub fn cleanup_snapshots(dir: &Path, keep_from_block: u64) -> Result<u64, PerfDbError> {
    let snapshots = list_snapshots(dir)?;
    let mut removed = 0u64;

    for (block, path) in snapshots {
        if block < keep_from_block {
            fs::remove_file(&path).map_err(|e| PerfDbError::Io {
                context: format!("remove snapshot: {}", path.display()),
                source: e,
            })?;
            removed += 1;
        }
    }

    if removed > 0 {
        tracing::info!(removed, keep_from_block, "old snapshots cleaned up");
    }

    Ok(removed)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_snapshot(block: u64) -> StateSnapshot {
        StateSnapshot {
            checkpoint: SnapshotCheckpoint {
                last_block: block,
                last_block_hash: format!("0x{:016x}", block),
                last_block_timestamp: block * 12,
            },
            positions: Vec::new(),
            orders: Vec::new(),
            balances: Vec::new(),
            users: Vec::new(),
            trader_stats: Vec::new(),
            daily_stats: Vec::new(),
            market_states: Vec::new(),
            snapshot_timestamp: block * 12,
        }
    }

    #[test]
    fn write_and_load_roundtrip() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        let snap = make_snapshot(100);
        let path = write_snapshot(&snap_dir, &snap).unwrap();
        assert!(path.exists());

        let loaded = load_latest_snapshot(&snap_dir).unwrap().unwrap();
        assert_eq!(loaded.checkpoint.last_block, 100);
        assert_eq!(loaded.checkpoint.last_block_hash, "0x0000000000000064");
    }

    #[test]
    fn load_latest_picks_highest_block() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        write_snapshot(&snap_dir, &make_snapshot(50)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(200)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(100)).unwrap();

        let loaded = load_latest_snapshot(&snap_dir).unwrap().unwrap();
        assert_eq!(loaded.checkpoint.last_block, 200);
    }

    #[test]
    fn load_specific_snapshot() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        write_snapshot(&snap_dir, &make_snapshot(100)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(200)).unwrap();

        let loaded = load_snapshot(&snap_dir, 100).unwrap().unwrap();
        assert_eq!(loaded.checkpoint.last_block, 100);

        assert!(load_snapshot(&snap_dir, 999).unwrap().is_none());
    }

    #[test]
    fn load_from_empty_dir() {
        let dir = tempfile::tempdir().unwrap();
        assert!(load_latest_snapshot(dir.path()).unwrap().is_none());
    }

    #[test]
    fn load_from_nonexistent_dir() {
        let dir = Path::new("/tmp/perfdb_test_nonexistent_snapshot_dir_xyz");
        assert!(load_latest_snapshot(dir).unwrap().is_none());
    }

    #[test]
    fn list_snapshots_sorted() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        write_snapshot(&snap_dir, &make_snapshot(300)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(100)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(200)).unwrap();

        let list = list_snapshots(&snap_dir).unwrap();
        assert_eq!(list.len(), 3);
    }

    #[test]
    fn cleanup_old_snapshots() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        write_snapshot(&snap_dir, &make_snapshot(100)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(200)).unwrap();
        write_snapshot(&snap_dir, &make_snapshot(300)).unwrap();

        let removed = cleanup_snapshots(&snap_dir, 200).unwrap();
        assert_eq!(removed, 1);

        let remaining = list_snapshots(&snap_dir).unwrap();
        assert_eq!(remaining.len(), 2);
        assert!(remaining.iter().all(|(b, _)| *b >= 200));
    }

    #[test]
    fn snapshot_with_data() {
        let dir = tempfile::tempdir().unwrap();
        let snap_dir = dir.path().join("snapshots");

        let mut snap = make_snapshot(42);
        snap.positions.push(Position {
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
            status: crate::core::types::PositionStatus::Open,
            open_tx: [4u8; 32],
            close_tx: None,
            open_block: 40,
            close_block: None,
            opened_at: 480,
            closed_at: None,
        });
        snap.users.push(User {
            address: [2u8; 20],
            vault_address: None,
            referral_code: Some("ABC".to_string()),
            referred_by: None,
            tier: "silver".to_string(),
            fee_discount_bps: 5,
            created_at: 100,
            first_trade_at: Some(200),
            last_active_at: Some(480),
        });
        snap.market_states.push(SnapshotMarketState {
            market_id: 0,
            latest_price: 67_000.5,
            last_update_ns: 42_000_000_000,
            mark_price: Some(67_000_000_000_000_000_000_000),
            index_price: None,
            funding_rate_per_second: 500,
            funding_rate_24h: 43_200_000,
            long_oi: 10_000_000,
            short_oi: 8_000_000,
            enabled: true,
            paused: false,
            max_leverage: Some(100_000_000_000_000_000_000),
            maintenance_margin_bps: Some(50),
            volume_24h: None,
            trades_24h: None,
            price_change_24h: None,
            price_change_pct_24h: None,
        });

        write_snapshot(&snap_dir, &snap).unwrap();

        let loaded = load_latest_snapshot(&snap_dir).unwrap().unwrap();
        assert_eq!(loaded.positions.len(), 1);
        assert_eq!(loaded.positions[0].market_id, 0);
        assert_eq!(loaded.users.len(), 1);
        assert_eq!(loaded.users[0].referral_code, Some("ABC".to_string()));
        assert_eq!(loaded.market_states.len(), 1);
        assert_eq!(loaded.market_states[0].latest_price, 67_000.5);
    }
}
