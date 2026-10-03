use std::{path::{Path, PathBuf}, time::Duration};

use layerx_programs::SourceStatus;
use rustls::pki_types::pem::PemObject;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use zeroize::Zeroizing;

use crate::ops::program::ProgramInterfaceRead;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RegistrySourceError {
    Configuration,
    ProtectedFile,
    Identity,
    Unavailable,
    Refused,
    Bounds,
    Malformed,
    Binding,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Configuration {
    version: u16,
    endpoint: String,
    server_ca_der: PathBuf,
    client_certificate_pem: PathBuf,
    client_private_key_pem: PathBuf,
    request_token_file: PathBuf,
    deadline_ms: u64,
    maximum_response_bytes: usize,
}

pub struct RegistrySourceProvider {
    agent: ureq::Agent,
    endpoint: String,
    bearer: Zeroizing<String>,
    maximum_response_bytes: usize,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct VerifiedRegistrySource {
    source: SourceStatus,
    pipeline: Option<String>,
    deployment_receipt_digest: [u8; 32],
    current_head_receipt_digest: [u8; 32],
    program: [u8; 32],
    version: u32,
    code_hash: [u8; 32],
    state_root: [u8; 32],
    observed_sequence: u64,
    observed_at: u64,
    valid_through: u64,
}

impl VerifiedRegistrySource {
    pub const fn source(&self) -> &SourceStatus { &self.source }
    pub fn pipeline(&self) -> Option<&str> { self.pipeline.as_deref() }
    pub const fn deployment_receipt_digest(&self) -> [u8; 32] { self.deployment_receipt_digest }
    pub const fn current_head_receipt_digest(&self) -> [u8; 32] { self.current_head_receipt_digest }
    pub const fn program(&self) -> [u8; 32] { self.program }
    pub const fn version(&self) -> u32 { self.version }
    pub const fn code_hash(&self) -> [u8; 32] { self.code_hash }
    pub const fn state_root(&self) -> [u8; 32] { self.state_root }
    pub const fn observed_sequence(&self) -> u64 { self.observed_sequence }
    pub const fn observed_at(&self) -> u64 { self.observed_at }
    pub const fn valid_through(&self) -> u64 { self.valid_through }
}

pub struct ProgramInterfaceWithSource {
    pub interface: ProgramInterfaceRead,
    pub source: VerifiedRegistrySource,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RegistryInterface {
    program_id: String,
    version: u32,
    code_hash: String,
    abi_version: u16,
    interface: String,
    interface_digest: String,
    deployment_receipt_digest: String,
    state_root: String,
    observed_sequence: u64,
    observed_at: u64,
    valid_through: u64,
    source: RegistrySource,
    verification: String,
}

#[derive(Deserialize)]
#[serde(tag = "status", rename_all = "lowercase", deny_unknown_fields)]
enum RegistrySource {
    Unpublished,
    Verified { source_digest: String, environment_digest: String, pipeline: String },
    Mismatch { expected_code_hash: String, reproduced_artifact_digest: String },
}

fn protected(path: &Path, maximum: usize) -> Result<Zeroizing<Vec<u8>>, RegistrySourceError> {
    crate::config::read_protected_source(path, maximum)
        .map(Zeroizing::new).map_err(|_| RegistrySourceError::ProtectedFile)
}

fn endpoint(value: &str) -> Result<String, RegistrySourceError> {
    let authority = value.strip_prefix("https://").ok_or(RegistrySourceError::Configuration)?;
    let authority = authority.strip_suffix('/').unwrap_or(authority);
    if authority.is_empty() || authority.bytes().any(|byte| byte <= b' ' || matches!(byte, b'/' | b'?' | b'#' | b'@' | b'\\')) {
        return Err(RegistrySourceError::Configuration);
    }
    let (host, port) = if let Some(rest) = authority.strip_prefix('[') {
        let (host, suffix) = rest.split_once(']').ok_or(RegistrySourceError::Configuration)?;
        let _: std::net::Ipv6Addr = host.parse().map_err(|_| RegistrySourceError::Configuration)?;
        (host, if suffix.is_empty() { None } else { Some(suffix.strip_prefix(':').ok_or(RegistrySourceError::Configuration)?) })
    } else {
        match authority.split_once(':') { Some((host, port)) => (host, Some(port)), None => (authority, None) }
    };
    rustls::pki_types::ServerName::try_from(host.to_owned()).map_err(|_| RegistrySourceError::Configuration)?;
    if let Some(port) = port {
        let parsed: u16 = port.parse().map_err(|_| RegistrySourceError::Configuration)?;
        if parsed == 0 || parsed.to_string() != port { return Err(RegistrySourceError::Configuration); }
    }
    Ok(format!("https://{authority}"))
}

impl RegistrySourceProvider {
    pub fn from_protected_config(path: &Path) -> Result<Self, RegistrySourceError> {
        let bytes = protected(path, 16 * 1024)?;
        let config: Configuration = serde_json::from_slice(&bytes).map_err(|_| RegistrySourceError::Configuration)?;
        if config.version != 1 || !(1..=60_000).contains(&config.deadline_ms)
            || !(1..=1_048_576).contains(&config.maximum_response_bytes) {
            return Err(RegistrySourceError::Configuration);
        }
        let endpoint = endpoint(&config.endpoint)?;
        let ca = protected(&config.server_ca_der, 64 * 1024)?;
        let certificates = protected(&config.client_certificate_pem, 64 * 1024)?;
        let private = protected(&config.client_private_key_pem, 64 * 1024)?;
        let token = protected(&config.request_token_file, 4096)?;
        let token = std::str::from_utf8(&token).map_err(|_| RegistrySourceError::Configuration)?;
        let token = token.strip_suffix('\n').unwrap_or(token);
        if token.len() < 32 || token.bytes().any(|byte| !(0x21..=0x7e).contains(&byte)) {
            return Err(RegistrySourceError::Configuration);
        }
        let mut roots = rustls::RootCertStore::empty();
        roots.add(rustls::pki_types::CertificateDer::from(ca.to_vec())).map_err(|_| RegistrySourceError::Identity)?;
        let chain = rustls::pki_types::CertificateDer::pem_slice_iter(&certificates)
            .collect::<Result<Vec<_>, _>>().map_err(|_| RegistrySourceError::Identity)?;
        let key = rustls::pki_types::PrivateKeyDer::from_pem_slice(&private).map_err(|_| RegistrySourceError::Identity)?;
        let provider = std::sync::Arc::new(rustls::crypto::ring::default_provider());
        rustls::ClientConfig::builder_with_provider(provider).with_safe_default_protocol_versions()
            .map_err(|_| RegistrySourceError::Identity)?.with_root_certificates(roots)
            .with_client_auth_cert(chain, key).map_err(|_| RegistrySourceError::Identity)?;
        let mut chain = Vec::new();
        for item in ureq::tls::parse_pem(&certificates) {
            match item.map_err(|_| RegistrySourceError::Identity)? {
                ureq::tls::PemItem::Certificate(value) => chain.push(value),
                _ => return Err(RegistrySourceError::Identity),
            }
        }
        let mut keys = ureq::tls::parse_pem(&private);
        let private_key = match keys.next().transpose().map_err(|_| RegistrySourceError::Identity)? {
            Some(ureq::tls::PemItem::PrivateKey(value)) => value,
            _ => return Err(RegistrySourceError::Identity),
        };
        if chain.is_empty() || keys.next().is_some() { return Err(RegistrySourceError::Identity); }
        let identity = ureq::tls::ClientCert::new_with_certs(&chain, private_key);
        let tls = ureq::tls::TlsConfig::builder().provider(ureq::tls::TlsProvider::Rustls)
            .root_certs(ureq::tls::RootCerts::new_with_certs(&[ureq::tls::Certificate::from_der(&ca).to_owned()]))
            .client_cert(Some(identity)).build();
        let agent = ureq::Agent::config_builder().tls_config(tls)
            .timeout_global(Some(Duration::from_millis(config.deadline_ms)))
            .http_status_as_error(false).max_redirects(0).build().into();
        Ok(Self { agent, endpoint, bearer: Zeroizing::new(token.to_owned()), maximum_response_bytes: config.maximum_response_bytes })
    }

    pub(crate) fn read(&self, native: &ProgramInterfaceRead, now: u64) -> Result<VerifiedRegistrySource, RegistrySourceError> {
        let path = format!("{}/v1/programs/registry/{}/interface", self.endpoint, lower_hex(&native.discovery.program.bytes()));
        let mut response = self.agent.get(path).header("Authorization", &format!("Bearer {}", self.bearer.as_str()))
            .call().map_err(|_| RegistrySourceError::Unavailable)?;
        if response.status().as_u16() != 200 { return Err(RegistrySourceError::Refused); }
        if response.body().content_length().is_some_and(|length| length > self.maximum_response_bytes as u64) {
            return Err(RegistrySourceError::Bounds);
        }
        let body = response.body_mut().with_config().limit((self.maximum_response_bytes + 1) as u64)
            .read_to_vec().map_err(|_| RegistrySourceError::Unavailable)?;
        if body.len() > self.maximum_response_bytes { return Err(RegistrySourceError::Bounds); }
        verified_source(&body, native, now)
    }
}

fn lower_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut result = String::with_capacity(bytes.len() * 2);
    for byte in bytes { result.push(char::from(DIGITS[usize::from(byte >> 4)])); result.push(char::from(DIGITS[usize::from(byte & 15)])); }
    result
}

fn digest(value: &str) -> Result<[u8; 32], RegistrySourceError> {
    if value.len() != 64 || !value.bytes().all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f')) {
        return Err(RegistrySourceError::Malformed);
    }
    let mut result = [0; 32];
    for (index, pair) in value.as_bytes().chunks_exact(2).enumerate() {
        result[index] = u8::from_str_radix(std::str::from_utf8(pair).map_err(|_| RegistrySourceError::Malformed)?, 16)
            .map_err(|_| RegistrySourceError::Malformed)?;
    }
    Ok(result)
}

fn verified_source(body: &[u8], native: &ProgramInterfaceRead, now: u64) -> Result<VerifiedRegistrySource, RegistrySourceError> {
    let record: RegistryInterface = serde_json::from_slice(body).map_err(|_| RegistrySourceError::Malformed)?;
    let head = &native.discovery;
    if record.verification != "deployment-interface-and-current-head-verified"
        || digest(&record.program_id)? != head.program.bytes() || record.version != native.version || record.version != head.version
        || digest(&record.code_hash)? != head.code_hash || record.abi_version != head.abi_version
        || record.interface != lower_hex(native.interface.canonical_encoding())
        || digest(&record.interface_digest)? != <[u8; 32]>::from(Sha256::digest(native.interface.canonical_encoding()))
        || digest(&record.state_root)? != head.state_root || record.observed_sequence != head.observed_sequence
        || record.observed_at != head.observed_at || now < head.observed_at || now > head.valid_through
        || record.valid_through < record.observed_at || now > record.valid_through {
        return Err(RegistrySourceError::Binding);
    }
    let deployment_receipt_digest = digest(&record.deployment_receipt_digest)?;
    if deployment_receipt_digest == [0; 32] { return Err(RegistrySourceError::Binding); }
    let (source, pipeline) = match record.source {
        RegistrySource::Unpublished => (SourceStatus::Unpublished, None),
        RegistrySource::Verified { source_digest, environment_digest, pipeline } => {
            if pipeline != layerx_programs::programs_source_verification() { return Err(RegistrySourceError::Binding); }
            (SourceStatus::Verified { source_digest: digest(&source_digest)?, environment_digest: digest(&environment_digest)? }, Some(pipeline))
        }
        RegistrySource::Mismatch { expected_code_hash, reproduced_artifact_digest } => {
            let expected = digest(&expected_code_hash)?;
            let reproduced = digest(&reproduced_artifact_digest)?;
            if expected != head.code_hash || reproduced == expected { return Err(RegistrySourceError::Binding); }
            (SourceStatus::Mismatch { expected, reproduced }, None)
        }
    };
    Ok(VerifiedRegistrySource { source, pipeline, deployment_receipt_digest, current_head_receipt_digest: head.receipt_digest,
        program: head.program.bytes(), version: head.version, code_hash: head.code_hash, state_root: head.state_root,
        observed_sequence: head.observed_sequence, observed_at: head.observed_at, valid_through: head.valid_through.min(record.valid_through) })
}
