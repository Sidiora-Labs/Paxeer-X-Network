//! AI.F01-T03 close journal over the complete shared state value. Every market is produced by
//! the real F01/F02/F03/F06/F08 producers; CREATE, policy, activation, `OPEN_EPOCH` and the
//! task-set selectors go through `dispatch::route`, and `REQUEST_CLOSE`/`ADVANCE_CLOSE` go
//! through `closure::apply` with real envelopes. F06 claims, terminalization and the
//! reference reward transitions are the real `RewardState` transitions.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    closure::{self, Outcome as Close},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        encode_envelope, Envelope, ResultStatus,
    },
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, F01_ALREADY_ACTIVATED, F01_ALREADY_CLOSING,
        F01_ALREADY_CREATED, F01_INVALID_REASON, F01_LIFECYCLE_CLOSED, F01_OBLIGATIONS_OUTSTANDING,
        F01_STALE_REVISION, F01_WRONG_LIFECYCLE, F06_CLAIM_EXPIRED, F06_REFUND_RECIPIENT_MISMATCH,
        F06_WRONG_CLAIM_RECIPIENT, INSUFFICIENT_FREE, NON_CANONICAL, NOT_FOUND, STALE_CURSOR,
        UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_PHASE,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{
        fold_policy_history, PolicyCommitments, PolicyHistoryHeader, TaskPolicyV1,
        TASK_POLICY_BYTES,
    },
    registry::{derive_rewards_account, MarketHeader, OperatorGrant},
    registry_ops::{
        CallContext, PolicySection, ACTIVATED, ACTIVE, CANCELLED, CLOSED, REGISTERED, WINDING_DOWN,
    },
    reward_math::allocate,
    rewards::{
        decode_reward_state, ClaimRequest, FundReplay, FundRequest, FundingAuthority, FundingPhase,
        RefundRequest, RewardEffect, RewardLedger, RewardState, FUNDING_POLICY_VERSION,
        REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayDecision,
        ReplayRequest, ReplayTable, Section, SharedState,
    },
    tasks::{task_set_digest, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, FrozenBinding, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Version, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OTHER_PROGRAM: [u8; 32] = [15; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 128;
const METADATA: [u8; 32] = [0x44; 32];
const REASON: [u8; 32] = [0x52; 32];
const ACK: [u8; 32] = [0xa5; 32];
const RESULT: [u8; 32] = [0xe5; 32];
/// Epoch 6 of the market at origin 128: T = 896, Work [896, 960), commit from 960.
const OPEN_AT: u64 = 896;
const SEAL_AT: u64 = 960;
const TERMINAL_AT: u64 = 1008;
/// F06 claim expiry of epoch 6: terminal height + 4096.
const EXPIRY: u64 = TERMINAL_AT + 4096;
/// The single frozen worker.
const LOW: u8 = 0x20;
const BUDGET: u128 = 90;
const FUNDING: u128 = 100;

fn principal(n: u8) -> CodecResult<PrincipalId> {
    let mut bytes = [0x50; 32];
    bytes[0] = n;
    PrincipalId::new(bytes)
}
fn owner() -> CodecResult<PrincipalId> {
    PrincipalId::new(OWNER)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn rubric() -> CodecResult<RubricDigest> {
    RubricDigest::new([4; 32])
}
fn policy(config: u64) -> CodecResult<TaskPolicyV1> {
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
        BUDGET,
        1,
    )?;
    policy.minimum_evaluator_count = 3;
    Ok(policy)
}
fn policy_bytes(config: u64) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; TASK_POLICY_BYTES];
    policy(config)?.encode(&mut out)?;
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
    program: [u8; 32],
    actor: PrincipalId,
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
        program: PROGRAM,
        actor,
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
        let program = ProgramId::new(self.program)?;
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
            program: ProgramId::new(self.program)?,
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
    revision: u64,
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
            state_len,
            result_len,
            ..
        } => {
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
            revision: frame.revision,
            payload: frame.payload.to_vec(),
        },
        committed,
    ))
}

/// The owner CREATE of the market of `program`.
fn create_call(program_bytes: [u8; 32]) -> CodecResult<Req> {
    let program = ProgramId::new(program_bytes)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&policy_bytes(1)?);
    payload.extend_from_slice(&[16; 32]);
    Ok(Req {
        program: program_bytes,
        sequence: 1,
        ..req(dispatch::CREATE, owner()?, payload)
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
    EvaluatorGrant::nominate(
        market.market_id,
        principal(n)?,
        [n; 32],
        GrantTerms {
            rubric: rubric()?,
            grant_version: version()?,
            key_version: version()?,
            signing_key: public(&evaluator_key(n)),
            effective_epoch: effective,
            expiry_epoch_exclusive: effective + 32,
        },
    )
}
fn counters(ledger: &RewardLedger) -> [u128; 6] {
    [
        ledger.tracked_deposits,
        ledger.total_claimed,
        ledger.tracked_refunds,
        ledger.free,
        ledger.reserved,
        ledger.liability,
    ]
}

/// One committed market state and its next owner sequence.
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    /// The real F01 CREATE through the router at `ORIGIN`.
    fn create() -> CodecResult<Self> {
        let (frame, next) = route(&create_call(PROGRAM)?, None, ORIGIN)?;
        assert_eq!((frame.status, frame.revision), (ResultStatus::Ok, 1));
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
    fn reward_state(&self) -> CodecResult<RewardState<'_>> {
        let state = decode_shared_state(&self.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)
    }
    fn ledger(&self) -> CodecResult<[u128; 6]> {
        Ok(counters(&self.reward_state()?.ledger()?))
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
    /// The next owner registry request carrying `body` after the expected revision.
    fn owner_call(&self, operation: Operation, body: &[u8]) -> CodecResult<Req> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(body);
        Ok(Req {
            config: self.header()?.active_config_version,
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(operation, owner()?, payload)
        })
    }
    /// A routed owner registry operation that must apply.
    fn owner_op(&mut self, operation: Operation, body: &[u8], at: u64) -> CodecResult<Frame> {
        let call = self.owner_call(operation, body)?;
        let frame = self.route(&call, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        self.owner_sequence += 1;
        Ok(frame)
    }
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
/// An envelope bound to the opened epoch `frozen`.
fn bound(frozen: &Frozen, operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
    Req {
        epoch: frozen.epoch,
        config: frozen.config.get(),
        roster: Presence::Present(frozen.roster),
        ..req(operation, actor, payload)
    }
}

/// Worker, evaluator, funding and settlement producers.
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
            roster_entry(&record)
        })
    }
    /// F03 nomination accepted through the signed F08 consent of evaluator `n`.
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let who = principal(n)?;
            let key = evaluator_key(n);
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
    /// Real F06 `TerminalizeRewards` of `frozen`, weight 5 for every frozen worker; the F05
    /// bytes after the reward state are carried unchanged.
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
    /// Real F06 `Claim` of epoch 6, committed at the next revision only when it pays.
    fn claim(&mut self, request: &ClaimRequest, at: u64) -> CodecResult<RewardEffect> {
        let mut paid = vec![0; REWARD_STATE_BYTES];
        let (_, effect) = self.reward_state()?.claim(6, request, at, &mut paid)?;
        if matches!(effect, RewardEffect::Payout { .. }) {
            self.edit(|parts, _| {
                paid.extend_from_slice(parts.rewards.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?);
                parts.rewards = paid;
                Ok(())
            })?;
        }
        Ok(effect)
    }
}

/// The routed `OPEN_EPOCH`, `ADVANCE_ACTIVATION` and task-set calls.
impl World {
    /// Keeper `OPEN_EPOCH` of the clock epoch of `at` through the router.
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
    /// Keeper `ADVANCE_ACTIVATION` naming the scheduled epoch at the current revision.
    fn activate_call(&self) -> CodecResult<Req> {
        let header = self.header()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&header.activation_epoch.to_be_bytes());
        Ok(Req {
            config: header.active_config_version,
            request: [0x5f; 32],
            ..req(dispatch::ADVANCE_ACTIVATION, principal(KEEPER)?, payload)
        })
    }
    /// One routed task-set call that must apply.
    fn task(&mut self, call: &Req, at: u64) -> TestResult {
        let frame = self.route(call, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        Ok(())
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's region; returns the sealed digest.
    fn seal(&mut self, frozen: &Frozen, at: u64) -> CodecResult<Digest32> {
        let set = SetBinding {
            market: self.header()?.market_id,
            epoch: frozen.epoch,
            config: frozen.config,
            policy: frozen.policy,
            roster: frozen.roster,
        };
        let region = self.section()?.task_region.to_vec();
        let digest = task_set_digest(&set, &TaskSet::decode(&region)?)?;
        let mut payload = frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&frozen.config.get().to_be_bytes());
        payload.extend_from_slice(digest.as_bytes());
        let call = Req {
            request: tag(0x5e, 0, frozen.epoch),
            ..bound(frozen, dispatch::SEAL_TASK_SET, principal(KEEPER)?, payload)
        };
        self.task(&call, at)?;
        Ok(digest)
    }
}

/// Created at `ORIGIN` with worker `LOW` and evaluators 2..=4 enrolled.
fn prepared() -> CodecResult<(World, WorkerRosterEntry)> {
    let mut world = World::create()?;
    let worker = world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    Ok((world, worker))
}
/// Activation scheduled for epoch 6 at 134 (routed), 100 funded at 135, epoch 6 opened at
/// 896 with budget 90 and the lifecycle advanced to ACTIVE at 897 (both routed).
fn opened() -> CodecResult<(World, WorkerRosterEntry, Frozen)> {
    let (mut world, worker) = prepared()?;
    world.owner_op(dispatch::SCHEDULE_ACTIVATION, &6u64.to_be_bytes(), 134)?;
    world.fund(FUNDING, 135)?;
    let frozen = world.open(OPEN_AT)?;
    assert_eq!(
        (
            frozen.epoch,
            frozen.workers,
            frozen.evaluators,
            frozen.budget
        ),
        (6, 1, 3, BUDGET)
    );
    let call = world.activate_call()?;
    let frame = world.route(&call, OPEN_AT + 1)?;
    assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
    assert_eq!(world.header()?.lifecycle, ACTIVE);
    Ok((world, worker, frozen))
}
/// The opened market with its empty task set sealed at 960 and epoch 6 terminalized at
/// 1008: Free 10, Liability 90 owed to worker `LOW`; returns the sealed digest.
fn terminal() -> CodecResult<(World, WorkerRosterEntry, Digest32)> {
    let (mut world, worker, frozen) = opened()?;
    let sealed = world.seal(&frozen, SEAL_AT)?;
    world.terminalize(&frozen, &[worker], TERMINAL_AT)?;
    assert_eq!(
        world.ledger()?,
        [FUNDING, 0, 0, FUNDING - BUDGET, 0, BUDGET]
    );
    Ok((world, worker, sealed))
}
fn worker_claim(worker: &WorkerRosterEntry) -> ClaimRequest {
    ClaimRequest {
        worker: worker.worker,
        recipient: worker.recipient,
        amount: BUDGET,
    }
}
/// Requester admission of one task for worker `worker` in the opened epoch, deadline 930.
fn admit(
    frozen: &Frozen,
    worker: &WorkerRosterEntry,
    requester: u8,
    nonce: u8,
) -> CodecResult<Req> {
    let mut payload = frozen.epoch.to_be_bytes().to_vec();
    payload.extend_from_slice(&frozen.config.get().to_be_bytes());
    payload.extend_from_slice(frozen.policy.as_bytes());
    payload.extend_from_slice(frozen.roster.as_bytes());
    payload.extend_from_slice(principal(requester)?.as_bytes());
    payload.extend_from_slice(worker.worker.as_bytes());
    payload.extend_from_slice(&METADATA);
    payload.extend_from_slice(&[nonce; 32]);
    payload.extend_from_slice(&[0xa0 + nonce; 32]);
    payload.extend_from_slice(&930u64.to_be_bytes());
    Ok(Req {
        request: tag(0xb0, requester, 0),
        ..bound(frozen, dispatch::ADMIT_TASK, principal(requester)?, payload)
    })
}

/// The settlement change one `ADVANCE_CLOSE` must make.
#[derive(Clone, Copy)]
enum Settle {
    Keep,
    Refund,
    Expire(u64),
}
/// The journal position, F06 effect, event subject and settlement change of one step.
#[derive(Clone, Copy)]
struct Expect {
    lifecycle: u8,
    phase: u8,
    effect: RewardEffect,
    subject: [u8; 32],
    settle: Settle,
}
/// A step to `phase` without any F06 change.
const fn step(phase: u8, subject: [u8; 32]) -> Expect {
    Expect {
        lifecycle: WINDING_DOWN,
        phase,
        effect: RewardEffect::NoTransfer,
        subject,
        settle: Settle::Keep,
    }
}
/// The phase 3 `RefundFree` of `amount` to the immutable recipient.
fn refund(amount: u128) -> CodecResult<Expect> {
    Ok(Expect {
        lifecycle: WINDING_DOWN,
        phase: 4,
        effect: RewardEffect::Payout {
            recipient: AccountId::new(REFUND)?,
            amount,
        },
        subject: REFUND,
        settle: Settle::Refund,
    })
}
/// The phase 4 release of epoch 6's `amount` of expired entitlements.
fn released(amount: u128) -> Expect {
    let mut subject = [0; 32];
    subject[24..].copy_from_slice(&6u64.to_be_bytes());
    Expect {
        lifecycle: WINDING_DOWN,
        phase: 3,
        effect: RewardEffect::Released(amount),
        subject,
        settle: Settle::Expire(6),
    }
}
/// The Closed tombstone; its subject is the closing request digest.
const fn closed() -> Expect {
    Expect {
        lifecycle: CLOSED,
        ..step(5, [0; 32])
    }
}
/// `old` committed at the next revision with `change` applied to the F01 header and the
/// settlement section replaced by `rewards`; every other section and the control payload are
/// carried byte for byte.
fn advanced(
    old: &[u8],
    rewards: Option<&[u8]>,
    change: impl FnOnce(&mut MarketHeader),
) -> CodecResult<Vec<u8>> {
    let state = decode_shared_state(old)?;
    let revision = state.revision + 1;
    let mut section =
        PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    change(&mut section.header);
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    let mut feature_sections = state.feature_sections;
    feature_sections[Section::PolicyLifecycle.index()] = &policy;
    if let Some(rewards) = rewards {
        feature_sections[Section::SettlementClaims.index()] = rewards;
    }
    encode(&SharedState {
        revision,
        feature_sections,
        control: state.control.clone(),
    })
}

/// The `REQUEST_CLOSE`/`ADVANCE_CLOSE` calls through `closure::apply`.
impl World {
    /// The independently built F01 receipt of `call` committed at `revision`.
    fn receipt(&self, call: &Req, revision: u64) -> CodecResult<Vec<u8>> {
        let section = self.section()?;
        let mut bytes = section.header.market_id.as_bytes().to_vec();
        bytes.extend_from_slice(&revision.to_be_bytes());
        bytes.extend_from_slice(&call.request);
        bytes.extend_from_slice(&call.operation.selector().to_be_bytes());
        bytes.extend_from_slice(&section.header.active_config_version.to_be_bytes());
        bytes.push(1);
        bytes.extend_from_slice(section.current.digest()?.as_bytes());
        Ok(bytes)
    }
    /// `closure::apply` of `call` at `at`. On `Applied` the receipt and event are checked,
    /// `next` is committed and the event suffix returned.
    fn close(&mut self, call: &Req, at: u64) -> CodecResult<(Close, Vec<u8>)> {
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; closure::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = closure::apply(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        let Close::Applied {
            receipt,
            revision,
            state_len,
            event_len,
            ..
        } = outcome
        else {
            return Ok((outcome, Vec::new()));
        };
        let bytes = self.receipt(call, revision)?;
        assert_eq!(revision, self.revision()? + 1);
        assert_eq!(receipt.bytes().as_slice(), bytes.as_slice());
        assert_eq!(receipt.digest, codec::result_digest(&bytes)?);
        let header = self.header()?;
        let mut topic = [0; 64];
        let topic_len = codec::event_topic(call.operation, &mut topic)?;
        let (named, common, suffix) =
            codec::decode_event_frame(&topic[..topic_len], &event[..event_len])?;
        assert_eq!(
            (named, common.market, common.epoch, common.config.get()),
            (
                call.operation,
                header.market_id,
                (at - ORIGIN) / 128,
                header.active_config_version
            )
        );
        assert_eq!(
            (common.revision, common.request, common.result),
            (revision, call.digest()?, receipt.digest)
        );
        let suffix = suffix.to_vec();
        next.truncate(state_len);
        self.bytes = next;
        Ok((outcome, suffix))
    }
    /// A refused close call; the committed bytes stay unchanged.
    fn refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        let before = self.bytes.clone();
        let error = self.close(call, at).err().ok_or(NON_CANONICAL)?;
        assert_eq!(self.bytes, before);
        Ok(error)
    }
    /// The owner `REQUEST_CLOSE` at `at`, which must apply; returns its call.
    fn request_close(&mut self, at: u64) -> CodecResult<Req> {
        let config = self.header()?.active_config_version;
        let call = self.owner_call(dispatch::REQUEST_CLOSE, &REASON)?;
        let (outcome, suffix) = self.close(&call, at)?;
        let Close::Applied {
            lifecycle,
            phase,
            cursor,
            effect,
            ..
        } = outcome
        else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(
            (lifecycle, phase, cursor, effect),
            (WINDING_DOWN, 1, 0, RewardEffect::NoTransfer)
        );
        let mut expected = vec![WINDING_DOWN];
        expected.extend_from_slice(&config.to_be_bytes());
        expected.extend_from_slice(&at.to_be_bytes());
        expected.extend_from_slice(&REASON);
        assert_eq!(suffix, expected);
        self.owner_sequence += 1;
        Ok(call)
    }
    /// A keeper `ADVANCE_CLOSE` naming journal `phase` and `cursor` at the current revision.
    fn journal_call(&self, phase: u8, cursor: u16) -> CodecResult<Req> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.push(phase);
        payload.extend_from_slice(&cursor.to_be_bytes());
        Ok(Req {
            config: self.header()?.active_config_version,
            request: tag(0xc0, phase, u64::from(cursor)),
            ..req(dispatch::ADVANCE_CLOSE, principal(KEEPER)?, payload)
        })
    }
    fn advance_call(&self) -> CodecResult<Req> {
        let header = self.header()?;
        self.journal_call(header.close_phase, header.close_cursor)
    }
    /// The settlement section `settle` must produce from the current state for `call`.
    fn settled(&self, call: &Req, at: u64, settle: Settle) -> CodecResult<Option<Vec<u8>>> {
        let mut head = vec![0; REWARD_STATE_BYTES];
        match settle {
            Settle::Keep => return Ok(None),
            Settle::Refund => {
                let state = self.reward_state()?;
                let ledger = state.ledger()?;
                let receipt = self.receipt(call, self.revision()? + 1)?;
                state.refund_free(
                    FundingPhase::Closing,
                    &RefundRequest {
                        expected_refunded: ledger.tracked_refunds,
                        amount: ledger.free,
                        recipient: AccountId::new(REFUND)?,
                    },
                    call.digest()?,
                    codec::result_digest(&receipt)?,
                    &mut head,
                )?;
            }
            Settle::Expire(epoch) => {
                self.reward_state()?
                    .expire_epoch_claims(epoch, at, &mut head)?;
            }
        }
        let tail = self.parts()?.rewards;
        head.extend_from_slice(tail.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?);
        Ok(Some(head))
    }
    /// One keeper `ADVANCE_CLOSE` at `at` whose committed bytes must equal the prior state
    /// with the journal moved as `expect` says.
    fn advance(&mut self, at: u64, expect: Expect) -> TestResult {
        let old = self.bytes.clone();
        let header = self.header()?;
        let call = self.advance_call()?;
        let rewards = self.settled(&call, at, expect.settle)?;
        let (outcome, suffix) = self.close(&call, at)?;
        let cursor = header.close_cursor + 1;
        let Close::Applied {
            lifecycle,
            phase,
            cursor: moved,
            effect,
            ..
        } = outcome
        else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(
            (lifecycle, phase, moved, effect),
            (expect.lifecycle, expect.phase, cursor, expect.effect)
        );
        let (subject, closing) = if expect.lifecycle == CLOSED {
            let digest = call.digest()?.bytes();
            (digest, digest)
        } else {
            (expect.subject, header.closing_request_digest)
        };
        let amount = match expect.effect {
            RewardEffect::Payout { amount, .. } | RewardEffect::Released(amount) => amount,
            _ => 0,
        };
        let mut expected = vec![expect.lifecycle, expect.phase];
        expected.extend_from_slice(&cursor.to_be_bytes());
        expected.extend_from_slice(&amount.to_be_bytes());
        expected.extend_from_slice(&subject);
        assert_eq!(suffix, expected);
        let state = advanced(&old, rewards.as_deref(), |h| {
            h.lifecycle = expect.lifecycle;
            h.close_phase = expect.phase;
            h.close_cursor = cursor;
            h.closing_request_digest = closing;
        })?;
        assert_eq!(self.bytes, state);
        Ok(())
    }
}

#[test]
fn request_close_withdraws_policy_operator_and_activation() -> TestResult {
    let mut world = World::create()?;
    for config in 2..=4u64 {
        let mut body = policy_bytes(config)?;
        body.extend_from_slice(&10u64.to_be_bytes());
        world.owner_op(dispatch::STAGE_POLICY, &body, 130)?;
        world.owner_op(dispatch::CANCEL_POLICY, &config.to_be_bytes(), 130)?;
    }
    let mut body = policy_bytes(5)?;
    body.extend_from_slice(&10u64.to_be_bytes());
    world.owner_op(dispatch::STAGE_POLICY, &body, 131)?;
    let mut body = principal(0x66)?.as_bytes().to_vec();
    body.push(3);
    body.extend_from_slice(&0u64.to_be_bytes());
    world.owner_op(dispatch::APPOINT_OPERATOR, &body, 132)?;
    world.owner_op(dispatch::SCHEDULE_ACTIVATION, &6u64.to_be_bytes(), 133)?;
    let old = world.bytes.clone();
    let old_state = decode_shared_state(&old)?;
    let before =
        PolicySection::decode(old_state.feature_sections[Section::PolicyLifecycle.index()])?;
    let (Presence::Present(pending), Presence::Present(grant)) = (before.pending, before.operator)
    else {
        return Err(NOT_FOUND);
    };
    assert_eq!(before.recent().len(), 4);
    assert_eq!(before.recent()[0].disposition, ACTIVATED);
    assert!(before.header.activation_scheduled);
    let call = world.request_close(300)?;
    let revision = world.revision()?;
    let after = world.section()?;
    assert_eq!(
        after.header,
        MarketHeader {
            lifecycle: WINDING_DOWN,
            state_revision: revision,
            activation_epoch: 0,
            activation_scheduled: false,
            closure_requested_at: 300,
            close_phase: 1,
            close_cursor: 0,
            ..before.header
        }
    );
    assert_eq!(
        after.operator,
        Presence::Present(OperatorGrant {
            revoked: true,
            ..grant
        })
    );
    assert_eq!(
        (after.current, after.pending, after.task_region),
        (before.current, Presence::Absent, before.task_region)
    );
    let mut recent = before.recent()[1..].to_vec();
    recent.push(PolicyHistoryHeader {
        config_version: 5,
        digest: pending.digest,
        effective_epoch: 10,
        disposition: CANCELLED,
    });
    assert_eq!(after.recent(), recent.as_slice());
    assert_eq!(
        after.history_root,
        fold_policy_history(before.history_root, 0, &before.recent()[..1])?
    );
    let state = decode_shared_state(&world.bytes)?;
    assert_eq!(state.revision, old_state.revision + 1);
    assert_eq!(state.feature_sections[1..], old_state.feature_sections[1..]);
    let request = ReplayRequest::from_envelope(
        ActorSlot::OWNER,
        version()?,
        &decode_envelope(&call.encode()?)?,
    )?;
    let ReplayDecision::AlreadyApplied(retained) = state.control.replay.check(&request, 301)?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (retained.applied_revision, retained.result_digest),
        (
            revision,
            codec::result_digest(&world.receipt(&call, revision)?)?
        )
    );
    Ok(())
}

#[test]
fn request_close_refusals_leave_the_market_unchanged() -> TestResult {
    let (mut world, _, _) = opened()?;
    let stranger = Req {
        actor: principal(0x66)?,
        sequence: 1,
        ..world.owner_call(dispatch::REQUEST_CLOSE, &REASON)?
    };
    assert_eq!(world.refused(&stranger, 900)?, UNAUTHORIZED);
    let zero = world.owner_call(dispatch::REQUEST_CLOSE, &[0; 32])?;
    assert_eq!(world.refused(&zero, 900)?, F01_INVALID_REASON);
    let mut stale = world.owner_call(dispatch::REQUEST_CLOSE, &REASON)?;
    stale.payload[..8].copy_from_slice(&(world.revision()? - 1).to_be_bytes());
    assert_eq!(world.refused(&stale, 900)?, F01_STALE_REVISION);
    let config = Req {
        config: 2,
        ..world.owner_call(dispatch::REQUEST_CLOSE, &REASON)?
    };
    assert_eq!(world.refused(&config, 900)?, WRONG_CONFIG);
    let other = world.owner_call(dispatch::SCHEDULE_ACTIVATION, &7u64.to_be_bytes())?;
    assert_eq!(world.refused(&other, 900)?, UNKNOWN_OPERATION);
    let early = world.advance_call()?;
    assert_eq!(world.refused(&early, 900)?, F01_WRONG_LIFECYCLE);
    world.request_close(900)?;
    let again = world.owner_call(dispatch::REQUEST_CLOSE, &REASON)?;
    assert_eq!(world.refused(&again, 901)?, F01_ALREADY_CLOSING);
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
    assert_eq!(
        epoch::preview_open(&world.bytes, OPEN_AT + 128, &mut next, &mut scratch).err(),
        Some(F01_LIFECYCLE_CLOSED)
    );
    let mut body = policy_bytes(2)?;
    body.extend_from_slice(&10u64.to_be_bytes());
    let staged = world.owner_call(dispatch::STAGE_POLICY, &body)?;
    let closing = world.bytes.clone();
    assert_eq!(world.route(&staged, 902)?.error, Some(F01_LIFECYCLE_CLOSED));
    assert_eq!(world.bytes, closing);
    Ok(())
}

#[test]
fn unfunded_market_closes_through_every_phase() -> TestResult {
    let mut world = World::create()?;
    world.request_close(200)?;
    world.advance(201, step(2, [0; 32]))?;
    world.advance(202, step(3, [0; 32]))?;
    world.advance(203, step(4, [0; 32]))?;
    world.advance(204, step(5, [0; 32]))?;
    world.advance(205, closed())?;
    assert_eq!(world.header()?.lifecycle, CLOSED);
    let late = world.advance_call()?;
    assert_eq!(world.refused(&late, 206)?, F01_LIFECYCLE_CLOSED);
    Ok(())
}

#[test]
fn a07_close_refunds_free_once_and_waits_for_live_claims() -> TestResult {
    let (mut world, worker, sealed) = terminal()?;
    world.request_close(1010)?;
    world.advance(1011, step(2, [0; 32]))?;
    world.advance(1012, step(3, sealed.bytes()))?;
    let refund_call = world.advance_call()?;
    world.advance(1013, refund(FUNDING - BUDGET)?)?;
    let waiting = world.advance_call()?;
    assert_eq!(world.refused(&waiting, 1014)?, F01_OBLIGATIONS_OUTSTANDING);
    let replayed = world.journal_call(3, 2)?;
    assert_eq!(world.refused(&replayed, 1014)?, STALE_CURSOR);
    assert_eq!(world.refused(&refund_call, 1014)?, F01_STALE_REVISION);
    let recipient = AccountId::new(REFUND)?;
    let mut scratch = vec![0; REWARD_STATE_BYTES];
    let rewards = world.reward_state()?;
    let again = RefundRequest {
        expected_refunded: 0,
        amount: FUNDING - BUDGET,
        recipient,
    };
    let repeated = rewards
        .refund_free(
            FundingPhase::Closing,
            &again,
            refund_call.digest()?,
            ResultDigest::new([9; 32])?,
            &mut scratch,
        )
        .map(|(_, effect)| effect)?;
    assert!(matches!(repeated, RewardEffect::RepeatedRefund(_)));
    let reserved = RefundRequest {
        expected_refunded: FUNDING - BUDGET,
        amount: BUDGET,
        recipient,
    };
    let refusal = rewards
        .refund_free(
            FundingPhase::Closing,
            &reserved,
            waiting.digest()?,
            ResultDigest::new([9; 32])?,
            &mut scratch,
        )
        .err();
    assert_eq!(refusal, Some(INSUFFICIENT_FREE));
    let wrong = ClaimRequest {
        recipient,
        ..worker_claim(&worker)
    };
    assert_eq!(
        world.claim(&wrong, 1015).err(),
        Some(F06_WRONG_CLAIM_RECIPIENT)
    );
    assert_eq!(
        world.claim(&worker_claim(&worker), 1015)?,
        RewardEffect::Payout {
            recipient: worker.recipient,
            amount: BUDGET
        }
    );
    let claimed = world.bytes.clone();
    assert!(matches!(
        world.claim(&worker_claim(&worker), 1016)?,
        RewardEffect::AlreadyApplied(_)
    ));
    assert_eq!(world.bytes, claimed);
    world.advance(1017, step(5, [0; 32]))?;
    world.advance(1018, closed())?;
    assert_eq!(
        world.ledger()?,
        [FUNDING, BUDGET, FUNDING - BUDGET, 0, 0, 0]
    );
    let late = world.advance_call()?;
    assert_eq!(world.refused(&late, 1019)?, F01_LIFECYCLE_CLOSED);
    let reopen = world.owner_call(dispatch::REQUEST_CLOSE, &REASON)?;
    assert_eq!(world.refused(&reopen, 1019)?, F01_LIFECYCLE_CLOSED);
    Ok(())
}

#[test]
fn a07_expired_entitlements_return_to_the_fixed_recipient() -> TestResult {
    let (mut world, worker, sealed) = terminal()?;
    world.request_close(1010)?;
    world.advance(1011, step(2, [0; 32]))?;
    world.advance(1012, step(3, sealed.bytes()))?;
    world.advance(1013, refund(FUNDING - BUDGET)?)?;
    let waiting = world.advance_call()?;
    assert_eq!(
        world.refused(&waiting, EXPIRY - 1)?,
        F01_OBLIGATIONS_OUTSTANDING
    );
    let mut scratch = vec![0; REWARD_STATE_BYTES];
    assert_eq!(
        world
            .reward_state()?
            .expire_epoch_claims(6, EXPIRY - 1, &mut scratch)
            .err(),
        Some(WRONG_PHASE)
    );
    world.advance(EXPIRY, released(BUDGET))?;
    assert_eq!(
        world.ledger()?,
        [FUNDING, 0, FUNDING - BUDGET, BUDGET, 0, 0]
    );
    let misdirected = RefundRequest {
        expected_refunded: FUNDING - BUDGET,
        amount: BUDGET,
        recipient: worker.recipient,
    };
    assert_eq!(
        world
            .reward_state()?
            .refund_free(
                FundingPhase::Closing,
                &misdirected,
                waiting.digest()?,
                ResultDigest::new([9; 32])?,
                &mut scratch,
            )
            .err(),
        Some(F06_REFUND_RECIPIENT_MISMATCH)
    );
    world.advance(EXPIRY + 1, refund(BUDGET)?)?;
    world.advance(EXPIRY + 2, step(5, [0; 32]))?;
    world.advance(EXPIRY + 3, closed())?;
    assert_eq!(world.ledger()?, [FUNDING, 0, FUNDING, 0, 0, 0]);
    assert_eq!(
        world.claim(&worker_claim(&worker), EXPIRY + 4).err(),
        Some(F06_CLAIM_EXPIRED)
    );
    Ok(())
}

#[test]
fn a08_close_waits_for_the_terminal_epoch_and_sealed_tasks_and_resumes_from_bytes() -> TestResult {
    let (mut world, worker, frozen) = opened()?;
    let market = world.header()?.market_id;
    world.task(&admit(&frozen, &worker, 0x21, 1)?, 900)?;
    let first = derive_task(market, frozen.epoch, principal(0x21)?, [1; 32])?;
    let worker_step = |world: &World, operation, sequence, digest: [u8; 32]| {
        let mut payload = world.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(first.as_bytes());
        payload.extend_from_slice(&digest);
        Ok::<_, ApplicationError>(Req {
            sequence,
            request: tag(0x77, LOW, sequence),
            ..bound(&frozen, operation, principal(LOW)?, payload)
        })
    };
    world.task(&worker_step(&world, dispatch::ACCEPT_TASK, 1, ACK)?, 901)?;
    world.task(&admit(&frozen, &worker, 0x22, 2)?, 902)?;
    let second = derive_task(market, frozen.epoch, principal(0x22)?, [2; 32])?;
    world.request_close(905)?;
    let waiting = world.advance_call()?;
    assert_eq!(world.refused(&waiting, 906)?, F01_OBLIGATIONS_OUTSTANDING);
    let cancel = bound(
        &frozen,
        dispatch::CANCEL_TASK,
        principal(0x22)?,
        second.bytes().to_vec(),
    );
    world.task(&cancel, 907)?;
    let waiting = world.advance_call()?;
    assert_eq!(world.refused(&waiting, 908)?, F01_OBLIGATIONS_OUTSTANDING);
    world.task(
        &worker_step(&world, dispatch::COMMIT_TASK_RESULT, 2, RESULT)?,
        909,
    )?;
    world.terminalize(&frozen, &[worker], TERMINAL_AT)?;
    world.advance(TERMINAL_AT + 1, step(2, [0; 32]))?;
    let waiting = world.advance_call()?;
    assert_eq!(
        world.refused(&waiting, TERMINAL_AT + 2)?,
        F01_OBLIGATIONS_OUTSTANDING
    );
    let records = |world: &World| -> CodecResult<Vec<_>> {
        let region = world.section()?.task_region.to_vec();
        TaskSet::decode(&region)?.bindings().collect()
    };
    let tasks = records(&world)?;
    assert_eq!(tasks.len(), 2);
    let sealed = world.seal(&frozen, TERMINAL_AT + 2)?;
    world.advance(TERMINAL_AT + 3, step(3, sealed.bytes()))?;
    assert_eq!(records(&world)?, tasks);
    let mut restarted = World {
        bytes: world.bytes.clone(),
        owner_sequence: world.owner_sequence,
    };
    let header = restarted.header()?;
    assert_eq!(
        (header.lifecycle, header.close_phase, header.close_cursor),
        (WINDING_DOWN, 3, 2)
    );
    let old = restarted.journal_call(2, 1)?;
    assert_eq!(restarted.refused(&old, TERMINAL_AT + 4)?, STALE_CURSOR);
    restarted.advance(TERMINAL_AT + 4, refund(FUNDING - BUDGET)?)?;
    restarted.claim(&worker_claim(&worker), TERMINAL_AT + 5)?;
    restarted.advance(TERMINAL_AT + 6, step(5, [0; 32]))?;
    restarted.advance(TERMINAL_AT + 7, closed())?;
    let (frame, _) = route(
        &create_call(PROGRAM)?,
        Some(&restarted.bytes),
        TERMINAL_AT + 8,
    )?;
    assert_eq!(frame.error, Some(F01_ALREADY_CREATED));
    let (frame, next) = route(&create_call(OTHER_PROGRAM)?, None, TERMINAL_AT + 8)?;
    assert_eq!((frame.status, frame.revision), (ResultStatus::Ok, 1));
    let fresh = World {
        bytes: next.ok_or(NOT_FOUND)?,
        owner_sequence: 2,
    };
    assert_ne!(fresh.header()?.market_id, market);
    assert_eq!(fresh.header()?.lifecycle, REGISTERED);
    Ok(())
}

#[test]
fn a11_exact_retries_return_retained_results_without_new_effects() -> TestResult {
    let create = create_call(PROGRAM)?;
    let (first, next) = route(&create, None, ORIGIN)?;
    let mut world = World {
        bytes: next.ok_or(NOT_FOUND)?,
        owner_sequence: 2,
    };
    let created = world.bytes.clone();
    let retry = world.route(&create, ORIGIN + 1)?;
    assert_eq!(
        (retry.status, retry.revision, retry.payload),
        (
            ResultStatus::AlreadyApplied,
            1,
            codec::result_digest(&first.payload)?.as_bytes().to_vec()
        )
    );
    assert_eq!(world.bytes, created);
    world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    let schedule = world.owner_call(dispatch::SCHEDULE_ACTIVATION, &6u64.to_be_bytes())?;
    let first = world.route(&schedule, 134)?;
    assert_eq!(first.status, ResultStatus::Ok);
    world.owner_sequence += 1;
    let scheduled = world.bytes.clone();
    let retry = world.route(&schedule, 135)?;
    assert_eq!(
        (retry.status, retry.revision, retry.payload),
        (
            ResultStatus::AlreadyApplied,
            first.revision,
            codec::result_digest(&first.payload)?.as_bytes().to_vec()
        )
    );
    assert_eq!(world.bytes, scheduled);
    world.fund(FUNDING, 135)?;
    assert_eq!(world.open(OPEN_AT)?.epoch, 6);
    let activate = world.activate_call()?;
    world.route(&activate, OPEN_AT + 1)?;
    let mut restarted = World {
        bytes: world.bytes.clone(),
        owner_sequence: world.owner_sequence,
    };
    let activated = restarted.bytes.clone();
    let retry = restarted.route(&activate, OPEN_AT + 2)?;
    assert_eq!(retry.error, Some(F01_ALREADY_ACTIVATED));
    assert_eq!(restarted.bytes, activated);
    let header = restarted.header()?;
    assert_eq!((header.lifecycle, header.origin_height), (ACTIVE, ORIGIN));
    let close = restarted.request_close(OPEN_AT + 3)?;
    let closing = restarted.bytes.clone();
    let revision = restarted.revision()?;
    let Close::Retained(retained) = restarted.close(&close, OPEN_AT + 4)?.0 else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            retained.applied_revision,
            retained.request_digest,
            retained.result_digest
        ),
        (
            revision,
            close.digest()?,
            codec::result_digest(&restarted.receipt(&close, revision)?)?
        )
    );
    assert_eq!(restarted.bytes, closing);
    let fresh = restarted.owner_call(dispatch::REQUEST_CLOSE, &REASON)?;
    assert_eq!(restarted.refused(&fresh, OPEN_AT + 5)?, F01_ALREADY_CLOSING);
    Ok(())
}
