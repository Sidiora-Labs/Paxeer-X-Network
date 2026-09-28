use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_human_identity_provider::{
    AssertionConfig, AssertionReceipt, AssertionVerifier, Policy, State,
};
use p256::ecdsa::signature::Signer as _;
use p256::ecdsa::{Signature, SigningKey};
use serde_json::{json, Value};
use std::collections::HashMap;
use std::error::Error;
use std::fs;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt as _;
use std::path::Path;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

type Result<T = ()> = std::result::Result<T, Box<dyn Error>>;

const ISSUER: &str = "https://identity.example.test/auth/v1";
const AUDIENCE: &str = "authenticated";
const WALLET_DID: &str =
    "did:layerx:3f1c0a9e5b7d2468ace013579bdf2468ace013579bdf2468ace013579bdf2468";
const OTHER_DID: &str =
    "did:layerx:9a8b7c6d5e4f30211203f4e5d6c7b8a99a8b7c6d5e4f30211203f4e5d6c7b8a9";

struct KeySetServer {
    address: SocketAddr,
    body: Arc<Mutex<String>>,
    hits: Arc<AtomicUsize>,
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl KeySetServer {
    fn serve(keys: &[Value]) -> Result<Self> {
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let address = listener.local_addr()?;
        let body = Arc::new(Mutex::new(json!({ "keys": keys }).to_string()));
        let hits = Arc::new(AtomicUsize::new(0));
        let shutdown = Arc::new(AtomicBool::new(false));
        let worker = {
            let body = Arc::clone(&body);
            let hits = Arc::clone(&hits);
            let shutdown = Arc::clone(&shutdown);
            thread::spawn(move || {
                for stream in listener.incoming() {
                    if shutdown.load(Ordering::Acquire) {
                        break;
                    }
                    if let Ok(mut stream) = stream {
                        let _ = respond(&mut stream, &body, &hits);
                    }
                }
            })
        };
        Ok(Self {
            address,
            body,
            hits,
            shutdown,
            worker: Some(worker),
        })
    }

    fn url(&self) -> String {
        format!("http://{}/auth/v1/.well-known/jwks.json", self.address)
    }

    fn replace(&self, keys: &[Value]) -> Result {
        *self.body.lock().map_err(|_| "key set lock poisoned")? =
            json!({ "keys": keys }).to_string();
        Ok(())
    }

    fn hits(&self) -> usize {
        self.hits.load(Ordering::Acquire)
    }
}

impl Drop for KeySetServer {
    fn drop(&mut self) {
        self.shutdown.store(true, Ordering::Release);
        let _ = TcpStream::connect(self.address);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

fn respond(stream: &mut TcpStream, body: &Mutex<String>, hits: &AtomicUsize) -> Result {
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;
    let mut request = Vec::new();
    let mut buffer = [0u8; 1024];
    while !request.windows(4).any(|window| window == b"\r\n\r\n") {
        let read = stream.read(&mut buffer)?;
        if read == 0 || request.len() > 16 * 1024 {
            return Ok(());
        }
        request.extend_from_slice(&buffer[..read]);
    }
    if !request.starts_with(b"GET /auth/v1/.well-known/jwks.json ") {
        stream.write_all(
            b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
        )?;
        return Ok(());
    }
    hits.fetch_add(1, Ordering::AcqRel);
    let document = body.lock().map_err(|_| "key set lock poisoned")?.clone();
    write!(
        stream,
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        document.len(),
        document
    )?;
    stream.flush()?;
    Ok(())
}

struct TokenSigner {
    key_id: String,
    key: SigningKey,
}

impl TokenSigner {
    fn generate(key_id: &str) -> Result<Self> {
        loop {
            let mut scalar = [0u8; 32];
            getrandom::fill(&mut scalar)?;
            if let Ok(key) = SigningKey::from_slice(&scalar) {
                return Ok(Self {
                    key_id: key_id.to_owned(),
                    key,
                });
            }
        }
    }

    fn jwk(&self) -> Result<Value> {
        let point = self.key.verifying_key().to_encoded_point(false);
        Ok(json!({
            "kty": "EC",
            "crv": "P-256",
            "kid": self.key_id,
            "use": "sig",
            "alg": "ES256",
            "x": URL_SAFE_NO_PAD.encode(point.x().ok_or("missing x")?),
            "y": URL_SAFE_NO_PAD.encode(point.y().ok_or("missing y")?),
        }))
    }

    fn mint(&self, claims: &Value) -> String {
        let header = json!({ "alg": "ES256", "typ": "JWT", "kid": self.key_id });
        let input = format!(
            "{}.{}",
            URL_SAFE_NO_PAD.encode(header.to_string()),
            URL_SAFE_NO_PAD.encode(claims.to_string())
        );
        let signature: Signature = self.key.sign(input.as_bytes());
        format!("{input}.{}", URL_SAFE_NO_PAD.encode(signature.to_bytes()))
    }
}

fn now() -> Result<u64> {
    Ok(SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs())
}

fn claims(subject: &str, now: u64) -> Value {
    json!({
        "iss": ISSUER,
        "sub": subject,
        "aud": AUDIENCE,
        "exp": now + 3600,
        "iat": now,
        "nbf": now - 5,
        "role": "authenticated",
        "email": "holder@example.test",
    })
}

fn config(url: String, refresh_interval_seconds: u64) -> AssertionConfig {
    AssertionConfig {
        jwks_url: url,
        issuer: ISSUER.to_owned(),
        audience: AUDIENCE.to_owned(),
        clock_skew_seconds: 30,
        refresh_interval_seconds,
    }
}

fn policy() -> Policy {
    Policy {
        root: [0x43; 32],
        threshold: 1,
        delay_seconds: 86_400,
    }
}

fn open_state(root: &Path, config: AssertionConfig) -> Result<State> {
    let mut state = State::open(root, policy())?;
    state.enable_assertion(AssertionVerifier::new(config)?)?;
    Ok(state)
}

struct Fixture {
    _directory: tempfile::TempDir,
    root: std::path::PathBuf,
    server: KeySetServer,
    signer: TokenSigner,
}

impl Fixture {
    fn new() -> Result<Self> {
        let directory = tempfile::Builder::new()
            .permissions(fs::Permissions::from_mode(0o700))
            .tempdir()?;
        let root = directory.path().join("identity");
        let signer = TokenSigner::generate("signing-key-1")?;
        let server = KeySetServer::serve(&[signer.jwk()?])?;
        Ok(Self {
            _directory: directory,
            root,
            server,
            signer,
        })
    }

    fn state(&self) -> Result<State> {
        open_state(&self.root, config(self.server.url(), 300))
    }
}

fn assert_chain(receipts: &[AssertionReceipt]) -> Result {
    let mut previous = [0u8; 32];
    for (index, receipt) in receipts.iter().enumerate() {
        assert_eq!(receipt.sequence, u64::try_from(index)?);
        assert_eq!(receipt.previous, previous);
        assert_eq!(receipt.compute_digest()?, receipt.digest);
        previous = receipt.digest;
    }
    Ok(())
}

#[test]
fn assertion_first_login_creates_account_and_second_login_reopens_it() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0001", now));

    let (first, created) = state.open_or_create_by_assertion(&token, None, now)?;
    assert!(created);
    assert!(first.principal().starts_with("act_"));
    assert_eq!(first.issuer(), ISSUER);
    assert_eq!(first.subject(), "user-0001");
    assert_eq!(first.did(), None);
    assert_eq!(first.created_at(), now);

    let second_token = fixture.signer.mint(&claims("user-0001", now + 1));
    let (second, created) = state.open_or_create_by_assertion(&second_token, None, now + 1)?;
    assert!(!created);
    assert_eq!(second, first);
    assert_eq!(fixture.server.hits(), 1);

    let receipts = state.assertion_receipts();
    assert_eq!(receipts.len(), 1);
    assert_eq!(receipts[0].event, "account.created.assertion");
    assert_eq!(receipts[0].principal, first.principal());
    assert_eq!(receipts[0].issuer, ISSUER);
    assert_eq!(receipts[0].subject, "user-0001");
    assert_eq!(receipts[0].recorded_at, now);
    assert_chain(receipts)?;

    let other = fixture.signer.mint(&claims("user-0002", now));
    let (third, created) = state.open_or_create_by_assertion(&other, None, now)?;
    assert!(created);
    assert_ne!(third.principal(), first.principal());
    assert_eq!(state.assertion_receipts().len(), 2);
    assert_chain(state.assertion_receipts())?;
    drop(state);

    let reopened = fixture.state()?;
    assert_eq!(
        reopened.assertion_principal(ISSUER, "user-0001"),
        Some(first.clone())
    );
    assert_eq!(reopened.resolve_assertion(&second_token, now + 1)?, first);
    assert_eq!(reopened.assertion_receipts().len(), 2);
    Ok(())
}

#[test]
fn assertion_wrong_issuer_audience_expired_and_not_yet_valid_are_refused() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;

    let mut wrong_issuer = claims("user-0003", now);
    wrong_issuer["iss"] = json!("https://other.example.test/auth/v1");
    let mut wrong_audience = claims("user-0003", now);
    wrong_audience["aud"] = json!(["service_role", "anon"]);
    let mut expired = claims("user-0003", now);
    expired["exp"] = json!(now - 120);
    expired["nbf"] = json!(now - 3600);
    let mut not_yet_valid = claims("user-0003", now);
    not_yet_valid["nbf"] = json!(now + 120);
    let empty_subject = claims("", now);

    for refused in [
        wrong_issuer,
        wrong_audience,
        expired,
        not_yet_valid,
        empty_subject,
    ] {
        let token = fixture.signer.mint(&refused);
        assert!(state
            .open_or_create_by_assertion(&token, None, now)
            .is_err());
    }

    let forger = TokenSigner::generate("signing-key-1")?;
    let forged = forger.mint(&claims("user-0003", now));
    assert!(state
        .open_or_create_by_assertion(&forged, None, now)
        .is_err());

    let mut array_audience = claims("user-0003", now);
    array_audience["aud"] = json!(["anon", AUDIENCE]);
    let accepted = fixture.signer.mint(&array_audience);
    assert!(state.assertion_principal(ISSUER, "user-0003").is_none());
    assert!(state.assertion_receipts().is_empty());
    let (principal, created) = state.open_or_create_by_assertion(&accepted, None, now)?;
    assert!(created);
    assert_eq!(principal.subject(), "user-0003");
    Ok(())
}

#[test]
fn assertion_did_recorded_on_first_login_and_returned_on_reopen() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0004", now));

    let (first, created) = state.open_or_create_by_assertion(&token, Some(WALLET_DID), now)?;
    assert!(created);
    assert_eq!(first.did(), Some(WALLET_DID));
    assert_eq!(
        state.assertion_receipts()[0].did.as_deref(),
        Some(WALLET_DID)
    );

    let (again, created) = state.open_or_create_by_assertion(&token, None, now)?;
    assert!(!created);
    assert_eq!(again.did(), Some(WALLET_DID));
    let (same, created) = state.open_or_create_by_assertion(&token, Some(WALLET_DID), now)?;
    assert!(!created);
    assert_eq!(same, first);
    assert_eq!(state.assertion_receipts().len(), 1);
    drop(state);

    let reopened = fixture.state()?;
    let resolved = reopened.resolve_assertion(&token, now)?;
    assert_eq!(resolved.did(), Some(WALLET_DID));
    assert_eq!(resolved.principal(), first.principal());
    assert_eq!(reopened.assertion_principal_by_did(WALLET_DID), Some(first));
    Ok(())
}

#[test]
fn assertion_did_supplied_after_creation_is_recorded_with_a_receipt() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0005", now));

    let (first, _) = state.open_or_create_by_assertion(&token, None, now)?;
    let (recorded, created) =
        state.open_or_create_by_assertion(&token, Some(WALLET_DID), now + 7)?;
    assert!(!created);
    assert_eq!(recorded.principal(), first.principal());
    assert_eq!(recorded.did(), Some(WALLET_DID));
    let receipts = state.assertion_receipts();
    assert_eq!(receipts.len(), 2);
    assert_eq!(receipts[1].event, "did.recorded.assertion");
    assert_eq!(receipts[1].did.as_deref(), Some(WALLET_DID));
    assert_eq!(receipts[1].recorded_at, now + 7);
    assert_chain(receipts)?;
    Ok(())
}

#[test]
fn assertion_different_did_for_the_same_account_is_refused() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0006", now));

    state.open_or_create_by_assertion(&token, Some(WALLET_DID), now)?;
    assert!(state
        .open_or_create_by_assertion(&token, Some(OTHER_DID), now)
        .is_err());
    let principal = state
        .assertion_principal(ISSUER, "user-0006")
        .ok_or("account missing")?;
    assert_eq!(principal.did(), Some(WALLET_DID));
    assert!(state.assertion_principal_by_did(OTHER_DID).is_none());
    assert_eq!(state.assertion_receipts().len(), 1);
    Ok(())
}

#[test]
fn assertion_same_did_for_another_subject_is_refused() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let owner = fixture.signer.mint(&claims("user-0007", now));
    let other = fixture.signer.mint(&claims("user-0008", now));

    state.open_or_create_by_assertion(&owner, Some(WALLET_DID), now)?;
    assert!(state
        .open_or_create_by_assertion(&other, Some(WALLET_DID), now)
        .is_err());
    assert!(state.assertion_principal(ISSUER, "user-0008").is_none());

    state.open_or_create_by_assertion(&other, None, now)?;
    assert!(state
        .open_or_create_by_assertion(&other, Some(WALLET_DID), now)
        .is_err());
    assert_eq!(
        state
            .assertion_principal(ISSUER, "user-0008")
            .ok_or("account missing")?
            .did(),
        None
    );
    Ok(())
}

#[test]
fn assertion_invalid_did_is_refused() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0009", now));
    for invalid in [
        "did:layerx:3F1C0A9E5B7D2468ACE013579BDF2468ACE013579BDF2468ACE013579BDF2468",
        "did:layerx:3f1c0a9e5b7d2468",
        "did:key:3f1c0a9e5b7d2468ace013579bdf2468ace013579bdf2468ace013579bdf2468",
        "did:layerx:0000000000000000000000000000000000000000000000000000000000000000",
    ] {
        assert!(state
            .open_or_create_by_assertion(&token, Some(invalid), now)
            .is_err());
    }
    assert!(state.assertion_principal(ISSUER, "user-0009").is_none());
    Ok(())
}

#[test]
fn assertion_hs256_and_unsigned_tokens_are_refused() -> Result {
    let fixture = Fixture::new()?;
    let mut state = fixture.state()?;
    let now = now()?;
    let body = claims("user-0010", now);

    let mut header = jsonwebtoken::Header::new(jsonwebtoken::Algorithm::HS256);
    header.kid = Some("signing-key-1".to_owned());
    let hs256 = jsonwebtoken::encode(
        &header,
        &body,
        &jsonwebtoken::EncodingKey::from_secret(b"shared-secret-known-to-the-client"),
    )?;
    assert!(state
        .open_or_create_by_assertion(&hs256, None, now)
        .is_err());

    let unsigned = format!(
        "{}.{}.",
        URL_SAFE_NO_PAD
            .encode(json!({"alg": "none", "typ": "JWT", "kid": "signing-key-1"}).to_string()),
        URL_SAFE_NO_PAD.encode(body.to_string())
    );
    assert!(state
        .open_or_create_by_assertion(&unsigned, None, now)
        .is_err());
    assert!(state.assertion_principal(ISSUER, "user-0010").is_none());
    assert_eq!(fixture.server.hits(), 0);
    Ok(())
}

#[test]
fn assertion_unknown_key_id_refreshes_only_after_the_minimum_interval() -> Result {
    let fixture = Fixture::new()?;
    let mut state = open_state(&fixture.root, config(fixture.server.url(), 1))?;
    let now = now()?;
    let first = fixture.signer.mint(&claims("user-0011", now));
    state.open_or_create_by_assertion(&first, None, now)?;
    assert_eq!(fixture.server.hits(), 1);

    let rotated = TokenSigner::generate("signing-key-2")?;
    fixture
        .server
        .replace(&[fixture.signer.jwk()?, rotated.jwk()?])?;
    let second = rotated.mint(&claims("user-0012", now));
    assert!(state
        .open_or_create_by_assertion(&second, None, now)
        .is_err());
    assert_eq!(fixture.server.hits(), 1);

    thread::sleep(Duration::from_millis(1100));
    let (principal, created) = state.open_or_create_by_assertion(&second, None, now)?;
    assert!(created);
    assert_eq!(principal.subject(), "user-0012");
    assert_eq!(fixture.server.hits(), 2);
    Ok(())
}

#[test]
fn assertion_disabled_principal_refuses_tokens() -> Result {
    let fixture = Fixture::new()?;
    let mut state = State::open(&fixture.root, policy())?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0013", now));
    assert!(state
        .open_or_create_by_assertion(&token, None, now)
        .is_err());
    assert!(state.resolve_assertion(&token, now).is_err());
    assert_eq!(fixture.server.hits(), 0);
    state.enable_assertion(AssertionVerifier::new(config(fixture.server.url(), 300))?)?;
    assert!(state
        .enable_assertion(AssertionVerifier::new(config(fixture.server.url(), 300))?)
        .is_err());
    state.open_or_create_by_assertion(&token, None, now)?;
    Ok(())
}

#[test]
fn assertion_configuration_is_absent_by_default_and_complete_when_set() -> Result {
    let empty: HashMap<&str, &str> = HashMap::new();
    let lookup = |values: &HashMap<&str, &str>| {
        let values: HashMap<String, String> = values
            .iter()
            .map(|(key, value)| ((*key).to_owned(), (*value).to_owned()))
            .collect();
        move |name: &str| Ok::<_, std::io::Error>(values.get(name).cloned())
    };
    assert_eq!(AssertionConfig::from_lookup(lookup(&empty))?, None);

    let mut values = HashMap::from([
        (
            "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_JWKS_URL",
            "https://identity.example.test/auth/v1/.well-known/jwks.json",
        ),
        ("LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_ISSUER", ISSUER),
        (
            "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_AUDIENCE",
            AUDIENCE,
        ),
    ]);
    let loaded = AssertionConfig::from_lookup(lookup(&values))?.ok_or("config missing")?;
    assert_eq!(loaded.issuer, ISSUER);
    assert_eq!(loaded.audience, AUDIENCE);
    assert_eq!(loaded.clock_skew_seconds, 60);
    assert_eq!(loaded.refresh_interval_seconds, 300);

    values.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_CLOCK_SKEW_SECONDS",
        "10",
    );
    values.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_REFRESH_INTERVAL_SECONDS",
        "30",
    );
    let tuned = AssertionConfig::from_lookup(lookup(&values))?.ok_or("config missing")?;
    assert_eq!(tuned.clock_skew_seconds, 10);
    assert_eq!(tuned.refresh_interval_seconds, 30);

    let mut partial = values.clone();
    partial.remove("LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_AUDIENCE");
    assert!(AssertionConfig::from_lookup(lookup(&partial)).is_err());

    let mut plaintext = values.clone();
    plaintext.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_JWKS_URL",
        "http://identity.example.test/auth/v1/.well-known/jwks.json",
    );
    assert!(AssertionConfig::from_lookup(lookup(&plaintext)).is_err());

    let mut zero_interval = values.clone();
    zero_interval.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_REFRESH_INTERVAL_SECONDS",
        "0",
    );
    assert!(AssertionConfig::from_lookup(lookup(&zero_interval)).is_err());

    let mut wide_skew = values;
    wide_skew.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_CLOCK_SKEW_SECONDS",
        "3600",
    );
    assert!(AssertionConfig::from_lookup(lookup(&wide_skew)).is_err());
    Ok(())
}
