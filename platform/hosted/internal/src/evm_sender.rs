use k256::ecdsa::SigningKey;
pub use layerx_crypto::evm_transaction::Eip1559Call;
use std::env;
use std::path::Path;
use zeroize::Zeroizing;

use crate::kms::HANDLE_PREFIX;
use crate::secret::{read_secret_file, unhex, valid_hex, valid_token};

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

pub struct EvmSender {
    secret: Zeroizing<[u8; 32]>,
    address: [u8; 20],
}

impl EvmSender {
    /// Loads a hexadecimal scalar from `<prefix>_KEY_FILE`, or selects
    /// `<prefix>_KMS_HANDLE` without falling back to another source.
    ///
    /// # Errors
    /// Refuses absent, ambiguous or invalid configuration and unreadable keys.
    /// The existing KMS only supports Ed25519, so its handles are refused.
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
        let handle = env::var_os(format!("{prefix}_KMS_HANDLE"));
        match (file, handle) {
            (Some(_), Some(_)) => Err(EvmSenderError::AmbiguousSource),
            (None, None) => Err(EvmSenderError::MissingSource),
            (Some(path), None) => Self::from_file(Path::new(&path)),
            (None, Some(handle)) => {
                let handle = handle.to_str().ok_or(EvmSenderError::InvalidKmsHandle)?;
                if !valid_token(handle, 512) {
                    return Err(EvmSenderError::InvalidKmsHandle);
                }
                if handle.starts_with(HANDLE_PREFIX) {
                    return Err(EvmSenderError::UnsupportedKmsAlgorithm);
                }
                Err(EvmSenderError::KmsUnavailable)
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
        Ok(Self { secret, address })
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
        tx.sign(&self.secret).map_err(|_| EvmSenderError::Signing)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kms::{Created, KeyStore, SEAL_LABEL};
    use crate::seal::SealKey;
    use crate::secret::{random_hex, MAX_SECRET_BYTES};
    use ed25519_dalek::{Signature as Ed25519Signature, Verifier, VerifyingKey};
    use k256::ecdsa::Signature;
    use std::fs;
    use std::io::Write;
    use std::path::PathBuf;

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
            env::set_var(format!("{}_KMS_HANDLE", self.prefix), handle);
        }

        fn load(&self) -> Result<EvmSender, EvmSenderError> {
            EvmSender::from_env(&self.prefix)
        }
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            env::remove_var(format!("{}_KEY_FILE", self.prefix));
            env::remove_var(format!("{}_KMS_HANDLE", self.prefix));
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
    fn ambiguous_source_refused() -> Result<(), Box<dyn std::error::Error>> {
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
    fn missing_file_refused_without_path() -> Result<(), Box<dyn std::error::Error>> {
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
        assert_eq!(store.lookup(&key.key_id), Some(key.clone()));
        let signature = store
            .sign(&key.handle, b"algorithm-check")?
            .ok_or("created handle was absent")?;
        VerifyingKey::from_bytes(&key.public_key)?.verify(
            b"algorithm-check",
            &Ed25519Signature::from_bytes(&signature),
        )?;
        fixture.handle(&key.handle);
        let error = fixture.load().err().ok_or("Ed25519 handle was accepted")?;
        assert_eq!(error, EvmSenderError::UnsupportedKmsAlgorithm);
        assert_eq!(error.to_string(), "evm_sender_unsupported_kms_algorithm");
        Ok(())
    }

    #[test]
    fn kms_handle_failures_do_not_fall_back() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = Fixture::new()?;
        for handle in ["", "invalid handle", &"a".repeat(513)] {
            fixture.handle(handle);
            assert_eq!(fixture.load().err(), Some(EvmSenderError::InvalidKmsHandle));
        }
        fixture.handle("unavailable-handle");
        assert_eq!(fixture.load().err(), Some(EvmSenderError::KmsUnavailable));
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
