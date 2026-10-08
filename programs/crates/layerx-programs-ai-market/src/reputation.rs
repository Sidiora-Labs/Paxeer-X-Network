use crate::{
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F07_BINDING_MISMATCH, F07_GENERATION_MISMATCH,
        F07_RESOURCE_LIMIT, F07_SEGMENT_MISMATCH, F07_UNKNOWN_WORKER, NON_CANONICAL, NOT_FOUND,
        RETENTION_FULL, WRONG_EPOCH,
    },
    types::{Digest32, MarketId, Presence, PrincipalId, Score, Version, WorkerId},
};

pub const CURRENT_BYTES: usize = 184;
pub const HISTORY_BYTES: usize = 92;
pub const HEADER_BYTES: usize = 64;
pub const LIMIT: usize = 32;
pub const SECTION_CAP: usize = 9088;
pub const JOINT_CAP: usize = 24576;
pub const COMMON_CAP: usize = 196_608;
pub const UNIT: u32 = 1_000_000;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SegmentKey {
    pub market: MarketId,
    pub worker: WorkerId,
    pub config: Version,
    pub policy: Digest32,
    pub model: Digest32,
    pub reset_generation: Version,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum HistoryStatus {
    Active = 1,
    Suspended = 2,
    Retired = 3,
}
impl HistoryStatus {
    /// Decodes a history status byte.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the byte is not 1, 2 or 3.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Active),
            2 => Ok(Self::Suspended),
            3 => Ok(Self::Retired),
            _ => Err(NON_CANONICAL),
        }
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum ClosureReason {
    OwnerReset = 1,
    ModelChanged = 2,
    PolicyChanged = 3,
    AdminIntegrity = 4,
}
impl ClosureReason {
    /// Decodes a closure reason byte.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the byte is not 1 through 4.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::OwnerReset),
            2 => Ok(Self::ModelChanged),
            3 => Ok(Self::PolicyChanged),
            4 => Ok(Self::AdminIntegrity),
            _ => Err(NON_CANONICAL),
        }
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Observation {
    pub epoch: u64,
    pub height: u64,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReputationCurrent {
    pub worker: WorkerId,
    pub segment: Digest32,
    pub owner: PrincipalId,
    pub reset_generation: Version,
    pub quality: Score,
    pub qualifying_count: u32,
    pub coverage: Presence<Score>,
    pub last_applied: Presence<u64>,
    pub last_observed: Presence<Observation>,
    pub last_transition_height: u64,
    pub previous_segment: Presence<Digest32>,
    pub status: HistoryStatus,
}
impl ReputationCurrent {
    /// Opens a worker's first reputation segment at `height`.
    ///
    /// # Errors
    /// Returns `F07_GENERATION_MISMATCH` when the key's reset generation is not 1; propagates `segment_digest`'s `NON_CANONICAL` refusal.
    pub fn bootstrap(key: SegmentKey, owner: PrincipalId, height: u64) -> CodecResult<Self> {
        if key.reset_generation.get() != 1 {
            return Err(F07_GENERATION_MISMATCH);
        }
        Ok(Self {
            worker: key.worker,
            segment: crate::reputation_codec::segment_digest(key)?,
            owner,
            reset_generation: key.reset_generation,
            quality: Score::new(0)?,
            qualifying_count: 0,
            coverage: Presence::Absent,
            last_applied: Presence::Absent,
            last_observed: Presence::Absent,
            last_transition_height: height,
            previous_segment: Presence::Absent,
            status: HistoryStatus::Active,
        })
    }
    /// Checks the record's internal invariants.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the qualifying count exceeds 32, the reset generation disagrees with the previous-segment presence, or the observation, applied-epoch, coverage and transition-height fields are inconsistent.
    pub fn validate(&self) -> CodecResult<()> {
        if self.qualifying_count > 32 {
            return Err(NON_CANONICAL);
        }
        if (self.reset_generation.get() == 1) != (self.previous_segment == Presence::Absent) {
            return Err(NON_CANONICAL);
        }
        match self.last_observed {
            Presence::Absent => {
                if self.qualifying_count != 0 || self.quality.get() != 0 {
                    return Err(NON_CANONICAL);
                }
            }
            Presence::Present(o) => {
                if o.height == 0
                    || self.qualifying_count == 0
                    || o.height > self.last_transition_height
                {
                    return Err(NON_CANONICAL);
                }
                match self.last_applied {
                    Presence::Present(e) if o.epoch <= e => (),
                    _ => return Err(NON_CANONICAL),
                }
            }
        }
        if self.coverage != Presence::Absent && self.last_applied == Presence::Absent {
            return Err(NON_CANONICAL);
        }
        if self.last_applied != Presence::Absent && self.last_transition_height == 0 {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
    /// Checks that the record is valid and bound to `key`.
    ///
    /// # Errors
    /// Propagates `validate`'s and `segment_digest`'s `NON_CANONICAL` refusals; returns `F07_SEGMENT_MISMATCH` when the worker, reset generation or segment digest differ from `key`.
    pub fn matches_segment(&self, key: SegmentKey) -> CodecResult<()> {
        self.validate()?;
        if self.worker != key.worker
            || self.reset_generation != key.reset_generation
            || self.segment != crate::reputation_codec::segment_digest(key)?
        {
            return Err(F07_SEGMENT_MISMATCH);
        }
        Ok(())
    }
    /// Confidence score for the record's qualifying count.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the qualifying count exceeds 32.
    pub fn confidence(&self) -> CodecResult<Score> {
        confidence(self.qualifying_count)
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CompletedHistory {
    pub epoch: u64,
    pub execution_height: u64,
    pub config: Version,
    pub result: Digest32,
    pub root: Digest32,
    pub observed_workers: u8,
    pub total_workers: u8,
    pub covered_workers: u8,
}
impl CompletedHistory {
    /// Checks the history row's invariants.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the execution height is zero, the total exceeds 32, or the observed or covered count exceeds the total.
    pub fn validate(&self) -> CodecResult<()> {
        if self.execution_height == 0
            || self.total_workers > 32
            || self.observed_workers > self.total_workers
            || self.covered_workers > self.total_workers
        {
            return Err(NON_CANONICAL);
        }
        Ok(())
    }
}
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReputationState {
    pub market: MarketId,
    pub(crate) current: [Option<ReputationCurrent>; LIMIT],
    pub(crate) history: [Option<CompletedHistory>; LIMIT],
    pub(crate) current_len: u8,
    pub(crate) history_len: u8,
    pub(crate) completed_through: Presence<u64>,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CompletionIdentity {
    New,
    Retained(CompletedHistory),
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HistoryLookupError {
    HistoryOutsideRetention,
}
impl ReputationState {
    #[must_use]
    pub const fn new(market: MarketId) -> Self {
        Self {
            market,
            current: [None; LIMIT],
            history: [None; LIMIT],
            current_len: 0,
            history_len: 0,
            completed_through: Presence::Absent,
        }
    }
    pub fn records(&self) -> impl Iterator<Item = &ReputationCurrent> {
        self.current.iter().filter_map(Option::as_ref)
    }
    pub fn completed(&self) -> impl Iterator<Item = &CompletedHistory> {
        self.history.iter().filter_map(Option::as_ref)
    }
    #[must_use]
    pub fn latest_completed(&self) -> Presence<u64> {
        self.completed_through
    }
    /// Retained completion row for `epoch`.
    ///
    /// # Errors
    /// Returns `HistoryLookupError::HistoryOutsideRetention` when no retained row has that epoch.
    pub fn lookup_epoch(&self, epoch: u64) -> Result<CompletedHistory, HistoryLookupError> {
        self.completed()
            .find(|r| r.epoch == epoch)
            .copied()
            .ok_or(HistoryLookupError::HistoryOutsideRetention)
    }
    /// Current record of `worker`, bound to `key`.
    ///
    /// # Errors
    /// Returns `F07_BINDING_MISMATCH` when `key` names another market or worker; `F07_UNKNOWN_WORKER` when the worker has no record; propagates `ReputationCurrent::matches_segment`'s `NON_CANONICAL`/`F07_SEGMENT_MISMATCH` refusals.
    pub fn worker(&self, worker: WorkerId, key: SegmentKey) -> CodecResult<&ReputationCurrent> {
        if key.market != self.market || key.worker != worker {
            return Err(F07_BINDING_MISMATCH);
        }
        let r = self
            .records()
            .find(|r| r.worker == worker)
            .ok_or(F07_UNKNOWN_WORKER)?;
        r.matches_segment(key)?;
        Ok(r)
    }
    /// Checks the section's slot, ordering and completion invariants.
    ///
    /// # Errors
    /// Returns `CAPACITY` when a length exceeds `LIMIT`; `NON_CANONICAL` when a record is invalid, slots and lengths disagree, workers or epochs are not strictly ascending, heights decrease, or `completed_through` disagrees with the rows.
    pub fn validate(&self) -> CodecResult<()> {
        if self.current_len as usize > LIMIT || self.history_len as usize > LIMIT {
            return Err(CAPACITY);
        }
        let mut worker = None;
        for (i, slot) in self.current.iter().enumerate() {
            match slot {
                Some(r) if i < self.current_len as usize => {
                    r.validate()?;
                    if worker.is_some_and(|w| w >= r.worker) {
                        return Err(NON_CANONICAL);
                    }
                    worker = Some(r.worker);
                    if let Presence::Present(e) = r.last_applied {
                        match self.completed_through {
                            Presence::Present(last) if e <= last => (),
                            _ => return Err(NON_CANONICAL),
                        }
                    }
                }
                None if i >= self.current_len as usize => (),
                _ => return Err(NON_CANONICAL),
            }
        }
        let mut epoch = None;
        let mut height = 0;
        for (i, slot) in self.history.iter().enumerate() {
            match slot {
                Some(r) if i < self.history_len as usize => {
                    r.validate()?;
                    if epoch.is_some_and(|e| e >= r.epoch) || r.execution_height < height {
                        return Err(NON_CANONICAL);
                    }
                    epoch = Some(r.epoch);
                    height = r.execution_height;
                }
                None if i >= self.history_len as usize => (),
                _ => return Err(NON_CANONICAL),
            }
        }
        if let Some(e) = epoch {
            if self.completed_through != Presence::Present(e) {
                return Err(NON_CANONICAL);
            }
        }
        Ok(())
    }
    /// Classifies a completion of `epoch` with `result` as new or already retained.
    ///
    /// # Errors
    /// Propagates `validate`'s `CAPACITY`/`NON_CANONICAL` refusals; returns `CONFLICT` when the retained row for `epoch` has another result; `WRONG_EPOCH` when `epoch` is not retained and not after the latest completed epoch.
    pub fn assess_completion(
        &self,
        epoch: u64,
        result: Digest32,
    ) -> CodecResult<CompletionIdentity> {
        self.validate()?;
        if let Ok(r) = self.lookup_epoch(epoch) {
            if r.result != result {
                return Err(CONFLICT);
            }
            return Ok(CompletionIdentity::Retained(r));
        }
        if let Presence::Present(last) = self.completed_through {
            if epoch <= last {
                return Err(WRONG_EPOCH);
            }
        }
        Ok(CompletionIdentity::New)
    }
    /// Encoded section size in bytes.
    ///
    /// # Errors
    /// Propagates `validate`'s `CAPACITY`/`NON_CANONICAL` refusals; returns `F07_RESOURCE_LIMIT` when the size exceeds `SECTION_CAP`.
    pub fn encoded_len(&self) -> CodecResult<usize> {
        self.validate()?;
        let n = HEADER_BYTES
            + usize::from(self.current_len) * CURRENT_BYTES
            + usize::from(self.history_len) * HISTORY_BYTES;
        if n > SECTION_CAP {
            return Err(F07_RESOURCE_LIMIT);
        }
        Ok(n)
    }
    #[allow(
        dead_code,
        reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
    )]
    pub(crate) fn insert(&mut self, record: ReputationCurrent) -> CodecResult<()> {
        self.validate()?;
        record.validate()?;
        if let Presence::Present(epoch) = record.last_applied {
            match self.completed_through {
                Presence::Present(last) if epoch <= last => (),
                _ => return Err(WRONG_EPOCH),
            }
        }
        if self.current_len as usize == LIMIT {
            return Err(CAPACITY);
        }
        if self.records().any(|r| r.worker == record.worker) {
            return Err(CONFLICT);
        }
        let n = self.current_len as usize;
        let index = self
            .records()
            .position(|r| r.worker > record.worker)
            .unwrap_or(n);
        for i in (index..n).rev() {
            self.current[i + 1] = self.current[i];
        }
        self.current[index] = Some(record);
        self.current_len += 1;
        Ok(())
    }
    #[allow(
        dead_code,
        reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
    )]
    pub(crate) fn append(&mut self, record: CompletedHistory) -> CodecResult<()> {
        record.validate()?;
        if self.assess_completion(record.epoch, record.result)? != CompletionIdentity::New {
            return Err(CONFLICT);
        }
        if self.history_len as usize == LIMIT {
            return Err(RETENTION_FULL);
        }
        if let Some(old) = self.completed().last() {
            if record.execution_height < old.execution_height {
                return Err(NON_CANONICAL);
            }
        }
        self.history[self.history_len as usize] = Some(record);
        self.history_len += 1;
        self.completed_through = Presence::Present(record.epoch);
        Ok(())
    }
    // Invoked only alongside F06's actual safe oldest-row prune.
    #[allow(
        dead_code,
        reason = "crate-internal F07 primitive: its F05/F06 bridge caller is not in tree; tests/reputation_vectors.rs drives it via #[path]"
    )]
    pub(crate) fn prune_oldest(&mut self, epoch: u64) -> CodecResult<()> {
        self.validate()?;
        let first = self.history[0].ok_or(NOT_FOUND)?;
        if first.epoch != epoch {
            return Err(WRONG_EPOCH);
        }
        let n = self.history_len as usize;
        for i in 1..n {
            self.history[i - 1] = self.history[i];
        }
        self.history[n - 1] = None;
        self.history_len -= 1;
        Ok(())
    }
}

/// Quality after a qualifying observation: floor((7q + score) / 8).
///
/// # Errors
/// Returns `ARITHMETIC` when the weighted sum overflows; propagates `Score::new`'s `F03_SCORE_RANGE` refusal.
pub fn qualified_quality(q: Score, score: Score) -> CodecResult<Score> {
    let n = u64::from(q.get())
        .checked_mul(7)
        .and_then(|v| v.checked_add(u64::from(score.get())))
        .ok_or(ARITHMETIC)?;
    Score::new(u32::try_from(n / 8).map_err(|_| ARITHMETIC)?)
}
/// Quality after a missing observation: floor(63q / 64).
///
/// # Errors
/// Returns `ARITHMETIC` when the product overflows; propagates `Score::new`'s `F03_SCORE_RANGE` refusal.
pub fn missing_quality(q: Score) -> CodecResult<Score> {
    let n = u64::from(q.get()).checked_mul(63).ok_or(ARITHMETIC)?;
    Score::new(u32::try_from(n / 64).map_err(|_| ARITHMETIC)?)
}
/// Qualifying count after one epoch, capped at 32.
///
/// # Errors
/// Returns `NON_CANONICAL` when `count` exceeds 32; `ARITHMETIC` when the increment overflows.
pub fn next_count(count: u32, qualified: bool) -> CodecResult<u32> {
    if count > 32 {
        return Err(NON_CANONICAL);
    }
    if qualified {
        Ok(count.checked_add(1).ok_or(ARITHMETIC)?.min(32))
    } else {
        Ok(count)
    }
}
/// Confidence score for a qualifying count, capped at `UNIT`.
///
/// # Errors
/// Returns `NON_CANONICAL` when `count` exceeds 32; `ARITHMETIC` when the product overflows.
pub fn confidence(count: u32) -> CodecResult<Score> {
    if count > 32 {
        return Err(NON_CANONICAL);
    }
    Score::new(count.checked_mul(125_000).ok_or(ARITHMETIC)?.min(UNIT))
}
/// Evidence coverage as the supporting share of eligible evaluators.
///
/// # Errors
/// Returns `F07_BINDING_MISMATCH` when `eligible` exceeds 8 or `support` exceeds `eligible`; `ARITHMETIC` when the product overflows; propagates `Score::new`'s `F03_SCORE_RANGE` refusal.
pub fn evidence_coverage(eligible: u8, support: u8) -> CodecResult<Presence<Score>> {
    if eligible > 8 || support > eligible {
        return Err(F07_BINDING_MISMATCH);
    }
    if eligible == 0 {
        return Ok(Presence::Absent);
    }
    let n = u64::from(UNIT)
        .checked_mul(u64::from(support))
        .ok_or(ARITHMETIC)?;
    Ok(Presence::Present(Score::new(
        u32::try_from(n / u64::from(eligible)).map_err(|_| ARITHMETIC)?,
    )?))
}
/// Checks the F07 section against its own, joint and common storage caps.
///
/// # Errors
/// Returns `ARITHMETIC` when a running sum overflows; `F07_RESOURCE_LIMIT` when the section, joint or common size exceeds its cap.
pub fn check_storage_budget(
    section: usize,
    f08: usize,
    other_common_and_framing: usize,
) -> CodecResult<()> {
    let joint = section.checked_add(f08).ok_or(ARITHMETIC)?;
    let common = joint
        .checked_add(other_common_and_framing)
        .ok_or(ARITHMETIC)?;
    if section > SECTION_CAP || joint > JOINT_CAP || common > COMMON_CAP {
        return Err(F07_RESOURCE_LIMIT);
    }
    Ok(())
}
