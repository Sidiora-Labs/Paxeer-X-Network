//! Server side of the bounded LXKP protocol. The gateway terminates mutual TLS
//! from one pinned client identity and relays probe, create, describe, rotate,
//! destroy and sign to an explicitly configured production KMS provider. It
//! never holds, receives or returns private key material.

use std::future::Future;
use std::io::{ErrorKind, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, Mutex};
use std::task::{Context, Poll, Wake, Waker};
use std::time::{Duration, Instant};

use layerx_client::lni::transport::{ConnectionGate, ConnectionPermit};
use layerx_types::payload::ModuleRegistry;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::server::WebPkiClientVerifier;
use rustls::{RootCertStore, ServerConfig, ServerConnection, StreamOwned};
use sha2::{Digest as _, Sha256};
use zeroize::{Zeroize, Zeroizing};

use super::provider::validate_disclosed;
use super::{
    CustodyError, KeyClass, KmsError, KmsProvider, PrincipalKeyBinding, ProviderDeployment,
    ProviderKeyDescription, ProviderKeyReference, ProviderSignRequest, RotationState,
    KEY_REFERENCE_LIMIT,
};

/// Largest LXKP frame the gateway ever admits or relays.
pub const GATEWAY_FRAME_LIMIT: usize = 2_097_152;
const MAGIC: &[u8; 4] = b"LXKP";
const PROVIDER_REFERENCE_LIMIT: usize = 4096;
const CERTIFICATE_LIMIT: usize = 65_536;
const MAXIMUM_CONNECTIONS: usize = 1024;
const MAXIMUM_DEADLINE: Duration = Duration::from_secs(60);
const MAXIMUM_WINDOW: Duration = Duration::from_secs(3600);
const STATUS_OK: u8 = 0;

/// The only LXKP contracts the gateway serves. EVM custody, send
/// authorization, recipient authorization and primary-key export are never
/// relayed, so no key material can leave the provider through the gateway.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Operation {
    Probe,
    Create,
    Describe,
    Rotate,
    RotateIfCurrent,
    Destroy,
    Sign,
}

impl Operation {
    const fn from_wire(version: u16, operation: u8) -> Option<Self> {
        match (version, operation) {
            (1, 0) => Some(Self::Probe),
            (1, 1) => Some(Self::Create),
            (1, 2) => Some(Self::Describe),
            (1, 3) => Some(Self::Rotate),
            (2, 3) => Some(Self::RotateIfCurrent),
            (1, 4) => Some(Self::Destroy),
            (1, 5) => Some(Self::Sign),
            _ => None,
        }
    }
}

/// Finite connection, frame, deadline and rate bounds of one gateway.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct GatewayLimits {
    /// Live authenticated or handshaking connections admitted at once.
    pub maximum_connections: usize,
    /// Largest request frame read from a client.
    pub maximum_frame_bytes: usize,
    /// Wall-clock budget for the handshake and the complete request read, and
    /// separately for the relayed provider operation.
    pub deadline: Duration,
    /// Operations admitted per window across every authenticated connection.
    pub maximum_operations: u32,
    /// Length of the fixed operation-rate window.
    pub operation_window: Duration,
}

impl GatewayLimits {
    fn validate(self) -> Result<Self, CustodyError> {
        if self.maximum_connections == 0
            || self.maximum_connections > MAXIMUM_CONNECTIONS
            || self.maximum_frame_bytes == 0
            || self.maximum_frame_bytes > GATEWAY_FRAME_LIMIT
            || self.deadline.is_zero()
            || self.deadline > MAXIMUM_DEADLINE
            || self.maximum_operations == 0
            || self.operation_window.is_zero()
            || self.operation_window > MAXIMUM_WINDOW
        {
            return Err(CustodyError::InvalidLimits);
        }
        Ok(self)
    }
}

/// Mandatory mutual-TLS server identity plus the single pinned client leaf.
pub struct GatewayTls {
    config: Arc<ServerConfig>,
    client_pin: [u8; 32],
}

impl GatewayTls {
    /// Builds a server configuration that always demands a client certificate
    /// chaining to `client_ca` and pins the exact client leaf certificate.
    ///
    /// # Errors
    ///
    /// Refuses empty or oversized material, an invalid client authority, and a
    /// server certificate that does not match its private key.
    pub fn new(
        client_ca: &[u8],
        server_certificate: &[u8],
        server_private_key: &Zeroizing<Vec<u8>>,
        client_certificate: &[u8],
    ) -> Result<Self, CustodyError> {
        let refused = || CustodyError::Kms(KmsError::Authentication);
        for material in [
            client_ca,
            server_certificate,
            server_private_key.as_slice(),
            client_certificate,
        ] {
            if material.is_empty() || material.len() > CERTIFICATE_LIMIT {
                return Err(refused());
            }
        }
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let mut roots = RootCertStore::empty();
        roots
            .add(CertificateDer::from(client_ca.to_vec()))
            .map_err(|_| refused())?;
        let verifier =
            WebPkiClientVerifier::builder_with_provider(Arc::new(roots), Arc::clone(&provider))
                .build()
                .map_err(|_| refused())?;
        let key = PrivateKeyDer::try_from(server_private_key.to_vec()).map_err(|_| refused())?;
        let config = ServerConfig::builder_with_provider(provider)
            .with_safe_default_protocol_versions()
            .map_err(|_| refused())?
            .with_client_cert_verifier(verifier)
            .with_single_cert(vec![CertificateDer::from(server_certificate.to_vec())], key)
            .map_err(|_| refused())?;
        Ok(Self {
            config: Arc::new(config),
            client_pin: Sha256::digest(client_certificate).into(),
        })
    }
}

impl std::fmt::Debug for GatewayTls {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("GatewayTls")
            .field("client_pin", &self.client_pin)
            .finish_non_exhaustive()
    }
}

/// Network, protocol, registry and bounds every relayed operation is held to.
#[derive(Clone, Debug)]
pub struct GatewayPolicy {
    pub network_id: u32,
    pub protocol_version: u16,
    pub registry: ModuleRegistry,
    pub limits: GatewayLimits,
}

/// Production LXKP gateway backed by one non-exportable KMS provider.
pub struct LxkpGateway {
    provider: Arc<dyn KmsProvider>,
    tls: GatewayTls,
    network_id: u32,
    protocol_version: u16,
    registry: ModuleRegistry,
    limits: GatewayLimits,
    gate: ConnectionGate,
    window: Mutex<(Instant, u32)>,
}

impl std::fmt::Debug for LxkpGateway {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("LxkpGateway")
            .field("provider", &self.provider)
            .field("tls", &self.tls)
            .field("network_id", &self.network_id)
            .field("protocol_version", &self.protocol_version)
            .field("limits", &self.limits)
            .finish_non_exhaustive()
    }
}

impl LxkpGateway {
    /// Admits a production provider and proves it reachable and authenticated
    /// before the gateway can serve anything.
    ///
    /// # Errors
    ///
    /// Refuses a development-only provider, an invalid provider reference,
    /// network, protocol or bound, and fails closed when the provider is
    /// absent or rejects the gateway's identity.
    pub fn new(
        provider: Arc<dyn KmsProvider>,
        tls: GatewayTls,
        policy: GatewayPolicy,
    ) -> Result<Self, CustodyError> {
        if provider.deployment() != ProviderDeployment::Production {
            return Err(CustodyError::DevelopmentProviderInProduction);
        }
        let reference = provider.provider_reference();
        if reference.is_empty()
            || reference.len() > KEY_REFERENCE_LIMIT
            || reference.as_bytes().contains(&0)
        {
            return Err(CustodyError::InvalidKeyReference);
        }
        if policy.network_id == 0
            || !layerx_intents::canonical::protocol_version_supported(policy.protocol_version)
        {
            return Err(CustodyError::InvalidNetwork);
        }
        let limits = policy.limits.validate()?;
        provider.probe().map_err(CustodyError::Kms)?;
        Ok(Self {
            provider,
            tls,
            network_id: policy.network_id,
            protocol_version: policy.protocol_version,
            registry: policy.registry,
            limits,
            gate: ConnectionGate::new(limits.maximum_connections),
            window: Mutex::new((Instant::now(), 0)),
        })
    }

    /// Accepts connections until the listener fails. Each admitted connection
    /// runs on its own worker holding one connection permit; connections over
    /// the bound are closed before the TLS handshake.
    ///
    /// # Errors
    ///
    /// Returns the listener or worker failure that stopped the gateway.
    pub fn serve(self: Arc<Self>, listener: &TcpListener) -> Result<(), CustodyError> {
        for stream in listener.incoming() {
            let stream = match stream {
                Ok(stream) => stream,
                Err(error)
                    if matches!(
                        error.kind(),
                        ErrorKind::ConnectionAborted | ErrorKind::Interrupted
                    ) =>
                {
                    continue;
                }
                Err(error) => return Err(CustodyError::Io(error)),
            };
            let Ok(permit) = self.gate.acquire() else {
                drop(stream);
                continue;
            };
            let gateway = Arc::clone(&self);
            std::thread::Builder::new()
                .name("lxkp-gateway".into())
                .spawn(move || {
                    let _permit: ConnectionPermit = permit;
                    let _ = gateway.exchange(stream);
                })
                .map_err(CustodyError::Io)?;
        }
        Err(CustodyError::Kms(KmsError::Unavailable))
    }

    fn exchange(&self, mut tcp: TcpStream) -> Result<(), KmsError> {
        let started = Instant::now();
        let mut server = ServerConnection::new(Arc::clone(&self.tls.config))
            .map_err(|_| KmsError::Unavailable)?;
        while server.is_handshaking() {
            self.bound(&tcp, started)?;
            server
                .complete_io(&mut tcp)
                .map_err(|_| KmsError::Authentication)?;
        }
        let leaf = server
            .peer_certificates()
            .and_then(|chain| chain.first())
            .ok_or(KmsError::Authentication)?;
        let observed: [u8; 32] = Sha256::digest(leaf.as_ref()).into();
        if !layerx_crypto::ct::eq_fixed(&observed, &self.tls.client_pin) {
            return Err(KmsError::Authentication);
        }
        let mut stream = StreamOwned::new(server, tcp);
        let frame = self.read_request(&mut stream, started)?;
        let Some(response) = self.respond(&frame) else {
            return Err(KmsError::Refused);
        };
        let response = Zeroizing::new(response);
        let length = u32::try_from(response.len()).map_err(|_| KmsError::Refused)?;
        stream
            .sock
            .set_write_timeout(Some(self.limits.deadline))
            .map_err(|_| KmsError::Unavailable)?;
        stream
            .write_all(&length.to_be_bytes())
            .and_then(|()| stream.write_all(&response))
            .and_then(|()| stream.flush())
            .map_err(|_| KmsError::Unavailable)?;
        stream.conn.send_close_notify();
        stream.flush().map_err(|_| KmsError::Unavailable)
    }

    fn bound(&self, tcp: &TcpStream, started: Instant) -> Result<(), KmsError> {
        let remaining = self
            .limits
            .deadline
            .checked_sub(started.elapsed())
            .filter(|remaining| !remaining.is_zero())
            .ok_or(KmsError::Timeout)?;
        tcp.set_read_timeout(Some(remaining))
            .and_then(|()| tcp.set_write_timeout(Some(remaining)))
            .map_err(|_| KmsError::Unavailable)
    }

    fn read_request(
        &self,
        stream: &mut StreamOwned<ServerConnection, TcpStream>,
        started: Instant,
    ) -> Result<Zeroizing<Vec<u8>>, KmsError> {
        let mut prefix = [0_u8; 4];
        self.read_exact(stream, &mut prefix, started)?;
        let length = usize::try_from(u32::from_be_bytes(prefix)).map_err(|_| KmsError::Refused)?;
        if length == 0 || length > self.limits.maximum_frame_bytes {
            return Err(KmsError::Refused);
        }
        let mut frame = Zeroizing::new(vec![0_u8; length]);
        self.read_exact(stream, &mut frame, started)?;
        Ok(frame)
    }

    fn read_exact(
        &self,
        stream: &mut StreamOwned<ServerConnection, TcpStream>,
        output: &mut [u8],
        started: Instant,
    ) -> Result<(), KmsError> {
        let mut offset = 0;
        while offset < output.len() {
            self.bound(&stream.sock, started)?;
            match stream.read(&mut output[offset..]) {
                Ok(0) => return Err(KmsError::Refused),
                Ok(read) => offset += read,
                Err(error) if error.kind() == ErrorKind::Interrupted => {}
                Err(_) => return Err(KmsError::Timeout),
            }
        }
        Ok(())
    }

    fn admit(&self) -> bool {
        let Ok(mut window) = self.window.lock() else {
            return false;
        };
        let now = Instant::now();
        if now.duration_since(window.0) >= self.limits.operation_window {
            *window = (now, 0);
        }
        if window.1 >= self.limits.maximum_operations {
            return false;
        }
        window.1 += 1;
        true
    }

    /// Answers one authenticated request frame. A frame without a readable
    /// LXKP header gets no answer and its connection is closed.
    fn respond(&self, frame: &[u8]) -> Option<Vec<u8>> {
        let header = frame.get(..7)?;
        if &header[..4] != MAGIC {
            return None;
        }
        let version = u16::from_be_bytes([header[4], header[5]]);
        let operation = header[6];
        let result = if self.admit() {
            decode(frame, self.provider.provider_reference(), self.network_id)
                .and_then(|request| self.dispatch(&request))
        } else {
            Err(KmsError::Unavailable)
        };
        let mut response = MAGIC.to_vec();
        response.extend_from_slice(&version.to_be_bytes());
        response.push(operation);
        match result {
            Ok(mut body) => {
                response.push(STATUS_OK);
                response.extend_from_slice(&body);
                body.zeroize();
            }
            Err(error) => response.push(status(error)),
        }
        Some(response)
    }

    fn dispatch(&self, request: &Request<'_>) -> Result<Vec<u8>, KmsError> {
        if request.operation == Operation::Probe {
            self.provider.probe()?;
            return Ok(Vec::new());
        }
        let class = KeyClass::from_code(request.class).map_err(|_| KmsError::Refused)?;
        let binding = PrincipalKeyBinding::from_digest(request.binding, request.network, class)?;
        if request.operation == Operation::Create {
            return describe_response(&binding, &self.provider.create_key(&binding)?);
        }
        let reference = ProviderKeyReference::new(request.reference.to_vec())?;
        match request.operation {
            Operation::Describe => {
                describe_response(&binding, &self.provider.describe_key(&binding, &reference)?)
            }
            Operation::Rotate => {
                describe_response(&binding, &self.provider.rotate_key(&binding, &reference)?)
            }
            Operation::RotateIfCurrent => describe_response(
                &binding,
                &self
                    .provider
                    .rotate_key_if_current(&binding, &reference, request.expected)?,
            ),
            Operation::Destroy => {
                self.provider.destroy_key(&binding, &reference)?;
                Ok(Vec::new())
            }
            Operation::Sign => self.sign(&binding, &reference, request),
            Operation::Probe | Operation::Create => Err(KmsError::Refused),
        }
    }

    /// Re-derives the disclosure from the canonical bytes under the gateway's
    /// own registry, requires the client's digest and disclosure to match it
    /// exactly, relays the request bound to the described public key and
    /// verifies the returned signature before answering.
    fn sign(
        &self,
        binding: &PrincipalKeyBinding,
        reference: &ProviderKeyReference,
        request: &Request<'_>,
    ) -> Result<Vec<u8>, KmsError> {
        let activity =
            layerx_intents::canonical::decode_unsigned_activity(request.canonical, &self.registry)
                .map_err(|_| KmsError::Refused)?;
        if activity.network_id() != self.network_id
            || activity.protocol_version() != self.protocol_version
        {
            return Err(KmsError::Refused);
        }
        let disclosure = layerx_crypto::disclosure::bind(request.canonical, &self.registry)
            .map_err(|_| KmsError::Refused)?;
        let admitted = validate_disclosed(request.canonical, &disclosure, &self.registry)
            .map_err(|_| KmsError::Refused)?;
        if !layerx_crypto::ct::eq_fixed(&admitted.canonical_digest, &request.digest) {
            return Err(KmsError::Integrity);
        }
        if !layerx_crypto::ct::eq(&admitted.disclosure, request.disclosure) {
            return Err(KmsError::Refused);
        }
        let description = self.provider.describe_key(binding, reference)?;
        if !layerx_crypto::ct::eq_fixed(&description.binding_digest(), &binding.digest()) {
            return Err(KmsError::Integrity);
        }
        let public_key = description.public_key();
        let signature = block_on(
            self.provider.sign(
                binding,
                reference,
                ProviderSignRequest::new(
                    request.canonical,
                    &disclosure,
                    &self.registry,
                    public_key,
                ),
            ),
            self.limits.deadline,
        )?
        .map_err(custody_refusal)?;
        layerx_crypto::ed25519::verify_digest(&public_key, &signature, &admitted.canonical_digest)
            .map_err(|_| KmsError::Integrity)?;
        Ok(signature.to_vec())
    }
}

fn describe_response(
    binding: &PrincipalKeyBinding,
    description: &ProviderKeyDescription,
) -> Result<Vec<u8>, KmsError> {
    if !layerx_crypto::ct::eq_fixed(&description.binding_digest(), &binding.digest()) {
        return Err(KmsError::Integrity);
    }
    let reference = description.reference().as_bytes();
    let length = u32::try_from(reference.len()).map_err(|_| KmsError::InvalidResponse)?;
    let mut out = Vec::with_capacity(4 + reference.len() + 65);
    out.extend_from_slice(&length.to_be_bytes());
    out.extend_from_slice(reference);
    out.extend_from_slice(&description.public_key());
    out.extend_from_slice(&description.binding_digest());
    out.push(match description.rotation() {
        RotationState::Stable => 0,
        RotationState::InProgress => 1,
        RotationState::Failed => 2,
        RotationState::Unknown => 3,
    });
    Ok(out)
}

/// One decoded request. Every field borrows the zeroizing request frame; the
/// copied fixed-width fields are wiped when the request is released.
struct Request<'a> {
    operation: Operation,
    binding: [u8; 32],
    network: u32,
    class: u8,
    reference: &'a [u8],
    expected: [u8; 32],
    digest: [u8; 32],
    canonical: &'a [u8],
    disclosure: &'a [u8],
}

impl Drop for Request<'_> {
    fn drop(&mut self) {
        self.binding.zeroize();
        self.expected.zeroize();
        self.digest.zeroize();
    }
}

fn decode<'a>(
    frame: &'a [u8],
    provider_reference: &str,
    network: u32,
) -> Result<Request<'a>, KmsError> {
    if frame.len() > GATEWAY_FRAME_LIMIT {
        return Err(KmsError::Refused);
    }
    let mut reader = Reader {
        bytes: frame,
        at: 0,
    };
    if reader.take(4)? != MAGIC {
        return Err(KmsError::Refused);
    }
    let version = u16::from_be_bytes(reader.fixed()?);
    let operation = Operation::from_wire(version, reader.byte()?).ok_or(KmsError::Refused)?;
    if reader.blob(KEY_REFERENCE_LIMIT)? != provider_reference.as_bytes() {
        return Err(KmsError::Refused);
    }
    let mut request = Request {
        operation,
        binding: [0; 32],
        network: 0,
        class: 0,
        reference: &[],
        expected: [0; 32],
        digest: [0; 32],
        canonical: &[],
        disclosure: &[],
    };
    if operation != Operation::Probe {
        request.binding = reader.fixed()?;
        request.network = u32::from_be_bytes(reader.fixed()?);
        request.class = reader.byte()?;
        request.reference = reader.blob(PROVIDER_REFERENCE_LIMIT)?;
        if request.binding == [0; 32]
            || request.network != network
            || !matches!(request.class, 1 | 2)
            || (operation == Operation::Create) != request.reference.is_empty()
        {
            return Err(KmsError::Refused);
        }
    }
    if operation == Operation::RotateIfCurrent {
        request.expected = reader.fixed()?;
        if request.expected == [0; 32] {
            return Err(KmsError::Refused);
        }
    }
    if operation == Operation::Sign {
        request.digest = reader.fixed()?;
        request.canonical = reader.blob(GATEWAY_FRAME_LIMIT)?;
        request.disclosure = reader.blob(GATEWAY_FRAME_LIMIT)?;
        if request.canonical.is_empty() || request.disclosure.is_empty() {
            return Err(KmsError::Refused);
        }
    }
    if reader.at != frame.len() {
        return Err(KmsError::Refused);
    }
    Ok(request)
}

struct Reader<'a> {
    bytes: &'a [u8],
    at: usize,
}

impl<'a> Reader<'a> {
    fn take(&mut self, count: usize) -> Result<&'a [u8], KmsError> {
        let end = self.at.checked_add(count).ok_or(KmsError::Refused)?;
        let value = self.bytes.get(self.at..end).ok_or(KmsError::Refused)?;
        self.at = end;
        Ok(value)
    }

    fn fixed<const N: usize>(&mut self) -> Result<[u8; N], KmsError> {
        self.take(N)?.try_into().map_err(|_| KmsError::Refused)
    }

    fn byte(&mut self) -> Result<u8, KmsError> {
        Ok(self.fixed::<1>()?[0])
    }

    fn blob(&mut self, maximum: usize) -> Result<&'a [u8], KmsError> {
        let length =
            usize::try_from(u32::from_be_bytes(self.fixed()?)).map_err(|_| KmsError::Refused)?;
        if length > maximum {
            return Err(KmsError::Refused);
        }
        self.take(length)
    }
}

/// LXKP status byte for a refusal; every provider fault the client cannot
/// correct is reported as unavailable so callers fail closed.
const fn status(error: KmsError) -> u8 {
    match error {
        KmsError::Refused
        | KmsError::InvalidReference
        | KmsError::InvalidEnvelope
        | KmsError::DevelopmentOnly => 1,
        KmsError::KeyNotFound => 2,
        KmsError::Conflict => 3,
        KmsError::Unavailable
        | KmsError::Timeout
        | KmsError::Authentication
        | KmsError::InvalidConfiguration
        | KmsError::InvalidResponse => 4,
        KmsError::Integrity => 5,
        KmsError::SelfCustodied => 6,
    }
}

fn custody_refusal(error: CustodyError) -> KmsError {
    match error {
        CustodyError::Kms(error) => error,
        CustodyError::Sign(layerx_crypto::signer::SignError::ReturnedSignatureInvalid) => {
            KmsError::Integrity
        }
        _ => KmsError::Refused,
    }
}

struct ThreadWaker(std::thread::Thread);

impl Wake for ThreadWaker {
    fn wake(self: Arc<Self>) {
        self.0.unpark();
    }

    fn wake_by_ref(self: &Arc<Self>) {
        self.0.unpark();
    }
}

/// Drives one provider future on the connection worker within `deadline`.
fn block_on<F: Future>(future: F, deadline: Duration) -> Result<F::Output, KmsError> {
    let started = Instant::now();
    let mut future = std::pin::pin!(future);
    let waker = Waker::from(Arc::new(ThreadWaker(std::thread::current())));
    let mut context = Context::from_waker(&waker);
    loop {
        if let Poll::Ready(output) = future.as_mut().poll(&mut context) {
            return Ok(output);
        }
        let remaining = deadline
            .checked_sub(started.elapsed())
            .filter(|remaining| !remaining.is_zero())
            .ok_or(KmsError::Timeout)?;
        std::thread::park_timeout(remaining);
    }
}
