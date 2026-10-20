//! Authenticated, digest-bound decisions over agent approval holds.

use std::fmt::{Display, Formatter, Write as _};

use layerx_agent_api::track::{SubmissionState, TrackedSubmission};
use serde::Serialize;
use sha2::{Digest as _, Sha256};

use crate::audit::{
    ApprovalOutcome as AuditApprovalOutcome, AuditChain, AuditError, AuditEvent,
    StepUpEvidence as AuditStepUpEvidence,
};
use crate::auth::{
    AccessDecision, AuthError, AuthorizationRequest, OperationClass, OperationDigest, Passkeys,
    StepUpEvidence,
};
use crate::store::{EvidenceRef, PrincipalScope, RowKey, StoreError, Table};
use crate::trace::TraceId;

use super::{AgentApprovalRecord, ApprovalBoundary, ApprovalBoundaryError};

const EVIDENCE_DOMAIN: &[u8] = b"layerx-human-approval-decision-evidence/v1";
const ROW_DOMAIN: &[u8] = b"layerx-human-approval-decision-row/v1";
const IDEMPOTENCY_LIMIT: usize = 255;

/// The user action requested of the agent approval module.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum DecisionAction {
    Approve,
    Reject,
}

/// The terminal state established by the agent approval authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AgentDecisionStatus {
    Approved { submission_ref: Option<[u8; 32]> },
    Rejected,
    Expired,
    Defective,
}

/// Whether this request won, repeated its own decision, or observed another winner.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum AgentDecisionResolution {
    Applied,
    Repeated,
    AlreadyDecided,
}

/// Normalized response from the real agent approval module.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AgentDecision {
    pub status: AgentDecisionStatus,
    pub resolution: AgentDecisionResolution,
}

/// Mutating extension of the authenticated agent approval boundary.
pub trait AgentDecisionBoundary: ApprovalBoundary {
    /// Grants exactly the held digest through the approval module.
    ///
    /// # Errors
    ///
    /// Returns a typed boundary failure when no authoritative outcome is available.
    fn approve(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<AgentDecision, ApprovalBoundaryError>;

    /// Rejects exactly the named hold through the approval module.
    ///
    /// # Errors
    ///
    /// Returns a typed boundary failure when no authoritative outcome is available.
    fn reject(
        &mut self,
        approval_id: [u8; 32],
        held_digest: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
    ) -> Result<AgentDecision, ApprovalBoundaryError>;
}

/// Honest decision response suitable for every shell.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecisionResult {
    approval_id: [u8; 32],
    held_digest: [u8; 32],
    action: DecisionAction,
    status: AgentDecisionStatus,
    resolution: AgentDecisionResolution,
    tracking: Option<TrackedSubmission>,
    evidence: EvidenceRef,
}

impl DecisionResult {
    #[must_use]
    pub const fn approval_id(&self) -> [u8; 32] {
        self.approval_id
    }

    #[must_use]
    pub const fn held_digest(&self) -> [u8; 32] {
        self.held_digest
    }

    #[must_use]
    pub const fn action(&self) -> DecisionAction {
        self.action
    }

    #[must_use]
    pub const fn status(&self) -> AgentDecisionStatus {
        self.status
    }

    #[must_use]
    pub const fn resolution(&self) -> AgentDecisionResolution {
        self.resolution
    }

    #[must_use]
    pub const fn tracking(&self) -> Option<&TrackedSubmission> {
        self.tracking.as_ref()
    }

    #[must_use]
    pub const fn evidence(&self) -> &EvidenceRef {
        &self.evidence
    }

    /// Whether this response reports another device's durable winner.
    #[must_use]
    pub const fn already_decided(&self) -> bool {
        matches!(self.resolution, AgentDecisionResolution::AlreadyDecided)
    }

    /// Immediate no-movement confirmation for terminal non-release outcomes.
    #[must_use]
    pub const fn nothing_moved(&self) -> Option<&'static str> {
        match self.status {
            AgentDecisionStatus::Rejected
            | AgentDecisionStatus::Expired
            | AgentDecisionStatus::Defective => Some("Nothing moved."),
            AgentDecisionStatus::Approved { .. } => None,
        }
    }
}

/// Stateless orchestration over the Human auth, agent approval and audit authorities.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Decisions;

impl Decisions {
    /// Approves one hold after a fresh passkey ceremony bound to its exact digest.
    ///
    /// # Errors
    ///
    /// Refuses malformed holds, invalid or stale step-up evidence, reauthentication,
    /// unavailable agent decisions, missing release tracking and audit persistence failures.
    #[allow(clippy::too_many_arguments)]
    pub fn approve<B: AgentDecisionBoundary>(
        scope: &mut PrincipalScope<'_>,
        passkeys: &Passkeys,
        access_token: &str,
        csrf_token: &str,
        step_up: &StepUpEvidence,
        boundary: &mut B,
        approval_id: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
        now: u64,
        audit: &mut AuditChain,
        trace: &TraceId,
    ) -> Result<DecisionResult, DecisionError> {
        validate_idempotency_key(idempotency_key)?;
        let hold = load_hold(boundary, approval_id, current_sequence)?;
        match passkeys.authorize(
            scope,
            access_token,
            Some(csrf_token),
            &AuthorizationRequest {
                operation: OperationClass::Approval,
                digest: Some(OperationDigest::new(hold.canonical_bytes_digest)),
                step_up: Some(step_up),
                intended_destination: "/app/approvals",
            },
            now,
        )? {
            AccessDecision::Authorized(_) => {}
            AccessDecision::Reauthenticate { .. } => {
                return Err(DecisionError::ReauthenticationRequired)
            }
        }
        let decision = boundary.approve(
            approval_id,
            hold.canonical_bytes_digest,
            idempotency_key,
            current_sequence,
        )?;
        let tracking = tracking(boundary, decision)?;
        let ceremony_digest = step_up_digest(step_up);
        record(
            scope,
            audit,
            trace,
            now,
            current_sequence,
            approval_id,
            hold.canonical_bytes_digest,
            idempotency_key,
            DecisionAction::Approve,
            decision,
            Some(ceremony_digest),
            tracking,
        )
    }

    /// Rejects one hold under an authenticated, CSRF-bound passkey session.
    ///
    /// # Errors
    ///
    /// Refuses malformed holds, invalid sessions, unavailable agent decisions and
    /// audit persistence failures.
    #[allow(clippy::too_many_arguments)]
    pub fn reject<B: AgentDecisionBoundary>(
        scope: &mut PrincipalScope<'_>,
        passkeys: &Passkeys,
        access_token: &str,
        csrf_token: &str,
        boundary: &mut B,
        approval_id: [u8; 32],
        idempotency_key: &str,
        current_sequence: u64,
        now: u64,
        audit: &mut AuditChain,
        trace: &TraceId,
    ) -> Result<DecisionResult, DecisionError> {
        validate_idempotency_key(idempotency_key)?;
        let hold = load_hold(boundary, approval_id, current_sequence)?;
        match passkeys.authorize(
            scope,
            access_token,
            Some(csrf_token),
            &AuthorizationRequest {
                operation: OperationClass::MoneyMovement,
                digest: None,
                step_up: None,
                intended_destination: "/app/approvals",
            },
            now,
        )? {
            AccessDecision::Authorized(_) => {}
            AccessDecision::Reauthenticate { .. } => {
                return Err(DecisionError::ReauthenticationRequired)
            }
        }
        let decision = boundary.reject(
            approval_id,
            hold.canonical_bytes_digest,
            idempotency_key,
            current_sequence,
        )?;
        let tracking = tracking(boundary, decision)?;
        record(
            scope,
            audit,
            trace,
            now,
            current_sequence,
            approval_id,
            hold.canonical_bytes_digest,
            idempotency_key,
            DecisionAction::Reject,
            decision,
            None,
            tracking,
        )
    }
}

fn validate_idempotency_key(value: &str) -> Result<(), DecisionError> {
    if value.is_empty() || value.len() > IDEMPOTENCY_LIMIT || value.as_bytes().contains(&0) {
        Err(DecisionError::InvalidIdempotencyKey)
    } else {
        Ok(())
    }
}

fn load_hold<B: AgentDecisionBoundary>(
    boundary: &mut B,
    approval_id: [u8; 32],
    current_sequence: u64,
) -> Result<AgentApprovalRecord, DecisionError> {
    let hold = boundary.approval(approval_id, current_sequence)?;
    if hold.approval_id != approval_id
        || hold.canonical_bytes_digest == [0; 32]
        || hold.held_activity.canonical_digest != hold.canonical_bytes_digest
    {
        return Err(DecisionError::DefectiveHold);
    }
    Ok(hold)
}

fn tracking<B: AgentDecisionBoundary>(
    boundary: &mut B,
    decision: AgentDecision,
) -> Result<Option<TrackedSubmission>, DecisionError> {
    match decision.status {
        AgentDecisionStatus::Approved {
            submission_ref: Some(reference),
        } => Ok(Some(boundary.track_released(reference)?)),
        AgentDecisionStatus::Approved {
            submission_ref: None,
        } if decision.resolution == AgentDecisionResolution::AlreadyDecided => Ok(None),
        AgentDecisionStatus::Approved {
            submission_ref: None,
        } => Err(DecisionError::MissingSubmissionReference),
        AgentDecisionStatus::Rejected
        | AgentDecisionStatus::Expired
        | AgentDecisionStatus::Defective => Ok(None),
    }
}

#[allow(clippy::too_many_arguments)]
fn record(
    scope: &mut PrincipalScope<'_>,
    audit: &mut AuditChain,
    trace: &TraceId,
    now: u64,
    current_sequence: u64,
    approval_id: [u8; 32],
    held_digest: [u8; 32],
    idempotency_key: &str,
    action: DecisionAction,
    decision: AgentDecision,
    step_up_digest: Option<[u8; 32]>,
    tracking: Option<TrackedSubmission>,
) -> Result<DecisionResult, DecisionError> {
    let evidence_bytes = decision_evidence(
        current_sequence,
        approval_id,
        held_digest,
        idempotency_key,
        action,
        decision,
        step_up_digest,
        tracking.as_ref(),
    )?;
    let evidence_key = decision_row(
        trace,
        action,
        approval_id,
        idempotency_key,
        now,
        audit.head().length(),
    )?;
    scope.put(Table::Journeys, evidence_key.clone(), now, evidence_bytes)?;
    let evidence = EvidenceRef::new(Table::Journeys, evidence_key);
    audit.append(
        scope,
        now,
        trace,
        &AuditEvent::ApprovalDecision {
            hold_digest: held_digest,
            step_up: step_up_digest.map_or(AuditStepUpEvidence::NotRequired, |ceremony_digest| {
                AuditStepUpEvidence::Fresh { ceremony_digest }
            }),
            outcome: audit_outcome(decision.status),
        },
        std::slice::from_ref(&evidence),
    )?;
    Ok(DecisionResult {
        approval_id,
        held_digest,
        action,
        status: decision.status,
        resolution: decision.resolution,
        tracking,
        evidence,
    })
}

fn audit_outcome(status: AgentDecisionStatus) -> AuditApprovalOutcome {
    match status {
        AgentDecisionStatus::Approved { .. } => AuditApprovalOutcome::Approved,
        AgentDecisionStatus::Rejected => AuditApprovalOutcome::Rejected,
        AgentDecisionStatus::Expired => AuditApprovalOutcome::Expired,
        AgentDecisionStatus::Defective => AuditApprovalOutcome::Defective,
    }
}

fn step_up_digest(evidence: &StepUpEvidence) -> [u8; 32] {
    let mut digest = Sha256::new();
    digest.update(EVIDENCE_DOMAIN);
    digest.update(evidence.challenge_id().as_bytes());
    digest.update(evidence.confirms().bytes());
    digest.update(evidence.passkey_id().as_bytes());
    digest.update(evidence.completed_at().to_be_bytes());
    digest.update(evidence.expires_at().to_be_bytes());
    digest.finalize().into()
}

fn decision_row(
    trace: &TraceId,
    action: DecisionAction,
    approval_id: [u8; 32],
    idempotency_key: &str,
    now: u64,
    audit_sequence: u64,
) -> Result<RowKey, DecisionError> {
    let mut digest = Sha256::new();
    digest.update(ROW_DOMAIN);
    digest.update(trace.as_str().as_bytes());
    digest.update([action_code(action)]);
    digest.update(approval_id);
    digest.update(idempotency_key.as_bytes());
    digest.update(now.to_be_bytes());
    digest.update(audit_sequence.to_be_bytes());
    Ok(RowKey::new(format!(
        "approval-decision-{}",
        hex(digest.finalize())
    ))?)
}

#[derive(Serialize)]
struct StoredDecision<'a> {
    version: u8,
    action: DecisionAction,
    resolution: AgentDecisionResolution,
    status: &'static str,
    approval_id: String,
    held_digest: String,
    idempotency_digest: String,
    step_up_evidence_digest: Option<String>,
    current_sequence: u64,
    tracking: Option<StoredTracking<'a>>,
}

#[derive(Serialize)]
struct StoredTracking<'a> {
    submission_ref: &'a str,
    state: &'static str,
    receipt_ref: Option<&'a str>,
    rung: u8,
    evidence: Vec<StoredAgentEvidence>,
}

#[derive(Serialize)]
struct StoredAgentEvidence {
    kind: String,
    digest: String,
}

#[allow(clippy::too_many_arguments)]
fn decision_evidence(
    current_sequence: u64,
    approval_id: [u8; 32],
    held_digest: [u8; 32],
    idempotency_key: &str,
    action: DecisionAction,
    decision: AgentDecision,
    step_up_digest: Option<[u8; 32]>,
    tracking: Option<&TrackedSubmission>,
) -> Result<Vec<u8>, DecisionError> {
    let mut idempotency_digest = Sha256::new();
    idempotency_digest.update(idempotency_key.as_bytes());
    let stored_tracking = tracking.map(|value| StoredTracking {
        submission_ref: value.submission_ref.as_str(),
        state: value.state.name(),
        receipt_ref: match &value.state {
            SubmissionState::Executed { receipt_ref }
            | SubmissionState::Committed { receipt_ref } => Some(receipt_ref.as_str()),
            SubmissionState::Prepared
            | SubmissionState::Signed
            | SubmissionState::Queued
            | SubmissionState::Submitted
            | SubmissionState::Unknown
            | SubmissionState::Failed { .. }
            | SubmissionState::Expired => None,
        },
        rung: value.rung.wire_rank(),
        evidence: value
            .evidence
            .iter()
            .map(|evidence| StoredAgentEvidence {
                kind: evidence.kind.clone(),
                digest: hex(evidence.digest),
            })
            .collect(),
    });
    Ok(serde_json::to_vec(&StoredDecision {
        version: 1,
        action,
        resolution: decision.resolution,
        status: status_name(decision.status),
        approval_id: hex(approval_id),
        held_digest: hex(held_digest),
        idempotency_digest: hex(idempotency_digest.finalize()),
        step_up_evidence_digest: step_up_digest.map(hex),
        current_sequence,
        tracking: stored_tracking,
    })?)
}

const fn action_code(action: DecisionAction) -> u8 {
    match action {
        DecisionAction::Approve => 1,
        DecisionAction::Reject => 2,
    }
}

const fn status_name(status: AgentDecisionStatus) -> &'static str {
    match status {
        AgentDecisionStatus::Approved { .. } => "approved",
        AgentDecisionStatus::Rejected => "rejected",
        AgentDecisionStatus::Expired => "expired",
        AgentDecisionStatus::Defective => "defective",
    }
}

fn hex(bytes: impl AsRef<[u8]>) -> String {
    let bytes = bytes.as_ref();
    let mut output = String::with_capacity(bytes.len().saturating_mul(2));
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

/// Typed refusal from the Human approval decision path.
#[derive(Debug)]
pub enum DecisionError {
    InvalidIdempotencyKey,
    DefectiveHold,
    ReauthenticationRequired,
    MissingSubmissionReference,
    Agent(ApprovalBoundaryError),
    Auth(AuthError),
    Store(StoreError),
    Audit(AuditError),
    Encode(serde_json::Error),
}

impl Display for DecisionError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidIdempotencyKey => formatter.write_str("invalid approval idempotency key"),
            Self::DefectiveHold => formatter.write_str("agent approval hold is defective"),
            Self::ReauthenticationRequired => {
                formatter.write_str("approval decision requires reauthentication")
            }
            Self::MissingSubmissionReference => {
                formatter.write_str("approved hold has no released submission reference")
            }
            Self::Agent(error) => write!(formatter, "approval agent failure: {error:?}"),
            Self::Auth(error) => write!(formatter, "approval authentication failure: {error}"),
            Self::Store(error) => write!(formatter, "approval evidence failure: {error}"),
            Self::Audit(error) => write!(formatter, "approval audit failure: {error}"),
            Self::Encode(error) => write!(formatter, "approval evidence encoding failure: {error}"),
        }
    }
}

impl std::error::Error for DecisionError {}

impl From<ApprovalBoundaryError> for DecisionError {
    fn from(value: ApprovalBoundaryError) -> Self {
        Self::Agent(value)
    }
}

impl From<AuthError> for DecisionError {
    fn from(value: AuthError) -> Self {
        Self::Auth(value)
    }
}

impl From<StoreError> for DecisionError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

impl From<AuditError> for DecisionError {
    fn from(value: AuditError) -> Self {
        Self::Audit(value)
    }
}

impl From<serde_json::Error> for DecisionError {
    fn from(value: serde_json::Error) -> Self {
        Self::Encode(value)
    }
}
