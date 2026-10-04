//! Production HTTPS+JSON boundary for the versioned human-api contract.

pub mod agent_creation;
pub mod agent_runtime;
pub mod backend;
mod component;
mod component_protocol;
mod executor;
mod http;
pub mod identity;
mod identity_dispatch;
mod identity_services;
mod limits;
pub mod movement_provider;
mod privileged;
mod projection;
pub mod production_auth;
pub mod production_components;
mod production_reads;
pub mod schema;
pub(crate) mod stream_journal;

use std::io;
use std::net::{SocketAddr, TcpListener};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use layerx_client::lni::transport::ConnectionGate;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::{ServerConfig, ServerConnection, StreamOwned};
use zeroize::Zeroize;

pub use backend::{
    default_component_limits, ApiFailure, BackendResponse, BearerCredentials, ComponentState,
    HumanApiComponents, Readiness, RequestContext, ScopedRequest, SessionCredentials,
    UnixComponents,
};
pub use component::{
    BoundHumanComponentServer, ComponentMaintenance, ComponentServerConfig, ComponentServerError,
    ComponentShutdown, HumanComponentServer,
};
pub(crate) use executor::poll_once_ready;
pub use http::{HttpConfig, Router};
pub use identity::IdentityProjector;
pub use identity_dispatch::{
    IdentityDispatchError, IdentityProviderConfig, RemoteIdentityProvider,
};
pub use identity_services::{IdentityServices, ProvisionedAccount, ProvisionedAccounts};
pub use limits::PrincipalLimits;
pub use privileged::{
    AuthorizationGrantPolicy, AuthorizedSession, ComponentOperationRequest,
    PrivilegedHumanComponents, PrivilegedHumanServices,
};
pub use production_components::{ProductionComponents, ProductionComponentsConfig};

/// The listener the deployment selects: TLS terminated in the service, or plain
/// HTTP behind a proxy that terminates public TLS.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Listener {
    Tls,
    Plain,
}

impl Listener {
    /// Selects the listener from `LAYERX_HUMAN_LISTENER` and the two TLS material paths.
    ///
    /// # Errors
    ///
    /// Refuses an unknown mode by name and TLS material supplied to a plain listener by name.
    pub fn parse(
        mode: Option<&str>,
        certificate_path: Option<&str>,
        private_key_path: Option<&str>,
    ) -> Result<Self, String> {
        match mode {
            None | Some("tls") => Ok(Self::Tls),
            Some("plain") => {
                for (name, value) in [
                    ("LAYERX_HUMAN_TLS_CERT_DER", certificate_path),
                    ("LAYERX_HUMAN_TLS_KEY_DER", private_key_path),
                ] {
                    if value.is_some_and(|value| !value.is_empty()) {
                        return Err(format!("{name} is set with LAYERX_HUMAN_LISTENER plain"));
                    }
                }
                Ok(Self::Plain)
            }
            Some(_) => Err("LAYERX_HUMAN_LISTENER must be tls or plain".to_owned()),
        }
    }
}

/// Explicit finite plain HTTP listener configuration for deployment behind a
/// proxy that terminates public TLS.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct PlainConfig {
    pub bind: SocketAddr,
    pub maximum_connections: usize,
    pub io_deadline: Duration,
}

/// Runnable plain HTTP server which accepts one bounded request per connection.
pub struct PlainServer<B: HumanApiComponents> {
    router: Arc<Router<B>>,
    configuration: PlainConfig,
}

impl<B: HumanApiComponents> PlainServer<B> {
    #[must_use]
    pub const fn new(router: Arc<Router<B>>, configuration: PlainConfig) -> Self {
        Self {
            router,
            configuration,
        }
    }

    /// Binds the configured address without serving yet.
    ///
    /// # Errors
    ///
    /// Refuses disabled bounds and propagates the bind failure.
    pub fn bind(self) -> Result<BoundPlainServer<B>, ServerError> {
        if self.configuration.maximum_connections == 0 || self.configuration.io_deadline.is_zero() {
            return Err(ServerError::Configuration(ApiFailure::unavailable()));
        }
        let listener = TcpListener::bind(self.configuration.bind).map_err(ServerError::Io)?;
        Ok(BoundPlainServer {
            router: self.router,
            listener,
            configuration: self.configuration,
        })
    }
}

/// A plain HTTP server holding its bound listener.
pub struct BoundPlainServer<B: HumanApiComponents> {
    router: Arc<Router<B>>,
    listener: TcpListener,
    configuration: PlainConfig,
}

impl<B: HumanApiComponents> BoundPlainServer<B> {
    /// # Errors
    ///
    /// Propagates the listener's address lookup failure.
    pub fn local_addr(&self) -> io::Result<SocketAddr> {
        self.listener.local_addr()
    }

    /// Serves until the listener fails.
    ///
    /// # Errors
    ///
    /// Propagates listener failures. Individual connection failures are isolated
    /// to their bounded worker.
    pub fn serve(self) -> Result<(), ServerError> {
        let gate = ConnectionGate::new(self.configuration.maximum_connections);
        loop {
            let (mut tcp, peer) = self.listener.accept().map_err(ServerError::Io)?;
            let Ok(permit) = gate.acquire() else {
                continue;
            };
            let router = Arc::clone(&self.router);
            let deadline = self.configuration.io_deadline;
            thread::spawn(move || {
                let _permit = permit;
                if tcp.set_read_timeout(Some(deadline)).is_err()
                    || tcp.set_write_timeout(Some(deadline)).is_err()
                {
                    return;
                }
                let public_rate_key = public_rate_key(peer);
                let watched = tcp.try_clone();
                let _ = router.serve_one_with_disconnect(&mut tcp, &public_rate_key, || {
                    StreamCancellation::watch(watched?).map(Some)
                });
            });
        }
    }
}

/// Explicit finite HTTPS listener configuration.
pub struct HttpsConfig {
    pub bind: SocketAddr,
    pub certificate_der: Vec<u8>,
    pub private_key_der: Vec<u8>,
    pub maximum_connections: usize,
    pub io_deadline: Duration,
}

impl HttpsConfig {
    fn rustls(&mut self) -> Result<Arc<ServerConfig>, ApiFailure> {
        if self.certificate_der.is_empty()
            || self.private_key_der.is_empty()
            || self.maximum_connections == 0
            || self.io_deadline.is_zero()
        {
            return Err(ApiFailure::unavailable());
        }
        let private_key_bytes = std::mem::take(&mut self.private_key_der);
        let private_key =
            PrivateKeyDer::try_from(private_key_bytes).map_err(|_| ApiFailure::unavailable())?;
        let configuration = ServerConfig::builder()
            .with_no_client_auth()
            .with_single_cert(
                vec![CertificateDer::from(self.certificate_der.clone())],
                private_key,
            )
            .map_err(|_| ApiFailure::unavailable())?;
        Ok(Arc::new(configuration))
    }
}

impl Drop for HttpsConfig {
    fn drop(&mut self) {
        self.private_key_der.zeroize();
    }
}

/// Runnable HTTPS server which accepts one bounded request per TLS connection.
pub struct HttpsServer<B: HumanApiComponents> {
    router: Arc<Router<B>>,
    configuration: HttpsConfig,
}

impl<B: HumanApiComponents> HttpsServer<B> {
    #[must_use]
    pub const fn new(router: Arc<Router<B>>, configuration: HttpsConfig) -> Self {
        Self {
            router,
            configuration,
        }
    }

    /// Binds the configured HTTPS address and serves until the listener fails.
    ///
    /// # Errors
    ///
    /// Refuses invalid TLS material and propagates listener failures. Individual
    /// connection failures are isolated to their bounded worker.
    pub fn run(mut self) -> Result<(), ServerError> {
        let tls = self
            .configuration
            .rustls()
            .map_err(ServerError::Configuration)?;
        let listener = TcpListener::bind(self.configuration.bind).map_err(ServerError::Io)?;
        let gate = ConnectionGate::new(self.configuration.maximum_connections);
        loop {
            let (tcp, peer) = listener.accept().map_err(ServerError::Io)?;
            let Ok(permit) = gate.acquire() else {
                continue;
            };
            let router = Arc::clone(&self.router);
            let tls = Arc::clone(&tls);
            let deadline = self.configuration.io_deadline;
            thread::spawn(move || {
                let _permit = permit;
                if tcp.set_read_timeout(Some(deadline)).is_err()
                    || tcp.set_write_timeout(Some(deadline)).is_err()
                {
                    return;
                }
                let Ok(connection) = ServerConnection::new(tls) else {
                    return;
                };
                let mut stream = StreamOwned::new(connection, tcp);
                let public_rate_key = public_rate_key(peer);
                let watched = stream.sock.try_clone();
                let _ = router.serve_one_with_disconnect(&mut stream, &public_rate_key, || {
                    StreamCancellation::watch(watched?).map(Some)
                });
            });
        }
    }
}

fn public_rate_key(peer: SocketAddr) -> String {
    format!("bootstrap:{}", peer.ip())
}

#[derive(Debug)]
pub enum ServerError {
    Configuration(ApiFailure),
    Io(io::Error),
}

impl std::fmt::Display for ServerError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Configuration(_) => formatter.write_str("invalid human service configuration"),
            Self::Io(error) => write!(formatter, "human service listener failed: {error}"),
        }
    }
}

impl std::error::Error for ServerError {}

pub(super) struct StreamCancellation {
    cancelled: Arc<std::sync::atomic::AtomicBool>,
    stop: Arc<std::sync::atomic::AtomicBool>,
    wake: std::os::unix::net::UnixStream,
    deadline: Arc<std::sync::Mutex<Option<std::time::Instant>>>,
    worker: Option<thread::JoinHandle<()>>,
}

impl StreamCancellation {
    pub(super) fn watch<S: std::os::fd::AsFd + Send + 'static>(socket: S) -> io::Result<Self> {
        use rustix::event::{poll, PollFd, PollFlags};
        use std::sync::atomic::{AtomicBool, Ordering};
        let (wake, waiter) = std::os::unix::net::UnixStream::pair()?;
        let cancelled = Arc::new(AtomicBool::new(false));
        let stop = Arc::new(AtomicBool::new(false));
        let deadline = Arc::new(std::sync::Mutex::new(None::<std::time::Instant>));
        let thread_deadline = Arc::clone(&deadline);
        let thread_cancelled = Arc::clone(&cancelled);
        let thread_stop = Arc::clone(&stop);
        let worker = thread::Builder::new()
            .name("human-stream-disconnect".to_owned())
            .spawn(move || {
                let mut descriptors = [
                    PollFd::new(&socket, PollFlags::RDHUP),
                    PollFd::new(&waiter, PollFlags::IN),
                ];
                loop {
                    if thread_stop.load(Ordering::Acquire) {
                        return;
                    }
                    let deadline = match thread_deadline.lock() {
                        Ok(deadline) => *deadline,
                        Err(_) => {
                            thread_cancelled.store(true, Ordering::Release);
                            let _ = rustix::net::shutdown(&socket, rustix::net::Shutdown::Both);
                            stream_journal::changed();
                            return;
                        }
                    };
                    let remaining = deadline.map(|deadline| {
                        deadline.saturating_duration_since(std::time::Instant::now())
                    });
                    if remaining.is_some_and(|remaining| remaining.is_zero()) {
                        thread_cancelled.store(true, Ordering::Release);
                        let _ = rustix::net::shutdown(&socket, rustix::net::Shutdown::Both);
                        stream_journal::changed();
                        return;
                    }
                    let timeout = remaining.map(|remaining| rustix::event::Timespec {
                        tv_sec: i64::try_from(remaining.as_secs()).unwrap_or(i64::MAX),
                        tv_nsec: i64::from(remaining.subsec_nanos()),
                    });
                    match poll(&mut descriptors, timeout.as_ref()) {
                        Ok(_) => {
                            if thread_stop.load(Ordering::Acquire) {
                                return;
                            }
                            if !descriptors[1].revents().is_empty() {
                                let mut byte = [0_u8; 1];
                                let mut input = &waiter;
                                if std::io::Read::read(&mut input, &mut byte).unwrap_or(0) == 0 {
                                    return;
                                }
                                continue;
                            }
                            if descriptors[0].revents().intersects(
                                PollFlags::RDHUP
                                    | PollFlags::HUP
                                    | PollFlags::ERR
                                    | PollFlags::NVAL,
                            ) {
                                thread_cancelled.store(true, Ordering::Release);
                                stream_journal::changed();
                                return;
                            }
                        }
                        Err(rustix::io::Errno::INTR) => continue,
                        Err(_) => {
                            thread_cancelled.store(true, Ordering::Release);
                            stream_journal::changed();
                            return;
                        }
                    }
                }
            })?;
        Ok(Self {
            cancelled,
            stop,
            wake,
            deadline,
            worker: Some(worker),
        })
    }

    pub(super) fn set_deadline(&self, deadline: std::time::Instant) -> io::Result<()> {
        let mut current = self
            .deadline
            .lock()
            .map_err(|_| io::Error::other("stream deadline unavailable"))?;
        if current.is_some_and(|current| deadline > current) {
            return Err(io::Error::other("stream deadline cannot extend"));
        }
        *current = Some(deadline);
        drop(current);
        let mut wake = &self.wake;
        std::io::Write::write_all(&mut wake, &[1])
    }

    pub(super) fn cancelled(&self) -> bool {
        self.cancelled.load(std::sync::atomic::Ordering::Acquire)
    }
}

impl Drop for StreamCancellation {
    fn drop(&mut self) {
        self.stop.store(true, std::sync::atomic::Ordering::Release);
        let _ = self.wake.shutdown(std::net::Shutdown::Write);
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}
