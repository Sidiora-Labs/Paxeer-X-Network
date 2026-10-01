//! Operator cosign contract: every approved bridge attestor runs as an
//! independent operator with its own `layerx-mirror-signer bridge` process,
//! its own attestor and fee-payer handles, its own journal, cosign and
//! delivery directories and its own `layerx-bridge-cosign` transport process
//! over pinned mutual TLS. An operator's collection reaches the approved
//! threshold only through shares the transport delivered from the others.
//! Shares are untrusted: wrong-digest, unknown, duplicate and malformed
//! entries never count, an outsider's share is refused by every operator and
//! below the threshold assembly keeps waiting. Killing and restarting one
//! operator keeps every share already published, delivery to a stopped
//! operator resumes on its restart, and republishing never adds a second
//! share.

mod support;

use std::fs;
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use k256::ecdsa::SigningKey;
use layerx_bridge_relayer::attestation::{
    assemble_signatures, ethereum_address, recover_signer, uint256_from_u64, InboundAttestation,
    SignatureError,
};
use layerx_bridge_relayer::cosign::transport::{spki_pin, ENABLE_VARIABLE};
use layerx_bridge_relayer::cosign::CosignDirectory;
use layerx_bridge_relayer::hex;
use layerx_bridge_relayer::signer::{Attestor, ATTEST_INBOUND_DOMAIN, PAXEER_TRANSACTION_DOMAIN};
use layerx_mirror::signer::{
    RemoteChainSigner, RemoteSignerConfig, SignerEndpoint, SigningAlgorithm,
};
use openssl::asn1::Asn1Time;
use openssl::bn::{BigNum, MsbOption};
use openssl::ec::{EcGroup, EcKey};
use openssl::hash::MessageDigest;
use openssl::nid::Nid;
use openssl::pkey::{PKey, Private};
use openssl::x509::extension::{
    BasicConstraints, ExtendedKeyUsage, KeyUsage, SubjectAlternativeName,
};
use openssl::x509::{X509Name, X509};

const HANDLE: &str = "bridge-attestor";
const TRANSPORT: &str = env!("CARGO_BIN_EXE_layerx-bridge-cosign");
const WAIT: Duration = Duration::from_secs(60);

/// The real bridge signer binary. A missing binary is a refused fixture,
/// never a skipped or simulated signer.
fn signer_binary() -> PathBuf {
    if let Some(path) = std::env::var_os("LAYERX_MIRROR_SIGNER_BIN") {
        return PathBuf::from(path);
    }
    let target = std::env::var_os("CARGO_TARGET_DIR").map_or_else(
        || Path::new(env!("CARGO_MANIFEST_DIR")).join("../../target"),
        PathBuf::from,
    );
    let binary = target.join("debug/layerx-mirror-signer");
    assert!(
        binary.is_file(),
        "refused: the real bridge signer binary {} is missing; build it with \
         `cargo build --locked --manifest-path interop/Cargo.toml -p layerx-mirror-signer` \
         or name it in LAYERX_MIRROR_SIGNER_BIN",
        binary.display()
    );
    binary
}

/// The approved membership size and threshold from the bridge manifest.
fn approved_quorum() -> (usize, usize) {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../bridge/deploy/attestors.json");
    let text = fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("approved membership {}: {error}", path.display()));
    let document: serde_json::Value =
        serde_json::from_str(&text).unwrap_or_else(|error| panic!("attestors.json: {error}"));
    let threshold = document["threshold"]
        .as_u64()
        .and_then(|value| usize::try_from(value).ok())
        .unwrap_or_else(|| panic!("attestors.json has no threshold"));
    let members = document["attestors"].as_array().map_or(0, Vec::len);
    assert!(
        threshold >= 2 && threshold <= members,
        "approved threshold {threshold} of {members}"
    );
    (members, threshold)
}

fn wait_for(what: &str, mut done: impl FnMut() -> bool) {
    let deadline = Instant::now() + WAIT;
    while !done() {
        assert!(Instant::now() < deadline, "timed out waiting for {what}");
        std::thread::sleep(Duration::from_millis(50));
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .unwrap_or_else(|error| panic!("port: {error}"))
        .local_addr()
        .unwrap_or_else(|error| panic!("port: {error}"))
        .port()
}

fn ec_key() -> PKey<Private> {
    let group = EcGroup::from_curve_name(Nid::X9_62_PRIME256V1).unwrap();
    PKey::from_ec_key(EcKey::generate(&group).unwrap()).unwrap()
}

fn certificate(name: &str, key: &PKey<Private>, issuer: Option<(&X509, &PKey<Private>)>) -> X509 {
    let mut subject = X509Name::builder().unwrap();
    subject.append_entry_by_text("CN", name).unwrap();
    let subject = subject.build();
    let mut builder = X509::builder().unwrap();
    builder.set_version(2).unwrap();
    let mut serial = BigNum::new().unwrap();
    serial.rand(64, MsbOption::MAYBE_ZERO, false).unwrap();
    builder
        .set_serial_number(&serial.to_asn1_integer().unwrap())
        .unwrap();
    builder.set_subject_name(&subject).unwrap();
    builder
        .set_not_before(&Asn1Time::days_from_now(0).unwrap())
        .unwrap();
    builder
        .set_not_after(&Asn1Time::days_from_now(1).unwrap())
        .unwrap();
    builder.set_pubkey(key).unwrap();
    if let Some((ca, ca_key)) = issuer {
        builder.set_issuer_name(ca.subject_name()).unwrap();
        let san = SubjectAlternativeName::new()
            .dns(name)
            .build(&builder.x509v3_context(Some(ca), None))
            .unwrap();
        builder.append_extension(san).unwrap();
        builder
            .append_extension(
                ExtendedKeyUsage::new()
                    .server_auth()
                    .client_auth()
                    .build()
                    .unwrap(),
            )
            .unwrap();
        builder.sign(ca_key, MessageDigest::sha256()).unwrap();
    } else {
        builder.set_issuer_name(&subject).unwrap();
        builder
            .append_extension(BasicConstraints::new().critical().ca().build().unwrap())
            .unwrap();
        builder
            .append_extension(
                KeyUsage::new()
                    .critical()
                    .key_cert_sign()
                    .crl_sign()
                    .build()
                    .unwrap(),
            )
            .unwrap();
        builder.sign(key, MessageDigest::sha256()).unwrap();
    }
    builder.build()
}

/// The test trust anchor every operator's transport authenticates against.
struct Ca {
    cert: X509,
    key: PKey<Private>,
    path: PathBuf,
}

impl Ca {
    fn new(root: &Path) -> Self {
        let key = ec_key();
        let cert = certificate("operator-test-ca", &key, None);
        let path = root.join("ca.pem");
        fs::write(&path, cert.to_pem().unwrap()).unwrap();
        Self { cert, key, path }
    }
}

/// One operator: its own attestor and fee-payer keys, key manifest, signer
/// process, journal, cosign and delivery directories, TLS identity and
/// transport process. Nothing is shared between operators but the CA.
struct Operator {
    name: String,
    key: SigningKey,
    fee_payer: SigningKey,
    fee_payer_handle: String,
    home: PathBuf,
    socket: PathBuf,
    cosign: PathBuf,
    delivery: PathBuf,
    port: u16,
    tls_key: PathBuf,
    tls_cert: PathBuf,
    pin: [u8; 32],
    config: PathBuf,
    log: PathBuf,
    child: Option<Child>,
    transport: Option<Child>,
}

impl Operator {
    fn new(root: &Path, ca: &Ca, index: u8) -> Self {
        let name = format!("operator-{index}");
        let home = root.join(&name);
        let cosign = home.join("cosign");
        let delivery = home.join("delivery");
        let secrets = home.join("secrets");
        for directory in [home.join("journal"), cosign.clone(), delivery.clone()] {
            fs::create_dir_all(&directory).unwrap_or_else(|error| panic!("home: {error}"));
        }
        fs::create_dir_all(&secrets).unwrap_or_else(|error| panic!("secrets: {error}"));
        fs::set_permissions(&secrets, fs::Permissions::from_mode(0o700))
            .unwrap_or_else(|error| panic!("secrets mode: {error}"));
        let key = support::key(0x40 + index);
        let fee_payer = support::key(0x60 + index);
        let fee_payer_handle = format!("bridge-fee-payer-{index}");
        let key_file = secrets.join("attestor.key");
        let fee_payer_file = secrets.join("fee-payer.key");
        for (path, last) in [(&key_file, 0x40 + index), (&fee_payer_file, 0x60 + index)] {
            fs::write(path, hex::encode(&support::secret(last)))
                .unwrap_or_else(|error| panic!("key file: {error}"));
            fs::set_permissions(path, fs::Permissions::from_mode(0o400))
                .unwrap_or_else(|error| panic!("key mode: {error}"));
        }
        let domain = |domain: &'static [u8]| {
            std::str::from_utf8(domain).unwrap_or_else(|_| panic!("domain"))
        };
        let path = |path: &Path| {
            path.to_str()
                .unwrap_or_else(|| panic!("key path"))
                .to_owned()
        };
        let manifest = serde_json::json!({"keys": [{
            "handle": HANDLE,
            "algorithm": "secp256k1",
            "key_file": path(&key_file),
            "domains": [domain(ATTEST_INBOUND_DOMAIN)],
        }, {
            "handle": fee_payer_handle,
            "algorithm": "secp256k1",
            "key_file": path(&fee_payer_file),
            "domains": [domain(PAXEER_TRANSACTION_DOMAIN)],
        }]});
        fs::write(home.join("keys.json"), manifest.to_string())
            .unwrap_or_else(|error| panic!("manifest: {error}"));
        let tls_key_pair = ec_key();
        let tls = certificate(&name, &tls_key_pair, Some((&ca.cert, &ca.key)));
        let tls_cert = secrets.join("certificate.pem");
        let tls_key = secrets.join("private-key.pem");
        fs::write(&tls_cert, tls.to_pem().unwrap()).unwrap();
        fs::write(&tls_key, tls_key_pair.private_key_to_pem_pkcs8().unwrap()).unwrap();
        fs::set_permissions(&tls_key, fs::Permissions::from_mode(0o400)).unwrap();
        fs::set_permissions(&tls_cert, fs::Permissions::from_mode(0o400)).unwrap();
        let socket = home.join("signer.sock");
        let mut operator = Self {
            name,
            key,
            fee_payer,
            fee_payer_handle,
            config: home.join("transport.json"),
            log: home.join("transport.log"),
            home,
            socket,
            cosign,
            delivery,
            port: free_port(),
            tls_key,
            tls_cert,
            pin: spki_pin(&tls).unwrap(),
            child: None,
            transport: None,
        };
        operator.start();
        operator
    }

    fn listen(&self) -> SocketAddr {
        SocketAddr::from(([127, 0, 0, 1], self.port))
    }

    /// Writes this operator's transport configuration naming `peers`.
    fn configure(&self, ca: &Ca, peers: &[&Self]) {
        let peers: Vec<_> = peers
            .iter()
            .map(|peer| {
                serde_json::json!({
                    "attestor": hex::prefixed(&peer.address()),
                    "address": peer.listen().to_string(),
                    "server_name": peer.name,
                    "spki_sha256": hex::prefixed(&peer.pin),
                })
            })
            .collect();
        let config = serde_json::json!({
            "attestor": hex::prefixed(&self.address()),
            "listen": self.listen().to_string(),
            "server_name": self.name,
            "trust_anchor": ca.path,
            "certificate": self.tls_cert,
            "private_key": self.tls_key,
            "cosign_directory": self.cosign,
            "delivery_directory": self.delivery,
            "peers": peers,
            "timeout_ms": 2000,
            "sweep_interval_ms": 100,
            "max_connections": 16,
        });
        fs::write(
            &self.config,
            serde_json::to_vec_pretty(&config).unwrap_or_else(|error| panic!("{error}")),
        )
        .unwrap_or_else(|error| panic!("transport config: {error}"));
    }

    fn start(&mut self) {
        let _ = fs::remove_file(&self.socket);
        let child = Command::new(signer_binary())
            .arg("bridge")
            .arg("--socket")
            .arg(&self.socket)
            .arg("--keys")
            .arg(self.home.join("keys.json"))
            .stdin(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("spawn bridge signer: {error}"));
        self.child = Some(child);
        let started = Instant::now();
        while !self.socket.exists() {
            assert!(
                started.elapsed() < Duration::from_secs(10),
                "bridge signer did not bind"
            );
            std::thread::sleep(Duration::from_millis(20));
        }
    }

    fn kill(&mut self) {
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }

    /// Starts this operator's real cosign transport process.
    fn start_transport(&mut self) {
        assert!(self.transport.is_none());
        let log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log)
            .unwrap_or_else(|error| panic!("transport log: {error}"));
        let child = Command::new(TRANSPORT)
            .env(ENABLE_VARIABLE, &self.config)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(log)
            .spawn()
            .unwrap_or_else(|error| panic!("spawn cosign transport: {error}"));
        self.transport = Some(child);
        let deadline = Instant::now() + WAIT;
        while TcpStream::connect(self.listen()).is_err() {
            assert!(Instant::now() < deadline, "{} never listened", self.name);
            assert!(
                self.transport
                    .as_mut()
                    .and_then(|child| child.try_wait().ok())
                    .is_some_and(|status| status.is_none()),
                "{} transport exited: {}",
                self.name,
                self.transport_log()
            );
            std::thread::sleep(Duration::from_millis(20));
        }
    }

    fn kill_transport(&mut self) {
        if let Some(mut child) = self.transport.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }

    fn transport_log(&self) -> String {
        fs::read_to_string(&self.log).unwrap_or_default()
    }

    fn remote(&self, handle: &str, key: &SigningKey) -> RemoteChainSigner {
        RemoteChainSigner::new(RemoteSignerConfig {
            endpoint: SignerEndpoint::Uds {
                socket: self.socket.clone(),
            },
            algorithm: SigningAlgorithm::Secp256k1Recoverable,
            key_handle: handle.to_owned(),
            public_key: support::public_key(key),
            timeout: Duration::from_secs(5),
        })
        .unwrap_or_else(|error| panic!("remote signer: {error:?}"))
    }

    fn attestor(&self) -> Attestor {
        Attestor::new(self.remote(HANDLE, &self.key))
            .unwrap_or_else(|error| panic!("attestor: {error:?}"))
    }

    fn address(&self) -> [u8; 20] {
        ethereum_address(self.key.verifying_key())
    }

    fn directory(&self) -> CosignDirectory {
        CosignDirectory::new(self.cosign.clone())
    }

    fn collect(&self, digest: &[u8; 32]) -> Vec<[u8; 65]> {
        self.directory().collect(digest)
    }

    fn holds(&self, digest: &[u8; 32], signer: &[u8; 20]) -> bool {
        digest_directory(&self.cosign, digest)
            .join(format!("{}.sig", hex::encode(signer)))
            .is_file()
    }

    /// Signs through the real signer process and publishes the share into
    /// this operator's own cosign directory only.
    fn publish(&self, attestation: &InboundAttestation) -> [u8; 65] {
        let attestor = self.attestor();
        assert_eq!(attestor.address(), self.address());
        let signature = attestor
            .sign_inbound(attestation)
            .unwrap_or_else(|error| panic!("sign: {error:?}"));
        self.directory()
            .publish(&attestation.digest(), &attestor.address(), &signature)
            .unwrap_or_else(|error| panic!("publish: {error:?}"));
        signature
    }
}

impl Drop for Operator {
    fn drop(&mut self) {
        self.kill_transport();
        self.kill();
    }
}

fn attestation(log_index: u64) -> InboundAttestation {
    let mut recipient = [0_u8; 32];
    recipient[31] = 0x77;
    InboundAttestation {
        chain_id: 1,
        vault: [0x11; 20],
        tx_hash: [0xab; 32],
        log_index,
        recipient,
        asset: [0x22; 20],
        amount: uint256_from_u64(1_000_000),
    }
}

fn digest_directory(shared: &Path, digest: &[u8; 32]) -> PathBuf {
    shared.join(hex::encode(digest))
}

/// Waits until every operator in `receivers` holds `signer`'s share for
/// `digest`, delivered by the transport.
fn wait_delivered(receivers: &[&Operator], digest: &[u8; 32], signer: &[u8; 20]) {
    for receiver in receivers {
        wait_for(
            &format!("delivery of {} to {}", hex::prefixed(signer), receiver.name),
            || receiver.holds(digest, signer),
        );
    }
}

#[test]
fn the_approved_threshold_assembles_only_from_distinct_real_operator_shares() {
    let (size, threshold) = approved_quorum();
    assert!(
        size > threshold,
        "a member beyond the quorum stays reachable"
    );
    let root = support::work_directory("operator-cosign");
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700))
        .unwrap_or_else(|error| panic!("root mode: {error}"));
    let ca = Ca::new(&root);
    let mut operators: Vec<Operator> = (0..=u8::try_from(size).unwrap_or(u8::MAX))
        .map(|index| Operator::new(&root, &ca, index))
        .collect();
    let mut outsider = operators.pop().unwrap_or_else(|| panic!("outsider"));
    let members: Vec<[u8; 20]> = operators.iter().map(Operator::address).collect();
    let mut distinct = members.clone();
    distinct.sort_unstable();
    distinct.dedup();
    assert_eq!(
        distinct.len(),
        size,
        "operators hold distinct attestor keys"
    );
    assert!(!members.contains(&outsider.address()));
    for (index, operator) in operators.iter().enumerate() {
        assert_ne!(operator.fee_payer_handle, HANDLE);
        assert_ne!(
            support::public_key(&operator.fee_payer),
            support::public_key(&operator.key),
            "the fee payer is not the attestor key"
        );
        for other in &operators[index + 1..] {
            assert_ne!(operator.home.join("journal"), other.home.join("journal"));
            assert_ne!(operator.socket, other.socket);
            assert_ne!(operator.cosign, other.cosign);
            assert_ne!(operator.delivery, other.delivery);
            assert_ne!(operator.port, other.port);
            assert_ne!(operator.pin, other.pin);
            assert_ne!(operator.fee_payer_handle, other.fee_payer_handle);
            assert_ne!(
                support::public_key(&operator.fee_payer),
                support::public_key(&other.fee_payer),
                "fee payers are independent"
            );
        }
    }

    let wanted = attestation(7);
    let digest = wanted.digest();
    let other = attestation(8);

    // The fee-payer handle never signs an attestation.
    for operator in &operators {
        let fee_payer =
            Attestor::new(operator.remote(&operator.fee_payer_handle, &operator.fee_payer))
                .unwrap_or_else(|error| panic!("fee payer: {error:?}"));
        assert!(
            fee_payer.sign_inbound(&wanted).is_err(),
            "{} fee payer refused attestation",
            operator.name
        );
    }

    // Each member delivers to every other member; the outsider is pinned by nobody.
    for operator in &operators {
        let peers: Vec<&Operator> = operators
            .iter()
            .filter(|peer| peer.name != operator.name)
            .collect();
        operator.configure(&ca, &peers);
    }
    outsider.configure(&ca, &operators.iter().collect::<Vec<_>>());
    for operator in &mut operators {
        operator.start_transport();
    }
    outsider.start_transport();

    // Below the threshold every operator's own collection keeps waiting and
    // never yields a partial set, once the transport delivered every share.
    for operator in &operators[..threshold - 1] {
        operator.publish(&wanted);
    }
    for signer in &members[..threshold - 1] {
        wait_delivered(&operators.iter().collect::<Vec<_>>(), &digest, signer);
    }
    for operator in &operators {
        assert_eq!(
            assemble_signatures(&digest, operator.collect(&digest), &members, threshold),
            Err(SignatureError::BelowThreshold {
                signatures: threshold - 1,
                threshold
            }),
            "{} waits below the threshold",
            operator.name
        );
    }

    // An unknown signer's valid share is refused by every operator's transport.
    let outsider_share = outsider.publish(&wanted);
    let outsider_pending =
        |peer: &Operator| format!("delivery to {} pending", hex::prefixed(&peer.address()));
    for operator in &operators {
        wait_for(&format!("{} refusing the outsider", operator.name), || {
            operator
                .transport_log()
                .contains("refused inbound share: unpinned client")
                && outsider
                    .transport_log()
                    .contains(&outsider_pending(operator))
        });
        assert!(
            !operator.holds(&digest, &outsider.address()),
            "{} refused the outsider's share",
            operator.name
        );
    }
    assert!(
        fs::read_dir(&outsider.delivery)
            .into_iter()
            .flatten()
            .flatten()
            .all(|entry| !entry.file_name().to_string_lossy().ends_with(".ack")),
        "no operator acknowledged the outsider"
    );

    // Hostile entries in one operator's collection: the outsider's valid share,
    // a member's share over another digest, a duplicate under another spelling,
    // malformed, misnamed and oversized entries.
    let target = &operators[threshold];
    let directory = digest_directory(&target.cosign, &digest);
    let unknown = directory.join(format!("{}.sig", hex::encode(&outsider.address())));
    fs::write(&unknown, hex::prefixed(&outsider_share))
        .unwrap_or_else(|error| panic!("unknown share: {error}"));
    assert_eq!(target.collect(&digest).len(), threshold);
    assert_eq!(
        assemble_signatures(&digest, target.collect(&digest), &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        }),
        "an unknown signer's valid share never counts"
    );
    let last = &operators[threshold - 1];
    let foreign = last.publish(&other);
    let first = operators[0].address();
    let first_share = fs::read_to_string(directory.join(format!("{}.sig", hex::encode(&first))))
        .unwrap_or_else(|error| panic!("first share: {error}"));
    let hostile = [
        (
            directory.join(format!("{}.sig", hex::encode(&last.address()))),
            hex::prefixed(&foreign),
        ),
        (
            directory.join(format!("{}.sig", hex::encode(&first).to_uppercase())),
            first_share.clone(),
        ),
        (
            directory.join(format!("{}.sig", hex::encode(&[0x99; 20]))),
            "0xnot-a-signature".to_owned(),
        ),
        (directory.join("garbage.sig"), first_share),
        (
            directory.join(format!("{}.sig", hex::encode(&members[size - 1]))),
            "x".repeat(4096),
        ),
    ];
    for (path, text) in &hostile {
        fs::write(path, text).unwrap_or_else(|error| panic!("hostile share: {error}"));
    }
    assert_eq!(
        assemble_signatures(&digest, target.collect(&digest), &members, threshold),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        }),
        "wrong-digest, duplicate and malformed shares never count"
    );
    for (path, _) in &hostile {
        fs::remove_file(path).unwrap_or_else(|error| panic!("hostile share: {error}"));
    }

    // Republishing an already delivered member leaves every waiting set exactly.
    operators[1].publish(&wanted);
    for operator in &operators {
        assert_eq!(
            assemble_signatures(&digest, operator.collect(&digest), &members, threshold),
            Err(SignatureError::BelowThreshold {
                signatures: threshold - 1,
                threshold
            })
        );
    }

    // The last quorum operator dies, signer and transport, while another
    // operator's transport is down; the restarted operator completes the
    // quorum, every share published before the restart is preserved, and the
    // stopped operator reaches the threshold only once delivery resumes.
    let late = size - 1;
    let before: Vec<Vec<[u8; 65]>> = operators
        .iter()
        .map(|operator| operator.collect(&digest))
        .collect();
    operators[threshold - 1].kill_transport();
    operators[threshold - 1].kill();
    assert!(
        operators[threshold - 1]
            .attestor()
            .sign_inbound(&wanted)
            .is_err(),
        "a dead signer process produces no share"
    );
    operators[late].kill_transport();
    let offset = operators[threshold - 1].transport_log().len();
    operators[threshold - 1].start();
    operators[threshold - 1].start_transport();
    for (operator, shares) in operators.iter().zip(&before) {
        for share in shares {
            assert!(
                operator.collect(&digest).contains(share),
                "restart preserves published shares"
            );
        }
    }
    operators[threshold - 1].publish(&wanted);
    let completing = members[threshold - 1];
    let reached: Vec<&Operator> = operators
        .iter()
        .filter(|operator| operator.name != operators[late].name)
        .collect();
    wait_delivered(&reached, &digest, &completing);
    let late_pending = format!("delivery to {} pending", hex::prefixed(&members[late]));
    wait_for("delivery to the stopped operator pending", || {
        operators[threshold - 1].transport_log()[offset..].contains(&late_pending)
    });
    assert_eq!(
        assemble_signatures(
            &digest,
            operators[late].collect(&digest),
            &members,
            threshold
        ),
        Err(SignatureError::BelowThreshold {
            signatures: threshold - 1,
            threshold
        }),
        "an operator not yet reached by delivery keeps waiting"
    );
    let assembled =
        assemble_signatures(&digest, operators[0].collect(&digest), &members, threshold)
            .unwrap_or_else(|error| panic!("quorum: {error:?}"));
    assert_eq!(assembled.len(), threshold);
    let signers: Vec<[u8; 20]> = assembled
        .iter()
        .map(|signature| {
            recover_signer(&digest, signature).unwrap_or_else(|error| panic!("{error:?}"))
        })
        .collect();
    assert!(
        signers.windows(2).all(|pair| pair[0] < pair[1]),
        "ascending distinct signers"
    );
    assert!(signers.iter().all(|signer| members.contains(signer)));
    for operator in &reached {
        assert_eq!(
            assemble_signatures(&digest, operator.collect(&digest), &members, threshold),
            Ok(assembled.clone()),
            "{} assembles the same authorization",
            operator.name
        );
    }
    operators[late].start_transport();
    wait_delivered(&[&operators[late]], &digest, &completing);
    assert_eq!(
        assemble_signatures(
            &digest,
            operators[late].collect(&digest),
            &members,
            threshold
        ),
        Ok(assembled.clone())
    );

    // Once-only completion: every quorum operator republishing after a
    // restart yields the identical authorization at every operator, never a
    // second or larger set.
    for operator in &mut operators[..threshold] {
        operator.kill_transport();
        operator.kill();
        operator.start();
        operator.start_transport();
        operator.publish(&wanted);
    }
    for operator in &operators {
        assert_eq!(
            assemble_signatures(&digest, operator.collect(&digest), &members, threshold),
            Ok(assembled.clone()),
            "{} keeps the identical authorization",
            operator.name
        );
    }
    // Every remaining member cosigns too; each operator still assembles
    // exactly one threshold-sized set of distinct members.
    for operator in &operators[threshold..] {
        operator.publish(&wanted);
    }
    for signer in &members {
        wait_delivered(&operators.iter().collect::<Vec<_>>(), &digest, signer);
    }
    let complete = assemble_signatures(&digest, operators[0].collect(&digest), &members, threshold)
        .unwrap_or_else(|error| panic!("complete quorum: {error:?}"));
    assert_eq!(complete.len(), threshold);
    for operator in &operators {
        assert_eq!(
            assemble_signatures(&digest, operator.collect(&digest), &members, threshold),
            Ok(complete.clone())
        );
    }

    // Private keys never enter any operator's cosign or delivery storage.
    let mut secrets = Vec::new();
    for operator in operators.iter().chain(std::iter::once(&outsider)) {
        secrets.push(hex::encode(&operator.key.to_bytes()[..]));
        secrets.push(hex::encode(&operator.fee_payer.to_bytes()[..]));
        let pem = fs::read_to_string(&operator.tls_key)
            .unwrap_or_else(|error| panic!("tls key: {error}"));
        secrets.extend(
            pem.lines()
                .filter(|line| !line.starts_with("-----") && line.len() >= 32)
                .map(str::to_owned),
        );
    }
    for operator in operators.iter().chain(std::iter::once(&outsider)) {
        for entry in walk(&operator.cosign)
            .into_iter()
            .chain(walk(&operator.delivery))
        {
            let text = fs::read(&entry).unwrap_or_default();
            let text = String::from_utf8_lossy(&text);
            for secret in &secrets {
                assert!(
                    !text.contains(secret.as_str()),
                    "{} carries a private key",
                    entry.display()
                );
            }
        }
    }
    outsider.kill_transport();
    drop(outsider);
}

fn walk(directory: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    for entry in fs::read_dir(directory).into_iter().flatten().flatten() {
        let path = entry.path();
        if path.is_dir() {
            files.extend(walk(&path));
        } else {
            files.push(path);
        }
    }
    files
}
