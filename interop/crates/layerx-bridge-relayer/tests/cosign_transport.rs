//! Real-process qualification of the cosign share transport: three child
//! processes of the built binary, an ephemeral CA, generated attestor keys,
//! hostile wire inputs, a byte-flipping relay and kill -9 restarts.

use std::collections::BTreeMap;
use std::fs;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use k256::ecdsa::SigningKey;
use layerx_bridge_relayer::attestation::{
    assemble_signatures, ethereum_address, to_attestor_signature, SignatureError,
};
use layerx_bridge_relayer::cosign::transport::{
    encode_wire, spki_pin, ENABLE_VARIABLE, REPLY_ALREADY_HELD,
};
use layerx_bridge_relayer::cosign::CosignDirectory;
use layerx_bridge_relayer::hex;
use openssl::asn1::Asn1Time;
use openssl::bn::{BigNum, MsbOption};
use openssl::ec::{EcGroup, EcKey};
use openssl::hash::MessageDigest;
use openssl::nid::Nid;
use openssl::pkey::{PKey, Private};
use openssl::ssl::{SslConnector, SslMethod, SslVerifyMode, SslVersion};
use openssl::x509::extension::{
    BasicConstraints, ExtendedKeyUsage, KeyUsage, SubjectAlternativeName,
};
use openssl::x509::{X509Name, X509};
use sha2::{Digest as _, Sha256};

const BIN: &str = env!("CARGO_BIN_EXE_layerx-bridge-cosign");
const WAIT: Duration = Duration::from_secs(60);

fn temp_root(tag: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "cosign-transport-{tag}-{}-{}",
        std::process::id(),
        hex::encode(&random::<8>())
    ));
    fs::create_dir_all(&root).unwrap();
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700)).unwrap();
    root
}

fn random<const N: usize>() -> [u8; N] {
    let mut bytes = [0_u8; N];
    openssl::rand::rand_bytes(&mut bytes).unwrap();
    bytes
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

struct Ca {
    cert: X509,
    key: PKey<Private>,
    path: PathBuf,
}

fn make_ca(root: &Path, name: &str) -> Ca {
    let key = ec_key();
    let cert = certificate(name, &key, None);
    let path = root.join(format!("{name}.pem"));
    fs::write(&path, cert.to_pem().unwrap()).unwrap();
    Ca { cert, key, path }
}

struct Identity {
    cert_path: PathBuf,
    key_path: PathBuf,
    pin: [u8; 32],
}

fn issue(root: &Path, ca: &Ca, name: &str, tag: &str) -> Identity {
    let dir = root.join(format!("secrets-{name}-{tag}"));
    fs::create_dir_all(&dir).unwrap();
    fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
    let key = ec_key();
    let cert = certificate(name, &key, Some((&ca.cert, &ca.key)));
    let cert_path = dir.join("cert.pem");
    let key_path = dir.join("key.pem");
    fs::write(&cert_path, cert.to_pem().unwrap()).unwrap();
    fs::write(&key_path, key.private_key_to_pem_pkcs8().unwrap()).unwrap();
    fs::set_permissions(&key_path, fs::Permissions::from_mode(0o600)).unwrap();
    fs::set_permissions(&cert_path, fs::Permissions::from_mode(0o600)).unwrap();
    Identity {
        cert_path,
        key_path,
        pin: spki_pin(&cert).unwrap(),
    }
}

struct Node {
    name: String,
    signing: SigningKey,
    attestor: [u8; 20],
    port: u16,
    cosign: PathBuf,
    delivery: PathBuf,
    identity: Identity,
    config: PathBuf,
    log: PathBuf,
    child: Option<Child>,
}

impl Node {
    fn new(root: &Path, ca: &Ca, name: &str) -> Self {
        let signing = SigningKey::from_slice(&Sha256::digest(random::<32>())).unwrap();
        let attestor = ethereum_address(signing.verifying_key());
        let base = root.join(name);
        let cosign = base.join("cosign");
        let delivery = base.join("delivery");
        fs::create_dir_all(&cosign).unwrap();
        fs::create_dir_all(&delivery).unwrap();
        Self {
            name: name.to_owned(),
            signing,
            attestor,
            port: free_port(),
            cosign,
            delivery,
            identity: issue(root, ca, name, "1"),
            config: base.join("transport.json"),
            log: base.join("stderr.log"),
            child: None,
        }
    }

    fn address(&self) -> SocketAddr {
        SocketAddr::from(([127, 0, 0, 1], self.port))
    }

    fn sign(&self, digest: &[u8; 32]) -> [u8; 65] {
        let (signature, recovery) = self.signing.sign_prehash_recoverable(digest).unwrap();
        let mut bytes = [0_u8; 65];
        bytes[..64].copy_from_slice(&signature.to_bytes());
        bytes[64] = recovery.to_byte();
        to_attestor_signature(bytes).unwrap()
    }

    fn directory(&self) -> CosignDirectory {
        CosignDirectory::new(self.cosign.clone())
    }

    fn publish(&self, digest: &[u8; 32]) -> [u8; 65] {
        let signature = self.sign(digest);
        self.directory()
            .publish(digest, &self.attestor, &signature)
            .unwrap();
        signature
    }

    fn write_config(&self, ca: &Ca, peers: &[(&Node, [u8; 32])]) {
        let peers: Vec<_> = peers
            .iter()
            .map(|(peer, pin)| {
                serde_json::json!({
                    "attestor": hex::prefixed(&peer.attestor),
                    "address": peer.address().to_string(),
                    "server_name": peer.name,
                    "spki_sha256": hex::prefixed(pin),
                })
            })
            .collect();
        let config = serde_json::json!({
            "attestor": hex::prefixed(&self.attestor),
            "listen": self.address().to_string(),
            "server_name": self.name,
            "trust_anchor": ca.path,
            "certificate": self.identity.cert_path,
            "private_key": self.identity.key_path,
            "cosign_directory": self.cosign,
            "delivery_directory": self.delivery,
            "peers": peers,
            "timeout_ms": 2000,
            "sweep_interval_ms": 100,
            "max_connections": 16,
        });
        fs::write(&self.config, serde_json::to_vec_pretty(&config).unwrap()).unwrap();
    }

    fn start(&mut self) {
        assert!(self.child.is_none());
        let log = fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.log)
            .unwrap();
        let child = Command::new(BIN)
            .env(ENABLE_VARIABLE, &self.config)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(log)
            .spawn()
            .unwrap();
        self.child = Some(child);
        let deadline = Instant::now() + WAIT;
        while TcpStream::connect(self.address()).is_err() {
            assert!(Instant::now() < deadline, "{} never listened", self.name);
            assert!(
                self.child.as_mut().unwrap().try_wait().unwrap().is_none(),
                "{} exited: {}",
                self.name,
                fs::read_to_string(&self.log).unwrap_or_default()
            );
            std::thread::sleep(Duration::from_millis(20));
        }
    }

    fn kill(&mut self) {
        let mut child = self.child.take().unwrap();
        child.kill().unwrap();
        child.wait().unwrap();
    }

    fn holds(&self, digest: &[u8; 32], signer: &[u8; 20]) -> Option<String> {
        fs::read_to_string(
            self.cosign
                .join(hex::encode(digest))
                .join(format!("{}.sig", hex::encode(signer))),
        )
        .ok()
    }

    fn record(
        &self,
        peer: &[u8; 20],
        pin: &[u8; 32],
        digest: &[u8; 32],
        own: &[u8; 65],
        suffix: &str,
    ) -> PathBuf {
        let mut identity = Sha256::new();
        identity.update(self.attestor);
        identity.update(own);
        self.delivery.join(format!(
            "{}-{}-{}-{}.{suffix}",
            hex::encode(peer),
            hex::encode(pin),
            hex::encode(digest),
            hex::encode(&identity.finalize())
        ))
    }
}

impl Drop for Node {
    fn drop(&mut self) {
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port()
}

fn wait_for(what: &str, mut done: impl FnMut() -> bool) {
    let deadline = Instant::now() + WAIT;
    while !done() {
        assert!(Instant::now() < deadline, "timed out waiting for {what}");
        std::thread::sleep(Duration::from_millis(50));
    }
}

fn snapshot(root: &Path) -> BTreeMap<PathBuf, Vec<u8>> {
    let mut files = BTreeMap::new();
    let mut stack = vec![root.to_path_buf()];
    while let Some(dir) = stack.pop() {
        for entry in fs::read_dir(&dir).unwrap().flatten() {
            let path = entry.path();
            if path.is_dir() {
                files.insert(path.clone(), Vec::new());
                stack.push(path);
            } else {
                files.insert(path.clone(), fs::read(&path).unwrap_or_default());
            }
        }
    }
    files
}

fn raw_client(ca: &Path, identity: &Identity) -> SslConnector {
    let mut builder = SslConnector::builder(SslMethod::tls_client()).unwrap();
    builder
        .set_min_proto_version(Some(SslVersion::TLS1_3))
        .unwrap();
    builder.set_ca_file(ca).unwrap();
    builder
        .set_certificate_file(&identity.cert_path, openssl::ssl::SslFiletype::PEM)
        .unwrap();
    builder
        .set_private_key_file(&identity.key_path, openssl::ssl::SslFiletype::PEM)
        .unwrap();
    builder.set_verify(SslVerifyMode::PEER);
    builder.build()
}

/// Sends raw bytes over mutual TLS and returns the reply byte, if any.
fn exchange(connector: &SslConnector, address: SocketAddr, name: &str, wire: &[u8]) -> Option<u8> {
    let stream = TcpStream::connect(address).ok()?;
    stream
        .set_read_timeout(Some(Duration::from_secs(10)))
        .unwrap();
    let mut tls = connector.connect(name, stream).ok()?;
    if tls.write_all(wire).and_then(|()| tls.flush()).is_err() {
        return None;
    }
    let mut reply = [0_u8; 1];
    match tls.read(&mut reply) {
        Ok(1) => Some(reply[0]),
        _ => None,
    }
}

/// A TCP relay that flips one byte in every client-to-server chunk after the
/// first (the ClientHello), so every protected record is modified.
fn flipping_relay(target: SocketAddr) -> SocketAddr {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    std::thread::spawn(move || {
        for client in listener.incoming().flatten() {
            let Ok(server) = TcpStream::connect(target) else {
                continue;
            };
            let (mut client_read, mut server_write) =
                (client.try_clone().unwrap(), server.try_clone().unwrap());
            let (mut server_read, mut client_write) = (server, client);
            std::thread::spawn(move || {
                let mut buffer = [0_u8; 16384];
                let mut chunks = 0_usize;
                while let Ok(read) = client_read.read(&mut buffer) {
                    if read == 0 {
                        break;
                    }
                    if chunks > 0 {
                        buffer[read / 2] ^= 0x01;
                    }
                    chunks += 1;
                    if server_write.write_all(&buffer[..read]).is_err() {
                        break;
                    }
                }
                let _ = server_write.shutdown(std::net::Shutdown::Both);
            });
            std::thread::spawn(move || {
                let _ = std::io::copy(&mut server_read, &mut client_write);
                let _ = client_write.shutdown(std::net::Shutdown::Both);
            });
        }
    });
    address
}

fn digest(label: &str) -> [u8; 32] {
    Sha256::digest(label.as_bytes()).into()
}

#[test]
fn three_processes_deliver_authenticated_durable_shares() {
    let root = temp_root("main");
    let ca = make_ca(&root, "cosign-test-ca");
    let mut a = Node::new(&root, &ca, "node-a.cosign.test");
    let mut b = Node::new(&root, &ca, "node-b.cosign.test");
    let mut c = Node::new(&root, &ca, "node-c.cosign.test");
    let attestors = [a.attestor, b.attestor, c.attestor];
    let (pa, pb, pc) = (a.identity.pin, b.identity.pin, c.identity.pin);
    a.write_config(&ca, &[(&b, pb), (&c, pc)]);
    b.write_config(&ca, &[(&a, pa), (&c, pc)]);
    c.write_config(&ca, &[(&a, pa), (&b, pb)]);

    // ac_1/ac_5: two live processes stay below threshold; C is unavailable.
    let d = digest("withdrawal-1");
    let sig_a = a.publish(&d);
    let sig_b = b.publish(&d);
    let sig_c = c.publish(&d);
    // ac_5/ac_4: C already holds different bytes for A's share of d2.
    let d2 = digest("withdrawal-2");
    let held = c.cosign.join(hex::encode(&d2));
    fs::create_dir_all(&held).unwrap();
    let conflicting = format!("{}.sig", hex::encode(&a.attestor));
    fs::write(held.join(&conflicting), b"0xprevious-bytes").unwrap();
    let sig_a2 = a.publish(&d2);

    a.start();
    b.start();
    wait_for("A and B exchange", || {
        a.holds(&d, &b.attestor).is_some() && b.holds(&d, &a.attestor).is_some()
    });
    for node in [&a, &b] {
        assert!(matches!(
            assemble_signatures(&d, node.directory().collect(&d), &attestors, 3),
            Err(SignatureError::BelowThreshold {
                signatures: 2,
                threshold: 3
            })
        ));
    }
    wait_for("A ack from B", || {
        a.record(&b.attestor, &pb, &d, &sig_a, "ack").exists()
    });
    assert!(!a.record(&c.attestor, &pc, &d, &sig_a, "ack").exists());

    // ac_4/ac_5: kill -9 A, bring C up, restart A on the same directories.
    a.kill();
    c.start();
    a.start();
    wait_for("three-way exchange", || {
        attestors.iter().all(|signer| {
            [&a, &b, &c]
                .iter()
                .all(|node| node.holds(&d, signer).is_some())
        })
    });
    for node in [&a, &b, &c] {
        let assembled =
            assemble_signatures(&d, node.directory().collect(&d), &attestors, 3).unwrap();
        assert_eq!(assembled.len(), 3);
        for signature in [sig_a, sig_b, sig_c] {
            assert!(assembled.contains(&signature));
        }
    }
    wait_for("A ack from C after restart", || {
        a.record(&c.attestor, &pc, &d, &sig_a, "ack").exists()
    });

    // Durable explicit conflict, existing bytes never overwritten.
    wait_for("conflict record", || {
        a.record(&c.attestor, &pc, &d2, &sig_a2, "conflict")
            .exists()
    });
    assert_eq!(
        fs::read(held.join(&conflicting)).unwrap(),
        b"0xprevious-bytes"
    );
    assert!(!a.record(&c.attestor, &pc, &d2, &sig_a2, "ack").exists());
    a.kill();
    a.start();
    std::thread::sleep(Duration::from_millis(600));
    assert!(a
        .record(&c.attestor, &pc, &d2, &sig_a2, "conflict")
        .exists());
    assert_eq!(
        fs::read(held.join(&conflicting)).unwrap(),
        b"0xprevious-bytes"
    );

    // ac_4: identical redelivery is idempotent.
    let a_client = raw_client(&ca.path, &a.identity);
    let before = snapshot(&b.cosign);
    assert_eq!(
        exchange(
            &a_client,
            b.address(),
            &b.name,
            &encode_wire(&b.attestor, &d, &a.attestor, &sig_a)
        ),
        Some(REPLY_ALREADY_HELD)
    );
    assert_eq!(snapshot(&b.cosign), before);

    // ac_2/ac_3: hostile inputs leave no share or acknowledgement writes.
    let foreign_ca = make_ca(&root, "foreign-ca");
    let foreign = issue(&root, &foreign_ca, "node-a.cosign.test", "foreign");
    let unpinned = issue(&root, &ca, "node-x.cosign.test", "unpinned");
    let d3 = digest("withdrawal-3");
    let sig_a3 = a.sign(&d3);
    let good = encode_wire(&b.attestor, &d3, &a.attestor, &sig_a3);
    let mut hostile: Vec<(&str, SslConnector, Vec<u8>)> = Vec::new();
    let foreign_client = {
        let mut builder = SslConnector::builder(SslMethod::tls_client()).unwrap();
        builder
            .set_min_proto_version(Some(SslVersion::TLS1_3))
            .unwrap();
        builder.set_ca_file(&ca.path).unwrap();
        builder
            .set_certificate_file(&foreign.cert_path, openssl::ssl::SslFiletype::PEM)
            .unwrap();
        builder
            .set_private_key_file(&foreign.key_path, openssl::ssl::SslFiletype::PEM)
            .unwrap();
        builder.build()
    };
    hostile.push(("foreign CA", foreign_client, good.clone()));
    hostile.push((
        "unpinned client",
        raw_client(&ca.path, &unpinned),
        good.clone(),
    ));
    hostile.push((
        "wrong recipient",
        raw_client(&ca.path, &a.identity),
        encode_wire(&c.attestor, &d3, &a.attestor, &sig_a3),
    ));
    let sig_c3 = c.sign(&d3);
    hostile.push((
        "wrong signer",
        raw_client(&ca.path, &a.identity),
        encode_wire(&b.attestor, &d3, &c.attestor, &sig_c3),
    ));
    let mut flipped = sig_a3;
    flipped[10] ^= 0x55;
    hostile.push((
        "invalid signature",
        raw_client(&ca.path, &a.identity),
        encode_wire(&b.attestor, &d3, &a.attestor, &flipped),
    ));
    hostile.push((
        "invalid digest",
        raw_client(&ca.path, &a.identity),
        encode_wire(&b.attestor, &digest("other"), &a.attestor, &sig_a3),
    ));
    let mut short = good.clone();
    short[..4].copy_from_slice(&142_u32.to_be_bytes());
    short.pop();
    hostile.push(("short length", raw_client(&ca.path, &a.identity), short));
    let mut oversize = good.clone();
    oversize[..4].copy_from_slice(&144_u32.to_be_bytes());
    oversize.push(0);
    hostile.push(("oversize", raw_client(&ca.path, &a.identity), oversize));
    let mut huge = good.clone();
    huge[..4].copy_from_slice(&u32::MAX.to_be_bytes());
    hostile.push(("huge length", raw_client(&ca.path, &a.identity), huge));
    hostile.push((
        "truncated",
        raw_client(&ca.path, &a.identity),
        good[..100].to_vec(),
    ));
    let mut magic = good.clone();
    magic[4] = b'X';
    hostile.push(("bad magic", raw_client(&ca.path, &a.identity), magic));
    let mut version = good.clone();
    version[9] = 2;
    hostile.push(("bad version", raw_client(&ca.path, &a.identity), version));
    let mut surplus = good.clone();
    surplus.push(0);
    hostile.push(("surplus", raw_client(&ca.path, &a.identity), surplus));
    let shared_before = (
        snapshot(&b.cosign),
        snapshot(&b.delivery),
        snapshot(&a.delivery),
    );
    for (label, connector, wire) in &hostile {
        assert_eq!(
            exchange(connector, b.address(), &b.name, wire),
            None,
            "{label} was answered"
        );
    }
    // ac_3: modified ciphertext through a byte-flipping relay is refused.
    let relay = flipping_relay(b.address());
    assert_eq!(
        exchange(&a_client, relay, &b.name, &good),
        None,
        "relay modification answered"
    );
    // Hostname verification: B's certificate does not name node-c.
    assert_eq!(exchange(&a_client, b.address(), &c.name, &good), None);
    assert!(b.holds(&d3, &a.attestor).is_none());
    assert!(b.holds(&d3, &c.attestor).is_none());
    assert_eq!(
        (
            snapshot(&b.cosign),
            snapshot(&b.delivery),
            snapshot(&a.delivery)
        ),
        shared_before
    );
    // The untouched good frame is still admitted afterwards.
    assert_eq!(exchange(&a_client, b.address(), &b.name, &good), Some(1));
    assert!(b.holds(&d3, &a.attestor).is_some());

    // ac_2/ac_5: C rotates its certificate; A and B still pin the old key, so
    // A refuses C as a server and nothing for d4 reaches C.
    let d4 = digest("withdrawal-4");
    let sig_a4 = a.publish(&d4);
    c.kill();
    c.identity = issue(&root, &ca, "node-c.cosign.test", "2");
    let pc2 = c.identity.pin;
    assert_ne!(pc, pc2);
    c.write_config(&ca, &[(&a, pa), (&b, pb)]);
    c.start();
    std::thread::sleep(Duration::from_secs(2));
    assert!(c.holds(&d4, &a.attestor).is_none());
    assert!(!a.record(&c.attestor, &pc, &d4, &sig_a4, "ack").exists());
    assert!(!a.record(&c.attestor, &pc2, &d4, &sig_a4, "ack").exists());
    // A learns the new pin; old-pin acknowledgements are not inherited, so d
    // is re-offered under the new pin and acknowledged afresh.
    a.kill();
    a.write_config(&ca, &[(&b, pb), (&c, pc2)]);
    a.start();
    wait_for("delivery under the new pin", || {
        a.record(&c.attestor, &pc2, &d4, &sig_a4, "ack").exists()
            && a.record(&c.attestor, &pc2, &d, &sig_a, "ack").exists()
    });
    assert!(a.record(&c.attestor, &pc, &d, &sig_a, "ack").exists());
    assert!(c.holds(&d4, &a.attestor).is_some());

    // ac_5: bounded fair sweeps reach entries beyond the first page.
    let mut page = Vec::new();
    for index in 0..300 {
        let entry = digest(&format!("page-{index}"));
        page.push((entry, a.publish(&entry)));
    }
    page.sort_unstable_by_key(|entry| entry.0);
    wait_for("all 300 entries at B", || {
        page.iter()
            .all(|(entry, _)| b.holds(entry, &a.attestor).is_some())
    });
    let (last, last_sig) = page[299];
    wait_for("ack beyond the first page", || {
        a.record(&b.attestor, &pb, &last, &last_sig, "ack").exists()
    });

    // ac_6: shared directories hold signatures and public metadata only.
    for node in [&a, &b, &c] {
        for dir in [&node.cosign, &node.delivery] {
            for (path, bytes) in snapshot(dir) {
                assert!(
                    !String::from_utf8_lossy(&bytes).contains("PRIVATE KEY"),
                    "{} holds key material",
                    path.display()
                );
            }
        }
    }
    drop((a, b, c));
    let _ = fs::remove_dir_all(&root);
}

#[test]
fn enablement_and_bounds_are_enforced() {
    let root = temp_root("config");
    let ca = make_ca(&root, "config-ca");
    let a = Node::new(&root, &ca, "node-a.cosign.test");
    let b = Node::new(&root, &ca, "node-b.cosign.test");
    let run = |variable: Option<&Path>| {
        let mut command = Command::new(BIN);
        command
            .env_remove(ENABLE_VARIABLE)
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        if let Some(path) = variable {
            command.env(ENABLE_VARIABLE, path);
        }
        command.status().unwrap().code()
    };
    // Not enabled: the binary refuses to act as a transport at all.
    assert_eq!(run(None), Some(2));
    // ac_7: configured but missing, unreadable or invalid is fatal.
    assert_eq!(run(Some(&root.join("missing.json"))), Some(1));
    fs::write(root.join("invalid.json"), b"{not json").unwrap();
    assert_eq!(run(Some(&root.join("invalid.json"))), Some(1));
    a.write_config(&ca, &[(&b, b.identity.pin)]);
    let valid: serde_json::Value = serde_json::from_slice(&fs::read(&a.config).unwrap()).unwrap();
    // ac_6: every bound is finite and validated.
    for (field, value) in [
        ("timeout_ms", serde_json::json!(0)),
        ("timeout_ms", serde_json::json!(60_001)),
        ("sweep_interval_ms", serde_json::json!(0)),
        ("max_connections", serde_json::json!(0)),
        ("max_connections", serde_json::json!(65)),
        ("peers", serde_json::json!([])),
        ("unknown_field", serde_json::json!(1)),
    ] {
        let mut config = valid.clone();
        config[field] = value;
        let path = root.join(format!("bad-{field}.json"));
        fs::write(&path, serde_json::to_vec(&config).unwrap()).unwrap();
        assert_eq!(run(Some(&path)), Some(1), "{field} accepted");
    }
    let mut duplicate = valid.clone();
    let peer = duplicate["peers"][0].clone();
    duplicate["peers"].as_array_mut().unwrap().push(peer);
    let path = root.join("duplicate.json");
    fs::write(&path, serde_json::to_vec(&duplicate).unwrap()).unwrap();
    assert_eq!(run(Some(&path)), Some(1));
    // Private key readable by others, or placed in a shared directory.
    fs::set_permissions(&a.identity.key_path, fs::Permissions::from_mode(0o644)).unwrap();
    assert_eq!(run(Some(&a.config)), Some(1));
    fs::set_permissions(&a.identity.key_path, fs::Permissions::from_mode(0o600)).unwrap();
    let shared_key = a.cosign.join("key.pem");
    fs::copy(&a.identity.key_path, &shared_key).unwrap();
    fs::set_permissions(&shared_key, fs::Permissions::from_mode(0o600)).unwrap();
    let mut inside = valid;
    inside["private_key"] = serde_json::json!(shared_key);
    let path = root.join("inside.json");
    fs::write(&path, serde_json::to_vec(&inside).unwrap()).unwrap();
    assert_eq!(run(Some(&path)), Some(1));
    drop((a, b));
    let _ = fs::remove_dir_all(&root);
}
