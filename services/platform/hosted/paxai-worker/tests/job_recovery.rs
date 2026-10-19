//! AI.F02-T03 job recovery: atomic admission shared by replicas with a durable signed
//! acknowledgment, queue bounds, the finalized task-acceptance wait, fenced leases, runner
//! evidence validation, `UNKNOWN_EXECUTION` reconciliation, private retrieval, retention
//! tombstones and late results, all against a really opened market. Cases that need the real
//! model runner read its executable from `PAXAI_RUNNER` and fail when it is not provisioned.
use ed25519_dalek::{Signer, SigningKey};
use layerx_client::head::Head;
use layerx_paxai_worker::{
    auth::{
        acknowledgment_digest, admit, decode_acknowledgment, decode_service, encode_service,
        sign_metadata, verify_signed_metadata, Admission, JobReference, MetadataContext,
        MetadataPublication, Route, ServiceContext, ServiceError, ServiceOperation, ServiceRequest,
    },
    discovery::{
        AuthorityEvidence, FinalizedAuthority, IdentityEvidence, Readiness, VerifiedMetadata,
        MAX_FINALITY_LAG,
    },
    jobs::{
        decode_result, evidence_digest, result_manifest_digest, AdmissionView, Input, JobService,
        Lease, Outcome, QueryAnswer, ResultLocator, ServiceResult, LEASE_GRACE_MS,
    },
    metadata::{Capability, Endpoint, Manifest},
    runner::{
        processing_ms, Bounds, Completion, Dispatch, EvidenceFault, ProcessRunner, Progress,
        Report, Role, Segment, Usage, TOKENS, VECTOR_ELEMENTS,
    },
    store::{
        JobKey, JobState, JobStore, NewJob, Usage as QueueUsage, MAX_ENCRYPTED_BYTES, MAX_QUEUED,
        MAX_QUEUED_BYTES, MAX_RUNNING, RETENTION_MS, TOMBSTONE_DOMAIN,
    },
};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission as Membership, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        encode_roster, Envelope, Roster,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{ApplicationError, CodecResult, ARITHMETIC, NON_CANONICAL},
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    evidence::{
        chunk_count, encode_manifest, manifest_root, object_content_root, ArtifactContext,
        ArtifactError, ArtifactKind, ArtifactManifest, Items, Privacy, MAX_MANIFEST_BYTES,
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        read_state_chunk, CaptureFacts, FinalityEvidence, QueryError, ReadProof, StateCapture,
    },
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome as Registered, PolicySection},
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{TaskBinding, TaskStatus},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorRosterEntry, MarketId,
        PolicyDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId,
        ResultDigest, RosterDigest, RubricDigest, StateDigest, TaskId, Version, WorkerId,
        WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::Barrier;
use std::thread;
use std::time::Duration;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const ROOT: [u8; 32] = [0xA1; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 1000;
const DELEGATE_SEED: [u8; 32] = [0xD1; 32];
const CUSTOMER_SEED: [u8; 32] = [0xC9; 32];
const VALID_FROM: u64 = 1140;
const EXPIRY: u64 = 1180;
const HEIGHT: u64 = 1150;
const TASK_EXPIRY: u64 = 1190;
const CHUNK_RESPONSE_MAX: usize = 8_244;

const DEFAULT_URI: &str = "https://localhost/paxai/v1";
const NOW: u64 = 1_700_000_000_000;
const PAYLOAD_BYTES: usize = 1_024;
const RUNNER_ENV: &str = "PAXAI_RUNNER";

enum Failure {
    Application(ApplicationError),
    Service(ServiceError),
    Artifact(ArtifactError),
    Query(QueryError),
    Io(io::ErrorKind),
    Unexpected(&'static str),
}
impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Service(error) => write!(f, "service refusal {error:?}"),
            Self::Artifact(error) => write!(f, "artifact refusal {error:?}"),
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
impl From<ArtifactError> for Failure {
    fn from(error: ArtifactError) -> Self {
        Self::Artifact(error)
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
fn delegate() -> SigningKey {
    SigningKey::from_bytes(&DELEGATE_SEED)
}
fn customer() -> SigningKey {
    SigningKey::from_bytes(&CUSTOMER_SEED)
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// Market, worker owner and worker of the journey.
fn identities() -> CodecResult<(MarketId, PrincipalId, WorkerId)> {
    let market = derive_market(ChainDomain::new(CHAIN)?, ProgramId::new(PROGRAM)?)?;
    let owner = principal(1)?;
    Ok((market, owner, derive_worker(market, owner, [1; 32])?))
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
    let Registered::Applied { state, .. } = registry_ops::apply(
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
}

fn admission_context(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height: at,
    }
}
/// Market-owner approval bound to the required effective epoch.
fn approve(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    owner: PrincipalId,
    delegate: PublicKey32,
    at: u64,
) -> CodecResult<(u64, Digest32)> {
    let effective = table.required_effective_epoch(&admission_context(market, owner, at))?;
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
        expiry_height: epoch_height(market.origin_height, effective, 64),
    };
    let digest = table.approve(
        &admission_context(market, market.owner_principal, at),
        &terms,
    )?;
    Ok((effective, digest))
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
    /// A real owner-authorized F01 registry operation.
    fn owner_op(&mut self, operation: Operation, payload: &[u8], at: u64) -> CodecResult<()> {
        let call = Call {
            operation,
            actor: PrincipalId::new(OWNER)?,
            epoch: 0,
            config: self.header()?.active_config_version,
            roster: Presence::Absent,
            sequence: self.owner_sequence,
            request: 0x20 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?,
            payload,
        };
        let encoded = call.encode()?;
        let current = decode_shared_state(&self.bytes)?;
        let mut section = vec![0; F01_SECTION_CAP];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Registered::Applied { state, .. } = registry_ops::apply(
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
    fn schedule(&mut self, epoch: u64, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, &payload, at)
    }
}

/// Worker, evaluator and funding producers of the market journey.
impl World {
    /// F02 record, worker replay slot and F08 approval plus owner acceptance.
    fn enroll(&mut self, record: &WorkerCurrent, at: u64) -> CodecResult<WorkerRosterEntry> {
        self.edit(|parts, market| {
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
            let participant = Participant::Worker(record.worker);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                record.owner,
                record.delegate,
                at,
            )?;
            parts.admission.admit(
                &admission_context(market, record.owner, at),
                &Membership {
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
    /// F03 nomination accepted through the signed F08 evaluator consent of its delegate.
    fn evaluator(&mut self, n: u8, at: u64) -> CodecResult<EvaluatorRosterEntry> {
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
            let grant = evaluator_grant(market, owner, n, signing_key, effective)?;
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
                expiry_height: epoch_height(market.origin_height, effective, 64),
            };
            let mut signed = [0u8; 362];
            consent.encode(&mut signed)?;
            let mut payload = signed.to_vec();
            payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
            admit_evaluator(
                &mut parts.admission,
                &admission_context(market, owner, at),
                &grant,
                consent.request,
                &payload,
            )?;
            parts.insert_grant(grant, n)?;
            Ok(EvaluatorRosterEntry {
                evaluator: grant.evaluator,
                owner: grant.principal,
                grant: grant.grant_version,
                key_version: grant.key_version,
                public_key: grant.signing_key,
                rubric: grant.rubric,
            })
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
}

/// The permissionless `ADVANCE_ACTIVATION` and `OPEN_EPOCH` calls.
impl World {
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
    /// Opens the clock epoch of `at` naming the previewed frozen config and roster.
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

fn capability(model: u8) -> Capability {
    Capability {
        kind: 1,
        mode: 1,
        model: [model; 32],
        model_manifest: [0x32; 32],
        tokenizer: [0x33; 32],
        input_schema: [0x34; 32],
        output_schema: [0x35; 32],
        max_input_bytes: 65_536,
        max_output_bytes: 65_536,
        max_input_units: 4_096,
        max_output_units: 4_096,
        unit_kind: 1,
        latency_ms: 30_000,
        concurrency: 4,
        determinism: 1,
    }
}
fn endpoint(uri: &str, pin: [u8; 32]) -> Endpoint {
    Endpoint {
        id: 1,
        uri: uri.to_owned(),
        spki_sha256: pin,
    }
}
fn manifest(
    revision: u64,
    capabilities: Vec<Capability>,
    endpoints: Vec<Endpoint>,
) -> Checked<Manifest> {
    let (market, owner, worker) = identities()?;
    Ok(Manifest {
        market,
        worker,
        owner,
        generation: 1,
        key_version: 1,
        revision,
        valid_from: VALID_FROM,
        expiry: EXPIRY,
        deployment: Digest32::new([0xDE; 32])?,
        capabilities,
        endpoints,
        privacy_policy: Digest32::new([0x9A; 32])?,
        service_terms: Digest32::new([0x7E; 32])?,
    })
}
fn metadata_context() -> Checked<MetadataContext> {
    let (market, owner, worker) = identities()?;
    Ok(MetadataContext {
        chain: ChainDomain::new(CHAIN)?,
        program: ProgramId::new(PROGRAM)?,
        market,
        worker,
        owner,
        delegate: public(&delegate()),
    })
}
fn publication(manifest: &[u8], expected_revision: u64) -> Checked<MetadataPublication<'_>> {
    Ok(MetadataPublication {
        manifest,
        expected_revision,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: expected_revision + 1,
        expiry: EXPIRY,
        request: RequestId::new([0x71; 32])?,
    })
}
fn sign(manifest: &Manifest, expected_revision: u64) -> Checked<Vec<u8>> {
    let bytes = manifest.encode()?;
    Ok(sign_metadata(
        &metadata_context()?,
        &publication(&bytes, expected_revision)?,
        &delegate(),
    )?)
}

/// The opened journey market and the evidence a discovery client holds about it.
struct Market {
    world: World,
    roster: Vec<u8>,
    signed: Vec<u8>,
}

/// Real `CREATE`, `SCHEDULE_ACTIVATION`, worker enrollment bound to the signed manifest digest,
/// three accepted evaluators, `FUND`, `ADVANCE_ACTIVATION` and `OPEN_EPOCH` 1, whose Work window
/// is 1128..1192.
fn opened(endpoints: Vec<Endpoint>) -> Checked<Market> {
    let (market, owner, worker) = identities()?;
    let manifest = manifest(1, vec![capability(0x31)], endpoints)?;
    let signed = sign(&manifest, 0)?;
    let (_, digest) = Manifest::decode(&manifest.encode()?)?;
    let mut world = World::create(ORIGIN)?;
    world.schedule(1, 1001)?;
    let entry = world.enroll(
        &WorkerCurrent {
            worker,
            owner,
            delegate: public(&delegate()),
            metadata: digest,
            generation: 1,
            key_version: 1,
            metadata_revision: 1,
            valid_from: VALID_FROM,
            expiry: EXPIRY,
            revocation_sequence: 0,
            effective_epoch: 0,
            last_sequence: 0,
            last_request_id: [0; 32],
            last_request_digest: [0; 32],
            last_result_digest: [0; 32],
            state: WorkerState::Enrolled,
            slot: 0,
            last_metadata_height: 1002,
        },
        1002,
    )?;
    let mut evaluators = Vec::new();
    for n in 2..=4u8 {
        evaluators.push(world.evaluator(n, 1004 + u64::from(n))?);
    }
    world.fund(500, 1128)?;
    world.advance(1128)?;
    world.open(1129)?;
    evaluators.sort_by_key(|e| e.evaluator);
    let mut roster = vec![0; 54 + 176 + 144 * evaluators.len()];
    let n = encode_roster(
        &Roster {
            market,
            epoch: 1,
            config: version()?,
            workers: &[entry],
            evaluators: &evaluators,
        },
        &mut roster,
    )?;
    roster.truncate(n);
    Ok(Market {
        world,
        roster,
        signed,
    })
}

fn chunk_payload(revision: u64, pinned: Option<StateDigest>, offset: u32) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.map_or([0; 32], StateDigest::bytes));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&8_192u16.to_be_bytes());
    payload
}

/// One complete verified capture of `state` through the real chunked read path.
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
            &chunk_payload(revision, Some(pinned), offset),
            &mut out,
        )?;
        capture.accept(&proof, &out[..n])?;
    }
    let (bytes, facts) = capture.finish()?;
    Ok((bytes.to_vec(), facts))
}

/// Evidence knobs of one finalized authority binding.
#[derive(Clone, Copy)]
struct Knobs {
    sealed: u64,
    rank: u8,
    frozen: bool,
    owner_height: u64,
    roster: bool,
    market: MarketId,
    worker: WorkerId,
}
struct View {
    bytes: Vec<u8>,
    facts: CaptureFacts,
    roster: Vec<u8>,
    at: u64,
}
fn view(state: &[u8], roster: &[u8], at: u64) -> Checked<View> {
    let (bytes, facts) = capture(state, at)?;
    Ok(View {
        bytes,
        facts,
        roster: roster.to_vec(),
        at,
    })
}
impl View {
    fn knobs(&self) -> CodecResult<Knobs> {
        let (market, _, worker) = identities()?;
        Ok(Knobs {
            sealed: self.at + MAX_FINALITY_LAG,
            rank: 4,
            frozen: false,
            owner_height: self.at,
            roster: true,
            market,
            worker,
        })
    }
    fn bind(&self, knobs: Knobs) -> Result<FinalizedAuthority, ServiceError> {
        let (_, owner, _) = identities()?;
        let finality = FinalityEvidence {
            native_state_root: Digest32::new(ROOT)?,
            checkpoint: Digest32::new([0xC1; 32])?,
            settlement: Presence::Present(Digest32::new([0xD3; 32])?),
            rank: knobs.rank,
        };
        FinalizedAuthority::bind(
            &AuthorityEvidence {
                state: &self.bytes,
                facts: &self.facts,
                finality: &finality,
                head: Head {
                    chain_sequence: 77,
                    sealed_batch: knobs.sealed,
                    finalised_checkpoint: [0xC1; 32],
                },
                owner: IdentityEvidence {
                    principal: owner,
                    primary_key: PublicKey32([0x0E; 32]),
                    frozen: knobs.frozen,
                    execution_height: knobs.owner_height,
                },
                roster: knobs.roster.then_some(self.roster.as_slice()),
                observed_ms: 5_000,
            },
            ChainDomain::new(CHAIN)?,
            ProgramId::new(PROGRAM)?,
            knobs.market,
            knobs.worker,
        )
    }
    fn authority(&self) -> Checked<FinalizedAuthority> {
        Ok(self.bind(self.knobs()?)?)
    }
}
fn authority_at(market: &Market, at: u64) -> Checked<FinalizedAuthority> {
    view(&market.world.bytes, &market.roster, at)?.authority()
}
fn verified(authority: &FinalizedAuthority, signed: &[u8]) -> Checked<VerifiedMetadata> {
    Ok(authority.bind_metadata(verify_signed_metadata(
        signed,
        &authority.metadata_context(),
    )?)?)
}
fn customer_at(at: u64) -> Checked<IdentityEvidence> {
    Ok(IdentityEvidence {
        principal: principal(9)?,
        primary_key: public(&customer()),
        frozen: false,
        execution_height: at,
    })
}

/// A customer submit bound to `authority` and naming `capability` of the current revision.
#[derive(Clone, Copy)]
struct Submit {
    context: ServiceContext,
    request: ServiceRequest,
    task: TaskBinding,
}
fn submit(authority: &FinalizedAuthority, capability: &Capability) -> Checked<Submit> {
    let binding = authority.worker_binding();
    let request = ServiceRequest {
        receiver: binding.worker,
        task: TaskId::new([0x7A; 32])?,
        generation: 1,
        key_version: 1,
        metadata_revision: binding.metadata_revision,
        method: Route::Submit.code(),
        route: Route::Submit.code(),
        capability: capability.digest()?,
        workload_policy: authority.policy(),
        model: Digest32::new(capability.model)?,
        input_commitment: Digest32::new([0x1C; 32])?,
        payload_bytes: 1_024,
        max_output_bytes: 2_048,
        max_units: 256,
        deadline_ms: 0,
        task_expiry: TASK_EXPIRY,
        evaluation_access: Digest32::new([0xEA; 32])?,
        result_key: Some([0x4B; 32]),
    };
    let context = ServiceContext {
        chain: binding.chain,
        program: binding.program,
        market: binding.market,
        actor: principal(9)?,
        epoch: 1,
        config: authority.config(),
        roster: authority.roster(),
        sequence: 1,
        expiry: TASK_EXPIRY,
        request: RequestId::new([0x88; 32])?,
    };
    let task = TaskBinding {
        task: request.task,
        requester: context.actor,
        worker: binding.worker,
        input: request.input_commitment,
        deadline: TASK_EXPIRY,
        status: TaskStatus::Admitted,
        acknowledgement: None,
        result: None,
        admission: Digest32::new([0xAD; 32])?,
    };
    Ok(Submit {
        context,
        request,
        task,
    })
}
impl Submit {
    fn signed(&self, key: &SigningKey) -> Checked<Vec<u8>> {
        Ok(encode_service(
            ServiceOperation::SubmitJob,
            &self.context,
            &self.request.encode()?,
            key,
        )?)
    }
    fn admit(
        &self,
        authority: &FinalizedAuthority,
        evidence: &IdentityEvidence,
        metadata: &VerifiedMetadata,
    ) -> Checked<Result<Admission, ServiceError>> {
        let envelope = decode_service(&self.signed(&customer())?)?;
        Ok(admit(
            &envelope,
            &self.request,
            authority,
            evidence,
            metadata,
            &self.task,
        ))
    }
    fn with(&self, change: impl FnOnce(&mut Self)) -> Self {
        let mut changed = *self;
        change(&mut changed);
        changed
    }
}

/// The opened market at `HEIGHT` with its verified revision-1 metadata.
struct Admitting {
    market: Market,
    authority: FinalizedAuthority,
    metadata: VerifiedMetadata,
    submit: Submit,
}
fn admitting() -> Checked<Admitting> {
    let market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    let authority = authority_at(&market, HEIGHT)?;
    let metadata = verified(&authority, &market.signed)?;
    let submit = submit(&authority, &capability(0x31))?;
    Ok(Admitting {
        market,
        authority,
        metadata,
        submit,
    })
}

/// A private store directory removed when the test ends.
struct Scratch(PathBuf);
impl Scratch {
    fn new(name: &str) -> Checked<Self> {
        let path =
            std::env::temp_dir().join(format!("paxai-job-recovery-{}-{name}", std::process::id()));
        if path.exists() {
            fs::remove_dir_all(&path)?;
        }
        fs::create_dir_all(&path)?;
        Ok(Self(path))
    }
    fn store(&self) -> PathBuf {
        self.0.join("store")
    }
    /// The configured runner path when no runner executable is installed.
    fn absent(&self) -> PathBuf {
        self.0.join("runner-not-installed")
    }
}
impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn service(root: &Path, runner: PathBuf) -> Checked<JobService> {
    Ok(JobService::new(
        JobStore::open(root)?,
        ProcessRunner::new(runner, Vec::new(), Duration::from_secs(5)),
        delegate(),
    ))
}

fn real_runner() -> Checked<PathBuf> {
    std::env::var_os(RUNNER_ENV)
        .map(PathBuf::from)
        .ok_or(Failure::Unexpected(
            "real model runner executable: PAXAI_RUNNER is not provisioned",
        ))
}

/// Encoded encrypted F09 manifest of `object` bound to `task` of epoch 1.
fn object_manifest(
    kind: ArtifactKind,
    publisher: PrincipalId,
    task: TaskId,
    policy: PolicyDigest,
    object: &[u8],
) -> Checked<Vec<u8>> {
    let byte_length = u64::try_from(object.len()).map_err(|_| ARITHMETIC)?;
    let chunks = chunk_count(byte_length)?;
    let mut scratch = vec![[0; 32]; usize::try_from(chunks).map_err(|_| ARITHMETIC)?];
    let content_root = object_content_root(object, &mut scratch)?;
    let (market, _, _) = identities()?;
    let manifest = ArtifactManifest {
        kind,
        privacy: Privacy::Encrypted,
        context: ArtifactContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            market,
            policy,
        },
        epoch: 1,
        publisher,
        subject: task.bytes(),
        byte_length,
        chunk_count: chunks,
        content_root,
        parents: Items::Typed(&[]),
        declaration_root: [0; 32],
        reproduction_root: [0; 32],
        access_policy_root: [0xAC; 32],
        not_after_height: 4_000,
    };
    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let n = encode_manifest(&manifest, &mut out)?;
    out.truncate(n);
    Ok(out)
}
fn root(manifest: &[u8]) -> Checked<Digest32> {
    Ok(Digest32::new(manifest_root(manifest)?.bytes())?)
}

/// One customer job: the signed submit, its encrypted input and its admitted F01 task.
struct Order {
    signed: Vec<u8>,
    manifest: Vec<u8>,
    payload: Vec<u8>,
    submit: Submit,
}
fn order(fixture: &Admitting, request: u8, sequence: u64, max_output_bytes: u32) -> Checked<Order> {
    let payload = vec![request ^ 0x5A; PAYLOAD_BYTES];
    let task = TaskId::new([request; 32])?;
    let manifest = object_manifest(
        ArtifactKind::Input,
        principal(9)?,
        task,
        fixture.authority.policy(),
        &payload,
    )?;
    let input = root(&manifest)?;
    let id = RequestId::new([request; 32])?;
    let submit = fixture.submit.with(|s| {
        s.request.task = task;
        s.request.input_commitment = input;
        s.request.max_output_bytes = max_output_bytes;
        s.context.request = id;
        s.context.sequence = sequence;
        s.task.task = task;
        s.task.input = input;
    });
    Ok(Order {
        signed: submit.signed(&customer())?,
        manifest,
        payload,
        submit,
    })
}
impl Order {
    fn input(&self) -> Input<'_> {
        Input {
            manifest: &self.manifest,
            payload: &self.payload,
        }
    }
    fn view<'a>(
        &'a self,
        authority: &'a FinalizedAuthority,
        customer: &'a IdentityEvidence,
        metadata: &'a VerifiedMetadata,
    ) -> AdmissionView<'a> {
        AdmissionView {
            authority,
            customer,
            metadata,
            task: &self.submit.task,
        }
    }
    fn key(&self) -> Checked<JobKey> {
        Ok(JobKey::derive(
            self.submit.context.market,
            self.submit.context.actor,
            self.submit.context.request,
        )?)
    }
    fn accepted(&self, acknowledgment: &[u8]) -> Checked<TaskBinding> {
        Ok(TaskBinding {
            status: TaskStatus::Accepted,
            acknowledgement: Some(acknowledgment_digest(acknowledgment)?),
            ..self.submit.task
        })
    }
    /// A signed query or cancel of this job by `actor` under `key`.
    fn reference(
        &self,
        route: Route,
        actor: PrincipalId,
        sequence: u64,
        key: &SigningKey,
    ) -> Checked<Vec<u8>> {
        let payload = JobReference {
            receiver: self.submit.request.receiver,
            task: self.submit.request.task,
            generation: 1,
            key_version: 1,
            metadata_revision: 1,
            method: route.code(),
            route: route.code(),
            original: self.submit.context.request,
        }
        .encode();
        let context = ServiceContext {
            actor,
            sequence,
            ..self.submit.context
        };
        Ok(encode_service(route.operation(), &context, &payload, key)?)
    }
    /// The F09 RESULT object a worker returns for this job.
    fn output(&self, fixture: &Admitting, bytes: usize) -> Checked<(Vec<u8>, Vec<u8>)> {
        let output = vec![0x0F; bytes];
        let manifest = object_manifest(
            ArtifactKind::Result,
            principal(1)?,
            self.submit.request.task,
            fixture.authority.policy(),
            &output,
        )?;
        Ok((output, manifest))
    }
}

/// Submits `order` through `service` with the fixture's finalized view at `HEIGHT`.
fn place(service: &JobService, fixture: &Admitting, order: &Order) -> Checked<Vec<u8>> {
    let customer = customer_at(HEIGHT)?;
    let receipt = service.submit(
        &order.signed,
        order.input(),
        &order.view(&fixture.authority, &customer, &fixture.metadata),
        NOW,
    )?;
    Ok(receipt.acknowledgment)
}

fn tokens(input: u64, output: u64) -> Vec<Segment> {
    vec![
        Segment {
            role: Role::Input,
            unit_kind: TOKENS,
            count: input,
        },
        Segment {
            role: Role::Output,
            unit_kind: TOKENS,
            count: output,
        },
    ]
}

fn completion(output: Vec<u8>, manifest: Vec<u8>, segments: Vec<Segment>) -> Completion {
    Completion {
        succeeded: true,
        error_code: 0,
        start_ns: 1_000_000,
        finish_ns: 4_999_999,
        started_at_ms: NOW + 100,
        finished_at_ms: NOW + 104,
        segments,
        output,
        output_manifest: manifest,
    }
}

fn report(lease: Lease, completion: Completion) -> Checked<Vec<u8>> {
    Ok(Report {
        job: lease.key,
        fence: lease.fence,
        progress: Progress::Finished(completion),
    }
    .encode()?)
}

fn result_of(service: &JobService, key: JobKey) -> Checked<ServiceResult> {
    let saved = service
        .store()
        .job(key)?
        .result
        .ok_or(Failure::Unexpected("missing result"))?;
    Ok(decode_result(&saved, public(&delegate()))?.0)
}

fn identity(n: u8, frozen: bool) -> Checked<IdentityEvidence> {
    Ok(IdentityEvidence {
        principal: principal(n)?,
        primary_key: public(&SigningKey::from_bytes(&[n; 32])),
        frozen,
        execution_height: HEIGHT,
    })
}

fn refused<T>(result: Checked<T>) -> Option<ServiceError> {
    match result.err()? {
        Failure::Service(error) => Some(error),
        _ => None,
    }
}

#[test]
fn a05_replicas_share_one_admission_and_acknowledgment() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a05")?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let requester = customer_at(HEIGHT)?;
    let replicas = [
        service(&dir.store(), dir.absent())?,
        service(&dir.store(), dir.absent())?,
    ];
    let barrier = Barrier::new(replicas.len());
    let view = order.view(&fixture.authority, &requester, &fixture.metadata);
    let receipts = thread::scope(|scope| {
        let handles: Vec<_> = replicas
            .iter()
            .map(|replica| {
                let (barrier, order, view) = (&barrier, &order, &view);
                scope.spawn(move || {
                    barrier.wait();
                    replica.submit(&order.signed, order.input(), view, NOW)
                })
            })
            .collect();
        handles
            .into_iter()
            .map(|handle| {
                handle
                    .join()
                    .map_err(|_| Failure::Unexpected("replica panicked"))
            })
            .collect::<Result<Vec<_>, _>>()
    })?;
    let (first, second) = (receipts[0].clone()?, receipts[1].clone()?);
    assert_eq!(first, second);
    let (ack, _) = decode_acknowledgment(&first.acknowledgment, public(&delegate()))?;
    assert_eq!(
        (ack.sequence, ack.request_commitment, ack.request),
        (
            1,
            decode_service(&order.signed)?.digest,
            order.submit.context.request
        )
    );
    let store = replicas[0].store();
    assert_eq!(
        store
            .by_request(order.submit.context.market, order.submit.context.request)?
            .len(),
        1
    );
    assert_eq!(
        store.usage()?,
        QueueUsage {
            queued: 1,
            running: 0,
            queued_bytes: 1_024
        }
    );
    let changed = |sequence| -> Checked<Vec<u8>> {
        order
            .submit
            .with(|s| {
                s.request.max_output_bytes = 1_024;
                s.context.sequence = sequence;
            })
            .signed(&customer())
    };
    for sequence in [1, 2] {
        assert_eq!(
            replicas[1]
                .submit(&changed(sequence)?, order.input(), &view, NOW)
                .map(|_| ()),
            Err(ServiceError::IdempotencyConflict)
        );
    }
    let later = authority_at(&fixture.market, 1_191)?;
    let late_customer = customer_at(1_191)?;
    let again = replicas[1].submit(
        &order.signed,
        order.input(),
        &order.view(&later, &late_customer, &fixture.metadata),
        NOW + 60_000,
    )?;
    assert_eq!(again, first);
    assert_eq!(
        replicas
            .iter()
            .map(|r| r.runner().invocations())
            .sum::<u64>(),
        0
    );
    Ok(())
}

#[test]
fn a05_real_runner_executes_once_across_replicas() -> Checked {
    let runner = real_runner()?;
    let fixture = admitting()?;
    let dir = Scratch::new("a05-runner")?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let replicas = [
        service(&dir.store(), runner.clone())?,
        service(&dir.store(), runner)?,
    ];
    let ack = place(&replicas[0], &fixture, &order)?;
    assert_eq!(place(&replicas[1], &fixture, &order)?, ack);
    let task = order.accepted(&ack)?;
    let key = order.key()?;
    let barrier = Barrier::new(replicas.len());
    let outcomes = thread::scope(|scope| {
        let handles: Vec<_> = replicas
            .iter()
            .map(|replica| {
                let (barrier, task, fixture) = (&barrier, &task, &fixture);
                scope.spawn(move || {
                    barrier.wait();
                    replica.dispatch(key, task, &fixture.authority, NOW + 1)
                })
            })
            .collect();
        handles
            .into_iter()
            .map(|handle| {
                handle
                    .join()
                    .map_err(|_| Failure::Unexpected("replica panicked"))
            })
            .collect::<Result<Vec<_>, _>>()
    })?;
    assert_eq!(
        outcomes
            .iter()
            .filter(|o| **o == Err(ServiceError::IdempotencyConflict))
            .count(),
        1
    );
    assert_eq!(
        replicas
            .iter()
            .map(|r| r.runner().invocations())
            .sum::<u64>(),
        1
    );
    Ok(())
}

#[test]
fn a05_retention_tombstone_never_reexecutes() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("tombstone")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    place(&worker, &fixture, &order)?;
    let key = order.key()?;
    let cancel = order.reference(Route::Cancel, principal(9)?, 2, &customer())?;
    assert_eq!(
        worker.cancel(&cancel, &fixture.authority, &customer_at(HEIGHT)?, NOW),
        Ok(JobState::Ended(Outcome::CancelledBeforeStart))
    );
    assert_eq!(
        result_of(&worker, key)?,
        ServiceResult::admitted(
            &worker.store().job(key)?.record.admission,
            Outcome::CancelledBeforeStart
        )
    );
    assert_eq!(worker.purge(NOW + RETENTION_MS - 1, HEIGHT)?, 0);
    assert_eq!(worker.purge(NOW + RETENTION_MS, HEIGHT)?, 1);
    let mut preimage = key.digest().bytes().to_vec();
    preimage.extend_from_slice(decode_service(&order.signed)?.digest.as_bytes());
    assert_eq!(
        worker.store().tombstone(key)?,
        Some(
            codec::domain_hash(TOMBSTONE_DOMAIN, &preimage)?
                .bytes()
                .to_vec()
        )
    );
    assert_eq!(
        worker.store().job(key).map(|_| ()),
        Err(ServiceError::NotFound)
    );
    assert_eq!(
        worker
            .store()
            .by_request(order.submit.context.market, order.submit.context.request)?
            .len(),
        0
    );
    assert_eq!(
        refused(place(&worker, &fixture, &order)),
        Some(ServiceError::IdempotencyConflict)
    );
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

#[test]
fn a08_output_above_bound_is_failed_evidence() -> Checked {
    let bounds = Bounds {
        unit_kind: TOKENS,
        max_output_bytes: 1_024,
        max_input_units: 4_096,
        max_units: 256,
    };
    let fixture = admitting()?;
    let order = order(&fixture, 0x31, 1, 1_024)?;
    let (big, big_manifest) = order.output(&fixture, 1_025)?;
    let oversized = completion(big, big_manifest, tokens(100, 200));
    assert_eq!(
        oversized.validate(&bounds),
        Err(EvidenceFault::OutputTooLarge)
    );
    assert_eq!(EvidenceFault::OutputTooLarge.error().code(), 21);
    let (fits, fits_manifest) = order.output(&fixture, 1_024)?;
    let exact = completion(fits.clone(), fits_manifest.clone(), tokens(100, 200));
    assert_eq!(
        exact.validate(&bounds),
        Ok(Usage {
            input_units: 100,
            output_units: 200,
            processing_ms: 3
        })
    );
    let vectors = completion(
        fits,
        fits_manifest,
        vec![Segment {
            role: Role::Output,
            unit_kind: VECTOR_ELEMENTS,
            count: 200,
        }],
    );
    assert_eq!(vectors.validate(&bounds), Err(EvidenceFault::UnitMismatch));
    assert_eq!(
        EvidenceFault::UnitMismatch.error(),
        ServiceError::CapabilityMismatch
    );
    let dir = Scratch::new("a08")?;
    let worker = service(&dir.store(), dir.absent())?;
    let ack = place(&worker, &fixture, &order)?;
    let key = order.key()?;
    let lease = worker.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?;
    let evidence = report(lease, oversized)?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 2),
        Ok(JobState::Ended(Outcome::Failed))
    );
    let admission = worker.store().job(key)?.record.admission;
    assert_eq!(
        result_of(&worker, key)?,
        ServiceResult {
            started_at_ms: NOW + 100,
            finished_at_ms: NOW + 104,
            error_code: 21,
            evidence: Some(evidence_digest(&evidence)?),
            ..ServiceResult::admitted(&admission, Outcome::Failed)
        }
    );
    assert_eq!(worker.store().retained(key, false)?, vec![evidence]);
    assert_eq!(worker.store().output(key)?, None);
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

#[test]
fn a09_restart_reconciles_unknown_execution_without_reexecution() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a09")?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let key = order.key()?;
    let lease = {
        let crashed = service(&dir.store(), dir.absent())?;
        let ack = place(&crashed, &fixture, &order)?;
        crashed.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?
    };
    assert_eq!(lease, Lease { key, fence: 1 });
    let restarted = service(&dir.store(), dir.absent())?;
    assert_eq!(
        restarted.reconcile(NOW + 2)?,
        vec![(key, JobState::Running)]
    );
    assert_eq!(restarted.runner().invocations(), 0);
    let expiry = NOW + 1 + 30_000 + LEASE_GRACE_MS;
    let unknown = JobState::Ended(Outcome::UnknownExecution);
    assert_eq!(restarted.reconcile(expiry)?, vec![(key, unknown)]);
    assert_eq!(restarted.runner().invocations(), 1);
    let ack = restarted.store().job(key)?.record.acknowledgment;
    assert_eq!(
        restarted.claim(key, &order.accepted(&ack)?, &fixture.authority, expiry),
        Err(ServiceError::UnknownExecution)
    );
    assert_eq!(
        restarted.commit_record(key, &fixture.authority),
        Err(ServiceError::UnknownExecution)
    );
    assert_eq!(restarted.purge(expiry + 2 * RETENTION_MS, HEIGHT)?, 0);
    assert_eq!(restarted.store().job(key)?.state.state, unknown);
    let (output, manifest) = order.output(&fixture, 512)?;
    let delivered = report(
        lease,
        completion(output.clone(), manifest.clone(), tokens(100, 200)),
    )?;
    let succeeded = JobState::Ended(Outcome::Succeeded);
    assert_eq!(
        restarted.complete(lease, &delivered, expiry + 1),
        Ok(succeeded)
    );
    let result = result_of(&restarted, key)?;
    assert_eq!(
        (
            result.output_commitment,
            result.output_bytes,
            result.input_units,
            result.output_units,
            result.processing_ms,
            result.error_code,
            result.evidence
        ),
        (
            Some(root(&manifest)?),
            512,
            100,
            200,
            3,
            0,
            Some(evidence_digest(&delivered)?)
        )
    );
    assert_eq!(
        restarted.complete(lease, &delivered, expiry + 2),
        Ok(succeeded)
    );
    assert_eq!(
        restarted.store().retained(key, false)?,
        vec![delivered.clone()]
    );
    let contradicting = report(
        lease,
        completion(output.clone(), manifest.clone(), tokens(101, 200)),
    )?;
    assert_eq!(
        restarted.complete(lease, &contradicting, expiry + 3),
        Err(ServiceError::IdempotencyConflict)
    );
    let fenced = Lease { key, fence: 2 };
    let stale = report(
        fenced,
        completion(output.clone(), manifest, tokens(100, 200)),
    )?;
    assert_eq!(
        restarted.complete(fenced, &stale, expiry + 4),
        Err(ServiceError::IdempotencyConflict)
    );
    assert_eq!(
        restarted.store().retained(key, true)?,
        vec![contradicting, stale]
    );
    assert_eq!(result_of(&restarted, key)?, result);
    assert_eq!(restarted.store().output(key)?, Some(output));
    assert_eq!(restarted.store().usage()?, QueueUsage::default());
    assert_eq!(restarted.runner().invocations(), 1);
    Ok(())
}

#[test]
fn a09_real_runner_lookup_resolves_after_restart() -> Checked {
    let runner = real_runner()?;
    let fixture = admitting()?;
    let dir = Scratch::new("a09-runner")?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let key = order.key()?;
    let first = service(&dir.store(), runner.clone())?;
    let ack = place(&first, &fixture, &order)?;
    let lease = first.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?;
    let job = first.store().job(key)?;
    let request = job.record.request()?;
    let payload = first.store().payload(key)?;
    let admission = job.record.admission;
    let _lost = first.runner().dispatch(&Dispatch {
        job: key,
        fence: lease.fence,
        model: admission.model,
        deployment: admission.deployment,
        capability: admission.capability,
        unit_kind: job.record.unit_kind,
        max_output_bytes: request.max_output_bytes,
        max_input_units: job.record.max_input_units,
        max_units: request.max_units,
        deadline_ms: admission.deadline_ms,
        result_key: request.result_key,
        input_manifest: &job.record.input_manifest,
        payload: &payload,
    });
    drop(first);
    let restarted = service(&dir.store(), runner)?;
    let states = restarted.reconcile(NOW + 1 + 30_000 + LEASE_GRACE_MS)?;
    assert_eq!(states.len(), 1);
    assert!(matches!(
        states[0].1,
        JobState::Ended(Outcome::Succeeded | Outcome::Failed)
    ));
    assert_eq!(restarted.store().retained(key, false)?.len(), 1);
    assert_eq!(
        restarted.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 2),
        Err(ServiceError::IdempotencyConflict)
    );
    Ok(())
}

#[test]
fn a10_queue_count_bounds_refuse_before_any_runner_call() -> Checked {
    assert_eq!((MAX_RUNNING, MAX_QUEUED), (32, 128));
    let fixture = admitting()?;
    let dir = Scratch::new("a10-count")?;
    let worker = service(&dir.store(), dir.absent())?;
    let mut waiting = Vec::new();
    for n in 1..=160u8 {
        let order = order(&fixture, n, u64::from(n), 2_048)?;
        let ack = place(&worker, &fixture, &order)?;
        if n <= 32 {
            let lease = worker.claim(
                order.key()?,
                &order.accepted(&ack)?,
                &fixture.authority,
                NOW + 1,
            )?;
            assert_eq!(lease.fence, u64::from(n));
        } else {
            waiting.push((order, ack));
        }
    }
    assert_eq!(
        worker.store().usage()?,
        QueueUsage {
            queued: 128,
            running: 32,
            queued_bytes: 160 * 1_024
        }
    );
    let refused_order = order(&fixture, 161, 161, 2_048)?;
    assert_eq!(
        refused(place(&worker, &fixture, &refused_order)),
        Some(ServiceError::CapacityExceeded)
    );
    assert_eq!(
        worker
            .store()
            .find(refused_order.key()?)?
            .map(|job| job.record.sequence),
        None
    );
    let (queued, ack) = &waiting[0];
    let key = queued.key()?;
    assert_eq!(
        worker.claim(key, &queued.accepted(ack)?, &fixture.authority, NOW + 1),
        Err(ServiceError::CapacityExceeded)
    );
    assert_eq!(worker.store().job(key)?.state.state, JobState::Accepted);
    assert_eq!(
        worker.readiness(
            Readiness::RunnerReady,
            &fixture.authority,
            &fixture.metadata
        ),
        Readiness::NotReady
    );
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

/// Commits the real admission of `order` re-keyed to request id `n` straight into the store.
fn bulk(
    store: &JobStore,
    admission: &Admission,
    order: &Order,
    n: u16,
    payload: &[u8],
) -> Checked<(JobKey, Vec<u8>)> {
    let mut id = [0x88; 32];
    id[..2].copy_from_slice(&n.to_be_bytes());
    let admission = Admission {
        context: ServiceContext {
            request: RequestId::new(id)?,
            ..admission.context
        },
        ..*admission
    };
    let key = JobKey::derive(
        admission.context.market,
        admission.context.actor,
        admission.context.request,
    )?;
    let job = store.insert(
        &NewJob {
            key,
            admission,
            request: &order.signed,
            input_manifest: &order.manifest,
            payload,
            unit_kind: TOKENS,
            max_input_units: 4_096,
            accepted_ms: NOW,
        },
        |sequence| admission.sign_acknowledgment(sequence, &delegate()),
    )?;
    Ok((key, job.record.acknowledgment))
}

#[test]
fn a10_queue_byte_bound_is_exact() -> Checked {
    assert_eq!(MAX_QUEUED_BYTES, 134_217_728);
    let fixture = admitting()?;
    let dir = Scratch::new("a10-bytes")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let admission =
        order
            .submit
            .admit(&fixture.authority, &customer_at(HEIGHT)?, &fixture.metadata)??;
    let payload = vec![0x22; MAX_ENCRYPTED_BYTES];
    for n in 0..128u16 {
        let (key, ack) = bulk(worker.store(), &admission, &order, n, &payload)?;
        if n < 32 {
            worker.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?;
        }
    }
    assert_eq!(
        worker.store().usage()?,
        QueueUsage {
            queued: 96,
            running: 32,
            queued_bytes: MAX_QUEUED_BYTES
        }
    );
    assert_eq!(
        refused(bulk(worker.store(), &admission, &order, 128, &[0x33])),
        Some(ServiceError::CapacityExceeded)
    );
    assert_eq!(
        refused(bulk(
            worker.store(),
            &admission,
            &order,
            129,
            &vec![0; MAX_ENCRYPTED_BYTES + 1]
        )),
        Some(ServiceError::InputTooLarge)
    );
    assert_eq!(worker.store().usage()?.queued_bytes, MAX_QUEUED_BYTES);
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

#[test]
fn a11_processing_time_and_count_arithmetic() -> Checked {
    assert_eq!(processing_ms(1_000_000, 4_999_999), Ok(3));
    assert_eq!(
        processing_ms(1_000_000, 999_999),
        Err(EvidenceFault::MeasurementReversed)
    );
    assert_eq!(processing_ms(7, 7), Ok(0));
    let bounds = Bounds {
        unit_kind: TOKENS,
        max_output_bytes: 2_048,
        max_input_units: 4_096,
        max_units: 256,
    };
    let fixture = admitting()?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let (output, manifest) = order.output(&fixture, 512)?;
    let counted = |segments| completion(output.clone(), manifest.clone(), segments);
    let input = |count| Segment {
        role: Role::Input,
        unit_kind: TOKENS,
        count,
    };
    assert_eq!(
        counted(vec![input(u64::MAX), input(1)]).validate(&bounds),
        Err(EvidenceFault::CountOverflow)
    );
    assert_eq!(EvidenceFault::CountOverflow.error(), ServiceError::Overflow);
    assert_eq!(
        counted(tokens(4_097, 1)).validate(&bounds),
        Err(EvidenceFault::InputUnitsAboveBound)
    );
    assert_eq!(
        counted(tokens(1, 257)).validate(&bounds),
        Err(EvidenceFault::UnitsAboveBound)
    );
    assert_eq!(
        EvidenceFault::MeasurementReversed.error(),
        ServiceError::NonCanonical
    );
    let dir = Scratch::new("a11")?;
    let worker = service(&dir.store(), dir.absent())?;
    let ack = place(&worker, &fixture, &order)?;
    let key = order.key()?;
    let lease = worker.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?;
    let reversed = Completion {
        finish_ns: 999_999,
        ..counted(tokens(100, 200))
    };
    let evidence = report(lease, reversed)?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 2),
        Ok(JobState::Ended(Outcome::Failed))
    );
    let result = result_of(&worker, key)?;
    assert_eq!(
        (
            result.outcome,
            result.error_code,
            result.processing_ms,
            result.output_commitment
        ),
        (Outcome::Failed, 1, 0, None)
    );
    Ok(())
}

/// Places `order`, runs it to a SUCCEEDED result with a `bytes`-long output and returns the
/// acknowledgment, the RESULT manifest and the saved signed result.
fn succeed(
    worker: &JobService,
    fixture: &Admitting,
    order: &Order,
    bytes: usize,
) -> Checked<(Vec<u8>, Vec<u8>, Vec<u8>)> {
    let ack = place(worker, fixture, order)?;
    let key = order.key()?;
    let lease = worker.claim(key, &order.accepted(&ack)?, &fixture.authority, NOW + 1)?;
    let (output, manifest) = order.output(fixture, bytes)?;
    let evidence = report(
        lease,
        completion(output, manifest.clone(), tokens(100, 200)),
    )?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 2),
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let saved = worker
        .store()
        .job(key)?
        .result
        .ok_or(Failure::Unexpected("missing result"))?;
    Ok((ack, manifest, saved))
}

#[test]
fn a12_private_retrieval_is_customer_or_granted_evaluator_only() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a12")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let (ack, manifest, saved) = succeed(&worker, &fixture, &order, 512)?;
    let key = order.key()?;
    let requester = customer_at(HEIGHT)?;
    let answer = QueryAnswer {
        key,
        state: JobState::Ended(Outcome::Succeeded),
        acknowledgment: ack,
        result: Some(saved.clone()),
        locator: Some(ResultLocator {
            output_commitment: root(&manifest)?,
            output_bytes: 512,
        }),
    };
    let query = order.reference(Route::Query, principal(9)?, 2, &customer())?;
    assert_eq!(
        worker.query(&query, &fixture.authority, &requester, &[], NOW + 3),
        Ok(answer.clone())
    );
    assert_eq!(
        worker.query(&query, &fixture.authority, &requester, &[], NOW + 4),
        Ok(answer.clone())
    );
    let unknown = order_for(&fixture, 0x77, 1)?;
    let reused = unknown.reference(Route::Query, principal(9)?, 2, &customer())?;
    assert_eq!(
        worker.query(&reused, &fixture.authority, &requester, &[], NOW + 5),
        Err(ServiceError::IdempotencyConflict)
    );
    let missing = unknown.reference(Route::Query, principal(9)?, 3, &customer())?;
    assert_eq!(
        worker.query(&missing, &fixture.authority, &requester, &[], NOW + 5),
        Err(ServiceError::NotFound)
    );
    let stranger_key = SigningKey::from_bytes(&[0x33; 32]);
    let stranger = order.reference(Route::Query, principal(0x33)?, 1, &stranger_key)?;
    assert_eq!(
        worker.query(
            &stranger,
            &fixture.authority,
            &identity(0x33, false)?,
            &[],
            NOW + 6
        ),
        Err(ServiceError::AccessDenied)
    );
    let evaluator_key = SigningKey::from_bytes(&[3; 32]);
    let evaluator = identity(3, false)?;
    let granted = order.reference(Route::Query, principal(3)?, 1, &evaluator_key)?;
    assert_eq!(
        worker.query(
            &granted,
            &fixture.authority,
            &evaluator,
            &[principal(3)?],
            NOW + 7
        ),
        Ok(answer)
    );
    let ungranted = order.reference(Route::Query, principal(3)?, 2, &evaluator_key)?;
    assert_eq!(
        worker.query(
            &ungranted,
            &fixture.authority,
            &evaluator,
            &[principal(4)?],
            NOW + 8
        ),
        Err(ServiceError::AccessDenied)
    );
    let cancel = order.reference(Route::Cancel, principal(3)?, 3, &evaluator_key)?;
    assert_eq!(
        worker.cancel(&cancel, &fixture.authority, &evaluator, NOW + 9),
        Err(ServiceError::AccessDenied)
    );
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

#[test]
fn a12_frozen_owner_withholds_release_but_keeps_the_commitment() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a12-frozen")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order(&fixture, 0x31, 1, 2_048)?;
    let (_, _, saved) = succeed(&worker, &fixture, &order, 512)?;
    let key = order.key()?;
    let requester = customer_at(HEIGHT)?;
    let current = view(&fixture.market.world.bytes, &fixture.market.roster, HEIGHT)?;
    let frozen = current.bind(Knobs {
        frozen: true,
        ..current.knobs()?
    })?;
    let blocked = order.reference(Route::Query, principal(9)?, 4, &customer())?;
    assert_eq!(
        worker.query(&blocked, &frozen, &requester, &[], NOW + 10),
        Err(ServiceError::IdentityFrozen)
    );
    let next = order_for(&fixture, 0x32, 5)?;
    assert_eq!(
        worker
            .submit(
                &next.signed,
                next.input(),
                &next.view(&frozen, &requester, &fixture.metadata),
                NOW + 11
            )
            .map(|_| ()),
        Err(ServiceError::IdentityFrozen)
    );
    assert_eq!(
        worker.commit_record(key, &frozen),
        Ok(result_manifest_digest(&saved)?)
    );
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

fn order_for(fixture: &Admitting, request: u8, sequence: u64) -> Checked<Order> {
    order(fixture, request, sequence, 2_048)
}

#[test]
fn a14_unavailable_storage_and_runner_never_fake_progress() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a14")?;
    let root_dir = dir.store();
    let worker = service(&root_dir, dir.absent())?;
    let order = order_for(&fixture, 0x31, 1)?;
    let key = order.key()?;
    let (jobs, away) = (root_dir.join("jobs"), root_dir.join("jobs-away"));
    fs::rename(&jobs, &away)?;
    fs::write(&jobs, b"")?;
    assert_eq!(
        refused(place(&worker, &fixture, &order)),
        Some(ServiceError::ExecutionUnavailable)
    );
    fs::remove_file(&jobs)?;
    fs::rename(&away, &jobs)?;
    assert_eq!(
        worker.store().find(key)?.map(|job| job.record.sequence),
        None
    );
    let ack = place(&worker, &fixture, &order)?;
    let (kept, _) = decode_acknowledgment(&ack, public(&delegate()))?;
    assert_eq!(kept.sequence, 1);
    let task = order.accepted(&ack)?;
    let first = worker.claim(key, &task, &fixture.authority, NOW + 1)?;
    assert_eq!(worker.run(first, NOW + 2), Ok(JobState::Accepted));
    assert_eq!(worker.runner().invocations(), 1);
    assert_eq!(worker.store().job(key)?.result, None);
    let lease = worker.claim(key, &task, &fixture.authority, NOW + 3)?;
    assert_eq!(lease, Lease { key, fence: 2 });
    assert_eq!(
        worker.run(first, NOW + 4),
        Err(ServiceError::IdempotencyConflict)
    );
    let cancel = order.reference(Route::Cancel, principal(9)?, 2, &customer())?;
    assert_eq!(
        worker.cancel(&cancel, &fixture.authority, &customer_at(HEIGHT)?, NOW + 5),
        Ok(JobState::Running)
    );
    let state = worker.store().job(key)?.state;
    assert_eq!(
        (state.state, state.cancel_requested, state.fence),
        (JobState::Running, true, 2)
    );
    assert_eq!(worker.runner().invocations(), 2);
    let (output, manifest) = order.output(&fixture, 256)?;
    let evidence = report(lease, completion(output, manifest, tokens(100, 200)))?;
    let (counters, hidden) = (root_dir.join("counters"), root_dir.join("counters-away"));
    fs::rename(&counters, &hidden)?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 6),
        Err(ServiceError::ExecutionUnavailable)
    );
    fs::rename(&hidden, &counters)?;
    assert_eq!(worker.store().job(key)?.state, state);
    assert_eq!(worker.store().retained(key, false)?, Vec::<Vec<u8>>::new());
    let lapsed = NOW + 3 + 30_000 + LEASE_GRACE_MS;
    assert_eq!(
        worker.reconcile(lapsed)?,
        vec![(key, JobState::Ended(Outcome::UnknownExecution))]
    );
    assert_eq!(worker.runner().invocations(), 3);
    assert_eq!(
        worker.complete(lease, &evidence, lapsed + 1),
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    assert_eq!(result_of(&worker, key)?.outcome, Outcome::Succeeded);
    assert_eq!(worker.runner().invocations(), 3);
    Ok(())
}

#[test]
fn a15_admitted_work_keeps_its_metadata_across_expiry_and_rotation() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a15")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order_for(&fixture, 0x31, 1)?;
    let key = order.key()?;
    let ack = place(&worker, &fixture, &order)?;
    let expired = authority_at(&fixture.market, EXPIRY)?;
    let late_customer = customer_at(EXPIRY)?;
    let next = order_for(&fixture, 0x32, 2)?;
    assert_eq!(
        worker
            .submit(
                &next.signed,
                next.input(),
                &next.view(&expired, &late_customer, &fixture.metadata),
                NOW + 1
            )
            .map(|_| ()),
        Err(ServiceError::MetadataExpired)
    );
    let lease = worker.claim(key, &order.accepted(&ack)?, &expired, NOW + 1)?;
    let (output, published) = order.output(&fixture, 512)?;
    let evidence = report(lease, completion(output, published, tokens(100, 200)))?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 2),
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let result = result_of(&worker, key)?;
    assert_eq!(
        (result.metadata, result.model, result.deployment),
        (
            fixture.metadata.digest(),
            Digest32::new([0x31; 32])?,
            Digest32::new([0xDE; 32])?
        )
    );
    let replacement = manifest(
        2,
        vec![capability(0x41)],
        vec![endpoint(DEFAULT_URI, [0xE1; 32])],
    )?;
    let replacement_signed = sign(&replacement, 1)?;
    let (_, replacement_digest) = Manifest::decode(&replacement.encode()?)?;
    let mut world = fixture.market.world.clone();
    let owner_worker = result.worker;
    world.edit(|parts, _| {
        let current = parts.workers.get(owner_worker).ok_or(NON_CANONICAL)?;
        parts.workers.replace(&WorkerCurrent {
            metadata: replacement_digest,
            metadata_revision: 2,
            last_metadata_height: HEIGHT - 1,
            ..current
        })
    })?;
    let rotated = view(&world.bytes, &fixture.market.roster, HEIGHT)?.authority()?;
    let metadata = verified(&rotated, &replacement_signed)?;
    assert_eq!(metadata.manifest().capabilities, vec![capability(0x41)]);
    let saved = worker
        .store()
        .job(key)?
        .result
        .ok_or(Failure::Unexpected("missing result"))?;
    assert_eq!(
        worker.commit_record(key, &rotated),
        Ok(result_manifest_digest(&saved)?)
    );
    assert_eq!(result_of(&worker, key)?, result);
    let query = order.reference(Route::Query, principal(9)?, 2, &customer())?;
    let answer = worker.query(&query, &rotated, &customer_at(HEIGHT)?, &[], NOW + 3)?;
    assert_eq!(answer.result, Some(saved));
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}

#[test]
fn a17_dispatch_waits_for_finalized_acceptance_and_late_results_stay_local() -> Checked {
    let fixture = admitting()?;
    let dir = Scratch::new("a17")?;
    let worker = service(&dir.store(), dir.absent())?;
    let order = order_for(&fixture, 0x31, 1)?;
    let key = order.key()?;
    let ack = place(&worker, &fixture, &order)?;
    let accepted = order.accepted(&ack)?;
    let refusals = [
        (order.submit.task, ServiceError::AdmissionNotEffective),
        (
            TaskBinding {
                acknowledgement: Some(Digest32::new([0xAB; 32])?),
                ..accepted
            },
            ServiceError::StaleAuthority,
        ),
        (
            TaskBinding {
                status: TaskStatus::Cancelled,
                ..accepted
            },
            ServiceError::AdmissionNotEffective,
        ),
        (
            TaskBinding {
                input: Digest32::new([0x1C; 32])?,
                ..accepted
            },
            ServiceError::StaleAuthority,
        ),
    ];
    for (task, refusal) in refusals {
        assert_eq!(
            worker.dispatch(key, &task, &fixture.authority, NOW + 1),
            Err(refusal)
        );
    }
    assert_eq!(worker.store().job(key)?.state.state, JobState::Accepted);
    assert_eq!(worker.runner().invocations(), 0);
    let lease = worker.claim(key, &accepted, &fixture.authority, NOW + 1)?;
    assert_eq!(lease.fence, 1);
    assert_eq!(
        worker.claim(key, &accepted, &fixture.authority, NOW + 1),
        Err(ServiceError::IdempotencyConflict)
    );
    let (output, manifest) = order.output(&fixture, 512)?;
    let evidence = report(
        lease,
        completion(output.clone(), manifest, tokens(100, 200)),
    )?;
    assert_eq!(
        worker.complete(lease, &evidence, NOW + 2),
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let saved = worker
        .store()
        .job(key)?
        .result
        .ok_or(Failure::Unexpected("missing result"))?;
    assert_eq!(
        worker.commit_record(key, &fixture.authority),
        Ok(result_manifest_digest(&saved)?)
    );
    let late = authority_at(&fixture.market, TASK_EXPIRY)?;
    assert_eq!(
        worker.commit_record(key, &late),
        Err(ServiceError::DeadlineInvalid)
    );
    let job = worker.store().job(key)?;
    assert_eq!(
        (job.state.late, job.result, worker.store().output(key)?),
        (true, Some(saved), Some(output))
    );
    let stale = order_for(&fixture, 0x32, 2)?;
    let stale_ack = place(&worker, &fixture, &stale)?;
    let stale_key = stale.key()?;
    assert_eq!(
        worker.claim(stale_key, &stale.accepted(&stale_ack)?, &late, NOW + 3),
        Err(ServiceError::DeadlineInvalid)
    );
    let expired = ServiceResult::admitted(
        &worker.store().job(stale_key)?.record.admission,
        Outcome::ExpiredBeforeStart,
    );
    assert_eq!(result_of(&worker, stale_key)?, expired);
    let idle = order_for(&fixture, 0x33, 3)?;
    place(&worker, &fixture, &idle)?;
    assert_eq!(worker.expire(&fixture.authority, NOW + 29_999)?, 0);
    assert_eq!(worker.expire(&fixture.authority, NOW + 30_000)?, 1);
    assert_eq!(
        worker.store().job(idle.key()?)?.state.state,
        JobState::Ended(Outcome::ExpiredBeforeStart)
    );
    assert_eq!(worker.store().usage()?, QueueUsage::default());
    assert_eq!(worker.runner().invocations(), 0);
    Ok(())
}
