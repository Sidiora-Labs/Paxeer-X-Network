//! AI.F09-A07..A13, A17 and the R11/R15/R20 recovery paths against the actual
//! service binary as a child process over real TLS on loopback (one explicitly
//! plaintext listener), its real on-disk store, real Ed25519 publisher
//! envelopes, the local file key provider and real XChaCha20-Poly1305 frames
//! sealed by an independent encoder written from the V1 private-object profile.
//! Tombstone deadlines come from the retention ledger: terminal finality is
//! observed through the service route on market state built by the real F01
//! CREATE.
#[path = "../src/crypto.rs"]
#[allow(dead_code)]
mod crypto;
#[path = "../src/resolver.rs"]
#[allow(dead_code)]
mod resolver;
#[path = "../src/retention.rs"]
#[allow(dead_code)]
mod retention;
#[path = "../src/store.rs"]
#[allow(dead_code)]
mod store;

use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{XChaCha20Poly1305, XNonce};
use crypto::{open_stream, FrameContext, MAX_FRAME_PLAINTEXT};
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::codec::{self, derive_market};
use layerx_programs_ai_market::dispatch;
use layerx_programs_ai_market::evidence::*;
use layerx_programs_ai_market::policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES};
use layerx_programs_ai_market::registry::{derive_rewards_account, F01_SECTION_CAP};
use layerx_programs_ai_market::registry_ops::{self, CallContext, Outcome, PolicySection};
use layerx_programs_ai_market::state::{self, Section};
use layerx_programs_ai_market::tasks::{TaskBinding, TaskStatus};
use layerx_programs_ai_market::types::*;
use layerx_programs_ai_market::MAX_EVENT_BYTES;
use retention::{Ledger, PurgeReport};
use rustls::pki_types::{CertificateDer, ServerName};
use rustls::{ClientConfig, ClientConnection, RootCertStore, StreamOwned};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::cell::Cell;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{IpAddr, Shutdown, TcpListener, TcpStream};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::{Arc, OnceLock};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use store::{Delivery, DeliveryState, ObjectState, Store};

const TOKEN_A: &str = "tenant-a-publisher-token";
const TOKEN_B: &str = "tenant-b-consumer-token";
const TOKEN_O: &str = "finality-observer-token";
const P1: [u8; 32] = [0x55; 32];
const P7: [u8; 32] = [0x57; 32];
const TASK: [u8; 32] = [0x31; 32];

fn sha(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}
fn key1() -> SigningKey {
    SigningKey::from_bytes(&[1; 32])
}
fn key7() -> SigningKey {
    SigningKey::from_bytes(&[7; 32])
}
/// The market is the one the F01 CREATE derives, so finality observations of
/// its state bind to these artifacts.
fn ctx() -> ArtifactContext {
    let chain = ChainDomain::new([0x11; 32]).unwrap();
    let program = ProgramId::new([0x22; 32]).unwrap();
    ArtifactContext {
        chain,
        program,
        market: derive_market(chain, program).unwrap(),
        policy: PolicyDigest::new([0x44; 32]).unwrap(),
    }
}
fn frame_ctx() -> FrameContext {
    FrameContext {
        context: ctx().bytes(),
        subject: TASK,
        kind: ArtifactKind::Result as u8,
    }
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
fn seal(key: &[u8; 32], plain: &[u8]) -> Vec<u8> {
    seal_with(key, &frame_ctx(), plain)
}
fn sealed_len(plain: usize) -> usize {
    let body = plain + 8;
    body + body.div_ceil(262_144) * (34 + 16)
}
/// Consumer-side open: plaintext is kept only when the whole stream verifies.
fn open_with(key: &[u8; 32], ctx: &FrameContext, stream: &[u8]) -> Result<Vec<u8>, ArtifactError> {
    let mut out = Vec::new();
    let result = open_stream(key, ctx, stream, |part| out.extend_from_slice(part));
    result.map(|_| out)
}
fn open(key: &[u8; 32], stream: &[u8]) -> Result<Vec<u8>, ArtifactError> {
    open_with(key, &frame_ctx(), stream)
}
fn rid(tag: u8, n: u8) -> String {
    let mut id = [tag; 32];
    id[31] = n;
    hex::encode(id)
}

/// One locally generated self-signed loopback certificate per test process.
struct Cert {
    cert_pem: String,
    key_pem: String,
    client: Arc<ClientConfig>,
}
fn client_trusting(der: CertificateDer<'static>) -> Arc<ClientConfig> {
    let mut roots = RootCertStore::empty();
    roots.add(der).unwrap();
    Arc::new(
        ClientConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_root_certificates(roots)
            .with_no_client_auth(),
    )
}
fn cert() -> &'static Cert {
    static CERT: OnceLock<Cert> = OnceLock::new();
    CERT.get_or_init(|| {
        let issued = rcgen::generate_simple_self_signed(vec!["localhost".to_string()]).unwrap();
        Cert {
            cert_pem: issued.cert.pem(),
            key_pem: issued.signing_key.serialize_pem(),
            client: client_trusting(issued.cert.der().clone()),
        }
    })
}

struct Svc {
    child: Child,
    addr: String,
    dir: PathBuf,
    tls: bool,
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
        "ai-artifacts-{name}-{}-{nanos}",
        std::process::id()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

fn mark_file(dir: &Path) -> PathBuf {
    dir.join("revocation.mark")
}
fn open_store(dir: &Path) -> Store {
    Store::open(&dir.join("store"), &mark_file(dir)).unwrap()
}
fn open_ledger(dir: &Path) -> Ledger {
    Ledger::open(&dir.join("store"), &dir.join("retention.mark")).unwrap()
}

fn start(dir: &Path) -> Svc {
    start_with(dir, true)
}
fn start_with(dir: &Path, tls: bool) -> Svc {
    try_start(dir, tls).expect("service did not start")
}
/// Starts the binary; a refused startup returns its exit status.
fn try_start(dir: &Path, tls: bool) -> Result<Svc, ExitStatus> {
    let config = dir.join("config.json");
    if !config.exists() {
        let listener = if tls {
            std::fs::write(dir.join("cert.pem"), &cert().cert_pem).unwrap();
            std::fs::write(dir.join("key.pem"), &cert().key_pem).unwrap();
            json!({"tls": {"certificate": dir.join("cert.pem"), "private_key": dir.join("key.pem")}})
        } else {
            json!("plaintext")
        };
        let cfg = json!({
            "capacity_bytes": 64u64 << 20,
            "listener": listener,
            "revocation_mark": mark_file(dir),
            "retention_mark": dir.join("retention.mark"),
            "finality_observers": ["observer"],
            "tenants": [
                {"id": "tenant-a", "token_sha256": hex::encode(sha(TOKEN_A.as_bytes())), "quota_bytes": 8u64 << 20,
                 "locator_hosts": ["objects.example", "169.254.169.254", "::1", "localhost", "93.184.216.34"]},
                {"id": "tenant-b", "token_sha256": hex::encode(sha(TOKEN_B.as_bytes())), "quota_bytes": 1u64 << 20},
                {"id": "observer", "token_sha256": hex::encode(sha(TOKEN_O.as_bytes())), "quota_bytes": 0},
            ],
            "publishers": [
                {"tenant": "tenant-a", "principal": hex::encode(P1), "generation": 1, "key": hex::encode(key1().verifying_key().to_bytes())},
                {"tenant": "tenant-a", "principal": hex::encode(P7), "generation": 7, "key": hex::encode(key7().verifying_key().to_bytes())},
            ],
        });
        std::fs::write(&config, cfg.to_string()).unwrap();
        std::fs::write(dir.join("kek.bin"), crypto::random::<32>().unwrap()).unwrap();
    }
    let log = std::fs::OpenOptions::new()
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
    Ok(Svc {
        child,
        addr,
        dir: dir.to_path_buf(),
        tls,
    })
}

fn exchange(stream: &mut (impl Read + Write), request: &[u8]) -> Vec<u8> {
    stream.write_all(request).unwrap();
    let mut out = Vec::new();
    stream.read_to_end(&mut out).unwrap();
    out
}
/// One request per connection: verified TLS against the locally generated
/// certificate for "localhost", or plaintext only for an explicitly
/// plaintext service.
fn http(svc: &Svc, method: &str, path: &str, token: &str, body: &[u8]) -> (u16, Vec<u8>) {
    let head = format!("{method} {path} HTTP/1.1\r\nHost: artifacts\r\nAuthorization: Bearer {token}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", body.len());
    let request = [head.as_bytes(), body].concat();
    let mut tcp = TcpStream::connect(&svc.addr).unwrap();
    let out = if svc.tls {
        let name = ServerName::try_from("localhost").unwrap();
        let conn = ClientConnection::new(cert().client.clone(), name).unwrap();
        exchange(&mut StreamOwned::new(conn, tcp), &request)
    } else {
        exchange(&mut tcp, &request)
    };
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
fn post_raw(svc: &Svc, path: &str, token: &str, body: &[u8]) -> (u16, Value) {
    let (status, bytes) = http(svc, "POST", path, token, body);
    (status, serde_json::from_slice(&bytes).unwrap())
}

struct Object<'a> {
    plain: Vec<u8>,
    privacy: Privacy,
    principal: [u8; 32],
    generation: u64,
    key: SigningKey,
    declaration: Option<&'a [u8]>,
}

struct Staged {
    session: String,
    object_key: Option<[u8; 32]>,
    bytes: Vec<u8>,
}

fn manifest_for(o: &Object<'_>, bytes: &[u8]) -> Vec<u8> {
    let count = chunk_count(bytes.len() as u64).unwrap();
    let mut scratch = vec![[0u8; 32]; count as usize];
    let manifest = ArtifactManifest {
        kind: ArtifactKind::Result,
        privacy: o.privacy,
        context: ctx(),
        epoch: 9,
        publisher: PrincipalId::new(o.principal).unwrap(),
        subject: TASK,
        byte_length: bytes.len() as u64,
        chunk_count: count,
        content_root: object_content_root(bytes, &mut scratch).unwrap(),
        parents: Items::Typed(&[]),
        declaration_root: o
            .declaration
            .map_or([0; 32], |d| declaration_root(&ctx(), d).unwrap().bytes()),
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
fn envelope_for(o: &Object<'_>, bytes: &[u8]) -> Vec<u8> {
    let manifest = manifest_for(o, bytes);
    let generation = Version::new(o.generation).unwrap();
    let digest = publisher_digest(&ctx(), manifest_root(&manifest).unwrap(), generation);
    let envelope = PublisherEnvelope {
        manifest: &manifest,
        generation,
        key: PublicKey32(o.key.verifying_key().to_bytes()),
        signature: Signature64(o.key.sign(&digest).to_bytes()),
    };
    let mut out = vec![0; MAX_ENVELOPE_BYTES];
    let n = encode_envelope(&envelope, &mut out).unwrap();
    out.truncate(n);
    out
}

fn prepare(svc: &Svc, request: &str, privacy: Privacy, byte_length: usize) -> Value {
    let (status, prepared) = post(
        svc,
        "/v1/prepare",
        TOKEN_A,
        &json!({
            "request_id": request, "kind": ArtifactKind::Result as u8, "privacy": privacy as u8,
            "context": hex::encode(ctx().bytes()), "byte_length": byte_length,
        }),
    );
    assert_eq!(status, 200, "{prepared}");
    assert_eq!(prepared["state"], "STAGING");
    prepared
}
fn upload(svc: &Svc, session: &str, bytes: &[u8]) {
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
}
/// PrepareObject (the service issues a fresh object key for a private object),
/// client-side sealing under that key, then every upload frame.
fn stage(svc: &Svc, request: &str, o: &Object<'_>) -> Staged {
    let length = match o.privacy {
        Privacy::Public => o.plain.len(),
        Privacy::Encrypted => sealed_len(o.plain.len()),
    };
    let prepared = prepare(svc, request, o.privacy, length);
    let session = prepared["session"].as_str().unwrap().to_string();
    let object_key: Option<[u8; 32]> = prepared["object_key"]
        .as_str()
        .map(|k| hex::decode(k).unwrap().try_into().unwrap());
    assert_eq!(object_key.is_some(), o.privacy == Privacy::Encrypted);
    let bytes = match &object_key {
        Some(key) => seal(key, &o.plain),
        None => o.plain.clone(),
    };
    assert_eq!(bytes.len(), length);
    upload(svc, &session, &bytes);
    Staged {
        session,
        object_key,
        bytes,
    }
}
fn finalize_body(request: &str, staged: &Staged, o: &Object<'_>) -> Vec<u8> {
    json!({
        "request_id": request, "session": staged.session, "envelope": hex::encode(envelope_for(o, &staged.bytes)),
        "declaration": o.declaration.map(hex::encode),
    })
    .to_string()
    .into_bytes()
}
fn publish(svc: &Svc, tag: u8, o: &Object<'_>) -> (String, Staged) {
    let staged = stage(svc, &rid(tag, 1), o);
    let (status, receipt) = post_raw(
        svc,
        "/v1/finalize",
        TOKEN_A,
        &finalize_body(&rid(tag, 2), &staged, o),
    );
    assert_eq!(status, 200, "{receipt}");
    assert_eq!(receipt["protocol_receipt"], false);
    assert_eq!(receipt["signature_status"], "VALID");
    (receipt["root"].as_str().unwrap().to_string(), staged)
}
fn private(plain: &[u8], principal: [u8; 32], generation: u64, key: SigningKey) -> Object<'static> {
    Object {
        plain: plain.to_vec(),
        privacy: Privacy::Encrypted,
        principal,
        generation,
        key,
        declaration: None,
    }
}
fn public(bytes: &[u8]) -> Object<'static> {
    Object {
        plain: bytes.to_vec(),
        privacy: Privacy::Public,
        principal: P1,
        generation: 1,
        key: key1(),
        declaration: None,
    }
}
fn fetch(svc: &Svc, token: &str, access: &Value, chunks: u32) -> Vec<u8> {
    let mut out = Vec::new();
    for index in 0..chunks {
        let mut req = access.clone();
        req["index"] = json!(index);
        let (status, bytes) = http(
            svc,
            "POST",
            "/v1/content",
            token,
            req.to_string().as_bytes(),
        );
        assert_eq!(status, 200);
        out.extend_from_slice(&bytes);
    }
    out
}
fn grant(svc: &Svc, tag: u8, root: &str, ttl: u64, purposes: u16) -> String {
    let (status, g) = post(
        svc,
        "/v1/grants",
        TOKEN_A,
        &json!({
            "request_id": rid(tag, 9), "root": root, "task": hex::encode(TASK), "grantee": "tenant-b",
            "purposes": purposes, "ttl_secs": ttl,
        }),
    );
    assert_eq!(status, 200, "{g}");
    g["grant_id"].as_str().unwrap().to_string()
}

#[test]
fn a07_reused_request_id_with_different_manifest_conflicts_across_restart() {
    let dir = data_dir("a07");
    let first_object = public(b"first result bytes");
    let second_object = public(b"second, byte-different result");
    let svc = start_with(&dir, false);
    let reused = rid(0x70, 7);
    assert!(!svc.tls, "explicitly plaintext listener");
    let s1 = stage(&svc, &rid(0x71, 1), &first_object);
    let first = finalize_body(&reused, &s1, &first_object);
    let (status, receipt) = post_raw(&svc, "/v1/finalize", TOKEN_A, &first);
    assert_eq!(status, 200, "{receipt}");
    let s2 = stage(&svc, &rid(0x72, 1), &second_object);
    let second = finalize_body(&reused, &s2, &second_object);
    let (status, refused) = post_raw(&svc, "/v1/finalize", TOKEN_A, &second);
    assert_eq!(
        (status, refused["error"].as_str()),
        (409, Some("IdempotencyConflict"))
    );
    drop(svc);
    let svc = start_with(&dir, false);
    let (status, refused) = post_raw(&svc, "/v1/finalize", TOKEN_A, &second);
    assert_eq!(
        (status, refused["error"].as_str()),
        (409, Some("IdempotencyConflict"))
    );
    let (status, replay) = post_raw(&svc, "/v1/finalize", TOKEN_A, &first);
    assert_eq!(status, 200);
    assert_eq!(replay, receipt, "first canonical disposition is retained");
    let (_, usage) = http(&svc, "GET", "/v1/usage", TOKEN_A, b"");
    let usage: Value = serde_json::from_slice(&usage).unwrap();
    assert_eq!(usage["objects"], 1);
}

#[test]
fn a08_concurrent_identical_finalize_records_one_root_and_one_lease() {
    let dir = data_dir("a08");
    let svc = start(&dir);
    let object = public(&vec![0xab; CHUNK_BYTES as usize + 1234]);
    let staged = stage(&svc, &rid(0x80, 1), &object);
    let body = finalize_body(&rid(0x80, 2), &staged, &object);
    let replies: Vec<(u16, Vec<u8>)> = std::thread::scope(|scope| {
        let handles: Vec<_> = (0..2)
            .map(|_| scope.spawn(|| http(&svc, "POST", "/v1/finalize", TOKEN_A, &body)))
            .collect();
        handles.into_iter().map(|h| h.join().unwrap()).collect()
    });
    assert_eq!(replies[0].0, 200);
    assert_eq!(
        replies[0], replies[1],
        "both callers reconcile to one disposition"
    );
    let other = finalize_body(&rid(0x80, 3), &staged, &object);
    let (status, again) = post_raw(&svc, "/v1/finalize", TOKEN_A, &other);
    assert_eq!(status, 200);
    let first: Value = serde_json::from_slice(&replies[0].1).unwrap();
    assert_eq!(again, first);
    let (_, usage) = http(&svc, "GET", "/v1/usage", TOKEN_A, b"");
    let usage: Value = serde_json::from_slice(&usage).unwrap();
    assert_eq!(usage["objects"], 1);
    assert_eq!(
        usage["quota_used"],
        object.plain.len() as u64,
        "no duplicate usage"
    );
    let state: Value =
        serde_json::from_slice(&std::fs::read(svc.dir.join("store/state.json")).unwrap()).unwrap();
    assert_eq!(state["objects"].as_object().unwrap().len(), 1);
}

#[test]
fn a09_expired_grant_or_revoked_generation7_refuses_key_release() {
    let dir = data_dir("a09");
    let svc = start(&dir);
    let plaintext = b"private task input for generation seven";
    let o = private(plaintext, P7, 7, key7());
    let (root, _) = publish(&svc, 0x90, &o);
    let access = |g: &str| json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});

    let expired = grant(&svc, 0x91, &root, 0, 1);
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&expired));
    assert_eq!(
        (status, r["error"].as_str(), r["status"].as_str()),
        (403, Some("Expired"), Some("EXPIRED"))
    );
    assert!(r.get("object_key").is_none());

    let live = grant(&svc, 0x92, &root, 3600, 1);
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&live));
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_AUTHORIZED")),
        "{r}"
    );
    let released: [u8; 32] = hex::decode(r["object_key"].as_str().unwrap())
        .unwrap()
        .try_into()
        .unwrap();
    let stream = fetch(
        &svc,
        TOKEN_B,
        &access(&live),
        r["chunk_count"].as_u64().unwrap() as u32,
    );
    assert_eq!(open(&released, &stream).unwrap(), plaintext);
    let (status, _) = post(&svc, "/v1/resolve", "not-a-tenant", &access(&live));
    assert_eq!(status, 401);

    let revoke = |tag: u8, generation: u64, sequence: u64| {
        post(
            &svc,
            "/v1/revoke",
            TOKEN_A,
            &json!({
        "request_id": rid(tag, 4), "grant_id": live, "generation": generation, "sequence": sequence}),
        )
    };
    assert_eq!(revoke(0x93, 1, 1).0, 200);
    assert_eq!(
        revoke(0x94, 1, 1).0,
        200,
        "same sequence and payload is idempotent"
    );
    assert_eq!(revoke(0x95, 2, 1).1["error"], "IdempotencyConflict");
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&live));
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("AuthorityRevoked"))
    );

    let fresh = grant(&svc, 0x96, &root, 3600, 1);
    let (status, r) = post(
        &svc,
        "/v1/publishers/revoke",
        TOKEN_A,
        &json!({
        "request_id": rid(0x97, 1), "principal": hex::encode(P7), "generation": 7}),
    );
    assert_eq!(status, 200, "{r}");
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&fresh));
    assert_eq!(
        (status, r["error"].as_str(), r["status"].as_str()),
        (403, Some("AuthorityRevoked"), Some("ACCESS_DENIED"))
    );
    assert!(r.get("object_key").is_none());
    assert_eq!(
        r["attribution"]["generation"], 7,
        "historical signature stays attributable"
    );
    assert_eq!(r["attribution"]["signature_status"], "VALID");
    let mut req = access(&fresh);
    req["index"] = json!(0);
    let (status, _) = http(
        &svc,
        "POST",
        "/v1/content",
        TOKEN_B,
        req.to_string().as_bytes(),
    );
    assert_eq!(status, 403);

    let next = private(b"new", P7, 7, key7());
    let staged = stage(&svc, &rid(0x98, 1), &next);
    let (status, r) = post_raw(
        &svc,
        "/v1/finalize",
        TOKEN_A,
        &finalize_body(&rid(0x98, 2), &staged, &next),
    );
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("AuthorityRevoked")),
        "no new authority from a revoked generation"
    );
}

fn scan(dir: &Path, needles: &[Vec<u8>]) {
    for entry in std::fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        if path.is_dir() {
            scan(&path, needles);
            continue;
        }
        if path.file_name().is_some_and(|n| n == "kek.bin") {
            continue;
        }
        let bytes = std::fs::read(&path).unwrap();
        for needle in needles {
            assert!(
                !bytes.windows(needle.len()).any(|w| w == needle.as_slice()),
                "secret material in {}",
                path.display()
            );
        }
    }
}

#[test]
fn a10_same_private_input_under_distinct_keys_has_distinct_roots_and_no_leak() {
    let dir = data_dir("a10");
    let svc = start(&dir);
    let plaintext = *b"8bytes!!";
    let (r1, s1) = publish(&svc, 0xa1, &private(&plaintext, P1, 1, key1()));
    let (r2, s2) = publish(&svc, 0xa2, &private(&plaintext, P1, 1, key1()));
    let (k1, k2) = (s1.object_key.unwrap(), s2.object_key.unwrap());
    assert_ne!(k1, k2, "distinct object keys");
    assert_ne!(r1, r2, "public roots differ");
    let mut opened = Vec::new();
    for (tag, root) in [(0xa3, &r1), (0xa4, &r2)] {
        let g = grant(&svc, tag, root, 3600, 1);
        let access = json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});
        let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
        assert_eq!(status, 200, "{r}");
        let key: [u8; 32] = hex::decode(r["object_key"].as_str().unwrap())
            .unwrap()
            .try_into()
            .unwrap();
        let stream = fetch(&svc, TOKEN_B, &access, 1);
        opened.push(open(&key, &stream).unwrap());
    }
    assert_eq!(opened[0], plaintext);
    assert_eq!(opened[0], opened[1]);
    let digest = sha(&plaintext);
    let needles: Vec<Vec<u8>> = vec![
        plaintext.to_vec(),
        hex::encode(plaintext).into_bytes(),
        k1.to_vec(),
        k2.to_vec(),
        hex::encode(k1).into_bytes(),
        hex::encode(k2).into_bytes(),
        digest.to_vec(),
        hex::encode(digest).into_bytes(),
    ];
    scan(&dir, &needles);
}

#[test]
fn a11_swapped_frames_or_altered_nonce_tag_release_nothing() {
    let dir = data_dir("a11");
    let svc = start(&dir);
    let key = crypto::random::<32>().unwrap();
    let plaintext: Vec<u8> = (0..300_000u32).map(|i| (i % 251) as u8).collect();
    let stream = seal(&key, &plaintext);
    assert_eq!(open(&key, &stream).unwrap(), plaintext);
    let frame0 = crypto::FRAME_HEADER_BYTES + MAX_FRAME_PLAINTEXT + crypto::TAG_BYTES;
    let swapped = [&stream[frame0..], &stream[..frame0]].concat();
    assert_eq!(open(&key, &swapped), Err(ArtifactError::IntegrityConflict));
    let mut relabeled = stream.clone();
    relabeled[frame0 + 5] = 0;
    assert_eq!(
        open(&key, &relabeled),
        Err(ArtifactError::IntegrityConflict)
    );
    let mut nonce = stream.clone();
    nonce[6] ^= 1;
    assert_eq!(open(&key, &nonce), Err(ArtifactError::IntegrityConflict));
    let mut tag = stream.clone();
    *tag.last_mut().unwrap() ^= 1;
    assert_eq!(open(&key, &tag), Err(ArtifactError::IntegrityConflict));
    assert_eq!(
        open(&key, &stream[..frame0]),
        Err(ArtifactError::LengthMismatch),
        "truncation releases nothing"
    );
    let mut trailing = stream.clone();
    trailing.push(0);
    assert_eq!(open(&key, &trailing), Err(ArtifactError::IntegrityConflict));
    let mut other_task = frame_ctx();
    other_task.subject = [0x32; 32];
    assert_eq!(
        open_with(&key, &other_task, &stream),
        Err(ArtifactError::IntegrityConflict)
    );

    let o = private(&plaintext, P1, 1, key1());
    let (root, staged) = publish(&svc, 0xb1, &o);
    assert_ne!(staged.object_key, Some(key));
    let g = grant(&svc, 0xb2, &root, 3600, 1);
    let access = json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
    assert_eq!(status, 200);
    let released: [u8; 32] = hex::decode(r["object_key"].as_str().unwrap())
        .unwrap()
        .try_into()
        .unwrap();
    assert_eq!(Some(released), staged.object_key);
    let fetched = fetch(
        &svc,
        TOKEN_B,
        &access,
        r["chunk_count"].as_u64().unwrap() as u32,
    );
    assert_eq!(open(&released, &fetched).unwrap(), plaintext);
    let (status, rotated) = post(
        &svc,
        "/v1/rotate",
        TOKEN_A,
        &json!({"request_id": rid(0xb3, 1), "root": root}),
    );
    assert_eq!(
        (status, rotated["access_generation"].as_u64()),
        (200, Some(2))
    );
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("AuthorityRevoked")),
        "prior access generation invalidated"
    );

    // Frames sealed under a key other than the issued object key never publish.
    let wrong = private(b"sealed under a foreign key", P1, 1, key1());
    let length = sealed_len(wrong.plain.len());
    let prepared = prepare(&svc, &rid(0xb4, 1), Privacy::Encrypted, length);
    let session = prepared["session"].as_str().unwrap().to_string();
    let foreign = seal(&crypto::random::<32>().unwrap(), &wrong.plain);
    upload(&svc, &session, &foreign);
    let staged = Staged {
        session,
        object_key: None,
        bytes: foreign,
    };
    let (status, r) = post_raw(
        &svc,
        "/v1/finalize",
        TOKEN_A,
        &finalize_body(&rid(0xb4, 2), &staged, &wrong),
    );
    assert_eq!(
        (status, r["error"].as_str()),
        (409, Some("IntegrityConflict")),
        "{r}"
    );

    // Damaged durable bytes of an acknowledged root quarantine it on retrieval.
    let damaged = public(&vec![0x5a; CHUNK_BYTES as usize + 10]);
    let (root, staged) = publish(&svc, 0xb5, &damaged);
    let chunk = svc
        .dir
        .join("store/staging")
        .join(&staged.session)
        .join("1");
    std::fs::write(&chunk, [0x5a; 3]).unwrap();
    let access = json!({"root": root, "purpose": "inference", "index": 1});
    let (status, body) = http(
        &svc,
        "POST",
        "/v1/content",
        TOKEN_B,
        access.to_string().as_bytes(),
    );
    let body: Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(
        (status, body["status"].as_str()),
        (409, Some("INTEGRITY_FAILED"))
    );
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "inference"}),
    );
    assert_eq!(
        (status, r["status"].as_str()),
        (409, Some("INTEGRITY_FAILED"))
    );
}

#[test]
fn a12_inference_only_declaration_refuses_training_and_documented_is_not_clearance() {
    let dir = data_dir("a12");
    let svc = start(&dir);
    let documents = [RightsDocument {
        role: DocumentRole::License,
        root: [0x77; 32],
        label: "provider license",
    }];
    let declaration = Declaration {
        publisher: PrincipalId::new(P1).unwrap(),
        rights: RightsStatus::Documented,
        purpose_mask: 1,
        restriction_mask: 0,
        valid_until_height: 0,
        documents: Items::Typed(&documents),
        review_reference_root: [0; 32],
    };
    let mut bytes = vec![0; MAX_RECORD_BYTES];
    let n = encode_declaration(&declaration, &mut bytes).unwrap();
    bytes.truncate(n);
    let o = Object {
        declaration: Some(&bytes),
        ..public(b"model weights shard")
    };
    let (root, _) = publish(&svc, 0xc1, &o);
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "training"}),
    );
    assert_eq!(
        (status, r["error"].as_str(), r["status"].as_str()),
        (403, Some("PurposeDenied"), Some("ACCESS_DENIED"))
    );
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "inference"}),
    );
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_PUBLIC")),
        "{r}"
    );
    let rights = r["rights"].as_str().unwrap();
    assert!(rights.starts_with("DOCUMENTED") && rights.contains("not legally adjudicated"));
    assert!(
        !rights.to_ascii_lowercase().contains("cleared")
            && !rights.to_ascii_lowercase().contains("owner")
    );
    let (undeclared, _) = publish(&svc, 0xc2, &public(b"undeclared object"));
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": undeclared, "purpose": "training"}),
    );
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("PurposeDenied")),
        "missing declaration infers no training consent"
    );

    let (root, _) = publish(&svc, 0xc3, &private(b"private", P1, 1, key1()));
    let g = grant(&svc, 0xc4, &root, 3600, 1);
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "task": hex::encode(TASK), "purpose": "evaluation", "grant_id": g}),
    );
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("PurposeDenied")),
        "grant purpose mask bounds release"
    );
}

#[test]
fn a13_unsafe_locators_refuse_before_any_request_byte() {
    let dir = data_dir("a13");
    let svc = start(&dir);
    for uri in [
        "https://169.254.169.254/latest/meta-data",
        "https://[::1]/object",
        "https://localhost/object",
        "file:///etc/passwd",
        "http://93.184.216.34/object",
        "https://93.184.216.34:8443/object",
        "https://user:secret@93.184.216.34/object",
        "https://93.184.216.34/object#frag",
        "https://unlisted.example/object",
    ] {
        let (status, r) = post(&svc, "/v1/locators/check", TOKEN_A, &json!({"uri": uri}));
        assert_eq!(
            (status, r["error"].as_str()),
            (422, Some("UnsafeLocator")),
            "{uri}"
        );
    }
    let (status, r) = post(
        &svc,
        "/v1/locators/check",
        TOKEN_A,
        &json!({"uri": "https://93.184.216.34/object"}),
    );
    assert_eq!(status, 200, "{r}");
    assert_eq!(r["addresses"], json!(["93.184.216.34:443"]));

    let prepared = prepare(&svc, &rid(0xe1, 1), Privacy::Public, 16);
    for uri in [
        "https://localhost/object",
        "https://[::1]/object",
        "file:///etc/passwd",
        "https://169.254.169.254/object",
    ] {
        let (status, r) = post(
            &svc,
            "/v1/import",
            TOKEN_A,
            &json!({"session": prepared["session"], "uri": uri}),
        );
        assert_eq!(
            (status, r["error"].as_str()),
            (422, Some("UnsafeLocator")),
            "{uri}"
        );
    }

    let allow = vec!["objects.example".to_string()];
    let calls = Cell::new(0);
    let rebinding = |_: &str| {
        calls.set(calls.get() + 1);
        Ok(vec![
            "93.184.216.34".parse::<IpAddr>().unwrap(),
            "169.254.169.254".parse().unwrap(),
        ])
    };
    assert_eq!(
        resolver::admit("https://objects.example/o", &allow, rebinding),
        Err(ArtifactError::UnsafeLocator)
    );
    assert_eq!(calls.get(), 1, "resolved exactly once");
    let admitted = resolver::admit("https://objects.example/o", &allow, |_| {
        Ok(vec!["93.184.216.34".parse().unwrap()])
    })
    .unwrap();

    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let landed = listener.local_addr().unwrap();
    let server = std::thread::spawn(move || {
        let (mut conn, _) = listener.accept().unwrap();
        let mut seen = Vec::new();
        let _ = conn.read_to_end(&mut seen);
        seen
    });
    let refused = admitted.connect(|_| TcpStream::connect(landed));
    assert_eq!(refused.err(), Some(ArtifactError::UnsafeLocator));
    assert!(
        server.join().unwrap().is_empty(),
        "no request bytes or credentials sent"
    );
    if let Ok(v6) = TcpListener::bind("[::1]:0") {
        let landed = v6.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut conn, _) = v6.accept().unwrap();
            let mut seen = Vec::new();
            let _ = conn.read_to_end(&mut seen);
            seen
        });
        assert_eq!(
            admitted.connect(|_| TcpStream::connect(landed)).err(),
            Some(ArtifactError::UnsafeLocator)
        );
        assert!(server.join().unwrap().is_empty());
    }
    assert_eq!(
        resolver::check_response_head(
            b"HTTP/1.1 302 Found\r\nLocation: file:///etc/passwd\r\n\r\n"
        ),
        Err(ArtifactError::UnsafeLocator)
    );
    assert_eq!(
        resolver::check_response_head(b"HTTP/1.1 200 OK\r\n\r\n"),
        Ok(())
    );
}

fn encode_shared(shared: &state::SharedState<'_>) -> Vec<u8> {
    let mut out = vec![0; shared.encoded_len().unwrap()];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = state::encode_shared_state(shared, &mut out, &mut scratch).unwrap();
    out.truncate(n);
    out
}
/// Real F01 CREATE through `registry_ops::apply`, then a task region holding
/// one canonical CANCELLED record for TASK.
fn cancelled_task_state() -> Vec<u8> {
    let (chain, program) = (ctx().chain, ctx().program);
    let digest = |b: u8| Digest32::new([b; 32]).unwrap();
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
    let rewards = derive_rewards_account(program, AssetId::new([13; 32]).unwrap()).unwrap();
    let mut payload = [12; 32].to_vec();
    payload.extend_from_slice(&[13; 32]);
    payload.extend_from_slice(rewards.as_bytes());
    payload.extend_from_slice(&[14; 32]);
    payload.push(0);
    payload.extend_from_slice(&encoded_policy);
    payload.extend_from_slice(&[16; 32]);
    let owner = PrincipalId::new([12; 32]).unwrap();
    let envelope = codec::Envelope {
        operation: dispatch::CREATE,
        chain,
        program,
        market: ctx().market,
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
    let n = codec::encode_envelope(&envelope, &mut encoded).unwrap();
    let validated = codec::decode_envelope(&encoded[..n]).unwrap();
    let call = CallContext {
        chain,
        program,
        principal: owner,
        height: 1000,
    };
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Outcome::Applied { state: created, .. } =
        registry_ops::apply(&call, None, &validated, &mut section, &mut event).unwrap()
    else {
        panic!("fresh create retried");
    };
    let binding = TaskBinding {
        task: TaskId::new(TASK).unwrap(),
        requester: PrincipalId::new([0x61; 32]).unwrap(),
        worker: WorkerId::new([0x62; 32]).unwrap(),
        input: digest(0x67),
        deadline: 5_000,
        status: TaskStatus::Cancelled,
        acknowledgement: None,
        result: None,
        admission: digest(0x65),
    };
    let mut region = 1u16.to_be_bytes().to_vec();
    region.push(0);
    region.extend_from_slice(&binding.encode().unwrap());
    let mut policy_section =
        PolicySection::decode(created.section(Section::PolicyLifecycle).unwrap()).unwrap();
    policy_section.task_region = &region;
    let mut bytes = vec![0; Section::PolicyLifecycle.payload_cap()];
    let n = policy_section.encode(&mut bytes).unwrap();
    encode_shared(
        &created
            .replace_section(Section::PolicyLifecycle, &bytes[..n])
            .unwrap(),
    )
}
/// A rank-4 observation of TASK cancelled, through the observer route; the
/// ledger's terminal floor it returns is day 0 plus 30 days.
fn observe_terminal(svc: &Svc, request: &str, root: &str) -> u64 {
    let before = now_secs();
    let (status, r) = post(
        svc,
        "/v1/retention/terminal",
        TOKEN_O,
        &json!({"request_id": request, "root": root, "state": hex::encode(cancelled_task_state()),
                "native_state_root": hex::encode([0x5a; 32]), "observed_sequence": 77,
                "execution_height": 1010, "batch_id": hex::encode([0xbb; 32]),
                "checkpoint": hex::encode([0xcc; 32]), "rank": 4,
                "publication_time_ms": 1_700_000_000_000u64}),
    );
    assert_eq!(status, 200, "{r}");
    let floor = r["retain_until"].as_u64().unwrap();
    assert!((before + 30 * 86_400..=now_secs() + 30 * 86_400).contains(&floor));
    floor
}

#[test]
fn a17_signed_false_answer_passes_integrity_but_quality_stays_unestablished() {
    let dir = data_dir("a17");
    let svc = start(&dir);
    let o = public(b"answer: the capital of France is Berlin");
    let (root, _) = publish(&svc, 0xd1, &o);
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "evaluation"}),
    );
    assert_eq!(status, 200, "{r}");
    assert_eq!(r["integrity"], "ROOT_AND_SIGNATURE_VERIFIED");
    assert_eq!(r["attribution"]["signature_status"], "VALID");
    assert_eq!(r["quality"], "UNESTABLISHED");
    assert_eq!(r["reward_admission"], false);
    let envelope_bytes = hex::decode(r["envelope"].as_str().unwrap()).unwrap();
    let envelope = decode_envelope(&envelope_bytes).unwrap();
    let verified = verify_publisher(&envelope).unwrap();
    assert_eq!(hex::encode(verified.root.bytes()), root);
    let manifest = decode_manifest(envelope.manifest).unwrap();
    let fetched = fetch(
        &svc,
        TOKEN_B,
        &json!({"root": root, "purpose": "evaluation"}),
        manifest.chunk_count,
    );
    let mut scratch = vec![[0u8; 32]; manifest.chunk_count as usize];
    let mut assembler = ContentAssembler::new(&manifest, &mut scratch).unwrap();
    for (i, chunk) in fetched.chunks(CHUNK_BYTES as usize).enumerate() {
        assembler.deliver(i as u32, chunk).unwrap();
    }
    assembler.finish().unwrap();
    assert_eq!(fetched, o.plain);
    let floor = observe_terminal(&svc, &rid(0xd3, 1), &root);
    let (status, t) = post(
        &svc,
        "/v1/tombstone",
        TOKEN_A,
        &json!({"request_id": rid(0xd2, 1), "root": root, "reason": 2}),
    );
    assert_eq!(
        (status, t["error"].as_str(), t["until"].as_u64()),
        (409, Some("RETENTION_ACTIVE"), Some(floor)),
        "{t}"
    );
    let (status, t) = post(
        &svc,
        "/v1/tombstone",
        TOKEN_A,
        &json!({"request_id": rid(0xd2, 2), "root": root, "reason": 3}),
    );
    assert_eq!(status, 200, "{t}");
    assert_eq!(t["purge_after"].as_u64(), Some(floor));
    let (status, r) = post(
        &svc,
        "/v1/resolve",
        TOKEN_B,
        &json!({"root": root, "purpose": "evaluation"}),
    );
    assert_eq!((status, r["status"].as_str()), (410, Some("TOMBSTONED")));
}

fn now_secs() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
}
fn key_of(r: &Value) -> [u8; 32] {
    hex::decode(r["object_key"].as_str().unwrap())
        .unwrap()
        .try_into()
        .unwrap()
}
fn usage(svc: &Svc, token: &str) -> Value {
    let (status, body) = http(svc, "GET", "/v1/usage", token, b"");
    assert_eq!(status, 200);
    serde_json::from_slice(&body).unwrap()
}
fn copy_dir(from: &Path, to: &Path) {
    std::fs::create_dir_all(to).unwrap();
    for entry in std::fs::read_dir(from).unwrap() {
        let path = entry.unwrap().path();
        let target = to.join(path.file_name().unwrap());
        if path.is_dir() {
            copy_dir(&path, &target);
        } else {
            std::fs::copy(&path, &target).unwrap();
        }
    }
}

#[test]
fn r15_purge_at_retention_deadline_removes_payload_and_keeps_record() {
    let dir = data_dir("r15");
    let svc = start(&dir);
    let plain = vec![0x3c; CHUNK_BYTES as usize + 77];
    let (root, staged) = publish(&svc, 0xf1, &private(&plain, P1, 1, key1()));
    let g = grant(&svc, 0xf2, &root, 3600, 1);
    assert_eq!(
        usage(&svc, TOKEN_A)["quota_used"],
        staged.bytes.len() as u64
    );
    let floor = observe_terminal(&svc, &rid(0xf4, 1), &root);
    let (status, t) = post(
        &svc,
        "/v1/tombstone",
        TOKEN_A,
        &json!({"request_id": rid(0xf3, 1), "root": root, "reason": 3}),
    );
    assert_eq!(status, 200, "{t}");
    let deadline = t["purge_after"].as_u64().unwrap();
    assert_eq!(deadline, floor, "the ledger's terminal-finality deadline");
    drop(svc);

    // The ledger purge pass runs on an explicit clock value.
    let chunks = dir.join("store/staging").join(&staged.session);
    let (mut store, mut ledger) = (open_store(&dir), open_ledger(&dir));
    assert_eq!(
        ledger.purge(&mut store, deadline - 1),
        Ok(PurgeReport::default())
    );
    assert_eq!(
        std::fs::read_dir(&chunks).unwrap().count(),
        2,
        "payload bytes retained until the deadline"
    );
    assert_eq!(
        ledger.purge(&mut store, deadline),
        Ok(PurgeReport {
            purged: vec![root.clone()],
            pending: vec![],
        })
    );
    assert!(!chunks.exists(), "payload bytes physically removed");
    drop((store, ledger));

    let store = open_store(&dir);
    let record = &store.state.objects[&root];
    assert_eq!(record.state, ObjectState::Purged);
    assert_eq!(record.tombstone_reason, Some(3), "reason retained");
    assert_eq!(record.retention_until, Some(deadline));
    assert!(record.wrapped_key.is_none(), "key envelope purged");
    assert!(!record.manifest.is_empty() && !record.envelope.is_empty());
    let history: Vec<ObjectState> = record.history.iter().map(|t| t.state).collect();
    assert_eq!(
        history,
        [
            ObjectState::Publishing,
            ObjectState::Available,
            ObjectState::Tombstoned,
            ObjectState::Purged
        ],
        "history retained"
    );
    assert_eq!(record.history[3].at, deadline);
    drop(store);

    let svc = start(&dir);
    let access = json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
    assert_eq!(
        (status, r["status"].as_str(), r["object_state"].as_str()),
        (410, Some("TOMBSTONED"), Some("PURGED")),
        "{r}"
    );
    assert!(r.get("object_key").is_none());
    assert_eq!(r["attribution"]["publisher"], hex::encode(P1));
    let mut req = access.clone();
    req["index"] = json!(0);
    let (status, _) = http(
        &svc,
        "POST",
        "/v1/content",
        TOKEN_B,
        req.to_string().as_bytes(),
    );
    assert_eq!(status, 410);
    assert_eq!(
        usage(&svc, TOKEN_A)["quota_used"],
        0,
        "purge releases quota"
    );
}

#[test]
fn r11_stalled_or_interrupted_key_delivery_is_unknown_until_retry_reconciles() {
    let dir = data_dir("r11");
    let svc = start(&dir);
    let plain = b"private result released over tls";
    let (root, staged) = publish(&svc, 0x51, &private(plain, P1, 1, key1()));
    let g = grant(&svc, 0x52, &root, 3600, 3);
    let access = |request: &str| json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": request});
    let mut anonymous = access(&rid(0x53, 1));
    anonymous.as_object_mut().unwrap().remove("request_id");
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &anonymous);
    assert_eq!((status, r["error"].as_str()), (400, Some("Malformed")));
    assert!(
        r.get("object_key").is_none(),
        "no key release without a delivery identity"
    );
    drop(svc);

    let (open_sink, mut reader) = UnixStream::pair().unwrap();
    let sink = store::deliver(open_sink, b"acknowledged".to_vec(), Duration::from_secs(5));
    assert!(sink.is_some());
    drop(sink);
    let mut received = Vec::new();
    reader.read_to_end(&mut received).unwrap();
    assert_eq!(received, b"acknowledged");

    let digest = store::delivery_digest(&root, &g, &hex::encode(TASK), "inference");
    let at = now_secs();
    let intent = || Delivery {
        digest: digest.clone(),
        root: root.clone(),
        grant: g.clone(),
        generation: 1,
        attempts: 0,
        state: DeliveryState::Pending,
        updated_at: at,
    };
    let (stalled_id, interrupted_id) = (rid(0x54, 1), rid(0x55, 1));
    let (stalled_key, interrupted_key) = (
        format!("tenant-b:{stalled_id}"),
        format!("tenant-b:{interrupted_id}"),
    );
    let mut store = open_store(&dir);
    // A sink that stalls past its deadline: a real socket whose peer never reads.
    assert_eq!(store.begin_delivery(&stalled_key, intent()), Ok(None));
    let (stalled, peer) = UnixStream::pair().unwrap();
    let delivered = store::deliver(stalled, vec![0; 16 << 20], Duration::from_millis(200));
    assert!(delivered.is_none(), "deadline exceeded");
    store
        .settle_delivery(&stalled_key, delivered.is_some(), at)
        .unwrap();
    drop(peer);
    assert_eq!(
        store.begin_delivery(
            &stalled_key,
            Delivery {
                digest: "different release".to_string(),
                ..intent()
            }
        ),
        Err(ArtifactError::IdempotencyConflict)
    );
    // The process stops after the durable intent and before acknowledgement.
    assert_eq!(store.begin_delivery(&interrupted_key, intent()), Ok(None));
    assert_eq!(
        store.begin_delivery(&interrupted_key, intent()),
        Err(ArtifactError::UnknownDelivery),
        "an attempt in flight is unknown"
    );
    drop(store);
    let durable = open_store(&dir);
    assert_eq!(
        durable.state.deliveries[&interrupted_key].state,
        DeliveryState::Pending
    );
    assert_eq!(
        durable.state.deliveries[&stalled_key].state,
        DeliveryState::Unknown
    );
    drop(durable);

    let svc = start(&dir);
    for id in [&stalled_id, &interrupted_id] {
        let (status, d) = post(&svc, "/v1/deliveries", TOKEN_B, &json!({"request_id": id}));
        assert_eq!(
            (status, d["error"].as_str(), d["status"].as_str()),
            (504, Some("UnknownDelivery"), Some("UNKNOWN_DELIVERY")),
            "{d}"
        );
    }
    let (status, _) = post(
        &svc,
        "/v1/deliveries",
        TOKEN_A,
        &json!({"request_id": interrupted_id}),
    );
    assert_eq!(status, 404, "delivery state is tenant-isolated");

    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&interrupted_id));
    assert_eq!(status, 200, "{r}");
    assert_eq!(r["delivery"]["reconciles"], "UNKNOWN_DELIVERY");
    assert_eq!(r["delivery"]["attempt"], 2);
    let key = key_of(&r);
    assert_eq!(Some(key), staged.object_key);
    let stream = fetch(&svc, TOKEN_B, &access(&interrupted_id), 1);
    assert_eq!(open(&key, &stream).unwrap(), plain);
    let (status, d) = post(
        &svc,
        "/v1/deliveries",
        TOKEN_B,
        &json!({"request_id": interrupted_id}),
    );
    assert_eq!((status, d["status"].as_str()), (200, Some("DELIVERED")));

    let mut other = access(&interrupted_id);
    other["purpose"] = json!("evaluation");
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &other);
    assert_eq!(
        (status, r["error"].as_str()),
        (409, Some("IdempotencyConflict"))
    );
    assert!(r.get("object_key").is_none());

    let (status, _) = post(
        &svc,
        "/v1/revoke",
        TOKEN_A,
        &json!({"request_id": rid(0x56, 1), "grant_id": g, "generation": 1, "sequence": 1}),
    );
    assert_eq!(status, 200);
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access(&stalled_id));
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("AuthorityRevoked"))
    );
    assert!(r.get("object_key").is_none());
    let (status, d) = post(
        &svc,
        "/v1/deliveries",
        TOKEN_B,
        &json!({"request_id": stalled_id}),
    );
    assert_eq!(
        (status, d["status"].as_str()),
        (504, Some("UNKNOWN_DELIVERY")),
        "an unknown key retrieval stays recorded for reconciliation"
    );
    drop(svc);
    scan(
        &dir.join("store"),
        &[key.to_vec(), hex::encode(key).into_bytes()],
    );
}

#[test]
fn r20_restore_below_live_revocation_mark_stays_non_ready() {
    let dir = data_dir("r20");
    let svc = start(&dir);
    let (root, _) = publish(
        &svc,
        0x21,
        &private(b"grant generation seven backup", P7, 7, key7()),
    );
    let g = grant(&svc, 0x22, &root, 3600, 1);
    let access = json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});
    assert_eq!(post(&svc, "/v1/resolve", TOKEN_B, &access).0, 200);
    drop(svc);
    copy_dir(&dir.join("store"), &dir.join("backup"));

    let svc = start(&dir);
    let (status, _) = post(
        &svc,
        "/v1/revoke",
        TOKEN_A,
        &json!({"request_id": rid(0x23, 1), "grant_id": g, "generation": 1, "sequence": 1}),
    );
    assert_eq!(status, 200);
    assert_eq!(
        post(&svc, "/v1/resolve", TOKEN_B, &access).1["error"],
        "AuthorityRevoked"
    );
    drop(svc);
    let mark = || std::fs::read_to_string(mark_file(&dir)).unwrap();
    assert_eq!(mark(), "1\n");

    // Stale restore: newer object files, unrevoked grant, mark 0 below live 1.
    std::fs::rename(dir.join("store"), dir.join("current")).unwrap();
    copy_dir(&dir.join("backup"), &dir.join("store"));
    let refused = try_start(&dir, true)
        .err()
        .expect("a stale restore must not become ready");
    assert_eq!(refused.code(), Some(3));
    let log = std::fs::read_to_string(dir.join("service.log")).unwrap();
    assert!(log.contains("restore not ready: revocation mark 0 below live mark 1"));
    assert_eq!(mark(), "1\n", "the live mark never rolls back");

    // Equal mark: admitted, and the revoked grant is not served.
    std::fs::remove_dir_all(dir.join("store")).unwrap();
    std::fs::rename(dir.join("current"), dir.join("store")).unwrap();
    let svc = start(&dir);
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
    assert_eq!(
        (status, r["error"].as_str()),
        (403, Some("AuthorityRevoked"))
    );
    drop(svc);

    // Above: a restore ahead of the live mark is admitted and raises it.
    std::fs::write(mark_file(&dir), "0\n").unwrap();
    let svc = start(&dir);
    assert_eq!(mark(), "1\n");
    assert_eq!(
        post(&svc, "/v1/resolve", TOKEN_B, &access).1["error"],
        "AuthorityRevoked"
    );
}

#[test]
fn tls_listener_releases_object_key_only_to_verified_tls_clients() {
    let dir = data_dir("tls");
    let svc = start(&dir);
    assert!(svc.tls);
    let mut plain = TcpStream::connect(&svc.addr).unwrap();
    let request = format!(
        "GET /v1/usage HTTP/1.1\r\nAuthorization: Bearer {TOKEN_A}\r\nContent-Length: 0\r\n\r\n"
    );
    let _ = plain.write_all(request.as_bytes());
    let _ = plain.shutdown(Shutdown::Write);
    let mut seen = Vec::new();
    let _ = plain.read_to_end(&mut seen);
    assert!(
        !seen.starts_with(b"HTTP/"),
        "no plaintext HTTP on a TLS listener"
    );

    let foreign = rcgen::generate_simple_self_signed(vec!["localhost".to_string()]).unwrap();
    let conn = ClientConnection::new(
        client_trusting(foreign.cert.der().clone()),
        ServerName::try_from("localhost").unwrap(),
    )
    .unwrap();
    let mut untrusting = StreamOwned::new(conn, TcpStream::connect(&svc.addr).unwrap());
    let handshake = untrusting
        .write_all(request.as_bytes())
        .and_then(|()| untrusting.flush())
        .and_then(|()| untrusting.read(&mut [0; 1]));
    assert!(handshake.is_err(), "certificate verification is enforced");

    let plaintext = b"released only inside verified tls";
    let (root, staged) = publish(&svc, 0x61, &private(plaintext, P1, 1, key1()));
    let g = grant(&svc, 0x62, &root, 3600, 1);
    let access = json!({"root": root, "task": hex::encode(TASK), "purpose": "inference", "grant_id": g, "request_id": g});
    let (status, r) = post(&svc, "/v1/resolve", TOKEN_B, &access);
    assert_eq!(
        (status, r["status"].as_str()),
        (200, Some("AVAILABLE_AUTHORIZED"))
    );
    let key = key_of(&r);
    assert_eq!(Some(key), staged.object_key);
    assert_eq!(
        open(&key, &fetch(&svc, TOKEN_B, &access, 1)).unwrap(),
        plaintext
    );
}
