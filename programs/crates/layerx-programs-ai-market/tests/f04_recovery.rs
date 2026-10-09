//! AI.F04-T03 recovery of commitment secrets and uncertain reveals. Every market is produced by
//! the real F01/F02/F03/F06/F08/F09 producers, `OPEN_EPOCH` and the F01 task-set lifecycle; every
//! `CommitScore` and `RevealScore` is routed through `dispatch::route` with real envelopes and
//! real ed25519 evaluator keys, and only an `Applied` decision is finalized into the committed
//! state. The client side keeps a durable local journal: a per-report salt drawn from the OS
//! CSPRNG, the signed report and every envelope are written, synced and read back before they
//! are sent, and recovery after a crash or a dropped response reads only the journal and the
//! finalized state. Epoch 7 settles through the real F05 begin/process/finalize transitions
//! before `OPEN_EPOCH` rolls the market into epoch 8.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{self as agg, Outcome as Agg, Progress},
    aggregation_codec::{
        decode_history, input_digest, AggregationPhase, EpochAggregation, QualityStatus,
        ReportCommitment, WorkerAggregate,
    },
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        ApplicationResult, CommitScorePayload, Envelope, ReportBody, ResultStatus,
        RevealScorePayload, ScoreVector,
    },
    commit_reveal::{self as cr, commitment, CommitRecord, Status},
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, CONFLICT, EXPIRED, F04_COMMIT_MISMATCH, NON_CANONICAL,
        NOT_FOUND, REPLAY_CONFLICT, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::{
        admission::{self as f03, AdmissionReceipt, ReportRegion},
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        codec::{decode_signed_report, encode_signed_report},
        model::{EvaluatorGrant, GrantTerms, SignedReport, SIGNED_REPORT_MAX_BYTES},
    },
    evidence::{self, Outcome as Sealing, SEAL_SCRATCH_BYTES},
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reward_math::allocate,
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
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvidenceRoot, FrozenBinding, MarketId, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Salt32, Signature64, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use std::{
    fmt,
    fs::{self, File},
    io::{self, Read, Write},
    path::{Path, PathBuf},
    process,
};

type TestResult = CodecResult<()>;
type Checked = Result<(), Failure>;

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
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120),
/// settlement from 1120.
const COMMIT_AT: u64 = 1088;
const REVEAL_AT: u64 = 1104;
const REVEAL_END: u64 = 1120;
const SETTLE_AT: u64 = 1120;
/// Epoch 8: T = 1152, commit [1216, 1232), reveal [1232, 1248).
const ROLLOVER_AT: u64 = 1152;
const NEXT_COMMIT_AT: u64 = 1216;
const NEXT_REVEAL_AT: u64 = 1232;
/// The two frozen workers of epoch 7.
const LOW: u8 = 0x20;
const HIGH: u8 = 0x21;
/// Journal entries of one report.
const SALT_FILE: &str = "salt";
const REPORT_FILE: &str = "report";
const COMMIT_FILE: &str = "commit";
const REVEAL_FILE: &str = "reveal";

/// A failed test step: a canonical application refusal or a client journal I/O error.
enum Failure {
    App(ApplicationError),
    Io(io::Error),
}
impl fmt::Debug for Failure {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::App(error) => write!(f, "application refusal {error:?}"),
            Self::Io(error) => write!(f, "journal i/o error {error}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::App(error)
    }
}
impl From<io::Error> for Failure {
    fn from(error: io::Error) -> Self {
        Self::Io(error)
    }
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
/// The evidence root evaluator `n` seals in `epoch`.
fn root(n: u8, epoch: u64) -> [u8; 32] {
    let mut out = [0xe0 + n; 32];
    out[24..].copy_from_slice(&epoch.to_be_bytes());
    out
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
    delegate: Option<(SigningKey, SigningKey)>,
    signed: Option<Vec<u8>>,
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
        delegate: None,
        signed: None,
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
        let mut envelope = Envelope {
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
            payload: self.signed.as_deref().unwrap_or(&self.payload),
            authentication: Authentication::Native,
        };
        let mut out = vec![0; 32_768];
        if let Some((claimed, signer)) = &self.delegate {
            envelope.authentication = Authentication::Delegate {
                key: public(claimed),
                signature: Signature64([0; 64]),
            };
            let n = encode_envelope(&envelope, &mut out)?;
            let digest = decode_envelope(&out[..n])?.request_digest()?;
            envelope.authentication = Authentication::Delegate {
                key: public(claimed),
                signature: Signature64(signer.sign(digest.as_bytes()).to_bytes()),
            };
        }
        envelope.payload = &self.payload;
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

/// The evaluator replay slot bound to `owner`.
fn evaluator_slot(replay: &ReplayTable, owner: PrincipalId) -> CodecResult<ActorSlot> {
    for index in 0..8 {
        let slot = ActorSlot::evaluator(index)?;
        if replay
            .actor(slot)
            .is_some_and(|actor| actor.principal == owner)
        {
            return Ok(slot);
        }
    }
    Err(NOT_FOUND)
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
    /// F05 terminal result for every frozen worker, then `TerminalizeRewards`.
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
            let mut terminal = vec![0; REWARD_STATE_BYTES];
            decode_reward_state(&parts.rewards)?.terminalize(
                &binding,
                aggregation.root(),
                &allocation,
                roster,
                at,
                &mut terminal,
            )?;
            parts.rewards = terminal;
            Ok(())
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
/// activation, epoch 6 opened at 896, worker `HIGH` and evaluators 5..=7 enrolled in epoch 6,
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
    let high = m.world.enroll(HIGH, 900)?;
    for n in 5..=7 {
        m.world.evaluator(n, 896 + u64::from(n))?;
    }
    m.seal(&[], 960, 1)?;
    m.world.terminalize(&first, &[low], 1008)?;
    m.frozen = m.world.open(1024)?;
    m.workers.push(high);
    m.workers.sort_by_key(|w| w.worker);
    assert_eq!(
        (m.frozen.epoch, m.frozen.workers, m.frozen.evaluators),
        (7, 2, 6)
    );
    m.seal(&[2, 3, 4, 5, 6, 7], COMMIT_AT, 1)?;
    Ok(m)
}

/// Identities, bindings, reports and the task-set and evidence seals of the opened epoch.
impl Market {
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
            evidence: EvidenceRoot::new(root(n, binding.frozen.epoch))?,
            scores: ScoreVector::Encoded(scores),
        })
    }
    /// Scores for the two lowest frozen workers, in worker order.
    fn both(&self, first: u32, second: u32) -> Vec<u8> {
        entries(&[
            (self.workers[0].worker, first),
            (self.workers[1].worker, second),
        ])
    }
    fn slot(&self, n: u8, at: u64) -> CodecResult<cr::Slot> {
        cr::slot(&self.world.bytes, self.evaluator(n)?, at)
    }
    fn admitted(&self, n: u8) -> CodecResult<Option<AdmissionReceipt>> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?.map(|r| r.receipt))
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's task region, then `SealEvidence` of each
    /// of `evaluators` under evaluator `sequence`.
    fn seal(&mut self, evaluators: &[u8], at: u64, sequence: u64) -> TestResult {
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
            claim.extend_from_slice(&root(n, frozen.epoch));
            claim.extend_from_slice(self.frozen.policy.as_bytes());
            claim.extend_from_slice(sealed.as_bytes());
            claim.extend_from_slice(rubric()?.as_bytes());
            claim.push(1);
            let call = Req {
                sequence,
                request: tag(0xc5, n, sequence),
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

/// `CommitScore` and `RevealScore` requests and the F03 owner authority operations.
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
    /// Evaluator `n`'s signed report over `scores` under its frozen binding.
    fn signed<'a>(&self, n: u8, scores: &'a [u8]) -> CodecResult<SignedReport<'a>> {
        sign(
            Market::body(self.binding(n)?, n, scores)?,
            &evaluator_key(n),
        )
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
    /// Real F03 `RevokeEvaluator` of evaluator `n` by the market owner.
    fn revoke(&mut self, n: u8, aggregate_sealed: bool, at: u64) -> TestResult {
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.extend_from_slice(&1u16.to_be_bytes());
        payload.extend_from_slice(&[0x5e; 32]);
        self.owner_authority(dispatch::RevokeEvaluator, payload, aggregate_sealed, at)
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
}

/// The client's durable local record of one report. Every entry is written to a staged file,
/// synced, renamed into place with the directory synced, and read back before anything that
/// depends on it is sent.
struct Journal {
    dir: PathBuf,
}
impl Journal {
    fn open(name: &str) -> Result<Self, Failure> {
        let dir = Path::new(env!("CARGO_TARGET_TMPDIR"))
            .join(format!("f04-recovery-{}-{name}", process::id()));
        if dir.exists() {
            fs::remove_dir_all(&dir)?;
        }
        fs::create_dir_all(&dir)?;
        Ok(Self { dir })
    }
    fn persist(&self, entry: &str, bytes: &[u8]) -> Checked {
        let staged = self.dir.join(format!("{entry}.staged"));
        let mut file = File::create(&staged)?;
        file.write_all(bytes)?;
        file.sync_all()?;
        fs::rename(&staged, self.dir.join(entry))?;
        File::open(&self.dir)?.sync_all()?;
        assert_eq!(self.load(entry)?, bytes);
        Ok(())
    }
    fn load(&self, entry: &str) -> Result<Vec<u8>, Failure> {
        Ok(fs::read(self.dir.join(entry))?)
    }
    fn forget(&self, entry: &str) -> Checked {
        fs::remove_file(self.dir.join(entry))?;
        File::open(&self.dir)?.sync_all()?;
        Ok(())
    }
}
impl Drop for Journal {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// A fresh 32-byte salt from the operating system CSPRNG; a zero draw is refused.
fn os_salt() -> Result<[u8; 32], Failure> {
    let mut salt = [0; 32];
    File::open("/dev/urandom")?.read_exact(&mut salt)?;
    Salt32::new(salt)?;
    Ok(salt)
}
/// Client step one: draws a fresh OS salt for this report and persists the salt, the signed
/// report and the exact `CommitScore` envelope at `sequence` before anything is sent.
fn prepare(
    journal: &Journal,
    n: u8,
    report: &SignedReport<'_>,
    sequence: u64,
    expiry: u64,
) -> Checked {
    let salt = os_salt()?;
    let mut signed = vec![0; SIGNED_REPORT_MAX_BYTES];
    let len = encode_signed_report(report, &mut signed)?;
    journal.persist(SALT_FILE, &salt)?;
    journal.persist(REPORT_FILE, &signed[..len])?;
    let c = commitment_of(&report.body, salt)?;
    let call = Market::commit_call(n, &report.body.binding, c, sequence, expiry)?;
    journal.persist(COMMIT_FILE, &call.encode()?)?;
    Ok(())
}
/// The persisted salt and signed report bytes; recovery is refused without the salt.
fn saved(journal: &Journal) -> Result<([u8; 32], Vec<u8>), Failure> {
    let salt = <[u8; 32]>::try_from(journal.load(SALT_FILE)?).map_err(|_| NON_CANONICAL)?;
    Salt32::new(salt)?;
    Ok((salt, journal.load(REPORT_FILE)?))
}
/// Client step two, after a restart: rebuilds the reveal from the journal alone, checks the
/// saved report and salt against the finalized C of evaluator `n` and persists the exact
/// `RevealScore` envelope at `sequence` before it is sent.
fn recover(
    journal: &Journal,
    m: &Market,
    n: u8,
    sequence: u64,
    expiry: u64,
    at: u64,
) -> Result<Vec<u8>, Failure> {
    let (salt, signed) = saved(journal)?;
    let report = decode_signed_report(&signed)?;
    let record = m.slot(n, at)?.commit.ok_or(NOT_FOUND)?;
    if commitment_of(&report.body, salt)? != record.commitment {
        return Err(F04_COMMIT_MISMATCH.into());
    }
    let call = Market::reveal_call(n, &report, salt, sequence, expiry)?;
    journal.persist(REVEAL_FILE, &call.encode()?)?;
    journal.load(REVEAL_FILE)
}
/// The commit record a first acceptance of the journaled commitment at `height` and
/// `sequence` stores.
fn expected_commit(journal: &Journal, height: u64, sequence: u64) -> Result<CommitRecord, Failure> {
    let (salt, signed) = saved(journal)?;
    let report = decode_signed_report(&signed)?;
    let binding = report.body.binding;
    Ok(CommitRecord {
        evaluator: binding.evaluator,
        commitment: commitment_of(&report.body, salt)?,
        height,
        sequence,
        grant: binding.grant,
        key_version: binding.key_version,
    })
}
/// The receipt a first admission of the journaled report at `height` and `activity` stores.
fn expected_reveal(
    journal: &Journal,
    height: u64,
    activity: u64,
) -> Result<AdmissionReceipt, Failure> {
    let (_, signed) = saved(journal)?;
    let report = decode_signed_report(&signed)?;
    Ok(AdmissionReceipt {
        evaluator: report.body.binding.evaluator,
        report: codec::report_digest(&report.body)?,
        height,
        activity,
    })
}

/// One call delivered to the router: its decision, the caller buffers and the request digest.
struct Delivery {
    routed: Routed,
    next: Vec<u8>,
    event: Vec<u8>,
    frame: Vec<u8>,
    request: RequestDigest,
}
impl Delivery {
    fn result(&self) -> CodecResult<ApplicationResult<'_>> {
        codec::decode_result(&self.frame)
    }
    /// An `Ok` frame carrying `payload` at the routed revision; returns that revision.
    fn applied(&self, payload: &[u8]) -> CodecResult<u64> {
        let Routed::Applied {
            revision,
            event_len,
            ..
        } = self.routed
        else {
            return Err(NON_CANONICAL);
        };
        let result = self.result()?;
        assert_eq!(
            (
                result.status,
                result.error,
                result.request,
                result.revision,
                result.payload
            ),
            (
                ResultStatus::Ok,
                None,
                Presence::Present(self.request),
                revision,
                payload
            )
        );
        assert_eq!(result.digest, codec::result_digest(payload)?);
        assert_eq!(self.event.len(), event_len);
        Ok(revision)
    }
    /// An `AlreadyApplied` frame of the retained `revision` and result digest, without an event.
    fn retained(&self, revision: u64, digest: ResultDigest) -> TestResult {
        assert!(matches!(self.routed, Routed::Unchanged { .. }));
        let result = self.result()?;
        assert_eq!(
            (
                result.status,
                result.error,
                result.request,
                result.revision,
                result.payload
            ),
            (
                ResultStatus::AlreadyApplied,
                None,
                Presence::Present(self.request),
                revision,
                &digest.bytes()[..]
            )
        );
        assert!(self.event.iter().all(|b| *b == 0));
        Ok(())
    }
    /// The error frame of `code` at the visible `revision`, without an event.
    fn refused(&self, code: ApplicationError, revision: u64) -> TestResult {
        assert!(matches!(self.routed, Routed::Refused { .. }));
        let result = self.result()?;
        assert_eq!(
            (result.status, result.error, result.request, result.revision),
            (
                ResultStatus::Error,
                Some(code),
                Presence::Present(self.request),
                revision
            )
        );
        assert_eq!(
            result.digest,
            codec::error_digest(code, Presence::Present(self.request))?
        );
        assert!(self.event.iter().all(|b| *b == 0));
        Ok(())
    }
}

/// Routed F04 calls and the finalized state.
impl Market {
    /// Routes `envelope` of evaluator `n` at `at` over the committed bytes; nothing is
    /// finalized, so the response may still be lost.
    fn submit(&self, envelope: &[u8], n: u8, at: u64) -> CodecResult<Delivery> {
        let request = decode_envelope(envelope)?.request_digest()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut frame = vec![0; MAX_RESULT_BYTES];
        let ctx = CallContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            principal: principal(n)?,
            height: at,
        };
        let routed = dispatch::route(
            &ctx,
            envelope,
            Some(&self.world.bytes),
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut frame,
            },
        )?;
        match routed {
            Routed::Applied {
                state_len,
                event_len,
                result_len,
                ..
            } => {
                next.truncate(state_len);
                event.truncate(event_len);
                frame.truncate(result_len);
            }
            Routed::Unchanged { result_len } | Routed::Refused { result_len } => {
                next.clear();
                frame.truncate(result_len);
            }
        }
        Ok(Delivery {
            routed,
            next,
            event,
            frame,
            request,
        })
    }
    /// Finalizes an `Applied` delivery composed over the current committed revision.
    fn land(&mut self, delivery: &Delivery) -> TestResult {
        let Routed::Applied { revision, .. } = delivery.routed else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(revision, self.world.revision()? + 1);
        assert_eq!(decode_shared_state(&delivery.next)?.revision, revision);
        self.world.bytes.clone_from(&delivery.next);
        Ok(())
    }
    /// Routes `envelope` and finalizes it when it is `Applied`; the response is then the only
    /// thing that can still be lost.
    fn deliver(&mut self, envelope: &[u8], n: u8, at: u64) -> CodecResult<Delivery> {
        let delivery = self.submit(envelope, n, at)?;
        if matches!(delivery.routed, Routed::Applied { .. }) {
            self.land(&delivery)?;
        }
        Ok(delivery)
    }
    /// The canonical signed report bytes of evaluator `n`'s admitted row in the opened epoch.
    fn admitted_bytes(&self, n: u8) -> CodecResult<Option<Vec<u8>>> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(
            f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?
                .map(|row| row.signed_bytes().to_vec()),
        )
    }
    /// The last request sequence retained for evaluator `n`.
    fn last_sequence(&self, n: u8) -> CodecResult<u64> {
        let replay = decode_shared_state(&self.world.bytes)?.control.replay;
        let slot = evaluator_slot(&replay, principal(n)?)?;
        Ok(replay
            .actor(slot)
            .and_then(|actor| actor.last)
            .ok_or(NOT_FOUND)?
            .sequence)
    }
}

/// Real F05 settlement of the opened epoch through `aggregation::apply`; F05 selectors are not
/// routed yet.
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
    /// One F05 call, finalized only on `Applied`.
    fn aggregate(&mut self, call: &Req, at: u64) -> CodecResult<Progress> {
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; agg::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Agg::Applied {
            progress,
            state_len,
            ..
        } = agg::apply(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        next.truncate(state_len);
        self.world.bytes = next;
        Ok(progress)
    }
    fn progress(&self) -> CodecResult<Progress> {
        agg::progress(&self.world.bytes)
    }
    /// `BeginAggregation` seals the eligible admitted rows at `at`.
    fn begin(&mut self, at: u64) -> CodecResult<Progress> {
        let call = self.aggregation_call(dispatch::BeginAggregation, Vec::new(), 0)?;
        let progress = self.aggregate(&call, at)?;
        assert_eq!(
            (progress.phase, progress.cursor),
            (AggregationPhase::Processing, 0)
        );
        Ok(progress)
    }
    /// Every `ProcessAggregation` chunk, then `FinalizeAggregation`; the F06 row of the epoch
    /// becomes terminal.
    fn complete(&mut self, at: u64) -> CodecResult<Progress> {
        let mut progress = self.progress()?;
        while progress.cursor < progress.worker_count {
            let input = input_of(&progress)?;
            let mut payload = input.as_bytes().to_vec();
            payload.extend_from_slice(&progress.cursor.to_be_bytes());
            let call =
                self.aggregation_call(dispatch::ProcessAggregation, payload, progress.cursor)?;
            progress = self.aggregate(&call, at)?;
        }
        let input = input_of(&progress)?.as_bytes().to_vec();
        let call = self.aggregation_call(dispatch::FinalizeAggregation, input, u16::MAX)?;
        let done = self.aggregate(&call, at)?;
        assert_eq!(done.phase, AggregationPhase::Terminal);
        assert_eq!(
            self.reward_row(self.frozen.epoch)?.status,
            EpochStatus::Terminal
        );
        Ok(done)
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
    /// The F05 history after the current record: (epoch, root) per retained epoch.
    fn history(&self) -> CodecResult<Vec<(u64, Digest32)>> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        let tail = section.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
        let (_, history) = tail.split_at(record_len(tail)?);
        let decoded = decode_history(history)?;
        (0..decoded.len())
            .map(|i| decoded.entry(i).map(|entry| (entry.epoch, entry.root)))
            .collect()
    }
    fn reward_row(&self, epoch: u64) -> CodecResult<RewardEpoch> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?.row(epoch)
    }
}

fn input_of(progress: &Progress) -> CodecResult<Digest32> {
    match progress.input {
        Presence::Present(input) => Ok(input),
        Presence::Absent => Err(NOT_FOUND),
    }
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

#[test]
fn a09_client_salts_are_os_drawn_per_report_and_persisted_before_send() -> Checked {
    let m = market()?;
    let scores = m.both(250_000, 750_000);
    let journals = [Journal::open("salt-2")?, Journal::open("salt-3")?];
    for (n, journal) in [2, 3].into_iter().zip(&journals) {
        let report = m.signed(n, &scores)?;
        prepare(journal, n, &report, 2, REVEAL_AT)?;
        let (salt, signed) = saved(journal)?;
        let mut canonical = vec![0; SIGNED_REPORT_MAX_BYTES];
        let len = encode_signed_report(&report, &mut canonical)?;
        assert_eq!(signed, &canonical[..len]);
        let envelope = journal.load(COMMIT_FILE)?;
        let decoded = decode_envelope(&envelope)?;
        assert_eq!(
            (
                decoded.envelope.actor,
                decoded.envelope.sequence,
                decoded.envelope.expiry
            ),
            (principal(n)?, 2, REVEAL_AT)
        );
        assert_eq!(
            commitment::decode_commit_score(decoded.envelope.payload)?,
            CommitScorePayload {
                binding: report.body.binding,
                commitment: commitment_of(&report.body, salt)?,
            }
        );
    }
    let [(first, _), (second, _)] = [saved(&journals[0])?, saved(&journals[1])?];
    assert_ne!(first, second);
    assert_ne!(first, os_salt()?);
    journals[0].forget(SALT_FILE)?;
    let Err(Failure::Io(lost)) = saved(&journals[0]) else {
        return Err(NON_CANONICAL.into());
    };
    assert_eq!(lost.kind(), io::ErrorKind::NotFound);
    Ok(())
}

#[test]
fn a09_crash_after_finalized_commit_reveals_from_the_journal() -> Checked {
    let mut m = market()?;
    let journal = Journal::open("crash")?;
    {
        let scores = m.both(250_000, 750_000);
        let report = m.signed(2, &scores)?;
        prepare(&journal, 2, &report, 2, REVEAL_AT)?;
        let envelope = journal.load(COMMIT_FILE)?;
        let unfinalized = m.submit(&envelope, 2, COMMIT_AT)?;
        unfinalized.applied(&expected_commit(&journal, COMMIT_AT, 2)?.payload()?)?;
        let slot = m.slot(2, COMMIT_AT)?;
        assert_eq!(
            (slot.status, slot.commit, slot.reveal),
            (Status::Absent, None, None)
        );
        let landed = m.deliver(&envelope, 2, COMMIT_AT + 1)?;
        landed.applied(&expected_commit(&journal, COMMIT_AT + 1, 2)?.payload()?)?;
    }
    let committed = expected_commit(&journal, COMMIT_AT + 1, 2)?;
    let slot = m.slot(2, COMMIT_AT + 2)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Committed, Some(committed), None)
    );
    let reveal = recover(&journal, &m, 2, 3, REVEAL_END, COMMIT_AT + 2)?;
    let revision = m.world.revision()?;
    m.deliver(&reveal, 2, COMMIT_AT + 2)?
        .refused(WRONG_PHASE, revision)?;
    assert_eq!(m.last_sequence(2)?, 2);
    let receipt = expected_reveal(&journal, REVEAL_AT, 3)?;
    let revealed = m
        .deliver(&reveal, 2, REVEAL_AT)?
        .applied(&receipt.payload()?)?;
    assert_eq!(revealed, revision + 1);
    let slot = m.slot(2, REVEAL_AT)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Revealed, Some(committed), Some(receipt))
    );
    assert_eq!(m.admitted(2)?, Some(receipt));
    assert_eq!(m.admitted_bytes(2)?, Some(journal.load(REPORT_FILE)?));
    assert_eq!(m.last_sequence(2)?, 3);
    Ok(())
}

#[test]
fn a09_lost_salt_cannot_replace_the_commitment() -> Checked {
    let mut m = market()?;
    let journal = Journal::open("lost")?;
    let scores = m.both(1, 2);
    let report = m.signed(3, &scores)?;
    prepare(&journal, 3, &report, 2, REVEAL_AT)?;
    let committed = expected_commit(&journal, COMMIT_AT, 2)?;
    let revision = m
        .deliver(&journal.load(COMMIT_FILE)?, 3, COMMIT_AT)?
        .applied(&committed.payload()?)?;
    journal.forget(SALT_FILE)?;
    let Err(Failure::Io(lost)) = recover(&journal, &m, 3, 3, ROLE_EXPIRY, COMMIT_AT + 1) else {
        return Err(NON_CANONICAL.into());
    };
    assert_eq!(lost.kind(), io::ErrorKind::NotFound);
    let finalized = m.world.bytes.clone();
    let fresh = os_salt()?;
    let replacement = Market::commit_call(
        3,
        &report.body.binding,
        commitment_of(&report.body, fresh)?,
        3,
        ROLE_EXPIRY,
    )?;
    m.deliver(&replacement.encode()?, 3, COMMIT_AT + 1)?
        .refused(CONFLICT, revision)?;
    let guessed = Market::reveal_call(3, &report, fresh, 3, ROLE_EXPIRY)?.encode()?;
    m.deliver(&guessed, 3, REVEAL_AT)?
        .refused(F04_COMMIT_MISMATCH, revision)?;
    m.deliver(&guessed, 3, REVEAL_END - 1)?
        .refused(F04_COMMIT_MISMATCH, revision)?;
    m.deliver(&guessed, 3, REVEAL_END)?
        .refused(WRONG_PHASE, revision)?;
    assert_eq!(m.world.bytes, finalized);
    assert_eq!(m.last_sequence(3)?, 2);
    let slot = m.slot(3, REVEAL_END)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Expired, Some(committed), None)
    );
    assert_eq!(m.admitted(3)?, None);
    Ok(())
}

#[test]
fn a09_dropped_responses_reconcile_by_query_and_retry_until_expiry() -> Checked {
    let mut m = market()?;
    let journal = Journal::open("dropped")?;
    let scores = m.both(250_000, 750_000);
    prepare(&journal, 2, &m.signed(2, &scores)?, 2, REVEAL_AT)?;
    let commit = journal.load(COMMIT_FILE)?;
    let record = expected_commit(&journal, COMMIT_AT, 2)?;
    let committed = m
        .deliver(&commit, 2, COMMIT_AT)?
        .applied(&record.payload()?)?;
    assert_eq!(m.slot(2, COMMIT_AT + 1)?.commit, Some(record));
    let finalized = m.world.bytes.clone();
    m.deliver(&commit, 2, COMMIT_AT + 1)?
        .retained(committed, record.result()?)?;
    m.deliver(&commit, 2, REVEAL_AT)?
        .refused(EXPIRED, committed)?;
    assert_eq!(m.world.bytes, finalized);
    let reveal = recover(&journal, &m, 2, 3, REVEAL_END, REVEAL_AT)?;
    let receipt = expected_reveal(&journal, REVEAL_AT, 3)?;
    let revealed = m
        .deliver(&reveal, 2, REVEAL_AT)?
        .applied(&receipt.payload()?)?;
    let slot = m.slot(2, REVEAL_AT + 1)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Revealed, Some(record), Some(receipt))
    );
    assert_eq!(m.admitted_bytes(2)?, Some(journal.load(REPORT_FILE)?));
    let finalized = m.world.bytes.clone();
    m.deliver(&reveal, 2, REVEAL_AT + 1)?
        .retained(revealed, receipt.result()?)?;
    m.deliver(&reveal, 2, REVEAL_END - 1)?
        .retained(revealed, receipt.result()?)?;
    let (salt, _) = saved(&journal)?;
    let other_scores = m.both(1, 2);
    let other = m.signed(2, &other_scores)?;
    let changed = Market::reveal_call(2, &other, salt, 3, REVEAL_END)?.encode()?;
    m.deliver(&changed, 2, REVEAL_AT + 2)?
        .refused(REPLAY_CONFLICT, revealed)?;
    let renewed = Market::reveal_call(2, &other, salt, 4, REVEAL_END)?.encode()?;
    m.deliver(&renewed, 2, REVEAL_AT + 2)?
        .refused(CONFLICT, revealed)?;
    let mut again = decode_envelope(&reveal)?.envelope;
    again.sequence = 4;
    again.request = RequestId::new(tag(0xa2, 2, 4))?;
    let mut resent = vec![0; reveal.len()];
    let len = encode_envelope(&again, &mut resent)?;
    m.deliver(&resent[..len], 2, REVEAL_AT + 2)?
        .refused(CONFLICT, revealed)?;
    m.deliver(&reveal, 2, REVEAL_END)?
        .refused(EXPIRED, revealed)?;
    assert_eq!(m.world.bytes, finalized);
    assert_eq!(m.last_sequence(2)?, 3);
    let slot = m.slot(2, REVEAL_END)?;
    assert_eq!((slot.commit, slot.reveal), (Some(record), Some(receipt)));
    Ok(())
}

#[test]
fn a09_concurrent_reveals_reread_the_finalized_state() -> Checked {
    let mut m = market()?;
    let scores = m.both(3, 4);
    let journals = [
        Journal::open("concurrent-3")?,
        Journal::open("concurrent-4")?,
    ];
    let mut reveals = Vec::new();
    for (n, journal) in [3, 4].into_iter().zip(&journals) {
        prepare(journal, n, &m.signed(n, &scores)?, 2, REVEAL_END)?;
        let record = expected_commit(journal, COMMIT_AT, 2)?;
        m.deliver(&journal.load(COMMIT_FILE)?, n, COMMIT_AT)?
            .applied(&record.payload()?)?;
    }
    for (n, journal) in [3, 4].into_iter().zip(&journals) {
        reveals.push(recover(journal, &m, n, 3, REVEAL_END, REVEAL_AT)?);
    }
    let third = expected_reveal(&journals[0], REVEAL_AT, 3)?;
    let fourth = expected_reveal(&journals[1], REVEAL_AT, 3)?;
    let base = m.clone();
    let first = m
        .deliver(&reveals[0], 3, REVEAL_AT)?
        .applied(&third.payload()?)?;
    let stale = base.submit(&reveals[1], 4, REVEAL_AT)?;
    assert_eq!(stale.applied(&fourth.payload()?)?, first);
    let composed = decode_shared_state(&stale.next)?;
    assert_eq!(
        f03::admitted_report(&composed, 7, m.evaluator(3)?)?.map(|row| row.receipt),
        None
    );
    let second = m
        .deliver(&reveals[1], 4, REVEAL_AT)?
        .applied(&fourth.payload()?)?;
    assert_eq!(second, first + 1);
    assert_eq!(
        (m.admitted(3)?, m.admitted(4)?),
        (Some(third), Some(fourth))
    );
    let finalized = m.world.bytes.clone();
    for (n, reveal, receipt, revision) in [
        (3, &reveals[0], third, first),
        (4, &reveals[1], fourth, second),
    ] {
        m.deliver(reveal, n, REVEAL_AT + 1)?
            .retained(revision, receipt.result()?)?;
    }
    assert_eq!(m.world.bytes, finalized);
    Ok(())
}

/// Evaluators 2..=4 commit and reveal their journaled reports with role-length envelopes;
/// evaluator 2's key rotation and configuration 2 are staged for epoch 8 in between.
fn epoch_seven(m: &mut Market, journals: &[Journal; 3]) -> Checked {
    let scores = m.both(250_000, 750_000);
    for (n, journal) in (2..=4).zip(journals) {
        prepare(journal, n, &m.signed(n, &scores)?, 2, ROLE_EXPIRY)?;
        let record = expected_commit(journal, COMMIT_AT, 2)?;
        m.deliver(&journal.load(COMMIT_FILE)?, n, COMMIT_AT)?
            .applied(&record.payload()?)?;
    }
    m.rotate(2, COMMIT_AT + 1)?;
    m.world.stage(&policy(2, 3)?, 8, COMMIT_AT + 1)?;
    for (n, journal) in (2..=4).zip(journals) {
        let reveal = recover(journal, m, n, 3, ROLE_EXPIRY, REVEAL_AT)?;
        let receipt = expected_reveal(journal, REVEAL_AT, 3)?;
        m.deliver(&reveal, n, REVEAL_AT)?
            .applied(&receipt.payload()?)?;
        assert_eq!(m.slot(n, REVEAL_AT)?.status, Status::Revealed);
    }
    Ok(())
}

/// Epoch 8 under the rotated key and configuration 2: the epoch-7 report and salt are refused,
/// a fresh journaled report seals, commits and reveals, and the current-reports region is
/// rewritten for epoch 8 alone.
fn epoch_eight(m: &mut Market, previous: &Journal) -> Checked {
    let revision = m.world.revision()?;
    let (salt, signed) = saved(previous)?;
    let old = decode_signed_report(&signed)?;
    let commit = Market::commit_call(
        2,
        &old.body.binding,
        commitment_of(&old.body, salt)?,
        4,
        ROLE_EXPIRY,
    )?;
    m.deliver(&commit.encode()?, 2, NEXT_COMMIT_AT)?
        .refused(WRONG_EPOCH, revision)?;
    let reveal = Market::reveal_call(2, &old, salt, 4, ROLE_EXPIRY)?;
    m.deliver(&reveal.encode()?, 2, NEXT_REVEAL_AT)?
        .refused(WRONG_EPOCH, revision)?;
    m.seal(&[2], NEXT_COMMIT_AT, 4)?;
    let binding = EvaluatorBinding {
        key_version: Version::new(2)?,
        ..m.binding(2)?
    };
    assert_eq!((binding.frozen.epoch, binding.frozen.config.get()), (8, 2));
    let scores = m.both(250_000, 750_000);
    let rotated = sign(Market::body(binding, 2, &scores)?, &rotated_key(2))?;
    assert_ne!(
        commitment_of(&rotated.body, salt)?,
        commitment_of(&old.body, salt)?
    );
    let journal = Journal::open("rollover-epoch-8")?;
    prepare(&journal, 2, &rotated, 5, ROLE_EXPIRY)?;
    let record = expected_commit(&journal, NEXT_COMMIT_AT, 5)?;
    assert_eq!(record.key_version.get(), 2);
    m.deliver(&journal.load(COMMIT_FILE)?, 2, NEXT_COMMIT_AT)?
        .applied(&record.payload()?)?;
    let reveal = recover(&journal, m, 2, 6, ROLE_EXPIRY, NEXT_REVEAL_AT)?;
    let receipt = expected_reveal(&journal, NEXT_REVEAL_AT, 6)?;
    m.deliver(&reveal, 2, NEXT_REVEAL_AT)?
        .applied(&receipt.payload()?)?;
    assert_eq!(m.admitted_bytes(2)?, Some(journal.load(REPORT_FILE)?));
    assert_eq!(m.admitted(3)?, None);
    let state = decode_shared_state(&m.world.bytes)?;
    let region = ReportRegion::decode(state.feature_sections[Section::CurrentReports.index()])?;
    assert_eq!(region.epoch, 8);
    Ok(())
}

#[test]
fn a12_rollover_binds_epoch_eight_and_historical_retries_recreate_nothing() -> Checked {
    let mut m = market()?;
    let journals = [
        Journal::open("rollover-2")?,
        Journal::open("rollover-3")?,
        Journal::open("rollover-4")?,
    ];
    epoch_seven(&mut m, &journals)?;
    m.revoke(4, false, REVEAL_AT + 1)?;
    let revoked = m.admitted(4)?;
    assert_eq!(revoked, Some(expected_reveal(&journals[2], REVEAL_AT, 3)?));
    let sealed = m.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(m.expected_input(&[2, 3], SETTLE_AT)?)
    );
    let done = m.complete(SETTLE_AT)?;
    let settled = m.frozen.epoch;
    m.frozen = m.world.open(ROLLOVER_AT)?;
    assert_eq!((m.frozen.epoch, m.frozen.config.get()), (settled + 1, 2));
    let rolled = m.world.bytes.clone();
    let revision = m.world.revision()?;
    for (n, journal) in (2..=4).zip(&journals) {
        for entry in [COMMIT_FILE, REVEAL_FILE] {
            m.deliver(&journal.load(entry)?, n, NEXT_COMMIT_AT)?
                .refused(WRONG_EPOCH, revision)?;
        }
    }
    assert_eq!(m.world.bytes, rolled);
    let state = decode_shared_state(&m.world.bytes)?;
    for n in 2..=4 {
        assert_eq!(
            f03::admitted_report(&state, settled, m.evaluator(n)?).map(|row| row.is_some()),
            Err(WRONG_EPOCH)
        );
        assert_eq!(m.admitted(n)?, None);
    }
    for n in 2..=3 {
        let slot = m.slot(n, NEXT_COMMIT_AT)?;
        assert_eq!(
            (slot.status, slot.commit, slot.reveal),
            (Status::Absent, None, None)
        );
    }
    let next = m.progress()?;
    assert_eq!(
        (next.epoch, next.phase, next.input),
        (8, AggregationPhase::Unsealed, Presence::Absent)
    );
    let Presence::Present(root) = done.root else {
        return Err(NOT_FOUND.into());
    };
    assert_eq!(m.history()?, vec![(settled, root)]);
    assert_eq!(m.reward_row(settled)?.status, EpochStatus::Terminal);
    epoch_eight(&mut m, &journals[0])?;
    assert_eq!(m.history()?, vec![(settled, root)]);
    Ok(())
}
