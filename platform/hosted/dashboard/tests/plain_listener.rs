use serde_json::Value;
use std::fs;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const BINARY: &str = env!("CARGO_BIN_EXE_layerx-dashboard");
const LISTENER_CERTIFICATE_VARIABLES: [&str; 2] = [
    "LAYERX_DASHBOARD_TLS_CERT_DER",
    "LAYERX_DASHBOARD_TLS_KEY_DER",
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
        "layerx-dashboard-{label}-{}-{nanos}",
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
    [
        ("LAYERX_DASHBOARD_LISTEN", format!("127.0.0.1:{listen}")),
        ("LAYERX_DASHBOARD_TLS_CERT_DER", ca.clone()),
        (
            "LAYERX_DASHBOARD_TLS_KEY_DER",
            path(&directory.join("server-key.der")),
        ),
        (
            "LAYERX_DASHBOARD_GATEWAY_REDIS_URL",
            format!("rediss://localhost:{unused}"),
        ),
        ("LAYERX_DASHBOARD_REDIS_CA_DER", ca.clone()),
        (
            "LAYERX_DASHBOARD_GATEWAY_REDIS_USERNAME_FILE",
            secret(directory, "gateway-redis-username", "dashboard"),
        ),
        (
            "LAYERX_DASHBOARD_GATEWAY_REDIS_PASSWORD_FILE",
            secret(directory, "gateway-redis-password", "dashboard-secret"),
        ),
        ("LAYERX_DASHBOARD_INTERNAL_CA_DER", ca.clone()),
        (
            "LAYERX_DASHBOARD_CLIENT_IDENTITY_PKCS12",
            path(&directory.join("client.p12")),
        ),
        (
            "LAYERX_DASHBOARD_CLIENT_IDENTITY_PASSWORD_FILE",
            secret(directory, "identity-password", "integration-only"),
        ),
        (
            "LAYERX_DASHBOARD_IDENTITY_URL",
            format!("https://localhost:{unused}"),
        ),
        (
            "LAYERX_DASHBOARD_IDENTITY_TOKEN_FILE",
            secret(directory, "identity-token", "identity-token"),
        ),
        ("LAYERX_WEBHOOKS_INTERNAL_CA_DER", ca),
        (
            "LAYERX_WEBHOOKS_REDIS_URL",
            format!("rediss://localhost:{unused}"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_USERNAME_FILE",
            secret(directory, "webhooks-redis-username", "dashboard"),
        ),
        (
            "LAYERX_WEBHOOKS_REDIS_PASSWORD_FILE",
            secret(directory, "webhooks-redis-password", "dashboard-secret"),
        ),
    ]
    .into_iter()
    .map(|(name, value)| (name.to_owned(), value))
    .collect()
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
            .unwrap_or_else(|error| panic!("layerx-dashboard must start: {error}")),
    }
}

fn readiness(service: &mut Service, arguments: &[&str]) -> (u16, Value) {
    for _ in 0..100 {
        if let Ok(Some(status)) = service.child.try_wait() {
            panic!("layerx-dashboard exited before answering: {status}");
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
    panic!("layerx-dashboard never answered readiness with {arguments:?}")
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
    environment.push(("LAYERX_DASHBOARD_LISTENER".to_owned(), "plain".to_owned()));
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
            environment.push(("LAYERX_DASHBOARD_LISTENER".to_owned(), mode.to_owned()));
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
        environment.push(("LAYERX_DASHBOARD_LISTENER".to_owned(), "plain".to_owned()));
        let output = command(&environment)
            .output()
            .unwrap_or_else(|error| panic!("layerx-dashboard must run: {error}"));
        assert_eq!(output.status.code(), Some(2));
        assert_eq!(
            String::from_utf8_lossy(&output.stderr).trim_end(),
            format!("layerx-dashboard: {variable} is set with LAYERX_DASHBOARD_LISTENER plain")
        );
    }
    let mut environment = environment(&material, free_port());
    environment.push(("LAYERX_DASHBOARD_LISTENER".to_owned(), "none".to_owned()));
    let output = command(&environment)
        .output()
        .unwrap_or_else(|error| panic!("layerx-dashboard must run: {error}"));
    assert_eq!(output.status.code(), Some(2));
    assert_eq!(
        String::from_utf8_lossy(&output.stderr).trim_end(),
        "layerx-dashboard: LAYERX_DASHBOARD_LISTENER must be tls or plain"
    );
}
