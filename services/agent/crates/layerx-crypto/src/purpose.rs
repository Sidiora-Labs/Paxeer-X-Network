//! Versioned purpose commitment for textual purpose labels.
//!
//! This is a new versioned commitment contract declared for issuer and SDK use. It is not a
//! historical codec and never reinterprets existing 32-byte purpose references: a caller that
//! already holds a 32-byte purpose reference keeps it as is.

use sha2::{Digest as _, Sha256};

/// Domain separator prefixed to the purpose text before hashing.
pub const PURPOSE_COMMITMENT_DOMAIN_V1: &[u8; 29] = b"layerx:purpose-commitment:v1\0";

/// Refusal to commit to a purpose text.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurposeCommitmentError {
    /// The purpose text is empty.
    Empty,
}

impl std::fmt::Display for PurposeCommitmentError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::Empty => "empty_purpose",
        })
    }
}

impl std::error::Error for PurposeCommitmentError {}

/// Commits to the exact UTF-8 bytes of `text` as SHA-256 over
/// [`PURPOSE_COMMITMENT_DOMAIN_V1`] followed by the text. The text is neither normalized nor
/// trimmed.
///
/// # Errors
///
/// Returns [`PurposeCommitmentError::Empty`] for an empty text.
pub fn purpose_commitment_v1(text: &str) -> Result<[u8; 32], PurposeCommitmentError> {
    if text.is_empty() {
        return Err(PurposeCommitmentError::Empty);
    }
    Ok(Sha256::new()
        .chain_update(PURPOSE_COMMITMENT_DOMAIN_V1)
        .chain_update(text.as_bytes())
        .finalize()
        .into())
}

#[cfg(test)]
mod tests {
    use sha2::{Digest as _, Sha256};

    use super::{purpose_commitment_v1, PurposeCommitmentError, PURPOSE_COMMITMENT_DOMAIN_V1};

    #[test]
    fn commitment_is_sha256_of_domain_then_text() {
        let mut preimage = PURPOSE_COMMITMENT_DOMAIN_V1.to_vec();
        preimage.extend_from_slice("rent".as_bytes());
        let expected: [u8; 32] = Sha256::digest(&preimage).into();
        assert_eq!(purpose_commitment_v1("rent"), Ok(expected));
    }

    #[test]
    fn commitment_matches_pinned_vector() {
        assert_eq!(
            purpose_commitment_v1("groceries"),
            Ok([
                0xbc, 0xc5, 0x5b, 0x16, 0xc0, 0x95, 0xeb, 0xfa, 0x98, 0x5a, 0xbd, 0xc1, 0x66, 0xcf,
                0xfb, 0xa3, 0x51, 0xa3, 0x98, 0xf8, 0xc0, 0xf1, 0x8d, 0xe3, 0x14, 0x54, 0xd3, 0x65,
                0x39, 0x71, 0xa9, 0x08,
            ])
        );
    }

    #[test]
    fn empty_text_is_refused() {
        assert_eq!(
            purpose_commitment_v1(""),
            Err(PurposeCommitmentError::Empty)
        );
    }

    #[test]
    fn surrounding_whitespace_is_committed() {
        assert_ne!(
            purpose_commitment_v1(" rent \n"),
            purpose_commitment_v1("rent")
        );
    }

    #[test]
    fn different_texts_differ() {
        assert_ne!(
            purpose_commitment_v1("rent"),
            purpose_commitment_v1("groceries")
        );
    }
}
