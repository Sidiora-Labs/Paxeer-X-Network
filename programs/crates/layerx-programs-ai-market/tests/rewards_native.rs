//! AI.F06-T03 real account funding and reward claims. Fund, Claim, `ExpireEpochClaims`,
//! `RefundFree` and `PruneEpoch` go through `rewards::apply` with real envelopes over the
//! complete shared state value of a market produced by the real F01 CREATE, F02/F03/F08
//! enrollments, `ADVANCE_ACTIVATION` and `OPEN_EPOCH`. Every applied transition is planned by
//! the value adapter against the registered rewards account and immutable asset, and its
//! cover is checked against the tracked physical balance of that account, so conservation
//! holds across the ledger and the account. The native part runs Fund through the registered
//! Program activity, where the transfer, the next state and the replay record commit or
//! refuse together.
use ed25519_dalek::{Signer, SigningKey};
use layerx_program_sdk::{Field, HostRefusal, ProgramError, Reason, ValueError};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        ApplicationResult, ChunkRequest, Envelope, ResultStatus,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F01_LIFECYCLE_CLOSED,
        F06_CLAIM_EXPIRED, F06_CONTRIBUTION_CONSENT_REQUIRED, F06_EPOCH_TERMINAL,
        F06_FUNDING_POLICY_MISMATCH, F06_INVALID_AMOUNT, F06_LEDGER_INVARIANT_VIOLATION,
        F06_REFUND_RECIPIENT_MISMATCH, F06_UNKNOWN_WORKER_ENTITLEMENT, F06_WRONG_CLAIM_AMOUNT,
        F06_WRONG_CLAIM_RECIPIENT, HOST_CAPABILITY, HOST_TRANSFER, INSUFFICIENT_FREE,
        NON_CANONICAL, NOT_FOUND, READINESS_BLOCKED, REPLAY_CONFLICT, RETENTION_FULL, SEQUENCE_GAP,
        STALE_CURSOR, UNAUTHORIZED, WRONG_CONFIG, WRONG_EPOCH, WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        bind_snapshot, read_state_chunk, FinalityEvidence, QueryError, QueryResult, ReadProof,
        SnapshotBinding, StateCapture, FINALIZED_RANK,
    },
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, PolicySection, ACTIVE, WINDING_DOWN},
    reward_math::allocate,
    rewards::{
        self, decode_reward_state, entitlement_id, ClaimRequest, Disposition, EpochStatus, Outcome,
        RecipientSlot, RefundRequest, RewardEffect, RewardLedger, RewardState,
        CLAIMS_EXPIRED_BYTES, CLAIM_PAID_BYTES, EPOCH_SPAN_HEIGHTS, FUNDED_BYTES,
        FUNDING_POLICY_VERSION, PRUNED_BYTES, REFUNDED_BYTES, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayTable, Section,
        SharedState,
    },
    types::{
        AccountId, Amount, AssetId, Authentication, ChainDomain, Digest32, FrozenBinding,
        MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId,
        ResultDigest, RosterDigest, RubricDigest, StateDigest, Version, WorkerId,
        WorkerRosterEntry,
    },
    value_adapter::{self, RewardsAccount, ValueAction},
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_CHUNK_BYTES, MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES, MAX_WORKERS,
};
use layerx_programs_runtime::terminal::{
    decode_terminal_payload, CandidateTerminalOutcome, ExecutionTerminal, TerminalDetail,
};
use layerx_programs_runtime::RefusalClass;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const TREASURY: u8 = 0x71;
const OUTSIDER: u8 = 0x72;
const KEEPER: u8 = 0x7f;
const LOW: u8 = 0x20;
const ORIGIN: u64 = 128;
const METADATA: [u8; 32] = [0x44; 32];
const BUDGET_CAP: Amount = 101;
const TERMINAL_AT: u64 = 1100;
const EXPIRY_AT: u64 = TERMINAL_AT + 4096;
/// Low bytes of the big-endian deposits and free counters in the ledger at the head of the
/// reward state.
const DEPOSITS_LOW: usize = 32 * 3 + 8 + 16 - 1;
const FREE_LOW: usize = 32 * 3 + 8 + 16 * 4 - 1;

const NATIVE_OWNER: usize = 0;
const NATIVE_OUTSIDER: usize = 2;
const NATIVE_ASSET: [u8; 32] = {
    let mut asset = [0; 32];
    asset[0] = 9;
    asset
};
const NATIVE_EXPIRY: u64 = 1_000_000;
const EVENTS_DOMAIN: &[u8] = b"LayerX/programs/events/v1\0";
const PROGRAM_REFUSED: i64 = -736;
/// An unpinned chunk read: revision 0 with no digest.
const UNPINNED: (u64, Presence<StateDigest>) = (0, Presence::Absent);
const PUBLISHED_AT_MS: u64 = 1_000;

enum Failure {
    Application(ApplicationError),
    Conversion(core::num::TryFromIntError),
    Io(std::io::Error),
    Harness(String),
    Query(QueryError),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Conversion(error) => write!(f, "integer conversion {error}"),
            Self::Io(error) => write!(f, "native harness io {error}"),
            Self::Harness(what) => write!(f, "native harness {what}"),
            Self::Query(error) => write!(f, "snapshot query {error:?}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<core::num::TryFromIntError> for Failure {
    fn from(error: core::num::TryFromIntError) -> Self {
        Self::Conversion(error)
    }
}
impl From<std::io::Error> for Failure {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}
impl From<QueryError> for Failure {
    fn from(error: QueryError) -> Self {
        Self::Query(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;
type TestResult = CodecResult<()>;

fn harness(what: impl Into<String>) -> Failure {
    Failure::Harness(what.into())
}

fn principal(n: u8) -> CodecResult<PrincipalId> {
    let mut bytes = [0x50; 32];
    bytes[0] = n;
    PrincipalId::new(bytes)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn rubric() -> CodecResult<RubricDigest> {
    RubricDigest::new([4; 32])
}
fn policy() -> CodecResult<TaskPolicyV1> {
    TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: Digest32::new([1; 32])?,
            dataset_artifact: [2; 32],
            benchmark_suite: Digest32::new([3; 32])?,
            rubric: rubric()?,
            task_schema: Digest32::new([5; 32])?,
            result_schema: Digest32::new([6; 32])?,
            service_terms: Digest32::new([7; 32])?,
        },
        BUDGET_CAP,
        1,
    )
}
fn policy_bytes() -> CodecResult<Vec<u8>> {
    let mut out = vec![0; TASK_POLICY_BYTES];
    policy()?.encode(&mut out)?;
    Ok(out)
}
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// A request id unique per kind, actor and sequence.
fn tag(kind: u8, n: u8, sequence: u64) -> [u8; 32] {
    let mut out = [kind; 32];
    out[0] = n;
    out[1..9].copy_from_slice(&sequence.to_be_bytes());
    out
}
fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}
fn fund_payload(amount: Amount, recipient: [u8; 32], policy: u64, consent: bool) -> Vec<u8> {
    let mut out = vec![0; 57];
    out[..16].copy_from_slice(&amount.to_be_bytes());
    out[16..48].copy_from_slice(&recipient);
    out[48..56].copy_from_slice(&policy.to_be_bytes());
    out[56] = u8::from(consent);
    out
}
fn claim_payload(worker: WorkerId, recipient: AccountId, amount: Amount) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; 80];
    let n = ClaimRequest {
        worker,
        recipient,
        amount,
    }
    .encode(&mut out)?;
    out.truncate(n);
    Ok(out)
}
fn refund_payload(expected_refunded: Amount, amount: Amount) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; 64];
    let n = RefundRequest {
        expected_refunded,
        amount,
        recipient: AccountId::new(REFUND)?,
    }
    .encode(&mut out)?;
    out.truncate(n);
    Ok(out)
}

/// One request envelope of the in-process market.
#[derive(Clone)]
struct Req {
    operation: Operation,
    actor: PrincipalId,
    submitter: Option<PrincipalId>,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    request: [u8; 32],
    payload: Vec<u8>,
}
fn req(operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
    Req {
        operation,
        actor,
        submitter: None,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        request: [1; 32],
        payload,
    }
}
impl Req {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = ChainDomain::new(CHAIN)?;
        let program = ProgramId::new(PROGRAM)?;
        let mut out = vec![0; 32_768];
        let n = encode_envelope(
            &Envelope {
                operation: self.operation,
                chain,
                program,
                market: derive_market(chain, program)?,
                actor: self.actor,
                epoch: self.epoch,
                config: self.config,
                roster: self.roster,
                sequence: self.sequence,
                expiry: u64::MAX,
                request: RequestId::new(self.request)?,
                payload: &self.payload,
                authentication: Authentication::Native,
            },
            &mut out,
        )?;
        out.truncate(n);
        Ok(out)
    }
    fn context(&self, at: u64) -> CodecResult<CallContext> {
        Ok(CallContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            principal: self.submitter.unwrap_or(self.actor),
            height: at,
        })
    }
    fn request_digest(&self) -> CodecResult<RequestDigest> {
        decode_envelope(&self.encode()?)?.request_digest()
    }
}

/// Committed state after the real F01 CREATE at `origin` with a present treasury.
fn create(origin: u64) -> CodecResult<Vec<u8>> {
    let program = ProgramId::new(PROGRAM)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(1);
    payload.extend_from_slice(principal(TREASURY)?.as_bytes());
    payload.extend_from_slice(&policy_bytes()?);
    payload.extend_from_slice(&[16; 32]);
    let call = Req {
        sequence: 1,
        ..req(dispatch::CREATE, PrincipalId::new(OWNER)?, payload)
    };
    let encoded = call.encode()?;
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let registry_ops::Outcome::Applied { state, .. } = registry_ops::apply(
        &call.context(origin)?,
        None,
        &decode_envelope(&encoded)?,
        &mut section,
        &mut event,
    )?
    else {
        return Err(NON_CANONICAL);
    };
    encode(&state)
}

/// Owned, decoded sections of one committed shared state value.
struct Parts {
    revision: u64,
    policy: Vec<u8>,
    workers: WorkerTable,
    region: Vec<u8>,
    reports: Vec<u8>,
    rewards: Vec<u8>,
    admission: AdmissionTable,
    replay: ReplayTable,
    features: Vec<u8>,
}
impl Parts {
    fn load(bytes: &[u8]) -> CodecResult<Self> {
        let state = decode_shared_state(bytes)?;
        let [policy, identity, reports, rewards, admission] = state.feature_sections;
        let (workers, region) = split_identity_section(identity)?;
        Ok(Self {
            revision: state.revision,
            policy: policy.to_vec(),
            workers: WorkerTable::decode(workers)?,
            region: region.to_vec(),
            reports: reports.to_vec(),
            rewards: rewards.to_vec(),
            admission: AdmissionTable::decode(admission)?,
            replay: state.control.replay,
            features: state.control.feature_bytes.to_vec(),
        })
    }
    fn market(&self) -> CodecResult<MarketHeader> {
        Ok(PolicySection::decode(&self.policy)?.header)
    }
    /// Commits at `self.revision`, keeping the F01 header revision equal to it.
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let mut section = PolicySection::decode(&self.policy)?;
        section.header.state_revision = self.revision;
        let mut policy = vec![0; section.encoded_len()?];
        section.encode(&mut policy)?;
        let mut identity = vec![0; WORKER_TABLE_MAX_BYTES];
        let workers_len = self.workers.encode(&mut identity)?;
        identity.truncate(workers_len);
        identity.extend_from_slice(&self.region);
        let mut admission = vec![0; TABLE_MAX_BYTES];
        let admission_len = self.admission.encode(&mut admission)?;
        encode(&SharedState {
            revision: self.revision,
            feature_sections: [
                &policy,
                &identity,
                &self.reports,
                &self.rewards,
                &admission[..admission_len],
            ],
            control: Control {
                replay: self.replay.clone(),
                feature_bytes: &self.features,
            },
        })
    }
    fn insert_grant(&mut self, grant: EvaluatorGrant, n: u8) -> TestResult {
        let mut region = EvaluatorRegion::decode(&self.region)?;
        region.insert(&EvaluatorRecord {
            grant,
            last: LastRequest {
                sequence: 1,
                request: RequestId::new([n; 32])?,
                digest: RequestDigest::new([0x33; 32])?,
                result: ResultDigest::new([0x34; 32])?,
            },
            rekey: None,
            revocation: None,
        })?;
        let mut out = vec![0; region.encoded_len()];
        let len = region.encode(&mut out)?;
        out.truncate(len);
        self.region = out;
        Ok(())
    }
}

fn ctx(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height: at,
    }
}
/// Market-owner approval bound to the required effective epoch.
fn approve(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    owner: PrincipalId,
    delegate: PublicKey32,
    at: u64,
) -> CodecResult<(u64, Digest32)> {
    let effective = table.required_effective_epoch(&ctx(market, owner, at))?;
    let terms = ApprovalTerms {
        participant,
        owner,
        enrollment_nonce_commitment: Digest32::new([7; 32])?,
        delegate,
        delegate_generation: 1,
        identity_commitment: Digest32::new([9; 32])?,
        effective_epoch: effective,
        config_version: 1,
        request: RequestId::new(participant.bytes())?,
        expiry_height: market.origin_height + effective * 128 + 64,
    };
    let digest = table.approve(&ctx(market, market.owner_principal, at), &terms)?;
    Ok((effective, digest))
}
fn worker_record(
    worker: WorkerId,
    owner: PrincipalId,
    delegate: PublicKey32,
    slot: u8,
    at: u64,
) -> CodecResult<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker,
        owner,
        delegate,
        metadata: MetadataDigest::new(METADATA)?,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        valid_from: at,
        expiry: at + 4096,
        revocation_sequence: 0,
        effective_epoch: 0,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Enrolled,
        slot,
        last_metadata_height: at,
    })
}
fn roster_entry(record: &WorkerCurrent) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: record.worker,
        owner: record.owner,
        recipient: AccountId::new(record.owner.bytes())?,
        generation: version()?,
        key_version: version()?,
        public_key: record.delegate,
        metadata: record.metadata,
    })
}

/// One reward transition committed through `rewards::apply`.
struct Applied {
    effect: RewardEffect,
    before: RewardLedger,
    after: RewardLedger,
    response: Vec<u8>,
    revision: u64,
    epoch: u64,
}

/// One market's committed shared state bytes, its next owner and treasury sequences and the
/// physical balance of its rewards account.
#[derive(Clone)]
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
    treasury_sequence: u64,
    held: Amount,
}
impl World {
    fn create(origin: u64) -> CodecResult<Self> {
        Ok(Self {
            bytes: create(origin)?,
            owner_sequence: 2,
            treasury_sequence: 1,
            held: 0,
        })
    }
    fn parts(&self) -> CodecResult<Parts> {
        Parts::load(&self.bytes)
    }
    fn revision(&self) -> CodecResult<u64> {
        Ok(decode_shared_state(&self.bytes)?.revision)
    }
    fn section(&self) -> CodecResult<PolicySection<'_>> {
        PolicySection::decode(
            decode_shared_state(&self.bytes)?.feature_sections[Section::PolicyLifecycle.index()],
        )
    }
    fn settlement(&self) -> CodecResult<&[u8]> {
        Ok(decode_shared_state(&self.bytes)?.feature_sections[Section::SettlementClaims.index()])
    }
    /// The committed ledger; an empty section is the initial ledger of the header's asset,
    /// rewards account and refund recipient.
    fn ledger(&self) -> CodecResult<RewardLedger> {
        let settlement = self.settlement()?;
        if settlement.is_empty() {
            let asset = AssetId::new(ASSET)?;
            return RewardLedger::new(
                asset,
                derive_rewards_account(ProgramId::new(PROGRAM)?, asset)?,
                AccountId::new(REFUND)?,
            );
        }
        decode_reward_state(settlement.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?.ledger()
    }
    /// `[D, P, X, F, R, C]`.
    fn counters(&self) -> CodecResult<[Amount; 6]> {
        let l = self.ledger()?;
        Ok([
            l.tracked_deposits,
            l.total_claimed,
            l.tracked_refunds,
            l.free,
            l.reserved,
            l.liability,
        ])
    }
    fn roster(&self, epoch: u64) -> CodecResult<RosterDigest> {
        let settlement = self.settlement()?;
        Ok(
            decode_reward_state(settlement.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?
                .row(epoch)?
                .roster,
        )
    }
    /// Applies `change` to the decoded sections and commits it at the next revision.
    fn edit<T>(
        &mut self,
        change: impl FnOnce(&mut Parts, &MarketHeader) -> CodecResult<T>,
    ) -> CodecResult<T> {
        let mut parts = self.parts()?;
        let market = parts.market()?;
        let out = change(&mut parts, &market)?;
        parts.revision += 1;
        self.bytes = parts.encode()?;
        Ok(out)
    }
    /// Real owner F01 `SCHEDULE_ACTIVATION` of `epoch`.
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        let call = Req {
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(
                dispatch::SCHEDULE_ACTIVATION,
                PrincipalId::new(OWNER)?,
                payload,
            )
        };
        let encoded = call.encode()?;
        let current = decode_shared_state(&self.bytes)?;
        let mut section = vec![0; F01_SECTION_CAP];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let registry_ops::Outcome::Applied { state, .. } = registry_ops::apply(
            &call.context(at)?,
            Some(&current),
            &decode_envelope(&encoded)?,
            &mut section,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        self.bytes = encode(&state)?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// F02 ENROLLED record with an ed25519 delegate, worker replay slot and F08 approval plus
    /// owner acceptance.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<WorkerRosterEntry> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let worker = derive_worker(market.market_id, owner, [n; 32])?;
            let slot = parts.workers.free_slot()?;
            let record = worker_record(worker, owner, public(&delegate_key(n)), slot, at)?;
            parts.workers.insert(&record)?;
            parts
                .replay
                .bind(ActorSlot::worker(usize::from(slot))?, owner, version()?)?;
            let participant = Participant::Worker(worker);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                owner,
                record.delegate,
                at,
            )?;
            parts.admission.admit(
                &ctx(market, owner, at),
                &Admission {
                    participant,
                    delegate_generation: 1,
                    effective_epoch: effective,
                    config_version: 1,
                    approval_digest: digest,
                },
            )?;
            roster_entry(&record)
        })
    }
    /// F03 nomination accepted through the signed F08 evaluator consent of its key.
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let key = evaluator_key(n);
            let signing_key = public(&key);
            let participant =
                Participant::Evaluator(derive_evaluator(market.market_id, owner, [n; 32])?);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                owner,
                signing_key,
                at,
            )?;
            let grant = EvaluatorGrant::nominate(
                market.market_id,
                owner,
                [n; 32],
                GrantTerms {
                    rubric: rubric()?,
                    grant_version: version()?,
                    key_version: version()?,
                    signing_key,
                    effective_epoch: effective,
                    expiry_epoch_exclusive: effective + 32,
                },
            )?;
            let consent = EvaluatorConsent {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                evaluator: grant.evaluator,
                owner,
                signing_key,
                enrollment_nonce: [n; 32],
                rubric: grant.rubric,
                approval_digest: digest,
                request: RequestId::new([n; 32])?,
                grant_version: 1,
                key_version: 1,
                effective_epoch: effective,
                config_version: 1,
                expiry_height: market.origin_height + effective * 128 + 64,
            };
            let mut signed = [0u8; 362];
            consent.encode(&mut signed)?;
            let mut payload = signed.to_vec();
            payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
            admit_evaluator(
                &mut parts.admission,
                &ctx(market, owner, at),
                &grant,
                consent.request,
                &payload,
            )?;
            parts
                .replay
                .bind(ActorSlot::evaluator(usize::from(n - 2))?, owner, version()?)?;
            parts.insert_grant(grant, n)
        })
    }
    /// Permissionless object-local `OPEN_EPOCH` of the clock epoch of `at`.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let preview = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Req {
            epoch: preview.epoch,
            config: preview.config.get(),
            roster: Presence::Present(preview.roster),
            request: tag(0x60, 0, preview.epoch),
            ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
        };
        let encoded = call.encode()?;
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Opening::Opened {
            frozen, state_len, ..
        } = epoch::open_epoch(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        next.truncate(state_len);
        self.bytes = next;
        Ok(frozen)
    }
    /// Permissionless object-local `ADVANCE_ACTIVATION`; the lifecycle becomes ACTIVE.
    fn advance(&mut self, at: u64) -> TestResult {
        let section = self.section()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&section.header.activation_epoch.to_be_bytes());
        let call = Req {
            config: section.header.active_config_version,
            request: [0x5f; 32],
            ..req(dispatch::ADVANCE_ACTIVATION, principal(KEEPER)?, payload)
        };
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; ADVANCE_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let activated = epoch::advance_activation(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        next.truncate(activated.state_len);
        self.bytes = next;
        Ok(())
    }
    /// Real F06 `TerminalizeRewards` of `frozen` with one weight per frozen worker (zero
    /// weights are a scored-zero vote); F05 bytes after the reward state are carried.
    fn terminalize(
        &mut self,
        frozen: &Frozen,
        weights: &[(WorkerRosterEntry, u32)],
        at: u64,
    ) -> TestResult {
        self.edit(|parts, market| {
            let binding = FrozenBinding {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                epoch: frozen.epoch,
                config: frozen.config,
                roster: frozen.roster,
            };
            let (head, tail) = parts
                .rewards
                .split_at_checked(REWARD_STATE_BYTES)
                .ok_or(NOT_FOUND)?;
            let mut terminal = terminalized(head, binding, frozen.budget, weights, at)?;
            terminal.extend_from_slice(tail);
            parts.rewards = terminal;
            Ok(())
        })
    }
    /// The market lifecycle set directly in the F01 header; `REQUEST_CLOSE` has no routed
    /// producer yet.
    fn lifecycle(&mut self, lifecycle: u8) -> TestResult {
        self.edit(|parts, _| {
            let mut section = PolicySection::decode(&parts.policy)?;
            section.header.lifecycle = lifecycle;
            let mut policy = vec![0; section.encoded_len()?];
            section.encode(&mut policy)?;
            parts.policy = policy;
            Ok(())
        })
    }
}

/// The F06 calls through `rewards::apply`.
impl World {
    /// An owner or treasury Fund envelope bound to the ledger context.
    fn fund_call(&mut self, actor: u8, payload: Vec<u8>) -> CodecResult<Req> {
        let (who, sequence) = if actor == 0 {
            let sequence = self.owner_sequence;
            self.owner_sequence += 1;
            (PrincipalId::new(OWNER)?, sequence)
        } else {
            let sequence = self.treasury_sequence;
            self.treasury_sequence += 1;
            (principal(actor)?, sequence)
        };
        Ok(Req {
            sequence,
            request: tag(0x40, actor, sequence),
            ..self.context_call(dispatch::FUND, who, payload)?
        })
    }
    /// An envelope bound to the latest opened epoch, its config and retained roster.
    fn context_call(
        &self,
        operation: Operation,
        who: PrincipalId,
        payload: Vec<u8>,
    ) -> CodecResult<Req> {
        let opened = AdmissionTable::decode(
            decode_shared_state(&self.bytes)?.feature_sections
                [Section::ReputationAdmission.index()],
        )?
        .current_epoch();
        let (epoch, roster) = match opened {
            None => (0, Presence::Absent),
            Some(epoch) => (epoch, Presence::Present(self.roster(epoch)?)),
        };
        Ok(Req {
            epoch,
            config: self.section()?.header.active_config_version,
            roster,
            ..req(operation, who, payload)
        })
    }
    /// A permissionless envelope bound to the retained row of `epoch`.
    fn row_call(
        &self,
        operation: Operation,
        epoch: u64,
        n: u8,
        payload: Vec<u8>,
    ) -> CodecResult<Req> {
        Ok(Req {
            epoch,
            config: self.section()?.header.active_config_version,
            roster: Presence::Present(self.roster(epoch)?),
            request: tag(0x70, n, epoch),
            ..req(operation, principal(KEEPER)?, payload)
        })
    }
    fn run(&self, call: &Req, at: u64) -> CodecResult<(Outcome, Vec<u8>, Vec<u8>)> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; rewards::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = rewards::apply(
            &call.context(at)?,
            &envelope,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        let (state_len, event_len) = match outcome {
            Outcome::Applied {
                state_len,
                event_len,
                ..
            } => (state_len, event_len),
            Outcome::AlreadyApplied { .. } | Outcome::Retained(_) => (0, 0),
        };
        next.truncate(state_len);
        event.truncate(event_len);
        Ok((outcome, next, event))
    }
    /// Commits one applied transition after checking its state, event, value plan and cover
    /// against the tracked physical balance of the rewards account.
    fn apply(&mut self, call: &Req, at: u64) -> CodecResult<Applied> {
        let previous = self.revision()?;
        let committed = self.ledger()?;
        let tail = self
            .settlement()?
            .get(REWARD_STATE_BYTES..)
            .unwrap_or(&[])
            .to_vec();
        let (outcome, next, event) = self.run(call, at)?;
        let Outcome::Applied {
            effect,
            before,
            after,
            response,
            revision,
            result,
            ..
        } = outcome
        else {
            panic!("expected an applied transition, got {outcome:?}");
        };
        assert_eq!(before, committed);
        assert_eq!(revision, previous + 1);
        assert_eq!(result, codec::result_digest(response.as_bytes())?);
        let mut topic = [0; 64];
        let topic_len = codec::event_topic(call.operation, &mut topic)?;
        let (named, common, suffix) = codec::decode_event_frame(&topic[..topic_len], &event)?;
        assert_eq!(named, call.operation);
        assert_eq!(common.market, self.section()?.header.market_id);
        assert_eq!(common.epoch, call.epoch);
        assert_eq!(common.config.get(), call.config);
        assert_eq!(common.revision, revision);
        assert_eq!(common.request, call.request_digest()?);
        assert_eq!(common.result, result);
        assert_eq!(suffix, response.as_bytes());
        let bound = RewardsAccount::for_ledger(ProgramId::new(PROGRAM)?, &before)?;
        let action = value_adapter::plan(&bound, &before, &after, effect)?;
        assert_eq!(value_adapter::cover(&after, self.held, action)?, 0);
        self.held = match action {
            ValueAction::None => self.held,
            ValueAction::Fund { amount } => self.held + amount,
            ValueAction::Pay { amount, .. } => self.held - amount,
        };
        self.bytes = next;
        assert_eq!(self.revision()?, revision);
        assert_eq!(self.section()?.header.state_revision, revision);
        assert_eq!(self.ledger()?, after);
        assert_eq!(
            self.settlement()?.get(REWARD_STATE_BYTES..).unwrap_or(&[]),
            tail
        );
        let free = after.free + after.reserved + after.liability;
        assert_eq!(free, self.held);
        Ok(Applied {
            effect,
            before,
            after,
            response: response.as_bytes().to_vec(),
            revision,
            epoch: common.epoch,
        })
    }
    /// An exact repetition: nothing is written and the subject is returned.
    fn repeated(&self, call: &Req, at: u64) -> CodecResult<[u8; 32]> {
        let before = self.bytes.clone();
        let (outcome, next, event) = self.run(call, at)?;
        assert!(next.is_empty() && event.is_empty());
        assert_eq!(self.bytes, before);
        match outcome {
            Outcome::AlreadyApplied { subject } => Ok(subject),
            other => panic!("expected an exact repetition, got {other:?}"),
        }
    }
    /// A refused call: the committed value and the physical balance stay unchanged.
    fn refused(&self, call: &Req, at: u64) -> ApplicationError {
        let before = self.bytes.clone();
        let held = self.held;
        let error = match self.run(call, at) {
            Err(error) => error,
            Ok((outcome, ..)) => panic!("expected a refusal, got {outcome:?}"),
        };
        assert_eq!(self.bytes, before);
        assert_eq!(self.held, held);
        error
    }
}

/// The reward state after the real `TerminalizeRewards` of the reserved row bound to
/// `binding`, allocating `budget` by one weight per frozen worker (zero weights are a
/// scored-zero vote).
fn terminalized(
    head: &[u8],
    binding: FrozenBinding,
    budget: Amount,
    weights: &[(WorkerRosterEntry, u32)],
    at: u64,
) -> CodecResult<Vec<u8>> {
    let roster: Vec<WorkerRosterEntry> = weights.iter().map(|(entry, _)| *entry).collect();
    let outputs = weights
        .iter()
        .map(|(entry, weight)| {
            let status = if *weight == 0 {
                QualityStatus::ScoredZero
            } else {
                QualityStatus::ScoredPositive
            };
            WorkerAggregate::new(entry.worker, entry.generation, 3, status, *weight, *weight)
        })
        .collect::<CodecResult<Vec<_>>>()?;
    let allocation = allocate(budget, &outputs)?;
    let aggregation =
        EpochAggregation::structural(binding, Digest32::new([5; 32])?, &roster, &outputs)?;
    let mut terminal = vec![0; REWARD_STATE_BYTES];
    decode_reward_state(head)?.terminalize(
        &binding,
        aggregation.root(),
        &allocation,
        &roster,
        at,
        &mut terminal,
    )?;
    Ok(terminal)
}

fn funded(actor: PrincipalId, amount: Amount, d: Amount, f: Amount) -> Vec<u8> {
    let mut out = actor.bytes().to_vec();
    out.extend_from_slice(&amount.to_be_bytes());
    out.extend_from_slice(&d.to_be_bytes());
    out.extend_from_slice(&f.to_be_bytes());
    out.extend_from_slice(&REFUND);
    assert_eq!(out.len(), FUNDED_BYTES);
    out
}

/// The in-process journey: funding authority and replay, two opened epochs (the first with
/// no eligible score, the second allocated 58/43), claims, expiry, closing refunds and
/// pruning, with exact counters, refusals and conservation at every step.
#[allow(clippy::too_many_lines)]
fn journey() -> TestResult {
    let owner = PrincipalId::new(OWNER)?;
    let treasury = principal(TREASURY)?;
    let mut w = World::create(ORIGIN)?;
    let low = w.enroll(LOW, 130)?;
    for n in 2..=4 {
        w.evaluator(n, 129 + u64::from(n))?;
    }
    w.schedule(6, 134)?;

    let mut outsider = w.context_call(
        dispatch::FUND,
        principal(OUTSIDER)?,
        fund_payload(10, REFUND, FUNDING_POLICY_VERSION, true),
    )?;
    outsider.sequence = 1;
    assert_eq!(w.refused(&outsider, 135), UNAUTHORIZED);
    let mut forged = w.fund_call(0, fund_payload(10, REFUND, FUNDING_POLICY_VERSION, true))?;
    forged.submitter = Some(principal(OUTSIDER)?);
    assert_eq!(w.refused(&forged, 135), UNAUTHORIZED);
    w.owner_sequence -= 1;
    for (payload, error) in [
        (
            fund_payload(10, REFUND, 0, true),
            F06_FUNDING_POLICY_MISMATCH,
        ),
        (
            fund_payload(10, REFUND, 2, true),
            F06_FUNDING_POLICY_MISMATCH,
        ),
        (
            fund_payload(10, REFUND, FUNDING_POLICY_VERSION, false),
            F06_CONTRIBUTION_CONSENT_REQUIRED,
        ),
        (
            fund_payload(10, [15; 32], FUNDING_POLICY_VERSION, true),
            F06_REFUND_RECIPIENT_MISMATCH,
        ),
        (
            fund_payload(0, REFUND, FUNDING_POLICY_VERSION, true),
            F06_INVALID_AMOUNT,
        ),
    ] {
        let call = w.fund_call(0, payload)?;
        assert_eq!(w.refused(&call, 135), error);
        w.owner_sequence -= 1;
    }
    let mut gap = w.fund_call(0, fund_payload(10, REFUND, FUNDING_POLICY_VERSION, true))?;
    gap.sequence += 1;
    assert_eq!(w.refused(&gap, 135), SEQUENCE_GAP);
    w.owner_sequence -= 1;
    let mut misbound = w.fund_call(0, fund_payload(10, REFUND, FUNDING_POLICY_VERSION, true))?;
    misbound.config = 2;
    assert_eq!(w.refused(&misbound, 135), WRONG_CONFIG);
    w.owner_sequence -= 1;

    let by_owner = w.fund_call(0, fund_payload(100, REFUND, FUNDING_POLICY_VERSION, true))?;
    let applied = w.apply(&by_owner, 135)?;
    assert_eq!(
        applied.effect,
        RewardEffect::Deposit {
            principal: owner,
            amount: 100
        }
    );
    assert_eq!(applied.response, funded(owner, 100, 100, 100));
    assert_eq!(
        (
            applied.before.tracked_deposits,
            applied.after.tracked_deposits
        ),
        (0, 100)
    );
    assert_eq!(applied.epoch, 0);
    assert_eq!(w.counters()?, [100, 0, 0, 100, 0, 0]);
    let owner_revision = applied.revision;
    let Outcome::Retained(retained) = w.run(&by_owner, 136)?.0 else {
        panic!("an exact Fund retry is the retained result");
    };
    assert_eq!(retained.applied_revision, owner_revision);
    assert_eq!(retained.sequence, by_owner.sequence);
    assert_eq!(
        retained.result_digest,
        codec::result_digest(&applied.response)?
    );
    let conflicting = Req {
        request: [0x77; 32],
        ..by_owner.clone()
    };
    assert_eq!(w.refused(&conflicting, 136), REPLAY_CONFLICT);
    let overflow = w.fund_call(
        0,
        fund_payload(u128::MAX - 50, REFUND, FUNDING_POLICY_VERSION, true),
    )?;
    assert_eq!(w.refused(&overflow, 136), ARITHMETIC);
    w.owner_sequence -= 1;
    let by_treasury = w.fund_call(
        TREASURY,
        fund_payload(20, REFUND, FUNDING_POLICY_VERSION, true),
    )?;
    let applied = w.apply(&by_treasury, 136)?;
    assert_eq!(applied.response, funded(treasury, 20, 120, 120));
    assert_eq!(w.counters()?, [120, 0, 0, 120, 0, 0]);

    let first = w.open(896)?;
    assert_eq!(
        (first.epoch, first.budget, first.workers),
        (6, BUDGET_CAP, 1)
    );
    assert_eq!(w.counters()?, [120, 0, 0, 19, 101, 0]);
    w.advance(897)?;
    assert_eq!(w.section()?.header.lifecycle, ACTIVE);
    let mut stale = w.fund_call(0, fund_payload(5, REFUND, FUNDING_POLICY_VERSION, true))?;
    stale.epoch = 0;
    stale.roster = Presence::Absent;
    assert_eq!(w.refused(&stale, 898), WRONG_EPOCH);
    w.owner_sequence -= 1;
    let mut unrostered = w.fund_call(0, fund_payload(5, REFUND, FUNDING_POLICY_VERSION, true))?;
    unrostered.roster = Presence::Absent;
    assert_eq!(w.refused(&unrostered, 898), WRONG_ROSTER);
    w.owner_sequence -= 1;
    let early = w.row_call(dispatch::EXPIRE_EPOCH_CLAIMS, 6, 1, Vec::new())?;
    assert_eq!(w.refused(&early, 898), WRONG_PHASE);

    let high = w.enroll(LOW + 1, 900)?;
    w.terminalize(&first, &[(low, 0)], 1008)?;
    assert_eq!(w.counters()?, [120, 0, 0, 120, 0, 0]);
    let second = w.open(1024)?;
    assert_eq!(
        (second.epoch, second.budget, second.workers),
        (7, BUDGET_CAP, 2)
    );
    assert_eq!(w.counters()?, [120, 0, 0, 19, 101, 0]);
    let mut frozen = vec![(low, 58), (high, 43)];
    frozen.sort_by_key(|(entry, _)| entry.worker);
    w.terminalize(&second, &frozen, TERMINAL_AT)?;
    assert_eq!(w.counters()?, [120, 0, 0, 19, 0, 101]);

    let low_account = AccountId::new(low.owner.bytes())?;
    let claim = |w: &World, n: u8, worker: WorkerId, recipient: AccountId, amount: Amount| {
        w.row_call(
            dispatch::CLAIM,
            7,
            n,
            claim_payload(worker, recipient, amount)?,
        )
    };
    let wrong_recipient = claim(&w, 1, low.worker, AccountId::new([0x66; 32])?, 58)?;
    assert_eq!(w.refused(&wrong_recipient, 1110), F06_WRONG_CLAIM_RECIPIENT);
    let wrong_amount = claim(&w, 2, low.worker, low_account, 57)?;
    assert_eq!(w.refused(&wrong_amount, 1110), F06_WRONG_CLAIM_AMOUNT);
    let unknown = claim(&w, 3, WorkerId::new([0x67; 32])?, low_account, 58)?;
    assert_eq!(w.refused(&unknown, 1110), F06_UNKNOWN_WORKER_ENTITLEMENT);
    let mut wrong_epoch = claim(&w, 4, low.worker, low_account, 58)?;
    wrong_epoch.roster = Presence::Present(w.roster(6)?);
    assert_eq!(w.refused(&wrong_epoch, 1110), WRONG_ROSTER);
    let mut wrong_config = claim(&w, 5, low.worker, low_account, 58)?;
    wrong_config.config = 2;
    assert_eq!(w.refused(&wrong_config, 1110), WRONG_CONFIG);
    let paid = claim(&w, 6, low.worker, low_account, 58)?;
    let applied = w.apply(&paid, 1110)?;
    assert_eq!(
        applied.effect,
        RewardEffect::Payout {
            recipient: low_account,
            amount: 58
        }
    );
    let settlement = w.settlement()?;
    let row = decode_reward_state(&settlement[..REWARD_STATE_BYTES])?.row(7)?;
    let Presence::Present(allocation) = row.allocation else {
        panic!("an allocated row carries its allocation digest");
    };
    let entitlement = entitlement_id(allocation, low.worker)?;
    let mut expected = entitlement.bytes().to_vec();
    expected.extend_from_slice(&ASSET);
    expected.extend_from_slice(&58u128.to_be_bytes());
    assert_eq!(expected.len(), CLAIM_PAID_BYTES);
    assert_eq!(applied.response, expected);
    assert_eq!(w.counters()?, [120, 58, 0, 19, 0, 43]);
    assert_eq!(w.repeated(&paid, 1111)?, entitlement.bytes());
    let replayed = claim(&w, 7, low.worker, low_account, 58)?;
    assert_eq!(w.repeated(&replayed, 1111)?, entitlement.bytes());

    let expire = w.row_call(dispatch::EXPIRE_EPOCH_CLAIMS, 7, 8, Vec::new())?;
    assert_eq!(w.refused(&expire, EXPIRY_AT - 1), WRONG_PHASE);
    let late = claim(&w, 9, high.worker, AccountId::new(high.owner.bytes())?, 43)?;
    assert_eq!(w.refused(&late, EXPIRY_AT), F06_CLAIM_EXPIRED);
    let applied = w.apply(&expire, EXPIRY_AT)?;
    assert_eq!(applied.effect, RewardEffect::Released(43));
    let mut expected = 1u16.to_be_bytes().to_vec();
    expected.extend_from_slice(&43u128.to_be_bytes());
    assert_eq!(expected.len(), CLAIMS_EXPIRED_BYTES);
    assert_eq!(applied.response, expected);
    assert_eq!(w.counters()?, [120, 58, 0, 62, 0, 0]);
    assert_eq!(w.repeated(&expire, EXPIRY_AT + 1)?, allocation.bytes());
    let settlement = w.settlement()?;
    assert_eq!(
        decode_reward_state(&settlement[..REWARD_STATE_BYTES])?
            .row(7)?
            .status,
        EpochStatus::Expired
    );

    let accepting = w.context_call(dispatch::REFUND_FREE, owner, refund_payload(0, 20)?)?;
    assert_eq!(w.refused(&accepting, EXPIRY_AT + 2), WRONG_PHASE);
    w.lifecycle(WINDING_DOWN)?;
    let closing = w.fund_call(0, fund_payload(5, REFUND, FUNDING_POLICY_VERSION, true))?;
    assert_eq!(w.refused(&closing, EXPIRY_AT + 3), WRONG_PHASE);
    w.owner_sequence -= 1;
    let refund = |w: &World, n: u8, expected: Amount, amount: Amount| -> CodecResult<Req> {
        Ok(Req {
            request: tag(0x50, n, 0),
            ..w.context_call(
                dispatch::REFUND_FREE,
                principal(KEEPER)?,
                refund_payload(expected, amount)?,
            )?
        })
    };
    let mut elsewhere = refund(&w, 1, 0, 20)?;
    elsewhere.payload[32..].copy_from_slice(&[0x66; 32]);
    assert_eq!(
        w.refused(&elsewhere, EXPIRY_AT + 3),
        F06_REFUND_RECIPIENT_MISMATCH
    );
    let first_refund = refund(&w, 2, 0, 20)?;
    let applied = w.apply(&first_refund, EXPIRY_AT + 3)?;
    assert_eq!(
        applied.effect,
        RewardEffect::Payout {
            recipient: AccountId::new(REFUND)?,
            amount: 20
        }
    );
    let mut expected = 0u128.to_be_bytes().to_vec();
    expected.extend_from_slice(&20u128.to_be_bytes());
    expected.extend_from_slice(&REFUND);
    assert_eq!(expected.len(), REFUNDED_BYTES);
    assert_eq!(applied.response, expected);
    assert_eq!(w.counters()?, [120, 58, 20, 42, 0, 0]);
    assert_eq!(
        w.repeated(&first_refund, EXPIRY_AT + 4)?,
        codec::result_digest(&expected)?.bytes()
    );
    assert_eq!(
        w.refused(&refund(&w, 3, 0, 21)?, EXPIRY_AT + 4),
        STALE_CURSOR
    );
    assert_eq!(
        w.refused(&refund(&w, 4, 20, 43)?, EXPIRY_AT + 4),
        INSUFFICIENT_FREE
    );
    w.apply(&refund(&w, 5, 20, 42)?, EXPIRY_AT + 4)?;
    let counters = w.counters()?;
    assert_eq!(counters, [120, 58, 62, 0, 0, 0]);
    assert_eq!(counters[0], counters[1] + counters[2]);
    assert_eq!(w.held, 0);

    let prune_latest = w.row_call(dispatch::PRUNE_EPOCH, 7, 1, Vec::new())?;
    assert_eq!(w.refused(&prune_latest, EXPIRY_AT + 5), WRONG_PHASE);
    let settlement = w.settlement()?;
    let oldest = decode_reward_state(&settlement[..REWARD_STATE_BYTES])?.row(6)?;
    let Presence::Present(unscored) = oldest.allocation else {
        panic!("a terminal row with no eligible score carries its allocation digest");
    };
    let prune = w.row_call(dispatch::PRUNE_EPOCH, 6, 2, Vec::new())?;
    let applied = w.apply(&prune, EXPIRY_AT + 5)?;
    assert_eq!(
        applied.effect,
        RewardEffect::Pruned(Presence::Present(unscored))
    );
    assert_eq!(applied.response, unscored.bytes());
    assert_eq!(applied.response.len(), PRUNED_BYTES);
    assert_eq!(w.counters()?, [120, 58, 62, 0, 0, 0]);
    assert_eq!(applied.after.retained_epochs, 1);
    let mut pruned = w.row_call(
        dispatch::CLAIM,
        7,
        3,
        claim_payload(low.worker, low_account, 58)?,
    )?;
    pruned.epoch = 6;
    assert_eq!(w.refused(&pruned, EXPIRY_AT + 6), NOT_FOUND);
    let settlement = w.settlement()?;
    assert_eq!(
        decode_reward_state(&settlement[..REWARD_STATE_BYTES])?.row(6),
        Err(NOT_FOUND)
    );
    Ok(())
}

/// A15: forged committed ledger bytes are refused before any mutation, and a conserving
/// forgery that the ledger accepts cannot be covered by the physical balance.
fn forged_ledger() -> TestResult {
    let mut w = World::create(ORIGIN)?;
    let call = w.fund_call(0, fund_payload(100, REFUND, FUNDING_POLICY_VERSION, true))?;
    w.apply(&call, 135)?;
    let honest = w.bytes.clone();
    w.edit(|parts, _| {
        parts.rewards[FREE_LOW] += 1;
        Ok(())
    })?;
    let next = w.fund_call(0, fund_payload(5, REFUND, FUNDING_POLICY_VERSION, true))?;
    assert_eq!(w.refused(&next, 136), F06_LEDGER_INVARIANT_VIOLATION);
    w.bytes = honest;
    w.edit(|parts, _| {
        parts.rewards[DEPOSITS_LOW] += 1;
        parts.rewards[FREE_LOW] += 1;
        Ok(())
    })?;
    let (outcome, ..) = w.run(&next, 136)?;
    let Outcome::Applied {
        effect,
        before,
        after,
        ..
    } = outcome
    else {
        panic!("a conserving forgery passes the ledger, got {outcome:?}");
    };
    assert_eq!(after.free, 106);
    let bound = RewardsAccount::for_ledger(ProgramId::new(PROGRAM)?, &before)?;
    let action = value_adapter::plan(&bound, &before, &after, effect)?;
    assert_eq!(action, ValueAction::Fund { amount: 5 });
    assert_eq!(
        value_adapter::cover(&after, w.held, action),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    Ok(())
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|b| [DIGITS[usize::from(b >> 4)], DIGITS[usize::from(b & 15)]])
        .map(char::from)
        .collect()
}
fn unhex(text: &str) -> Checked<Vec<u8>> {
    if text == "-" {
        return Ok(Vec::new());
    }
    if !text.len().is_multiple_of(2) {
        return Err(harness(format!("odd hex length {}", text.len())));
    }
    (0..text.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&text[i..i + 2], 16).map_err(|e| harness(format!("hex {e}"))))
        .collect()
}
fn fixed(bytes: &[u8]) -> Checked<[u8; 32]> {
    bytes
        .try_into()
        .map_err(|_| harness(format!("expected 32 bytes, got {}", bytes.len())))
}
fn number<T: core::str::FromStr>(token: &str) -> Checked<T> {
    token
        .parse()
        .map_err(|_| harness(format!("bad number {token}")))
}

struct Manifest {
    wasm: String,
    harness: String,
    chain: [u8; 32],
}
fn manifest() -> Checked<Manifest> {
    let path = std::env::var_os("PAXAI_HOST_BOUNDARY_MANIFEST").map_or_else(
        || {
            PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                .join("../../../build/paxai-host-boundary/manifest")
        },
        PathBuf::from,
    );
    let text = std::fs::read_to_string(&path).map_err(|e| {
        harness(format!(
            "{}: {e}; run make paxai-host-boundary-build",
            path.display()
        ))
    })?;
    let lines: Vec<&str> = text.lines().collect();
    let [wasm, binary, chain] = lines[..] else {
        return Err(harness(format!("manifest has {} lines", lines.len())));
    };
    Ok(Manifest {
        wasm: wasm.to_owned(),
        harness: binary.to_owned(),
        chain: fixed(&unhex(chain)?)?,
    })
}

struct NativeEvent {
    producer: Vec<u8>,
    principal: Vec<u8>,
    topic: Vec<u8>,
    data: Vec<u8>,
}
struct Cursor<'a> {
    bytes: &'a [u8],
    at: usize,
}
impl<'a> Cursor<'a> {
    fn take(&mut self, n: usize) -> Checked<&'a [u8]> {
        let end = self
            .at
            .checked_add(n)
            .ok_or_else(|| harness("event overflow"))?;
        let out = self
            .bytes
            .get(self.at..end)
            .ok_or_else(|| harness("short event envelope"))?;
        self.at = end;
        Ok(out)
    }
    fn be32(&mut self) -> Checked<usize> {
        let bytes: [u8; 4] = self
            .take(4)?
            .try_into()
            .map_err(|_| harness("event length"))?;
        Ok(usize::try_from(u32::from_be_bytes(bytes))?)
    }
}
fn native_events(raw: &[u8]) -> Checked<Vec<NativeEvent>> {
    if raw.is_empty() {
        return Ok(Vec::new());
    }
    let mut r = Cursor { bytes: raw, at: 0 };
    if r.take(EVENTS_DOMAIN.len())? != EVENTS_DOMAIN {
        return Err(harness("event envelope domain"));
    }
    let count = r.be32()?;
    let mut events = Vec::new();
    for _ in 0..count {
        let producer = r.take(32)?.to_vec();
        let principal = r.take(32)?.to_vec();
        r.take(9)?;
        let topic_len = r.be32()?;
        let topic = r.take(topic_len)?.to_vec();
        let data_len = r.be32()?;
        let data = r.take(data_len)?.to_vec();
        events.push(NativeEvent {
            producer,
            principal,
            topic,
            data,
        });
    }
    if r.at != raw.len() {
        return Err(harness("trailing event envelope bytes"));
    }
    Ok(events)
}

struct Line {
    status: i64,
    result_code: i64,
    kind: u8,
    abi: u16,
    terminal: Vec<u8>,
    state: Vec<u8>,
    blobs_before: usize,
    blobs_after: usize,
    application_blobs: usize,
    kv_before: usize,
    kv_after: usize,
    balance_before: u64,
    balance_after: u64,
    fee: u64,
    rewards_balance: u64,
    events: Vec<NativeEvent>,
}
fn parse_line(text: &str) -> Checked<Line> {
    let t: Vec<&str> = text.split_whitespace().collect();
    if t.len() != 18 || t[0] != "call" {
        return Err(harness(format!("unexpected line {text}")));
    }
    Ok(Line {
        status: number(t[1])?,
        result_code: number(t[2])?,
        kind: number(t[3])?,
        abi: number(t[4])?,
        terminal: unhex(t[5])?,
        state: unhex(t[6])?,
        blobs_before: number(t[7])?,
        blobs_after: number(t[8])?,
        application_blobs: number(t[9])?,
        kv_before: number(t[10])?,
        kv_after: number(t[11])?,
        balance_before: number(t[13])?,
        balance_after: number(t[14])?,
        fee: number(t[15])?,
        rewards_balance: number(t[16])?,
        events: native_events(&unhex(t[17])?)?,
    })
}

enum Terminal {
    Success(Vec<u8>),
    Rejected(Vec<u8>),
}
fn terminal(line: &Line, program: ProgramId) -> Checked<Terminal> {
    let decoded = decode_terminal_payload(line.kind, line.abi, &line.terminal)
        .map_err(|e| harness(format!("terminal {e:?}")))?;
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 {
        program: producer,
        outcome,
        ..
    }) = decoded.detail
    else {
        return Err(harness("terminal is not a CandidateV4 execution"));
    };
    assert_eq!(producer, program.bytes());
    match outcome {
        CandidateTerminalOutcome::Success { code: 0, response } => Ok(Terminal::Success(response)),
        CandidateTerminalOutcome::Failure(failure) if failure.class() == RefusalClass::Rejected => {
            Ok(Terminal::Rejected(failure.reason().bytes().to_vec()))
        }
        _ => Err(harness(
            "terminal is neither success code 0 nor a Rejected refusal",
        )),
    }
}

/// The registered Program activity in the native host harness.
struct Native {
    child: Child,
    input: Option<ChildStdin>,
    output: BufReader<ChildStdout>,
    chain: ChainDomain,
    program: ProgramId,
    principals: [PrincipalId; 3],
    rewards: [u8; 32],
    /// Kernel blob, staged blob and module KV limits from the bring-up line.
    limits: [usize; 3],
    state: Option<Vec<u8>>,
}
impl Drop for Native {
    fn drop(&mut self) {
        if self.input.is_some() {
            drop(self.child.kill());
            drop(self.child.wait());
        }
    }
}
impl Native {
    fn start() -> Checked<Self> {
        let m = manifest()?;
        let mut child = Command::new(&m.harness)
            .arg(&m.wasm)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .spawn()?;
        let input = child.stdin.take().ok_or_else(|| harness("stdin"))?;
        let stdout = child.stdout.take().ok_or_else(|| harness("stdout"))?;
        let mut output = BufReader::new(stdout);
        let mut ready = String::new();
        output.read_line(&mut ready)?;
        let t: Vec<&str> = ready.split_whitespace().collect();
        if t.len() != 9 || t[0] != "ready" {
            return Err(harness(format!("bring-up line {ready}")));
        }
        let principal =
            |i: usize| -> Checked<PrincipalId> { Ok(PrincipalId::new(fixed(&unhex(t[i])?)?)?) };
        Ok(Self {
            child,
            input: Some(input),
            output,
            chain: ChainDomain::new(m.chain)?,
            program: ProgramId::new(fixed(&unhex(t[1])?)?)?,
            principals: [principal(2)?, principal(3)?, principal(4)?],
            rewards: fixed(&unhex(t[5])?)?,
            limits: [number(t[6])?, number(t[7])?, number(t[8])?],
            state: None,
        })
    }
    fn finish(mut self) -> Checked {
        drop(self.input.take());
        let status = self.child.wait()?;
        assert!(status.success(), "native harness exit {status}");
        Ok(())
    }
    fn context(&self, actor: usize, height: u64) -> CallContext {
        CallContext {
            chain: self.chain,
            program: self.program,
            principal: self.principals[actor],
            height,
        }
    }
    fn envelope(
        &self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &[u8],
    ) -> Checked<Vec<u8>> {
        let mut out = vec![0; 16_384];
        let n = encode_envelope(
            &Envelope {
                operation,
                chain: self.chain,
                program: self.program,
                market: derive_market(self.chain, self.program)?,
                actor: self.principals[actor],
                epoch: 0,
                config: 1,
                roster: Presence::Absent,
                sequence,
                expiry: NATIVE_EXPIRY,
                request: RequestId::new(tag(0x80, u8::try_from(actor)?, sequence))?,
                payload,
                authentication: Authentication::Native,
            },
            &mut out,
        )?;
        out.truncate(n);
        Ok(out)
    }
    fn submit(&mut self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Line> {
        let line = self.submit_raw(actor, height, envelope)?;
        assert_eq!(line.status, 0, "native CALL status");
        Ok(line)
    }
    /// One CALL whose kernel status may be a refusal.
    fn submit_raw(&mut self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Line> {
        let input = self.input.as_mut().ok_or_else(|| harness("closed"))?;
        writeln!(input, "{actor} {height} {}", hex(envelope))?;
        input.flush()?;
        let mut text = String::new();
        if self.output.read_line(&mut text)? == 0 {
            return Err(harness("native harness stopped"));
        }
        parse_line(&text)
    }
    /// The in-process outcome of one reward call over `current` and the next state it writes.
    fn local(
        &self,
        actor: usize,
        height: u64,
        current: &[u8],
        envelope: &[u8],
    ) -> Checked<(Outcome, Vec<u8>)> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; rewards::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = rewards::apply(
            &self.context(actor, height),
            &decode_envelope(envelope)?,
            current,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        let state_len = match outcome {
            Outcome::Applied { state_len, .. } => state_len,
            Outcome::AlreadyApplied { .. } | Outcome::Retained(_) => 0,
        };
        next.truncate(state_len);
        Ok((outcome, next))
    }
    /// Real F01 CREATE through the routed Program; the committed state is the in-process one.
    fn create(&mut self, height: u64) -> Checked {
        let mut payload = self.principals[NATIVE_OWNER].bytes().to_vec();
        payload.extend_from_slice(&NATIVE_ASSET);
        payload.extend_from_slice(
            derive_rewards_account(self.program, AssetId::new(NATIVE_ASSET)?)?.as_bytes(),
        );
        payload.extend_from_slice(&REFUND);
        payload.push(0);
        payload.extend_from_slice(&policy_bytes()?);
        payload.extend_from_slice(&[16; 32]);
        let envelope = self.envelope(dispatch::CREATE, NATIVE_OWNER, 1, &payload)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut result = vec![0; MAX_RESULT_BYTES];
        let dispatch::Routed::Applied { state_len, .. } = dispatch::route(
            &self.context(NATIVE_OWNER, height),
            &envelope,
            None,
            dispatch::Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?
        else {
            return Err(harness("in-process CREATE did not apply"));
        };
        let line = self.submit(NATIVE_OWNER, height, &envelope)?;
        assert!(matches!(
            terminal(&line, self.program)?,
            Terminal::Success(_)
        ));
        assert_eq!(line.state, next[..state_len]);
        self.state = Some(line.state);
        Ok(())
    }
}

/// Native Fund: the owner's deposit moves exactly `amount` into the registered rewards
/// account in the same commit as the state and replay record; an outsider's Fund is
/// rejected with nothing but the fee charged.
fn native_fund() -> Checked {
    let mut n = Native::start()?;
    assert_eq!(
        derive_rewards_account(n.program, AssetId::new(NATIVE_ASSET)?)?.bytes(),
        n.rewards
    );
    n.create(1000)?;
    let committed = n
        .state
        .clone()
        .ok_or_else(|| harness("no committed state"))?;
    let payload = fund_payload(120, REFUND, FUNDING_POLICY_VERSION, true);
    let envelope = n.envelope(dispatch::FUND, NATIVE_OWNER, 2, &payload)?;
    let validated = decode_envelope(&envelope)?;
    let request = validated.request_digest()?;
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut scratch = vec![0; rewards::SCRATCH_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Outcome::Applied {
        response,
        revision,
        state_len,
        event_len,
        after,
        ..
    } = rewards::apply(
        &n.context(NATIVE_OWNER, 1001),
        &validated,
        &committed,
        &mut next,
        &mut scratch,
        &mut event,
    )?
    else {
        return Err(harness("in-process Fund did not apply"));
    };
    assert_eq!(after.tracked_deposits, 120);
    let mut frame = vec![0; MAX_RESULT_BYTES];
    let frame_len = codec::encode_result(
        &ApplicationResult::success(ResultStatus::Ok, request, revision, response.as_bytes())?,
        &mut frame,
    )?;
    let line = n.submit(NATIVE_OWNER, 1001, &envelope)?;
    let Terminal::Success(body) = terminal(&line, n.program)? else {
        return Err(harness("native Fund was rejected"));
    };
    assert_eq!(line.result_code, 0);
    assert_eq!(body, frame[..frame_len]);
    assert_eq!(line.state, next[..state_len]);
    assert_eq!(line.balance_after, line.balance_before - line.fee - 120);
    let [emitted] = &line.events[..] else {
        return Err(harness(format!("{} native events", line.events.len())));
    };
    let mut topic = [0; 64];
    let topic_len = codec::event_topic(dispatch::FUND, &mut topic)?;
    assert_eq!(emitted.producer, n.program.bytes());
    assert_eq!(emitted.principal, n.principals[NATIVE_OWNER].bytes());
    assert_eq!(emitted.topic, &topic[..topic_len]);
    assert_eq!(emitted.data, event[..event_len]);
    let funded_balance = line.rewards_balance;
    n.state = Some(line.state);

    let outsider = n.envelope(dispatch::FUND, NATIVE_OUTSIDER, 1, &payload)?;
    let refused = rewards::apply(
        &n.context(NATIVE_OUTSIDER, 1002),
        &decode_envelope(&outsider)?,
        n.state.as_deref().unwrap_or_default(),
        &mut next,
        &mut scratch,
        &mut event,
    );
    assert_eq!(refused, Err(UNAUTHORIZED));
    let mut frame = vec![0; MAX_RESULT_BYTES];
    let frame_len = codec::encode_result(
        &ApplicationResult::failure(
            UNAUTHORIZED,
            Presence::Present(decode_envelope(&outsider)?.request_digest()?),
            revision,
        )?,
        &mut frame,
    )?;
    let line = n.submit(NATIVE_OUTSIDER, 1002, &outsider)?;
    let Terminal::Rejected(reason) = terminal(&line, n.program)? else {
        return Err(harness("native outsider Fund applied"));
    };
    assert_eq!(line.result_code, PROGRAM_REFUSED);
    assert_eq!(reason, frame[..frame_len]);
    assert_eq!(Some(&line.state), n.state.as_ref());
    assert_eq!(line.rewards_balance, funded_balance);
    assert_eq!(line.balance_after, line.balance_before - line.fee);
    assert!(line.events.is_empty());
    n.finish()
}

#[test]
fn reward_conservation_authority_and_rollback() -> Checked {
    journey()?;
    forged_ledger()?;
    native_fund()
}

/// The reward state of one committed shared state value.
fn reward_view(state: &[u8]) -> CodecResult<RewardState<'_>> {
    let settlement =
        decode_shared_state(state)?.feature_sections[Section::SettlementClaims.index()];
    decode_reward_state(settlement.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)
}
/// Disposition and entitlement of `worker` in the retained row of `epoch`.
fn entitlement_of(
    state: &[u8],
    epoch: u64,
    worker: WorkerId,
) -> CodecResult<(Disposition, Amount)> {
    let rewards = reward_view(state)?;
    let dictionary = rewards.dictionary();
    for entry in rewards.row(epoch)?.entries() {
        if dictionary.slot(entry.slot)?.worker == worker {
            return Ok((entry.disposition, entry.entitlement));
        }
    }
    Err(NOT_FOUND)
}
/// Host `ProgramRead` facts of one native state root.
fn read_proof(root: u8) -> CodecResult<ReadProof> {
    Ok(ReadProof {
        chain: ChainDomain::new(CHAIN)?,
        program: ProgramId::new(PROGRAM)?,
        native_state_root: Digest32::new([root; 32])?,
        observed_sequence: u64::from(root),
        execution_height: 1000 + u64::from(root),
        batch_id: Digest32::new([root ^ 0x80; 32])?,
    })
}
/// One real `READ_STATE_CHUNK` of `state` at `offset` under `pin`; returns the body length.
fn chunk(
    state: &[u8],
    pin: (u64, Presence<StateDigest>),
    offset: usize,
    out: &mut [u8],
) -> CodecResult<usize> {
    let mut payload = [0; 46];
    let n = codec::encode_chunk_request(
        &ChunkRequest {
            revision: pin.0,
            digest: pin.1,
            offset: u32::try_from(offset).map_err(|_| ARITHMETIC)?,
            requested: u16::try_from(MAX_CHUNK_BYTES).map_err(|_| ARITHMETIC)?,
        },
        &mut payload,
    )?;
    read_state_chunk(state, &payload[..n], out)
}
/// A whole-state capture of `state` proved by native root `root`, pinned after its first
/// chunk, then bound to finality evidence of rank `rank` for root `evidenced`.
fn capture(
    state: &[u8],
    root: u8,
    evidenced: u8,
    rank: u8,
) -> QueryResult<(Vec<u8>, SnapshotBinding)> {
    let proof = read_proof(root)?;
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut body = vec![0; MAX_RESULT_BYTES];
    let mut assembly = StateCapture::new(&mut buffer);
    let (mut pin, mut offset) = (UNPINNED, 0);
    loop {
        let n = chunk(state, pin, offset, &mut body)?;
        assembly.accept(&proof, &body[..n])?;
        let response = codec::decode_chunk_response(&body[..n])?;
        pin = (response.revision, Presence::Present(response.digest));
        offset += response.bytes.len();
        if offset == usize::try_from(response.total_bytes).map_err(|_| ARITHMETIC)? {
            break;
        }
    }
    let (bytes, facts) = assembly.finish()?;
    let binding = bind_snapshot(
        bytes,
        &facts,
        &FinalityEvidence {
            native_state_root: Digest32::new([evidenced; 32])?,
            checkpoint: Digest32::new([0x3c; 32])?,
            settlement: Presence::Absent,
            rank,
        },
        PUBLISHED_AT_MS,
    )?;
    Ok((bytes.to_vec(), binding))
}
/// A synthetic frozen worker with a distinct (worker, recipient) pair.
fn pinned_entry(i: u8) -> CodecResult<WorkerRosterEntry> {
    let distinct = |fill: u8| {
        let mut bytes = [fill; 32];
        bytes[0] = i;
        bytes
    };
    Ok(WorkerRosterEntry {
        worker: WorkerId::new(distinct(0x5c))?,
        owner: PrincipalId::new(distinct(0x5d))?,
        recipient: AccountId::new(distinct(0x6d))?,
        generation: version()?,
        key_version: version()?,
        public_key: PublicKey32(distinct(0x5e)),
        metadata: MetadataDigest::new(METADATA)?,
    })
}
/// The frozen 58/43 weights of `low` and `high` in `WorkerId` order.
fn allocated(low: WorkerRosterEntry, high: WorkerRosterEntry) -> Vec<(WorkerRosterEntry, u32)> {
    let mut weights = vec![(low, 58), (high, 43)];
    weights.sort_by_key(|(entry, _)| entry.worker);
    weights
}

/// Recovery and capacity steps over the committed value.
impl World {
    /// An `OPEN_EPOCH` of the clock epoch of `at` that both the preview and the real opening
    /// refuse with the same code; nothing is written.
    fn open_refused(&self, at: u64) -> CodecResult<ApplicationError> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let previewed = match epoch::preview_open(&self.bytes, at, &mut next, &mut scratch) {
            Err(error) => error,
            Ok(frozen) => panic!("expected a refused opening preview, got {frozen:?}"),
        };
        let call = Req {
            epoch: (at - ORIGIN) / EPOCH_SPAN_HEIGHTS,
            config: self.section()?.header.active_config_version,
            roster: Presence::Present(RosterDigest::new([0x5a; 32])?),
            request: tag(0x61, 0, at),
            ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
        };
        let encoded = call.encode()?;
        let mut event = vec![0; MAX_EVENT_BYTES];
        let refused = match epoch::open_epoch(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        ) {
            Err(error) => error,
            Ok(outcome) => panic!("expected a refused opening, got {outcome:?}"),
        };
        assert_eq!(refused, previewed);
        Ok(refused)
    }
    /// A call run with `next` and scratch buffers of the given sizes; it must refuse.
    fn refused_sized(
        &self,
        call: &Req,
        at: u64,
        next: usize,
        scratch: usize,
    ) -> CodecResult<ApplicationError> {
        let encoded = call.encode()?;
        let mut next = vec![0; next];
        let mut scratch = vec![0; scratch];
        let mut event = vec![0; MAX_EVENT_BYTES];
        match rewards::apply(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        ) {
            Err(error) => Ok(error),
            Ok(outcome) => panic!("expected a buffer refusal, got {outcome:?}"),
        }
    }
    /// A permissionless Claim of `entry`'s exact entitlement in `epoch`.
    fn claim_call(
        &self,
        epoch: u64,
        n: u8,
        entry: &WorkerRosterEntry,
        amount: Amount,
    ) -> CodecResult<Req> {
        self.row_call(
            dispatch::CLAIM,
            epoch,
            n,
            claim_payload(entry.worker, entry.recipient, amount)?,
        )
    }
    /// A permissionless `RefundFree` of `amount` after `expected` already refunded.
    fn refund_call(&self, n: u8, expected: Amount, amount: Amount) -> CodecResult<Req> {
        Ok(Req {
            request: tag(0x50, n, 0),
            ..self.context_call(
                dispatch::REFUND_FREE,
                principal(KEEPER)?,
                refund_payload(expected, amount)?,
            )?
        })
    }
    /// Renews every F02 worker manifest at `at` for another 4096 heights.
    fn renew(&mut self, at: u64) -> TestResult {
        self.edit(|parts, _| {
            let records: Vec<WorkerCurrent> = parts.workers.iter().copied().collect();
            for record in records {
                parts.workers.replace(&WorkerCurrent {
                    valid_from: at,
                    expiry: at + 4096,
                    last_metadata_height: at,
                    ..record
                })?;
            }
            Ok(())
        })
    }
    /// Pins all 256 dictionary slots with `real` and 255 synthetic pairs: eight completed
    /// epochs 1..=8 of 32 members each, every one reserved by the real `ReserveEpoch` and
    /// allocated one unit per member by the real `TerminalizeRewards` at `at`.
    fn pin_dictionary(&mut self, real: WorkerRosterEntry, at: u64) -> TestResult {
        let mut members = (0..=254)
            .map(pinned_entry)
            .collect::<CodecResult<Vec<_>>>()?;
        members.push(real);
        members.sort_by_key(|entry| entry.worker);
        self.edit(|parts, market| {
            let (head, tail) = parts
                .rewards
                .split_at_checked(REWARD_STATE_BYTES)
                .ok_or(NOT_FOUND)?;
            let tail = tail.to_vec();
            let mut state = head.to_vec();
            for (n, roster) in (1u8..).zip(members.chunks(MAX_WORKERS)) {
                let epoch = u64::from(n);
                let digest = RosterDigest::new([0xd0 + n; 32])?;
                let budget = Amount::try_from(roster.len()).map_err(|_| ARITHMETIC)?;
                let mut reserved = vec![0; REWARD_STATE_BYTES];
                decode_reward_state(&state)?.reserve_epoch(
                    epoch,
                    budget,
                    digest,
                    roster,
                    at,
                    &mut reserved,
                )?;
                let binding = FrozenBinding {
                    chain: market.deployment_chain_domain,
                    program: market.program_id,
                    market: market.market_id,
                    epoch,
                    config: version()?,
                    roster: digest,
                };
                let weights: Vec<(WorkerRosterEntry, u32)> =
                    roster.iter().map(|entry| (*entry, 1)).collect();
                state = terminalized(&reserved, binding, budget, &weights, at)?;
            }
            state.extend_from_slice(&tail);
            parts.rewards = state;
            Ok(())
        })
    }
}

/// An active market whose first epoch 6 froze one worker and ended unscored; the second
/// worker joins in the next enrollment window and both serve epoch 7.
fn active_market() -> CodecResult<(World, WorkerRosterEntry, WorkerRosterEntry)> {
    let mut w = World::create(ORIGIN)?;
    let low = w.enroll(LOW, 130)?;
    for n in 2..=4 {
        w.evaluator(n, 129 + u64::from(n))?;
    }
    w.schedule(6, 134)?;
    let fund = w.fund_call(0, fund_payload(120, REFUND, FUNDING_POLICY_VERSION, true))?;
    w.apply(&fund, 135)?;
    let first = w.open(896)?;
    assert_eq!(
        (first.epoch, first.budget, first.workers),
        (6, BUDGET_CAP, 1)
    );
    w.advance(897)?;
    let high = w.enroll(LOW + 1, 898)?;
    w.terminalize(&first, &[(low, 0)], 1000)?;
    assert_eq!(w.counters()?, [120, 0, 0, 120, 0, 0]);
    Ok((w, low, high))
}

/// A funded market whose epoch 7 is open over two workers: `[120, 0, 0, 19, 101, 0]`.
fn opened_market() -> CodecResult<(World, Frozen, WorkerRosterEntry, WorkerRosterEntry)> {
    let (mut w, low, high) = active_market()?;
    let frozen = w.open(1024)?;
    assert_eq!(
        (frozen.epoch, frozen.budget, frozen.workers),
        (7, BUDGET_CAP, 2)
    );
    assert_eq!(w.counters()?, [120, 0, 0, 19, 101, 0]);
    Ok((w, frozen, low, high))
}

/// A13: with no keeper through the settlement interval the reserve stays whole and no later
/// epoch opens; the late keeper allocates the sealed epoch once, and closing withdraws
/// neither R before terminalization nor C after it.
fn late_settlement_and_close() -> TestResult {
    let (mut w, frozen, low, high) = opened_market()?;
    let reserved = w.counters()?;
    for at in [1152, 1280] {
        assert_eq!(w.open_refused(at)?, WRONG_PHASE);
        assert_eq!(w.counters()?, reserved);
    }
    let row = reward_view(&w.bytes)?.row(7)?;
    assert_eq!(
        (row.status, row.budget),
        (EpochStatus::Reserved, BUDGET_CAP)
    );
    let accepting = w.refund_call(1, 0, 19)?;
    assert_eq!(w.refused(&accepting, 1281), WRONG_PHASE);

    w.lifecycle(WINDING_DOWN)?;
    assert_eq!(w.open_refused(1408)?, F01_LIFECYCLE_CLOSED);
    let reserve = w.refund_call(2, 0, 20)?;
    assert_eq!(w.refused(&reserve, 1409), INSUFFICIENT_FREE);
    let free = w.refund_call(3, 0, 19)?;
    w.apply(&free, 1409)?;
    assert_eq!(w.counters()?, [120, 0, 19, 0, 101, 0]);
    assert_eq!(w.held, 101);

    let weights = allocated(low, high);
    w.terminalize(&frozen, &weights, 1500)?;
    assert_eq!(w.counters()?, [120, 0, 19, 0, 0, 101]);
    let row = reward_view(&w.bytes)?.row(7)?;
    assert_eq!(
        (row.status, row.terminal_height, row.expiry_height),
        (EpochStatus::Terminal, 1500, 1500 + 4096)
    );
    let terminal = w.bytes.clone();
    assert_eq!(
        w.terminalize(&frozen, &weights, 1501),
        Err(F06_EPOCH_TERMINAL)
    );
    assert_eq!(w.bytes, terminal);
    let liability = w.refund_call(4, 19, 1)?;
    assert_eq!(w.refused(&liability, 1502), INSUFFICIENT_FREE);
    let claim = w.claim_call(7, 1, &low, 58)?;
    w.apply(&claim, 1503)?;
    assert_eq!(w.counters()?, [120, 58, 19, 0, 0, 43]);
    Ok(())
}

/// A18 and the in-process half of A17: a Claim whose response is lost is recovered from a
/// finalized whole-state capture, never from an unfinalized, misbound, torn or stale one,
/// and its blind resubmission pays nothing; a payout whose physical cover, host transfer or
/// buffers are unavailable commits nothing and leaves the claim unpaid, then pays once.
#[allow(clippy::too_many_lines)]
fn finality_recovery() -> QueryResult<()> {
    let (mut w, frozen, low, high) = opened_market()?;
    w.terminalize(&frozen, &allocated(low, high), 1100)?;
    assert_eq!(w.counters()?, [120, 0, 0, 19, 0, 101]);

    let before = w.bytes.clone();
    let paid = w.claim_call(7, 1, &low, 58)?;
    let lost = w.apply(&paid, 1110)?;
    let held = w.held;
    assert_eq!(held, 62);

    let (_, unfinalized) = capture(&w.bytes, 0x31, 0x31, FINALIZED_RANK - 1)?;
    assert_eq!(
        unfinalized.require_finalized(),
        Err(QueryError::FinalityUnavailable)
    );
    assert_eq!(
        capture(&w.bytes, 0x31, 0x32, FINALIZED_RANK).err(),
        Some(QueryError::BindingMismatch)
    );
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut body = vec![0; MAX_RESULT_BYTES];
    let old = read_proof(0x30)?;
    let mut torn = StateCapture::new(&mut buffer);
    let n = chunk(&before, UNPINNED, 0, &mut body)?;
    torn.accept(&old, &body[..n])?;
    let n = chunk(&w.bytes, UNPINNED, MAX_CHUNK_BYTES, &mut body)?;
    assert_eq!(
        torn.accept(&old, &body[..n]),
        Err(QueryError::SnapshotConflict)
    );
    let n = chunk(&before, UNPINNED, MAX_CHUNK_BYTES, &mut body)?;
    assert_eq!(
        torn.accept(&old, &body[..n]),
        Err(QueryError::IntegrityFailure)
    );
    assert_eq!(torn.finish().err(), Some(QueryError::IntegrityFailure));
    let stale = (
        decode_shared_state(&before)?.revision,
        Presence::Present(codec::state_digest(&before)?),
    );
    assert_eq!(chunk(&w.bytes, stale, 0, &mut body), Err(CONFLICT));

    let (captured, binding) = capture(&w.bytes, 0x31, 0x31, FINALIZED_RANK)?;
    binding.require_finalized()?;
    assert_eq!(captured, w.bytes);
    assert_eq!(
        (binding.revision, binding.state_digest),
        (lost.revision, codec::state_digest(&w.bytes)?)
    );
    assert_eq!(
        entitlement_of(&captured, 7, low.worker)?,
        (Disposition::Claimed, 58)
    );
    let recovered = reward_view(&captured)?;
    let row = recovered.row(7)?;
    assert_eq!(row.paid_sum, 58);
    assert_eq!(recovered.ledger()?.total_claimed, 58);
    let Presence::Present(allocation) = row.allocation else {
        panic!("an allocated row carries its allocation digest");
    };
    let entitlement = entitlement_id(allocation, low.worker)?;
    let mut acknowledged = entitlement.bytes().to_vec();
    acknowledged.extend_from_slice(&ASSET);
    acknowledged.extend_from_slice(&58u128.to_be_bytes());
    assert_eq!(acknowledged, lost.response);
    assert_eq!(w.repeated(&paid, 1111)?, entitlement.bytes());
    assert_eq!(w.held, held);

    let unpaid = w.bytes.clone();
    let payout = w.claim_call(7, 2, &high, 43)?;
    let (outcome, next, _) = w.run(&payout, 1112)?;
    let Outcome::Applied {
        effect,
        before: prev,
        after,
        ..
    } = outcome
    else {
        panic!("an unpaid entitlement is payable, got {outcome:?}");
    };
    assert_eq!(
        effect,
        RewardEffect::Payout {
            recipient: high.recipient,
            amount: 43
        }
    );
    let bound = RewardsAccount::for_ledger(ProgramId::new(PROGRAM)?, &prev)?;
    let action = value_adapter::plan(&bound, &prev, &after, effect)?;
    assert_eq!(
        action,
        ValueAction::Pay {
            recipient: high.recipient,
            amount: 43
        }
    );
    assert_eq!(value_adapter::cover(&after, held, action), Ok(0));
    assert_eq!(
        value_adapter::cover(&after, held - 1, action),
        Err(F06_LEDGER_INVARIANT_VIOLATION)
    );
    for (error, expected) in [
        (ProgramError::Host(HostRefusal::Denied), HOST_CAPABILITY),
        (ProgramError::Host(HostRefusal::Bounds), HOST_TRANSFER),
        (ProgramError::Host(HostRefusal::Meter), HOST_TRANSFER),
        (
            ProgramError::Value(ValueError::new(Field::Amount, Reason::Zero)),
            F06_INVALID_AMOUNT,
        ),
    ] {
        assert_eq!(value_adapter::transfer_refusal(error), expected);
    }
    for (refusal, expected) in [
        (HostRefusal::Denied, HOST_CAPABILITY),
        (HostRefusal::Evidence, READINESS_BLOCKED),
        (HostRefusal::VerificationFailed, READINESS_BLOCKED),
    ] {
        assert_eq!(
            value_adapter::balance_refusal(ProgramError::Host(refusal)),
            expected
        );
    }
    assert_eq!(
        w.refused_sized(&payout, 1112, next.len() - 1, rewards::SCRATCH_BYTES)?,
        CAPACITY
    );
    assert_eq!(
        w.refused_sized(
            &payout,
            1112,
            MAX_STATE_BYTES,
            Section::PolicyLifecycle.payload_cap()
        )?,
        CAPACITY
    );
    assert_eq!(w.bytes, unpaid);
    assert_eq!(w.held, held);
    let (captured, binding) = capture(&w.bytes, 0x33, 0x33, FINALIZED_RANK)?;
    binding.require_finalized()?;
    assert_eq!(captured, unpaid);
    assert_eq!(
        entitlement_of(&captured, 7, high.worker)?,
        (Disposition::Unclaimed, 43)
    );
    assert_eq!(reward_view(&captured)?.ledger()?.liability, 43);

    let applied = w.apply(&payout, 1113)?;
    assert_eq!(applied.effect, effect);
    assert_eq!(w.held, held - 43);
    assert_eq!(
        w.repeated(&payout, 1114)?,
        entitlement_id(allocation, high.worker)?.bytes()
    );
    assert_eq!(w.counters()?, [120, 101, 0, 19, 0, 0]);
    Ok(())
}

/// A14 dictionary: with all 256 recipient pairs pinned a reused pair still opens, but a new
/// frozen recipient refuses the opening before any liability; once the oldest epoch is paid
/// out and pruned its pairs are free and the same opening reserves.
fn dictionary_capacity() -> TestResult {
    let mut w = World::create(ORIGIN)?;
    let low = w.enroll(LOW, 130)?;
    for n in 2..=4 {
        w.evaluator(n, 129 + u64::from(n))?;
    }
    w.schedule(9, 134)?;
    let fund = w.fund_call(0, fund_payload(400, REFUND, FUNDING_POLICY_VERSION, true))?;
    w.apply(&fund, 135)?;
    w.pin_dictionary(low, 136)?;
    assert_eq!(w.counters()?, [400, 0, 0, 144, 0, 256]);
    let pinned = w.ledger()?;
    assert_eq!((pinned.recipient_count, pinned.retained_epochs), (256, 8));

    let reused = w.open(1280)?;
    assert_eq!(
        (reused.epoch, reused.budget, reused.workers),
        (9, BUDGET_CAP, 1)
    );
    {
        let rewards = reward_view(&w.bytes)?;
        let dictionary = rewards.dictionary();
        let slot = dictionary
            .find(low.worker, low.recipient)?
            .ok_or(NOT_FOUND)?;
        assert_eq!(dictionary.slot(slot)?.references, 2);
        assert_eq!(dictionary.occupied(), 256);
    }
    w.advance(1281)?;
    w.terminalize(&reused, &[(low, 1)], 1380)?;
    assert_eq!(w.counters()?, [400, 0, 0, 43, 0, 357]);

    let high = w.enroll(LOW + 1, 1381)?;
    assert_eq!(w.open_refused(1408)?, CAPACITY);
    assert_eq!(w.counters()?, [400, 0, 0, 43, 0, 357]);
    assert_eq!(
        reward_view(&w.bytes)?
            .dictionary()
            .find(high.worker, high.recipient)?,
        None
    );

    let oldest: Vec<RecipientSlot> = {
        let rewards = reward_view(&w.bytes)?;
        let dictionary = rewards.dictionary();
        rewards
            .row(1)?
            .entries()
            .iter()
            .map(|entry| dictionary.slot(entry.slot))
            .collect::<CodecResult<_>>()?
    };
    assert_eq!(oldest.len(), MAX_WORKERS);
    for (n, member) in (0u8..).zip(&oldest) {
        let claim = w.row_call(
            dispatch::CLAIM,
            1,
            n,
            claim_payload(member.worker, member.recipient, 1)?,
        )?;
        w.apply(&claim, 1410)?;
    }
    assert_eq!(w.counters()?, [400, 32, 0, 43, 0, 325]);
    let prune = w.row_call(dispatch::PRUNE_EPOCH, 1, 32, Vec::new())?;
    w.apply(&prune, 1411)?;
    let freed = oldest
        .iter()
        .filter(|member| member.worker != low.worker)
        .count();
    assert_eq!(usize::from(w.ledger()?.recipient_count), 256 - freed);

    let opened = w.open(1412)?;
    assert_eq!((opened.epoch, opened.budget, opened.workers), (10, 43, 2));
    assert_eq!(w.counters()?, [400, 32, 0, 0, 43, 325]);
    assert!(reward_view(&w.bytes)?
        .dictionary()
        .find(high.worker, high.recipient)?
        .is_some());
    Ok(())
}

/// A14 ring over a sustained funded lifecycle: thirty-two completed epochs fit, the oldest
/// completed row being pruned by the opening that would exceed them; a full ring whose
/// oldest row still owes an unexpired claim refuses the next opening without erasing it,
/// and once that claim is paid, or has expired, the opening prunes the oldest row first.
fn sustained_retention() -> TestResult {
    let start = |epoch: u64| ORIGIN + epoch * EPOCH_SPAN_HEIGHTS;
    let (mut w, low, high) = active_market()?;
    for epoch in 7..=38 {
        let s = start(epoch);
        if epoch > 7 {
            let fund = w.fund_call(
                0,
                fund_payload(BUDGET_CAP, REFUND, FUNDING_POLICY_VERSION, true),
            )?;
            w.apply(&fund, s)?;
        }
        let frozen = w.open(s + 1)?;
        assert_eq!(
            (frozen.epoch, frozen.budget, frozen.workers),
            (epoch, BUDGET_CAP, 2)
        );
        if epoch == 10 {
            for n in 5..=7 {
                w.evaluator(n, s + 3)?;
            }
        }
        if epoch == 20 {
            w.renew(s + 3)?;
        }
        w.terminalize(&frozen, &allocated(low, high), s + 100)?;
        let claim = w.claim_call(epoch, 1, &high, 43)?;
        w.apply(&claim, s + 101)?;
        if epoch > 7 {
            let claim = w.claim_call(epoch, 2, &low, 58)?;
            w.apply(&claim, s + 102)?;
        }
        let k = Amount::from(epoch - 6);
        assert_eq!(
            w.counters()?,
            [
                120 + BUDGET_CAP * (k - 1),
                43 * k + 58 * (k - 1),
                0,
                19,
                0,
                58
            ]
        );
    }
    assert_eq!(reward_view(&w.bytes)?.row(6), Err(NOT_FOUND));
    assert_eq!(w.ledger()?.retained_epochs, 32);

    let s = start(39);
    let fund = w.fund_call(
        0,
        fund_payload(BUDGET_CAP, REFUND, FUNDING_POLICY_VERSION, true),
    )?;
    w.apply(&fund, s)?;
    assert_eq!(w.open_refused(s + 1)?, RETENTION_FULL);
    assert_eq!(w.counters()?, [3352, 3174, 0, 120, 0, 58]);
    assert_eq!(
        entitlement_of(&w.bytes, 7, low.worker)?,
        (Disposition::Unclaimed, 58)
    );
    let mut lapsed = w.clone();

    let claim = w.claim_call(7, 2, &low, 58)?;
    w.apply(&claim, s + 2)?;
    let pruned = w.open(s + 3)?;
    assert_eq!(
        (pruned.epoch, pruned.previous, pruned.skipped, pruned.budget),
        (39, Some(38), 0, BUDGET_CAP)
    );
    assert_eq!(reward_view(&w.bytes)?.row(7), Err(NOT_FOUND));
    assert_eq!(w.ledger()?.retained_epochs, 31);
    assert_eq!(w.counters()?, [3352, 3232, 0, 19, 101, 0]);

    let late = start(40);
    assert_eq!(
        reward_view(&lapsed.bytes)?.row(7)?.expiry_height,
        start(7) + 100 + 4096
    );
    let expire = lapsed.row_call(dispatch::EXPIRE_EPOCH_CLAIMS, 7, 3, Vec::new())?;
    let applied = lapsed.apply(&expire, late)?;
    assert_eq!(applied.effect, RewardEffect::Released(58));
    let reopened = lapsed.open(late + 1)?;
    assert_eq!(
        (
            reopened.epoch,
            reopened.previous,
            reopened.skipped,
            reopened.budget
        ),
        (40, Some(38), 1, BUDGET_CAP)
    );
    assert_eq!(reward_view(&lapsed.bytes)?.row(7), Err(NOT_FOUND));
    assert_eq!(lapsed.counters()?, [3352, 3174, 0, 77, 101, 0]);
    Ok(())
}

/// A18 and A17 through the registered Program activity: a Fund whose response is lost is
/// reconciled from the committed state and replay record alone; its identical resubmission
/// returns the retained result with no second transfer; repeated deposits then reach the
/// real kernel blob admission boundary, which refuses with the state, the replay record and
/// the rewards account balance unchanged.
#[allow(clippy::too_many_lines)]
fn native_recovery_and_capacity() -> Checked {
    let mut n = Native::start()?;
    n.create(1000)?;
    let created = n
        .state
        .clone()
        .ok_or_else(|| harness("no committed state"))?;
    let envelope = n.envelope(
        dispatch::FUND,
        NATIVE_OWNER,
        2,
        &fund_payload(120, REFUND, FUNDING_POLICY_VERSION, true),
    )?;
    let (Outcome::Applied { result, .. }, expected) =
        n.local(NATIVE_OWNER, 1001, &created, &envelope)?
    else {
        return Err(harness("in-process Fund did not apply"));
    };
    let line = n.submit(NATIVE_OWNER, 1001, &envelope)?;
    assert_eq!(line.state, expected);
    assert_eq!(line.balance_after, line.balance_before - line.fee - 120);
    assert_eq!(reward_view(&line.state)?.ledger()?.tracked_deposits, 120);
    let retained = decode_shared_state(&line.state)?
        .control
        .replay
        .actor(ActorSlot::OWNER)
        .and_then(|actor| actor.last)
        .ok_or_else(|| harness("no retained owner result"))?;
    assert_eq!((retained.sequence, retained.result_digest), (2, result));
    let funded = line.rewards_balance;
    let mut committed = line.state;

    let Outcome::Retained(repeat) = n.local(NATIVE_OWNER, 1002, &committed, &envelope)?.0 else {
        return Err(harness(
            "in-process resubmission is not the retained result",
        ));
    };
    assert_eq!(repeat, retained);
    let mut frame = vec![0; MAX_RESULT_BYTES];
    let frame_len = codec::encode_result(
        &ApplicationResult::success(
            ResultStatus::AlreadyApplied,
            decode_envelope(&envelope)?.request_digest()?,
            repeat.applied_revision,
            &repeat.result_digest.bytes(),
        )?,
        &mut frame,
    )?;
    let line = n.submit(NATIVE_OWNER, 1002, &envelope)?;
    let Terminal::Success(body) = terminal(&line, n.program)? else {
        return Err(harness("native resubmission was rejected"));
    };
    assert_eq!(line.result_code, 0);
    assert_eq!(body, frame[..frame_len]);
    assert_eq!(line.state, committed);
    assert_eq!(line.rewards_balance, funded);
    assert_eq!(line.balance_after, line.balance_before - line.fee);
    assert!(line.events.is_empty());
    assert_eq!(
        (line.blobs_after, line.kv_after),
        (line.blobs_before, line.kv_before)
    );

    let [max_blobs, ..] = n.limits;
    let mut held = funded;
    let mut per_commit = None;
    for sequence in 3..=u64::try_from(max_blobs)? + 2 {
        let height = 1000 + sequence;
        let envelope = n.envelope(
            dispatch::FUND,
            NATIVE_OWNER,
            sequence,
            &fund_payload(1, REFUND, FUNDING_POLICY_VERSION, true),
        )?;
        let (Outcome::Applied { .. }, expected) =
            n.local(NATIVE_OWNER, height, &committed, &envelope)?
        else {
            return Err(harness("in-process deposit did not apply"));
        };
        let line = n.submit_raw(NATIVE_OWNER, height, &envelope)?;
        if line.status != 0 || line.result_code != 0 {
            let needed: usize =
                per_commit.ok_or_else(|| harness("the first native deposit was refused"))?;
            assert!(line.blobs_before + needed > max_blobs);
            assert_eq!(
                (line.blobs_after, line.kv_after),
                (line.blobs_before, line.kv_before)
            );
            assert_eq!(line.state, committed);
            assert_eq!(line.rewards_balance, held);
            assert_eq!(line.balance_before - line.balance_after, line.fee);
            assert!(line.events.is_empty());
            return n.finish();
        }
        assert_eq!(line.state, expected);
        assert!((1..=2).contains(&line.application_blobs));
        assert!(line.blobs_after > line.blobs_before);
        assert!(line.blobs_after <= max_blobs);
        assert!(line.kv_after <= line.kv_before + 1);
        assert_eq!(line.rewards_balance, held + 1);
        assert_eq!(line.balance_after, line.balance_before - line.fee - 1);
        assert_eq!(line.events.len(), 1);
        per_commit = Some(line.blobs_after - line.blobs_before);
        held = line.rewards_balance;
        committed = line.state;
    }
    Err(harness(format!(
        "no blob admission boundary within {max_blobs} blobs"
    )))
}

#[test]
fn reward_finality_recovery_and_capacity() -> Checked {
    late_settlement_and_close()?;
    finality_recovery()?;
    dictionary_capacity()?;
    sustained_retention()?;
    native_recovery_and_capacity()
}
