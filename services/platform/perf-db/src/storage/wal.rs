use std::fs::{self, File, OpenOptions};
use std::io::{BufWriter, Read, Write};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use crc32fast::Hasher as Crc32Hasher;
use serde::{Deserialize, Serialize};

use crate::core::types::IndexedEvent;
use crate::error::PerfDbError;

/// Every mutation in PerfDB is first written to the WAL as a `WalEntry`.
///
/// This is the complete vocabulary of state changes, covering all 4 replaced
/// databases.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum WalEntry {
    /// Chain event from the indexer (covers all 17 DecodedEvent variants).
    IndexedEvent(IndexedEvent),

    /// Raw price tick from the price feed.
    PriceTick {
        timestamp_ns: u64,
        market_id: u16,
        price: f64,
    },

    /// Pre-computed candle update.
    CandleUpdate {
        timestamp_ns: u64,
        market_id: u16,
        timeframe: u8,
        open: f64,
        high: f64,
        low: f64,
        close: f64,
        volume: f32,
    },

    /// User balance change.
    BalanceUpdate {
        user: [u8; 20],
        token: [u8; 20],
        amount: i128,
        locked: i128,
        available: i128,
    },

    /// Indexer checkpoint (last processed block).
    Checkpoint { block_number: u64 },

    /// Marks the end of a batch (used for batch fsync).
    BatchEnd,
}

/// WAL behavior configuration.
#[derive(Debug, Clone)]
pub struct WalConfig {
    /// Directory where WAL segment files are stored.
    pub dir: PathBuf,

    /// Maximum segment size before rotation (bytes).
    /// Default: 64 MB.
    pub max_segment_size: u64,

    /// Fsync policy.
    pub sync_policy: SyncPolicy,
}

#[derive(Debug, Clone, Copy)]
pub enum SyncPolicy {
    /// Fsync after every write. Safest. ~10μs per write.
    EveryWrite,

    /// Fsync every N milliseconds. Risk losing last batch on crash.
    Interval(Duration),

    /// Never fsync (OS decides). Fastest. Data loss on crash.
    None,
}

impl Default for WalConfig {
    fn default() -> Self {
        Self {
            dir: PathBuf::from("wal"),
            max_segment_size: 64 * 1024 * 1024,
            sync_policy: SyncPolicy::Interval(Duration::from_millis(10)),
        }
    }
}

/// On-disk entry format:
/// ```text
/// [len: u32 LE] [crc32: u32 LE] [type: u8] [payload: len bytes]
/// ```
///
/// `type` is the bincode tag for the `WalEntry` enum (first byte of bincode output).
/// `payload` is the full bincode-serialized `WalEntry`.
/// `len` is the byte length of `payload`.
/// `crc32` covers `[type][payload]`.
const ENTRY_HEADER_SIZE: usize = 4 + 4; // len + crc32

pub struct WalWriter {
    config: WalConfig,
    current_segment: u64,
    writer: BufWriter<File>,
    segment_bytes: u64,
    last_sync: Instant,
}

impl WalWriter {
    /// Open or create a WAL in the given directory.
    pub fn open(config: WalConfig) -> Result<Self, PerfDbError> {
        fs::create_dir_all(&config.dir).map_err(|e| PerfDbError::Io {
            context: format!("create WAL dir: {}", config.dir.display()),
            source: e,
        })?;

        let current_segment = find_latest_segment(&config.dir)?;
        let seg_path = segment_path(&config.dir, current_segment);

        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&seg_path)
            .map_err(|e| PerfDbError::Io {
                context: format!("open WAL segment: {}", seg_path.display()),
                source: e,
            })?;

        let segment_bytes = file.metadata().map_err(|e| PerfDbError::Io {
            context: "WAL segment metadata".into(),
            source: e,
        })?.len();

        Ok(Self {
            config,
            current_segment,
            writer: BufWriter::with_capacity(64 * 1024, file),
            segment_bytes,
            last_sync: Instant::now(),
        })
    }

    /// Append a WAL entry.
    pub fn append(&mut self, entry: &WalEntry) -> Result<(), PerfDbError> {
        let payload = bincode::serialize(entry).map_err(|e| PerfDbError::Serialization {
            context: "WAL entry serialize".into(),
            detail: e.to_string(),
        })?;

        let len = payload.len() as u32;

        let mut hasher = Crc32Hasher::new();
        hasher.update(&payload);
        let crc = hasher.finalize();

        self.writer.write_all(&len.to_le_bytes()).map_err(|e| PerfDbError::Io {
            context: "WAL write len".into(),
            source: e,
        })?;
        self.writer.write_all(&crc.to_le_bytes()).map_err(|e| PerfDbError::Io {
            context: "WAL write crc".into(),
            source: e,
        })?;
        self.writer.write_all(&payload).map_err(|e| PerfDbError::Io {
            context: "WAL write payload".into(),
            source: e,
        })?;

        self.segment_bytes += ENTRY_HEADER_SIZE as u64 + payload.len() as u64;

        match self.config.sync_policy {
            SyncPolicy::EveryWrite => {
                self.sync()?;
            }
            SyncPolicy::Interval(d) => {
                if self.last_sync.elapsed() >= d {
                    self.sync()?;
                }
            }
            SyncPolicy::None => {}
        }

        if self.segment_bytes >= self.config.max_segment_size {
            self.rotate()?;
        }

        Ok(())
    }

    /// Force flush + fsync.
    pub fn sync(&mut self) -> Result<(), PerfDbError> {
        self.writer.flush().map_err(|e| PerfDbError::Io {
            context: "WAL flush".into(),
            source: e,
        })?;
        self.writer.get_ref().sync_data().map_err(|e| PerfDbError::Io {
            context: "WAL fsync".into(),
            source: e,
        })?;
        self.last_sync = Instant::now();
        Ok(())
    }

    /// Current segment number.
    pub fn current_segment(&self) -> u64 {
        self.current_segment
    }

    /// Total bytes written to current segment.
    pub fn segment_bytes(&self) -> u64 {
        self.segment_bytes
    }

    fn rotate(&mut self) -> Result<(), PerfDbError> {
        self.sync()?;

        self.current_segment += 1;
        let seg_path = segment_path(&self.config.dir, self.current_segment);

        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&seg_path)
            .map_err(|e| PerfDbError::Io {
                context: format!("open new WAL segment: {}", seg_path.display()),
                source: e,
            })?;

        self.writer = BufWriter::with_capacity(64 * 1024, file);
        self.segment_bytes = 0;

        tracing::info!(segment = self.current_segment, "WAL segment rotated");

        Ok(())
    }
}

/// Replay all WAL entries from all segments in order.
///
/// Calls `callback` for each valid entry. Stops on the first corrupted entry
/// within a segment (truncation-safe: partial writes at the end are ignored).
pub fn replay(
    dir: impl AsRef<Path>,
    mut callback: impl FnMut(WalEntry) -> Result<(), PerfDbError>,
) -> Result<u64, PerfDbError> {
    let dir = dir.as_ref();
    let mut segments = list_segments(dir)?;
    segments.sort();

    let mut count = 0u64;

    for seg_id in segments {
        let seg_path = segment_path(dir, seg_id);
        let mut file = File::open(&seg_path).map_err(|e| PerfDbError::Io {
            context: format!("open WAL segment for replay: {}", seg_path.display()),
            source: e,
        })?;

        let file_len = file.metadata().map_err(|e| PerfDbError::Io {
            context: "segment metadata".into(),
            source: e,
        })?.len();

        let mut pos = 0u64;

        while pos + ENTRY_HEADER_SIZE as u64 <= file_len {
            let mut header = [0u8; ENTRY_HEADER_SIZE];
            if file.read_exact(&mut header).is_err() {
                break;
            }

            let len = u32::from_le_bytes(header[0..4].try_into().unwrap()) as u64;
            let expected_crc = u32::from_le_bytes(header[4..8].try_into().unwrap());

            if len > 128 * 1024 * 1024 {
                tracing::warn!(
                    segment = seg_id,
                    offset = pos,
                    len = len,
                    "WAL entry too large, stopping replay at this segment"
                );
                break;
            }

            if pos + ENTRY_HEADER_SIZE as u64 + len > file_len {
                break;
            }

            let mut payload = vec![0u8; len as usize];
            if file.read_exact(&mut payload).is_err() {
                break;
            }

            let mut hasher = Crc32Hasher::new();
            hasher.update(&payload);
            let actual_crc = hasher.finalize();

            if actual_crc != expected_crc {
                tracing::warn!(
                    segment = seg_id,
                    offset = pos,
                    expected_crc,
                    actual_crc,
                    "WAL CRC mismatch, stopping replay at this segment"
                );
                break;
            }

            match bincode::deserialize::<WalEntry>(&payload) {
                Ok(entry) => {
                    callback(entry)?;
                    count += 1;
                }
                Err(e) => {
                    tracing::warn!(
                        segment = seg_id,
                        offset = pos,
                        error = %e,
                        "WAL entry deserialize failed, stopping replay"
                    );
                    break;
                }
            }

            pos += ENTRY_HEADER_SIZE as u64 + len;
        }
    }

    Ok(count)
}

/// Delete all segments before (not including) the given segment number.
/// Used after a successful checkpoint/snapshot to reclaim disk space.
pub fn truncate_before(dir: impl AsRef<Path>, keep_from: u64) -> Result<u64, PerfDbError> {
    let dir = dir.as_ref();
    let segments = list_segments(dir)?;
    let mut removed = 0u64;

    for seg_id in segments {
        if seg_id < keep_from {
            let path = segment_path(dir, seg_id);
            fs::remove_file(&path).map_err(|e| PerfDbError::Io {
                context: format!("remove WAL segment: {}", path.display()),
                source: e,
            })?;
            removed += 1;
        }
    }

    if removed > 0 {
        tracing::info!(removed, keep_from, "WAL segments truncated");
    }

    Ok(removed)
}

fn segment_path(dir: &Path, id: u64) -> PathBuf {
    dir.join(format!("wal_{:08}.bin", id))
}

fn list_segments(dir: &Path) -> Result<Vec<u64>, PerfDbError> {
    if !dir.exists() {
        return Ok(Vec::new());
    }

    let mut ids = Vec::new();

    for entry in fs::read_dir(dir).map_err(|e| PerfDbError::Io {
        context: format!("read WAL dir: {}", dir.display()),
        source: e,
    })? {
        let entry = entry.map_err(|e| PerfDbError::Io {
            context: "read WAL dir entry".into(),
            source: e,
        })?;

        let name = entry.file_name();
        let name = name.to_string_lossy();

        if let Some(num_str) = name.strip_prefix("wal_").and_then(|s| s.strip_suffix(".bin")) {
            if let Ok(id) = num_str.parse::<u64>() {
                ids.push(id);
            }
        }
    }

    Ok(ids)
}

fn find_latest_segment(dir: &Path) -> Result<u64, PerfDbError> {
    let segments = list_segments(dir)?;
    Ok(segments.into_iter().max().unwrap_or(0))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_config(dir: &Path) -> WalConfig {
        WalConfig {
            dir: dir.to_path_buf(),
            max_segment_size: 1024, // small for testing rotation
            sync_policy: SyncPolicy::EveryWrite,
        }
    }

    #[test]
    fn write_and_replay() {
        let dir = tempfile::tempdir().unwrap();
        let config = test_config(dir.path());

        {
            let mut wal = WalWriter::open(config.clone()).unwrap();

            wal.append(&WalEntry::PriceTick {
                timestamp_ns: 1_000_000,
                market_id: 0,
                price: 67_000.0,
            }).unwrap();

            wal.append(&WalEntry::Checkpoint { block_number: 42 }).unwrap();

            wal.sync().unwrap();
        }

        let mut entries = Vec::new();
        let count = replay(dir.path(), |e| {
            entries.push(e);
            Ok(())
        }).unwrap();

        assert_eq!(count, 2);

        match &entries[0] {
            WalEntry::PriceTick { market_id, price, .. } => {
                assert_eq!(*market_id, 0);
                assert_eq!(*price, 67_000.0);
            }
            other => panic!("unexpected entry: {other:?}"),
        }

        match &entries[1] {
            WalEntry::Checkpoint { block_number } => {
                assert_eq!(*block_number, 42);
            }
            other => panic!("unexpected entry: {other:?}"),
        }
    }

    #[test]
    fn segment_rotation() {
        let dir = tempfile::tempdir().unwrap();
        let config = WalConfig {
            dir: dir.path().to_path_buf(),
            max_segment_size: 200, // very small to force rotation
            sync_policy: SyncPolicy::EveryWrite,
        };

        let mut wal = WalWriter::open(config).unwrap();
        assert_eq!(wal.current_segment(), 0);

        for i in 0..50 {
            wal.append(&WalEntry::PriceTick {
                timestamp_ns: i * 1_000_000,
                market_id: 0,
                price: 60_000.0 + i as f64,
            }).unwrap();
        }

        assert!(wal.current_segment() > 0, "expected rotation, got segment 0");

        let mut count = 0u64;
        replay(dir.path(), |_| { count += 1; Ok(()) }).unwrap();
        assert_eq!(count, 50);
    }

    #[test]
    fn truncation() {
        let dir = tempfile::tempdir().unwrap();
        let config = WalConfig {
            dir: dir.path().to_path_buf(),
            max_segment_size: 100,
            sync_policy: SyncPolicy::EveryWrite,
        };

        let mut wal = WalWriter::open(config).unwrap();

        for i in 0..100 {
            wal.append(&WalEntry::PriceTick {
                timestamp_ns: i,
                market_id: 0,
                price: 1.0,
            }).unwrap();
        }
        wal.sync().unwrap();

        let current = wal.current_segment();
        assert!(current > 2);

        let removed = truncate_before(dir.path(), current).unwrap();
        assert!(removed > 0);

        let remaining = list_segments(dir.path()).unwrap();
        for seg_id in &remaining {
            assert!(*seg_id >= current);
        }
    }

    #[test]
    fn corrupted_entry_stops_replay() {
        let dir = tempfile::tempdir().unwrap();
        let seg_path = segment_path(dir.path(), 0);

        {
            let config = test_config(dir.path());
            let mut wal = WalWriter::open(config).unwrap();
            wal.append(&WalEntry::Checkpoint { block_number: 1 }).unwrap();
            wal.sync().unwrap();
        }

        {
            let mut f = OpenOptions::new().append(true).open(&seg_path).unwrap();
            f.write_all(&[0xFF; 50]).unwrap();
        }

        let mut count = 0u64;
        replay(dir.path(), |_| { count += 1; Ok(()) }).unwrap();
        assert_eq!(count, 1);
    }
}
