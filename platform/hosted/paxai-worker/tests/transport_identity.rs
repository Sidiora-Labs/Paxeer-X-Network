//! AI.F02-T02 transport identity: signed service envelopes, delegate-signed metadata,
//! discovery from a finalized capture of a really opened market, the pinned TLS transport and
//! the worker binary.
use ed25519_dalek::{Signer, SigningKey};
use layerx_client::head::Head;
use layerx_paxai_worker::{
    auth::{
        acknowledgment_digest, admit, decode_acknowledgment, decode_service, encode_service,
        sign_metadata, verify_request, verify_signed_metadata, Acknowledgment, Admission,
        AuthenticatedRequest, JobReference, MetadataContext, MetadataPublication, Route,
        ServiceContext, ServiceError, ServiceOperation, ServiceRequest, StatusChallenge,
        ACKNOWLEDGMENT_BYTES, ACKNOWLEDGMENT_DOMAIN, ERROR_BODY_BYTES, JOB_REFERENCE_BYTES,
        QUERY_PATH, RESULT_KEY_BYTES, SERVICE_ERRORS, SERVICE_PREFIX_BYTES, SERVICE_REQUEST_BYTES,
        SERVICE_SUFFIX_BYTES, SUBMIT_PATH,
    },
    discovery::{
        discover, spki_sha256, AuthorityEvidence, EndpointClient, FinalizedAuthority,
        IdentityEvidence, NetworkProfile, Readiness, ReadinessObservation, TransportError,
        VerifiedMetadata, MAX_FINALITY_LAG, READINESS_TTL_MS,
    },
    metadata::{next_revision, Capability, Endpoint, Manifest},
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
    errors::{ApplicationError, CodecResult, ARITHMETIC, NON_CANONICAL, UNKNOWN_OPERATION},
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
    workers::{
        WorkerCurrent, WorkerState, WorkerTable, MAX_MANIFEST_BYTES, WORKER_TABLE_MAX_BYTES,
    },
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};
use rcgen::{
    date_time_ymd, BasicConstraints, CertificateParams, DistinguishedName, DnType, IsCa, Issuer,
    KeyPair, PublicKeyData,
};
use rustls::{
    pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer},
    RootCertStore, ServerConfig, ServerConnection, StreamOwned,
};
use serde_json::{Map, Value};
use sha2::{Digest as _, Sha256};
use std::fmt::Write as _;
use std::fs;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, TcpListener};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
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
const WORK_CLOSE: u64 = 1192;
const CHUNK_RESPONSE_MAX: usize = 8_244;
const DEFAULT_URI: &str = "https://localhost/paxai/v1";
const IO_TIMEOUT: Duration = Duration::from_secs(10);
const UNPINNED: &str = "refusing to start: leaf certificate is not pinned by the signed metadata\n";

enum Failure {
    Application(ApplicationError),
    Service(ServiceError),
    Transport(TransportError),
    Query(QueryError),
    Io(io::ErrorKind),
    Tls(rustls::Error),
    Certificate(rcgen::Error),
    Json(serde_json::Error),
    Unexpected(&'static str),
}
impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Service(error) => write!(f, "service refusal {error:?}"),
            Self::Transport(error) => write!(f, "transport refusal {error:?}"),
            Self::Query(error) => write!(f, "query refusal {error:?}"),
            Self::Io(kind) => write!(f, "io failure {kind:?}"),
            Self::Tls(error) => write!(f, "tls failure {error:?}"),
            Self::Certificate(error) => write!(f, "certificate generation {error:?}"),
            Self::Json(error) => write!(f, "configuration encoding {error:?}"),
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
    fn schedule(&mut self, epoch: u64, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, &payload, at)
    }
    fn suspend(&mut self, at: u64) -> CodecResult<()> {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&[0x5E; 32]);
        self.owner_op(dispatch::SUSPEND, &payload, at)
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
fn endpoint_at(port: u16, pin: [u8; 32]) -> Endpoint {
    endpoint(&format!("https://localhost:{port}/paxai/v1"), pin)
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
    frozen: Frozen,
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
    let frozen = world.open(1129)?;
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
        frozen,
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
impl Admitting {
    fn admit(&self, submit: &Submit) -> Checked<Result<(), ServiceError>> {
        Ok(submit
            .admit(&self.authority, &customer_at(HEIGHT)?, &self.metadata)?
            .map(|_| ()))
    }
}

/// A test CA, a `localhost` leaf it issued, and an expired leaf over the same key.
struct Pki {
    roots: Arc<RootCertStore>,
    ca_pem: String,
    leaf: CertificateDer<'static>,
    leaf_pem: String,
    expired: CertificateDer<'static>,
    key_der: Vec<u8>,
    key_pem: String,
    spki: Vec<u8>,
}
fn pki() -> Checked<Pki> {
    let ca_key = KeyPair::generate()?;
    let mut ca_params = CertificateParams::new(Vec::<String>::new())?;
    ca_params.distinguished_name = DistinguishedName::new();
    ca_params
        .distinguished_name
        .push(DnType::CommonName, "paxai transport test root");
    ca_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let ca = ca_params.self_signed(&ca_key)?;
    let issuer = Issuer::from_params(&ca_params, &ca_key);
    let leaf_key = KeyPair::generate()?;
    let leaf =
        CertificateParams::new(vec!["localhost".to_owned()])?.signed_by(&leaf_key, &issuer)?;
    let mut expired = CertificateParams::new(vec!["localhost".to_owned()])?;
    expired.not_before = date_time_ymd(2000, 1, 1);
    expired.not_after = date_time_ymd(2001, 1, 1);
    let expired = expired.signed_by(&leaf_key, &issuer)?;
    let mut roots = RootCertStore::empty();
    roots.add(ca.der().clone())?;
    Ok(Pki {
        roots: Arc::new(roots),
        ca_pem: ca.pem(),
        leaf: leaf.der().clone(),
        leaf_pem: leaf.pem(),
        expired: expired.der().clone(),
        key_der: leaf_key.serialize_der(),
        key_pem: leaf_key.serialize_pem(),
        spki: leaf_key.subject_public_key_info(),
    })
}
fn server_config(certificate: CertificateDer<'static>, key: &[u8]) -> Checked<Arc<ServerConfig>> {
    let config =
        ServerConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_protocol_versions(&[&rustls::version::TLS13])?
            .with_no_client_auth()
            .with_single_cert(
                vec![certificate],
                PrivateKeyDer::from(PrivatePkcs8KeyDer::from(key.to_vec())),
            )?;
    Ok(Arc::new(config))
}
fn complete(request: &[u8]) -> bool {
    let Some(end) = request.windows(4).position(|w| w == b"\r\n\r\n") else {
        return false;
    };
    let head = String::from_utf8_lossy(&request[..end]);
    let length = head
        .lines()
        .find_map(|line| line.strip_prefix("Content-Length: "))
        .and_then(|value| value.parse::<usize>().ok())
        .unwrap_or(0);
    request.len() >= end + 4 + length
}
/// One TLS 1.3 connection: returns every application byte received; a complete request is
/// answered with a redirect.
fn serve_once(listener: TcpListener, config: Arc<ServerConfig>) -> JoinHandle<Vec<u8>> {
    thread::spawn(move || {
        let mut received = Vec::new();
        let Ok((socket, _)) = listener.accept() else {
            return received;
        };
        let Ok(connection) = ServerConnection::new(config) else {
            return received;
        };
        if socket.set_read_timeout(Some(IO_TIMEOUT)).is_err() {
            return received;
        }
        let mut stream = StreamOwned::new(connection, socket);
        let mut buffer = [0u8; 4_096];
        while let Ok(n @ 1..) = stream.read(&mut buffer) {
            received.extend_from_slice(&buffer[..n]);
            if complete(&received) {
                let redirect = b"HTTP/1.1 302 Found\r\nLocation: https://localhost/paxai/v1/jobs\r\nContent-Length: 0\r\nConnection: close\r\n\r\n";
                if stream
                    .write_all(redirect)
                    .and_then(|()| stream.flush())
                    .is_ok()
                {
                    stream.conn.send_close_notify();
                    if stream.conn.complete_io(&mut stream.sock).is_err() {
                        return received;
                    }
                }
                break;
            }
        }
        received
    })
}
fn joined(server: JoinHandle<Vec<u8>>) -> Checked<Vec<u8>> {
    server
        .join()
        .map_err(|_| Failure::Unexpected("server thread panicked"))
}
fn answers(ips: Vec<IpAddr>) -> impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>> {
    move |_, _| Ok(ips)
}
fn loopback() -> impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>> {
    answers(vec![IpAddr::V4(Ipv4Addr::LOCALHOST)])
}
fn local_client(pki: &Pki) -> EndpointClient {
    EndpointClient {
        profile: NetworkProfile::Private {
            allowlist: vec![IpAddr::V4(Ipv4Addr::LOCALHOST)],
        },
        roots: Arc::clone(&pki.roots),
        timeout: IO_TIMEOUT,
    }
}
fn free_port() -> Checked<u16> {
    Ok(TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?
        .local_addr()?
        .port())
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
/// Worker files and configuration under the test target directory.
fn worker_config(name: &str, pki: &Pki, signed: &[u8], port: u16) -> Checked<PathBuf> {
    let dir = PathBuf::from(env!("CARGO_TARGET_TMPDIR"))
        .join(format!("transport-identity-{name}-{}", std::process::id()));
    fs::create_dir_all(&dir)?;
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
    Ok(config_path)
}
fn worker_command(config: &Path) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_layerx-paxai-worker"));
    command.arg("--config").arg(config);
    command
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
fn start(config: &Path) -> Checked<Running> {
    let mut child = worker_command(config)
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

#[test]
fn service_error_space_and_bodies() -> Checked {
    assert_eq!(SERVICE_ERRORS.len(), 29);
    let id = RequestId::new([0x42; 32])?;
    for (index, error) in SERVICE_ERRORS.iter().enumerate() {
        assert_eq!(usize::from(error.code()), index + 1);
        assert_eq!(ServiceError::from_code(error.code()), Ok(*error));
        assert!(error.name().is_ok(), "{error:?} is registered");
        let body = error.body(Presence::Present(id));
        assert_eq!(body.len(), ERROR_BODY_BYTES);
        assert_eq!(&body[..2], &error.code().to_be_bytes());
        assert_eq!(&body[2..], id.as_bytes());
        assert_eq!(
            ServiceError::decode_body(&body),
            Ok((*error, Presence::Present(id)))
        );
        assert_eq!(
            ServiceError::decode_body(&error.body(Presence::Absent)),
            Ok((*error, Presence::Absent))
        );
    }
    assert_eq!(ServiceError::from_code(0), Err(ServiceError::NonCanonical));
    assert_eq!(ServiceError::from_code(30), Err(ServiceError::NonCanonical));
    let body = ServiceError::AccessDenied.body(Presence::Present(id));
    assert_eq!(
        ServiceError::decode_body(&body[..33]),
        Err(ServiceError::NonCanonical)
    );
    Ok(())
}

#[test]
fn service_envelope_layout_and_route_binding() -> Checked {
    let fixture = admitting()?;
    let submit = &fixture.submit;
    let payload = submit.request.encode()?;
    assert_eq!(payload.len(), SERVICE_REQUEST_BYTES + RESULT_KEY_BYTES);
    let signed = submit.signed(&customer())?;
    assert_eq!(
        signed.len(),
        SERVICE_PREFIX_BYTES + payload.len() + SERVICE_SUFFIX_BYTES
    );
    assert_eq!(&signed[..7], b"PAXAIS1");
    assert_eq!(&signed[203..235], submit.context.request.as_bytes());
    assert_eq!(
        &signed[SERVICE_PREFIX_BYTES..][..payload.len()],
        &payload[..]
    );
    let envelope = decode_service(&signed)?;
    assert_eq!(
        (envelope.operation, envelope.context, envelope.signer),
        (
            ServiceOperation::SubmitJob,
            submit.context,
            public(&customer())
        )
    );
    assert_eq!(ServiceRequest::decode(&envelope.payload)?, submit.request);
    let mut trailing = signed.clone();
    trailing.push(0);
    assert_eq!(decode_service(&trailing), Err(ServiceError::NonCanonical));
    let mut schema = signed.clone();
    schema[8] = 2;
    assert_eq!(
        decode_service(&schema),
        Err(ServiceError::UnsupportedVersion)
    );
    let mut request_id = signed.clone();
    request_id[203] ^= 1;
    assert_eq!(decode_service(&request_id), Err(ServiceError::BadSignature));
    let mut body = signed;
    body[SERVICE_PREFIX_BYTES] ^= 1;
    assert_eq!(decode_service(&body), Err(ServiceError::BadSignature));
    let reference = JobReference {
        receiver: submit.request.receiver,
        task: submit.request.task,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        method: Route::Query.code(),
        route: Route::Query.code(),
        original: submit.context.request,
    };
    assert_eq!(reference.encode().len(), JOB_REFERENCE_BYTES);
    assert_eq!(JobReference::decode(&reference.encode())?, reference);
    let query = decode_service(&encode_service(
        ServiceOperation::QueryJob,
        &submit.context,
        &reference.encode(),
        &customer(),
    )?)?;
    let binding = fixture.authority.worker_binding();
    assert_eq!(
        verify_request(&query, Route::Query, &binding),
        Ok(AuthenticatedRequest::Query(reference))
    );
    assert_eq!(
        verify_request(&query, Route::Cancel, &binding),
        Err(ServiceError::BadSignature)
    );
    assert_eq!(
        verify_request(&envelope, Route::Query, &binding),
        Err(ServiceError::BadSignature)
    );
    assert_eq!(
        verify_request(&envelope, Route::Submit, &binding),
        Ok(AuthenticatedRequest::Submit(submit.request))
    );
    Ok(())
}

#[test]
fn service_operations_are_not_native_selectors() -> Checked {
    let fixture = admitting()?;
    let result = encode_service(
        ServiceOperation::JobResult,
        &fixture.submit.context,
        &[0x5A; 64],
        &delegate(),
    )?;
    assert_eq!(decode_envelope(&result).map(|_| ()), Err(NON_CANONICAL));
    for selector in 0x0281..=0x0286u16 {
        assert_eq!(
            ServiceOperation::from_code(selector).map(ServiceOperation::code),
            Ok(selector)
        );
        assert_eq!(Operation::decode(selector), Err(UNKNOWN_OPERATION));
    }
    assert_eq!(
        ServiceOperation::from_code(0x0287),
        Err(ServiceError::NonCanonical)
    );
    Ok(())
}

#[test]
fn a03_metadata_window_lifetime_and_revision_overflow() -> Checked {
    let base = manifest(
        1,
        vec![capability(0x31)],
        vec![endpoint(DEFAULT_URI, [0xE1; 32])],
    )?;
    let window = Manifest {
        valid_from: 300,
        expiry: 428,
        ..base.clone()
    };
    let (decoded, _) = Manifest::decode(&window.encode()?)?;
    assert_eq!(decoded, window);
    assert_eq!(
        decoded.check_window(299),
        Err(ServiceError::AdmissionNotEffective)
    );
    assert_eq!(decoded.check_window(300), Ok(()));
    assert_eq!(decoded.check_window(427), Ok(()));
    assert_eq!(
        decoded.check_window(428),
        Err(ServiceError::MetadataExpired)
    );
    let longest = Manifest {
        valid_from: 300,
        expiry: 556,
        ..base.clone()
    };
    assert!(Manifest::decode(&longest.encode()?).is_ok());
    let too_long = Manifest {
        valid_from: 300,
        expiry: 557,
        ..base.clone()
    };
    assert_eq!(too_long.encode(), Err(ServiceError::NonCanonical));
    let empty = Manifest {
        valid_from: 428,
        expiry: 428,
        ..base
    };
    assert_eq!(empty.encode(), Err(ServiceError::NonCanonical));
    assert_eq!(next_revision(1), Ok(2));
    assert_eq!(next_revision(u64::MAX), Err(ServiceError::Overflow));
    Ok(())
}

#[test]
fn a13_manifest_grammar_refusals() -> Checked {
    let base = manifest(
        1,
        vec![capability(0x31)],
        vec![endpoint(DEFAULT_URI, [0xE1; 32])],
    )?;
    let bytes = base.encode()?;
    let mut schema = bytes.clone();
    schema[1] = 2;
    assert_eq!(
        Manifest::decode(&schema),
        Err(ServiceError::UnsupportedVersion)
    );
    let mut reserved = bytes.clone();
    if let Some(last) = reserved.last_mut() {
        *last = 1;
    }
    assert_eq!(Manifest::decode(&reserved), Err(ServiceError::NonCanonical));
    let mut trailing = bytes;
    trailing.push(0);
    assert_eq!(Manifest::decode(&trailing), Err(ServiceError::NonCanonical));
    for capabilities in [
        vec![capability(0x31), capability(0x31)],
        vec![capability(0x32), capability(0x31)],
        vec![],
    ] {
        let refused = Manifest {
            capabilities,
            ..base.clone()
        };
        assert_eq!(refused.encode(), Err(ServiceError::NonCanonical));
    }
    for uri in [
        "https://user@localhost/paxai/v1",
        "https://localhost/paxai/v1?q=1",
        "https://localhost/paxai/v1#f",
        "http://localhost/paxai/v1",
        "https://localhost/paxai/v2",
        "https://localhost:0/paxai/v1",
        "https://127.0.0.1/paxai/v1",
        "https://LOCALHOST/paxai/v1",
    ] {
        let refused = Manifest {
            endpoints: vec![endpoint(uri, [0xE1; 32])],
            ..base.clone()
        };
        assert_eq!(refused.encode(), Err(ServiceError::NonCanonical), "{uri}");
    }
    let unpinned = Manifest {
        endpoints: vec![endpoint(DEFAULT_URI, [0; 32])],
        ..base
    };
    assert_eq!(unpinned.encode(), Err(ServiceError::NonCanonical));
    assert_eq!(
        endpoint("https://localhost:8443/paxai/v1", [0xE1; 32]).authority(),
        Ok(("localhost", 8443))
    );
    assert_eq!(
        endpoint(DEFAULT_URI, [0xE1; 32]).authority(),
        Ok(("localhost", 443))
    );
    Ok(())
}

/// The canonical grammar caps a manifest at 8 capabilities and 2 endpoints of at most
/// 276-byte URIs, so the largest canonical manifest is 2366 bytes; padding to the 8192-byte
/// cap is trailing data and one byte more is over capacity.
#[test]
fn a13_manifest_size_bound() -> Checked {
    let host = format!(
        "{}.{}.{}.{}",
        "a".repeat(63),
        "b".repeat(63),
        "c".repeat(63),
        "d".repeat(61)
    );
    let largest = Manifest {
        capabilities: (1..=8u8).map(capability).collect(),
        endpoints: vec![
            endpoint(&format!("https://{host}:65534/paxai/v1"), [0xE1; 32]),
            Endpoint {
                id: 2,
                ..endpoint(&format!("https://{host}:65535/paxai/v1"), [0xE2; 32])
            },
        ],
        ..manifest(1, vec![], vec![])?
    };
    let bytes = largest.encode()?;
    assert_eq!(bytes.len(), 2_366);
    assert_eq!(Manifest::decode(&bytes)?.0, largest);
    let mut padded = bytes;
    padded.resize(MAX_MANIFEST_BYTES, 0);
    assert_eq!(Manifest::decode(&padded), Err(ServiceError::NonCanonical));
    padded.push(0);
    assert_eq!(
        Manifest::decode(&padded),
        Err(ServiceError::CapacityExceeded)
    );
    let nine = Manifest {
        capabilities: (1..=9u8).map(capability).collect(),
        ..largest.clone()
    };
    assert_eq!(nine.encode(), Err(ServiceError::NonCanonical));
    let same_uri = Manifest {
        endpoints: vec![
            endpoint(DEFAULT_URI, [0xE1; 32]),
            Endpoint {
                id: 2,
                ..endpoint(DEFAULT_URI, [0xE2; 32])
            },
        ],
        ..largest
    };
    assert_eq!(same_uri.encode(), Err(ServiceError::NonCanonical));
    Ok(())
}

#[test]
fn a13_signed_metadata_binds_delegate_owner_and_worker() -> Checked {
    let base = manifest(
        1,
        vec![capability(0x31)],
        vec![endpoint(DEFAULT_URI, [0xE1; 32])],
    )?;
    let bytes = base.encode()?;
    let context = metadata_context()?;
    let signed = sign(&base, 0)?;
    let accepted = verify_signed_metadata(&signed, &context)?;
    assert_eq!(
        (
            &accepted.manifest,
            accepted.expected_revision,
            accepted.context
        ),
        (&base, 0, context)
    );
    assert_eq!(accepted.digest, Manifest::decode(&bytes)?.1);
    assert_eq!(accepted.binding().delegate, public(&delegate()));
    let stranger = SigningKey::from_bytes(&[0x5D; 32]);
    assert_eq!(
        sign_metadata(&context, &publication(&bytes, 0)?, &stranger),
        Err(ServiceError::BadSignature)
    );
    let foreign = MetadataContext {
        delegate: public(&stranger),
        ..context
    };
    let by_stranger = sign_metadata(&foreign, &publication(&bytes, 0)?, &stranger)?;
    assert_eq!(
        verify_signed_metadata(&by_stranger, &context),
        Err(ServiceError::BadSignature)
    );
    let mut forged = signed.clone();
    if let Some(last) = forged.last_mut() {
        *last ^= 1;
    }
    assert_eq!(
        verify_signed_metadata(&forged, &context),
        Err(ServiceError::BadSignature)
    );
    let other_chain = MetadataContext {
        chain: ChainDomain::new([0x55; 32])?,
        ..context
    };
    assert_eq!(
        verify_signed_metadata(&signed, &other_chain),
        Err(ServiceError::WrongDomain)
    );
    let other_owner = MetadataContext {
        owner: principal(2)?,
        ..context
    };
    assert_eq!(
        verify_signed_metadata(&signed, &other_owner),
        Err(ServiceError::OwnerRequired)
    );
    let unbound = MetadataContext {
        worker: WorkerId::new([0x99; 32])?,
        ..context
    };
    let relabelled = sign_metadata(&unbound, &publication(&bytes, 0)?, &delegate())?;
    assert_eq!(
        verify_signed_metadata(&relabelled, &unbound),
        Err(ServiceError::MetadataIntegrityFailure)
    );
    assert_eq!(
        verify_signed_metadata(&sign(&base, 1)?, &context),
        Err(ServiceError::WrongRevision)
    );
    Ok(())
}

#[test]
fn discovery_binds_a_finalized_capture_of_the_opened_market() -> Checked {
    let market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    let authority = authority_at(&market, HEIGHT)?;
    let (market_id, owner, worker) = identities()?;
    assert_eq!(
        (
            authority.height(),
            authority.config(),
            authority.current_epoch()
        ),
        (HEIGHT, 1, Some(1))
    );
    assert_eq!(authority.roster(), Presence::Present(market.frozen.roster));
    assert_eq!(
        codec::decode_roster(&market.roster)?.digest()?,
        market.frozen.roster
    );
    assert_eq!(authority.policy(), task_policy()?.digest()?);
    let binding = authority.worker_binding();
    assert_eq!(
        (
            binding.market,
            binding.worker,
            binding.owner,
            binding.delegate
        ),
        (market_id, worker, owner, public(&delegate()))
    );
    assert_eq!(authority.work_close()?, WORK_CLOSE);
    assert_eq!(authority.admission_gate(), Ok(()));
    assert_eq!(authority.release_gate(), Ok(()));
    let found = discover(&authority, |digest| {
        if digest == authority.record().metadata {
            Ok(market.signed.clone())
        } else {
            Err(io::ErrorKind::NotFound.into())
        }
    });
    assert_eq!(found.eligibility, Ok(()));
    assert_eq!(found.readiness, Readiness::Advertised);
    let metadata = found.metadata?;
    assert_eq!(metadata.digest(), authority.record().metadata);
    assert_eq!(metadata.manifest().capabilities, vec![capability(0x31)]);
    let missing = discover(&authority, |_| Err(io::ErrorKind::TimedOut.into()));
    assert_eq!(missing.metadata, Err(ServiceError::MetadataUnavailable));
    assert_eq!(missing.readiness, Readiness::NotReady);
    let replaced = sign(
        &manifest(
            1,
            vec![capability(0x41)],
            vec![endpoint(DEFAULT_URI, [0xE1; 32])],
        )?,
        0,
    )?;
    let substituted = discover(&authority, |_| Ok(replaced));
    assert_eq!(
        substituted.metadata,
        Err(ServiceError::MetadataIntegrityFailure)
    );
    assert_eq!(substituted.readiness, Readiness::NotReady);
    let garbage = discover(&authority, |_| Ok(vec![0x5A; 64]));
    assert_eq!(garbage.metadata, Err(ServiceError::NonCanonical));
    Ok(())
}

#[test]
fn finality_freshness_and_roster_evidence_refusals() -> Checked {
    let market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    let view = view(&market.world.bytes, &market.roster, HEIGHT)?;
    let knobs = view.knobs()?;
    assert!(view.bind(knobs).is_ok());
    assert!(view
        .bind(Knobs {
            sealed: HEIGHT,
            ..knobs
        })
        .is_ok());
    let refusals = [
        (Knobs { rank: 3, ..knobs }, ServiceError::StaleAuthority),
        (
            Knobs {
                sealed: HEIGHT + MAX_FINALITY_LAG + 1,
                ..knobs
            },
            ServiceError::StaleAuthority,
        ),
        (
            Knobs {
                sealed: HEIGHT - 1,
                ..knobs
            },
            ServiceError::StaleAuthority,
        ),
        (
            Knobs {
                roster: false,
                ..knobs
            },
            ServiceError::StaleAuthority,
        ),
        (
            Knobs {
                owner_height: HEIGHT - 1,
                ..knobs
            },
            ServiceError::StaleAuthority,
        ),
        (
            Knobs {
                market: MarketId::new([0x66; 32])?,
                ..knobs
            },
            ServiceError::WrongDomain,
        ),
        (
            Knobs {
                worker: WorkerId::new([0x99; 32])?,
                ..knobs
            },
            ServiceError::NotFound,
        ),
    ];
    for (changed, refusal) in refusals {
        assert_eq!(view.bind(changed).map(|_| ()), Err(refusal));
    }
    let mut other_roster = market.roster.clone();
    if let Some(last) = other_roster.last_mut() {
        *last ^= 1;
    }
    let tampered = View {
        roster: other_roster,
        ..view
    };
    assert_eq!(
        tampered.bind(knobs).map(|_| ()),
        Err(ServiceError::StaleAuthority)
    );
    Ok(())
}

#[test]
fn a03_admission_gate_follows_the_metadata_window() -> Checked {
    let market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    assert_eq!(
        authority_at(&market, VALID_FROM - 1)?.admission_gate(),
        Err(ServiceError::AdmissionNotEffective)
    );
    assert_eq!(authority_at(&market, VALID_FROM)?.admission_gate(), Ok(()));
    assert_eq!(authority_at(&market, EXPIRY - 1)?.admission_gate(), Ok(()));
    let expired = authority_at(&market, EXPIRY)?;
    assert_eq!(expired.admission_gate(), Err(ServiceError::MetadataExpired));
    assert_eq!(expired.release_gate(), Ok(()));
    let found = discover(&expired, |_| Ok(market.signed.clone()));
    assert_eq!(found.eligibility, Err(ServiceError::MetadataExpired));
    assert_eq!(found.readiness, Readiness::NotReady);
    assert_eq!(found.metadata?.digest(), expired.record().metadata);
    let fixture = admitting()?;
    let late = fixture
        .submit
        .admit(&expired, &customer_at(EXPIRY)?, &fixture.metadata)?;
    assert_eq!(late.map(|_| ()), Err(ServiceError::MetadataExpired));
    assert_eq!(
        authority_at(&market, WORK_CLOSE)?.admission_gate(),
        Err(ServiceError::AdmissionNotEffective)
    );
    Ok(())
}

#[test]
fn a12_owner_freeze_and_suspension_block_new_admission() -> Checked {
    let mut market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    let current = view(&market.world.bytes, &market.roster, HEIGHT)?;
    let frozen = current.bind(Knobs {
        frozen: true,
        ..current.knobs()?
    })?;
    assert_eq!(frozen.admission_gate(), Err(ServiceError::IdentityFrozen));
    assert_eq!(frozen.release_gate(), Err(ServiceError::IdentityFrozen));
    let metadata = verified(&frozen, &market.signed)?;
    let refused =
        submit(&frozen, &capability(0x31))?.admit(&frozen, &customer_at(HEIGHT)?, &metadata)?;
    assert_eq!(refused.map(|_| ()), Err(ServiceError::IdentityFrozen));
    let found = discover(&frozen, |_| Ok(market.signed.clone()));
    assert_eq!(
        (found.eligibility, found.readiness),
        (Err(ServiceError::IdentityFrozen), Readiness::NotReady)
    );
    market.world.suspend(HEIGHT)?;
    let paused = authority_at(&market, HEIGHT)?;
    assert_eq!(paused.admission_gate(), Err(ServiceError::MarketPaused));
    let found = discover(&paused, |_| Ok(market.signed.clone()));
    assert_eq!(
        (found.eligibility, found.readiness),
        (Err(ServiceError::MarketPaused), Readiness::NotReady)
    );
    Ok(())
}

#[test]
fn a07_admission_binds_the_request_and_acknowledgment() -> Checked {
    let fixture = admitting()?;
    let submit = &fixture.submit;
    let admission =
        submit.admit(&fixture.authority, &customer_at(HEIGHT)?, &fixture.metadata)??;
    assert_eq!(
        (
            admission.worker,
            admission.task,
            admission.metadata,
            admission.model,
            admission.deadline_ms,
            admission.task_deadline,
            admission.admitted_height
        ),
        (
            submit.request.receiver,
            submit.request.task,
            fixture.metadata.digest(),
            Digest32::new([0x31; 32])?,
            30_000,
            TASK_EXPIRY,
            HEIGHT
        )
    );
    assert_eq!(
        admission.request_commitment,
        decode_service(&submit.signed(&customer())?)?.digest
    );
    let signed = admission.sign_acknowledgment(1, &delegate())?;
    assert_eq!(admission.sign_acknowledgment(1, &delegate())?, signed);
    let (acknowledgment, envelope) = decode_acknowledgment(&signed, public(&delegate()))?;
    assert_eq!(acknowledgment, admission.acknowledgment(1));
    assert_eq!(acknowledgment.encode().len(), ACKNOWLEDGMENT_BYTES);
    assert_eq!(
        Acknowledgment::decode(&acknowledgment.encode())?,
        acknowledgment
    );
    assert_eq!(
        (
            envelope.operation,
            envelope.context.actor,
            envelope.context.request
        ),
        (
            ServiceOperation::Acknowledgment,
            identities()?.1,
            submit.context.request
        )
    );
    assert_eq!(
        acknowledgment_digest(&signed)?,
        codec::domain_hash(ACKNOWLEDGMENT_DOMAIN, &signed)?
    );
    assert_eq!(
        admission.sign_acknowledgment(0, &delegate()),
        Err(ServiceError::NonCanonical)
    );
    assert_eq!(
        admission.sign_acknowledgment(1, &customer()),
        Err(ServiceError::BadSignature)
    );
    assert_eq!(
        decode_acknowledgment(&signed, public(&customer())),
        Err(ServiceError::BadSignature)
    );
    let by_worker = decode_service(&submit.signed(&delegate())?)?;
    assert_eq!(
        admit(
            &by_worker,
            &submit.request,
            &fixture.authority,
            &customer_at(HEIGHT)?,
            &fixture.metadata,
            &submit.task
        ),
        Err(ServiceError::BadSignature)
    );
    let frozen_customer = IdentityEvidence {
        frozen: true,
        ..customer_at(HEIGHT)?
    };
    let refused = submit.admit(&fixture.authority, &frozen_customer, &fixture.metadata)?;
    assert_eq!(refused.map(|_| ()), Err(ServiceError::IdentityFrozen));
    let stale_customer = submit.admit(
        &fixture.authority,
        &customer_at(HEIGHT - 1)?,
        &fixture.metadata,
    )?;
    assert_eq!(
        stale_customer.map(|_| ()),
        Err(ServiceError::StaleAuthority)
    );
    Ok(())
}

#[test]
fn a07_admission_refuses_request_rebinding() -> Checked {
    let fixture = admitting()?;
    let base = &fixture.submit;
    let stranger = WorkerId::new([0x99; 32])?;
    let other_input = Digest32::new([0x1D; 32])?;
    let other_capability = Digest32::new([0xCA; 32])?;
    let other_policy = PolicyDigest::new([0x50; 32])?;
    let cases = [
        (
            base.with(|s| s.request.receiver = stranger),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.request.method = 2),
            ServiceError::BadSignature,
        ),
        (
            base.with(|s| s.request.route = 3),
            ServiceError::BadSignature,
        ),
        (
            base.with(|s| s.request.input_commitment = other_input),
            ServiceError::BadSignature,
        ),
        (
            base.with(|s| s.request.metadata_revision = 2),
            ServiceError::WrongRevision,
        ),
        (
            base.with(|s| s.request.key_version = 2),
            ServiceError::WrongGeneration,
        ),
        (
            base.with(|s| s.request.capability = other_capability),
            ServiceError::CapabilityMismatch,
        ),
        (
            base.with(|s| s.request.workload_policy = other_policy),
            ServiceError::CapabilityMismatch,
        ),
        (
            base.with(|s| s.request.deadline_ms = 30_001),
            ServiceError::DeadlineInvalid,
        ),
        (
            base.with(|s| s.request.payload_bytes = 65_537),
            ServiceError::InputTooLarge,
        ),
        (
            base.with(|s| s.request.max_output_bytes = 65_537),
            ServiceError::OutputTooLarge,
        ),
        (
            base.with(|s| s.request.max_units = 4_097),
            ServiceError::OutputTooLarge,
        ),
        (
            base.with(|s| s.request.task_expiry = TASK_EXPIRY + 1),
            ServiceError::DeadlineInvalid,
        ),
    ];
    for (case, refusal) in &cases {
        assert_eq!(fixture.admit(case)?, Err(*refusal));
    }
    assert_eq!(
        fixture.admit(&base.with(|s| s.request.deadline_ms = 30_000))?,
        Ok(())
    );
    let beyond = base.with(|s| {
        s.request.task_expiry = WORK_CLOSE + 1;
        s.task.deadline = WORK_CLOSE + 1;
    });
    assert_eq!(fixture.admit(&beyond)?, Err(ServiceError::DeadlineInvalid));
    let other_task = TaskId::new([0x7B; 32])?;
    let unknown = base.with(|s| s.task.task = other_task);
    assert_eq!(fixture.admit(&unknown)?, Err(ServiceError::BadSignature));
    let acknowledgement = Some(Digest32::new([0xAC; 32])?);
    let accepted = base.with(|s| {
        s.task.status = TaskStatus::Accepted;
        s.task.acknowledgement = acknowledgement;
    });
    assert_eq!(
        fixture.admit(&accepted)?,
        Err(ServiceError::IdempotencyConflict)
    );
    let cancelled = base.with(|s| s.task.status = TaskStatus::Cancelled);
    assert_eq!(
        fixture.admit(&cancelled)?,
        Err(ServiceError::AdmissionNotEffective)
    );
    Ok(())
}

#[test]
fn a07_admission_refuses_context_rebinding() -> Checked {
    let fixture = admitting()?;
    let base = &fixture.submit;
    let other_chain = ChainDomain::new([0x55; 32])?;
    let other_program = ProgramId::new([0x56; 32])?;
    let other_market = MarketId::new([0x57; 32])?;
    let cases = [
        (
            base.with(|s| s.context.chain = other_chain),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.context.program = other_program),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.context.market = other_market),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.context.config = 2),
            ServiceError::StaleAuthority,
        ),
        (
            base.with(|s| s.context.epoch = 2),
            ServiceError::StaleAuthority,
        ),
        (
            base.with(|s| s.context.roster = Presence::Absent),
            ServiceError::StaleAuthority,
        ),
        (
            base.with(|s| s.context.expiry = HEIGHT),
            ServiceError::DeadlineInvalid,
        ),
    ];
    for (case, refusal) in &cases {
        assert_eq!(fixture.admit(case)?, Err(*refusal));
    }
    let stranger = principal(0x33)?;
    let impersonated = base.with(|s| s.context.actor = stranger);
    assert_eq!(
        fixture.admit(&impersonated)?,
        Err(ServiceError::BadSignature)
    );
    Ok(())
}

#[test]
fn a12_query_and_cancel_authorization() -> Checked {
    let fixture = admitting()?;
    let admission =
        fixture
            .submit
            .admit(&fixture.authority, &customer_at(HEIGHT)?, &fixture.metadata)??;
    let customer_id = principal(9)?;
    let evaluator = principal(3)?;
    let stranger = principal(0x33)?;
    let reference = JobReference {
        receiver: admission.worker,
        task: admission.task,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        method: Route::Query.code(),
        route: Route::Query.code(),
        original: admission.context.request,
    };
    let granted = [evaluator];
    for (actor, route, outcome) in [
        (customer_id, Route::Query, Ok(())),
        (evaluator, Route::Query, Ok(())),
        (customer_id, Route::Cancel, Ok(())),
        (evaluator, Route::Cancel, Err(ServiceError::AccessDenied)),
        (stranger, Route::Query, Err(ServiceError::AccessDenied)),
        (stranger, Route::Cancel, Err(ServiceError::AccessDenied)),
        (customer_id, Route::Submit, Err(ServiceError::AccessDenied)),
    ] {
        assert_eq!(
            admission.authorize(actor, route, &reference, &granted),
            outcome
        );
    }
    assert_eq!(
        admission.authorize(evaluator, Route::Query, &reference, &[]),
        Err(ServiceError::AccessDenied)
    );
    let body = ServiceError::AccessDenied.body(Presence::Present(admission.context.request));
    assert_eq!(&body[..2], &27u16.to_be_bytes());
    assert_eq!(
        ServiceError::decode_body(&body),
        Ok((
            ServiceError::AccessDenied,
            Presence::Present(admission.context.request)
        ))
    );
    let other = JobReference {
        original: RequestId::new([0x89; 32])?,
        ..reference
    };
    assert_eq!(
        admission.authorize(customer_id, Route::Query, &other, &[]),
        Err(ServiceError::NotFound)
    );
    for relabelled in [
        JobReference {
            generation: 2,
            ..reference
        },
        JobReference {
            metadata_revision: 2,
            ..reference
        },
        JobReference {
            task: TaskId::new([0x7B; 32])?,
            ..reference
        },
    ] {
        assert_eq!(
            admission.authorize(customer_id, Route::Query, &relabelled, &[]),
            Err(ServiceError::IdempotencyConflict)
        );
    }
    Ok(())
}

#[test]
fn a15_later_metadata_cannot_relabel_admitted_work() -> Checked {
    let fixture = admitting()?;
    let admission =
        fixture
            .submit
            .admit(&fixture.authority, &customer_at(HEIGHT)?, &fixture.metadata)??;
    let acknowledged = admission.sign_acknowledgment(1, &delegate())?;
    let replacement = manifest(
        2,
        vec![capability(0x41)],
        vec![endpoint(DEFAULT_URI, [0xE1; 32])],
    )?;
    let replacement_signed = sign(&replacement, 1)?;
    let (_, replacement_digest) = Manifest::decode(&replacement.encode()?)?;
    let mut world = fixture.market.world.clone();
    let worker = admission.worker;
    world.edit(|parts, _| {
        let current = parts.workers.get(worker).ok_or(NON_CANONICAL)?;
        parts.workers.replace(&WorkerCurrent {
            metadata: replacement_digest,
            metadata_revision: 2,
            last_metadata_height: HEIGHT - 1,
            ..current
        })
    })?;
    let authority = view(&world.bytes, &fixture.market.roster, HEIGHT)?.authority()?;
    assert_eq!(authority.admission_gate(), Ok(()));
    let metadata = verified(&authority, &replacement_signed)?;
    assert_eq!(metadata.manifest().capabilities, vec![capability(0x41)]);
    let stale = verify_signed_metadata(&fixture.market.signed, &authority.metadata_context())?;
    assert_eq!(
        authority.bind_metadata(stale),
        Err(ServiceError::MetadataIntegrityFailure)
    );
    let (kept, _) = decode_acknowledgment(&acknowledged, public(&delegate()))?;
    assert_eq!(
        (kept.metadata, kept.model, kept.admitted_height),
        (
            fixture.metadata.digest(),
            Digest32::new([0x31; 32])?,
            HEIGHT
        )
    );
    let customer = customer_at(HEIGHT)?;
    let old = fixture.submit.admit(&authority, &customer, &metadata)?;
    assert_eq!(old.map(|_| ()), Err(ServiceError::WrongRevision));
    let relabelled =
        submit(&authority, &capability(0x31))?.admit(&authority, &customer, &metadata)?;
    assert_eq!(
        relabelled.map(|_| ()),
        Err(ServiceError::CapabilityMismatch)
    );
    let current =
        submit(&authority, &capability(0x41))?.admit(&authority, &customer, &metadata)??;
    assert_eq!(
        (current.metadata, current.model, current.metadata_revision),
        (replacement_digest, Digest32::new([0x41; 32])?, 2)
    );
    assert_eq!(
        admission.authorize(
            principal(9)?,
            Route::Query,
            &JobReference {
                receiver: worker,
                task: admission.task,
                generation: 1,
                key_version: 1,
                metadata_revision: 2,
                method: Route::Query.code(),
                route: Route::Query.code(),
                original: admission.context.request,
            },
            &[]
        ),
        Err(ServiceError::IdempotencyConflict)
    );
    Ok(())
}

#[test]
fn readiness_observations_expire() {
    let observation = ReadinessObservation {
        state: Readiness::VerifiedTransport,
        observed_ms: 1_000,
    };
    assert_eq!(observation.current(1_000), Readiness::VerifiedTransport);
    assert_eq!(
        observation.current(1_000 + READINESS_TTL_MS - 1),
        Readiness::VerifiedTransport
    );
    assert_eq!(
        observation.current(1_000 + READINESS_TTL_MS),
        Readiness::NotReady
    );
    assert_eq!(observation.current(999), Readiness::NotReady);
}

#[test]
fn network_profiles_refuse_loopback_and_cloud_metadata() {
    let loopback = IpAddr::V4(Ipv4Addr::LOCALHOST);
    let cloud = Ipv4Addr::new(169, 254, 169, 254);
    let public = NetworkProfile::Public;
    for ip in [
        loopback,
        IpAddr::V6(Ipv6Addr::LOCALHOST),
        IpAddr::V4(cloud),
        IpAddr::V6(cloud.to_ipv6_mapped()),
        IpAddr::V4(Ipv4Addr::UNSPECIFIED),
        IpAddr::V4(Ipv4Addr::BROADCAST),
    ] {
        assert!(!public.admits(ip), "{ip}");
    }
    let private = NetworkProfile::Private {
        allowlist: vec![loopback, IpAddr::V4(cloud)],
    };
    assert!(private.admits(loopback));
    assert!(private.admits(IpAddr::V6(Ipv4Addr::LOCALHOST.to_ipv6_mapped())));
    assert!(!private.admits(IpAddr::V6(Ipv6Addr::LOCALHOST)));
    assert!(!private.admits(IpAddr::V4(cloud)));
    assert!(!private.admits(IpAddr::V6(cloud.to_ipv6_mapped())));
}

#[test]
fn a06_pinned_tls_refuses_before_any_request_byte() -> Checked {
    let pki = pki()?;
    let pin = spki_sha256(&pki.leaf)?;
    let expected: [u8; 32] = Sha256::digest(&pki.spki).into();
    assert_eq!(pin, expected);
    let mut flipped = pin;
    flipped[0] ^= 1;
    let body = [0x42u8; 64];
    let cases = [
        (pki.leaf.clone(), flipped, TransportError::PinMismatch),
        (pki.expired.clone(), pin, TransportError::Certificate),
        (pki.leaf.clone(), pin, TransportError::Redirect),
    ];
    for (certificate, pinned, refusal) in cases {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?;
        let port = listener.local_addr()?.port();
        let server = serve_once(listener, server_config(certificate, &pki.key_der)?);
        let result =
            local_client(&pki).post(&endpoint_at(port, pinned), SUBMIT_PATH, &body, loopback());
        assert_eq!(result, Err(refusal));
        let received = joined(server)?;
        if refusal == TransportError::Redirect {
            assert!(received.starts_with(b"POST /paxai/v1/jobs HTTP/1.1\r\n"));
            assert!(received.ends_with(&body));
        } else {
            assert!(received.is_empty());
        }
    }
    Ok(())
}

#[test]
fn a06_resolution_outside_the_profile_never_connects() -> Checked {
    let pki = pki()?;
    let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?;
    listener.set_nonblocking(true)?;
    let port = listener.local_addr()?.port();
    let endpoint = endpoint_at(port, spki_sha256(&pki.leaf)?);
    let loopback_v4 = IpAddr::V4(Ipv4Addr::LOCALHOST);
    let cloud = IpAddr::V4(Ipv4Addr::new(169, 254, 169, 254));
    let private = local_client(&pki);
    let public = EndpointClient {
        profile: NetworkProfile::Public,
        ..local_client(&pki)
    };
    for (client, ips) in [
        (&public, vec![loopback_v4]),
        (&public, vec![cloud]),
        (&private, vec![loopback_v4, IpAddr::V6(Ipv6Addr::LOCALHOST)]),
        (&private, vec![cloud]),
    ] {
        assert_eq!(
            client.post(&endpoint, SUBMIT_PATH, b"job", answers(ips)),
            Err(TransportError::UnsafeAddress)
        );
    }
    assert_eq!(
        private.post(&endpoint, "/other", b"job", loopback()),
        Err(TransportError::UnsafeEndpoint)
    );
    let plain = Endpoint {
        uri: format!("http://localhost:{port}/paxai/v1"),
        ..endpoint.clone()
    };
    assert_eq!(
        private.post(&plain, SUBMIT_PATH, b"job", loopback()),
        Err(TransportError::UnsafeEndpoint)
    );
    assert_eq!(
        private.post(&endpoint, SUBMIT_PATH, b"job", answers(Vec::new())),
        Err(TransportError::Unavailable)
    );
    assert_eq!(
        listener.accept().map(|_| ()).map_err(|error| error.kind()),
        Err(io::ErrorKind::WouldBlock)
    );
    Ok(())
}

#[test]
fn worker_binary_serves_signed_status_and_refuses_jobs() -> Checked {
    let pki = pki()?;
    let port = free_port()?;
    let market = opened(vec![endpoint_at(port, spki_sha256(&pki.leaf)?)])?;
    let authority = authority_at(&market, HEIGHT)?;
    let found = discover(&authority, |_| Ok(market.signed.clone()));
    assert_eq!(found.readiness, Readiness::Advertised);
    let endpoint = found
        .metadata?
        .manifest()
        .endpoints
        .first()
        .cloned()
        .ok_or(Failure::Unexpected("advertised endpoint"))?;
    let _worker = start(&worker_config("serves", &pki, &market.signed, port)?)?;
    let client = local_client(&pki);
    let challenge = StatusChallenge {
        epoch: 1,
        config: authority.config(),
        roster: authority.roster(),
        expiry: TASK_EXPIRY,
        challenge: RequestId::new([0x5C; 32])?,
    };
    assert_eq!(
        client.verify_status(&authority, &endpoint, &challenge, 7_000, loopback())?,
        ReadinessObservation {
            state: Readiness::VerifiedTransport,
            observed_ms: 7_000,
        }
    );
    let send = |path: &str, body: &[u8]| client.post(&endpoint, path, body, loopback());
    let refused = |error| Err(TransportError::Service(error));
    let base = submit(&authority, &capability(0x31))?;
    let valid = base.signed(&customer())?;
    assert_eq!(
        send(SUBMIT_PATH, &valid),
        refused(ServiceError::StaleAuthority)
    );
    assert_eq!(
        send(QUERY_PATH, &valid),
        refused(ServiceError::BadSignature)
    );
    let mut tampered = valid.clone();
    tampered[SERVICE_PREFIX_BYTES] ^= 1;
    assert_eq!(
        send(SUBMIT_PATH, &tampered),
        refused(ServiceError::BadSignature)
    );
    let stranger = WorkerId::new([0x99; 32])?;
    let other_chain = ChainDomain::new([0x55; 32])?;
    for (changed, refusal) in [
        (
            base.with(|s| s.request.receiver = stranger),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.context.chain = other_chain),
            ServiceError::WrongDomain,
        ),
        (
            base.with(|s| s.request.metadata_revision = 2),
            ServiceError::WrongRevision,
        ),
    ] {
        assert_eq!(
            send(SUBMIT_PATH, &changed.signed(&customer())?),
            refused(refusal)
        );
    }
    assert_eq!(
        send("/paxai/v1/unknown", &valid),
        refused(ServiceError::NotFound)
    );
    Ok(())
}

#[test]
fn worker_binary_refuses_an_unpinned_leaf() -> Checked {
    let pki = pki()?;
    let market = opened(vec![endpoint(DEFAULT_URI, [0xE1; 32])])?;
    let config = worker_config("unpinned", &pki, &market.signed, free_port()?)?;
    let output = worker_command(&config).output()?;
    assert_eq!(output.status.code(), Some(1));
    assert_eq!(String::from_utf8_lossy(&output.stderr), UNPINNED);
    assert!(output.stdout.is_empty());
    Ok(())
}
