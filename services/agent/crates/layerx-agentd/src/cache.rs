//! Tenant-bounded cache whose values can only be constructed from verified evidence.

use std::collections::BTreeMap;

use layerx_proof::checkpoint::ThresholdReport;
use layerx_proof::inclusion::InclusionEvidence;
use layerx_proof::receipt::VerifiedReceipt;
use layerx_types::verify::VerificationLevel;

use crate::store::TenantId;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EvidenceKind {
    Receipt,
    Inclusion,
    Checkpoint,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CacheValue {
    core_bytes: Vec<u8>,
    level: VerificationLevel,
    evidence_kind: EvidenceKind,
    evidence_id: [u8; 32],
    observed_head_sequence: u64,
    observed_checkpoint: [u8; 32],
}

impl CacheValue {
    #[must_use]
    pub fn from_receipt(
        core_bytes: Vec<u8>,
        evidence: &VerifiedReceipt,
        observed_head_sequence: u64,
        observed_checkpoint: [u8; 32],
    ) -> Self {
        Self {
            core_bytes,
            level: evidence.level(),
            evidence_kind: EvidenceKind::Receipt,
            evidence_id: evidence.evidence().receipt_digest().unwrap_or([0; 32]),
            observed_head_sequence,
            observed_checkpoint,
        }
    }

    #[must_use]
    pub fn from_inclusion(
        core_bytes: Vec<u8>,
        evidence: &InclusionEvidence,
        observed_head_sequence: u64,
        observed_checkpoint: [u8; 32],
    ) -> Self {
        Self {
            core_bytes,
            level: evidence.level(),
            evidence_kind: EvidenceKind::Inclusion,
            evidence_id: evidence.evidence().header_digest().unwrap_or([0; 32]),
            observed_head_sequence,
            observed_checkpoint,
        }
    }

    #[must_use]
    pub fn from_checkpoint(
        core_bytes: Vec<u8>,
        evidence: &ThresholdReport,
        observed_head_sequence: u64,
        observed_checkpoint: [u8; 32],
    ) -> Self {
        Self {
            core_bytes,
            level: evidence.level(),
            evidence_kind: EvidenceKind::Checkpoint,
            evidence_id: evidence.evidence().checkpoint_id().unwrap_or([0; 32]),
            observed_head_sequence,
            observed_checkpoint,
        }
    }

    #[must_use]
    pub fn core_bytes(&self) -> &[u8] {
        &self.core_bytes
    }

    #[must_use]
    pub const fn level(&self) -> VerificationLevel {
        self.level
    }

    #[must_use]
    pub const fn evidence_kind(&self) -> EvidenceKind {
        self.evidence_kind
    }

    #[must_use]
    pub const fn evidence_id(&self) -> [u8; 32] {
        self.evidence_id
    }

    #[must_use]
    pub const fn observed_head_sequence(&self) -> u64 {
        self.observed_head_sequence
    }

    #[must_use]
    pub const fn observed_checkpoint(&self) -> [u8; 32] {
        self.observed_checkpoint
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CacheMetrics {
    pub entries: usize,
    pub bytes: usize,
    pub hits: u64,
    pub misses: u64,
    pub stale: u64,
    pub revalidations: u64,
    pub quota_refusals: u64,
}

#[derive(Debug)]
pub struct EvidenceCache {
    maximum_entries_per_tenant: usize,
    maximum_bytes_per_tenant: usize,
    entries: BTreeMap<(TenantId, Vec<u8>), CacheValue>,
    metrics: BTreeMap<TenantId, CacheMetrics>,
}

impl EvidenceCache {
    /// Creates an empty cache with per-tenant entry and byte ceilings.
    ///
    /// # Errors
    ///
    /// Refuses a zero entry ceiling or a zero byte ceiling.
    pub fn new(
        maximum_entries_per_tenant: usize,
        maximum_bytes_per_tenant: usize,
    ) -> Result<Self, CacheError> {
        if maximum_entries_per_tenant == 0 || maximum_bytes_per_tenant == 0 {
            return Err(CacheError::InvalidLimits);
        }
        Ok(Self {
            maximum_entries_per_tenant,
            maximum_bytes_per_tenant,
            entries: BTreeMap::new(),
            metrics: BTreeMap::new(),
        })
    }

    /// Inserts a value whose evidence has already been verified.
    ///
    /// # Errors
    ///
    /// Refuses an empty key or an absent evidence identifier, and refuses a write that would
    /// cross the tenant's entry or byte ceiling; overflowing totals return `Arithmetic`.
    pub fn insert(
        &mut self,
        tenant: TenantId,
        key: Vec<u8>,
        value: CacheValue,
    ) -> Result<(), CacheError> {
        if key.is_empty() || value.evidence_id == [0; 32] {
            return Err(CacheError::MissingEvidence);
        }
        let current = self.metrics.get(&tenant).copied().unwrap_or_default();
        let previous_bytes = self
            .entries
            .get(&(tenant.clone(), key.clone()))
            .map_or(0, |entry| entry.core_bytes.len());
        let adding_entry = usize::from(!self.entries.contains_key(&(tenant.clone(), key.clone())));
        let projected_entries = current
            .entries
            .checked_add(adding_entry)
            .ok_or(CacheError::Arithmetic)?;
        let projected_bytes = current
            .bytes
            .checked_sub(previous_bytes)
            .and_then(|bytes| bytes.checked_add(value.core_bytes.len()))
            .ok_or(CacheError::Arithmetic)?;
        if projected_entries > self.maximum_entries_per_tenant
            || projected_bytes > self.maximum_bytes_per_tenant
        {
            self.metrics.entry(tenant).or_default().quota_refusals += 1;
            return Err(CacheError::QuotaExceeded);
        }
        self.entries.insert((tenant.clone(), key), value);
        let metrics = self.metrics.entry(tenant).or_default();
        metrics.entries = projected_entries;
        metrics.bytes = projected_bytes;
        Ok(())
    }

    #[must_use]
    pub fn metrics(&self, tenant: &TenantId) -> CacheMetrics {
        self.metrics.get(tenant).copied().unwrap_or_default()
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CacheError {
    InvalidLimits,
    MissingEvidence,
    Missing,
    QuotaExceeded,
    InsufficientEvidence {
        requested: VerificationLevel,
        held: VerificationLevel,
    },
    CoreUnavailable {
        cached_level: VerificationLevel,
        stale_by_sequences: u64,
    },
    RevalidationMismatch,
    Arithmetic,
}

/// Returns a cached value only at its held proof level, revalidating any stale entry.
///
/// # Errors
///
/// Returns `Missing` for an unknown key, `InsufficientEvidence` when the held or refreshed level
/// is below the requested one, and `RevalidationMismatch` when a refresh does not observe the
/// current head and checkpoint; a stale entry propagates the refusal raised by `fetch_verified`.
pub fn revalidate<F>(
    cache: &mut EvidenceCache,
    tenant: TenantId,
    key: &[u8],
    requested: VerificationLevel,
    current_head_sequence: u64,
    current_checkpoint: [u8; 32],
    mut fetch_verified: F,
) -> Result<CacheValue, CacheError>
where
    F: FnMut() -> Result<CacheValue, CacheError>,
{
    let cache_key = (tenant.clone(), key.to_vec());
    let Some(cached) = cache.entries.get(&cache_key).cloned() else {
        cache.metrics.entry(tenant).or_default().misses += 1;
        return Err(CacheError::Missing);
    };
    let stale = cached.observed_head_sequence != current_head_sequence
        || cached.observed_checkpoint != current_checkpoint;
    if !stale {
        if cached.level < requested {
            return Err(CacheError::InsufficientEvidence {
                requested,
                held: cached.level,
            });
        }
        cache.metrics.entry(tenant).or_default().hits += 1;
        return Ok(cached);
    }
    cache.metrics.entry(tenant.clone()).or_default().stale += 1;
    let refreshed = fetch_verified().map_err(|error| match error {
        CacheError::CoreUnavailable { .. } => CacheError::CoreUnavailable {
            cached_level: cached.level,
            stale_by_sequences: current_head_sequence.saturating_sub(cached.observed_head_sequence),
        },
        other => other,
    })?;
    if refreshed.observed_head_sequence != current_head_sequence
        || refreshed.observed_checkpoint != current_checkpoint
    {
        return Err(CacheError::RevalidationMismatch);
    }
    if refreshed.level < requested {
        return Err(CacheError::InsufficientEvidence {
            requested,
            held: refreshed.level,
        });
    }
    cache.insert(tenant.clone(), key.to_vec(), refreshed.clone())?;
    cache.metrics.entry(tenant).or_default().revalidations += 1;
    Ok(refreshed)
}
