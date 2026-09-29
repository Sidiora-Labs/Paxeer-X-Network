use layerx_human_test_support as support;

use std::collections::BTreeMap;
use std::fmt::Debug;
use std::fs;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt as _;
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::Duration;

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_client::runtime_clock::RuntimeClock;
use layerx_human_identity_provider::{AssertionConfig, AssertionVerifier, Policy, Server, State};
use layerx_human_service::auth::{AuthConfig, Passkeys, RateLimit};
use layerx_human_service::server::schema::ApiSchema;
use layerx_human_service::server::{
    default_component_limits, AuthorizationGrantPolicy, BearerCredentials, ComponentServerConfig,
    ComponentShutdown, HttpConfig, HumanApiComponents, HumanComponentServer,
    IdentityProviderConfig, IdentityServices, PrincipalLimits, PrivilegedHumanComponents,
    ProvisionedAccount, ProvisionedAccounts, RemoteIdentityProvider, Router, UnixComponents,
};
use layerx_types::clock::Clock as _;
use p256::ecdsa::signature::Signer as _;
use p256::ecdsa::{Signature, SigningKey};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use support::{directory, install_and_open, retention_uniform, tenancy};

const ISSUER: &str = "https://identity.example.test/auth/v1";
const AUDIENCE: &str = "authenticated";
const SUBJECT: &str = "wallet-user-0001";
const WALLET_DID: &str =
    "did:layerx:0102030405060708091011121314151617181920212223242526272829303132";
const APP_ORIGIN: &str = "https://app.wallet.example";
const ORIGINS: &str = "https://app.wallet.example,https://wallet.example";
const FOREIGN_ORIGIN: &str = "https://other.example";
const TRACE: &str = "trc_00112233445566778899aabbccddeeff";
const PLAN_GOLDEN: &str = include_str!("../../../schema/human-api/golden/intent.plan.request.json");

fn required<T, E: Debug>(result: Result<T, E>, label: &str) -> T {
    result.unwrap_or_else(|error| panic!("{label}: {error:?}"))
}

struct KeySetServer {
    address: SocketAddr,
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl KeySetServer {
    fn serve(keys: &[Value]) -> Self {
        let listener = required(TcpListener::bind("127.0.0.1:0"), "key set listener");
        let address = required(listener.local_addr(), "key set address");
        let body = Arc::new(Mutex::new(json!({ "keys": keys }).to_string()));
        let shutdown = Arc::new(AtomicBool::new(false));
        let worker = {
            let shutdown = Arc::clone(&shutdown);
            thread::spawn(move || {
                for stream in listener.incoming() {
                    if shutdown.load(Ordering::Acquire) {
                        break;
                    }
                    if let Ok(mut stream) = stream {
                        respond(&mut stream, &body);
                    }
                }
            })
        };
        Self {
            address,
            shutdown,
            worker: Some(worker),
        }
    }

    fn url(&self) -> String {
        format!("http://{}/auth/v1/.well-known/jwks.json", self.address)
    }
}

impl Drop for KeySetServer {
    fn drop(&mut self) {
        self.shutdown.store(true, Ordering::Release);
        let _ = TcpStream::connect(self.address);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

fn respond(stream: &mut TcpStream, body: &Mutex<String>) {
    let _ = stream.set_read_timeout(Some(Duration::from_secs(5)));
    let mut request = Vec::new();
    let mut buffer = [0u8; 1024];
    while !request.windows(4).any(|window| window == b"\r\n\r\n") {
        let Ok(read) = stream.read(&mut buffer) else {
            return;
        };
        if read == 0 || request.len() > 16 * 1024 {
            return;
        }
        request.extend_from_slice(&buffer[..read]);
    }
    if !request.starts_with(b"GET /auth/v1/.well-known/jwks.json ") {
        let _ = stream
            .write_all(b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n");
        return;
    }
    let document = body
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .clone();
    let _ = write!(
        stream,
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        document.len(),
        document
    );
    let _ = stream.flush();
}

struct TokenSigner {
    key_id: String,
    key: SigningKey,
}

impl TokenSigner {
    fn generate(key_id: &str) -> Self {
        loop {
            let mut scalar = [0u8; 32];
            required(getrandom::fill(&mut scalar), "scalar entropy");
            if let Ok(key) = SigningKey::from_slice(&scalar) {
                return Self {
                    key_id: key_id.to_owned(),
                    key,
                };
            }
        }
    }

    fn jwk(&self) -> Value {
        let point = self.key.verifying_key().to_encoded_point(false);
        json!({
            "kty": "EC",
            "crv": "P-256",
            "kid": self.key_id,
            "use": "sig",
            "alg": "ES256",
            "x": URL_SAFE_NO_PAD.encode(point.x().unwrap_or_else(|| panic!("missing x"))),
            "y": URL_SAFE_NO_PAD.encode(point.y().unwrap_or_else(|| panic!("missing y"))),
        })
    }

    fn mint(&self, claims: &Value) -> String {
        let header = json!({ "alg": "ES256", "typ": "JWT", "kid": self.key_id });
        let input = format!(
            "{}.{}",
            URL_SAFE_NO_PAD.encode(header.to_string()),
            URL_SAFE_NO_PAD.encode(claims.to_string())
        );
        let signature: Signature = self.key.sign(input.as_bytes());
        format!("{input}.{}", URL_SAFE_NO_PAD.encode(signature.to_bytes()))
    }
}

fn claims(subject: &str, now: u64) -> Value {
    json!({
        "iss": ISSUER,
        "sub": subject,
        "aud": AUDIENCE,
        "exp": now + 3600,
        "iat": now,
        "nbf": now - 5,
        "role": "authenticated",
        "email": "holder@example.test",
    })
}

fn auth_config() -> AuthConfig {
    AuthConfig {
        rp_id: "paxportwallet.com".to_owned(),
        rp_name: "LayerX".to_owned(),
        origin: "https://paxportwallet.com".to_owned(),
        ceremony_ttl_secs: 300,
        assertion_ttl_secs: 60,
        session_ttl_secs: 300,
        refresh_ttl_secs: 3_600,
        step_up_ttl_secs: 60,
        rate_limit: RateLimit {
            attempts: 100,
            window_secs: 60,
        },
    }
}

fn http_config() -> HttpConfig {
    HttpConfig {
        maximum_header_bytes: 32_768,
        maximum_body_bytes: 1_048_576,
        allowed_origin: ORIGINS.to_owned(),
        service_version: "test".to_owned(),
    }
}

fn clock() -> Arc<RuntimeClock> {
    required(RuntimeClock::from_environment(), "clock authority")
}

fn private_directory(label: &str) -> PathBuf {
    let path = directory(label);
    required(fs::create_dir_all(&path), "create directory");
    required(
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)),
        "directory permissions",
    );
    path
}

fn digest_field(digest: &mut Sha256, value: &[u8]) {
    digest.update((value.len() as u64).to_be_bytes());
    digest.update(value);
}

fn authorized_request_digest(
    operation: &layerx_human_service::server::schema::Operation,
    destination: &str,
    body: &Value,
    trace: &str,
) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(b"layerx-human/authorized-operation/v1\0");
    digest_field(&mut digest, operation.name.as_bytes());
    digest_field(&mut digest, operation.method.as_bytes());
    digest_field(&mut digest, destination.as_bytes());
    let body = required(serde_json::to_vec(body), "encode authorized body");
    digest_field(&mut digest, &body);
    digest_field(&mut digest, b"");
    digest_field(&mut digest, trace.as_bytes());
    digest.finalize().into()
}

struct ProviderRun {
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<std::io::Result<()>>>,
}

impl Drop for ProviderRun {
    fn drop(&mut self) {
        self.shutdown.store(true, Ordering::Release);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

struct Fixture {
    _keys: KeySetServer,
    signer: TokenSigner,
    principal: String,
    _provider: ProviderRun,
    components: ComponentShutdown,
    router: Arc<Router<UnixComponents>>,
    client: UnixComponents,
    schema: ApiSchema,
    now: u64,
}

impl Drop for Fixture {
    fn drop(&mut self) {
        self.components.request();
    }
}

impl Fixture {
    fn start() -> Self {
        let clock = clock();
        let now = required(clock.sample(Duration::from_secs(1)), "clock sample").unix_seconds();
        let signer = TokenSigner::generate("signing-key-1");
        let keys = KeySetServer::serve(&[signer.jwk()]);
        let provider_root = private_directory("bearer-provider");
        let mut state = required(
            State::open(
                &provider_root.join("identity"),
                Policy {
                    root: [0x43; 32],
                    threshold: 1,
                    delay_seconds: 86_400,
                },
            ),
            "provider state",
        );
        required(
            state.enable_assertion(required(
                AssertionVerifier::new(AssertionConfig {
                    jwks_url: keys.url(),
                    issuer: ISSUER.to_owned(),
                    audience: AUDIENCE.to_owned(),
                    clock_skew_seconds: 30,
                    refresh_interval_seconds: 300,
                }),
                "assertion verifier",
            )),
            "enable assertion",
        );
        let token = signer.mint(&claims(SUBJECT, now));
        let (account, _) = required(
            state.open_or_create_by_assertion(&token, Some(WALLET_DID), now),
            "create account by assertion",
        );
        let principal = account.principal().to_owned();
        let socket = provider_root.join("identity.sock");
        let server = required(
            Server::bind(
                &socket,
                state,
                rustix::process::geteuid().as_raw(),
                Duration::from_secs(2),
                clock.clone(),
            ),
            "bind identity provider",
        );
        let shutdown = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&shutdown);
        let provider = ProviderRun {
            shutdown,
            worker: Some(thread::spawn(move || server.run(&flag))),
        };

        let store_root = directory("bearer-store");
        let map = tenancy(&[(principal.as_str(), "tenant-bearer")]);
        let (store, _) = install_and_open(&store_root, &map, retention_uniform(86_400));
        let components = required(
            PrivilegedHumanComponents::new(
                store,
                required(Passkeys::new(auth_config()), "passkeys"),
                IdentityServices::new(required(
                    ProvisionedAccounts::new([required(
                        ProvisionedAccount::new(
                            principal.as_str(),
                            "holder@example.test",
                            "Holder",
                            "Primary passkey",
                        ),
                        "provisioned account",
                    )]),
                    "accounts",
                )),
                AuthorizationGrantPolicy {
                    lifetime_seconds: 30,
                    maximum_outstanding: 32,
                },
                clock.clone(),
            ),
            "privileged components",
        )
        .with_identity_provider(required(
            RemoteIdentityProvider::new(IdentityProviderConfig {
                socket,
                deadline: Duration::from_secs(2),
                maximum_frame_bytes: 65_536,
                peer_uid: rustix::process::geteuid().as_raw(),
                peer_gid: rustix::process::getegid().as_raw(),
            }),
            "identity provider client",
        ));
        let component_socket = private_directory("bearer-components").join("human.sock");
        let bound = required(
            HumanComponentServer::new(Arc::new(components)).bind(ComponentServerConfig {
                socket_path: component_socket.clone(),
                allowed_uid: rustix::process::getuid().as_raw(),
                worker_count: 1,
                queue_capacity: 1,
                limits: default_component_limits(),
            }),
            "bind component server",
        );
        let component_shutdown = bound.shutdown();
        thread::spawn(move || {
            let _ = bound.run();
        });
        let client = required(
            UnixComponents::new(&component_socket, default_component_limits()),
            "component client",
        );
        let backend = Arc::new(required(
            UnixComponents::new(&component_socket, default_component_limits()),
            "router backend",
        ));
        let router = Arc::new(required(
            Router::new(
                backend,
                required(PrincipalLimits::new(100, 60, 100), "limits"),
                http_config(),
                clock,
            ),
            "router",
        ));
        Self {
            _keys: keys,
            signer,
            principal,
            _provider: provider,
            components: component_shutdown,
            router,
            client,
            schema: required(ApiSchema::v1(), "schema"),
            now,
        }
    }

    fn token(&self) -> String {
        self.signer.mint(&claims(SUBJECT, self.now))
    }

    fn expired_token(&self) -> String {
        let mut expired = claims(SUBJECT, self.now);
        expired["exp"] = json!(self.now - 120);
        self.signer.mint(&expired)
    }

    fn exchange(&self, request: &str) -> Response {
        let (mut client, mut server) = required(UnixStream::pair(), "HTTP pair");
        let shared = Arc::clone(&self.router);
        let worker = thread::spawn(move || {
            required(shared.serve_one(&mut server, "bearer-test"), "serve");
        });
        required(client.write_all(request.as_bytes()), "request");
        let mut raw = String::new();
        required(client.read_to_string(&mut raw), "response");
        required(worker.join().map_err(|_| "panicked"), "worker");
        Response::parse(&raw)
    }
}

struct Response {
    status: u16,
    body: Value,
}

impl Response {
    fn parse(raw: &str) -> Self {
        let (head, body) = raw
            .split_once("\r\n\r\n")
            .unwrap_or_else(|| panic!("HTTP body in {raw}"));
        let status_line = head.split("\r\n").next().unwrap_or_default();
        let status = status_line
            .split(' ')
            .nth(1)
            .and_then(|code| code.parse().ok())
            .unwrap_or_else(|| panic!("status in {status_line}"));
        Self {
            status,
            body: required(serde_json::from_str(body), "JSON body"),
        }
    }
}

fn plan_body() -> Value {
    let golden: Value = required(serde_json::from_str(PLAN_GOLDEN), "golden plan");
    golden["body"].clone()
}

fn plan_request(origin: Option<&str>, bearer: Option<&str>) -> String {
    let body = required(serde_json::to_string(&plan_body()), "golden body");
    let origin = origin.map_or_else(String::new, |origin| format!("Origin: {origin}\r\n"));
    let bearer = bearer.map_or_else(String::new, |token| {
        format!("Authorization: Bearer {token}\r\n")
    });
    format!(
        "POST /v1/intents/plan HTTP/1.1\r\nHost: human.wallet.example\r\n{origin}{bearer}Content-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
        body.len()
    )
}

fn balance_request(origin: Option<&str>, bearer: &str) -> String {
    let origin = origin.map_or_else(String::new, |origin| format!("Origin: {origin}\r\n"));
    format!(
        "GET /v1/account/balance HTTP/1.1\r\nHost: human.wallet.example\r\n{origin}Authorization: Bearer {bearer}\r\n\r\n"
    )
}

#[test]
fn bearer_assertion_is_admitted_through_operation_four_and_carried_on_the_context() {
    let fixture = Fixture::start();
    let token = fixture.token();
    let operation = fixture
        .schema
        .operation("intent.plan")
        .unwrap_or_else(|| panic!("intent.plan operation"));
    let body = plan_body();
    let path_parameters = BTreeMap::new();
    let disclosure_digest =
        Sha256::digest(required(serde_json::to_vec(&body), "encode disclosure")).into();
    let request_digest = authorized_request_digest(operation, "/v1/intents/plan", &body, TRACE);
    let context = required(
        fixture.client.admit_bearer_assertion(
            operation,
            BearerCredentials {
                assertion: &token,
                intended_destination: "/v1/intents/plan",
                request_digest,
                disclosure_digest,
                path_parameters: &path_parameters,
                body: &body,
                idempotency_key: None,
            },
            TRACE,
        ),
        "bearer admission",
    );
    assert_eq!(context.principal.as_str(), fixture.principal);
    assert_eq!(context.tenant.as_str(), "tenant-bearer");
    assert_eq!(context.did(), Some(WALLET_DID));
    assert_eq!(context.assertion(), Some(token.as_str()));
    assert!(context.session_id.starts_with("bearer-"));

    let response = fixture.exchange(&plan_request(Some(APP_ORIGIN), Some(&token)));
    assert_eq!(response.status, 503, "{:?}", response.body);
    assert_eq!(response.body["error"]["code"], "unavailable");

    let response = fixture.exchange(&plan_request(Some(APP_ORIGIN), None));
    assert_eq!(response.status, 401, "{:?}", response.body);
    assert_eq!(response.body["error"]["code"], "unauthenticated");
}

#[test]
fn bearer_assertion_from_a_foreign_origin_is_refused() {
    let fixture = Fixture::start();
    let token = fixture.token();
    for origin in [Some(FOREIGN_ORIGIN), None] {
        let response = fixture.exchange(&balance_request(origin, &token));
        assert_eq!(response.status, 403, "{origin:?}: {:?}", response.body);
        assert_eq!(response.body["error"]["code"], "forbidden");
    }
    let response = fixture.exchange(&balance_request(Some(APP_ORIGIN), &token));
    assert_eq!(response.status, 503, "{:?}", response.body);
    assert_eq!(response.body["error"]["code"], "unavailable");
}

#[test]
fn expired_bearer_assertion_is_refused() {
    let fixture = Fixture::start();
    let response = fixture.exchange(&plan_request(
        Some(APP_ORIGIN),
        Some(&fixture.expired_token()),
    ));
    assert_eq!(response.status, 401, "{:?}", response.body);
    assert_eq!(response.body["error"]["code"], "unauthenticated");
}
