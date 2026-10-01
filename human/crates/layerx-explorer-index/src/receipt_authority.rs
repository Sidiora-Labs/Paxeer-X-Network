use layerx_client::handover::{HistoryError, SequencerHistory};
use layerx_platform_authority::{
    authorized_batch_by_activity, hex, parse_replica_evidence, receipt_locator, AuthorityFacts,
    EvidenceRefusal,
};
use std::io::Read;
use std::time::Duration;
use ureq::tls::{Certificate, RootCerts, TlsConfig, TlsProvider};
use zeroize::Zeroizing;

pub const MAX_RESPONSE_BYTES: usize = 256 * 1024;
const CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
const READ_TIMEOUT: Duration = Duration::from_secs(8);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ConfigurationError {
    Endpoint,
    SameEndpoint,
    Bearer,
    Certificate,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Refusal {
    History(HistoryError),
    Evidence(EvidenceRefusal),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ReadOutcome {
    Verified(AuthorityFacts),
    NotYetAuthorised,
    Unavailable,
    Malformed,
    Refused(Refusal),
}

pub struct ReceiptAuthorityReader {
    endpoint: String,
    bearer: Zeroizing<String>,
    replica_id: [u8; 32],
    agent: ureq::Agent,
}

fn endpoint(value: &str) -> Result<String, ConfigurationError> {
    let uri: ureq::http::Uri = value.parse().map_err(|_| ConfigurationError::Endpoint)?;
    let authority = uri.authority().ok_or(ConfigurationError::Endpoint)?;
    if uri.scheme_str() != Some("https")
        || authority.as_str().contains('@')
        || uri.host().is_none()
        || uri.path() != "/" && !uri.path().is_empty()
        || uri.query().is_some()
        || value.contains('#')
    {
        return Err(ConfigurationError::Endpoint);
    }
    Ok(format!(
        "https://{}:{}",
        uri.host().unwrap().to_ascii_lowercase(),
        uri.port_u16().unwrap_or(443)
    ))
}

impl ReceiptAuthorityReader {
    pub fn new(
        authority_endpoint: &str,
        node_endpoint: &str,
        bearer: String,
        replica_id: [u8; 32],
        ca_der: &[u8],
    ) -> Result<Self, ConfigurationError> {
        let bearer = Zeroizing::new(bearer);
        let authority_endpoint = endpoint(authority_endpoint)?;
        if authority_endpoint == endpoint(node_endpoint)? {
            return Err(ConfigurationError::SameEndpoint);
        }
        if bearer.is_empty()
            || bearer.len() > 4096
            || bearer.bytes().any(|b| !(0x21..=0x7e).contains(&b))
        {
            return Err(ConfigurationError::Bearer);
        }
        if ca_der.is_empty()
            || ca_der.len() > 64 * 1024
            || native_tls::Certificate::from_der(ca_der).is_err()
        {
            return Err(ConfigurationError::Certificate);
        }
        let tls = TlsConfig::builder()
            .provider(TlsProvider::Rustls)
            .root_certs(RootCerts::new_with_certs(&[
                Certificate::from_der(ca_der).to_owned()
            ]))
            .build();
        let agent = ureq::Agent::config_builder()
            .tls_config(tls)
            .timeout_connect(Some(CONNECT_TIMEOUT))
            .timeout_recv_response(Some(READ_TIMEOUT))
            .timeout_recv_body(Some(READ_TIMEOUT))
            .timeout_global(Some(CONNECT_TIMEOUT + READ_TIMEOUT))
            .max_redirects(0)
            .http_status_as_error(false)
            .build()
            .into();
        Ok(Self {
            endpoint: authority_endpoint,
            bearer,
            replica_id,
            agent,
        })
    }

    pub fn read(&self, canonical_receipt: &[u8], history: &SequencerHistory) -> ReadOutcome {
        let locator = match receipt_locator(canonical_receipt) {
            Ok(value) => value,
            Err(error) => return ReadOutcome::Refused(Refusal::Evidence(error)),
        };
        let url = format!(
            "{}/v1/batches/{}/receipt-authority?receipt_digest={}",
            self.endpoint,
            hex::encode(&locator.batch_id),
            hex::encode(&locator.receipt_digest)
        );
        let authorization = Zeroizing::new(format!("Bearer {}", self.bearer.as_str()));
        let mut response = match self
            .agent
            .get(&url)
            .header("Authorization", authorization.as_str())
            .call()
        {
            Ok(response) => response,
            Err(_) => return ReadOutcome::Unavailable,
        };
        match response.status().as_u16() {
            200 => {}
            404 => return ReadOutcome::NotYetAuthorised,
            503 => return ReadOutcome::Unavailable,
            _ => return ReadOutcome::Malformed,
        }
        let mut document = Vec::new();
        if response
            .body_mut()
            .as_reader()
            .take((MAX_RESPONSE_BYTES + 1) as u64)
            .read_to_end(&mut document)
            .is_err()
        {
            return ReadOutcome::Unavailable;
        }
        self.verify_document(canonical_receipt, history, &document)
    }

    pub fn verify_document(
        &self,
        canonical_receipt: &[u8],
        history: &SequencerHistory,
        document: &[u8],
    ) -> ReadOutcome {
        if document.len() > MAX_RESPONSE_BYTES {
            return ReadOutcome::Malformed;
        }
        let document_value: serde_json::Value = match serde_json::from_slice(document) {
            Ok(value) => value,
            Err(_) => return ReadOutcome::Malformed,
        };
        let locator = match receipt_locator(canonical_receipt) {
            Ok(value) => value,
            Err(error) => return ReadOutcome::Refused(Refusal::Evidence(error)),
        };
        let receipt = match layerx_wire::receipt::decode(canonical_receipt) {
            Ok(value) => value,
            Err(_) => {
                return ReadOutcome::Refused(Refusal::Evidence(EvidenceRefusal::ReceiptDecode))
            }
        };
        let Some(protocol) = receipt.protocol() else {
            return ReadOutcome::Refused(Refusal::Evidence(EvidenceRefusal::ReceiptShape));
        };
        let authorization = match history.authorization_for_sequence(protocol.global_sequence()) {
            Ok(value) => value,
            Err(error) => return ReadOutcome::Refused(Refusal::History(error)),
        };
        let header = document_value
            .pointer("/batch_evidence/header_hex")
            .and_then(|v| v.as_str())
            .and_then(|v| hex::decode(v).ok());
        let signature = document_value
            .pointer("/batch_evidence/header_signature")
            .and_then(|v| v.as_str())
            .and_then(|v| hex::decode(v).ok())
            .and_then(|v| <[u8; 64]>::try_from(v).ok());
        let (Some(header), Some(signature)) = (header, signature) else {
            return ReadOutcome::Malformed;
        };
        if let Err(error) = history.verify_header(&header, &signature) {
            return ReadOutcome::Refused(Refusal::History(error));
        }
        let evidence =
            match parse_replica_evidence(document, self.replica_id, authorization.public_key()) {
                Ok(value) => value,
                Err(error) => return ReadOutcome::Refused(Refusal::Evidence(error)),
            };
        match authorized_batch_by_activity(
            locator.activity_id,
            canonical_receipt,
            &evidence,
            &authorization,
        ) {
            Ok(facts) => ReadOutcome::Verified(facts),
            Err(error) => ReadOutcome::Refused(Refusal::Evidence(error)),
        }
    }
}
