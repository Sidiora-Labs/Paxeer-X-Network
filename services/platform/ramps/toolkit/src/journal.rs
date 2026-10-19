use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{BufRead as _, BufReader, Read as _, Seek as _, Write as _};
use std::path::Path;

use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::migration::SourceSettlementRecordV2;
use crate::{AggregateStatus, EXTERNAL_CUSTODY_LABEL, RampError, RampOrder, RampPresentation};

const JOURNAL_DOMAIN: &[u8] = b"LXP/market-maker-ramp/journal/v1\0";
const MAX_JOURNAL_RECORD_BYTES: usize = 4 * 1024 * 1024;
const APPEND_INTENT_DOMAIN: &[u8] = b"LXP/market-maker-ramp/append-intent/v1\0";

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkflowStage {
    CompliancePending,
    ManualReview,
    ComplianceRefused,
    AwaitingExternalCredit,
    SourceSettledV2,
    AwaitingLayerxPayment,
    ProviderSubmissionPlanned,
    ProviderSubmittedUnknown,
    ProviderPending,
    ProviderSettled,
    ProviderRefused,
    ProviderReversed,
    LayerxSubmissionPlanned,
    LayerxSubmittedUnknown,
    LayerxPending,
    LayerxRefused,
    LayerxVerified,
    ReversalPending,
    Reversed,
    Done,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct TransitionEvidence {
    pub provider_operation_id: Option<String>,
    pub provider_evidence_digest: Option<[u8; 32]>,
    pub activity_id: Option<[u8; 32]>,
    pub canonical_activity: Option<Vec<u8>>,
    pub receipt_digest: Option<[u8; 32]>,
    pub refusal_code: Option<String>,
    pub retry_at: Option<u64>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ProviderCallbackWrite<'a> {
    pub order_digest: [u8; 32],
    pub callback_id: &'a str,
    pub provider_sequence: u64,
    pub evidence_digest: [u8; 32],
    pub expected: WorkflowStage,
    pub next: WorkflowStage,
    pub evidence: &'a TransitionEvidence,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PaxeerObservation<'a> {
    pub operation_id: &'a str,
    pub transaction_hash: [u8; 32],
    pub stage: &'a str,
    pub block_hash: Option<[u8; 32]>,
    pub confirmations: u64,
}

impl<'a> PaxeerObservation<'a> {
    #[must_use]
    pub fn from_finality(
        operation_id: &'a str,
        report: &layerx_paxeer_client::FinalityReport,
    ) -> Self {
        use layerx_paxeer_client::FinalityStage;
        let (stage, block_hash) = match report.stage() {
            FinalityStage::Announced => ("announced", None),
            FinalityStage::Missing { .. } => ("missing", None),
            FinalityStage::Pooled { .. } => ("pooled", None),
            FinalityStage::Confirming { inclusion, .. } => {
                ("confirming", Some(inclusion.block.hash))
            }
            FinalityStage::Final { inclusion, .. } => ("final", Some(inclusion.block.hash)),
            FinalityStage::Displaced { lost, .. } => ("displaced", Some(lost.block.hash)),
        };
        Self {
            operation_id,
            transaction_hash: report.transaction().bytes(),
            stage,
            block_hash,
            confirmations: report.progress().confirmed,
        }
    }
}

impl TransitionEvidence {
    #[must_use]
    pub const fn empty() -> Self {
        Self {
            provider_operation_id: None,
            provider_evidence_digest: None,
            activity_id: None,
            canonical_activity: None,
            receipt_digest: None,
            refusal_code: None,
            retry_at: None,
        }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case", deny_unknown_fields)]
pub enum Event {
    OrderCreated {
        order: Box<RampOrder>,
    },
    LeaseAcquired {
        order_digest: [u8; 32],
        worker_id: String,
        expires_at: u64,
    },
    Transition {
        order_digest: [u8; 32],
        expected: WorkflowStage,
        next: WorkflowStage,
        evidence: TransitionEvidence,
    },
    ProviderCallbackApplied {
        order_digest: [u8; 32],
        callback_id: String,
        provider_sequence: u64,
        evidence_digest: [u8; 32],
        expected: WorkflowStage,
        next: WorkflowStage,
        evidence: TransitionEvidence,
    },
    SourceSettlementVerifiedV2 {
        settlement: SourceSettlementRecordV2,
    },
    PaxeerPlanned {
        idempotency_key: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    },
    PaxeerObserved {
        idempotency_key: [u8; 32],
        operation_id: String,
        transaction_hash: [u8; 32],
        stage: String,
        block_hash: Option<[u8; 32]>,
        confirmations: u64,
    },
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
struct RecordBody {
    sequence: u64,
    previous_hash: [u8; 32],
    recorded_at: u64,
    event: Event,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
struct Record {
    sequence: u64,
    previous_hash: [u8; 32],
    recorded_at: u64,
    event: Event,
    #[serde(rename = "record_hash")]
    hash: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OrderSnapshot {
    pub order: RampOrder,
    pub stage: WorkflowStage,
    pub evidence: TransitionEvidence,
    pub lease: Option<(String, u64)>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PaxeerSnapshot {
    pub idempotency_key: [u8; 32],
    pub asset: [u8; 32],
    pub amount: u128,
    pub operation_id: Option<String>,
    pub transaction_hash: Option<[u8; 32]>,
    pub stage: String,
    pub block_hash: Option<[u8; 32]>,
    pub confirmations: u64,
}

impl OrderSnapshot {
    #[must_use]
    pub fn presentation(&self) -> RampPresentation {
        let status = match self.stage {
            WorkflowStage::ManualReview => AggregateStatus::ManualReview,
            WorkflowStage::ComplianceRefused
            | WorkflowStage::ProviderRefused
            | WorkflowStage::LayerxRefused => AggregateStatus::Refused,
            WorkflowStage::ProviderSubmittedUnknown
            | WorkflowStage::LayerxSubmittedUnknown
            | WorkflowStage::ProviderSubmissionPlanned
            | WorkflowStage::LayerxSubmissionPlanned => AggregateStatus::Unknown,
            WorkflowStage::ProviderReversed
            | WorkflowStage::ReversalPending
            | WorkflowStage::Reversed => AggregateStatus::Reversed,
            WorkflowStage::Done => AggregateStatus::Done,
            WorkflowStage::CompliancePending
            | WorkflowStage::AwaitingExternalCredit
            | WorkflowStage::AwaitingLayerxPayment
            | WorkflowStage::ProviderPending
            | WorkflowStage::ProviderSettled
            | WorkflowStage::SourceSettledV2
            | WorkflowStage::LayerxPending
            | WorkflowStage::LayerxVerified => AggregateStatus::Pending,
        };
        let refusal_code = if matches!(
            status,
            AggregateStatus::Refused | AggregateStatus::ManualReview | AggregateStatus::Reversed
        ) {
            self.evidence.refusal_code.clone()
        } else {
            None
        };
        let retry_at = if matches!(status, AggregateStatus::Pending | AggregateStatus::Unknown) {
            self.evidence.retry_at
        } else {
            None
        };
        RampPresentation {
            external_custody_label: EXTERNAL_CUSTODY_LABEL,
            status,
            order_digest: self.order.order_digest,
            activity_id: self.evidence.activity_id,
            receipt_digest: self.evidence.receipt_digest,
            provider_evidence_digest: self.evidence.provider_evidence_digest,
            refusal_code,
            retry_at,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CallbackIdentity {
    pub order_digest: [u8; 32],
    pub provider_sequence: u64,
    pub evidence_digest: [u8; 32],
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct Projection {
    orders: BTreeMap<[u8; 32], OrderSnapshot>,
    order_ids: BTreeMap<String, [u8; 32]>,
    callbacks: BTreeMap<String, CallbackIdentity>,
    provider_sequences: BTreeMap<[u8; 32], u64>,
    source_claims: BTreeMap<[u8; 32], SourceSettlementRecordV2>,
    source_orders: BTreeMap<[u8; 32], [u8; 32]>,
    paxeer: BTreeMap<[u8; 32], PaxeerSnapshot>,
}

impl Projection {
    #[must_use]
    pub fn order(&self, digest: &[u8; 32]) -> Option<&OrderSnapshot> {
        self.orders.get(digest)
    }

    #[must_use]
    pub fn order_by_id(&self, order_id: &str) -> Option<&OrderSnapshot> {
        self.order_ids
            .get(order_id)
            .and_then(|digest| self.orders.get(digest))
    }

    #[must_use]
    pub const fn orders(&self) -> &BTreeMap<[u8; 32], OrderSnapshot> {
        &self.orders
    }

    #[must_use]
    pub fn callback(&self, callback_id: &str) -> Option<CallbackIdentity> {
        self.callbacks.get(callback_id).copied()
    }

    #[must_use]
    pub const fn callbacks(&self) -> &BTreeMap<String, CallbackIdentity> {
        &self.callbacks
    }

    #[must_use]
    pub fn provider_sequence(&self, order_digest: &[u8; 32]) -> Option<u64> {
        self.provider_sequences.get(order_digest).copied()
    }

    #[must_use]
    pub const fn provider_sequences(&self) -> &BTreeMap<[u8; 32], u64> {
        &self.provider_sequences
    }

    #[must_use]
    pub fn source_settlement(&self, order_digest: &[u8; 32]) -> Option<&SourceSettlementRecordV2> {
        self.source_orders
            .get(order_digest)
            .and_then(|claim| self.source_claims.get(claim))
    }

    #[must_use]
    pub fn source_claim(&self, claim_id: &[u8; 32]) -> Option<&SourceSettlementRecordV2> {
        self.source_claims.get(claim_id)
    }

    pub fn source_settlements(&self) -> impl Iterator<Item = &SourceSettlementRecordV2> {
        self.source_claims.values()
    }

    #[must_use]
    pub fn paxeer(&self, idempotency_key: &[u8; 32]) -> Option<&PaxeerSnapshot> {
        self.paxeer.get(idempotency_key)
    }

    pub fn paxeer_transfers(&self) -> impl Iterator<Item = &PaxeerSnapshot> {
        self.paxeer.values()
    }

    fn stage(&self, event: &Event) -> Result<StagedMutation, RampError> {
        match event {
            Event::OrderCreated { order } => self.stage_order_created(order),
            Event::LeaseAcquired {
                order_digest,
                worker_id,
                expires_at,
            } => self.stage_lease(order_digest, worker_id, *expires_at),
            Event::Transition {
                order_digest,
                expected,
                next,
                evidence,
            } => self.stage_transition(order_digest, *expected, *next, evidence),
            Event::ProviderCallbackApplied {
                order_digest,
                callback_id,
                provider_sequence,
                evidence_digest,
                expected,
                next,
                evidence,
            } => StagedCallback::stage(
                self,
                ProviderCallbackWrite {
                    order_digest: *order_digest,
                    callback_id,
                    provider_sequence: *provider_sequence,
                    evidence_digest: *evidence_digest,
                    expected: *expected,
                    next: *next,
                    evidence,
                },
            )
            .map(StagedMutation::Callback),
            Event::SourceSettlementVerifiedV2 { settlement } => {
                self.stage_source_settlement(settlement)
            }
            Event::PaxeerPlanned {
                idempotency_key,
                asset,
                amount,
            } => self.stage_paxeer_planned(*idempotency_key, *asset, *amount),
            Event::PaxeerObserved {
                idempotency_key,
                operation_id,
                transaction_hash,
                stage,
                block_hash,
                confirmations,
            } => self.stage_paxeer_observed(
                idempotency_key,
                operation_id,
                *transaction_hash,
                stage,
                *block_hash,
                *confirmations,
            ),
        }
    }

    fn stage_order_created(&self, order: &RampOrder) -> Result<StagedMutation, RampError> {
        order.validate_bound()?;
        if self.orders.contains_key(&order.order_digest)
            || self.order_ids.contains_key(&order.order_id)
        {
            return Err(RampError::Conflict);
        }
        Ok(StagedMutation::CreateOrder(OrderSnapshot {
            order: order.clone(),
            stage: WorkflowStage::CompliancePending,
            evidence: TransitionEvidence::empty(),
            lease: None,
        }))
    }

    fn stage_lease(
        &self,
        order_digest: &[u8; 32],
        worker_id: &str,
        expires_at: u64,
    ) -> Result<StagedMutation, RampError> {
        if !safe_identifier(worker_id) || expires_at == 0 {
            return Err(RampError::InvalidOrder);
        }
        let mut snapshot = self
            .orders
            .get(order_digest)
            .cloned()
            .ok_or(RampError::InvalidOrder)?;
        snapshot.lease = Some((worker_id.to_owned(), expires_at));
        Ok(StagedMutation::UpdateOrder(snapshot))
    }

    fn stage_transition(
        &self,
        order_digest: &[u8; 32],
        expected: WorkflowStage,
        next: WorkflowStage,
        evidence: &TransitionEvidence,
    ) -> Result<StagedMutation, RampError> {
        if !allowed(expected, next) {
            return Err(RampError::IllegalTransition);
        }
        let mut snapshot = self
            .orders
            .get(order_digest)
            .cloned()
            .ok_or(RampError::InvalidOrder)?;
        if snapshot.stage != expected {
            return Err(RampError::Conflict);
        }
        if self.source_settlement(order_digest).is_some()
            && (evidence.provider_operation_id.is_some()
                || evidence.provider_evidence_digest.is_some())
        {
            return Err(RampError::IllegalTransition);
        }
        validate_resulting_evidence(next, &snapshot.evidence, evidence)?;
        if evidence_conflicts(&snapshot.evidence, evidence) {
            return Err(RampError::Conflict);
        }
        merge_evidence(&mut snapshot.evidence, evidence);
        if next == WorkflowStage::Done {
            if let Some(settlement) = self.source_settlement(order_digest) {
                settlement.validate_order(&snapshot.order)?;
                if snapshot.evidence.activity_id.is_none()
                    || snapshot.evidence.canonical_activity.is_none()
                    || snapshot.evidence.receipt_digest.is_none()
                {
                    return Err(RampError::IllegalTransition);
                }
            } else if completion_missing(&snapshot.evidence) {
                return Err(RampError::IllegalTransition);
            }
        }
        snapshot.stage = next;
        Ok(StagedMutation::UpdateOrder(snapshot))
    }

    fn stage_source_settlement(
        &self,
        settlement: &SourceSettlementRecordV2,
    ) -> Result<StagedMutation, RampError> {
        let mut snapshot = self
            .orders
            .get(&settlement.order_digest)
            .cloned()
            .ok_or(RampError::InvalidOrder)?;
        settlement.validate_order(&snapshot.order)?;
        if self.source_claims.contains_key(&settlement.source_claim_id)
            || self.source_orders.contains_key(&settlement.order_digest)
        {
            return Err(RampError::Conflict);
        }
        if snapshot.order.quote.direction != crate::RampDirection::OnRamp
            || snapshot.stage != WorkflowStage::AwaitingExternalCredit
            || snapshot.evidence.provider_operation_id.is_some()
            || snapshot.evidence.provider_evidence_digest.is_some()
            || snapshot.evidence.activity_id.is_some()
            || snapshot.evidence.canonical_activity.is_some()
            || snapshot.evidence.receipt_digest.is_some()
        {
            return Err(RampError::IllegalTransition);
        }
        snapshot.stage = WorkflowStage::SourceSettledV2;
        Ok(StagedMutation::Source(settlement.clone(), snapshot))
    }

    fn stage_paxeer_planned(
        &self,
        idempotency_key: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    ) -> Result<StagedMutation, RampError> {
        if idempotency_key == [0; 32] || asset == [0; 32] || amount == 0 {
            return Err(RampError::Paxeer);
        }
        if self.paxeer.contains_key(&idempotency_key) {
            return Err(RampError::Conflict);
        }
        Ok(StagedMutation::Paxeer(PaxeerSnapshot {
            idempotency_key,
            asset,
            amount,
            operation_id: None,
            transaction_hash: None,
            stage: "submission_planned".to_owned(),
            block_hash: None,
            confirmations: 0,
        }))
    }

    fn stage_paxeer_observed(
        &self,
        idempotency_key: &[u8; 32],
        operation_id: &str,
        transaction_hash: [u8; 32],
        stage: &str,
        block_hash: Option<[u8; 32]>,
        confirmations: u64,
    ) -> Result<StagedMutation, RampError> {
        if !safe_identifier(operation_id) || !safe_identifier(stage) || transaction_hash == [0; 32]
        {
            return Err(RampError::Paxeer);
        }
        let mut snapshot = self
            .paxeer
            .get(idempotency_key)
            .cloned()
            .ok_or(RampError::Paxeer)?;
        if snapshot
            .operation_id
            .as_deref()
            .is_some_and(|value| value != operation_id)
            || snapshot
                .transaction_hash
                .is_some_and(|value| value != transaction_hash)
        {
            return Err(RampError::Conflict);
        }
        let included = matches!(stage, "confirming" | "final" | "displaced");
        if !matches!(
            stage,
            "broadcast_unknown"
                | "announced"
                | "missing"
                | "pooled"
                | "confirming"
                | "final"
                | "displaced"
        ) || included != block_hash.is_some()
            || block_hash == Some([0; 32])
            || (!included && confirmations != 0)
            || (stage == "final" && confirmations == 0)
        {
            return Err(RampError::Paxeer);
        }
        if snapshot.block_hash.is_some() && stage != "displaced" {
            if block_hash != snapshot.block_hash && snapshot.stage != "displaced" {
                return Err(RampError::Conflict);
            }
            if snapshot.stage != "displaced"
                && (confirmations < snapshot.confirmations
                    || (snapshot.stage == "final" && stage != "final"))
            {
                return Err(RampError::Conflict);
            }
        }
        if stage == "displaced"
            && (snapshot.block_hash.is_none()
                || block_hash != snapshot.block_hash
                || confirmations != 0)
        {
            return Err(RampError::Conflict);
        }
        snapshot.operation_id = Some(operation_id.to_owned());
        snapshot.transaction_hash = Some(transaction_hash);
        stage.clone_into(&mut snapshot.stage);
        snapshot.block_hash = block_hash;
        snapshot.confirmations = confirmations;
        Ok(StagedMutation::Paxeer(snapshot))
    }

    fn commit(&mut self, staged: StagedMutation) {
        match staged {
            StagedMutation::CreateOrder(snapshot) => {
                self.order_ids
                    .insert(snapshot.order.order_id.clone(), snapshot.order.order_digest);
                self.orders.insert(snapshot.order.order_digest, snapshot);
            }
            StagedMutation::UpdateOrder(snapshot) => {
                self.orders.insert(snapshot.order.order_digest, snapshot);
            }
            StagedMutation::Callback(staged) => {
                let identity = staged.identity;
                self.callbacks.insert(staged.callback_id, identity);
                self.provider_sequences
                    .insert(identity.order_digest, identity.provider_sequence);
                self.orders.insert(identity.order_digest, staged.order);
            }
            StagedMutation::Source(settlement, snapshot) => {
                self.source_orders
                    .insert(settlement.order_digest, settlement.source_claim_id);
                self.source_claims
                    .insert(settlement.source_claim_id, settlement);
                self.orders.insert(snapshot.order.order_digest, snapshot);
            }
            StagedMutation::Paxeer(snapshot) => {
                self.paxeer.insert(snapshot.idempotency_key, snapshot);
            }
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct StagedCallback {
    callback_id: String,
    identity: CallbackIdentity,
    order: OrderSnapshot,
}

impl StagedCallback {
    fn stage(projection: &Projection, write: ProviderCallbackWrite<'_>) -> Result<Self, RampError> {
        let ProviderCallbackWrite {
            order_digest,
            callback_id,
            provider_sequence,
            evidence_digest,
            expected,
            next,
            evidence,
        } = write;
        if projection.source_settlement(&order_digest).is_some() {
            return Err(RampError::IllegalTransition);
        }
        if !safe_identifier(callback_id) || provider_sequence == 0 || evidence_digest == [0; 32] {
            return Err(RampError::Provider);
        }
        if projection.callbacks.contains_key(callback_id) {
            return Err(RampError::Conflict);
        }
        if projection
            .provider_sequences
            .get(&order_digest)
            .is_some_and(|latest| provider_sequence <= *latest)
        {
            return Err(RampError::Conflict);
        }
        if !allowed(expected, next) {
            return Err(RampError::IllegalTransition);
        }
        let mut order = projection
            .orders
            .get(&order_digest)
            .cloned()
            .ok_or(RampError::InvalidOrder)?;
        if order.stage != expected {
            return Err(RampError::Conflict);
        }
        validate_resulting_evidence(next, &order.evidence, evidence)?;
        if evidence_conflicts(&order.evidence, evidence) {
            return Err(RampError::Conflict);
        }
        order.stage = next;
        merge_evidence(&mut order.evidence, evidence);
        Ok(Self {
            callback_id: callback_id.to_owned(),
            identity: CallbackIdentity {
                order_digest,
                provider_sequence,
                evidence_digest,
            },
            order,
        })
    }

    #[must_use]
    pub fn callback_id(&self) -> &str {
        &self.callback_id
    }

    #[must_use]
    pub const fn identity(&self) -> CallbackIdentity {
        self.identity
    }

    #[must_use]
    pub const fn order(&self) -> &OrderSnapshot {
        &self.order
    }
}

enum StagedMutation {
    CreateOrder(OrderSnapshot),
    UpdateOrder(OrderSnapshot),
    Callback(StagedCallback),
    Source(SourceSettlementRecordV2, OrderSnapshot),
    Paxeer(PaxeerSnapshot),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WriteStep {
    BeforeRecord,
    DuringRecord,
    BeforeTerminator,
    AfterRecord,
    AfterSync,
}

impl WriteStep {
    pub const ALL: [Self; 5] = [
        Self::BeforeRecord,
        Self::DuringRecord,
        Self::BeforeTerminator,
        Self::AfterRecord,
        Self::AfterSync,
    ];
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WriteFault {
    Fail,
    Interrupt,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TornTail {
    pub offset: u64,
    pub bytes: u64,
}

enum WriteFailure {
    Failed,
    Interrupted,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct PendingAppend {
    offset: u64,
    record: Record,
    bytes: Vec<u8>,
}

#[derive(Clone, Debug, Serialize)]
pub struct JournalHealth {
    pub ready: bool,
    pub writer_held: bool,
    pub halted: bool,
    pub uncertain_write: bool,
    pub recovery_required: bool,
    pub torn_tail: bool,
    pub record_count: u64,
    pub durable_bytes: u64,
}

pub struct Journal {
    path: std::path::PathBuf,
    intent_path: std::path::PathBuf,
    _lock: LockClaim,
    file: File,
    durable_len: u64,
    durable_modified: Option<std::time::SystemTime>,
    next_sequence: u64,
    head: [u8; 32],
    previous_head: [u8; 32],
    projection: Projection,
    recovery: Option<TornTail>,
    fault: Option<(WriteStep, WriteFault)>,
    halted: bool,
}

impl Journal {
    /// # Errors
    /// Returns [`RampError::Journal`] when the file cannot be locked, read or decoded.
    pub fn open(path: impl AsRef<Path>) -> Result<Self, RampError> {
        let path = path.as_ref();
        let lock = LockClaim::acquire(path)?;
        let reader = open_journal(path)?;
        require_private_file(&reader)?;
        let mut journal = Self {
            path: path.to_owned(),
            intent_path: {
                let mut value = path.as_os_str().to_owned();
                value.push(".append-intent");
                value.into()
            },
            _lock: lock,
            file: reader.try_clone().map_err(|_| RampError::Journal)?,
            durable_len: 0,
            durable_modified: None,
            next_sequence: 0,
            head: [0; 32],
            previous_head: [0; 32],
            projection: Projection::default(),
            recovery: None,
            fault: None,
            halted: false,
        };
        journal.reload()?;
        journal.halted =
            journal.next_sequence != 0 || journal.recovery.is_some() || journal.has_intent()?;
        journal.sync_parent()?;
        Ok(journal)
    }

    fn reload(&mut self) -> Result<(), RampError> {
        self.file.rewind().map_err(|_| RampError::Journal)?;
        let reader = self.file.try_clone().map_err(|_| RampError::Journal)?;
        self.projection = Projection::default();
        self.next_sequence = 0;
        self.head = [0; 32];
        self.previous_head = [0; 32];
        self.durable_len = 0;
        self.recovery = None;
        let mut reader = BufReader::new(reader);
        let mut line = Vec::new();
        loop {
            line.clear();
            let read = reader
                .by_ref()
                .take((MAX_JOURNAL_RECORD_BYTES + 1) as u64)
                .read_until(b'\n', &mut line)
                .map_err(|_| RampError::Journal)?;
            if read == 0 {
                break;
            }
            if line.len() > MAX_JOURNAL_RECORD_BYTES {
                return Err(RampError::Journal);
            }
            if line.last() != Some(&b'\n') {
                self.recovery = Some(TornTail {
                    offset: self.durable_len,
                    bytes: read as u64,
                });
                break;
            }
            line.pop();
            if line.is_empty() {
                return Err(RampError::Journal);
            }
            let Record {
                sequence,
                previous_hash,
                recorded_at,
                event,
                hash: record_hash,
            } = serde_json::from_slice(&line).map_err(|_| RampError::Journal)?;
            if sequence != self.next_sequence || previous_hash != self.head {
                return Err(RampError::Journal);
            }
            let body = RecordBody {
                sequence,
                previous_hash,
                recorded_at,
                event,
            };
            if record_hash != record_digest(&body)? {
                return Err(RampError::Journal);
            }
            let staged = self
                .projection
                .stage(&body.event)
                .map_err(|_| RampError::Journal)?;
            self.projection.commit(staged);
            self.next_sequence = self
                .next_sequence
                .checked_add(1)
                .ok_or(RampError::Journal)?;
            self.previous_head = self.head;
            self.head = record_hash;
            self.durable_len = self
                .durable_len
                .checked_add(read as u64)
                .ok_or(RampError::Journal)?;
        }
        self.durable_modified = Some(
            self.file
                .metadata()
                .and_then(|metadata| metadata.modified())
                .map_err(|_| RampError::Journal)?,
        );
        Ok(())
    }

    pub fn health(&self) -> JournalHealth {
        let uncertain = self.has_intent().unwrap_or(true);
        let durable = same_file(&self.file, &self.path)
            && self.file.metadata().is_ok_and(|metadata| {
                metadata.len() == self.durable_len
                    && metadata.modified().ok() == self.durable_modified
            });
        let writer_held = self._lock.held();
        let ready = !self.halted && !uncertain && self.recovery.is_none() && durable && writer_held;
        JournalHealth {
            ready,
            writer_held,
            halted: self.halted || !durable || uncertain,
            uncertain_write: uncertain || !durable,
            recovery_required: !ready,
            torn_tail: self.recovery.is_some(),
            record_count: self.next_sequence,
            durable_bytes: self.durable_len,
        }
    }

    pub fn recover_verified<F>(&mut self, verify: F) -> Result<(), RampError>
    where
        F: FnOnce(&Projection) -> Result<(), RampError>,
    {
        self.halted = true;
        if !self._lock.held() || !same_file(&self.file, &self.path) {
            return Err(RampError::Journal);
        }
        self.reload()?;
        let pending = self.pending_append()?;
        let observed = self.file.metadata().map_err(|_| RampError::Journal)?;
        let observed_modified = observed.modified().map_err(|_| RampError::Journal)?;
        let mut verified_projection = self.projection.clone();
        let mut retained = 0;
        if let Some(intent) = &pending {
            let end = intent
                .offset
                .checked_add(intent.bytes.len() as u64)
                .ok_or(RampError::Journal)?;
            if observed.len() < intent.offset || observed.len() > end {
                return Err(RampError::Journal);
            }
            retained =
                usize::try_from(observed.len() - intent.offset).map_err(|_| RampError::Journal)?;
            let mut reader = self.file.try_clone().map_err(|_| RampError::Journal)?;
            reader
                .seek(std::io::SeekFrom::Start(intent.offset))
                .map_err(|_| RampError::Journal)?;
            let mut actual = Vec::new();
            reader
                .take((intent.bytes.len() + 1) as u64)
                .read_to_end(&mut actual)
                .map_err(|_| RampError::Journal)?;
            if actual.as_slice() != &intent.bytes[..retained] {
                return Err(RampError::Journal);
            }
            if retained == intent.bytes.len() {
                if self.recovery.is_some()
                    || self.durable_len != end
                    || self.next_sequence
                        != intent
                            .record
                            .sequence
                            .checked_add(1)
                            .ok_or(RampError::Journal)?
                    || self.head != intent.record.hash
                    || self.previous_head != intent.record.previous_hash
                {
                    return Err(RampError::Journal);
                }
            } else {
                if self.durable_len != intent.offset
                    || self.next_sequence != intent.record.sequence
                    || self.head != intent.record.previous_hash
                    || self.recovery
                        != (retained != 0).then_some(TornTail {
                            offset: intent.offset,
                            bytes: retained as u64,
                        })
                {
                    return Err(RampError::Journal);
                }
                let staged = verified_projection.stage(&intent.record.event)?;
                verified_projection.commit(staged);
            }
        } else if self.recovery.is_some() {
            return Err(RampError::Journal);
        }
        verify(&verified_projection)?;
        let current = self.file.metadata().map_err(|_| RampError::Journal)?;
        if !self._lock.held()
            || !same_file(&self.file, &self.path)
            || current.len() != observed.len()
            || current.modified().map_err(|_| RampError::Journal)? != observed_modified
            || self.pending_append()? != pending
        {
            return Err(RampError::Journal);
        }
        if let Some(intent) = &pending {
            self.file
                .write_all(&intent.bytes[retained..])
                .map_err(|_| RampError::Journal)?;
        }
        self.file.sync_all().map_err(|_| RampError::Journal)?;
        self.reload()?;
        if self.recovery.is_some() || self.projection != verified_projection {
            return Err(RampError::Journal);
        }
        self.clear_intent()?;
        self.halted = false;
        Ok(())
    }

    fn has_intent(&self) -> Result<bool, RampError> {
        match std::fs::symlink_metadata(&self.intent_path) {
            Ok(_) => Ok(true),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(false),
            Err(_) => Err(RampError::Journal),
        }
    }

    fn pending_append(&self) -> Result<Option<PendingAppend>, RampError> {
        if !self.has_intent()? {
            return Ok(None);
        }
        let file = File::open(&self.intent_path).map_err(|_| RampError::Journal)?;
        if !same_file(&file, &self.intent_path) {
            return Err(RampError::Journal);
        }
        let header_len = APPEND_INTENT_DOMAIN.len() + 12;
        let maximum = header_len + MAX_JOURNAL_RECORD_BYTES + 32;
        let mut raw = Vec::new();
        file.take((maximum + 1) as u64)
            .read_to_end(&mut raw)
            .map_err(|_| RampError::Journal)?;
        if raw.len() < header_len + 34
            || raw.len() > maximum
            || !raw.starts_with(APPEND_INTENT_DOMAIN)
        {
            return Err(RampError::Journal);
        }
        let offset = u64::from_be_bytes(
            raw[APPEND_INTENT_DOMAIN.len()..APPEND_INTENT_DOMAIN.len() + 8]
                .try_into()
                .map_err(|_| RampError::Journal)?,
        );
        let length = u32::from_be_bytes(
            raw[APPEND_INTENT_DOMAIN.len() + 8..header_len]
                .try_into()
                .map_err(|_| RampError::Journal)?,
        ) as usize;
        if length > MAX_JOURNAL_RECORD_BYTES || raw.len() != header_len + length + 32 {
            return Err(RampError::Journal);
        }
        let digest: [u8; 32] = Sha256::digest(&raw[..header_len + length]).into();
        if &raw[header_len + length..] != digest.as_slice() {
            return Err(RampError::Journal);
        }
        let bytes = raw[header_len..header_len + length].to_vec();
        if bytes.last() != Some(&b'\n') {
            return Err(RampError::Journal);
        }
        let record: Record =
            serde_json::from_slice(&bytes[..bytes.len() - 1]).map_err(|_| RampError::Journal)?;
        let mut canonical = serde_json::to_vec(&record).map_err(|_| RampError::Journal)?;
        canonical.push(b'\n');
        let body = RecordBody {
            sequence: record.sequence,
            previous_hash: record.previous_hash,
            recorded_at: record.recorded_at,
            event: record.event.clone(),
        };
        if canonical != bytes || record_digest(&body)? != record.hash {
            return Err(RampError::Journal);
        }
        Ok(Some(PendingAppend {
            offset,
            record,
            bytes,
        }))
    }

    fn sync_parent(&self) -> Result<(), RampError> {
        let parent = self
            .path
            .parent()
            .filter(|path| !path.as_os_str().is_empty())
            .unwrap_or_else(|| Path::new("."));
        File::open(parent)
            .and_then(|file| file.sync_all())
            .map_err(|_| RampError::Journal)
    }

    fn begin_intent(&self, bytes: &[u8]) -> Result<(), RampError> {
        let length = u32::try_from(bytes.len()).map_err(|_| RampError::Journal)?;
        if bytes.is_empty() || bytes.len() > MAX_JOURNAL_RECORD_BYTES {
            return Err(RampError::Journal);
        }
        let mut intent = Vec::with_capacity(APPEND_INTENT_DOMAIN.len() + 12 + bytes.len() + 32);
        intent.extend_from_slice(APPEND_INTENT_DOMAIN);
        intent.extend_from_slice(&self.durable_len.to_be_bytes());
        intent.extend_from_slice(&length.to_be_bytes());
        intent.extend_from_slice(bytes);
        let digest: [u8; 32] = Sha256::digest(&intent).into();
        intent.extend_from_slice(&digest);
        let mut options = OpenOptions::new();
        options.create_new(true).write(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt as _;
            options.mode(0o600);
        }
        let mut file = options
            .open(&self.intent_path)
            .map_err(|_| RampError::Journal)?;
        file.write_all(&intent)
            .and_then(|()| file.sync_all())
            .map_err(|_| RampError::Journal)?;
        self.sync_parent()
    }

    fn clear_intent(&self) -> Result<(), RampError> {
        match std::fs::remove_file(&self.intent_path) {
            Ok(()) => self.sync_parent(),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(_) => Err(RampError::Journal),
        }
    }

    fn require_operational(&self) -> Result<(), RampError> {
        if self.health().ready {
            Ok(())
        } else {
            Err(RampError::Journal)
        }
    }

    /// # Errors
    /// Returns [`RampError::InvalidOrder`] or [`RampError::OrderBinding`] when the order is unbound,
    /// [`RampError::Conflict`] when the identifier is reused with different content, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn create_order(&mut self, order: RampOrder, now: u64) -> Result<OrderSnapshot, RampError> {
        self.require_operational()?;
        order.validate_bound()?;
        if let Some(existing) = self.projection.order_ids.get(&order.order_id) {
            return if *existing == order.order_digest {
                self.projection
                    .orders
                    .get(existing)
                    .cloned()
                    .ok_or(RampError::Journal)
            } else {
                Err(RampError::Conflict)
            };
        }
        let order_digest = order.order_digest;
        self.append(
            Event::OrderCreated {
                order: Box::new(order),
            },
            now,
        )?;
        self.projection
            .orders
            .get(&order_digest)
            .cloned()
            .ok_or(RampError::Journal)
    }

    #[must_use]
    pub fn order(&self, digest: &[u8; 32]) -> Option<&OrderSnapshot> {
        self.projection.order(digest)
    }

    #[must_use]
    pub fn order_by_id(&self, order_id: &str) -> Option<&OrderSnapshot> {
        self.projection.order_by_id(order_id)
    }

    #[must_use]
    pub fn orders(&self) -> Vec<OrderSnapshot> {
        self.projection.orders.values().cloned().collect()
    }

    #[must_use]
    pub const fn projection(&self) -> &Projection {
        &self.projection
    }

    #[must_use]
    pub fn callback(&self, callback_id: &str) -> Option<CallbackIdentity> {
        self.projection.callback(callback_id)
    }

    #[must_use]
    pub fn provider_sequence(&self, order_digest: &[u8; 32]) -> Option<u64> {
        self.projection.provider_sequence(order_digest)
    }

    #[must_use]
    pub fn source_settlement(&self, order_digest: &[u8; 32]) -> Option<&SourceSettlementRecordV2> {
        self.projection.source_settlement(order_digest)
    }

    pub fn apply_source_settlement(
        &mut self,
        verified: crate::migration::VerifiedSourceSettlement,
        now: u64,
    ) -> Result<bool, RampError> {
        let settlement = verified.into_record();
        self.require_operational()?;
        let order = self
            .projection
            .orders
            .get(&settlement.order_digest)
            .ok_or(RampError::InvalidOrder)?;
        settlement.validate_order(&order.order)?;
        if let Some(existing) = self
            .projection
            .source_claims
            .get(&settlement.source_claim_id)
        {
            return if existing == &settlement
                && self.projection.source_orders.get(&settlement.order_digest)
                    == Some(&settlement.source_claim_id)
            {
                Ok(false)
            } else {
                Err(RampError::Conflict)
            };
        }
        if self
            .projection
            .source_orders
            .contains_key(&settlement.order_digest)
        {
            return Err(RampError::Conflict);
        }
        self.append(Event::SourceSettlementVerifiedV2 { settlement }, now)?;
        Ok(true)
    }

    #[must_use]
    pub const fn head(&self) -> [u8; 32] {
        self.head
    }

    #[must_use]
    pub const fn record_count(&self) -> u64 {
        self.next_sequence
    }

    #[must_use]
    pub const fn durable_len(&self) -> u64 {
        self.durable_len
    }

    #[must_use]
    pub const fn recovery(&self) -> Option<TornTail> {
        self.recovery
    }

    #[must_use]
    pub const fn halted(&self) -> bool {
        self.halted
    }

    pub fn arm_write_fault(&mut self, step: WriteStep, fault: WriteFault) {
        self.fault = Some((step, fault));
    }

    #[must_use]
    pub const fn armed_write_fault(&self) -> Option<(WriteStep, WriteFault)> {
        self.fault
    }

    /// # Errors
    /// Returns [`RampError::InvalidOrder`] for an unknown order or invalid worker,
    /// [`RampError::LeaseHeld`] when another worker still holds the lease, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn acquire_lease(
        &mut self,
        order_digest: [u8; 32],
        worker_id: &str,
        now: u64,
        lease_seconds: u64,
    ) -> Result<(), RampError> {
        self.require_operational()?;
        if !safe_identifier(worker_id) || lease_seconds == 0 {
            return Err(RampError::InvalidOrder);
        }
        let snapshot = self
            .projection
            .orders
            .get(&order_digest)
            .ok_or(RampError::InvalidOrder)?;
        if snapshot
            .lease
            .as_ref()
            .is_some_and(|(owner, expiry)| *expiry > now && owner != worker_id)
        {
            return Err(RampError::LeaseHeld);
        }
        self.append(
            Event::LeaseAcquired {
                order_digest,
                worker_id: worker_id.to_owned(),
                expires_at: now.saturating_add(lease_seconds),
            },
            now,
        )
    }

    /// # Errors
    /// Returns [`RampError::IllegalTransition`] or [`RampError::Conflict`] when the stage is not admitted,
    /// [`RampError::LeaseHeld`] when the worker does not hold a live lease,
    /// [`RampError::InvalidOrder`] when the order is absent, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn transition(
        &mut self,
        order_digest: [u8; 32],
        expected: WorkflowStage,
        next: WorkflowStage,
        evidence: TransitionEvidence,
        worker_id: &str,
        now: u64,
    ) -> Result<(), RampError> {
        self.require_operational()?;
        if !allowed(expected, next) {
            return Err(RampError::IllegalTransition);
        }
        let snapshot = self
            .projection
            .orders
            .get(&order_digest)
            .ok_or(RampError::InvalidOrder)?;
        if snapshot.stage != expected {
            return Err(RampError::Conflict);
        }
        if !snapshot
            .lease
            .as_ref()
            .is_some_and(|(owner, expires_at)| owner == worker_id && *expires_at > now)
        {
            return Err(RampError::LeaseHeld);
        }
        self.append(
            Event::Transition {
                order_digest,
                expected,
                next,
                evidence,
            },
            now,
        )
    }

    /// # Errors
    /// Returns [`RampError::Provider`] for an invalid callback identity,
    /// [`RampError::Conflict`] when the callback or sequence disagrees with prior facts,
    /// [`RampError::IllegalTransition`] or [`RampError::InvalidOrder`] when the stage cannot be applied, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn apply_provider_callback(
        &mut self,
        write: ProviderCallbackWrite<'_>,
        now: u64,
    ) -> Result<bool, RampError> {
        self.require_operational()?;
        if !safe_identifier(write.callback_id)
            || write.provider_sequence == 0
            || write.evidence_digest == [0; 32]
        {
            return Err(RampError::Provider);
        }
        let identity = CallbackIdentity {
            order_digest: write.order_digest,
            provider_sequence: write.provider_sequence,
            evidence_digest: write.evidence_digest,
        };
        if let Some(existing) = self.projection.callbacks.get(write.callback_id) {
            return if *existing == identity {
                Ok(false)
            } else {
                Err(RampError::Conflict)
            };
        }
        self.append(
            Event::ProviderCallbackApplied {
                order_digest: write.order_digest,
                callback_id: write.callback_id.to_owned(),
                provider_sequence: write.provider_sequence,
                evidence_digest: write.evidence_digest,
                expected: write.expected,
                next: write.next,
                evidence: write.evidence.clone(),
            },
            now,
        )?;
        Ok(true)
    }

    /// # Errors
    /// Returns [`RampError::Paxeer`] when the observation or planned transfer is absent or invalid,
    /// [`RampError::Conflict`] when identifiers disagree with prior facts, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn observe_paxeer(
        &mut self,
        idempotency_key: [u8; 32],
        observation: PaxeerObservation<'_>,
        now: u64,
    ) -> Result<(), RampError> {
        self.require_operational()?;
        if !safe_identifier(observation.operation_id)
            || observation.transaction_hash == [0; 32]
            || !safe_identifier(observation.stage)
        {
            return Err(RampError::Paxeer);
        }
        let existing = self
            .projection
            .paxeer
            .get(&idempotency_key)
            .ok_or(RampError::Paxeer)?;
        if existing
            .operation_id
            .as_ref()
            .is_some_and(|value| value != observation.operation_id)
            || existing
                .transaction_hash
                .is_some_and(|value| value != observation.transaction_hash)
        {
            return Err(RampError::Conflict);
        }
        self.append(
            Event::PaxeerObserved {
                idempotency_key,
                operation_id: observation.operation_id.to_owned(),
                transaction_hash: observation.transaction_hash,
                stage: observation.stage.to_owned(),
                block_hash: observation.block_hash,
                confirmations: observation.confirmations,
            },
            now,
        )
    }

    /// # Errors
    /// Returns [`RampError::Paxeer`] for a zero key, asset or amount,
    /// [`RampError::Conflict`] when the key is reused with different terms, and
    /// [`RampError::Journal`] when the durable append cannot complete.
    pub fn plan_paxeer(
        &mut self,
        idempotency_key: [u8; 32],
        asset: [u8; 32],
        amount: u128,
        now: u64,
    ) -> Result<(), RampError> {
        self.require_operational()?;
        if idempotency_key == [0; 32] || asset == [0; 32] || amount == 0 {
            return Err(RampError::Paxeer);
        }
        if let Some(existing) = self.projection.paxeer.get(&idempotency_key) {
            return if existing.asset == asset && existing.amount == amount {
                Ok(())
            } else {
                Err(RampError::Conflict)
            };
        }
        self.append(
            Event::PaxeerPlanned {
                idempotency_key,
                asset,
                amount,
            },
            now,
        )
    }

    #[must_use]
    pub fn paxeer(&self, idempotency_key: &[u8; 32]) -> Option<&PaxeerSnapshot> {
        self.projection.paxeer(idempotency_key)
    }

    fn append(&mut self, event: Event, recorded_at: u64) -> Result<(), RampError> {
        self.require_operational()?;
        let staged = self.projection.stage(&event)?;
        let next_sequence = self
            .next_sequence
            .checked_add(1)
            .ok_or(RampError::Journal)?;
        let body = RecordBody {
            sequence: self.next_sequence,
            previous_hash: self.head,
            recorded_at,
            event,
        };
        let record_hash = record_digest(&body)?;
        let record = Record {
            sequence: body.sequence,
            previous_hash: body.previous_hash,
            recorded_at: body.recorded_at,
            event: body.event,
            hash: record_hash,
        };
        let mut bytes = serde_json::to_vec(&record).map_err(|_| RampError::Journal)?;
        if bytes.len().saturating_add(1) > MAX_JOURNAL_RECORD_BYTES {
            return Err(RampError::Journal);
        }
        bytes.push(b'\n');
        let durable_len = self
            .durable_len
            .checked_add(bytes.len() as u64)
            .ok_or(RampError::Journal)?;
        if self.begin_intent(&bytes).is_err() {
            self.halted = true;
            return Err(RampError::Journal);
        }
        match self.write_record(&bytes) {
            Ok(()) => {}
            Err(WriteFailure::Failed) => {
                self.halted = true;
                return Err(RampError::Journal);
            }
            Err(WriteFailure::Interrupted) => {
                self.halted = true;
                return Err(RampError::Journal);
            }
        }
        let modified = match self
            .file
            .metadata()
            .and_then(|metadata| metadata.modified())
        {
            Ok(modified) => modified,
            Err(_) => {
                self.halted = true;
                return Err(RampError::Journal);
            }
        };
        if self.clear_intent().is_err() {
            self.halted = true;
            return Err(RampError::Journal);
        }
        self.projection.commit(staged);
        self.next_sequence = next_sequence;
        self.previous_head = self.head;
        self.head = record_hash;
        self.durable_len = durable_len;
        self.durable_modified = Some(modified);
        Ok(())
    }

    fn write_record(&mut self, bytes: &[u8]) -> Result<(), WriteFailure> {
        self.checkpoint(WriteStep::BeforeRecord)?;
        let terminator = bytes.len().saturating_sub(1);
        let written = match self.fault {
            Some((step @ WriteStep::DuringRecord, _)) => {
                self.write_prefix(bytes, terminator / 2, step)?
            }
            Some((step @ WriteStep::BeforeTerminator, _)) => {
                self.write_prefix(bytes, terminator, step)?
            }
            _ => 0,
        };
        self.file
            .write_all(&bytes[written..])
            .map_err(|_| WriteFailure::Failed)?;
        self.checkpoint(WriteStep::AfterRecord)?;
        self.file.sync_data().map_err(|_| WriteFailure::Failed)?;
        self.checkpoint(WriteStep::AfterSync)
    }

    fn write_prefix(
        &mut self,
        bytes: &[u8],
        cut: usize,
        step: WriteStep,
    ) -> Result<usize, WriteFailure> {
        self.file
            .write_all(&bytes[..cut])
            .map_err(|_| WriteFailure::Failed)?;
        self.checkpoint(step)?;
        Ok(cut)
    }

    fn checkpoint(&mut self, step: WriteStep) -> Result<(), WriteFailure> {
        match self.fault {
            Some((armed, fault)) if armed == step => {
                self.fault = None;
                Err(match fault {
                    WriteFault::Fail => WriteFailure::Failed,
                    WriteFault::Interrupt => WriteFailure::Interrupted,
                })
            }
            _ => Ok(()),
        }
    }
}

fn same_file(file: &File, path: &Path) -> bool {
    let Ok(opened) = file.metadata() else {
        return false;
    };
    let Ok(named) = std::fs::symlink_metadata(path) else {
        return false;
    };
    if !named.is_file() || require_private_file(file).is_err() {
        return false;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt as _;
        opened.dev() == named.dev()
            && opened.ino() == named.ino()
            && opened.nlink() == 1
            && named.nlink() == 1
    }
    #[cfg(not(unix))]
    {
        opened.len() == named.len() && opened.modified().ok() == named.modified().ok()
    }
}

struct LockClaim {
    identity_path: std::path::PathBuf,
    path: Option<std::path::PathBuf>,
    _file: File,
}

impl LockClaim {
    fn held(&self) -> bool {
        same_file(&self._file, &self.identity_path)
    }

    fn acquire(journal: &Path) -> Result<Self, RampError> {
        let mut path = journal.as_os_str().to_owned();
        path.push(".writer-lock");
        let path = std::path::PathBuf::from(path);
        let file = claim_lock(&path)?;
        file.sync_data().map_err(|_| RampError::Journal)?;
        Ok(Self {
            identity_path: path.clone(),
            path: if cfg!(unix) { None } else { Some(path) },
            _file: file,
        })
    }
}

impl Drop for LockClaim {
    fn drop(&mut self) {
        if let Some(path) = &self.path {
            if let Err(error) = std::fs::remove_file(path) {
                eprintln!("ramp journal writer lock retained: {error}");
            }
        }
    }
}

#[cfg(unix)]
fn claim_lock(path: &Path) -> Result<File, RampError> {
    use std::os::unix::fs::OpenOptionsExt as _;
    let file = OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .mode(0o600)
        .open(path)
        .map_err(|_| RampError::Journal)?;
    require_private_file(&file)?;
    rustix::fs::flock(&file, rustix::fs::FlockOperation::NonBlockingLockExclusive)
        .map_err(|_| RampError::Journal)?;
    Ok(file)
}

#[cfg(not(unix))]
fn claim_lock(path: &Path) -> Result<File, RampError> {
    OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(path)
        .map_err(|_| RampError::Journal)
}

#[cfg(unix)]
fn open_journal(path: &Path) -> Result<File, RampError> {
    use std::os::unix::fs::OpenOptionsExt as _;
    OpenOptions::new()
        .create(true)
        .read(true)
        .append(true)
        .mode(0o600)
        .open(path)
        .map_err(|_| RampError::Journal)
}

#[cfg(not(unix))]
fn open_journal(path: &Path) -> Result<File, RampError> {
    OpenOptions::new()
        .create(true)
        .read(true)
        .append(true)
        .open(path)
        .map_err(|_| RampError::Journal)
}

fn record_digest(body: &RecordBody) -> Result<[u8; 32], RampError> {
    let bytes = serde_json::to_vec(body).map_err(|_| RampError::Journal)?;
    let mut hasher = Sha256::new();
    hasher.update(JOURNAL_DOMAIN);
    hasher.update(bytes);
    Ok(hasher.finalize().into())
}

fn merge_evidence(target: &mut TransitionEvidence, value: &TransitionEvidence) {
    if value.provider_operation_id.is_some() {
        target
            .provider_operation_id
            .clone_from(&value.provider_operation_id);
    }
    if value.provider_evidence_digest.is_some() {
        target.provider_evidence_digest = value.provider_evidence_digest;
    }
    if value.activity_id.is_some() {
        target.activity_id = value.activity_id;
    }
    if value.canonical_activity.is_some() {
        target
            .canonical_activity
            .clone_from(&value.canonical_activity);
    }
    if value.receipt_digest.is_some() {
        target.receipt_digest = value.receipt_digest;
    }
    if value.refusal_code.is_some() {
        target.refusal_code.clone_from(&value.refusal_code);
    }
    if value.retry_at.is_some() {
        target.retry_at = value.retry_at;
    }
}

fn evidence_conflicts(existing: &TransitionEvidence, incoming: &TransitionEvidence) -> bool {
    existing
        .provider_operation_id
        .as_ref()
        .zip(incoming.provider_operation_id.as_ref())
        .is_some_and(|(current, next)| current != next && !current.starts_with("idempotency:"))
        || existing
            .activity_id
            .zip(incoming.activity_id)
            .is_some_and(|(current, next)| current != next)
        || existing
            .canonical_activity
            .as_ref()
            .zip(incoming.canonical_activity.as_ref())
            .is_some_and(|(current, next)| current != next)
        || existing
            .receipt_digest
            .zip(incoming.receipt_digest)
            .is_some_and(|(current, next)| current != next)
}

fn completion_missing(evidence: &TransitionEvidence) -> bool {
    evidence.activity_id.is_none()
        || evidence.canonical_activity.is_none()
        || evidence.receipt_digest.is_none()
        || evidence.provider_operation_id.is_none()
        || evidence.provider_evidence_digest.is_none()
}

fn safe_identifier(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b':'))
}

fn validate_resulting_evidence(
    stage: WorkflowStage,
    existing: &TransitionEvidence,
    incoming: &TransitionEvidence,
) -> Result<(), RampError> {
    let mut evidence = existing.clone();
    merge_evidence(&mut evidence, incoming);
    let provider_operation = evidence
        .provider_operation_id
        .as_deref()
        .is_some_and(safe_identifier);
    let provider_digest = evidence
        .provider_evidence_digest
        .is_some_and(|value| value != [0; 32]);
    let activity = evidence.activity_id.is_some_and(|value| value != [0; 32]);
    let canonical_activity = evidence
        .canonical_activity
        .as_ref()
        .is_some_and(|value| !value.is_empty() && value.len() <= 1024 * 1024);
    let receipt = evidence
        .receipt_digest
        .is_some_and(|value| value != [0; 32]);
    let refusal = evidence
        .refusal_code
        .as_deref()
        .is_some_and(safe_identifier);
    let valid = match stage {
        WorkflowStage::ProviderSubmissionPlanned
        | WorkflowStage::ProviderSubmittedUnknown
        | WorkflowStage::ProviderPending => {
            provider_operation && evidence.retry_at.is_some_and(|retry| retry != 0)
        }
        WorkflowStage::ProviderSettled | WorkflowStage::ProviderReversed => {
            provider_operation && provider_digest
        }
        WorkflowStage::ProviderRefused => provider_operation && refusal,
        WorkflowStage::LayerxSubmittedUnknown | WorkflowStage::LayerxPending => {
            activity && canonical_activity
        }
        WorkflowStage::LayerxSubmissionPlanned => activity && canonical_activity,
        WorkflowStage::LayerxVerified | WorkflowStage::Done => {
            activity && canonical_activity && receipt
        }
        WorkflowStage::ComplianceRefused | WorkflowStage::LayerxRefused => refusal,
        _ => true,
    };
    if valid {
        Ok(())
    } else {
        Err(RampError::IllegalTransition)
    }
}

const fn allowed(from: WorkflowStage, to: WorkflowStage) -> bool {
    use WorkflowStage as S;
    matches!(
        (from, to),
        (
            S::CompliancePending,
            S::ManualReview
                | S::ComplianceRefused
                | S::AwaitingExternalCredit
                | S::AwaitingLayerxPayment
        ) | (
            S::ManualReview,
            S::ComplianceRefused
                | S::AwaitingExternalCredit
                | S::AwaitingLayerxPayment
                | S::ProviderSubmittedUnknown
                | S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
                | S::ProviderReversed
                | S::ManualReview
        ) | (
            S::AwaitingExternalCredit,
            S::ProviderSubmissionPlanned
                | S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
        ) | (
            S::ProviderSubmissionPlanned,
            S::ProviderSubmittedUnknown
                | S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
                | S::ManualReview
        ) | (
            S::ProviderSubmittedUnknown,
            S::ProviderSubmittedUnknown
                | S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
                | S::ProviderReversed
                | S::ManualReview
        ) | (
            S::ProviderPending,
            S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
                | S::ProviderReversed
                | S::ManualReview
        ) | (
            S::ProviderSettled,
            S::LayerxSubmissionPlanned | S::LayerxPending | S::ProviderReversed | S::Done
        ) | (
            S::SourceSettledV2,
            S::LayerxSubmissionPlanned | S::LayerxPending
        ) | (
            S::AwaitingLayerxPayment,
            S::LayerxSubmissionPlanned | S::LayerxPending | S::LayerxVerified | S::LayerxRefused
        ) | (
            S::LayerxSubmissionPlanned | S::LayerxSubmittedUnknown,
            S::LayerxSubmittedUnknown | S::LayerxPending | S::LayerxVerified | S::LayerxRefused
        ) | (
            S::LayerxPending,
            S::LayerxPending | S::LayerxVerified | S::LayerxRefused
        ) | (
            S::LayerxVerified,
            S::ProviderSubmissionPlanned
                | S::ProviderPending
                | S::ProviderSettled
                | S::ProviderRefused
                | S::Done
        ) | (S::Done, S::ProviderReversed | S::ReversalPending)
            | (
                S::ProviderReversed | S::ReversalPending,
                S::ReversalPending | S::Reversed
            )
    )
}

#[cfg(unix)]
fn require_private_file(file: &File) -> Result<(), RampError> {
    use std::os::unix::fs::PermissionsExt as _;
    if file
        .metadata()
        .map_err(|_| RampError::Journal)?
        .permissions()
        .mode()
        & 0o077
        != 0
    {
        return Err(RampError::Journal);
    }
    Ok(())
}

#[cfg(not(unix))]
fn require_private_file(_file: &File) -> Result<(), RampError> {
    Ok(())
}
