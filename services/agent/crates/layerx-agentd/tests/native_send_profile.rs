use layerx_agentd::prepare::{DurablePreparation, LifecycleState};
use layerx_agentd::store::{ObjectKind, Store, TenantId, TenantKey};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{ConnectionGate, FrameTransport, Limits, Uds};
use layerx_client::Client;
use native_tls::{Certificate, Identity, TlsConnector};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::fs;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::time::Duration;

fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key]
        .as_str()
        .expect("protected real owner fixture field")
}
fn protected(path: &str, bound: u64) -> Vec<u8> {
    let info = fs::symlink_metadata(path).expect("protected fixture metadata");
    assert!(
        info.is_file()
            && info.len() > 0
            && info.len() <= bound
            && info.permissions().mode() & 0o077 == 0
    );
    fs::read(path).expect("protected fixture bytes")
}
fn id(value: &str) -> [u8; 32] {
    let bytes = layerx_programs::hex::decode(value).expect("native identifier");
    let result: [u8; 32] = bytes.try_into().expect("native identifier length");
    assert_ne!(result, [0; 32]);
    result
}
fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: 1_212_416,
        maximum_connections: 1,
        maximum_streams: 1,
        maximum_queued_bytes: 1_212_416,
        deadline: Duration::from_secs(8),
    }
}
fn node(fixture: &Value) -> Client {
    Client::connect(ClientConfig {
        endpoint: Path::new(text(fixture, "lni_socket")).to_path_buf(),
        handshake: HandshakeConfig {
            built_interface_version: Version::V1_8,
            expected_protocol_version: 3,
            expected_network_id: u32::try_from(fixture["network_id"].as_u64().expect("network"))
                .expect("network bound"),
        },
        limits: limits(),
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    })
    .expect("actual authenticated LNI owner")
}
fn rpc(fixture: &Value, request: &Value) -> (u16, Value) {
    assert!(matches!(
        request["operation"].as_str(),
        Some("prepare" | "submit")
    ));
    let host = text(fixture, "rpc_host");
    assert!(!host.is_empty() && !host.contains(['\r', '\n', '/', ':']));
    let port =
        u16::try_from(fixture["rpc_port"].as_u64().expect("TLS port")).expect("TLS port bound");
    let identity = Identity::from_pkcs8(
        &protected(text(fixture, "rpc_client_cert_pem"), 65_536),
        &protected(text(fixture, "rpc_client_key_pem"), 65_536),
    )
    .expect("real mTLS client identity");
    let connector = TlsConnector::builder()
        .identity(identity)
        .add_root_certificate(
            Certificate::from_der(&protected(text(fixture, "ca_der_file"), 65_536))
                .expect("real CA"),
        )
        .build()
        .expect("authenticated TLS connector");
    let tcp = TcpStream::connect((host, port)).expect("real AgentRPC listener");
    tcp.set_read_timeout(Some(Duration::from_secs(8)))
        .expect("read bound");
    tcp.set_write_timeout(Some(Duration::from_secs(8)))
        .expect("write bound");
    let mut stream = connector
        .connect(host, tcp)
        .expect("verified AgentRPC mTLS server");
    let body = serde_json::to_vec(request).expect("real envelope");
    assert!(body.len() <= 1_048_576);
    let header = format!("POST /rpc HTTP/1.1\r\nHost: {host}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", body.len());
    stream.write_all(header.as_bytes()).expect("TLS header");
    stream.write_all(&body).expect("TLS real mutation");
    let mut response = Vec::new();
    stream
        .take(1_212_417)
        .read_to_end(&mut response)
        .expect("bounded real response");
    assert!(response.len() <= 1_212_416);
    let end = response
        .windows(4)
        .position(|v| v == b"\r\n\r\n")
        .expect("HTTP framing")
        + 4;
    let head = std::str::from_utf8(&response[..end]).expect("HTTP header");
    let status = head
        .lines()
        .next()
        .expect("HTTP status")
        .split(' ')
        .nth(1)
        .expect("HTTP status code")
        .parse()
        .expect("HTTP numeric status");
    let length: usize = head
        .lines()
        .find_map(|line| {
            line.split_once(':')
                .filter(|(name, _)| name.eq_ignore_ascii_case("content-length"))
                .map(|(_, value)| value.trim().parse().expect("bounded HTTP length"))
        })
        .expect("HTTP response length");
    assert_eq!(length, response.len() - end);
    (
        status,
        serde_json::from_slice(&response[end..]).expect("real AgentRPC response JSON"),
    )
}
fn field(out: &mut Vec<u8>, bytes: &[u8]) {
    out.extend_from_slice(
        &u32::try_from(bytes.len())
            .expect("field bound")
            .to_be_bytes(),
    );
    out.extend_from_slice(bytes);
}
fn private(fixture: &Value, inner: &[u8], owner: &str, principal: &str) -> Vec<u8> {
    let mut body = b"LXHAGT01".to_vec();
    body.push(44);
    field(&mut body, principal.as_bytes());
    field(&mut body, owner.as_bytes());
    let account = if owner == text(fixture, "subject_owner") {
        text(fixture, "subject_account")
    } else {
        assert_eq!(owner, text(fixture, "foreign_subject_owner"));
        text(fixture, "foreign_subject_account")
    };
    field(&mut body, account.as_bytes());
    body.extend_from_slice(&id(text(fixture, "asset_id")));
    field(&mut body, inner);
    let mut transport = Uds::connect(
        Path::new(text(fixture, "human_socket")),
        &ConnectionGate::new(1),
        limits(),
    )
    .expect("real SO_PEERCRED Human peer");
    transport.send(&body).expect("real Human private request");
    transport.receive().expect("real Human private response")
}
fn operation(selector: u8, approval: Option<[u8; 32]>) -> Vec<u8> {
    let mut out = b"LXHAGT01".to_vec();
    out.push(selector);
    if let Some(id) = approval {
        out.extend_from_slice(&id);
    }
    out
}
fn load(path: &str) -> Value {
    serde_json::from_slice(&protected(path, 4_194_304))
        .expect("genuine protected native Send input")
}
fn bytes(value: &Value) -> Vec<u8> {
    value
        .as_array()
        .expect("real stored native bytes")
        .iter()
        .map(|byte| u8::try_from(byte.as_u64().expect("stored byte")).expect("byte bound"))
        .collect()
}
fn retained(fixture: &Value, preparation: [u8; 32]) -> (DurablePreparation, Vec<u8>, Value) {
    let tenant = TenantId::new(text(fixture, "agent_tenant")).expect("actual tenant");
    let store = Store::open(text(fixture, "retained_store")).expect("real durable store reopen");
    let key = DurablePreparation::store_key(&tenant, preparation).expect("actual lifecycle key");
    let record = DurablePreparation::decode(
        tenant.clone(),
        store.get(&key).expect("real retained preparation").bytes(),
    )
    .expect("real lifecycle encoding");
    let key = TenantKey::new(
        tenant,
        ObjectKind::PreparedActivity,
        [
            b"native-effect-approval-carrier-v1:".as_slice(),
            preparation.as_slice(),
        ]
        .concat(),
    )
    .expect("actual Send approval key");
    let raw = store
        .get(&key)
        .expect("real atomically persisted Send carrier")
        .bytes()
        .to_vec();
    let carrier = serde_json::from_slice(&raw).expect("real persisted Send carrier JSON");
    (record, raw, carrier)
}
fn approval(response: &[u8], preparation: [u8; 32]) -> [u8; 32] {
    assert!(response.len() >= 75);
    assert_eq!(&response[..9], b"LXHAGT01\x00");
    assert_eq!(&response[9..11], &3u16.to_be_bytes());
    assert_eq!(&response[11..43], preparation);
    response[43..75]
        .try_into()
        .expect("actual immutable hold digest")
}
fn unchanged(before: &Value, after: &Value) {
    let mut left = before.clone();
    let mut right = after.clone();
    for value in [&mut left, &mut right] {
        let map = value.as_object_mut().expect("real carrier object");
        map.remove("terminal");
        map.remove("queued_submission");
        map.remove("state");
    }
    assert_eq!(left, right);
}
struct DisposableDaemon {
    pid: u32,
    child: Option<std::process::Child>,
}
impl DisposableDaemon {
    fn reopen(&mut self, fixture: &Value) {
        let daemon = &fixture["disposable_daemon"];
        assert_eq!(daemon["allow_restart"], true);
        let binary = Path::new(text(daemon, "binary"));
        assert_eq!(
            fs::read_link(format!("/proc/{}/exe", self.pid))
                .expect("actual disposable daemon executable"),
            binary
        );
        let argv: Vec<&str> = daemon["argv"]
            .as_array()
            .expect("actual owner launch argv")
            .iter()
            .map(|value| value.as_str().expect("bounded daemon launch argument"))
            .collect();
        assert!(argv.len() >= 2 && argv[0] == binary.to_str().expect("daemon path"));
        let actual = fs::read(format!("/proc/{}/cmdline", self.pid)).expect("real daemon argv");
        let expected: Vec<u8> = argv
            .iter()
            .flat_map(|arg| arg.as_bytes().iter().copied().chain(std::iter::once(0)))
            .collect();
        assert!(
            actual == expected,
            "daemon argv must match exact disposable owner launch"
        );
        assert!(std::process::Command::new("/bin/kill")
            .args(["-TERM", &self.pid.to_string()])
            .status()
            .expect("stop expressly disposable daemon")
            .success());
        if let Some(child) = self.child.as_mut() {
            let deadline = std::time::Instant::now() + Duration::from_secs(8);
            while child.try_wait().expect("actual daemon status").is_none() {
                assert!(
                    std::time::Instant::now() < deadline,
                    "disposable daemon stop timed out"
                );
                std::thread::sleep(Duration::from_millis(20));
            }
        } else {
            let deadline = std::time::Instant::now() + Duration::from_secs(8);
            while Path::new(&format!("/proc/{}", self.pid)).exists() {
                assert!(
                    std::time::Instant::now() < deadline,
                    "disposable daemon stop timed out"
                );
                std::thread::sleep(Duration::from_millis(20));
            }
        }
        let log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(text(daemon, "restart_log"))
            .expect("private disposable daemon log");
        let child = std::process::Command::new(binary)
            .args(&argv[1..])
            .env_clear()
            .current_dir(text(daemon, "state_directory"))
            .stdin(std::process::Stdio::null())
            .stdout(log.try_clone().expect("daemon log"))
            .stderr(log)
            .spawn()
            .expect("real disposable daemon reopen with exact owner argv");
        self.pid = child.id();
        self.child = Some(child);
        let deadline = std::time::Instant::now() + Duration::from_secs(8);
        loop {
            assert!(
                self.child
                    .as_mut()
                    .unwrap()
                    .try_wait()
                    .expect("actual process status")
                    .is_none(),
                "disposable daemon exited during reopen"
            );
            if std::os::unix::net::UnixStream::connect(text(fixture, "human_socket")).is_ok() {
                break;
            }
            assert!(
                std::time::Instant::now() < deadline,
                "disposable daemon readiness timed out"
            );
            std::thread::sleep(Duration::from_millis(20));
        }
    }
}
impl Drop for DisposableDaemon {
    fn drop(&mut self) {
        if let Some(child) = self.child.as_mut() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}
fn genuine_signed_purpose(
    request: &Value,
    case: &Value,
) -> layerx_agent_api::identity::SignedNativeSendPurposeV1 {
    use ed25519_dalek::{Signature, VerifyingKey};
    use layerx_agent_api::identity::{
        NativePreparationPurposeV1, NativeSendPurposeV1, SignedNativeSendPurposeV1,
    };
    let canonical = protected(text(case, "purpose_canonical_file"), 4096);
    let purpose = NativeSendPurposeV1::from_canonical_bytes(&canonical)
        .expect("actual issuer-produced native Send statement");
    assert_eq!(
        purpose
            .canonical_bytes()
            .expect("real Send canonical statement"),
        canonical
    );
    assert!(NativePreparationPurposeV1::from_canonical_bytes(&canonical).is_err());
    let signed = SignedNativeSendPurposeV1 {
        purpose,
        owner_public_key: id(text(&request["request"]["purpose"], "owner_public_key")),
        signature: layerx_programs::hex::decode(text(&request["request"]["purpose"], "signature"))
            .expect("real owner signature")
            .try_into()
            .expect("64-byte actual signature"),
    }
    .validate()
    .expect("real Send purpose type");
    VerifyingKey::from_bytes(&signed.owner_public_key)
        .expect("genuine owner public key")
        .verify_strict(
            &<[u8; 32]>::from(Sha256::digest(&canonical)),
            &Signature::from_bytes(&signed.signature),
        )
        .expect("actual issuer owner signature over distinct Send domain");
    let statement = &signed.purpose;
    let expected = serde_json::json!({
        "version": "1", "tenant": statement.tenant.as_str(), "agent_did": statement.agent_did.as_str(),
        "owner_did": statement.owner_did.as_str(), "owner_public_key": layerx_programs::hex::encode(&statement.owner_public_key),
        "session_id": statement.session_id.as_str(), "generation": statement.generation.to_string(),
        "expires_at_ms": statement.expires_at_ms.to_string(), "capability_id": statement.capability_id.as_str(),
        "protocol_version": statement.protocol_version.to_string(), "network_id": statement.network_id.to_string(),
        "activity": {"version": "1", "module": "1", "ordinal": "5"},
        "preparation_id": layerx_programs::hex::encode(&statement.preparation_id),
        "canonical_digest": layerx_programs::hex::encode(&statement.canonical_digest),
        "economic_action": layerx_programs::hex::encode(&statement.economic_action),
        "idempotency_key": layerx_programs::hex::encode(&statement.idempotency_key),
        "commitment": layerx_programs::hex::encode(&statement.commitment),
    });
    assert!(
        request["request"]["purpose"]["purpose"] == expected,
        "wire statement must be the exact genuinely signed canonical Send statement"
    );
    for length in 0..canonical.len() {
        assert!(NativeSendPurposeV1::from_canonical_bytes(&canonical[..length]).is_err());
    }
    let mut trailing = canonical.clone();
    trailing.push(0);
    assert!(NativeSendPurposeV1::from_canonical_bytes(&trailing).is_err());
    let mut changed = canonical;
    *changed.last_mut().expect("nonempty genuine statement") ^= 1;
    assert!(VerifyingKey::from_bytes(&signed.owner_public_key)
        .unwrap()
        .verify_strict(
            &<[u8; 32]>::from(Sha256::digest(&changed)),
            &Signature::from_bytes(&signed.signature)
        )
        .is_err());
    signed
}
fn genuine_grant(request: &Value, purpose: &layerx_agent_api::identity::NativeSendPurposeV1) {
    use layerx_agentd::capability::binding::SignedNativeLocalGrantV1;
    use layerx_agentd::capability::timed::NativeTimedCapabilityV1;
    use layerx_agentd::session::NativeSessionScopeV1;
    let wire = &request["request"]["local_grant"];
    assert!(
        wire.is_object(),
        "genuine issuer-signed native local grant required"
    );
    let capability_bytes =
        layerx_programs::hex::decode(text(wire, "capability")).expect("actual capability bytes");
    let scope_bytes = layerx_programs::hex::decode(text(wire, "session_scope"))
        .expect("actual session scope bytes");
    let grant = SignedNativeLocalGrantV1 {
        capability: NativeTimedCapabilityV1::decode(&capability_bytes)
            .expect("actual native timed capability"),
        session: NativeSessionScopeV1::decode(&scope_bytes).expect("actual native session scope"),
        expires_at_ms: text(wire, "expires_at_ms")
            .parse()
            .expect("real grant expiry"),
        owner_public_key: id(text(wire, "owner_public_key")),
        signature: layerx_programs::hex::decode(text(wire, "signature"))
            .expect("real grant signature")
            .try_into()
            .expect("64-byte grant signature"),
    };
    assert_eq!(
        grant
            .capability
            .encode()
            .expect("native capability encoding"),
        capability_bytes
    );
    assert_eq!(
        grant.session.encode().expect("native scope encoding"),
        scope_bytes
    );
    assert_eq!(grant.owner_public_key, purpose.owner_public_key);
    assert_eq!(grant.session.tenant.as_str(), purpose.tenant.as_str());
    assert_eq!(
        grant.session.agent.as_bytes(),
        purpose.agent_did.as_str().as_bytes()
    );
    assert_eq!(
        grant.session.session_id.0,
        purpose.session_id.to_bytes().expect("signed session")
    );
    assert_eq!(grant.session.generation, purpose.generation);
    assert_eq!(
        grant.capability.record.id,
        purpose.capability_id.to_bytes().expect("signed capability")
    );
    assert!(grant
        .session
        .permitted_activities
        .contains(&purpose.activity));
    assert!(grant.capability.activities.contains(&purpose.activity));
    assert!(grant
        .capability
        .purpose_commitments
        .contains(&purpose.commitment));
    assert!(purpose.expires_at_ms <= grant.expires_at_ms);
    ed25519_dalek::VerifyingKey::from_bytes(&grant.owner_public_key)
        .expect("genuine grant owner")
        .verify_strict(
            &grant
                .signing_digest()
                .expect("actual native grant statement digest"),
            &ed25519_dalek::Signature::from_bytes(&grant.signature),
        )
        .expect("genuine issuer grant signature");
}
#[test]
fn real_native_send_profile_owner_hold_reopen_decisions_and_exact_resume() {
    let path = std::env::var("PAXEER_X_NATIVE_SEND_PROFILE_FIXTURE")
        .expect("genuine protected disposable native Send authority fixture required");
    let fixture = load(&path);
    assert_eq!(fixture["schema"], "paxeer-x.native-send-profile.v1");
    assert_eq!(fixture["isolated_real_owner"], true);
    let mut daemon = DisposableDaemon {
        pid: u32::try_from(
            fixture["disposable_daemon"]["pid"]
                .as_u64()
                .expect("actual daemon PID"),
        )
        .expect("PID bound"),
        child: None,
    };
    assert_ne!(node(&fixture).head().chain_sequence, 0);
    let generic = load(text(&fixture, "generic_prepare_file"));
    assert!(generic["request"].get("variant").is_none());
    let (status, refusal) = rpc(&fixture, &generic);
    assert_eq!(status, 403);
    assert_eq!(refusal["reason"], "policy.intent_binding_missing");
    let cases = fixture["cases"]
        .as_array()
        .expect("genuine independent Send owner cases");
    assert_eq!(cases.len(), 2);
    let owner = text(&fixture, "subject_owner");
    let principal = text(&fixture, "subject_principal");
    let mut ids = Vec::new();
    for (index, case) in cases.iter().enumerate() {
        assert_eq!(
            case["decision"],
            if index == 0 { "grant" } else { "reject" }
        );
        let request = load(text(case, "prepare_file"));
        assert_eq!(request["operation"], "prepare");
        assert_eq!(request["request"]["variant"], "native_send_v1");
        assert_eq!(request["request"]["activity"]["module"], "1");
        assert_eq!(request["request"]["activity"]["ordinal"], "5");
        let signed_purpose = genuine_signed_purpose(&request, case);
        genuine_grant(&request, &signed_purpose.purpose);
        let (status, response) = rpc(&fixture, &request);
        assert_eq!(status, 200);
        let result = &response["value"];
        assert_eq!(result["approval_required"], true);
        let preparation = id(text(result, "preparation_id"));
        assert_eq!(preparation, id(text(result, "approval_id")));
        assert_eq!(preparation, signed_purpose.purpose.preparation_id);
        assert_eq!(preparation, signed_purpose.purpose.canonical_digest);
        assert_eq!(
            signed_purpose.purpose.idempotency_key,
            id(text(&request["request"], "idempotency_key"))
        );
        assert!(!ids.contains(&preparation));
        ids.push(preparation);
        let canonical = layerx_programs::hex::decode(text(result, "canonical_bytes"))
            .expect("actual prepared Send bytes");
        assert_eq!(preparation, <[u8; 32]>::from(Sha256::digest(&canonical)));
        let (record, carrier_raw, held) = retained(&fixture, preparation);
        assert_eq!(record.state, LifecycleState::Prepared);
        assert!(record.activity_id.is_none());
        assert_eq!(held["state"], "Awaiting");
        assert_eq!(bytes(&held["canonical_bytes"]), canonical);
        assert_eq!(
            bytes(&held["actor"]),
            text(&request["request"], "actor").as_bytes()
        );
        assert_eq!(
            record.payload_hash,
            id(text(&request["request"], "payload_hash"))
        );
        let get = operation(53, Some(preparation));
        let first_get = private(&fixture, &get, owner, principal);
        let digest = approval(&first_get, preparation);
        assert_eq!(digest, <[u8; 32]>::from(Sha256::digest(&carrier_raw)));
        assert_eq!(
            record.extensions.get(&9).map(Vec::as_slice),
            Some(digest.as_slice())
        );
        assert_eq!(
            private(
                &fixture,
                &get,
                text(&fixture, "foreign_subject_owner"),
                principal
            ),
            b"LXHAGT01\x01"
        );
        assert_eq!(
            private(
                &fixture,
                &get,
                owner,
                text(&fixture, "foreign_subject_principal")
            ),
            b"LXHAGT01\x01"
        );
        let held_submit = load(text(case, "held_submit_file"));
        let resume = load(text(case, "original_resume_file"));
        assert_eq!(held_submit["request"]["variant"], "native_send_submit_v1");
        assert_eq!(resume["request"]["variant"], "native_send_submit_v1");
        assert_eq!(held_submit["operation"], "submit");
        assert_eq!(resume["operation"], "submit");
        assert!(
            held_submit["request"] == resume["request"],
            "exact original signed submit must resume"
        );
        assert!(
            held_submit["credential"] == resume["credential"],
            "original authenticated owner session must resume"
        );
        assert_eq!(
            text(&resume["request"], "preparation_ref"),
            text(result, "preparation_id")
        );
        assert_eq!(
            resume["request"]["signer_public_key"],
            request["request"]["purpose"]["owner_public_key"]
        );
        let signature: [u8; 64] =
            layerx_programs::hex::decode(text(&resume["request"], "signature"))
                .expect("genuine original Send signature")
                .try_into()
                .expect("signature bound");
        ed25519_dalek::VerifyingKey::from_bytes(&id(text(&resume["request"], "signer_public_key")))
            .expect("actual activity signer")
            .verify_strict(
                &id(text(result, "signing_preimage")),
                &ed25519_dalek::Signature::from_bytes(&signature),
            )
            .expect("original signature binds actual preparation");
        assert_eq!(rpc(&fixture, &held_submit).0, 403);
        assert_eq!(retained(&fixture, preparation).1, carrier_raw);
        if index == 0 {
            daemon.reopen(&fixture);
            assert_eq!(retained(&fixture, preparation).1, carrier_raw);
            assert_eq!(private(&fixture, &get, owner, principal), first_get);
        }
        let mut decision = operation(54, Some(preparation));
        decision.extend_from_slice(&digest);
        field(&mut decision, text(case, "decision_key").as_bytes());
        decision.push(u8::from(index == 0));
        decision.extend_from_slice(&node(&fixture).head().chain_sequence.to_be_bytes());
        let mut tampered = decision.clone();
        tampered[41] ^= 1;
        assert_eq!(
            private(&fixture, &tampered, owner, principal),
            b"LXHAGT01\x01"
        );
        assert_eq!(retained(&fixture, preparation).1, carrier_raw);
        let decided = private(&fixture, &decision, owner, principal);
        assert_eq!(approval(&decided, preparation), digest);
        assert_eq!(private(&fixture, &decision, owner, principal), decided);
        let mut conflicting = decision.clone();
        let grant_position = conflicting.len() - 9;
        conflicting[grant_position] ^= 1;
        assert_eq!(
            private(&fixture, &conflicting, owner, principal),
            b"LXHAGT01\x01"
        );
        let (after, after_raw, terminal) = retained(&fixture, preparation);
        unchanged(&held, &terminal);
        assert_eq!(
            terminal["state"],
            if index == 0 { "Granted" } else { "Rejected" }
        );
        assert_eq!(
            after.state,
            if index == 0 {
                LifecycleState::Prepared
            } else {
                LifecycleState::Failed
            }
        );
        assert_eq!(after.session_id, record.session_id);
        assert_eq!(
            record.session_id,
            signed_purpose
                .purpose
                .session_id
                .to_bytes()
                .expect("actual signed session")
        );
        assert_eq!(record.generation, signed_purpose.purpose.generation);
        assert_eq!(after.generation, record.generation);
        assert_eq!(after.payload_hash, record.payload_hash);
        assert_eq!(private(&fixture, &get, owner, principal), decided);
        if index == 0 {
            assert_eq!(
                id(text(&resume["request"], "approval_release_ref")),
                <[u8; 32]>::try_from(bytes(&terminal["terminal"]["release_ref"]))
                    .expect("real granted release")
            );
            daemon.reopen(&fixture);
            assert_eq!(retained(&fixture, preparation).1, after_raw);
            let (status, submitted) = rpc(&fixture, &resume);
            assert_eq!(status, 200);
            assert_eq!(rpc(&fixture, &resume), (status, submitted));
            let (queued, _, carrier) = retained(&fixture, preparation);
            assert_ne!(queued.state, LifecycleState::Failed);
            assert!(queued.activity_id.is_some());
            unchanged(&held, &carrier);
            assert_eq!(
                bytes(&carrier["queued_submission"]["activity_id"]),
                queued.activity_id.unwrap()
            );
            println!("native-send: exact-original-resume");
        } else {
            assert_eq!(rpc(&fixture, &resume).0, 403);
            assert_eq!(retained(&fixture, preparation).1, after_raw);
        }
        println!(
            "native-send: {}",
            if index == 0 { "grant" } else { "reject" }
        );
    }
    let negatives = fixture["refusal_cases"]
        .as_array()
        .expect("genuine independently scoped refusal inputs");
    let mut covered = std::collections::BTreeSet::new();
    for case in negatives {
        let name = text(case, "case");
        assert!(matches!(
            name,
            "cross-profile" | "replay" | "expiry" | "principal" | "session" | "canonical-digest"
        ));
        assert!(
            covered.insert(name.to_owned()),
            "refusal case must be unique"
        );
        let request = load(text(case, "request_file"));
        assert_eq!(request["operation"], "prepare");
        let original = load(text(case, "original_prepare_file"));
        assert_eq!(original["request"]["variant"], "native_send_v1");
        match name {
            "cross-profile" => {
                assert!(matches!(
                    request["request"]["variant"].as_str(),
                    Some("native_v1" | "native_effect_v1")
                ));
                assert!(
                    request["request"]["purpose"] == original["request"]["purpose"],
                    "cross-profile case must reuse the genuinely signed Send statement"
                );
            }
            "replay" => {
                assert!(
                    request["request"] == original["request"],
                    "replay must retain original signed Send"
                );
                assert!(
                    request["idempotency_key"] != original["idempotency_key"],
                    "purpose replay must cross an independent mutation admission"
                );
            }
            "expiry" => {
                let expired = genuine_signed_purpose(&request, case);
                let wall_ms = u64::try_from(
                    std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .expect("clock")
                        .as_millis(),
                )
                .expect("clock bound");
                assert!(
                    expired.purpose.expires_at_ms <= wall_ms,
                    "expiry input must be genuine owner-signed already-expired authority"
                );
            }
            "principal" | "session" => {
                assert!(
                    request["request"] == original["request"],
                    "foreign credential must keep genuine original Send"
                );
                let foreign = load(text(case, "foreign_credential_file"));
                assert!(
                    request["credential"] == foreign,
                    "actual foreign authenticated credential required"
                );
                assert!(
                    request["credential"] != original["credential"],
                    "credential substitution must be foreign"
                );
            }
            "canonical-digest" => {
                let mut expected = original["request"].clone();
                let target = &mut expected["purpose"]["purpose"]["canonical_digest"];
                let value = target.as_str().expect("original canonical digest");
                let mut digest = id(value);
                digest[0] ^= 1;
                *target = Value::String(layerx_programs::hex::encode(&digest));
                assert!(
                    request["request"] == expected,
                    "digest refusal must change only genuinely signed canonical digest"
                );
            }
            _ => unreachable!(),
        }
        let (status, response) = rpc(&fixture, &request);
        assert_eq!(
            u64::from(status),
            case["expected_status"]
                .as_u64()
                .expect("exact refusal status")
        );
        assert!(matches!(status, 400 | 403 | 409));
        assert_eq!(response["reason"], case["expected_reason"]);
        assert!(response.get("value").is_none());
        if name == "cross-profile" {
            for profile in ["native_v1", "native_effect_v1"] {
                let mut crossed = request.clone();
                crossed["request"]["variant"] = Value::String(profile.to_owned());
                let (status, refused) = rpc(&fixture, &crossed);
                assert!(matches!(status, 400 | 403 | 409));
                assert!(refused.get("value").is_none());
                assert!(refused["reason"].is_string());
            }
        }
        println!("native-send: refusal-{name}");
    }
    assert_eq!(
        covered,
        [
            "cross-profile",
            "replay",
            "expiry",
            "principal",
            "session",
            "canonical-digest"
        ]
        .into_iter()
        .map(str::to_owned)
        .collect()
    );
    println!("native-send: distinct-domain-held-reopen");
}
