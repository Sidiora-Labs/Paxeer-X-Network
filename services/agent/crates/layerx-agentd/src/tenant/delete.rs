use std::collections::BTreeSet;

use sha2::{Digest, Sha256};

use crate::store::{ObjectKind, StorageClass, Store, StoreError, TenantId, TenantKey};

const LEGAL_MAGIC: &[u8; 4] = b"LXLR";
const DELETION_MAGIC: &[u8; 4] = b"LXTD";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum LegalAuditClass {
    RegulatoryHold = 1,
    LitigationHold = 2,
    TaxRecord = 3,
}

/// Legal-retention metadata only; it cannot contain protocol-derived values.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LegalAuditRecord {
    pub class: LegalAuditClass,
    pub reason_code: String,
    pub retain_until_sequence: u64,
}

impl LegalAuditRecord {
    /// Builds a legal-retention record out of metadata alone.
    ///
    /// # Errors
    ///
    /// Returns `DeletionError::InvalidRetention` when the reason code is empty, longer than
    /// 128 bytes or carries a NUL, or when the retain-until sequence is zero.
    pub fn new(
        class: LegalAuditClass,
        reason_code: impl Into<String>,
        retain_until_sequence: u64,
    ) -> Result<Self, DeletionError> {
        let reason_code = reason_code.into();
        if reason_code.is_empty()
            || reason_code.len() > 128
            || reason_code.as_bytes().contains(&0)
            || retain_until_sequence == 0
        {
            return Err(DeletionError::InvalidRetention);
        }
        Ok(Self {
            class,
            reason_code,
            retain_until_sequence,
        })
    }
}

/// Explicit allow-list of legal audit records; an empty set means retain none.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct LegalRetention {
    pub audit_object_ids: BTreeSet<Vec<u8>>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeletionReport {
    pub tenant: TenantId,
    pub local_removed: usize,
    pub core_cache_removed: usize,
    pub legal_audits_retained: usize,
    pub deletion_audit_id: Vec<u8>,
    pub retained_protocol_values: usize,
}

#[derive(Debug)]
pub enum DeletionError {
    InvalidRetention,
    InvalidDeletionId,
    CorruptLegalAudit,
    Store(StoreError),
}

impl From<StoreError> for DeletionError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Persists one schema-restricted legal record that contains metadata only.
///
/// # Errors
///
/// Returns `DeletionError::Store` wrapping `StoreError::InvalidObjectId` for an empty or
/// oversized audit object id, or the `Io`/`SizeOverflow` raised while persisting, and
/// `InvalidRetention` if the reason code will not fit its `u16` length prefix.
pub fn record_legal_audit(
    store: &mut Store,
    tenant: TenantId,
    object_id: Vec<u8>,
    record: &LegalAuditRecord,
) -> Result<(), DeletionError> {
    let key = TenantKey::new(tenant, ObjectKind::Audit, object_id)?;
    store.put_local(key, encode_legal(record)?)?;
    Ok(())
}

pub(crate) fn delete_tenant(
    store: &mut Store,
    tenant: &TenantId,
    policy: &LegalRetention,
    current_sequence: u64,
    deletion_id: [u8; 16],
) -> Result<DeletionReport, DeletionError> {
    if deletion_id == [0; 16] {
        return Err(DeletionError::InvalidDeletionId);
    }
    let mut retained = BTreeSet::new();
    for object_id in &policy.audit_object_ids {
        let key = TenantKey::new(tenant.clone(), ObjectKind::Audit, object_id.clone())?;
        let stored = store.get(&key).ok_or(DeletionError::InvalidRetention)?;
        if stored.class() != StorageClass::LocalOnly {
            return Err(DeletionError::InvalidRetention);
        }
        let record = decode_legal(stored.bytes())?;
        if current_sequence < record.retain_until_sequence {
            retained.insert(object_id.clone());
        }
    }
    let mut deletion_audit_id = b"tenant-deletion:".to_vec();
    deletion_audit_id.extend_from_slice(&deletion_id);
    let policy_digest: [u8; 32] = Sha256::digest(encode_retention(&retained)).into();
    let tenant_for_audit = tenant.clone();
    let audit_id_for_report = deletion_audit_id.clone();
    let removal = store.delete_tenant_entries(tenant, &retained, deletion_audit_id, |removal| {
        let mut bytes = Vec::with_capacity(92);
        bytes.extend_from_slice(DELETION_MAGIC);
        bytes.extend_from_slice(&current_sequence.to_be_bytes());
        bytes.extend_from_slice(&deletion_id);
        bytes.extend_from_slice(&policy_digest);
        for count in [
            removal.local_removed,
            removal.core_cache_removed,
            removal.legal_audits_retained,
        ] {
            let count = u64::try_from(count).map_err(|_| StoreError::SizeOverflow)?;
            bytes.extend_from_slice(&count.to_be_bytes());
        }
        Ok(bytes)
    })?;
    Ok(DeletionReport {
        tenant: tenant_for_audit,
        local_removed: removal.local_removed,
        core_cache_removed: removal.core_cache_removed,
        legal_audits_retained: removal.legal_audits_retained,
        deletion_audit_id: audit_id_for_report,
        retained_protocol_values: 0,
    })
}

fn encode_legal(record: &LegalAuditRecord) -> Result<Vec<u8>, DeletionError> {
    let reason = record.reason_code.as_bytes();
    let length = u16::try_from(reason.len()).map_err(|_| DeletionError::InvalidRetention)?;
    let mut bytes = Vec::with_capacity(15 + reason.len());
    bytes.extend_from_slice(LEGAL_MAGIC);
    bytes.push(record.class as u8);
    bytes.extend_from_slice(&record.retain_until_sequence.to_be_bytes());
    bytes.extend_from_slice(&length.to_be_bytes());
    bytes.extend_from_slice(reason);
    Ok(bytes)
}

fn decode_legal(bytes: &[u8]) -> Result<LegalAuditRecord, DeletionError> {
    if bytes.len() < 15 || &bytes[..4] != LEGAL_MAGIC {
        return Err(DeletionError::CorruptLegalAudit);
    }
    let class = match bytes[4] {
        1 => LegalAuditClass::RegulatoryHold,
        2 => LegalAuditClass::LitigationHold,
        3 => LegalAuditClass::TaxRecord,
        _ => return Err(DeletionError::CorruptLegalAudit),
    };
    let retain_until_sequence = u64::from_be_bytes(
        bytes[5..13]
            .try_into()
            .map_err(|_| DeletionError::CorruptLegalAudit)?,
    );
    let reason_length = usize::from(u16::from_be_bytes([bytes[13], bytes[14]]));
    if bytes.len() != 15 + reason_length {
        return Err(DeletionError::CorruptLegalAudit);
    }
    let reason_code = std::str::from_utf8(&bytes[15..])
        .map_err(|_| DeletionError::CorruptLegalAudit)?
        .to_owned();
    LegalAuditRecord::new(class, reason_code, retain_until_sequence)
        .map_err(|_| DeletionError::CorruptLegalAudit)
}

fn encode_retention(retained: &BTreeSet<Vec<u8>>) -> Vec<u8> {
    let mut bytes = Vec::new();
    for object_id in retained {
        bytes.extend_from_slice(
            &u32::try_from(object_id.len())
                .unwrap_or(u32::MAX)
                .to_be_bytes(),
        );
        bytes.extend_from_slice(object_id);
    }
    bytes
}
