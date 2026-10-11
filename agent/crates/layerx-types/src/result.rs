//! Exact protocol result codes derived from `include/layerx/lxp_result.h`.
//!
//! `build.rs` generates the code table from the normative `LXP_RESULT_CODE_LIST`
//! macro in that header and the Retryable column of the error code registry
//! (`spec/layerx-merge/contracts/address-table.md` section (b)), and refuses to
//! build this crate when a header code has no single valid registry row.

/// Protocol result-code domain. Numeric ranges match the C17 core.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ResultDomain {
    /// Successful execution.
    Success,
    /// Canonical codec failure.
    Codec,
    /// Envelope validation failure.
    Envelope,
    /// Authority validation failure.
    Authority,
    /// Sequencing failure.
    Sequencing,
    /// Ledger invariant failure.
    Ledger,
    /// Exact arithmetic failure.
    Arithmetic,
    /// Metering or fee failure.
    Metering,
    /// Protocol module failure.
    Module,
    /// Batch or availability failure.
    Batch,
    /// Durable storage failure.
    Storage,
    /// Fatal consensus invariant failure.
    Fatal,
    /// A non-protocol or future positive value.
    Unknown,
}

/// Whether retrying byte-identical activity bytes can become meaningful after
/// external protocol state advances.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Retriability {
    /// The same activity must not be retried automatically.
    Terminal,
    /// Resolution may be retried without changing the activity intent.
    Retriable,
}

macro_rules! protocol_result_codes {
    ($( $variant:ident = $raw:literal, $retry:ident; )+) => {
        /// Every result code currently declared by the normative C17 header.
        #[derive(Clone, Copy, Debug, Eq, PartialEq)]
        #[repr(i32)]
        pub enum KnownResult {
            $( $variant = $raw, )+
        }

        impl KnownResult {
            /// All declared result variants, in canonical header order.
            pub const ALL: &'static [Self] = &[$( Self::$variant, )+];

            /// Returns the exact signed 32-bit consensus number.
            #[must_use]
            pub const fn raw(self) -> i32 {
                match self { $( Self::$variant => $raw, )+ }
            }

            /// Classifies retry behaviour from one auditable data table.
            #[must_use]
            pub const fn retriability(self) -> Retriability {
                match self { $( Self::$variant => Retriability::$retry, )+ }
            }

            const fn from_raw(raw: i32) -> Option<Self> {
                match raw { $( $raw => Some(Self::$variant), )+ _ => None }
            }

            const fn name(self) -> &'static str {
                match self { $( Self::$variant => stringify!($variant), )+ }
            }
        }
    };
}

include!(concat!(env!("OUT_DIR"), "/result_codes.rs"));

impl KnownResult {
    /// Parses the registry wire form `LXP_<code>: <message>` of an ABI
    /// `Error(string)` revert, where `<code>` is the header name without its
    /// `LXP_ERR_` or `LXP_` prefix. Success and fatal values never travel as a
    /// revert, so neither parses; an unknown or malformed reason is `None`.
    #[must_use]
    pub fn from_revert_reason(text: &str) -> Option<Self> {
        let (code, _message) = text.split_once(": ")?;
        let wire = code.strip_prefix("LXP_")?;
        Self::ALL.iter().copied().find(|known| {
            !matches!(
                ResultCode::from(*known).domain(),
                ResultDomain::Success | ResultDomain::Fatal
            ) && wire_name_matches(known.name(), wire)
        })
    }
}

impl Retriability {
    /// Classifies the revert reason of a submitted transaction through the
    /// mirror table; an unknown or malformed reason fails closed as terminal.
    #[must_use]
    pub fn of_revert_reason(text: &str) -> Self {
        KnownResult::from_revert_reason(text).map_or(Self::Terminal, KnownResult::retriability)
    }
}

/// Compares a mirror variant name with its upper snake case header word list.
fn wire_name_matches(variant: &str, wire: &str) -> bool {
    let mut wire = wire.bytes();
    for (index, byte) in variant.bytes().enumerate() {
        if index > 0 && byte.is_ascii_uppercase() && wire.next() != Some(b'_') {
            return false;
        }
        if wire.next() != Some(byte.to_ascii_uppercase()) {
            return false;
        }
    }
    wire.next().is_none()
}

/// A lossless protocol result code. Future unknown numbers survive unchanged.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ResultCode(i32);

impl ResultCode {
    /// Preserves any signed protocol number without reinterpretation.
    #[must_use]
    pub const fn from_raw(raw: i32) -> Self {
        Self(raw)
    }

    /// Returns the byte-for-byte numeric value carried in a receipt.
    #[must_use]
    pub const fn raw(self) -> i32 {
        self.0
    }

    /// Returns the typed variant when this version knows the number.
    #[must_use]
    pub const fn known(self) -> Option<KnownResult> {
        KnownResult::from_raw(self.0)
    }

    /// Returns the protocol domain using the exact C17 range partition.
    #[must_use]
    pub const fn domain(self) -> ResultDomain {
        match self.0 {
            0 => ResultDomain::Success,
            ..=-1000 => ResultDomain::Fatal,
            -999..=-900 => ResultDomain::Storage,
            -899..=-800 => ResultDomain::Batch,
            -799..=-700 => ResultDomain::Module,
            -699..=-600 => ResultDomain::Metering,
            -599..=-500 => ResultDomain::Arithmetic,
            -499..=-400 => ResultDomain::Ledger,
            -399..=-300 => ResultDomain::Sequencing,
            -299..=-200 => ResultDomain::Authority,
            -199..=-100 => ResultDomain::Envelope,
            -99..=-1 => ResultDomain::Codec,
            _ => ResultDomain::Unknown,
        }
    }

    /// Unknown result numbers are fail-closed and therefore terminal.
    #[must_use]
    pub const fn retriability(self) -> Retriability {
        match self.known() {
            Some(known) => known.retriability(),
            None => Retriability::Terminal,
        }
    }
}

impl From<KnownResult> for ResultCode {
    fn from(value: KnownResult) -> Self {
        Self(value.raw())
    }
}
