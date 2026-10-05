//! Runtime adapters over the canonical ABI version policy the program SDK owns.

pub use layerx_program_sdk::abi_policy::{
    account_profile2_guest_supported, admit_abi_upgrade, admit_abi_version, capability_encoding,
    AbiVersionRefusal, CapabilityEncoding, ABI_V1_VERSION, ABI_V2_VERSION, ABI_V3_VERSION,
    ABI_V4_VERSION, ABI_V5_VERSION,
};

use crate::AbiRevision;

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
        ABI_V5_VERSION => Ok(AbiRevision::V5),
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
        AbiRevision::V5 => ABI_V5_VERSION,
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
        assert_eq!(capability_encoding(5), Ok(CapabilityEncoding::V2));
        assert_eq!(
            capability_encoding(6),
            Err(AbiVersionRefusal::Unsupported { requested: 6 })
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
            AbiRevision::V5,
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
