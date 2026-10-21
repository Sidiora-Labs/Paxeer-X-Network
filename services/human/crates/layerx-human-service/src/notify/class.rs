use std::fmt::{Display, Formatter};

pub(crate) const CLASS_COUNT: usize = 9;

/// Every event class the service notifies on.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum NotificationClass {
    ApprovalWaiting,
    MoneyArrived,
    JourneyFinished,
    ClaimReady,
    SecurityNewDevice,
    SecurityRecovery,
    SecurityWalletRebinding,
    SecurityKeyRotation,
    ServiceStatus,
}

impl NotificationClass {
    /// Every class in contract order.
    pub const ALL: [Self; CLASS_COUNT] = [
        Self::ApprovalWaiting,
        Self::MoneyArrived,
        Self::JourneyFinished,
        Self::ClaimReady,
        Self::SecurityNewDevice,
        Self::SecurityRecovery,
        Self::SecurityWalletRebinding,
        Self::SecurityKeyRotation,
        Self::ServiceStatus,
    ];

    /// Returns the contract name of the class.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::ApprovalWaiting => "approval-waiting",
            Self::MoneyArrived => "money-arrived",
            Self::JourneyFinished => "journey-finished",
            Self::ClaimReady => "claim-ready",
            Self::SecurityNewDevice => "security-new-device",
            Self::SecurityRecovery => "security-recovery",
            Self::SecurityWalletRebinding => "security-wallet-rebinding",
            Self::SecurityKeyRotation => "security-key-rotation",
            Self::ServiceStatus => "service-status",
        }
    }

    /// Returns whether the class is a security event carrying an action
    /// button.
    #[must_use]
    pub const fn security(self) -> bool {
        matches!(
            self,
            Self::SecurityNewDevice
                | Self::SecurityRecovery
                | Self::SecurityWalletRebinding
                | Self::SecurityKeyRotation
        )
    }

    /// Returns whether the class may never be fully suppressed.
    #[must_use]
    pub const fn security_critical(self) -> bool {
        matches!(
            self,
            Self::SecurityRecovery | Self::SecurityWalletRebinding | Self::SecurityKeyRotation
        )
    }

    pub(crate) const fn index(self) -> usize {
        match self {
            Self::ApprovalWaiting => 0,
            Self::MoneyArrived => 1,
            Self::JourneyFinished => 2,
            Self::ClaimReady => 3,
            Self::SecurityNewDevice => 4,
            Self::SecurityRecovery => 5,
            Self::SecurityWalletRebinding => 6,
            Self::SecurityKeyRotation => 7,
            Self::ServiceStatus => 8,
        }
    }

    pub(crate) const fn code(self) -> u8 {
        match self {
            Self::ApprovalWaiting => 1,
            Self::MoneyArrived => 2,
            Self::JourneyFinished => 3,
            Self::ClaimReady => 4,
            Self::SecurityNewDevice => 5,
            Self::SecurityRecovery => 6,
            Self::SecurityWalletRebinding => 7,
            Self::SecurityKeyRotation => 8,
            Self::ServiceStatus => 9,
        }
    }

    pub(crate) const fn from_code(value: u8) -> Option<Self> {
        match value {
            1 => Some(Self::ApprovalWaiting),
            2 => Some(Self::MoneyArrived),
            3 => Some(Self::JourneyFinished),
            4 => Some(Self::ClaimReady),
            5 => Some(Self::SecurityNewDevice),
            6 => Some(Self::SecurityRecovery),
            7 => Some(Self::SecurityWalletRebinding),
            8 => Some(Self::SecurityKeyRotation),
            9 => Some(Self::ServiceStatus),
            _ => None,
        }
    }
}

impl Display for NotificationClass {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.as_str())
    }
}

/// How much financial detail notification payloads carry off the
/// authenticated surface.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DetailLevel {
    Full,
    Summary,
    Minimal,
}

impl DetailLevel {
    /// Returns the contract name of the level.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Full => "full",
            Self::Summary => "summary",
            Self::Minimal => "minimal",
        }
    }

    pub(crate) const fn code(self) -> u8 {
        match self {
            Self::Full => 1,
            Self::Summary => 2,
            Self::Minimal => 3,
        }
    }

    pub(crate) const fn from_code(value: u8) -> Option<Self> {
        match value {
            1 => Some(Self::Full),
            2 => Some(Self::Summary),
            3 => Some(Self::Minimal),
            _ => None,
        }
    }
}

impl Display for DetailLevel {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.as_str())
    }
}

/// The honest final state a notification's subject has reached.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Resolution {
    Approved,
    Rejected,
    Expired,
    Defective,
    Done,
    Failed,
}

impl Resolution {
    /// Returns the contract name of the resolved state.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Approved => "approved",
            Self::Rejected => "rejected",
            Self::Expired => "expired",
            Self::Defective => "defective",
            Self::Done => "done",
            Self::Failed => "failed",
        }
    }
}

impl Display for Resolution {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.as_str())
    }
}
