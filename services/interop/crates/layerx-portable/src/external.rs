use std::fmt::{Display, Formatter};

use layerx_interop_gateway::adapter::{AdapterDescriptor, AdapterId, SpecVersion};
use sha2::{Digest as _, Sha256};

const MAX_MEDIA_TYPE_BYTES: usize = 128;
const MAX_EXTERNAL_PRESENTATION_BYTES: usize = 2_097_152;
const EVIDENCE_DIGEST_DOMAIN: &[u8] = b"LayerX/interop/external-evidence/v1\0";

/// The external claim class an adapter verifies.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExternalEvidenceKind {
    Mandate,
    Receipt,
}

impl ExternalEvidenceKind {
    const fn tag(self) -> u8 {
        match self {
            Self::Mandate => 1,
            Self::Receipt => 2,
        }
    }
}

/// Exact untrusted presentation delivered to a version-pinned adapter.
///
/// The bytes are borrowed and are never retained in the verified token. This
/// matters for mandates that contain payment-instrument references or other
/// secret-class material.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ExternalPresentation<'a> {
    adapter: &'a str,
    protocol: &'a str,
    spec_version: &'a str,
    kind: ExternalEvidenceKind,
    media_type: &'a str,
    payload: &'a [u8],
}

impl<'a> ExternalPresentation<'a> {
    /// Creates a bounded external presentation. This validates only the
    /// transport envelope; all cryptographic, schema, time, delegation and
    /// constraint checks remain the named adapter's responsibility.
    ///
    /// # Errors
    ///
    /// Refuses invalid identifiers, unpinned versions, media types containing
    /// control bytes, and empty or oversize payloads.
    pub fn new(
        adapter: &'a str,
        protocol: &'a str,
        spec_version: &'a str,
        kind: ExternalEvidenceKind,
        media_type: &'a str,
        payload: &'a [u8],
    ) -> Result<Self, ExternalPresentationError> {
        AdapterId::new(adapter).map_err(|_| ExternalPresentationError::InvalidAdapter)?;
        AdapterId::new(protocol).map_err(|_| ExternalPresentationError::InvalidProtocol)?;
        SpecVersion::parse(spec_version).map_err(|_| ExternalPresentationError::UnpinnedVersion)?;
        if media_type.is_empty()
            || media_type.len() > MAX_MEDIA_TYPE_BYTES
            || media_type.bytes().any(|byte| byte.is_ascii_control())
        {
            return Err(ExternalPresentationError::InvalidMediaType);
        }
        if payload.is_empty() || payload.len() > MAX_EXTERNAL_PRESENTATION_BYTES {
            return Err(ExternalPresentationError::PayloadBounds);
        }
        Ok(Self {
            adapter,
            protocol,
            spec_version,
            kind,
            media_type,
            payload,
        })
    }

    #[must_use]
    pub const fn adapter(&self) -> &str {
        self.adapter
    }

    #[must_use]
    pub const fn protocol(&self) -> &str {
        self.protocol
    }

    #[must_use]
    pub const fn spec_version(&self) -> &str {
        self.spec_version
    }

    #[must_use]
    pub const fn kind(&self) -> ExternalEvidenceKind {
        self.kind
    }

    #[must_use]
    pub const fn media_type(&self) -> &str {
        self.media_type
    }

    #[must_use]
    pub const fn payload(&self) -> &[u8] {
        self.payload
    }
}

/// Adapter implementation boundary for external mandates and receipts.
///
/// The associated context and verified types keep protocol-specific inputs
/// and outputs typed. AP2 can therefore return `VerifiedMandates`, for
/// example, without erasing it into generic JSON or trusting claims assembled
/// by this crate.
pub trait ExternalEvidenceVerifier<C: ?Sized> {
    type Verified;
    type Error;

    /// Returns the adapter's versioned upstream pin and conformance identity.
    fn descriptor(&self) -> &AdapterDescriptor;

    /// Returns whether this verifier accepts mandates or receipts.
    fn evidence_kind(&self) -> ExternalEvidenceKind;

    /// Returns the exact media type this verifier accepts.
    fn media_type(&self) -> &str;

    /// Performs the protocol-specific verification over the exact input
    /// bytes. The implementation must return a typed value only after every
    /// required signature, binding, time and constraint check passes.
    ///
    /// # Errors
    ///
    /// Returns the adapter's own typed verification refusal.
    fn verify(&self, payload: &[u8], context: &C) -> Result<Self::Verified, Self::Error>;
}

/// Runs a version-pinned adapter and binds its typed output to the exact bytes
/// and specification that it verified.
///
/// # Errors
///
/// Refuses any adapter, protocol, version, claim-class or media-type mismatch
/// before invoking adapter code, then preserves the adapter's typed error.
pub fn verify_external_evidence<C: ?Sized, V: ExternalEvidenceVerifier<C>>(
    verifier: &V,
    presentation: &ExternalPresentation<'_>,
    context: &C,
) -> Result<VerifiedExternalEvidence<V::Verified>, ExternalVerificationError<V::Error>> {
    let descriptor = verifier.descriptor();
    if descriptor.id().as_str() != presentation.adapter
        || descriptor.spec().protocol().as_str() != presentation.protocol
        || descriptor.spec().version().as_str() != presentation.spec_version
    {
        return Err(ExternalVerificationError::DescriptorMismatch);
    }
    if verifier.evidence_kind() != presentation.kind {
        return Err(ExternalVerificationError::EvidenceKindMismatch);
    }
    if verifier.media_type() != presentation.media_type {
        return Err(ExternalVerificationError::MediaTypeMismatch);
    }
    let verified_value = verifier
        .verify(presentation.payload, context)
        .map_err(ExternalVerificationError::Adapter)?;
    let document_digest = descriptor.spec().document_digest();
    let suite = descriptor.conformance();
    let evidence_digest = evidence_digest(presentation, descriptor);
    Ok(VerifiedExternalEvidence {
        adapter: presentation.adapter.to_owned(),
        protocol: presentation.protocol.to_owned(),
        spec_version: presentation.spec_version.to_owned(),
        spec_document_digest: document_digest,
        conformance_suite: suite.suite().as_str().to_owned(),
        conformance_vector_count: suite.vector_count(),
        conformance_suite_digest: suite.suite_digest(),
        kind: presentation.kind,
        media_type: presentation.media_type.to_owned(),
        evidence_digest,
        verified: verified_value,
    })
}

/// A protocol-specific verified value bound to the exact external bytes and
/// upstream specification that produced it. Fields are private so callers
/// cannot manufacture verification status from untrusted claims.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedExternalEvidence<T> {
    adapter: String,
    protocol: String,
    spec_version: String,
    spec_document_digest: [u8; 32],
    conformance_suite: String,
    conformance_vector_count: u64,
    conformance_suite_digest: [u8; 32],
    kind: ExternalEvidenceKind,
    media_type: String,
    evidence_digest: [u8; 32],
    verified: T,
}

impl<T> VerifiedExternalEvidence<T> {
    #[must_use]
    pub fn adapter(&self) -> &str {
        &self.adapter
    }

    #[must_use]
    pub fn protocol(&self) -> &str {
        &self.protocol
    }

    #[must_use]
    pub fn spec_version(&self) -> &str {
        &self.spec_version
    }

    #[must_use]
    pub const fn spec_document_digest(&self) -> [u8; 32] {
        self.spec_document_digest
    }

    #[must_use]
    pub fn conformance_suite(&self) -> &str {
        &self.conformance_suite
    }

    #[must_use]
    pub const fn conformance_vector_count(&self) -> u64 {
        self.conformance_vector_count
    }

    #[must_use]
    pub const fn conformance_suite_digest(&self) -> [u8; 32] {
        self.conformance_suite_digest
    }

    #[must_use]
    pub const fn kind(&self) -> ExternalEvidenceKind {
        self.kind
    }

    #[must_use]
    pub fn media_type(&self) -> &str {
        &self.media_type
    }

    /// Returns a domain-separated digest of the adapter, protocol, exact spec
    /// pin, conformance suite, claim class, media type and exact untrusted
    /// presentation bytes.
    #[must_use]
    pub const fn evidence_digest(&self) -> [u8; 32] {
        self.evidence_digest
    }

    /// Borrows the adapter's protocol-specific verified value.
    #[must_use]
    pub const fn verified(&self) -> &T {
        &self.verified
    }

    /// Consumes the binding and returns the adapter's typed verified value.
    #[must_use]
    pub fn into_verified(self) -> T {
        self.verified
    }
}

/// External transport-envelope refusal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExternalPresentationError {
    InvalidAdapter,
    InvalidProtocol,
    UnpinnedVersion,
    InvalidMediaType,
    PayloadBounds,
}

impl Display for ExternalPresentationError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidAdapter => formatter.write_str("invalid external adapter identifier"),
            Self::InvalidProtocol => formatter.write_str("invalid external protocol identifier"),
            Self::UnpinnedVersion => {
                formatter.write_str("external protocol version is not exactly pinned")
            }
            Self::InvalidMediaType => formatter.write_str("invalid external evidence media type"),
            Self::PayloadBounds => {
                formatter.write_str("external evidence payload is outside its bounds")
            }
        }
    }
}

impl std::error::Error for ExternalPresentationError {}

/// Failure to bind a presentation to and verify it through the named adapter.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExternalVerificationError<E> {
    DescriptorMismatch,
    EvidenceKindMismatch,
    MediaTypeMismatch,
    Adapter(E),
}

impl<E: Display> Display for ExternalVerificationError<E> {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::DescriptorMismatch => {
                formatter.write_str("external evidence does not match the pinned adapter")
            }
            Self::EvidenceKindMismatch => {
                formatter.write_str("external evidence class does not match the verifier")
            }
            Self::MediaTypeMismatch => {
                formatter.write_str("external evidence media type does not match the verifier")
            }
            Self::Adapter(error) => write!(formatter, "external evidence refused: {error}"),
        }
    }
}

impl<E: std::error::Error + 'static> std::error::Error for ExternalVerificationError<E> {}

fn evidence_digest(
    presentation: &ExternalPresentation<'_>,
    descriptor: &AdapterDescriptor,
) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(EVIDENCE_DIGEST_DOMAIN);
    hash.update([presentation.kind.tag()]);
    hash_part(&mut hash, presentation.adapter.as_bytes());
    hash_part(&mut hash, presentation.protocol.as_bytes());
    hash_part(&mut hash, presentation.spec_version.as_bytes());
    hash.update(descriptor.spec().document_digest());
    hash_part(
        &mut hash,
        descriptor.conformance().suite().as_str().as_bytes(),
    );
    hash.update(descriptor.conformance().vector_count().to_be_bytes());
    hash.update(descriptor.conformance().suite_digest());
    hash_part(&mut hash, presentation.media_type.as_bytes());
    hash_part(&mut hash, presentation.payload);
    hash.finalize().into()
}

fn hash_part(hash: &mut Sha256, value: &[u8]) {
    hash.update(u64::try_from(value.len()).unwrap_or(u64::MAX).to_be_bytes());
    hash.update(value);
}
