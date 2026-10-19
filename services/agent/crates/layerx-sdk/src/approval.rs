//! Contract 1.1 approval operations, records and lifecycle events.

use layerx_agent_api::identity::TenantId;
use layerx_agent_api::prepare::Disclosure;
use layerx_agent_api::TimestampSeconds;

pub const CONTRACT_INTRODUCED: (u16, u16) = (1, 1);
pub const ENFORCEMENT_NOTICE: &str = "An approval hold is a daemon-enforced restriction. It confers no protocol authority, and bypassing the daemon bypasses the restriction.";

#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ApprovalId([u8; 32]);

impl ApprovalId {
    #[must_use]
    pub const fn new(bytes: [u8; 32]) -> Self {
        Self(bytes)
    }

    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecisionKey(String);

impl DecisionKey {
    /// Constructs the caller-owned idempotency key for one approval decision.
    ///
    /// # Errors
    ///
    /// Refuses empty values, values longer than 255 bytes and embedded NUL bytes.
    pub fn new(value: impl Into<String>) -> Result<Self, ApprovalContractError> {
        let value = value.into();
        if value.is_empty() || value.len() > 255 || value.as_bytes().contains(&0) {
            return Err(ApprovalContractError::InvalidIdempotencyKey);
        }
        Ok(Self(value))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalState {
    Held,
    Granted,
    Rejected,
    Expired,
    Defective,
}

impl ApprovalState {
    pub const ALL: &'static [Self] = &[
        Self::Held,
        Self::Granted,
        Self::Rejected,
        Self::Expired,
        Self::Defective,
    ];

    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            Self::Held => "Held",
            Self::Granted => "Granted",
            Self::Rejected => "Rejected",
            Self::Expired => "Expired",
            Self::Defective => "Defective",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalDecisionOutcome {
    Granted,
    Rejected,
    Expired,
    Defective,
    AlreadyDecided,
    Conflict,
}

impl ApprovalDecisionOutcome {
    pub const ALL: &'static [Self] = &[
        Self::Granted,
        Self::Rejected,
        Self::Expired,
        Self::Defective,
        Self::AlreadyDecided,
        Self::Conflict,
    ];

    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            Self::Granted => "Granted",
            Self::Rejected => "Rejected",
            Self::Expired => "Expired",
            Self::Defective => "Defective",
            Self::AlreadyDecided => "AlreadyDecided",
            Self::Conflict => "Conflict",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalEventKind {
    Created,
    Granted,
    Rejected,
    Expired,
    Defective,
}

impl ApprovalEventKind {
    pub const ALL: &'static [Self] = &[
        Self::Created,
        Self::Granted,
        Self::Rejected,
        Self::Expired,
        Self::Defective,
    ];

    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            Self::Created => "Created",
            Self::Granted => "Granted",
            Self::Rejected => "Rejected",
            Self::Expired => "Expired",
            Self::Defective => "Defective",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalEnforcement {
    DaemonOnly,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HoldReason {
    pub code: String,
    pub message: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalRecord {
    pub approval_id: ApprovalId,
    pub tenant: TenantId,
    pub held_activity: Disclosure,
    pub canonical_bytes_digest: [u8; 32],
    pub hold_reason: HoldReason,
    pub created_at: TimestampSeconds,
    pub expires_at: TimestampSeconds,
    pub state: ApprovalState,
    pub enforcement: ApprovalEnforcement,
    pub authority_notice: &'static str,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalPage {
    pub approvals: Vec<ApprovalRecord>,
    pub next_cursor: Option<ApprovalId>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalListRequest {
    pub tenant: TenantId,
    pub cursor: Option<ApprovalId>,
    pub page_limit: u16,
}

impl ApprovalListRequest {
    /// Builds one bounded tenant-scoped page request.
    ///
    /// # Errors
    ///
    /// Refuses a zero limit or a page larger than the daemon contract maximum.
    pub fn new(
        tenant: TenantId,
        cursor: Option<ApprovalId>,
        page_limit: u16,
    ) -> Result<Self, ApprovalContractError> {
        if page_limit == 0 || page_limit > 100 {
            return Err(ApprovalContractError::InvalidPageLimit);
        }
        Ok(Self {
            tenant,
            cursor,
            page_limit,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalGetRequest {
    pub tenant: TenantId,
    pub approval_id: ApprovalId,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalApproveRequest {
    pub tenant: TenantId,
    pub approval_id: ApprovalId,
    pub idempotency_key: DecisionKey,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalRejectRequest {
    pub tenant: TenantId,
    pub approval_id: ApprovalId,
    pub idempotency_key: DecisionKey,
    pub reason: String,
}

impl ApprovalRejectRequest {
    /// Builds a rejection with an explicit human reason.
    ///
    /// # Errors
    ///
    /// Refuses an empty reason.
    pub fn new(
        tenant: TenantId,
        approval_id: ApprovalId,
        idempotency_key: DecisionKey,
        reason: impl Into<String>,
    ) -> Result<Self, ApprovalContractError> {
        let reason = reason.into();
        if reason.is_empty() {
            return Err(ApprovalContractError::EmptyRejectionReason);
        }
        Ok(Self {
            tenant,
            approval_id,
            idempotency_key,
            reason,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalDecision {
    pub outcome: ApprovalDecisionOutcome,
    pub submission_ref: Option<[u8; 32]>,
    pub winning_outcome: Option<ApprovalDecisionOutcome>,
    pub enforcement: ApprovalEnforcement,
    pub authority_notice: &'static str,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalEventDetails {
    Created {
        hold_reason: HoldReason,
        expires_at: TimestampSeconds,
    },
    Granted {
        submission_ref: [u8; 32],
    },
    Rejected {
        reason: String,
    },
    Expired {
        deterministic_expiry: bool,
    },
    Defective {
        defect_code: String,
    },
}

impl ApprovalEventDetails {
    #[must_use]
    pub const fn kind(&self) -> ApprovalEventKind {
        match self {
            Self::Created { .. } => ApprovalEventKind::Created,
            Self::Granted { .. } => ApprovalEventKind::Granted,
            Self::Rejected { .. } => ApprovalEventKind::Rejected,
            Self::Expired { .. } => ApprovalEventKind::Expired,
            Self::Defective { .. } => ApprovalEventKind::Defective,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalLifecycleEvent {
    pub event_id: [u8; 32],
    pub tenant: TenantId,
    pub approval_id: ApprovalId,
    pub at: TimestampSeconds,
    pub record_digest: [u8; 32],
    pub details: ApprovalEventDetails,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalContractError {
    InvalidIdempotencyKey,
    InvalidPageLimit,
    EmptyRejectionReason,
}
