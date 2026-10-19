//! F10 minimal finalized snapshot projection over real market state, real chunk capture,
//! real k256-signed checkpoint certificates and a file-backed `SQLite` projection.

use std::fs;
use std::path::PathBuf;

use k256::ecdsa::{Signature, SigningKey};
use layerx_crypto::secp256k1;
use layerx_indexer::ai_market::event_id;
use layerx_indexer::ai_market::{
    Alert, Availability, CheckpointProof, CursorKeyring, FinalityAuthority, Freshness, Observation,
    PageRequest, ParticipantKind, ProjectionState, ProjectionStore, QueryLimiter, RateConfig,
    ReadVerification, SnapshotRecord, ViewError, Viewer, AUTHORITY_FRESHNESS_HEIGHTS,
    MINIMUM_PUBLICATION_RANK, SNAPSHOT_EVENT_TAG,
};
use layerx_indexer::codec::hex;
use layerx_programs_ai_market::{
    codec::{self, decode_envelope, derive_market, derive_worker, encode_envelope, Envelope},
    dispatch,
    errors::ApplicationError,
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    queries::{read_state_chunk, CaptureFacts, QueryError, ReadProof, StateCapture},
    registry::{derive_rewards_account, F01_SECTION_CAP},
    registry_ops::{self, Outcome},
    state::{self, Section, SharedState},
    types::{
        AssetId, Authentication, ChainDomain, Digest32, MarketId, MetadataDigest, Presence,
        PrincipalId, ProgramId, PublicKey32, RequestId, RubricDigest, StateDigest,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};
use layerx_proof::checkpoint::{
    checkpoint_id, Attestation, Certificate, Checkpoint, CheckpointError, GuarantorKey,
    SettlementDomain,
};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{checkpoint_attestation_digest, program_execution_batch_id};
use layerx_wire::limits::PROTOCOL_VERSION;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const CHUNK_RESPONSE_MAX: usize = 8_244;
const NETWORK: u32 = 42;
const PAXEER_CHAIN: u64 = 31_337;
const CONTRACT: [u8; 20] = [0x55; 20];
const CHECKPOINT_EPOCH: u64 = 7;
const BATCH: u64 = 8;
const PREVIOUS_ROOT: [u8; 32] = [7; 32];
const ACTIVITY_ROOT: [u8; 32] = [9; 32];
const DA_ROOT: [u8; 32] = [12; 32];
const HEADER_TIME_MS: u64 = 1_000;
const PUBLISHED_AT_MS: u64 = 2_000;
const NOW_MS: u64 = 5_000_000;
const SECRET: [u8; 32] = [0x5A; 32];

enum Failure {
    View(ViewError),
    Other(String),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::View(error) => write!(f, "view refusal {error:?}"),
            Self::Other(what) => f.write_str(what),
        }
    }
}
impl From<ViewError> for Failure {
    fn from(error: ViewError) -> Self {
        Self::View(error)
    }
}
macro_rules! other_failure {
    ($($error:ty),+ $(,)?) => {$(
        impl From<$error> for Failure {
            fn from(error: $error) -> Self {
                Self::Other(format!("{error:?}"))
            }
        }
    )+};
}
other_failure!(
    ApplicationError,
    QueryError,
    layerx_wire::WireError,
    CheckpointError,
    k256::ecdsa::Error,
    layerx_crypto::VerifyError,
    rusqlite::Error,
    std::io::Error,
    &'static str,
);
type Checked<T = ()> = Result<T, Failure>;

fn chain() -> Checked<ChainDomain> {
    Ok(ChainDomain::new(CHAIN)?)
}
fn program() -> Checked<ProgramId> {
    Ok(ProgramId::new(PROGRAM)?)
}
fn market() -> Checked<MarketId> {
    Ok(derive_market(chain()?, program()?)?)
}
fn market_hex() -> Checked<String> {
    Ok(hex(market()?.as_bytes()))
}
fn principal(bytes: [u8; 32]) -> Checked<PrincipalId> {
    Ok(PrincipalId::new(bytes)?)
}
fn digest(byte: u8) -> Checked<Digest32> {
    Ok(Digest32::new([byte; 32])?)
}

fn policy() -> Checked<TaskPolicyV1> {
    Ok(TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: digest(1)?,
            dataset_artifact: [2; 32],
            benchmark_suite: digest(3)?,
            rubric: RubricDigest::new([4; 32])?,
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
    let n = state::encode_shared_state(shared, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// Real F01 CREATE through `registry_ops::apply`.
fn created_state() -> Checked<Vec<u8>> {
    let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
    policy()?.encode(&mut encoded_policy)?;
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
    let mut encoded = vec![0; 16_384];
    let n = encode_envelope(&envelope, &mut encoded)?;
    let validated = decode_envelope(encoded.get(..n).ok_or("envelope")?)?;
    let ctx = registry_ops::CallContext {
        chain: chain()?,
        program: program()?,
        principal: principal(OWNER)?,
        height: 1000,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match registry_ops::apply(&ctx, None, &validated, &mut section, &mut event)? {
        Outcome::Applied { state, .. } => encode_shared(&state),
        Outcome::AlreadyApplied(_) => Err(Failure::Other("fresh create retried".to_owned())),
    }
}

fn worker(slot: u8, nonce: u8, state: WorkerState) -> Checked<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker: derive_worker(market()?, principal([20; 32])?, [nonce; 32])?,
        owner: principal([20; 32])?,
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
        state,
        slot,
        last_metadata_height: 200,
    })
}

/// Created market plus an F02 worker table; even slots available, odd slots retired. Seat
/// `slot` is held by the worker derived from nonce `slot + nonce_base`.
fn market_with_workers(count: u8, nonce_base: u8) -> Checked<Vec<u8>> {
    let base = created_state()?;
    let shared = state::decode_shared_state(&base)?;
    let mut table = WorkerTable::new();
    for slot in 0..count {
        let status = if slot % 2 == 0 {
            WorkerState::Available
        } else {
            WorkerState::Retired
        };
        table.insert(&worker(slot, slot + nonce_base, status)?)?;
    }
    let mut section = vec![0; WORKER_TABLE_MAX_BYTES];
    let n = table.encode(&mut section)?;
    let identity = section.get(..n).ok_or("worker table")?;
    encode_shared(&shared.replace_section(Section::IdentityRoster, identity)?)
}

fn chunk_payload(revision: u64, pinned: Option<StateDigest>, offset: u32) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.map_or([0; 32], StateDigest::bytes));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&8192u16.to_be_bytes());
    payload
}

fn batch_id(sequence: u64) -> Checked<Digest32> {
    Ok(Digest32::new(program_execution_batch_id(
        PREVIOUS_ROOT,
        ACTIVITY_ROOT,
        sequence,
        sequence,
        BATCH,
    )?)?)
}

/// Whole same-root capture through the real chunk selector and `StateCapture`.
fn capture(state_bytes: &[u8], root: u8, sequence: u64) -> Checked<(Vec<u8>, CaptureFacts)> {
    let proof = ReadProof {
        chain: chain()?,
        program: program()?,
        native_state_root: digest(root)?,
        observed_sequence: sequence,
        execution_height: sequence + 110,
        batch_id: batch_id(sequence)?,
    };
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let n = read_state_chunk(state_bytes, &chunk_payload(0, None, 0), &mut out)?;
    let first = codec::decode_chunk_response(out.get(..n).ok_or("chunk")?)?;
    let (revision, pinned, total) = (first.revision, first.digest, first.total_bytes);
    let mut bodies = vec![out.get(..n).ok_or("chunk")?.to_vec()];
    for offset in (8192..total).step_by(8192) {
        let n = read_state_chunk(
            state_bytes,
            &chunk_payload(revision, Some(pinned), offset),
            &mut out,
        )?;
        bodies.push(out.get(..n).ok_or("chunk")?.to_vec());
    }
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut whole = StateCapture::new(&mut buffer);
    for body in &bodies {
        whole.accept(&proof, body)?;
    }
    let (bytes, facts) = whole.finish()?;
    Ok((bytes.to_vec(), facts))
}

fn header(root: [u8; 32], sequence: u64, network: u32) -> Checked<Vec<u8>> {
    let mut e = Encoder::new(354);
    e.structure_header_version(0x1701, PROTOCOL_VERSION)?;
    e.u8(15)?;
    for field in 1..=15u8 {
        e.tag(field, 15)?;
        match field {
            1 => e.u16(PROTOCOL_VERSION)?,
            2 => e.u32(network)?,
            3 => e.u64(CHECKPOINT_EPOCH)?,
            4 => e.u64(BATCH)?,
            5 | 6 => e.u64(sequence)?,
            7 => e.bytes(&PREVIOUS_ROOT, 32)?,
            8 => e.bytes(&root, 32)?,
            9 => e.bytes(&ACTIVITY_ROOT, 32)?,
            12 => e.bytes(&DA_ROOT, 32)?,
            14 => e.u64(HEADER_TIME_MS)?,
            _ => e.bytes(&[field; 32], 32)?,
        }
    }
    Ok(e.finish())
}

fn guarantor(value: u8) -> Checked<(SigningKey, GuarantorKey)> {
    let mut scalar = [0u8; 32];
    scalar[31] = value;
    let signing = SigningKey::from_bytes((&scalar).into())?;
    let public: [u8; 33] = signing
        .verifying_key()
        .to_encoded_point(true)
        .as_bytes()
        .try_into()
        .map_err(|_| "compressed key width")?;
    let mut id = [0u8; 32];
    id[0] = value;
    Ok((signing, GuarantorKey::new(id, public, true)))
}

fn attestation(
    id: [u8; 32],
    network: u32,
    key: &GuarantorKey,
    signing: &SigningKey,
) -> Checked<Attestation> {
    let guarantor_id = key.guarantor_id();
    let attested_at = HEADER_TIME_MS + u64::from(guarantor_id[0]);
    let mut message = [0u8; 189];
    message[..2].copy_from_slice(&PROTOCOL_VERSION.to_be_bytes());
    message[2..6].copy_from_slice(&network.to_be_bytes());
    message[6..14].copy_from_slice(&PAXEER_CHAIN.to_be_bytes());
    message[14..34].copy_from_slice(&CONTRACT);
    message[34..42].copy_from_slice(&CHECKPOINT_EPOCH.to_be_bytes());
    message[42..74].copy_from_slice(&id);
    message[74..106].copy_from_slice(&id);
    message[106..138].copy_from_slice(&guarantor_id);
    message[138..146].copy_from_slice(&BATCH.to_be_bytes());
    message[146..178].copy_from_slice(&DA_ROOT);
    message[178] = 1;
    message[179] = 1;
    message[180] = 0x1f;
    message[181..].copy_from_slice(&attested_at.to_be_bytes());
    let prehash = checkpoint_attestation_digest(&message)?;
    let (signature, recovery): (Signature, _) = signing.sign_prehash_recoverable(&prehash)?;
    let signer = secp256k1::evm_address(&key.public_key())?;
    Ok(Attestation::new(
        PROTOCOL_VERSION,
        network,
        PAXEER_CHAIN,
        CONTRACT,
        CHECKPOINT_EPOCH,
        id,
        id,
        guarantor_id,
        BATCH,
        DA_ROOT,
        true,
        true,
        0x1f,
        attested_at,
        signer,
        signature.to_bytes().into(),
        27 + u8::from(recovery),
    ))
}

fn authority() -> Checked<FinalityAuthority> {
    Ok(FinalityAuthority {
        network_id: NETWORK,
        settlement: SettlementDomain::new(PAXEER_CHAIN, CONTRACT),
        guarantors: (1..=3)
            .map(|v| guarantor(v).map(|(_, key)| key))
            .collect::<Checked<_>>()?,
    })
}

/// Two-of-three threshold certificate for a checkpoint whose header commits `root` at
/// `sequence`; returns it with its registered identifier.
fn certificate(root: u8, sequence: u64, network: u32) -> Checked<(Certificate, [u8; 32])> {
    let checkpoint = Checkpoint::new(header([root; 32], sequence, network)?, b"PROOF".to_vec());
    let id = checkpoint_id(&checkpoint)?;
    let mut attestations = Vec::new();
    for value in 1..=3 {
        let (signing, key) = guarantor(value)?;
        attestations.push(attestation(id, network, &key, &signing)?);
    }
    Ok((Certificate::new(checkpoint, attestations, 2, None), id))
}

fn observe(
    store: &ProjectionStore,
    state_bytes: &[u8],
    root: u8,
    sequence: u64,
    activity: u8,
) -> Checked<SnapshotRecord> {
    let (bytes, facts) = capture(state_bytes, root, sequence)?;
    Ok(store.observe(&Observation {
        state: &bytes,
        facts,
        read: ReadVerification::SequencerSigned,
        source_activity: digest(activity)?,
        observed_at_ms: NOW_MS,
    })?)
}

fn promote_with(
    store: &ProjectionStore,
    record: &SnapshotRecord,
    root: u8,
    sequence: u64,
    network: u32,
) -> Result<SnapshotRecord, ViewError> {
    let (certificate, id) =
        certificate(root, sequence, network).map_err(|e| ViewError::Store(format!("{e:?}")))?;
    let authority = authority().map_err(|e| ViewError::Store(format!("{e:?}")))?;
    store.promote(
        &record.snapshot_id,
        &CheckpointProof {
            certificate: &certificate,
            registered_checkpoint_id: id,
            registered_settlement_reference: None,
        },
        &authority,
        PUBLISHED_AT_MS,
    )
}

fn finalize(store: &ProjectionStore, record: &SnapshotRecord) -> Checked<SnapshotRecord> {
    let root = record.native_state_root.as_bytes()[0];
    Ok(promote_with(
        store,
        record,
        root,
        record.observed_sequence,
        NETWORK,
    )?)
}

fn scratch(name: &str) -> Checked<PathBuf> {
    let dir = PathBuf::from(env!("CARGO_TARGET_TMPDIR"))
        .join("ai_snapshot_minimal")
        .join(name);
    if dir.exists() {
        fs::remove_dir_all(&dir)?;
    }
    fs::create_dir_all(&dir)?;
    Ok(dir)
}

fn viewer(name: &str) -> Checked<Viewer> {
    Ok(Viewer::for_principal(name)?)
}

fn page_request(query: &str) -> Checked<PageRequest> {
    Ok(PageRequest::parse(&market_hex()?, Some(query))?)
}

#[test]
fn a01_pages_stay_pinned_to_one_snapshot_across_a_newer_root() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let keys = CursorKeyring::new(1, SECRET);
    let alice = viewer("principal-a")?;
    let first = finalize(
        &store,
        &observe(&store, &market_with_workers(2, 1)?, 0xA1, 900, 1)?,
    )?;
    let page1 = store.participants(&alice, &keys, &page_request("limit=1")?, NOW_MS)?;
    assert_eq!(page1.snapshot_id, first.snapshot_id);
    assert_eq!(page1.rows.len(), 1);
    let cursor = page1.cursor.clone().ok_or("first page cursor")?;

    let second = finalize(
        &store,
        &observe(&store, &market_with_workers(3, 40)?, 0xB2, 901, 2)?,
    )?;
    assert_ne!(second.snapshot_id, first.snapshot_id);
    let latest = store.market_view(&alice, &market()?, None, None)?;
    assert_eq!(latest.snapshot_id, second.snapshot_id);

    let page2 = store.participants(
        &alice,
        &keys,
        &page_request(&format!("limit=1&cursor={cursor}"))?,
        NOW_MS + 1,
    )?;
    assert_eq!(page2.snapshot_id, first.snapshot_id);
    assert_eq!(page2.binding.native_state_root, digest(0xA1)?);
    assert_eq!(page2.binding.observed_sequence, 900);
    assert_eq!(page2.rows.len(), 1);
    assert!(page2.cursor.is_none());
    let (w1, w2) = (page1.rows[0].id, page2.rows[0].id);
    assert!(w1 < w2, "kind-then-id order, each worker exactly once");

    let before = store.stream(&market()?)?;
    let refiltered = page_request(&format!("kind=worker&limit=1&cursor={cursor}"))?;
    assert_eq!(
        store
            .participants(&alice, &keys, &refiltered, NOW_MS + 2)
            .err(),
        Some(ViewError::CursorMismatch)
    );
    let replay = page_request(&format!("limit=1&cursor={cursor}"))?;
    assert_eq!(
        store
            .participants(&viewer("principal-b")?, &keys, &replay, NOW_MS + 2)
            .err(),
        Some(ViewError::CursorMismatch)
    );
    let pinned_elsewhere = page_request(&format!(
        "snapshot={}&limit=1&cursor={cursor}",
        hex(second.snapshot_id.as_bytes())
    ))?;
    assert_eq!(
        store
            .participants(&alice, &keys, &pinned_elsewhere, NOW_MS + 2)
            .err(),
        Some(ViewError::CursorMismatch)
    );
    assert_eq!(store.stream(&market()?)?, before);
    assert_eq!(store.snapshot(&first.snapshot_id)?, Some(first));
    Ok(())
}

#[test]
fn a02_only_a_rank4_checkpoint_for_the_same_root_finalizes() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let alice = viewer("principal-a")?;
    let record = observe(&store, &market_with_workers(2, 1)?, 0xA1, 900, 1)?;
    assert_eq!(
        record.projection,
        ProjectionState::EvidenceVerifiedUnfinalized
    );
    assert_eq!(record.binding, None);
    assert_eq!(
        store.market_view(&alice, &market()?, None, None).err(),
        Some(ViewError::FinalityUnavailable)
    );
    assert_eq!(
        store
            .market_view(&alice, &market()?, Some(&record.snapshot_id), None)
            .err(),
        Some(ViewError::FinalityUnavailable)
    );
    assert!(store.pending_events(&market()?, 16)?.is_empty());

    assert_eq!(
        promote_with(&store, &record, 0xB2, 900, NETWORK).err(),
        Some(ViewError::BindingMismatch)
    );
    assert_eq!(
        promote_with(&store, &record, 0xA1, 899, NETWORK).err(),
        Some(ViewError::BindingMismatch)
    );
    let (certificate, id) = certificate(0xA1, 900, NETWORK)?;
    let mut wrong_id = id;
    wrong_id[0] ^= 1;
    assert_eq!(
        store
            .promote(
                &record.snapshot_id,
                &CheckpointProof {
                    certificate: &certificate,
                    registered_checkpoint_id: wrong_id,
                    registered_settlement_reference: None,
                },
                &authority()?,
                PUBLISHED_AT_MS,
            )
            .err(),
        Some(ViewError::FinalityUnavailable)
    );
    assert_eq!(
        store.snapshot(&record.snapshot_id)?.map(|r| r.projection),
        Some(ProjectionState::EvidenceVerifiedUnfinalized)
    );
    assert!(store.pending_events(&market()?, 16)?.is_empty());

    let finalized = finalize(&store, &record)?;
    assert_eq!(finalized.projection, ProjectionState::FinalizedPublishable);
    assert_eq!(finalized.rank, MINIMUM_PUBLICATION_RANK);
    assert_eq!(finalized.checkpoint, Some(Digest32::new(id)?));
    assert_eq!(finalize(&store, &record)?, finalized);
    let events = store.pending_events(&market()?, 16)?;
    assert_eq!(events.len(), 1);
    assert_eq!(events[0].snapshot_id, record.snapshot_id);
    assert_eq!(events[0].rank, 4);
    assert_eq!(Some(&events[0].binding), finalized.binding.as_ref());
    let view = store.market_view(&alice, &market()?, None, None)?;
    assert_eq!(view.binding.checkpoint, Digest32::new(id)?);
    assert_eq!(view.binding.rank, 4);
    assert_eq!(view.binding.settlement, Presence::Absent);
    assert_eq!(view.availability[5], Availability::NotEnabled);
    assert_eq!(view.availability[2], Availability::NotYetProduced);
    Ok(())
}

#[test]
fn a04_bounded_pages_and_refusals_before_row_work() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let keys = CursorKeyring::new(1, SECRET);
    let alice = viewer("principal-a")?;
    finalize(
        &store,
        &observe(&store, &market_with_workers(32, 1)?, 0xA1, 900, 1)?,
    )?;
    let all = store.participants(&alice, &keys, &page_request("limit=32")?, NOW_MS)?;
    assert_eq!(all.rows.len(), 32);
    assert!(all.cursor.is_none());
    assert!(all.encoded_bytes <= 65_536);
    assert!(all
        .rows
        .windows(2)
        .all(|p| (p[0].kind, p[0].id) < (p[1].kind, p[1].id)));
    assert!(all.rows.iter().all(|r| r.kind == ParticipantKind::Worker));

    let half = store.participants(&alice, &keys, &page_request("")?, NOW_MS)?;
    assert_eq!(half.rows.len(), 16);
    let cursor = half.cursor.ok_or("default-limit cursor")?;
    let rest = store.participants(
        &alice,
        &keys,
        &page_request(&format!("cursor={cursor}"))?,
        NOW_MS,
    )?;
    assert_eq!(rest.rows.len(), 16);
    assert!(rest.cursor.is_none());
    assert_eq!(
        half.rows
            .iter()
            .chain(&rest.rows)
            .copied()
            .collect::<Vec<_>>(),
        all.rows
    );

    let m = market_hex()?;
    for query in [
        "limit=0",
        "limit=33",
        "limit=01",
        "kind=robot",
        "limit=1&limit=2",
        "page=1",
    ] {
        assert_eq!(
            PageRequest::parse(&m, Some(query)).err(),
            Some(ViewError::InvalidEncoding),
            "{query}"
        );
    }
    assert_eq!(
        PageRequest::parse(&format!("{m}0"), None).err(),
        Some(ViewError::InvalidEncoding)
    );
    assert_eq!(
        PageRequest::parse(&m, Some(&format!("cursor={}", "a".repeat(1025)))).err(),
        Some(ViewError::ResponseTooLarge)
    );
    let mut forged = cursor.into_bytes();
    forged[2 * 130 + 3] = if forged[2 * 130 + 3] == b'9' {
        b'8'
    } else {
        b'9'
    };
    let forged = String::from_utf8(forged).map_err(|_| "utf8")?;
    assert_eq!(
        store
            .participants(
                &alice,
                &keys,
                &page_request(&format!("cursor={forged}"))?,
                NOW_MS
            )
            .err(),
        Some(ViewError::CursorMismatch)
    );

    let empty = store.participants(&alice, &keys, &page_request("kind=evaluator")?, NOW_MS)?;
    assert!(empty.rows.is_empty());
    assert!(empty.cursor.is_none());

    Ok(())
}

#[test]
fn a04_rate_limit_is_per_principal_and_disabled_without_configuration() -> Checked {
    let alice = viewer("principal-a")?;
    let limiter = QueryLimiter::new(Some(RateConfig {
        per_minute: 60,
        burst: 2,
        concurrent: 1,
    }));
    let held = limiter.admit(&alice, NOW_MS)?;
    assert_eq!(
        limiter.admit(&alice, NOW_MS).err(),
        Some(ViewError::RateLimited)
    );
    drop(held);
    drop(limiter.admit(&alice, NOW_MS)?);
    assert_eq!(
        limiter.admit(&alice, NOW_MS).err(),
        Some(ViewError::RateLimited)
    );
    drop(limiter.admit(&viewer("principal-b")?, NOW_MS)?);
    drop(limiter.admit(&alice, NOW_MS + 1_000)?);
    assert_eq!(
        QueryLimiter::new(None).admit(&alice, NOW_MS).err(),
        Some(ViewError::EndpointDisabled)
    );
    Ok(())
}

#[test]
fn a05_outbox_survives_restart_and_acknowledges_idempotently() -> Checked {
    let dir = scratch("a05")?;
    let path = dir.join("projection.sqlite");
    let (snapshot, event) = {
        let store = ProjectionStore::open(&path)?;
        let record = finalize(
            &store,
            &observe(&store, &market_with_workers(2, 1)?, 0xA1, 900, 1)?,
        )?;
        let pending = store.pending_events(&market()?, 16)?;
        assert_eq!(pending.len(), 1);
        (record, pending[0].clone())
    };
    let store = ProjectionStore::open(&path)?;
    assert_eq!(
        store.snapshot(&snapshot.snapshot_id)?,
        Some(snapshot.clone())
    );
    let replayed = store.pending_events(&market()?, 16)?;
    assert_eq!(replayed, vec![event.clone()]);
    assert_eq!(event.subject_sequence, 1);
    assert!(event.event_id.starts_with("0x") && event.event_id.len() == 66);

    let mut changed = event.body.clone();
    changed[0] ^= 1;
    assert_eq!(
        store
            .mark_published(&event.event_id, &changed, NOW_MS)
            .err(),
        Some(ViewError::SnapshotConflict)
    );
    assert!(store.mark_published(&event.event_id, &event.body, NOW_MS)?);
    assert!(!store.mark_published(&event.event_id, &event.body, NOW_MS + 1)?);
    assert!(store.pending_events(&market()?, 16)?.is_empty());

    let next = observe(&store, &market_with_workers(3, 40)?, 0xB2, 901, 2)?;
    let unfinalized = event_id(market()?, digest(2)?, 0, SNAPSHOT_EVENT_TAG);
    assert_eq!(
        store.mark_published(&unfinalized, &[], NOW_MS).err(),
        Some(ViewError::FinalityUnavailable)
    );
    assert!(store.pending_events(&market()?, 16)?.is_empty());
    finalize(&store, &next)?;
    let second = store.pending_events(&market()?, 16)?;
    assert_eq!(second.len(), 1);
    assert_eq!(second[0].subject_sequence, 2);
    assert_ne!(second[0].event_id, event.event_id);
    Ok(())
}

#[test]
fn a06_rollback_removes_provisional_descendants_and_refuses_below_finality() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let finalized = finalize(
        &store,
        &observe(&store, &market_with_workers(2, 1)?, 0xA1, 900, 1)?,
    )?;
    let child = observe(&store, &market_with_workers(3, 40)?, 0xB2, 901, 2)?;
    let grandchild = observe(&store, &market_with_workers(4, 80)?, 0xC3, 902, 3)?;
    assert_eq!(store.stream(&market()?)?.map(|s| s.next_subject), Some(4));
    assert!(!store.committed_rows(&child.snapshot_id)?.is_empty());

    assert_eq!(store.rollback(&market()?, 900)?, 2);
    assert_eq!(store.snapshot(&child.snapshot_id)?, None);
    assert_eq!(store.snapshot(&grandchild.snapshot_id)?, None);
    assert!(store.committed_rows(&child.snapshot_id)?.is_empty());
    let stream = store.stream(&market()?)?.ok_or("stream")?;
    assert_eq!(stream.observed_sequence, 900);
    assert_eq!(stream.observed_root, Some(digest(0xA1)?));
    assert_eq!(stream.next_subject, 2);
    assert_eq!(stream.finalized_snapshot, Some(finalized.snapshot_id));
    assert_eq!(store.pending_events(&market()?, 16)?.len(), 1);

    let replacement = observe(&store, &market_with_workers(3, 40)?, 0xD4, 901, 4)?;
    assert_eq!(
        store.stream(&market()?)?.map(|s| s.observed_sequence),
        Some(901)
    );
    assert_ne!(replacement.snapshot_id, child.snapshot_id);

    assert_eq!(
        store.rollback(&market()?, 899).err(),
        Some(ViewError::RollbackRefused)
    );
    let stream = store.stream(&market()?)?.ok_or("stream")?;
    assert_eq!(stream.quarantine.as_deref(), Some("rollback-refused"));
    assert_eq!(
        store.snapshot(&finalized.snapshot_id)?,
        Some(finalized.clone())
    );
    assert!(store.pending_events(&market()?, 16)?.is_empty());
    assert_eq!(
        store.alerts()?,
        vec![Alert {
            market: market()?.bytes(),
            subject: finalized.snapshot_id.bytes(),
            category: "rollback-refused".to_owned(),
        }]
    );
    assert_eq!(
        store.rollback(&market()?, 900).err(),
        Some(ViewError::Quarantined)
    );
    assert!(matches!(
        observe(&store, &market_with_workers(5, 90)?, 0xE5, 903, 5),
        Err(Failure::View(ViewError::Quarantined))
    ));
    Ok(())
}

#[test]
fn a09_archived_content_is_typed_and_restored_only_by_digest() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let keys = CursorKeyring::new(1, SECRET);
    let alice = viewer("principal-a")?;
    let old_state = market_with_workers(2, 1)?;
    let old = finalize(&store, &observe(&store, &old_state, 0xA1, 900, 1)?)?;
    let new = finalize(
        &store,
        &observe(&store, &market_with_workers(2, 60)?, 0xB2, 901, 2)?,
    )?;
    let pinned = page_request(&format!(
        "snapshot={}&limit=1",
        hex(old.snapshot_id.as_bytes())
    ))?;
    assert!(store
        .participants(&alice, &keys, &pinned, NOW_MS)?
        .cursor
        .is_some());
    assert_eq!(
        store.archive(&old.snapshot_id, NOW_MS).err(),
        Some(ViewError::CapacityExceeded)
    );
    assert_eq!(
        store.archive(&new.snapshot_id, NOW_MS).err(),
        Some(ViewError::ProjectionUnavailable)
    );
    let later = NOW_MS + 900_001;
    store.archive(&old.snapshot_id, later)?;
    let archived = store.snapshot(&old.snapshot_id)?.ok_or("archived")?;
    assert_eq!(archived.projection, ProjectionState::Archived);
    assert!(!archived.retained);
    assert_eq!(archived.binding, old.binding);
    let pruned = Some(ViewError::SnapshotPruned {
        oldest: Some((new.snapshot_id, 901)),
    });
    assert_eq!(
        store.participants(&alice, &keys, &pinned, later).err(),
        pruned
    );
    assert_eq!(
        store
            .market_view(&alice, &market()?, Some(&old.snapshot_id), None)
            .err(),
        pruned
    );

    let (genuine, _) = capture(&old_state, 0xA1, 900)?;
    let mut off_by_one = genuine.clone();
    let last = off_by_one.last_mut().ok_or("empty state")?;
    *last ^= 1;
    assert_eq!(
        store.restore_archived(&old.snapshot_id, &off_by_one).err(),
        Some(ViewError::IntegrityFailure)
    );
    assert_eq!(store.snapshot(&old.snapshot_id)?, Some(archived));
    assert_eq!(
        store.alerts()?.last().map(|a| a.category.clone()),
        Some("integrity-failure".to_owned())
    );
    store.restore_archived(&old.snapshot_id, &genuine)?;
    let restored = store.market_view(&alice, &market()?, Some(&old.snapshot_id), None)?;
    assert_eq!(restored.binding.native_state_root, digest(0xA1)?);

    let history = store.epochs(&market()?, 4, 2)?;
    assert_eq!(history.component, Availability::NotYetProduced);
    assert!(history.entries.is_empty());
    assert_eq!(
        store.epochs(&market()?, 4, 0).err(),
        Some(ViewError::InvalidEncoding)
    );

    let all = page_request(&format!("snapshot={}", hex(old.snapshot_id.as_bytes())))?;
    let previous = store.participants(&alice, &keys, &all, later)?.rows;
    let current = store
        .participants(&alice, &keys, &page_request("")?, later)?
        .rows;
    assert_eq!((previous.len(), current.len()), (2, 2));
    assert!(current
        .iter()
        .all(|row| previous.iter().all(|old| old.id != row.id)));
    assert!(current
        .iter()
        .all(|row| row.history == Presence::Absent
            && row.history_status == Availability::NotYetProduced));
    Ok(())
}

#[test]
fn a14_stale_authority_fails_closed_while_history_stays_labelled() -> Checked {
    let store = ProjectionStore::open_in_memory()?;
    let alice = viewer("principal-a")?;
    let record = finalize(
        &store,
        &observe(&store, &market_with_workers(2, 1)?, 0xA1, 900, 1)?,
    )?;
    assert_eq!(record.execution_height, 1010);
    let stale = record.execution_height + AUTHORITY_FRESHNESS_HEIGHTS + 1;
    assert_eq!(
        store.require_fresh_authority(&market()?, stale).err(),
        Some(ViewError::AuthorityStale { lag: 9 })
    );
    let view = store.market_view(&alice, &market()?, None, Some(stale))?;
    assert_eq!(view.freshness, Freshness::Stale { lag: 9 });
    assert_eq!(view.snapshot_id, record.snapshot_id);
    assert_eq!(view.binding.epoch, Presence::Absent);
    let live = record.execution_height + AUTHORITY_FRESHNESS_HEIGHTS;
    assert_eq!(store.require_fresh_authority(&market()?, live)?, record);
    assert_eq!(
        store
            .market_view(&alice, &market()?, None, Some(live))?
            .freshness,
        Freshness::Current
    );
    assert_eq!(store.snapshot(&record.snapshot_id)?, Some(record));
    Ok(())
}

#[test]
fn a15_conflicts_refuse_or_quarantine_with_id_only_alerts() -> Checked {
    let dir = scratch("a15-conflict")?;
    let path = dir.join("projection.sqlite");
    let store = ProjectionStore::open(&path)?;
    let s = market_with_workers(2, 1)?;
    let record = observe(&store, &s, 0xA1, 900, 1)?;
    assert_eq!(observe(&store, &s, 0xA1, 900, 1)?, record);
    let (mut tampered, _) = capture(&s, 0xA1, 900)?;
    let last = tampered.last_mut().ok_or("empty state")?;
    *last ^= 1;
    rusqlite::Connection::open(&path)?.execute(
        "UPDATE ai_snapshot SET state = ?1 WHERE snapshot_id = ?2",
        rusqlite::params![tampered, record.snapshot_id.as_bytes().as_slice()],
    )?;
    assert!(matches!(
        observe(&store, &s, 0xA1, 900, 1),
        Err(Failure::View(ViewError::SnapshotConflict))
    ));
    assert_eq!(
        store.stream(&market()?)?.and_then(|s| s.quarantine),
        Some("snapshot-conflict".to_owned())
    );
    assert!(matches!(
        finalize(&store, &record),
        Err(Failure::View(ViewError::Quarantined))
    ));
    let conflict = Alert {
        market: market()?.bytes(),
        subject: record.snapshot_id.bytes(),
        category: "snapshot-conflict".to_owned(),
    };
    assert_eq!(store.alerts()?, vec![conflict]);

    let events = ProjectionStore::open_in_memory()?;
    observe(&events, &s, 0xA1, 900, 1)?;
    assert!(matches!(
        observe(&events, &market_with_workers(3, 40)?, 0xB2, 901, 1),
        Err(Failure::View(ViewError::SnapshotConflict))
    ));
    let alert = events.alerts()?.pop().ok_or("event conflict alert")?;
    assert_eq!(alert.category, "snapshot-conflict");
    assert_eq!(events.snapshot(&Digest32::new(alert.subject)?)?, None);
    assert_eq!(
        events.stream(&market()?)?.map(|s| s.observed_sequence),
        Some(900)
    );

    let foreign = ProjectionStore::open_in_memory()?;
    let pending = observe(&foreign, &s, 0xA1, 900, 1)?;
    assert_eq!(
        promote_with(&foreign, &pending, 0xA1, 900, NETWORK + 1).err(),
        Some(ViewError::WrongDomain)
    );
    let (_, foreign_id) = certificate(0xA1, 900, NETWORK + 1)?;
    assert_eq!(
        foreign.alerts()?,
        vec![Alert {
            market: market()?.bytes(),
            subject: foreign_id,
            category: "wrong-domain".to_owned(),
        }]
    );
    assert_eq!(foreign.stream(&market()?)?.and_then(|s| s.quarantine), None);
    assert_eq!(
        foreign.snapshot(&pending.snapshot_id)?,
        Some(pending.clone())
    );
    assert!(foreign.pending_events(&market()?, 16)?.is_empty());

    let versions = ProjectionStore::open_in_memory()?;
    let (mut future, facts) = capture(&s, 0xA1, 900)?;
    future[7] = 2;
    assert_eq!(
        versions
            .observe(&Observation {
                state: &future,
                facts,
                read: ReadVerification::SequencerSigned,
                source_activity: digest(1)?,
                observed_at_ms: NOW_MS,
            })
            .err(),
        Some(ViewError::UnsupportedVersion)
    );
    assert_eq!(
        versions.alerts()?,
        vec![Alert {
            market: market()?.bytes(),
            subject: facts.digest.bytes(),
            category: "unsupported-version".to_owned(),
        }]
    );
    assert!(matches!(
        observe(&versions, &s, 0xA1, 900, 1),
        Err(Failure::View(ViewError::Quarantined))
    ));
    Ok(())
}

#[test]
fn a15_restored_database_reproduces_identical_snapshots() -> Checked {
    let dir = scratch("a15-restore")?;
    let original = dir.join("projection.sqlite");
    let keys = CursorKeyring::new(1, SECRET);
    let alice = viewer("principal-a")?;
    let snapshot_of = |store: &ProjectionStore, id: &Digest32| -> Checked<_> {
        Ok((
            store.snapshot(id)?,
            store.committed_rows(id)?,
            store.stream(&market()?)?,
            store.pending_events(&market()?, 16)?,
            store.market_view(&alice, &market()?, None, None)?,
            store.participants(&alice, &keys, &page_request("limit=32")?, NOW_MS)?,
        ))
    };
    let (id, before) = {
        let store = ProjectionStore::open(&original)?;
        let record = finalize(
            &store,
            &observe(&store, &market_with_workers(4, 1)?, 0xA1, 900, 1)?,
        )?;
        observe(&store, &market_with_workers(3, 40)?, 0xB2, 901, 2)?;
        (
            record.snapshot_id,
            snapshot_of(&store, &record.snapshot_id)?,
        )
    };
    let restored_dir = scratch("a15-restored")?;
    let restored = restored_dir.join("projection.sqlite");
    fs::copy(&original, &restored)?;
    let wal = dir.join("projection.sqlite-wal");
    if wal.exists() {
        fs::copy(&wal, restored_dir.join("projection.sqlite-wal"))?;
    }
    let store = ProjectionStore::open(&restored)?;
    let after = snapshot_of(&store, &id)?;
    assert_eq!(after, before);
    assert_eq!(after.3.len(), 1);
    assert!(store.alerts()?.is_empty());
    Ok(())
}
