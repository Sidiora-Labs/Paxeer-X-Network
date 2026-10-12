//! The gateway core: adapter registration and the edge-only translation
//! lifecycle. Adapters translate at the edge only - they hold no protocol
//! authority and construct no payload bytes outside the plane's single
//! payload authorities - and every state-changing translation terminates in a
//! receipt-verified `LayerX` operation checked through the protocol's own
//! offline verifier. Principal isolation, idempotency, typed errors,
//! redaction, audit and trace propagation follow the human service exactly.

use std::collections::BTreeMap;
use std::fmt::{Display, Formatter};

use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::merkle::leaf_hash;
use layerx_proof::receipt::{verify, verify_sequencer_signature, AuthorizedBatch, ReceiptKeys};
use layerx_proof::sequencer_logs::{verify_logs, SequencerLog, SequencerLogRefusal};
use sha2::{Digest as _, Sha256};
use sha3::Keccak256;

use crate::adapter::{AdapterDescriptor, AdapterId};
use crate::audit::{AuditChain, AuditEventKind};
use crate::codec::{self, Decoder};
use crate::error::GatewayError;
use crate::principal::{GatewayStore, PrincipalId, RowKey, Table};
use crate::redaction::{emit, FieldValue, Label, ADAPTER_CALL_SCHEMA};
use crate::trace::{TraceId, Traced};

const RECORD_MAGIC: &[u8; 4] = b"LXIT";
const RECORD_VERSION: u8 = 1;
const REGISTRY_CHAIN: &str = "gateway-registry";
const TRANSLATION_PREFIX: &str = "tr-";
const TELEMETRY_PREFIX: &str = "tel-";
/// The layerxledger precompile address, 0x101A.
const LEDGER_ADDRESS: [u8; 20] = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x1a,
];
const PAYMENT_EXACT_EVENT: &[u8] = b"PaymentExact(bytes32,bytes32,bytes32,bytes32,uint128)";
const PAYMENT_DRAWN_EVENT: &[u8] =
    b"PaymentDrawn(bytes32,bytes32,bytes32,bytes32,uint128,bytes32,bytes32)";
/// `PaymentExact` data: assetId and amount, one ABI word each.
const PAYMENT_EXACT_DATA_BYTES: usize = 64;
/// `PaymentDrawn` data: assetId, amount, idempotencyKey and contextHash.
const PAYMENT_DRAWN_DATA_BYTES: usize = 128;
/// Position of the idempotencyKey word in `PaymentDrawn` data.
const PAYMENT_DRAWN_KEY_WORD: std::ops::Range<usize> = 64..96;

const fn hex_digit(nibble: u8) -> char {
    match nibble {
        0..=9 => (b'0' + nibble) as char,
        _ => (b'a' + (nibble - 10)) as char,
    }
}

fn hex(bytes: &[u8; 32]) -> String {
    let mut value = String::with_capacity(64);
    for byte in bytes {
        value.push(hex_digit(byte >> 4));
        value.push(hex_digit(byte & 0x0f));
    }
    value
}

/// Whether a translation reads state or changes it.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TranslationKind {
    ReadOnly,
    StateChanging,
}

impl TranslationKind {
    /// Returns the emission label.
    #[must_use]
    pub const fn label(self) -> &'static str {
        match self {
            Self::ReadOnly => "read-only",
            Self::StateChanging => "state-changing",
        }
    }

    const fn code(self) -> u8 {
        match self {
            Self::ReadOnly => 1,
            Self::StateChanging => 2,
        }
    }

    fn from_code(value: u8) -> Result<Self, GatewayError> {
        match value {
            1 => Ok(Self::ReadOnly),
            2 => Ok(Self::StateChanging),
            _ => Err(GatewayError::Corrupt("unknown translation kind")),
        }
    }
}

/// One adapter translation request: the registered adapter, the declared
/// kind, the caller's stable idempotency key and the digest of the exact
/// translated request content the key covers.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TranslationRequest {
    adapter: AdapterId,
    kind: TranslationKind,
    idempotency_key: [u8; 32],
    request_digest: [u8; 32],
}

impl TranslationRequest {
    /// Builds one translation request.
    ///
    /// # Errors
    ///
    /// Refuses the reserved zero idempotency key and the zero request digest.
    pub fn new(
        adapter: AdapterId,
        kind: TranslationKind,
        idempotency_key: [u8; 32],
        request_digest: [u8; 32],
    ) -> Result<Self, GatewayError> {
        if idempotency_key == [0; 32] || request_digest == [0; 32] {
            return Err(GatewayError::InvalidTranslation);
        }
        Ok(Self {
            adapter,
            kind,
            idempotency_key,
            request_digest,
        })
    }

    /// Returns the adapter the request names.
    #[must_use]
    pub const fn adapter(&self) -> &AdapterId {
        &self.adapter
    }

    /// Returns the declared translation kind.
    #[must_use]
    pub const fn kind(&self) -> TranslationKind {
        self.kind
    }

    /// Returns the caller's idempotency key.
    #[must_use]
    pub const fn idempotency_key(&self) -> [u8; 32] {
        self.idempotency_key
    }

    /// Returns the digest of the translated request content.
    #[must_use]
    pub const fn request_digest(&self) -> [u8; 32] {
        self.request_digest
    }
}

/// Honest durable state of one translation. A state-changing translation
/// reaches `ReceiptVerified` only through the protocol's offline receipt
/// verifier; no other terminal success exists for it.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TranslationStatus {
    Pending,
    Translated,
    ReceiptVerified { receipt_digest: [u8; 32] },
    Refused,
}

impl TranslationStatus {
    /// Returns the emission label.
    #[must_use]
    pub const fn label(self) -> &'static str {
        match self {
            Self::Pending => "pending",
            Self::Translated => "translated",
            Self::ReceiptVerified { .. } => "receipt-verified",
            Self::Refused => "refused",
        }
    }

    const fn code(self) -> u8 {
        match self {
            Self::Pending => 1,
            Self::Translated => 2,
            Self::ReceiptVerified { .. } => 3,
            Self::Refused => 4,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct TranslationRecord {
    adapter: String,
    kind: TranslationKind,
    request_digest: [u8; 32],
    status: TranslationStatus,
}

fn encode_record(record: &TranslationRecord) -> Result<Vec<u8>, GatewayError> {
    let mut bytes = Vec::new();
    bytes.extend_from_slice(RECORD_MAGIC);
    bytes.push(RECORD_VERSION);
    codec::push_bytes(&mut bytes, record.adapter.as_bytes())
        .map_err(|_| GatewayError::Corrupt("translation record exceeds encoding bounds"))?;
    bytes.push(record.kind.code());
    bytes.extend_from_slice(&record.request_digest);
    bytes.push(record.status.code());
    let receipt_digest = match record.status {
        TranslationStatus::ReceiptVerified { receipt_digest } => receipt_digest,
        TranslationStatus::Pending | TranslationStatus::Translated | TranslationStatus::Refused => {
            [0; 32]
        }
    };
    bytes.extend_from_slice(&receipt_digest);
    Ok(bytes)
}

fn decode_record(bytes: &[u8]) -> Result<TranslationRecord, GatewayError> {
    let corrupt = |_| GatewayError::Corrupt("truncated translation record");
    let mut reader = Decoder::new(bytes);
    if reader.take(4).map_err(corrupt)? != RECORD_MAGIC {
        return Err(GatewayError::Corrupt("invalid translation record header"));
    }
    if reader.byte().map_err(corrupt)? != RECORD_VERSION {
        return Err(GatewayError::Corrupt("unknown translation record version"));
    }
    let adapter = reader.text().map_err(corrupt)?.to_owned();
    let kind = TranslationKind::from_code(reader.byte().map_err(corrupt)?)?;
    let request_digest = reader.array().map_err(corrupt)?;
    let status_code = reader.byte().map_err(corrupt)?;
    let receipt_digest = reader.array().map_err(corrupt)?;
    if !reader.is_empty() {
        return Err(GatewayError::Corrupt("trailing bytes"));
    }
    let status = match status_code {
        1 => TranslationStatus::Pending,
        2 => TranslationStatus::Translated,
        3 => TranslationStatus::ReceiptVerified { receipt_digest },
        4 => TranslationStatus::Refused,
        _ => return Err(GatewayError::Corrupt("unknown translation status")),
    };
    Ok(TranslationRecord {
        adapter,
        kind,
        request_digest,
        status,
    })
}

fn translation_row(idempotency_key: [u8; 32]) -> Result<RowKey, GatewayError> {
    RowKey::new(format!("{TRANSLATION_PREFIX}{}", hex(&idempotency_key)))
        .map_err(GatewayError::Store)
}

fn telemetry_row(operation: &str, idempotency_key: [u8; 32]) -> Result<RowKey, GatewayError> {
    RowKey::new(format!(
        "{TELEMETRY_PREFIX}{operation}-{}",
        hex(&idempotency_key)
    ))
    .map_err(GatewayError::Store)
}

fn adapter_call_emission(
    adapter: &str,
    operation: &str,
    outcome: &str,
    trace: &TraceId,
) -> Result<Vec<u8>, GatewayError> {
    let redacted = |_| GatewayError::Corrupt("emission refused by the redaction gate");
    emit(
        ADAPTER_CALL_SCHEMA,
        &[
            FieldValue::Label(Label::new(adapter).map_err(redacted)?),
            FieldValue::Label(Label::new(operation).map_err(redacted)?),
            FieldValue::Label(Label::new(outcome).map_err(redacted)?),
            FieldValue::Trace(trace.clone()),
        ],
    )
    .map_err(redacted)
}

/// Why a signed sequencer receipt could not settle a translation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SequencerSettlementError {
    /// A refusal shared with every settlement path, including a receipt the
    /// sequencer signature verifier rejected.
    Gateway(GatewayError),
    /// The served logs disagree with the receipt's signed effects.
    Logs(SequencerLogRefusal),
    /// The verified logs do not hold exactly one layerxledger payment log
    /// carrying the translation's idempotency key.
    PaymentUnmatched,
}

impl Display for SequencerSettlementError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Gateway(error) => Display::fmt(error, formatter),
            Self::Logs(refusal) => {
                write!(
                    formatter,
                    "receipt logs disagree with the signed effects: {refusal:?}"
                )
            }
            Self::PaymentUnmatched => formatter
                .write_str("receipt holds no single layerxledger payment for the idempotency key"),
        }
    }
}

impl std::error::Error for SequencerSettlementError {}

impl From<GatewayError> for SequencerSettlementError {
    fn from(value: GatewayError) -> Self {
        Self::Gateway(value)
    }
}

fn keccak256(bytes: &[u8]) -> [u8; 32] {
    Keccak256::digest(bytes).into()
}

/// Whether one verified log is a layerxledger payment for `idempotency_key`:
/// a `PaymentExact` whose indexed paymentReference is the key, or a
/// `PaymentDrawn` whose idempotencyKey data word is the key.
fn pays_idempotency_key(
    log: &SequencerLog,
    idempotency_key: &[u8; 32],
    payment_exact: &[u8; 32],
    payment_drawn: &[u8; 32],
) -> bool {
    if log.address() != LEDGER_ADDRESS {
        return false;
    }
    match log.topics() {
        [topic, _, _, reference] if topic == payment_exact => {
            log.data().len() == PAYMENT_EXACT_DATA_BYTES && reference == idempotency_key
        }
        [topic, _, _, _] if topic == payment_drawn => {
            log.data().len() == PAYMENT_DRAWN_DATA_BYTES
                && log.data().get(PAYMENT_DRAWN_KEY_WORD) == Some(idempotency_key.as_slice())
        }
        _ => false,
    }
}

/// The interoperability gateway core: the adapter registry, the
/// principal-scoped translation store, and the hash-chained audit logs.
#[derive(Debug)]
pub struct GatewayCore {
    adapters: BTreeMap<AdapterId, AdapterDescriptor>,
    registry_audit: AuditChain,
    store: GatewayStore,
}

impl Default for GatewayCore {
    fn default() -> Self {
        Self::new()
    }
}

impl GatewayCore {
    /// Creates an empty gateway core.
    #[must_use]
    pub fn new() -> Self {
        Self {
            adapters: BTreeMap::new(),
            registry_audit: AuditChain::new(REGISTRY_CHAIN),
            store: GatewayStore::new(),
        }
    }

    /// Registers one adapter. The descriptor type already carries the pinned
    /// upstream specification and the conformance suite declared at
    /// registration; re-registering an identifier is refused so an upstream
    /// version bump is an explicit adapter change, never silent drift.
    ///
    /// # Errors
    ///
    /// Refuses duplicate adapter identifiers and returns audit failures.
    pub fn register_adapter(
        &mut self,
        descriptor: AdapterDescriptor,
        trace: &TraceId,
        now: u64,
    ) -> Result<(), Traced<GatewayError>> {
        if self.adapters.contains_key(descriptor.id()) {
            return Err(trace.wrap(GatewayError::DuplicateAdapter));
        }
        self.registry_audit
            .append(
                now,
                trace,
                AuditEventKind::AdapterRegistered,
                descriptor.id().as_str(),
                descriptor.spec().document_digest(),
            )
            .map_err(|error| trace.wrap(GatewayError::Audit(error)))?;
        self.adapters.insert(descriptor.id().clone(), descriptor);
        Ok(())
    }

    /// Returns one registered adapter's declaration.
    #[must_use]
    pub fn adapter(&self, id: &AdapterId) -> Option<&AdapterDescriptor> {
        self.adapters.get(id)
    }

    /// Returns every registered adapter identifier in deterministic order.
    #[must_use]
    pub fn adapter_ids(&self) -> Vec<AdapterId> {
        self.adapters.keys().cloned().collect()
    }

    /// Returns the registration audit chain.
    #[must_use]
    pub const fn registry_audit(&self) -> &AuditChain {
        &self.registry_audit
    }

    /// Returns one principal's audit chain, keyed by that principal only.
    #[must_use]
    pub fn principal_audit(&self, principal: &PrincipalId) -> Option<&AuditChain> {
        self.store.principal_audit(principal)
    }

    /// Reads one translation's status from the named principal's scope only;
    /// another principal's key resolves to nothing.
    #[must_use]
    pub fn translation(
        &self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
    ) -> Option<TranslationStatus> {
        let row = translation_row(idempotency_key).ok()?;
        let stored = self.store.read(principal, Table::Translations, &row)?;
        decode_record(stored.bytes())
            .ok()
            .map(|record| record.status)
    }

    /// Opens one translation under the caller's idempotency key. Repeating
    /// the key with identical content returns the translation's current
    /// status; repeating it with different content is refused.
    ///
    /// # Errors
    ///
    /// Refuses unregistered adapters, conflicting repeated keys, and returns
    /// audit and record failures.
    pub fn begin_translation(
        &mut self,
        principal: &PrincipalId,
        request: &TranslationRequest,
        trace: &TraceId,
        now: u64,
    ) -> Result<TranslationStatus, Traced<GatewayError>> {
        let fail = |error: GatewayError| trace.wrap(error);
        if !self.adapters.contains_key(request.adapter()) {
            return Err(fail(GatewayError::UnknownAdapter));
        }
        let row = translation_row(request.idempotency_key()).map_err(fail)?;
        let mut scope = self.store.principal(principal);
        if let Some(existing) = scope.get(Table::Translations, &row) {
            let record = decode_record(existing.bytes()).map_err(fail)?;
            if record.adapter != request.adapter().as_str()
                || record.kind != request.kind()
                || record.request_digest != request.request_digest()
            {
                return Err(fail(GatewayError::IdempotencyConflict));
            }
            return Ok(record.status);
        }
        let record = TranslationRecord {
            adapter: request.adapter().as_str().to_owned(),
            kind: request.kind(),
            request_digest: request.request_digest(),
            status: TranslationStatus::Pending,
        };
        let bytes = encode_record(&record).map_err(fail)?;
        scope.put(Table::Translations, row, now, bytes);
        scope
            .audit()
            .append(
                now,
                trace,
                AuditEventKind::TranslationBegun,
                request.adapter().as_str(),
                request.request_digest(),
            )
            .map_err(|error| fail(GatewayError::Audit(error)))?;
        let emission =
            adapter_call_emission(request.adapter().as_str(), "begin", "accepted", trace)
                .map_err(fail)?;
        let telemetry = telemetry_row("begin", request.idempotency_key()).map_err(fail)?;
        scope.put(Table::Telemetry, telemetry, now, emission);
        Ok(TranslationStatus::Pending)
    }

    /// Terminates one state-changing translation in a receipt-verified
    /// `LayerX` operation. The canonical receipt bytes come from the plane's
    /// payload authorities and are verified here through the protocol's
    /// offline verifier; the adapter supplies no payload bytes of its own and
    /// no other path can mark a state-changing translation successful.
    ///
    /// # Errors
    ///
    /// Refuses unknown translations, read-only translations, closed
    /// translations, conflicting repeated settlements, and every receipt that
    /// fails the protocol verifier, leaving the translation pending.
    pub fn settle_with_receipt(
        &mut self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        canonical_receipt: &[u8],
        authorised: &AuthorizedBatch,
        trace: &TraceId,
        now: u64,
    ) -> Result<TranslationStatus, Traced<GatewayError>> {
        self.settle_verified(principal, idempotency_key, trace, now, || {
            let verified =
                verify(canonical_receipt, authorised).map_err(GatewayError::ReceiptRejected)?;
            leaf_hash(verified.canonical_bytes())
                .map_err(|_| GatewayError::Corrupt("receipt cannot be digested"))
        })
    }

    /// Terminates one state-changing translation on a sequencer-signed
    /// `ProtocolReceipt` v2. The receipt signature is verified against the
    /// height-ranged `ReceiptKeys` list, the served logs are bound to the
    /// receipt's signed effects, and exactly one layerxledger (0x101A) log
    /// must pay the translation's idempotency key: a `PaymentExact` matched by
    /// its paymentReference or a `PaymentDrawn` matched by the idempotencyKey
    /// in its data. The translation records the sha256 digest of the signed
    /// receipt bytes.
    ///
    /// # Errors
    ///
    /// Refuses unknown, read-only, closed and conflicting translations as
    /// [`Self::settle_with_receipt`] does, a receipt the signature verifier
    /// rejects, logs that disagree with the signed effects, and a receipt
    /// without exactly one matching payment log, leaving the translation
    /// pending.
    #[allow(clippy::too_many_arguments)]
    pub fn settle_with_sequencer_receipt(
        &mut self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        signed_receipt: &[u8],
        logs: Vec<SequencerLog>,
        receipt_keys: &[SequencerAuthorization],
        trace: &TraceId,
        now: u64,
    ) -> Result<TranslationStatus, Traced<SequencerSettlementError>> {
        self.settle_verified(principal, idempotency_key, trace, now, || {
            let receipt =
                verify_sequencer_signature(signed_receipt, ReceiptKeys::HeightRanges(receipt_keys))
                    .map_err(GatewayError::ReceiptRejected)?;
            let verified = verify_logs(&receipt, logs).map_err(SequencerSettlementError::Logs)?;
            let payment_exact = keccak256(PAYMENT_EXACT_EVENT);
            let payment_drawn = keccak256(PAYMENT_DRAWN_EVENT);
            let matches = verified
                .logs()
                .iter()
                .filter(|log| {
                    pays_idempotency_key(log, &idempotency_key, &payment_exact, &payment_drawn)
                })
                .count();
            if matches != 1 {
                return Err(SequencerSettlementError::PaymentUnmatched);
            }
            Ok(Sha256::digest(signed_receipt).into())
        })
    }

    /// Records one state-changing translation as receipt-verified once
    /// `verify` yields the receipt digest. Nothing is written unless the
    /// translation exists in the principal's scope, is state-changing, the
    /// receipt verifies, and the translation is pending or already settled on
    /// the same digest.
    fn settle_verified<E: From<GatewayError>>(
        &mut self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        trace: &TraceId,
        now: u64,
        verify: impl FnOnce() -> Result<[u8; 32], E>,
    ) -> Result<TranslationStatus, Traced<E>> {
        let fail = |error: GatewayError| trace.wrap(E::from(error));
        let row = translation_row(idempotency_key).map_err(fail)?;
        let mut scope = self.store.principal(principal);
        let stored = scope
            .get(Table::Translations, &row)
            .ok_or_else(|| fail(GatewayError::UnknownTranslation))?;
        let mut record = decode_record(stored.bytes()).map_err(fail)?;
        if record.kind != TranslationKind::StateChanging {
            return Err(fail(GatewayError::NotStateChanging));
        }
        let receipt_digest = verify().map_err(|error| trace.wrap(error))?;
        match record.status {
            TranslationStatus::Pending => {}
            TranslationStatus::ReceiptVerified {
                receipt_digest: settled,
            } => {
                if settled == receipt_digest {
                    return Ok(record.status);
                }
                return Err(fail(GatewayError::IdempotencyConflict));
            }
            TranslationStatus::Translated => {
                return Err(fail(GatewayError::Corrupt(
                    "state-changing translation carries a read-only completion",
                )));
            }
            TranslationStatus::Refused => return Err(fail(GatewayError::TranslationClosed)),
        }
        record.status = TranslationStatus::ReceiptVerified { receipt_digest };
        let bytes = encode_record(&record).map_err(fail)?;
        scope.put(Table::Translations, row, now, bytes);
        scope
            .audit()
            .append(
                now,
                trace,
                AuditEventKind::TranslationCompleted,
                &record.adapter,
                receipt_digest,
            )
            .map_err(|error| fail(GatewayError::Audit(error)))?;
        let emission = adapter_call_emission(&record.adapter, "settle", "receipt-verified", trace)
            .map_err(fail)?;
        let telemetry = telemetry_row("settle", idempotency_key).map_err(fail)?;
        scope.put(Table::Telemetry, telemetry, now, emission);
        Ok(record.status)
    }

    /// Completes one read-only translation. A state-changing translation
    /// cannot take this path: it terminates only in a receipt-verified
    /// operation.
    ///
    /// # Errors
    ///
    /// Refuses unknown translations, state-changing translations, and closed
    /// translations.
    pub fn complete_read_only(
        &mut self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        trace: &TraceId,
        now: u64,
    ) -> Result<TranslationStatus, Traced<GatewayError>> {
        let fail = |error: GatewayError| trace.wrap(error);
        let row = translation_row(idempotency_key).map_err(fail)?;
        let mut scope = self.store.principal(principal);
        let stored = scope
            .get(Table::Translations, &row)
            .ok_or_else(|| fail(GatewayError::UnknownTranslation))?;
        let mut record = decode_record(stored.bytes()).map_err(fail)?;
        if record.kind != TranslationKind::ReadOnly {
            return Err(fail(GatewayError::ReceiptRequired));
        }
        match record.status {
            TranslationStatus::Pending => {}
            TranslationStatus::Translated => return Ok(record.status),
            TranslationStatus::ReceiptVerified { .. } => {
                return Err(fail(GatewayError::Corrupt(
                    "read-only translation carries a settlement receipt",
                )));
            }
            TranslationStatus::Refused => return Err(fail(GatewayError::TranslationClosed)),
        }
        record.status = TranslationStatus::Translated;
        let bytes = encode_record(&record).map_err(fail)?;
        scope.put(Table::Translations, row, now, bytes);
        scope
            .audit()
            .append(
                now,
                trace,
                AuditEventKind::TranslationCompleted,
                &record.adapter,
                record.request_digest,
            )
            .map_err(|error| fail(GatewayError::Audit(error)))?;
        let emission = adapter_call_emission(&record.adapter, "complete", "translated", trace)
            .map_err(fail)?;
        let telemetry = telemetry_row("complete", idempotency_key).map_err(fail)?;
        scope.put(Table::Telemetry, telemetry, now, emission);
        Ok(record.status)
    }

    /// Records one translation's honest terminal refusal.
    ///
    /// # Errors
    ///
    /// Refuses unknown translations and translations that already completed.
    pub fn refuse_translation(
        &mut self,
        principal: &PrincipalId,
        idempotency_key: [u8; 32],
        trace: &TraceId,
        now: u64,
    ) -> Result<TranslationStatus, Traced<GatewayError>> {
        let fail = |error: GatewayError| trace.wrap(error);
        let row = translation_row(idempotency_key).map_err(fail)?;
        let mut scope = self.store.principal(principal);
        let stored = scope
            .get(Table::Translations, &row)
            .ok_or_else(|| fail(GatewayError::UnknownTranslation))?;
        let mut record = decode_record(stored.bytes()).map_err(fail)?;
        match record.status {
            TranslationStatus::Pending => {}
            TranslationStatus::Refused => return Ok(record.status),
            TranslationStatus::Translated | TranslationStatus::ReceiptVerified { .. } => {
                return Err(fail(GatewayError::TranslationClosed));
            }
        }
        record.status = TranslationStatus::Refused;
        let bytes = encode_record(&record).map_err(fail)?;
        scope.put(Table::Translations, row, now, bytes);
        scope
            .audit()
            .append(
                now,
                trace,
                AuditEventKind::TranslationRefused,
                &record.adapter,
                record.request_digest,
            )
            .map_err(|error| fail(GatewayError::Audit(error)))?;
        let emission =
            adapter_call_emission(&record.adapter, "refuse", "refused", trace).map_err(fail)?;
        let telemetry = telemetry_row("refuse", idempotency_key).map_err(fail)?;
        scope.put(Table::Telemetry, telemetry, now, emission);
        Ok(record.status)
    }
}
