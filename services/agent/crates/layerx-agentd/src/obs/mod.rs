//! Observability output is constrained by the same tenant redaction seam as audit.

pub mod health;
pub mod metrics;
pub mod trace;

use crate::audit::{self, DataClass, OutputSurface, RedactionError, RenderedOutput};
use crate::store::TenantId;
use crate::tenant::Config;

/// Renders observability output through the same tenant redaction seam as audit.
///
/// # Errors
///
/// Returns `WrongTenant` when the configuration belongs to another tenant, and
/// `InvalidPublicText` when public text is not UTF-8, is empty, exceeds 4096 bytes, or carries a
/// control character other than tab or newline.
pub fn redact(
    config: &Config,
    tenant: &TenantId,
    surface: OutputSurface,
    class: DataClass,
    value: &[u8],
    current_sequence: u64,
) -> Result<RenderedOutput, RedactionError> {
    audit::redact(config, tenant, surface, class, value, current_sequence)
}
