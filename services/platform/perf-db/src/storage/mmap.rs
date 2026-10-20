use memmap2::{MmapMut, MmapOptions};
use std::fs::{File, OpenOptions};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use crate::error::PerfDbError;

/// Initial file allocation when creating a new file.
const INITIAL_FILE_SIZE: u64 = 4 * 1024 * 1024; // 4 MB

/// Memory-mapped file abstraction.
///
/// Supports:
/// - Atomic appends via an internal length counter
/// - Zero-copy reads via `read_at`
/// - Double-on-resize growth strategy (amortized O(1) append)
///
/// Concurrency model:
/// - Single writer (caller must enforce)
/// - Multiple concurrent readers (zero-copy from mmap)
///
/// The file has a simple layout:
/// ```text
/// [header: 8 bytes — u64 LE logical length]
/// [data: variable length payload]
/// ```
pub struct MmapFile {
    path: PathBuf,
    file: File,
    mmap: MmapMut,
    /// Logical data length (excludes header). Atomic for lock-free reads.
    logical_len: AtomicU64,
    /// Allocated file capacity (excludes header).
    capacity: u64,
}

const HEADER_SIZE: u64 = 8;

impl MmapFile {
    /// Open or create a memory-mapped file at `path`.
    ///
    /// If the file exists, the logical length is read from the header.
    /// If `create` is true and the file doesn't exist, a new file is created.
    pub fn open(path: impl AsRef<Path>, create: bool) -> Result<Self, PerfDbError> {
        let path = path.as_ref().to_path_buf();

        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(create)
            .open(&path)
            .map_err(|e| PerfDbError::Io {
                context: format!("open mmap file: {}", path.display()),
                source: e,
            })?;

        let file_len = file.metadata().map_err(|e| PerfDbError::Io {
            context: "file metadata".into(),
            source: e,
        })?.len();

        if file_len == 0 {
            file.set_len(HEADER_SIZE + INITIAL_FILE_SIZE)
                .map_err(|e| PerfDbError::Io {
                    context: "initial file allocation".into(),
                    source: e,
                })?;
        }

        let total_len = file.metadata().map_err(|e| PerfDbError::Io {
            context: "file metadata after resize".into(),
            source: e,
        })?.len();

        let mmap = unsafe {
            MmapOptions::new()
                .len(total_len as usize)
                .map_mut(&file)
                .map_err(|e| PerfDbError::Io {
                    context: "mmap".into(),
                    source: e,
                })?
        };

        let logical_len = if file_len == 0 {
            0u64
        } else {
            let header_bytes: [u8; 8] = mmap[..8].try_into().unwrap();
            u64::from_le_bytes(header_bytes)
        };

        let capacity = total_len - HEADER_SIZE;

        Ok(Self {
            path,
            file,
            mmap,
            logical_len: AtomicU64::new(logical_len),
            capacity,
        })
    }

    /// Current logical data length in bytes (excludes header).
    pub fn len(&self) -> u64 {
        self.logical_len.load(Ordering::Acquire)
    }

    /// Whether the file contains no data.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Append raw bytes. Grows the file if necessary (double-on-resize).
    ///
    /// **Single writer only.** Caller must ensure exclusive write access.
    pub fn append(&mut self, data: &[u8]) -> Result<u64, PerfDbError> {
        let data_len = data.len() as u64;
        if data_len == 0 {
            return Ok(self.logical_len.load(Ordering::Acquire));
        }

        let current_len = self.logical_len.load(Ordering::Acquire);
        let needed = current_len + data_len;

        if needed > self.capacity {
            self.grow(needed)?;
        }

        let offset = (HEADER_SIZE + current_len) as usize;
        self.mmap[offset..offset + data.len()].copy_from_slice(data);

        let new_len = current_len + data_len;
        self.mmap[..8].copy_from_slice(&new_len.to_le_bytes());
        self.logical_len.store(new_len, Ordering::Release);

        Ok(new_len)
    }

    /// Append a typed `repr(C)` record. Returns the offset where it was written.
    pub fn append_record<T: bytemuck::Pod>(&mut self, record: &T) -> Result<u64, PerfDbError> {
        let offset = self.logical_len.load(Ordering::Acquire);
        let bytes = bytemuck::bytes_of(record);
        self.append(bytes)?;
        Ok(offset)
    }

    /// Zero-copy read at a given offset within the logical data region.
    ///
    /// Returns `None` if the range is out of bounds.
    pub fn read_at(&self, offset: u64, len: u64) -> Option<&[u8]> {
        let logical_len = self.logical_len.load(Ordering::Acquire);
        if offset + len > logical_len {
            return None;
        }
        let start = (HEADER_SIZE + offset) as usize;
        let end = start + len as usize;
        Some(&self.mmap[start..end])
    }

    /// Zero-copy read of a typed record at a given offset.
    pub fn read_record<T: bytemuck::Pod>(&self, offset: u64) -> Option<&T> {
        let size = size_of::<T>() as u64;
        let bytes = self.read_at(offset, size)?;
        Some(bytemuck::from_bytes(bytes))
    }

    /// Number of fixed-size records that fit in the current data.
    pub fn record_count<T: bytemuck::Pod>(&self) -> u64 {
        self.len() / size_of::<T>() as u64
    }

    /// Iterate all records of a fixed-size type. Zero-copy.
    pub fn iter_records<T: bytemuck::Pod>(&self) -> RecordIter<'_, T> {
        RecordIter {
            mmap: self,
            offset: 0,
            _marker: std::marker::PhantomData,
        }
    }

    /// Zero-copy typed slice of all records in the file.
    ///
    /// Returns `&[T]` backed directly by the mmap. Records must be stored
    /// contiguously starting from offset 0. Trailing bytes that don't fill
    /// a complete record are excluded.
    pub fn as_slice<T: bytemuck::Pod>(&self) -> &[T] {
        let logical_len = self.logical_len.load(Ordering::Acquire) as usize;
        let record_size = size_of::<T>();
        if record_size == 0 || logical_len == 0 {
            return &[];
        }
        let count = logical_len / record_size;
        let usable = count * record_size;
        let start = HEADER_SIZE as usize;
        bytemuck::cast_slice(&self.mmap[start..start + usable])
    }

    /// Overwrite data at a given offset within the logical data region.
    ///
    /// **Single writer only.** Does NOT extend the file — offset + data.len()
    /// must be within the current logical length.
    pub fn write_at(&mut self, offset: u64, data: &[u8]) -> Result<(), PerfDbError> {
        let data_len = data.len() as u64;
        let logical_len = self.logical_len.load(Ordering::Acquire);
        if offset + data_len > logical_len {
            return Err(PerfDbError::InvalidArgument(format!(
                "write_at: offset {} + len {} exceeds logical len {}",
                offset, data_len, logical_len
            )));
        }
        let start = (HEADER_SIZE + offset) as usize;
        self.mmap[start..start + data.len()].copy_from_slice(data);
        Ok(())
    }

    /// Flush the mmap to disk.
    pub fn flush(&self) -> Result<(), PerfDbError> {
        self.mmap.flush().map_err(|e| PerfDbError::Io {
            context: "mmap flush".into(),
            source: e,
        })
    }

    /// Flush asynchronously (OS may defer actual I/O).
    pub fn flush_async(&self) -> Result<(), PerfDbError> {
        self.mmap.flush_async().map_err(|e| PerfDbError::Io {
            context: "mmap flush_async".into(),
            source: e,
        })
    }

    /// Path to the underlying file.
    pub fn path(&self) -> &Path {
        &self.path
    }

    /// Grow the file using double-on-resize strategy.
    fn grow(&mut self, needed: u64) -> Result<(), PerfDbError> {
        let mut new_capacity = self.capacity;
        while new_capacity < needed {
            new_capacity = new_capacity.saturating_mul(2).max(INITIAL_FILE_SIZE);
        }

        let new_file_size = HEADER_SIZE + new_capacity;

        self.mmap.flush().map_err(|e| PerfDbError::Io {
            context: "flush before grow".into(),
            source: e,
        })?;

        self.file.set_len(new_file_size).map_err(|e| PerfDbError::Io {
            context: format!("grow file to {new_file_size} bytes"),
            source: e,
        })?;

        self.mmap = unsafe {
            MmapOptions::new()
                .len(new_file_size as usize)
                .map_mut(&self.file)
                .map_err(|e| PerfDbError::Io {
                    context: "remap after grow".into(),
                    source: e,
                })?
        };

        self.capacity = new_capacity;

        tracing::debug!(
            path = %self.path.display(),
            new_capacity_mb = new_capacity / (1024 * 1024),
            "mmap file grown"
        );

        Ok(())
    }
}

/// Zero-copy iterator over fixed-size records in an `MmapFile`.
pub struct RecordIter<'a, T: bytemuck::Pod> {
    mmap: &'a MmapFile,
    offset: u64,
    _marker: std::marker::PhantomData<T>,
}

impl<'a, T: bytemuck::Pod> Iterator for RecordIter<'a, T> {
    type Item = &'a T;

    fn next(&mut self) -> Option<Self::Item> {
        let record = self.mmap.read_record::<T>(self.offset)?;
        self.offset += size_of::<T>() as u64;
        Some(record)
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let remaining = self.mmap.len().saturating_sub(self.offset) / size_of::<T>() as u64;
        (remaining as usize, Some(remaining as usize))
    }
}

impl<T: bytemuck::Pod> ExactSizeIterator for RecordIter<'_, T> {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::types::PriceTick;

    #[test]
    fn create_and_append() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("test.dat");

        let mut f = MmapFile::open(&path, true).unwrap();
        assert_eq!(f.len(), 0);
        assert!(f.is_empty());

        f.append(b"hello").unwrap();
        assert_eq!(f.len(), 5);

        let data = f.read_at(0, 5).unwrap();
        assert_eq!(data, b"hello");

        assert!(f.read_at(0, 6).is_none());
    }

    #[test]
    fn reopen_preserves_data() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("test.dat");

        {
            let mut f = MmapFile::open(&path, true).unwrap();
            f.append(b"persist").unwrap();
            f.flush().unwrap();
        }

        let f = MmapFile::open(&path, false).unwrap();
        assert_eq!(f.len(), 7);
        assert_eq!(f.read_at(0, 7).unwrap(), b"persist");
    }

    #[test]
    fn growth_doubles() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("grow.dat");

        let mut f = MmapFile::open(&path, true).unwrap();
        let initial_cap = f.capacity;

        let big = vec![0xABu8; (initial_cap + 1) as usize];
        f.append(&big).unwrap();

        assert!(f.capacity >= initial_cap * 2);
        assert_eq!(f.len(), big.len() as u64);
    }

    #[test]
    fn typed_records() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("ticks.dat");

        let mut f = MmapFile::open(&path, true).unwrap();

        let tick = PriceTick {
            timestamp_ns: 1_000_000_000,
            market_id: 0,
            _pad: [0; 6],
            price: 67_000.50,
        };

        let offset = f.append_record(&tick).unwrap();
        assert_eq!(offset, 0);

        let read_back: &PriceTick = f.read_record(0).unwrap();
        assert_eq!(read_back.timestamp_ns, tick.timestamp_ns);
        assert_eq!(read_back.market_id, tick.market_id);
        assert_eq!(read_back.price, tick.price);

        assert_eq!(f.record_count::<PriceTick>(), 1);
    }

    #[test]
    fn as_slice_and_write_at() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("slice.dat");
        let mut f = MmapFile::open(&path, true).unwrap();

        for i in 0..10u64 {
            let tick = PriceTick {
                timestamp_ns: i * 1_000_000,
                market_id: 0,
                _pad: [0; 6],
                price: 60_000.0 + i as f64,
            };
            f.append_record(&tick).unwrap();
        }

        let slice: &[PriceTick] = f.as_slice();
        assert_eq!(slice.len(), 10);
        assert_eq!(slice[0].price, 60_000.0);
        assert_eq!(slice[9].price, 60_009.0);

        let mut updated = slice[5];
        updated.price = 99_999.0;
        f.write_at(5 * 24, bytemuck::bytes_of(&updated)).unwrap();

        let slice: &[PriceTick] = f.as_slice();
        assert_eq!(slice[5].price, 99_999.0);

        assert!(f.write_at(f.len(), &[0]).is_err());
    }

    #[test]
    fn iter_records() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("iter.dat");
        let mut f = MmapFile::open(&path, true).unwrap();

        for i in 0..100u64 {
            let tick = PriceTick {
                timestamp_ns: i * 1_000_000,
                market_id: (i % 18) as u16,
                _pad: [0; 6],
                price: 60_000.0 + i as f64,
            };
            f.append_record(&tick).unwrap();
        }

        assert_eq!(f.record_count::<PriceTick>(), 100);

        let all: Vec<&PriceTick> = f.iter_records().collect();
        assert_eq!(all.len(), 100);
        assert_eq!(all[0].timestamp_ns, 0);
        assert_eq!(all[99].timestamp_ns, 99 * 1_000_000);
    }
}
