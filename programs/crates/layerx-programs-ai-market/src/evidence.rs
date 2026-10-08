//! F09 offchain artifact manifest, publisher envelope, declaration, reproduction,
//! chunk-proof and evidence-manifest codecs with domain-separated SHA256 roots.
//! Pure codecs: no publication, grant admission, availability, protocol state or
//! finality is established by any value here.
use crate::{
    codec::{domain_hash, Reader, Writer},
    commit_reveal::commitment::{decode_binding, encode_binding, BINDING_BYTES},
    errors::{offchain_error, ApplicationError, OffchainSpace},
    evaluators::{codec::verify_digest, model::VerificationError},
    types::*,
    MAX_TASKS, MAX_WORKERS,
};
use sha2::{Digest, Sha256};

pub const MAGIC: &[u8; 8] = b"PAXAIF09";
pub const VERSION: u16 = 1;
pub const SIGNATURE_SCHEME_ED25519: u16 = 1;
pub const CHUNK_BYTES: u32 = 262_144;
pub const MAX_CHUNKS: u32 = 131_072;
pub const MAX_OBJECT_BYTES: u64 = 34_359_738_368;
pub const MAX_PARENTS: usize = 16;
pub const MAX_MANIFEST_BYTES: usize = 4_096;
pub const MAX_ENVELOPE_BYTES: usize = 8_192;
pub const MAX_RECORD_BYTES: usize = 16_384;
pub const MAX_DOCUMENTS: usize = 16;
pub const MAX_LABEL_BYTES: usize = 256;
pub const MAX_PROOF_SIBLINGS: usize = 17;
pub const MAX_EVIDENCE_BYTES: usize = 65_536;
pub const MANIFEST_FIXED_BYTES: usize = 386;
pub const PARENT_BYTES: usize = 33;
pub const ENVELOPE_FIXED_BYTES: usize = 110;
pub const DECLARATION_FIXED_BYTES: usize = 97;
pub const DOCUMENT_FIXED_BYTES: usize = 37;
pub const REPRODUCTION_BYTES: usize = 379;
pub const PROOF_FIXED_BYTES: usize = 43;
pub const EVIDENCE_FIXED_BYTES: usize = 373;
pub const WORKER_GROUP_FIXED_BYTES: usize = 144;
pub const TASK_ENTRY_BYTES: usize = 161;

pub const MANIFEST_DOMAIN: &[u8] = b"PAXAI/artifact-manifest/v1\0";
pub const PUBLISHER_DOMAIN: &[u8] = b"PAXAI/artifact-publisher/v1\0";
pub const DECLARATION_DOMAIN: &[u8] = b"PAXAI/artifact-declaration/v1\0";
pub const REPRODUCTION_DOMAIN: &[u8] = b"PAXAI/artifact-reproduction/v1\0";
pub const CHUNK_DOMAIN: &[u8] = b"PAXAI/artifact-chunk/v1\0";
pub const EMPTY_DOMAIN: &[u8] = b"PAXAI/artifact-empty/v1\0";
pub const NODE_DOMAIN: &[u8] = b"PAXAI/artifact-node/v1\0";
pub const CONTENT_DOMAIN: &[u8] = b"PAXAI/artifact-content/v1\0";
pub const EVIDENCE_DOMAIN: &str = "PAXAI/evidence/v1";

/// Typed failures of the independent offchain artifact error space; the
/// discriminant is the 1-based code of `errors::ARTIFACT_SERVICE_ERRORS`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum ArtifactError {
    Malformed = 1,
    UnsupportedVersion,
    UnsupportedKind,
    InvalidContext,
    Unauthorized,
    AuthorityRevoked,
    PurposeDenied,
    Expired,
    QuotaExceeded,
    CapacityUnavailable,
    MissingChunk,
    LengthMismatch,
    RootMismatch,
    SignatureInvalid,
    IdempotencyConflict,
    IntegrityConflict,
    Tombstoned,
    ContentUnavailable,
    UnsafeLocator,
    UnknownDelivery,
    StorageFailure,
}
impl ArtifactError {
    pub const fn code(self) -> u16 {
        self as u16
    }
    pub fn name(self) -> &'static str {
        offchain_error(OffchainSpace::ArtifactService, self.code()).unwrap_or("Malformed")
    }
}
/// Common reader/writer/identity refusals (noncanonical, bound, overflow,
/// zero identity) are malformed artifact bytes.
impl From<ApplicationError> for ArtifactError {
    fn from(_: ApplicationError) -> Self {
        Self::Malformed
    }
}
pub type ArtifactResult<T> = Result<T, ArtifactError>;

/// Host verification failures stay distinct from typed signature refusal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum VerificationFailure {
    Artifact(ArtifactError),
    Host(VerificationError),
}
impl From<ArtifactError> for VerificationFailure {
    fn from(error: ArtifactError) -> Self {
        Self::Artifact(error)
    }
}

macro_rules! u8_enum {
    ($name:ident, $unknown:expr, { $($variant:ident = $value:literal),+ $(,)? }) => {
        #[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
        #[repr(u8)]
        pub enum $name { $($variant = $value),+ }
        impl $name {
            pub fn decode(value: u8) -> ArtifactResult<Self> {
                match value {
                    $($value => Ok(Self::$variant),)+
                    _ => Err($unknown),
                }
            }
        }
    };
}
u8_enum!(ArtifactKind, ArtifactError::UnsupportedKind, {
    Model = 1, Dataset = 2, Benchmark = 3, Input = 4, Result = 5, ExecutionEvidence = 6,
    Reproduction = 7, Declaration = 8, EvaluationEvidence = 9,
});
u8_enum!(Privacy, ArtifactError::Malformed, { Public = 0, Encrypted = 1 });
u8_enum!(ParentPurpose, ArtifactError::Malformed, {
    ModelShard = 1, Input = 2, Result = 3, Model = 4, Dataset = 5, Benchmark = 6,
    Execution = 7, Reproduction = 8, ScoreEvidence = 9, Declaration = 10,
});
u8_enum!(RightsStatus, ArtifactError::Malformed, { Undeclared = 0, Declared = 1, Documented = 2 });
u8_enum!(DocumentRole, ArtifactError::Malformed, {
    License = 1, Consent = 2, Acquisition = 3, Restriction = 4,
});
u8_enum!(ReproductionStatus, ArtifactError::Malformed, {
    NotAttempted = 0, Reproduced = 1, Diverged = 2, Unavailable = 3,
});
u8_enum!(AssessmentMode, ArtifactError::Malformed, { Objective = 1, Subjective = 2 });
u8_enum!(TerminalStatus, ArtifactError::Malformed, {
    Success = 1, Refused = 2, Timeout = 3, Unknown = 4, Cancelled = 5,
});

/// Bounded list in the crate's typed-or-encoded view style. Encoded views are
/// produced only by strict decoders after full validation.
pub trait Item<'a>: Copy {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self>;
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()>;
}
#[derive(Clone, Copy, Debug)]
pub enum Items<'a, T> {
    Typed(&'a [T]),
    Encoded { count: usize, bytes: &'a [u8] },
}
impl<'a, T: Item<'a>> Items<'a, T> {
    pub fn len(&self) -> usize {
        match self {
            Self::Typed(items) => items.len(),
            Self::Encoded { count, .. } => *count,
        }
    }
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
    pub fn iter(&self) -> ItemIter<'a, T> {
        let bytes: &'a [u8] = match self {
            Self::Typed(_) => &[],
            Self::Encoded { bytes, .. } => bytes,
        };
        ItemIter {
            items: *self,
            index: 0,
            reader: Reader::new(bytes),
        }
    }
}
pub struct ItemIter<'a, T> {
    items: Items<'a, T>,
    index: usize,
    reader: Reader<'a>,
}
impl<'a, T: Item<'a>> Iterator for ItemIter<'a, T> {
    type Item = ArtifactResult<T>;
    fn next(&mut self) -> Option<Self::Item> {
        let item = match self.items {
            Items::Typed(items) => items.get(self.index).copied().map(Ok),
            Items::Encoded { count, .. } if self.index < count => Some(T::read(&mut self.reader)),
            Items::Encoded { .. } => None,
        };
        self.index += 1;
        item
    }
}
impl<'a> Item<'a> for [u8; 32] {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self> {
        Ok(r.fixed()?)
    }
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()> {
        Ok(w.put(self)?)
    }
}

fn h(domain: &[u8], parts: &[&[u8]]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(domain);
    for part in parts {
        hasher.update(part);
    }
    hasher.finalize().into()
}
fn nonzero(root: &[u8; 32]) -> ArtifactResult<()> {
    if *root == [0; 32] {
        Err(ArtifactError::Malformed)
    } else {
        Ok(())
    }
}
fn version(r: &mut Reader<'_>) -> ArtifactResult<()> {
    if r.u16()? == VERSION {
        Ok(())
    } else {
        Err(ArtifactError::UnsupportedVersion)
    }
}
fn length_u32(length: usize) -> ArtifactResult<[u8; 4]> {
    Ok(u32::try_from(length)
        .map_err(|_| ArtifactError::Malformed)?
        .to_be_bytes())
}

/// Common context C = chain_domain32||program32||market32||policy32.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ArtifactContext {
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub policy: PolicyDigest,
}
impl ArtifactContext {
    pub fn bytes(&self) -> [u8; 128] {
        let mut out = [0; 128];
        out[..32].copy_from_slice(self.chain.as_bytes());
        out[32..64].copy_from_slice(self.program.as_bytes());
        out[64..96].copy_from_slice(self.market.as_bytes());
        out[96..].copy_from_slice(self.policy.as_bytes());
        out
    }
    fn read(r: &mut Reader<'_>) -> ArtifactResult<Self> {
        Ok(Self {
            chain: ChainDomain::new(r.fixed()?)?,
            program: ProgramId::new(r.fixed()?)?,
            market: MarketId::new(r.fixed()?)?,
            policy: PolicyDigest::new(r.fixed()?)?,
        })
    }
}

/// Unsigned-manifest root, distinct from the F09 `EvidenceRoot`.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd)]
pub struct ArtifactManifestRoot(Digest32);
impl ArtifactManifestRoot {
    pub fn new(bytes: [u8; 32]) -> ArtifactResult<Self> {
        Ok(Self(Digest32::new(bytes)?))
    }
    pub const fn bytes(self) -> [u8; 32] {
        self.0.bytes()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ParentRef {
    pub purpose: ParentPurpose,
    pub root: [u8; 32],
}
impl<'a> Item<'a> for ParentRef {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self> {
        Ok(Self {
            purpose: ParentPurpose::decode(r.u8()?)?,
            root: r.fixed()?,
        })
    }
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()> {
        w.u8(self.purpose as u8)?;
        Ok(w.put(&self.root)?)
    }
}

#[derive(Clone, Copy, Debug)]
pub struct ArtifactManifest<'a> {
    pub kind: ArtifactKind,
    pub privacy: Privacy,
    pub context: ArtifactContext,
    pub epoch: u64,
    pub publisher: PrincipalId,
    pub subject: [u8; 32],
    pub byte_length: u64,
    pub chunk_count: u32,
    pub content_root: [u8; 32],
    pub parents: Items<'a, ParentRef>,
    pub declaration_root: [u8; 32],
    pub reproduction_root: [u8; 32],
    pub access_policy_root: [u8; 32],
    pub not_after_height: u64,
}
impl ArtifactManifest<'_> {
    const fn epoch_independent(&self) -> bool {
        matches!(
            self.kind,
            ArtifactKind::Model | ArtifactKind::Dataset | ArtifactKind::Benchmark
        )
    }
    const fn task_bound(&self) -> bool {
        matches!(
            self.kind,
            ArtifactKind::Input | ArtifactKind::Result | ArtifactKind::ExecutionEvidence
        )
    }
    pub fn encoded_len(&self) -> ArtifactResult<usize> {
        self.parents
            .len()
            .checked_mul(PARENT_BYTES)
            .and_then(|n| n.checked_add(MANIFEST_FIXED_BYTES))
            .ok_or(ArtifactError::Malformed)
    }
    pub fn validate(&self) -> ArtifactResult<()> {
        if self.byte_length > MAX_OBJECT_BYTES || self.chunk_count > MAX_CHUNKS {
            return Err(ArtifactError::Malformed);
        }
        if chunk_count(self.byte_length)? != self.chunk_count {
            return Err(ArtifactError::LengthMismatch);
        }
        nonzero(&self.content_root)?;
        if self.byte_length == 0 {
            if self.privacy == Privacy::Encrypted {
                return Err(ArtifactError::Malformed);
            }
            if self.content_root != content_root(0, 0, &empty_tree_root()) {
                return Err(ArtifactError::RootMismatch);
            }
        }
        if self.privacy == Privacy::Encrypted {
            nonzero(&self.access_policy_root)?;
        }
        if self.parents.len() > MAX_PARENTS {
            return Err(ArtifactError::Malformed);
        }
        let mut previous: Option<(u8, [u8; 32])> = None;
        for parent in self.parents.iter() {
            let parent = parent?;
            if !matches!(
                parent.purpose,
                ParentPurpose::Reproduction | ParentPurpose::Declaration
            ) {
                nonzero(&parent.root)?;
            }
            let key = (parent.purpose as u8, parent.root);
            if previous.is_some_and(|p| p >= key) {
                return Err(ArtifactError::Malformed);
            }
            previous = Some(key);
        }
        nonzero(&self.subject)?;
        if self.epoch_independent()
            && (self.epoch != 0 || self.subject != self.context.policy.bytes())
        {
            return Err(ArtifactError::InvalidContext);
        }
        Ok(())
    }
}

pub fn encode_manifest(value: &ArtifactManifest<'_>, out: &mut [u8]) -> ArtifactResult<usize> {
    value.validate()?;
    let size = value.encoded_len()?;
    if size > MAX_MANIFEST_BYTES || out.len() < size {
        return Err(ArtifactError::Malformed);
    }
    let mut w = Writer::new(out);
    w.put(MAGIC)?;
    w.u16(VERSION)?;
    w.u8(value.kind as u8)?;
    w.u8(value.privacy as u8)?;
    w.u32(0)?;
    w.put(&value.context.bytes())?;
    w.u64(value.epoch)?;
    w.put(value.publisher.as_bytes())?;
    w.put(&value.subject)?;
    w.u64(value.byte_length)?;
    w.u32(CHUNK_BYTES)?;
    w.u32(value.chunk_count)?;
    w.put(&value.content_root)?;
    w.u16(u16::try_from(value.parents.len()).map_err(|_| ArtifactError::Malformed)?)?;
    for parent in value.parents.iter() {
        parent?.write(&mut w)?;
    }
    w.put(&value.declaration_root)?;
    w.put(&value.reproduction_root)?;
    w.put(&value.access_policy_root)?;
    w.u64(value.not_after_height)?;
    w.put(&[0; 16])?;
    Ok(w.len())
}

pub fn decode_manifest(input: &[u8]) -> ArtifactResult<ArtifactManifest<'_>> {
    if input.len() > MAX_MANIFEST_BYTES || input.len() < MANIFEST_FIXED_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let mut r = Reader::new(input);
    if r.fixed::<8>()? != *MAGIC {
        return Err(ArtifactError::Malformed);
    }
    version(&mut r)?;
    let kind = ArtifactKind::decode(r.u8()?)?;
    let privacy = Privacy::decode(r.u8()?)?;
    r.reserved(4)?;
    let context = ArtifactContext::read(&mut r)?;
    let epoch = r.u64()?;
    let publisher = PrincipalId::new(r.fixed()?)?;
    let subject = r.fixed()?;
    let byte_length = r.u64()?;
    if r.u32()? != CHUNK_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let chunk_count = r.u32()?;
    let content_root = r.fixed()?;
    let count = usize::from(r.u16()?);
    if count > MAX_PARENTS {
        return Err(ArtifactError::Malformed);
    }
    let parents = Items::Encoded {
        count,
        bytes: r.take(count * PARENT_BYTES)?,
    };
    let manifest = ArtifactManifest {
        kind,
        privacy,
        context,
        epoch,
        publisher,
        subject,
        byte_length,
        chunk_count,
        content_root,
        parents,
        declaration_root: r.fixed()?,
        reproduction_root: r.fixed()?,
        access_policy_root: r.fixed()?,
        not_after_height: r.u64()?,
    };
    r.reserved(16)?;
    r.finish()?;
    manifest.validate()?;
    Ok(manifest)
}

/// H(D(manifest)||C||u32(len)||encoded) over strictly decoded canonical bytes.
pub fn manifest_root(encoded: &[u8]) -> ArtifactResult<ArtifactManifestRoot> {
    let manifest = decode_manifest(encoded)?;
    ArtifactManifestRoot::new(h(
        MANIFEST_DOMAIN,
        &[
            &manifest.context.bytes(),
            &length_u32(encoded.len())?,
            encoded,
        ],
    ))
}

/// Supplied subject expectation; a comparison input, never host authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SubjectContext {
    Policy,
    Task { epoch: u64, task: TaskId },
}

pub fn check_manifest_context(
    manifest: &ArtifactManifest<'_>,
    expected: &ArtifactContext,
    subject: SubjectContext,
) -> ArtifactResult<()> {
    if manifest.context != *expected {
        return Err(ArtifactError::InvalidContext);
    }
    let matches = match subject {
        SubjectContext::Policy => {
            !manifest.task_bound()
                && manifest.epoch == 0
                && manifest.subject == expected.policy.bytes()
        }
        SubjectContext::Task { epoch, task } => {
            !manifest.epoch_independent()
                && manifest.epoch == epoch
                && manifest.subject == task.bytes()
        }
    };
    if matches {
        Ok(())
    } else {
        Err(ArtifactError::InvalidContext)
    }
}

pub fn publisher_digest(
    context: &ArtifactContext,
    root: ArtifactManifestRoot,
    generation: Version,
) -> [u8; 32] {
    h(
        PUBLISHER_DOMAIN,
        &[
            &context.bytes(),
            &root.bytes(),
            &generation.get().to_be_bytes(),
        ],
    )
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PublisherEnvelope<'a> {
    pub manifest: &'a [u8],
    pub generation: Version,
    pub key: PublicKey32,
    pub signature: Signature64,
}
/// Signature-verified attribution; not tenant admission or grant authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VerifiedPublisher {
    pub root: ArtifactManifestRoot,
    pub publisher: PrincipalId,
    pub generation: Version,
    pub key: PublicKey32,
}

pub fn encode_envelope(value: &PublisherEnvelope<'_>, out: &mut [u8]) -> ArtifactResult<usize> {
    decode_manifest(value.manifest)?;
    if value.key.0 == [0; 32] {
        return Err(ArtifactError::Malformed);
    }
    let size = ENVELOPE_FIXED_BYTES + value.manifest.len();
    if size > MAX_ENVELOPE_BYTES || out.len() < size {
        return Err(ArtifactError::Malformed);
    }
    let mut w = Writer::new(out);
    w.put(&length_u32(value.manifest.len())?)?;
    w.put(value.manifest)?;
    w.u16(SIGNATURE_SCHEME_ED25519)?;
    w.u64(value.generation.get())?;
    w.put(&value.key.0)?;
    w.put(&value.signature.0)?;
    Ok(w.len())
}

pub fn decode_envelope(input: &[u8]) -> ArtifactResult<PublisherEnvelope<'_>> {
    if input.len() > MAX_ENVELOPE_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let mut r = Reader::new(input);
    let length = usize::try_from(r.u32()?).map_err(|_| ArtifactError::Malformed)?;
    if length > MAX_MANIFEST_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let manifest = r.take(length)?;
    decode_manifest(manifest)?;
    if r.u16()? != SIGNATURE_SCHEME_ED25519 {
        return Err(ArtifactError::Malformed);
    }
    let generation = Version::new(r.u64()?)?;
    let key = PublicKey32(r.fixed()?);
    if key.0 == [0; 32] {
        return Err(ArtifactError::Malformed);
    }
    let signature = Signature64(r.fixed()?);
    r.finish()?;
    Ok(PublisherEnvelope {
        manifest,
        generation,
        key,
        signature,
    })
}

/// Ed25519 over the 32-byte publisher digest only; the manifest root ignores
/// the signature, so distinct valid signatures share one root.
pub fn verify_publisher(
    envelope: &PublisherEnvelope<'_>,
) -> Result<VerifiedPublisher, VerificationFailure> {
    let manifest = decode_manifest(envelope.manifest)?;
    let root = manifest_root(envelope.manifest)?;
    let digest = publisher_digest(&manifest.context, root, envelope.generation);
    verify_digest(envelope.key, envelope.signature, digest).map_err(|error| match error {
        VerificationError::Application(_) => {
            VerificationFailure::Artifact(ArtifactError::SignatureInvalid)
        }
        #[cfg(target_arch = "wasm32")]
        host => VerificationFailure::Host(host),
    })?;
    Ok(VerifiedPublisher {
        root,
        publisher: manifest.publisher,
        generation: envelope.generation,
        key: envelope.key,
    })
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RightsDocument<'a> {
    pub role: DocumentRole,
    pub root: [u8; 32],
    pub label: &'a str,
}
impl<'a> Item<'a> for RightsDocument<'a> {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self> {
        let role = DocumentRole::decode(r.u8()?)?;
        let root = r.fixed()?;
        let length = usize::try_from(r.u32()?).map_err(|_| ArtifactError::Malformed)?;
        if length > MAX_LABEL_BYTES {
            return Err(ArtifactError::Malformed);
        }
        let label = core::str::from_utf8(r.take(length)?).map_err(|_| ArtifactError::Malformed)?;
        Ok(Self { role, root, label })
    }
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()> {
        w.u8(self.role as u8)?;
        w.put(&self.root)?;
        w.put(&length_u32(self.label.len())?)?;
        Ok(w.put(self.label.as_bytes())?)
    }
}

/// A rights claim and evidence reference; DOCUMENTED is never legal clearance.
#[derive(Clone, Copy, Debug)]
pub struct Declaration<'a> {
    pub publisher: PrincipalId,
    pub rights: RightsStatus,
    pub purpose_mask: u16,
    pub restriction_mask: u16,
    pub valid_until_height: u64,
    pub documents: Items<'a, RightsDocument<'a>>,
    pub review_reference_root: [u8; 32],
}
impl Declaration<'_> {
    pub fn validate(&self) -> ArtifactResult<()> {
        if self.purpose_mask & !0x000f != 0 || self.restriction_mask & !0x001f != 0 {
            return Err(ArtifactError::Malformed);
        }
        if self.documents.len() > MAX_DOCUMENTS {
            return Err(ArtifactError::Malformed);
        }
        if self.rights == RightsStatus::Documented && self.documents.is_empty() {
            return Err(ArtifactError::Malformed);
        }
        let mut previous: Option<RightsDocument<'_>> = None;
        for document in self.documents.iter() {
            let document = document?;
            nonzero(&document.root)?;
            if document.label.len() > MAX_LABEL_BYTES {
                return Err(ArtifactError::Malformed);
            }
            let key = (document.role, document.root, document.label.as_bytes());
            if previous.is_some_and(|p| (p.role, p.root, p.label.as_bytes()) >= key) {
                return Err(ArtifactError::Malformed);
            }
            previous = Some(document);
        }
        Ok(())
    }
    pub fn encoded_len(&self) -> ArtifactResult<usize> {
        let mut size = DECLARATION_FIXED_BYTES;
        for document in self.documents.iter() {
            size = size
                .checked_add(DOCUMENT_FIXED_BYTES + document?.label.len())
                .ok_or(ArtifactError::Malformed)?;
        }
        Ok(size)
    }
}
/// A missing declaration stays UNDECLARED; nothing is inferred.
pub fn rights_status(declaration: Option<&Declaration<'_>>) -> RightsStatus {
    declaration.map_or(RightsStatus::Undeclared, |d| d.rights)
}

pub fn encode_declaration(value: &Declaration<'_>, out: &mut [u8]) -> ArtifactResult<usize> {
    value.validate()?;
    let size = value.encoded_len()?;
    if size > MAX_RECORD_BYTES || out.len() < size {
        return Err(ArtifactError::Malformed);
    }
    let mut w = Writer::new(out);
    w.u16(VERSION)?;
    w.put(value.publisher.as_bytes())?;
    w.u8(value.rights as u8)?;
    w.u16(value.purpose_mask)?;
    w.u16(value.restriction_mask)?;
    w.u64(value.valid_until_height)?;
    w.u16(u16::try_from(value.documents.len()).map_err(|_| ArtifactError::Malformed)?)?;
    for document in value.documents.iter() {
        document?.write(&mut w)?;
    }
    w.put(&value.review_reference_root)?;
    w.put(&[0; 16])?;
    Ok(w.len())
}

pub fn decode_declaration(input: &[u8]) -> ArtifactResult<Declaration<'_>> {
    if input.len() > MAX_RECORD_BYTES || input.len() < DECLARATION_FIXED_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let mut r = Reader::new(input);
    version(&mut r)?;
    let publisher = PrincipalId::new(r.fixed()?)?;
    let rights = RightsStatus::decode(r.u8()?)?;
    let purpose_mask = r.u16()?;
    let restriction_mask = r.u16()?;
    let valid_until_height = r.u64()?;
    let count = usize::from(r.u16()?);
    if count > MAX_DOCUMENTS {
        return Err(ArtifactError::Malformed);
    }
    let start = r.offset();
    for _ in 0..count {
        RightsDocument::read(&mut r)?;
    }
    let documents = Items::Encoded {
        count,
        bytes: &input[start..r.offset()],
    };
    let declaration = Declaration {
        publisher,
        rights,
        purpose_mask,
        restriction_mask,
        valid_until_height,
        documents,
        review_reference_root: r.fixed()?,
    };
    r.reserved(16)?;
    r.finish()?;
    declaration.validate()?;
    Ok(declaration)
}

pub fn declaration_root(context: &ArtifactContext, encoded: &[u8]) -> ArtifactResult<Digest32> {
    decode_declaration(encoded)?;
    Ok(Digest32::new(h(
        DECLARATION_DOMAIN,
        &[&context.bytes(), &length_u32(encoded.len())?, encoded],
    ))?)
}

/// Matching roots mean equal bytes, never independent re-execution.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Reproduction {
    pub task: TaskId,
    pub request_root: [u8; 32],
    pub model_root: [u8; 32],
    pub input_root: [u8; 32],
    pub result_root: [u8; 32],
    pub evaluator_program_record_root: [u8; 32],
    pub environment_root: [u8; 32],
    pub method_root: [u8; 32],
    pub seed_root: [u8; 32],
    pub metric_schema_root: [u8; 32],
    pub observed_units: u64,
    pub status: ReproductionStatus,
    pub repeated_result_root: [u8; 32],
}
impl Reproduction {
    pub fn validate(&self) -> ArtifactResult<()> {
        nonzero(&self.request_root)?;
        nonzero(&self.model_root)?;
        nonzero(&self.input_root)?;
        nonzero(&self.result_root)
    }
}

pub fn encode_reproduction(value: &Reproduction) -> ArtifactResult<[u8; REPRODUCTION_BYTES]> {
    value.validate()?;
    let mut out = [0; REPRODUCTION_BYTES];
    let mut w = Writer::new(&mut out);
    w.u16(VERSION)?;
    w.put(value.task.as_bytes())?;
    for root in [
        &value.request_root,
        &value.model_root,
        &value.input_root,
        &value.result_root,
        &value.evaluator_program_record_root,
        &value.environment_root,
        &value.method_root,
        &value.seed_root,
        &value.metric_schema_root,
    ] {
        w.put(root)?;
    }
    w.u64(value.observed_units)?;
    w.u8(value.status as u8)?;
    w.put(&value.repeated_result_root)?;
    w.put(&[0; 16])?;
    Ok(out)
}

pub fn decode_reproduction(input: &[u8]) -> ArtifactResult<Reproduction> {
    if input.len() != REPRODUCTION_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let mut r = Reader::new(input);
    version(&mut r)?;
    let value = Reproduction {
        task: TaskId::new(r.fixed()?)?,
        request_root: r.fixed()?,
        model_root: r.fixed()?,
        input_root: r.fixed()?,
        result_root: r.fixed()?,
        evaluator_program_record_root: r.fixed()?,
        environment_root: r.fixed()?,
        method_root: r.fixed()?,
        seed_root: r.fixed()?,
        metric_schema_root: r.fixed()?,
        observed_units: r.u64()?,
        status: ReproductionStatus::decode(r.u8()?)?,
        repeated_result_root: r.fixed()?,
    };
    r.reserved(16)?;
    r.finish()?;
    value.validate()?;
    Ok(value)
}

pub fn reproduction_root(context: &ArtifactContext, encoded: &[u8]) -> ArtifactResult<Digest32> {
    decode_reproduction(encoded)?;
    Ok(Digest32::new(h(
        REPRODUCTION_DOMAIN,
        &[&context.bytes(), &length_u32(encoded.len())?, encoded],
    ))?)
}

/// n = ceil(byte_length / 262144), refused above 32 GiB / 131072 chunks.
pub fn chunk_count(byte_length: u64) -> ArtifactResult<u32> {
    if byte_length > MAX_OBJECT_BYTES {
        return Err(ArtifactError::Malformed);
    }
    u32::try_from(byte_length.div_ceil(u64::from(CHUNK_BYTES)))
        .map_err(|_| ArtifactError::Malformed)
}
pub fn expected_chunk_length(byte_length: u64, count: u32, index: u32) -> ArtifactResult<u32> {
    if index >= count {
        return Err(ArtifactError::Malformed);
    }
    if index + 1 < count {
        return Ok(CHUNK_BYTES);
    }
    let before = u64::from(index)
        .checked_mul(u64::from(CHUNK_BYTES))
        .ok_or(ArtifactError::Malformed)?;
    let last = byte_length
        .checked_sub(before)
        .ok_or(ArtifactError::Malformed)?;
    if last == 0 || last > u64::from(CHUNK_BYTES) {
        return Err(ArtifactError::LengthMismatch);
    }
    u32::try_from(last).map_err(|_| ArtifactError::Malformed)
}
pub fn chunk_leaf(index: u32, chunk: &[u8]) -> ArtifactResult<[u8; 32]> {
    if chunk.len() > CHUNK_BYTES as usize {
        return Err(ArtifactError::Malformed);
    }
    Ok(h(
        CHUNK_DOMAIN,
        &[&index.to_be_bytes(), &length_u32(chunk.len())?, chunk],
    ))
}
pub fn empty_tree_root() -> [u8; 32] {
    h(EMPTY_DOMAIN, &[])
}
pub fn node_root(left: &[u8; 32], right: &[u8; 32]) -> [u8; 32] {
    h(NODE_DOMAIN, &[left, right])
}
pub fn content_root(byte_length: u64, count: u32, tree_root: &[u8; 32]) -> [u8; 32] {
    h(
        CONTENT_DOMAIN,
        &[&byte_length.to_be_bytes(), &count.to_be_bytes(), tree_root],
    )
}
/// Reduces leaves in place; an unpaired final node is paired with itself.
pub fn tree_root(leaves: &mut [[u8; 32]]) -> [u8; 32] {
    let mut width = leaves.len();
    if width == 0 {
        return empty_tree_root();
    }
    while width > 1 {
        let next = width.div_ceil(2);
        for i in 0..next {
            let left = leaves[2 * i];
            let right = if 2 * i + 1 < width {
                leaves[2 * i + 1]
            } else {
                left
            };
            leaves[i] = node_root(&left, &right);
        }
        width = next;
    }
    leaves[0]
}
/// Number of proof siblings implied by successive ceil(n/2) levels.
pub fn proof_sibling_count(count: u32) -> usize {
    let mut width = count;
    let mut siblings = 0;
    while width > 1 {
        width = width.div_ceil(2);
        siblings += 1;
    }
    siblings
}
/// Content root of in-memory object bytes; scratch must hold every leaf.
pub fn object_content_root(object: &[u8], scratch: &mut [[u8; 32]]) -> ArtifactResult<[u8; 32]> {
    let byte_length = u64::try_from(object.len()).map_err(|_| ArtifactError::Malformed)?;
    let count = chunk_count(byte_length)?;
    let leaves = scratch
        .get_mut(..count as usize)
        .ok_or(ArtifactError::CapacityUnavailable)?;
    for (index, chunk) in object.chunks(CHUNK_BYTES as usize).enumerate() {
        leaves[index] = chunk_leaf(
            u32::try_from(index).map_err(|_| ArtifactError::Malformed)?,
            chunk,
        )?;
    }
    Ok(content_root(byte_length, count, &tree_root(leaves)))
}

/// Complete-retrieval verifier: every index exactly once (identical duplicates
/// add no coverage), exact lengths and the recomputed manifest content root.
pub struct ContentAssembler<'s> {
    byte_length: u64,
    count: u32,
    expected: [u8; 32],
    leaves: &'s mut [[u8; 32]],
}
impl<'s> ContentAssembler<'s> {
    pub fn new(
        manifest: &ArtifactManifest<'_>,
        scratch: &'s mut [[u8; 32]],
    ) -> ArtifactResult<Self> {
        manifest.validate()?;
        let leaves = scratch
            .get_mut(..manifest.chunk_count as usize)
            .ok_or(ArtifactError::CapacityUnavailable)?;
        leaves.fill([0; 32]);
        Ok(Self {
            byte_length: manifest.byte_length,
            count: manifest.chunk_count,
            expected: manifest.content_root,
            leaves,
        })
    }
    /// Returns true only when the chunk adds new coverage.
    pub fn deliver(&mut self, index: u32, chunk: &[u8]) -> ArtifactResult<bool> {
        let expected = expected_chunk_length(self.byte_length, self.count, index)?;
        if chunk.len() != expected as usize {
            return Err(ArtifactError::LengthMismatch);
        }
        let leaf = chunk_leaf(index, chunk)?;
        let slot = &mut self.leaves[index as usize];
        if *slot == [0; 32] {
            *slot = leaf;
            Ok(true)
        } else if *slot == leaf {
            Ok(false)
        } else {
            Err(ArtifactError::IntegrityConflict)
        }
    }
    pub fn finish(self) -> ArtifactResult<()> {
        if self.leaves.iter().any(|leaf| *leaf == [0; 32]) {
            return Err(ArtifactError::MissingChunk);
        }
        let tree = tree_root(self.leaves);
        if content_root(self.byte_length, self.count, &tree) == self.expected {
            Ok(())
        } else {
            Err(ArtifactError::RootMismatch)
        }
    }
}

/// Membership/integrity of one chunk only; never complete availability.
#[derive(Clone, Copy, Debug)]
pub struct ChunkProof<'a> {
    pub manifest_root: ArtifactManifestRoot,
    pub index: u32,
    pub chunk: &'a [u8],
    pub siblings: Items<'a, [u8; 32]>,
}

pub fn encode_chunk_proof(value: &ChunkProof<'_>, out: &mut [u8]) -> ArtifactResult<usize> {
    if value.chunk.len() > CHUNK_BYTES as usize || value.siblings.len() > MAX_PROOF_SIBLINGS {
        return Err(ArtifactError::Malformed);
    }
    let size = PROOF_FIXED_BYTES + value.chunk.len() + 32 * value.siblings.len();
    if out.len() < size {
        return Err(ArtifactError::Malformed);
    }
    let mut w = Writer::new(out);
    w.u16(VERSION)?;
    w.put(&value.manifest_root.bytes())?;
    w.u32(value.index)?;
    w.put(&length_u32(value.chunk.len())?)?;
    w.put(value.chunk)?;
    w.u8(u8::try_from(value.siblings.len()).map_err(|_| ArtifactError::Malformed)?)?;
    for sibling in value.siblings.iter() {
        sibling?.write(&mut w)?;
    }
    Ok(w.len())
}

pub fn decode_chunk_proof(input: &[u8]) -> ArtifactResult<ChunkProof<'_>> {
    let mut r = Reader::new(input);
    version(&mut r)?;
    let manifest_root = ArtifactManifestRoot::new(r.fixed()?)?;
    let index = r.u32()?;
    let length = r.u32()?;
    if length > CHUNK_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let chunk = r.take(length as usize)?;
    let count = usize::from(r.u8()?);
    if count > MAX_PROOF_SIBLINGS {
        return Err(ArtifactError::Malformed);
    }
    let siblings = Items::Encoded {
        count,
        bytes: r.take(count * 32)?,
    };
    r.finish()?;
    Ok(ChunkProof {
        manifest_root,
        index,
        chunk,
        siblings,
    })
}

/// Verifies against the strictly decoded manifest whose root the proof names.
pub fn verify_chunk_proof(proof: &ChunkProof<'_>, manifest_bytes: &[u8]) -> ArtifactResult<()> {
    let manifest = decode_manifest(manifest_bytes)?;
    if manifest_root(manifest_bytes)? != proof.manifest_root {
        return Err(ArtifactError::RootMismatch);
    }
    let count = manifest.chunk_count;
    let expected = expected_chunk_length(manifest.byte_length, count, proof.index)?;
    if proof.chunk.len() != expected as usize {
        return Err(ArtifactError::LengthMismatch);
    }
    if proof.siblings.len() != proof_sibling_count(count) {
        return Err(ArtifactError::Malformed);
    }
    let mut node = chunk_leaf(proof.index, proof.chunk)?;
    let mut index = proof.index;
    let mut width = count;
    for sibling in proof.siblings.iter() {
        let sibling = sibling?;
        if index + 1 == width && width % 2 == 1 {
            if sibling != node {
                return Err(ArtifactError::Malformed);
            }
            node = node_root(&node, &node);
        } else if index % 2 == 0 {
            node = node_root(&node, &sibling);
        } else {
            node = node_root(&sibling, &node);
        }
        index /= 2;
        width = width.div_ceil(2);
    }
    if content_root(manifest.byte_length, count, &node) == manifest.content_root {
        Ok(())
    } else {
        Err(ArtifactError::RootMismatch)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvidenceTask {
    pub task: TaskId,
    pub request_root: [u8; 32],
    pub result_root: [u8; 32],
    pub execution_root: [u8; 32],
    pub status: TerminalStatus,
    pub reproduction_root: [u8; 32],
}
impl<'a> Item<'a> for EvidenceTask {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self> {
        Ok(Self {
            task: TaskId::new(r.fixed()?)?,
            request_root: r.fixed()?,
            result_root: r.fixed()?,
            execution_root: r.fixed()?,
            status: TerminalStatus::decode(r.u8()?)?,
            reproduction_root: r.fixed()?,
        })
    }
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()> {
        w.put(self.task.as_bytes())?;
        w.put(&self.request_root)?;
        w.put(&self.result_root)?;
        w.put(&self.execution_root)?;
        w.u8(self.status as u8)?;
        Ok(w.put(&self.reproduction_root)?)
    }
}

#[derive(Clone, Copy, Debug)]
pub struct WorkerGroup<'a> {
    pub worker: WorkerId,
    pub generation: Version,
    pub model_root: [u8; 32],
    pub deployment_root: [u8; 32],
    pub score: Score,
    pub reason_code: u16,
    pub reason_artifact_root: [u8; 32],
    pub tasks: Items<'a, EvidenceTask>,
}
impl<'a> Item<'a> for WorkerGroup<'a> {
    fn read(r: &mut Reader<'a>) -> ArtifactResult<Self> {
        let worker = WorkerId::new(r.fixed()?)?;
        let generation = Version::new(r.u64()?)?;
        let model_root = r.fixed()?;
        let deployment_root = r.fixed()?;
        let score = Score::new(r.u32()?)?;
        let reason_code = r.u16()?;
        let reason_artifact_root = r.fixed()?;
        let count = usize::from(r.u16()?);
        if count > MAX_TASKS {
            return Err(ArtifactError::Malformed);
        }
        let tasks = Items::Encoded {
            count,
            bytes: r.take(count * TASK_ENTRY_BYTES)?,
        };
        for task in tasks.iter() {
            task?;
        }
        Ok(Self {
            worker,
            generation,
            model_root,
            deployment_root,
            score,
            reason_code,
            reason_artifact_root,
            tasks,
        })
    }
    fn write(&self, w: &mut Writer<'_>) -> ArtifactResult<()> {
        w.put(self.worker.as_bytes())?;
        w.u64(self.generation.get())?;
        w.put(&self.model_root)?;
        w.put(&self.deployment_root)?;
        w.u32(self.score.get())?;
        w.u16(self.reason_code)?;
        w.put(&self.reason_artifact_root)?;
        w.u16(u16::try_from(self.tasks.len()).map_err(|_| ArtifactError::Malformed)?)?;
        for task in self.tasks.iter() {
            task?.write(w)?;
        }
        Ok(())
    }
}

/// Explicit frozen-policy inputs for evidence admission of optional roots.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvidencePolicy {
    pub mode: AssessmentMode,
    pub rubric: RubricDigest,
    pub dataset_absence_admitted: bool,
    pub benchmark_absence_admitted: bool,
    pub missing_result_admitted: bool,
}

#[derive(Clone, Copy, Debug)]
pub struct EvidenceManifest<'a> {
    pub binding: EvaluatorBinding,
    pub task_policy: PolicyDigest,
    pub task_set: Digest32,
    pub rubric: RubricDigest,
    pub dataset_root: [u8; 32],
    pub benchmark_root: [u8; 32],
    pub mode: AssessmentMode,
    pub groups: Items<'a, WorkerGroup<'a>>,
}
impl EvidenceManifest<'_> {
    pub fn validate(&self, policy: &EvidencePolicy) -> ArtifactResult<()> {
        if self.mode != policy.mode || self.rubric != policy.rubric {
            return Err(ArtifactError::InvalidContext);
        }
        if (self.dataset_root == [0; 32] && !policy.dataset_absence_admitted)
            || (self.benchmark_root == [0; 32] && !policy.benchmark_absence_admitted)
        {
            return Err(ArtifactError::Malformed);
        }
        if self.groups.is_empty() || self.groups.len() > MAX_WORKERS {
            return Err(ArtifactError::Malformed);
        }
        let mut seen = [[0u8; 32]; MAX_TASKS];
        let mut total = 0usize;
        let mut previous_worker: Option<WorkerId> = None;
        for group in self.groups.iter() {
            let group = group?;
            if previous_worker.is_some_and(|w| w >= group.worker) {
                return Err(ArtifactError::Malformed);
            }
            previous_worker = Some(group.worker);
            nonzero(&group.model_root)?;
            nonzero(&group.deployment_root)?;
            if group.tasks.is_empty() {
                return Err(ArtifactError::Malformed);
            }
            let mut previous_task: Option<TaskId> = None;
            for task in group.tasks.iter() {
                let task = task?;
                if previous_task.is_some_and(|t| t >= task.task) {
                    return Err(ArtifactError::Malformed);
                }
                previous_task = Some(task.task);
                if total >= MAX_TASKS || seen[..total].contains(&task.task.bytes()) {
                    return Err(ArtifactError::Malformed);
                }
                seen[total] = task.task.bytes();
                total += 1;
                nonzero(&task.request_root)?;
                let missing = task.result_root == [0; 32] || task.execution_root == [0; 32];
                if missing
                    && (task.status == TerminalStatus::Success || !policy.missing_result_admitted)
                {
                    return Err(ArtifactError::Malformed);
                }
            }
        }
        Ok(())
    }
    pub fn encoded_len(&self) -> ArtifactResult<usize> {
        let mut size = EVIDENCE_FIXED_BYTES;
        for group in self.groups.iter() {
            size = group?
                .tasks
                .len()
                .checked_mul(TASK_ENTRY_BYTES)
                .and_then(|n| n.checked_add(WORKER_GROUP_FIXED_BYTES))
                .and_then(|n| n.checked_add(size))
                .ok_or(ArtifactError::Malformed)?;
        }
        Ok(size)
    }
}

pub fn encode_evidence_manifest(
    value: &EvidenceManifest<'_>,
    policy: &EvidencePolicy,
    out: &mut [u8],
) -> ArtifactResult<usize> {
    value.validate(policy)?;
    let size = value.encoded_len()?;
    if size > MAX_EVIDENCE_BYTES || out.len() < size {
        return Err(ArtifactError::Malformed);
    }
    let mut w = Writer::new(out);
    w.u16(VERSION)?;
    w.put(&encode_binding(&value.binding)?)?;
    w.put(value.task_policy.as_bytes())?;
    w.put(value.task_set.as_bytes())?;
    w.put(value.rubric.as_bytes())?;
    w.put(&value.dataset_root)?;
    w.put(&value.benchmark_root)?;
    w.u8(value.mode as u8)?;
    w.u16(u16::try_from(value.groups.len()).map_err(|_| ArtifactError::Malformed)?)?;
    for group in value.groups.iter() {
        group?.write(&mut w)?;
    }
    w.put(&[0; 16])?;
    Ok(w.len())
}

pub fn decode_evidence_manifest<'a>(
    input: &'a [u8],
    policy: &EvidencePolicy,
) -> ArtifactResult<EvidenceManifest<'a>> {
    if input.len() > MAX_EVIDENCE_BYTES || input.len() < EVIDENCE_FIXED_BYTES {
        return Err(ArtifactError::Malformed);
    }
    let mut r = Reader::new(input);
    version(&mut r)?;
    let binding = decode_binding(r.take(BINDING_BYTES)?)?;
    let task_policy = PolicyDigest::new(r.fixed()?)?;
    let task_set = Digest32::new(r.fixed()?)?;
    let rubric = RubricDigest::new(r.fixed()?)?;
    let dataset_root = r.fixed()?;
    let benchmark_root = r.fixed()?;
    let mode = AssessmentMode::decode(r.u8()?)?;
    let count = usize::from(r.u16()?);
    if count == 0 || count > MAX_WORKERS {
        return Err(ArtifactError::Malformed);
    }
    let start = r.offset();
    let mut total = 0usize;
    for _ in 0..count {
        total = total
            .checked_add(WorkerGroup::read(&mut r)?.tasks.len())
            .ok_or(ArtifactError::Malformed)?;
        if total > MAX_TASKS {
            return Err(ArtifactError::Malformed);
        }
    }
    let groups = Items::Encoded {
        count,
        bytes: &input[start..r.offset()],
    };
    r.reserved(16)?;
    r.finish()?;
    let manifest = EvidenceManifest {
        binding,
        task_policy,
        task_set,
        rubric,
        dataset_root,
        benchmark_root,
        mode,
        groups,
    };
    manifest.validate(policy)?;
    Ok(manifest)
}

/// H(PAXAI/evidence/v1, canonical EvidenceManifestV1 bytes): no artifact
/// wrapping, context prefix or extra length prefix.
pub fn evidence_root(encoded: &[u8], policy: &EvidencePolicy) -> ArtifactResult<EvidenceRoot> {
    decode_evidence_manifest(encoded, policy)?;
    Ok(EvidenceRoot::new(
        domain_hash(EVIDENCE_DOMAIN, encoded)?.bytes(),
    )?)
}
