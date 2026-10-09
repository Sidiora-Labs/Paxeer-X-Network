use crate::{
    codec::{Reader, Writer},
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F07_BINDING_MISMATCH, F07_EPOCH_NOT_SEALED,
        F07_GENERATION_MISMATCH, F07_SEGMENT_MISMATCH, F07_UNKNOWN_WORKER, NON_CANONICAL,
        RETENTION_FULL, WRONG_EPOCH,
    },
    reputation::{
        check_storage_budget, evidence_coverage, missing_quality, next_count, qualified_quality,
        ClosureReason, CompletedHistory, CompletionIdentity, HistoryStatus, Observation,
        ReputationCurrent, ReputationState, SegmentKey, CURRENT_BYTES, HEADER_BYTES, HISTORY_BYTES,
        LIMIT,
    },
    reputation_codec::{
        closure_digest, decode_section, encode_section, segment_digest, state_root,
    },
    types::{Digest32, FrozenBinding, Presence, Score, Version, WorkerId, WorkerRosterEntry},
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

const SECTION_MAGIC: [u8; 4] = *b"RP07";
const SEAL_MAGIC: [u8; 4] = *b"HA07";
/// Most evaluators one frozen epoch can count as eligible.
const MAX_ELIGIBLE: u8 = 8;
/// Bytes of the per-epoch history seal: `HA07`, epoch:u64, allowed bitmap:u32, eligible:u8
/// and three reserved zero bytes.
pub const SEAL_BYTES: usize = 20;

/// The F07 admission frozen at one epoch's aggregation seal. Bit `i` of `allowed` is the
/// `history_allowed` bit of the `i`-th frozen worker in ascending `WorkerId` order; `eligible`
/// is the number of frozen evaluators still eligible at the seal (the coverage denominator).
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HistorySeal {
    pub epoch: u64,
    pub allowed: u32,
    pub eligible: u8,
}
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
impl HistorySeal {
    /// The sealed `history_allowed` bit of the frozen worker at ascending roster `index`.
    #[must_use]
    pub fn allows(&self, index: usize) -> bool {
        index < LIMIT && (self.allowed >> index) & 1 == 1
    }
}

/// The F07 region that leads the joint F07/F08 section: the reputation state and the history
/// seal of the epoch whose aggregation is sealed and not yet completed.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Region {
    pub state: ReputationState,
    pub seal: Presence<HistorySeal>,
}

/// Splits the joint F07/F08 section `RP07 section || [HA07 seal] || F08 table` into its F07
/// region and the F08 table bytes. A section that does not start with `RP07` has no F07
/// region; an F08 table starts with its `u16` schema version, so neither magic can begin it.
///
/// # Errors
/// Returns `CAPACITY` when a header count exceeds `LIMIT`; `NON_CANONICAL` for a truncated
/// region or seal, nonzero reserved seal bytes, more than eight eligible evaluators or a seal
/// for an already completed epoch; propagates `decode_section`'s refusals.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn split_joint(section: &[u8]) -> CodecResult<(Option<Region>, &[u8])> {
    if section.get(..4) != Some(SECTION_MAGIC.as_slice()) {
        return Ok((None, section));
    }
    let counts = section.get(6..8).ok_or(NON_CANONICAL)?;
    let (current, history) = (usize::from(counts[0]), usize::from(counts[1]));
    if current > LIMIT || history > LIMIT {
        return Err(CAPACITY);
    }
    let len = HEADER_BYTES + current * CURRENT_BYTES + history * HISTORY_BYTES;
    let (bytes, rest) = section.split_at_checked(len).ok_or(NON_CANONICAL)?;
    let state = decode_section(bytes)?;
    if rest.get(..4) != Some(SEAL_MAGIC.as_slice()) {
        return Ok((
            Some(Region {
                state,
                seal: Presence::Absent,
            }),
            rest,
        ));
    }
    let (seal, table) = rest.split_at_checked(SEAL_BYTES).ok_or(NON_CANONICAL)?;
    let mut r = Reader::new(seal);
    r.take(4)?;
    let seal = HistorySeal {
        epoch: r.u64()?,
        allowed: r.u32()?,
        eligible: r.u8()?,
    };
    r.reserved(3)?;
    r.finish()?;
    check_seal(&state, seal).map_err(|_| NON_CANONICAL)?;
    Ok((
        Some(Region {
            state,
            seal: Presence::Present(seal),
        }),
        table,
    ))
}

fn check_seal(state: &ReputationState, seal: HistorySeal) -> CodecResult<()> {
    if seal.eligible > MAX_ELIGIBLE {
        return Err(F07_BINDING_MISMATCH);
    }
    if let Presence::Present(last) = state.latest_completed() {
        if seal.epoch <= last {
            return Err(WRONG_EPOCH);
        }
    }
    Ok(())
}

/// Encodes `region` followed by the F08 `table` into `out` and returns the written length.
///
/// # Errors
/// Propagates `encode_section`'s refusals; returns `NON_CANONICAL` for an invalid seal;
/// `F07_RESOURCE_LIMIT` when the F07 or joint size exceeds its cap; `CAPACITY` when `out` is
/// too short.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn encode_joint(region: &Region, table: &[u8], out: &mut [u8]) -> CodecResult<usize> {
    let mut len = encode_section(&region.state, out)?;
    if let Presence::Present(seal) = region.seal {
        check_seal(&region.state, seal).map_err(|_| NON_CANONICAL)?;
        let mut w = Writer::new(out.get_mut(len..).ok_or(CAPACITY)?);
        w.put(&SEAL_MAGIC)?;
        w.u64(seal.epoch)?;
        w.u32(seal.allowed)?;
        w.u8(seal.eligible)?;
        w.put(&[0; 3])?;
        len += SEAL_BYTES;
    }
    check_storage_budget(len, table.len(), 0)?;
    let end = len.checked_add(table.len()).ok_or(ARITHMETIC)?;
    out.get_mut(len..end)
        .ok_or(CAPACITY)?
        .copy_from_slice(table);
    Ok(end)
}

/// Freezes `history_allowed` for the ascending frozen `workers` of `epoch` at its aggregation
/// seal: a worker's bit is set exactly when its history status is `Active`.
///
/// # Errors
/// Returns `CAPACITY` for more than `LIMIT` workers; `F07_BINDING_MISMATCH` for more than eight
/// eligible evaluators; `WRONG_EPOCH` for an already completed epoch; `NON_CANONICAL` for
/// unordered workers; `F07_UNKNOWN_WORKER` for a frozen worker without a record.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn seal_history(
    state: &ReputationState,
    epoch: u64,
    workers: &[WorkerId],
    eligible: u8,
) -> CodecResult<HistorySeal> {
    if workers.len() > LIMIT {
        return Err(CAPACITY);
    }
    let mut seal = HistorySeal {
        epoch,
        allowed: 0,
        eligible,
    };
    check_seal(state, seal)?;
    let mut previous = None;
    for (i, worker) in workers.iter().enumerate() {
        if previous.is_some_and(|id| id >= *worker) {
            return Err(NON_CANONICAL);
        }
        previous = Some(*worker);
        let record = state
            .records()
            .find(|r| r.worker == *worker)
            .ok_or(F07_UNKNOWN_WORKER)?;
        if record.status == HistoryStatus::Active {
            seal.allowed |= 1u32 << i;
        }
    }
    Ok(seal)
}

/// The deterministic F07 phase of one epoch opening. Every live record bound to the
/// `previous` binding rolls over to `next` (closing its segment as `PolicyChanged` or
/// `ModelChanged`); a record whose worker `departed` the F02 table is retired with its segment
/// digest preserved; a frozen worker without a record is bootstrapped unobserved at reset
/// generation 1.
///
/// # Errors
/// Propagates `rollover_segment`'s refusals (`F07_SEGMENT_MISMATCH` for a record not bound to
/// `previous`); returns `F07_BINDING_MISMATCH` when a frozen worker's owner differs from its
/// record or its record is retired; `CAPACITY` for a record beyond `LIMIT`; `NON_CANONICAL`
/// for a height before a record's last transition.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn open_segments(
    state: &ReputationState,
    previous: SegmentBinding,
    next: SegmentBinding,
    roster: &[WorkerRosterEntry],
    departed: impl Fn(WorkerId) -> bool,
    height: u64,
) -> CodecResult<ReputationState> {
    let mut opened = state.clone();
    let latest = state.latest_completed();
    for slot in opened.current.iter_mut().flatten() {
        if slot.status == HistoryStatus::Retired {
            continue;
        }
        if height == 0 || height < slot.last_transition_height {
            return Err(NON_CANONICAL);
        }
        if departed(slot.worker) {
            slot.status = HistoryStatus::Retired;
            slot.last_transition_height = height;
            continue;
        }
        let key = SegmentKey {
            market: state.market,
            worker: slot.worker,
            config: previous.config,
            policy: previous.policy,
            model: previous.model,
            reset_generation: slot.reset_generation,
        };
        if let Presence::Present((_, rolled)) = rollover_segment(
            slot,
            key,
            next.config,
            next.policy,
            next.model,
            latest,
            height,
        )? {
            *slot = rolled;
        }
    }
    for entry in roster {
        let existing = opened.records().find(|r| r.worker == entry.worker).copied();
        match existing {
            Some(r) if r.owner != entry.owner || r.status == HistoryStatus::Retired => {
                return Err(F07_BINDING_MISMATCH)
            }
            Some(_) => (),
            None => {
                let key = SegmentKey {
                    market: state.market,
                    worker: entry.worker,
                    config: next.config,
                    policy: next.policy,
                    model: next.model,
                    reset_generation: Version::new(1)?,
                };
                opened.insert(ReputationCurrent::bootstrap(key, entry.owner, height)?)?;
            }
        }
    }
    opened.validate()?;
    Ok(opened)
}

/// The record of `worker` and its segment key under the current `binding`.
///
/// # Errors
/// Returns `F07_UNKNOWN_WORKER` without a record; propagates `ReputationCurrent::matches_segment`.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn bound_record(
    state: &ReputationState,
    worker: WorkerId,
    binding: SegmentBinding,
) -> CodecResult<(ReputationCurrent, SegmentKey)> {
    let record = *state
        .records()
        .find(|r| r.worker == worker)
        .ok_or(F07_UNKNOWN_WORKER)?;
    let key = SegmentKey {
        market: state.market,
        worker,
        config: binding.config,
        policy: binding.policy,
        model: binding.model,
        reset_generation: record.reset_generation,
    };
    record.matches_segment(key)?;
    Ok((record, key))
}

/// The expected-context check of an administrative request: the reset generation first, so a
/// previous generation's replay is a `F07_GENERATION_MISMATCH`, then the segment digest.
///
/// # Errors
/// Returns `F07_GENERATION_MISMATCH` or `F07_SEGMENT_MISMATCH`.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn check_expected(
    record: &ReputationCurrent,
    segment: Digest32,
    generation: u64,
) -> CodecResult<()> {
    if record.reset_generation.get() != generation {
        return Err(F07_GENERATION_MISMATCH);
    }
    if record.segment != segment {
        return Err(F07_SEGMENT_MISMATCH);
    }
    Ok(())
}

/// `record` with history status `status` from `height`; scores and counters are retained.
///
/// # Errors
/// Propagates `ReputationCurrent::matches_segment`; returns `CONFLICT` for a retired record, a
/// retirement request or an unchanged status; `NON_CANONICAL` for a height before the last
/// transition.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn set_status(
    record: &ReputationCurrent,
    key: SegmentKey,
    status: HistoryStatus,
    height: u64,
) -> CodecResult<ReputationCurrent> {
    record.matches_segment(key)?;
    if record.status == HistoryStatus::Retired
        || status == HistoryStatus::Retired
        || record.status == status
    {
        return Err(CONFLICT);
    }
    if height == 0 || height < record.last_transition_height {
        return Err(NON_CANONICAL);
    }
    let mut next = *record;
    next.status = status;
    next.last_transition_height = height;
    next.validate()?;
    Ok(next)
}

/// `state` with the record of `record.worker` replaced.
///
/// # Errors
/// Returns `F07_UNKNOWN_WORKER` without a record; propagates `ReputationState::validate`.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn replace_record(
    state: &ReputationState,
    record: ReputationCurrent,
) -> CodecResult<ReputationState> {
    let mut next = state.clone();
    let slot = next
        .current
        .iter_mut()
        .flatten()
        .find(|r| r.worker == record.worker)
        .ok_or(F07_UNKNOWN_WORKER)?;
    *slot = record;
    next.validate()?;
    Ok(next)
}

/// Removes, oldest first, every completed summary whose epoch the common F06 ring no longer
/// `retained`; a summary F06 still retains (and every later one) is kept.
///
/// # Errors
/// Propagates `retained`'s and `ReputationState::prune_oldest`'s refusals.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
pub fn prune_released(
    state: &ReputationState,
    retained: impl Fn(u64) -> CodecResult<bool>,
) -> CodecResult<ReputationState> {
    let mut next = state.clone();
    loop {
        let Some(first) = next.completed().next().copied() else {
            break;
        };
        if retained(first.epoch)? {
            break;
        }
        next.prune_oldest(first.epoch)?;
    }
    Ok(next)
}

/// One frozen worker's sealed F05 terminal output.
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerObservation {
    pub median: Presence<Score>,
    pub support: u8,
}

/// `ApplyCompletedEpoch`: one update per frozen worker of `frozen.epoch` from its sealed F05
/// output under the sealed `history_allowed` bit and eligible count, then one completed
/// summary bound to `result`. `binding` is the frozen segment binding of the epoch.
///
/// # Errors
/// Returns `F07_EPOCH_NOT_SEALED` without the epoch's seal; `F07_BINDING_MISMATCH` when the
/// observations or binding disagree with the frozen roster; `NON_CANONICAL` when the epoch is
/// already retained (F05 cannot complete it twice); propagates `CompletionDraft`'s refusals
/// (`RETENTION_FULL`, `WRONG_EPOCH`, `CONFLICT`, `F07_UNKNOWN_WORKER`, ...).
#[allow(
    dead_code,
    reason = "joint-section runtime used by epoch.rs; tests/reputation_vectors.rs includes this file via #[path]"
)]
#[allow(
    clippy::too_many_arguments,
    reason = "every input is a distinct authenticated producer value of the completion"
)]
pub(crate) fn complete_epoch(
    state: &ReputationState,
    seal: Presence<HistorySeal>,
    frozen: FrozenBinding,
    binding: SegmentBinding,
    result: Digest32,
    height: u64,
    roster: &[WorkerRosterEntry],
    observations: &[WorkerObservation],
) -> CodecResult<(ReputationState, CompletedHistory)> {
    let seal = match seal {
        Presence::Present(seal) if seal.epoch == frozen.epoch => seal,
        _ => return Err(F07_EPOCH_NOT_SEALED),
    };
    if observations.len() != roster.len() || binding.config != frozen.config {
        return Err(F07_BINDING_MISMATCH);
    }
    let mut draft = match CompletionDraft::begin(state, frozen, result, height, roster)? {
        CompletionStart::Pending(draft) => draft,
        CompletionStart::Retained(_) => return Err(NON_CANONICAL),
    };
    for (i, (entry, observation)) in roster.iter().zip(observations).enumerate() {
        let (_, key) = bound_record(state, entry.worker, binding)?;
        draft.apply_worker(
            key,
            observation.median,
            observation.support,
            seal.eligible,
            seal.allows(i),
        )?;
    }
    draft.finish()
}
