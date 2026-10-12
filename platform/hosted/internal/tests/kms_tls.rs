use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::thread;
use std::time::Duration;

use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use k256::ecdsa::{RecoveryId, Signature as EvmSignature, VerifyingKey as EvmVerifyingKey};
use layerx_crypto::evm_transaction::{assemble, Eip1559Call};
use layerx_crypto::secp256k1::evm_address;
use layerx_crypto::signer::AgentSignature;
use layerx_platform_internal::base64;
use native_tls::{Certificate, Identity, TlsConnector};
use serde_json::{json, Value};

struct Fixture {
    directory: PathBuf,
    child: Child,
    port: u16,
    health_port: u16,
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = fs::remove_dir_all(&self.directory);
    }
}
fn command(directory: &Path, args: &[&str]) {
    let output = Command::new("openssl")
        .current_dir(directory)
        .args(args)
        .output()
        .unwrap_or_else(|error| panic!("openssl: {error}"));
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
}
fn free_port() -> u16 {
    TcpListener::bind("127.0.0.1:0")
        .and_then(|listener| listener.local_addr())
        .unwrap_or_else(|error| panic!("port: {error}"))
        .port()
}
fn spawn(directory: &Path, port: u16, health_port: u16) -> Child {
    Command::new(env!("CARGO_BIN_EXE_layerx-kms"))
        .env("LAYERX_KMS_LISTEN", format!("127.0.0.1:{port}"))
        .env("LAYERX_HEALTH_ADDR", format!("127.0.0.1:{health_port}"))
        .env("LAYERX_KMS_STATE_DIR", directory.join("state"))
        .env("LAYERX_KMS_TOKEN_FILE", directory.join("token"))
        .env("LAYERX_KMS_SEAL_SECRET_FILE", directory.join("seal"))
        .env("LAYERX_KMS_TLS_CERT_DER", directory.join("server.der"))
        .env("LAYERX_KMS_TLS_KEY_DER", directory.join("server-key.der"))
        .env("LAYERX_KMS_CLIENT_CA_DER", directory.join("ca.der"))
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap_or_else(|error| panic!("kms spawn: {error}"))
}
fn create_certificates(directory: &Path) {
    command(
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
            "/CN=LayerX Test CA",
            "-addext",
            "basicConstraints=critical,CA:TRUE",
        ],
    );
    fs::write(directory.join("server.ext"), "basicConstraints=critical,CA:FALSE\nsubjectAltName=DNS:localhost\nextendedKeyUsage=serverAuth\n").unwrap_or_else(|error| panic!("ext: {error}"));
    fs::write(
        directory.join("client.ext"),
        "basicConstraints=critical,CA:FALSE\nextendedKeyUsage=clientAuth\n",
    )
    .unwrap_or_else(|error| panic!("ext: {error}"));
    for name in ["server", "client"] {
        command(
            directory,
            &[
                "req",
                "-new",
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
        );
        command(
            directory,
            &[
                "x509",
                "-req",
                "-in",
                &format!("{name}.csr"),
                "-CA",
                "ca.pem",
                "-CAkey",
                "ca.key",
                "-CAcreateserial",
                "-out",
                &format!("{name}.pem"),
                "-days",
                "1",
                "-extfile",
                &format!("{name}.ext"),
            ],
        );
    }
}
fn convert_certificates(directory: &Path) {
    for name in ["server", "ca"] {
        command(
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
        );
    }
    command(
        directory,
        &[
            "pkcs8",
            "-topk8",
            "-nocrypt",
            "-in",
            "server.key",
            "-outform",
            "DER",
            "-out",
            "server-key.der",
        ],
    );
    command(
        directory,
        &[
            "pkcs12",
            "-export",
            "-inkey",
            "client.key",
            "-in",
            "client.pem",
            "-out",
            "client.p12",
            "-passout",
            "pass:integration-only",
        ],
    );
}
impl Fixture {
    fn start() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let directory = std::env::temp_dir().join(format!(
            "layerx-kms-tls-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&directory).unwrap_or_else(|error| panic!("directory: {error}"));
        create_certificates(&directory);
        convert_certificates(&directory);
        fs::write(directory.join("token"), "kms-integration-token")
            .unwrap_or_else(|error| panic!("token: {error}"));
        fs::write(
            directory.join("seal"),
            "kms-integration-seal-secret-32-bytes",
        )
        .unwrap_or_else(|error| panic!("seal: {error}"));
        let port = free_port();
        let health_port = free_port();
        let child = spawn(&directory, port, health_port);
        let fixture = Self {
            directory,
            child,
            port,
            health_port,
        };
        fixture.wait();
        fixture
    }
    fn wait(&self) {
        for _ in 0..100 {
            if TcpStream::connect(("127.0.0.1", self.port)).is_ok() {
                return;
            }
            thread::sleep(Duration::from_millis(25));
        }
        panic!("KMS never listened");
    }
    fn health(&self, method: &str, path: &str) -> (u16, String) {
        let mut tcp = TcpStream::connect(("127.0.0.1", self.health_port))
            .unwrap_or_else(|error| panic!("health connect: {error}"));
        tcp.set_read_timeout(Some(Duration::from_secs(5)))
            .unwrap_or_else(|error| panic!("timeout: {error}"));
        write!(
            tcp,
            "{method} {path} HTTP/1.1\r\nHost: localhost\r\nContent-Length: 0\r\n\r\n"
        )
        .unwrap_or_else(|error| panic!("health request: {error}"));
        let mut response = String::new();
        tcp.read_to_string(&mut response)
            .unwrap_or_else(|error| panic!("health response: {error}"));
        let (headers, body) = response
            .split_once("\r\n\r\n")
            .unwrap_or_else(|| panic!("framed health response"));
        let status = headers
            .split_whitespace()
            .nth(1)
            .and_then(|value| value.parse().ok())
            .unwrap_or_else(|| panic!("health status"));
        (status, body.to_owned())
    }
    fn request(&self, path: &str, token: &str, certificate: bool, body: &Value) -> (u16, Value) {
        let ca =
            fs::read(self.directory.join("ca.der")).unwrap_or_else(|error| panic!("CA: {error}"));
        let mut builder = TlsConnector::builder();
        builder.disable_built_in_roots(true).add_root_certificate(
            Certificate::from_der(&ca).unwrap_or_else(|error| panic!("CA: {error}")),
        );
        if certificate {
            builder.identity(
                Identity::from_pkcs12(
                    &fs::read(self.directory.join("client.p12"))
                        .unwrap_or_else(|error| panic!("identity: {error}")),
                    "integration-only",
                )
                .unwrap_or_else(|error| panic!("identity: {error}")),
            );
        }
        let connector = builder
            .build()
            .unwrap_or_else(|error| panic!("TLS: {error}"));
        let tcp = TcpStream::connect(("127.0.0.1", self.port))
            .unwrap_or_else(|error| panic!("connect: {error}"));
        tcp.set_read_timeout(Some(Duration::from_secs(5)))
            .unwrap_or_else(|error| panic!("timeout: {error}"));
        let mut tls = connector
            .connect("localhost", tcp)
            .unwrap_or_else(|error| panic!("TLS connect: {error}"));
        let method =
            if matches!(path, "/readyz" | "/livez") || path.starts_with("/v1/signing-keys/") {
                "GET"
            } else {
                "POST"
            };
        let body = body.to_string();
        write!(tls, "{method} {path} HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer {token}\r\nIdempotency-Key: registration-one\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len()).unwrap_or_else(|error| panic!("request: {error}"));
        tls.flush().unwrap_or_else(|error| panic!("flush: {error}"));
        let mut bytes = Vec::new();
        tls.read_to_end(&mut bytes)
            .unwrap_or_else(|error| panic!("response: {error}"));
        let response =
            String::from_utf8(bytes).unwrap_or_else(|error| panic!("response utf8: {error}"));
        let (headers, body) = response
            .split_once("\r\n\r\n")
            .unwrap_or_else(|| panic!("framed response"));
        let status = headers
            .split_whitespace()
            .nth(1)
            .and_then(|value| value.parse().ok())
            .unwrap_or_else(|| panic!("status"));
        (
            status,
            serde_json::from_str(body).unwrap_or_else(|error| panic!("JSON: {error}")),
        )
    }
}

#[test]
fn real_tls_kms_refuses_missing_credentials_and_preserves_signing_identity_after_restart() {
    let mut fixture = Fixture::start();
    assert_eq!(
        fixture.request("/readyz", "", false, &json!({})),
        (200, json!({"ready":true,"ed25519_non_exportable":true}))
    );
    assert_eq!(
        fixture.request("/livez", "", false, &json!({})),
        (200, json!({"alive":true}))
    );
    let create = json!({"algorithm":"ed25519","purpose":"layerx-webhook-v1"});
    assert_eq!(
        fixture
            .request("/v1/signing-keys", "kms-integration-token", false, &create)
            .0,
        401
    );
    assert_eq!(
        fixture
            .request("/v1/signing-keys", "wrong-token", true, &create)
            .0,
        401
    );
    let (status, key) = fixture.request("/v1/signing-keys", "kms-integration-token", true, &create);
    assert_eq!(status, 201);
    assert_eq!(
        fixture.request("/v1/signing-keys", "kms-integration-token", true, &create),
        (200, key.clone())
    );
    assert!(key.get("seed").is_none());
    fixture
        .child
        .kill()
        .unwrap_or_else(|error| panic!("stop: {error}"));
    fixture
        .child
        .wait()
        .unwrap_or_else(|error| panic!("wait: {error}"));
    fixture.child = spawn(&fixture.directory, fixture.port, fixture.health_port);
    fixture.wait();
    assert_eq!(
        fixture.request("/v1/signing-keys", "kms-integration-token", true, &create),
        (200, key.clone())
    );
    let (status, signature) = fixture.request("/v1/signatures", "kms-integration-token", true, &json!({"algorithm":"ed25519","key_handle":key["handle"],"message":base64::encode(b"real webhook message")}));
    assert_eq!(status, 200);
    let decode = |value: &Value| {
        base64::decode(value.as_str().unwrap_or_else(|| panic!("base64 string")))
            .unwrap_or_else(|| panic!("base64"))
    };
    let public: [u8; 32] = decode(&key["public_key"])
        .try_into()
        .unwrap_or_else(|_| panic!("public length"));
    let signature: [u8; 64] = decode(&signature["signature"])
        .try_into()
        .unwrap_or_else(|_| panic!("signature length"));
    let public =
        VerifyingKey::from_bytes(&public).unwrap_or_else(|error| panic!("public: {error}"));
    assert!(public
        .verify(b"real webhook message", &Signature::from_bytes(&signature))
        .is_ok());
    assert!(public
        .verify(
            b"tampered webhook message",
            &Signature::from_bytes(&signature)
        )
        .is_err());
}

#[test]
fn kms_serves_plain_healthz_beside_its_tls_listener() {
    let fixture = Fixture::start();
    for _ in 0..100 {
        if TcpStream::connect(("127.0.0.1", fixture.health_port)).is_ok() {
            break;
        }
        thread::sleep(Duration::from_millis(25));
    }
    assert_eq!(
        fixture.health("GET", "/healthz"),
        (200, r#"{"status":"ok"}"#.to_owned())
    );
    assert_eq!(fixture.health("POST", "/healthz").0, 405);
    assert_eq!(fixture.health("GET", "/readyz").0, 404);
}

type TestResult<T> = Result<T, Box<dyn std::error::Error>>;

fn decode_base64<const N: usize>(value: &Value) -> TestResult<[u8; N]> {
    let bytes =
        base64::decode(value.as_str().ok_or("base64 string required")?).ok_or("invalid base64")?;
    bytes
        .try_into()
        .map_err(|_| "unexpected byte length".into())
}

fn rlp_length(input: &[u8], count: usize) -> TestResult<usize> {
    let bytes = input.get(..count).ok_or("truncated RLP length")?;
    if bytes.first() == Some(&0) {
        return Err("noncanonical RLP length".into());
    }
    let length = bytes
        .iter()
        .try_fold(0_usize, |value, byte| {
            value.checked_mul(256)?.checked_add(usize::from(*byte))
        })
        .ok_or("RLP length overflow")?;
    if length < 56 {
        return Err("noncanonical long RLP length".into());
    }
    Ok(length)
}

fn take_rlp<'a>(input: &mut &'a [u8]) -> TestResult<(bool, &'a [u8])> {
    let prefix = *input.first().ok_or("missing RLP item")?;
    let (list, offset, length) = match prefix {
        0..=0x7f => (false, 0, 1),
        0x80..=0xb7 => (false, 1, usize::from(prefix - 0x80)),
        0xb8..=0xbf => {
            let count = usize::from(prefix - 0xb7);
            (false, count + 1, rlp_length(&input[1..], count)?)
        }
        0xc0..=0xf7 => (true, 1, usize::from(prefix - 0xc0)),
        _ => {
            let count = usize::from(prefix - 0xf7);
            (true, count + 1, rlp_length(&input[1..], count)?)
        }
    };
    let end = offset.checked_add(length).ok_or("RLP length overflow")?;
    let bytes = input.get(offset..end).ok_or("truncated RLP item")?;
    if !list && offset == 1 && length == 1 && bytes[0] < 0x80 {
        return Err("noncanonical RLP string".into());
    }
    *input = &input[end..];
    Ok((list, bytes))
}

fn rlp_integer(bytes: &[u8]) -> TestResult<u128> {
    if bytes.len() > 16 || bytes.first() == Some(&0) {
        return Err("invalid RLP integer".into());
    }
    Ok(bytes
        .iter()
        .fold(0_u128, |value, byte| value * 256 + u128::from(*byte)))
}

fn rlp_scalar(bytes: &[u8]) -> TestResult<[u8; 32]> {
    if bytes.is_empty() || bytes.len() > 32 || bytes.first() == Some(&0) {
        return Err("invalid RLP signature scalar".into());
    }
    let mut scalar = [0; 32];
    scalar[32 - bytes.len()..].copy_from_slice(bytes);
    Ok(scalar)
}

fn transaction_sender(raw: &[u8], expected_call: &Eip1559Call) -> TestResult<[u8; 20]> {
    assert_eq!(raw.first(), Some(&2));
    let mut encoded = raw.get(1..).ok_or("missing transaction body")?;
    let (list, mut payload) = take_rlp(&mut encoded)?;
    assert!(list);
    assert!(encoded.is_empty());
    let mut fields = Vec::new();
    while !payload.is_empty() {
        let (list, bytes) = take_rlp(&mut payload)?;
        assert_eq!(list, fields.len() == 8);
        fields.push(bytes);
    }
    assert_eq!(fields.len(), 12);
    assert!(fields[6].is_empty());
    assert!(fields[8].is_empty());
    let decoded = Eip1559Call {
        chain_id: u64::try_from(rlp_integer(fields[0])?)?,
        nonce: u64::try_from(rlp_integer(fields[1])?)?,
        max_priority_fee_per_gas: rlp_integer(fields[2])?,
        max_fee_per_gas: rlp_integer(fields[3])?,
        gas_limit: u64::try_from(rlp_integer(fields[4])?)?,
        to: fields[5].try_into()?,
        data: fields[7].to_vec(),
    };
    assert_eq!(&decoded, expected_call);
    let parity = u8::try_from(rlp_integer(fields[9])?)?;
    assert!(parity <= 1);
    let signature = EvmSignature::from_scalars(rlp_scalar(fields[10])?, rlp_scalar(fields[11])?)?;
    assert!(signature.normalize_s().is_none());
    let public = EvmVerifyingKey::recover_from_prehash(
        &decoded.signing_hash(),
        &signature,
        RecoveryId::try_from(parity)?,
    )?;
    evm_address(public.to_encoded_point(false).as_bytes())
        .map_err(|_| "invalid recovered public key".into())
}

fn assert_secp256k1_refusals(fixture: &Fixture, handle: &Value, digest: &[u8; 32]) {
    for message in [&digest[..31], &[0_u8; 33][..]] {
        assert_eq!(
            fixture.request(
                "/v1/signatures",
                "kms-integration-token",
                true,
                &json!({"algorithm":"secp256k1", "key_handle":handle,
                    "message":base64::encode(message)})
            ),
            (
                400,
                json!({"error":{"code":"invalid_message","retry":"never"}})
            ),
        );
    }
    assert_eq!(
        fixture.request(
            "/v1/signatures",
            "kms-integration-token",
            true,
            &json!({"algorithm":"ed25519", "key_handle":handle,
                "message":base64::encode(digest)})
        ),
        (
            422,
            json!({"error":{"code":"algorithm_mismatch","retry":"never"}})
        ),
    );
}

fn secp256k1_identity(fixture: &Fixture, create: &Value) -> TestResult<(Value, String, [u8; 20])> {
    let (status, key) = fixture.request("/v1/signing-keys", "kms-integration-token", true, create);
    assert_eq!(status, 201);
    assert_eq!(key.as_object().ok_or("key object required")?.len(), 3);
    assert!(key["key_id"]
        .as_str()
        .ok_or("key id required")?
        .starts_with("whk_"));
    assert!(key["handle"]
        .as_str()
        .ok_or("handle required")?
        .starts_with("kms-secp256k1-"));
    assert_eq!(
        fixture.request("/v1/signing-keys", "kms-integration-token", true, create),
        (200, key.clone())
    );
    let path = format!(
        "/v1/signing-keys/{}",
        key["key_id"].as_str().ok_or("key id required")?
    );
    assert_eq!(
        fixture.request(&path, "kms-integration-token", true, &json!({})),
        (200, key.clone())
    );
    let public = decode_base64::<33>(&key["public_key"])?;
    assert!(matches!(public[0], 2 | 3));
    let address = evm_address(&public).map_err(|_| "invalid KMS public key")?;
    Ok((key, path, address))
}

fn secp256k1_signature(fixture: &Fixture, request: &Value) -> TestResult<(Value, [u8; 64], u8)> {
    let (status, signed) =
        fixture.request("/v1/signatures", "kms-integration-token", true, request);
    assert_eq!(status, 200);
    assert_eq!(
        signed.as_object().ok_or("signature object required")?.len(),
        2
    );
    let signature = decode_base64::<64>(&signed["signature"])?;
    let recovery = u8::try_from(
        signed["recovery_id"]
            .as_u64()
            .ok_or("recovery id required")?,
    )?;
    assert!(recovery <= 1);
    assert!(EvmSignature::from_slice(&signature)?
        .normalize_s()
        .is_none());
    assert_eq!(
        fixture.request("/v1/signatures", "kms-integration-token", true, request),
        (200, signed.clone())
    );
    Ok((signed, signature, recovery))
}

#[test]
fn secp256k1_handle_signs_transaction_recoverable_to_its_address() -> TestResult<()> {
    let mut fixture = Fixture::start();
    let create = json!({"algorithm":"secp256k1", "purpose":"evm-sender-v1"});
    let (key, path, address) = secp256k1_identity(&fixture, &create)?;
    let call = Eip1559Call {
        chain_id: 125,
        nonce: 7,
        max_priority_fee_per_gas: 1,
        max_fee_per_gas: 2,
        gas_limit: 75_000,
        to: [0x11; 20],
        data: vec![0xab; 64],
    };
    let digest = call.signing_hash();
    let request = json!({"algorithm":"secp256k1", "key_handle":key["handle"],
        "message":base64::encode(&digest)});
    let (signed, signature, recovery) = secp256k1_signature(&fixture, &request)?;
    let mut bytes = [0_u8; 65];
    bytes[..64].copy_from_slice(&signature);
    bytes[64] = 27 + recovery;
    let sig = AgentSignature::new(bytes).map_err(|_| "noncanonical signature")?;
    let raw = assemble(&call, &sig);
    assert_eq!(transaction_sender(&raw, &call)?, address);
    assert_secp256k1_refusals(&fixture, &key["handle"], &digest);
    fixture.child.kill()?;
    fixture.child.wait()?;
    fixture.child = spawn(&fixture.directory, fixture.port, fixture.health_port);
    fixture.wait();
    assert_eq!(
        fixture.request(&path, "kms-integration-token", true, &json!({})),
        (200, key.clone())
    );
    assert_eq!(
        fixture.request("/v1/signing-keys", "kms-integration-token", true, &create),
        (200, key)
    );
    assert_eq!(
        fixture.request("/v1/signatures", "kms-integration-token", true, &request),
        (200, signed)
    );
    Ok(())
}
