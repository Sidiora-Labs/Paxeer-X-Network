//! Tenant-scoped, fail-closed audit evidence.

#[path = "export.rs"]
mod exporting;
pub mod log;
#[path = "record.rs"]
mod records;
#[path = "redact.rs"]
mod redaction;

pub use exporting::{
    export, review, verify_chain_material, AuditExport, ChainError, ChainLink, ChainMaterial,
    EvidenceStore, ExportError, ExportedEntry, Query, ReferencedEvidence, ReviewError,
    ReviewReport,
};
pub use log::{
    verify_chain, AppendReceipt, AuditError, ChainFailure, ChainIssue, Log, Verification,
};
pub use records::{
    read_entries, reconstruct_session, record, Coverage, Decision, DecisionEvidence, Entry,
    EventClass, Reconstruction, RecordError, StoredReceiptEvidence,
};
pub use redaction::{
    protect_payload, redact, DataClass, OutputSurface, PayloadEvidence, Redacted, RedactionError,
    RedactionRegistry, RenderedOutput,
};
