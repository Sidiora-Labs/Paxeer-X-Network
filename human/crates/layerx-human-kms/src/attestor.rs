use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::net::SocketAddr;
use std::sync::{Mutex, MutexGuard};
use std::time::Duration;

use layerx_crypto::disclosure::{bind, Disclosure};
use layerx_crypto::signer::SignError;
use layerx_types::payload::ModuleRegistry;
use rustls::pki_types::CertificateDer;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use zeroize::Zeroizing;

pub const KIND_ACTIVITY: &str = "lx_activity";
pub const MIN_SIGNERS: usize = 3;
const PATH_SIGN: &str = "/v1/sign";
const PATH_GENERATE: &str = "/v1/keys/generate";
const PATH_HEALTH: &str = "/health";
const PREIMAGE_DOMAIN: &[u8] = b"LXP/v1/signature-preimage\0";
const MAX_RESPONSE_BYTES: u64 = 65_536;
const MAX_IDENTIFIER: usize = 128;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AttestorError {
    Configuration(&'static str),
    Disclosure(SignError),
    WrongNetwork {
        expected: u32,
        actual: u32,
    },
    Refused {
        node: String,
        status: u16,
        category: String,
        code: String,
        policy_code: Option<String>,
    },
    Timeout {
        node: String,
    },
    Unavailable {
        node: String,
    },
    Authentication {
        node: String,
    },
    MalformedResponse {
        node: String,
    },
    SignatureInvalid {
        node: String,
    },
}

impl fmt::Display for AttestorError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Configuration(what) => {
                write!(formatter, "attestor configuration refused: {what}")
            }
            Self::Disclosure(error) => write!(formatter, "attestor signing refused: {error}"),
            Self::WrongNetwork { expected, actual } => write!(
                formatter,
                "activity names network {actual} but the signer serves network {expected}"
            ),
            Self::Refused {
                node,
                status,
                category,
                code,
                policy_code,
            } => {
                write!(
                    formatter,
                    "attestor {node} refused with {status} {category}/{code}"
                )?;
                if let Some(policy) = policy_code {
                    write!(formatter, " policy {policy}")?;
                }
                Ok(())
            }
            Self::Timeout { node } => write!(formatter, "attestor {node} timed out"),
            Self::Unavailable { node } => write!(formatter, "attestor {node} is unavailable"),
            Self::Authentication { node } => {
                write!(
                    formatter,
                    "mutual authentication with attestor {node} failed"
                )
            }
            Self::MalformedResponse { node } => {
                write!(formatter, "attestor {node} returned a malformed response")
            }
            Self::SignatureInvalid { node } => {
                write!(formatter, "attestor {node} returned an invalid signature")
            }
        }
    }
}

impl std::error::Error for AttestorError {}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AttestorHealth {
    pub node_id: String,
    pub ready: bool,
    pub audit_sequence: u64,
    pub reachable_peers: u32,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct GeneratedKey {
    pub key_id: String,
    pub public_key: [u8; 32],
    pub audit: BTreeMap<String, u64>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AttestorSignature {
    signature: [u8; 64],
    digest: [u8; 32],
    audit: BTreeMap<String, u64>,
}

impl AttestorSignature {
    #[must_use]
    pub const fn signature(&self) -> &[u8; 64] {
        &self.signature
    }

    #[must_use]
    pub const fn digest(&self) -> &[u8; 32] {
        &self.digest
    }

    #[must_use]
    pub const fn audit(&self) -> &BTreeMap<String, u64> {
        &self.audit
    }
}

pub struct AttestorClient {
    agent: ureq::Agent,
    nodes: BTreeMap<String, SocketAddr>,
}

impl fmt::Debug for AttestorClient {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("AttestorClient")
            .field("nodes", &self.nodes)
            .field("identity", &"[gateway client identity]")
            .finish()
    }
}

impl AttestorClient {
    /// Builds a mutual-TLS client that trusts only the given attestor roots and presents the
    /// gateway client identity the attestors accept for key generation and signing.
    ///
    /// # Errors
    /// Refuses an empty or duplicated node list, unparsable roots, a missing client identity
    /// and a zero deadline.
    pub fn new(
        nodes: &[(String, SocketAddr)],
        roots: &[Vec<u8>],
        certificate_chain: &[Vec<u8>],
        private_key_pkcs8: &[u8],
        deadline: Duration,
    ) -> Result<Self, AttestorError> {
        let mut table = BTreeMap::new();
        for (id, address) in nodes {
            if !valid_identifier(id) {
                return Err(AttestorError::Configuration("attestor node id"));
            }
            if table.insert(id.clone(), *address).is_some() {
                return Err(AttestorError::Configuration("duplicate attestor node id"));
            }
        }
        if table.len() < MIN_SIGNERS {
            return Err(AttestorError::Configuration("too few attestor nodes"));
        }
        if roots.is_empty() || certificate_chain.is_empty() || private_key_pkcs8.is_empty() {
            return Err(AttestorError::Configuration(
                "attestor trust or client identity",
            ));
        }
        if deadline.is_zero() {
            return Err(AttestorError::Configuration("attestor deadline"));
        }
        let mut store = rustls::RootCertStore::empty();
        for root in roots {
            store
                .add(CertificateDer::from(root.clone()))
                .map_err(|_| AttestorError::Configuration("attestor trust root"))?;
        }
        let key = pkcs8_pem(private_key_pkcs8);
        let private_key = ureq::tls::PrivateKey::from_pem(key.as_bytes())
            .map_err(|_| AttestorError::Configuration("gateway client key"))?;
        let chain: Vec<_> = certificate_chain
            .iter()
            .map(|certificate| ureq::tls::Certificate::from_der(certificate).to_owned())
            .collect();
        let trusted: Vec<_> = roots
            .iter()
            .map(|root| ureq::tls::Certificate::from_der(root).to_owned())
            .collect();
        let identity = ureq::tls::ClientCert::new_with_certs(&chain, private_key);
        let agent: ureq::Agent = ureq::Agent::config_builder()
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .provider(ureq::tls::TlsProvider::Rustls)
                    .root_certs(ureq::tls::RootCerts::new_with_certs(&trusted))
                    .client_cert(Some(identity))
                    .build(),
            )
            .timeout_global(Some(deadline))
            .http_status_as_error(false)
            .max_redirects(0)
            .build()
            .into();
        Ok(Self {
            agent,
            nodes: table,
        })
    }

    #[must_use]
    pub fn nodes(&self) -> Vec<String> {
        self.nodes.keys().cloned().collect()
    }

    /// Reads one attestor's health report, ready or not.
    ///
    /// # Errors
    /// Returns transport, authentication and malformed-response failures.
    pub fn health(&self, node: &str) -> Result<AttestorHealth, AttestorError> {
        let address = self.address(node)?;
        let response = self
            .agent
            .get(format!("https://{address}{PATH_HEALTH}"))
            .header("accept", "application/json")
            .call()
            .map_err(|error| transport_error(node, &error))?;
        let (status, body) = read_body(node, response)?;
        if status != 200 && status != 503 {
            return Err(refusal(node, status, &body));
        }
        let report: HealthBody = serde_json::from_slice(&body).map_err(|_| malformed(node))?;
        if report.node_id != node {
            return Err(malformed(node));
        }
        Ok(AttestorHealth {
            node_id: report.node_id,
            ready: report.ready,
            audit_sequence: report.audit_sequence,
            reachable_peers: report.reachable_peers,
        })
    }

    /// Runs a distributed Ed25519 key generation on every configured attestor and returns the
    /// group key they all agree on.
    ///
    /// # Errors
    /// Refuses when any attestor refuses, disagrees on the group key or answers malformed data.
    pub fn generate_ed25519(
        &self,
        key_id: &str,
        owner: &str,
        account: &str,
    ) -> Result<GeneratedKey, AttestorError> {
        if !valid_identifier(key_id) || owner.is_empty() {
            return Err(AttestorError::Configuration("key id or owner"));
        }
        let session = session_id("keygen")?;
        let body = serde_json::to_vec(&GenerateBody {
            session_id: &session,
            key_id,
            curve: "ed25519",
            owner,
            account,
        })
        .map_err(|_| AttestorError::Configuration("generate request"))?;
        let nodes = self.nodes();
        let mut public_key = None;
        let mut audit = BTreeMap::new();
        for (node, result) in self.post_all(&nodes, PATH_GENERATE, &body, None) {
            let raw = result?;
            let response: KeyBody = serde_json::from_slice(&raw).map_err(|_| malformed(&node))?;
            if response.node_id != node || response.key_id != key_id || response.curve != "ed25519"
            {
                return Err(malformed(&node));
            }
            let key = decode_fixed::<32>(&response.public_key).ok_or_else(|| malformed(&node))?;
            match public_key {
                None => public_key = Some(key),
                Some(agreed) if agreed == key => {}
                Some(_) => return Err(malformed(&node)),
            }
            audit.insert(node, response.audit_sequence);
        }
        let public_key = public_key.ok_or(AttestorError::Configuration("no attestor nodes"))?;
        Ok(GeneratedKey {
            key_id: key_id.to_owned(),
            public_key,
            audit,
        })
    }

    fn address(&self, node: &str) -> Result<SocketAddr, AttestorError> {
        self.nodes
            .get(node)
            .copied()
            .ok_or(AttestorError::Configuration("unknown attestor node"))
    }

    fn post(
        &self,
        node: &str,
        path: &str,
        body: &[u8],
        bearer: Option<&str>,
    ) -> Result<Vec<u8>, AttestorError> {
        let address = self.address(node)?;
        let mut request = self
            .agent
            .post(format!("https://{address}{path}"))
            .header("content-type", "application/json")
            .header("accept", "application/json");
        if let Some(token) = bearer {
            request = request.header("authorization", format!("Bearer {token}"));
        }
        let response = request
            .send(body)
            .map_err(|error| transport_error(node, &error))?;
        let (status, body) = read_body(node, response)?;
        if status == 200 {
            Ok(body)
        } else {
            Err(refusal(node, status, &body))
        }
    }

    fn post_all(
        &self,
        nodes: &[String],
        path: &str,
        body: &[u8],
        bearer: Option<&str>,
    ) -> Vec<(String, Result<Vec<u8>, AttestorError>)> {
        std::thread::scope(|scope| {
            let handles: Vec<_> = nodes
                .iter()
                .map(|node| {
                    (
                        node,
                        scope.spawn(move || self.post(node, path, body, bearer)),
                    )
                })
                .collect();
            handles
                .into_iter()
                .map(|(node, handle)| {
                    let result = handle
                        .join()
                        .unwrap_or_else(|_| Err(AttestorError::Unavailable { node: node.clone() }));
                    (node.clone(), result)
                })
                .collect()
        })
    }
}

pub struct AttestorSigner {
    client: AttestorClient,
    key_id: String,
    public_key: [u8; 32],
    signers: Vec<String>,
    network: u32,
    audit: Mutex<BTreeMap<String, u64>>,
}

impl fmt::Debug for AttestorSigner {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("AttestorSigner")
            .field("client", &self.client)
            .field("key_id", &self.key_id)
            .field("signers", &self.signers)
            .field("network", &self.network)
            .finish_non_exhaustive()
    }
}

impl AttestorSigner {
    /// Binds a client to one attestor-held Ed25519 key, its expected group key, the signing
    /// quorum and the network whose activities it signs.
    ///
    /// # Errors
    /// Refuses fewer than three distinct signers, signers outside the client's nodes, an
    /// invalid key id and network zero.
    pub fn new(
        client: AttestorClient,
        key_id: &str,
        public_key: [u8; 32],
        signers: &[&str],
        network: u32,
    ) -> Result<Self, AttestorError> {
        if !valid_identifier(key_id) {
            return Err(AttestorError::Configuration("key id"));
        }
        if network == 0 {
            return Err(AttestorError::Configuration("network"));
        }
        let distinct: BTreeSet<&str> = signers.iter().copied().collect();
        if distinct.len() != signers.len() || distinct.len() < MIN_SIGNERS {
            return Err(AttestorError::Configuration("signing quorum"));
        }
        if distinct
            .iter()
            .any(|node| !client.nodes.contains_key(*node))
        {
            return Err(AttestorError::Configuration(
                "signer outside the attestor nodes",
            ));
        }
        Ok(Self {
            client,
            key_id: key_id.to_owned(),
            public_key,
            signers: distinct.into_iter().map(str::to_owned).collect(),
            network,
            audit: Mutex::new(BTreeMap::new()),
        })
    }

    #[must_use]
    pub const fn public_key(&self) -> [u8; 32] {
        self.public_key
    }

    #[must_use]
    pub fn key_id(&self) -> &str {
        &self.key_id
    }

    #[must_use]
    pub fn client(&self) -> &AttestorClient {
        &self.client
    }

    #[must_use]
    pub fn audit_sequences(&self) -> BTreeMap<String, u64> {
        self.recorded().clone()
    }

    /// Signs one disclosed activity preimage through the attestor quorum with the user's
    /// assertion, after proving the disclosure describes exactly these canonical bytes.
    ///
    /// # Errors
    /// Refuses a mismatched disclosure or foreign network before any request is sent, any
    /// quorum refusal, transport and authentication failures, a response for other bytes, a
    /// signature that fails the crate's verifier and an audit sequence that does not advance.
    pub fn sign_activity(
        &self,
        canonical: &[u8],
        disclosure: &Disclosure,
        registry: &ModuleRegistry,
        assertion: &str,
    ) -> Result<AttestorSignature, AttestorError> {
        validate_disclosure(canonical, disclosure, registry)?;
        let activity = layerx_intents::canonical::decode_unsigned_activity(canonical, registry)
            .map_err(|_| AttestorError::Disclosure(SignError::InvalidDisclosure))?;
        if activity.network_id() != self.network {
            return Err(AttestorError::WrongNetwork {
                expected: self.network,
                actual: activity.network_id(),
            });
        }
        if assertion.is_empty() {
            return Err(AttestorError::Configuration("user assertion"));
        }
        let digest = preimage(canonical);
        let session = session_id("activity")?;
        let activity_hex = encode_hex(canonical);
        let body = Zeroizing::new(
            serde_json::to_vec(&SignBody {
                session_id: &session,
                key_id: &self.key_id,
                kind: KIND_ACTIVITY,
                signers: &self.signers,
                activity: &activity_hex,
            })
            .map_err(|_| AttestorError::Configuration("sign request"))?,
        );
        let results = self
            .client
            .post_all(&self.signers, PATH_SIGN, &body, Some(assertion));
        let mut signature = None;
        let mut audit = BTreeMap::new();
        for (node, result) in results {
            let raw = result?;
            let response: SignResponseBody =
                serde_json::from_slice(&raw).map_err(|_| malformed(&node))?;
            if response.node_id != node
                || response.key_id != self.key_id
                || response.kind != KIND_ACTIVITY
                || response.recovery_id.is_some()
            {
                return Err(malformed(&node));
            }
            let signed =
                decode_fixed::<32>(&response.signed_bytes).ok_or_else(|| malformed(&node))?;
            if !layerx_crypto::ct::eq_fixed(&signed, &digest) {
                return Err(malformed(&node));
            }
            let returned =
                decode_fixed::<64>(&response.signature).ok_or_else(|| malformed(&node))?;
            layerx_crypto::ed25519::verify_digest(&self.public_key, &returned, &digest)
                .map_err(|_| AttestorError::SignatureInvalid { node: node.clone() })?;
            signature.get_or_insert(returned);
            audit.insert(node, response.audit_sequence);
        }
        let signature = signature.ok_or(AttestorError::Configuration("signing quorum"))?;
        let mut recorded = self.recorded();
        for (node, sequence) in &audit {
            if recorded
                .get(node)
                .is_some_and(|previous| sequence <= previous)
                || *sequence == 0
            {
                return Err(malformed(node));
            }
        }
        recorded.extend(
            audit
                .iter()
                .map(|(node, sequence)| (node.clone(), *sequence)),
        );
        Ok(AttestorSignature {
            signature,
            digest,
            audit,
        })
    }

    fn recorded(&self) -> MutexGuard<'_, BTreeMap<String, u64>> {
        match self.audit.lock() {
            Ok(guard) => guard,
            Err(poisoned) => poisoned.into_inner(),
        }
    }
}

fn validate_disclosure(
    canonical: &[u8],
    disclosure: &Disclosure,
    registry: &ModuleRegistry,
) -> Result<(), AttestorError> {
    let refuse =
        |field: &'static str| AttestorError::Disclosure(SignError::DisclosureMismatch(field));
    let actual = bind(canonical, registry)
        .map_err(|error| AttestorError::Disclosure(SignError::from(error)))?;
    macro_rules! require_field {
        ($($field:ident),+) => {
            $(if disclosure.$field != actual.$field {
                return Err(refuse(stringify!($field)));
            })+
        };
    }
    require_field!(
        activity_type,
        actor,
        authority,
        counterparties,
        amounts,
        asset,
        fee_limit,
        expiry,
        idempotency_key,
        evm_payout_binding,
        withdrawal,
        payment,
        authority_grant,
        session_grant,
        onboarding,
        native_operation
    );
    let reencoded = disclosure
        .reencode()
        .map_err(|error| AttestorError::Disclosure(SignError::from(error)))?;
    if !layerx_crypto::ct::eq(&reencoded, canonical) || *disclosure != actual {
        return Err(refuse("canonical_bytes"));
    }
    Ok(())
}

/// Computes the domain-separated activity preimage the attestors sign.
#[must_use]
pub fn preimage(canonical: &[u8]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(PREIMAGE_DOMAIN);
    hash.update(canonical);
    hash.finalize().into()
}

#[derive(Serialize)]
struct GenerateBody<'a> {
    session_id: &'a str,
    key_id: &'a str,
    curve: &'a str,
    owner: &'a str,
    account: &'a str,
}

#[derive(Serialize)]
struct SignBody<'a> {
    session_id: &'a str,
    key_id: &'a str,
    kind: &'a str,
    signers: &'a [String],
    activity: &'a str,
}

#[derive(Deserialize)]
struct KeyBody {
    node_id: String,
    key_id: String,
    curve: String,
    public_key: String,
    audit_sequence: u64,
}

#[derive(Deserialize)]
struct SignResponseBody {
    node_id: String,
    key_id: String,
    kind: String,
    signed_bytes: String,
    signature: String,
    recovery_id: Option<u8>,
    audit_sequence: u64,
}

#[derive(Deserialize)]
struct HealthBody {
    node_id: String,
    ready: bool,
    audit_sequence: u64,
    reachable_peers: u32,
}

#[derive(Deserialize)]
struct ErrorEnvelope {
    error: ErrorBody,
}

#[derive(Deserialize)]
struct ErrorBody {
    category: String,
    code: String,
    policy_code: Option<String>,
}

fn read_body(
    node: &str,
    mut response: ureq::http::Response<ureq::Body>,
) -> Result<(u16, Vec<u8>), AttestorError> {
    let status = response.status().as_u16();
    let body = response
        .body_mut()
        .with_config()
        .limit(MAX_RESPONSE_BYTES)
        .read_to_vec()
        .map_err(|error| transport_error(node, &error))?;
    Ok((status, body))
}

fn refusal(node: &str, status: u16, body: &[u8]) -> AttestorError {
    match serde_json::from_slice::<ErrorEnvelope>(body) {
        Ok(envelope) if !envelope.error.code.is_empty() => AttestorError::Refused {
            node: node.to_owned(),
            status,
            category: envelope.error.category,
            code: envelope.error.code,
            policy_code: envelope.error.policy_code.filter(|code| !code.is_empty()),
        },
        _ => malformed(node),
    }
}

fn transport_error(node: &str, error: &ureq::Error) -> AttestorError {
    let node = node.to_owned();
    match error {
        ureq::Error::Timeout(_) => AttestorError::Timeout { node },
        ureq::Error::Tls(_) | ureq::Error::Pem(_) | ureq::Error::Rustls(_) => {
            AttestorError::Authentication { node }
        }
        ureq::Error::Io(io)
            if io
                .get_ref()
                .is_some_and(|inner| inner.is::<rustls::Error>()) =>
        {
            AttestorError::Authentication { node }
        }
        ureq::Error::BodyExceedsLimit(_) | ureq::Error::Protocol(_) => {
            AttestorError::MalformedResponse { node }
        }
        _ => AttestorError::Unavailable { node },
    }
}

fn malformed(node: &str) -> AttestorError {
    AttestorError::MalformedResponse {
        node: node.to_owned(),
    }
}

fn pkcs8_pem(der: &[u8]) -> Zeroizing<String> {
    use base64::Engine as _;
    let encoded = Zeroizing::new(base64::engine::general_purpose::STANDARD.encode(der));
    let mut pem = Zeroizing::new(String::from("-----BEGIN PRIVATE KEY-----\n"));
    for line in encoded.as_bytes().chunks(64) {
        pem.push_str(&String::from_utf8_lossy(line));
        pem.push('\n');
    }
    pem.push_str("-----END PRIVATE KEY-----\n");
    pem
}

fn valid_identifier(value: &str) -> bool {
    !value.is_empty() && value.len() <= MAX_IDENTIFIER && !value.contains('/')
}

fn session_id(label: &str) -> Result<String, AttestorError> {
    let mut nonce = [0_u8; 16];
    getrandom::fill(&mut nonce).map_err(|_| AttestorError::Configuration("session randomness"))?;
    Ok(format!("{label}-{}", encode_hex(&nonce)))
}

fn encode_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(char::from(DIGITS[usize::from(byte >> 4)]));
        out.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    out
}

fn decode_fixed<const N: usize>(text: &str) -> Option<[u8; N]> {
    let raw = text.as_bytes();
    if raw.len() != N * 2 {
        return None;
    }
    let mut out = [0_u8; N];
    for (slot, pair) in out.iter_mut().zip(raw.chunks_exact(2)) {
        *slot = (nibble(pair[0])? << 4) | nibble(pair[1])?;
    }
    Some(out)
}

const fn nibble(digit: u8) -> Option<u8> {
    match digit {
        b'0'..=b'9' => Some(digit - b'0'),
        b'a'..=b'f' => Some(digit - b'a' + 10),
        b'A'..=b'F' => Some(digit - b'A' + 10),
        _ => None,
    }
}
