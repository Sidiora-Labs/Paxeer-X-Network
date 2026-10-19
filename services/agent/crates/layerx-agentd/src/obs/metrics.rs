use std::collections::{BTreeMap, BTreeSet};
use std::fmt::{Display, Formatter};

use crate::store::TenantId;

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum MetricKind {
    SubmissionOutcome,
    UnknownPopulation,
    UnknownAge,
    VerificationLevel,
    BoundaryLatency,
    ErrorClass,
    PolicyDecision,
    CapabilityDecision,
    BudgetUtilization,
    SubscriptionLag,
    RateLimitRefusal,
    DegradedState,
}

impl MetricKind {
    pub const ALL: [Self; 12] = [
        Self::SubmissionOutcome,
        Self::UnknownPopulation,
        Self::UnknownAge,
        Self::VerificationLevel,
        Self::BoundaryLatency,
        Self::ErrorClass,
        Self::PolicyDecision,
        Self::CapabilityDecision,
        Self::BudgetUtilization,
        Self::SubscriptionLag,
        Self::RateLimitRefusal,
        Self::DegradedState,
    ];
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum MetricLabel {
    Executed,
    Failed,
    Unknown,
    AgeUnderSecond,
    AgeUnderMinute,
    AgeAtLeastMinute,
    SequencerSigned,
    BatchIncluded,
    StateProven,
    CheckpointFinalised,
    SettlementAnchored,
    BoundaryReady,
    BoundaryBackpressured,
    BoundaryUnavailable,
    BoundaryBehind,
    CoreHalted,
    CoreEmergency,
    DataUnavailable,
    AccessDenied,
    InvalidRequest,
    StorageFailure,
    InternalFailure,
    Allowed,
    Denied,
    UtilizationLow,
    UtilizationHigh,
    LagHealthy,
    Lagging,
    RateExceeded,
}

#[derive(Clone, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct MetricKey {
    pub tenant: TenantId,
    pub kind: MetricKind,
    pub label: MetricLabel,
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct MetricPoint {
    pub total: u128,
    pub samples: u64,
    pub maximum: u64,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MetricsError {
    InvalidCapacity,
    TenantCapacityExceeded,
    UnknownTenant,
    InvalidLabel,
}

impl Display for MetricsError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        let message = match self {
            Self::InvalidCapacity => "metric tenant capacity must be finite and non-zero",
            Self::TenantCapacityExceeded => "metric tenant capacity is exhausted",
            Self::UnknownTenant => "metric tenant is not registered",
            Self::InvalidLabel => "metric label does not belong to this metric",
        };
        formatter.write_str(message)
    }
}

impl std::error::Error for MetricsError {}

#[derive(Debug)]
pub struct Metrics {
    maximum_tenants: usize,
    tenants: BTreeSet<TenantId>,
    points: BTreeMap<MetricKey, MetricPoint>,
}

impl Metrics {
    /// Creates a metrics registry bounded to a finite tenant cardinality.
    ///
    /// # Errors
    ///
    /// Returns `InvalidCapacity` when the tenant maximum is zero.
    pub fn new(maximum_tenants: usize) -> Result<Self, MetricsError> {
        if maximum_tenants == 0 {
            return Err(MetricsError::InvalidCapacity);
        }
        Ok(Self {
            maximum_tenants,
            tenants: BTreeSet::new(),
            points: BTreeMap::new(),
        })
    }

    /// Admits one tenant, accepting an already registered tenant unchanged.
    ///
    /// # Errors
    ///
    /// Returns `TenantCapacityExceeded` when a new tenant would pass the maximum.
    pub fn register_tenant(&mut self, tenant: TenantId) -> Result<(), MetricsError> {
        if self.tenants.contains(&tenant) {
            return Ok(());
        }
        if self.tenants.len() >= self.maximum_tenants {
            return Err(MetricsError::TenantCapacityExceeded);
        }
        self.tenants.insert(tenant);
        Ok(())
    }

    /// Folds one sample into the tenant point for a metric and label.
    ///
    /// # Errors
    ///
    /// Returns `UnknownTenant` for an unregistered tenant and `InvalidLabel` when the
    /// label does not belong to the metric.
    pub fn observe(
        &mut self,
        tenant: &TenantId,
        kind: MetricKind,
        label: MetricLabel,
        value: u64,
    ) -> Result<(), MetricsError> {
        if !self.tenants.contains(tenant) {
            return Err(MetricsError::UnknownTenant);
        }
        if !valid_label(kind, label) {
            return Err(MetricsError::InvalidLabel);
        }
        let point = self
            .points
            .entry(MetricKey {
                tenant: tenant.clone(),
                kind,
                label,
            })
            .or_default();
        point.total = point.total.saturating_add(u128::from(value));
        point.samples = point.samples.saturating_add(1);
        point.maximum = point.maximum.max(value);
        Ok(())
    }

    #[must_use]
    pub fn snapshot(&self, tenant: &TenantId) -> Vec<(MetricKey, MetricPoint)> {
        self.points
            .iter()
            .filter(|(key, _)| &key.tenant == tenant)
            .map(|(key, point)| (key.clone(), *point))
            .collect()
    }
}

fn valid_label(kind: MetricKind, label: MetricLabel) -> bool {
    match kind {
        MetricKind::SubmissionOutcome => matches!(
            label,
            MetricLabel::Executed | MetricLabel::Failed | MetricLabel::Unknown
        ),
        MetricKind::UnknownPopulation => label == MetricLabel::Unknown,
        MetricKind::UnknownAge => matches!(
            label,
            MetricLabel::AgeUnderSecond
                | MetricLabel::AgeUnderMinute
                | MetricLabel::AgeAtLeastMinute
        ),
        MetricKind::VerificationLevel => matches!(
            label,
            MetricLabel::SequencerSigned
                | MetricLabel::BatchIncluded
                | MetricLabel::StateProven
                | MetricLabel::CheckpointFinalised
                | MetricLabel::SettlementAnchored
        ),
        MetricKind::BoundaryLatency => matches!(
            label,
            MetricLabel::BoundaryReady
                | MetricLabel::BoundaryBackpressured
                | MetricLabel::BoundaryUnavailable
        ),
        MetricKind::ErrorClass => matches!(
            label,
            MetricLabel::AccessDenied
                | MetricLabel::InvalidRequest
                | MetricLabel::StorageFailure
                | MetricLabel::BoundaryUnavailable
                | MetricLabel::InternalFailure
        ),
        MetricKind::PolicyDecision | MetricKind::CapabilityDecision => {
            matches!(label, MetricLabel::Allowed | MetricLabel::Denied)
        }
        MetricKind::BudgetUtilization => matches!(
            label,
            MetricLabel::UtilizationLow | MetricLabel::UtilizationHigh
        ),
        MetricKind::SubscriptionLag => {
            matches!(label, MetricLabel::LagHealthy | MetricLabel::Lagging)
        }
        MetricKind::RateLimitRefusal => label == MetricLabel::RateExceeded,
        MetricKind::DegradedState => matches!(
            label,
            MetricLabel::BoundaryReady
                | MetricLabel::BoundaryUnavailable
                | MetricLabel::BoundaryBehind
                | MetricLabel::CoreHalted
                | MetricLabel::CoreEmergency
                | MetricLabel::DataUnavailable
        ),
    }
}
