use std::fmt::{Display, Formatter};

use layerx_interop_gateway::error::GatewayError;

/// Typed AP2 verification, mapping and evidence failures. No variant carries
/// mandate, payment-instrument or receipt bytes into logs or traces.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Ap2Error {
    Bounds,
    Malformed(&'static str),
    UnsupportedAlgorithm,
    UnsupportedMandateVersion,
    UnsupportedDelegation,
    KeyResolution,
    InvalidSignature,
    InvalidDisclosure,
    InvalidKeyBinding,
    AudienceMismatch,
    NonceMismatch,
    NotYetValid,
    Expired,
    CheckoutBindingMismatch,
    PaymentBindingMismatch,
    ConstraintMissing(&'static str),
    ConstraintUnsupported,
    ConstraintViolated(&'static str),
    UsageEvidenceRequired,
    AmountConversion,
    IntentCompilation,
    IntentMismatch,
    PlaneRefused,
    EvidenceMissing,
    EvidenceMismatch,
    ReceiptSigning,
    Gateway(GatewayError),
}

impl Display for Ap2Error {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Bounds => formatter.write_str("AP2 input exceeds a declared bound"),
            Self::Malformed(field) => write!(formatter, "malformed AP2 {field}"),
            Self::UnsupportedAlgorithm => {
                formatter.write_str("AP2 signing or digest algorithm is unsupported")
            }
            Self::UnsupportedMandateVersion => {
                formatter.write_str("AP2 mandate version is unsupported")
            }
            Self::UnsupportedDelegation => {
                formatter.write_str("AP2 delegation shape is unsupported")
            }
            Self::KeyResolution => formatter.write_str("AP2 signing key is not trusted"),
            Self::InvalidSignature => formatter.write_str("AP2 signature verification failed"),
            Self::InvalidDisclosure => formatter.write_str("AP2 selective disclosure is invalid"),
            Self::InvalidKeyBinding => formatter.write_str("AP2 mandate key binding is invalid"),
            Self::AudienceMismatch => formatter.write_str("AP2 audience does not match"),
            Self::NonceMismatch => formatter.write_str("AP2 nonce does not match"),
            Self::NotYetValid => formatter.write_str("AP2 mandate is not yet valid"),
            Self::Expired => formatter.write_str("AP2 mandate has expired"),
            Self::CheckoutBindingMismatch => {
                formatter.write_str("AP2 checkout is not bound to the signed mandate")
            }
            Self::PaymentBindingMismatch => {
                formatter.write_str("AP2 payment is not bound to the checkout")
            }
            Self::ConstraintMissing(name) => write!(formatter, "AP2 constraint missing: {name}"),
            Self::ConstraintUnsupported => {
                formatter.write_str("AP2 constraint cannot be honoured by this activity")
            }
            Self::ConstraintViolated(name) => {
                write!(formatter, "AP2 constraint violated: {name}")
            }
            Self::UsageEvidenceRequired => {
                formatter.write_str("AP2 recurrence usage evidence is required")
            }
            Self::AmountConversion => formatter.write_str("AP2 amount cannot map exactly"),
            Self::IntentCompilation => {
                formatter.write_str("LayerX typed intent compilation failed")
            }
            Self::IntentMismatch => {
                formatter.write_str("compiled LayerX intent cannot honour the AP2 mandate")
            }
            Self::PlaneRefused => formatter.write_str("LayerX payment plane refused the mandate"),
            Self::EvidenceMissing => formatter.write_str("LayerX evidence is not available"),
            Self::EvidenceMismatch => {
                formatter.write_str("LayerX evidence does not match the AP2 mandate")
            }
            Self::ReceiptSigning => formatter.write_str("AP2 receipt signing failed"),
            Self::Gateway(error) => write!(formatter, "gateway refused AP2 translation: {error}"),
        }
    }
}

impl std::error::Error for Ap2Error {}

impl From<GatewayError> for Ap2Error {
    fn from(value: GatewayError) -> Self {
        Self::Gateway(value)
    }
}
