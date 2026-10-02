use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_human_identity_provider::{AssertionConfig, AssertionVerifier, Policy, Server, State};
use layerx_human_service::server::production_auth::{
    authorize_bearer_execution, consume_context, AuthDiscoveryIndex, AuthorizationDisclosure, IndexAuthenticationKey,
};
use layerx_human_service::server::schema::ApiSchema;
use layerx_human_service::server::{
    IdentityDispatchError, IdentityProviderConfig, RemoteIdentityProvider,
};
use layerx_human_service::store::{
    AgentTenantId, PrincipalId, PrincipalStore, PrincipalTenancyAuthority, RetentionPeriod,
    RetentionPolicy, RowKey, StoreError, Table, TenancyDigest, TenancyMap,
};
use layerx_identity_binding::{Client, Config};
use layerx_types::clock::Clock as _;
use p256::ecdsa::signature::Signer as _;
use p256::ecdsa::{Signature, SigningKey};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::error::Error;
use std::fs;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::{symlink, PermissionsExt as _};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use std::thread::{self, JoinHandle};
use std::time::Duration;

type Result<T = ()> = std::result::Result<T, Box<dyn Error>>;

const ISSUER: &str = "https://identity.example.test/auth/v1";
const AUDIENCE: &str = "authenticated";
const TENANT: &str = "human-provider";
const BINDING_TYPE: &str = "layerx-wallet-binding+jwt";
const WALLET_DID: &str =
    "did:layerx:3f1c0a9e5b7d2468ace013579bdf2468ace013579bdf2468ace013579bdf2468";
const OTHER_DID: &str =
    "did:layerx:9a8b7c6d5e4f30211203f4e5d6c7b8a99a8b7c6d5e4f30211203f4e5d6c7b8a9";

struct KeySetServer {
    address: SocketAddr,
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl KeySetServer {
    fn serve(keys: &[Value]) -> Result<Self> {
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let address = listener.local_addr()?;
        let body = json!({ "keys": keys }).to_string();
        let shutdown = Arc::new(AtomicBool::new(false));
        let stop = Arc::clone(&shutdown);
        let worker = thread::spawn(move || {
            for stream in listener.incoming() {
                if stop.load(Ordering::Acquire) {
                    break;
                }
                if let Ok(mut stream) = stream {
                    let _ = respond(&mut stream, &body);
                }
            }
        });
        Ok(Self {
            address,
            shutdown,
            worker: Some(worker),
        })
    }

    fn url(&self) -> String {
        format!("http://{}/auth/v1/.well-known/jwks.json", self.address)
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

fn respond(stream: &mut TcpStream, body: &str) -> Result {
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
    write!(
        stream,
        "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    )?;
    stream.flush()?;
    Ok(())
}

struct Signer(SigningKey);

impl Signer {
    fn generate() -> Result<Self> {
        loop {
            let mut scalar = [0u8; 32];
            getrandom::fill(&mut scalar)?;
            if let Ok(key) = SigningKey::from_slice(&scalar) {
                return Ok(Self(key));
            }
        }
    }

    fn jwk(&self, key_id: &str) -> Result<Value> {
        let point = self.0.verifying_key().to_encoded_point(false);
        Ok(json!({
            "kty": "EC", "crv": "P-256", "kid": key_id, "use": "sig", "alg": "ES256",
            "x": URL_SAFE_NO_PAD.encode(point.x().ok_or("missing x")?),
            "y": URL_SAFE_NO_PAD.encode(point.y().ok_or("missing y")?),
        }))
    }

    fn sec1_hex(&self) -> String {
        self.0
            .verifying_key()
            .to_encoded_point(false)
            .as_bytes()
            .iter()
            .map(|byte| format!("{byte:02x}"))
            .collect()
    }

    fn mint(&self, header: &Value, claims: &Value) -> String {
        let input = format!(
            "{}.{}",
            URL_SAFE_NO_PAD.encode(header.to_string()),
            URL_SAFE_NO_PAD.encode(claims.to_string())
        );
        let signature: Signature = self.0.sign(input.as_bytes());
        format!("{input}.{}", URL_SAFE_NO_PAD.encode(signature.to_bytes()))
    }

    fn assertion(&self, subject: &str, now: u64) -> String {
        self.mint(
            &json!({"alg": "ES256", "typ": "JWT", "kid": "supabase-1"}),
            &json!({"iss": ISSUER, "sub": subject, "aud": AUDIENCE, "exp": now + 3600,
                "iat": now, "nbf": now - 5, "role": "authenticated"}),
        )
    }

    fn binding(&self, subject: &str, did: &str, tenant: &str) -> String {
        self.mint(
            &json!({"alg": "ES256", "typ": BINDING_TYPE}),
            &json!({"iss": ISSUER, "sub": subject, "did": did, "tenant": tenant}),
        )
    }
}

fn authority_now() -> Result<u64> {
    Ok(
        layerx_client::runtime_clock::RuntimeClock::from_environment()?
            .sample(Duration::from_secs(1))?
            .unix_seconds(),
    )
}

fn verifier(keys: &KeySetServer, producer: &Signer) -> Result<AssertionVerifier> {
    Ok(AssertionVerifier::new(AssertionConfig {
        jwks_url: keys.url(),
        issuer: ISSUER.to_owned(),
        audience: AUDIENCE.to_owned(),
        clock_skew_seconds: 30,
        refresh_interval_seconds: 300,
    })?
    .with_binding_producer_key(&producer.sec1_hex())?)
}

fn identity(root: &Path, peer_gid: u32) -> Result<RemoteIdentityProvider> {
    Ok(RemoteIdentityProvider::new(IdentityProviderConfig {
        socket: root.join("identity.sock"),
        deadline: Duration::from_secs(1),
        maximum_frame_bytes: 65_536,
        peer_uid: rustix::process::geteuid().as_raw(),
        peer_gid,
    })?)
}

#[derive(Debug)]
struct Authority(Client);
impl PrincipalTenancyAuthority for Authority {
    fn tenant_for(
        &self,
        principal: &PrincipalId,
    ) -> std::result::Result<AgentTenantId, StoreError> {
        let binding = self.0.lookup(principal.as_str())?;
        AgentTenantId::new(binding.agent_tenant())
    }
}

struct Running {
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<std::io::Result<()>>>,
}
impl Running {
    fn start(root: &Path) -> Result<Self> {
        Self::start_with(root, None)
    }

    fn start_with(root: &Path, assertion: Option<AssertionVerifier>) -> Result<Self> {
        let uid = rustix::process::geteuid().as_raw();
        let mut state = State::open(
            &root.join("identity"),
            Policy {
                root: [0x43; 32],
                threshold: 1,
                delay_seconds: 86_400,
            },
        )?;
        if let Some(verifier) = assertion {
            state.enable_assertion(verifier)?;
        }
        let server = Server::bind(
            &root.join("identity.sock"),
            state,
            uid,
            Duration::from_secs(1),
            layerx_client::runtime_clock::RuntimeClock::from_environment()?,
        )?
        .with_binding_reader(&root.join("binding.sock"), "human-provider", &[uid])?;
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
            .map_err(|_| "provider panicked")??;
        Ok(())
    }
}
impl Drop for Running {
    fn drop(&mut self) {
        self.shutdown.store(true, Ordering::Release);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

fn provision(root: &Path, email: &str, idempotency: &str) -> Result<PrincipalId> {
    let mut request = b"LXIP\x01\x01".to_vec();
    let fields = [
        email.as_bytes(),
        b"Person",
        idempotency.as_bytes(),
        &1_u64.to_be_bytes(),
    ];
    request.extend_from_slice(&4_u32.to_be_bytes());
    for field in fields {
        request.extend_from_slice(&u32::try_from(field.len())?.to_be_bytes());
        request.extend_from_slice(field);
    }
    let mut connection = UnixStream::connect(root.join("identity.sock"))?;
    connection.set_read_timeout(Some(Duration::from_secs(1)))?;
    connection.set_write_timeout(Some(Duration::from_secs(1)))?;
    connection.write_all(&u32::try_from(request.len())?.to_be_bytes())?;
    connection.write_all(&request)?;
    let mut size = [0; 4];
    connection.read_exact(&mut size)?;
    let size = u32::from_be_bytes(size) as usize;
    assert!((10..=4096).contains(&size));
    let mut response = vec![0; size];
    connection.read_exact(&mut response)?;
    assert_eq!(&response[..10], b"LXIP\x01\x00\x00\x00\x00\x05");
    let length = u32::from_be_bytes(response[10..14].try_into()?) as usize;
    assert!((1..=128).contains(&length));
    Ok(PrincipalId::new(std::str::from_utf8(
        &response[14..14 + length],
    )?)?)
}

fn client(root: &Path) -> Result<Client> {
    tenant_client(root, TENANT)
}

fn tenant_client(root: &Path, tenant: &str) -> Result<Client> {
    Ok(Client::new(
        Config {
            socket: root.join("binding.sock"),
            tenant: tenant.into(),
            peer_uid: rustix::process::geteuid().as_raw(),
            peer_gid: rustix::process::getegid().as_raw(),
            deadline: Duration::from_secs(1),
        },
        layerx_client::runtime_clock::RuntimeClock::from_environment()?,
    )?)
}

fn open(root: &Path, digest: TenancyDigest, client: Client) -> Result<PrincipalStore> {
    let period = RetentionPeriod::new(1_000);
    Ok(PrincipalStore::open_with_authority(
        root,
        RetentionPolicy {
            journeys: period,
            notifications: period,
            audit: period,
            telemetry: period,
            cache: period,
        },
        digest,
        Arc::new(Authority(client)),
    )?)
}

#[test]
fn actual_provider_binding_preserves_dynamic_store_isolation_and_restart() -> Result {
    let directory = tempfile::Builder::new()
        .permissions(fs::Permissions::from_mode(0o700))
        .tempdir()?;
    let root = directory.path();
    let running = Running::start(root)?;
    let alice = provision(root, "alice@example.com", "alice")?;
    let bob = provision(root, "bob@example.com", "bob")?;
    assert_eq!(provision(root, "alice@example.com", "alice")?, alice);
    let client = client(root)?;
    let store_root = root.join("store");
    let digest = TenancyMap::new([])?.install(&store_root)?;
    let mut store = open(&store_root, digest, client.clone())?;
    assert!(open(&store_root, digest, client.clone()).is_err());
    let key = RowKey::new("record")?;
    {
        let mut scope = store.principal(&alice)?;
        assert_eq!(
            scope.tenant().as_str(),
            client.lookup(alice.as_str())?.agent_tenant()
        );
        scope.put(Table::Journeys, key.clone(), 1, b"alice".to_vec())?;
    }
    {
        let mut scope = store.principal(&bob)?;
        assert!(scope.get(Table::Journeys, &key).is_none());
        scope.put(Table::Journeys, key.clone(), 1, b"bob".to_vec())?;
    }
    let mut expected = vec![alice.clone(), bob.clone()];
    expected.sort();
    assert_eq!(store.known_principals()?, expected);
    assert!(store
        .principal(&PrincipalId::new("unprovisioned")?)
        .is_err());
    drop(store);
    running.stop()?;
    assert!(open(&store_root, digest, client.clone()).is_err());
    let running = Running::start(root)?;
    let mut store = open(&store_root, digest, client)?;
    assert_eq!(
        store
            .principal(&alice)?
            .get(Table::Journeys, &key)
            .ok_or("alice row")?
            .bytes(),
        b"alice"
    );
    assert_eq!(
        store
            .principal(&bob)?
            .get(Table::Journeys, &key)
            .ok_or("bob row")?
            .bytes(),
        b"bob"
    );
    running.stop()?;
    assert!(store.principal(&alice).is_err());
    Ok(())
}

#[test]
fn actual_provider_refuses_static_conflicts_and_durable_binding_replacement() -> Result {
    let directory = tempfile::Builder::new()
        .permissions(fs::Permissions::from_mode(0o700))
        .tempdir()?;
    let root = directory.path();
    let running = Running::start(root)?;
    let principal = provision(root, "owner@example.com", "owner")?;
    let client = client(root)?;
    let static_root = root.join("static-store");
    let digest = TenancyMap::new([(principal.clone(), AgentTenantId::new("wrong-tenant")?)])?
        .install(&static_root)?;
    assert!(open(&static_root, digest, client.clone()).is_err());
    let store_root = root.join("store");
    let digest = TenancyMap::new([])?.install(&store_root)?;
    let mut store = open(&store_root, digest, client.clone())?;
    store.principal(&principal)?.put(
        Table::Journeys,
        RowKey::new("record")?,
        1,
        b"durable".to_vec(),
    )?;
    drop(store);
    let binding = store_root
        .join("principals")
        .join(principal.as_str())
        .join("provider-binding");
    let original = fs::read(&binding)?;
    fs::write(&binding, b"changed-tenant")?;
    assert!(open(&store_root, digest, client.clone()).is_err());
    fs::write(&binding, original)?;
    let mut store = open(&store_root, digest, client.clone())?;
    assert!(store
        .principal(&principal)?
        .get(Table::Journeys, &RowKey::new("record")?)
        .is_some());
    drop(store);
    let missing = root.join("missing");
    fs::remove_file(&binding)?;
    symlink(&missing, &binding)?;
    assert!(open(&store_root, digest, client).is_err());
    assert!(!missing.exists());
    assert!(binding.is_symlink());
    running.stop()?;
    Ok(())
}

fn refused(result: std::result::Result<impl std::fmt::Debug, IdentityDispatchError>) -> bool {
    matches!(result, Err(IdentityDispatchError::ProviderRefused))
}

#[test]
fn assertion_subject_resolves_one_durable_wallet_principal_and_tenant_across_restart() -> Result {
    let directory = tempfile::Builder::new()
        .permissions(fs::Permissions::from_mode(0o700))
        .tempdir()?;
    let root = directory.path();
    let supabase = Signer::generate()?;
    let producer = Signer::generate()?;
    let forger = Signer::generate()?;
    let keys = KeySetServer::serve(&[supabase.jwk("supabase-1")?])?;
    let running = Running::start_with(root, Some(verifier(&keys, &producer)?))?;
    let now = authority_now()?;
    let egid = rustix::process::getegid().as_raw();
    let identity = identity(root, egid)?;
    let client = client(root)?;
    let store_root = root.join("store");
    let digest = TenancyMap::new([])?.install(&store_root)?;
    let mut store = open(&store_root, digest, client.clone())?;
    let alice_token = supabase.assertion("supabase-user-a", now);

    let pending = identity.resolve_assertion(&alice_token)?;
    assert!(pending.binding_pending());
    assert!(client.lookup(pending.principal.as_str()).is_err());
    assert!(store.principal(&pending.principal).is_err());

    let alice_binding = producer.binding("supabase-user-a", WALLET_DID, TENANT);
    for offered in [
        forger.binding("supabase-user-a", WALLET_DID, TENANT),
        producer.binding("supabase-user-b", WALLET_DID, TENANT),
        producer.binding("supabase-user-a", WALLET_DID, "other-tenant"),
        WALLET_DID.to_owned(),
    ] {
        assert!(refused(
            identity.resolve_assertion_with_binding(&alice_token, Some(&offered))
        ));
    }
    let unverified = forger.assertion("supabase-user-a", now);
    assert!(refused(identity.resolve_assertion_with_binding(
        &unverified,
        Some(&alice_binding)
    )));
    assert_eq!(identity.resolve_assertion(&alice_token)?, pending);

    let alice = identity.resolve_assertion_with_binding(&alice_token, Some(&alice_binding))?;
    assert_eq!(alice.principal, pending.principal);
    assert_eq!(alice.did.as_deref(), Some(WALLET_DID));
    assert_eq!(identity.resolve_assertion(&alice_token)?, alice);
    assert_eq!(
        identity.resolve_assertion_with_binding(&alice_token, Some(&alice_binding))?,
        alice
    );
    assert!(refused(identity.resolve_assertion_with_binding(
        &alice_token,
        Some(&producer.binding("supabase-user-a", OTHER_DID, TENANT))
    )));

    let bob_token = supabase.assertion("supabase-user-b", now);
    assert!(refused(identity.resolve_assertion_with_binding(
        &bob_token,
        Some(&producer.binding("supabase-user-b", WALLET_DID, TENANT))
    )));
    let bob = identity.resolve_assertion_with_binding(
        &bob_token,
        Some(&producer.binding("supabase-user-b", OTHER_DID, TENANT)),
    )?;
    assert_ne!(bob.principal, alice.principal);
    assert_eq!(bob.did.as_deref(), Some(OTHER_DID));
    let carol = provision(root, "carol@example.com", "carol")?;
    assert!(carol != alice.principal && carol != bob.principal);

    let binding = client.lookup(alice.principal.as_str())?;
    assert_eq!(binding.did().as_bytes(), WALLET_DID.as_bytes());
    assert_eq!(binding.tenant(), TENANT);
    let key = RowKey::new("record")?;
    {
        let mut scope = store.principal(&alice.principal)?;
        assert_eq!(scope.tenant().as_str(), binding.agent_tenant());
        scope.put(Table::Journeys, key.clone(), 1, b"alice".to_vec())?;
    }
    {
        let scope = store.principal(&bob.principal)?;
        assert!(scope.get(Table::Journeys, &key).is_none());
        assert_ne!(scope.tenant().as_str(), binding.agent_tenant());
    }
    assert_ne!(
        store.principal(&carol)?.tenant().as_str(),
        binding.agent_tenant()
    );

    let index = AuthDiscoveryIndex::open(
        root.join("auth-index"),
        IndexAuthenticationKey::new([0x5a; 32]).map_err(|_| "index key refused")?,
    )
    .map_err(|_| "index refused")?;
    let schema = ApiSchema::v1().map_err(|_| "schema refused")?;
    let operation = schema
        .operation("intent.plan")
        .ok_or("intent.plan missing")?;
    let body = json!({"amount": "1"});
    let path_parameters = BTreeMap::new();
    let disclosure = || AuthorizationDisclosure {
        operation,
        destination: "/v1/intents/plan",
        path_parameters: &path_parameters,
        body: &body,
        idempotency_key: None,
        trace: "trace-identity-binding",
    };
    let capability = authorize_bearer_execution(
        &mut store,
        &index,
        &alice.principal,
        &alice_token,
        disclosure(),
        now,
        30,
    )
    .map_err(|_| "bearer capability refused")?;
    assert_eq!(capability.principal(), &alice.principal);
    assert_eq!(capability.tenant().as_str(), binding.agent_tenant());
    assert_eq!(capability.operation(), "intent.plan");
    let mut context = capability
        .into_bearer_context(&alice_token, WALLET_DID)
        .map_err(|_| "capability context refused")?;
    assert_eq!(context.principal, alice.principal);
    assert_eq!(context.tenant.as_str(), binding.agent_tenant());
    assert_eq!(context.assertion(), Some(alice_token.as_str()));
    let changed_body = json!({"amount": "2"});
    assert!(consume_context(&index, &context, AuthorizationDisclosure {
        body: &changed_body,
        ..disclosure()
    }, now).is_err());
    assert!(consume_context(&index, &context, AuthorizationDisclosure {
        destination: "/v1/intents/other",
        ..disclosure()
    }, now).is_err());
    let changed_operation = schema.operation("intent.submit").ok_or("intent.submit missing")?;
    assert!(consume_context(&index, &context, AuthorizationDisclosure {
        operation: changed_operation,
        ..disclosure()
    }, now).is_err());
    context.principal = bob.principal.clone();
    assert!(consume_context(&index, &context, disclosure(), now).is_err());
    context.principal = alice.principal.clone();
    context.tenant = AgentTenantId::new("other-tenant")?;
    assert!(consume_context(&index, &context, disclosure(), now).is_err());
    context.tenant = AgentTenantId::new(binding.agent_tenant())?;
    let session = context.session_id.clone();
    context.session_id = layerx_human_service::server::production_auth::bearer_session_id(&bob_token);
    assert!(consume_context(&index, &context, disclosure(), now).is_err());
    context.session_id = session;
    assert!(consume_context(&index, &context, disclosure(), now + 31).is_err());
    drop(index);
    let index = AuthDiscoveryIndex::open(
        root.join("auth-index"),
        IndexAuthenticationKey::new([0x5a; 32]).map_err(|_| "index key refused")?,
    ).map_err(|_| "index reopen refused")?;
    consume_context(&index, &context, disclosure(), now)
        .map_err(|_| "persisted capability refused")?;
    assert!(consume_context(&index, &context, disclosure(), now).is_err());
    let unsigned_context = authorize_bearer_execution(
        &mut store, &index, &alice.principal, &alice_token, disclosure(), now, 30,
    ).map_err(|_| "bearer capability refused")?
        .into_context().map_err(|_| "capability context refused")?;
    assert!(consume_context(&index, &unsigned_context, disclosure(), now).is_err());
    let replaced_assertion = authorize_bearer_execution(
        &mut store, &index, &alice.principal, &alice_token, disclosure(), now, 30,
    ).map_err(|_| "bearer capability refused")?;
    assert!(replaced_assertion.into_bearer_context(&bob_token, OTHER_DID).is_err());
    let carol_did = String::from_utf8(client.lookup(carol.as_str())?.did().as_bytes().to_vec())?;
    assert!(refused(identity.resolve_assertion_with_binding(
        &supabase.assertion("supabase-user-e", now),
        Some(&producer.binding("supabase-user-e", &carol_did, TENANT)),
    )));
    let mut wrong_gid = Config {
        socket: root.join("binding.sock"), tenant: TENANT.into(),
        peer_uid: rustix::process::geteuid().as_raw(), peer_gid: egid.wrapping_add(1),
        deadline: Duration::from_secs(1),
    };
    assert!(Client::new(wrong_gid.clone(), layerx_client::runtime_clock::RuntimeClock::from_environment()?)?
        .lookup(alice.principal.as_str()).is_err());
    wrong_gid.peer_uid = wrong_gid.peer_uid.wrapping_add(1);
    assert!(Client::new(wrong_gid, layerx_client::runtime_clock::RuntimeClock::from_environment()?).is_err());
    let dave = identity.resolve_assertion(&supabase.assertion("supabase-user-d", now))?;
    assert!(dave.binding_pending());
    assert!(authorize_bearer_execution(
        &mut store,
        &index,
        &dave.principal,
        &alice_token,
        disclosure(),
        now,
        30,
    )
    .is_err());

    assert!(tenant_client(root, "other-tenant")?
        .lookup(alice.principal.as_str())
        .is_err());
    assert!(matches!(
        self::identity(root, egid.wrapping_add(1))?.resolve_assertion(&alice_token),
        Err(IdentityDispatchError::ProviderAuthentication)
    ));

    drop(store);
    running.stop()?;
    assert!(identity.resolve_assertion(&alice_token).is_err());
    let running = Running::start_with(root, Some(verifier(&keys, &producer)?))?;
    assert_eq!(identity.resolve_assertion(&alice_token)?, alice);
    assert_eq!(identity.resolve_assertion(&bob_token)?, bob);
    assert!(identity
        .resolve_assertion(&supabase.assertion("supabase-user-d", now))?
        .binding_pending());
    let mut store = open(&store_root, digest, client.clone())?;
    assert_eq!(
        store
            .principal(&alice.principal)?
            .get(Table::Journeys, &key)
            .ok_or("alice row")?
            .bytes(),
        b"alice"
    );
    assert_eq!(
        client.lookup(alice.principal.as_str())?.agent_tenant(),
        binding.agent_tenant()
    );
    assert!(store.principal(&dave.principal).is_err());
    running.stop()?;
    Ok(())
}

#[derive(serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct AttestorAdmissionFixture {
    nodes: Vec<(String, SocketAddr)>,
    root_certificate: String,
    client_certificate: String,
    client_private_key: String,
    jwks_url: String,
    assertion: String,
    subject: String,
}

#[test]
fn assertion_capability_reaches_existing_attestor_admission() -> Result {
    use layerx_human_service::server::production_components::{AttestorCustodyConfig, AttestorKms};
    let path = std::env::var("PAXEER_X_IDENTITY_ATTESTOR_FIXTURE")
        .map_err(|_| "missing isolated attestor admission fixture")?;
    let fixture: AttestorAdmissionFixture = serde_json::from_slice(&fs::read(path)?)?;
    if fixture.nodes.len() < 3 || fixture.nodes.iter().any(|(_, address)| !address.ip().is_loopback()) {
        return Err("attestor admission requires an isolated local quorum".into());
    }
    let config = AttestorCustodyConfig::new(
        fixture.nodes.clone(), fixture.nodes.iter().map(|(id, _)| id.clone()).collect(),
        fs::read(&fixture.root_certificate)?, fs::read(&fixture.client_certificate)?,
        fs::read(&fixture.client_private_key)?, Duration::from_secs(5),
    ).map_err(|_| "attestor configuration refused")?;
    let kms = AttestorKms::connect(config).map_err(|_| "actual attestor quorum unavailable")?;
    let directory = tempfile::Builder::new()
        .permissions(fs::Permissions::from_mode(0o700)).tempdir()?;
    let root = directory.path();
    let producer = Signer::generate()?;
    let verifier = AssertionVerifier::new(AssertionConfig {
        jwks_url: fixture.jwks_url,
        issuer: ISSUER.to_owned(), audience: AUDIENCE.to_owned(),
        clock_skew_seconds: 30, refresh_interval_seconds: 300,
    })?.with_binding_producer_key(&producer.sec1_hex())?;
    let running = Running::start_with(root, Some(verifier))?;
    let identity = identity(root, rustix::process::getegid().as_raw())?;
    let account = identity.resolve_assertion_with_binding(
        &fixture.assertion, Some(&producer.binding(&fixture.subject, WALLET_DID, TENANT)),
    )?;
    let store_root = root.join("store");
    let digest = TenancyMap::new([])?.install(&store_root)?;
    let mut store = open(&store_root, digest, client(root)?)?;
    let index = AuthDiscoveryIndex::open(
        root.join("auth-index"), IndexAuthenticationKey::new([0x6b; 32])
            .map_err(|_| "index key refused")?,
    ).map_err(|_| "index refused")?;
    let schema = ApiSchema::v1().map_err(|_| "schema refused")?;
    let operation = schema.operation("intent.plan").ok_or("intent.plan missing")?;
    let body = json!({"amount": "1"});
    let parameters = BTreeMap::new();
    let disclosure = AuthorizationDisclosure {
        operation, destination: "/v1/intents/plan", path_parameters: &parameters,
        body: &body, idempotency_key: None, trace: "trace-attestor-admission",
    };
    let now = authority_now()?;
    let capability = authorize_bearer_execution(
        &mut store, &index, &account.principal, &fixture.assertion, disclosure, now, 30,
    ).map_err(|_| "bearer capability refused")?;
    let mut context = capability.into_bearer_context(
        &fixture.assertion, account.did.as_deref().ok_or("binding pending")?,
    ).map_err(|_| "bearer context refused")?;
    consume_context(&index, &context, disclosure, now)
        .map_err(|_| "bearer capability consumption refused")?;
    assert_eq!(context.assertion(), Some(fixture.assertion.as_str()));
    kms.admit_context_assertion(&context).map_err(|_| "attestor admission refused")?;
    assert!(kms.admit_assertion("different-subject", &fixture.assertion).is_err());
    context.session_id = "different-session".to_owned();
    assert!(kms.admit_context_assertion(&context).is_err());
    running.stop()?;
    Ok(())
}
