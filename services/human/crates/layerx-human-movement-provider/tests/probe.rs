use std::fs;
use std::net::{TcpListener, TcpStream};
use std::os::unix::fs::{DirBuilderExt as _, PermissionsExt as _};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::thread;
use std::time::Duration;

use layerx_human_service::custody::{
    KeyClass, KmsError, KmsProvider as _, PrincipalKeyBinding, ProviderKeyReference,
};

mod kms;

type Result<T = ()> = std::result::Result<T, Box<dyn std::error::Error>>;

const BINARY: &str = env!("CARGO_BIN_EXE_layerx-human-movement-provider");

struct Directory(PathBuf);

impl Drop for Directory {
    fn drop(&mut self) {
        if std::env::var_os("PAXEER_X_RETAIN_STATE").is_none() {
            let _ = fs::remove_dir_all(&self.0);
        }
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

/// Mints the internal CA and the TLS certificate the two chain origins
/// present, as the private files the provider's environment names.
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
    openssl(
        directory,
        &["x509", "-in", "ca.pem", "-outform", "DER", "-out", "ca.der"],
    )?;
    for name in ["ca.der", "origin.key"] {
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

fn ready(socket: &Path) -> Result<bool> {
    let output = probe(socket)?;
    if !output.stdout.is_empty() {
        return Err("the readiness probe wrote to stdout".into());
    }
    match output.status.code() {
        Some(0) => Ok(true),
        Some(1) => Ok(false),
        other => Err(format!("the readiness probe exited with {other:?}").into()),
    }
}

/// Recovery is observed, not assumed: the provider must report ready again
/// within a bounded window once its execution authority is back.
fn ready_within(socket: &Path, window: Duration) -> Result<bool> {
    let started = std::time::Instant::now();
    loop {
        if ready(socket)? {
            return Ok(true);
        }
        if started.elapsed() > window {
            return Ok(false);
        }
        thread::sleep(Duration::from_millis(200));
    }
}

/// Two TLS chain origins in front of one local chain, and a real Human KMS
/// process as the movement execution authority.
struct Fixture {
    kms: kms::Kms,
    _origins: [Process; 2],
    _chain: Process,
    origins: [u16; 2],
    authority: String,
    directory: Directory,
}

struct Provider {
    _process: Process,
    socket: PathBuf,
}

impl Fixture {
    fn new(name: &str) -> Result<Self> {
        let directory = directory(name)?;
        let root = directory.0.clone();
        material(&root)?;
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
        let (first, first_port) = tls_origin(&root, "127.0.0.1", chain)?;
        let (second, second_port) = tls_origin(&root, "127.0.0.2", chain)?;
        let kms = kms::Kms::start(&root.join("kms"), &kms::beside(Path::new(BINARY))?)?;
        Ok(Self {
            kms,
            _origins: [first, second],
            _chain: anvil,
            origins: [first_port, second_port],
            authority: checkpoint_authority()?,
            directory,
        })
    }

    /// Starts a movement-mode provider named `label` whose execution
    /// authority is the fixture KMS under the restricted executor identity,
    /// with `overrides` replacing individual settings.
    fn provider(&self, label: &str, overrides: &[(&str, String)]) -> Result<Provider> {
        let root = &self.directory.0;
        let socket = root.join(format!("{label}.sock"));
        let evidence = root.join(format!("{label}-evidence"));
        fs::DirBuilder::new().mode(0o700).create(&evidence)?;
        let [first_port, second_port] = self.origins;
        let mut settings = vec![
            ("MODE", "movement".to_owned()),
            ("DEADLINE_SECONDS", "5".to_owned()),
            ("MAX_FRAME_BYTES", "1048576".to_owned()),
            ("PROTOCOL_VERSION", "2".to_owned()),
            ("PAXEER_CHAIN_ID", "31337".to_owned()),
            ("PAXEER_CA_DER", root.join("ca.der").display().to_string()),
            (
                "PAXEER_RPC_URLS",
                format!("[\"https://127.0.0.1:{first_port}\",\"https://127.0.0.2:{second_port}\"]"),
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
            (
                "STATE_ROOT",
                root.join(format!("{label}-state")).display().to_string(),
            ),
            ("EVIDENCE_ROOT", evidence.display().to_string()),
            (
                "PAXEER_CHECKPOINT_REGISTRY",
                format!("0x{}", "0c".repeat(20)),
            ),
            ("KMS_CA_DER", self.kms.file("ca.der").display().to_string()),
            (
                "KMS_CLIENT_CERT_DER",
                self.kms.file("executor.der").display().to_string(),
            ),
            (
                "KMS_CLIENT_KEY_DER",
                self.kms.file("executor-key.der").display().to_string(),
            ),
            ("KMS_PROVIDER_REFERENCE", kms::PROVIDER.to_owned()),
            ("KMS_ENDPOINT", self.kms.address.to_string()),
            ("KMS_SERVER_NAME", "localhost".to_owned()),
            ("CHECKPOINT_INTERVAL_SECONDS", "10".to_owned()),
            ("PAXEER_BLOCK_SECONDS", "1".to_owned()),
            ("REMINDER_INTERVAL_SECONDS", "60".to_owned()),
            ("POLL_SECONDS", "1".to_owned()),
            ("DELAYED_AFTER_POLLS", "2".to_owned()),
            ("PAXEER_CHECKPOINT_AUTHORITY", self.authority.clone()),
            ("CUSTODY_REFERENCE", format!("0x{}", "09".repeat(32))),
            ("NETWORK_ID", kms::NETWORK.to_string()),
        ];
        for (name, value) in overrides {
            let slot = settings
                .iter_mut()
                .find(|(existing, _)| existing == name)
                .ok_or_else(|| format!("unknown provider setting {name}"))?;
            slot.1.clone_from(value);
        }
        let log = root.join(format!("{label}-provider.log"));
        let mut process =
            Process(
                Command::new(BINARY)
                    .env_clear()
                    .envs(settings.into_iter().map(|(name, value)| {
                        (format!("LAYERX_HUMAN_MOVEMENT_PROVIDER_{name}"), value)
                    }))
                    .stdin(Stdio::null())
                    .stdout(Stdio::null())
                    .stderr(fs::File::create(&log)?)
                    .spawn()?,
            );
        for _ in 0..200 {
            if socket.exists() {
                return Ok(Provider {
                    _process: process,
                    socket,
                });
            }
            if let Some(status) = process.0.try_wait()? {
                return Err(format!(
                    "movement provider {label} exited before binding with {status}: {}",
                    fs::read_to_string(&log)?
                )
                .into());
            }
            thread::sleep(Duration::from_millis(50));
        }
        Err(format!("movement provider {label} never bound its socket").into())
    }
}

#[test]
fn probe_exits_zero_only_on_the_serving_providers_healthy_answer() -> Result {
    let fixture = Fixture::new("serve")?;
    let socket = fixture.directory.0.join("serve.sock");
    let absent = probe(&socket)?;
    assert_eq!(absent.status.code(), Some(1));
    assert!(absent.stdout.is_empty());

    let provider = fixture.provider("serve", &[])?;
    assert_eq!(provider.socket, socket);
    let durable = fixture.kms.state_digest()?;
    let ready = probe(&socket)?;
    assert_eq!(ready.status.code(), Some(0));
    assert!(ready.stdout.is_empty());
    assert_eq!(
        fixture.kms.state_digest()?,
        durable,
        "a readiness probe changed the execution authority's durable state"
    );

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

#[test]
fn readiness_withdraws_on_executor_loss_or_stall_and_returns_on_recovery() -> Result {
    let mut fixture = Fixture::new("recovery")?;
    let provider = fixture.provider("recovery", &[])?;
    assert!(ready(&provider.socket)?, "healthy executor must be ready");

    fixture.kms.stop();
    assert!(
        !ready(&provider.socket)?,
        "an absent executor must withdraw readiness while origins and storage answer"
    );

    fixture.kms.restart()?;
    let durable = fixture.kms.state_digest()?;
    assert!(
        ready_within(&provider.socket, Duration::from_secs(20))?,
        "a restarted executor must restore readiness"
    );
    assert_eq!(fixture.kms.state_digest()?, durable);

    fixture.kms.pause()?;
    let stalled = ready(&provider.socket);
    fixture.kms.resume()?;
    assert!(
        !stalled?,
        "an executor that does not answer within the deadline must withdraw readiness"
    );
    assert!(
        ready_within(&provider.socket, Duration::from_secs(20))?,
        "a resumed executor must restore readiness"
    );
    assert_eq!(
        fixture.kms.state_digest()?,
        durable,
        "loss, stall and recovery must leave no signing or persistence side effect"
    );
    Ok(())
}

#[test]
fn readiness_refuses_an_executor_with_the_wrong_identity_or_handshake() -> Result {
    let fixture = Fixture::new("identity")?;
    let durable = fixture.kms.state_digest()?;
    let identity = |name: &str| {
        [
            (
                "KMS_CLIENT_CERT_DER",
                fixture
                    .kms
                    .file(&format!("{name}.der"))
                    .display()
                    .to_string(),
            ),
            (
                "KMS_CLIENT_KEY_DER",
                fixture
                    .kms
                    .file(&format!("{name}-key.der"))
                    .display()
                    .to_string(),
            ),
        ]
    };
    let cases: Vec<(&str, Vec<(&str, String)>)> = vec![
        ("service-identity", identity("service").to_vec()),
        ("unpinned-identity", identity("stranger").to_vec()),
        ("foreign-identity", identity("foreign").to_vec()),
        (
            "wrong-provider",
            vec![("KMS_PROVIDER_REFERENCE", "another-kms".to_owned())],
        ),
        (
            "wrong-network",
            vec![("NETWORK_ID", (kms::NETWORK + 1).to_string())],
        ),
        (
            "wrong-server-name",
            vec![("KMS_SERVER_NAME", "kms.invalid".to_owned())],
        ),
        (
            "untrusted-server",
            vec![(
                "KMS_CA_DER",
                fixture.kms.file("other-ca.der").display().to_string(),
            )],
        ),
    ];
    for (label, overrides) in &cases {
        let provider = fixture.provider(label, overrides)?;
        assert!(
            !ready(&provider.socket)?,
            "{label}: readiness must be withdrawn while origins and storage answer"
        );
    }
    let control = fixture.provider("control", &[])?;
    assert!(
        ready(&control.socket)?,
        "the correctly configured executor must be ready against the same origins"
    );
    assert_eq!(fixture.kms.state_digest()?, durable);
    Ok(())
}

#[test]
fn executor_certificate_gains_no_export_owner_or_signing_authority() -> Result {
    let directory = directory("authority")?;
    let kms = kms::Kms::start(&directory.0.join("kms"), &kms::beside(Path::new(BINARY))?)?;
    let executor = kms.remote("executor", kms::PROVIDER, "localhost", "ca.der")?;
    assert_eq!(executor.probe_executor(kms::NETWORK), Ok(()));
    let durable = kms.state_digest()?;

    let binding = PrincipalKeyBinding::new(
        b"movement-readiness-owner".to_vec(),
        kms::NETWORK,
        KeyClass::HumanPrimary,
        kms::PROVIDER,
    )
    .map_err(|error| format!("{error:?}"))?;
    let reference = ProviderKeyReference::new(vec![7; 32]).map_err(|error| format!("{error:?}"))?;
    assert_eq!(executor.probe(), Err(KmsError::Refused));
    assert_eq!(executor.create_key(&binding).err(), Some(KmsError::Refused));
    assert_eq!(
        executor.describe_key(&binding, &reference).err(),
        Some(KmsError::Refused)
    );
    assert_eq!(
        executor.rotate_key(&binding, &reference).err(),
        Some(KmsError::Refused)
    );
    assert_eq!(
        executor.destroy_key(&binding, &reference),
        Err(KmsError::Refused)
    );
    assert_eq!(
        executor
            .export_primary_key(&binding, &reference)
            .map(|_| ())
            .err(),
        Some(KmsError::Refused)
    );
    for operation in [7, 11, 13] {
        assert_eq!(
            executor
                .evm_operation(operation, &binding, &reference, b"{}")
                .err(),
            Some(KmsError::Refused),
            "operation {operation} must stay outside the executor identity"
        );
    }

    assert_eq!(
        kms.remote("service", kms::PROVIDER, "localhost", "ca.der")?
            .probe_executor(kms::NETWORK),
        Err(KmsError::Refused)
    );
    assert!(kms
        .remote("stranger", kms::PROVIDER, "localhost", "ca.der")?
        .probe_executor(kms::NETWORK)
        .is_err());
    assert!(kms
        .remote("foreign", kms::PROVIDER, "localhost", "ca.der")?
        .probe_executor(kms::NETWORK)
        .is_err());
    assert_eq!(
        executor.probe_executor(kms::NETWORK + 1),
        Err(KmsError::Refused)
    );
    assert_eq!(
        kms.remote("executor", "another-kms", "localhost", "ca.der")?
            .probe_executor(kms::NETWORK),
        Err(KmsError::Refused)
    );
    assert!(kms
        .remote("executor", kms::PROVIDER, "kms.invalid", "ca.der")?
        .probe_executor(kms::NETWORK)
        .is_err());

    assert_eq!(executor.probe_executor(kms::NETWORK), Ok(()));
    assert_eq!(
        kms.state_digest()?,
        durable,
        "refused operations and executor probes must leave the durable state untouched"
    );
    Ok(())
}
