//! Durable, preference-aware notification dispatch with immutable channel
//! payloads and audit-bound deduplication.

mod class;
mod content;
mod delivery;
mod event;
mod links;
mod preferences;

use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};

use sha2::{Digest as _, Sha256};

use crate::audit::{
    AuditChain, AuditError, AuditEvent, NotificationChannel as AuditedChannel,
    NotificationClass as AuditedClass,
};
use crate::store::{EvidenceRef, PrincipalScope, RowKey, StoreError, Table};
use crate::trace::TraceId;

pub use class::{DetailLevel, NotificationClass, Resolution};
pub use delivery::Delivery;
pub use event::{
    ActivityEntryId, AgentId, ApprovalId, DegradedComponent, DeviceId, Event, EventId, JourneyId,
    JourneyOutcome, Money, NotificationId, Subject,
};
pub use links::{
    ActiveShell, BadgeCounts, DeepLinks, InAppInventory, Landing, LandingState, NotificationGroup,
    NotificationSummary, Recency, SubjectState, Surface,
};
pub use preferences::{Channel, ChannelPreferences, Preferences};

const PREFERENCES_KEY: &str = "notify_preferences";
const BATCH_MAGIC: &[u8; 4] = b"LXNB";
const BATCH_VERSION: u8 = 1;
const BATCH_PREFIX: &str = "notify_batch_";
const DELIVERY_PREFIX: &str = "notify_delivery_";
const DEDUP_DOMAIN: &[u8] = b"LayerX notification dedup v1\0";

/// Typed notification-service failure.
#[derive(Debug)]
pub enum NotifyError {
    Store(StoreError),
    Audit(AuditError),
    InvalidIdentifier { kind: &'static str },
    InvalidCurrency,
    NotificationNotFound,
    InvalidDeepLink,
    LinkSubjectMismatch,
    LinkStateMismatch,
    Corrupt(&'static str),
    SizeOverflow,
}

impl Display for NotifyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Store(error) => write!(formatter, "notification store failure: {error}"),
            Self::Audit(error) => write!(formatter, "notification audit failure: {error}"),
            Self::InvalidIdentifier { kind } => write!(formatter, "invalid {kind} identifier"),
            Self::InvalidCurrency => formatter.write_str("invalid notification currency"),
            Self::NotificationNotFound => formatter.write_str("notification not found"),
            Self::InvalidDeepLink => formatter.write_str("notification deep link is invalid"),
            Self::LinkSubjectMismatch => {
                formatter.write_str("notification deep link subject does not match")
            }
            Self::LinkStateMismatch => {
                formatter.write_str("notification state does not match its subject")
            }
            Self::Corrupt(reason) => write!(formatter, "corrupt notification record: {reason}"),
            Self::SizeOverflow => formatter.write_str("notification record exceeds size bounds"),
        }
    }
}

impl std::error::Error for NotifyError {}

impl From<StoreError> for NotifyError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

impl From<AuditError> for NotifyError {
    fn from(value: AuditError) -> Self {
        Self::Audit(value)
    }
}

/// Whether this call created, resumed, deduplicated or suppressed a dispatch.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DispatchOutcome {
    Dispatched,
    Resumed,
    Deduplicated,
    Suppressed,
}

/// The immutable result of one notification dispatch attempt.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DispatchReport {
    notification_id: NotificationId,
    outcome: DispatchOutcome,
    deliveries: Vec<Delivery>,
}

impl DispatchReport {
    /// Returns the stable identifier shared by all channel deliveries.
    #[must_use]
    pub const fn notification_id(&self) -> &NotificationId {
        &self.notification_id
    }

    /// Returns how this attempt converged.
    #[must_use]
    pub const fn outcome(&self) -> DispatchOutcome {
        self.outcome
    }

    /// Returns the exact immutable channel deliveries.
    #[must_use]
    pub fn deliveries(&self) -> &[Delivery] {
        &self.deliveries
    }
}

/// Principal-scoped notification dispatcher.
#[derive(Clone, Copy, Debug, Default)]
pub struct Dispatcher;

impl Dispatcher {
    /// Loads current preferences, returning the contract defaults before a
    /// principal has saved a choice.
    ///
    /// # Errors
    ///
    /// Refuses a corrupt durable preference record.
    pub fn preferences(scope: &PrincipalScope<'_>) -> Result<Preferences, NotifyError> {
        let key = preference_key()?;
        scope.get(Table::Notifications, &key).map_or_else(
            || Ok(Preferences::default()),
            |row| Preferences::decode(row.bytes()),
        )
    }

    /// Persists a complete preference replacement. The next dispatch reloads
    /// this row, so the change takes effect immediately.
    ///
    /// # Errors
    ///
    /// Returns durable-store failures.
    pub fn update_preferences(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        preferences: &Preferences,
    ) -> Result<(), NotifyError> {
        scope.put(
            Table::Notifications,
            preference_key()?,
            now,
            preferences.encode(),
        )?;
        Ok(())
    }

    /// Dispatches one typed event into every currently selected channel.
    /// Stable event identity, immutable batches and audit evidence make crash
    /// retries and repeated source events converge without duplicate effects.
    ///
    /// # Errors
    ///
    /// Returns validation, persistence, corruption and audit-chain failures.
    pub fn dispatch(
        scope: &mut PrincipalScope<'_>,
        audit: &mut AuditChain,
        now: u64,
        trace: &TraceId,
        event: &Event,
    ) -> Result<DispatchReport, NotifyError> {
        let digest = dedup_digest(event);
        let digest_text = hex_digest(digest);
        let marker_key = row_key(&format!("{BATCH_PREFIX}{digest_text}"))?;
        let existing = scope.get(Table::Notifications, &marker_key);
        let marker_existed = existing.is_some();
        let batch = if let Some(row) = existing {
            Batch::decode(row.bytes())?
        } else {
            let preferences = Self::preferences(scope)?;
            let id = NotificationId::new(format!("ntf_{digest_text}"))?;
            let built = content::build(event, preferences.detail());
            let deliveries = preferences
                .selected(event.class())
                .into_iter()
                .map(|channel| Delivery::build(&built, id.clone(), channel, now))
                .collect::<Vec<_>>();
            let batch = Batch {
                notification_id: id,
                deliveries,
            };
            scope.put(
                Table::Notifications,
                marker_key.clone(),
                now,
                batch.encode()?,
            )?;
            batch
        };

        if batch.deliveries.is_empty() {
            return Ok(DispatchReport {
                notification_id: batch.notification_id,
                outcome: DispatchOutcome::Suppressed,
                deliveries: Vec::new(),
            });
        }

        let mut audited = audited_delivery_keys(audit, scope)?;
        let mut appended = 0_usize;
        for delivery in &batch.deliveries {
            let key = delivery_key(&digest_text, delivery.channel())?;
            if scope.get(Table::Notifications, &key).is_none() {
                scope.put(
                    Table::Notifications,
                    key.clone(),
                    delivery.created_at(),
                    delivery.encode()?,
                )?;
            }
            if audited.insert(key.clone()) {
                let evidence = [
                    EvidenceRef::new(Table::Notifications, marker_key.clone()),
                    EvidenceRef::new(Table::Notifications, key),
                ];
                audit.append(
                    scope,
                    now,
                    trace,
                    &AuditEvent::NotificationDispatch {
                        class: audited_class(event),
                        channel: audited_channel(delivery.channel()),
                    },
                    &evidence,
                )?;
                appended = appended.saturating_add(1);
            }
        }
        let outcome = if appended == 0 {
            DispatchOutcome::Deduplicated
        } else if marker_existed {
            DispatchOutcome::Resumed
        } else {
            DispatchOutcome::Dispatched
        };
        if let Some(delivery) = batch
            .deliveries
            .iter()
            .find(|delivery| delivery.channel() == Channel::InApp)
        {
            let summary = NotificationSummary::from_delivery(scope, delivery.clone())?;
            let payload = crate::server::stream_journal::notification_wire(&summary)
                .map_err(|_| NotifyError::Corrupt("notification stream projection"))?;
            crate::server::stream_journal::StreamJournal::append(
                scope,
                &format!("notification-delivery:{}", batch.notification_id.as_str()),
                "notification",
                delivery.created_at(),
                serde_json::json!({"notification":payload}),
            )
            .map_err(|_| NotifyError::Corrupt("notification stream journal"))?;
        }
        Ok(DispatchReport {
            notification_id: batch.notification_id,
            outcome,
            deliveries: batch.deliveries,
        })
    }

    /// Lists every immutable delivery in deterministic key order.
    ///
    /// # Errors
    ///
    /// Refuses corrupt durable delivery rows.
    pub fn deliveries(scope: &PrincipalScope<'_>) -> Result<Vec<Delivery>, NotifyError> {
        scope
            .keys(Table::Notifications)
            .into_iter()
            .filter(|key| key.as_str().starts_with(DELIVERY_PREFIX))
            .map(|key| {
                let row = scope
                    .get(Table::Notifications, &key)
                    .ok_or(NotifyError::Corrupt("delivery disappeared while listing"))?;
                Delivery::decode(row.bytes())
            })
            .collect()
    }
}

struct Batch {
    notification_id: NotificationId,
    deliveries: Vec<Delivery>,
}

impl Batch {
    fn encode(&self) -> Result<Vec<u8>, NotifyError> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(BATCH_MAGIC);
        bytes.push(BATCH_VERSION);
        let id_length = u8::try_from(self.notification_id.as_str().len())
            .map_err(|_| NotifyError::SizeOverflow)?;
        bytes.push(id_length);
        bytes.extend_from_slice(self.notification_id.as_str().as_bytes());
        bytes.push(u8::try_from(self.deliveries.len()).map_err(|_| NotifyError::SizeOverflow)?);
        for delivery in &self.deliveries {
            let encoded = delivery.encode()?;
            let length = u32::try_from(encoded.len()).map_err(|_| NotifyError::SizeOverflow)?;
            bytes.extend_from_slice(&length.to_be_bytes());
            bytes.extend_from_slice(&encoded);
        }
        Ok(bytes)
    }

    fn decode(bytes: &[u8]) -> Result<Self, NotifyError> {
        if bytes.len() < 7
            || bytes.get(..4) != Some(BATCH_MAGIC.as_slice())
            || bytes[4] != BATCH_VERSION
        {
            return Err(NotifyError::Corrupt("invalid dispatch batch"));
        }
        let id_length = usize::from(bytes[5]);
        let id_end = 6_usize
            .checked_add(id_length)
            .ok_or(NotifyError::Corrupt("batch identifier overflow"))?;
        let notification_id = NotificationId::new(
            std::str::from_utf8(
                bytes
                    .get(6..id_end)
                    .ok_or(NotifyError::Corrupt("truncated batch identifier"))?,
            )
            .map_err(|_| NotifyError::Corrupt("batch identifier is not UTF-8"))?,
        )?;
        let count = usize::from(
            *bytes
                .get(id_end)
                .ok_or(NotifyError::Corrupt("missing batch channel count"))?,
        );
        if count > Channel::ALL.len() {
            return Err(NotifyError::Corrupt("dispatch batch has too many channels"));
        }
        let mut offset = id_end.saturating_add(1);
        let mut deliveries = Vec::with_capacity(count);
        for _ in 0..count {
            let length_end = offset
                .checked_add(4)
                .ok_or(NotifyError::Corrupt("batch length overflow"))?;
            let length_bytes: [u8; 4] = bytes
                .get(offset..length_end)
                .ok_or(NotifyError::Corrupt("truncated batch length"))?
                .try_into()
                .map_err(|_| NotifyError::Corrupt("invalid batch length"))?;
            offset = length_end;
            let length = usize::try_from(u32::from_be_bytes(length_bytes))
                .map_err(|_| NotifyError::SizeOverflow)?;
            let end = offset
                .checked_add(length)
                .ok_or(NotifyError::Corrupt("batch item overflow"))?;
            let delivery = Delivery::decode(
                bytes
                    .get(offset..end)
                    .ok_or(NotifyError::Corrupt("truncated batch item"))?,
            )?;
            offset = end;
            deliveries.push(delivery);
        }
        if offset != bytes.len() {
            return Err(NotifyError::Corrupt("trailing batch bytes"));
        }
        if deliveries
            .iter()
            .any(|delivery| delivery.notification_id() != &notification_id)
        {
            return Err(NotifyError::Corrupt(
                "batch notification identifiers disagree",
            ));
        }
        let mut channels = BTreeSet::new();
        if deliveries
            .iter()
            .any(|delivery| !channels.insert(delivery.channel()))
        {
            return Err(NotifyError::Corrupt("duplicate channel in dispatch batch"));
        }
        Ok(Self {
            notification_id,
            deliveries,
        })
    }
}

fn preference_key() -> Result<RowKey, NotifyError> {
    row_key(PREFERENCES_KEY)
}

fn row_key(value: &str) -> Result<RowKey, NotifyError> {
    RowKey::new(value).map_err(NotifyError::Store)
}

fn delivery_key(digest: &str, channel: Channel) -> Result<RowKey, NotifyError> {
    row_key(&format!("{DELIVERY_PREFIX}{digest}_{}", channel.code()))
}

fn dedup_digest(event: &Event) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(DEDUP_DOMAIN);
    hasher.update([event.class().code()]);
    hasher.update(event.subject().canonical().as_bytes());
    hasher.finalize().into()
}

fn hex_digest(digest: [u8; 32]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut text = String::with_capacity(64);
    for byte in digest {
        text.push(char::from(DIGITS[usize::from(byte >> 4)]));
        text.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    text
}

fn audited_delivery_keys(
    audit: &AuditChain,
    scope: &PrincipalScope<'_>,
) -> Result<BTreeSet<RowKey>, NotifyError> {
    let entries = audit.entries(scope)?;
    Ok(entries
        .iter()
        .filter(|entry| matches!(entry.event(), AuditEvent::NotificationDispatch { .. }))
        .flat_map(crate::audit::ChainEntry::evidence)
        .filter(|evidence| {
            evidence.table() == Table::Notifications
                && evidence.key().as_str().starts_with(DELIVERY_PREFIX)
        })
        .map(|evidence| evidence.key().clone())
        .collect())
}

const fn audited_class(event: &Event) -> AuditedClass {
    match event {
        Event::ApprovalWaiting { .. } => AuditedClass::ApprovalWaiting,
        Event::MoneyArrived { .. } => AuditedClass::MoneyArrived,
        Event::JourneyFinished {
            outcome: JourneyOutcome::Completed,
            ..
        } => AuditedClass::JourneyCompleted,
        Event::JourneyFinished {
            outcome: JourneyOutcome::Failed,
            ..
        } => AuditedClass::JourneyFailed,
        Event::ClaimReady { .. } => AuditedClass::ClaimReady,
        Event::SecurityNewDevice { .. }
        | Event::SecurityRecovery { .. }
        | Event::SecurityWalletRebinding { .. }
        | Event::SecurityKeyRotation { .. } => AuditedClass::Security,
        Event::ServiceStatus { .. } => AuditedClass::Degradation,
    }
}

const fn audited_channel(channel: Channel) -> AuditedChannel {
    match channel {
        Channel::Push => AuditedChannel::Push,
        Channel::Email => AuditedChannel::Email,
        Channel::InApp => AuditedChannel::InApp,
    }
}
