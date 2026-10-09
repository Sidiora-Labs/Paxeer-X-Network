//! AI.CORE fuel budget of the real guest. The wasm guest is built exactly as the Makefile
//! builds it and validated as ABI 5 under the genesis `FuelSchedule::WASMI_0_31_2`; one
//! market journey starting from a fresh market sends every operation `dispatch::route`
//! serves through the runtime executor under the protocol budget. Each call is first routed
//! in-process over the same committed bytes; the guest must return that exact result frame,
//! emit that exact event and store that exact next state, within `DEFAULT_CPU_FUEL`.
//! Prerequisites no routed operation produces (F08 admission, F03 grants, F06 funding and
//! terminalization) are written by the real producers, as in the F04 recovery journey.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        encode_envelope, CommitScorePayload, Envelope, ReportBody, RevealScorePayload, ScoreVector,
    },
    commit_reveal::commitment,
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{ApplicationError, CodecResult, ARITHMETIC, NOT_FOUND},
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms, SignedReport},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader},
    registry_ops::{CallContext, PolicySection, PERMIT_METADATA, PERMIT_SUSPEND},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvidenceRoot, FrozenBinding, MetadataDigest, Presence,
        PrincipalId, ProgramId, PublicKey32, ReportDigest, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, Salt32, Signature64, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{
        consent_digest, decode_manifest, WorkerCurrent, WorkerState, WorkerTable,
        WORKER_TABLE_MAX_BYTES,
    },
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES, SHARED_STATE_KEY,
};
use layerx_programs_runtime::{
    self as rt, abi::UnavailableReceiptOracle, meter::DEFAULT_CPU_FUEL, ActivityBudgetBinding,
    AuthorizationContext, AuthorizedExecutionRequest, BudgetedAuthorizedExecutionRequest,
    Capability, CapabilitySet, CompositionContext, DeclaredBudget, Executor, FuelSchedule, Storage,
    StorageNamespace, V2ActivityOutcome, V2AuthorizedExecutionRecord, ValidatedModule, WasmEngine,
    ABI_V5_VERSION, CALL_ENTRY_EXPORT,
};
use std::{
    fmt::{self, Write as _},
    fs,
    path::Path,
    process::Command,
};

/// The Makefile's default guest chain domain, `PAXAI/host-boundary/chain-domain`.
const CHAIN: [u8; 32] = *b"PAXAI/host-boundary/chain-domain";
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const REQUESTER: u8 = 0x30;
const CHALLENGER: u8 = 9;
const OPERATOR: u8 = 0x60;
const NOMINEE: u8 = 0x40;
const LAPSED: u8 = 0x41;
const SCHEDULED: u8 = 8;
const ORIGIN: u64 = 128;
const METADATA: [u8; 32] = [0x44; 32];
const ROLE_EXPIRY: u64 = 5000;
const SALT: [u8; 32] = [0x5a; 32];
/// Epoch 7 of the market at origin 128: T = 1024, commit [1088, 1104), reveal [1104, 1120).
const WORK_AT: u64 = 1030;
const COMMIT_AT: u64 = 1088;
const REVEAL_AT: u64 = 1104;
/// The two frozen workers of epoch 7 and the evaluators that commit and reveal.
const LOW: u8 = 0x20;
const HIGH: u8 = 0x21;
const REVEALERS: [u8; 3] = [2, 3, 4];
/// Every selector `dispatch::route` serves.
const ROUTED: [Operation; 31] = [
    dispatch::CREATE,
    dispatch::STAGE_POLICY,
    dispatch::CANCEL_POLICY,
    dispatch::SCHEDULE_ACTIVATION,
    dispatch::ADVANCE_ACTIVATION,
    dispatch::SUSPEND,
    dispatch::UPDATE_METADATA,
    dispatch::APPOINT_OPERATOR,
    dispatch::REVOKE_OPERATOR,
    dispatch::ADMIT_TASK,
    dispatch::ACCEPT_TASK,
    dispatch::CANCEL_TASK,
    dispatch::COMMIT_TASK_RESULT,
    dispatch::SEAL_TASK_SET,
    dispatch::OPEN_EPOCH,
    dispatch::EnrollWorker,
    dispatch::PublishMetadata,
    dispatch::SetDraining,
    dispatch::UndoDrain,
    dispatch::RotateDelegate,
    dispatch::RevokeDelegate,
    dispatch::RetireWorker,
    dispatch::AcceptEnrollment,
    dispatch::ExpireEnrollment,
    dispatch::ScheduleEvaluator,
    dispatch::RotateEvaluatorKey,
    dispatch::RevokeEvaluator,
    dispatch::ChallengeAssessment,
    dispatch::CommitScore,
    dispatch::RevealScore,
    dispatch::SealEvidence,
];

/// A failed journey step: an application refusal, a routed call that did not apply, or a
/// guest build or validation failure.
enum Failure {
    App(ApplicationError),
    NotApplied(&'static str, String),
    Guest(String),
}
impl fmt::Debug for Failure {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::App(error) => write!(f, "application refusal {error:?}"),
            Self::NotApplied(op, routed) => write!(f, "{op} routed to {routed}"),
            Self::Guest(reason) => write!(f, "guest: {reason}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::App(error)
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
fn policy_bytes(value: &TaskPolicyV1) -> CodecResult<Vec<u8>> {
    let mut bytes = vec![0; TASK_POLICY_BYTES];
    value.encode(&mut bytes)?;
    Ok(bytes)
}
/// The ed25519 delegate of worker `n`.
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
/// The frozen signing key of evaluator `n`.
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
/// The rotated signing key of evaluator or worker `n`.
fn rotated_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0xe0 ^ n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// The evidence root evaluator `n` seals in `epoch`.
fn root(n: u8, epoch: u64) -> [u8; 32] {
    let mut out = [0xe0 + n; 32];
    out[24..].copy_from_slice(&epoch.to_be_bytes());
    out
}
/// A request id unique per kind, actor and sequence.
fn tag(kind: u8, n: u8, sequence: u64) -> [u8; 32] {
    let mut out = [kind; 32];
    out[0] = n;
    out[1..9].copy_from_slice(&sequence.to_be_bytes());
    out
}
fn u64s(values: &[u64]) -> Vec<u8> {
    values.iter().flat_map(|v| v.to_be_bytes()).collect()
}

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// One request envelope; `delegate` signs the request digest when present.
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
    delegate: Option<SigningKey>,
    payload: Vec<u8>,
}
fn req(operation: Operation, actor: PrincipalId, request: [u8; 32], payload: Vec<u8>) -> Req {
    Req {
        operation,
        actor,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        request,
        expiry: u64::MAX,
        delegate: None,
        payload,
    }
}
/// An envelope carrying the frozen binding `frozen`.
fn bound(
    operation: Operation,
    actor: PrincipalId,
    frozen: &FrozenBinding,
    request: [u8; 32],
    payload: Vec<u8>,
) -> Req {
    Req {
        epoch: frozen.epoch,
        config: frozen.config.get(),
        roster: Presence::Present(frozen.roster),
        ..req(operation, actor, request, payload)
    }
}
impl Req {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = ChainDomain::new(CHAIN)?;
        let program = ProgramId::new(PROGRAM)?;
        let mut envelope = Envelope {
            operation: self.operation,
            chain,
            program,
            market: derive_market(chain, program)?,
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
        let mut out = vec![0; MAX_RESULT_BYTES];
        if let Some(key) = &self.delegate {
            envelope.authentication = Authentication::Delegate {
                key: public(key),
                signature: Signature64([0; 64]),
            };
            let n = encode_envelope(&envelope, &mut out)?;
            let digest = decode_envelope(&out[..n])?.request_digest()?;
            envelope.authentication = Authentication::Delegate {
                key: public(key),
                signature: Signature64(key.sign(digest.as_bytes()).to_bytes()),
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

fn admission_ctx(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
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
    let effective = table.required_effective_epoch(&admission_ctx(market, owner, at))?;
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
    let digest = table.approve(&admission_ctx(market, market.owner_principal, at), &terms)?;
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
/// The canonical worker manifest of `record` at metadata `revision`.
fn manifest(
    market: &MarketHeader,
    record: &WorkerCurrent,
    revision: u64,
    valid_from: u64,
    expiry: u64,
) -> CodecResult<Vec<u8>> {
    let uri = b"https://worker.example/paxai/v1";
    let mut v = 1u16.to_be_bytes().to_vec();
    v.extend_from_slice(market.market_id.as_bytes());
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
    v.push(1);
    v.push(1);
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
        &u32::try_from(uri.len())
            .map_err(|_| ARITHMETIC)?
            .to_be_bytes(),
    );
    v.extend_from_slice(uri);
    v.extend_from_slice(&[70; 32]);
    v.push(1);
    v.extend_from_slice(&1u16.to_be_bytes());
    v.extend_from_slice(&1u16.to_be_bytes());
    v.extend_from_slice(&[71; 32]);
    v.extend_from_slice(&[72; 32]);
    v.extend_from_slice(&0u32.to_be_bytes());
    Ok(v)
}

/// The guest built as `make paxai-host-boundary-build` builds it, validated as ABI 5 under
/// the genesis metering schedule.
fn guest_module() -> Checked<ValidatedModule> {
    let programs = Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
    let target = Path::new(env!("CARGO_TARGET_TMPDIR")).join("fuel-budget-guest");
    let domain = CHAIN
        .iter()
        .try_fold(String::with_capacity(64), |mut hex, b| {
            write!(hex, "{b:02x}").map(|()| hex)
        })
        .map_err(|error| Failure::Guest(format!("chain domain: {error}")))?;
    let status = Command::new(env!("CARGO"))
        .current_dir(&programs)
        .env("PAXAI_CHAIN_DOMAIN", domain)
        .env("CARGO_TARGET_DIR", &target)
        .args([
            "build",
            "--locked",
            "--release",
            "--target",
            "wasm32-unknown-unknown",
            "-p",
            "layerx-programs-ai-market",
        ])
        .status()
        .map_err(|error| Failure::Guest(format!("guest build did not start: {error}")))?;
    if !status.success() {
        return Err(Failure::Guest(format!("guest build {status}")));
    }
    let wasm =
        fs::read(target.join("wasm32-unknown-unknown/release/layerx_programs_ai_market.wasm"))
            .map_err(|error| Failure::Guest(format!("guest artifact: {error}")))?;
    WasmEngine::declared()
        .map_err(|error| Failure::Guest(format!("engine: {error}")))?
        .validate_versioned_metered(ABI_V5_VERSION, &wasm, FuelSchedule::WASMI_0_31_2)
        .map_err(|error| Failure::Guest(format!("validation: {error}")))
}

/// One guest execution of `envelope` by the call's principal under the protocol maximum
/// budget, as the first activity of the batch at the call height, through the public
/// ABI-v5 budgeted qualification route of the runtime executor.
fn execute(
    module: &ValidatedModule,
    storage: &mut Storage,
    ctx: &CallContext,
    envelope: &[u8],
    activity: [u8; 32],
) -> Result<V2AuthorizedExecutionRecord, String> {
    let program = rt::ProgramId::new(ctx.program.bytes()).map_err(|e| e.to_string())?;
    let actor = rt::PrincipalId::new(ctx.principal.bytes()).map_err(|e| e.to_string())?;
    let capabilities = CapabilitySet::new([
        Capability::SharedStorageRead,
        Capability::SharedStorageWrite,
        Capability::EmitEvent,
    ])
    .map_err(|e| e.to_string())?;
    let executor = Executor::declared();
    let binding = ActivityBudgetBinding::new(activity).map_err(|e| e.to_string())?;
    let admitted = executor
        .admit_activity_budget_for_qualification(
            DeclaredBudget::protocol_maximum(),
            actor,
            binding,
            u128::MAX,
        )
        .map_err(|e| e.to_string())?;
    let request = BudgetedAuthorizedExecutionRequest::new(
        AuthorizedExecutionRequest {
            module,
            program,
            authorization: AuthorizationContext::new(actor, capabilities),
            receipts: &UnavailableReceiptOracle,
            entrypoint: CALL_ENTRY_EXPORT,
            calldata: envelope,
            composition: CompositionContext::isolated(),
            response_capacity: MAX_RESULT_BYTES,
        },
        admitted,
        actor,
        binding,
    );
    executor
        .execute_authorized_v5_budgeted_for_qualification(storage, request, 1, ctx.height)
        .map_err(|e| format!("executor refused: {e}"))
}

/// What the in-process route decided for one call.
struct Expected<'a> {
    state: &'a [u8],
    topic: &'a [u8],
    event: &'a [u8],
    result: &'a [u8],
}
/// The application error a refusal frame carries, or its raw length.
fn describe(frame: &[u8]) -> String {
    codec::decode_result(frame).map_or_else(
        |_| format!("{} unframed bytes", frame.len()),
        |result| format!("{:?}", result.error),
    )
}
/// Runs the guest over `current` and compares its response, event and stored state with the
/// routed decision.
fn run_guest(
    module: &ValidatedModule,
    ctx: &CallContext,
    current: Option<&[u8]>,
    envelope: &[u8],
    expected: &Expected<'_>,
) -> Checked<(u64, Result<(), String>)> {
    let program = rt::ProgramId::new(PROGRAM).map_err(|e| Failure::Guest(e.to_string()))?;
    let namespace = StorageNamespace::shared(program);
    let mut storage = Storage::new();
    if let Some(bytes) = current {
        let mut seed = storage.transaction(namespace);
        seed.write(SHARED_STATE_KEY, bytes)
            .map_err(|e| Failure::Guest(e.to_string()))?;
        assert_eq!(seed.commit(), 1);
    }
    let activity = decode_envelope(envelope)?.request_digest()?.bytes();
    let record = match execute(module, &mut storage, ctx, envelope, activity) {
        Ok(record) => record,
        Err(reason) => return Ok((0, Err(reason))),
    };
    let fuel = record.execution().usage().cpu_fuel;
    let verdict = match record.outcome() {
        V2ActivityOutcome::Success { response, effects } => {
            let events: Vec<(&[u8], &[u8])> = effects
                .events
                .iter()
                .map(|e| (e.topic.as_slice(), e.data.as_slice()))
                .collect();
            let stored = storage
                .transaction(namespace)
                .read(SHARED_STATE_KEY)
                .map_err(|e| Failure::Guest(e.to_string()))?;
            if response.code != 0 || response.bytes != expected.result {
                Err(format!("response {} differs", response.code))
            } else if events != [(expected.topic, expected.event)] {
                Err(format!("{} events differ", events.len()))
            } else if stored.as_deref() != Some(expected.state) {
                Err("stored state differs".to_string())
            } else {
                Ok(())
            }
        }
        V2ActivityOutcome::Failure(failure) => Err(format!(
            "guest refused {:?} {}",
            failure.class(),
            describe(failure.reason().bytes())
        )),
        V2ActivityOutcome::Resource(refusal) => Err(format!("resource {refusal:?}")),
    };
    Ok((fuel, verdict))
}

/// One measured guest call.
struct Row {
    operation: Operation,
    height: u64,
    state: usize,
    fuel: u64,
    verdict: Result<(), String>,
}

/// The committed state of the journey's market, its next owner sequence and every measured
/// guest call.
struct Journey {
    module: ValidatedModule,
    bytes: Vec<u8>,
    owner_sequence: u64,
    rows: Vec<Row>,
}

/// Routed calls: decided in-process, then measured in the guest.
impl Journey {
    /// Routes `call` at `at` over the committed bytes, requires `Applied`, runs the guest over
    /// the same bytes and commits the routed next state.
    fn send(&mut self, call: &Req, at: u64) -> Checked {
        let envelope = call.encode()?;
        let ctx = call.context(at)?;
        let current = (!self.bytes.is_empty()).then_some(self.bytes.as_slice());
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; dispatch::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let mut result = vec![0; MAX_RESULT_BYTES];
        let routed = dispatch::route(
            &ctx,
            &envelope,
            current,
            Buffers {
                next: &mut next,
                scratch: &mut scratch,
                event: &mut event,
                result: &mut result,
            },
        )?;
        let name = call.operation.metadata().name;
        let Routed::Applied {
            operation,
            state_len,
            event_len,
            result_len,
            ..
        } = routed
        else {
            let detail = match routed {
                Routed::Refused { result_len } | Routed::Unchanged { result_len } => {
                    describe(&result[..result_len])
                }
                Routed::Applied { .. } => String::new(),
            };
            return Err(Failure::NotApplied(name, format!("{routed:?} {detail}")));
        };
        let mut topic = [0; 64];
        let topic_len = codec::event_topic(operation, &mut topic)?;
        let expected = Expected {
            state: &next[..state_len],
            topic: &topic[..topic_len],
            event: &event[..event_len],
            result: &result[..result_len],
        };
        let (fuel, verdict) = run_guest(&self.module, &ctx, current, &envelope, &expected)?;
        self.rows.push(Row {
            operation,
            height: at,
            state: self.bytes.len(),
            fuel,
            verdict,
        });
        next.truncate(state_len);
        self.bytes = next;
        Ok(())
    }
    fn parts(&self) -> CodecResult<Parts> {
        Parts::load(&self.bytes)
    }
    fn header(&self) -> CodecResult<MarketHeader> {
        self.parts()?.market()
    }
    fn revision(&self) -> CodecResult<u64> {
        Ok(decode_shared_state(&self.bytes)?.revision)
    }
    /// An owner-authorized F01 registry call carrying the expected revision first.
    fn owner(&mut self, operation: Operation, tail: &[u8], at: u64) -> Checked {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(tail);
        let call = Req {
            config: self.header()?.active_config_version,
            sequence: self.owner_sequence,
            ..req(
                operation,
                PrincipalId::new(OWNER)?,
                tag(0x20, 0, self.owner_sequence),
                payload,
            )
        };
        self.send(&call, at)?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// An owner-authorized F03 authority call bound to `frozen`.
    fn owner_bound(
        &mut self,
        operation: Operation,
        frozen: &FrozenBinding,
        payload: Vec<u8>,
        at: u64,
    ) -> Checked {
        let call = Req {
            sequence: self.owner_sequence,
            ..bound(
                operation,
                PrincipalId::new(OWNER)?,
                frozen,
                tag(0x30, 0, self.owner_sequence),
                payload,
            )
        };
        self.send(&call, at)?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// A native owner F02 call at the owner sequence.
    fn owner_worker(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> Checked {
        let call = Req {
            sequence: self.owner_sequence,
            ..req(
                operation,
                PrincipalId::new(OWNER)?,
                tag(0x21, 0, self.owner_sequence),
                payload,
            )
        };
        self.send(&call, at)?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// Permissionless `OPEN_EPOCH` of the clock epoch of `at`.
    fn open(&mut self, at: u64) -> Checked<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let preview = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Req {
            epoch: preview.epoch,
            config: preview.config.get(),
            roster: Presence::Present(preview.roster),
            ..req(
                dispatch::OPEN_EPOCH,
                principal(KEEPER)?,
                tag(0x60, 0, preview.epoch),
                Vec::new(),
            )
        };
        self.send(&call, at)?;
        Ok(preview)
    }
    /// Permissionless `ADVANCE_ACTIVATION`; the lifecycle becomes ACTIVE.
    fn advance(&mut self, at: u64) -> Checked {
        let header = self.header()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&header.activation_epoch.to_be_bytes());
        let call = Req {
            config: header.active_config_version,
            ..req(
                dispatch::ADVANCE_ACTIVATION,
                principal(KEEPER)?,
                [0x5f; 32],
                payload,
            )
        };
        self.send(&call, at)
    }
}

/// Producers of the prerequisites no routed operation writes.
impl Journey {
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
                &admission_ctx(market, owner, at),
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
                &admission_ctx(market, owner, at),
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
    /// Owner FUND of `amount` into the F06 reward state.
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
    /// F05 terminal result for every frozen worker, then `TerminalizeRewards`.
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
}

/// The opened epoch's task, evidence, score and challenge calls.
impl Journey {
    fn frozen_binding(&self, frozen: &Frozen) -> CodecResult<FrozenBinding> {
        let header = self.header()?;
        Ok(FrozenBinding {
            chain: header.deployment_chain_domain,
            program: header.program_id,
            market: header.market_id,
            epoch: frozen.epoch,
            config: frozen.config,
            roster: frozen.roster,
        })
    }
    fn evaluator_id(&self, n: u8) -> CodecResult<EvaluatorId> {
        derive_evaluator(self.header()?.market_id, principal(n)?, [n; 32])
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch's task region, then `SealEvidence` of each
    /// of `evaluators` under evaluator sequence 1.
    fn seal(&mut self, frozen: &Frozen, evaluators: &[u8], at: u64) -> Checked {
        let binding = self.frozen_binding(frozen)?;
        let set = SetBinding {
            market: binding.market,
            epoch: binding.epoch,
            config: binding.config,
            policy: frozen.policy,
            roster: binding.roster,
        };
        let section = PolicySection::decode(&self.parts()?.policy)?
            .task_region
            .to_vec();
        let digest = tasks::task_set_digest(&set, &TaskSet::decode(&section)?)?;
        let mut payload = u64s(&[binding.epoch, binding.config.get()]);
        payload.extend_from_slice(digest.as_bytes());
        let call = bound(
            dispatch::SEAL_TASK_SET,
            principal(KEEPER)?,
            &binding,
            tag(0x61, 0, binding.epoch),
            payload,
        );
        self.send(&call, at)?;
        let sealed = tasks::sealed_task_set(&decode_shared_state(&self.bytes)?, binding.epoch)?;
        for &n in evaluators {
            let mut claim = 1u16.to_be_bytes().to_vec();
            claim.extend_from_slice(&root(n, binding.epoch));
            claim.extend_from_slice(frozen.policy.as_bytes());
            claim.extend_from_slice(sealed.as_bytes());
            claim.extend_from_slice(rubric()?.as_bytes());
            claim.push(1);
            let call = Req {
                sequence: 1,
                expiry: ROLE_EXPIRY,
                ..bound(
                    dispatch::SealEvidence,
                    principal(n)?,
                    &binding,
                    tag(0xc5, n, 1),
                    claim,
                )
            };
            self.send(&call, at)?;
        }
        Ok(())
    }
    /// `ADMIT_TASK` of `worker` by the requester under `nonce`; returns the task id.
    fn admit_task(
        &mut self,
        frozen: &Frozen,
        worker: WorkerId,
        nonce: u8,
        at: u64,
    ) -> Checked<[u8; 32]> {
        let binding = self.frozen_binding(frozen)?;
        let requester = principal(REQUESTER)?;
        let mut payload = u64s(&[binding.epoch, binding.config.get()]);
        payload.extend_from_slice(frozen.policy.as_bytes());
        payload.extend_from_slice(binding.roster.as_bytes());
        payload.extend_from_slice(requester.as_bytes());
        payload.extend_from_slice(worker.as_bytes());
        payload.extend_from_slice(&METADATA);
        payload.extend_from_slice(&[nonce; 32]);
        payload.extend_from_slice(&[0xa0 + nonce; 32]);
        payload.extend_from_slice(&(at + 24).to_be_bytes());
        let call = bound(
            dispatch::ADMIT_TASK,
            requester,
            &binding,
            [0xb0 + nonce; 32],
            payload,
        );
        self.send(&call, at)?;
        Ok(derive_task(binding.market, binding.epoch, requester, [nonce; 32])?.bytes())
    }
    /// `ACCEPT_TASK` or `COMMIT_TASK_RESULT` of `task` by the worker owner at `sequence`.
    fn worker_step(
        &mut self,
        frozen: &Frozen,
        operation: Operation,
        owner: PrincipalId,
        task: [u8; 32],
        sequence: u64,
        at: u64,
    ) -> Checked {
        let binding = self.frozen_binding(frozen)?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&task);
        payload.extend_from_slice(&[0x60 + u8::try_from(sequence).map_err(|_| ARITHMETIC)?; 32]);
        let call = Req {
            sequence,
            ..bound(
                operation,
                owner,
                &binding,
                tag(0x70, owner.as_bytes()[0], sequence),
                payload,
            )
        };
        self.send(&call, at)
    }
    /// The frozen binding B of evaluator `n` at grant and key version 1.
    fn evaluator_binding(&self, frozen: &Frozen, n: u8) -> CodecResult<EvaluatorBinding> {
        Ok(EvaluatorBinding {
            frozen: self.frozen_binding(frozen)?,
            evaluator: self.evaluator_id(n)?,
            grant: version()?,
            key_version: version()?,
        })
    }
    /// `CommitScore` (sequence 2) of evaluator `n` over its signed report of `scores`.
    fn commit_score(&mut self, frozen: &Frozen, n: u8, scores: &[u8], at: u64) -> Checked {
        let binding = self.evaluator_binding(frozen, n)?;
        let body = ReportBody {
            binding,
            evidence: EvidenceRoot::new(root(n, frozen.epoch))?,
            scores: ScoreVector::Encoded(scores),
        };
        let digest: CommitmentDigest =
            codec::commitment_digest(&binding, codec::report_digest(&body)?, Salt32::new(SALT)?)?;
        let mut payload = vec![0; commitment::COMMIT_SCORE_BYTES];
        codec::encode_commit_score(
            &CommitScorePayload {
                binding,
                commitment: digest,
            },
            &mut payload,
        )?;
        let call = Req {
            sequence: 2,
            expiry: REVEAL_AT,
            ..bound(
                dispatch::CommitScore,
                principal(n)?,
                &binding.frozen,
                tag(0xa1, n, 2),
                payload,
            )
        };
        self.send(&call, at)
    }
    /// `RevealScore` (sequence 3) of evaluator `n`; returns the report digest.
    fn reveal_score(
        &mut self,
        frozen: &Frozen,
        n: u8,
        scores: &[u8],
        at: u64,
    ) -> Checked<ReportDigest> {
        let binding = self.evaluator_binding(frozen, n)?;
        let body = ReportBody {
            binding,
            evidence: EvidenceRoot::new(root(n, frozen.epoch))?,
            scores: ScoreVector::Encoded(scores),
        };
        let report = sign(body, &evaluator_key(n))?;
        let mut payload = vec![0; commitment::REVEAL_MAX_BYTES];
        let len = codec::encode_reveal_score(
            &RevealScorePayload {
                report: report.body,
                signature: report.signature,
                salt: Salt32::new(SALT)?,
            },
            &mut payload,
        )?;
        payload.truncate(len);
        let call = Req {
            sequence: 3,
            expiry: ROLE_EXPIRY,
            ..bound(
                dispatch::RevealScore,
                principal(n)?,
                &binding.frozen,
                tag(0xa2, n, 3),
                payload,
            )
        };
        self.send(&call, at)?;
        Ok(codec::report_digest(&report.body)?)
    }
    /// Permissionless `ChallengeAssessment` of `report` by `requester` under the derived id
    /// `H('PAXAI/evaluator-challenge/v1', chain || program || market || epoch || payload ||
    /// requester)`.
    fn challenge(
        &mut self,
        frozen: &Frozen,
        evaluator: EvaluatorId,
        report: ReportDigest,
        at: u64,
    ) -> Checked {
        let binding = self.frozen_binding(frozen)?;
        let requester = principal(CHALLENGER)?;
        let mut payload = evaluator.as_bytes().to_vec();
        payload.extend_from_slice(report.as_bytes());
        payload.push(3);
        payload.extend_from_slice(&[0x77; 32]);
        let mut preimage = CHAIN.to_vec();
        preimage.extend_from_slice(&PROGRAM);
        preimage.extend_from_slice(binding.market.as_bytes());
        preimage.extend_from_slice(&binding.epoch.to_be_bytes());
        preimage.extend_from_slice(&payload);
        preimage.extend_from_slice(requester.as_bytes());
        let id = codec::domain_hash("PAXAI/evaluator-challenge/v1", &preimage)?;
        let call = bound(
            dispatch::ChallengeAssessment,
            requester,
            &binding,
            id.bytes(),
            payload,
        );
        self.send(&call, at)
    }
}

/// The F02 lifecycle of one nominee and the expiry of another, after the epoch's reveals.
impl Journey {
    /// A native or delegate-signed F02 call of nominee `n` at its worker `sequence`.
    fn nominee(
        &mut self,
        operation: Operation,
        n: u8,
        sequence: u64,
        delegate: Option<SigningKey>,
        payload: Vec<u8>,
        at: u64,
    ) -> Checked {
        let call = Req {
            sequence,
            delegate,
            ..req(operation, principal(n)?, tag(0x80, n, sequence), payload)
        };
        self.send(&call, at)
    }
    fn worker_of(&self, n: u8) -> CodecResult<WorkerCurrent> {
        let state = decode_shared_state(&self.bytes)?;
        let (workers, _) = split_identity_section(state.section(Section::IdentityRoster)?)?;
        let worker = derive_worker(self.header()?.market_id, principal(n)?, [n; 32])?;
        WorkerTable::decode(workers)?.get(worker).ok_or(NOT_FOUND)
    }
    /// Owner `EnrollWorker` of nominee `n` whose consent expires at `expiry`.
    fn enroll_worker(&mut self, n: u8, expiry: u64, at: u64) -> Checked {
        let mut payload = principal(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&[n; 32]);
        payload.extend_from_slice(&public(&delegate_key(n)).0);
        payload.extend_from_slice(&[0x45; 32]);
        payload.extend_from_slice(&expiry.to_be_bytes());
        self.owner_worker(dispatch::EnrollWorker, payload, at)
    }
    /// Enrollment, acceptance, metadata, drain, undo, revocation, rotation and retirement of
    /// one nominee; returns the height of its last call.
    fn worker_lifecycle(&mut self, at: u64) -> Checked<u64> {
        let header = self.header()?;
        let key = delegate_key(NOMINEE);
        self.enroll_worker(NOMINEE, at + 100, at)?;
        let record = self.worker_of(NOMINEE)?;
        let consent = consent_digest(
            &header,
            record.worker,
            record.owner,
            record.delegate,
            1,
            1,
            record.metadata,
            record.expiry,
        )?;
        let mut accept = record.worker.as_bytes().to_vec();
        accept.extend_from_slice(&u64s(&[1, 1]));
        accept.extend_from_slice(record.metadata.as_bytes());
        accept.extend_from_slice(&key.sign(consent.as_bytes()).to_bytes());
        self.nominee(dispatch::AcceptEnrollment, NOMINEE, 1, None, accept, at)?;

        let published = at + 8;
        let record = self.worker_of(NOMINEE)?;
        let bytes = manifest(&header, &record, 2, published, published + 200)?;
        let summary = decode_manifest(&bytes)?;
        let mut publish = record.worker.as_bytes().to_vec();
        publish.extend_from_slice(&u64s(&[1, 2]));
        publish.extend_from_slice(summary.digest.as_bytes());
        publish.extend_from_slice(&u64s(&[published, published + 200]));
        publish.extend_from_slice(
            &u32::try_from(bytes.len())
                .map_err(|_| ARITHMETIC)?
                .to_be_bytes(),
        );
        publish.extend_from_slice(&bytes);
        self.nominee(
            dispatch::PublishMetadata,
            NOMINEE,
            2,
            Some(key),
            publish,
            published,
        )?;

        let worker = record.worker.as_bytes().to_vec();
        self.nominee(
            dispatch::SetDraining,
            NOMINEE,
            3,
            None,
            worker.clone(),
            published,
        )?;
        self.nominee(
            dispatch::UndoDrain,
            NOMINEE,
            4,
            None,
            worker.clone(),
            published,
        )?;
        let mut revoke = worker.clone();
        revoke.extend_from_slice(&1u64.to_be_bytes());
        revoke.push(1);
        revoke.extend_from_slice(&1u64.to_be_bytes());
        self.nominee(
            dispatch::RevokeDelegate,
            NOMINEE,
            5,
            None,
            revoke,
            published,
        )?;

        let rotated = rotated_key(NOMINEE);
        let metadata = MetadataDigest::new([0x46; 32])?;
        let consent_expiry = published + 50;
        let consent = consent_digest(
            &header,
            record.worker,
            record.owner,
            public(&rotated),
            2,
            2,
            metadata,
            consent_expiry,
        )?;
        let mut rotate = worker.clone();
        rotate.extend_from_slice(&u64s(&[1, 1]));
        rotate.extend_from_slice(&public(&rotated).0);
        rotate.extend_from_slice(metadata.as_bytes());
        rotate.extend_from_slice(&consent_expiry.to_be_bytes());
        rotate.extend_from_slice(&rotated.sign(consent.as_bytes()).to_bytes());
        self.nominee(
            dispatch::RotateDelegate,
            NOMINEE,
            6,
            None,
            rotate,
            published,
        )?;
        self.nominee(dispatch::RetireWorker, NOMINEE, 7, None, worker, published)?;
        Ok(published)
    }
    /// Owner enrollment of a second nominee at `lapse` that is never accepted, then its
    /// expiry once the consent window closes.
    fn expire_enrollment(&mut self, lapse: u64) -> Checked {
        let header = self.header()?;
        self.enroll_worker(LAPSED, lapse + 1, lapse)?;
        let candidate = self.worker_of(LAPSED)?;
        let mut expire = candidate.worker.as_bytes().to_vec();
        expire.extend_from_slice(candidate.proposal_digest(&header)?.as_bytes());
        expire.extend_from_slice(&candidate.expiry.to_be_bytes());
        self.owner_worker(dispatch::ExpireEnrollment, expire, lapse + 1)
    }
}

/// The F01 registry calls of the fresh market and the F03 authority calls of the owner.
impl Journey {
    /// `CREATE` at the origin, then operator appointment, metadata update and a staged policy
    /// the owner cancels.
    fn registry(&mut self) -> Checked {
        let owner = PrincipalId::new(OWNER)?;
        let mut create = OWNER.to_vec();
        create.extend_from_slice(&ASSET);
        create.extend_from_slice(
            derive_rewards_account(ProgramId::new(PROGRAM)?, AssetId::new(ASSET)?)?.as_bytes(),
        );
        create.extend_from_slice(&REFUND);
        create.push(0);
        create.extend_from_slice(&policy_bytes(&policy(1, 3)?)?);
        create.extend_from_slice(&[16; 32]);
        self.send(
            &Req {
                sequence: 1,
                ..req(dispatch::CREATE, owner, [1; 32], create)
            },
            ORIGIN,
        )?;
        let mut appoint = principal(OPERATOR)?.as_bytes().to_vec();
        appoint.push(PERMIT_METADATA | PERMIT_SUSPEND);
        appoint.extend_from_slice(&0u64.to_be_bytes());
        self.owner(dispatch::APPOINT_OPERATOR, &appoint, ORIGIN + 1)?;
        self.owner(dispatch::UPDATE_METADATA, &[0x17; 32], ORIGIN + 1)?;
        let mut staged = policy_bytes(&policy(2, 3)?)?;
        staged.extend_from_slice(&10u64.to_be_bytes());
        self.owner(dispatch::STAGE_POLICY, &staged, ORIGIN + 1)?;
        self.owner(dispatch::CANCEL_POLICY, &2u64.to_be_bytes(), ORIGIN + 1)?;
        Ok(())
    }
    /// Owner scheduling of a new evaluator, key rotation of evaluator 6 and revocation of
    /// evaluator 7, bound to the opened epoch.
    fn authority(&mut self, binding: &FrozenBinding, at: u64) -> Checked {
        let mut schedule = principal(SCHEDULED)?.as_bytes().to_vec();
        schedule.extend_from_slice(&[SCHEDULED; 32]);
        schedule.extend_from_slice(rubric()?.as_bytes());
        schedule.extend_from_slice(&public(&evaluator_key(SCHEDULED)).0);
        schedule.extend_from_slice(&u64s(&[1, 1, binding.epoch + 1, binding.epoch + 33]));
        self.owner_bound(dispatch::ScheduleEvaluator, binding, schedule, at)?;
        let mut rotate = self.evaluator_id(6)?.as_bytes().to_vec();
        rotate.extend_from_slice(&public(&rotated_key(6)).0);
        rotate.extend_from_slice(&u64s(&[2, 1, binding.epoch + 1]));
        self.owner_bound(dispatch::RotateEvaluatorKey, binding, rotate, at)?;
        let mut revoke = self.evaluator_id(7)?.as_bytes().to_vec();
        revoke.extend_from_slice(&1u64.to_be_bytes());
        revoke.extend_from_slice(&1u16.to_be_bytes());
        revoke.extend_from_slice(&[0x5e; 32]);
        self.owner_bound(dispatch::RevokeEvaluator, binding, revoke, at)?;
        Ok(())
    }
}

/// The journey from a fresh market: the registry, epoch 6 opening and activation, epoch 7
/// opening, its tasks, seals, scores and challenge, the F03 authority calls, the F02 worker
/// lifecycle, and finally the operator revocation and suspension.
fn journey(module: ValidatedModule) -> Checked<Journey> {
    let mut j = Journey {
        module,
        bytes: Vec::new(),
        owner_sequence: 2,
        rows: Vec::new(),
    };
    j.registry()?;

    let low = j.enroll(LOW, 130)?;
    for n in 2..=4 {
        j.evaluator(n, 129 + u64::from(n))?;
    }
    j.owner(dispatch::SCHEDULE_ACTIVATION, &6u64.to_be_bytes(), 134)?;
    j.fund(500, 135)?;
    let first = j.open(896)?;
    assert_eq!((first.epoch, first.workers, first.evaluators), (6, 1, 3));
    j.advance(897)?;
    let high = j.enroll(HIGH, 900)?;
    for n in 5..=7 {
        j.evaluator(n, 896 + u64::from(n))?;
    }
    j.seal(&first, &[], 960)?;
    j.terminalize(&first, &[low], 1008)?;

    let opened = j.open(1024)?;
    assert_eq!((opened.epoch, opened.workers, opened.evaluators), (7, 2, 6));
    let mut workers = [low, high];
    workers.sort_by_key(|w| w.worker);
    let [first_worker, second_worker] = workers;
    let task = j.admit_task(&opened, first_worker.worker, 1, WORK_AT)?;
    j.worker_step(
        &opened,
        dispatch::ACCEPT_TASK,
        first_worker.owner,
        task,
        1,
        WORK_AT + 1,
    )?;
    j.worker_step(
        &opened,
        dispatch::COMMIT_TASK_RESULT,
        first_worker.owner,
        task,
        2,
        WORK_AT + 2,
    )?;
    let cancelled = j.admit_task(&opened, first_worker.worker, 2, WORK_AT + 3)?;
    let binding = j.frozen_binding(&opened)?;
    j.send(
        &bound(
            dispatch::CANCEL_TASK,
            principal(REQUESTER)?,
            &binding,
            [0xbe; 32],
            cancelled.to_vec(),
        ),
        WORK_AT + 4,
    )?;
    j.seal(&opened, &[2, 3, 4, 5, 6, 7], COMMIT_AT)?;

    let scores = entries(&[
        (first_worker.worker, 500_000),
        (second_worker.worker, 600_000),
    ]);
    for n in REVEALERS {
        j.commit_score(&opened, n, &scores, COMMIT_AT + 2)?;
    }
    let mut reports = Vec::new();
    for n in REVEALERS {
        reports.push(j.reveal_score(&opened, n, &scores, REVEAL_AT)?);
    }
    let challenged = j.evaluator_id(REVEALERS[0])?;
    j.challenge(&opened, challenged, reports[0], REVEAL_AT + 2)?;

    j.authority(&binding, REVEAL_AT + 3)?;

    let retired = j.worker_lifecycle(REVEAL_AT + 4)?;
    j.expire_enrollment(retired + 1)?;
    j.owner(
        dispatch::REVOKE_OPERATOR,
        &1u64.to_be_bytes(),
        REVEAL_AT + 15,
    )?;
    j.owner(dispatch::SUSPEND, &[0x50; 32], REVEAL_AT + 15)?;
    Ok(j)
}

#[test]
fn every_routed_operation_fits_the_protocol_cpu_fuel_cap() -> Checked {
    let journey = journey(guest_module()?)?;
    println!(
        "{:<22} {:>6} {:>7} {:>10}  verdict (cap {DEFAULT_CPU_FUEL})",
        "operation", "height", "state", "fuel"
    );
    for row in &journey.rows {
        println!(
            "{:<22} {:>6} {:>7} {:>10}  {}",
            row.operation.metadata().name,
            row.height,
            row.state,
            row.fuel,
            row.verdict
                .as_ref()
                .map_or_else(Clone::clone, |()| "ok".to_string())
        );
    }
    let missing: Vec<&str> = ROUTED
        .iter()
        .filter(|op| !journey.rows.iter().any(|row| row.operation == **op))
        .map(|op| op.metadata().name)
        .collect();
    assert_eq!(missing, Vec::<&str>::new());
    let over: Vec<(&str, u64)> = journey
        .rows
        .iter()
        .filter(|row| row.fuel > DEFAULT_CPU_FUEL)
        .map(|row| (row.operation.metadata().name, row.fuel))
        .collect();
    assert_eq!(over, Vec::<(&str, u64)>::new());
    let failed: Vec<(&str, u64, &str)> = journey
        .rows
        .iter()
        .filter_map(|row| {
            row.verdict
                .as_ref()
                .err()
                .map(|reason| (row.operation.metadata().name, row.height, reason.as_str()))
        })
        .collect();
    assert_eq!(failed, Vec::<(&str, u64, &str)>::new());
    Ok(())
}
