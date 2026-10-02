//! Textual purpose labels and their versioned 32-byte commitments.
//!
//! A purpose label is text the caller wants committed into a 32-byte purpose field such as a
//! grant `purpose_hash` or a native budget `purpose`. A caller that already holds 32 raw bytes
//! sets the field directly; those bytes are never hashed again.

pub use layerx_crypto::purpose::{
    purpose_commitment_v1, PurposeCommitmentError, PURPOSE_COMMITMENT_DOMAIN_V1,
};

/// Refusal to build a [`PurposeLabel`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurposeLabelError {
    /// The text cannot be committed.
    Commitment(PurposeCommitmentError),
    /// The text is 64 hexadecimal characters, which is a literal 32-byte identifier and is
    /// never hashed as a label.
    LiteralIdentifier,
}

impl std::fmt::Display for PurposeLabelError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Commitment(error) => error.fmt(formatter),
            Self::LiteralIdentifier => formatter.write_str("purpose_literal_identifier"),
        }
    }
}

impl std::error::Error for PurposeLabelError {}

/// A purpose text together with its version 1 commitment.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurposeLabel {
    text: String,
    commitment: [u8; 32],
}

impl PurposeLabel {
    /// Commits to the exact text with [`purpose_commitment_v1`] and keeps the text.
    ///
    /// # Errors
    ///
    /// Refuses an empty text and a 64-character hexadecimal text, which is a literal identifier.
    pub fn new(text: &str) -> Result<Self, PurposeLabelError> {
        if text.len() == 64 && text.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return Err(PurposeLabelError::LiteralIdentifier);
        }
        let commitment = purpose_commitment_v1(text).map_err(PurposeLabelError::Commitment)?;
        Ok(Self {
            text: text.to_owned(),
            commitment,
        })
    }

    /// The exact text that was committed.
    #[must_use]
    pub fn text(&self) -> &str {
        &self.text
    }

    /// The 32-byte value for a purpose field.
    #[must_use]
    pub const fn commitment(&self) -> [u8; 32] {
        self.commitment
    }
}

#[cfg(test)]
mod tests {
    use super::{purpose_commitment_v1, PurposeCommitmentError, PurposeLabel, PurposeLabelError};

    #[test]
    fn label_keeps_text_and_commits_it() -> Result<(), PurposeLabelError> {
        let label = PurposeLabel::new(" rent")?;
        assert_eq!(label.text(), " rent");
        assert_eq!(
            Ok(label.commitment()),
            purpose_commitment_v1(" rent").map_err(PurposeLabelError::Commitment)
        );
        Ok(())
    }

    #[test]
    fn empty_label_is_refused() {
        assert_eq!(
            PurposeLabel::new(""),
            Err(PurposeLabelError::Commitment(PurposeCommitmentError::Empty))
        );
    }

    #[test]
    fn literal_identifier_is_not_hashed() {
        assert_eq!(
            PurposeLabel::new(&"ab".repeat(32)),
            Err(PurposeLabelError::LiteralIdentifier)
        );
    }
}
