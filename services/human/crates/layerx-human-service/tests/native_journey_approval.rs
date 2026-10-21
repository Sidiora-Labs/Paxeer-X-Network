use std::fmt::Debug;
use std::fs;
use std::future::Future;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::pin::pin;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::{Duration, Instant};

use layerx_agent_api::error::RequestId;
use layerx_agent_api::idempotency::{BodyDigest, IdempotentMutation, Key};
use layerx_agent_api::identity::{
    AgentDid, AuthorityRef, NativeSendPurposeV1, SessionId, SignedNativeSendPurposeV1,
};
use layerx_agent_api::prepare::{IdempotencyRef, PayloadBytes, PrepareRequest, TimestampBound};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{Limits, MutualTlsConfig};
use layerx_client::Client;
use layerx_human_service::audit::{AuditChain, AuditEvent, Decision};
use layerx_human_service::custody::{
    CustodySigner, KeyId, Keystore, Operation, RemoteKmsProvider, SigningLimits,
};
use layerx_human_service::journeys::{
    AgentBoundary, JourneyEngine, JourneyKind, JourneyLeg, JourneyPhase, JourneyPlan, JourneyState,
};
use layerx_human_service::notify::JourneyId;
use layerx_human_service::server::agent_runtime::{AgentRuntime, NativeEffectApprovalFactState};
use layerx_human_service::store::{
    PrincipalId, PrincipalScope, PrincipalStore, RetentionPeriod, RetentionPolicy, RowKey, Table,
    TenancyDigest,
};
use layerx_human_service::trace::TraceId;
use layerx_intents::{Intent, IntentKind, LxpSend};
use layerx_sdk::{Call, Client as AgentClient};
use layerx_types::account::AccountId;
use layerx_types::amount::Amount;
use layerx_types::ids::{AssetId, Did, IdempotencyKey};
use layerx_types::intent::{
    AuthorizationSignature, ContextHash, NetworkId, ProtocolVersion, PublicKey, SendAuthorization,
    SendAuthorizationKind, Sequence, TimestampSeconds,
};
use native_tls::{Certificate, TlsConnector};
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
use rustls::RootCertStore;
use serde_json::Value;
use sha2::{Digest, Sha256};

fn checked<T, E: Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|_| panic!("genuine native journey boundary refused"))
}
fn text<'a>(value: &'a Value, key: &str) -> &'a str {
    value[key]
        .as_str()
        .expect("genuine protected fixture field")
}
fn number(value: &Value, key: &str) -> u64 {
    value[key].as_u64().expect("genuine bounded protocol value")
}
fn protected(path: &str, bound: u64) -> Vec<u8> {
    let info = checked(fs::symlink_metadata(path));
    assert!(
        info.is_file()
            && info.len() > 0
            && info.len() <= bound
            && info.permissions().mode() & 0o077 == 0
    );
    checked(fs::read(path))
}
fn decode_hex(value: &str) -> Vec<u8> {
    assert!(
        value.len() % 2 == 0
            && value
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    );
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| checked(u8::from_str_radix(checked(std::str::from_utf8(pair)), 16)))
        .collect()
}
fn id(value: &str) -> [u8; 32] {
    let id = checked(decode_hex(value).try_into());
    assert_ne!(id, [0; 32]);
    id
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
fn clock(fixture: &Value) -> (u64, u64) {
    let mut node = checked(Client::connect(ClientConfig {
        endpoint: Path::new(text(fixture, "lni_socket")).to_path_buf(),
        handshake: HandshakeConfig {
            built_interface_version: Version::V1_8,
            expected_protocol_version: 3,
            expected_network_id: checked(number(fixture, "network_id").try_into()),
        },
        limits: limits(),
        reconnect: ReconnectPolicy {
            maximum_attempts: 1,
            base_delay: Duration::from_millis(1),
            maximum_delay: Duration::from_millis(1),
            jitter_percent: 0,
        },
    }));
    let state =
        checked(node.preparation_state(&checked(Did::new(text(fixture, "owner").as_bytes())), 1));
    assert_ne!(state.observed_state_root, [0; 32]);
    (state.protocol_timestamp, state.observed_head_sequence)
}
fn store(fixture: &Value) -> PrincipalStore {
    let retention = RetentionPeriod::new(number(fixture, "retention_seconds"));
    checked(PrincipalStore::open(
        text(fixture, "human_store"),
        RetentionPolicy {
            journeys: retention,
            notifications: retention,
            audit: retention,
            telemetry: retention,
            cache: retention,
        },
        TenancyDigest::new(id(text(fixture, "tenancy_digest"))),
    ))
}
fn runtime(fixture: &Value, principal: &str, owner: &str, account: &str) -> AgentRuntime {
    checked(
        checked(AgentRuntime::connect(
            text(fixture, "agent_socket"),
            limits(),
        ))
        .for_subject(
            &checked(PrincipalId::new(principal)),
            &checked(Did::new(owner.as_bytes())),
            &checked(AccountId::parse(account)),
            id(text(fixture, "asset_id")),
        ),
    )
}
fn custody(fixture: &Value, registry: &layerx_types::payload::ModuleRegistry) -> CustodySigner {
    let mut roots = RootCertStore::empty();
    checked(roots.add(CertificateDer::from(protected(
        text(fixture, "kms_ca_der"),
        65_536,
    ))));
    let tls = checked(MutualTlsConfig::new(
        roots,
        vec![CertificateDer::from(protected(
            text(fixture, "kms_client_cert_der"),
            65_536,
        ))],
        PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(protected(
            text(fixture, "kms_client_key_der"),
            65_536,
        ))),
    ));
    let provider = checked(RemoteKmsProvider::new(
        text(fixture, "kms_provider"),
        checked(text(fixture, "kms_endpoint").parse()),
        text(fixture, "kms_server_name"),
        tls,
        limits(),
    ));
    let keys = checked(Keystore::open_production(
        text(fixture, "custody_store"),
        checked(number(fixture, "network_id").try_into()),
        provider,
    ));
    CustodySigner::new(
        keys,
        store(fixture),
        registry.clone(),
        checked(SigningLimits::new(
            checked(number(fixture, "signing_maximum").try_into()),
            number(fixture, "signing_window_seconds"),
        )),
    )
}
struct Ready;
impl Wake for Ready {
    fn wake(self: Arc<Self>) {}
}
fn ready<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let waker = Waker::from(Arc::new(Ready));
    let mut context = Context::from_waker(&waker);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(value) => value,
        Poll::Pending => panic!("bounded synchronous native boundary did not complete"),
    }
}
fn plan(fixture: &Value, case: &Value) -> JourneyPlan {
    let data: Value = checked(serde_json::from_slice(&protected(
        text(case, "intent_file"),
        65_536,
    )));
    let authorization = SendAuthorization::new(
        SendAuthorizationKind::Owner,
        PublicKey::new(id(text(&data, "authorization_public_key"))),
        AuthorizationSignature::new(checked(
            decode_hex(text(&data, "authorization_signature")).try_into(),
        )),
    );
    let send = checked(LxpSend::new(
        checked(AccountId::parse(text(&data, "from"))),
        checked(AccountId::parse(text(&data, "to"))),
        AssetId::new(id(text(fixture, "asset_id"))),
        Amount::from_u128(checked(text(&data, "amount").parse())),
        Sequence::from_u64(number(&data, "account_sequence")),
        IdempotencyKey::new(id(text(&data, "action_key"))),
        TimestampSeconds::from_u64(number(&data, "expires_at")),
        ContextHash::new(id(text(&data, "context_hash"))),
        authorization,
        checked(NetworkId::new(checked(
            number(fixture, "network_id").try_into(),
        ))),
        checked(ProtocolVersion::new(3)),
    ));
    let leg = checked(JourneyLeg::new(
        Intent::v1(IntentKind::LxpSend(send)),
        id(text(&data, "action_key")),
        checked(AgentDid::new(text(fixture, "owner"))),
        checked(AuthorityRef::new(text(&data, "authority"))),
        number(&data, "account_sequence"),
        number(&data, "not_before"),
        number(&data, "not_after"),
        checked(text(&data, "fee_limit").parse()),
    ));
    checked(JourneyPlan::new(
        checked(JourneyId::new(text(case, "journey_id"))),
        JourneyKind::Move,
        id(text(case, "request_key")),
        checked(KeyId::new(text(fixture, "custody_key"))),
        Operation::ProtocolMutation,
        vec![leg],
    ))
}
fn row(case: &Value) -> RowKey {
    checked(RowKey::new(format!(
        "journey-{}",
        text(case, "request_key")
    )))
}
fn journal(scope: &PrincipalScope<'_>, case: &Value) -> Value {
    checked(serde_json::from_slice(
        scope
            .get(Table::Journeys, &row(case))
            .expect("actual retained native journey")
            .bytes(),
    ))
}
fn bytes(value: &Value) -> Vec<u8> {
    checked(serde_json::from_value(value.clone()))
}
fn original_call(
    contract: &AgentClient,
    journal: &Value,
) -> Call<IdempotentMutation<PrepareRequest>> {
    let leg = &journal["legs"][0];
    let prep = &leg["preparation"];
    let action: [u8; 32] = checked(bytes(&leg["action_key"]).try_into());
    let request = PrepareRequest {
        protocol_activity_type: checked(number(leg, "activity_type").try_into()),
        actor: checked(AgentDid::new(text(prep, "actor"))),
        authority: checked(AuthorityRef::new(text(prep, "authority"))),
        account_sequence: layerx_agent_api::Sequence(number(prep, "account_sequence")),
        timestamp_bound: checked(
            TimestampBound {
                not_before: layerx_agent_api::TimestampSeconds(number(prep, "not_before")),
                not_after: layerx_agent_api::TimestampSeconds(number(prep, "not_after")),
            }
            .validate(),
        ),
        idempotency_key: checked(IdempotencyRef::new(
            action
                .iter()
                .map(|b| format!("{b:02x}"))
                .collect::<String>(),
        )),
        fee_limit: layerx_agent_api::Amount(checked(serde_json::from_value(
            prep["fee_limit"].clone(),
        ))),
        payload: checked(PayloadBytes::new(bytes(&leg["payload"]))),
        payload_hash: checked(bytes(&leg["payload_hash"]).try_into()),
    };
    let mut digest = Sha256::new();
    digest.update(b"layerx-human-journey-prepare/v1");
    digest.update(request.protocol_activity_type.to_be_bytes());
    for value in [request.actor.as_str(), request.authority.as_str()] {
        digest.update(checked(u32::try_from(value.len())).to_be_bytes());
        digest.update(value.as_bytes());
    }
    digest.update(request.account_sequence.get().to_be_bytes());
    digest.update(request.timestamp_bound.not_before.get().to_be_bytes());
    digest.update(request.timestamp_bound.not_after.get().to_be_bytes());
    let key = request.idempotency_key.as_str();
    digest.update(checked(u32::try_from(key.len())).to_be_bytes());
    digest.update(key.as_bytes());
    digest.update(request.fee_limit.get().to_be_bytes());
    digest.update(request.payload_hash);
    digest.update(Sha256::digest(request.payload.as_bytes()));
    contract.prepare(IdempotentMutation {
        request_id: RequestId(u64::from_be_bytes(checked(action[..8].try_into()))),
        key: checked(Key::new(action)),
        body_digest: BodyDigest(digest.finalize().into()),
        operation: request,
    })
}
fn signed_purpose(journal: &Value) -> SignedNativeSendPurposeV1 {
    let native = &journal["legs"][0]["native"];
    SignedNativeSendPurposeV1 {
        purpose: checked(NativeSendPurposeV1::from_canonical_bytes(&bytes(
            &native["purpose"],
        ))),
        owner_public_key: checked(bytes(&native["owner_public_key"]).try_into()),
        signature: checked(bytes(&native["purpose_signature"]).try_into()),
    }
}
fn no_money(fixture: &Value, journal: &Value) {
    let leg = &journal["legs"][0];
    assert!(
        leg["submission_ref"].is_null()
            && leg["activity_id"].is_null()
            && leg["receipt"].is_null()
            && leg["receipt_digest"].is_null()
    );
    let store = checked(layerx_agentd::store::Store::open(text(
        fixture,
        "agent_store",
    )));
    let tenant = checked(layerx_agentd::store::TenantId::new(text(
        fixture,
        "agent_tenant",
    )));
    let key = checked(layerx_agentd::store::TenantKey::new(
        tenant,
        layerx_agentd::store::ObjectKind::Outbox,
        bytes(&leg["action_key"]),
    ));
    assert!(store.get(&key).is_none());
}
fn activity_signatures(scope: &PrincipalScope<'_>, journal: &Value) -> usize {
    let digest: [u8; 32] =
        checked(bytes(&journal["legs"][0]["native"]["disclosure_digest"]).try_into());
    let chain = checked(AuditChain::open(scope));
    checked(chain.entries(scope)).iter().filter(|entry| matches!(entry.event(), AuditEvent::SigningDecision { disclosure_digest, outcome: Decision::Granted, .. } if *disclosure_digest == digest)).count()
}
fn public_request(fixture: &Value, path: &str, request: &Value) -> (u16, Value) {
    assert!(matches!(
        path,
        "/v1/native/send/access/begin" | "/v1/native/send/access/confirm"
    ));
    let host = text(fixture, "human_tls_host");
    assert!(!host.is_empty() && !host.contains(['\r', '\n', '/', ':']));
    let connector = checked(
        TlsConnector::builder()
            .add_root_certificate(checked(Certificate::from_der(&protected(
                text(fixture, "human_tls_ca_der"),
                65_536,
            ))))
            .build(),
    );
    let tcp = checked(TcpStream::connect_timeout(
        &checked(text(fixture, "human_tls_address").parse()),
        Duration::from_secs(8),
    ));
    checked(tcp.set_read_timeout(Some(Duration::from_secs(8))));
    checked(tcp.set_write_timeout(Some(Duration::from_secs(8))));
    let mut tls = checked(connector.connect(host, tcp));
    let body = checked(serde_json::to_vec(&request["body"]));
    assert!(body.len() <= 1_048_576);
    let mut header = format!("POST {path} HTTP/1.1\r\nHost: {host}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n", body.len());
    let headers = request["headers"]
        .as_object()
        .expect("protected genuine browser session headers");
    assert!(headers.keys().any(|key| key.eq_ignore_ascii_case("cookie")));
    for (name, value) in headers {
        assert!([
            "authorization",
            "cookie",
            "origin",
            "idempotency-key",
            "x-layerx-csrf",
            "x-layerx-trace"
        ]
        .iter()
        .any(|allowed| name.eq_ignore_ascii_case(allowed)));
        let value = value.as_str().expect("protected session header value");
        assert!(!value.contains(['\r', '\n']));
        header.push_str(name);
        header.push_str(": ");
        header.push_str(value);
        header.push_str("\r\n");
    }
    header.push_str("\r\n");
    checked(tls.write_all(header.as_bytes()));
    checked(tls.write_all(&body));
    let mut response = Vec::new();
    checked(tls.take(1_212_417).read_to_end(&mut response));
    assert!(response.len() <= 1_212_416);
    let end = response
        .windows(4)
        .position(|bytes| bytes == b"\r\n\r\n")
        .expect("genuine HTTP framing")
        + 4;
    let head = checked(std::str::from_utf8(&response[..end]));
    let status = checked(
        head.lines()
            .next()
            .expect("HTTP status")
            .split(' ')
            .nth(1)
            .expect("HTTP numeric status")
            .parse(),
    );
    let length: usize = head
        .lines()
        .find_map(|line| {
            line.split_once(':')
                .filter(|(name, _)| name.eq_ignore_ascii_case("content-length"))
                .map(|(_, value)| checked(value.trim().parse()))
        })
        .expect("genuine bounded HTTP body length");
    assert_eq!(length, response.len() - end);
    (status, checked(serde_json::from_slice(&response[end..])))
}
fn refusal_key(request: &mut Value, key: &str) {
    id(key);
    let headers = request["headers"]
        .as_object_mut()
        .expect("genuine browser header object");
    let previous = headers
        .keys()
        .find(|name| name.eq_ignore_ascii_case("idempotency-key"))
        .cloned()
        .expect("genuine mutation idempotency header");
    assert!(headers[&previous]
        .as_str()
        .is_some_and(|original| original != key));
    headers.insert(previous, Value::String(key.to_owned()));
}
fn public_access(fixture: &Value, case: &Value) {
    let begin: Value = checked(serde_json::from_slice(&protected(
        text(case, "access_begin_request_file"),
        65_536,
    )));
    let confirm: Value = checked(serde_json::from_slice(&protected(
        text(case, "access_confirm_request_file"),
        65_536,
    )));
    assert_eq!(begin["body"]["access_id"], case["capability_id"]);
    let mut immutable = confirm["body"].clone();
    assert!(immutable
        .as_object_mut()
        .expect("genuine full consent request")
        .remove("step_up")
        .is_some());
    assert!(immutable == begin["body"]);
    let (status, disclosure) = public_request(fixture, "/v1/native/send/access/begin", &begin);
    assert_eq!(status, 200);
    assert_eq!(disclosure["ok"], true);
    assert_eq!(disclosure["result"]["access_id"], case["capability_id"]);
    assert!(disclosure["result"]["confirms"]
        .as_str()
        .is_some_and(|digest| digest.starts_with("opd_")));
    let (status, repeated) = public_request(fixture, "/v1/native/send/access/begin", &begin);
    assert_eq!(status, 200);
    assert_eq!(repeated["result"], disclosure["result"]);
    let mut changed = begin.clone();
    let maximum: u128 = checked(text(&begin["body"], "maximum_amount").parse());
    changed["body"]["maximum_amount"] = Value::String(
        maximum
            .checked_add(1)
            .expect("genuine fixture amount permits bounded refusal mutation")
            .to_string(),
    );
    refusal_key(&mut changed, text(case, "begin_refusal_key"));
    let (status, refused) = public_request(fixture, "/v1/native/send/access/begin", &changed);
    assert_eq!(status, 403);
    assert_eq!(refused["ok"], false);
    assert_eq!(refused["error"]["code"], "forbidden");
    let mut changed = confirm.clone();
    assert_ne!(
        changed["body"]["commitment"].as_str(),
        Some(text(fixture, "foreign_commitment"))
    );
    id(text(fixture, "foreign_commitment"));
    changed["body"]["commitment"] = Value::String(text(fixture, "foreign_commitment").to_owned());
    refusal_key(&mut changed, text(case, "confirm_refusal_key"));
    let (status, refused) = public_request(fixture, "/v1/native/send/access/confirm", &changed);
    assert_eq!(status, 403);
    assert_eq!(refused["ok"], false);
    assert_eq!(refused["error"]["code"], "forbidden");
    let mut retained = store(fixture);
    let scope = checked(retained.principal(&checked(PrincipalId::new(text(fixture, "principal")))));
    let disclosure_key = checked(RowKey::new(format!(
        "native-send-public-disclosure-{}",
        text(case, "capability_id")
    )));
    let stored: Value = checked(serde_json::from_slice(
        scope
            .get(Table::Journeys, &disclosure_key)
            .expect("actual immutable public consent disclosure")
            .bytes(),
    ));
    assert!(stored["body"] == begin["body"]);
    assert_eq!(stored["owner"], fixture["owner"]);
    assert!(stored["issued_at_ms"].as_u64().is_some_and(|time| time > 0));
    drop(scope);
    drop(retained);
    let (status, accepted) = public_request(fixture, "/v1/native/send/access/confirm", &confirm);
    assert_eq!(status, 200);
    assert_eq!(accepted["ok"], true);
    assert_eq!(
        accepted["result"]["native_access_id"],
        case["capability_id"]
    );
}

#[test]
fn genuine_native_journey_holds_reopens_and_resumes_original_authority() {
    let path = std::env::var("PAXEER_X_NATIVE_JOURNEY_APPROVAL_FIXTURE")
        .expect("protected genuine native Journey authority fixture required");
    let fixture: Value = checked(serde_json::from_slice(&protected(&path, 65_536)));
    assert_eq!(fixture["schema"], "paxeer-x.native-journey-approval.v2");
    assert_eq!(fixture["isolated_real_owner"], true);
    let principal = checked(PrincipalId::new(text(&fixture, "principal")));
    let mut agent = runtime(
        &fixture,
        principal.as_str(),
        text(&fixture, "owner"),
        text(&fixture, "account"),
    );
    let registry = agent.registry().clone();
    let contract = checked(AgentClient::daemon(
        text(&fixture, "agent_socket"),
        layerx_agent_api::agent_api_schema_v1().version,
    ));
    let trace = checked(TraceId::parse(text(&fixture, "trace_id")));
    let cases = fixture["cases"]
        .as_array()
        .expect("grant rejection expiry cases");
    assert_eq!(cases.len(), 3);
    for (index, case) in cases.iter().enumerate() {
        public_access(&fixture, case);
        let plan = plan(&fixture, case);
        let mut retained = store(&fixture);
        let mut scope = checked(retained.principal(&principal));
        let intent: Value = checked(serde_json::from_slice(&protected(
            text(case, "intent_file"),
            65_536,
        )));
        let context = checked(layerx_human_service::server::native_send::load_access(
            &scope,
            id(text(case, "capability_id")),
            clock(&fixture).0,
        ));
        checked(
            agent.bind_native_journey_context(
                context,
                number(&intent, "not_after")
                    .checked_mul(1000)
                    .expect("protocol expiry bound"),
            ),
        );
        let mut engine = checked(JourneyEngine::start_native(
            &mut scope,
            &plan,
            &registry,
            clock(&fixture).0,
        ));
        let signer = custody(&fixture, &registry);
        for _ in 0..3 {
            if checked(engine.status()).phases() == [JourneyPhase::AwaitingApproval] {
                break;
            }
            checked(ready(engine.advance(
                &mut scope,
                &contract,
                &mut agent,
                &signer,
                &registry,
                &trace,
                clock(&fixture).0,
            )));
        }
        assert_eq!(
            checked(engine.status()).phases(),
            [JourneyPhase::AwaitingApproval]
        );
        let original = journal(&scope, case);
        assert!(original["legs"][0]["signed"].is_null());
        no_money(&fixture, &original);
        assert_eq!(activity_signatures(&scope, &original), 0);
        let (approval, digest) = engine
            .native_approval_binding()
            .expect("actual held native approval");
        let daemon = checked(layerx_agentd::store::Store::open(text(
            &fixture,
            "agent_store",
        )));
        let tenant = checked(layerx_agentd::store::TenantId::new(text(
            &fixture,
            "agent_tenant",
        )));
        let durable_key = checked(layerx_agentd::prepare::DurablePreparation::store_key(
            &tenant, approval,
        ));
        let durable = checked(layerx_agentd::prepare::DurablePreparation::decode(
            tenant,
            daemon
                .get(&durable_key)
                .expect("actual durable held approval")
                .bytes(),
        ));
        assert_eq!(
            durable.state,
            layerx_agentd::prepare::LifecycleState::Prepared
        );
        assert!(durable
            .extensions
            .get(&6)
            .is_some_and(|bytes| !bytes.is_empty()));
        let material = checked(agent.native_effect_approval_material(approval, digest));
        assert_eq!(
            material.canonical_unsigned_bytes,
            bytes(&original["legs"][0]["native"]["unsigned_canonical_bytes"])
        );
        assert_eq!(
            <[u8; 32]>::from(Sha256::digest(&material.canonical_unsigned_bytes)),
            approval
        );
        let call = original_call(&contract, &original);
        let purpose = signed_purpose(&original);
        assert_eq!(purpose.purpose.protocol_version, 3);
        assert_eq!(
            u64::from(purpose.purpose.network_id),
            number(&fixture, "network_id")
        );
        assert_eq!(purpose.purpose.activity.module, 1);
        assert_eq!(purpose.purpose.activity.ordinal, 5);
        assert_eq!(purpose.purpose.owner_did.as_str(), text(&fixture, "owner"));
        assert_eq!(purpose.purpose.owner_public_key, purpose.owner_public_key);
        let original_action: [u8; 32] =
            checked(bytes(&original["legs"][0]["action_key"]).try_into());
        assert_eq!(
            purpose.purpose.economic_action,
            id(text(case, "capability_id"))
        );
        assert_eq!(purpose.purpose.idempotency_key, original_action);
        assert_ne!(
            purpose.purpose.economic_action,
            purpose.purpose.idempotency_key
        );
        let mut wrong_session = purpose.clone();
        wrong_session.purpose.session_id =
            checked(SessionId::new(text(&fixture, "foreign_session_id")));
        assert_ne!(wrong_session.purpose.session_id, purpose.purpose.session_id);
        assert!(agent
            .native_journey_resume(&call, &wrong_session, approval, digest)
            .is_err());
        let mut wrong_context = purpose.clone();
        wrong_context.purpose.commitment[0] ^= 1;
        assert!(agent
            .native_journey_resume(&call, &wrong_context, approval, digest)
            .is_err());
        let mut wrong_digest = digest;
        wrong_digest[0] ^= 1;
        assert!(agent
            .native_effect_approval_decide(
                approval,
                wrong_digest,
                text(case, "decision_key"),
                true,
                clock(&fixture).1
            )
            .is_err());
        let mut foreign = runtime(
            &fixture,
            text(&fixture, "foreign_principal"),
            text(&fixture, "foreign_owner"),
            text(&fixture, "foreign_account"),
        );
        assert!(foreign.native_effect_approval_get_facts(approval).is_err());
        assert!(foreign
            .native_journey_resume(&call, &purpose, approval, digest)
            .is_err());
        assert_eq!(journal(&scope, case), original);
        drop(scope);
        drop(retained);
        drop(signer);
        let mut retained = store(&fixture);
        let mut scope = checked(retained.principal(&principal));
        engine = checked(JourneyEngine::load(
            &scope,
            &checked(JourneyId::new(text(case, "journey_id"))),
        ))
        .expect("genuine native reopen");
        assert_eq!(journal(&scope, case), original);
        let context = checked(layerx_human_service::server::native_send::load_access(
            &scope,
            id(text(case, "capability_id")),
            clock(&fixture).0,
        ));
        checked(agent.bind_native_journey_context(context, purpose.purpose.expires_at_ms));
        assert_eq!(
            checked(JourneyEngine::start_native(
                &mut scope,
                &plan,
                &registry,
                clock(&fixture).0
            ))
            .native_approval_binding(),
            Some((approval, digest))
        );
        let signer = custody(&fixture, &registry);
        if index == 2 {
            let expires = purpose.purpose.expires_at_ms;
            let started = Instant::now();
            while clock(&fixture)
                .0
                .checked_mul(1000)
                .expect("protocol time bound")
                < expires
            {
                assert!(
                    started.elapsed() <= Duration::from_secs(45),
                    "genuine fixture expiry must be within bounded window"
                );
                std::thread::sleep(Duration::from_millis(100));
            }
        }
        let decided = checked(agent.native_effect_approval_decide(
            approval,
            digest,
            text(case, "decision_key"),
            index != 1,
            clock(&fixture).1,
        ));
        assert_eq!(
            decided.state,
            match index {
                0 => NativeEffectApprovalFactState::Granted,
                1 => NativeEffectApprovalFactState::Rejected,
                _ => NativeEffectApprovalFactState::Expired,
            }
        );
        assert_eq!(decided.approval_id, approval);
        assert_eq!(decided.held_digest, digest);
        checked(ready(engine.advance(
            &mut scope,
            &contract,
            &mut agent,
            &signer,
            &registry,
            &trace,
            clock(&fixture).0,
        )));
        assert_eq!(
            checked(engine.status()).phases(),
            [if index == 0 {
                JourneyPhase::Prepared
            } else {
                JourneyPhase::Refused
            }]
        );
        let resumed = journal(&scope, case);
        assert!(resumed["legs"][0]["signed"].is_null());
        assert_eq!(activity_signatures(&scope, &resumed), 0);
        for field in ["idempotency_key", "plan_digest", "custody_key"] {
            assert_eq!(resumed[field], original[field]);
        }
        for field in ["action_key", "payload", "payload_hash", "preparation"] {
            assert_eq!(resumed["legs"][0][field], original["legs"][0][field]);
        }
        for field in [
            "purpose",
            "purpose_signature",
            "owner_public_key",
            "owner_key",
            "unsigned_canonical_bytes",
            "signing_preimage",
            "disclosure_digest",
            "created_head_sequence",
            "created_protocol_time_ms",
            "approval_id",
            "held_digest",
        ] {
            assert_eq!(
                resumed["legs"][0]["native"][field],
                original["legs"][0]["native"][field]
            );
        }
        if index == 0 {
            assert_eq!(
                bytes(&resumed["legs"][0]["native"]["release_ref"]),
                decided.release_ref.expect("genuine approval permission")
            );
        }
        no_money(&fixture, &resumed);
        if index == 0 {
            checked(ready(engine.advance(
                &mut scope,
                &contract,
                &mut agent,
                &signer,
                &registry,
                &trace,
                clock(&fixture).0,
            )));
            assert_eq!(checked(engine.status()).phases(), [JourneyPhase::Signed]);
            let signed = journal(&scope, case);
            assert!(signed["legs"][0]["signed"].is_object());
            assert_eq!(activity_signatures(&scope, &signed), 1);
            assert_eq!(
                bytes(&signed["legs"][0]["signed"]["canonical_digest"]),
                Sha256::digest(&material.canonical_unsigned_bytes).to_vec()
            );
            no_money(&fixture, &signed);
        } else {
            assert_eq!(checked(engine.status()).state(), JourneyState::Refused);
        }
        let daemon = checked(layerx_agentd::store::Store::open(text(
            &fixture,
            "agent_store",
        )));
        let tenant = checked(layerx_agentd::store::TenantId::new(text(
            &fixture,
            "agent_tenant",
        )));
        let durable = checked(layerx_agentd::prepare::DurablePreparation::decode(
            tenant,
            daemon
                .get(&durable_key)
                .expect("actual retained decision")
                .bytes(),
        ));
        assert_eq!(
            durable.state,
            match index {
                0 => layerx_agentd::prepare::LifecycleState::Prepared,
                1 => layerx_agentd::prepare::LifecycleState::Failed,
                _ => layerx_agentd::prepare::LifecycleState::Expired,
            }
        );
        assert_ne!(checked(engine.status()).state(), JourneyState::Done);
        let before_reopen = journal(&scope, case);
        drop(scope);
        drop(retained);
        drop(signer);
        let mut retained = store(&fixture);
        let scope = checked(retained.principal(&principal));
        let reopened = checked(JourneyEngine::load(
            &scope,
            &checked(JourneyId::new(text(case, "journey_id"))),
        ))
        .expect("genuine decided native reopen");
        assert_eq!(journal(&scope, case), before_reopen);
        assert_eq!(reopened.native_approval_binding(), Some((approval, digest)));
        no_money(&fixture, &before_reopen);
    }
}
