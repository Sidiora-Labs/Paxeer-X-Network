use layerx_platform_internal::http;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
use rustls::{ServerConfig, ServerConnection, StreamOwned};
use serde_json::{json, Value};
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const CHAIN_ID: &str = "0x7d0";
const RPC_URLS: &str = "LAYERX_GATEWAY_PAXEER_RPC_URLS";

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
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .unwrap_or_else(|error| panic!("test port must be allocated: {error}"))
        .port()
}

/// A real TLS Redis for the gateway's durable store, and the localhost
/// certificate every TLS peer of the test presents.
struct Fixture {
    redis: Child,
    directory: PathBuf,
    redis_port: u16,
    tls: Arc<ServerConfig>,
}

impl Fixture {
    fn start() -> Self {
        let _ = rustls::crypto::ring::default_provider().install_default();
        let directory = std::env::temp_dir().join(format!(
            "layerx-paxeer-failover-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        ));
        fs::create_dir(&directory)
            .unwrap_or_else(|error| panic!("test directory must be created: {error}"));
        let pem = directory.join("server.pem");
        let der = directory.join("server.der");
        let key = directory.join("server.key");
        let key_der = directory.join("server-key.der");
        command(
            "openssl",
            &[
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                path(&key),
                "-out",
                path(&pem),
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
                path(&pem),
                "-outform",
                "DER",
                "-out",
                path(&der),
            ],
        );
        command(
            "openssl",
            &[
                "pkcs8",
                "-topk8",
                "-nocrypt",
                "-in",
                path(&key),
                "-outform",
                "DER",
                "-out",
                path(&key_der),
            ],
        );
        let redis_port = free_port();
        fs::write(
            directory.join("users.acl"),
            "user default off\nuser gateway on >gateway-secret ~* &* +@all\n",
        )
        .unwrap_or_else(|error| panic!("test Redis ACL must be written: {error}"));
        let config = directory.join("redis.conf");
        fs::write(
            &config,
            format!(
                "bind 127.0.0.1\nport 0\ntls-port {redis_port}\ntls-cert-file {}\ntls-key-file {}\ntls-ca-cert-file {}\ntls-auth-clients no\naclfile {}\nappendonly yes\nappendfsync always\ndir {}\nprotected-mode yes\n",
                path(&pem),
                path(&key),
                path(&pem),
                path(&directory.join("users.acl")),
                path(&directory),
            ),
        )
        .unwrap_or_else(|error| panic!("test Redis config must be written: {error}"));
        let redis = Command::new("redis-server")
            .arg(&config)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("real Redis server must start: {error}"));
        let certificate = CertificateDer::from(
            fs::read(&der).unwrap_or_else(|error| panic!("certificate must be read: {error}")),
        );
        let private_key = PrivateKeyDer::from(PrivatePkcs8KeyDer::from(
            fs::read(&key_der).unwrap_or_else(|error| panic!("key must be read: {error}")),
        ));
        let tls = Arc::new(
            ServerConfig::builder()
                .with_no_client_auth()
                .with_single_cert(vec![certificate], private_key)
                .unwrap_or_else(|error| panic!("listener TLS must build: {error}")),
        );
        let fixture = Self {
            redis,
            directory,
            redis_port,
            tls,
        };
        for _ in 0..100 {
            if TcpStream::connect(("127.0.0.1", redis_port)).is_ok() {
                return fixture;
            }
            thread::sleep(Duration::from_millis(20));
        }
        panic!("real Redis server did not become reachable")
    }

    fn secret(&self, name: &str, value: &str) -> String {
        let file = self.directory.join(name);
        fs::write(&file, value).unwrap_or_else(|error| panic!("{name} must be written: {error}"));
        path(&file).to_owned()
    }

    fn environment(&self, listen: u16, urls: &str) -> Vec<(String, String)> {
        let ca = path(&self.directory.join("server.der")).to_owned();
        vec![
            (
                "LAYERX_GATEWAY_LISTEN".to_owned(),
                format!("127.0.0.1:{listen}"),
            ),
            ("LAYERX_GATEWAY_LISTENER".to_owned(), "plain".to_owned()),
            ("LAYERX_GATEWAY_OUTBOUND_CA_DER".to_owned(), ca),
            (
                "LAYERX_GATEWAY_NETWORK_ID".to_owned(),
                "paxeer-failover".to_owned(),
            ),
            (
                "LAYERX_GATEWAY_LXP_WIRE_VERSION".to_owned(),
                layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION.to_string(),
            ),
            (
                "LAYERX_GATEWAY_PROTOCOL_NETWORK_ID".to_owned(),
                "7".to_owned(),
            ),
            (RPC_URLS.to_owned(), urls.to_owned()),
            (
                "LAYERX_GATEWAY_REDIS_URL".to_owned(),
                format!("rediss://localhost:{}", self.redis_port),
            ),
            (
                "LAYERX_GATEWAY_REDIS_USERNAME_FILE".to_owned(),
                self.secret("redis-username", "gateway"),
            ),
            (
                "LAYERX_GATEWAY_REDIS_PASSWORD_FILE".to_owned(),
                self.secret("redis-password", "gateway-secret"),
            ),
        ]
    }
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.redis.kill();
        let _ = self.redis.wait();
        let _ = fs::remove_dir_all(&self.directory);
    }
}

fn gateway_command(environment: &[(String, String)]) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_layerx-gateway"));
    command
        .env_clear()
        .env("PATH", std::env::var_os("PATH").unwrap_or_default())
        .envs(environment.iter().map(|(name, value)| (name, value)));
    command
}

/// One local JSON-RPC upstream over TLS that either answers as a Paxeer
/// node or answers 502, and counts the requests it received.
struct Upstream {
    port: u16,
    failing: Arc<AtomicBool>,
    hits: Arc<AtomicUsize>,
    stop: Arc<AtomicBool>,
    join: Option<JoinHandle<()>>,
}

impl Upstream {
    fn start(tls: &Arc<ServerConfig>, failing: bool) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let port = listener
            .local_addr()
            .unwrap_or_else(|error| panic!("{error}"))
            .port();
        listener
            .set_nonblocking(true)
            .unwrap_or_else(|error| panic!("{error}"));
        let failing = Arc::new(AtomicBool::new(failing));
        let hits = Arc::new(AtomicUsize::new(0));
        let stop = Arc::new(AtomicBool::new(false));
        let (tls, fail, count, stopped) = (
            Arc::clone(tls),
            Arc::clone(&failing),
            Arc::clone(&hits),
            Arc::clone(&stop),
        );
        let join = thread::spawn(move || {
            while !stopped.load(Ordering::Acquire) {
                match listener.accept() {
                    Ok((tcp, _)) => {
                        let _ = tcp.set_nonblocking(false);
                        let _ = tcp.set_read_timeout(Some(Duration::from_secs(3)));
                        let _ = tcp.set_write_timeout(Some(Duration::from_secs(3)));
                        let Ok(connection) = ServerConnection::new(Arc::clone(&tls)) else {
                            continue;
                        };
                        let mut stream = StreamOwned::new(connection, tcp);
                        if let Ok(request) = http::parse_client_request(&mut stream) {
                            count.fetch_add(1, Ordering::AcqRel);
                            let response = if fail.load(Ordering::Acquire) {
                                http::refusal(502, "bad_gateway", None)
                            } else {
                                chain_answer(&request)
                            };
                            let _ = http::write_response(&mut stream, &response);
                        }
                    }
                    Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(5));
                    }
                    Err(error) => panic!("{error}"),
                }
            }
        });
        Self {
            port,
            failing,
            hits,
            stop,
            join: Some(join),
        }
    }

    fn url(&self) -> String {
        format!("https://localhost:{}", self.port)
    }

    fn fail(&self, failing: bool) {
        self.failing.store(failing, Ordering::Release);
    }

    fn hits(&self) -> usize {
        self.hits.load(Ordering::Acquire)
    }
}

impl Drop for Upstream {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Release);
        if let Some(join) = self.join.take() {
            let _ = join.join();
        }
    }
}

fn chain_answer(request: &http::Request) -> http::Response {
    let Ok(call) = serde_json::from_slice::<Value>(&request.body) else {
        return http::refusal(400, "invalid_json", None);
    };
    let id = call["id"].clone();
    match call["method"].as_str() {
        Some("eth_chainId") => http::json(
            200,
            &json!({"jsonrpc": "2.0", "id": id, "result": CHAIN_ID}),
        ),
        _ => http::json(
            200,
            &json!({"jsonrpc": "2.0", "id": id, "error": {"code": -32601, "message": "method not found"}}),
        ),
    }
}

struct Gateway {
    child: Child,
    port: u16,
}

impl Drop for Gateway {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Gateway {
    fn start(fixture: &Fixture, urls: &[String]) -> Self {
        let port = free_port();
        let child = gateway_command(&fixture.environment(port, &json!(urls).to_string()))
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("gateway must start: {error}"));
        let mut gateway = Self { child, port };
        for _ in 0..200 {
            if let Ok(Some(status)) = gateway.child.try_wait() {
                panic!("gateway refused start-up: {status}");
            }
            if gateway
                .exchange("GET", "/livez", b"")
                .is_ok_and(|(status, _)| status == 200)
            {
                return gateway;
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
        let header_end = answer
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| "answer has no header terminator".to_owned())?;
        let head = String::from_utf8_lossy(&answer[..header_end]).into_owned();
        let status = head
            .split_whitespace()
            .nth(1)
            .and_then(|value| value.parse::<u16>().ok())
            .ok_or_else(|| format!("answer status is invalid: {head}"))?;
        Ok((status, answer[header_end + 4..].to_vec()))
    }

    fn chain_id(&self) -> Value {
        let request = json!({"jsonrpc": "2.0", "id": 7, "method": "eth_chainId", "params": []});
        let (status, body) = self
            .exchange("POST", "/rpc", request.to_string().as_bytes())
            .unwrap_or_else(|error| panic!("eth_chainId must be answered: {error}"));
        assert_eq!(status, 200);
        let document: Value = serde_json::from_slice(&body)
            .unwrap_or_else(|error| panic!("eth_chainId answer must be JSON: {error}"));
        assert_eq!(document["id"], 7, "{document}");
        document
    }

    fn paxeer_chain(&self) -> Value {
        let (_, body) = self
            .exchange("GET", "/readyz", b"")
            .unwrap_or_else(|error| panic!("/readyz must be answered: {error}"));
        let readiness: Value = serde_json::from_slice(&body)
            .unwrap_or_else(|error| panic!("/readyz answer must be JSON: {error}"));
        readiness["backends"]["paxeer_chain"].clone()
    }

    fn assert_up(&self) {
        assert_eq!(self.chain_id()["result"], CHAIN_ID);
        assert_eq!(
            self.paxeer_chain(),
            json!({"state": "ready", "reason": "ready"})
        );
    }

    fn assert_down(&self, code: &str) {
        let answer = self.chain_id();
        assert!(answer.get("result").is_none(), "{answer}");
        assert_eq!(answer["error"]["data"]["code"], code, "{answer}");
        assert_eq!(
            self.paxeer_chain(),
            json!({"state": "unavailable", "reason": "unreachable"})
        );
    }
}

#[test]
fn a_failing_first_rpc_name_fails_over_and_the_chain_is_down_only_when_both_fail() {
    let fixture = Fixture::start();

    let bad_gateway = Upstream::start(&fixture.tls, true);
    let second = Upstream::start(&fixture.tls, false);
    let gateway = Gateway::start(&fixture, &[bad_gateway.url(), second.url()]);
    gateway.assert_up();
    assert!(bad_gateway.hits() > 0, "the first name must be tried first");
    assert!(second.hits() > 0, "the second name must answer");
    second.fail(true);
    gateway.assert_down("paxeer_node_unavailable");
    bad_gateway.fail(false);
    gateway.assert_up();
    drop(gateway);

    let refusing = format!("https://localhost:{}", free_port());
    let answering = Upstream::start(&fixture.tls, false);
    let gateway = Gateway::start(&fixture, &[refusing, answering.url()]);
    gateway.assert_up();
    answering.fail(true);
    gateway.assert_down("paxeer_node_unavailable");
    drop(gateway);

    let gateway = Gateway::start(
        &fixture,
        &[
            format!("https://localhost:{}", free_port()),
            format!("https://localhost:{}", free_port()),
        ],
    );
    gateway.assert_down("paxeer_unreachable");
}

#[test]
fn an_rpc_name_array_outside_two_to_eight_https_urls_refuses_startup() {
    let fixture = Fixture::start();
    let url = |index: u16| format!("https://localhost:{}", 20_000 + index);
    let count = "LAYERX_GATEWAY_PAXEER_RPC_URLS must hold two to eight Paxeer RPC urls";
    let cases = [
        (json!([]), count.to_owned()),
        (json!([url(1)]), count.to_owned()),
        (
            json!((1..=9).map(url).collect::<Vec<_>>()),
            count.to_owned(),
        ),
        (
            json!([format!("http://localhost:{}", 20_001), url(2)]),
            "LAYERX_GATEWAY_PAXEER_RPC_URLS entry is invalid: component endpoint must use HTTPS"
                .to_owned(),
        ),
    ];
    for (urls, reason) in cases {
        let output = gateway_command(&fixture.environment(free_port(), &urls.to_string()))
            .stdout(Stdio::null())
            .output()
            .unwrap_or_else(|error| panic!("gateway must run: {error}"));
        assert_eq!(output.status.code(), Some(1), "{urls}");
        assert_eq!(
            String::from_utf8_lossy(&output.stderr).trim_end(),
            format!("layerx-gateway refused startup: {reason}"),
            "{urls}"
        );
    }
}
