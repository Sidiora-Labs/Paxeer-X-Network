use std::collections::HashMap;
use std::io;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use jsonwebtoken::jwk::{AlgorithmParameters, EllipticCurve, Jwk, KeyAlgorithm, PublicKeyUse};
use jsonwebtoken::{Algorithm, DecodingKey, Validation};
use serde::Deserialize;

use crate::state::text;
use crate::{invalid, State};

const MAX_TOKEN_BYTES: usize = 16 * 1024;
const MAX_KEY_SET_BYTES: u64 = 256 * 1024;
const MAX_KEYS: usize = 64;
const MAX_CLOCK_SKEW_SECONDS: u64 = 300;
const MAX_KEY_SET_LIFETIME_SECONDS: u64 = 3_600;
const DEFAULT_CLOCK_SKEW_SECONDS: u64 = 60;
const DEFAULT_REFRESH_INTERVAL_SECONDS: u64 = 300;
pub(crate) const MAX_ISSUER_BYTES: usize = 1024;
pub(crate) const MAX_SUBJECT_BYTES: usize = 255;

const JWKS_URL: &str = "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_JWKS_URL";
const ISSUER: &str = "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_ISSUER";
const AUDIENCE: &str = "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_AUDIENCE";
const CLOCK_SKEW: &str = "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_CLOCK_SKEW_SECONDS";
const REFRESH_INTERVAL: &str = "LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_REFRESH_INTERVAL_SECONDS";

/// The optional assertion login section; absent unless its variables are set.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AssertionConfig {
    pub jwks_url: String,
    pub issuer: String,
    pub audience: String,
    pub clock_skew_seconds: u64,
    /// The freshness lifetime of a successfully fetched key set and the minimum
    /// spacing between key set fetch attempts, at most one hour.
    pub refresh_interval_seconds: u64,
}

impl AssertionConfig {
    /// Reads the assertion section from the process environment.
    ///
    /// # Errors
    /// Refuses a partial section, non-Unicode values and invalid bounds.
    pub fn from_environment() -> io::Result<Option<Self>> {
        Self::from_lookup(|name| match std::env::var(name) {
            Ok(value) => Ok(Some(value)),
            Err(std::env::VarError::NotPresent) => Ok(None),
            Err(std::env::VarError::NotUnicode(_)) => {
                Err(invalid("invalid assertion configuration"))
            }
        })
    }

    /// Reads the assertion section through a variable lookup.
    ///
    /// # Errors
    /// Refuses a partial section, lookup failures and invalid bounds.
    pub fn from_lookup(
        lookup: impl Fn(&str) -> io::Result<Option<String>>,
    ) -> io::Result<Option<Self>> {
        let jwks_url = lookup(JWKS_URL)?;
        let issuer = lookup(ISSUER)?;
        let audience = lookup(AUDIENCE)?;
        let clock_skew = lookup(CLOCK_SKEW)?;
        let refresh_interval = lookup(REFRESH_INTERVAL)?;
        if jwks_url.is_none()
            && issuer.is_none()
            && audience.is_none()
            && clock_skew.is_none()
            && refresh_interval.is_none()
        {
            return Ok(None);
        }
        let missing = || invalid("incomplete assertion configuration");
        let config = Self {
            jwks_url: jwks_url.ok_or_else(missing)?,
            issuer: issuer.ok_or_else(missing)?,
            audience: audience.ok_or_else(missing)?,
            clock_skew_seconds: seconds(clock_skew, DEFAULT_CLOCK_SKEW_SECONDS)?,
            refresh_interval_seconds: seconds(refresh_interval, DEFAULT_REFRESH_INTERVAL_SECONDS)?,
        };
        config.validate()?;
        Ok(Some(config))
    }

    /// Checks the key set location, the expected claims and the time bounds.
    ///
    /// # Errors
    /// Refuses plaintext non-loopback URLs, empty claims and out-of-range bounds.
    pub fn validate(&self) -> io::Result<()> {
        bounded(&self.issuer, MAX_ISSUER_BYTES)?;
        bounded(&self.audience, MAX_ISSUER_BYTES)?;
        validate_url(&self.jwks_url)?;
        if self.clock_skew_seconds > MAX_CLOCK_SKEW_SECONDS
            || self.refresh_interval_seconds == 0
            || self.refresh_interval_seconds > MAX_KEY_SET_LIFETIME_SECONDS
        {
            return Err(invalid("invalid assertion time bounds"));
        }
        Ok(())
    }
}

/// The application account an assertion's issuer and subject map to.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AssertionPrincipal {
    principal: String,
    issuer: String,
    subject: String,
    did: Option<String>,
    created_at: u64,
}

impl AssertionPrincipal {
    pub(crate) fn new(
        principal: String,
        issuer: String,
        subject: String,
        did: Option<String>,
        created_at: u64,
    ) -> Self {
        Self {
            principal,
            issuer,
            subject,
            did,
            created_at,
        }
    }

    #[must_use]
    pub fn principal(&self) -> &str {
        &self.principal
    }

    #[must_use]
    pub fn issuer(&self) -> &str {
        &self.issuer
    }

    #[must_use]
    pub fn subject(&self) -> &str {
        &self.subject
    }

    #[must_use]
    pub fn did(&self) -> Option<&str> {
        self.did.as_deref()
    }

    #[must_use]
    pub const fn created_at(&self) -> u64 {
        self.created_at
    }
}

/// A typed refusal of LXIP operation 4, carried on the wire as the response status.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AssertionRefusal {
    /// The token's signature, key or claims were refused.
    Assertion,
    /// The wallet DID is bound to another account or differs from the recorded DID.
    Identity,
    /// The assertion principal is disabled or its key set is unavailable.
    Unavailable,
}

impl AssertionRefusal {
    /// The response status carrying this refusal; status 1 stays the malformed-request refusal.
    #[must_use]
    pub const fn status(self) -> u8 {
        match self {
            Self::Assertion => 2,
            Self::Identity => 3,
            Self::Unavailable => 4,
        }
    }
}

impl std::fmt::Display for AssertionRefusal {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::Assertion => "assertion refused",
            Self::Identity => "wallet DID conflicts with a recorded identity",
            Self::Unavailable => "assertion principal unavailable",
        })
    }
}

impl std::error::Error for AssertionRefusal {}

impl From<AssertionRefusal> for io::Error {
    fn from(refusal: AssertionRefusal) -> Self {
        match refusal {
            AssertionRefusal::Unavailable => Self::other(refusal),
            AssertionRefusal::Assertion | AssertionRefusal::Identity => {
                Self::new(io::ErrorKind::InvalidData, refusal)
            }
        }
    }
}

/// The issuer and subject of a bearer token whose signature and claims verified.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedAssertion {
    issuer: String,
    subject: String,
}

impl VerifiedAssertion {
    #[must_use]
    pub fn issuer(&self) -> &str {
        &self.issuer
    }

    #[must_use]
    pub fn subject(&self) -> &str {
        &self.subject
    }
}

#[derive(Default)]
struct KeyCache {
    keys: HashMap<String, (Algorithm, DecodingKey)>,
    fetched: Option<Instant>,
    attempted: Option<Instant>,
}

impl KeyCache {
    fn expire(&mut self, lifetime: Duration) {
        if self
            .fetched
            .is_some_and(|fetched| fetched.elapsed() >= lifetime)
        {
            self.keys.clear();
            self.fetched = None;
        }
    }

    fn find(&self, key_id: &str, algorithm: Algorithm) -> Option<DecodingKey> {
        self.keys
            .get(key_id)
            .filter(|(expected, _)| *expected == algorithm)
            .map(|(_, key)| key.clone())
    }
}

/// Verifies RS256 and ES256 bearer tokens against a JWKS cached by key id.
///
/// A fetched key set is used only for the refresh interval after the fetch that
/// produced it; an expired set is dropped before any token is accepted, and fetches,
/// whether for an unknown key id or an expired set, start at most once per interval
/// and one at a time, so a failing key set endpoint refuses rather than extends keys.
pub struct AssertionVerifier {
    config: AssertionConfig,
    agent: ureq::Agent,
    cache: Mutex<KeyCache>,
}

impl AssertionVerifier {
    /// Builds a verifier; the key set is fetched on first use.
    ///
    /// # Errors
    /// Refuses invalid configuration and unavailable trust roots for HTTPS.
    pub fn new(config: AssertionConfig) -> io::Result<Self> {
        config.validate()?;
        let agent: ureq::Agent = ureq::Agent::config_builder()
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .provider(ureq::tls::TlsProvider::Rustls)
                    .root_certs(trust_roots(&config.jwks_url)?)
                    .build(),
            )
            .timeout_global(Some(Duration::from_secs(10)))
            .http_status_as_error(false)
            .max_redirects(0)
            .build()
            .into();
        Ok(Self {
            config,
            agent,
            cache: Mutex::new(KeyCache::default()),
        })
    }

    #[must_use]
    pub fn config(&self) -> &AssertionConfig {
        &self.config
    }

    /// Verifies the token's signature, issuer, audience, expiry, not-before and subject.
    ///
    /// # Errors
    /// Refuses unsupported algorithms, unknown keys, bad signatures and failed claims.
    pub fn verify(&self, token: &str, now: u64) -> io::Result<VerifiedAssertion> {
        if token.is_empty() || token.len() > MAX_TOKEN_BYTES {
            return Err(invalid("assertion refused"));
        }
        let header =
            jsonwebtoken::decode_header(token).map_err(|_| invalid("assertion refused"))?;
        if !matches!(header.alg, Algorithm::RS256 | Algorithm::ES256) {
            return Err(invalid("assertion algorithm refused"));
        }
        let key_id = header
            .kid
            .ok_or_else(|| invalid("assertion key id missing"))?;
        let key = self.key(&key_id, header.alg)?;
        let mut validation = Validation::new(header.alg);
        validation.validate_exp = false;
        validation.validate_nbf = false;
        validation.validate_aud = false;
        validation.required_spec_claims.clear();
        let claims = jsonwebtoken::decode::<Claims>(token, &key, &validation)
            .map_err(|_| invalid("assertion refused"))?
            .claims;
        let skew = self.config.clock_skew_seconds;
        if claims.iss != self.config.issuer {
            return Err(invalid("assertion issuer refused"));
        }
        if !claims.aud.contains(&self.config.audience) {
            return Err(invalid("assertion audience refused"));
        }
        if claims.exp.saturating_add(skew) <= now {
            return Err(invalid("assertion expired"));
        }
        if claims.nbf.is_some_and(|nbf| nbf > now.saturating_add(skew)) {
            return Err(invalid("assertion not yet valid"));
        }
        bounded(&claims.sub, MAX_SUBJECT_BYTES)?;
        Ok(VerifiedAssertion {
            issuer: claims.iss,
            subject: claims.sub,
        })
    }

    fn key(&self, key_id: &str, algorithm: Algorithm) -> io::Result<DecodingKey> {
        let mut cache = self
            .cache
            .lock()
            .map_err(|_| io::Error::other("assertion key cache poisoned"))?;
        let interval = Duration::from_secs(self.config.refresh_interval_seconds);
        cache.expire(interval);
        if let Some(key) = cache.find(key_id, algorithm) {
            return Ok(key);
        }
        if cache
            .attempted
            .is_some_and(|attempted| attempted.elapsed() < interval)
        {
            return Err(if cache.fetched.is_some() {
                invalid("unknown assertion key")
            } else {
                io::Error::other("assertion key set unavailable")
            });
        }
        let started = Instant::now();
        cache.attempted = Some(started);
        cache.keys = self.fetch()?;
        cache.fetched = Some(started);
        cache
            .find(key_id, algorithm)
            .ok_or_else(|| invalid("unknown assertion key"))
    }

    fn fetch(&self) -> io::Result<HashMap<String, (Algorithm, DecodingKey)>> {
        let mut response = self
            .agent
            .get(&self.config.jwks_url)
            .header("accept", "application/json")
            .call()
            .map_err(|_| io::Error::other("assertion key set unavailable"))?;
        if response.status().as_u16() != 200 {
            return Err(io::Error::other("assertion key set unavailable"));
        }
        let bytes = response
            .body_mut()
            .with_config()
            .limit(MAX_KEY_SET_BYTES)
            .read_to_vec()
            .map_err(|_| io::Error::other("assertion key set unreadable"))?;
        let document: KeySet = serde_json::from_slice(&bytes)
            .map_err(|_| io::Error::other("invalid assertion key set"))?;
        if document.keys.len() > MAX_KEYS {
            return Err(io::Error::other("assertion key set too large"));
        }
        let mut keys = HashMap::new();
        for value in document.keys {
            let Ok(jwk) = serde_json::from_value::<Jwk>(value) else {
                continue;
            };
            let Some((key_id, algorithm)) = usable(&jwk) else {
                continue;
            };
            let key = DecodingKey::from_jwk(&jwk)
                .map_err(|_| io::Error::other("invalid assertion key set"))?;
            if keys.insert(key_id, (algorithm, key)).is_some() {
                return Err(io::Error::other("duplicate assertion key id"));
            }
        }
        if keys.is_empty() {
            return Err(io::Error::other("assertion key set has no usable key"));
        }
        Ok(keys)
    }
}

impl State {
    /// Enables the assertion login principal for this state.
    ///
    /// # Errors
    /// Refuses a second verifier.
    pub fn enable_assertion(&mut self, verifier: AssertionVerifier) -> io::Result<()> {
        self.install_assertion_verifier(verifier)
    }

    /// Verifies a bearer assertion and opens or creates the account mapped to its
    /// issuer and subject, recording the wallet's DID when supplied.
    ///
    /// # Errors
    /// Refuses a disabled principal, a refused token, an invalid or conflicting DID,
    /// exhausted capacity and persistence failures.
    pub fn open_or_create_by_assertion(
        &mut self,
        token: &str,
        did: Option<&str>,
        now: u64,
    ) -> io::Result<(AssertionPrincipal, bool)> {
        self.ready()?;
        if let Some(did) = did {
            validate_wallet_did(did)?;
        }
        let verified = self.assertion_verifier()?.verify(token, now)?;
        self.record_assertion(&verified.issuer, &verified.subject, did, now)
    }

    /// Resolves a bearer assertion to its existing account and DID without creating one.
    ///
    /// # Errors
    /// Refuses a disabled principal, a refused token and an unknown subject.
    pub fn resolve_assertion(&self, token: &str, now: u64) -> io::Result<AssertionPrincipal> {
        let verified = self.assertion_verifier()?.verify(token, now)?;
        self.assertion_principal(&verified.issuer, &verified.subject)
            .ok_or_else(|| invalid("unknown assertion principal"))
    }

    pub(crate) fn assertion(
        &mut self,
        fields: &[Vec<u8>],
        now: u64,
    ) -> io::Result<(u8, Vec<Vec<u8>>)> {
        let token = std::str::from_utf8(&fields[0]).map_err(|_| invalid("invalid UTF-8"))?;
        let did = fields.get(1).map(|field| text(field)).transpose()?;
        if let Some(did) = did {
            validate_wallet_did(did)?;
        }
        let Ok(verifier) = self.assertion_verifier() else {
            return Ok(refused(AssertionRefusal::Unavailable));
        };
        let verified = match verifier.verify(token, now) {
            Ok(verified) => verified,
            Err(error) if error.kind() == io::ErrorKind::InvalidData => {
                return Ok(refused(AssertionRefusal::Assertion))
            }
            Err(_) => return Ok(refused(AssertionRefusal::Unavailable)),
        };
        match self.record_assertion(&verified.issuer, &verified.subject, did, now) {
            Ok((principal, _)) => Ok((
                0,
                std::iter::once(principal.principal())
                    .chain(principal.did())
                    .map(|value| value.as_bytes().to_vec())
                    .collect(),
            )),
            Err(error) => match error
                .get_ref()
                .and_then(|source| source.downcast_ref::<AssertionRefusal>())
                .copied()
            {
                Some(refusal) => Ok(refused(refusal)),
                None => Err(error),
            },
        }
    }
}

fn refused(refusal: AssertionRefusal) -> (u8, Vec<Vec<u8>>) {
    (refusal.status(), Vec::new())
}

pub(crate) fn validate_wallet_did(did: &str) -> io::Result<()> {
    let key = did
        .strip_prefix("did:layerx:")
        .ok_or_else(|| invalid("invalid wallet DID"))?;
    if key.len() != 64
        || !key
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        || key.bytes().all(|b| b == b'0')
    {
        return Err(invalid("invalid wallet DID"));
    }
    layerx_types::ids::Did::new(did.as_bytes()).map_err(|_| invalid("invalid wallet DID"))?;
    Ok(())
}

#[derive(Deserialize)]
struct KeySet {
    keys: Vec<serde_json::Value>,
}

#[derive(Deserialize)]
struct Claims {
    iss: String,
    sub: String,
    aud: Audience,
    exp: u64,
    #[serde(default)]
    nbf: Option<u64>,
}

#[derive(Deserialize)]
#[serde(untagged)]
enum Audience {
    One(String),
    Many(Vec<String>),
}

impl Audience {
    fn contains(&self, expected: &str) -> bool {
        match self {
            Self::One(value) => value == expected,
            Self::Many(values) => values.iter().any(|value| value == expected),
        }
    }
}

fn usable(jwk: &Jwk) -> Option<(String, Algorithm)> {
    let key_id = jwk
        .common
        .key_id
        .clone()
        .filter(|value| !value.is_empty() && value.len() <= 256)?;
    if jwk
        .common
        .public_key_use
        .as_ref()
        .is_some_and(|value| *value != PublicKeyUse::Signature)
    {
        return None;
    }
    let algorithm = match &jwk.algorithm {
        AlgorithmParameters::RSA(_) => Algorithm::RS256,
        AlgorithmParameters::EllipticCurve(parameters)
            if parameters.curve == EllipticCurve::P256 =>
        {
            Algorithm::ES256
        }
        _ => return None,
    };
    match (jwk.common.key_algorithm, algorithm) {
        (None, _)
        | (Some(KeyAlgorithm::RS256), Algorithm::RS256)
        | (Some(KeyAlgorithm::ES256), Algorithm::ES256) => Some((key_id, algorithm)),
        _ => None,
    }
}

fn trust_roots(url: &str) -> io::Result<ureq::tls::RootCerts> {
    let mut certificates = Vec::new();
    if url.starts_with("https://") {
        let loaded = rustls_native_certs::load_native_certs();
        if loaded.certs.is_empty() || !loaded.errors.is_empty() {
            return Err(io::Error::other("trust roots unavailable"));
        }
        certificates.extend(
            loaded.certs.iter().map(|certificate| {
                ureq::tls::Certificate::from_der(certificate.as_ref()).to_owned()
            }),
        );
    }
    Ok(ureq::tls::RootCerts::new_with_certs(&certificates))
}

fn validate_url(url: &str) -> io::Result<()> {
    if url.len() > 2048 || url.chars().any(|c| c.is_control() || c.is_whitespace()) {
        return Err(invalid("invalid assertion key set URL"));
    }
    let (secure, rest) = if let Some(rest) = url.strip_prefix("https://") {
        (true, rest)
    } else if let Some(rest) = url.strip_prefix("http://") {
        (false, rest)
    } else {
        return Err(invalid("invalid assertion key set URL"));
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or_default();
    if authority.is_empty() || authority.contains('@') {
        return Err(invalid("invalid assertion key set URL"));
    }
    let (host, port) = if authority.starts_with('[') {
        let end = authority
            .find(']')
            .ok_or_else(|| invalid("invalid assertion key set URL"))?;
        (&authority[..=end], &authority[end + 1..])
    } else {
        match authority.find(':') {
            Some(index) => (&authority[..index], &authority[index..]),
            None => (authority, ""),
        }
    };
    if host.is_empty()
        || !(port.is_empty()
            || (port.len() > 1
                && port.len() <= 6
                && port[1..].bytes().all(|b| b.is_ascii_digit())
                && port.starts_with(':')))
    {
        return Err(invalid("invalid assertion key set URL"));
    }
    if secure || matches!(host, "127.0.0.1" | "localhost" | "[::1]") {
        Ok(())
    } else {
        Err(invalid("plaintext assertion key set URL outside loopback"))
    }
}

fn bounded(value: &str, maximum: usize) -> io::Result<()> {
    if value.is_empty() || value.len() > maximum || value.chars().any(char::is_control) {
        return Err(invalid("invalid assertion text"));
    }
    Ok(())
}

fn seconds(value: Option<String>, default: u64) -> io::Result<u64> {
    value.map_or(Ok(default), |value| {
        value
            .parse::<u64>()
            .map_err(|_| invalid("invalid assertion time bound"))
    })
}
