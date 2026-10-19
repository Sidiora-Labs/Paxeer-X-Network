//! The Ed25519 signing boundary behind `kms.layerx-internal.svc`.
//!
//! Keys are generated inside the service from the operating-system entropy
//! source, sealed at rest under the operator's seal secret, and never leave
//! the process: the API hands out an opaque handle and the public key, and
//! signs messages on request. Key creation is idempotent per
//! `Idempotency-Key` and purpose so a retried registration reuses the key it
//! already created.

use ed25519_dalek::{Signer, SigningKey, Verifier};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::Path;
use std::sync::Mutex;
use zeroize::Zeroizing;

use crate::base64;
use crate::http::{json, ok, refusal, Request, Response};
use crate::journal::Journal;
use crate::seal::SealKey;
use crate::secret::{random_hex, sha256_hex, unix_seconds, valid_hex, valid_token};

/// The domain label under which the seal keys are derived.
pub const SEAL_LABEL: &[u8] = b"layerx-kms-ed25519-seed";
pub const SECP256K1_SEAL_LABEL: &[u8] = b"layerx-kms-secp256k1-scalar";
/// Prefix of every key identifier the webhooks scheme accepts.
pub const KEY_ID_PREFIX: &str = "whk_";
/// Prefix of every handle this boundary issues.
pub const HANDLE_PREFIX: &str = "kms-ed25519-";
pub const SECP256K1_HANDLE_PREFIX: &str = "kms-secp256k1-";
/// Largest message the boundary signs.
pub const MAX_MESSAGE_BYTES: usize = 64 * 1024;
const MAX_KEYS: usize = 100_000;
const MAX_PURPOSE_BYTES: usize = 64;

#[derive(Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
enum Algorithm {
    #[default]
    Ed25519,
    Secp256k1,
}

impl Algorithm {
    fn parse(value: &str) -> Option<Self> {
        match value {
            "ed25519" => Some(Self::Ed25519),
            "secp256k1" => Some(Self::Secp256k1),
            _ => None,
        }
    }

    fn handle_prefix(self) -> &'static str {
        match self {
            Self::Ed25519 => HANDLE_PREFIX,
            Self::Secp256k1 => SECP256K1_HANDLE_PREFIX,
        }
    }

    fn public_bytes(self) -> usize {
        match self {
            Self::Ed25519 => 32,
            Self::Secp256k1 => 33,
        }
    }
}

enum OpenedKey {
    Ed25519(SigningKey),
    Secp256k1(k256::ecdsa::SigningKey),
}

impl OpenedKey {
    fn public_key(&self) -> Vec<u8> {
        match self {
            Self::Ed25519(key) => key.verifying_key().to_bytes().to_vec(),
            Self::Secp256k1(key) => key
                .verifying_key()
                .to_encoded_point(true)
                .as_bytes()
                .to_vec(),
        }
    }
}

#[derive(Serialize, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
struct KeyRecord {
    #[serde(default)]
    algorithm: Algorithm,
    key_id: String,
    handle: String,
    purpose: String,
    scope: String,
    request_digest: String,
    public_key: String,
    sealed_seed: String,
    created_at: u64,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct CreateKeyRequest {
    algorithm: String,
    purpose: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct SignRequest {
    key_handle: String,
    algorithm: String,
    message: String,
}

#[derive(Serialize)]
struct KeyResponse<'a> {
    key_id: &'a str,
    handle: &'a str,
    public_key: String,
}

#[derive(Serialize)]
struct SignatureResponse {
    signature: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    recovery_id: Option<u8>,
}

#[derive(Serialize)]
struct Readiness {
    ready: bool,
    ed25519_non_exportable: bool,
}

/// The sealed key store.
pub struct KeyStore {
    journal: Journal,
    seal: SealKey,
    by_handle: BTreeMap<String, KeyRecord>,
    by_scope: BTreeMap<String, String>,
    by_key_id: BTreeMap<String, String>,
}

/// Outcome of a create request.
pub enum Created<P = [u8; 32]> {
    /// A key was generated for a new scope.
    Fresh(KeyView<P>),
    /// The scope had already created this key with the same request.
    Existing(KeyView<P>),
    /// The scope already created a key with a different request body.
    Conflict,
}

/// The public view of a key.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct KeyView<P = [u8; 32]> {
    pub key_id: String,
    pub handle: String,
    pub public_key: P,
}

impl KeyStore {
    /// Opens the store under `state_dir`, replaying and authenticating every
    /// sealed key.
    ///
    /// # Errors
    /// Returns a description when the journal is unusable or a sealed key
    /// fails authentication under the configured seal secret.
    pub fn open(state_dir: &Path, seal: SealKey) -> Result<Self, String> {
        let mut records = Vec::new();
        let journal = Journal::open::<KeyRecord>(state_dir, |record| records.push(record))?;
        let mut store = Self {
            journal,
            seal,
            by_handle: BTreeMap::new(),
            by_scope: BTreeMap::new(),
            by_key_id: BTreeMap::new(),
        };
        for record in records {
            if store.by_handle.contains_key(&record.handle)
                || store.by_scope.contains_key(&record.scope)
                || store.by_key_id.contains_key(&record.key_id)
            {
                return Err("duplicate key identity in journal".to_owned());
            }
            let signing = store.open_key(&record)?;
            if signing.public_key() != decode_public(&record.public_key, record.algorithm)? {
                return Err(format!(
                    "journaled key {} does not match its sealed seed",
                    record.key_id
                ));
            }
            store.index(record);
        }
        Ok(store)
    }

    /// Number of keys held.
    #[must_use]
    pub fn len(&self) -> usize {
        self.by_handle.len()
    }

    /// True when no key is held.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.by_handle.is_empty()
    }

    /// Proves the state directory accepts durable writes.
    ///
    /// # Errors
    /// Returns a description when the readiness marker cannot be synced.
    pub fn probe_writable(&self) -> Result<(), String> {
        self.journal.probe_writable()
    }

    fn index(&mut self, record: KeyRecord) {
        self.by_scope
            .insert(record.scope.clone(), record.handle.clone());
        self.by_key_id
            .insert(record.key_id.clone(), record.handle.clone());
        self.by_handle.insert(record.handle.clone(), record);
    }

    fn open_key(&self, record: &KeyRecord) -> Result<OpenedKey, String> {
        if !record.handle.starts_with(record.algorithm.handle_prefix()) {
            return Err("sealed key algorithm does not match handle".to_owned());
        }
        let opened = self.seal.open(&record.sealed_seed)?;
        match record.algorithm {
            Algorithm::Ed25519 => {
                let mut seed = Zeroizing::new([0_u8; 32]);
                if opened.len() != seed.len() {
                    return Err("sealed seed has an invalid length".to_owned());
                }
                seed.copy_from_slice(&opened);
                Ok(OpenedKey::Ed25519(SigningKey::from_bytes(&seed)))
            }
            Algorithm::Secp256k1 => {
                let scalar = opened
                    .strip_prefix(SECP256K1_SEAL_LABEL)
                    .filter(|value| value.len() == 32)
                    .ok_or_else(|| "sealed scalar has an invalid domain or length".to_owned())?;
                let mut secret = Zeroizing::new([0_u8; 32]);
                secret.copy_from_slice(scalar);
                k256::ecdsa::SigningKey::from_slice(secret.as_ref())
                    .map(OpenedKey::Secp256k1)
                    .map_err(|_| "sealed scalar is invalid".to_owned())
            }
        }
    }

    /// Creates, or returns the already created, Ed25519 key for `scope`.
    ///
    /// # Errors
    /// Returns a description when entropy, sealing or journaling fails.
    pub fn create(
        &mut self,
        scope: &str,
        purpose: &str,
        request_digest: &str,
    ) -> Result<Created, String> {
        match self.create_key(scope, purpose, request_digest, Algorithm::Ed25519)? {
            Created::Fresh(key) => Ok(Created::Fresh(ed25519_view(key)?)),
            Created::Existing(key) => Ok(Created::Existing(ed25519_view(key)?)),
            Created::Conflict => Ok(Created::Conflict),
        }
    }

    pub(crate) fn create_with_algorithm(
        &mut self,
        scope: &str,
        purpose: &str,
        request_digest: &str,
        algorithm: &str,
    ) -> Result<Created<Vec<u8>>, String> {
        let algorithm =
            Algorithm::parse(algorithm).ok_or_else(|| "unsupported algorithm".to_owned())?;
        self.create_key(scope, purpose, request_digest, algorithm)
    }

    fn create_key(
        &mut self,
        scope: &str,
        purpose: &str,
        request_digest: &str,
        algorithm: Algorithm,
    ) -> Result<Created<Vec<u8>>, String> {
        if let Some(handle) = self.by_scope.get(scope) {
            let record = self
                .by_handle
                .get(handle)
                .ok_or_else(|| "scope index is inconsistent".to_owned())?;
            if record.request_digest == request_digest && record.algorithm == algorithm {
                return Ok(Created::Existing(view(record)?));
            }
            return Ok(Created::Conflict);
        }
        if self.by_handle.len() >= MAX_KEYS {
            return Err("key store is full".to_owned());
        }
        let mut seed = Zeroizing::new([0_u8; 32]);
        getrandom::fill(seed.as_mut()).map_err(|_| "entropy is unavailable".to_owned())?;
        let signing = match algorithm {
            Algorithm::Ed25519 => OpenedKey::Ed25519(SigningKey::from_bytes(&seed)),
            Algorithm::Secp256k1 => OpenedKey::Secp256k1(
                k256::ecdsa::SigningKey::from_slice(seed.as_ref())
                    .map_err(|_| "generated scalar is invalid".to_owned())?,
            ),
        };
        let sealed_seed = match algorithm {
            Algorithm::Ed25519 => self.seal.seal(seed.as_slice())?,
            Algorithm::Secp256k1 => {
                let mut material =
                    Zeroizing::new(Vec::with_capacity(SECP256K1_SEAL_LABEL.len() + seed.len()));
                material.extend_from_slice(SECP256K1_SEAL_LABEL);
                material.extend_from_slice(seed.as_slice());
                self.seal.seal(&material)?
            }
        };
        let record = KeyRecord {
            algorithm,
            key_id: format!("{KEY_ID_PREFIX}{}", random_hex(12)?.as_str()),
            handle: format!("{}{}", algorithm.handle_prefix(), random_hex(16)?.as_str()),
            purpose: purpose.to_owned(),
            scope: scope.to_owned(),
            request_digest: request_digest.to_owned(),
            public_key: crate::secret::hex(&signing.public_key()),
            sealed_seed,
            created_at: unix_seconds()?,
        };
        if self.by_key_id.contains_key(&record.key_id)
            || self.by_handle.contains_key(&record.handle)
        {
            return Err("generated identifiers collided".to_owned());
        }
        self.journal.append(&record)?;
        let created = view(&record)?;
        self.index(record);
        Ok(Created::Fresh(created))
    }

    /// Looks a key up by identifier.
    #[must_use]
    pub fn lookup(&self, key_id: &str) -> Option<KeyView> {
        let handle = self.by_key_id.get(key_id)?;
        self.by_handle
            .get(handle)
            .and_then(|record| view(record).ok())
            .and_then(|key| ed25519_view(key).ok())
    }

    /// Signs `message` under the key behind `handle`.
    ///
    /// # Errors
    /// Returns `Ok(None)` for an unknown handle and a description when the
    /// sealed key cannot be opened or no longer matches its public key.
    pub fn sign(&self, handle: &str, message: &[u8]) -> Result<Option<[u8; 64]>, String> {
        let Some(record) = self.by_handle.get(handle) else {
            return Ok(None);
        };
        if record.algorithm != Algorithm::Ed25519 {
            return Err("key algorithm mismatch".to_owned());
        }
        self.sign_record(record, message)
            .map(|signed| Some(signed.0))
    }

    fn sign_record(
        &self,
        record: &KeyRecord,
        message: &[u8],
    ) -> Result<([u8; 64], Option<u8>), String> {
        let signing = self.open_key(record)?;
        if signing.public_key() != decode_public(&record.public_key, record.algorithm)? {
            return Err("sealed key does not match its public key".to_owned());
        }
        match signing {
            OpenedKey::Ed25519(signing) => {
                let public = signing.verifying_key();
                let signature = signing.sign(message);
                public
                    .verify(message, &signature)
                    .map_err(|_| "signature self-check failed".to_owned())?;
                Ok((signature.to_bytes(), None))
            }
            OpenedKey::Secp256k1(signing) => {
                if message.len() != 32 {
                    return Err("prehash has an invalid length".to_owned());
                }
                let (signature, recovery) = signing
                    .sign_prehash_recoverable(message)
                    .map_err(|_| "signature failed".to_owned())?;
                let (signature, recovery) =
                    signature
                        .normalize_s()
                        .map_or((signature, recovery), |low| {
                            (
                                low,
                                k256::ecdsa::RecoveryId::new(
                                    !recovery.is_y_odd(),
                                    recovery.is_x_reduced(),
                                ),
                            )
                        });
                if recovery.is_x_reduced() {
                    return Err("signature is not EVM recoverable".to_owned());
                }
                let public =
                    k256::ecdsa::VerifyingKey::recover_from_prehash(message, &signature, recovery)
                        .map_err(|_| "signature self-check failed".to_owned())?;
                if public != *signing.verifying_key() {
                    return Err("signature self-check failed".to_owned());
                }
                Ok((signature.to_bytes().into(), Some(recovery.to_byte())))
            }
        }
    }
}

fn view(record: &KeyRecord) -> Result<KeyView<Vec<u8>>, String> {
    Ok(KeyView {
        key_id: record.key_id.clone(),
        handle: record.handle.clone(),
        public_key: decode_public(&record.public_key, record.algorithm)?,
    })
}

fn ed25519_view(key: KeyView<Vec<u8>>) -> Result<KeyView, String> {
    Ok(KeyView {
        key_id: key.key_id,
        handle: key.handle,
        public_key: key
            .public_key
            .try_into()
            .map_err(|_| "key algorithm mismatch".to_owned())?,
    })
}

fn decode_public(value: &str, algorithm: Algorithm) -> Result<Vec<u8>, String> {
    if !valid_hex(value, algorithm.public_bytes()) {
        return Err("journaled public key has an invalid length or encoding".to_owned());
    }
    let bytes = crate::secret::unhex(value)
        .ok_or_else(|| "journaled public key is malformed".to_owned())?;
    if algorithm == Algorithm::Secp256k1 {
        k256::ecdsa::VerifyingKey::from_sec1_bytes(&bytes)
            .map_err(|_| "journaled public key is malformed".to_owned())?;
    }
    Ok(bytes)
}

/// The service state a listener shares across connections.
pub struct Service {
    store: Mutex<KeyStore>,
    token: Zeroizing<String>,
    require_peer_certificate: bool,
}

impl Service {
    /// Wraps an open store with its bearer token and peer policy.
    #[must_use]
    pub fn new(store: KeyStore, token: Zeroizing<String>, require_peer_certificate: bool) -> Self {
        Self {
            store: Mutex::new(store),
            token,
            require_peer_certificate,
        }
    }

    fn authenticate(&self, request: &Request) -> Option<Response> {
        if self.require_peer_certificate && !request.peer_verified {
            return Some(refusal(401, "client_certificate_required", None));
        }
        if !request.bearer_matches(self.token.as_str()) {
            return Some(refusal(401, "unauthorized", None));
        }
        None
    }

    fn readiness(&self) -> Response {
        let ready = self
            .store
            .lock()
            .is_ok_and(|store| store.probe_writable().is_ok());
        json(
            if ready { 200 } else { 503 },
            &Readiness {
                ready,
                ed25519_non_exportable: true,
            },
        )
    }

    fn create_key(&self, request: &Request) -> Response {
        if let Some(refused) = self.authenticate(request) {
            return refused;
        }
        let Some(idempotency) = request
            .headers
            .get("idempotency-key")
            .filter(|value| valid_token(value, 128))
        else {
            return refusal(400, "idempotency_key_required", None);
        };
        if !request.json_body() {
            return refusal(400, "invalid_request", None);
        }
        let Ok(body) = serde_json::from_slice::<CreateKeyRequest>(&request.body) else {
            return refusal(400, "invalid_request", None);
        };
        if Algorithm::parse(&body.algorithm).is_none() {
            return refusal(422, "unsupported_algorithm", None);
        }
        if !valid_token(&body.purpose, MAX_PURPOSE_BYTES) {
            return refusal(422, "invalid_purpose", None);
        }
        let scope = format!("{}:{idempotency}", body.purpose);
        let digest = sha256_hex(&request.body);
        let Ok(mut store) = self.store.lock() else {
            return refusal(503, "dependency_unavailable", Some(5));
        };
        match store.create_with_algorithm(&scope, &body.purpose, &digest, &body.algorithm) {
            Ok(Created::Fresh(key)) => json(201, &key_response(&key)),
            Ok(Created::Existing(key)) => json(200, &key_response(&key)),
            Ok(Created::Conflict) => refusal(409, "idempotency_conflict", None),
            Err(error) => {
                eprintln!("layerx-kms create failed: {error}");
                refusal(503, "dependency_unavailable", Some(5))
            }
        }
    }

    fn get_key(&self, request: &Request, key_id: &str) -> Response {
        if let Some(refused) = self.authenticate(request) {
            return refused;
        }
        if !valid_token(key_id, 64) {
            return refusal(404, "unknown_key", None);
        }
        let Ok(store) = self.store.lock() else {
            return refusal(503, "dependency_unavailable", Some(5));
        };
        store
            .by_key_id
            .get(key_id)
            .and_then(|handle| store.by_handle.get(handle))
            .and_then(|record| view(record).ok())
            .map_or_else(
                || refusal(404, "unknown_key", None),
                |key| json(200, &key_response(&key)),
            )
    }

    fn sign(&self, request: &Request) -> Response {
        if let Some(refused) = self.authenticate(request) {
            return refused;
        }
        if !request.json_body() {
            return refusal(400, "invalid_request", None);
        }
        let Ok(body) = serde_json::from_slice::<SignRequest>(&request.body) else {
            return refusal(400, "invalid_request", None);
        };
        if Algorithm::parse(&body.algorithm).is_none() {
            return refusal(422, "unsupported_algorithm", None);
        }
        if !valid_token(&body.key_handle, 512) {
            return refusal(404, "unknown_key", None);
        }
        let Some(message) = base64::decode(&body.message) else {
            return refusal(400, "invalid_message", None);
        };
        if message.len() > MAX_MESSAGE_BYTES {
            return refusal(413, "message_too_large", None);
        }
        let Ok(store) = self.store.lock() else {
            return refusal(503, "dependency_unavailable", Some(5));
        };
        let Some(record) = store.by_handle.get(&body.key_handle) else {
            return refusal(404, "unknown_key", None);
        };
        if Algorithm::parse(&body.algorithm) != Some(record.algorithm) {
            return refusal(422, "algorithm_mismatch", None);
        }
        if record.algorithm == Algorithm::Secp256k1 && message.len() != 32 {
            return refusal(400, "invalid_message", None);
        }
        match store.sign_record(record, &message) {
            Ok((signature, recovery_id)) => ok(serde_json::json!(SignatureResponse {
                signature: base64::encode(&signature),
                recovery_id,
            })
            .to_string()),
            Err(error) => {
                eprintln!("layerx-kms sign failed: {error}");
                refusal(503, "dependency_unavailable", Some(5))
            }
        }
    }

    /// Routes one request.
    #[must_use]
    pub fn route(&self, request: &Request) -> Response {
        match (request.method.as_str(), request.path.as_str()) {
            ("GET", "/livez") => ok(r#"{"alive":true}"#.to_owned()),
            ("GET", "/readyz") => self.readiness(),
            ("POST", "/v1/signing-keys") => self.create_key(request),
            ("POST", "/v1/signatures") => self.sign(request),
            ("GET", path) => path.strip_prefix("/v1/signing-keys/").map_or_else(
                || refusal(404, "not_found", None),
                |key_id| self.get_key(request, key_id),
            ),
            (_, "/livez" | "/readyz" | "/v1/signing-keys" | "/v1/signatures") => {
                refusal(405, "method_not_allowed", None)
            }
            _ => refusal(404, "not_found", None),
        }
    }
}

fn key_response<P: AsRef<[u8]>>(key: &KeyView<P>) -> KeyResponse<'_> {
    KeyResponse {
        key_id: &key.key_id,
        handle: &key.handle,
        public_key: base64::encode(key.public_key.as_ref()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::VerifyingKey;
    use std::fs;
    use std::path::PathBuf;

    fn temporary_directory(name: &str) -> PathBuf {
        let directory = std::env::temp_dir()
            .join("layerx-internal-tests")
            .join("kms")
            .join(format!("{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&directory);
        directory
    }

    #[test]
    fn keys_are_idempotent_sealed_and_durable() {
        let directory = temporary_directory("durable");
        let seal = || SealKey::derive(SEAL_LABEL, b"seal secret");
        let mut store =
            KeyStore::open(&directory, seal()).unwrap_or_else(|error| panic!("{error}"));
        let Created::Fresh(first) = store
            .create(
                "layerx-webhook-v1:register:abc",
                "layerx-webhook-v1",
                "digest-a",
            )
            .unwrap_or_else(|error| panic!("{error}"))
        else {
            panic!("first create must be fresh");
        };
        assert!(first.key_id.starts_with(KEY_ID_PREFIX));
        assert!(first.handle.starts_with(HANDLE_PREFIX));
        let Created::Existing(again) = store
            .create(
                "layerx-webhook-v1:register:abc",
                "layerx-webhook-v1",
                "digest-a",
            )
            .unwrap_or_else(|error| panic!("{error}"))
        else {
            panic!("repeat must return the existing key");
        };
        assert_eq!(first, again);
        assert!(matches!(
            store.create(
                "layerx-webhook-v1:register:abc",
                "layerx-webhook-v1",
                "digest-b"
            ),
            Ok(Created::Conflict)
        ));
        let signature = store
            .sign(&first.handle, b"message")
            .unwrap_or_else(|error| panic!("{error}"))
            .unwrap_or_else(|| panic!("handle must sign"));
        VerifyingKey::from_bytes(&first.public_key)
            .unwrap_or_else(|error| panic!("{error}"))
            .verify(
                b"message",
                &ed25519_dalek::Signature::from_bytes(&signature),
            )
            .unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(
            store
                .sign("kms-ed25519-missing", b"message")
                .unwrap_or_else(|error| panic!("{error}")),
            None
        );
        let journal = fs::read_to_string(directory.join("journal.log"))
            .unwrap_or_else(|error| panic!("{error}"));
        assert!(journal.contains(&crate::secret::hex(&first.public_key)));
        assert!(journal.contains("\"sealed_seed\":\""));
        assert!(!journal.contains("\"seed\":"));
        drop(store);
        let reopened = KeyStore::open(&directory, seal()).unwrap_or_else(|error| panic!("{error}"));
        assert_eq!(reopened.len(), 1);
        assert_eq!(reopened.lookup(&first.key_id), Some(first.clone()));
        drop(reopened);
        let refused = KeyStore::open(&directory, SealKey::derive(SEAL_LABEL, b"other secret"))
            .err()
            .unwrap_or_else(|| panic!("wrong seal secret accepted"));
        assert!(refused.contains("failed authentication"));
        let _ = fs::remove_dir_all(&directory);
    }
}
