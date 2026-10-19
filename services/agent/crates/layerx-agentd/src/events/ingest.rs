use std::collections::VecDeque;
use std::fmt::{Display, Formatter};

use layerx_types::verify::VerificationLevel;

use crate::store::{
    EventIngestion, ObjectKind, StorageClass, Store, StoreError, TenantId, TenantKey,
};

const WATERMARK_ID: &[u8] = b"event-ingestion-watermark";
const WATERMARK_MAGIC: &[u8; 4] = b"LXEW";
const METADATA_PREFIX: &[u8] = b"event-evidence:";
const METADATA_MAGIC: &[u8; 4] = b"LXEM";

/// Durable position of the strict global event stream.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Watermark {
    pub last_ingested: Option<u64>,
    pub next_expected: u64,
}

/// Exact event bytes and the receipt evidence supplied by the core boundary.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CoreEvent {
    pub global_sequence: u64,
    pub canonical_bytes: Vec<u8>,
    pub receipt_reference: Option<[u8; 32]>,
    pub receipt_verification_level: VerificationLevel,
    pub attributes: EventAttributes,
}

/// Core-produced dimensions used to narrow an event only after tenant and
/// authority scope have already been established.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EventAttributes {
    pub agent: String,
    pub account: String,
    pub activity_type: u16,
    pub module: String,
    pub asset: String,
    pub counterparty: String,
    pub result_code: i32,
}

/// Failures that prevent an event from entering the durable ordered stream.
#[derive(Debug)]
pub enum IngestError {
    InvalidCapacity,
    EmptyCoreEvent,
    InvalidAttributes,
    ReceiptEvidenceMismatch,
    Repeated { sequence: u64 },
    OutOfOrder { expected: u64, received: u64 },
    Backpressure { capacity: usize },
    SequenceExhausted,
    CorruptWatermark,
    CorruptEvent,
    Store(StoreError),
}

impl Display for IngestError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidCapacity => {
                formatter.write_str("event ingestion capacity must be non-zero")
            }
            Self::EmptyCoreEvent => formatter.write_str("core event bytes must not be empty"),
            Self::InvalidAttributes => {
                formatter.write_str("core event filter attributes are invalid")
            }
            Self::ReceiptEvidenceMismatch => {
                formatter.write_str("receipt reference and verification level disagree")
            }
            Self::Repeated { sequence } => {
                write!(formatter, "event sequence {sequence} was repeated")
            }
            Self::OutOfOrder { expected, received } => write!(
                formatter,
                "event sequence {received} arrived out of order; expected {expected}"
            ),
            Self::Backpressure { capacity } => {
                write!(
                    formatter,
                    "event ingestion buffer reached capacity {capacity}"
                )
            }
            Self::SequenceExhausted => formatter.write_str("event sequence space is exhausted"),
            Self::CorruptWatermark => formatter.write_str("durable event watermark is corrupt"),
            Self::CorruptEvent => formatter.write_str("durable event record is corrupt"),
            Self::Store(error) => Display::fmt(error, formatter),
        }
    }
}

impl std::error::Error for IngestError {}

impl From<StoreError> for IngestError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Tenant-scoped bounded event ingestion state.
#[derive(Debug)]
pub struct EventIngestor {
    store: Store,
    tenant: TenantId,
    capacity: usize,
    buffered: VecDeque<CoreEvent>,
    watermark: Watermark,
}

impl EventIngestor {
    /// Opens an ingestor at the durable watermark, or at `initial_sequence`
    /// when the tenant has never ingested an event.
    ///
    /// # Errors
    ///
    /// Refuses a zero capacity, returns `CorruptWatermark` when the persisted watermark is not a
    /// consistent 21-byte record, and returns the store failure otherwise.
    pub fn open(
        store: Store,
        tenant: TenantId,
        capacity: usize,
        initial_sequence: u64,
    ) -> Result<Self, IngestError> {
        if capacity == 0 {
            return Err(IngestError::InvalidCapacity);
        }
        let key = watermark_key(tenant.clone())?;
        let watermark = store
            .get(&key)
            .map(|value| decode_watermark(value.bytes()))
            .transpose()?
            .unwrap_or(Watermark {
                last_ingested: None,
                next_expected: initial_sequence,
            });
        Ok(Self {
            store,
            tenant,
            capacity,
            buffered: VecDeque::with_capacity(capacity),
            watermark,
        })
    }

    /// Returns the configured maximum number of undelivered in-memory events.
    #[must_use]
    pub const fn capacity(&self) -> usize {
        self.capacity
    }

    /// Returns the current number of buffered events.
    #[must_use]
    pub fn buffered_len(&self) -> usize {
        self.buffered.len()
    }

    /// Returns the last durable position and next required sequence.
    #[must_use]
    pub const fn watermark(&self) -> Watermark {
        self.watermark
    }

    /// Removes the oldest buffered event without changing the durable watermark.
    pub fn take_next(&mut self) -> Option<CoreEvent> {
        self.buffered.pop_front()
    }

    /// Borrows the durable store for exact-byte inspection and downstream reads.
    #[must_use]
    pub const fn store(&self) -> &Store {
        &self.store
    }

    /// Returns the tenant whose single ordered stream this ingestor owns.
    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    /// Consumes the ingestor and returns its durable store.
    #[must_use]
    pub fn into_store(self) -> Store {
        self.store
    }
}

pub(super) fn ingest_event(
    ingestor: &mut EventIngestor,
    event: CoreEvent,
) -> Result<(), IngestError> {
    ingest_with_class(ingestor, event, StorageClass::CoreProducedCache)
}

pub(super) fn ingest_local_event(
    ingestor: &mut EventIngestor,
    event: CoreEvent,
) -> Result<(), IngestError> {
    ingest_with_class(ingestor, event, StorageClass::LocalOnly)
}

fn ingest_with_class(
    ingestor: &mut EventIngestor,
    event: CoreEvent,
    class: StorageClass,
) -> Result<(), IngestError> {
    validate_event(&event)?;
    let expected = ingestor.watermark.next_expected;
    if event.global_sequence < expected {
        return Err(IngestError::Repeated {
            sequence: event.global_sequence,
        });
    }
    if event.global_sequence > expected {
        return Err(IngestError::OutOfOrder {
            expected,
            received: event.global_sequence,
        });
    }
    if ingestor.buffered.len() == ingestor.capacity {
        return Err(IngestError::Backpressure {
            capacity: ingestor.capacity,
        });
    }
    let next_expected = expected
        .checked_add(1)
        .ok_or(IngestError::SequenceExhausted)?;
    let next_watermark = Watermark {
        last_ingested: Some(expected),
        next_expected,
    };
    let event_key = event_key(ingestor.tenant.clone(), expected)?;
    let metadata_key = metadata_key(ingestor.tenant.clone(), expected)?;
    let watermark_key = watermark_key(ingestor.tenant.clone())?;
    let ingestion = EventIngestion {
        event_key,
        event_bytes: event.canonical_bytes.clone(),
        metadata_key,
        metadata: encode_metadata(&event),
        watermark_key,
        watermark: encode_watermark(next_watermark),
    };
    match class {
        StorageClass::CoreProducedCache => ingestor.store.record_event(ingestion)?,
        StorageClass::LocalOnly => ingestor.store.record_local_event(ingestion)?,
    }
    ingestor.watermark = next_watermark;
    ingestor.buffered.push_back(event);
    Ok(())
}

pub(super) fn durable_sequences(store: &Store, tenant: &TenantId) -> Result<Vec<u64>, IngestError> {
    store
        .list_object_ids(tenant, ObjectKind::Event)
        .into_iter()
        .map(|object_id| {
            let encoded: [u8; 8] = object_id
                .try_into()
                .map_err(|_| IngestError::CorruptEvent)?;
            Ok(u64::from_be_bytes(encoded))
        })
        .collect()
}

pub(super) fn durable_event(
    store: &Store,
    tenant: &TenantId,
    sequence: u64,
) -> Result<CoreEvent, IngestError> {
    let event_key = event_key(tenant.clone(), sequence)?;
    let stored_event = store.get(&event_key).ok_or(IngestError::CorruptEvent)?;
    if !matches!(
        stored_event.class(),
        StorageClass::CoreProducedCache | StorageClass::LocalOnly
    ) || stored_event.bytes().is_empty()
    {
        return Err(IngestError::CorruptEvent);
    }
    let metadata = store
        .get(&metadata_key(tenant.clone(), sequence)?)
        .ok_or(IngestError::CorruptEvent)?;
    if metadata.class() != StorageClass::LocalOnly {
        return Err(IngestError::CorruptEvent);
    }
    let (receipt_reference, receipt_verification_level, attributes) =
        decode_metadata(metadata.bytes())?;
    Ok(CoreEvent {
        global_sequence: sequence,
        canonical_bytes: stored_event.bytes().to_vec(),
        receipt_reference,
        receipt_verification_level,
        attributes,
    })
}

fn validate_event(event: &CoreEvent) -> Result<(), IngestError> {
    if event.canonical_bytes.is_empty() {
        return Err(IngestError::EmptyCoreEvent);
    }
    if [
        event.attributes.agent.as_str(),
        event.attributes.account.as_str(),
        event.attributes.module.as_str(),
        event.attributes.asset.as_str(),
        event.attributes.counterparty.as_str(),
    ]
    .iter()
    .any(|value| value.is_empty() || value.len() > 1024 || value.as_bytes().contains(&0))
    {
        return Err(IngestError::InvalidAttributes);
    }
    let has_receipt = event.receipt_reference.is_some();
    let is_verified = event.receipt_verification_level != VerificationLevel::UNVERIFIED;
    if has_receipt != is_verified {
        return Err(IngestError::ReceiptEvidenceMismatch);
    }
    Ok(())
}

fn event_key(tenant: TenantId, sequence: u64) -> Result<TenantKey, StoreError> {
    TenantKey::new(tenant, ObjectKind::Event, sequence.to_be_bytes().to_vec())
}

fn metadata_key(tenant: TenantId, sequence: u64) -> Result<TenantKey, StoreError> {
    let mut object_id = METADATA_PREFIX.to_vec();
    object_id.extend_from_slice(&sequence.to_be_bytes());
    TenantKey::new(tenant, ObjectKind::Configuration, object_id)
}

fn watermark_key(tenant: TenantId) -> Result<TenantKey, StoreError> {
    TenantKey::new(tenant, ObjectKind::Configuration, WATERMARK_ID.to_vec())
}

fn encode_watermark(watermark: Watermark) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(21);
    bytes.extend_from_slice(WATERMARK_MAGIC);
    bytes.push(u8::from(watermark.last_ingested.is_some()));
    bytes.extend_from_slice(&watermark.last_ingested.unwrap_or(0).to_be_bytes());
    bytes.extend_from_slice(&watermark.next_expected.to_be_bytes());
    bytes
}

fn decode_watermark(bytes: &[u8]) -> Result<Watermark, IngestError> {
    if bytes.len() != 21 || &bytes[..4] != WATERMARK_MAGIC || bytes[4] > 1 {
        return Err(IngestError::CorruptWatermark);
    }
    let mut last = [0_u8; 8];
    last.copy_from_slice(&bytes[5..13]);
    let mut next = [0_u8; 8];
    next.copy_from_slice(&bytes[13..21]);
    let last_ingested = (bytes[4] == 1).then(|| u64::from_be_bytes(last));
    let next_expected = u64::from_be_bytes(next);
    if last_ingested.and_then(|last| last.checked_add(1)) != Some(next_expected) {
        return Err(IngestError::CorruptWatermark);
    }
    Ok(Watermark {
        last_ingested,
        next_expected,
    })
}

fn encode_metadata(event: &CoreEvent) -> Vec<u8> {
    let mut bytes = Vec::new();
    bytes.extend_from_slice(METADATA_MAGIC);
    bytes.push(event.receipt_verification_level.wire_rank());
    bytes.push(u8::from(event.receipt_reference.is_some()));
    bytes.extend_from_slice(&event.receipt_reference.unwrap_or([0_u8; 32]));
    push_metadata_string(&mut bytes, &event.attributes.agent);
    push_metadata_string(&mut bytes, &event.attributes.account);
    bytes.extend_from_slice(&event.attributes.activity_type.to_be_bytes());
    push_metadata_string(&mut bytes, &event.attributes.module);
    push_metadata_string(&mut bytes, &event.attributes.asset);
    push_metadata_string(&mut bytes, &event.attributes.counterparty);
    bytes.extend_from_slice(&event.attributes.result_code.to_be_bytes());
    bytes
}

fn decode_metadata(
    bytes: &[u8],
) -> Result<(Option<[u8; 32]>, VerificationLevel, EventAttributes), IngestError> {
    if bytes.len() < 38 || &bytes[..4] != METADATA_MAGIC || bytes[5] > 1 {
        return Err(IngestError::CorruptEvent);
    }
    let level = match bytes[4] {
        0 => VerificationLevel::UNVERIFIED,
        1 => VerificationLevel::SEQUENCER_SIGNED,
        2 => VerificationLevel::BATCH_INCLUDED,
        3 => VerificationLevel::STATE_PROVEN,
        4 => VerificationLevel::CHECKPOINT_FINALISED,
        5 => VerificationLevel::SETTLEMENT_ANCHORED,
        _ => return Err(IngestError::CorruptEvent),
    };
    let mut reference = [0_u8; 32];
    reference.copy_from_slice(&bytes[6..38]);
    let receipt_reference = (bytes[5] == 1).then_some(reference);
    let has_receipt = receipt_reference.is_some();
    let is_verified = level != VerificationLevel::UNVERIFIED;
    if has_receipt != is_verified {
        return Err(IngestError::CorruptEvent);
    }
    let mut decoder = MetadataDecoder::new(&bytes[38..]);
    let attributes = EventAttributes {
        agent: decoder.string()?,
        account: decoder.string()?,
        activity_type: decoder.u16()?,
        module: decoder.string()?,
        asset: decoder.string()?,
        counterparty: decoder.string()?,
        result_code: decoder.i32()?,
    };
    if !decoder.is_empty() {
        return Err(IngestError::CorruptEvent);
    }
    let event = CoreEvent {
        global_sequence: 0,
        canonical_bytes: vec![1],
        receipt_reference,
        receipt_verification_level: level,
        attributes: attributes.clone(),
    };
    validate_event(&event).map_err(|_| IngestError::CorruptEvent)?;
    Ok((receipt_reference, level, attributes))
}

fn push_metadata_string(bytes: &mut Vec<u8>, value: &str) {
    let length = u16::try_from(value.len()).unwrap_or(u16::MAX);
    bytes.extend_from_slice(&length.to_be_bytes());
    bytes.extend_from_slice(value.as_bytes());
}

struct MetadataDecoder<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> MetadataDecoder<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], IngestError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(IngestError::CorruptEvent)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(IngestError::CorruptEvent)?;
        self.offset = end;
        Ok(value)
    }

    fn u16(&mut self) -> Result<u16, IngestError> {
        let mut value = [0_u8; 2];
        value.copy_from_slice(self.take(2)?);
        Ok(u16::from_be_bytes(value))
    }

    fn i32(&mut self) -> Result<i32, IngestError> {
        let mut value = [0_u8; 4];
        value.copy_from_slice(self.take(4)?);
        Ok(i32::from_be_bytes(value))
    }

    fn string(&mut self) -> Result<String, IngestError> {
        let length = usize::from(self.u16()?);
        let value =
            std::str::from_utf8(self.take(length)?).map_err(|_| IngestError::CorruptEvent)?;
        Ok(value.to_owned())
    }

    const fn is_empty(&self) -> bool {
        self.offset == self.bytes.len()
    }
}
