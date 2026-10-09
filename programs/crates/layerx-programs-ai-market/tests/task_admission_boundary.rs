//! AI.F01-T04-SERVICE task handoff boundary: the F01 task path exactly as a durable worker
//! observes it through `dispatch::route`. The market is produced by the real F01/F02/F03/F06/F08
//! producers; CREATE, `SCHEDULE_ACTIVATION`, `OPEN_EPOCH`, `ADVANCE_ACTIVATION`,
//! `RevokeDelegate` and `ADMIT_TASK`/`ACCEPT_TASK`/`CANCEL_TASK`/`COMMIT_TASK_RESULT`/
//! `SEAL_TASK_SET` are routed, and every case checks the committed task record together with
//! the exact result frame or refusal.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        domain_hash, encode_envelope, Envelope, ResultStatus,
    },
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, F01_ALREADY_CREATED, F01_POLICY_MISMATCH,
        F01_STALE_REVISION, F01_TASK_ALREADY_ACCEPTED, F01_TASK_CONFLICT, F01_TASK_EXPIRED,
        F01_TASK_NOT_FOUND, F01_UNKNOWN_WORKER, F01_WRONG_WORKER, F02_DELEGATE_REVOKED,
        NON_CANONICAL, WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_ROSTER,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader},
    registry_ops::{CallContext, PolicySection, ACTIVE},
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, task_set_digest, SetBinding, TaskBinding, TaskSet, TaskStatus},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, MarketId, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 1000;
const METADATA: [u8; 32] = [0x44; 32];
const ACK: [u8; 32] = [0xa5; 32];
const RESULT: [u8; 32] = [0xe5; 32];
const ROLE_EXPIRY: u64 = 5000;
/// The single frozen worker's owner and the requester of every task.
const WORKER: u8 = 1;
const REQUESTER: u8 = 0x21;
/// Epoch 1 of the market at origin 1000: Work is [1128, 1192), the commit window opens at 1192.
const SEAL_AT: u64 = 1192;

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
fn policy_bytes() -> CodecResult<Vec<u8>> {
    let mut policy = TaskPolicyV1::bounded_default(
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
        100,
        1,
    )?;
    policy.minimum_evaluator_count = 3;
    let mut out = vec![0; TASK_POLICY_BYTES];
    policy.encode(&mut out)?;
    Ok(out)
}
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// A request id unique per kind, subject and sequence.
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

/// One native request envelope.
#[derive(Clone)]
struct Req {
    operation: Operation,
    actor: PrincipalId,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    request: [u8; 32],
    expiry: u64,
    market: Option<MarketId>,
    payload: Vec<u8>,
}
fn req(operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
    Req {
        operation,
        actor,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        request: [1; 32],
        expiry: u64::MAX,
        market: None,
        payload,
    }
}
impl Req {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = ChainDomain::new(CHAIN)?;
        let program = ProgramId::new(PROGRAM)?;
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
    fn digest(&self) -> CodecResult<RequestDigest> {
        decode_envelope(&self.encode()?)?.request_digest()
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

/// The decoded result frame of one routed call.
#[derive(Debug, Eq, PartialEq)]
struct Frame {
    status: ResultStatus,
    error: Option<ApplicationError>,
    request: Presence<RequestDigest>,
    revision: u64,
    digest: [u8; 32],
    payload: Vec<u8>,
}
/// Routes `call` over `current`; returns the result frame and, on `Applied`, the next state.
fn route(call: &Req, current: Option<&[u8]>, at: u64) -> CodecResult<(Frame, Option<Vec<u8>>)> {
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let mut result = vec![0; MAX_RESULT_BYTES];
    let routed = dispatch::route(
        &call.context(at)?,
        &call.encode()?,
        current,
        Buffers {
            next: &mut next,
            scratch: &mut scratch,
            event: &mut event,
            result: &mut result,
        },
    )?;
    let (result_len, committed) = match routed {
        Routed::Applied {
            operation,
            state_len,
            result_len,
            ..
        } => {
            assert_eq!(operation, call.operation);
            next.truncate(state_len);
            (result_len, Some(next))
        }
        Routed::Unchanged { result_len } | Routed::Refused { result_len } => (result_len, None),
    };
    let frame = codec::decode_result(&result[..result_len])?;
    Ok((
        Frame {
            status: frame.status,
            error: frame.error,
            request: frame.request,
            revision: frame.revision,
            digest: frame.digest.bytes(),
            payload: frame.payload.to_vec(),
        },
        committed,
    ))
}

/// The owner CREATE of the market.
fn create_call() -> CodecResult<Req> {
    let program = ProgramId::new(PROGRAM)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&policy_bytes()?);
    payload.extend_from_slice(&[16; 32]);
    Ok(Req {
        sequence: 1,
        ..req(dispatch::CREATE, PrincipalId::new(OWNER)?, payload)
    })
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
/// Market-owner approval bound to the required effective epoch, expiring at its work end.
fn approve(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    delegate: PublicKey32,
    who: PrincipalId,
    at: u64,
) -> CodecResult<(u64, Digest32)> {
    let effective = table.required_effective_epoch(&ctx(market, who, at))?;
    let terms = ApprovalTerms {
        participant,
        owner: who,
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
fn worker_record(n: u8, market: &MarketHeader, slot: u8, at: u64) -> CodecResult<WorkerCurrent> {
    let who = principal(n)?;
    Ok(WorkerCurrent {
        worker: derive_worker(market.market_id, who, [n; 32])?,
        owner: who,
        delegate: public(&delegate_key(n)),
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
fn grant(market: &MarketHeader, n: u8, effective: u64) -> CodecResult<EvaluatorGrant> {
    let key = SigningKey::from_bytes(&[n; 32]);
    EvaluatorGrant::nominate(
        market.market_id,
        principal(n)?,
        [n; 32],
        GrantTerms {
            rubric: rubric()?,
            grant_version: version()?,
            key_version: version()?,
            signing_key: public(&key),
            effective_epoch: effective,
            expiry_epoch_exclusive: effective + 32,
        },
    )
}

/// One committed market state and its next owner sequence.
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    /// The real F01 CREATE through the router at `ORIGIN`.
    fn create() -> CodecResult<Self> {
        let (frame, next) = route(&create_call()?, None, ORIGIN)?;
        assert_eq!(
            (frame.status, frame.error, frame.revision),
            (ResultStatus::Ok, None, 1)
        );
        Ok(Self {
            bytes: next.ok_or(NON_CANONICAL)?,
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
    fn header(&self) -> CodecResult<MarketHeader> {
        Ok(self.section()?.header)
    }
    /// The committed F06 settlement section bytes.
    fn rewards(&self) -> CodecResult<Vec<u8>> {
        Ok(decode_shared_state(&self.bytes)?
            .section(Section::SettlementClaims)?
            .to_vec())
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
    /// Routes `call`, committing the state on `Applied`.
    fn route(&mut self, call: &Req, at: u64) -> CodecResult<Frame> {
        let (frame, next) = route(call, Some(&self.bytes), at)?;
        if let Some(next) = next {
            self.bytes = next;
        }
        Ok(frame)
    }
    /// A routed owner registry operation carrying `body` after the expected revision.
    fn owner_op(&mut self, operation: Operation, body: &[u8], at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(body);
        let call = Req {
            config: self.header()?.active_config_version,
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(operation, PrincipalId::new(OWNER)?, payload)
        };
        let frame = self.route(&call, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        self.owner_sequence += 1;
        Ok(())
    }
}

/// Worker, evaluator and funding producers.
impl World {
    /// F02 ENROLLED record with its worker replay slot and F08 approval plus admission.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<WorkerRosterEntry> {
        self.edit(|parts, market| {
            let slot = parts.workers.free_slot()?;
            let record = worker_record(n, market, slot, at)?;
            parts.workers.insert(&record)?;
            parts.replay.bind(
                ActorSlot::worker(usize::from(slot))?,
                record.owner,
                version()?,
            )?;
            let participant = Participant::Worker(record.worker);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                record.delegate,
                record.owner,
                at,
            )?;
            parts.admission.admit(
                &ctx(market, record.owner, at),
                &Admission {
                    participant,
                    delegate_generation: 1,
                    effective_epoch: effective,
                    config_version: 1,
                    approval_digest: digest,
                },
            )?;
            Ok(WorkerRosterEntry {
                worker: record.worker,
                owner: record.owner,
                recipient: AccountId::new(record.owner.bytes())?,
                generation: version()?,
                key_version: version()?,
                public_key: record.delegate,
                metadata: record.metadata,
            })
        })
    }
    /// F03 nomination accepted through the signed F08 consent of evaluator `n`.
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let who = principal(n)?;
            let key = SigningKey::from_bytes(&[n; 32]);
            let signing_key = public(&key);
            let participant =
                Participant::Evaluator(derive_evaluator(market.market_id, who, [n; 32])?);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                signing_key,
                who,
                at,
            )?;
            let grant = grant(market, n, effective)?;
            let consent = EvaluatorConsent {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                evaluator: grant.evaluator,
                owner: who,
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
                &ctx(market, who, at),
                &grant,
                consent.request,
                &payload,
            )?;
            parts
                .replay
                .bind(ActorSlot::evaluator(usize::from(n - 2))?, who, version()?)?;
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
}

/// The routed epoch, activation and identity calls.
impl World {
    /// Keeper `OPEN_EPOCH` of the clock epoch of `at`, naming the previewed binding.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let frozen = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Req {
            epoch: frozen.epoch,
            config: frozen.config.get(),
            roster: Presence::Present(frozen.roster),
            request: tag(0x60, 0, frozen.epoch),
            ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
        };
        let frame = self.route(&call, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        Ok(frozen)
    }
    /// Keeper `ADVANCE_ACTIVATION` naming the scheduled epoch; the market becomes ACTIVE.
    fn activate(&mut self, at: u64) -> TestResult {
        let header = self.header()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&header.activation_epoch.to_be_bytes());
        let call = Req {
            config: header.active_config_version,
            request: [0x5f; 32],
            ..req(dispatch::ADVANCE_ACTIVATION, principal(KEEPER)?, payload)
        };
        let frame = self.route(&call, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        assert_eq!(self.header()?.lifecycle, ACTIVE);
        Ok(())
    }
    /// F02 `RevokeDelegate` by worker `n`'s owner under its first role sequence; the router
    /// re-binds the F01 header revision to the shared revision.
    fn revoke(&mut self, n: u8, worker: WorkerId, at: u64) -> TestResult {
        let mut payload = worker.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.push(2);
        payload.extend_from_slice(&1u64.to_be_bytes());
        let call = Req {
            sequence: 1,
            request: tag(0xd0, n, 1),
            expiry: at + 1000,
            ..req(dispatch::RevokeDelegate, principal(n)?, payload)
        };
        let before = self.revision()?;
        let frame = self.route(&call, at)?;
        assert_eq!(
            (frame.status, frame.error, frame.revision),
            (ResultStatus::Ok, None, before + 1)
        );
        assert_eq!(self.section()?.header.state_revision, before + 1);
        Ok(())
    }
}

/// A market with an opened, ACTIVE epoch 1 and its single frozen worker.
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
    worker: WorkerRosterEntry,
}
/// Enrolls worker 1 and evaluators 2..=4, schedules activation at epoch 1, funds 500, opens
/// epoch 1 at 1128 and advances the lifecycle to ACTIVE at 1129.
fn opened() -> CodecResult<Market> {
    let mut world = World::create()?;
    let worker = world.enroll(WORKER, ORIGIN + 2)?;
    for n in 2..=4 {
        world.evaluator(n, ORIGIN + 2 + u64::from(n))?;
    }
    world.owner_op(dispatch::SCHEDULE_ACTIVATION, &1u64.to_be_bytes(), 1006)?;
    world.fund(500, 1007)?;
    let frozen = world.open(1128)?;
    assert_eq!(
        (
            frozen.epoch,
            frozen.config.get(),
            frozen.workers,
            frozen.evaluators
        ),
        (1, 1, 1, 3)
    );
    world.activate(1129)?;
    let header = world.header()?;
    Ok(Market {
        world,
        frozen,
        header,
        worker,
    })
}

#[derive(Clone, Copy)]
struct Admit {
    requester: PrincipalId,
    worker: WorkerId,
    policy: [u8; 32],
    metadata: [u8; 32],
    nonce: [u8; 32],
    input: [u8; 32],
    deadline: u64,
}

/// A worker step: `expected_revision || task || digest`.
#[derive(Clone, Copy)]
struct Step {
    operation: Operation,
    worker: u8,
    sequence: u64,
    expected: u64,
    task: TaskId,
    digest: [u8; 32],
}

impl Market {
    fn binding(&self) -> SetBinding {
        SetBinding {
            market: self.header.market_id,
            epoch: self.frozen.epoch,
            config: self.frozen.config,
            policy: self.frozen.policy,
            roster: self.frozen.roster,
        }
    }
    /// An envelope bound to the opened epoch.
    fn bound(&self, operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
        Req {
            epoch: self.frozen.epoch,
            config: self.frozen.config.get(),
            roster: Presence::Present(self.frozen.roster),
            ..req(operation, actor, payload)
        }
    }
    /// The requester's admission of nonce `nonce` for the frozen worker with metadata M.
    fn admission(&self, nonce: u8, deadline: u64) -> CodecResult<Admit> {
        Ok(Admit {
            requester: principal(REQUESTER)?,
            worker: self.worker.worker,
            policy: self.frozen.policy.bytes(),
            metadata: METADATA,
            nonce: [nonce; 32],
            input: [0xa0 + nonce; 32],
            deadline,
        })
    }
    fn admit_payload(&self, admit: &Admit) -> Vec<u8> {
        let mut out = self.frozen.epoch.to_be_bytes().to_vec();
        out.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        out.extend_from_slice(&admit.policy);
        out.extend_from_slice(self.frozen.roster.as_bytes());
        out.extend_from_slice(admit.requester.as_bytes());
        out.extend_from_slice(admit.worker.as_bytes());
        out.extend_from_slice(&admit.metadata);
        out.extend_from_slice(&admit.nonce);
        out.extend_from_slice(&admit.input);
        out.extend_from_slice(&admit.deadline.to_be_bytes());
        out
    }
    fn admit_call(&self, admit: &Admit) -> Req {
        Req {
            request: tag(0xb0, admit.nonce[0], 0),
            ..self.bound(
                dispatch::ADMIT_TASK,
                admit.requester,
                self.admit_payload(admit),
            )
        }
    }
    fn task_id(&self, nonce: u8) -> CodecResult<TaskId> {
        derive_task(
            self.header.market_id,
            self.frozen.epoch,
            principal(REQUESTER)?,
            [nonce; 32],
        )
    }
    fn cancel_call(&self, task: TaskId) -> CodecResult<Req> {
        Ok(self.bound(
            dispatch::CANCEL_TASK,
            principal(REQUESTER)?,
            task.bytes().to_vec(),
        ))
    }
    fn step_call(&self, step: &Step) -> CodecResult<Req> {
        let mut payload = step.expected.to_be_bytes().to_vec();
        payload.extend_from_slice(step.task.as_bytes());
        payload.extend_from_slice(&step.digest);
        let mut request = tag(0x77, step.worker, step.sequence);
        request[9..11].copy_from_slice(&step.operation.selector().to_be_bytes());
        Ok(Req {
            sequence: step.sequence,
            request,
            expiry: ROLE_EXPIRY,
            ..self.bound(step.operation, principal(step.worker)?, payload)
        })
    }
    /// The frozen worker's native step expecting the current revision.
    fn step(
        &self,
        operation: Operation,
        sequence: u64,
        task: TaskId,
        digest: [u8; 32],
    ) -> CodecResult<Req> {
        self.step_call(&Step {
            operation,
            worker: WORKER,
            sequence,
            expected: self.world.revision()?,
            task,
            digest,
        })
    }
    /// Keeper `SEAL_TASK_SET` naming the R044 digest of the committed region.
    fn seal_call(&self) -> CodecResult<(Req, Digest32)> {
        let region = self.region()?;
        let digest = task_set_digest(&self.binding(), &TaskSet::decode(&region)?)?;
        let mut payload = self.frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(digest.as_bytes());
        let call = Req {
            request: tag(0x5e, 0, self.frozen.epoch),
            ..self.bound(dispatch::SEAL_TASK_SET, principal(KEEPER)?, payload)
        };
        Ok((call, digest))
    }
    /// The independently built 115-byte F01 receipt of `call` committed at `revision`.
    fn receipt(&self, call: &Req, revision: u64) -> Vec<u8> {
        let mut bytes = self.header.market_id.as_bytes().to_vec();
        bytes.extend_from_slice(&revision.to_be_bytes());
        bytes.extend_from_slice(&call.request);
        bytes.extend_from_slice(&call.operation.selector().to_be_bytes());
        bytes.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        bytes.push(1);
        bytes.extend_from_slice(self.frozen.policy.as_bytes());
        bytes
    }
    /// Routes `call`, which must apply exactly one revision with the F01 receipt as payload.
    fn applied(&mut self, call: &Req, at: u64) -> CodecResult<Frame> {
        let before = self.world.revision()?;
        let frame = self.world.route(call, at)?;
        assert_eq!(
            (frame.status, frame.error, frame.revision),
            (ResultStatus::Ok, None, before + 1)
        );
        assert_eq!(frame.request, Presence::Present(call.digest()?));
        assert_eq!(frame.payload, self.receipt(call, before + 1));
        assert_eq!(frame.payload.len(), 115);
        assert_eq!(frame.digest, codec::result_digest(&frame.payload)?.bytes());
        assert_eq!(self.world.revision()?, before + 1);
        assert_eq!(self.world.section()?.header.state_revision, before + 1);
        Ok(frame)
    }
    /// Routes `call`, which must be refused at the visible revision without any new state.
    fn refused(&self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let (frame, next) = route(call, Some(&self.world.bytes), at)?;
        assert!(next.is_none());
        assert_eq!(
            (
                frame.status,
                frame.request,
                frame.revision,
                frame.payload.len()
            ),
            (
                ResultStatus::Error,
                Presence::Present(call.digest()?),
                self.world.revision()?,
                0
            )
        );
        frame.error.ok_or(NON_CANONICAL)
    }
    /// Routes `call`, which must be answered `AlreadyApplied` without any new state.
    fn unchanged(&self, call: &Req, at: u64) -> CodecResult<Frame> {
        let (frame, next) = route(call, Some(&self.world.bytes), at)?;
        assert!(next.is_none());
        assert_eq!(
            (frame.status, frame.error, frame.request),
            (
                ResultStatus::AlreadyApplied,
                None,
                Presence::Present(call.digest()?)
            )
        );
        assert_eq!(frame.digest, codec::result_digest(&frame.payload)?.bytes());
        Ok(frame)
    }
    /// The same committed bytes as a freshly started process would decode them.
    fn restart(&self) -> Self {
        Self {
            world: World {
                bytes: self.world.bytes.clone(),
                owner_sequence: self.world.owner_sequence,
            },
            frozen: self.frozen,
            header: self.header,
            worker: self.worker,
        }
    }
    fn region(&self) -> CodecResult<Vec<u8>> {
        Ok(self.world.section()?.task_region.to_vec())
    }
    fn tasks(&self) -> CodecResult<Vec<TaskBinding>> {
        let region = self.region()?;
        TaskSet::decode(&region)?.bindings().collect()
    }
    fn find(&self, task: TaskId) -> CodecResult<TaskBinding> {
        self.tasks()?
            .into_iter()
            .find(|binding| binding.task == task)
            .ok_or(F01_TASK_NOT_FOUND)
    }
}

#[test]
fn a13_routed_admission_binds_the_frozen_worker_commitments() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(2, 1172)?;
    let call = m.admit_call(&admission);
    m.applied(&call, 1140)?;
    let task = m.task_id(2)?;
    assert_eq!(
        m.tasks()?,
        [TaskBinding {
            task,
            requester: principal(REQUESTER)?,
            worker: m.worker.worker,
            input: Digest32::new([0xa2; 32])?,
            deadline: 1172,
            status: TaskStatus::Admitted,
            acknowledgement: None,
            result: None,
            admission: domain_hash("PAXAI/task-admission/v1", &call.payload)?,
        }]
    );
    let region = m.region()?;
    assert_eq!(
        (region.len(), region[..2].to_vec(), region[2]),
        (3 + 233, vec![0, 1], 0)
    );
    Ok(())
}

#[test]
fn a13_finalized_acceptance_then_one_result_commitment() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(2, 1172)?;
    m.applied(&m.admit_call(&admission), 1140)?;
    let task = m.task_id(2)?;
    let admitted = m.find(task)?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, task, ACK)?;
    let accepted = m.applied(&accept, 1141)?;
    let acknowledged = TaskBinding {
        status: TaskStatus::Accepted,
        acknowledgement: Some(Digest32::new(ACK)?),
        ..admitted
    };
    assert_eq!(m.find(task)?, acknowledged);
    let retry = m.unchanged(&accept, 1142)?;
    assert_eq!(
        (retry.revision, retry.payload),
        (accepted.revision, accepted.digest.to_vec())
    );
    assert_eq!(
        m.refused(&m.cancel_call(task)?, 1142)?,
        F01_TASK_ALREADY_ACCEPTED
    );
    assert_eq!(
        m.refused(&m.step(dispatch::ACCEPT_TASK, 2, task, ACK)?, 1142)?,
        F01_TASK_ALREADY_ACCEPTED
    );
    assert_eq!(m.find(task)?, acknowledged);
    let commit = m.step(dispatch::COMMIT_TASK_RESULT, 2, task, RESULT)?;
    let committed = m.applied(&commit, 1171)?;
    let resulted = TaskBinding {
        status: TaskStatus::ResultCommitted,
        result: Some(Digest32::new(RESULT)?),
        ..acknowledged
    };
    assert_eq!(m.find(task)?, resulted);
    let retry = m.unchanged(&commit, 1171)?;
    assert_eq!(
        (retry.revision, retry.payload),
        (committed.revision, committed.digest.to_vec())
    );
    assert_eq!(
        m.refused(
            &m.step(dispatch::COMMIT_TASK_RESULT, 3, task, [0xe6; 32])?,
            1171
        )?,
        F01_TASK_CONFLICT
    );
    assert_eq!(
        m.refused(
            &m.step(dispatch::COMMIT_TASK_RESULT, 3, task, [0xe6; 32])?,
            1172
        )?,
        F01_TASK_EXPIRED
    );
    assert_eq!(m.find(task)?, resulted);
    assert_eq!(m.world.revision()?, committed.revision);
    Ok(())
}

#[test]
fn a13_commit_at_the_deadline_is_expired_and_keeps_the_acceptance() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(4, 1172)?;
    m.applied(&m.admit_call(&admission), 1140)?;
    let task = m.task_id(4)?;
    m.applied(&m.step(dispatch::ACCEPT_TASK, 1, task, ACK)?, 1141)?;
    let accepted = m.find(task)?;
    let late = m.step(dispatch::COMMIT_TASK_RESULT, 2, task, RESULT)?;
    assert_eq!(m.refused(&late, 1172)?, F01_TASK_EXPIRED);
    assert_eq!(m.refused(&late, 1180)?, F01_TASK_EXPIRED);
    assert_eq!(
        (accepted.status, accepted.acknowledgement, accepted.result),
        (TaskStatus::Accepted, Some(Digest32::new(ACK)?), None)
    );
    assert_eq!(m.find(task)?, accepted);
    Ok(())
}

#[test]
fn a13_worker_step_refusals_leave_ack_and_result_unchanged() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(2, 1172)?;
    m.applied(&m.admit_call(&admission), 1140)?;
    let task = m.task_id(2)?;
    m.applied(&m.step(dispatch::ACCEPT_TASK, 1, task, ACK)?, 1141)?;
    let accepted = m.find(task)?;
    let revision = m.world.revision()?;
    let commit = Step {
        operation: dispatch::COMMIT_TASK_RESULT,
        worker: WORKER,
        sequence: 2,
        expected: revision,
        task,
        digest: RESULT,
    };
    let wrong_worker = Step {
        worker: 5,
        ..commit
    };
    let zero_result = Step {
        digest: [0; 32],
        ..commit
    };
    let stale = Step {
        expected: revision - 1,
        ..commit
    };
    let missing = Step {
        task: TaskId::new([0x5a; 32])?,
        ..commit
    };
    let cases = [
        (m.step_call(&wrong_worker)?, F01_WRONG_WORKER),
        (m.step_call(&zero_result)?, NON_CANONICAL),
        (m.step_call(&stale)?, F01_STALE_REVISION),
        (m.step_call(&missing)?, F01_TASK_NOT_FOUND),
        (
            Req {
                market: Some(MarketId::new([9; 32])?),
                ..m.step_call(&commit)?
            },
            WRONG_MARKET,
        ),
        (
            Req {
                config: 2,
                ..m.step_call(&commit)?
            },
            WRONG_CONFIG,
        ),
        (
            Req {
                epoch: 2,
                ..m.step_call(&commit)?
            },
            WRONG_EPOCH,
        ),
    ];
    for (call, error) in &cases {
        assert_eq!(m.refused(call, 1150)?, *error);
        assert_eq!(m.find(task)?, accepted);
    }
    m.applied(&m.step_call(&commit)?, 1150)?;
    let committed = m.find(task)?;
    assert_eq!(
        (committed.acknowledgement, committed.result),
        (Some(Digest32::new(ACK)?), Some(Digest32::new(RESULT)?))
    );
    let foreign = Step {
        operation: dispatch::ACCEPT_TASK,
        worker: 5,
        sequence: 1,
        expected: revision + 1,
        task,
        digest: [0xa6; 32],
    };
    assert_eq!(m.refused(&m.step_call(&foreign)?, 1151)?, F01_WRONG_WORKER);
    assert_eq!(m.find(task)?, committed);
    Ok(())
}

#[test]
fn a13_admission_refusals_take_no_slot() -> TestResult {
    let m = opened()?;
    let admission = m.admission(2, 1172)?;
    let wrong_policy = Admit {
        policy: [0x33; 32],
        ..admission
    };
    let cases = [
        (m.admit_call(&wrong_policy), F01_POLICY_MISMATCH),
        (
            Req {
                market: Some(MarketId::new([9; 32])?),
                ..m.admit_call(&admission)
            },
            WRONG_MARKET,
        ),
        (
            Req {
                config: 2,
                ..m.admit_call(&admission)
            },
            WRONG_CONFIG,
        ),
    ];
    for (call, error) in &cases {
        assert_eq!(m.refused(call, 1140)?, *error);
    }
    assert!(m.tasks()?.is_empty());
    Ok(())
}

#[test]
fn a15_expired_acceptance_and_late_result_leave_the_chain_unchanged() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(3, 1150)?;
    m.applied(&m.admit_call(&admission), 1140)?;
    let task = m.task_id(3)?;
    let admitted = m.find(task)?;
    let rewards = m.world.rewards()?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, task, ACK)?;
    assert_eq!(m.refused(&accept, 1150)?, F01_TASK_EXPIRED);
    assert_eq!(m.refused(&accept, 1151)?, F01_TASK_EXPIRED);
    let late = m.step(dispatch::COMMIT_TASK_RESULT, 1, task, RESULT)?;
    assert_eq!(m.refused(&late, 1151)?, F01_TASK_EXPIRED);
    assert_eq!(m.refused(&m.cancel_call(task)?, 1151)?, F01_TASK_EXPIRED);
    assert_eq!(m.find(task)?, admitted);
    assert_eq!(
        (admitted.status, admitted.acknowledgement, admitted.result),
        (TaskStatus::Admitted, None, None)
    );
    let (seal, digest) = m.seal_call()?;
    m.applied(&seal, SEAL_AT)?;
    assert_eq!(m.tasks()?, [admitted]);
    assert_eq!(m.world.rewards()?, rewards);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(tasks::sealed_task_set(&state, 1)?, digest);
    Ok(())
}

#[test]
fn a15_restart_from_committed_bytes_recovers_one_task_identity() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(3, 1172)?;
    let admit = m.admit_call(&admission);
    m.applied(&admit, 1140)?;
    let task = m.task_id(3)?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, task, ACK)?;
    let accepted = m.applied(&accept, 1141)?;
    let before = m.find(task)?;
    let mut restarted = m.restart();
    let revision = restarted.world.revision()?;
    let repeat = restarted.unchanged(&admit, 1142)?;
    assert_eq!(
        (repeat.revision, repeat.payload.as_slice()),
        (revision, task.as_bytes().as_slice())
    );
    let resent = Req {
        request: tag(0xb1, 3, 1),
        ..admit.clone()
    };
    let again = restarted.unchanged(&resent, 1142)?;
    assert_eq!((again.revision, again.payload), (revision, repeat.payload));
    let other_input = Admit {
        input: [0xb3; 32],
        ..admission
    };
    assert_eq!(
        restarted.refused(&restarted.admit_call(&other_input), 1142)?,
        F01_TASK_CONFLICT
    );
    let retry = restarted.unchanged(&accept, 1142)?;
    assert_eq!(
        (retry.revision, retry.payload),
        (accepted.revision, accepted.digest.to_vec())
    );
    assert_eq!(
        restarted.refused(&restarted.step(dispatch::ACCEPT_TASK, 2, task, ACK)?, 1142)?,
        F01_TASK_ALREADY_ACCEPTED
    );
    assert_eq!(restarted.tasks()?, [before]);
    let (create, next) = route(&create_call()?, Some(&restarted.world.bytes), 1142)?;
    assert!(next.is_none());
    assert_eq!(create.error, Some(F01_ALREADY_CREATED));
    assert_eq!(restarted.world.header()?.origin_height, ORIGIN);
    let commit = restarted.step(dispatch::COMMIT_TASK_RESULT, 2, task, RESULT)?;
    restarted.applied(&commit, 1150)?;
    assert_eq!(
        restarted.tasks()?,
        [TaskBinding {
            status: TaskStatus::ResultCommitted,
            result: Some(Digest32::new(RESULT)?),
            ..before
        }]
    );
    Ok(())
}

#[test]
fn a18_routed_admission_checks_frozen_worker_commitments() -> TestResult {
    let mut m = opened()?;
    let admission = m.admission(1, 1172)?;
    let other_metadata = Admit {
        metadata: [0x45; 32],
        ..admission
    };
    assert_eq!(
        m.refused(&m.admit_call(&other_metadata), 1140)?,
        WRONG_ROSTER
    );
    let unknown = Admit {
        worker: derive_worker(m.header.market_id, principal(9)?, [9; 32])?,
        ..admission
    };
    assert_eq!(
        m.refused(&m.admit_call(&unknown), 1140)?,
        F01_UNKNOWN_WORKER
    );
    assert!(m.tasks()?.is_empty());
    m.applied(&m.admit_call(&admission), 1140)?;
    let admitted = m.find(m.task_id(1)?)?;
    assert_eq!(
        (admitted.status, admitted.acknowledgement, admitted.result),
        (TaskStatus::Admitted, None, None)
    );
    let worker = m.worker.worker;
    m.world.revoke(WORKER, worker, 1141)?;
    let revoked = m.admission(2, 1172)?;
    assert_eq!(
        m.refused(&m.admit_call(&revoked), 1142)?,
        F02_DELEGATE_REVOKED
    );
    assert_eq!(m.tasks()?, [admitted]);
    Ok(())
}
