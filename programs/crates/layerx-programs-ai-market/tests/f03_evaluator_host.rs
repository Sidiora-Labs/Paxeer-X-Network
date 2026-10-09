//! AI.F03-T04 evaluator ordering through the real router and the native host boundary.
//! Every routed selector goes through `dispatch::route` with real envelopes; only selectors the
//! router does not route yet (F05 aggregation, F06 funding and terminalization, F08 admission)
//! and the post-seal `RevokeEvaluator` arm (the router always passes `aggregate_sealed: false`)
//! call their landed transitions directly.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{self as agg, Outcome as Agg, Progress},
    aggregation_codec::{
        decode_current, decode_history, input_digest, AggregationPhase, EpochAggregation,
        HistorySummary, QualityStatus, ReportCommitment, WorkerAggregate,
    },
    codec::{
        self, decode_envelope, decode_result, derive_evaluator, derive_market, derive_worker,
        encode_envelope, CommitScorePayload, Envelope, ReportBody, ResultStatus,
        RevealScorePayload, ScoreVector, REPORT_FIXED_BYTES, REPORT_MAX_BYTES,
    },
    commit_reveal::{self as cr, commitment, Status},
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, CAPACITY, F03_EVALUATOR_CAPACITY,
        F03_NONCANONICAL_VECTOR, F03_NO_SCORES, F03_REPORT_ALREADY_FINAL, F03_SCORE_RANGE,
        F08_BAD_CONSENT, F08_OWNER_REQUIRED, KEY_MISMATCH, NON_CANONICAL, NOT_FOUND, REVOKED,
        ROLE_CONFLICT, UNAUTHORIZED, WRONG_DOMAIN, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::{
        admission::{self as f03, AdmissionReceipt},
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        model::{EvaluatorGrant, GrantStatus, GrantTerms, SignedReport},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader},
    registry_ops::{CallContext, PolicySection, SUSPENDED},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardEpoch,
        RewardLedger, RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvidenceRoot, FrozenBinding, MarketId, MetadataDigest,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Salt32, Signature64, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use layerx_programs_runtime::terminal::{
    decode_terminal_payload, CandidateTerminalOutcome, ExecutionTerminal, TerminalDetail,
};
use layerx_programs_runtime::RefusalClass;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const RELAYER: u8 = 0x70;
const CHALLENGER: u8 = 0x71;
const ORIGIN: u64 = 128;
const METADATA: [u8; 32] = [0x44; 32];
const ROLE_EXPIRY: u64 = 5000;
const SALT: [u8; 32] = [0x5a; 32];
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120),
/// settlement from 1120.
const COMMIT_AT: u64 = 1088;
const SETTLE_AT: u64 = 1120;
/// A height inside epoch 8 (T = 1152) before its commit window.
const NEXT_OPEN: u64 = 1192;
/// The lowest frozen worker.
const LOW: u8 = 0x20;
/// Caller buffers able to hold every routed output: next, scratch, event and result.
const ROUTE: [usize; 4] = [
    MAX_STATE_BYTES,
    dispatch::SCRATCH_BYTES,
    MAX_EVENT_BYTES,
    MAX_RESULT_BYTES,
];

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
fn policy_bytes() -> CodecResult<Vec<u8>> {
    let mut out = vec![0; TASK_POLICY_BYTES];
    policy(1, 3)?.encode(&mut out)?;
    Ok(out)
}
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
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
/// The 160-byte `ScheduleEvaluator` payload at grant and key version 1.
fn schedule_payload(nominee: PrincipalId, nonce: u8, key: PublicKey32, effective: u64) -> Vec<u8> {
    let mut out = nominee.bytes().to_vec();
    out.extend_from_slice(&[nonce; 32]);
    out.extend_from_slice(&[4; 32]);
    out.extend_from_slice(&key.0);
    for value in [1, 1, effective, effective + 32] {
        out.extend_from_slice(&u64::to_be_bytes(value));
    }
    out
}
/// The 74-byte `RevokeEvaluator` payload of grant version 1.
fn revoke_payload(evaluator: EvaluatorId) -> Vec<u8> {
    let mut out = evaluator.as_bytes().to_vec();
    out.extend_from_slice(&1u64.to_be_bytes());
    out.extend_from_slice(&1u16.to_be_bytes());
    out.extend_from_slice(&[0x5e; 32]);
    out
}

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}
fn section_bytes(bytes: &[u8], section: Section) -> CodecResult<Vec<u8>> {
    Ok(decode_shared_state(bytes)?.feature_sections[section.index()].to_vec())
}
fn region_of(bytes: &[u8]) -> CodecResult<EvaluatorRegion> {
    authority::evaluator_region(
        decode_shared_state(bytes)?.feature_sections[Section::IdentityRoster.index()],
    )
}
fn reward_row(bytes: &[u8], epoch: u64) -> CodecResult<RewardEpoch> {
    let section = section_bytes(bytes, Section::SettlementClaims)?;
    decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?)?.row(epoch)
}

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
        Ok(CallContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            principal: self.actor,
            height: at,
        })
    }
}
/// The native envelope of `call` with its payload replaced by raw `payload` bytes the encoder
/// would refuse.
fn spliced(call: &Req, payload: &[u8]) -> CodecResult<Vec<u8>> {
    let encoded = call.encode()?;
    let head = encoded.len() - 1 - call.payload.len() - 4;
    let mut out = encoded[..head].to_vec();
    out.extend_from_slice(
        &u32::try_from(payload.len())
            .map_err(|_| ARITHMETIC)?
            .to_be_bytes(),
    );
    out.extend_from_slice(payload);
    out.push(0);
    Ok(out)
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
/// The 362-byte evaluator consent of `grant` followed by its 64-byte signature by `key`.
fn consent_payload(
    market: &MarketHeader,
    grant: &EvaluatorGrant,
    nonce: u8,
    approval: Digest32,
    key: &SigningKey,
) -> CodecResult<(RequestId, Vec<u8>)> {
    let consent = EvaluatorConsent {
        chain: market.deployment_chain_domain,
        program: market.program_id,
        market: market.market_id,
        evaluator: grant.evaluator,
        owner: grant.principal,
        signing_key: grant.signing_key,
        enrollment_nonce: [nonce; 32],
        rubric: grant.rubric,
        approval_digest: approval,
        request: RequestId::new([nonce; 32])?,
        grant_version: 1,
        key_version: 1,
        effective_epoch: grant.effective_epoch,
        config_version: 1,
        expiry_height: market.origin_height + grant.effective_epoch * 128 + 64,
    };
    let mut signed = [0u8; 362];
    consent.encode(&mut signed)?;
    let mut payload = signed.to_vec();
    payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
    Ok((consent.request, payload))
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

/// What one routed call returned to the host.
enum Sent {
    /// The committed `Ok` result frame.
    Applied(Vec<u8>),
    /// The `AlreadyApplied` result frame; nothing was committed.
    Unchanged(Vec<u8>),
    /// The refusal of the error frame; nothing was committed.
    Refused(ApplicationError),
}
/// The caller buffers of one routed call after `dispatch::route` returned.
struct Composed {
    routed: Routed,
    next: Vec<u8>,
    event: Vec<u8>,
    result: Vec<u8>,
}

/// One market's committed shared state bytes (empty before CREATE) and its next owner
/// sequence.
#[derive(Clone)]
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    /// The real routed F01 CREATE at `origin` (lifecycle REGISTERED).
    fn create(origin: u64) -> CodecResult<Self> {
        let program = ProgramId::new(PROGRAM)?;
        let mut payload = OWNER.to_vec();
        payload.extend_from_slice(&ASSET);
        payload
            .extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
        payload.extend_from_slice(&REFUND);
        payload.push(0);
        payload.extend_from_slice(&policy_bytes()?);
        payload.extend_from_slice(&[16; 32]);
        let call = Req {
            sequence: 1,
            ..req(dispatch::CREATE, PrincipalId::new(OWNER)?, payload)
        };
        let mut world = Self {
            bytes: Vec::new(),
            owner_sequence: 2,
        };
        world.apply(&call, origin)?;
        Ok(world)
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
    /// One `dispatch::route` call over the committed bytes into caller buffers of `sizes`;
    /// nothing is committed.
    fn compose(
        &self,
        actor: PrincipalId,
        encoded: &[u8],
        at: u64,
        sizes: [usize; 4],
    ) -> CodecResult<Composed> {
        let [next_len, scratch_len, event_len, result_len] = sizes;
        let mut next = vec![0; next_len];
        let mut scratch = vec![0; scratch_len];
        let mut event = vec![0; event_len];
        let mut result = vec![0; result_len];
        let current = if self.bytes.is_empty() {
            None
        } else {
            Some(self.bytes.as_slice())
        };
        let routed = dispatch::route(
            &CallContext {
                chain: ChainDomain::new(CHAIN)?,
                program: ProgramId::new(PROGRAM)?,
                principal: actor,
                height: at,
            },
            encoded,
            current,
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?;
        Ok(Composed {
            routed,
            next,
            event,
            result,
        })
    }
    /// One routed call. `Applied` is committed after its frame, event and rebound F01 header
    /// revision agree; anything else commits nothing.
    fn submit(&mut self, actor: PrincipalId, encoded: &[u8], at: u64) -> CodecResult<Sent> {
        let c = self.compose(actor, encoded, at, ROUTE)?;
        match c.routed {
            Routed::Applied {
                operation,
                revision,
                state_len,
                event_len,
                result_len,
            } => {
                let before = if self.bytes.is_empty() {
                    0
                } else {
                    self.revision()?
                };
                let frame = &c.result[..result_len];
                let decoded = decode_result(frame)?;
                assert_eq!(
                    (decoded.status, decoded.error, decoded.revision, revision),
                    (ResultStatus::Ok, None, before + 1, before + 1)
                );
                let next = &c.next[..state_len];
                let state = decode_shared_state(next)?;
                let header = PolicySection::decode(
                    state.feature_sections[Section::PolicyLifecycle.index()],
                )?
                .header;
                assert_eq!(
                    (state.revision, header.state_revision),
                    (revision, revision)
                );
                let mut topic = [0; 64];
                let topic_len = codec::event_topic(operation, &mut topic)?;
                let (named, common, _) =
                    codec::decode_event_frame(&topic[..topic_len], &c.event[..event_len])?;
                assert_eq!(
                    (
                        named,
                        common.revision,
                        common.result,
                        Presence::Present(common.request)
                    ),
                    (operation, revision, decoded.digest, decoded.request)
                );
                self.bytes = next.to_vec();
                Ok(Sent::Applied(frame.to_vec()))
            }
            Routed::Unchanged { result_len } => {
                assert!(c.event.iter().all(|b| *b == 0));
                let frame = c.result[..result_len].to_vec();
                assert_eq!(decode_result(&frame)?.status, ResultStatus::AlreadyApplied);
                Ok(Sent::Unchanged(frame))
            }
            Routed::Refused { result_len } => {
                let decoded = decode_result(&c.result[..result_len])?;
                assert_eq!(decoded.status, ResultStatus::Error);
                decoded.error.map(Sent::Refused).ok_or(NON_CANONICAL)
            }
        }
    }
    /// A routed call that must apply; a refusal is returned as the error.
    fn apply(&mut self, call: &Req, at: u64) -> CodecResult<Vec<u8>> {
        match self.submit(call.actor, &call.encode()?, at)? {
            Sent::Applied(frame) => Ok(frame),
            Sent::Refused(code) => Err(code),
            Sent::Unchanged(_) => Err(NON_CANONICAL),
        }
    }
    /// A routed call that must answer `AlreadyApplied`, leaving the committed bytes.
    fn unchanged(&mut self, call: &Req, at: u64) -> CodecResult<Vec<u8>> {
        let before = self.bytes.clone();
        let Sent::Unchanged(frame) = self.submit(call.actor, &call.encode()?, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.bytes, before);
        Ok(frame)
    }
    /// A routed raw envelope that must be refused, leaving the committed bytes.
    fn refused_raw(
        &mut self,
        actor: PrincipalId,
        encoded: &[u8],
        at: u64,
    ) -> CodecResult<ApplicationError> {
        let before = self.bytes.clone();
        let Sent::Refused(code) = self.submit(actor, encoded, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.bytes, before);
        Ok(code)
    }
    fn refused(&mut self, call: &Req, at: u64) -> CodecResult<ApplicationError> {
        self.refused_raw(call.actor, &call.encode()?, at)
    }
    /// A routed owner-authorized F01 registry operation (the owner role sequence advances).
    fn owner_op(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let call = Req {
            config: self.section()?.header.active_config_version,
            sequence: self.owner_sequence,
            request: tag(0x20, 0, self.owner_sequence),
            ..req(operation, PrincipalId::new(OWNER)?, payload)
        };
        self.apply(&call, at)?;
        self.owner_sequence += 1;
        Ok(())
    }
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
    }
    fn suspend(&mut self, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&[0x50; 32]);
        self.owner_op(dispatch::SUSPEND, payload, at)
    }
    /// The trial opening of the clock epoch of `at` over the committed bytes.
    fn preview(&self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)
    }
    /// The routed permissionless `OPEN_EPOCH` of the clock epoch of `at`.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let frozen = self.preview(at)?;
        self.apply(&open_call(&frozen)?, at)?;
        Ok(frozen)
    }
    /// The routed permissionless `ADVANCE_ACTIVATION`; the lifecycle becomes ACTIVE.
    fn advance(&mut self, at: u64) -> TestResult {
        let section = self.section()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&section.header.activation_epoch.to_be_bytes());
        let call = Req {
            config: section.header.active_config_version,
            request: [0x5f; 32],
            ..req(dispatch::ADVANCE_ACTIVATION, principal(KEEPER)?, payload)
        };
        self.apply(&call, at)?;
        Ok(())
    }
}
fn open_call(frozen: &Frozen) -> CodecResult<Req> {
    Ok(Req {
        epoch: frozen.epoch,
        config: frozen.config.get(),
        roster: Presence::Present(frozen.roster),
        request: tag(0x60, 0, frozen.epoch),
        ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
    })
}

/// Unrouted worker, evaluator, funding and settlement producers of the market journey.
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
    /// F03 nomination accepted through the signed F08 evaluator consent of its key; the
    /// evaluator replay slot `n - 2` is bound to its owner.
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let key = evaluator_key(n);
            let participant =
                Participant::Evaluator(derive_evaluator(market.market_id, owner, [n; 32])?);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                owner,
                public(&key),
                at,
            )?;
            let grant = grant(market, owner, n, public(&key), effective)?;
            let (request, payload) = consent_payload(market, &grant, n, digest, &key)?;
            admit_evaluator(
                &mut parts.admission,
                &ctx(market, owner, at),
                &grant,
                request,
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
    /// Real F06 `TerminalizeRewards` of `frozen`, weight 5 for every frozen worker.
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
    /// Restores workers `LOW + 1 .. LOW + workers` and evaluators 5..=9 from the real admitted
    /// memberships of worker `first` and evaluator 2 (the F08 budget admits four enrollments
    /// per epoch).
    fn restore(
        &mut self,
        first: WorkerRosterEntry,
        workers: u8,
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
            for n in LOW + 1..LOW + workers {
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
    /// A grant of `who` that would activate at the next opening, with the admitted F08
    /// membership of evaluator 2 copied to it: a conflicting principal in the live roster.
    fn conflicting_grant(&mut self, who: PrincipalId, nonce: u8) -> TestResult {
        self.edit(|parts, market| {
            let template = parts
                .admission
                .get(Participant::Evaluator(derive_evaluator(
                    market.market_id,
                    principal(2)?,
                    [2; 32],
                )?))
                .ok_or(NOT_FOUND)?;
            let grant = grant(market, who, nonce, public(&evaluator_key(nonce)), 0)?;
            parts.admission.insert(AdmissionMeta {
                participant: Participant::Evaluator(grant.evaluator),
                owner: who,
                ..template
            })?;
            parts.insert_grant(grant, nonce)
        })
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
/// Epoch 7 (T = 1024): worker `LOW` and evaluators 2..=4 enrolled before activation, epoch 6
/// opened at 896, worker `LOW + 1` and evaluators 5..=7 enrolled in epoch 6, epoch 6
/// terminalized, epoch 7 opened at 1024, its empty task set and every evidence root sealed.
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
        (7, 2, 6)
    );
    m.seal(&[2, 3, 4, 5, 6, 7], COMMIT_AT)?;
    Ok(m)
}
/// Epoch 6 (T = 896) with `workers` frozen workers and evaluators 2..=9 frozen.
fn crowded(workers: u8) -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let first = world.enroll(LOW, 130)?;
    for n in 2..=4 {
        world.evaluator(n, 129 + u64::from(n))?;
    }
    let mut roster = world.restore(first, workers, 134)?;
    world.schedule(6, 135)?;
    world.fund(500, 136)?;
    let frozen = world.open(896)?;
    assert_eq!(
        (frozen.epoch, frozen.workers, frozen.evaluators),
        (6, workers, 8)
    );
    world.advance(897)?;
    roster.sort_by_key(|w| w.worker);
    let header = world.parts()?.market()?;
    let mut m = Market {
        world,
        frozen,
        header,
        workers: roster,
    };
    m.seal(&[2, 3, 4, 5, 6, 7, 8, 9], 960)?;
    Ok(m)
}

/// Identities, bindings, reports and the routed task-set and evidence seals.
impl Market {
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
    fn binding(&self, n: u8) -> CodecResult<EvaluatorBinding> {
        Ok(EvaluatorBinding {
            frozen: self.frozen_binding(),
            evaluator: self.evaluator(n)?,
            grant: version()?,
            key_version: version()?,
        })
    }
    fn body(binding: EvaluatorBinding, n: u8, scores: &[u8]) -> CodecResult<ReportBody<'_>> {
        Ok(ReportBody {
            binding,
            evidence: EvidenceRoot::new([root(n); 32])?,
            scores: ScoreVector::Encoded(scores),
        })
    }
    /// Canonical entries scoring the frozen workers at ascending roster `indices`.
    fn scores(&self, pairs: &[(usize, u32)]) -> Vec<u8> {
        let pairs = pairs
            .iter()
            .map(|&(index, score)| (self.workers[index].worker, score))
            .collect::<Vec<_>>();
        entries(&pairs)
    }
    fn all(&self, score: u32) -> Vec<u8> {
        let pairs = (0..self.workers.len())
            .map(|index| (index, score))
            .collect::<Vec<_>>();
        self.scores(&pairs)
    }
    fn slot(&self, n: u8, at: u64) -> CodecResult<cr::Slot> {
        cr::slot(&self.world.bytes, self.evaluator(n)?, at)
    }
    fn admitted(&self, n: u8) -> CodecResult<Option<AdmissionReceipt>> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?.map(|r| r.receipt))
    }
    /// The evidence root of evaluator `n`'s admitted row.
    fn admitted_evidence(&self, n: u8) -> CodecResult<EvidenceRoot> {
        let state = decode_shared_state(&self.world.bytes)?;
        let row = f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?
            .ok_or(NOT_FOUND)?;
        Ok(row.signed()?.body.evidence)
    }
    /// Routed keeper `SEAL_TASK_SET`, then routed `SealEvidence` of each of `evaluators` under
    /// evaluator sequence 1.
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
        self.world.apply(&call, at)?;
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
            self.world.apply(&call, at)?;
        }
        Ok(())
    }
}

fn entries(pairs: &[(WorkerId, u32)]) -> Vec<u8> {
    let mut out = Vec::new();
    for (worker, score) in pairs {
        out.extend_from_slice(worker.as_bytes());
        out.extend_from_slice(&score.to_be_bytes());
    }
    out
}
fn sign<'a>(body: ReportBody<'a>, key: &SigningKey) -> CodecResult<SignedReport<'a>> {
    let digest = codec::attestation_digest(codec::report_digest(&body)?)?;
    Ok(SignedReport {
        body,
        signature: Signature64(key.sign(&digest.bytes()).to_bytes()),
    })
}
fn commitment_of(body: &ReportBody<'_>, salt: [u8; 32]) -> CodecResult<CommitmentDigest> {
    codec::commitment_digest(
        &body.binding,
        codec::report_digest(body)?,
        Salt32::new(salt)?,
    )
}
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
/// `report` with its frozen binding replaced; the original signature is retained.
fn rebound<'a>(report: &SignedReport<'a>, frozen: FrozenBinding) -> SignedReport<'a> {
    SignedReport {
        body: ReportBody {
            binding: EvaluatorBinding {
                frozen,
                ..report.body.binding
            },
            ..report.body
        },
        signature: report.signature,
    }
}
fn allegation(receipt: &AdmissionReceipt, category: u8, evidence: u8) -> Vec<u8> {
    let mut out = receipt.evaluator.as_bytes().to_vec();
    out.extend_from_slice(receipt.report.as_bytes());
    out.push(category);
    out.extend_from_slice(&[evidence; 32]);
    out
}

/// Routed `CommitScore`, `RevealScore`, `ChallengeAssessment` and F03 owner authority calls.
impl Market {
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
    /// Evaluator `n` commits `C` of `report` and `SALT` at evaluator sequence 2.
    fn commit(&mut self, n: u8, report: &SignedReport<'_>, at: u64) -> TestResult {
        let c = commitment_of(&report.body, SALT)?;
        let call = Market::commit_call(n, &report.body.binding, c, 2, self.start() + 80)?;
        self.world.apply(&call, at)?;
        let slot = self.slot(n, at)?;
        let record = slot.commit.ok_or(NOT_FOUND)?;
        assert_eq!(
            (slot.status, record.commitment, record.height),
            (Status::Committed, c, at)
        );
        Ok(())
    }
    /// Evaluator `n` reveals `report` and `SALT` at evaluator sequence 3; the `Ok` frame
    /// carries the admitted receipt.
    fn reveal(
        &mut self,
        n: u8,
        report: &SignedReport<'_>,
        at: u64,
    ) -> CodecResult<AdmissionReceipt> {
        let call = Market::reveal_call(n, report, SALT, 3, self.start() + 96)?;
        let frame = self.world.apply(&call, at)?;
        let receipt = self.admitted(n)?.ok_or(NOT_FOUND)?;
        let decoded = decode_result(&frame)?;
        assert_eq!(decoded.payload, receipt.payload()?.as_slice());
        assert_eq!(decoded.digest, receipt.result()?);
        assert_eq!(receipt.report, codec::report_digest(&report.body)?);
        Ok(receipt)
    }
    fn signed<'a>(&self, n: u8, scores: &'a [u8]) -> CodecResult<SignedReport<'a>> {
        sign(
            Market::body(self.binding(n)?, n, scores)?,
            &evaluator_key(n),
        )
    }
    /// Every evaluator of `plan` commits at T+64; they reveal in plan order from T+80.
    fn admit(&mut self, plan: &[(u8, Vec<u8>)]) -> TestResult {
        let start = self.start();
        for (n, scores) in plan {
            let report = self.signed(*n, scores)?;
            self.commit(*n, &report, start + 64)?;
        }
        for (height, (n, scores)) in (start + 80..).zip(plan) {
            let report = self.signed(*n, scores)?;
            self.reveal(*n, &report, height)?;
        }
        Ok(())
    }
    /// A reveal of evaluator `n` at sequence 3 whose report carries raw `scores` the encoder
    /// refuses; it must be refused before any slot, row, event or state change.
    fn malformed_reveal(
        &mut self,
        n: u8,
        report: &SignedReport<'_>,
        scores: &[u8],
        at: u64,
    ) -> CodecResult<ApplicationError> {
        let count = u16::try_from(scores.len() / 36).map_err(|_| ARITHMETIC)?;
        let body = raw_body(&report.body, count, scores)?;
        let payload = raw_reveal(&body, &report.signature.0, &SALT)?;
        let call = Market::reveal_call(n, report, SALT, 3, self.start() + 96)?;
        let encoded = spliced(&call, &payload)?;
        let slot = self.slot(n, at)?;
        let composed = self.world.compose(call.actor, &encoded, at, ROUTE)?;
        assert!(matches!(composed.routed, Routed::Refused { .. }));
        assert!(composed.event.iter().all(|b| *b == 0));
        let code = self.world.refused_raw(call.actor, &encoded, at)?;
        assert_eq!(self.slot(n, at)?, slot);
        assert_eq!(self.admitted(n)?, None);
        Ok(code)
    }
    /// The permissionless native `ChallengeAssessment` of the challenger with the derived id.
    fn challenge_call(&self, payload: Vec<u8>) -> CodecResult<Req> {
        let actor = principal(CHALLENGER)?;
        let mut preimage = CHAIN.to_vec();
        preimage.extend_from_slice(&PROGRAM);
        preimage.extend_from_slice(self.header.market_id.as_bytes());
        preimage.extend_from_slice(&self.frozen.epoch.to_be_bytes());
        preimage.extend_from_slice(&payload);
        preimage.extend_from_slice(actor.as_bytes());
        let id = codec::domain_hash("PAXAI/evaluator-challenge/v1", &preimage)?;
        Ok(Req {
            request: id.bytes(),
            ..bound(
                dispatch::ChallengeAssessment,
                actor,
                &self.frozen_binding(),
                payload,
            )
        })
    }
    /// An owner F03 authority request at the current owner sequence, bound to the opened
    /// epoch.
    fn owner_call(&self, operation: Operation, payload: Vec<u8>) -> CodecResult<Req> {
        let sequence = self.world.owner_sequence;
        Ok(Req {
            sequence,
            request: tag(0x30, 0, sequence),
            ..bound(
                operation,
                PrincipalId::new(OWNER)?,
                &self.frozen_binding(),
                payload,
            )
        })
    }
    /// One routed owner F03 authority operation.
    fn owner_authority(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let call = self.owner_call(operation, payload)?;
        self.world.apply(&call, at)?;
        self.world.owner_sequence += 1;
        Ok(())
    }
    /// Real F03 `RevokeEvaluator` of evaluator `n`. The host derives `aggregate_sealed` from
    /// the committed F05 progress: unsealed revocations are routed; a sealed one calls
    /// `authority::apply` directly because the routed arm always passes `false`.
    fn revoke(&mut self, n: u8, at: u64) -> TestResult {
        let call = self.owner_call(
            dispatch::RevokeEvaluator,
            revoke_payload(self.evaluator(n)?),
        )?;
        if self.progress()?.phase == AggregationPhase::Unsealed {
            self.world.apply(&call, at)?;
        } else {
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
                    aggregate_sealed: true,
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
        }
        self.world.owner_sequence += 1;
        Ok(())
    }
    /// Routed `RotateEvaluatorKey` of evaluator `n` to key version 2, effective next epoch.
    fn rotate(&mut self, n: u8, at: u64) -> TestResult {
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&public(&rotated_key(n)).0);
        for value in [2, 1, self.frozen.epoch + 1] {
            payload.extend_from_slice(&u64::to_be_bytes(value));
        }
        self.owner_authority(dispatch::RotateEvaluatorKey, payload, at)
    }
}

fn input_of(progress: &Progress) -> CodecResult<Digest32> {
    match progress.input {
        Presence::Present(input) => Ok(input),
        Presence::Absent => Err(NOT_FOUND),
    }
}
fn fields(output: WorkerAggregate) -> (u8, QualityStatus, u32, u32) {
    (
        output.support(),
        output.status(),
        output.score().get(),
        output.weight(),
    )
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

/// The unrouted F05 aggregation consumer through `aggregation::apply`.
impl Market {
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
        assert_eq!(self.progress()?, progress);
        Ok(progress)
    }
    fn progress(&self) -> CodecResult<Progress> {
        agg::progress(&self.world.bytes)
    }
    fn begin(&mut self, at: u64) -> CodecResult<Progress> {
        let call = self.aggregation_call(dispatch::BeginAggregation, Vec::new(), 0)?;
        let progress = self.aggregate(&call, at)?;
        assert_eq!(progress.phase, AggregationPhase::Processing);
        Ok(progress)
    }
    /// Every remaining Process chunk, then Finalize.
    fn complete(&mut self, at: u64) -> CodecResult<Progress> {
        let mut progress = self.progress()?;
        let input = input_of(&progress)?;
        while progress.cursor < progress.worker_count {
            let mut payload = input.as_bytes().to_vec();
            payload.extend_from_slice(&progress.cursor.to_be_bytes());
            let call =
                self.aggregation_call(dispatch::ProcessAggregation, payload, progress.cursor)?;
            progress = self.aggregate(&call, at)?;
        }
        let call = self.aggregation_call(
            dispatch::FinalizeAggregation,
            input.as_bytes().to_vec(),
            u16::MAX,
        )?;
        let done = self.aggregate(&call, at)?;
        assert_eq!(done.phase, AggregationPhase::Terminal);
        Ok(done)
    }
    fn settle(&mut self, at: u64) -> CodecResult<Progress> {
        self.begin(at)?;
        self.complete(at)
    }
    fn tail(&self) -> CodecResult<(Vec<u8>, Vec<u8>)> {
        let section = section_bytes(&self.world.bytes, Section::SettlementClaims)?;
        let tail = section.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
        let (record, history) = tail.split_at(record_len(tail)?);
        Ok((record.to_vec(), history.to_vec()))
    }
    fn outputs(&self) -> CodecResult<Vec<WorkerAggregate>> {
        let (record, _) = self.tail()?;
        let current = decode_current(&record, self.frozen_binding(), &self.workers)?;
        (0..current.output_count())
            .map(|i| current.output(i))
            .collect()
    }
    fn history(&self) -> CodecResult<Vec<HistorySummary>> {
        let (_, history) = self.tail()?;
        let decoded = decode_history(&history)?;
        (0..decoded.len()).map(|i| decoded.entry(i)).collect()
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
}

/// AI.F03-A01 and AI.F03-A02 through the routed reveal path: accepted scores are unchanged,
/// the explicit zero is a vote, an omitted worker has no vote and the F05 input differs; no
/// reveal moves any F06 value.
#[test]
fn f03_a01_a02_explicit_zero_is_a_vote_and_an_omitted_worker_is_none() -> TestResult {
    let base = market()?;
    let rewards = section_bytes(&base.world.bytes, Section::SettlementClaims)?;
    let scored = base.scores(&[(0, 700_000), (1, 0)]);
    let omitted = base.scores(&[(0, 700_000)]);
    let mut full = base.clone();
    let mut partial = base;
    full.admit(&(2..=4).map(|n| (n, scored.clone())).collect::<Vec<_>>())?;
    partial.admit(&(2..=4).map(|n| (n, omitted.clone())).collect::<Vec<_>>())?;
    for (m, scores) in [(&full, &scored), (&partial, &omitted)] {
        for n in 2..=4 {
            let receipt = m.admitted(n)?.ok_or(NOT_FOUND)?;
            let expected = Market::body(m.binding(n)?, n, scores)?;
            assert_eq!(receipt.report, codec::report_digest(&expected)?);
            assert_eq!(m.admitted_evidence(n)?, EvidenceRoot::new([root(n); 32])?);
        }
        assert_eq!(
            section_bytes(&m.world.bytes, Section::SettlementClaims)?,
            rewards
        );
    }
    let with_zero = full.begin(SETTLE_AT)?;
    let without = partial.begin(SETTLE_AT)?;
    assert_eq!(
        with_zero.input,
        Presence::Present(full.expected_input(&[2, 3, 4], SETTLE_AT)?)
    );
    assert_eq!(
        without.input,
        Presence::Present(partial.expected_input(&[2, 3, 4], SETTLE_AT)?)
    );
    assert_ne!(with_zero.input, without.input);
    full.complete(SETTLE_AT)?;
    partial.complete(SETTLE_AT)?;
    let (scored_outputs, omitted_outputs) = (full.outputs()?, partial.outputs()?);
    for outputs in [&scored_outputs, &omitted_outputs] {
        assert_eq!(
            fields(outputs[0]),
            (3, QualityStatus::ScoredPositive, 700_000, 700_000)
        );
    }
    let (zero, none) = (scored_outputs[1], omitted_outputs[1]);
    assert_eq!(fields(zero), (3, QualityStatus::ScoredZero, 0, 0));
    assert_eq!(fields(none), (0, QualityStatus::InsufficientQuorum, 0, 0));
    assert_eq!(
        (zero.quality(), none.quality()),
        (Presence::Present(zero.score()), Presence::Absent)
    );
    Ok(())
}

/// AI.F03-A03: unsorted and duplicate vectors are refused by the routed reveal before any slot
/// use or event; the same sequence then reveals the valid report.
#[test]
fn f03_a03_unsorted_and_duplicate_vectors_consume_no_slot() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let (first, second) = (m.workers[0].worker, m.workers[1].worker);
    let valid = m.scores(&[(0, 6), (1, 5)]);
    let report = m.signed(2, &valid)?;
    m.commit(2, &report, start + 64)?;
    let mut codes = Vec::new();
    for scores in [
        entries(&[(second, 5), (first, 6)]),
        entries(&[(first, 6), (first, 6)]),
    ] {
        codes.push(m.malformed_reveal(2, &report, &scores, start + 80)?);
    }
    let receipt = m.reveal(2, &report, start + 81)?;
    assert_eq!(receipt.activity, 3);
    assert_eq!(codes, [F03_NONCANONICAL_VECTOR, F03_NONCANONICAL_VECTOR]);
    Ok(())
}

/// AI.F03-A04 score and count bounds: the maximum score and a one-entry vector are admitted,
/// zero is admitted, an out-of-range score and 33 entries are refused without mutation.
#[test]
fn f03_a04_score_and_count_bounds() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let top = m.scores(&[(0, 1_000_000)]);
    let zero = m.scores(&[(1, 0)]);
    let high = m.signed(2, &top)?;
    let low = m.signed(3, &zero)?;
    m.commit(2, &high, start + 64)?;
    m.commit(3, &low, start + 64)?;
    let over = entries(&[(m.workers[0].worker, 1_000_001)]);
    assert_eq!(
        m.malformed_reveal(2, &high, &over, start + 80)?,
        F03_SCORE_RANGE
    );
    let mut long = Vec::new();
    for i in 1..=33u8 {
        long.push((WorkerId::new([i; 32])?, 1));
    }
    assert_eq!(
        m.malformed_reveal(2, &high, &entries(&long), start + 80)?,
        NON_CANONICAL
    );
    m.reveal(2, &high, start + 81)?;
    m.reveal(3, &low, start + 82)?;
    Ok(())
}

/// AI.F03-A04: a zero-length vector returns `NO_SCORES` on the routed reveal path.
#[test]
fn f03_a04_empty_vector_returns_no_scores() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let valid = m.scores(&[(0, 1)]);
    let report = m.signed(2, &valid)?;
    m.commit(2, &report, start + 64)?;
    let code = m.malformed_reveal(2, &report, &[], start + 80)?;
    m.reveal(2, &report, start + 81)?;
    assert_eq!(code, F03_NO_SCORES);
    Ok(())
}

/// AI.F03-A05: changing only the chain domain of the binding while retaining the signature
/// is refused `WRONG_DOMAIN`, whether the envelope follows the binding or not.
#[test]
fn f03_a05_chain_change_keeps_the_signature_and_refuses_wrong_domain() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 500_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    let frozen = report.body.binding.frozen;
    let moved = rebound(
        &report,
        FrozenBinding {
            chain: ChainDomain::new([0x56; 32])?,
            ..frozen
        },
    );
    assert_eq!(moved.signature, report.signature);
    let slot = m.slot(2, start + 80)?;
    let follows = Market::reveal_call(2, &moved, SALT, 3, start + 96)?;
    assert_eq!(m.world.refused(&follows, start + 80)?, WRONG_DOMAIN);
    let original = Market::reveal_call(2, &report, SALT, 3, start + 96)?;
    let inner = spliced(&original, &reveal_payload(&moved, SALT)?)?;
    assert_eq!(
        m.world.refused_raw(original.actor, &inner, start + 80)?,
        WRONG_DOMAIN
    );
    assert_eq!(m.slot(2, start + 80)?, slot);
    m.reveal(2, &report, start + 81)?;
    Ok(())
}

/// AI.F03-A05: the same `WRONG_DOMAIN` refusal applies to another Program or market domain.
#[test]
fn f03_a05_program_and_market_changes_refuse_wrong_domain() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 500_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    let frozen = report.body.binding.frozen;
    let mut codes = Vec::new();
    for changed in [
        FrozenBinding {
            program: ProgramId::new([0x62; 32])?,
            ..frozen
        },
        FrozenBinding {
            market: MarketId::new([0x63; 32])?,
            ..frozen
        },
    ] {
        let call = Market::reveal_call(2, &rebound(&report, changed), SALT, 3, start + 96)?;
        codes.push(m.world.refused(&call, start + 80)?);
    }
    m.reveal(2, &report, start + 81)?;
    assert_eq!(codes, [WRONG_DOMAIN, WRONG_DOMAIN]);
    Ok(())
}

/// Epoch 7 after the routed pre-seal orderings: evaluator 7 revoked before its commit,
/// evaluator 6 revoked between its commit and reveal, evaluators 2..=5 admitted and evaluator 3
/// rotated to key version 2 for epoch 8.
fn revoked_before_seal() -> CodecResult<(Market, Vec<u8>)> {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 400_000), (1, 600_000)]);
    m.revoke(7, start + 64)?;
    let late = m.signed(7, &scores)?;
    let c = commitment_of(&late.body, SALT)?;
    let call = Market::commit_call(7, &late.body.binding, c, 2, start + 80)?;
    assert_eq!(m.world.refused(&call, start + 65)?, REVOKED);
    let pending = m.signed(6, &scores)?;
    m.commit(6, &pending, start + 65)?;
    m.admit(&(2..=5).map(|n| (n, scores.clone())).collect::<Vec<_>>())?;
    m.revoke(6, start + 86)?;
    let call = Market::reveal_call(6, &pending, SALT, 3, ROLE_EXPIRY)?;
    assert_eq!(m.world.refused(&call, start + 87)?, REVOKED);
    m.rotate(3, start + 88)?;
    Ok((m, scores))
}
/// A revocation before the aggregate seal excludes the admitted row from the F05 input while
/// its acceptance history and evidence digest remain.
fn preseal_excludes(m: &Market) -> TestResult {
    let mut before_seal = m.clone();
    let receipt = before_seal.admitted(4)?.ok_or(NOT_FOUND)?;
    before_seal.revoke(4, m.start() + 89)?;
    assert!(region_of(&before_seal.world.bytes)?.excluded(m.evaluator(4)?));
    let sealed = before_seal.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(before_seal.expected_input(&[2, 3, 5], SETTLE_AT)?)
    );
    before_seal.complete(SETTLE_AT)?;
    assert_eq!(fields(before_seal.outputs()?[0]).0, 3);
    assert_eq!(before_seal.admitted(4)?, Some(receipt));
    assert_eq!(
        before_seal.admitted_evidence(4)?,
        EvidenceRoot::new([root(4); 32])?
    );
    Ok(())
}
/// A revocation after the aggregate seal leaves the sealed set, progress and every F06 value
/// unchanged; the epoch completes over all four sealed rows.
fn postseal_keeps_the_sealed_set(m: &mut Market) -> CodecResult<Progress> {
    let sealed = m.begin(SETTLE_AT)?;
    assert_eq!(
        sealed.input,
        Presence::Present(m.expected_input(&[2, 3, 4, 5], SETTLE_AT)?)
    );
    let settlement = section_bytes(&m.world.bytes, Section::SettlementClaims)?;
    m.revoke(4, SETTLE_AT + 1)?;
    let region = region_of(&m.world.bytes)?;
    assert!(!region.excluded(m.evaluator(4)?));
    assert_eq!(
        region.get(m.evaluator(4)?).map(|r| r.grant.status),
        Some(GrantStatus::Revoked)
    );
    assert_eq!(m.progress()?, sealed);
    assert_eq!(
        section_bytes(&m.world.bytes, Section::SettlementClaims)?,
        settlement
    );
    let done = m.complete(SETTLE_AT + 2)?;
    assert_eq!(done.input, sealed.input);
    assert_eq!(fields(m.outputs()?[0]).0, 4);
    assert!(m.admitted(4)?.is_some());
    Ok(done)
}

/// AI.F03-A07 and AI.F03-A08 over the routed revocation, reveal and seal orderings.
#[test]
fn f03_a07_a08_revocation_and_seal_orderings() -> TestResult {
    let (mut m, _) = revoked_before_seal()?;
    preseal_excludes(&m)?;
    postseal_keeps_the_sealed_set(&mut m)?;
    Ok(())
}

/// AI.F04-A12 unchanged: rotation in epoch 7 affects epoch 8 only, revocation blocks new
/// commit/reveal at once, pre-seal revocation excludes while retaining acceptance history,
/// post-seal revocation leaves the sealed set unchanged, and neither the same B/report/salt nor
/// a historical exact retry recreates epoch 7 after rollover.
#[test]
fn f04_a12_rotation_revocation_and_rollover() -> TestResult {
    let (mut m, scores) = revoked_before_seal()?;
    preseal_excludes(&m)?;
    let old = m.signed(2, &scores)?;
    let old_reveal = Market::reveal_call(2, &old, SALT, 3, ROLE_EXPIRY)?;
    let c = commitment_of(&old.body, SALT)?;
    let old_commit = Market::commit_call(2, &old.body.binding, c, 4, ROLE_EXPIRY)?;
    let done = postseal_keeps_the_sealed_set(&mut m)?;
    m.frozen = m.world.open(NEXT_OPEN)?;
    assert_eq!(m.frozen.epoch, 8);
    let region = region_of(&m.world.bytes)?;
    let snapshot = region.snapshot().ok_or(NOT_FOUND)?;
    assert_eq!((snapshot.epoch, snapshot.len()), (8, 3));
    for n in [4, 6, 7] {
        assert!(snapshot.get(m.evaluator(n)?).is_none());
    }
    for (n, key_version, key) in [
        (2, 1, public(&evaluator_key(2))),
        (3, 2, public(&rotated_key(3))),
        (5, 1, public(&evaluator_key(5))),
    ] {
        let frozen = snapshot.get(m.evaluator(n)?).ok_or(NOT_FOUND)?;
        assert_eq!(
            (frozen.entry.key_version.get(), frozen.entry.public_key),
            (key_version, key)
        );
    }
    assert_eq!(m.world.refused(&old_reveal, NEXT_OPEN + 1)?, WRONG_EPOCH);
    assert_eq!(m.world.refused(&old_commit, NEXT_OPEN + 1)?, WRONG_EPOCH);
    let history = m.history()?;
    assert_eq!(history.len(), 1);
    assert_eq!(
        (history[0].epoch, Presence::Present(history[0].root)),
        (7, done.root)
    );
    let window = m.start() + 64;
    let next = m.scores(&[(0, 500_000)]);
    let stale = m.binding(3)?;
    let current = EvaluatorBinding {
        key_version: Version::new(2)?,
        ..stale
    };
    for (binding, refusal) in [(stale, Some(KEY_MISMATCH)), (current, None)] {
        let c = commitment_of(&Market::body(binding, 3, &next)?, SALT)?;
        let call = Market::commit_call(3, &binding, c, 4, m.start() + 80)?;
        match refusal {
            Some(code) => assert_eq!(m.world.refused(&call, window)?, code),
            None => {
                m.world.apply(&call, window)?;
            }
        }
    }
    assert_eq!(m.slot(3, window)?.status, Status::Committed);
    Ok(())
}

/// AI.F03-A09 and AI.F03-A10: a worker or market-owner principal is refused as an evaluator by
/// the routed schedule and by the routed opening, which creates no snapshot or reservation.
#[test]
fn f03_a09_a10_role_conflicts_refuse_scheduling_and_opening() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let conflicts = [(principal(LOW)?, 0x31), (PrincipalId::new(OWNER)?, 0x32)];
    for (who, nonce) in conflicts {
        let payload = schedule_payload(who, nonce, public(&evaluator_key(nonce)), 8);
        let call = m.owner_call(dispatch::ScheduleEvaluator, payload)?;
        assert_eq!(m.world.refused(&call, start + 10)?, ROLE_CONFLICT);
    }
    m.settle(SETTLE_AT)?;
    let clean = m.world.preview(NEXT_OPEN)?;
    assert_eq!(clean.epoch, 8);
    for (who, nonce) in conflicts {
        let mut conflicted = m.clone();
        conflicted.world.conflicting_grant(who, nonce)?;
        let call = open_call(&clean)?;
        assert_eq!(conflicted.world.refused(&call, NEXT_OPEN)?, ROLE_CONFLICT);
        let region = region_of(&conflicted.world.bytes)?;
        assert_eq!(region.snapshot().map(|s| s.epoch), Some(7));
        assert_eq!(
            reward_row(&conflicted.world.bytes, 8).err(),
            Some(NOT_FOUND)
        );
    }
    Ok(())
}

/// AI.F03-A09: a corrupted live roster in which a frozen worker is owned by the evaluator
/// principal fails report admission on the routed reveal.
#[test]
fn f03_a09_corrupted_conflicting_roster_fails_report_admission() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 500_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    let worker = derive_worker(m.header.market_id, principal(LOW)?, [LOW; 32])?;
    let evaluator_owner = principal(2)?;
    m.world.edit(|parts, _| {
        let mut record = parts.workers.get(worker).ok_or(NOT_FOUND)?;
        record.owner = evaluator_owner;
        parts.workers.replace(&record)
    })?;
    let call = Market::reveal_call(2, &report, SALT, 3, start + 96)?;
    assert_eq!(m.world.refused(&call, start + 80)?, ROLE_CONFLICT);
    let slot = m.slot(2, start + 80)?;
    assert_eq!((slot.status, slot.reveal), (Status::Committed, None));
    Ok(())
}

/// AI.F03-A14: an exact retransmission, also after a restart from the finalized value,
/// recovers the original result without a second row, event or revision, and the slot query
/// recovers the receipt.
#[test]
fn f03_a14_retransmission_and_restart_recover_the_receipt() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 500_000), (1, 250_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    let call = Market::reveal_call(2, &report, SALT, 3, start + 96)?;
    let frame = m.world.apply(&call, start + 80)?;
    let applied = decode_result(&frame)?;
    let receipt = m.admitted(2)?.ok_or(NOT_FOUND)?;
    assert_eq!(applied.payload, receipt.payload()?.as_slice());
    let committed = m.world.bytes.clone();
    let restarted = World {
        bytes: encode(&decode_shared_state(&committed)?)?,
        owner_sequence: m.world.owner_sequence,
    };
    assert_eq!(restarted.bytes, committed);
    for mut world in [m.world.clone(), restarted] {
        let retry = world.unchanged(&call, start + 90)?;
        let retained = decode_result(&retry)?;
        assert_eq!(
            (
                retained.status,
                retained.error,
                retained.request,
                retained.revision
            ),
            (
                ResultStatus::AlreadyApplied,
                None,
                applied.request,
                applied.revision
            )
        );
        assert_eq!(retained.payload, applied.digest.bytes().as_slice());
        assert_eq!(world.bytes, committed);
        let slot = cr::slot(&world.bytes, m.evaluator(2)?, start + 90)?;
        assert_eq!(
            (slot.status, slot.reveal),
            (Status::Revealed, Some(receipt))
        );
    }
    Ok(())
}

/// AI.F03-A14: a different digest for the final slot returns `REPORT_ALREADY_FINAL`.
#[test]
fn f03_a14_a_different_digest_for_the_final_slot_is_already_final() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 500_000), (1, 250_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    let receipt = m.reveal(2, &report, start + 80)?;
    let changed = m.scores(&[(0, 500_000), (1, 250_001)]);
    let other = m.signed(2, &changed)?;
    let call = Market::reveal_call(2, &other, SALT, 4, start + 96)?;
    let code = m.world.refused(&call, start + 81)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    assert_eq!(code, F03_REPORT_ALREADY_FINAL);
    Ok(())
}

/// AI.F03-A15: eight maximum 32-entry reports fit the report section and the shared state, an
/// undersized next-state or event buffer refuses the last reveal leaving its commitment and row
/// untouched, and a ninth evaluator schedule returns `EVALUATOR_CAPACITY`.
#[test]
fn f03_a15_eight_maximum_reports_and_resource_rollback() -> TestResult {
    let mut m = crowded(32)?;
    let start = m.start();
    let scores = m.all(500_000);
    for n in 2..=9 {
        let report = m.signed(n, &scores)?;
        m.commit(n, &report, start + 64)?;
    }
    for n in 2..=8 {
        let report = m.signed(n, &scores)?;
        m.reveal(n, &report, start + 80)?;
    }
    let last = m.signed(9, &scores)?;
    let call = Market::reveal_call(9, &last, SALT, 3, start + 96)?;
    let encoded = call.encode()?;
    let committed = m.world.bytes.clone();
    let [next, scratch, event, result] = ROUTE;
    for sizes in [
        [committed.len(), scratch, event, result],
        [next, scratch, 64, result],
    ] {
        let composed = m.world.compose(call.actor, &encoded, start + 81, sizes)?;
        let Routed::Refused { result_len } = composed.routed else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(
            decode_result(&composed.result[..result_len])?.error,
            Some(CAPACITY)
        );
        assert_eq!(m.world.bytes, committed);
        let slot = m.slot(9, start + 81)?;
        assert_eq!((slot.status, slot.reveal), (Status::Committed, None));
    }
    m.reveal(9, &last, start + 81)?;
    let reports = section_bytes(&m.world.bytes, Section::CurrentReports)?;
    assert!(reports.len() <= Section::CurrentReports.payload_cap());
    assert!(Section::CurrentReports.payload_cap() <= 24_576);
    assert!(m.world.bytes.len() <= MAX_STATE_BYTES);
    for n in 2..=9 {
        assert!(m.admitted(n)?.is_some());
    }
    let ninth = schedule_payload(
        principal(10)?,
        10,
        public(&evaluator_key(10)),
        m.frozen.epoch + 1,
    );
    let call = m.owner_call(dispatch::ScheduleEvaluator, ninth)?;
    assert_eq!(m.world.refused(&call, start + 82)?, F03_EVALUATOR_CAPACITY);
    Ok(())
}

/// The F08 acceptance of pending evaluator 8: a wrong principal and a wrong-key proof refuse
/// atomically, the correct one sets the existing admitted flag and leaves the grant pending.
fn accept_pending(m: &mut Market, at: u64) -> TestResult {
    let nominee = principal(8)?;
    let outsider = PrincipalId::new(OWNER)?;
    m.world.edit(|parts, market| {
        let evaluator = derive_evaluator(market.market_id, nominee, [8; 32])?;
        let grant = EvaluatorRegion::decode(&parts.region)?
            .get(evaluator)
            .ok_or(NOT_FOUND)?
            .grant;
        let participant = Participant::Evaluator(evaluator);
        let (effective, approval) = approve(
            &mut parts.admission,
            market,
            participant,
            nominee,
            grant.signing_key,
            at,
        )?;
        assert_eq!(effective, grant.effective_epoch);
        let (request, payload) = consent_payload(market, &grant, 8, approval, &evaluator_key(8))?;
        let (_, forged) = consent_payload(market, &grant, 8, approval, &evaluator_key(9))?;
        let before = parts.admission;
        for (who, proof, code) in [
            (outsider, &payload, F08_OWNER_REQUIRED),
            (nominee, &forged, F08_BAD_CONSENT),
        ] {
            let refused = admit_evaluator(
                &mut parts.admission,
                &ctx(market, who, at),
                &grant,
                request,
                proof,
            );
            assert_eq!(refused.err(), Some(code));
            assert_eq!(parts.admission, before);
        }
        assert!(!before.get(participant).ok_or(NOT_FOUND)?.admitted());
        let meta = admit_evaluator(
            &mut parts.admission,
            &ctx(market, nominee, at),
            &grant,
            request,
            &payload,
        )?;
        assert!(meta.admitted());
        assert_eq!(parts.admission.len(), before.len());
        assert_eq!(
            EvaluatorRegion::decode(&parts.region)?
                .get(evaluator)
                .map(|r| r.grant.status),
            Some(GrantStatus::Pending)
        );
        Ok(())
    })
}

/// AI.F03-A16: a routed schedule creates a pending grant outside the frozen roster; without
/// F08 acceptance it stays pending and the routed opening of its effective epoch excludes it,
/// with acceptance the opening activates it.
#[test]
fn f03_a16_pending_grants_stay_ineligible_until_accepted() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let payload = schedule_payload(
        principal(8)?,
        8,
        public(&evaluator_key(8)),
        m.frozen.epoch + 1,
    );
    m.owner_authority(dispatch::ScheduleEvaluator, payload, start + 10)?;
    let pending = m.evaluator(8)?;
    let region = region_of(&m.world.bytes)?;
    assert_eq!(
        region.get(pending).map(|r| r.grant.status),
        Some(GrantStatus::Pending)
    );
    assert!(region.snapshot().ok_or(NOT_FOUND)?.get(pending).is_none());
    let scores = m.scores(&[(0, 1)]);
    let report = m.signed(8, &scores)?;
    let c = commitment_of(&report.body, SALT)?;
    let call = Market::commit_call(8, &report.body.binding, c, 1, start + 80)?;
    assert_eq!(m.world.refused(&call, start + 64)?, UNAUTHORIZED);
    let unaccepted = m.clone();
    accept_pending(&mut m, start + 11)?;
    for (mut opened, included) in [(unaccepted, false), (m, true)] {
        opened.settle(SETTLE_AT)?;
        opened.frozen = opened.world.open(NEXT_OPEN)?;
        let region = region_of(&opened.world.bytes)?;
        let snapshot = region.snapshot().ok_or(NOT_FOUND)?;
        assert_eq!(snapshot.epoch, 8);
        assert_eq!(snapshot.get(pending).is_some(), included);
        let status = if included {
            GrantStatus::Active
        } else {
            GrantStatus::Pending
        };
        assert_eq!(region.get(pending).map(|r| r.grant.status), Some(status));
    }
    Ok(())
}

/// AI.F03-A16: suspension halfway through the epoch refuses a new grant while the already
/// eligible evaluator's accepted commitment is still revealed and admitted.
#[test]
fn f03_a16_suspension_rejects_new_grants_and_keeps_admitted_work() -> TestResult {
    let mut m = market()?;
    let start = m.start();
    let scores = m.scores(&[(0, 300_000)]);
    let report = m.signed(2, &scores)?;
    m.commit(2, &report, start + 64)?;
    m.world.suspend(start + 66)?;
    assert_eq!(m.world.section()?.header.lifecycle, SUSPENDED);
    let payload = schedule_payload(
        principal(8)?,
        8,
        public(&evaluator_key(8)),
        m.frozen.epoch + 1,
    );
    let call = m.owner_call(dispatch::ScheduleEvaluator, payload)?;
    assert_eq!(m.world.refused(&call, start + 67)?, WRONG_PHASE);
    let receipt = m.reveal(2, &report, start + 80)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

/// AI.F03-A16: a routed quality-disagreement challenge alone changes no admitted report,
/// sealed score or reward.
#[test]
fn f03_a16_quality_challenge_changes_no_score_or_reward() -> TestResult {
    let mut m = market()?;
    let plan = [(2, 200_000), (3, 400_000), (4, 600_000)]
        .map(|(n, score)| (n, m.scores(&[(0, score), (1, score)])));
    m.admit(&plan)?;
    let mut unchallenged = m.clone();
    let receipt = m.admitted(3)?.ok_or(NOT_FOUND)?;
    let reports = section_bytes(&m.world.bytes, Section::CurrentReports)?;
    let call = m.challenge_call(allegation(&receipt, 4, 0x71))?;
    m.world.apply(&call, m.start() + 84)?;
    assert_eq!(
        section_bytes(&m.world.bytes, Section::CurrentReports)?,
        reports
    );
    assert_eq!(m.admitted(3)?, Some(receipt));
    let challenged = m.settle(SETTLE_AT)?;
    let baseline = unchallenged.settle(SETTLE_AT)?;
    assert_eq!(challenged.input, baseline.input);
    assert_eq!(challenged.root, baseline.root);
    let outputs =
        |m: &Market| -> CodecResult<Vec<_>> { Ok(m.outputs()?.into_iter().map(fields).collect()) };
    assert_eq!(outputs(&m)?, outputs(&unchallenged)?);
    assert_eq!(
        reward_row(&m.world.bytes, 7)?,
        reward_row(&unchallenged.world.bytes, 7)?
    );
    Ok(())
}

const NATIVE_OWNER: usize = 0;
const NATIVE_OUTSIDER: usize = 2;
const NATIVE_ASSET: [u8; 32] = {
    let mut asset = [0; 32];
    asset[0] = 9;
    asset
};
const NATIVE_EXPIRY: u64 = 1_000_000;
const EVENTS_DOMAIN: &[u8] = b"LayerX/programs/events/v1\0";
const PROGRAM_REFUSED: i64 = -736;

enum Failure {
    Application(ApplicationError),
    Conversion(core::num::TryFromIntError),
    Io(std::io::Error),
    Harness(String),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Conversion(error) => write!(f, "integer conversion {error}"),
            Self::Io(error) => write!(f, "native harness io {error}"),
            Self::Harness(what) => write!(f, "native harness {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<core::num::TryFromIntError> for Failure {
    fn from(error: core::num::TryFromIntError) -> Self {
        Self::Conversion(error)
    }
}
impl From<std::io::Error> for Failure {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn harness(what: impl Into<String>) -> Failure {
    Failure::Harness(what.into())
}
fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    bytes
        .iter()
        .flat_map(|b| [DIGITS[usize::from(b >> 4)], DIGITS[usize::from(b & 15)]])
        .map(char::from)
        .collect()
}
fn unhex(text: &str) -> Checked<Vec<u8>> {
    if text == "-" {
        return Ok(Vec::new());
    }
    if !text.len().is_multiple_of(2) {
        return Err(harness(format!("odd hex length {}", text.len())));
    }
    (0..text.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&text[i..i + 2], 16).map_err(|e| harness(format!("hex {e}"))))
        .collect()
}
fn fixed(bytes: &[u8]) -> Checked<[u8; 32]> {
    bytes
        .try_into()
        .map_err(|_| harness(format!("expected 32 bytes, got {}", bytes.len())))
}
fn number<T: core::str::FromStr>(token: &str) -> Checked<T> {
    token
        .parse()
        .map_err(|_| harness(format!("bad number {token}")))
}

struct Manifest {
    wasm: String,
    harness: String,
    chain: [u8; 32],
}
fn manifest() -> Checked<Manifest> {
    let path = std::env::var_os("PAXAI_HOST_BOUNDARY_MANIFEST").map_or_else(
        || {
            PathBuf::from(env!("CARGO_MANIFEST_DIR"))
                .join("../../../build/paxai-host-boundary/manifest")
        },
        PathBuf::from,
    );
    let text = std::fs::read_to_string(&path).map_err(|e| {
        harness(format!(
            "{}: {e}; run make paxai-host-boundary-build",
            path.display()
        ))
    })?;
    let lines: Vec<&str> = text.lines().collect();
    let [wasm, binary, chain] = lines[..] else {
        return Err(harness(format!("manifest has {} lines", lines.len())));
    };
    Ok(Manifest {
        wasm: wasm.to_owned(),
        harness: binary.to_owned(),
        chain: fixed(&unhex(chain)?)?,
    })
}

struct NativeEvent {
    producer: Vec<u8>,
    principal: Vec<u8>,
    frame: Vec<u8>,
    topic: Vec<u8>,
    data: Vec<u8>,
}
struct Cursor<'a> {
    bytes: &'a [u8],
    at: usize,
}
impl<'a> Cursor<'a> {
    fn take(&mut self, n: usize) -> Checked<&'a [u8]> {
        let end = self
            .at
            .checked_add(n)
            .ok_or_else(|| harness("event overflow"))?;
        let out = self
            .bytes
            .get(self.at..end)
            .ok_or_else(|| harness("short event envelope"))?;
        self.at = end;
        Ok(out)
    }
    fn be32(&mut self) -> Checked<usize> {
        let bytes: [u8; 4] = self
            .take(4)?
            .try_into()
            .map_err(|_| harness("event length"))?;
        Ok(usize::try_from(u32::from_be_bytes(bytes))?)
    }
}
fn native_events(raw: &[u8]) -> Checked<Vec<NativeEvent>> {
    if raw.is_empty() {
        return Ok(Vec::new());
    }
    let mut r = Cursor { bytes: raw, at: 0 };
    if r.take(EVENTS_DOMAIN.len())? != EVENTS_DOMAIN {
        return Err(harness("event envelope domain"));
    }
    let count = r.be32()?;
    let mut events = Vec::new();
    for _ in 0..count {
        let producer = r.take(32)?.to_vec();
        let principal = r.take(32)?.to_vec();
        let frame = r.take(9)?.to_vec();
        let topic_len = r.be32()?;
        let topic = r.take(topic_len)?.to_vec();
        let data_len = r.be32()?;
        let data = r.take(data_len)?.to_vec();
        events.push(NativeEvent {
            producer,
            principal,
            frame,
            topic,
            data,
        });
    }
    if r.at != raw.len() {
        return Err(harness("trailing event envelope bytes"));
    }
    Ok(events)
}

struct Line {
    status: i64,
    result_code: i64,
    kind: u8,
    abi: u16,
    terminal: Vec<u8>,
    state: Vec<u8>,
    blobs_before: usize,
    blobs_after: usize,
    application_blobs: usize,
    kv_after: usize,
    witness: usize,
    balance_before: u64,
    balance_after: u64,
    fee: u64,
    rewards_balance: u64,
    events: Vec<NativeEvent>,
}
fn parse_line(text: &str) -> Checked<Line> {
    let t: Vec<&str> = text.split_whitespace().collect();
    if t.len() != 18 || t[0] != "call" {
        return Err(harness(format!("unexpected line {text}")));
    }
    Ok(Line {
        status: number(t[1])?,
        result_code: number(t[2])?,
        kind: number(t[3])?,
        abi: number(t[4])?,
        terminal: unhex(t[5])?,
        state: unhex(t[6])?,
        blobs_before: number(t[7])?,
        blobs_after: number(t[8])?,
        application_blobs: number(t[9])?,
        kv_after: number(t[11])?,
        witness: number(t[12])?,
        balance_before: number(t[13])?,
        balance_after: number(t[14])?,
        fee: number(t[15])?,
        rewards_balance: number(t[16])?,
        events: native_events(&unhex(t[17])?)?,
    })
}

enum Terminal {
    Success(Vec<u8>),
    Rejected(Vec<u8>),
}
fn terminal(line: &Line, program: ProgramId) -> Checked<Terminal> {
    let decoded = decode_terminal_payload(line.kind, line.abi, &line.terminal)
        .map_err(|e| harness(format!("terminal {e:?}")))?;
    let TerminalDetail::Execution(ExecutionTerminal::CandidateV4 {
        program: producer,
        outcome,
        ..
    }) = decoded.detail
    else {
        return Err(harness("terminal is not a CandidateV4 execution"));
    };
    assert_eq!(producer, program.bytes());
    match outcome {
        CandidateTerminalOutcome::Success { code: 0, response } => Ok(Terminal::Success(response)),
        CandidateTerminalOutcome::Failure(failure) if failure.class() == RefusalClass::Rejected => {
            Ok(Terminal::Rejected(failure.reason().bytes().to_vec()))
        }
        _ => Err(harness(
            "terminal is neither success code 0 nor a Rejected refusal",
        )),
    }
}

struct Expected {
    applied: Option<(Operation, Vec<u8>, Vec<u8>)>,
    refused: bool,
    result: Vec<u8>,
}

/// The native host boundary: every call runs in the real interpreter through the native
/// adapter and must agree byte for byte with the in-process route.
struct Native {
    child: Child,
    input: Option<ChildStdin>,
    output: BufReader<ChildStdout>,
    chain: ChainDomain,
    program: ProgramId,
    principals: [PrincipalId; 3],
    rewards: [u8; 32],
    blob_cap: usize,
    staged_cap: usize,
    kv_cap: usize,
    state: Option<Vec<u8>>,
    rewards_balance: Option<u64>,
    next_request: u8,
}
impl Drop for Native {
    fn drop(&mut self) {
        if self.input.is_some() {
            drop(self.child.kill());
            drop(self.child.wait());
        }
    }
}
impl Native {
    fn start() -> Checked<Self> {
        let m = manifest()?;
        let mut child = Command::new(&m.harness)
            .arg(&m.wasm)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .spawn()?;
        let input = child.stdin.take().ok_or_else(|| harness("stdin"))?;
        let stdout = child.stdout.take().ok_or_else(|| harness("stdout"))?;
        let mut output = BufReader::new(stdout);
        let mut ready = String::new();
        output.read_line(&mut ready)?;
        let t: Vec<&str> = ready.split_whitespace().collect();
        if t.len() != 9 || t[0] != "ready" {
            return Err(harness(format!("bring-up line {ready}")));
        }
        let principal =
            |i: usize| -> Checked<PrincipalId> { Ok(PrincipalId::new(fixed(&unhex(t[i])?)?)?) };
        Ok(Self {
            child,
            input: Some(input),
            output,
            chain: ChainDomain::new(m.chain)?,
            program: ProgramId::new(fixed(&unhex(t[1])?)?)?,
            principals: [principal(2)?, principal(3)?, principal(4)?],
            rewards: fixed(&unhex(t[5])?)?,
            blob_cap: number(t[6])?,
            staged_cap: number(t[7])?,
            kv_cap: number(t[8])?,
            state: None,
            rewards_balance: None,
            next_request: 0,
        })
    }
    fn finish(mut self) -> Checked {
        drop(self.input.take());
        let status = self.child.wait()?;
        assert!(status.success(), "native harness exit {status}");
        Ok(())
    }
    fn market(&self) -> CodecResult<MarketId> {
        derive_market(self.chain, self.program)
    }
    fn rewards(&self) -> CodecResult<AccountId> {
        derive_rewards_account(self.program, AssetId::new(NATIVE_ASSET)?)
    }
    fn envelope(
        &mut self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &[u8],
    ) -> Checked<Vec<u8>> {
        self.next_request = self.next_request.wrapping_add(1).max(1);
        let envelope = Envelope {
            operation,
            chain: self.chain,
            program: self.program,
            market: self.market()?,
            actor: self.principals[actor],
            epoch: 0,
            config: 1,
            roster: Presence::Absent,
            sequence,
            expiry: NATIVE_EXPIRY,
            request: RequestId::new([self.next_request; 32])?,
            payload,
            authentication: Authentication::Native,
        };
        let mut out = vec![0; 16_384];
        let n = encode_envelope(&envelope, &mut out)?;
        out.truncate(n);
        Ok(out)
    }
    fn expected(&self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Expected> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut result = vec![0; MAX_RESULT_BYTES];
        let routed = dispatch::route(
            &CallContext {
                chain: self.chain,
                program: self.program,
                principal: self.principals[actor],
                height,
            },
            envelope,
            self.state.as_deref(),
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?;
        Ok(match routed {
            Routed::Applied {
                operation,
                state_len,
                event_len,
                result_len,
                ..
            } => Expected {
                applied: Some((
                    operation,
                    next[..state_len].to_vec(),
                    event[..event_len].to_vec(),
                )),
                refused: false,
                result: result[..result_len].to_vec(),
            },
            Routed::Unchanged { result_len } => Expected {
                applied: None,
                refused: false,
                result: result[..result_len].to_vec(),
            },
            Routed::Refused { result_len } => Expected {
                applied: None,
                refused: true,
                result: result[..result_len].to_vec(),
            },
        })
    }
    fn submit(&mut self, actor: usize, height: u64, envelope: &[u8]) -> Checked<Vec<u8>> {
        let expected = self.expected(actor, height, envelope)?;
        let input = self.input.as_mut().ok_or_else(|| harness("closed"))?;
        writeln!(input, "{actor} {height} {}", hex(envelope))?;
        input.flush()?;
        let mut text = String::new();
        if self.output.read_line(&mut text)? == 0 {
            return Err(harness("native harness stopped"));
        }
        let line = parse_line(&text)?;
        self.check_common(&line);
        let terminal = terminal(&line, self.program)?;
        match (&expected.applied, expected.refused, terminal) {
            (Some((operation, state, event)), false, Terminal::Success(response)) => {
                assert_eq!(line.result_code, 0);
                assert_eq!(response, expected.result);
                assert_eq!(&line.state, state);
                assert!(line.state.len() <= MAX_STATE_BYTES);
                assert_eq!(line.application_blobs, 2);
                assert!(line.witness > 0);
                self.check_event(&line, actor, *operation, event)?;
                self.state = Some(line.state);
            }
            (None, false, Terminal::Success(response)) => {
                assert_eq!(line.result_code, 0);
                assert_eq!(response, expected.result);
                self.check_unchanged(&line);
            }
            (None, true, Terminal::Rejected(reason)) => {
                assert_eq!(line.result_code, PROGRAM_REFUSED);
                assert_eq!(reason, expected.result);
                self.check_unchanged(&line);
            }
            _ => panic!("native terminal disagrees with the in-process route"),
        }
        Ok(expected.result)
    }
    fn check_common(&mut self, line: &Line) {
        assert_eq!(line.status, 0);
        assert!(line.blobs_after >= line.blobs_before);
        assert!(line.blobs_after - line.blobs_before <= self.staged_cap);
        assert!(line.blobs_after <= self.blob_cap);
        assert!(line.kv_after <= self.kv_cap);
        assert_eq!(line.balance_after, line.balance_before - line.fee);
        let rewards = *self.rewards_balance.get_or_insert(line.rewards_balance);
        assert_eq!(line.rewards_balance, rewards);
    }
    fn check_unchanged(&self, line: &Line) {
        assert_eq!(line.state, self.state.clone().unwrap_or_default());
        assert_eq!(line.application_blobs, 0);
        assert_eq!(line.blobs_after, line.blobs_before);
        assert!(line.events.is_empty());
    }
    fn check_event(&self, line: &Line, actor: usize, operation: Operation, body: &[u8]) -> Checked {
        let mut topic = [0; 64];
        let topic_len = codec::event_topic(operation, &mut topic)?;
        let [event] = &line.events[..] else {
            return Err(harness(format!("{} native events", line.events.len())));
        };
        assert_eq!(event.producer, self.program.bytes());
        assert_eq!(event.principal, self.principals[actor].bytes());
        assert_eq!(event.frame, [0; 9]);
        assert_eq!(event.topic, &topic[..topic_len]);
        assert_eq!(event.data, body);
        Ok(())
    }
    fn send(
        &mut self,
        operation: Operation,
        actor: usize,
        sequence: u64,
        payload: &[u8],
        height: u64,
    ) -> Checked<Vec<u8>> {
        let envelope = self.envelope(operation, actor, sequence, payload)?;
        self.submit(actor, height, &envelope)
    }
    fn revision(&self) -> Checked<u64> {
        let state = self
            .state
            .as_deref()
            .ok_or_else(|| harness("no committed state"))?;
        Ok(decode_shared_state(state)?.revision)
    }
    fn region(&self) -> Checked<EvaluatorRegion> {
        let state = self
            .state
            .as_deref()
            .ok_or_else(|| harness("no committed state"))?;
        Ok(region_of(state)?)
    }
    /// Native CREATE at height 1000 and the real `EnrollWorker` of the outsider principal.
    fn create_with_worker(&mut self) -> Checked {
        let mut create = self.principals[NATIVE_OWNER].bytes().to_vec();
        create.extend_from_slice(&NATIVE_ASSET);
        create.extend_from_slice(self.rewards()?.as_bytes());
        create.extend_from_slice(&REFUND);
        create.push(0);
        create.extend_from_slice(&policy_bytes()?);
        create.extend_from_slice(&[16; 32]);
        assert_eq!(
            applied_at(&self.send(dispatch::CREATE, NATIVE_OWNER, 1, &create, 1000)?)?,
            1
        );
        let mut enroll = self.principals[NATIVE_OUTSIDER].bytes().to_vec();
        enroll.extend_from_slice(&[21; 32]);
        enroll.extend_from_slice(&public(&SigningKey::from_bytes(&[0x41; 32])).0);
        enroll.extend_from_slice(&[31; 32]);
        enroll.extend_from_slice(&1101u64.to_be_bytes());
        assert_eq!(
            applied_at(&self.send(dispatch::EnrollWorker, NATIVE_OWNER, 2, &enroll, 1001)?)?,
            2
        );
        Ok(())
    }
}

fn status(frame: &[u8]) -> Checked<(ResultStatus, Option<ApplicationError>, u64)> {
    let r = decode_result(frame)?;
    Ok((r.status, r.error, r.revision))
}
fn refusal(frame: &[u8]) -> Checked<ApplicationError> {
    match status(frame)? {
        (ResultStatus::Error, Some(error), _) => Ok(error),
        other => Err(harness(format!("expected a refusal, got {other:?}"))),
    }
}
fn applied_at(frame: &[u8]) -> Checked<u64> {
    match status(frame)? {
        (ResultStatus::Ok, None, revision) => Ok(revision),
        other => Err(harness(format!("expected Ok, got {other:?}"))),
    }
}
fn nominee(i: u8) -> CodecResult<PrincipalId> {
    PrincipalId::new([0x50 + i; 32])
}

/// AI.F03-A09/A10/A15 on the real interpreter: eight evaluator grants are scheduled with at
/// most two new blobs each, a ninth returns `EVALUATOR_CAPACITY`, worker-owner and market-owner
/// principals return `ROLE_CONFLICT`, and an identical repeated revocation changes nothing; the
/// rewards account never moves.
#[test]
fn native_f03_schedule_capacity_role_conflicts_and_revocation() -> Checked {
    let mut n = Native::start()?;
    assert_eq!(n.rewards()?.bytes(), n.rewards);
    n.create_with_worker()?;
    let mut sequence = 3;
    for i in 0..8u8 {
        let payload = schedule_payload(nominee(i)?, 0x70 + i, public(&evaluator_key(0x60 + i)), 0);
        let frame = n.send(
            dispatch::ScheduleEvaluator,
            NATIVE_OWNER,
            sequence,
            &payload,
            1002 + u64::from(i),
        )?;
        assert_eq!(applied_at(&frame)?, sequence);
        sequence += 1;
    }
    let region = n.region()?;
    assert_eq!(region.len(), 8);
    assert!(region
        .records()
        .all(|r| r.grant.status == GrantStatus::Pending));
    let refusals = [
        (nominee(8)?, F03_EVALUATOR_CAPACITY),
        (n.principals[NATIVE_OUTSIDER], ROLE_CONFLICT),
        (n.principals[NATIVE_OWNER], ROLE_CONFLICT),
    ];
    for (i, (who, code)) in (0x78u8..).zip(refusals) {
        let payload = schedule_payload(who, i, public(&evaluator_key(i)), 0);
        let frame = n.send(
            dispatch::ScheduleEvaluator,
            NATIVE_OWNER,
            sequence,
            &payload,
            1010,
        )?;
        assert_eq!(refusal(&frame)?, code);
    }
    let first = derive_evaluator(n.market()?, nominee(0)?, [0x70; 32])?;
    let revoke = revoke_payload(first);
    let frame = n.send(
        dispatch::RevokeEvaluator,
        NATIVE_OWNER,
        sequence,
        &revoke,
        1011,
    )?;
    assert_eq!(applied_at(&frame)?, sequence);
    let revoked = n.revision()?;
    let repeat = n.send(
        dispatch::RevokeEvaluator,
        NATIVE_OWNER,
        sequence + 1,
        &revoke,
        1012,
    )?;
    assert_eq!(
        status(&repeat)?,
        (ResultStatus::AlreadyApplied, None, revoked)
    );
    assert_eq!(
        n.region()?.get(first).map(|r| r.grant.status),
        Some(GrantStatus::Revoked)
    );
    assert_eq!(n.revision()?, revoked);
    n.finish()
}
