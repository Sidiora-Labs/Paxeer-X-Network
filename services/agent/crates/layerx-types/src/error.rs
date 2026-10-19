//! Disjoint interaction-layer error classes.

use crate::result::ResultCode;

/// A stable class suitable for metrics and exhaustive client handling.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum ErrorClass {
    /// The boundary connection failed.
    TransportFailure,
    /// A caller-supplied deadline elapsed.
    Deadline,
    /// The peer uses an incompatible interface or protocol version.
    ProtocolIncompatibility,
    /// The node does not expose a required capability.
    UnavailableCapability,
    /// The core rejected canonical activity bytes.
    CoreRejection,
    /// Core-produced evidence did not verify.
    VerificationFailure,
    /// Local policy denied the operation.
    PolicyRefusal,
    /// A local capability denied the operation.
    CapabilityRefusal,
    /// A reconciled budget denied the operation.
    BudgetRefusal,
    /// A rate ceiling denied the operation.
    RateLimit,
    /// An invariant inside the interaction layer failed.
    InternalFault,
}

/// The complete error vocabulary. Each source class has its own variant, so a
/// verification failure cannot be represented as a transport failure.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum LayerError {
    /// Boundary transport failed with a stable transport code.
    TransportFailure { code: u32 },
    /// The explicit operation deadline elapsed.
    Deadline,
    /// The peer's major version is incompatible.
    ProtocolIncompatibility { local: u16, peer: u16 },
    /// A required named capability is unavailable.
    UnavailableCapability { capability: String },
    /// The core returned an exact protocol result code.
    CoreRejection { result: ResultCode },
    /// A named verification check failed.
    VerificationFailure { check: String },
    /// A policy rule refused the operation.
    PolicyRefusal { rule: String },
    /// A capability dimension refused the operation.
    CapabilityRefusal { dimension: String },
    /// A budget refused the operation.
    BudgetRefusal { budget: String },
    /// A stable rate bucket refused the operation.
    RateLimit { bucket: String },
    /// An internal invariant failed.
    InternalFault { invariant: String },
}

impl LayerError {
    /// Returns the stable, lossless class of this error.
    #[must_use]
    pub const fn class(&self) -> ErrorClass {
        match self {
            Self::TransportFailure { .. } => ErrorClass::TransportFailure,
            Self::Deadline => ErrorClass::Deadline,
            Self::ProtocolIncompatibility { .. } => ErrorClass::ProtocolIncompatibility,
            Self::UnavailableCapability { .. } => ErrorClass::UnavailableCapability,
            Self::CoreRejection { .. } => ErrorClass::CoreRejection,
            Self::VerificationFailure { .. } => ErrorClass::VerificationFailure,
            Self::PolicyRefusal { .. } => ErrorClass::PolicyRefusal,
            Self::CapabilityRefusal { .. } => ErrorClass::CapabilityRefusal,
            Self::BudgetRefusal { .. } => ErrorClass::BudgetRefusal,
            Self::RateLimit { .. } => ErrorClass::RateLimit,
            Self::InternalFault { .. } => ErrorClass::InternalFault,
        }
    }
}
