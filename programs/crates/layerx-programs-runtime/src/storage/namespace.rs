//! Closed storage namespace identities fixed by the host before guest entry.

use super::{PrincipalId, ProgramId};
use core::cmp::Ordering;

const PRINCIPAL_SCOPED_TAG: u8 = 0;
const PROGRAM_SHARED_TAG: u8 = 1;
const PROTOCOL_PRIVATE_TAG: u8 = 2;

/// A durable namespace owned by exactly one program.
///
/// Ordering is protocol-significant and is implemented explicitly as owning
/// program, frozen scope tag, then principal when present. Guests never
/// construct this value; the runtime fixes both variants from the executing
/// frame before guest entry.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StorageNamespace {
    /// State isolated by both executing program and invoking principal.
    PrincipalScoped {
        program: ProgramId,
        principal: PrincipalId,
    },
    /// State shared by every principal invoking the owning program.
    ProgramShared {
        program: ProgramId,
    },
    ProtocolPrivate {
        program: ProgramId,
        scope: [u8; 32],
    },
}

impl StorageNamespace {
    /// Fixes a principal-scoped namespace for one executing frame.
    #[must_use]
    pub const fn principal(program: ProgramId, principal: PrincipalId) -> Self {
        Self::PrincipalScoped { program, principal }
    }

    /// Fixes the program-shared namespace for one executing frame.
    #[must_use]
    pub const fn shared(program: ProgramId) -> Self {
        Self::ProgramShared { program }
    }

    #[must_use]
    pub const fn protocol_private(program: ProgramId, scope: [u8; 32]) -> Self {
        Self::ProtocolPrivate { program, scope }
    }

    /// Returns the program that exclusively owns this namespace.
    #[must_use]
    pub const fn program(self) -> ProgramId {
        match self {
            Self::PrincipalScoped { program, .. }
            | Self::ProgramShared { program }
            | Self::ProtocolPrivate { program, .. } => program,
        }
    }

    /// Returns the principal scope, or `None` for program-shared state.
    #[must_use]
    pub const fn principal_scope(self) -> Option<PrincipalId> {
        match self {
            Self::PrincipalScoped { principal, .. } => Some(principal),
            Self::ProgramShared { .. } | Self::ProtocolPrivate { .. } => None,
        }
    }

    /// Returns the frozen canonical namespace bytes used by ordered storage
    /// consumers: program, scope tag, then principal for principal scope.
    #[must_use]
    pub fn canonical_bytes(self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(65);
        bytes.extend_from_slice(&self.program().bytes());
        match self {
            Self::PrincipalScoped { principal, .. } => {
                bytes.push(PRINCIPAL_SCOPED_TAG);
                bytes.extend_from_slice(&principal.bytes());
            }
            Self::ProgramShared { .. } => bytes.push(PROGRAM_SHARED_TAG),
            Self::ProtocolPrivate { scope, .. } => {
                bytes.push(PROTOCOL_PRIVATE_TAG);
                bytes.extend_from_slice(&scope);
            }
        }
        bytes
    }

    pub(crate) fn write_canonical(self, output: &mut [u8; 65]) -> usize {
        output[..32].copy_from_slice(&self.program().bytes());
        match self {
            Self::PrincipalScoped { principal, .. } => {
                output[32] = PRINCIPAL_SCOPED_TAG;
                output[33..65].copy_from_slice(&principal.bytes());
                65
            }
            Self::ProgramShared { .. } => {
                output[32] = PROGRAM_SHARED_TAG;
                33
            }
            Self::ProtocolPrivate { scope, .. } => {
                output[32] = PROTOCOL_PRIVATE_TAG;
                output[33..65].copy_from_slice(&scope);
                65
            }
        }
    }
}

impl Ord for StorageNamespace {
    fn cmp(&self, other: &Self) -> Ordering {
        self.program()
            .cmp(&other.program())
            .then_with(|| match (*self, *other) {
                (
                    Self::PrincipalScoped {
                        principal: left, ..
                    },
                    Self::PrincipalScoped {
                        principal: right, ..
                    },
                ) => left.cmp(&right),
                (Self::ProgramShared { .. }, Self::ProgramShared { .. }) => Ordering::Equal,
                (
                    Self::ProtocolPrivate { scope: left, .. },
                    Self::ProtocolPrivate { scope: right, .. },
                ) => left.cmp(&right),
                (Self::ProgramShared { .. }, Self::PrincipalScoped { .. })
                | (Self::ProtocolPrivate { .. }, _) => Ordering::Greater,
                (Self::PrincipalScoped { .. }, Self::ProgramShared { .. })
                | (_, Self::ProtocolPrivate { .. }) => Ordering::Less,
            })
    }
}

impl PartialOrd for StorageNamespace {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn program(byte: u8) -> ProgramId {
        ProgramId::new([byte; 32]).unwrap_or_else(|error| panic!("program: {error}"))
    }

    fn principal(byte: u8) -> PrincipalId {
        PrincipalId::new([byte; 32]).unwrap_or_else(|error| panic!("principal: {error}"))
    }

    fn every_variant() -> Vec<StorageNamespace> {
        let mut namespaces = Vec::new();
        for owner in [9, 1, 5] {
            namespaces.push(StorageNamespace::shared(program(owner)));
            for scope in [7, 2] {
                namespaces.push(StorageNamespace::protocol_private(
                    program(owner),
                    [scope; 32],
                ));
            }
            for actor in [8, 3, 6] {
                namespaces.push(StorageNamespace::principal(
                    program(owner),
                    principal(actor),
                ));
            }
        }
        namespaces
    }

    #[test]
    fn canonical_bytes_are_frozen_for_both_program_scopes() {
        let owner = program(1);
        let mut shared = owner.bytes().to_vec();
        shared.push(1);
        assert_eq!(StorageNamespace::shared(owner).canonical_bytes(), shared);
        let mut scoped = owner.bytes().to_vec();
        scoped.push(0);
        scoped.extend_from_slice(&[2; 32]);
        assert_eq!(
            StorageNamespace::principal(owner, principal(2)).canonical_bytes(),
            scoped
        );
        let mut private = owner.bytes().to_vec();
        private.push(2);
        private.extend_from_slice(&[3; 32]);
        assert_eq!(
            StorageNamespace::protocol_private(owner, [3; 32]).canonical_bytes(),
            private
        );
        for namespace in every_variant() {
            let mut written = [0; 65];
            let length = namespace.write_canonical(&mut written);
            assert_eq!(&written[..length], namespace.canonical_bytes().as_slice());
        }
    }

    #[test]
    fn ordering_matches_canonical_bytes_across_every_variant() {
        let namespaces = every_variant();
        for left in &namespaces {
            for right in &namespaces {
                assert_eq!(
                    left.cmp(right),
                    left.canonical_bytes().cmp(&right.canonical_bytes()),
                    "{left:?} vs {right:?}"
                );
                assert_eq!(left.partial_cmp(right), Some(left.cmp(right)));
                assert_eq!(
                    left == right,
                    left.canonical_bytes() == right.canonical_bytes()
                );
            }
        }
        let mut forward = namespaces.clone();
        forward.sort();
        let mut reverse = namespaces;
        reverse.reverse();
        reverse.sort();
        assert_eq!(forward, reverse);
        assert_eq!(
            forward.first(),
            Some(&StorageNamespace::principal(program(1), principal(3)))
        );
        assert_eq!(
            forward.last(),
            Some(&StorageNamespace::protocol_private(program(9), [7; 32]))
        );
    }

    #[test]
    fn every_namespace_names_exactly_its_owning_program_and_scope() {
        let owner = program(4);
        let actor = principal(5);
        let scoped = StorageNamespace::principal(owner, actor);
        let shared = StorageNamespace::shared(owner);
        assert_eq!(
            scoped,
            StorageNamespace::PrincipalScoped {
                program: owner,
                principal: actor
            }
        );
        assert_eq!(shared, StorageNamespace::ProgramShared { program: owner });
        assert_eq!(
            (scoped.program(), scoped.principal_scope()),
            (owner, Some(actor))
        );
        assert_eq!((shared.program(), shared.principal_scope()), (owner, None));
        let private = StorageNamespace::protocol_private(owner, [6; 32]);
        assert_eq!(
            (private.program(), private.principal_scope()),
            (owner, None)
        );
        assert_ne!(scoped, shared);
        assert_ne!(shared, StorageNamespace::shared(program(6)));
        assert_ne!(scoped, StorageNamespace::principal(program(6), actor));
        assert_ne!(scoped, StorageNamespace::principal(owner, principal(6)));
        for namespace in every_variant() {
            assert_eq!(
                namespace.canonical_bytes()[..32],
                namespace.program().bytes()
            );
        }
    }
}
