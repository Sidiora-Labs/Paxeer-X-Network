use k256::ecdsa::SigningKey;
pub use layerx_crypto::evm_transaction::Eip1559Call;
use serde::Deserialize;
use std::env;
use std::path::Path;
use zeroize::Zeroizing;

use crate::base64;
use crate::kms::{HANDLE_PREFIX, KEY_ID_PREFIX, SECP256K1_HANDLE_PREFIX};
use crate::secret::{read_secret_file, unhex, valid_hex, valid_token};
use crate::tls::Upstream;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvmSenderError {
    InvalidPrefix,
    MissingSource,
    AmbiguousSource,
    KeyFileUnavailable,
    InvalidKey,
    InvalidKmsHandle,
    UnsupportedKmsAlgorithm,
    KmsUnavailable,
    Signing,
}

impl std::fmt::Display for EvmSenderError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::InvalidPrefix => "evm_sender_invalid_prefix",
            Self::MissingSource => "evm_sender_missing_source",
            Self::AmbiguousSource => "evm_sender_ambiguous_source",
            Self::KeyFileUnavailable => "evm_sender_key_file_unavailable",
            Self::InvalidKey => "evm_sender_invalid_key",
            Self::InvalidKmsHandle => "evm_sender_invalid_kms_handle",
            Self::UnsupportedKmsAlgorithm => "evm_sender_unsupported_kms_algorithm",
            Self::KmsUnavailable => "evm_sender_kms_unavailable",
            Self::Signing => "evm_sender_signing_failed",
        })
    }
}

impl std::error::Error for EvmSenderError {}

enum SenderKey {
    File(Zeroizing<[u8; 32]>),
    Kms { upstream: Upstream, handle: String },
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct KmsKey {
    key_id: String,
    handle: String,
    public_key: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct KmsSignature {
    signature: String,
    recovery_id: u8,
}

pub struct EvmSender {
    key: SenderKey,
    address: [u8; 20],
}

impl EvmSender {
    /// Loads a hexadecimal scalar from `<prefix>_KEY_FILE`, or selects
    /// `<prefix>_KMS_KEY_ID` through the configured upstream.
    ///
    /// # Errors
    /// Refuses absent, ambiguous or invalid configuration and unreadable keys.
    pub fn from_env(prefix: &str) -> Result<Self, EvmSenderError> {
        if prefix.is_empty()
            || prefix.len() > 128
            || !prefix
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_')
        {
            return Err(EvmSenderError::InvalidPrefix);
        }
        let file = env::var_os(format!("{prefix}_KEY_FILE"));
        let handle = env::var_os(format!("{prefix}_KMS_KEY_ID"));
        match (file, handle) {
            (Some(_), Some(_)) => Err(EvmSenderError::AmbiguousSource),
            (None, None) => Err(EvmSenderError::MissingSource),
            (Some(path), None) => Self::from_file(Path::new(&path)),
            (None, Some(key_id)) => {
                let key_id = key_id.to_str().ok_or(EvmSenderError::InvalidKmsHandle)?;
                Self::from_kms(prefix, key_id)
            }
        }
    }

    fn from_file(path: &Path) -> Result<Self, EvmSenderError> {
        let encoded = read_secret_file(path).map_err(|_| EvmSenderError::KeyFileUnavailable)?;
        let raw = encoded
            .strip_prefix("0x")
            .map_or(encoded.as_str(), |value| value);
        if !valid_hex(raw, 32) {
            return Err(EvmSenderError::InvalidKey);
        }
        let decoded = Zeroizing::new(unhex(raw).ok_or(EvmSenderError::InvalidKey)?);
        let mut secret = Zeroizing::new([0_u8; 32]);
        secret.copy_from_slice(&decoded);
        let key =
            SigningKey::from_slice(secret.as_ref()).map_err(|_| EvmSenderError::InvalidKey)?;
        let public = key.verifying_key().to_encoded_point(false);
        let address = layerx_crypto::secp256k1::evm_address(public.as_bytes())
            .map_err(|_| EvmSenderError::InvalidKey)?;
        Ok(Self {
            key: SenderKey::File(secret),
            address,
        })
    }

    fn from_kms(prefix: &str, key_id: &str) -> Result<Self, EvmSenderError> {
        if !valid_token(key_id, 64) || !key_id.starts_with(KEY_ID_PREFIX) {
            return Err(EvmSenderError::InvalidKmsHandle);
        }
        let upstream =
            Upstream::from_environment(prefix).map_err(|_| EvmSenderError::KmsUnavailable)?;
        let response = upstream
            .get(&format!("/v1/signing-keys/{key_id}"))
            .map_err(|_| EvmSenderError::KmsUnavailable)?;
        if response.status != 200
            || response.content_type.split(';').next() != Some("application/json")
        {
            return Err(EvmSenderError::KmsUnavailable);
        }
        let key: KmsKey =
            serde_json::from_slice(&response.body).map_err(|_| EvmSenderError::InvalidKey)?;
        if key.key_id != key_id || !valid_token(&key.handle, 512) {
            return Err(EvmSenderError::InvalidKmsHandle);
        }
        if key.handle.starts_with(HANDLE_PREFIX) {
            return Err(EvmSenderError::UnsupportedKmsAlgorithm);
        }
        if !key.handle.starts_with(SECP256K1_HANDLE_PREFIX) {
            return Err(EvmSenderError::InvalidKmsHandle);
        }
        let public = base64::decode(&key.public_key).ok_or(EvmSenderError::InvalidKey)?;
        if public.len() != 33 {
            return Err(EvmSenderError::InvalidKey);
        }
        let address = layerx_crypto::secp256k1::evm_address(&public)
            .map_err(|_| EvmSenderError::InvalidKey)?;
        Ok(Self {
            key: SenderKey::Kms {
                upstream,
                handle: key.handle,
            },
            address,
        })
    }

    #[must_use]
    pub const fn address(&self) -> [u8; 20] {
        self.address
    }

    /// Signs an existing type-2 call for `eth_sendRawTransaction`.
    ///
    /// # Errors
    /// Returns a sanitized refusal if signing fails.
    pub fn sign_transaction(&self, tx: &Eip1559Call) -> Result<Vec<u8>, EvmSenderError> {
        match &self.key {
            SenderKey::File(secret) => tx.sign(secret).map_err(|_| EvmSenderError::Signing),
            SenderKey::Kms { upstream, handle } => {
                let request = serde_json::to_vec(&serde_json::json!({
                    "key_handle": handle,
                    "algorithm": "secp256k1",
                    "message": base64::encode(&tx.signing_hash()),
                }))
                .map_err(|_| EvmSenderError::Signing)?;
                let response = upstream
                    .post("/v1/signatures", &request)
                    .map_err(|_| EvmSenderError::KmsUnavailable)?;
                if response.status != 200
                    || response.content_type.split(';').next() != Some("application/json")
                {
                    return Err(EvmSenderError::Signing);
                }
                let signature: KmsSignature =
                    serde_json::from_slice(&response.body).map_err(|_| EvmSenderError::Signing)?;
                let bytes: [u8; 64] = base64::decode(&signature.signature)
                    .and_then(|bytes| bytes.try_into().ok())
                    .ok_or(EvmSenderError::Signing)?;
                tx.assemble(&bytes, signature.recovery_id, &self.address)
                    .map_err(|_| EvmSenderError::Signing)
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kms::{Created, KeyStore, Service, SEAL_LABEL, SECP256K1_SEAL_LABEL};
    use crate::seal::SealKey;
    use crate::secret::{random_hex, MAX_SECRET_BYTES};
    use crate::{http, tls};
    use ed25519_dalek::Verifier as _;
    use k256::ecdsa::Signature;
    use std::fs;
    use std::io::Write;
    use std::net::{TcpListener, TcpStream};
    use std::path::PathBuf;
    use std::process::Command;
    use std::thread;
    use std::time::{Duration, Instant};

    const ADDRESS: [u8; 20] = [
        0x7e, 0x5f, 0x45, 0x52, 0x09, 0x1a, 0x69, 0x12, 0x5d, 0x5d, 0xfc, 0xb7, 0xb8, 0xc2, 0x65,
        0x90, 0x29, 0x39, 0x5b, 0xdf,
    ];

    struct Fixture {
        directory: PathBuf,
        prefix: String,
    }

    impl Fixture {
        fn new() -> Result<Self, Box<dyn std::error::Error>> {
            let suffix = random_hex(12)?;
            let directory = env::temp_dir().join(format!("paxeer-evm-sender-{}", suffix.as_str()));
            fs::create_dir(&directory)?;
            Ok(Self {
                directory,
                prefix: format!("PAXEER_EVM_SENDER_TEST_{}", suffix.as_str()),
            })
        }

        fn key_file(&self, contents: &[u8]) -> Result<PathBuf, std::io::Error> {
            let path = self.directory.join("key");
            let mut options = fs::OpenOptions::new();
            options.write(true).create_new(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                options.mode(0o600);
            }
            options.open(&path)?.write_all(contents)?;
            env::set_var(format!("{}_KEY_FILE", self.prefix), &path);
            Ok(path)
        }

        fn handle(&self, handle: &str) {
            env::set_var(format!("{}_KMS_KEY_ID", self.prefix), handle);
        }

        fn load(&self) -> Result<EvmSender, EvmSenderError> {
            EvmSender::from_env(&self.prefix)
        }
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            env::remove_var(format!("{}_KEY_FILE", self.prefix));
            env::remove_var(format!("{}_KMS_KEY_ID", self.prefix));
            for suffix in [
                "UPSTREAM_URL",
                "UPSTREAM_CA_DER",
                "UPSTREAM_TOKEN_FILE",
                "UPSTREAM_CLIENT_IDENTITY_PKCS12",
                "UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE",
                "TLS_CERT_DER",
                "TLS_KEY_DER",
                "CLIENT_CA_DER",
            ] {
                env::remove_var(format!("{}_{suffix}", self.prefix));
            }
            let _ = fs::remove_dir_all(&self.directory);
        }
    }

    #[test]
    fn file_key_derives_address() -> Result<(), Box<dyn std::error::Error>> {
        for encoded in [format!("{:064x}", 1), format!("0x{:064x}\r\n", 1)] {
            let fixture = Fixture::new()?;
            fixture.key_file(encoded.as_bytes())?;
            assert_eq!(fixture.load()?.address(), ADDRESS);
        }
        Ok(())
    }

    #[test]
    fn both_sources_refused() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        fixture.key_file(format!("{:064x}", 1).as_bytes())?;
        fixture.handle("kms-ed25519-unknown");
        assert_eq!(fixture.load().err(), Some(EvmSenderError::AmbiguousSource));
        env::set_var(format!("{}_KEY_FILE", fixture.prefix), "");
        fixture.handle("");
        assert_eq!(fixture.load().err(), Some(EvmSenderError::AmbiguousSource));
        Ok(())
    }

    #[test]
    fn missing_file_refused_without_contents() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let path = fixture.directory.join("private-file-marker");
        env::set_var(format!("{}_KEY_FILE", fixture.prefix), &path);
        let error = fixture.load().err().ok_or("missing file was accepted")?;
        assert_eq!(error, EvmSenderError::KeyFileUnavailable);
        assert_eq!(error.to_string(), "evm_sender_key_file_unavailable");
        assert_eq!(format!("{error:?}"), "KeyFileUnavailable");
        Ok(())
    }

    #[test]
    fn missing_source_refused() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        assert_eq!(fixture.load().err(), Some(EvmSenderError::MissingSource));
        assert_eq!(
            EvmSender::from_env("").err(),
            Some(EvmSenderError::InvalidPrefix)
        );
        assert_eq!(
            EvmSender::from_env("bad-prefix").err(),
            Some(EvmSenderError::InvalidPrefix)
        );
        assert_eq!(
            EvmSender::from_env(&"a".repeat(129)).err(),
            Some(EvmSenderError::InvalidPrefix)
        );
        Ok(())
    }

    #[test]
    fn invalid_scalars_refused_without_contents() -> Result<(), Box<dyn std::error::Error>> {
        for encoded in [
            "00".repeat(32),
            "ff".repeat(32),
            "01".repeat(31),
            "private-content-marker".to_owned(),
        ] {
            let fixture = Fixture::new()?;
            fixture.key_file(encoded.as_bytes())?;
            let error = fixture.load().err().ok_or("invalid scalar was accepted")?;
            assert_eq!(error, EvmSenderError::InvalidKey);
            assert_eq!(error.to_string(), "evm_sender_invalid_key");
            assert_eq!(format!("{error:?}"), "InvalidKey");
        }
        Ok(())
    }

    #[test]
    fn secret_reader_bounds_preserved() -> Result<(), Box<dyn std::error::Error>> {
        for contents in [
            Vec::new(),
            vec![b'a'; MAX_SECRET_BYTES + 1],
            vec![0xff; 32],
            b"invalid\tsecret".to_vec(),
        ] {
            let fixture = Fixture::new()?;
            fixture.key_file(&contents)?;
            assert_eq!(
                fixture.load().err(),
                Some(EvmSenderError::KeyFileUnavailable)
            );
        }
        Ok(())
    }

    #[test]
    fn kms_key_ids_refused_without_fallback() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        for key_id in ["", "invalid key", &"a".repeat(65), "unavailable-handle"] {
            fixture.handle(key_id);
            assert_eq!(fixture.load().err(), Some(EvmSenderError::InvalidKmsHandle));
        }
        fixture.handle("whk_unavailable");
        assert_eq!(fixture.load().err(), Some(EvmSenderError::KmsUnavailable));
        Ok(())
    }

    fn openssl(directory: &Path, arguments: &[&str]) -> Result<(), Box<dyn std::error::Error>> {
        let output = Command::new("openssl")
            .current_dir(directory)
            .args(arguments)
            .output()?;
        if !output.status.success() {
            return Err("certificate generation failed".into());
        }
        Ok(())
    }

    fn ca_certificate(directory: &Path) -> Result<(), Box<dyn std::error::Error>> {
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
                "/CN=Paxeer Test CA",
                "-addext",
                "basicConstraints=critical,CA:TRUE",
            ],
        )?;
        Ok(())
    }

    fn certificates(directory: &Path) -> Result<(), Box<dyn std::error::Error>> {
        ca_certificate(directory)?;
        fs::write(directory.join("server.ext"), "basicConstraints=critical,CA:FALSE\nsubjectAltName=DNS:localhost\nextendedKeyUsage=serverAuth\n")?;
        fs::write(
            directory.join("client.ext"),
            "basicConstraints=critical,CA:FALSE\nextendedKeyUsage=clientAuth\n",
        )?;
        for name in ["server", "client"] {
            openssl(
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
            )?;
            openssl(
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
            )?;
        }
        for name in ["server", "ca"] {
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
        }
        openssl(
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
        )?;
        openssl(
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
        )?;
        Ok(())
    }

    fn start_kms(fixture: &Fixture, store: KeyStore) -> Result<(), Box<dyn std::error::Error>> {
        certificates(&fixture.directory)?;
        fs::write(fixture.directory.join("token"), "kms-integration-token")?;
        fs::write(fixture.directory.join("password"), "integration-only")?;
        for (suffix, file) in [
            ("TLS_CERT_DER", "server.der"),
            ("TLS_KEY_DER", "server-key.der"),
            ("CLIENT_CA_DER", "ca.der"),
            ("UPSTREAM_CA_DER", "ca.der"),
            ("UPSTREAM_TOKEN_FILE", "token"),
            ("UPSTREAM_CLIENT_IDENTITY_PKCS12", "client.p12"),
            ("UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE", "password"),
        ] {
            env::set_var(
                format!("{}_{suffix}", fixture.prefix),
                fixture.directory.join(file),
            );
        }
        let tls = tls::server_config(&fixture.prefix)?;
        let listener = TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0))?;
        let address = listener.local_addr()?;
        drop(listener);
        let service = Service::new(
            store,
            Zeroizing::new("kms-integration-token".to_owned()),
            true,
        );
        thread::spawn(move || {
            http::serve("paxeer-kms-test", address, &tls, move |request| {
                service.route(request)
            })
        });
        env::set_var(
            format!("{}_UPSTREAM_URL", fixture.prefix),
            format!("https://localhost:{}", address.port()),
        );
        for _ in 0..100 {
            if TcpStream::connect(address).is_ok() {
                return Ok(());
            }
            thread::sleep(Duration::from_millis(25));
        }
        Err("KMS listener unavailable".into())
    }

    fn call() -> Eip1559Call {
        Eip1559Call {
            chain_id: 125,
            nonce: 0,
            max_priority_fee_per_gas: 1,
            max_fee_per_gas: 2,
            gas_limit: 21_000,
            to: [0x11; 20],
            data: Vec::new(),
        }
    }

    fn recovered_address(
        call: &Eip1559Call,
        signed: &[u8],
    ) -> Result<[u8; 20], Box<dyn std::error::Error>> {
        assert_eq!(signed[0], 0x02);
        assert_eq!(signed[1], 0xf8);
        assert_eq!(usize::from(signed[2]), signed.len() - 3);
        let parity = match signed[34] {
            0x80 => 0,
            0x01 => 1,
            _ => return Err("noncanonical parity".into()),
        };
        let mut payload = &signed[35..];
        let signature = Signature::from_scalars(
            signature_scalar(&mut payload),
            signature_scalar(&mut payload),
        )
        .map_err(|_| "invalid signature scalars")?;
        assert!(payload.is_empty());
        assert!(signature.normalize_s().is_none());
        let recovered = k256::ecdsa::VerifyingKey::recover_from_prehash(
            &call.signing_hash(),
            &signature,
            k256::ecdsa::RecoveryId::try_from(parity).map_err(|_| "invalid parity")?,
        )
        .map_err(|_| "recovery failed")?;
        layerx_crypto::secp256k1::evm_address(recovered.to_encoded_point(false).as_bytes())
            .map_err(|_| "invalid recovered key".into())
    }

    #[test]
    fn kms_handle_resolves() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let mut store = KeyStore::open(
            &fixture.directory.join("store"),
            SealKey::derive(SEAL_LABEL, b"test-seal-material"),
        )?;
        let Created::Fresh(key) = store.create_with_algorithm(
            "sender-key",
            "evm-sender",
            "request-digest",
            "secp256k1",
        )?
        else {
            return Err("fresh key was not created".into());
        };
        let address = layerx_crypto::secp256k1::evm_address(&key.public_key)
            .map_err(|_| "invalid public key")?;
        start_kms(&fixture, store)?;
        fixture.handle(&key.key_id);
        let sender = fixture.load()?;
        assert_eq!(sender.address(), address);
        let call = call();
        let signed = sender.sign_transaction(&call)?;
        assert_eq!(sender.sign_transaction(&call)?, signed);
        assert_eq!(recovered_address(&call, &signed)?, address);
        Ok(())
    }

    #[test]
    fn kms_ed25519_handle_refused() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let mut store = KeyStore::open(
            &fixture.directory.join("store"),
            SealKey::derive(SEAL_LABEL, b"test-seal-material"),
        )?;
        let Created::Fresh(key) = store.create("sender-key", "evm-sender", "request-digest")?
        else {
            return Err("fresh key was not created".into());
        };
        start_kms(&fixture, store)?;
        fixture.handle(&key.key_id);
        assert_eq!(
            fixture.load().err(),
            Some(EvmSenderError::UnsupportedKmsAlgorithm)
        );
        fixture.handle("whk_missing");
        assert_eq!(fixture.load().err(), Some(EvmSenderError::KmsUnavailable));
        Ok(())
    }

    fn journal_record(
        fixture: &Fixture,
        algorithm: &str,
    ) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        let directory = fixture.directory.join("store");
        let mut store = KeyStore::open(
            &directory,
            SealKey::derive(SEAL_LABEL, b"test-seal-material"),
        )?;
        let created =
            store.create_with_algorithm("sender-key", "evm-sender", "request-digest", algorithm)?;
        assert!(matches!(created, Created::Fresh(_)));
        drop(store);
        Ok(serde_json::from_str(&fs::read_to_string(
            directory.join("journal.log"),
        )?)?)
    }

    fn reopen_record(fixture: &Fixture, record: &serde_json::Value) -> Result<KeyStore, String> {
        let directory = fixture.directory.join("store");
        let mut encoded = serde_json::to_vec(record).map_err(|error| error.to_string())?;
        encoded.push(b'\n');
        fs::write(directory.join("journal.log"), encoded).map_err(|error| error.to_string())?;
        let deadline = Instant::now() + Duration::from_millis(2500);
        loop {
            match KeyStore::open(
                &directory,
                SealKey::derive(SEAL_LABEL, b"test-seal-material"),
            ) {
                Err(error)
                    if error.starts_with(crate::journal::LOCK_REFUSAL)
                        && Instant::now() < deadline =>
                {
                    thread::sleep(Duration::from_millis(25));
                }
                result => return result,
            }
        }
    }

    #[test]
    fn legacy_ed25519_record_opens_and_unknown_algorithm_refuses(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let mut record = journal_record(&fixture, "ed25519")?;
        record
            .as_object_mut()
            .ok_or("record object required")?
            .remove("algorithm");
        let store = reopen_record(&fixture, &record)?;
        let key_id = record["key_id"].as_str().ok_or("key id required")?;
        let key = store.lookup(key_id).ok_or("legacy key missing")?;
        assert_eq!(key.handle, record["handle"]);
        let signature = store
            .sign(&key.handle, b"legacy message")?
            .ok_or("signature missing")?;
        let public = key.public_key;
        ed25519_dalek::VerifyingKey::from_bytes(&public)?.verify(
            b"legacy message",
            &ed25519_dalek::Signature::from_bytes(&signature),
        )?;
        drop(store);
        record["algorithm"] = serde_json::json!("unknown");
        let error = reopen_record(&fixture, &record)
            .err()
            .ok_or("unknown algorithm accepted")?;
        assert!(error.starts_with("journal line 0: unknown variant `unknown`"));
        Ok(())
    }

    #[test]
    fn sealing_domains_refuse_cross_algorithm_open() -> Result<(), Box<dyn std::error::Error>> {
        for (source, target, handle, refusal) in [
            (
                "ed25519",
                "secp256k1",
                "kms-secp256k1-transposed",
                "sealed scalar has an invalid domain or length",
            ),
            (
                "secp256k1",
                "ed25519",
                "kms-ed25519-transposed",
                "sealed seed has an invalid length",
            ),
        ] {
            let fixture = Fixture::new()?;
            let mut record = journal_record(&fixture, source)?;
            record["algorithm"] = serde_json::json!(target);
            record["handle"] = serde_json::json!(handle);
            assert_eq!(
                reopen_record(&fixture, &record).err().as_deref(),
                Some(refusal)
            );
        }
        Ok(())
    }

    #[test]
    fn sealed_scalars_refuse_invalid_domain_zero_and_curve_order(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        let mut record = journal_record(&fixture, "secp256k1")?;
        let seal = SealKey::derive(SEAL_LABEL, b"test-seal-material");
        let order = [
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
            0xff, 0xfe, 0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b, 0xbf, 0xd2, 0x5e, 0x8c,
            0xd0, 0x36, 0x41, 0x41,
        ];
        for scalar in [[0; 32], order, [0xff; 32]] {
            let mut material = Zeroizing::new(SECP256K1_SEAL_LABEL.to_vec());
            material.extend_from_slice(&scalar);
            record["sealed_seed"] = serde_json::json!(seal.seal(&material)?);
            assert_eq!(
                reopen_record(&fixture, &record).err().as_deref(),
                Some("sealed scalar is invalid")
            );
        }
        let mut material = Zeroizing::new(SEAL_LABEL.to_vec());
        material.extend_from_slice(&[1; 32]);
        record["sealed_seed"] = serde_json::json!(seal.seal(&material)?);
        assert_eq!(
            reopen_record(&fixture, &record).err().as_deref(),
            Some("sealed scalar has an invalid domain or length")
        );
        Ok(())
    }

    #[test]
    fn assembled_signature_refuses_wrong_sender_and_noncanonical_values(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let call = call();
        let key = SigningKey::from_slice(&[1; 32]).map_err(|_| "invalid scalar")?;
        let (signature, recovery) = key
            .sign_prehash_recoverable(&call.signing_hash())
            .map_err(|_| "signature failed")?;
        let address = layerx_crypto::secp256k1::evm_address(
            key.verifying_key().to_encoded_point(false).as_bytes(),
        )
        .map_err(|_| "invalid public key")?;
        let bytes: [u8; 64] = signature.to_bytes().into();
        assert_eq!(
            call.assemble(&bytes, recovery.to_byte(), &address)?,
            call.sign(&[1; 32])?
        );
        let mut wrong_address = address;
        wrong_address[0] ^= 1;
        assert_eq!(
            call.assemble(&bytes, recovery.to_byte(), &wrong_address),
            Err(layerx_crypto::evm_transaction::InvalidEvmKey)
        );
        for parity in 2..=u8::MAX {
            assert_eq!(
                call.assemble(&bytes, parity, &address),
                Err(layerx_crypto::evm_transaction::InvalidEvmKey)
            );
        }
        for invalid in [[0; 64], [0xff; 64]] {
            assert_eq!(
                call.assemble(&invalid, recovery.to_byte(), &address),
                Err(layerx_crypto::evm_transaction::InvalidEvmKey)
            );
        }
        let mut high = bytes;
        high[32..].copy_from_slice(&[
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
            0xff, 0xfe, 0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b, 0xbf, 0xd2, 0x5e, 0x8c,
            0xd0, 0x36, 0x41, 0x40,
        ]);
        assert_eq!(
            call.assemble(&high, recovery.to_byte(), &address),
            Err(layerx_crypto::evm_transaction::InvalidEvmKey)
        );
        Ok(())
    }

    fn signature_scalar(input: &mut &[u8]) -> [u8; 32] {
        assert!(!input.is_empty());
        let prefix = input[0];
        let (length, offset) = if prefix < 0x80 {
            (1, 0)
        } else {
            assert!((0x81..=0xa0).contains(&prefix));
            (usize::from(prefix - 0x80), 1)
        };
        assert!(input.len() >= length + offset);
        let bytes = &input[offset..offset + length];
        assert_ne!(bytes[0], 0);
        assert!(offset == 0 || length > 1 || bytes[0] >= 0x80);
        let mut scalar = [0; 32];
        scalar[32 - length..].copy_from_slice(bytes);
        *input = &input[offset + length..];
        scalar
    }

    #[test]
    fn signed_type_two_transaction_recovers_sender() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        fixture.key_file(format!("{:064x}", 1).as_bytes())?;
        let sender = fixture.load()?;
        let tx = Eip1559Call {
            chain_id: 125,
            nonce: 0,
            max_priority_fee_per_gas: 1,
            max_fee_per_gas: 2,
            gas_limit: 21_000,
            to: [0x11; 20],
            data: Vec::new(),
        };
        let signed = sender.sign_transaction(&tx)?;
        assert_eq!(signed[0], 0x02);
        assert_eq!(signed[1], 0xf8);
        assert_eq!(usize::from(signed[2]), signed.len() - 3);
        let mut fields = vec![0x7d, 0x80, 0x01, 0x02, 0x82, 0x52, 0x08, 0x94];
        fields.extend_from_slice(&[0x11; 20]);
        fields.extend_from_slice(&[0x80, 0x80, 0xc0]);
        assert_eq!(&signed[3..34], fields.as_slice());
        let parity = match signed[34] {
            0x80 => 0,
            0x01 => 1,
            _ => return Err("noncanonical recovery parity".into()),
        };
        let mut payload = &signed[35..];
        let r = signature_scalar(&mut payload);
        let s = signature_scalar(&mut payload);
        assert_eq!(payload.len(), 0);
        let signature = Signature::from_scalars(r, s).map_err(|_| "invalid signature scalars")?;
        assert!(signature.normalize_s().is_none());
        let recovered = k256::ecdsa::VerifyingKey::recover_from_prehash(
            &tx.signing_hash(),
            &signature,
            k256::ecdsa::RecoveryId::try_from(parity).map_err(|_| "invalid recovery parity")?,
        )
        .map_err(|_| "signature recovery failed")?;
        assert_eq!(
            layerx_crypto::secp256k1::evm_address(recovered.to_encoded_point(false).as_bytes())
                .map_err(|_| "invalid recovered key")?,
            sender.address()
        );
        assert_eq!(sender.address(), ADDRESS);
        Ok(())
    }
}
