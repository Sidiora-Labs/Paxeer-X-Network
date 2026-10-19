use ed25519_dalek::SigningKey;
use k256::ecdsa::{Signature, SigningKey as GuarantorSigningKey};
use layerx_crypto::secp256k1;
use layerx_platform_webhooks::ai_market::{
    market_event_id, Admission, CapturedEvent, CheckpointProof, Consumed, CursorKeys, Evidence,
    FinalityPolicy, MarketConsumer, MarketEventError, MarketOutbox, OperatorAlert, ProducerGrant,
    QuarantineCause, ResumePage, SignedDelivery, UpstreamManifest,
};
use layerx_platform_webhooks::encoding::hex_encode;
use layerx_platform_webhooks::trusted::{OPERATOR_ROLE, PRODUCER_ROLE};
use layerx_platform_webhooks::{
    EndpointId, EventKind, Presentation, Principal, ProtocolEvent, SubjectId, Verification,
    WebhookError,
};
use layerx_programs_ai_market::codec::{encode_state, state_digest, StateFrame};
use layerx_programs_ai_market::queries::SnapshotBinding;
use layerx_programs_ai_market::{
    ChainDomain, Digest32, MarketId, PolicyDigest, Presence, ProgramId, RosterDigest, Version,
};
use layerx_proof::checkpoint::{
    checkpoint_id, Attestation, Certificate, Checkpoint, GuarantorKey, SettlementDomain,
};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{checkpoint_attestation_digest, program_execution_batch_id};
use layerx_wire::limits::PROTOCOL_VERSION;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use std::fmt::Debug;
use std::fs;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const BINARY: &str = env!("CARGO_BIN_EXE_layerx-webhooks");
const CHILD: &str = "PAXAI_CONSUMER_CHILD";
const CHAIN: [u8; 32] = [0x11; 32];
const PROGRAM: [u8; 32] = [0x22; 32];
const MARKET: [u8; 32] = [0x33; 32];
const NETWORK: u32 = 42;
const SETTLEMENT_CHAIN: u64 = 31_337;
const SETTLEMENT_CONTRACT: [u8; 20] = [0x55; 20];
const CHECKPOINT_EPOCH: u64 = 7;
const BATCH_NUMBER: u64 = 8;
const PREVIOUS_ROOT: [u8; 32] = [0x44; 32];
const ACTIVITY_ROOT: [u8; 32] = [0x45; 32];
const AVAILABILITY_ROOT: [u8; 32] = [0x48; 32];
const BONDED: [u8; 3] = [1, 2, 3];
const SIGNED: [(u8, u8); 2] = [(1, 1), (2, 2)];
const THRESHOLD: usize = 2;
const TAG: u16 = 0x0401;
const KEY_ID: &str = "whk_paxai";
const LOCATOR: &str = "locator:private/result/7f3a";
const MARKUP: &str = "<script>alert(1)</script>";

fn ok<T, E: Debug>(value: Result<T, E>, context: &str) -> T {
    value.unwrap_or_else(|error| panic!("{context}: {error:?}"))
}

struct Scratch(PathBuf);

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn scratch(label: &str) -> Scratch {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |duration| duration.as_nanos());
    let directory = std::env::temp_dir().join(format!(
        "paxai-webhooks-{label}-{}-{nanos}",
        std::process::id()
    ));
    ok(fs::create_dir_all(&directory), "scratch directory");
    Scratch(directory)
}

fn text(value: &Path) -> String {
    value
        .to_str()
        .unwrap_or_else(|| panic!("test path must be UTF-8"))
        .to_owned()
}

fn run(program: &str, arguments: &[&str]) {
    let output = ok(
        Command::new(program).args(arguments).output(),
        "tool must run",
    );
    assert!(
        output.status.success(),
        "{program} {arguments:?} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |duration| duration.as_secs())
}

fn header(network: u32, root: [u8; 32], last_sequence: u64) -> Vec<u8> {
    let mut encoder = Encoder::new(354);
    ok(
        encoder.structure_header_version(0x1701, PROTOCOL_VERSION),
        "header version",
    );
    ok(encoder.u8(15), "header fields");
    ok(encoder.tag(1, 15), "tag");
    ok(encoder.u16(PROTOCOL_VERSION), "protocol version");
    ok(encoder.tag(2, 15), "tag");
    ok(encoder.u32(network), "network");
    for (field, value) in [
        (3_u8, CHECKPOINT_EPOCH),
        (4, BATCH_NUMBER),
        (5, last_sequence),
        (6, last_sequence),
    ] {
        ok(encoder.tag(field, 15), "tag");
        ok(encoder.u64(value), "scalar");
    }
    for (field, value) in [
        (7_u8, PREVIOUS_ROOT),
        (8, root),
        (9, ACTIVITY_ROOT),
        (10, [0x46; 32]),
        (11, [0x47; 32]),
        (12, AVAILABILITY_ROOT),
        (13, [0x49; 32]),
    ] {
        ok(encoder.tag(field, 15), "tag");
        ok(encoder.bytes(&value, 32), "root");
    }
    ok(encoder.tag(14, 15), "tag");
    ok(encoder.u64(1_000), "timestamp");
    ok(encoder.tag(15, 15), "tag");
    ok(encoder.bytes(&[0x4a; 32], 32), "sequencer");
    encoder.finish()
}

fn guarantor(value: u8) -> (GuarantorSigningKey, GuarantorKey) {
    let mut scalar = [0_u8; 32];
    scalar[31] = value;
    let signing = ok(
        GuarantorSigningKey::from_bytes((&scalar).into()),
        "guarantor key",
    );
    let public_key: [u8; 33] = signing
        .verifying_key()
        .to_encoded_point(true)
        .as_bytes()
        .try_into()
        .unwrap_or_else(|_| panic!("compressed key width"));
    let mut identifier = [0_u8; 32];
    identifier[0] = value;
    (signing, GuarantorKey::new(identifier, public_key, true))
}

fn attestation(
    network: u32,
    checkpoint: [u8; 32],
    guarantor_value: u8,
    key_value: u8,
) -> Attestation {
    let (_, record) = guarantor(guarantor_value);
    let (key, _) = guarantor(key_value);
    let signer = ok(
        secp256k1::evm_address(key.verifying_key().to_encoded_point(true).as_bytes()),
        "signer address",
    );
    let build = |signature: [u8; 64], signature_v: u8| {
        Attestation::new(
            PROTOCOL_VERSION,
            network,
            SETTLEMENT_CHAIN,
            SETTLEMENT_CONTRACT,
            CHECKPOINT_EPOCH,
            checkpoint,
            checkpoint,
            record.guarantor_id(),
            BATCH_NUMBER,
            AVAILABILITY_ROOT,
            true,
            true,
            0x1f,
            1_000 + u64::from(guarantor_value),
            signer,
            signature,
            signature_v,
        )
    };
    let digest = ok(
        checkpoint_attestation_digest(&build([0; 64], 27).canonical_statement()),
        "attestation digest",
    );
    let (signature, recovery): (Signature, _) = ok(
        key.sign_prehash_recoverable(&digest),
        "attestation signature",
    );
    build(signature.to_bytes().into(), 27 + u8::from(recovery))
}

/// A certificate over one batch, attested by `(guarantor, signing key)` pairs.
fn certificate(
    network: u32,
    root: [u8; 32],
    last_sequence: u64,
    signers: &[(u8, u8)],
) -> (Certificate, [u8; 32]) {
    let checkpoint = Checkpoint::new(
        header(network, root, last_sequence),
        b"validity-proof".to_vec(),
    );
    let identifier = ok(checkpoint_id(&checkpoint), "checkpoint id");
    let attestations = signers
        .iter()
        .map(|(guarantor_value, key_value)| {
            attestation(network, identifier, *guarantor_value, *key_value)
        })
        .collect();
    (
        Certificate::new(checkpoint, attestations, THRESHOLD, None),
        identifier,
    )
}

fn market_state(revision: u64, section: &[u8]) -> Vec<u8> {
    let frame = StateFrame {
        revision,
        sections: [section, &[], &[], &[], &[], &[]],
    };
    let mut bytes = vec![0; ok(frame.encoded_len(), "state frame length")];
    ok(encode_state(&frame, &mut bytes), "state frame");
    bytes
}

fn digest(bytes: [u8; 32]) -> Digest32 {
    ok(Digest32::new(bytes), "digest")
}

fn snapshot(binding: &SnapshotBinding) -> [u8; 32] {
    ok(binding.snapshot_id(), "snapshot id").bytes()
}

type Change = fn(&mut Fixture);

struct Fixture {
    row: CapturedEvent,
    state: Vec<u8>,
    certificate: Certificate,
    registered: [u8; 32],
}

impl Fixture {
    fn evidence(&self) -> Evidence<'_> {
        Evidence {
            state: &self.state,
            proof: Some(CheckpointProof {
                certificate: &self.certificate,
                registered_checkpoint_id: self.registered,
                registered_settlement_reference: None,
            }),
        }
    }

    fn reseal(&mut self) {
        self.row.snapshot_id = snapshot(&self.row.binding);
    }

    fn recertify(
        &mut self,
        network: u32,
        root: [u8; 32],
        last_sequence: u64,
        signers: &[(u8, u8)],
    ) {
        let (certificate, identifier) = certificate(network, root, last_sequence, signers);
        self.certificate = certificate;
        self.registered = identifier;
        self.row.binding.checkpoint = digest(identifier);
    }
}

fn root(observed: u64) -> [u8; 32] {
    let mut value = [0x90; 32];
    value[..8].copy_from_slice(&observed.to_be_bytes());
    value
}

fn batch(observed: u64) -> [u8; 32] {
    ok(
        program_execution_batch_id(
            PREVIOUS_ROOT,
            ACTIVITY_ROOT,
            observed,
            observed,
            BATCH_NUMBER,
        ),
        "batch id",
    )
}

fn binding(observed: u64, state: &[u8], checkpoint: [u8; 32], rank: u8) -> SnapshotBinding {
    SnapshotBinding {
        chain: ok(ChainDomain::new(CHAIN), "chain"),
        program: ok(ProgramId::new(PROGRAM), "program"),
        market: ok(MarketId::new(MARKET), "market"),
        observed_sequence: observed,
        execution_height: observed + 100,
        batch_id: digest(batch(observed)),
        native_state_root: digest(root(observed)),
        revision: observed,
        state_digest: ok(state_digest(state), "state digest"),
        epoch: Presence::Present(3),
        config: ok(Version::new(2), "config"),
        policy: ok(PolicyDigest::new([0xab; 32]), "policy"),
        roster: Presence::Present(ok(RosterDigest::new([0xcd; 32]), "roster")),
        checkpoint: digest(checkpoint),
        settlement: Presence::Absent,
        rank,
        publication_time_ms: 1_700_000_000_000 + observed,
    }
}

fn fixture(observed: u64, ordinal: u16, rank: u8) -> Fixture {
    let state = market_state(observed, format!("market-state-{observed}").as_bytes());
    let (certificate, registered) = certificate(NETWORK, root(observed), observed, &SIGNED);
    let binding = binding(observed, &state, registered, rank);
    let mut activity = [0x5c; 32];
    activity[24..].copy_from_slice(&observed.to_be_bytes());
    let event_id = ok(
        market_event_id(&MARKET, &activity, ordinal, TAG),
        "event id",
    );
    Fixture {
        row: CapturedEvent {
            snapshot_id: snapshot(&binding),
            binding,
            event_id: event_id.as_str().to_owned(),
            decoder_version: 1,
            action: "COMMIT_TASK_RESULT".to_owned(),
            ai_event_tag: TAG,
            source_activity_id: activity,
            effect_ordinal: ordinal,
            receipt_digest: [0xef; 32],
            manifest: UpstreamManifest {
                display_name: Some(MARKUP.to_owned()),
                result_locator: Some(LOCATOR.to_owned()),
            },
        },
        state,
        certificate,
        registered,
    }
}

fn principal() -> Principal {
    ok(Principal::new("tenant:paxai-markets"), "principal")
}

fn endpoint() -> EndpointId {
    ok(EndpointId::new("whep_paxai"), "endpoint")
}

fn subject() -> SubjectId {
    ok(
        SubjectId::new(format!("paxai_market_{}", hex_encode(&MARKET))),
        "subject",
    )
}

fn policy() -> FinalityPolicy {
    FinalityPolicy {
        chain_domain: CHAIN,
        network_id: NETWORK,
        settlement: SettlementDomain::new(SETTLEMENT_CHAIN, SETTLEMENT_CONTRACT),
        guarantors: BONDED.iter().map(|value| guarantor(*value).1).collect(),
    }
}

fn open_with(directory: &Path, keys: CursorKeys) -> MarketOutbox {
    ok(
        MarketOutbox::open(directory, principal(), policy(), keys),
        "outbox opens",
    )
}

fn open(directory: &Path) -> MarketOutbox {
    open_with(directory, ok(CursorKeys::new((1, [1; 32]), &[]), "keys"))
}

fn granted(directory: &Path) -> MarketOutbox {
    let outbox = open(directory);
    ok(outbox.grant(&MARKET), "grant");
    outbox
}

fn role_leaf(directory: &Path, label: &str, role: &str, usage: &str) -> Vec<u8> {
    let key = directory.join(format!("{label}-key.pem"));
    let leaf = directory.join(format!("{label}.der"));
    run(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "ed25519",
            "-nodes",
            "-keyout",
            &text(&key),
            "-out",
            &text(&leaf),
            "-outform",
            "DER",
            "-days",
            "1",
            "-subj",
            "/CN=paxai-role",
            "-addext",
            &format!("subjectAltName=URI:{role}"),
            "-addext",
            &format!("extendedKeyUsage={usage}"),
        ],
    );
    ok(fs::read(leaf), "leaf")
}

fn producer(directory: &Path) -> ProducerGrant {
    let leaf = role_leaf(directory, "producer", PRODUCER_ROLE, "clientAuth");
    ok(ProducerGrant::from_certificate(&leaf), "producer grant")
}

fn finalized(admission: Admission) -> ProtocolEvent {
    match admission {
        Admission::Finalized { event, .. } => event,
        Admission::Delayed => panic!("row must be finalized"),
    }
}

fn admit(outbox: &MarketOutbox, grant: &ProducerGrant, fixture: &Fixture) -> ProtocolEvent {
    finalized(ok(
        outbox.admit(grant, &fixture.row, &fixture.evidence()),
        "admission",
    ))
}

fn signer() -> SigningKey {
    SigningKey::from_bytes(&[0x42; 32])
}

fn receiver_keys() -> BTreeMap<String, [u8; 32]> {
    BTreeMap::from([(KEY_ID.to_owned(), signer().verifying_key().to_bytes())])
}

fn consumer(directory: &Path) -> MarketConsumer {
    ok(MarketConsumer::open(directory, receiver_keys()), "consumer")
}

fn deliver(event: &ProtocolEvent, timestamp: u64) -> SignedDelivery {
    ok(
        SignedDelivery::sign(event, &endpoint(), KEY_ID, &signer(), timestamp),
        "signed delivery",
    )
}

fn consume(
    consumer: &MarketConsumer,
    delivery: &SignedDelivery,
    at: u64,
) -> Result<Consumed, WebhookError> {
    consumer.receive(&delivery.presentation(at), |event| {
        format!("credited:{}:{}", event.id(), event.subject_sequence())
    })
}

fn independent_snapshot_id(binding: &SnapshotBinding, frame: &[u8]) -> [u8; 32] {
    let mut bytes = b"PAXAI/view/v1".to_vec();
    bytes.push(0);
    bytes.extend_from_slice(&1_u16.to_be_bytes());
    bytes.extend_from_slice(&CHAIN);
    bytes.extend_from_slice(&PROGRAM);
    bytes.extend_from_slice(&MARKET);
    bytes.extend_from_slice(&binding.observed_sequence.to_be_bytes());
    bytes.extend_from_slice(&binding.execution_height.to_be_bytes());
    bytes.extend_from_slice(&batch(binding.observed_sequence));
    bytes.extend_from_slice(&root(binding.observed_sequence));
    bytes.extend_from_slice(&binding.revision.to_be_bytes());
    let mut state = b"PAXAI/view-state/v1".to_vec();
    state.push(0);
    state.extend_from_slice(frame);
    bytes.extend_from_slice(&Sha256::digest(&state));
    bytes.push(1);
    bytes.extend_from_slice(&3_u64.to_be_bytes());
    bytes.extend_from_slice(&2_u64.to_be_bytes());
    bytes.extend_from_slice(&[0xab; 32]);
    bytes.push(1);
    bytes.extend_from_slice(&[0xcd; 32]);
    Sha256::digest(&bytes).into()
}

fn independent_event_id(fixture: &Fixture) -> String {
    let mut hasher = Sha256::new();
    hasher.update(b"PAXAI/view-event/v1");
    hasher.update([0]);
    hasher.update(MARKET);
    hasher.update(fixture.row.source_activity_id);
    hasher.update(fixture.row.effect_ordinal.to_be_bytes());
    hasher.update(TAG.to_be_bytes());
    format!("0x{}", hex_encode(&hasher.finalize()))
}

#[test]
fn finalized_event_carries_exact_identity_and_public_content_only() {
    let scratch = scratch("identity");
    let outbox = granted(&scratch.0.join("outbox"));
    let grant = producer(&scratch.0);
    let fixture = fixture(10, 0, 4);
    let expected_id = independent_event_id(&fixture);
    assert_eq!(fixture.row.event_id, expected_id);
    assert_eq!(expected_id.len(), 66);
    assert_eq!(
        fixture.row.snapshot_id,
        independent_snapshot_id(&fixture.row.binding, &fixture.state)
    );
    let event = admit(&outbox, &grant, &fixture);
    assert_eq!(event.id().as_str(), expected_id);
    assert_eq!(event.kind(), EventKind::Program);
    assert_eq!(event.subject(), &subject());
    assert_eq!(event.subject_sequence(), 1);
    assert_eq!(event.occurred_at(), 1_700_000_000);
    assert_eq!(event.verification(), Verification::CheckpointFinalised);
    assert_eq!(event.receipt_digest(), Some("ef".repeat(32).as_str()));
    assert!(event
        .facts()
        .iter()
        .all(|fact| fact.verification() == Verification::CheckpointFinalised));
    let facts: BTreeMap<&str, &str> = event
        .facts()
        .iter()
        .map(|fact| (fact.name(), fact.value()))
        .collect();
    assert_eq!(facts.len(), 17);
    assert_eq!(facts["ai_action"], "COMMIT_TASK_RESULT");
    assert_eq!(facts["ai_event_tag"], "1025");
    assert_eq!(facts["market_id"], "33".repeat(32));
    assert_eq!(facts["snapshot_id"], hex_encode(&fixture.row.snapshot_id));
    assert_eq!(facts["epoch"], "3");
    assert_eq!(
        facts["source_activity_id"],
        hex_encode(&fixture.row.source_activity_id)
    );
    assert_eq!(facts["effect_ordinal"], "0");
    assert_eq!(facts["chain_domain"], "11".repeat(32));
    assert_eq!(facts["native_state_root"], hex_encode(&root(10)));
    assert_eq!(facts["checkpoint_id"], hex_encode(&fixture.registered));
    assert_eq!(facts["achieved_rank"], "4");
    let delivery = deliver(&event, now());
    let body = ok(String::from_utf8(delivery.body), "body is UTF-8");
    assert!(body.contains(&expected_id));
    assert!(!body.contains(LOCATOR));
    assert!(!body.contains("<script>"));
    assert!(!body.contains("market-state-"));
}

#[test]
fn missing_finality_delays_publication_and_retries_are_idempotent() {
    let scratch = scratch("delay");
    let outbox = granted(&scratch.0.join("outbox"));
    let grant = producer(&scratch.0);
    let observed = fixture(20, 1, 3);
    assert_eq!(
        ok(
            outbox.admit(&grant, &observed.row, &observed.evidence()),
            "observed row"
        ),
        Admission::Delayed
    );
    assert!(ok(outbox.pending(&MARKET), "pending").is_empty());
    let proven = fixture(20, 1, 4);
    assert_eq!(proven.row.snapshot_id, observed.row.snapshot_id);
    assert_eq!(proven.row.event_id, observed.row.event_id);
    let event = admit(&outbox, &grant, &proven);
    assert_eq!(event.subject_sequence(), 1);
    assert_eq!(
        ok(
            outbox.admit(&grant, &proven.row, &proven.evidence()),
            "retry"
        ),
        Admission::Finalized {
            event: event.clone(),
            duplicate: true
        }
    );
    let second = admit(&outbox, &grant, &fixture(20, 2, 4));
    assert_eq!(second.subject_sequence(), 2);
    assert_eq!(ok(outbox.pending(&MARKET), "pending"), vec![event, second]);
}

fn refused_certificate(
    outbox: &MarketOutbox,
    grant: &ProducerGrant,
    label: &str,
    sample: &Fixture,
) {
    let refusal = outbox.admit(grant, &sample.row, &sample.evidence());
    assert!(
        matches!(refusal, Err(MarketEventError::CertificateRejected)),
        "{label}: {refusal:?}"
    );
}

#[test]
fn bonded_guarantor_signatures_gate_finalized_publication() {
    let scratch = scratch("signatures");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let mut unsigned = fixture(30, 0, 4);
    unsigned.recertify(NETWORK, root(30), 30, &[]);
    refused_certificate(&outbox, &grant, "unsigned", &unsigned);
    let mut forged = fixture(30, 0, 4);
    forged.recertify(NETWORK, root(30), 30, &[(1, 1), (2, 9)]);
    refused_certificate(&outbox, &grant, "wrong key", &forged);
    let mut unbonded = fixture(30, 0, 4);
    unbonded.recertify(NETWORK, root(30), 30, &[(1, 1), (4, 4)]);
    refused_certificate(
        &outbox,
        &grant,
        "guarantor outside the bonded set",
        &unbonded,
    );
    let mut short = fixture(30, 0, 4);
    short.recertify(NETWORK, root(30), 30, &[(1, 1)]);
    refused_certificate(&outbox, &grant, "below threshold", &short);
    let mut unregistered = fixture(30, 0, 4);
    unregistered.registered = [0x5a; 32];
    refused_certificate(&outbox, &grant, "unregistered checkpoint", &unregistered);
    let mut other_batch = fixture(30, 0, 4);
    other_batch.recertify(NETWORK, root(30), 31, &SIGNED);
    refused_certificate(&outbox, &grant, "another batch", &other_batch);
    let mut malformed = fixture(30, 0, 4);
    malformed.certificate = Certificate::new(
        Checkpoint::new(vec![1, 2, 3], b"validity-proof".to_vec()),
        Vec::new(),
        THRESHOLD,
        None,
    );
    refused_certificate(&outbox, &grant, "malformed header", &malformed);
    assert!(ok(outbox.pending(&MARKET), "nothing admitted").is_empty());
    assert_eq!(ok(outbox.quarantine(&MARKET), "quarantine"), None);
    assert!(ok(outbox.alerts(), "alerts").is_empty());
    let signed = fixture(30, 0, 4);
    assert_eq!(signed.certificate.attestations().len(), 2);
    let event = admit(&outbox, &grant, &signed);
    assert_eq!(event.subject_sequence(), 1);
    assert_eq!(event.verification(), Verification::CheckpointFinalised);
    assert_eq!(ok(outbox.pending(&MARKET), "published"), vec![event]);
}

fn publish_three(directory: &Path, grant: &ProducerGrant, first: u64) -> Vec<ProtocolEvent> {
    let outbox = granted(directory);
    (first..first + 3)
        .map(|observed| admit(&outbox, grant, &fixture(observed, 0, 4)))
        .collect()
}

#[test]
fn restart_after_commit_publishes_the_same_events_and_receiver_applies_once() {
    let scratch = scratch("restart");
    let grant = producer(&scratch.0);
    let directory = scratch.0.join("outbox");
    let admitted = publish_three(&directory, &grant, 40);
    let restarted = open(&directory);
    let pending = ok(restarted.pending(&MARKET), "pending after restart");
    assert_eq!(pending, admitted);
    let sequences: Vec<u64> = pending
        .iter()
        .map(ProtocolEvent::subject_sequence)
        .collect();
    assert_eq!(sequences, vec![1, 2, 3]);
    let receiver = consumer(&scratch.0.join("consumer"));
    let at = now();
    let first = deliver(&pending[0], at);
    assert_eq!(first, deliver(&pending[0], at));
    let credited = format!("credited:{}:1", pending[0].id());
    assert_eq!(
        ok(consume(&receiver, &first, at), "first delivery"),
        Consumed::Applied {
            sequence: 1,
            result: credited.clone()
        }
    );
    assert_eq!(
        ok(consume(&receiver, &first, at), "retried delivery"),
        Consumed::Duplicate {
            sequence: 1,
            result: credited
        }
    );
    let other = ok(EndpointId::new("whep_other"), "endpoint");
    let changed = ok(
        SignedDelivery::sign(&pending[0], &other, KEY_ID, &signer(), at),
        "changed body",
    );
    assert!(matches!(
        consume(&receiver, &changed, at),
        Err(WebhookError::EventConflict)
    ));
    let third = deliver(&pending[2], at);
    assert_eq!(
        ok(consume(&receiver, &third, at), "gap"),
        Consumed::Suspended {
            expected: 2,
            received: 3
        }
    );
    assert_eq!(ok(receiver.last_sequence(&subject()), "last"), 1);
    for (index, delivery) in [(2, deliver(&pending[1], at)), (3, third)] {
        assert!(matches!(
            ok(consume(&receiver, &delivery, at), "resumed"),
            Consumed::Applied { sequence, .. } if sequence == index
        ));
    }
    assert_eq!(ok(receiver.applied(&subject()), "applied"), 3);
    ok(
        restarted.acknowledge(&MARKET, pending[0].id()),
        "acknowledge",
    );
    assert_eq!(ok(restarted.pending(&MARKET), "pending").len(), 2);
    let behind = consumer(&scratch.0.join("behind"));
    ok(behind.resynchronize(&subject(), 2), "resync");
    assert!(matches!(
        consume(&behind, &first, at),
        Err(WebhookError::OrderViolation)
    ));
    assert!(matches!(
        behind.resynchronize(&subject(), 1),
        Err(WebhookError::OrderViolation)
    ));
}

#[test]
fn rollback_removes_provisional_rows_and_refuses_crossing_finality() {
    let scratch = scratch("rollback");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let event = admit(&outbox, &grant, &fixture(50, 0, 4));
    for observed in [51, 52] {
        let provisional = fixture(observed, 0, 3);
        assert_eq!(
            ok(
                outbox.admit(&grant, &provisional.row, &provisional.evidence()),
                "provisional"
            ),
            Admission::Delayed
        );
    }
    assert_eq!(
        ok(outbox.rollback(&MARKET, 51), "rollback above finality"),
        1
    );
    assert_eq!(ok(outbox.rollback(&MARKET, 51), "repeat rollback"), 0);
    assert!(matches!(
        outbox.rollback(&MARKET, 49),
        Err(MarketEventError::Quarantined(
            QuarantineCause::FinalityBoundary
        ))
    ));
    assert_eq!(
        ok(outbox.quarantine(&MARKET), "quarantine"),
        Some(QuarantineCause::FinalityBoundary)
    );
    assert_eq!(ok(outbox.pending(&MARKET), "history kept"), vec![event]);
    let later = fixture(51, 0, 4);
    assert!(matches!(
        outbox.admit(&grant, &later.row, &later.evidence()),
        Err(MarketEventError::StreamQuarantined)
    ));
    assert!(matches!(
        outbox.rollback(&MARKET, 60),
        Err(MarketEventError::StreamQuarantined)
    ));
    assert_eq!(
        ok(outbox.pending(&MARKET), "no compensating event").len(),
        1
    );
    assert_eq!(
        ok(outbox.alerts(), "alerts"),
        vec![OperatorAlert {
            market_id: "33".repeat(32),
            event_id: None,
            snapshot_id: None,
            category: "finality_boundary_crossed".to_owned(),
        }]
    );
}

fn quarantine_case(
    root_directory: &Path,
    leaf: &[u8],
    label: &str,
    change: impl FnOnce(&mut Fixture),
    expected: QuarantineCause,
) {
    let outbox = granted(&root_directory.join(label));
    let grant = ok(ProducerGrant::from_certificate(leaf), "producer grant");
    let mut sample = fixture(70, 0, 4);
    change(&mut sample);
    let refusal = outbox.admit(&grant, &sample.row, &sample.evidence());
    assert!(
        matches!(refusal, Err(MarketEventError::Quarantined(cause)) if cause == expected),
        "{label}: {refusal:?}"
    );
    assert_eq!(ok(outbox.quarantine(&MARKET), label), Some(expected));
    let alerts = ok(outbox.alerts(), label);
    assert_eq!(
        alerts,
        vec![OperatorAlert {
            market_id: "33".repeat(32),
            event_id: Some(sample.row.event_id.clone()),
            snapshot_id: Some(hex_encode(&sample.row.snapshot_id)),
            category: expected.as_str().to_owned(),
        }]
    );
    let rendered = ok(serde_json::to_value(&alerts[0]), "alert renders");
    let mut keys: Vec<&String> = rendered
        .as_object()
        .unwrap_or_else(|| panic!("alert is an object"))
        .keys()
        .collect();
    keys.sort();
    assert_eq!(keys, ["category", "event_id", "market_id", "snapshot_id"]);
    let rendered = rendered.to_string();
    assert!(
        !rendered.contains(LOCATOR)
            && !rendered.contains("script")
            && !rendered.contains("market-state")
    );
    let healthy = fixture(71, 0, 4);
    assert!(matches!(
        outbox.admit(&grant, &healthy.row, &healthy.evidence()),
        Err(MarketEventError::StreamQuarantined)
    ));
}

#[test]
fn conflicting_artifacts_quarantine_with_identifier_only_alerts() {
    let scratch = scratch("quarantine");
    let leaf = role_leaf(&scratch.0, "producer", PRODUCER_ROLE, "clientAuth");
    let root_directory = scratch.0.as_path();
    quarantine_case(
        root_directory,
        &leaf,
        "state",
        |fixture| {
            fixture.state = market_state(70, b"market-state-forged");
        },
        QuarantineCause::StateDigest,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "decoder",
        |fixture| {
            fixture.row.decoder_version = 2;
        },
        QuarantineCause::DecoderVersion,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "chain",
        |fixture| {
            fixture.row.binding.chain = ok(ChainDomain::new([0x12; 32]), "chain");
            fixture.reseal();
        },
        QuarantineCause::ChainDomain,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "network",
        |fixture| fixture.recertify(99, root(70), 70, &SIGNED),
        QuarantineCause::CheckpointNetwork,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "root",
        |fixture| fixture.recertify(NETWORK, [0x01; 32], 70, &SIGNED),
        QuarantineCause::FinalityConflict,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "snapshot",
        |fixture| {
            fixture.row.snapshot_id = [0x02; 32];
        },
        QuarantineCause::SnapshotId,
    );
    quarantine_case(
        root_directory,
        &leaf,
        "event",
        |fixture| {
            fixture.row.event_id = format!("0x{}", "00".repeat(32));
        },
        QuarantineCause::EventId,
    );
}

#[test]
fn reused_event_identifier_with_changed_content_quarantines() {
    let scratch = scratch("reuse");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let original = fixture(72, 0, 4);
    let event = admit(&outbox, &grant, &original);
    let mut changed = fixture(72, 0, 4);
    changed.row.receipt_digest = [0xee; 32];
    assert!(matches!(
        outbox.admit(&grant, &changed.row, &changed.evidence()),
        Err(MarketEventError::Quarantined(
            QuarantineCause::EventConflict
        ))
    ));
    assert_eq!(ok(outbox.pending(&MARKET), "kept"), vec![event]);
    assert_eq!(
        ok(outbox.alerts(), "alerts")[0].category,
        "event_conflict".to_owned()
    );
}

#[test]
fn restored_rows_reproduce_canonical_events_without_new_entitlement() {
    let scratch = scratch("restore");
    let grant = producer(&scratch.0);
    let source = scratch.0.join("source");
    let admitted = publish_three(&source, &grant, 60);
    let restored = scratch.0.join("restored");
    ok(fs::create_dir_all(&restored), "restored directory");
    for entry in ok(fs::read_dir(&source), "source rows") {
        let entry = ok(entry, "row entry");
        ok(
            fs::copy(entry.path(), restored.join(entry.file_name())),
            "row copy",
        );
    }
    let outbox = open(&restored);
    assert_eq!(ok(outbox.pending(&MARKET), "restored pending"), admitted);
    for (observed, event) in (60..63).zip(&admitted) {
        let row = fixture(observed, 0, 4);
        assert_eq!(
            ok(outbox.admit(&grant, &row.row, &row.evidence()), "replay"),
            Admission::Finalized {
                event: event.clone(),
                duplicate: true
            }
        );
    }
    assert_eq!(ok(outbox.pending(&MARKET), "no new rows").len(), 3);
    assert_eq!(
        admit(&outbox, &grant, &fixture(63, 0, 4)).subject_sequence(),
        4
    );
    assert!(ok(outbox.alerts(), "alerts").is_empty());
}

#[test]
fn revoked_tenant_cannot_resume_with_an_unexpired_cursor() {
    let scratch = scratch("revoke");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let event = admit(&outbox, &grant, &fixture(73, 0, 4));
    let ResumePage::Events {
        events,
        next_cursor,
        has_more,
    } = ok(
        outbox.resume(&principal(), &endpoint(), &MARKET, None, 32),
        "first page",
    )
    else {
        panic!("first page must list events");
    };
    assert_eq!((events, has_more), (vec![event], false));
    assert!(matches!(
        ok(
            outbox.resume(&principal(), &endpoint(), &MARKET, Some(&next_cursor), 32),
            "resume"
        ),
        ResumePage::Events { events, .. } if events.is_empty()
    ));
    let other = ok(Principal::new("tenant:other"), "other principal");
    assert!(matches!(
        outbox.resume(&other, &endpoint(), &MARKET, Some(&next_cursor), 32),
        Err(MarketEventError::Revoked)
    ));
    let foreign = ok(EndpointId::new("whep_foreign"), "endpoint");
    assert!(matches!(
        outbox.resume(&principal(), &foreign, &MARKET, Some(&next_cursor), 32),
        Err(MarketEventError::Webhook(WebhookError::InvalidCursor))
    ));
    let mut tampered = next_cursor.clone();
    let last = if tampered.ends_with('0') { "1" } else { "0" };
    tampered.pop();
    tampered.push_str(last);
    assert!(matches!(
        outbox.resume(&principal(), &endpoint(), &MARKET, Some(&tampered), 32),
        Err(MarketEventError::Webhook(WebhookError::InvalidCursor))
    ));
    ok(outbox.revoke(&MARKET), "revoke");
    assert!(matches!(
        outbox.resume(&principal(), &endpoint(), &MARKET, Some(&next_cursor), 32),
        Err(MarketEventError::Revoked)
    ));
    assert!(matches!(
        outbox.pending(&MARKET),
        Err(MarketEventError::Revoked)
    ));
    let later = fixture(74, 0, 4);
    assert!(matches!(
        outbox.admit(&grant, &later.row, &later.evidence()),
        Err(MarketEventError::Revoked)
    ));
}

fn resync_through(page: &ResumePage) -> (Option<[u8; 32]>, u64) {
    match page {
        ResumePage::Resync {
            snapshot_id,
            through_sequence,
            ..
        } => (*snapshot_id, *through_sequence),
        ResumePage::Events { .. } => panic!("expired cursor must answer an explicit resync"),
    }
}

#[test]
fn expired_cursor_answers_explicit_gap_and_snapshot_resync() {
    let scratch = scratch("expired");
    let grant = producer(&scratch.0);
    let directory = scratch.0.join("outbox");
    let admitted = publish_three(&directory, &grant, 80);
    let outbox = open(&directory);
    for event in &admitted {
        ok(outbox.acknowledge(&MARKET, event.id()), "acknowledge");
    }
    let ResumePage::Events {
        next_cursor,
        has_more,
        ..
    } = ok(
        outbox.resume(&principal(), &endpoint(), &MARKET, None, 1),
        "page",
    )
    else {
        panic!("first page must list events");
    };
    assert!(has_more);
    assert_eq!(ok(outbox.prune(&MARKET, 2), "prune"), 2);
    let latest = fixture(82, 0, 4).row.snapshot_id;
    assert_eq!(
        resync_through(&ok(
            outbox.resume(&principal(), &endpoint(), &MARKET, Some(&next_cursor), 32),
            "expired cursor"
        )),
        (Some(latest), 3)
    );
    let ResumePage::Events {
        next_cursor: current,
        ..
    } = ok(
        outbox.resume(&principal(), &endpoint(), &MARKET, None, 32),
        "retained page",
    )
    else {
        panic!("retained rows must page");
    };
    let overlap = open_with(
        &directory,
        ok(CursorKeys::new((2, [2; 32]), &[(1, [1; 32])]), "keys"),
    );
    assert!(matches!(
        ok(
            overlap.resume(&principal(), &endpoint(), &MARKET, Some(&current), 32),
            "overlap"
        ),
        ResumePage::Events { events, .. } if events.is_empty()
    ));
    let retired = open_with(&directory, ok(CursorKeys::new((2, [2; 32]), &[]), "keys"));
    assert_eq!(
        resync_through(&ok(
            retired.resume(&principal(), &endpoint(), &MARKET, Some(&current), 32),
            "retired key"
        )),
        (Some(latest), 3)
    );
    let receiver = consumer(&scratch.0.join("consumer"));
    ok(receiver.resynchronize(&subject(), 3), "snapshot resync");
    let next = admit(&outbox, &grant, &fixture(83, 0, 4));
    let at = now();
    assert!(matches!(
        ok(consume(&receiver, &deliver(&next, at), at), "after resync"),
        Consumed::Applied { sequence: 4, .. }
    ));
    assert!(matches!(
        outbox.prune(&MARKET, 4),
        Err(MarketEventError::InvalidRequest)
    ));
}

#[test]
fn receiver_refuses_wrong_body_key_and_timestamp() {
    let scratch = scratch("receiver");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let event = admit(&outbox, &grant, &fixture(90, 0, 4));
    let receiver = consumer(&scratch.0.join("consumer"));
    let at = now();
    let good = deliver(&event, at);
    let mut body = good.clone();
    body.body.push(b' ');
    let forged = ok(
        SignedDelivery::sign(
            &event,
            &endpoint(),
            KEY_ID,
            &SigningKey::from_bytes(&[0x43; 32]),
            at,
        ),
        "forged",
    );
    let unknown = ok(
        SignedDelivery::sign(&event, &endpoint(), "whk_unknown", &signer(), at),
        "unknown key",
    );
    let mut renamed = good.clone();
    renamed.id = "paxai_other".to_owned();
    for (label, delivery) in [
        ("body", &body),
        ("key", &forged),
        ("key id", &unknown),
        ("id", &renamed),
    ] {
        assert!(
            matches!(
                consume(&receiver, delivery, at),
                Err(WebhookError::SignatureRejected)
            ),
            "{label}"
        );
    }
    assert!(matches!(
        consume(&receiver, &good, at + 301),
        Err(WebhookError::StaleTimestamp)
    ));
    assert!(matches!(
        consume(&receiver, &good, at - 31),
        Err(WebhookError::StaleTimestamp)
    ));
    assert_eq!(ok(receiver.applied(&subject()), "nothing applied"), 0);
    assert!(matches!(
        ok(consume(&receiver, &good, at), "valid"),
        Consumed::Applied { sequence: 1, .. }
    ));
}

fn child_consume(root: &Path) {
    let delivery = root.join("delivery");
    let read = |name: &str| ok(fs::read_to_string(delivery.join(name)), name);
    let (id, timestamp, key_id, signature) = (
        read("id"),
        read("timestamp"),
        read("key"),
        read("signature"),
    );
    let body = ok(fs::read(delivery.join("body")), "body");
    let receiver = consumer(&root.join("consumer"));
    let presentation = Presentation {
        id: &id,
        timestamp: &timestamp,
        key_id: &key_id,
        signature: &signature,
        payload: &body,
        now: now(),
        tolerance_seconds: 300,
    };
    match receiver.receive(&presentation, |event| {
        format!("credited:{}:{}", event.id(), std::process::id())
    }) {
        Ok(Consumed::Applied { result, .. }) => println!("paxai-consumed applied {result}"),
        Ok(Consumed::Duplicate { result, .. }) => println!("paxai-consumed duplicate {result}"),
        other => panic!("child consumer refused: {other:?}"),
    }
}

#[test]
fn concurrent_retry_across_two_consumer_processes_creates_one_effect() {
    if let Ok(root) = std::env::var(CHILD) {
        child_consume(Path::new(&root));
        return;
    }
    let scratch = scratch("processes");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let event = admit(&outbox, &grant, &fixture(95, 0, 4));
    let signed = deliver(&event, now());
    let delivery = scratch.0.join("delivery");
    ok(fs::create_dir_all(&delivery), "delivery directory");
    for (name, value) in [
        ("id", signed.id.as_bytes()),
        ("timestamp", signed.timestamp.as_bytes()),
        ("key", signed.key_id.as_bytes()),
        ("signature", signed.signature.as_bytes()),
        ("body", signed.body.as_slice()),
    ] {
        ok(fs::write(delivery.join(name), value), name);
    }
    let executable = ok(std::env::current_exe(), "test executable");
    let children: Vec<Child> = (0..2)
        .map(|_| {
            ok(
                Command::new(&executable)
                    .args([
                        "concurrent_retry_across_two_consumer_processes_creates_one_effect",
                        "--exact",
                        "--nocapture",
                        "--test-threads=1",
                    ])
                    .env(CHILD, &scratch.0)
                    .stdout(Stdio::piped())
                    .stderr(Stdio::piped())
                    .spawn(),
                "consumer process",
            )
        })
        .collect();
    let mut verdicts: Vec<(String, String)> = children
        .into_iter()
        .map(|child| {
            let output = ok(child.wait_with_output(), "consumer process output");
            let stdout = String::from_utf8_lossy(&output.stdout).into_owned();
            assert!(
                output.status.success(),
                "{stdout}{}",
                String::from_utf8_lossy(&output.stderr)
            );
            let line = stdout
                .lines()
                .find_map(|line| {
                    line.split_once("paxai-consumed ")
                        .map(|(_, verdict)| verdict)
                })
                .unwrap_or_else(|| panic!("consumer process reported nothing: {stdout}"));
            let (kind, result) = line
                .split_once(' ')
                .unwrap_or_else(|| panic!("malformed verdict: {line}"));
            (kind.to_owned(), result.to_owned())
        })
        .collect();
    verdicts.sort();
    assert_eq!(verdicts[0].0, "applied");
    assert_eq!(verdicts[1].0, "duplicate");
    assert_eq!(verdicts[0].1, verdicts[1].1);
    let receiver = consumer(&scratch.0.join("consumer"));
    assert_eq!(ok(receiver.applied(&subject()), "one effect"), 1);
    assert_eq!(ok(receiver.last_sequence(&subject()), "sequence"), 1);
}

#[test]
fn bounds_refuse_before_any_quarantine_or_cryptographic_work() {
    let scratch = scratch("bounds");
    let grant = producer(&scratch.0);
    let outbox = granted(&scratch.0.join("outbox"));
    let cases: [(&str, Change); 7] = [
        ("state", |fixture| {
            fixture.state = vec![7; 262_145];
            fixture.reseal();
        }),
        ("evidence", |fixture| {
            fixture.certificate = Certificate::new(
                Checkpoint::new(
                    fixture.certificate.checkpoint().header_bytes().to_vec(),
                    vec![0; 1_048_577],
                ),
                Vec::new(),
                THRESHOLD,
                None,
            );
        }),
        ("action", |fixture| fixture.row.action = "commit".to_owned()),
        ("settlement rank", |fixture| fixture.row.binding.rank = 5),
        ("rank", |fixture| fixture.row.binding.rank = 6),
        ("revision", |fixture| fixture.row.binding.revision = 0),
        ("receipt", |fixture| fixture.row.receipt_digest = [0; 32]),
    ];
    for (label, change) in cases {
        let mut sample = fixture(99, 0, 4);
        change(&mut sample);
        assert!(
            matches!(
                outbox.admit(&grant, &sample.row, &sample.evidence()),
                Err(MarketEventError::InvalidRequest)
            ),
            "{label}"
        );
    }
    let unproven = fixture(99, 0, 4);
    assert!(matches!(
        outbox.admit(
            &grant,
            &unproven.row,
            &Evidence {
                state: &unproven.state,
                proof: None
            }
        ),
        Err(MarketEventError::InvalidRequest)
    ));
    for limit in [0, 33] {
        assert!(matches!(
            outbox.resume(&principal(), &endpoint(), &MARKET, None, limit),
            Err(MarketEventError::InvalidRequest)
        ));
    }
    let long = "p".repeat(65);
    assert!(matches!(
        outbox.resume(&principal(), &endpoint(), &MARKET, Some(&long), 32),
        Err(MarketEventError::InvalidRequest)
    ));
    assert_eq!(ok(outbox.quarantine(&MARKET), "quarantine"), None);
    assert!(ok(outbox.alerts(), "alerts").is_empty());
}

fn free_port() -> u16 {
    ok(
        TcpListener::bind("127.0.0.1:0").and_then(|listener| listener.local_addr()),
        "port",
    )
    .port()
}

fn secret(directory: &Path, name: &str, value: &str) -> String {
    let file = directory.join(name);
    ok(fs::write(&file, value), name);
    text(&file)
}

fn service_material(directory: &Path) {
    let key = text(&directory.join("server-key.pem"));
    let certificate = text(&directory.join("server.pem"));
    run(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            &key,
            "-out",
            &certificate,
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
        ],
    );
    run(
        "openssl",
        &[
            "x509",
            "-in",
            &certificate,
            "-outform",
            "DER",
            "-out",
            &text(&directory.join("server.der")),
        ],
    );
    run(
        "openssl",
        &[
            "pkcs12",
            "-export",
            "-inkey",
            &key,
            "-in",
            &certificate,
            "-out",
            &text(&directory.join("client.p12")),
            "-passout",
            "pass:integration-only",
        ],
    );
}

fn public_environment(directory: &Path, listen: u16) -> Vec<(String, String)> {
    let ca = text(&directory.join("server.der"));
    let unused = format!("https://localhost:{}", free_port());
    let mut environment: Vec<(String, String)> = [
        ("LAYERX_WEBHOOKS_LISTEN", format!("127.0.0.1:{listen}")),
        ("LAYERX_WEBHOOKS_LISTENER", "plain".to_owned()),
        ("LAYERX_WEBHOOKS_ROLE", "public".to_owned()),
        ("LAYERX_WEBHOOKS_INTERNAL_CA_DER", ca.clone()),
        ("LAYERX_WEBHOOKS_PUBLIC_CA_DER", ca),
        (
            "LAYERX_WEBHOOKS_CLIENT_IDENTITY_PKCS12",
            text(&directory.join("client.p12")),
        ),
        (
            "LAYERX_WEBHOOKS_CLIENT_IDENTITY_PASSWORD_FILE",
            secret(directory, "identity-password", "integration-only"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_URL",
            unused.replace("https", "rediss"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_USERNAME_FILE",
            secret(directory, "redis-username", "webhooks"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_PASSWORD_FILE",
            secret(directory, "redis-password", "webhooks-secret"),
        ),
        (
            "LAYERX_WEBHOOKS_CURSOR_KEY_FILE",
            secret(directory, "cursor-key", &"11".repeat(32)),
        ),
        ("LAYERX_WEBHOOKS_KMS_URL", unused.clone()),
        (
            "LAYERX_WEBHOOKS_KMS_TOKEN_FILE",
            secret(directory, "kms-token", "kms-token"),
        ),
        (
            "LAYERX_WEBHOOKS_INSTANCE_ID",
            "webhooks-paxai-public".to_owned(),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_PUBLIC_KEY_FILE",
            secret(
                directory,
                "sequencer-public-key",
                &format!("58{}", "66".repeat(31)),
            ),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_ID_FILE",
            secret(directory, "sequencer-id", &"22".repeat(32)),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_FIRST_BATCH_FILE",
            secret(directory, "sequencer-first-batch", "1"),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_LAST_BATCH_FILE",
            secret(directory, "sequencer-last-batch", &u64::MAX.to_string()),
        ),
        ("LAYERX_WEBHOOKS_LXP_WIRE_VERSION", "3".to_owned()),
        (
            "LAYERX_WEBHOOKS_NETWORK_ID",
            "paxai-public-process".to_owned(),
        ),
        ("LAYERX_WEBHOOKS_COMPONENT_URL", unused.clone()),
        (
            "LAYERX_WEBHOOKS_COMPONENT_TOKEN_FILE",
            secret(directory, "component-token", "component-token"),
        ),
        ("LAYERX_WEBHOOKS_AUTHORITY_URL", unused.clone()),
        (
            "LAYERX_WEBHOOKS_AUTHORITY_TOKEN_FILE",
            secret(directory, "authority-token", "authority-token"),
        ),
        ("LAYERX_WEBHOOKS_IDENTITY_URL", unused.clone()),
        (
            "LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE",
            secret(directory, "identity-token", "identity-token"),
        ),
    ]
    .into_iter()
    .map(|(name, value)| (name.to_owned(), value))
    .collect();
    for stem in ["JOURNEY", "PAYMENT", "APPROVAL", "PROGRAM"] {
        environment.push((format!("LAYERX_WEBHOOKS_{stem}_SOURCE_URL"), unused.clone()));
        environment.push((
            format!("LAYERX_WEBHOOKS_{stem}_SOURCE_TOKEN_FILE"),
            secret(directory, "source-token", "source-token"),
        ));
    }
    environment
}

struct Service(Child);

impl Drop for Service {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn post(service: &mut Service, url: &str) -> (u16, String) {
    for _ in 0..100 {
        if let Ok(Some(status)) = service.0.try_wait() {
            panic!("layerx-webhooks exited before answering: {status}");
        }
        let output = ok(
            Command::new("curl")
                .args([
                    "-sS",
                    "--max-time",
                    "5",
                    "-X",
                    "POST",
                    "-H",
                    "content-type: application/json",
                ])
                .args(["--data", "{}", "-w", "\n%{http_code}", url])
                .output(),
            "curl",
        );
        if output.status.success() {
            let answer = ok(String::from_utf8(output.stdout), "answer is UTF-8");
            let (body, status) = answer
                .rsplit_once('\n')
                .unwrap_or_else(|| panic!("answer carries a status: {answer}"));
            return (ok(status.parse(), "status"), body.to_owned());
        }
        thread::sleep(Duration::from_millis(100));
    }
    panic!("layerx-webhooks never answered {url}")
}

#[test]
fn public_process_refuses_private_producer_route_and_role_mismatch() {
    let scratch = scratch("public");
    service_material(&scratch.0);
    let port = free_port();
    let environment = public_environment(&scratch.0, port);
    let mut service = Service(ok(
        Command::new(BINARY)
            .env_clear()
            .envs(environment.iter().map(|(name, value)| (name, value)))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn(),
        "layerx-webhooks starts",
    ));
    let (status, body) = post(
        &mut service,
        &format!("http://127.0.0.1:{port}/internal/v1/events/program/paxai_probe"),
    );
    assert_eq!(status, 404);
    assert_eq!(body, r#"{"error":{"code":"not_found","retry":"never"}}"#);
    for (label, role, usage) in [
        ("operator", OPERATOR_ROLE, "clientAuth"),
        ("server", PRODUCER_ROLE, "serverAuth"),
        (
            "unknown",
            "urn:layerx:webhooks:role:publisher",
            "clientAuth",
        ),
    ] {
        let leaf = role_leaf(&scratch.0, label, role, usage);
        assert!(
            matches!(
                ProducerGrant::from_certificate(&leaf),
                Err(MarketEventError::ProducerRoleRequired)
            ),
            "{label}"
        );
    }
    let outbox = granted(&scratch.0.join("outbox"));
    assert!(ok(outbox.pending(&MARKET), "nothing published").is_empty());
    let leaf = role_leaf(&scratch.0, "producer", PRODUCER_ROLE, "clientAuth");
    assert!(ProducerGrant::from_certificate(&leaf).is_ok());
}
