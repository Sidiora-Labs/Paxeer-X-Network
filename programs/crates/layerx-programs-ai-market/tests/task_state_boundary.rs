//! AI.F01-T04 task-set lifecycle over the complete shared state value: `ADMIT_TASK`,
//! `ACCEPT_TASK`, `CANCEL_TASK`, `COMMIT_TASK_RESULT` and `SEAL_TASK_SET` against an epoch
//! opened through the real F01/F02/F03/F06/F08 producers and `OPEN_EPOCH`.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        domain_hash, encode_envelope, Envelope,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        CodecResult, ARITHMETIC, BAD_SIGNATURE, CONFLICT, F01_INVALID_POLICY, F01_NO_TASK_CAPACITY,
        F01_POLICY_MISMATCH, F01_PRINCIPAL_MISMATCH, F01_STALE_REVISION, F01_TASK_ALREADY_ACCEPTED,
        F01_TASK_CONFLICT, F01_TASK_EXPIRED, F01_TASK_NOT_FOUND, F01_UNKNOWN_WORKER,
        F01_WRONG_LIFECYCLE, F01_WRONG_WORKER, F02_DELEGATE_REVOKED,
        F09_EVIDENCE_TASK_SET_UNSEALED, KEY_MISMATCH, NON_CANONICAL, SEQUENCE_GAP, UNAUTHORIZED,
        WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection, EMPTY_TASK_REGION},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{
        self, Outcome as Task, SetBinding, TaskBinding, TaskSet, TaskStatus, ADMIT_PAYLOAD_BYTES,
        TASK_BINDING_BYTES, TASK_REGION_MAX_BYTES,
    },
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, FrozenBinding, MarketId,
        MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId,
        ResultDigest, RosterDigest, RubricDigest, Signature64, TaskId, Version, WorkerId,
        WorkerRosterEntry,
    },
    workers::{self, WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};
use sha2::{Digest as _, Sha256};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 1000;
const METADATA: [u8; 32] = [0x44; 32];
const ADMIT_REQUEST: [u8; 32] = [0xb0; 32];
const ACK: [u8; 32] = [0xa5; 32];
const RESULT: [u8; 32] = [0xe5; 32];
const ROLE_EXPIRY: u64 = 5000;

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
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// One request envelope; `delegate` is (claimed key, signer).
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
    delegate: Option<(SigningKey, SigningKey)>,
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
        delegate: None,
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
            payload: &self.payload,
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

/// One market's committed shared state bytes, its next owner sequence and the event suffix
/// of the last committed task transition.
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
    suffix: Vec<u8>,
}
impl World {
    fn create(origin: u64) -> CodecResult<Self> {
        Ok(Self {
            bytes: create(origin)?,
            owner_sequence: 2,
            suffix: Vec::new(),
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
            request: [0x20 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?; 32],
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
        let next = encode(&state)?;
        self.bytes = next;
        self.owner_sequence += 1;
        Ok(())
    }
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
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
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let key = SigningKey::from_bytes(&[n; 32]);
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
    /// Real F02 `RevokeDelegate` by worker `n`'s owner under its first role sequence. F02
    /// advances only the shared revision, so the F01 header revision is re-bound to it.
    fn revoke(&mut self, n: u8, worker: WorkerId, at: u64) -> TestResult {
        let mut payload = worker.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.push(2);
        payload.extend_from_slice(&1u64.to_be_bytes());
        let owner = principal(n)?;
        let call = Req {
            sequence: 1,
            request: [0xd0; 32],
            expiry: at + 1000,
            ..req(dispatch::RevokeDelegate, owner, payload)
        };
        let market = self.parts()?.market()?;
        let mut out = vec![0; MAX_STATE_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut control = vec![0; workers::CONTROL_SCRATCH_BYTES];
        let workers::Applied::Applied { state_len, .. } = workers::apply(
            &self.bytes,
            &workers::CallContext {
                market: &market,
                invoking_principal: owner,
                height: at,
            },
            &call.encode()?,
            &mut out,
            &mut event,
            &mut control,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        out.truncate(state_len);
        self.bytes = Parts::load(&out)?.encode()?;
        Ok(())
    }
}

/// The `OPEN_EPOCH`, `ADVANCE_ACTIVATION` and task-set calls.
impl World {
    /// Permissionless object-local `OPEN_EPOCH` of the clock epoch of `at`, naming the
    /// previewed frozen config and roster.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let preview = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Req {
            epoch: preview.epoch,
            config: preview.config.get(),
            roster: Presence::Present(preview.roster),
            request: [0x60; 32],
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
            request: [0x61; 32],
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
    /// One task-set call; commits only on `Applied`, checking one revision increment, the
    /// F01 header binding and the emitted event.
    fn task(&mut self, call: &Req, at: u64) -> CodecResult<Task> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let before = self.revision()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; tasks::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = tasks::apply(
            &call.context(at)?,
            &envelope,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        if let Task::Applied {
            receipt,
            revision,
            state_len,
            event_len,
        } = outcome
        {
            next.truncate(state_len);
            self.bytes = next;
            assert_eq!(revision, before + 1);
            assert_eq!(self.revision()?, revision);
            assert_eq!(self.section()?.header.state_revision, revision);
            let topic = [
                b"PAXAI/v1/".as_slice(),
                call.operation.metadata().name.as_bytes(),
            ]
            .concat();
            let (operation, common, suffix) =
                codec::decode_event_frame(&topic, &event[..event_len])?;
            assert_eq!(
                (
                    operation,
                    common.epoch,
                    common.config.get(),
                    common.revision
                ),
                (call.operation, call.epoch, call.config, revision)
            );
            assert_eq!(
                (common.request, common.result),
                (envelope.request_digest()?, receipt.digest)
            );
            self.suffix = suffix.to_vec();
        }
        Ok(outcome)
    }
}

/// A market with an opened epoch 1 (Work is [1128, 1192)) and its frozen binding.
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
    workers: Vec<WorkerRosterEntry>,
}
/// Enrolls `workers` and three evaluators, funds, schedules activation at epoch 1, opens
/// epoch 1 at 1128 and, when `activate`, advances the lifecycle to ACTIVE at 1129.
fn opened(workers: &[u8], activate: bool) -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let mut entries = Vec::new();
    for &n in workers {
        entries.push(world.enroll(n, ORIGIN + 2)?);
    }
    for n in 2..=4 {
        world.evaluator(n, ORIGIN + 2 + u64::from(n))?;
    }
    world.schedule(1, 1006)?;
    world.fund(500, 1007)?;
    let frozen = world.open(1128)?;
    assert_eq!((frozen.epoch, frozen.config.get()), (1, 1));
    if activate {
        world.advance(1129)?;
    }
    let header = world.parts()?.market()?;
    Ok(Market {
        world,
        frozen,
        header,
        workers: entries,
    })
}

#[derive(Clone, Copy)]
struct Admit {
    epoch: u64,
    config: u64,
    policy: [u8; 32],
    roster: [u8; 32],
    requester: PrincipalId,
    worker: WorkerId,
    metadata: [u8; 32],
    nonce: [u8; 32],
    input: [u8; 32],
    deadline: u64,
}
impl Admit {
    fn payload(&self) -> Vec<u8> {
        let mut out = self.epoch.to_be_bytes().to_vec();
        out.extend_from_slice(&self.config.to_be_bytes());
        out.extend_from_slice(&self.policy);
        out.extend_from_slice(&self.roster);
        out.extend_from_slice(self.requester.as_bytes());
        out.extend_from_slice(self.worker.as_bytes());
        out.extend_from_slice(&self.metadata);
        out.extend_from_slice(&self.nonce);
        out.extend_from_slice(&self.input);
        out.extend_from_slice(&self.deadline.to_be_bytes());
        out
    }
}

/// A worker step: `expected_revision || task || digest`.
struct Step {
    operation: Operation,
    worker: u8,
    sequence: u64,
    expected: u64,
    task: TaskId,
    digest: [u8; 32],
}

fn role_request(operation: Operation, worker: u8, sequence: u64) -> [u8; 32] {
    let mut request = [0x77; 32];
    request[..2].copy_from_slice(&operation.selector().to_be_bytes());
    request[2] = worker;
    request[3..11].copy_from_slice(&sequence.to_be_bytes());
    request
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
    fn admission(
        &self,
        requester: u8,
        worker: usize,
        nonce: u8,
        deadline: u64,
    ) -> CodecResult<Admit> {
        Ok(Admit {
            epoch: self.frozen.epoch,
            config: self.frozen.config.get(),
            policy: self.frozen.policy.bytes(),
            roster: self.frozen.roster.bytes(),
            requester: principal(requester)?,
            worker: self.workers.get(worker).ok_or(NON_CANONICAL)?.worker,
            metadata: METADATA,
            nonce: [nonce; 32],
            input: [0xa0 + nonce; 32],
            deadline,
        })
    }
    fn task_id(&self, requester: u8, nonce: u8) -> CodecResult<TaskId> {
        derive_task(
            self.header.market_id,
            self.frozen.epoch,
            principal(requester)?,
            [nonce; 32],
        )
    }
    fn admit_with(
        &mut self,
        admit: &Admit,
        at: u64,
        edit: impl FnOnce(Req) -> Req,
    ) -> CodecResult<Task> {
        let call = Req {
            request: ADMIT_REQUEST,
            ..self.bound(dispatch::ADMIT_TASK, admit.requester, admit.payload())
        };
        self.world.task(&edit(call), at)
    }
    fn admit(&mut self, admit: &Admit, at: u64) -> CodecResult<Task> {
        self.admit_with(admit, at, |call| call)
    }
    fn cancel(&mut self, requester: u8, task: TaskId, at: u64) -> CodecResult<Task> {
        let call = self.bound(
            dispatch::CANCEL_TASK,
            principal(requester)?,
            task.bytes().to_vec(),
        );
        self.world.task(&call, at)
    }
    fn seal_with(&mut self, epoch: u64, digest: [u8; 32], at: u64) -> CodecResult<Task> {
        let mut payload = epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(&digest);
        let call = self.bound(dispatch::SEAL_TASK_SET, principal(KEEPER)?, payload);
        self.world.task(&call, at)
    }
    fn seal(&mut self, digest: Digest32, at: u64) -> CodecResult<Task> {
        self.seal_with(self.frozen.epoch, digest.bytes(), at)
    }
    fn step_with(
        &mut self,
        step: &Step,
        at: u64,
        edit: impl FnOnce(Req) -> Req,
    ) -> CodecResult<Task> {
        let mut payload = step.expected.to_be_bytes().to_vec();
        payload.extend_from_slice(step.task.as_bytes());
        payload.extend_from_slice(&step.digest);
        let call = Req {
            sequence: step.sequence,
            request: role_request(step.operation, step.worker, step.sequence),
            expiry: ROLE_EXPIRY,
            ..self.bound(step.operation, principal(step.worker)?, payload)
        };
        self.world.task(&edit(call), at)
    }
    /// A native worker step expecting the current revision.
    fn step(
        &mut self,
        operation: Operation,
        worker: u8,
        sequence: u64,
        task: TaskId,
        digest: [u8; 32],
        at: u64,
    ) -> CodecResult<Task> {
        let step = Step {
            operation,
            worker,
            sequence,
            expected: self.world.revision()?,
            task,
            digest,
        };
        self.step_with(&step, at, |call| call)
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
    /// R044 digest recomputed independently over an unsealed region.
    fn expected_digest(&self, region: &[u8]) -> CodecResult<Digest32> {
        let mut h = Sha256::new();
        h.update(b"PAXAI/task-set/v1");
        h.update([0]);
        h.update(self.header.market_id.as_bytes());
        h.update(self.frozen.epoch.to_be_bytes());
        h.update(self.frozen.config.get().to_be_bytes());
        h.update(self.frozen.policy.as_bytes());
        h.update(self.frozen.roster.as_bytes());
        h.update(&region[..2]);
        h.update(&region[3..]);
        Digest32::new(h.finalize().into())
    }
}

#[test]
fn a12_admission_binds_one_immutable_task() -> TestResult {
    let mut m = opened(&[1], true)?;
    let rewards = m.world.parts()?.rewards;
    let revision = m.world.revision()?;
    let first = m.admission(0x21, 0, 1, 1172)?;
    let Task::Applied {
        receipt,
        revision: applied,
        ..
    } = m.admit(&first, 1140)?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(applied, revision + 1);
    let task = m.task_id(0x21, 1)?;
    let record = TaskBinding {
        task,
        requester: principal(0x21)?,
        worker: m.workers[0].worker,
        input: Digest32::new([0xa1; 32])?,
        deadline: 1172,
        status: TaskStatus::Admitted,
        acknowledgement: None,
        result: None,
        admission: domain_hash("PAXAI/task-admission/v1", &first.payload())?,
    };
    assert_eq!(m.tasks()?, vec![record]);
    let mut suffix = record.encode()?.to_vec();
    suffix.extend_from_slice(&first.nonce);
    assert_eq!(m.world.suffix, suffix);
    let mut bytes = m.header.market_id.as_bytes().to_vec();
    bytes.extend_from_slice(&applied.to_be_bytes());
    bytes.extend_from_slice(&ADMIT_REQUEST);
    bytes.extend_from_slice(&0x010c_u16.to_be_bytes());
    bytes.extend_from_slice(&1u64.to_be_bytes());
    bytes.push(1);
    bytes.extend_from_slice(m.frozen.policy.as_bytes());
    assert_eq!(receipt.bytes().as_slice(), bytes.as_slice());
    assert_eq!(receipt.digest, codec::result_digest(&bytes)?);
    assert_eq!(m.world.parts()?.rewards, rewards);
    assert_eq!(
        m.admit(&first, 1141)?,
        Task::AlreadyApplied {
            subject: Digest32::new(task.bytes())?
        }
    );
    let changed = Admit {
        input: [0xa9; 32],
        ..first
    };
    assert_eq!(m.admit(&changed, 1141), Err(F01_TASK_CONFLICT));
    assert_eq!(m.world.revision()?, applied);
    let other = Admit {
        requester: principal(0x22)?,
        ..first
    };
    m.admit(&other, 1141)?;
    let second = m.task_id(0x22, 1)?;
    assert_ne!(second, task);
    let tasks = m.tasks()?;
    assert_eq!(tasks.len(), 2);
    assert_eq!(m.find(task)?, record);
    assert_eq!(
        (m.find(second)?.requester, m.find(second)?.input),
        (principal(0x22)?, record.input)
    );
    assert!(tasks[0].task < tasks[1].task);
    assert_eq!(m.world.revision()?, applied + 1);
    assert_eq!(m.world.parts()?.rewards, rewards);
    Ok(())
}

#[test]
fn a12_cancelled_slots_stay_occupied() -> TestResult {
    let mut m = opened(&[1], true)?;
    let first = m.admission(0x21, 0, 1, 1172)?;
    m.admit(&first, 1140)?;
    let task = m.task_id(0x21, 1)?;
    assert_eq!(m.cancel(0x22, task, 1141), Err(UNAUTHORIZED));
    m.cancel(0x21, task, 1141)?;
    let cancelled = m.find(task)?;
    assert_eq!(
        (
            cancelled.status,
            cancelled.acknowledgement,
            cancelled.result
        ),
        (TaskStatus::Cancelled, None, None)
    );
    assert_eq!(m.world.suffix, cancelled.encode()?.to_vec());
    assert_eq!(
        m.cancel(0x21, task, 1142)?,
        Task::AlreadyApplied {
            subject: Digest32::new(task.bytes())?
        }
    );
    assert_eq!(
        m.step(dispatch::ACCEPT_TASK, 1, 1, task, ACK, 1142),
        Err(F01_TASK_CONFLICT)
    );
    assert_eq!(
        m.admit(&first, 1142)?,
        Task::AlreadyApplied {
            subject: Digest32::new(task.bytes())?
        }
    );
    for nonce in 2..=64 {
        let admit = m.admission(0x21, 0, nonce, 1172)?;
        m.admit(&admit, 1150)?;
        if nonce % 2 == 0 {
            let cancel = m.task_id(0x21, nonce)?;
            m.cancel(0x21, cancel, 1150)?;
        }
    }
    let tasks = m.tasks()?;
    assert_eq!(tasks.len(), 64);
    assert_eq!(
        tasks
            .iter()
            .filter(|t| t.status == TaskStatus::Cancelled)
            .count(),
        33
    );
    let revision = m.world.revision()?;
    let overflow = m.admission(0x21, 0, 65, 1172)?;
    assert_eq!(m.admit(&overflow, 1151), Err(F01_NO_TASK_CAPACITY));
    let other = m.admission(0x22, 0, 1, 1172)?;
    assert_eq!(m.admit(&other, 1151), Err(F01_NO_TASK_CAPACITY));
    assert_eq!(m.world.revision()?, revision);
    let region = m.region()?;
    assert_eq!(region.len(), 3 + 64 * TASK_BINDING_BYTES);
    let digest = m.expected_digest(&region)?;
    m.seal(digest, 1192)?;
    let sealed = m.region()?;
    assert_eq!(sealed.len(), TASK_REGION_MAX_BYTES);
    assert_eq!(sealed.len(), 14_947);
    assert_eq!(sealed[..3], [0, 64, 1]);
    assert_eq!(sealed[3..35], digest.bytes());
    assert_eq!(sealed[35..], region[3..]);
    Ok(())
}

#[test]
fn a12_admission_refuses_without_a_slot() -> TestResult {
    let mut m = opened(&[1], true)?;
    let revision = m.world.revision()?;
    let a = m.admission(0x21, 0, 1, 1172)?;
    let stranger = principal(0x22)?;
    let elsewhere = MarketId::new([9; 32])?;
    let other_roster = RosterDigest::new([9; 32])?;
    for (admit, at, code) in [
        (Admit { epoch: 2, ..a }, 1140, WRONG_EPOCH),
        (Admit { config: 2, ..a }, 1140, F01_POLICY_MISMATCH),
        (
            Admit {
                policy: [9; 32],
                ..a
            },
            1140,
            F01_POLICY_MISMATCH,
        ),
        (
            Admit {
                roster: [9; 32],
                ..a
            },
            1140,
            WRONG_ROSTER,
        ),
        (
            Admit {
                input: [0; 32],
                ..a
            },
            1140,
            NON_CANONICAL,
        ),
        (
            Admit {
                deadline: 1140,
                ..a
            },
            1140,
            F01_TASK_EXPIRED,
        ),
        (
            Admit {
                deadline: 1173,
                ..a
            },
            1140,
            F01_TASK_EXPIRED,
        ),
        (a, 1192, WRONG_PHASE),
    ] {
        assert_eq!(m.admit(&admit, at), Err(code));
    }
    assert_eq!(
        m.admit_with(&a, 1140, |call| Req {
            actor: stranger,
            ..call
        }),
        Err(F01_PRINCIPAL_MISMATCH)
    );
    assert_eq!(
        m.admit_with(&a, 1140, |call| Req { epoch: 2, ..call }),
        Err(WRONG_EPOCH)
    );
    assert_eq!(
        m.admit_with(&a, 1140, |call| Req { config: 2, ..call }),
        Err(WRONG_CONFIG)
    );
    assert_eq!(
        m.admit_with(&a, 1140, |call| Req {
            roster: Presence::Present(other_roster),
            ..call
        }),
        Err(WRONG_ROSTER)
    );
    assert_eq!(
        m.admit_with(&a, 1140, |call| Req {
            market: Some(elsewhere),
            ..call
        }),
        Err(WRONG_MARKET)
    );
    assert_eq!(m.world.revision()?, revision);
    assert!(m.tasks()?.is_empty());
    let mut idle = opened(&[1], false)?;
    let a = idle.admission(0x21, 0, 1, 1172)?;
    assert_eq!(idle.admit(&a, 1140), Err(F01_WRONG_LIFECYCLE));
    Ok(())
}

#[test]
fn a13_acceptance_and_result_commitment() -> TestResult {
    let mut m = opened(&[1], true)?;
    let a = m.admission(0x21, 0, 2, 1172)?;
    m.admit(&a, 1140)?;
    let task = m.task_id(0x21, 2)?;
    let expected = m.world.revision()?;
    let Task::Applied {
        receipt, revision, ..
    } = m.step(dispatch::ACCEPT_TASK, 1, 1, task, ACK, 1141)?
    else {
        return Err(NON_CANONICAL);
    };
    let accepted = m.find(task)?;
    assert_eq!(
        (accepted.status, accepted.acknowledgement, accepted.result),
        (TaskStatus::Accepted, Some(Digest32::new(ACK)?), None)
    );
    assert_eq!(m.world.suffix, accepted.encode()?.to_vec());
    let retry = Step {
        operation: dispatch::ACCEPT_TASK,
        worker: 1,
        sequence: 1,
        expected,
        task,
        digest: ACK,
    };
    let Task::Retained(retained) = m.step_with(&retry, 1142, |call| call)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            retained.sequence,
            retained.applied_revision,
            retained.result_digest
        ),
        (1, revision, receipt.digest)
    );
    assert_eq!(m.world.revision()?, revision);
    assert_eq!(m.cancel(0x21, task, 1142), Err(F01_TASK_ALREADY_ACCEPTED));
    assert_eq!(
        m.step(dispatch::ACCEPT_TASK, 1, 2, task, ACK, 1142),
        Err(F01_TASK_ALREADY_ACCEPTED)
    );
    assert_eq!(
        m.step(dispatch::COMMIT_TASK_RESULT, 1, 2, task, RESULT, 1172),
        Err(F01_TASK_EXPIRED)
    );
    let expected = m.world.revision()?;
    let Task::Applied {
        receipt, revision, ..
    } = m.step(dispatch::COMMIT_TASK_RESULT, 1, 2, task, RESULT, 1171)?
    else {
        return Err(NON_CANONICAL);
    };
    let committed = m.find(task)?;
    assert_eq!(
        (
            committed.status,
            committed.acknowledgement,
            committed.result
        ),
        (
            TaskStatus::ResultCommitted,
            Some(Digest32::new(ACK)?),
            Some(Digest32::new(RESULT)?)
        )
    );
    let retry = Step {
        operation: dispatch::COMMIT_TASK_RESULT,
        sequence: 2,
        expected,
        digest: RESULT,
        ..retry
    };
    let Task::Retained(retained) = m.step_with(&retry, 1171, |call| call)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            retained.sequence,
            retained.applied_revision,
            retained.result_digest
        ),
        (2, revision, receipt.digest)
    );
    assert_eq!(
        m.step(dispatch::COMMIT_TASK_RESULT, 1, 3, task, [0xe6; 32], 1171),
        Err(F01_TASK_CONFLICT)
    );
    assert_eq!(m.find(task)?, committed);
    assert_eq!(m.world.revision()?, revision);
    Ok(())
}

#[test]
fn a13_refusals_leave_ack_and_result_unchanged() -> TestResult {
    let mut m = opened(&[1], true)?;
    let a = m.admission(0x21, 0, 2, 1172)?;
    m.admit(&a, 1140)?;
    let task = m.task_id(0x21, 2)?;
    let admitted = m.find(task)?;
    let revision = m.world.revision()?;
    let elsewhere = MarketId::new([9; 32])?;
    let unknown = TaskId::new([0x5a; 32])?;
    for operation in [dispatch::ACCEPT_TASK, dispatch::COMMIT_TASK_RESULT] {
        let step = Step {
            operation,
            worker: 1,
            sequence: 1,
            expected: revision,
            task,
            digest: ACK,
        };
        let wrong_worker = Step { worker: 5, ..step };
        assert_eq!(
            m.step_with(&wrong_worker, 1141, |c| c),
            Err(F01_WRONG_WORKER)
        );
        let zero = Step {
            digest: [0; 32],
            ..step
        };
        assert_eq!(m.step_with(&zero, 1141, |c| c), Err(NON_CANONICAL));
        let stale = Step {
            expected: revision - 1,
            ..step
        };
        assert_eq!(m.step_with(&stale, 1141, |c| c), Err(F01_STALE_REVISION));
        let missing = Step {
            task: unknown,
            ..step
        };
        assert_eq!(m.step_with(&missing, 1141, |c| c), Err(F01_TASK_NOT_FOUND));
        let gap = Step {
            sequence: 2,
            ..step
        };
        assert_eq!(m.step_with(&gap, 1141, |c| c), Err(SEQUENCE_GAP));
        assert_eq!(
            m.step_with(&step, 1141, |c| Req {
                market: Some(elsewhere),
                ..c
            }),
            Err(WRONG_MARKET)
        );
        assert_eq!(
            m.step_with(&step, 1141, |c| Req { config: 2, ..c }),
            Err(WRONG_CONFIG)
        );
        assert_eq!(
            m.step_with(&step, 1141, |c| Req { epoch: 2, ..c }),
            Err(WRONG_EPOCH)
        );
    }
    assert_eq!(
        m.step(dispatch::COMMIT_TASK_RESULT, 1, 1, task, RESULT, 1141),
        Err(F01_TASK_CONFLICT)
    );
    assert_eq!(m.find(task)?, admitted);
    assert_eq!(m.world.revision()?, revision);
    m.step(dispatch::ACCEPT_TASK, 1, 1, task, ACK, 1141)?;
    let accepted = m.find(task)?;
    let wrong = Step {
        operation: dispatch::COMMIT_TASK_RESULT,
        worker: 5,
        sequence: 1,
        expected: revision + 1,
        task,
        digest: RESULT,
    };
    assert_eq!(m.step_with(&wrong, 1150, |c| c), Err(F01_WRONG_WORKER));
    assert_eq!(m.find(task)?, accepted);
    assert_eq!(m.world.revision()?, revision + 1);
    Ok(())
}

#[test]
fn a13_delegate_authority() -> TestResult {
    let mut m = opened(&[1], true)?;
    let a = m.admission(0x21, 0, 2, 1172)?;
    m.admit(&a, 1140)?;
    let task = m.task_id(0x21, 2)?;
    let revision = m.world.revision()?;
    let key = delegate_key(1);
    let foreign = delegate_key(5);
    let step = Step {
        operation: dispatch::ACCEPT_TASK,
        worker: 1,
        sequence: 1,
        expected: revision,
        task,
        digest: ACK,
    };
    let delegated = |claimed: &SigningKey, signer: &SigningKey| {
        let pair = (claimed.clone(), signer.clone());
        move |call: Req| Req {
            delegate: Some(pair),
            ..call
        }
    };
    assert_eq!(
        m.step_with(&step, 1141, delegated(&foreign, &foreign)),
        Err(KEY_MISMATCH)
    );
    assert_eq!(
        m.step_with(&step, 1141, delegated(&key, &foreign)),
        Err(BAD_SIGNATURE)
    );
    let fifth = Step { worker: 5, ..step };
    assert_eq!(
        m.step_with(&fifth, 1141, delegated(&foreign, &foreign)),
        Err(F01_WRONG_WORKER)
    );
    assert_eq!(m.world.revision()?, revision);
    m.step_with(&step, 1141, delegated(&key, &key))?;
    assert_eq!(m.find(task)?.acknowledgement, Some(Digest32::new(ACK)?));
    let commit = Step {
        operation: dispatch::COMMIT_TASK_RESULT,
        sequence: 2,
        expected: revision + 1,
        digest: RESULT,
        ..step
    };
    m.step_with(&commit, 1142, delegated(&key, &key))?;
    let committed = m.find(task)?;
    assert_eq!(
        (committed.status, committed.result),
        (TaskStatus::ResultCommitted, Some(Digest32::new(RESULT)?))
    );
    assert_eq!(m.world.revision()?, revision + 2);
    Ok(())
}

#[test]
fn a14_deadline_ends_at_work_end() -> TestResult {
    let mut m = opened(&[1], true)?;
    let late = m.admission(0x21, 0, 1, 1193)?;
    assert_eq!(m.admit(&late, 1190), Err(F01_TASK_EXPIRED));
    let edge = m.admission(0x21, 0, 1, 1192)?;
    m.admit(&edge, 1190)?;
    let task = m.task_id(0x21, 1)?;
    m.step(dispatch::ACCEPT_TASK, 1, 1, task, ACK, 1191)?;
    let accepted = m.find(task)?;
    assert_eq!(
        m.step(dispatch::COMMIT_TASK_RESULT, 1, 2, task, RESULT, 1192),
        Err(F01_TASK_EXPIRED)
    );
    assert_eq!(m.find(task)?, accepted);
    let digest = m.expected_digest(&m.region()?)?;
    assert_eq!(m.seal(digest, 1191), Err(WRONG_PHASE));
    Ok(())
}

/// Two tasks admitted at 1190 in the given requester order; the first requester's task is
/// accepted at 1191.
fn ordered(first: u8, second: u8) -> CodecResult<Market> {
    let mut m = opened(&[1], true)?;
    for requester in [first, second] {
        let admit = m.admission(requester, 0, 1, 1192)?;
        m.admit(&admit, 1190)?;
    }
    let task = m.task_id(0x21, 1)?;
    m.step(dispatch::ACCEPT_TASK, 1, 1, task, ACK, 1191)?;
    Ok(m)
}

#[test]
fn a14_seal_is_canonical_and_order_independent() -> TestResult {
    let mut a = ordered(0x21, 0x22)?;
    let mut b = ordered(0x22, 0x21)?;
    let region = a.region()?;
    assert_eq!(region, b.region()?);
    assert_eq!(region[..3], [0, 2, 0]);
    assert!(region[3..35] < region[3 + TASK_BINDING_BYTES..35 + TASK_BINDING_BYTES]);
    let digest = a.expected_digest(&region)?;
    assert_eq!(
        tasks::task_set_digest(&a.binding(), &TaskSet::decode(&region)?)?,
        digest
    );
    let revision = a.world.revision()?;
    assert_eq!(a.seal(Digest32::new([9; 32])?, 1192), Err(CONFLICT));
    assert_eq!(a.seal_with(2, digest.bytes(), 1192), Err(WRONG_EPOCH));
    assert_eq!(a.world.revision()?, revision);
    assert_eq!(a.region()?, region);
    a.seal(digest, 1192)?;
    let mut suffix = digest.bytes().to_vec();
    suffix.extend_from_slice(&2u16.to_be_bytes());
    assert_eq!(a.world.suffix, suffix);
    b.seal(digest, 1200)?;
    let sealed = a.region()?;
    assert_eq!(sealed, b.region()?);
    assert_eq!(sealed[..3], [0, 2, 1]);
    assert_eq!(sealed[3..35], digest.bytes());
    assert_eq!(sealed[35..], region[3..]);
    assert_eq!(
        a.seal(digest, 1193)?,
        Task::AlreadyApplied { subject: digest }
    );
    assert_eq!(a.seal(Digest32::new([9; 32])?, 1193), Err(CONFLICT));
    let task = a.task_id(0x21, 1)?;
    assert_eq!(
        a.step(dispatch::COMMIT_TASK_RESULT, 1, 2, task, RESULT, 1193),
        Err(F01_TASK_EXPIRED)
    );
    assert_eq!(a.region()?, sealed);
    let state = decode_shared_state(&a.world.bytes)?;
    assert_eq!(tasks::sealed_task_set(&state, 1)?, digest);
    assert_eq!(tasks::terminal_task_set(&state, 1)?, digest);
    Ok(())
}

#[test]
fn a16_policy_mode_identity() -> TestResult {
    let one = policy(1, 3)?;
    let mut bytes = [0; TASK_POLICY_BYTES];
    assert_eq!(one.encode(&mut bytes)?, 307);
    assert_eq!((one.task_kind, one.assessment_mode), (1, 1));
    let two = TaskPolicyV1 {
        assessment_mode: 2,
        ..one
    };
    assert_ne!(two.digest()?, one.digest()?);
    for mode in [0, 3] {
        let unknown = TaskPolicyV1 {
            assessment_mode: mode,
            ..one
        };
        assert_eq!(unknown.digest(), Err(F01_INVALID_POLICY));
    }
    let mut m = opened(&[1], true)?;
    assert_eq!(m.frozen.policy, one.digest()?);
    let a = m.admission(0x21, 0, 1, 1172)?;
    let revision = m.world.revision()?;
    let moded = Admit {
        policy: two.digest()?.bytes(),
        ..a
    };
    assert_eq!(m.admit(&moded, 1140), Err(F01_POLICY_MISMATCH));
    assert_eq!(m.world.revision()?, revision);
    m.admit(&a, 1140)?;
    assert_eq!(m.world.revision()?, revision + 1);
    Ok(())
}

#[test]
fn a17_seal_phase_and_evidence_views() -> TestResult {
    let mut m = opened(&[1], true)?;
    let empty = m.expected_digest(&EMPTY_TASK_REGION)?;
    assert_eq!(
        tasks::task_set_digest(&m.binding(), &TaskSet::decode(&EMPTY_TASK_REGION)?)?,
        empty
    );
    assert_eq!(m.seal(empty, 1191), Err(WRONG_PHASE));
    {
        let state = decode_shared_state(&m.world.bytes)?;
        assert_eq!(
            tasks::sealed_task_set(&state, 1),
            Err(F09_EVIDENCE_TASK_SET_UNSEALED)
        );
        assert_eq!(tasks::sealed_task_set(&state, 2), Err(WRONG_EPOCH));
        assert_eq!(tasks::terminal_task_set(&state, 1)?, empty);
    }
    assert_eq!(m.region()?, EMPTY_TASK_REGION);
    assert_eq!(
        tasks::region_after_open(&EMPTY_TASK_REGION)?,
        EMPTY_TASK_REGION.as_slice()
    );
    m.seal(empty, 1192)?;
    let mut suffix = empty.bytes().to_vec();
    suffix.extend_from_slice(&[0, 0]);
    assert_eq!(m.world.suffix, suffix);
    {
        let state = decode_shared_state(&m.world.bytes)?;
        assert_eq!(tasks::sealed_task_set(&state, 1)?, empty);
        assert_eq!(tasks::terminal_task_set(&state, 1)?, empty);
    }
    assert_eq!(
        tasks::region_after_open(&m.region()?)?,
        EMPTY_TASK_REGION.as_slice()
    );
    let mut busy = opened(&[1], true)?;
    let a = busy.admission(0x21, 0, 1, 1172)?;
    busy.admit(&a, 1140)?;
    {
        let state = decode_shared_state(&busy.world.bytes)?;
        assert_eq!(tasks::terminal_task_set(&state, 1), Err(WRONG_PHASE));
        assert_eq!(
            tasks::sealed_task_set(&state, 1),
            Err(F09_EVIDENCE_TASK_SET_UNSEALED)
        );
    }
    assert_eq!(tasks::region_after_open(&busy.region()?), Err(WRONG_PHASE));
    let digest = busy.expected_digest(&busy.region()?)?;
    busy.seal(digest, 1207)?;
    let state = decode_shared_state(&busy.world.bytes)?;
    assert_eq!(tasks::sealed_task_set(&state, 1)?, digest);
    assert_eq!(tasks::terminal_task_set(&state, 1)?, digest);
    Ok(())
}

/// Epoch 1 with one admitted task, optionally sealed, terminalized at 1240 and followed by
/// the opening of epoch 2 at 1256 (Work is [1256, 1320)).
/// Epoch 1 with one admitted task, sealed when `seal`, terminalized and moved to the epoch 2
/// opening height; returns the market and the committed epoch-1 region.
fn before_next_epoch(seal: bool) -> CodecResult<(Market, Vec<u8>)> {
    let mut m = opened(&[1], true)?;
    let a = m.admission(0x21, 0, 1, 1172)?;
    m.admit(&a, 1140)?;
    if seal {
        let digest = m.expected_digest(&m.region()?)?;
        m.seal(digest, 1192)?;
    }
    let previous = m.region()?;
    let frozen = m.frozen;
    let entry = m.workers[0];
    m.world.terminalize(&frozen, &entry, 1240)?;
    Ok((m, previous))
}

#[test]
fn a17_next_opening_reads_only_a_sealed_set_as_empty() -> TestResult {
    let (mut m, previous) = before_next_epoch(true)?;
    assert_eq!(
        tasks::region_after_open(&previous)?,
        EMPTY_TASK_REGION.as_slice()
    );
    m.frozen = m.world.open(1256)?;
    assert_eq!(m.frozen.epoch, 2);
    assert_eq!(m.region()?, EMPTY_TASK_REGION);
    {
        let state = decode_shared_state(&m.world.bytes)?;
        assert_eq!(
            tasks::sealed_task_set(&state, 2),
            Err(F09_EVIDENCE_TASK_SET_UNSEALED)
        );
        assert_eq!(tasks::sealed_task_set(&state, 1), Err(WRONG_EPOCH));
        let empty = m.expected_digest(&EMPTY_TASK_REGION)?;
        assert_eq!(tasks::terminal_task_set(&state, 2)?, empty);
    }
    let a = m.admission(0x21, 0, 1, 1300)?;
    m.admit(&a, 1268)?;
    let task = m.task_id(0x21, 1)?;
    let tasks = m.tasks()?;
    assert_eq!(tasks.len(), 1);
    assert_eq!((tasks[0].task, tasks[0].deadline), (task, 1300));
    assert_eq!(m.region()?[..3], [0, 1, 0]);
    let (mut stale, previous) = before_next_epoch(false)?;
    assert_eq!(tasks::region_after_open(&previous), Err(WRONG_PHASE));
    let before = stale.world.bytes.clone();
    assert_eq!(stale.world.open(1256).map(|f| f.epoch), Err(WRONG_PHASE));
    assert_eq!(stale.world.bytes, before);
    assert_eq!(stale.region()?, previous);
    Ok(())
}

#[test]
fn a17_open_epoch_resets_the_sealed_region() -> TestResult {
    let (m, previous) = before_next_epoch(true)?;
    let sealed = {
        let state = decode_shared_state(&m.world.bytes)?;
        let sealed = tasks::sealed_task_set(&state, 1)?;
        assert_eq!(TaskSet::decode(&previous)?.seal(), Some(sealed));
        let mut unsealed = previous[..2].to_vec();
        unsealed.push(0);
        unsealed.extend_from_slice(&previous[35..]);
        assert_eq!(sealed, m.expected_digest(&unsealed)?);
        sealed
    };
    let before = m.world.bytes.clone();
    let revision = m.world.revision()?;
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
    let preview = epoch::preview_open(&before, 1256, &mut next, &mut scratch)?;
    assert_eq!(preview.epoch, 2);
    let call = Req {
        epoch: preview.epoch,
        config: preview.config.get(),
        roster: Presence::Present(preview.roster),
        request: [0x61; 32],
        ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
    };
    let encoded = call.encode()?;
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Opening::Opened {
        frozen, state_len, ..
    } = epoch::open_epoch(
        &call.context(1256)?,
        &decode_envelope(&encoded)?,
        &before,
        &mut next,
        &mut scratch,
        &mut event,
    )?
    else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(frozen.epoch, 2);
    let opened = decode_shared_state(&next[..state_len])?;
    assert_eq!(opened.revision, revision + 1);
    let policy = PolicySection::decode(opened.feature_sections[0])?;
    assert_eq!(policy.header.state_revision, opened.revision);
    assert_eq!(policy.task_region, EMPTY_TASK_REGION.as_slice());
    assert_eq!(
        tasks::sealed_task_set(&opened, 2),
        Err(F09_EVIDENCE_TASK_SET_UNSEALED)
    );
    assert_eq!(
        tasks::terminal_task_set(&opened, 2)?,
        tasks::task_set_digest(
            &SetBinding {
                epoch: frozen.epoch,
                config: frozen.config,
                policy: frozen.policy,
                roster: frozen.roster,
                ..m.binding()
            },
            &TaskSet::decode(&EMPTY_TASK_REGION)?
        )?
    );
    assert_eq!(m.world.bytes, before);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(tasks::sealed_task_set(&state, 1)?, sealed);
    Ok(())
}

#[test]
fn a18_admission_checks_frozen_worker_commitments() -> TestResult {
    let mut m = opened(&[1], true)?;
    let revision = m.world.revision()?;
    let a = m.admission(0x21, 0, 1, 1172)?;
    let other_metadata = Admit {
        metadata: [0x45; 32],
        ..a
    };
    assert_eq!(m.admit(&other_metadata, 1140), Err(WRONG_ROSTER));
    let unknown = Admit {
        worker: derive_worker(m.header.market_id, principal(9)?, [9; 32])?,
        ..a
    };
    assert_eq!(m.admit(&unknown, 1140), Err(F01_UNKNOWN_WORKER));
    assert_eq!(m.world.revision()?, revision);
    assert!(m.tasks()?.is_empty());
    m.admit(&a, 1140)?;
    let late = m.world.enroll(5, 1150)?;
    let unfrozen = Admit {
        worker: late.worker,
        ..m.admission(0x21, 0, 2, 1172)?
    };
    assert_eq!(m.admit(&unfrozen, 1151), Err(F01_UNKNOWN_WORKER));
    let digest = m.expected_digest(&m.region()?)?;
    m.seal(digest, 1192)?;
    let (frozen, entry) = (m.frozen, m.workers[0]);
    m.world.terminalize(&frozen, &entry, 1240)?;
    m.frozen = m.world.open(1256)?;
    m.workers.push(late);
    assert_eq!((m.frozen.epoch, m.frozen.workers), (2, 2));
    let fifth = m.admission(0x21, 1, 2, 1300)?;
    let moved = Admit {
        metadata: [0x45; 32],
        ..fifth
    };
    assert_eq!(m.admit(&moved, 1268), Err(WRONG_ROSTER));
    m.admit(&fifth, 1268)?;
    let fifth_task = m.task_id(0x21, 2)?;
    m.world.revoke(5, late.worker, 1270)?;
    let revoked = m.admission(0x21, 1, 3, 1300)?;
    assert_eq!(m.admit(&revoked, 1271), Err(F02_DELEGATE_REVOKED));
    let key = delegate_key(5);
    let accept = Step {
        operation: dispatch::ACCEPT_TASK,
        worker: 5,
        sequence: 2,
        expected: m.world.revision()?,
        task: fifth_task,
        digest: ACK,
    };
    assert_eq!(
        m.step_with(&accept, 1271, |c| Req {
            delegate: Some((key.clone(), key)),
            ..c
        }),
        Err(F02_DELEGATE_REVOKED)
    );
    assert_eq!(m.find(fifth_task)?.status, TaskStatus::Admitted);
    let first = m.admission(0x21, 0, 4, 1300)?;
    m.admit(&first, 1271)?;
    let tasks = m.tasks()?;
    assert_eq!(tasks.len(), 2);
    assert!(tasks.iter().all(|t| t.deadline == 1300));
    Ok(())
}

#[test]
fn task_codec_invariants() -> TestResult {
    assert_eq!(
        (
            TASK_BINDING_BYTES,
            ADMIT_PAYLOAD_BYTES,
            TASK_REGION_MAX_BYTES
        ),
        (233, 248, 14_947)
    );
    let admitted = TaskBinding {
        task: TaskId::new([3; 32])?,
        requester: principal(0x21)?,
        worker: WorkerId::new([4; 32])?,
        input: Digest32::new([5; 32])?,
        deadline: 1172,
        status: TaskStatus::Admitted,
        acknowledgement: None,
        result: None,
        admission: Digest32::new([8; 32])?,
    };
    let bytes = admitted.encode()?;
    assert_eq!(TaskBinding::decode(&bytes)?, admitted);
    assert_eq!(bytes[128..136], 1172u64.to_be_bytes());
    assert_eq!((bytes[136], &bytes[137..201]), (1, [0; 64].as_slice()));
    let ack = Some(Digest32::new(ACK)?);
    let result = Some(Digest32::new(RESULT)?);
    let committed = TaskBinding {
        status: TaskStatus::ResultCommitted,
        acknowledgement: ack,
        result,
        ..admitted
    };
    let encoded = committed.encode()?;
    assert_eq!(TaskBinding::decode(&encoded)?, committed);
    assert_eq!(encoded[169..201], RESULT);
    for (status, acknowledgement, result) in [
        (TaskStatus::Admitted, ack, None),
        (TaskStatus::Cancelled, None, result),
        (TaskStatus::Accepted, None, None),
        (TaskStatus::Accepted, ack, result),
        (TaskStatus::ResultCommitted, None, result),
        (TaskStatus::ResultCommitted, ack, None),
    ] {
        let invalid = TaskBinding {
            status,
            acknowledgement,
            result,
            ..admitted
        };
        assert_eq!(invalid.encode(), Err(NON_CANONICAL));
    }
    let mut unknown = bytes;
    unknown[136] = 5;
    assert_eq!(TaskBinding::decode(&unknown), Err(NON_CANONICAL));
    let mut missing_ack = bytes;
    missing_ack[136] = 2;
    assert_eq!(TaskBinding::decode(&missing_ack), Err(NON_CANONICAL));
    let mut zero_admission = bytes;
    zero_admission[201..].fill(0);
    assert_eq!(TaskBinding::decode(&zero_admission), Err(NON_CANONICAL));
    assert_eq!(TaskBinding::decode(&bytes[..232]), Err(NON_CANONICAL));
    let later = TaskBinding {
        task: TaskId::new([6; 32])?,
        ..admitted
    }
    .encode()?;
    let region = |count: u8, presence: &[u8], records: &[&[u8]]| {
        let mut out = vec![0, count];
        out.extend_from_slice(presence);
        for record in records {
            out.extend_from_slice(record);
        }
        out
    };
    let zero_seal = [[1].as_slice(), &[0; 32]].concat();
    let ascending = region(2, &[0], &[&bytes, &later]);
    assert_eq!(TaskSet::decode(&ascending)?.len(), 2);
    for invalid in [
        region(2, &[0], &[&later, &bytes]),
        region(2, &[0], &[&bytes, &bytes]),
        region(1, &[2], &[&bytes]),
        region(1, &zero_seal, &[&bytes]),
        region(1, &[0], &[&bytes, &[0]]),
        region(65, &[0], &[]),
    ] {
        assert_eq!(TaskSet::decode(&invalid), Err(NON_CANONICAL));
    }
    Ok(())
}
