use super::*;

pub struct Runtime {
    payment_port: u16,
    program_port: u16,
    webhook_port: u16,
    producer_token: String,
    webhook_token: String,
}

struct Context<'a> {
    cluster: &'a Cluster,
    certificates: &'a Certificates,
    identity: &'a LocalIdentity,
    authority: &'a LocalAuthority,
    redis: &'a LocalRedis,
    gateway: &'a Gateway,
}

impl Runtime {
    pub fn prepare(cluster: &Cluster) -> Self {
        Self {
            payment_port: free_port(),
            program_port: free_port(),
            webhook_port: free_port(),
            producer_token: local_secret(&cluster.root, "payment-producer-token", &token()),
            webhook_token: local_secret(&cluster.root, "payment-webhook-token", &token()),
        }
    }

    pub fn configure(
        &self,
        environment: &mut BTreeMap<&'static str, String>,
        certificates: &Certificates,
    ) {
        for (prefix, port, token_file) in [
            ("PAYMENT", self.payment_port, &self.producer_token),
            ("PROGRAM", self.program_port, &self.producer_token),
            ("WEBHOOKS", self.webhook_port, &self.webhook_token),
        ] {
            let names = match prefix {
                "PAYMENT" => [
                    "LAYERX_EVENTS_PAYMENT_UPSTREAM_URL",
                    "LAYERX_EVENTS_PAYMENT_UPSTREAM_CA_DER",
                    "LAYERX_EVENTS_PAYMENT_UPSTREAM_TOKEN_FILE",
                    "LAYERX_EVENTS_PAYMENT_UPSTREAM_CLIENT_IDENTITY_PKCS12",
                    "LAYERX_EVENTS_PAYMENT_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
                ],
                "PROGRAM" => [
                    "LAYERX_EVENTS_PROGRAM_UPSTREAM_URL",
                    "LAYERX_EVENTS_PROGRAM_UPSTREAM_CA_DER",
                    "LAYERX_EVENTS_PROGRAM_UPSTREAM_TOKEN_FILE",
                    "LAYERX_EVENTS_PROGRAM_UPSTREAM_CLIENT_IDENTITY_PKCS12",
                    "LAYERX_EVENTS_PROGRAM_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
                ],
                _ => [
                    "LAYERX_EVENTS_WEBHOOKS_UPSTREAM_URL",
                    "LAYERX_EVENTS_WEBHOOKS_UPSTREAM_CA_DER",
                    "LAYERX_EVENTS_WEBHOOKS_UPSTREAM_TOKEN_FILE",
                    "LAYERX_EVENTS_WEBHOOKS_UPSTREAM_CLIENT_IDENTITY_PKCS12",
                    "LAYERX_EVENTS_WEBHOOKS_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
                ],
            };
            let values = [
                origin(port),
                text(&certificates.path("ca.der")),
                token_file.clone(),
                environment["LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12"].clone(),
                environment["LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE"].clone(),
            ];
            environment.extend(names.into_iter().zip(values));
        }
    }

    pub fn start(
        &self,
        cluster: &Cluster,
        certificates: &Certificates,
        identity: &LocalIdentity,
        authority: &LocalAuthority,
        redis: &LocalRedis,
        gateway: &Gateway,
    ) -> Vec<Daemon> {
        let context = Context {
            cluster,
            certificates,
            identity,
            authority,
            redis,
            gateway,
        };
        let key = local_secret(&cluster.root, "event-api-key", &event_key(&context));
        let credentials = local_secret(
            &cluster.root,
            "event-credentials.json",
            &serde_json::json!({cluster.treasury_did.clone(): key}).to_string(),
        );
        let mut processes = Vec::new();
        let mut sources = Vec::new();
        for kind in ["payment", "program", "journey", "approval"] {
            let port = if kind == "payment" {
                self.payment_port
            } else if kind == "program" {
                self.program_port
            } else {
                free_port()
            };
            let poll_token = local_secret(&cluster.root, &format!("{kind}-poll-token"), &token());
            processes.push(start_source(
                &context,
                kind,
                port,
                &credentials,
                &poll_token,
                &self.producer_token,
            ));
            sources.push((kind, port, poll_token));
        }
        let kms_port = free_port();
        let kms_token = local_secret(&cluster.root, "webhook-kms-token", &token());
        processes.push(start_kms(&context, kms_port, &kms_token));
        processes.push(start_webhooks(
            &context,
            self.webhook_port,
            &self.webhook_token,
            kms_port,
            &kms_token,
            &sources,
        ));
        processes
    }
}

fn origin(port: u16) -> String {
    format!("https://localhost:{port}")
}

fn event_key(context: &Context<'_>) -> String {
    let http = Http {
        port: context.gateway.port,
        ca: Certificate::from_der(&context.certificates.ca_der).required("CA"),
        identity: None,
    };
    let key = local_json_with_idempotency(
        &http,
        "/v1/keys",
        &context.identity.session,
        "event-source-key",
        &serde_json::json!({"signer_public_key":context.identity.signer,"scopes":["activity:write","program:call"],"quota_requests":1000,"quota_window_seconds":60}),
        201,
    );
    format!(
        "{}:{}",
        key["key"]["id"].as_str().required("event key id"),
        key["key"]["secret"].as_str().required("event key secret")
    )
}

fn tls_environment(context: &Context<'_>, prefix: &str, port: u16) -> BTreeMap<String, String> {
    BTreeMap::from([
        (format!("{prefix}_LISTEN"), format!("127.0.0.1:{port}")),
        (
            format!("{prefix}_TLS_CERT_DER"),
            text(&context.certificates.path("core.der")),
        ),
        (
            format!("{prefix}_TLS_KEY_DER"),
            text(&context.certificates.path("core-key.der")),
        ),
        (
            format!("{prefix}_CLIENT_CA_DER"),
            text(&context.certificates.path("ca.der")),
        ),
    ])
}

fn upstream_environment(
    context: &Context<'_>,
    prefix: &str,
    port: u16,
    token_file: &str,
) -> BTreeMap<String, String> {
    BTreeMap::from([
        (format!("{prefix}_UPSTREAM_URL"), origin(port)),
        (
            format!("{prefix}_UPSTREAM_CA_DER"),
            text(&context.certificates.path("ca.der")),
        ),
        (
            format!("{prefix}_UPSTREAM_TOKEN_FILE"),
            token_file.to_owned(),
        ),
        (
            format!("{prefix}_UPSTREAM_CLIENT_IDENTITY_PKCS12"),
            text(&context.certificates.path("gateway-client.p12")),
        ),
        (
            format!("{prefix}_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE"),
            text(&context.cluster.root.join("client-password")),
        ),
    ])
}

fn start_process(
    context: &Context<'_>,
    name: &str,
    label: &str,
    port: u16,
    environment: &BTreeMap<String, String>,
) -> Daemon {
    let environment = environment
        .iter()
        .map(|(name, value)| (name.as_str(), value.clone()))
        .collect();
    let mut process = spawn(
        &local_binary(name),
        &[],
        &environment,
        false,
        context.cluster.root.join(format!("{label}.stderr")),
    );
    wait_for_port(port, &mut process, label);
    process
}

fn start_source(
    context: &Context<'_>,
    kind: &str,
    port: u16,
    credentials: &str,
    poll_token: &str,
    producer: &str,
) -> Daemon {
    let mut environment = tls_environment(context, "LAYERX_EVENTS", port);
    environment.extend(upstream_environment(
        context,
        "LAYERX_EVENTS",
        context.gateway.port,
        poll_token,
    ));
    let producers = local_secret(
        &context.cluster.root,
        &format!("{kind}-producers.json"),
        &serde_json::json!([{
            "token_file":producer,"allow_principal_digest":matches!(kind, "payment" | "program")
        }])
        .to_string(),
    );
    environment.extend([
        ("LAYERX_EVENTS_KIND".to_owned(), format!("{kind}s")),
        (
            "LAYERX_EVENTS_CREDENTIALS_FILE".to_owned(),
            credentials.to_owned(),
        ),
        ("LAYERX_EVENTS_TOKEN_FILE".to_owned(), poll_token.to_owned()),
        ("LAYERX_EVENTS_PRODUCERS_FILE".to_owned(), producers),
        (
            "LAYERX_EVENTS_STATE_DIR".to_owned(),
            text(&context.cluster.root.join(format!("{kind}-event-state"))),
        ),
    ]);
    start_process(
        context,
        "layerx-event-source",
        &format!("events-{kind}"),
        port,
        &environment,
    )
}

fn start_kms(context: &Context<'_>, port: u16, credential: &str) -> Daemon {
    let mut environment = tls_environment(context, "LAYERX_KMS", port);
    environment.extend([
        ("LAYERX_KMS_TOKEN_FILE".to_owned(), credential.to_owned()),
        (
            "LAYERX_KMS_SEAL_SECRET_FILE".to_owned(),
            local_secret(&context.cluster.root, "event-kms-seal", &token()),
        ),
        (
            "LAYERX_KMS_STATE_DIR".to_owned(),
            text(&context.cluster.root.join("event-kms-state")),
        ),
    ]);
    start_process(context, "layerx-kms", "event-kms", port, &environment)
}

fn start_webhooks(
    context: &Context<'_>,
    port: u16,
    trigger: &str,
    kms_port: u16,
    kms_token: &str,
    sources: &[(&str, u16, String)],
) -> Daemon {
    let mut environment = tls_environment(context, "LAYERX_WEBHOOKS", port);
    for (name, value) in [
        (
            "INTERNAL_CA_DER",
            text(&context.certificates.path("ca.der")),
        ),
        ("PUBLIC_CA_DER", text(&context.certificates.path("ca.der"))),
        (
            "CLIENT_IDENTITY_PKCS12",
            text(&context.certificates.path("gateway-client.p12")),
        ),
        (
            "CLIENT_IDENTITY_PASSWORD_FILE",
            text(&context.cluster.root.join("client-password")),
        ),
        (
            "CURSOR_KEY_FILE",
            local_secret(
                &context.cluster.root,
                "event-cursor-key",
                &hex_encode(&random32()),
            ),
        ),
        ("INSTANCE_ID", "gateway-integration".to_owned()),
        ("KMS_URL", origin(kms_port)),
        ("KMS_TOKEN_FILE", kms_token.to_owned()),
        (
            "REDIS_URL",
            format!("rediss://localhost:{}", context.redis.port),
        ),
        (
            "REDIS_USERNAME_FILE",
            text(&context.cluster.root.join("redis-user")),
        ),
        (
            "REDIS_PASSWORD_FILE",
            text(&context.cluster.root.join("redis-password")),
        ),
        ("IDENTITY_URL", origin(context.identity.port)),
        (
            "IDENTITY_TOKEN_FILE",
            text(&context.identity.tokens.join("webhooks")),
        ),
        (
            "COMPONENT_URL",
            context.gateway.environment["LAYERX_GATEWAY_COMPONENT_URL"].clone(),
        ),
        (
            "COMPONENT_TOKEN_FILE",
            text(&context.cluster.root.join("registry-boundary-webhook-token")),
        ),
        ("AUTHORITY_URL", origin(context.authority.port)),
        ("AUTHORITY_TOKEN_FILE", context.authority.token_file.clone()),
        (
            "SEQUENCER_PUBLIC_KEY_FILE",
            context.gateway.signer_file.clone(),
        ),
        (
            "SEQUENCER_ID_FILE",
            text(&context.cluster.root.join("sequencer-id.hex")),
        ),
        (
            "SEQUENCER_FIRST_BATCH_FILE",
            text(&context.cluster.root.join("sequencer-first-batch")),
        ),
        (
            "SEQUENCER_LAST_BATCH_FILE",
            text(&context.cluster.root.join("sequencer-last-batch")),
        ),
        ("NETWORK_ID", NETWORK_ID.to_string()),
        ("LXP_WIRE_VERSION", PROTOCOL_VERSION.to_string()),
        ("SOURCE_TRIGGER_TOKEN_FILE", trigger.to_owned()),
        (
            "OPERATOR_TOKEN_FILE",
            local_secret(&context.cluster.root, "event-operator-token", &token()),
        ),
    ] {
        environment.insert(format!("LAYERX_WEBHOOKS_{name}"), value);
    }
    for (kind, port, token_file) in sources {
        let stem = kind.to_ascii_uppercase();
        environment.insert(format!("LAYERX_WEBHOOKS_{stem}_SOURCE_URL"), origin(*port));
        environment.insert(
            format!("LAYERX_WEBHOOKS_{stem}_SOURCE_TOKEN_FILE"),
            token_file.clone(),
        );
    }
    start_process(
        context,
        "layerx-webhooks",
        "payment-webhooks",
        port,
        &environment,
    )
}

const WEB_REQUEST_TOPIC: &[u8] = b"PAXEERX_WEB_REQUEST_V1";
const WEB_REQUEST_ID: u64 = 0x0102_0304_0506_0708;
const WEB_REQUEST_KIND_FETCH: u8 = 1;
const WEB_REQUEST_OPERATION: u8 = 1;
const WEB_REQUEST_PAYLOAD: &[u8] = b"https://paxeer.app/status";
const WEB_REQUEST_FEE: u8 = 100;
const WEB_READER_GUEST_ABI: u16 = 4;
const PROGRAM_FEE_LIMIT: u128 = 67_108_864;

fn web_fee_account(asset: &[u8; 32]) -> [u8; 32] {
    sha256(&[b"PAXEERX_WEB_FEES_V1", asset])
}

fn web_request_capabilities(asset: &[u8; 32], fee_account: &[u8; 32]) -> Vec<u8> {
    let mut capabilities = vec![0, 2, 3, 5];
    capabilities.extend_from_slice(asset);
    capabilities.extend_from_slice(fee_account);
    capabilities.extend_from_slice(&u128::from(WEB_REQUEST_FEE).to_be_bytes());
    capabilities
}

fn web_request_calldata(asset: &[u8; 32], fee_account: &[u8; 32]) -> Vec<u8> {
    let mut calldata = vec![1, WEB_REQUEST_OPERATION];
    calldata.extend_from_slice(&WEB_REQUEST_ID.to_be_bytes());
    calldata.push(WEB_REQUEST_KIND_FETCH);
    calldata.extend_from_slice(asset);
    calldata.extend_from_slice(fee_account);
    calldata.extend_from_slice(&u128::from(WEB_REQUEST_FEE).to_be_bytes());
    calldata.extend_from_slice(WEB_REQUEST_PAYLOAD);
    calldata
}

fn web_request_record() -> Vec<u8> {
    let mut record = WEB_REQUEST_ID.to_be_bytes().to_vec();
    record.push(WEB_REQUEST_KIND_FETCH);
    record.extend_from_slice(
        &u32::try_from(WEB_REQUEST_PAYLOAD.len())
            .required("request payload length")
            .to_be_bytes(),
    );
    record.extend_from_slice(WEB_REQUEST_PAYLOAD);
    record
}

fn submit_program_activity(
    cluster: &Cluster,
    boundary: &Boundary,
    ordinal: u16,
    path: &str,
    payload: &[u8],
    fee_limit: u128,
) -> serde_json::Value {
    let sequence = account_sequence(&cluster.lni_socket, &cluster.treasury_did);
    let signed = signed_program_activity_with_fee(
        &cluster.treasury_seed,
        &cluster.treasury_did,
        sequence,
        ordinal,
        payload,
        fee_limit,
    );
    let kind = must(ActivityType::new(ModuleId::Programs, ordinal), "kind");
    let registry = must(
        ModuleRegistry::new(&[must(
            ModuleRegistration::new(ModuleId::Programs, &[kind]),
            "registration",
        )]),
        "registry",
    );
    let activity = must(
        layerx_wire::activity::decode_signed(&signed, &registry),
        "signed activity",
    );
    let key = hex_encode(&activity.idempotency_key());
    let answer = boundary.core.request(
        "POST",
        path,
        &[
            ("Content-Type", "application/octet-stream"),
            ("Idempotency-Key", key.as_str()),
        ],
        &signed,
    );
    assert_eq!(answer.status, 200, "{path}: {}", answer.body);
    let result = json(&answer);
    let receipt = layerx_platform_core::hex_decode(
        result["result"]["receipt"]
            .as_str()
            .required("program receipt"),
    )
    .required("program receipt bytes");
    let receipt =
        layerx_proof::receipt::verify_sequencer_signature(&receipt, cluster.sequencer_key)
            .required("program receipt signature");
    let protocol = receipt.protocol().required("program receipt protocol");
    assert_eq!(protocol.result_code(), 0, "{path}: {result}");
    assert_eq!(
        (protocol.module_id(), u16::from(protocol.operation())),
        (9, ordinal),
        "{path}: {result}"
    );
    result
}

fn deploy_web_reader(cluster: &Cluster, boundary: &Boundary) -> [u8; 32] {
    use layerx_types::program_lifecycle::{NativeProgramDeploy, ProgramUpgradePolicy};
    let wasm =
        fs::read(std::env::var_os("LAYERX_TEST_WEB_READER_WASM").required("built web-reader WASM"))
            .required("web-reader WASM bytes");
    let program_id = random32();
    let owner = must(
        layerx_types::account::AccountId::parse(&format!("agent:{}:main", cluster.treasury_did)),
        "treasury account",
    );
    let deploy = NativeProgramDeploy {
        program_id: ProgramId::new(program_id),
        guest_abi: WEB_READER_GUEST_ABI,
        policy: ProgramUpgradePolicy::Authority(must(
            layerx_wire::hash::account_id_for_protocol(&owner, PROTOCOL_VERSION),
            "principal",
        )),
        new_hash: Sha256::digest(wasm.as_slice()).into(),
        interface: None,
        wasm: &wasm,
    };
    submit_program_activity(
        cluster,
        boundary,
        1,
        "/v1/programs/deploy",
        &must(deploy.encode(), "web-reader deploy"),
        PROGRAM_FEE_LIMIT,
    );
    program_id
}

fn call_web_request(cluster: &Cluster, boundary: &Boundary, program_id: [u8; 32]) {
    let fee_account = web_fee_account(&cluster.asset);
    let capabilities = web_request_capabilities(&cluster.asset, &fee_account);
    let calldata = web_request_calldata(&cluster.asset, &fee_account);
    let call = NativeProgramCall {
        program_id: ProgramId::new(program_id),
        guest_abi: WEB_READER_GUEST_ABI,
        entrypoint: b"layerx_call",
        calldata: &calldata,
        capabilities: &capabilities,
        access_declaration: b"LayerX/programs/access-declaration/v1\0\0",
        response_capacity: 16,
        resources: Resources([
            1_000_000, 16_777_216, 1_048_576, 1_048_576, 64, 1_048_576, 4096,
        ]),
    };
    submit_program_activity(
        cluster,
        boundary,
        3,
        "/v1/programs/call",
        &must(call.encode(), "web request call"),
        PROGRAM_FEE_LIMIT,
    );
}

fn program_events_page(
    call: &impl Fn(&str, serde_json::Value, bool) -> serde_json::Value,
    topic: &str,
    from_sequence: u64,
    limit: u64,
) -> serde_json::Value {
    let answer = call(
        "lx_getProgramEvents",
        serde_json::json!([{"topic": topic, "from_sequence": from_sequence, "limit": limit}]),
        false,
    );
    assert!(answer.get("error").is_none(), "{answer}");
    answer["result"].clone()
}

fn next_sequence(page: &serde_json::Value) -> u64 {
    page["next_sequence"].as_u64().required("next_sequence")
}

#[test]
fn local_gateway_program_events_read_the_web_request() {
    let (cluster, _funding) = funding::start();
    let certificates = certificates(&cluster.root);
    let boundary = start_boundary(&cluster, &certificates);
    let identity = start_local_identity(&cluster, &certificates);
    let authority = start_local_authority(&cluster, &certificates);
    let redis = start_local_redis(&cluster, &certificates);
    let gateway = start_gateway_runtime(
        &cluster,
        &certificates,
        &boundary,
        &identity,
        &authority,
        &redis,
        false,
    );
    let http = Http {
        port: gateway.port,
        ca: Certificate::from_der(&certificates.ca_der).required("CA"),
        identity: None,
    };
    let call = |method: &str, params: serde_json::Value, authenticated: bool| {
        local_rpc(&http, "", method, &params, authenticated)
    };
    let topic = hex_encode(WEB_REQUEST_TOPIC);
    let before = program_events_page(&call, &topic, 0, 256);
    assert_eq!(before["events"], serde_json::json!([]), "{before}");
    let head = next_sequence(&before);

    let program_id = deploy_web_reader(&cluster, &boundary);
    call_web_request(&cluster, &boundary, program_id);

    let deadline = Instant::now() + Duration::from_secs(60);
    let page = loop {
        let page = program_events_page(&call, &topic, head, 256);
        if page["events"]
            .as_array()
            .is_some_and(|events| !events.is_empty())
        {
            break page;
        }
        assert!(
            Instant::now() < deadline,
            "no committed web request: {page}"
        );
        thread::sleep(Duration::from_millis(250));
    };
    let events = page["events"].as_array().required("events");
    assert_eq!(events.len(), 1, "{page}");
    let event = &events[0];
    let sequence = event["sequence"].as_u64().required("event sequence");
    assert!(
        sequence >= head && sequence < next_sequence(&page),
        "{page}"
    );
    assert_eq!(
        event,
        &serde_json::json!({
            "sequence": sequence,
            "program_id": hex_encode(&program_id),
            "topic": topic,
            "data": hex_encode(&web_request_record()),
        })
    );
    let request = layerx_platform_core::hex_decode(event["data"].as_str().required("event data"))
        .required("event data bytes");
    assert_eq!(&request[13..], WEB_REQUEST_PAYLOAD);

    let exact = program_events_page(&call, &topic, sequence, 1);
    assert_eq!(exact["events"], page["events"], "{exact}");
    assert_eq!(next_sequence(&exact), sequence + 1, "{exact}");
    let after = program_events_page(&call, &topic, sequence + 1, 256);
    assert_eq!(after["events"], serde_json::json!([]), "{after}");
    assert!(next_sequence(&after) > sequence, "{after}");
    let other = program_events_page(&call, &hex_encode(b"PAXEERX_OTHER_TOPIC_V1"), 0, 256);
    assert_eq!(other["events"], serde_json::json!([]), "{other}");

    let relayed = boundary
        .core
        .get(&format!("/v1/programs/events/{topic}/{sequence}/1"));
    assert_eq!(relayed.status, 200, "{}", relayed.body);
    assert_eq!(json(&relayed)["result"], exact);
    for params in [
        serde_json::json!([{"topic": topic.to_ascii_uppercase(), "from_sequence": 0, "limit": 1}]),
        serde_json::json!([{"topic": topic, "from_sequence": 0, "limit": 257}]),
        serde_json::json!([{"topic": topic, "from_sequence": 0}]),
    ] {
        assert_eq!(
            call("lx_getProgramEvents", params, false)["error"]["code"],
            -32602
        );
    }
}
