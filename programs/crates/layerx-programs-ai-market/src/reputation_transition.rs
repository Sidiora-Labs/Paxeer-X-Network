use crate::{
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, F07_BINDING_MISMATCH, F07_EPOCH_NOT_SEALED,
        F07_UNKNOWN_WORKER, NON_CANONICAL, RETENTION_FULL, WRONG_EPOCH,
    },
    reputation::{
        evidence_coverage, missing_quality, next_count, qualified_quality, ClosureReason,
        CompletedHistory, CompletionIdentity, Observation, ReputationCurrent, ReputationState,
        SegmentKey, LIMIT,
    },
    reputation_codec::{closure_digest, segment_digest, state_root},
    types::{Digest32, FrozenBinding, Presence, Score, Version, WorkerRosterEntry},
};

/// Configuration, policy and model a new reputation segment binds to.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SegmentBinding {
    pub config: Version,
    pub policy: Digest32,
    pub model: Digest32,
}

/// Closes `old`'s segment for `reason` and opens the next generation bound to `next`.
///
/// # Errors
/// Propagates `ReputationCurrent::matches_segment`'s `NON_CANONICAL`/`F07_SEGMENT_MISMATCH` refusals; returns `NON_CANONICAL` when `height` is zero or before the last transition; `F07_BINDING_MISMATCH` when the config moves backwards or `reason` disagrees with the binding change; `WRONG_EPOCH` when the last applied epoch is not completed; `ARITHMETIC` when the generation overflows; propagates the digest and validation refusals.
pub fn reset_segment(
    old: &ReputationCurrent,
    old_key: SegmentKey,
    next: SegmentBinding,
    reason: ClosureReason,
    latest_completed: Presence<u64>,
    height: u64,
) -> CodecResult<(SegmentKey, ReputationCurrent)> {
    old.matches_segment(old_key)?;
    if height == 0 || height < old.last_transition_height {
        return Err(NON_CANONICAL);
    }
    let SegmentBinding {
        config: new_config,
        policy: new_policy,
        model: new_model,
    } = next;
    if new_config < old_key.config {
        return Err(F07_BINDING_MISMATCH);
    }
    if let Presence::Present(applied) = old.last_applied {
        match latest_completed {
            Presence::Present(last) if last >= applied => (),
            _ => return Err(WRONG_EPOCH),
        }
    }
    match reason {
        ClosureReason::ModelChanged if new_model == old_key.model => {
            return Err(F07_BINDING_MISMATCH)
        }
        ClosureReason::PolicyChanged
            if new_model != old_key.model
                || new_config == old_key.config && new_policy == old_key.policy =>
        {
            return Err(F07_BINDING_MISMATCH)
        }
        _ => (),
    }
    let generation = old.reset_generation.next()?;
    let key = SegmentKey {
        market: old_key.market,
        worker: old_key.worker,
        config: new_config,
        policy: new_policy,
        model: new_model,
        reset_generation: generation,
    };
    let value = ReputationCurrent {
        worker: old.worker,
        segment: segment_digest(key)?,
        owner: old.owner,
        reset_generation: generation,
        quality: Score::new(0)?,
        qualifying_count: 0,
        coverage: Presence::Absent,
        last_applied: latest_completed,
        last_observed: Presence::Absent,
        last_transition_height: height,
        previous_segment: Presence::Present(closure_digest(old_key.market, old, reason)?),
        status: old.status,
    };
    value.validate()?;
    Ok((key, value))
}
/// Rolls `old` over to a new segment when its config, policy or model changed.
///
/// # Errors
/// Propagates `ReputationCurrent::matches_segment`'s and `reset_segment`'s refusals.
pub fn rollover_segment(
    old: &ReputationCurrent,
    old_key: SegmentKey,
    config: Version,
    policy: Digest32,
    model: Digest32,
    latest_completed: Presence<u64>,
    height: u64,
) -> CodecResult<Presence<(SegmentKey, ReputationCurrent)>> {
    old.matches_segment(old_key)?;
    if old_key.config == config && old_key.policy == policy && old_key.model == model {
        return Ok(Presence::Absent);
    }
    let reason = if old_key.model == model {
        ClosureReason::PolicyChanged
    } else {
        ClosureReason::ModelChanged
    };
    Ok(Presence::Present(reset_segment(
        old,
        old_key,
        SegmentBinding {
            config,
            policy,
            model,
        },
        reason,
        latest_completed,
        height,
    )?))
}

// Internal numerical primitive. The real F05/F06 bridge must select these inputs
// from authenticated shared producer state; no public score-vector entry exists.
/// One worker's epoch observation: aggregated median, supporting and eligible evaluators.
#[allow(
    dead_code,
    reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) struct EpochTally {
    pub(crate) median: Presence<Score>,
    pub(crate) support: u8,
    pub(crate) eligible: u8,
}

#[allow(
    dead_code,
    reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
)]
pub(crate) fn update_record(
    record: &ReputationCurrent,
    key: SegmentKey,
    frozen: FrozenBinding,
    worker: &WorkerRosterEntry,
    tally: EpochTally,
    history_allowed: bool,
    height: u64,
) -> CodecResult<ReputationCurrent> {
    let EpochTally {
        median,
        support,
        eligible,
    } = tally;
    record.matches_segment(key)?;
    if key.market != frozen.market
        || key.config != frozen.config
        || worker.worker != key.worker
        || record.owner != worker.owner
    {
        return Err(F07_BINDING_MISMATCH);
    }
    if height == 0 || height < record.last_transition_height {
        return Err(NON_CANONICAL);
    }
    if let Presence::Present(e) = record.last_applied {
        if frozen.epoch <= e {
            return Err(WRONG_EPOCH);
        }
    }
    let coverage = evidence_coverage(eligible, support)?;
    if matches!(median, Presence::Present(_)) != (support >= 3) {
        return Err(F07_BINDING_MISMATCH);
    }
    let mut next = *record;
    let observation = if history_allowed {
        median
    } else {
        Presence::Absent
    };
    match observation {
        Presence::Present(s) => {
            next.quality = qualified_quality(record.quality, s)?;
            next.qualifying_count = next_count(record.qualifying_count, true)?;
            next.last_observed = Presence::Present(Observation {
                epoch: frozen.epoch,
                height,
            });
        }
        Presence::Absent => {
            next.quality = missing_quality(record.quality)?;
        }
    }
    next.coverage = coverage;
    next.last_applied = Presence::Present(frozen.epoch);
    next.last_transition_height = height;
    next.validate()?;
    Ok(next)
}

#[allow(
    dead_code,
    reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
)]
#[allow(
    clippy::large_enum_variant,
    reason = "no_std without alloc: Box is unavailable, and the large Pending draft is the primary path"
)]
pub(crate) enum CompletionStart {
    Retained(CompletedHistory),
    Pending(CompletionDraft),
}
#[allow(
    dead_code,
    reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
)]
pub(crate) struct CompletionDraft {
    staged: ReputationState,
    original: ReputationState,
    frozen: FrozenBinding,
    result: Digest32,
    height: u64,
    roster: [Option<WorkerRosterEntry>; LIMIT],
    roster_len: usize,
    cursor: usize,
    observed: u8,
    covered: u8,
}
#[allow(
    dead_code,
    reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
)]
impl CompletionDraft {
    pub(crate) fn begin(
        state: &ReputationState,
        frozen: FrozenBinding,
        result: Digest32,
        height: u64,
        roster: &[WorkerRosterEntry],
    ) -> CodecResult<CompletionStart> {
        if state.market != frozen.market {
            return Err(F07_BINDING_MISMATCH);
        }
        match state.assess_completion(frozen.epoch, result)? {
            CompletionIdentity::Retained(record) => {
                if record.config != frozen.config {
                    return Err(F07_BINDING_MISMATCH);
                }
                return Ok(CompletionStart::Retained(record));
            }
            CompletionIdentity::New => (),
        }
        if height == 0 {
            return Err(NON_CANONICAL);
        }
        if roster.len() > LIMIT {
            return Err(CAPACITY);
        }
        if state.history_len as usize == LIMIT {
            return Err(RETENTION_FULL);
        }
        if let Some(last) = state.completed().last() {
            if height < last.execution_height {
                return Err(NON_CANONICAL);
            }
        }
        for record in state.records() {
            if let Presence::Present(e) = record.last_applied {
                if e >= frozen.epoch {
                    return Err(WRONG_EPOCH);
                }
            }
        }
        let mut previous = None;
        let mut bounded_roster = [None; LIMIT];
        for (i, worker) in roster.iter().enumerate() {
            if previous.is_some_and(|id| id >= worker.worker) {
                return Err(NON_CANONICAL);
            }
            let current = state
                .records()
                .find(|r| r.worker == worker.worker)
                .ok_or(F07_UNKNOWN_WORKER)?;
            if current.owner != worker.owner {
                return Err(F07_BINDING_MISMATCH);
            }
            previous = Some(worker.worker);
            bounded_roster[i] = Some(*worker);
        }
        Ok(CompletionStart::Pending(Self {
            staged: state.clone(),
            original: state.clone(),
            frozen,
            result,
            height,
            roster: bounded_roster,
            roster_len: roster.len(),
            cursor: 0,
            observed: 0,
            covered: 0,
        }))
    }
    pub(crate) fn apply_worker(
        &mut self,
        key: SegmentKey,
        median: Presence<Score>,
        support: u8,
        eligible: u8,
        history_allowed: bool,
    ) -> CodecResult<()> {
        let worker = self
            .roster
            .get(self.cursor)
            .and_then(Option::as_ref)
            .ok_or(WRONG_EPOCH)?;
        let index = self
            .staged
            .records()
            .position(|r| r.worker == worker.worker)
            .ok_or(F07_UNKNOWN_WORKER)?;
        let old = self.staged.current[index].ok_or(NON_CANONICAL)?;
        let next = update_record(
            &old,
            key,
            self.frozen,
            worker,
            EpochTally {
                median,
                support,
                eligible,
            },
            history_allowed,
            self.height,
        )?;
        let observed = self
            .observed
            .checked_add(u8::from(
                history_allowed && matches!(median, Presence::Present(_)),
            ))
            .ok_or(ARITHMETIC)?;
        let covered = self
            .covered
            .checked_add(u8::from(eligible > 0))
            .ok_or(ARITHMETIC)?;
        self.staged.current[index] = Some(next);
        self.cursor += 1;
        self.observed = observed;
        self.covered = covered;
        Ok(())
    }
    pub(crate) fn finish(mut self) -> CodecResult<(ReputationState, CompletedHistory)> {
        if self.cursor != self.roster_len {
            return Err(F07_EPOCH_NOT_SEALED);
        }
        let root = state_root(&self.staged, self.frozen.epoch)?;
        let record = CompletedHistory {
            epoch: self.frozen.epoch,
            execution_height: self.height,
            config: self.frozen.config,
            result: self.result,
            root,
            observed_workers: self.observed,
            total_workers: u8::try_from(self.roster_len).map_err(|_| CAPACITY)?,
            covered_workers: self.covered,
        };
        // Append's precondition validates committed state, not the staged pending epoch.
        let mut ring = self.original;
        ring.append(record)?;
        self.staged.history = ring.history;
        self.staged.history_len = ring.history_len;
        self.staged.completed_through = ring.completed_through;
        self.staged.validate()?;
        self.staged.encoded_len()?;
        Ok((self.staged, record))
    }
}
