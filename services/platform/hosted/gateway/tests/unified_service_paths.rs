use serde_json::{json, Value};
use std::fs;
use std::io::{Cursor, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use layerx_platform_gateway::http::read_request;
use layerx_platform_gateway::{unified_service_catalogue, unified_service_route};

const WALLET: &str = include_str!("../../../../../services/wallet/gateway/src/routes/wallet.ts");
const SIGN: &str = include_str!("../../../../../services/wallet/gateway/src/routes/sign.ts");
const OWNER: &str = include_str!("../../../../../services/wallet/gateway/src/index.ts");
const SDK_PROVIDER: &str = include_str!("../../../../../sdk/wallet/src/provider.ts");
const SDK_LX: &str = include_str!("../../../../../sdk/wallet/src/index.ts");
const SDK_GAS: &str = include_str!("../../../../../sdk/wallet/src/modules/gas-station.ts");
const ARCHIVE: &str = include_str!("../../../../../services/platform/relay_archive/runtime.py");

fn operation(method: &str, target: &str, source: &str) {
    let entry = unified_service_route(method, target).expect("actual unified service operation");
    assert_eq!(entry["service"], "wallet-gateway");
    assert_eq!(entry["upstream"], "wallet-gateway");
    assert_eq!(entry["authentication"], "requireAuth");
    assert_eq!(
        entry["health_predicate"],
        "service-ready-and-network-version-bound"
    );
    assert_eq!(entry["retries"], 0);
    assert_eq!(entry["proxy"], true);
    assert_eq!(entry["source"], source);
    let routes = unified_service_catalogue()["routes"]
        .as_array()
        .expect("production routes");
    assert_eq!(
        routes
            .iter()
            .filter(|route| route["method"] == method && route["path"] == entry["path"])
            .count(),
        1
    );
}

#[test]
fn real_registered_wallet_handlers_have_the_same_unified_owner() {
    assert!(OWNER.contains("app.register(walletSponsorRoutes)"));
    assert!(OWNER.contains("app.register(signRoutes)"));
    assert!(SIGN.contains("await lxApprovalRoutes(app, pool, attestors, limiter)"));
    for name in ["submit", "status"] {
        let path = format!("/v1/wallet/sponsored/{name}");
        assert!(WALLET.contains(&format!("app.post('{path}',{{preHandler:requireAuth}}")));
        operation(
            "POST",
            &path,
            "services/wallet/gateway/src/routes/wallet.ts",
        );
    }
    for path in [
        "/v1/wallet/sign-digest",
        "/v1/wallet/sign-custody",
        "/v1/wallet/lx/review",
        "/v1/wallet/lx/approve",
        "/v1/wallet/lx/sign",
    ] {
        assert!(SIGN.contains(&format!("app.post('{path}',{{preHandler:requireAuth")));
        operation("POST", path, "services/wallet/gateway/src/routes/sign.ts");
    }
    operation(
        "GET",
        &format!("/v1/wallet/custody/0x{}", "ab".repeat(32)),
        "services/wallet/gateway/src/routes/sign.ts",
    );
    operation(
        "GET",
        "/v1/wallet/lx/approvals/12345678-1234-4234-8234-123456789abc",
        "services/wallet/gateway/src/routes/sign.ts",
    );
    assert!(SIGN.contains("app.get('/v1/wallet/custody/:id',{preHandler:requireAuth"));
    assert!(SIGN.contains("app.get('/v1/wallet/lx/approvals/:id',{preHandler:requireAuth"));
}

#[test]
fn actual_sdk_callers_and_existing_ownership_checks_stay_connected() {
    for path in [
        "/v1/wallet/sign-digest",
        "/v1/wallet/sign-custody",
        "/v1/wallet/custody/",
    ] {
        assert!(SDK_PROVIDER.contains(path), "actual SDK caller {path}");
    }
    for path in [
        "/v1/wallet/lx/review",
        "/v1/wallet/lx/approve",
        "/v1/wallet/lx/sign",
        "/v1/wallet/lx/approvals/",
    ] {
        assert!(SDK_LX.contains(path), "actual retained LX caller {path}");
    }
    assert!(SDK_GAS.contains("/v1/wallet/sponsored/status"));
    assert!(SDK_GAS.contains("/v1/wallet/sponsored/submit"));
    assert!(WALLET.contains("wallet.address.toLowerCase()!==account.toLowerCase()"));
    assert!(SIGN.contains("where id=$1 and user_id=$2"));
    assert!(SIGN.contains("where id=$1 and principal=$2 for update"));
    assert!(SIGN.contains("explicit_lx_approval_required"));
    assert!(SIGN.contains("attestor_custody_required"));
}

#[test]
fn newly_connected_financial_routes_are_closed_to_methods_queries_and_path_aliases() {
    let custody = format!("/v1/wallet/custody/0x{}", "ab".repeat(32));
    for (method, path) in [
        ("GET", "/v1/wallet/sign-digest"),
        ("PUT", "/v1/wallet/lx/approve"),
        ("DELETE", "/v1/wallet/sponsored/submit"),
        ("POST", custody.as_str()),
        ("GET", "/v1/wallet/lx/approvals/not-a-uuid"),
        ("GET", "/v1/wallet/custody/0x11"),
        ("POST", "/v1/wallet/lx/sign?tenant=other"),
        ("POST", "/v1/wallet/lx/sign/"),
        ("POST", "/v1/wallet/lx/%73ign"),
        ("POST", "/v1/wallet/lx/../sign"),
        ("POST", "/v1/wallet/sponsored/unknown"),
        (
            "GET",
            "/v1/wallet/lx/approvals/12345678-1234-4234-8234-123456789abc?principal=other",
        ),
        ("GET", "/v1/sync/readiness?ready=true"),
        ("POST", "/v1/sync/readiness"),
        ("POST", "/internal/v1/wallet/sign-digest"),
        ("POST", "/v1/wallet/sign-digest?x=%0d%0aHost:other"),
    ] {
        assert!(
            unified_service_route(method, path).is_none(),
            "closed {method} {path}"
        );
    }
}

#[test]
fn real_ingress_retains_the_principal_credential_and_exact_financial_body() {
    let body = br#"{"approval_id":"12345678-1234-4234-8234-123456789abc"}"#;
    let head = format!("POST /v1/wallet/lx/sign HTTP/1.1\r\nHost: api-mainnet-beta.paxeer.network\r\nAuthorization: Bearer principal-session\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n", body.len());
    let wire = [head.as_bytes(), body.as_slice()].concat();
    let request = read_request(&mut Cursor::new(wire), 4096).expect("real production ingress");
    assert_eq!(
        request.headers.get("authorization").map(String::as_str),
        Some("Bearer principal-session")
    );
    assert_eq!(request.body, body);
    operation(
        &request.method,
        &request.path,
        "services/wallet/gateway/src/routes/sign.ts",
    );
}

#[test]
fn archive_sync_readiness_reaches_the_actual_sync_predicate() {
    let entry =
        unified_service_route("GET", "/v1/sync/readiness").expect("actual archive sync readiness");
    assert_eq!(entry["service"], "archive");
    assert_eq!(entry["upstream"], "archive");
    assert_eq!(entry["authentication"], "public");
    assert_eq!(
        entry["source"],
        "services/platform/relay_archive/runtime.py"
    );
    assert!(ARCHIVE.contains("if path == \"/v1/sync/readiness\":"));
    assert!(ARCHIVE.contains("value = self.readiness()"));
    assert!(unified_service_route("GET", "/v1/sync/network").is_some());
    assert!(unified_service_route("GET", "/v1/history/activities?limit=10&cursor=0").is_some());
}

fn run(program: &str, arguments: &[&str]) {
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

fn text(value: &Path) -> &str {
    value
        .to_str()
        .unwrap_or_else(|| panic!("test path must be UTF-8"))
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .unwrap_or_else(|error| panic!("test port must be allocated: {error}"))
        .port()
}

/// The real gateway binary over a real TLS Redis, answering JSON-RPC on a
/// plain loopback listener with no kernel, indexer or Paxeer upstream.
struct Gateway {
    gateway: Child,
    redis: Child,
    directory: PathBuf,
    port: u16,
}

impl Drop for Gateway {
    fn drop(&mut self) {
        let _ = self.gateway.kill();
        let _ = self.gateway.wait();
        let _ = self.redis.kill();
        let _ = self.redis.wait();
        let _ = fs::remove_dir_all(&self.directory);
    }
}

impl Gateway {
    fn start() -> Self {
        let directory = std::env::temp_dir().join(format!(
            "layerx-unified-paths-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        ));
        fs::create_dir(&directory)
            .unwrap_or_else(|error| panic!("test directory must be created: {error}"));
        let file = |name: &str| directory.join(name);
        run(
            "openssl",
            &[
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                text(&file("server.key")),
                "-out",
                text(&file("server.pem")),
                "-days",
                "1",
                "-subj",
                "/CN=localhost",
                "-addext",
                "subjectAltName=DNS:localhost",
            ],
        );
        run(
            "openssl",
            &[
                "x509",
                "-in",
                text(&file("server.pem")),
                "-outform",
                "DER",
                "-out",
                text(&file("server.der")),
            ],
        );
        let redis_port = free_port();
        fs::write(
            file("users.acl"),
            "user default off\nuser gateway on >gateway-secret ~* &* +@all\n",
        )
        .unwrap_or_else(|error| panic!("test Redis ACL must be written: {error}"));
        fs::write(
            file("redis.conf"),
            format!(
                "bind 127.0.0.1\nport 0\ntls-port {redis_port}\ntls-cert-file {}\ntls-key-file {}\ntls-ca-cert-file {}\ntls-auth-clients no\naclfile {}\ndir {}\nprotected-mode yes\n",
                text(&file("server.pem")),
                text(&file("server.key")),
                text(&file("server.pem")),
                text(&file("users.acl")),
                text(&directory),
            ),
        )
        .unwrap_or_else(|error| panic!("test Redis config must be written: {error}"));
        let redis = Command::new("redis-server")
            .arg(file("redis.conf"))
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("real Redis server must start: {error}"));
        for (name, value) in [
            ("redis-username", "gateway"),
            ("redis-password", "gateway-secret"),
        ] {
            fs::write(file(name), value)
                .unwrap_or_else(|error| panic!("{name} must be written: {error}"));
        }
        let port = free_port();
        let gateway = Command::new(env!("CARGO_BIN_EXE_layerx-gateway"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap_or_default())
            .env("LAYERX_GATEWAY_LISTEN", format!("127.0.0.1:{port}"))
            .env("LAYERX_GATEWAY_LISTENER", "plain")
            .env("LAYERX_GATEWAY_OUTBOUND_CA_DER", file("server.der"))
            .env("LAYERX_GATEWAY_NETWORK_ID", "unified-service-paths")
            .env(
                "LAYERX_GATEWAY_LXP_WIRE_VERSION",
                layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION.to_string(),
            )
            .env("LAYERX_GATEWAY_PROTOCOL_NETWORK_ID", "7")
            .env(
                "LAYERX_GATEWAY_REDIS_URL",
                format!("rediss://localhost:{redis_port}"),
            )
            .env("LAYERX_GATEWAY_REDIS_USERNAME_FILE", file("redis-username"))
            .env("LAYERX_GATEWAY_REDIS_PASSWORD_FILE", file("redis-password"))
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("gateway must start: {error}"));
        let mut started = Self {
            gateway,
            redis,
            directory,
            port,
        };
        for _ in 0..200 {
            if let Ok(Some(status)) = started.gateway.try_wait() {
                panic!("gateway refused start-up: {status}");
            }
            if started
                .exchange("GET", "/livez", b"")
                .is_ok_and(|(status, _)| status == 200)
            {
                return started;
            }
            thread::sleep(Duration::from_millis(50));
        }
        panic!("gateway did not become live")
    }

    fn exchange(&self, method: &str, path: &str, body: &[u8]) -> Result<(u16, Vec<u8>), String> {
        let mut stream =
            TcpStream::connect(("127.0.0.1", self.port)).map_err(|error| error.to_string())?;
        stream
            .set_read_timeout(Some(Duration::from_secs(30)))
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
        let end = answer
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| "answer has no header terminator".to_owned())?;
        let head = String::from_utf8_lossy(&answer[..end]).into_owned();
        let status = head
            .split_whitespace()
            .nth(1)
            .and_then(|value| value.parse::<u16>().ok())
            .ok_or_else(|| format!("answer status is invalid: {head}"))?;
        Ok((status, answer[end + 4..].to_vec()))
    }

    fn call(&self, method: &str, params: &Value) -> Value {
        let request = json!({"jsonrpc": "2.0", "id": 9, "method": method, "params": params});
        let (status, body) = self
            .exchange("POST", "/", request.to_string().as_bytes())
            .unwrap_or_else(|error| panic!("{method} must be answered: {error}"));
        assert_eq!(status, 200, "{method}");
        let answer: Value = serde_json::from_slice(&body)
            .unwrap_or_else(|error| panic!("{method} answer must be JSON: {error}"));
        assert_eq!(answer["id"], 9, "{answer}");
        answer
    }
}

const ACCOUNT: &str = "0x00000000000000000000000000000000000000ab";

#[test]
fn register_replaced() {
    let gateway = Gateway::start();
    let signer = "ab".repeat(32);
    let signature = "cd".repeat(64);
    for params in [
        json!([signer, signature]),
        json!([signer, signature, "someone@example.com"]),
        json!([]),
    ] {
        let answer = gateway.call("lx_register", &params);
        assert!(answer.get("result").is_none(), "{answer}");
        assert_eq!(answer["error"]["code"], -32601, "{answer}");
    }
    let served = gateway.call("px_register", &json!([signer, signature]));
    assert!(served.get("result").is_none(), "{served}");
    assert_eq!(served["error"]["code"], -32002, "{served}");
    let (status, schema) = gateway
        .exchange("GET", "/rpc/schema", b"")
        .unwrap_or_else(|error| panic!("schema must be answered: {error}"));
    assert_eq!(status, 200);
    let schema: Value =
        serde_json::from_slice(&schema).unwrap_or_else(|error| panic!("schema JSON: {error}"));
    let names: Vec<&str> = schema["methods"]
        .as_array()
        .unwrap_or_else(|| panic!("schema methods"))
        .iter()
        .filter_map(|method| method["name"].as_str())
        .collect();
    assert!(!names.contains(&"lx_register"), "{names:?}");
    assert!(!names.contains(&"lx_getHistory"), "{names:?}");
    assert!(names.contains(&"px_getHistory"), "{names:?}");
    assert!(names.contains(&"px_register"), "{names:?}");
}

#[test]
fn lx_reads_switched_off() {
    let gateway = Gateway::start();
    let key = "ab".repeat(32);
    let did = format!("did:layerx:{key}");
    for (method, params) in [
        ("lx_getHistory", json!([key])),
        ("lx_getHistory", json!([ACCOUNT])),
        ("lx_getSequence", json!([did, "identity"])),
        ("lx_getAccount", json!([key])),
        ("lx_getBalance", json!([key])),
        ("lx_getBalances", json!([did])),
        ("lx_getReceipt", json!([key])),
        ("lx_getActivityStatus", json!([key])),
        ("lx_getBatchHeader", json!(["1"])),
        ("lx_getCheckpoint", json!([key])),
        ("lx_getProof", json!(["activity", key])),
        ("lx_listAssets", json!([])),
        ("lx_getAsset", json!([key])),
        ("lx_getNodeInfo", json!([])),
        ("lx_requestFunds", json!([key])),
    ] {
        let answer = gateway.call(method, &params);
        assert!(answer.get("result").is_none(), "{method} {answer}");
        assert_eq!(answer["error"]["code"], -32601, "{method} {answer}");
    }
    for method in ["px_getHistory", "px_getUnifiedHistory"] {
        let answer = gateway.call(method, &json!([ACCOUNT]));
        assert_ne!(answer["error"]["code"], -32601, "{method} {answer}");
        assert_ne!(answer["error"]["code"], -32602, "{method} {answer}");
    }
}
