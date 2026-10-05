//! Guest-side refusal taxonomy.
//!
//! A program fails for exactly two reasons: a protocol value did not satisfy
//! the bound its constructor enforces, or the host refused the operation.
//!
//! Both travel as integers, because integers are the only values the boundary
//! carries. The first band mirrors the host status codes the runtime itself
//! produces. The second band names the guest-side refusals the host never
//! produces, so a program that refuses its own input says which invariant
//! broke instead of collapsing everything onto one code. Every authoring
//! language ships the same two bands with the same numbers, and
//! [`ProgramError::abi_code`] performs the one collapse onto the host band.
//! These numbers cross the guest boundary inside canonical execution evidence
//! and are consensus data: renumbering one is a protocol-version change.

use core::fmt::{self, Display};

/// Status returned when the invoking activity granted no such authority.
pub const STATUS_DENIED: i32 = -1;
/// Status returned when an argument or encoding is not well formed.
pub const STATUS_INVALID: i32 = -2;
/// Status returned when a length or amount leaves its declared bound.
pub const STATUS_BOUNDS: i32 = -3;
/// Status returned when the deterministic meter refused the operation.
pub const STATUS_METER: i32 = -4;
/// Status returned when receipt evidence is absent or does not match.
pub const STATUS_EVIDENCE: i32 = -5;
/// Status returned when a well-formed signature does not verify or recover.
pub const STATUS_VERIFY_FAILED: i32 = -6;

/// Status reserved for authoring languages whose bindings admit an absent
/// argument. These bindings make absence unrepresentable, so the number is
/// held in the shared band and never produced here.
pub const STATUS_NULL_ARGUMENT: i32 = -16;
/// Status returned when a storage key carries no bytes.
pub const STATUS_EMPTY_KEY: i32 = -17;
/// Status returned when a storage key leaves its declared bound.
pub const STATUS_KEY_TOO_LARGE: i32 = -18;
/// Status returned when a storage value leaves its declared bound.
pub const STATUS_VALUE_TOO_LARGE: i32 = -19;
/// Status returned when an event topic carries no bytes.
pub const STATUS_EMPTY_TOPIC: i32 = -20;
/// Status returned when an event topic leaves its declared bound.
pub const STATUS_TOPIC_TOO_LARGE: i32 = -21;
/// Status returned when an event payload leaves its declared bound.
pub const STATUS_DATA_TOO_LARGE: i32 = -22;
/// Status returned when call input leaves its declared bound.
pub const STATUS_INPUT_TOO_LARGE: i32 = -23;
/// Status returned when a monetary amount is the refused zero.
pub const STATUS_ZERO_AMOUNT: i32 = -24;
/// Status returned when an identifier is the all-zero reserved value.
pub const STATUS_RESERVED_IDENTIFIER: i32 = -25;
/// Status returned when one authority key is declared twice.
pub const STATUS_DUPLICATE_CAPABILITY: i32 = -26;
/// Status returned when a capability set leaves its declared capacity.
pub const STATUS_CAPABILITY_LIMIT: i32 = -27;
/// Status returned when an encoded capability list leaves its ABI bound.
pub const STATUS_CAPABILITY_BYTES: i32 = -28;
/// Status returned when a caller-owned buffer cannot hold the value.
pub const STATUS_BUFFER_TOO_SMALL: i32 = -29;
/// Status returned when a receipt view is not canonically encoded.
pub const STATUS_RECEIPT_ENCODING: i32 = -30;
/// Status returned when exact integer arithmetic leaves the protocol width.
pub const STATUS_OVERFLOW: i32 = -31;
/// Status returned when exact integer arithmetic would become negative.
pub const STATUS_UNDERFLOW: i32 = -32;

/// Refusal reported by a `layerx_v1` host function.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HostRefusal {
    /// The capability was not granted, or narrowing would escalate it.
    Denied,
    /// The ABI version, capability encoding or argument shape is invalid.
    Invalid,
    /// A length, amount or storage bound was exceeded.
    Bounds,
    /// The deterministic meter refused the operation.
    Meter,
    /// The named receipt evidence is absent or does not match.
    Evidence,
    /// A well-formed signature failed verification or public-key recovery.
    VerificationFailed,
    /// The host reported a status outside the frozen refusal set.
    Unknown(i32),
}

impl HostRefusal {
    /// Classifies one negative host status.
    #[must_use]
    pub const fn from_status(status: i32) -> Self {
        match status {
            STATUS_DENIED => Self::Denied,
            STATUS_INVALID => Self::Invalid,
            STATUS_BOUNDS => Self::Bounds,
            STATUS_METER => Self::Meter,
            STATUS_EVIDENCE => Self::Evidence,
            STATUS_VERIFY_FAILED => Self::VerificationFailed,
            other => Self::Unknown(other),
        }
    }

    /// Returns the frozen status this refusal travels as.
    #[must_use]
    pub const fn status(self) -> i32 {
        match self {
            Self::Denied => STATUS_DENIED,
            Self::Invalid => STATUS_INVALID,
            Self::Bounds => STATUS_BOUNDS,
            Self::Meter => STATUS_METER,
            Self::Evidence => STATUS_EVIDENCE,
            Self::VerificationFailed => STATUS_VERIFY_FAILED,
            Self::Unknown(status) => status,
        }
    }
}

impl Display for HostRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Denied => formatter.write_str("capability was not granted"),
            Self::Invalid => formatter.write_str("host argument or encoding is invalid"),
            Self::Bounds => formatter.write_str("operation exceeds an ABI bound"),
            Self::Meter => formatter.write_str("deterministic meter refused the operation"),
            Self::Evidence => formatter.write_str("verified receipt facts do not match"),
            Self::VerificationFailed => formatter.write_str("signature verification failed"),
            Self::Unknown(status) => write!(formatter, "unclassified host status {status}"),
        }
    }
}

/// Field of the program vocabulary whose invariant was violated.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Field {
    /// A monetary amount.
    Amount,
    /// A destination account identifier.
    Account,
    /// An asset identifier.
    Asset,
    /// A caller-owned byte buffer.
    Buffer,
    /// The input handed to a called program.
    CallInput,
    /// The result code a called program returns.
    CallResult,
    /// A capability grant or grant list.
    Capability,
    /// The canonical encoding of a capability list.
    CapabilityEncoding,
    /// An event payload.
    EventData,
    /// An event topic.
    EventTopic,
    /// A program identifier.
    Program,
    /// A decoded receipt view.
    Receipt,
    /// A receipt digest.
    ReceiptDigest,
    /// A namespaced storage key.
    StorageKey,
    /// A namespaced storage value.
    StorageValue,
}

impl Display for Field {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Amount => formatter.write_str("amount"),
            Self::Account => formatter.write_str("account"),
            Self::Asset => formatter.write_str("asset"),
            Self::Buffer => formatter.write_str("buffer"),
            Self::CallInput => formatter.write_str("call input"),
            Self::CallResult => formatter.write_str("call result"),
            Self::Capability => formatter.write_str("capability"),
            Self::CapabilityEncoding => formatter.write_str("capability encoding"),
            Self::EventData => formatter.write_str("event data"),
            Self::EventTopic => formatter.write_str("event topic"),
            Self::Program => formatter.write_str("program"),
            Self::Receipt => formatter.write_str("receipt"),
            Self::ReceiptDigest => formatter.write_str("receipt digest"),
            Self::StorageKey => formatter.write_str("storage key"),
            Self::StorageValue => formatter.write_str("storage value"),
        }
    }
}

/// Invariant a value failed at construction.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Reason {
    /// The value carries no bytes.
    Empty,
    /// The value is the zero reserved for absence.
    Zero,
    /// The value exceeds its declared bound.
    TooLarge,
    /// The destination is shorter than the value it must hold.
    TooSmall,
    /// The authority key is already present in the set.
    Duplicate,
    /// The encoding does not match the frozen format.
    Malformed,
    /// Exact integer arithmetic left the protocol width.
    Overflow,
    /// Exact integer subtraction would become negative.
    Underflow,
}

impl Display for Reason {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => formatter.write_str("is empty"),
            Self::Zero => formatter.write_str("is the reserved zero value"),
            Self::TooLarge => formatter.write_str("exceeds its declared bound"),
            Self::TooSmall => formatter.write_str("is smaller than the value it must hold"),
            Self::Duplicate => formatter.write_str("is declared twice"),
            Self::Malformed => formatter.write_str("is not canonically encoded"),
            Self::Overflow => formatter.write_str("overflows the protocol width"),
            Self::Underflow => formatter.write_str("underflows below zero"),
        }
    }
}

/// Construction failure naming both the field and the invariant.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ValueError {
    /// Field whose invariant was violated.
    pub field: Field,
    /// Invariant the field failed.
    pub reason: Reason,
}

impl ValueError {
    /// Names one violated invariant.
    #[must_use]
    pub const fn new(field: Field, reason: Reason) -> Self {
        Self { field, reason }
    }

    /// Returns the guest-band status naming this exact refusal.
    #[must_use]
    pub const fn status(self) -> i32 {
        match (self.field, self.reason) {
            (Field::StorageKey, Reason::Empty) => STATUS_EMPTY_KEY,
            (Field::StorageKey, Reason::TooLarge) => STATUS_KEY_TOO_LARGE,
            (Field::StorageValue, Reason::TooLarge) => STATUS_VALUE_TOO_LARGE,
            (Field::EventTopic, Reason::Empty) => STATUS_EMPTY_TOPIC,
            (Field::EventTopic, Reason::TooLarge) => STATUS_TOPIC_TOO_LARGE,
            (Field::EventData, Reason::TooLarge) => STATUS_DATA_TOO_LARGE,
            (Field::CallInput, Reason::TooLarge) => STATUS_INPUT_TOO_LARGE,
            (Field::Amount, Reason::Zero) => STATUS_ZERO_AMOUNT,
            (Field::Amount, Reason::Overflow) => STATUS_OVERFLOW,
            (Field::Amount, Reason::Underflow) => STATUS_UNDERFLOW,
            (
                Field::Account | Field::Asset | Field::Program | Field::ReceiptDigest,
                Reason::Zero,
            ) => STATUS_RESERVED_IDENTIFIER,
            (Field::Capability, Reason::Duplicate) => STATUS_DUPLICATE_CAPABILITY,
            (Field::Capability, Reason::TooLarge) => STATUS_CAPABILITY_LIMIT,
            (Field::CapabilityEncoding, Reason::TooLarge) => STATUS_CAPABILITY_BYTES,
            (Field::Buffer, Reason::TooSmall | Reason::TooLarge) => STATUS_BUFFER_TOO_SMALL,
            (Field::Receipt, Reason::Malformed) => STATUS_RECEIPT_ENCODING,
            _ => self.abi_status(),
        }
    }

    /// Collapses this refusal onto the frozen host status band.
    ///
    /// A bound, a capacity or an exact-integer overflow becomes
    /// [`STATUS_BOUNDS`]; a reserved value, a duplicate authority or a
    /// non-canonical encoding becomes [`STATUS_INVALID`].
    #[must_use]
    pub const fn abi_status(self) -> i32 {
        match self.reason {
            Reason::Empty
            | Reason::TooLarge
            | Reason::TooSmall
            | Reason::Overflow
            | Reason::Underflow => STATUS_BOUNDS,
            Reason::Zero | Reason::Duplicate | Reason::Malformed => STATUS_INVALID,
        }
    }
}

impl Display for ValueError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{} {}", self.field, self.reason)
    }
}

/// Stable guest-side refusal taxonomy.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProgramError {
    /// A protocol value failed the bound its constructor enforces.
    Value(ValueError),
    /// A host function refused the operation.
    Host(HostRefusal),
}

impl ProgramError {
    /// Names one violated construction invariant.
    #[must_use]
    pub const fn value(field: Field, reason: Reason) -> Self {
        Self::Value(ValueError::new(field, reason))
    }

    /// Classifies one raw host status.
    ///
    /// # Errors
    ///
    /// Returns the typed refusal for every negative status.
    pub const fn from_status(status: i32) -> Result<i32, Self> {
        if status < 0 {
            Err(Self::Host(HostRefusal::from_status(status)))
        } else {
            Ok(status)
        }
    }

    /// Returns the exact status this refusal travels as.
    #[must_use]
    pub const fn code(self) -> i32 {
        match self {
            Self::Value(error) => error.status(),
            Self::Host(refusal) => refusal.status(),
        }
    }

    /// Returns the frozen host status this refusal collapses onto.
    #[must_use]
    pub const fn abi_code(self) -> i32 {
        match self {
            Self::Value(error) => error.abi_status(),
            Self::Host(refusal) => refusal.status(),
        }
    }

    /// Returns the integer status an entrypoint reports for this refusal.
    #[must_use]
    pub fn status(self) -> i64 {
        i64::from(self.code())
    }
}

impl Display for ProgramError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Value(error) => write!(formatter, "value refusal: {error}"),
            Self::Host(refusal) => write!(formatter, "host refusal: {refusal}"),
        }
    }
}

impl core::error::Error for ProgramError {}

impl From<HostRefusal> for ProgramError {
    fn from(value: HostRefusal) -> Self {
        Self::Host(value)
    }
}

impl From<ValueError> for ProgramError {
    fn from(value: ValueError) -> Self {
        Self::Value(value)
    }
}

/// Stable candidate refusal classes mirrored from the runtime receipt vocabulary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u32)]
pub enum RefusalClass {
    Rejected = 1,
    InvalidInput = 2,
    Unauthorized = 3,
    Conflict = 4,
    NotFound = 5,
    RuntimeFault = 254,
    Legacy = 255,
}

/// Canonical class vocabulary mirrored by runtime parity checks.
pub const REFUSAL_CLASS_MANIFEST: &str = "Rejected=1\0InvalidInput=2\0Unauthorized=3\0Conflict=4\0NotFound=5\0RuntimeFault=254\0Legacy=255\0";

impl RefusalClass {
    #[must_use]
    pub const fn code(self) -> u32 {
        self as u32
    }

    #[must_use]
    pub const fn is_guest_publishable(self) -> bool {
        matches!(
            self,
            Self::Rejected
                | Self::InvalidInput
                | Self::Unauthorized
                | Self::Conflict
                | Self::NotFound
        )
    }

    /// Decodes a stable refusal-class discriminant.
    ///
    /// # Errors
    ///
    /// Returns an error when `code` is not a declared class.
    pub const fn decode(code: u32) -> Result<Self, ProgramError> {
        match code {
            1 => Ok(Self::Rejected),
            2 => Ok(Self::InvalidInput),
            3 => Ok(Self::Unauthorized),
            4 => Ok(Self::Conflict),
            5 => Ok(Self::NotFound),
            254 => Ok(Self::RuntimeFault),
            255 => Ok(Self::Legacy),
            _ => Err(ProgramError::value(Field::Buffer, Reason::Malformed)),
        }
    }
}

/// Allocation-free borrowed candidate refusal reason.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RefusalReason<'a>(&'a [u8]);

impl<'a> RefusalReason<'a> {
    /// Borrows a bounded binary refusal reason.
    ///
    /// # Errors
    ///
    /// Returns an error when the reason exceeds the candidate ABI bound.
    pub const fn new(bytes: &'a [u8]) -> Result<Self, ProgramError> {
        if bytes.len() > crate::abi::MAX_REFUSAL_REASON_BYTES {
            return Err(ProgramError::value(Field::Buffer, Reason::TooLarge));
        }
        Ok(Self(bytes))
    }

    #[must_use]
    pub const fn bytes(self) -> &'a [u8] {
        self.0
    }

    /// Returns the length of the canonical `u32 length || bytes` encoding.
    #[must_use]
    pub const fn encoded_len(self) -> usize {
        4 + self.0.len()
    }

    /// Writes the canonical `u32 length || bytes` encoding into `output`.
    ///
    /// # Errors
    ///
    /// Returns an error when `output` is shorter than [`Self::encoded_len`].
    pub fn encode_into(self, output: &mut [u8]) -> Result<usize, ProgramError> {
        let length = u32::try_from(self.0.len())
            .map_err(|_| ProgramError::value(Field::Buffer, Reason::TooLarge))?;
        let (prefix, rest) = output
            .split_first_chunk_mut::<4>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooSmall))?;
        let body = rest
            .get_mut(..self.0.len())
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooSmall))?;
        *prefix = length.to_be_bytes();
        body.copy_from_slice(self.0);
        Ok(self.encoded_len())
    }

    /// Decodes a canonical length-prefixed refusal reason.
    ///
    /// # Errors
    ///
    /// Returns an error for malformed, oversized, truncated, or trailing input.
    pub fn decode(encoded: &'a [u8]) -> Result<Self, ProgramError> {
        let length = encoded
            .get(..4)
            .and_then(|bytes| <[u8; 4]>::try_from(bytes).ok())
            .map(u32::from_be_bytes)
            .and_then(|length| usize::try_from(length).ok())
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?;
        if length > crate::abi::MAX_REFUSAL_REASON_BYTES {
            return Err(ProgramError::value(Field::Buffer, Reason::TooLarge));
        }
        let end = 4usize
            .checked_add(length)
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?;
        if end != encoded.len() {
            return Err(ProgramError::value(Field::Buffer, Reason::Malformed));
        }
        Self::new(
            encoded
                .get(4..end)
                .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?,
        )
    }
}

/// Guest-constructible candidate refusal without a forgeable program identity.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProgramRefusal<'a> {
    class: RefusalClass,
    reason: RefusalReason<'a>,
}

impl<'a> ProgramRefusal<'a> {
    /// Constructs a guest-publishable refusal.
    ///
    /// # Errors
    ///
    /// Returns an error for host-only refusal classes.
    pub const fn new(class: RefusalClass, reason: RefusalReason<'a>) -> Result<Self, ProgramError> {
        if !class.is_guest_publishable() {
            return Err(ProgramError::value(Field::Buffer, Reason::Malformed));
        }
        Ok(Self { class, reason })
    }

    /// Decodes a canonical guest refusal.
    ///
    /// # Errors
    ///
    /// Returns an error for malformed input or a host-only class.
    pub fn decode(encoded: &'a [u8]) -> Result<Self, ProgramError> {
        let class = encoded
            .get(..4)
            .and_then(|bytes| <[u8; 4]>::try_from(bytes).ok())
            .map(u32::from_be_bytes)
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?;
        let class = RefusalClass::decode(class)?;
        let reason = RefusalReason::decode(
            encoded
                .get(4..)
                .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?,
        )?;
        Self::new(class, reason)
    }

    #[must_use]
    pub const fn class(self) -> RefusalClass {
        self.class
    }

    #[must_use]
    pub const fn reason(self) -> RefusalReason<'a> {
        self.reason
    }

    /// Returns the length of the canonical `u32 class || reason` encoding.
    #[must_use]
    pub const fn encoded_len(self) -> usize {
        4 + self.reason.encoded_len()
    }

    /// Writes the canonical `u32 class || reason` encoding into `output`.
    ///
    /// # Errors
    ///
    /// Returns an error when `output` is shorter than [`Self::encoded_len`].
    pub fn encode_into(self, output: &mut [u8]) -> Result<usize, ProgramError> {
        let (class, rest) = output
            .split_first_chunk_mut::<4>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooSmall))?;
        let written = self.reason.encode_into(rest)?;
        *class = self.class.code().to_be_bytes();
        Ok(4 + written)
    }
}

/// Width of the refusing program identifier inside a failure encoding.
const FAILURE_PROGRAM_BYTES: usize = 32;

/// Host-authenticated program failure as an activity receipt carries it.
///
/// The runtime names the frame that refused, never the frame that observed
/// the refusal, so `program` is the actual cause of a nested failure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProgramFailure<'a> {
    program: crate::ProgramId,
    class: RefusalClass,
    reason: RefusalReason<'a>,
}

impl<'a> ProgramFailure<'a> {
    /// Constructs a canonical failure naming the refusing program.
    ///
    /// # Errors
    ///
    /// Host-only refusal classes require an empty reason.
    pub const fn new(
        program: crate::ProgramId,
        class: RefusalClass,
        reason: RefusalReason<'a>,
    ) -> Result<Self, ProgramError> {
        if !class.is_guest_publishable() && !reason.bytes().is_empty() {
            return Err(ProgramError::value(Field::Buffer, Reason::Malformed));
        }
        Ok(Self {
            program,
            class,
            reason,
        })
    }

    /// Strictly decodes `program || u32 class || u32 length || reason`.
    ///
    /// # Errors
    ///
    /// Returns an error for a reserved program, an unknown class, a reason
    /// past its bound, a host-only class carrying a reason, truncated input
    /// or trailing input.
    pub fn decode(encoded: &'a [u8]) -> Result<Self, ProgramError> {
        let (program, rest) = encoded
            .split_first_chunk::<FAILURE_PROGRAM_BYTES>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?;
        let (class, rest) = rest
            .split_first_chunk::<4>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::Malformed))?;
        Self::new(
            crate::ProgramId::new(*program)?,
            RefusalClass::decode(u32::from_be_bytes(*class))?,
            RefusalReason::decode(rest)?,
        )
    }

    #[must_use]
    pub const fn program(self) -> crate::ProgramId {
        self.program
    }

    #[must_use]
    pub const fn class(self) -> RefusalClass {
        self.class
    }

    #[must_use]
    pub const fn reason(self) -> RefusalReason<'a> {
        self.reason
    }

    /// Returns the length of the canonical failure encoding.
    #[must_use]
    pub const fn encoded_len(self) -> usize {
        FAILURE_PROGRAM_BYTES + 4 + self.reason.encoded_len()
    }

    /// Writes the canonical failure encoding into `output`.
    ///
    /// # Errors
    ///
    /// Returns an error when `output` is shorter than [`Self::encoded_len`].
    pub fn encode_into(self, output: &mut [u8]) -> Result<usize, ProgramError> {
        let (program, rest) = output
            .split_first_chunk_mut::<FAILURE_PROGRAM_BYTES>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooSmall))?;
        let (class, rest) = rest
            .split_first_chunk_mut::<4>()
            .ok_or_else(|| ProgramError::value(Field::Buffer, Reason::TooSmall))?;
        let written = self.reason.encode_into(rest)?;
        *program = self.program.bytes();
        *class = self.class.code().to_be_bytes();
        Ok(FAILURE_PROGRAM_BYTES + 4 + written)
    }
}

#[cfg(test)]
mod candidate_refusal_tests {
    use super::{ProgramFailure, ProgramRefusal, RefusalClass, RefusalReason};

    #[test]
    fn borrowed_reason_accepts_exact_bound_and_rejects_one_past() {
        let maximum = std::vec![0xa5; crate::MAX_REFUSAL_REASON_BYTES];
        assert_eq!(
            RefusalReason::new(&maximum)
                .unwrap_or_else(|error| panic!("maximum: {error}"))
                .bytes(),
            maximum
        );
        assert!(RefusalReason::new(&std::vec![0; crate::MAX_REFUSAL_REASON_BYTES + 1]).is_err());
    }

    #[test]
    fn guest_cannot_publish_host_only_classes_and_decode_is_strict() {
        let empty = RefusalReason::new(&[]).unwrap_or_else(|error| panic!("empty: {error}"));
        assert!(ProgramRefusal::new(RefusalClass::RuntimeFault, empty).is_err());
        assert!(ProgramRefusal::new(RefusalClass::Legacy, empty).is_err());
        let encoded = [0, 0, 0, 2, 0, 0, 0, 2, 0, 0xff];
        let decoded = ProgramRefusal::decode(&encoded)
            .unwrap_or_else(|error| panic!("binary decode: {error}"));
        assert_eq!(decoded.class(), RefusalClass::InvalidInput);
        assert_eq!(decoded.reason().bytes(), [0, 0xff]);
        assert!(ProgramRefusal::decode(&[0, 0, 0, 99, 0, 0, 0, 0]).is_err());
        assert!(ProgramRefusal::decode(&[0, 0, 0, 2, 0, 0, 0, 1]).is_err());
        assert!(ProgramRefusal::decode(&[0, 0, 0, 2, 0, 0, 0, 0, 7]).is_err());
    }

    #[test]
    fn reason_and_refusal_encodings_round_trip_and_refuse_short_buffers() {
        let maximum = std::vec![0x5a; crate::MAX_REFUSAL_REASON_BYTES];
        for bytes in [&[][..], &[0, 0xff, 0x80][..], &maximum[..]] {
            let reason =
                RefusalReason::new(bytes).unwrap_or_else(|error| panic!("reason: {error}"));
            let mut encoded = std::vec![0xee; reason.encoded_len()];
            assert_eq!(
                reason
                    .encode_into(&mut encoded)
                    .unwrap_or_else(|error| panic!("encode: {error}")),
                bytes.len() + 4
            );
            assert_eq!(
                RefusalReason::decode(&encoded)
                    .unwrap_or_else(|error| panic!("decode: {error}"))
                    .bytes(),
                bytes
            );
            assert!(reason
                .encode_into(&mut std::vec![0; reason.encoded_len() - 1])
                .is_err());
            let refusal = ProgramRefusal::new(RefusalClass::Unauthorized, reason)
                .unwrap_or_else(|error| panic!("refusal: {error}"));
            let mut encoded = std::vec![0; refusal.encoded_len()];
            assert_eq!(
                refusal
                    .encode_into(&mut encoded)
                    .unwrap_or_else(|error| panic!("encode refusal: {error}")),
                bytes.len() + 8
            );
            assert_eq!(
                ProgramRefusal::decode(&encoded)
                    .unwrap_or_else(|error| panic!("decode refusal: {error}")),
                refusal
            );
            assert!(refusal
                .encode_into(&mut std::vec![0; refusal.encoded_len() - 1])
                .is_err());
        }
    }

    #[test]
    fn program_failure_names_the_refusing_program_and_decodes_strictly() {
        let program =
            crate::ProgramId::new([9; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let reason = RefusalReason::new(&[0x11, 0, 0xff]).unwrap_or_else(|error| panic!("{error}"));
        let failure = ProgramFailure::new(program, RefusalClass::Conflict, reason)
            .unwrap_or_else(|error| panic!("failure: {error}"));
        let mut encoded = std::vec![0; failure.encoded_len()];
        assert_eq!(
            failure
                .encode_into(&mut encoded)
                .unwrap_or_else(|error| panic!("encode: {error}")),
            43
        );
        let mut expected = std::vec![9; 32];
        expected.extend_from_slice(&[0, 0, 0, 4, 0, 0, 0, 3, 0x11, 0, 0xff]);
        assert_eq!(encoded, expected);
        let decoded =
            ProgramFailure::decode(&encoded).unwrap_or_else(|error| panic!("decode: {error}"));
        assert_eq!(decoded, failure);
        assert_eq!(decoded.program(), program);
        assert_eq!(decoded.class(), RefusalClass::Conflict);
        assert_eq!(decoded.reason().bytes(), [0x11, 0, 0xff]);
        assert!(failure.encode_into(&mut std::vec![0; 42]).is_err());

        for host_only in [RefusalClass::RuntimeFault, RefusalClass::Legacy] {
            let empty = RefusalReason::new(&[]).unwrap_or_else(|error| panic!("{error}"));
            let fault = ProgramFailure::new(program, host_only, empty)
                .unwrap_or_else(|error| panic!("host failure: {error}"));
            let mut bytes = std::vec![0; fault.encoded_len()];
            fault
                .encode_into(&mut bytes)
                .unwrap_or_else(|error| panic!("encode host failure: {error}"));
            assert_eq!(
                ProgramFailure::decode(&bytes).unwrap_or_else(|error| panic!("{error}")),
                fault
            );
            assert!(ProgramFailure::new(program, host_only, reason).is_err());
            let mut forged = expected.clone();
            forged[32..36].copy_from_slice(&host_only.code().to_be_bytes());
            assert!(ProgramFailure::decode(&forged).is_err());
        }

        let mut zero_program = expected.clone();
        zero_program[..32].fill(0);
        assert!(ProgramFailure::decode(&zero_program).is_err());
        let mut unknown_class = expected.clone();
        unknown_class[35] = 6;
        assert!(ProgramFailure::decode(&unknown_class).is_err());
        assert!(ProgramFailure::decode(&expected[..expected.len() - 1]).is_err());
        let mut trailing = expected.clone();
        trailing.push(0);
        assert!(ProgramFailure::decode(&trailing).is_err());
        assert!(ProgramFailure::decode(&expected[..35]).is_err());
        let mut oversized = std::vec![9; 32];
        oversized.extend_from_slice(&[0, 0, 0, 1]);
        oversized.extend_from_slice(
            &u32::try_from(crate::MAX_REFUSAL_REASON_BYTES + 1)
                .unwrap_or_else(|error| panic!("{error}"))
                .to_be_bytes(),
        );
        oversized.extend(std::vec![0; crate::MAX_REFUSAL_REASON_BYTES + 1]);
        assert!(ProgramFailure::decode(&oversized).is_err());
    }
}
