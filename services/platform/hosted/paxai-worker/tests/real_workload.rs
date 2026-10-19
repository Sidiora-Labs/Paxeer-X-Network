//! AI.F02-T04 real inference and embedding workloads. A really opened market routes the
//! customer's `ADMIT_TASK` and the worker owner's `ACCEPT_TASK`/`COMMIT_TASK_RESULT` through
//! `dispatch::route`; the worker serves pinned TLS from its binary, admits and persists through
//! the durable `JobService` against finalized captures, and executes on the real model runner
//! process. Quality comes only from admitted evaluator reports aggregated by F05.
//!
//! Pinned licensed artifacts, the runner executable and the durable store are named by the JSON
//! file in `PAXAI_REAL_WORKLOAD` (see [`Provisioned`]); cases that need them fail with
//! `Unprovisioned` when it is absent, they are never skipped.
use ed25519_dalek::{Signer, SigningKey};
use layerx_client::head::Head;
use layerx_paxai_worker::{
    auth::{
        acknowledgment_digest, decode_acknowledgment, decode_service, encode_service,
        sign_metadata, verify_signed_metadata, MetadataContext, MetadataPublication, Route,
        ServiceContext, ServiceError, ServiceOperation, ServiceRequest, StatusChallenge,
    },
    discovery::{
        spki_sha256, AuthorityEvidence, EndpointClient, FinalizedAuthority, IdentityEvidence,
        NetworkProfile, Readiness, ReadinessObservation, TransportError, VerifiedMetadata,
        MAX_FINALITY_LAG,
    },
    jobs::{
        decode_result, evidence_digest, result_manifest_digest, AdmissionView, Input, JobService,
        Outcome, Receipt, ServiceResult,
    },
    metadata::{Capability, Endpoint, Manifest},
    runner::{
        processing_ms, Bounds, Completion, EvidenceFault, ProcessRunner,
        Progress as RunnerProgress, Report, Role, Segment, Usage, TOKENS, VECTOR_ELEMENTS,
    },
    store::{JobKey, JobState, JobStore, Usage as QueueUsage},
};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission as Membership, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation::{self as agg, Outcome as Agg, Progress},
    aggregation_codec::{decode_current, AggregationPhase, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        encode_envelope, encode_roster, CommitScorePayload, Envelope, ReportBody, ResultStatus,
        RevealScorePayload, Roster, ScoreVector,
    },
    commit_reveal::commitment,
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, F01_TASK_EXPIRED, F01_TASK_NOT_FOUND,
        F02_DELEGATE_REVOKED, NON_CANONICAL, NOT_FOUND, UNAUTHORIZED,
    },
    evaluators::{
        admission as f03,
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms, SignedReport},
    },
    evidence::{
        chunk_count, decode_manifest, encode_manifest, manifest_root, object_content_root,
        ArtifactContext, ArtifactError, ArtifactKind, ArtifactManifest, Items, Privacy,
        MAX_MANIFEST_BYTES,
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        read_state_chunk, CaptureFacts, FinalityEvidence, QueryError, ReadProof, StateCapture,
    },
    registry::{derive_rewards_account, MarketHeader},
    registry_ops::{CallContext, PolicySection, ACTIVE},
    rewards::{
        decode_reward_state, EpochStatus, FundReplay, FundRequest, FundingAuthority, FundingPhase,
        RewardEpoch, RewardLedger, RewardOutcome, RewardState, FUNDING_POLICY_VERSION,
        REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState,
    },
    tasks::{self, SetBinding, TaskBinding, TaskSet, TaskStatus},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, CommitmentDigest, Digest32,
        EvaluatorBinding, EvaluatorId, EvaluatorRosterEntry, EvidenceRoot, FrozenBinding, MarketId,
        MetadataDigest, PolicyDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest,
        RequestId, ResultDigest, RosterDigest, RubricDigest, Salt32, Signature64, StateDigest,
        TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use rcgen::{
    BasicConstraints, CertificateParams, DistinguishedName, DnType, IsCa, Issuer, KeyPair,
};
use rustls::{pki_types::CertificateDer, RootCertStore};
use serde::Deserialize;
use serde_json::{Map, Value};
use sha2::{Digest as _, Sha256};
use std::ffi::OsString;
use std::fmt::Write as _;
use std::fs;
use std::io::{self, BufRead, BufReader};
use std::net::{IpAddr, Ipv4Addr, TcpListener};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::time::Duration;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const ROOT: [u8; 32] = [0xA1; 32];
const KEEPER: u8 = 0x7f;
const RELAYER: u8 = 0x70;
const ORIGIN: u64 = 1000;
const DELEGATE_SEED: [u8; 32] = [0xD1; 32];
const CUSTOMER_SEED: [u8; 32] = [0xC9; 32];
/// The customer principal that admits every task on chain and signs every submit.
const CUSTOMER: u8 = 9;
const VALID_FROM: u64 = 1140;
/// Metadata (license) expiry of the enrolled worker.
const EXPIRY: u64 = 1180;
const ADMIT_AT: u64 = 1145;
/// Epoch 1 work window `[1128, 1192)`; commit, reveal and settlement windows follow.
const WORK_START: u64 = 1128;
const COMMIT_OPEN: u64 = 1192;
const REVEAL_OPEN: u64 = 1208;
const SETTLE_OPEN: u64 = 1224;
const ROLE_EXPIRY: u64 = 5000;
const CHUNK_RESPONSE_MAX: usize = 8_244;
const SALT: [u8; 32] = [0x5a; 32];
const IO_TIMEOUT: Duration = Duration::from_secs(10);
/// Wall clock of the worker replica, in milliseconds.
const NOW: u64 = 1_700_000_000_000;
const CONFIG_ENV: &str = "PAXAI_REAL_WORKLOAD";
const UNPROVISIONED: &str = "P-REAL-SERVICES: PAXAI_REAL_WORKLOAD names no pinned runner, \
                             licensed inference and embedding artifacts or durable store";
const EVIDENCE_DOMAIN: &str = "PAXAI/real-workload-evaluator-evidence/v1";

enum Failure {
    Application(ApplicationError),
    Service(ServiceError),
    Transport(TransportError),
    Artifact(ArtifactError),
    Query(QueryError),
    Io(io::ErrorKind),
    Tls(rustls::Error),
    Certificate(rcgen::Error),
    Json(serde_json::Error),
    Unprovisioned(&'static str),
    Unexpected(&'static str),
}
impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Service(error) => write!(f, "service refusal {error:?}"),
            Self::Transport(error) => write!(f, "transport refusal {error:?}"),
            Self::Artifact(error) => write!(f, "artifact refusal {error:?}"),
            Self::Query(error) => write!(f, "query refusal {error:?}"),
            Self::Io(kind) => write!(f, "io failure {kind:?}"),
            Self::Tls(error) => write!(f, "tls failure {error:?}"),
            Self::Certificate(error) => write!(f, "certificate generation {error:?}"),
            Self::Json(error) => write!(f, "configuration {error:?}"),
            Self::Unprovisioned(what) => write!(f, "unprovisioned {what}"),
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
impl From<TransportError> for Failure {
    fn from(error: TransportError) -> Self {
        Self::Transport(error)
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
impl From<rustls::Error> for Failure {
    fn from(error: rustls::Error) -> Self {
        Self::Tls(error)
    }
}
impl From<rcgen::Error> for Failure {
    fn from(error: rcgen::Error) -> Self {
        Self::Certificate(error)
    }
}
impl From<serde_json::Error> for Failure {
    fn from(error: serde_json::Error) -> Self {
        Self::Json(error)
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
fn delegate() -> SigningKey {
    SigningKey::from_bytes(&DELEGATE_SEED)
}
fn customer() -> SigningKey {
    SigningKey::from_bytes(&CUSTOMER_SEED)
}
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// Market, worker owner and worker of every workload.
fn identities() -> CodecResult<(MarketId, PrincipalId, WorkerId)> {
    let market = derive_market(ChainDomain::new(CHAIN)?, ProgramId::new(PROGRAM)?)?;
    let owner = principal(1)?;
    Ok((market, owner, derive_worker(market, owner, [1; 32])?))
}
fn evaluator_id(n: u8) -> CodecResult<EvaluatorId> {
    derive_evaluator(identities()?.0, principal(n)?, [n; 32])
}
fn encode_state(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}
/// A request id unique per kind, actor and sequence.
fn tag(kind: u8, n: u8, sequence: u64) -> [u8; 32] {
    let mut out = [kind; 32];
    out[0] = n;
    out[1..9].copy_from_slice(&sequence.to_be_bytes());
    out
}
fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        if write!(out, "{byte:02x}").is_err() {
            return String::new();
        }
    }
    out
}
fn unhex32(text: &str) -> Checked<[u8; 32]> {
    let digits = text.as_bytes();
    let mut out = [0; 32];
    if digits.len() != 64 {
        return Err(Failure::Unexpected("a pinned digest is not 64 hex digits"));
    }
    for (byte, pair) in out.iter_mut().zip(digits.chunks_exact(2)) {
        let pair = std::str::from_utf8(pair).map_err(|_| Failure::Unexpected("hex digits"))?;
        *byte = u8::from_str_radix(pair, 16).map_err(|_| Failure::Unexpected("hex digits"))?;
    }
    Ok(out)
}
fn section_bytes(bytes: &[u8], section: Section) -> CodecResult<Vec<u8>> {
    Ok(decode_shared_state(bytes)?.feature_sections[section.index()].to_vec())
}

/// One native, or worker-delegate-signed, request envelope.
#[derive(Clone)]
struct Call {
    operation: Operation,
    actor: PrincipalId,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    expiry: u64,
    request: [u8; 32],
    payload: Vec<u8>,
    delegate: bool,
}
fn call(operation: Operation, actor: PrincipalId, request: u8, payload: Vec<u8>) -> Call {
    Call {
        operation,
        actor,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        expiry: u64::MAX,
        request: [request; 32],
        payload,
        delegate: false,
    }
}
impl Call {
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
        let mut encoded = vec![0; 32_768];
        if self.delegate {
            let key = public(&delegate());
            envelope.authentication = Authentication::Delegate {
                key,
                signature: Signature64([0; 64]),
            };
            let n = encode_envelope(&envelope, &mut encoded)?;
            let digest = decode_envelope(&encoded[..n])?.request_digest()?;
            envelope.authentication = Authentication::Delegate {
                key,
                signature: Signature64(delegate().sign(digest.as_bytes()).to_bytes()),
            };
        }
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

/// The decoded result frame of one routed call.
struct Frame {
    status: ResultStatus,
    error: Option<ApplicationError>,
    revision: u64,
    digest: [u8; 32],
    payload: Vec<u8>,
}
/// Routes `call` over `current`; returns the frame and, on `Applied`, the next state.
fn route(call: &Call, current: Option<&[u8]>, at: u64) -> CodecResult<(Frame, Option<Vec<u8>>)> {
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
            digest: frame.digest.bytes(),
            payload: frame.payload.to_vec(),
        },
        committed,
    ))
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

/// One market's committed shared state bytes and its next owner sequence.
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    /// The routed owner CREATE at `ORIGIN`.
    fn create() -> CodecResult<Self> {
        let program = ProgramId::new(PROGRAM)?;
        let mut payload = OWNER.to_vec();
        payload.extend_from_slice(&ASSET);
        payload
            .extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
        payload.extend_from_slice(&REFUND);
        payload.push(0);
        payload.extend_from_slice(&policy_bytes()?);
        payload.extend_from_slice(&[16; 32]);
        let create = Call {
            sequence: 1,
            ..call(dispatch::CREATE, PrincipalId::new(OWNER)?, 1, payload)
        };
        let (frame, next) = route(&create, None, ORIGIN)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
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
    /// Routes `call`, committing the state on `Applied`.
    fn route(&mut self, call: &Call, at: u64) -> CodecResult<Frame> {
        let (frame, next) = route(call, Some(&self.bytes), at)?;
        if let Some(next) = next {
            self.bytes = next;
        }
        Ok(frame)
    }
    /// Routed owner `SCHEDULE_ACTIVATION` of `epoch`.
    fn schedule(&mut self, epoch: u64, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        let schedule = Call {
            config: self.header()?.active_config_version,
            sequence: self.owner_sequence,
            ..call(
                dispatch::SCHEDULE_ACTIVATION,
                PrincipalId::new(OWNER)?,
                0x20,
                payload,
            )
        };
        let frame = self.route(&schedule, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        self.owner_sequence += 1;
        Ok(())
    }
}

/// Worker, evaluator and funding producers.
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
            let grant = EvaluatorGrant::nominate(
                market.market_id,
                owner,
                [n; 32],
                GrantTerms {
                    rubric: rubric()?,
                    grant_version: version()?,
                    key_version: version()?,
                    signing_key,
                    effective_epoch: effective,
                    expiry_epoch_exclusive: effective + 32,
                },
            )?;
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
            parts
                .replay
                .bind(ActorSlot::evaluator(usize::from(n - 2))?, owner, version()?)?;
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
        let tag = [0x40; 32];
        let request = ReplayRequest {
            slot: ActorSlot::OWNER,
            principal: market.owner_principal,
            authority_version: version()?,
            sequence: self.owner_sequence,
            request_id: RequestId::new(tag)?,
            digest: RequestDigest::new(tag)?,
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
                result: ResultDigest::new(tag)?,
            },
            &mut funded,
        )?;
        parts.rewards = funded;
        self.bytes = parts.encode()?;
        self.owner_sequence += 1;
        Ok(())
    }
}

/// The routed keeper `ADVANCE_ACTIVATION` and `OPEN_EPOCH` calls.
impl World {
    fn advance(&mut self, at: u64) -> CodecResult<()> {
        let header = self.header()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&header.activation_epoch.to_be_bytes());
        let advance = Call {
            config: header.active_config_version,
            ..call(
                dispatch::ADVANCE_ACTIVATION,
                principal(KEEPER)?,
                0x61,
                payload,
            )
        };
        let frame = self.route(&advance, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        assert_eq!(self.header()?.lifecycle, ACTIVE);
        Ok(())
    }
    /// Opens the clock epoch of `at` naming the previewed frozen config and roster.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let frozen = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let open = Call {
            epoch: frozen.epoch,
            config: frozen.config.get(),
            roster: Presence::Present(frozen.roster),
            ..call(dispatch::OPEN_EPOCH, principal(KEEPER)?, 0x60, Vec::new())
        };
        let frame = self.route(&open, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        Ok(frozen)
    }
}

/// The fixture capability of the cases that run without provisioned services.
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
        unit_kind: TOKENS,
        latency_ms: 30_000,
        concurrency: 4,
        determinism: 1,
    }
}
fn endpoint_at(port: u16, pin: [u8; 32]) -> Endpoint {
    Endpoint {
        id: 1,
        uri: format!("https://localhost:{port}/paxai/v1"),
        spki_sha256: pin,
    }
}
/// The signed worker manifest at `revision` valid over `[VALID_FROM, expiry)`.
fn manifest(
    revision: u64,
    expiry: u64,
    deployment: Digest32,
    mut capabilities: Vec<Capability>,
    endpoints: Vec<Endpoint>,
) -> Checked<Manifest> {
    let (market, owner, worker) = identities()?;
    capabilities.sort_by_key(|c| (c.kind, c.model, c.input_schema));
    Ok(Manifest {
        market,
        worker,
        owner,
        generation: 1,
        key_version: 1,
        revision,
        valid_from: VALID_FROM,
        expiry,
        deployment,
        capabilities,
        endpoints,
        privacy_policy: Digest32::new([0x9A; 32])?,
        service_terms: Digest32::new([0x7E; 32])?,
    })
}
/// The delegate-signed publication of `manifest` over `expected_revision`.
fn signed_metadata(manifest: &Manifest, expected_revision: u64) -> Checked<Vec<u8>> {
    let (market, owner, worker) = identities()?;
    let bytes = manifest.encode()?;
    Ok(sign_metadata(
        &MetadataContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            market,
            worker,
            owner,
            delegate: public(&delegate()),
        },
        &MetadataPublication {
            manifest: &bytes,
            expected_revision,
            epoch: 0,
            config: 1,
            roster: Presence::Absent,
            sequence: expected_revision + 1,
            expiry: manifest.expiry,
            request: RequestId::new([0x71; 32])?,
        },
        &delegate(),
    )?)
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
/// The F01 task record as the finalized capture holds it.
fn finalized_task(captured: &[u8], task: TaskId) -> Checked<TaskBinding> {
    let state = decode_shared_state(captured)?;
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    for binding in TaskSet::decode(section.task_region)?.bindings() {
        let binding = binding?;
        if binding.task == task {
            return Ok(binding);
        }
    }
    Err(F01_TASK_NOT_FOUND.into())
}
fn customer_at(at: u64) -> Checked<IdentityEvidence> {
    Ok(IdentityEvidence {
        principal: principal(CUSTOMER)?,
        primary_key: public(&customer()),
        frozen: false,
        execution_height: at,
    })
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
    let manifest = ArtifactManifest {
        kind,
        privacy: Privacy::Encrypted,
        context: ArtifactContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            market: identities()?.0,
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
fn manifest_digest(manifest: &[u8]) -> Checked<Digest32> {
    Ok(Digest32::new(manifest_root(manifest)?.bytes())?)
}

/// What the customer asks of one admitted capability.
#[derive(Clone, Copy)]
struct Ask {
    max_output_bytes: u32,
    max_units: u32,
    result_key: Option<[u8; 32]>,
}
const FIXTURE_ASK: Ask = Ask {
    max_output_bytes: 2_048,
    max_units: 256,
    result_key: Some([0x4B; 32]),
};

/// One customer order: the task it admitted on chain and its encrypted F09 input.
struct Order {
    task: TaskId,
    request: u8,
    deadline: u64,
    payload: Vec<u8>,
    manifest: Vec<u8>,
    input: Digest32,
}
impl Order {
    fn input(&self) -> Input<'_> {
        Input {
            manifest: &self.manifest,
            payload: &self.payload,
        }
    }
    fn key(&self) -> Checked<JobKey> {
        Ok(JobKey::derive(
            identities()?.0,
            principal(CUSTOMER)?,
            RequestId::new([self.request; 32])?,
        )?)
    }
}

/// The customer's signed submit of one order.
#[derive(Clone, Copy)]
struct Submit {
    context: ServiceContext,
    request: ServiceRequest,
}
impl Submit {
    fn signed(&self) -> Checked<Vec<u8>> {
        Ok(encode_service(
            ServiceOperation::SubmitJob,
            &self.context,
            &self.request.encode()?,
            &customer(),
        )?)
    }
}

/// The finalized view a worker replica holds at one height.
struct View {
    authority: FinalizedAuthority,
    metadata: VerifiedMetadata,
    captured: Vec<u8>,
    customer: IdentityEvidence,
}
impl View {
    fn task(&self, task: TaskId) -> Checked<TaskBinding> {
        finalized_task(&self.captured, task)
    }
    /// The durable `SubmitJob` of `submit` for `order`, admitted against `task`.
    fn place(
        &self,
        service: &JobService,
        submit: &Submit,
        order: &Order,
        task: &TaskBinding,
        now_ms: u64,
    ) -> Checked<Receipt> {
        Ok(service.submit(
            &submit.signed()?,
            order.input(),
            &AdmissionView {
                authority: &self.authority,
                customer: &self.customer,
                metadata: &self.metadata,
                task,
            },
            now_ms,
        )?)
    }
}

/// The opened market at origin 1000 with Work window 1128..1192 and its discovery evidence.
struct Market {
    world: World,
    roster: Vec<u8>,
    signed: Vec<u8>,
    manifest: Manifest,
    metadata: MetadataDigest,
    frozen: Frozen,
    worker: WorkerId,
    entry: WorkerRosterEntry,
}
/// Routed `CREATE` and `SCHEDULE_ACTIVATION`, worker enrollment bound to the signed manifest
/// digest, three accepted evaluators with bound replay slots, `FUND`, then routed
/// `ADVANCE_ACTIVATION` and `OPEN_EPOCH` 1.
fn opened(
    capabilities: Vec<Capability>,
    endpoints: Vec<Endpoint>,
    deployment: Digest32,
) -> Checked<Market> {
    let (market, owner, worker) = identities()?;
    let manifest = manifest(1, EXPIRY, deployment, capabilities, endpoints)?;
    let signed = signed_metadata(&manifest, 0)?;
    let (_, digest) = Manifest::decode(&manifest.encode()?)?;
    let mut world = World::create()?;
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
    world.fund(500, WORK_START)?;
    world.advance(WORK_START)?;
    let frozen = world.open(WORK_START + 1)?;
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
        manifest,
        metadata: digest,
        frozen,
        worker,
        entry,
    })
}

/// Customer and worker-owner calls of the F01 handoff.
impl Market {
    fn bound(
        &self,
        operation: Operation,
        actor: PrincipalId,
        request: u8,
        payload: Vec<u8>,
    ) -> Call {
        Call {
            epoch: self.frozen.epoch,
            config: self.frozen.config.get(),
            roster: Presence::Present(self.frozen.roster),
            ..call(operation, actor, request, payload)
        }
    }
    fn frozen_binding(&self) -> Checked<FrozenBinding> {
        Ok(FrozenBinding {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            market: identities()?.0,
            epoch: self.frozen.epoch,
            config: self.frozen.config,
            roster: self.frozen.roster,
        })
    }
    fn task_id(&self, nonce: u8) -> Checked<TaskId> {
        Ok(derive_task(
            identities()?.0,
            self.frozen.epoch,
            principal(CUSTOMER)?,
            [nonce; 32],
        )?)
    }
    /// The customer's routed `ADMIT_TASK` of `[nonce; 32]` committing to `input` with
    /// `deadline`, naming the frozen worker and the digest of its signed manifest.
    fn admit_call(&self, nonce: u8, input: Digest32, deadline: u64) -> Checked<Call> {
        let mut payload = self.frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(self.frozen.policy.as_bytes());
        payload.extend_from_slice(self.frozen.roster.as_bytes());
        payload.extend_from_slice(principal(CUSTOMER)?.as_bytes());
        payload.extend_from_slice(self.worker.as_bytes());
        payload.extend_from_slice(self.metadata.as_bytes());
        payload.extend_from_slice(&[nonce; 32]);
        payload.extend_from_slice(input.as_bytes());
        payload.extend_from_slice(&deadline.to_be_bytes());
        Ok(Call {
            request: tag(0xB0, nonce, 0),
            ..self.bound(dispatch::ADMIT_TASK, principal(CUSTOMER)?, 0, payload)
        })
    }
    /// The customer publishes the encrypted INPUT manifest of `payload` for the task of
    /// `nonce` and routes its `ADMIT_TASK` at `at`.
    fn order(
        &mut self,
        nonce: u8,
        request: u8,
        payload: Vec<u8>,
        deadline: u64,
        at: u64,
    ) -> Checked<Order> {
        let task = self.task_id(nonce)?;
        let manifest = object_manifest(
            ArtifactKind::Input,
            principal(CUSTOMER)?,
            task,
            self.frozen.policy,
            &payload,
        )?;
        let input = manifest_digest(&manifest)?;
        let admit = self.admit_call(nonce, input, deadline)?;
        self.applied(&admit, at)?;
        Ok(Order {
            task,
            request,
            deadline,
            payload,
            manifest,
            input,
        })
    }
    /// The worker owner's step under role `sequence`, expecting the current revision.
    fn step(
        &self,
        operation: Operation,
        sequence: u8,
        task: TaskId,
        digest: [u8; 32],
    ) -> Checked<Call> {
        let mut payload = self.world.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(task.as_bytes());
        payload.extend_from_slice(&digest);
        Ok(Call {
            sequence: u64::from(sequence),
            expiry: ROLE_EXPIRY,
            ..self.bound(operation, identities()?.1, 0x70 + sequence, payload)
        })
    }
    /// The worker owner's `ACCEPT_TASK` binding the signed acknowledgment of `receipt`.
    fn accept(&mut self, order: &Order, receipt: &Receipt, sequence: u8, at: u64) -> Checked {
        let acknowledgment = acknowledgment_digest(&receipt.acknowledgment)?;
        let accept = self.step(
            dispatch::ACCEPT_TASK,
            sequence,
            order.task,
            acknowledgment.bytes(),
        )?;
        self.applied(&accept, at)?;
        Ok(())
    }
    /// The owner's routed `RevokeDelegate` of generation 1 (reason 1) under role `sequence`.
    fn revoke(&mut self, sequence: u8, at: u64) -> Checked {
        let mut payload = self.worker.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.push(1);
        payload.extend_from_slice(&1u64.to_be_bytes());
        let revoke = Call {
            sequence: u64::from(sequence),
            expiry: ROLE_EXPIRY,
            ..self.bound(
                dispatch::RevokeDelegate,
                identities()?.1,
                0x70 + sequence,
                payload,
            )
        };
        self.applied(&revoke, at)?;
        Ok(())
    }
    /// Routes `call`, which must apply exactly one revision.
    fn applied(&mut self, call: &Call, at: u64) -> Checked<Frame> {
        let before = self.world.revision()?;
        let frame = self.world.route(call, at)?;
        assert_eq!(
            (frame.status, frame.error, frame.revision),
            (ResultStatus::Ok, None, before + 1)
        );
        assert_eq!(frame.digest, codec::result_digest(&frame.payload)?.bytes());
        Ok(frame)
    }
    /// Routes `call`, which must be refused without any new state; an `UNAUTHORIZED` frame
    /// discloses no revision.
    fn refused(&self, call: &Call, at: u64) -> Checked<ApplicationError> {
        let (frame, next) = route(call, Some(&self.world.bytes), at)?;
        assert!(next.is_none());
        let error = frame.error.ok_or(NON_CANONICAL)?;
        let visible = if error == UNAUTHORIZED {
            0
        } else {
            self.world.revision()?
        };
        assert_eq!(
            (frame.status, frame.revision, frame.payload.len()),
            (ResultStatus::Error, visible, 0)
        );
        Ok(error)
    }
    /// A finalized authority over a capture of the committed bytes at `at`, and that capture.
    fn finalized(&self, at: u64) -> Checked<(FinalizedAuthority, Vec<u8>)> {
        let (bytes, facts) = capture(&self.world.bytes, at)?;
        let (market, owner, worker) = identities()?;
        let authority = FinalizedAuthority::bind(
            &AuthorityEvidence {
                state: &bytes,
                facts: &facts,
                finality: &FinalityEvidence {
                    native_state_root: Digest32::new(ROOT)?,
                    checkpoint: Digest32::new([0xC1; 32])?,
                    settlement: Presence::Present(Digest32::new([0xD3; 32])?),
                    rank: 4,
                },
                head: Head {
                    chain_sequence: 77,
                    sealed_batch: at + MAX_FINALITY_LAG,
                    finalised_checkpoint: [0xC1; 32],
                },
                owner: IdentityEvidence {
                    principal: owner,
                    primary_key: PublicKey32([0x0E; 32]),
                    frozen: false,
                    execution_height: at,
                },
                roster: Some(self.roster.as_slice()),
                observed_ms: 5_000,
            },
            ChainDomain::new(CHAIN)?,
            ProgramId::new(PROGRAM)?,
            market,
            worker,
        )?;
        Ok((authority, bytes))
    }
    /// The worker's finalized view at `at` with the currently published signed metadata.
    fn view(&self, at: u64) -> Checked<View> {
        let (authority, captured) = self.finalized(at)?;
        assert_eq!(authority.policy(), self.frozen.policy);
        let metadata = authority.bind_metadata(verify_signed_metadata(
            &self.signed,
            &authority.metadata_context(),
        )?)?;
        Ok(View {
            authority,
            metadata,
            captured,
            customer: customer_at(at)?,
        })
    }
    /// The customer's signed submit of `order` naming `capability`.
    fn submit(
        authority: &FinalizedAuthority,
        order: &Order,
        capability: &Capability,
        ask: Ask,
    ) -> Checked<Submit> {
        let binding = authority.worker_binding();
        let request = ServiceRequest {
            receiver: binding.worker,
            task: order.task,
            generation: 1,
            key_version: 1,
            metadata_revision: binding.metadata_revision,
            method: Route::Submit.code(),
            route: Route::Submit.code(),
            capability: capability.digest()?,
            workload_policy: authority.policy(),
            model: Digest32::new(capability.model)?,
            input_commitment: order.input,
            payload_bytes: u32::try_from(order.payload.len()).map_err(|_| ARITHMETIC)?,
            max_output_bytes: ask.max_output_bytes,
            max_units: ask.max_units,
            deadline_ms: 0,
            task_expiry: order.deadline,
            evaluation_access: Digest32::new([0xEA; 32])?,
            result_key: ask.result_key,
        };
        let context = ServiceContext {
            chain: binding.chain,
            program: binding.program,
            market: binding.market,
            actor: principal(CUSTOMER)?,
            epoch: 1,
            config: authority.config(),
            roster: authority.roster(),
            sequence: u64::from(order.request),
            expiry: order.deadline,
            request: RequestId::new([order.request; 32])?,
        };
        Ok(Submit { context, request })
    }
    /// The F06 reward state and the F05 tail, which no worker-side step may move.
    fn settlement(&self) -> Checked<Vec<u8>> {
        Ok(section_bytes(&self.world.bytes, Section::SettlementClaims)?)
    }
    fn admitted_report(&self, n: u8) -> Checked<bool> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, evaluator_id(n)?)?.is_some())
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
fn commitment_of(body: &ReportBody<'_>) -> CodecResult<CommitmentDigest> {
    codec::commitment_digest(
        &body.binding,
        codec::report_digest(body)?,
        Salt32::new(SALT)?,
    )
}
fn commit_payload(report: &SignedReport<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; commitment::COMMIT_SCORE_BYTES];
    codec::encode_commit_score(
        &CommitScorePayload {
            binding: report.body.binding,
            commitment: commitment_of(&report.body)?,
        },
        &mut out,
    )?;
    Ok(out)
}
fn reveal_payload(report: &SignedReport<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; commitment::REVEAL_MAX_BYTES];
    let len = codec::encode_reveal_score(
        &RevealScorePayload {
            report: report.body,
            signature: report.signature,
            salt: Salt32::new(SALT)?,
        },
        &mut out,
    )?;
    out.truncate(len);
    Ok(out)
}
/// Evidence root an evaluator seals over the committed worker result it assessed.
fn assessed(result: Digest32, n: u8) -> Checked<EvidenceRoot> {
    let mut bytes = result.as_bytes().to_vec();
    bytes.push(n);
    Ok(EvidenceRoot::new(
        codec::domain_hash(EVIDENCE_DOMAIN, &bytes)?.bytes(),
    )?)
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

/// F03 evaluator calls and the unrouted F05 aggregation through `aggregation::apply`.
impl Market {
    /// Routed keeper `SEAL_TASK_SET`, then routed `SealEvidence` of each `(evaluator, root)`
    /// under evaluator sequence 1.
    fn seal(&mut self, claims: &[(u8, EvidenceRoot)], at: u64) -> Checked {
        let binding = self.frozen_binding()?;
        let set = SetBinding {
            market: binding.market,
            epoch: binding.epoch,
            config: binding.config,
            policy: self.frozen.policy,
            roster: binding.roster,
        };
        let digest = {
            let state = decode_shared_state(&self.world.bytes)?;
            let section =
                PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
            tasks::task_set_digest(&set, &TaskSet::decode(section.task_region)?)?
        };
        let mut payload = binding.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&binding.config.get().to_be_bytes());
        payload.extend_from_slice(digest.as_bytes());
        let seal = self.bound(dispatch::SEAL_TASK_SET, principal(KEEPER)?, 0x62, payload);
        self.applied(&seal, at)?;
        let sealed =
            tasks::sealed_task_set(&decode_shared_state(&self.world.bytes)?, binding.epoch)?;
        for &(n, root) in claims {
            let mut claim = 1u16.to_be_bytes().to_vec();
            claim.extend_from_slice(root.as_bytes());
            claim.extend_from_slice(self.frozen.policy.as_bytes());
            claim.extend_from_slice(sealed.as_bytes());
            claim.extend_from_slice(rubric()?.as_bytes());
            claim.push(1);
            let call = Call {
                sequence: 1,
                request: tag(0xc5, n, 1),
                expiry: ROLE_EXPIRY,
                ..self.bound(dispatch::SealEvidence, principal(n)?, 0, claim)
            };
            self.applied(&call, at)?;
        }
        Ok(())
    }
    /// A report body naming `evaluator` with `evidence` and canonical `scores`.
    fn report<'a>(
        &self,
        evaluator: EvaluatorId,
        evidence: EvidenceRoot,
        scores: &'a [u8],
        key: &SigningKey,
    ) -> Checked<SignedReport<'a>> {
        Ok(sign(
            ReportBody {
                binding: EvaluatorBinding {
                    frozen: self.frozen_binding()?,
                    evaluator,
                    grant: version()?,
                    key_version: version()?,
                },
                evidence,
                scores: ScoreVector::Encoded(scores),
            },
            key,
        )?)
    }
    /// `CommitScore` of `report` by `actor` at evaluator sequence 2.
    fn commit(&self, actor: PrincipalId, n: u8, report: &SignedReport<'_>) -> Checked<Call> {
        Ok(Call {
            sequence: 2,
            request: tag(0xa1, n, 2),
            expiry: WORK_START + 80,
            ..self.bound(dispatch::CommitScore, actor, 0, commit_payload(report)?)
        })
    }
    /// `RevealScore` of `report` by `actor` at evaluator sequence 3.
    fn reveal(&self, actor: PrincipalId, n: u8, report: &SignedReport<'_>) -> Checked<Call> {
        Ok(Call {
            sequence: 3,
            request: tag(0xa2, n, 3),
            expiry: WORK_START + 96,
            ..self.bound(dispatch::RevealScore, actor, 0, reveal_payload(report)?)
        })
    }
    fn aggregation_call(
        &self,
        operation: Operation,
        payload: Vec<u8>,
        cursor: u16,
    ) -> Checked<Call> {
        let [_, selector] = operation.selector().to_be_bytes();
        Ok(Call {
            request: tag(0xf5, selector, u64::from(cursor)),
            ..self.bound(operation, principal(RELAYER)?, 0, payload)
        })
    }
    fn aggregate(&mut self, call: &Call, at: u64) -> Checked<Progress> {
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
            return Err(Failure::Unexpected("aggregation applied nothing"));
        };
        next.truncate(state_len);
        self.world.bytes = next;
        Ok(progress)
    }
    /// Begin, every Process chunk, then Finalize.
    fn settle(&mut self, at: u64) -> Checked<Progress> {
        let begin = self.aggregation_call(dispatch::BeginAggregation, Vec::new(), 0)?;
        let mut progress = self.aggregate(&begin, at)?;
        assert_eq!(progress.phase, AggregationPhase::Processing);
        let Presence::Present(input) = progress.input else {
            return Err(Failure::Unexpected("aggregation input"));
        };
        while progress.cursor < progress.worker_count {
            let mut payload = input.as_bytes().to_vec();
            payload.extend_from_slice(&progress.cursor.to_be_bytes());
            let call =
                self.aggregation_call(dispatch::ProcessAggregation, payload, progress.cursor)?;
            progress = self.aggregate(&call, at)?;
        }
        let finalize = self.aggregation_call(
            dispatch::FinalizeAggregation,
            input.as_bytes().to_vec(),
            u16::MAX,
        )?;
        let done = self.aggregate(&finalize, at)?;
        assert_eq!(done.phase, AggregationPhase::Terminal);
        Ok(done)
    }
    /// Every F05 worker aggregate of the current record.
    fn outputs(&self) -> Checked<Vec<WorkerAggregate>> {
        let section = self.settlement()?;
        let tail = section.get(REWARD_STATE_BYTES..).ok_or(NOT_FOUND)?;
        let (record, _) = tail.split_at(record_len(tail)?);
        let current = decode_current(record, self.frozen_binding()?, &[self.entry])?;
        Ok((0..current.output_count())
            .map(|i| current.output(i))
            .collect::<CodecResult<Vec<_>>>()?)
    }
}

/// Test PKI of the pinned-TLS worker binary.
struct Pki {
    roots: Arc<RootCertStore>,
    ca_pem: String,
    leaf: CertificateDer<'static>,
    leaf_pem: String,
    key_pem: String,
}
fn pki() -> Checked<Pki> {
    let ca_key = KeyPair::generate()?;
    let mut ca_params = CertificateParams::new(Vec::<String>::new())?;
    ca_params.distinguished_name = DistinguishedName::new();
    ca_params
        .distinguished_name
        .push(DnType::CommonName, "paxai real workload test root");
    ca_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let ca = ca_params.self_signed(&ca_key)?;
    let issuer = Issuer::from_params(&ca_params, &ca_key);
    let leaf_key = KeyPair::generate()?;
    let leaf =
        CertificateParams::new(vec!["localhost".to_owned()])?.signed_by(&leaf_key, &issuer)?;
    let mut roots = RootCertStore::empty();
    roots.add(ca.der().clone())?;
    Ok(Pki {
        roots: Arc::new(roots),
        ca_pem: ca.pem(),
        leaf: leaf.der().clone(),
        leaf_pem: leaf.pem(),
        key_pem: leaf_key.serialize_pem(),
    })
}
fn free_port() -> Checked<u16> {
    Ok(TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?
        .local_addr()?
        .port())
}
fn loopback() -> impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>> {
    |_, _| Ok(vec![IpAddr::V4(Ipv4Addr::LOCALHOST)])
}
/// A fresh directory under `parent`.
fn fresh(parent: &Path, name: &str) -> Checked<PathBuf> {
    let dir = parent.join(format!("real-workload-{name}-{}", std::process::id()));
    if dir.exists() {
        fs::remove_dir_all(&dir)?;
    }
    fs::create_dir_all(&dir)?;
    Ok(dir)
}
/// The running worker binary; killed when dropped.
struct Running(Child);
impl Drop for Running {
    fn drop(&mut self) {
        if self.0.kill().is_ok() {
            let _ = self.0.wait();
        }
    }
}
/// The worker binary serving `signed` over TLS with the test PKI on `port`.
fn serve(name: &str, pki: &Pki, signed: &[u8], port: u16) -> Checked<Running> {
    let dir = fresh(Path::new(env!("CARGO_TARGET_TMPDIR")), name)?;
    let file = |name: &str| dir.join(name);
    fs::write(file("chain.pem"), format!("{}{}", pki.leaf_pem, pki.ca_pem))?;
    fs::write(file("key.pem"), &pki.key_pem)?;
    fs::write(file("delegate.hex"), hex(&DELEGATE_SEED))?;
    fs::write(file("metadata.bin"), signed)?;
    let (market, owner, worker) = identities()?;
    let text = |value: String| Value::String(value);
    let path = |name: &str| Value::String(file(name).to_string_lossy().into_owned());
    let mut config = Map::new();
    config.insert("listen".into(), text(format!("127.0.0.1:{port}")));
    config.insert("certificate_chain".into(), path("chain.pem"));
    config.insert("private_key".into(), path("key.pem"));
    config.insert("delegate_seed".into(), path("delegate.hex"));
    config.insert("signed_metadata".into(), path("metadata.bin"));
    config.insert("chain".into(), text(hex(&CHAIN)));
    config.insert("program".into(), text(hex(&PROGRAM)));
    config.insert("market".into(), text(hex(market.as_bytes())));
    config.insert("worker".into(), text(hex(worker.as_bytes())));
    config.insert("owner".into(), text(hex(owner.as_bytes())));
    let config_path = file("worker.json");
    fs::write(&config_path, serde_json::to_string(&Value::Object(config))?)?;
    let mut child = Command::new(env!("CARGO_BIN_EXE_layerx-paxai-worker"))
        .arg("--config")
        .arg(&config_path)
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()?;
    let stdout = child.stdout.take();
    let running = Running(child);
    let mut line = String::new();
    BufReader::new(stdout.ok_or(Failure::Unexpected("worker stdout"))?).read_line(&mut line)?;
    if line.starts_with("listening 127.0.0.1:") {
        Ok(running)
    } else {
        Err(Failure::Unexpected("worker did not start listening"))
    }
}
/// The signed status of the advertised endpoint verified against `authority`.
fn verified_transport(
    pki: &Pki,
    authority: &FinalizedAuthority,
    metadata: &VerifiedMetadata,
) -> Checked<Readiness> {
    let client = EndpointClient {
        profile: NetworkProfile::Private {
            allowlist: vec![IpAddr::V4(Ipv4Addr::LOCALHOST)],
        },
        roots: Arc::clone(&pki.roots),
        timeout: IO_TIMEOUT,
    };
    let endpoint = metadata
        .manifest()
        .endpoints
        .first()
        .cloned()
        .ok_or(Failure::Unexpected("advertised endpoint"))?;
    let observed = client.verify_status(
        authority,
        &endpoint,
        &StatusChallenge {
            epoch: 1,
            config: authority.config(),
            roster: authority.roster(),
            expiry: COMMIT_OPEN - 2,
            challenge: RequestId::new([0x5C; 32])?,
        },
        7_000,
        loopback(),
    )?;
    assert_eq!(
        observed,
        ReadinessObservation {
            state: Readiness::VerifiedTransport,
            observed_ms: 7_000,
        }
    );
    Ok(observed.state)
}

/// The P-REAL-SERVICES pins: the model runner executable, the durable store root, the
/// deployment every model is served under and the two licensed workloads. The runner speaks
/// the `runner` module protocol, publishes each RESULT manifest as the worker owner bound to
/// the admitted task, and for an output above the requested bound either returns the full
/// output or fails with `OUTPUT_TOO_LARGE`.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Provisioned {
    runner: RunnerPin,
    store: PathBuf,
    deployment: String,
    inference: WorkloadPin,
    embedding: WorkloadPin,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RunnerPin {
    program: PathBuf,
    args: Vec<String>,
    control_timeout_ms: u64,
}
/// One licensed model artifact pinned as a capability, with its encrypted test payload and
/// the deterministic output it must produce.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct WorkloadPin {
    kind: u8,
    mode: u8,
    model: String,
    model_manifest: String,
    tokenizer: String,
    input_schema: String,
    output_schema: String,
    max_input_bytes: u32,
    max_output_bytes: u32,
    max_input_units: u32,
    max_output_units: u32,
    unit_kind: u8,
    latency_ms: u32,
    concurrency: u16,
    determinism: u8,
    payload: PathBuf,
    result_key: Option<String>,
    request_max_output_bytes: u32,
    request_max_units: u32,
    expected: Expected,
}
#[derive(Clone, Deserialize)]
#[serde(deny_unknown_fields)]
struct Expected {
    output_bytes: u32,
    output_sha256: String,
    input_units: u64,
    output_units: u64,
    dimension: Option<u64>,
    items: Option<u64>,
}
/// A loaded workload pin.
#[derive(Clone)]
struct Workload {
    capability: Capability,
    payload: Vec<u8>,
    ask: Ask,
    expected: Expected,
    output_sha256: [u8; 32],
}
impl WorkloadPin {
    fn load(&self) -> Checked<Workload> {
        let result_key = match &self.result_key {
            Some(key) => Some(unhex32(key)?),
            None => None,
        };
        Ok(Workload {
            capability: Capability {
                kind: self.kind,
                mode: self.mode,
                model: unhex32(&self.model)?,
                model_manifest: unhex32(&self.model_manifest)?,
                tokenizer: unhex32(&self.tokenizer)?,
                input_schema: unhex32(&self.input_schema)?,
                output_schema: unhex32(&self.output_schema)?,
                max_input_bytes: self.max_input_bytes,
                max_output_bytes: self.max_output_bytes,
                max_input_units: self.max_input_units,
                max_output_units: self.max_output_units,
                unit_kind: self.unit_kind,
                latency_ms: self.latency_ms,
                concurrency: self.concurrency,
                determinism: self.determinism,
            },
            payload: fs::read(&self.payload)?,
            ask: Ask {
                max_output_bytes: self.request_max_output_bytes,
                max_units: self.request_max_units,
                result_key,
            },
            expected: self.expected.clone(),
            output_sha256: unhex32(&self.expected.output_sha256)?,
        })
    }
}

/// A provisioned replica: the opened market advertising both pinned workloads at a pinned-TLS
/// worker binary, and the durable job service on the real runner.
struct Real {
    m: Market,
    pki: Pki,
    service: JobService,
    inference: Workload,
    embedding: Workload,
    deployment: Digest32,
    _worker: Running,
}
fn real(name: &str) -> Checked<Real> {
    let path = std::env::var_os(CONFIG_ENV).ok_or(Failure::Unprovisioned(UNPROVISIONED))?;
    let pins: Provisioned = serde_json::from_slice(&fs::read(path)?)?;
    let (inference, embedding) = (pins.inference.load()?, pins.embedding.load()?);
    assert_eq!(
        (
            inference.capability.unit_kind,
            embedding.capability.unit_kind
        ),
        (TOKENS, VECTOR_ELEMENTS)
    );
    let deployment = Digest32::new(unhex32(&pins.deployment)?)?;
    let pki = pki()?;
    let port = free_port()?;
    let m = opened(
        vec![inference.capability, embedding.capability],
        vec![endpoint_at(port, spki_sha256(&pki.leaf)?)],
        deployment,
    )?;
    let worker = serve(name, &pki, &m.signed, port)?;
    let store = fresh(&pins.store, name)?;
    let service = JobService::new(
        JobStore::open(&store)?,
        ProcessRunner::new(
            pins.runner.program,
            pins.runner.args.into_iter().map(OsString::from).collect(),
            Duration::from_millis(pins.runner.control_timeout_ms),
        ),
        delegate(),
    );
    Ok(Real {
        m,
        pki,
        service,
        inference,
        embedding,
        deployment,
        _worker: worker,
    })
}
/// One placed and chain-accepted job.
struct Placed {
    order: Order,
    submit: Submit,
    receipt: Receipt,
}
impl Real {
    /// ADMIT at `at`, then the submit to the durable service at `at + 5` after the pinned TLS
    /// and the runner report ready; nothing is accepted on chain.
    fn ordered(
        &mut self,
        workload: &Workload,
        nonce: u8,
        deadline: u64,
        ask: Ask,
        at: u64,
    ) -> Checked<Placed> {
        let order = self
            .m
            .order(nonce, nonce, workload.payload.clone(), deadline, at)?;
        let view = self.m.view(at + 5)?;
        let transport = verified_transport(&self.pki, &view.authority, &view.metadata)?;
        assert_eq!(
            self.service
                .readiness(transport, &view.authority, &view.metadata),
            Readiness::RunnerReady
        );
        let submit = Market::submit(&view.authority, &order, &workload.capability, ask)?;
        let admitted = view.task(order.task)?;
        let receipt = view.place(&self.service, &submit, &order, &admitted, NOW)?;
        let (acknowledged, _) =
            decode_acknowledgment(&receipt.acknowledgment, public(&delegate()))?;
        assert_eq!(
            (
                acknowledged.task,
                acknowledged.request_commitment,
                acknowledged.model,
                acknowledged.deployment,
                acknowledged.admitted_height
            ),
            (
                order.task,
                decode_service(&submit.signed()?)?.digest,
                Digest32::new(workload.capability.model)?,
                self.deployment,
                at + 5
            )
        );
        Ok(Placed {
            order,
            submit,
            receipt,
        })
    }
    /// [`Self::ordered`], then the owner's ACCEPT of the signed acknowledgment at `at + 6`
    /// under role `sequence`.
    fn placed(
        &mut self,
        workload: &Workload,
        (nonce, sequence): (u8, u8),
        deadline: u64,
        ask: Ask,
        at: u64,
    ) -> Checked<Placed> {
        let placed = self.ordered(workload, nonce, deadline, ask, at)?;
        self.m
            .accept(&placed.order, &placed.receipt, sequence, at + 6)?;
        Ok(placed)
    }
    /// Claim and run against the finalized F01 task at `at`.
    fn dispatch(
        &self,
        placed: &Placed,
        at: u64,
        now_ms: u64,
    ) -> Checked<Result<JobState, ServiceError>> {
        let view = self.m.view(at)?;
        let task = view.task(placed.order.task)?;
        Ok(self
            .service
            .dispatch(placed.receipt.key, &task, &view.authority, now_ms))
    }
    /// The signed result body of a finished job.
    fn result(&self, key: JobKey) -> Checked<(Vec<u8>, ServiceResult)> {
        let saved = self
            .service
            .store()
            .job(key)?
            .result
            .ok_or(Failure::Unexpected("no signed result"))?;
        let (body, _) = decode_result(&saved, public(&delegate()))?;
        Ok((saved, body))
    }
}

/// The finished completion of the one retained runner report of `key`.
fn completion(service: &JobService, key: JobKey) -> Checked<(Vec<u8>, Report, Completion)> {
    let retained = service.store().retained(key, false)?;
    let [raw] = retained.as_slice() else {
        return Err(Failure::Unexpected("exactly one retained runner report"));
    };
    let report = Report::decode(raw)?;
    let RunnerProgress::Finished(completion) = report.progress.clone() else {
        return Err(Failure::Unexpected("a finished runner report"));
    };
    Ok((raw.clone(), report, completion))
}

impl View {
    /// The refusal of a new `SubmitJob` of `submit`, which must not be admitted.
    fn refusal(
        &self,
        service: &JobService,
        submit: &Submit,
        order: &Order,
        task: &TaskBinding,
        now_ms: u64,
    ) -> Checked<ServiceError> {
        match self.place(service, submit, order, task, now_ms) {
            Ok(_) => Err(Failure::Unexpected("a refused submit was admitted")),
            Err(Failure::Service(error)) => Ok(error),
            Err(other) => Err(other),
        }
    }
}
/// `submit` under a new customer request id and sequence `request`.
fn renewed(submit: &Submit, request: u8) -> Checked<Submit> {
    Ok(Submit {
        context: ServiceContext {
            sequence: u64::from(request),
            request: RequestId::new([request; 32])?,
            ..submit.context
        },
        ..*submit
    })
}
fn segment(role: Role, unit_kind: u8, count: u64) -> Segment {
    Segment {
        role,
        unit_kind,
        count,
    }
}
/// A successful runner completion measured over `[start_ns, finish_ns]`.
fn finished(segments: Vec<Segment>, output: Vec<u8>, start_ns: u64, finish_ns: u64) -> Completion {
    Completion {
        succeeded: true,
        error_code: 0,
        start_ns,
        finish_ns,
        started_at_ms: NOW,
        finished_at_ms: NOW + 4,
        segments,
        output,
        output_manifest: vec![1],
    }
}
const TOKEN_BOUNDS: Bounds = Bounds {
    unit_kind: TOKENS,
    max_output_bytes: 1_024,
    max_input_units: 4_096,
    max_units: 256,
};
/// The F06 reward row of the opened epoch.
fn reward_row(m: &Market) -> Checked<RewardEpoch> {
    let settlement = m.settlement()?;
    let state = settlement.get(..REWARD_STATE_BYTES).ok_or(NOT_FOUND)?;
    Ok(decode_reward_state(state)?.row(m.frozen.epoch)?)
}

/// A durable replica whose configured runner executable is not installed.
fn uninstalled(name: &str) -> Checked<JobService> {
    let dir = fresh(Path::new(env!("CARGO_TARGET_TMPDIR")), name)?;
    Ok(JobService::new(
        JobStore::open(&dir.join("store"))?,
        ProcessRunner::new(
            dir.join("runner-not-installed"),
            Vec::new(),
            Duration::from_secs(5),
        ),
        delegate(),
    ))
}

#[test]
fn a11_processing_time_is_floored_and_counts_never_wrap() -> Checked {
    assert_eq!(processing_ms(1_000_000, 4_999_999), Ok(3));
    assert_eq!(processing_ms(5, 5), Ok(0));
    assert_eq!(processing_ms(5, 4), Err(EvidenceFault::MeasurementReversed));
    let measured = finished(
        vec![
            segment(Role::Input, TOKENS, 100),
            segment(Role::Output, TOKENS, 200),
        ],
        vec![0x6F; 64],
        1_000_000,
        4_999_999,
    );
    assert_eq!(
        measured.validate(&TOKEN_BOUNDS),
        Ok(Usage {
            input_units: 100,
            output_units: 200,
            processing_ms: 3,
        })
    );
    let reversed = Completion {
        start_ns: 4_999_999,
        finish_ns: 1_000_000,
        ..measured.clone()
    };
    assert_eq!(
        reversed.validate(&TOKEN_BOUNDS),
        Err(EvidenceFault::MeasurementReversed)
    );
    let output_overflow = Completion {
        segments: vec![
            segment(Role::Output, TOKENS, u64::MAX),
            segment(Role::Output, TOKENS, 1),
        ],
        ..measured.clone()
    };
    let input_overflow = Completion {
        segments: vec![
            segment(Role::Input, TOKENS, 1),
            segment(Role::Input, TOKENS, u64::MAX),
        ],
        ..measured.clone()
    };
    for overflowing in [&output_overflow, &input_overflow] {
        assert_eq!(
            overflowing.validate(&TOKEN_BOUNDS),
            Err(EvidenceFault::CountOverflow)
        );
    }
    let reversed_and_overflowing = Completion {
        start_ns: 2,
        finish_ns: 1,
        ..output_overflow.clone()
    };
    assert_eq!(
        reversed_and_overflowing.validate(&TOKEN_BOUNDS),
        Err(EvidenceFault::MeasurementReversed)
    );
    assert_eq!(
        (
            EvidenceFault::MeasurementReversed.error(),
            EvidenceFault::CountOverflow.error()
        ),
        (ServiceError::NonCanonical, ServiceError::Overflow)
    );
    let report = Report {
        job: JobKey::derive(
            identities()?.0,
            principal(CUSTOMER)?,
            RequestId::new([0x11; 32])?,
        )?,
        fence: 1,
        progress: RunnerProgress::Finished(output_overflow),
    };
    let carried = Report::decode(&report.encode()?)?;
    assert_eq!(carried, report);
    let RunnerProgress::Finished(carried) = carried.progress else {
        return Err(Failure::Unexpected("a finished report"));
    };
    assert_eq!(
        carried.validate(&TOKEN_BOUNDS),
        Err(EvidenceFault::CountOverflow)
    );
    Ok(())
}

#[test]
fn a08_output_bytes_units_and_unit_kind_are_bounded() {
    let at_bound = finished(
        vec![
            segment(Role::Input, TOKENS, 4_096),
            segment(Role::Output, TOKENS, 200),
            segment(Role::Output, TOKENS, 56),
        ],
        vec![0x6F; 1_024],
        0,
        2_000_000,
    );
    assert_eq!(
        at_bound.validate(&TOKEN_BOUNDS),
        Ok(Usage {
            input_units: 4_096,
            output_units: 256,
            processing_ms: 2,
        })
    );
    let too_long = Completion {
        output: vec![0x6F; 1_025],
        ..at_bound.clone()
    };
    let too_many_units = Completion {
        segments: vec![
            segment(Role::Input, TOKENS, 4_096),
            segment(Role::Output, TOKENS, 200),
            segment(Role::Output, TOKENS, 57),
        ],
        ..at_bound.clone()
    };
    let too_much_input = Completion {
        segments: vec![
            segment(Role::Input, TOKENS, 4_097),
            segment(Role::Output, TOKENS, 256),
        ],
        ..at_bound.clone()
    };
    let vectors = Completion {
        segments: vec![
            segment(Role::Input, VECTOR_ELEMENTS, 4),
            segment(Role::Output, VECTOR_ELEMENTS, 256),
        ],
        ..at_bound.clone()
    };
    let mixed = Completion {
        segments: vec![
            segment(Role::Input, TOKENS, 10),
            segment(Role::Output, VECTOR_ELEMENTS, 10),
        ],
        ..at_bound.clone()
    };
    let refused = [
        (
            too_long,
            EvidenceFault::OutputTooLarge,
            ServiceError::OutputTooLarge,
        ),
        (
            too_many_units,
            EvidenceFault::UnitsAboveBound,
            ServiceError::OutputTooLarge,
        ),
        (
            too_much_input,
            EvidenceFault::InputUnitsAboveBound,
            ServiceError::InputTooLarge,
        ),
        (
            vectors.clone(),
            EvidenceFault::UnitMismatch,
            ServiceError::CapabilityMismatch,
        ),
        (
            mixed,
            EvidenceFault::UnitMismatch,
            ServiceError::CapabilityMismatch,
        ),
    ];
    for (completion, fault, error) in refused {
        assert_eq!(completion.validate(&TOKEN_BOUNDS), Err(fault));
        assert_eq!(fault.error(), error);
    }
    assert_eq!(
        vectors.validate(&Bounds {
            unit_kind: VECTOR_ELEMENTS,
            ..TOKEN_BOUNDS
        }),
        Ok(Usage {
            input_units: 4,
            output_units: 256,
            processing_ms: 2,
        })
    );
}

#[test]
fn a17_local_acknowledgment_without_finalized_acceptance_never_dispatches() -> Checked {
    let pki = pki()?;
    let port = free_port()?;
    let deployment = Digest32::new([0xDE; 32])?;
    let model = capability(0x31);
    let mut m = opened(
        vec![model],
        vec![endpoint_at(port, spki_sha256(&pki.leaf)?)],
        deployment,
    )?;
    let _worker = serve("a17-endpoint", &pki, &m.signed, port)?;
    let service = uninstalled("a17-replica")?;
    let order = m.order(0x31, 0x31, vec![0x5A; 1_024], 1_172, ADMIT_AT)?;
    let view = m.view(1_150)?;
    let admitted = view.task(order.task)?;
    assert_eq!(
        (admitted.status, admitted.acknowledgement, admitted.result),
        (TaskStatus::Admitted, None, None)
    );
    let submit = Market::submit(&view.authority, &order, &model, FIXTURE_ASK)?;
    let receipt = view.place(&service, &submit, &order, &admitted, NOW)?;
    assert_eq!((receipt.key, receipt.result), (order.key()?, None));
    let (acknowledged, _) = decode_acknowledgment(&receipt.acknowledgment, public(&delegate()))?;
    assert_eq!(
        (
            acknowledged.task,
            acknowledged.request_commitment,
            acknowledged.model,
            acknowledged.deployment,
            acknowledged.admitted_height
        ),
        (
            order.task,
            decode_service(&submit.signed()?)?.digest,
            Digest32::new(model.model)?,
            deployment,
            1_150
        )
    );
    assert_eq!(
        service.store().usage()?,
        QueueUsage {
            queued: 1,
            running: 0,
            queued_bytes: 1_024,
        }
    );
    let settlement = m.settlement()?;
    let unaccepted = m.view(1_151)?;
    assert_eq!(
        service.dispatch(
            receipt.key,
            &unaccepted.task(order.task)?,
            &unaccepted.authority,
            NOW + 1
        ),
        Err(ServiceError::AdmissionNotEffective)
    );
    let foreign = Digest32::new([0xA5; 32])?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, order.task, foreign.bytes())?;
    m.applied(&accept, 1_151)?;
    let view = m.view(1_152)?;
    let accepted = view.task(order.task)?;
    assert_eq!(
        (accepted.status, accepted.acknowledgement, accepted.result),
        (TaskStatus::Accepted, Some(foreign), None)
    );
    assert_ne!(
        Some(acknowledgment_digest(&receipt.acknowledgment)?),
        accepted.acknowledgement
    );
    assert_eq!(
        service.dispatch(receipt.key, &accepted, &view.authority, NOW + 2),
        Err(ServiceError::StaleAuthority)
    );
    assert_eq!(service.runner().invocations(), 0);
    let job = service.store().job(receipt.key)?;
    assert_eq!((job.state.state, job.result), (JobState::Accepted, None));
    assert_eq!(service.store().output(receipt.key)?, None);
    assert_eq!(service.store().retained(receipt.key, false)?.len(), 0);
    assert_eq!(m.settlement()?, settlement);
    assert_eq!(
        service.readiness(Readiness::Advertised, &view.authority, &view.metadata),
        Readiness::Advertised
    );
    assert_eq!(service.runner().invocations(), 0);
    let transport = verified_transport(&pki, &view.authority, &view.metadata)?;
    assert_eq!(
        service.readiness(transport, &view.authority, &view.metadata),
        Readiness::NotReady
    );
    assert_eq!(service.runner().invocations(), 1);
    Ok(())
}

#[test]
fn a15_worker_signed_quality_claim_is_no_evaluator_score() -> Checked {
    let mut m = opened(
        vec![capability(0x31)],
        vec![endpoint_at(443, [0xE1; 32])],
        Digest32::new([0xDE; 32])?,
    )?;
    let roots = [
        (2u8, EvidenceRoot::new([0xE2; 32])?),
        (3, EvidenceRoot::new([0xE3; 32])?),
        (4, EvidenceRoot::new([0xE4; 32])?),
    ];
    m.seal(&roots, COMMIT_OPEN)?;
    let reserved = reward_row(&m)?;
    assert_eq!(
        (reserved.status, reserved.outcome, reserved.paid_sum),
        (EpochStatus::Reserved, RewardOutcome::Undecided, 0)
    );
    let scores = entries(&[(m.worker, 1_000_000)]);
    let (market, owner, _) = identities()?;
    let own = m.report(
        derive_evaluator(market, owner, [1; 32])?,
        roots[0].1,
        &scores,
        &delegate(),
    )?;
    assert_eq!(
        m.refused(&m.commit(owner, 1, &own)?, COMMIT_OPEN)?,
        UNAUTHORIZED
    );
    let borrowed = m.report(evaluator_id(2)?, roots[0].1, &scores, &delegate())?;
    assert_eq!(
        m.refused(&m.commit(owner, 2, &borrowed)?, COMMIT_OPEN)?,
        UNAUTHORIZED
    );
    assert_eq!(
        m.refused(&m.reveal(owner, 2, &borrowed)?, REVEAL_OPEN)?,
        UNAUTHORIZED
    );
    for n in 2..=4 {
        assert!(!m.admitted_report(n)?);
    }
    m.settle(SETTLE_OPEN)?;
    let outputs = m.outputs()?;
    let [output] = outputs.as_slice() else {
        return Err(Failure::Unexpected("exactly one worker aggregate"));
    };
    assert_eq!(
        (
            output.worker(),
            output.generation(),
            output.support(),
            output.status(),
            output.score().get(),
            output.quality()
        ),
        (
            m.worker,
            version()?,
            0,
            QualityStatus::InsufficientQuorum,
            0,
            Presence::Absent
        )
    );
    let terminal = reward_row(&m)?;
    assert_eq!(
        (
            terminal.status,
            terminal.outcome,
            terminal.budget,
            terminal.paid_sum,
            terminal.entry_count
        ),
        (
            EpochStatus::Terminal,
            RewardOutcome::NoEligibleScore,
            reserved.budget,
            0,
            0
        )
    );
    Ok(())
}

/// The signed usage of `body` is exactly the runner's measured completion under the admitted
/// bounds, inside the capability latency, and an embedding counts `dimension * items`.
fn measured(workload: &Workload, body: &ServiceResult, completion: &Completion) -> Checked {
    let (capability, expected) = (workload.capability, &workload.expected);
    assert_eq!(
        completion.validate(&Bounds {
            unit_kind: capability.unit_kind,
            max_output_bytes: workload.ask.max_output_bytes,
            max_input_units: capability.max_input_units,
            max_units: workload.ask.max_units,
        }),
        Ok(Usage {
            input_units: body.input_units,
            output_units: body.output_units,
            processing_ms: body.processing_ms,
        })
    );
    assert!(body.processing_ms <= u64::from(capability.latency_ms));
    assert_eq!(
        (body.started_at_ms, body.finished_at_ms),
        (completion.started_at_ms, completion.finished_at_ms)
    );
    if capability.unit_kind == VECTOR_ELEMENTS {
        let (dimension, items) = expected
            .dimension
            .zip(expected.items)
            .ok_or(Failure::Unexpected("the pinned embedding shape"))?;
        assert_eq!(
            body.output_units,
            dimension.checked_mul(items).ok_or(ARITHMETIC)?
        );
        assert!(completion
            .segments
            .iter()
            .all(|segment| segment.unit_kind == VECTOR_ELEMENTS));
    }
    Ok(())
}

/// Checks the signed SUCCEEDED result of `placed` against its runner evidence, the pinned
/// output and the admitted bounds; returns the signed result.
fn succeeded(real: &Real, workload: &Workload, placed: &Placed) -> Checked<Vec<u8>> {
    let (capability, expected) = (workload.capability, &workload.expected);
    let key = placed.receipt.key;
    let (saved, body) = real.result(key)?;
    let (raw, report, completion) = completion(&real.service, key)?;
    let (market, owner, worker) = identities()?;
    assert_eq!(
        (
            body.market,
            body.worker,
            body.task,
            body.request,
            body.request_commitment,
            body.metadata,
            (body.generation, body.key_version, body.epoch),
            body.deployment,
            body.model
        ),
        (
            market,
            worker,
            placed.order.task,
            RequestId::new([placed.order.request; 32])?,
            decode_service(&placed.submit.signed()?)?.digest,
            real.m.metadata,
            (1, 1, 1),
            real.deployment,
            Digest32::new(capability.model)?
        )
    );
    assert_eq!(
        (
            body.outcome,
            body.error_code,
            body.output_bytes,
            body.input_units,
            body.output_units,
            body.evidence
        ),
        (
            Outcome::Succeeded,
            0,
            expected.output_bytes,
            expected.input_units,
            expected.output_units,
            Some(evidence_digest(&raw)?)
        )
    );
    assert_eq!(
        (report.job, report.fence),
        (key, real.service.store().job(key)?.state.fence)
    );
    let output = real
        .service
        .store()
        .output(key)?
        .ok_or(Failure::Unexpected("the retained output"))?;
    let output_sha256: [u8; 32] = Sha256::digest(&output).into();
    assert_eq!(output, completion.output);
    assert_eq!(
        (
            u32::try_from(output.len()).map_err(|_| ARITHMETIC)?,
            output_sha256
        ),
        (expected.output_bytes, workload.output_sha256)
    );
    assert_eq!(
        body.output_commitment,
        Some(manifest_digest(&completion.output_manifest)?)
    );
    let published = decode_manifest(&completion.output_manifest)?;
    assert_eq!(
        (
            published.kind,
            published.publisher,
            published.epoch,
            published.subject,
            published.byte_length
        ),
        (
            ArtifactKind::Result,
            owner,
            1,
            placed.order.task.bytes(),
            u64::from(expected.output_bytes)
        )
    );
    measured(workload, &body, &completion)?;
    Ok(saved)
}

/// A pinned workload placed, accepted, run once on the real runner and committed on chain.
fn end_to_end(name: &str, embedding: bool) -> Checked {
    let mut real = real(name)?;
    let workload = if embedding {
        real.embedding.clone()
    } else {
        real.inference.clone()
    };
    let placed = real.placed(&workload, (1, 1), 1_177, workload.ask, ADMIT_AT)?;
    let settlement = real.m.settlement()?;
    assert_eq!(
        real.dispatch(&placed, 1_152, NOW + 1)?,
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let key = placed.receipt.key;
    let saved = succeeded(&real, &workload, &placed)?;
    let view = real.m.view(1_153)?;
    let digest = real.service.commit_record(key, &view.authority)?;
    assert_eq!(digest, result_manifest_digest(&saved)?);
    let commit = real.m.step(
        dispatch::COMMIT_TASK_RESULT,
        2,
        placed.order.task,
        digest.bytes(),
    )?;
    real.m.applied(&commit, 1_153)?;
    let committed = real.m.view(1_154)?.task(placed.order.task)?;
    assert_eq!(
        (committed.status, committed.result),
        (TaskStatus::ResultCommitted, Some(digest))
    );
    assert_eq!(real.m.settlement()?, settlement);
    for n in 2..=4 {
        assert!(!real.m.admitted_report(n)?);
    }
    Ok(())
}

#[test]
fn a08_a11_real_inference_workload_end_to_end() -> Checked {
    end_to_end("inference", false)
}

#[test]
fn a08_a11_real_embedding_workload_end_to_end() -> Checked {
    end_to_end("embedding", true)
}

#[test]
fn a08_real_output_above_the_requested_bound_fails_without_output() -> Checked {
    let mut real = real("bound")?;
    let workload = real.inference.clone();
    let ask = Ask {
        max_output_bytes: workload
            .expected
            .output_bytes
            .checked_sub(1)
            .ok_or(ARITHMETIC)?,
        ..workload.ask
    };
    let placed = real.placed(&workload, (1, 1), 1_177, ask, ADMIT_AT)?;
    assert_eq!(
        real.dispatch(&placed, 1_152, NOW + 1)?,
        Ok(JobState::Ended(Outcome::Failed))
    );
    let key = placed.receipt.key;
    let (_, body) = real.result(key)?;
    assert_eq!(
        (
            body.outcome,
            body.error_code,
            body.output_commitment,
            body.output_bytes
        ),
        (
            Outcome::Failed,
            ServiceError::OutputTooLarge.code(),
            None,
            0
        )
    );
    assert_eq!(real.service.store().output(key)?, None);
    assert_eq!(real.service.store().retained(key, false)?.len(), 1);
    let view = real.m.view(1_153)?;
    assert_eq!(
        real.service.commit_record(key, &view.authority)?,
        result_manifest_digest(&real.result(key)?.0)?
    );
    Ok(())
}

#[test]
fn a17_real_single_dispatch_and_late_commit_refusal() -> Checked {
    let mut real = real("single")?;
    let workload = real.inference.clone();
    let placed = real.ordered(&workload, 1, 1_160, workload.ask, ADMIT_AT)?;
    let key = placed.receipt.key;
    let before = real.service.runner().invocations();
    assert_eq!(
        real.dispatch(&placed, 1_150, NOW + 1)?,
        Err(ServiceError::AdmissionNotEffective)
    );
    assert_eq!(real.service.runner().invocations(), before);
    real.m.accept(&placed.order, &placed.receipt, 1, 1_151)?;
    let settlement = real.m.settlement()?;
    assert_eq!(
        real.dispatch(&placed, 1_152, NOW + 2)?,
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    assert_eq!(real.service.runner().invocations(), before + 1);
    assert_eq!(
        real.dispatch(&placed, 1_153, NOW + 3)?,
        Err(ServiceError::IdempotencyConflict)
    );
    assert_eq!(real.service.runner().invocations(), before + 1);
    let (saved, _) = real.result(key)?;
    let output = real.service.store().output(key)?;
    assert!(output.is_some());
    let late = real.m.view(1_160)?;
    assert_eq!(
        real.service.commit_record(key, &late.authority),
        Err(ServiceError::DeadlineInvalid)
    );
    let job = real.service.store().job(key)?;
    assert!(job.state.late);
    assert_eq!(
        (job.result, real.service.store().output(key)?),
        (Some(saved.clone()), output)
    );
    let commit = real.m.step(
        dispatch::COMMIT_TASK_RESULT,
        2,
        placed.order.task,
        result_manifest_digest(&saved)?.bytes(),
    )?;
    assert_eq!(real.m.refused(&commit, 1_160)?, F01_TASK_EXPIRED);
    let task = late.task(placed.order.task)?;
    assert_eq!((task.status, task.result), (TaskStatus::Accepted, None));
    assert_eq!(real.m.settlement()?, settlement);
    Ok(())
}

#[test]
fn a15_real_expired_metadata_keeps_the_admitted_model_binding() -> Checked {
    let mut real = real("relabel")?;
    let workload = real.inference.clone();
    let placed = real.placed(&workload, (1, 1), 1_182, workload.ask, 1_150)?;
    let key = placed.receipt.key;
    let expired = real.m.view(EXPIRY)?;
    assert_eq!(
        expired.authority.admission_gate(),
        Err(ServiceError::MetadataExpired)
    );
    let task = expired.task(placed.order.task)?;
    assert_eq!(
        expired.refusal(
            &real.service,
            &renewed(&placed.submit, 0x21)?,
            &placed.order,
            &task,
            NOW + 1
        )?,
        ServiceError::MetadataExpired
    );
    assert_eq!(
        real.dispatch(&placed, EXPIRY, NOW + 2)?,
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let (saved, body) = real.result(key)?;
    let (admitted_metadata, admitted_model) =
        (real.m.metadata, Digest32::new(workload.capability.model)?);
    assert_eq!(
        (body.metadata, body.model),
        (admitted_metadata, admitted_model)
    );
    let relabelled = manifest(
        2,
        COMMIT_OPEN,
        real.deployment,
        vec![capability(0x41)],
        real.m.manifest.endpoints.clone(),
    )?;
    let signed = signed_metadata(&relabelled, 1)?;
    let (_, digest) = Manifest::decode(&relabelled.encode()?)?;
    let worker = real.m.worker;
    real.m.world.edit(|parts, _| {
        let current = parts.workers.get(worker).ok_or(NON_CANONICAL)?;
        parts.workers.replace(&WorkerCurrent {
            metadata: digest,
            metadata_revision: 2,
            expiry: COMMIT_OPEN,
            last_metadata_height: EXPIRY,
            ..current
        })
    })?;
    (real.m.signed, real.m.manifest, real.m.metadata) = (signed, relabelled, digest);
    let view = real.m.view(EXPIRY + 1)?;
    assert_eq!(view.authority.admission_gate(), Ok(()));
    assert_eq!(real.result(key)?, (saved.clone(), body));
    let committed = real.service.commit_record(key, &view.authority)?;
    assert_eq!(committed, result_manifest_digest(&saved)?);
    let commit = real.m.step(
        dispatch::COMMIT_TASK_RESULT,
        2,
        placed.order.task,
        committed.bytes(),
    )?;
    real.m.applied(&commit, EXPIRY + 1)?;
    let stale = Market::submit(
        &view.authority,
        &placed.order,
        &workload.capability,
        workload.ask,
    )?;
    assert_eq!(
        view.refusal(
            &real.service,
            &renewed(&stale, 0x22)?,
            &placed.order,
            &view.task(placed.order.task)?,
            NOW + 3
        )?,
        ServiceError::CapabilityMismatch
    );
    let (kept, _) = decode_acknowledgment(&placed.receipt.acknowledgment, public(&delegate()))?;
    assert_eq!(
        (kept.metadata, kept.model),
        (admitted_metadata, admitted_model)
    );
    Ok(())
}

/// Evaluators 2..=4 seal evidence over the committed `result`, commit and reveal 700,000 for
/// the worker; F05 aggregates exactly that score and F06 allocates the whole reserved budget to
/// the worker without any transfer.
fn scored(m: &mut Market, result: Digest32) -> Checked {
    let mut claims = Vec::new();
    for n in 2..=4u8 {
        claims.push((n, assessed(result, n)?));
    }
    m.seal(&claims, COMMIT_OPEN)?;
    let reserved = reward_row(m)?;
    let scores = entries(&[(m.worker, 700_000)]);
    let mut reports = Vec::new();
    for &(n, root) in &claims {
        reports.push((
            n,
            m.report(evaluator_id(n)?, root, &scores, &evaluator_key(n))?,
        ));
    }
    for (n, report) in &reports {
        let commit = m.commit(principal(*n)?, *n, report)?;
        m.applied(&commit, COMMIT_OPEN)?;
    }
    for ((n, report), at) in reports.iter().zip(REVEAL_OPEN..) {
        let reveal = m.reveal(principal(*n)?, *n, report)?;
        m.applied(&reveal, at)?;
    }
    for n in 2..=4 {
        assert!(m.admitted_report(n)?);
    }
    m.settle(SETTLE_OPEN)?;
    let outputs = m.outputs()?;
    let [aggregate] = outputs.as_slice() else {
        return Err(Failure::Unexpected("exactly one worker aggregate"));
    };
    assert_eq!(
        (
            aggregate.worker(),
            aggregate.generation(),
            aggregate.support(),
            aggregate.status(),
            aggregate.score().get()
        ),
        (
            m.worker,
            version()?,
            3,
            QualityStatus::ScoredPositive,
            700_000
        )
    );
    let terminal = reward_row(m)?;
    assert_eq!(
        (
            terminal.status,
            terminal.outcome,
            terminal.budget,
            terminal.paid_sum,
            terminal.entry_count,
            terminal.entries[0].entitlement
        ),
        (
            EpochStatus::Terminal,
            RewardOutcome::Allocated,
            reserved.budget,
            0,
            1,
            reserved.budget
        )
    );
    Ok(())
}

#[test]
fn a19_real_revoked_delegate_keeps_completed_evidence_and_evaluator_samples() -> Checked {
    let mut real = real("revoked")?;
    let workload = real.inference.clone();
    let first = real.placed(&workload, (1, 1), 1_177, workload.ask, ADMIT_AT)?;
    assert_eq!(
        real.dispatch(&first, 1_152, NOW + 1)?,
        Ok(JobState::Ended(Outcome::Succeeded))
    );
    let (saved, _) = real.result(first.receipt.key)?;
    let evidence = real.service.store().retained(first.receipt.key, false)?;
    let output = real.service.store().output(first.receipt.key)?;
    let view = real.m.view(1_153)?;
    let result = real
        .service
        .commit_record(first.receipt.key, &view.authority)?;
    let commit = real.m.step(
        dispatch::COMMIT_TASK_RESULT,
        2,
        first.order.task,
        result.bytes(),
    )?;
    real.m.applied(&commit, 1_153)?;
    let second = real.ordered(&workload, 2, 1_176, workload.ask, 1_154)?;
    let invocations = real.service.runner().invocations();
    real.m.revoke(3, 1_160)?;
    let revoked = real.m.view(1_161)?;
    assert_eq!(
        (
            revoked.authority.admission_gate(),
            revoked.authority.release_gate()
        ),
        (
            Err(ServiceError::DelegateRevoked),
            Err(ServiceError::DelegateRevoked)
        )
    );
    assert_eq!(
        real.service.readiness(
            Readiness::RunnerReady,
            &revoked.authority,
            &revoked.metadata
        ),
        Readiness::NotReady
    );
    let pending = revoked.task(second.order.task)?;
    assert_eq!(
        revoked.refusal(
            &real.service,
            &renewed(&second.submit, 0x23)?,
            &second.order,
            &pending,
            NOW + 2
        )?,
        ServiceError::DelegateRevoked
    );
    assert_eq!(
        real.dispatch(&second, 1_161, NOW + 3)?,
        Err(ServiceError::DelegateRevoked)
    );
    assert_eq!(real.service.runner().invocations(), invocations);
    let admit = real.m.admit_call(3, Digest32::new([0x1E; 32])?, 1_176)?;
    assert_eq!(real.m.refused(&admit, 1_161)?, F02_DELEGATE_REVOKED);
    let acknowledgment = acknowledgment_digest(&second.receipt.acknowledgment)?;
    let delegated = Call {
        delegate: true,
        ..real.m.step(
            dispatch::ACCEPT_TASK,
            4,
            second.order.task,
            acknowledgment.bytes(),
        )?
    };
    assert_eq!(real.m.refused(&delegated, 1_161)?, F02_DELEGATE_REVOKED);
    let kept = revoked.task(first.order.task)?;
    assert_eq!(
        (kept.status, kept.result),
        (TaskStatus::ResultCommitted, Some(result))
    );
    assert_eq!(
        (
            real.service.store().job(first.receipt.key)?.result,
            real.service.store().retained(first.receipt.key, false)?,
            real.service.store().output(first.receipt.key)?
        ),
        (Some(saved), evidence, output)
    );
    scored(&mut real.m, result)
}
