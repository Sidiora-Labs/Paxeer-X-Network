//! PAXAI F02 worker identity values shared with the interaction layer.

/// Native F02 operation selectors; 0x0208 is reserved and never admitted.
pub const ENROLL_WORKER: u16 = 0x0201;
pub const PUBLISH_METADATA: u16 = 0x0202;
pub const SET_DRAINING: u16 = 0x0203;
pub const UNDO_DRAIN: u16 = 0x0204;
pub const ROTATE_DELEGATE: u16 = 0x0205;
pub const REVOKE_DELEGATE: u16 = 0x0206;
pub const RETIRE_WORKER: u16 = 0x0207;
pub const RESERVED_UNAVAILABLE: u16 = 0x0208;
pub const ACCEPT_ENROLLMENT: u16 = 0x0209;
pub const EXPIRE_ENROLLMENT: u16 = 0x020A;
pub const EXPIRE_ENROLLMENT_PAYLOAD_BYTES: usize = 72;
pub const MAX_WORKERS: usize = 32;
pub const MAX_MANIFEST_BYTES: usize = 8_192;

/// Current worker record state; other values refuse.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkerState {
    Enrolled = 1,
    Available = 2,
    Draining = 3,
    Revoked = 4,
    Retired = 5,
    PendingOwner = 6,
}

impl TryFrom<u8> for WorkerState {
    type Error = u8;
    fn try_from(value: u8) -> Result<Self, u8> {
        Ok(match value {
            1 => Self::Enrolled,
            2 => Self::Available,
            3 => Self::Draining,
            4 => Self::Revoked,
            5 => Self::Retired,
            6 => Self::PendingOwner,
            other => return Err(other),
        })
    }
}

/// True only for selectors a native F02 call may carry.
#[must_use]
pub const fn is_native_selector(selector: u16) -> bool {
    matches!(selector, 0x0201..=0x0207 | 0x0209 | 0x020A)
}
