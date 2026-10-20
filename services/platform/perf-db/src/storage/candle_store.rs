use std::path::{Path, PathBuf};

use crate::core::market::{MarketId, MARKET_COUNT};
use crate::core::timeframe::Timeframe;
use crate::core::types::CandleRecord;
use crate::error::PerfDbError;
use crate::storage::mmap::MmapFile;

const TF_COUNT: usize = 16;

/// Pre-computed candle storage. One file per (market × timeframe) pair.
///
/// Directory layout:
/// ```text
/// {base_dir}/
///   m0000_tf00.candle    (BTC, 1s)
///   m0000_tf04.candle    (BTC, 1m)
///   m0001_tf10.candle    (ETH, 1h)
///   ...
/// ```
///
/// Records are `CandleRecord` (48 bytes), stored contiguously and sorted
/// by `timestamp_ns`.
///
/// Supports two write modes:
/// - `append`: adds a new candle (for finalized bucket closures)
/// - `upsert`: overwrites the last candle if same `timestamp_ns`, else appends
///
/// Concurrency: single writer, multiple readers (via mmap zero-copy).
pub struct CandleStore {
    base_dir: PathBuf,
    /// Flat array: `files[market_id * TF_COUNT + tf_index]`.
    files: Vec<Option<MmapFile>>,
}

impl CandleStore {
    /// Open or create the candle store directory.
    pub fn open(base_dir: impl AsRef<Path>) -> Result<Self, PerfDbError> {
        let base_dir = base_dir.as_ref().to_path_buf();

        std::fs::create_dir_all(&base_dir).map_err(|e| PerfDbError::Io {
            context: format!("create candle_store dir: {}", base_dir.display()),
            source: e,
        })?;

        let total = MARKET_COUNT * TF_COUNT;
        let mut files: Vec<Option<MmapFile>> = (0..total).map(|_| None).collect();

        for mid in 0..MARKET_COUNT as MarketId {
            for tf in Timeframe::ALL {
                let path = file_path(&base_dir, mid, tf);
                if path.exists() {
                    let idx = flat_index(mid, tf);
                    files[idx] = Some(MmapFile::open(&path, false)?);
                }
            }
        }

        Ok(Self { base_dir, files })
    }

    /// Append a finalized candle. No dedup — caller must ensure this is a new bucket.
    pub fn append(&mut self, candle: &CandleRecord) -> Result<(), PerfDbError> {
        let tf = Timeframe::from_u8(candle.timeframe).ok_or_else(|| {
            PerfDbError::InvalidArgument(format!("invalid timeframe: {}", candle.timeframe))
        })?;
        let file = self.ensure_file(candle.market_id, tf)?;
        file.append_record(candle)?;
        Ok(())
    }

    /// Upsert: if the last stored candle has the same `timestamp_ns`, overwrite
    /// it in place. Otherwise append a new record.
    ///
    /// Used for live candle updates where the current bucket's OHLCV evolves
    /// with each tick.
    pub fn upsert(&mut self, candle: &CandleRecord) -> Result<(), PerfDbError> {
        let tf = Timeframe::from_u8(candle.timeframe).ok_or_else(|| {
            PerfDbError::InvalidArgument(format!("invalid timeframe: {}", candle.timeframe))
        })?;
        let file = self.ensure_file(candle.market_id, tf)?;

        let count = file.record_count::<CandleRecord>();
        if count > 0 {
            let last_offset = (count - 1) * size_of::<CandleRecord>() as u64;
            if let Some(last) = file.read_record::<CandleRecord>(last_offset) {
                if last.timestamp_ns == candle.timestamp_ns {
                    file.write_at(last_offset, bytemuck::bytes_of(candle))?;
                    return Ok(());
                }
            }
        }

        file.append_record(candle)?;
        Ok(())
    }

    /// Zero-copy slice of all candles for a (market, timeframe) pair.
    pub fn candles(&self, market_id: MarketId, tf: Timeframe) -> &[CandleRecord] {
        let idx = flat_index(market_id, tf);
        match self.files.get(idx).and_then(|f| f.as_ref()) {
            Some(file) => file.as_slice::<CandleRecord>(),
            None => &[],
        }
    }

    /// Number of stored candles for a (market, timeframe) pair.
    pub fn candle_count(&self, market_id: MarketId, tf: Timeframe) -> u64 {
        let idx = flat_index(market_id, tf);
        match self.files.get(idx).and_then(|f| f.as_ref()) {
            Some(file) => file.record_count::<CandleRecord>(),
            None => 0,
        }
    }

    /// Latest candle for a (market, timeframe) pair.
    pub fn latest(&self, market_id: MarketId, tf: Timeframe) -> Option<&CandleRecord> {
        self.candles(market_id, tf).last()
    }

    /// Flush all open files to disk.
    pub fn flush(&self) -> Result<(), PerfDbError> {
        for file in self.files.iter().flatten() {
            file.flush()?;
        }
        Ok(())
    }

    fn ensure_file(&mut self, market_id: MarketId, tf: Timeframe) -> Result<&mut MmapFile, PerfDbError> {
        let idx = flat_index(market_id, tf);
        if idx >= self.files.len() {
            return Err(PerfDbError::InvalidArgument(format!(
                "market_id {} out of range",
                market_id
            )));
        }
        if self.files[idx].is_none() {
            let path = file_path(&self.base_dir, market_id, tf);
            self.files[idx] = Some(MmapFile::open(&path, true)?);
        }
        Ok(self.files[idx].as_mut().unwrap())
    }
}

fn flat_index(market_id: MarketId, tf: Timeframe) -> usize {
    market_id as usize * TF_COUNT + tf as usize
}

fn file_path(base_dir: &Path, market_id: MarketId, tf: Timeframe) -> PathBuf {
    base_dir.join(format!("m{:04}_tf{:02}.candle", market_id, tf as u8))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_candle(ts_ns: u64, market_id: u16, tf: Timeframe, close: f64) -> CandleRecord {
        CandleRecord {
            timestamp_ns: ts_ns,
            open: close - 10.0,
            high: close + 5.0,
            low: close - 15.0,
            close,
            volume: 100.0,
            market_id,
            timeframe: tf as u8,
            _pad: 0,
        }
    }

    #[test]
    fn create_and_append() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = CandleStore::open(dir.path().join("candles")).unwrap();

        let c = make_candle(60_000_000_000, 0, Timeframe::Min1, 67_000.0);
        store.append(&c).unwrap();

        assert_eq!(store.candle_count(0, Timeframe::Min1), 1);
        let candles = store.candles(0, Timeframe::Min1);
        assert_eq!(candles.len(), 1);
        assert_eq!(candles[0].close, 67_000.0);
    }

    #[test]
    fn upsert_overwrites_same_timestamp() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = CandleStore::open(dir.path().join("candles")).unwrap();

        let c1 = make_candle(60_000_000_000, 0, Timeframe::Min1, 67_000.0);
        store.upsert(&c1).unwrap();
        assert_eq!(store.candle_count(0, Timeframe::Min1), 1);

        let c2 = make_candle(60_000_000_000, 0, Timeframe::Min1, 67_500.0);
        store.upsert(&c2).unwrap();
        assert_eq!(store.candle_count(0, Timeframe::Min1), 1);

        let candles = store.candles(0, Timeframe::Min1);
        assert_eq!(candles[0].close, 67_500.0);
    }

    #[test]
    fn upsert_appends_different_timestamp() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = CandleStore::open(dir.path().join("candles")).unwrap();

        store.upsert(&make_candle(60_000_000_000, 0, Timeframe::Min1, 67_000.0)).unwrap();
        store.upsert(&make_candle(120_000_000_000, 0, Timeframe::Min1, 67_100.0)).unwrap();

        assert_eq!(store.candle_count(0, Timeframe::Min1), 2);
    }

    #[test]
    fn multi_market_multi_timeframe() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = CandleStore::open(dir.path().join("candles")).unwrap();

        store.append(&make_candle(60_000_000_000, 0, Timeframe::Min1, 67_000.0)).unwrap();
        store.append(&make_candle(3_600_000_000_000, 0, Timeframe::Hour1, 67_500.0)).unwrap();
        store.append(&make_candle(60_000_000_000, 1, Timeframe::Min1, 3_500.0)).unwrap();

        assert_eq!(store.candle_count(0, Timeframe::Min1), 1);
        assert_eq!(store.candle_count(0, Timeframe::Hour1), 1);
        assert_eq!(store.candle_count(1, Timeframe::Min1), 1);
        assert_eq!(store.candle_count(1, Timeframe::Hour1), 0);
    }

    #[test]
    fn latest() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = CandleStore::open(dir.path().join("candles")).unwrap();

        assert!(store.latest(0, Timeframe::Min1).is_none());

        store.append(&make_candle(60_000_000_000, 0, Timeframe::Min1, 67_000.0)).unwrap();
        store.append(&make_candle(120_000_000_000, 0, Timeframe::Min1, 67_100.0)).unwrap();

        let latest = store.latest(0, Timeframe::Min1).unwrap();
        assert_eq!(latest.timestamp_ns, 120_000_000_000);
        assert_eq!(latest.close, 67_100.0);
    }

    #[test]
    fn reopen_preserves_data() {
        let dir = tempfile::tempdir().unwrap();
        let store_dir = dir.path().join("candles");

        {
            let mut store = CandleStore::open(&store_dir).unwrap();
            for i in 0..50u64 {
                let ts = i * 60_000_000_000;
                store.append(&make_candle(ts, 0, Timeframe::Min1, 67_000.0 + i as f64)).unwrap();
            }
            store.flush().unwrap();
        }

        let store = CandleStore::open(&store_dir).unwrap();
        assert_eq!(store.candle_count(0, Timeframe::Min1), 50);
        assert_eq!(store.candles(0, Timeframe::Min1)[49].close, 67_049.0);
    }

    #[test]
    fn empty_returns_empty_slice() {
        let dir = tempfile::tempdir().unwrap();
        let store = CandleStore::open(dir.path().join("candles")).unwrap();

        assert!(store.candles(0, Timeframe::Min1).is_empty());
        assert!(store.candles(17, Timeframe::Week1).is_empty());
    }
}
