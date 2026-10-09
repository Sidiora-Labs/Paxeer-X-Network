//! AI.F07-T02 reputation lifecycle over the complete shared state value, whose joint F07/F08
//! section leads with the F07 region. Every market is produced by the real F01/F02/F03/F04/F06/
//! F08/F09 producers, each run through `epoch::carry_reputation`; `OPEN_EPOCH` rolls the
//! reputation segments over; `epoch::aggregate` runs `BeginAggregation`, `ProcessAggregation`
//! and `FinalizeAggregation`, freezing `history_allowed` at the F05 seal and applying the
//! completed epoch inside the F05/F06 terminal transition; `epoch::history` runs
//! `ResetHistory`, `SuspendHistory` and `ResumeHistory` with real envelopes. Every score is
//! admitted through real `CommitScore`/`RevealScore` calls. A clause that needs the
//! production-linked native host, or that no producer can reach, prints `NOT_RUN` with its
//! reason and is not claimed.
use std::collections::BTreeMap;

use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, ExitReason, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{self as agg, Outcome as Agg, Progress},
    aggregation_codec::AggregationPhase,
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        CommitScorePayload, Envelope, ReportBody, RevealScorePayload, ScoreVector,
        ValidatedEnvelope, REPORT_FIXED_BYTES, REPORT_MAX_BYTES,
    },
    commit_reveal::{self as cr, commitment, Outcome as F04},
    dispatch::{self, Operation},
    epoch::{
        self, Frozen, HistoryOutcome, Outcome as Opening, ADVANCE_SCRATCH_BYTES,
        AGGREGATE_SCRATCH_BYTES, CARRY_SCRATCH_BYTES, HISTORY_PAYLOAD_BYTES, HISTORY_SCRATCH_BYTES,
        OPEN_SCRATCH_BYTES, RESET_SUFFIX_BYTES, STATUS_SUFFIX_BYTES,
    },
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F03_SCORE_RANGE,
        F03_UNKNOWN_WORKER, F07_BINDING_MISMATCH, F07_GENERATION_MISMATCH,
        F07_IDEMPOTENCY_CONFLICT, F07_IDENTITY_FROZEN, F07_SEGMENT_MISMATCH, F07_UNKNOWN_WORKER,
        NON_CANONICAL, NOT_FOUND, RETENTION_FULL, ROLE_CONFLICT, UNAUTHORIZED, WRONG_CONFIG,
        WRONG_EPOCH,
    },
    evaluators::{
        admission::{self as f03, AdmissionReceipt},
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        model::{EvaluatorGrant, GrantTerms, SignedReport},
    },
    evidence::{self, Outcome as Sealing, SEAL_SCRATCH_BYTES},
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reputation::{
        CompletedHistory, CompletionIdentity, HistoryLookupError, HistoryStatus, Observation,
        ReputationCurrent, ReputationState, SegmentKey, COMMON_CAP, CURRENT_BYTES, HEADER_BYTES,
        HISTORY_BYTES, JOINT_CAP, LIMIT, SECTION_CAP,
    },
    reputation_codec::segment_digest,
    reputation_transition::{encode_joint, replace_record, split_joint, Region},
    rewards::{
        decode_reward_state, EpochStatus, FundReplay, FundRequest, FundingAuthority, FundingPhase,
        RewardEpoch, RewardLedger, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32, EvaluatorBinding,
        EvaluatorId, EvidenceRoot, FrozenBinding, MarketId, MetadataDigest, Presence, PrincipalId,
        ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest, RosterDigest, RubricDigest,
        Salt32, Signature64, Version, WorkerId,
    },
    workers::{
        self as f02, consent_digest, WorkerCurrent, WorkerState, WorkerTable,
        CONTROL_SCRATCH_BYTES, WORKER_TABLE_MAX_BYTES,
    },
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const RELAYER: u8 = 0x70;
const ORIGIN: u64 = 128;
const METADATA: [u8; 32] = [0x44; 32];
const SALT: [u8; 32] = [0x5a; 32];
/// The model artifact byte of the created market's policy.
const MODEL: u8 = 1;
/// Funding that covers every reserved epoch of the longest journey.
const FUND: u128 = 5000;
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120),
/// settlement from 1120.
const COMMIT_AT: u64 = 1088;
const SETTLE_AT: u64 = 1120;
/// The first enrolled worker; `LOW + 1` joins in epoch 6.
const LOW: u8 = 0x20;
/// A worker that enrolls into the seat a departed worker left.
const SUCCESSOR: u8 = 0x30;
const RESET_TOPIC: &[u8] = b"PAXAI/v1/ResetHistory";
const SUSPEND_TOPIC: &[u8] = b"PAXAI/v1/SuspendHistory";
const RESUME_TOPIC: &[u8] = b"PAXAI/v1/ResumeHistory";
/// `ResetHistory` reasons and the status reason.
const OWNER_RESET: u8 = 1;
const MODEL_CHANGED: u8 = 2;
const OWNER_REQUEST: u8 = 1;
/// Caller buffers able to hold every aggregation output.
const FULL: [usize; 3] = [MAX_STATE_BYTES, AGGREGATE_SCRATCH_BYTES, MAX_EVENT_BYTES];

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
/// The bounded default policy at `config` committing to model artifact `[model; 32]`.
fn policy(config: u64, model: u8) -> CodecResult<TaskPolicyV1> {
    TaskPolicyV1::bounded_default(
        config,
        1,
        PolicyCommitments {
            model_artifact: Digest32::new([model; 32])?,
            dataset_artifact: [2; 32],
            benchmark_suite: Digest32::new([3; 32])?,
            rubric: rubric()?,
            task_schema: Digest32::new([5; 32])?,
            result_schema: Digest32::new([6; 32])?,
            service_terms: Digest32::new([7; 32])?,
        },
        100,
        1,
    )
}
/// The ed25519 delegate of worker `n`.
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
/// The replacement delegate of worker `n`.
fn replacement_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n + 0x40; 32])
}
/// The frozen signing key of evaluator `n`.
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// The evidence root evaluator `n` seals.
const fn root(n: u8) -> u8 {
    0xe0 + n
}
/// A request id unique per kind, actor and sequence.
fn tag(kind: u8, n: u8, sequence: u64) -> [u8; 32] {
    let mut out = [kind; 32];
    out[0] = n;
    out[1..9].copy_from_slice(&sequence.to_be_bytes());
    out
}
fn market_id() -> CodecResult<MarketId> {
    derive_market(ChainDomain::new(CHAIN)?, ProgramId::new(PROGRAM)?)
}
/// The worker id of worker `n`.
fn id(n: u8) -> CodecResult<WorkerId> {
    derive_worker(market_id()?, principal(n)?, [n; 32])
}

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// One request envelope.
#[derive(Clone)]
struct Req {
    operation: Operation,
    chain: Option<ChainDomain>,
    program: Option<ProgramId>,
    market: Option<MarketId>,
    actor: PrincipalId,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    request: [u8; 32],
    expiry: u64,
    payload: Vec<u8>,
}
fn req(operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
    Req {
        operation,
        chain: None,
        program: None,
        market: None,
        actor,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        request: [1; 32],
        expiry: u64::MAX,
        payload,
    }
}
/// An envelope carrying the frozen binding `frozen`.
fn bound(
    operation: Operation,
    actor: PrincipalId,
    frozen: &FrozenBinding,
    payload: Vec<u8>,
) -> Req {
    Req {
        chain: Some(frozen.chain),
        program: Some(frozen.program),
        market: Some(frozen.market),
        epoch: frozen.epoch,
        config: frozen.config.get(),
        roster: Presence::Present(frozen.roster),
        ..req(operation, actor, payload)
    }
}
impl Req {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = match self.chain {
            Some(chain) => chain,
            None => ChainDomain::new(CHAIN)?,
        };
        let program = match self.program {
            Some(program) => program,
            None => ProgramId::new(PROGRAM)?,
        };
        let market = match self.market {
            Some(market) => market,
            None => derive_market(chain, program)?,
        };
        let envelope = Envelope {
            operation: self.operation,
            chain,
            program,
            market,
            actor: self.actor,
            epoch: self.epoch,
            config: self.config,
            roster: self.roster,
            sequence: self.sequence,
            expiry: self.expiry,
            request: RequestId::new(self.request)?,
            payload: &self.payload,
            authentication: Authentication::Native,
        };
        let mut out = vec![0; 32_768];
        let n = encode_envelope(&envelope, &mut out)?;
        out.truncate(n);
        Ok(out)
    }
    fn context(&self, at: u64) -> CodecResult<CallContext> {
        Ok(CallContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            principal: self.actor,
            height: at,
        })
    }
}

/// Committed state after the real F01 CREATE at `origin` (lifecycle REGISTERED).
fn create(origin: u64) -> CodecResult<Vec<u8>> {
    let program = ProgramId::new(PROGRAM)?;
    let mut policy_bytes = [0; TASK_POLICY_BYTES];
    policy(1, MODEL)?.encode(&mut policy_bytes)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&policy_bytes);
    payload.extend_from_slice(&[16; 32]);
    let call = Req {
        sequence: 1,
        ..req(dispatch::CREATE, PrincipalId::new(OWNER)?, payload)
    };
    let encoded = call.encode()?;
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Outcome::Applied { state, .. } = registry_ops::apply(
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

/// Runs one producer that reads the F08 table only through `epoch::carry_reputation` over
/// `current`; the joined next state is returned when the producer wrote one.
fn carried<T>(
    current: &[u8],
    produce: impl FnOnce(&[u8], &mut [u8]) -> CodecResult<(T, Option<usize>)>,
) -> CodecResult<(T, Option<Vec<u8>>)> {
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut carry = vec![0; CARRY_SCRATCH_BYTES];
    let (value, written) = epoch::carry_reputation(current, &mut next, &mut carry, produce)?;
    Ok((
        value,
        written.map(|len| {
            next.truncate(len);
            next
        }),
    ))
}

/// Owned, decoded sections of one committed shared state value; the joint section is kept as
/// its F07 region and F08 table.
struct Parts {
    revision: u64,
    policy: Vec<u8>,
    workers: WorkerTable,
    region: Vec<u8>,
    reports: Vec<u8>,
    rewards: Vec<u8>,
    admission: AdmissionTable,
    reputation: Option<Region>,
    replay: ReplayTable,
    features: Vec<u8>,
}
impl Parts {
    fn load(bytes: &[u8]) -> CodecResult<Self> {
        let state = decode_shared_state(bytes)?;
        let [policy, identity, reports, rewards, joint] = state.feature_sections;
        let (workers, region) = split_identity_section(identity)?;
        let (reputation, table) = split_joint(joint)?;
        Ok(Self {
            revision: state.revision,
            policy: policy.to_vec(),
            workers: WorkerTable::decode(workers)?,
            region: region.to_vec(),
            reports: reports.to_vec(),
            rewards: rewards.to_vec(),
            admission: AdmissionTable::decode(table)?,
            reputation,
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
        admission.truncate(admission_len);
        let joint = match &self.reputation {
            None => admission,
            Some(region) => {
                let mut out = vec![0; JOINT_CAP];
                let len = encode_joint(region, &admission, &mut out)?;
                out.truncate(len);
                out
            }
        };
        encode(&SharedState {
            revision: self.revision,
            feature_sections: [&policy, &identity, &self.reports, &self.rewards, &joint],
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

/// The committed state as a reader of the bare F08 table sees it: the F07 region is removed
/// and nothing else changes. The F05 progress view, F03 admitted reports and the F01 sealed
/// task-set view read the joint section as an F08 table.
fn plain(bytes: &[u8]) -> CodecResult<Vec<u8>> {
    let mut parts = Parts::load(bytes)?;
    parts.reputation = None;
    parts.encode()
}
/// The committed F07 region.
fn region_of(bytes: &[u8]) -> CodecResult<Region> {
    let state = decode_shared_state(bytes)?;
    split_joint(state.feature_sections[Section::ReputationAdmission.index()])?
        .0
        .ok_or(NOT_FOUND)
}
/// The committed F08 table bytes of the joint section.
fn table_of(bytes: &[u8]) -> CodecResult<Vec<u8>> {
    let state = decode_shared_state(bytes)?;
    Ok(
        split_joint(state.feature_sections[Section::ReputationAdmission.index()])?
            .1
            .to_vec(),
    )
}
fn section_of(bytes: &[u8], section: Section) -> CodecResult<Vec<u8>> {
    Ok(decode_shared_state(bytes)?.feature_sections[section.index()].to_vec())
}
/// The F01 section of `previous` with its header revision moved to `revision`.
fn rebound_policy(previous: &[u8], revision: u64) -> CodecResult<Vec<u8>> {
    let state = decode_shared_state(previous)?;
    let mut section =
        PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    Ok(policy)
}
/// Quality, qualifying count, coverage and last applied epoch of `record`.
fn standing(record: &ReputationCurrent) -> (u32, u32, Option<u32>, Option<u64>) {
    let coverage = match record.coverage {
        Presence::Present(value) => Some(value.get()),
        Presence::Absent => None,
    };
    let applied = match record.last_applied {
        Presence::Present(epoch) => Some(epoch),
        Presence::Absent => None,
    };
    (
        record.quality.get(),
        record.qualifying_count,
        coverage,
        applied,
    )
}
/// The retained completed summary of `epoch`.
fn completed(state: &ReputationState, epoch: u64) -> CodecResult<CompletedHistory> {
    state.lookup_epoch(epoch).map_err(|_| NOT_FOUND)
}
fn root_of(progress: &Progress) -> CodecResult<Digest32> {
    match progress.root {
        Presence::Present(root) => Ok(root),
        Presence::Absent => Err(NOT_FOUND),
    }
}
fn input_of(progress: &Progress) -> CodecResult<Digest32> {
    match progress.input {
        Presence::Present(input) => Ok(input),
        Presence::Absent => Err(NOT_FOUND),
    }
}
/// `floor((7q + s) / 8)` and `floor(63q / 64)` by plain integer arithmetic.
const fn qualified(q: u32, s: u32) -> u32 {
    (7 * q + s) / 8
}
const fn missing(q: u32) -> u32 {
    63 * q / 64
}

fn ctx(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height: at,
    }
}
/// Market-owner approval bound to the required effective epoch, expiring at its work end.
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
        expiry: at + 8192,
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

fn grant(
    market: &MarketHeader,
    owner: PrincipalId,
    nonce: u8,
    signing_key: PublicKey32,
    effective: u64,
) -> CodecResult<EvaluatorGrant> {
    EvaluatorGrant::nominate(
        market.market_id,
        owner,
        [nonce; 32],
        GrantTerms {
            rubric: rubric()?,
            grant_version: version()?,
            key_version: version()?,
            signing_key,
            effective_epoch: effective,
            expiry_epoch_exclusive: effective + 32,
        },
    )
}

/// One market's committed shared state bytes, its next owner sequence and the last applied
/// role sequence of every evaluator and worker owner (keyed by its principal byte).
#[derive(Clone)]
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
    sequences: BTreeMap<u8, u64>,
}
impl World {
    /// The real F01 CREATE. CREATE allocates no F07 region, so the empty region is placed
    /// before the F08 table at the next revision, before any other producer runs.
    fn create(origin: u64) -> CodecResult<Self> {
        let mut world = Self {
            bytes: create(origin)?,
            owner_sequence: 2,
            sequences: BTreeMap::new(),
        };
        world.edit(|parts, market| {
            parts.reputation = Some(Region {
                state: ReputationState::new(market.market_id),
                seal: Presence::Absent,
            });
            Ok(())
        })?;
        Ok(world)
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
    /// The next role sequence of principal `n`.
    fn next(&self, n: u8) -> u64 {
        self.sequences.get(&n).copied().unwrap_or(0) + 1
    }
    fn used(&mut self, n: u8, sequence: u64) {
        self.sequences.insert(n, sequence);
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
    /// A real owner-authorized F01 registry operation carried around the F07 region.
    fn owner_op(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let call = Req {
            config: self.section()?.header.active_config_version,
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(operation, PrincipalId::new(OWNER)?, payload)
        };
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let ctx = call.context(at)?;
        let ((), next) = carried(&self.bytes, |current, next| {
            let current = decode_shared_state(current)?;
            let mut section = vec![0; F01_SECTION_CAP];
            let mut event = vec![0; MAX_EVENT_BYTES];
            let Outcome::Applied { state, .. } =
                registry_ops::apply(&ctx, Some(&current), &envelope, &mut section, &mut event)?
            else {
                return Err(NON_CANONICAL);
            };
            let mut control = vec![0; Section::Control.payload_cap()];
            Ok(((), Some(encode_shared_state(&state, next, &mut control)?)))
        })?;
        self.bytes = next.ok_or(NON_CANONICAL)?;
        self.owner_sequence += 1;
        Ok(())
    }
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
    }
    /// Real F01 `STAGE_POLICY` of `staged` effective at `effective`.
    fn stage(&mut self, staged: &TaskPolicyV1, effective: u64, at: u64) -> TestResult {
        let mut bytes = [0; TASK_POLICY_BYTES];
        staged.encode(&mut bytes)?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&bytes);
        payload.extend_from_slice(&effective.to_be_bytes());
        self.owner_op(dispatch::STAGE_POLICY, payload, at)
    }
}

/// Worker, evaluator, membership and funding producers of the market journey.
impl World {
    /// F02 ENROLLED record with an ed25519 delegate, worker replay slot and F08 approval
    /// plus owner acceptance; returns the F02 seat.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<u8> {
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
            Ok(slot)
        })
    }
    /// F03 nomination accepted through the signed F08 evaluator consent of its delegate; the
    /// evaluator replay slot `n - 2` is bound to its owner.
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
            let grant = grant(market, owner, n, signing_key, effective)?;
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
    /// The F08 membership of worker `n`.
    fn membership(&self, n: u8) -> CodecResult<AdmissionMeta> {
        self.parts()?
            .admission
            .get(Participant::Worker(id(n)?))
            .ok_or(NOT_FOUND)
    }
    /// Owner-requested F08 retirement of worker `n`, effective at `expected` (the next
    /// opened snapshot); the rollover of that opening removes it from F08 and F02.
    fn exit(&mut self, n: u8, expected: u64, at: u64) -> TestResult {
        let meta = self.membership(n)?;
        self.edit(|parts, market| {
            parts.admission.request_exit(
                &ctx(market, principal(n)?, at),
                Participant::Worker(id(n)?),
                meta.membership_generation,
                expected,
                ExitReason::Retire,
            )?;
            Ok(())
        })
    }
    /// F08 heartbeat of worker `n` in the opened `epoch`.
    fn heartbeat(&mut self, n: u8, epoch: u64, at: u64) -> TestResult {
        let meta = self.membership(n)?;
        self.edit(|parts, market| {
            parts.admission.heartbeat(
                &ctx(market, principal(n)?, at),
                Participant::Worker(id(n)?),
                meta.membership_generation,
                meta.delegate_generation,
                epoch,
            )?;
            Ok(())
        })
    }
    /// Real owner FUND of `amount` into the F06 reward state.
    fn fund(&mut self, amount: u128, at: u64) -> TestResult {
        let mut parts = self.parts()?;
        let market = parts.market()?;
        if parts.rewards.is_empty() {
            let ledger = RewardLedger::new(
                market.funding_asset,
                market.rewards_account,
                market.refund_recipient_account,
            )?;
            let mut initial = vec![0; REWARD_STATE_BYTES];
            RewardState::init(&ledger, &mut initial)?;
            parts.rewards = initial;
        }
        let request = ReplayRequest {
            slot: ActorSlot::OWNER,
            principal: market.owner_principal,
            authority_version: version()?,
            sequence: self.owner_sequence,
            request_id: RequestId::new(tag(0x40, 0, self.owner_sequence))?,
            digest: RequestDigest::new(tag(0x41, 0, self.owner_sequence))?,
            expiry_height: at + 1000,
        };
        let mut funded = vec![0; REWARD_STATE_BYTES];
        decode_reward_state(&parts.rewards)?.fund(
            &FundingAuthority {
                owner: market.owner_principal,
                treasury: Presence::Absent,
            },
            FundingPhase::Accepting,
            &FundRequest {
                amount,
                refund_recipient: market.refund_recipient_account,
                policy_version: FUNDING_POLICY_VERSION,
                consent: true,
            },
            &mut FundReplay {
                table: &mut parts.replay,
                request: &request,
                height: at,
                revision: &mut parts.revision,
                result: ResultDigest::new(tag(0x42, 0, self.owner_sequence))?,
            },
            &mut funded,
        )?;
        parts.rewards = funded;
        self.bytes = parts.encode()?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// Restores workers `LOW + 1 .. LOW + workers` and evaluators 5..=9 beside the real first
    /// enrollments from the real admitted memberships of worker `LOW` and evaluator 2: the F08
    /// budget admits four enrollments per epoch and no producer admits a whole bounded roster
    /// at once.
    fn restore(&mut self, workers: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let worker = parts
                .admission
                .get(Participant::Worker(id(LOW)?))
                .ok_or(NOT_FOUND)?;
            let evaluator = parts
                .admission
                .get(Participant::Evaluator(derive_evaluator(
                    market.market_id,
                    principal(2)?,
                    [2; 32],
                )?))
                .ok_or(NOT_FOUND)?;
            for n in LOW + 1..LOW + workers {
                let owner = principal(n)?;
                let slot = parts.workers.free_slot()?;
                let record = worker_record(id(n)?, owner, public(&delegate_key(n)), slot, at)?;
                parts.workers.insert(&record)?;
                parts
                    .replay
                    .bind(ActorSlot::worker(usize::from(slot))?, owner, version()?)?;
                parts.admission.insert(AdmissionMeta {
                    participant: Participant::Worker(id(n)?),
                    owner,
                    ..worker
                })?;
            }
            for n in 5..=9 {
                let owner = principal(n)?;
                let grant = grant(market, owner, n, public(&evaluator_key(n)), 0)?;
                parts.admission.insert(AdmissionMeta {
                    participant: Participant::Evaluator(grant.evaluator),
                    owner,
                    ..evaluator
                })?;
                parts
                    .replay
                    .bind(ActorSlot::evaluator(usize::from(n - 2))?, owner, version()?)?;
                parts.insert_grant(grant, n)?;
            }
            Ok(())
        })
    }
}

/// The `OPEN_EPOCH`, `ADVANCE_ACTIVATION`, task-set and `SealEvidence` calls.
impl World {
    /// Permissionless object-local `OPEN_EPOCH` of the clock epoch of `at`; a refusal leaves
    /// the committed bytes unchanged.
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
    /// One F01 task-set call carried around the F07 region, committed only on `Applied`.
    fn task(&mut self, call: &Req, at: u64) -> TestResult {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let ctx = call.context(at)?;
        let ((), next) = carried(&self.bytes, |current, next| {
            let mut scratch = vec![0; tasks::SCRATCH_BYTES];
            let mut event = vec![0; MAX_EVENT_BYTES];
            let Task::Applied { state_len, .. } =
                tasks::apply(&ctx, &envelope, current, next, &mut scratch, &mut event)?
            else {
                return Err(NON_CANONICAL);
            };
            Ok(((), Some(state_len)))
        })?;
        self.bytes = next.ok_or(NON_CANONICAL)?;
        Ok(())
    }
    /// One real `SealEvidence` carried around the F07 region, committed only on `Applied`.
    fn evidence(&mut self, call: &Req, at: u64) -> TestResult {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let ctx = call.context(at)?;
        let ((), next) = carried(&self.bytes, |current, next| {
            let mut scratch = vec![0; SEAL_SCRATCH_BYTES];
            let mut event = vec![0; MAX_EVENT_BYTES];
            let Sealing::Applied { state_len, .. } =
                evidence::apply(&ctx, &envelope, current, next, &mut scratch, &mut event)?
            else {
                return Err(NON_CANONICAL);
            };
            Ok(((), Some(state_len)))
        })?;
        self.bytes = next.ok_or(NON_CANONICAL)?;
        Ok(())
    }
}

/// A market with an opened epoch.
#[derive(Clone)]
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
}
/// Epoch 7 of the journey (T = 1024): worker `LOW` and evaluators 2..=4 enrolled before
/// activation, epoch 6 opened at 896, worker `LOW + 1` and evaluators 5..=7 enrolled in epoch
/// 6, epoch 6 scored by `six` (each scoring `LOW` 800000) and settled through F05 at 992,
/// epoch 7 opened at 1024 and the evidence of evaluators 2..=7 sealed at 1088.
fn journey(six: &[u8]) -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    world.schedule(6, 134)?;
    world.fund(FUND, 135)?;
    let frozen = world.open(896)?;
    assert_eq!((frozen.epoch, frozen.workers, frozen.evaluators), (6, 1, 3));
    world.advance(897)?;
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen,
        header,
    };
    m.world.enroll(LOW + 1, 900)?;
    for n in 5..=7 {
        m.world.evaluator(n, 896 + u64::from(n))?;
    }
    let plan = plan(six, &[(LOW, 800_000)])?;
    m.seal(six, 960)?;
    m.score(&plan)?;
    let opened = m.open_epoch(7)?;
    assert_eq!((opened.epoch, opened.workers, opened.evaluators), (7, 2, 6));
    m.seal(&[2, 3, 4, 5, 6, 7], COMMIT_AT)?;
    Ok(m)
}
/// The journey with an unscored epoch 6.
fn market() -> CodecResult<Market> {
    journey(&[])
}
/// Epoch 6 (T = 896) of a market with `workers` frozen workers and evaluators 2..=9 frozen,
/// the empty task set sealed and every evidence root sealed at 960.
fn crowded(workers: u8) -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    world.restore(workers, 134)?;
    world.schedule(6, 135)?;
    world.fund(FUND, 136)?;
    let frozen = world.open(896)?;
    assert_eq!(
        (frozen.epoch, frozen.workers, frozen.evaluators),
        (6, workers, 8)
    );
    world.advance(897)?;
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen,
        header,
    };
    m.seal(&[2, 3, 4, 5, 6, 7, 8, 9], 960)?;
    Ok(m)
}

/// Canonical 36-byte score entries in the given order.
fn entries(pairs: &[(WorkerId, u32)]) -> Vec<u8> {
    let mut out = Vec::new();
    for (worker, score) in pairs {
        out.extend_from_slice(worker.as_bytes());
        out.extend_from_slice(&score.to_be_bytes());
    }
    out
}
/// Canonical entries scoring workers `n` in ascending worker id order.
fn ballot(pairs: &[(u8, u32)]) -> CodecResult<Vec<u8>> {
    let mut scored = pairs
        .iter()
        .map(|&(n, score)| Ok((id(n)?, score)))
        .collect::<CodecResult<Vec<_>>>()?;
    scored.sort_by_key(|&(worker, _)| worker);
    Ok(entries(&scored))
}
/// Every evaluator of `voters` submits the same ballot of `pairs`.
fn plan(voters: &[u8], pairs: &[(u8, u32)]) -> CodecResult<Vec<(u8, Vec<u8>)>> {
    let scores = ballot(pairs)?;
    Ok(voters.iter().map(|&n| (n, scores.clone())).collect())
}
/// The evaluator signature over the attestation digest of `body`.
fn sign<'a>(body: ReportBody<'a>, key: &SigningKey) -> CodecResult<SignedReport<'a>> {
    let digest = codec::attestation_digest(codec::report_digest(&body)?)?;
    Ok(SignedReport {
        body,
        signature: Signature64(key.sign(&digest.bytes()).to_bytes()),
    })
}
/// `C = H(commit domain, B || report digest || salt)`.
fn commitment_of(body: &ReportBody<'_>, salt: [u8; 32]) -> CodecResult<CommitmentDigest> {
    codec::commitment_digest(
        &body.binding,
        codec::report_digest(body)?,
        Salt32::new(salt)?,
    )
}
/// The 224-byte `CommitScore` payload.
fn commit_payload(
    binding: &EvaluatorBinding,
    commitment: CommitmentDigest,
) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; commitment::COMMIT_SCORE_BYTES];
    codec::encode_commit_score(
        &CommitScorePayload {
            binding: *binding,
            commitment,
        },
        &mut out,
    )?;
    Ok(out)
}
/// The canonical `RevealScore` payload.
fn reveal_payload(report: &SignedReport<'_>, salt: [u8; 32]) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; commitment::REVEAL_MAX_BYTES];
    let len = codec::encode_reveal_score(
        &RevealScorePayload {
            report: report.body,
            signature: report.signature,
            salt: Salt32::new(salt)?,
        },
        &mut out,
    )?;
    out.truncate(len);
    Ok(out)
}
/// The `len:u32 || body || signature || salt` reveal payload over raw parts.
fn raw_reveal(body: &[u8], signature: &[u8], salt: &[u8]) -> CodecResult<Vec<u8>> {
    let mut out = u32::try_from(body.len())
        .map_err(|_| ARITHMETIC)?
        .to_be_bytes()
        .to_vec();
    out.extend_from_slice(body);
    out.extend_from_slice(signature);
    out.extend_from_slice(salt);
    Ok(out)
}
/// Raw report bytes: the canonical fixed part of `body` with a raw `count` and raw entries.
fn raw_body(body: &ReportBody<'_>, count: u16, scores: &[u8]) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; REPORT_MAX_BYTES];
    codec::encode_report(body, &mut out)?;
    out.truncate(REPORT_FIXED_BYTES - 2);
    out.extend_from_slice(&count.to_be_bytes());
    out.extend_from_slice(scores);
    Ok(out)
}

/// Identities, bindings, reports and the task-set and evidence seals of the opened epoch.
impl Market {
    /// T of the opened epoch.
    fn start(&self) -> u64 {
        ORIGIN + 128 * self.frozen.epoch
    }
    /// Opens epoch `epoch` at its first height and refreshes the frozen binding and header.
    fn open_epoch(&mut self, epoch: u64) -> CodecResult<Frozen> {
        self.frozen = self.world.open(ORIGIN + 128 * epoch)?;
        self.header = self.world.parts()?.market()?;
        Ok(self.frozen)
    }
    fn frozen_binding(&self) -> FrozenBinding {
        FrozenBinding {
            chain: self.header.deployment_chain_domain,
            program: self.header.program_id,
            market: self.header.market_id,
            epoch: self.frozen.epoch,
            config: self.frozen.config,
            roster: self.frozen.roster,
        }
    }
    fn evaluator(&self, n: u8) -> CodecResult<EvaluatorId> {
        derive_evaluator(self.header.market_id, principal(n)?, [n; 32])
    }
    /// The frozen binding B of evaluator `n` at grant and key version 1.
    fn binding(&self, n: u8) -> CodecResult<EvaluatorBinding> {
        Ok(EvaluatorBinding {
            frozen: self.frozen_binding(),
            evaluator: self.evaluator(n)?,
            grant: version()?,
            key_version: version()?,
        })
    }
    /// Evaluator `n`'s report under `binding` with its sealed evidence root.
    fn body(binding: EvaluatorBinding, n: u8, scores: &[u8]) -> CodecResult<ReportBody<'_>> {
        Ok(ReportBody {
            binding,
            evidence: EvidenceRoot::new([root(n); 32])?,
            scores: ScoreVector::Encoded(scores),
        })
    }
    /// The F03 admitted report of evaluator `n` in the opened epoch.
    fn admitted(&self, n: u8) -> CodecResult<Option<AdmissionReceipt>> {
        let bytes = plain(&self.world.bytes)?;
        let state = decode_shared_state(&bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?.map(|r| r.receipt))
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's task region, then `SealEvidence` of each
    /// of `evaluators` under its next role sequence.
    fn seal(&mut self, evaluators: &[u8], at: u64) -> TestResult {
        let frozen = self.frozen_binding();
        let set = SetBinding {
            market: frozen.market,
            epoch: frozen.epoch,
            config: frozen.config,
            policy: self.frozen.policy,
            roster: frozen.roster,
        };
        let region = self.world.section()?.task_region.to_vec();
        let digest = tasks::task_set_digest(&set, &TaskSet::decode(&region)?)?;
        let mut payload = frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&frozen.config.get().to_be_bytes());
        payload.extend_from_slice(digest.as_bytes());
        let call = bound(
            dispatch::SEAL_TASK_SET,
            principal(KEEPER)?,
            &frozen,
            payload,
        );
        self.world.task(&call, at)?;
        let bytes = plain(&self.world.bytes)?;
        let sealed = tasks::sealed_task_set(&decode_shared_state(&bytes)?, frozen.epoch)?;
        for &n in evaluators {
            let mut claim = 1u16.to_be_bytes().to_vec();
            claim.extend_from_slice(&[root(n); 32]);
            claim.extend_from_slice(self.frozen.policy.as_bytes());
            claim.extend_from_slice(sealed.as_bytes());
            claim.extend_from_slice(rubric()?.as_bytes());
            claim.push(1);
            let sequence = self.world.next(n);
            let call = Req {
                sequence,
                request: tag(0xc5, n, sequence),
                ..bound(dispatch::SealEvidence, principal(n)?, &frozen, claim)
            };
            self.world.evidence(&call, at)?;
            self.world.used(n, sequence);
        }
        Ok(())
    }
}

/// `CommitScore` and `RevealScore` requests, the F04 call and the F02/F03 identity producers.
impl Market {
    /// Evaluator `n`'s native `CommitScore` envelope over `binding` and `commitment`.
    fn commit_call(
        n: u8,
        binding: &EvaluatorBinding,
        commitment: CommitmentDigest,
        sequence: u64,
        expiry: u64,
    ) -> CodecResult<Req> {
        Ok(Req {
            sequence,
            request: tag(0xa1, n, sequence),
            expiry,
            ..bound(
                dispatch::CommitScore,
                principal(n)?,
                &binding.frozen,
                commit_payload(binding, commitment)?,
            )
        })
    }
    /// Evaluator `n`'s native `RevealScore` envelope carrying `report` and `salt`.
    fn reveal_call(
        n: u8,
        report: &SignedReport<'_>,
        salt: [u8; 32],
        sequence: u64,
        expiry: u64,
    ) -> CodecResult<Req> {
        Ok(Req {
            sequence,
            request: tag(0xa2, n, sequence),
            expiry,
            ..bound(
                dispatch::RevealScore,
                principal(n)?,
                &report.body.binding.frozen,
                reveal_payload(report, salt)?,
            )
        })
    }
    /// One F04 call carried around the F07 region, committed only on `Committed` or
    /// `Revealed`; a retained repetition writes no event.
    fn call(&mut self, call: &Req, at: u64) -> CodecResult<F04> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let ctx = call.context(at)?;
        let mut event = vec![0; MAX_EVENT_BYTES];
        let (outcome, next) = carried(&self.world.bytes, |current, next| {
            let mut scratch = vec![0; cr::SCRATCH_BYTES];
            let outcome = cr::apply(&ctx, &envelope, current, next, &mut scratch, &mut event)?;
            let written = match outcome {
                F04::Committed { state_len, .. } | F04::Revealed { state_len, .. } => {
                    Some(state_len)
                }
                F04::Retained(_) => None,
            };
            Ok((outcome, written))
        })?;
        match next {
            Some(next) => self.world.bytes = next,
            None => assert!(event.iter().all(|b| *b == 0)),
        }
        Ok(outcome)
    }
    /// A refused F04 call leaves the committed bytes unchanged.
    fn refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.call(call, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    /// Evaluator `n` commits `C` of `report` and `SALT` under its next role sequence.
    fn commit(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> TestResult {
        let c = commitment_of(&report.body, SALT)?;
        let sequence = self.world.next(n);
        let call = Market::commit_call(n, &report.body.binding, c, sequence, self.start() + 80)?;
        let F04::Committed { record, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!((record.commitment, record.height), (c, at));
        self.world.used(n, sequence);
        Ok(())
    }
    /// Evaluator `n`'s `RevealScore` of `report` and `SALT` under its next role sequence.
    fn reveal_request(&self, n: u8, report: &SignedReport<'_>) -> CodecResult<Req> {
        Market::reveal_call(n, report, SALT, self.world.next(n), self.start() + 96)
    }
    /// Evaluator `n` reveals `report`; the F03 admitted report is its receipt.
    fn reveal(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> CodecResult<Req> {
        let call = self.reveal_request(n, report)?;
        let F04::Revealed { receipt, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.admitted(n)?, Some(receipt));
        self.world.used(n, call.sequence);
        Ok(call)
    }
    /// Evaluator `n`'s signed report over `scores` under its frozen binding.
    fn signed<'a>(&self, n: u8, scores: &'a [u8]) -> CodecResult<SignedReport<'a>> {
        sign(
            Market::body(self.binding(n)?, n, scores)?,
            &evaluator_key(n),
        )
    }
    /// Every evaluator of `plan` commits at T+64; they reveal in plan order from T+80, one
    /// height apart. Returns the reveal requests.
    fn admit(&mut self, plan: &[(u8, Vec<u8>)]) -> CodecResult<Vec<Req>> {
        let start = self.start();
        for (n, scores) in plan {
            let report = self.signed(*n, scores)?;
            self.commit(*n, &report, start + 64)?;
        }
        let mut reveals = Vec::new();
        for (height, (n, scores)) in (start + 80..).zip(plan) {
            let report = self.signed(*n, scores)?;
            reveals.push(self.reveal(*n, &report, height)?);
        }
        Ok(reveals)
    }
    /// Admits `plan` and settles the opened epoch at T+96.
    fn score(&mut self, plan: &[(u8, Vec<u8>)]) -> CodecResult<Progress> {
        self.admit(plan)?;
        self.settle(self.start() + 96)
    }
    /// Seals the task set and the evidence of every evaluator of `plan` at T+64, admits
    /// `plan` and settles at T+96.
    fn run(&mut self, plan: &[(u8, Vec<u8>)]) -> CodecResult<Progress> {
        let voters = plan.iter().map(|(n, _)| *n).collect::<Vec<_>>();
        self.seal(&voters, self.start() + 64)?;
        self.score(plan)
    }
    /// Opens `epoch` and runs `plan` in it.
    fn epoch(&mut self, epoch: u64, plan: &[(u8, Vec<u8>)]) -> CodecResult<Progress> {
        self.open_epoch(epoch)?;
        self.run(plan)
    }
    /// One real owner F03 authority operation bound to the opened epoch, carried around the
    /// F07 region. F03 advances only the shared revision, so the F01 header revision is
    /// re-bound to it.
    fn owner_authority(
        &mut self,
        operation: Operation,
        payload: Vec<u8>,
        aggregate_sealed: bool,
        at: u64,
    ) -> TestResult {
        let sequence = self.world.owner_sequence;
        let call = Req {
            sequence,
            request: tag(0x30, 0, sequence),
            ..bound(
                operation,
                PrincipalId::new(OWNER)?,
                &self.frozen_binding(),
                payload,
            )
        };
        let encoded = call.encode()?;
        let market = self.world.parts()?.market()?;
        let ((), next) = carried(&self.world.bytes, |current, next| {
            let mut scratch = vec![0; authority::SCRATCH_BYTES];
            let mut event = vec![0; MAX_EVENT_BYTES];
            let authority::Outcome::Applied { state_len, .. } = authority::apply(
                current,
                &AuthorityContext {
                    market: &market,
                    invoking_principal: PrincipalId::new(OWNER)?,
                    immediate_caller: Presence::Absent,
                    height: at,
                    approved_rubric: rubric()?,
                    aggregate_sealed,
                },
                &encoded,
                &mut scratch,
                next,
                &mut event,
            )?
            else {
                return Err(NON_CANONICAL);
            };
            Ok(((), Some(state_len)))
        })?;
        self.world.bytes = Parts::load(&next.ok_or(NON_CANONICAL)?)?.encode()?;
        self.world.owner_sequence += 1;
        Ok(())
    }
    /// Real F03 `RevokeEvaluator` of evaluator `n`; the host derives `aggregate_sealed` from
    /// the committed F05 progress.
    fn revoke(&mut self, n: u8, at: u64) -> TestResult {
        let sealed = self.progress()?.phase != AggregationPhase::Unsealed;
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.extend_from_slice(&1u16.to_be_bytes());
        payload.extend_from_slice(&[0x5e; 32]);
        self.owner_authority(dispatch::RevokeEvaluator, payload, sealed, at)
    }
    /// Real F02 `RotateDelegate` of worker `n` by its owner, carried around the F07 region:
    /// a replacement generation. F02 advances only the shared revision, so the F01 header
    /// revision is re-bound to it.
    fn replace_worker(&mut self, n: u8, at: u64) -> TestResult {
        let market = self.world.parts()?.market()?;
        let owner = principal(n)?;
        let worker = id(n)?;
        let record = self.world.parts()?.workers.get(worker).ok_or(NOT_FOUND)?;
        let key = replacement_key(n);
        let metadata = MetadataDigest::new([0x45; 32])?;
        let expiry = at + 4096;
        let consent = consent_digest(
            &market,
            worker,
            owner,
            public(&key),
            record.generation + 1,
            record.key_version + 1,
            metadata,
            expiry,
        )?;
        let mut payload = worker.as_bytes().to_vec();
        payload.extend_from_slice(&record.generation.to_be_bytes());
        payload.extend_from_slice(&record.key_version.to_be_bytes());
        payload.extend_from_slice(&public(&key).0);
        payload.extend_from_slice(metadata.as_bytes());
        payload.extend_from_slice(&expiry.to_be_bytes());
        payload.extend_from_slice(&key.sign(consent.as_bytes()).to_bytes());
        let sequence = self.world.next(n);
        let call = Req {
            sequence,
            request: tag(0x25, n, sequence),
            ..req(dispatch::RotateDelegate, owner, payload)
        };
        let encoded = call.encode()?;
        let ((), next) = carried(&self.world.bytes, |current, next| {
            let mut event = vec![0; MAX_EVENT_BYTES];
            let mut scratch = vec![0; CONTROL_SCRATCH_BYTES];
            let f02::Applied::Applied { state_len, .. } = f02::apply(
                current,
                &f02::CallContext {
                    market: &market,
                    invoking_principal: owner,
                    height: at,
                },
                &encoded,
                next,
                &mut event,
                &mut scratch,
            )?
            else {
                return Err(NON_CANONICAL);
            };
            Ok(((), Some(state_len)))
        })?;
        self.world.bytes = Parts::load(&next.ok_or(NON_CANONICAL)?)?.encode()?;
        self.world.used(n, sequence);
        Ok(())
    }
}

fn aggregation_topic(operation: Operation) -> CodecResult<&'static [u8]> {
    if operation == dispatch::BeginAggregation {
        Ok(b"PAXAI/v1/BeginAggregation")
    } else if operation == dispatch::ProcessAggregation {
        Ok(b"PAXAI/v1/ProcessAggregation")
    } else if operation == dispatch::FinalizeAggregation {
        Ok(b"PAXAI/v1/FinalizeAggregation")
    } else {
        Err(NON_CANONICAL)
    }
}
fn history_topic(operation: Operation) -> CodecResult<(&'static [u8], usize)> {
    if operation == dispatch::ResetHistory {
        Ok((RESET_TOPIC, RESET_SUFFIX_BYTES))
    } else if operation == dispatch::SuspendHistory {
        Ok((SUSPEND_TOPIC, STATUS_SUFFIX_BYTES))
    } else if operation == dispatch::ResumeHistory {
        Ok((RESUME_TOPIC, STATUS_SUFFIX_BYTES))
    } else {
        Err(NON_CANONICAL)
    }
}

/// One aggregation revision: only the F01 header revision, the F05/F06 settlement section and
/// the F07 region change; identity, reports, the F08 table and control feature bytes are
/// byte-identical. Begin seals `history_allowed` for the epoch, Process carries the region and
/// Finalize applies the completed epoch bound to the aggregate root and clears the seal. The
/// operation event carries the response as its suffix.
fn check_applied(
    previous: &[u8],
    current: &[u8],
    envelope: &ValidatedEnvelope<'_>,
    outcome: &Agg,
    event: &[u8],
) -> TestResult {
    let Agg::Applied {
        progress,
        response,
        revision,
        result,
        ..
    } = *outcome
    else {
        return Err(NON_CANONICAL);
    };
    let before = decode_shared_state(previous)?;
    let after = decode_shared_state(current)?;
    assert_eq!(
        (after.revision, revision),
        (before.revision + 1, before.revision + 1)
    );
    assert_eq!(
        after.feature_sections[0],
        rebound_policy(previous, revision)?.as_slice()
    );
    assert_eq!(after.feature_sections[1..3], before.feature_sections[1..3]);
    assert_eq!(table_of(current)?, table_of(previous)?);
    assert_eq!(after.control.feature_bytes, before.control.feature_bytes);
    assert_eq!(agg::progress(&plain(current)?)?, progress);
    let e = &envelope.envelope;
    let (operation, common, suffix) =
        codec::decode_event_frame(aggregation_topic(e.operation)?, event)?;
    assert_eq!(operation, e.operation);
    assert_eq!(
        (
            common.market,
            common.epoch,
            common.config.get(),
            common.revision
        ),
        (e.market, e.epoch, e.config, revision)
    );
    assert_eq!(
        (common.request, common.result),
        (envelope.request_digest()?, result)
    );
    assert_eq!(suffix, response.as_bytes());
    assert_eq!(result, codec::result_digest(suffix)?);
    let (old, new) = (region_of(previous)?, region_of(current)?);
    if e.operation == dispatch::BeginAggregation {
        assert_eq!(new.state, old.state);
        assert_eq!(old.seal, Presence::Absent);
        let Presence::Present(seal) = new.seal else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(seal.epoch, progress.epoch);
    } else if e.operation == dispatch::ProcessAggregation {
        assert_eq!(new, old);
    } else {
        assert_eq!(new.seal, Presence::Absent);
        assert_eq!(
            new.state.latest_completed(),
            Presence::Present(progress.epoch)
        );
        assert_eq!(
            completed(&new.state, progress.epoch)?.result,
            root_of(&progress)?
        );
    }
    Ok(())
}

/// F05 aggregation requests through `epoch::aggregate` and the committed F05/F06/F07 values.
impl Market {
    /// A permissionless relayer request for `operation` bound to the opened epoch.
    fn aggregation_call(
        &self,
        operation: Operation,
        payload: Vec<u8>,
        cursor: u16,
    ) -> CodecResult<Req> {
        let [_, selector] = operation.selector().to_be_bytes();
        Ok(Req {
            request: tag(0xf5, selector, u64::from(cursor)),
            ..bound(
                operation,
                principal(RELAYER)?,
                &self.frozen_binding(),
                payload,
            )
        })
    }
    fn begin_call(&self) -> CodecResult<Req> {
        self.aggregation_call(dispatch::BeginAggregation, Vec::new(), 0)
    }
    fn process_call(&self, input: Digest32, cursor: u16) -> CodecResult<Req> {
        let mut payload = input.as_bytes().to_vec();
        payload.extend_from_slice(&cursor.to_be_bytes());
        self.aggregation_call(dispatch::ProcessAggregation, payload, cursor)
    }
    fn finalize_call(&self, input: Digest32) -> CodecResult<Req> {
        self.aggregation_call(
            dispatch::FinalizeAggregation,
            input.as_bytes().to_vec(),
            u16::MAX,
        )
    }
    /// One `epoch::aggregate` call over the committed bytes into caller buffers of `sizes`
    /// (next, scratch, event); nothing is committed.
    fn compose(
        &self,
        call: &Req,
        at: u64,
        sizes: [usize; 3],
    ) -> CodecResult<(CodecResult<Agg>, Vec<u8>, Vec<u8>)> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let [next_len, scratch_len, event_len] = sizes;
        let mut next = vec![0; next_len];
        let mut scratch = vec![0; scratch_len];
        let mut event = vec![0; event_len];
        let outcome = epoch::aggregate(
            &call.context(at)?,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        );
        Ok((outcome, next, event))
    }
    /// One aggregation call; `Applied` is committed after the complete next state and its
    /// event are checked, anything else writes no event and leaves the committed bytes.
    fn aggregate(&mut self, call: &Req, at: u64) -> CodecResult<Agg> {
        let (outcome, next, event) = self.compose(call, at, FULL)?;
        match outcome {
            Ok(
                applied @ Agg::Applied {
                    state_len,
                    event_len,
                    ..
                },
            ) => {
                let encoded = call.encode()?;
                check_applied(
                    &self.world.bytes,
                    &next[..state_len],
                    &decode_envelope(&encoded)?,
                    &applied,
                    &event[..event_len],
                )?;
                self.world.bytes = next[..state_len].to_vec();
            }
            Ok(Agg::AlreadyApplied { progress, .. }) => {
                assert_eq!(progress, self.progress()?);
                assert!(event.iter().all(|b| *b == 0));
            }
            Err(code) => assert!(event.iter().all(|b| *b == 0), "{code:?}"),
        }
        outcome
    }
    /// A refused aggregation call leaves the committed bytes unchanged.
    fn aggregation_refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.aggregate(call, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    /// The committed F05 progress, read from the F08 table the F05 view decodes.
    fn progress(&self) -> CodecResult<Progress> {
        agg::progress(&plain(&self.world.bytes)?)
    }
    fn applied(outcome: Agg) -> CodecResult<Progress> {
        let Agg::Applied { progress, .. } = outcome else {
            return Err(NON_CANONICAL);
        };
        Ok(progress)
    }
    fn begin(&mut self, at: u64) -> CodecResult<Progress> {
        let call = self.begin_call()?;
        let progress = Market::applied(self.aggregate(&call, at)?)?;
        assert_eq!(progress.phase, AggregationPhase::Processing);
        Ok(progress)
    }
    fn process(&mut self, at: u64) -> CodecResult<Progress> {
        let current = self.progress()?;
        let call = self.process_call(input_of(&current)?, current.cursor)?;
        Market::applied(self.aggregate(&call, at)?)
    }
    fn finalize(&mut self, at: u64) -> CodecResult<Progress> {
        let input = input_of(&self.progress()?)?;
        let call = self.finalize_call(input)?;
        let progress = Market::applied(self.aggregate(&call, at)?)?;
        assert_eq!(progress.phase, AggregationPhase::Terminal);
        assert_eq!(
            self.reward_row(self.frozen.epoch)?.status,
            EpochStatus::Terminal
        );
        Ok(progress)
    }
    /// Every remaining Process chunk, then Finalize.
    fn complete(&mut self, at: u64) -> CodecResult<Progress> {
        let mut progress = self.progress()?;
        while progress.cursor < progress.worker_count {
            progress = self.process(at)?;
        }
        self.finalize(at)
    }
    /// Begin, every Process chunk and Finalize at `at`.
    fn settle(&mut self, at: u64) -> CodecResult<Progress> {
        self.begin(at)?;
        self.complete(at)
    }
    fn reward_row(&self, epoch: u64) -> CodecResult<RewardEpoch> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?.row(epoch)
    }
    fn region(&self) -> CodecResult<Region> {
        region_of(&self.world.bytes)
    }
    /// The F07 record of worker `n`.
    fn record(&self, n: u8) -> CodecResult<ReputationCurrent> {
        let worker = id(n)?;
        self.region()?
            .state
            .records()
            .find(|r| r.worker == worker)
            .copied()
            .ok_or(NOT_FOUND)
    }
    /// The segment key of worker `n` at reset `generation` under the active binding.
    fn key(&self, n: u8, generation: u64) -> CodecResult<SegmentKey> {
        let section = self.world.section()?;
        Ok(SegmentKey {
            market: self.header.market_id,
            worker: id(n)?,
            config: Version::new(section.header.active_config_version)?,
            policy: Digest32::new(section.current.digest()?.bytes())?,
            model: section.current.commitments.model_artifact,
            reset_generation: Version::new(generation)?,
        })
    }
    /// Index of worker `n` in the ascending frozen roster of `workers`.
    fn index(n: u8, workers: &[u8]) -> CodecResult<usize> {
        let mut ids = workers
            .iter()
            .map(|&w| id(w))
            .collect::<CodecResult<Vec<_>>>()?;
        ids.sort();
        let target = id(n)?;
        ids.iter().position(|w| *w == target).ok_or(NOT_FOUND)
    }
}

/// Who signs a history request: the market owner or the owner of worker `n`.
#[derive(Clone, Copy)]
enum Actor {
    Market,
    Worker(u8),
}
/// `worker_id32 || expected_segment_digest32 || expected_reset_generation:u64 || reason:u8`.
fn history_payload(worker: WorkerId, segment: Digest32, generation: u64, reason: u8) -> Vec<u8> {
    let mut payload = worker.as_bytes().to_vec();
    payload.extend_from_slice(segment.as_bytes());
    payload.extend_from_slice(&generation.to_be_bytes());
    payload.push(reason);
    payload
}

/// `ResetHistory`, `SuspendHistory` and `ResumeHistory` through `epoch::history`.
impl Market {
    /// A history request by `actor` under its next role sequence, bound to the opened epoch.
    fn history_call(
        &self,
        operation: Operation,
        actor: Actor,
        payload: Vec<u8>,
    ) -> CodecResult<Req> {
        let (who, byte, sequence) = match actor {
            Actor::Market => (PrincipalId::new(OWNER)?, 0, self.world.owner_sequence),
            Actor::Worker(n) => (principal(n)?, n, self.world.next(n)),
        };
        Ok(Req {
            sequence,
            request: tag(0x71, byte, sequence),
            ..bound(operation, who, &self.frozen_binding(), payload)
        })
    }
    /// The request of `actor` naming worker `n`'s committed segment and generation.
    fn current_request(
        &self,
        operation: Operation,
        actor: Actor,
        n: u8,
        reason: u8,
    ) -> CodecResult<Req> {
        let record = self.record(n)?;
        let payload = history_payload(
            record.worker,
            record.segment,
            record.reset_generation.get(),
            reason,
        );
        self.history_call(operation, actor, payload)
    }
    /// One history call. `Applied` is committed after checking that only the F01 header
    /// revision, the worker's F07 record and the actor's retained replay result changed and
    /// that the event carries the canonical suffix; anything else writes no event.
    fn history_op(&mut self, call: &Req, at: u64) -> CodecResult<HistoryOutcome> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; HISTORY_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = epoch::history(
            &call.context(at)?,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        );
        let Ok(HistoryOutcome::Applied {
            record,
            revision,
            result,
            state_len,
            event_len,
        }) = outcome
        else {
            assert!(event.iter().all(|b| *b == 0));
            return outcome;
        };
        let previous = &self.world.bytes;
        let current = &next[..state_len];
        let before = region_of(previous)?;
        assert_eq!(
            region_of(current)?,
            Region {
                state: replace_record(&before.state, record)?,
                seal: before.seal,
            }
        );
        let (b, a) = (
            decode_shared_state(previous)?,
            decode_shared_state(current)?,
        );
        assert_eq!((a.revision, revision), (b.revision + 1, b.revision + 1));
        assert_eq!(
            a.feature_sections[0],
            rebound_policy(previous, revision)?.as_slice()
        );
        assert_eq!(a.feature_sections[1..4], b.feature_sections[1..4]);
        assert_eq!(table_of(current)?, table_of(previous)?);
        assert_eq!(a.control.feature_bytes, b.control.feature_bytes);
        let e = &envelope.envelope;
        let (topic, suffix_len) = history_topic(e.operation)?;
        let (operation, common, suffix) = codec::decode_event_frame(topic, &event[..event_len])?;
        assert_eq!(operation, e.operation);
        assert_eq!(
            (
                common.market,
                common.epoch,
                common.config.get(),
                common.revision
            ),
            (e.market, e.epoch, e.config, revision)
        );
        assert_eq!(
            (common.request, common.result),
            (envelope.request_digest()?, result)
        );
        assert_eq!(suffix.len(), suffix_len);
        assert_eq!(&suffix[..32], record.worker.as_bytes());
        assert_eq!(result, codec::result_digest(suffix)?);
        self.world.bytes = current.to_vec();
        if e.actor == PrincipalId::new(OWNER)? {
            self.world.owner_sequence = e.sequence + 1;
        } else {
            self.world.used(e.actor.bytes()[0], e.sequence);
        }
        outcome
    }
    /// A refused history call leaves the committed bytes unchanged.
    fn history_refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.history_op(call, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    /// An applied history call and the resulting record.
    fn history(&mut self, call: &Req, at: u64) -> CodecResult<ReputationCurrent> {
        let HistoryOutcome::Applied { record, .. } = self.history_op(call, at)? else {
            return Err(NON_CANONICAL);
        };
        Ok(record)
    }
}

/// F06 `ExpireEpochClaims` of `epoch` at `at`; the F05 bytes after the reward state are kept.
impl World {
    fn expire(&mut self, epoch: u64, at: u64) -> TestResult {
        self.edit(|parts, _| {
            let head = parts.rewards.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?;
            let tail = parts.rewards.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
            let mut out = vec![0; REWARD_STATE_BYTES];
            decode_reward_state(head)?.expire_epoch_claims(epoch, at, &mut out)?;
            out.extend_from_slice(tail);
            parts.rewards = out;
            Ok(())
        })
    }
}

fn print_not_run(case: &str, reason: &str) {
    println!("NOT_RUN {case}: {reason}");
}

#[test]
fn f07_a01_qualified_observations_and_key_rotation() -> TestResult {
    let mut m = market()?;
    let low = m.record(LOW)?;
    assert_eq!(standing(&low), (0, 0, Some(0), Some(6)));
    assert_eq!(low.reset_generation.get(), 1);
    assert_eq!(low.segment, segment_digest(m.key(LOW, 1)?)?);
    assert_eq!(low.status, HistoryStatus::Active);
    assert_eq!(standing(&m.record(LOW + 1)?), (0, 0, None, None));
    let six = completed(&m.region()?.state, 6)?;
    assert_eq!(
        (six.observed_workers, six.total_workers, six.covered_workers),
        (0, 1, 1)
    );
    let progress = m.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let low = m.record(LOW)?;
    assert_eq!(standing(&low), (100_000, 1, Some(500_000), Some(7)));
    assert_eq!(low.confidence()?.get(), 125_000);
    assert_eq!(
        low.last_observed,
        Presence::Present(Observation {
            epoch: 7,
            height: SETTLE_AT
        })
    );
    assert_eq!(low.segment, segment_digest(m.key(LOW, 1)?)?);
    assert_eq!(standing(&m.record(LOW + 1)?), (0, 0, Some(0), Some(7)));
    let seven = completed(&m.region()?.state, 7)?;
    assert_eq!(
        (
            seven.observed_workers,
            seven.total_workers,
            seven.covered_workers
        ),
        (1, 2, 2)
    );
    assert_eq!(seven.result, root_of(&progress)?);
    let region = m.region()?;
    m.replace_worker(LOW, SETTLE_AT + 1)?;
    assert_eq!(m.region()?, region);
    m.open_epoch(8)?;
    assert_eq!(m.record(LOW)?, low);
    let worker = m.world.parts()?.workers.get(id(LOW)?).ok_or(NOT_FOUND)?;
    assert_eq!(
        (worker.generation, worker.delegate),
        (2, public(&replacement_key(LOW)))
    );
    m.run(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let low = m.record(LOW)?;
    assert_eq!(standing(&low), (187_500, 2, Some(500_000), Some(8)));
    assert_eq!(low.confidence()?.get(), 250_000);
    assert_eq!(low.segment, segment_digest(m.key(LOW, 1)?)?);
    println!("AI.F07-A01 PASS q100000/count1/confidence125000 at 7, 187500/count2/confidence250000 at 8, F02 replacement changes no F07 value");
    Ok(())
}

#[test]
fn f07_a02_a03_quality_branches() -> TestResult {
    let mut m = market()?;
    m.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let observed = m.record(LOW)?.last_observed;
    m.epoch(8, &plan(&[2, 3], &[(LOW, 800_000), (LOW + 1, 900_000)])?)?;
    let low = m.record(LOW)?;
    assert_eq!(
        standing(&low),
        (missing(100_000), 1, Some(333_333), Some(8))
    );
    assert_eq!(missing(100_000), 98_437);
    assert_eq!(low.last_observed, observed);
    assert_eq!(
        standing(&m.record(LOW + 1)?),
        (0, 0, Some(333_333), Some(8))
    );
    m.epoch(9, &plan(&[2, 3, 4], &[(LOW, 0), (LOW + 1, 0)])?)?;
    let low = m.record(LOW)?;
    assert_eq!(
        standing(&low),
        (qualified(98_437, 0), 2, Some(500_000), Some(9))
    );
    assert_eq!(qualified(98_437, 0), 86_132);
    assert_eq!(
        low.last_observed,
        Presence::Present(Observation {
            epoch: 9,
            height: ORIGIN + 128 * 9 + 96
        })
    );
    assert_eq!(
        standing(&m.record(LOW + 1)?),
        (0, 1, Some(500_000), Some(9))
    );
    let nine = completed(&m.region()?.state, 9)?;
    assert_eq!((nine.observed_workers, nine.total_workers), (2, 2));
    println!("AI.F07-A02 PASS missing branch 100000->98437 without quorum, observed flag kept");
    println!("AI.F07-A03 PASS qualified s0 98437->86132 count 2, q0 with missing stays 0, zero-observed labelled by last_observed");
    print_not_run(
        "AI.F07-A02/A03 exact vectors",
        "q123457 and q800000 starting values are not reachable from q0 by the producers within the grant window; the pure vectors live in tests/reputation_vectors.rs",
    );
    Ok(())
}

/// Worker `LOW` and evaluators 2..=4 enrolled before activation and epoch 6 opened at 896 with
/// only `LOW` frozen.
fn alone() -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    world.schedule(6, 134)?;
    world.fund(FUND, 135)?;
    let frozen = world.open(896)?;
    world.advance(897)?;
    let header = world.parts()?.market()?;
    Ok(Market {
        world,
        frozen,
        header,
    })
}

#[test]
fn f07_a04_a11_count_cap_retention_and_pruning() -> TestResult {
    let mut m = alone()?;
    let scores = plan(&[2, 3, 4], &[(LOW, 800_000)])?;
    let later = plan(&[5, 6, 7], &[(LOW, 800_000)])?;
    m.seal(&[2, 3, 4], 960)?;
    m.score(&scores)?;
    let (mut q, mut count) = (100_000, 1);
    assert_eq!(
        standing(&m.record(LOW)?),
        (q, count, Some(1_000_000), Some(6))
    );
    for e in 7..=37 {
        m.open_epoch(e)?;
        if e == 30 {
            for n in 5..=7 {
                m.world.evaluator(n, m.start() + u64::from(n))?;
            }
        }
        m.run(if e < 32 { &scores } else { &later })?;
        q = qualified(q, 800_000);
        count = (count + 1).min(32);
        let coverage = if e == 31 { 500_000 } else { 1_000_000 };
        assert_eq!(
            standing(&m.record(LOW)?),
            (q, count, Some(coverage), Some(e))
        );
        if e == 36 {
            assert_eq!(count, 31);
        }
        if e == 20 {
            m.replace_worker(LOW, m.start() + 100)?;
        }
    }
    let low = m.record(LOW)?;
    assert_eq!(
        (low.qualifying_count, low.confidence()?.get()),
        (32, 1_000_000)
    );
    let state = m.region()?.state;
    assert_eq!(state.completed().count(), 32);
    assert_eq!(state.completed().next().map(|r| r.epoch), Some(6));
    assert!(state.encoded_len()? <= SECTION_CAP);
    let before = m.world.bytes.clone();
    assert_eq!(m.world.open(ORIGIN + 128 * 38), Err(RETENTION_FULL));
    assert_eq!(m.world.bytes, before);
    m.world.expire(6, ORIGIN + 128 * 39)?;
    let opened = m.open_epoch(39)?;
    assert_eq!(
        (opened.previous, opened.skipped, opened.evaluators),
        (Some(37), 1, 3)
    );
    assert_eq!(m.reward_row(6).err(), Some(NOT_FOUND));
    let state = m.region()?.state;
    assert_eq!(
        state.lookup_epoch(6),
        Err(HistoryLookupError::HistoryOutsideRetention)
    );
    assert_eq!(state.completed().count(), 31);
    assert_eq!(state.completed().next().map(|r| r.epoch), Some(7));
    assert_eq!(m.record(LOW)?, low);
    m.seal(&[5, 6, 7], m.start() + 64)?;
    let mut bad = m.clone();
    let good = ballot(&[(LOW, 1)])?;
    let report = bad.signed(5, &good)?;
    bad.commit(5, &report, bad.start() + 64)?;
    let body = raw_body(&report.body, 1, &entries(&[(id(LOW)?, 1_000_001)]))?;
    let call = Req {
        payload: raw_reveal(&body, &report.signature.0, &SALT)?,
        ..bad.reveal_request(5, &report)?
    };
    assert_eq!(call.encode().map(|_| ()), Err(F03_SCORE_RANGE));
    assert_eq!(bad.admitted(5)?, None);
    m.score(&later)?;
    q = qualified(q, 800_000);
    let low = m.record(LOW)?;
    assert_eq!(standing(&low), (q, 32, Some(1_000_000), Some(39)));
    assert_eq!(low.confidence()?.get(), 1_000_000);
    let state = m.region()?.state;
    assert_eq!(state.completed().count(), 32);
    assert_eq!(
        (
            state.completed().next().map(|r| r.epoch),
            state.latest_completed()
        ),
        (Some(7), Presence::Present(39))
    );
    assert_eq!(
        state.encoded_len()?,
        HEADER_BYTES + CURRENT_BYTES + LIMIT * HISTORY_BYTES
    );
    println!("AI.F07-A04 PASS count 31->32 at 37, 33rd qualifying epoch keeps 32 with confidence 1000000, score 1000001 refused before mutation");
    println!("AI.F07-A11 PASS RETENTION_FULL at 38 byte-identical, oldest summary 6 pruned with its F06 row at 39, HistoryOutsideRetention for 6, 32 summaries fit the section");
    print_not_run(
        "AI.F07-A04 q1000000 and u64max generation",
        "floor((7q+1000000)/8) never reaches 1000000 from q<1000000 and no producer reaches reset generation u64::MAX",
    );
    Ok(())
}

#[test]
fn f07_a05_coverage_and_quorum() -> TestResult {
    let mut m = market()?;
    let mut beat = m.clone();
    beat.world.heartbeat(LOW, 7, COMMIT_AT)?;
    let scores = plan(&[2, 3, 4, 5], &[(LOW, 800_000)])?;
    let reveals = m.admit(&scores)?;
    let start = m.start();
    for at in [90, 91, 92] {
        assert!(matches!(m.call(&reveals[0], start + at)?, F04::Retained(_)));
    }
    let dup = m.signed(6, &scores[0].1)?;
    m.commit(6, &dup, start + 64)?;
    let body = raw_body(&dup.body, 2, &entries(&[(id(LOW)?, 1), (id(LOW)?, 1)]))?;
    let call = Req {
        payload: raw_reveal(&body, &dup.signature.0, &SALT)?,
        ..m.reveal_request(6, &dup)?
    };
    assert_eq!(call.encode().map(|_| ()), Err(NON_CANONICAL));
    m.settle(SETTLE_AT)?;
    assert_eq!(
        standing(&m.record(LOW)?),
        (100_000, 1, Some(666_666), Some(7))
    );
    beat.admit(&scores)?;
    beat.settle(SETTLE_AT)?;
    assert_eq!(beat.region()?, m.region()?);
    let mut c = crowded(2)?;
    c.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    assert_eq!(
        standing(&c.record(LOW)?),
        (100_000, 1, Some(375_000), Some(6))
    );
    c.epoch(7, &plan(&[2, 3], &[(LOW, 800_000)])?)?;
    assert_eq!(
        standing(&c.record(LOW)?),
        (98_437, 1, Some(250_000), Some(7))
    );
    c.open_epoch(8)?;
    c.seal(&[], c.start() + 64)?;
    for n in 2..=9 {
        c.revoke(n, c.start() + 64 + u64::from(n))?;
    }
    c.settle(c.start() + 96)?;
    let low = c.record(LOW)?;
    assert_eq!(standing(&low), (missing(98_437), 1, None, Some(8)));
    let eight = completed(&c.region()?.state, 8)?;
    assert_eq!((eight.observed_workers, eight.covered_workers), (0, 0));
    println!("AI.F07-A05 PASS E6/N4 666666, retries and duplicate entries keep N, heartbeat changes nothing, E8/N2 250000 decays, E0/N0 coverage absent");
    print_not_run(
        "AI.F07-A05 E0/N1",
        "F05 admits no accepted score once every frozen evaluator is ineligible, so no producer yields support 1 with eligible 0",
    );
    Ok(())
}

#[test]
fn f07_a06_completion_identity_and_unopened_interval() -> TestResult {
    let mut m = market()?;
    m.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    m.open_epoch(8)?;
    let scores = plan(&[2, 3, 4], &[(LOW, 800_000)])?;
    m.seal(&[2, 3, 4], m.start() + 64)?;
    m.admit(&scores)?;
    let at = m.start() + 96;
    m.begin(at)?;
    let input = input_of(&m.progress()?)?;
    let progress = m.complete(at)?;
    assert_eq!(
        standing(&m.record(LOW)?),
        (187_500, 2, Some(500_000), Some(8))
    );
    let retry = m.finalize_call(input)?;
    let before = m.world.bytes.clone();
    assert!(matches!(
        m.aggregate(&retry, at + 1)?,
        Agg::AlreadyApplied { .. }
    ));
    assert_eq!(m.world.bytes, before);
    let state = m.region()?.state;
    assert_eq!(state.completed().filter(|r| r.epoch == 8).count(), 1);
    let root = root_of(&progress)?;
    assert_eq!(
        state.assess_completion(8, Digest32::new([0xee; 32])?),
        Err(CONFLICT)
    );
    assert_eq!(
        state.assess_completion(8, root)?,
        CompletionIdentity::Retained(completed(&state, 8)?)
    );
    let low = m.record(LOW)?;
    let opened = m.open_epoch(19)?;
    assert_eq!((opened.previous, opened.skipped), (Some(8), 10));
    assert_eq!(m.record(LOW)?, low);
    assert_eq!(m.aggregation_refused(&retry, m.start() + 1)?, WRONG_EPOCH);
    m.run(&scores)?;
    assert_eq!(
        standing(&m.record(LOW)?),
        (qualified(187_500, 800_000), 3, Some(500_000), Some(19))
    );
    let state = m.region()?.state;
    assert_eq!(
        state.assess_completion(12, Digest32::new([0xee; 32])?),
        Err(WRONG_EPOCH)
    );
    assert_eq!(
        state.completed().map(|r| r.epoch).collect::<Vec<_>>(),
        vec![6, 7, 8, 19]
    );
    println!("AI.F07-A06 PASS 187500 once, retry AlreadyApplied with one summary, CONFLICT for another result, WRONG_EPOCH for an older epoch, ten unopened epochs fabricate nothing");
    Ok(())
}

#[test]
fn f07_a07_rotation_seat_reuse_and_model_rollover() -> TestResult {
    let mut m = market()?;
    m.score(&plan(&[2, 3, 4], &[(LOW, 800_000), (LOW + 1, 600_000)])?)?;
    let w = m.record(LOW + 1)?;
    assert_eq!(w.quality.get(), 75_000);
    let low = m.record(LOW)?;
    m.replace_worker(LOW, SETTLE_AT + 1)?;
    assert_eq!(m.record(LOW)?, low);
    let seat = m
        .world
        .parts()?
        .workers
        .get(id(LOW + 1)?)
        .ok_or(NOT_FOUND)?
        .slot;
    m.world.exit(LOW + 1, 8, SETTLE_AT + 2)?;
    let opened = m.open_epoch(8)?;
    assert_eq!(opened.workers, 1);
    assert!(m.world.parts()?.workers.get(id(LOW + 1)?).is_none());
    let retired = m.record(LOW + 1)?;
    assert_eq!(retired.status, HistoryStatus::Retired);
    assert_eq!(
        (retired.quality, retired.segment, retired.qualifying_count),
        (w.quality, w.segment, w.qualifying_count)
    );
    assert_eq!(m.world.enroll(SUCCESSOR, m.start() + 8)?, seat);
    m.run(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let opened = m.open_epoch(9)?;
    assert_eq!(opened.workers, 2);
    let v = m.record(SUCCESSOR)?;
    assert_eq!(standing(&v), (0, 0, None, None));
    assert_eq!(v.segment, segment_digest(m.key(SUCCESSOR, 1)?)?);
    assert_eq!(m.record(LOW + 1)?, retired);
    let start = m.start();
    m.seal(&[2, 3, 4, 5], start + 64)?;
    let ghost_scores = ballot(&[(LOW + 1, 900_000)])?;
    let ghost = m.signed(2, &ghost_scores)?;
    m.commit(2, &ghost, start + 64)?;
    m.admit(&plan(&[3, 4, 5], &[(LOW, 800_000)])?)?;
    let call = m.reveal_request(2, &ghost)?;
    assert_eq!(m.refused(&call, start + 83)?, F03_UNKNOWN_WORKER);
    m.settle(start + 96)?;
    assert_eq!(
        standing(&m.record(LOW)?),
        (qualified(187_500, 800_000), 3, Some(500_000), Some(9))
    );
    assert_eq!(standing(&m.record(SUCCESSOR)?), (0, 0, Some(0), Some(9)));
    assert_eq!(m.record(LOW + 1)?, retired);
    let old = m.key(LOW, 1)?;
    let old_segment = m.record(LOW)?.segment;
    m.world.stage(&policy(2, 9)?, 10, start + 97)?;
    let opened = m.open_epoch(10)?;
    assert_eq!((opened.config.get(), opened.policy_activated), (2, true));
    for n in [LOW, SUCCESSOR] {
        let r = m.record(n)?;
        assert_eq!(standing(&r), (0, 0, None, Some(9)));
        assert_eq!(r.reset_generation.get(), 2);
        assert_eq!(r.segment, segment_digest(m.key(n, 2)?)?);
        assert!(matches!(r.previous_segment, Presence::Present(_)));
    }
    assert_eq!(m.record(LOW + 1)?, retired);
    let state = m.region()?.state;
    assert_eq!(
        state.worker(id(LOW)?, old).err(),
        Some(F07_SEGMENT_MISMATCH)
    );
    assert_eq!(state.worker(id(LOW)?, m.key(LOW, 2)?)?.quality.get(), 0);
    let mut call = m.history_call(
        dispatch::SuspendHistory,
        Actor::Market,
        history_payload(id(LOW)?, old_segment, 1, OWNER_REQUEST),
    )?;
    call.config = 1;
    assert_eq!(m.history_refused(&call, m.start() + 1)?, WRONG_CONFIG);
    println!("AI.F07-A07 PASS rotation keeps the segment, seat successor starts unobserved, stale evaluation naming the departed worker refused, model change rolls every live segment over at opening 10");
    println!("AI.F07-A07 CONFLICT spec labels the old-model lookup BindingMismatch; the code refuses F07_SEGMENT_MISMATCH (lookup) and WRONG_CONFIG (request)");
    Ok(())
}

#[test]
fn f07_a08_reset_and_status_authority() -> TestResult {
    let mut m = market()?;
    m.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let suspend = m.current_request(dispatch::SuspendHistory, Actor::Market, LOW, OWNER_REQUEST)?;
    let suspended = m.history(&suspend, SETTLE_AT + 1)?;
    assert_eq!(suspended.status, HistoryStatus::Suspended);
    assert_eq!(standing(&suspended), (100_000, 1, Some(500_000), Some(7)));
    let other = m.current_request(
        dispatch::ResetHistory,
        Actor::Worker(LOW + 1),
        LOW,
        OWNER_RESET,
    )?;
    assert_eq!(m.history_refused(&other, SETTLE_AT + 2)?, UNAUTHORIZED);
    let by_worker = m.current_request(
        dispatch::SuspendHistory,
        Actor::Worker(LOW),
        LOW,
        OWNER_REQUEST,
    )?;
    assert_eq!(m.history_refused(&by_worker, SETTLE_AT + 2)?, UNAUTHORIZED);
    let mut delegate =
        m.current_request(dispatch::ResetHistory, Actor::Worker(LOW), LOW, OWNER_RESET)?;
    delegate.actor = PrincipalId::new(public(&delegate_key(LOW)).0)?;
    assert_eq!(m.history_refused(&delegate, SETTLE_AT + 2)?, UNAUTHORIZED);
    let reset = m.current_request(dispatch::ResetHistory, Actor::Worker(LOW), LOW, OWNER_RESET)?;
    let rewards = section_of(&m.world.bytes, Section::SettlementClaims)?;
    let record = m.history(&reset, SETTLE_AT + 3)?;
    assert_eq!(record.reset_generation.get(), 2);
    assert_eq!(standing(&record), (0, 0, None, Some(7)));
    assert_eq!(record.status, HistoryStatus::Suspended);
    assert_eq!(record.segment, segment_digest(m.key(LOW, 2)?)?);
    let Presence::Present(closure) = record.previous_segment else {
        return Err(NOT_FOUND);
    };
    assert_ne!(closure.as_bytes(), &[0; 32]);
    assert_eq!(
        section_of(&m.world.bytes, Section::SettlementClaims)?,
        rewards
    );
    let before = m.world.bytes.clone();
    assert!(matches!(
        m.history_op(&reset, SETTLE_AT + 4)?,
        HistoryOutcome::Retained(_)
    ));
    assert_eq!(m.world.bytes, before);
    let changed = Req {
        payload: history_payload(id(LOW)?, record.segment, 2, OWNER_RESET),
        ..reset.clone()
    };
    assert_eq!(
        m.history_refused(&changed, SETTLE_AT + 4)?,
        F07_IDEMPOTENCY_CONFLICT
    );
    m.open_epoch(8)?;
    let frozen = m.current_request(dispatch::ResetHistory, Actor::Worker(LOW), LOW, OWNER_RESET)?;
    assert_eq!(
        m.history_refused(&frozen, m.start() + 1)?,
        F07_IDENTITY_FROZEN
    );
    let replay = m.history_call(
        dispatch::ResetHistory,
        Actor::Worker(LOW),
        history_payload(id(LOW)?, suspended.segment, 1, OWNER_RESET),
    )?;
    assert_eq!(
        m.history_refused(&replay, m.start() + 1)?,
        F07_GENERATION_MISMATCH
    );
    let resume = m.current_request(dispatch::ResumeHistory, Actor::Market, LOW, OWNER_REQUEST)?;
    assert_eq!(
        m.history(&resume, m.start() + 2)?.status,
        HistoryStatus::Active
    );
    let again = m.current_request(dispatch::ResumeHistory, Actor::Market, LOW, OWNER_REQUEST)?;
    assert_eq!(m.history_refused(&again, m.start() + 3)?, CONFLICT);
    println!("AI.F07-A08 PASS reset of a suspended segment keeps Suspended at generation 2 with a closure digest and unchanged F06, other owner/delegate/market-owner-only refusals, retained retry, idempotency conflict, frozen and previous-generation refusals");
    print_not_run(
        "AI.F07-A08 q900000/count8 start",
        "the reset is run from the producer-reached q100000/count1; q900000 needs more qualifying epochs than one grant window",
    );
    Ok(())
}

#[test]
fn f07_a10_segment_isolation_and_forged_requests() -> TestResult {
    let mut m = market()?;
    let mut nomination = principal(LOW)?.bytes().to_vec();
    nomination.extend_from_slice(&[LOW + 1; 32]);
    nomination.extend_from_slice(rubric()?.as_bytes());
    nomination.extend_from_slice(&public(&evaluator_key(LOW)).0);
    for value in [1u64, 1, m.frozen.epoch + 1, m.frozen.epoch + 33] {
        nomination.extend_from_slice(&value.to_be_bytes());
    }
    let before = m.world.bytes.clone();
    assert_eq!(
        m.owner_authority(dispatch::ScheduleEvaluator, nomination, false, 1090),
        Err(ROLE_CONFLICT)
    );
    assert_eq!(m.world.bytes, before);
    m.score(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let state = m.region()?.state;
    let key = m.key(LOW, 1)?;
    assert_eq!(state.worker(id(LOW)?, key)?.quality.get(), 100_000);
    let other_market = derive_market(ChainDomain::new([0x0b; 32])?, ProgramId::new(PROGRAM)?)?;
    for (probe, code) in [
        (
            SegmentKey {
                market: other_market,
                ..key
            },
            F07_BINDING_MISMATCH,
        ),
        (
            SegmentKey {
                worker: id(LOW + 1)?,
                ..key
            },
            F07_BINDING_MISMATCH,
        ),
        (
            SegmentKey {
                policy: Digest32::new(policy(2, MODEL)?.digest()?.bytes())?,
                ..key
            },
            F07_SEGMENT_MISMATCH,
        ),
        (
            SegmentKey {
                model: Digest32::new([9; 32])?,
                ..key
            },
            F07_SEGMENT_MISMATCH,
        ),
        (
            SegmentKey {
                config: Version::new(2)?,
                ..key
            },
            F07_SEGMENT_MISMATCH,
        ),
    ] {
        assert_eq!(state.worker(id(LOW)?, probe).err(), Some(code));
    }
    let record = m.record(LOW)?;
    let mut forged = history_payload(id(LOW)?, record.segment, 1, OWNER_REQUEST);
    forged.extend_from_slice(&1_000_000u32.to_be_bytes());
    assert_eq!(forged.len(), HISTORY_PAYLOAD_BYTES + 4);
    let call = m.history_call(dispatch::SuspendHistory, Actor::Market, forged)?;
    assert_eq!(m.history_refused(&call, SETTLE_AT + 1)?, NON_CANONICAL);
    for reason in [MODEL_CHANGED, 3] {
        let call = m.current_request(dispatch::ResetHistory, Actor::Market, LOW, reason)?;
        assert_eq!(
            m.history_refused(&call, SETTLE_AT + 1)?,
            F07_BINDING_MISMATCH
        );
    }
    let unknown = m.history_call(
        dispatch::SuspendHistory,
        Actor::Market,
        history_payload(id(0x55)?, record.segment, 1, OWNER_REQUEST),
    )?;
    assert_eq!(
        m.history_refused(&unknown, SETTLE_AT + 1)?,
        F07_UNKNOWN_WORKER
    );
    assert_eq!(m.record(LOW)?, record);
    println!("AI.F07-A10 PASS segment lookup refused across market/worker/policy/model/config, forged score payload and unchanged-binding resets refused, owner evaluator for its own worker ROLE_CONFLICT");
    Ok(())
}

#[test]
fn f07_a11_capacity_and_sizes() -> TestResult {
    let mut m = crowded(32)?;
    let region = m.region()?;
    assert_eq!(region.state.records().count(), LIMIT);
    let pairs = (LOW..LOW + 32).map(|n| (n, 500_000)).collect::<Vec<_>>();
    m.score(&plan(&[2, 3, 4], &pairs)?)?;
    let state = m.region()?.state;
    for r in state.records() {
        assert_eq!(standing(r), (62_500, 1, Some(375_000), Some(6)));
    }
    let six = completed(&state, 6)?;
    assert_eq!(
        (six.observed_workers, six.total_workers, six.covered_workers),
        (32, 32, 32)
    );
    let section = state.encoded_len()?;
    assert_eq!(
        section,
        HEADER_BYTES + LIMIT * CURRENT_BYTES + HISTORY_BYTES
    );
    let joint = section_of(&m.world.bytes, Section::ReputationAdmission)?.len();
    let table = table_of(&m.world.bytes)?.len();
    let full = HEADER_BYTES + LIMIT * (CURRENT_BYTES + HISTORY_BYTES);
    assert!(section <= full && full <= SECTION_CAP);
    assert!(joint - section + full <= JOINT_CAP);
    assert!(m.world.bytes.len() - section + full <= COMMON_CAP);
    assert!(table < joint);
    let before = m.world.bytes.clone();
    assert_eq!(m.world.enroll(0x60, 1000), Err(CAPACITY));
    assert_eq!(m.world.bytes, before);
    println!("AI.F07-A11 PASS 32 records scored, 33rd worker CAPACITY, 32 current + 32 history within 9088, joint within 24576, common within 196608");
    Ok(())
}

#[test]
fn f07_a12_atomic_terminal_and_sealed_history_allowed() -> TestResult {
    let mut m = market()?;
    m.admit(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    m.begin(SETTLE_AT)?;
    let mut progress = m.progress()?;
    while progress.cursor < progress.worker_count {
        progress = m.process(SETTLE_AT)?;
    }
    let input = input_of(&progress)?;
    let call = m.finalize_call(input)?;
    let before = m.world.bytes.clone();
    for sizes in [
        [before.len(), AGGREGATE_SCRATCH_BYTES, MAX_EVENT_BYTES],
        [MAX_STATE_BYTES, agg::SCRATCH_BYTES, MAX_EVENT_BYTES],
        [MAX_STATE_BYTES, AGGREGATE_SCRATCH_BYTES, 64],
    ] {
        let (outcome, _, _) = m.compose(&call, SETTLE_AT, sizes)?;
        assert_eq!(outcome.err(), Some(CAPACITY));
    }
    assert_eq!(m.world.bytes, before);
    m.finalize(SETTLE_AT)?;
    let after = m.world.bytes.clone();
    let Agg::AlreadyApplied { progress, .. } = m.aggregate(&call, SETTLE_AT + 1)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(m.world.bytes, after);
    assert_eq!(
        completed(&m.region()?.state, 7)?.result,
        root_of(&progress)?
    );
    m.open_epoch(8)?;
    m.seal(&[2, 3, 4], m.start() + 64)?;
    m.admit(&plan(&[2, 3, 4], &[(LOW, 800_000)])?)?;
    let at = m.start() + 96;
    let mut a = m.clone();
    let suspend = a.current_request(dispatch::SuspendHistory, Actor::Market, LOW, OWNER_REQUEST)?;
    a.history(&suspend, at)?;
    a.settle(at)?;
    let mut b = m.clone();
    b.settle(at)?;
    assert_eq!(
        standing(&a.record(LOW)?),
        (missing(100_000), 1, Some(500_000), Some(8))
    );
    assert_eq!(
        standing(&b.record(LOW)?),
        (187_500, 2, Some(500_000), Some(8))
    );
    assert_eq!(
        section_of(&a.world.bytes, Section::SettlementClaims)?,
        section_of(&b.world.bytes, Section::SettlementClaims)?
    );
    let mut c = m.clone();
    c.begin(at)?;
    let suspend = c.current_request(dispatch::SuspendHistory, Actor::Market, LOW, OWNER_REQUEST)?;
    c.history(&suspend, at)?;
    c.complete(at)?;
    let low = c.record(LOW)?;
    assert_eq!(standing(&low), (187_500, 2, Some(500_000), Some(8)));
    assert_eq!(low.status, HistoryStatus::Suspended);
    c.open_epoch(9)?;
    c.seal(&[], c.start() + 64)?;
    c.begin(c.start() + 96)?;
    let Presence::Present(seal) = c.region()?.seal else {
        return Err(NOT_FOUND);
    };
    let low_index = Market::index(LOW, &[LOW, LOW + 1])?;
    let other_index = Market::index(LOW + 1, &[LOW, LOW + 1])?;
    assert!(!seal.allows(low_index));
    assert!(seal.allows(other_index));
    c.complete(c.start() + 96)?;
    assert_eq!(
        standing(&c.record(LOW)?),
        (missing(187_500), 2, Some(0), Some(9))
    );
    println!("AI.F07-A12 PASS buffer refusals in the terminal transition change nothing, retry returns the retained step, suspension before the seal decays with identical F05/F06 bytes, suspension after the seal applies from the next seal");
    print_not_run(
        "AI.F07-A12 native host storage refusal",
        "needs the production-linked native host; the in-process refusals use undersized caller buffers",
    );
    Ok(())
}
