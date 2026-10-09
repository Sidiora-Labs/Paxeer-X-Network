use std::collections::BTreeMap;
use std::fmt;
use std::fs;
use std::future::Future;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::pin::pin;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::thread;
use std::time::Duration;

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_client::ai_market::{
    AiClientError, ApprovalTerms, CheckpointFinality, DomainStatus, EpochHistoryEntry, EpochStatus,
    FinalizedSnapshot, Freshness, HistoricalSource, NativeTerms, ObservedSnapshot, OperationEffect,
    OperationJournal, OperationRecord, OperationRequest, OperationState, Review, ReviewField,
    AUTHORITY_FRESHNESS_HEIGHTS, ENTRYPOINT, GUEST_ABI, NATIVE_CALL_PROTOCOL_VERSION,
    PROGRAM_CALL_ORDINAL, SETTLEMENT_RANK,
};
use layerx_client::lni::framing::{read_frame, write_frame};
use layerx_client::lni::schema::{
    decode_envelope as decode_frame, encode_envelope as encode_frame, Envelope as Frame,
    Version as Interface,
};
use layerx_client::lni::transport::{ConnectionGate, Limits, Uds};
use layerx_client::receipt::{AuthenticatedLookupContext, ReceiptWaitMode};
use layerx_crypto::local::LocalSigner;
use layerx_crypto::signer::Signer;
use layerx_crypto::{ed25519, SignatureMessage};
use layerx_programs_ai_market::codec::{
    self, decode_envelope, decode_roster, derive_market, derive_worker, encode_envelope, Envelope,
};
use layerx_programs_ai_market::dispatch::{self, CallBoundary, Operation, SequencePolicy};
use layerx_programs_ai_market::errors::{
    ApplicationError, F06_CONTRIBUTION_CONSENT_REQUIRED, F06_INVALID_AMOUNT, F06_NOTHING_TO_CLAIM,
    F06_UNKNOWN_WORKER_ENTITLEMENT, F06_WRONG_CLAIM_RECIPIENT, NON_CANONICAL, NOT_FOUND,
    UNAUTHORIZED, WRONG_ROSTER,
};
use layerx_programs_ai_market::policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES};
use layerx_programs_ai_market::queries::{
    read_state_chunk, Availability, ParticipantKind, QueryError, ReadProof, ScoreStatus,
    CURSOR_LIFETIME_MS, CURSOR_MAX_BYTES, DEFAULT_LIMIT, FINALIZED_RANK, PAGE_MAX_BYTES,
    PAGE_MAX_ROWS,
};
use layerx_programs_ai_market::registry::{derive_rewards_account, market_clock, F01_SECTION_CAP};
use layerx_programs_ai_market::registry_ops::{self, Outcome};
use layerx_programs_ai_market::rewards::{ClaimRequest, FundRequest, FUNDING_POLICY_VERSION};
use layerx_programs_ai_market::state::{self, Section, SharedState};
use layerx_programs_ai_market::types::{
    AccountId, AssetId, Authentication, ChainDomain, Digest32, EpochWindows, MarketId,
    MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32, RequestId, RubricDigest,
    Version as ConfigVersion, WorkerId, WorkerRosterEntry,
};
use layerx_programs_ai_market::workers::{
    WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES,
};
use layerx_programs_ai_market::{
    MAX_CHUNK_BYTES, MAX_ENVELOPE_BYTES, MAX_EVALUATORS, MAX_EVENT_BYTES, MAX_PAYLOAD_BYTES,
    MAX_STATE_BYTES, MAX_TASKS, MAX_WORKERS,
};
use layerx_proof::checkpoint::{Attestation, Certificate, Checkpoint};
use layerx_proof::settlement::declared_domain;
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_types::program_call::{NativeProgramCall, Resources};
use layerx_types::vectors::checkpoint::{load_checkpoint_vectors, CheckpointVector};
use layerx_wire::activity::{decode_signed, encode_unsigned};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{activity_id, receipt_digest, Domain};
use layerx_wire::limits::PROTOCOL_VERSION;
use layerx_wire::WireError;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const WORKER_OWNER: [u8; 32] = [20; 32];
const PAYEE: [u8; 32] = [40; 32];
const OWNER_SEED: [u8; 32] = [0x41; 32];
const WORKER_SEED: [u8; 32] = [0x52; 32];
const CHUNK_RESPONSE_MAX: usize = 8_244;
const ORIGIN: u64 = 1_000;
const EXECUTION_HEIGHT: u64 = 1_010;
const FRESH_AUTHORITY: u64 = EXECUTION_HEIGHT + AUTHORITY_FRESHNESS_HEIGHTS;
const NETWORK: u32 = 42;
const ACCESS: &[u8] = b"LayerX/programs/access-declaration/v1\0\0";
const RESOURCES: Resources = Resources([
    1_000_000, 16_777_216, 1_048_576, 1_048_576, 64, 1_048_576, 4_096,
]);
const SUBMIT_REQUEST: u16 = 3;
const SUBMIT_ACK: u16 = 4;
const LOOKUP_REQUEST: u16 = 5;
const LOOKUP_RESPONSE: u16 = 6;

static NEXT_SCRATCH: AtomicU64 = AtomicU64::new(1);

enum Failure {
    Application(ApplicationError),
    Query(QueryError),
    Client(AiClientError),
    Setup(String),
}

impl fmt::Debug for Failure {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Application(error) => write!(formatter, "application refusal {error:?}"),
            Self::Query(error) => write!(formatter, "query refusal {error:?}"),
            Self::Client(error) => write!(formatter, "client refusal {error:?}"),
            Self::Setup(detail) => write!(formatter, "setup failure {detail}"),
        }
    }
}

impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}

impl From<QueryError> for Failure {
    fn from(error: QueryError) -> Self {
        Self::Query(error)
    }
}

impl From<AiClientError> for Failure {
    fn from(error: AiClientError) -> Self {
        Self::Client(error)
    }
}

type Checked<T = ()> = Result<T, Failure>;

fn setup<E: fmt::Debug>(what: &'static str) -> impl FnOnce(E) -> Failure {
    move |error| Failure::Setup(format!("{what}: {error:?}"))
}

fn application(code: ApplicationError) -> AiClientError {
    AiClientError::Query(QueryError::Application(code))
}

fn registry() -> Checked<ModuleRegistry> {
    let call = ActivityType::new(ModuleId::Programs, PROGRAM_CALL_ORDINAL)
        .map_err(setup("activity type"))?;
    let registration =
        ModuleRegistration::new(ModuleId::Programs, &[call]).map_err(setup("registration"))?;
    ModuleRegistry::new(&[registration]).map_err(setup("registry"))
}

fn chain() -> Checked<ChainDomain> {
    Ok(ChainDomain::new(CHAIN)?)
}

fn program() -> Checked<ProgramId> {
    Ok(ProgramId::new(PROGRAM)?)
}

fn market() -> Checked<MarketId> {
    Ok(derive_market(chain()?, program()?)?)
}

fn principal(bytes: [u8; 32]) -> Checked<PrincipalId> {
    Ok(PrincipalId::new(bytes)?)
}

fn digest(byte: u8) -> Checked<Digest32> {
    Ok(Digest32::new([byte; 32])?)
}

fn public_key(seed: [u8; 32]) -> [u8; 32] {
    LocalSigner::new(seed).public_key()
}

fn policy_with(rubric: u8) -> Checked<TaskPolicyV1> {
    Ok(TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: digest(1)?,
            dataset_artifact: [2; 32],
            benchmark_suite: digest(3)?,
            rubric: RubricDigest::new([rubric; 32])?,
            task_schema: digest(5)?,
            result_schema: digest(6)?,
            service_terms: digest(7)?,
        },
        100,
        1,
    )?)
}

fn encode_shared(shared: &SharedState<'_>) -> Checked<Vec<u8>> {
    let mut out = vec![0; shared.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let length = state::encode_shared_state(shared, &mut out, &mut scratch)?;
    out.truncate(length);
    Ok(out)
}

/// Real F01 CREATE at the market origin through `registry_ops::apply`.
fn created_state() -> Checked<Vec<u8>> {
    let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
    policy_with(4)?.encode(&mut encoded_policy)?;
    let rewards = derive_rewards_account(program()?, AssetId::new(ASSET)?)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&encoded_policy);
    payload.extend_from_slice(&[16; 32]);
    let envelope = Envelope {
        operation: dispatch::CREATE,
        chain: chain()?,
        program: program()?,
        market: market()?,
        actor: principal(OWNER)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        expiry: 1_000_000,
        request: RequestId::new([1; 32])?,
        payload: &payload,
        authentication: Authentication::Native,
    };
    let mut encoded = vec![0; MAX_ENVELOPE_BYTES];
    let length = encode_envelope(&envelope, &mut encoded)?;
    let validated = decode_envelope(&encoded[..length])?;
    let context = registry_ops::CallContext {
        chain: chain()?,
        program: program()?,
        principal: principal(OWNER)?,
        height: ORIGIN,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match registry_ops::apply(&context, None, &validated, &mut section, &mut event)? {
        Outcome::Applied { state, .. } => encode_shared(&state),
        Outcome::AlreadyApplied(_) => Err(Failure::Setup("fresh create retried".to_owned())),
    }
}

fn worker(slot: u8, nonce: u8) -> Checked<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker: derive_worker(market()?, principal(WORKER_OWNER)?, [nonce; 32])?,
        owner: principal(WORKER_OWNER)?,
        delegate: PublicKey32([slot + 50; 32]),
        metadata: MetadataDigest::new([30; 32])?,
        generation: 2,
        key_version: 2,
        metadata_revision: 4,
        valid_from: 200,
        expiry: 400,
        revocation_sequence: 0,
        effective_epoch: 1,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Available,
        slot,
        last_metadata_height: 200,
    })
}

fn market_with_workers(workers: &[WorkerCurrent]) -> Checked<Vec<u8>> {
    let base = created_state()?;
    let shared = state::decode_shared_state(&base)?;
    let mut table = WorkerTable::new();
    for current in workers {
        table.insert(current)?;
    }
    let mut section = vec![0; WORKER_TABLE_MAX_BYTES];
    let length = table.encode(&mut section)?;
    encode_shared(&shared.replace_section(Section::IdentityRoster, &section[..length])?)
}

fn frozen_roster(workers: &[WorkerCurrent]) -> Checked<Vec<u8>> {
    let mut frozen = Vec::new();
    for current in workers {
        frozen.push(WorkerRosterEntry {
            worker: current.worker,
            owner: current.owner,
            recipient: AccountId::new(PAYEE)?,
            generation: ConfigVersion::new(current.generation)?,
            key_version: ConfigVersion::new(current.key_version)?,
            public_key: current.delegate,
            metadata: current.metadata,
        });
    }
    frozen.sort_by_key(|entry| entry.worker);
    let roster = codec::Roster {
        market: market()?,
        epoch: 7,
        config: ConfigVersion::new(1)?,
        workers: &frozen,
        evaluators: &[],
    };
    let mut out = vec![0; codec::ROSTER_MAX_BYTES];
    let length = codec::encode_roster(&roster, &mut out)?;
    out.truncate(length);
    Ok(out)
}

fn chunk_payload(revision: u64, pinned: Option<[u8; 32]>, offset: u32) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.unwrap_or([0; 32]));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&8_192_u16.to_be_bytes());
    payload
}

/// Real `READ_STATE_CHUNK` reads: discovery, then pinned reads, each paired with its proof.
fn chunks(state: &[u8], root: [u8; 32], sequence: u64) -> Checked<Vec<(ReadProof, Vec<u8>)>> {
    let proof = ReadProof {
        chain: chain()?,
        program: program()?,
        native_state_root: Digest32::new(root)?,
        observed_sequence: sequence,
        execution_height: EXECUTION_HEIGHT,
        batch_id: digest(0xBB)?,
    };
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let length = read_state_chunk(state, &chunk_payload(0, None, 0), &mut out)?;
    let first = codec::decode_chunk_response(&out[..length])?;
    let (revision, pinned, total) = (first.revision, first.digest.bytes(), first.total_bytes);
    let mut bodies = vec![(proof, out[..length].to_vec())];
    for offset in (8_192..total).step_by(8_192) {
        let length = read_state_chunk(
            state,
            &chunk_payload(revision, Some(pinned), offset),
            &mut out,
        )?;
        bodies.push((proof, out[..length].to_vec()));
    }
    Ok(bodies)
}

fn certificate(vector: &CheckpointVector) -> Checked<Certificate> {
    let domain = declared_domain(&vector.settlement_domain).map_err(setup("domain"))?;
    let attestations = vector
        .attestations
        .iter()
        .map(|attestation| {
            Attestation::new(
                vector.header.protocol_version,
                vector.header.network_id,
                domain.settlement().paxeer_chain_id(),
                domain.settlement().settlement_contract(),
                vector.header.epoch,
                vector.expected_digest,
                vector.expected_digest,
                attestation.guarantor_id,
                vector.header.batch_number,
                vector.header.data_availability_root,
                attestation.replayed,
                attestation.data_possessed,
                attestation.availability_class_mask,
                attestation.attested_at_ms,
                attestation.signer,
                attestation.signature,
                attestation.signature_v,
            )
        })
        .collect();
    Ok(Certificate::new(
        Checkpoint::new(vector.header.bytes.clone(), vector.validity_proof.clone()),
        attestations,
        vector.threshold,
        None,
    ))
}

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../..")
}

/// The published accepting checkpoint vector, verified through the declared domain.
fn fresh() -> Checked<(CheckpointVector, CheckpointFinality)> {
    let vector = load_checkpoint_vectors(&repo_root())
        .map_err(setup("checkpoint vectors"))?
        .into_iter()
        .find(|vector| vector.case_name == "fresh")
        .ok_or_else(|| Failure::Setup("fresh vector missing".to_owned()))?;
    let finality = CheckpointFinality::verify(
        &certificate(&vector)?,
        &vector.settlement_domain,
        &vector.expected_digest,
    )?;
    Ok((vector, finality))
}

fn finalized(state: &[u8]) -> Checked<FinalizedSnapshot> {
    let (vector, finality) = fresh()?;
    let captured = ObservedSnapshot::capture(&chunks(
        state,
        vector.header.resulting_state_root,
        vector.header.last_sequence,
    )?)?;
    Ok(captured.finalize(Some(&finality), 5_000)?)
}

fn terms(idempotency_key: [u8; 32]) -> NativeTerms<'static> {
    NativeTerms {
        network_id: NETWORK,
        actor_did: b"did:layerx:paxai-owner",
        owner_public_key: public_key(OWNER_SEED),
        account_sequence: 1,
        idempotency_key,
        not_before: 100,
        not_after: 200,
        fee_limit: 1_000,
        capabilities: &[0, 0],
        access_declaration: ACCESS,
        response_capacity: 16,
        resources: RESOURCES,
    }
}

fn request<'a>(
    operation: Operation,
    payload: &'a [u8],
    actor: [u8; 32],
    roster: Option<&'a [u8]>,
) -> Checked<OperationRequest<'a>> {
    Ok(OperationRequest {
        operation,
        payload,
        actor: principal(actor)?,
        roster,
        sequence: match operation.metadata().sequence {
            SequencePolicy::Role => 1,
            SequencePolicy::ObjectLocal => 0,
        },
        expiry: 1_000_000,
        request: RequestId::new([0x33; 32])?,
        required_rank: FINALIZED_RANK,
    })
}

fn claim_payload(worker: WorkerId, recipient: [u8; 32], amount: u128) -> Checked<Vec<u8>> {
    let mut out = vec![0; 80];
    let length = ClaimRequest {
        worker,
        recipient: AccountId::new(recipient)?,
        amount,
    }
    .encode(&mut out)?;
    out.truncate(length);
    Ok(out)
}

fn fund_payload(amount: u128, consent: bool) -> Checked<Vec<u8>> {
    let mut out = vec![0; 57];
    let length = FundRequest {
        amount,
        refund_recipient: AccountId::new(REFUND)?,
        policy_version: FUNDING_POLICY_VERSION,
        consent,
    }
    .encode(&mut out)?;
    out.truncate(length);
    Ok(out)
}

/// A finalized snapshot with one available worker and the epoch-7 roster freezing it.
fn claim_context() -> Checked<(FinalizedSnapshot, Vec<u8>, WorkerCurrent)> {
    let current = worker(0, 1)?;
    let snapshot = finalized(&market_with_workers(&[current])?)?;
    Ok((snapshot, frozen_roster(&[current])?, current))
}

fn claim_review(amount: u128, idempotency_key: [u8; 32]) -> Checked<Review> {
    let registry = registry()?;
    let (snapshot, roster, current) = claim_context()?;
    let payload = claim_payload(current.worker, PAYEE, amount)?;
    let prepared = snapshot.prepare(
        &registry,
        FRESH_AUTHORITY,
        &request(dispatch::CLAIM, &payload, OWNER, Some(&roster))?,
        &terms(idempotency_key),
    )?;
    Ok(prepared.review(&registry)?)
}

fn signed_operation(idempotency_key: [u8; 32]) -> Checked<OperationRecord> {
    let review = claim_review(10, idempotency_key)?;
    let approved = review.terms().clone();
    let approval = review.approve(&approved, public_key(OWNER_SEED))?;
    Ok(ready(approval.sign(
        &LocalSigner::new(OWNER_SEED),
        &registry()?,
        FRESH_AUTHORITY,
    ))?)
}

/// Sequencer-signed receipt for `activity` in the Programs module at global sequence 9.
fn receipt(activity: [u8; 32], result: i32) -> Checked<(Vec<u8>, [u8; 32])> {
    let key = SigningKey::from_bytes(&[0x63; 32]);
    let encode = |signature: Option<[u8; 64]>| -> Result<Vec<u8>, WireError> {
        let mut encoder = Encoder::new(4096);
        encoder.structure_header_version(0x5201, PROTOCOL_VERSION)?;
        encoder.u16(PROTOCOL_VERSION)?;
        encoder.bytes(&activity, 32)?;
        encoder.u64(9)?;
        for filler in [2, 3, 8] {
            encoder.bytes(&[filler; 32], 32)?;
        }
        encoder.i32(result)?;
        encoder.sequence_length(0, 512)?;
        encoder.u128(1)?;
        encoder.bytes(&[4; 32], 32)?;
        encoder.u16(ModuleId::Programs as u16)?;
        encoder.u32(1)?;
        encoder.u32(1)?;
        encoder.u8(1)?;
        encoder.bytes(&[5; 32], 32)?;
        encoder.u128(25)?;
        encoder.bytes(&[6; 32], 32)?;
        encoder.u128(100)?;
        encoder.u128(75)?;
        encoder.u64(1)?;
        encoder.bytes(&[7; 32], 32)?;
        encoder.u128(10)?;
        encoder.u128(35)?;
        for filler in [9, 10, 11] {
            encoder.bytes(&[filler; 32], 32)?;
        }
        encoder.u64(1_000)?;
        encoder.u8(u8::from(signature.is_some()))?;
        if let Some(value) = signature {
            encoder.bytes(&value, 64)?;
        }
        Ok(encoder.finish())
    };
    let digest = receipt_digest(&encode(None).map_err(setup("receipt"))?)
        .map_err(setup("receipt digest"))?;
    let signed = encode(Some(key.sign(&digest).to_bytes())).map_err(setup("receipt"))?;
    Ok((signed, key.verifying_key().to_bytes()))
}

struct Scratch(PathBuf);

impl Scratch {
    fn new(label: &str) -> Checked<Self> {
        let sequence = NEXT_SCRATCH.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir().join(format!(
            "layerx-paxai-{label}-{}-{sequence}",
            std::process::id()
        ));
        fs::create_dir_all(&path).map_err(setup("scratch"))?;
        Ok(Self(path))
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: 1024 * 1024,
        maximum_connections: 1,
        maximum_streams: 4,
        maximum_queued_bytes: 2 * 1024 * 1024,
        deadline: Duration::from_secs(2),
    }
}

fn receive(stream: &mut UnixStream, tag: u16) -> (u64, Vec<u8>) {
    let frame = read_frame(stream, 1024 * 1024)
        .unwrap_or_else(|error| panic!("request frame failed: {error:?}"));
    let request =
        decode_frame(&frame).unwrap_or_else(|error| panic!("request envelope failed: {error:?}"));
    assert_eq!(request.message_tag, tag);
    (request.correlation_id, request.canonical_payload.to_vec())
}

fn respond(stream: &mut UnixStream, tag: u16, correlation_id: u64, payload: &[u8], proof: &[u8]) {
    let response = encode_frame(Frame {
        version: Interface::V1_6,
        message_tag: tag,
        correlation_id,
        canonical_payload: payload,
        proof_material: proof,
    })
    .unwrap_or_else(|error| panic!("response encoding failed: {error:?}"));
    write_frame(stream, &response, 1024 * 1024)
        .unwrap_or_else(|error| panic!("response write failed: {error:?}"));
}

fn lookup(
    sequencer: [u8; 32],
    correlation_id: u64,
    wait_mode: ReceiptWaitMode,
) -> AuthenticatedLookupContext {
    AuthenticatedLookupContext {
        interface_version: Interface::V1_6,
        correlation_id,
        sequencer_public_key: sequencer,
        wait_mode,
    }
}

fn ready<F: Future>(future: F) -> F::Output {
    struct Idle;
    impl Wake for Idle {
        fn wake(self: Arc<Self>) {}
    }
    let waker = Waker::from(Arc::new(Idle));
    let mut context = Context::from_waker(&waker);
    let mut future = pin!(future);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(output) => output,
        Poll::Pending => panic!("signing future did not complete"),
    }
}

#[test]
fn a02_snapshot_finalizes_only_with_checkpoint_evidence_for_the_same_root() -> Checked {
    let state = market_with_workers(&[worker(0, 1)?])?;
    let (vector, finality) = fresh()?;
    let root = vector.header.resulting_state_root;
    let sequence = vector.header.last_sequence;
    assert_eq!(finality.network_id(), NETWORK);
    assert_eq!(finality.state_root().bytes(), root);
    assert_eq!(finality.checkpoint().bytes(), vector.expected_digest);
    assert_eq!(finality.first_sequence(), vector.header.first_sequence);
    assert_eq!(finality.last_sequence(), sequence);
    assert_eq!(finality.batch_number(), vector.header.batch_number);

    let observed = ObservedSnapshot::capture(&chunks(&state, root, sequence)?)?;
    assert_eq!(observed.facts().proof.observed_sequence, sequence);
    assert_eq!(
        observed.clone().finalize(None, 5_000),
        Err(AiClientError::Query(QueryError::FinalityUnavailable))
    );
    let other_root = ObservedSnapshot::capture(&chunks(&state, [0x77; 32], sequence)?)?;
    assert_eq!(
        other_root.finalize(Some(&finality), 5_000),
        Err(AiClientError::Query(QueryError::BindingMismatch))
    );
    let uncovered = ObservedSnapshot::capture(&chunks(&state, root, sequence + 1)?)?;
    assert_eq!(
        uncovered.finalize(Some(&finality), 5_000),
        Err(AiClientError::Query(QueryError::BindingMismatch))
    );

    let snapshot = observed.finalize(Some(&finality), 5_000)?;
    let binding = snapshot.binding();
    assert_eq!(binding.rank, FINALIZED_RANK);
    assert_eq!(binding.native_state_root.bytes(), root);
    assert_eq!(binding.checkpoint.bytes(), vector.expected_digest);
    assert_eq!(binding.settlement, Presence::Absent);
    assert_eq!(binding.observed_sequence, sequence);
    assert_eq!(binding.execution_height, EXECUTION_HEIGHT);
    assert_eq!(binding.market, market()?);
    assert_eq!(binding.revision, snapshot.header().state_revision);
    assert_eq!(binding.publication_time_ms, 5_000);
    assert_eq!(snapshot.snapshot_id(), binding.snapshot_id()?);
    assert_eq!(snapshot.state_bytes(), state.as_slice());

    let registry = registry()?;
    let payload = fund_payload(25, true)?;
    let mut settled = request(dispatch::FUND, &payload, OWNER, None)?;
    settled.required_rank = SETTLEMENT_RANK;
    assert_eq!(
        snapshot.prepare(&registry, FRESH_AUTHORITY, &settled, &terms([0x70; 32])),
        Err(AiClientError::Query(QueryError::FinalityUnavailable))
    );
    settled.required_rank = FINALIZED_RANK - 1;
    assert_eq!(
        snapshot.prepare(&registry, FRESH_AUTHORITY, &settled, &terms([0x70; 32])),
        Err(application(NON_CANONICAL))
    );
    Ok(())
}

#[test]
fn a14_stale_authority_is_labelled_for_reads_and_refused_for_decisions() -> Checked {
    let snapshot = finalized(&market_with_workers(&[worker(0, 1)?])?)?;
    let stale = FRESH_AUTHORITY + 1;
    assert_eq!(snapshot.require_current(FRESH_AUTHORITY), Ok(()));
    assert_eq!(
        snapshot.require_current(stale),
        Err(AiClientError::StaleAuthority { lag: 9, limit: 8 })
    );
    let clock = market_clock(ORIGIN, EXECUTION_HEIGHT)?;
    assert_eq!(snapshot.header().origin_height, ORIGIN);
    let labelled = snapshot.inspect(Some(stale))?;
    assert_eq!(labelled.freshness, Freshness::Stale { lag: 9 });
    assert_eq!(labelled.freshness.code(), 2);
    assert_eq!(labelled.binding, *snapshot.binding());
    assert_eq!(labelled.snapshot_id, snapshot.snapshot_id());
    assert_eq!(labelled.clock, clock);
    let current = snapshot.inspect(Some(FRESH_AUTHORITY))?;
    assert_eq!(current.freshness, Freshness::Current);
    assert_eq!(current.freshness.code(), 1);
    assert_eq!(current.clock, clock);
    let unknown = snapshot.inspect(None)?;
    assert_eq!(unknown.freshness, Freshness::Unknown);
    assert_eq!(unknown.freshness.code(), 3);

    let registry = registry()?;
    let payload = fund_payload(25, true)?;
    let fund = request(dispatch::FUND, &payload, OWNER, None)?;
    assert_eq!(
        snapshot.prepare(&registry, stale, &fund, &terms([0x70; 32])),
        Err(AiClientError::StaleAuthority { lag: 9, limit: 8 })
    );
    let review = snapshot
        .prepare(&registry, FRESH_AUTHORITY, &fund, &terms([0x70; 32]))?
        .review(&registry)?;
    let approved = review.terms().clone();
    let approval = review.approve(&approved, public_key(OWNER_SEED))?;
    assert_eq!(
        ready(approval.sign(&LocalSigner::new(OWNER_SEED), &registry, stale)),
        Err(AiClientError::StaleAuthority { lag: 9, limit: 8 })
    );
    Ok(())
}

#[test]
fn a07_review_shows_every_disclosed_term_of_the_exact_bytes() -> Checked {
    let (snapshot, roster, current) = claim_context()?;
    let review = claim_review(10, [0x71; 32])?;
    let terms = review.terms();
    assert_eq!(terms.action, dispatch::CLAIM.selector());
    assert_eq!(terms.chain, chain()?);
    assert_eq!(terms.program, program()?);
    assert_eq!(terms.market, market()?);
    assert_eq!(terms.actor, principal(OWNER)?);
    assert_eq!(terms.epoch, 7);
    assert_eq!(terms.config, 1);
    assert_eq!(
        terms.roster,
        Presence::Present(decode_roster(&roster)?.digest()?)
    );
    assert_eq!(terms.policy, policy_with(4)?.digest()?);
    assert_eq!(terms.policy, snapshot.binding().policy);
    assert_eq!(terms.snapshot, snapshot.snapshot_id());
    assert_eq!(
        terms.effect,
        OperationEffect::Claim {
            worker: current.worker,
            recipient: AccountId::new(PAYEE)?,
            amount: 10,
            asset: AssetId::new(ASSET)?,
        }
    );
    assert_eq!(terms.capabilities, vec![0, 0]);
    assert_eq!(terms.access_declaration, ACCESS.to_vec());
    assert_eq!(terms.response_capacity, 16);
    assert_eq!(terms.resources, RESOURCES);
    assert_eq!(terms.fee_limit, 1_000);
    assert_eq!((terms.not_before, terms.not_after), (100, 200));
    assert_eq!(terms.expiry, 1_000_000);
    assert_eq!(terms.idempotency_key, [0x71; 32]);
    assert_eq!(terms.authority, public_key(OWNER_SEED).to_vec());
    assert_ne!(terms.commitment, [0; 32]);
    assert_eq!(review.prepared().effect(), terms.effect);
    assert_eq!(review.prepared().snapshot_id(), snapshot.snapshot_id());
    assert_eq!(
        review.prepared().expected_revision(),
        snapshot.binding().revision
    );
    Ok(())
}

fn refused(review: &Review, approved: &ApprovalTerms) -> Option<AiClientError> {
    review
        .clone()
        .approve(approved, public_key(OWNER_SEED))
        .err()
}

fn edited(exact: &ApprovalTerms) -> Checked<Vec<(ApprovalTerms, ReviewField)>> {
    let OperationEffect::Claim {
        worker,
        amount,
        asset,
        ..
    } = exact.effect
    else {
        return Err(Failure::Setup("claim effect expected".to_owned()));
    };
    let other_worker = derive_worker(market()?, principal(WORKER_OWNER)?, [9; 32])?;
    let refund = AccountId::new(REFUND)?;
    Ok(vec![
        (
            ApprovalTerms {
                actor: principal([21; 32])?,
                ..exact.clone()
            },
            ReviewField::Actor,
        ),
        (
            ApprovalTerms {
                market: MarketId::new([22; 32])?,
                ..exact.clone()
            },
            ReviewField::Market,
        ),
        (
            ApprovalTerms {
                roster: Presence::Absent,
                ..exact.clone()
            },
            ReviewField::Roster,
        ),
        (
            ApprovalTerms {
                snapshot: digest(0x5A)?,
                ..exact.clone()
            },
            ReviewField::Snapshot,
        ),
        (
            ApprovalTerms {
                effect: OperationEffect::Claim {
                    worker,
                    recipient: refund,
                    amount,
                    asset,
                },
                ..exact.clone()
            },
            ReviewField::Payee,
        ),
        (
            ApprovalTerms {
                effect: OperationEffect::Claim {
                    worker: other_worker,
                    recipient: AccountId::new(PAYEE)?,
                    amount,
                    asset,
                },
                ..exact.clone()
            },
            ReviewField::Worker,
        ),
    ])
}

fn native_edits(exact: &ApprovalTerms) -> Vec<(ApprovalTerms, ReviewField)> {
    vec![
        (
            ApprovalTerms {
                access_declaration: b"other".to_vec(),
                ..exact.clone()
            },
            ReviewField::AccessDeclaration,
        ),
        (
            ApprovalTerms {
                response_capacity: 17,
                ..exact.clone()
            },
            ReviewField::ResponseCapacity,
        ),
        (
            ApprovalTerms {
                resources: Resources([1, 1, 1, 1, 1, 1, 1]),
                ..exact.clone()
            },
            ReviewField::Resources,
        ),
        (
            ApprovalTerms {
                fee_limit: 1_001,
                ..exact.clone()
            },
            ReviewField::FeeLimit,
        ),
        (
            ApprovalTerms {
                not_after: 201,
                ..exact.clone()
            },
            ReviewField::Validity,
        ),
        (
            ApprovalTerms {
                authority: public_key(WORKER_SEED).to_vec(),
                ..exact.clone()
            },
            ReviewField::Authority,
        ),
        (
            ApprovalTerms {
                commitment: [0x5C; 32],
                ..exact.clone()
            },
            ReviewField::Commitment,
        ),
    ]
}

#[test]
fn a07_approval_refuses_any_term_that_differs_from_the_disclosure() -> Checked {
    let review = claim_review(10, [0x71; 32])?;
    let exact = review.terms().clone();
    let larger = claim_review(11, [0x71; 32])?;
    assert_eq!(
        refused(&review, larger.terms()),
        Some(AiClientError::ReviewMismatch(ReviewField::Amount))
    );
    let mut edits = edited(&exact)?;
    edits.extend(native_edits(&exact));
    for (approved, field) in &edits {
        assert_eq!(
            refused(&review, approved),
            Some(AiClientError::ReviewMismatch(*field))
        );
    }
    assert_eq!(
        review
            .clone()
            .approve(&exact, public_key(WORKER_SEED))
            .err(),
        Some(AiClientError::UnauthorizedKey)
    );
    assert!(review.approve(&exact, public_key(OWNER_SEED)).is_ok());
    Ok(())
}

#[test]
fn a07_owner_signs_exactly_the_reviewed_native_program_call() -> Checked {
    let registry = registry()?;
    let review = claim_review(10, [0x71; 32])?;
    let canonical = review.prepared().canonical_bytes().to_vec();
    let intent = review.prepared().intent();
    let approved = review.terms().clone();
    let approval = review.approve(&approved, public_key(OWNER_SEED))?;
    assert_eq!(
        ready(
            approval
                .clone()
                .sign(&LocalSigner::new(WORKER_SEED), &registry, FRESH_AUTHORITY)
        ),
        Err(AiClientError::UnauthorizedKey)
    );
    let record = ready(approval.sign(&LocalSigner::new(OWNER_SEED), &registry, FRESH_AUTHORITY))?;
    assert_eq!(record.state(), OperationState::Signed);
    assert_eq!(record.domain(), DomainStatus::Pending);
    assert_eq!(record.attempt(), 0);
    assert_eq!(record.intent(), intent.bytes());
    assert_eq!(record.network_id(), NETWORK);
    assert_eq!(record.protocol_version(), NATIVE_CALL_PROTOCOL_VERSION);
    assert_eq!(record.idempotency_key(), [0x71; 32]);
    assert_eq!(record.signer_public_key(), public_key(OWNER_SEED));
    assert_eq!(record.not_after(), 200);
    assert_eq!(
        (
            record.result_code(),
            record.global_sequence(),
            record.checkpoint()
        ),
        (None, None, None)
    );

    let activity = decode_signed(record.signed_bytes(), &registry).map_err(setup("signed"))?;
    assert_eq!(
        activity_id(&activity).map_err(setup("id"))?,
        record.activity_id()
    );
    let unsigned = encode_unsigned(&activity).map_err(setup("unsigned"))?;
    assert_eq!(unsigned, canonical);
    let call = NativeProgramCall::decode(activity.payload()).map_err(setup("call"))?;
    assert_eq!(call.program_id.bytes(), PROGRAM);
    assert_eq!(call.guest_abi, GUEST_ABI);
    assert_eq!(call.entrypoint, ENTRYPOINT);
    assert_eq!(call.access_declaration, ACCESS);
    let envelope = decode_envelope(call.calldata)?;
    assert_eq!(envelope.envelope.operation, dispatch::CLAIM);
    assert_eq!(envelope.request_digest()?, intent);
    let (_, _, current) = claim_context()?;
    assert_eq!(
        envelope.envelope.payload,
        claim_payload(current.worker, PAYEE, 10)?
    );
    let signature: [u8; 64] = activity
        .signature()
        .and_then(|bytes| bytes.try_into().ok())
        .ok_or_else(|| Failure::Setup("signature".to_owned()))?;
    let message = SignatureMessage::new(Domain::SignaturePreimage, 3, NETWORK, &unsigned)
        .map_err(setup("message"))?;
    assert_eq!(
        ed25519::verify(&public_key(OWNER_SEED), &signature, message),
        Ok(())
    );

    let encoded = record.encode()?;
    assert_eq!(OperationRecord::decode(&encoded, &registry)?, record);
    let mut prepared_state = encoded.clone();
    prepared_state[8] = OperationState::Prepared.code();
    assert_eq!(
        OperationRecord::decode(&prepared_state, &registry),
        Err(AiClientError::CorruptRecord)
    );
    let mut trailing = encoded;
    trailing.push(0);
    assert_eq!(
        OperationRecord::decode(&trailing, &registry),
        Err(AiClientError::CorruptRecord)
    );
    Ok(())
}

#[test]
fn a07_preview_refuses_in_program_order_before_signing() -> Checked {
    let registry = registry()?;
    let (snapshot, roster, current) = claim_context()?;
    let plan = |request: &OperationRequest<'_>| {
        snapshot
            .prepare(&registry, FRESH_AUTHORITY, request, &terms([0x72; 32]))
            .err()
    };
    let funding = fund_payload(25, true)?;
    let review = snapshot
        .prepare(
            &registry,
            FRESH_AUTHORITY,
            &request(dispatch::FUND, &funding, OWNER, None)?,
            &terms([0x72; 32]),
        )?
        .review(&registry)?;
    let approved = review.terms().clone();
    assert_eq!(
        review.approve(&approved, public_key(WORKER_SEED)).err(),
        Some(AiClientError::UnauthorizedKey)
    );
    let stranger = request(dispatch::FUND, &funding, [21; 32], None)?;
    assert_eq!(plan(&stranger), Some(application(UNAUTHORIZED)));
    let unconsented = fund_payload(25, false)?;
    assert_eq!(
        plan(&request(dispatch::FUND, &unconsented, OWNER, None)?),
        Some(application(F06_CONTRIBUTION_CONSENT_REQUIRED))
    );
    let empty = fund_payload(0, true)?;
    assert_eq!(
        plan(&request(dispatch::FUND, &empty, OWNER, None)?),
        Some(application(F06_INVALID_AMOUNT))
    );
    let misdirected = claim_payload(current.worker, REFUND, 10)?;
    assert_eq!(
        plan(&request(
            dispatch::CLAIM,
            &misdirected,
            OWNER,
            Some(&roster)
        )?),
        Some(application(F06_WRONG_CLAIM_RECIPIENT))
    );
    let claim = claim_payload(current.worker, PAYEE, 10)?;
    assert_eq!(
        plan(&request(dispatch::CLAIM, &claim, OWNER, None)?),
        Some(application(WRONG_ROSTER))
    );
    let stranger_worker = derive_worker(market()?, principal(WORKER_OWNER)?, [9; 32])?;
    let unknown = claim_payload(stranger_worker, PAYEE, 10)?;
    assert_eq!(
        plan(&request(dispatch::CLAIM, &unknown, OWNER, Some(&roster))?),
        Some(application(F06_UNKNOWN_WORKER_ENTITLEMENT))
    );
    let nothing = claim_payload(current.worker, PAYEE, 0)?;
    assert_eq!(
        plan(&request(dispatch::CLAIM, &nothing, OWNER, Some(&roster))?),
        Some(application(F06_NOTHING_TO_CLAIM))
    );
    assert_eq!(
        plan(&request(dispatch::READ_HEADER, &[], OWNER, None)?),
        Some(AiClientError::NotMutation)
    );
    Ok(())
}

#[test]
fn a08_lost_ack_resolves_by_exact_resend_receipt_and_checkpoint() -> Checked {
    let registry = registry()?;
    let scratch = Scratch::new("lost-ack")?;
    let journal = OperationJournal::open(&scratch.0.join("journal"))?;
    let mut record = signed_operation([0x71; 32])?;
    journal.record_signed(&record)?;
    let id = record.activity_id();
    let record_path = journal.record_path(&id);
    let (receipt_bytes, sequencer) = receipt(id, 0)?;
    let socket = scratch.0.join("node.sock");
    let listener = UnixListener::bind(&socket).map_err(setup("bind"))?;
    let server = thread::spawn(move || {
        let (mut lost, _) = listener
            .accept()
            .unwrap_or_else(|error| panic!("accept: {error}"));
        let (_, submitted) = receive(&mut lost, SUBMIT_REQUEST);
        let journaled = fs::read(&record_path).unwrap_or_else(|error| panic!("journal: {error}"));
        drop(lost);
        let (mut node, _) = listener
            .accept()
            .unwrap_or_else(|error| panic!("accept: {error}"));
        let (correlation, selector) = receive(&mut node, LOOKUP_REQUEST);
        respond(&mut node, LOOKUP_RESPONSE, correlation, &[], &[]);
        let (correlation, resent) = receive(&mut node, SUBMIT_REQUEST);
        respond(&mut node, SUBMIT_ACK, correlation, &resent, &id);
        let (correlation, _) = receive(&mut node, LOOKUP_REQUEST);
        respond(&mut node, LOOKUP_RESPONSE, correlation, &receipt_bytes, &[]);
        (submitted, journaled, selector, resent)
    });
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&socket, &gate, limits()).map_err(setup("connect"))?;
    let state = journal.submit(&mut record, &mut transport, &registry, Interface::V1_6, 1)?;
    assert_eq!((state, record.attempt()), (OperationState::Unknown, 1));
    drop(transport);
    assert_eq!(journal.load(&id, &registry)?, record);

    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&socket, &gate, limits()).map_err(setup("reconnect"))?;
    let durable = lookup(sequencer, 2, ReceiptWaitMode::Durable);
    assert_eq!(
        journal.resolve(&mut record, &mut transport, durable)?,
        OperationState::Unknown
    );
    let state = journal.resend_exact(&mut record, &mut transport, &registry, Interface::V1_6, 3)?;
    assert_eq!((state, record.attempt()), (OperationState::Pending, 2));
    let immediate = lookup(sequencer, 4, ReceiptWaitMode::Immediate);
    assert_eq!(
        journal.resolve(&mut record, &mut transport, immediate)?,
        OperationState::Executed
    );
    assert_eq!(
        (record.result_code(), record.global_sequence()),
        (Some(0), Some(9))
    );
    let (_, finality) = fresh()?;
    assert_eq!(
        journal.finalize(&mut record, &finality)?,
        OperationState::Finalized
    );
    assert_eq!(record.checkpoint(), Some(finality.checkpoint().bytes()));
    assert_eq!(record.domain(), DomainStatus::Pending);
    assert_eq!(journal.load(&id, &registry)?, record);

    let (submitted, journaled, selector, resent) = server
        .join()
        .map_err(|_| Failure::Setup("node panicked".to_owned()))?;
    assert_eq!(submitted, record.signed_bytes());
    assert_eq!(resent, record.signed_bytes());
    let mut expected_selector = vec![1];
    expected_selector.extend_from_slice(&id);
    expected_selector.push(2);
    assert_eq!(selector, expected_selector);
    let interrupted = OperationRecord::decode(&journaled, &registry)?;
    assert_eq!(
        (interrupted.state(), interrupted.attempt()),
        (OperationState::Submitting, 1)
    );
    fs::write(journal.record_path(&id), &journaled).map_err(setup("crash"))?;
    let recovered = journal.load(&id, &registry)?;
    assert_eq!(
        (recovered.state(), recovered.attempt()),
        (OperationState::Unknown, 1)
    );
    let stored = fs::read(journal.record_path(&id)).map_err(setup("reread"))?;
    assert_eq!(OperationRecord::decode(&stored, &registry)?, recovered);
    Ok(())
}

#[test]
fn a08_refusal_receipt_fails_and_unsent_operations_expire_only_when_signed() -> Checked {
    let registry = registry()?;
    let scratch = Scratch::new("refusal")?;
    let journal = OperationJournal::open(&scratch.0.join("journal"))?;
    let mut record = signed_operation([0x71; 32])?;
    journal.record_signed(&record)?;
    let id = record.activity_id();
    let (receipt_bytes, sequencer) = receipt(id, -303)?;
    let socket = scratch.0.join("node.sock");
    let listener = UnixListener::bind(&socket).map_err(setup("bind"))?;
    let server = thread::spawn(move || {
        let (mut node, _) = listener
            .accept()
            .unwrap_or_else(|error| panic!("accept: {error}"));
        let (correlation, submitted) = receive(&mut node, SUBMIT_REQUEST);
        respond(&mut node, SUBMIT_ACK, correlation, &submitted, &id);
        let (correlation, _) = receive(&mut node, LOOKUP_REQUEST);
        respond(&mut node, LOOKUP_RESPONSE, correlation, &receipt_bytes, &[]);
        submitted
    });
    let gate = ConnectionGate::new(1);
    let mut transport = Uds::connect(&socket, &gate, limits()).map_err(setup("connect"))?;
    let state = journal.submit(&mut record, &mut transport, &registry, Interface::V1_6, 1)?;
    assert_eq!((state, record.attempt()), (OperationState::Pending, 1));
    let immediate = lookup(sequencer, 2, ReceiptWaitMode::Immediate);
    assert_eq!(
        journal.resolve(&mut record, &mut transport, immediate)?,
        OperationState::Failed
    );
    assert_eq!(
        (record.result_code(), record.global_sequence()),
        (Some(-303), Some(9))
    );
    let (_, finality) = fresh()?;
    let failed = AiClientError::InvalidTransition {
        from: OperationState::Failed,
    };
    assert_eq!(
        journal.finalize(&mut record, &finality),
        Err(failed.clone())
    );
    assert_eq!(
        journal.resend_exact(&mut record, &mut transport, &registry, Interface::V1_6, 3),
        Err(failed)
    );
    assert_eq!(journal.load(&id, &registry)?, record);
    let submitted = server
        .join()
        .map_err(|_| Failure::Setup("node panicked".to_owned()))?;
    assert_eq!(submitted, record.signed_bytes());

    let mut unsent = signed_operation([0x73; 32])?;
    journal.record_signed(&unsent)?;
    assert_eq!(journal.expire(&mut unsent, 200)?, OperationState::Signed);
    assert_eq!(journal.expire(&mut unsent, 201)?, OperationState::Failed);
    assert_eq!(
        (unsent.result_code(), unsent.global_sequence()),
        (None, None)
    );
    assert_eq!(journal.load(&unsent.activity_id(), &registry)?, unsent);
    assert_eq!(
        journal.expire(&mut unsent, 202),
        Err(AiClientError::InvalidTransition {
            from: OperationState::Failed
        })
    );
    Ok(())
}

fn source(policy: &TaskPolicyV1) -> Checked<HistoricalSource> {
    Ok(HistoricalSource {
        snapshot: digest(0x61)?,
        config: ConfigVersion::new(1)?,
        policy: policy.digest()?,
        roster: Presence::Absent,
    })
}

#[test]
fn a09_epoch_history_keeps_archive_status_and_the_epoch_own_policy() -> Checked {
    let recorded = policy_with(4)?;
    let mut content = vec![0; TASK_POLICY_BYTES];
    recorded.encode(&mut content)?;
    let present = Presence::Present(source(&recorded)?);

    let never = EpochHistoryEntry::new(3, EpochStatus::NeverOpened, Presence::Absent)?;
    assert_eq!((never.epoch(), never.status().code()), (3, 2));
    assert_eq!(
        never.policy(&content),
        Err(AiClientError::HistoryUnavailable(EpochStatus::NeverOpened))
    );
    assert_eq!(
        EpochHistoryEntry::new(3, EpochStatus::NeverOpened, present),
        Err(application(NON_CANONICAL))
    );
    assert_eq!(
        EpochHistoryEntry::new(4, EpochStatus::ArchiveRequired, Presence::Absent),
        Err(application(NON_CANONICAL))
    );

    let archived = EpochHistoryEntry::new(4, EpochStatus::ArchiveRequired, present)?;
    assert_eq!((archived.status().code(), archived.source()), (4, present));
    let mut flipped = content.clone();
    flipped[TASK_POLICY_BYTES - 1] ^= 1;
    let integrity = Err(AiClientError::Query(QueryError::IntegrityFailure));
    assert_eq!(archived.policy(&flipped), integrity);
    assert_eq!(
        archived.policy(&content[..TASK_POLICY_BYTES - 1]),
        integrity
    );
    let mut current = vec![0; TASK_POLICY_BYTES];
    policy_with(8)?.encode(&mut current)?;
    assert_eq!(archived.policy(&current), integrity);
    assert_eq!(archived.policy(&content), Ok(recorded));

    let lost = EpochHistoryEntry::new(5, EpochStatus::ArchiveUnavailable, present)?;
    assert_eq!(
        lost.policy(&content),
        Err(AiClientError::HistoryUnavailable(
            EpochStatus::ArchiveUnavailable
        ))
    );
    let unsupported = EpochHistoryEntry::new(6, EpochStatus::UnsupportedVersion, Presence::Absent)?;
    assert_eq!(unsupported.status().code(), 6);
    let retained = EpochHistoryEntry::new(7, EpochStatus::RetainedTerminal, present)?;
    assert_eq!(retained.policy(&content), Ok(recorded));
    Ok(())
}

#[test]
fn a09_reused_seat_never_inherits_the_previous_identity() -> Checked {
    let previous = worker(0, 1)?;
    let successor = worker(0, 9)?;
    let roster = frozen_roster(&[previous])?;
    let before = finalized(&market_with_workers(&[previous])?)?;
    let after = finalized(&market_with_workers(&[successor])?)?;
    let frozen = before.participant(
        ParticipantKind::Worker,
        previous.worker.bytes(),
        Some(&roster),
    )?;
    assert!(frozen.frozen_member);
    assert_eq!(
        after.participant(
            ParticipantKind::Worker,
            previous.worker.bytes(),
            Some(&roster)
        ),
        Err(application(NOT_FOUND))
    );
    let row = after.participant(
        ParticipantKind::Worker,
        successor.worker.bytes(),
        Some(&roster),
    )?;
    assert_eq!(row.id, successor.worker.bytes());
    assert!(!row.frozen_member);
    assert_eq!(row.frozen_generation, Presence::Absent);
    assert_eq!(row.history_status, Availability::NotYetProduced);
    assert_eq!(row.history, Presence::Absent);
    assert_eq!(row.score.status(), ScoreStatus::NotProduced);
    assert_eq!(row.score.ppm(), Presence::Absent);
    Ok(())
}

type Sections = BTreeMap<String, Vec<(String, String)>>;

fn schema() -> Checked<Sections> {
    let text = fs::read_to_string(repo_root().join("platform/sdk/schema/paxai-v1.kvx"))
        .map_err(setup("schema"))?;
    let mut sections = Sections::new();
    let mut current = String::new();
    for line in text
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
    {
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|rest| rest.strip_suffix(']'))
        {
            name.clone_into(&mut current);
            continue;
        }
        let (key, value) = line
            .split_once(" = ")
            .ok_or_else(|| Failure::Setup(format!("schema line {line}")))?;
        sections
            .entry(current.clone())
            .or_default()
            .push((key.to_owned(), value.to_owned()));
    }
    Ok(sections)
}

fn entries(sections: &Sections, name: &str) -> Vec<(String, String)> {
    sections.get(name).cloned().unwrap_or_default()
}

fn codes(pairs: &[(&str, u8)]) -> Vec<(String, String)> {
    pairs
        .iter()
        .map(|(key, code)| ((*key).to_owned(), code.to_string()))
        .collect()
}

fn operation_line(selector: u16) -> Checked<String> {
    let metadata = Operation::decode(selector)?.metadata();
    Ok(format!(
        "[\"0x{selector:04x}\", \"{}\", \"{}\", \"{}\", \"{}\", \"{}\", \"{}\"]",
        metadata.feature,
        match metadata.boundary {
            CallBoundary::Mutation => "mutation",
            CallBoundary::ProgramRead => "program-read",
        },
        match metadata.sequence {
            SequencePolicy::Role => "role",
            SequencePolicy::ObjectLocal => "object-local",
        },
        if metadata.delegate_allowed {
            "delegate"
        } else {
            "native-only"
        },
        metadata.payload_min,
        metadata.payload_max,
    ))
}

#[test]
fn schema_is_pinned_to_the_program_codecs_and_client_states() -> Checked {
    let sections = schema()?;
    let mut operations = Vec::new();
    for metadata in dispatch::OPERATIONS {
        let selector = metadata.operation.selector();
        operations.push((metadata.name.to_owned(), operation_line(selector)?));
    }
    assert_eq!(entries(&sections, "operations"), operations);
    let states = [
        OperationState::Prepared,
        OperationState::Reviewed,
        OperationState::Signed,
        OperationState::Submitting,
        OperationState::Pending,
        OperationState::Executed,
        OperationState::Finalized,
        OperationState::Failed,
        OperationState::Unknown,
    ];
    let names = [
        "prepared",
        "reviewed",
        "signed",
        "submitting",
        "pending",
        "executed",
        "finalized",
        "failed",
        "unknown",
    ];
    let state_codes: Vec<(&str, u8)> = names
        .into_iter()
        .zip(states.map(OperationState::code))
        .collect();
    assert_eq!(entries(&sections, "sdk_state"), codes(&state_codes));
    assert_eq!(
        entries(&sections, "domain_status"),
        codes(&[
            ("pending", DomainStatus::Pending.code()),
            ("completed", DomainStatus::Completed.code())
        ])
    );
    assert_eq!(
        entries(&sections, "freshness"),
        codes(&[
            ("current", Freshness::Current.code()),
            ("stale", Freshness::Stale { lag: 9 }.code()),
            ("unknown", Freshness::Unknown.code()),
        ])
    );
    assert_eq!(
        entries(&sections, "epoch_status"),
        codes(&[
            ("retained", EpochStatus::Retained.code()),
            ("never_opened", EpochStatus::NeverOpened.code()),
            ("retained_terminal", EpochStatus::RetainedTerminal.code()),
            ("archive_required", EpochStatus::ArchiveRequired.code()),
            (
                "archive_unavailable",
                EpochStatus::ArchiveUnavailable.code()
            ),
            (
                "unsupported_version",
                EpochStatus::UnsupportedVersion.code()
            ),
        ])
    );
    Ok(())
}

#[test]
fn schema_view_codes_match_the_program_query_enums() -> Checked {
    let sections = schema()?;
    assert_eq!(
        entries(&sections, "availability"),
        codes(&[
            ("available", Availability::Available as u8),
            ("not_enabled", Availability::NotEnabled as u8),
            ("not_yet_produced", Availability::NotYetProduced as u8),
            (
                "content_unavailable",
                Availability::ContentUnavailable as u8
            ),
            (
                "unsupported_version",
                Availability::UnsupportedVersion as u8
            ),
        ])
    );
    assert_eq!(
        entries(&sections, "participant_kind"),
        codes(&[
            ("worker", ParticipantKind::Worker as u8),
            ("evaluator", ParticipantKind::Evaluator as u8)
        ])
    );
    assert_eq!(
        entries(&sections, "score_status"),
        codes(&[
            ("present", ScoreStatus::Present as u8),
            ("no_admissible_score", ScoreStatus::NoAdmissibleScore as u8),
            (
                "insufficient_coverage",
                ScoreStatus::InsufficientCoverage as u8
            ),
            ("not_produced", ScoreStatus::NotProduced as u8),
            ("unavailable", ScoreStatus::Unavailable as u8),
            ("unsupported", ScoreStatus::Unsupported as u8),
        ])
    );
    Ok(())
}

#[test]
fn schema_limits_and_native_call_match_the_rust_constants() -> Checked {
    let sections = schema()?;
    let value = |section: &str, key: &str| {
        entries(&sections, section)
            .into_iter()
            .find(|(name, _)| name == key)
            .map(|(_, value)| value)
    };
    let windows = EpochWindows::new(0, 0)?;
    let limits: [(&str, u64); 21] = [
        ("finalized_rank", u64::from(FINALIZED_RANK)),
        ("settlement_rank", u64::from(SETTLEMENT_RANK)),
        ("default_page_rows", u64::from(DEFAULT_LIMIT)),
        (
            "max_page_rows",
            u64::try_from(PAGE_MAX_ROWS).map_err(setup("rows"))?,
        ),
        (
            "page_max_bytes",
            u64::try_from(PAGE_MAX_BYTES).map_err(setup("page"))?,
        ),
        ("cursor_ttl_ms", CURSOR_LIFETIME_MS),
        (
            "cursor_max_bytes",
            u64::try_from(CURSOR_MAX_BYTES).map_err(setup("cursor"))?,
        ),
        (
            "worker_rows",
            u64::try_from(MAX_WORKERS).map_err(setup("workers"))?,
        ),
        (
            "evaluator_rows",
            u64::try_from(MAX_EVALUATORS).map_err(setup("evaluators"))?,
        ),
        (
            "task_rows",
            u64::try_from(MAX_TASKS).map_err(setup("tasks"))?,
        ),
        ("authority_freshness_heights", AUTHORITY_FRESHNESS_HEIGHTS),
        ("epoch_span_heights", windows.end - windows.start),
        ("work_heights", windows.commit - windows.start),
        ("commit_heights", windows.reveal - windows.commit),
        ("reveal_heights", windows.settlement - windows.reveal),
        ("settlement_heights", windows.end - windows.settlement),
        (
            "envelope_max_bytes",
            u64::try_from(MAX_ENVELOPE_BYTES).map_err(setup("envelope"))?,
        ),
        (
            "payload_max_bytes",
            u64::try_from(MAX_PAYLOAD_BYTES).map_err(setup("payload"))?,
        ),
        (
            "state_max_bytes",
            u64::try_from(MAX_STATE_BYTES).map_err(setup("state"))?,
        ),
        (
            "chunk_max_bytes",
            u64::try_from(MAX_CHUNK_BYTES).map_err(setup("chunk"))?,
        ),
        ("public_aggregate_min_samples", 20),
    ];
    for (key, expected) in limits {
        assert_eq!(
            value("limits", key),
            Some(expected.to_string()),
            "limits.{key}"
        );
    }
    let entrypoint = String::from_utf8(ENTRYPOINT.to_vec()).map_err(setup("entrypoint"))?;
    assert_eq!(
        value("native_call", "protocol_version"),
        Some(NATIVE_CALL_PROTOCOL_VERSION.to_string())
    );
    assert_eq!(
        value("native_call", "module"),
        Some((ModuleId::Programs as u16).to_string())
    );
    assert_eq!(
        value("native_call", "activity_ordinal"),
        Some(PROGRAM_CALL_ORDINAL.to_string())
    );
    assert_eq!(
        value("native_call", "guest_abi"),
        Some(GUEST_ABI.to_string())
    );
    assert_eq!(
        value("native_call", "entrypoint"),
        Some(format!("\"{entrypoint}\""))
    );
    assert_eq!(
        value("binding", "snapshot_id_domain"),
        Some("\"PAXAI/view/v1\"".to_owned())
    );
    Ok(())
}
