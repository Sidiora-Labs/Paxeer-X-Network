use super::class::NotificationClass;
use super::NotifyError;

const ID_BODY_LIMIT: usize = 64;
const CURRENCY_LIMIT: usize = 12;

fn valid_id(prefix: &str, value: &str) -> bool {
    value.strip_prefix(prefix).is_some_and(|body| {
        !body.is_empty()
            && body.len() <= ID_BODY_LIMIT
            && body
                .bytes()
                .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
    })
}

/// A validated approval identifier from the agents contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ApprovalId(String);

impl ApprovalId {
    /// Validates an `apr_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("apr_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier { kind: "approval" })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated managed-agent identifier from the agents contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AgentId(String);

impl AgentId {
    /// Validates an `agt_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("agt_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier { kind: "agent" })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated journey identifier from the journeys contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct JourneyId(String);

impl JourneyId {
    /// Validates a `jrn_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("jrn_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier { kind: "journey" })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated activity entry identifier from the activity contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ActivityEntryId(String);

impl ActivityEntryId {
    /// Validates an `act_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("act_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier { kind: "activity" })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated device identifier from the identity contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DeviceId(String);

impl DeviceId {
    /// Validates a `dev_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("dev_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier { kind: "device" })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A validated notification identifier from the activity contract.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NotificationId(String);

impl NotificationId {
    /// Validates an `ntf_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("ntf_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier {
                kind: "notification",
            })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A stable source occurrence identifier used to distinguish a genuinely new
/// security or degradation event from a delivery retry.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EventId(String);

impl EventId {
    /// Validates an `evt_`-prefixed lowercase identifier.
    ///
    /// # Errors
    ///
    /// Rejects identifiers without the prefix or outside `a-z0-9`.
    pub fn new(value: impl Into<String>) -> Result<Self, NotifyError> {
        let value = value.into();
        if valid_id("evt_", &value) {
            Ok(Self(value))
        } else {
            Err(NotifyError::InvalidIdentifier {
                kind: "notification event",
            })
        }
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// A receipt-backed amount in base units with its explicit currency code.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Money {
    amount: u128,
    currency: String,
}

impl Money {
    /// Pairs base units with a currency code of uppercase ASCII letters and
    /// digits.
    ///
    /// # Errors
    ///
    /// Rejects empty, oversize and out-of-charset currency codes.
    pub fn new(amount: u128, currency: impl Into<String>) -> Result<Self, NotifyError> {
        let currency = currency.into();
        let valid = !currency.is_empty()
            && currency.len() <= CURRENCY_LIMIT
            && currency
                .bytes()
                .all(|byte| byte.is_ascii_uppercase() || byte.is_ascii_digit());
        if valid {
            Ok(Self { amount, currency })
        } else {
            Err(NotifyError::InvalidCurrency)
        }
    }

    /// Returns the amount in base units.
    #[must_use]
    pub const fn amount(&self) -> u128 {
        self.amount
    }

    /// Returns the currency code.
    #[must_use]
    pub fn currency(&self) -> &str {
        &self.currency
    }

    pub(crate) fn render(&self) -> String {
        format!("{} {}", self.amount, self.currency)
    }
}

/// Whether a finished journey completed or failed.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum JourneyOutcome {
    Completed,
    Failed,
}

impl JourneyOutcome {
    /// Returns the contract name of the outcome.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Completed => "completed",
            Self::Failed => "failed",
        }
    }
}

/// Which side of the plane a degradation notice reports.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DegradedComponent {
    Plane,
    AgentLayer,
    Paxeer,
}

impl DegradedComponent {
    /// Returns the contract name of the component.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Plane => "plane",
            Self::AgentLayer => "agent-layer",
            Self::Paxeer => "paxeer",
        }
    }
}

/// One notifiable occurrence with its subject and receipt-backed figures.
#[derive(Clone, Debug)]
pub enum Event {
    ApprovalWaiting {
        approval_id: ApprovalId,
        agent_id: AgentId,
        money: Option<Money>,
    },
    MoneyArrived {
        entry_id: ActivityEntryId,
        journey_id: JourneyId,
        money: Money,
    },
    JourneyFinished {
        journey_id: JourneyId,
        outcome: JourneyOutcome,
        money: Option<Money>,
    },
    ClaimReady {
        journey_id: JourneyId,
        money: Money,
    },
    SecurityNewDevice {
        device_id: DeviceId,
    },
    SecurityRecovery {
        event_id: EventId,
    },
    SecurityWalletRebinding {
        event_id: EventId,
    },
    SecurityKeyRotation {
        event_id: EventId,
    },
    ServiceStatus {
        event_id: EventId,
        component: DegradedComponent,
    },
}

impl Event {
    /// Returns the notification class this event dispatches as.
    #[must_use]
    pub const fn class(&self) -> NotificationClass {
        match self {
            Self::ApprovalWaiting { .. } => NotificationClass::ApprovalWaiting,
            Self::MoneyArrived { .. } => NotificationClass::MoneyArrived,
            Self::JourneyFinished { .. } => NotificationClass::JourneyFinished,
            Self::ClaimReady { .. } => NotificationClass::ClaimReady,
            Self::SecurityNewDevice { .. } => NotificationClass::SecurityNewDevice,
            Self::SecurityRecovery { .. } => NotificationClass::SecurityRecovery,
            Self::SecurityWalletRebinding { .. } => NotificationClass::SecurityWalletRebinding,
            Self::SecurityKeyRotation { .. } => NotificationClass::SecurityKeyRotation,
            Self::ServiceStatus { .. } => NotificationClass::ServiceStatus,
        }
    }

    /// Returns the stable subject repeated events deduplicate on.
    #[must_use]
    pub fn subject(&self) -> Subject {
        match self {
            Self::ApprovalWaiting { approval_id, .. } => Subject::Approval(approval_id.clone()),
            Self::MoneyArrived { entry_id, .. } => Subject::Activity(entry_id.clone()),
            Self::JourneyFinished { journey_id, .. } | Self::ClaimReady { journey_id, .. } => {
                Subject::Journey(journey_id.clone())
            }
            Self::SecurityNewDevice { device_id } => Subject::Device(device_id.clone()),
            Self::SecurityRecovery { event_id }
            | Self::SecurityWalletRebinding { event_id }
            | Self::SecurityKeyRotation { event_id }
            | Self::ServiceStatus { event_id, .. } => Subject::Occurrence(event_id.clone()),
        }
    }
}

/// The stable identity a notification is keyed by within its class.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Subject {
    Approval(ApprovalId),
    Activity(ActivityEntryId),
    Journey(JourneyId),
    Device(DeviceId),
    Occurrence(EventId),
    Service(DegradedComponent),
}

impl Subject {
    pub(crate) fn canonical(&self) -> String {
        match self {
            Self::Approval(id) => format!("approval:{}", id.as_str()),
            Self::Activity(id) => format!("activity:{}", id.as_str()),
            Self::Journey(id) => format!("journey:{}", id.as_str()),
            Self::Device(id) => format!("device:{}", id.as_str()),
            Self::Occurrence(id) => format!("occurrence:{}", id.as_str()),
            Self::Service(component) => format!("service:{}", component.as_str()),
        }
    }
}
