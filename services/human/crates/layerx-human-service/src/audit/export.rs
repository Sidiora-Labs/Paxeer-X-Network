use std::collections::{BTreeMap, BTreeSet};

use layerx_proof::merkle::leaf_hash;

use crate::store::{PrincipalId, PrincipalScope, RowKey};

use super::wire::{push_bytes, push_length, Reader};
use super::{
    decode_entry, evidence_digest, genesis_link, table_code, table_from_code, AuditChain,
    AuditError, ChainHead,
};

const EXPORT_MAGIC: &[u8; 4] = b"LXAX";
const EXPORT_VERSION: u8 = 1;

#[derive(Clone, Debug, Eq, PartialEq)]
struct EvidenceRow {
    written_at: u64,
    bytes: Vec<u8>,
}

/// The independently verified summary of one audit export.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExportReport {
    principal: PrincipalId,
    head: ChainHead,
    entries: usize,
    evidence_rows: usize,
}

impl ExportReport {
    /// Returns the principal whose chain the bundle proves.
    #[must_use]
    pub const fn principal(&self) -> &PrincipalId {
        &self.principal
    }

    /// Returns the verified chain head.
    #[must_use]
    pub const fn head(&self) -> ChainHead {
        self.head
    }

    /// Returns the number of verified audit entries.
    #[must_use]
    pub const fn entries(&self) -> usize {
        self.entries
    }

    /// Returns the number of distinct evidence rows verified by the bundle.
    #[must_use]
    pub const fn evidence_rows(&self) -> usize {
        self.evidence_rows
    }
}

pub(super) fn build(scope: &PrincipalScope<'_>, chain: &AuditChain) -> Result<Vec<u8>, AuditError> {
    let entries = chain.entries(scope)?;
    let mut evidence = BTreeMap::new();
    for entry in &entries {
        for binding in entry.evidence() {
            let row = scope
                .get(binding.table(), binding.key())
                .ok_or(AuditError::Corrupt("dangling export evidence"))?;
            let exported = EvidenceRow {
                written_at: row.written_at(),
                bytes: row.bytes().to_vec(),
            };
            let key = (binding.table(), binding.key().clone());
            if evidence
                .insert(key, exported.clone())
                .is_some_and(|old| old != exported)
            {
                return Err(AuditError::Corrupt("conflicting export evidence"));
            }
        }
    }

    let mut output = Vec::new();
    output.extend_from_slice(EXPORT_MAGIC);
    output.push(EXPORT_VERSION);
    push_bytes(&mut output, scope.principal().as_str().as_bytes())?;
    push_bytes(&mut output, &chain.head().encode())?;
    push_length(&mut output, entries.len())?;
    for entry in &entries {
        push_bytes(&mut output, entry.bytes())?;
    }
    push_length(&mut output, evidence.len())?;
    for ((table, key), row) in evidence {
        output.push(table_code(table));
        push_bytes(&mut output, key.as_str().as_bytes())?;
        output.extend_from_slice(&row.written_at.to_be_bytes());
        push_bytes(&mut output, &row.bytes)?;
    }
    Ok(output)
}

/// Verifies an exported audit chain and all referenced evidence without a
/// human-service store or any trust in the service that produced the bundle.
///
/// # Errors
///
/// Refuses malformed bundles, missing or extraneous evidence, broken chain
/// links, altered entry bytes, altered evidence and a mismatched chain head.
pub fn verify_export(bytes: &[u8]) -> Result<ExportReport, AuditError> {
    let mut reader = Reader::new(bytes);
    if reader.take(4)? != EXPORT_MAGIC {
        return Err(AuditError::Corrupt("invalid audit export header"));
    }
    if reader.byte()? != EXPORT_VERSION {
        return Err(AuditError::Corrupt("unknown audit export version"));
    }
    let principal_text = std::str::from_utf8(reader.bytes()?)
        .map_err(|_| AuditError::Corrupt("export principal is not UTF-8"))?;
    let principal = PrincipalId::new(principal_text)
        .map_err(|_| AuditError::Corrupt("invalid export principal"))?;
    let head = ChainHead::decode(reader.bytes()?)?;

    let entry_count = reader.length()?;
    let mut entry_bytes = Vec::with_capacity(entry_count);
    for _ in 0..entry_count {
        entry_bytes.push(reader.bytes()?.to_vec());
    }

    let evidence_count = reader.length()?;
    let mut evidence = BTreeMap::new();
    for _ in 0..evidence_count {
        let table = table_from_code(reader.byte()?)?;
        let key_text = std::str::from_utf8(reader.bytes()?)
            .map_err(|_| AuditError::Corrupt("export evidence key is not UTF-8"))?;
        let key = RowKey::new(key_text)
            .map_err(|_| AuditError::Corrupt("invalid export evidence key"))?;
        let row = EvidenceRow {
            written_at: reader.u64()?,
            bytes: reader.bytes()?.to_vec(),
        };
        if evidence.insert((table, key), row).is_some() {
            return Err(AuditError::Corrupt("duplicate export evidence"));
        }
    }
    if !reader.is_empty() {
        return Err(AuditError::Corrupt("trailing audit export bytes"));
    }

    if u64::try_from(entry_count).map_err(|_| AuditError::SizeOverflow)? != head.length() {
        return Err(AuditError::HeadMismatch {
            expected: head,
            found: ChainHead {
                length: u64::try_from(entry_count).map_err(|_| AuditError::SizeOverflow)?,
                link: head.link(),
            },
        });
    }

    let mut link = genesis_link(&principal)?;
    let mut used = BTreeSet::new();
    for (position, encoded) in entry_bytes.iter().enumerate() {
        let sequence = u64::try_from(position).map_err(|_| AuditError::SizeOverflow)?;
        let body = decode_entry(encoded)?;
        if body.sequence != sequence {
            return Err(AuditError::SequenceMismatch { sequence });
        }
        if body.prev_link != link {
            return Err(AuditError::LinkMismatch { sequence });
        }
        for binding in body.evidence {
            let key = (binding.table(), binding.key().clone());
            let row = evidence
                .get(&key)
                .ok_or(AuditError::EvidenceMismatch { sequence })?;
            if evidence_digest(row.written_at, &row.bytes)? != binding.digest() {
                return Err(AuditError::EvidenceDigestMismatch { sequence });
            }
            used.insert(key);
        }
        link = leaf_hash(encoded).map_err(|_| AuditError::Unhashable)?;
    }
    if link != head.link() {
        return Err(AuditError::HeadMismatch {
            expected: head,
            found: ChainHead {
                length: head.length(),
                link,
            },
        });
    }
    if used.len() != evidence.len() {
        return Err(AuditError::Corrupt("unreferenced export evidence"));
    }

    Ok(ExportReport {
        principal,
        head,
        entries: entry_count,
        evidence_rows: evidence_count,
    })
}
