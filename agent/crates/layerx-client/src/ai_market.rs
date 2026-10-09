//! AI market client: proof-bound finalized views, operation planning over the existing
//! `NativeProgramCall`, owner review and signing, and durable exact-byte submission with
//! receipt and checkpoint resolution.

use std::fs::{self, File};
use std::io::{self, Write as _};
use std::path::{Path, PathBuf};

use layerx_crypto::disclosure::{self, DisclosedNativeOperation, Disclosure, DisclosureError};
use layerx_crypto::signer::{sign_disclosed, SignError, Signer};
use layerx_crypto::{ed25519, SignatureMessage, VerifyError};
use layerx_programs_ai_market::codec::{
    decode_envelope, decode_roster, encode_envelope, Envelope, RosterView,
};
use layerx_programs_ai_market::dispatch::{self, CallBoundary, Operation};
use layerx_programs_ai_market::errors::{
    ApplicationError, F06_CONTRIBUTION_CONSENT_REQUIRED, F06_FUNDING_POLICY_MISMATCH,
    F06_INVALID_AMOUNT, F06_NOTHING_TO_CLAIM, F06_REFUND_RECIPIENT_MISMATCH,
    F06_UNKNOWN_WORKER_ENTITLEMENT, F06_WRONG_CLAIM_RECIPIENT, NON_CANONICAL, NOT_FOUND,
    UNAUTHORIZED, WRONG_CONFIG, WRONG_MARKET, WRONG_ROSTER,
};
use layerx_programs_ai_market::policy::{TaskPolicyV1, TASK_POLICY_BYTES};
use layerx_programs_ai_market::queries::{
    bind_snapshot, participant_rows, read_header, Availability, CaptureFacts, FinalityEvidence,
    ParticipantKind, ParticipantRow, QueryError, ReadProof, RewardField, ScoreField, ScoreStatus,
    SnapshotBinding, StateCapture, FINALIZED_RANK,
};
use layerx_programs_ai_market::registry::{market_clock, MarketClock, MarketHeader};
use layerx_programs_ai_market::registry_ops::PolicySection;
use layerx_programs_ai_market::rewards::{
    ClaimRequest, FundRequest, RefundRequest, FUNDING_POLICY_VERSION,
};
use layerx_programs_ai_market::state::{decode_shared_state, Section};
use layerx_programs_ai_market::types::{
    AccountId, AssetId, Authentication, ChainDomain, Digest32, MarketId, PolicyDigest, Presence,
    PrincipalId, ProgramId, RequestDigest, RequestId, RosterDigest, Version, WorkerId,
    WorkerRosterEntry,
};
use layerx_programs_ai_market::{MAX_ENVELOPE_BYTES, MAX_EVALUATORS, MAX_STATE_BYTES, MAX_WORKERS};
use layerx_proof::checkpoint::{verify_declared_certificate, Certificate, CheckpointError};
use layerx_types::activity::{
    ActivityBuildError, Authority, EnvelopeBuilder, Signature, TimestampBound, UnsignedEnvelope,
};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey, LengthError};
use layerx_types::intent;
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistry, Payload, PayloadError};
use layerx_types::program_call::{InvalidNativeCall, NativeProgramCall, Resources};
use layerx_types::result::{KnownResult, Retriability};
use layerx_types::verify::VerificationLevel;
use layerx_wire::activity::{
    decode_signed, encode_signed, encode_signed_envelope, encode_unsigned, encode_unsigned_envelope,
};
use layerx_wire::hash::{activity_id, payload_hash_for, Domain};
use layerx_wire::WireError;

use crate::lni::schema::Version as InterfaceVersion;
use crate::lni::transport::FrameTransport;
use crate::receipt::{
    lookup_authenticated, AuthenticatedLookup, AuthenticatedLookupContext, ReceiptError,
};
use crate::submit::{submit_signed, Submission, SubmissionContext, SubmitError};

/// F02 live-authority freshness bound: a snapshot lagging verified authority by more refuses.
pub const AUTHORITY_FRESHNESS_HEIGHTS: u64 = 8;
/// Settlement-anchored rank; displayed only when actually achieved, never by default.
pub const SETTLEMENT_RANK: u8 = 5;
/// Guest ABI of the AI market program call.
pub const GUEST_ABI: u16 = 5;
/// Exported guest entrypoint receiving the canonical PAXAI envelope as calldata.
pub const ENTRYPOINT: &[u8] = b"layerx_call";
/// Protocol version of native program-call activities.
pub const NATIVE_CALL_PROTOCOL_VERSION: u16 = 3;
/// Programs module activity ordinal of a native program call.
pub const PROGRAM_CALL_ORDINAL: u16 = 3;

const RECORD_MAGIC: &[u8; 8] = b"PAXAIOP1";
const RECORD_EXTENSION: &str = "op";
const HEX: &[u8; 16] = b"0123456789abcdef";

/// Typed client refusal; every variant keeps the exact refusal of the boundary it came from.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AiClientError {
    Query(QueryError),
    Checkpoint(CheckpointError),
    StaleAuthority { lag: u64, limit: u64 },
    NativeCall(InvalidNativeCall),
    Activity(ActivityBuildError),
    Actor(LengthError),
    Payload(PayloadError),
    Wire(WireError),
    Disclosure(DisclosureError),
    NotNativeProgramCall,
    NotMutation,
    ReviewMismatch(ReviewField),
    UnauthorizedKey,
    Sign(SignError),
    Signature(VerifyError),
    Submit(SubmitError),
    Receipt(ReceiptError),
    InvalidTransition { from: OperationState },
    ReceiptMismatch,
    FinalityMismatch,
    HistoryUnavailable(EpochStatus),
    Journal(io::ErrorKind),
    CorruptRecord,
}

macro_rules! from_refusal {
    ($($source:ty => $variant:ident),+ $(,)?) => {
        $(impl From<$source> for AiClientError {
            fn from(error: $source) -> Self {
                Self::$variant(error)
            }
        })+
    };
}

from_refusal!(
    QueryError => Query,
    CheckpointError => Checkpoint,
    InvalidNativeCall => NativeCall,
    ActivityBuildError => Activity,
    LengthError => Actor,
    PayloadError => Payload,
    WireError => Wire,
    DisclosureError => Disclosure,
    SignError => Sign,
    VerifyError => Signature,
    SubmitError => Submit,
    ReceiptError => Receipt,
);

impl From<ApplicationError> for AiClientError {
    fn from(error: ApplicationError) -> Self {
        Self::Query(QueryError::Application(error))
    }
}

impl From<io::Error> for AiClientError {
    fn from(error: io::Error) -> Self {
        Self::Journal(error.kind())
    }
}

/// Facts of a checkpoint certificate verified at checkpoint-finalised level.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CheckpointFinality {
    network_id: u32,
    checkpoint: Digest32,
    state_root: Digest32,
    first_sequence: u64,
    last_sequence: u64,
    batch_number: u64,
}

impl CheckpointFinality {
    /// Verifies a certificate against its declared settlement domain and registered id.
    ///
    /// # Errors
    /// Checkpoint verifier refusals, and `FinalityUnavailable` below checkpoint-finalised level
    /// or without a checkpoint identifier.
    pub fn verify(
        certificate: &Certificate,
        domain: &str,
        checkpoint_id: &[u8; 32],
    ) -> Result<Self, AiClientError> {
        let report = verify_declared_certificate(certificate, domain, checkpoint_id, None)?;
        if report.level() < VerificationLevel::CHECKPOINT_FINALISED {
            return Err(QueryError::FinalityUnavailable.into());
        }
        let checkpoint = report
            .evidence()
            .checkpoint_id()
            .ok_or(QueryError::FinalityUnavailable)?;
        Ok(Self {
            network_id: report.network_id(),
            checkpoint: Digest32::new(checkpoint)?,
            state_root: Digest32::new(report.resulting_state_root())?,
            first_sequence: report.first_sequence(),
            last_sequence: report.last_sequence(),
            batch_number: report.batch_number(),
        })
    }

    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }

    #[must_use]
    pub const fn checkpoint(&self) -> Digest32 {
        self.checkpoint
    }

    #[must_use]
    pub const fn state_root(&self) -> Digest32 {
        self.state_root
    }

    #[must_use]
    pub const fn first_sequence(&self) -> u64 {
        self.first_sequence
    }

    #[must_use]
    pub const fn last_sequence(&self) -> u64 {
        self.last_sequence
    }

    #[must_use]
    pub const fn batch_number(&self) -> u64 {
        self.batch_number
    }

    const fn covers(&self, sequence: u64) -> bool {
        self.first_sequence <= sequence && sequence <= self.last_sequence
    }
}

/// Age of a snapshot against independently verified live authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Freshness {
    Current,
    Stale { lag: u64 },
    Unknown,
}

impl Freshness {
    #[must_use]
    pub const fn code(self) -> u8 {
        match self {
            Self::Current => 1,
            Self::Stale { .. } => 2,
            Self::Unknown => 3,
        }
    }

    const fn of(execution_height: u64, authority_height: Option<u64>) -> Self {
        match authority_height {
            None => Self::Unknown,
            Some(authority) => {
                let lag = authority.saturating_sub(execution_height);
                if lag > AUTHORITY_FRESHNESS_HEIGHTS {
                    Self::Stale { lag }
                } else {
                    Self::Current
                }
            }
        }
    }
}

/// A complete same-root `ProgramRead` capture; evidence-verified but not yet finalized.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObservedSnapshot {
    state: Vec<u8>,
    facts: CaptureFacts,
}

impl ObservedSnapshot {
    /// Reassembles every chunk of one root in offset order and checks the market header.
    ///
    /// # Errors
    /// Capture refusals (`SnapshotConflict`, `IntegrityFailure`, `CAPACITY`) and header refusals.
    pub fn capture(chunks: &[(ReadProof, Vec<u8>)]) -> Result<Self, AiClientError> {
        let mut buffer = vec![0; MAX_STATE_BYTES];
        let mut capture = StateCapture::new(&mut buffer);
        for (proof, body) in chunks {
            capture.accept(proof, body)?;
        }
        let (bytes, facts) = capture.finish()?;
        let state = bytes.to_vec();
        read_header(&state, facts.proof.chain, facts.proof.program)?;
        Ok(Self { state, facts })
    }

    #[must_use]
    pub const fn facts(&self) -> &CaptureFacts {
        &self.facts
    }

    /// Promotes the capture only with checkpoint-finalised evidence for the same root.
    ///
    /// # Errors
    /// `FinalityUnavailable` without evidence, `BindingMismatch` for evidence over another root
    /// or a checkpoint that does not cover the observed sequence, and binding refusals.
    pub fn finalize(
        self,
        finality: Option<&CheckpointFinality>,
        publication_time_ms: u64,
    ) -> Result<FinalizedSnapshot, AiClientError> {
        let finality = finality.ok_or(QueryError::FinalityUnavailable)?;
        if finality.state_root != self.facts.proof.native_state_root
            || !finality.covers(self.facts.proof.observed_sequence)
        {
            return Err(QueryError::BindingMismatch.into());
        }
        let evidence = FinalityEvidence {
            native_state_root: finality.state_root,
            checkpoint: finality.checkpoint,
            settlement: Presence::Absent,
            rank: FINALIZED_RANK,
        };
        let binding = bind_snapshot(&self.state, &self.facts, &evidence, publication_time_ms)?;
        binding.require_finalized()?;
        let snapshot_id = binding.snapshot_id()?;
        let header = market_header(&self.state)?;
        Ok(FinalizedSnapshot {
            state: self.state,
            header,
            binding,
            snapshot_id,
        })
    }
}

fn market_header(state: &[u8]) -> Result<MarketHeader, AiClientError> {
    let shared = decode_shared_state(state)?;
    Ok(PolicySection::decode(shared.section(Section::PolicyLifecycle)?)?.header)
}

/// Public inspection of a finalized snapshot; staleness is labelled, never hidden.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Inspection {
    pub binding: SnapshotBinding,
    pub snapshot_id: Digest32,
    pub freshness: Freshness,
    pub clock: MarketClock,
}

/// A rank-4 snapshot bound to its exact root, state and checkpoint.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FinalizedSnapshot {
    state: Vec<u8>,
    header: MarketHeader,
    binding: SnapshotBinding,
    snapshot_id: Digest32,
}

/// Exact common request of one F01..F09 mutation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct OperationRequest<'a> {
    pub operation: Operation,
    pub payload: &'a [u8],
    pub actor: PrincipalId,
    pub roster: Option<&'a [u8]>,
    pub sequence: u64,
    pub expiry: u64,
    pub request: RequestId,
    pub required_rank: u8,
}

/// Native activity terms selected by the caller; every field is shown at review.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NativeTerms<'a> {
    pub network_id: u32,
    pub actor_did: &'a [u8],
    pub owner_public_key: [u8; 32],
    pub account_sequence: u64,
    pub idempotency_key: [u8; 32],
    pub not_before: u64,
    pub not_after: u64,
    pub fee_limit: u128,
    pub capabilities: &'a [u8],
    pub access_declaration: &'a [u8],
    pub response_capacity: u32,
    pub resources: Resources,
}

/// Previewed semantic effect; advisory, re-checked by the program at execution.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum OperationEffect {
    Fund {
        amount: u128,
        asset: AssetId,
        account: AccountId,
        refund_recipient: AccountId,
    },
    Claim {
        worker: WorkerId,
        recipient: AccountId,
        amount: u128,
        asset: AssetId,
    },
    RefundFree {
        expected_refunded: u128,
        amount: u128,
        recipient: AccountId,
        asset: AssetId,
    },
    Other {
        selector: u16,
    },
}

impl FinalizedSnapshot {
    #[must_use]
    pub const fn binding(&self) -> &SnapshotBinding {
        &self.binding
    }

    #[must_use]
    pub const fn snapshot_id(&self) -> Digest32 {
        self.snapshot_id
    }

    #[must_use]
    pub const fn header(&self) -> &MarketHeader {
        &self.header
    }

    #[must_use]
    pub fn state_bytes(&self) -> &[u8] {
        &self.state
    }

    /// Exact snapshot with its freshness label and the epoch clock at its execution height.
    ///
    /// # Errors
    /// `ARITHMETIC` when the execution height precedes the market origin.
    pub fn inspect(&self, authority_height: Option<u64>) -> Result<Inspection, AiClientError> {
        Ok(Inspection {
            binding: self.binding,
            snapshot_id: self.snapshot_id,
            freshness: Freshness::of(self.binding.execution_height, authority_height),
            clock: market_clock(self.header.origin_height, self.binding.execution_height)?,
        })
    }

    /// Refuses routing, signing or release decisions on stale authority.
    ///
    /// # Errors
    /// `StaleAuthority` when verified authority leads the snapshot by more than the bound.
    pub fn require_current(&self, authority_height: u64) -> Result<(), AiClientError> {
        match Freshness::of(self.binding.execution_height, Some(authority_height)) {
            Freshness::Stale { lag } => Err(AiClientError::StaleAuthority {
                lag,
                limit: AUTHORITY_FRESHNESS_HEIGHTS,
            }),
            Freshness::Current | Freshness::Unknown => Ok(()),
        }
    }

    /// One participant row by stable identifier; a reused seat never inherits another id.
    ///
    /// # Errors
    /// `NOT_FOUND` for an unknown identifier, `WRONG_MARKET`/`WRONG_CONFIG` for a foreign
    /// roster, and row derivation refusals.
    pub fn participant(
        &self,
        kind: ParticipantKind,
        id: [u8; 32],
        roster: Option<&[u8]>,
    ) -> Result<ParticipantRow, AiClientError> {
        let view = roster.map(decode_roster).transpose()?;
        if let Some(view) = &view {
            self.check_roster(view)?;
        }
        let filler = ParticipantRow {
            kind,
            id,
            owner: self.header.owner_principal,
            generation: 0,
            identity_state: 0,
            frozen_member: false,
            frozen_generation: Presence::Absent,
            eligibility: 0,
            metadata: Presence::Absent,
            metadata_revision: 0,
            score: ScoreField::absent(Presence::Absent, ScoreStatus::NotProduced)?,
            reward: RewardField::unavailable(Availability::NotEnabled)?,
            history_status: Availability::NotYetProduced,
            history: Presence::Absent,
        };
        let mut rows = vec![filler; MAX_WORKERS + MAX_EVALUATORS];
        let count = participant_rows(&self.state, view.as_ref(), &mut rows)?;
        rows.truncate(count);
        rows.into_iter()
            .find(|row| row.kind == kind && row.id == id)
            .ok_or_else(|| NOT_FOUND.into())
    }

    fn check_roster(&self, view: &RosterView<'_>) -> Result<(), AiClientError> {
        if view.market != self.binding.market {
            return Err(WRONG_MARKET.into());
        }
        if view.config != self.binding.config {
            return Err(WRONG_CONFIG.into());
        }
        Ok(())
    }

    /// Plans one mutation over this finalized context: the canonical PAXAI envelope becomes
    /// the calldata of a `NativeProgramCall` inside an unsigned protocol-3 activity.
    ///
    /// # Errors
    /// `StaleAuthority`, `FinalityUnavailable` for an unmet rank, `NotMutation` for a read
    /// selector, envelope/roster refusals, typed preview refusals and native encoding refusals.
    pub fn prepare(
        &self,
        registry: &ModuleRegistry,
        authority_height: u64,
        request: &OperationRequest<'_>,
        terms: &NativeTerms<'_>,
    ) -> Result<PreparedOperation, AiClientError> {
        self.require_current(authority_height)?;
        if !(FINALIZED_RANK..=SETTLEMENT_RANK).contains(&request.required_rank) {
            return Err(NON_CANONICAL.into());
        }
        if request.required_rank > self.binding.rank {
            return Err(QueryError::FinalityUnavailable.into());
        }
        if request.operation.metadata().boundary != CallBoundary::Mutation {
            return Err(AiClientError::NotMutation);
        }
        let roster = request.roster.map(decode_roster).transpose()?;
        let (epoch, roster_digest) = match &roster {
            Some(view) => {
                self.check_roster(view)?;
                (view.epoch, Presence::Present(view.digest()?))
            }
            None => (0, Presence::Absent),
        };
        let envelope = Envelope {
            operation: request.operation,
            chain: self.binding.chain,
            program: self.binding.program,
            market: self.binding.market,
            actor: request.actor,
            epoch,
            config: self.binding.config.get(),
            roster: roster_digest,
            sequence: request.sequence,
            expiry: request.expiry,
            request: request.request,
            payload: request.payload,
            authentication: Authentication::Native,
        };
        let mut calldata = vec![0; MAX_ENVELOPE_BYTES];
        let length = encode_envelope(&envelope, &mut calldata)?;
        calldata.truncate(length);
        let intent = decode_envelope(&calldata)?.request_digest()?;
        let effect = preview(&self.header, &envelope, roster.as_ref())?;
        let call = NativeProgramCall {
            program_id: intent::ProgramId::new(self.binding.program.bytes()),
            guest_abi: GUEST_ABI,
            entrypoint: ENTRYPOINT,
            calldata: &calldata,
            capabilities: terms.capabilities,
            access_declaration: terms.access_declaration,
            response_capacity: terms.response_capacity,
            resources: terms.resources,
        }
        .encode()?;
        let unsigned = unsigned_activity(registry, terms, &call)?;
        let canonical = encode_unsigned_envelope(&unsigned)?;
        Ok(PreparedOperation {
            unsigned,
            canonical,
            binding: self.binding,
            snapshot_id: self.snapshot_id,
            header: self.header,
            intent,
            effect,
        })
    }
}

fn unsigned_activity(
    registry: &ModuleRegistry,
    terms: &NativeTerms<'_>,
    call: &[u8],
) -> Result<UnsignedEnvelope, AiClientError> {
    let activity_type = ActivityType::new(ModuleId::Programs, PROGRAM_CALL_ORDINAL)?;
    let payload = Payload::new(registry, activity_type, call)?;
    let payload_hash = payload_hash_for(&payload)?;
    let mut builder = EnvelopeBuilder::new();
    builder
        .protocol_version(NATIVE_CALL_PROTOCOL_VERSION)?
        .network_id(terms.network_id)?
        .activity_type(activity_type)?
        .actor_did(Did::new(terms.actor_did)?)?
        .authority(Authority::owner(&terms.owner_public_key)?)?
        .account_sequence(terms.account_sequence)?
        .timestamp_bound(TimestampBound::new(terms.not_before, terms.not_after)?)?
        .idempotency_key(IdempotencyKey::new(terms.idempotency_key))?
        .fee_limit(Amount::from_u128(terms.fee_limit))?
        .payload_hash(payload_hash)?
        .payload(payload)?;
    Ok(builder.build()?)
}

/// Semantic effect decoded from the exact payload, without any precondition.
fn effect_of(
    header: &MarketHeader,
    envelope: &Envelope<'_>,
) -> Result<OperationEffect, AiClientError> {
    let asset = header.funding_asset;
    Ok(match envelope.operation {
        dispatch::FUND => {
            let fund = FundRequest::decode(envelope.payload)?;
            OperationEffect::Fund {
                amount: fund.amount,
                asset,
                account: header.rewards_account,
                refund_recipient: fund.refund_recipient,
            }
        }
        dispatch::CLAIM => {
            let claim = ClaimRequest::decode(envelope.payload)?;
            OperationEffect::Claim {
                worker: claim.worker,
                recipient: claim.recipient,
                amount: claim.amount,
                asset,
            }
        }
        dispatch::REFUND_FREE => {
            let refund = RefundRequest::decode(envelope.payload)?;
            OperationEffect::RefundFree {
                expected_refunded: refund.expected_refunded,
                amount: refund.amount,
                recipient: refund.recipient,
                asset,
            }
        }
        other => OperationEffect::Other {
            selector: other.selector(),
        },
    })
}

/// Advisory preview in the program's own refusal order for the checks it can see.
fn preview(
    header: &MarketHeader,
    envelope: &Envelope<'_>,
    roster: Option<&RosterView<'_>>,
) -> Result<OperationEffect, AiClientError> {
    let effect = effect_of(header, envelope)?;
    match effect {
        OperationEffect::Fund {
            amount,
            refund_recipient,
            ..
        } => {
            let fund = FundRequest::decode(envelope.payload)?;
            if envelope.actor != header.owner_principal
                && header.treasury_principal != Presence::Present(envelope.actor)
            {
                return Err(UNAUTHORIZED.into());
            }
            if fund.policy_version != FUNDING_POLICY_VERSION {
                return Err(F06_FUNDING_POLICY_MISMATCH.into());
            }
            if !fund.consent {
                return Err(F06_CONTRIBUTION_CONSENT_REQUIRED.into());
            }
            if refund_recipient != header.refund_recipient_account {
                return Err(F06_REFUND_RECIPIENT_MISMATCH.into());
            }
            if amount == 0 {
                return Err(F06_INVALID_AMOUNT.into());
            }
        }
        OperationEffect::Claim {
            worker,
            recipient,
            amount,
            ..
        } => {
            let entry = roster_worker(roster.ok_or(WRONG_ROSTER)?, worker)?
                .ok_or(F06_UNKNOWN_WORKER_ENTITLEMENT)?;
            if recipient != entry.recipient {
                return Err(F06_WRONG_CLAIM_RECIPIENT.into());
            }
            if amount == 0 {
                return Err(F06_NOTHING_TO_CLAIM.into());
            }
        }
        OperationEffect::RefundFree {
            amount, recipient, ..
        } => {
            if recipient != header.refund_recipient_account {
                return Err(F06_REFUND_RECIPIENT_MISMATCH.into());
            }
            if amount == 0 {
                return Err(F06_INVALID_AMOUNT.into());
            }
        }
        OperationEffect::Other { .. } => {}
    }
    Ok(effect)
}

fn roster_worker(
    view: &RosterView<'_>,
    worker: WorkerId,
) -> Result<Option<WorkerRosterEntry>, AiClientError> {
    for index in 0..view.worker_count() {
        let entry = view.worker(index)?;
        if entry.worker == worker {
            return Ok(Some(entry));
        }
    }
    Ok(None)
}

/// Planned operation: exact unsigned activity bytes plus the binding they were planned on.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PreparedOperation {
    unsigned: UnsignedEnvelope,
    canonical: Vec<u8>,
    binding: SnapshotBinding,
    snapshot_id: Digest32,
    header: MarketHeader,
    intent: RequestDigest,
    effect: OperationEffect,
}

/// The review field whose approved value differs from the bytes about to be signed.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReviewField {
    Action,
    Chain,
    Program,
    Market,
    Actor,
    Epoch,
    Config,
    Roster,
    Policy,
    Snapshot,
    Amount,
    Asset,
    Payee,
    Worker,
    Capabilities,
    AccessDeclaration,
    ResponseCapacity,
    Resources,
    FeeLimit,
    Validity,
    Expiry,
    IdempotencyKey,
    Authority,
    Commitment,
}

/// Every term the owner approves, derived from the disclosure of the exact canonical bytes.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalTerms {
    pub action: u16,
    pub chain: ChainDomain,
    pub program: ProgramId,
    pub market: MarketId,
    pub actor: PrincipalId,
    pub epoch: u64,
    pub config: u64,
    pub roster: Presence<RosterDigest>,
    pub policy: PolicyDigest,
    pub snapshot: Digest32,
    pub effect: OperationEffect,
    pub capabilities: Vec<u8>,
    pub access_declaration: Vec<u8>,
    pub response_capacity: u32,
    pub resources: Resources,
    pub fee_limit: u128,
    pub not_before: u64,
    pub not_after: u64,
    pub expiry: u64,
    pub idempotency_key: [u8; 32],
    pub authority: Vec<u8>,
    pub commitment: [u8; 32],
}

impl PreparedOperation {
    #[must_use]
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical
    }

    #[must_use]
    pub const fn binding(&self) -> &SnapshotBinding {
        &self.binding
    }

    #[must_use]
    pub const fn snapshot_id(&self) -> Digest32 {
        self.snapshot_id
    }

    #[must_use]
    pub const fn intent(&self) -> RequestDigest {
        self.intent
    }

    #[must_use]
    pub const fn effect(&self) -> OperationEffect {
        self.effect
    }

    #[must_use]
    pub const fn payload_digest(&self) -> [u8; 32] {
        self.unsigned.payload_hash()
    }

    #[must_use]
    pub const fn expected_revision(&self) -> u64 {
        self.binding.revision
    }

    /// Discloses the exact canonical bytes through the existing owner-consent binder and
    /// derives every approval term from that disclosure, not from the planning inputs.
    ///
    /// # Errors
    /// Disclosure refusals, `NotNativeProgramCall` for any other operation, ABI or program,
    /// and `IntegrityFailure` when the calldata no longer matches the planned intent or effect.
    pub fn review(self, registry: &ModuleRegistry) -> Result<Review, AiClientError> {
        let disclosure = disclosure::bind(&self.canonical, registry)?;
        let Some(DisclosedNativeOperation::ProgramCall(call)) = &disclosure.native_operation else {
            return Err(AiClientError::NotNativeProgramCall);
        };
        if call.guest_abi != GUEST_ABI
            || call.entrypoint != ENTRYPOINT
            || call.program_id.bytes() != self.binding.program.bytes()
        {
            return Err(AiClientError::NotNativeProgramCall);
        }
        let validated = decode_envelope(&call.calldata)?;
        let envelope = validated.envelope;
        envelope.check_domain(
            self.binding.chain,
            self.binding.program,
            self.binding.market,
        )?;
        if validated.request_digest()? != self.intent
            || effect_of(&self.header, &envelope)? != self.effect
        {
            return Err(QueryError::IntegrityFailure.into());
        }
        let terms = ApprovalTerms {
            action: envelope.operation.selector(),
            chain: envelope.chain,
            program: envelope.program,
            market: envelope.market,
            actor: envelope.actor,
            epoch: envelope.epoch,
            config: envelope.config,
            roster: envelope.roster,
            policy: self.binding.policy,
            snapshot: self.snapshot_id,
            effect: self.effect,
            capabilities: call.capabilities.clone(),
            access_declaration: call.access_declaration.clone(),
            response_capacity: call.response_capacity,
            resources: call.resources,
            fee_limit: disclosure.fee_limit,
            not_before: disclosure.expiry.not_before,
            not_after: disclosure.expiry.not_after,
            expiry: envelope.expiry,
            idempotency_key: disclosure.idempotency_key,
            authority: disclosure.authority.clone(),
            commitment: disclosure.audit_digest()?,
        };
        Ok(Review {
            prepared: self,
            disclosure,
            terms,
        })
    }
}

/// The disclosure-derived terms presented to the owner for one prepared operation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Review {
    prepared: PreparedOperation,
    disclosure: Disclosure,
    terms: ApprovalTerms,
}

/// Owner approval of exactly the reviewed terms by the key the activity names.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Approval {
    review: Review,
    key: [u8; 32],
}

impl Review {
    #[must_use]
    pub const fn terms(&self) -> &ApprovalTerms {
        &self.terms
    }

    #[must_use]
    pub const fn prepared(&self) -> &PreparedOperation {
        &self.prepared
    }

    /// Accepts the owner's approved terms only when every term equals the disclosed bytes and
    /// the approving key is the activity's owner authority.
    ///
    /// # Errors
    /// `ReviewMismatch` naming the first differing term, then `UnauthorizedKey`.
    pub fn approve(
        self,
        approved: &ApprovalTerms,
        key: [u8; 32],
    ) -> Result<Approval, AiClientError> {
        if let Some(field) = first_difference(&self.terms, approved) {
            return Err(AiClientError::ReviewMismatch(field));
        }
        if self.disclosure.authority.as_slice() != key.as_slice() {
            return Err(AiClientError::UnauthorizedKey);
        }
        Ok(Approval { review: self, key })
    }
}

fn first_difference(actual: &ApprovalTerms, approved: &ApprovalTerms) -> Option<ReviewField> {
    let binding = [
        (actual.action == approved.action, ReviewField::Action),
        (actual.chain == approved.chain, ReviewField::Chain),
        (actual.program == approved.program, ReviewField::Program),
        (actual.market == approved.market, ReviewField::Market),
        (actual.actor == approved.actor, ReviewField::Actor),
        (actual.epoch == approved.epoch, ReviewField::Epoch),
        (actual.config == approved.config, ReviewField::Config),
        (actual.roster == approved.roster, ReviewField::Roster),
        (actual.policy == approved.policy, ReviewField::Policy),
        (actual.snapshot == approved.snapshot, ReviewField::Snapshot),
    ];
    let native = [
        (
            actual.capabilities == approved.capabilities,
            ReviewField::Capabilities,
        ),
        (
            actual.access_declaration == approved.access_declaration,
            ReviewField::AccessDeclaration,
        ),
        (
            actual.response_capacity == approved.response_capacity,
            ReviewField::ResponseCapacity,
        ),
        (
            actual.resources == approved.resources,
            ReviewField::Resources,
        ),
        (
            actual.fee_limit == approved.fee_limit,
            ReviewField::FeeLimit,
        ),
        (
            actual.not_before == approved.not_before && actual.not_after == approved.not_after,
            ReviewField::Validity,
        ),
        (actual.expiry == approved.expiry, ReviewField::Expiry),
        (
            actual.idempotency_key == approved.idempotency_key,
            ReviewField::IdempotencyKey,
        ),
        (
            actual.authority == approved.authority,
            ReviewField::Authority,
        ),
        (
            actual.commitment == approved.commitment,
            ReviewField::Commitment,
        ),
    ];
    let unequal = |checks: &[(bool, ReviewField)]| {
        checks
            .iter()
            .find(|(equal, _)| !equal)
            .map(|(_, field)| *field)
    };
    unequal(&binding)
        .or_else(|| effect_difference(&actual.effect, &approved.effect))
        .or_else(|| unequal(&native))
}

fn effect_difference(actual: &OperationEffect, approved: &OperationEffect) -> Option<ReviewField> {
    let checks: Vec<(bool, ReviewField)> = match (actual, approved) {
        (
            OperationEffect::Fund {
                amount,
                asset,
                account,
                refund_recipient,
            },
            OperationEffect::Fund {
                amount: approved_amount,
                asset: approved_asset,
                account: approved_account,
                refund_recipient: approved_refund,
            },
        ) => vec![
            (amount == approved_amount, ReviewField::Amount),
            (asset == approved_asset, ReviewField::Asset),
            (
                account == approved_account && refund_recipient == approved_refund,
                ReviewField::Payee,
            ),
        ],
        (
            OperationEffect::Claim {
                worker,
                recipient,
                amount,
                asset,
            },
            OperationEffect::Claim {
                worker: approved_worker,
                recipient: approved_recipient,
                amount: approved_amount,
                asset: approved_asset,
            },
        ) => vec![
            (amount == approved_amount, ReviewField::Amount),
            (asset == approved_asset, ReviewField::Asset),
            (recipient == approved_recipient, ReviewField::Payee),
            (worker == approved_worker, ReviewField::Worker),
        ],
        (
            OperationEffect::RefundFree {
                expected_refunded,
                amount,
                recipient,
                asset,
            },
            OperationEffect::RefundFree {
                expected_refunded: approved_refunded,
                amount: approved_amount,
                recipient: approved_recipient,
                asset: approved_asset,
            },
        ) => vec![
            (
                amount == approved_amount && expected_refunded == approved_refunded,
                ReviewField::Amount,
            ),
            (asset == approved_asset, ReviewField::Asset),
            (recipient == approved_recipient, ReviewField::Payee),
        ],
        (
            OperationEffect::Other { selector },
            OperationEffect::Other {
                selector: approved_selector,
            },
        ) => {
            vec![(selector == approved_selector, ReviewField::Action)]
        }
        _ => vec![(false, ReviewField::Action)],
    };
    checks
        .into_iter()
        .find(|(equal, _)| !equal)
        .map(|(_, field)| field)
}

impl Approval {
    #[must_use]
    pub const fn review(&self) -> &Review {
        &self.review
    }

    /// Signs exactly the approved canonical bytes through the existing disclosure-bound
    /// signer, verifies the signature, and returns the durable `Signed` record.
    ///
    /// # Errors
    /// `StaleAuthority`, `UnauthorizedKey` for a signer other than the approver, signer and
    /// signature refusals, and wire refusals of the signed activity.
    pub async fn sign(
        self,
        signer: &dyn Signer,
        registry: &ModuleRegistry,
        authority_height: u64,
    ) -> Result<OperationRecord, AiClientError> {
        let prepared = &self.review.prepared;
        let lag = authority_height.saturating_sub(prepared.binding.execution_height);
        if lag > AUTHORITY_FRESHNESS_HEIGHTS {
            return Err(AiClientError::StaleAuthority {
                lag,
                limit: AUTHORITY_FRESHNESS_HEIGHTS,
            });
        }
        if signer.public_key() != self.key {
            return Err(AiClientError::UnauthorizedKey);
        }
        let signature = sign_disclosed(
            signer,
            &prepared.canonical,
            &self.review.disclosure,
            registry,
        )
        .await?;
        let message = SignatureMessage::new(
            Domain::SignaturePreimage,
            prepared.unsigned.protocol_version(),
            prepared.unsigned.network_id(),
            &prepared.canonical,
        )?;
        ed25519::verify(&self.key, signature.as_bytes(), message)?;
        let authenticated = prepared
            .unsigned
            .clone()
            .attach_signature(Signature::new(signature.as_bytes())?);
        let signed_bytes = encode_signed_envelope(&authenticated)?;
        OperationRecord::signed(signed_bytes, self.key, prepared.intent.bytes(), registry)
    }
}

/// SDK lifecycle of one native operation; distinct from the AI domain job status.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum OperationState {
    Prepared = 1,
    Reviewed = 2,
    Signed = 3,
    Submitting = 4,
    Pending = 5,
    Executed = 6,
    Finalized = 7,
    Failed = 8,
    Unknown = 9,
}

impl OperationState {
    #[must_use]
    pub const fn code(self) -> u8 {
        self as u8
    }

    /// # Errors
    /// `CorruptRecord` for an undefined code.
    pub const fn from_code(code: u8) -> Result<Self, AiClientError> {
        Ok(match code {
            1 => Self::Prepared,
            2 => Self::Reviewed,
            3 => Self::Signed,
            4 => Self::Submitting,
            5 => Self::Pending,
            6 => Self::Executed,
            7 => Self::Finalized,
            8 => Self::Failed,
            9 => Self::Unknown,
            _ => return Err(AiClientError::CorruptRecord),
        })
    }
}

/// AI domain job status; a finalized native operation can still have a pending job.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum DomainStatus {
    Pending = 1,
    Completed = 2,
}

impl DomainStatus {
    #[must_use]
    pub const fn code(self) -> u8 {
        self as u8
    }

    /// # Errors
    /// `CorruptRecord` for an undefined code.
    pub const fn from_code(code: u8) -> Result<Self, AiClientError> {
        match code {
            1 => Ok(Self::Pending),
            2 => Ok(Self::Completed),
            _ => Err(AiClientError::CorruptRecord),
        }
    }
}

struct SignedIdentity {
    activity_id: [u8; 32],
    idempotency_key: [u8; 32],
    protocol_version: u16,
    network_id: u32,
    not_after: u64,
}

fn signed_identity(
    signed_bytes: &[u8],
    key: &[u8; 32],
    registry: &ModuleRegistry,
) -> Result<SignedIdentity, AiClientError> {
    let activity = decode_signed(signed_bytes, registry)?;
    if encode_signed(&activity)? != signed_bytes || activity.authority() != key.as_slice() {
        return Err(AiClientError::CorruptRecord);
    }
    let signature: &[u8; 64] = activity
        .signature()
        .ok_or(AiClientError::CorruptRecord)?
        .try_into()
        .map_err(|_| AiClientError::CorruptRecord)?;
    let unsigned = encode_unsigned(&activity)?;
    let message = SignatureMessage::new(
        Domain::SignaturePreimage,
        activity.protocol_version(),
        activity.network_id(),
        &unsigned,
    )?;
    ed25519::verify(key, signature, message)?;
    Ok(SignedIdentity {
        activity_id: activity_id(&activity)?,
        idempotency_key: activity.idempotency_key(),
        protocol_version: activity.protocol_version(),
        network_id: activity.network_id(),
        not_after: activity.timestamp_bound().not_after,
    })
}

/// Durable record of one signed operation; the signed bytes never change after signing.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OperationRecord {
    state: OperationState,
    domain: DomainStatus,
    protocol_version: u16,
    network_id: u32,
    activity_id: [u8; 32],
    idempotency_key: [u8; 32],
    intent: [u8; 32],
    signer_public_key: [u8; 32],
    not_after: u64,
    attempt: u32,
    result_code: Option<i32>,
    global_sequence: Option<u64>,
    checkpoint: Option<[u8; 32]>,
    signed_bytes: Vec<u8>,
}

impl OperationRecord {
    fn signed(
        signed_bytes: Vec<u8>,
        key: [u8; 32],
        intent: [u8; 32],
        registry: &ModuleRegistry,
    ) -> Result<Self, AiClientError> {
        let identity = signed_identity(&signed_bytes, &key, registry)?;
        Ok(Self {
            state: OperationState::Signed,
            domain: DomainStatus::Pending,
            protocol_version: identity.protocol_version,
            network_id: identity.network_id,
            activity_id: identity.activity_id,
            idempotency_key: identity.idempotency_key,
            intent,
            signer_public_key: key,
            not_after: identity.not_after,
            attempt: 0,
            result_code: None,
            global_sequence: None,
            checkpoint: None,
            signed_bytes,
        })
    }

    #[must_use]
    pub const fn state(&self) -> OperationState {
        self.state
    }

    #[must_use]
    pub const fn domain(&self) -> DomainStatus {
        self.domain
    }

    #[must_use]
    pub const fn protocol_version(&self) -> u16 {
        self.protocol_version
    }

    #[must_use]
    pub const fn network_id(&self) -> u32 {
        self.network_id
    }

    #[must_use]
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }

    #[must_use]
    pub const fn idempotency_key(&self) -> [u8; 32] {
        self.idempotency_key
    }

    #[must_use]
    pub const fn intent(&self) -> [u8; 32] {
        self.intent
    }

    #[must_use]
    pub const fn signer_public_key(&self) -> [u8; 32] {
        self.signer_public_key
    }

    #[must_use]
    pub const fn not_after(&self) -> u64 {
        self.not_after
    }

    #[must_use]
    pub const fn attempt(&self) -> u32 {
        self.attempt
    }

    #[must_use]
    pub const fn result_code(&self) -> Option<i32> {
        self.result_code
    }

    #[must_use]
    pub const fn global_sequence(&self) -> Option<u64> {
        self.global_sequence
    }

    #[must_use]
    pub const fn checkpoint(&self) -> Option<[u8; 32]> {
        self.checkpoint
    }

    #[must_use]
    pub fn signed_bytes(&self) -> &[u8] {
        &self.signed_bytes
    }

    /// Strict binary encoding of the durable record.
    ///
    /// # Errors
    /// `CorruptRecord` when the signed bytes exceed the length field.
    pub fn encode(&self) -> Result<Vec<u8>, AiClientError> {
        let length =
            u32::try_from(self.signed_bytes.len()).map_err(|_| AiClientError::CorruptRecord)?;
        let mut out = Vec::with_capacity(256 + self.signed_bytes.len());
        out.extend_from_slice(RECORD_MAGIC);
        out.push(self.state.code());
        out.push(self.domain.code());
        out.extend_from_slice(&self.protocol_version.to_be_bytes());
        out.extend_from_slice(&self.network_id.to_be_bytes());
        out.extend_from_slice(&self.activity_id);
        out.extend_from_slice(&self.idempotency_key);
        out.extend_from_slice(&self.intent);
        out.extend_from_slice(&self.signer_public_key);
        out.extend_from_slice(&self.not_after.to_be_bytes());
        out.extend_from_slice(&self.attempt.to_be_bytes());
        put_optional(&mut out, self.result_code.map(i32::to_be_bytes));
        put_optional(&mut out, self.global_sequence.map(u64::to_be_bytes));
        put_optional(&mut out, self.checkpoint);
        out.extend_from_slice(&length.to_be_bytes());
        out.extend_from_slice(&self.signed_bytes);
        Ok(out)
    }

    /// Decodes a durable record and re-derives its identity from the signed bytes.
    ///
    /// # Errors
    /// `CorruptRecord` for malformed, trailing, inconsistent or pre-signing records, and
    /// wire or signature refusals of the retained bytes.
    pub fn decode(bytes: &[u8], registry: &ModuleRegistry) -> Result<Self, AiClientError> {
        let mut input = Cursor(bytes);
        if &input.take::<8>()? != RECORD_MAGIC {
            return Err(AiClientError::CorruptRecord);
        }
        let state = OperationState::from_code(input.take::<1>()?[0])?;
        let domain = DomainStatus::from_code(input.take::<1>()?[0])?;
        let record = Self {
            state,
            domain,
            protocol_version: u16::from_be_bytes(input.take()?),
            network_id: u32::from_be_bytes(input.take()?),
            activity_id: input.take()?,
            idempotency_key: input.take()?,
            intent: input.take()?,
            signer_public_key: input.take()?,
            not_after: u64::from_be_bytes(input.take()?),
            attempt: u32::from_be_bytes(input.take()?),
            result_code: input.optional()?.map(i32::from_be_bytes),
            global_sequence: input.optional()?.map(u64::from_be_bytes),
            checkpoint: input.optional()?,
            signed_bytes: input.rest()?.to_vec(),
        };
        let identity = signed_identity(&record.signed_bytes, &record.signer_public_key, registry)?;
        if identity.activity_id != record.activity_id
            || identity.idempotency_key != record.idempotency_key
            || identity.protocol_version != record.protocol_version
            || identity.network_id != record.network_id
            || identity.not_after != record.not_after
            || !record.consistent()
        {
            return Err(AiClientError::CorruptRecord);
        }
        Ok(record)
    }

    const fn consistent(&self) -> bool {
        let pristine = self.result_code.is_none()
            && self.global_sequence.is_none()
            && self.checkpoint.is_none();
        let executed = matches!(self.result_code, Some(0)) && self.global_sequence.is_some();
        match self.state {
            OperationState::Prepared | OperationState::Reviewed => false,
            OperationState::Signed => pristine && self.attempt == 0,
            OperationState::Submitting | OperationState::Pending | OperationState::Unknown => {
                pristine && self.attempt > 0
            }
            OperationState::Executed => executed && self.checkpoint.is_none() && self.attempt > 0,
            OperationState::Finalized => executed && self.checkpoint.is_some() && self.attempt > 0,
            OperationState::Failed => {
                self.checkpoint.is_none()
                    && match self.result_code {
                        Some(code) => code != 0,
                        None => self.global_sequence.is_none(),
                    }
            }
        }
    }
}

fn put_optional<const N: usize>(out: &mut Vec<u8>, value: Option<[u8; N]>) {
    match value {
        Some(bytes) => {
            out.push(1);
            out.extend_from_slice(&bytes);
        }
        None => out.push(0),
    }
}

struct Cursor<'a>(&'a [u8]);

impl Cursor<'_> {
    fn take<const N: usize>(&mut self) -> Result<[u8; N], AiClientError> {
        let (head, tail) = self
            .0
            .split_first_chunk::<N>()
            .ok_or(AiClientError::CorruptRecord)?;
        self.0 = tail;
        Ok(*head)
    }

    fn optional<const N: usize>(&mut self) -> Result<Option<[u8; N]>, AiClientError> {
        match self.take::<1>()? {
            [0] => Ok(None),
            [1] => Ok(Some(self.take()?)),
            _ => Err(AiClientError::CorruptRecord),
        }
    }

    fn rest(&mut self) -> Result<&[u8], AiClientError> {
        let length = usize::try_from(u32::from_be_bytes(self.take()?))
            .map_err(|_| AiClientError::CorruptRecord)?;
        if self.0.len() != length {
            return Err(AiClientError::CorruptRecord);
        }
        Ok(self.0)
    }
}

fn hex(bytes: &[u8; 32]) -> String {
    bytes
        .iter()
        .flat_map(|byte| [HEX[usize::from(byte >> 4)], HEX[usize::from(byte & 0x0f)]])
        .map(char::from)
        .collect()
}

/// Durable journal: every lifecycle change is synced before the next side effect.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OperationJournal {
    directory: PathBuf,
}

impl OperationJournal {
    /// # Errors
    /// `Journal` when the directory cannot be created.
    pub fn open(directory: &Path) -> Result<Self, AiClientError> {
        fs::create_dir_all(directory)?;
        Ok(Self {
            directory: directory.to_path_buf(),
        })
    }

    #[must_use]
    pub fn record_path(&self, activity_id: &[u8; 32]) -> PathBuf {
        self.directory
            .join(format!("{}.{RECORD_EXTENSION}", hex(activity_id)))
    }

    /// Atomically replaces the record file and syncs file and directory.
    ///
    /// # Errors
    /// `Journal` for any filesystem failure, `CorruptRecord` for an unencodable record.
    pub fn persist(&self, record: &OperationRecord) -> Result<(), AiClientError> {
        let bytes = record.encode()?;
        let path = self.record_path(&record.activity_id);
        let partial = path.with_extension("partial");
        let mut file = File::create(&partial)?;
        file.write_all(&bytes)?;
        file.sync_all()?;
        drop(file);
        fs::rename(&partial, &path)?;
        File::open(&self.directory)?.sync_all()?;
        Ok(())
    }

    /// Loads one record; an interrupted `Submitting` record becomes `Unknown` durably.
    ///
    /// # Errors
    /// `Journal`, `CorruptRecord` and the refusals of [`OperationRecord::decode`].
    pub fn load(
        &self,
        activity_id: &[u8; 32],
        registry: &ModuleRegistry,
    ) -> Result<OperationRecord, AiClientError> {
        let mut record =
            OperationRecord::decode(&fs::read(self.record_path(activity_id))?, registry)?;
        if record.activity_id != *activity_id {
            return Err(AiClientError::CorruptRecord);
        }
        if record.state == OperationState::Submitting {
            record.state = OperationState::Unknown;
            self.persist(&record)?;
        }
        Ok(record)
    }

    /// Persists a freshly signed record before any transmission.
    ///
    /// # Errors
    /// `InvalidTransition` unless the record is `Signed`; journal failures.
    pub fn record_signed(&self, record: &OperationRecord) -> Result<(), AiClientError> {
        require(record, &[OperationState::Signed])?;
        self.persist(record)
    }

    /// First transmission of the exact signed bytes. `Submitting` is durable before sending;
    /// acknowledgment means pending, and lost or indeterminate delivery means unknown.
    ///
    /// # Errors
    /// `InvalidTransition` unless `Signed`; pre-send verification refusals restore `Signed`.
    pub fn submit(
        &self,
        record: &mut OperationRecord,
        transport: &mut dyn FrameTransport,
        registry: &ModuleRegistry,
        interface_version: InterfaceVersion,
        correlation_id: u64,
    ) -> Result<OperationState, AiClientError> {
        require(record, &[OperationState::Signed])?;
        self.deliver(
            record,
            transport,
            registry,
            interface_version,
            correlation_id,
        )
    }

    /// Separately authorised resend of the same signed bytes from `Unknown`; never re-signs.
    ///
    /// # Errors
    /// `InvalidTransition` unless `Unknown`; pre-send verification refusals restore `Unknown`.
    pub fn resend_exact(
        &self,
        record: &mut OperationRecord,
        transport: &mut dyn FrameTransport,
        registry: &ModuleRegistry,
        interface_version: InterfaceVersion,
        correlation_id: u64,
    ) -> Result<OperationState, AiClientError> {
        require(record, &[OperationState::Unknown])?;
        self.deliver(
            record,
            transport,
            registry,
            interface_version,
            correlation_id,
        )
    }

    fn deliver(
        &self,
        record: &mut OperationRecord,
        transport: &mut dyn FrameTransport,
        registry: &ModuleRegistry,
        interface_version: InterfaceVersion,
        correlation_id: u64,
    ) -> Result<OperationState, AiClientError> {
        let (prior_state, prior_attempt) = (record.state, record.attempt);
        record.attempt = record
            .attempt
            .checked_add(1)
            .ok_or(AiClientError::CorruptRecord)?;
        record.state = OperationState::Submitting;
        self.persist(record)?;
        let context = SubmissionContext {
            interface_version,
            protocol_version: record.protocol_version,
            network_id: record.network_id,
            correlation_id,
            signer_public_key: record.signer_public_key,
            attempt: record.attempt,
        };
        record.state = match submit_signed(transport, registry, context, &record.signed_bytes) {
            Ok(Submission::Acknowledged(_)) => OperationState::Pending,
            Err(SubmitError::CoreRefusal { result, .. })
                if record.attempt == 1
                    && result.retriability() == Retriability::Terminal
                    && result.known() != Some(KnownResult::IdempotentReplay) =>
            {
                record.result_code = Some(result.raw());
                OperationState::Failed
            }
            Ok(Submission::Unknown(_))
            | Err(
                SubmitError::CoreRefusal { .. }
                | SubmitError::Envelope(_)
                | SubmitError::UnavailableCapability
                | SubmitError::Disconnected,
            ) => OperationState::Unknown,
            Err(
                error @ (SubmitError::Wire(_)
                | SubmitError::SignatureLength(_)
                | SubmitError::Signature(_)
                | SubmitError::ProtocolVersion { .. }
                | SubmitError::Network { .. }),
            ) => {
                record.state = prior_state;
                record.attempt = prior_attempt;
                self.persist(record)?;
                return Err(error.into());
            }
        };
        self.persist(record)?;
        Ok(record.state)
    }

    /// Resolves `Pending` or `Unknown` only from an authenticated receipt for this activity;
    /// absence or a wait timeout leaves the state unchanged.
    ///
    /// # Errors
    /// `InvalidTransition`, receipt lookup refusals (state unchanged), and `ReceiptMismatch`
    /// for a receipt of another activity or module.
    pub fn resolve(
        &self,
        record: &mut OperationRecord,
        transport: &mut dyn FrameTransport,
        context: AuthenticatedLookupContext,
    ) -> Result<OperationState, AiClientError> {
        require(record, &[OperationState::Pending, OperationState::Unknown])?;
        let receipt = match lookup_authenticated(transport, record.activity_id, context)? {
            AuthenticatedLookup::Absent | AuthenticatedLookup::TimedOut => return Ok(record.state),
            AuthenticatedLookup::Verified(receipt) => receipt,
        };
        if receipt.activity_id() != record.activity_id
            || receipt.module_id() != ModuleId::Programs as u16
        {
            return Err(AiClientError::ReceiptMismatch);
        }
        let code = receipt.result_code().raw();
        record.result_code = Some(code);
        record.global_sequence = Some(receipt.global_sequence());
        record.state = if code == 0 {
            OperationState::Executed
        } else {
            OperationState::Failed
        };
        self.persist(record)?;
        Ok(record.state)
    }

    /// Promotes a successful execution with checkpoint-finalised evidence covering it.
    ///
    /// # Errors
    /// `InvalidTransition` unless `Executed`; `FinalityMismatch` for another network or a
    /// checkpoint range that excludes the receipt sequence.
    pub fn finalize(
        &self,
        record: &mut OperationRecord,
        finality: &CheckpointFinality,
    ) -> Result<OperationState, AiClientError> {
        require(record, &[OperationState::Executed])?;
        let covered = record
            .global_sequence
            .is_some_and(|sequence| finality.covers(sequence));
        if finality.network_id != record.network_id || !covered {
            return Err(AiClientError::FinalityMismatch);
        }
        record.checkpoint = Some(finality.checkpoint.bytes());
        record.state = OperationState::Finalized;
        self.persist(record)?;
        Ok(record.state)
    }

    /// A signed but never-sent operation fails once its validity has passed; submitted
    /// operations never fail through elapsed time.
    ///
    /// # Errors
    /// `InvalidTransition` unless `Signed`; journal failures.
    pub fn expire(
        &self,
        record: &mut OperationRecord,
        now: u64,
    ) -> Result<OperationState, AiClientError> {
        require(record, &[OperationState::Signed])?;
        if now > record.not_after {
            record.state = OperationState::Failed;
            self.persist(record)?;
        }
        Ok(record.state)
    }
}

fn require(record: &OperationRecord, allowed: &[OperationState]) -> Result<(), AiClientError> {
    if allowed.contains(&record.state) {
        Ok(())
    } else {
        Err(AiClientError::InvalidTransition { from: record.state })
    }
}

/// Retention status of one requested epoch.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum EpochStatus {
    Retained = 1,
    NeverOpened = 2,
    RetainedTerminal = 3,
    ArchiveRequired = 4,
    ArchiveUnavailable = 5,
    UnsupportedVersion = 6,
}

impl EpochStatus {
    #[must_use]
    pub const fn code(self) -> u8 {
        self as u8
    }
}

/// The immutable earlier binding a historical epoch refers to explicitly.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HistoricalSource {
    pub snapshot: Digest32,
    pub config: Version,
    pub policy: PolicyDigest,
    pub roster: Presence<RosterDigest>,
}

/// One epoch header: a status plus its source, absent exactly when nothing was retained.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EpochHistoryEntry {
    epoch: u64,
    status: EpochStatus,
    source: Presence<HistoricalSource>,
}

impl EpochHistoryEntry {
    /// # Errors
    /// `NON_CANONICAL` when presence of the source contradicts the status.
    pub fn new(
        epoch: u64,
        status: EpochStatus,
        source: Presence<HistoricalSource>,
    ) -> Result<Self, AiClientError> {
        let sourceless = matches!(
            status,
            EpochStatus::NeverOpened | EpochStatus::UnsupportedVersion
        );
        if sourceless != matches!(source, Presence::Absent) {
            return Err(NON_CANONICAL.into());
        }
        Ok(Self {
            epoch,
            status,
            source,
        })
    }

    #[must_use]
    pub const fn epoch(&self) -> u64 {
        self.epoch
    }

    #[must_use]
    pub const fn status(&self) -> EpochStatus {
        self.status
    }

    #[must_use]
    pub const fn source(&self) -> Presence<HistoricalSource> {
        self.source
    }

    /// The epoch's own policy from retained or archived content, verified against the
    /// historical digest; the current policy is never substituted.
    ///
    /// # Errors
    /// `HistoryUnavailable` for a status without retrievable content and `IntegrityFailure`
    /// for content that is not exactly the recorded policy.
    pub fn policy(&self, content: &[u8]) -> Result<TaskPolicyV1, AiClientError> {
        let Presence::Present(source) = self.source else {
            return Err(AiClientError::HistoryUnavailable(self.status));
        };
        if self.status == EpochStatus::ArchiveUnavailable {
            return Err(AiClientError::HistoryUnavailable(self.status));
        }
        if content.len() != TASK_POLICY_BYTES {
            return Err(QueryError::IntegrityFailure.into());
        }
        let policy = TaskPolicyV1::decode(content).map_err(|_| QueryError::IntegrityFailure)?;
        let mut encoded = vec![0; TASK_POLICY_BYTES];
        let length = policy
            .encode(&mut encoded)
            .map_err(|_| QueryError::IntegrityFailure)?;
        if encoded.get(..length) != Some(content)
            || policy.digest().map_err(|_| QueryError::IntegrityFailure)? != source.policy
        {
            return Err(QueryError::IntegrityFailure.into());
        }
        Ok(policy)
    }
}
