//! Canonical version-one offline export records and the strict fact-reference grammar.

use std::collections::BTreeSet;

use layerx_wire::hash::batch_header_digest;
use layerx_wire::receipt::{decode_batch_header, encode_batch_header};

use crate::availability::{AvailabilityClass, Chunk};
use crate::checkpoint::{Attestation, Certificate, Checkpoint, GuarantorKey};
use crate::merkle::{decode_proof, encode_proof, MerkleError, Proof, MAX_DEPTH};
use crate::receipt::AuthorizedBatch;

/// The only record version this codec reads or writes.
pub const RECORD_VERSION: u8 = 1;
/// Maximum number of requested facts in one export.
pub const MAX_FACT_REFS: usize = 16;
/// Maximum byte length of one canonical fact reference.
pub const MAX_FACT_REF_BYTES: usize = 135;
/// Maximum byte length of one encoded record.
pub const MAX_RECORD_BYTES: usize = 1_048_576;
/// Maximum number of availability chunks carried by one checkpoint record.
pub const MAX_AVAILABILITY_CHUNKS: usize = 4096;
/// Maximum number of checkpoint guarantors or attestations.
pub const MAX_CHECKPOINT_GUARANTORS: usize = 32;

const RECEIPT_MAGIC: [u8; 4] = *b"LXRF";
const HEADER_MAGIC: [u8; 4] = *b"LXHD";
const INCLUSION_MAGIC: [u8; 4] = *b"LXIP";
const ACCOUNT_STATE_MAGIC: [u8; 4] = *b"LXAS";
const CHECKPOINT_MAGIC: [u8; 4] = *b"LXCP";
const MAX_HEADER_BYTES: usize = 4096;
const MAX_VALIDITY_PROOF_BYTES: usize = 1_048_576;
const MAX_SETTLEMENT_REFERENCE_BYTES: usize = 1024;
const MAX_CHUNK_BYTES: usize = 65_536;
const MAX_PROOF_BYTES: usize = 10 + 32 * MAX_DEPTH;
const ATTESTATION_STATEMENT_BYTES: usize = 189;

/// One parsed fact reference. Every kind carries the activity that anchors
/// tenant ownership.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum FactSelector {
    /// `receipt:<activity_id>`.
    Receipt { activity_id: [u8; 32] },
    /// `activity:<activity_id>`.
    Activity { activity_id: [u8; 32] },
    /// `state:<activity_id>:<account_id>`.
    State {
        activity_id: [u8; 32],
        account_id: [u8; 32],
    },
    /// `checkpoint:<activity_id>:<batch_number>`.
    Checkpoint {
        activity_id: [u8; 32],
        batch_number: u64,
    },
}

/// Exact fact-reference grammar failure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FactRefError {
    /// The fact set is empty.
    NoFacts,
    /// The fact set exceeds [`MAX_FACT_REFS`].
    TooMany { count: usize },
    /// The reference at `index` repeats an earlier reference.
    Duplicate { index: usize },
    /// The reference is empty.
    Empty,
    /// The reference exceeds [`MAX_FACT_REF_BYTES`].
    TooLong { length: usize },
    /// The kind token is not one of the four kinds.
    UnknownKind,
    /// Field count, hex length or hex alphabet is not canonical.
    Malformed,
    /// An identifier is all zero.
    ZeroIdentifier,
    /// The batch decimal is not canonical or overflows `u64`.
    Decimal,
    /// Batch zero has no signed checkpoint header.
    ZeroBatch,
}

impl FactSelector {
    /// Parses one canonical fact reference.
    ///
    /// # Errors
    ///
    /// Refuses empty, oversized, unknown, non-canonical, zero-identifier and
    /// zero-batch references.
    pub fn parse(text: &str) -> Result<Self, FactRefError> {
        if text.is_empty() {
            return Err(FactRefError::Empty);
        }
        if text.len() > MAX_FACT_REF_BYTES {
            return Err(FactRefError::TooLong { length: text.len() });
        }
        let mut parts = text.split(':');
        let kind = parts.next().ok_or(FactRefError::Malformed)?;
        let fields: Vec<&str> = parts.collect();
        match (kind, fields.as_slice()) {
            ("receipt", [activity]) => Ok(Self::Receipt {
                activity_id: hex32(activity)?,
            }),
            ("activity", [activity]) => Ok(Self::Activity {
                activity_id: hex32(activity)?,
            }),
            ("state", [activity, account]) => Ok(Self::State {
                activity_id: hex32(activity)?,
                account_id: hex32(account)?,
            }),
            ("checkpoint", [activity, batch]) => {
                let activity_id = hex32(activity)?;
                let batch_number = decimal(batch)?;
                if batch_number == 0 {
                    return Err(FactRefError::ZeroBatch);
                }
                Ok(Self::Checkpoint {
                    activity_id,
                    batch_number,
                })
            }
            ("receipt" | "activity" | "state" | "checkpoint", _) => Err(FactRefError::Malformed),
            _ => Err(FactRefError::UnknownKind),
        }
    }

    /// Returns the single canonical text of this reference.
    #[must_use]
    pub fn canonical_text(&self) -> String {
        match self {
            Self::Receipt { activity_id } => format!("receipt:{}", hex(activity_id)),
            Self::Activity { activity_id } => format!("activity:{}", hex(activity_id)),
            Self::State {
                activity_id,
                account_id,
            } => format!("state:{}:{}", hex(activity_id), hex(account_id)),
            Self::Checkpoint {
                activity_id,
                batch_number,
            } => format!("checkpoint:{}:{batch_number}", hex(activity_id)),
        }
    }

    /// Returns the activity that anchors tenant ownership of this fact.
    #[must_use]
    pub const fn activity_id(&self) -> [u8; 32] {
        match self {
            Self::Receipt { activity_id }
            | Self::Activity { activity_id }
            | Self::State { activity_id, .. }
            | Self::Checkpoint { activity_id, .. } => *activity_id,
        }
    }
}

/// Parses a complete requested fact set, preserving request order.
///
/// # Errors
///
/// Refuses an empty set, more than [`MAX_FACT_REFS`] references, any
/// grammar failure, and duplicate references.
pub fn parse_fact_set<S: AsRef<str>>(refs: &[S]) -> Result<Vec<FactSelector>, FactRefError> {
    check_fact_count(refs.len())?;
    let facts = refs
        .iter()
        .map(|text| FactSelector::parse(text.as_ref()))
        .collect::<Result<Vec<_>, _>>()?;
    check_fact_selectors(&facts)?;
    Ok(facts)
}

fn check_fact_count(count: usize) -> Result<(), FactRefError> {
    if count == 0 {
        return Err(FactRefError::NoFacts);
    }
    if count > MAX_FACT_REFS {
        return Err(FactRefError::TooMany { count });
    }
    Ok(())
}

pub(crate) fn check_fact_selectors(facts: &[FactSelector]) -> Result<(), FactRefError> {
    check_fact_count(facts.len())?;
    let mut seen = BTreeSet::new();
    for (index, fact) in facts.iter().enumerate() {
        if !seen.insert(*fact) {
            return Err(FactRefError::Duplicate { index });
        }
    }
    Ok(())
}

fn hex32(text: &str) -> Result<[u8; 32], FactRefError> {
    let bytes = text.as_bytes();
    if bytes.len() != 64 {
        return Err(FactRefError::Malformed);
    }
    let mut value = [0_u8; 32];
    for (index, byte) in value.iter_mut().enumerate() {
        let high = nibble(bytes[index * 2])?;
        let low = nibble(bytes[index * 2 + 1])?;
        *byte = (high << 4) | low;
    }
    if value == [0; 32] {
        return Err(FactRefError::ZeroIdentifier);
    }
    Ok(value)
}

const fn nibble(byte: u8) -> Result<u8, FactRefError> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        _ => Err(FactRefError::Malformed),
    }
}

fn decimal(text: &str) -> Result<u64, FactRefError> {
    let bytes = text.as_bytes();
    if bytes.is_empty()
        || !bytes.iter().all(u8::is_ascii_digit)
        || (bytes.len() > 1 && bytes[0] == b'0')
    {
        return Err(FactRefError::Decimal);
    }
    bytes.iter().try_fold(0_u64, |value, digit| {
        value
            .checked_mul(10)
            .and_then(|value| value.checked_add(u64::from(digit - b'0')))
            .ok_or(FactRefError::Decimal)
    })
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut text = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        text.push(char::from(DIGITS[usize::from(byte >> 4)]));
        text.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    text
}

/// Exact record codec failure.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExportCodecError {
    /// A field extends past the end of the record.
    Truncated,
    /// Bytes remain after the final field.
    TrailingBytes,
    /// The record magic is not the expected record kind.
    Magic,
    /// The record version is not [`RECORD_VERSION`].
    Version,
    /// An option, variant, kind, class or boolean tag is unknown.
    Tag,
    /// A count, length or value lies outside its declared bound.
    Bound,
    /// Decoding then canonical re-encoding did not reproduce the input.
    NonCanonical,
    /// An embedded fact reference failed the strict grammar.
    Reference(FactRefError),
    /// An embedded fact reference has the wrong kind or identity for its record.
    ReferenceKind,
    /// An embedded Merkle proof failed its canonical decoding.
    Proof(MerkleError),
    /// An embedded batch header is not canonical.
    Header,
}

struct Reader<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], ExportCodecError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(ExportCodecError::Truncated)?;
        let slice = self
            .bytes
            .get(self.offset..end)
            .ok_or(ExportCodecError::Truncated)?;
        self.offset = end;
        Ok(slice)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], ExportCodecError> {
        self.take(N)?
            .try_into()
            .map_err(|_| ExportCodecError::Truncated)
    }

    fn u8(&mut self) -> Result<u8, ExportCodecError> {
        Ok(self.array::<1>()?[0])
    }

    fn u16(&mut self) -> Result<u16, ExportCodecError> {
        Ok(u16::from_be_bytes(self.array()?))
    }

    fn u32(&mut self) -> Result<u32, ExportCodecError> {
        Ok(u32::from_be_bytes(self.array()?))
    }

    fn u64(&mut self) -> Result<u64, ExportCodecError> {
        Ok(u64::from_be_bytes(self.array()?))
    }

    fn boolean(&mut self) -> Result<bool, ExportCodecError> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(ExportCodecError::Tag),
        }
    }

    fn length_prefixed(&mut self, maximum: usize) -> Result<&'a [u8], ExportCodecError> {
        let length = usize::try_from(self.u32()?).map_err(|_| ExportCodecError::Bound)?;
        if length > maximum {
            return Err(ExportCodecError::Bound);
        }
        self.take(length)
    }

    fn nonempty(&mut self, maximum: usize) -> Result<Vec<u8>, ExportCodecError> {
        let bytes = self.length_prefixed(maximum)?;
        if bytes.is_empty() {
            return Err(ExportCodecError::Bound);
        }
        Ok(bytes.to_vec())
    }

    fn proof(&mut self) -> Result<Proof, ExportCodecError> {
        decode_proof(self.length_prefixed(MAX_PROOF_BYTES)?).map_err(ExportCodecError::Proof)
    }

    fn reference(&mut self) -> Result<FactSelector, ExportCodecError> {
        let bytes = self.length_prefixed(MAX_FACT_REF_BYTES)?;
        let text = core::str::from_utf8(bytes)
            .map_err(|_| ExportCodecError::Reference(FactRefError::Malformed))?;
        FactSelector::parse(text).map_err(ExportCodecError::Reference)
    }

    fn option_bytes(&mut self, maximum: usize) -> Result<Option<Vec<u8>>, ExportCodecError> {
        if self.boolean()? {
            Ok(Some(self.nonempty(maximum)?))
        } else {
            Ok(None)
        }
    }

    fn preamble(&mut self, magic: [u8; 4]) -> Result<(), ExportCodecError> {
        if self.array::<4>()? != magic {
            return Err(ExportCodecError::Magic);
        }
        if self.u8()? != RECORD_VERSION {
            return Err(ExportCodecError::Version);
        }
        Ok(())
    }

    fn finish(&self) -> Result<(), ExportCodecError> {
        if self.offset == self.bytes.len() {
            Ok(())
        } else {
            Err(ExportCodecError::TrailingBytes)
        }
    }
}

struct Writer(Vec<u8>);

impl Writer {
    fn new(magic: [u8; 4]) -> Self {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(&magic);
        bytes.push(RECORD_VERSION);
        Self(bytes)
    }

    fn raw(&mut self, bytes: &[u8]) {
        self.0.extend_from_slice(bytes);
    }

    fn u8(&mut self, value: u8) {
        self.0.push(value);
    }

    fn u16(&mut self, value: u16) {
        self.raw(&value.to_be_bytes());
    }

    fn u32(&mut self, value: u32) {
        self.raw(&value.to_be_bytes());
    }

    fn u64(&mut self, value: u64) {
        self.raw(&value.to_be_bytes());
    }

    fn length_prefixed(&mut self, bytes: &[u8], maximum: usize) -> Result<(), ExportCodecError> {
        if bytes.len() > maximum {
            return Err(ExportCodecError::Bound);
        }
        self.u32(u32::try_from(bytes.len()).map_err(|_| ExportCodecError::Bound)?);
        self.raw(bytes);
        Ok(())
    }

    fn nonempty(&mut self, bytes: &[u8], maximum: usize) -> Result<(), ExportCodecError> {
        if bytes.is_empty() {
            return Err(ExportCodecError::Bound);
        }
        self.length_prefixed(bytes, maximum)
    }

    fn proof(&mut self, proof: &Proof) -> Result<(), ExportCodecError> {
        self.length_prefixed(&encode_proof(proof), MAX_PROOF_BYTES)
    }

    fn reference(&mut self, reference: &FactSelector) -> Result<(), ExportCodecError> {
        self.length_prefixed(reference.canonical_text().as_bytes(), MAX_FACT_REF_BYTES)
    }

    fn option_bytes(
        &mut self,
        value: Option<&[u8]>,
        maximum: usize,
    ) -> Result<(), ExportCodecError> {
        match value {
            None => {
                self.u8(0);
                Ok(())
            }
            Some(bytes) => {
                self.u8(1);
                self.nonempty(bytes, maximum)
            }
        }
    }

    fn finish(self) -> Result<Vec<u8>, ExportCodecError> {
        if self.0.len() > MAX_RECORD_BYTES {
            return Err(ExportCodecError::Bound);
        }
        Ok(self.0)
    }
}

fn canonical<T>(
    bytes: &[u8],
    parse: impl FnOnce(&mut Reader<'_>) -> Result<T, ExportCodecError>,
    encode: impl FnOnce(&T) -> Result<Vec<u8>, ExportCodecError>,
) -> Result<T, ExportCodecError> {
    if bytes.len() > MAX_RECORD_BYTES {
        return Err(ExportCodecError::Bound);
    }
    let mut reader = Reader::new(bytes);
    let value = parse(&mut reader)?;
    reader.finish()?;
    if encode(&value)? != bytes {
        return Err(ExportCodecError::NonCanonical);
    }
    Ok(value)
}

/// `LXRF` v1: one exact receipt with its authorised batch binding.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReceiptRecord {
    pub reference: FactSelector,
    pub canonical_receipt: Vec<u8>,
    pub authorised_batch: AuthorizedBatch,
    pub expected_receipt_digest: [u8; 32],
}

impl ReceiptRecord {
    /// Encodes the canonical record.
    ///
    /// # Errors
    ///
    /// Refuses a wrong reference kind, empty receipt or bound overflow.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        if !matches!(self.reference, FactSelector::Receipt { .. }) {
            return Err(ExportCodecError::ReferenceKind);
        }
        let mut writer = Writer::new(RECEIPT_MAGIC);
        writer.reference(&self.reference)?;
        writer.nonempty(&self.canonical_receipt, MAX_RECORD_BYTES)?;
        writer.raw(&self.authorised_batch.batch_id());
        writer.raw(&self.authorised_batch.asset());
        writer.raw(&self.authorised_batch.previous_state_root());
        writer.raw(&self.authorised_batch.resulting_state_root());
        writer.raw(&self.authorised_batch.sequencer_public_key());
        writer.raw(&self.expected_receipt_digest);
        writer.finish()
    }

    /// Decodes one canonical record.
    ///
    /// # Errors
    ///
    /// Refuses any non-canonical, truncated, trailing or out-of-bound input.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        canonical(
            bytes,
            |reader| {
                reader.preamble(RECEIPT_MAGIC)?;
                let reference = reader.reference()?;
                let canonical_receipt = reader.nonempty(MAX_RECORD_BYTES)?;
                let authorised_batch = AuthorizedBatch::new(
                    reader.array()?,
                    reader.array()?,
                    reader.array()?,
                    reader.array()?,
                    reader.array()?,
                );
                Ok(Self {
                    reference,
                    canonical_receipt,
                    authorised_batch,
                    expected_receipt_digest: reader.array()?,
                })
            },
            Self::encode,
        )
    }
}

/// `LXHD` v1: one exact signed batch header and the authority it claims.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HeaderRecord {
    pub canonical_header: Vec<u8>,
    pub signature: [u8; 64],
    pub sequencer_id: [u8; 32],
    pub public_key: [u8; 32],
    pub first_batch: u64,
    pub last_batch: u64,
}

impl HeaderRecord {
    /// Encodes the canonical record.
    ///
    /// # Errors
    ///
    /// Refuses a non-canonical header, reversed range or bound overflow.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        let header =
            decode_batch_header(&self.canonical_header).map_err(|_| ExportCodecError::Header)?;
        if encode_batch_header(&header).map_err(|_| ExportCodecError::Header)?
            != self.canonical_header
        {
            return Err(ExportCodecError::Header);
        }
        if self.first_batch > self.last_batch {
            return Err(ExportCodecError::Bound);
        }
        let mut writer = Writer::new(HEADER_MAGIC);
        writer.nonempty(&self.canonical_header, MAX_HEADER_BYTES)?;
        writer.raw(&self.signature);
        writer.raw(&self.sequencer_id);
        writer.raw(&self.public_key);
        writer.u64(self.first_batch);
        writer.u64(self.last_batch);
        writer.finish()
    }

    /// Decodes one canonical record.
    ///
    /// # Errors
    ///
    /// Refuses any non-canonical, truncated, trailing or out-of-bound input.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        canonical(
            bytes,
            |reader| {
                reader.preamble(HEADER_MAGIC)?;
                Ok(Self {
                    canonical_header: reader.nonempty(MAX_HEADER_BYTES)?,
                    signature: reader.array()?,
                    sequencer_id: reader.array()?,
                    public_key: reader.array()?,
                    first_batch: reader.u64()?,
                    last_batch: reader.u64()?,
                })
            },
            Self::encode,
        )
    }

    /// Returns the signed header digest, which is the record identity.
    ///
    /// # Errors
    ///
    /// Refuses a header the digest function cannot hash.
    pub fn digest(&self) -> Result<[u8; 32], ExportCodecError> {
        batch_header_digest(&self.canonical_header).map_err(|_| ExportCodecError::Header)
    }
}

/// Root an [`InclusionRecord`] leaf is proven under.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
#[repr(u8)]
pub enum InclusionRecordKind {
    /// Canonical signed activity under the header activity root.
    Activity = 1,
    /// Canonical receipt under the header receipt root.
    Receipt = 2,
}

/// `LXIP` v1: one exact leaf, its batch Merkle path and its header identity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InclusionRecord {
    pub kind: InclusionRecordKind,
    pub reference: FactSelector,
    pub canonical_leaf: Vec<u8>,
    pub proof: Proof,
    pub header_digest: [u8; 32],
}

impl InclusionRecord {
    fn check_kind(&self) -> Result<(), ExportCodecError> {
        match (self.kind, self.reference) {
            (InclusionRecordKind::Activity, FactSelector::Activity { .. })
            | (InclusionRecordKind::Receipt, FactSelector::Receipt { .. }) => Ok(()),
            _ => Err(ExportCodecError::ReferenceKind),
        }
    }

    /// Encodes the canonical record.
    ///
    /// # Errors
    ///
    /// Refuses a kind/reference mismatch, empty leaf or bound overflow.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        self.check_kind()?;
        let mut writer = Writer::new(INCLUSION_MAGIC);
        writer.u8(self.kind as u8);
        writer.reference(&self.reference)?;
        writer.nonempty(&self.canonical_leaf, MAX_RECORD_BYTES)?;
        writer.proof(&self.proof)?;
        writer.raw(&self.header_digest);
        writer.finish()
    }

    /// Decodes one canonical record.
    ///
    /// # Errors
    ///
    /// Refuses any non-canonical, truncated, trailing or out-of-bound input.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        canonical(
            bytes,
            |reader| {
                reader.preamble(INCLUSION_MAGIC)?;
                let kind = match reader.u8()? {
                    1 => InclusionRecordKind::Activity,
                    2 => InclusionRecordKind::Receipt,
                    _ => return Err(ExportCodecError::Tag),
                };
                let record = Self {
                    kind,
                    reference: reader.reference()?,
                    canonical_leaf: reader.nonempty(MAX_RECORD_BYTES)?,
                    proof: reader.proof()?,
                    header_digest: reader.array()?,
                };
                record.check_kind()?;
                Ok(record)
            },
            Self::encode,
        )
    }
}

/// Which nested account verifier an [`AccountStateRecord`] uses.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AccountStateVariant {
    /// Account proof terminated by the activity receipt.
    Activity,
    /// Account proof terminated by the batch maintenance leaf, linked to the
    /// activity receipt exported in the same header.
    Maintenance {
        activity_count: u32,
        parameter_version: u32,
        dependency_receipt_activity_id: [u8; 32],
    },
}

/// `LXAS` v1: one nested account proof under a signed header.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountStateRecord {
    pub reference: FactSelector,
    pub variant: AccountStateVariant,
    pub account_id: [u8; 32],
    pub account_value: Vec<u8>,
    pub account_root: [u8; 32],
    pub universal_root: [u8; 32],
    pub resulting_state_root: [u8; 32],
    pub account_proof: Proof,
    pub account_tree_proof: Proof,
    pub universal_root_proof: Proof,
    pub receipt_bytes: Vec<u8>,
    pub receipt_proof: Proof,
    pub header_digest: [u8; 32],
}

impl AccountStateRecord {
    fn check_reference(&self) -> Result<(), ExportCodecError> {
        match self.reference {
            FactSelector::State { account_id, .. } if account_id == self.account_id => Ok(()),
            _ => Err(ExportCodecError::ReferenceKind),
        }
    }

    /// Encodes the canonical record.
    ///
    /// # Errors
    ///
    /// Refuses a reference that does not name this account, empty material or
    /// bound overflow.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        self.check_reference()?;
        let mut writer = Writer::new(ACCOUNT_STATE_MAGIC);
        writer.reference(&self.reference)?;
        writer.u8(match self.variant {
            AccountStateVariant::Activity => 1,
            AccountStateVariant::Maintenance { .. } => 2,
        });
        writer.raw(&self.account_id);
        writer.nonempty(&self.account_value, MAX_RECORD_BYTES)?;
        writer.raw(&self.account_root);
        writer.raw(&self.universal_root);
        writer.raw(&self.resulting_state_root);
        writer.proof(&self.account_proof)?;
        writer.proof(&self.account_tree_proof)?;
        writer.proof(&self.universal_root_proof)?;
        writer.nonempty(&self.receipt_bytes, MAX_RECORD_BYTES)?;
        writer.proof(&self.receipt_proof)?;
        writer.raw(&self.header_digest);
        if let AccountStateVariant::Maintenance {
            activity_count,
            parameter_version,
            dependency_receipt_activity_id,
        } = self.variant
        {
            writer.u32(activity_count);
            writer.u32(parameter_version);
            writer.raw(&dependency_receipt_activity_id);
        }
        writer.finish()
    }

    /// Decodes one canonical record.
    ///
    /// # Errors
    ///
    /// Refuses any non-canonical, truncated, trailing or out-of-bound input.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        canonical(
            bytes,
            |reader| {
                reader.preamble(ACCOUNT_STATE_MAGIC)?;
                let reference = reader.reference()?;
                let tag = reader.u8()?;
                if !matches!(tag, 1 | 2) {
                    return Err(ExportCodecError::Tag);
                }
                let mut record = Self {
                    reference,
                    variant: AccountStateVariant::Activity,
                    account_id: reader.array()?,
                    account_value: reader.nonempty(MAX_RECORD_BYTES)?,
                    account_root: reader.array()?,
                    universal_root: reader.array()?,
                    resulting_state_root: reader.array()?,
                    account_proof: reader.proof()?,
                    account_tree_proof: reader.proof()?,
                    universal_root_proof: reader.proof()?,
                    receipt_bytes: reader.nonempty(MAX_RECORD_BYTES)?,
                    receipt_proof: reader.proof()?,
                    header_digest: reader.array()?,
                };
                if tag == 2 {
                    record.variant = AccountStateVariant::Maintenance {
                        activity_count: reader.u32()?,
                        parameter_version: reader.u32()?,
                        dependency_receipt_activity_id: reader.array()?,
                    };
                }
                record.check_reference()?;
                Ok(record)
            },
            Self::encode,
        )
    }
}

/// `LXCP` v1: one checkpoint certificate, its checkpoint-relative bonded set
/// and every availability chunk of the certified batch.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointRecord {
    pub reference: FactSelector,
    pub certificate: Certificate,
    pub set_version: u64,
    pub bonded_set: Vec<GuarantorKey>,
    pub checkpoint_id: [u8; 32],
    pub registered_settlement_reference: Option<Vec<u8>>,
    pub availability: Vec<(Chunk, Proof)>,
}

impl CheckpointRecord {
    /// Encodes the canonical record.
    ///
    /// # Errors
    ///
    /// Refuses a wrong reference kind, empty or oversized sets, non-contiguous
    /// chunk indices and bound overflow.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        if !matches!(self.reference, FactSelector::Checkpoint { .. }) {
            return Err(ExportCodecError::ReferenceKind);
        }
        if self.set_version == 0
            || self.bonded_set.is_empty()
            || self.bonded_set.len() > MAX_CHECKPOINT_GUARANTORS
            || self.availability.is_empty()
            || self.availability.len() > MAX_AVAILABILITY_CHUNKS
        {
            return Err(ExportCodecError::Bound);
        }
        let mut writer = Writer::new(CHECKPOINT_MAGIC);
        writer.reference(&self.reference)?;
        writer.nonempty(&encode_certificate(&self.certificate)?, MAX_RECORD_BYTES)?;
        writer.u64(self.set_version);
        writer.u16(u16::try_from(self.bonded_set.len()).map_err(|_| ExportCodecError::Bound)?);
        for key in &self.bonded_set {
            writer.raw(&key.guarantor_id());
            writer.raw(&key.public_key());
            writer.u8(u8::from(key.bonded()));
        }
        writer.raw(&self.checkpoint_id);
        writer.option_bytes(
            self.registered_settlement_reference.as_deref(),
            MAX_SETTLEMENT_REFERENCE_BYTES,
        )?;
        writer.u16(u16::try_from(self.availability.len()).map_err(|_| ExportCodecError::Bound)?);
        for (position, (chunk, proof)) in self.availability.iter().enumerate() {
            if usize::try_from(chunk.index) != Ok(position) {
                return Err(ExportCodecError::Bound);
            }
            writer.u64(chunk.batch_number);
            writer.u32(chunk.index);
            writer.u8(chunk.class as u8);
            writer.u64(chunk.class_offset);
            writer.length_prefixed(&chunk.bytes, MAX_CHUNK_BYTES)?;
            writer.raw(&chunk.claimed_hash);
            writer.proof(proof)?;
        }
        writer.finish()
    }

    /// Decodes one canonical record.
    ///
    /// # Errors
    ///
    /// Refuses any non-canonical, truncated, trailing or out-of-bound input.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        canonical(
            bytes,
            |reader| {
                reader.preamble(CHECKPOINT_MAGIC)?;
                let reference = reader.reference()?;
                if !matches!(reference, FactSelector::Checkpoint { .. }) {
                    return Err(ExportCodecError::ReferenceKind);
                }
                let certificate = decode_certificate(reader.length_prefixed(MAX_RECORD_BYTES)?)?;
                let set_version = reader.u64()?;
                let key_count = usize::from(reader.u16()?);
                if set_version == 0 || key_count == 0 || key_count > MAX_CHECKPOINT_GUARANTORS {
                    return Err(ExportCodecError::Bound);
                }
                let mut bonded_set = Vec::with_capacity(key_count);
                for _ in 0..key_count {
                    bonded_set.push(GuarantorKey::new(
                        reader.array()?,
                        reader.array()?,
                        reader.boolean()?,
                    ));
                }
                let checkpoint_id = reader.array()?;
                let registered_settlement_reference =
                    reader.option_bytes(MAX_SETTLEMENT_REFERENCE_BYTES)?;
                let chunk_count = usize::from(reader.u16()?);
                if chunk_count == 0 || chunk_count > MAX_AVAILABILITY_CHUNKS {
                    return Err(ExportCodecError::Bound);
                }
                let mut availability = Vec::with_capacity(chunk_count);
                for position in 0..chunk_count {
                    let batch_number = reader.u64()?;
                    let index = reader.u32()?;
                    if usize::try_from(index) != Ok(position) {
                        return Err(ExportCodecError::Bound);
                    }
                    let class = match reader.u8()? {
                        1 => AvailabilityClass::Activities,
                        2 => AvailabilityClass::Receipts,
                        3 => AvailabilityClass::Oracle,
                        4 => AvailabilityClass::StateDiff,
                        5 => AvailabilityClass::Recovery,
                        _ => return Err(ExportCodecError::Tag),
                    };
                    let class_offset = reader.u64()?;
                    let chunk_bytes = reader.length_prefixed(MAX_CHUNK_BYTES)?.to_vec();
                    let claimed_hash = reader.array()?;
                    let proof = reader.proof()?;
                    availability.push((
                        Chunk {
                            batch_number,
                            index,
                            class,
                            class_offset,
                            bytes: chunk_bytes,
                            claimed_hash,
                        },
                        proof,
                    ));
                }
                Ok(Self {
                    reference,
                    certificate,
                    set_version,
                    bonded_set,
                    checkpoint_id,
                    registered_settlement_reference,
                    availability,
                })
            },
            Self::encode,
        )
    }
}

fn encode_certificate(certificate: &Certificate) -> Result<Vec<u8>, ExportCodecError> {
    let attestations = certificate.attestations();
    if attestations.is_empty()
        || attestations.len() > MAX_CHECKPOINT_GUARANTORS
        || certificate.threshold() == 0
        || certificate.threshold() > attestations.len()
        || attestations
            .windows(2)
            .any(|pair| pair[0].guarantor_id() >= pair[1].guarantor_id())
    {
        return Err(ExportCodecError::Bound);
    }
    let mut writer = Writer(Vec::new());
    writer.nonempty(certificate.checkpoint().header_bytes(), MAX_HEADER_BYTES)?;
    writer.length_prefixed(
        certificate.checkpoint().validity_proof(),
        MAX_VALIDITY_PROOF_BYTES,
    )?;
    writer.u8(u8::try_from(certificate.threshold()).map_err(|_| ExportCodecError::Bound)?);
    writer.u8(u8::try_from(attestations.len()).map_err(|_| ExportCodecError::Bound)?);
    for attestation in attestations {
        writer.raw(&attestation.canonical_statement());
        writer.raw(&attestation.signer());
        writer.raw(&attestation.signature());
        writer.u8(attestation.signature_v());
    }
    writer.option_bytes(
        certificate.settlement_reference(),
        MAX_SETTLEMENT_REFERENCE_BYTES,
    )?;
    writer.finish()
}

fn decode_certificate(bytes: &[u8]) -> Result<Certificate, ExportCodecError> {
    let mut reader = Reader::new(bytes);
    let header = reader.nonempty(MAX_HEADER_BYTES)?;
    let validity = reader.length_prefixed(MAX_VALIDITY_PROOF_BYTES)?.to_vec();
    let threshold = usize::from(reader.u8()?);
    let count = usize::from(reader.u8()?);
    if count == 0 || count > MAX_CHECKPOINT_GUARANTORS || threshold == 0 || threshold > count {
        return Err(ExportCodecError::Bound);
    }
    let mut attestations = Vec::with_capacity(count);
    for _ in 0..count {
        let statement: [u8; ATTESTATION_STATEMENT_BYTES] = reader.array()?;
        attestations.push(decode_attestation(
            &statement,
            reader.array()?,
            reader.array()?,
            reader.u8()?,
        )?);
    }
    if attestations
        .windows(2)
        .any(|pair| pair[0].guarantor_id() >= pair[1].guarantor_id())
    {
        return Err(ExportCodecError::Bound);
    }
    let settlement_reference = reader.option_bytes(MAX_SETTLEMENT_REFERENCE_BYTES)?;
    reader.finish()?;
    let certificate = Certificate::new(
        Checkpoint::new(header, validity),
        attestations,
        threshold,
        settlement_reference,
    );
    if encode_certificate(&certificate)? != bytes {
        return Err(ExportCodecError::NonCanonical);
    }
    Ok(certificate)
}

fn decode_attestation(
    statement: &[u8; ATTESTATION_STATEMENT_BYTES],
    signer: [u8; 20],
    signature: [u8; 64],
    signature_v: u8,
) -> Result<Attestation, ExportCodecError> {
    let mut reader = Reader::new(statement);
    let attestation = Attestation::new(
        reader.u16()?,
        reader.u32()?,
        reader.u64()?,
        reader.array()?,
        reader.u64()?,
        reader.array()?,
        reader.array()?,
        reader.array()?,
        reader.u64()?,
        reader.array()?,
        reader.boolean()?,
        reader.boolean()?,
        reader.u8()?,
        reader.u64()?,
        signer,
        signature,
        signature_v,
    );
    reader.finish()?;
    if attestation.canonical_statement() != *statement {
        return Err(ExportCodecError::NonCanonical);
    }
    Ok(attestation)
}

/// One element of the proofs bucket.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProofRecord {
    Inclusion(InclusionRecord),
    AccountState(AccountStateRecord),
}

impl ProofRecord {
    /// Encodes the wrapped canonical record.
    ///
    /// # Errors
    ///
    /// Returns the wrapped record's encoding failure.
    pub fn encode(&self) -> Result<Vec<u8>, ExportCodecError> {
        match self {
            Self::Inclusion(record) => record.encode(),
            Self::AccountState(record) => record.encode(),
        }
    }

    /// Decodes an `LXIP` or `LXAS` record selected by its magic.
    ///
    /// # Errors
    ///
    /// Refuses any other magic and every wrapped decoding failure.
    pub fn decode(bytes: &[u8]) -> Result<Self, ExportCodecError> {
        match bytes.get(..4) {
            Some(magic) if magic == INCLUSION_MAGIC => {
                InclusionRecord::decode(bytes).map(Self::Inclusion)
            }
            Some(magic) if magic == ACCOUNT_STATE_MAGIC => {
                AccountStateRecord::decode(bytes).map(Self::AccountState)
            }
            Some(_) => Err(ExportCodecError::Magic),
            None => Err(ExportCodecError::Truncated),
        }
    }
}

/// A complete export decoded into typed canonical records.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CompleteOfflineArtifact {
    pub facts: Vec<FactSelector>,
    pub receipts: Vec<ReceiptRecord>,
    pub proofs: Vec<ProofRecord>,
    pub certificates: Vec<CheckpointRecord>,
    pub headers: Vec<HeaderRecord>,
}

/// The canonical byte form of every bucket of a [`CompleteOfflineArtifact`].
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EncodedOfflineArtifact {
    pub facts: Vec<String>,
    pub receipts: Vec<Vec<u8>>,
    pub proofs: Vec<Vec<u8>>,
    pub certificates: Vec<Vec<u8>>,
    pub headers: Vec<Vec<u8>>,
}

/// Failure decoding a complete artifact.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ArtifactDecodeError {
    /// The requested fact set failed the strict grammar.
    Facts(FactRefError),
    /// One record failed; `bucket` names facts/receipts/proofs/certificates/headers.
    Record {
        bucket: &'static str,
        index: usize,
        error: ExportCodecError,
    },
}

fn decode_bucket<B: AsRef<[u8]>, T>(
    bucket: &'static str,
    records: &[B],
    decode: impl Fn(&[u8]) -> Result<T, ExportCodecError>,
) -> Result<Vec<T>, ArtifactDecodeError> {
    records
        .iter()
        .enumerate()
        .map(|(index, bytes)| {
            decode(bytes.as_ref()).map_err(|error| ArtifactDecodeError::Record {
                bucket,
                index,
                error,
            })
        })
        .collect()
}

fn encode_bucket<T>(
    bucket: &'static str,
    records: &[T],
    encode: impl Fn(&T) -> Result<Vec<u8>, ExportCodecError>,
) -> Result<Vec<Vec<u8>>, ArtifactDecodeError> {
    records
        .iter()
        .enumerate()
        .map(|(index, record)| {
            encode(record).map_err(|error| ArtifactDecodeError::Record {
                bucket,
                index,
                error,
            })
        })
        .collect()
}

impl CompleteOfflineArtifact {
    /// Decodes every bucket with the strict grammar and canonical record codecs.
    ///
    /// # Errors
    ///
    /// Returns the first fact-set or record failure; nothing is partially accepted.
    pub fn decode<F: AsRef<str>, B: AsRef<[u8]>>(
        facts: &[F],
        receipts: &[B],
        proofs: &[B],
        certificates: &[B],
        headers: &[B],
    ) -> Result<Self, ArtifactDecodeError> {
        Ok(Self {
            facts: parse_fact_set(facts).map_err(ArtifactDecodeError::Facts)?,
            receipts: decode_bucket("receipts", receipts, ReceiptRecord::decode)?,
            proofs: decode_bucket("proofs", proofs, ProofRecord::decode)?,
            certificates: decode_bucket("certificates", certificates, CheckpointRecord::decode)?,
            headers: decode_bucket("headers", headers, HeaderRecord::decode)?,
        })
    }

    /// Encodes every bucket canonically, preserving fact request order.
    ///
    /// # Errors
    ///
    /// Returns the first fact-set or record encoding failure.
    pub fn encode(&self) -> Result<EncodedOfflineArtifact, ArtifactDecodeError> {
        check_fact_selectors(&self.facts).map_err(ArtifactDecodeError::Facts)?;
        Ok(EncodedOfflineArtifact {
            facts: self
                .facts
                .iter()
                .map(FactSelector::canonical_text)
                .collect(),
            receipts: encode_bucket("receipts", &self.receipts, ReceiptRecord::encode)?,
            proofs: encode_bucket("proofs", &self.proofs, ProofRecord::encode)?,
            certificates: encode_bucket(
                "certificates",
                &self.certificates,
                CheckpointRecord::encode,
            )?,
            headers: encode_bucket("headers", &self.headers, HeaderRecord::encode)?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::{parse_fact_set, FactRefError, FactSelector};

    #[test]
    fn fact_grammar_is_strict_and_round_trips() {
        let id = "01".repeat(32);
        let account = "ab".repeat(32);
        for text in [
            format!("receipt:{id}"),
            format!("activity:{id}"),
            format!("state:{id}:{account}"),
            format!("checkpoint:{id}:18446744073709551615"),
        ] {
            let parsed = FactSelector::parse(&text);
            assert_eq!(parsed.map(|fact| fact.canonical_text()), Ok(text));
        }
        assert_eq!(
            FactSelector::parse(&format!("state:{id}:{account}")).map(|_| ()),
            Ok(())
        );
        assert_eq!(
            format!("state:{id}:{account}").len(),
            super::MAX_FACT_REF_BYTES
        );
        let zero = "00".repeat(32);
        let upper = "AB".repeat(32);
        for (text, error) in [
            (String::new(), FactRefError::Empty),
            (format!("receipt:{zero}"), FactRefError::ZeroIdentifier),
            (format!("receipt:{upper}"), FactRefError::Malformed),
            (format!("receipt:0x{}", &id[2..]), FactRefError::Malformed),
            (format!("receipt:{id}:1"), FactRefError::Malformed),
            (format!("receipt: {id}"), FactRefError::Malformed),
            (format!("proof:{id}"), FactRefError::UnknownKind),
            (format!("checkpoint:{id}:0"), FactRefError::ZeroBatch),
            (format!("checkpoint:{id}:01"), FactRefError::Decimal),
            (format!("checkpoint:{id}:+1"), FactRefError::Decimal),
            (
                format!("checkpoint:{id}:18446744073709551616"),
                FactRefError::Decimal,
            ),
        ] {
            assert_eq!(FactSelector::parse(&text), Err(error), "{text}");
        }
        let receipt = format!("receipt:{id}");
        assert_eq!(
            parse_fact_set(&[receipt.clone(), receipt.clone()]),
            Err(FactRefError::Duplicate { index: 1 })
        );
        assert_eq!(parse_fact_set::<String>(&[]), Err(FactRefError::NoFacts));
        let many: Vec<String> = (1..=17_u8)
            .map(|byte| format!("receipt:{}", format!("{byte:02x}").repeat(32)))
            .collect();
        assert_eq!(
            parse_fact_set(&many),
            Err(FactRefError::TooMany { count: 17 })
        );
        assert_eq!(parse_fact_set(&many[..16]).map(|facts| facts.len()), Ok(16));
    }
}
