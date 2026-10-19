//! Non-interchangeable protocol identifiers.

use crate::limits::{IDENTIFIER_BYTES, MAX_DID_BYTES};

/// A constructor error for a protocol-declared byte bound.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LengthError {
    /// Maximum accepted byte count.
    pub maximum: usize,
    /// Presented byte count.
    pub actual: usize,
}

macro_rules! fixed_identifier {
    ($name:ident) => {
        #[doc = concat!("A distinct ", stringify!($name), " with the protocol fixed width.")]
        #[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name([u8; IDENTIFIER_BYTES]);

        impl $name {
            /// Constructs the identifier from its exact canonical bytes.
            #[must_use]
            pub const fn new(bytes: [u8; IDENTIFIER_BYTES]) -> Self {
                Self(bytes)
            }

            /// Returns the exact canonical bytes.
            #[must_use]
            pub const fn bytes(self) -> [u8; IDENTIFIER_BYTES] {
                self.0
            }
        }
    };
}

/// A distinct activity identifier with the protocol fixed width.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ActivityId([u8; IDENTIFIER_BYTES]);

impl ActivityId {
    /// Constructs the identifier from its exact canonical bytes.
    #[must_use]
    pub const fn new(bytes: [u8; IDENTIFIER_BYTES]) -> Self {
        Self(bytes)
    }

    /// Returns the exact canonical bytes.
    #[must_use]
    pub const fn bytes(self) -> [u8; IDENTIFIER_BYTES] {
        self.0
    }
}

fixed_identifier!(BatchId);
fixed_identifier!(CheckpointId);
fixed_identifier!(TransactionId);
fixed_identifier!(IdempotencyKey);
fixed_identifier!(AssetId);

/// A canonical DID byte string bounded before allocation into protocol data.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct Did(Vec<u8>);

impl Did {
    /// Constructs a non-empty DID no longer than the protocol maximum.
    ///
    /// # Errors
    ///
    /// Returns [`LengthError`] for an empty or over-long DID.
    pub fn new(bytes: &[u8]) -> Result<Self, LengthError> {
        if bytes.is_empty() || bytes.len() > MAX_DID_BYTES {
            return Err(LengthError {
                maximum: MAX_DID_BYTES,
                actual: bytes.len(),
            });
        }
        Ok(Self(bytes.to_vec()))
    }

    /// Borrows the exact DID bytes.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}
