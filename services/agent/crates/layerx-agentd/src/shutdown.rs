//! Graceful daemon shutdown with durable in-flight and audit draining.

use std::collections::BTreeMap;

use crate::outbox::Outbox;
use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

#[derive(Default)]
pub struct DaemonLifecycle {
    accepting_work: bool,
    in_flight: BTreeMap<[u8; 32], InFlight>,
    pending_audit: Vec<Vec<u8>>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WriteStage {
    Preparing,
    Signing,
    Queued,
    Submitted,
    Acknowledged,
    Unknown,
}

impl WriteStage {
    const fn code(self) -> u8 {
        match self {
            Self::Preparing => 1,
            Self::Signing => 2,
            Self::Queued => 3,
            Self::Submitted => 4,
            Self::Acknowledged => 5,
            Self::Unknown => 6,
        }
    }

    const fn outbox_state(self) -> Option<crate::outbox::SubmissionState> {
        match self {
            Self::Preparing | Self::Signing => None,
            Self::Queued => Some(crate::outbox::SubmissionState::Queued),
            Self::Submitted => Some(crate::outbox::SubmissionState::Submitted),
            Self::Acknowledged => Some(crate::outbox::SubmissionState::Acknowledged),
            Self::Unknown => Some(crate::outbox::SubmissionState::Unknown),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct InFlight {
    stage: WriteStage,
    durable_state: Vec<u8>,
}

impl DaemonLifecycle {
    #[must_use]
    pub fn running() -> Self {
        Self {
            accepting_work: true,
            in_flight: BTreeMap::new(),
            pending_audit: Vec::new(),
        }
    }

    /// Records one queued submission and its durable state as in-flight work.
    ///
    /// # Errors
    ///
    /// Returns `NotAccepting` once shutdown has begun, `EmptyInFlight` for empty
    /// durable state, and `DuplicateInFlight` for a repeated submission identifier.
    pub fn begin_work(
        &mut self,
        submission_id: [u8; 32],
        durable_state: Vec<u8>,
    ) -> Result<(), ShutdownError> {
        self.begin_stage(submission_id, WriteStage::Queued, durable_state)
    }

    /// Records in-flight work at an explicit write stage with its durable state.
    ///
    /// # Errors
    ///
    /// Returns `NotAccepting` once shutdown has begun, `EmptyInFlight` for empty
    /// durable state, and `DuplicateInFlight` for a repeated work identifier.
    pub fn begin_stage(
        &mut self,
        work_id: [u8; 32],
        stage: WriteStage,
        durable_state: Vec<u8>,
    ) -> Result<(), ShutdownError> {
        if !self.accepting_work {
            return Err(ShutdownError::NotAccepting);
        }
        if durable_state.is_empty() {
            return Err(ShutdownError::EmptyInFlight);
        }
        if self.in_flight.contains_key(&work_id) {
            return Err(ShutdownError::DuplicateInFlight(work_id));
        }
        self.in_flight.insert(
            work_id,
            InFlight {
                stage,
                durable_state,
            },
        );
        Ok(())
    }

    /// Queues one audit record for the shutdown flush.
    ///
    /// # Errors
    ///
    /// Returns `EmptyAudit` when the record carries no bytes.
    pub fn append_audit(&mut self, record: Vec<u8>) -> Result<(), ShutdownError> {
        if record.is_empty() {
            return Err(ShutdownError::EmptyAudit);
        }
        self.pending_audit.push(record);
        Ok(())
    }

    #[must_use]
    pub const fn accepting_work(&self) -> bool {
        self.accepting_work
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ShutdownReport {
    pub in_flight_recorded: usize,
    pub pre_submission_recorded: usize,
    pub outbox_submissions_verified: usize,
    pub audit_entries_flushed: usize,
    pub accepting_work: bool,
}

#[derive(Debug)]
pub enum ShutdownError {
    NotAccepting,
    EmptyInFlight,
    DuplicateInFlight([u8; 32]),
    EmptyAudit,
    MissingOutboxRecord([u8; 32]),
    OutboxStageMismatch {
        submission_id: [u8; 32],
        expected: crate::outbox::SubmissionState,
        actual: crate::outbox::SubmissionState,
    },
    Store(StoreError),
    Arithmetic,
}

impl From<StoreError> for ShutdownError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Stops admission, durably records all in-flight work, and flushes audit entries.
///
/// # Errors
///
/// Returns `MissingOutboxRecord` or `OutboxStageMismatch` when the outbox does not
/// confirm a recorded stage, `Arithmetic` when an audit index is not
/// representable, and a store error when a key or durable write fails.
pub fn graceful(
    store: &mut Store,
    tenant: TenantId,
    outbox: &Outbox,
    lifecycle: &mut DaemonLifecycle,
) -> Result<ShutdownReport, ShutdownError> {
    lifecycle.accepting_work = false;
    let mut pre_submission_recorded = 0_usize;
    let mut outbox_submissions_verified = 0_usize;
    for (submission_id, in_flight) in &lifecycle.in_flight {
        if let Some(expected) = in_flight.stage.outbox_state() {
            let actual = outbox
                .status(*submission_id)
                .ok_or(ShutdownError::MissingOutboxRecord(*submission_id))?
                .state;
            if actual != expected {
                return Err(ShutdownError::OutboxStageMismatch {
                    submission_id: *submission_id,
                    expected,
                    actual,
                });
            }
            outbox_submissions_verified = outbox_submissions_verified.saturating_add(1);
        } else {
            pre_submission_recorded = pre_submission_recorded.saturating_add(1);
        }
        let mut durable_state = Vec::with_capacity(in_flight.durable_state.len() + 1);
        durable_state.push(in_flight.stage.code());
        durable_state.extend_from_slice(&in_flight.durable_state);
        store.put_local(
            prefixed_key(
                tenant.clone(),
                ObjectKind::Configuration,
                b"shutdown-inflight:",
                submission_id,
            )?,
            durable_state,
        )?;
    }
    for (index, audit) in lifecycle.pending_audit.iter().enumerate() {
        let index = u64::try_from(index).map_err(|_| ShutdownError::Arithmetic)?;
        store.put_local(
            prefixed_key(
                tenant.clone(),
                ObjectKind::Audit,
                b"shutdown-audit:",
                &index.to_be_bytes(),
            )?,
            audit.clone(),
        )?;
    }
    store.put_local(
        TenantKey::new(
            tenant,
            ObjectKind::Configuration,
            b"shutdown-complete".to_vec(),
        )?,
        b"durable".to_vec(),
    )?;
    let in_flight_recorded = lifecycle.in_flight.len();
    let audit_entries_flushed = lifecycle.pending_audit.len();
    lifecycle.in_flight.clear();
    lifecycle.pending_audit.clear();
    Ok(ShutdownReport {
        in_flight_recorded,
        pre_submission_recorded,
        outbox_submissions_verified,
        audit_entries_flushed,
        accepting_work: false,
    })
}

fn prefixed_key(
    tenant: TenantId,
    kind: ObjectKind,
    prefix: &[u8],
    suffix: &[u8],
) -> Result<TenantKey, StoreError> {
    let mut object_id = prefix.to_vec();
    object_id.extend_from_slice(suffix);
    TenantKey::new(tenant, kind, object_id)
}
