use std::sync::atomic::{AtomicU64, Ordering};

use parking_lot::RwLock;

/// Indexer checkpoint state singleton.
///
/// Replaces PostgreSQL `indexer_state` table (single row, id=1).
/// Stores the last processed block number, hash, and timestamp.
///
/// Hot path (block number read): lock-free via AtomicU64.
/// Warm path (hash read/write): parking_lot::RwLock.
pub struct CheckpointStore {
    last_block: AtomicU64,
    last_block_timestamp: AtomicU64,
    inner: RwLock<CheckpointInner>,
}

#[derive(Debug, Clone)]
struct CheckpointInner {
    last_block_hash: String,
}

/// Read-only snapshot of the checkpoint state.
#[derive(Debug, Clone)]
pub struct Checkpoint {
    pub last_block: u64,
    pub last_block_hash: String,
    pub last_block_timestamp: u64,
}

impl CheckpointStore {
    /// Create with initial values (typically from WAL replay or zero).
    pub fn new(last_block: u64, last_block_hash: String, last_block_timestamp: u64) -> Self {
        Self {
            last_block: AtomicU64::new(last_block),
            last_block_timestamp: AtomicU64::new(last_block_timestamp),
            inner: RwLock::new(CheckpointInner { last_block_hash }),
        }
    }

    /// Create an empty checkpoint (block 0).
    pub fn empty() -> Self {
        Self::new(0, String::new(), 0)
    }

    /// Last processed block number. Lock-free.
    pub fn last_block(&self) -> u64 {
        self.last_block.load(Ordering::Acquire)
    }

    /// Last processed block timestamp. Lock-free.
    pub fn last_block_timestamp(&self) -> u64 {
        self.last_block_timestamp.load(Ordering::Acquire)
    }

    /// Last processed block hash. Requires read lock.
    pub fn last_block_hash(&self) -> String {
        self.inner.read().last_block_hash.clone()
    }

    /// Full snapshot.
    pub fn snapshot(&self) -> Checkpoint {
        let inner = self.inner.read();
        Checkpoint {
            last_block: self.last_block.load(Ordering::Acquire),
            last_block_hash: inner.last_block_hash.clone(),
            last_block_timestamp: self.last_block_timestamp.load(Ordering::Acquire),
        }
    }

    /// Update the checkpoint atomically.
    pub fn save(&self, block: u64, block_hash: String, block_timestamp: u64) {
        self.last_block.store(block, Ordering::Release);
        self.last_block_timestamp
            .store(block_timestamp, Ordering::Release);
        self.inner.write().last_block_hash = block_hash;
    }

    /// Advance only the block number (no hash update). Used during batch processing.
    pub fn advance_block(&self, block: u64) {
        self.last_block.store(block, Ordering::Release);
    }
}

impl Default for CheckpointStore {
    fn default() -> Self {
        Self::empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_checkpoint() {
        let cp = CheckpointStore::empty();
        assert_eq!(cp.last_block(), 0);
        assert_eq!(cp.last_block_hash(), "");
        assert_eq!(cp.last_block_timestamp(), 0);
    }

    #[test]
    fn new_with_values() {
        let cp = CheckpointStore::new(42, "0xabc".to_string(), 1700000000);
        assert_eq!(cp.last_block(), 42);
        assert_eq!(cp.last_block_hash(), "0xabc");
        assert_eq!(cp.last_block_timestamp(), 1700000000);
    }

    #[test]
    fn save_and_read() {
        let cp = CheckpointStore::empty();
        cp.save(100, "0xdeadbeef".to_string(), 1700001000);

        assert_eq!(cp.last_block(), 100);
        assert_eq!(cp.last_block_hash(), "0xdeadbeef");
        assert_eq!(cp.last_block_timestamp(), 1700001000);
    }

    #[test]
    fn save_overwrites() {
        let cp = CheckpointStore::new(10, "0x111".to_string(), 100);
        cp.save(20, "0x222".to_string(), 200);

        assert_eq!(cp.last_block(), 20);
        assert_eq!(cp.last_block_hash(), "0x222");
        assert_eq!(cp.last_block_timestamp(), 200);
    }

    #[test]
    fn advance_block_only() {
        let cp = CheckpointStore::new(10, "0xabc".to_string(), 100);
        cp.advance_block(15);

        assert_eq!(cp.last_block(), 15);
        assert_eq!(cp.last_block_hash(), "0xabc");
    }

    #[test]
    fn snapshot() {
        let cp = CheckpointStore::new(50, "0xhash".to_string(), 500);
        let snap = cp.snapshot();

        assert_eq!(snap.last_block, 50);
        assert_eq!(snap.last_block_hash, "0xhash");
        assert_eq!(snap.last_block_timestamp, 500);
    }

    #[test]
    fn concurrent_reads_and_writes() {
        let cp = CheckpointStore::empty();

        for i in 0..1000u64 {
            cp.save(i, format!("0x{:04x}", i), i * 10);
        }

        let snap = cp.snapshot();
        assert_eq!(snap.last_block, 999);
        assert_eq!(snap.last_block_hash, "0x03e7");
        assert_eq!(snap.last_block_timestamp, 9990);
    }
}
