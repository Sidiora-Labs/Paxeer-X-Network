//! AI.F05-T03 atomic aggregation and epoch completion over the complete shared state value.
//! Every market is produced by the real F01/F02/F03/F06/F08/F09 producers, `OPEN_EPOCH` and the
//! F01 task-set lifecycle; every score is admitted through real `CommitScore`/`RevealScore`
//! calls, and `BeginAggregation`, `ProcessAggregation` and `FinalizeAggregation` go through
//! `aggregation::apply` with real envelopes. A missing, expired or refused report is no vote;
//! an explicit zero is a vote.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{
        self as agg, display_share_ppm, Outcome as Agg, Progress, BEGIN_RESPONSE_BYTES,
        FINALIZE_RESPONSE_BYTES, PROCESS_RESPONSE_BYTES,
    },
    aggregation_codec::{
        decode_current, decode_history, input_digest, AggregationPhase, EpochAggregation,
        HistorySummary, QualityStatus, ReportCommitment, WorkerAggregate,
    },
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        CommitScorePayload, Envelope, ReportBody, RevealScorePayload, ScoreVector,
        ValidatedEnvelope, REPORT_FIXED_BYTES, REPORT_MAX_BYTES,
    },
    commit_reveal::{self as cr, commitment, Outcome as F04, Status},
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F03_SCORE_RANGE,
        F05_REPORT_INVARIANT, F06_EPOCH_TERMINAL, NON_CANONICAL, NOT_FOUND, REVOKED, ROLE_CONFLICT,
        STALE_CURSOR, WRONG_CONFIG, WRONG_EPOCH, WRONG_PHASE, WRONG_ROSTER,
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
    reward_math::allocate,
    rewards::{
        decode_reward_state, EpochStatus, FundReplay, FundRequest, FundingAuthority, FundingPhase,
        RewardEpoch, RewardLedger, RewardOutcome, RewardState, FUNDING_POLICY_VERSION,
        REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvidenceRoot, FrozenBinding, MarketId, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Salt32, Signature64, Version, WorkerId, WorkerRosterEntry,
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
const ROLE_EXPIRY: u64 = 5000;
const SALT: [u8; 32] = [0x5a; 32];
const BEGIN_TOPIC: &[u8] = b"PAXAI/v1/BeginAggregation";
const PROCESS_TOPIC: &[u8] = b"PAXAI/v1/ProcessAggregation";
const FINALIZE_TOPIC: &[u8] = b"PAXAI/v1/FinalizeAggregation";
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120),
/// settlement from 1120.
const COMMIT_AT: u64 = 1088;
const REVEAL_END: u64 = 1120;
const SETTLE_AT: u64 = 1120;
/// The lowest frozen worker.
const LOW: u8 = 0x20;
/// Caller buffers able to hold every aggregation output.
const FULL: [usize; 3] = [MAX_STATE_BYTES, agg::SCRATCH_BYTES, MAX_EVENT_BYTES];

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
fn policy(config: u64, minimum_evaluators: u8) -> CodecResult<TaskPolicyV1> {
    let mut policy = TaskPolicyV1::bounded_default(
        config,
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
        100,
        1,
    )?;
    policy.minimum_evaluator_count = minimum_evaluators;
    Ok(policy)
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
/// The rotated signing key of evaluator `n`.
fn rotated_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0xe0 + n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// The evidence root evaluator `n` seals.
const fn root(n: u8) -> u8 {
    0xe0 + n
}
/// A request id unique per kind, evaluator and sequence.
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

/// One request envelope; `delegate` is (claimed key, signer) and `signed` replaces the payload
/// the signer signs.
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
    policy(1, 3)?.encode(&mut policy_bytes)?;
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

/// Owned, decoded sections of one committed shared state value, control feature bytes
/// included.
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

/// One market's committed shared state bytes and its next owner sequence.
#[derive(Clone)]
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    fn create(origin: u64) -> CodecResult<Self> {
        Ok(Self {
            bytes: create(origin)?,
            owner_sequence: 2,
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
    /// A real owner-authorized F01 registry operation (the owner role sequence advances).
    fn owner_op(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let call = Req {
            config: self.section()?.header.active_config_version,
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(operation, PrincipalId::new(OWNER)?, payload)
        };
        let encoded = call.encode()?;
        let current = decode_shared_state(&self.bytes)?;
        let mut section = vec![0; F01_SECTION_CAP];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Outcome::Applied { state, .. } = registry_ops::apply(
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
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
    }
    /// Real F01 owner `SUSPEND`; the lifecycle becomes SUSPENDED.
    fn suspend(&mut self, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&[0x50; 32]);
        self.owner_op(dispatch::SUSPEND, payload, at)
    }
}

/// Worker, evaluator, funding and settlement producers of the market journey.
impl World {
    /// F02 ENROLLED record with an ed25519 delegate, worker replay slot and F08 approval
    /// plus owner acceptance.
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
    /// F03 nomination accepted through the signed F08 evaluator consent of its delegate; the
    /// evaluator replay slot `n - 2` is bound to its owner (no producer binds it yet).
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
    /// Real F06 `TerminalizeRewards` of `frozen` outside F05, weight 5 for every frozen
    /// worker; F05 bytes after the reward state are carried unchanged.
    fn terminalize(
        &mut self,
        frozen: &Frozen,
        roster: &[WorkerRosterEntry],
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
            let outputs = roster
                .iter()
                .map(|entry| {
                    WorkerAggregate::new(
                        entry.worker,
                        entry.generation,
                        3,
                        QualityStatus::ScoredPositive,
                        5,
                        5,
                    )
                })
                .collect::<CodecResult<Vec<_>>>()?;
            let allocation = allocate(frozen.budget, &outputs)?;
            let aggregation =
                EpochAggregation::structural(binding, Digest32::new([5; 32])?, roster, &outputs)?;
            let (head, tail) = parts
                .rewards
                .split_at_checked(REWARD_STATE_BYTES)
                .ok_or(NOT_FOUND)?;
            let mut terminal = vec![0; REWARD_STATE_BYTES];
            decode_reward_state(head)?.terminalize(
                &binding,
                aggregation.root(),
                &allocation,
                roster,
                at,
                &mut terminal,
            )?;
            terminal.extend_from_slice(tail);
            parts.rewards = terminal;
            Ok(())
        })
    }
    /// Restores workers `LOW + 1 .. LOW + workers` and evaluators 5..=9 beside the real first
    /// enrollments from the real admitted memberships of worker `first` and evaluator 2: the
    /// F08 budget admits four enrollments per epoch and no producer admits a whole bounded
    /// roster at once.
    fn restore(
        &mut self,
        first: WorkerRosterEntry,
        workers: u8,
        at: u64,
    ) -> CodecResult<Vec<WorkerRosterEntry>> {
        self.edit(|parts, market| {
            let worker = parts
                .admission
                .get(Participant::Worker(first.worker))
                .ok_or(NOT_FOUND)?;
            let evaluator = parts
                .admission
                .get(Participant::Evaluator(derive_evaluator(
                    market.market_id,
                    principal(2)?,
                    [2; 32],
                )?))
                .ok_or(NOT_FOUND)?;
            let mut entries = vec![first];
            for n in LOW + 1..LOW + workers {
                let owner = principal(n)?;
                let id = derive_worker(market.market_id, owner, [n; 32])?;
                let slot = parts.workers.free_slot()?;
                let record = worker_record(id, owner, public(&delegate_key(n)), slot, at)?;
                parts.workers.insert(&record)?;
                parts
                    .replay
                    .bind(ActorSlot::worker(usize::from(slot))?, owner, version()?)?;
                parts.admission.insert(AdmissionMeta {
                    participant: Participant::Worker(id),
                    owner,
                    ..worker
                })?;
                entries.push(roster_entry(&record)?);
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
            Ok(entries)
        })
    }
}

/// The `OPEN_EPOCH`, `ADVANCE_ACTIVATION`, task-set and `SealEvidence` calls.
impl World {
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
    /// One F01 task-set call, committed only on `Applied`.
    fn task(&mut self, call: &Req, at: u64) -> TestResult {
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; tasks::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Task::Applied { state_len, .. } = tasks::apply(
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
        Ok(())
    }
    /// One real `SealEvidence`, committed only on `Applied`.
    fn evidence(&mut self, call: &Req, at: u64) -> TestResult {
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; SEAL_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Sealing::Applied { state_len, .. } = evidence::apply(
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
        Ok(())
    }
}

/// A market with an opened epoch.
#[derive(Clone)]
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
    workers: Vec<WorkerRosterEntry>,
}
/// Epoch 7 of the journey (T = 1024): worker `LOW` and evaluators 2..=4 enrolled before
/// activation, epoch 6 opened at 896, worker `LOW + 1` and evaluators 5..=7 enrolled in epoch 6,
/// epoch 6 terminalized, epoch 7 opened at 1024, its empty task set sealed and the evidence of
/// every evaluator sealed at 1088 under evaluator sequence 1.
fn market() -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let low = world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    world.schedule(6, 134)?;
    world.fund(500, 135)?;
    let first = world.open(896)?;
    assert_eq!((first.epoch, first.workers, first.evaluators), (6, 1, 3));
    world.advance(897)?;
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen: first,
        header,
        workers: vec![low],
    };
    let high = m.world.enroll(LOW + 1, 900)?;
    for n in 5..=7 {
        m.world.evaluator(n, 896 + u64::from(n))?;
    }
    m.seal(&[], 960)?;
    m.world.terminalize(&first, &[low], 1008)?;
    m.frozen = m.world.open(1024)?;
    m.workers.push(high);
    m.workers.sort_by_key(|w| w.worker);
    assert_eq!(
        (m.frozen.epoch, m.frozen.workers, m.frozen.evaluators),
        (7, 2, 6)
    );
    m.seal(&[2, 3, 4, 5, 6, 7], COMMIT_AT)?;
    Ok(m)
}
/// Epoch 6 (T = 896) of a market with `workers` frozen workers and evaluators 2..=9 frozen,
/// the empty task set sealed and every evidence root sealed at 960.
fn crowded(workers: u8) -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let first = world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    let mut roster = world.restore(first, workers, 134)?;
    world.schedule(6, 135)?;
    world.fund(500, 136)?;
    let frozen = world.open(896)?;
    assert_eq!(
        (frozen.epoch, frozen.workers, frozen.evaluators),
        (6, workers, 8)
    );
    world.advance(897)?;
    roster.sort_by_key(|w| w.worker);
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen,
        header,
        workers: roster,
    };
    m.seal(&[2, 3, 4, 5, 6, 7, 8, 9], 960)?;
    Ok(m)
}

/// Identities, bindings, reports and the task-set and evidence seals of the opened epoch.
impl Market {
    /// T of the opened epoch.
    fn start(&self) -> u64 {
        ORIGIN + 128 * self.frozen.epoch
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
    /// Canonical entries scoring the frozen workers at ascending roster `indices`.
    fn scores(&self, pairs: &[(usize, u32)]) -> Vec<u8> {
        let pairs = pairs
            .iter()
            .map(|&(index, score)| (self.workers[index].worker, score))
            .collect::<Vec<_>>();
        entries(&pairs)
    }
    /// The same score for every frozen worker.
    fn all(&self, score: u32) -> Vec<u8> {
        let pairs = (0..self.workers.len())
            .map(|index| (index, score))
            .collect::<Vec<_>>();
        self.scores(&pairs)
    }
    fn slot(&self, n: u8, at: u64) -> CodecResult<cr::Slot> {
        cr::slot(&self.world.bytes, self.evaluator(n)?, at)
    }
    fn admitted(&self, n: u8) -> CodecResult<Option<AdmissionReceipt>> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?.map(|r| r.receipt))
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's task region, then `SealEvidence` of each
    /// of `evaluators` under evaluator sequence 1.
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
        let sealed =
            tasks::sealed_task_set(&decode_shared_state(&self.world.bytes)?, frozen.epoch)?;
        for &n in evaluators {
            let mut claim = 1u16.to_be_bytes().to_vec();
            claim.extend_from_slice(&[root(n); 32]);
            claim.extend_from_slice(self.frozen.policy.as_bytes());
            claim.extend_from_slice(sealed.as_bytes());
            claim.extend_from_slice(rubric()?.as_bytes());
            claim.push(1);
            let call = Req {
                sequence: 1,
                request: tag(0xc5, n, 1),
                expiry: ROLE_EXPIRY,
                ..bound(dispatch::SealEvidence, principal(n)?, &frozen, claim)
            };
            self.world.evidence(&call, at)?;
        }
        Ok(())
    }
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
    /// One F04 call by the envelope actor, committed only on `Committed` or `Revealed`.
    fn call(&mut self, call: &Req, at: u64) -> CodecResult<F04> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; cr::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = cr::apply(
            &call.context(at)?,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        match outcome {
            F04::Committed { state_len, .. } | F04::Revealed { state_len, .. } => {
                next.truncate(state_len);
                self.world.bytes = next;
            }
            F04::Retained(_) => assert!(event.iter().all(|b| *b == 0)),
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
    /// Evaluator `n` commits `C` of `report` and `SALT` at evaluator sequence 2.
    fn commit(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> TestResult {
        let c = commitment_of(&report.body, SALT)?;
        let call = Market::commit_call(n, &report.body.binding, c, 2, self.start() + 80)?;
        let F04::Committed { record, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!((record.commitment, record.height), (c, at));
        Ok(())
    }
    /// Evaluator `n` reveals `report` and `SALT` at evaluator sequence 3.
    fn reveal(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> TestResult {
        let call = Market::reveal_call(n, report, SALT, 3, self.start() + 96)?;
        let F04::Revealed { receipt, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.admitted(n)?, Some(receipt));
        Ok(())
    }
    /// Evaluator `n`'s signed report over `scores` under its frozen binding.
    fn signed<'a>(&self, n: u8, scores: &'a [u8]) -> CodecResult<SignedReport<'a>> {
        sign(
            Market::body(self.binding(n)?, n, scores)?,
            &evaluator_key(n),
        )
    }
    /// Every evaluator of `plan` commits at T+64; they reveal in plan order from T+80, one
    /// height apart.
    fn admit(&mut self, plan: &[(u8, Vec<u8>)]) -> TestResult {
        let start = self.start();
        for (n, scores) in plan {
            let report = self.signed(*n, scores)?;
            self.commit(*n, &report, start + 64)?;
        }
        for (height, (n, scores)) in (start + 80..).zip(plan) {
            let report = self.signed(*n, scores)?;
            self.reveal(*n, &report, height)?;
        }
        Ok(())
    }
    /// One real owner F03 authority operation bound to the opened epoch. F03 advances only the
    /// shared revision, so the F01 header revision is re-bound to it.
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
        let market = self.world.parts()?.market()?;
        let mut scratch = vec![0; authority::SCRATCH_BYTES];
        let mut out = vec![0; MAX_STATE_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let authority::Outcome::Applied { state_len, .. } = authority::apply(
            &self.world.bytes,
            &AuthorityContext {
                market: &market,
                invoking_principal: PrincipalId::new(OWNER)?,
                immediate_caller: Presence::Absent,
                height: at,
                approved_rubric: rubric()?,
                aggregate_sealed,
            },
            &call.encode()?,
            &mut scratch,
            &mut out,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        self.world.bytes = Parts::load(&out[..state_len])?.encode()?;
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
    /// Real F03 `RotateEvaluatorKey` of evaluator `n` to key version 2, effective next epoch.
    fn rotate(&mut self, n: u8, at: u64) -> TestResult {
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&public(&rotated_key(n)).0);
        for value in [2, 1, self.frozen.epoch + 1] {
            payload.extend_from_slice(&u64::to_be_bytes(value));
        }
        self.owner_authority(dispatch::RotateEvaluatorKey, payload, false, at)
    }
    /// Real F02 `RotateDelegate` of worker `n` by its owner: a replacement generation. F02
    /// advances only the shared revision, so the F01 header revision is re-bound to it.
    fn replace_worker(&mut self, n: u8, at: u64) -> TestResult {
        let market = self.world.parts()?.market()?;
        let owner = principal(n)?;
        let worker = derive_worker(market.market_id, owner, [n; 32])?;
        let record = self.world.parts()?.workers.get(worker).ok_or(NOT_FOUND)?;
        let key = replacement_key(n);
        let metadata = MetadataDigest::new([0x45; 32])?;
        let expiry = at + 100;
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
        let call = Req {
            sequence: 1,
            request: tag(0x25, n, 1),
            ..req(dispatch::RotateDelegate, owner, payload)
        };
        let mut out = vec![0; MAX_STATE_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut scratch = vec![0; CONTROL_SCRATCH_BYTES];
        let f02::Applied::Applied { state_len, .. } = f02::apply(
            &self.world.bytes,
            &f02::CallContext {
                market: &market,
                invoking_principal: owner,
                height: at,
            },
            &call.encode()?,
            &mut out,
            &mut event,
            &mut scratch,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        self.world.bytes = Parts::load(&out[..state_len])?.encode()?;
        Ok(())
    }
}

fn topic(operation: Operation) -> CodecResult<&'static [u8]> {
    if operation == dispatch::BeginAggregation {
        Ok(BEGIN_TOPIC)
    } else if operation == dispatch::ProcessAggregation {
        Ok(PROCESS_TOPIC)
    } else if operation == dispatch::FinalizeAggregation {
        Ok(FINALIZE_TOPIC)
    } else {
        Err(NON_CANONICAL)
    }
}
fn input_of(progress: &Progress) -> CodecResult<Digest32> {
    match progress.input {
        Presence::Present(input) => Ok(input),
        Presence::Absent => Err(NOT_FOUND),
    }
}
fn fields(output: WorkerAggregate) -> (u8, QualityStatus, u32, u32) {
    (
        output.support(),
        output.status(),
        output.score().get(),
        output.weight(),
    )
}
/// Length of the F05 current record at the start of the bytes after the reward state:
/// phase, seal height, input, cursor, reports, outputs, running weight and root presence.
fn record_len(tail: &[u8]) -> CodecResult<usize> {
    let count = |at: usize| -> CodecResult<usize> {
        let bytes = tail.get(at..at + 2).ok_or(NON_CANONICAL)?;
        Ok(usize::from(u16::from_be_bytes([bytes[0], bytes[1]])))
    };
    let outputs_at = 45 + count(43)? * 64;
    let root_at = outputs_at + 2 + count(outputs_at)? * 50 + 8;
    let root = tail.get(root_at).ok_or(NON_CANONICAL)?;
    Ok(root_at + 1 + if *root == 1 { 32 } else { 0 })
}
/// Byte offset of output `index` inside a current record with `reports` sealed rows.
const fn output_at(reports: usize, index: usize) -> usize {
    45 + reports * 64 + 2 + index * 50
}

/// One revision: only the F01 header revision and the joint F05/F06 settlement section change;
/// identity, reports, admission and control bytes are byte-identical, and the operation event
/// carries the response as its suffix.
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
    let mut section = PolicySection::decode(before.feature_sections[0])?;
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    assert_eq!(after.feature_sections[0], policy.as_slice());
    assert_eq!(after.feature_sections[1..3], before.feature_sections[1..3]);
    assert_eq!(after.feature_sections[4], before.feature_sections[4]);
    assert_eq!(after.control.feature_bytes, before.control.feature_bytes);
    assert_eq!(agg::progress(current)?, progress);
    let e = &envelope.envelope;
    let (operation, common, suffix) = codec::decode_event_frame(topic(e.operation)?, event)?;
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
    Ok(())
}

/// F05 aggregation requests, the F05 call and the committed F05 values.
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
    /// One F05 call over the committed bytes into caller buffers of `sizes` (next, scratch,
    /// event); nothing is committed.
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
        let outcome = agg::apply(
            &call.context(at)?,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        );
        Ok((outcome, next, event))
    }
    /// One F05 call; `Applied` is committed after the complete next state and its event are
    /// checked, anything else writes no event and leaves the committed bytes.
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
            Err(_) => assert!(event.iter().all(|b| *b == 0)),
        }
        outcome
    }
    /// A refused F05 call leaves the committed bytes unchanged.
    fn aggregation_refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.aggregate(call, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    fn progress(&self) -> CodecResult<Progress> {
        agg::progress(&self.world.bytes)
    }
    /// A fresh `BeginAggregation` at `at`.
    fn begin(&mut self, at: u64) -> CodecResult<Progress> {
        let call = self.begin_call()?;
        let Agg::Applied {
            progress, response, ..
        } = self.aggregate(&call, at)?
        else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(response.as_bytes().len(), BEGIN_RESPONSE_BYTES);
        assert_eq!(
            (progress.phase, progress.cursor, progress.running_weight),
            (AggregationPhase::Processing, 0, 0)
        );
        Ok(progress)
    }
    /// A fresh `ProcessAggregation` from the committed cursor: at most eight more workers.
    fn process(&mut self, at: u64) -> CodecResult<Progress> {
        let current = self.progress()?;
        let call = self.process_call(input_of(&current)?, current.cursor)?;
        let Agg::Applied {
            progress, response, ..
        } = self.aggregate(&call, at)?
        else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(response.as_bytes().len(), PROCESS_RESPONSE_BYTES);
        assert_eq!(
            (progress.input, progress.cursor),
            (
                current.input,
                (current.cursor + 8).min(current.worker_count)
            )
        );
        Ok(progress)
    }
    /// A fresh `FinalizeAggregation`.
    fn finalize(&mut self, at: u64) -> CodecResult<Progress> {
        let input = input_of(&self.progress()?)?;
        let call = self.finalize_call(input)?;
        let Agg::Applied {
            progress, response, ..
        } = self.aggregate(&call, at)?
        else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(response.as_bytes().len(), FINALIZE_RESPONSE_BYTES);
        assert_eq!(progress.phase, AggregationPhase::Terminal);
        assert_eq!(self.reward_row()?.status, EpochStatus::Terminal);
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
    /// The committed F05 current record and history bytes after the reward state.
    fn tail(&self) -> CodecResult<(Vec<u8>, Vec<u8>)> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        let tail = section.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
        let (record, history) = tail.split_at(record_len(tail)?);
        Ok((record.to_vec(), history.to_vec()))
    }
    /// The persisted outputs, decoded against the frozen roster.
    fn outputs(&self) -> CodecResult<Vec<WorkerAggregate>> {
        let (record, _) = self.tail()?;
        let current = decode_current(&record, self.frozen_binding(), &self.workers)?;
        (0..current.output_count())
            .map(|i| current.output(i))
            .collect()
    }
    fn history(&self) -> CodecResult<Vec<HistorySummary>> {
        let (_, history) = self.tail()?;
        let decoded = decode_history(&history)?;
        (0..decoded.len()).map(|i| decoded.entry(i)).collect()
    }
    fn reward_row(&self) -> CodecResult<RewardEpoch> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?
            .row(self.frozen.epoch)
    }
    /// The input digest of the admitted rows of `evaluators` sealed at `height`.
    fn expected_input(&self, evaluators: &[u8], height: u64) -> CodecResult<Digest32> {
        let mut rows = Vec::new();
        for &n in evaluators {
            let receipt = self.admitted(n)?.ok_or(NOT_FOUND)?;
            rows.push(ReportCommitment {
                evaluator: receipt.evaluator,
                report: receipt.report,
            });
        }
        rows.sort_by_key(|row| row.evaluator);
        input_digest(self.frozen_binding(), height, &rows)
    }
    /// Overwrites persisted F05 record bytes at `at` (relative to the record start) and
    /// commits the corrupted state at the next revision.
    fn tamper(&mut self, at: usize, bytes: &[u8]) -> TestResult {
        self.world.edit(|parts, _| {
            let start = REWARD_STATE_BYTES + at;
            parts
                .rewards
                .get_mut(start..start + bytes.len())
                .ok_or(NOT_FOUND)?
                .copy_from_slice(bytes);
            Ok(())
        })
    }
}

/// One worker scored by each listed evaluator; the other frozen workers receive no vote.
fn single(m: &Market, votes: &[(u8, u32)]) -> Vec<(u8, Vec<u8>)> {
    votes
        .iter()
        .map(|&(n, score)| (n, m.scores(&[(0, score)])))
        .collect()
}

#[test]
fn f05_a01_three_distinct_scores_select_the_median() -> TestResult {
    let mut m = market()?;
    let plan = single(&m, &[(2, 210_000), (3, 620_000), (4, 970_000)]);
    m.admit(&plan)?;
    let free = |m: &Market| -> CodecResult<u128> {
        let state = decode_shared_state(&m.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        Ok(decode_reward_state(&section[..REWARD_STATE_BYTES])?
            .ledger()?
            .free)
    };
    let before = free(&m)?;
    let done = m.settle(SETTLE_AT)?;
    let outputs = m.outputs()?;
    assert_eq!(
        fields(outputs[0]),
        (3, QualityStatus::ScoredPositive, 620_000, 620_000)
    );
    assert_eq!(
        fields(outputs[1]),
        (0, QualityStatus::InsufficientQuorum, 0, 0)
    );
    assert_eq!(done.running_weight, 620_000);
    let row = m.reward_row()?;
    assert_eq!(
        (row.outcome, row.paid_sum, free(&m)?),
        (RewardOutcome::Allocated, 0, before)
    );
    Ok(())
}

#[test]
fn f05_a02_four_scores_select_the_lower_median() -> TestResult {
    let mut m = market()?;
    let plan = single(&m, &[(2, 10_000), (3, 20_000), (4, 900_000), (5, 990_000)]);
    m.admit(&plan)?;
    m.settle(SETTLE_AT)?;
    assert_eq!(
        fields(m.outputs()?[0]),
        (4, QualityStatus::ScoredPositive, 20_000, 20_000)
    );
    Ok(())
}

/// AI.F05-A03 and AI.F04-A07: explicit zeros are votes, two votes are no quorum.
#[test]
fn f05_a03_zero_quorum_and_insufficient_quorum_stay_distinct() -> TestResult {
    let mut m = market()?;
    let plan = vec![
        (2, m.scores(&[(0, 0)])),
        (3, m.scores(&[(0, 0)])),
        (4, m.scores(&[(0, 1_000_000)])),
        (5, m.scores(&[(1, 0)])),
        (6, m.scores(&[(1, 1_000_000)])),
    ];
    m.admit(&plan)?;
    let done = m.settle(SETTLE_AT)?;
    let outputs = m.outputs()?;
    assert_eq!(fields(outputs[0]), (3, QualityStatus::ScoredZero, 0, 0));
    assert_eq!(
        fields(outputs[1]),
        (2, QualityStatus::InsufficientQuorum, 0, 0)
    );
    assert_eq!(
        (outputs[0].quality(), outputs[1].quality()),
        (Presence::Present(outputs[0].score()), Presence::Absent)
    );
    assert_eq!(done.running_weight, 0);
    let row = m.reward_row()?;
    assert_eq!(row.outcome, RewardOutcome::NoEligibleScore);
    assert!(row.entries().iter().all(|entry| entry.entitlement == 0));
    Ok(())
}

#[test]
fn f05_a04_absent_reports_add_no_observations() -> TestResult {
    let mut m = market()?;
    let plan = single(&m, &[(2, 0), (3, 300_000), (4, 700_000)]);
    m.admit(&plan)?;
    m.settle(SETTLE_AT)?;
    assert_eq!(
        fields(m.outputs()?[0]),
        (3, QualityStatus::ScoredPositive, 300_000, 300_000)
    );
    Ok(())
}

#[test]
fn f05_a05_arrival_order_never_changes_bytes_and_seal_height_does() -> TestResult {
    let base = market()?;
    let plan = single(&base, &[(2, 111_111), (3, 111_111), (4, 999_999)]);
    let orders = [
        [0, 1, 2],
        [0, 2, 1],
        [1, 0, 2],
        [1, 2, 0],
        [2, 0, 1],
        [2, 1, 0],
    ];
    let mut results = Vec::new();
    for order in orders {
        let mut m = base.clone();
        let arrivals = order.map(|i| plan[i].clone());
        m.admit(&arrivals)?;
        let done = m.settle(SETTLE_AT)?;
        results.push((m.tail()?, done.input, done.root));
    }
    assert!(results.windows(2).all(|pair| pair[0] == pair[1]));
    let mut later = base.clone();
    later.admit(&plan)?;
    let done = later.settle(SETTLE_AT + 1)?;
    assert_ne!((done.input, done.root), (results[0].1, results[0].2));
    assert_eq!(
        fields(later.outputs()?[0]),
        (3, QualityStatus::ScoredPositive, 111_111, 111_111)
    );
    Ok(())
}

#[test]
fn f05_a06_canonical_worker_order_and_display_shares() -> TestResult {
    let mut m = crowded(3)?;
    let scores = m.scores(&[(0, 250_000), (1, 500_000), (2, 250_000)]);
    let plan = (2..=4).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    m.admit(&plan)?;
    let done = m.settle(m.start() + 96)?;
    let outputs = m.outputs()?;
    let workers = outputs.iter().map(|o| o.worker()).collect::<Vec<_>>();
    let frozen = m.workers.iter().map(|w| w.worker).collect::<Vec<_>>();
    assert_eq!(workers, frozen);
    assert!(workers.windows(2).all(|pair| pair[0] < pair[1]));
    assert_eq!(done.running_weight, 1_000_000);
    let shares = outputs
        .iter()
        .map(|o| display_share_ppm(o.weight(), done.running_weight))
        .collect::<CodecResult<Vec<_>>>()?;
    assert_eq!(
        shares,
        [250_000, 500_000, 250_000].map(Presence::Present).to_vec()
    );
    Ok(())
}

#[test]
fn f05_a07_display_truncation_and_weight_settlement() -> TestResult {
    let mut m = crowded(3)?;
    let scores = m.all(1);
    let plan = (2..=4).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    m.admit(&plan)?;
    let done = m.settle(m.start() + 96)?;
    assert_eq!(done.running_weight, 3);
    let outputs = m.outputs()?;
    let mut displayed = 0;
    for output in &outputs {
        let Presence::Present(share) = display_share_ppm(output.weight(), 3)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(share, 333_333);
        displayed += share;
    }
    assert_eq!(displayed, 999_999);
    let allocation = allocate(m.frozen.budget, &outputs)?;
    let row = m.reward_row()?;
    assert_eq!(row.outcome, RewardOutcome::Allocated);
    for (i, entry) in row.entries().iter().enumerate() {
        assert_eq!(entry.entitlement, allocation.entitlement(i)?.1);
    }
    Ok(())
}

#[test]
fn f05_a08_largest_roster_in_four_chunks() -> TestResult {
    let mut m = crowded(32)?;
    let scores = m.all(1_000_000);
    let plan = (2..=9).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    m.admit(&plan)?;
    let at = m.start() + 96;
    let sealed = m.begin(at)?;
    assert_eq!(sealed.worker_count, 32);
    let mut cursors = Vec::new();
    for _ in 0..4 {
        cursors.push(m.process(at)?.cursor);
    }
    assert_eq!(cursors, [8, 16, 24, 32]);
    let done = m.finalize(at)?;
    assert_eq!(done.running_weight, 32_000_000);
    let outputs = m.outputs()?;
    assert_eq!(outputs.len(), 32);
    assert!(outputs
        .iter()
        .all(|o| fields(*o) == (8, QualityStatus::ScoredPositive, 1_000_000, 1_000_000)));
    Ok(())
}

#[test]
fn f05_a09_no_reports_settles_no_eligible_score() -> TestResult {
    let mut m = market()?;
    let done = m.settle(SETTLE_AT)?;
    assert_eq!(done.running_weight, 0);
    assert!(m
        .outputs()?
        .iter()
        .all(|o| fields(*o) == (0, QualityStatus::InsufficientQuorum, 0, 0)));
    let row = m.reward_row()?;
    assert_eq!(row.outcome, RewardOutcome::NoEligibleScore);
    assert!(row.entries().iter().all(|entry| entry.entitlement == 0));
    assert_eq!(m.history()?.len(), 1);
    Ok(())
}

/// Three evaluators scoring both frozen workers.
fn both(m: &Market) -> Vec<(u8, Vec<u8>)> {
    let scores = m.scores(&[(0, 400_000), (1, 600_000)]);
    (2..=4).map(|n| (n, scores.clone())).collect()
}

#[test]
fn f05_a10_refused_scores_and_tampered_persisted_records() -> TestResult {
    let mut m = market()?;
    let (a, b) = (m.workers[0].worker, m.workers[1].worker);
    let plan = both(&m);
    let report = m.signed(5, &plan[0].1)?;
    m.commit(5, &report, m.start() + 64)?;
    m.admit(&plan)?;
    let template = Market::reveal_call(5, &report, SALT, 3, ROLE_EXPIRY)?;
    for (scores, code) in [
        (entries(&[(a, 1_000_001), (b, 1)]), F03_SCORE_RANGE),
        (entries(&[(a, 1), (a, 1)]), NON_CANONICAL),
    ] {
        let body = raw_body(&report.body, 2, &scores)?;
        let call = Req {
            payload: raw_reveal(&body, &report.signature.0, &SALT)?,
            ..template.clone()
        };
        assert_eq!(call.encode().map(|_| ()), Err(code));
    }
    assert_eq!(m.admitted(5)?, None);
    let sealed = m.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(m.expected_input(&[2, 3, 4], SETTLE_AT)?)
    );
    let input = input_of(&sealed)?;
    let (record, _) = m.tail()?;
    let mut unsorted = m.clone();
    let mut swapped = record[109..173].to_vec();
    swapped.extend_from_slice(&record[45..109]);
    unsorted.tamper(45, &swapped)?;
    let call = unsorted.process_call(input, 0)?;
    assert_eq!(
        unsorted.aggregation_refused(&call, SETTLE_AT)?,
        NON_CANONICAL
    );
    m.process(SETTLE_AT)?;
    let mut unknown = m.clone();
    unknown.tamper(output_at(3, 0) + 32, &2u64.to_be_bytes())?;
    let call = unknown.finalize_call(input)?;
    assert_eq!(
        unknown.aggregation_refused(&call, SETTLE_AT)?,
        F05_REPORT_INVARIANT
    );
    for refused in [&unsorted, &unknown] {
        assert_eq!(refused.reward_row()?.status, EpochStatus::Reserved);
        assert_eq!(refused.tail()?.1, [0, 0]);
    }
    Ok(())
}

#[test]
fn f05_a11_self_assessment_and_unbacked_persisted_votes() -> TestResult {
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
    let plan = both(&m);
    m.admit(&plan)?;
    let sealed = m.begin(SETTLE_AT)?;
    m.process(SETTLE_AT)?;
    let mut unbacked = m.clone();
    unbacked.tamper(output_at(3, 1) + 40, &[8])?;
    let call = unbacked.finalize_call(input_of(&sealed)?)?;
    assert_eq!(
        unbacked.aggregation_refused(&call, SETTLE_AT)?,
        F05_REPORT_INVARIANT
    );
    assert_eq!(unbacked.reward_row()?.status, EpochStatus::Reserved);
    m.finalize(SETTLE_AT)?;
    assert_eq!(fields(m.outputs()?[1]).0, 3);
    Ok(())
}

/// AI.F05-A12 and the rollover part of AI.F04-A12.
#[test]
fn f05_a12_settlement_window_and_delayed_completion() -> TestResult {
    let mut m = market()?;
    let plan = both(&m);
    let late = m.signed(5, &plan[0].1)?;
    m.commit(5, &late, m.start() + 64)?;
    m.admit(&plan)?;
    let begin = m.begin_call()?;
    assert_eq!(m.aggregation_refused(&begin, SETTLE_AT - 1)?, WRONG_PHASE);
    let reveal = Market::reveal_call(5, &late, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&reveal, SETTLE_AT)?, WRONG_PHASE);
    let sealed = m.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(m.expected_input(&[2, 3, 4], SETTLE_AT)?)
    );
    let mut paused = m.clone();
    paused.world.suspend(SETTLE_AT + 1)?;
    paused.complete(SETTLE_AT + 2)?;
    let delayed = m.start() + 128 + 40;
    let before = m.world.bytes.clone();
    assert_eq!(m.world.open(delayed).err(), Some(WRONG_PHASE));
    assert_eq!(m.world.bytes, before);
    let done = m.complete(delayed)?;
    assert_eq!(done.input, sealed.input);
    let stale = m.finalize_call(input_of(&done)?)?;
    let epoch7 = m.frozen;
    m.frozen = m.world.open(delayed)?;
    assert_eq!(m.frozen.epoch, epoch7.epoch + 1);
    let next = m.progress()?;
    assert_eq!(
        (next.epoch, next.phase, next.cursor),
        (8, AggregationPhase::Unsealed, 0)
    );
    assert_eq!(m.aggregation_refused(&stale, delayed + 1)?, WRONG_EPOCH);
    let history = m.history()?;
    assert_eq!(history.len(), 1);
    assert_eq!(
        (history[0].epoch, Presence::Present(history[0].root)),
        (7, done.root)
    );
    Ok(())
}

#[test]
fn f05_a13_cursor_retries_and_once_only_completion() -> TestResult {
    let mut m = crowded(13)?;
    let scores = m.all(500_000);
    let plan = (2..=4).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    m.admit(&plan)?;
    let at = m.start() + 96;
    let sealed = m.begin(at)?;
    assert_eq!((sealed.cursor, sealed.worker_count), (0, 13));
    let begin = m.begin_call()?;
    let Agg::AlreadyApplied { progress, response } = m.aggregate(&begin, at + 1)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (progress, response.as_bytes().len()),
        (sealed, BEGIN_RESPONSE_BYTES)
    );
    let input = input_of(&sealed)?;
    assert_eq!(m.process(at)?.running_weight, 4_000_000);
    let retry = m.process_call(input, 0)?;
    assert_eq!(m.aggregation_refused(&retry, at + 1)?, STALE_CURSOR);
    assert_eq!((m.progress()?.cursor, m.outputs()?.len()), (8, 8));
    let early = m.finalize_call(input)?;
    assert_eq!(m.aggregation_refused(&early, at + 1)?, WRONG_PHASE);
    assert_eq!(m.process(at + 2)?.cursor, 13);
    let complete = m.process_call(input, 13)?;
    assert!(matches!(
        m.aggregate(&complete, at + 2)?,
        Agg::AlreadyApplied { .. }
    ));
    let done = m.finalize(at + 3)?;
    let terminal = m.world.bytes.clone();
    let finalize = m.finalize_call(input)?;
    let Agg::AlreadyApplied { progress, response } = m.aggregate(&finalize, at + 4)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (progress, response.as_bytes().len()),
        (done, FINALIZE_RESPONSE_BYTES)
    );
    assert_eq!(m.world.bytes, terminal);
    assert_eq!(m.history()?.len(), 1);
    let binding = m.frozen_binding();
    let variants = [
        (
            Req {
                epoch: binding.epoch + 1,
                ..finalize.clone()
            },
            WRONG_EPOCH,
        ),
        (
            Req {
                config: binding.config.get() + 1,
                ..finalize.clone()
            },
            WRONG_CONFIG,
        ),
        (
            Req {
                roster: Presence::Present(RosterDigest::new([0x77; 32])?),
                ..finalize.clone()
            },
            WRONG_ROSTER,
        ),
        (m.finalize_call(Digest32::new([0x66; 32])?)?, CONFLICT),
        (m.process_call(Digest32::new([0x66; 32])?, 13)?, CONFLICT),
    ];
    for (call, code) in variants {
        assert_eq!(m.aggregation_refused(&call, at + 5)?, code);
    }
    Ok(())
}

/// AI.F05-A14 and the revocation and rotation parts of AI.F04-A12, in both authoritative
/// orderings of revocation and seal.
#[test]
fn f05_a14_revocation_orderings() -> TestResult {
    let mut m = market()?;
    let scores = m.scores(&[(0, 400_000), (1, 600_000)]);
    let plan = (2..=5).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    let pending = m.signed(6, &scores)?;
    m.commit(6, &pending, m.start() + 64)?;
    m.admit(&plan)?;
    m.revoke(6, m.start() + 86)?;
    let call = Market::reveal_call(6, &pending, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, m.start() + 87)?, REVOKED);
    m.rotate(3, m.start() + 88)?;
    let mut before_seal = m.clone();
    before_seal.revoke(4, m.start() + 89)?;
    let sealed = before_seal.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(before_seal.expected_input(&[2, 3, 5], SETTLE_AT)?)
    );
    before_seal.complete(SETTLE_AT)?;
    assert_eq!(fields(before_seal.outputs()?[0]).0, 3);
    let sealed = m.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(m.expected_input(&[2, 3, 4, 5], SETTLE_AT)?)
    );
    m.revoke(4, SETTLE_AT + 1)?;
    m.process(SETTLE_AT + 2)?;
    m.revoke(5, SETTLE_AT + 3)?;
    let done = m.finalize(SETTLE_AT + 4)?;
    assert_eq!(done.input, sealed.input);
    assert_eq!(fields(m.outputs()?[0]).0, 4);
    let terminal = m.tail()?;
    m.revoke(2, SETTLE_AT + 5)?;
    assert_eq!((m.tail()?, m.progress()?), (terminal, done));
    Ok(())
}

#[test]
fn f05_a15_worker_replacement_after_freeze() -> TestResult {
    let mut m = market()?;
    let plan = both(&m);
    m.admit(&plan)?;
    let mut sealed_first = m.clone();
    let sealed = sealed_first.begin(SETTLE_AT)?;
    m.replace_worker(LOW, 1110)?;
    let begin = m.begin_call()?;
    assert_eq!(m.aggregation_refused(&begin, SETTLE_AT)?, WRONG_ROSTER);
    sealed_first.replace_worker(LOW, SETTLE_AT + 1)?;
    let call = sealed_first.process_call(input_of(&sealed)?, 0)?;
    assert_eq!(
        sealed_first.aggregation_refused(&call, SETTLE_AT + 2)?,
        WRONG_ROSTER
    );
    for replaced in [&m, &sealed_first] {
        assert_eq!(replaced.reward_row()?.status, EpochStatus::Reserved);
        assert_eq!(replaced.progress()?.cursor, 0);
    }
    assert!(m.tail().is_err());
    assert!(sealed_first.history()?.is_empty());
    Ok(())
}

/// The storage and event refusal parts of AI.F05-A16 through the real buffers of the call,
/// and a real F06 terminalization refusal. The native fuel meter is the host boundary's.
#[test]
fn f05_a16_refused_steps_keep_committed_progress() -> TestResult {
    let mut m = crowded(13)?;
    let scores = m.all(500_000);
    let plan = (2..=4).map(|n| (n, scores.clone())).collect::<Vec<_>>();
    m.admit(&plan)?;
    let at = m.start() + 96;
    let sealed = m.begin(at)?;
    m.process(at)?;
    let committed = m.world.bytes.clone();
    let call = m.process_call(input_of(&sealed)?, 8)?;
    let [next, scratch, event] = FULL;
    for sizes in [
        [m.world.bytes.len(), scratch, event],
        [
            next,
            Section::PolicyLifecycle.payload_cap() + REWARD_STATE_BYTES,
            event,
        ],
        [next, scratch, 64],
    ] {
        let (outcome, _, _) = m.compose(&call, at, sizes)?;
        assert_eq!(outcome, Err(CAPACITY));
        assert_eq!(m.world.bytes, committed);
        assert_eq!((m.progress()?.cursor, m.outputs()?.len()), (8, 8));
    }
    let mut terminalized = m.clone();
    assert_eq!(m.process(at)?.cursor, 13);
    let complete = m.clone();
    let frozen = terminalized.frozen;
    let workers = terminalized.workers.clone();
    terminalized.process(at)?;
    terminalized.world.terminalize(&frozen, &workers, at + 1)?;
    let before = terminalized.tail()?;
    let call = terminalized.finalize_call(input_of(&sealed)?)?;
    assert_eq!(
        terminalized.aggregation_refused(&call, at + 2)?,
        F06_EPOCH_TERMINAL
    );
    assert_eq!(terminalized.tail()?, before);
    assert_eq!(before.1, [0, 0]);
    assert_eq!(terminalized.progress()?.phase, AggregationPhase::Processing);
    assert_eq!(complete.progress()?.cursor, 13);
    Ok(())
}

#[test]
fn f05_a17_colluding_low_reports_select_zero() -> TestResult {
    let mut m = market()?;
    let plan = single(&m, &[(2, 0), (3, 0), (4, 900_000), (5, 1_000_000)]);
    m.admit(&plan)?;
    m.settle(SETTLE_AT)?;
    assert_eq!(
        fields(m.outputs()?[0]),
        (4, QualityStatus::ScoredZero, 0, 0)
    );
    Ok(())
}

/// The sealed evidence roots name offchain artifacts nobody can fetch: replacing every sealed
/// root changes no aggregation byte, so settlement reads no evidence.
#[test]
fn f05_a18_settlement_reads_no_evidence() -> TestResult {
    let mut m = market()?;
    let plan = both(&m);
    m.admit(&plan)?;
    let mut unreachable = m.clone();
    unreachable.world.edit(|parts, _| {
        for n in 2..=7 {
            let sealed = [root(n); 32];
            let at = parts
                .features
                .windows(32)
                .position(|window| window == sealed)
                .ok_or(NOT_FOUND)?;
            parts.features[at..at + 32].copy_from_slice(&[0xd0 + n; 32]);
        }
        Ok(())
    })?;
    let done = m.settle(SETTLE_AT)?;
    let other = unreachable.settle(SETTLE_AT)?;
    assert_eq!((m.tail()?, done), (unreachable.tail()?, other));
    Ok(())
}

#[test]
fn f04_a06_expired_commitments_supply_no_rows() -> TestResult {
    let mut c = crowded(2)?;
    let start = c.start();
    let scores = c.scores(&[(0, 400_000), (1, 600_000)]);
    let reports = (2..=9)
        .map(|n| c.signed(n, &scores))
        .collect::<CodecResult<Vec<_>>>()?;
    for (n, report) in (2..=9).zip(&reports) {
        c.commit(n, report, start + 64)?;
    }
    c.reveal(2, &reports[0], start + 80)?;
    c.reveal(3, &reports[1], start + 95)?;
    let late = Market::reveal_call(4, &reports[2], SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(c.refused(&late, start + 96)?, WRONG_PHASE);
    let sealed = c.begin(start + 96)?;
    assert_eq!(
        sealed.input,
        Presence::Present(c.expected_input(&[2, 3], start + 96)?)
    );
    let done = c.complete(start + 96)?;
    assert!(c
        .outputs()?
        .iter()
        .all(|o| fields(*o) == (2, QualityStatus::InsufficientQuorum, 0, 0)));
    assert_eq!(done.running_weight, 0);
    assert_eq!(c.reward_row()?.outcome, RewardOutcome::NoEligibleScore);
    Ok(())
}

#[test]
fn f04_a11_stable_set_at_reveal_end() -> TestResult {
    let mut m = market()?;
    let plan = both(&m);
    let delayed = m.signed(5, &plan[0].1)?;
    m.commit(5, &delayed, m.start() + 64)?;
    m.admit(&plan)?;
    let call = Market::reveal_call(5, &delayed, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_END)?, WRONG_PHASE);
    let mut projected = m.clone();
    assert_eq!(projected.slot(5, REVEAL_END)?.status, Status::Expired);
    assert_eq!(projected.world.bytes, m.world.bytes);
    let quiet = m.begin(SETTLE_AT)?;
    let observed = projected.begin(SETTLE_AT)?;
    assert_eq!((quiet, &m.world.bytes), (observed, &projected.world.bytes));
    assert_eq!(
        quiet.input,
        Presence::Present(m.expected_input(&[2, 3, 4], SETTLE_AT)?)
    );
    Ok(())
}
