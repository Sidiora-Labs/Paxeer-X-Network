//! AI.F08-T03 finalized membership recovery: real F01/F02/F06/F08 transitions and real
//! `OpenEpoch` openings, captured through the chunked read path and bound to finality
//! evidence before the worker treats any membership fact as current.
use ed25519_dalek::{Signer, SigningKey};
use layerx_client::head::Head;
use layerx_paxai_worker::{
    auth::ServiceError,
    discovery::{AuthorityEvidence, IdentityEvidence, MAX_FINALITY_LAG},
    membership::{
        DelegateGeneration, EnrollmentResolution, MembershipError, MembershipObserver,
        MembershipRecord, PendingEnrollment, Retained, Standing, Subject,
    },
};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission as Membership, AdmissionContext, AdmissionMeta, AdmissionTable,
        ApprovalTerms, EvaluatorConsent, ExitCause, ExitReason, Participant, PendingExit,
        RemovalReason, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        encode_roster, Envelope, Roster,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, F06_UNKNOWN_WORKER_ENTITLEMENT,
        F08_ADMISSION_WINDOW_FULL, F08_BAD_CONSENT, F08_DELEGATE_REVOKED, F08_OWNER_REQUIRED,
        F08_RETENTION_BLOCKED, F08_WRONG_GENERATION, NON_CANONICAL, READINESS_BLOCKED, WRONG_EPOCH,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        read_state_chunk, CaptureFacts, FinalityEvidence, QueryError, ReadProof, StateCapture,
    },
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reward_math::allocate,
    rewards::{
        decode_reward_state, ClaimDecision, FundReplay, FundRequest, FundingAuthority,
        FundingPhase, RewardLedger, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorRosterEntry,
        FrozenBinding, MarketId, MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32,
        RequestDigest, RequestId, ResultDigest, RosterDigest, RubricDigest, Version, WorkerId,
        WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const ROOT: [u8; 32] = [0xA1; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 1000;
const CHUNK_RESPONSE_MAX: usize = 8_244;

enum Failure {
    Application(ApplicationError),
    Service(ServiceError),
    Membership(MembershipError),
    Query(QueryError),
    Io(io::ErrorKind),
    Unexpected(&'static str),
}
impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Service(error) => write!(f, "service refusal {error:?}"),
            Self::Membership(error) => write!(f, "membership refusal {error:?}"),
            Self::Query(error) => write!(f, "query refusal {error:?}"),
            Self::Io(kind) => write!(f, "io failure {kind:?}"),
            Self::Unexpected(what) => write!(f, "unexpected {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<ServiceError> for Failure {
    fn from(error: ServiceError) -> Self {
        Self::Service(error)
    }
}
impl From<MembershipError> for Failure {
    fn from(error: MembershipError) -> Self {
        Self::Membership(error)
    }
}
impl From<QueryError> for Failure {
    fn from(error: QueryError) -> Self {
        Self::Query(error)
    }
}
impl From<io::Error> for Failure {
    fn from(error: io::Error) -> Self {
        Self::Io(error.kind())
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn principal(n: u8) -> CodecResult<PrincipalId> {
    let mut bytes = [0x50; 32];
    bytes[0] = n;
    PrincipalId::new(bytes)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn epoch_height(origin: u64, epoch: u64, offset: u64) -> u64 {
    origin + epoch * 128 + offset
}
fn rubric() -> CodecResult<RubricDigest> {
    RubricDigest::new([4; 32])
}
fn task_policy() -> CodecResult<TaskPolicyV1> {
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
    Ok(policy)
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
fn market_id() -> CodecResult<MarketId> {
    derive_market(ChainDomain::new(CHAIN)?, ProgramId::new(PROGRAM)?)
}
/// Delegate key of worker owner `n` at key version `version`.
fn delegate(n: u8, version: u8) -> PublicKey32 {
    public(&SigningKey::from_bytes(
        &[0xD0 + n + 0x10 * (version - 1); 32],
    ))
}
/// Fresh F02 ENROLLED record of the worker owned by `principal(n)`.
fn worker_record(n: u8) -> CodecResult<WorkerCurrent> {
    let owner = principal(n)?;
    Ok(WorkerCurrent {
        worker: derive_worker(market_id()?, owner, [n; 32])?,
        owner,
        delegate: delegate(n, 1),
        metadata: MetadataDigest::new([0x3D; 32])?,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        valid_from: 1,
        expiry: 4_000,
        revocation_sequence: 0,
        effective_epoch: 0,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Enrolled,
        slot: 0,
        last_metadata_height: 1,
    })
}
fn roster_entry(record: &WorkerCurrent) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: record.worker,
        owner: record.owner,
        recipient: AccountId::new(record.owner.bytes())?,
        generation: Version::new(record.generation)?,
        key_version: Version::new(record.key_version)?,
        public_key: record.delegate,
        metadata: record.metadata,
    })
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
/// The frozen roster document of `epoch`, as an indexer would serve it.
fn roster_document(
    epoch: u64,
    workers: &[WorkerRosterEntry],
    evaluators: &[EvaluatorRosterEntry],
) -> CodecResult<Vec<u8>> {
    let mut workers = workers.to_vec();
    workers.sort_by_key(|w| w.worker);
    let mut evaluators = evaluators.to_vec();
    evaluators.sort_by_key(|e| e.evaluator);
    let mut out = vec![0; 54 + 176 * workers.len() + 144 * evaluators.len()];
    let n = encode_roster(
        &Roster {
            market: market_id()?,
            epoch,
            config: version()?,
            workers: &workers,
            evaluators: &evaluators,
        },
        &mut out,
    )?;
    out.truncate(n);
    Ok(out)
}

fn encode_state(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
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

/// Committed state after the real F01 CREATE at `origin`.
fn create(origin: u64) -> CodecResult<Vec<u8>> {
    let program = ProgramId::new(PROGRAM)?;
    let mut policy_bytes = [0; TASK_POLICY_BYTES];
    task_policy()?.encode(&mut policy_bytes)?;
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
    encode_state(&state)
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
        encode_state(&SharedState {
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
    /// The F06 reward state and the bytes that follow it in the section.
    fn reward_parts(&self) -> CodecResult<(RewardState<'_>, &[u8])> {
        let (head, tail) = self
            .rewards
            .split_at_checked(REWARD_STATE_BYTES)
            .ok_or(NON_CANONICAL)?;
        Ok((decode_reward_state(head)?, tail))
    }
}

fn admission_context(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height: at,
    }
}

fn evaluator_grant(
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

/// Exact F08 consent of evaluator `n` for its pending grant and stored approval.
fn consent(
    market: &MarketHeader,
    grant: &EvaluatorGrant,
    n: u8,
    approval: Digest32,
) -> CodecResult<EvaluatorConsent> {
    Ok(EvaluatorConsent {
        chain: market.deployment_chain_domain,
        program: market.program_id,
        market: market.market_id,
        evaluator: grant.evaluator,
        owner: grant.principal,
        signing_key: grant.signing_key,
        enrollment_nonce: [n; 32],
        rubric: grant.rubric,
        approval_digest: approval,
        request: RequestId::new([n; 32])?,
        grant_version: 1,
        key_version: 1,
        effective_epoch: grant.effective_epoch,
        config_version: 1,
        expiry_height: epoch_height(market.origin_height, grant.effective_epoch, 64),
    })
}
fn signed(consent: &EvaluatorConsent, key: &SigningKey) -> CodecResult<Vec<u8>> {
    let mut bytes = [0u8; 362];
    consent.encode(&mut bytes)?;
    let mut payload = bytes.to_vec();
    payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
    Ok(payload)
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
    fn header(&self) -> CodecResult<MarketHeader> {
        self.parts()?.market()
    }
    fn admission(&self) -> CodecResult<AdmissionTable> {
        Ok(self.parts()?.admission)
    }
    fn meta(&self, participant: Participant) -> Checked<AdmissionMeta> {
        self.admission()?
            .get(participant)
            .ok_or(Failure::Unexpected("member record"))
    }
    fn record(&self, worker: WorkerId) -> Checked<WorkerCurrent> {
        self.parts()?
            .workers
            .get(worker)
            .ok_or(Failure::Unexpected("worker record"))
    }
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
    fn schedule(&mut self, epoch: u64, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        let call = Call {
            operation: dispatch::SCHEDULE_ACTIVATION,
            actor: PrincipalId::new(OWNER)?,
            epoch: 0,
            config: self.header()?.active_config_version,
            roster: Presence::Absent,
            sequence: self.owner_sequence,
            request: 0x20 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?,
            payload: &payload,
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
        self.bytes = encode_state(&state)?;
        self.owner_sequence += 1;
        Ok(())
    }
}

/// F02 records and F08 approvals, admissions and liveness.
impl World {
    /// F02 record in the lowest free slot and its bound worker replay slot.
    fn register(&mut self, record: &WorkerCurrent) -> CodecResult<WorkerCurrent> {
        self.edit(|parts, _| {
            let record = WorkerCurrent {
                slot: parts.workers.free_slot()?,
                ..*record
            };
            parts.workers.insert(&record)?;
            parts.replay.bind(
                ActorSlot::worker(usize::from(record.slot))?,
                record.owner,
                version()?,
            )?;
            Ok(record)
        })
    }
    /// Market-owner approval for `effective` expiring at `expiry`.
    fn approve(
        &mut self,
        participant: Participant,
        owner: PrincipalId,
        delegate: PublicKey32,
        effective: u64,
        expiry: u64,
        at: u64,
    ) -> CodecResult<Digest32> {
        self.edit(|parts, market| {
            parts.admission.approve(
                &admission_context(market, market.owner_principal, at),
                &ApprovalTerms {
                    participant,
                    owner,
                    enrollment_nonce_commitment: Digest32::new([7; 32])?,
                    delegate,
                    delegate_generation: 1,
                    identity_commitment: Digest32::new([9; 32])?,
                    effective_epoch: effective,
                    config_version: 1,
                    request: RequestId::new(participant.bytes())?,
                    expiry_height: expiry,
                },
            )
        })
    }
    /// Owner acceptance consuming the stored approval.
    fn admit(
        &mut self,
        participant: Participant,
        owner: PrincipalId,
        effective: u64,
        approval: Digest32,
        at: u64,
    ) -> CodecResult<()> {
        self.edit(|parts, market| {
            parts
                .admission
                .admit(
                    &admission_context(market, owner, at),
                    &Membership {
                        participant,
                        delegate_generation: 1,
                        effective_epoch: effective,
                        config_version: 1,
                        approval_digest: approval,
                    },
                )
                .map(|_| ())
        })
    }
    /// Effective epoch and approval expiry an approval at `at` must carry.
    fn required(&self, at: u64) -> CodecResult<(u64, u64)> {
        let market = self.header()?;
        let effective = self
            .admission()?
            .required_effective_epoch(&admission_context(&market, market.owner_principal, at))?;
        Ok((effective, epoch_height(market.origin_height, effective, 64)))
    }
    /// Register, approve and admit the worker of owner `n` for the required epoch.
    fn enroll(&mut self, n: u8, at: u64) -> Checked<WorkerCurrent> {
        let record = self.register(&worker_record(n)?)?;
        let participant = Participant::Worker(record.worker);
        let (effective, expiry) = self.required(at)?;
        let approval = self.approve(
            participant,
            record.owner,
            record.delegate,
            effective,
            expiry,
            at,
        )?;
        self.admit(participant, record.owner, effective, approval, at)?;
        Ok(record)
    }
    /// F03 PENDING grant of evaluator `n` and its stored F08 approval.
    fn schedule_evaluator(&mut self, n: u8, at: u64) -> CodecResult<(EvaluatorGrant, Digest32)> {
        let (effective, expiry) = self.required(at)?;
        let market = self.header()?;
        let owner = principal(n)?;
        let signing_key = public(&SigningKey::from_bytes(&[n; 32]));
        let grant = evaluator_grant(&market, owner, n, signing_key, effective)?;
        let participant =
            Participant::Evaluator(derive_evaluator(market.market_id, owner, [n; 32])?);
        let approval = self.approve(participant, owner, signing_key, effective, expiry, at)?;
        self.edit(|parts, _| parts.insert_grant(grant, n))?;
        Ok((grant, approval))
    }
    /// `AdmitEvaluator` by `caller` over one signed consent payload.
    fn accept(
        &mut self,
        grant: &EvaluatorGrant,
        caller: PrincipalId,
        request: RequestId,
        payload: &[u8],
        at: u64,
    ) -> CodecResult<()> {
        self.edit(|parts, market| {
            admit_evaluator(
                &mut parts.admission,
                &admission_context(market, caller, at),
                grant,
                request,
                payload,
            )
            .map(|_| ())
        })
    }
    /// Scheduled, consented and accepted evaluator `n`.
    fn evaluator(&mut self, n: u8, at: u64) -> CodecResult<EvaluatorRosterEntry> {
        let (grant, approval) = self.schedule_evaluator(n, at)?;
        let consent = consent(&self.header()?, &grant, n, approval)?;
        let payload = signed(&consent, &SigningKey::from_bytes(&[n; 32]))?;
        self.accept(&grant, grant.principal, consent.request, &payload, at)?;
        Ok(evaluator_entry(&grant))
    }
    fn heartbeat(
        &mut self,
        participant: Participant,
        membership_generation: u64,
        delegate_generation: u64,
        epoch: u64,
        at: u64,
    ) -> CodecResult<AdmissionMeta> {
        self.edit(|parts, market| {
            let owner = parts.admission.get(participant).ok_or(NON_CANONICAL)?.owner;
            parts.admission.heartbeat(
                &admission_context(market, owner, at),
                participant,
                membership_generation,
                delegate_generation,
                epoch,
            )
        })
    }
}

/// F06 funding, terminal results and claims; the permissionless activation and opening.
impl World {
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
        let mut replay = parts.replay.clone();
        let mut revision = parts.revision;
        let (rewards, tail) = parts.reward_parts()?;
        rewards.fund(
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
                table: &mut replay,
                request: &request,
                height: at,
                revision: &mut revision,
                result: ResultDigest::new([tag; 32])?,
            },
            &mut funded,
        )?;
        funded.extend_from_slice(tail);
        parts.rewards = funded;
        parts.replay = replay;
        parts.revision = revision;
        self.bytes = parts.encode()?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// F05 terminal result giving the whole budget of 19 to the single frozen worker.
    fn terminalize(
        &mut self,
        epoch: u64,
        roster: RosterDigest,
        entry: &WorkerRosterEntry,
        at: u64,
    ) -> CodecResult<()> {
        self.edit(|parts, market| {
            let binding = FrozenBinding {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                epoch,
                config: version()?,
                roster,
            };
            let frozen = [*entry];
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
                EpochAggregation::structural(binding, Digest32::new([5; 32])?, &frozen, &outputs)?;
            let mut terminal = vec![0; REWARD_STATE_BYTES];
            let (rewards, tail) = parts.reward_parts()?;
            rewards.terminalize(
                &binding,
                aggregation.root(),
                &allocation,
                &frozen,
                at,
                &mut terminal,
            )?;
            terminal.extend_from_slice(tail);
            parts.rewards = terminal;
            Ok(())
        })
    }
    fn claim(
        &self,
        epoch: u64,
        worker: WorkerId,
        recipient: AccountId,
        at: u64,
    ) -> CodecResult<ClaimDecision> {
        let parts = self.parts()?;
        let (rewards, _) = parts.reward_parts()?;
        rewards
            .row(epoch)?
            .check_claim(&rewards.dictionary(), worker, recipient, 19, at)
    }
    fn advance(&mut self, at: u64) -> CodecResult<()> {
        let header = self.header()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&header.activation_epoch.to_be_bytes());
        let call = Call {
            operation: dispatch::ADVANCE_ACTIVATION,
            actor: principal(KEEPER)?,
            epoch: 0,
            config: header.active_config_version,
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
        Ok(())
    }
    /// Real `OPEN_EPOCH` of the clock epoch of `at` naming the previewed binding.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let preview = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Call {
            operation: dispatch::OPEN_EPOCH,
            actor: principal(KEEPER)?,
            epoch: preview.epoch,
            config: preview.config.get(),
            roster: Presence::Present(preview.roster),
            sequence: 0,
            request: 0x60,
            payload: &[],
        };
        let encoded = call.encode()?;
        let mut event = vec![0; MAX_EVENT_BYTES];
        match epoch::open_epoch(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )? {
            Opening::Opened {
                state_len, frozen, ..
            } => {
                next.truncate(state_len);
                self.bytes = next;
                Ok(frozen)
            }
            Opening::AlreadyApplied { .. } => Err(NON_CANONICAL),
        }
    }
}

/// Bootstrap market at `origin`, still REGISTERED: worker of owner 1 and evaluators 2..=4,
/// all admitted for epoch 0, and a funding of 19.
struct Bootstrap {
    world: World,
    worker: WorkerCurrent,
    evaluators: Vec<EvaluatorRosterEntry>,
}
fn bootstrap(origin: u64, step: u64) -> Checked<Bootstrap> {
    let mut world = World::create(origin)?;
    let worker = world.enroll(1, origin + step)?;
    let mut evaluators = Vec::new();
    for n in 2..=4u8 {
        evaluators.push(world.evaluator(n, origin + step * u64::from(n))?);
    }
    world.fund(19, origin + step * 5)?;
    Ok(Bootstrap {
        world,
        worker,
        evaluators,
    })
}

/// Activated market with epoch 1 open at 1129 (Work window 1128..1192): worker of owner 1
/// admitted for epoch 0 and installed at the actual epoch 1, three evaluators, budget 19.
struct Journey {
    world: World,
    worker: WorkerCurrent,
    evaluators: Vec<EvaluatorRosterEntry>,
    frozen: Frozen,
    roster: Vec<u8>,
}
fn journey() -> Checked<Journey> {
    let mut world = World::create(ORIGIN)?;
    world.schedule(1, 1001)?;
    let worker = world.enroll(1, 1002)?;
    let mut evaluators = Vec::new();
    for n in 2..=4u8 {
        evaluators.push(world.evaluator(n, 1004 + u64::from(n))?);
    }
    world.fund(19, 1128)?;
    world.advance(1128)?;
    let frozen = world.open(1129)?;
    let roster = roster_document(1, &[roster_entry(&worker)?], &evaluators)?;
    Ok(Journey {
        world,
        worker,
        evaluators,
        frozen,
        roster,
    })
}

fn chunk_payload(revision: u64, pinned: Option<[u8; 32]>, offset: u32) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.unwrap_or([0; 32]));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&8_192u16.to_be_bytes());
    payload
}

/// One complete verified capture of `state` at `at` through the real chunked read path.
fn capture(state: &[u8], at: u64) -> Checked<(Vec<u8>, CaptureFacts)> {
    let proof = ReadProof {
        chain: ChainDomain::new(CHAIN)?,
        program: ProgramId::new(PROGRAM)?,
        native_state_root: Digest32::new(ROOT)?,
        observed_sequence: 77,
        execution_height: at,
        batch_id: Digest32::new([0xBB; 32])?,
    };
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let n = read_state_chunk(state, &chunk_payload(0, None, 0), &mut out)?;
    let first = codec::decode_chunk_response(&out[..n])?;
    let (revision, pinned, total) = (first.revision, first.digest, first.total_bytes);
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut capture = StateCapture::new(&mut buffer);
    capture.accept(&proof, &out[..n])?;
    for offset in (8_192..total).step_by(8_192) {
        let n = read_state_chunk(
            state,
            &chunk_payload(revision, Some(pinned.bytes()), offset),
            &mut out,
        )?;
        capture.accept(&proof, &out[..n])?;
    }
    let (bytes, facts) = capture.finish()?;
    Ok((bytes.to_vec(), facts))
}

/// Everything a client holds about one finalized height, every field adjustable.
struct View {
    bytes: Vec<u8>,
    facts: CaptureFacts,
    finality: FinalityEvidence,
    sealed: u64,
    owner: IdentityEvidence,
    roster: Option<Vec<u8>>,
}
fn view(world: &World, roster: Option<&[u8]>, at: u64, owner: PrincipalId) -> Checked<View> {
    let (bytes, facts) = capture(&world.bytes, at)?;
    Ok(View {
        bytes,
        facts,
        finality: FinalityEvidence {
            native_state_root: Digest32::new(ROOT)?,
            checkpoint: Digest32::new([0xC1; 32])?,
            settlement: Presence::Present(Digest32::new([0xD3; 32])?),
            rank: 4,
        },
        sealed: at + MAX_FINALITY_LAG,
        owner: IdentityEvidence {
            principal: owner,
            primary_key: PublicKey32([0x0E; 32]),
            frozen: false,
            execution_height: at,
        },
        roster: roster.map(<[u8]>::to_vec),
    })
}
impl View {
    fn evidence(&self) -> AuthorityEvidence<'_> {
        AuthorityEvidence {
            state: &self.bytes,
            facts: &self.facts,
            finality: &self.finality,
            head: Head {
                chain_sequence: 77,
                sealed_batch: self.sealed,
                finalised_checkpoint: [0xC1; 32],
            },
            owner: self.owner,
            roster: self.roster.as_deref(),
            observed_ms: 5_000,
        }
    }
}
fn observe(observer: &mut MembershipObserver, view: &View) -> Result<Standing, MembershipError> {
    observer.observe(Some(&view.evidence()))
}

fn subject(worker: WorkerId) -> CodecResult<Subject> {
    Ok(Subject {
        chain: ChainDomain::new(CHAIN)?,
        program: ProgramId::new(PROGRAM)?,
        market: market_id()?,
        worker,
    })
}
/// An empty directory private to one test.
fn fresh(name: &str) -> Checked<PathBuf> {
    let directory =
        std::env::temp_dir().join(format!("paxai-membership-{name}-{}", std::process::id()));
    match fs::remove_dir_all(&directory) {
        Err(error) if error.kind() != io::ErrorKind::NotFound => return Err(error.into()),
        _ => {}
    }
    Ok(directory)
}
fn observer(directory: &Path, worker: WorkerId) -> Checked<MembershipObserver> {
    Ok(MembershipObserver::open(directory, subject(worker)?)?)
}
fn stale() -> Result<Standing, MembershipError> {
    Err(MembershipError::Service(ServiceError::StaleAuthority))
}

#[test]
fn membership_recovery_a17_open_epoch_zero_freezes_bootstrap_members() -> Checked {
    let directory = fresh("a17")?;
    let mut world = World::create(ORIGIN)?;
    let worker = world.enroll(1, 1001)?;
    let mut evaluators = vec![world.evaluator(2, 1002)?, world.evaluator(3, 1003)?];
    let mut short = world.clone();
    evaluators.push(world.evaluator(4, 1004)?);
    world.fund(19, 1005)?;
    short.fund(19, 1005)?;
    assert_eq!(short.open(1008).err(), Some(READINESS_BLOCKED));

    let mut observer = observer(&directory, worker.worker)?;
    let before = view(&world, None, 1007, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &before)?,
        Standing::Staged { effective_epoch: 0 }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );

    let frozen = world.open(1008)?;
    assert_eq!(
        (
            frozen.epoch,
            frozen.previous,
            frozen.workers,
            frozen.evaluators,
            frozen.budget
        ),
        (0, None, 1, 3, 19)
    );
    assert_eq!(world.admission()?.current_epoch(), Some(0));
    let meta = world.meta(Participant::Worker(worker.worker))?;
    assert_eq!(
        (
            meta.admitted_epoch,
            meta.immunity_until_epoch,
            meta.last_activity(),
            meta.last_heartbeat_epoch,
            meta.last_heartbeat_height,
            meta.admission_height
        ),
        (Some(0), 0, Some(0), None, None, Some(1001))
    );

    let roster = roster_document(0, &[roster_entry(&worker)?], &evaluators)?;
    let after = view(&world, Some(&roster), 1010, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &after)?,
        Standing::Active { admitted_epoch: 0 }
    );
    let current = observer.current().ok_or(Failure::Unexpected("current"))?;
    assert_eq!(current.authority.current_epoch(), Some(0));
    assert_eq!(current.authority.roster(), Presence::Present(frozen.roster));
    assert_eq!(current.retained, Retained::default());
    assert_eq!(current.meta.map(|m| m.last_heartbeat_height), Some(None));
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );
    assert!(observer.admitted_work().is_ok());
    assert_eq!(observer.record().observed.map(|o| o.height), Some(1010));
    Ok(())
}

#[test]
fn membership_recovery_a16_missing_or_invalid_finality_keeps_not_ready() -> Checked {
    let directory = fresh("a16-finality")?;
    let j = journey()?;
    let (worker, owner) = (j.worker.worker, j.worker.owner);
    let mut observer = observer(&directory, worker)?;
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );
    assert_eq!(
        observer.admitted_work().err(),
        Some(ServiceError::StaleAuthority)
    );
    assert_eq!(observer.observe(None), stale());

    let mut v = view(&j.world, Some(&j.roster), 1150, owner)?;
    v.finality.rank = 3;
    assert_eq!(observe(&mut observer, &v), stale());
    v.finality.rank = 4;
    v.sealed = 1150 + MAX_FINALITY_LAG + 1;
    assert_eq!(observe(&mut observer, &v), stale());
    v.sealed = 1150 + MAX_FINALITY_LAG;
    v.finality.native_state_root = Digest32::new([0xA2; 32])?;
    assert_eq!(observe(&mut observer, &v), stale());
    v.finality.native_state_root = Digest32::new(ROOT)?;
    v.roster = None;
    assert_eq!(observe(&mut observer, &v), stale());
    assert_eq!(observer.record(), &MembershipRecord::default());
    assert!(!directory.join("membership.record").exists());
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );

    v.roster = Some(j.roster.clone());
    assert_eq!(
        observe(&mut observer, &v)?,
        Standing::Active { admitted_epoch: 1 }
    );
    assert_eq!(observer.new_work()?.height(), 1150);
    assert_eq!(j.frozen.epoch, 1);

    assert_eq!(observer.observe(None), stale());
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );
    assert_eq!(observer.record().observed.map(|o| o.height), Some(1150));

    drop(observer);
    let mut observer = self::observer(&directory, worker)?;
    assert_eq!(observer.record().observed.map(|o| o.height), Some(1150));
    assert_eq!(observer.current(), None);
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );
    let older = view(&j.world, Some(&j.roster), 1149, owner)?;
    assert_eq!(observe(&mut observer, &older), stale());
    assert_eq!(observer.record().observed.map(|o| o.height), Some(1150));
    assert_eq!(
        observe(&mut observer, &v)?,
        Standing::Active { admitted_epoch: 1 }
    );
    assert!(observer.new_work().is_ok());
    Ok(())
}

#[test]
fn membership_recovery_a16_ambiguous_enrollment_resolves_original_activity() -> Checked {
    let directory = fresh("a16-enrollment")?;
    let j = journey()?;
    let mut world = j.world;
    let second = world.register(&worker_record(6)?)?;
    let participant = Participant::Worker(second.worker);
    let approval = world.approve(participant, second.owner, second.delegate, 2, 1320, 1150)?;
    let pending = PendingEnrollment {
        approval,
        request: RequestId::new([0xE1; 32])?,
    };
    let mut observer = observer(&directory, second.worker)?;
    observer.record_enrollment(pending)?;
    observer.record_enrollment(pending)?;
    assert_eq!(
        observer.record_enrollment(PendingEnrollment {
            approval,
            request: RequestId::new([0xE2; 32])?,
        }),
        Err(MembershipError::Service(ServiceError::IdempotencyConflict))
    );

    drop(observer);
    let mut observer = self::observer(&directory, second.worker)?;
    assert_eq!(observer.record().enrollment, Some(pending));
    assert_eq!(
        observer.reconcile_enrollment(),
        Err(MembershipError::Service(ServiceError::StaleAuthority))
    );

    let approved = view(&world, Some(&j.roster), 1151, second.owner)?;
    assert_eq!(
        observe(&mut observer, &approved)?,
        Standing::Approved {
            effective_epoch: 2,
            expiry_height: 1320
        }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );
    assert_eq!(
        observer.reconcile_enrollment()?,
        EnrollmentResolution::ResendOriginal {
            request: pending.request
        }
    );
    assert_eq!(observer.record().enrollment, Some(pending));

    world.admit(participant, second.owner, 2, approval, 1152)?;
    let staged = view(&world, Some(&j.roster), 1153, second.owner)?;
    assert_eq!(
        observe(&mut observer, &staged)?,
        Standing::Staged { effective_epoch: 2 }
    );
    assert_eq!(
        observer.reconcile_enrollment()?,
        EnrollmentResolution::Admitted
    );
    assert_eq!(observer.record().enrollment, None);
    assert_eq!(
        observer.reconcile_enrollment(),
        Err(MembershipError::Service(ServiceError::NotFound))
    );
    drop(observer);
    assert_eq!(
        self::observer(&directory, second.worker)?
            .record()
            .enrollment,
        None
    );

    let lapsing = fresh("a16-lapsed")?;
    let third = world.register(&worker_record(7)?)?;
    let approval = world.approve(
        Participant::Worker(third.worker),
        third.owner,
        third.delegate,
        2,
        1170,
        1154,
    )?;
    let mut observer = self::observer(&lapsing, third.worker)?;
    observer.record_enrollment(PendingEnrollment {
        approval,
        request: RequestId::new([0xE3; 32])?,
    })?;
    let expired = view(&world, Some(&j.roster), 1171, third.owner)?;
    assert_eq!(
        observe(&mut observer, &expired)?,
        Standing::Approved {
            effective_epoch: 2,
            expiry_height: 1170
        }
    );
    assert_eq!(
        observer.reconcile_enrollment()?,
        EnrollmentResolution::Lapsed
    );
    assert_eq!(observer.record().enrollment, None);
    Ok(())
}

#[test]
fn membership_recovery_a09_last_vacancy_admits_exactly_one() -> Checked {
    let directory = fresh("a09-winner")?;
    let losing = fresh("a09-loser")?;
    let mut world = World::create(ORIGIN)?;
    world.enroll(1, 1001)?;
    world.evaluator(2, 1002)?;
    world.evaluator(3, 1003)?;
    let winner = world.register(&worker_record(6)?)?;
    let loser = world.register(&worker_record(7)?)?;
    let (w, l) = (
        Participant::Worker(winner.worker),
        Participant::Worker(loser.worker),
    );
    let winner_approval = world.approve(w, winner.owner, winner.delegate, 0, 1064, 1004)?;
    let loser_approval = world.approve(l, loser.owner, loser.delegate, 0, 1064, 1004)?;
    world.admit(w, winner.owner, 0, winner_approval, 1005)?;
    let before = world.admission()?;
    assert_eq!(
        world.admit(l, loser.owner, 0, loser_approval, 1005).err(),
        Some(F08_ADMISSION_WINDOW_FULL)
    );
    let table = world.admission()?;
    assert_eq!(table, before);
    assert_eq!(table.enrollments_this_epoch(), 4);
    assert_eq!(table.iter().filter(|m| m.admitted()).count(), 4);
    let lost = world.meta(l)?;
    assert_eq!(
        (
            lost.admitted(),
            lost.admission_height,
            lost.membership_flags,
            lost.approval.map(|a| a.digest)
        ),
        (false, None, 0, Some(loser_approval))
    );

    let mut first = observer(&directory, winner.worker)?;
    let mut second = observer(&losing, loser.worker)?;
    let winner_view = view(&world, None, 1006, winner.owner)?;
    let loser_view = view(&world, None, 1006, loser.owner)?;
    assert_eq!(
        observe(&mut first, &winner_view)?,
        Standing::Staged { effective_epoch: 0 }
    );
    assert_eq!(
        observe(&mut second, &loser_view)?,
        Standing::Approved {
            effective_epoch: 0,
            expiry_height: 1064
        }
    );
    assert_eq!(
        second.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );
    let recorded = first.record().clone();
    let snapshot = recorded
        .observed
        .ok_or(Failure::Unexpected("observation"))?;

    drop(first);
    let mut first = observer(&directory, winner.worker)?;
    assert_eq!(first.record(), &recorded);
    assert_eq!(
        observe(&mut first, &winner_view)?,
        Standing::Staged { effective_epoch: 0 }
    );
    assert_eq!(first.record().observed, Some(snapshot));
    assert_eq!(world.admission()?.enrollments_this_epoch(), 4);
    Ok(())
}

/// Epoch-1 member after a heartbeat, an F02 rotation to generation 2 and the opening of
/// epoch 2, with its terminal epoch-1 entitlement observed by `observer`.
struct Rotation {
    directory: PathBuf,
    world: World,
    worker: WorkerCurrent,
    rotated: WorkerCurrent,
    roster: Vec<u8>,
    observer: MembershipObserver,
    superseded: DelegateGeneration,
    current: DelegateGeneration,
    owed: Retained,
}
/// F02 rotation pending for epoch 2: the observer records it and refuses new work.
fn rotate(
    world: &mut World,
    observer: &mut MembershipObserver,
    worker: &WorkerCurrent,
    roster: &[u8],
    original: DelegateGeneration,
) -> Checked<(WorkerCurrent, DelegateGeneration, DelegateGeneration)> {
    let rotated = WorkerCurrent {
        generation: 2,
        key_version: 2,
        delegate: delegate(1, 2),
        effective_epoch: 2,
        ..world.record(worker.worker)?
    };
    world.edit(|parts, _| parts.workers.replace(&rotated))?;
    let mut pending = view(world, Some(roster), 1160, worker.owner)?;
    pending.owner.primary_key = PublicKey32([0x0F; 32]);
    assert_eq!(
        observe(observer, &pending)?,
        Standing::Active { admitted_epoch: 1 }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::WrongGeneration)
    );
    let superseded = DelegateGeneration {
        superseded_height: Some(1160),
        ..original
    };
    let current = DelegateGeneration {
        generation: 2,
        key_version: 2,
        delegate: delegate(1, 2),
        first_height: 1160,
        superseded_height: None,
        revoked: false,
    };
    assert_eq!(observer.record().generations, vec![superseded, current]);

    Ok((rotated, superseded, current))
}
fn rotated(name: &str) -> Checked<Rotation> {
    let directory = fresh(name)?;
    let j = journey()?;
    let mut world = j.world;
    let worker = j.worker;
    let participant = Participant::Worker(worker.worker);
    world.heartbeat(participant, 1, 1, 1, 1150)?;
    let mut observer = observer(&directory, worker.worker)?;
    let first = view(&world, Some(&j.roster), 1151, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &first)?,
        Standing::Active { admitted_epoch: 1 }
    );
    let original = DelegateGeneration {
        generation: 1,
        key_version: 1,
        delegate: delegate(1, 1),
        first_height: 1151,
        superseded_height: None,
        revoked: false,
    };
    assert_eq!(observer.record().generations, vec![original]);
    let (rotated, superseded, current) =
        rotate(&mut world, &mut observer, &worker, &j.roster, original)?;

    world.terminalize(1, j.frozen.roster, &roster_entry(&worker)?, 1200)?;
    world.fund(19, 1201)?;
    let frozen = world.open(1256)?;
    assert_eq!((frozen.epoch, frozen.workers), (2, 1));
    let meta = world.meta(participant)?;
    assert_eq!(
        (
            meta.admitted_epoch,
            meta.immunity_until_epoch,
            meta.membership_generation,
            meta.delegate_generation,
            meta.last_heartbeat_epoch,
            meta.last_heartbeat_height,
            meta.owner
        ),
        (Some(1), 1, 1, 2, Some(1), Some(1150), worker.owner)
    );
    assert_eq!(
        world.heartbeat(participant, 1, 1, 2, 1260).err(),
        Some(F08_WRONG_GENERATION)
    );
    world.heartbeat(participant, 1, 2, 2, 1260)?;

    let roster = roster_document(2, &[roster_entry(&rotated)?], &j.evaluators)?;
    let mut second = view(&world, Some(&roster), 1262, worker.owner)?;
    second.owner.primary_key = PublicKey32([0x0F; 32]);
    assert_eq!(
        observe(&mut observer, &second)?,
        Standing::Active { admitted_epoch: 1 }
    );
    assert!(observer.new_work().is_ok());
    let owed = Retained {
        entitlements: 1,
        amount: 19,
        unresolved: None,
    };
    assert_eq!(observer.record().retained, owed);
    let recipient = AccountId::new(worker.owner.bytes())?;
    assert_eq!(
        world.claim(1, worker.worker, recipient, 1262)?,
        ClaimDecision::Payable {
            index: 0,
            amount: 19
        }
    );

    Ok(Rotation {
        directory,
        world,
        worker,
        rotated,
        roster,
        observer,
        superseded,
        current,
        owed,
    })
}

#[test]
fn membership_recovery_a10_rotation_keeps_history_and_old_claims() -> Checked {
    let Rotation {
        directory,
        worker,
        observer,
        superseded,
        ..
    } = rotated("a10")?;
    drop(observer);
    let observer = self::observer(&directory, worker.worker)?;
    assert_eq!(observer.attribute(1, 1, delegate(1, 1))?, superseded);
    assert_eq!(
        observer.attribute(1, 1, delegate(1, 2)).err(),
        Some(ServiceError::WrongGeneration)
    );

    Ok(())
}

#[test]
fn membership_recovery_a10_finalized_revoke_blocks_new_service() -> Checked {
    let Rotation {
        directory,
        mut world,
        worker,
        rotated,
        roster,
        observer,
        superseded,
        current,
        owed,
    } = rotated("a10-revoke")?;
    drop(observer);
    let mut observer = self::observer(&directory, worker.worker)?;
    let participant = Participant::Worker(worker.worker);
    world.edit(|parts, market| {
        parts.admission.administrative_remove(
            &admission_context(market, market.owner_principal, 1270),
            participant,
            1,
            RemovalReason::Security,
        )?;
        parts.workers.replace(&WorkerCurrent {
            state: WorkerState::Revoked,
            ..rotated
        })
    })?;
    assert_eq!(
        world.heartbeat(participant, 1, 2, 2, 1280).err(),
        Some(F08_DELEGATE_REVOKED)
    );
    let mut revoked = view(&world, Some(&roster), 1281, worker.owner)?;
    revoked.owner.primary_key = PublicKey32([0x0F; 32]);
    assert_eq!(
        observe(&mut observer, &revoked)?,
        Standing::Revoked {
            exit: Some(PendingExit {
                epoch: 3,
                cause: ExitCause::Security
            })
        }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::DelegateRevoked)
    );
    assert_eq!(
        observer.admitted_work().err(),
        Some(ServiceError::DelegateRevoked)
    );
    assert_eq!(
        observer.record().generations,
        vec![
            superseded,
            DelegateGeneration {
                revoked: true,
                ..current
            }
        ]
    );
    assert_eq!(observer.attribute(1, 1, delegate(1, 1))?, superseded);
    assert_eq!(observer.record().retained, owed);
    Ok(())
}

/// Epoch-1 member that requested exit, left one replay result unresolved and holds its
/// terminal epoch-1 entitlement, as `observer` recorded it.
struct Drained {
    directory: PathBuf,
    world: World,
    worker: WorkerCurrent,
    evaluators: Vec<EvaluatorRosterEntry>,
    observer: MembershipObserver,
    owed: Retained,
}
fn drained() -> Checked<Drained> {
    let directory = fresh("a12-old")?;
    let j = journey()?;
    let mut world = j.world;
    let worker = j.worker;
    let participant = Participant::Worker(worker.worker);
    let slot = ActorSlot::worker(usize::from(worker.slot))?;
    world.edit(|parts, market| {
        parts
            .admission
            .request_exit(
                &admission_context(market, worker.owner, 1150),
                participant,
                1,
                2,
                ExitReason::Voluntary,
            )
            .map(|_| ())
    })?;
    world.edit(|parts, _| {
        let request = ReplayRequest {
            slot,
            principal: worker.owner,
            authority_version: version()?,
            sequence: 1,
            request_id: RequestId::new([0xE6; 32])?,
            digest: RequestDigest::new([0xE7; 32])?,
            expiry_height: 1400,
        };
        parts
            .replay
            .record_success(
                &request,
                1151,
                &mut parts.revision,
                ResultDigest::new([0xE8; 32])?,
            )
            .map(|_| ())
    })?;
    let unresolved = world.parts()?.replay.actor(slot).and_then(|a| a.last);
    assert_eq!(
        unresolved.map(|r| (r.sequence, r.request_id, r.expiry_height)),
        Some((1, RequestId::new([0xE6; 32])?, 1400))
    );

    let mut observer = observer(&directory, worker.worker)?;
    let draining = view(&world, Some(&j.roster), 1152, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &draining)?,
        Standing::Draining {
            admitted_epoch: Some(1),
            exit: Some(PendingExit {
                epoch: 2,
                cause: ExitCause::Voluntary
            })
        }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );
    assert!(observer.admitted_work().is_ok());
    assert_eq!(
        observer.record().retained,
        Retained {
            entitlements: 0,
            amount: 0,
            unresolved
        }
    );

    world.terminalize(1, j.frozen.roster, &roster_entry(&worker)?, 1200)?;
    let owed = Retained {
        entitlements: 1,
        amount: 19,
        unresolved,
    };
    let terminal = view(&world, Some(&j.roster), 1201, worker.owner)?;
    observe(&mut observer, &terminal)?;
    assert_eq!(observer.record().retained, owed);

    Ok(Drained {
        directory,
        world,
        worker,
        evaluators: j.evaluators,
        observer,
        owed,
    })
}

#[test]
fn membership_recovery_a12_drain_keeps_entitlement_and_unknown_request() -> Checked {
    let Drained {
        directory,
        mut world,
        worker,
        evaluators,
        observer,
        owed,
    } = drained()?;
    let successor_directory = fresh("a12-successor")?;
    drop(observer);
    let mut observer = self::observer(&directory, worker.worker)?;
    assert_eq!(observer.record().retained, owed);
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );

    let staged = world.enroll(7, 1210)?;
    world.fund(19, 1211)?;
    assert_eq!(world.clone().open(1256).err(), Some(F08_RETENTION_BLOCKED));
    let frozen = world.open(epoch_height(ORIGIN, 4, 0))?;
    assert_eq!(
        (frozen.epoch, frozen.previous, frozen.workers),
        (4, Some(1), 1)
    );

    let removed = view(&world, None, 1514, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &removed),
        Err(MembershipError::Service(ServiceError::NotFound))
    );
    assert_eq!(observer.record().retained, owed);
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::StaleAuthority)
    );
    let recipient = AccountId::new(worker.owner.bytes())?;
    assert_eq!(
        world.claim(1, worker.worker, recipient, 1514)?,
        ClaimDecision::Payable {
            index: 0,
            amount: 19
        }
    );

    let successor = world.enroll(6, 1520)?;
    assert_eq!(successor.slot, worker.slot);
    let roster = roster_document(4, &[roster_entry(&staged)?], &evaluators)?;
    let mut fresh_observer = self::observer(&successor_directory, successor.worker)?;
    let inherited = view(&world, Some(&roster), 1522, successor.owner)?;
    assert_eq!(
        observe(&mut fresh_observer, &inherited)?,
        Standing::Staged { effective_epoch: 5 }
    );
    assert_eq!(fresh_observer.record().retained, Retained::default());
    assert_eq!(fresh_observer.record().generations.len(), 1);
    assert_eq!(
        world
            .claim(
                1,
                successor.worker,
                AccountId::new(successor.owner.bytes())?,
                1522
            )
            .err(),
        Some(F06_UNKNOWN_WORKER_ENTITLEMENT)
    );
    Ok(())
}

#[test]
fn membership_recovery_a18_effective_epoch_never_backdates() -> Checked {
    let directory = fresh("a18")?;
    let mut world = World::create(ORIGIN)?;
    let worker = world.register(&worker_record(1)?)?;
    let participant = Participant::Worker(worker.worker);
    let approval = world.approve(participant, worker.owner, worker.delegate, 0, 1064, 1001)?;
    let before = world.admission()?;
    assert_eq!(
        world
            .admit(participant, worker.owner, 1, approval, 1001)
            .err(),
        Some(WRONG_EPOCH)
    );
    assert_eq!(world.admission()?, before);
    assert_eq!(before.enrollments_this_epoch(), 0);
    world.admit(participant, worker.owner, 0, approval, 1001)?;
    let mut evaluators = Vec::new();
    for n in 2..=4u8 {
        evaluators.push(world.evaluator(n, 1000 + u64::from(n))?);
    }
    world.fund(19, 1005)?;
    let opened = world.open(1008)?;

    let staged = world.register(&worker_record(6)?)?;
    let candidate = Participant::Worker(staged.worker);
    assert_eq!(
        world
            .approve(candidate, staged.owner, staged.delegate, 0, 1064, 1020)
            .err(),
        Some(WRONG_EPOCH)
    );
    assert_eq!(
        world
            .approve(candidate, staged.owner, staged.delegate, 3, 1448, 1020)
            .err(),
        Some(WRONG_EPOCH)
    );
    let approval = world.approve(candidate, staged.owner, staged.delegate, 1, 1192, 1020)?;
    world.admit(candidate, staged.owner, 1, approval, 1021)?;

    let mut observer = observer(&directory, staged.worker)?;
    let epoch_zero = roster_document(0, &[roster_entry(&worker)?], &evaluators)?;
    let waiting = view(&world, Some(&epoch_zero), 1022, staged.owner)?;
    assert_eq!(
        observe(&mut observer, &waiting)?,
        Standing::Staged { effective_epoch: 1 }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );

    world.terminalize(0, opened.roster, &roster_entry(&worker)?, 1100)?;
    world.fund(19, 1101)?;
    let frozen = world.open(epoch_height(ORIGIN, 2, 0))?;
    assert_eq!(
        (
            frozen.epoch,
            frozen.previous,
            frozen.skipped,
            frozen.workers
        ),
        (2, Some(0), 1, 2)
    );
    let meta = world.meta(candidate)?;
    assert_eq!(
        (meta.admitted_epoch, meta.immunity_until_epoch),
        (Some(2), 2)
    );
    let epoch_two = roster_document(
        2,
        &[roster_entry(&worker)?, roster_entry(&staged)?],
        &evaluators,
    )?;
    let joined = view(&world, Some(&epoch_two), 1258, staged.owner)?;
    assert_eq!(
        observe(&mut observer, &joined)?,
        Standing::Active { admitted_epoch: 2 }
    );
    Ok(())
}

#[test]
fn membership_recovery_a18_heartbeat_height_zero_is_present() -> Checked {
    let directory = fresh("a18-zero")?;
    let Bootstrap {
        mut world,
        worker,
        evaluators,
    } = bootstrap(0, 0)?;
    let frozen = world.open(0)?;
    assert_eq!((frozen.epoch, frozen.workers, frozen.evaluators), (0, 1, 3));
    let participant = Participant::Worker(worker.worker);
    let silent = world.meta(participant)?;
    assert_eq!(
        (silent.last_heartbeat_epoch, silent.last_heartbeat_height),
        (None, None)
    );
    world.heartbeat(participant, 1, 1, 0, 0)?;
    let beat = world.meta(participant)?;
    assert_eq!(
        (beat.last_heartbeat_epoch, beat.last_heartbeat_height),
        (Some(0), Some(0))
    );

    let roster = roster_document(0, &[roster_entry(&worker)?], &evaluators)?;
    let mut observer = observer(&directory, worker.worker)?;
    let later = view(&world, Some(&roster), 5, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &later)?,
        Standing::Active { admitted_epoch: 0 }
    );
    assert_eq!(
        observer
            .current()
            .and_then(|c| c.meta)
            .map(|m| m.last_heartbeat_height),
        Some(Some(0))
    );
    Ok(())
}

#[test]
fn membership_recovery_a19_pending_evaluator_never_fills_quorum() -> Checked {
    let directory = fresh("a19")?;
    let mut world = World::create(ORIGIN)?;
    let worker = world.enroll(1, 1001)?;
    let mut evaluators = vec![world.evaluator(2, 1002)?, world.evaluator(3, 1003)?];
    let (grant, approval) = world.schedule_evaluator(4, 1004)?;
    world.fund(19, 1005)?;

    let mut observer = observer(&directory, worker.worker)?;
    let pending = view(&world, None, 1006, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &pending)?,
        Standing::Staged { effective_epoch: 0 }
    );
    assert_eq!(
        observer.new_work().err(),
        Some(ServiceError::AdmissionNotEffective)
    );
    assert_eq!(world.clone().open(1008).err(), Some(READINESS_BLOCKED));

    let market = world.header()?;
    let key = SigningKey::from_bytes(&[4; 32]);
    let exact = consent(&market, &grant, 4, approval)?;
    let table = world.admission()?;
    let bytes = world.bytes.clone();
    let refusals = [
        (principal(9)?, signed(&exact, &key)?, F08_OWNER_REQUIRED),
        (
            grant.principal,
            signed(&exact, &SigningKey::from_bytes(&[0x44; 32]))?,
            F08_BAD_CONSENT,
        ),
        (
            grant.principal,
            signed(
                &EvaluatorConsent {
                    enrollment_nonce: [0x45; 32],
                    ..exact
                },
                &key,
            )?,
            F08_BAD_CONSENT,
        ),
        (
            grant.principal,
            signed(
                &EvaluatorConsent {
                    rubric: RubricDigest::new([0x46; 32])?,
                    ..exact
                },
                &key,
            )?,
            F08_BAD_CONSENT,
        ),
        (
            grant.principal,
            signed(
                &EvaluatorConsent {
                    grant_version: 2,
                    ..exact
                },
                &key,
            )?,
            F08_WRONG_GENERATION,
        ),
    ];
    for (caller, payload, refusal) in refusals {
        assert_eq!(
            world
                .accept(&grant, caller, exact.request, &payload, 1006)
                .err(),
            Some(refusal)
        );
        assert_eq!(world.admission()?, table);
        assert_eq!(world.bytes, bytes);
    }

    world.accept(
        &grant,
        grant.principal,
        exact.request,
        &signed(&exact, &key)?,
        1006,
    )?;
    let accepted = world.meta(Participant::Evaluator(grant.evaluator))?;
    assert_eq!((accepted.admitted(), accepted.approval), (true, None));
    evaluators.push(evaluator_entry(&grant));
    let frozen = world.open(1008)?;
    assert_eq!((frozen.epoch, frozen.workers, frozen.evaluators), (0, 1, 3));
    let roster = roster_document(0, &[roster_entry(&worker)?], &evaluators)?;
    let opened = view(&world, Some(&roster), 1010, worker.owner)?;
    assert_eq!(
        observe(&mut observer, &opened)?,
        Standing::Active { admitted_epoch: 0 }
    );
    Ok(())
}

#[test]
fn membership_recovery_store_refuses_corrupt_or_foreign_record() -> Checked {
    let directory = fresh("store")?;
    let j = journey()?;
    let worker = j.worker;
    let mut observer = observer(&directory, worker.worker)?;
    let v = view(&j.world, Some(&j.roster), 1150, worker.owner)?;
    observe(&mut observer, &v)?;
    let recorded = observer.record().clone();
    drop(observer);
    assert_eq!(
        self::observer(&directory, worker.worker)?.record(),
        &recorded
    );

    let other = worker_record(6)?.worker;
    assert_eq!(
        MembershipObserver::open(&directory, subject(other)?).err(),
        Some(MembershipError::CorruptRecord)
    );
    let path = directory.join("membership.record");
    let bytes = fs::read(&path)?;
    let mut truncated = bytes.clone();
    truncated.pop();
    let mut trailing = bytes.clone();
    trailing.push(0);
    let mut flag = bytes;
    flag[8 + 4 * 32] = 2;
    for corrupt in [truncated, trailing, flag] {
        fs::write(&path, &corrupt)?;
        assert_eq!(
            MembershipObserver::open(&directory, subject(worker.worker)?).err(),
            Some(MembershipError::CorruptRecord)
        );
    }
    Ok(())
}
