use std::collections::BTreeMap;

use sha2::{Digest, Sha256};

use super::Surface;
use crate::store::TenantId;

const NORMALIZATION_ROUNDS: u16 = 64;

/// Reviewable mitigation for cross-tenant existence timing.
pub const TIMING_MITIGATION: &str = "Missing and unauthorized targets enter one match arm before formatting, emit the same response, and execute 64 SHA-256 rounds over one fixed-size block. Raw foreign tenant and object values are discarded when the internal error is constructed.";

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum ErrorClass {
    AccessDenied,
    InvalidRequest,
    Storage,
    Boundary,
    Internal,
}

/// Internal failure whose potentially foreign context is deliberately discarded.
#[derive(Clone, Eq, PartialEq)]
pub struct InternalError {
    class: InternalClass,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum InternalClass {
    Missing,
    NotAuthorized,
    InvalidRequest,
    Storage,
    Boundary,
    Internal,
}

impl InternalError {
    #[must_use]
    pub fn missing(_raw_object_identifier: impl Into<Vec<u8>>) -> Self {
        Self {
            class: InternalClass::Missing,
        }
    }

    #[must_use]
    pub fn not_authorized(
        _foreign_tenant: impl Into<String>,
        _raw_object_identifier: impl Into<Vec<u8>>,
    ) -> Self {
        Self {
            class: InternalClass::NotAuthorized,
        }
    }

    #[must_use]
    pub const fn invalid_request() -> Self {
        Self {
            class: InternalClass::InvalidRequest,
        }
    }

    #[must_use]
    pub fn storage(_internal_diagnostic: impl Into<String>) -> Self {
        Self {
            class: InternalClass::Storage,
        }
    }

    #[must_use]
    pub fn boundary(_internal_diagnostic: impl Into<String>) -> Self {
        Self {
            class: InternalClass::Boundary,
        }
    }

    #[must_use]
    pub fn internal(_internal_diagnostic: impl Into<String>) -> Self {
        Self {
            class: InternalClass::Internal,
        }
    }
}

impl std::fmt::Debug for InternalError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("InternalError")
            .field("class", &self.class)
            .field("context", &"[redacted]")
            .finish()
    }
}

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
}

/// Closed metric labels. No raw or caller-provided identifier can be represented.
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
    AccessDenied,
    InvalidRequest,
    StorageFailure,
    BoundaryFailure,
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
pub struct BoundedMetricKey {
    pub tenant: TenantId,
    pub surface: Surface,
    pub kind: MetricKind,
    pub label: MetricLabel,
}

#[derive(Debug, Default)]
pub struct BoundedMetrics {
    counters: BTreeMap<BoundedMetricKey, u64>,
    traces: Vec<SanitizedTrace>,
}

impl BoundedMetrics {
    pub fn record(&mut self, key: BoundedMetricKey, value: u64) {
        let counter = self.counters.entry(key).or_default();
        *counter = counter.saturating_add(value);
    }

    #[must_use]
    pub fn counters(&self) -> &BTreeMap<BoundedMetricKey, u64> {
        &self.counters
    }

    #[must_use]
    pub fn traces(&self) -> &[SanitizedTrace] {
        &self.traces
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SanitizedTrace {
    pub tenant: TenantId,
    pub surface: Surface,
    pub error_class: ErrorClass,
    pub normalization_work_units: u16,
    pub normalization_tag: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NormalizedError {
    pub status: u16,
    pub code: &'static str,
    pub message: &'static str,
    pub class: ErrorClass,
    pub retryable: bool,
}

pub(super) fn normalize(
    error: &InternalError,
    tenant: &TenantId,
    surface: Surface,
    metrics: &mut BoundedMetrics,
) -> NormalizedError {
    let normalized = match error.class {
        InternalClass::Missing | InternalClass::NotAuthorized => NormalizedError {
            status: 404,
            code: "not_authorized",
            message: "the requested resource is unavailable",
            class: ErrorClass::AccessDenied,
            retryable: false,
        },
        InternalClass::InvalidRequest => NormalizedError {
            status: 400,
            code: "invalid_request",
            message: "the request is invalid",
            class: ErrorClass::InvalidRequest,
            retryable: false,
        },
        InternalClass::Storage => NormalizedError {
            status: 503,
            code: "internal_unavailable",
            message: "the service is temporarily unavailable",
            class: ErrorClass::Storage,
            retryable: true,
        },
        InternalClass::Boundary => NormalizedError {
            status: 503,
            code: "boundary_unavailable",
            message: "the service is temporarily unavailable",
            class: ErrorClass::Boundary,
            retryable: true,
        },
        InternalClass::Internal => NormalizedError {
            status: 500,
            code: "internal_error",
            message: "the request could not be completed",
            class: ErrorClass::Internal,
            retryable: false,
        },
    };
    let label = match normalized.class {
        ErrorClass::AccessDenied => MetricLabel::AccessDenied,
        ErrorClass::InvalidRequest => MetricLabel::InvalidRequest,
        ErrorClass::Storage => MetricLabel::StorageFailure,
        ErrorClass::Boundary => MetricLabel::BoundaryFailure,
        ErrorClass::Internal => MetricLabel::InternalFailure,
    };
    metrics.record(
        BoundedMetricKey {
            tenant: tenant.clone(),
            surface,
            kind: MetricKind::ErrorClass,
            label,
        },
        1,
    );
    let normalization_tag = fixed_normalization_work(tenant, surface, normalized.class);
    metrics.traces.push(SanitizedTrace {
        tenant: tenant.clone(),
        surface,
        error_class: normalized.class,
        normalization_work_units: NORMALIZATION_ROUNDS,
        normalization_tag,
    });
    normalized
}

fn fixed_normalization_work(tenant: &TenantId, surface: Surface, class: ErrorClass) -> [u8; 32] {
    let mut block = [0_u8; 64];
    let tenant_digest = Sha256::digest(tenant.as_str().as_bytes());
    block[..32].copy_from_slice(&tenant_digest);
    block[32] = surface as u8;
    block[33] = class as u8;
    let mut digest: [u8; 32] = Sha256::digest(block).into();
    for round in 1..NORMALIZATION_ROUNDS {
        block[..32].copy_from_slice(&digest);
        block[32..34].copy_from_slice(&round.to_be_bytes());
        digest = Sha256::digest(block).into();
    }
    digest
}
