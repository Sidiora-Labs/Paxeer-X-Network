use std::path::{Path, PathBuf};

use crate::core::market::{MarketId, MARKET_COUNT};
use crate::core::types::PriceTick;
use crate::error::PerfDbError;
use crate::storage::mmap::MmapFile;

/// Per-market append-only tick storage backed by memory-mapped files.
///
/// Directory layout:
/// ```text
/// {base_dir}/
///   market_0000.tick    (BTC)
///   market_0001.tick    (ETH)
///   ...
/// ```
///
/// Each file contains contiguous `PriceTick` records (24 bytes each),
/// naturally sorted by `timestamp_ns` (ticks arrive in chronological order
/// from the price feed).
///
/// Concurrency: single writer, multiple readers (via mmap zero-copy).
pub struct TickStore {
    base_dir: PathBuf,
    /// One MmapFile per market. `None` = not yet opened (lazy).
    files: Vec<Option<MmapFile>>,
}

impl TickStore {
    /// Open or create the tick store directory.
    ///
    /// Eagerly opens existing market files. New markets are opened lazily
    /// on first append.
    pub fn open(base_dir: impl AsRef<Path>) -> Result<Self, PerfDbError> {
        let base_dir = base_dir.as_ref().to_path_buf();

        std::fs::create_dir_all(&base_dir).map_err(|e| PerfDbError::Io {
            context: format!("create tick_store dir: {}", base_dir.display()),
            source: e,
        })?;

        let mut files: Vec<Option<MmapFile>> = (0..MARKET_COUNT).map(|_| None).collect();

        for mid in 0..MARKET_COUNT as MarketId {
            let path = market_path(&base_dir, mid);
            if path.exists() {
                files[mid as usize] = Some(MmapFile::open(&path, false)?);
            }
        }

        Ok(Self { base_dir, files })
    }

    /// Append a tick to the appropriate market file.
    pub fn append(&mut self, tick: &PriceTick) -> Result<(), PerfDbError> {
        let mid = tick.market_id as usize;
        if mid >= MARKET_COUNT {
            return Err(PerfDbError::InvalidArgument(format!(
                "market_id {} out of range (max {})",
                tick.market_id,
                MARKET_COUNT - 1
            )));
        }

        let file = self.ensure_file(tick.market_id)?;
        file.append_record(tick)?;
        Ok(())
    }

    /// Zero-copy slice of all ticks for a market, sorted by `timestamp_ns`.
    pub fn ticks(&self, market_id: MarketId) -> &[PriceTick] {
        match self.files.get(market_id as usize).and_then(|f| f.as_ref()) {
            Some(file) => file.as_slice::<PriceTick>(),
            None => &[],
        }
    }

    /// Number of stored ticks for a market.
    pub fn tick_count(&self, market_id: MarketId) -> u64 {
        match self.files.get(market_id as usize).and_then(|f| f.as_ref()) {
            Some(file) => file.record_count::<PriceTick>(),
            None => 0,
        }
    }

    /// Latest tick for a market, or `None` if no ticks exist.
    pub fn latest(&self, market_id: MarketId) -> Option<&PriceTick> {
        self.ticks(market_id).last()
    }

    /// Flush all open market files to disk.
    pub fn flush(&self) -> Result<(), PerfDbError> {
        for file in self.files.iter().flatten() {
            file.flush()?;
        }
        Ok(())
    }

    /// Flush asynchronously (OS may defer).
    pub fn flush_async(&self) -> Result<(), PerfDbError> {
        for file in self.files.iter().flatten() {
            file.flush_async()?;
        }
        Ok(())
    }

    fn ensure_file(&mut self, market_id: MarketId) -> Result<&mut MmapFile, PerfDbError> {
        let mid = market_id as usize;
        if self.files[mid].is_none() {
            let path = market_path(&self.base_dir, market_id);
            self.files[mid] = Some(MmapFile::open(&path, true)?);
        }
        Ok(self.files[mid].as_mut().unwrap())
    }
}

fn market_path(base_dir: &Path, market_id: MarketId) -> PathBuf {
    base_dir.join(format!("market_{:04}.tick", market_id))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_tick(ts_ns: u64, market_id: u16, price: f64) -> PriceTick {
        PriceTick {
            timestamp_ns: ts_ns,
            market_id,
            _pad: [0; 6],
            price,
        }
    }

    #[test]
    fn create_and_append() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = TickStore::open(dir.path().join("ticks")).unwrap();

        store.append(&make_tick(1_000, 0, 67_000.0)).unwrap();
        store.append(&make_tick(2_000, 0, 67_001.0)).unwrap();

        assert_eq!(store.tick_count(0), 2);
        assert_eq!(store.ticks(0).len(), 2);
        assert_eq!(store.ticks(0)[0].price, 67_000.0);
        assert_eq!(store.ticks(0)[1].price, 67_001.0);
    }

    #[test]
    fn multi_market() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = TickStore::open(dir.path().join("ticks")).unwrap();

        store.append(&make_tick(1_000, 0, 67_000.0)).unwrap();
        store.append(&make_tick(1_000, 1, 3_500.0)).unwrap();
        store.append(&make_tick(2_000, 0, 67_100.0)).unwrap();

        assert_eq!(store.tick_count(0), 2);
        assert_eq!(store.tick_count(1), 1);
        assert_eq!(store.tick_count(2), 0);

        assert_eq!(store.ticks(1)[0].price, 3_500.0);
    }

    #[test]
    fn latest() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = TickStore::open(dir.path().join("ticks")).unwrap();

        assert!(store.latest(0).is_none());

        store.append(&make_tick(1_000, 0, 67_000.0)).unwrap();
        store.append(&make_tick(2_000, 0, 67_100.0)).unwrap();

        let latest = store.latest(0).unwrap();
        assert_eq!(latest.timestamp_ns, 2_000);
        assert_eq!(latest.price, 67_100.0);
    }

    #[test]
    fn reopen_preserves_data() {
        let dir = tempfile::tempdir().unwrap();
        let store_dir = dir.path().join("ticks");

        {
            let mut store = TickStore::open(&store_dir).unwrap();
            for i in 0..100u64 {
                store.append(&make_tick(i * 1_000, 0, 60_000.0 + i as f64)).unwrap();
            }
            store.flush().unwrap();
        }

        let store = TickStore::open(&store_dir).unwrap();
        assert_eq!(store.tick_count(0), 100);
        assert_eq!(store.ticks(0)[0].price, 60_000.0);
        assert_eq!(store.ticks(0)[99].price, 60_099.0);
    }

    #[test]
    fn invalid_market_id() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = TickStore::open(dir.path().join("ticks")).unwrap();

        let result = store.append(&make_tick(1_000, 99, 1.0));
        assert!(result.is_err());
    }

    #[test]
    fn empty_market_returns_empty_slice() {
        let dir = tempfile::tempdir().unwrap();
        let store = TickStore::open(dir.path().join("ticks")).unwrap();

        assert!(store.ticks(0).is_empty());
        assert!(store.ticks(17).is_empty());
        assert!(store.ticks(99).is_empty());
    }

    #[test]
    fn bulk_append_performance() {
        let dir = tempfile::tempdir().unwrap();
        let mut store = TickStore::open(dir.path().join("ticks")).unwrap();

        for i in 0..10_000u64 {
            let mid = (i % 3) as u16;
            store.append(&make_tick(i * 1_000, mid, 50_000.0 + i as f64)).unwrap();
        }

        let total: u64 = (0..3).map(|mid| store.tick_count(mid)).sum();
        assert_eq!(total, 10_000);

        for mid in 0..3u16 {
            let ticks = store.ticks(mid);
            for w in ticks.windows(2) {
                assert!(w[0].timestamp_ns < w[1].timestamp_ns);
            }
        }
    }
}
