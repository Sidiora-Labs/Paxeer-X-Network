use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_client::runtime_clock::RuntimeClock;
use layerx_human_identity_provider::{
    AssertionConfig, AssertionReceipt, AssertionRefusal, AssertionVerifier, Policy, Server, State,
};
use layerx_types::clock::{Clock as _, Deadline};
use p256::ecdsa::signature::Signer as _;
use p256::ecdsa::{Signature, SigningKey};
use serde_json::{json, Value};
use std::collections::HashMap;
use std::error::Error;
use std::fs;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::{OpenOptionsExt as _, PermissionsExt as _};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::atomic::{AtomicBool, AtomicU8, AtomicUsize, Ordering};
use std::sync::{Arc, Barrier, Mutex};
use std::thread::{self, JoinHandle};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

type Result<T = ()> = std::result::Result<T, Box<dyn Error>>;

const ISSUER: &str = "https://identity.example.test/auth/v1";
const AUDIENCE: &str = "authenticated";
const WALLET_DID: &str =
    "did:layerx:3f1c0a9e5b7d2468ace013579bdf2468ace013579bdf2468ace013579bdf2468";
const OTHER_DID: &str =
    "did:layerx:9a8b7c6d5e4f30211203f4e5d6c7b8a99a8b7c6d5e4f30211203f4e5d6c7b8a9";
const SERVE: u8 = 0;
const UNAVAILABLE: u8 = 1;
const REDIRECT: u8 = 2;
const PAST_INTERVAL: Duration = Duration::from_millis(1100);

struct KeySetServer {
    address: SocketAddr,
    body: Arc<Mutex<String>>,
    hits: Arc<AtomicUsize>,
    mode: Arc<AtomicU8>,
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl KeySetServer {
    fn serve(keys: &[Value]) -> Result<Self> {
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let address = listener.local_addr()?;
        let body = Arc::new(Mutex::new(json!({ "keys": keys }).to_string()));
        let hits = Arc::new(AtomicUsize::new(0));
        let mode = Arc::new(AtomicU8::new(SERVE));
        let shutdown = Arc::new(AtomicBool::new(false));
        let worker = {
            let body = Arc::clone(&body);
            let hits = Arc::clone(&hits);
            let mode = Arc::clone(&mode);
            let shutdown = Arc::clone(&shutdown);
            thread::spawn(move || {
                for stream in listener.incoming() {
                    if shutdown.load(Ordering::Acquire) {
                        break;
                    }
                    if let Ok(mut stream) = stream {
                        let _ = respond(&mut stream, &body, &hits, mode.load(Ordering::Acquire));
                    }
                }
            })
        };
        Ok(Self {
            address,
            body,
            hits,
            mode,
            shutdown,
            worker: Some(worker),
        })
    }

    fn url(&self) -> String {
        format!("http://{}/auth/v1/.well-known/jwks.json", self.address)
    }

    fn replace(&self, keys: &[Value]) -> Result {
        self.replace_document(&json!({ "keys": keys }))
    }

    fn replace_document(&self, document: &Value) -> Result {
        *self.body.lock().map_err(|_| "key set lock poisoned")? = document.to_string();
        Ok(())
    }

    fn set_mode(&self, mode: u8) {
        self.mode.store(mode, Ordering::Release);
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

fn respond(stream: &mut TcpStream, body: &Mutex<String>, hits: &AtomicUsize, mode: u8) -> Result {
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
    let followed = request.starts_with(b"GET /auth/v1/.well-known/jwks.json?followed ");
    if !followed && !request.starts_with(b"GET /auth/v1/.well-known/jwks.json ") {
        stream.write_all(
            b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
        )?;
        return Ok(());
    }
    hits.fetch_add(1, Ordering::AcqRel);
    if mode == UNAVAILABLE {
        stream.write_all(
            b"HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
        )?;
        return Ok(());
    }
    if mode == REDIRECT && !followed {
        stream.write_all(
            b"HTTP/1.1 302 Found\r\nLocation: /auth/v1/.well-known/jwks.json?followed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
        )?;
        return Ok(());
    }
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

    let mut hour = values.clone();
    hour.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_REFRESH_INTERVAL_SECONDS",
        "3600",
    );
    let hour = AssertionConfig::from_lookup(lookup(&hour))?.ok_or("config missing")?;
    assert_eq!(hour.refresh_interval_seconds, 3600);

    let mut beyond_lifetime = values.clone();
    beyond_lifetime.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_REFRESH_INTERVAL_SECONDS",
        "3601",
    );
    assert!(AssertionConfig::from_lookup(lookup(&beyond_lifetime)).is_err());
    let mut stale_config = config(
        "https://identity.example.test/auth/v1/.well-known/jwks.json".to_owned(),
        86_400,
    );
    assert!(stale_config.validate().is_err());
    stale_config.refresh_interval_seconds = 3600;
    stale_config.validate()?;

    let mut wide_skew = values;
    wide_skew.insert(
        "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_CLOCK_SKEW_SECONDS",
        "3600",
    );
    assert!(AssertionConfig::from_lookup(lookup(&wide_skew)).is_err());
    Ok(())
}

struct Running {
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<std::io::Result<()>>>,
}

impl Running {
    fn start(socket: &Path, state: State) -> Result<Self> {
        let server = Server::bind(
            socket,
            state,
            rustix::process::geteuid().as_raw(),
            Duration::from_secs(2),
            RuntimeClock::from_environment()?,
        )?;
        let shutdown = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&shutdown);
        Ok(Self {
            shutdown,
            worker: Some(thread::spawn(move || server.run(&flag))),
        })
    }

    fn stop(mut self) -> Result {
        self.shutdown.store(true, Ordering::Release);
        self.worker
            .take()
            .ok_or("worker missing")?
            .join()
            .map_err(|_| "worker panic")??;
        Ok(())
    }
}

struct OwnedChild(std::process::Child);

impl Drop for OwnedChild {
    fn drop(&mut self) {
        if matches!(self.0.try_wait(), Ok(None)) {
            let _ = self.0.kill();
            let _ = self.0.wait();
        }
    }
}

fn authority_now() -> Result<u64> {
    Ok(RuntimeClock::from_environment()?
        .sample(Duration::from_secs(1))?
        .unix_seconds())
}

fn request(operation: u8, fields: &[&[u8]]) -> Result<Vec<u8>> {
    let mut bytes = b"LXIP\x01".to_vec();
    bytes.push(operation);
    bytes.extend_from_slice(&u32::try_from(fields.len())?.to_be_bytes());
    for field in fields {
        bytes.extend_from_slice(&u32::try_from(field.len())?.to_be_bytes());
        bytes.extend_from_slice(field);
    }
    Ok(bytes)
}

fn exchange(socket: &Path, bytes: &[u8]) -> Result<Vec<u8>> {
    let mut stream = UnixStream::connect(socket)?;
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;
    stream.write_all(&u32::try_from(bytes.len())?.to_be_bytes())?;
    stream.write_all(bytes)?;
    let mut length = [0; 4];
    stream.read_exact(&mut length)?;
    let length = u32::from_be_bytes(length) as usize;
    assert!((10..=1_048_576).contains(&length));
    let mut response = vec![0; length];
    stream.read_exact(&mut response)?;
    Ok(response)
}

fn probe(socket: &Path) -> Result {
    assert_eq!(
        exchange(socket, &request(0, &[])?)?,
        b"LXIP\x01\x00\x00\x00\x00\x00"
    );
    Ok(())
}

fn operation_four(socket: &Path, fields: &[&[u8]]) -> Result<(u8, Vec<Vec<u8>>)> {
    let response = exchange(socket, &request(4, fields)?)?;
    assert_eq!(&response[..5], b"LXIP\x01");
    let count = u32::from_be_bytes(response[6..10].try_into()?);
    let mut remaining = &response[10..];
    let mut decoded = Vec::new();
    for _ in 0..count {
        let length = u32::from_be_bytes(remaining[..4].try_into()?) as usize;
        decoded.push(remaining[4..4 + length].to_vec());
        remaining = &remaining[4 + length..];
    }
    assert!(remaining.is_empty());
    Ok((response[5], decoded))
}

fn wait_ready(socket: &Path, child: &mut std::process::Child) -> Result {
    let clock = RuntimeClock::from_environment()?;
    let mut expires = Deadline::start(clock.as_ref(), Duration::from_secs(5))?;
    while !expires.remaining(clock.as_ref())?.is_zero() {
        if exchange(socket, &request(0, &[])?).is_ok() {
            return Ok(());
        }
        assert!(
            child.try_wait()?.is_none(),
            "provider exited during startup"
        );
        thread::sleep(Duration::from_millis(10));
    }
    Err("provider did not become ready".into())
}

#[test]
fn assertion_wire_operation_four_returns_principal_and_recorded_did() -> Result {
    let fixture = Fixture::new()?;
    let socket = fixture
        .root
        .parent()
        .ok_or("parent missing")?
        .join("identity.sock");
    let running = Running::start(&socket, fixture.state()?)?;
    let now = authority_now()?;
    let token = fixture.signer.mint(&claims("user-0014", now));

    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!(status, 0);
    assert_eq!(fields.len(), 1);
    let principal = String::from_utf8(fields[0].clone())?;
    assert!(principal.starts_with("act_"));

    let (status, fields) = operation_four(&socket, &[token.as_bytes(), WALLET_DID.as_bytes()])?;
    assert_eq!(status, 0);
    assert_eq!(
        fields,
        vec![
            principal.as_bytes().to_vec(),
            WALLET_DID.as_bytes().to_vec()
        ]
    );
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!(status, 0);
    assert_eq!(
        fields,
        vec![
            principal.as_bytes().to_vec(),
            WALLET_DID.as_bytes().to_vec()
        ]
    );

    let (status, fields) = operation_four(&socket, &[token.as_bytes(), OTHER_DID.as_bytes()])?;
    assert_eq!(
        (status, fields.len()),
        (AssertionRefusal::Identity.status(), 0)
    );
    let other = fixture.signer.mint(&claims("user-0015", now));
    let (status, _) = operation_four(&socket, &[other.as_bytes(), WALLET_DID.as_bytes()])?;
    assert_eq!(status, AssertionRefusal::Identity.status());

    let mut expired = claims("user-0014", now);
    expired["exp"] = json!(now - 120);
    let expired = fixture.signer.mint(&expired);
    let (status, fields) = operation_four(&socket, &[expired.as_bytes()])?;
    assert_eq!(
        (status, fields.len()),
        (AssertionRefusal::Assertion.status(), 0)
    );
    let forged = TokenSigner::generate("signing-key-1")?.mint(&claims("user-0014", now));
    let (status, _) = operation_four(&socket, &[forged.as_bytes()])?;
    assert_eq!(status, AssertionRefusal::Assertion.status());
    let (status, _) = operation_four(&socket, &[b""])?;
    assert_eq!(status, AssertionRefusal::Assertion.status());

    for malformed in [
        vec![token.as_bytes(), b"did:layerx:3f1c0a9e5b7d2468".as_slice()],
        vec![token.as_bytes(), b"\xff"],
        vec![b"\xff".as_slice()],
        vec![],
        vec![token.as_bytes(), WALLET_DID.as_bytes(), b"extra"],
    ] {
        let (status, fields) = operation_four(&socket, &malformed)?;
        assert_eq!((status, fields.len()), (1, 0));
    }
    probe(&socket)?;
    running.stop()?;
    assert_eq!(fixture.server.hits(), 1);

    let reopened = fixture.state()?;
    let resolved = reopened.resolve_assertion(&token, now)?;
    assert_eq!(resolved.principal(), principal);
    assert_eq!(resolved.did(), Some(WALLET_DID));
    assert!(reopened.assertion_principal(ISSUER, "user-0015").is_none());
    assert_eq!(reopened.assertion_receipts().len(), 2);
    assert_chain(reopened.assertion_receipts())?;
    Ok(())
}

#[test]
fn assertion_wire_operation_four_reports_unavailable_without_a_verifier_or_key_set() -> Result {
    let fixture = Fixture::new()?;
    let socket = fixture
        .root
        .parent()
        .ok_or("parent missing")?
        .join("identity.sock");
    let now = authority_now()?;
    let token = fixture.signer.mint(&claims("user-0016", now));

    let disabled = Running::start(&socket, State::open(&fixture.root, policy())?)?;
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!(
        (status, fields.len()),
        (AssertionRefusal::Unavailable.status(), 0)
    );
    probe(&socket)?;
    disabled.stop()?;

    let closed = TcpListener::bind("127.0.0.1:0")?;
    let unreachable = format!(
        "http://{}/auth/v1/.well-known/jwks.json",
        closed.local_addr()?
    );
    drop(closed);
    let running = Running::start(
        &socket,
        open_state(&fixture.root, config(unreachable, 300))?,
    )?;
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!(
        (status, fields.len()),
        (AssertionRefusal::Unavailable.status(), 0)
    );
    probe(&socket)?;
    running.stop()?;

    assert!(State::open(&fixture.root, policy())?
        .assertion_principal(ISSUER, "user-0016")
        .is_none());
    assert_eq!(fixture.server.hits(), 0);
    Ok(())
}

#[test]
fn assertion_binary_serve_wires_the_configuration_from_the_environment() -> Result {
    let fixture = Fixture::new()?;
    let directory = fixture.root.parent().ok_or("parent missing")?;
    let socket = directory.join("identity.sock");
    let policy_file = directory.join("policy.json");
    fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&policy_file)?
        .write_all(&serde_json::to_vec(&policy())?)?;
    let command = || {
        let mut command =
            std::process::Command::new(env!("CARGO_BIN_EXE_layerx-human-identity-provider"));
        command
            .arg("serve")
            .env("LAYERX_HUMAN_IDENTITY_PROVIDER_STATE_ROOT", &fixture.root)
            .env("LAYERX_HUMAN_IDENTITY_PROVIDER_SOCKET", &socket)
            .env(
                "LAYERX_HUMAN_IDENTITY_PROVIDER_RECOVERY_POLICY_FILE",
                &policy_file,
            )
            .env(
                "LAYERX_HUMAN_IDENTITY_PROVIDER_ALLOWED_UID",
                rustix::process::geteuid().as_raw().to_string(),
            );
        command
    };
    let now = authority_now()?;
    let token = fixture.signer.mint(&claims("user-0017", now));

    let mut configured = OwnedChild(
        command()
            .env(
                "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_JWKS_URL",
                fixture.server.url(),
            )
            .env("LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_ISSUER", ISSUER)
            .env(
                "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_AUDIENCE",
                AUDIENCE,
            )
            .spawn()?,
    );
    wait_ready(&socket, &mut configured.0)?;
    let (status, fields) = operation_four(&socket, &[token.as_bytes(), WALLET_DID.as_bytes()])?;
    assert_eq!(status, 0);
    assert_eq!(fields.len(), 2);
    assert!(fields[0].starts_with(b"act_"));
    assert_eq!(fields[1], WALLET_DID.as_bytes());
    assert_eq!(fixture.server.hits(), 1);
    let principal = String::from_utf8(fields[0].clone())?;
    drop(configured);

    let mut unconfigured = OwnedChild(command().spawn()?);
    wait_ready(&socket, &mut unconfigured.0)?;
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!(
        (status, fields.len()),
        (AssertionRefusal::Unavailable.status(), 0)
    );
    drop(unconfigured);

    let mut partial = OwnedChild(
        command()
            .env("LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_ISSUER", ISSUER)
            .spawn()?,
    );
    let clock = RuntimeClock::from_environment()?;
    let mut expires = Deadline::start(clock.as_ref(), Duration::from_secs(5))?;
    let status = loop {
        if let Some(status) = partial.0.try_wait()? {
            break status;
        }
        assert!(
            !expires.remaining(clock.as_ref())?.is_zero(),
            "partial assertion configuration was not refused"
        );
        thread::sleep(Duration::from_millis(10));
    };
    assert!(!status.success());

    let reopened = fixture.state()?;
    let resolved = reopened.resolve_assertion(&token, now)?;
    assert_eq!(resolved.principal(), principal);
    assert_eq!(resolved.did(), Some(WALLET_DID));
    Ok(())
}

fn refusal_kind(result: std::io::Result<impl std::fmt::Debug>) -> Result<std::io::ErrorKind> {
    match result {
        Ok(accepted) => Err(format!("token accepted: {accepted:?}").into()),
        Err(error) => Ok(error.kind()),
    }
}

#[test]
fn assertion_known_key_removed_from_the_key_set_is_refused_after_the_refresh_interval() -> Result {
    let fixture = Fixture::new()?;
    let verifier = AssertionVerifier::new(config(fixture.server.url(), 1))?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0018", now));
    assert_eq!(verifier.verify(&token, now)?.subject(), "user-0018");
    assert_eq!(fixture.server.hits(), 1);

    let successor = TokenSigner::generate("signing-key-2")?;
    fixture.server.replace(&[successor.jwk()?])?;
    let stranger = TokenSigner::generate("signing-key-3")?.mint(&claims("user-0019", now));
    for _ in 0..3 {
        assert_eq!(
            refusal_kind(verifier.verify(&stranger, now))?,
            std::io::ErrorKind::InvalidData
        );
        assert_eq!(verifier.verify(&token, now)?.subject(), "user-0018");
    }
    assert_eq!(fixture.server.hits(), 1);

    thread::sleep(PAST_INTERVAL);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::InvalidData
    );
    assert_eq!(fixture.server.hits(), 2);
    let successor_token = successor.mint(&claims("user-0020", now));
    assert_eq!(
        verifier.verify(&successor_token, now)?.subject(),
        "user-0020"
    );
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::InvalidData
    );
    assert_eq!(fixture.server.hits(), 2);
    Ok(())
}

#[test]
fn assertion_known_key_id_replaced_in_the_key_set_uses_the_new_key_after_the_refresh_interval(
) -> Result {
    let fixture = Fixture::new()?;
    let mut state = open_state(&fixture.root, config(fixture.server.url(), 1))?;
    let now = now()?;
    let original = fixture.signer.mint(&claims("user-0021", now));
    let (first, created) = state.open_or_create_by_assertion(&original, None, now)?;
    assert!(created);
    assert_eq!(fixture.server.hits(), 1);

    let replacement = TokenSigner::generate("signing-key-1")?;
    fixture.server.replace(&[replacement.jwk()?])?;
    let replaced = replacement.mint(&claims("user-0022", now));
    assert!(state
        .open_or_create_by_assertion(&replaced, None, now)
        .is_err());
    assert_eq!(
        state.open_or_create_by_assertion(&original, None, now)?,
        (first.clone(), false)
    );
    assert_eq!(fixture.server.hits(), 1);

    thread::sleep(PAST_INTERVAL);
    assert!(state.resolve_assertion(&original, now).is_err());
    assert_eq!(fixture.server.hits(), 2);
    let (second, created) = state.open_or_create_by_assertion(&replaced, None, now)?;
    assert!(created);
    assert_eq!(second.subject(), "user-0022");
    assert!(state
        .open_or_create_by_assertion(&original, None, now)
        .is_err());
    assert_eq!(state.assertion_principal(ISSUER, "user-0021"), Some(first));
    assert_eq!(fixture.server.hits(), 2);
    Ok(())
}

#[test]
fn assertion_key_set_outage_refuses_expired_keys_until_a_refresh_succeeds() -> Result {
    let fixture = Fixture::new()?;
    let verifier = AssertionVerifier::new(config(fixture.server.url(), 1))?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0023", now));
    verifier.verify(&token, now)?;
    assert_eq!(fixture.server.hits(), 1);

    fixture.server.set_mode(UNAVAILABLE);
    verifier.verify(&token, now)?;
    assert_eq!(fixture.server.hits(), 1);

    thread::sleep(PAST_INTERVAL);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 2);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 2);

    thread::sleep(PAST_INTERVAL);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 3);

    fixture.server.set_mode(SERVE);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 3);
    thread::sleep(PAST_INTERVAL);
    assert_eq!(verifier.verify(&token, now)?.subject(), "user-0023");
    assert_eq!(fixture.server.hits(), 4);
    Ok(())
}

#[test]
fn assertion_wire_operation_four_reports_an_expired_key_set_during_an_outage_as_unavailable(
) -> Result {
    let fixture = Fixture::new()?;
    let socket = fixture
        .root
        .parent()
        .ok_or("parent missing")?
        .join("identity.sock");
    let running = Running::start(
        &socket,
        open_state(&fixture.root, config(fixture.server.url(), 1))?,
    )?;
    let now = authority_now()?;
    let token = fixture.signer.mint(&claims("user-0024", now));
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!((status, fields.len()), (0, 1));
    let principal = fields[0].clone();

    fixture.server.set_mode(UNAVAILABLE);
    thread::sleep(PAST_INTERVAL);
    for expected_hits in [2, 2] {
        let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
        assert_eq!(
            (status, fields.len()),
            (AssertionRefusal::Unavailable.status(), 0)
        );
        assert_eq!(fixture.server.hits(), expected_hits);
    }

    fixture.server.set_mode(SERVE);
    thread::sleep(PAST_INTERVAL);
    let (status, fields) = operation_four(&socket, &[token.as_bytes()])?;
    assert_eq!((status, fields), (0, vec![principal]));
    assert_eq!(fixture.server.hits(), 3);
    probe(&socket)?;
    running.stop()?;
    Ok(())
}

#[test]
fn assertion_simultaneous_requests_share_one_key_set_fetch() -> Result {
    let fixture = Fixture::new()?;
    let verifier = Arc::new(AssertionVerifier::new(config(fixture.server.url(), 1))?);
    let now = now()?;
    let tokens: Vec<String> = (0..16)
        .map(|index| {
            fixture
                .signer
                .mint(&claims(&format!("user-01{index:02}"), now))
        })
        .collect();
    let simultaneous =
        |tokens: &[String]| -> Result<Vec<std::result::Result<String, std::io::ErrorKind>>> {
            let barrier = Arc::new(Barrier::new(tokens.len()));
            let workers: Vec<_> = tokens
                .iter()
                .cloned()
                .map(|token| {
                    let verifier = Arc::clone(&verifier);
                    let barrier = Arc::clone(&barrier);
                    thread::spawn(move || {
                        barrier.wait();
                        verifier
                            .verify(&token, now)
                            .map(|verified| verified.subject().to_owned())
                            .map_err(|error| error.kind())
                    })
                })
                .collect();
            workers
                .into_iter()
                .map(|worker| {
                    worker
                        .join()
                        .map_err(|_| Box::<dyn Error>::from("verifier thread panicked"))
                })
                .collect()
        };

    let accepted = simultaneous(&tokens)?;
    for (index, result) in accepted.into_iter().enumerate() {
        assert_eq!(result, Ok(format!("user-01{index:02}")));
    }
    assert_eq!(fixture.server.hits(), 1);

    fixture.server.set_mode(UNAVAILABLE);
    thread::sleep(PAST_INTERVAL);
    for result in simultaneous(&tokens)? {
        assert_eq!(result, Err(std::io::ErrorKind::Other));
    }
    assert_eq!(fixture.server.hits(), 2);
    Ok(())
}

#[test]
fn assertion_restart_assumes_no_cached_key() -> Result {
    let fixture = Fixture::new()?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0025", now));
    let mut state = fixture.state()?;
    let (first, _) = state.open_or_create_by_assertion(&token, None, now)?;
    assert_eq!(fixture.server.hits(), 1);
    drop(state);

    fixture.server.set_mode(UNAVAILABLE);
    let restarted = fixture.state()?;
    assert!(restarted.resolve_assertion(&token, now).is_err());
    assert_eq!(fixture.server.hits(), 2);
    assert_eq!(
        restarted.assertion_principal(ISSUER, "user-0025"),
        Some(first.clone())
    );
    drop(restarted);

    fixture.server.set_mode(SERVE);
    let successor = TokenSigner::generate("signing-key-2")?;
    fixture.server.replace(&[successor.jwk()?])?;
    let verifier = AssertionVerifier::new(config(fixture.server.url(), 300))?;
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::InvalidData
    );
    assert_eq!(fixture.server.hits(), 3);

    fixture.server.replace(&[fixture.signer.jwk()?])?;
    let restarted = fixture.state()?;
    assert_eq!(restarted.resolve_assertion(&token, now)?, first);
    assert_eq!(fixture.server.hits(), 4);
    Ok(())
}

#[test]
fn assertion_key_set_redirects_and_oversized_documents_are_refused() -> Result {
    let fixture = Fixture::new()?;
    let verifier = AssertionVerifier::new(config(fixture.server.url(), 1))?;
    let now = now()?;
    let token = fixture.signer.mint(&claims("user-0026", now));

    fixture.server.set_mode(REDIRECT);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 1);

    fixture.server.set_mode(SERVE);
    fixture.server.replace_document(&json!({
        "keys": [fixture.signer.jwk()?],
        "padding": "x".repeat(300 * 1024),
    }))?;
    thread::sleep(PAST_INTERVAL);
    assert_eq!(
        refusal_kind(verifier.verify(&token, now))?,
        std::io::ErrorKind::Other
    );
    assert_eq!(fixture.server.hits(), 2);

    fixture.server.replace(&[fixture.signer.jwk()?])?;
    thread::sleep(PAST_INTERVAL);
    assert_eq!(verifier.verify(&token, now)?.subject(), "user-0026");
    assert_eq!(fixture.server.hits(), 3);
    Ok(())
}
