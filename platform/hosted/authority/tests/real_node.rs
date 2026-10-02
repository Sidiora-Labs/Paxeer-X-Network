//! Exercises the receipt authority against a real `layerxd` sequencer and a
//! real `layerxd --authority-replica` started from `build/bin`.

#[path = "../../../../tests/support/lxgb_metadata.rs"]
mod lxgb_metadata;

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::refusal::decode_core_refusal;
use layerx_client::lni::schema::{decode_envelope, encode_envelope, Envelope, Version};
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_platform_authority::{
    authorized_batch_by_activity, hex, parse_replica_evidence, receipt_locator, BatchEvidence,
    BatchIdentityEvidence, EvidenceRefusal,
};
use layerx_proof::inclusion::{verify_receipt, InclusionError, SequencerAuthorization};
use layerx_proof::merkle::decode_proof;
use layerx_proof::receipt::{
    verify_outcome_maintained, AuthorizedBatch, MaintainedOutcomeEvidence,
};
use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload};
use layerx_wire::activity::{decode_signed, encode_signed_envelope, encode_unsigned_envelope};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{activity_id, execution_batch_id, program_execution_batch_id, Domain};
use layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION as PROTOCOL_VERSION;
use native_tls::{Certificate, TlsConnector};
use sha2::{Digest as _, Sha256};
use std::collections::BTreeMap;
use std::fmt::Debug;
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const NETWORK_ID: u32 = 7331;
const NETWORK_NAME: &str = "layerx-authority-test";
const FIRST_BATCH: u64 = 1;
const LAST_BATCH: u64 = 1_000_000;
const LNI_FRAME_BYTES: usize = 1_212_416;
const LOG_BYTES: u64 = 64 * 1024 * 1024;
const SEND_ACTIVITY: u16 = 5;
const MODULE_GOVERNANCE: u16 = 7;
const METERING_AUTHORITY_GENESIS: u8 = 1;

fn must<T, E: Debug>(result: Result<T, E>, what: &str) -> T {
    result.unwrap_or_else(|error| panic!("{what}: {error:?}"))
}

fn random32() -> [u8; 32] {
    let mut bytes = [0_u8; 32];
    must(getrandom::fill(&mut bytes), "random bytes");
    if bytes == [0; 32] {
        bytes[0] = 1;
    }
    bytes
}

fn now_ms() -> u64 {
    let elapsed = must(SystemTime::now().duration_since(UNIX_EPOCH), "clock");
    u64::try_from(elapsed.as_millis()).unwrap_or(u64::MAX)
}

fn repository_root() -> PathBuf {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    manifest.ancestors().nth(3).map_or_else(
        || panic!("repository root above {}", manifest.display()),
        Path::to_path_buf,
    )
}

fn free_port() -> u16 {
    let listener = must(TcpListener::bind("127.0.0.1:0"), "port allocation");
    must(listener.local_addr(), "port address").port()
}

fn serve_recorded_chain() -> (u64, u16) {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../tests/daemon/fixtures/finality-authority/rpc.json");
    let fixture: serde_json::Value = must(
        serde_json::from_slice(&must(fs::read(&path), "recorded chain fixture")),
        "recorded chain fixture JSON",
    );
    let chain_id = fixture["chain_id"]
        .as_u64()
        .unwrap_or_else(|| panic!("recorded chain fixture chain_id"));
    let exchanges = fixture["exchanges"]
        .as_array()
        .unwrap_or_else(|| panic!("recorded chain fixture exchanges"))
        .clone();
    let listener = must(TcpListener::bind("127.0.0.1:0"), "recorded chain listener");
    let port = must(listener.local_addr(), "recorded chain address").port();
    thread::spawn(move || {
        for stream in listener.incoming() {
            let Ok(mut stream) = stream else { continue };
            let mut request = Vec::new();
            let mut buffer = [0_u8; 4096];
            let body = loop {
                let Ok(count) = stream.read(&mut buffer) else {
                    break None;
                };
                if count == 0 {
                    break None;
                }
                request.extend_from_slice(&buffer[..count]);
                let Some(end) = request.windows(4).position(|w| w == b"\r\n\r\n") else {
                    continue;
                };
                let head = String::from_utf8_lossy(&request[..end]).to_ascii_lowercase();
                let length = head
                    .lines()
                    .find_map(|line| line.strip_prefix("content-length:"))
                    .and_then(|value| value.trim().parse::<usize>().ok())
                    .unwrap_or(0);
                if request.len() >= end + 4 + length {
                    break Some(request[end + 4..end + 4 + length].to_vec());
                }
            };
            let Some(call) =
                body.and_then(|b| serde_json::from_slice::<serde_json::Value>(&b).ok())
            else {
                continue;
            };
            let recorded = exchanges
                .iter()
                .find(|item| item["method"] == call["method"] && item["params"] == call["params"]);
            let reply = match recorded {
                Some(item) => {
                    serde_json::json!({"jsonrpc": "2.0", "id": call["id"], "result": item["result"]})
                }
                None => serde_json::json!({"jsonrpc": "2.0", "id": call["id"],
                    "error": {"code": -32000, "message": "not recorded"}}),
            };
            let reply = reply.to_string();
            let _ = write!(
                stream,
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                reply.len(),
                reply
            );
        }
    });
    (chain_id, port)
}

fn write(path: &Path, bytes: &[u8], mode: u32) {
    must(fs::write(path, bytes), &format!("write {}", path.display()));
    must(
        fs::set_permissions(path, fs::Permissions::from_mode(mode)),
        &format!("chmod {}", path.display()),
    );
}

fn preallocate_log(path: &Path) {
    let mode =
        std::env::var("LAYERX_TEST_BOOTSTRAP_LOG_MODE").unwrap_or_else(|_| "absent".to_owned());
    assert!(
        !path.exists(),
        "fresh log already exists: {}",
        path.display()
    );
    if mode == "absent" {
        return;
    }
    assert!(
        mode == "empty" || mode == "preallocated",
        "unsupported bootstrap log mode: {mode}"
    );
    let file = must(
        fs::File::create(path),
        &format!("create {}", path.display()),
    );
    let size = if mode == "empty" { 0 } else { LOG_BYTES };
    must(file.set_len(size), &format!("size {}", path.display()));
    assert_eq!(must(file.metadata(), "initial log metadata").len(), size);
    must(
        fs::set_permissions(path, fs::Permissions::from_mode(0o600)),
        &format!("chmod {}", path.display()),
    );
}

fn make_dir(path: &Path, mode: u32) {
    must(
        fs::create_dir_all(path),
        &format!("mkdir {}", path.display()),
    );
    must(
        fs::set_permissions(path, fs::Permissions::from_mode(mode)),
        &format!("chmod {}", path.display()),
    );
}

fn chown_tree(path: &Path, uid: u32, gid: u32) {
    must(
        std::os::unix::fs::chown(path, Some(uid), Some(gid)),
        &format!("chown {}", path.display()),
    );
    if path.is_dir() {
        for entry in must(fs::read_dir(path), &format!("list {}", path.display())) {
            chown_tree(&must(entry, "directory entry").path(), uid, gid);
        }
    }
}

fn command(program: &str, arguments: &[&str]) {
    let output = must(
        Command::new(program).args(arguments).output(),
        &format!("run {program}"),
    );
    assert!(
        output.status.success(),
        "{program} {arguments:?} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
}

fn token() -> String {
    hex::encode(&random32())
}

struct Daemon {
    child: Child,
    stderr: PathBuf,
}

impl Daemon {
    fn stop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }

    fn diagnostics(&self) -> String {
        fs::read_to_string(&self.stderr).unwrap_or_default()
    }
}

impl Drop for Daemon {
    fn drop(&mut self) {
        self.stop();
    }
}

struct Genesis {
    directory: PathBuf,
    asset: [u8; 32],
    receipt_state_root: [u8; 32],
}

fn genesis_request(asset: &[u8; 32], guarantor_key: &[u8; 33]) -> Vec<u8> {
    let mut request = Vec::with_capacity(512);
    request.extend_from_slice(b"LXGB");
    request.push(2);
    request.extend_from_slice(&PROTOCOL_VERSION.to_be_bytes());
    request.extend_from_slice(&NETWORK_ID.to_be_bytes());
    request.extend_from_slice(&now_ms().to_be_bytes());
    request.extend_from_slice(&1_u16.to_be_bytes());
    request.extend_from_slice(&MODULE_GOVERNANCE.to_be_bytes());
    let mut parameter_key = [0_u8; 32];
    parameter_key[..17].copy_from_slice(b"parameter-version");
    request.extend_from_slice(&parameter_key);
    let mut parameter_value = [0_u8; 32];
    parameter_value[31] = 1;
    request.extend_from_slice(&parameter_value);
    request.extend_from_slice(&1_u16.to_be_bytes());
    let mut guarantor_id = [0_u8; 32];
    guarantor_id[0] = 1;
    request.extend_from_slice(&guarantor_id);
    request.extend_from_slice(guarantor_key);
    request.extend_from_slice(&[0_u8; 16]);
    request.extend_from_slice(asset);
    request.extend_from_slice(&1_u32.to_be_bytes());
    for value in [1_u64, 1, 1, 1, 1, 8, 8, 64, 8] {
        request.extend_from_slice(&value.to_be_bytes());
    }
    request.extend_from_slice(&1_u64.to_be_bytes());
    request.push(METERING_AUTHORITY_GENESIS);
    request.extend_from_slice(&1_u32.to_be_bytes());
    for value in [1_u64, 1, 2, 4, 1, 1, 100] {
        request.extend_from_slice(&value.to_be_bytes());
    }
    for value in [100_u64, 1, 1, 10, 1, 1000] {
        request.extend_from_slice(&value.to_be_bytes());
    }
    let issuer = SigningKey::from_bytes(&random32());
    lxgb_metadata::append(
        &mut request,
        asset,
        &issuer.verifying_key().to_bytes(),
        &random32(),
    );
    request
}

fn genesis_guarantor_key(directory: &Path) -> [u8; 33] {
    let private = directory.join("guarantor-key.pem");
    let public = directory.join("guarantor-public.der");
    write(&private, &[], 0o600);
    command(
        "openssl",
        &[
            "genpkey",
            "-algorithm",
            "EC",
            "-pkeyopt",
            "ec_paramgen_curve:secp256k1",
            "-out",
            &private.to_string_lossy(),
        ],
    );
    command(
        "openssl",
        &[
            "ec",
            "-in",
            &private.to_string_lossy(),
            "-pubout",
            "-conv_form",
            "compressed",
            "-outform",
            "DER",
            "-out",
            &public.to_string_lossy(),
        ],
    );
    let encoded = must(fs::read(&public), "guarantor public key");
    let compressed = encoded
        .get(encoded.len().saturating_sub(33)..)
        .unwrap_or_else(|| panic!("compressed guarantor public key missing"));
    must(
        compressed.try_into(),
        "compressed guarantor public key length",
    )
}

fn build_genesis(root: &Path, builder: &Path) -> Genesis {
    let directory = root.join("genesis");
    make_dir(&directory, 0o755);
    let asset = random32();
    let guarantor_key = genesis_guarantor_key(&directory);
    write(
        &directory.join("request.lxgb"),
        &genesis_request(&asset, &guarantor_key),
        0o600,
    );
    write(&directory.join("signer.key"), &random32(), 0o600);
    let artifacts = directory.join("artifacts");
    command(
        &builder.to_string_lossy(),
        &[
            &directory.join("request.lxgb").to_string_lossy(),
            &directory.join("signer.key").to_string_lossy(),
            &artifacts.to_string_lossy(),
        ],
    );
    let request = must(
        fs::read(artifacts.join("paxeer-registration-request.lxrr")),
        "registration request",
    );
    assert_eq!(request.len(), 73, "LXRR artifact length");
    assert_eq!(&request[..4], b"LXRR");
    let mut receipt_state_root = [0_u8; 32];
    receipt_state_root.copy_from_slice(&request[41..73]);
    Genesis {
        directory: artifacts,
        asset,
        receipt_state_root,
    }
}

fn registration(receipt_state_root: &[u8; 32]) -> Vec<u8> {
    let mut encoded = Vec::with_capacity(82);
    encoded.extend_from_slice(b"LXGR");
    encoded.push(1);
    encoded.extend_from_slice(&NETWORK_ID.to_be_bytes());
    encoded.extend_from_slice(&0_u64.to_be_bytes());
    encoded.extend_from_slice(receipt_state_root);
    encoded.extend_from_slice(receipt_state_root);
    encoded.push(1);
    encoded
}

fn node_config(role: &str) -> String {
    format!(
        "role={role}\nnetwork_id={NETWORK_ID}\nstart_sequence=0\nverify_workers=0\nnetwork_workers=0\nprojection_workers=0\ncheckpoint_workers=0\nserial_execution=true\n"
    )
}

struct Identity {
    daemon_uid: u32,
    daemon_gid: u32,
    client_uid: u32,
    client_gid: u32,
}

fn identity() -> Identity {
    let client_uid = rustix_free_uid();
    assert_eq!(
        client_uid, 0,
        "the real-node harness must run as root so layerxd can run under a distinct uid"
    );
    Identity {
        daemon_uid: 65534,
        daemon_gid: 0,
        client_uid,
        client_gid: 0,
    }
}

fn rustix_free_uid() -> u32 {
    let status = must(fs::read_to_string("/proc/self/status"), "process status");
    status
        .lines()
        .find_map(|line| line.strip_prefix("Uid:"))
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|value| value.parse::<u32>().ok())
        .unwrap_or_else(|| panic!("effective uid is not readable"))
}

fn spawn_daemon(
    layerxd: &Path,
    mode: &str,
    config: &Path,
    environment: &BTreeMap<&str, String>,
    identity: &Identity,
    stderr: PathBuf,
) -> Daemon {
    let mut command = Command::new(layerxd);
    command
        .arg(mode)
        .arg(config)
        .env_clear()
        .env("PATH", "/usr/bin:/bin")
        .envs(environment.iter().map(|(key, value)| (key, value.as_str())))
        .uid(identity.daemon_uid)
        .gid(identity.daemon_gid)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::from(must(
            fs::File::create(&stderr),
            "daemon stderr file",
        )));
    let mut dump = String::new();
    for (key, value) in environment {
        dump.push_str(key);
        dump.push('=');
        dump.push_str(value);
        dump.push('\n');
    }
    write(&stderr.with_extension("env"), dump.as_bytes(), 0o600);
    let child = must(command.spawn(), &format!("spawn {mode}"));
    Daemon { child, stderr }
}

fn wait_for_port(port: u16, daemon: &mut Daemon, what: &str) {
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        if TcpStream::connect(("127.0.0.1", port)).is_ok() {
            return;
        }
        if let Ok(Some(status)) = daemon.child.try_wait() {
            panic!(
                "{what} exited early with {status}: {}",
                daemon.diagnostics()
            );
        }
        assert!(
            Instant::now() < deadline,
            "{what} did not open port {port}: {}",
            daemon.diagnostics()
        );
        thread::sleep(Duration::from_millis(50));
    }
}

fn lni_limits() -> Limits {
    Limits {
        maximum_frame_bytes: LNI_FRAME_BYTES,
        maximum_connections: 4,
        maximum_streams: 1,
        maximum_queued_bytes: LNI_FRAME_BYTES,
        deadline: Duration::from_secs(10),
    }
}

fn connect_lni(socket: &Path, gate: &ConnectionGate) -> Uds {
    let mut transport = must(Uds::connect(socket, gate, lni_limits()), "LNI connect");
    must(
        perform(
            &mut transport,
            &HandshakeConfig {
                built_interface_version: Version::V1_3,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: NETWORK_ID,
            },
            None,
        ),
        "LNI handshake",
    );
    transport
}

fn wait_for_lni(socket: &Path, gate: &ConnectionGate, daemon: &mut Daemon) {
    let deadline = Instant::now() + Duration::from_secs(60);
    let expected = HandshakeConfig {
        built_interface_version: Version::V1_3,
        expected_protocol_version: PROTOCOL_VERSION,
        expected_network_id: NETWORK_ID,
    };
    loop {
        if socket.exists() {
            if let Ok(mut transport) = Uds::connect(socket, gate, lni_limits()) {
                if perform(&mut transport, &expected, None).is_ok() {
                    return;
                }
            }
        }
        if let Ok(Some(status)) = daemon.child.try_wait() {
            panic!(
                "sequencer exited early with {status}: {}",
                daemon.diagnostics()
            );
        }
        assert!(
            Instant::now() < deadline,
            "sequencer LNI did not come up: {}",
            daemon.diagnostics()
        );
        thread::sleep(Duration::from_millis(100));
    }
}

fn exchange(
    transport: &mut dyn FrameTransport,
    tag: u16,
    correlation_id: u64,
    payload: &[u8],
) -> (u16, Vec<u8>, Vec<u8>) {
    let request = must(
        encode_envelope(Envelope {
            version: Version::V1_3,
            message_tag: tag,
            correlation_id,
            canonical_payload: payload,
            proof_material: &[],
        }),
        "LNI request encoding",
    );
    must(transport.send(&request), "LNI send");
    let response = must(transport.receive(), "LNI receive");
    let response = must(decode_envelope(&response), "LNI response decoding");
    assert_eq!(response.correlation_id, correlation_id);
    (
        response.message_tag,
        response.canonical_payload.to_vec(),
        response.proof_material.to_vec(),
    )
}

fn domain_hash(domain: Domain, bytes: &[u8]) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(domain.tag());
    digest.update(bytes);
    digest.finalize().into()
}

fn registry() -> ModuleRegistry {
    let activity = must(
        ActivityType::new(ModuleId::Asset, SEND_ACTIVITY),
        "activity type",
    );
    let registration = must(
        ModuleRegistration::new(ModuleId::Asset, &[activity]),
        "registration",
    );
    must(ModuleRegistry::new(&[registration]), "registry")
}

#[allow(clippy::too_many_arguments)]
fn send_payload(
    signing_key: &SigningKey,
    source: [u8; 32],
    destination: [u8; 32],
    asset: [u8; 32],
    amount: u128,
    sequence: u64,
    idempotency: [u8; 32],
    expires_at: u64,
    context: [u8; 32],
) -> Vec<u8> {
    let public_key = signing_key.verifying_key().to_bytes();
    let mut authorization = Encoder::new(512);
    must(
        authorization
            .u16(0x5301)
            .and_then(|()| authorization.fixed(&source))
            .and_then(|()| authorization.fixed(&destination))
            .and_then(|()| authorization.fixed(&asset))
            .and_then(|()| authorization.u128(amount))
            .and_then(|()| authorization.u64(sequence))
            .and_then(|()| authorization.fixed(&idempotency))
            .and_then(|()| authorization.u64(expires_at))
            .and_then(|()| authorization.fixed(&context))
            .and_then(|()| authorization.u8(0))
            .and_then(|()| authorization.u8(1))
            .and_then(|()| authorization.fixed(&source))
            .and_then(|()| authorization.fixed(&context))
            .and_then(|()| authorization.u32(NETWORK_ID))
            .and_then(|()| authorization.u16(PROTOCOL_VERSION)),
        "authorization encoding",
    );
    let signature = signing_key
        .sign(&domain_hash(
            Domain::SignaturePreimage,
            &authorization.finish(),
        ))
        .to_bytes();
    let mut payload = Encoder::new(512);
    must(
        payload
            .u16(0x5301)
            .and_then(|()| payload.u16(10))
            .and_then(|()| payload.fixed(&source))
            .and_then(|()| payload.fixed(&destination))
            .and_then(|()| payload.fixed(&asset))
            .and_then(|()| payload.u128(amount))
            .and_then(|()| payload.u64(sequence))
            .and_then(|()| payload.fixed(&idempotency))
            .and_then(|()| payload.u64(expires_at))
            .and_then(|()| payload.fixed(&context))
            .and_then(|()| payload.u8(0))
            .and_then(|()| payload.u8(1))
            .and_then(|()| payload.fixed(&source))
            .and_then(|()| payload.fixed(&public_key))
            .and_then(|()| payload.fixed(&signature))
            .and_then(|()| payload.fixed(&context))
            .and_then(|()| payload.u32(NETWORK_ID))
            .and_then(|()| payload.u16(PROTOCOL_VERSION)),
        "payload encoding",
    );
    payload.finish()
}

struct Actor {
    signing_key: SigningKey,
    did: String,
    source: [u8; 32],
}

fn actor() -> Actor {
    let signing_key = SigningKey::from_bytes(&random32());
    let did = format!(
        "did:layerx:{}",
        hex::encode(&signing_key.verifying_key().to_bytes())
    );
    let name = format!("agent:{did}:main");
    let length = must(u32::try_from(name.len()), "account name length");
    let mut digest = Sha256::new();
    digest.update(b"LX:ACCOUNT:v1");
    digest.update(length.to_be_bytes());
    digest.update(name.as_bytes());
    Actor {
        signing_key,
        did,
        source: digest.finalize().into(),
    }
}

fn signed_send(actor: &Actor, asset: [u8; 32], sequence: u64) -> Vec<u8> {
    signed_send_payload(actor, asset, sequence, false)
}

fn signed_send_payload(actor: &Actor, asset: [u8; 32], sequence: u64, truncated: bool) -> Vec<u8> {
    let destination = random32();
    let idempotency = random32();
    let amount = 1_u128;
    let not_before = now_ms().saturating_sub(30_000);
    let expires_at = now_ms() + 120_000;
    let mut context_material = Vec::with_capacity(144);
    context_material.extend_from_slice(&actor.source);
    context_material.extend_from_slice(&destination);
    context_material.extend_from_slice(&asset);
    context_material.extend_from_slice(&amount.to_be_bytes());
    context_material.extend_from_slice(&idempotency);
    let context = domain_hash(Domain::ContextHash, &context_material);
    let mut payload_bytes = send_payload(
        &actor.signing_key,
        actor.source,
        destination,
        asset,
        amount,
        sequence,
        idempotency,
        expires_at,
        context,
    );
    if truncated {
        assert!(payload_bytes.pop().is_some());
    }
    let activity_type = must(
        ActivityType::new(ModuleId::Asset, SEND_ACTIVITY),
        "activity type",
    );
    let payload = must(
        Payload::new(&registry(), activity_type, &payload_bytes),
        "payload",
    );
    let payload_hash = domain_hash(Domain::PayloadHash, payload.as_bytes());
    let public_key = actor.signing_key.verifying_key().to_bytes();
    let mut builder = EnvelopeBuilder::new();
    must(
        builder
            .protocol_version(PROTOCOL_VERSION)
            .and_then(|value| value.network_id(NETWORK_ID))
            .and_then(|value| value.activity_type(activity_type))
            .and_then(|value| value.actor_did(must(Did::new(actor.did.as_bytes()), "did")))
            .and_then(|value| value.authority(must(Authority::owner(&public_key), "authority")))
            .and_then(|value| value.account_sequence(sequence))
            .and_then(|value| {
                value.timestamp_bound(must(
                    TimestampBound::new(not_before, expires_at),
                    "timestamp bound",
                ))
            })
            .and_then(|value| value.idempotency_key(IdempotencyKey::new(idempotency)))
            .and_then(|value| value.fee_limit(Amount::from_u128(0)))
            .and_then(|value| value.payload_hash(payload_hash))
            .and_then(|value| value.payload(payload))
            .map(|_| ()),
        "envelope",
    );
    let unsigned = must(builder.build(), "unsigned envelope");
    let unsigned_bytes = must(encode_unsigned_envelope(&unsigned), "unsigned bytes");
    let signature = actor
        .signing_key
        .sign(&domain_hash(Domain::SignaturePreimage, &unsigned_bytes))
        .to_bytes();
    let signed = unsigned.attach_signature(must(Signature::new(&signature), "signature"));
    must(encode_signed_envelope(&signed), "signed bytes")
}

struct Submitted {
    activity_id: [u8; 32],
    receipt: Vec<u8>,
}

fn submit_and_wait(
    transport: &mut dyn FrameTransport,
    signed: &[u8],
    correlation: u64,
) -> Submitted {
    let decoded = must(decode_signed(signed, &registry()), "signed activity decode");
    let expected_id = must(activity_id(&decoded), "activity id");
    let (tag, retained, evidence) = exchange(transport, 3, correlation, signed);
    assert_eq!(
        tag,
        4,
        "submit was refused: {:?}",
        decode_core_refusal(&retained)
    );
    assert_eq!(retained, signed);
    assert_eq!(evidence.as_slice(), expected_id);
    let mut selector = Vec::with_capacity(33);
    selector.push(1);
    selector.extend_from_slice(&expected_id);
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut attempt = 0_u64;
    loop {
        let (tag, receipt, proof) = exchange(transport, 5, correlation + 1000 + attempt, &selector);
        assert_eq!(tag, 6);
        assert!(proof.is_empty());
        if !receipt.is_empty() {
            return Submitted {
                activity_id: expected_id,
                receipt,
            };
        }
        assert!(Instant::now() < deadline, "receipt never became durable");
        attempt += 1;
        thread::sleep(Duration::from_millis(50));
    }
}

struct HttpAnswer {
    status: u16,
    content_type: String,
    body: Vec<u8>,
    headers: BTreeMap<String, String>,
}

fn parse_http(raw: &[u8]) -> HttpAnswer {
    let end = raw
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .unwrap_or_else(|| panic!("HTTP response without header terminator"));
    let head = String::from_utf8_lossy(&raw[..end]).into_owned();
    let mut lines = head.split("\r\n");
    let status = lines
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|code| code.parse::<u16>().ok())
        .unwrap_or_else(|| panic!("HTTP status line missing in {head}"));
    let mut content_type = String::new();
    let mut content_length = None;
    let mut headers = BTreeMap::new();
    for line in lines {
        if let Some((name, value)) = line.split_once(':') {
            assert!(
                headers
                    .insert(name.to_ascii_lowercase(), value.trim().to_owned())
                    .is_none(),
                "duplicate header"
            );
            if name.eq_ignore_ascii_case("content-type") {
                value.trim().clone_into(&mut content_type);
            }
            if name.eq_ignore_ascii_case("content-length") {
                content_length = value.trim().parse::<usize>().ok();
            }
        }
    }
    let body = raw[end + 4..].to_vec();
    assert_eq!(content_length, Some(body.len()), "content length mismatch");
    HttpAnswer {
        status,
        content_type,
        body,
        headers,
    }
}

fn https_get(port: u16, certificate: &Certificate, path: &str, bearer: Option<&str>) -> HttpAnswer {
    let connector = must(
        TlsConnector::builder()
            .add_root_certificate(certificate.clone())
            .build(),
        "TLS connector",
    );
    let tcp = must(TcpStream::connect(("127.0.0.1", port)), "authority connect");
    must(
        tcp.set_read_timeout(Some(Duration::from_secs(30))),
        "read timeout",
    );
    let mut stream = must(connector.connect("localhost", tcp), "TLS handshake");
    let authorization = bearer.map_or(String::new(), |token| {
        format!("Authorization: Bearer {token}\r\n")
    });
    must(
        stream.write_all(
            format!("GET {path} HTTP/1.1\r\nHost: localhost\r\n{authorization}Connection: close\r\n\r\n")
                .as_bytes(),
        ),
        "request write",
    );
    let mut raw = Vec::new();
    let _ = stream.read_to_end(&mut raw);
    parse_http(&raw)
}

fn persistent_https_get(
    stream: &mut (impl Read + Write),
    path: &str,
    bearer: Option<&str>,
    close: bool,
) -> HttpAnswer {
    let authorization = bearer.map_or(String::new(), |token| {
        format!("Authorization: Bearer {token}\r\n")
    });
    let connection = if close { "close" } else { "keep-alive" };
    must(
        stream.write_all(
            format!("GET {path} HTTP/1.1\r\nHost: localhost\r\n{authorization}Connection: {connection}\r\n\r\n")
                .as_bytes(),
        ),
        "persistent request write",
    );
    let mut raw = Vec::with_capacity(2048);
    while !raw.ends_with(b"\r\n\r\n") {
        let mut byte = [0_u8; 1];
        must(stream.read_exact(&mut byte), "persistent response headers");
        raw.push(byte[0]);
        assert!(raw.len() <= 8192, "persistent response headers are bounded");
    }
    let headers = std::str::from_utf8(&raw).unwrap_or_else(|_| panic!("response headers UTF-8"));
    let content_length = headers
        .lines()
        .find_map(|line| {
            line.split_once(':').and_then(|(name, value)| {
                if name.eq_ignore_ascii_case("content-length") {
                    value.trim().parse::<usize>().ok()
                } else {
                    None
                }
            })
        })
        .unwrap_or_else(|| panic!("persistent content length"));
    assert!(
        content_length <= 8 * 1024 * 1024,
        "persistent response is bounded"
    );
    let header_length = raw.len();
    raw.resize(header_length + content_length, 0);
    must(
        stream.read_exact(&mut raw[header_length..]),
        "persistent response body",
    );
    parse_http(&raw)
}

fn http_get(port: u16, path: &str, bearer: &str) -> HttpAnswer {
    let mut stream = must(TcpStream::connect(("127.0.0.1", port)), "replica connect");
    must(
        stream.set_read_timeout(Some(Duration::from_secs(30))),
        "read timeout",
    );
    must(
        stream.write_all(
            format!("GET {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nAuthorization: Bearer {bearer}\r\nConnection: close\r\n\r\n")
                .as_bytes(),
        ),
        "request write",
    );
    let mut raw = Vec::new();
    let _ = stream.read_to_end(&mut raw);
    parse_http(&raw)
}

fn json(answer: &HttpAnswer) -> serde_json::Value {
    must(serde_json::from_slice(&answer.body), "JSON body")
}

fn field<'a>(value: &'a serde_json::Value, name: &str) -> &'a str {
    value[name]
        .as_str()
        .unwrap_or_else(|| panic!("field {name} missing in {value}"))
}

fn error_code(answer: &HttpAnswer) -> String {
    let value = json(answer);
    field(&value["error"], "code").to_owned()
}

struct Cluster {
    root: PathBuf,
    sequencer: Option<Daemon>,
    replica: Daemon,
    authority: Daemon,
    authority_command: Command,
    authority_port: u16,
    replica_port: u16,
    replica_token: String,
    gateway_token: String,
    registry_token: String,
    evidence_token: String,
    tokens_dir: PathBuf,
    certificate: Certificate,
    socket: PathBuf,
    gate: ConnectionGate,
    asset: [u8; 32],
    replica_id: [u8; 32],
    sequencer_key: [u8; 32],
    sequencer_id: [u8; 32],
    actor: Actor,
}

#[allow(clippy::too_many_lines)]
fn start_cluster(with_sequencer: bool) -> Cluster {
    start_cluster_with_lni(with_sequencer, None)
}

fn start_cluster_with_lni(with_sequencer: bool, live_lni: Option<&serde_json::Value>) -> Cluster {
    let fixture_network_id = live_lni.map_or(NETWORK_ID, |lni| {
        u32::try_from(lni["network_id"].as_u64().expect("real LNI network")).expect("network width")
    });
    if let Some(lni) = live_lni {
        assert!(!with_sequencer);
        assert_eq!(lni["network_id"], serde_json::json!(fixture_network_id));
        assert!(Path::new(lni["node_socket"].as_str().expect("real LNI path")).is_absolute());
    }
    let identity = identity();
    let repository = repository_root();
    let binaries = std::env::var_os("LAYERX_TEST_NATIVE_BIN_DIR")
        .map_or_else(|| repository.join("build/bin"), PathBuf::from);
    let layerxd = binaries.join("layerxd");
    let builder = binaries.join("layerx-genesis-build");
    assert!(layerxd.is_file(), "{} is not built", layerxd.display());
    assert!(builder.is_file(), "{} is not built", builder.display());
    let root = std::env::temp_dir().join(format!(
        "layerx-authority-{}-{}-{}",
        std::process::id(),
        now_ms(),
        hex::encode(&random32()[..8])
    ));
    make_dir(&root, 0o755);
    let genesis = build_genesis(&root, &builder);
    let daemon_binary = root.join("layerxd");
    must(
        fs::copy(&layerxd, &daemon_binary),
        "copy layerxd into the harness root",
    );
    must(
        fs::set_permissions(&daemon_binary, fs::Permissions::from_mode(0o755)),
        "allow the isolated replica uid to execute its private copy",
    );
    let layerxd = daemon_binary;
    let migrations = root.join("migrations");
    make_dir(&migrations, 0o755);
    for entry in must(
        fs::read_dir(repository.join("migrations")),
        "migrations listing",
    ) {
        let source = must(entry, "migration entry").path();
        if source.is_file() {
            let name = source
                .file_name()
                .unwrap_or_else(|| panic!("migration file name"));
            must(fs::copy(&source, migrations.join(name)), "copy migration");
        }
    }

    let sequencer_seed = random32();
    let sequencer_signing = SigningKey::from_bytes(&sequencer_seed);
    let sequencer_key = live_lni.map_or_else(
        || sequencer_signing.verifying_key().to_bytes(),
        |lni| {
            must(
                hex::decode32(lni["sequencer_public_key"].as_str().expect("real LNI key")),
                "real LNI key",
            )
        },
    );
    let sequencer_id = live_lni.map_or_else(random32, |lni| {
        must(
            hex::decode32(lni["sequencer_id"].as_str().expect("real sequencer id")),
            "real sequencer id",
        )
    });
    let replica_id = random32();
    let replica_token = token();
    let program_token = token();
    let replica_port = free_port();
    let program_port = free_port();
    let authority_port = free_port();

    let replica_dir = root.join("replica");
    make_dir(&replica_dir, 0o700);
    write(
        &replica_dir.join("config.txt"),
        node_config("replica")
            .replace(
                &format!("network_id={NETWORK_ID}"),
                &format!("network_id={fixture_network_id}"),
            )
            .as_bytes(),
        0o600,
    );
    preallocate_log(&replica_dir.join("replica.log"));
    chown_tree(&replica_dir, identity.daemon_uid, identity.daemon_gid);
    let mut replica_env = BTreeMap::new();
    replica_env.insert(
        "LAYERX_AUTHORITY_REPLICA_LOG",
        replica_dir
            .join("replica.log")
            .to_string_lossy()
            .into_owned(),
    );
    replica_env.insert("LAYERX_AUTHORITY_REPLICA_ID", hex::encode(&replica_id));
    replica_env.insert("LAYERX_AUTHORITY_SEQUENCER_ID", hex::encode(&sequencer_id));
    replica_env.insert(
        "LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY",
        hex::encode(&sequencer_key),
    );
    replica_env.insert("LAYERX_AUTHORITY_FIRST_BATCH", FIRST_BATCH.to_string());
    replica_env.insert("LAYERX_AUTHORITY_LAST_BATCH", LAST_BATCH.to_string());
    replica_env.insert("LAYERX_AUTHORITY_BEARER_TOKEN", replica_token.clone());
    replica_env.insert("LAYERX_AUTHORITY_ADDRESS", "127.0.0.1".to_owned());
    replica_env.insert("LAYERX_AUTHORITY_PORT", replica_port.to_string());
    let mut replica = spawn_daemon(
        &layerxd,
        "--authority-replica",
        &replica_dir.join("config.txt"),
        &replica_env,
        &identity,
        root.join("replica.stderr"),
    );
    wait_for_port(replica_port, &mut replica, "authority replica");
    assert!(
        must(
            fs::metadata(replica_dir.join("replica.log")),
            "replica log metadata"
        )
        .len()
            > 0,
        "replica did not grow its fresh log"
    );

    let node_dir = root.join("node");
    let checkpoints = node_dir.join("checkpoints");
    let logs = node_dir.join("logs");
    let run_dir = root.join("run");
    make_dir(&node_dir, 0o700);
    make_dir(&checkpoints, 0o700);
    make_dir(&logs, 0o700);
    make_dir(&run_dir, 0o750);
    write(
        &node_dir.join("registration.lxgr"),
        &registration(&genesis.receipt_state_root),
        0o600,
    );
    let actor = actor();
    let identities = format!(
        "{}:{}:1\n",
        hex::encode(actor.did.as_bytes()),
        hex::encode(&actor.signing_key.verifying_key().to_bytes())
    );
    write(
        &node_dir.join("identities.txt"),
        identities.as_bytes(),
        0o600,
    );
    write(
        &node_dir.join("config.txt"),
        node_config("sequencer").as_bytes(),
        0o600,
    );
    for name in [
        "feed.log",
        "canonical.log",
        "receipt-authority.log",
        "batch.log",
        "evidence.log",
    ] {
        preallocate_log(&logs.join(name));
    }
    chown_tree(&genesis.directory, identity.daemon_uid, identity.daemon_gid);
    chown_tree(&node_dir, identity.daemon_uid, identity.daemon_gid);
    chown_tree(&run_dir, identity.daemon_uid, identity.client_gid);
    let socket = live_lni.map_or_else(
        || run_dir.join("layerxd.sock"),
        |lni| PathBuf::from(lni["node_socket"].as_str().expect("real LNI socket")),
    );
    let text = |path: PathBuf| path.to_string_lossy().into_owned();
    let mut node_env = BTreeMap::new();
    if with_sequencer {
        let (paxeer_chain_id, paxeer_rpc_port) = serve_recorded_chain();
        node_env.insert("LAYERX_NODE_PAXEER_CHAIN_ID", paxeer_chain_id.to_string());
        node_env.insert(
            "LAYERX_NODE_PAXEER_RPC_URL",
            format!("http://127.0.0.1:{paxeer_rpc_port}"),
        );
    }
    node_env.insert(
        "LAYERX_NODE_CHECKPOINT_DIRECTORY",
        text(checkpoints.clone()),
    );
    node_env.insert(
        "LAYERX_NODE_SNAPSHOT",
        text(genesis.directory.join("00000000000000000000.lxs")),
    );
    node_env.insert(
        "LAYERX_NODE_GENESIS_MANIFEST",
        text(genesis.directory.join("genesis.manifest")),
    );
    node_env.insert(
        "LAYERX_NODE_GENESIS_REGISTRATION",
        text(node_dir.join("registration.lxgr")),
    );
    node_env.insert(
        "LAYERX_NODE_IDENTITIES",
        text(node_dir.join("identities.txt")),
    );
    node_env.insert("LAYERX_NODE_PROGRAM_FEED_LOG", text(logs.join("feed.log")));
    node_env.insert(
        "LAYERX_NODE_CANONICAL_LOG",
        text(logs.join("canonical.log")),
    );
    node_env.insert(
        "LAYERX_NODE_RECEIPT_AUTHORITY_LOG",
        text(logs.join("receipt-authority.log")),
    );
    node_env.insert("LAYERX_NODE_BATCH_LOG", text(logs.join("batch.log")));
    node_env.insert("LAYERX_NODE_EVIDENCE_LOG", text(logs.join("evidence.log")));
    node_env.insert(
        "LAYERX_NODE_HISTORY_DATABASE",
        text(node_dir.join("history.db")),
    );
    node_env.insert(
        "LAYERX_NODE_HISTORY_MIGRATIONS",
        text(migrations.join("0007_history_index.sql")),
    );
    node_env.insert("LAYERX_NODE_SEQUENCER_ID", hex::encode(&sequencer_id));
    node_env.insert(
        "LAYERX_NODE_SEQUENCER_PUBLIC_KEY",
        hex::encode(&sequencer_key),
    );
    node_env.insert(
        "LAYERX_NODE_SEQUENCER_PRIVATE_KEY",
        hex::encode(&sequencer_seed),
    );
    node_env.insert("LAYERX_NODE_FIRST_BATCH", FIRST_BATCH.to_string());
    node_env.insert("LAYERX_NODE_LAST_BATCH", LAST_BATCH.to_string());
    node_env.insert(
        "LAYERX_NODE_AUTHORITY_REPLICA_ADDRESS",
        "127.0.0.1".to_owned(),
    );
    node_env.insert(
        "LAYERX_NODE_AUTHORITY_REPLICA_PORT",
        replica_port.to_string(),
    );
    node_env.insert("LAYERX_NODE_AUTHORITY_REPLICA_ID", hex::encode(&replica_id));
    node_env.insert(
        "LAYERX_NODE_AUTHORITY_REPLICA_BEARER_TOKEN",
        replica_token.clone(),
    );
    node_env.insert("LAYERX_NODE_PROGRAM_BEARER_TOKEN", program_token);
    node_env.insert("LAYERX_NODE_PROGRAM_ADDRESS", "127.0.0.1".to_owned());
    node_env.insert("LAYERX_NODE_PROGRAM_PORT", program_port.to_string());
    node_env.insert("LAYERX_NODE_LNI_SOCKET", text(socket.clone()));
    node_env.insert(
        "LAYERX_NODE_LNI_ALLOWED_UID",
        identity.client_uid.to_string(),
    );
    node_env.insert(
        "LAYERX_NODE_LNI_ALLOWED_GID",
        identity.client_gid.to_string(),
    );
    node_env.insert("LAYERX_NODE_LNI_FRAME_BYTES", LNI_FRAME_BYTES.to_string());
    node_env.insert("LAYERX_NODE_LNI_DEADLINE_MS", "10000".to_owned());
    let gate = ConnectionGate::new(8);
    let sequencer = if with_sequencer {
        let mut sequencer = spawn_daemon(
            &layerxd,
            "--serve",
            &node_dir.join("config.txt"),
            &node_env,
            &identity,
            root.join("sequencer.stderr"),
        );
        wait_for_lni(&socket, &gate, &mut sequencer);
        for name in [
            "feed.log",
            "canonical.log",
            "receipt-authority.log",
            "batch.log",
            "evidence.log",
        ] {
            assert!(
                must(fs::metadata(logs.join(name)), "sequencer log metadata").len() > 0,
                "sequencer did not grow fresh log {name}"
            );
        }
        Some(sequencer)
    } else {
        None
    };

    let tls_dir = root.join("tls");
    make_dir(&tls_dir, 0o700);
    let certificate_pem = tls_dir.join("server.pem");
    let certificate_der = tls_dir.join("server.der");
    let key_pem = tls_dir.join("server-key.pem");
    let key_der = tls_dir.join("server-key.der");
    command(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "ec",
            "-pkeyopt",
            "ec_paramgen_curve:P-256",
            "-nodes",
            "-keyout",
            &key_pem.to_string_lossy(),
            "-out",
            &certificate_pem.to_string_lossy(),
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost",
        ],
    );
    command(
        "openssl",
        &[
            "x509",
            "-in",
            &certificate_pem.to_string_lossy(),
            "-outform",
            "DER",
            "-out",
            &certificate_der.to_string_lossy(),
        ],
    );
    command(
        "openssl",
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            &key_pem.to_string_lossy(),
            "-outform",
            "DER",
            "-out",
            &key_der.to_string_lossy(),
        ],
    );
    let certificate = must(
        Certificate::from_der(&must(fs::read(&certificate_der), "certificate")),
        "certificate parse",
    );
    let tokens_dir = root.join("tokens");
    make_dir(&tokens_dir, 0o700);
    let gateway_token = token();
    let registry_token = token();
    write(
        &tokens_dir.join("gateway.token"),
        format!("{gateway_token}\n").as_bytes(),
        0o600,
    );
    write(
        &tokens_dir.join("registry.token"),
        registry_token.as_bytes(),
        0o600,
    );
    write(
        &tokens_dir.join("replica.token"),
        replica_token.as_bytes(),
        0o600,
    );
    let tokens_dir = must(fs::canonicalize(&tokens_dir), "canonical token directory");
    let evidence_token = token();
    write(
        &tokens_dir.join("evidence-read.token"),
        evidence_token.as_bytes(),
        0o600,
    );
    let authority_stderr = root.join("authority.stderr");
    let authority_binary = std::env::var_os("PAXEER_X_AUTHORITY_BIN")
        .unwrap_or_else(|| env!("CARGO_BIN_EXE_layerx-receipt-authority").into());
    let mut authority_command = Command::new(authority_binary);
    authority_command
        .env_clear()
        .env(
            "LAYERX_AUTHORITY_LISTEN",
            format!("127.0.0.1:{authority_port}"),
        )
        .env("LAYERX_AUTHORITY_TLS_CERT_DER", &certificate_der)
        .env("LAYERX_AUTHORITY_TLS_KEY_DER", &key_der)
        .env(
            "LAYERX_AUTHORITY_TOKEN_FILES",
            format!(
                "{}:{}",
                tokens_dir.join("gateway.token").display(),
                tokens_dir.join("registry.token").display()
            ),
        )
        .env(
            "LAYERX_AUTHORITY_REPLICA_URL",
            format!("http://127.0.0.1:{replica_port}"),
        )
        .env(
            "LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE",
            tokens_dir.join("replica.token"),
        )
        .env(
            "LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE",
            tokens_dir.join("evidence-read.token"),
        )
        .env("LAYERX_AUTHORITY_REPLICA_ID", hex::encode(&replica_id))
        .env("LAYERX_AUTHORITY_LNI_SOCKET", &socket)
        .env(
            "LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID",
            fixture_network_id.to_string(),
        )
        .env("LAYERX_AUTHORITY_NETWORK_ID", NETWORK_NAME)
        .env(
            "LAYERX_AUTHORITY_WIRE_VERSION",
            PROTOCOL_VERSION.to_string(),
        )
        .env("LAYERX_AUTHORITY_SEQUENCER_ID", hex::encode(&sequencer_id))
        .env(
            "LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY",
            hex::encode(&sequencer_key),
        )
        .env("LAYERX_AUTHORITY_FIRST_BATCH", FIRST_BATCH.to_string())
        .env("LAYERX_AUTHORITY_LAST_BATCH", LAST_BATCH.to_string())
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::from(must(
            fs::File::create(&authority_stderr),
            "authority stderr file",
        )));
    let mut authority = Daemon {
        child: must(authority_command.spawn(), "spawn authority"),
        stderr: authority_stderr,
    };
    wait_for_port(authority_port, &mut authority, "receipt authority");
    Cluster {
        root,
        sequencer,
        replica,
        authority,
        authority_command,
        authority_port,
        replica_port,
        replica_token,
        gateway_token,
        registry_token,
        evidence_token,
        tokens_dir,
        certificate,
        socket,
        gate,
        asset: genesis.asset,
        replica_id,
        sequencer_key,
        sequencer_id,
        actor,
    }
}

impl Drop for Cluster {
    fn drop(&mut self) {
        self.authority.stop();
        if let Some(sequencer) = self.sequencer.as_mut() {
            sequencer.stop();
        }
        self.replica.stop();
        if std::thread::panicking() {
            if let Some(sequencer) = self.sequencer.as_ref() {
                eprintln!("sequencer stderr:\n{}", sequencer.diagnostics());
            }
            eprintln!("replica stderr:\n{}", self.replica.diagnostics());
            eprintln!("authority stderr:\n{}", self.authority.diagnostics());
        } else if std::env::var_os("PAXEER_X_AUTHORITY_RETAIN_STATE").is_none() {
            let _ = fs::remove_dir_all(&self.root);
        }
    }
}

fn ask(cluster: &Cluster, path: &str, bearer: &str) -> HttpAnswer {
    https_get(
        cluster.authority_port,
        &cluster.certificate,
        path,
        Some(bearer),
    )
}

fn refused_start(command: &mut Command, expected: &str, secret: &str) {
    command.stdout(Stdio::null()).stderr(Stdio::piped());
    let mut child = must(command.spawn(), "spawn authority under a refused configuration");
    let deadline = Instant::now() + Duration::from_secs(30);
    let status = loop {
        if let Some(status) = must(child.try_wait(), "refused authority status") {
            break status;
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            panic!("authority started under a configuration that must be refused: {expected}");
        }
        thread::sleep(Duration::from_millis(50));
    };
    let mut stderr = String::new();
    must(
        child
            .stderr
            .take()
            .unwrap_or_else(|| panic!("refused authority stderr pipe"))
            .read_to_string(&mut stderr),
        "refused authority stderr",
    );
    assert!(!status.success(), "refused authority exited successfully");
    assert!(stderr.contains(expected), "unexpected startup refusal: {stderr}");
    assert!(
        secret.is_empty() || !stderr.contains(secret),
        "startup refusal must not reveal a token"
    );
}

fn authority_facts(answer: &HttpAnswer) -> AuthorizedBatch {
    let value = json(answer);
    AuthorizedBatch::new(
        must(hex::decode32(field(&value, "batch_id")), "batch_id"),
        must(hex::decode32(field(&value, "asset")), "asset"),
        must(
            hex::decode32(field(&value, "previous_state_root")),
            "previous_state_root",
        ),
        must(
            hex::decode32(field(&value, "resulting_state_root")),
            "resulting_state_root",
        ),
        must(
            hex::decode32(field(&value, "sequencer_public_key")),
            "sequencer_public_key",
        ),
    )
}

#[test]
#[allow(clippy::too_many_lines)]
fn real_node_authority_serves_verified_facts_and_reflects_replica_loss() {
    let mut cluster = start_cluster(true);
    let mut transport = connect_lni(&cluster.socket, &cluster.gate);
    let admission_log = cluster
        .root
        .join("node/checkpoints/.layerxd-lni-admission.log");
    let admission_before = must(fs::metadata(&admission_log), "admission log").len();
    let malformed = signed_send_payload(&cluster.actor, cluster.asset, 1, true);
    let (tag, refusal, proof) = exchange(&mut transport, 3, 9000, &malformed);
    assert_eq!(tag, 25);
    assert!(proof.is_empty());
    let refusal = decode_core_refusal(&refusal).unwrap_or_else(|| panic!("typed decode refusal"));
    assert_eq!(refusal.class, 4);
    assert_eq!(
        refusal.result.raw(),
        layerx_types::result::KnownResult::MalformedSend.raw()
    );
    assert_eq!(
        must(fs::metadata(&admission_log), "admission log after refusal").len(),
        admission_before
    );
    let signed = signed_send(&cluster.actor, cluster.asset, 1);
    let submitted = submit_and_wait(&mut transport, &signed, 10_000);
    let signed_second = signed_send(&cluster.actor, cluster.asset, 2);
    let second = submit_and_wait(&mut transport, &signed_second, 20_000);
    drop(transport);
    let activity_hex = hex::encode(&submitted.activity_id);
    let authorization = SequencerAuthorization::new(
        cluster.sequencer_id,
        cluster.sequencer_key,
        FIRST_BATCH,
        LAST_BATCH,
    );

    let live = https_get(cluster.authority_port, &cluster.certificate, "/livez", None);
    assert_eq!(live.status, 200);
    assert_eq!(live.content_type, "application/json");

    let connector = must(
        TlsConnector::builder()
            .add_root_certificate(cluster.certificate.clone())
            .build(),
        "persistent TLS connector",
    );
    let tcp = must(
        TcpStream::connect(("127.0.0.1", cluster.authority_port)),
        "persistent authority connect",
    );
    must(
        tcp.set_read_timeout(Some(Duration::from_secs(30))),
        "persistent authority timeout",
    );
    let mut persistent = must(
        connector.connect("localhost", tcp),
        "persistent authority TLS handshake",
    );
    let first_on_connection = persistent_https_get(&mut persistent, "/livez", None, false);
    let second_on_connection = persistent_https_get(&mut persistent, "/readyz", None, true);
    assert_eq!(first_on_connection.status, 200);
    assert_eq!(second_on_connection.status, 200);

    let ready = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(
        ready.status,
        200,
        "{}",
        String::from_utf8_lossy(&ready.body)
    );
    assert_eq!(ready.content_type, "application/json");
    let ready_body = json(&ready);
    assert_eq!(ready_body["ready"], serde_json::Value::Bool(true));
    assert_eq!(field(&ready_body, "network_id"), NETWORK_NAME);
    assert_eq!(
        field(&ready_body, "wire_version"),
        PROTOCOL_VERSION.to_string()
    );
    assert_eq!(
        ready_body["protocol_network_id"],
        serde_json::Value::from(NETWORK_ID)
    );
    assert_eq!(ready_body.as_object().map(serde_json::Map::len), Some(4));

    let unauthenticated = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        None,
    );
    assert_eq!(unauthenticated.status, 401);
    assert_eq!(error_code(&unauthenticated), "identity_required");
    let wrong_token = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        Some(&token()),
    );
    assert_eq!(wrong_token.status, 401);

    let gateway_view = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        Some(&cluster.gateway_token),
    );
    assert_eq!(
        gateway_view.status,
        200,
        "{}",
        String::from_utf8_lossy(&gateway_view.body)
    );
    assert_eq!(gateway_view.content_type, "application/json");
    let facts = json(&gateway_view);
    if let Some(path) = std::env::var_os("LAYERX_TEST_MAINTAINED_AUTHORITY_FIXTURE") {
        let capture = serde_json::json!({
            "authority": facts,
            "receipt_hex": hex::encode(&submitted.receipt),
            "sequencer_id": hex::encode(&cluster.sequencer_id),
            "sequencer_public_key": hex::encode(&cluster.sequencer_key),
            "first_batch": FIRST_BATCH.to_string(),
            "last_batch": LAST_BATCH.to_string(),
        });
        write(
            Path::new(&path),
            &must(serde_json::to_vec_pretty(&capture), "public evidence"),
            0o644,
        );
    }
    let keys: Vec<&String> = facts
        .as_object()
        .unwrap_or_else(|| panic!("facts must be an object"))
        .keys()
        .collect();
    assert_eq!(
        keys,
        [
            "activity_id",
            "asset",
            "batch_evidence",
            "batch_id",
            "network_id",
            "previous_state_root",
            "protocol_network_id",
            "receipt",
            "resulting_state_root",
            "sequencer_public_key",
            "wire_version",
        ]
        .iter()
        .map(|key| (*key).to_owned())
        .collect::<Vec<String>>()
        .iter()
        .collect::<Vec<&String>>()
    );
    assert_eq!(field(&facts, "activity_id"), activity_hex);
    assert_eq!(field(&facts, "receipt"), hex::encode(&submitted.receipt));
    assert_eq!(field(&facts, "network_id"), NETWORK_NAME);
    assert_eq!(field(&facts, "wire_version"), PROTOCOL_VERSION.to_string());
    assert_eq!(
        facts["protocol_network_id"],
        serde_json::Value::from(NETWORK_ID)
    );
    assert_eq!(
        field(&facts, "sequencer_public_key"),
        hex::encode(&cluster.sequencer_key)
    );
    assert_eq!(field(&facts, "asset"), hex::encode(&cluster.asset));
    let authorised = authority_facts(&gateway_view);
    let served_document = serde_json::json!({
        "authority_replica_id": hex::encode(&cluster.replica_id),
        "sequencer_public_key": hex::encode(&cluster.sequencer_key),
        "batch_evidence": facts["batch_evidence"],
    });
    let served_evidence = must(
        parse_replica_evidence(
            &must(serde_json::to_vec(&served_document), "served evidence JSON"),
            cluster.replica_id,
            cluster.sequencer_key,
        ),
        "served evidence",
    );
    let served_proof = must(
        decode_proof(&served_evidence.receipt_proof),
        "served activity proof",
    );
    let BatchIdentityEvidence::BatchMaintenanceV1 {
        receipt: maintenance_bytes,
        proof: maintenance_path,
        ..
    } = &served_evidence.batch_identity
    else {
        panic!("maintained response")
    };
    let maintenance_path = must(decode_proof(maintenance_path), "served maintenance proof");
    let verified = must(
        verify_outcome_maintained(
            &submitted.receipt,
            &authorised,
            &MaintainedOutcomeEvidence {
                header: &served_evidence.header,
                header_signature: &served_evidence.header_signature,
                activity_proof: &served_proof,
                maintenance: maintenance_bytes,
                maintenance_proof: &maintenance_path,
                authorization: &authorization,
            },
        ),
        "receipt verification under the served facts",
    );
    let protocol = verified
        .receipt()
        .protocol()
        .unwrap_or_else(|| panic!("protocol receipt"));
    assert_eq!(protocol.activity_id(), submitted.activity_id);

    let webhooks_view = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/internal/v1/activities/{activity_hex}/authority"),
        Some(&cluster.registry_token),
    );
    assert_eq!(webhooks_view.status, 200);
    assert_eq!(webhooks_view.content_type, "application/json");
    assert_eq!(json(&webhooks_view), facts);

    let locator = must(receipt_locator(&submitted.receipt), "receipt locator");
    assert_eq!(hex::encode(&locator.batch_id), field(&facts, "batch_id"));
    let relay_path = format!(
        "/v1/batches/{}/receipt-authority?receipt_digest={}",
        hex::encode(&locator.batch_id),
        hex::encode(&locator.receipt_digest)
    );
    let relayed = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &relay_path,
        Some(&cluster.registry_token),
    );
    assert_eq!(
        relayed.status,
        200,
        "{}",
        String::from_utf8_lossy(&relayed.body)
    );
    assert_eq!(relayed.content_type, "application/json");
    let direct = http_get(cluster.replica_port, &relay_path, &cluster.replica_token);
    assert_eq!(direct.status, 200);
    assert_eq!(
        relayed.body, direct.body,
        "relay must not alter the replica document"
    );
    let evidence = must(
        parse_replica_evidence(&relayed.body, cluster.replica_id, cluster.sequencer_key),
        "replica evidence",
    );
    assert_eq!(served_evidence, evidence);
    let proof = must(decode_proof(&evidence.receipt_proof), "receipt proof");
    let inclusion = must(
        verify_receipt(
            &submitted.receipt,
            &proof,
            &evidence.header,
            &evidence.header_signature,
            &authorization,
        ),
        "independent inclusion verification",
    );
    let header = inclusion.header().header();
    let BatchIdentityEvidence::BatchMaintenanceV1 {
        receipt,
        proof: maintenance_proof,
        ..
    } = &evidence.batch_identity
    else {
        panic!("native maintained batch must carry maintenance evidence")
    };
    let maintenance_proof = must(decode_proof(maintenance_proof), "maintenance proof");
    must(
        verify_receipt(
            receipt,
            &maintenance_proof,
            &evidence.header,
            &evidence.header_signature,
            &authorization,
        ),
        "independent maintenance inclusion verification",
    );
    let envelope = must(
        layerx_wire::batch_maintenance::decode_batch_maintenance(receipt),
        "maintenance record",
    );
    assert_eq!(envelope.protocol_version, header.protocol_version());
    assert_eq!(envelope.epoch, header.epoch());
    assert_eq!(envelope.timestamp_ms, header.timestamp_ms());
    let maintenance = &envelope.occupancy;
    assert_eq!(maintenance.batch_number, header.batch_number());
    assert_eq!(maintenance.global_sequence, header.last_sequence());
    assert_eq!(
        maintenance.resulting_state_root,
        header.resulting_state_root()
    );
    assert_eq!(maintenance_proof.leaf_index(), proof.leaf_count() - 1);
    assert_eq!(maintenance_proof.leaf_count(), proof.leaf_count());
    assert_eq!(
        u64::from(maintenance_proof.leaf_index()),
        header.last_sequence() - header.first_sequence()
    );
    assert_eq!(
        header.first_sequence() + u64::from(proof.leaf_index()),
        protocol.global_sequence()
    );
    let expected_batch_id = must(
        program_execution_batch_id(
            header.previous_state_root(),
            header.activity_merkle_root(),
            header.first_sequence(),
            header.last_sequence() - 1,
            header.batch_number(),
        ),
        "maintained execution batch id",
    );
    assert_ne!(
        expected_batch_id,
        must(
            execution_batch_id(
                header.previous_state_root(),
                protocol.activity_id(),
                protocol.global_sequence(),
                header.batch_number()
            ),
            "legacy execution batch id"
        )
    );
    assert_eq!(hex::encode(&expected_batch_id), field(&facts, "batch_id"));
    assert_eq!(
        hex::encode(&header.previous_state_root()),
        field(&facts, "previous_state_root")
    );
    assert_eq!(
        hex::encode(&header.resulting_state_root()),
        field(&facts, "resulting_state_root")
    );
    let derived = must(
        authorized_batch_by_activity(
            submitted.activity_id,
            &submitted.receipt,
            &evidence,
            &authorization,
        ),
        "library derivation",
    );
    assert_eq!(derived.batch_id, expected_batch_id);
    assert_eq!(derived.batch_number, header.batch_number());

    let relayed_unknown = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!(
            "/v1/batches/{}/receipt-authority?receipt_digest={}",
            hex::encode(&random32()),
            hex::encode(&locator.receipt_digest)
        ),
        Some(&cluster.registry_token),
    );
    assert_eq!(relayed_unknown.status, 404);
    let relay_no_query = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!(
            "/v1/batches/{}/receipt-authority",
            hex::encode(&locator.batch_id)
        ),
        Some(&cluster.registry_token),
    );
    assert_eq!(relay_no_query.status, 400);

    let mut tampered_signature = evidence.clone();
    tampered_signature.header_signature[7] ^= 0x40;
    assert_eq!(
        authorized_batch_by_activity(
            submitted.activity_id,
            &submitted.receipt,
            &tampered_signature,
            &authorization
        ),
        Err(EvidenceRefusal::Inclusion(InclusionError::HeaderSignature))
    );
    let mut tampered_header = evidence.clone();
    let last = tampered_header.header.len() - 1;
    tampered_header.header[last] ^= 0x01;
    assert!(matches!(
        authorized_batch_by_activity(
            submitted.activity_id,
            &submitted.receipt,
            &tampered_header,
            &authorization
        ),
        Err(EvidenceRefusal::Inclusion(_))
    ));
    let second_locator = must(receipt_locator(&second.receipt), "second locator");
    assert_ne!(second_locator.batch_id, locator.batch_id);
    let second_path = format!(
        "/v1/batches/{}/receipt-authority?receipt_digest={}",
        hex::encode(&second_locator.batch_id),
        hex::encode(&second_locator.receipt_digest)
    );
    let second_document = http_get(cluster.replica_port, &second_path, &cluster.replica_token);
    assert_eq!(second_document.status, 200);
    let second_evidence: BatchEvidence = must(
        parse_replica_evidence(
            &second_document.body,
            cluster.replica_id,
            cluster.sequencer_key,
        ),
        "second evidence",
    );
    assert!(matches!(
        authorized_batch_by_activity(
            submitted.activity_id,
            &submitted.receipt,
            &second_evidence,
            &authorization
        ),
        Err(EvidenceRefusal::Inclusion(InclusionError::Merkle(_)))
    ));
    assert_eq!(
        authorized_batch_by_activity(
            second.activity_id,
            &submitted.receipt,
            &evidence,
            &authorization
        ),
        Err(EvidenceRefusal::ActivityMismatch)
    );
    let mismatched_batch = http_get(
        cluster.replica_port,
        &format!(
            "/v1/batches/{}/receipt-authority?receipt_digest={}",
            hex::encode(&second_locator.batch_id),
            hex::encode(&locator.receipt_digest)
        ),
        &cluster.replica_token,
    );
    assert_eq!(
        mismatched_batch.status, 404,
        "replica must refuse a receipt digest under a batch it was not sealed in"
    );

    let unknown = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!(
            "/v1/authorized-batches/by-activity/{}",
            hex::encode(&random32())
        ),
        Some(&cluster.gateway_token),
    );
    assert_eq!(unknown.status, 404);
    assert_eq!(error_code(&unknown), "unknown_activity");
    let malformed = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/v1/authorized-batches/by-activity/not-hex",
        Some(&cluster.gateway_token),
    );
    assert_eq!(malformed.status, 400);
    let missing = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/v1/other",
        None,
    );
    assert_eq!(missing.status, 404);

    cluster.replica.stop();
    let not_ready = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(not_ready.status, 503);
    let not_ready_body = json(&not_ready);
    assert_eq!(not_ready_body["ready"], serde_json::Value::Bool(false));
    assert_eq!(field(&not_ready_body, "network_id"), NETWORK_NAME);
    let without_replica = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        Some(&cluster.gateway_token),
    );
    assert_eq!(without_replica.status, 503);
    assert_eq!(error_code(&without_replica), "replica_unavailable");
    let relay_without_replica = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &relay_path,
        Some(&cluster.registry_token),
    );
    assert_eq!(relay_without_replica.status, 503);
    assert_eq!(error_code(&relay_without_replica), "replica_unavailable");
}

#[test]
#[allow(clippy::too_many_lines)]
fn real_replica_readiness_relay_and_refusals_without_sequencer() {
    let mut cluster = start_cluster(false);
    let live = https_get(cluster.authority_port, &cluster.certificate, "/livez", None);
    assert_eq!(live.status, 200);
    assert_eq!(live.content_type, "application/json");
    assert_eq!(json(&live)["live"], serde_json::Value::Bool(true));

    let ready = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(
        ready.status,
        200,
        "{}",
        String::from_utf8_lossy(&ready.body)
    );
    let ready_body = json(&ready);
    assert_eq!(ready_body["ready"], serde_json::Value::Bool(true));
    assert_eq!(field(&ready_body, "network_id"), NETWORK_NAME);
    assert_eq!(
        field(&ready_body, "wire_version"),
        PROTOCOL_VERSION.to_string()
    );

    let activity_hex = hex::encode(&random32());
    let unauthenticated = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        None,
    );
    assert_eq!(unauthenticated.status, 401);
    assert_eq!(error_code(&unauthenticated), "identity_required");
    let wrong_token = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/internal/v1/activities/{activity_hex}/authority"),
        Some(&token()),
    );
    assert_eq!(wrong_token.status, 401);

    let no_receipt_source = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        Some(&cluster.gateway_token),
    );
    assert_eq!(no_receipt_source.status, 503);
    assert_eq!(error_code(&no_receipt_source), "receipt_source_unavailable");
    let malformed = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/v1/authorized-batches/by-activity/not-hex",
        Some(&cluster.gateway_token),
    );
    assert_eq!(malformed.status, 400);
    assert_eq!(error_code(&malformed), "invalid_activity_id");

    let unknown_batch = hex::encode(&random32());
    let digest = hex::encode(&random32());
    let relay_path =
        format!("/v1/batches/{unknown_batch}/receipt-authority?receipt_digest={digest}");
    let relayed = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &relay_path,
        Some(&cluster.registry_token),
    );
    let direct = http_get(cluster.replica_port, &relay_path, &cluster.replica_token);
    assert_eq!(direct.status, 404);
    assert_eq!(relayed.status, 404);
    assert_eq!(relayed.body, direct.body);
    let relay_no_query = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &format!("/v1/batches/{unknown_batch}/receipt-authority"),
        Some(&cluster.registry_token),
    );
    assert_eq!(relay_no_query.status, 400);
    assert_eq!(error_code(&relay_no_query), "invalid_request");
    let posted = {
        let connector = must(
            TlsConnector::builder()
                .add_root_certificate(cluster.certificate.clone())
                .build(),
            "TLS connector",
        );
        let tcp = must(
            TcpStream::connect(("127.0.0.1", cluster.authority_port)),
            "authority connect",
        );
        let mut stream = must(connector.connect("localhost", tcp), "TLS handshake");
        must(
            stream
                .write_all(b"POST /livez HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"),
            "request write",
        );
        let mut raw = Vec::new();
        let _ = stream.read_to_end(&mut raw);
        parse_http(&raw)
    };
    assert_eq!(posted.status, 405);

    cluster.replica.stop();
    let not_ready = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(not_ready.status, 503);
    assert_eq!(json(&not_ready)["ready"], serde_json::Value::Bool(false));
    let relay_without_replica = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &relay_path,
        Some(&cluster.registry_token),
    );
    assert_eq!(relay_without_replica.status, 503);
    assert_eq!(error_code(&relay_without_replica), "replica_unavailable");
}

#[test]
fn router_authority_readiness_schema_restart_contract() {
    let lni_path = std::env::var("PAXEER_X_AUTHORITY_READY_LNI_FIXTURE")
        .expect("a real authenticated LNI fixture is required by authority readiness");
    let live_lni: serde_json::Value = must(
        serde_json::from_slice(&must(fs::read(lni_path), "real LNI fixture")),
        "real LNI fixture JSON",
    );
    let fixture_network_id =
        u32::try_from(live_lni["network_id"].as_u64().expect("real LNI network"))
            .expect("network width");
    let gateway_tests =
        std::env::var_os("PAXEER_X_GATEWAY_TEST_BIN").expect("prebuilt gateway test binary");
    let mut cluster = start_cluster_with_lni(false, Some(&live_lni));
    assert!(cluster.sequencer.is_none());
    eprintln!("retained_state={}", cluster.root.display());
    let response = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(response.status, 200);
    let body = json(&response);
    assert_eq!(body.as_object().map(serde_json::Map::len), Some(4));
    assert_eq!(
        body["protocol_network_id"],
        serde_json::json!(fixture_network_id)
    );
    let response_file = cluster.root.join("readiness-response.json");
    write(&response_file, &response.body, 0o600);
    let untrusted_pem = cluster.root.join("tls/untrusted.pem");
    let untrusted_der = cluster.root.join("tls/untrusted.der");
    command(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "ec",
            "-pkeyopt",
            "ec_paramgen_curve:P-256",
            "-nodes",
            "-keyout",
            &cluster.root.join("tls/untrusted-key.pem").to_string_lossy(),
            "-out",
            &untrusted_pem.to_string_lossy(),
            "-days",
            "1",
            "-subj",
            "/CN=untrusted",
        ],
    );
    command(
        "openssl",
        &[
            "x509",
            "-in",
            &untrusted_pem.to_string_lossy(),
            "-outform",
            "DER",
            "-out",
            &untrusted_der.to_string_lossy(),
        ],
    );
    let mut count = 0_u64;
    let mut probe = |cluster: &Cluster,
                     expected: &str,
                     mutations: bool,
                     network: u32,
                     label: &str,
                     wire: &str,
                     trusted: bool| {
        let case = serde_json::json!({
            "ca_der": if trusted { cluster.root.join("tls/server.der") } else { untrusted_der.clone() },
            "endpoint": format!("https://localhost:{}", cluster.authority_port),
            "token_file": cluster.root.join("tokens/gateway.token"),
            "protocol_network_id": network, "network_id": label, "wire_version": wire,
            "expected": expected, "mutations": mutations, "response_file": response_file,
        });
        let path = cluster.root.join("gateway-case.json");
        write(&path, &must(serde_json::to_vec(&case), "case JSON"), 0o600);
        let result = must(
            Command::new(&gateway_tests)
                .args([
                    "authority_readiness_contract_tests::authority_readiness_contract",
                    "--exact",
                    "--nocapture",
                ])
                .env("PAXEER_X_AUTHORITY_CONTRACT_CASE", path)
                .output(),
            "gateway decoder test",
        );
        let stdout = String::from_utf8_lossy(&result.stdout);
        assert!(
            result.status.success(),
            "gateway contract: {stdout} {}",
            String::from_utf8_lossy(&result.stderr)
        );
        assert!(
            stdout.contains("1 passed; 0 failed"),
            "one real gateway test required: {stdout}"
        );
        let observed: u64 = stdout
            .lines()
            .find_map(|line| line.strip_prefix("PAXEER_X_AUTHORITY_CASES="))
            .expect("actual assertion count")
            .parse()
            .expect("count");
        assert!(observed > 0);
        count += observed;
    };
    let wire = PROTOCOL_VERSION.to_string();
    probe(
        &cluster,
        "ready",
        true,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        true,
    );
    probe(
        &cluster,
        "identity_mismatch",
        false,
        fixture_network_id + 1,
        NETWORK_NAME,
        &wire,
        true,
    );
    probe(
        &cluster,
        "identity_mismatch",
        false,
        fixture_network_id,
        "different",
        &wire,
        true,
    );
    probe(
        &cluster,
        "identity_mismatch",
        false,
        fixture_network_id,
        NETWORK_NAME,
        "0",
        true,
    );
    probe(
        &cluster,
        "transport",
        false,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        false,
    );
    cluster.authority.stop();
    probe(
        &cluster,
        "transport",
        false,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        true,
    );
    cluster.authority_command.env(
        "LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID",
        (fixture_network_id + 1).to_string(),
    );
    cluster.authority.child = must(
        cluster.authority_command.spawn(),
        "restart authority with mismatch",
    );
    wait_for_port(
        cluster.authority_port,
        &mut cluster.authority,
        "restarted authority",
    );
    probe(
        &cluster,
        "identity_mismatch",
        false,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        true,
    );
    cluster.authority.stop();
    cluster.authority_command.env(
        "LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID",
        fixture_network_id.to_string(),
    );
    cluster.authority.child = must(
        cluster.authority_command.spawn(),
        "restart authority restored",
    );
    wait_for_port(
        cluster.authority_port,
        &mut cluster.authority,
        "restored authority",
    );
    probe(
        &cluster,
        "ready",
        false,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        true,
    );
    cluster.replica.stop();
    let unavailable = https_get(
        cluster.authority_port,
        &cluster.certificate,
        "/readyz",
        None,
    );
    assert_eq!(unavailable.status, 503);
    assert_eq!(json(&unavailable)["ready"], serde_json::json!(false));
    probe(
        &cluster,
        "unavailable",
        false,
        fixture_network_id,
        NETWORK_NAME,
        &wire,
        true,
    );
    println!("PAXEER_X_GATE tests={count} skipped=0");
}

#[test]
fn authority_lni_readiness_case() {
    let path = std::env::var("PAXEER_X_AUTHORITY_LNI_CASE").expect("real isolated case required");
    let case: serde_json::Value = must(
        serde_json::from_slice(&must(fs::read(path), "case")),
        "case JSON",
    );
    let certificate = must(
        Certificate::from_der(&must(
            fs::read(case["ca_der"].as_str().expect("CA")),
            "CA file",
        )),
        "CA DER",
    );
    let start = Instant::now();
    let answer = https_get(
        case["port"].as_u64().expect("port") as u16,
        &certificate,
        "/readyz",
        None,
    );
    assert!(
        start.elapsed() < Duration::from_secs(3),
        "readiness exceeded two-second deadline plus TLS/scheduling allowance"
    );
    let body = json(&answer);
    let ready = case["ready"].as_bool().expect("ready expectation");
    assert_eq!(answer.status, if ready { 200 } else { 503 });
    assert_eq!(body["ready"], serde_json::json!(ready));
    assert_eq!(body.as_object().expect("readiness object").len(), 4);
    for (field, header) in [
        ("replica_ready", "x-layerx-authority-replica"),
        ("lni_ready", "x-layerx-authority-lni"),
    ] {
        let expected = if case[field] == true {
            "ready"
        } else {
            "unavailable"
        };
        assert_eq!(
            answer.headers.get(header).map(String::as_str),
            Some(expected)
        );
    }
    if let Some(output) = case["response_file"].as_str() {
        write(Path::new(output), &answer.body, 0o600);
    }
    if let Some(socket) = case["node_socket"].as_str() {
        let gate = ConnectionGate::new(1);
        let mut transport = must(
            Uds::connect(Path::new(socket), &gate, lni_limits()),
            "real LNI connect",
        );
        let handshake = must(
            perform(
                &mut transport,
                &HandshakeConfig {
                    built_interface_version: Version::V1_5,
                    expected_protocol_version: PROTOCOL_VERSION,
                    expected_network_id: case["network_id"].as_u64().expect("network") as u32,
                },
                None,
            ),
            "real LNI handshake",
        );
        let capture = PathBuf::from(case["capture"].as_str().expect("capture"));
        let encoded = must(
            layerx_client::lni::handshake::encode_node_info(handshake.node()),
            "production NodeInfo serializer",
        );
        if capture.exists() {
            let before = must(
                layerx_client::lni::handshake::decode_node_info(&must(
                    fs::read(&capture),
                    "retained capture",
                )),
                "retained NodeInfo",
            );
            assert_eq!(
                handshake.node().chain_head_sequence,
                before.chain_head_sequence,
                "readiness mutated chain sequence"
            );
            assert_eq!(
                handshake.node().latest_sealed_batch,
                before.latest_sealed_batch,
                "readiness mutated batch state"
            );
        } else {
            write(&capture, &encoded, 0o600);
        }
    }
    println!("PAXEER_X_LNI_CASES=1");
}

#[test]
#[allow(clippy::too_many_lines)]
fn evidence_read_token_reads_only_receipt_authority() {
    let mut cluster = start_cluster(true);
    let mut transport = connect_lni(&cluster.socket, &cluster.gate);
    let signed = signed_send(&cluster.actor, cluster.asset, 1);
    let submitted = submit_and_wait(&mut transport, &signed, 10_000);
    drop(transport);
    let activity_hex = hex::encode(&submitted.activity_id);
    let locator = must(receipt_locator(&submitted.receipt), "receipt locator");
    let batch_hex = hex::encode(&locator.batch_id);
    let digest_hex = hex::encode(&locator.receipt_digest);
    let relay_path = format!("/v1/batches/{batch_hex}/receipt-authority?receipt_digest={digest_hex}");
    let evidence = cluster.evidence_token.clone();
    let mut cases = 0_u32;

    let general_routes = [
        format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        format!("/v1/authorized-batches/wait-by-activity/{activity_hex}"),
        format!("/internal/v1/activities/{activity_hex}/authority"),
        relay_path.clone(),
    ];
    for bearer in [&cluster.registry_token, &cluster.gateway_token] {
        for path in &general_routes {
            let answer = ask(&cluster, path, bearer);
            assert_eq!(
                answer.status,
                200,
                "{path}: {}",
                String::from_utf8_lossy(&answer.body)
            );
            assert_eq!(answer.content_type, "application/json");
            cases += 1;
        }
    }

    let registry_view = ask(&cluster, &relay_path, &cluster.registry_token);
    let evidence_view = ask(&cluster, &relay_path, &evidence);
    assert_eq!(
        evidence_view.status,
        200,
        "{}",
        String::from_utf8_lossy(&evidence_view.body)
    );
    assert_eq!(evidence_view.content_type, "application/json");
    assert_eq!(evidence_view.body, registry_view.body);
    let direct = http_get(cluster.replica_port, &relay_path, &cluster.replica_token);
    assert_eq!(direct.status, 200);
    assert_eq!(evidence_view.body, direct.body);
    let parsed = must(
        parse_replica_evidence(&evidence_view.body, cluster.replica_id, cluster.sequencer_key),
        "replica evidence read with the evidence token",
    );
    assert_eq!(
        parsed,
        must(
            parse_replica_evidence(&registry_view.body, cluster.replica_id, cluster.sequencer_key),
            "replica evidence read with the registry token",
        )
    );
    cases += 1;

    let unknown_path = format!(
        "/v1/batches/{}/receipt-authority?receipt_digest={digest_hex}",
        hex::encode(&random32())
    );
    let unknown = ask(&cluster, &unknown_path, &evidence);
    assert_eq!(unknown.status, 404);
    assert_eq!(
        unknown.body,
        ask(&cluster, &unknown_path, &cluster.registry_token).body
    );
    cases += 1;

    let no_digest_path = format!("/v1/batches/{batch_hex}/receipt-authority");
    let no_digest = ask(&cluster, &no_digest_path, &evidence);
    assert_eq!(no_digest.status, 400);
    assert_eq!(error_code(&no_digest), "invalid_request");
    assert_eq!(
        no_digest.body,
        ask(&cluster, &no_digest_path, &cluster.registry_token).body
    );
    cases += 1;

    let malformed_path = format!("/v1/batches/{batch_hex}/receipt-authority?receipt_digest=zz");
    let malformed = ask(&cluster, &malformed_path, &evidence);
    assert_eq!(malformed.status, 400);
    assert_eq!(error_code(&malformed), "invalid_request");
    cases += 1;

    let unauthenticated = https_get(
        cluster.authority_port,
        &cluster.certificate,
        &relay_path,
        None,
    );
    assert_eq!(unauthenticated.status, 401);
    assert_eq!(error_code(&unauthenticated), "identity_required");
    let stranger = ask(&cluster, &relay_path, &token());
    assert_eq!(stranger.status, 401);
    assert_eq!(error_code(&stranger), "identity_required");
    cases += 1;

    for path in &general_routes[..3] {
        let refused = ask(&cluster, path, &evidence);
        assert_eq!(refused.status, 401, "{path} admitted the evidence token");
        assert_eq!(error_code(&refused), "identity_required");
        cases += 1;
    }

    cluster.authority.stop();
    let tokens = cluster.tokens_dir.clone();
    let evidence_file = tokens.join("evidence-read.token");
    for (name, bytes, mode, expected) in [
        (
            "evidence-general.token",
            cluster.registry_token.as_bytes().to_vec(),
            0o600,
            "must differ from every general and replica bearer",
        ),
        (
            "evidence-gateway.token",
            cluster.gateway_token.as_bytes().to_vec(),
            0o600,
            "must differ from every general and replica bearer",
        ),
        (
            "evidence-replica.token",
            cluster.replica_token.as_bytes().to_vec(),
            0o600,
            "must differ from every general and replica bearer",
        ),
        (
            "evidence-group.token",
            token().into_bytes(),
            0o640,
            "is unavailable or unprotected",
        ),
        (
            "evidence-world.token",
            token().into_bytes(),
            0o604,
            "is unavailable or unprotected",
        ),
        (
            "evidence-empty.token",
            Vec::new(),
            0o600,
            "must contain 32..4096 printable bytes without whitespace",
        ),
        (
            "evidence-short.token",
            token().as_bytes()[..31].to_vec(),
            0o600,
            "must contain 32..4096 printable bytes without whitespace",
        ),
        (
            "evidence-newline.token",
            format!("{}\n", token()).into_bytes(),
            0o600,
            "must contain 32..4096 printable bytes without whitespace",
        ),
    ] {
        let path = tokens.join(name);
        write(&path, &bytes, mode);
        cluster
            .authority_command
            .env("LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE", &path);
        refused_start(
            &mut cluster.authority_command,
            expected,
            std::str::from_utf8(&bytes).unwrap_or_default(),
        );
        cases += 1;
    }
    let link = tokens.join("evidence-link.token");
    must(
        std::os::unix::fs::symlink(&evidence_file, &link),
        "evidence token symlink",
    );
    cluster
        .authority_command
        .env("LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE", &link);
    refused_start(
        &mut cluster.authority_command,
        "is unavailable or unprotected",
        &evidence,
    );
    cases += 1;
    cluster
        .authority_command
        .env("LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE", &evidence_file);

    let state = tokens.join("human-state");
    make_dir(&state, 0o700);
    write(&tokens.join("human-collision.token"), evidence.as_bytes(), 0o600);
    cluster
        .authority_command
        .env(
            "LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE",
            tokens.join("human-collision.token"),
        )
        .env("LAYERX_AUTHORITY_HUMAN_AGENT_TENANT", "tenant")
        .env("LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL", "principal")
        .env(
            "LAYERX_AUTHORITY_PRINCIPAL_POLICY_FILE",
            tokens.join("principal-policy.json"),
        )
        .env(
            "LAYERX_AUTHORITY_MODULE_REGISTRY_FILE",
            tokens.join("module-registry.json"),
        )
        .env("LAYERX_AUTHORITY_CORE_CLOCK_HORIZON", "100")
        .env("LAYERX_AUTHORITY_STATE_ROOT", &state);
    refused_start(
        &mut cluster.authority_command,
        "human token must be distinct",
        &evidence,
    );
    cases += 1;

    let human_token = token();
    write(&tokens.join("human.token"), human_token.as_bytes(), 0o600);
    let authority_stderr = cluster.root.join("authority-human.stderr");
    cluster
        .authority_command
        .env(
            "LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE",
            tokens.join("human.token"),
        )
        .stderr(Stdio::from(must(
            fs::File::create(&authority_stderr),
            "authority stderr file",
        )));
    cluster.authority.stderr = authority_stderr;
    cluster.authority.child = must(
        cluster.authority_command.spawn(),
        "restart authority with human routes",
    );
    wait_for_port(
        cluster.authority_port,
        &mut cluster.authority,
        "authority with human routes",
    );
    let agent_path = format!("/v1/agent/activities/{activity_hex}?tenant=tenant&principal=principal");
    let agent = ask(&cluster, &agent_path, &evidence);
    assert_eq!(agent.status, 401, "/v1/agent/* admitted the evidence token");
    assert_eq!(error_code(&agent), "identity_required");
    cases += 1;
    let agent_general = ask(&cluster, &agent_path, &cluster.registry_token);
    assert_eq!(agent_general.status, 401);
    assert_eq!(error_code(&agent_general), "identity_required");
    cases += 1;
    let agent_human = ask(&cluster, &agent_path, &human_token);
    assert_ne!(agent_human.status, 401, "the human token must keep its route");
    cases += 1;
    for path in &general_routes[..3] {
        let refused = ask(&cluster, path, &evidence);
        assert_eq!(refused.status, 401, "{path} admitted the evidence token");
        assert_eq!(error_code(&refused), "identity_required");
        let human_refused = ask(&cluster, path, &human_token);
        assert_eq!(human_refused.status, 401, "{path} admitted the human token");
        cases += 1;
    }
    let restarted_view = ask(&cluster, &relay_path, &evidence);
    assert_eq!(restarted_view.status, 200);
    assert_eq!(restarted_view.body, registry_view.body);
    let human_relay = ask(&cluster, &relay_path, &human_token);
    assert_eq!(human_relay.status, 401, "the human token must not read evidence");
    cases += 1;

    cluster.replica.stop();
    let unavailable = ask(&cluster, &relay_path, &evidence);
    assert_eq!(unavailable.status, 503);
    assert_eq!(error_code(&unavailable), "replica_unavailable");
    let registry_unavailable = ask(&cluster, &relay_path, &cluster.registry_token);
    assert_eq!(registry_unavailable.status, 503);
    assert_eq!(unavailable.body, registry_unavailable.body);
    cases += 1;
    let refused_while_unavailable = ask(
        &cluster,
        &format!("/v1/authorized-batches/by-activity/{activity_hex}"),
        &evidence,
    );
    assert_eq!(refused_while_unavailable.status, 401);
    cases += 1;

    println!("\nPAXEER_X_GATE tests={cases} skipped=0");
}
