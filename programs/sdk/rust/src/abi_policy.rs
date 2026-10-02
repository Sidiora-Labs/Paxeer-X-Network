//! Frozen ABI version policy shared by the runtime, the registry and typed bindings.
//!
//! This module is the sole definition of the admitted program ABI versions,
//! their monotonic upgrade order and the capability encoding each version
//! carries. It uses only `core`, so the guest SDK, the host runtime and the
//! SDK build script compile the same policy source.

use core::fmt::{self, Display};

/// Frozen version-one program ABI.
pub const ABI_V1_VERSION: u16 = 1;
/// Frozen version-two program ABI.
pub const ABI_V2_VERSION: u16 = 2;
/// Frozen version-three program ABI.
pub const ABI_V3_VERSION: u16 = 3;
/// Frozen version-four program ABI.
pub const ABI_V4_VERSION: u16 = 4;

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

impl core::error::Error for AbiVersionRefusal {}

const fn supported(requested: u16) -> Result<(), AbiVersionRefusal> {
    match requested {
        ABI_V1_VERSION | ABI_V2_VERSION | ABI_V3_VERSION | ABI_V4_VERSION => Ok(()),
        _ => Err(AbiVersionRefusal::Unsupported { requested }),
    }
}

/// Accepts an ABI version for a new deployment or historical replay.
///
/// A new deployment is the transition from the base ABI, so admission and
/// upgrades share one monotonic check.
///
/// # Errors
///
/// Returns a version refusal when the requested ABI is not admitted.
pub const fn admit_abi_version(requested: u16) -> Result<(), AbiVersionRefusal> {
    admit_abi_upgrade(ABI_V1_VERSION, requested)
}

/// Freezes upgrades as monotonic transitions across supported ABI versions.
///
/// # Errors
///
/// Returns a version refusal when either ABI or the requested transition is unsupported.
pub const fn admit_abi_upgrade(current: u16, requested: u16) -> Result<(), AbiVersionRefusal> {
    match (supported(current), supported(requested)) {
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

#[cfg(test)]
mod tests {
    use super::{
        admit_abi_upgrade, admit_abi_version, capability_encoding, AbiVersionRefusal,
        CapabilityEncoding,
    };

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
    }

    #[test]
    fn upgrades_are_monotonic_across_admitted_versions() {
        for current in 1..=4 {
            for requested in 1..=4 {
                assert_eq!(
                    admit_abi_upgrade(current, requested),
                    if requested < current {
                        Err(AbiVersionRefusal::Downgrade { current, requested })
                    } else {
                        Ok(())
                    }
                );
            }
        }
        assert_eq!(
            admit_abi_upgrade(0, 1),
            Err(AbiVersionRefusal::Unsupported { requested: 0 })
        );
        assert_eq!(
            admit_abi_upgrade(4, 5),
            Err(AbiVersionRefusal::Unsupported { requested: 5 })
        );
    }
}
