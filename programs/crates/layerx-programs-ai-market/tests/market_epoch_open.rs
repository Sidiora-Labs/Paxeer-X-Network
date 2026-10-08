//! AI.F01-T03 `OpenEpoch` and `ADVANCE_ACTIVATION` over the complete shared state value:
//! the real F01 CREATE/STAGE/CANCEL/SCHEDULE transitions, F02 worker records, F03 grants
//! accepted through signed F08 consent, F06 funding and terminalization.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        Envelope, Roster,
    },
    dispatch::{self, Operation},
    epoch::{
        self, Activated, Frozen, Outcome as Opening, Readiness, ADVANCE_SCRATCH_BYTES,
        OPEN_SCRATCH_BYTES,
    },
    errors::{
        CodecResult, ARITHMETIC, F01_ACTIVATION_NOT_READY, F01_ACTIVATION_TOO_EARLY,
        F01_ALREADY_ACTIVATED, F01_VERSION_MISMATCH, NON_CANONICAL, NOT_FOUND, READINESS_BLOCKED,
        ROLE_CONFLICT, WRONG_CONFIG, WRONG_EPOCH, WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, market_clock, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection, ACTIVE, REGISTERED},
    reward_math::allocate,
    rewards::{
        decode_reward_state, EpochStatus, FundReplay, FundRequest, FundingAuthority, FundingPhase,
        RewardLedger, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorRosterEntry,
        FrozenBinding, MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32,
        RequestDigest, RequestId, ResultDigest, RosterDigest, RubricDigest, Version, WorkerId,
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
const ORIGIN: u64 = 1000;

fn principal(n: u8) -> CodecResult<PrincipalId> {
    let mut bytes = [0x50; 32];
    bytes[0] = n;
    PrincipalId::new(bytes)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn height(origin: u64, epoch: u64, offset: u64) -> u64 {
    origin + epoch * 128 + offset
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

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

struct Call<'a> {
    operation: Operation,
    actor: PrincipalId,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    request: u8,
    payload: &'a [u8],
}
impl Call<'_> {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = ChainDomain::new(CHAIN)?;
        let program = ProgramId::new(PROGRAM)?;
        let envelope = Envelope {
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
            request: RequestId::new([self.request; 32])?,
            payload: self.payload,
            authentication: Authentication::Native,
        };
        let mut encoded = vec![0; 32_768];
        let n = encode_envelope(&envelope, &mut encoded)?;
        encoded.truncate(n);
        Ok(encoded)
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
    let call = Call {
        operation: dispatch::CREATE,
        actor: PrincipalId::new(OWNER)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        request: 1,
        payload: &payload,
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
                feature_bytes: &[],
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
        expiry_height: height(market.origin_height, effective, 64),
    };
    let digest = table.approve(&ctx(market, market.owner_principal, at), &terms)?;
    Ok((effective, digest))
}

fn worker_record(
    worker: WorkerId,
    owner: PrincipalId,
    slot: u8,
    at: u64,
) -> CodecResult<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker,
        owner,
        delegate: PublicKey32([0x33; 32]),
        metadata: MetadataDigest::new([0x44; 32])?,
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
fn evaluator_entry(grant: &EvaluatorGrant) -> EvaluatorRosterEntry {
    EvaluatorRosterEntry {
        evaluator: grant.evaluator,
        owner: grant.principal,
        grant: grant.grant_version,
        key_version: grant.key_version,
        public_key: grant.signing_key,
        rubric: grant.rubric,
    }
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
    fn rewards(&self) -> CodecResult<Vec<u8>> {
        Ok(self.parts()?.rewards)
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
    fn owner_op(&mut self, operation: Operation, payload: &[u8], at: u64) -> TestResult {
        let call = Call {
            operation,
            actor: PrincipalId::new(OWNER)?,
            epoch: 0,
            config: self.section()?.header.active_config_version,
            roster: Presence::Absent,
            sequence: self.owner_sequence,
            request: 0x20 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?,
            payload,
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
        let next = encode(&state)?;
        self.bytes = next;
        self.owner_sequence += 1;
        Ok(())
    }
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.expected_revision_bytes()?;
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, &payload, at)
    }
    fn stage(&mut self, policy: &TaskPolicyV1, effective: u64, at: u64) -> TestResult {
        let mut bytes = [0; TASK_POLICY_BYTES];
        policy.encode(&mut bytes)?;
        let mut payload = self.expected_revision_bytes()?;
        payload.extend_from_slice(&bytes);
        payload.extend_from_slice(&effective.to_be_bytes());
        self.owner_op(dispatch::STAGE_POLICY, &payload, at)
    }
    fn cancel(&mut self, config: u64, at: u64) -> TestResult {
        let mut payload = self.expected_revision_bytes()?;
        payload.extend_from_slice(&config.to_be_bytes());
        self.owner_op(dispatch::CANCEL_POLICY, &payload, at)
    }
    fn expected_revision_bytes(&self) -> CodecResult<Vec<u8>> {
        Ok(self.revision()?.to_be_bytes().to_vec())
    }
}

/// Worker, evaluator, funding and settlement producers of the market journey.
impl World {
    /// F02 ENROLLED record, worker replay slot and F08 approval plus owner acceptance.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<WorkerRosterEntry> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let worker = derive_worker(market.market_id, owner, [n; 32])?;
            let slot = parts.workers.free_slot()?;
            let record = worker_record(worker, owner, slot, at)?;
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
            Ok(WorkerRosterEntry {
                worker,
                owner,
                recipient: AccountId::new(owner.bytes())?,
                generation: version()?,
                key_version: version()?,
                public_key: record.delegate,
                metadata: record.metadata,
            })
        })
    }
    /// F03 nomination accepted through the signed F08 evaluator consent of its delegate.
    fn evaluator(&mut self, n: u8, at: u64) -> CodecResult<EvaluatorRosterEntry> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let key = SigningKey::from_bytes(&[n; 32]);
            let signing_key = PublicKey32(key.verifying_key().to_bytes());
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
                expiry_height: height(market.origin_height, effective, 64),
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
            parts.insert_grant(grant, n)?;
            Ok(evaluator_entry(&grant))
        })
    }
    /// Restored committed membership that F08 admission itself refuses up front: an
    /// evaluator owned by `owner` signing with `key`, staged for the first opening.
    fn restored_evaluator(
        &mut self,
        owner: PrincipalId,
        nonce: u8,
        key: PublicKey32,
        like: &EvaluatorRosterEntry,
    ) -> TestResult {
        self.edit(|parts, market| {
            let template = parts
                .admission
                .get(Participant::Evaluator(like.evaluator))
                .ok_or(NOT_FOUND)?;
            let grant = grant(market, owner, nonce, key, 0)?;
            parts.admission.insert(AdmissionMeta {
                participant: Participant::Evaluator(grant.evaluator),
                owner,
                ..template
            })?;
            parts.insert_grant(grant, nonce)
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
        let tag = 0x40 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?;
        let request = ReplayRequest {
            slot: ActorSlot::OWNER,
            principal: market.owner_principal,
            authority_version: version()?,
            sequence: self.owner_sequence,
            request_id: RequestId::new([tag; 32])?,
            digest: RequestDigest::new([tag; 32])?,
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
                result: ResultDigest::new([tag; 32])?,
            },
            &mut funded,
        )?;
        parts.rewards = funded;
        self.bytes = parts.encode()?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// F05 terminal result for the single frozen worker, then `TerminalizeRewards`.
    fn terminalize(&mut self, frozen: &Frozen, entry: &WorkerRosterEntry, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let binding = FrozenBinding {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                epoch: frozen.epoch,
                config: frozen.config,
                roster: frozen.roster,
            };
            let roster = [*entry];
            let outputs = [WorkerAggregate::new(
                entry.worker,
                entry.generation,
                3,
                QualityStatus::ScoredPositive,
                5,
                5,
            )?];
            let allocation = allocate(frozen.budget, &outputs)?;
            let aggregation =
                EpochAggregation::structural(binding, Digest32::new([5; 32])?, &roster, &outputs)?;
            let mut terminal = vec![0; REWARD_STATE_BYTES];
            decode_reward_state(&parts.rewards)?.terminalize(
                &binding,
                aggregation.root(),
                &allocation,
                &roster,
                at,
                &mut terminal,
            )?;
            parts.rewards = terminal;
            Ok(())
        })
    }
}

/// The `OPEN_EPOCH` and `ADVANCE_ACTIVATION` calls under test.
impl World {
    fn preview(&self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)
    }
    /// Permissionless object-local `OPEN_EPOCH`; commits only on `Opened`.
    fn open_with(
        &mut self,
        at: u64,
        epoch: u64,
        config: u64,
        roster: Presence<RosterDigest>,
    ) -> CodecResult<Opening> {
        let call = Call {
            operation: dispatch::OPEN_EPOCH,
            actor: principal(KEEPER)?,
            epoch,
            config,
            roster,
            sequence: 0,
            request: 0x60,
            payload: &[],
        };
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = epoch::open_epoch(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        if let Opening::Opened { state_len, .. } = outcome {
            next.truncate(state_len);
            self.bytes = next;
        }
        Ok(outcome)
    }
    /// Opens the clock epoch of `at` naming the previewed frozen config and roster.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let preview = self.preview(at)?;
        match self.open_with(
            at,
            preview.epoch,
            preview.config.get(),
            Presence::Present(preview.roster),
        )? {
            Opening::Opened { frozen, .. } => Ok(frozen),
            Opening::AlreadyApplied { .. } => Err(NON_CANONICAL),
        }
    }
    fn readiness(&self, at: u64) -> CodecResult<Readiness> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; ADVANCE_SCRATCH_BYTES];
        epoch::activation_readiness(&self.bytes, at, &mut next, &mut scratch)
    }
    /// Permissionless object-local `ADVANCE_ACTIVATION`; commits only on success.
    fn advance(&mut self, at: u64) -> CodecResult<Activated> {
        let section = self.section()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&section.header.activation_epoch.to_be_bytes());
        let call = Call {
            operation: dispatch::ADVANCE_ACTIVATION,
            actor: principal(KEEPER)?,
            epoch: 0,
            config: section.header.active_config_version,
            roster: Presence::Absent,
            sequence: 0,
            request: 0x61,
            payload: &payload,
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
        Ok(activated)
    }
}

/// One worker and three accepted evaluators staged before the first opening.
fn staff(world: &mut World) -> CodecResult<(WorkerRosterEntry, Vec<EvaluatorRosterEntry>)> {
    let origin = world.parts()?.market()?.origin_height;
    let worker = world.enroll(1, origin + 2)?;
    let mut evaluators = Vec::new();
    for n in 2..=4 {
        evaluators.push(world.evaluator(n, origin + 2 + u64::from(n))?);
    }
    Ok((worker, evaluators))
}
fn staffed(origin: u64) -> CodecResult<(World, WorkerRosterEntry, Vec<EvaluatorRosterEntry>)> {
    let mut world = World::create(origin)?;
    let (worker, evaluators) = staff(&mut world)?;
    Ok((world, worker, evaluators))
}

fn roster_digest(
    market: &MarketHeader,
    epoch: u64,
    config: u64,
    worker: &WorkerRosterEntry,
    evaluators: &[EvaluatorRosterEntry],
) -> CodecResult<RosterDigest> {
    let mut evaluators = evaluators.to_vec();
    evaluators.sort_by_key(|e| e.evaluator);
    codec::roster_digest(&Roster {
        market: market.market_id,
        epoch,
        config: Version::new(config)?,
        workers: core::slice::from_ref(worker),
        evaluators: &evaluators,
    })
}

/// A complete committed state whose F01 header revision equals the shared revision.
fn assert_canonical(world: &World) -> TestResult {
    assert_eq!(world.section()?.header.state_revision, world.revision()?);
    Ok(())
}

#[test]
fn a02_activation_needs_work_window_and_complete_readiness() -> TestResult {
    let mut world = World::create(ORIGIN)?;
    assert_eq!(world.schedule(0, 1001), Err(F01_ACTIVATION_TOO_EARLY));
    world.schedule(1, 1001)?;
    staff(&mut world)?;
    let scheduled = world.bytes.clone();
    assert_eq!(world.advance(1127), Err(WRONG_PHASE));
    assert_eq!(world.bytes, scheduled);
    let header = world.section()?.header;
    assert_eq!(
        (
            header.lifecycle,
            header.activation_scheduled,
            header.activation_epoch
        ),
        (REGISTERED, true, 1)
    );
    assert_eq!(world.advance(1128), Err(F01_ACTIVATION_NOT_READY));
    assert_eq!(world.readiness(1128), Err(READINESS_BLOCKED));
    assert_eq!(world.bytes, scheduled);

    world.fund(500, 1128)?;
    let revision = world.revision()?;
    assert_eq!(
        world.readiness(1128)?,
        Readiness {
            epoch: 1,
            workers: 1,
            evaluators: 3,
            budget: 100
        }
    );
    let activated = world.advance(1128)?;
    assert_eq!(
        (activated.activation_epoch, activated.revision),
        (1, revision + 1)
    );
    assert_eq!(world.revision()?, revision + 1);
    assert_canonical(&world)?;
    let header = world.section()?.header;
    assert_eq!(
        (header.lifecycle, header.activation_scheduled),
        (ACTIVE, false)
    );
    let parts = world.parts()?;
    assert_eq!(parts.admission.current_epoch(), None);
    let ledger = decode_reward_state(&parts.rewards)?.ledger()?;
    assert_eq!((ledger.free, ledger.reserved), (500, 0));
    assert_eq!(world.advance(1129), Err(F01_ALREADY_ACTIVATED));

    let frozen = world.open(1129)?;
    assert_eq!(
        (frozen.epoch, frozen.previous, frozen.skipped, frozen.budget),
        (1, None, 1, 100)
    );
    let rewards = world.rewards()?;
    let rewards = decode_reward_state(&rewards)?;
    assert_eq!(rewards.row(0).map(|_| ()), Err(NOT_FOUND));
    assert_eq!(rewards.row(1)?.budget, 100);
    assert_eq!(rewards.ledger()?.reserved, 100);
    Ok(())
}

#[test]
fn a02_activation_refuses_fewer_than_three_evaluators() -> TestResult {
    let mut world = World::create(ORIGIN)?;
    world.enroll(1, 1002)?;
    world.evaluator(2, 1003)?;
    world.evaluator(3, 1004)?;
    world.schedule(1, 1005)?;
    world.fund(500, 1006)?;
    let before = world.bytes.clone();
    assert_eq!(world.advance(1128), Err(F01_ACTIVATION_NOT_READY));
    assert_eq!(world.readiness(1128), Err(READINESS_BLOCKED));
    assert_eq!(world.preview(1128), Err(READINESS_BLOCKED));
    assert_eq!(world.bytes, before);
    let section = world.section()?;
    assert_eq!(
        (
            section.header.lifecycle,
            section.header.activation_scheduled
        ),
        (REGISTERED, true)
    );
    Ok(())
}

#[test]
fn advance_reads_an_already_opened_epoch() -> TestResult {
    let (mut world, _, _) = staffed(ORIGIN)?;
    world.schedule(1, 1006)?;
    world.fund(500, 1007)?;
    let frozen = world.open(1128)?;
    assert_eq!(frozen.epoch, 1);
    let opened = world.bytes.clone();
    let readiness = world.readiness(1129)?;
    assert_eq!(
        readiness,
        Readiness {
            epoch: 1,
            workers: 1,
            evaluators: 3,
            budget: 100
        }
    );
    let activated = world.advance(1129)?;
    assert_eq!(activated.readiness, readiness);
    assert_canonical(&world)?;
    let after = decode_shared_state(&world.bytes)?.feature_sections;
    let before = decode_shared_state(&opened)?.feature_sections;
    assert_eq!(after[1..], before[1..]);
    Ok(())
}

#[test]
fn open_freezes_roster_recipients_and_budget() -> TestResult {
    let (mut world, worker, evaluators) = staffed(ORIGIN)?;
    world.fund(250, 1007)?;
    let market = world.parts()?.market()?;
    let expected = roster_digest(&market, 1, 1, &worker, &evaluators)?;
    let revision = world.revision()?;
    let committed = world.bytes.clone();
    assert_eq!(
        world.open_with(1128, 1, 1, Presence::Present(RosterDigest::new([9; 32])?)),
        Err(WRONG_ROSTER)
    );
    assert_eq!(
        world.open_with(1128, 1, 2, Presence::Present(expected)),
        Err(WRONG_CONFIG)
    );
    assert_eq!(
        world.open_with(1128, 2, 1, Presence::Present(expected)),
        Err(WRONG_EPOCH)
    );
    assert_eq!(
        world.open_with(1192, 1, 1, Presence::Present(expected)),
        Err(WRONG_PHASE)
    );
    assert_eq!(world.bytes, committed);
    let Opening::Opened {
        frozen,
        revision: opened,
        ..
    } = world.open_with(1128, 1, 1, Presence::Present(expected))?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(opened, revision + 1);
    assert_eq!(world.revision()?, revision + 1);
    assert_canonical(&world)?;
    assert_eq!(
        (
            frozen.roster,
            frozen.workers,
            frozen.evaluators,
            frozen.budget
        ),
        (expected, 1, 3, 100)
    );
    assert_eq!(
        (frozen.config.get(), frozen.policy, frozen.policy_activated),
        (1, policy(1, 3)?.digest()?, false)
    );
    let rewards = world.rewards()?;
    let row = decode_reward_state(&rewards)?.row(1)?;
    assert_eq!(
        (row.status, row.budget, row.roster, row.entries().len()),
        (EpochStatus::Reserved, 100, expected, 1)
    );
    let parts = world.parts()?;
    assert_eq!(parts.admission.current_epoch(), Some(1));
    let meta = parts
        .admission
        .get(Participant::Worker(worker.worker))
        .ok_or(NOT_FOUND)?;
    assert_eq!(meta.admitted_epoch, Some(1));
    let region = EvaluatorRegion::decode(&parts.region)?;
    let snapshot = region.snapshot().ok_or(NOT_FOUND)?;
    assert_eq!((snapshot.epoch, snapshot.len()), (1, 3));
    assert_eq!(
        world.open_with(1129, 1, 1, Presence::Present(expected))?,
        Opening::AlreadyApplied {
            epoch: 1,
            roster: expected
        }
    );
    assert_eq!(world.revision()?, revision + 1);
    assert_eq!(
        world.open_with(1129, 1, 1, Presence::Present(RosterDigest::new([9; 32])?)),
        Err(WRONG_ROSTER)
    );
    assert_eq!(world.open(1256), Err(WRONG_PHASE));
    Ok(())
}

/// Opens epoch 1 under config1 and stages `staged` effective at epoch 2 at height 1130.
fn staged_journey(staged: &TaskPolicyV1) -> CodecResult<World> {
    let (mut world, worker, _) = staffed(ORIGIN)?;
    world.fund(1000, 1007)?;
    let first = world.open(1128)?;
    assert_eq!((first.epoch, first.config.get()), (1, 1));
    world.stage(staged, 2, 1130)?;
    let section = world.section()?;
    assert_eq!(section.header.active_config_version, 1);
    assert_eq!(section.current, policy(1, 3)?);
    assert!(matches!(section.pending, Presence::Present(p) if p.effective_epoch == 2));
    world.terminalize(&first, &worker, 1240)?;
    Ok(world)
}

#[test]
fn a03_opening_freezes_the_due_pending_policy() -> TestResult {
    let config2 = policy(2, 3)?;
    let mut world = staged_journey(&config2)?;
    let revision = world.revision()?;
    let frozen = world.open(1256)?;
    assert_eq!(
        (frozen.epoch, frozen.previous, frozen.skipped),
        (2, Some(1), 0)
    );
    assert_eq!(
        (frozen.config.get(), frozen.policy, frozen.policy_activated),
        (2, config2.digest()?, true)
    );
    assert_eq!(world.revision()?, revision + 1);
    assert_canonical(&world)?;
    let section = world.section()?;
    assert_eq!(
        (
            section.header.active_config_version,
            section.current,
            section.pending
        ),
        (2, config2, Presence::Absent)
    );
    let rewards = world.rewards()?;
    assert_eq!(decode_reward_state(&rewards)?.row(2)?.budget, 100);
    Ok(())
}

#[test]
fn a03_readiness_refusal_keeps_config1_and_pending2() -> TestResult {
    let mut world = staged_journey(&policy(2, 4)?)?;
    let committed = world.bytes.clone();
    assert_eq!(world.open(1256), Err(READINESS_BLOCKED));
    let market = world.parts()?.market()?;
    assert_eq!(
        world.open_with(1256, 2, 2, Presence::Present(RosterDigest::new([9; 32])?)),
        Err(READINESS_BLOCKED)
    );
    assert_eq!(world.bytes, committed);
    let section = world.section()?;
    assert_eq!(section.header.active_config_version, 1);
    assert_eq!(section.current, policy(1, 3)?);
    assert!(matches!(section.pending, Presence::Present(p) if p.policy == policy(2, 4)?));
    assert_eq!(world.parts()?.admission.current_epoch(), Some(1));
    assert_eq!(
        decode_reward_state(&world.rewards()?)?.ledger()?.reserved,
        0
    );
    assert_eq!(market.active_config_version, 1);

    world.cancel(2, 1257)?;
    assert_eq!(
        world.stage(&policy(2, 3)?, 3, 1258),
        Err(F01_VERSION_MISMATCH)
    );
    world.stage(&policy(3, 3)?, 3, 1258)?;
    assert_eq!(world.section()?.header.highest_config_version, 3);
    let frozen = world.open(1259)?;
    assert_eq!(
        (frozen.epoch, frozen.config.get(), frozen.policy_activated),
        (2, 1, false)
    );
    Ok(())
}

#[test]
fn a04_worker_principal_cannot_also_evaluate() -> TestResult {
    let (mut world, worker, evaluators) = staffed(ORIGIN)?;
    world.restored_evaluator(worker.owner, 0x41, PublicKey32([0x71; 32]), &evaluators[0])?;
    world.fund(500, 1010)?;
    let committed = world.bytes.clone();
    assert_eq!(world.preview(1128), Err(ROLE_CONFLICT));
    let market = world.parts()?.market()?;
    let digest = roster_digest(&market, 1, 1, &worker, &evaluators)?;
    assert_eq!(
        world.open_with(1128, 1, 1, Presence::Present(digest)),
        Err(ROLE_CONFLICT)
    );
    assert_eq!(world.bytes, committed);
    let parts = world.parts()?;
    assert_eq!(parts.admission.current_epoch(), None);
    assert!(EvaluatorRegion::decode(&parts.region)?.snapshot().is_none());
    assert_eq!(decode_reward_state(&parts.rewards)?.ledger()?.reserved, 0);
    Ok(())
}

#[test]
fn a04_owner_cannot_evaluate_through_a_new_delegate() -> TestResult {
    let (mut world, _, evaluators) = staffed(ORIGIN)?;
    let owner = PrincipalId::new(OWNER)?;
    world.restored_evaluator(owner, 0x42, PublicKey32([0x72; 32]), &evaluators[0])?;
    world.fund(500, 1010)?;
    let committed = world.bytes.clone();
    assert_eq!(world.preview(1128), Err(ROLE_CONFLICT));
    assert_eq!(world.bytes, committed);
    Ok(())
}

#[test]
fn a04_evaluator_sharing_a_worker_key_is_not_independent() -> TestResult {
    let (mut world, worker, evaluators) = staffed(ORIGIN)?;
    world.restored_evaluator(principal(5)?, 0x43, worker.public_key, &evaluators[0])?;
    world.fund(500, 1010)?;
    assert_eq!(world.preview(1128), Err(ROLE_CONFLICT));
    Ok(())
}

#[test]
fn f04_a10_epoch_end_overflow_refuses_without_mutation() -> TestResult {
    let origin = u64::MAX - 200;
    let mut world = World::create(origin)?;
    assert_eq!(
        market_clock(origin, origin + 128).map(|c| c.epoch),
        Err(ARITHMETIC)
    );
    let committed = world.bytes.clone();
    assert_eq!(world.preview(origin + 128), Err(ARITHMETIC));
    assert_eq!(
        world.open_with(
            origin + 128,
            1,
            1,
            Presence::Present(RosterDigest::new([9; 32])?)
        ),
        Err(ARITHMETIC)
    );
    assert_eq!(world.bytes, committed);
    assert_eq!(world.preview(origin), Err(READINESS_BLOCKED));
    Ok(())
}
