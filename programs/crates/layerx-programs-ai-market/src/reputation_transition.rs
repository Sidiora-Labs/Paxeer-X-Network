use crate::{errors::*, reputation::*, reputation_codec::*, types::*};

pub fn reset_segment(
    old: &ReputationCurrent,
    old_key: SegmentKey,
    new_config: Version,
    new_policy: Digest32,
    new_model: Digest32,
    reason: ClosureReason,
    latest_completed: Presence<u64>,
    height: u64,
) -> CodecResult<(SegmentKey, ReputationCurrent)> {
    old.matches_segment(old_key)?;
    if height == 0 || height < old.last_transition_height {
        return Err(NON_CANONICAL);
    }
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
    let reason = if old_key.model != model {
        ClosureReason::ModelChanged
    } else {
        ClosureReason::PolicyChanged
    };
    Ok(Presence::Present(reset_segment(
        old,
        old_key,
        config,
        policy,
        model,
        reason,
        latest_completed,
        height,
    )?))
}

// Internal numerical primitive. The real F05/F06 bridge must select these inputs
// from authenticated shared producer state; no public score-vector entry exists.
pub(crate) fn update_record(
    record: &ReputationCurrent,
    key: SegmentKey,
    frozen: FrozenBinding,
    worker: &WorkerRosterEntry,
    median: Presence<Score>,
    support: u8,
    eligible: u8,
    history_allowed: bool,
    height: u64,
) -> CodecResult<ReputationCurrent> {
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

pub(crate) enum CompletionStart {
    Retained(CompletedHistory),
    Pending(CompletionDraft),
}
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
            if previous.map_or(false, |id| id >= worker.worker) {
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
            median,
            support,
            eligible,
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
            total_workers: self.roster_len as u8,
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
