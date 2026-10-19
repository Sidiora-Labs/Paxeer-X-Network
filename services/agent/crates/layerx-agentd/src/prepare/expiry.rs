//! Prepared-activity expiry, reservation release and signed-byte retention.

use std::collections::BTreeMap;
use std::sync::Mutex;

use crate::budget::{release, BudgetLimiter, LimitRefusal, ReleaseKind};
use crate::session::SessionRef;

use super::Prepared;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LifecycleState {
    Prepared,
    Signing,
    Signed,
    Submitted,
    Acknowledged,
    Unknown,
    Executed,
    Failed,
    Expired,
}

impl LifecycleState {
    const fn terminal(self) -> bool {
        matches!(self, Self::Executed | Self::Failed | Self::Expired)
    }

    const fn unresolved(self) -> bool {
        matches!(self, Self::Submitted | Self::Acknowledged | Self::Unknown)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PayloadRedaction {
    Omit,
    DigestOnly,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct RetainedPreparation {
    authorization: Option<PreparationAuthorization>,
    state: LifecycleState,
    not_after: u64,
    reservation_ids: Vec<[u8; 32]>,
    signed_bytes: Option<Vec<u8>>,
    activity_id: Option<[u8; 32]>,
    payload_hash: [u8; 32],
    terminal_at_sequence: Option<u64>,
}

/// Exact session generation that owns a token-gated preparation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct PreparationAuthorization {
    pub(crate) session: SessionRef,
    pub(crate) generation: u64,
}

#[derive(Default)]
pub struct PreparationLifecycle {
    records: Mutex<BTreeMap<[u8; 32], RetainedPreparation>>,
}

impl PreparationLifecycle {
    /// Records one prepared activity with its expiry bound and its held reservations.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, or `Duplicate` when the
    /// preparation id is already registered.
    pub fn register(
        &self,
        preparation_id: [u8; 32],
        prepared: &Prepared,
        reservation_ids: Vec<[u8; 32]>,
    ) -> Result<(), LifecycleError> {
        self.register_inner(preparation_id, prepared, reservation_ids, None)
    }

    /// Records a preparation under the exact session generation that authorized it.
    pub(crate) fn register_authorized(
        &self,
        preparation_id: [u8; 32],
        prepared: &Prepared,
        reservation_ids: Vec<[u8; 32]>,
        authorization: PreparationAuthorization,
    ) -> Result<(), LifecycleError> {
        if authorization.generation == 0 {
            return Err(LifecycleError::InvalidAuthorization);
        }
        self.register_inner(
            preparation_id,
            prepared,
            reservation_ids,
            Some(authorization),
        )
    }

    fn register_inner(
        &self,
        preparation_id: [u8; 32],
        prepared: &Prepared,
        reservation_ids: Vec<[u8; 32]>,
        authorization: Option<PreparationAuthorization>,
    ) -> Result<(), LifecycleError> {
        let mut records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        if records.contains_key(&preparation_id) {
            return Err(LifecycleError::Duplicate);
        }
        records.insert(
            preparation_id,
            RetainedPreparation {
                authorization,
                state: LifecycleState::Prepared,
                not_after: prepared.envelope.timestamp_bound().not_after(),
                reservation_ids,
                signed_bytes: None,
                activity_id: None,
                payload_hash: prepared.envelope.payload_hash(),
                terminal_at_sequence: None,
            },
        );
        Ok(())
    }

    /// Fails every not-yet-submitted preparation owned by an invalidated exact generation while
    /// preserving submitted/unknown work for honest receipt resolution.
    ///
    /// # Errors
    ///
    /// Returns an error if preparation invalidation or reservation release fails.
    pub fn invalidate_authorizations(
        &self,
        invalidated: &[(SessionRef, u64)],
        current_sequence: u64,
        limiter: &BudgetLimiter,
    ) -> Result<PreparationInvalidationReport, LifecycleError> {
        self.invalidate_selected(current_sequence, limiter, |_, record| {
            record.authorization.as_ref().is_some_and(|authorization| {
                invalidated.iter().any(|(session, generation)| {
                    &authorization.session == session && authorization.generation == *generation
                })
            })
        })
    }

    /// Fails every not-yet-submitted preparation named by id (capability revocation) while
    /// preserving submitted/unknown work for honest receipt resolution.
    ///
    /// # Errors
    ///
    /// Returns an error if preparation invalidation or reservation release fails.
    pub fn invalidate_preparations(
        &self,
        preparation_ids: &std::collections::BTreeSet<[u8; 32]>,
        current_sequence: u64,
        limiter: &BudgetLimiter,
    ) -> Result<PreparationInvalidationReport, LifecycleError> {
        self.invalidate_selected(current_sequence, limiter, |preparation_id, _| {
            preparation_ids.contains(preparation_id)
        })
    }

    fn invalidate_selected(
        &self,
        current_sequence: u64,
        limiter: &BudgetLimiter,
        selected: impl Fn(&[u8; 32], &RetainedPreparation) -> bool,
    ) -> Result<PreparationInvalidationReport, LifecycleError> {
        let mut records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let mut report = PreparationInvalidationReport::default();
        for (preparation_id, record) in records.iter_mut() {
            if !selected(preparation_id, &*record) {
                continue;
            }
            match record.state {
                LifecycleState::Prepared | LifecycleState::Signing | LifecycleState::Signed => {
                    for reservation in &record.reservation_ids {
                        if release(limiter, *reservation, ReleaseKind::Failed, current_sequence)
                            .map_err(LifecycleError::Reservation)?
                        {
                            report.released_reservations.push(*reservation);
                        }
                    }
                    record.state = LifecycleState::Failed;
                    record.signed_bytes = None;
                    record.terminal_at_sequence = Some(current_sequence);
                    report.cancelled_preparations += 1;
                }
                LifecycleState::Submitted
                | LifecycleState::Acknowledged
                | LifecycleState::Unknown => {
                    report.unresolved_preserved += 1;
                }
                LifecycleState::Executed | LifecycleState::Failed | LifecycleState::Expired => {
                    report.terminal_untouched += 1;
                }
            }
        }
        Ok(report)
    }

    /// Advances one preparation along the permitted lifecycle edges only.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, `NotFound` for an unregistered
    /// preparation, or `InvalidTransition` carrying both states for a disallowed edge.
    pub fn transition(
        &self,
        preparation_id: [u8; 32],
        next: LifecycleState,
        current_sequence: u64,
    ) -> Result<(), LifecycleError> {
        self.transition_inner(preparation_id, next, current_sequence, None)
    }

    /// Advances a token-bound preparation only for its exact owning session generation.
    pub(crate) fn transition_authorized(
        &self,
        preparation_id: [u8; 32],
        next: LifecycleState,
        current_sequence: u64,
        authorization: &PreparationAuthorization,
    ) -> Result<(), LifecycleError> {
        self.transition_inner(preparation_id, next, current_sequence, Some(authorization))
    }

    fn transition_inner(
        &self,
        preparation_id: [u8; 32],
        next: LifecycleState,
        current_sequence: u64,
        authorization: Option<&PreparationAuthorization>,
    ) -> Result<(), LifecycleError> {
        let mut records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get_mut(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, authorization)?;
        if !valid_transition(record.state, next) {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: next,
            });
        }
        record.state = next;
        if next.terminal() {
            record.terminal_at_sequence = Some(current_sequence);
        }
        Ok(())
    }

    /// Attaches signed bytes and their activity id to a preparation being signed.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` for empty bytes, `Unavailable` when the record lock is
    /// poisoned, `NotFound` for an unregistered preparation, or `InvalidTransition` unless the
    /// record is currently `Signing`.
    pub fn retain_signed_bytes(
        &self,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
    ) -> Result<(), LifecycleError> {
        self.retain_signed_bytes_inner(preparation_id, signed_bytes, activity_id, None)
    }

    /// Retains signed bytes only for the exact session generation that owns the preparation.
    pub(crate) fn retain_signed_bytes_authorized(
        &self,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
        authorization: &PreparationAuthorization,
    ) -> Result<(), LifecycleError> {
        self.retain_signed_bytes_inner(
            preparation_id,
            signed_bytes,
            activity_id,
            Some(authorization),
        )
    }

    fn retain_signed_bytes_inner(
        &self,
        preparation_id: [u8; 32],
        signed_bytes: Vec<u8>,
        activity_id: [u8; 32],
        authorization: Option<&PreparationAuthorization>,
    ) -> Result<(), LifecycleError> {
        if signed_bytes.is_empty() {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let mut records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get_mut(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, authorization)?;
        if record.state != LifecycleState::Signing {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: LifecycleState::Signed,
            });
        }
        record.signed_bytes = Some(signed_bytes);
        record.activity_id = Some(activity_id);
        record.state = LifecycleState::Signed;
        Ok(())
    }

    /// Admits a signed preparation for submission at the authoritative core batch time.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, `NotFound` for an unregistered
    /// preparation, `PreparationExpired` past `not_after` or once already `Expired`, or
    /// `InvalidTransition` unless the record is `Signed`.
    pub fn admit_submission(
        &self,
        preparation_id: [u8; 32],
        core_batch_time: u64,
    ) -> Result<(), LifecycleError> {
        self.admit_submission_inner(preparation_id, core_batch_time, None)
    }

    /// Admits a token-bound preparation only for its exact owning session generation.
    pub(crate) fn admit_submission_authorized(
        &self,
        preparation_id: [u8; 32],
        core_batch_time: u64,
        authorization: &PreparationAuthorization,
    ) -> Result<(), LifecycleError> {
        self.admit_submission_inner(preparation_id, core_batch_time, Some(authorization))
    }

    fn admit_submission_inner(
        &self,
        preparation_id: [u8; 32],
        core_batch_time: u64,
        authorization: Option<&PreparationAuthorization>,
    ) -> Result<(), LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, authorization)?;
        require_unexpired(record, core_batch_time)?;
        if record.state != LifecycleState::Signed {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: LifecycleState::Submitted,
            });
        }
        Ok(())
    }

    /// Checks that a preparation has not expired at the authoritative core batch time.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, `NotFound` for an unregistered
    /// preparation, or `PreparationExpired` past `not_after` or once already `Expired`.
    pub fn check_unexpired(
        &self,
        preparation_id: [u8; 32],
        core_batch_time_ms: u64,
    ) -> Result<(), LifecycleError> {
        self.check_unexpired_inner(preparation_id, core_batch_time_ms, None)
    }

    /// Checks expiry only for the exact owning session generation of a token-bound preparation.
    pub(crate) fn check_unexpired_authorized(
        &self,
        preparation_id: [u8; 32],
        core_batch_time_ms: u64,
        authorization: &PreparationAuthorization,
    ) -> Result<(), LifecycleError> {
        self.check_unexpired_inner(preparation_id, core_batch_time_ms, Some(authorization))
    }

    fn check_unexpired_inner(
        &self,
        preparation_id: [u8; 32],
        core_batch_time_ms: u64,
        authorization: Option<&PreparationAuthorization>,
    ) -> Result<(), LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, authorization)?;
        require_unexpired(record, core_batch_time_ms)
    }

    /// Returns the current lifecycle state of one preparation.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, or `NotFound` for an
    /// unregistered preparation.
    pub fn state(&self, preparation_id: [u8; 32]) -> Result<LifecycleState, LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        records
            .get(&preparation_id)
            .map(|record| record.state)
            .ok_or(LifecycleError::NotFound)
    }

    /// Reports whether signed bytes are still retained for one preparation.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, or `NotFound` for an
    /// unregistered preparation.
    pub fn has_signed_bytes(&self, preparation_id: [u8; 32]) -> Result<bool, LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        records
            .get(&preparation_id)
            .map(|record| record.signed_bytes.is_some())
            .ok_or(LifecycleError::NotFound)
    }

    /// Returns the retained signed bytes only for the exact owning session generation.
    pub(crate) fn signed_bytes_authorized(
        &self,
        preparation_id: [u8; 32],
        authorization: &PreparationAuthorization,
    ) -> Result<Vec<u8>, LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, Some(authorization))?;
        if record.state != LifecycleState::Signed {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: LifecycleState::Signed,
            });
        }
        record
            .signed_bytes
            .clone()
            .ok_or(LifecycleError::InvalidSignedBytes)
    }

    /// Returns the retained activity id and signed bytes only for the exact owning generation.
    pub(crate) fn signed_activity_authorized(
        &self,
        preparation_id: [u8; 32],
        authorization: &PreparationAuthorization,
    ) -> Result<([u8; 32], Vec<u8>), LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        require_authorization(record, Some(authorization))?;
        if record.state != LifecycleState::Signed {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: LifecycleState::Signed,
            });
        }
        let activity_id = record
            .activity_id
            .ok_or(LifecycleError::ActivityIdUnavailable)?;
        let signed_bytes = record
            .signed_bytes
            .clone()
            .ok_or(LifecycleError::InvalidSignedBytes)?;
        Ok((activity_id, signed_bytes))
    }

    /// Renders one log line carrying the activity id and never the payload bytes.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the record lock is poisoned, `NotFound` for an unregistered
    /// preparation, or `ActivityIdUnavailable` before signing assigned an activity id.
    pub fn redacted_log(
        &self,
        preparation_id: [u8; 32],
        policy: PayloadRedaction,
    ) -> Result<String, LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        let activity_id = record
            .activity_id
            .ok_or(LifecycleError::ActivityIdUnavailable)?;
        let mut line = format!("activity_id={} payload=[redacted]", hex(&activity_id));
        if policy == PayloadRedaction::DigestOnly {
            line.push_str(" payload_hash=");
            line.push_str(&hex(&record.payload_hash));
        }
        Ok(line)
    }

    pub(crate) fn restore_durable(
        &self,
        durable: &[DurablePreparation],
    ) -> Result<usize, LifecycleError> {
        let mut records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let mut restored = BTreeMap::new();
        for record in durable.iter().filter(|record| !record.terminal()) {
            if record.generation == 0 {
                return Err(LifecycleError::InvalidAuthorization);
            }
            let signed_bytes = record.signed_bytes()?;
            if records.contains_key(&record.preparation_id)
                || restored.contains_key(&record.preparation_id)
            {
                return Err(LifecycleError::Duplicate);
            }
            restored.insert(
                record.preparation_id,
                RetainedPreparation {
                    authorization: Some(PreparationAuthorization {
                        session: SessionRef::new(
                            record.tenant.clone(),
                            crate::session::SessionId(record.session_id),
                        ),
                        generation: record.generation,
                    }),
                    state: record.state,
                    not_after: record.not_after,
                    reservation_ids: vec![record.preparation_id],
                    signed_bytes,
                    activity_id: record.activity_id,
                    payload_hash: record.payload_hash,
                    terminal_at_sequence: None,
                },
            );
        }
        let count = restored.len();
        records.extend(restored);
        Ok(count)
    }

    pub(crate) fn persist_signature(
        &self,
        durable: &mut DurablePreparation,
        next: LifecycleState,
    ) -> Result<bool, LifecycleError> {
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&durable.preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        let owner = PreparationAuthorization {
            session: SessionRef::new(
                durable.tenant.clone(),
                crate::session::SessionId(durable.session_id),
            ),
            generation: durable.generation,
        };
        require_authorization(record, Some(&owner))?;
        if record.state != next {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: next,
            });
        }
        let signed = match next {
            LifecycleState::Signing => None,
            LifecycleState::Signed => Some((
                record
                    .activity_id
                    .ok_or(LifecycleError::ActivityIdUnavailable)?,
                record
                    .signed_bytes
                    .clone()
                    .ok_or(LifecycleError::InvalidSignedBytes)?,
            )),
            _ => {
                return Err(LifecycleError::InvalidTransition {
                    from: durable.state,
                    to: next,
                })
            }
        };
        let stored = durable
            .signed_bytes()?
            .zip(durable.activity_id)
            .map(|(bytes, activity_id)| (activity_id, bytes));
        if durable.state == next {
            return if stored == signed {
                Ok(false)
            } else {
                Err(LifecycleError::AuthorizationMismatch)
            };
        }
        let reachable = valid_transition(durable.state, next)
            || (valid_transition(durable.state, LifecycleState::Signing)
                && valid_transition(LifecycleState::Signing, next));
        if !reachable {
            return Err(LifecycleError::InvalidTransition {
                from: durable.state,
                to: next,
            });
        }
        durable.state = next;
        if let Some((activity_id, bytes)) = signed {
            durable.activity_id = Some(activity_id);
            durable.extensions.insert(EXTENSION_SIGNED_BYTES, bytes);
        }
        Ok(true)
    }

    pub(crate) fn persist_sent(
        &self,
        durable: &mut DurablePreparation,
        next: LifecycleState,
    ) -> Result<bool, LifecycleError> {
        if !matches!(
            next,
            LifecycleState::Submitted | LifecycleState::Acknowledged
        ) {
            return Err(LifecycleError::InvalidTransition {
                from: durable.state,
                to: next,
            });
        }
        let records = self
            .records
            .lock()
            .map_err(|_| LifecycleError::Unavailable)?;
        let record = records
            .get(&durable.preparation_id)
            .ok_or(LifecycleError::NotFound)?;
        let owner = PreparationAuthorization {
            session: SessionRef::new(
                durable.tenant.clone(),
                crate::session::SessionId(durable.session_id),
            ),
            generation: durable.generation,
        };
        require_authorization(record, Some(&owner))?;
        if record.state != next {
            return Err(LifecycleError::InvalidTransition {
                from: record.state,
                to: next,
            });
        }
        if durable.state == next {
            return Ok(false);
        }
        if !valid_transition(durable.state, next) {
            return Err(LifecycleError::InvalidTransition {
                from: durable.state,
                to: next,
            });
        }
        let stored = durable.signed_bytes()?;
        if stored.is_none() || stored != record.signed_bytes {
            return Err(LifecycleError::AuthorizationMismatch);
        }
        durable.state = next;
        Ok(true)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExpirationReport {
    pub expired_preparations: Vec<[u8; 32]>,
    pub released_reservations: Vec<[u8; 32]>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RetentionReport {
    pub discarded_terminal_signed_bytes: usize,
    pub preserved_unresolved_signed_bytes: usize,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct PreparationInvalidationReport {
    pub cancelled_preparations: usize,
    pub unresolved_preserved: usize,
    pub terminal_untouched: usize,
    pub released_reservations: Vec<[u8; 32]>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum LifecycleError {
    Duplicate,
    NotFound,
    Unavailable,
    InvalidSignedBytes,
    ActivityIdUnavailable,
    PreparationExpired,
    InvalidAuthorization,
    AuthorizationRequired,
    AuthorizationMismatch,
    InvalidTransition {
        from: LifecycleState,
        to: LifecycleState,
    },
    Reservation(LimitRefusal),
}

fn require_unexpired(
    record: &RetainedPreparation,
    core_batch_time_ms: u64,
) -> Result<(), LifecycleError> {
    if core_batch_time_ms > record.not_after || record.state == LifecycleState::Expired {
        return Err(LifecycleError::PreparationExpired);
    }
    Ok(())
}

fn require_authorization(
    record: &RetainedPreparation,
    presented: Option<&PreparationAuthorization>,
) -> Result<(), LifecycleError> {
    match (&record.authorization, presented) {
        (None, None) => Ok(()),
        (Some(expected), Some(presented)) if expected == presented => Ok(()),
        (Some(_), None) => Err(LifecycleError::AuthorizationRequired),
        _ => Err(LifecycleError::AuthorizationMismatch),
    }
}

pub(crate) fn expire_elapsed(
    lifecycle: &PreparationLifecycle,
    limiter: &BudgetLimiter,
    core_batch_time_ms: u64,
    current_sequence: u64,
) -> Result<ExpirationReport, LifecycleError> {
    let mut records = lifecycle
        .records
        .lock()
        .map_err(|_| LifecycleError::Unavailable)?;
    let candidates: Vec<_> = records
        .iter()
        .filter(|(_, record)| {
            matches!(
                record.state,
                LifecycleState::Prepared | LifecycleState::Signing | LifecycleState::Signed
            ) && core_batch_time_ms > record.not_after
        })
        .map(|(id, record)| (*id, record.reservation_ids.clone()))
        .collect();
    let mut report = ExpirationReport {
        expired_preparations: Vec::new(),
        released_reservations: Vec::new(),
    };
    for (preparation_id, reservations) in candidates {
        for reservation_id in reservations {
            if release(
                limiter,
                reservation_id,
                ReleaseKind::Expired,
                current_sequence,
            )
            .map_err(LifecycleError::Reservation)?
            {
                report.released_reservations.push(reservation_id);
            }
        }
        if let Some(record) = records.get_mut(&preparation_id) {
            record.state = LifecycleState::Expired;
            record.terminal_at_sequence = Some(current_sequence);
        }
        report.expired_preparations.push(preparation_id);
    }
    Ok(report)
}

pub(crate) fn sweep_retention(
    lifecycle: &PreparationLifecycle,
    current_sequence: u64,
    retention_sequences: u64,
) -> Result<RetentionReport, LifecycleError> {
    let mut records = lifecycle
        .records
        .lock()
        .map_err(|_| LifecycleError::Unavailable)?;
    let mut report = RetentionReport {
        discarded_terminal_signed_bytes: 0,
        preserved_unresolved_signed_bytes: 0,
    };
    for record in records.values_mut() {
        if record.state.unresolved() && record.signed_bytes.is_some() {
            report.preserved_unresolved_signed_bytes += 1;
            continue;
        }
        if record.state.terminal()
            && record.signed_bytes.is_some()
            && record.terminal_at_sequence.is_some_and(|terminal| {
                current_sequence >= terminal.saturating_add(retention_sequences)
            })
        {
            record.signed_bytes = None;
            report.discarded_terminal_signed_bytes += 1;
        }
    }
    Ok(report)
}

const fn valid_transition(from: LifecycleState, to: LifecycleState) -> bool {
    matches!(
        (from, to),
        (LifecycleState::Prepared, LifecycleState::Signing)
            | (LifecycleState::Signing, LifecycleState::Signed)
            | (LifecycleState::Signed, LifecycleState::Submitted)
            | (
                LifecycleState::Submitted,
                LifecycleState::Acknowledged
                    | LifecycleState::Unknown
                    | LifecycleState::Executed
                    | LifecycleState::Failed,
            )
            | (
                LifecycleState::Acknowledged,
                LifecycleState::Unknown | LifecycleState::Executed | LifecycleState::Failed,
            )
            | (
                LifecycleState::Unknown,
                LifecycleState::Executed | LifecycleState::Failed,
            )
    )
}

fn hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(DIGITS[usize::from(byte >> 4)]));
        output.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    output
}

/// Extension tag reserved for the capability binding of a preparation.
pub const EXTENSION_CAPABILITY: u16 = 1;
/// Extension tag reserved for an idempotent preparation outcome.
pub const EXTENSION_OUTCOME: u16 = 2;
/// Extension tag written by the admission seam: the activity's 32-byte idempotency key.
pub const EXTENSION_IDEMPOTENCY: u16 = 3;

const EXTENSION_SIGNED_BYTES: u16 = 4;
const PREPARATION_PREFIX: &[u8] = b"prepare/record/";
const PREPARATION_MAGIC: &[u8; 4] = b"LXPR";
const PREPARATION_VERSION: u8 = 1;

const PREPARATION_VERSION_SIGNED: u8 = 2;
const MAX_SIGNED_BYTES: usize = 1_048_576;

const fn extension_bound(version: u8, tag: u16) -> usize {
    if version == PREPARATION_VERSION_SIGNED && tag == EXTENSION_SIGNED_BYTES {
        MAX_SIGNED_BYTES
    } else {
        MAX_EXTENSION_BYTES
    }
}
const MAX_PREPARATION_BYTES: usize = 1_048_576;
const MAX_EXTENSION_BYTES: usize = 65_536;
const MAX_HOLDS: usize = 64;
const MAX_EXTENSIONS: usize = 64;

/// One tagged extension carried by a durable preparation record.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PreparationExtension {
    pub tag: u16,
    pub bytes: Vec<u8>,
}

/// Durable preparation record holding the authorizing session generation, the lifecycle state,
/// every reservation hold applied to it and opaque tagged extensions, in one store record so
/// publication is atomic.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DurablePreparation {
    pub tenant: crate::store::TenantId,
    pub preparation_id: [u8; 32],
    pub session_id: [u8; 32],
    pub generation: u64,
    pub not_after: u64,
    pub payload_hash: [u8; 32],
    pub state: LifecycleState,
    pub activity_id: Option<[u8; 32]>,
    pub holds: Vec<(
        crate::budget::DurableBudgetReservation,
        Option<crate::budget::CoreTimestampMs>,
    )>,
    pub extensions: BTreeMap<u16, Vec<u8>>,
}

const STATES: [LifecycleState; 9] = [
    LifecycleState::Prepared,
    LifecycleState::Signing,
    LifecycleState::Signed,
    LifecycleState::Submitted,
    LifecycleState::Acknowledged,
    LifecycleState::Unknown,
    LifecycleState::Executed,
    LifecycleState::Failed,
    LifecycleState::Expired,
];

impl DurablePreparation {
    /// Whether the record reached a terminal state.
    #[must_use]
    pub const fn terminal(&self) -> bool {
        self.state.terminal()
    }

    /// Store key of one tenant's durable preparation record.
    ///
    /// # Errors
    ///
    /// Returns `Unavailable` when the key cannot be constructed.
    pub fn store_key(
        tenant: &crate::store::TenantId,
        preparation_id: [u8; 32],
    ) -> Result<crate::store::TenantKey, LifecycleError> {
        crate::store::TenantKey::new(
            tenant.clone(),
            crate::store::ObjectKind::Configuration,
            [PREPARATION_PREFIX, &preparation_id].concat(),
        )
        .map_err(|_| LifecycleError::Unavailable)
    }

    /// Lists the preparation identifiers durably recorded for one tenant.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` for a record identifier that is not 32 bytes.
    pub fn recorded_ids(
        store: &crate::store::Store,
        tenant: &crate::store::TenantId,
    ) -> Result<Vec<[u8; 32]>, LifecycleError> {
        store
            .list_object_ids(tenant, crate::store::ObjectKind::Configuration)
            .into_iter()
            .filter_map(|id| {
                id.strip_prefix(PREPARATION_PREFIX).map(|rest| {
                    <[u8; 32]>::try_from(rest).map_err(|_| LifecycleError::InvalidSignedBytes)
                })
            })
            .collect()
    }

    /// Canonical record bytes.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` when a bound is exceeded.
    pub fn encode(&self) -> Result<Vec<u8>, LifecycleError> {
        use crate::budget::LimitScope;
        let invalid = |_| LifecycleError::InvalidSignedBytes;
        if self.holds.len() > MAX_HOLDS || self.extensions.len() > MAX_EXTENSIONS {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let version = if self
            .extensions
            .get(&EXTENSION_SIGNED_BYTES)
            .is_some_and(|bytes| bytes.len() > MAX_EXTENSION_BYTES)
        {
            PREPARATION_VERSION_SIGNED
        } else {
            PREPARATION_VERSION
        };
        let capacity = if version == PREPARATION_VERSION {
            MAX_PREPARATION_BYTES
        } else {
            MAX_PREPARATION_BYTES + MAX_SIGNED_BYTES
        };
        let mut encoder = layerx_wire::encode::Encoder::new(capacity);
        encoder.fixed(PREPARATION_MAGIC).map_err(invalid)?;
        encoder.u8(version).map_err(invalid)?;
        encoder.fixed(&self.preparation_id).map_err(invalid)?;
        encoder.fixed(&self.session_id).map_err(invalid)?;
        encoder.u64(self.generation).map_err(invalid)?;
        encoder.u64(self.not_after).map_err(invalid)?;
        encoder.fixed(&self.payload_hash).map_err(invalid)?;
        let state = STATES
            .iter()
            .position(|state| *state == self.state)
            .and_then(|position| u8::try_from(position).ok())
            .ok_or(LifecycleError::InvalidSignedBytes)?;
        encoder.u8(state).map_err(invalid)?;
        match self.activity_id {
            Some(activity_id) => {
                encoder.u8(1).map_err(invalid)?;
                encoder.fixed(&activity_id).map_err(invalid)?;
            }
            None => encoder.u8(0).map_err(invalid)?,
        }
        encoder
            .u16(u16::try_from(self.holds.len()).map_err(|_| LifecycleError::InvalidSignedBytes)?)
            .map_err(invalid)?;
        for (hold, deadline) in &self.holds {
            let (tag, identity) = match hold.scope {
                LimitScope::Tenant(value) => (0_u8, value),
                LimitScope::Agent(value) => (1, value),
                LimitScope::Session(value) => (2, value),
                LimitScope::Capability(value) => (3, value),
                LimitScope::Counterparty(value) => (4, value),
            };
            encoder.fixed(&hold.reservation_id).map_err(invalid)?;
            encoder.fixed(&hold.limit_id.0).map_err(invalid)?;
            encoder.u8(tag).map_err(invalid)?;
            encoder.fixed(&identity).map_err(invalid)?;
            encoder.u128(hold.amount).map_err(invalid)?;
            encoder.u128(hold.ceiling).map_err(invalid)?;
            encoder.u64(hold.expiry_sequence).map_err(invalid)?;
            encoder.fixed(&hold.digest).map_err(invalid)?;
            match deadline {
                Some(deadline) => {
                    encoder.u8(1).map_err(invalid)?;
                    encoder.u64(deadline.0).map_err(invalid)?;
                }
                None => encoder.u8(0).map_err(invalid)?,
            }
        }
        encoder
            .u16(
                u16::try_from(self.extensions.len())
                    .map_err(|_| LifecycleError::InvalidSignedBytes)?,
            )
            .map_err(invalid)?;
        for (tag, bytes) in &self.extensions {
            if bytes.len() > extension_bound(version, *tag) {
                return Err(LifecycleError::InvalidSignedBytes);
            }
            encoder.u16(*tag).map_err(invalid)?;
            encoder
                .u32(u32::try_from(bytes.len()).map_err(|_| LifecycleError::InvalidSignedBytes)?)
                .map_err(invalid)?;
            encoder.fixed(bytes).map_err(invalid)?;
        }
        Ok(encoder.finish())
    }

    /// Decodes one canonical record; unknown extension tags are kept opaque and re-encode
    /// byte-identically.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` for any malformed, non-canonical or trailing byte.
    pub fn decode(tenant: crate::store::TenantId, bytes: &[u8]) -> Result<Self, LifecycleError> {
        use crate::budget::{CoreTimestampMs, DurableBudgetReservation, LimitId, LimitScope};
        let invalid = |_| LifecycleError::InvalidSignedBytes;
        let mut decoder = layerx_wire::decode::Decoder::new(bytes, 0);
        fn fixed<const N: usize>(
            decoder: &mut layerx_wire::decode::Decoder<'_>,
        ) -> Result<[u8; N], LifecycleError> {
            decoder
                .fixed(N)
                .map_err(|_| LifecycleError::InvalidSignedBytes)?
                .try_into()
                .map_err(|_| LifecycleError::InvalidSignedBytes)
        }
        if fixed::<4>(&mut decoder)? != *PREPARATION_MAGIC {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let version = decoder.u8().map_err(invalid)?;
        if version != PREPARATION_VERSION && version != PREPARATION_VERSION_SIGNED {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let preparation_id = fixed::<32>(&mut decoder)?;
        let session_id = fixed::<32>(&mut decoder)?;
        let generation = decoder.u64().map_err(invalid)?;
        let not_after = decoder.u64().map_err(invalid)?;
        let payload_hash = fixed::<32>(&mut decoder)?;
        let state = *STATES
            .get(usize::from(decoder.u8().map_err(invalid)?))
            .ok_or(LifecycleError::InvalidSignedBytes)?;
        let activity_id = match decoder.u8().map_err(invalid)? {
            0 => None,
            1 => Some(fixed::<32>(&mut decoder)?),
            _ => return Err(LifecycleError::InvalidSignedBytes),
        };
        let hold_count = usize::from(decoder.u16().map_err(invalid)?);
        if hold_count > MAX_HOLDS {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let mut holds = Vec::with_capacity(hold_count);
        for _ in 0..hold_count {
            let reservation_id = fixed::<32>(&mut decoder)?;
            let limit_id = LimitId(fixed::<16>(&mut decoder)?);
            let tag = decoder.u8().map_err(invalid)?;
            let identity = fixed::<32>(&mut decoder)?;
            let scope = match tag {
                0 => LimitScope::Tenant(identity),
                1 => LimitScope::Agent(identity),
                2 => LimitScope::Session(identity),
                3 => LimitScope::Capability(identity),
                4 => LimitScope::Counterparty(identity),
                _ => return Err(LifecycleError::InvalidSignedBytes),
            };
            let hold = DurableBudgetReservation {
                reservation_id,
                limit_id,
                scope,
                amount: decoder.u128().map_err(invalid)?,
                ceiling: decoder.u128().map_err(invalid)?,
                expiry_sequence: decoder.u64().map_err(invalid)?,
                digest: fixed::<32>(&mut decoder)?,
            };
            let deadline = match decoder.u8().map_err(invalid)? {
                0 => None,
                1 => Some(CoreTimestampMs(decoder.u64().map_err(invalid)?)),
                _ => return Err(LifecycleError::InvalidSignedBytes),
            };
            if hold.reservation_id != preparation_id || hold.digest != hold.canonical_digest() {
                return Err(LifecycleError::InvalidSignedBytes);
            }
            holds.push((hold, deadline));
        }
        let extension_count = usize::from(decoder.u16().map_err(invalid)?);
        if extension_count > MAX_EXTENSIONS {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        let mut extensions = BTreeMap::new();
        let mut previous: Option<u16> = None;
        for _ in 0..extension_count {
            let tag = decoder.u16().map_err(invalid)?;
            let length = usize::try_from(decoder.u32().map_err(invalid)?)
                .map_err(|_| LifecycleError::InvalidSignedBytes)?;
            if previous.is_some_and(|previous| tag <= previous)
                || length > extension_bound(version, tag)
            {
                return Err(LifecycleError::InvalidSignedBytes);
            }
            extensions.insert(tag, decoder.fixed(length).map_err(invalid)?.to_vec());
            previous = Some(tag);
        }
        decoder.finish().map_err(invalid)?;
        if version == PREPARATION_VERSION_SIGNED
            && !extensions
                .get(&EXTENSION_SIGNED_BYTES)
                .is_some_and(|bytes| bytes.len() > MAX_EXTENSION_BYTES)
        {
            return Err(LifecycleError::InvalidSignedBytes);
        }
        Ok(Self {
            tenant,
            preparation_id,
            session_id,
            generation,
            not_after,
            payload_hash,
            state,
            activity_id,
            holds,
            extensions,
        })
    }

    /// Strictly decodes every durable preparation record of one tenant.
    ///
    /// # Errors
    ///
    /// Returns `InvalidSignedBytes` for a malformed record identifier, a malformed record or a
    /// record whose identifier differs from its key, and `NotFound` for a listed key without a
    /// value.
    pub fn load_all(
        store: &crate::store::Store,
        tenant: &crate::store::TenantId,
    ) -> Result<Vec<Self>, LifecycleError> {
        store
            .list_object_ids(tenant, crate::store::ObjectKind::Configuration)
            .into_iter()
            .filter_map(|id| id.strip_prefix(PREPARATION_PREFIX).map(<[u8]>::to_vec))
            .map(|rest| {
                let preparation_id = <[u8; 32]>::try_from(rest.as_slice())
                    .map_err(|_| LifecycleError::InvalidSignedBytes)?;
                let key = Self::store_key(tenant, preparation_id)?;
                let stored = store.get(&key).ok_or(LifecycleError::NotFound)?;
                let record = Self::decode(tenant.clone(), stored.bytes())?;
                if record.preparation_id != preparation_id {
                    return Err(LifecycleError::InvalidSignedBytes);
                }
                Ok(record)
            })
            .collect()
    }

    pub(crate) fn signed_bytes(&self) -> Result<Option<Vec<u8>>, LifecycleError> {
        let bytes = self.extensions.get(&EXTENSION_SIGNED_BYTES);
        let unsigned = matches!(
            self.state,
            LifecycleState::Prepared | LifecycleState::Signing
        );
        match bytes {
            None if unsigned && self.activity_id.is_none() => Ok(None),
            Some(bytes) if !unsigned && !bytes.is_empty() && self.activity_id.is_some() => {
                Ok(Some(bytes.clone()))
            }
            _ if self.terminal() => Ok(bytes.cloned()),
            _ => Err(LifecycleError::InvalidSignedBytes),
        }
    }

    pub(crate) fn drop_signed_bytes(&mut self) {
        self.extensions.remove(&EXTENSION_SIGNED_BYTES);
    }
}
