use layerx_agent_api::identity::{AgentDid, AuthorityRef};
use layerx_agent_api::prepare::{IdempotencyRef, PayloadBytes, PrepareRequest, TimestampBound};
use layerx_client::client::{ClientConfig, ReconnectPolicy};
use layerx_client::lni::handshake::HandshakeConfig;
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::Limits;
use layerx_client::Client;
use layerx_human_service::custody::{
    CustodySigner, KeyId, Keystore, NativeConsent, NativeConsentRequest, Operation,
    SignAuthorization, SigningLimits,
};
use layerx_human_service::server::agent_runtime::AgentRuntime;
use layerx_human_service::server::native_send::{
    load_access, preview_send_v2, HumanOwnerNativeContextV1,
};
use layerx_human_service::server::production_components::{AttestorCustodyConfig, AttestorKms};
use layerx_human_service::store::{
    PrincipalId, PrincipalStore, RetentionPeriod, RetentionPolicy, RowKey, Table, TenancyDigest,
};
use layerx_human_service::trace::TraceId;
use layerx_intents::{Intent, IntentKind, LxpSend};
use layerx_types::account::AccountId;
use layerx_types::amount::Amount;
use layerx_types::ids::{AssetId, Did, IdempotencyKey};
use layerx_types::intent::{
    AuthorizationSignature, ContextHash, NetworkId, ProtocolVersion, PublicKey, SendAuthorization,
    SendAuthorizationKind, Sequence, TimestampSeconds,
};
use native_tls::{Certificate, TlsConnector};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::fmt::Debug;
use std::fs;
use std::future::Future;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::Duration;

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

fn prepare(
    fixture: &Value,
    case: &Value,
    agent: &mut AgentRuntime,
    context: &HumanOwnerNativeContextV1,
) -> PrepareRequest {
    let data: Value = checked(serde_json::from_slice(&protected(
        text(case, "intent_file"),
        65536,
    )));
    let source = checked(AccountId::parse(text(&data, "from")));
    let source_id = checked(layerx_intents::canonical::account_id_for_protocol(
        &source, 3,
    ));
    let source_state = checked(agent.account_state(source_id));
    assert_eq!(
        source_state.next_sequence,
        number(&data, "account_sequence")
    );
    assert_eq!(
        source_state.authority_key,
        Some(context.session.owner_public_key)
    );
    assert_eq!(
        id(text(&data, "authorization_public_key")),
        context.session.owner_public_key
    );
    let send = checked(LxpSend::new(
        source,
        checked(AccountId::parse(text(&data, "to"))),
        AssetId::new(id(text(fixture, "asset_id"))),
        Amount::from_u128(checked(text(&data, "amount").parse())),
        Sequence::from_u64(source_state.next_sequence),
        IdempotencyKey::new(id(text(&data, "action_key"))),
        TimestampSeconds::from_u64(number(&data, "expires_at")),
        ContextHash::new(id(text(&data, "context_hash"))),
        SendAuthorization::new(
            SendAuthorizationKind::Owner,
            PublicKey::new(context.session.owner_public_key),
            AuthorizationSignature::new(checked(
                decode_hex(text(&data, "authorization_signature")).try_into(),
            )),
        ),
        checked(NetworkId::new(checked(
            number(fixture, "network_id").try_into(),
        ))),
        checked(ProtocolVersion::new(3)),
    ));
    let compiled = checked(layerx_intents::compile(
        &Intent::v1(IntentKind::LxpSend(send)),
        agent.registry(),
    ));
    let coordinates =
        checked(agent.native_send_owner_context(&context.session, number(fixture, "request_id")));
    PrepareRequest {
        protocol_activity_type: compiled.activity_type().value(),
        actor: checked(AgentDid::new(context.session.owner.clone())),
        authority: checked(AuthorityRef::new(
            context
                .session
                .owner_public_key
                .iter()
                .map(|b| format!("{b:02x}"))
                .collect::<String>(),
        )),
        account_sequence: layerx_agent_api::Sequence(coordinates.account_sequence),
        timestamp_bound: checked(
            TimestampBound {
                not_before: layerx_agent_api::TimestampSeconds(coordinates.protocol_time_ms),
                not_after: layerx_agent_api::TimestampSeconds(context.expires_at_ms),
            }
            .validate(),
        ),
        idempotency_key: checked(IdempotencyRef::new(text(&data, "action_key"))),
        fee_limit: layerx_agent_api::Amount(checked(text(&data, "fee_limit").parse())),
        payload: checked(PayloadBytes::new(compiled.payload().as_bytes().to_vec())),
        payload_hash: compiled.payload_hash(),
    }
}
fn local_state(fixture: &Value) -> Vec<(u8, Vec<u8>, Vec<u8>)> {
    use layerx_agentd::store::{ObjectKind, Store, TenantId, TenantKey};
    let store = checked(Store::open_read_only(text(fixture, "agent_store")));
    let tenant = checked(TenantId::new(text(fixture, "agent_tenant")));
    let mut result = Vec::new();
    for kind in [
        ObjectKind::Capability,
        ObjectKind::Budget,
        ObjectKind::PreparedActivity,
        ObjectKind::Outbox,
    ] {
        for object in store.list_object_ids(&tenant, kind) {
            let key = checked(TenantKey::new(tenant.clone(), kind, object.clone()));
            result.push((
                kind as u8,
                object,
                store
                    .get(&key)
                    .expect("actual owner local record")
                    .bytes()
                    .to_vec(),
            ));
        }
    }
    result.sort();
    result
}
struct Ready;
impl Wake for Ready {
    fn wake(self: Arc<Self>) {}
}
fn ready<F: Future>(future: F) -> F::Output {
    let mut future = std::pin::pin!(future);
    match future
        .as_mut()
        .poll(&mut Context::from_waker(&Waker::from(Arc::new(Ready))))
    {
        Poll::Ready(value) => value,
        Poll::Pending => panic!("actual synchronous custody boundary did not complete"),
    }
}
fn signer(fixture: &Value, agent: &AgentRuntime) -> CustodySigner {
    let attestor = &fixture["attestor"];
    let nodes = attestor["nodes"]
        .as_array()
        .expect("actual attestor nodes")
        .iter()
        .map(|node| {
            (
                text(node, "id").to_owned(),
                checked(text(node, "address").parse()),
            )
        })
        .collect();
    let signers = attestor["signers"]
        .as_array()
        .expect("actual quorum")
        .iter()
        .map(|v| v.as_str().expect("actual signer id").to_owned())
        .collect();
    let provider = checked(AttestorKms::connect(checked(AttestorCustodyConfig::new(
        nodes,
        signers,
        protected(text(attestor, "root_der_file"), 65536),
        protected(text(attestor, "client_der_file"), 65536),
        protected(text(attestor, "client_pkcs8_file"), 65536),
        Duration::from_secs(60),
    ))));
    let assertion = checked(String::from_utf8(protected(
        text(attestor, "purpose_assertion_file"),
        65536,
    )));
    checked(provider.admit_assertion(text(fixture, "owner"), assertion.trim()));
    let keys = checked(Keystore::open_production(
        text(fixture, "custody_store"),
        checked(number(fixture, "network_id").try_into()),
        provider,
    ));
    CustodySigner::new(
        keys,
        store(fixture),
        agent.registry().clone(),
        checked(SigningLimits::new(32, 60)),
    )
}
#[test]
fn genuine_human_owner_session_consent_preview_and_reopen() {
    let path = std::env::var("PAXEER_X_HUMAN_NATIVE_SEND_PRODUCER_FIXTURE")
        .expect("genuine protected Human native producer foundation required");
    let fixture: Value = checked(serde_json::from_slice(&protected(&path, 65536)));
    assert_eq!(fixture["schema"], "paxeer-x.human-native-send-producer.v1");
    assert_eq!(fixture["isolated_real_owner"], true);
    let case = &fixture["case"];
    let principal = checked(PrincipalId::new(text(&fixture, "principal")));
    let capability = id(text(case, "capability_id"));
    let mut retained = store(&fixture);
    let mut scope = checked(retained.principal(&principal));
    assert!(load_access(&scope, capability, clock(&fixture).0).is_err());
    drop(scope);
    drop(retained);
    println!("NATIVE_SEND_PRODUCER_CASE denied-before-consent");
    public_access(&fixture, case);
    let mut retained = store(&fixture);
    let mut scope = checked(retained.principal(&principal));
    let context = checked(load_access(&scope, capability, clock(&fixture).0));
    assert_eq!(context.session.owner, text(&fixture, "owner"));
    assert_eq!(context.session.principal, principal.as_str());
    assert_eq!(context.session.tenant, text(&fixture, "agent_tenant"));
    assert_ne!(context.session.grant_receipt_digest, [0; 32]);
    assert_ne!(context.session.grant_activity_id, [0; 32]);
    let grant = checked(context.signed_grant());
    let digest: [u8; 32] = Sha256::digest(checked(
        layerx_human_kms::attestor::native_local_grant_signing_bytes(&grant),
    ))
    .into();
    checked(ed25519_dalek::Verifier::verify(
        &checked(ed25519_dalek::VerifyingKey::from_bytes(
            &context.session.owner_public_key,
        )),
        &digest,
        &ed25519_dalek::Signature::from_bytes(&grant.signature),
    ));
    let mut agent = runtime(
        &fixture,
        principal.as_str(),
        text(&fixture, "owner"),
        text(&fixture, "account"),
    );
    let owner =
        checked(agent.native_send_owner_context(&context.session, number(&fixture, "request_id")));
    assert_eq!(owner.generation, context.session.generation);
    assert_eq!(owner.owner_public_key, context.session.owner_public_key);
    assert_eq!(owner.actor, context.session.owner);
    println!("NATIVE_SEND_PRODUCER_CASE actual-session-and-signed-grant");
    let request = prepare(&fixture, case, &mut agent, &context);
    let before = local_state(&fixture);
    let head = clock(&fixture).1;
    let fee = checked(agent.session_fee_state(context.session.grant_id));
    let expiry = context.expires_at_ms;
    let first = checked(preview_send_v2(
        &scope,
        &mut agent,
        &request,
        capability,
        expiry,
        clock(&fixture).0,
    ));
    let second = checked(preview_send_v2(
        &scope,
        &mut agent,
        &request,
        capability,
        expiry,
        clock(&fixture).0,
    ));
    assert_eq!(first.canonical_bytes, second.canonical_bytes);
    assert_eq!(first.signing_preimage, second.signing_preimage);
    assert_eq!(first.purpose, second.purpose);
    assert_eq!(
        first.purpose.canonical_digest,
        <[u8; 32]>::from(Sha256::digest(&first.canonical_bytes))
    );
    assert_eq!(first.purpose.economic_action, capability);
    assert_eq!(
        first.purpose.idempotency_key,
        id(request.idempotency_key.as_str())
    );
    assert_eq!(
        first.purpose.owner_public_key,
        context.session.owner_public_key
    );
    assert_eq!(local_state(&fixture), before);
    assert_eq!(clock(&fixture).1, head);
    let after = checked(agent.session_fee_state(context.session.grant_id));
    assert_eq!(
        (
            after.spent_total,
            after.spent_this_period,
            after.charge_commitment
        ),
        (
            fee.spent_total,
            fee.spent_this_period,
            fee.charge_commitment
        )
    );
    println!("NATIVE_SEND_PRODUCER_CASE immutable-preview-without-economic-effect");
    let mut changed = request.clone();
    changed.actor = checked(AgentDid::new(text(&fixture, "foreign_owner")));
    assert!(preview_send_v2(
        &scope,
        &mut agent,
        &changed,
        capability,
        expiry,
        clock(&fixture).0
    )
    .is_err());
    changed = request.clone();
    changed.payload_hash[0] ^= 1;
    assert!(preview_send_v2(
        &scope,
        &mut agent,
        &changed,
        capability,
        expiry,
        clock(&fixture).0
    )
    .is_err());
    let foreign = checked(PrincipalId::new(text(&fixture, "foreign_principal")));
    let mut foreign_store = store(&fixture);
    let foreign_scope = checked(foreign_store.principal(&foreign));
    assert!(load_access(&foreign_scope, capability, clock(&fixture).0).is_err());
    assert!(context
        .session
        .validate(&foreign_scope, clock(&fixture).0)
        .is_err());
    println!("NATIVE_SEND_PRODUCER_CASE foreign-and-changed-context-refused");
    let custody = signer(&fixture, &agent);
    let trace = checked(TraceId::parse(text(&fixture, "trace_id")));
    let key = checked(KeyId::new("human-primary"));
    let signature = checked(ready(custody.sign_native_consent_in_scope(
        &mut scope,
        NativeConsentRequest::new(
            &principal,
            &key,
            NativeConsent::SendPurpose(&first.purpose),
            SignAuthorization::new(Operation::ProtocolMutation, None),
            clock(&fixture).0,
            trace,
        ),
    )));
    assert_eq!(
        signature.signer_public_key(),
        context.session.owner_public_key
    );
    let purpose_digest: [u8; 32] = Sha256::digest(checked(first.purpose.canonical_bytes())).into();
    checked(ed25519_dalek::Verifier::verify(
        &checked(ed25519_dalek::VerifyingKey::from_bytes(
            &context.session.owner_public_key,
        )),
        &purpose_digest,
        &ed25519_dalek::Signature::from_bytes(signature.signature()),
    ));
    let mut changed_purpose = first.purpose.clone();
    changed_purpose.canonical_digest[0] ^= 1;
    let changed_digest: [u8; 32] =
        Sha256::digest(checked(changed_purpose.canonical_bytes())).into();
    assert!(ed25519_dalek::Verifier::verify(
        &checked(ed25519_dalek::VerifyingKey::from_bytes(
            &context.session.owner_public_key
        )),
        &changed_digest,
        &ed25519_dalek::Signature::from_bytes(signature.signature())
    )
    .is_err());
    println!("NATIVE_SEND_PRODUCER_CASE exact-preview-purpose-signed");
    let original_session =
        serde_json::to_vec(&context.session).expect("actual retained session serialization");
    drop(context);
    drop(scope);
    drop(retained);
    drop(agent);
    let mut reopened = store(&fixture);
    let scope = checked(reopened.principal(&principal));
    let context = checked(load_access(&scope, capability, clock(&fixture).0));
    assert_eq!(
        serde_json::to_vec(&context.session).expect("actual reopened session serialization"),
        original_session
    );
    let mut agent = runtime(
        &fixture,
        principal.as_str(),
        text(&fixture, "owner"),
        text(&fixture, "account"),
    );
    let resumed = checked(preview_send_v2(
        &scope,
        &mut agent,
        &request,
        capability,
        expiry,
        clock(&fixture).0,
    ));
    assert_eq!(resumed.canonical_bytes, first.canonical_bytes);
    assert_eq!(resumed.purpose, first.purpose);
    let confirm: Value = checked(serde_json::from_slice(&protected(
        text(case, "access_confirm_request_file"),
        65536,
    )));
    let (status, response) = public_request(&fixture, "/v1/native/send/access/confirm", &confirm);
    assert_eq!(status, 200);
    assert_eq!(
        response["result"]["native_access_id"],
        case["capability_id"]
    );
    assert_eq!(local_state(&fixture), before);
    assert_eq!(clock(&fixture).1, head);
    println!("NATIVE_SEND_PRODUCER_CASE durable-reopen-and-exact-replay");
}
