use std::fmt::Write as _;

use layerx_crypto::keystore::{Keystore, KeystoreEntropy, KeystoreError};
use layerx_crypto::redact::Secret;
use layerx_crypto::remote::SignRefusal;
use layerx_crypto::session::SessionIssueError;
use layerx_crypto::signer::{LocalSigner, SignError};
use layerx_crypto::VerifyError;

fn capture<T: std::fmt::Debug + std::fmt::Display>(value: &T) -> String {
    format!("log={value:?}\nmetric={value}\ntrace={value:?}\nerror={value}")
}

#[test]
fn loaded_secrets_are_redacted_from_every_rendering_and_error_path() {
    let key = [b'K'; 32];
    let marker = "KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK";
    let operator_secret = b"operator-super-secret-marker";
    let identity = b"did:layerx:secret-check";
    let Ok(entropy) = KeystoreEntropy::new([0x31; 16], [0x42; 24]) else {
        panic!("valid entropy rejected");
    };
    let Ok(keystore) = Keystore::seal(&key, operator_secret, identity, 17, entropy) else {
        panic!("valid keystore rejected");
    };
    let Ok(loaded) = keystore.open(operator_secret, identity, 17) else {
        panic!("valid keystore could not be opened");
    };
    assert!(loaded.matches(&key));

    let mut captured = capture(&loaded);
    assert!(writeln!(captured, "keystore={keystore:?}").is_ok());
    assert!(writeln!(
        captured,
        "signer={:?}",
        LocalSigner::from_secret(Secret::new(key))
    )
    .is_ok());
    let token = Secret::new(String::from("session-token-value-marker"));
    assert!(writeln!(captured, "token={token:?}/{token}").is_ok());

    for error in [
        KeystoreError::InvalidInput,
        KeystoreError::IdentityMismatch,
        KeystoreError::NetworkMismatch,
        KeystoreError::KeyDerivation,
        KeystoreError::AuthenticationFailed,
        KeystoreError::MalformedStorage,
    ] {
        assert!(writeln!(captured, "{error:?}/{error}").is_ok());
    }
    for error in [
        SignError::DisclosureMismatch("amounts"),
        SignError::InvalidDisclosure,
        SignError::KeyRejected,
        SignError::KeystoreUnavailable,
        SignError::KeystoreRefused,
        SignError::MalformedResponse,
        SignError::ReturnedSignatureInvalid,
        SignError::RemoteRefused,
        SignError::RemoteTimeout,
        SignError::RemoteUnavailable,
        SignError::RemoteAuthentication,
        SignError::RemoteMalformedResponse,
    ] {
        assert!(writeln!(captured, "{error:?}/{error}").is_ok());
    }
    for error in [
        SignRefusal::Refused,
        SignRefusal::Timeout,
        SignRefusal::Unavailable,
        SignRefusal::Authentication,
        SignRefusal::MalformedResponse,
        SignRefusal::InvalidSignature,
    ] {
        assert!(writeln!(captured, "{error:?}/{error}").is_ok());
    }
    for error in [
        SessionIssueError::MissingExpiry,
        SessionIssueError::EmptyActivitySet,
        SessionIssueError::MissingRevocationSequence,
        SessionIssueError::InvalidExpiry,
        SessionIssueError::InvalidIdentityOrKey,
        SessionIssueError::NonRepresentableActivitySet,
        SessionIssueError::Encoding,
    ] {
        assert!(writeln!(captured, "{error:?}/{error}").is_ok());
    }
    for error in [
        VerifyError::VersionUnsupported,
        VerifyError::WrongNetwork,
        VerifyError::BadSignature,
    ] {
        assert!(writeln!(captured, "{error:?}").is_ok());
    }

    let panic_secret = Secret::new(key);
    let panic = std::panic::catch_unwind(|| panic!("panic payload: {panic_secret:?}"));
    let Err(payload) = panic else {
        panic!("redaction panic did not run");
    };
    if let Some(text) = payload.downcast_ref::<String>() {
        captured.push_str(text);
    }

    for forbidden in [
        marker,
        "4b4b4b4b",
        "operator-super-secret-marker",
        "session-token-value-marker",
    ] {
        assert!(
            !captured.contains(forbidden),
            "captured output leaked {forbidden}"
        );
    }
    assert!(captured.matches("[REDACTED]").count() >= 6);
}
