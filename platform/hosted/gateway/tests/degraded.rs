use layerx_platform_gateway::store::{RedisEndpoint, RedisStore};
use layerx_platform_internal::http;
use layerx_platform_internal::tls::{Origin, Upstream};
use native_tls::Certificate;
use serde_json::{json, Value};
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use zeroize::Zeroizing;

const CHAIN_ID: &str = "0x7d0";
const LATEST_BLOCK: &str = "0x1a2b";
const ACCOUNT: &str = "0x102132435465768798a9bacbdcedfe0f1e2d3c4b";

struct RedisProcess {
    child: Child,
    directory: PathBuf,
    endpoint: RedisEndpoint,
    certificate: Certificate,
    port: u16,
}

impl RedisProcess {
    fn start() -> Self {
        let unique = format!(
            "layerx-degraded-redis-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        );
        let directory = std::env::temp_dir().join(unique);
        fs::create_dir(&directory)
            .unwrap_or_else(|error| panic!("test Redis directory must be created: {error}"));
        let certificate_pem = directory.join("server.pem");
        let certificate_der = directory.join("server.der");
        let private_key = directory.join("server.key");
        command(
            "openssl",
            &[
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                path(&private_key),
                "-out",
                path(&certificate_pem),
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
                path(&certificate_pem),
                "-outform",
                "DER",
                "-out",
                path(&certificate_der),
            ],
        );
        let port = free_port();
        let acl = directory.join("users.acl");
        fs::write(
            &acl,
            "user default off\nuser gateway on >gateway-secret ~* &* +@all\n",
        )
        .unwrap_or_else(|error| panic!("test Redis ACL must be written: {error}"));
        let config = directory.join("redis.conf");
        fs::write(
            &config,
            format!(
                "bind 127.0.0.1\nport 0\ntls-port {port}\ntls-cert-file {}\ntls-key-file {}\ntls-ca-cert-file {}\ntls-auth-clients no\naclfile {}\nappendonly yes\nappendfsync always\ndir {}\nprotected-mode yes\n",
                path(&certificate_pem),
                path(&private_key),
                path(&certificate_pem),
                path(&acl),
                path(&directory),
            ),
        )
        .unwrap_or_else(|error| panic!("test Redis config must be written: {error}"));
        let endpoint = RedisEndpoint::parse(&format!("rediss://localhost:{port}"))
            .unwrap_or_else(|error| panic!("test Redis endpoint must parse: {error}"));
        let certificate = Certificate::from_der(
            &fs::read(&certificate_der)
                .unwrap_or_else(|error| panic!("test certificate must be read: {error}")),
        )
        .unwrap_or_else(|error| panic!("test certificate must parse: {error}"));
        let child = Command::new("redis-server")
            .arg(&config)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("real Redis server must start: {error}"));
        let process = Self {
            child,
            directory,
            endpoint,
            certificate,
            port,
        };
        for _ in 0..100 {
            if TcpStream::connect(("127.0.0.1", port)).is_ok() {
                return process;
            }
            thread::sleep(Duration::from_millis(20));
        }
        panic!("real Redis server did not become reachable")
    }

    fn store(&self) -> RedisStore {
        RedisStore::new(
            self.endpoint.clone(),
            self.certificate.clone(),
            Zeroizing::new("gateway".to_owned()),
            Zeroizing::new("gateway-secret".to_owned()),
        )
    }
}

impl Drop for RedisProcess {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = fs::remove_dir_all(&self.directory);
    }
}

struct Gateway {
    child: Child,
    client: Upstream,
}

impl Drop for Gateway {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Gateway {
    fn rpc(&self, method: &str, params: &Value) -> Value {
        let request = json!({"jsonrpc": "2.0", "id": 7, "method": method, "params": params});
        let answer = self
            .client
            .post("/rpc", request.to_string().as_bytes())
            .unwrap_or_else(|error| panic!("{method} must be answered: {error:?}"));
        assert_eq!(answer.status, 200, "{method}");
        let document: Value = serde_json::from_slice(&answer.body)
            .unwrap_or_else(|error| panic!("{method} answer must be JSON: {error}"));
        assert_eq!(document["jsonrpc"], "2.0");
        assert_eq!(document["id"], 7);
        document
    }

    fn get(&self, path: &str) -> (u16, Value) {
        let answer = self
            .client
            .get(path)
            .unwrap_or_else(|error| panic!("{path} must be answered: {error:?}"));
        let document = serde_json::from_slice(&answer.body)
            .unwrap_or_else(|error| panic!("{path} answer must be JSON: {error}"));
        (answer.status, document)
    }
}

struct PlainGateway {
    child: Child,
    port: u16,
}

impl Drop for PlainGateway {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl PlainGateway {
    fn exchange(&self, method: &str, path: &str, body: &[u8]) -> Result<(u16, Vec<u8>), String> {
        let mut stream =
            TcpStream::connect(("127.0.0.1", self.port)).map_err(|error| error.to_string())?;
        stream
            .set_read_timeout(Some(Duration::from_secs(10)))
            .map_err(|error| error.to_string())?;
        let mut request = format!(
            "{method} {path} HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
            body.len()
        )
        .into_bytes();
        request.extend_from_slice(body);
        stream
            .write_all(&request)
            .map_err(|error| error.to_string())?;
        let mut answer = Vec::new();
        stream
            .read_to_end(&mut answer)
            .map_err(|error| error.to_string())?;
        let header_end = answer
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| "plain answer has no header terminator".to_owned())?;
        let head = std::str::from_utf8(&answer[..header_end])
            .map_err(|_| "plain answer headers are not UTF-8".to_owned())?;
        let mut start = head
            .split("\r\n")
            .next()
            .unwrap_or_default()
            .split_whitespace();
        if start.next() != Some("HTTP/1.1") {
            return Err(format!("plain answer is not HTTP/1.1: {head}"));
        }
        let status = start
            .next()
            .and_then(|value| value.parse::<u16>().ok())
            .ok_or_else(|| format!("plain answer status is invalid: {head}"))?;
        Ok((status, answer[header_end + 4..].to_vec()))
    }

    fn rpc(&self, method: &str, params: &Value) -> Value {
        let request = json!({"jsonrpc": "2.0", "id": 7, "method": method, "params": params});
        let (status, body) = self
            .exchange("POST", "/rpc", request.to_string().as_bytes())
            .unwrap_or_else(|error| panic!("{method} must be answered over plain HTTP: {error}"));
        assert_eq!(status, 200, "{method}");
        let document: Value = serde_json::from_slice(&body)
            .unwrap_or_else(|error| panic!("{method} answer must be JSON: {error}"));
        assert_eq!(document["jsonrpc"], "2.0");
        assert_eq!(document["id"], 7);
        document
    }

    fn get(&self, path: &str) -> (u16, Value) {
        let (status, body) = self
            .exchange("GET", path, b"")
            .unwrap_or_else(|error| panic!("{path} must be answered over plain HTTP: {error}"));
        let document = serde_json::from_slice(&body)
            .unwrap_or_else(|error| panic!("{path} answer must be JSON: {error}"));
        (status, document)
    }
}

fn command(program: &str, arguments: &[&str]) {
    let output = Command::new(program)
        .args(arguments)
        .output()
        .unwrap_or_else(|error| panic!("{program} must run: {error}"));
    assert!(
        output.status.success(),
        "{program} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
}

fn path(value: &Path) -> &str {
    value
        .to_str()
        .unwrap_or_else(|| panic!("test path must be UTF-8"))
}

fn free_port() -> u16 {
    let listener = TcpListener::bind("127.0.0.1:0")
        .unwrap_or_else(|error| panic!("test port must be allocated: {error}"));
    listener
        .local_addr()
        .unwrap_or_else(|error| panic!("test port must resolve: {error}"))
        .port()
}

fn precompile_answers() -> Vec<(String, String)> {
    let vectors: Value = serde_json::from_str(include_str!("fixtures/paxeer-abi-vectors.json"))
        .unwrap_or_else(|error| panic!("ABI vectors: {error}"));
    vectors["cases"]
        .as_array()
        .unwrap_or_else(|| panic!("ABI vector cases missing"))
        .iter()
        .filter_map(|case| {
            Some((
                format!("0x{}", case["calldata"].as_str()?),
                format!("0x{}", case["result"].as_str()?),
            ))
        })
        .collect()
}

fn chain_answer(answers: &[(String, String)], request: &http::Request) -> http::Response {
    let Ok(call) = serde_json::from_slice::<Value>(&request.body) else {
        return http::refusal(400, "invalid_json", None);
    };
    let id = call["id"].clone();
    let result = match call["method"].as_str() {
        Some("eth_chainId") => Some(json!(CHAIN_ID)),
        Some("eth_blockNumber") => Some(json!(LATEST_BLOCK)),
        Some("eth_call") => call["params"][0]["data"].as_str().and_then(|data| {
            answers
                .iter()
                .find(|(calldata, _)| calldata == data)
                .map(|(_, result)| json!(result))
        }),
        _ => None,
    };
    http::json(
        200,
        &result.map_or_else(
            || json!({"jsonrpc": "2.0", "id": id, "error": {"code": -32000, "message": "execution reverted"}}),
            |result| json!({"jsonrpc": "2.0", "id": id, "result": result}),
        ),
    )
}

fn secret(directory: &Path, name: &str, value: &str) -> String {
    let file = directory.join(name);
    fs::write(&file, value).unwrap_or_else(|error| panic!("{name} must be written: {error}"));
    path(&file).to_owned()
}

const KERNEL_SIDE_VARIABLES: [&str; 8] = [
    "LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12",
    "LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE",
    "LAYERX_GATEWAY_SEQUENCER_PUBLIC_KEY_FILE",
    "LAYERX_GATEWAY_SEQUENCER_ID_FILE",
    "LAYERX_GATEWAY_SEQUENCER_FIRST_BATCH_FILE",
    "LAYERX_GATEWAY_SEQUENCER_LAST_BATCH_FILE",
    "LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE",
    "LAYERX_GATEWAY_MODULE_REGISTRY_FILE",
];

fn chain_only_environment(
    redis: &RedisProcess,
    chain_port: u16,
    listen: u16,
    producer: bool,
) -> Vec<(String, String)> {
    let directory = &redis.directory;
    let unused = free_port();
    let ca = path(&directory.join("server.der")).to_owned();
    let mut environment = vec![
        (
            "LAYERX_GATEWAY_LISTEN".to_owned(),
            format!("127.0.0.1:{listen}"),
        ),
        ("LAYERX_GATEWAY_TLS_CERT_DER".to_owned(), ca.clone()),
        (
            "LAYERX_GATEWAY_TLS_KEY_DER".to_owned(),
            path(&directory.join("server-key.der")).to_owned(),
        ),
        ("LAYERX_GATEWAY_OUTBOUND_CA_DER".to_owned(), ca.clone()),
        (
            "LAYERX_GATEWAY_NETWORK_ID".to_owned(),
            "paxeer-degraded".to_owned(),
        ),
        (
            "LAYERX_GATEWAY_LXP_WIRE_VERSION".to_owned(),
            layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION.to_string(),
        ),
        (
            "LAYERX_GATEWAY_PROTOCOL_NETWORK_ID".to_owned(),
            "7".to_owned(),
        ),
        (
            "LAYERX_GATEWAY_PAXEER_RPC_URL".to_owned(),
            format!("https://localhost:{chain_port}"),
        ),
        (
            "LAYERX_GATEWAY_REDIS_URL".to_owned(),
            format!("rediss://localhost:{}", redis.port),
        ),
        (
            "LAYERX_GATEWAY_REDIS_USERNAME_FILE".to_owned(),
            secret(directory, "redis-username", "gateway"),
        ),
        (
            "LAYERX_GATEWAY_REDIS_PASSWORD_FILE".to_owned(),
            secret(directory, "redis-password", "gateway-secret"),
        ),
    ];
    if !producer {
        return environment;
    }
    for kind in ["PAYMENT", "WEBHOOKS"] {
        environment.push((
            format!("LAYERX_EVENTS_{kind}_UPSTREAM_URL"),
            format!("https://localhost:{unused}"),
        ));
        environment.push((format!("LAYERX_EVENTS_{kind}_UPSTREAM_CA_DER"), ca.clone()));
        environment.push((
            format!("LAYERX_EVENTS_{kind}_UPSTREAM_TOKEN_FILE"),
            secret(directory, "producer-token", "producer-token"),
        ));
        environment.push((
            format!("LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PKCS12"),
            path(&directory.join("client.p12")).to_owned(),
        ));
        environment.push((
            format!("LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE"),
            secret(directory, "producer-identity-password", "integration-only"),
        ));
    }
    environment
}

fn gateway_command(environment: &[(String, String)]) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_layerx-gateway"));
    command
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .envs(environment.iter().map(|(name, value)| (name, value)));
    command
}

fn start_gateway(redis: &RedisProcess, chain_port: u16, producer: bool) -> Gateway {
    let listen = free_port();
    let environment = chain_only_environment(redis, chain_port, listen, producer);
    assert!(environment.iter().all(|(name, _)| {
        !KERNEL_SIDE_VARIABLES.contains(&name.as_str()) && name != "LAYERX_GATEWAY_COMPONENT_URL"
    }));
    let child = gateway_command(&environment)
        .stdout(Stdio::null())
        .stderr(Stdio::inherit())
        .spawn()
        .unwrap_or_else(|error| panic!("gateway must start: {error}"));
    let mut gateway = Gateway {
        child,
        client: Upstream::new(
            Origin::parse(&format!("https://localhost:{listen}"))
                .unwrap_or_else(|error| panic!("{error}")),
            redis.certificate.clone(),
            None,
            None,
        ),
    };
    for _ in 0..200 {
        if let Ok(Some(status)) = gateway.child.try_wait() {
            panic!("gateway refused start-up without the kernel: {status}");
        }
        if gateway
            .client
            .get("/livez")
            .is_ok_and(|answer| answer.status == 200)
        {
            return gateway;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("gateway did not become live")
}

const LISTENER_CERTIFICATE_VARIABLES: [&str; 2] =
    ["LAYERX_GATEWAY_TLS_CERT_DER", "LAYERX_GATEWAY_TLS_KEY_DER"];

fn plain_environment(redis: &RedisProcess, chain_port: u16, listen: u16) -> Vec<(String, String)> {
    let mut environment = chain_only_environment(redis, chain_port, listen, false)
        .into_iter()
        .filter(|(name, _)| !LISTENER_CERTIFICATE_VARIABLES.contains(&name.as_str()))
        .collect::<Vec<_>>();
    environment.push(("LAYERX_GATEWAY_LISTENER".to_owned(), "plain".to_owned()));
    environment
}

fn start_plain_gateway(redis: &RedisProcess, chain_port: u16) -> PlainGateway {
    let port = free_port();
    let environment = plain_environment(redis, chain_port, port);
    assert!(environment.iter().all(|(name, _)| {
        !KERNEL_SIDE_VARIABLES.contains(&name.as_str())
            && !LISTENER_CERTIFICATE_VARIABLES.contains(&name.as_str())
            && name != "LAYERX_GATEWAY_COMPONENT_URL"
            && !name.starts_with("LAYERX_EVENTS_")
    }));
    let child = gateway_command(&environment)
        .stdout(Stdio::null())
        .stderr(Stdio::inherit())
        .spawn()
        .unwrap_or_else(|error| panic!("gateway must start: {error}"));
    let mut gateway = PlainGateway { child, port };
    for _ in 0..200 {
        if let Ok(Some(status)) = gateway.child.try_wait() {
            panic!("gateway refused start-up with the plain listener: {status}");
        }
        if gateway
            .exchange("GET", "/livez", b"")
            .is_ok_and(|(status, _)| status == 200)
        {
            return gateway;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("gateway did not become live on the plain listener")
}

fn startup_refusal(environment: &[(String, String)]) -> (Option<i32>, String) {
    let output = gateway_command(environment)
        .stdout(Stdio::null())
        .output()
        .unwrap_or_else(|error| panic!("gateway must run: {error}"));
    (
        output.status.code(),
        String::from_utf8_lossy(&output.stderr)
            .trim_end()
            .to_owned(),
    )
}

fn assert_kernel_unavailable(answer: &Value, backend: &str) {
    assert!(answer.get("result").is_none(), "{answer}");
    assert_eq!(answer["error"]["code"], -32010, "{answer}");
    assert_eq!(answer["error"]["message"], "Kernel unavailable");
    assert_eq!(
        answer["error"]["data"],
        json!({"code": "kernel_unavailable", "backend": backend, "reason": "not_configured"})
    );
}

#[test]
fn degraded_endpoint_serves_the_chain_and_refuses_kernel_methods_without_the_kernel() {
    let _ = rustls::crypto::ring::default_provider().install_default();
    let redis = RedisProcess::start();
    assert!(redis.store().ready());
    let (tls, _) = payment_events::tls(&redis);
    let answers = precompile_answers();
    let chain = payment_events::Listener::start(Arc::clone(&tls), move |request| {
        chain_answer(&answers, request)
    });
    let gateway = start_gateway(&redis, chain.port, true);

    let chain_id = gateway.rpc("eth_chainId", &json!([]));
    assert_eq!(chain_id["result"], CHAIN_ID, "{chain_id}");

    let resolved = gateway.rpc("px_resolveAccount", &json!([ACCOUNT]));
    assert_eq!(
        resolved["result"],
        json!({
            "evm_address": ACCOUNT,
            "pax_address": "pax1exampleaccount",
            "layerx_did": format!("did:layerx:{}", "61".repeat(32)),
            "layerx_account": "7c".repeat(32),
            "bound": true
        }),
        "{resolved}"
    );

    assert_kernel_unavailable(
        &gateway.rpc("lx_getAccount", &json!(["ab".repeat(32)])),
        "public_core",
    );
    assert_kernel_unavailable(
        &gateway.rpc("px_getBalances", &json!([ACCOUNT])),
        "public_core",
    );

    let network = gateway.rpc("px_getNetwork", &json!([]));
    assert_eq!(
        network["result"]["kernel"],
        json!({"available": false, "reason": "not_configured"}),
        "{network}"
    );
    assert_eq!(
        network["result"]["paxeer"],
        json!({"chain_id": CHAIN_ID, "latest_block": LATEST_BLOCK})
    );
    assert_eq!(network["result"]["network_id"], "paxeer-degraded");

    let (status, readiness) = gateway.get("/readyz");
    assert_eq!(status, 200, "{readiness}");
    assert_eq!(readiness["status"], "degraded");
    let backends = &readiness["backends"];
    assert_eq!(
        backends["paxeer_chain"],
        json!({"state": "ready", "reason": "ready"}),
        "{readiness}"
    );
    assert_eq!(
        backends["durable_store"],
        json!({"state": "ready", "reason": "ready"})
    );
    for kernel in [
        "core_agent_boundary",
        "independent_receipt_authority",
        "program_registry",
    ] {
        assert_eq!(
            backends[kernel],
            json!({"state": "unavailable", "reason": "not_configured"}),
            "{kernel}"
        );
    }
    drop(gateway);
    drop(chain);
}

#[test]
fn degraded_endpoint_starts_chain_only_through_its_real_configuration() {
    let _ = rustls::crypto::ring::default_provider().install_default();
    let redis = RedisProcess::start();
    let (tls, _) = payment_events::tls(&redis);
    let answers = precompile_answers();
    let chain = payment_events::Listener::start(Arc::clone(&tls), move |request| {
        chain_answer(&answers, request)
    });
    let gateway = start_gateway(&redis, chain.port, true);

    let chain_id = gateway.rpc("eth_chainId", &json!([]));
    assert_eq!(chain_id["result"], CHAIN_ID, "{chain_id}");

    let network = gateway.rpc("px_getNetwork", &json!([]));
    assert_eq!(
        network["result"]["kernel"],
        json!({"available": false, "reason": "not_configured"}),
        "{network}"
    );
    assert_eq!(
        network["result"]["paxeer"],
        json!({"chain_id": CHAIN_ID, "latest_block": LATEST_BLOCK}),
        "{network}"
    );

    assert_kernel_unavailable(
        &gateway.rpc("lx_sendActivity", &json!(["00"])),
        "core_agent_boundary",
    );

    let activity = gateway
        .client
        .post("/v1/activities", b"{}")
        .unwrap_or_else(|error| panic!("activity route must be answered: {error:?}"));
    assert_eq!(activity.status, 503);
    let refusal: Value = serde_json::from_slice(&activity.body)
        .unwrap_or_else(|error| panic!("activity refusal must be JSON: {error}"));
    assert_eq!(
        refusal,
        json!({"ok": false, "error": {"code": "kernel_unavailable", "backend": "core_agent_boundary", "reason": "not_configured"}})
    );

    let (status, readiness) = gateway.get("/readyz");
    assert_eq!(status, 200, "{readiness}");
    assert_eq!(readiness["status"], "degraded");
    for kernel in [
        "core_agent_boundary",
        "independent_receipt_authority",
        "program_registry",
    ] {
        assert_eq!(
            readiness["backends"][kernel],
            json!({"state": "unavailable", "reason": "not_configured"}),
            "{kernel}"
        );
    }
    drop(gateway);
    drop(chain);
}

#[test]
fn degraded_endpoint_refuses_a_kernel_side_input_without_the_component_url() {
    let redis = RedisProcess::start();
    let _ = payment_events::tls(&redis);
    let input = secret(&redis.directory, "kernel-side-input", &"42".repeat(32));
    for variable in KERNEL_SIDE_VARIABLES {
        let mut environment = chain_only_environment(&redis, free_port(), free_port(), true);
        environment.push((variable.to_owned(), input.clone()));
        let output = gateway_command(&environment)
            .stdout(Stdio::null())
            .output()
            .unwrap_or_else(|error| panic!("gateway must run: {error}"));
        assert_eq!(output.status.code(), Some(1), "{variable}");
        let stderr = String::from_utf8_lossy(&output.stderr);
        assert_eq!(
            stderr.trim_end(),
            format!(
                "layerx-gateway refused startup: {variable} is set without LAYERX_GATEWAY_COMPONENT_URL"
            ),
            "{variable}"
        );
    }
}

#[test]
fn degraded_endpoint_starts_without_the_event_producer_and_reports_it_unconfigured() {
    let _ = rustls::crypto::ring::default_provider().install_default();
    let redis = RedisProcess::start();
    let (tls, _) = payment_events::tls(&redis);
    let answers = precompile_answers();
    let chain = payment_events::Listener::start(Arc::clone(&tls), move |request| {
        chain_answer(&answers, request)
    });
    let environment = chain_only_environment(&redis, chain.port, free_port(), false);
    assert!(environment
        .iter()
        .all(|(name, _)| !name.starts_with("LAYERX_EVENTS_")));
    let gateway = start_gateway(&redis, chain.port, false);

    let chain_id = gateway.rpc("eth_chainId", &json!([]));
    assert_eq!(chain_id["result"], CHAIN_ID, "{chain_id}");

    let network = gateway.rpc("px_getNetwork", &json!([]));
    assert_eq!(
        network["result"]["kernel"],
        json!({"available": false, "reason": "not_configured"}),
        "{network}"
    );
    assert_eq!(
        network["result"]["paxeer"],
        json!({"chain_id": CHAIN_ID, "latest_block": LATEST_BLOCK}),
        "{network}"
    );

    assert_kernel_unavailable(
        &gateway.rpc("lx_sendActivity", &json!(["00"])),
        "core_agent_boundary",
    );

    let (status, readiness) = gateway.get("/readyz");
    assert_eq!(status, 200, "{readiness}");
    assert_eq!(readiness["status"], "degraded");
    assert_eq!(
        readiness["backends"]["event_producer"],
        json!({"state": "unavailable", "reason": "not_configured"}),
        "{readiness}"
    );
    assert_eq!(
        readiness["backends"]["durable_store"],
        json!({"state": "ready", "reason": "ready"}),
        "{readiness}"
    );
    assert_eq!(
        readiness["backends"]["paxeer_chain"],
        json!({"state": "ready", "reason": "ready"}),
        "{readiness}"
    );
    drop(gateway);
    drop(chain);
}

#[test]
fn degraded_endpoint_refuses_a_half_set_event_producer_by_name() {
    let redis = RedisProcess::start();
    let _ = payment_events::tls(&redis);
    let refusal = |environment: &[(String, String)]| {
        let output = gateway_command(environment)
            .stdout(Stdio::null())
            .output()
            .unwrap_or_else(|error| panic!("gateway must run: {error}"));
        (
            output.status.code(),
            String::from_utf8_lossy(&output.stderr)
                .trim_end()
                .to_owned(),
        )
    };
    let chain_only = chain_only_environment(&redis, free_port(), free_port(), false);
    let producer = chain_only_environment(&redis, free_port(), free_port(), true)
        .into_iter()
        .filter(|(name, _)| name.starts_with("LAYERX_EVENTS_"))
        .collect::<Vec<_>>();
    assert_eq!(producer.len(), 10);
    for entry in producer
        .iter()
        .filter(|(name, _)| !name.ends_with("_UPSTREAM_URL"))
    {
        let mut environment = chain_only.clone();
        environment.push(entry.clone());
        assert_eq!(
            refusal(&environment),
            (
                Some(1),
                format!(
                    "layerx-gateway refused startup: {} is set without LAYERX_EVENTS_PAYMENT_UPSTREAM_URL",
                    entry.0
                )
            ),
            "{}",
            entry.0
        );
    }
    for (missing, _) in &producer {
        let mut environment = chain_only.clone();
        environment.extend(producer.iter().filter(|(name, _)| name != missing).cloned());
        assert_eq!(
            refusal(&environment),
            (
                Some(1),
                format!("layerx-gateway refused startup: {missing} is required")
            ),
            "{missing}"
        );
    }
}

#[test]
fn degraded_endpoint_serves_the_chain_over_the_plain_listener() {
    let _ = rustls::crypto::ring::default_provider().install_default();
    let redis = RedisProcess::start();
    assert!(redis.store().ready());
    let (tls, _) = payment_events::tls(&redis);
    let answers = precompile_answers();
    let chain = payment_events::Listener::start(Arc::clone(&tls), move |request| {
        chain_answer(&answers, request)
    });
    let gateway = start_plain_gateway(&redis, chain.port);

    let chain_id = gateway.rpc("eth_chainId", &json!([]));
    assert_eq!(chain_id["result"], CHAIN_ID, "{chain_id}");

    let network = gateway.rpc("px_getNetwork", &json!([]));
    assert_eq!(
        network["result"]["kernel"],
        json!({"available": false, "reason": "not_configured"}),
        "{network}"
    );
    assert_eq!(
        network["result"]["paxeer"],
        json!({"chain_id": CHAIN_ID, "latest_block": LATEST_BLOCK}),
        "{network}"
    );
    assert_eq!(network["result"]["network_id"], "paxeer-degraded");

    assert_kernel_unavailable(
        &gateway.rpc("lx_sendActivity", &json!(["00"])),
        "core_agent_boundary",
    );

    let (status, readiness) = gateway.get("/readyz");
    assert_eq!(status, 200, "{readiness}");
    assert_eq!(readiness["status"], "degraded");
    assert_eq!(
        readiness["backends"]["paxeer_chain"],
        json!({"state": "ready", "reason": "ready"}),
        "{readiness}"
    );
    assert_eq!(
        readiness["backends"]["durable_store"],
        json!({"state": "ready", "reason": "ready"}),
        "{readiness}"
    );
    assert_eq!(
        readiness["backends"]["event_producer"],
        json!({"state": "unavailable", "reason": "not_configured"}),
        "{readiness}"
    );
    for kernel in [
        "core_agent_boundary",
        "independent_receipt_authority",
        "program_registry",
    ] {
        assert_eq!(
            readiness["backends"][kernel],
            json!({"state": "unavailable", "reason": "not_configured"}),
            "{kernel}"
        );
    }
    drop(gateway);
    drop(chain);
}

#[test]
fn degraded_endpoint_refuses_a_plain_listener_with_a_listener_certificate_by_name() {
    let redis = RedisProcess::start();
    let _ = payment_events::tls(&redis);
    let certificates = chain_only_environment(&redis, free_port(), free_port(), false)
        .into_iter()
        .filter(|(name, _)| LISTENER_CERTIFICATE_VARIABLES.contains(&name.as_str()))
        .collect::<Vec<_>>();
    assert_eq!(certificates.len(), 2);
    for entry in &certificates {
        let mut environment = plain_environment(&redis, free_port(), free_port());
        environment.push(entry.clone());
        assert_eq!(
            startup_refusal(&environment),
            (
                Some(1),
                format!(
                    "layerx-gateway refused startup: {} is set with LAYERX_GATEWAY_LISTENER plain",
                    entry.0
                )
            ),
            "{}",
            entry.0
        );
    }
}

#[test]
fn degraded_endpoint_refuses_an_unknown_listener_by_name_and_never_downgrades() {
    let redis = RedisProcess::start();
    let _ = payment_events::tls(&redis);
    for mode in ["https", "PLAIN", "", "plain "] {
        let mut environment = chain_only_environment(&redis, free_port(), free_port(), false);
        environment.push(("LAYERX_GATEWAY_LISTENER".to_owned(), mode.to_owned()));
        assert_eq!(
            startup_refusal(&environment),
            (
                Some(1),
                "layerx-gateway refused startup: LAYERX_GATEWAY_LISTENER must be tls or plain"
                    .to_owned()
            ),
            "{mode:?}"
        );
    }
    for mode in [None, Some("tls")] {
        let mut environment = chain_only_environment(&redis, free_port(), free_port(), false)
            .into_iter()
            .filter(|(name, _)| name != "LAYERX_GATEWAY_TLS_CERT_DER")
            .collect::<Vec<_>>();
        if let Some(mode) = mode {
            environment.push(("LAYERX_GATEWAY_LISTENER".to_owned(), mode.to_owned()));
        }
        assert_eq!(
            startup_refusal(&environment),
            (
                Some(1),
                "layerx-gateway refused startup: gateway TLS certificate is required".to_owned()
            ),
            "{mode:?}"
        );
    }
}

#[path = "support/payment_events.rs"]
mod payment_events;
