#![forbid(unsafe_code)]

//! Portable, authority-free evidence boundaries for interoperability adapters.
//!
//! A [`PortableReceipt`] carries exact receipt bytes and batch claims that are
//! checked against an independently trusted batch authorization, allowing a
//! `LayerX` receipt to be verified without a node, gateway, daemon, database,
//! clock, or network connection. External evidence travels
//! in the opposite direction through [`verify_external_evidence`]: the
//! adapter keeps ownership of protocol-specific cryptography and constraints,
//! while this crate binds its typed result to the exact presentation and
//! pinned upstream specification that were verified.

mod external;
mod receipt;

pub use external::{
    verify_external_evidence, ExternalEvidenceKind, ExternalEvidenceVerifier, ExternalPresentation,
    ExternalPresentationError, ExternalVerificationError, VerifiedExternalEvidence,
};
pub use receipt::{
    verify_portable_json, PortableReceipt, PortableReceiptError, PortableReceiptV2,
    PortableVerified, PortableVerifiedReceipt, PortableVerifiedReceiptV2, ReceiptKeyEntry,
    PORTABLE_RECEIPT_FORMAT, PORTABLE_RECEIPT_FORMAT_V2,
};

/// Codify anchor for portable receipt export and typed external verification.
#[must_use]
pub const fn interop_portable_verification() -> &'static str {
    "layerx-receipt-proof-v1-and-pinned-external-evidence-v1"
}
