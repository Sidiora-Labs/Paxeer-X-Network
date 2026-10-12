use std::error::Error;
use std::future::Future;
use std::pin::pin;
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};
use std::time::Duration;

use ed25519_dalek::SigningKey;
use k256::ecdsa::Signature;
use layerx_crypto::account_derivation::derive_from_seed;
use layerx_crypto::evm_transaction::{
    assemble, decode_signed, recover_sender, transaction_hash, Eip1559Call, EvmTransactionError,
};
use layerx_crypto::remote::RemoteSigner;
use layerx_crypto::signer::{AgentSignature, KeystoreSigner, LocalSigner, SignError, Signer};
use layerx_crypto::VerifyError;
use sha3::{Digest as _, Keccak256};

type Outcome = Result<(), Box<dyn Error>>;

const KEY_ONE_ADDRESS: [u8; 20] = [
    0x1a, 0x64, 0x2f, 0x0e, 0x3c, 0x3a, 0xf5, 0x45, 0xe7, 0xac, 0xbd, 0x38, 0xb0, 0x72, 0x51, 0xb3,
    0x99, 0x09, 0x14, 0xf1,
];

struct NoopWake;

impl Wake for NoopWake {
    fn wake(self: Arc<Self>) {}
}

fn ready<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let waker = Waker::from(Arc::new(NoopWake));
    let mut context = Context::from_waker(&waker);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(value) => value,
        Poll::Pending => panic!("signer future unexpectedly blocked"),
    }
}

fn call() -> Eip1559Call {
    Eip1559Call {
        chain_id: 1,
        nonce: 7,
        max_priority_fee_per_gas: 2,
        max_fee_per_gas: 3,
        gas_limit: 30_000,
        to: [0x11; 20],
        data: vec![0xde, 0xad, 0xbe, 0xef],
    }
}

fn key_one_transaction() -> Result<Vec<u8>, Box<dyn Error>> {
    let raw = call().sign(&[1; 32])?;
    let length = raw.len();
    assert_eq!(raw[length - 33], 0xa0, "s must be a full 32-byte scalar");
    assert_eq!(raw[length - 66], 0xa0, "r must be a full 32-byte scalar");
    Ok(raw)
}

fn payload(raw: &[u8]) -> Vec<u8> {
    assert_eq!(raw[0], 0x02);
    match raw[1] {
        0xc0..=0xf7 => raw[2..].to_vec(),
        header => raw[2 + usize::from(header - 0xf7)..].to_vec(),
    }
}

fn relist(payload: &[u8]) -> Vec<u8> {
    let mut raw = vec![0x02];
    if payload.len() < 56 {
        raw.push(0xc0 + u8::try_from(payload.len()).unwrap_or(u8::MAX));
    } else {
        let length = u16::try_from(payload.len())
            .unwrap_or(u16::MAX)
            .to_be_bytes();
        if length[0] == 0 {
            raw.extend_from_slice(&[0xf8, length[1]]);
        } else {
            raw.extend_from_slice(&[0xf9, length[0], length[1]]);
        }
    }
    raw.extend_from_slice(payload);
    raw
}

#[test]
fn assemble_and_decode_round_trip_byte_for_byte() -> Outcome {
    let account = derive_from_seed(&[0x07; 64], 0)?;
    let signer = LocalSigner::from_derived_account(&account);
    assert_eq!(signer.evm_address(), Some(account.evm_address()));
    let call = call();
    let signature = ready(signer.sign_transaction(&call))?;
    assert!(matches!(signature.as_bytes()[64], 27 | 28));
    let raw = assemble(&call, &signature);
    assert_eq!(raw[0], 0x02);
    let Some(secret) = account.evm_secret() else {
        return Err("seed-derived account lacks its EVM key".into());
    };
    assert_eq!(call.sign(secret)?, raw);
    let (decoded_call, decoded_signature) = decode_signed(&raw)?;
    assert_eq!(decoded_call, call);
    assert_eq!(decoded_signature, signature);
    assert_eq!(assemble(&decoded_call, &decoded_signature), raw);
    Ok(())
}

#[test]
fn transaction_hash_is_keccak_of_the_raw_bytes() -> Outcome {
    let raw = key_one_transaction()?;
    let expected: [u8; 32] = Keccak256::digest(&raw).into();
    assert_eq!(transaction_hash(&raw), expected);
    let mut changed = raw.clone();
    changed[1] ^= 1;
    assert_ne!(transaction_hash(&changed), expected);
    Ok(())
}

#[test]
fn recover_sender_returns_the_local_signer_address() -> Outcome {
    let account = derive_from_seed(&[0x07; 64], 0)?;
    let signer = LocalSigner::from_derived_account(&account);
    let mut call = call();
    let signature = ready(signer.sign_transaction(&call))?;
    assert_eq!(recover_sender(&call, &signature), Ok(account.evm_address()));
    let (decoded, decoded_signature) = decode_signed(&key_one_transaction()?)?;
    assert_eq!(
        recover_sender(&decoded, &decoded_signature),
        Ok(KEY_ONE_ADDRESS)
    );
    call.nonce += 1;
    assert_ne!(recover_sender(&call, &signature), Ok(account.evm_address()));
    Ok(())
}

#[test]
fn high_s_is_refused() -> Outcome {
    let raw = key_one_transaction()?;
    let (_, signature) = decode_signed(&raw)?;
    let parsed = Signature::from_slice(&signature.as_bytes()[..64])
        .map_err(|_| "canonical signature rejected")?;
    let high = Signature::from_scalars(parsed.r().to_bytes(), (-*parsed.s()).to_bytes())
        .map_err(|_| "negated scalar rejected")?;
    assert!(high.normalize_s().is_some(), "negated s must be high");
    let mut bytes = *signature.as_bytes();
    bytes[32..64].copy_from_slice(&high.s().to_bytes());
    assert_eq!(AgentSignature::new(bytes), Err(VerifyError::BadSignature));
    let mut tampered = raw.clone();
    let length = tampered.len();
    tampered[length - 32..].copy_from_slice(&high.s().to_bytes());
    assert_eq!(
        decode_signed(&tampered),
        Err(EvmTransactionError::BadSignature)
    );
    Ok(())
}

#[test]
fn recovery_byte_outside_27_and_28_is_refused() -> Outcome {
    let raw = key_one_transaction()?;
    let (_, signature) = decode_signed(&raw)?;
    for v in [0_u8, 1, 26, 29, 35, 36, u8::MAX] {
        let mut bytes = *signature.as_bytes();
        bytes[64] = v;
        assert_eq!(AgentSignature::new(bytes), Err(VerifyError::BadSignature));
    }
    for v in [27_u8, 28] {
        let mut bytes = *signature.as_bytes();
        bytes[64] = v;
        assert!(AgentSignature::new(bytes).is_ok());
    }
    let mut tampered = raw.clone();
    let length = tampered.len();
    tampered[length - 67] = 0x02;
    assert_eq!(
        decode_signed(&tampered),
        Err(EvmTransactionError::BadSignature)
    );
    Ok(())
}

#[test]
fn trailing_bytes_and_other_envelopes_are_refused() -> Outcome {
    let raw = key_one_transaction()?;
    for suffix in [&[0x00][..], &[0x80], &[0xc0], &raw[1..]] {
        let mut extended = raw.clone();
        extended.extend_from_slice(suffix);
        assert_eq!(
            decode_signed(&extended),
            Err(EvmTransactionError::MalformedRlp)
        );
    }
    assert_eq!(
        decode_signed(&raw[..raw.len() - 1]),
        Err(EvmTransactionError::MalformedRlp)
    );
    assert_eq!(decode_signed(&[]), Err(EvmTransactionError::NotEip1559));
    let mut legacy = raw.clone();
    legacy[0] = 0x01;
    assert_eq!(decode_signed(&legacy), Err(EvmTransactionError::NotEip1559));
    assert_eq!(
        decode_signed(&raw[1..]),
        Err(EvmTransactionError::NotEip1559)
    );
    Ok(())
}

#[test]
fn non_minimal_integers_and_lengths_are_refused() -> Outcome {
    let raw = key_one_transaction()?;
    let fields = payload(&raw);
    assert_eq!(relist(&fields), raw);
    assert_eq!(fields[0], 0x01);
    for chain_id in [&[0x81, 0x01][..], &[0x82, 0x00, 0x01], &[0x00]] {
        let mut changed = chain_id.to_vec();
        changed.extend_from_slice(&fields[1..]);
        assert_eq!(
            decode_signed(&relist(&changed)),
            Err(EvmTransactionError::MalformedRlp)
        );
    }
    let mut wide = vec![0x89, 0x01, 0, 0, 0, 0, 0, 0, 0, 0];
    wide.extend_from_slice(&fields[1..]);
    assert_eq!(
        decode_signed(&relist(&wide)),
        Err(EvmTransactionError::UnsupportedField)
    );
    let length = u8::try_from(fields.len())?;
    let mut padded_length = vec![0x02, 0xf9, 0x00, length];
    padded_length.extend_from_slice(&fields);
    assert_eq!(
        decode_signed(&padded_length),
        Err(EvmTransactionError::MalformedRlp)
    );
    Ok(())
}

#[test]
fn value_and_access_list_are_refused() -> Outcome {
    let raw = key_one_transaction()?;
    let fields = payload(&raw);
    assert_eq!(fields[7], 0x94);
    assert_eq!(fields[28], 0x80);
    assert_eq!(fields[34], 0xc0);
    let mut value = fields.clone();
    value[28] = 0x01;
    assert_eq!(
        decode_signed(&relist(&value)),
        Err(EvmTransactionError::UnsupportedField)
    );
    let mut access_list = fields[..34].to_vec();
    access_list.extend_from_slice(&[0xc1, 0xc0]);
    access_list.extend_from_slice(&fields[35..]);
    assert_eq!(
        decode_signed(&relist(&access_list)),
        Err(EvmTransactionError::UnsupportedField)
    );
    Ok(())
}

#[test]
fn local_signer_without_an_evm_key_refuses() {
    let signer = LocalSigner::new([0x53; 32]);
    assert_eq!(signer.evm_address(), None);
    assert_eq!(
        ready(signer.sign_transaction(&call())),
        Err(SignError::EvmKeyUnavailable)
    );
}

#[test]
fn keystore_signer_refuses_transactions() {
    let public_key = SigningKey::from_bytes(&[0x67; 32])
        .verifying_key()
        .to_bytes();
    let Ok(signer) = KeystoreSigner::new(std::env::temp_dir().join("absent.sock"), public_key)
    else {
        panic!("valid external key was rejected");
    };
    assert_eq!(signer.evm_address(), None);
    assert_eq!(
        ready(signer.sign_transaction(&call())),
        Err(SignError::EvmKeyUnavailable)
    );
}

#[test]
fn remote_signer_refuses_transactions() {
    let directory =
        std::env::temp_dir().join(format!("layerx-evm-remote-signer-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&directory);
    assert!(std::fs::create_dir(&directory).is_ok());
    let key = directory.join("client.key");
    let certificate = directory.join("client.pem");
    let status = Command::new("openssl")
        .args(["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-sha256"])
        .args(["-days", "1", "-subj", "/CN=client", "-keyout"])
        .arg(&key)
        .arg("-out")
        .arg(&certificate)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    assert!(status.is_ok_and(|value| value.success()));
    let public_key = SigningKey::from_bytes(&[0x67; 32])
        .verifying_key()
        .to_bytes();
    let signer = RemoteSigner::new(
        std::net::SocketAddr::from(([127, 0, 0, 1], 9)),
        "localhost",
        public_key,
        &certificate,
        &certificate,
        &key,
        Duration::from_secs(1),
    );
    let _ = std::fs::remove_dir_all(&directory);
    let Ok(signer) = signer else {
        panic!("valid remote signer TLS material rejected");
    };
    assert_eq!(signer.evm_address(), None);
    assert_eq!(
        ready(signer.sign_transaction(&call())),
        Err(SignError::EvmKeyUnavailable)
    );
}
