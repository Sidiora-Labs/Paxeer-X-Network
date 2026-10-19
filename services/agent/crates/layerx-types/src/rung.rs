//! Write rungs a client can hold for a submitted transaction.

/// Evidence rung reached by a write, ordered from weakest to strongest.
///
/// Every caller uses these meanings:
/// - `Submitted`: only the transaction hash is known.
/// - `Receipted`: a sequencer receipt verified against the pinned `ReceiptKeys` entry.
/// - `Committed`: a verified Comet commit at the receipt height.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd, Hash)]
#[repr(u8)]
pub enum Rung {
    /// Only the transaction hash is known.
    Submitted = 0,
    /// A sequencer receipt verified against the pinned `ReceiptKeys` entry.
    Receipted = 1,
    /// A verified Comet commit at the receipt height.
    Committed = 2,
}

impl Rung {
    /// Returns the stable wire rank from zero through two.
    #[must_use]
    pub const fn wire_rank(self) -> u8 {
        self as u8
    }

    /// Returns the canonical rung name.
    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            Self::Submitted => "submitted",
            Self::Receipted => "receipted",
            Self::Committed => "committed",
        }
    }

    /// Parses a canonical rung name; any other text, including retired level names, is refused.
    #[must_use]
    pub fn from_name(name: &str) -> Option<Self> {
        match name {
            "submitted" => Some(Self::Submitted),
            "receipted" => Some(Self::Receipted),
            "committed" => Some(Self::Committed),
            _ => None,
        }
    }
}
