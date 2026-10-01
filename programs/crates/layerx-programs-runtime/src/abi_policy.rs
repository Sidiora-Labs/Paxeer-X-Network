//! Frozen ABI transition policy shared by consensus-derived projections.

use core::fmt::{self, Display};

use crate::{AbiRevision, ABI_V1_VERSION, ABI_V2_VERSION, ABI_V3_VERSION, ABI_V4_VERSION};

/// The sole typed refusal for an invalid ABI version transition.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AbiVersionRefusal {
    Unsupported { requested: u16 },
    Downgrade { current: u16, requested: u16 },
}

impl Display for AbiVersionRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Unsupported { requested } => {
                write!(formatter, "unsupported program ABI version {requested}")
            }
            Self::Downgrade { current, requested } => write!(
                formatter,
                "program ABI version {requested} downgrades current version {current}"
            ),
        }
    }
}

impl std::error::Error for AbiVersionRefusal {}

/// Accepts an ABI version for a new deployment or historical replay.
///
/// # Errors
///
/// Returns a version refusal when the requested ABI is not admitted.
pub const fn admit_abi_version(requested: u16) -> Result<(), AbiVersionRefusal> {
    match requested {
        ABI_V1_VERSION | ABI_V2_VERSION | ABI_V3_VERSION | ABI_V4_VERSION => Ok(()),
        _ => Err(AbiVersionRefusal::Unsupported { requested }),
    }
}

/// Freezes upgrades as monotonic transitions across supported ABI versions.
///
/// # Errors
///
/// Returns a version refusal when either ABI or the requested transition is unsupported.
pub const fn admit_abi_upgrade(current: u16, requested: u16) -> Result<(), AbiVersionRefusal> {
    match (admit_abi_version(current), admit_abi_version(requested)) {
        (Err(refusal), _) | (_, Err(refusal)) => Err(refusal),
        (Ok(()), Ok(())) if requested < current => {
            Err(AbiVersionRefusal::Downgrade { current, requested })
        }
        (Ok(()), Ok(())) => Ok(()),
    }
}

/// The capability encoding an admitted program ABI version carries.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CapabilityEncoding {
    /// The ABI-one canonical grant encoding.
    V1,
    /// The ABI-two canonical grant encoding every later admitted ABI carries.
    V2,
}

/// Binds an ABI version to the capability encoding its grants are written in.
///
/// Every projection that decodes the grants of one call - the CALL scheduling
/// projection as much as the execution that follows it - reads this binding, so
/// a call cannot be planned against one encoding and executed against another.
///
/// # Errors
///
/// Returns a version refusal when the requested ABI is not admitted.
pub const fn capability_encoding(requested: u16) -> Result<CapabilityEncoding, AbiVersionRefusal> {
    match requested {
        ABI_V1_VERSION => Ok(CapabilityEncoding::V1),
        ABI_V2_VERSION | ABI_V3_VERSION | ABI_V4_VERSION => Ok(CapabilityEncoding::V2),
        _ => Err(AbiVersionRefusal::Unsupported { requested }),
    }
}

/// Selects the version-specific validator revision for an admitted ABI version.
///
/// Every module validation route, including interface admission, resolves its
/// import validator through this binding instead of a local version switch.
///
/// # Errors
///
/// Returns a version refusal when the requested ABI is not admitted.
pub const fn abi_revision(requested: u16) -> Result<AbiRevision, AbiVersionRefusal> {
    match requested {
        ABI_V1_VERSION => Ok(AbiRevision::V1),
        ABI_V2_VERSION => Ok(AbiRevision::V2),
        ABI_V3_VERSION => Ok(AbiRevision::V3),
        ABI_V4_VERSION => Ok(AbiRevision::V4),
        _ => Err(AbiVersionRefusal::Unsupported { requested }),
    }
}

/// The ABI version a validated module's revision records.
#[must_use]
pub const fn abi_version(revision: AbiRevision) -> u16 {
    match revision {
        AbiRevision::V1 => ABI_V1_VERSION,
        AbiRevision::V2 => ABI_V2_VERSION,
        AbiRevision::V3 => ABI_V3_VERSION,
        AbiRevision::V4 => ABI_V4_VERSION,
    }
}

#[cfg(test)]
mod tests {
    use super::{
        abi_revision, abi_version, admit_abi_version, capability_encoding, AbiVersionRefusal,
        CapabilityEncoding,
    };
    use crate::AbiRevision;

    #[test]
    fn every_admitted_abi_version_carries_one_capability_encoding() {
        for requested in 0..=u16::from(u8::MAX) {
            assert_eq!(
                admit_abi_version(requested).is_ok(),
                capability_encoding(requested).is_ok(),
                "ABI {requested} admission and capability encoding disagree"
            );
        }
        assert_eq!(capability_encoding(1), Ok(CapabilityEncoding::V1));
        assert_eq!(capability_encoding(2), Ok(CapabilityEncoding::V2));
        assert_eq!(capability_encoding(3), Ok(CapabilityEncoding::V2));
        assert_eq!(capability_encoding(4), Ok(CapabilityEncoding::V2));
        assert_eq!(
            capability_encoding(5),
            Err(AbiVersionRefusal::Unsupported { requested: 5 })
        );
        for requested in 0..=u16::from(u8::MAX) {
            assert_eq!(
                admit_abi_version(requested).is_ok(),
                abi_revision(requested).is_ok(),
                "ABI {requested} admission and validator revision disagree"
            );
        }
    }

    #[test]
    fn a_validated_revision_and_its_recorded_version_select_the_same_encoding() {
        for revision in [
            AbiRevision::V1,
            AbiRevision::V2,
            AbiRevision::V3,
            AbiRevision::V4,
        ] {
            let recorded = abi_version(revision);
            assert!(admit_abi_version(recorded).is_ok());
            assert_eq!(abi_revision(recorded), Ok(revision));
            assert_eq!(
                capability_encoding(recorded),
                Ok(if matches!(revision, AbiRevision::V1) {
                    CapabilityEncoding::V1
                } else {
                    CapabilityEncoding::V2
                })
            );
        }
    }
}
