use ed25519_dalek::{Signer as _, SigningKey};
use layerx_platform_gateway::authority_evidence::MaintainedBatchDocument;
use layerx_platform_gateway::store::{
    KeyRecord, RedisEndpoint, RedisStore, Reservation, ReservationRequest,
};
use layerx_platform_gateway::{
    gateway_digest, verify_live_receipt, verify_settlement_receipt, AuthorityFacts, GatewayError,
    ReceiptKeyList,
};
use layerx_platform_internal::base64;
use layerx_platform_internal::secret::{hex, sha256_hex, unhex};
use layerx_proof::receipt::ReceiptCheck;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
use rustls::{ServerConfig, ServerConnection, StreamOwned};
use serde_json::{json, Value};
use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use zeroize::Zeroizing;

type TestResult = Result<(), Box<dyn std::error::Error>>;

fn entry(id: [u8; 32], signer: &SigningKey, first: u64, last: u64) -> Value {
    json!({
        "key_id": base64::encode(&id),
        "public_key": base64::encode(&signer.verifying_key().to_bytes()),
        "first_height": first,
        "last_height": last,
    })
}

fn batch(height: u64) -> [u8; 32] {
    let mut id = [0; 32];
    id[24..].copy_from_slice(&height.to_be_bytes());
    id
}

fn unsigned_receipt(key_id: [u8; 32], batch_id: [u8; 32], activity: [u8; 32]) -> Vec<u8> {
    let mut encoder = Encoder::new(4096);
    assert_eq!(encoder.structure_header_version(0x5201, 2), Ok(()));
    assert_eq!(encoder.u16(2), Ok(()));
    assert_eq!(encoder.bytes(&activity, 32), Ok(()));
    assert_eq!(encoder.u64(9), Ok(()));
    assert_eq!(encoder.bytes(&[2; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[3; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[8; 32], 32), Ok(()));
    assert_eq!(encoder.i32(0), Ok(()));
    assert_eq!(encoder.sequence_length(0, 512), Ok(()));
    assert_eq!(encoder.u128(1), Ok(()));
    assert_eq!(encoder.bytes(&batch_id, 32), Ok(()));
    assert_eq!(encoder.u16(1), Ok(()));
    assert_eq!(encoder.u32(1), Ok(()));
    assert_eq!(encoder.u32(0), Ok(()));
    assert_eq!(encoder.u8(1), Ok(()));
    assert_eq!(encoder.bytes(&[5; 32], 32), Ok(()));
    assert_eq!(encoder.u128(25), Ok(()));
    assert_eq!(encoder.bytes(&[6; 32], 32), Ok(()));
    assert_eq!(encoder.u128(100), Ok(()));
    assert_eq!(encoder.u128(75), Ok(()));
    assert_eq!(encoder.u64(1), Ok(()));
    assert_eq!(encoder.bytes(&[7; 32], 32), Ok(()));
    assert_eq!(encoder.u128(10), Ok(()));
    assert_eq!(encoder.u128(35), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&[0; 32], 32), Ok(()));
    assert_eq!(encoder.bytes(&key_id, 32), Ok(()));
    assert_eq!(encoder.u64(1_000), Ok(()));
    assert_eq!(encoder.u8(0), Ok(()));
    encoder.finish()
}

fn sign(mut unsigned: Vec<u8>, signer: &SigningKey) -> Result<Vec<u8>, String> {
    let digest = receipt_digest(&unsigned).map_err(|error| format!("{error:?}"))?;
    assert_eq!(unsigned.pop(), Some(0));
    let mut signature = Encoder::new(128);
    assert_eq!(signature.u8(1), Ok(()));
    assert_eq!(
        signature.bytes(&signer.sign(&digest).to_bytes(), 64),
        Ok(())
    );
    unsigned.extend(signature.finish());
    Ok(unsigned)
}

fn facts(height: u64, signer: &SigningKey) -> AuthorityFacts {
    AuthorityFacts::new(
        batch(height),
        [5; 32],
        [2; 32],
        [3; 32],
        signer.verifying_key().to_bytes(),
    )
}

#[test]
fn list_covers_rotation() -> TestResult {
    let first = SigningKey::from_bytes(&[3; 32]);
    let second = SigningKey::from_bytes(&[4; 32]);
    let document = json!([
        entry([11; 32], &first, 5000, 6999),
        entry([12; 32], &second, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    assert_eq!(keys.single(), None);
    for (height, id, signer) in [
        (5000, [11; 32], &first),
        (6500, [11; 32], &first),
        (6999, [11; 32], &first),
        (7000, [12; 32], &second),
        (7100, [12; 32], &second),
        (u64::MAX, [12; 32], &second),
    ] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let verified = verify_settlement_receipt(&bytes, facts(height, signer), &keys, [1; 32])?;
        assert_eq!(verified.receipt(), bytes);
        assert_eq!(verified.result_code(), 0);
        assert_eq!(verified.activity_id(), [1; 32]);
        let decoded = keys.verify_receipt(&bytes)?;
        let protocol = decoded.protocol().ok_or("protocol receipt required")?;
        assert_eq!(protocol.context_hash(), id);
        assert_eq!(protocol.batch_id(), batch(height));
        assert_eq!(protocol.global_sequence(), 9);
    }
    for height in [4999, 7000, 7100] {
        let bytes = sign(unsigned_receipt([11; 32], batch(height), [1; 32]), &first)?;
        assert_eq!(
            keys.verify_receipt(&bytes),
            Err(GatewayError::UntrustedSequencer)
        );
    }
    let bytes = sign(unsigned_receipt([12; 32], batch(6999), [1; 32]), &second)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::UntrustedSequencer)
    );
    Ok(())
}

#[test]
fn overlapping_primary_and_standby_ranges_are_pinned_by_id() -> TestResult {
    let primary = SigningKey::from_bytes(&[3; 32]);
    let standby = SigningKey::from_bytes(&[4; 32]);
    let document = json!([
        entry([11; 32], &primary, 5000, 7999),
        entry([12; 32], &standby, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    for (id, signer) in [([11; 32], &primary), ([12; 32], &standby)] {
        let bytes = sign(unsigned_receipt(id, batch(7100), [1; 32]), signer)?;
        let decoded = keys.verify_receipt(&bytes)?;
        assert_eq!(
            decoded
                .protocol()
                .ok_or("protocol receipt required")?
                .context_hash(),
            id
        );
    }
    let bytes = sign(unsigned_receipt([12; 32], batch(7100), [1; 32]), &primary)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::SequencerSignature))
    );
    let bytes = sign(unsigned_receipt([13; 32], batch(7100), [1; 32]), &primary)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::UntrustedSequencer)
    );
    Ok(())
}

#[test]
fn invalid_lists_are_refused_without_partial_pins() {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let valid = entry([11; 32], &signer, 5000, 6999);
    for document in [
        json!([]),
        json!({"receipt_keys": [valid.clone()]}),
        Value::Null,
    ] {
        assert_eq!(
            ReceiptKeyList::from_config(&document.to_string()),
            Err("receipt key list")
        );
    }
    assert_eq!(
        ReceiptKeyList::from_config(&" ".repeat(256 * 1024 + 1)),
        Err("receipt key list")
    );
    assert_eq!(
        ReceiptKeyList::from_config(&json!([valid.clone(), valid.clone()]).to_string()),
        Err("duplicate receipt key ID")
    );
    for (field, value, refused) in [
        ("first_height", json!(7000), "authorized batch range"),
        ("first_height", json!("5000"), "receipt key list"),
        ("last_height", json!(-1), "receipt key list"),
        ("key_id", json!(base64::encode(&[0; 32])), "sequencer ID"),
        ("key_id", json!(base64::encode(&[11; 31])), "sequencer ID"),
        (
            "public_key",
            json!(base64::encode(&[0; 32])),
            "sequencer public key",
        ),
        (
            "public_key",
            json!(base64::encode(&[255; 32])),
            "sequencer public key",
        ),
        (
            "public_key",
            json!(base64::encode(&[3; 31])),
            "sequencer public key",
        ),
        ("extra", json!(1), "receipt key list"),
    ] {
        let mut invalid = valid.clone();
        invalid[field] = value;
        assert_eq!(
            ReceiptKeyList::from_config(&json!([invalid]).to_string()),
            Err(refused)
        );
    }
}

#[test]
fn receipts_require_canonical_height_identity_and_signature() -> TestResult {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let keys =
        ReceiptKeyList::from_config(&json!([entry([11; 32], &signer, 5000, 6999)]).to_string())?;
    let unsigned = unsigned_receipt([11; 32], batch(6500), [1; 32]);
    assert_eq!(
        keys.verify_receipt(&unsigned),
        Err(GatewayError::Receipt(ReceiptCheck::MissingSignature))
    );
    let mut invalid_height = batch(6500);
    invalid_height[0] = 1;
    let bytes = sign(unsigned_receipt([11; 32], invalid_height, [1; 32]), &signer)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::BatchId))
    );
    let bytes = sign(unsigned_receipt([11; 32], batch(6500), [0; 32]), &signer)?;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::ActivityId))
    );
    let mut bytes = sign(unsigned, &signer)?;
    let last = bytes.last_mut().ok_or("signature required")?;
    *last ^= 1;
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::SequencerSignature))
    );
    assert_eq!(
        keys.verify_receipt(&bytes[..bytes.len() - 1]),
        Err(GatewayError::Receipt(ReceiptCheck::Decode))
    );
    bytes.push(0);
    assert_eq!(
        keys.verify_receipt(&bytes),
        Err(GatewayError::Receipt(ReceiptCheck::Decode))
    );
    Ok(())
}

#[test]
fn settlement_preserves_authority_and_activity_checks() -> TestResult {
    let signer = SigningKey::from_bytes(&[3; 32]);
    let other = SigningKey::from_bytes(&[4; 32]);
    let keys =
        ReceiptKeyList::from_config(&json!([entry([11; 32], &signer, 5000, 6999)]).to_string())?;
    let bytes = sign(unsigned_receipt([11; 32], batch(6500), [1; 32]), &signer)?;
    assert_eq!(
        keys.single().ok_or("single pin required")?.sequencer_id(),
        [11; 32]
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6500, &other), &keys, [1; 32]).err(),
        Some(GatewayError::UntrustedSequencer)
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6500, &signer), &keys, [2; 32]).err(),
        Some(GatewayError::ActivityMismatch)
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, facts(6501, &signer), &keys, [1; 32]).err(),
        Some(GatewayError::Receipt(ReceiptCheck::BatchId))
    );
    let wrong_asset = AuthorityFacts::new(
        batch(6500),
        [9; 32],
        [2; 32],
        [3; 32],
        signer.verifying_key().to_bytes(),
    );
    assert_eq!(
        verify_settlement_receipt(&bytes, wrong_asset, &keys, [1; 32]).err(),
        Some(GatewayError::Receipt(ReceiptCheck::Asset))
    );
    Ok(())
}

#[test]
fn two_key_list_verifies_historical_and_current_receipts() -> TestResult {
    let first = SigningKey::from_bytes(&[3; 32]);
    let second = SigningKey::from_bytes(&[4; 32]);
    let stranger = SigningKey::from_bytes(&[5; 32]);
    let document = json!([
        entry([11; 32], &first, 5000, 6999),
        entry([12; 32], &second, 7000, u64::MAX)
    ]);
    let keys = ReceiptKeyList::from_config(&document.to_string())?;
    assert_eq!(keys.single(), None);
    for (height, id, signer) in [(6000, [11; 32], &first), (7500, [12; 32], &second)] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let authority = facts(height, signer);
        let selected = keys.authorization_for_facts(authority)?;
        assert_eq!(selected.sequencer_id(), id);
        assert_eq!(selected.public_key(), signer.verifying_key().to_bytes());
        assert_eq!(keys.authorization_for_receipt(&bytes)?.sequencer_id(), id);
        assert_eq!(
            keys.authorization_for_header(id, height)?.sequencer_id(),
            id
        );
        let verified = verify_live_receipt(&bytes, authority, &keys, Some([1; 32]))?;
        assert_eq!(verified.receipt(), bytes);
        assert_eq!(verified.result_code(), 0);
        assert_eq!(verified.activity_id(), [1; 32]);
    }
    for (height, id, signer) in [
        (7500, [13; 32], &stranger),
        (7500, [11; 32], &first),
        (4999, [11; 32], &first),
    ] {
        let bytes = sign(unsigned_receipt(id, batch(height), [1; 32]), signer)?;
        let authority = facts(height, signer);
        assert_eq!(
            keys.authorization_for_facts(authority),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            keys.authorization_for_receipt(&bytes),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            keys.authorization_for_header(id, height),
            Err(GatewayError::UntrustedSequencer)
        );
        assert_eq!(
            verify_live_receipt(&bytes, authority, &keys, Some([1; 32])).err(),
            Some(GatewayError::UntrustedSequencer)
        );
    }
    Ok(())
}

fn maintained_capture() -> Result<Value, Box<dyn std::error::Error>> {
    let path =
        Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/maintained-authority.json");
    Ok(serde_json::from_slice(&fs::read(path)?)?)
}

fn text<'a>(value: &'a Value, name: &str) -> Result<&'a str, String> {
    value[name]
        .as_str()
        .ok_or_else(|| format!("fixture field {name}"))
}

fn fixed(value: &Value, name: &str) -> Result<[u8; 32], String> {
    unhex(text(value, name)?)
        .and_then(|bytes| bytes.try_into().ok())
        .ok_or_else(|| format!("fixture field {name} is not 32 hex bytes"))
}

fn maintained_facts(authority: &Value) -> Result<AuthorityFacts, String> {
    Ok(AuthorityFacts::new(
        fixed(authority, "batch_id")?,
        fixed(authority, "asset")?,
        fixed(authority, "previous_state_root")?,
        fixed(authority, "resulting_state_root")?,
        fixed(authority, "sequencer_public_key")?,
    ))
}

fn pinned_entry(id: [u8; 32], public_key: [u8; 32], first: u64, last: u64) -> Value {
    json!({
        "key_id": base64::encode(&id),
        "public_key": base64::encode(&public_key),
        "first_height": first,
        "last_height": last,
    })
}

fn command(program: &str, arguments: &[&str]) {
    let output = Command::new(program)
        .args(arguments)
        .output()
        .unwrap_or_else(|error| panic!("{program} must run: {error}"));
    assert!(
        output.status.success(),
        "{program} failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
}

fn path(value: &Path) -> &str {
    value
        .to_str()
        .unwrap_or_else(|| panic!("test path must be UTF-8"))
}

fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .unwrap_or_else(|error| panic!("test port must be allocated: {error}"))
        .port()
}

/// A real TLS Redis, the localhost certificate every TLS peer presents, and
/// the gateway's kernel client identity issued from that certificate.
struct Kernel {
    redis: Child,
    directory: PathBuf,
    redis_port: u16,
    tls: Arc<ServerConfig>,
}

impl Kernel {
    fn start() -> Self {
        let _ = rustls::crypto::ring::default_provider().install_default();
        let directory = std::env::temp_dir().join(format!(
            "layerx-maintained-receipt-{}-{}",
            std::process::id(),
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        ));
        fs::create_dir(&directory)
            .unwrap_or_else(|error| panic!("test directory must be created: {error}"));
        let pem = directory.join("server.pem");
        let der = directory.join("server.der");
        let key = directory.join("server.key");
        let key_der = directory.join("server-key.der");
        command(
            "openssl",
            &[
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                path(&key),
                "-out",
                path(&pem),
                "-days",
                "1",
                "-subj",
                "/CN=localhost",
                "-addext",
                "subjectAltName=DNS:localhost",
            ],
        );
        command(
            "openssl",
            &[
                "x509",
                "-in",
                path(&pem),
                "-outform",
                "DER",
                "-out",
                path(&der),
            ],
        );
        command(
            "openssl",
            &[
                "pkcs8",
                "-topk8",
                "-nocrypt",
                "-in",
                path(&key),
                "-outform",
                "DER",
                "-out",
                path(&key_der),
            ],
        );
        command(
            "openssl",
            &[
                "pkcs12",
                "-export",
                "-inkey",
                path(&key),
                "-in",
                path(&pem),
                "-out",
                path(&directory.join("client.p12")),
                "-passout",
                "pass:integration-only",
            ],
        );
        let redis_port = free_port();
        fs::write(
            directory.join("users.acl"),
            "user default off\nuser gateway on >gateway-secret ~* &* +@all\n",
        )
        .unwrap_or_else(|error| panic!("test Redis ACL must be written: {error}"));
        let config = directory.join("redis.conf");
        fs::write(
            &config,
            format!(
                "bind 127.0.0.1\nport 0\ntls-port {redis_port}\ntls-cert-file {}\ntls-key-file {}\ntls-ca-cert-file {}\ntls-auth-clients no\naclfile {}\nappendonly yes\nappendfsync always\ndir {}\nprotected-mode yes\n",
                path(&pem),
                path(&key),
                path(&pem),
                path(&directory.join("users.acl")),
                path(&directory),
            ),
        )
        .unwrap_or_else(|error| panic!("test Redis config must be written: {error}"));
        let redis = Command::new("redis-server")
            .arg(&config)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap_or_else(|error| panic!("real Redis server must start: {error}"));
        let certificate = CertificateDer::from(
            fs::read(&der).unwrap_or_else(|error| panic!("certificate must be read: {error}")),
        );
        let private_key = PrivateKeyDer::from(PrivatePkcs8KeyDer::from(
            fs::read(&key_der).unwrap_or_else(|error| panic!("key must be read: {error}")),
        ));
        let tls = Arc::new(
            ServerConfig::builder()
                .with_no_client_auth()
                .with_single_cert(vec![certificate], private_key)
                .unwrap_or_else(|error| panic!("listener TLS must build: {error}")),
        );
        let kernel = Self {
            redis,
            directory,
            redis_port,
            tls,
        };
        for _ in 0..100 {
            if TcpStream::connect(("127.0.0.1", redis_port)).is_ok() {
                return kernel;
            }
            thread::sleep(Duration::from_millis(20));
        }
        panic!("real Redis server did not become reachable")
    }

    fn file(&self, name: &str, value: &str) -> String {
        let file = self.directory.join(name);
        fs::write(&file, value).unwrap_or_else(|error| panic!("{name} must be written: {error}"));
        path(&file).to_owned()
    }

    fn store(&self) -> RedisStore {
        RedisStore::new(
            RedisEndpoint::parse(&format!("rediss://localhost:{}", self.redis_port))
                .unwrap_or_else(|error| panic!("{error}")),
            native_tls::Certificate::from_der(
                &fs::read(self.directory.join("server.der"))
                    .unwrap_or_else(|error| panic!("{error}")),
            )
            .unwrap_or_else(|error| panic!("{error}")),
            Zeroizing::new("gateway".to_owned()),
            Zeroizing::new("gateway-secret".to_owned()),
        )
    }
}

impl Drop for Kernel {
    fn drop(&mut self) {
        let _ = self.redis.kill();
        let _ = self.redis.wait();
        let _ = fs::remove_dir_all(&self.directory);
    }
}

/// One TLS upstream answering the component receipt read and the receipt
/// authority read with the captured maintained response, one request per
/// accepted connection.
struct Upstream {
    port: u16,
    stop: Arc<AtomicBool>,
    join: Option<JoinHandle<()>>,
}

impl Upstream {
    fn start(tls: &Arc<ServerConfig>, component: Value, authority: Value) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap_or_else(|error| panic!("{error}"));
        let port = listener
            .local_addr()
            .unwrap_or_else(|error| panic!("{error}"))
            .port();
        listener
            .set_nonblocking(true)
            .unwrap_or_else(|error| panic!("{error}"));
        let stop = Arc::new(AtomicBool::new(false));
        let (tls, stopped) = (Arc::clone(tls), Arc::clone(&stop));
        let activity = component["activity_id"]
            .as_str()
            .unwrap_or_default()
            .to_owned();
        let join = thread::spawn(move || {
            while !stopped.load(Ordering::Acquire) {
                match listener.accept() {
                    Ok((tcp, _)) => {
                        let _ = tcp.set_nonblocking(false);
                        let _ = tcp.set_read_timeout(Some(Duration::from_secs(3)));
                        let _ = tcp.set_write_timeout(Some(Duration::from_secs(3)));
                        let Ok(connection) = ServerConnection::new(Arc::clone(&tls)) else {
                            continue;
                        };
                        let mut stream = StreamOwned::new(connection, tcp);
                        if let Ok(request) =
                            layerx_platform_internal::http::parse_client_request(&mut stream)
                        {
                            let answer = if request.method != "GET" {
                                layerx_platform_internal::http::refusal(405, "method", None)
                            } else if request.path == format!("/v1/receipts/{activity}") {
                                layerx_platform_internal::http::json(200, &component)
                            } else if request.path
                                == format!("/v1/authorized-batches/by-activity/{activity}")
                            {
                                layerx_platform_internal::http::json(200, &authority)
                            } else {
                                layerx_platform_internal::http::refusal(404, "not_found", None)
                            };
                            let _ = layerx_platform_internal::http::write_response(
                                &mut stream,
                                &answer,
                            );
                        }
                    }
                    Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(5));
                    }
                    Err(error) => panic!("{error}"),
                }
            }
        });
        Self {
            port,
            stop,
            join: Some(join),
        }
    }
}

impl Drop for Upstream {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Release);
        if let Some(join) = self.join.take() {
            let _ = join.join();
        }
    }
}

struct Gateway {
    child: Child,
    port: u16,
}

impl Drop for Gateway {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Gateway {
    fn start(
        kernel: &Kernel,
        upstream: &Upstream,
        network: &str,
        protocol: u32,
        keys: &Value,
    ) -> Self {
        let port = free_port();
        let upstream_url = format!("https://localhost:{}", upstream.port);
        let environment = [
            ("LAYERX_GATEWAY_LISTEN", format!("127.0.0.1:{port}")),
            ("LAYERX_GATEWAY_LISTENER", "plain".to_owned()),
            (
                "LAYERX_GATEWAY_OUTBOUND_CA_DER",
                path(&kernel.directory.join("server.der")).to_owned(),
            ),
            ("LAYERX_GATEWAY_NETWORK_ID", network.to_owned()),
            (
                "LAYERX_GATEWAY_LXP_WIRE_VERSION",
                layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION.to_string(),
            ),
            ("LAYERX_GATEWAY_PROTOCOL_NETWORK_ID", protocol.to_string()),
            (
                "LAYERX_GATEWAY_REDIS_URL",
                format!("rediss://localhost:{}", kernel.redis_port),
            ),
            (
                "LAYERX_GATEWAY_REDIS_USERNAME_FILE",
                kernel.file("redis-username", "gateway"),
            ),
            (
                "LAYERX_GATEWAY_REDIS_PASSWORD_FILE",
                kernel.file("redis-password", "gateway-secret"),
            ),
            ("LAYERX_GATEWAY_COMPONENT_URL", upstream_url.clone()),
            (
                "LAYERX_GATEWAY_COMPONENT_TOKEN_FILE",
                kernel.file("component-token", "component-token"),
            ),
            ("LAYERX_GATEWAY_AUTHORITY_URL", upstream_url.clone()),
            (
                "LAYERX_GATEWAY_AUTHORITY_TOKEN_FILE",
                kernel.file("authority-token", "authority-token"),
            ),
            ("LAYERX_GATEWAY_IDENTITY_URL", upstream_url.clone()),
            (
                "LAYERX_GATEWAY_IDENTITY_TOKEN_FILE",
                kernel.file("identity-token", "identity-token"),
            ),
            ("LAYERX_GATEWAY_PROGRAM_REGISTRY_URL", upstream_url),
            (
                "LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE",
                kernel.file("registry-token", "registry-token"),
            ),
            (
                "LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12",
                path(&kernel.directory.join("client.p12")).to_owned(),
            ),
            (
                "LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE",
                kernel.file("client-identity-password", "integration-only"),
            ),
            (
                "LAYERX_GATEWAY_RECEIPT_KEYS_FILE",
                kernel.file("receipt-keys.json", &keys.to_string()),
            ),
            (
                "LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE",
                kernel.file("key-provisioning-key", &"5a".repeat(32)),
            ),
            (
                "LAYERX_GATEWAY_MODULE_REGISTRY_FILE",
                kernel.file(
                    "module-registry.json",
                    &json!({
                        "schema_version": 2,
                        "assets": [{
                            "asset": "02".repeat(32),
                            "currency": "USD",
                            "decimals": 6,
                            "symbol": "$"
                        }],
                        "modules": [{"module": 9, "ordinals": [1, 2, 7]}]
                    })
                    .to_string(),
                ),
            ),
        ];
        let child = Command::new(env!("CARGO_BIN_EXE_layerx-gateway"))
            .env_clear()
            .env("PATH", std::env::var_os("PATH").unwrap_or_default())
            .envs(environment.iter().map(|(name, value)| (name, value)))
            .stdout(Stdio::null())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap_or_else(|error| panic!("gateway must start: {error}"));
        let mut gateway = Self { child, port };
        for _ in 0..200 {
            if let Ok(Some(status)) = gateway.child.try_wait() {
                panic!("gateway refused start-up: {status}");
            }
            if gateway
                .get("/livez", None)
                .is_ok_and(|(status, _)| status == 200)
            {
                return gateway;
            }
            thread::sleep(Duration::from_millis(50));
        }
        panic!("gateway did not become live")
    }

    fn get(&self, path: &str, authorization: Option<&str>) -> Result<(u16, Value), String> {
        let mut stream =
            TcpStream::connect(("127.0.0.1", self.port)).map_err(|error| error.to_string())?;
        stream
            .set_read_timeout(Some(Duration::from_secs(30)))
            .map_err(|error| error.to_string())?;
        let credential = authorization
            .map(|value| format!("Authorization: {value}\r\n"))
            .unwrap_or_default();
        let request = format!(
            "GET {path} HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\n{credential}Connection: close\r\n\r\n"
        );
        stream
            .write_all(request.as_bytes())
            .map_err(|error| error.to_string())?;
        let mut answer = Vec::new();
        stream
            .read_to_end(&mut answer)
            .map_err(|error| error.to_string())?;
        let header_end = answer
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| "answer has no header terminator".to_owned())?;
        let head = String::from_utf8_lossy(&answer[..header_end]).into_owned();
        let status = head
            .split_whitespace()
            .nth(1)
            .and_then(|value| value.parse::<u16>().ok())
            .ok_or_else(|| format!("answer status is invalid: {head}"))?;
        let body = serde_json::from_slice(&answer[header_end + 4..])
            .map_err(|error| format!("answer is not JSON: {error}"))?;
        Ok((status, body))
    }
}

#[test]
fn maintained_hash_batch_id_verifies_with_single_and_two_key_lists() -> TestResult {
    let capture = maintained_capture()?;
    let authority = &capture["authority"];
    let receipt = unhex(text(&capture, "receipt_hex")?).ok_or("fixture receipt_hex")?;
    let activity = fixed(authority, "activity_id")?;
    let sequencer_id = fixed(&capture, "sequencer_id")?;
    let public_key = fixed(&capture, "sequencer_public_key")?;
    let first: u64 = text(&capture, "first_batch")?.parse()?;
    let last: u64 = text(&capture, "last_batch")?.parse()?;
    let batch_id = fixed(authority, "batch_id")?;
    assert_ne!(batch_id[..24], [0; 24], "fixture batch ID is a hash");
    let header = layerx_wire::receipt::decode_batch_header(
        &unhex(text(&authority["batch_evidence"], "header_hex")?).ok_or("fixture header_hex")?,
    )
    .map_err(|error| format!("{error:?}"))?;
    let batch_number = header.batch_number();
    assert_eq!(header.sequencer_id(), sequencer_id);
    assert!((first..=last).contains(&batch_number));
    let document: MaintainedBatchDocument =
        serde_json::from_value(authority["batch_evidence"].clone())?;
    let facts = maintained_facts(authority)?;
    let standby = SigningKey::from_bytes(&[4; 32]).verifying_key().to_bytes();
    let mut published = Vec::new();

    let single = json!([pinned_entry(sequencer_id, public_key, first, last)]);
    let two = json!([
        pinned_entry(sequencer_id, public_key, batch_number, batch_number),
        pinned_entry([12; 32], standby, batch_number + 1, u64::MAX)
    ]);
    for list in [&single, &two] {
        let keys = ReceiptKeyList::from_config(&list.to_string())?;
        assert_eq!(
            keys.authorization_for_facts(facts),
            Err(GatewayError::Receipt(ReceiptCheck::BatchId))
        );
        let verified = document.authorize_with_keys(&receipt, facts, &keys)?;
        let selected_batch = verified.authorized();
        assert_eq!(selected_batch.batch_id(), batch_id);
        assert_eq!(selected_batch.asset(), facts.authorized().asset());
        published.push(json!({
            "batch_id": hex(&selected_batch.batch_id()),
            "asset": hex(&selected_batch.asset()),
            "previous_state_root": hex(&selected_batch.previous_state_root()),
            "resulting_state_root": hex(&selected_batch.resulting_state_root()),
            "sequencer_public_key": hex(&selected_batch.sequencer_public_key()),
        }));
        assert_eq!(verified.sequencer_public_key(), public_key);
        let selected = keys.authorization_for_facts(verified)?;
        assert_eq!(selected.sequencer_id(), sequencer_id);
        assert_eq!(selected.public_key(), public_key);
        let operation = verify_live_receipt(&receipt, verified, &keys, Some(activity))?;
        assert_eq!(operation.receipt(), receipt);
        assert_eq!(operation.activity_id(), activity);
        assert_eq!(
            verify_live_receipt(&receipt, verified, &keys, Some([0; 32])).err(),
            Some(GatewayError::ActivityMismatch)
        );
    }
    for list in [
        json!([pinned_entry([12; 32], standby, first, last)]),
        json!([pinned_entry(
            sequencer_id,
            public_key,
            batch_number + 1,
            u64::MAX
        )]),
        json!([
            pinned_entry(sequencer_id, public_key, batch_number + 1, u64::MAX),
            pinned_entry([12; 32], standby, first, batch_number)
        ]),
    ] {
        let keys = ReceiptKeyList::from_config(&list.to_string())?;
        assert_eq!(
            document.authorize_with_keys(&receipt, facts, &keys).err(),
            Some(GatewayError::UntrustedSequencer)
        );
    }
    let keys = ReceiptKeyList::from_config(
        &json!([pinned_entry(sequencer_id, standby, first, last)]).to_string(),
    )?;
    assert_eq!(
        document.authorize_with_keys(&receipt, facts, &keys).err(),
        Some(GatewayError::VerificationRequired)
    );
    let keys = ReceiptKeyList::from_config(&single.to_string())?;
    let mut tampered = authority.clone();
    tampered["resulting_state_root"] = json!("aa".repeat(32));
    assert_eq!(
        document
            .authorize_with_keys(&receipt, maintained_facts(&tampered)?, &keys)
            .err(),
        Some(GatewayError::VerificationRequired)
    );

    let kernel = Kernel::start();
    let store = kernel.store();
    assert!(store.ready());
    let secret = format!("lxp_live_{}", "34".repeat(32));
    let record = KeyRecord {
        key_id: "maintained-reader".to_owned(),
        principal_digest: sha256_hex(b"maintained-reader"),
        salt: "maintained-salt".to_owned(),
        secret_digest: gateway_digest(&[b"gateway-key-v1", b"maintained-salt", secret.as_bytes()]),
        signer_public_key: "11".repeat(32),
        scopes: "receipt:read".to_owned(),
        quota_requests: 100,
        quota_window_seconds: 60,
        epoch: 1,
        disabled: false,
    };
    store.issue_key(&record, "issue-maintained-reader")?;
    let activity_hex = text(authority, "activity_id")?;
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let reservation = store.reserve(
        &record,
        ReservationRequest {
            idempotency_scope: &sha256_hex(b"maintained-reader-scope"),
            request_digest: &sha256_hex(b"maintained-reader-request"),
            now,
            retention_seconds: 3600,
            activity_id: activity_hex,
            protocol_idempotency_key: &sha256_hex(b"maintained-reader-idempotency"),
            principal_digest: &record.principal_digest,
            audit_event: "reserve-maintained-reader",
            continuation: "",
        },
    )?;
    assert!(matches!(reservation, Reservation::Reserved));
    let mut answer = authority.clone();
    answer["receipt"] = capture["receipt_hex"].clone();
    answer["protocol_network_id"] = json!(header.network_id());
    let upstream = Upstream::start(
        &kernel.tls,
        json!({"activity_id": activity_hex, "receipt": capture["receipt_hex"]}),
        answer,
    );
    let credential = format!("LayerX-Key {}:{secret}", record.key_id);
    let route = format!("/v1/receipts/{activity_hex}");
    for (list, expected) in [&single, &two].into_iter().zip(&published) {
        let gateway = Gateway::start(
            &kernel,
            &upstream,
            text(authority, "network_id")?,
            header.network_id(),
            list,
        );
        let (status, body) = gateway.get(&route, Some(&credential))?;
        assert_eq!(status, 200, "{body}");
        assert_eq!(body["ok"], json!(true), "{body}");
        assert_eq!(body["result"]["activity_id"], json!(activity_hex));
        assert_eq!(body["result"]["receipt"], capture["receipt_hex"]);
        assert_eq!(&body["result"]["authority"], expected, "{body}");
        let (status, body) = gateway.get(&route, None)?;
        assert_eq!(status, 401, "{body}");
    }
    let gateway = Gateway::start(
        &kernel,
        &upstream,
        text(authority, "network_id")?,
        header.network_id(),
        &json!([pinned_entry([12; 32], standby, first, last)]),
    );
    let (status, body) = gateway.get(&route, Some(&credential))?;
    assert_eq!(status, 502, "{body}");
    assert_eq!(
        body["error"]["code"],
        json!("receipt_key_unauthorized"),
        "{body}"
    );
    Ok(())
}
