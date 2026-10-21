use std::collections::BTreeMap;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::Path;

use layerx_proof::merkle::leaf_hash;

use super::codec::{self, CodecError, Decoder};
use super::{PrincipalId, StoreError};

const TENANCY_MAGIC: &[u8; 4] = b"LXHT";
const TENANCY_FILE: &str = "tenancy.map";
const TENANCY_TEMP_FILE: &str = "tenancy.map.tmp";
const DIGEST_LENGTH: usize = 32;

/// A validated agent-layer tenant identifier obeying the agent daemon's
/// tenant bounds, so agent-layer isolation composes with principal scoping.
#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct AgentTenantId(String);

impl AgentTenantId {
    /// Creates a non-empty bounded tenant identifier.
    ///
    /// # Errors
    ///
    /// Rejects empty values, values longer than 255 bytes, and embedded NUL bytes.
    pub fn new(value: impl Into<String>) -> Result<Self, StoreError> {
        let value = value.into();
        if value.is_empty() || value.len() > 255 || value.as_bytes().contains(&0) {
            return Err(StoreError::InvalidTenant);
        }
        Ok(Self(value))
    }

    /// Returns the tenant identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// The 32-byte content digest that authenticates one tenancy configuration
/// out of band.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TenancyDigest([u8; 32]);

impl TenancyDigest {
    /// Reconstructs a digest recorded out of band.
    #[must_use]
    pub const fn new(bytes: [u8; 32]) -> Self {
        Self(bytes)
    }

    /// Returns the digest bytes.
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}

/// The total mapping from human principals to their agent-layer tenancy,
/// stored as digest-authenticated configuration.
#[derive(Debug, Eq, PartialEq)]
pub struct TenancyMap {
    entries: BTreeMap<PrincipalId, AgentTenantId>,
}

impl TenancyMap {
    /// Builds a mapping, refusing duplicate principals.
    ///
    /// # Errors
    ///
    /// Returns [`TenancyError::DuplicatePrincipal`] when a principal appears twice.
    pub fn new(
        entries: impl IntoIterator<Item = (PrincipalId, AgentTenantId)>,
    ) -> Result<Self, TenancyError> {
        let mut map = BTreeMap::new();
        for (principal, tenant) in entries {
            if map.insert(principal, tenant).is_some() {
                return Err(TenancyError::DuplicatePrincipal);
            }
        }
        Ok(Self { entries: map })
    }

    /// Resolves the agent-layer tenant for one principal; the mapping is total.
    ///
    /// # Errors
    ///
    /// Returns [`TenancyError::Unmapped`] for a principal without a mapping.
    pub fn tenant_for(&self, principal: &PrincipalId) -> Result<&AgentTenantId, TenancyError> {
        self.entries.get(principal).ok_or(TenancyError::Unmapped)
    }

    /// Returns every mapped principal in deterministic order.
    #[must_use]
    pub fn principals(&self) -> Vec<PrincipalId> {
        self.entries.keys().cloned().collect()
    }

    fn canonical_payload(&self) -> Result<Vec<u8>, TenancyError> {
        let mut payload = Vec::new();
        payload.extend_from_slice(TENANCY_MAGIC);
        codec::push_length(&mut payload, self.entries.len())?;
        for (principal, tenant) in &self.entries {
            codec::push_bytes(&mut payload, principal.as_str().as_bytes())?;
            codec::push_bytes(&mut payload, tenant.as_str().as_bytes())?;
        }
        Ok(payload)
    }

    /// Computes the digest a caller records out of band to authenticate this map.
    ///
    /// # Errors
    ///
    /// Returns an encoding or hashing failure.
    pub fn digest(&self) -> Result<TenancyDigest, TenancyError> {
        let payload = self.canonical_payload()?;
        let digest = leaf_hash(&payload).map_err(|_| TenancyError::Unhashable)?;
        Ok(TenancyDigest(digest))
    }

    /// Encodes the map with its integrity digest appended.
    ///
    /// # Errors
    ///
    /// Returns an encoding or hashing failure.
    pub fn seal(&self) -> Result<Vec<u8>, TenancyError> {
        let mut bytes = self.canonical_payload()?;
        let digest = leaf_hash(&bytes).map_err(|_| TenancyError::Unhashable)?;
        bytes.extend_from_slice(&digest);
        Ok(bytes)
    }

    /// Decodes a sealed map, verifying its integrity digest before parsing.
    ///
    /// # Errors
    ///
    /// Returns [`TenancyError::Tampered`] on a digest mismatch and a typed
    /// corruption failure on any malformed entry.
    pub fn load(bytes: &[u8]) -> Result<Self, TenancyError> {
        let split = bytes
            .len()
            .checked_sub(DIGEST_LENGTH)
            .ok_or(TenancyError::Corrupt("truncated tenancy configuration"))?;
        let (payload, stored_digest) = bytes.split_at(split);
        let digest = leaf_hash(payload).map_err(|_| TenancyError::Unhashable)?;
        if stored_digest != digest.as_slice() {
            return Err(TenancyError::Tampered);
        }
        let mut decoder = Decoder::new(payload);
        if decoder.take(4)? != TENANCY_MAGIC {
            return Err(TenancyError::Corrupt("invalid tenancy header"));
        }
        let count = decoder.length()?;
        let mut entries = BTreeMap::new();
        for _ in 0..count {
            let principal_text = std::str::from_utf8(decoder.bytes()?)
                .map_err(|_| TenancyError::Corrupt("principal is not UTF-8"))?;
            let principal = PrincipalId::new(principal_text)
                .map_err(|_| TenancyError::Corrupt("invalid principal identifier"))?;
            let tenant_text = std::str::from_utf8(decoder.bytes()?)
                .map_err(|_| TenancyError::Corrupt("tenant is not UTF-8"))?;
            let tenant = AgentTenantId::new(tenant_text)
                .map_err(|_| TenancyError::Corrupt("invalid tenant identifier"))?;
            if entries.insert(principal, tenant).is_some() {
                return Err(TenancyError::DuplicatePrincipal);
            }
        }
        if !decoder.is_empty() {
            return Err(TenancyError::Corrupt("trailing bytes"));
        }
        Ok(Self { entries })
    }

    /// Atomically installs the sealed map under the store root and returns the
    /// digest to record out of band.
    ///
    /// # Errors
    ///
    /// Returns an encoding, hashing or I/O failure.
    pub fn install(&self, root: &Path) -> Result<TenancyDigest, TenancyError> {
        let sealed = self.seal()?;
        codec::write_atomic(root, TENANCY_FILE, TENANCY_TEMP_FILE, &sealed)?;
        self.digest()
    }
}

pub(crate) fn load_installed(root: &Path) -> Result<TenancyMap, TenancyError> {
    let path = root.join(TENANCY_FILE);
    if !path.exists() {
        return Err(TenancyError::Missing);
    }
    TenancyMap::load(&fs::read(path)?)
}

/// Tenancy configuration failures.
#[derive(Debug)]
pub enum TenancyError {
    Io(io::Error),
    Missing,
    Tampered,
    DigestMismatch,
    Corrupt(&'static str),
    DuplicatePrincipal,
    Unmapped,
    Unhashable,
    SizeOverflow,
}

impl Display for TenancyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Io(error) => write!(formatter, "tenancy I/O failure: {error}"),
            Self::Missing => formatter.write_str("tenancy configuration is not installed"),
            Self::Tampered => {
                formatter.write_str("tenancy configuration failed its integrity digest")
            }
            Self::DigestMismatch => {
                formatter.write_str("tenancy configuration does not match the expected digest")
            }
            Self::Corrupt(reason) => write!(formatter, "corrupt tenancy configuration: {reason}"),
            Self::DuplicatePrincipal => {
                formatter.write_str("duplicate principal in tenancy configuration")
            }
            Self::Unmapped => formatter.write_str("principal has no agent-layer tenancy mapping"),
            Self::Unhashable => formatter.write_str("tenancy configuration cannot be digested"),
            Self::SizeOverflow => {
                formatter.write_str("tenancy configuration exceeds encoding bounds")
            }
        }
    }
}

impl std::error::Error for TenancyError {}

impl From<io::Error> for TenancyError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

impl From<CodecError> for TenancyError {
    fn from(value: CodecError) -> Self {
        match value {
            CodecError::Truncated => Self::Corrupt("truncated tenancy configuration"),
            CodecError::Overflow => Self::SizeOverflow,
        }
    }
}
