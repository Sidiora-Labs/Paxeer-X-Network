use crate::{errors::*, types::*};

pub const CURRENT_BYTES: usize = 184;
pub const HISTORY_BYTES: usize = 92;
pub const HEADER_BYTES: usize = 64;
pub const LIMIT: usize = 32;
pub const SECTION_CAP: usize = 9088;
pub const JOINT_CAP: usize = 24576;
pub const COMMON_CAP: usize = 196608;
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
    pub fn latest_completed(&self) -> Presence<u64> {
        self.completed_through
    }
    pub fn lookup_epoch(&self, epoch: u64) -> Result<CompletedHistory, HistoryLookupError> {
        self.completed()
            .find(|r| r.epoch == epoch)
            .copied()
            .ok_or(HistoryLookupError::HistoryOutsideRetention)
    }
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
    pub fn validate(&self) -> CodecResult<()> {
        if self.current_len as usize > LIMIT || self.history_len as usize > LIMIT {
            return Err(CAPACITY);
        }
        let mut worker = None;
        for (i, slot) in self.current.iter().enumerate() {
            match slot {
                Some(r) if i < self.current_len as usize => {
                    r.validate()?;
                    if worker.map_or(false, |w| w >= r.worker) {
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
                    if epoch.map_or(false, |e| e >= r.epoch) || r.execution_height < height {
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

pub fn qualified_quality(q: Score, score: Score) -> CodecResult<Score> {
    let n = u64::from(q.get())
        .checked_mul(7)
        .and_then(|v| v.checked_add(u64::from(score.get())))
        .ok_or(ARITHMETIC)?;
    Score::new(u32::try_from(n / 8).map_err(|_| ARITHMETIC)?)
}
pub fn missing_quality(q: Score) -> CodecResult<Score> {
    let n = u64::from(q.get()).checked_mul(63).ok_or(ARITHMETIC)?;
    Score::new(u32::try_from(n / 64).map_err(|_| ARITHMETIC)?)
}
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
pub fn confidence(count: u32) -> CodecResult<Score> {
    if count > 32 {
        return Err(NON_CANONICAL);
    }
    Score::new(count.checked_mul(125_000).ok_or(ARITHMETIC)?.min(UNIT))
}
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
        (n / u64::from(eligible)) as u32,
    )?))
}
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
