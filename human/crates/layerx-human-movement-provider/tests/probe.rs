use std::fs;
use std::net::{TcpListener, TcpStream};
use std::os::unix::fs::{DirBuilderExt as _, PermissionsExt as _};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::thread;
use std::time::Duration;

type Result<T = ()> = std::result::Result<T, Box<dyn std::error::Error>>;

const BINARY: &str = env!("CARGO_BIN_EXE_layerx-human-movement-provider");

struct Directory(PathBuf);

impl Drop for Directory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn directory(name: &str) -> Result<Directory> {
    let root = std::env::temp_dir().join(format!("lxmp-probe-{name}-{}", std::process::id()));
    let _ = fs::remove_dir_all(&root);
    fs::DirBuilder::new().mode(0o700).create(&root)?;
    Ok(Directory(root))
}

/// A child process that is killed and reaped when the test ends, pass or fail.
struct Process(Child);

impl Drop for Process {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn free_port(host: &str) -> Result<u16> {
    let reserved = TcpListener::bind((host, 0))?;
    Ok(reserved.local_addr()?.port())
}

fn wait_for_listener(host: &str, port: u16, process: &mut Process) -> Result {
    for _ in 0..200 {
        if TcpStream::connect((host, port)).is_ok() {
            return Ok(());
        }
        if process.0.try_wait()?.is_some() {
            return Err(format!("the process for {host}:{port} exited before listening").into());
        }
        thread::sleep(Duration::from_millis(50));
    }
    Err(format!("nothing listened on {host}:{port}").into())
}

fn openssl(directory: &Path, arguments: &[&str]) -> Result {
    let status = Command::new("openssl")
        .current_dir(directory)
        .args(arguments)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()?;
    if !status.success() {
        return Err(format!("openssl {} failed", arguments.join(" ")).into());
    }
    Ok(())
}

fn sign(directory: &Path, name: &str, subject: &str, extensions: &str) -> Result {
    let (key, request, certificate, file) = (
        format!("{name}.key"),
        format!("{name}.csr"),
        format!("{name}.pem"),
        format!("{name}.ext"),
    );
    openssl(
        directory,
        &[
            "req", "-newkey", "rsa:2048", "-nodes", "-keyout", &key, "-out", &request, "-subj",
            subject,
        ],
    )?;
    fs::write(directory.join(&file), extensions)?;
    openssl(
        directory,
        &[
            "x509",
            "-req",
            "-in",
            &request,
            "-CA",
            "ca.pem",
            "-CAkey",
            "ca.key",
            "-CAcreateserial",
            "-out",
            &certificate,
            "-days",
            "1",
            "-extfile",
            &file,
        ],
    )
}

/// Mints the internal CA, the TLS certificate the two chain origins present
/// and the movement execution authority's client identity, all as the
/// private DER files the provider's environment names.
fn material(directory: &Path) -> Result {
    openssl(
        directory,
        &[
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            "ca.key",
            "-out",
            "ca.pem",
            "-days",
            "1",
            "-subj",
            "/CN=movement probe test CA",
            "-addext",
            "basicConstraints=critical,CA:TRUE",
        ],
    )?;
    sign(
        directory,
        "origin",
        "/CN=paxeer origin",
        "basicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\nsubjectAltName=IP:127.0.0.1,IP:127.0.0.2\n",
    )?;
    sign(
        directory,
        "client",
        "/CN=movement-provider",
        "extendedKeyUsage=clientAuth\n",
    )?;
    openssl(
        directory,
        &["x509", "-in", "ca.pem", "-outform", "DER", "-out", "ca.der"],
    )?;
    openssl(
        directory,
        &[
            "x509",
            "-in",
            "client.pem",
            "-outform",
            "DER",
            "-out",
            "client.der",
        ],
    )?;
    openssl(
        directory,
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            "client.key",
            "-outform",
            "DER",
            "-out",
            "client-key.der",
        ],
    )?;
    for name in ["ca.der", "client.der", "client-key.der", "origin.key"] {
        fs::set_permissions(directory.join(name), fs::Permissions::from_mode(0o600))?;
    }
    Ok(())
}

fn anvil_binary() -> PathBuf {
    let foundry = PathBuf::from("/root/.foundry/bin/anvil");
    if foundry.exists() {
        foundry
    } else {
        PathBuf::from("anvil")
    }
}

/// A local Paxeer-compatible chain origin behind a real TLS listener bound to
/// `host`, presenting the certificate the provider pins.
fn tls_origin(directory: &Path, host: &str, chain: u16) -> Result<(Process, u16)> {
    let port = free_port(host)?;
    let mut front = Process(
        Command::new("socat")
            .current_dir(directory)
            .arg(format!(
                "OPENSSL-LISTEN:{port},bind={host},reuseaddr,fork,cert=origin.pem,key=origin.key,verify=0"
            ))
            .arg(format!("TCP:127.0.0.1:{chain}"))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()?,
    );
    wait_for_listener(host, port, &mut front)?;
    Ok((front, port))
}

/// A real Ed25519 verifying key, since the deposit-proof verifier refuses
/// bytes that are not a point on the curve.
fn checkpoint_authority() -> Result<String> {
    let mut seed = [0; 32];
    getrandom::fill(&mut seed).map_err(|error| error.to_string())?;
    let key = ed25519_dalek::SigningKey::from_bytes(&seed).verifying_key();
    Ok(format!(
        "0x{}",
        key.as_bytes()
            .iter()
            .map(|byte| format!("{byte:02x}"))
            .collect::<String>()
    ))
}

fn probe(socket: &Path) -> Result<Output> {
    Ok(Command::new(BINARY)
        .arg("probe")
        .env_clear()
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_SOCKET", socket)
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_DEADLINE_SECONDS", "10")
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_MAX_FRAME_BYTES", "1048576")
        .env("LAYERX_HUMAN_MOVEMENT_PROVIDER_PROTOCOL_VERSION", "2")
        .output()?)
}

#[test]
fn probe_exits_zero_only_on_the_serving_providers_healthy_answer() -> Result {
    let directory = directory("serve")?;
    let root = directory.0.as_path();
    let socket = root.join("movement.sock");
    let evidence = root.join("evidence");
    fs::DirBuilder::new().mode(0o700).create(&evidence)?;
    material(root)?;

    let absent = probe(&socket)?;
    assert_eq!(absent.status.code(), Some(1));
    assert!(absent.stdout.is_empty());

    let chain = free_port("127.0.0.1")?;
    let mut anvil = Process(
        Command::new(anvil_binary())
            .arg("--port")
            .arg(chain.to_string())
            .arg("--silent")
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()?,
    );
    wait_for_listener("127.0.0.1", chain, &mut anvil)?;
    let (_first, first_port) = tls_origin(root, "127.0.0.1", chain)?;
    let (_second, second_port) = tls_origin(root, "127.0.0.2", chain)?;
    let execution_authority = free_port("127.0.0.1")?;

    let mut provider = Process(
        Command::new(BINARY)
            .env_clear()
            .envs(
                [
                    ("MODE", "movement".to_owned()),
                    ("DEADLINE_SECONDS", "5".to_owned()),
                    ("MAX_FRAME_BYTES", "1048576".to_owned()),
                    ("PROTOCOL_VERSION", "2".to_owned()),
                    ("PAXEER_CHAIN_ID", "31337".to_owned()),
                    ("PAXEER_CA_DER", root.join("ca.der").display().to_string()),
                    (
                        "PAXEER_RPC_URLS",
                        format!(
                        "[\"https://127.0.0.1:{first_port}\",\"https://127.0.0.2:{second_port}\"]"
                    ),
                    ),
                    ("PAXEER_MINIMUM_AGREEMENT", "2".to_owned()),
                    ("PAXEER_CONFIRMATIONS", "2".to_owned()),
                    ("SOCKET", socket.display().to_string()),
                    (
                        "ALLOWED_UID",
                        rustix::process::geteuid().as_raw().to_string(),
                    ),
                    (
                        "ALLOWED_GID",
                        rustix::process::getegid().as_raw().to_string(),
                    ),
                    ("STATE_ROOT", root.join("state").display().to_string()),
                    ("EVIDENCE_ROOT", evidence.display().to_string()),
                    (
                        "PAXEER_CHECKPOINT_REGISTRY",
                        format!("0x{}", "0c".repeat(20)),
                    ),
                    ("KMS_CA_DER", root.join("ca.der").display().to_string()),
                    (
                        "KMS_CLIENT_CERT_DER",
                        root.join("client.der").display().to_string(),
                    ),
                    (
                        "KMS_CLIENT_KEY_DER",
                        root.join("client-key.der").display().to_string(),
                    ),
                    (
                        "KMS_PROVIDER_REFERENCE",
                        "movement-probe-test-kms".to_owned(),
                    ),
                    ("KMS_ENDPOINT", format!("127.0.0.1:{execution_authority}")),
                    ("KMS_SERVER_NAME", "localhost".to_owned()),
                    ("CHECKPOINT_INTERVAL_SECONDS", "10".to_owned()),
                    ("PAXEER_BLOCK_SECONDS", "1".to_owned()),
                    ("REMINDER_INTERVAL_SECONDS", "60".to_owned()),
                    ("POLL_SECONDS", "1".to_owned()),
                    ("DELAYED_AFTER_POLLS", "2".to_owned()),
                    ("PAXEER_CHECKPOINT_AUTHORITY", checkpoint_authority()?),
                    ("CUSTODY_REFERENCE", format!("0x{}", "09".repeat(32))),
                    ("NETWORK_ID", "77".to_owned()),
                ]
                .into_iter()
                .map(|(name, value)| (format!("LAYERX_HUMAN_MOVEMENT_PROVIDER_{name}"), value)),
            )
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()?,
    );
    let mut bound = false;
    for _ in 0..200 {
        if socket.exists() {
            bound = true;
            break;
        }
        if let Some(status) = provider.0.try_wait()? {
            let mut stderr = String::new();
            if let Some(mut pipe) = provider.0.stderr.take() {
                std::io::Read::read_to_string(&mut pipe, &mut stderr)?;
            }
            panic!("movement provider exited before binding with {status}: {stderr}");
        }
        thread::sleep(Duration::from_millis(50));
    }
    assert!(bound, "movement provider never bound its socket");

    let ready = probe(&socket)?;
    assert_eq!(ready.status.code(), Some(0));
    assert!(ready.stdout.is_empty());

    drop(provider);
    let stopped = probe(&socket)?;
    assert_eq!(stopped.status.code(), Some(1));
    Ok(())
}

#[test]
fn probe_refuses_a_missing_socket() -> Result {
    let directory = directory("missing")?;
    let missing = probe(&directory.0.join("absent.sock"))?;
    assert_eq!(missing.status.code(), Some(1));
    assert!(missing.stdout.is_empty());
    Ok(())
}
