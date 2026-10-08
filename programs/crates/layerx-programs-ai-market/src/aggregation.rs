//! F05 schema-1 integer aggregation over already validated bounded views.
//! Unweighted lower median, fixed quorum three, absent votes excluded, checked
//! raw weight sums and display-only ppm ratios. No transfer or entitlement here.
use crate::{aggregation_codec::*, errors::*, types::*};

pub const QUORUM: usize = 3;
pub const MAX_VOTES: usize = 8;
pub const MAX_WORKERS: usize = 32;
pub const WORKERS_PER_CHUNK: usize = 8;
/// Insertion sort over at most eight scores: n(n-1)/2 <= 28 comparisons.
pub const MAX_COMPARISONS_PER_WORKER: u32 = 28;
pub const DISPLAY_SCALE_PPM: u64 = 1_000_000;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Median {
    pub support: u8,
    pub selected: Presence<Score>,
    pub comparisons: u32,
}

/// Sorts a bounded copy and selects a[floor((n-1)/2)] when n >= quorum.
/// Fewer than three votes yield absent quality, never a measured zero.
pub fn lower_median(scores: &[Score]) -> CodecResult<Median> {
    if scores.len() > MAX_VOTES {
        return Err(CAPACITY);
    }
    let n = scores.len();
    let mut sorted = [0u32; MAX_VOTES];
    for (slot, score) in sorted.iter_mut().zip(scores) {
        *slot = score.get();
    }
    let mut comparisons = 0u32;
    for i in 1..n {
        let mut j = i;
        while j > 0 {
            comparisons = comparisons.checked_add(1).ok_or(ARITHMETIC)?;
            if sorted[j - 1] <= sorted[j] {
                break;
            }
            sorted.swap(j - 1, j);
            j -= 1;
        }
    }
    if comparisons > MAX_COMPARISONS_PER_WORKER {
        return Err(ARITHMETIC);
    }
    let support = u8::try_from(n).map_err(|_| CAPACITY)?;
    let selected = if n < QUORUM {
        Presence::Absent
    } else {
        Presence::Present(Score::new(sorted[(n - 1) / 2])?)
    };
    Ok(Median {
        support,
        selected,
        comparisons,
    })
}

/// Votes must be the canonical projection in ascending distinct evaluator identity.
pub fn aggregate_votes(votes: &WorkerVotes) -> CodecResult<WorkerAggregate> {
    if votes.len() > MAX_VOTES {
        return Err(CAPACITY);
    }
    let mut scores = [Score::new(0)?; MAX_VOTES];
    let mut last: Option<EvaluatorId> = None;
    for (i, slot) in scores.iter_mut().enumerate().take(votes.len()) {
        let vote = votes.vote(i)?;
        if last.is_some_and(|p| p >= vote.evaluator) {
            return Err(F05_REPORT_INVARIANT);
        }
        last = Some(vote.evaluator);
        *slot = vote.score;
    }
    let median = lower_median(&scores[..votes.len()])?;
    let (status, score) = match median.selected {
        Presence::Absent => (QualityStatus::InsufficientQuorum, 0),
        Presence::Present(s) if s.get() == 0 => (QualityStatus::ScoredZero, 0),
        Presence::Present(s) => (QualityStatus::ScoredPositive, s.get()),
    };
    WorkerAggregate::new(
        votes.worker(),
        votes.generation(),
        median.support,
        status,
        score,
        score,
    )
}

pub fn aggregate_worker(
    view: &AggregationInputView<'_>,
    worker: WorkerRosterEntry,
) -> CodecResult<WorkerAggregate> {
    aggregate_votes(&view.worker_votes(worker.worker, worker.generation)?)
}

/// Checked raw weight sum; exceeding the 32-worker maximum is an invariant failure.
pub fn add_weight(running: u64, output: WorkerAggregate) -> CodecResult<u64> {
    let sum = running
        .checked_add(u64::from(output.weight()))
        .ok_or(ARITHMETIC)?;
    if sum > MAX_TOTAL_WEIGHT {
        return Err(ARITHMETIC);
    }
    Ok(sum)
}

pub fn total_weight(outputs: &[WorkerAggregate]) -> CodecResult<u64> {
    if outputs.len() > MAX_WORKERS {
        return Err(CAPACITY);
    }
    outputs.iter().try_fold(0, |sum, v| add_weight(sum, *v))
}

/// floor(1_000_000 * weight / total) for client display only. W = 0 has no
/// positive proportions and returns Absent: no equal or fallback share exists.
pub fn display_share_ppm(weight: u32, total: u64) -> CodecResult<Presence<u32>> {
    if total > MAX_TOTAL_WEIGHT || u64::from(weight) > total || weight > 1_000_000 {
        return Err(ARITHMETIC);
    }
    if total == 0 {
        return Ok(Presence::Absent);
    }
    let numerator = DISPLAY_SCALE_PPM
        .checked_mul(u64::from(weight))
        .ok_or(ARITHMETIC)?;
    let share = numerator.checked_div(total).ok_or(ARITHMETIC)?;
    Ok(Presence::Present(
        u32::try_from(share).map_err(|_| ARITHMETIC)?,
    ))
}

/// One bounded resumable step: exactly min(8, worker_count - cursor) frozen
/// workers in canonical order, extending the checked running sum.
#[derive(Clone, Debug)]
pub struct Chunk {
    outputs: [Option<WorkerAggregate>; WORKERS_PER_CHUNK],
    count: usize,
    cursor: u16,
    running_weight: u64,
}
impl Chunk {
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn cursor(&self) -> u16 {
        self.cursor
    }
    pub fn running_weight(&self) -> u64 {
        self.running_weight
    }
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .filter(|_| index < self.count)
            .ok_or(NON_CANONICAL)
    }
}

pub fn aggregate_chunk(
    view: &AggregationInputView<'_>,
    cursor: u16,
    running_weight: u64,
) -> CodecResult<Chunk> {
    let workers = view.workers();
    if workers.len() > MAX_WORKERS {
        return Err(CAPACITY);
    }
    if workers.windows(2).any(|p| p[0].worker >= p[1].worker) {
        return Err(NON_CANONICAL);
    }
    let start = usize::from(cursor);
    if start > workers.len() {
        return Err(STALE_CURSOR);
    }
    let end = workers.len().min(start + WORKERS_PER_CHUNK);
    let mut outputs = [None; WORKERS_PER_CHUNK];
    let mut sum = running_weight;
    for (slot, worker) in outputs.iter_mut().zip(&workers[start..end]) {
        let output = aggregate_worker(view, *worker)?;
        sum = add_weight(sum, output)?;
        *slot = Some(output);
    }
    Ok(Chunk {
        outputs,
        count: end - start,
        cursor: u16::try_from(end).map_err(|_| CAPACITY)?,
        running_weight: sum,
    })
}

/// Complete per-epoch result: every frozen worker receives exactly one record.
#[derive(Clone, Debug)]
pub struct EpochWeights {
    outputs: [Option<WorkerAggregate>; MAX_WORKERS],
    count: usize,
    total: u64,
}
impl EpochWeights {
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn total_weight(&self) -> u64 {
        self.total
    }
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .filter(|_| index < self.count)
            .ok_or(NON_CANONICAL)
    }
    pub fn display_share_ppm(&self, index: usize) -> CodecResult<Presence<u32>> {
        display_share_ppm(self.output(index)?.weight(), self.total)
    }
}

pub fn aggregate_epoch(view: &AggregationInputView<'_>) -> CodecResult<EpochWeights> {
    let mut outputs = [None; MAX_WORKERS];
    let mut cursor = 0u16;
    let mut total = 0u64;
    loop {
        let chunk = aggregate_chunk(view, cursor, total)?;
        if chunk.is_empty() {
            break;
        }
        for i in 0..chunk.len() {
            outputs[usize::from(cursor) + i] = Some(chunk.output(i)?);
        }
        cursor = chunk.cursor();
        total = chunk.running_weight();
    }
    Ok(EpochWeights {
        outputs,
        count: usize::from(cursor),
        total,
    })
}
