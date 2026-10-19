//! Evaluator service epochs over real guest state: the market is produced by the real F01,
//! F02, F03, F06 and F08 producers, `OPEN_EPOCH` and `SealEvidence`; the service reads the
//! committed state through real `READ_STATE_CHUNK` program reads and every call it emits goes
//! through `dispatch::route`.
#[path = "../src/main.rs"]
mod evaluator;

use std::fmt;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::sync::atomic::{AtomicUsize, Ordering};

use ed25519_dalek::{Signer, SigningKey};
use evaluator::{
    capture_state, outcome_of, AssessmentKey, Call, CallOutcome, EpochView, Evaluator, Identity,
    Metrics,
};
use layerx_platform_internal::secret::hex;
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{self as agg, QUORUM},
    aggregation_codec::{decode_current, EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope, Envelope,
    },
    commit_reveal::{self as cr, Status},
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{ApplicationError, CodecResult, KEY_MISMATCH, NON_CANONICAL, NOT_FOUND},
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    evidence::{self, Outcome as Sealing, SEAL_SCRATCH_BYTES},
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::ReadProof,
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardEpoch,
        RewardLedger, RewardOutcome, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorId, FrozenBinding,
        MarketId, MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest,
        RequestId, ResultDigest, RosterDigest, RubricDigest, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use serde_json::json;

/// Any failure of a test step, shown by its message.
struct Failure(String);
impl fmt::Debug for Failure {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self(format!("application error 0x{:04x}", error.code()))
    }
}
impl From<String> for Failure {
    fn from(error: String) -> Self {
        Self(error)
    }
}
impl From<std::io::Error> for Failure {
    fn from(error: std::io::Error) -> Self {
        Self(error.to_string())
    }
}
type TestResult = Result<(), Failure>;

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
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120).
const EPOCH: u64 = 7;
const COMMIT_AT: u64 = 1088;
const REVEAL_AT: u64 = 1104;
const REVEAL_END: u64 = 1120;
/// The lowest frozen worker.
const LOW: u8 = 0x20;
const COMMIT_SELECTOR: u16 = 0x0401;
const REVEAL_SELECTOR: u16 = 0x0402;

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
/// The frozen assessment key seed of evaluator `n`.
const fn evaluator_seed(n: u8) -> [u8; 32] {
    [n; 32]
}
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&evaluator_seed(n))
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

/// One native request envelope.
#[derive(Clone)]
struct Req {
    operation: Operation,
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
        market: Some(frozen.market),
        epoch: frozen.epoch,
        config: frozen.config.get(),
        roster: Presence::Present(frozen.roster),
        ..req(operation, actor, payload)
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
    fn context(&self, at: u64) -> CodecResult<CallContext> {
        context(self.actor, at)
    }
}
fn context(principal: PrincipalId, at: u64) -> CodecResult<CallContext> {
    Ok(CallContext {
        chain: ChainDomain::new(CHAIN)?,
        program: ProgramId::new(PROGRAM)?,
        principal,
        height: at,
    })
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
    fn insert_grant(&mut self, grant: EvaluatorGrant, n: u8) -> CodecResult<()> {
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
    fn owner_op(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> CodecResult<()> {
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
    fn schedule(&mut self, epoch: u64, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
    }
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
    /// F03 nomination accepted through the signed F08 evaluator consent of its key; the
    /// evaluator replay slot `n - 2` is bound to its owner.
    fn evaluator(&mut self, n: u8, at: u64) -> CodecResult<()> {
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
    fn fund(&mut self, amount: u128, at: u64) -> CodecResult<()> {
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
    /// Real F06 `TerminalizeRewards` of `frozen`, weight 5 for every frozen worker.
    fn terminalize(
        &mut self,
        frozen: &Frozen,
        roster: &[WorkerRosterEntry],
        at: u64,
    ) -> CodecResult<()> {
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
    fn advance(&mut self, at: u64) -> CodecResult<()> {
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
    fn task(&mut self, call: &Req, at: u64) -> CodecResult<()> {
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
    fn evidence(&mut self, call: &Req, at: u64) -> CodecResult<()> {
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

/// A market with an opened epoch; it is also the chain the service reads and calls.
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
    workers: Vec<WorkerRosterEntry>,
}
/// Epoch 7 (T = 1024): worker `LOW` and evaluators 2..=4 enrolled before activation, epoch 6
/// opened at 896, worker `LOW + 1` and evaluators 5..=7 enrolled in epoch 6, epoch 6
/// terminalized, epoch 7 opened at 1024, its empty task set sealed and the evidence of every
/// evaluator sealed at 1088 under evaluator sequence 1.
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
        (EPOCH, 2, 6)
    );
    m.seal(&[2, 3, 4, 5, 6, 7], COMMIT_AT)?;
    Ok(m)
}

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
    fn slot(&self, n: u8, at: u64) -> CodecResult<cr::Slot> {
        cr::slot(&self.world.bytes, self.evaluator(n)?, at)
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's task region, then `SealEvidence` of each
    /// of `evaluators` under evaluator sequence 1.
    fn seal(&mut self, evaluators: &[u8], at: u64) -> CodecResult<()> {
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
    /// One routed call from `principal` at `at` over the committed bytes; `next` is
    /// committed only on `Applied`. Answers the routing and the result frame.
    fn route(
        &mut self,
        envelope: &[u8],
        principal: PrincipalId,
        at: u64,
    ) -> CodecResult<(Routed, Vec<u8>)> {
        let (routed, next, result) = self.compose(envelope, principal, at)?;
        if let Routed::Applied { state_len, .. } = routed {
            self.world.bytes = next[..state_len].to_vec();
        }
        Ok((routed, result))
    }
    /// One routed call that commits nothing: the program read path.
    fn compose(
        &self,
        envelope: &[u8],
        principal: PrincipalId,
        at: u64,
    ) -> CodecResult<(Routed, Vec<u8>, Vec<u8>)> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut result = vec![0; MAX_RESULT_BYTES];
        let routed = dispatch::route(
            &context(principal, at)?,
            envelope,
            Some(&self.world.bytes),
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?;
        let result_len = match routed {
            Routed::Applied { result_len, .. }
            | Routed::Unchanged { result_len }
            | Routed::Refused { result_len } => result_len,
        };
        result.truncate(result_len);
        Ok((routed, next, result))
    }
    /// A permissionless relayer aggregation request bound to the opened epoch, applied.
    fn aggregate(&mut self, operation: Operation, payload: Vec<u8>, cursor: u16) -> TestResult {
        let [_, selector] = operation.selector().to_be_bytes();
        let call = Req {
            request: tag(0xf5, selector, u64::from(cursor)),
            ..bound(
                operation,
                principal(RELAYER)?,
                &self.frozen_binding(),
                payload,
            )
        };
        let (routed, _) = self.route(&call.encode()?, principal(RELAYER)?, REVEAL_END + 1)?;
        assert!(
            matches!(routed, Routed::Applied { operation: applied, .. } if applied == operation)
        );
        Ok(())
    }
    /// Begin, every Process chunk and Finalize of the opened epoch.
    fn settle(&mut self) -> TestResult {
        self.aggregate(dispatch::BeginAggregation, Vec::new(), 0)?;
        let mut progress = agg::progress(&self.world.bytes)?;
        let Presence::Present(input) = progress.input else {
            return Err(NOT_FOUND.into());
        };
        while progress.cursor < progress.worker_count {
            let mut payload = input.as_bytes().to_vec();
            payload.extend_from_slice(&progress.cursor.to_be_bytes());
            self.aggregate(dispatch::ProcessAggregation, payload, progress.cursor)?;
            progress = agg::progress(&self.world.bytes)?;
        }
        self.aggregate(
            dispatch::FinalizeAggregation,
            input.as_bytes().to_vec(),
            u16::MAX,
        )
    }
    /// The persisted F05 outputs, decoded against the frozen roster.
    fn outputs(&self) -> CodecResult<Vec<WorkerAggregate>> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        let tail = section.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
        let (record, _) = tail.split_at(record_len(tail)?);
        let current = decode_current(record, self.frozen_binding(), &self.workers)?;
        (0..current.output_count())
            .map(|i| current.output(i))
            .collect()
    }
    fn reward_row(&self) -> CodecResult<RewardEpoch> {
        let state = decode_shared_state(&self.world.bytes)?;
        let section = state.feature_sections[Section::SettlementClaims.index()];
        decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?
            .row(self.frozen.epoch)
    }
}

/// Length of the F05 current record at the start of the bytes after the reward state.
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
fn fields(output: WorkerAggregate) -> (u8, QualityStatus, u32, u32) {
    (
        output.support(),
        output.status(),
        output.score().get(),
        output.weight(),
    )
}

/// A private directory removed when the test ends.
struct Scratch(PathBuf);
impl Scratch {
    fn new(name: &str) -> Result<Self, Failure> {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let path = std::env::temp_dir().join(format!(
            "paxai-evaluator-{name}-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir_all(&path)?;
        Ok(Self(path))
    }
    fn path(&self) -> &Path {
        &self.0
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

/// The service of evaluator `n` holding assessment key seed `seed`: its key file, its
/// epoch-7 assessment (one score per frozen worker) and its journal live under `dir`.
struct Service {
    identity: Identity,
    dir: Scratch,
    evaluator: Evaluator,
}
impl Service {
    fn start(m: &Market, n: u8, seed: [u8; 32]) -> Result<Self, Failure> {
        let dir = Scratch::new(&format!("e{n}"))?;
        fs::write(dir.path().join("key"), hex(&seed))?;
        let assessments = dir.path().join("assessments");
        fs::create_dir_all(&assessments)?;
        let assessment = json!({
            "evidence_root": hex(&[root(n); 32]),
            "scores": m.workers.iter().rev().enumerate().map(|(i, worker)| json!({
                "worker": hex(worker.worker.as_bytes()),
                "score": 400_000 + 100_000 * u32::from(n) + u32::try_from(i).unwrap_or(0),
            })).collect::<Vec<_>>(),
        });
        fs::write(
            assessments.join(format!("{EPOCH}.json")),
            assessment.to_string(),
        )?;
        let identity = Identity {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            evaluator: m.evaluator(n)?,
            owner: principal(n)?,
            reader: principal(RELAYER)?,
        };
        let evaluator = Self::open(identity, dir.path())?;
        Ok(Self {
            identity,
            dir,
            evaluator,
        })
    }
    fn open(identity: Identity, dir: &Path) -> Result<Evaluator, Failure> {
        Ok(Evaluator::open(
            identity,
            AssessmentKey::load(&dir.join("key"))?,
            &dir.join("state"),
            &dir.join("assessments"),
        )?)
    }
    /// Stops the service (its journal lock is released) and starts it again from disk.
    fn restart(self) -> Result<Self, Failure> {
        let Self {
            identity,
            dir,
            evaluator,
        } = self;
        drop(evaluator);
        let evaluator = Self::open(identity, dir.path())?;
        Ok(Self {
            identity,
            dir,
            evaluator,
        })
    }
    /// The epoch as the service sees it at `at`: a complete chunked program read of the
    /// committed state.
    fn view(&self, m: &Market, at: u64) -> Result<EpochView, Failure> {
        let proof = ReadProof {
            chain: self.identity.chain,
            program: self.identity.program,
            native_state_root: Digest32::new([0x31; 32])?,
            observed_sequence: at - 1,
            execution_height: at - 1,
            batch_id: Digest32::new([0x32; 32])?,
        };
        let state = capture_state(&self.identity, &proof, |envelope| {
            m.compose(envelope, self.identity.reader, at)
                .map(|(_, _, result)| result)
                .map_err(|error| format!("read routing 0x{:04x}", error.code()))
        })?;
        assert_eq!(state, m.world.bytes);
        Ok(EpochView::read(&state, &self.identity, at)?)
    }
    /// One service poll at `at`: read, step, route the emitted call, settle its outcome.
    fn tick(
        &mut self,
        m: &mut Market,
        at: u64,
    ) -> Result<Option<(Call, Routed, CallOutcome)>, Failure> {
        let view = self.view(m, at)?;
        let Some(call) = self.evaluator.step(&view)? else {
            return Ok(None);
        };
        let (routed, result) = m.route(&call.envelope, principal(RELAYER)?, at)?;
        let outcome = outcome_of(&result)?;
        self.evaluator.settle(&call, outcome)?;
        Ok(Some((call, routed, outcome)))
    }
}

fn applied(sent: Option<(Call, Routed, CallOutcome)>, operation: Operation) -> TestResult {
    let Some((call, routed, outcome)) = sent else {
        return Err(Failure("no call was sent".to_owned()));
    };
    assert_eq!((call.operation, call.epoch), (operation, EPOCH));
    assert!(matches!(routed, Routed::Applied { operation: landed, .. } if landed == operation));
    assert_eq!(outcome, CallOutcome::Applied);
    Ok(())
}

#[test]
fn commit_then_reveal() -> TestResult {
    let _: fn() -> ExitCode = evaluator::main;
    let mut m = market()?;
    let mut service = Service::start(&m, 2, evaluator_seed(2))?;
    assert!(service.tick(&mut m, COMMIT_AT - 1)?.is_none());
    assert_eq!(service.evaluator.journal().epoch(EPOCH), None);

    applied(service.tick(&mut m, COMMIT_AT)?, dispatch::CommitScore)?;
    let slot = m.slot(2, COMMIT_AT)?;
    let record = slot.commit.ok_or(NOT_FOUND)?;
    let secret = service
        .evaluator
        .journal()
        .epoch(EPOCH)
        .and_then(|epoch| epoch.secret.clone())
        .ok_or(NOT_FOUND)?;
    assert_eq!(slot.status, Status::Committed);
    assert_eq!((record.sequence, record.height), (2, COMMIT_AT));
    assert_eq!(hex(record.commitment.as_bytes()), secret.commitment);
    assert_eq!(secret.epoch, EPOCH);

    assert!(service.tick(&mut m, COMMIT_AT + 1)?.is_none());
    assert!(service.tick(&mut m, REVEAL_AT - 1)?.is_none());

    let mut service = service.restart()?;
    let record = service.evaluator.journal().epoch(EPOCH).ok_or(NOT_FOUND)?;
    assert_eq!(record.secret.as_ref(), Some(&secret));
    assert_eq!(record.applied, vec![COMMIT_SELECTOR]);

    applied(service.tick(&mut m, REVEAL_AT)?, dispatch::RevealScore)?;
    assert_eq!(m.slot(2, REVEAL_AT)?.status, Status::Revealed);
    assert!(service.tick(&mut m, REVEAL_AT + 1)?.is_none());
    assert!(service.tick(&mut m, REVEAL_END)?.is_none());

    let record = service.evaluator.journal().epoch(EPOCH).ok_or(NOT_FOUND)?;
    assert_eq!(record.applied, vec![COMMIT_SELECTOR, REVEAL_SELECTOR]);
    assert_eq!((record.refused, record.forfeited), (None, false));
    assert_eq!(service.evaluator.journal().totals(), (2, 0, 0));
    let metrics = Metrics::observe(
        &service.view(&m, REVEAL_END - 1)?,
        service.evaluator.journal(),
        1,
    );
    assert_eq!(
        metrics,
        Metrics {
            epoch: EPOCH,
            active: 6,
            revealed: 1,
            unrevealed_near_end: 0,
            applied: 2,
            refused: 0,
            forfeited: 0,
        }
    );
    Ok(())
}

#[test]
fn below_quorum_forfeits_and_settles_without_a_score() -> TestResult {
    let mut m = market()?;
    let mut services = [
        Service::start(&m, 2, evaluator_seed(2))?,
        Service::start(&m, 3, evaluator_seed(3))?,
        Service::start(&m, 4, evaluator_seed(4))?,
    ];
    for service in &mut services {
        applied(service.tick(&mut m, COMMIT_AT)?, dispatch::CommitScore)?;
    }
    let [two, three, four] = &mut services;
    applied(two.tick(&mut m, REVEAL_AT)?, dispatch::RevealScore)?;
    applied(three.tick(&mut m, REVEAL_AT + 1)?, dispatch::RevealScore)?;

    let late = four.view(&m, REVEAL_END - 1)?;
    assert_eq!(late.slot.status, Status::Committed);
    assert_eq!((late.active, late.revealed, late.unrevealed), (6, 2, 1));
    assert!(late.near_end(1));
    let metrics = Metrics::observe(&late, four.evaluator.journal(), 1);
    assert_eq!(metrics.unrevealed_near_end, 1);
    let text = metrics.render();
    for line in [
        "paxai_evaluator_epoch 7\n",
        "paxai_evaluator_active 6\n",
        &format!("paxai_evaluator_quorum {QUORUM}\n"),
        "paxai_evaluator_below_quorum 0\n",
        "paxai_evaluator_revealed 2\n",
        "paxai_evaluator_unrevealed_near_end 1\n",
        "paxai_evaluator_applied_total 1\n",
        "paxai_evaluator_forfeited_total 0\n",
    ] {
        assert!(text.contains(line), "{line:?} missing from {text:?}");
    }

    assert_eq!(m.slot(4, REVEAL_END)?.status, Status::Expired);
    assert!(four.tick(&mut m, REVEAL_END)?.is_none());
    let record = four
        .evaluator
        .journal()
        .epoch(EPOCH)
        .cloned()
        .ok_or(NOT_FOUND)?;
    assert!(record.forfeited);
    assert_eq!(record.applied, vec![COMMIT_SELECTOR]);
    assert_eq!(four.evaluator.journal().totals(), (1, 0, 1));
    assert!(four.tick(&mut m, REVEAL_END + 1)?.is_none());
    assert_eq!(four.evaluator.journal().epoch(EPOCH), Some(&record));
    assert_eq!(four.evaluator.journal().totals(), (1, 0, 1));

    m.settle()?;
    let outputs = m.outputs()?;
    assert_eq!(outputs.len(), 2);
    assert!(outputs
        .iter()
        .all(|output| fields(*output) == (2, QualityStatus::InsufficientQuorum, 0, 0)));
    assert_eq!(m.reward_row()?.outcome, RewardOutcome::NoEligibleScore);
    Ok(())
}

#[test]
fn ungranted_key_is_refused_and_never_retried() -> TestResult {
    let mut m = market()?;
    let mut service = Service::start(&m, 2, [0x77; 32])?;
    let before = m.world.bytes.clone();
    let Some((call, routed, outcome)) = service.tick(&mut m, COMMIT_AT)? else {
        return Err(Failure("no call was sent".to_owned()));
    };
    assert_eq!(call.operation, dispatch::CommitScore);
    assert!(matches!(routed, Routed::Refused { .. }));
    assert_eq!(outcome, CallOutcome::Refused(Some(KEY_MISMATCH)));
    assert_eq!(m.world.bytes, before);
    let record = service
        .evaluator
        .journal()
        .epoch(EPOCH)
        .cloned()
        .ok_or(NOT_FOUND)?;
    assert_eq!(record.refused, Some((COMMIT_SELECTOR, KEY_MISMATCH.code())));
    assert!(record.applied.is_empty());
    assert_eq!(service.evaluator.journal().totals(), (0, 1, 0));
    for at in [COMMIT_AT + 1, REVEAL_AT, REVEAL_END] {
        assert!(service.tick(&mut m, at)?.is_none());
    }
    assert_eq!(service.evaluator.journal().epoch(EPOCH), Some(&record));
    assert_eq!(m.world.bytes, before);
    assert_eq!(m.slot(2, REVEAL_END)?.status, Status::Absent);

    let outsider = Service::start(&m, 9, evaluator_seed(9))?;
    let view = outsider.view(&m, COMMIT_AT)?;
    assert_eq!((view.entry, view.sequence, view.active), (None, None, 6));
    let mut outsider = outsider;
    for at in [COMMIT_AT, REVEAL_AT, REVEAL_END] {
        assert!(outsider.tick(&mut m, at)?.is_none());
    }
    assert_eq!(outsider.evaluator.journal().epoch(EPOCH), None);
    assert_eq!(m.world.bytes, before);
    Ok(())
}
