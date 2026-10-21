use std::collections::BTreeMap;
use std::num::NonZeroUsize;

use crate::approvals::{InboxSnapshot, InboxState};
use crate::journeys::JourneyState;
use crate::store::{PrincipalScope, RowKey, Table};

use super::{
    ActivityEntryId, ApprovalId, Channel, Delivery, Dispatcher, JourneyId, NotificationClass,
    NotificationId, NotifyError, Resolution,
};

const READ_PREFIX: &str = "notify_read_";
const READ_MARKER: &[u8; 5] = b"LXNR\x01";
const SECONDS_PER_DAY: u64 = 86_400;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ActiveShell {
    Mobile,
    Desktop,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Surface {
    Approval,
    Activity,
    Journey,
    Claim,
    Devices,
    Recovery,
    Wallet,
    Keys,
    ServiceStatus,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LandingState {
    Current,
    Actionable,
    Resolved(Resolution),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Landing {
    shell: ActiveShell,
    surface: Surface,
    path: String,
    state: LandingState,
}

impl Landing {
    #[must_use]
    pub const fn shell(&self) -> ActiveShell {
        self.shell
    }

    #[must_use]
    pub const fn surface(&self) -> Surface {
        self.surface
    }

    #[must_use]
    pub fn path(&self) -> &str {
        &self.path
    }

    #[must_use]
    pub const fn state(&self) -> LandingState {
        self.state
    }
}

#[derive(Clone, Copy, Debug)]
pub enum SubjectState<'a> {
    Current,
    Approval {
        approval_id: &'a ApprovalId,
        state: &'a InboxState,
    },
    Journey {
        journey_id: &'a JourneyId,
        state: JourneyState,
    },
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum Recency {
    Today,
    Yesterday,
    ThisWeek,
    Earlier,
}

impl Recency {
    const ALL: [Self; 4] = [Self::Today, Self::Yesterday, Self::ThisWeek, Self::Earlier];

    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Today => "today",
            Self::Yesterday => "yesterday",
            Self::ThisWeek => "this-week",
            Self::Earlier => "earlier",
        }
    }

    const fn index(self) -> usize {
        match self {
            Self::Today => 0,
            Self::Yesterday => 1,
            Self::ThisWeek => 2,
            Self::Earlier => 3,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NotificationSummary {
    delivery: Delivery,
    read: bool,
}

impl NotificationSummary {
    pub(crate) fn from_delivery(
        scope: &PrincipalScope<'_>,
        delivery: Delivery,
    ) -> Result<Self, NotifyError> {
        let read = read_state(scope, delivery.notification_id())?;
        Ok(Self { delivery, read })
    }
    #[must_use]
    pub const fn notification_id(&self) -> &NotificationId {
        self.delivery.notification_id()
    }

    #[must_use]
    pub const fn class(&self) -> NotificationClass {
        self.delivery.class()
    }

    #[must_use]
    pub const fn created_at(&self) -> u64 {
        self.delivery.created_at()
    }

    #[must_use]
    pub fn deep_link(&self) -> &str {
        self.delivery.deep_link()
    }

    #[must_use]
    pub const fn read(&self) -> bool {
        self.read
    }

    #[must_use]
    pub const fn delivery(&self) -> &Delivery {
        &self.delivery
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NotificationGroup {
    recency: Recency,
    notifications: Vec<NotificationSummary>,
}

impl NotificationGroup {
    #[must_use]
    pub const fn recency(&self) -> Recency {
        self.recency
    }

    #[must_use]
    pub fn notifications(&self) -> &[NotificationSummary] {
        &self.notifications
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct InAppInventory {
    groups: Vec<NotificationGroup>,
    unread_count: usize,
}

impl InAppInventory {
    #[must_use]
    pub fn groups(&self) -> &[NotificationGroup] {
        &self.groups
    }

    #[must_use]
    pub const fn unread_count(&self) -> usize {
        self.unread_count
    }

    #[must_use]
    pub fn notification(&self, id: &NotificationId) -> Option<&NotificationSummary> {
        self.groups
            .iter()
            .flat_map(NotificationGroup::notifications)
            .find(|notification| notification.notification_id() == id)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct BadgeCounts {
    notifications: Option<NonZeroUsize>,
    approvals: Option<NonZeroUsize>,
}

impl BadgeCounts {
    const fn new(unread: usize, approvals: usize) -> Self {
        Self {
            notifications: NonZeroUsize::new(unread),
            approvals: NonZeroUsize::new(approvals),
        }
    }

    #[must_use]
    pub const fn notifications(self) -> Option<usize> {
        match self.notifications {
            Some(count) => Some(count.get()),
            None => None,
        }
    }

    #[must_use]
    pub const fn approvals(self) -> Option<usize> {
        match self.approvals {
            Some(count) => Some(count.get()),
            None => None,
        }
    }
}

#[derive(Clone, Copy, Debug, Default)]
pub struct DeepLinks;

impl DeepLinks {
    /// Resolves a stored notification against the subject's current state in
    /// the active application shell.
    ///
    /// # Errors
    ///
    /// Refuses a mismatched subject, malformed route or non-final state for a
    /// notification that claims its journey finished.
    pub fn resolve(
        delivery: &Delivery,
        shell: ActiveShell,
        subject: SubjectState<'_>,
    ) -> Result<Landing, NotifyError> {
        match (delivery.class(), subject) {
            (NotificationClass::ApprovalWaiting, SubjectState::Approval { approval_id, state }) => {
                resolve_approval(delivery, shell, approval_id, state)
            }
            (
                NotificationClass::JourneyFinished | NotificationClass::ClaimReady,
                SubjectState::Journey { journey_id, state },
            ) => resolve_journey(delivery, shell, journey_id, state),
            (
                NotificationClass::MoneyArrived
                | NotificationClass::SecurityNewDevice
                | NotificationClass::SecurityRecovery
                | NotificationClass::SecurityWalletRebinding
                | NotificationClass::SecurityKeyRotation
                | NotificationClass::ServiceStatus,
                SubjectState::Current,
            ) => resolve_current(delivery, shell),
            _ => Err(NotifyError::LinkSubjectMismatch),
        }
    }

    /// Lists the principal's deduplicated in-app notifications by recency.
    ///
    /// # Errors
    ///
    /// Refuses corrupt delivery or read-state records.
    pub fn inventory(scope: &PrincipalScope<'_>, now: u64) -> Result<InAppInventory, NotifyError> {
        let mut unique = BTreeMap::<String, Delivery>::new();
        for delivery in Dispatcher::deliveries(scope)? {
            if delivery.channel() != Channel::InApp {
                continue;
            }
            let id = delivery.notification_id().as_str().to_owned();
            match unique.get(&id) {
                Some(existing) if existing.created_at() > delivery.created_at() => {}
                _ => {
                    unique.insert(id, delivery);
                }
            }
        }

        let mut grouped: [Vec<NotificationSummary>; 4] = std::array::from_fn(|_| Vec::new());
        let mut unread_count = 0_usize;
        for delivery in unique.into_values() {
            let read = read_state(scope, delivery.notification_id())?;
            if !read {
                unread_count = unread_count.saturating_add(1);
            }
            grouped[recency(now, delivery.created_at()).index()]
                .push(NotificationSummary { delivery, read });
        }
        for notifications in &mut grouped {
            notifications.sort_by(|left, right| {
                right.created_at().cmp(&left.created_at()).then_with(|| {
                    left.notification_id()
                        .as_str()
                        .cmp(right.notification_id().as_str())
                })
            });
        }
        let groups = Recency::ALL
            .into_iter()
            .filter_map(|recency| {
                let notifications = std::mem::take(&mut grouped[recency.index()]);
                (!notifications.is_empty()).then_some(NotificationGroup {
                    recency,
                    notifications,
                })
            })
            .collect();
        Ok(InAppInventory {
            groups,
            unread_count,
        })
    }

    /// Durably marks one principal-scoped in-app notification as read.
    ///
    /// # Errors
    ///
    /// Returns not-found, corrupt-record and durable-store failures.
    pub fn mark_read(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        id: &NotificationId,
    ) -> Result<NotificationSummary, NotifyError> {
        let delivery = Dispatcher::deliveries(scope)?
            .into_iter()
            .find(|delivery| {
                delivery.channel() == Channel::InApp && delivery.notification_id() == id
            })
            .ok_or(NotifyError::NotificationNotFound)?;
        let key = read_key(id)?;
        if let Some(row) = scope.get(Table::Notifications, &key) {
            validate_read_marker(row.bytes())?;
        } else {
            scope.put(Table::Notifications, key, now, READ_MARKER.to_vec())?;
        }
        Ok(NotificationSummary {
            delivery,
            read: true,
        })
    }

    #[must_use]
    pub fn badges(inventory: &InAppInventory, inbox: &InboxSnapshot<'_>) -> BadgeCounts {
        BadgeCounts::new(inventory.unread_count(), inbox.awaiting_count())
    }
}

fn resolve_approval(
    delivery: &Delivery,
    shell: ActiveShell,
    approval_id: &ApprovalId,
    state: &InboxState,
) -> Result<Landing, NotifyError> {
    let path = format!("/app/approvals/{}", approval_id.as_str());
    require_path(delivery, &path)?;
    let state = match state {
        InboxState::AwaitingApproval => LandingState::Actionable,
        InboxState::Approved { .. } => LandingState::Resolved(Resolution::Approved),
        InboxState::Rejected => LandingState::Resolved(Resolution::Rejected),
        InboxState::Expired => LandingState::Resolved(Resolution::Expired),
    };
    Ok(Landing {
        shell,
        surface: Surface::Approval,
        path,
        state,
    })
}

fn resolve_journey(
    delivery: &Delivery,
    shell: ActiveShell,
    journey_id: &JourneyId,
    state: JourneyState,
) -> Result<Landing, NotifyError> {
    let journey_path = format!("/app/journeys/{}", journey_id.as_str());
    let link_path = if delivery.class() == NotificationClass::ClaimReady {
        format!("{journey_path}/claim")
    } else {
        journey_path.clone()
    };
    require_path(delivery, &link_path)?;
    let (surface, path, landing_state) = match state {
        JourneyState::Done => (
            Surface::Journey,
            journey_path,
            LandingState::Resolved(Resolution::Done),
        ),
        JourneyState::Refused => (
            Surface::Journey,
            journey_path,
            LandingState::Resolved(Resolution::Failed),
        ),
        JourneyState::GettingReady
        | JourneyState::Sending
        | JourneyState::Processing
        | JourneyState::StillChecking
            if delivery.class() == NotificationClass::ClaimReady =>
        {
            (Surface::Claim, link_path, LandingState::Actionable)
        }
        JourneyState::GettingReady
        | JourneyState::Sending
        | JourneyState::Processing
        | JourneyState::StillChecking => return Err(NotifyError::LinkStateMismatch),
    };
    Ok(Landing {
        shell,
        surface,
        path,
        state: landing_state,
    })
}

fn resolve_current(delivery: &Delivery, shell: ActiveShell) -> Result<Landing, NotifyError> {
    let (surface, state) = match delivery.class() {
        NotificationClass::MoneyArrived => {
            let id = delivery
                .deep_link()
                .strip_prefix("/app/activity/")
                .ok_or(NotifyError::InvalidDeepLink)?;
            ActivityEntryId::new(id)?;
            (Surface::Activity, LandingState::Resolved(Resolution::Done))
        }
        NotificationClass::SecurityNewDevice => {
            require_path(delivery, "/app/security/devices")?;
            (Surface::Devices, LandingState::Actionable)
        }
        NotificationClass::SecurityRecovery => {
            require_path(delivery, "/app/security/recovery")?;
            (Surface::Recovery, LandingState::Actionable)
        }
        NotificationClass::SecurityWalletRebinding => {
            require_path(delivery, "/app/security/wallet")?;
            (Surface::Wallet, LandingState::Actionable)
        }
        NotificationClass::SecurityKeyRotation => {
            require_path(delivery, "/app/security/keys")?;
            (Surface::Keys, LandingState::Actionable)
        }
        NotificationClass::ServiceStatus => {
            require_path(delivery, "/app/status")?;
            (Surface::ServiceStatus, LandingState::Current)
        }
        NotificationClass::ApprovalWaiting
        | NotificationClass::JourneyFinished
        | NotificationClass::ClaimReady => return Err(NotifyError::LinkSubjectMismatch),
    };
    Ok(Landing {
        shell,
        surface,
        path: delivery.deep_link().to_owned(),
        state,
    })
}

fn require_path(delivery: &Delivery, expected: &str) -> Result<(), NotifyError> {
    if delivery.deep_link() == expected {
        Ok(())
    } else {
        Err(NotifyError::InvalidDeepLink)
    }
}

fn recency(now: u64, created_at: u64) -> Recency {
    match (now / SECONDS_PER_DAY).saturating_sub(created_at / SECONDS_PER_DAY) {
        0 => Recency::Today,
        1 => Recency::Yesterday,
        2..=6 => Recency::ThisWeek,
        _ => Recency::Earlier,
    }
}

fn read_key(id: &NotificationId) -> Result<RowKey, NotifyError> {
    Ok(RowKey::new(format!("{READ_PREFIX}{}", id.as_str()))?)
}

fn read_state(scope: &PrincipalScope<'_>, id: &NotificationId) -> Result<bool, NotifyError> {
    let Some(row) = scope.get(Table::Notifications, &read_key(id)?) else {
        return Ok(false);
    };
    validate_read_marker(row.bytes())?;
    Ok(true)
}

fn validate_read_marker(bytes: &[u8]) -> Result<(), NotifyError> {
    if bytes == READ_MARKER {
        Ok(())
    } else {
        Err(NotifyError::Corrupt("invalid notification read marker"))
    }
}

#[cfg(test)]
mod tests {
    use super::BadgeCounts;

    #[test]
    fn zero_badges_are_absent() {
        let empty = BadgeCounts::new(0, 0);
        assert_eq!(empty.notifications(), None);
        assert_eq!(empty.approvals(), None);

        let waiting = BadgeCounts::new(2, 3);
        assert_eq!(waiting.notifications(), Some(2));
        assert_eq!(waiting.approvals(), Some(3));
    }
}
