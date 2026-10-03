//! Persistent program storage whose address space is structurally scoped to
//! either one program/principal pair or one program-shared plane. Guest-facing
//! APIs never accept an arbitrary namespace, so neither adjacent programs nor
//! adjacent principals can be reached by choosing a key.

use core::fmt::{self, Display};
use std::cell::RefCell;
use std::collections::{BTreeMap, BTreeSet};

mod namespace;
#[path = "scan.rs"]
mod ordered_scan;
pub mod reclaim;

pub use namespace::StorageNamespace;
pub(crate) use ordered_scan::scan_cells;
pub use ordered_scan::{ScanEntry, ScanLimits, StorageScan, MAX_STORAGE_SCAN_CURSOR_BYTES};
pub use reclaim::NamespaceDrop;

/// Maximum key length admitted by the version-one storage ABI.
pub const MAX_STORAGE_KEY_BYTES: usize = 256;
/// Maximum value length admitted by the version-one storage ABI.
pub const MAX_STORAGE_VALUE_BYTES: usize = 1_048_576;

/// Stable identifier of a deployed program.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct ProgramId([u8; 32]);

impl ProgramId {
    /// Constructs a nonzero program identifier.
    ///
    /// # Errors
    ///
    /// Refuses the all-zero identifier reserved for absence.
    pub fn new(bytes: [u8; 32]) -> Result<Self, StorageError> {
        if bytes == [0; 32] {
            return Err(StorageError::InvalidProgram);
        }
        Ok(Self(bytes))
    }

    /// Returns the canonical identifier bytes.
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}

/// Stable identifier of the principal whose authority invoked a program.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct PrincipalId([u8; 32]);

impl PrincipalId {
    /// Constructs a nonzero principal identifier.
    ///
    /// # Errors
    ///
    /// Refuses the all-zero identifier reserved for absence.
    pub fn new(bytes: [u8; 32]) -> Result<Self, StorageError> {
        if bytes == [0; 32] {
            return Err(StorageError::InvalidPrincipal);
        }
        Ok(Self(bytes))
    }

    /// Returns the canonical identifier bytes.
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}

#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub(crate) struct StorageAddress {
    namespace: StorageNamespace,
    key: Vec<u8>,
}

/// Typed namespaced-storage refusal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StorageError {
    InvalidProgram,
    InvalidPrincipal,
    EmptyKey,
    KeyTooLarge,
    ValueTooLarge,
    PrefixTooLarge,
    InvalidScanCursor,
    InvalidScanLimits,
    ScanCeilingExceeded,
    FrozenNamespace,
    SizeOverflow,
}

impl Display for StorageError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidProgram => formatter.write_str("program identifier is reserved"),
            Self::InvalidPrincipal => formatter.write_str("principal identifier is reserved"),
            Self::EmptyKey => formatter.write_str("storage key is empty"),
            Self::KeyTooLarge => formatter.write_str("storage key exceeds the ABI bound"),
            Self::ValueTooLarge => formatter.write_str("storage value exceeds the ABI bound"),
            Self::PrefixTooLarge => {
                formatter.write_str("storage scan prefix exceeds the ABI bound")
            }
            Self::InvalidScanCursor => {
                formatter.write_str("storage scan cursor is invalid or belongs to another scan")
            }
            Self::InvalidScanLimits => formatter.write_str("storage scan limits are invalid"),
            Self::ScanCeilingExceeded => formatter
                .write_str("storage scan entry exceeds the declared complete page byte ceiling"),
            Self::FrozenNamespace => formatter.write_str("storage namespace is frozen"),
            Self::SizeOverflow => formatter.write_str("storage accounting overflowed"),
        }
    }
}

impl std::error::Error for StorageError {}

/// Durable storage shared by program executions. Every map key includes a
/// closed namespace value carrying its owning program and declared scope.
#[derive(Clone, Debug, Default)]
pub struct Storage {
    cells: BTreeMap<StorageAddress, Vec<u8>>,
    frozen_namespaces: BTreeSet<StorageNamespace>,
    accessed_namespaces: RefCell<BTreeSet<StorageNamespace>>,
}

impl PartialEq for Storage {
    fn eq(&self, other: &Self) -> bool {
        self.cells == other.cells && self.frozen_namespaces == other.frozen_namespaces
    }
}

impl Eq for Storage {}

type ProtocolEntries = Vec<(Vec<u8>, Vec<u8>)>;

fn namespace_cells(
    cells: &BTreeMap<StorageAddress, Vec<u8>>,
    namespace: StorageNamespace,
) -> impl Iterator<Item = (&StorageAddress, &Vec<u8>)> {
    cells
        .range(StorageAddress { namespace, key: Vec::new() }..)
        .take_while(move |(address, _)| address.namespace == namespace)
}

impl Storage {
    fn commitment_key_len(address: &StorageAddress) -> Option<u64> {
        let mut namespace = [0_u8; 65];
        let namespace_len = address.namespace.write_canonical(&mut namespace);
        u64::try_from(
            2_usize
                .checked_add(namespace_len)?
                .checked_add(address.key.len())?,
        )
        .ok()
    }
    fn commitment_key(address: &StorageAddress) -> Vec<u8> {
        let namespace = address.namespace.canonical_bytes();
        let namespace_length = u16::try_from(namespace.len())
            .unwrap_or_else(|_| unreachable!("closed storage namespace length is bounded"));
        let mut key = Vec::with_capacity(
            2_usize
                .saturating_add(namespace.len())
                .saturating_add(address.key.len()),
        );
        key.extend_from_slice(&namespace_length.to_be_bytes());
        key.extend_from_slice(&namespace);
        key.extend_from_slice(&address.key);
        key
    }
    /// Creates an empty storage plane.
    #[must_use]
    pub const fn new() -> Self {
        Self {
            cells: BTreeMap::new(),
            frozen_namespaces: BTreeSet::new(),
            accessed_namespaces: RefCell::new(BTreeSet::new()),
        }
    }

    #[cfg(feature = "host-ffi")]
    pub(crate) fn enforce_frozen_namespaces(
        &mut self,
        namespaces: impl IntoIterator<Item = StorageNamespace>,
    ) {
        self.frozen_namespaces = namespaces.into_iter().collect();
    }

    fn ensure_accessible(&self, namespace: StorageNamespace) -> Result<(), StorageError> {
        if self.frozen_namespaces.contains(&namespace) {
            Err(StorageError::FrozenNamespace)
        } else {
            self.accessed_namespaces.borrow_mut().insert(namespace);
            Ok(())
        }
    }

    #[cfg(feature = "host-ffi")]
    pub(crate) fn clear_access_log(&self) {
        self.accessed_namespaces.borrow_mut().clear();
    }

    #[cfg(feature = "host-ffi")]
    pub(crate) fn was_accessed(&self, namespace: StorageNamespace) -> bool {
        self.accessed_namespaces.borrow().contains(&namespace)
    }

    pub(crate) fn try_for_each_commitment_entry<E>(&self, mut visit: impl FnMut(Vec<u8>, &[u8]) -> Result<(), E>) -> Result<(), E> {
        for (address, value) in &self.cells {
            visit(Self::commitment_key(address), value)?;
        }
        Ok(())
    }

    pub(crate) fn for_each_commitment_entry(&self, mut visit: impl FnMut(Vec<u8>, &[u8])) {
        for (address, value) in &self.cells {
            visit(Self::commitment_key(address), value);
        }
    }

    pub(crate) fn try_for_each_commitment_delta<E>(
        &self,
        baseline: &Self,
        mut visit: impl FnMut(Vec<u8>, Option<&[u8]>) -> Result<(), E>,
    ) -> Result<(), E> {
        let mut current = self.cells.iter().peekable();
        let mut baseline = baseline.cells.iter().peekable();
        loop {
            match (current.peek(), baseline.peek()) {
                (Some((address, value)), Some((baseline_address, baseline_value))) => {
                    match address.cmp(baseline_address) {
                        core::cmp::Ordering::Less => {
                            visit(Self::commitment_key(address), Some(value))?;
                            current.next();
                        }
                        core::cmp::Ordering::Greater => {
                            visit(Self::commitment_key(baseline_address), None)?;
                            baseline.next();
                        }
                        core::cmp::Ordering::Equal => {
                            if value.as_slice() != baseline_value.as_slice() {
                                visit(Self::commitment_key(address), Some(value))?;
                            }
                            current.next();
                            baseline.next();
                        }
                    }
                }
                (Some((address, value)), None) => {
                    visit(Self::commitment_key(address), Some(value))?;
                    current.next();
                }
                (None, Some((address, _))) => {
                    visit(Self::commitment_key(address), None)?;
                    baseline.next();
                }
                (None, None) => break,
            }
        }
        Ok(())
    }

    pub(crate) fn for_each_commitment_delta(
        &self,
        baseline: &Self,
        mut visit: impl FnMut(Vec<u8>, Option<&[u8]>),
    ) {
        let mut current = self.cells.iter().peekable();
        let mut baseline = baseline.cells.iter().peekable();
        loop {
            match (current.peek(), baseline.peek()) {
                (Some((address, value)), Some((baseline_address, baseline_value))) => {
                    match address.cmp(baseline_address) {
                        core::cmp::Ordering::Less => {
                            visit(Self::commitment_key(address), Some(value));
                            current.next();
                        }
                        core::cmp::Ordering::Greater => {
                            visit(Self::commitment_key(baseline_address), None);
                            baseline.next();
                        }
                        core::cmp::Ordering::Equal => {
                            if value.as_slice() != baseline_value.as_slice() {
                                visit(Self::commitment_key(address), Some(value));
                            }
                            current.next();
                            baseline.next();
                        }
                    }
                }
                (Some((address, value)), None) => {
                    visit(Self::commitment_key(address), Some(value));
                    current.next();
                }
                (None, Some((address, _))) => {
                    visit(Self::commitment_key(address), None);
                    baseline.next();
                }
                (None, None) => break,
            }
        }
    }

    pub(crate) fn commitment_delta_metrics(&self, baseline: &Self) -> Option<(usize, u64)> {
        let mut current = self.cells.iter().peekable();
        let mut baseline = baseline.cells.iter().peekable();
        let mut entries = 0_usize;
        let mut bytes = 4_u64;
        loop {
            let (key_bytes, value_bytes, advance_current, advance_baseline) =
                match (current.peek(), baseline.peek()) {
                    (Some((address, value)), Some((baseline_address, baseline_value))) => {
                        match address.cmp(baseline_address) {
                            core::cmp::Ordering::Less => (
                                Self::commitment_key_len(address)?,
                                Some(u64::try_from(value.len()).ok()?),
                                true,
                                false,
                            ),
                            core::cmp::Ordering::Greater => (
                                Self::commitment_key_len(baseline_address)?,
                                None,
                                false,
                                true,
                            ),
                            core::cmp::Ordering::Equal
                                if value.as_slice() != baseline_value.as_slice() =>
                            {
                                (
                                    Self::commitment_key_len(address)?,
                                    Some(u64::try_from(value.len()).ok()?),
                                    true,
                                    true,
                                )
                            }
                            core::cmp::Ordering::Equal => {
                                current.next();
                                baseline.next();
                                continue;
                            }
                        }
                    }
                    (Some((address, value)), None) => (
                        Self::commitment_key_len(address)?,
                        Some(u64::try_from(value.len()).ok()?),
                        true,
                        false,
                    ),
                    (None, Some((baseline_address, _))) => (
                        Self::commitment_key_len(baseline_address)?,
                        None,
                        false,
                        true,
                    ),
                    (None, None) => break,
                };
            entries = entries.checked_add(1)?;
            bytes = bytes.checked_add(match value_bytes {
                Some(value_bytes) => 9_u64.checked_add(key_bytes)?.checked_add(value_bytes)?,
                None => 5_u64.checked_add(key_bytes)?,
            })?;
            if advance_current {
                current.next();
            }
            if advance_baseline {
                baseline.next();
            }
        }
        Some((entries, bytes))
    }

    /// Begins an isolated write transaction. Dropping it without commit leaves
    /// durable storage byte-identical.
    #[must_use]
    pub fn transaction(&mut self, namespace: StorageNamespace) -> StorageTransaction<'_> {
        StorageTransaction {
            owner: self,
            namespace,
            writes: BTreeMap::new(),
        }
    }

    /// Returns the number of cells visible in exactly one namespace.
    #[must_use]
    pub fn namespace_cell_count(&self, namespace: StorageNamespace) -> usize {
        namespace_cells(&self.cells, namespace).count()
    }

    /// Returns one fixed namespace in canonical key order for the protocol
    /// persistence bridge. The returned copies cannot mutate the held state.
    pub(crate) fn namespace_entries(&self, namespace: StorageNamespace) -> Vec<(Vec<u8>, Vec<u8>)> {
        namespace_cells(&self.cells, namespace)
            .map(|(address, value)| (address.key.clone(), value.clone()))
            .collect()
    }

    /// Returns exact persistent key-plus-value bytes in one namespace.
    /// Adjacent program or principal namespaces never contribute.
    ///
    /// # Errors
    ///
    /// Refuses accounting that cannot fit the runtime's `u64` counters.
    pub fn namespace_persistent_bytes(
        &self,
        namespace: StorageNamespace,
    ) -> Result<u64, StorageError> {
        namespace_cells(&self.cells, namespace)
            .try_fold(0u64, |total, (address, value)| {
                let cell_bytes = metered_bytes(&address.key, Some(value))?;
                total
                    .checked_add(cell_bytes)
                    .ok_or(StorageError::SizeOverflow)
            })
    }

    /// Reads one protocol-owned canonical state cell. This host-side seam is
    /// not linked into either guest ABI and grants no write authority.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal for an invalid namespace or key.
    pub fn protocol_state_value(
        &self,
        namespace: StorageNamespace,
        key: &[u8],
    ) -> Result<Option<Vec<u8>>, StorageError> {
        self.read(namespace, key)
    }

    /// Returns every nonempty namespace and its exact persistent bytes in
    /// canonical namespace order. Protocol state transitions use this to prove
    /// that no occupied namespace escaped responsibility accounting.
    ///
    /// # Errors
    ///
    /// Returns `SizeOverflow` if a namespace size cannot be represented.
    pub fn namespace_sizes(&self) -> Result<Vec<(StorageNamespace, u64)>, StorageError> {
        let mut sizes = BTreeMap::<StorageNamespace, u64>::new();
        for (address, value) in &self.cells {
            let bytes = metered_bytes(&address.key, Some(value))?;
            let size = sizes.entry(address.namespace).or_default();
            *size = size.checked_add(bytes).ok_or(StorageError::SizeOverflow)?;
        }
        Ok(sizes.into_iter().collect())
    }

    /// Returns canonical copies of every cell in one protocol-owned namespace.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal if namespace entries cannot be retrieved.
    pub fn protocol_namespace_entries(
        &self,
        namespace: StorageNamespace,
    ) -> Result<ProtocolEntries, StorageError> {
        self.ensure_accessible(namespace)?;
        Ok(self.namespace_entries(namespace))
    }

    /// Atomically replaces one protocol-owned namespace with an exact canonical cell set.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal if any replacement entry is invalid.
    pub fn replace_protocol_namespace(
        &mut self,
        namespace: StorageNamespace,
        entries: &[(Vec<u8>, Vec<u8>)],
    ) -> Result<(), StorageError> {
        if entries.windows(2).any(|pair| pair[0].0 >= pair[1].0) {
            return Err(StorageError::PrefixTooLarge);
        }
        let existing = self.protocol_namespace_entries(namespace)?;
        let mut transaction = self.transaction(namespace);
        for (key, _) in existing {
            transaction.delete(&key)?;
        }
        for (key, value) in entries {
            transaction.write(key, value)?;
        }
        let _ = transaction.commit();
        Ok(())
    }

    /// Returns canonical copies of cells beneath one protocol-owned key prefix.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal if the namespace or prefix is invalid.
    pub fn protocol_prefix_entries(
        &self,
        namespace: StorageNamespace,
        prefix: &[u8],
    ) -> Result<ProtocolEntries, StorageError> {
        self.ensure_accessible(namespace)?;
        validate_key(prefix)?;
        Ok(self
            .cells
            .range(StorageAddress { namespace, key: prefix.to_vec() }..)
            .take_while(|(address, _)| {
                address.namespace == namespace && address.key.starts_with(prefix)
            })
            .map(|(address, value)| (address.key.clone(), value.clone()))
            .collect())
    }

    /// Returns exact key-plus-value occupancy beneath one protocol-owned prefix.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal for invalid prefix access or byte-count overflow.
    pub fn protocol_prefix_bytes(
        &self,
        namespace: StorageNamespace,
        prefix: &[u8],
    ) -> Result<u64, StorageError> {
        self.protocol_prefix_entries(namespace, prefix)?
            .iter()
            .try_fold(0u64, |total, (key, value)| {
                total
                    .checked_add(metered_bytes(key, Some(value))?)
                    .ok_or(StorageError::SizeOverflow)
            })
    }

    /// Atomically replaces one protocol-owned prefix with an exact canonical cell set.
    ///
    /// # Errors
    ///
    /// Returns a storage refusal if the prefix or a replacement entry is invalid.
    pub fn replace_protocol_prefix(
        &mut self,
        namespace: StorageNamespace,
        prefix: &[u8],
        entries: &[(Vec<u8>, Vec<u8>)],
    ) -> Result<(), StorageError> {
        validate_key(prefix)?;
        if entries.windows(2).any(|pair| pair[0].0 >= pair[1].0)
            || entries.iter().any(|(key, _)| !key.starts_with(prefix))
        {
            return Err(StorageError::PrefixTooLarge);
        }
        let existing = self.protocol_prefix_entries(namespace, prefix)?;
        let mut transaction = self.transaction(namespace);
        for (key, _) in existing {
            transaction.delete(&key)?;
        }
        for (key, value) in entries {
            transaction.write(key, value)?;
        }
        let _ = transaction.commit();
        Ok(())
    }

    /// Computes exact facts for dropping one namespace without mutating this
    /// storage snapshot. The caller charges this preview before committing the
    /// corresponding reclamation.
    pub(crate) fn namespace_drop_preview(
        &self,
        namespace: StorageNamespace,
    ) -> Result<NamespaceDrop, StorageError> {
        self.ensure_accessible(namespace)?;
        reclaim::preview(&self.cells, namespace)
    }

    /// Removes every cell of a preflighted namespace from this storage
    /// snapshot. Only the ABI can obtain a namespace drop fact from a
    /// guest-selected scope and matching write authority.
    pub(crate) fn reclaim_namespace(&mut self, drop: NamespaceDrop) {
        reclaim::apply(&mut self.cells, drop);
    }

    pub(crate) fn read(
        &self,
        namespace: StorageNamespace,
        key: &[u8],
    ) -> Result<Option<Vec<u8>>, StorageError> {
        self.ensure_accessible(namespace)?;
        validate_key(key)?;
        Ok(self
            .cells
            .get(&StorageAddress {
                namespace,
                key: key.to_vec(),
            })
            .cloned())
    }

    pub(crate) fn write(
        &mut self,
        namespace: StorageNamespace,
        key: &[u8],
        value: &[u8],
    ) -> Result<(), StorageError> {
        self.ensure_accessible(namespace)?;
        validate_key(key)?;
        if value.len() > MAX_STORAGE_VALUE_BYTES {
            return Err(StorageError::ValueTooLarge);
        }
        self.cells.insert(
            StorageAddress {
                namespace,
                key: key.to_vec(),
            },
            value.to_vec(),
        );
        Ok(())
    }

    pub(crate) fn delete(
        &mut self,
        namespace: StorageNamespace,
        key: &[u8],
    ) -> Result<(), StorageError> {
        self.ensure_accessible(namespace)?;
        validate_key(key)?;
        self.cells.remove(&StorageAddress {
            namespace,
            key: key.to_vec(),
        });
        Ok(())
    }

    /// Scans one fixed namespace in canonical key order. The cursor is an
    /// externally portable, self-describing continuation token; it is checked
    /// against this exact namespace, prefix, and declared page contract before
    /// any entries are returned.
    ///
    /// # Errors
    ///
    /// Refuses malformed, foreign, or non-canonical cursors, invalid limits,
    /// and an entry that cannot fit the caller-declared complete canonical
    /// page byte ceiling.
    pub(crate) fn scan(
        &self,
        namespace: StorageNamespace,
        prefix: &[u8],
        cursor: &[u8],
        limits: ScanLimits,
    ) -> Result<StorageScan, StorageError> {
        self.ensure_accessible(namespace)?;
        scan_cells(&self.cells, namespace, prefix, cursor, limits)
    }
}

/// An atomic transaction fixed to one namespace at construction.
#[derive(Debug)]
pub struct StorageTransaction<'a> {
    owner: &'a mut Storage,
    namespace: StorageNamespace,
    writes: BTreeMap<Vec<u8>, Option<Vec<u8>>>,
}

impl StorageTransaction<'_> {
    /// Reads only from the transaction's fixed namespace.
    ///
    /// # Errors
    ///
    /// Refuses empty and oversized keys.
    pub fn read(&self, key: &[u8]) -> Result<Option<Vec<u8>>, StorageError> {
        self.owner.ensure_accessible(self.namespace)?;
        validate_key(key)?;
        if let Some(value) = self.writes.get(key) {
            return Ok(value.clone());
        }
        Ok(self
            .owner
            .cells
            .get(&StorageAddress {
                namespace: self.namespace,
                key: key.to_vec(),
            })
            .cloned())
    }

    /// Stages a bounded value in the fixed namespace.
    ///
    /// # Errors
    ///
    /// Refuses empty or oversized keys and oversized values.
    pub fn write(&mut self, key: &[u8], value: &[u8]) -> Result<(), StorageError> {
        self.owner.ensure_accessible(self.namespace)?;
        validate_key(key)?;
        if value.len() > MAX_STORAGE_VALUE_BYTES {
            return Err(StorageError::ValueTooLarge);
        }
        self.writes.insert(key.to_vec(), Some(value.to_vec()));
        Ok(())
    }

    /// Stages deletion of a key in the fixed namespace.
    ///
    /// # Errors
    ///
    /// Refuses empty and oversized keys.
    pub fn delete(&mut self, key: &[u8]) -> Result<(), StorageError> {
        self.owner.ensure_accessible(self.namespace)?;
        validate_key(key)?;
        self.writes.insert(key.to_vec(), None);
        Ok(())
    }

    /// Atomically applies all staged writes and returns the number of changed
    /// cells. No guest-visible operation can alter the transaction namespace.
    #[must_use]
    pub fn commit(self) -> usize {
        let mut changed = 0usize;
        for (key, value) in self.writes {
            let address = StorageAddress {
                namespace: self.namespace,
                key,
            };
            match value {
                Some(value) => {
                    if self.owner.cells.get(&address) != Some(&value) {
                        self.owner.cells.insert(address, value);
                        changed = changed.saturating_add(1);
                    }
                }
                None => {
                    if self.owner.cells.remove(&address).is_some() {
                        changed = changed.saturating_add(1);
                    }
                }
            }
        }
        changed
    }
}

fn validate_key(key: &[u8]) -> Result<(), StorageError> {
    if key.is_empty() {
        return Err(StorageError::EmptyKey);
    }
    if key.len() > MAX_STORAGE_KEY_BYTES {
        return Err(StorageError::KeyTooLarge);
    }
    Ok(())
}

/// Computes exact storage metering bytes for a key and optional value.
///
/// # Errors
///
/// Refuses lengths that cannot fit the runtime's `u64` meter.
pub fn metered_bytes(key: &[u8], value: Option<&[u8]>) -> Result<u64, StorageError> {
    let bytes = key
        .len()
        .checked_add(value.map_or(0, <[u8]>::len))
        .ok_or(StorageError::SizeOverflow)?;
    u64::try_from(bytes).map_err(|_| StorageError::SizeOverflow)
}

#[cfg(test)]
mod replay_visit_tests {
    use super::*;
    #[test]
    fn bounded_commitment_visit_stops_at_first_refusal() {
        let program = ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let mut storage = Storage::new();
        let mut transaction = storage.transaction(StorageNamespace::shared(program));
        transaction.write(b"a", b"one").unwrap_or_else(|error| panic!("write: {error}"));
        transaction.write(b"b", b"two").unwrap_or_else(|error| panic!("write: {error}"));
        transaction.commit();
        let mut count = 0;
        let result = storage.try_for_each_commitment_entry(|_, _| { count += 1; Err(()) });
        assert_eq!(result, Err(())); assert_eq!(count, 1);
        let mut original = Vec::new(); storage.for_each_commitment_entry(|key, value| original.push((key, value.to_vec())));
        let mut bounded = Vec::new();
        storage.try_for_each_commitment_entry(|key, value| { bounded.push((key, value.to_vec())); Ok::<_, ()>(()) }).unwrap_or_else(|()| panic!("traversal"));
        assert_eq!(bounded, original);
    }
}

const STORAGE_REPLAY_DOMAIN: &[u8] = b"LayerX/programs/replay-storage-state/v1\0";

fn replay_storage_field(out: &mut Vec<u8>, value: &[u8], maximum: usize) -> Result<(), crate::replay::ReplayWitnessError> {
    use crate::replay::{append, ReplayWitnessError as E};
    append(out, &u32::try_from(value.len()).map_err(|_| E::Bounds)?.to_be_bytes(), maximum)?;
    append(out, value, maximum)
}
fn replay_namespace(bytes: &[u8]) -> Result<StorageNamespace, crate::replay::ReplayWitnessError> {
    use crate::replay::ReplayWitnessError as E;
    if bytes.len() != 33 && bytes.len() != 65 { return Err(E::Encoding); }
    let program = ProgramId::new(bytes[..32].try_into().map_err(|_| E::Encoding)?).map_err(|_| E::Encoding)?;
    match bytes[32] {
        0 if bytes.len() == 65 => Ok(StorageNamespace::principal(program, PrincipalId::new(bytes[33..].try_into().map_err(|_| E::Encoding)?).map_err(|_| E::Encoding)?)),
        1 if bytes.len() == 33 => Ok(StorageNamespace::shared(program)),
        2 if bytes.len() == 65 => Ok(StorageNamespace::protocol_private(program, bytes[33..].try_into().map_err(|_| E::Encoding)?)),
        _ => Err(E::Encoding),
    }
}
fn replay_storage_copy(bytes: &[u8]) -> Result<Vec<u8>, crate::replay::ReplayWitnessError> {
    let mut value = Vec::new();
    crate::replay::append(&mut value, bytes, bytes.len())?;
    Ok(value)
}

impl Storage {
    pub fn replay_state_bytes(&self, maximum: usize) -> Result<Vec<u8>, crate::replay::ReplayWitnessError> {
        use crate::replay::{append, maximum_bytes, ReplayWitnessError as E};
        maximum_bytes(maximum)?;
        let accessed = self.accessed_namespaces.try_borrow().map_err(|_| E::StateUnavailable)?;
        let mut out = Vec::new();
        append(&mut out, STORAGE_REPLAY_DOMAIN, maximum)?;
        append(&mut out, &u32::try_from(self.cells.len()).map_err(|_| E::Bounds)?.to_be_bytes(), maximum)?;
        for (address, value) in &self.cells {
            let mut namespace = [0; 65];
            let length = address.namespace.write_canonical(&mut namespace);
            replay_storage_field(&mut out, &namespace[..length], maximum)?;
            replay_storage_field(&mut out, &address.key, maximum)?;
            replay_storage_field(&mut out, value, maximum)?;
        }
        for namespaces in [&self.frozen_namespaces, &*accessed] {
            append(&mut out, &u32::try_from(namespaces.len()).map_err(|_| E::Bounds)?.to_be_bytes(), maximum)?;
            for namespace in namespaces {
                let mut bytes = [0; 65];
                let length = namespace.write_canonical(&mut bytes);
                replay_storage_field(&mut out, &bytes[..length], maximum)?;
            }
        }
        Ok(out)
    }

    pub fn decode_untrusted_replay_state(bytes: &[u8], maximum: usize) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::{maximum_bytes, ReplayCursor, ReplayWitnessError as E};
        maximum_bytes(maximum)?;
        if bytes.len() > maximum { return Err(E::Bounds); }
        let mut cursor = ReplayCursor::new(bytes);
        if cursor.take(STORAGE_REPLAY_DOMAIN.len())? != STORAGE_REPLAY_DOMAIN { return Err(E::Encoding); }
        let mut cells = BTreeMap::new();
        let count = usize::try_from(cursor.u32()?).map_err(|_| E::Bounds)?;
        if count > bytes.len() / (12 + 33 + 1) { return Err(E::Bounds); }
        for _ in 0..count {
            let namespace = replay_namespace(cursor.field()?)?;
            let key = cursor.field()?;
            let value = cursor.field()?;
            validate_key(key).map_err(|_| E::Encoding)?;
            if value.len() > MAX_STORAGE_VALUE_BYTES { return Err(E::Bounds); }
            let address = StorageAddress { namespace, key: replay_storage_copy(key)? };
            if cells.last_key_value().is_some_and(|(previous, _)| previous >= &address) { return Err(E::Encoding); }
            cells.insert(address, replay_storage_copy(value)?);
        }
        let mut read_namespaces = || -> Result<BTreeSet<StorageNamespace>, E> {
            let count = usize::try_from(cursor.u32()?).map_err(|_| E::Bounds)?;
            if count > bytes.len() / (4 + 33) { return Err(E::Bounds); }
            let mut namespaces = BTreeSet::new();
            for _ in 0..count {
                let namespace = replay_namespace(cursor.field()?)?;
                if namespaces.last().is_some_and(|previous| previous >= &namespace) { return Err(E::Encoding); }
                namespaces.insert(namespace);
            }
            Ok(namespaces)
        };
        let frozen_namespaces = read_namespaces()?;
        let accessed_namespaces = RefCell::new(read_namespaces()?);
        if !cursor.done() { return Err(E::Encoding); }
        let restored = Self { cells, frozen_namespaces, accessed_namespaces };
        if restored.replay_state_bytes(maximum)?.as_slice() != bytes { return Err(E::Encoding); }
        Ok(restored)
    }

    pub(crate) fn bounded_replay_overlay(&self, baseline: &Self, maximum: usize) -> Result<Vec<(Vec<u8>, Option<Vec<u8>>)>, crate::replay::ReplayWitnessError> {
        use crate::replay::ReplayWitnessError as E;
        crate::replay::maximum_bytes(maximum)?;
        let mut entries = Vec::new();
        let mut encoded_bytes = 4_usize;
        if encoded_bytes > maximum { return Err(E::Bounds); }
        self.try_for_each_commitment_delta(baseline, |key, value| {
            let additional = 5_usize.checked_add(key.len()).and_then(|n| match value { None => Some(n), Some(value) => n.checked_add(4)?.checked_add(value.len()) }).ok_or(E::Bounds)?;
            encoded_bytes = encoded_bytes.checked_add(additional).filter(|n| *n <= maximum).ok_or(E::Bounds)?;
            entries.try_reserve_exact(1).map_err(|_| E::Allocation)?;
            entries.push((key, value.map(replay_storage_copy).transpose()?));
            Ok::<_, E>(())
        })?;
        entries.sort_unstable_by(|left, right| left.0.cmp(&right.0));
        Ok(entries)
    }
}

#[cfg(test)]
mod storage_replay_tests {
    use super::*;
    use crate::replay::ReplayWitnessError;

    #[test]
    fn restores_real_cells_freeze_policy_and_access_history_without_defaulting() {
        let program = ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let principal = PrincipalId::new([2; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
        let principal_namespace = StorageNamespace::principal(program, principal);
        let shared = StorageNamespace::shared(program);
        let frozen = StorageNamespace::protocol_private(program, [3; 32]);
        let mut storage = Storage::new();
        storage.write(principal_namespace, b"a", b"one").unwrap_or_else(|error| panic!("write: {error}"));
        storage.write(principal_namespace, b"b", b"two").unwrap_or_else(|error| panic!("write: {error}"));
        assert_eq!(storage.read(shared, b"absent"), Ok(None));
        storage.frozen_namespaces.insert(frozen);
        let maximum = crate::MAX_TRACE_STATE_BYTES as usize;
        let encoded = storage.replay_state_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        let restored = Storage::decode_untrusted_replay_state(&encoded, encoded.len()).unwrap_or_else(|error| panic!("decode: {error:?}"));
        assert_eq!(restored.cells, storage.cells);
        assert_eq!(restored.frozen_namespaces, storage.frozen_namespaces);
        assert_eq!(*restored.accessed_namespaces.borrow(), *storage.accessed_namespaces.borrow());
        assert_eq!(restored.read(frozen, b"absent"), Err(StorageError::FrozenNamespace));
        assert_eq!(restored.read(principal_namespace, b"a"), Ok(Some(b"one".to_vec())));
        assert_eq!(restored.replay_state_bytes(maximum), Ok(encoded.clone()));
        assert_eq!(storage.replay_state_bytes(encoded.len() - 1), Err(ReplayWitnessError::Bounds));
        let mut trailing = encoded.clone(); trailing.push(0);
        assert!(Storage::decode_untrusted_replay_state(&trailing, maximum).is_err());
        for length in 0..encoded.len() { assert!(Storage::decode_untrusted_replay_state(&encoded[..length], maximum).is_err()); }
        let mut unknown_namespace = encoded;
        unknown_namespace[STORAGE_REPLAY_DOMAIN.len() + 4 + 4 + 32] = 255;
        assert_eq!(Storage::decode_untrusted_replay_state(&unknown_namespace, maximum), Err(ReplayWitnessError::Encoding));
    }

    #[test]
    fn refuses_duplicate_namespace_sets_and_preserves_an_empty_cells_access_log() {
        let program = ProgramId::new([4; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let namespace = StorageNamespace::shared(program);
        let storage = Storage::new();
        assert_eq!(storage.read(namespace, b"missing"), Ok(None));
        let maximum = crate::MAX_TRACE_STATE_BYTES as usize;
        let encoded = storage.replay_state_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        let restored = Storage::decode_untrusted_replay_state(&encoded, maximum).unwrap_or_else(|error| panic!("decode: {error:?}"));
        assert!(restored.cells.is_empty());
        assert!(restored.accessed_namespaces.borrow().contains(&namespace));
        let mut malformed = STORAGE_REPLAY_DOMAIN.to_vec();
        malformed.extend_from_slice(&0_u32.to_be_bytes());
        malformed.extend_from_slice(&2_u32.to_be_bytes());
        for _ in 0..2 { replay_storage_field(&mut malformed, &namespace.canonical_bytes(), maximum).unwrap_or_else(|error| panic!("field: {error:?}")); }
        malformed.extend_from_slice(&0_u32.to_be_bytes());
        assert_eq!(Storage::decode_untrusted_replay_state(&malformed, maximum), Err(ReplayWitnessError::Encoding));
    }

    #[test]
    fn fallible_delta_matches_real_legacy_delta_and_stops_on_first_refusal() {
        let program = ProgramId::new([5; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let namespace = StorageNamespace::shared(program);
        let mut baseline = Storage::new();
        baseline.write(namespace, b"a", b"old").unwrap_or_else(|error| panic!("write: {error}"));
        baseline.write(namespace, b"b", b"remove").unwrap_or_else(|error| panic!("write: {error}"));
        let mut current = baseline.clone();
        current.write(namespace, b"a", b"new").unwrap_or_else(|error| panic!("write: {error}"));
        current.delete(namespace, b"b").unwrap_or_else(|error| panic!("delete: {error}"));
        current.write(namespace, b"c", b"added").unwrap_or_else(|error| panic!("write: {error}"));
        let mut original = Vec::new();
        current.for_each_commitment_delta(&baseline, |key, value| original.push((key, value.map(<[u8]>::to_vec))));
        original.sort_by(|left, right| left.0.cmp(&right.0));
        assert_eq!(current.bounded_replay_overlay(&baseline, crate::MAX_TRACE_STATE_BYTES as usize), Ok(original));
        let mut visited = 0;
        let result = current.try_for_each_commitment_delta(&baseline, |_, _| { visited += 1; Err(()) });
        assert_eq!(result, Err(())); assert_eq!(visited, 1);
    }
}
