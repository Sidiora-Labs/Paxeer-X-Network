//! Event-driven projection of the agent approval module into the Human inbox.

mod decide;
mod render;

pub use decide::{
    AgentDecision, AgentDecisionBoundary, AgentDecisionResolution, AgentDecisionStatus,
    DecisionAction, DecisionError, DecisionResult, Decisions,
};
pub use render::{
    ApprovalActivityClass, ApprovalPresentation, DisclosureRenderError, DisclosureRenderer,
    RenderedApproval, RenderedApprovalFacts, RenderedCounterparty, UnrenderableApproval,
};

use std::collections::BTreeMap;
use std::fmt::{Display, Formatter, Write as _};

use layerx_agent_api::prepare::Disclosure;
use layerx_agent_api::track::TrackedSubmission;
use layerx_agent_api::verify::Level;
use sha2::{Digest as _, Sha256};

use crate::audit::{AuditChain, AuditError};
use crate::notify::{
    AgentId, ApprovalId, Dispatcher, Event as NotificationEvent, Money, NotifyError,
};
use crate::store::PrincipalScope;
use crate::trace::TraceId;

const ID_DOMAIN: &[u8] = b"layerx-human-approval-agent/v1";

/// Lifecycle vocabulary received from the agent approval event stream.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalEventKind {
    Created,
    Approved,
    Rejected,
    Expired,
}

/// One ordered agent-generated lifecycle event.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApprovalEvent {
    pub sequence: u64,
    pub approval_id: [u8; 32],
    pub disclosure_digest: [u8; 32],
    pub kind: ApprovalEventKind,
    pub observed_at: u64,
}

impl ApprovalEvent {
    /// Decodes the canonical lifecycle envelope emitted by the agent daemon's
    /// approval event stream.
    ///
    /// # Errors
    ///
    /// Refuses unknown versions/kinds, truncated fields, trailing bytes and
    /// non-UTF-8 decision principals.
    pub fn decode_agent_stream(
        canonical_bytes: &[u8],
        observed_at: u64,
    ) -> Result<Self, InboxError> {
        const FIXED_LENGTH: usize = 80;
        if canonical_bytes.len() < FIXED_LENGTH
            || &canonical_bytes[..4] != b"LXAE"
            || canonical_bytes[4] != 1
        {
            return Err(InboxError::MalformedEvent);
        }
        let kind = match canonical_bytes[5] {
            1 => ApprovalEventKind::Created,
            2 => ApprovalEventKind::Approved,
            3 => ApprovalEventKind::Rejected,
            4 => ApprovalEventKind::Expired,
            _ => return Err(InboxError::MalformedEvent),
        };
        let sequence = u64::from_be_bytes(
            canonical_bytes[6..14]
                .try_into()
                .map_err(|_| InboxError::MalformedEvent)?,
        );
        let approval_id = canonical_bytes[14..46]
            .try_into()
            .map_err(|_| InboxError::MalformedEvent)?;
        let disclosure_digest = canonical_bytes[46..78]
            .try_into()
            .map_err(|_| InboxError::MalformedEvent)?;
        let principal_length = usize::from(u16::from_be_bytes(
            canonical_bytes[78..80]
                .try_into()
                .map_err(|_| InboxError::MalformedEvent)?,
        ));
        if canonical_bytes.len() != FIXED_LENGTH.saturating_add(principal_length)
            || std::str::from_utf8(&canonical_bytes[FIXED_LENGTH..]).is_err()
        {
            return Err(InboxError::MalformedEvent);
        }
        Ok(Self {
            sequence,
            approval_id,
            disclosure_digest,
            kind,
            observed_at,
        })
    }
}

/// Agent approval-module state, including the released activity reference.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AgentApprovalState {
    AwaitingApproval,
    Approved { submission_ref: [u8; 32] },
    Rejected,
    Expired,
    Defective,
}

/// Exact hold returned by the authenticated agent approval module.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AgentApprovalRecord {
    pub approval_id: [u8; 32],
    pub held_activity: Disclosure,
    pub canonical_bytes_digest: [u8; 32],
    pub hold_reason_code: String,
    pub hold_reason: String,
    pub created_at_sequence: u64,
    pub expires_at_sequence: u64,
    pub state: AgentApprovalState,
}

/// A contextual budget value backed by an agent-layer verified read.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VerifiedBudgetAfter {
    pub remaining: u128,
    pub level: Level,
    pub evidence_digest: [u8; 32],
    pub observed_at_sequence: u64,
}

/// Agent contract used by the inbox. Lifecycle discovery happens exclusively
/// through ordered events; point calls only hydrate the event's named hold.
pub trait ApprovalBoundary {
    /// Hydrates the hold named by one lifecycle event at that event's head.
    ///
    /// # Errors
    ///
    /// Returns a typed agent-boundary failure when the hold is unavailable or
    /// cannot be authenticated.
    fn approval(
        &mut self,
        approval_id: [u8; 32],
        at_sequence: u64,
    ) -> Result<AgentApprovalRecord, ApprovalBoundaryError>;

    /// Reads the post-activity budget context through the verified agent read.
    ///
    /// # Errors
    ///
    /// Returns a typed agent-boundary failure when evidence is unavailable or
    /// verification fails.
    fn verified_budget_after(
        &mut self,
        hold: &AgentApprovalRecord,
        at_sequence: u64,
    ) -> Result<VerifiedBudgetAfter, ApprovalBoundaryError>;

    /// Starts tracking the exact preparation released by a granted approval.
    ///
    /// # Errors
    ///
    /// Returns a typed agent-boundary failure when the release reference is
    /// unknown or tracking cannot be started.
    fn track_released(
        &mut self,
        submission_ref: [u8; 32],
    ) -> Result<TrackedSubmission, ApprovalBoundaryError>;
}

/// Stable agent-boundary failures that never become inbox content.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalBoundaryError {
    Unavailable,
    NotFound,
    VerificationFailed,
    Corrupt,
}

/// Honest item lifecycle rendered by all inbox surfaces.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum InboxState {
    AwaitingApproval,
    Approved { tracking: TrackedSubmission },
    Rejected,
    Expired,
}

impl InboxState {
    /// Whether the approve control may be rendered.
    #[must_use]
    pub const fn can_approve(&self) -> bool {
        matches!(self, Self::AwaitingApproval)
    }

    /// Honest terminal statement for states in which no value moved.
    #[must_use]
    pub const fn nothing_moved(&self) -> Option<&'static str> {
        match self {
            Self::Rejected | Self::Expired => Some("Nothing moved."),
            Self::AwaitingApproval | Self::Approved { .. } => None,
        }
    }
}

/// One disclosure-derived inbox item plus separately labelled verified context.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InboxItem {
    approval_id: ApprovalId,
    agent: String,
    counterparty: String,
    amount: u128,
    asset: String,
    fee_limit: u128,
    activity_expiry: u64,
    hold_expiry: u64,
    reason_code: String,
    reason: String,
    disclosure_digest: [u8; 32],
    budget_after: VerifiedBudgetAfter,
    state: InboxState,
}

impl InboxItem {
    #[must_use]
    pub const fn approval_id(&self) -> &ApprovalId {
        &self.approval_id
    }
    #[must_use]
    pub fn agent(&self) -> &str {
        &self.agent
    }
    #[must_use]
    pub fn counterparty(&self) -> &str {
        &self.counterparty
    }
    #[must_use]
    pub const fn amount(&self) -> u128 {
        self.amount
    }
    #[must_use]
    pub fn asset(&self) -> &str {
        &self.asset
    }
    #[must_use]
    pub const fn fee_limit(&self) -> u128 {
        self.fee_limit
    }
    #[must_use]
    pub const fn activity_expiry(&self) -> u64 {
        self.activity_expiry
    }
    #[must_use]
    pub const fn hold_expiry(&self) -> u64 {
        self.hold_expiry
    }
    #[must_use]
    pub fn reason_code(&self) -> &str {
        &self.reason_code
    }
    #[must_use]
    pub fn reason(&self) -> &str {
        &self.reason
    }
    #[must_use]
    pub const fn disclosure_digest(&self) -> [u8; 32] {
        self.disclosure_digest
    }
    #[must_use]
    pub const fn budget_after(&self) -> VerifiedBudgetAfter {
        self.budget_after
    }
    #[must_use]
    pub const fn state(&self) -> &InboxState {
        &self.state
    }

    /// Sequence-relative countdown that never wraps after expiry.
    #[must_use]
    pub const fn remaining(&self, current_sequence: u64) -> u64 {
        self.hold_expiry.saturating_sub(current_sequence)
    }
}

/// Event-driven approval inbox with one freshness coordinate shared by list
/// and count surfaces.
#[derive(Debug)]
pub struct Inbox {
    items: BTreeMap<[u8; 32], InboxItem>,
    next_sequence: u64,
    last_observed: Option<u64>,
    freshness_bound: u64,
}

impl Inbox {
    /// Creates an empty projection that must consume the event stream from its
    /// first sequence. A zero freshness bound would make every view stale.
    ///
    /// # Errors
    ///
    /// Refuses a zero freshness bound.
    pub const fn new(first_sequence: u64, freshness_bound: u64) -> Result<Self, InboxError> {
        if freshness_bound == 0 {
            return Err(InboxError::InvalidFreshnessBound);
        }
        Ok(Self {
            items: BTreeMap::new(),
            next_sequence: first_sequence,
            last_observed: None,
            freshness_bound,
        })
    }

    /// Applies an ordered batch emitted by the real agent approval stream.
    /// Created holds dispatch the existing approval notification with its
    /// stable deep link; later events replace state from the module itself.
    ///
    /// # Errors
    ///
    /// Refuses gaps/replays, digest changes, unverified context and malformed
    /// movement disclosures. The cursor advances only after the item converges.
    pub fn consume(
        &mut self,
        events: &[ApprovalEvent],
        agent: &mut dyn ApprovalBoundary,
        scope: &mut PrincipalScope<'_>,
        audit: &mut AuditChain,
        trace: &TraceId,
    ) -> Result<(), InboxError> {
        for event in events {
            if event.sequence != self.next_sequence {
                return Err(InboxError::EventOrder {
                    expected: self.next_sequence,
                    actual: event.sequence,
                });
            }
            let record = agent.approval(event.approval_id, event.sequence)?;
            if record.approval_id != event.approval_id {
                return Err(InboxError::RecordMismatch);
            }
            let existing = self.items.get(&event.approval_id);
            if matches!(event.kind, ApprovalEventKind::Created) != existing.is_none() {
                return Err(InboxError::LifecycleMismatch);
            }
            let existing_digest = existing.map(InboxItem::disclosure_digest);
            let mut item = build_item(&record, agent, event.sequence)?;
            if event.disclosure_digest != item.disclosure_digest
                || existing_digest.is_some_and(|digest| digest != item.disclosure_digest)
            {
                return Err(InboxError::DisclosureChanged);
            }
            item.state = state(&record, agent)?;
            if event.kind == ApprovalEventKind::Created {
                if !matches!(item.state, InboxState::AwaitingApproval) {
                    return Err(InboxError::LifecycleMismatch);
                }
                let notification = NotificationEvent::ApprovalWaiting {
                    approval_id: item.approval_id.clone(),
                    agent_id: notification_agent_id(&item.agent)?,
                    money: Some(Money::new(item.amount, item.asset.clone())?),
                };
                Dispatcher::dispatch(scope, audit, event.observed_at, trace, &notification)?;
            } else if event_kind(&item.state) != event.kind {
                return Err(InboxError::LifecycleMismatch);
            }
            self.items.insert(event.approval_id, item);
            self.last_observed = Some(event.sequence);
            self.next_sequence = self
                .next_sequence
                .checked_add(1)
                .ok_or(InboxError::SequenceOverflow)?;
        }
        Ok(())
    }

    /// Returns the exact item set and the count derived from that same set.
    ///
    /// # Errors
    ///
    /// Refuses to serve a projection older than its declared freshness bound.
    pub fn snapshot(&self, current_sequence: u64) -> Result<InboxSnapshot<'_>, InboxError> {
        let observed = self.last_observed.ok_or(InboxError::Stale)?;
        if current_sequence.saturating_sub(observed) > self.freshness_bound {
            return Err(InboxError::Stale);
        }
        let awaiting = self
            .items
            .values()
            .filter(|item| item.state.can_approve())
            .count();
        Ok(InboxSnapshot {
            items: self.items.values().collect(),
            awaiting,
            observed_sequence: observed,
        })
    }

    #[must_use]
    pub const fn next_sequence(&self) -> u64 {
        self.next_sequence
    }
}

/// One internally consistent list/count view.
#[derive(Clone, Debug)]
pub struct InboxSnapshot<'a> {
    items: Vec<&'a InboxItem>,
    awaiting: usize,
    observed_sequence: u64,
}

impl InboxSnapshot<'_> {
    #[must_use]
    pub fn items(&self) -> &[&InboxItem] {
        &self.items
    }
    #[must_use]
    pub const fn awaiting_count(&self) -> usize {
        self.awaiting
    }
    #[must_use]
    pub const fn observed_sequence(&self) -> u64 {
        self.observed_sequence
    }
}

fn build_item(
    record: &AgentApprovalRecord,
    agent: &mut dyn ApprovalBoundary,
    sequence: u64,
) -> Result<InboxItem, InboxError> {
    let disclosure = &record.held_activity;
    if disclosure.canonical_digest != record.canonical_bytes_digest
        || record.hold_reason_code.is_empty()
        || record.hold_reason.is_empty()
        || record.expires_at_sequence <= record.created_at_sequence
    {
        return Err(InboxError::RecordMismatch);
    }
    let [counterparty] = disclosure.counterparties.values() else {
        return Err(InboxError::UnsupportedDisclosure);
    };
    let [amount] = disclosure.amounts.values() else {
        return Err(InboxError::UnsupportedDisclosure);
    };
    if &amount.counterparty != counterparty {
        return Err(InboxError::UnsupportedDisclosure);
    }
    let budget_after = agent.verified_budget_after(record, sequence)?;
    if budget_after.level == Level::Unverified
        || budget_after.evidence_digest == [0; 32]
        || budget_after.observed_at_sequence > sequence
    {
        return Err(InboxError::UnverifiedBudget);
    }
    Ok(InboxItem {
        approval_id: approval_id(record.approval_id)?,
        agent: disclosure.actor.as_str().to_owned(),
        counterparty: counterparty.as_str().to_owned(),
        amount: amount.amount.0,
        asset: disclosure.asset.as_str().to_owned(),
        fee_limit: disclosure.fee_limit.0,
        activity_expiry: disclosure.expiry.0,
        hold_expiry: record.expires_at_sequence,
        reason_code: record.hold_reason_code.clone(),
        reason: record.hold_reason.clone(),
        disclosure_digest: disclosure.canonical_digest,
        budget_after,
        state: InboxState::AwaitingApproval,
    })
}

fn state(
    record: &AgentApprovalRecord,
    agent: &mut dyn ApprovalBoundary,
) -> Result<InboxState, InboxError> {
    match record.state {
        AgentApprovalState::AwaitingApproval => Ok(InboxState::AwaitingApproval),
        AgentApprovalState::Approved { submission_ref } => Ok(InboxState::Approved {
            tracking: agent.track_released(submission_ref)?,
        }),
        AgentApprovalState::Rejected => Ok(InboxState::Rejected),
        AgentApprovalState::Expired => Ok(InboxState::Expired),
        AgentApprovalState::Defective => Err(InboxError::DefectiveHold),
    }
}

const fn event_kind(state: &InboxState) -> ApprovalEventKind {
    match state {
        InboxState::AwaitingApproval => ApprovalEventKind::Created,
        InboxState::Approved { .. } => ApprovalEventKind::Approved,
        InboxState::Rejected => ApprovalEventKind::Rejected,
        InboxState::Expired => ApprovalEventKind::Expired,
    }
}

fn approval_id(bytes: [u8; 32]) -> Result<ApprovalId, InboxError> {
    Ok(ApprovalId::new(format!("apr_{}", hex(bytes)))?)
}

fn notification_agent_id(agent: &str) -> Result<AgentId, InboxError> {
    let mut digest = Sha256::new();
    digest.update(ID_DOMAIN);
    digest.update(agent.as_bytes());
    Ok(AgentId::new(format!("agt_{}", hex(digest.finalize())))?)
}

fn hex(bytes: impl AsRef<[u8]>) -> String {
    let bytes = bytes.as_ref();
    let mut output = String::with_capacity(bytes.len().saturating_mul(2));
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

/// Typed inbox failure.
#[derive(Debug)]
pub enum InboxError {
    Agent(ApprovalBoundaryError),
    Notify(NotifyError),
    Audit(AuditError),
    InvalidFreshnessBound,
    MalformedEvent,
    EventOrder { expected: u64, actual: u64 },
    SequenceOverflow,
    RecordMismatch,
    DisclosureChanged,
    UnsupportedDisclosure,
    UnverifiedBudget,
    LifecycleMismatch,
    DefectiveHold,
    Stale,
}

impl Display for InboxError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Agent(error) => write!(formatter, "approval agent failure: {error:?}"),
            Self::Notify(error) => write!(formatter, "approval notification failure: {error}"),
            Self::Audit(error) => write!(formatter, "approval audit failure: {error}"),
            Self::InvalidFreshnessBound => formatter.write_str("approval freshness bound is zero"),
            Self::MalformedEvent => formatter.write_str("malformed agent approval event"),
            Self::EventOrder { expected, actual } => write!(
                formatter,
                "approval event gap: expected {expected}, got {actual}"
            ),
            Self::SequenceOverflow => formatter.write_str("approval event sequence overflow"),
            Self::RecordMismatch => formatter.write_str("approval record does not match its event"),
            Self::DisclosureChanged => formatter.write_str("held approval disclosure changed"),
            Self::UnsupportedDisclosure => {
                formatter.write_str("held disclosure is not one movement")
            }
            Self::UnverifiedBudget => formatter.write_str("budget-after read is not verified"),
            Self::LifecycleMismatch => {
                formatter.write_str("approval lifecycle event disagrees with module state")
            }
            Self::DefectiveHold => formatter.write_str("agent approval hold is defective"),
            Self::Stale => formatter.write_str("approval inbox is outside its freshness bound"),
        }
    }
}

impl std::error::Error for InboxError {}
impl From<ApprovalBoundaryError> for InboxError {
    fn from(value: ApprovalBoundaryError) -> Self {
        Self::Agent(value)
    }
}
impl From<NotifyError> for InboxError {
    fn from(value: NotifyError) -> Self {
        Self::Notify(value)
    }
}
impl From<AuditError> for InboxError {
    fn from(value: AuditError) -> Self {
        Self::Audit(value)
    }
}
