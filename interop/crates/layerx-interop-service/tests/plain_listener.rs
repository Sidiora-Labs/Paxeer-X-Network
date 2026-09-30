use std::fs;
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::thread;
use std::time::Duration;

static FIXTURES: AtomicUsize = AtomicUsize::new(0);

struct Fixture {
    dir: PathBuf,
    port: u16,
}

impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

struct Gateway(Child);

impl Drop for Gateway {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn run(program: &str, args: &[&str]) -> Output {
    let output = Command::new(program)
        .args(args)
        .output()
        .unwrap_or_else(|error| panic!("{program} failed to start: {error}"));
    assert!(
        output.status.success(),
        "{program} {args:?} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    output
}

fn path(dir: &Path, name: &str) -> String {
    dir.join(name).to_str().expect("utf-8 path").to_owned()
}

fn fixture() -> Fixture {
    let dir = std::env::temp_dir().join(format!(
        "layerx-interop-plain-listener-{}-{}",
        std::process::id(),
        FIXTURES.fetch_add(1, Ordering::Relaxed)
    ));
    let _ = fs::remove_dir_all(&dir);
    fs::create_dir_all(&dir).expect("fixture directory");
    run(
        "openssl",
        &[
            "req",
            "-x509",
            "-newkey",
            "ec",
            "-pkeyopt",
            "ec_paramgen_curve:prime256v1",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
            "-keyout",
            &path(&dir, "key.pem"),
            "-out",
            &path(&dir, "cert.pem"),
        ],
    );
    run(
        "openssl",
        &[
            "x509",
            "-in",
            &path(&dir, "cert.pem"),
            "-outform",
            "DER",
            "-out",
            &path(&dir, "cert.der"),
        ],
    );
    run(
        "openssl",
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            &path(&dir, "key.pem"),
            "-outform",
            "DER",
            "-out",
            &path(&dir, "key.der"),
        ],
    );
    run(
        "openssl",
        &[
            "pkcs12",
            "-export",
            "-in",
            &path(&dir, "cert.pem"),
            "-inkey",
            &path(&dir, "key.pem"),
            "-passout",
            "pass:interop-client-identity",
            "-out",
            &path(&dir, "client.p12"),
        ],
    );
    for (name, contents) in [
        ("identity-password", "interop-client-identity\n"),
        ("redis-username", "interop\n"),
        ("redis-password", "interop-redis-password\n"),
        ("authority-token", "interop-authority-token\n"),
        (
            "sequencer-public-key",
            "5866666666666666666666666666666666666666666666666666666666666666\n",
        ),
        (
            "sequencer-id",
            "1111111111111111111111111111111111111111111111111111111111111111\n",
        ),
        ("sequencer-first-batch", "1\n"),
        ("sequencer-last-batch", "100\n"),
        (
            "registry.json",
            "{\"modules\":[{\"module\":1,\"ordinals\":[1]}]}",
        ),
    ] {
        fs::write(dir.join(name), contents).expect("fixture file");
    }
    let port = TcpListener::bind("127.0.0.1:0")
        .expect("free port")
        .local_addr()
        .expect("local address")
        .port();
    Fixture { dir, port }
}

fn gateway(fixture: &Fixture, extra: &[(&str, String)]) -> Command {
    let dir = &fixture.dir;
    let mut command = Command::new(env!("CARGO_BIN_EXE_layerx-interop-gateway"));
    command
        .env_clear()
        .env(
            "LAYERX_INTEROP_CONFIG",
            concat!(
                env!("CARGO_MANIFEST_DIR"),
                "/../../deploy/gateway/config.example.json"
            ),
        )
        .env(
            "LAYERX_INTEROP_LISTEN",
            format!("127.0.0.1:{}", fixture.port),
        )
        .env("LAYERX_INTEROP_OUTBOUND_CA_DER", path(dir, "cert.der"))
        .env(
            "LAYERX_INTEROP_CLIENT_IDENTITY_PKCS12",
            path(dir, "client.p12"),
        )
        .env(
            "LAYERX_INTEROP_CLIENT_IDENTITY_PASSWORD_FILE",
            path(dir, "identity-password"),
        )
        .env("LAYERX_INTEROP_REDIS_URL", "rediss://localhost:1")
        .env(
            "LAYERX_INTEROP_REDIS_USERNAME_FILE",
            path(dir, "redis-username"),
        )
        .env(
            "LAYERX_INTEROP_REDIS_PASSWORD_FILE",
            path(dir, "redis-password"),
        )
        .env(
            "LAYERX_INTEROP_SEQUENCER_PUBLIC_KEY_FILE",
            path(dir, "sequencer-public-key"),
        )
        .env(
            "LAYERX_INTEROP_SEQUENCER_ID_FILE",
            path(dir, "sequencer-id"),
        )
        .env(
            "LAYERX_INTEROP_SEQUENCER_FIRST_BATCH_FILE",
            path(dir, "sequencer-first-batch"),
        )
        .env(
            "LAYERX_INTEROP_SEQUENCER_LAST_BATCH_FILE",
            path(dir, "sequencer-last-batch"),
        )
        .env("LAYERX_INTEROP_NETWORK_ID", "layerx-testnet")
        .env("LAYERX_INTEROP_WIRE_VERSION", "3")
        .env("LAYERX_INTEROP_PROTOCOL_NETWORK_ID", "402")
        .env(
            "LAYERX_INTEROP_MODULE_REGISTRY_FILE",
            path(dir, "registry.json"),
        )
        .env("LAYERX_INTEROP_TAP_CLOCK_SKEW_SECONDS", "60")
        .env("LAYERX_INTEROP_HOSTED_GATEWAY_URL", "https://localhost:2")
        .env(
            "LAYERX_INTEROP_RECEIPT_AUTHORITY_URL",
            "https://localhost:3",
        )
        .env(
            "LAYERX_INTEROP_RECEIPT_AUTHORITY_TOKEN_FILE",
            path(dir, "authority-token"),
        );
    for (name, value) in extra {
        command.env(name, value);
    }
    command
}

fn start(fixture: &Fixture, extra: &[(&str, String)]) -> Gateway {
    let child = gateway(fixture, extra)
        .stdout(Stdio::null())
        .stderr(Stdio::piped())
        .spawn()
        .expect("interop gateway starts");
    Gateway(child)
}

fn livez(gateway: &mut Gateway, curl: &[String]) -> String {
    for _ in 0..200 {
        if let Some(status) = gateway.0.try_wait().expect("child status") {
            let output = gateway
                .0
                .stderr
                .take()
                .map_or_else(String::new, |mut stderr| {
                    let mut text = String::new();
                    let _ = std::io::Read::read_to_string(&mut stderr, &mut text);
                    text
                });
            panic!("interop gateway exited with {status}: {output}");
        }
        let output = Command::new("curl").args(curl).output().expect("curl runs");
        let text = String::from_utf8_lossy(&output.stdout).into_owned();
        if output.status.success() && text.ends_with("200") {
            return text;
        }
        thread::sleep(Duration::from_millis(50));
    }
    panic!("interop gateway never answered /livez");
}

#[test]
fn plain_listener_answers_livez_over_http() {
    let fixture = fixture();
    let mut gateway = start(&fixture, &[("LAYERX_INTEROP_LISTENER", "plain".to_owned())]);
    let body = livez(
        &mut gateway,
        &[
            "-s".to_owned(),
            "-o".to_owned(),
            "-".to_owned(),
            "-w".to_owned(),
            "%{http_code}".to_owned(),
            format!("http://127.0.0.1:{}/livez", fixture.port),
        ],
    );
    assert!(body.contains("\"status\":\"live\""), "{body}");
    assert!(body.contains("layerx-interop-gateway"), "{body}");
}

#[test]
fn default_listener_answers_livez_over_tls() {
    let fixture = fixture();
    let dir = &fixture.dir;
    let mut gateway = start(
        &fixture,
        &[
            ("LAYERX_INTEROP_TLS_CERT_DER", path(dir, "cert.der")),
            ("LAYERX_INTEROP_TLS_KEY_DER", path(dir, "key.der")),
        ],
    );
    let body = livez(
        &mut gateway,
        &[
            "-s".to_owned(),
            "--cacert".to_owned(),
            path(dir, "cert.pem"),
            "--resolve".to_owned(),
            format!("localhost:{}:127.0.0.1", fixture.port),
            "-o".to_owned(),
            "-".to_owned(),
            "-w".to_owned(),
            "%{http_code}".to_owned(),
            format!("https://localhost:{}/livez", fixture.port),
        ],
    );
    assert!(body.contains("\"status\":\"live\""), "{body}");
    let plain = Command::new("curl")
        .args([
            "-s",
            "-o",
            "/dev/null",
            "-w",
            "%{http_code}",
            &format!("http://127.0.0.1:{}/livez", fixture.port),
        ])
        .output()
        .expect("curl runs");
    assert_ne!(String::from_utf8_lossy(&plain.stdout), "200");
}

fn refused(fixture: &Fixture, extra: &[(&str, String)]) -> String {
    let output = gateway(fixture, extra)
        .output()
        .expect("interop gateway runs");
    assert!(!output.status.success(), "gateway accepted {extra:?}");
    String::from_utf8_lossy(&output.stderr).into_owned()
}

#[test]
fn plain_listener_refuses_tls_material_and_unknown_modes() {
    let fixture = fixture();
    let dir = &fixture.dir;
    let plain = || ("LAYERX_INTEROP_LISTENER", "plain".to_owned());
    let stderr = refused(
        &fixture,
        &[
            plain(),
            ("LAYERX_INTEROP_TLS_CERT_DER", path(dir, "cert.der")),
        ],
    );
    assert!(
        stderr.contains("LAYERX_INTEROP_TLS_CERT_DER is set with LAYERX_INTEROP_LISTENER plain"),
        "{stderr}"
    );
    let stderr = refused(
        &fixture,
        &[
            plain(),
            ("LAYERX_INTEROP_TLS_KEY_DER", path(dir, "key.der")),
        ],
    );
    assert!(
        stderr.contains("LAYERX_INTEROP_TLS_KEY_DER is set with LAYERX_INTEROP_LISTENER plain"),
        "{stderr}"
    );
    let stderr = refused(&fixture, &[("LAYERX_INTEROP_LISTENER", "http".to_owned())]);
    assert!(
        stderr.contains("LAYERX_INTEROP_LISTENER must be tls or plain"),
        "{stderr}"
    );
}
