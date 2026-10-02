use std::fmt;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::Arc;

use rustls::crypto::{ring, CryptoProvider};
use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName};
use rustls::server::WebPkiClientVerifier;
use rustls::{RootCertStore, ServerConfig, ServerConnection, StreamOwned};

pub struct AgentRpcTlsPaths {
    pub cert: PathBuf,
    pub key: PathBuf,
    pub client_ca: PathBuf,
    pub peer: String,
}

pub struct AgentRpcTls {
    config: Arc<ServerConfig>,
    peer: ServerName<'static>,
    peer_text: String,
}

#[derive(Debug)]
pub enum AgentRpcTlsError {
    Io(String),
    Pem(String),
    Config(String),
    Peer,
}

impl fmt::Display for AgentRpcTlsError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(reason) => write!(formatter, "agent rpc tls io: {reason}"),
            Self::Pem(reason) => write!(formatter, "agent rpc tls pem: {reason}"),
            Self::Config(reason) => write!(formatter, "agent rpc tls config: {reason}"),
            Self::Peer => formatter.write_str("agent rpc tls peer identity refused"),
        }
    }
}

impl std::error::Error for AgentRpcTlsError {}

fn certificates(
    path: &Path,
    what: &str,
) -> Result<Vec<CertificateDer<'static>>, AgentRpcTlsError> {
    let certificates = CertificateDer::pem_file_iter(path)
        .map_err(|error| AgentRpcTlsError::Pem(format!("{what} {}: {error:?}", path.display())))?
        .collect::<Result<Vec<_>, _>>()
        .map_err(|error| AgentRpcTlsError::Pem(format!("{what} {}: {error:?}", path.display())))?;
    if certificates.is_empty() {
        return Err(AgentRpcTlsError::Pem(format!(
            "{what} {}: no certificate",
            path.display()
        )));
    }
    Ok(certificates)
}

impl AgentRpcTls {
    pub fn from_paths(paths: &AgentRpcTlsPaths) -> Result<Self, AgentRpcTlsError> {
        if paths.peer.is_empty() || paths.peer.contains('*') {
            return Err(AgentRpcTlsError::Config(
                "peer identity must be one exact DNS name".into(),
            ));
        }
        let peer = match ServerName::try_from(paths.peer.as_str()) {
            Ok(name @ ServerName::DnsName(_)) => name.to_owned(),
            _ => {
                return Err(AgentRpcTlsError::Config(
                    "peer identity must be one exact DNS name".into(),
                ))
            }
        };

        let mut roots = RootCertStore::empty();
        for certificate in certificates(&paths.client_ca, "client ca")? {
            roots
                .add(certificate)
                .map_err(|error| AgentRpcTlsError::Config(format!("client ca: {error}")))?;
        }
        let chain = certificates(&paths.cert, "server certificate")?;
        let key = PrivateKeyDer::from_pem_file(&paths.key).map_err(|error| {
            AgentRpcTlsError::Pem(format!("server key {}: {error:?}", paths.key.display()))
        })?;

        let provider: Arc<CryptoProvider> = Arc::new(ring::default_provider());
        let verifier =
            WebPkiClientVerifier::builder_with_provider(Arc::new(roots), Arc::clone(&provider))
                .build()
                .map_err(|error| AgentRpcTlsError::Config(format!("client verifier: {error:?}")))?;
        let config = ServerConfig::builder_with_provider(provider)
            .with_protocol_versions(&[&rustls::version::TLS13, &rustls::version::TLS12])
            .map_err(|error| AgentRpcTlsError::Config(format!("protocol versions: {error}")))?
            .with_client_cert_verifier(verifier)
            .with_single_cert(chain, key)
            .map_err(|error| AgentRpcTlsError::Config(format!("server identity: {error}")))?;

        Ok(Self {
            config: Arc::new(config),
            peer,
            peer_text: paths.peer.clone(),
        })
    }

    pub fn accept<S: Read + Write>(
        &self,
        mut stream: S,
    ) -> Result<StreamOwned<ServerConnection, S>, AgentRpcTlsError> {
        let mut connection = ServerConnection::new(Arc::clone(&self.config))
            .map_err(|error| AgentRpcTlsError::Config(error.to_string()))?;
        while connection.is_handshaking() {
            connection
                .complete_io(&mut stream)
                .map_err(|error| AgentRpcTlsError::Io(error.to_string()))?;
        }
        let end_entity = connection
            .peer_certificates()
            .and_then(|chain| chain.first())
            .ok_or(AgentRpcTlsError::Peer)?;
        let parsed =
            webpki::EndEntityCert::try_from(end_entity).map_err(|_| AgentRpcTlsError::Peer)?;
        parsed
            .verify_is_valid_for_subject_name(&self.peer)
            .map_err(|_| AgentRpcTlsError::Peer)?;
        if !parsed
            .valid_dns_names()
            .any(|name| name.eq_ignore_ascii_case(&self.peer_text))
        {
            return Err(AgentRpcTlsError::Peer);
        }
        Ok(StreamOwned::new(connection, stream))
    }
}

#[test]
fn absent_material_and_inexact_peer_fail_closed() {
    let missing = |peer: &str| AgentRpcTlsPaths {
        cert: PathBuf::from("/nonexistent/agent-rpc/server.pem"),
        key: PathBuf::from("/nonexistent/agent-rpc/server.key"),
        client_ca: PathBuf::from("/nonexistent/agent-rpc/client-ca.pem"),
        peer: peer.to_owned(),
    };
    for peer in ["", "*.gateway.invalid", "127.0.0.1", "not a name"] {
        assert!(matches!(
            AgentRpcTls::from_paths(&missing(peer)),
            Err(AgentRpcTlsError::Config(_))
        ));
    }
    assert!(matches!(
        AgentRpcTls::from_paths(&missing("gateway.invalid")),
        Err(AgentRpcTlsError::Pem(_))
    ));
}
