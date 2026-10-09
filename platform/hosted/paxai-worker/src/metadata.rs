//! F02-R005/R007 canonical worker metadata manifest: typed construction and parsing over the
//! shared `workers::decode_manifest` grammar, which stays the single canonical validator.
use crate::auth::ServiceError;
use layerx_programs_ai_market::{
    codec::{self, Reader},
    types::{Digest32, MarketId, MetadataDigest, PrincipalId, WorkerId},
    workers::{self, MAX_MANIFEST_BYTES, MAX_URI_BYTES},
};

pub const MANIFEST_SCHEMA: u16 = 1;
pub const API_VERSION: u16 = 1;
pub const TRANSPORT_HTTPS: u8 = 1;
pub const CAPABILITY_BYTES: usize = 186;
pub const CAPABILITY_DOMAIN: &str = "PAXAI/worker-capability/v1";
pub const SERVICE_BASE_PATH: &str = "/paxai/v1";
pub const DEFAULT_PORT: u16 = 443;

/// One capability entry: workload kind, execution mode, content digests (raw, may be zero),
/// the declared bounds and the service shape.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Capability {
    pub kind: u8,
    pub mode: u8,
    pub model: [u8; 32],
    pub model_manifest: [u8; 32],
    pub tokenizer: [u8; 32],
    pub input_schema: [u8; 32],
    pub output_schema: [u8; 32],
    pub max_input_bytes: u32,
    pub max_output_bytes: u32,
    pub max_input_units: u32,
    pub max_output_units: u32,
    pub unit_kind: u8,
    pub latency_ms: u32,
    pub concurrency: u16,
    pub determinism: u8,
}
impl Capability {
    #[must_use]
    pub fn encoded(&self) -> [u8; CAPABILITY_BYTES] {
        let mut out = [0; CAPABILITY_BYTES];
        let mut at = 0;
        let mut put = |bytes: &[u8]| {
            out[at..at + bytes.len()].copy_from_slice(bytes);
            at += bytes.len();
        };
        put(&[self.kind, self.mode]);
        for digest in [
            &self.model,
            &self.model_manifest,
            &self.tokenizer,
            &self.input_schema,
            &self.output_schema,
        ] {
            put(digest);
        }
        for bound in [
            self.max_input_bytes,
            self.max_output_bytes,
            self.max_input_units,
            self.max_output_units,
        ] {
            put(&bound.to_be_bytes());
        }
        put(&[self.unit_kind]);
        put(&self.latency_ms.to_be_bytes());
        put(&self.concurrency.to_be_bytes());
        put(&[self.determinism]);
        out
    }
    /// `capability_digest = H("PAXAI/worker-capability/v1", entry)`, the value a request binds.
    ///
    /// # Errors
    /// `NonCanonical` when the digest is all zero.
    pub fn digest(&self) -> Result<Digest32, ServiceError> {
        Ok(codec::domain_hash(CAPABILITY_DOMAIN, &self.encoded())?)
    }
    fn read(r: &mut Reader<'_>) -> Result<Self, ServiceError> {
        Ok(Self {
            kind: r.u8()?,
            mode: r.u8()?,
            model: r.fixed()?,
            model_manifest: r.fixed()?,
            tokenizer: r.fixed()?,
            input_schema: r.fixed()?,
            output_schema: r.fixed()?,
            max_input_bytes: r.u32()?,
            max_output_bytes: r.u32()?,
            max_input_units: r.u32()?,
            max_output_units: r.u32()?,
            unit_kind: r.u8()?,
            latency_ms: r.u32()?,
            concurrency: r.u16()?,
            determinism: r.u8()?,
        })
    }
}

/// One HTTPS service endpoint and the SHA-256 pin of its leaf certificate SPKI.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Endpoint {
    pub id: u8,
    pub uri: String,
    pub spki_sha256: [u8; 32],
}
impl Endpoint {
    /// Host and port of the canonical `https://<host>[:port]/paxai/v1` URI.
    ///
    /// # Errors
    /// `NonCanonical` when the URI is not canonical.
    pub fn authority(&self) -> Result<(&str, u16), ServiceError> {
        workers::check_uri(self.uri.as_bytes())?;
        let authority = self
            .uri
            .strip_prefix("https://")
            .and_then(|rest| rest.strip_suffix(SERVICE_BASE_PATH))
            .ok_or(ServiceError::NonCanonical)?;
        match authority.split_once(':') {
            None => Ok((authority, DEFAULT_PORT)),
            Some((host, port)) => Ok((
                host,
                port.parse().map_err(|_| ServiceError::NonCanonical)?,
            )),
        }
    }
}

/// Typed canonical manifest.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Manifest {
    pub market: MarketId,
    pub worker: WorkerId,
    pub owner: PrincipalId,
    pub generation: u64,
    pub key_version: u64,
    pub revision: u64,
    pub valid_from: u64,
    pub expiry: u64,
    pub deployment: Digest32,
    pub capabilities: Vec<Capability>,
    pub endpoints: Vec<Endpoint>,
    pub privacy_policy: Digest32,
    pub service_terms: Digest32,
}
impl Manifest {
    /// Canonical bytes, re-validated through the shared manifest grammar.
    ///
    /// # Errors
    /// `CapacityExceeded` above the manifest cap; `NonCanonical` for any field, count, order,
    /// window or endpoint the grammar refuses.
    pub fn encode(&self) -> Result<Vec<u8>, ServiceError> {
        let mut out = Vec::with_capacity(MAX_MANIFEST_BYTES);
        out.extend_from_slice(&MANIFEST_SCHEMA.to_be_bytes());
        out.extend_from_slice(self.market.as_bytes());
        out.extend_from_slice(self.worker.as_bytes());
        out.extend_from_slice(self.owner.as_bytes());
        for value in [
            self.generation,
            self.key_version,
            self.revision,
            self.valid_from,
            self.expiry,
        ] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out.extend_from_slice(self.deployment.as_bytes());
        let count =
            u16::try_from(self.capabilities.len()).map_err(|_| ServiceError::NonCanonical)?;
        out.extend_from_slice(&count.to_be_bytes());
        for capability in &self.capabilities {
            out.extend_from_slice(&capability.encoded());
        }
        let count = u16::try_from(self.endpoints.len()).map_err(|_| ServiceError::NonCanonical)?;
        out.extend_from_slice(&count.to_be_bytes());
        for endpoint in &self.endpoints {
            let length =
                u32::try_from(endpoint.uri.len()).map_err(|_| ServiceError::NonCanonical)?;
            out.push(endpoint.id);
            out.extend_from_slice(&length.to_be_bytes());
            out.extend_from_slice(endpoint.uri.as_bytes());
            out.extend_from_slice(&endpoint.spki_sha256);
            out.push(TRANSPORT_HTTPS);
            out.extend_from_slice(&API_VERSION.to_be_bytes());
            out.extend_from_slice(&API_VERSION.to_be_bytes());
        }
        out.extend_from_slice(self.privacy_policy.as_bytes());
        out.extend_from_slice(self.service_terms.as_bytes());
        out.extend_from_slice(&0u32.to_be_bytes());
        workers::decode_manifest(&out)?;
        Ok(out)
    }

    /// Parses canonical bytes after the shared grammar accepted them.
    ///
    /// # Errors
    /// `CapacityExceeded` above the manifest cap; `UnsupportedVersion` for another schema;
    /// `NonCanonical` for anything else the grammar refuses.
    pub fn decode(bytes: &[u8]) -> Result<(Self, MetadataDigest), ServiceError> {
        let summary = workers::decode_manifest(bytes)?;
        let mut r = Reader::new(bytes);
        r.u16()?;
        let market = MarketId::new(r.fixed()?)?;
        let worker = WorkerId::new(r.fixed()?)?;
        let owner = PrincipalId::new(r.fixed()?)?;
        let generation = r.u64()?;
        let key_version = r.u64()?;
        let revision = r.u64()?;
        let valid_from = r.u64()?;
        let expiry = r.u64()?;
        let deployment = Digest32::new(r.fixed()?)?;
        let capabilities = (0..r.u16()?)
            .map(|_| Capability::read(&mut r))
            .collect::<Result<Vec<_>, _>>()?;
        let mut endpoints = Vec::new();
        for _ in 0..r.u16()? {
            let id = r.u8()?;
            let uri = r.bytes(MAX_URI_BYTES)?;
            let uri = String::from_utf8(uri.to_vec()).map_err(|_| ServiceError::NonCanonical)?;
            let spki_sha256 = r.fixed()?;
            r.take(5)?;
            endpoints.push(Endpoint {
                id,
                uri,
                spki_sha256,
            });
        }
        let manifest = Self {
            market,
            worker,
            owner,
            generation,
            key_version,
            revision,
            valid_from,
            expiry,
            deployment,
            capabilities,
            endpoints,
            privacy_policy: Digest32::new(r.fixed()?)?,
            service_terms: Digest32::new(r.fixed()?)?,
        };
        r.u32()?;
        r.finish()?;
        Ok((manifest, summary.digest))
    }

    /// The capability a request names by digest.
    ///
    /// # Errors
    /// `CapabilityMismatch` when no capability of this manifest has that digest.
    pub fn capability(&self, digest: Digest32) -> Result<&Capability, ServiceError> {
        for capability in &self.capabilities {
            if capability.digest()? == digest {
                return Ok(capability);
            }
        }
        Err(ServiceError::CapabilityMismatch)
    }

    /// Inclusive at `valid_from`, exclusive at `expiry`.
    ///
    /// # Errors
    /// `AdmissionNotEffective` before `valid_from`; `MetadataExpired` at or after `expiry`.
    pub fn check_window(&self, height: u64) -> Result<(), ServiceError> {
        if height < self.valid_from {
            Err(ServiceError::AdmissionNotEffective)
        } else if height >= self.expiry {
            Err(ServiceError::MetadataExpired)
        } else {
            Ok(())
        }
    }
}

/// The revision a replacement manifest must carry.
///
/// # Errors
/// `Overflow` at `u64::MAX`.
pub fn next_revision(current: u64) -> Result<u64, ServiceError> {
    current.checked_add(1).ok_or(ServiceError::Overflow)
}
