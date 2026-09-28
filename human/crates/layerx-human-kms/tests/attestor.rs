use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_crypto::disclosure::{bind, Disclosure};
use layerx_crypto::signer::SignError;
use layerx_human_kms::attestor::{AttestorClient, AttestorError, AttestorSigner};
use layerx_intents::canonical::Domain;
use p256::ecdsa::signature::Signer as _;
use p256::ecdsa::{Signature, SigningKey};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::error::Error;
use std::fs;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::os::unix::fs::PermissionsExt as _;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

type Result<T> = std::result::Result<T, Box<dyn Error>>;

const NETWORK: u32 = 77;
const CHAIN_ID: &str = "125";
const ISSUER: &str = "attestor-test-issuer";
const AUDIENCE: &str = "attestor-test-audience";
const OWNER: &str = "user-0001";
const STRANGER: &str = "user-0002";
const ACCOUNT: &str = "0x00000000000000000000000000000000000000a1";
const NODES: usize = 5;
const SIGNERS: [&str; 3] = ["node-1", "node-3", "node-5"];

static SCRATCH: AtomicU64 = AtomicU64::new(0);

fn checked<T, E: std::fmt::Debug>(value: std::result::Result<T, E>) -> Result<T> {
    value.map_err(|error| format!("{error:?}").into())
}

fn scratch(label: &str) -> Result<PathBuf> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let root = std::env::temp_dir().join(format!(
        "{label}-{}-{nanos}-{}",
        std::process::id(),
        SCRATCH.fetch_add(1, Ordering::AcqRel)
    ));
    fs::create_dir(&root)?;
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700))?;
    Ok(root)
}

fn openssl(dir: &Path, args: &[&str]) -> Result<()> {
    let output = Command::new("openssl")
        .current_dir(dir)
        .args(args)
        .stdin(Stdio::null())
        .output()?;
    if !output.status.success() {
        return Err(format!(
            "openssl {} failed: {}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr)
        )
        .into());
    }
    Ok(())
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

struct Pki {
    dir: PathBuf,
    serial: u64,
}

impl Pki {
    fn new(dir: &Path) -> Self {
        Self {
            dir: dir.to_path_buf(),
            serial: 1,
        }
    }

    fn authority(&self, name: &str) -> Result<()> {
        openssl(
            &self.dir,
            &[
                "req",
                "-x509",
                "-newkey",
                "ec",
                "-pkeyopt",
                "ec_paramgen_curve:prime256v1",
                "-nodes",
                "-keyout",
                &format!("{name}.key"),
                "-out",
                &format!("{name}.pem"),
                "-days",
                "1",
                "-subj",
                &format!("/CN={name}"),
                "-addext",
                "basicConstraints=critical,CA:TRUE",
            ],
        )
    }

    fn leaf(&mut self, authority: &str, name: &str, server: bool) -> Result<()> {
        openssl(
            &self.dir,
            &[
                "req",
                "-new",
                "-newkey",
                "ec",
                "-pkeyopt",
                "ec_paramgen_curve:prime256v1",
                "-nodes",
                "-keyout",
                &format!("{name}.key"),
                "-out",
                &format!("{name}.csr"),
                "-subj",
                &format!("/CN={name}"),
            ],
        )?;
        let extensions = if server {
            "basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth,clientAuth\nsubjectAltName=IP:127.0.0.1\n"
        } else {
            "basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=clientAuth\n"
        };
        fs::write(self.dir.join(format!("{name}.ext")), extensions)?;
        self.serial += 1;
        openssl(
            &self.dir,
            &[
                "x509",
                "-req",
                "-in",
                &format!("{name}.csr"),
                "-CA",
                &format!("{authority}.pem"),
                "-CAkey",
                &format!("{authority}.key"),
                "-set_serial",
                &self.serial.to_string(),
                "-days",
                "1",
                "-sha256",
                "-extfile",
                &format!("{name}.ext"),
                "-out",
                &format!("{name}.pem"),
            ],
        )
    }

    fn certificate_der(&self, name: &str) -> Result<Vec<u8>> {
        openssl(
            &self.dir,
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
        Ok(fs::read(self.dir.join(format!("{name}.der")))?)
    }

    fn key_der(&self, name: &str) -> Result<Vec<u8>> {
        openssl(
            &self.dir,
            &[
                "pkcs8",
                "-topk8",
                "-nocrypt",
                "-in",
                &format!("{name}.key"),
                "-outform",
                "DER",
                "-out",
                &format!("{name}.pk8"),
            ],
        )?;
        Ok(fs::read(self.dir.join(format!("{name}.pk8")))?)
    }

    fn pin(&self, name: &str) -> Result<String> {
        openssl(
            &self.dir,
            &[
                "x509",
                "-in",
                &format!("{name}.pem"),
                "-noout",
                "-pubkey",
                "-out",
                &format!("{name}.pub"),
            ],
        )?;
        openssl(
            &self.dir,
            &[
                "pkey",
                "-pubin",
                "-in",
                &format!("{name}.pub"),
                "-outform",
                "DER",
                "-out",
                &format!("{name}.spki"),
            ],
        )?;
        Ok(hex(&Sha256::digest(fs::read(
            self.dir.join(format!("{name}.spki")),
        )?)))
    }

    fn path(&self, file: &str) -> PathBuf {
        self.dir.join(file)
    }
}

fn free_addresses(count: usize) -> Result<Vec<SocketAddr>> {
    let listeners = (0..count)
        .map(|_| TcpListener::bind("127.0.0.1:0"))
        .collect::<std::io::Result<Vec<_>>>()?;
    listeners
        .iter()
        .map(|listener| listener.local_addr().map_err(Into::into))
        .collect()
}

fn operator_client(pki: &Pki, nodes: &[(String, SocketAddr)]) -> Result<AttestorClient> {
    Ok(AttestorClient::new(
        nodes,
        &[pki.certificate_der("node-ca")?],
        &[pki.certificate_der("operator")?],
        &pki.key_der("operator")?,
        Duration::from_secs(120),
    )?)
}

struct KeySetServer {
    address: SocketAddr,
    shutdown: Arc<AtomicBool>,
    worker: Option<JoinHandle<()>>,
}

impl KeySetServer {
    fn serve(document: String) -> Result<Self> {
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let address = listener.local_addr()?;
        let shutdown = Arc::new(AtomicBool::new(false));
        let worker = {
            let shutdown = Arc::clone(&shutdown);
            thread::spawn(move || {
                for stream in listener.incoming() {
                    if shutdown.load(Ordering::Acquire) {
                        break;
                    }
                    if let Ok(mut stream) = stream {
                        let _ = respond(&mut stream, &document);
                    }
                }
            })
        };
        Ok(Self {
            address,
            shutdown,
            worker: Some(worker),
        })
    }

    fn url(&self) -> String {
        format!("http://{}/jwks.json", self.address)
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

fn respond(stream: &mut TcpStream, document: &str) -> Result<()> {
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;
    let mut request = Vec::new();
    let mut buffer = [0_u8; 1024];
    while !request.windows(4).any(|window| window == b"\r\n\r\n") {
        let read = stream.read(&mut buffer)?;
        if read == 0 || request.len() > 16 * 1024 {
            return Ok(());
        }
        request.extend_from_slice(&buffer[..read]);
    }
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
            let mut scalar = [0_u8; 32];
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

    fn mint(&self, subject: &str) -> Result<String> {
        let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
        let header = json!({ "alg": "ES256", "typ": "JWT", "kid": self.key_id });
        let claims = json!({
            "iss": ISSUER,
            "sub": subject,
            "aud": AUDIENCE,
            "exp": now + 3600,
            "iat": now,
            "nbf": now - 5,
        });
        let input = format!(
            "{}.{}",
            URL_SAFE_NO_PAD.encode(header.to_string()),
            URL_SAFE_NO_PAD.encode(claims.to_string())
        );
        let signature: Signature = self.key.sign(input.as_bytes());
        Ok(format!(
            "{input}.{}",
            URL_SAFE_NO_PAD.encode(signature.to_bytes())
        ))
    }
}

struct Cluster {
    root: PathBuf,
    pki: Pki,
    nodes: Vec<(String, SocketAddr)>,
    children: Vec<(String, Child)>,
    tokens: TokenSigner,
    keys: KeySetServer,
}

impl Drop for Cluster {
    fn drop(&mut self) {
        for (_, child) in &mut self.children {
            let _ = child.kill();
            let _ = child.wait();
        }
        let _ = fs::remove_dir_all(&self.root);
    }
}

impl Cluster {
    fn start() -> Result<Self> {
        let root = scratch("attestor-cluster")?;
        let daemon = build_daemon(&root)?;
        let mut pki = Pki::new(&root);
        pki.authority("node-ca")?;
        pki.authority("operator-ca")?;
        pki.leaf("operator-ca", "operator", false)?;
        let ids: Vec<String> = (1..=NODES).map(|index| format!("node-{index}")).collect();
        let mut pins = Vec::new();
        for id in &ids {
            pki.leaf("node-ca", id, true)?;
            pins.push(pki.pin(id)?);
        }
        let tokens = TokenSigner::generate("attestor-test-key")?;
        let keys = KeySetServer::serve(json!({ "keys": [tokens.jwk()?] }).to_string())?;
        fs::write(
            root.join("policy.json"),
            serde_json::to_vec(&json!({
                "version": 1,
                "defaults": {
                    "chain_id": 125,
                    "kinds": ["lx_activity"],
                    "caps": {"native": {"per_transaction": "1000000000000000000", "daily": "10000000000000000000"}},
                    "rate_per_minute": 1000
                }
            }))?,
        )?;
        let activity_types: Vec<String> = registry()?
            .registrations()
            .iter()
            .flat_map(|module| {
                module
                    .activity_types()
                    .iter()
                    .map(|kind| kind.value().to_string())
            })
            .collect();
        let addresses = free_addresses(NODES * 2)?;
        let nodes: Vec<(String, SocketAddr)> = ids
            .iter()
            .enumerate()
            .map(|(index, id)| (id.clone(), addresses[index]))
            .collect();
        let peers: Vec<SocketAddr> = addresses[NODES..].to_vec();
        let mut cluster = Self {
            root: root.clone(),
            pki,
            nodes,
            children: Vec::new(),
            tokens,
            keys,
        };
        for (index, id) in ids.iter().enumerate() {
            let others: Vec<usize> = (0..NODES).filter(|other| *other != index).collect();
            let peer_list = others
                .iter()
                .map(|other| format!("{}={}", ids[*other], peers[*other]))
                .collect::<Vec<_>>()
                .join(",");
            let pin_list = others
                .iter()
                .map(|other| format!("{}={}", ids[*other], pins[*other]))
                .collect::<Vec<_>>()
                .join(",");
            let mut node_key = [0_u8; 32];
            getrandom::fill(&mut node_key)?;
            let key_file = root.join(format!("{id}.node-key"));
            fs::write(&key_file, hex(&node_key))?;
            fs::set_permissions(&key_file, fs::Permissions::from_mode(0o600))?;
            let log = fs::File::create(root.join(format!("{id}.log")))?;
            let child = Command::new(&daemon)
                .env_clear()
                .env("ATTESTOR_NODE_ID", id)
                .env("ATTESTOR_REGION", "test")
                .env("ATTESTOR_LISTEN_ADDR", cluster.nodes[index].1.to_string())
                .env("ATTESTOR_PEER_LISTEN_ADDR", peers[index].to_string())
                .env("ATTESTOR_PEERS", peer_list)
                .env("ATTESTOR_PEER_PINS", pin_list)
                .env("ATTESTOR_NODE_KEY_FILE", &key_file)
                .env("ATTESTOR_DATA_DIR", root.join(format!("{id}.data")))
                .env("ATTESTOR_CHAIN_ID", CHAIN_ID)
                .env("ATTESTOR_JWKS_URL", cluster.keys.url())
                .env("ATTESTOR_JWT_ISSUER", ISSUER)
                .env("ATTESTOR_JWT_AUDIENCE", AUDIENCE)
                .env("ATTESTOR_POLICY_FILE", root.join("policy.json"))
                .env(
                    "ATTESTOR_TLS_CERT_FILE",
                    cluster.pki.path(&format!("{id}.pem")),
                )
                .env(
                    "ATTESTOR_TLS_KEY_FILE",
                    cluster.pki.path(&format!("{id}.key")),
                )
                .env("ATTESTOR_TLS_CA_FILE", cluster.pki.path("node-ca.pem"))
                .env(
                    "ATTESTOR_OPERATOR_CA_FILE",
                    cluster.pki.path("operator-ca.pem"),
                )
                .env("ATTESTOR_ACTIVITY_TYPES", activity_types.join(","))
                .stdin(Stdio::null())
                .stdout(log.try_clone()?)
                .stderr(log)
                .spawn()?;
            cluster.children.push((id.clone(), child));
        }
        cluster.wait_ready()?;
        Ok(cluster)
    }

    fn client(&self) -> Result<AttestorClient> {
        operator_client(&self.pki, &self.nodes)
    }

    fn wait_ready(&mut self) -> Result<()> {
        let client = self.client()?;
        let started = Instant::now();
        let mut pending: Vec<String> = self.nodes.iter().map(|(id, _)| id.clone()).collect();
        while !pending.is_empty() {
            let mut exited = None;
            for (id, child) in &mut self.children {
                if let Some(status) = child.try_wait()? {
                    exited = Some(format!("{id} exited with {status}"));
                    break;
                }
            }
            if let Some(exit) = exited {
                return Err(format!("{exit}: {}", self.logs()).into());
            }
            pending.retain(|id| {
                !matches!(client.health(id), Ok(health) if health.ready && health.reachable_peers == 4)
            });
            if started.elapsed() > Duration::from_secs(120) {
                return Err(
                    format!("attestors {pending:?} never became ready: {}", self.logs()).into(),
                );
            }
            thread::sleep(Duration::from_millis(200));
        }
        Ok(())
    }

    fn logs(&self) -> String {
        self.nodes
            .iter()
            .map(|(id, _)| {
                format!(
                    "\n{id}: {}",
                    fs::read_to_string(self.root.join(format!("{id}.log"))).unwrap_or_default()
                )
            })
            .collect()
    }

    fn audit(&self, client: &AttestorClient) -> Result<Vec<u64>> {
        SIGNERS
            .iter()
            .map(|id| Ok(checked(client.health(id))?.audit_sequence))
            .collect()
    }
}

fn build_daemon(root: &Path) -> Result<PathBuf> {
    let source = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../wallet/attestor");
    let binary = root.join("attestor");
    let output = Command::new("go")
        .current_dir(&source)
        .arg("build")
        .arg("-o")
        .arg(&binary)
        .arg("./cmd/attestor")
        .stdin(Stdio::null())
        .output()?;
    if !output.status.success() {
        return Err(format!(
            "attestor build failed: {}",
            String::from_utf8_lossy(&output.stderr)
        )
        .into());
    }
    Ok(binary)
}

fn registry() -> Result<layerx_types::payload::ModuleRegistry> {
    use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
    checked(ModuleRegistry::new(&[
        checked(ModuleRegistration::new(
            ModuleId::Asset,
            &[
                checked(ActivityType::new(ModuleId::Asset, 1))?,
                checked(ActivityType::new(ModuleId::Asset, 4))?,
                checked(ActivityType::new(ModuleId::Asset, 5))?,
            ],
        ))?,
        checked(ModuleRegistration::new(
            ModuleId::Budget,
            &[checked(ActivityType::new(ModuleId::Budget, 1))?],
        ))?,
        checked(ModuleRegistration::new(
            ModuleId::Governance,
            &[
                checked(ActivityType::new(ModuleId::Governance, 1))?,
                checked(ActivityType::new(ModuleId::Governance, 2))?,
                checked(ActivityType::new(ModuleId::Governance, 3))?,
                checked(ActivityType::new(ModuleId::Governance, 5))?,
                checked(ActivityType::new(ModuleId::Governance, 8))?,
            ],
        ))?,
    ]))
}

fn registration() -> Result<layerx_types::payload::Payload> {
    use layerx_crypto::payments::{asset_id, Payment, Registration};
    use layerx_types::ids::Did;
    use layerx_types::payload::{ActivityType, ModuleId, Payload};
    let actor = checked(Did::new(b"did:layerx:alice"))?;
    let issuer = checked(layerx_intents::canonical::did_id_for_protocol(&actor, 3))?;
    let registration = Payment::Register(Registration {
        asset: asset_id(&issuer, &[21; 32]),
        salt: [21; 32],
        symbol: "KMS".into(),
        name: "KMS asset".into(),
        decimals: 6,
        supply_cap: 1000,
        issuer_kind: 1,
        custody_ref: Vec::new(),
    });
    checked(Payload::new(
        &registry()?,
        checked(ActivityType::new(ModuleId::Asset, 1))?,
        &checked(registration.encode(actor.as_bytes()))?,
    ))
}

fn activity(public: [u8; 32], fee: u128) -> Result<(Vec<u8>, Disclosure)> {
    use layerx_types::activity::{Authority, EnvelopeBuilder, TimestampBound};
    use layerx_types::amount::Amount;
    use layerx_types::ids::{Did, IdempotencyKey};
    let payload = registration()?;
    let mut builder = EnvelopeBuilder::new();
    checked(builder.protocol_version(3))?;
    checked(builder.network_id(NETWORK))?;
    checked(builder.activity_type(payload.activity_type()))?;
    checked(builder.actor_did(checked(Did::new(b"did:layerx:alice"))?))?;
    checked(builder.authority(checked(Authority::owner(&public))?))?;
    checked(builder.account_sequence(7))?;
    checked(builder.timestamp_bound(checked(TimestampBound::new(1000, 1010))?))?;
    checked(builder.idempotency_key(IdempotencyKey::new([4; 32])))?;
    checked(builder.fee_limit(Amount::from_u128(fee)))?;
    checked(
        builder.payload_hash(checked(layerx_intents::canonical::payload_hash_for(
            &payload,
        ))?),
    )?;
    checked(builder.payload(payload))?;
    let canonical = checked(layerx_intents::canonical::unsigned_envelope_bytes(
        &checked(builder.build())?,
    ))?;
    let disclosure = checked(bind(&canonical, &registry()?))?;
    Ok((canonical, disclosure))
}

fn expected_digest(canonical: &[u8]) -> Result<[u8; 32]> {
    Ok(checked(layerx_crypto::SignatureMessage::new(
        Domain::SignaturePreimage,
        3,
        NETWORK,
        canonical,
    ))?
    .digest())
}

fn assert_verified(
    signer: &AttestorSigner,
    canonical: &[u8],
    signature: &layerx_human_kms::attestor::AttestorSignature,
) -> Result<()> {
    let message = checked(layerx_crypto::SignatureMessage::new(
        Domain::SignaturePreimage,
        3,
        NETWORK,
        canonical,
    ))?;
    assert_eq!(signature.digest(), &expected_digest(canonical)?);
    checked(layerx_crypto::ed25519::verify(
        &signer.public_key(),
        signature.signature(),
        message,
    ))?;
    checked(layerx_crypto::ed25519::verify_digest(
        &signer.public_key(),
        signature.signature(),
        signature.digest(),
    ))?;
    let mut tampered = *signature.signature();
    tampered[0] ^= 1;
    assert!(layerx_crypto::ed25519::verify(&signer.public_key(), &tampered, message).is_err());
    Ok(())
}

#[test]
fn attestor_signs_a_disclosed_activity_through_the_real_daemon_quorum() -> Result<()> {
    let cluster = Cluster::start()?;
    let registry = registry()?;
    let generated = checked(
        cluster
            .client()?
            .generate_ed25519("lx-user-key", OWNER, ACCOUNT),
    )?;
    assert_eq!(generated.audit.len(), NODES);
    assert!(generated.audit.values().all(|sequence| *sequence > 0));
    let signer = checked(AttestorSigner::new(
        cluster.client()?,
        "lx-user-key",
        generated.public_key,
        &SIGNERS,
        NETWORK,
    ))?;
    let observer = cluster.client()?;
    let assertion = cluster.tokens.mint(OWNER)?;

    let (canonical, disclosure) = activity(generated.public_key, 1)?;
    let first = checked(signer.sign_activity(&canonical, &disclosure, &registry, &assertion))?;
    assert_verified(&signer, &canonical, &first)?;
    assert_eq!(
        first.audit().keys().map(String::as_str).collect::<Vec<_>>(),
        SIGNERS
    );
    assert_eq!(&signer.audit_sequences(), first.audit());
    for (node, sequence) in first.audit() {
        assert!(*sequence > generated.audit[node]);
    }

    let before = cluster.audit(&observer)?;
    let (other_canonical, other_disclosure) = activity(generated.public_key, 2)?;
    assert_eq!(
        signer.sign_activity(&canonical, &other_disclosure, &registry, &assertion),
        Err(AttestorError::Disclosure(SignError::DisclosureMismatch(
            "fee_limit"
        )))
    );
    let mut altered = disclosure.clone();
    altered.amounts[0].value = altered.amounts[0]
        .value
        .checked_add(1)
        .ok_or("amount overflow")?;
    assert_eq!(
        signer.sign_activity(&canonical, &altered, &registry, &assertion),
        Err(AttestorError::Disclosure(SignError::DisclosureMismatch(
            "amounts"
        )))
    );
    assert_eq!(cluster.audit(&observer)?, before);
    assert_eq!(&signer.audit_sequences(), first.audit());

    let refused = signer.sign_activity(
        &other_canonical,
        &other_disclosure,
        &registry,
        &cluster.tokens.mint(STRANGER)?,
    );
    assert!(
        matches!(
            &refused,
            Err(AttestorError::Refused { status: 401, code, .. }) if code == "token_not_owner"
        ),
        "{refused:?}"
    );
    assert_eq!(&signer.audit_sequences(), first.audit());

    let second =
        checked(signer.sign_activity(&other_canonical, &other_disclosure, &registry, &assertion))?;
    assert_verified(&signer, &other_canonical, &second)?;
    for (node, sequence) in second.audit() {
        assert!(*sequence > first.audit()[node]);
    }
    assert_eq!(&signer.audit_sequences(), second.audit());
    Ok(())
}

#[test]
fn attestor_refuses_a_mismatched_disclosure_before_contacting_any_node() -> Result<()> {
    let root = scratch("attestor-offline")?;
    let outcome = (|| -> Result<()> {
        let mut pki = Pki::new(&root);
        pki.authority("node-ca")?;
        pki.authority("operator-ca")?;
        pki.leaf("operator-ca", "operator", false)?;
        let nodes: Vec<(String, SocketAddr)> = free_addresses(NODES)?
            .into_iter()
            .enumerate()
            .map(|(index, address)| (format!("node-{}", index + 1), address))
            .collect();
        let registry = registry()?;
        let public = [9; 32];
        let signer = checked(AttestorSigner::new(
            operator_client(&pki, &nodes)?,
            "lx-user-key",
            public,
            &SIGNERS,
            NETWORK,
        ))?;
        let (canonical, disclosure) = activity(public, 1)?;
        let (_, other) = activity(public, 2)?;
        assert_eq!(
            signer.sign_activity(&canonical, &other, &registry, "assertion"),
            Err(AttestorError::Disclosure(SignError::DisclosureMismatch(
                "fee_limit"
            )))
        );
        let mut altered = disclosure.clone();
        altered.actor = b"did:layerx:mallory".to_vec();
        assert_eq!(
            signer.sign_activity(&canonical, &altered, &registry, "assertion"),
            Err(AttestorError::Disclosure(SignError::DisclosureMismatch(
                "actor"
            )))
        );
        assert!(matches!(
            signer.sign_activity(&canonical, &disclosure, &registry, "assertion"),
            Err(AttestorError::Unavailable { .. })
        ));
        assert!(signer.audit_sequences().is_empty());
        let foreign = checked(AttestorSigner::new(
            operator_client(&pki, &nodes)?,
            "lx-user-key",
            public,
            &SIGNERS,
            NETWORK + 1,
        ))?;
        assert_eq!(
            foreign.sign_activity(&canonical, &disclosure, &registry, "assertion"),
            Err(AttestorError::WrongNetwork {
                expected: NETWORK + 1,
                actual: NETWORK
            })
        );
        assert!(AttestorSigner::new(
            operator_client(&pki, &nodes)?,
            "lx-user-key",
            public,
            &["node-1", "node-2"],
            NETWORK,
        )
        .is_err());
        Ok(())
    })();
    let _ = fs::remove_dir_all(&root);
    outcome
}
