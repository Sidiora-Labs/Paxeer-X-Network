//! F02-R016/R020/R025 discovery from current finalized authority and the authenticated
//! transport that reaches an advertised worker.
//!
//! Seam: the native finalized-authority reader of the client crate is not present yet, so
//! [`FinalizedAuthority::bind`] takes the pieces it would deliver directly: one complete
//! verified state capture, the finality evidence for the SAME root, the client head for the
//! freshness bound, the frozen roster document and the owner's identity evidence.
use crate::auth::{
    self, MetadataContext, ServiceError, SignedMetadata, StatusChallenge, WorkerBinding,
    ERROR_BODY_BYTES,
};
use crate::metadata::{Endpoint, Manifest};
use layerx_client::head::Head;
use layerx_programs_ai_market::{
    admission::{AdmissionMeta, AdmissionTable, Participant},
    codec,
    evaluators::authority::split_identity_section,
    queries::{bind_snapshot, CaptureFacts, FinalityEvidence, QueryError, SnapshotBinding},
    registry::{market_clock, MarketClock, MarketHeader},
    registry_ops::{PolicySection, ACTIVE, SUSPENDED},
    rewards::{decode_reward_state, REWARD_STATE_BYTES},
    state::{self, Section},
    types::{
        ChainDomain, EpochPhase, MarketId, MetadataDigest, PolicyDigest, Presence, PrincipalId,
        ProgramId, PublicKey32, RosterDigest, WorkerId, WorkerRosterEntry,
    },
    workers::{check_new_admission, WorkerCurrent, WorkerState, WorkerTable},
    MAX_ENVELOPE_BYTES,
};
use rustls::{
    client::{
        danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier},
        WebPkiServerVerifier,
    },
    pki_types::{CertificateDer, ServerName, UnixTime},
    server::ParsedCertificate,
    CertificateError, ClientConfig, ClientConnection, DigitallySignedStruct, RootCertStore,
    SignatureScheme, StreamOwned,
};
use sha2::{Digest, Sha256};
use std::fmt;
use std::io::{self, Read, Write};
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr, TcpStream};
use std::sync::Arc;
use std::time::Duration;

/// A finalized view older than this many sealed batches is stale for new admission.
pub const MAX_FINALITY_LAG: u64 = 8;
/// A transport or runner observation is current for this long.
pub const READINESS_TTL_MS: u64 = 30_000;
const MAX_RESPONSE_HEAD_BYTES: usize = 8_192;
const CLOUD_METADATA_V4: Ipv4Addr = Ipv4Addr::new(169, 254, 169, 254);
const CLOUD_METADATA_V6: Ipv6Addr = Ipv6Addr::new(0xfd00, 0x0ec2, 0, 0, 0, 0, 0, 0x0254);

/// Finalized identity facts of one principal at one execution height.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct IdentityEvidence {
    pub principal: PrincipalId,
    pub primary_key: PublicKey32,
    pub frozen: bool,
    pub execution_height: u64,
}

/// Everything one finalized authority view is built from.
#[derive(Clone, Copy, Debug)]
pub struct AuthorityEvidence<'a> {
    pub state: &'a [u8],
    pub facts: &'a CaptureFacts,
    pub finality: &'a FinalityEvidence,
    pub head: Head,
    pub owner: IdentityEvidence,
    pub roster: Option<&'a [u8]>,
    pub observed_ms: u64,
}

/// One worker's finalized authority: the snapshot binding, the F01 header, the F02 record,
/// F08 membership, the frozen roster entry of the open epoch and the owner's identity.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FinalizedAuthority {
    snapshot: SnapshotBinding,
    header: MarketHeader,
    record: WorkerCurrent,
    membership: Option<AdmissionMeta>,
    current_epoch: Option<u64>,
    roster: Presence<RosterDigest>,
    frozen: Option<WorkerRosterEntry>,
    owner: IdentityEvidence,
}

fn query(error: QueryError) -> ServiceError {
    match error {
        QueryError::Application(error) => error.into(),
        _ => ServiceError::StaleAuthority,
    }
}

impl FinalizedAuthority {
    /// Binds one finalized capture of `chain`/`program`/`market` to `worker`.
    ///
    /// # Errors
    /// `StaleAuthority` below finality rank 4, outside the freshness bound, for a roster
    /// document that is missing, superfluous or not the frozen one, or for owner evidence of
    /// another principal or height; `WrongDomain` for another market; `NotFound` for an
    /// unknown worker; state decoding refusals.
    pub fn bind(
        evidence: &AuthorityEvidence<'_>,
        chain: ChainDomain,
        program: ProgramId,
        market: MarketId,
        worker: WorkerId,
    ) -> Result<Self, ServiceError> {
        let snapshot = bind_snapshot(
            evidence.state,
            evidence.facts,
            evidence.finality,
            evidence.observed_ms,
        )
        .map_err(query)?;
        snapshot.require_finalized().map_err(query)?;
        if snapshot.chain != chain || snapshot.program != program || snapshot.market != market {
            return Err(ServiceError::WrongDomain);
        }
        let height = snapshot.execution_height;
        let sealed = evidence.head.sealed_batch;
        if sealed < height || sealed - height > MAX_FINALITY_LAG {
            return Err(ServiceError::StaleAuthority);
        }
        let shared = state::decode_shared_state(evidence.state)?;
        let header = PolicySection::decode(shared.section(Section::PolicyLifecycle)?)?.header;
        let (workers, _) = split_identity_section(shared.section(Section::IdentityRoster)?)?;
        let record = WorkerTable::decode(workers)?
            .get(worker)
            .ok_or(ServiceError::NotFound)?;
        let admission = AdmissionTable::decode(shared.section(Section::ReputationAdmission)?)?;
        let current_epoch = admission.current_epoch();
        let (roster, frozen) = match (current_epoch, evidence.roster) {
            (None, None) => (Presence::Absent, None),
            (Some(epoch), Some(document)) => {
                let rewards = shared.section(Section::SettlementClaims)?;
                let rewards = rewards
                    .get(..REWARD_STATE_BYTES)
                    .ok_or(ServiceError::StaleAuthority)?;
                let digest = decode_reward_state(rewards)?
                    .row(epoch)
                    .map_err(|_| ServiceError::StaleAuthority)?
                    .roster;
                let view = codec::decode_roster(document)?;
                if view.market != market
                    || view.epoch != epoch
                    || view.config != snapshot.config
                    || view.digest()? != digest
                {
                    return Err(ServiceError::StaleAuthority);
                }
                let mut frozen = None;
                for index in 0..view.worker_count() {
                    let entry = view.worker(index)?;
                    if entry.worker == worker {
                        frozen = Some(entry);
                    }
                }
                (Presence::Present(digest), frozen)
            }
            _ => return Err(ServiceError::StaleAuthority),
        };
        if evidence.owner.principal != record.owner || evidence.owner.execution_height != height {
            return Err(ServiceError::StaleAuthority);
        }
        Ok(Self {
            snapshot,
            header,
            record,
            membership: admission.get(Participant::Worker(worker)),
            current_epoch,
            roster,
            frozen,
            owner: evidence.owner,
        })
    }

    #[must_use]
    pub const fn snapshot(&self) -> &SnapshotBinding {
        &self.snapshot
    }
    #[must_use]
    pub const fn height(&self) -> u64 {
        self.snapshot.execution_height
    }
    #[must_use]
    pub const fn policy(&self) -> PolicyDigest {
        self.snapshot.policy
    }
    #[must_use]
    pub const fn config(&self) -> u64 {
        self.snapshot.config.get()
    }
    #[must_use]
    pub const fn roster(&self) -> Presence<RosterDigest> {
        self.roster
    }
    #[must_use]
    pub const fn current_epoch(&self) -> Option<u64> {
        self.current_epoch
    }
    #[must_use]
    pub const fn record(&self) -> &WorkerCurrent {
        &self.record
    }
    #[must_use]
    pub const fn worker_binding(&self) -> WorkerBinding {
        WorkerBinding {
            chain: self.snapshot.chain,
            program: self.snapshot.program,
            market: self.snapshot.market,
            worker: self.record.worker,
            owner: self.record.owner,
            delegate: self.record.delegate,
            generation: self.record.generation,
            key_version: self.record.key_version,
            metadata_revision: self.record.metadata_revision,
        }
    }
    #[must_use]
    pub const fn metadata_context(&self) -> MetadataContext {
        MetadataContext {
            chain: self.snapshot.chain,
            program: self.snapshot.program,
            market: self.snapshot.market,
            worker: self.record.worker,
            owner: self.record.owner,
            delegate: self.record.delegate,
        }
    }
    /// The market clock at the finalized height.
    ///
    /// # Errors
    /// `Overflow` when the height precedes the market origin or a window overflows.
    pub fn clock(&self) -> Result<MarketClock, ServiceError> {
        Ok(market_clock(self.header.origin_height, self.height())?)
    }
    /// Exclusive end of the open Work window: the latest admissible task deadline.
    ///
    /// # Errors
    /// Clock refusals.
    pub fn work_close(&self) -> Result<u64, ServiceError> {
        Ok(self.clock()?.windows.commit)
    }

    /// F02-R017 readiness for NEW work at the finalized height.
    ///
    /// # Errors
    /// `MarketPaused` while suspended; `AdmissionNotEffective` outside an active market, an
    /// open epoch's Work window, the frozen roster or current F08 membership, or before the
    /// metadata window; `IdentityFrozen` for a frozen owner; `DelegateRevoked` for a revoked
    /// worker; `WrongGeneration` when the frozen versions or key differ from the record;
    /// `MetadataExpired` at or after the metadata expiry.
    pub fn admission_gate(&self) -> Result<(), ServiceError> {
        match self.header.lifecycle {
            ACTIVE => {}
            SUSPENDED => return Err(ServiceError::MarketPaused),
            _ => return Err(ServiceError::AdmissionNotEffective),
        }
        self.release_gate()?;
        let clock = self.clock()?;
        if self.current_epoch != Some(clock.epoch) || clock.phase != EpochPhase::Work {
            return Err(ServiceError::AdmissionNotEffective);
        }
        let frozen = self.frozen.ok_or(ServiceError::AdmissionNotEffective)?;
        if !self
            .membership
            .is_some_and(|m| m.admitted() && !m.revoked() && !m.draining())
        {
            return Err(ServiceError::AdmissionNotEffective);
        }
        check_new_admission(&self.record, &frozen, self.height()).map_err(|error| {
            match ServiceError::from(error) {
                ServiceError::StaleAuthority => ServiceError::AdmissionNotEffective,
                other => other,
            }
        })?;
        if self.height() < self.record.valid_from {
            return Err(ServiceError::AdmissionNotEffective);
        }
        Ok(())
    }

    /// Readiness to serve or release anything for already admitted work.
    ///
    /// # Errors
    /// `IdentityFrozen` for a frozen owner; `DelegateRevoked` for a revoked worker.
    pub fn release_gate(&self) -> Result<(), ServiceError> {
        if self.owner.frozen {
            return Err(ServiceError::IdentityFrozen);
        }
        if self.record.state == WorkerState::Revoked {
            return Err(ServiceError::DelegateRevoked);
        }
        Ok(())
    }

    /// Binds a verified signed manifest to the finalized current record.
    ///
    /// # Errors
    /// `WrongDomain`, `OwnerRequired` or `BadSignature` when it was verified for another
    /// worker, owner or delegate; `MetadataIntegrityFailure` for another digest or window;
    /// `WrongGeneration`; `WrongRevision`.
    pub fn bind_metadata(&self, signed: SignedMetadata) -> Result<VerifiedMetadata, ServiceError> {
        let expected = self.metadata_context();
        let context = signed.context;
        if (
            context.chain,
            context.program,
            context.market,
            context.worker,
        ) != (
            expected.chain,
            expected.program,
            expected.market,
            expected.worker,
        ) {
            return Err(ServiceError::WrongDomain);
        }
        if context.owner != expected.owner {
            return Err(ServiceError::OwnerRequired);
        }
        if context.delegate != expected.delegate {
            return Err(ServiceError::BadSignature);
        }
        if signed.digest != self.record.metadata {
            return Err(ServiceError::MetadataIntegrityFailure);
        }
        let manifest = signed.manifest;
        if manifest.generation != self.record.generation
            || manifest.key_version != self.record.key_version
        {
            return Err(ServiceError::WrongGeneration);
        }
        if manifest.revision != self.record.metadata_revision {
            return Err(ServiceError::WrongRevision);
        }
        if manifest.valid_from != self.record.valid_from || manifest.expiry != self.record.expiry {
            return Err(ServiceError::MetadataIntegrityFailure);
        }
        Ok(VerifiedMetadata {
            manifest,
            digest: signed.digest,
        })
    }
}

/// A manifest whose signature, grammar and digest match the finalized current record.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedMetadata {
    manifest: Manifest,
    digest: MetadataDigest,
}
impl VerifiedMetadata {
    #[must_use]
    pub const fn manifest(&self) -> &Manifest {
        &self.manifest
    }
    #[must_use]
    pub const fn digest(&self) -> MetadataDigest {
        self.digest
    }
}

/// Worker readiness ladder; only `VerifiedTransport` and `RunnerReady` come from probes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Readiness {
    NotReady,
    Advertised,
    VerifiedTransport,
    RunnerReady,
}

/// A probe result that expires `READINESS_TTL_MS` after it was observed.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReadinessObservation {
    pub state: Readiness,
    pub observed_ms: u64,
}
impl ReadinessObservation {
    #[must_use]
    pub const fn current(&self, now_ms: u64) -> Readiness {
        match now_ms.checked_sub(self.observed_ms) {
            Some(age) if age < READINESS_TTL_MS => self.state,
            _ => Readiness::NotReady,
        }
    }
}

/// One discovery result: eligibility from finalized authority, the verified manifest and
/// the advertised readiness that follows from both.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Discovered {
    pub eligibility: Result<(), ServiceError>,
    pub metadata: Result<VerifiedMetadata, ServiceError>,
    pub readiness: Readiness,
}

/// Discovers one worker: retrieves its signed manifest by the finalized digest, verifies it
/// and advertises it only when both the manifest and new-work eligibility hold.
pub fn discover(
    authority: &FinalizedAuthority,
    retrieve: impl FnOnce(MetadataDigest) -> io::Result<Vec<u8>>,
) -> Discovered {
    let eligibility = authority.admission_gate();
    let metadata = retrieve(authority.record().metadata)
        .map_err(|_| ServiceError::MetadataUnavailable)
        .and_then(|signed| auth::verify_signed_metadata(&signed, &authority.metadata_context()))
        .and_then(|signed| authority.bind_metadata(signed));
    let readiness = if eligibility.is_ok() && metadata.is_ok() {
        Readiness::Advertised
    } else {
        Readiness::NotReady
    };
    Discovered {
        eligibility,
        metadata,
        readiness,
    }
}

/// Which resolved addresses a client may connect to.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum NetworkProfile {
    Public,
    Private { allowlist: Vec<IpAddr> },
}

fn public_v4(ip: Ipv4Addr) -> bool {
    let [a, b, c, _] = ip.octets();
    !(ip.is_unspecified()
        || ip.is_loopback()
        || ip.is_private()
        || ip.is_link_local()
        || ip.is_multicast()
        || ip.is_broadcast()
        || ip.is_documentation()
        || a == 0
        || a >= 240
        || (a == 100 && (64..128).contains(&b))
        || (a == 192 && b == 0 && c == 0)
        || (a == 198 && (b == 18 || b == 19)))
}

fn public_v6(ip: Ipv6Addr) -> bool {
    let s = ip.segments();
    !(ip.is_unspecified()
        || ip.is_loopback()
        || ip.is_multicast()
        || (s[0] & 0xfe00) == 0xfc00
        || (s[0] & 0xffc0) == 0xfe80
        || (s[0] & 0xffc0) == 0xfec0
        || (s[0] == 0x2001 && s[1] == 0x0db8)
        || (s[0] == 0x0064 && s[1] == 0xff9b)
        || s[0] == 0x2002
        || (s[0] == 0x2001 && s[1] == 0))
}

/// Never reachable under any profile: unspecified, multicast, broadcast, link-local and
/// cloud metadata addresses.
fn forbidden(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            v4.is_unspecified()
                || v4.is_multicast()
                || v4.is_broadcast()
                || v4.is_link_local()
                || v4 == CLOUD_METADATA_V4
        }
        IpAddr::V6(v6) => {
            v6.is_unspecified()
                || v6.is_multicast()
                || (v6.segments()[0] & 0xffc0) == 0xfe80
                || v6 == CLOUD_METADATA_V6
        }
    }
}

fn canonical(ip: IpAddr) -> IpAddr {
    match ip {
        IpAddr::V6(v6) => v6.to_ipv4_mapped().map_or(ip, IpAddr::V4),
        IpAddr::V4(_) => ip,
    }
}

impl NetworkProfile {
    /// Whether this profile admits `ip` as a connection target.
    #[must_use]
    pub fn admits(&self, ip: IpAddr) -> bool {
        let ip = canonical(ip);
        if forbidden(ip) {
            return false;
        }
        match self {
            Self::Public => match ip {
                IpAddr::V4(v4) => public_v4(v4),
                IpAddr::V6(v6) => public_v6(v6),
            },
            Self::Private { allowlist } => allowlist.iter().any(|a| canonical(*a) == ip),
        }
    }
}

/// Transport refusals; a `Service` refusal carries the worker's signed-transport error code.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportError {
    UnsafeEndpoint,
    UnsafeAddress,
    Unavailable,
    Certificate,
    PinMismatch,
    Protocol,
    Redirect,
    Status(u16),
    Malformed,
    Service(ServiceError),
}
impl fmt::Display for TransportError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::UnsafeEndpoint => f.write_str("endpoint is not a canonical service endpoint"),
            Self::UnsafeAddress => {
                f.write_str("endpoint resolved to an address outside the profile")
            }
            Self::Unavailable => f.write_str("endpoint unavailable"),
            Self::Certificate => f.write_str("endpoint certificate refused"),
            Self::PinMismatch => f.write_str("endpoint certificate does not match its pin"),
            Self::Protocol => f.write_str("transport protocol failure"),
            Self::Redirect => f.write_str("endpoint redirected"),
            Self::Status(code) => write!(f, "endpoint answered status {code}"),
            Self::Malformed => f.write_str("malformed endpoint response"),
            Self::Service(error) => error.fmt(f),
        }
    }
}
impl std::error::Error for TransportError {}

/// SHA-256 of the leaf certificate's DER `SubjectPublicKeyInfo`, the manifest pin.
///
/// # Errors
/// The parser's refusal for a malformed certificate.
pub fn spki_sha256(certificate: &CertificateDer<'_>) -> Result<[u8; 32], rustls::Error> {
    let parsed = ParsedCertificate::try_from(certificate)?;
    Ok(Sha256::digest(parsed.subject_public_key_info().as_ref()).into())
}

/// `WebPKI` chain and name validation, then the manifest SPKI pin of the leaf.
#[derive(Debug)]
struct PinnedVerifier {
    inner: Arc<WebPkiServerVerifier>,
    pin: [u8; 32],
}
impl ServerCertVerifier for PinnedVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        intermediates: &[CertificateDer<'_>],
        server_name: &ServerName<'_>,
        ocsp_response: &[u8],
        now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        let verified = self.inner.verify_server_cert(
            end_entity,
            intermediates,
            server_name,
            ocsp_response,
            now,
        )?;
        if spki_sha256(end_entity)? != self.pin {
            return Err(rustls::Error::InvalidCertificate(
                CertificateError::ApplicationVerificationFailure,
            ));
        }
        Ok(verified)
    }
    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        self.inner.verify_tls12_signature(message, cert, dss)
    }
    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        self.inner.verify_tls13_signature(message, cert, dss)
    }
    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.inner.supported_verify_schemes()
    }
}

fn tls_failure(error: &io::Error) -> TransportError {
    match error
        .get_ref()
        .and_then(|inner| inner.downcast_ref::<rustls::Error>())
    {
        Some(rustls::Error::InvalidCertificate(
            CertificateError::ApplicationVerificationFailure,
        )) => TransportError::PinMismatch,
        Some(rustls::Error::InvalidCertificate(_)) => TransportError::Certificate,
        Some(_) => TransportError::Protocol,
        None => TransportError::Unavailable,
    }
}

/// Client for one worker endpoint: one resolution, every answer inside the profile, a TLS
/// 1.3 handshake validated by `WebPKI` and the manifest pin before any request byte is sent,
/// and no redirect following.
#[derive(Clone, Debug)]
pub struct EndpointClient {
    pub profile: NetworkProfile,
    pub roots: Arc<RootCertStore>,
    pub timeout: Duration,
}
impl EndpointClient {
    fn connect(
        &self,
        host: &str,
        port: u16,
        resolve: impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>>,
    ) -> Result<TcpStream, TransportError> {
        let answers = resolve(host, port).map_err(|_| TransportError::Unavailable)?;
        if answers.is_empty() {
            return Err(TransportError::Unavailable);
        }
        if !answers.iter().all(|ip| self.profile.admits(*ip)) {
            return Err(TransportError::UnsafeAddress);
        }
        let socket = answers
            .iter()
            .find_map(|ip| {
                TcpStream::connect_timeout(&SocketAddr::new(*ip, port), self.timeout).ok()
            })
            .ok_or(TransportError::Unavailable)?;
        let peer = socket
            .peer_addr()
            .map_err(|_| TransportError::Unavailable)?;
        if !self.profile.admits(peer.ip()) {
            return Err(TransportError::UnsafeAddress);
        }
        socket
            .set_read_timeout(Some(self.timeout))
            .and_then(|()| socket.set_write_timeout(Some(self.timeout)))
            .map_err(|_| TransportError::Unavailable)?;
        Ok(socket)
    }

    fn handshake(
        &self,
        endpoint: &Endpoint,
        host: &str,
        socket: TcpStream,
    ) -> Result<StreamOwned<ClientConnection, TcpStream>, TransportError> {
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let inner =
            WebPkiServerVerifier::builder_with_provider(self.roots.clone(), provider.clone())
                .build()
                .map_err(|_| TransportError::Certificate)?;
        let config = ClientConfig::builder_with_provider(provider)
            .with_protocol_versions(&[&rustls::version::TLS13])
            .map_err(|_| TransportError::Protocol)?
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(PinnedVerifier {
                inner,
                pin: endpoint.spki_sha256,
            }))
            .with_no_client_auth();
        let name =
            ServerName::try_from(host.to_owned()).map_err(|_| TransportError::UnsafeEndpoint)?;
        let connection =
            ClientConnection::new(Arc::new(config), name).map_err(|_| TransportError::Protocol)?;
        let mut stream = StreamOwned::new(connection, socket);
        while stream.conn.is_handshaking() {
            stream
                .conn
                .complete_io(&mut stream.sock)
                .map_err(|error| tls_failure(&error))?;
        }
        Ok(stream)
    }

    /// Posts one signed body to `path` of `endpoint` and returns the 200 response body.
    ///
    /// # Errors
    /// `UnsafeEndpoint` for a noncanonical endpoint or path; `UnsafeAddress` when any resolved
    /// or connected address is outside the profile; `Unavailable`; `Certificate` for a chain,
    /// name or validity refusal; `PinMismatch`; `Protocol`; `Redirect` for any 3xx;
    /// `Service` for a worker refusal body; `Status` for any other status; `Malformed`.
    pub fn post(
        &self,
        endpoint: &Endpoint,
        path: &str,
        body: &[u8],
        resolve: impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>>,
    ) -> Result<Vec<u8>, TransportError> {
        let (host, port) = endpoint
            .authority()
            .map_err(|_| TransportError::UnsafeEndpoint)?;
        if !path.starts_with("/paxai/v1/") || body.len() > MAX_ENVELOPE_BYTES {
            return Err(TransportError::UnsafeEndpoint);
        }
        let socket = self.connect(host, port, resolve)?;
        let mut stream = self.handshake(endpoint, host, socket)?;
        let head = format!(
            "POST {path} HTTP/1.1\r\nHost: {host}:{port}\r\nContent-Type: application/octet-stream\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
            body.len()
        );
        stream
            .write_all(head.as_bytes())
            .and_then(|()| stream.write_all(body))
            .and_then(|()| stream.flush())
            .map_err(|error| tls_failure(&error))?;
        let (status, response) = read_response(&mut stream)?;
        match status {
            200 => Ok(response),
            300..=399 => Err(TransportError::Redirect),
            _ if response.len() == ERROR_BODY_BYTES => Err(TransportError::Service(
                ServiceError::decode_body(&response)
                    .map_err(|_| TransportError::Malformed)?
                    .0,
            )),
            _ => Err(TransportError::Status(status)),
        }
    }

    /// Authenticated status probe: a verified signed status from the bound delegate
    /// promotes the endpoint to `VerifiedTransport` as of `now_ms`.
    ///
    /// # Errors
    /// Transport refusals of [`Self::post`]; `Service` for a status that does not verify.
    pub fn verify_status(
        &self,
        authority: &FinalizedAuthority,
        endpoint: &Endpoint,
        challenge: &StatusChallenge,
        now_ms: u64,
        resolve: impl FnOnce(&str, u16) -> io::Result<Vec<IpAddr>>,
    ) -> Result<ReadinessObservation, TransportError> {
        let signed = self.post(endpoint, auth::STATUS_PATH, &challenge.encode(), resolve)?;
        auth::verify_status(&signed, &authority.worker_binding(), challenge)
            .map_err(TransportError::Service)?;
        Ok(ReadinessObservation {
            state: Readiness::VerifiedTransport,
            observed_ms: now_ms,
        })
    }
}

/// Reads `HTTP/1.1 <status>` and a `Content-Length` body of at most one envelope.
fn read_response(stream: &mut impl Read) -> Result<(u16, Vec<u8>), TransportError> {
    let mut head = Vec::new();
    let mut byte = [0u8; 1];
    while !head.ends_with(b"\r\n\r\n") {
        if head.len() >= MAX_RESPONSE_HEAD_BYTES {
            return Err(TransportError::Malformed);
        }
        stream
            .read_exact(&mut byte)
            .map_err(|error| tls_failure(&error))?;
        head.push(byte[0]);
    }
    let text = std::str::from_utf8(&head).map_err(|_| TransportError::Malformed)?;
    let mut lines = text.split("\r\n");
    let status = lines
        .next()
        .and_then(|line| line.strip_prefix("HTTP/1.1 "))
        .and_then(|rest| rest.get(..3))
        .and_then(|code| code.parse::<u16>().ok())
        .ok_or(TransportError::Malformed)?;
    let mut length = None;
    for line in lines {
        if let Some((name, value)) = line.split_once(':') {
            if name.eq_ignore_ascii_case("content-length") {
                length = Some(
                    value
                        .trim()
                        .parse::<usize>()
                        .map_err(|_| TransportError::Malformed)?,
                );
            }
        }
    }
    let length = length.unwrap_or(0);
    if length > MAX_ENVELOPE_BYTES {
        return Err(TransportError::Malformed);
    }
    let mut body = vec![0; length];
    stream
        .read_exact(&mut body)
        .map_err(|error| tls_failure(&error))?;
    Ok((status, body))
}
