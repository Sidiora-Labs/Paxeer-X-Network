//! F05 schema-1 integer aggregation over already validated bounded views.
//! Unweighted lower median, fixed quorum three, absent votes excluded, checked
//! raw weight sums and display-only ppm ratios. No transfer or entitlement here.
//!
//! The runtime ([`apply`]) composes `BeginAggregation` (0x0501), `ProcessAggregation`
//! (0x0502) and `FinalizeAggregation` (0x0503) over the complete committed shared state value.
//! Readings chosen where producers are silent:
//! - The joint F05/F06 settlement section is the F06 reward state (`REWARD_STATE_BYTES`), then
//!   the F05 current record, then the F05 history. Without a record the tail is empty; once a
//!   record exists the history always follows it (an empty history is `count:u16 = 0`).
//! - The current record carries no epoch. A TERMINAL record while the opened epoch's F06 row is
//!   still RESERVED is the previous epoch's result (it must equal the last history row) and the
//!   opened epoch reads as unsealed. The next `BeginAggregation` replaces it.
//! - Begin seals the admitted F03 rows of the opened epoch, excluding an evaluator whose live
//!   grant is REVOKED, whose revocation excludes the frozen epoch or whose F08 membership is
//!   missing or revoked. Every later step reads the sealed digests only.
//! - The frozen worker attributes are rebuilt from the F06 row (worker, recipient) and the
//!   live F02 table, exactly as F03 admission does, and must hash to the frozen roster.
//! - Finalize terminalizes the F06 row in the same transition and appends one history row;
//!   history rows whose F06 epoch row is no longer retained are dropped (shared retention).
//! - The host derives `AuthorityContext::aggregate_sealed` from [`progress`].
use crate::{
    admission::{AdmissionTable, Participant},
    aggregation_codec::{
        decode_current, decode_history, encode_current, encode_history, input_digest,
        validate_current_history, AggregationInputView, AggregationPhase, CurrentAggregation,
        DecodedCurrent, DecodedHistory, EpochAggregation, HistorySummary, QualityStatus,
        ReportCommitment, WorkerAggregate, WorkerVotes, MAX_TOTAL_WEIGHT, WORKER_AGGREGATE_BYTES,
    },
    codec::{self, EventCommon, Reader, ReportBody, Roster, ValidatedEnvelope, Writer},
    dispatch,
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F05_REPORT_INVARIANT, NON_CANONICAL,
        NOT_FOUND, STALE_CURSOR, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET,
        WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        authority::evaluator_region, authority::split_identity_section, model::GrantStatus,
    },
    registry::check_f01_capacity,
    registry_ops::{CallContext, PolicySection},
    reward_math::allocate_aggregation,
    rewards::{
        decode_reward_state, EpochStatus, RewardEffect, RewardEpoch, RewardState,
        REWARD_STATE_BYTES,
    },
    state::{decode_shared_state, encode_shared_state, HeightWindow, Section, SharedState},
    types::{
        Digest32, EvaluatorId, EvaluatorRosterEntry, FrozenBinding, Presence, ResultDigest, Score,
        Version, WorkerRosterEntry,
    },
    workers::WorkerTable,
};

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
///
/// # Errors
/// Returns `CAPACITY` for more than `MAX_VOTES` scores and `ARITHMETIC` when the comparison
/// count overflows or exceeds `MAX_COMPARISONS_PER_WORKER`.
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
///
/// # Errors
/// Returns `CAPACITY` for more than `MAX_VOTES` votes, `NON_CANONICAL` for a missing vote slot
/// or an invalid output and `F05_REPORT_INVARIANT` when evaluators are not strictly ascending;
/// propagates `lower_median` refusals.
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

/// Aggregates one frozen roster worker from the structural view.
///
/// # Errors
/// Propagates `worker_votes` (`F05_REPORT_INVARIANT`, `CAPACITY`, score-cell decode) and
/// `aggregate_votes` refusals.
pub fn aggregate_worker(
    view: &AggregationInputView<'_>,
    worker: WorkerRosterEntry,
) -> CodecResult<WorkerAggregate> {
    aggregate_votes(&view.worker_votes(worker.worker, worker.generation)?)
}

/// Checked raw weight sum; exceeding the 32-worker maximum is an invariant failure.
///
/// # Errors
/// Returns `ARITHMETIC` when the sum overflows or exceeds `MAX_TOTAL_WEIGHT`.
pub fn add_weight(running: u64, output: WorkerAggregate) -> CodecResult<u64> {
    let sum = running
        .checked_add(u64::from(output.weight()))
        .ok_or(ARITHMETIC)?;
    if sum > MAX_TOTAL_WEIGHT {
        return Err(ARITHMETIC);
    }
    Ok(sum)
}

/// Checked sum of every output weight.
///
/// # Errors
/// Returns `CAPACITY` for more than `MAX_WORKERS` outputs and `ARITHMETIC` when the sum
/// overflows or exceeds `MAX_TOTAL_WEIGHT`.
pub fn total_weight(outputs: &[WorkerAggregate]) -> CodecResult<u64> {
    if outputs.len() > MAX_WORKERS {
        return Err(CAPACITY);
    }
    outputs.iter().try_fold(0, |sum, v| add_weight(sum, *v))
}

/// `floor(1_000_000 * weight / total)` for client display only. W = 0 has no
/// positive proportions and returns Absent: no equal or fallback share exists.
///
/// # Errors
/// Returns `ARITHMETIC` when `total` exceeds `MAX_TOTAL_WEIGHT` or `weight` exceeds `total` or
/// 1,000,000.
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

/// One bounded resumable step: exactly min(8, `worker_count` - cursor) frozen
/// workers in canonical order, extending the checked running sum.
#[derive(Clone, Debug)]
pub struct Chunk {
    outputs: [Option<WorkerAggregate>; WORKERS_PER_CHUNK],
    count: usize,
    cursor: u16,
    running_weight: u64,
}
impl Chunk {
    #[must_use]
    pub fn len(&self) -> usize {
        self.count
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    #[must_use]
    pub fn cursor(&self) -> u16 {
        self.cursor
    }
    #[must_use]
    pub fn running_weight(&self) -> u64 {
        self.running_weight
    }
    /// Output at `index`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when `index` is outside the recorded outputs.
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .filter(|_| index < self.count)
            .ok_or(NON_CANONICAL)
    }
}

/// Aggregates the next chunk of frozen workers starting at `cursor`.
///
/// # Errors
/// Returns `CAPACITY` for more than `MAX_WORKERS` workers, `NON_CANONICAL` for an unordered
/// roster and `STALE_CURSOR` when `cursor` is past the roster end; propagates
/// `aggregate_worker` and `add_weight` refusals.
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
    #[must_use]
    pub fn len(&self) -> usize {
        self.count
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    #[must_use]
    pub fn total_weight(&self) -> u64 {
        self.total
    }
    /// Output at `index`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when `index` is outside the recorded outputs.
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .filter(|_| index < self.count)
            .ok_or(NON_CANONICAL)
    }
    /// Display-only share of the output at `index`.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when `index` is out of range; propagates `display_share_ppm`
    /// refusals.
    pub fn display_share_ppm(&self, index: usize) -> CodecResult<Presence<u32>> {
        display_share_ppm(self.output(index)?.weight(), self.total)
    }
}

/// Aggregates every frozen worker chunk by chunk into one epoch result.
///
/// # Errors
/// Propagates `aggregate_chunk` refusals.
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

const SETTLEMENT_OPENS: u64 = 96;
const EPOCH_HEIGHTS: u64 = 128;
const REPORT_COMMITMENT_BYTES: usize = 64;
const PROCESS_PAYLOAD_BYTES: usize = 34;
const FINALIZE_PAYLOAD_BYTES: usize = 32;
/// Shared terminal retention; one more row is held so an overflow reaches the codec bound.
const HISTORY_ROWS: usize = 32;
const POLICY_CAP: usize = Section::PolicyLifecycle.payload_cap();
const SETTLEMENT_CAP: usize = Section::SettlementClaims.payload_cap();
const CONTROL_CAP: usize = Section::Control.payload_cap();
/// Caller scratch for [`apply`]: the next F01 section, the next joint F05/F06 settlement
/// section and the control encoding.
pub const SCRATCH_BYTES: usize = POLICY_CAP + SETTLEMENT_CAP + CONTROL_CAP;
/// `input32 || cursor:u16 || worker_count:u16`.
pub const BEGIN_RESPONSE_BYTES: usize = 36;
/// `input32 || cursor:u16 || worker_count:u16 || partial_weight_sum:u64`.
pub const PROCESS_RESPONSE_BYTES: usize = 44;
/// `input32 || aggregate_root32 || worker_count:u16 || total_reward_weight:u64`.
pub const FINALIZE_RESPONSE_BYTES: usize = 74;

/// The structural fields of one persisted current record, read without the frozen roster.
#[derive(Clone, Copy, Debug)]
struct Record<'a> {
    bytes: &'a [u8],
    phase: AggregationPhase,
    input: Presence<Digest32>,
    cursor: u16,
    outputs: u16,
    running_weight: u64,
    root: Presence<Digest32>,
}

/// Splits the bytes after the F06 reward state into the F05 current record and its history.
fn split_tail(tail: &[u8]) -> CodecResult<(Option<Record<'_>>, &[u8])> {
    if tail.is_empty() {
        return Ok((None, &[]));
    }
    let mut r = Reader::new(tail);
    let phase = match r.u8()? {
        0 => AggregationPhase::Unsealed,
        1 => AggregationPhase::Processing,
        2 => AggregationPhase::Terminal,
        _ => return Err(NON_CANONICAL),
    };
    r.u64()?;
    let raw: [u8; 32] = r.fixed()?;
    let input = if raw == [0; 32] {
        Presence::Absent
    } else {
        Presence::Present(Digest32::new(raw)?)
    };
    let cursor = r.u16()?;
    let reports = usize::from(r.u16()?);
    r.take(
        reports
            .checked_mul(REPORT_COMMITMENT_BYTES)
            .ok_or(ARITHMETIC)?,
    )?;
    let outputs = r.u16()?;
    r.take(
        usize::from(outputs)
            .checked_mul(WORKER_AGGREGATE_BYTES)
            .ok_or(ARITHMETIC)?,
    )?;
    let running_weight = r.u64()?;
    let root = r.presence(|r| Digest32::new(r.fixed()?))?;
    let (bytes, history) = tail.split_at_checked(r.offset()).ok_or(NON_CANONICAL)?;
    Ok((
        Some(Record {
            bytes,
            phase,
            input,
            cursor,
            outputs,
            running_weight,
            root,
        }),
        history,
    ))
}

/// Committed views of the currently opened epoch and its F05 tail.
struct View<'a> {
    state: SharedState<'a>,
    section: PolicySection<'a>,
    frozen: FrozenBinding,
    rewards: RewardState<'a>,
    row: RewardEpoch,
    reports: crate::evaluators::admission::ReportRegion<'a>,
    record: Option<Record<'a>>,
    history: &'a [u8],
}

fn view(current: &[u8]) -> CodecResult<View<'_>> {
    let state = decode_shared_state(current)?;
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
    let epoch = admission.current_epoch().ok_or(WRONG_EPOCH)?;
    let settlement = state.feature_sections[Section::SettlementClaims.index()];
    let (rewards, tail) = settlement
        .split_at_checked(REWARD_STATE_BYTES)
        .ok_or(WRONG_EPOCH)?;
    let rewards = decode_reward_state(rewards)?;
    let row = match rewards.row(epoch) {
        Err(NOT_FOUND) => return Err(WRONG_EPOCH),
        row => row?,
    };
    let header = &section.header;
    let frozen = FrozenBinding {
        chain: header.deployment_chain_domain,
        program: header.program_id,
        market: header.market_id,
        epoch,
        config: Version::new(header.active_config_version)?,
        roster: row.roster,
    };
    let reports = crate::evaluators::admission::ReportRegion::decode(
        state.feature_sections[Section::CurrentReports.index()],
    )?;
    let (record, history) = split_tail(tail)?;
    Ok(View {
        state,
        section,
        frozen,
        rewards,
        row,
        reports,
        record,
        history,
    })
}

impl<'a> View<'a> {
    fn history(&self) -> CodecResult<DecodedHistory> {
        if self.record.is_some() {
            decode_history(self.history)
        } else {
            decode_history(&[0, 0])
        }
    }
    fn last_history(&self) -> CodecResult<Option<HistorySummary>> {
        let history = self.history()?;
        match history.len().checked_sub(1) {
            Some(last) => history.entry(last).map(Some),
            None => Ok(None),
        }
    }
    /// The opened epoch's sealed record; `None` before its seal.
    fn sealed(&self) -> CodecResult<Option<Record<'a>>> {
        let Some(record) = self.record else {
            return Ok(None);
        };
        match record.phase {
            AggregationPhase::Unsealed => {
                decode_current(record.bytes, self.frozen, &[])?;
                self.history()?;
                Ok(None)
            }
            AggregationPhase::Processing => {
                self.history()?;
                Ok(Some(record))
            }
            AggregationPhase::Terminal => {
                let last = self.last_history()?.ok_or(F05_REPORT_INVARIANT)?;
                if record.input != Presence::Present(last.input)
                    || record.root != Presence::Present(last.root)
                {
                    return Err(F05_REPORT_INVARIANT);
                }
                if self.row.status == EpochStatus::Reserved {
                    if last.epoch >= self.frozen.epoch {
                        return Err(F05_REPORT_INVARIANT);
                    }
                    return Ok(None);
                }
                if last.epoch != self.frozen.epoch
                    || last.config != self.frozen.config
                    || last.roster != self.frozen.roster
                {
                    return Err(F05_REPORT_INVARIANT);
                }
                Ok(Some(record))
            }
        }
    }
    fn progress(&self, sealed: Option<Record<'_>>) -> CodecResult<Progress> {
        let worker_count = match sealed {
            Some(record) if record.phase == AggregationPhase::Terminal => record.outputs,
            _ => u16::try_from(self.row.entries().len()).map_err(|_| CAPACITY)?,
        };
        let epoch = self.frozen.epoch;
        Ok(match sealed {
            None => Progress {
                epoch,
                phase: AggregationPhase::Unsealed,
                input: Presence::Absent,
                cursor: 0,
                worker_count,
                running_weight: 0,
                root: Presence::Absent,
            },
            Some(record) => Progress {
                epoch,
                phase: record.phase,
                input: record.input,
                cursor: record.cursor,
                worker_count,
                running_weight: record.running_weight,
                root: record.root,
            },
        })
    }
}

/// Copies the leading `Some` items into a plain array; `None` when there is none.
fn pack<T: Copy, const N: usize>(items: &[Option<T>; N]) -> Option<[T; N]> {
    let first = items.first().copied().flatten()?;
    Some(items.map(|item| item.unwrap_or(first)))
}

fn packed<T, const N: usize>(items: Option<&[T; N]>, count: usize) -> CodecResult<&[T]> {
    match items {
        Some(items) => items.get(..count).ok_or(CAPACITY),
        None if count == 0 => Ok(&[]),
        None => Err(NON_CANONICAL),
    }
}

/// The frozen roster of the opened epoch: the F06 row workers with their F02 attributes,
/// sorted by worker, and the F03 snapshot evaluators.
struct FrozenRoster {
    workers: Option<[WorkerRosterEntry; MAX_WORKERS]>,
    worker_count: usize,
    evaluators: Option<[EvaluatorRosterEntry; MAX_VOTES]>,
    evaluator_count: usize,
}
impl FrozenRoster {
    fn workers(&self) -> CodecResult<&[WorkerRosterEntry]> {
        packed(self.workers.as_ref(), self.worker_count)
    }
    fn roster<'r>(&'r self, frozen: &FrozenBinding) -> CodecResult<Roster<'r>> {
        Ok(Roster {
            market: frozen.market,
            epoch: frozen.epoch,
            config: frozen.config,
            workers: self.workers()?,
            evaluators: packed(self.evaluators.as_ref(), self.evaluator_count)?,
        })
    }
}

fn frozen_roster(view: &View<'_>) -> CodecResult<FrozenRoster> {
    let identity = view.state.feature_sections[Section::IdentityRoster.index()];
    let live = WorkerTable::decode(split_identity_section(identity)?.0)?;
    let dictionary = view.rewards.dictionary();
    let mut workers = [None; MAX_WORKERS];
    let entries = view.row.entries();
    for (slot, frozen) in workers.iter_mut().zip(entries) {
        let frozen = dictionary.slot(frozen.slot)?;
        let record = live.get(frozen.worker).ok_or(WRONG_ROSTER)?;
        *slot = Some(WorkerRosterEntry {
            worker: frozen.worker,
            owner: record.owner,
            recipient: frozen.recipient,
            generation: Version::new(record.generation)?,
            key_version: Version::new(record.key_version)?,
            public_key: record.delegate,
            metadata: record.metadata,
        });
    }
    let mut workers = pack(&workers);
    let worker_count = entries.len();
    if let Some(workers) = workers.as_mut() {
        workers
            .get_mut(..worker_count)
            .ok_or(CAPACITY)?
            .sort_unstable_by_key(|w| w.worker);
    }
    let region = evaluator_region(identity)?;
    let snapshot = region
        .snapshot()
        .filter(|s| s.epoch == view.frozen.epoch)
        .ok_or(NON_CANONICAL)?;
    let mut evaluators = [None; MAX_VOTES];
    for (slot, frozen) in evaluators.iter_mut().zip(snapshot.entries()) {
        *slot = Some(frozen.entry);
    }
    Ok(FrozenRoster {
        workers,
        worker_count,
        evaluators: pack(&evaluators),
        evaluator_count: snapshot.len(),
    })
}

/// Report bodies with their sealed commitments, in ascending evaluator order.
struct Inputs<'a> {
    bodies: [Option<ReportBody<'a>>; MAX_VOTES],
    commitments: [Option<ReportCommitment>; MAX_VOTES],
    count: usize,
}
impl<'a> Inputs<'a> {
    const fn new() -> Self {
        Self {
            bodies: [None; MAX_VOTES],
            commitments: [None; MAX_VOTES],
            count: 0,
        }
    }
    fn push(&mut self, commitment: ReportCommitment, body: ReportBody<'a>) -> CodecResult<()> {
        *self.bodies.get_mut(self.count).ok_or(CAPACITY)? = Some(body);
        *self.commitments.get_mut(self.count).ok_or(CAPACITY)? = Some(commitment);
        self.count += 1;
        Ok(())
    }
    fn pack(&self) -> PackedInputs<'a> {
        PackedInputs {
            bodies: pack(&self.bodies),
            commitments: pack(&self.commitments),
            count: self.count,
        }
    }
}
struct PackedInputs<'a> {
    bodies: Option<[ReportBody<'a>; MAX_VOTES]>,
    commitments: Option<[ReportCommitment; MAX_VOTES]>,
    count: usize,
}
impl<'a> PackedInputs<'a> {
    fn bodies(&self) -> CodecResult<&[ReportBody<'a>]> {
        packed(self.bodies.as_ref(), self.count)
    }
    fn commitments(&self) -> CodecResult<&[ReportCommitment]> {
        packed(self.commitments.as_ref(), self.count)
    }
}

/// Live eligibility at the seal: the evaluator's grant is not revoked, its revocation does
/// not exclude the frozen epoch and its F08 membership is present and not revoked.
fn eligible_inputs<'a>(view: &View<'a>) -> CodecResult<PackedInputs<'a>> {
    let region = evaluator_region(view.state.feature_sections[Section::IdentityRoster.index()])?;
    let admission =
        AdmissionTable::decode(view.state.feature_sections[Section::ReputationAdmission.index()])?;
    let mut inputs = Inputs::new();
    for row in view.reports.rows(view.frozen.epoch) {
        let row = row?;
        let evaluator = row.receipt.evaluator;
        let live = region.get(evaluator).ok_or(F05_REPORT_INVARIANT)?;
        let eligible = live.grant.status != GrantStatus::Revoked
            && !region.excluded(evaluator)
            && admission
                .get(Participant::Evaluator(evaluator))
                .is_some_and(|meta| !meta.revoked());
        if eligible {
            let commitment = ReportCommitment {
                evaluator,
                report: row.receipt.report,
            };
            inputs.push(commitment, row.signed()?.body)?;
        }
    }
    Ok(inputs.pack())
}

/// The sealed rows, read back from the admitted region by their sealed digests.
fn sealed_inputs<'a>(view: &View<'a>, current: &DecodedCurrent) -> CodecResult<PackedInputs<'a>> {
    let mut inputs = Inputs::new();
    for i in 0..current.report_count() {
        let sealed = current.report(i)?;
        let admitted = view
            .reports
            .get(view.frozen.epoch, sealed.evaluator)?
            .ok_or(F05_REPORT_INVARIANT)?;
        if admitted.receipt.report != sealed.report {
            return Err(F05_REPORT_INVARIANT);
        }
        inputs.push(sealed, admitted.signed()?.body)?;
    }
    Ok(inputs.pack())
}

/// The object-local progress of the opened epoch's aggregation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Progress {
    pub epoch: u64,
    pub phase: AggregationPhase,
    pub input: Presence<Digest32>,
    pub cursor: u16,
    pub worker_count: u16,
    pub running_weight: u64,
    pub root: Presence<Digest32>,
}
impl Progress {
    fn sealed_input(&self) -> CodecResult<Digest32> {
        match self.input {
            Presence::Present(input) => Ok(input),
            Presence::Absent => Err(WRONG_PHASE),
        }
    }
    fn begin_response(&self) -> CodecResult<Response> {
        Response::write(|w| {
            w.put(self.sealed_input()?.as_bytes())?;
            w.u16(self.cursor)?;
            w.u16(self.worker_count)
        })
    }
    fn process_response(&self) -> CodecResult<Response> {
        Response::write(|w| {
            w.put(self.sealed_input()?.as_bytes())?;
            w.u16(self.cursor)?;
            w.u16(self.worker_count)?;
            w.u64(self.running_weight)
        })
    }
    fn finalize_response(&self) -> CodecResult<Response> {
        let Presence::Present(root) = self.root else {
            return Err(NON_CANONICAL);
        };
        Response::write(|w| {
            w.put(self.sealed_input()?.as_bytes())?;
            w.put(root.as_bytes())?;
            w.u16(self.worker_count)?;
            w.u64(self.running_weight)
        })
    }
}

/// The aggregation progress of the opened epoch in committed `current`. A host computes
/// `AuthorityContext::aggregate_sealed` as `phase != AggregationPhase::Unsealed`; a caller
/// refused with `STALE_CURSOR` reads the current cursor here.
///
/// # Errors
/// Returns `WRONG_EPOCH` without an opened epoch; `NON_CANONICAL` and `F05_REPORT_INVARIANT`
/// for an inconsistent committed state.
pub fn progress(current: &[u8]) -> CodecResult<Progress> {
    let view = view(current)?;
    let sealed = view.sealed()?;
    view.progress(sealed)
}

/// The compact C08 result payload of one aggregation operation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Response {
    bytes: [u8; FINALIZE_RESPONSE_BYTES],
    len: usize,
}
impl Response {
    fn write(fill: impl FnOnce(&mut Writer<'_>) -> CodecResult<()>) -> CodecResult<Self> {
        let mut bytes = [0; FINALIZE_RESPONSE_BYTES];
        let mut w = Writer::new(&mut bytes);
        fill(&mut w)?;
        let len = w.len();
        Ok(Self { bytes, len })
    }
    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        self.bytes.get(..self.len).unwrap_or(&[])
    }
    /// `H('PAXAI/result/v1', payload)`.
    ///
    /// # Errors
    /// Propagates result-digest refusals.
    pub fn result(&self) -> CodecResult<ResultDigest> {
        codec::result_digest(self.as_bytes())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    /// One semantic mutation: one revision increment composed into `next` and the operation
    /// event into `event`.
    Applied {
        progress: Progress,
        response: Response,
        revision: u64,
        result: ResultDigest,
        state_len: usize,
        event_len: usize,
    },
    /// Exact repetition of an already applied step: nothing was written.
    AlreadyApplied {
        progress: Progress,
        response: Response,
    },
}

struct Call<'c> {
    ctx: &'c CallContext,
    envelope: &'c ValidatedEnvelope<'c>,
    view: &'c View<'c>,
}
impl Call<'_> {
    /// Permissionless native relayer, domain, expiry and the exact frozen bindings.
    fn check_envelope(&self) -> CodecResult<()> {
        let e = &self.envelope.envelope;
        let frozen = &self.view.frozen;
        codec::compare_native_principal(e, self.ctx.principal)?;
        let market = codec::derive_market(self.ctx.chain, self.ctx.program)?;
        e.check_domain(self.ctx.chain, self.ctx.program, market)?;
        e.check_expiry(self.ctx.height)?;
        if frozen.market != market {
            return Err(WRONG_MARKET);
        }
        if e.epoch != frozen.epoch {
            return Err(WRONG_EPOCH);
        }
        if e.config != frozen.config.get() {
            return Err(WRONG_CONFIG);
        }
        if e.roster != Presence::Present(frozen.roster) {
            return Err(WRONG_ROSTER);
        }
        Ok(())
    }
    /// Writes `settlement` and the next F01 revision into `next`, then the operation event.
    fn commit(
        &self,
        settlement: &[u8],
        out: Out<'_>,
        progress: Progress,
        response: Response,
    ) -> CodecResult<Outcome> {
        let Out {
            policy: policy_out,
            control: control_out,
            next,
            event,
        } = out;
        let view = self.view;
        let revision = view.state.revision.checked_add(1).ok_or(ARITHMETIC)?;
        let mut section = view.section;
        section.header.state_revision = revision;
        let policy_len = section.encode(policy_out)?;
        let mut feature_sections: [&[u8]; 5] = view.state.feature_sections;
        feature_sections[Section::PolicyLifecycle.index()] =
            policy_out.get(..policy_len).ok_or(CAPACITY)?;
        feature_sections[Section::SettlementClaims.index()] = settlement;
        let candidate = SharedState {
            revision,
            feature_sections,
            control: view.state.control.clone(),
        };
        check_f01_capacity(policy_len, candidate.encoded_len()?)?;
        let state_len = encode_shared_state(&candidate, next, control_out)?;
        let result = response.result()?;
        let frozen = &view.frozen;
        let event_len = codec::encode_event_frame(
            self.envelope.envelope.operation,
            &EventCommon {
                market: frozen.market,
                epoch: frozen.epoch,
                config: frozen.config,
                revision,
                request: self.envelope.request_digest()?,
                result,
            },
            response.as_bytes(),
            event,
        )?;
        Ok(Outcome::Applied {
            progress,
            response,
            revision,
            result,
            state_len,
            event_len,
        })
    }
}

/// The caller outputs of one mutation besides the settlement section.
struct Out<'o> {
    policy: &'o mut [u8],
    control: &'o mut [u8],
    next: &'o mut [u8],
    event: &'o mut [u8],
}

/// Splits caller scratch into the settlement output and the remaining outputs.
fn split_outputs<'o>(
    scratch: &'o mut [u8],
    next: &'o mut [u8],
    event: &'o mut [u8],
) -> CodecResult<(&'o mut [u8], Out<'o>)> {
    let (policy, rest) = scratch.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let (settlement, control) = rest.split_at_mut_checked(SETTLEMENT_CAP).ok_or(CAPACITY)?;
    Ok((
        settlement,
        Out {
            policy,
            control,
            next,
            event,
        },
    ))
}

/// Writes the unchanged reward state, `record` and `history` into `out`.
fn write_settlement<'o>(
    rewards: &[u8],
    record: impl FnOnce(&mut [u8]) -> CodecResult<usize>,
    history: &[u8],
    out: &'o mut [u8],
) -> CodecResult<&'o [u8]> {
    let (reward_out, tail) = out
        .split_at_mut_checked(REWARD_STATE_BYTES)
        .ok_or(CAPACITY)?;
    reward_out.copy_from_slice(rewards);
    let record_len = record(tail)?;
    let end = record_len.checked_add(history.len()).ok_or(ARITHMETIC)?;
    tail.get_mut(record_len..end)
        .ok_or(CAPACITY)?
        .copy_from_slice(history);
    let total = REWARD_STATE_BYTES.checked_add(end).ok_or(ARITHMETIC)?;
    out.get(..total).ok_or(CAPACITY)
}

fn begin(
    call: &Call<'_>,
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let view = call.view;
    if !call.envelope.envelope.payload.is_empty() {
        return Err(NON_CANONICAL);
    }
    call.check_envelope()?;
    let origin = view.section.header.origin_height;
    let settlement_opens =
        HeightWindow::epoch(origin, view.frozen.epoch, SETTLEMENT_OPENS, EPOCH_HEIGHTS)?.start;
    if call.ctx.height < settlement_opens {
        return Err(WRONG_PHASE);
    }
    if let Some(sealed) = view.sealed()? {
        let progress = view.progress(Some(sealed))?;
        return Ok(Outcome::AlreadyApplied {
            progress,
            response: progress.begin_response()?,
        });
    }
    if view.row.status != EpochStatus::Reserved {
        return Err(WRONG_PHASE);
    }
    let roster = frozen_roster(view)?;
    let workers = roster.workers()?;
    let inputs = eligible_inputs(view)?;
    let bodies = inputs.bodies()?;
    AggregationInputView::structural(view.frozen, roster.roster(&view.frozen)?, bodies)?;
    let commitments = inputs.commitments()?;
    let seal_height = call.ctx.height;
    let input = input_digest(view.frozen, seal_height, commitments)?;
    let record = CurrentAggregation {
        phase: AggregationPhase::Processing,
        seal_height,
        input: Presence::Present(input),
        cursor: 0,
        reports: commitments,
        outputs: &[],
        running_weight: 0,
        root: Presence::Absent,
    };
    record.validate_seal_window(view.frozen, origin)?;
    let history: &[u8] = if view.record.is_some() {
        view.history
    } else {
        &[0, 0]
    };
    let (settlement_out, out) = split_outputs(scratch, next, event)?;
    let settlement = write_settlement(
        view.rewards.bytes(),
        |out| encode_current(&record, view.frozen, workers, out),
        history,
        settlement_out,
    )?;
    let progress = Progress {
        epoch: view.frozen.epoch,
        phase: AggregationPhase::Processing,
        input: Presence::Present(input),
        cursor: 0,
        worker_count: u16::try_from(workers.len()).map_err(|_| CAPACITY)?,
        running_weight: 0,
        root: Presence::Absent,
    };
    let response = progress.begin_response()?;
    call.commit(settlement, out, progress, response)
}

/// The sealed record decoded against the frozen roster, with its sealed inputs.
fn sealed_current<'a>(
    view: &View<'a>,
    sealed: &Record<'_>,
    roster: &FrozenRoster,
) -> CodecResult<(DecodedCurrent, PackedInputs<'a>)> {
    let current = decode_current(sealed.bytes, view.frozen, roster.workers()?)?;
    let inputs = sealed_inputs(view, &current)?;
    Ok((current, inputs))
}

fn outputs_of(current: &DecodedCurrent) -> CodecResult<[Option<WorkerAggregate>; MAX_WORKERS]> {
    let mut outputs = [None; MAX_WORKERS];
    for (i, slot) in outputs.iter_mut().enumerate().take(current.output_count()) {
        *slot = Some(current.output(i)?);
    }
    Ok(outputs)
}

fn process(
    call: &Call<'_>,
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let view = call.view;
    let payload = call.envelope.envelope.payload;
    if payload.len() != PROCESS_PAYLOAD_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(payload);
    r.take(32)?;
    let expected = r.u16()?;
    r.finish()?;
    call.check_envelope()?;
    let sealed = view.sealed()?.ok_or(WRONG_PHASE)?;
    check_input(payload, &sealed)?;
    let progress = view.progress(Some(sealed))?;
    if expected != sealed.cursor {
        return Err(STALE_CURSOR);
    }
    if sealed.phase == AggregationPhase::Terminal || sealed.cursor == progress.worker_count {
        return Ok(Outcome::AlreadyApplied {
            progress,
            response: progress.process_response()?,
        });
    }
    let roster = frozen_roster(view)?;
    let workers = roster.workers()?;
    let (current, inputs) = sealed_current(view, &sealed, &roster)?;
    let input_view = AggregationInputView::structural(
        view.frozen,
        roster.roster(&view.frozen)?,
        inputs.bodies()?,
    )?;
    current.validate_inputs(&input_view)?;
    let chunk = aggregate_chunk(&input_view, current.cursor(), current.running_weight())?;
    let mut outputs = outputs_of(&current)?;
    let start = current.output_count();
    for i in 0..chunk.len() {
        *outputs
            .get_mut(start.checked_add(i).ok_or(ARITHMETIC)?)
            .ok_or(CAPACITY)? = Some(chunk.output(i)?);
    }
    let outputs = pack(&outputs);
    let record = CurrentAggregation {
        phase: AggregationPhase::Processing,
        seal_height: current.seal_height(),
        input: current.input(),
        cursor: chunk.cursor(),
        reports: inputs.commitments()?,
        outputs: packed(outputs.as_ref(), usize::from(chunk.cursor()))?,
        running_weight: chunk.running_weight(),
        root: Presence::Absent,
    };
    let (settlement_out, out) = split_outputs(scratch, next, event)?;
    let settlement = write_settlement(
        view.rewards.bytes(),
        |out| encode_current(&record, view.frozen, workers, out),
        view.history,
        settlement_out,
    )?;
    let progress = Progress {
        cursor: chunk.cursor(),
        running_weight: chunk.running_weight(),
        ..progress
    };
    let response = progress.process_response()?;
    call.commit(settlement, out, progress, response)
}

/// History after this epoch's terminal row: rows whose F06 epoch is still retained, then the
/// new row.
fn next_history(
    view: &View<'_>,
    retained: &RewardState<'_>,
    terminal: HistorySummary,
) -> CodecResult<([Option<HistorySummary>; HISTORY_ROWS + 1], usize)> {
    let history = view.history()?;
    let mut rows = [None; HISTORY_ROWS + 1];
    let mut count = 0;
    for i in 0..history.len() {
        let row = history.entry(i)?;
        match retained.row(row.epoch) {
            Ok(_) => {
                *rows.get_mut(count).ok_or(CAPACITY)? = Some(row);
                count += 1;
            }
            Err(NOT_FOUND) => {}
            Err(error) => return Err(error),
        }
    }
    *rows.get_mut(count).ok_or(CAPACITY)? = Some(terminal);
    Ok((rows, count + 1))
}

fn finalize(
    call: &Call<'_>,
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let view = call.view;
    let payload = call.envelope.envelope.payload;
    if payload.len() != FINALIZE_PAYLOAD_BYTES {
        return Err(NON_CANONICAL);
    }
    call.check_envelope()?;
    let sealed = view.sealed()?.ok_or(WRONG_PHASE)?;
    check_input(payload, &sealed)?;
    let progress = view.progress(Some(sealed))?;
    if sealed.phase == AggregationPhase::Terminal {
        return Ok(Outcome::AlreadyApplied {
            progress,
            response: progress.finalize_response()?,
        });
    }
    if sealed.cursor != progress.worker_count {
        return Err(WRONG_PHASE);
    }
    let roster = frozen_roster(view)?;
    let workers = roster.workers()?;
    let (current, inputs) = sealed_current(view, &sealed, &roster)?;
    let input_view = AggregationInputView::structural(
        view.frozen,
        roster.roster(&view.frozen)?,
        inputs.bodies()?,
    )?;
    current.validate_inputs(&input_view)?;
    let outputs = pack(&outputs_of(&current)?);
    let outputs = packed(outputs.as_ref(), current.output_count())?;
    let Presence::Present(input) = current.input() else {
        return Err(NON_CANONICAL);
    };
    let aggregation = EpochAggregation::structural(view.frozen, input, workers, outputs)?;
    let root = aggregation.root();
    let allocation = allocate_aggregation(view.row.budget, &aggregation)?;
    let record = CurrentAggregation {
        phase: AggregationPhase::Terminal,
        seal_height: current.seal_height(),
        input: current.input(),
        cursor: current.cursor(),
        reports: inputs.commitments()?,
        outputs,
        running_weight: aggregation.total_weight(),
        root: Presence::Present(root),
    };
    let terminal = HistorySummary {
        epoch: view.frozen.epoch,
        config: view.frozen.config,
        roster: view.frozen.roster,
        input,
        root,
    };
    let (settlement_out, out) = split_outputs(scratch, next, event)?;
    let (reward_out, tail) = settlement_out
        .split_at_mut_checked(REWARD_STATE_BYTES)
        .ok_or(CAPACITY)?;
    let (retained, effect) = view.rewards.terminalize(
        &view.frozen,
        root,
        &allocation,
        workers,
        call.ctx.height,
        reward_out,
    )?;
    if effect != RewardEffect::NoTransfer {
        return Err(NON_CANONICAL);
    }
    let (history, count) = next_history(view, &retained, terminal)?;
    let history = pack(&history);
    let history = packed(history.as_ref(), count)?;
    validate_current_history(&record, view.frozen, workers, history)?;
    let record_len = encode_current(&record, view.frozen, workers, tail)?;
    let history_len = encode_history(history, tail.get_mut(record_len..).ok_or(CAPACITY)?)?;
    let total = REWARD_STATE_BYTES
        .checked_add(record_len)
        .and_then(|n| n.checked_add(history_len))
        .ok_or(ARITHMETIC)?;
    let settlement = settlement_out.get(..total).ok_or(CAPACITY)?;
    let progress = Progress {
        phase: AggregationPhase::Terminal,
        running_weight: aggregation.total_weight(),
        root: Presence::Present(root),
        ..progress
    };
    let response = progress.finalize_response()?;
    call.commit(settlement, out, progress, response)
}

/// Applies one `BeginAggregation` (0x0501), `ProcessAggregation` (0x0502) or
/// `FinalizeAggregation` (0x0503) request to the committed `current` state, writing the whole
/// next state into `next` (at least `MAX_STATE_BYTES`) and its event into `event`; `scratch`
/// holds at least [`SCRATCH_BYTES`]. Dispatch arm: `dispatch::BeginAggregation |
/// dispatch::ProcessAggregation | dispatch::FinalizeAggregation => aggregation::apply(&ctx,
/// &envelope, current, next, scratch, event)`. Begin seals the eligible admitted rows at
/// `height >= T+96` (no upper bound: delayed settlement stays valid); Process aggregates exactly
/// `min(8, worker_count - cursor)` frozen workers; Finalize computes the aggregate root,
/// terminalizes the F06 epoch and appends the history row in the same transition. No step
/// reads evidence or performs any network access.
///
/// # Errors
/// `UNKNOWN_OPERATION`; `NON_CANONICAL` for a malformed payload or an inconsistent committed
/// state; envelope principal, domain and expiry refusals; `WRONG_MARKET`; `WRONG_EPOCH` (also
/// with no opened epoch), `WRONG_CONFIG` and `WRONG_ROSTER` (also when the rebuilt frozen
/// roster no longer hashes to the frozen digest); `WRONG_PHASE` before `T+96`, for Process or
/// Finalize before the seal, for Finalize before the complete cursor or for Begin on an epoch
/// whose F06 row is no longer reserved; `CONFLICT` for another sealed input digest;
/// `STALE_CURSOR` for any other expected cursor (read the current one with [`progress`]);
/// `F05_REPORT_INVARIANT` for persisted rows violating a checked invariant; the F06
/// terminalization refusals (`F06_EPOCH_TERMINAL`, `F06_AGGREGATION_MISMATCH`,
/// `RETENTION_FULL`, ...); `CAPACITY`, `ARITHMETIC` and `F01_CAPACITY_UNAVAILABLE`. On any
/// error `current` is unchanged and the outputs must be discarded.
pub fn apply(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let operation = envelope.envelope.operation;
    if operation != dispatch::BeginAggregation
        && operation != dispatch::ProcessAggregation
        && operation != dispatch::FinalizeAggregation
    {
        return Err(UNKNOWN_OPERATION);
    }
    let view = view(current)?;
    let call = Call {
        ctx,
        envelope,
        view: &view,
    };
    if operation == dispatch::BeginAggregation {
        begin(&call, next, scratch, event)
    } else if operation == dispatch::ProcessAggregation {
        process(&call, next, scratch, event)
    } else {
        finalize(&call, next, scratch, event)
    }
}

/// The sealed input digest named by the payload must equal the persisted one.
fn check_input(payload: &[u8], sealed: &Record<'_>) -> CodecResult<()> {
    let named = Digest32::new(
        payload
            .get(..32)
            .and_then(|b| <[u8; 32]>::try_from(b).ok())
            .ok_or(NON_CANONICAL)?,
    )?;
    if sealed.input != Presence::Present(named) {
        return Err(CONFLICT);
    }
    Ok(())
}
