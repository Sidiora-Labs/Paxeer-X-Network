//! Codec-wide hard bounds.

use layerx_types::result::KnownResult;

use crate::WireError;

/// Maximum canonical activity or receipt allocation.
pub const MAX_MESSAGE_BYTES: usize = 1_048_576;
/// Smallest canonical receipt effect: module id, ordinal and event type, kind,
/// monetary flag, a bounded 32-byte transfer-set root and an empty bounded body.
const MIN_RECEIPT_EFFECT_BYTES: usize = 2 + 2 + 2 + 1 + 1 + (4 + 32) + 4;
/// Effect-count bound of the sequencer receipt layout (module version 1): every
/// effect count that fits in one canonical message, so the count never refuses
/// a receipt the message bound admits.
pub const MAX_SEQUENCER_EFFECTS: usize = MAX_MESSAGE_BYTES / MIN_RECEIPT_EFFECT_BYTES;
/// Legacy protocol version retained for explicit decode compatibility.
pub const LEGACY_PROTOCOL_VERSION: u16 = 1;
/// Default protocol version when a caller does not explicitly opt in.
pub const PROTOCOL_VERSION: u16 = 2;
/// Highest protocol version accepted in a version-carrying envelope.
pub const MAX_PROTOCOL_VERSION: u16 = STATE_COMMITMENT_PROTOCOL_VERSION;
/// Explicit composite-state commitment protocol; callers must opt in.
pub const STATE_COMMITMENT_PROTOCOL_VERSION: u16 = 3;

/// Whether canonical version-carrying structures support this protocol.
#[must_use]
pub const fn protocol_version_supported(version: u16) -> bool {
    matches!(
        version,
        LEGACY_PROTOCOL_VERSION | PROTOCOL_VERSION | STATE_COMMITMENT_PROTOCOL_VERSION
    )
}

/// Whether the protocol requires the occupancy layout and evidence checks.
#[must_use]
pub const fn protocol_version_uses_occupancy(version: u16) -> bool {
    matches!(
        version,
        PROTOCOL_VERSION | STATE_COMMITMENT_PROTOCOL_VERSION
    )
}
/// Version used by structures whose canonical layout is not version-carrying.
pub const STRUCTURE_VERSION: u16 = LEGACY_PROTOCOL_VERSION;

/// Enforces a declared element/count bound before allocation.
///
/// # Errors
///
/// Returns the protocol length-limit result at `offset` when `actual` exceeds
/// `maximum`.
pub fn enforce(actual: usize, maximum: usize, offset: usize) -> Result<(), WireError> {
    if actual > maximum {
        Err(WireError::known(KnownResult::LengthLimit, offset))
    } else {
        Ok(())
    }
}
