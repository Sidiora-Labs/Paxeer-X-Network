//! Type-enforced zeroization and fixed rendering for secret values.

use std::fmt;

use zeroize::{Zeroize, Zeroizing};

/// A value that zeroizes on drop and cannot be borrowed or rendered publicly.
pub struct Secret<T: Zeroize>(Zeroizing<T>);

impl<T: Zeroize> Secret<T> {
    /// Moves a secret value directly into zeroizing storage.
    #[must_use]
    pub fn new(value: T) -> Self {
        Self(Zeroizing::new(value))
    }

    pub(crate) fn expose<R>(&self, operation: impl FnOnce(&T) -> R) -> R {
        operation(&self.0)
    }
}

impl<const N: usize> Secret<[u8; N]> {
    /// Compares a candidate without exposing the stored value.
    #[must_use]
    pub fn matches(&self, candidate: &[u8; N]) -> bool {
        self.expose(|value| crate::ct::eq_fixed(value, candidate))
    }
}

impl<T: Zeroize> fmt::Debug for Secret<T> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("[REDACTED]")
    }
}

impl<T: Zeroize> fmt::Display for Secret<T> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("[REDACTED]")
    }
}
