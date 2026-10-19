//! Exact fixed-width amounts and wide intermediates.

/// Checked amount arithmetic failure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ArithmeticError {
    /// Addition or multiplication exceeded the fixed width.
    Overflow,
    /// Subtraction would become negative.
    Underflow,
}

/// A protocol amount represented by the exact unsigned 128-bit width.
#[derive(Clone, Copy, Debug, Default, Eq, Ord, PartialEq, PartialOrd)]
pub struct Amount(u128);

impl Amount {
    /// Zero amount.
    pub const ZERO: Self = Self(0);

    /// Constructs from an exact integer. No floating-point conversion exists.
    #[must_use]
    pub const fn from_u128(value: u128) -> Self {
        Self(value)
    }

    /// Decodes the canonical big-endian 16-byte representation.
    #[must_use]
    pub const fn from_be_bytes(bytes: [u8; 16]) -> Self {
        Self(u128::from_be_bytes(bytes))
    }

    /// Returns the exact integer.
    #[must_use]
    pub const fn value(self) -> u128 {
        self.0
    }

    /// Returns the canonical big-endian bytes.
    #[must_use]
    pub const fn to_be_bytes(self) -> [u8; 16] {
        self.0.to_be_bytes()
    }

    /// Adds without wrapping.
    ///
    /// # Errors
    ///
    /// Returns [`ArithmeticError::Overflow`] when the sum does not fit.
    pub const fn checked_add(self, right: Self) -> Result<Self, ArithmeticError> {
        match self.0.checked_add(right.0) {
            Some(value) => Ok(Self(value)),
            None => Err(ArithmeticError::Overflow),
        }
    }

    /// Subtracts without wrapping or signed reinterpretation.
    ///
    /// # Errors
    ///
    /// Returns [`ArithmeticError::Underflow`] when `right` is greater.
    pub const fn checked_sub(self, right: Self) -> Result<Self, ArithmeticError> {
        match self.0.checked_sub(right.0) {
            Some(value) => Ok(Self(value)),
            None => Err(ArithmeticError::Underflow),
        }
    }
}

/// Exact 256-bit unsigned intermediate, least-significant word first as in C.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct U256([u64; 4]);

impl U256 {
    /// Constructs from four exact least-significant-first words.
    #[must_use]
    pub const fn from_words(words: [u64; 4]) -> Self {
        Self(words)
    }

    /// Returns the four exact least-significant-first words.
    #[must_use]
    pub const fn words(self) -> [u64; 4] {
        self.0
    }

    /// Adds at full width without wrapping.
    ///
    /// # Errors
    ///
    /// Returns [`ArithmeticError::Overflow`] when carry leaves word four.
    pub const fn checked_add(self, right: Self) -> Result<Self, ArithmeticError> {
        let mut out = [0_u64; 4];
        let mut carry = false;
        let mut index = 0;
        while index < 4 {
            let (partial, first) = self.0[index].overflowing_add(right.0[index]);
            let carry_word = if carry { 1 } else { 0 };
            let (value, second) = partial.overflowing_add(carry_word);
            out[index] = value;
            carry = first || second;
            index += 1;
        }
        if carry {
            Err(ArithmeticError::Overflow)
        } else {
            Ok(Self(out))
        }
    }
}
