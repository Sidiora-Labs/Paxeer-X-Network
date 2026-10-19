//! Approval lifecycle projection into ordered delivery and durable audit.

use std::fmt::{Display, Formatter, Write as _};

use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::verify::VerificationLevel;

use crate::audit::{
    record, AppendReceipt, Coverage, Decision, Entry, EventClass, Log, PayloadEvidence,
    RecordError, Redacted,
};
use crate::events::{
    ingest_local_restriction, CoreEvent, EventAttributes, EventIngestor, IngestError,
};
use crate::session::SessionId;
use crate::store::TenantId;

use super::APPROVAL_ENFORCEMENT_NOTICE;

/// Ordered approval lifecycle vocabulary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ApprovalEventKind {
    Created = 1,
    Granted = 2,
    Rejected = 3,
    Expired = 4,
}

/// Complete context required for one stream and audit transition.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalLifecycle {
    pub tenant: TenantId,
    pub agent: Did,
    pub session: SessionId,
    pub capability: [u8; 32],
    pub policy_version: String,
    pub approval_id: [u8; 32],
    pub canonical_digest: [u8; 32],
    pub activity_type: u16,
    pub asset: String,
    pub kind: ApprovalEventKind,
    pub principal: Option<String>,
    pub observed_at_ms: u64,
}

/// Evidence returned only after audit and ordered-stream persistence succeed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalEmission {
    pub global_sequence: u64,
    pub canonical_event_bytes: Vec<u8>,
    pub canonical_digest: [u8; 32],
    pub audit_receipt: AppendReceipt,
    pub enforcement_notice: &'static str,
}

/// Emits approval lifecycle events into the daemon's existing ordered stream.
pub struct ApprovalEvents;

impl ApprovalEvents {
    /// Audits one transition before atomically ingesting its local-only stream event.
    ///
    /// # Errors
    ///
    /// Refuses cross-tenant, malformed, audit, or ordered-ingestion failures.
    pub fn emit(
        ingestor: &mut EventIngestor,
        audit: &mut Log,
        coverage: &mut Coverage,
        lifecycle: &ApprovalLifecycle,
    ) -> Result<ApprovalEmission, ApprovalEventError> {
        if ingestor.tenant() != &lifecycle.tenant
            || !audit.owns_tenant(&lifecycle.tenant)
            || lifecycle.policy_version.is_empty()
            || lifecycle.asset.is_empty()
            || lifecycle.principal.as_deref().is_some_and(str::is_empty)
        {
            return Err(ApprovalEventError::Invalid);
        }
        let global_sequence = ingestor.watermark().next_expected;
        let canonical_event_bytes = encode(global_sequence, lifecycle)?;
        let reason =
            Redacted::stored(reason(lifecycle)).map_err(|_| ApprovalEventError::Invalid)?;
        let entry = Entry {
            class: EventClass::PolicyDecision,
            observed_at_ms: lifecycle.observed_at_ms,
            tenant: lifecycle.tenant.clone(),
            agent: lifecycle.agent.clone(),
            session: Some(lifecycle.session),
            capability: Some(lifecycle.capability),
            policy_version: lifecycle.policy_version.clone(),
            request_id: lifecycle.approval_id,
            idempotency_key: Some(IdempotencyKey::new(lifecycle.approval_id)),
            decision: audit_decision(lifecycle.kind),
            reason,
            resulting_activity_id: None,
            verification_level: VerificationLevel::UNVERIFIED,
            protocol_authority: None,
            submitted_bytes: Some(PayloadEvidence::Digest(lifecycle.canonical_digest)),
            receipt_id: None,
        };
        let event = CoreEvent {
            global_sequence,
            canonical_bytes: canonical_event_bytes.clone(),
            receipt_reference: None,
            receipt_verification_level: VerificationLevel::UNVERIFIED,
            attributes: EventAttributes {
                agent: String::from_utf8_lossy(lifecycle.agent.as_bytes()).into_owned(),
                account: lifecycle.tenant.as_str().to_owned(),
                activity_type: lifecycle.activity_type,
                module: "approval".to_owned(),
                asset: lifecycle.asset.clone(),
                counterparty: "human-approval".to_owned(),
                result_code: result_code(lifecycle.kind),
            },
        };
        let (audit_receipt, stream_result) = record(audit, coverage, &entry, || {
            ingest_local_restriction(ingestor, event)
        })
        .map_err(ApprovalEventError::Audit)?;
        stream_result.map_err(ApprovalEventError::Stream)?;
        Ok(ApprovalEmission {
            global_sequence,
            canonical_event_bytes,
            canonical_digest: lifecycle.canonical_digest,
            audit_receipt,
            enforcement_notice: APPROVAL_ENFORCEMENT_NOTICE,
        })
    }
}

#[derive(Debug)]
pub enum ApprovalEventError {
    Invalid,
    Audit(RecordError),
    Stream(IngestError),
}

impl Display for ApprovalEventError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid => formatter.write_str("invalid approval lifecycle event"),
            Self::Audit(error) => Display::fmt(error, formatter),
            Self::Stream(error) => Display::fmt(error, formatter),
        }
    }
}

impl std::error::Error for ApprovalEventError {}

fn encode(sequence: u64, lifecycle: &ApprovalLifecycle) -> Result<Vec<u8>, ApprovalEventError> {
    let principal = lifecycle
        .principal
        .as_deref()
        .unwrap_or_default()
        .as_bytes();
    let principal_length =
        u16::try_from(principal.len()).map_err(|_| ApprovalEventError::Invalid)?;
    let mut bytes = Vec::with_capacity(82 + principal.len());
    bytes.extend_from_slice(b"LXAE");
    bytes.push(1);
    bytes.push(lifecycle.kind as u8);
    bytes.extend_from_slice(&sequence.to_be_bytes());
    bytes.extend_from_slice(&lifecycle.approval_id);
    bytes.extend_from_slice(&lifecycle.canonical_digest);
    bytes.extend_from_slice(&principal_length.to_be_bytes());
    bytes.extend_from_slice(principal);
    Ok(bytes)
}

fn reason(lifecycle: &ApprovalLifecycle) -> String {
    let mut output = format!("approval_{:?} digest=sha256:", lifecycle.kind).to_ascii_lowercase();
    for byte in lifecycle.canonical_digest {
        let _ = write!(output, "{byte:02x}");
    }
    if let Some(principal) = &lifecycle.principal {
        output.push_str(" principal=");
        output.push_str(principal);
    }
    output.push_str(" enforcement=");
    output.push_str(APPROVAL_ENFORCEMENT_NOTICE);
    output
}

const fn audit_decision(kind: ApprovalEventKind) -> Decision {
    match kind {
        ApprovalEventKind::Created => Decision::Requested,
        ApprovalEventKind::Granted => Decision::Allowed,
        ApprovalEventKind::Rejected => Decision::Refused,
        ApprovalEventKind::Expired => Decision::Failed,
    }
}

const fn result_code(kind: ApprovalEventKind) -> i32 {
    match kind {
        ApprovalEventKind::Created | ApprovalEventKind::Granted => 0,
        ApprovalEventKind::Rejected => -1,
        ApprovalEventKind::Expired => -2,
    }
}
