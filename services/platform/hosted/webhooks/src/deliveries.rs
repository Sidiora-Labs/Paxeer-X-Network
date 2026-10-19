//! Delivery state that is honest about pending, in flight, retrying, delivered
//! and dead-lettered, and the per-attempt record behind each transition.

use serde::{Deserialize, Serialize};

use crate::events::{DeliveryId, EndpointId, EventId, EventKind, SubjectId, Verification};

/// Why one attempt did not result in acceptance.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum FailureKind {
    /// The destination could not be reached.
    Unreachable,
    /// The destination did not answer inside the attempt deadline.
    Timeout,
    /// The destination answered with a status outside the accepted range.
    Refused,
    /// The destination answered, but not with usable HTTP.
    Protocol,
    /// The destination declared the endpoint permanently gone.
    Gone,
    /// The endpoint is suspended, so the attempt was not made.
    Suspended,
}

impl FailureKind {
    /// Returns the wire word for this failure.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Unreachable => "unreachable",
            Self::Timeout => "timeout",
            Self::Refused => "refused",
            Self::Protocol => "protocol",
            Self::Gone => "gone",
            Self::Suspended => "suspended",
        }
    }

    /// Returns true when retrying can never succeed.
    #[must_use]
    pub const fn permanent(self) -> bool {
        matches!(self, Self::Gone)
    }
}

/// The exact state of one delivery. No variant claims acceptance without an
/// observed accepting status from the developer's own endpoint.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(tag = "state", rename_all = "kebab-case")]
pub enum DeliveryState {
    /// Queued and never attempted.
    Pending,
    /// An attempt is outstanding.
    InFlight {
        /// One-based attempt counter.
        attempt: u32,
        /// When the attempt started.
        started_at: u64,
    },
    /// The last attempt failed and another is scheduled.
    Retrying {
        /// Number of attempts already made.
        attempt: u32,
        /// When the next attempt becomes due.
        next_attempt_at: u64,
        /// Why the last attempt failed.
        failure: FailureKind,
        /// The refusing status, when the destination answered.
        status: Option<u16>,
    },
    /// The destination accepted the delivery.
    Delivered {
        /// The attempt that was accepted.
        attempt: u32,
        /// When acceptance was observed.
        at: u64,
        /// The accepting status.
        status: u16,
    },
    /// Attempts were exhausted and the delivery moved to the dead-letter path.
    DeadLettered {
        /// Attempts made before giving up.
        attempts: u32,
        /// When the delivery was dead-lettered.
        at: u64,
        /// Why the last attempt failed.
        failure: FailureKind,
        /// The refusing status, when the destination answered.
        status: Option<u16>,
    },
}

impl DeliveryState {
    /// Returns the wire word for this state.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Pending => "pending",
            Self::InFlight { .. } => "in-flight",
            Self::Retrying { .. } => "retrying",
            Self::Delivered { .. } => "delivered",
            Self::DeadLettered { .. } => "dead-lettered",
        }
    }

    /// Returns true when the delivery will never be attempted again.
    #[must_use]
    pub const fn terminal(self) -> bool {
        matches!(self, Self::Delivered { .. } | Self::DeadLettered { .. })
    }

    /// Returns the number of attempts already made.
    #[must_use]
    pub const fn attempts(self) -> u32 {
        match self {
            Self::Pending => 0,
            Self::InFlight { attempt, .. }
            | Self::Delivered { attempt, .. }
            | Self::Retrying { attempt, .. } => attempt,
            Self::DeadLettered { attempts, .. } => attempts,
        }
    }
}

/// One recorded attempt against a developer endpoint.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AttemptRecord {
    /// One-based attempt counter.
    pub attempt: u32,
    /// When the attempt was made.
    pub at: u64,
    /// Observed status, when the destination answered.
    pub status: Option<u16>,
    /// Failure reason, when the attempt did not succeed.
    pub failure: Option<FailureKind>,
    /// Wall-clock duration of the attempt in milliseconds.
    pub latency_ms: u64,
}

/// One at-least-once delivery of one event to one endpoint.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Delivery {
    pub(crate) id: DeliveryId,
    pub(crate) endpoint: EndpointId,
    pub(crate) event: EventId,
    pub(crate) subject: SubjectId,
    pub(crate) subject_sequence: u64,
    pub(crate) log_position: u64,
    pub(crate) created_at: u64,
    pub(crate) state: DeliveryState,
    pub(crate) attempts: Vec<AttemptRecord>,
    pub(crate) replay_of: Option<DeliveryId>,
}

impl Delivery {
    /// Borrows the delivery identifier.
    #[must_use]
    pub const fn id(&self) -> &DeliveryId {
        &self.id
    }

    /// Borrows the endpoint this delivery targets.
    #[must_use]
    pub const fn endpoint(&self) -> &EndpointId {
        &self.endpoint
    }

    /// Borrows the event being delivered.
    #[must_use]
    pub const fn event(&self) -> &EventId {
        &self.event
    }

    /// Borrows the ordering subject.
    #[must_use]
    pub const fn subject(&self) -> &SubjectId {
        &self.subject
    }

    /// Returns the position of the event inside its subject.
    #[must_use]
    pub const fn subject_sequence(&self) -> u64 {
        self.subject_sequence
    }

    /// Returns the position of the event in the durable log.
    #[must_use]
    pub const fn log_position(&self) -> u64 {
        self.log_position
    }

    /// Returns when the delivery was queued.
    #[must_use]
    pub const fn created_at(&self) -> u64 {
        self.created_at
    }

    /// Returns the exact current state.
    #[must_use]
    pub const fn state(&self) -> DeliveryState {
        self.state
    }

    /// Borrows the per-attempt history.
    #[must_use]
    pub fn attempts(&self) -> &[AttemptRecord] {
        &self.attempts
    }

    /// Borrows the delivery this one replaces, when it is a redelivery.
    #[must_use]
    pub const fn replay_of(&self) -> Option<&DeliveryId> {
        self.replay_of.as_ref()
    }

    /// Returns when this delivery next becomes eligible for an attempt, taking
    /// the crash-recovery deadline for an outstanding attempt into account.
    #[must_use]
    pub fn due_at(&self, in_flight_timeout_seconds: u64) -> Option<u64> {
        match self.state {
            DeliveryState::Pending => Some(self.created_at),
            DeliveryState::Retrying {
                next_attempt_at, ..
            } => Some(next_attempt_at),
            DeliveryState::InFlight { started_at, .. } => {
                Some(started_at.saturating_add(in_flight_timeout_seconds))
            }
            DeliveryState::Delivered { .. } | DeliveryState::DeadLettered { .. } => None,
        }
    }
}

/// One delivery as the developer dashboards render it, carrying the verification
/// status of the event it transports.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct DeliveryRecord {
    /// Delivery identifier.
    pub delivery: String,
    /// Endpoint identifier.
    pub endpoint: String,
    /// Event identifier receivers deduplicate on.
    pub event: String,
    /// Event family.
    pub kind: EventKind,
    /// Ordering subject.
    pub subject: String,
    /// Position of the event inside its subject.
    pub subject_sequence: u64,
    /// Position of the event in the durable log.
    pub log_position: u64,
    /// When the delivery was queued.
    pub created_at: u64,
    /// Exact delivery state.
    pub state: DeliveryState,
    /// Attempts made so far.
    pub attempts: Vec<AttemptRecord>,
    /// Verification level of the transported event.
    pub verification: Verification,
    /// Receipt digest backing that level, when evidence exists.
    pub receipt_digest: Option<String>,
    /// The delivery this one replaces, when it is a redelivery.
    pub replay_of: Option<String>,
}
