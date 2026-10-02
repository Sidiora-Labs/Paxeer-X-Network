//! Durable exact-byte submission outbox and explicit state machine.

use std::collections::BTreeMap;

use crate::prepare::PreparationAuthorization;
use crate::protocol_evidence::VerifiedReceiptEvidence;
use crate::session::{SessionId, SessionRef};
use crate::sign::VerifiedSubmission;
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

#[path = "unknown.rs"]
mod resolution;
#[path = "recover.rs"]
mod restart;

pub use resolution::{
    resolve_unknown, ReceiptLookup, ResendObservation, ResolutionObservation, UnknownAge,
    UnknownBoundaryError, UnknownResolution, UnknownResolutionError,
};
pub use restart::{
    recover, RecoveredOutbox, RecoveryError, RecoveryInputs, UnknownCeilingReservation,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SubmissionState {
    Prepared,
    Signed,
    Queued,
    Submitted,
    Acknowledged,
    Unknown,
    Executed,
    Failed,
    Expired,
    Superseded,
}

impl SubmissionState {
    #[must_use]
    pub const fn terminal(self) -> bool {
        matches!(
            self,
            Self::Executed | Self::Failed | Self::Expired | Self::Superseded
        )
    }

    const fn code(self) -> u8 {
        match self {
            Self::Prepared => 1,
            Self::Signed => 2,
            Self::Queued => 3,
            Self::Submitted => 4,
            Self::Acknowledged => 5,
            Self::Unknown => 6,
            Self::Executed => 7,
            Self::Failed => 8,
            Self::Expired => 9,
            Self::Superseded => 10,
        }
    }

    fn from_code(code: u8) -> Result<Self, OutboxError> {
        match code {
            1 => Ok(Self::Prepared),
            2 => Ok(Self::Signed),
            3 => Ok(Self::Queued),
            4 => Ok(Self::Submitted),
            5 => Ok(Self::Acknowledged),
            6 => Ok(Self::Unknown),
            7 => Ok(Self::Executed),
            8 => Ok(Self::Failed),
            9 => Ok(Self::Expired),
            10 => Ok(Self::Superseded),
            _ => Err(OutboxError::Corrupt),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReceiptEvidence {
    receipt_ref: [u8; 32],
}

impl ReceiptEvidence {
    #[must_use]
    pub const fn receipt_ref(self) -> [u8; 32] {
        self.receipt_ref
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StateTransition {
    pub from: SubmissionState,
    pub to: SubmissionState,
    pub cause: String,
    pub receipt: Option<ReceiptEvidence>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubmissionStatus {
    pub submission_id: [u8; 32],
    pub state: SubmissionState,
    pub activity_id: [u8; 32],
    pub evidence: Option<ReceiptEvidence>,
    pub transitions: Vec<StateTransition>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct OutboxRecord {
    tenant: TenantId,
    status: SubmissionStatus,
    signed_canonical_bytes: Vec<u8>,
    origin: Option<PreparationAuthorization>,
}

#[derive(Default)]
pub struct Outbox {
    records: BTreeMap<[u8; 32], OutboxRecord>,
}

impl Outbox {
    /// Durably queues exact verified bytes before they can be obtained for transport.
    ///
    /// # Errors
    ///
    /// Returns `Duplicate` for an already-tracked submission identifier, `IdempotencyMismatch` when
    /// the signed audit key differs from it, `Corrupt` when the record exceeds the `u32` length
    /// prefixes, or `Store` when the three durable records cannot be written atomically.
    pub fn enqueue(
        &mut self,
        store: &mut Store,
        tenant: TenantId,
        submission_id: [u8; 32],
        verified: VerifiedSubmission,
        origin: Option<PreparationAuthorization>,
    ) -> Result<(), OutboxError> {
        if self.records.contains_key(&submission_id) {
            return Err(OutboxError::Duplicate);
        }
        if verified.idempotency_key() != submission_id {
            return Err(OutboxError::IdempotencyMismatch);
        }
        let activity_id = verified.activity_id();
        let signed_canonical_bytes = verified.into_exact_bytes();
        let transitions = vec![
            StateTransition {
                from: SubmissionState::Prepared,
                to: SubmissionState::Signed,
                cause: "exact signature verified".to_owned(),
                receipt: None,
            },
            StateTransition {
                from: SubmissionState::Signed,
                to: SubmissionState::Queued,
                cause: "durable outbox record created".to_owned(),
                receipt: None,
            },
        ];
        let record = OutboxRecord {
            tenant: tenant.clone(),
            status: SubmissionStatus {
                submission_id,
                state: SubmissionState::Queued,
                activity_id,
                evidence: None,
                transitions,
            },
            signed_canonical_bytes: signed_canonical_bytes.clone(),
            origin,
        };
        store
            .record_submission(
                tenant,
                submission_id.to_vec(),
                signed_canonical_bytes,
                encode_record(&record)?,
            )
            .map_err(OutboxError::Store)?;
        self.records.insert(submission_id, record);
        Ok(())
    }

    /// Restores one durable record by its tenant-scoped idempotency key.
    ///
    /// # Errors
    ///
    /// Returns `NotFound` when no durable outbox record exists, `Corrupt` for a missing signed-bytes
    /// record, undecodable state or a record naming a different submission, and `Store` when either
    /// tenant key cannot be built.
    pub fn restore(
        &mut self,
        store: &Store,
        tenant: TenantId,
        submission_id: [u8; 32],
    ) -> Result<(), OutboxError> {
        let outbox_key = TenantKey::new(tenant.clone(), ObjectKind::Outbox, submission_id.to_vec())
            .map_err(OutboxError::Store)?;
        let signed_key = TenantKey::new(
            tenant.clone(),
            ObjectKind::PreparedActivity,
            submission_id.to_vec(),
        )
        .map_err(OutboxError::Store)?;
        let encoded = store.get(&outbox_key).ok_or(OutboxError::NotFound)?;
        let signed = store.get(&signed_key).ok_or(OutboxError::Corrupt)?;
        let mut record = decode_record(encoded.bytes(), tenant)?;
        if record.status.submission_id != submission_id {
            return Err(OutboxError::Corrupt);
        }
        record.signed_canonical_bytes = signed.bytes().to_vec();
        self.records.insert(submission_id, record);
        Ok(())
    }

    /// Returns the session generation that authorized the durable submission, if one was recorded.
    ///
    /// # Errors
    ///
    /// Returns `NotFound` for a submission this outbox never enqueued or restored.
    pub(crate) fn origin(
        &self,
        submission_id: [u8; 32],
    ) -> Result<Option<PreparationAuthorization>, OutboxError> {
        self.records
            .get(&submission_id)
            .map(|record| record.origin.clone())
            .ok_or(OutboxError::NotFound)
    }

    /// Returns exact stored bytes only after the queued record is durable.
    ///
    /// # Errors
    ///
    /// Returns `NotFound` for a submission this outbox never enqueued or restored, and `NotQueued`
    /// once it has left `Queued` for any later state.
    pub fn bytes_for_transmission(&self, submission_id: [u8; 32]) -> Result<&[u8], OutboxError> {
        let record = self
            .records
            .get(&submission_id)
            .ok_or(OutboxError::NotFound)?;
        if record.status.state != SubmissionState::Queued {
            return Err(OutboxError::NotQueued);
        }
        Ok(&record.signed_canonical_bytes)
    }

    /// Returns the exact durable signed activity at any later lifecycle state.
    ///
    /// # Errors
    ///
    /// Returns an error if the submission does not exist or its retained bytes are invalid.
    pub fn exact_signed_bytes(&self, submission_id: [u8; 32]) -> Result<&[u8], OutboxError> {
        self.records
            .get(&submission_id)
            .map(|record| record.signed_canonical_bytes.as_slice())
            .ok_or(OutboxError::NotFound)
    }

    /// Applies one legal state change and durably records it with its cause.
    ///
    /// # Errors
    ///
    /// Returns `EmptyCause` for a blank cause, `NotFound` for an untracked submission,
    /// `InvalidTransition` for a move `legal_transition` forbids, `SuccessWithoutVerifiedReceipt`
    /// for `Executed` without verified evidence, `ReceiptMismatch` for evidence naming another
    /// activity, and `Corrupt` or `Store` when the updated record cannot be encoded or persisted.
    pub fn transition(
        &mut self,
        store: &mut Store,
        submission_id: [u8; 32],
        to: SubmissionState,
        cause: impl Into<String>,
        receipt: Option<VerifiedReceiptEvidence>,
    ) -> Result<SubmissionStatus, OutboxError> {
        let cause = cause.into();
        if cause.is_empty() {
            return Err(OutboxError::EmptyCause);
        }
        let current = self
            .records
            .get(&submission_id)
            .cloned()
            .ok_or(OutboxError::NotFound)?;
        let from = current.status.state;
        if !legal_transition(from, to) {
            return Err(OutboxError::InvalidTransition { from, to });
        }
        if to == SubmissionState::Executed && receipt.is_none() {
            return Err(OutboxError::SuccessWithoutVerifiedReceipt);
        }
        if receipt
            .as_ref()
            .is_some_and(|evidence| evidence.activity_id() != current.status.activity_id)
        {
            return Err(OutboxError::ReceiptMismatch);
        }
        let receipt = receipt.map(|evidence| ReceiptEvidence {
            receipt_ref: evidence.receipt_ref(),
        });
        let mut updated = current;
        updated.status.state = to;
        updated.status.evidence = receipt;
        updated.status.transitions.push(StateTransition {
            from,
            to,
            cause,
            receipt,
        });
        let key = TenantKey::new(
            updated.tenant.clone(),
            ObjectKind::Outbox,
            submission_id.to_vec(),
        )
        .map_err(OutboxError::Store)?;
        store
            .put_local(key, encode_record(&updated)?)
            .map_err(OutboxError::Store)?;
        let status = updated.status.clone();
        self.records.insert(submission_id, updated);
        Ok(status)
    }

    #[must_use]
    pub fn status(&self, submission_id: [u8; 32]) -> Option<&SubmissionStatus> {
        self.records
            .get(&submission_id)
            .map(|record| &record.status)
    }
}

#[derive(Debug)]
pub enum OutboxError {
    Duplicate,
    IdempotencyMismatch,
    NotFound,
    NotQueued,
    EmptyCause,
    Corrupt,
    SuccessWithoutVerifiedReceipt,
    ReceiptMismatch,
    InvalidTransition {
        from: SubmissionState,
        to: SubmissionState,
    },
    Store(StoreError),
}

fn legal_transition(from: SubmissionState, to: SubmissionState) -> bool {
    matches!(
        (from, to),
        (
            SubmissionState::Queued,
            SubmissionState::Submitted | SubmissionState::Expired | SubmissionState::Superseded,
        ) | (
            SubmissionState::Submitted,
            SubmissionState::Acknowledged | SubmissionState::Unknown,
        ) | (
            SubmissionState::Acknowledged,
            SubmissionState::Unknown | SubmissionState::Executed | SubmissionState::Failed,
        ) | (
            SubmissionState::Unknown,
            SubmissionState::Executed | SubmissionState::Failed | SubmissionState::Superseded,
        )
    )
}

fn encode_record(record: &OutboxRecord) -> Result<Vec<u8>, OutboxError> {
    let mut bytes = Vec::new();
    bytes.extend_from_slice(b"LXOB");
    bytes.push(3);
    bytes.extend_from_slice(&record.status.submission_id);
    bytes.push(record.status.state.code());
    bytes.extend_from_slice(&record.status.activity_id);
    encode_receipt(&mut bytes, record.status.evidence);
    push_u32(&mut bytes, record.status.transitions.len())?;
    for transition in &record.status.transitions {
        bytes.push(transition.from.code());
        bytes.push(transition.to.code());
        push_bytes(&mut bytes, transition.cause.as_bytes())?;
        encode_receipt(&mut bytes, transition.receipt);
    }
    match &record.origin {
        Some(origin) => {
            bytes.push(1);
            push_bytes(&mut bytes, origin.session.tenant.as_str().as_bytes())?;
            bytes.extend_from_slice(&origin.session.session_id.0);
            bytes.extend_from_slice(&origin.generation.to_be_bytes());
        }
        None => bytes.push(0),
    }
    Ok(bytes)
}

fn decode_record(bytes: &[u8], tenant: TenantId) -> Result<OutboxRecord, OutboxError> {
    let mut decoder = RecordDecoder { bytes, offset: 0 };
    if decoder.take(4)? != b"LXOB" {
        return Err(OutboxError::Corrupt);
    }
    let version = decoder.u8()?;
    if version != 2 && version != 3 {
        return Err(OutboxError::Corrupt);
    }
    let submission_id = decoder.fixed()?;
    let state = SubmissionState::from_code(decoder.u8()?)?;
    let activity_id = decoder.fixed()?;
    let evidence = decoder.receipt()?;
    let transition_count = decoder.u32()? as usize;
    if transition_count > 1_024 {
        return Err(OutboxError::Corrupt);
    }
    let mut transitions = Vec::with_capacity(transition_count);
    for _ in 0..transition_count {
        let from = SubmissionState::from_code(decoder.u8()?)?;
        let to = SubmissionState::from_code(decoder.u8()?)?;
        let cause =
            String::from_utf8(decoder.bytes()?.to_vec()).map_err(|_| OutboxError::Corrupt)?;
        let receipt = decoder.receipt()?;
        transitions.push(StateTransition {
            from,
            to,
            cause,
            receipt,
        });
    }
    let origin = if version == 3 {
        decoder.origin()?
    } else {
        None
    };
    if decoder.offset != bytes.len() {
        return Err(OutboxError::Corrupt);
    }
    Ok(OutboxRecord {
        tenant,
        status: SubmissionStatus {
            submission_id,
            state,
            activity_id,
            evidence,
            transitions,
        },
        signed_canonical_bytes: Vec::new(),
        origin,
    })
}

fn encode_receipt(bytes: &mut Vec<u8>, receipt: Option<ReceiptEvidence>) {
    match receipt {
        Some(receipt) => {
            bytes.push(1);
            bytes.extend_from_slice(&receipt.receipt_ref);
        }
        None => bytes.push(0),
    }
}

fn push_u32(bytes: &mut Vec<u8>, value: usize) -> Result<(), OutboxError> {
    let value = u32::try_from(value).map_err(|_| OutboxError::Corrupt)?;
    bytes.extend_from_slice(&value.to_be_bytes());
    Ok(())
}

fn push_bytes(bytes: &mut Vec<u8>, value: &[u8]) -> Result<(), OutboxError> {
    push_u32(bytes, value.len())?;
    bytes.extend_from_slice(value);
    Ok(())
}

struct RecordDecoder<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> RecordDecoder<'a> {
    fn take(&mut self, length: usize) -> Result<&'a [u8], OutboxError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(OutboxError::Corrupt)?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(OutboxError::Corrupt)?;
        self.offset = end;
        Ok(value)
    }

    fn u8(&mut self) -> Result<u8, OutboxError> {
        Ok(*self.take(1)?.first().ok_or(OutboxError::Corrupt)?)
    }

    fn u32(&mut self) -> Result<u32, OutboxError> {
        let mut value = [0; 4];
        value.copy_from_slice(self.take(4)?);
        Ok(u32::from_be_bytes(value))
    }

    fn fixed(&mut self) -> Result<[u8; 32], OutboxError> {
        let mut value = [0; 32];
        value.copy_from_slice(self.take(32)?);
        Ok(value)
    }

    fn bytes(&mut self) -> Result<&'a [u8], OutboxError> {
        let length = self.u32()? as usize;
        if length > 1_048_576 {
            return Err(OutboxError::Corrupt);
        }
        self.take(length)
    }

    fn u64(&mut self) -> Result<u64, OutboxError> {
        let mut value = [0; 8];
        value.copy_from_slice(self.take(8)?);
        Ok(u64::from_be_bytes(value))
    }

    fn origin(&mut self) -> Result<Option<PreparationAuthorization>, OutboxError> {
        match self.u8()? {
            0 => Ok(None),
            1 => {
                let tenant = std::str::from_utf8(self.bytes()?)
                    .map_err(|_| OutboxError::Corrupt)
                    .and_then(|text| TenantId::new(text).map_err(|_| OutboxError::Corrupt))?;
                let session_id = SessionId(self.fixed()?);
                let generation = self.u64()?;
                Ok(Some(PreparationAuthorization {
                    session: SessionRef::new(tenant, session_id),
                    generation,
                }))
            }
            _ => Err(OutboxError::Corrupt),
        }
    }

    fn receipt(&mut self) -> Result<Option<ReceiptEvidence>, OutboxError> {
        match self.u8()? {
            0 => Ok(None),
            1 => Ok(Some(ReceiptEvidence {
                receipt_ref: self.fixed()?,
            })),
            _ => Err(OutboxError::Corrupt),
        }
    }
}

#[cfg(test)]
mod origin_codec_tests {
    use super::{
        decode_record, encode_record, OutboxRecord, PreparationAuthorization, ReceiptEvidence,
        SessionId, SessionRef, StateTransition, SubmissionState, SubmissionStatus, TenantId,
    };

    fn tenant() -> TenantId {
        TenantId::new("tenant-a").unwrap_or_else(|error| panic!("tenant: {error}"))
    }

    fn status() -> SubmissionStatus {
        SubmissionStatus {
            submission_id: [7; 32],
            state: SubmissionState::Queued,
            activity_id: [9; 32],
            evidence: None,
            transitions: vec![
                StateTransition {
                    from: SubmissionState::Prepared,
                    to: SubmissionState::Signed,
                    cause: "exact signature verified".to_owned(),
                    receipt: None,
                },
                StateTransition {
                    from: SubmissionState::Signed,
                    to: SubmissionState::Queued,
                    cause: "durable outbox record created".to_owned(),
                    receipt: Some(ReceiptEvidence {
                        receipt_ref: [4; 32],
                    }),
                },
            ],
        }
    }

    #[test]
    fn version_three_round_trips_origin() {
        let record = OutboxRecord {
            tenant: tenant(),
            status: status(),
            signed_canonical_bytes: Vec::new(),
            origin: Some(PreparationAuthorization {
                session: SessionRef::new(tenant(), SessionId([3; 32])),
                generation: 0x0102_0304_0506_0708,
            }),
        };
        let encoded = encode_record(&record).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(&encoded[..5], b"LXOB\x03");
        let decoded =
            decode_record(&encoded, tenant()).unwrap_or_else(|error| panic!("decode: {error:?}"));
        assert_eq!(decoded, record);

        let mut trailing = encoded.clone();
        trailing.push(0);
        assert!(decode_record(&trailing, tenant()).is_err());
        let mut unknown_version = encoded;
        unknown_version[4] = 4;
        assert!(decode_record(&unknown_version, tenant()).is_err());
    }

    #[test]
    fn version_two_layout_decodes_without_origin() {
        let status = status();
        let mut v2 = Vec::new();
        v2.extend_from_slice(b"LXOB");
        v2.push(2);
        v2.extend_from_slice(&status.submission_id);
        v2.push(3);
        v2.extend_from_slice(&status.activity_id);
        v2.push(0);
        v2.extend_from_slice(&2_u32.to_be_bytes());
        for (from, to, cause, receipt) in [
            (1_u8, 2_u8, "exact signature verified", None),
            (2, 3, "durable outbox record created", Some([4_u8; 32])),
        ] {
            v2.push(from);
            v2.push(to);
            let length = u32::try_from(cause.len()).unwrap_or_else(|error| panic!("{error}"));
            v2.extend_from_slice(&length.to_be_bytes());
            v2.extend_from_slice(cause.as_bytes());
            match receipt {
                Some(reference) => {
                    v2.push(1);
                    v2.extend_from_slice(&reference);
                }
                None => v2.push(0),
            }
        }
        let decoded = decode_record(&v2, tenant()).unwrap_or_else(|error| panic!("v2: {error:?}"));
        assert_eq!(
            decoded,
            OutboxRecord {
                tenant: tenant(),
                status,
                signed_canonical_bytes: Vec::new(),
                origin: None,
            }
        );
        let mut with_origin_tag = v2;
        with_origin_tag.push(0);
        assert!(decode_record(&with_origin_tag, tenant()).is_err());
    }
}
