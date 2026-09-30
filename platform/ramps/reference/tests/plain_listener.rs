use std::fs;
use std::net::TcpListener;
use std::os::unix::fs::PermissionsExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use serde_json::{json, Value};

struct Workspace {
    dir: PathBuf,
}

impl Workspace {
    fn new(label: &str) -> Self {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        let dir = std::env::temp_dir().join(format!(
            "layerx-ramp-listener-{label}-{}-{nanos}",
            std::process::id()
        ));
        fs::create_dir_all(&dir).expect("create workspace");
        let workspace = Self { dir };
        workspace.openssl(&[
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "2",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
            "-keyout",
            "key.pem",
            "-out",
            "cert.pem",
        ]);
        workspace.openssl(&[
            "pkcs12",
            "-export",
            "-inkey",
            "key.pem",
            "-in",
            "cert.pem",
            "-passout",
            "pass:ramp-test-password",
            "-out",
            "identity.p12",
        ]);
        workspace.openssl(&[
            "x509",
            "-in",
            "cert.pem",
            "-outform",
            "DER",
            "-out",
            "anchor.der",
        ]);
        workspace.private_file("password", "ramp-test-password\n");
        workspace.private(&workspace.path("identity.p12"));
        for token in [
            "identity-token",
            "compliance-token",
            "provider-token",
            "gateway-key",
            "authority-token",
            "signer-token",
            "custody-token",
            "operator-token",
        ] {
            workspace.private_file(token, &format!("{token}-value\n"));
        }
        workspace
    }

    fn path(&self, name: &str) -> PathBuf {
        self.dir.join(name)
    }

    fn openssl(&self, args: &[&str]) {
        let output = Command::new("openssl")
            .args(args)
            .current_dir(&self.dir)
            .output()
            .expect("run openssl");
        assert!(
            output.status.success(),
            "openssl {args:?}: {}",
            String::from_utf8_lossy(&output.stderr)
        );
    }

    fn private(&self, path: &Path) {
        fs::set_permissions(path, fs::Permissions::from_mode(0o600)).expect("private file");
    }

    fn private_file(&self, name: &str, contents: &str) {
        let path = self.path(name);
        fs::write(&path, contents).expect("write file");
        self.private(&path);
    }

    fn config(&self, port: u16, listener: Option<&str>, identity: bool, password: bool) -> PathBuf {
        let p = |name: &str| self.path(name).display().to_string();
        let did = "did:layerx:ramp-listener-test";
        let account = format!("agent:{did}:main");
        let mut config = json!({
            "listen": format!("127.0.0.1:{port}"),
            "journal_path": p("journal.jsonl"),
            "worker_id": "ramp-listener-worker",
            "lease_seconds": 60,
            "reconcile_seconds": 5,
            "operator": {
                "principal_id": "ramp-listener-operator",
                "account": account,
                "signer_key_handle": "kms://ramp-listener/layerx"
            },
            "quotes": [{
                "quote_id": "listener-quote",
                "direction": "on_ramp",
                "layerx_asset": ([1_u8; 32]),
                "layerx_amount": 1_000_000,
                "external_currency": "EUR",
                "external_amount_minor": 100,
                "rate_numerator": 1_000_000,
                "rate_denominator": 100,
                "fee_minor": 2,
                "maximum_slippage_bps": 25,
                "context": ([8_u8; 32]),
                "provider_token": "provider-product",
                "payout_token": "beneficiary-token",
                "expires_at": 4_000_000_000_u64
            }],
            "client_tls": {
                "ca_pem": p("cert.pem"),
                "identity_pkcs12": p("identity.p12"),
                "identity_password_file": p("password"),
                "timeout_seconds": 8
            },
            "identity": {
                "endpoint": "https://identity.example.test",
                "service_token_file": p("identity-token"),
                "audience": "layerx-ramp"
            },
            "compliance": {
                "endpoint": "https://compliance.example.test",
                "service_token_file": p("compliance-token"),
                "public_key": "22".repeat(32)
            },
            "provider": {
                "endpoint": "https://provider.example.test",
                "credential_file": p("provider-token"),
                "settlement_path": "/layerx-ramp-v1/settlements",
                "status_path": "/layerx-ramp-v1/settlements"
            },
            "layerx": {
                "gateway_endpoint": "https://gateway.example.test",
                "receipt_authority_endpoint": "https://authority.example.test",
                "signer_endpoint": "https://signer.example.test",
                "gateway_key_file": p("gateway-key"),
                "authority_token_file": p("authority-token"),
                "signer_token_file": p("signer-token"),
                "actor_did": did,
                "protocol_version": layerx_wire::limits::PROTOCOL_VERSION,
                "network_id": 1,
                "fee_limit": 1000,
                "signer_public_key": "33".repeat(32),
                "sequencer_id": "01".repeat(32),
                "sequencer_public_key": format!("58{}", "66".repeat(31)),
                "sequencer_first_batch": "1",
                "sequencer_last_batch": "10"
            },
            "paxeer": {
                "custody_endpoint": "https://custody.example.test",
                "custody_credential_file": p("custody-token"),
                "broadcast_path": "/layerx-paxeer-v1/rebalances",
                "status_path": "/layerx-paxeer-v1/rebalances",
                "operator_account": account,
                "wallet_address": "operator-wallet",
                "vault_id": "operator-vault",
                "signer_key_handle": "kms://ramp-listener/paxeer",
                "rpc_endpoints": ["https://rpc-a.example.test:8545", "https://rpc-b.example.test:8545"],
                "rpc_trust_anchor_der": p("anchor.der"),
                "rpc_chain_id": 229,
                "rpc_minimum_agreement": 2,
                "required_confirmations": 12,
                "poll_cadence_seconds": 5,
                "delayed_after_polls": 12
            },
            "provider_callback_public_key": "44".repeat(32),
            "operator_control_token_file": p("operator-token")
        });
        let object = config.as_object_mut().expect("config object");
        if let Some(listener) = listener {
            object.insert("listener".to_owned(), Value::from(listener));
        }
        if identity {
            object.insert(
                "server_identity_pkcs12".to_owned(),
                Value::from(p("identity.p12")),
            );
        }
        if password {
            object.insert(
                "server_identity_password_file".to_owned(),
                Value::from(p("password")),
            );
        }
        let path = self.path(&format!("config-{port}.json"));
        fs::write(&path, serde_json::to_vec(&config).expect("encode config"))
            .expect("write config");
        path
    }
}

impl Drop for Workspace {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

struct Ramp(Child);

impl Drop for Ramp {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .expect("bind free port")
        .local_addr()
        .expect("local address")
        .port()
}

fn start(config: &Path) -> Ramp {
    Ramp(
        Command::new(env!("CARGO_BIN_EXE_layerx-reference-ramp"))
            .arg(config)
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .expect("spawn ramp"),
    )
}

fn curl(args: &[&str]) -> Output {
    Command::new("curl")
        .args(["-s", "--max-time", "5", "-w", "\n%{http_code}"])
        .args(args)
        .output()
        .expect("run curl")
}

fn await_ready(ramp: &mut Ramp, args: &[&str]) -> String {
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        if let Some(status) = ramp.0.try_wait().expect("poll ramp") {
            let mut stderr = String::new();
            if let Some(mut pipe) = ramp.0.stderr.take() {
                let _ = std::io::Read::read_to_string(&mut pipe, &mut stderr);
            }
            panic!("ramp exited before ready: {status}: {stderr}");
        }
        let output = curl(args);
        let text = String::from_utf8_lossy(&output.stdout).into_owned();
        if text.ends_with("\n200") {
            return text;
        }
        assert!(Instant::now() < deadline, "ramp not ready: {text}");
        thread::sleep(Duration::from_millis(100));
    }
}

#[test]
fn plain_listener_serves_readiness_over_http() {
    let workspace = Workspace::new("plain");
    let port = free_port();
    let mut ramp = start(&workspace.config(port, Some("plain"), false, false));
    let body = await_ready(&mut ramp, &[&format!("http://127.0.0.1:{port}/readyz")]);
    assert!(body.contains("\"ready\":true"), "{body}");
}

#[test]
fn default_listener_serves_readiness_over_tls() {
    let workspace = Workspace::new("tls");
    let port = free_port();
    let mut ramp = start(&workspace.config(port, None, true, true));
    let cacert = workspace.path("cert.pem").display().to_string();
    let url = format!("https://localhost:{port}/readyz");
    let body = await_ready(&mut ramp, &["--cacert", &cacert, &url]);
    assert!(body.contains("\"ready\":true"), "{body}");
    let plain = curl(&[&format!("http://127.0.0.1:{port}/readyz")]);
    assert!(
        !String::from_utf8_lossy(&plain.stdout).ends_with("\n200"),
        "TLS listener answered plain HTTP"
    );
}

#[test]
fn plain_listener_refuses_configured_server_identity() {
    let workspace = Workspace::new("refuse");
    for (identity, password, setting) in [
        (true, false, "server_identity_pkcs12"),
        (false, true, "server_identity_password_file"),
        (true, true, "server_identity_pkcs12"),
    ] {
        let port = free_port();
        let output = Command::new(env!("CARGO_BIN_EXE_layerx-reference-ramp"))
            .arg(workspace.config(port, Some("plain"), identity, password))
            .output()
            .expect("run ramp");
        let stderr = String::from_utf8_lossy(&output.stderr);
        assert!(
            !output.status.success(),
            "plain listener started with {setting}"
        );
        assert!(
            stderr.contains(&format!("{setting} is set with listener plain")),
            "{stderr}"
        );
    }
    let port = free_port();
    let output = Command::new(env!("CARGO_BIN_EXE_layerx-reference-ramp"))
        .arg(workspace.config(port, Some("quic"), true, true))
        .output()
        .expect("run ramp");
    assert!(!output.status.success());
    assert!(
        String::from_utf8_lossy(&output.stderr).contains("listener must be tls or plain"),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
}
