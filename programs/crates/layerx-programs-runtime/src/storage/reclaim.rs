//! Exact reclamation of one host-fixed storage namespace.

use std::collections::BTreeMap;

use super::{metered_bytes, namespace_cells, StorageAddress, StorageError, StorageNamespace};

/// Exact provisional released-occupancy facts produced by dropping one namespace.
///
/// The facts are recorded with the committed activity so task 29.5's occupancy
/// ledger can net the pre- and post-activity state without reconstructing a
/// policy from wall-clock time or from post-commit storage.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NamespaceDrop {
    namespace: StorageNamespace,
    reclaimed_cells: u64,
    reclaimed_key_value_bytes: u64,
    metered_work: u64,
}

impl NamespaceDrop {
    #[must_use]
    pub const fn namespace(self) -> StorageNamespace {
        self.namespace
    }

    #[must_use]
    pub const fn reclaimed_cells(self) -> u64 {
        self.reclaimed_cells
    }

    #[must_use]
    pub const fn reclaimed_key_value_bytes(self) -> u64 {
        self.reclaimed_key_value_bytes
    }

    /// Returns the exact storage-write work for this drop: each reclaimed cell
    /// plus every reclaimed key and value byte.
    #[must_use]
    pub const fn metered_work(self) -> u64 {
        self.metered_work
    }
}

/// Computes exact namespace reclamation before a meter or state mutation.
pub(crate) fn preview(
    cells: &BTreeMap<StorageAddress, Vec<u8>>,
    namespace: StorageNamespace,
) -> Result<NamespaceDrop, StorageError> {
    let (reclaimed_cells, reclaimed_key_value_bytes) = namespace_cells(cells, namespace).try_fold(
        (0u64, 0u64),
        |(cell_count, byte_count), (address, value)| {
            let reclaimed_cells = cell_count
                .checked_add(1)
                .ok_or(StorageError::SizeOverflow)?;
            let reclaimed_bytes = metered_bytes(&address.key, Some(value))?;
            let reclaimed_key_value_bytes = byte_count
                .checked_add(reclaimed_bytes)
                .ok_or(StorageError::SizeOverflow)?;
            Ok((reclaimed_cells, reclaimed_key_value_bytes))
        },
    )?;
    let metered_work = reclaimed_cells
        .checked_add(reclaimed_key_value_bytes)
        .ok_or(StorageError::SizeOverflow)?;
    Ok(NamespaceDrop {
        namespace,
        reclaimed_cells,
        reclaimed_key_value_bytes,
        metered_work,
    })
}

/// Removes every cell of exactly the namespace described by `drop`.
///
/// The caller must use a preview from the same storage snapshot. The storage
/// transaction owns that snapshot, so no concurrent mutation can make the
/// recorded provisional fact diverge before this deterministic removal.
pub(crate) fn apply(cells: &mut BTreeMap<StorageAddress, Vec<u8>>, drop: NamespaceDrop) {
    let addresses: Vec<_> = namespace_cells(cells, drop.namespace)
        .map(|(address, _)| address.clone())
        .collect();
    for address in addresses {
        cells.remove(&address);
    }
}

impl NamespaceDrop {
    pub(crate) fn from_untrusted_replay_fields(
        namespace: StorageNamespace,
        reclaimed_cells: u64,
        reclaimed_key_value_bytes: u64,
        metered_work: u64,
    ) -> Result<Self, crate::replay::ReplayWitnessError> {
        if reclaimed_cells.checked_add(reclaimed_key_value_bytes) != Some(metered_work) {
            return Err(crate::replay::ReplayWitnessError::Encoding);
        }
        Ok(Self {
            namespace,
            reclaimed_cells,
            reclaimed_key_value_bytes,
            metered_work,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::replay::ReplayWitnessError;
    use crate::storage::{PrincipalId, ProgramId, MAX_STORAGE_KEY_BYTES};

    fn program(bytes: [u8; 32]) -> ProgramId {
        ProgramId::new(bytes).unwrap_or_else(|error| panic!("program: {error}"))
    }

    fn principal(bytes: [u8; 32]) -> PrincipalId {
        PrincipalId::new(bytes).unwrap_or_else(|error| panic!("principal: {error}"))
    }

    fn adjacent(byte: u8, last: u8) -> [u8; 32] {
        let mut bytes = [byte; 32];
        bytes[31] = last;
        bytes
    }

    fn insert(
        cells: &mut BTreeMap<StorageAddress, Vec<u8>>,
        namespace: StorageNamespace,
        key: &[u8],
        value: &[u8],
    ) {
        cells.insert(
            StorageAddress {
                namespace,
                key: key.to_vec(),
            },
            value.to_vec(),
        );
    }

    fn plane() -> (BTreeMap<StorageAddress, Vec<u8>>, Vec<StorageNamespace>) {
        let owner = program(adjacent(4, 4));
        let actor = principal(adjacent(6, 6));
        let namespaces = vec![
            StorageNamespace::principal(owner, actor),
            StorageNamespace::principal(owner, principal(adjacent(6, 5))),
            StorageNamespace::principal(owner, principal(adjacent(6, 7))),
            StorageNamespace::shared(owner),
            StorageNamespace::protocol_private(owner, adjacent(6, 6)),
            StorageNamespace::principal(program(adjacent(4, 3)), actor),
            StorageNamespace::principal(program(adjacent(4, 5)), actor),
            StorageNamespace::shared(program(adjacent(4, 5))),
        ];
        let long_key = vec![0xff; MAX_STORAGE_KEY_BYTES];
        let mut cells = BTreeMap::new();
        for (index, namespace) in namespaces.iter().copied().enumerate() {
            let tag = u8::try_from(index).unwrap_or_else(|error| panic!("tag: {error}"));
            insert(&mut cells, namespace, &[0x00], &[tag]);
            insert(&mut cells, namespace, b"same", &[tag, tag]);
            insert(&mut cells, namespace, &long_key, &[tag; 3]);
        }
        (cells, namespaces)
    }

    #[test]
    fn drop_removes_every_cell_of_one_namespace_and_no_byte_adjacent_namespace() {
        for (target_index, target) in plane().1.into_iter().enumerate() {
            let (mut cells, namespaces) = plane();
            let before = cells.clone();
            let drop = preview(&cells, target).unwrap_or_else(|error| panic!("preview: {error}"));
            assert_eq!(cells, before);
            assert_eq!(drop.namespace(), target);
            assert_eq!(drop.reclaimed_cells(), 3);
            let expected_bytes = u64::try_from(1 + 1 + 4 + 2 + MAX_STORAGE_KEY_BYTES + 3)
                .unwrap_or_else(|error| panic!("bytes: {error}"));
            assert_eq!(drop.reclaimed_key_value_bytes(), expected_bytes);
            assert_eq!(drop.metered_work(), expected_bytes + 3);

            apply(&mut cells, drop);
            let survivors: BTreeMap<_, _> = before
                .into_iter()
                .filter(|(address, _)| address.namespace != target)
                .collect();
            assert_eq!(cells, survivors);
            assert_eq!(namespace_cells(&cells, target).count(), 0);
            for (index, namespace) in namespaces.into_iter().enumerate() {
                if index != target_index {
                    assert_eq!(namespace_cells(&cells, namespace).count(), 3);
                }
            }
        }
    }

    #[test]
    fn drop_of_an_absent_namespace_is_exactly_zero_and_leaves_the_plane_unchanged() {
        let (mut cells, _) = plane();
        let before = cells.clone();
        let absent =
            StorageNamespace::principal(program(adjacent(4, 4)), principal(adjacent(9, 9)));
        let drop = preview(&cells, absent).unwrap_or_else(|error| panic!("preview: {error}"));
        assert_eq!(
            (
                drop.reclaimed_cells(),
                drop.reclaimed_key_value_bytes(),
                drop.metered_work()
            ),
            (0, 0, 0)
        );
        apply(&mut cells, drop);
        assert_eq!(cells, before);
    }

    #[test]
    fn replay_fields_must_carry_the_exact_metered_work_of_the_reclamation() {
        let (cells, namespaces) = plane();
        let drop =
            preview(&cells, namespaces[0]).unwrap_or_else(|error| panic!("preview: {error}"));
        assert_eq!(
            NamespaceDrop::from_untrusted_replay_fields(
                drop.namespace(),
                drop.reclaimed_cells(),
                drop.reclaimed_key_value_bytes(),
                drop.metered_work(),
            )
            .ok(),
            Some(drop)
        );
        for metered_work in [
            drop.metered_work() - 1,
            drop.metered_work() + 1,
            drop.reclaimed_key_value_bytes(),
        ] {
            assert!(matches!(
                NamespaceDrop::from_untrusted_replay_fields(
                    drop.namespace(),
                    drop.reclaimed_cells(),
                    drop.reclaimed_key_value_bytes(),
                    metered_work,
                ),
                Err(ReplayWitnessError::Encoding)
            ));
        }
        assert!(matches!(
            NamespaceDrop::from_untrusted_replay_fields(drop.namespace(), u64::MAX, 1, 0),
            Err(ReplayWitnessError::Encoding)
        ));
    }
}
