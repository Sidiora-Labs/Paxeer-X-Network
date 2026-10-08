//! The shared identity section: the F02 worker table prefix followed by the F03
//! evaluator region. F02 transitions and F10 selectors read the prefix through the
//! common split and every F02 rewrite carries the region byte-for-byte.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    codec::{decode_envelope, derive_market, derive_worker, encode_envelope, Envelope},
    dispatch::{self, Operation},
    errors::{CodecResult, ARITHMETIC, CAPACITY, NON_CANONICAL, NOT_FOUND, ROLE_CONFLICT},
    evaluators::authority::{
        self, AuthorityContext, EvaluatorRegion, Outcome, REGION_MAX_BYTES, SCRATCH_BYTES,
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        feature_availability, participant_rows, Availability, ParticipantKind, ParticipantRow,
        RewardField, ScoreField, ScoreStatus, ELIGIBLE_SERVING,
    },
    registry::{derive_rewards_account, market_clock, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, PolicySection},
    state::{self, Section, SharedState},
    types::{
        AssetId, Authentication, ChainDomain, Digest32, MetadataDigest, Presence, PrincipalId,
        ProgramId, PublicKey32, RequestId, RubricDigest, Signature64, WorkerId,
    },
    workers::{
        apply, consent_digest, decode_manifest, Applied, CallContext, WorkerCurrent, WorkerState,
        WorkerTable, CONTROL_SCRATCH_BYTES, WORKER_RECORD_BYTES, WORKER_TABLE_MAX_BYTES,
    },
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const RUBRIC: [u8; 32] = [4; 32];
const NOMINEE: [u8; 32] = [20; 32];
const NONCE: [u8; 32] = [21; 32];
const SECOND_NOMINEE: [u8; 32] = [22; 32];
const SECOND_NONCE: [u8; 32] = [23; 32];
const EVALUATOR: u8 = 40;
const METADATA: [u8; 32] = [31; 32];
const ROTATED_METADATA: [u8; 32] = [33; 32];
/// Market creation height and therefore the market clock origin.
const ORIGIN: u64 = 1_000;

const _: () =
    assert!(WORKER_TABLE_MAX_BYTES + REGION_MAX_BYTES < Section::IdentityRoster.payload_cap());

fn p(bytes: [u8; 32]) -> CodecResult<PrincipalId> {
    PrincipalId::new(bytes)
}
fn key(seed: u8) -> SigningKey {
    SigningKey::from_bytes(&[seed; 32])
}
fn pk(k: &SigningKey) -> PublicKey32 {
    PublicKey32(k.verifying_key().to_bytes())
}

fn encode_shared(shared: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; shared.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = state::encode_shared_state(shared, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// Real F01 CREATE at `ORIGIN`; returns the created header and state.
fn created() -> CodecResult<(MarketHeader, Vec<u8>)> {
    let policy = TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: Digest32::new([1; 32])?,
            dataset_artifact: [2; 32],
            benchmark_suite: Digest32::new([3; 32])?,
            rubric: RubricDigest::new(RUBRIC)?,
            task_schema: Digest32::new([5; 32])?,
            result_schema: Digest32::new([6; 32])?,
            service_terms: Digest32::new([7; 32])?,
        },
        100,
        1,
    )?;
    let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
    policy.encode(&mut encoded_policy)?;
    let chain = ChainDomain::new(CHAIN)?;
    let program = ProgramId::new(PROGRAM)?;
    let rewards = derive_rewards_account(program, AssetId::new(ASSET)?)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&encoded_policy);
    payload.extend_from_slice(&[16; 32]);
    let envelope = Envelope {
        operation: dispatch::CREATE,
        chain,
        program,
        market: derive_market(chain, program)?,
        actor: p(OWNER)?,
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
    let validated = decode_envelope(&encoded[..n])?;
    let ctx = registry_ops::CallContext {
        chain,
        program,
        principal: p(OWNER)?,
        height: ORIGIN,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let registry_ops::Outcome::Applied { state, .. } =
        registry_ops::apply(&ctx, None, &validated, &mut section, &mut event)?
    else {
        panic!("a fresh create is never a retry");
    };
    let header = PolicySection::decode(state.section(Section::PolicyLifecycle)?)?.header;
    assert_eq!(header.origin_height, ORIGIN);
    Ok((header, encode_shared(&state)?))
}

/// `base` with its identity section replaced by `identity`.
fn with_identity(base: &[u8], identity: &[u8]) -> CodecResult<Vec<u8>> {
    let shared = state::decode_shared_state(base)?;
    encode_shared(&shared.replace_section(Section::IdentityRoster, identity)?)
}

fn identity(state_bytes: &[u8]) -> CodecResult<Vec<u8>> {
    Ok(state::decode_shared_state(state_bytes)?
        .section(Section::IdentityRoster)?
        .to_vec())
}
fn workers_of(state_bytes: &[u8]) -> CodecResult<WorkerTable> {
    let section = identity(state_bytes)?;
    WorkerTable::decode(authority::split_identity_section(&section)?.0)
}
fn region_of(state_bytes: &[u8]) -> CodecResult<Vec<u8>> {
    let section = identity(state_bytes)?;
    Ok(authority::split_identity_section(&section)?.1.to_vec())
}
fn envelope(
    h: &MarketHeader,
    op: Operation,
    actor: [u8; 32],
    sequence: u64,
    height: u64,
    payload: &[u8],
    delegate: Option<&SigningKey>,
) -> CodecResult<Vec<u8>> {
    let mut request = actor;
    request[..2].copy_from_slice(&op.selector().to_be_bytes());
    request[2..10].copy_from_slice(&sequence.to_be_bytes());
    let mut env = Envelope {
        operation: op,
        chain: h.deployment_chain_domain,
        program: h.program_id,
        market: h.market_id,
        actor: p(actor)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence,
        expiry: height + 1_000,
        request: RequestId::new(request)?,
        payload,
        authentication: Authentication::Native,
    };
    let mut out = vec![0; 16_384];
    if let Some(k) = delegate {
        env.authentication = Authentication::Delegate {
            key: pk(k),
            signature: Signature64([0; 64]),
        };
        let n = encode_envelope(&env, &mut out)?;
        let digest = decode_envelope(&out[..n])?.request_digest()?;
        env.authentication = Authentication::Delegate {
            key: pk(k),
            signature: Signature64(k.sign(digest.as_bytes()).to_bytes()),
        };
    }
    let n = encode_envelope(&env, &mut out)?;
    out.truncate(n);
    Ok(out)
}

/// One F02 call through `workers::apply`; returns the next state of an applied call.
fn f02(
    state_bytes: &[u8],
    h: &MarketHeader,
    invoker: [u8; 32],
    height: u64,
    env: &[u8],
) -> CodecResult<Vec<u8>> {
    let ctx = CallContext {
        market: h,
        invoking_principal: p(invoker)?,
        height,
    };
    let mut out = vec![0; MAX_STATE_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let mut scratch = vec![0; CONTROL_SCRATCH_BYTES];
    let applied = apply(state_bytes, &ctx, env, &mut out, &mut event, &mut scratch)?;
    let Applied::Applied { state_len, .. } = applied else {
        panic!("expected a fresh F02 mutation, got {applied:?}");
    };
    out.truncate(state_len);
    Ok(out)
}

/// One Owner F03 call through `evaluators::authority::apply`.
fn f03(state_bytes: &[u8], h: &MarketHeader, height: u64, env: &[u8]) -> CodecResult<Vec<u8>> {
    let ctx = AuthorityContext {
        market: h,
        invoking_principal: p(OWNER)?,
        immediate_caller: Presence::Absent,
        height,
        approved_rubric: RubricDigest::new(RUBRIC)?,
        aggregate_sealed: false,
    };
    let mut scratch = vec![0; SCRATCH_BYTES];
    let mut out = vec![0; MAX_STATE_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let outcome = authority::apply(state_bytes, &ctx, env, &mut scratch, &mut out, &mut event)?;
    let Outcome::Applied { state_len, .. } = outcome else {
        panic!("expected a fresh F03 mutation, got {outcome:?}");
    };
    out.truncate(state_len);
    Ok(out)
}

/// `ScheduleEvaluator` terms for principal `who`, activating at epoch 0.
fn nomination(who: u8) -> Vec<u8> {
    let mut out = vec![who; 32];
    out.extend_from_slice(&[who + 1; 32]);
    out.extend_from_slice(&RUBRIC);
    out.extend_from_slice(&pk(&key(who + 2)).0);
    for value in [1u64, 1, 0, 32] {
        out.extend_from_slice(&value.to_be_bytes());
    }
    out
}
fn nominate(principal: [u8; 32], nonce: [u8; 32], delegate: &SigningKey, expiry: u64) -> Vec<u8> {
    let mut v = principal.to_vec();
    v.extend_from_slice(&nonce);
    v.extend_from_slice(&pk(delegate).0);
    v.extend_from_slice(&METADATA);
    v.extend_from_slice(&expiry.to_be_bytes());
    v
}
fn accept(h: &MarketHeader, record: &WorkerCurrent, delegate: &SigningKey) -> CodecResult<Vec<u8>> {
    let consent = consent_digest(
        h,
        record.worker,
        record.owner,
        record.delegate,
        1,
        1,
        record.metadata,
        record.expiry,
    )?;
    let mut v = record.worker.as_bytes().to_vec();
    v.extend_from_slice(&1u64.to_be_bytes());
    v.extend_from_slice(&1u64.to_be_bytes());
    v.extend_from_slice(record.metadata.as_bytes());
    v.extend_from_slice(&delegate.sign(consent.as_bytes()).to_bytes());
    Ok(v)
}
/// Grammar-valid worker manifest for `record` at `revision`.
fn manifest(
    h: &MarketHeader,
    record: &WorkerCurrent,
    revision: u64,
    valid_from: u64,
    expiry: u64,
) -> CodecResult<Vec<u8>> {
    const URI: &[u8] = b"https://worker.example/paxai/v1";
    let mut v = 1u16.to_be_bytes().to_vec();
    v.extend_from_slice(h.market_id.as_bytes());
    v.extend_from_slice(record.worker.as_bytes());
    v.extend_from_slice(record.owner.as_bytes());
    for n in [
        record.generation,
        record.key_version,
        revision,
        valid_from,
        expiry,
    ] {
        v.extend_from_slice(&n.to_be_bytes());
    }
    v.extend_from_slice(&[60; 32]);
    v.extend_from_slice(&1u16.to_be_bytes());
    v.extend_from_slice(&[1, 1]);
    v.extend_from_slice(&[50; 32]);
    for d in 0..4u8 {
        v.extend_from_slice(&[61 + d; 32]);
    }
    for n in [1024u32; 4] {
        v.extend_from_slice(&n.to_be_bytes());
    }
    v.push(1);
    v.extend_from_slice(&60_000u32.to_be_bytes());
    v.extend_from_slice(&4u16.to_be_bytes());
    v.push(1);
    v.extend_from_slice(&1u16.to_be_bytes());
    v.push(1);
    v.extend_from_slice(
        &u32::try_from(URI.len())
            .map_err(|_| ARITHMETIC)?
            .to_be_bytes(),
    );
    v.extend_from_slice(URI);
    v.extend_from_slice(&[70; 32]);
    v.push(1);
    v.extend_from_slice(&1u16.to_be_bytes());
    v.extend_from_slice(&1u16.to_be_bytes());
    v.extend_from_slice(&[71; 32]);
    v.extend_from_slice(&[72; 32]);
    v.extend_from_slice(&0u32.to_be_bytes());
    Ok(v)
}
fn publish(worker: WorkerId, expected: u64, manifest: &[u8]) -> CodecResult<Vec<u8>> {
    let m = decode_manifest(manifest)?;
    let mut v = worker.as_bytes().to_vec();
    v.extend_from_slice(&expected.to_be_bytes());
    v.extend_from_slice(&m.revision.to_be_bytes());
    v.extend_from_slice(m.digest.as_bytes());
    v.extend_from_slice(&m.valid_from.to_be_bytes());
    v.extend_from_slice(&m.expiry.to_be_bytes());
    v.extend_from_slice(
        &u32::try_from(manifest.len())
            .map_err(|_| ARITHMETIC)?
            .to_be_bytes(),
    );
    v.extend_from_slice(manifest);
    Ok(v)
}
fn rotate(h: &MarketHeader, record: &WorkerCurrent, next: &SigningKey) -> CodecResult<Vec<u8>> {
    let metadata = MetadataDigest::new(ROTATED_METADATA)?;
    let expiry = ORIGIN + 400;
    let consent = consent_digest(
        h,
        record.worker,
        record.owner,
        pk(next),
        record.generation + 1,
        record.key_version + 1,
        metadata,
        expiry,
    )?;
    let mut v = record.worker.as_bytes().to_vec();
    v.extend_from_slice(&record.generation.to_be_bytes());
    v.extend_from_slice(&record.key_version.to_be_bytes());
    v.extend_from_slice(&pk(next).0);
    v.extend_from_slice(metadata.as_bytes());
    v.extend_from_slice(&expiry.to_be_bytes());
    v.extend_from_slice(&next.sign(consent.as_bytes()).to_bytes());
    Ok(v)
}
fn revoke(worker: WorkerId, generation: u64) -> Vec<u8> {
    let mut v = worker.as_bytes().to_vec();
    v.extend_from_slice(&generation.to_be_bytes());
    v.push(1);
    v.extend_from_slice(&1u64.to_be_bytes());
    v
}

/// Created market whose identity section carries one real F03 nomination.
fn market_with_region() -> CodecResult<(MarketHeader, Vec<u8>)> {
    let (h, s0) = created()?;
    let env = envelope(
        &h,
        dispatch::ScheduleEvaluator,
        OWNER,
        2,
        ORIGIN + 10,
        &nomination(EVALUATOR),
        None,
    )?;
    let s1 = f03(&s0, &h, ORIGIN + 10, &env)?;
    let section = identity(&s1)?;
    let (workers, region) = authority::split_identity_section(&section)?;
    assert_eq!(workers, &[0]);
    assert_eq!(EvaluatorRegion::decode(region)?.len(), 1);
    Ok((h, s1))
}

/// A market walked through F02 transitions; every step must keep the region.
struct Walk {
    h: MarketHeader,
    state: Vec<u8>,
    region: Vec<u8>,
    evaluators: EvaluatorRegion,
}
impl Walk {
    fn start() -> CodecResult<Self> {
        let (h, state) = market_with_region()?;
        let region = region_of(&state)?;
        assert!(!region.is_empty());
        let evaluators = authority::evaluator_region(&identity(&state)?)?;
        Ok(Self {
            h,
            state,
            region,
            evaluators,
        })
    }
    /// One applied F02 call by `actor`; checks the region bytes and the decoded
    /// region are unchanged and returns the next worker table.
    fn step(
        &mut self,
        op: Operation,
        actor: [u8; 32],
        sequence: u64,
        height: u64,
        payload: &[u8],
        delegate: Option<&SigningKey>,
    ) -> CodecResult<WorkerTable> {
        let env = envelope(&self.h, op, actor, sequence, height, payload, delegate)?;
        self.state = f02(&self.state, &self.h, actor, height, &env)?;
        assert_eq!(region_of(&self.state)?, self.region);
        assert_eq!(
            authority::evaluator_region(&identity(&self.state)?)?,
            self.evaluators
        );
        workers_of(&self.state)
    }
    fn record(&self, worker: WorkerId) -> CodecResult<WorkerCurrent> {
        workers_of(&self.state)?.get(worker).ok_or(NOT_FOUND)
    }
    /// F02-A01: Owner nomination and worker-owner acceptance at height 130 of epoch 1.
    fn enrolled(delegate: &SigningKey) -> CodecResult<(Self, WorkerId)> {
        let mut walk = Self::start()?;
        let h = walk.h;
        let height = ORIGIN + 130;
        assert_eq!(market_clock(h.origin_height, height)?.epoch, 1);
        let payload = nominate(NOMINEE, NONCE, delegate, ORIGIN + 258);
        let table = walk.step(dispatch::EnrollWorker, OWNER, 3, height, &payload, None)?;
        assert_eq!(table.len(), 1);
        let worker = derive_worker(h.market_id, p(NOMINEE)?, NONCE)?;
        let pending = walk.record(worker)?;
        assert_eq!(pending.state, WorkerState::PendingOwner);
        assert_eq!((pending.slot, pending.effective_epoch), (0, 2));
        let payload = accept(&h, &pending, delegate)?;
        walk.step(
            dispatch::AcceptEnrollment,
            NOMINEE,
            1,
            height,
            &payload,
            None,
        )?;
        let enrolled = walk.record(worker)?;
        assert_eq!(enrolled.state, WorkerState::Enrolled);
        assert_eq!((enrolled.generation, enrolled.effective_epoch), (1, 2));
        Ok((walk, worker))
    }
}

#[test]
fn enrollment_metadata_and_drain_carry_the_region_byte_for_byte() -> TestResult {
    let k1 = key(0x41);
    let (mut walk, worker) = Walk::enrolled(&k1)?;
    let h = walk.h;
    let height = ORIGIN + 140;
    let m = manifest(&h, &walk.record(worker)?, 2, height, ORIGIN + 300)?;
    let payload = publish(worker, 1, &m)?;
    walk.step(
        dispatch::PublishMetadata,
        NOMINEE,
        2,
        height,
        &payload,
        Some(&k1),
    )?;
    let published = walk.record(worker)?;
    assert_eq!(published.metadata_revision, 2);
    assert_eq!(published.metadata, decode_manifest(&m)?.digest);
    assert_eq!(
        (published.valid_from, published.expiry),
        (height, ORIGIN + 300)
    );

    let height = ORIGIN + 150;
    let id = *worker.as_bytes();
    walk.step(dispatch::SetDraining, NOMINEE, 3, height, &id, None)?;
    assert_eq!(walk.record(worker)?.state, WorkerState::Draining);
    walk.step(dispatch::UndoDrain, NOMINEE, 4, height, &id, None)?;
    let available = walk.record(worker)?;
    assert_eq!(available.state, WorkerState::Available);
    assert_eq!(available.effective_epoch, 2);
    Ok(())
}

#[test]
fn rotation_revocation_retirement_and_expiry_carry_the_region() -> TestResult {
    let (mut walk, worker) = Walk::enrolled(&key(0x41))?;
    let h = walk.h;
    let height = ORIGIN + 150;
    let k2 = key(0x42);
    let payload = rotate(&h, &walk.record(worker)?, &k2)?;
    walk.step(dispatch::RotateDelegate, NOMINEE, 2, height, &payload, None)?;
    let rotated = walk.record(worker)?;
    assert_eq!((rotated.generation, rotated.key_version), (2, 2));
    assert_eq!(rotated.delegate, pk(&k2));
    assert_eq!(rotated.metadata, MetadataDigest::new(ROTATED_METADATA)?);

    let payload = revoke(worker, 2);
    walk.step(dispatch::RevokeDelegate, NOMINEE, 3, height, &payload, None)?;
    let revoked = walk.record(worker)?;
    assert_eq!(
        (revoked.state, revoked.revocation_sequence),
        (WorkerState::Revoked, 1)
    );
    let id = *worker.as_bytes();
    walk.step(dispatch::RetireWorker, NOMINEE, 4, height, &id, None)?;
    let retired = walk.record(worker)?;
    assert_eq!(retired.state, WorkerState::Retired);

    // A second nomination left unaccepted is cleaned up at its expiry.
    let payload = nominate(SECOND_NOMINEE, SECOND_NONCE, &key(0x43), ORIGIN + 628);
    let table = walk.step(
        dispatch::EnrollWorker,
        OWNER,
        4,
        ORIGIN + 500,
        &payload,
        None,
    )?;
    assert_eq!(table.len(), 2);
    let second = derive_worker(h.market_id, p(SECOND_NOMINEE)?, SECOND_NONCE)?;
    let candidate = walk.record(second)?;
    assert_eq!(candidate.slot, 1);
    let mut payload = second.as_bytes().to_vec();
    payload.extend_from_slice(candidate.proposal_digest(&h)?.as_bytes());
    payload.extend_from_slice(&candidate.expiry.to_be_bytes());
    let height = ORIGIN + 628;
    let table = walk.step(dispatch::ExpireEnrollment, OWNER, 5, height, &payload, None)?;
    assert_eq!(table.len(), 1);
    assert_eq!(table.get(second), None);
    assert_eq!(table.get(worker), Some(retired));
    Ok(())
}

#[test]
fn f03_reads_and_keeps_the_worker_prefix_f02_wrote() -> TestResult {
    let (walk, worker) = Walk::enrolled(&key(0x41))?;
    let h = walk.h;
    let height = ORIGIN + 140;
    let conflict = envelope(
        &h,
        dispatch::ScheduleEvaluator,
        OWNER,
        4,
        height,
        &nomination(NOMINEE[0]),
        None,
    )?;
    assert_eq!(
        f03(&walk.state, &h, height, &conflict).err(),
        Some(ROLE_CONFLICT)
    );
    let env = envelope(
        &h,
        dispatch::ScheduleEvaluator,
        OWNER,
        4,
        height,
        &nomination(EVALUATOR + 10),
        None,
    )?;
    let next = f03(&walk.state, &h, height, &env)?;
    let before = identity(&walk.state)?;
    let after = identity(&next)?;
    assert_eq!(
        authority::split_identity_section(&after)?.0,
        authority::split_identity_section(&before)?.0
    );
    assert_eq!(authority::evaluator_region(&after)?.len(), 2);
    assert_eq!(workers_of(&next)?.get(worker), Some(walk.record(worker)?));
    Ok(())
}

fn blank_row() -> CodecResult<ParticipantRow> {
    Ok(ParticipantRow {
        kind: ParticipantKind::Evaluator,
        id: [1; 32],
        owner: p(OWNER)?,
        generation: 0,
        identity_state: 0,
        frozen_member: false,
        frozen_generation: Presence::Absent,
        eligibility: 0,
        metadata: Presence::Absent,
        metadata_revision: 0,
        score: ScoreField::absent(Presence::Absent, ScoreStatus::Unavailable)?,
        reward: RewardField::unavailable(Availability::NotEnabled)?,
        history_status: Availability::NotYetProduced,
        history: Presence::Absent,
    })
}

#[test]
fn queries_read_the_worker_prefix_beside_the_region() -> TestResult {
    let (walk, worker) = Walk::enrolled(&key(0x41))?;
    let s = &walk.state;
    let mut expected = [Availability::NotYetProduced; 10];
    expected[0] = Availability::Available;
    expected[1] = Availability::Available;
    expected[5] = Availability::NotEnabled;
    expected[9] = Availability::Available;
    assert_eq!(feature_availability(s)?, expected);

    let blank = blank_row()?;
    let mut rows = [blank; 2];
    assert_eq!(participant_rows(s, None, &mut rows)?, 1);
    assert_eq!(
        rows[0],
        ParticipantRow {
            kind: ParticipantKind::Worker,
            id: worker.bytes(),
            owner: p(NOMINEE)?,
            generation: 1,
            identity_state: WorkerState::Enrolled as u8,
            frozen_member: false,
            frozen_generation: Presence::Absent,
            eligibility: ELIGIBLE_SERVING,
            metadata: Presence::Present(MetadataDigest::new(METADATA)?),
            metadata_revision: 1,
            score: ScoreField::absent(Presence::Absent, ScoreStatus::NotProduced)?,
            reward: RewardField::unavailable(Availability::NotEnabled)?,
            history_status: Availability::NotYetProduced,
            history: Presence::Absent,
        }
    );
    assert_eq!(rows[1], blank);
    Ok(())
}

#[test]
fn a_truncated_worker_prefix_is_refused_by_every_reader() -> TestResult {
    let (walk, worker) = Walk::enrolled(&key(0x41))?;
    let truncated = with_identity(&walk.state, &[1, 0, 0])?;
    assert_eq!(feature_availability(&truncated), Err(NON_CANONICAL));
    let mut rows = [blank_row()?; 2];
    assert_eq!(
        participant_rows(&truncated, None, &mut rows),
        Err(NON_CANONICAL)
    );
    let height = ORIGIN + 150;
    let env = envelope(
        &walk.h,
        dispatch::SetDraining,
        NOMINEE,
        2,
        height,
        worker.as_bytes(),
        None,
    )?;
    let ctx = CallContext {
        market: &walk.h,
        invoking_principal: p(NOMINEE)?,
        height,
    };
    let mut out = vec![0; MAX_STATE_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let mut scratch = vec![0; CONTROL_SCRATCH_BYTES];
    assert_eq!(
        apply(&truncated, &ctx, &env, &mut out, &mut event, &mut scratch),
        Err(NON_CANONICAL)
    );
    Ok(())
}

/// Created market whose identity section is an empty worker table followed by
/// `region_len` opaque region bytes. A real region never reaches the cap (see the
/// const assertion above), so the overflow witness pads the region.
fn padded(region_len: usize) -> CodecResult<(MarketHeader, Vec<u8>, Vec<u8>)> {
    let (h, s0) = created()?;
    let region: Vec<u8> = (0..region_len)
        .map(|i| u8::try_from(i % 251).map_err(|_| ARITHMETIC))
        .collect::<CodecResult<_>>()?;
    let mut section = vec![0];
    section.extend_from_slice(&region);
    Ok((h, with_identity(&s0, &section)?, region))
}

#[test]
fn table_growth_past_the_section_cap_refuses_capacity_without_mutation() -> TestResult {
    let cap = Section::IdentityRoster.payload_cap();
    let height = ORIGIN + 130;
    let nomination = nominate(NOMINEE, NONCE, &key(0x41), ORIGIN + 258);

    // One record fits exactly at the cap and the region rides along unchanged.
    let (h, fits, region) = padded(cap - 1 - WORKER_RECORD_BYTES)?;
    let env = envelope(
        &h,
        dispatch::EnrollWorker,
        OWNER,
        2,
        height,
        &nomination,
        None,
    )?;
    let next = f02(&fits, &h, OWNER, height, &env)?;
    assert_eq!(identity(&next)?.len(), cap);
    assert_eq!(region_of(&next)?, region);
    assert_eq!(workers_of(&next)?.len(), 1);

    // One more region byte pushes the same table past the cap.
    let (h, full, _) = padded(cap - WORKER_RECORD_BYTES)?;
    assert_eq!(identity(&full)?.len(), cap - WORKER_RECORD_BYTES + 1);
    let snapshot = full.clone();
    let ctx = CallContext {
        market: &h,
        invoking_principal: p(OWNER)?,
        height,
    };
    let mut out = vec![0; MAX_STATE_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let mut scratch = vec![0; CONTROL_SCRATCH_BYTES];
    assert_eq!(
        apply(&full, &ctx, &env, &mut out, &mut event, &mut scratch),
        Err(CAPACITY)
    );
    assert_eq!(full, snapshot);
    assert!(workers_of(&full)?.is_empty());
    Ok(())
}
