use std::collections::BTreeSet;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BoundaryConnectivity {
    Ready,
    Backpressured,
    Unavailable,
    VersionMismatch,
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum DegradedMode {
    CoreUnavailable,
    CoreBehind,
    CoreHalted,
    Emergency,
    DataUnavailable,
    ReadOnly,
    BudgetDivergence,
}

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum WriteBlocker {
    NotLive,
    BoundaryBackpressured,
    BoundaryUnavailable,
    BoundaryVersionMismatch,
    AuditUnavailable,
    RecoveryIncomplete,
    VerificationBacklog,
    UnknownBacklog,
    Degraded,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum WriteReadiness {
    Ready,
    NotReady(BTreeSet<WriteBlocker>),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HealthInput {
    pub live: bool,
    pub boundary: BoundaryConnectivity,
    pub audit_writable: bool,
    pub recovery_complete: bool,
    pub verification_backlog: u64,
    pub maximum_verification_backlog: u64,
    pub unknown_backlog: u64,
    pub maximum_unknown_backlog: u64,
    pub degraded_modes: BTreeSet<DegradedMode>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Health {
    pub live: bool,
    pub write_readiness: WriteReadiness,
    pub boundary: BoundaryConnectivity,
    pub verification_backlog: u64,
    pub unknown_backlog: u64,
    pub degraded_modes: BTreeSet<DegradedMode>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WriteRefused {
    pub blockers: BTreeSet<WriteBlocker>,
}

impl Health {
    /// Admits a write only while readiness carries no blockers.
    ///
    /// # Errors
    ///
    /// Returns the complete blocker set whenever write readiness is `NotReady`.
    pub fn require_write_ready(&self) -> Result<(), WriteRefused> {
        match &self.write_readiness {
            WriteReadiness::Ready => Ok(()),
            WriteReadiness::NotReady(blockers) => Err(WriteRefused {
                blockers: blockers.clone(),
            }),
        }
    }
}

#[must_use]
pub fn evaluate(input: HealthInput) -> Health {
    let mut blockers = BTreeSet::new();
    if !input.live {
        blockers.insert(WriteBlocker::NotLive);
    }
    match input.boundary {
        BoundaryConnectivity::Ready => {}
        BoundaryConnectivity::Backpressured => {
            blockers.insert(WriteBlocker::BoundaryBackpressured);
        }
        BoundaryConnectivity::Unavailable => {
            blockers.insert(WriteBlocker::BoundaryUnavailable);
        }
        BoundaryConnectivity::VersionMismatch => {
            blockers.insert(WriteBlocker::BoundaryVersionMismatch);
        }
    }
    if !input.audit_writable {
        blockers.insert(WriteBlocker::AuditUnavailable);
    }
    if !input.recovery_complete {
        blockers.insert(WriteBlocker::RecoveryIncomplete);
    }
    if input.verification_backlog > input.maximum_verification_backlog {
        blockers.insert(WriteBlocker::VerificationBacklog);
    }
    if input.unknown_backlog > input.maximum_unknown_backlog {
        blockers.insert(WriteBlocker::UnknownBacklog);
    }
    if !input.degraded_modes.is_empty() {
        blockers.insert(WriteBlocker::Degraded);
    }
    Health {
        live: input.live,
        write_readiness: if blockers.is_empty() {
            WriteReadiness::Ready
        } else {
            WriteReadiness::NotReady(blockers)
        },
        boundary: input.boundary,
        verification_backlog: input.verification_backlog,
        unknown_backlog: input.unknown_backlog,
        degraded_modes: input.degraded_modes,
    }
}

/// Runs an operation only behind a passing write-readiness check.
///
/// # Errors
///
/// Returns the complete blocker set, without running the operation, whenever write readiness is
/// `NotReady`.
pub fn guard_write<T>(health: &Health, operation: impl FnOnce() -> T) -> Result<T, WriteRefused> {
    health.require_write_ready()?;
    Ok(operation())
}
