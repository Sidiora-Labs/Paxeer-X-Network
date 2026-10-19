//! Tenant-scoped durable subscription and delivery contract types.

use crate::identity::{
    ActivityType, AgentDid, Asset, CapabilityId, ContractError, Counterparty, ExplicitSet, TenantId,
};
use crate::read::{AccountRef, ModuleRef};
use crate::track::ReceiptRef;
use crate::verify::Level;
use crate::{Sequence, TimestampSeconds};
use layerx_types::result::ResultCode;

macro_rules! required_reference {
    ($name:ident, $field:literal) => {
        #[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
        pub struct $name(String);

        impl $name {
            /// Constructs a non-empty subscription reference.
            ///
            /// # Errors
            /// Returns [`ContractError::Empty`] when the value is empty.
            pub fn new(value: impl Into<String>) -> Result<Self, ContractError> {
                let value = value.into();
                if value.is_empty() {
                    return Err(ContractError::Empty($field));
                }
                Ok(Self(value))
            }

            #[must_use]
            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

required_reference!(SubscriptionId, "subscription_id");
required_reference!(DeliveryTarget, "delivery_target");

/// Mandatory visibility boundary applied before any caller filter.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionScope {
    pub tenant: TenantId,
    pub agent: AgentDid,
    pub capability: CapabilityId,
}

/// A filter value tagged with the tenant that owns it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TenantObject<T> {
    pub tenant: TenantId,
    pub value: T,
}

/// Complete deterministic filter vocabulary. Every field is explicitly supplied.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionFilter {
    pub agents: ExplicitSet<TenantObject<AgentDid>>,
    pub accounts: ExplicitSet<TenantObject<AccountRef>>,
    pub activity_types: ExplicitSet<ActivityType>,
    pub modules: ExplicitSet<TenantObject<ModuleRef>>,
    pub assets: ExplicitSet<TenantObject<Asset>>,
    pub counterparties: ExplicitSet<TenantObject<Counterparty>>,
    pub result_classes: ExplicitSet<ResultCode>,
}

impl SubscriptionFilter {
    /// Confirms every object restriction remains inside the already-applied tenant scope.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] naming the first cross-tenant dimension.
    pub fn validate_for(self, scope: &SubscriptionScope) -> Result<Self, ContractError> {
        let inside = |tenant: &TenantId| tenant == &scope.tenant;
        if self
            .agents
            .values()
            .iter()
            .any(|item| !inside(&item.tenant))
        {
            return Err(ContractError::Empty("filter_agent_outside_tenant"));
        }
        if self
            .accounts
            .values()
            .iter()
            .any(|item| !inside(&item.tenant))
        {
            return Err(ContractError::Empty("filter_account_outside_tenant"));
        }
        if self
            .modules
            .values()
            .iter()
            .any(|item| !inside(&item.tenant))
        {
            return Err(ContractError::Empty("filter_module_outside_tenant"));
        }
        if self
            .assets
            .values()
            .iter()
            .any(|item| !inside(&item.tenant))
        {
            return Err(ContractError::Empty("filter_asset_outside_tenant"));
        }
        if self
            .counterparties
            .values()
            .iter()
            .any(|item| !inside(&item.tenant))
        {
            return Err(ContractError::Empty("filter_counterparty_outside_tenant"));
        }
        Ok(self)
    }
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct Cursor(pub Sequence);

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionCreate {
    pub scope: SubscriptionScope,
    pub filter: SubscriptionFilter,
    pub start: Cursor,
    pub delivery_target: DeliveryTarget,
}

impl SubscriptionCreate {
    /// Validates filter ownership before a durable subscription can be created.
    ///
    /// # Errors
    /// Returns the filter's cross-tenant refusal.
    pub fn validate(self) -> Result<Self, ContractError> {
        self.filter.clone().validate_for(&self.scope)?;
        Ok(self)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionList {
    pub scope: SubscriptionScope,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionTarget {
    pub scope: SubscriptionScope,
    pub subscription_id: SubscriptionId,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CursorAcknowledgement {
    pub scope: SubscriptionScope,
    pub subscription_id: SubscriptionId,
    pub cursor: Cursor,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionRecord {
    pub subscription_id: SubscriptionId,
    pub scope: SubscriptionScope,
    pub filter: SubscriptionFilter,
    pub start: Cursor,
    pub last_acknowledged: Cursor,
    pub delivery_target: DeliveryTarget,
    pub paused: bool,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct EventIdentity([u8; 32]);

impl EventIdentity {
    #[must_use]
    pub const fn new(bytes: [u8; 32]) -> Self {
        Self(bytes)
    }

    #[must_use]
    pub const fn as_bytes(self) -> [u8; 32] {
        self.0
    }
}

/// Deduplication identifier is derived only from the immutable event identity.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct DeduplicationId([u8; 32]);

impl DeduplicationId {
    #[must_use]
    pub const fn from_event_identity(identity: EventIdentity) -> Self {
        Self(identity.0)
    }

    #[must_use]
    pub const fn as_bytes(self) -> [u8; 32] {
        self.0
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ReceiptReference {
    None,
    Verified {
        receipt_ref: ReceiptRef,
        verification_level: Level,
    },
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct EventDelivery {
    pub event_identity: EventIdentity,
    pub event_bytes: Vec<u8>,
    pub deduplication_id: DeduplicationId,
    pub cursor: Cursor,
    pub receipt_reference: ReceiptReference,
}

impl EventDelivery {
    /// Binds deduplication to identity and refuses empty event bytes.
    ///
    /// # Errors
    /// Returns [`ContractError::Empty`] when event bytes are empty.
    pub fn new(
        event_identity: EventIdentity,
        event_bytes: Vec<u8>,
        cursor: Cursor,
        receipt_reference: ReceiptReference,
    ) -> Result<Self, ContractError> {
        if event_bytes.is_empty() {
            return Err(ContractError::Empty("event_bytes"));
        }
        Ok(Self {
            event_identity,
            event_bytes,
            deduplication_id: DeduplicationId::from_event_identity(event_identity),
            cursor,
            receipt_reference,
        })
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct GapNotice {
    pub missing_first: Sequence,
    pub missing_last: Sequence,
    pub backfill_cursor: Cursor,
    pub backfill_attempted: bool,
}

impl GapNotice {
    /// Validates an ordered missing range.
    ///
    /// # Errors
    /// Returns [`ContractError::Zero`] for an inverted range.
    pub const fn validate(self) -> Result<Self, ContractError> {
        if self.missing_last.0 < self.missing_first.0 {
            return Err(ContractError::Zero("gap_range"));
        }
        Ok(self)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TruncationNotice {
    pub requested_first: Sequence,
    pub oldest_available: Sequence,
    pub resume_cursor: Cursor,
}

/// All discontinuities are in-band delivery barriers.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Delivery {
    Event(EventDelivery),
    Gap(GapNotice),
    Truncated(TruncationNotice),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubscriptionHealth {
    pub target: SubscriptionTarget,
    pub last_acknowledged: Cursor,
    pub last_delivery_at: Option<TimestampSeconds>,
    pub pending_backfill: Option<GapNotice>,
}
