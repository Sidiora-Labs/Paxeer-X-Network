//! AI.F09-A14, A15, A16, A19, A21 and the R16/R20 retention and recovery paths
//! against the actual service binary over real TLS on loopback, its real
//! on-disk store, the retention ledger in src/retention.rs, real Ed25519
//! publisher envelopes and market state built by the real F01 CREATE whose
//! task region carries canonical task records, captured chunk by chunk and
//! bound to finality evidence by `queries::bind_snapshot`.
#[path = "../src/crypto.rs"]
#[allow(dead_code)]
mod crypto;
#[path = "../src/retention.rs"]
#[allow(dead_code)]
mod retention;
#[path = "../src/store.rs"]
#[allow(dead_code)]
mod store;

use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{XChaCha20Poly1305, XNonce};
use crypto::FrameContext;
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::codec::{
    self, decode_envelope, derive_market, encode_envelope, Envelope,
};
use layerx_programs_ai_market::dispatch;
use layerx_programs_ai_market::evidence::{
    chunk_count, encode_envelope as encode_publisher_envelope, encode_manifest, manifest_root,
    object_content_root, publisher_digest, ArtifactContext, ArtifactError, ArtifactKind,
    ArtifactManifest, Items, Privacy, PublisherEnvelope, CHUNK_BYTES, MAX_ENVELOPE_BYTES,
    MAX_MANIFEST_BYTES,
};
use layerx_programs_ai_market::policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES};
use layerx_programs_ai_market::queries::{
    bind_snapshot, read_state_chunk, FinalityEvidence, ReadProof, SnapshotBinding, StateCapture,
};
use layerx_programs_ai_market::registry::{derive_rewards_account, F01_SECTION_CAP};
use layerx_programs_ai_market::registry_ops::{self, CallContext, Outcome, PolicySection};
use layerx_programs_ai_market::state::{self, Section, SharedState};
use layerx_programs_ai_market::tasks::{TaskBinding, TaskStatus};
use layerx_programs_ai_market::types::{
    AssetId, Authentication, ChainDomain, Digest32, PolicyDigest, Presence, PrincipalId, ProgramId,
    PublicKey32, RequestId, RubricDigest, Signature64, TaskId, Version, WorkerId,
};
use layerx_programs_ai_market::{MAX_EVENT_BYTES, MAX_STATE_BYTES};
use retention::{
    Binding, Event, Ledger, Projection, PurgeReport, Refusal, Tombstone, EXPIRED,
    INTEGRITY_FAILURE, MAX_HOLD_SECS, OWNER_WITHDRAWN, POLICY_RESTRICTED,
};
use rustls::pki_types::ServerName;
use rustls::{ClientConfig, ClientConnection, RootCertStore, StreamOwned};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::fs;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::{Arc, OnceLock};
use std::time::{SystemTime, UNIX_EPOCH};
use store::{ObjectState, SessionState, Store};

const TOKEN_A: &str = "tenant-a-publisher-token";
const TOKEN_B: &str = "tenant-b-consumer-token";
const TENANT_A: &str = "tenant-a";
const P1: [u8; 32] = [0x55; 32];
const CHAIN: [u8; 32] = [0x11; 32];
const PROGRAM: [u8; 32] = [0x22; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const CAPACITY: u64 = 64 << 20;
const CHUNK_RESPONSE_MAX: usize = 8_244;
const DAY: u64 = 86_400;

fn sha(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}
fn key1() -> SigningKey {
    SigningKey::from_bytes(&[1; 32])
}
fn now_secs() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
}
fn ctx() -> ArtifactContext {
    let chain = ChainDomain::new(CHAIN).unwrap();
    let program = ProgramId::new(PROGRAM).unwrap();
    ArtifactContext {
        chain,
        program,
        market: derive_market(chain, program).unwrap(),
        policy: PolicyDigest::new([0x44; 32]).unwrap(),
    }
}
fn rid(tag: u8, n: u8) -> String {
    let mut id = [tag; 32];
    id[31] = n;
    hex::encode(id)
}

/// Independent V1 frame encoder: u64BE(length)||bytes partitioned into
/// 262144-byte frames, each sealed with a fresh 24-byte nonce.
fn seal_with(key: &[u8; 32], ctx: &FrameContext, plain: &[u8]) -> Vec<u8> {
    let cipher = XChaCha20Poly1305::new(key.into());
    let body = [&(plain.len() as u64).to_be_bytes()[..], plain].concat();
    let mut out = Vec::new();
    for (i, part) in body.chunks(262_144).enumerate() {
        let nonce = crypto::random::<24>().unwrap();
        let mut aad = b"PAXAI/private-frame/v1\0".to_vec();
        aad.extend_from_slice(&ctx.context);
        aad.extend_from_slice(&ctx.subject);
        aad.push(ctx.kind);
        aad.extend_from_slice(&(i as u32).to_be_bytes());
        let sealed = cipher
            .encrypt(
                XNonce::from_slice(&nonce),
                Payload {
                    msg: part,
                    aad: &aad,
                },
            )
            .unwrap();
        out.extend_from_slice(&1u16.to_be_bytes());
        out.extend_from_slice(&(i as u32).to_be_bytes());
        out.extend_from_slice(&nonce);
        out.extend_from_slice(&(sealed.len() as u32).to_be_bytes());
        out.extend_from_slice(&sealed);
    }
    out
}
fn sealed_len(plain: usize) -> usize {
    let body = plain + 8;
    body + body.div_ceil(262_144) * (34 + 16)
}

/// One locally generated self-signed loopback certificate per test process.
struct Cert {
    cert_pem: String,
    key_pem: String,
    client: Arc<ClientConfig>,
}
fn cert() -> &'static Cert {
    static CERT: OnceLock<Cert> = OnceLock::new();
    CERT.get_or_init(|| {
        let issued = rcgen::generate_simple_self_signed(vec!["localhost".to_string()]).unwrap();
        let mut roots = RootCertStore::empty();
        roots.add(issued.cert.der().clone()).unwrap();
        let client =
            ClientConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
                .with_safe_default_protocol_versions()
                .unwrap()
                .with_root_certificates(roots)
                .with_no_client_auth();
        Cert {
            cert_pem: issued.cert.pem(),
            key_pem: issued.signing_key.serialize_pem(),
            client: Arc::new(client),
        }
    })
}

struct Svc {
    child: Child,
    addr: String,
}
impl Drop for Svc {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn data_dir(name: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    let dir = PathBuf::from(env!("CARGO_TARGET_TMPDIR")).join(format!(
        "ai-artifacts-recovery-{name}-{}-{nanos}",
        std::process::id()
    ));
    fs::create_dir_all(&dir).unwrap();
    dir
}
fn mark_file(dir: &Path) -> PathBuf {
    dir.join("revocation.mark")
}
fn open_store(dir: &Path) -> Store {
    Store::open(&dir.join("store"), &mark_file(dir)).unwrap()
}
/// The ledger lives in the restorable data directory; its live mark does not.
fn open_ledger(dir: &Path) -> Ledger {
    Ledger::open(&dir.join("store"), &dir.join("retention.mark")).unwrap()
}
fn events(dir: &Path) -> String {
    fs::read_to_string(dir.join("store").join("events.log")).unwrap()
}
fn copy_dir(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap();
    for entry in fs::read_dir(from).unwrap() {
        let entry = entry.unwrap();
        let target = to.join(entry.file_name());
        if entry.file_type().unwrap().is_dir() {
            copy_dir(&entry.path(), &target);
        } else {
            fs::copy(entry.path(), target).unwrap();
        }
    }
}

fn start(dir: &Path, capacity: u64) -> Svc {
    try_start(dir, capacity).unwrap_or_else(|status| panic!("service refused: {status}"))
}
/// Starts the binary with the given storage capacity; a refused startup
/// returns its exit status.
fn try_start(dir: &Path, capacity: u64) -> Result<Svc, ExitStatus> {
    let config = dir.join("config.json");
    fs::write(dir.join("cert.pem"), &cert().cert_pem).unwrap();
    fs::write(dir.join("key.pem"), &cert().key_pem).unwrap();
    let cfg = json!({
        "capacity_bytes": capacity,
        "listener": {"tls": {"certificate": dir.join("cert.pem"), "private_key": dir.join("key.pem")}},
        "revocation_mark": mark_file(dir),
        "tenants": [
            {"id": TENANT_A, "token_sha256": hex::encode(sha(TOKEN_A.as_bytes())), "quota_bytes": 8u64 << 20},
            {"id": "tenant-b", "token_sha256": hex::encode(sha(TOKEN_B.as_bytes())), "quota_bytes": 1u64 << 20},
        ],
        "publishers": [
            {"tenant": TENANT_A, "principal": hex::encode(P1), "generation": 1, "key": hex::encode(key1().verifying_key().to_bytes())},
        ],
    });
    fs::write(&config, cfg.to_string()).unwrap();
    if !dir.join("kek.bin").exists() {
        fs::write(dir.join("kek.bin"), crypto::random::<32>().unwrap()).unwrap();
    }
    let log = fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(dir.join("service.log"))
        .unwrap();
    let mut child = Command::new(env!("CARGO_BIN_EXE_layerx-ai-artifacts"))
        .arg("--data-dir")
        .arg(dir.join("store"))
        .arg("--config")
        .arg(&config)
        .arg("--key-file")
        .arg(dir.join("kek.bin"))
        .stdout(Stdio::piped())
        .stderr(Stdio::from(log))
        .spawn()
        .unwrap();
    let mut line = String::new();
    BufReader::new(child.stdout.take().unwrap())
        .read_line(&mut line)
        .unwrap();
    let Some(addr) = line.trim().strip_prefix("listening ").map(str::to_string) else {
        return Err(child.wait().unwrap());
    };
    Ok(Svc { child, addr })
}

/// One request per connection over TLS verified against the local certificate.
fn http(svc: &Svc, method: &str, path: &str, token: &str, body: &[u8]) -> (u16, Vec<u8>) {
    let head = format!("{method} {path} HTTP/1.1\r\nHost: artifacts\r\nAuthorization: Bearer {token}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", body.len());
    let request = [head.as_bytes(), body].concat();
    let tcp = TcpStream::connect(&svc.addr).unwrap();
    let name = ServerName::try_from("localhost").unwrap();
    let conn = ClientConnection::new(cert().client.clone(), name).unwrap();
    let mut stream = StreamOwned::new(conn, tcp);
    stream.write_all(&request).unwrap();
    let mut out = Vec::new();
    stream.read_to_end(&mut out).unwrap();
    let split = out.windows(4).position(|w| w == b"\r\n\r\n").unwrap();
    let status = std::str::from_utf8(&out[..split])
        .unwrap()
        .split(' ')
        .nth(1)
        .unwrap()
        .parse()
        .unwrap();
    (status, out[split + 4..].to_vec())
}
fn post(svc: &Svc, path: &str, token: &str, body: &Value) -> (u16, Value) {
    let (status, bytes) = http(svc, "POST", path, token, body.to_string().as_bytes());
    (status, serde_json::from_slice(&bytes).unwrap())
}
fn usage(svc: &Svc) -> Value {
    let (status, bytes) = http(svc, "GET", "/v1/usage", TOKEN_A, b"");
    assert_eq!(status, 200);
    serde_json::from_slice(&bytes).unwrap()
}
fn resolve_public(svc: &Svc, root: &str) -> (u16, Value) {
    post(
        svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "evaluation"}),
    )
}

/// One task-bound INPUT artifact; its manifest subject is its task identity.
struct Object {
    plain: Vec<u8>,
    privacy: Privacy,
    subject: [u8; 32],
}
struct Staged {
    session: String,
    bytes: Vec<u8>,
}
fn input(plain: &[u8], task: u8, privacy: Privacy) -> Object {
    Object {
        plain: plain.to_vec(),
        privacy,
        subject: [task; 32],
    }
}

fn manifest_for(o: &Object, bytes: &[u8]) -> Vec<u8> {
    let count = chunk_count(bytes.len() as u64).unwrap();
    let mut scratch = vec![[0u8; 32]; count as usize];
    let manifest = ArtifactManifest {
        kind: ArtifactKind::Input,
        privacy: o.privacy,
        context: ctx(),
        epoch: 9,
        publisher: PrincipalId::new(P1).unwrap(),
        subject: o.subject,
        byte_length: bytes.len() as u64,
        chunk_count: count,
        content_root: object_content_root(bytes, &mut scratch).unwrap(),
        parents: Items::Typed(&[]),
        declaration_root: [0; 32],
        reproduction_root: [0; 32],
        access_policy_root: if o.privacy == Privacy::Encrypted {
            [0x66; 32]
        } else {
            [0; 32]
        },
        not_after_height: 0,
    };
    let mut out = vec![0; MAX_MANIFEST_BYTES];
    let n = encode_manifest(&manifest, &mut out).unwrap();
    out.truncate(n);
    out
}
fn envelope_for(o: &Object, bytes: &[u8]) -> Vec<u8> {
    let manifest = manifest_for(o, bytes);
    let generation = Version::new(1).unwrap();
    let digest = publisher_digest(&ctx(), manifest_root(&manifest).unwrap(), generation);
    let envelope = PublisherEnvelope {
        manifest: &manifest,
        generation,
        key: PublicKey32(key1().verifying_key().to_bytes()),
        signature: Signature64(key1().sign(&digest).to_bytes()),
    };
    let mut out = vec![0; MAX_ENVELOPE_BYTES];
    let n = encode_publisher_envelope(&envelope, &mut out).unwrap();
    out.truncate(n);
    out
}

fn prepare(svc: &Svc, request: &str, privacy: Privacy, byte_length: usize) -> (u16, Value) {
    post(
        svc,
        "/v1/prepare",
        TOKEN_A,
        &json!({
            "request_id": request, "kind": ArtifactKind::Input as u8, "privacy": privacy as u8,
            "context": hex::encode(ctx().bytes()), "byte_length": byte_length,
        }),
    )
}
/// PrepareObject, client-side sealing under the issued key, every upload frame.
fn stage(svc: &Svc, request: &str, o: &Object) -> Staged {
    let length = match o.privacy {
        Privacy::Public => o.plain.len(),
        Privacy::Encrypted => sealed_len(o.plain.len()),
    };
    let (status, prepared) = prepare(svc, request, o.privacy, length);
    assert_eq!(status, 200, "{prepared}");
    assert_eq!(prepared["state"], "STAGING");
    let session = prepared["session"].as_str().unwrap().to_string();
    let bytes = match prepared["object_key"].as_str() {
        Some(key) => {
            let key: [u8; 32] = hex::decode(key).unwrap().try_into().unwrap();
            let frames = FrameContext {
                context: ctx().bytes(),
                subject: o.subject,
                kind: ArtifactKind::Input as u8,
            };
            seal_with(&key, &frames, &o.plain)
        }
        None => o.plain.clone(),
    };
    assert_eq!(bytes.len(), length);
    for (i, chunk) in bytes.chunks(CHUNK_BYTES as usize).enumerate() {
        let (status, body) = http(
            svc,
            "PUT",
            &format!("/v1/upload/{session}/{i}"),
            TOKEN_A,
            chunk,
        );
        assert_eq!(status, 200, "{}", String::from_utf8_lossy(&body));
    }
    Staged { session, bytes }
}
fn finalize(svc: &Svc, request: &str, staged: &Staged, o: &Object) -> (u16, Value) {
    post(
        svc,
        "/v1/finalize",
        TOKEN_A,
        &json!({
            "request_id": request, "session": staged.session,
            "envelope": hex::encode(envelope_for(o, &staged.bytes)),
        }),
    )
}
fn publish(svc: &Svc, tag: u8, o: &Object) -> String {
    let staged = stage(svc, &rid(tag, 1), o);
    let (status, receipt) = finalize(svc, &rid(tag, 2), &staged, o);
    assert_eq!(status, 200, "{receipt}");
    assert_eq!(receipt["protocol_receipt"], false);
    receipt["root"].as_str().unwrap().to_string()
}
fn grant(svc: &Svc, tag: u8, root: &str, task: [u8; 32]) -> Value {
    let (status, granted) = post(
        svc,
        "/v1/grants",
        TOKEN_A,
        &json!({"request_id": rid(tag, 9), "root": root, "task": hex::encode(task),
                "grantee": "tenant-b", "purposes": 2, "ttl_secs": 3_600}),
    );
    assert_eq!(status, 200, "{granted}");
    granted
}
fn root_digest(root: &str) -> [u8; 32] {
    hex::decode(root).unwrap().try_into().unwrap()
}

fn chain() -> ChainDomain {
    ChainDomain::new(CHAIN).unwrap()
}
fn program() -> ProgramId {
    ProgramId::new(PROGRAM).unwrap()
}
fn digest(byte: u8) -> Digest32 {
    Digest32::new([byte; 32]).unwrap()
}
fn encode_shared(shared: &SharedState<'_>) -> Vec<u8> {
    let mut out = vec![0; shared.encoded_len().unwrap()];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = state::encode_shared_state(shared, &mut out, &mut scratch).unwrap();
    out.truncate(n);
    out
}
/// Real F01 CREATE through `registry_ops::apply`.
fn created_state() -> Vec<u8> {
    let policy = TaskPolicyV1::bounded_default(
        1,
        1,
        PolicyCommitments {
            model_artifact: digest(1),
            dataset_artifact: [2; 32],
            benchmark_suite: digest(3),
            rubric: RubricDigest::new([4; 32]).unwrap(),
            task_schema: digest(5),
            result_schema: digest(6),
            service_terms: digest(7),
        },
        100,
        1,
    )
    .unwrap();
    let mut encoded_policy = vec![0; TASK_POLICY_BYTES];
    policy.encode(&mut encoded_policy).unwrap();
    let rewards = derive_rewards_account(program(), AssetId::new(ASSET).unwrap()).unwrap();
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&encoded_policy);
    payload.extend_from_slice(&[16; 32]);
    let owner = PrincipalId::new(OWNER).unwrap();
    let envelope = Envelope {
        operation: dispatch::CREATE,
        chain: chain(),
        program: program(),
        market: derive_market(chain(), program()).unwrap(),
        actor: owner,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 1,
        expiry: 1_000_000,
        request: RequestId::new([1; 32]).unwrap(),
        payload: &payload,
        authentication: Authentication::Native,
    };
    let mut encoded = vec![0; 16_384];
    let n = encode_envelope(&envelope, &mut encoded).unwrap();
    let validated = decode_envelope(&encoded[..n]).unwrap();
    let call = CallContext {
        chain: chain(),
        program: program(),
        principal: owner,
        height: 1000,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    match registry_ops::apply(&call, None, &validated, &mut section, &mut event).unwrap() {
        Outcome::Applied { state, .. } => encode_shared(&state),
        Outcome::AlreadyApplied(_) => panic!("fresh create retried"),
    }
}
/// A canonical task record whose digests match its status.
fn task(id: u8, status: TaskStatus, input: [u8; 32]) -> TaskBinding {
    let acknowledged = !matches!(status, TaskStatus::Admitted | TaskStatus::Cancelled);
    TaskBinding {
        task: TaskId::new([id; 32]).unwrap(),
        requester: PrincipalId::new([0x61; 32]).unwrap(),
        worker: WorkerId::new([0x62; 32]).unwrap(),
        input: Digest32::new(input).unwrap(),
        deadline: 5_000,
        status,
        acknowledgement: acknowledged.then(|| digest(0x63)),
        result: (status == TaskStatus::ResultCommitted).then(|| digest(0x64)),
        admission: digest(0x65),
    }
}
/// The created market with its task region holding exactly `tasks`.
fn with_tasks(base: &[u8], tasks: &[TaskBinding]) -> Vec<u8> {
    let mut sorted = tasks.to_vec();
    sorted.sort_by_key(|t| t.task);
    let mut region = u16::try_from(sorted.len()).unwrap().to_be_bytes().to_vec();
    region.push(0);
    for binding in &sorted {
        region.extend_from_slice(&binding.encode().unwrap());
    }
    let shared = state::decode_shared_state(base).unwrap();
    let mut section =
        PolicySection::decode(shared.section(Section::PolicyLifecycle).unwrap()).unwrap();
    section.task_region = &region;
    let mut bytes = vec![0; Section::PolicyLifecycle.payload_cap()];
    let n = section.encode(&mut bytes).unwrap();
    encode_shared(
        &shared
            .replace_section(Section::PolicyLifecycle, &bytes[..n])
            .unwrap(),
    )
}
fn chunk_payload(revision: u64, pinned: Option<[u8; 32]>, offset: u32) -> Vec<u8> {
    let mut payload = revision.to_be_bytes().to_vec();
    payload.extend_from_slice(&pinned.unwrap_or([0; 32]));
    payload.extend_from_slice(&offset.to_be_bytes());
    payload.extend_from_slice(&8192u16.to_be_bytes());
    payload
}
/// Discovery read then pinned reads through `read_state_chunk`, reassembled by
/// `StateCapture` under one read proof and bound to finality of `rank`.
fn snapshot(state_bytes: &[u8], rank: u8) -> SnapshotBinding {
    let proof = ReadProof {
        chain: chain(),
        program: program(),
        native_state_root: digest(0x5a),
        observed_sequence: 77,
        execution_height: 1010,
        batch_id: digest(0xbb),
    };
    let mut out = vec![0; CHUNK_RESPONSE_MAX];
    let mut buffer = vec![0; MAX_STATE_BYTES];
    let mut capture = StateCapture::new(&mut buffer);
    let n = read_state_chunk(state_bytes, &chunk_payload(0, None, 0), &mut out).unwrap();
    let first = codec::decode_chunk_response(&out[..n]).unwrap();
    let (revision, pinned, total) = (first.revision, first.digest.bytes(), first.total_bytes);
    capture.accept(&proof, &out[..n]).unwrap();
    for offset in (8192..total).step_by(8192) {
        let payload = chunk_payload(revision, Some(pinned), offset);
        let n = read_state_chunk(state_bytes, &payload, &mut out).unwrap();
        capture.accept(&proof, &out[..n]).unwrap();
    }
    let (captured, facts) = capture.finish().unwrap();
    assert_eq!(captured, state_bytes);
    let finality = FinalityEvidence {
        native_state_root: proof.native_state_root,
        checkpoint: digest(0xcc),
        settlement: Presence::Absent,
        rank,
    };
    bind_snapshot(state_bytes, &facts, &finality, 1_700_000_000_000).unwrap()
}

#[test]
fn a14_kill_after_chunk_persistence_reconciles_to_one_publication_or_failed_staging() {
    let dir = data_dir("a14-unacknowledged");
    let svc = start(&dir, CAPACITY);
    let a = input(
        b"input bytes durable before any publication ack",
        0xa1,
        Privacy::Public,
    );
    let staged = stage(&svc, &rid(0xa1, 1), &a);
    let root = hex::encode(
        manifest_root(&manifest_for(&a, &staged.bytes))
            .unwrap()
            .bytes(),
    );
    drop(svc);
    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root);
    assert_eq!(
        (status, r["status"].as_str()),
        (404, Some("CONTENT_UNAVAILABLE"))
    );
    let (status, first) = finalize(&svc, &rid(0xa1, 2), &staged, &a);
    assert_eq!(status, 200, "{first}");
    assert_eq!(first["root"].as_str(), Some(root.as_str()));
    let (status, again) = finalize(&svc, &rid(0xa1, 3), &staged, &a);
    assert_eq!((status, &again), (200, &first));
    drop(svc);
    let mut store = open_store(&dir);
    assert_eq!(store.state.objects.len(), 1);
    let states: Vec<ObjectState> = store.state.objects[&root]
        .history
        .iter()
        .map(|t| t.state)
        .collect();
    assert_eq!(states, [ObjectState::Publishing, ObjectState::Available]);
    assert_eq!(open_ledger(&dir).recover(&mut store), Ok(0));

    let dir = data_dir("a14-intent");
    let svc = start(&dir, CAPACITY);
    let b = input(
        b"publication intent whose chunks all survive",
        0xb1,
        Privacy::Public,
    );
    let c = input(
        b"publication intent that lost a durable chunk",
        0xc1,
        Privacy::Public,
    );
    let root_b = publish(&svc, 0xb1, &b);
    let root_c = publish(&svc, 0xc1, &c);
    let (status, before) = resolve_public(&svc, &root_c);
    assert_eq!(status, 200, "{before}");
    drop(svc);
    // Crash point: the publication intent is durable, the acknowledgement is not.
    let mut store = open_store(&dir);
    for root in [&root_b, &root_c] {
        let record = store.state.objects.get_mut(root).unwrap();
        record.state = ObjectState::Publishing;
        record.history.truncate(1);
        let session = record.session.clone();
        let session = store.state.sessions.get_mut(&session).unwrap();
        session.state = SessionState::Staging;
        session.root = None;
    }
    for tag in [0xb1, 0xc1] {
        assert!(store
            .state
            .requests
            .remove(&format!("{TENANT_A}:{}", rid(tag, 2)))
            .is_some());
    }
    store.persist().unwrap();
    let session_c = store.state.objects[&root_c].session.clone();
    fs::remove_file(dir.join("store").join("staging").join(&session_c).join("0")).unwrap();
    drop(store);

    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root_b);
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_PUBLIC"))
    );
    let (status, r) = resolve_public(&svc, &root_c);
    assert_eq!(
        (status, r["status"].as_str()),
        (404, Some("CONTENT_UNAVAILABLE"))
    );
    let staged_c = Staged {
        session: session_c.clone(),
        bytes: c.plain.clone(),
    };
    let (status, r) = finalize(&svc, &rid(0xc1, 2), &staged_c, &c);
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some(ArtifactError::Expired.name()))
    );
    assert_eq!(usage(&svc)["quota_used"], b.plain.len() as u64);
    drop(svc);
    let log = events(&dir);
    assert!(log.contains(&format!("reconcile root={root_b} ok=true")));
    assert!(log.contains(&format!("reconcile root={root_c} ok=false")));
    let mut store = open_store(&dir);
    assert!(!store.state.objects.contains_key(&root_c));
    assert_eq!(store.state.sessions[&session_c].state, SessionState::Failed);
    let session_b = &store.state.sessions[&store.state.objects[&root_b].session];
    assert_eq!(session_b.state, SessionState::Published);
    assert_eq!(session_b.root.as_deref(), Some(root_b.as_str()));
    assert_eq!(open_ledger(&dir).recover(&mut store), Ok(0));
}

#[test]
fn a15_old_backup_with_generation_seven_grant_stays_not_ready_until_revocation_recovered() {
    let dir = data_dir("a15");
    let svc = start(&dir, CAPACITY);
    let x = input(
        b"private input restored from an old backup",
        0x15,
        Privacy::Encrypted,
    );
    let root = publish(&svc, 0x15, &x);
    for n in 1..=6 {
        let (status, r) = post(
            &svc,
            "/v1/rotate",
            TOKEN_A,
            &json!({"request_id": rid(0x16, n), "root": root}),
        );
        assert_eq!(
            (status, r["access_generation"].as_u64()),
            (200, Some(u64::from(n) + 1))
        );
    }
    let granted = grant(&svc, 0x15, &root, x.subject);
    assert_eq!(granted["generation"], 7);
    let grant_id = granted["grant_id"].as_str().unwrap().to_string();
    let access = |n: u8| {
        json!({"request_id": rid(0x17, n), "root": root, "task": hex::encode(x.subject),
               "purpose": "evaluation", "grant_id": grant_id, "index": 0})
    };
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(1));
    assert_eq!((status, r["access_generation"].as_u64()), (200, Some(7)));
    drop(svc);

    let at = now_secs();
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert_eq!(
        ledger.place_hold(&mut store, TENANT_A, &root, [0x71; 32], at + 40 * DAY, at),
        Ok(at + 40 * DAY)
    );
    let session = store.state.objects[&root].session.clone();
    drop((store, ledger));
    copy_dir(&dir.join("store"), &dir.join("backup"));

    let svc = start(&dir, CAPACITY);
    let (status, r) = post(
        &svc,
        "/v1/revoke",
        TOKEN_A,
        &json!({"request_id": rid(0x18, 1), "grant_id": grant_id, "generation": 7, "sequence": 1}),
    );
    assert_eq!((status, r["revoked"].as_bool()), (200, Some(true)));
    drop(svc);
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert_eq!(
        ledger.place_hold(&mut store, TENANT_A, &root, [0x72; 32], at + 50 * DAY, at),
        Ok(at + 50 * DAY)
    );
    drop((store, ledger));
    fs::copy(dir.join("store/state.json"), dir.join("current-state.json")).unwrap();
    fs::copy(
        dir.join("store/retention.json"),
        dir.join("current-retention.json"),
    )
    .unwrap();

    fs::remove_dir_all(dir.join("store")).unwrap();
    copy_dir(&dir.join("backup"), &dir.join("store"));
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert!(!store.restore_ready());
    assert!(!ledger.ready());
    let stale = &store.state.grants[&grant_id];
    assert_eq!((stale.generation, stale.revoked), (7, false));
    assert!(store.read_chunk(&session, 0).unwrap().is_some());
    assert_eq!(ledger.recover(&mut store), Err(Refusal::RestoreNotReady));
    drop((store, ledger));
    match try_start(&dir, CAPACITY) {
        Ok(_) => panic!("a stale restore became ready"),
        Err(status) => assert_eq!(status.code(), Some(3)),
    }

    fs::copy(dir.join("current-state.json"), dir.join("store/state.json")).unwrap();
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert!(store.restore_ready());
    assert!(!ledger.ready());
    assert_eq!(ledger.recover(&mut store), Err(Refusal::RestoreNotReady));
    assert_eq!(
        ledger.place_hold(&mut store, TENANT_A, &root, [0x73; 32], at + 60 * DAY, at),
        Err(Refusal::RestoreNotReady)
    );
    assert_eq!(ledger.entry(&root).unwrap().holds.len(), 1);
    drop((store, ledger));

    fs::copy(
        dir.join("current-retention.json"),
        dir.join("store/retention.json"),
    )
    .unwrap();
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert!(ledger.ready());
    assert_eq!(ledger.recover(&mut store), Ok(0));
    let holds: Vec<u64> = ledger
        .entry(&root)
        .unwrap()
        .holds
        .values()
        .copied()
        .collect();
    assert_eq!(holds, [at + 40 * DAY, at + 50 * DAY]);
    drop((store, ledger));

    let svc = start(&dir, CAPACITY);
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(2));
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some(ArtifactError::AuthorityRevoked.name()))
    );
    assert!(r.get("object_key").is_none());
    let (status, r) = post(&svc, "/v1/content", TOKEN_B, &access(3));
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some(ArtifactError::AuthorityRevoked.name()))
    );
}

#[test]
fn a16_terminal_finality_day_zero_retention_floor_holds_and_unresolved_execution() {
    let dir = data_dir("a16");
    let svc = start(&dir, CAPACITY);
    let t = input(b"input of a cancelled task", 0x41, Privacy::Public);
    let u = input(
        b"input of an accepted task that never resolved",
        0x42,
        Privacy::Public,
    );
    let r = input(
        b"input of a task with a committed result",
        0x43,
        Privacy::Public,
    );
    let root_t = publish(&svc, 0x41, &t);
    let root_u = publish(&svc, 0x42, &u);
    let root_r = publish(&svc, 0x43, &r);
    drop(svc);
    let base = created_state();
    let state = with_tasks(
        &base,
        &[
            task(0x41, TaskStatus::Cancelled, root_digest(&root_t)),
            task(0x42, TaskStatus::Accepted, root_digest(&root_u)),
            task(0x43, TaskStatus::ResultCommitted, root_digest(&root_r)),
        ],
    );
    let (soft, finalized) = (snapshot(&state, 2), snapshot(&state, 4));
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    let d = now_secs();
    let floor = d + 30 * DAY;

    assert_eq!(
        ledger.observe_terminal(&mut store, &root_t, &soft, &state, d),
        Err(Refusal::FinalityUnavailable)
    );
    assert_eq!(
        ledger.observe_terminal(&mut store, &root_t, &finalized, &base, d),
        Err(Refusal::Artifact(ArtifactError::IntegrityConflict))
    );
    assert_eq!(
        ledger.observe_terminal(&mut store, &root_t, &finalized, &state, d),
        Ok(floor)
    );
    assert_eq!(
        ledger.observe_terminal(&mut store, &root_t, &finalized, &state, d + 5 * DAY),
        Ok(floor)
    );
    let terminal = ledger.entry(&root_t).unwrap().terminal.clone().unwrap();
    assert_eq!(terminal.task, hex::encode([0x41; 32]));
    assert_eq!(
        terminal.snapshot,
        hex::encode(finalized.snapshot_id().unwrap().bytes())
    );
    assert_eq!((terminal.observed_at, terminal.retain_until), (d, floor));
    let refused = ledger.observe_terminal(&mut store, &root_u, &finalized, &state, d);
    assert_eq!(refused, Err(Refusal::UnresolvedExecution));
    assert_eq!(refused.unwrap_err().name(), "UNRESOLVED_EXECUTION");
    assert_eq!(
        ledger.observe_terminal(&mut store, &root_r, &finalized, &state, d),
        Err(Refusal::EvidenceUnavailable)
    );
    assert!(ledger.entry(&root_u).is_none());

    let refused = ledger.tombstone(&mut store, TENANT_A, &root_t, EXPIRED, d + 29 * DAY);
    assert_eq!(refused, Err(Refusal::RetentionActive { until: floor }));
    assert_eq!(refused.unwrap_err().name(), "RETENTION_ACTIVE");
    assert_eq!(store.state.objects[&root_t].state, ObjectState::Available);
    let hold = [0x48; 32];
    assert_eq!(
        ledger.place_hold(
            &mut store,
            TENANT_A,
            &root_t,
            [0; 32],
            d + 40 * DAY,
            d + 29 * DAY
        ),
        Err(Refusal::Artifact(ArtifactError::Malformed))
    );
    assert_eq!(
        ledger.place_hold(
            &mut store,
            TENANT_A,
            &root_t,
            hold,
            d + 29 * DAY + MAX_HOLD_SECS + 1,
            d + 29 * DAY
        ),
        Err(Refusal::Artifact(ArtifactError::CapacityUnavailable))
    );
    assert_eq!(
        ledger.place_hold(
            &mut store,
            "tenant-b",
            &root_t,
            hold,
            d + 40 * DAY,
            d + 29 * DAY
        ),
        Err(Refusal::Artifact(ArtifactError::Unauthorized))
    );
    assert_eq!(
        ledger.place_hold(
            &mut store,
            TENANT_A,
            &root_t,
            hold,
            d + 40 * DAY,
            d + 29 * DAY
        ),
        Ok(d + 40 * DAY)
    );
    assert_eq!(
        ledger.place_hold(
            &mut store,
            TENANT_A,
            &root_t,
            hold,
            d + 35 * DAY,
            d + 29 * DAY
        ),
        Ok(d + 40 * DAY)
    );
    let refused = ledger.tombstone(&mut store, TENANT_A, &root_t, OWNER_WITHDRAWN, d + 30 * DAY);
    assert_eq!(
        refused,
        Err(Refusal::HoldActive {
            until: d + 40 * DAY
        })
    );
    assert_eq!(refused.unwrap_err().name(), "HOLD_ACTIVE");
    assert_eq!(
        ledger.release_hold(&mut store, TENANT_A, &root_t, hold),
        Ok(())
    );
    assert_eq!(
        ledger.release_hold(&mut store, TENANT_A, &root_t, hold),
        Err(Refusal::Artifact(ArtifactError::ContentUnavailable))
    );
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_t, OWNER_WITHDRAWN, d + 30 * DAY),
        Ok(Tombstone {
            version: 1,
            reason: OWNER_WITHDRAWN,
            at: d + 30 * DAY,
            purge_after: Some(floor),
        })
    );
    assert_eq!(store.state.objects[&root_t].retention_until, Some(floor));
    let session_t = store.state.objects[&root_t].session.clone();
    assert_eq!(
        ledger.purge(&mut store, d + 30 * DAY),
        Ok(PurgeReport {
            purged: vec![root_t.clone()],
            pending: vec![],
        })
    );
    assert_eq!(store.read_chunk(&session_t, 0), Ok(None));
    assert_eq!(
        ledger.projection(&store, &root_t),
        Ok(Projection {
            state: "PURGED",
            availability: "EVIDENCE_UNAVAILABLE",
            binding: "UPLOADED_UNBOUND",
            tombstone_reason: Some("OWNER_WITHDRAWN"),
            tombstone_version: 1,
            retain_until: Some(floor),
        })
    );
    assert_eq!(
        ledger.entry(&root_t).unwrap().history,
        [
            Event::Tombstoned {
                version: 1,
                reason: OWNER_WITHDRAWN,
                at: d + 30 * DAY,
            },
            Event::Purged { at: d + 30 * DAY },
        ]
    );

    let day45 = d + 45 * DAY;
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_u, OWNER_WITHDRAWN, day45),
        Err(Refusal::EvidenceUnavailable)
    );
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_r, EXPIRED, day45),
        Err(Refusal::EvidenceUnavailable)
    );
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_u, POLICY_RESTRICTED, day45),
        Ok(Tombstone {
            version: 1,
            reason: POLICY_RESTRICTED,
            at: day45,
            purge_after: None,
        })
    );
    assert_eq!(store.state.objects[&root_u].retention_until, None);
    assert_eq!(ledger.purge(&mut store, day45), Ok(PurgeReport::default()));
    let session_u = store.state.objects[&root_u].session.clone();
    assert_eq!(store.read_chunk(&session_u, 0), Ok(Some(u.plain.clone())));
    assert_eq!(
        ledger.projection(&store, &root_u),
        Ok(Projection {
            state: "TOMBSTONED",
            availability: "EVIDENCE_UNAVAILABLE",
            binding: "UPLOADED_UNBOUND",
            tombstone_reason: Some("POLICY_RESTRICTED"),
            tombstone_version: 1,
            retain_until: None,
        })
    );
    let used: u64 = store.state.quota_used.values().sum();
    assert_eq!(used, (u.plain.len() + r.plain.len()) as u64);
    drop((store, ledger));

    let svc = start(&dir, used);
    let (status, refused) = prepare(&svc, &rid(0x49, 1), Privacy::Public, 1);
    assert_eq!(
        (status, refused["error"].as_str()),
        (507, Some(ArtifactError::CapacityUnavailable.name()))
    );
    let (status, r) = resolve_public(&svc, &root_u);
    assert_eq!((status, r["status"].as_str()), (410, Some("TOMBSTONED")));
    drop(svc);
    let store = open_store(&dir);
    let record = &store.state.objects[&root_u];
    assert_eq!(
        (record.state, record.retention_until),
        (ObjectState::Tombstoned, None)
    );
    assert_eq!(store.read_chunk(&session_u, 0), Ok(Some(u.plain)));
}

#[test]
fn a19_unknown_or_orphaned_binding_is_never_finalized_and_keeps_evidence() {
    let dir = data_dir("a19");
    let svc = start(&dir, CAPACITY);
    let y = input(
        b"input whose binding transaction is in flight",
        0x19,
        Privacy::Public,
    );
    let root = publish(&svc, 0x19, &y);
    drop(svc);
    let base = created_state();
    let bound = with_tasks(
        &base,
        &[task(0x19, TaskStatus::Admitted, root_digest(&root))],
    );
    let other = with_tasks(&base, &[task(0x19, TaskStatus::Admitted, [0x77; 32])]);
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    let at = now_secs();
    let binding = |ledger: &Ledger, store: &Store| ledger.projection(store, &root).unwrap().binding;

    assert_eq!(binding(&ledger, &store), "UPLOADED_UNBOUND");
    assert_eq!(
        ledger.binding_intent(&mut store, &root),
        Ok(Binding::Pending)
    );
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &snapshot(&bound, 2), &bound),
        Ok(Binding::Pending)
    );
    assert_eq!(binding(&ledger, &store), "BINDING_PENDING");
    let refused = ledger.tombstone(&mut store, TENANT_A, &root, OWNER_WITHDRAWN, at);
    assert_eq!(refused, Err(Refusal::BindingPending));
    assert_eq!(refused.unwrap_err().name(), "BINDING_PENDING");
    assert_eq!(
        ledger.orphan_binding(&mut store, &root),
        Ok(Binding::Unbound)
    );
    assert_eq!(binding(&ledger, &store), "UPLOADED_UNBOUND");
    let session = store.state.objects[&root].session.clone();
    assert_eq!(store.state.objects[&root].state, ObjectState::Available);
    assert_eq!(store.read_chunk(&session, 0), Ok(Some(y.plain.clone())));
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &snapshot(&other, 4), &other),
        Err(Refusal::EvidenceUnavailable)
    );
    assert_eq!(binding(&ledger, &store), "UPLOADED_UNBOUND");
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &snapshot(&bound, 4), &bound),
        Ok(Binding::Finalized)
    );
    assert_eq!(
        ledger.orphan_binding(&mut store, &root),
        Err(Refusal::Artifact(ArtifactError::IntegrityConflict))
    );
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &snapshot(&bound, 2), &bound),
        Ok(Binding::Finalized)
    );
    assert_eq!(binding(&ledger, &store), "FINALIZED");
    drop((store, ledger));

    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root);
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_PUBLIC"))
    );
}

#[test]
fn a21_tombstoning_a_finalized_task_artifact_leaves_protocol_records_unchanged() {
    let dir = data_dir("a21");
    let svc = start(&dir, CAPACITY);
    let z = input(
        b"input of a task whose result is finalized",
        0x21,
        Privacy::Public,
    );
    let root = publish(&svc, 0x21, &z);
    drop(svc);
    let state = with_tasks(
        &created_state(),
        &[task(0x21, TaskStatus::ResultCommitted, root_digest(&root))],
    );
    let finalized = snapshot(&state, 4);
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    let at = now_secs();
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &finalized, &state),
        Ok(Binding::Finalized)
    );
    let state_digest = codec::state_digest(&state).unwrap();
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root, POLICY_RESTRICTED, at),
        Ok(Tombstone {
            version: 1,
            reason: POLICY_RESTRICTED,
            at,
            purge_after: None,
        })
    );
    assert_eq!(codec::state_digest(&state).unwrap(), state_digest);
    assert_eq!(snapshot(&state, 4), finalized);
    assert_eq!(
        ledger.observe_binding(&mut store, &root, &finalized, &state),
        Ok(Binding::Finalized)
    );
    assert_eq!(
        ledger.projection(&store, &root),
        Ok(Projection {
            state: "TOMBSTONED",
            availability: "EVIDENCE_UNAVAILABLE",
            binding: "FINALIZED",
            tombstone_reason: Some("POLICY_RESTRICTED"),
            tombstone_version: 1,
            retain_until: None,
        })
    );
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root, POLICY_RESTRICTED, at),
        Err(Refusal::Artifact(ArtifactError::Tombstoned))
    );
    assert_eq!(
        ledger.entry(&root).unwrap().history,
        [Event::Tombstoned {
            version: 1,
            reason: POLICY_RESTRICTED,
            at,
        }]
    );
    drop((store, ledger));

    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root);
    assert_eq!((status, r["status"].as_str()), (410, Some("TOMBSTONED")));
    assert!(r.get("envelope").is_none());
    let (status, r) = post(
        &svc,
        "/v1/content",
        TOKEN_B,
        &json!({"root": root, "purpose": "evaluation", "index": 0}),
    );
    assert_eq!(
        (status, r["error"].as_str()),
        (410, Some(ArtifactError::Tombstoned.name()))
    );
}

#[test]
fn r16_r20_pending_purge_reinstatement_and_tombstone_intent_recovery() {
    let dir = data_dir("r16-r20");
    let svc = start(&dir, CAPACITY);
    let p = input(b"input whose purge fails once", 0x50, Privacy::Public);
    let q = input(
        b"input reinstated on a current grant",
        0x51,
        Privacy::Public,
    );
    let root_p = publish(&svc, 0x50, &p);
    let root_q = publish(&svc, 0x51, &q);
    let grant_q = grant(&svc, 0x51, &root_q, q.subject);
    let grant_p = grant(&svc, 0x50, &root_p, p.subject);
    let (grant_q, expires) = (
        grant_q["grant_id"].as_str().unwrap().to_string(),
        grant_q["expires_at"].as_u64().unwrap(),
    );
    let grant_p = grant_p["grant_id"].as_str().unwrap().to_string();
    drop(svc);
    let state = with_tasks(
        &created_state(),
        &[task(0x50, TaskStatus::Cancelled, root_digest(&root_p))],
    );
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    let d = now_secs();
    let floor = d + 30 * DAY;

    assert_eq!(
        ledger.observe_terminal(&mut store, &root_p, &snapshot(&state, 4), &state, d),
        Ok(floor)
    );
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_p, EXPIRED, floor),
        Ok(Tombstone {
            version: 1,
            reason: EXPIRED,
            at: floor,
            purge_after: Some(floor),
        })
    );
    let staging = dir
        .join("store")
        .join("staging")
        .join(&store.state.objects[&root_p].session);
    fs::rename(&staging, dir.join("parked")).unwrap();
    fs::write(&staging, b"not a directory").unwrap();
    assert_eq!(
        ledger.purge(&mut store, floor),
        Ok(PurgeReport {
            purged: vec![],
            pending: vec![root_p.clone()],
        })
    );
    assert_eq!(
        ledger.projection(&store, &root_p),
        Ok(Projection {
            state: "TOMBSTONED_PENDING_PURGE",
            availability: "UNAVAILABLE",
            binding: "UPLOADED_UNBOUND",
            tombstone_reason: Some("EXPIRED"),
            tombstone_version: 1,
            retain_until: Some(floor),
        })
    );
    let log = events(&dir);
    assert!(log.contains(&format!("purge-pending root={root_p}")));
    assert!(log.contains(&format!("purge-alert root={root_p} attempts=1")));
    fs::remove_file(&staging).unwrap();
    fs::rename(dir.join("parked"), &staging).unwrap();
    assert_eq!(
        ledger.purge(&mut store, floor + DAY),
        Ok(PurgeReport {
            purged: vec![root_p.clone()],
            pending: vec![],
        })
    );
    assert_eq!(store.state.objects[&root_p].state, ObjectState::Purged);
    assert!(!staging.exists());
    assert_eq!(
        ledger.entry(&root_p).unwrap().history,
        [
            Event::Tombstoned {
                version: 1,
                reason: EXPIRED,
                at: floor,
            },
            Event::PurgeFailed {
                attempt: 1,
                at: floor,
            },
            Event::Purged { at: floor + DAY },
        ]
    );
    let decision = [0xd5; 32];
    assert_eq!(
        ledger.reinstate(&mut store, TENANT_A, &root_p, &grant_p, decision, d),
        Err(Refusal::Artifact(ArtifactError::ContentUnavailable))
    );

    let at = now_secs();
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_q, POLICY_RESTRICTED, at),
        Ok(Tombstone {
            version: 1,
            reason: POLICY_RESTRICTED,
            at,
            purge_after: None,
        })
    );
    assert_eq!(
        ledger.reinstate(&mut store, TENANT_A, &root_q, &grant_q, [0; 32], at),
        Err(Refusal::Artifact(ArtifactError::Malformed))
    );
    assert_eq!(
        ledger.reinstate(&mut store, TENANT_A, &root_q, &grant_p, decision, at),
        Err(Refusal::Artifact(ArtifactError::Unauthorized))
    );
    assert_eq!(
        ledger.reinstate(&mut store, "tenant-b", &root_q, &grant_q, decision, at),
        Err(Refusal::Artifact(ArtifactError::Unauthorized))
    );
    assert_eq!(
        ledger.reinstate(&mut store, TENANT_A, &root_q, &grant_q, decision, expires),
        Err(Refusal::Artifact(ArtifactError::Expired))
    );
    assert_eq!(store.state.objects[&root_q].state, ObjectState::Tombstoned);
    assert_eq!(
        ledger.reinstate(&mut store, TENANT_A, &root_q, &grant_q, decision, at),
        Ok(())
    );
    let record = &store.state.objects[&root_q];
    let states: Vec<ObjectState> = record.history.iter().map(|t| t.state).collect();
    assert_eq!(
        states,
        [
            ObjectState::Publishing,
            ObjectState::Available,
            ObjectState::Tombstoned,
            ObjectState::Available,
        ]
    );
    assert_eq!(record.retention_until, None);
    assert_eq!(
        ledger.entry(&root_q).unwrap().history,
        [
            Event::Tombstoned {
                version: 1,
                reason: POLICY_RESTRICTED,
                at,
            },
            Event::Reinstated {
                grant: grant_q.clone(),
                decision: hex::encode(decision),
                at,
            },
        ]
    );
    assert_eq!(
        ledger.projection(&store, &root_q),
        Ok(Projection {
            state: "AVAILABLE",
            availability: "AVAILABLE",
            binding: "UPLOADED_UNBOUND",
            tombstone_reason: None,
            tombstone_version: 1,
            retain_until: None,
        })
    );
    drop((store, ledger));
    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root_q);
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_PUBLIC"))
    );
    drop(svc);

    // Crash point: the tombstone intent is durable in the ledger, the store
    // snapshot and its live mark are not.
    let state_file = dir.join("store").join("state.json");
    let (saved_state, saved_mark) = (
        fs::read(&state_file).unwrap(),
        fs::read(mark_file(&dir)).unwrap(),
    );
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    let later = now_secs();
    assert_eq!(
        ledger.tombstone(&mut store, TENANT_A, &root_q, INTEGRITY_FAILURE, later),
        Ok(Tombstone {
            version: 2,
            reason: INTEGRITY_FAILURE,
            at: later,
            purge_after: None,
        })
    );
    drop((store, ledger));
    fs::write(&state_file, saved_state).unwrap();
    fs::write(mark_file(&dir), saved_mark).unwrap();
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert!(store.restore_ready());
    assert_eq!(store.state.objects[&root_q].state, ObjectState::Available);
    assert_eq!(ledger.recover(&mut store), Ok(1));
    let record = &store.state.objects[&root_q];
    assert_eq!(
        (record.state, record.tombstone_reason),
        (ObjectState::Tombstoned, Some(INTEGRITY_FAILURE))
    );
    assert_eq!(ledger.recover(&mut store), Ok(0));
    assert!(events(&dir).contains(&format!("reconcile tombstone root={root_q}")));
    drop((store, ledger));
    let svc = start(&dir, CAPACITY);
    let (status, r) = resolve_public(&svc, &root_q);
    assert_eq!((status, r["status"].as_str()), (410, Some("TOMBSTONED")));
}
