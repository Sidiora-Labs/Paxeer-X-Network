use serde_json::Value;
use std::fs;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const BINARY: &str = env!("CARGO_BIN_EXE_layerx-webhooks");
const LISTENER_CERTIFICATE_VARIABLES: [&str; 2] = [
    "LAYERX_WEBHOOKS_TLS_CERT_DER",
    "LAYERX_WEBHOOKS_TLS_KEY_DER",
];

struct Material {
    directory: PathBuf,
}

impl Drop for Material {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.directory);
    }
}

struct Service {
    child: Child,
}

impl Drop for Service {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn path(value: &Path) -> String {
    value
        .to_str()
        .unwrap_or_else(|| panic!("test path must be UTF-8"))
        .to_owned()
}

fn run(program: &str, arguments: &[&str]) {
    let output = Command::new(program)
        .args(arguments)
        .output()
        .unwrap_or_else(|error| panic!("{program} must run: {error}"));
    assert!(
        output.status.success(),
        "{program} {arguments:?} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .map(|address| address.port())
        .unwrap_or_else(|error| panic!("test port must be allocated: {error}"))
}

fn material(label: &str) -> Material {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |duration| duration.as_nanos());
    let directory = std::env::temp_dir().join(format!(
        "layerx-webhooks-{label}-{}-{nanos}",
        std::process::id()
    ));
    fs::create_dir_all(&directory)
        .unwrap_or_else(|error| panic!("test directory must be created: {error}"));
    let key = path(&directory.join("server-key.pem"));
    let certificate = path(&directory.join("server.pem"));
    run(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            &key,
            "-out",
            &certificate,
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
        ],
    );
    run(
        "openssl",
        &[
            "x509",
            "-in",
            &certificate,
            "-outform",
            "DER",
            "-out",
            &path(&directory.join("server.der")),
        ],
    );
    run(
        "openssl",
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            &key,
            "-outform",
            "DER",
            "-out",
            &path(&directory.join("server-key.der")),
        ],
    );
    run(
        "openssl",
        &[
            "pkcs12",
            "-export",
            "-inkey",
            &key,
            "-in",
            &certificate,
            "-out",
            &path(&directory.join("client.p12")),
            "-passout",
            "pass:integration-only",
        ],
    );
    Material { directory }
}

fn secret(directory: &Path, name: &str, value: &str) -> String {
    let file = directory.join(name);
    fs::write(&file, value).unwrap_or_else(|error| panic!("{name} must be written: {error}"));
    path(&file)
}

fn environment(material: &Material, listen: u16) -> Vec<(String, String)> {
    let directory = &material.directory;
    let ca = path(&directory.join("server.der"));
    let unused = free_port();
    let mut environment: Vec<(String, String)> = [
        ("LAYERX_WEBHOOKS_LISTEN", format!("127.0.0.1:{listen}")),
        ("LAYERX_WEBHOOKS_TLS_CERT_DER", ca.clone()),
        (
            "LAYERX_WEBHOOKS_TLS_KEY_DER",
            path(&directory.join("server-key.der")),
        ),
        ("LAYERX_WEBHOOKS_INTERNAL_CA_DER", ca.clone()),
        ("LAYERX_WEBHOOKS_PUBLIC_CA_DER", ca),
        (
            "LAYERX_WEBHOOKS_CLIENT_IDENTITY_PKCS12",
            path(&directory.join("client.p12")),
        ),
        (
            "LAYERX_WEBHOOKS_CLIENT_IDENTITY_PASSWORD_FILE",
            secret(directory, "identity-password", "integration-only"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_URL",
            format!("rediss://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_USERNAME_FILE",
            secret(directory, "redis-username", "webhooks"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_PASSWORD_FILE",
            secret(directory, "redis-password", "webhooks-secret"),
        ),
        (
            "LAYERX_WEBHOOKS_CURSOR_KEY_FILE",
            secret(directory, "cursor-key", &"11".repeat(32)),
        ),
        (
            "LAYERX_WEBHOOKS_KMS_URL",
            format!("https://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_KMS_TOKEN_FILE",
            secret(directory, "kms-token", "kms-token"),
        ),
        (
            "LAYERX_WEBHOOKS_INSTANCE_ID",
            "webhooks-plain-listener".to_owned(),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_PUBLIC_KEY_FILE",
            secret(
                directory,
                "sequencer-public-key",
                &format!("58{}", "66".repeat(31)),
            ),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_ID_FILE",
            secret(directory, "sequencer-id", &"22".repeat(32)),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_FIRST_BATCH_FILE",
            secret(directory, "sequencer-first-batch", "1"),
        ),
        (
            "LAYERX_WEBHOOKS_SEQUENCER_LAST_BATCH_FILE",
            secret(directory, "sequencer-last-batch", &u64::MAX.to_string()),
        ),
        ("LAYERX_WEBHOOKS_LXP_WIRE_VERSION", "3".to_owned()),
        (
            "LAYERX_WEBHOOKS_NETWORK_ID",
            "paxeer-plain-listener".to_owned(),
        ),
        (
            "LAYERX_WEBHOOKS_COMPONENT_URL",
            format!("https://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_COMPONENT_TOKEN_FILE",
            secret(directory, "component-token", "component-token"),
        ),
        (
            "LAYERX_WEBHOOKS_AUTHORITY_URL",
            format!("https://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_AUTHORITY_TOKEN_FILE",
            secret(directory, "authority-token", "authority-token"),
        ),
        (
            "LAYERX_WEBHOOKS_IDENTITY_URL",
            format!("https://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE",
            secret(directory, "identity-token", "identity-token"),
        ),
        ("LAYERX_WEBHOOKS_ROLE", "public".to_owned()),
    ]
    .into_iter()
    .map(|(name, value)| (name.to_owned(), value))
    .collect();
    for stem in ["JOURNEY", "PAYMENT", "APPROVAL", "PROGRAM"] {
        environment.push((
            format!("LAYERX_WEBHOOKS_{stem}_SOURCE_URL"),
            format!("https://localhost:{unused}"),
        ));
        environment.push((
            format!("LAYERX_WEBHOOKS_{stem}_SOURCE_TOKEN_FILE"),
            secret(directory, "source-token", "source-token"),
        ));
    }
    environment
}

fn command(environment: &[(String, String)]) -> Command {
    let mut command = Command::new(BINARY);
    command
        .env_clear()
        .envs(environment.iter().map(|(name, value)| (name, value)))
        .stdin(Stdio::null());
    command
}

fn start(environment: &[(String, String)]) -> Service {
    Service {
        child: command(environment)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("layerx-webhooks must start: {error}")),
    }
}

fn readiness(service: &mut Service, arguments: &[&str]) -> (u16, Value) {
    for _ in 0..100 {
        if let Ok(Some(status)) = service.child.try_wait() {
            panic!("layerx-webhooks exited before answering: {status}");
        }
        let output = Command::new("curl")
            .args(["-sS", "--max-time", "5", "-w", "\n%{http_code}"])
            .args(arguments)
            .output()
            .unwrap_or_else(|error| panic!("curl must run: {error}"));
        if output.status.success() {
            let answer = String::from_utf8(output.stdout)
                .unwrap_or_else(|error| panic!("readiness answer must be UTF-8: {error}"));
            let (body, status) = answer
                .rsplit_once('\n')
                .unwrap_or_else(|| panic!("readiness answer must carry a status: {answer}"));
            let status = status
                .parse::<u16>()
                .unwrap_or_else(|error| panic!("readiness status must parse: {error}"));
            let document = serde_json::from_str(body)
                .unwrap_or_else(|error| panic!("readiness answer must be JSON: {error}"));
            return (status, document);
        }
        thread::sleep(Duration::from_millis(100));
    }
    panic!("layerx-webhooks never answered readiness with {arguments:?}")
}

fn assert_readiness((status, document): (u16, Value)) {
    let ready = document["ready"]
        .as_bool()
        .unwrap_or_else(|| panic!("readiness must report ready: {document}"));
    assert_eq!(status, if ready { 200 } else { 503 }, "{document}");
}

#[test]
fn plain_listener_answers_readiness_over_plain_http() {
    let material = material("plain");
    let port = free_port();
    let mut environment: Vec<_> = environment(&material, port)
        .into_iter()
        .filter(|(name, _)| !LISTENER_CERTIFICATE_VARIABLES.contains(&name.as_str()))
        .collect();
    environment.push(("LAYERX_WEBHOOKS_LISTENER".to_owned(), "plain".to_owned()));
    let mut service = start(&environment);
    assert_readiness(readiness(
        &mut service,
        &[&format!("http://127.0.0.1:{port}/healthz")],
    ));
}

#[test]
fn tls_listener_stays_the_default_and_answers_readiness_over_tls() {
    let material = material("tls");
    let ca = path(&material.directory.join("server.pem"));
    for mode in [None, Some("tls")] {
        let port = free_port();
        let mut environment = environment(&material, port);
        if let Some(mode) = mode {
            environment.push(("LAYERX_WEBHOOKS_LISTENER".to_owned(), mode.to_owned()));
        }
        let mut service = start(&environment);
        assert_readiness(readiness(
            &mut service,
            &[
                "--cacert",
                &ca,
                &format!("https://localhost:{port}/healthz"),
            ],
        ));
    }
}

#[test]
fn plain_listener_with_tls_material_refuses_to_start() {
    let material = material("refusal");
    for variable in LISTENER_CERTIFICATE_VARIABLES {
        let mut environment: Vec<_> = environment(&material, free_port())
            .into_iter()
            .filter(|(name, _)| {
                name == variable || !LISTENER_CERTIFICATE_VARIABLES.contains(&name.as_str())
            })
            .collect();
        environment.push(("LAYERX_WEBHOOKS_LISTENER".to_owned(), "plain".to_owned()));
        let output = command(&environment)
            .output()
            .unwrap_or_else(|error| panic!("layerx-webhooks must run: {error}"));
        assert_eq!(output.status.code(), Some(2));
        assert_eq!(
            String::from_utf8_lossy(&output.stderr).trim_end(),
            format!("layerx-webhooks: {variable} is set with LAYERX_WEBHOOKS_LISTENER plain")
        );
    }
    let mut environment = environment(&material, free_port());
    environment.push(("LAYERX_WEBHOOKS_LISTENER".to_owned(), "none".to_owned()));
    let output = command(&environment)
        .output()
        .unwrap_or_else(|error| panic!("layerx-webhooks must run: {error}"));
    assert_eq!(output.status.code(), Some(2));
    assert_eq!(
        String::from_utf8_lossy(&output.stderr).trim_end(),
        "layerx-webhooks: LAYERX_WEBHOOKS_LISTENER must be tls or plain"
    );
}

#[test]
fn roles_refuse_foreign_credentials_and_listeners() {
    let material = material("roles");
    let directory = &material.directory;
    for (variable, value) in [
        (
            "LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE",
            secret(directory, "source-trigger-token", "source-trigger-token"),
        ),
        (
            "LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE",
            secret(directory, "operator-token", "operator-token"),
        ),
        (
            "LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER",
            path(&directory.join("server.der")),
        ),
    ] {
        let mut environment = environment(&material, free_port());
        environment.push((variable.to_owned(), value));
        let output = command(&environment)
            .output()
            .unwrap_or_else(|error| panic!("layerx-webhooks must run: {error}"));
        assert_eq!(output.status.code(), Some(2));
        assert_eq!(
            String::from_utf8_lossy(&output.stderr).trim_end(),
            format!("layerx-webhooks: {variable} is set with LAYERX_WEBHOOKS_ROLE public")
        );
    }
    for role in [None, Some("both")] {
        let mut environment: Vec<_> = environment(&material, free_port())
            .into_iter()
            .filter(|(name, _)| name != "LAYERX_WEBHOOKS_ROLE")
            .collect();
        if let Some(role) = role {
            environment.push(("LAYERX_WEBHOOKS_ROLE".to_owned(), role.to_owned()));
        }
        let output = command(&environment)
            .output()
            .unwrap_or_else(|error| panic!("layerx-webhooks must run: {error}"));
        assert_eq!(output.status.code(), Some(2));
        assert_eq!(
            String::from_utf8_lossy(&output.stderr).trim_end(),
            "layerx-webhooks: LAYERX_WEBHOOKS_ROLE must be public or ingress"
        );
    }
    let mut environment: Vec<_> = environment(&material, free_port())
        .into_iter()
        .filter(|(name, _)| {
            name != "LAYERX_WEBHOOKS_ROLE" && name != "LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE"
        })
        .collect();
    environment.push(("LAYERX_WEBHOOKS_ROLE".to_owned(), "ingress".to_owned()));
    environment.push(("LAYERX_WEBHOOKS_LISTENER".to_owned(), "plain".to_owned()));
    let output = command(&environment)
        .output()
        .unwrap_or_else(|error| panic!("layerx-webhooks must run: {error}"));
    assert_eq!(output.status.code(), Some(2));
    assert_eq!(
        String::from_utf8_lossy(&output.stderr).trim_end(),
        "layerx-webhooks: LAYERX_WEBHOOKS_ROLE ingress requires LAYERX_WEBHOOKS_LISTENER tls"
    );
}

#[test]
fn private_readiness_listener_cannot_serve_product_or_internal_routes() {
    let material = material("private-readiness");
    let api = TcpListener::bind("127.0.0.1:0")
        .unwrap_or_else(|error| panic!("reserve API port: {error}"));
    let health = TcpListener::bind("127.0.0.1:0")
        .unwrap_or_else(|error| panic!("reserve health port: {error}"));
    let api_port = api.local_addr().unwrap_or_else(|error| panic!("API address: {error}")).port();
    let health_port = health.local_addr().unwrap_or_else(|error| panic!("health address: {error}")).port();
    let mut environment = environment(&material, api_port);
    environment.push(("LAYERX_WEBHOOKS_HEALTH_LISTEN".to_owned(), format!("127.0.0.1:{health_port}")));
    let ca = path(&material.directory.join("server.pem"));
    drop((api, health));
    let mut service = start(&environment);
    let api_health = readiness(&mut service, &["--cacert", &ca, &format!("https://localhost:{api_port}/healthz")]);
    let private_health = readiness(&mut service, &[&format!("http://127.0.0.1:{health_port}/healthz")]);
    assert_eq!(api_health, private_health);
    assert_eq!(private_health.0, 503, "unavailable real dependencies cannot advertise readiness");
    for route in ["/v1/webhooks/scheme", "/v1/webhooks/endpoints", "/internal/v1/dispatch", "/internal/v1/events/payment/event"] {
        let (status, body) = readiness(&mut service, &[&format!("http://127.0.0.1:{health_port}{route}")]);
        assert_eq!(status, 404, "{route}: {body}");
    }
    let (status, body) = readiness(&mut service, &["-X", "POST", &format!("http://127.0.0.1:{health_port}/healthz")]);
    assert_eq!(status, 404, "{body}");
    let (status, body) = readiness(&mut service, &["--cacert", &ca, &format!("https://localhost:{api_port}/v1/webhooks/scheme")]);
    assert_eq!(status, 200, "{body}");
}
