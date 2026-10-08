//! AI.F04-T02 atomic commit and reveal windows over the complete shared state value. Every
//! market is produced by the real F01/F02/F03/F06/F08/F09 producers, `OPEN_EPOCH` and the F01
//! task-set lifecycle; `CommitScore` and `RevealScore` go through `commit_reveal::apply` with
//! real envelopes, the canonical commitment digest and real ed25519 evaluator keys.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        CommitScorePayload, Envelope, ReportBody, RevealScorePayload, ScoreVector,
        ValidatedEnvelope, REPORT_FIXED_BYTES, REPORT_MAX_BYTES,
    },
    commit_reveal::{self as cr, commitment, CommitRecord, CommitRegion, Outcome as F04, Status},
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, BAD_SIGNATURE, CAPACITY, CONFLICT, EXPIRED,
        F03_SCORE_RANGE, F03_UNKNOWN_WORKER, F04_COMMIT_MISMATCH, F04_NO_SCORES, F04_SALT_INVALID,
        F08_MARKET_PAUSED, KEY_MISMATCH, NON_CANONICAL, NOT_FOUND, REPLAY_CONFLICT, REVOKED,
        UNAUTHORIZED, WRONG_CONFIG, WRONG_DOMAIN, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE,
        WRONG_PROGRAM, WRONG_ROSTER,
    },
    evaluators::{
        admission::{self as f03, AdmissionReceipt, ReportRegion},
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        codec::encode_signed_report,
        model::{EvaluatorGrant, GrantTerms, SignedReport, SIGNED_REPORT_MAX_BYTES},
    },
    evidence::{self, Outcome as Sealing, SEAL_SCRATCH_BYTES},
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, HeightWindow, ReplayRequest,
        ReplayTable, Section, SharedState,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvidenceRoot, FrozenBinding, MarketId, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Salt32, Score, ScoreEntry, Signature64, Version, WorkerId,
        WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
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
const COMMIT_TOPIC: &[u8] = b"PAXAI/v1/CommitScore";
const REVEAL_TOPIC: &[u8] = b"PAXAI/v1/RevealScore";
const SALT: [u8; 32] = [0x5a; 32];
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120).
const COMMIT_AT: u64 = 1088;
const REVEAL_AT: u64 = 1104;
const REVEAL_END: u64 = 1120;
/// The two frozen workers of epoch 7.
const LOW: u8 = 0x20;
const HIGH: u8 = 0x21;

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
    /// Restores 31 more workers and evaluators 5..=9 beside the real first enrollments, at the
    /// worker and evaluator bounds, from the real admitted memberships of worker `first` and
    /// evaluator 2: the F08 budget admits four enrollments per epoch and no producer admits a
    /// whole bounded roster at once.
    fn restore(
        &mut self,
        first: WorkerRosterEntry,
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
            for n in LOW + 1..LOW + 32 {
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
/// Epoch 6 (T = 896) of a market at both identity bounds: 32 frozen workers and evaluators
/// 2..=9 frozen, the empty task set sealed and every evidence root sealed at 960.
fn crowded() -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let first = world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    let mut workers = world.restore(first, 134)?;
    world.schedule(6, 135)?;
    world.fund(500, 136)?;
    let frozen = world.open(896)?;
    assert_eq!(
        (frozen.epoch, frozen.workers, frozen.evaluators),
        (6, 32, 8)
    );
    world.advance(897)?;
    workers.sort_by_key(|w| w.worker);
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen,
        header,
        workers,
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

/// `CommitScore` and `RevealScore` requests and the F04 call.
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
    /// One F04 call composed over the committed bytes into fresh caller buffers.
    fn compose(
        &self,
        call: &Req,
        ctx: &CallContext,
    ) -> CodecResult<(CodecResult<F04>, Vec<u8>, Vec<u8>)> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; cr::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = cr::apply(
            ctx,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        );
        Ok((outcome, next, event))
    }
    /// One F04 call, committed only on `Committed` or `Revealed` after the complete next
    /// state and its event are checked; anything else writes no event.
    fn send(&mut self, call: &Req, ctx: &CallContext) -> CodecResult<F04> {
        let (outcome, next, event) = self.compose(call, ctx)?;
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        match outcome {
            Ok(F04::Committed {
                record,
                revision,
                result,
                state_len,
                event_len,
            }) => {
                let previous =
                    core::mem::replace(&mut self.world.bytes, next[..state_len].to_vec());
                assert_eq!(record.height, ctx.height);
                check_committed(
                    &previous,
                    &self.world.bytes,
                    &envelope,
                    &record,
                    revision,
                    result,
                    &event[..event_len],
                )?;
            }
            Ok(F04::Revealed {
                receipt,
                revision,
                state_len,
                event_len,
            }) => {
                let previous =
                    core::mem::replace(&mut self.world.bytes, next[..state_len].to_vec());
                assert_eq!(receipt.height, ctx.height);
                check_revealed(
                    &previous,
                    &self.world.bytes,
                    &envelope,
                    &receipt,
                    revision,
                    &event[..event_len],
                )?;
            }
            _ => assert!(event.iter().all(|b| *b == 0)),
        }
        outcome
    }
    /// One F04 call by the envelope actor.
    fn call(&mut self, call: &Req, at: u64) -> CodecResult<F04> {
        self.send(call, &call.context(at)?)
    }
    /// A refused call leaves the committed bytes unchanged.
    fn refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.call(call, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    /// Evaluator `n` commits `C` of `report` and `SALT` at evaluator sequence 2, expiring at
    /// the commit end.
    fn commit(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> CodecResult<CommitRecord> {
        let c = commitment_of(&report.body, SALT)?;
        let call = Market::commit_call(n, &report.body.binding, c, 2, self.start() + 80)?;
        let F04::Committed { record, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(
            (
                record.commitment,
                record.sequence,
                record.grant,
                record.key_version
            ),
            (
                c,
                2,
                report.body.binding.grant,
                report.body.binding.key_version
            )
        );
        Ok(record)
    }
    /// Evaluator `n` reveals `report` and `SALT` at evaluator sequence 3, expiring at the
    /// reveal end.
    fn reveal(
        &mut self,
        n: u8,
        report: &SignedReport<'_>,
        at: u64,
    ) -> CodecResult<AdmissionReceipt> {
        let call = Market::reveal_call(n, report, SALT, 3, self.start() + 96)?;
        let F04::Revealed { receipt, .. } = self.call(&call, at)? else {
            return Err(NON_CANONICAL);
        };
        Ok(receipt)
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

/// One revision: the F01 section changes only its header revision; identity, rewards,
/// admission and control feature bytes are byte-identical, so no score normalization,
/// reward, slash or transfer follows.
fn check_one_revision(
    before: &SharedState<'_>,
    after: &SharedState<'_>,
    revision: u64,
) -> TestResult {
    assert_eq!(
        (after.revision, revision),
        (before.revision + 1, before.revision + 1)
    );
    let mut section = PolicySection::decode(before.feature_sections[0])?;
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    assert_eq!(after.feature_sections[0], policy.as_slice());
    assert_eq!(after.feature_sections[1], before.feature_sections[1]);
    assert_eq!(after.feature_sections[3..], before.feature_sections[3..]);
    assert_eq!(after.control.feature_bytes, before.control.feature_bytes);
    Ok(())
}
/// The current-reports section and its F03 region prefix.
fn split_reports<'a>(state: &SharedState<'a>) -> CodecResult<(&'a [u8], ReportRegion<'a>)> {
    let section = state.feature_sections[Section::CurrentReports.index()];
    let region = ReportRegion::decode(section)?;
    Ok((&section[..section.len() - region.rest().len()], region))
}
/// The retained evaluator result of the envelope actor.
fn check_retained(
    after: &SharedState<'_>,
    envelope: &ValidatedEnvelope<'_>,
    revision: u64,
    result: ResultDigest,
) -> TestResult {
    let slot = evaluator_slot(&after.control.replay, envelope.envelope.actor)?;
    let retained = after
        .control
        .replay
        .actor(slot)
        .and_then(|actor| actor.last)
        .ok_or(NON_CANONICAL)?;
    assert_eq!(
        (
            retained.sequence,
            retained.applied_revision,
            retained.request_digest,
            retained.result_digest
        ),
        (
            envelope.envelope.sequence,
            revision,
            envelope.request_digest()?,
            result
        )
    );
    Ok(())
}
/// ABSENT became COMMITTED: the F03 rows are carried, the record is inserted into the commit
/// region of the epoch, and the 194-byte `CommitScore` event is the only event.
fn check_committed(
    previous: &[u8],
    current: &[u8],
    envelope: &ValidatedEnvelope<'_>,
    record: &CommitRecord,
    revision: u64,
    result: ResultDigest,
    event: &[u8],
) -> TestResult {
    let before = decode_shared_state(previous)?;
    let after = decode_shared_state(current)?;
    check_one_revision(&before, &after, revision)?;
    let e = &envelope.envelope;
    let (old_prefix, old) = split_reports(&before)?;
    let (new_prefix, new) = split_reports(&after)?;
    if old_prefix.is_empty() {
        let mut empty = e.epoch.to_be_bytes().to_vec();
        empty.push(0);
        assert_eq!(new_prefix, empty.as_slice());
    } else {
        assert_eq!(new_prefix, old_prefix);
    }
    let commits = CommitRegion::decode(new.rest())?;
    assert_eq!(
        CommitRegion::decode(old.rest())?.get(e.epoch, record.evaluator)?,
        None
    );
    assert_eq!(
        (commits.epoch, commits.get(e.epoch, record.evaluator)?),
        (e.epoch, Some(*record))
    );
    assert_eq!(record.sequence, e.sequence);
    let mut payload = vec![1];
    payload.extend_from_slice(record.evaluator.as_bytes());
    payload.extend_from_slice(record.commitment.as_bytes());
    payload.extend_from_slice(&record.height.to_be_bytes());
    payload.extend_from_slice(&record.sequence.to_be_bytes());
    assert_eq!(record.payload()?.as_slice(), payload.as_slice());
    assert_eq!(result, codec::result_digest(&payload)?);
    check_retained(&after, envelope, revision, result)?;
    assert_eq!(event.len(), 194);
    let (operation, common, _) = codec::decode_event_frame(COMMIT_TOPIC, event)?;
    let decoded = codec::decode_commit_event(event)?;
    assert_eq!(operation, dispatch::CommitScore);
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
    assert_eq!(
        (
            decoded.evaluator,
            decoded.commitment,
            decoded.accepted_height
        ),
        (record.evaluator, record.commitment, record.height)
    );
    Ok(())
}
/// COMMITTED became REVEALED through F03 admission: the commit region is carried, the
/// admitted row holds the exact signed report, and the 228-byte `RevealScore` event is the
/// only event.
fn check_revealed(
    previous: &[u8],
    current: &[u8],
    envelope: &ValidatedEnvelope<'_>,
    receipt: &AdmissionReceipt,
    revision: u64,
    event: &[u8],
) -> TestResult {
    let before = decode_shared_state(previous)?;
    let after = decode_shared_state(current)?;
    check_one_revision(&before, &after, revision)?;
    let e = &envelope.envelope;
    assert_eq!(
        split_reports(&after)?.1.rest(),
        split_reports(&before)?.1.rest()
    );
    let payload = commitment::decode_reveal_score(e.payload)?;
    let signed = SignedReport {
        body: payload.report,
        signature: payload.signature,
    };
    let mut bytes = vec![0; SIGNED_REPORT_MAX_BYTES];
    let len = encode_signed_report(&signed, &mut bytes)?;
    let row = f03::admitted_report(&after, e.epoch, receipt.evaluator)?.ok_or(NOT_FOUND)?;
    assert_eq!((row.receipt, row.signed_bytes()), (*receipt, &bytes[..len]));
    assert_eq!(
        (receipt.report, receipt.activity),
        (codec::report_digest(&signed.body)?, e.sequence)
    );
    check_retained(&after, envelope, revision, receipt.result()?)?;
    assert_eq!(event.len(), 228);
    let (operation, common, _) = codec::decode_event_frame(REVEAL_TOPIC, event)?;
    let decoded = codec::decode_reveal_event(event)?;
    assert_eq!(operation, dispatch::RevealScore);
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
        (envelope.request_digest()?, receipt.result()?)
    );
    assert_eq!(
        (
            decoded.evaluator,
            decoded.report,
            decoded.evidence,
            usize::from(decoded.vector_count),
            decoded.admitted_height
        ),
        (
            receipt.evaluator,
            receipt.report,
            signed.body.evidence,
            signed.body.scores.len(),
            receipt.height
        )
    );
    Ok(())
}

#[test]
fn a01_one_immutable_commitment_exact_retry_and_expiry() -> TestResult {
    let mut m = market()?;
    let scores = m.both(250_000, 750_000);
    let report = m.signed(2, &scores)?;
    let binding = report.body.binding;
    let c = commitment_of(&report.body, SALT)?;
    let call = Market::commit_call(2, &binding, c, 2, REVEAL_AT)?;
    let F04::Committed {
        record,
        revision,
        result,
        ..
    } = m.call(&call, COMMIT_AT)?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (record.height, record.sequence, record.commitment),
        (COMMIT_AT, 2, c)
    );
    let committed = m.world.bytes.clone();
    let F04::Retained(retained) = m.call(&call, 1089)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            retained.sequence,
            retained.applied_revision,
            retained.result_digest
        ),
        (2, revision, result)
    );
    let mut flipped = c.bytes();
    flipped[31] ^= 1;
    let other = CommitmentDigest::new(flipped)?;
    let conflicting = Market::commit_call(2, &binding, other, 2, REVEAL_AT)?;
    assert_eq!(m.refused(&conflicting, 1090)?, REPLAY_CONFLICT);
    let renewed = Market::commit_call(2, &binding, other, 3, REVEAL_AT)?;
    assert_eq!(m.refused(&renewed, 1090)?, CONFLICT);
    assert_eq!(m.refused(&call, REVEAL_AT)?, EXPIRED);
    assert_eq!(m.world.bytes, committed);
    let slot = m.slot(2, REVEAL_AT)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Committed, Some(record), None)
    );
    Ok(())
}

#[test]
fn a02_half_open_windows_and_a_single_reveal() -> TestResult {
    let mut m = market()?;
    let scores = m.both(250_000, 750_000);
    let reports = (2..=4)
        .map(|n| m.signed(n, &scores))
        .collect::<CodecResult<Vec<_>>>()?;
    let c = commitment_of(&reports[0].body, SALT)?;
    for at in [COMMIT_AT - 1, REVEAL_AT] {
        let call = Market::commit_call(2, &reports[0].body.binding, c, 2, ROLE_EXPIRY)?;
        assert_eq!(m.refused(&call, at)?, WRONG_PHASE);
    }
    assert_eq!(m.slot(2, REVEAL_AT)?.status, Status::Absent);
    let record = m.commit(2, &reports[0], COMMIT_AT)?;
    m.commit(3, &reports[1], REVEAL_AT - 1)?;
    m.commit(4, &reports[2], 1090)?;
    let early = Market::reveal_call(2, &reports[0], SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&early, REVEAL_AT - 1)?, WRONG_PHASE);
    let receipt = m.reveal(2, &reports[0], REVEAL_AT)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let row = f03::admitted_report(&state, 7, m.evaluator(2)?)?.ok_or(NOT_FOUND)?;
    let listed = row
        .signed()?
        .body
        .scores
        .entries()
        .collect::<CodecResult<Vec<_>>>()?;
    let expected = vec![
        ScoreEntry {
            worker: m.workers[0].worker,
            score: Score::new(250_000)?,
        },
        ScoreEntry {
            worker: m.workers[1].worker,
            score: Score::new(750_000)?,
        },
    ];
    assert_eq!(listed, expected);
    let retry = Market::reveal_call(2, &reports[0], SALT, 3, REVEAL_END)?;
    let F04::Retained(retained) = m.call(&retry, REVEAL_AT + 1)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (retained.sequence, retained.result_digest),
        (3, receipt.result()?)
    );
    let last = m.reveal(3, &reports[1], REVEAL_END - 1)?;
    let late = Market::reveal_call(4, &reports[2], SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&late, REVEAL_END)?, WRONG_PHASE);
    let slot = m.slot(2, REVEAL_END)?;
    assert_eq!(
        (slot.status, slot.commit, slot.reveal),
        (Status::Revealed, Some(record), Some(receipt))
    );
    assert_eq!(m.slot(3, REVEAL_END)?.reveal, Some(last));
    assert_eq!(m.slot(4, REVEAL_END)?.status, Status::Expired);
    assert_eq!(m.admitted(4)?, None);
    Ok(())
}

#[test]
fn a03_malformed_vectors_refuse_before_admission() -> TestResult {
    let mut m = market()?;
    let (a, b) = (m.workers[0].worker, m.workers[1].worker);
    let scores = m.both(250_000, 750_000);
    let report = m.signed(2, &scores)?;
    let record = m.commit(2, &report, COMMIT_AT)?;
    let stranger = derive_worker(m.header.market_id, principal(0x60)?, [0x60; 32])?;
    let mut known = [(a, 250_000), (stranger, 750_000)];
    known.sort_unstable_by_key(|(w, _)| *w);
    let unknown_scores = entries(&known);
    let own = m.signed(3, &unknown_scores)?;
    m.commit(3, &own, COMMIT_AT)?;
    let valid = reveal_payload(&report, SALT)?;
    let mut trailing = valid[4..valid.len() - 96].to_vec();
    trailing.push(0);
    let variants = [
        (
            raw_body(&report.body, 2, &entries(&[(b, 750_000), (a, 250_000)]))?,
            NON_CANONICAL,
        ),
        (
            raw_body(&report.body, 2, &entries(&[(a, 250_000), (a, 250_000)]))?,
            NON_CANONICAL,
        ),
        (trailing, NON_CANONICAL),
        (raw_body(&report.body, 3, &scores)?, NON_CANONICAL),
        (
            raw_body(&report.body, 2, &entries(&[(a, 1_000_001), (b, 750_000)]))?,
            F03_SCORE_RANGE,
        ),
    ];
    let template = Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?;
    for (body, code) in variants {
        let payload = raw_reveal(&body, &report.signature.0, &SALT)?;
        assert_eq!(
            commitment::decode_reveal_score(&payload).map(|_| ()),
            Err(code)
        );
        let call = Req {
            payload,
            ..template.clone()
        };
        assert_eq!(call.encode().map(|_| ()), Err(code));
    }
    let unknown = m.signed(2, &unknown_scores)?;
    let call = Market::reveal_call(2, &unknown, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_AT)?, F04_COMMIT_MISMATCH);
    let call = Market::reveal_call(3, &own, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_AT)?, F03_UNKNOWN_WORKER);
    let changed_scores = m.both(250_001, 750_000);
    let changed = m.signed(2, &changed_scores)?;
    let call = Market::reveal_call(2, &changed, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_AT)?, F04_COMMIT_MISMATCH);
    assert_eq!((m.admitted(2)?, m.admitted(3)?), (None, None));
    assert_eq!(m.slot(3, REVEAL_END - 1)?.status, Status::Committed);
    assert_eq!(m.slot(2, REVEAL_AT)?.commit, Some(record));
    let receipt = m.reveal(2, &report, REVEAL_AT)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

/// Each binding field changed alone, with the envelope carrying the same change.
fn binding_variants(m: &Market) -> CodecResult<Vec<(EvaluatorBinding, ApplicationError)>> {
    let good = m.binding(2)?;
    let f = good.frozen;
    let frozen = [
        (
            FrozenBinding {
                chain: ChainDomain::new([0x61; 32])?,
                ..f
            },
            WRONG_DOMAIN,
        ),
        (
            FrozenBinding {
                program: ProgramId::new([0x62; 32])?,
                ..f
            },
            WRONG_PROGRAM,
        ),
        (
            FrozenBinding {
                market: MarketId::new([0x63; 32])?,
                ..f
            },
            WRONG_MARKET,
        ),
        (FrozenBinding { epoch: 6, ..f }, WRONG_EPOCH),
        (FrozenBinding { epoch: 8, ..f }, WRONG_EPOCH),
        (
            FrozenBinding {
                config: Version::new(2)?,
                ..f
            },
            WRONG_CONFIG,
        ),
        (
            FrozenBinding {
                roster: RosterDigest::new([0x64; 32])?,
                ..f
            },
            WRONG_ROSTER,
        ),
    ];
    let mut out = frozen
        .into_iter()
        .map(|(frozen, code)| (EvaluatorBinding { frozen, ..good }, code))
        .collect::<Vec<_>>();
    for (evaluator, grant, key_version, code) in [
        (m.evaluator(3)?, 1, 1, UNAUTHORIZED),
        (m.evaluator(9)?, 1, 1, UNAUTHORIZED),
        (good.evaluator, 2, 1, UNAUTHORIZED),
        (good.evaluator, 1, 2, KEY_MISMATCH),
    ] {
        let binding = EvaluatorBinding {
            evaluator,
            grant: Version::new(grant)?,
            key_version: Version::new(key_version)?,
            ..good
        };
        out.push((binding, code));
    }
    Ok(out)
}

#[test]
fn a04_each_binding_field_and_a_foreign_reveal() -> TestResult {
    let mut m = market()?;
    let scores = m.both(1, 2);
    let report = m.signed(2, &scores)?;
    let start = m.world.bytes.clone();
    for (binding, code) in binding_variants(&m)? {
        let body = Market::body(binding, 2, &scores)?;
        let call = Market::commit_call(2, &binding, commitment_of(&body, SALT)?, 2, ROLE_EXPIRY)?;
        assert_eq!(m.refused(&call, COMMIT_AT)?, code);
    }
    assert_eq!(m.world.bytes, start);
    let record = m.commit(2, &report, COMMIT_AT)?;
    let theirs = m.signed(3, &scores)?;
    m.commit(3, &theirs, COMMIT_AT)?;
    let stolen = Req {
        actor: principal(3)?,
        request: tag(0xa2, 3, 3),
        ..Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?
    };
    assert_eq!(m.refused(&stolen, REVEAL_AT)?, UNAUTHORIZED);
    let forged = sign(report.body, &evaluator_key(3))?;
    let call = Market::reveal_call(2, &forged, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_AT)?, BAD_SIGNATURE);
    let receipt = m.reveal(2, &report, REVEAL_AT)?;
    let slot = m.slot(2, REVEAL_AT)?;
    assert_eq!((slot.commit, slot.reveal), (Some(record), Some(receipt)));
    assert_eq!(m.admitted(3)?, None);
    Ok(())
}

#[test]
fn a04_native_principals_and_a_relayed_delegate() -> TestResult {
    let mut m = market()?;
    let scores = m.both(1, 2);
    let report = m.signed(2, &scores)?;
    let c = commitment_of(&report.body, SALT)?;
    let native = Market::commit_call(2, &report.body.binding, c, 2, ROLE_EXPIRY)?;
    let outsider = principal(RELAYER)?;
    let relayed = CallContext {
        principal: outsider,
        ..native.context(COMMIT_AT)?
    };
    let start = m.world.bytes.clone();
    assert_eq!(m.send(&native, &relayed), Err(UNAUTHORIZED));
    let impostor = Req {
        actor: outsider,
        ..native.clone()
    };
    assert_eq!(m.refused(&impostor, COMMIT_AT)?, UNAUTHORIZED);
    let delegated = Req {
        delegate: Some((evaluator_key(2), evaluator_key(2))),
        ..native.clone()
    };
    let changed = commit_payload(
        &report.body.binding,
        commitment_of(&report.body, [0x5b; 32])?,
    )?;
    let tampered = Req {
        signed: Some(changed),
        ..delegated.clone()
    };
    assert_eq!(m.send(&tampered, &relayed), Err(BAD_SIGNATURE));
    let foreign = Req {
        delegate: Some((evaluator_key(3), evaluator_key(3))),
        ..native.clone()
    };
    assert_eq!(m.send(&foreign, &relayed), Err(KEY_MISMATCH));
    assert_eq!(m.world.bytes, start);
    let F04::Committed { record, .. } = m.send(&delegated, &relayed)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!((record.commitment, record.sequence), (c, 2));
    let reveal = Req {
        delegate: Some((evaluator_key(2), evaluator_key(2))),
        ..Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?
    };
    let ctx = CallContext {
        principal: outsider,
        ..reveal.context(REVEAL_AT)?
    };
    let F04::Revealed { receipt, .. } = m.send(&reveal, &ctx)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn a05_signature_and_salt_refusals_keep_the_commitment() -> TestResult {
    let mut m = market()?;
    let scores = m.both(250_000, 750_000);
    let report = m.signed(2, &scores)?;
    let record = m.commit(2, &report, COMMIT_AT)?;
    let mut signature = report.signature;
    signature.0[0] ^= 1;
    let changed = SignedReport {
        signature,
        ..report
    };
    let call = Market::reveal_call(2, &changed, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, REVEAL_AT)?, BAD_SIGNATURE);
    assert_eq!(Salt32::new([0; 32]).map(|_| ()), Err(F04_SALT_INVALID));
    let valid = reveal_payload(&report, SALT)?;
    let body = &valid[4..valid.len() - 96];
    let sig = &report.signature.0;
    let template = Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?;
    for (salt, code) in [
        (&[0; 32][..], F04_SALT_INVALID),
        (&SALT[..31], NON_CANONICAL),
    ] {
        let payload = raw_reveal(body, sig, salt)?;
        assert_eq!(
            commitment::decode_reveal_score(&payload).map(|_| ()),
            Err(code)
        );
        let mut out = vec![0; commitment::REVEAL_MAX_BYTES];
        assert_eq!(
            commitment::encode_reveal_score(body, sig, salt, &mut out),
            Err(code)
        );
        let call = Req {
            payload,
            ..template.clone()
        };
        assert_eq!(call.encode().map(|_| ()), Err(code));
    }
    let other = Market::reveal_call(2, &report, [0x5b; 32], 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&other, REVEAL_AT)?, F04_COMMIT_MISMATCH);
    assert_eq!(m.slot(2, REVEAL_END - 1)?.commit, Some(record));
    let receipt = m.reveal(2, &report, REVEAL_END - 1)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn a06_missing_commit_and_partial_reveals() -> TestResult {
    let mut m = market()?;
    let scores = m.both(1, 2);
    let report = m.signed(7, &scores)?;
    let uncommitted = Market::reveal_call(7, &report, SALT, 2, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&uncommitted, REVEAL_AT)?, NOT_FOUND);
    assert_eq!(m.slot(7, REVEAL_AT)?.status, Status::Absent);
    let mut c = crowded()?;
    let start = c.start();
    let scores = c.both(400_000, 600_000);
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
    let state = decode_shared_state(&c.world.bytes)?;
    assert_eq!(split_reports(&state)?.1.rows(6).count(), 2);
    for n in 2..=9 {
        let slot = c.slot(n, start + 96)?;
        let expected = if n <= 3 {
            Status::Revealed
        } else {
            Status::Expired
        };
        assert_eq!((slot.status, slot.commit.is_some()), (expected, true));
        assert_eq!(c.admitted(n)?.is_some(), n <= 3);
    }
    Ok(())
}

#[test]
fn a07_empty_vectors_refuse_and_explicit_zero_is_admitted() -> TestResult {
    let mut m = market()?;
    let a = m.workers[0].worker;
    let scores = m.both(1, 2);
    let report = m.signed(2, &scores)?;
    let empty = raw_body(&report.body, 0, &[])?;
    assert_eq!(empty.len(), REPORT_FIXED_BYTES);
    let payload = raw_reveal(&empty, &report.signature.0, &SALT)?;
    assert_eq!(payload.len(), 328);
    assert_eq!(
        commitment::decode_reveal_score(&payload).map(|_| ()),
        Err(F04_NO_SCORES)
    );
    let mut preimage = vec![0; commitment::REPORT_PREIMAGE_MAX_BYTES];
    assert_eq!(
        commitment::report_preimage(&empty, &mut preimage),
        Err(F04_NO_SCORES)
    );
    let call = Req {
        payload,
        ..Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?
    };
    assert_eq!(call.encode().map(|_| ()), Err(NON_CANONICAL));
    let zero = entries(&[(a, 0)]);
    let explicit = m.signed(2, &zero)?;
    let mut body = vec![0; REPORT_MAX_BYTES];
    assert_eq!(codec::encode_report(&explicit.body, &mut body)?, 264);
    m.commit(2, &explicit, COMMIT_AT)?;
    let other = m.signed(3, &scores)?;
    m.commit(3, &other, COMMIT_AT)?;
    let receipt = m.reveal(2, &explicit, REVEAL_AT)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let row = f03::admitted_report(&state, 7, m.evaluator(2)?)?.ok_or(NOT_FOUND)?;
    let listed = row
        .signed()?
        .body
        .scores
        .entries()
        .collect::<CodecResult<Vec<_>>>()?;
    assert_eq!(
        listed,
        vec![ScoreEntry {
            worker: a,
            score: Score::new(0)?
        }]
    );
    let at = |m: &Market, n| m.slot(n, REVEAL_END).map(|s| s.status);
    assert_eq!(m.slot(3, REVEAL_END - 1)?.status, Status::Committed);
    assert_eq!(
        (at(&m, 2)?, at(&m, 3)?, at(&m, 4)?),
        (Status::Revealed, Status::Expired, Status::Absent)
    );
    assert_eq!(m.slot(2, REVEAL_END)?.reveal, Some(receipt));
    Ok(())
}

#[test]
fn a08_concurrent_and_repeated_requests_settle_once() -> TestResult {
    let mut m = market()?;
    let scores = m.both(250_000, 750_000);
    let report = m.signed(2, &scores)?;
    let binding = report.body.binding;
    let first = commitment_of(&report.body, SALT)?;
    let second = commitment_of(&report.body, [0x5b; 32])?;
    let a = Market::commit_call(2, &binding, first, 2, REVEAL_AT)?;
    let b = Market::commit_call(2, &binding, second, 2, REVEAL_AT)?;
    for call in [&a, &b] {
        let (outcome, _, _) = m.compose(call, &call.context(COMMIT_AT)?)?;
        assert!(matches!(outcome, Ok(F04::Committed { .. })));
    }
    let F04::Committed { record, .. } = m.call(&a, COMMIT_AT)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(m.refused(&b, COMMIT_AT)?, REPLAY_CONFLICT);
    let renewed = Market::commit_call(2, &binding, second, 3, REVEAL_AT)?;
    assert_eq!(m.refused(&renewed, COMMIT_AT)?, CONFLICT);
    let receipt = m.reveal(2, &report, REVEAL_AT)?;
    let revealed = m.world.bytes.clone();
    let retry = Market::reveal_call(2, &report, SALT, 3, REVEAL_END)?;
    let F04::Retained(retained) = m.call(&retry, REVEAL_AT + 2)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (retained.sequence, retained.result_digest),
        (3, receipt.result()?)
    );
    let changed_scores = m.both(250_001, 750_000);
    let changed = m.signed(2, &changed_scores)?;
    let conflicting = Market::reveal_call(2, &changed, SALT, 3, REVEAL_END)?;
    assert_eq!(m.refused(&conflicting, REVEAL_AT + 3)?, REPLAY_CONFLICT);
    let again = Market::reveal_call(2, &changed, SALT, 4, REVEAL_END)?;
    assert_eq!(m.refused(&again, REVEAL_AT + 3)?, CONFLICT);
    assert_eq!(m.world.bytes, revealed);
    let slot = m.slot(2, REVEAL_END)?;
    assert_eq!((slot.commit, slot.reveal), (Some(record), Some(receipt)));
    Ok(())
}

#[test]
fn a10_largest_reports_at_both_identity_bounds() -> TestResult {
    let mut c = crowded()?;
    let start = c.start();
    let pairs = c
        .workers
        .iter()
        .map(|w| (w.worker, 1_000_000))
        .collect::<Vec<_>>();
    let full = entries(&pairs);
    let reports = (2..=9)
        .map(|n| c.signed(n, &full))
        .collect::<CodecResult<Vec<_>>>()?;
    let mut bytes = vec![0; SIGNED_REPORT_MAX_BYTES];
    assert_eq!(codec::encode_report(&reports[0].body, &mut bytes)?, 1380);
    assert_eq!(encode_signed_report(&reports[0], &mut bytes)?, 1444);
    assert_eq!(
        reveal_payload(&reports[0], SALT)?.len(),
        commitment::REVEAL_MAX_BYTES
    );
    for (n, report) in (2..=9).zip(&reports) {
        c.commit(n, report, start + 64)?;
    }
    for (n, report) in (2..=9).zip(&reports) {
        c.reveal(n, report, start + 80)?;
    }
    let state = decode_shared_state(&c.world.bytes)?;
    let section = state.feature_sections[Section::CurrentReports.index()];
    assert_eq!(section.len(), 9 + 8 * 1526 + 9 + 8 * 96);
    let mut signed = 0;
    for n in 2..=9 {
        let row = f03::admitted_report(&state, 6, c.evaluator(n)?)?.ok_or(NOT_FOUND)?;
        signed += row.signed_bytes().len();
    }
    assert_eq!(signed, 8 * 1444);
    let stranger = derive_worker(c.header.market_id, principal(0x60)?, [0x60; 32])?;
    let mut over = pairs.clone();
    over.push((stranger, 1));
    over.sort_unstable_by_key(|(w, _)| *w);
    let over = entries(&over);
    let body = Market::body(c.binding(2)?, 2, &over)?;
    assert_eq!(codec::encode_report(&body, &mut bytes), Err(CAPACITY));
    let raw = raw_body(&reports[0].body, 33, &over)?;
    assert_eq!(codec::decode_report(&raw).map(|_| ()), Err(CAPACITY));
    let payload = raw_reveal(&raw, &reports[0].signature.0, &SALT)?;
    assert_eq!(
        commitment::decode_reveal_score(&payload).map(|_| ()),
        Err(CAPACITY)
    );
    let call = Req {
        payload,
        ..Market::reveal_call(2, &reports[0], SALT, 4, ROLE_EXPIRY)?
    };
    assert_eq!(call.encode().map(|_| ()), Err(NON_CANONICAL));
    Ok(())
}

#[test]
fn a10_window_arithmetic_overflow_refuses() -> TestResult {
    let origin = u64::MAX - 200;
    let world = World::create(origin)?;
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
    assert_eq!(
        epoch::preview_open(&world.bytes, origin + 128, &mut next, &mut scratch).map(|_| ()),
        Err(ARITHMETIC)
    );
    assert_eq!(
        HeightWindow::epoch(origin, 1, 80, 96).map(|_| ()),
        Err(ARITHMETIC)
    );
    assert_eq!(
        HeightWindow::epoch(origin, 2, 64, 80).map(|_| ()),
        Err(ARITHMETIC)
    );
    let market = world.parts()?.market()?.market_id;
    let evaluator = derive_evaluator(market, principal(2)?, [2; 32])?;
    assert_eq!(
        cr::slot(&world.bytes, evaluator, origin + 100).map(|_| ()),
        Err(WRONG_EPOCH)
    );
    Ok(())
}

#[test]
fn a11_expiry_is_a_projection_without_writes() -> TestResult {
    let mut m = market()?;
    let scores = m.both(1, 2);
    let report = m.signed(2, &scores)?;
    let record = m.commit(2, &report, COMMIT_AT)?;
    let committed = m.world.bytes.clone();
    for (at, status) in [
        (REVEAL_END - 1, Status::Committed),
        (REVEAL_END, Status::Expired),
        (1200, Status::Expired),
    ] {
        let slot = m.slot(2, at)?;
        assert_eq!(
            (slot.status, slot.commit, slot.reveal),
            (status, Some(record), None)
        );
    }
    assert_eq!(m.world.bytes, committed);
    let delayed = Market::reveal_call(2, &report, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&delayed, REVEAL_END)?, WRONG_PHASE);
    assert_eq!(m.slot(2, REVEAL_END)?.status, Status::Expired);
    assert_eq!(m.admitted(2)?, None);
    Ok(())
}

#[test]
fn a12_revocation_and_rotation_across_the_epoch_boundary() -> TestResult {
    let mut m = market()?;
    let scores = m.both(250_000, 750_000);
    let reports = (2..=5)
        .map(|n| m.signed(n, &scores))
        .collect::<CodecResult<Vec<_>>>()?;
    for (n, report) in (2..=5).zip(&reports) {
        m.commit(n, report, COMMIT_AT)?;
    }
    m.rotate(2, 1089)?;
    m.world.stage(&policy(2, 3)?, 8, 1089)?;
    m.revoke(3, false, 1090)?;
    let c = commitment_of(&reports[1].body, [0x5b; 32])?;
    let revoked = Market::commit_call(3, &reports[1].body.binding, c, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&revoked, 1091)?, REVOKED);
    for (n, report) in [(2, &reports[0]), (4, &reports[2]), (5, &reports[3])] {
        m.reveal(n, report, REVEAL_AT)?;
    }
    let late = Market::reveal_call(3, &reports[1], SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&late, REVEAL_AT)?, REVOKED);
    m.revoke(4, false, 1105)?;
    m.revoke(5, true, 1106)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let region =
        authority::evaluator_region(state.feature_sections[Section::IdentityRoster.index()])?;
    assert_eq!(
        (
            region.excluded(m.evaluator(4)?),
            region.excluded(m.evaluator(5)?)
        ),
        (true, false)
    );
    assert!(m.admitted(4)?.is_some() && m.admitted(5)?.is_some());
    let old = Market::commit_call(
        2,
        &reports[0].body.binding,
        commitment_of(&reports[0].body, SALT)?,
        2,
        REVEAL_AT,
    )?;
    let frozen = m.frozen;
    m.world.terminalize(&frozen, &m.workers, 1136)?;
    m.frozen = m.world.open(1152)?;
    assert_eq!(
        (m.frozen.epoch, m.frozen.config.get(), m.frozen.evaluators),
        (8, 2, 3)
    );
    let at = 1216;
    assert_eq!(m.refused(&old, at)?, EXPIRED);
    let previous_epoch = Market::commit_call(
        2,
        &reports[0].body.binding,
        commitment_of(&reports[0].body, SALT)?,
        4,
        ROLE_EXPIRY,
    )?;
    assert_eq!(m.refused(&previous_epoch, at)?, WRONG_EPOCH);
    let unrotated = m.signed(2, &scores)?;
    let call = Market::commit_call(
        2,
        &unrotated.body.binding,
        commitment_of(&unrotated.body, SALT)?,
        4,
        ROLE_EXPIRY,
    )?;
    assert_eq!(m.refused(&call, at)?, KEY_MISMATCH);
    assert_eq!(m.slot(2, at)?.status, Status::Absent);
    let binding = EvaluatorBinding {
        key_version: Version::new(2)?,
        ..m.binding(2)?
    };
    let rotated = sign(Market::body(binding, 2, &scores)?, &rotated_key(2))?;
    let c = commitment_of(&rotated.body, SALT)?;
    let F04::Committed { record, .. } =
        m.call(&Market::commit_call(2, &binding, c, 4, ROLE_EXPIRY)?, at)?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (record.key_version.get(), record.sequence, record.height),
        (2, 4, at)
    );
    Ok(())
}

#[test]
fn suspension_refuses_new_commitments_and_keeps_reveals() -> TestResult {
    let mut m = market()?;
    let scores = m.both(1, 2);
    let first = m.signed(2, &scores)?;
    let second = m.signed(3, &scores)?;
    m.commit(2, &first, COMMIT_AT)?;
    m.world.suspend(1089)?;
    let c = commitment_of(&second.body, SALT)?;
    let call = Market::commit_call(3, &second.body.binding, c, 2, ROLE_EXPIRY)?;
    assert_eq!(m.refused(&call, 1090)?, F08_MARKET_PAUSED);
    assert_eq!(m.slot(3, 1090)?.status, Status::Absent);
    let receipt = m.reveal(2, &first, REVEAL_AT)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}
