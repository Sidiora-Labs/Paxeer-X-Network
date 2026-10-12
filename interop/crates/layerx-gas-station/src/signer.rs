use crate::config::StationConfig;
use crate::quote::{keccak, Address, Word};
use k256::ecdsa::{RecoveryId, Signature, SigningKey, VerifyingKey};
use layerx_platform_internal::base64;
use layerx_platform_internal::evm_sender::{EvmSender, EvmSenderError};
use layerx_platform_internal::kms::SECP256K1_HANDLE_PREFIX;
use layerx_platform_internal::secret::read_secret_file;
use layerx_platform_internal::tls::{Upstream, UpstreamResponse};
use serde::Deserialize;
use std::path::Path;
use zeroize::Zeroizing;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SignerError {
    KeySource,
    InvalidKey,
    Signing,
}
impl std::fmt::Display for SignerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "relayer signer refused: {self:?}")
    }
}
impl std::error::Error for SignerError {}
impl From<EvmSenderError> for SignerError {
    fn from(error: EvmSenderError) -> Self {
        match error {
            EvmSenderError::InvalidKey => Self::InvalidKey,
            EvmSenderError::Signing => Self::Signing,
            EvmSenderError::InvalidPrefix
            | EvmSenderError::MissingSource
            | EvmSenderError::AmbiguousSource
            | EvmSenderError::KeyFileUnavailable
            | EvmSenderError::InvalidKmsHandle
            | EvmSenderError::UnsupportedKmsAlgorithm
            | EvmSenderError::KmsUnavailable => Self::KeySource,
        }
    }
}

pub trait QuoteSigner {
    fn address(&self) -> Address;
    /// # Errors
    /// Returns a sanitized refusal with no key material.
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError>;
}

enum DigestKey {
    Local(SigningKey),
    Kms { upstream: Upstream, handle: String },
}

#[derive(Deserialize)]
struct KmsKey {
    key_id: String,
    handle: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct KmsSignature {
    signature: String,
    recovery_id: u8,
}

pub struct LocalSigner {
    key: DigestKey,
    address: Address,
}
impl LocalSigner {
    /// Loads the relayer through the shared EVM sender loader with
    /// `relayer_key_env` as its prefix: `<prefix>_KEY_FILE` or
    /// `<prefix>_KMS_KEY_ID`. The digest key is bound to the loader's sender
    /// address and every signature must recover to it.
    ///
    /// # Errors
    /// Refuses an invalid configuration, an absent, ambiguous or unreadable
    /// source, an invalid key, and a digest key that is not the loader's sender.
    pub fn from_config(config: &StationConfig) -> Result<Self, SignerError> {
        config.validate().map_err(|_| SignerError::KeySource)?;
        let prefix = config.relayer_key_env.as_str();
        let sender = EvmSender::from_env(prefix)?;
        let signer = match std::env::var_os(format!("{prefix}_KEY_FILE")) {
            Some(path) => Self::from_file(Path::new(&path))?,
            None => Self::from_kms(prefix, sender.address())?,
        };
        if signer.address != sender.address() {
            return Err(SignerError::KeySource);
        }
        Ok(signer)
    }

    /// # Errors
    /// Refuses an absent or invalid key in the named environment variable.
    pub fn from_env(name: &str) -> Result<Self, SignerError> {
        let secret = Zeroizing::new(std::env::var(name).map_err(|_| SignerError::KeySource)?);
        Self::decode(&secret)
    }

    fn from_file(path: &Path) -> Result<Self, SignerError> {
        let secret = read_secret_file(path).map_err(|_| SignerError::KeySource)?;
        Self::decode(&secret)
    }

    fn from_kms(prefix: &str, address: Address) -> Result<Self, SignerError> {
        let key_id =
            std::env::var(format!("{prefix}_KMS_KEY_ID")).map_err(|_| SignerError::KeySource)?;
        let upstream = Upstream::from_environment(prefix).map_err(|_| SignerError::KeySource)?;
        let response = upstream
            .get(&format!("/v1/signing-keys/{key_id}"))
            .map_err(|_| SignerError::KeySource)?;
        let key: KmsKey = json_body(&response).ok_or(SignerError::KeySource)?;
        if key.key_id != key_id || !key.handle.starts_with(SECP256K1_HANDLE_PREFIX) {
            return Err(SignerError::InvalidKey);
        }
        Ok(Self {
            key: DigestKey::Kms {
                upstream,
                handle: key.handle,
            },
            address,
        })
    }

    fn decode(secret: &str) -> Result<Self, SignerError> {
        let raw = secret.strip_prefix("0x").unwrap_or(secret);
        if raw.len() != 64 || !raw.is_ascii() {
            return Err(SignerError::InvalidKey);
        }
        let mut bytes = Zeroizing::new([0_u8; 32]);
        for (index, byte) in bytes.iter_mut().enumerate() {
            *byte = u8::from_str_radix(&raw[index * 2..index * 2 + 2], 16)
                .map_err(|_| SignerError::InvalidKey)?;
        }
        let key = SigningKey::from_slice(bytes.as_ref()).map_err(|_| SignerError::InvalidKey)?;
        let address = evm_address(key.verifying_key());
        Ok(Self {
            key: DigestKey::Local(key),
            address,
        })
    }

    fn kms_sign(
        upstream: &Upstream,
        handle: &str,
        digest: &Word,
    ) -> Result<(Signature, RecoveryId), SignerError> {
        let request = serde_json::to_vec(&serde_json::json!({
            "key_handle": handle,
            "algorithm": "secp256k1",
            "message": base64::encode(digest),
        }))
        .map_err(|_| SignerError::Signing)?;
        let response = upstream
            .post("/v1/signatures", &request)
            .map_err(|_| SignerError::Signing)?;
        let reply: KmsSignature = json_body(&response).ok_or(SignerError::Signing)?;
        let bytes = base64::decode(&reply.signature).ok_or(SignerError::Signing)?;
        let signature = Signature::from_slice(&bytes).map_err(|_| SignerError::Signing)?;
        let recovery = RecoveryId::from_byte(reply.recovery_id).ok_or(SignerError::Signing)?;
        Ok((signature, recovery))
    }
}
impl QuoteSigner for LocalSigner {
    fn address(&self) -> Address {
        self.address
    }
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError> {
        let (signature, recovery) = match &self.key {
            DigestKey::Local(key) => key
                .sign_prehash_recoverable(&digest)
                .map_err(|_| SignerError::Signing)?,
            DigestKey::Kms { upstream, handle } => Self::kms_sign(upstream, handle, &digest)?,
        };
        if recovery.to_byte() > 1 || signature.normalize_s().is_some() {
            return Err(SignerError::Signing);
        }
        let recovered = VerifyingKey::recover_from_prehash(&digest, &signature, recovery)
            .map_err(|_| SignerError::Signing)?;
        if evm_address(&recovered) != self.address {
            return Err(SignerError::Signing);
        }
        let mut result = [0; 65];
        result[..64].copy_from_slice(&signature.to_bytes());
        result[64] = recovery.to_byte() + 27;
        Ok(result)
    }
}

fn json_body<T: serde::de::DeserializeOwned>(response: &UpstreamResponse) -> Option<T> {
    if response.status != 200 || response.content_type.split(';').next() != Some("application/json")
    {
        return None;
    }
    serde_json::from_slice(&response.body).ok()
}

fn evm_address(key: &VerifyingKey) -> Address {
    let point = key.to_encoded_point(false);
    let digest = keccak(&point.as_bytes()[1..]);
    let mut address = [0; 20];
    address.copy_from_slice(&digest[12..]);
    address
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use layerx_platform_internal::kms::{KeyStore, Service, SEAL_LABEL};
    use layerx_platform_internal::seal::SealKey;
    use layerx_platform_internal::secret::random_hex;
    use layerx_platform_internal::{http, tls};
    use std::fs;
    use std::io::{Read, Write};
    use std::net::{Ipv4Addr, TcpListener, TcpStream};
    use std::path::PathBuf;
    use std::process::Command;
    use std::time::Duration;

    const SPONSOR: Address = [
        0x7e, 0x5f, 0x45, 0x52, 0x09, 0x1a, 0x69, 0x12, 0x5d, 0x5d, 0xfc, 0xb7, 0xb8, 0xc2, 0x65,
        0x90, 0x29, 0x39, 0x5b, 0xdf,
    ];
    const TOKEN: &str = "relayer-kms-integration-token";

    pub(crate) fn signer() -> Result<LocalSigner, SignerError> {
        LocalSigner::decode(&format!("{:064x}", 1))
    }

    fn recover(digest: &Word, bytes: &[u8; 65]) -> Result<Address, Box<dyn std::error::Error>> {
        let signature = Signature::from_slice(&bytes[..64])?;
        assert!(signature.normalize_s().is_none());
        let recovery = RecoveryId::try_from(bytes[64] - 27)?;
        Ok(evm_address(&VerifyingKey::recover_from_prehash(
            digest, &signature, recovery,
        )?))
    }

    struct Fixture {
        directory: PathBuf,
        config: StationConfig,
    }

    impl Fixture {
        fn new() -> Result<Self, Box<dyn std::error::Error>> {
            let suffix = random_hex(12)?;
            let directory =
                std::env::temp_dir().join(format!("paxeer-gas-relayer-{}", suffix.as_str()));
            fs::create_dir(&directory)?;
            let mut config = crate::config::tests::config();
            config.relayer_key_env =
                format!("GAS_STATION_RELAYER_TEST_{}", suffix.as_str()).to_ascii_uppercase();
            Ok(Self { directory, config })
        }

        fn var(&self, suffix: &str) -> String {
            format!("{}_{suffix}", self.config.relayer_key_env)
        }

        fn key_file(&self, contents: &str) -> Result<(), Box<dyn std::error::Error>> {
            let path = self.directory.join("relayer.key");
            fs::write(&path, contents)?;
            std::env::set_var(self.var("KEY_FILE"), &path);
            Ok(())
        }

        fn openssl(&self, arguments: &[&str]) -> Result<(), Box<dyn std::error::Error>> {
            let output = Command::new("openssl")
                .current_dir(&self.directory)
                .args(arguments)
                .output()?;
            if !output.status.success() {
                return Err("certificate generation failed".into());
            }
            Ok(())
        }

        fn certificates(&self) -> Result<(), Box<dyn std::error::Error>> {
            fs::write(
                self.directory.join("server.ext"),
                "basicConstraints=critical,CA:FALSE\nsubjectAltName=DNS:localhost\nextendedKeyUsage=serverAuth\n",
            )?;
            for arguments in [
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
                    "/CN=Paxeer Test CA",
                    "-addext",
                    "basicConstraints=critical,CA:TRUE",
                ][..],
                &[
                    "req",
                    "-new",
                    "-newkey",
                    "rsa:2048",
                    "-nodes",
                    "-keyout",
                    "server.key",
                    "-out",
                    "server.csr",
                    "-subj",
                    "/CN=server",
                ],
                &[
                    "x509",
                    "-req",
                    "-in",
                    "server.csr",
                    "-CA",
                    "ca.pem",
                    "-CAkey",
                    "ca.key",
                    "-CAcreateserial",
                    "-out",
                    "server.pem",
                    "-days",
                    "1",
                    "-extfile",
                    "server.ext",
                ],
                &[
                    "x509",
                    "-in",
                    "server.pem",
                    "-outform",
                    "DER",
                    "-out",
                    "server.der",
                ],
                &["x509", "-in", "ca.pem", "-outform", "DER", "-out", "ca.der"],
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
            ] {
                self.openssl(arguments)?;
            }
            Ok(())
        }

        fn start_kms(&self) -> Result<u16, Box<dyn std::error::Error>> {
            self.certificates()?;
            fs::write(self.directory.join("token"), TOKEN)?;
            for (suffix, file) in [
                ("TLS_CERT_DER", "server.der"),
                ("TLS_KEY_DER", "server-key.der"),
                ("UPSTREAM_CA_DER", "ca.der"),
                ("UPSTREAM_TOKEN_FILE", "token"),
            ] {
                std::env::set_var(self.var(suffix), self.directory.join(file));
            }
            let tls = tls::server_config(&self.config.relayer_key_env)?;
            let store = KeyStore::open(
                &self.directory.join("store"),
                SealKey::derive(SEAL_LABEL, b"relayer-test-seal-material"),
            )?;
            let service = Service::new(store, Zeroizing::new(TOKEN.to_owned()), false);
            let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?;
            let address = listener.local_addr()?;
            drop(listener);
            std::thread::spawn(move || {
                http::serve("paxeer-relayer-kms-test", address, &tls, move |request| {
                    service.route(request)
                })
            });
            std::env::set_var(
                self.var("UPSTREAM_URL"),
                format!("https://localhost:{}", address.port()),
            );
            for _ in 0..100 {
                if TcpStream::connect(address).is_ok() {
                    return Ok(address.port());
                }
                std::thread::sleep(Duration::from_millis(25));
            }
            Err("KMS listener unavailable".into())
        }

        fn create_kms_key(
            &self,
            port: u16,
        ) -> Result<(String, Address), Box<dyn std::error::Error>> {
            let ca = native_tls::Certificate::from_der(&fs::read(self.directory.join("ca.der"))?)?;
            let connector = native_tls::TlsConnector::builder()
                .disable_built_in_roots(true)
                .add_root_certificate(ca)
                .build()?;
            let tcp = TcpStream::connect((Ipv4Addr::LOCALHOST, port))?;
            tcp.set_read_timeout(Some(Duration::from_secs(5)))?;
            let mut stream = connector.connect("localhost", tcp)?;
            let body = r#"{"algorithm":"secp256k1","purpose":"gas-station-relayer"}"#;
            write!(
                stream,
                "POST /v1/signing-keys HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer {TOKEN}\r\nIdempotency-Key: relayer-one\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )?;
            stream.flush()?;
            let mut response = String::new();
            stream.read_to_string(&mut response)?;
            let (head, body) = response.split_once("\r\n\r\n").ok_or("unframed response")?;
            assert_eq!(head.split_whitespace().nth(1), Some("201"));
            let key: serde_json::Value = serde_json::from_str(body)?;
            let key_id = key["key_id"].as_str().ok_or("key id required")?;
            let public = base64::decode(key["public_key"].as_str().ok_or("public key required")?)
                .ok_or("public key encoding")?;
            assert_eq!(public.len(), 33);
            Ok((
                key_id.to_owned(),
                evm_address(&VerifyingKey::from_sec1_bytes(&public)?),
            ))
        }
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            for suffix in [
                "KEY_FILE",
                "KMS_KEY_ID",
                "TLS_CERT_DER",
                "TLS_KEY_DER",
                "UPSTREAM_CA_DER",
                "UPSTREAM_TOKEN_FILE",
                "UPSTREAM_URL",
            ] {
                std::env::remove_var(self.var(suffix));
            }
            let _ = fs::remove_dir_all(&self.directory);
        }
    }

    #[test]
    fn actual_signature_recovers_sponsor() -> Result<(), Box<dyn std::error::Error>> {
        let signer = signer()?;
        let digest = keccak(b"quote digest");
        let bytes = signer.sign_digest(digest)?;
        assert_eq!(recover(&digest, &bytes)?, SPONSOR);
        assert_eq!(signer.address(), SPONSOR);
        Ok(())
    }

    #[test]
    fn key_file_produces_sponsor_address() -> Result<(), Box<dyn std::error::Error>> {
        for encoded in [format!("{:064x}", 1), format!("0x{:064x}\r\n", 1)] {
            let fixture = Fixture::new()?;
            fixture.key_file(&encoded)?;
            let loader = EvmSender::from_env(&fixture.config.relayer_key_env)?;
            assert_eq!(loader.address(), SPONSOR);
            let signer = LocalSigner::from_config(&fixture.config)?;
            assert_eq!(signer.address(), SPONSOR);
            let digest = keccak(b"key file quote digest");
            assert_eq!(recover(&digest, &signer.sign_digest(digest)?)?, SPONSOR);
        }
        Ok(())
    }

    #[test]
    fn kms_handle_produces_sponsor_address() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let port = fixture.start_kms()?;
        let (key_id, sponsor) = fixture.create_kms_key(port)?;
        std::env::set_var(fixture.var("KMS_KEY_ID"), &key_id);
        let loader = EvmSender::from_env(&fixture.config.relayer_key_env)?;
        assert_eq!(loader.address(), sponsor);
        let signer = LocalSigner::from_config(&fixture.config)?;
        assert_eq!(signer.address(), sponsor);
        for message in [&b"kms quote digest"[..], b"kms sponsored transaction"] {
            let digest = keccak(message);
            let bytes = signer.sign_digest(digest)?;
            assert_eq!(signer.sign_digest(digest)?, bytes);
            assert_eq!(recover(&digest, &bytes)?, sponsor);
        }
        Ok(())
    }

    #[test]
    fn loader_refusals_map_without_key_material() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        assert!(matches!(
            LocalSigner::from_config(&fixture.config),
            Err(SignerError::KeySource)
        ));
        fixture.key_file(&format!("{:064x}", 1))?;
        std::env::set_var(fixture.var("KMS_KEY_ID"), "whk_relayer");
        assert!(matches!(
            LocalSigner::from_config(&fixture.config),
            Err(SignerError::KeySource)
        ));
        std::env::remove_var(fixture.var("KMS_KEY_ID"));
        for invalid in ["00".repeat(32), "ff".repeat(32), "sensitive-value".into()] {
            fs::remove_file(fixture.directory.join("relayer.key"))?;
            fixture.key_file(&invalid)?;
            assert!(matches!(
                LocalSigner::from_config(&fixture.config),
                Err(SignerError::InvalidKey)
            ));
        }
        std::env::set_var(
            fixture.var("KEY_FILE"),
            fixture.directory.join("missing-relayer.key"),
        );
        assert!(matches!(
            LocalSigner::from_config(&fixture.config),
            Err(SignerError::KeySource)
        ));
        std::env::remove_var(fixture.var("KEY_FILE"));
        std::env::set_var(fixture.var("KMS_KEY_ID"), "whk_relayer");
        assert!(matches!(
            LocalSigner::from_config(&fixture.config),
            Err(SignerError::KeySource)
        ));
        Ok(())
    }

    #[test]
    fn errors_never_echo_key_material() {
        for invalid in [
            String::new(),
            "00".repeat(32),
            "ff".repeat(32),
            "sensitive-value".into(),
        ] {
            assert!(matches!(
                LocalSigner::decode(&invalid),
                Err(SignerError::InvalidKey)
            ));
        }
        assert_eq!(
            SignerError::InvalidKey.to_string(),
            "relayer signer refused: InvalidKey"
        );
        let mut config = crate::config::tests::config();
        config.relayer_key_env.clear();
        assert!(matches!(
            LocalSigner::from_config(&config),
            Err(SignerError::KeySource)
        ));
    }
}
