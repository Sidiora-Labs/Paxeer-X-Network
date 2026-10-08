//! AI.F08-T02 `RolloverRoster` over the complete shared state value: the real F01
//! CREATE policy section, F02 worker table, F06 reward state and F08 admission table.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, ExitReason, Participant, RemovalReason, Role, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope, Envelope,
    },
    dispatch,
    errors::{
        CodecResult, ACCOUNT_BINDING, ARITHMETIC, CAPACITY, F06_UNKNOWN_WORKER_ENTITLEMENT,
        F08_CAPACITY_EXCEEDED, F08_DELEGATE_REVOKED, F08_MARKET_PAUSED, F08_NO_PRUNABLE_MEMBER,
        F08_QUORUM_UNAVAILABLE, F08_RETENTION_BLOCKED, F08_STALE_STATE, F08_WRONG_GENERATION,
        NON_CANONICAL, NOT_FOUND, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, market_clock, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection, ACTIVE, CLOSED, SUSPENDED},
    reward_math::allocate,
    rewards::{
        decode_reward_state, ClaimDecision, FundReplay, FundRequest, FundingAuthority,
        FundingPhase, RewardLedger, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    roster::{self, RosterOpened, ROLLOVER_SCRATCH_BYTES},
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, FrozenBinding, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Version, WorkerId, WorkerRosterEntry,
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

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// Committed state after the real F01 CREATE at `origin` (lifecycle REGISTERED).
fn create(origin: u64) -> CodecResult<Vec<u8>> {
    let chain = ChainDomain::new(CHAIN)?;
    let program = ProgramId::new(PROGRAM)?;
    let policy = TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: Digest32::new([1; 32])?,
            dataset_artifact: [2; 32],
            benchmark_suite: Digest32::new([3; 32])?,
            rubric: RubricDigest::new([4; 32])?,
            task_schema: Digest32::new([5; 32])?,
            result_schema: Digest32::new([6; 32])?,
            service_terms: Digest32::new([7; 32])?,
        },
        100,
        1,
    )?;
    let mut policy_bytes = [0; TASK_POLICY_BYTES];
    policy.encode(&mut policy_bytes)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&policy_bytes);
    payload.extend_from_slice(&[16; 32]);
    let envelope = Envelope {
        operation: dispatch::CREATE,
        chain,
        program,
        market: derive_market(chain, program)?,
        actor: PrincipalId::new(OWNER)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        expiry: 1_000_000,
        request: RequestId::new([1; 32])?,
        payload: &payload,
        authentication: Authentication::Native,
    };
    let mut encoded = vec![0; 16_384];
    let n = encode_envelope(&envelope, &mut encoded)?;
    let ctx = CallContext {
        chain,
        program,
        principal: PrincipalId::new(OWNER)?,
        height: origin,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let validated = decode_envelope(&encoded[..n])?;
    let Outcome::Applied { state, .. } =
        registry_ops::apply(&ctx, None, &validated, &mut section, &mut event)?
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
    fn set_lifecycle(&mut self, lifecycle: u8) -> TestResult {
        let mut section = PolicySection::decode(&self.policy)?;
        section.header.lifecycle = lifecycle;
        let mut out = vec![0; section.encoded_len()?];
        section.encode(&mut out)?;
        self.policy = out;
        Ok(())
    }
    fn encode(&self, revision: u64) -> CodecResult<Vec<u8>> {
        let mut identity = vec![0; WORKER_TABLE_MAX_BYTES];
        let workers_len = self.workers.encode(&mut identity)?;
        identity.truncate(workers_len);
        identity.extend_from_slice(&self.region);
        let mut admission = vec![0; TABLE_MAX_BYTES];
        let admission_len = self.admission.encode(&mut admission)?;
        encode(&SharedState {
            revision,
            feature_sections: [
                &self.policy,
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
}

/// One market's committed shared state bytes.
struct World(Vec<u8>);
impl World {
    fn create(origin: u64) -> CodecResult<Self> {
        Ok(Self(create(origin)?))
    }
    fn parts(&self) -> CodecResult<Parts> {
        Parts::load(&self.0)
    }
    fn revision(&self) -> CodecResult<u64> {
        Ok(decode_shared_state(&self.0)?.revision)
    }
    fn section(&self, section: Section) -> CodecResult<Vec<u8>> {
        Ok(decode_shared_state(&self.0)?.feature_sections[section.index()].to_vec())
    }
    fn meta(&self, participant: Participant) -> CodecResult<AdmissionMeta> {
        self.parts()?.admission.get(participant).ok_or(NOT_FOUND)
    }
    fn record(&self, worker: WorkerId) -> CodecResult<WorkerCurrent> {
        self.parts()?.workers.get(worker).ok_or(NOT_FOUND)
    }
    /// Applies `change` to the decoded sections and commits it at the next revision;
    /// a refusal commits nothing.
    fn edit<T>(
        &mut self,
        change: impl FnOnce(&mut Parts, &MarketHeader) -> CodecResult<T>,
    ) -> CodecResult<T> {
        let mut parts = self.parts()?;
        let market = parts.market()?;
        let out = change(&mut parts, &market)?;
        let revision = parts.revision.checked_add(1).ok_or(ARITHMETIC)?;
        self.0 = parts.encode(revision)?;
        Ok(out)
    }
    fn rollover(
        &self,
        at: u64,
        revision: u64,
        next: usize,
        scratch: usize,
    ) -> CodecResult<(RosterOpened, Vec<u8>)> {
        let mut out = vec![0; next];
        let mut work = vec![0; scratch];
        let opened = roster::rollover_roster(&self.0, at, revision, &mut out, &mut work)?;
        out.truncate(opened.state_len);
        Ok((opened, out))
    }
    fn open(&mut self, at: u64) -> CodecResult<RosterOpened> {
        let (opened, next) = self.rollover(
            at,
            self.revision()?,
            MAX_STATE_BYTES,
            ROLLOVER_SCRATCH_BYTES,
        )?;
        self.0 = next;
        Ok(opened)
    }
    /// F02 ENROLLED record, worker replay slot and F08 approval plus owner acceptance.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<WorkerId> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let worker = derive_worker(market.market_id, owner, [n; 32])?;
            let slot = parts.workers.free_slot()?;
            let effective = market_clock(market.origin_height, at)?.epoch + 1;
            parts
                .workers
                .insert(&worker_record(worker, owner, slot, effective, at)?)?;
            parts
                .replay
                .bind(ActorSlot::worker(usize::from(slot))?, owner, version()?)?;
            admit(
                &mut parts.admission,
                market,
                Participant::Worker(worker),
                owner,
                at,
            )?;
            Ok(worker)
        })
    }
    fn evaluator(&mut self, n: u8, at: u64) -> CodecResult<Participant> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let participant =
                Participant::Evaluator(derive_evaluator(market.market_id, owner, [n; 32])?);
            admit(&mut parts.admission, market, participant, owner, at)?;
            Ok(participant)
        })
    }
    fn change_record(
        &mut self,
        worker: WorkerId,
        change: impl FnOnce(&mut WorkerCurrent),
    ) -> TestResult {
        self.edit(|parts, _| {
            let mut record = parts.workers.get(worker).ok_or(NOT_FOUND)?;
            change(&mut record);
            parts.workers.replace(&record)
        })
    }
}

fn worker_record(
    worker: WorkerId,
    owner: PrincipalId,
    slot: u8,
    effective: u64,
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
        effective_epoch: effective,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Enrolled,
        slot,
        last_metadata_height: at,
    })
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
fn admit(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    owner: PrincipalId,
    at: u64,
) -> CodecResult<AdmissionMeta> {
    let (effective, digest) = approve(table, market, participant, owner, PublicKey32([8; 32]), at)?;
    table.admit(
        &ctx(market, owner, at),
        &Admission {
            participant,
            delegate_generation: 1,
            effective_epoch: effective,
            config_version: 1,
            approval_digest: digest,
        },
    )
}

#[test]
fn a01_enrollment_stages_then_one_rollover_installs() -> TestResult {
    let mut world = World::create(0)?;
    let opened = world.open(512)?;
    assert_eq!(
        (opened.rollover.epoch, opened.previous, opened.skipped),
        (4, None, 4)
    );
    assert_eq!(opened.rollover.members, 0);
    let worker = world.enroll(1, 520)?;
    let staged = world.meta(Participant::Worker(worker))?;
    assert_eq!(
        (staged.admitted_epoch, staged.immunity_until_epoch),
        (None, 5)
    );
    assert_eq!(
        roster::health(&world.parts()?.admission)?.roster_evaluators,
        0
    );
    let identity = world.section(Section::IdentityRoster)?;
    let opened = world.open(640)?;
    assert_eq!(
        (opened.rollover.installed, opened.workers, opened.evaluators),
        (1, 1, 0)
    );
    assert_eq!((opened.previous, opened.skipped), (Some(4), 0));
    assert_eq!(
        world.meta(Participant::Worker(worker))?.admitted_epoch,
        Some(5)
    );
    assert_eq!(world.section(Section::IdentityRoster)?, identity);
    assert!(world.section(Section::SettlementClaims)?.is_empty());
    assert_eq!(
        opened.rollover.digest,
        roster::roster_digest(&world.parts()?.admission, 5)?
    );
    assert_eq!(world.open(641), Err(WRONG_EPOCH));
    Ok(())
}

#[test]
fn a02_capacity_refuses_until_exit_frees_the_slot() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let mut workers = Vec::new();
    for epoch in 0..8u8 {
        for k in 0..4u8 {
            let at = height(0, u64::from(epoch), 1 + u64::from(k));
            workers.push(world.enroll(epoch * 4 + k + 1, at)?);
        }
        world.open(height(0, u64::from(epoch) + 1, 0))?;
    }
    let mut parts = world.parts()?;
    let market = parts.market()?;
    assert_eq!(parts.workers.len(), 32);
    assert_eq!(parts.workers.free_slot(), Err(CAPACITY));
    let extra = derive_worker(market.market_id, principal(40)?, [40; 32])?;
    assert_eq!(
        parts
            .workers
            .insert(&worker_record(extra, principal(40)?, 0, 9, 1030)?),
        Err(CAPACITY)
    );
    let before = parts.admission;
    assert_eq!(
        approve(
            &mut parts.admission,
            &market,
            Participant::Worker(extra),
            principal(40)?,
            PublicKey32([8; 32]),
            1030,
        ),
        Err(F08_CAPACITY_EXCEEDED)
    );
    assert_eq!(parts.admission, before);
    let leaving = workers[2];
    let slot = world.record(leaving)?.slot;
    world.edit(|parts, market| {
        parts.admission.request_exit(
            &ctx(market, principal(3)?, 1030),
            Participant::Worker(leaving),
            1,
            9,
            ExitReason::Voluntary,
        )
    })?;
    let opened = world.open(height(0, 9, 0))?;
    assert_eq!((opened.rollover.removed, opened.workers), (1, 31));
    let parts = world.parts()?;
    assert_eq!(parts.workers.len(), 31);
    assert_eq!(parts.workers.get(leaving), None);
    assert_eq!(
        parts.replay.actor(ActorSlot::worker(usize::from(slot))?),
        None
    );
    assert_eq!(parts.workers.free_slot()?, slot);
    let newcomer = world.enroll(40, height(0, 9, 1))?;
    assert_eq!(world.record(newcomer)?.slot, slot);
    let staged = world.meta(Participant::Worker(newcomer))?;
    assert_eq!(
        (staged.admitted_epoch, staged.immunity_until_epoch),
        (None, 10)
    );
    let opened = world.open(height(0, 10, 0))?;
    assert_eq!((opened.rollover.installed, opened.workers), (1, 32));
    Ok(())
}

#[test]
fn a05_missed_epochs_count_opened_snapshots_only() -> TestResult {
    let mut world = World::create(0)?;
    world.open(height(0, 6, 0))?;
    let worker = Participant::Worker(world.enroll(1, height(0, 6, 1))?);
    let mut skipping = World(world.0.clone());
    world.open(height(0, 7, 0))?;
    world.open(height(0, 8, 0))?;
    assert_eq!(world.meta(worker)?.complete_missed_opened_epochs, 0);
    world.open(height(0, 9, 0))?;
    assert_eq!(world.meta(worker)?.complete_missed_opened_epochs, 1);
    assert_eq!(
        roster::prune_candidate(&world.parts()?.admission, Role::Worker, 9),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    world.open(height(0, 10, 0))?;
    assert_eq!(world.meta(worker)?.complete_missed_opened_epochs, 2);
    assert_eq!(
        roster::prune_candidate(&world.parts()?.admission, Role::Worker, 10)?.participant,
        worker
    );
    world.edit(|parts, market| {
        parts.admission.heartbeat(
            &ctx(market, principal(1)?, height(0, 10, 5)),
            worker,
            1,
            1,
            10,
        )
    })?;
    assert_eq!(
        roster::prune_candidate(&world.parts()?.admission, Role::Worker, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    world.open(height(0, 11, 0))?;
    assert_eq!(world.meta(worker)?.complete_missed_opened_epochs, 0);

    skipping.open(height(0, 7, 0))?;
    let opened = skipping.open(height(0, 10, 0))?;
    assert_eq!((opened.previous, opened.skipped), (Some(7), 2));
    assert_eq!(skipping.meta(worker)?.complete_missed_opened_epochs, 0);
    assert_eq!(
        roster::prune_candidate(&skipping.parts()?.admission, Role::Worker, 10),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    Ok(())
}

#[test]
fn a06_selection_orders_activity_admission_then_id() -> TestResult {
    let mut world = World::create(0)?;
    let first = world.enroll(1, 0)?;
    let second = world.enroll(2, 1)?;
    let third = world.enroll(3, 2)?;
    assert_eq!(world.open(10)?.rollover.installed, 3);
    let late = world.enroll(4, 20)?;
    world.open(height(0, 1, 10))?;
    world.edit(|parts, market| {
        parts.admission.heartbeat(
            &ctx(market, principal(1)?, height(0, 1, 12)),
            Participant::Worker(first),
            1,
            1,
            1,
        )
    })?;
    world.open(height(0, 2, 10))?;
    world.open(height(0, 3, 10))?;
    let (low, high) = if second < third {
        (second, third)
    } else {
        (third, second)
    };
    assert_eq!(
        roster::prune_candidate(&world.parts()?.admission, Role::Worker, 3)?.participant,
        Participant::Worker(low)
    );
    world.open(height(0, 4, 10))?;
    let mut table = world.parts()?.admission;
    let mut order = Vec::new();
    for _ in 0..4 {
        let candidate = roster::prune_candidate(&table, Role::Worker, 4)?;
        order.push(candidate.participant);
        table.prune_inactive(candidate.participant, 1, 1)?;
    }
    assert_eq!(order, [low, high, first, late].map(Participant::Worker));
    assert_eq!(
        roster::prune_candidate(&table, Role::Worker, 4),
        Err(F08_NO_PRUNABLE_MEMBER)
    );
    world.edit(|parts, _| {
        parts
            .admission
            .prune_inactive(Participant::Worker(low), 1, 1)
    })?;
    let opened = world.open(height(0, 5, 10))?;
    assert_eq!((opened.rollover.removed, opened.workers), (1, 3));
    assert_eq!(world.parts()?.workers.get(low), None);
    Ok(())
}

#[test]
fn a07_immunity_and_security_removal() -> TestResult {
    let mut world = World::create(0)?;
    let worker = world.enroll(1, 0)?;
    let revoked = world.evaluator(2, 1)?;
    world.evaluator(3, 2)?;
    world.evaluator(4, 3)?;
    let opened = world.open(10)?;
    assert_eq!((opened.rollover.installed, opened.evaluators), (4, 3));
    assert!(opened.rollover.health.quorum_ready);
    for role in [Role::Worker, Role::Evaluator] {
        assert_eq!(
            roster::prune_candidate(&world.parts()?.admission, role, 0),
            Err(F08_NO_PRUNABLE_MEMBER)
        );
    }
    world.open(height(0, 1, 0))?;
    world.edit(|parts, market| {
        parts.admission.administrative_remove(
            &ctx(market, market.owner_principal, height(0, 1, 2)),
            revoked,
            1,
            RemovalReason::Security,
        )
    })?;
    assert_eq!(
        world.edit(|parts, market| {
            parts.admission.heartbeat(
                &ctx(market, principal(2)?, height(0, 1, 3)),
                revoked,
                1,
                1,
                1,
            )
        }),
        Err(F08_DELEGATE_REVOKED)
    );
    let health = roster::health(&world.parts()?.admission)?;
    assert_eq!(
        (
            health.roster_evaluators,
            health.eligible_evaluators,
            health.quorum_ready
        ),
        (3, 2, false)
    );
    assert_eq!(
        roster::require_quorum(&world.parts()?.admission),
        Err(F08_QUORUM_UNAVAILABLE)
    );
    let slot = world.record(worker)?.slot;
    world.edit(|parts, market| {
        parts.admission.administrative_remove(
            &ctx(market, market.owner_principal, height(0, 1, 4)),
            Participant::Worker(worker),
            1,
            RemovalReason::Security,
        )
    })?;
    let opened = world.open(height(0, 2, 0))?;
    assert_eq!(
        (opened.rollover.removed, opened.workers, opened.evaluators),
        (2, 0, 2)
    );
    let parts = world.parts()?;
    assert_eq!(parts.workers.get(worker), None);
    assert_eq!(
        parts.replay.actor(ActorSlot::worker(usize::from(slot))?),
        None
    );
    Ok(())
}

#[test]
fn a10_rotation_freezes_only_from_its_effective_epoch() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let worker = world.enroll(1, 10)?;
    let participant = Participant::Worker(worker);
    world.open(height(0, 1, 0))?;
    let installed = world.meta(participant)?;
    world.change_record(worker, |record| {
        record.generation = 2;
        record.key_version = 2;
        record.delegate = PublicKey32([0x77; 32]);
        record.effective_epoch = 3;
    })?;
    let opened = world.open(height(0, 2, 4))?;
    assert_eq!(opened.rollover.rotated, 0);
    assert_eq!(world.meta(participant)?.delegate_generation, 1);
    world.edit(|parts, market| {
        parts.admission.heartbeat(
            &ctx(market, principal(1)?, height(0, 2, 8)),
            participant,
            1,
            1,
            2,
        )
    })?;
    let opened = world.open(height(0, 3, 4))?;
    assert_eq!(opened.rollover.rotated, 1);
    let rotated = world.meta(participant)?;
    assert_eq!(rotated.delegate_generation, 2);
    assert_eq!(
        (rotated.admitted_epoch, rotated.membership_generation),
        (installed.admitted_epoch, installed.membership_generation)
    );
    assert_eq!(rotated.last_heartbeat_epoch, Some(2));
    let mut unrotated = world.parts()?.admission;
    unrotated.replace(AdmissionMeta {
        delegate_generation: 1,
        ..rotated
    })?;
    assert_ne!(
        roster::roster_digest(&unrotated, 3)?,
        opened.rollover.digest
    );
    let at = height(0, 3, 8);
    assert_eq!(
        world.edit(|parts, market| {
            parts
                .admission
                .heartbeat(&ctx(market, principal(1)?, at), participant, 1, 1, 3)
        }),
        Err(F08_WRONG_GENERATION)
    );
    world.edit(|parts, market| {
        parts
            .admission
            .heartbeat(&ctx(market, principal(1)?, at), participant, 1, 2, 3)
    })?;
    Ok(())
}

/// F06 init, owner Fund and `ReserveEpoch` for the frozen roster of `epoch`.
fn reserve(
    parts: &mut Parts,
    market: &MarketHeader,
    epoch: u64,
    roster_digest: RosterDigest,
    entry: &WorkerRosterEntry,
) -> TestResult {
    let ledger = RewardLedger::new(
        market.funding_asset,
        market.rewards_account,
        market.refund_recipient_account,
    )?;
    let mut initial = vec![0; REWARD_STATE_BYTES];
    RewardState::init(&ledger, &mut initial)?;
    let request = ReplayRequest {
        slot: ActorSlot::OWNER,
        principal: market.owner_principal,
        authority_version: version()?,
        sequence: 2,
        request_id: RequestId::new([2; 32])?,
        digest: RequestDigest::new([3; 32])?,
        expiry_height: 1000,
    };
    let mut funded = vec![0; REWARD_STATE_BYTES];
    decode_reward_state(&initial)?.fund(
        &FundingAuthority {
            owner: market.owner_principal,
            treasury: Presence::Absent,
        },
        FundingPhase::Accepting,
        &FundRequest {
            amount: 50,
            refund_recipient: market.refund_recipient_account,
            policy_version: FUNDING_POLICY_VERSION,
            consent: true,
        },
        &mut FundReplay {
            table: &mut parts.replay,
            request: &request,
            height: 130,
            revision: &mut parts.revision,
            result: ResultDigest::new([4; 32])?,
        },
        &mut funded,
    )?;
    let mut reserved = vec![0; REWARD_STATE_BYTES];
    decode_reward_state(&funded)?.reserve_epoch(
        epoch,
        19,
        roster_digest,
        core::slice::from_ref(entry),
        height(0, epoch, 0),
        &mut reserved,
    )?;
    parts.rewards = reserved;
    Ok(())
}
/// F05 terminal result for the single frozen worker, then `TerminalizeRewards`.
fn terminalize(
    parts: &mut Parts,
    market: &MarketHeader,
    epoch: u64,
    roster_digest: RosterDigest,
    entry: &WorkerRosterEntry,
    at: u64,
) -> TestResult {
    let binding = FrozenBinding {
        chain: market.deployment_chain_domain,
        program: market.program_id,
        market: market.market_id,
        epoch,
        config: version()?,
        roster: roster_digest,
    };
    let roster = [*entry];
    let outputs = [WorkerAggregate::new(
        entry.worker,
        version()?,
        3,
        QualityStatus::ScoredPositive,
        5,
        5,
    )?];
    let allocation = allocate(19, &outputs)?;
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
}

#[test]
fn a12_exit_waits_for_retention_and_preserves_entitlements() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let worker = world.enroll(1, 10)?;
    let opened = world.open(height(0, 1, 0))?;
    let record = world.record(worker)?;
    let recipient = AccountId::new([0x61; 32])?;
    let entry = WorkerRosterEntry {
        worker,
        owner: record.owner,
        recipient,
        generation: Version::new(record.generation)?,
        key_version: Version::new(record.key_version)?,
        public_key: record.delegate,
        metadata: record.metadata,
    };
    let roster_digest = RosterDigest::new(opened.rollover.digest.bytes())?;
    world.edit(|parts, market| reserve(parts, market, 1, roster_digest, &entry))?;
    assert_eq!(world.open(height(0, 2, 0)), Err(WRONG_PHASE));
    world.edit(|parts, market| {
        terminalize(parts, market, 1, roster_digest, &entry, height(0, 2, 1))
    })?;
    world.edit(|parts, market| {
        parts.admission.request_exit(
            &ctx(market, principal(1)?, height(0, 2, 2)),
            Participant::Worker(worker),
            1,
            2,
            ExitReason::Voluntary,
        )
    })?;
    let slot = ActorSlot::worker(usize::from(record.slot))?;
    world.edit(|parts, _| {
        let request = ReplayRequest {
            slot,
            principal: principal(1)?,
            authority_version: version()?,
            sequence: 1,
            request_id: RequestId::new([6; 32])?,
            digest: RequestDigest::new([7; 32])?,
            expiry_height: 300,
        };
        let result = ResultDigest::new([8; 32])?;
        parts
            .replay
            .record_success(&request, height(0, 2, 3), &mut parts.revision, result)
            .map(|_| ())
    })?;
    let rewards = world.section(Section::SettlementClaims)?;
    assert_eq!(world.open(height(0, 2, 4)), Err(F08_RETENTION_BLOCKED));
    let opened = world.open(300)?;
    assert_eq!((opened.rollover.removed, opened.workers), (1, 0));
    assert_eq!(
        (opened.preserved_entitlements, opened.preserved_amount),
        (1, 19)
    );
    assert_eq!(world.section(Section::SettlementClaims)?, rewards);
    let parts = world.parts()?;
    assert_eq!(parts.workers.get(worker), None);
    assert_eq!(parts.replay.actor(slot), None);
    let state = decode_reward_state(&parts.rewards)?;
    assert_eq!(
        state
            .row(1)?
            .check_claim(&state.dictionary(), worker, recipient, 19, 301)?,
        ClaimDecision::Payable {
            index: 0,
            amount: 19
        }
    );
    let successor = world.enroll(2, 301)?;
    assert_eq!(world.record(successor)?.slot, record.slot);
    let fresh = world.meta(Participant::Worker(successor))?;
    assert_eq!(
        (
            fresh.admitted_epoch,
            fresh.membership_generation,
            fresh.complete_missed_opened_epochs,
            fresh.last_heartbeat_epoch
        ),
        (None, 1, 0, None)
    );
    let parts = world.parts()?;
    let state = decode_reward_state(&parts.rewards)?;
    assert_eq!(
        state
            .row(1)?
            .check_claim(&state.dictionary(), successor, recipient, 19, 302),
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
    world.edit(|parts, market| {
        let ledger = RewardLedger::new(
            AssetId::new([0x99; 32])?,
            market.rewards_account,
            market.refund_recipient_account,
        )?;
        RewardState::init(&ledger, &mut parts.rewards)?;
        Ok(())
    })?;
    assert_eq!(world.open(height(0, 3, 0)), Err(ACCOUNT_BINDING));
    Ok(())
}

#[test]
fn a13_clock_phase_revision_and_lifecycle_gates() -> TestResult {
    let mut world = World::create(0)?;
    assert_eq!(world.open(63)?.rollover.epoch, 0);
    for at in [64, 79, 80, 95, 96, 127] {
        assert_eq!(world.open(at), Err(WRONG_PHASE));
    }
    let worker = world.enroll(1, 127)?;
    assert_eq!(
        world
            .meta(Participant::Worker(worker))?
            .immunity_until_epoch,
        1
    );
    let revision = world.revision()?;
    assert_eq!(
        world
            .rollover(128, revision - 1, MAX_STATE_BYTES, ROLLOVER_SCRATCH_BYTES)
            .map(|(opened, _)| opened),
        Err(F08_STALE_STATE)
    );
    let opened = world.open(128)?;
    assert_eq!((opened.rollover.epoch, opened.rollover.installed), (1, 1));
    assert_eq!(world.revision()?, revision);
    assert_eq!(world.open(128), Err(WRONG_EPOCH));
    for (lifecycle, refusal) in [(SUSPENDED, F08_MARKET_PAUSED), (CLOSED, WRONG_PHASE)] {
        let mut closed = World(world.0.clone());
        closed.edit(|parts, _| parts.set_lifecycle(lifecycle))?;
        assert_eq!(closed.open(height(0, 2, 0)), Err(refusal));
    }
    world.edit(|parts, _| parts.set_lifecycle(ACTIVE))?;
    assert_eq!(world.open(height(0, 2, 0))?.rollover.epoch, 2);
    let mut late = World::create(1000)?;
    assert_eq!(late.open(999), Err(WRONG_EPOCH));
    assert_eq!(late.open(1000)?.rollover.epoch, 0);
    Ok(())
}

/// Every replay slot bound with a retained result; worker slots never expire here.
fn fill_replay(parts: &mut Parts) -> TestResult {
    let mut slots = vec![ActorSlot::TREASURY, ActorSlot::OPERATOR];
    for index in 0..8 {
        slots.push(ActorSlot::evaluator(index)?);
    }
    for (n, slot) in (200u8..).zip(&slots) {
        parts.replay.bind(*slot, principal(n)?, version()?)?;
    }
    slots.push(ActorSlot::OWNER);
    for index in 0..32 {
        slots.push(ActorSlot::worker(index)?);
    }
    for (n, slot) in (1u8..).zip(&slots) {
        let actor = parts.replay.actor(*slot).ok_or(NOT_FOUND)?;
        let sequence = actor.last.map_or(0, |last| last.sequence) + 1;
        let request = ReplayRequest {
            slot: *slot,
            principal: actor.principal,
            authority_version: actor.authority_version,
            sequence,
            request_id: RequestId::new([n; 32])?,
            digest: RequestDigest::new([n; 32])?,
            expiry_height: 100_000,
        };
        parts.replay.record_success(
            &request,
            height(0, 10, 2),
            &mut parts.revision,
            ResultDigest::new([n; 32])?,
        )?;
    }
    Ok(())
}

#[test]
fn a15_maximal_state_fits_and_short_buffers_refuse() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    for epoch in 0..10u8 {
        for k in 0..4u8 {
            let n = epoch * 4 + k;
            let at = height(0, u64::from(epoch), 1 + u64::from(k));
            if n < 32 {
                world.enroll(n + 1, at)?;
            } else {
                world.evaluator(n + 68, at)?;
            }
        }
        world.open(height(0, u64::from(epoch) + 1, 0))?;
    }
    world.edit(|parts, market| {
        fill_replay(parts)?;
        let ledger = RewardLedger::new(
            market.funding_asset,
            market.rewards_account,
            market.refund_recipient_account,
        )?;
        let mut rewards = vec![0xC3; Section::SettlementClaims.payload_cap()];
        RewardState::init(&ledger, &mut rewards[..REWARD_STATE_BYTES])?;
        parts.rewards = rewards;
        parts.reports = vec![0xA5; Section::CurrentReports.payload_cap()];
        Ok(())
    })?;
    let reports = world.section(Section::CurrentReports)?;
    let rewards = world.section(Section::SettlementClaims)?;
    let revision = world.revision()?;
    let at = height(0, 11, 0);
    let (opened, _) = world.rollover(at, revision, MAX_STATE_BYTES, ROLLOVER_SCRATCH_BYTES)?;
    assert!(opened.state_len <= MAX_STATE_BYTES);
    let short = [
        (opened.state_len - 1, ROLLOVER_SCRATCH_BYTES),
        (MAX_STATE_BYTES, TABLE_MAX_BYTES - 1),
        (
            MAX_STATE_BYTES,
            TABLE_MAX_BYTES + Section::IdentityRoster.payload_cap(),
        ),
    ];
    for (next, scratch) in short {
        assert_eq!(
            world
                .rollover(at, revision, next, scratch)
                .map(|(opened, _)| opened),
            Err(CAPACITY)
        );
    }
    let opened = world.open(at)?;
    assert_eq!(
        (opened.rollover.members, opened.workers, opened.evaluators),
        (40, 32, 8)
    );
    assert_eq!(world.0.len(), opened.state_len);
    assert_eq!(world.section(Section::CurrentReports)?, reports);
    assert_eq!(world.section(Section::SettlementClaims)?, rewards);
    assert_eq!(world.parts()?.workers.len(), 32);
    Ok(())
}

#[test]
fn a17_bootstrap_freezes_epoch_zero_with_quorum() -> TestResult {
    let mut world = World::create(1000)?;
    let worker = world.enroll(1, 1000)?;
    for (n, at) in [(2, 1001), (3, 1002), (4, 1003)] {
        world.evaluator(n, at)?;
    }
    let opened = world.open(1008)?;
    assert_eq!(
        (opened.rollover.epoch, opened.previous, opened.skipped),
        (0, None, 0)
    );
    assert_eq!(
        (opened.rollover.installed, opened.workers, opened.evaluators),
        (4, 1, 3)
    );
    assert!(opened.rollover.health.quorum_ready);
    let meta = world.meta(Participant::Worker(worker))?;
    assert_eq!(
        (
            meta.admitted_epoch,
            meta.last_activity(),
            meta.last_heartbeat_height,
            meta.immunity_until_epoch
        ),
        (Some(0), Some(0), None, 0)
    );
    roster::require_quorum(&world.parts()?.admission)?;

    let mut thin = World::create(1000)?;
    thin.enroll(1, 1000)?;
    thin.evaluator(2, 1001)?;
    thin.evaluator(3, 1002)?;
    let opened = thin.open(1008)?;
    assert_eq!(opened.evaluators, 2);
    assert!(!opened.rollover.health.quorum_ready);
    assert_eq!(
        roster::require_quorum(&thin.parts()?.admission),
        Err(F08_QUORUM_UNAVAILABLE)
    );
    Ok(())
}

#[test]
fn a18_skipped_epoch_installs_at_next_opening_and_drops_stale_approvals() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let worker = Participant::Worker(world.enroll(1, 10)?);
    let unaccepted = world.edit(|parts, market| {
        let owner = principal(2)?;
        let participant = Participant::Worker(derive_worker(market.market_id, owner, [2; 32])?);
        approve(
            &mut parts.admission,
            market,
            participant,
            owner,
            PublicKey32([8; 32]),
            11,
        )?;
        Ok(participant)
    })?;
    let opened = world.open(height(0, 2, 0))?;
    assert_eq!((opened.previous, opened.skipped), (Some(0), 1));
    assert_eq!((opened.rollover.installed, opened.rollover.expired), (1, 1));
    let meta = world.meta(worker)?;
    assert_eq!(
        (meta.admitted_epoch, meta.immunity_until_epoch),
        (Some(2), 2)
    );
    assert_eq!(world.parts()?.admission.get(unaccepted), None);
    Ok(())
}

#[test]
fn a19_only_accepted_evaluators_enter_the_roster() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let market = world.parts()?.market()?;
    let key = SigningKey::from_bytes(&[3; 32]);
    let owner = principal(9)?;
    let signing_key = PublicKey32(key.verifying_key().to_bytes());
    let grant = EvaluatorGrant::nominate(
        market.market_id,
        owner,
        [5; 32],
        GrantTerms {
            rubric: RubricDigest::new([6; 32])?,
            grant_version: version()?,
            key_version: version()?,
            signing_key,
            effective_epoch: 1,
            expiry_epoch_exclusive: 32,
        },
    )?;
    let accepted = Participant::Evaluator(grant.evaluator);
    let approval = world.edit(|parts, market| {
        approve(
            &mut parts.admission,
            market,
            accepted,
            owner,
            signing_key,
            5,
        )
    })?;
    let idle = world.edit(|parts, market| {
        let owner = principal(10)?;
        let participant =
            Participant::Evaluator(derive_evaluator(market.market_id, owner, [10; 32])?);
        approve(
            &mut parts.admission,
            market,
            participant,
            owner,
            PublicKey32([8; 32]),
            6,
        )?;
        Ok(participant)
    })?;
    let consent = EvaluatorConsent {
        chain: market.deployment_chain_domain,
        program: market.program_id,
        market: market.market_id,
        evaluator: grant.evaluator,
        owner,
        signing_key,
        enrollment_nonce: [5; 32],
        rubric: grant.rubric,
        approval_digest: approval.1,
        request: RequestId::new([2; 32])?,
        grant_version: 1,
        key_version: 1,
        effective_epoch: 1,
        config_version: 1,
        expiry_height: 60,
    };
    let mut signed = [0u8; 362];
    consent.encode(&mut signed)?;
    let mut payload = signed.to_vec();
    payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
    world.edit(|parts, market| {
        admit_evaluator(
            &mut parts.admission,
            &ctx(market, owner, 7),
            &grant,
            consent.request,
            &payload,
        )
    })?;
    assert_eq!(
        roster::health(&world.parts()?.admission)?.roster_evaluators,
        0
    );
    let opened = world.open(height(0, 1, 0))?;
    assert_eq!(
        (
            opened.rollover.installed,
            opened.evaluators,
            opened.rollover.expired
        ),
        (1, 1, 0)
    );
    assert_eq!(world.meta(accepted)?.admitted_epoch, Some(1));
    assert_eq!(world.meta(idle)?.admitted_epoch, None);
    let opened = world.open(height(0, 2, 0))?;
    assert_eq!((opened.evaluators, opened.rollover.expired), (1, 1));
    assert_eq!(world.parts()?.admission.get(idle), None);
    Ok(())
}

#[test]
fn identity_record_gates_installation() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let worker = world.enroll(1, 10)?;
    let participant = Participant::Worker(worker);
    world.change_record(worker, |record| record.state = WorkerState::PendingOwner)?;
    assert_eq!(world.open(height(0, 1, 0))?.rollover.installed, 0);
    assert_eq!(world.meta(participant)?.admitted_epoch, None);
    world.change_record(worker, |record| {
        record.state = WorkerState::Enrolled;
        record.expiry = 200;
    })?;
    assert_eq!(world.open(height(0, 2, 0))?.rollover.installed, 0);
    world.change_record(worker, |record| record.expiry = 10_000)?;
    let opened = world.open(height(0, 3, 0))?;
    assert_eq!((opened.rollover.installed, opened.workers), (1, 1));
    assert_eq!(world.meta(participant)?.admitted_epoch, Some(3));
    let stranger = principal(0x51)?;
    let mut foreign = World(world.0.clone());
    foreign.change_record(worker, |record| record.owner = stranger)?;
    assert_eq!(foreign.open(height(0, 4, 0)), Err(NON_CANONICAL));
    world.edit(|parts, _| parts.workers.remove(worker).map(|_| ()))?;
    assert_eq!(world.open(height(0, 4, 0)), Err(NON_CANONICAL));
    Ok(())
}

/// One encoded F03 evaluator region holding a single nominated grant.
fn evaluator_region(market: &MarketHeader) -> CodecResult<Vec<u8>> {
    let grant = EvaluatorGrant::nominate(
        market.market_id,
        principal(30)?,
        [30; 32],
        GrantTerms {
            rubric: RubricDigest::new([6; 32])?,
            grant_version: version()?,
            key_version: version()?,
            signing_key: PublicKey32([0x31; 32]),
            effective_epoch: 1,
            expiry_epoch_exclusive: 32,
        },
    )?;
    let mut region = EvaluatorRegion::new();
    region.insert(&EvaluatorRecord {
        grant,
        last: LastRequest {
            sequence: 1,
            request: RequestId::new([0x32; 32])?,
            digest: RequestDigest::new([0x33; 32])?,
            result: ResultDigest::new([0x34; 32])?,
        },
        rekey: None,
        revocation: None,
    })?;
    let mut out = vec![0; region.encoded_len()];
    let n = region.encode(&mut out)?;
    out.truncate(n);
    Ok(out)
}

#[test]
fn identity_section_keeps_the_evaluator_region() -> TestResult {
    let mut world = World::create(0)?;
    world.open(0)?;
    let staying = world.enroll(1, 10)?;
    let leaving = world.enroll(2, 30)?;
    let region = world.edit(|parts, market| {
        parts.region = evaluator_region(market)?;
        Ok(parts.region.clone())
    })?;
    assert!(!region.is_empty());
    let identity = world.section(Section::IdentityRoster)?;
    let opened = world.open(height(0, 1, 0))?;
    assert_eq!(opened.rollover.installed, 2);
    assert_eq!(world.section(Section::IdentityRoster)?, identity);
    world.edit(|parts, market| {
        parts.admission.request_exit(
            &ctx(market, principal(2)?, height(0, 1, 2)),
            Participant::Worker(leaving),
            1,
            2,
            ExitReason::Voluntary,
        )
    })?;
    let opened = world.open(height(0, 2, 0))?;
    assert_eq!((opened.rollover.removed, opened.workers), (1, 1));
    let identity = world.section(Section::IdentityRoster)?;
    let (workers, kept) = split_identity_section(&identity)?;
    assert_eq!(kept, region.as_slice());
    let table = WorkerTable::decode(workers)?;
    assert_eq!(table.len(), 1);
    assert!(table.get(staying).is_some());
    assert_eq!(table.get(leaving), None);
    Ok(())
}
