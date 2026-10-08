//! PAXAI F08 admission values shared with the interaction layer. Membership is
//! permission to participate only; it is not proof of AI work, quality or uniqueness.

/// Native F08 operation selectors; RolloverRoster is internal to OpenEpoch.
pub const APPROVE_ADMISSION: u16 = 0x0801;
pub const REVOKE_ADMISSION_APPROVAL: u16 = 0x0802;
pub const ADMIT_WORKER: u16 = 0x0803;
pub const ADMIT_EVALUATOR: u16 = 0x0804;
pub const HEARTBEAT: u16 = 0x0805;
pub const REQUEST_EXIT: u16 = 0x0806;
pub const CANCEL_EXIT: u16 = 0x0807;
pub const PRUNE_INACTIVE: u16 = 0x0808;
pub const ADMINISTRATIVE_REMOVE: u16 = 0x0809;

pub const EVALUATOR_CONSENT_BYTES: usize = 362;
pub const ADMIT_EVALUATOR_PAYLOAD_BYTES: usize = 426;
pub const APPROVAL_FIELD_BYTES: usize = 56;

pub const FLAG_ADMITTED: u8 = 1;
pub const FLAG_DRAINING: u8 = 2;
pub const FLAG_REVOKED: u8 = 4;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Role {
    Worker = 1,
    Evaluator = 2,
}
impl TryFrom<u8> for Role {
    type Error = u8;
    fn try_from(value: u8) -> Result<Self, u8> {
        match value {
            1 => Ok(Self::Worker),
            2 => Ok(Self::Evaluator),
            other => Err(other),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ExitReason {
    Voluntary = 1,
    Retire = 2,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RemovalReason {
    Security = 1,
    Terms = 2,
    OperatorDecision = 3,
}

/// Fixed v1 policy; constants are never caller-supplied.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionPolicyV1 {
    pub worker_capacity: u8,
    pub evaluator_capacity: u8,
    pub worker_per_owner: u8,
    pub evaluator_per_owner: u8,
    pub successful_enrollments_per_epoch: u8,
    pub minimum_owner_admission_distance: u16,
    pub heartbeat_distance: u16,
    pub missed_epoch_threshold: u8,
    pub new_member_immunity_epochs: u8,
    pub score_quorum: u8,
}
pub const POLICY_V1: AdmissionPolicyV1 = AdmissionPolicyV1 {
    worker_capacity: 32,
    evaluator_capacity: 8,
    worker_per_owner: 2,
    evaluator_per_owner: 1,
    successful_enrollments_per_epoch: 4,
    minimum_owner_admission_distance: 16,
    heartbeat_distance: 16,
    missed_epoch_threshold: 2,
    new_member_immunity_epochs: 1,
    score_quorum: 3,
};

/// Public membership view; absence is explicit, never a zero sentinel.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AdmissionMeta {
    pub participant: [u8; 32],
    pub owner: [u8; 32],
    pub role: Role,
    pub admitted_epoch: Option<u64>,
    pub last_heartbeat_epoch: Option<u64>,
    pub last_heartbeat_height: Option<u64>,
    pub immunity_until_epoch: u64,
    pub pending_exit_epoch: Option<u64>,
    pub membership_generation: u64,
    pub complete_missed_opened_epochs: u8,
    pub membership_flags: u8,
}

/// Evaluator health: readiness never implies a submitted score.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Health {
    pub roster_evaluators: u8,
    pub eligible_evaluators: u8,
    pub quorum_ready: bool,
}
