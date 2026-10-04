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
    assert_eq!(request["operation"], "prepare");
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
    stream.write_all(&body).expect("TLS real prepare");
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
struct Decoder<'a>(&'a [u8]);
impl<'a> Decoder<'a> {
    fn take(&mut self, n: usize) -> &'a [u8] {
        let (value, rest) = self
            .0
            .split_at_checked(n)
            .expect("bounded real Human facts");
        self.0 = rest;
        value
    }
    fn u8(&mut self) -> u8 {
        self.take(1)[0]
    }
    fn u16(&mut self) -> u16 {
        u16::from_be_bytes(self.take(2).try_into().expect("u16"))
    }
    fn bytes(&mut self) -> &'a [u8] {
        let n = u32::from_be_bytes(self.take(4).try_into().expect("field length"));
        self.take(usize::try_from(n).expect("field bound"))
    }
    fn facts(&mut self, expected_id: [u8; 32], fixture: &Value, expected_state: u8) -> [u8; 32] {
        assert_eq!(self.u16(), 3);
        assert_eq!(self.take(32), expected_id);
        let digest = self.take(32).try_into().expect("held digest");
        assert_eq!(self.bytes(), text(fixture, "subject_owner").as_bytes());
        assert_eq!(self.bytes(), text(fixture, "actor").as_bytes());
        assert_eq!(self.u16(), 1);
        assert_eq!(self.u16(), 5);
        assert_eq!(self.u8(), expected_state);
        assert_eq!(self.u8(), 1);
        self.take(32);
        assert_eq!(self.take(32), id(text(fixture, "asset_id")));
        self.take(48);
        assert_eq!(self.u16(), 2);
        assert_eq!(self.u8(), 0);
        assert_eq!(self.take(32), id(text(fixture, "payer_account")));
        assert_eq!(self.u8(), 1);
        assert_eq!(self.take(32), id(text(fixture, "recipient_account")));
        assert_eq!(self.u16(), 1);
        assert_eq!(self.u8(), 0);
        assert_ne!(self.take(16), &[0; 16]);
        let release = self.u8();
        assert_eq!(release, u8::from(expected_state == 1));
        if release == 1 {
            self.take(32);
        }
        assert_eq!(self.u8(), 0);
        digest
    }
}
fn decode(response: &[u8]) -> Decoder<'_> {
    let mut decoded = Decoder(response);
    assert_eq!(decoded.take(9), b"LXHAGT01\x00");
    decoded
}
fn durable(fixture: &Value, approval: [u8; 32]) -> (DurablePreparation, Vec<u8>) {
    let tenant = TenantId::new(text(fixture, "agent_tenant")).expect("actual tenant");
    let store = Store::open(text(fixture, "retained_store")).expect("genuine durable store reopen");
    let key = DurablePreparation::store_key(&tenant, approval).expect("actual durable key");
    let record = DurablePreparation::decode(
        tenant.clone(),
        store.get(&key).expect("real durable preparation").bytes(),
    )
    .expect("durable encoding");
    assert_eq!(record.preparation_id, approval);
    assert!(record.activity_id.is_none());
    let key = TenantKey::new(
        tenant,
        ObjectKind::PreparedActivity,
        [
            b"native-effect-approval-carrier-v1:".as_slice(),
            approval.as_slice(),
        ]
        .concat(),
    )
    .expect("actual carrier key");
    (
        record,
        store
            .get(&key)
            .expect("real atomic carrier")
            .bytes()
            .to_vec(),
    )
}

#[test]
fn real_native_effect_prepare_human_owner_decision_and_reopen() {
    let path = std::env::var("PAXEER_X_HUMAN_APPROVAL_ADMISSION_FIXTURE")
        .expect("genuine protected disposable mTLS/LNI owner fixture required");
    let fixture: Value =
        serde_json::from_slice(&protected(&path, 65_536)).expect("real fixture JSON");
    assert_eq!(fixture["schema"], "paxeer-x.human-approval-admission.v1");
    assert_eq!(fixture["isolated_real_owner"], true);
    assert_ne!(node(&fixture).head().chain_sequence, 0);
    let generic: Value = serde_json::from_slice(&protected(
        text(&fixture, "generic_prepare_file"),
        1_048_576,
    ))
    .expect("actual generic prepare fixture");
    assert!(generic["request"].get("variant").is_none());
    let (status, refusal) = rpc(&fixture, &generic);
    assert_eq!(status, 403);
    assert_eq!(refusal["reason"], "policy.intent_binding_missing");
    assert_eq!(refusal["class"], "PolicyRefusal");
    let cases = fixture["cases"]
        .as_array()
        .expect("genuine independent grant/reject cases");
    assert_eq!(cases.len(), 2);
    let mut prepared_ids = Vec::new();
    for (index, case) in cases.iter().enumerate() {
        let request: Value =
            serde_json::from_slice(&protected(text(case, "prepare_file"), 1_048_576))
                .expect("actual signed native prepare");
        assert!(request["credential"] == generic["credential"]);
        assert_eq!(request["request"]["variant"], "native_effect_v1");
        assert!(request["request"]["purpose"].is_object());
        let (status, response) = rpc(&fixture, &request);
        assert_eq!(status, 200);
        let value = &response["value"];
        assert_eq!(value["approval_required"], true);
        let approval = id(text(value, "approval_id"));
        assert_eq!(approval, id(text(value, "preparation_id")));
        assert_eq!(
            approval,
            <[u8; 32]>::from(Sha256::digest(
                layerx_programs::hex::decode(text(value, "canonical_bytes"))
                    .expect("actual canonical activity")
            ))
        );
        assert!(!prepared_ids.contains(&approval));
        prepared_ids.push(approval);
        let (record, carrier) = durable(&fixture, approval);
        assert_eq!(record.state, LifecycleState::Prepared);
        assert!(record
            .extensions
            .get(&6)
            .is_some_and(|bytes| !bytes.is_empty()));
        let get = operation(53, Some(approval));
        let owner = text(&fixture, "subject_owner");
        let principal = text(&fixture, "subject_principal");
        let response = private(&fixture, &get, owner, principal);
        let mut decoded = decode(&response);
        let digest = decoded.facts(approval, &fixture, 0);
        assert!(decoded.0.is_empty());
        let foreign = text(&fixture, "foreign_subject_owner");
        assert_ne!(foreign, owner);
        assert_ne!(
            text(&fixture, "foreign_subject_account"),
            text(&fixture, "subject_account")
        );
        assert_eq!(private(&fixture, &get, foreign, principal), b"LXHAGT01\x01");
        let foreign_principal = text(&fixture, "foreign_subject_principal");
        assert_ne!(foreign_principal, principal);
        assert_eq!(
            private(&fixture, &get, owner, foreign_principal),
            b"LXHAGT01\x01"
        );
        let mut list = operation(52, None);
        list.extend_from_slice(&[1]);
        list.extend_from_slice(&approval);
        list.push(100);
        assert_eq!(
            private(&fixture, &list, foreign, principal),
            b"LXHAGT01\x01"
        );
        let mut list = operation(52, None);
        list.extend_from_slice(&[0, 100]);
        let rows = private(&fixture, &list, owner, principal);
        let mut rows = decode(&rows);
        assert!(rows.u8() >= 1);
        let sequence = node(&fixture).head().chain_sequence;
        let mut decision = operation(54, Some(approval));
        decision.extend_from_slice(&digest);
        field(&mut decision, text(case, "decision_key").as_bytes());
        decision.push(u8::from(index == 0));
        decision.extend_from_slice(&sequence.to_be_bytes());
        assert_eq!(
            private(&fixture, &decision, foreign, principal),
            b"LXHAGT01\x01"
        );
        let mut bad_digest = decision.clone();
        bad_digest[41] ^= 1;
        assert_eq!(
            private(&fixture, &bad_digest, owner, principal),
            b"LXHAGT01\x01"
        );
        assert_eq!(durable(&fixture, approval).1, carrier);
        let first = private(&fixture, &decision, owner, principal);
        let mut decoded = decode(&first);
        assert_eq!(
            decoded.facts(approval, &fixture, if index == 0 { 1 } else { 2 }),
            digest
        );
        assert!(decoded.0.is_empty());
        assert_eq!(private(&fixture, &decision, owner, principal), first);
        let mut conflicting = decision.clone();
        let grant_position = conflicting.len() - 9;
        conflicting[grant_position] ^= 1;
        assert_eq!(
            private(&fixture, &conflicting, owner, principal),
            b"LXHAGT01\x01"
        );
        let (reopened, persisted) = durable(&fixture, approval);
        assert_eq!(
            reopened.state,
            if index == 0 {
                LifecycleState::Prepared
            } else {
                LifecycleState::Failed
            }
        );
        assert_ne!(persisted, carrier);
        assert_eq!(private(&fixture, &get, owner, principal), first);
        assert_eq!(durable(&fixture, approval).1, persisted);
    }
}
