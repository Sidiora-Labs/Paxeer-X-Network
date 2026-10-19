//! Constant-time equality for fixed digests and variable secret byte strings.

use subtle::ConstantTimeEq as _;

/// Compares two byte strings without data-dependent early exit.
#[must_use]
pub fn eq(left: &[u8], right: &[u8]) -> bool {
    left.len() == right.len() && bool::from(left.ct_eq(right))
}

/// Compares fixed-width values without data-dependent branching.
#[must_use]
pub fn eq_fixed<const N: usize>(left: &[u8; N], right: &[u8; N]) -> bool {
    bool::from(left.ct_eq(right))
}
