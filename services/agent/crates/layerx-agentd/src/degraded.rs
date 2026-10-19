//! Honest degraded-mode reads and operation gates.

use std::collections::BTreeMap;

use layerx_types::verify::VerificationLevel;

use crate::cache::{CacheValue, EvidenceKind};
use crate::obs::health::{evaluate, BoundaryConnectivity, DegradedMode, Health, HealthInput};
use crate::obs::metrics::{MetricKind, MetricLabel, Metrics, MetricsError};
use crate::store::TenantId;

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum Mode {
    Healthy,
    Unreachable,
    Behind,
    Halted,
    Emergency,
    DataUnavailable,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Reference {
    pub head_sequence: u64,
    pub checkpoint: [u8; 32],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Observation {
    Ready {
        reference: Reference,
        maximum_verification: VerificationLevel,
    },
    Unreachable,
    Halted {
        reference: Reference,
        maximum_verification: VerificationLevel,
    },
    Emergency {
        reference: Reference,
        maximum_verification: VerificationLevel,
    },
    DataUnavailable {
        reference: Reference,
        maximum_verification: VerificationLevel,
    },
}

/// Which boundary paths the current mode still serves.
#[allow(clippy::struct_excessive_bools)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Readiness {
    pub preparation: bool,
    pub submission_acknowledgement: bool,
    pub live_stream: bool,
    pub unknown_resolution: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Status {
    pub mode: Mode,
    pub reference: Option<Reference>,
    pub maximum_verification: VerificationLevel,
    pub readiness: Readiness,
    pub transitions: BTreeMap<Mode, u64>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Staleness {
    pub mode: Mode,
    pub stale: bool,
    pub value_head_sequence: u64,
    pub value_checkpoint: [u8; 32],
    pub observed_reference: Option<Reference>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DegradedRead {
    pub canonical_core_bytes: Vec<u8>,
    pub evidence_kind: EvidenceKind,
    pub evidence_id: [u8; 32],
    pub held_level: VerificationLevel,
    pub reported_level: VerificationLevel,
    pub staleness: Staleness,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReadError {
    NoVerifiedReference,
    InsufficientEvidence {
        requested: VerificationLevel,
        held: VerificationLevel,
    },
    FinalityUnavailable {
        requested: VerificationLevel,
        supported: VerificationLevel,
    },
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum OperationError {
    LiveCoreRequired(Mode),
    StreamUnavailable(Mode),
    ResolutionUnavailable,
}

#[derive(Clone, Debug)]
pub struct Controller {
    mode: Mode,
    reference: Option<Reference>,
    maximum_verification: VerificationLevel,
    transitions: BTreeMap<Mode, u64>,
}

impl Default for Controller {
    fn default() -> Self {
        Self {
            mode: Mode::Unreachable,
            reference: None,
            maximum_verification: VerificationLevel::UNVERIFIED,
            transitions: BTreeMap::new(),
        }
    }
}

impl Controller {
    #[must_use]
    pub fn status(&self) -> Status {
        let live = self.mode == Mode::Healthy;
        Status {
            mode: self.mode,
            reference: self.reference,
            maximum_verification: self.maximum_verification,
            readiness: Readiness {
                preparation: live,
                submission_acknowledgement: live,
                live_stream: live,
                unknown_resolution: self.mode != Mode::Unreachable,
            },
            transitions: self.transitions.clone(),
        }
    }

    #[must_use]
    pub fn health(&self, mut input: HealthInput) -> Health {
        input.boundary = match self.mode {
            Mode::Unreachable => BoundaryConnectivity::Unavailable,
            Mode::Healthy
            | Mode::Behind
            | Mode::Halted
            | Mode::Emergency
            | Mode::DataUnavailable => BoundaryConnectivity::Ready,
        };
        if let Some(mode) = health_mode(self.mode) {
            input.degraded_modes.insert(mode);
        }
        evaluate(input)
    }

    /// Records the controller's current mode as one degraded-state metric sample.
    ///
    /// # Errors
    ///
    /// Returns `MetricsError::UnknownTenant` when the tenant was never registered with the
    /// registry; every mode maps to a label `MetricKind::DegradedState` already accepts.
    pub fn record_metric(
        &self,
        metrics: &mut Metrics,
        tenant: &TenantId,
    ) -> Result<(), MetricsError> {
        metrics.observe(
            tenant,
            MetricKind::DegradedState,
            metric_label(self.mode),
            1,
        )
    }

    /// Serves only already verified core bytes, with an explicit level cap and staleness.
    ///
    /// # Errors
    ///
    /// Refuses a cache value below the requested level, a level the current core state no
    /// longer supports, or a read before any core reference has ever been accepted.
    pub fn serve_cached(
        &self,
        value: &CacheValue,
        requested: VerificationLevel,
    ) -> Result<DegradedRead, ReadError> {
        if self.reference.is_none() {
            return Err(ReadError::NoVerifiedReference);
        }
        if value.level() < requested {
            return Err(ReadError::InsufficientEvidence {
                requested,
                held: value.level(),
            });
        }
        if self.maximum_verification < requested {
            return Err(ReadError::FinalityUnavailable {
                requested,
                supported: self.maximum_verification,
            });
        }
        let reported_level = value.level().min(self.maximum_verification);
        let observed_reference = self.reference;
        let stale = self.mode != Mode::Healthy
            || observed_reference.is_some_and(|reference| {
                reference.head_sequence != value.observed_head_sequence()
                    || reference.checkpoint != value.observed_checkpoint()
            });
        Ok(DegradedRead {
            canonical_core_bytes: value.core_bytes().to_vec(),
            evidence_kind: value.evidence_kind(),
            evidence_id: value.evidence_id(),
            held_level: value.level(),
            reported_level,
            staleness: Staleness {
                mode: self.mode,
                stale,
                value_head_sequence: value.observed_head_sequence(),
                value_checkpoint: value.observed_checkpoint(),
                observed_reference,
            },
        })
    }

    /// Runs a preparation only while the core is live.
    ///
    /// # Errors
    ///
    /// Returns `LiveCoreRequired` carrying the current `Mode` for every mode other than
    /// `Healthy`, including `Behind`.
    pub fn guard_preparation<T>(&self, operation: impl FnOnce() -> T) -> Result<T, OperationError> {
        if self.mode == Mode::Healthy {
            Ok(operation())
        } else {
            Err(OperationError::LiveCoreRequired(self.mode))
        }
    }

    /// Acknowledges a submission only while the core is live.
    ///
    /// # Errors
    ///
    /// Returns `LiveCoreRequired` carrying the current `Mode` whenever the last observation
    /// left the controller anywhere but `Healthy`.
    pub fn guard_submission_acknowledgement<T>(
        &self,
        operation: impl FnOnce() -> T,
    ) -> Result<T, OperationError> {
        if self.mode == Mode::Healthy {
            Ok(operation())
        } else {
            Err(OperationError::LiveCoreRequired(self.mode))
        }
    }

    /// Serves a live stream only while the core is live.
    ///
    /// # Errors
    ///
    /// Returns `StreamUnavailable` carrying the current `Mode`, distinguishing a dropped stream
    /// from a refused write in every non-`Healthy` mode.
    pub fn guard_live_stream<T>(&self, operation: impl FnOnce() -> T) -> Result<T, OperationError> {
        if self.mode == Mode::Healthy {
            Ok(operation())
        } else {
            Err(OperationError::StreamUnavailable(self.mode))
        }
    }

    /// Resolves unknown submissions in every mode that still reaches the core.
    ///
    /// # Errors
    ///
    /// Returns `ResolutionUnavailable` only in `Mode::Unreachable`; halted, emergency, behind and
    /// data-unavailable modes all still resolve.
    pub fn resolve_unknown_when_reachable<T>(
        &self,
        operation: impl FnOnce() -> T,
    ) -> Result<T, OperationError> {
        if self.mode == Mode::Unreachable {
            Err(OperationError::ResolutionUnavailable)
        } else {
            Ok(operation())
        }
    }
}

/// Applies one core observation and records the resulting externally visible mode.
pub fn enter(controller: &mut Controller, observation: Observation) -> Status {
    let (mut mode, reference, maximum_verification) = match observation {
        Observation::Ready {
            reference,
            maximum_verification,
        } => (Mode::Healthy, Some(reference), maximum_verification),
        Observation::Unreachable => (
            Mode::Unreachable,
            controller.reference,
            controller.maximum_verification,
        ),
        Observation::Halted {
            reference,
            maximum_verification,
        } => (Mode::Halted, Some(reference), maximum_verification),
        Observation::Emergency {
            reference,
            maximum_verification,
        } => (Mode::Emergency, Some(reference), maximum_verification),
        Observation::DataUnavailable {
            reference,
            maximum_verification,
        } => (Mode::DataUnavailable, Some(reference), maximum_verification),
    };
    if mode == Mode::Healthy
        && (controller
            .reference
            .zip(reference)
            .is_some_and(|(previous, current)| current.head_sequence < previous.head_sequence)
            || maximum_verification < controller.maximum_verification)
    {
        mode = Mode::Behind;
    }
    controller.mode = mode;
    controller.reference = reference;
    controller.maximum_verification = maximum_verification;
    let count = controller.transitions.entry(mode).or_default();
    *count = count.saturating_add(1);
    controller.status()
}

fn health_mode(mode: Mode) -> Option<DegradedMode> {
    match mode {
        Mode::Healthy => None,
        Mode::Unreachable => Some(DegradedMode::CoreUnavailable),
        Mode::Behind => Some(DegradedMode::CoreBehind),
        Mode::Halted => Some(DegradedMode::CoreHalted),
        Mode::Emergency => Some(DegradedMode::Emergency),
        Mode::DataUnavailable => Some(DegradedMode::DataUnavailable),
    }
}

const fn metric_label(mode: Mode) -> MetricLabel {
    match mode {
        Mode::Healthy => MetricLabel::BoundaryReady,
        Mode::Unreachable => MetricLabel::BoundaryUnavailable,
        Mode::Behind => MetricLabel::BoundaryBehind,
        Mode::Halted => MetricLabel::CoreHalted,
        Mode::Emergency => MetricLabel::CoreEmergency,
        Mode::DataUnavailable => MetricLabel::DataUnavailable,
    }
}
