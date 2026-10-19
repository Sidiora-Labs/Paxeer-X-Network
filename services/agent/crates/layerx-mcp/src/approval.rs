//! Threshold selection over the daemon's digest-bound approval registry.

use layerx_agent_api::prepare::{Disclosure, Prepared};
use layerx_agentd::policy::approval::{
    self, ApprovalAuditEntry, ApprovalChoice, ApprovalContext, ApprovalRegistry, ApprovalTicket,
    ApproverId,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApprovalPolicy {
    pub amount_threshold: u128,
}

/// Whether the exact prepared disclosure must be approved before submission.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Requirement {
    NotRequired {
        disclosure_digest: [u8; 32],
        disclosed_amount: u128,
    },
    Required(Box<ApprovalTicket>),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalError {
    AmountOverflow,
    DisclosureChanged,
    Daemon(approval::ApprovalError),
}

/// Holds the prepared disclosure exactly when its total amount exceeds policy.
///
/// # Errors
///
/// Refuses arithmetic overflow or any daemon approval-hold validation failure.
pub fn require(
    registry: &ApprovalRegistry,
    policy: ApprovalPolicy,
    context: ApprovalContext,
    prepared: Prepared,
    current_sequence: u64,
) -> Result<Requirement, ApprovalError> {
    let disclosed_amount =
        prepared
            .disclosure
            .amounts
            .values()
            .iter()
            .try_fold(0_u128, |sum, amount| {
                sum.checked_add(amount.amount.0)
                    .ok_or(ApprovalError::AmountOverflow)
            })?;
    if disclosed_amount <= policy.amount_threshold {
        return Ok(Requirement::NotRequired {
            disclosure_digest: prepared.disclosure.canonical_digest,
            disclosed_amount,
        });
    }
    let expires_at_sequence = prepared.disclosure.expiry.0;
    approval::hold(
        registry,
        context,
        prepared,
        current_sequence,
        expires_at_sequence,
    )
    .map(|ticket| Requirement::Required(Box::new(ticket)))
    .map_err(ApprovalError::Daemon)
}

/// Approves only the complete disclosure returned by the daemon-held ticket.
///
/// # Errors
///
/// Refuses any altered field, invalid approver, elapsed hold, or prior decision.
pub fn approve(
    registry: &ApprovalRegistry,
    hold_id: [u8; 32],
    approver: ApproverId,
    presented: &Disclosure,
    current_sequence: u64,
) -> Result<ApprovalAuditEntry, ApprovalError> {
    decide(
        registry,
        hold_id,
        approver,
        ApprovalChoice::Approve,
        presented,
        current_sequence,
    )
}

/// Rejects only the complete disclosure returned by the daemon-held ticket.
///
/// # Errors
///
/// Refuses any altered field, invalid approver, elapsed hold, or prior decision.
pub fn reject(
    registry: &ApprovalRegistry,
    hold_id: [u8; 32],
    approver: ApproverId,
    presented: &Disclosure,
    current_sequence: u64,
) -> Result<ApprovalAuditEntry, ApprovalError> {
    decide(
        registry,
        hold_id,
        approver,
        ApprovalChoice::Reject,
        presented,
        current_sequence,
    )
}

/// Expires every elapsed hold at the explicit protocol-relative sequence.
///
/// # Errors
///
/// Returns the daemon registry's synchronization failure without approving any hold.
pub fn expire(
    registry: &ApprovalRegistry,
    current_sequence: u64,
) -> Result<Vec<ApprovalAuditEntry>, ApprovalError> {
    approval::expire(registry, current_sequence).map_err(ApprovalError::Daemon)
}

fn decide(
    registry: &ApprovalRegistry,
    hold_id: [u8; 32],
    approver: ApproverId,
    choice: ApprovalChoice,
    presented: &Disclosure,
    current_sequence: u64,
) -> Result<ApprovalAuditEntry, ApprovalError> {
    let ticket = registry
        .ticket(hold_id)
        .map_err(ApprovalError::Daemon)?
        .ok_or(ApprovalError::Daemon(approval::ApprovalError::NotFound))?;
    if &ticket.disclosure != presented {
        return Err(ApprovalError::DisclosureChanged);
    }
    approval::decide(
        registry,
        hold_id,
        approver,
        choice,
        presented.canonical_digest,
        current_sequence,
    )
    .map_err(ApprovalError::Daemon)
}
