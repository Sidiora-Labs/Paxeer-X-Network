//! A real Human KMS process on loopback with its own mutual-TLS material: the
//! service identity, the restricted executor identity, an unpinned identity
//! from the same CA and a foreign identity from an unrelated CA.

use std::fs;
use std::net::{SocketAddr, TcpListener};
use std::os::unix::fs::{DirBuilderExt as _, PermissionsExt as _};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use layerx_client::lni::transport::{Limits, MutualTlsConfig};
use layerx_human_service::custody::RemoteKmsProvider;
use layerx_types::payload::{ActivityType, ModuleId};
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::RootCertStore;
use sha2::{Digest as _, Sha256};

type Result<T = ()> = std::result::Result<T, Box<dyn std::error::Error>>;

pub const PROVIDER: &str = "movement-readiness-kms";
pub const NETWORK: u32 = 77;
const CLOCK: [&str; 3] = [
    "LAYERX_RUNTIME_CLOCK_SOCKET",
    "LAYERX_RUNTIME_CLOCK_PID",
    "LAYERX_RUNTIME_CLOCK_UID",
];

pub struct Kms {
    pub root: PathBuf,
    pub address: SocketAddr,
    binary: PathBuf,
    child: Option<Child>,
}

impl Drop for Kms {
    fn drop(&mut self) {
        self.stop();
    }
}

/// The KMS executable Cargo builds into the same profile directory as
/// `executable`.
pub fn beside(executable: &Path) -> Result<PathBuf> {
    let binary = executable.with_file_name("layerx-human-kms");
    if !binary.is_file() {
        return Err(format!(
            "the prebuilt Human KMS executable {} is missing; build layerx-human-kms first",
            binary.display()
        )
        .into());
    }
    Ok(binary)
}

fn openssl(directory: &Path, arguments: &[&str]) -> Result {
    let output = Command::new("openssl")
        .current_dir(directory)
        .args(arguments)
        .stdin(Stdio::null())
        .output()?;
    if !output.status.success() {
        return Err(format!(
            "openssl {} failed: {}",
            arguments.join(" "),
            String::from_utf8_lossy(&output.stderr)
        )
        .into());
    }
    Ok(())
}

fn authority(directory: &Path, name: &str) -> Result {
    openssl(
        directory,
        &[
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            &format!("{name}.key"),
            "-out",
            &format!("{name}.pem"),
            "-days",
            "1",
            "-subj",
            &format!("/CN=movement readiness {name}"),
            "-addext",
            "basicConstraints=critical,CA:TRUE",
        ],
    )?;
    openssl(
        directory,
        &[
            "x509",
            "-in",
            &format!("{name}.pem"),
            "-outform",
            "DER",
            "-out",
            &format!("{name}.der"),
        ],
    )
}

fn identity(directory: &Path, name: &str, issuer: &str, extensions: &str) -> Result {
    openssl(
        directory,
        &[
            "req",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            &format!("{name}.key"),
            "-out",
            &format!("{name}.csr"),
            "-subj",
            &format!("/CN={name}"),
        ],
    )?;
    fs::write(directory.join(format!("{name}.ext")), extensions)?;
    openssl(
        directory,
        &[
            "x509",
            "-req",
            "-in",
            &format!("{name}.csr"),
            "-CA",
            &format!("{issuer}.pem"),
            "-CAkey",
            &format!("{issuer}.key"),
            "-CAcreateserial",
            "-out",
            &format!("{name}.pem"),
            "-days",
            "1",
            "-extfile",
            &format!("{name}.ext"),
        ],
    )?;
    openssl(
        directory,
        &[
            "x509",
            "-in",
            &format!("{name}.pem"),
            "-outform",
            "DER",
            "-out",
            &format!("{name}.der"),
        ],
    )?;
    openssl(
        directory,
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            &format!("{name}.key"),
            "-outform",
            "DER",
            "-out",
            &format!("{name}-key.der"),
        ],
    )
}

impl Kms {
    /// Mints the material under `root`, writes the provider's registry and
    /// seal, and starts the KMS until the executor identity's read-only probe
    /// answers.
    pub fn start(root: &Path, binary: &Path) -> Result<Self> {
        let _ = rustls::crypto::ring::default_provider().install_default();
        fs::DirBuilder::new().mode(0o700).create(root)?;
        authority(root, "ca")?;
        authority(root, "other-ca")?;
        identity(
            root,
            "server",
            "ca",
            "subjectAltName=DNS:localhost\nextendedKeyUsage=serverAuth\n",
        )?;
        for (name, issuer) in [
            ("service", "ca"),
            ("executor", "ca"),
            ("stranger", "ca"),
            ("foreign", "other-ca"),
        ] {
            identity(root, name, issuer, "extendedKeyUsage=clientAuth\n")?;
        }
        let mut seal = [0; 32];
        getrandom::fill(&mut seal).map_err(|error| error.to_string())?;
        fs::write(root.join("seal"), seal)?;
        let activity = ActivityType::new(ModuleId::Asset, 1)
            .map_err(|error| format!("{error:?}"))?
            .value();
        fs::write(
            root.join("registry.json"),
            format!(
                "{{\"network_id\":{NETWORK},\"protocol_version\":3,\"modules\":[{{\"module_id\":{},\"activity_types\":[{activity}]}}]}}",
                ModuleId::Asset as u16
            ),
        )?;
        for entry in fs::read_dir(root)? {
            fs::set_permissions(entry?.path(), fs::Permissions::from_mode(0o600))?;
        }
        let reserved = TcpListener::bind("127.0.0.1:0")?;
        let address = reserved.local_addr()?;
        drop(reserved);
        let mut kms = Self {
            root: root.to_path_buf(),
            address,
            binary: binary.to_path_buf(),
            child: None,
        };
        kms.restart()?;
        Ok(kms)
    }

    pub fn file(&self, name: &str) -> PathBuf {
        self.root.join(name)
    }

    /// Starts the stopped KMS on the same address, state and identities.
    pub fn restart(&mut self) -> Result {
        self.stop();
        let mut command = Command::new(&self.binary);
        command.env_clear();
        for name in CLOCK {
            let value = std::env::var_os(name).ok_or_else(|| {
                format!("{name} is unset; run the test under tools/runtime/run-with-clock.sh")
            })?;
            command.env(name, value);
        }
        for (suffix, value) in [
            ("LISTEN", self.address.to_string()),
            ("PROVIDER_REFERENCE", PROVIDER.to_owned()),
            ("STATE_DIR", self.file("state").display().to_string()),
            ("DEADLINE_SECONDS", "2".to_owned()),
            (
                "REGISTRY_FILE",
                self.file("registry.json").display().to_string(),
            ),
            ("CLIENT_CA_DER", self.file("ca.der").display().to_string()),
            (
                "TLS_CERT_DER",
                self.file("server.der").display().to_string(),
            ),
            (
                "TLS_KEY_DER",
                self.file("server-key.der").display().to_string(),
            ),
            (
                "CLIENT_CERT_DER",
                self.file("service.der").display().to_string(),
            ),
            (
                "EVM_CLIENT_CERT_DER",
                self.file("executor.der").display().to_string(),
            ),
            ("SEAL_SECRET_FILE", self.file("seal").display().to_string()),
        ] {
            command.env(format!("LAYERX_HUMAN_KMS_{suffix}"), value);
        }
        let log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(self.file("kms.log"))?;
        self.child = Some(
            command
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(log)
                .spawn()?,
        );
        let executor = self.remote("executor", PROVIDER, "localhost", "ca.der")?;
        let started = Instant::now();
        loop {
            if executor.probe_executor(NETWORK).is_ok() {
                return Ok(());
            }
            if let Some(status) = self
                .child
                .as_mut()
                .ok_or("the KMS process is missing")?
                .try_wait()?
            {
                return Err(format!(
                    "the KMS exited at startup with {status}: {}",
                    fs::read_to_string(self.file("kms.log"))?
                )
                .into());
            }
            if started.elapsed() > Duration::from_secs(20) {
                return Err("the KMS did not answer the executor probe within 20 seconds".into());
            }
            thread::sleep(Duration::from_millis(50));
        }
    }

    pub fn stop(&mut self) {
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }

    fn signal(&self, signal: rustix::process::Signal) -> Result {
        let child = self
            .child
            .as_ref()
            .ok_or("the KMS process is not running")?;
        let pid = rustix::process::Pid::from_raw(i32::try_from(child.id())?)
            .ok_or("the KMS process id is invalid")?;
        rustix::process::kill_process(pid, signal)?;
        Ok(())
    }

    /// Stalls the KMS so connections are accepted by the kernel but never
    /// answered.
    pub fn pause(&self) -> Result {
        self.signal(rustix::process::Signal::STOP)
    }

    pub fn resume(&self) -> Result {
        self.signal(rustix::process::Signal::CONT)
    }

    /// The digest of the sealed durable state; any persisted change, signing
    /// record or key creation changes it.
    pub fn state_digest(&self) -> Result<[u8; 32]> {
        Ok(Sha256::digest(fs::read(self.file("state").join("state.aead"))?).into())
    }

    /// The production client the movement provider builds, presenting
    /// `identity` to this KMS under `provider` and `server_name`, trusting
    /// `trust` as the server CA.
    pub fn remote(
        &self,
        identity: &str,
        provider: &str,
        server_name: &str,
        trust: &str,
    ) -> Result<RemoteKmsProvider> {
        let mut roots = RootCertStore::empty();
        roots.add(CertificateDer::from(fs::read(self.file(trust))?))?;
        let certificate = CertificateDer::from(fs::read(self.file(&format!("{identity}.der")))?);
        let key = PrivateKeyDer::try_from(fs::read(self.file(&format!("{identity}-key.der")))?)?;
        let tls = MutualTlsConfig::new(roots, vec![certificate], key)
            .map_err(|error| format!("{error:?}"))?;
        Ok(RemoteKmsProvider::new(
            provider,
            self.address,
            server_name,
            tls,
            Limits {
                maximum_frame_bytes: 2_097_152,
                maximum_connections: 1,
                maximum_streams: 1,
                maximum_queued_bytes: 2_097_152,
                deadline: Duration::from_secs(2),
            },
        )
        .map_err(|error| format!("{error:?}"))?)
    }
}
