use std::collections::BTreeMap;
use std::fmt::{Display, Formatter};
use std::path::Path;

use layerx_types::ids::{ActivityId, Did, IdempotencyKey};
use layerx_types::verify::VerificationLevel;
use sha2::{Digest, Sha256};

use crate::identity::ProtocolAuthority;
use crate::session::SessionId;
use crate::store::TenantId;

use super::log::{read_payloads, AppendReceipt, AuditError, Log};
use super::redaction::{PayloadEvidence, Redacted, RedactionError};

const ENTRY_MAGIC: &[u8; 4] = b"LXAR";
const ENTRY_VERSION: u8 = 2;
const LEGACY_ENTRY_VERSION: u8 = 1;
const MAX_TEXT_BYTES: usize = 4_096;

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
#[repr(u8)]
pub enum EventClass {
    Authentication = 1,
    SessionLifecycle = 2,
    CapabilityDecision = 3,
    PolicyDecision = 4,
    BudgetReservation = 5,
    Preparation = 6,
    SignatureRequest = 7,
    Submission = 8,
    TerminalOutcome = 9,
    SubscriptionChange = 10,
    ConfigurationChange = 11,
    AdministrativeAction = 12,
    MutationAttempt = 13,
}

impl EventClass {
    pub const ALL: [Self; 13] = [
        Self::Authentication,
        Self::SessionLifecycle,
        Self::CapabilityDecision,
        Self::PolicyDecision,
        Self::BudgetReservation,
        Self::Preparation,
        Self::SignatureRequest,
        Self::Submission,
        Self::TerminalOutcome,
        Self::SubscriptionChange,
        Self::ConfigurationChange,
        Self::AdministrativeAction,
        Self::MutationAttempt,
    ];

    fn from_byte(value: u8) -> Result<Self, RecordError> {
        Self::ALL
            .into_iter()
            .find(|class| *class as u8 == value)
            .ok_or(RecordError::Decode("unknown audit event class"))
    }

    const fn index(self) -> usize {
        self as usize - 1
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Decision {
    Allowed = 1,
    Refused = 2,
    Observed = 3,
    Requested = 4,
    Submitted = 5,
    Executed = 6,
    Failed = 7,
    Changed = 8,
}

impl Decision {
    fn from_byte(value: u8) -> Result<Self, RecordError> {
        match value {
            1 => Ok(Self::Allowed),
            2 => Ok(Self::Refused),
            3 => Ok(Self::Observed),
            4 => Ok(Self::Requested),
            5 => Ok(Self::Submitted),
            6 => Ok(Self::Executed),
            7 => Ok(Self::Failed),
            8 => Ok(Self::Changed),
            _ => Err(RecordError::Decode("unknown audit decision")),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Entry {
    pub class: EventClass,
    pub observed_at_ms: u64,
    pub tenant: TenantId,
    pub agent: Did,
    pub session: Option<SessionId>,
    pub capability: Option<[u8; 32]>,
    pub policy_version: String,
    pub request_id: [u8; 32],
    pub idempotency_key: Option<IdempotencyKey>,
    pub decision: Decision,
    pub reason: Redacted,
    pub resulting_activity_id: Option<ActivityId>,
    pub verification_level: VerificationLevel,
    pub protocol_authority: Option<ProtocolAuthority>,
    pub submitted_bytes: Option<PayloadEvidence>,
    pub receipt_id: Option<[u8; 32]>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Coverage {
    counts: [u64; 13],
}

impl Coverage {
    #[must_use]
    pub fn count(&self, class: EventClass) -> u64 {
        self.counts[class.index()]
    }

    #[must_use]
    pub fn missing(&self) -> Vec<EventClass> {
        EventClass::ALL
            .into_iter()
            .filter(|class| self.count(*class) == 0)
            .collect()
    }

    /// Requires at least one recorded entry for every audited event class.
    ///
    /// # Errors
    ///
    /// Returns `IncompleteCoverage` naming every class without a recorded entry.
    pub fn require_complete(&self) -> Result<(), RecordError> {
        let missing = self.missing();
        if missing.is_empty() {
            Ok(())
        } else {
            Err(RecordError::IncompleteCoverage(missing))
        }
    }

    #[must_use]
    pub fn from_entries(entries: &[Entry]) -> Self {
        let mut coverage = Self::default();
        for entry in entries {
            coverage.observe(entry.class);
        }
        coverage
    }

    fn observe(&mut self, class: EventClass) {
        let count = &mut self.counts[class.index()];
        *count = count.saturating_add(1);
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecisionEvidence {
    pub class: EventClass,
    pub decision: Decision,
    pub reason: Redacted,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Reconstruction {
    pub decisions: Vec<DecisionEvidence>,
    pub submitted_bytes: Vec<u8>,
    pub protocol_authority: ProtocolAuthority,
    pub resulting_activity_id: ActivityId,
    pub core_receipt_bytes: Vec<u8>,
    pub verification_level: VerificationLevel,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StoredReceiptEvidence {
    pub submitted_bytes: Vec<u8>,
    pub core_receipt_bytes: Vec<u8>,
}

#[derive(Debug)]
pub enum RecordError {
    Audit(AuditError),
    Invalid(&'static str),
    Decode(&'static str),
    Redaction(RedactionError),
    IncompleteCoverage(Vec<EventClass>),
    IncompleteReconstruction(&'static str),
}

impl Display for RecordError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Audit(error) => write!(formatter, "{error}"),
            Self::Invalid(reason) => write!(formatter, "invalid audit entry: {reason}"),
            Self::Decode(reason) => write!(formatter, "cannot decode audit entry: {reason}"),
            Self::Redaction(error) => write!(formatter, "cannot redact audit entry: {error}"),
            Self::IncompleteCoverage(classes) => {
                write!(formatter, "unaudited event classes: {classes:?}")
            }
            Self::IncompleteReconstruction(reason) => {
                write!(formatter, "cannot reconstruct audited session: {reason}")
            }
        }
    }
}

impl std::error::Error for RecordError {}

impl From<AuditError> for RecordError {
    fn from(value: AuditError) -> Self {
        Self::Audit(value)
    }
}

impl From<RedactionError> for RecordError {
    fn from(value: RedactionError) -> Self {
        Self::Redaction(value)
    }
}

/// Appends the audit entry durably before the operation runs.
///
/// # Errors
///
/// Returns `Invalid` for an empty, oversized, or evidence-incomplete entry and an
/// audit error when the chain cannot be verified or appended; the operation does
/// not run in either case.
pub fn record<T>(
    log: &mut Log,
    coverage: &mut Coverage,
    entry: &Entry,
    operation: impl FnOnce() -> T,
) -> Result<(AppendReceipt, T), RecordError> {
    let encoded = encode_entry(entry)?;
    let output = log.before_operation(&encoded, || {
        coverage.observe(entry.class);
        operation()
    })?;
    Ok(output)
}

/// Reads every entry from the verified audit chain at `path`.
///
/// # Errors
///
/// Returns an audit error when the chain cannot be verified or read and `Decode`
/// when a payload is not a well-formed audit entry.
pub fn read_entries(path: impl AsRef<Path>) -> Result<Vec<Entry>, RecordError> {
    read_payloads(path)?
        .into_iter()
        .map(|payload| decode_entry(&payload))
        .collect()
}

/// Rebuilds one session's decisions and evidence from audited entries.
///
/// # Errors
///
/// Returns `IncompleteReconstruction` when the submission or terminal record is
/// absent, ambiguous, or missing its evidence, the referenced receipt is not
/// stored, or the stored submission does not match the audited digest.
pub fn reconstruct_session(
    entries: &[Entry],
    receipts: &BTreeMap<[u8; 32], StoredReceiptEvidence>,
    session_id: SessionId,
    idempotency_key: IdempotencyKey,
) -> Result<Reconstruction, RecordError> {
    let mut decisions = Vec::new();
    let mut submission: Option<([u8; 32], &ProtocolAuthority)> = None;
    let mut terminal: Option<(ActivityId, [u8; 32], VerificationLevel)> = None;
    for entry in entries.iter().filter(|entry| {
        entry.session == Some(session_id) && entry.idempotency_key == Some(idempotency_key)
    }) {
        decisions.push(DecisionEvidence {
            class: entry.class,
            decision: entry.decision,
            reason: entry.reason.clone(),
        });
        if entry.class == EventClass::Submission {
            let digest = entry
                .submitted_bytes
                .as_ref()
                .and_then(PayloadEvidence::digest)
                .ok_or(RecordError::IncompleteReconstruction(
                    "submission digest is absent",
                ))?;
            let authority =
                entry
                    .protocol_authority
                    .as_ref()
                    .ok_or(RecordError::IncompleteReconstruction(
                        "protocol authority is absent",
                    ))?;
            if submission.replace((digest, authority)).is_some() {
                return Err(RecordError::IncompleteReconstruction(
                    "multiple submission records are ambiguous",
                ));
            }
        }
        if entry.class == EventClass::TerminalOutcome {
            let activity_id =
                entry
                    .resulting_activity_id
                    .ok_or(RecordError::IncompleteReconstruction(
                        "terminal activity identifier is absent",
                    ))?;
            let receipt_id = entry
                .receipt_id
                .ok_or(RecordError::IncompleteReconstruction(
                    "terminal receipt reference is absent",
                ))?;
            if terminal
                .replace((activity_id, receipt_id, entry.verification_level))
                .is_some()
            {
                return Err(RecordError::IncompleteReconstruction(
                    "multiple terminal records are ambiguous",
                ));
            }
        }
    }
    let (submitted_digest, protocol_authority) = submission.ok_or(
        RecordError::IncompleteReconstruction("submission record is absent"),
    )?;
    let (resulting_activity_id, receipt_id, verification_level) = terminal.ok_or(
        RecordError::IncompleteReconstruction("terminal record is absent"),
    )?;
    let stored = receipts
        .get(&receipt_id)
        .ok_or(RecordError::IncompleteReconstruction(
            "referenced core receipt is absent",
        ))?;
    let observed_digest: [u8; 32] = Sha256::digest(&stored.submitted_bytes).into();
    if observed_digest != submitted_digest {
        return Err(RecordError::IncompleteReconstruction(
            "stored submission does not match the audit digest",
        ));
    }
    Ok(Reconstruction {
        decisions,
        submitted_bytes: stored.submitted_bytes.clone(),
        protocol_authority: protocol_authority.clone(),
        resulting_activity_id,
        core_receipt_bytes: stored.core_receipt_bytes.clone(),
        verification_level,
    })
}

fn encode_entry(entry: &Entry) -> Result<Vec<u8>, RecordError> {
    validate_entry(entry)?;
    let mut output = Vec::new();
    output.extend_from_slice(ENTRY_MAGIC);
    output.push(ENTRY_VERSION);
    output.push(entry.class as u8);
    output.extend_from_slice(&entry.observed_at_ms.to_be_bytes());
    push_u16_bytes(&mut output, entry.tenant.as_str().as_bytes())?;
    push_u16_bytes(&mut output, entry.agent.as_bytes())?;
    push_optional_fixed(&mut output, entry.session.map(|value| value.0));
    push_optional_fixed(&mut output, entry.capability);
    push_u16_bytes(&mut output, entry.policy_version.as_bytes())?;
    output.extend_from_slice(&entry.request_id);
    push_optional_fixed(
        &mut output,
        entry.idempotency_key.map(IdempotencyKey::bytes),
    );
    output.push(entry.decision as u8);
    push_u16_bytes(&mut output, entry.reason.as_str().as_bytes())?;
    push_optional_fixed(
        &mut output,
        entry.resulting_activity_id.map(ActivityId::bytes),
    );
    output.push(entry.verification_level.wire_rank());
    push_authority(&mut output, entry.protocol_authority.as_ref());
    push_payload(&mut output, entry.submitted_bytes.as_ref());
    push_optional_fixed(&mut output, entry.receipt_id);
    Ok(output)
}

pub(crate) fn decode_entry(bytes: &[u8]) -> Result<Entry, RecordError> {
    let mut decoder = Decoder::new(bytes);
    if decoder.take(4)? != ENTRY_MAGIC {
        return Err(RecordError::Decode("invalid audit entry header"));
    }
    let version = decoder.byte()?;
    if version != ENTRY_VERSION && version != LEGACY_ENTRY_VERSION {
        return Err(RecordError::Decode("invalid audit entry version"));
    }
    let class = EventClass::from_byte(decoder.byte()?)?;
    let observed_at_ms = if version == ENTRY_VERSION {
        decoder.u64()?
    } else {
        0
    };
    let tenant_text = std::str::from_utf8(decoder.u16_bytes()?)
        .map_err(|_| RecordError::Decode("tenant is not UTF-8"))?;
    let tenant = TenantId::new(tenant_text)
        .map_err(|_| RecordError::Decode("tenant identifier is invalid"))?;
    let agent =
        Did::new(decoder.u16_bytes()?).map_err(|_| RecordError::Decode("agent DID is invalid"))?;
    let session = decoder.optional_fixed()?.map(SessionId);
    let capability = decoder.optional_fixed()?;
    let policy_version = std::str::from_utf8(decoder.u16_bytes()?)
        .map_err(|_| RecordError::Decode("policy version is not UTF-8"))?
        .to_owned();
    let request_id = decoder.fixed()?;
    let idempotency_key = decoder.optional_fixed()?.map(IdempotencyKey::new);
    let decision = Decision::from_byte(decoder.byte()?)?;
    let reason = std::str::from_utf8(decoder.u16_bytes()?)
        .map_err(|_| RecordError::Decode("reason is not UTF-8"))?
        .to_owned();
    let reason = Redacted::stored(reason)?;
    let resulting_activity_id = decoder.optional_fixed()?.map(ActivityId::new);
    let verification_level = verification_level(decoder.byte()?)?;
    let protocol_authority = decoder.authority()?;
    let submitted_bytes = decoder.payload()?;
    let receipt_id = decoder.optional_fixed()?;
    if !decoder.finished() {
        return Err(RecordError::Decode("trailing audit entry bytes"));
    }
    let entry = Entry {
        class,
        observed_at_ms,
        tenant,
        agent,
        session,
        capability,
        policy_version,
        request_id,
        idempotency_key,
        decision,
        reason,
        resulting_activity_id,
        verification_level,
        protocol_authority,
        submitted_bytes,
        receipt_id,
    };
    validate_entry(&entry)?;
    Ok(entry)
}

fn validate_entry(entry: &Entry) -> Result<(), RecordError> {
    for text in [&entry.policy_version, entry.reason.as_str()] {
        if text.is_empty() || text.len() > MAX_TEXT_BYTES || text.as_bytes().contains(&0) {
            return Err(RecordError::Invalid("required text is empty or invalid"));
        }
    }
    if entry.class == EventClass::Submission
        && (entry.submitted_bytes.is_none() || entry.protocol_authority.is_none())
    {
        return Err(RecordError::Invalid(
            "submission evidence requires bytes and protocol authority",
        ));
    }
    if entry.class == EventClass::TerminalOutcome
        && (entry.resulting_activity_id.is_none() || entry.receipt_id.is_none())
    {
        return Err(RecordError::Invalid(
            "terminal evidence requires activity and receipt identifiers",
        ));
    }
    Ok(())
}

fn push_u16_bytes(output: &mut Vec<u8>, value: &[u8]) -> Result<(), RecordError> {
    let length = u16::try_from(value.len()).map_err(|_| RecordError::Invalid("text too large"))?;
    output.extend_from_slice(&length.to_be_bytes());
    output.extend_from_slice(value);
    Ok(())
}

fn push_optional_fixed(output: &mut Vec<u8>, value: Option<[u8; 32]>) {
    match value {
        Some(bytes) => {
            output.push(1);
            output.extend_from_slice(&bytes);
        }
        None => output.push(0),
    }
}

fn push_payload(output: &mut Vec<u8>, value: Option<&PayloadEvidence>) {
    match value {
        Some(PayloadEvidence::Digest(digest)) => {
            output.push(1);
            output.extend_from_slice(digest);
        }
        Some(PayloadEvidence::Redacted) => output.push(2),
        None => output.push(0),
    }
}

fn push_authority(output: &mut Vec<u8>, authority: Option<&ProtocolAuthority>) {
    match authority {
        Some(ProtocolAuthority::PrimaryKey(identifier)) => {
            output.push(1);
            output.extend_from_slice(identifier);
        }
        Some(ProtocolAuthority::SessionKey(identifier)) => {
            output.push(2);
            output.extend_from_slice(identifier);
        }
        Some(ProtocolAuthority::CapabilityGrant(identifier)) => {
            output.push(3);
            output.extend_from_slice(identifier);
        }
        None => output.push(0),
    }
}

fn verification_level(rank: u8) -> Result<VerificationLevel, RecordError> {
    match rank {
        0 => Ok(VerificationLevel::UNVERIFIED),
        1 => Ok(VerificationLevel::SEQUENCER_SIGNED),
        2 => Ok(VerificationLevel::BATCH_INCLUDED),
        3 => Ok(VerificationLevel::STATE_PROVEN),
        4 => Ok(VerificationLevel::CHECKPOINT_FINALISED),
        5 => Ok(VerificationLevel::SETTLEMENT_ANCHORED),
        _ => Err(RecordError::Decode("unknown verification level")),
    }
}

struct Decoder<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Decoder<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], RecordError> {
        let end = self
            .offset
            .checked_add(length)
            .ok_or(RecordError::Decode("entry length overflow"))?;
        let value = self
            .bytes
            .get(self.offset..end)
            .ok_or(RecordError::Decode("truncated audit entry"))?;
        self.offset = end;
        Ok(value)
    }

    fn byte(&mut self) -> Result<u8, RecordError> {
        Ok(self.take(1)?[0])
    }

    fn fixed(&mut self) -> Result<[u8; 32], RecordError> {
        self.take(32)?
            .try_into()
            .map_err(|_| RecordError::Decode("truncated fixed identifier"))
    }

    fn optional_fixed(&mut self) -> Result<Option<[u8; 32]>, RecordError> {
        match self.byte()? {
            0 => Ok(None),
            1 => self.fixed().map(Some),
            _ => Err(RecordError::Decode("invalid optional identifier tag")),
        }
    }

    fn u16_bytes(&mut self) -> Result<&'a [u8], RecordError> {
        let length = u16::from_be_bytes(
            self.take(2)?
                .try_into()
                .map_err(|_| RecordError::Decode("truncated text length"))?,
        );
        self.take(usize::from(length))
    }

    fn u64(&mut self) -> Result<u64, RecordError> {
        Ok(u64::from_be_bytes(self.take(8)?.try_into().map_err(
            |_| RecordError::Decode("truncated observation time"),
        )?))
    }

    fn payload(&mut self) -> Result<Option<PayloadEvidence>, RecordError> {
        match self.byte()? {
            0 => Ok(None),
            1 => self
                .fixed()
                .map(|digest| Some(PayloadEvidence::Digest(digest))),
            2 => Ok(Some(PayloadEvidence::Redacted)),
            _ => Err(RecordError::Decode("invalid payload evidence tag")),
        }
    }

    fn authority(&mut self) -> Result<Option<ProtocolAuthority>, RecordError> {
        let kind = self.byte()?;
        if kind == 0 {
            return Ok(None);
        }
        let identifier = self.fixed()?;
        match kind {
            1 => Ok(Some(ProtocolAuthority::PrimaryKey(identifier))),
            2 => Ok(Some(ProtocolAuthority::SessionKey(identifier))),
            3 => Ok(Some(ProtocolAuthority::CapabilityGrant(identifier))),
            _ => Err(RecordError::Decode("unknown protocol authority")),
        }
    }

    const fn finished(&self) -> bool {
        self.offset == self.bytes.len()
    }
}
