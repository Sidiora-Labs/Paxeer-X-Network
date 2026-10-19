//! Bounded Unix-socket and mutually authenticated TLS transports.

use std::collections::VecDeque;
use std::io::ErrorKind;
use std::net::{SocketAddr, TcpStream};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Duration;

use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName};
use rustls::{ClientConfig, ClientConnection, RootCertStore, StreamOwned};

use super::framing::{read_frame, write_frame};

/// Canonical-frame structural failure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FrameViolation {
    TruncatedPrefix,
    TruncatedBody,
    Oversized { declared: u32, maximum: usize },
    LengthOverflow,
}

/// Boundary-I/O failures, kept separate from core and verification errors.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportError {
    ConnectionFailure(ErrorKind),
    Deadline,
    Frame(FrameViolation),
    PeerShutdown,
    ConnectionLimit,
    StreamLimit,
    Backpressure,
    TlsConfiguration,
}

/// Required finite limits for every LNI connection.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Limits {
    pub maximum_frame_bytes: usize,
    pub maximum_connections: usize,
    pub maximum_streams: usize,
    pub maximum_queued_bytes: usize,
    pub deadline: Duration,
}

impl Limits {
    /// Validates that every required limit is non-zero.
    ///
    /// # Errors
    ///
    /// Refuses a configuration that would disable a boundary limit.
    pub const fn validate(self) -> Result<Self, TransportError> {
        if self.maximum_frame_bytes == 0
            || self.maximum_connections == 0
            || self.maximum_streams == 0
            || self.maximum_queued_bytes == 0
            || self.deadline.is_zero()
        {
            return Err(TransportError::ConnectionLimit);
        }
        Ok(self)
    }
}

/// Shared gate enforcing the configured number of live core connections.
#[derive(Clone, Debug)]
pub struct ConnectionGate {
    active: Arc<AtomicUsize>,
    maximum: usize,
}

impl ConnectionGate {
    #[must_use]
    pub fn new(maximum: usize) -> Self {
        Self {
            active: Arc::new(AtomicUsize::new(0)),
            maximum,
        }
    }

    /// Acquires one live-connection slot.
    ///
    /// # Errors
    ///
    /// Returns `ConnectionLimit` without opening a socket when the limit is
    /// already reached.
    pub fn acquire(&self) -> Result<ConnectionPermit, TransportError> {
        let result = self
            .active
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
                (active < self.maximum).then_some(active + 1)
            });
        result
            .map(|_| ConnectionPermit {
                active: Arc::clone(&self.active),
            })
            .map_err(|_| TransportError::ConnectionLimit)
    }

    #[must_use]
    pub fn active(&self) -> usize {
        self.active.load(Ordering::Acquire)
    }
}

#[derive(Debug)]
pub struct ConnectionPermit {
    active: Arc<AtomicUsize>,
}

impl Drop for ConnectionPermit {
    fn drop(&mut self) {
        let previous = self.active.fetch_sub(1, Ordering::AcqRel);
        debug_assert!(previous > 0);
    }
}

/// One framed transport with identical byte semantics across implementations.
pub trait FrameTransport {
    /// Sends one canonical envelope as a bounded frame.
    ///
    /// # Errors
    ///
    /// Returns the exact transport, deadline, shutdown, or frame failure.
    fn send(&mut self, canonical_envelope: &[u8]) -> Result<(), TransportError>;

    /// Receives one complete canonical envelope.
    ///
    /// # Errors
    ///
    /// Returns the exact transport, deadline, shutdown, or frame failure.
    fn receive(&mut self) -> Result<Vec<u8>, TransportError>;
}

/// Default local Unix-domain-socket transport.
#[derive(Debug)]
pub struct Uds {
    stream: UnixStream,
    maximum_frame_bytes: usize,
    _permit: ConnectionPermit,
}

impl Uds {
    /// Opens the default local transport with finite read and write deadlines.
    ///
    /// # Errors
    ///
    /// Returns typed connection, limit, or deadline-configuration failures.
    pub fn connect(
        path: &Path,
        gate: &ConnectionGate,
        limits: Limits,
    ) -> Result<Self, TransportError> {
        let limits = limits.validate()?;
        let permit = gate.acquire()?;
        let stream = UnixStream::connect(path).map_err(|error| map_connect(&error))?;
        stream
            .set_read_timeout(Some(limits.deadline))
            .map_err(|error| map_connect(&error))?;
        stream
            .set_write_timeout(Some(limits.deadline))
            .map_err(|error| map_connect(&error))?;
        Ok(Self {
            stream,
            maximum_frame_bytes: limits.maximum_frame_bytes,
            _permit: permit,
        })
    }
}

impl FrameTransport for Uds {
    fn send(&mut self, canonical_envelope: &[u8]) -> Result<(), TransportError> {
        write_frame(
            &mut self.stream,
            canonical_envelope,
            self.maximum_frame_bytes,
        )
    }

    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        read_frame(&mut self.stream, self.maximum_frame_bytes)
    }
}

/// Rustls client configuration that always presents a client certificate.
#[derive(Clone)]
pub struct MutualTlsConfig {
    inner: Arc<ClientConfig>,
}

impl MutualTlsConfig {
    /// Builds a server-authenticated configuration with mandatory local client
    /// certificate material.
    ///
    /// # Errors
    ///
    /// Refuses an empty certificate chain or invalid certificate/key pair.
    pub fn new(
        roots: RootCertStore,
        client_certificates: Vec<CertificateDer<'static>>,
        client_private_key: PrivateKeyDer<'static>,
    ) -> Result<Self, TransportError> {
        if client_certificates.is_empty() || roots.is_empty() {
            return Err(TransportError::TlsConfiguration);
        }
        let inner = ClientConfig::builder()
            .with_root_certificates(roots)
            .with_client_auth_cert(client_certificates, client_private_key)
            .map_err(|_| TransportError::TlsConfiguration)?;
        Ok(Self {
            inner: Arc::new(inner),
        })
    }
}

/// Remote mutual-TLS TCP transport.
pub struct Tls {
    stream: StreamOwned<ClientConnection, TcpStream>,
    maximum_frame_bytes: usize,
    _permit: ConnectionPermit,
}

impl Tls {
    /// Opens a remote transport and starts a Rustls handshake using the
    /// mandatory client-certificate configuration.
    ///
    /// # Errors
    ///
    /// Returns typed limit, TCP connection, TLS-configuration, or I/O failure.
    pub fn connect(
        address: SocketAddr,
        server_name: ServerName<'static>,
        configuration: &MutualTlsConfig,
        gate: &ConnectionGate,
        limits: Limits,
    ) -> Result<Self, TransportError> {
        let limits = limits.validate()?;
        let permit = gate.acquire()?;
        let tcp = TcpStream::connect_timeout(&address, limits.deadline)
            .map_err(|error| map_connect(&error))?;
        tcp.set_read_timeout(Some(limits.deadline))
            .map_err(|error| map_connect(&error))?;
        tcp.set_write_timeout(Some(limits.deadline))
            .map_err(|error| map_connect(&error))?;
        let connection = ClientConnection::new(Arc::clone(&configuration.inner), server_name)
            .map_err(|_| TransportError::TlsConfiguration)?;
        Ok(Self {
            stream: StreamOwned::new(connection, tcp),
            maximum_frame_bytes: limits.maximum_frame_bytes,
            _permit: permit,
        })
    }
}

impl FrameTransport for Tls {
    fn send(&mut self, canonical_envelope: &[u8]) -> Result<(), TransportError> {
        write_frame(
            &mut self.stream,
            canonical_envelope,
            self.maximum_frame_bytes,
        )
    }

    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        read_frame(&mut self.stream, self.maximum_frame_bytes)
    }
}

/// Scheduling class for bounded outbound work.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TrafficClass {
    Submission,
    ReceiptResolution,
    InteractiveRead,
    BulkStream,
}

/// One queued canonical frame associated with a logical stream.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct OutboundFrame {
    pub stream_id: u64,
    pub class: TrafficClass,
    pub bytes: Vec<u8>,
}

/// Bounded stream scheduler that always drains submissions before bulk work.
#[derive(Debug)]
pub struct Multiplexer {
    queues: [VecDeque<OutboundFrame>; 4],
    streams: Vec<u64>,
    queued_bytes: usize,
    limits: Limits,
}

impl Multiplexer {
    /// Creates a scheduler only from explicit finite limits.
    ///
    /// # Errors
    ///
    /// Refuses disabled limits.
    pub fn new(limits: Limits) -> Result<Self, TransportError> {
        Ok(Self {
            queues: std::array::from_fn(|_| VecDeque::new()),
            streams: Vec::new(),
            queued_bytes: 0,
            limits: limits.validate()?,
        })
    }

    /// Adds a frame without exceeding stream, frame, or queue bounds.
    ///
    /// # Errors
    ///
    /// Returns explicit backpressure instead of buffering beyond the bound.
    pub fn enqueue(&mut self, frame: OutboundFrame) -> Result<(), TransportError> {
        if frame.bytes.len() > self.limits.maximum_frame_bytes {
            let declared = u32::try_from(frame.bytes.len()).unwrap_or(u32::MAX);
            return Err(TransportError::Frame(FrameViolation::Oversized {
                declared,
                maximum: self.limits.maximum_frame_bytes,
            }));
        }
        let new_stream = !self.streams.contains(&frame.stream_id);
        if new_stream && self.streams.len() >= self.limits.maximum_streams {
            return Err(TransportError::StreamLimit);
        }
        let next_bytes = self
            .queued_bytes
            .checked_add(frame.bytes.len())
            .ok_or(TransportError::Backpressure)?;
        if next_bytes > self.limits.maximum_queued_bytes {
            return Err(TransportError::Backpressure);
        }
        if new_stream {
            self.streams.push(frame.stream_id);
        }
        self.queued_bytes = next_bytes;
        self.queues[class_index(frame.class)].push_back(frame);
        Ok(())
    }

    /// Pops the highest-priority frame while preserving order within a class.
    pub fn pop_next(&mut self) -> Option<OutboundFrame> {
        let frame = self.queues.iter_mut().find_map(VecDeque::pop_front)?;
        self.queued_bytes = self.queued_bytes.saturating_sub(frame.bytes.len());
        if !self
            .queues
            .iter()
            .flatten()
            .any(|queued| queued.stream_id == frame.stream_id)
        {
            self.streams.retain(|stream| *stream != frame.stream_id);
        }
        Some(frame)
    }

    #[must_use]
    pub const fn queued_bytes(&self) -> usize {
        self.queued_bytes
    }
}

const fn class_index(class: TrafficClass) -> usize {
    match class {
        TrafficClass::Submission => 0,
        TrafficClass::ReceiptResolution => 1,
        TrafficClass::InteractiveRead => 2,
        TrafficClass::BulkStream => 3,
    }
}

fn map_connect(error: &std::io::Error) -> TransportError {
    match error.kind() {
        ErrorKind::TimedOut | ErrorKind::WouldBlock => TransportError::Deadline,
        kind => TransportError::ConnectionFailure(kind),
    }
}
