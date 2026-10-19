//! AI.F01-T04-SERVICE worker side of the finalized task handoff. A really opened market routes
//! the requester's `ADMIT_TASK` and the worker owner's `ACCEPT_TASK`/`COMMIT_TASK_RESULT`
//! through `dispatch::route`; the worker reads every task record only from a finalized capture
//! of those committed bytes, admits through the real `auth::admit` and binds its signed
//! acknowledgment digest as the one chain acceptance.
use ed25519_dalek::{Signer, SigningKey};
use layerx_client::head::Head;
use layerx_paxai_worker::{
    auth::{
        acknowledgment_digest, admit, decode_acknowledgment, decode_service, encode_service,
        sign_metadata, verify_signed_metadata, Admission, MetadataContext, MetadataPublication,
        Route, ServiceContext, ServiceError, ServiceOperation, ServiceRequest,
    },
    discovery::{
        discover, AuthorityEvidence, FinalizedAuthority, IdentityEvidence, Readiness,
        VerifiedMetadata, MAX_FINALITY_LAG,
    },
    metadata::{Capability, Endpoint, Manifest},
};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission as Membership, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    codec::{
        self, derive_evaluator, derive_market, derive_task, derive_worker, encode_envelope,
        encode_roster, Envelope, ResultStatus, Roster,
    },
    dispatch::{self, Buffers, Operation, Routed},
    epoch::{self, Frozen, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, F01_TASK_ALREADY_ACCEPTED, F01_TASK_EXPIRED,
        F01_TASK_NOT_FOUND, F02_METADATA_EXPIRED, NON_CANONICAL,
    },
    evaluators::{
        authority::{split_identity_section, EvaluatorRecord, EvaluatorRegion, LastRequest},
        model::{EvaluatorGrant, GrantTerms},
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{
        read_state_chunk, CaptureFacts, FinalityEvidence, QueryError, ReadProof, StateCapture,
    },
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
    tasks::{TaskBinding, TaskSet, TaskStatus},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorRosterEntry, MarketId,
        Presence, PrincipalId, ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest,
        RosterDigest, RubricDigest, StateDigest, TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES,
};
use std::io;

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
/// The customer principal that admits every task on chain and signs every submit.
const CUSTOMER: u8 = 9;
const VALID_FROM: u64 = 1140;
/// Metadata (license) expiry of the enrolled worker.
const EXPIRY: u64 = 1180;
const ADMIT_AT: u64 = 1145;
const NONCE: [u8; 32] = [0x7A; 32];
const INPUT: [u8; 32] = [0x1C; 32];
/// F02-R009 result manifests are F02-T03 scope; the committed result is this fixed digest.
const RESULT: [u8; 32] = [0xE5; 32];
const ROLE_EXPIRY: u64 = 5000;
const CHUNK_RESPONSE_MAX: usize = 8_244;

enum Failure {
    Application(ApplicationError),
    Service(ServiceError),
    Query(QueryError),
}
impl std::fmt::Debug for Failure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Service(error) => write!(f, "service refusal {error:?}"),
            Self::Query(error) => write!(f, "query refusal {error:?}"),
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
impl From<QueryError> for Failure {
    fn from(error: QueryError) -> Self {
        Self::Query(error)
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
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// Market, worker owner and worker of the handoff.
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

/// One native request envelope.
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
    }
}
impl Call {
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
            expiry: self.expiry,
            request: RequestId::new(self.request)?,
            payload: &self.payload,
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
fn manifest() -> Checked<Manifest> {
    let (market, owner, worker) = identities()?;
    Ok(Manifest {
        market,
        worker,
        owner,
        generation: 1,
        key_version: 1,
        revision: 1,
        valid_from: VALID_FROM,
        expiry: EXPIRY,
        deployment: Digest32::new([0xDE; 32])?,
        capabilities: vec![capability(0x31)],
        endpoints: vec![Endpoint {
            id: 1,
            uri: "https://localhost/paxai/v1".to_owned(),
            spki_sha256: [0xE1; 32],
        }],
        privacy_policy: Digest32::new([0x9A; 32])?,
        service_terms: Digest32::new([0x7E; 32])?,
    })
}
/// The delegate-signed revision-1 metadata publication.
fn signed_metadata(manifest: &Manifest) -> Checked<Vec<u8>> {
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
            expected_revision: 0,
            epoch: 0,
            config: 1,
            roster: Presence::Absent,
            sequence: 1,
            expiry: EXPIRY,
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

/// The customer's signed submit of the task it admitted on chain.
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
    /// The real worker admission against `authority` and the finalized task record.
    fn admit(
        &self,
        authority: &FinalizedAuthority,
        metadata: &VerifiedMetadata,
        task: &TaskBinding,
    ) -> Checked<Result<Admission, ServiceError>> {
        let envelope = decode_service(&self.signed()?)?;
        Ok(admit(
            &envelope,
            &self.request,
            authority,
            &customer_at(authority.height())?,
            metadata,
            task,
        ))
    }
}

/// The opened market at origin 1000 with Work window 1128..1192 and its discovery evidence.
struct Market {
    world: World,
    roster: Vec<u8>,
    signed: Vec<u8>,
    frozen: Frozen,
    worker: WorkerId,
}
/// Routed `CREATE` and `SCHEDULE_ACTIVATION`, worker enrollment bound to the signed manifest
/// digest, three accepted evaluators, `FUND`, then routed `ADVANCE_ACTIVATION` and `OPEN_EPOCH` 1.
fn opened() -> Checked<Market> {
    let (market, owner, worker) = identities()?;
    let manifest = manifest()?;
    let signed = signed_metadata(&manifest)?;
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
        worker,
    })
}

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
    fn task_id(&self) -> Checked<TaskId> {
        Ok(derive_task(
            identities()?.0,
            self.frozen.epoch,
            principal(CUSTOMER)?,
            NONCE,
        )?)
    }
    /// The customer's routed `ADMIT_TASK` of `NONCE` with `deadline`, naming the frozen worker
    /// and the digest of its signed manifest.
    fn admit_call(&self, deadline: u64) -> Checked<Call> {
        let (_, digest) = Manifest::decode(&manifest()?.encode()?)?;
        let mut payload = self.frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(self.frozen.policy.as_bytes());
        payload.extend_from_slice(self.frozen.roster.as_bytes());
        payload.extend_from_slice(principal(CUSTOMER)?.as_bytes());
        payload.extend_from_slice(self.worker.as_bytes());
        payload.extend_from_slice(digest.as_bytes());
        payload.extend_from_slice(&NONCE);
        payload.extend_from_slice(&INPUT);
        payload.extend_from_slice(&deadline.to_be_bytes());
        Ok(self.bound(dispatch::ADMIT_TASK, principal(CUSTOMER)?, 0xB0, payload))
    }
    fn admit_task(&mut self, deadline: u64, at: u64) -> Checked<TaskId> {
        let admit = self.admit_call(deadline)?;
        let frame = self.world.route(&admit, at)?;
        assert_eq!((frame.status, frame.error), (ResultStatus::Ok, None));
        self.task_id()
    }
    /// The worker owner's native step under role `sequence`, expecting the current revision.
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
    /// Routes `call`, which must be refused without any new state.
    fn refused(&self, call: &Call, at: u64) -> Checked<ApplicationError> {
        let (frame, next) = route(call, Some(&self.world.bytes), at)?;
        assert!(next.is_none());
        assert_eq!(
            (frame.status, frame.revision, frame.payload.len()),
            (ResultStatus::Error, self.world.revision()?, 0)
        );
        Ok(frame.error.ok_or(NON_CANONICAL)?)
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
    fn verified(&self, authority: &FinalizedAuthority) -> Checked<VerifiedMetadata> {
        Ok(authority.bind_metadata(verify_signed_metadata(
            &self.signed,
            &authority.metadata_context(),
        )?)?)
    }
    /// The customer's signed submit of the `NONCE` task naming `capability`.
    fn submit(
        &self,
        authority: &FinalizedAuthority,
        deadline: u64,
        capability: &Capability,
    ) -> Checked<Submit> {
        let binding = authority.worker_binding();
        let request = ServiceRequest {
            receiver: binding.worker,
            task: self.task_id()?,
            generation: 1,
            key_version: 1,
            metadata_revision: binding.metadata_revision,
            method: Route::Submit.code(),
            route: Route::Submit.code(),
            capability: capability.digest()?,
            workload_policy: authority.policy(),
            model: Digest32::new(capability.model)?,
            input_commitment: Digest32::new(INPUT)?,
            payload_bytes: 1_024,
            max_output_bytes: 2_048,
            max_units: 256,
            deadline_ms: 0,
            task_expiry: deadline,
            evaluation_access: Digest32::new([0xEA; 32])?,
            result_key: Some([0x4B; 32]),
        };
        let context = ServiceContext {
            chain: binding.chain,
            program: binding.program,
            market: binding.market,
            actor: principal(CUSTOMER)?,
            epoch: 1,
            config: authority.config(),
            roster: authority.roster(),
            sequence: 1,
            expiry: deadline,
            request: RequestId::new([0x88; 32])?,
        };
        Ok(Submit { context, request })
    }
}

/// The worker's side of one handoff: the chain record it admitted against and the persisted
/// signed acknowledgment of that admission.
struct Handoff {
    task: TaskId,
    admitted: TaskBinding,
    submit: Submit,
    admission: Admission,
    persisted: Vec<u8>,
}
/// The customer routes `ADMIT_TASK` at `ADMIT_AT`; the worker admits the submit against a
/// finalized capture at 1150 and signs its first acknowledgment.
fn handoff(m: &mut Market, deadline: u64) -> Checked<Handoff> {
    let task = m.admit_task(deadline, ADMIT_AT)?;
    let (authority, captured) = m.finalized(1150)?;
    let admitted = finalized_task(&captured, task)?;
    assert_eq!(
        (
            admitted.requester,
            admitted.worker,
            admitted.input,
            admitted.deadline,
            admitted.status,
            admitted.acknowledgement,
            admitted.result
        ),
        (
            principal(CUSTOMER)?,
            m.worker,
            Digest32::new(INPUT)?,
            deadline,
            TaskStatus::Admitted,
            None,
            None
        )
    );
    let metadata = m.verified(&authority)?;
    let submit = m.submit(&authority, deadline, &capability(0x31))?;
    let admission = submit.admit(&authority, &metadata, &admitted)??;
    assert_eq!(
        (
            admission.task,
            admission.worker,
            admission.task_deadline,
            admission.admitted_height,
            admission.request_commitment
        ),
        (
            task,
            m.worker,
            deadline,
            1150,
            decode_service(&submit.signed()?)?.digest
        )
    );
    let persisted = admission.sign_acknowledgment(1, &delegate())?;
    Ok(Handoff {
        task,
        admitted,
        submit,
        admission,
        persisted,
    })
}

#[test]
fn a13_finalized_acknowledgment_becomes_the_one_chain_acceptance() -> Checked {
    let mut m = opened()?;
    let h = handoff(&mut m, 1172)?;
    let acknowledgment = acknowledgment_digest(&h.persisted)?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, h.task, acknowledgment.bytes())?;
    let accepted = m.applied(&accept, 1151)?;
    let (authority, captured) = m.finalized(1152)?;
    let acknowledged = finalized_task(&captured, h.task)?;
    assert_eq!(
        acknowledged,
        TaskBinding {
            status: TaskStatus::Accepted,
            acknowledgement: Some(acknowledgment),
            ..h.admitted
        }
    );
    let metadata = m.verified(&authority)?;
    assert_eq!(
        h.submit
            .admit(&authority, &metadata, &acknowledged)?
            .map(|_| ()),
        Err(ServiceError::IdempotencyConflict)
    );
    let (retry, next) = route(&accept, Some(&m.world.bytes), 1152)?;
    assert!(next.is_none());
    assert_eq!(
        (retry.status, retry.revision, retry.payload),
        (
            ResultStatus::AlreadyApplied,
            accepted.revision,
            accepted.digest.to_vec()
        )
    );
    let (recovered, envelope) = decode_acknowledgment(&h.persisted, public(&delegate()))?;
    assert_eq!(recovered, h.admission.acknowledgment(1));
    assert_eq!(
        (
            recovered.task,
            envelope.operation,
            envelope.context.request,
            Some(acknowledgment_digest(&h.persisted)?)
        ),
        (
            h.task,
            ServiceOperation::Acknowledgment,
            h.submit.context.request,
            acknowledged.acknowledgement
        )
    );
    Ok(())
}

#[test]
fn a13_second_acknowledgment_is_refused_and_one_result_commits() -> Checked {
    let mut m = opened()?;
    let h = handoff(&mut m, 1172)?;
    let acknowledgment = acknowledgment_digest(&h.persisted)?;
    let accept = m.step(dispatch::ACCEPT_TASK, 1, h.task, acknowledgment.bytes())?;
    m.applied(&accept, 1151)?;
    let second = h.admission.sign_acknowledgment(2, &delegate())?;
    let duplicate = m.step(
        dispatch::ACCEPT_TASK,
        2,
        h.task,
        acknowledgment_digest(&second)?.bytes(),
    )?;
    assert_eq!(m.refused(&duplicate, 1152)?, F01_TASK_ALREADY_ACCEPTED);
    let commit = m.step(dispatch::COMMIT_TASK_RESULT, 2, h.task, RESULT)?;
    m.applied(&commit, 1171)?;
    let (authority, captured) = m.finalized(1171)?;
    let resulted = finalized_task(&captured, h.task)?;
    assert_eq!(
        resulted,
        TaskBinding {
            status: TaskStatus::ResultCommitted,
            acknowledgement: Some(acknowledgment),
            result: Some(Digest32::new(RESULT)?),
            ..h.admitted
        }
    );
    let metadata = m.verified(&authority)?;
    assert_eq!(
        h.submit
            .admit(&authority, &metadata, &resulted)?
            .map(|_| ()),
        Err(ServiceError::AdmissionNotEffective)
    );
    Ok(())
}

#[test]
fn a15_acknowledgment_after_the_deadline_never_becomes_a_chain_acceptance() -> Checked {
    let mut m = opened()?;
    let h = handoff(&mut m, 1160)?;
    let accept = m.step(
        dispatch::ACCEPT_TASK,
        1,
        h.task,
        acknowledgment_digest(&h.persisted)?.bytes(),
    )?;
    assert_eq!(m.refused(&accept, 1160)?, F01_TASK_EXPIRED);
    let (authority, captured) = m.finalized(1160)?;
    assert_eq!(finalized_task(&captured, h.task)?, h.admitted);
    let metadata = m.verified(&authority)?;
    assert_eq!(
        h.submit
            .admit(&authority, &metadata, &h.admitted)?
            .map(|_| ()),
        Err(ServiceError::DeadlineInvalid)
    );
    let late = m.step(dispatch::COMMIT_TASK_RESULT, 1, h.task, RESULT)?;
    assert_eq!(m.refused(&late, 1161)?, F01_TASK_EXPIRED);
    assert_eq!(finalized_task(&m.finalized(1161)?.1, h.task)?, h.admitted);
    Ok(())
}

#[test]
fn a18_unsupported_model_expired_license_and_missing_metadata_refuse_before_acknowledgment(
) -> Checked {
    let mut m = opened()?;
    let task = m.admit_task(1182, 1150)?;
    let (authority, captured) = m.finalized(1151)?;
    let admitted = finalized_task(&captured, task)?;
    let metadata = m.verified(&authority)?;
    let unsupported = m.submit(&authority, 1182, &capability(0x41))?;
    assert_eq!(
        unsupported
            .admit(&authority, &metadata, &admitted)?
            .map(|_| ()),
        Err(ServiceError::CapabilityMismatch)
    );
    let missing = discover(&authority, |_| Err(io::ErrorKind::TimedOut.into()));
    assert_eq!(
        (missing.eligibility, missing.metadata, missing.readiness),
        (
            Ok(()),
            Err(ServiceError::MetadataUnavailable),
            Readiness::NotReady
        )
    );
    let (expired, captured) = m.finalized(EXPIRY)?;
    assert_eq!(finalized_task(&captured, task)?, admitted);
    assert_eq!(expired.admission_gate(), Err(ServiceError::MetadataExpired));
    let metadata = m.verified(&expired)?;
    let supported = m.submit(&expired, 1182, &capability(0x31))?;
    assert_eq!(
        supported.admit(&expired, &metadata, &admitted)?.map(|_| ()),
        Err(ServiceError::MetadataExpired)
    );
    let late = Call {
        payload: {
            let mut payload = m.admit_call(1190)?.payload;
            let at = payload.len() - 72;
            payload[at..at + 32].copy_from_slice(&[0x7B; 32]);
            payload
        },
        request: [0xB1; 32],
        ..m.admit_call(1190)?
    };
    assert_eq!(m.refused(&late, EXPIRY)?, F02_METADATA_EXPIRED);
    assert_eq!(finalized_task(&m.finalized(EXPIRY)?.1, task)?, admitted);
    assert_eq!(
        (admitted.status, admitted.acknowledgement, admitted.result),
        (TaskStatus::Admitted, None, None)
    );
    Ok(())
}
