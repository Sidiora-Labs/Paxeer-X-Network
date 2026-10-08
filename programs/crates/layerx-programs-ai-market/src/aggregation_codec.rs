//! F05 schema-1 structural codecs. These values confer no admission authority,
//! signature authentication, finality, or economic terminalization. The runtime
//! must obtain the closed authenticated F03/F04 set and preseal revocations from
//! authoritative state before using these commitments. No request flag supplies
//! that provenance. ReportBody binds generations through the frozen roster and
//! authenticated evidence admission; it has no invented per-cell generation.
use crate::{
    codec::{
        decode_report, domain_hash, report_digest, roster_digest, Reader, ReportBody, Roster,
        Writer,
    },
    errors::*,
    types::*,
    SCHEMA_VERSION,
};

pub const WORKER_AGGREGATE_BYTES: usize = 50;
pub const CURRENT_MAX_BYTES: usize = 2200;
pub const HISTORY_MAX_BYTES: usize = 3586;
pub const F05_MAX_BYTES: usize = 5786;
pub const INPUT_PREIMAGE_MAX_BYTES: usize = 668;
pub const OUTPUT_PREIMAGE_MAX_BYTES: usize = 1788;
pub const MAX_TOTAL_WEIGHT: u64 = 32_000_000;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum QualityStatus {
    InsufficientQuorum = 0,
    ScoredZero = 1,
    ScoredPositive = 2,
}
impl QualityStatus {
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            0 => Ok(Self::InsufficientQuorum),
            1 => Ok(Self::ScoredZero),
            2 => Ok(Self::ScoredPositive),
            _ => Err(NON_CANONICAL),
        }
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WorkerAggregate {
    worker: WorkerId,
    generation: Version,
    support: u8,
    status: QualityStatus,
    score: Score,
    weight: u32,
}
impl WorkerAggregate {
    /// Checks the output representation only; T02 supplies the selected score.
    pub fn new(
        worker: WorkerId,
        generation: Version,
        support: u8,
        status: QualityStatus,
        score: u32,
        weight: u32,
    ) -> CodecResult<Self> {
        let score = Score::new(score)?;
        if support > 8 || weight > 1_000_000 {
            return Err(NON_CANONICAL);
        }
        let valid = match status {
            QualityStatus::InsufficientQuorum => support < 3 && score.get() == 0 && weight == 0,
            QualityStatus::ScoredZero => support >= 3 && score.get() == 0 && weight == 0,
            QualityStatus::ScoredPositive => {
                support >= 3 && score.get() > 0 && weight == score.get()
            }
        };
        if !valid {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            worker,
            generation,
            support,
            status,
            score,
            weight,
        })
    }
    pub const fn worker(self) -> WorkerId {
        self.worker
    }
    pub const fn generation(self) -> Version {
        self.generation
    }
    pub const fn support(self) -> u8 {
        self.support
    }
    pub const fn status(self) -> QualityStatus {
        self.status
    }
    pub const fn score(self) -> Score {
        self.score
    }
    pub const fn weight(self) -> u32 {
        self.weight
    }
    pub fn quality(self) -> Presence<Score> {
        if self.status == QualityStatus::InsufficientQuorum {
            Presence::Absent
        } else {
            Presence::Present(self.score)
        }
    }
}
fn write_worker(w: &mut Writer<'_>, v: WorkerAggregate) -> CodecResult<()> {
    w.put(v.worker.as_bytes())?;
    w.u64(v.generation.get())?;
    w.u8(v.support)?;
    w.u8(v.status as u8)?;
    w.u32(v.score.get())?;
    w.u32(v.weight)
}
fn read_worker(r: &mut Reader<'_>) -> CodecResult<WorkerAggregate> {
    WorkerAggregate::new(
        WorkerId::new(r.fixed()?)?,
        Version::new(r.u64()?)?,
        r.u8()?,
        QualityStatus::decode(r.u8()?)?,
        r.u32()?,
        r.u32()?,
    )
}
fn capacity(out: &[u8], n: usize, max: usize) -> CodecResult<()> {
    if n > max || out.len() < n {
        Err(CAPACITY)
    } else {
        Ok(())
    }
}
pub fn encode_worker_aggregate(v: WorkerAggregate, out: &mut [u8]) -> CodecResult<usize> {
    capacity(out, 50, 50)?;
    let mut w = Writer::new(out);
    write_worker(&mut w, v)?;
    Ok(w.len())
}
pub fn decode_worker_aggregate(bytes: &[u8]) -> CodecResult<WorkerAggregate> {
    let mut r = Reader::new(bytes);
    let v = read_worker(&mut r)?;
    r.finish()?;
    Ok(v)
}
fn write_binding(w: &mut Writer<'_>, b: FrozenBinding) -> CodecResult<()> {
    w.u16(SCHEMA_VERSION)?;
    w.put(b.chain.as_bytes())?;
    w.put(b.program.as_bytes())?;
    w.put(b.market.as_bytes())?;
    w.u64(b.epoch)?;
    w.u64(b.config.get())?;
    w.put(b.roster.as_bytes())
}
fn read_binding(r: &mut Reader<'_>) -> CodecResult<FrozenBinding> {
    if r.u16()? != SCHEMA_VERSION {
        return Err(BAD_VERSION);
    }
    Ok(FrozenBinding {
        chain: ChainDomain::new(r.fixed()?)?,
        program: ProgramId::new(r.fixed()?)?,
        market: MarketId::new(r.fixed()?)?,
        epoch: r.u64()?,
        config: Version::new(r.u64()?)?,
        roster: RosterDigest::new(r.fixed()?)?,
    })
}
pub fn check_binding(actual: FrozenBinding, expected: FrozenBinding) -> CodecResult<()> {
    if actual.chain != expected.chain {
        Err(WRONG_DOMAIN)
    } else if actual.program != expected.program {
        Err(WRONG_PROGRAM)
    } else if actual.market != expected.market {
        Err(WRONG_MARKET)
    } else if actual.epoch != expected.epoch {
        Err(WRONG_EPOCH)
    } else if actual.config != expected.config {
        Err(WRONG_CONFIG)
    } else if actual.roster != expected.roster {
        Err(WRONG_ROSTER)
    } else {
        Ok(())
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReportCommitment {
    pub evaluator: EvaluatorId,
    pub report: ReportDigest,
}
fn check_reports(rows: &[ReportCommitment]) -> CodecResult<()> {
    if rows.len() > 8 {
        return Err(CAPACITY);
    }
    if rows.windows(2).any(|p| p[0].evaluator >= p[1].evaluator) {
        return Err(NON_CANONICAL);
    }
    Ok(())
}
/// Arrival order may be normalized here, never by a persisted-state decoder.
/// Duplicate stable identities refuse even when their operational keys differ.
pub fn canonicalize_arrivals(rows: &mut [ReportCommitment]) -> CodecResult<()> {
    if rows.len() > 8 {
        return Err(CAPACITY);
    }
    for i in 0..rows.len() {
        if rows[i + 1..]
            .iter()
            .any(|r| r.evaluator == rows[i].evaluator)
        {
            return Err(NON_CANONICAL);
        }
    }
    rows.sort_unstable_by_key(|r| r.evaluator);
    Ok(())
}
pub fn encode_input_preimage(
    binding: FrozenBinding,
    seal_height: u64,
    rows: &[ReportCommitment],
    out: &mut [u8],
) -> CodecResult<usize> {
    check_reports(rows)?;
    capacity(out, 156 + rows.len() * 64, INPUT_PREIMAGE_MAX_BYTES)?;
    let mut w = Writer::new(out);
    write_binding(&mut w, binding)?;
    w.u64(seal_height)?;
    w.u16(rows.len() as u16)?;
    for row in rows {
        w.put(row.evaluator.as_bytes())?;
        w.put(row.report.as_bytes())?;
    }
    Ok(w.len())
}
pub fn input_digest(
    binding: FrozenBinding,
    seal_height: u64,
    rows: &[ReportCommitment],
) -> CodecResult<Digest32> {
    let mut bytes = [0; INPUT_PREIMAGE_MAX_BYTES];
    let n = encode_input_preimage(binding, seal_height, rows, &mut bytes)?;
    domain_hash("PAXAI/aggregation-input/v1", &bytes[..n])
}

/// Structurally validated actual producer reports. This is deliberately NOT an
/// authenticated accepted/revealed set. Runtime must separately prove signatures,
/// commitment opening, evidence/generation admission, deadlines and revocation.
#[derive(Clone, Copy, Debug)]
pub struct AggregationInputView<'a> {
    binding: FrozenBinding,
    roster: Roster<'a>,
    reports: &'a [ReportBody<'a>],
}
impl<'a> AggregationInputView<'a> {
    pub fn structural(
        binding: FrozenBinding,
        roster: Roster<'a>,
        reports: &'a [ReportBody<'a>],
    ) -> CodecResult<Self> {
        roster.validate()?;
        if roster.market != binding.market {
            return Err(WRONG_MARKET);
        }
        if roster.epoch != binding.epoch {
            return Err(WRONG_EPOCH);
        }
        if roster.config != binding.config {
            return Err(WRONG_CONFIG);
        }
        if roster_digest(&roster)? != binding.roster {
            return Err(WRONG_ROSTER);
        }
        if reports.len() > 8 {
            return Err(CAPACITY);
        }
        let mut last = None;
        for report in reports {
            check_binding(report.binding.frozen, binding)?;
            report.scores.validate().map_err(|_| F05_REPORT_INVARIANT)?;
            let id = report.binding.evaluator;
            if last.is_some_and(|p| p >= id) {
                return Err(F05_REPORT_INVARIANT);
            }
            last = Some(id);
            let evaluator = roster
                .evaluators
                .iter()
                .find(|e| e.evaluator == id)
                .ok_or(F05_REPORT_INVARIANT)?;
            if evaluator.grant != report.binding.grant
                || evaluator.key_version != report.binding.key_version
            {
                return Err(F05_REPORT_INVARIANT);
            }
            for cell in report.scores.entries() {
                let cell = cell.map_err(|_| F05_REPORT_INVARIANT)?;
                let worker = roster
                    .workers
                    .iter()
                    .find(|w| w.worker == cell.worker)
                    .ok_or(F05_REPORT_INVARIANT)?;
                if worker.owner == evaluator.owner || worker.worker.bytes() == id.bytes() {
                    return Err(F05_REPORT_INVARIANT);
                }
            }
        }
        Ok(Self {
            binding,
            roster,
            reports,
        })
    }
    pub const fn binding(&self) -> FrozenBinding {
        self.binding
    }
    pub fn workers(&self) -> &'a [WorkerRosterEntry] {
        self.roster.workers
    }
    pub fn reports(&self) -> &'a [ReportBody<'a>] {
        self.reports
    }
    pub fn commitments(&self, out: &mut [ReportCommitment]) -> CodecResult<usize> {
        if out.len() < self.reports.len() {
            return Err(CAPACITY);
        }
        for (slot, report) in out.iter_mut().zip(self.reports) {
            *slot = ReportCommitment {
                evaluator: report.binding.evaluator,
                report: report_digest(report)?,
            };
        }
        Ok(self.reports.len())
    }
    /// Persisted bytes must be decoded using the real common report codec.
    pub fn persisted_report(bytes: &'a [u8]) -> CodecResult<ReportBody<'a>> {
        decode_report(bytes).map_err(|_| F05_REPORT_INVARIANT)
    }
    pub fn support(&self, worker: WorkerId) -> CodecResult<u8> {
        if !self.roster.workers.iter().any(|w| w.worker == worker) {
            return Err(F05_REPORT_INVARIANT);
        }
        let mut count = 0u8;
        for report in self.reports {
            for cell in report.scores.entries() {
                if cell?.worker == worker {
                    count = count.checked_add(1).ok_or(ARITHMETIC)?;
                }
            }
        }
        Ok(count)
    }
}
fn check_outputs(
    workers: &[WorkerRosterEntry],
    outputs: &[WorkerAggregate],
    complete: bool,
) -> CodecResult<u64> {
    if workers.len() > 32 || outputs.len() > 32 {
        return Err(CAPACITY);
    }
    if outputs.len() > workers.len() || (complete && outputs.len() != workers.len()) {
        return Err(NON_CANONICAL);
    }
    if workers.windows(2).any(|p| p[0].worker >= p[1].worker) {
        return Err(NON_CANONICAL);
    }
    let mut sum = 0u64;
    for (worker, output) in workers.iter().zip(outputs) {
        if worker.worker != output.worker || worker.generation != output.generation {
            return Err(F05_REPORT_INVARIANT);
        }
        sum = sum
            .checked_add(u64::from(output.weight))
            .ok_or(ARITHMETIC)?;
    }
    if sum > MAX_TOTAL_WEIGHT {
        return Err(ARITHMETIC);
    }
    Ok(sum)
}
/// Immutable canonical output commitment; numerical correctness is supplied by
/// T02 and terminal provenance by the atomic T03/F06 transition, not this codec.
#[derive(Clone, Copy, Debug)]
pub struct EpochAggregation<'a> {
    binding: FrozenBinding,
    input: Digest32,
    outputs: &'a [WorkerAggregate],
    total: u64,
    root: Digest32,
}
impl<'a> EpochAggregation<'a> {
    pub fn structural(
        binding: FrozenBinding,
        input: Digest32,
        workers: &[WorkerRosterEntry],
        outputs: &'a [WorkerAggregate],
    ) -> CodecResult<Self> {
        let total = check_outputs(workers, outputs, true)?;
        let mut bytes = [0; OUTPUT_PREIMAGE_MAX_BYTES];
        let n = write_output_preimage(binding, input, outputs, total, &mut bytes)?;
        let root = domain_hash("PAXAI/aggregation-output/v1", &bytes[..n])?;
        Ok(Self {
            binding,
            input,
            outputs,
            total,
            root,
        })
    }
    pub const fn root(&self) -> Digest32 {
        self.root
    }
    pub const fn total_weight(&self) -> u64 {
        self.total
    }
    pub fn outputs(&self) -> &'a [WorkerAggregate] {
        self.outputs
    }
    pub fn encode_preimage(&self, out: &mut [u8]) -> CodecResult<usize> {
        write_output_preimage(self.binding, self.input, self.outputs, self.total, out)
    }
}
fn write_output_preimage(
    binding: FrozenBinding,
    input: Digest32,
    outputs: &[WorkerAggregate],
    total: u64,
    out: &mut [u8],
) -> CodecResult<usize> {
    capacity(out, 188 + outputs.len() * 50, OUTPUT_PREIMAGE_MAX_BYTES)?;
    let mut w = Writer::new(out);
    write_binding(&mut w, binding)?;
    w.put(input.as_bytes())?;
    w.u16(outputs.len() as u16)?;
    for output in outputs {
        write_worker(&mut w, *output)?;
    }
    w.u64(total)?;
    Ok(w.len())
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum AggregationPhase {
    Unsealed = 0,
    Processing = 1,
    Terminal = 2,
}
#[derive(Clone, Copy, Debug)]
pub struct CurrentAggregation<'a> {
    pub phase: AggregationPhase,
    pub seal_height: u64,
    pub input: Presence<Digest32>,
    pub cursor: u16,
    pub reports: &'a [ReportCommitment],
    pub outputs: &'a [WorkerAggregate],
    pub running_weight: u64,
    pub root: Presence<Digest32>,
}
impl CurrentAggregation<'_> {
    /// Structural persisted-state validation, never a terminal authority token.
    pub fn validate(
        &self,
        binding: FrozenBinding,
        workers: &[WorkerRosterEntry],
    ) -> CodecResult<()> {
        check_reports(self.reports)?;
        let sum = check_outputs(workers, self.outputs, false)?;
        if usize::from(self.cursor) != self.outputs.len() || sum != self.running_weight {
            return Err(NON_CANONICAL);
        }
        if self
            .outputs
            .iter()
            .any(|v| usize::from(v.support) > self.reports.len())
        {
            return Err(F05_REPORT_INVARIANT);
        }
        match self.phase {
            AggregationPhase::Unsealed => {
                if self.seal_height != 0
                    || self.input != Presence::Absent
                    || self.cursor != 0
                    || !self.reports.is_empty()
                    || !self.outputs.is_empty()
                    || self.running_weight != 0
                    || self.root != Presence::Absent
                {
                    return Err(NON_CANONICAL);
                }
            }
            AggregationPhase::Processing | AggregationPhase::Terminal => {
                if self.input
                    != Presence::Present(input_digest(binding, self.seal_height, self.reports)?)
                {
                    return Err(CONFLICT);
                }
                match self.phase {
                    AggregationPhase::Processing if self.root != Presence::Absent => {
                        return Err(NON_CANONICAL)
                    }
                    AggregationPhase::Terminal => {
                        let input = match self.input {
                            Presence::Present(d) => d,
                            _ => return Err(NON_CANONICAL),
                        };
                        let epoch =
                            EpochAggregation::structural(binding, input, workers, self.outputs)?;
                        if self.root != Presence::Present(epoch.root()) {
                            return Err(CONFLICT);
                        }
                    }
                    _ => {}
                }
            }
        }
        Ok(())
    }
    /// Checks frozen sealed digests and exact support, without live revocation
    /// re-evaluation. The caller must authenticate the producer source separately.
    pub fn validate_inputs(&self, view: &AggregationInputView<'_>) -> CodecResult<()> {
        self.validate(view.binding, view.workers())?;
        if self.phase == AggregationPhase::Unsealed {
            return Ok(());
        }
        if self.reports.len() != view.reports.len() {
            return Err(F05_REPORT_INVARIANT);
        }
        for (row, report) in self.reports.iter().zip(view.reports) {
            if row.evaluator != report.binding.evaluator || row.report != report_digest(report)? {
                return Err(F05_REPORT_INVARIANT);
            }
        }
        for output in self.outputs {
            if output.support != view.support(output.worker)? {
                return Err(F05_REPORT_INVARIANT);
            }
        }
        Ok(())
    }
}
pub fn encode_current(
    v: &CurrentAggregation<'_>,
    binding: FrozenBinding,
    workers: &[WorkerRosterEntry],
    out: &mut [u8],
) -> CodecResult<usize> {
    v.validate(binding, workers)?;
    let n = 56
        + v.reports.len() * 64
        + v.outputs.len() * 50
        + if matches!(v.root, Presence::Present(_)) {
            32
        } else {
            0
        };
    capacity(out, n, CURRENT_MAX_BYTES)?;
    let mut w = Writer::new(out);
    w.u8(v.phase as u8)?;
    w.u64(v.seal_height)?;
    match v.input {
        Presence::Absent => w.put(&[0; 32])?,
        Presence::Present(d) => w.put(d.as_bytes())?,
    }
    w.u16(v.cursor)?;
    w.u16(v.reports.len() as u16)?;
    for row in v.reports {
        w.put(row.evaluator.as_bytes())?;
        w.put(row.report.as_bytes())?;
    }
    w.u16(v.outputs.len() as u16)?;
    for output in v.outputs {
        write_worker(&mut w, *output)?;
    }
    w.u64(v.running_weight)?;
    w.presence(&v.root, |w, d| w.put(d.as_bytes()))?;
    Ok(w.len())
}
/// Owns bounded decoded storage; borrowed views cannot outlive this record.
#[derive(Clone, Debug)]
pub struct DecodedCurrent {
    phase: AggregationPhase,
    seal_height: u64,
    input: Presence<Digest32>,
    cursor: u16,
    reports: [Option<ReportCommitment>; 8],
    report_count: usize,
    outputs: [Option<WorkerAggregate>; 32],
    output_count: usize,
    running_weight: u64,
    root: Presence<Digest32>,
}
impl DecodedCurrent {
    pub fn phase(&self) -> AggregationPhase {
        self.phase
    }
    pub fn cursor(&self) -> u16 {
        self.cursor
    }
    pub fn report(&self, index: usize) -> CodecResult<ReportCommitment> {
        self.reports
            .get(index)
            .copied()
            .flatten()
            .ok_or(NON_CANONICAL)
    }
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .ok_or(NON_CANONICAL)
    }
    pub fn encode(
        &self,
        binding: FrozenBinding,
        workers: &[WorkerRosterEntry],
        out: &mut [u8],
    ) -> CodecResult<usize> {
        if self.report_count == 0 && self.output_count == 0 {
            return encode_current(
                &CurrentAggregation {
                    phase: self.phase,
                    seal_height: self.seal_height,
                    input: self.input,
                    cursor: self.cursor,
                    reports: &[],
                    outputs: &[],
                    running_weight: self.running_weight,
                    root: self.root,
                },
                binding,
                workers,
                out,
            );
        }
        encode_decoded(self, binding, workers, out)
    }
}
fn encode_decoded(
    v: &DecodedCurrent,
    binding: FrozenBinding,
    workers: &[WorkerRosterEntry],
    out: &mut [u8],
) -> CodecResult<usize> {
    // Reconstruct wire using already checked immutable decoded fields; validation
    // is repeated by decode_current against the supplied frozen binding below.
    let n = 56
        + v.report_count * 64
        + v.output_count * 50
        + if matches!(v.root, Presence::Present(_)) {
            32
        } else {
            0
        };
    let mut scratch = [0; CURRENT_MAX_BYTES];
    let mut w = Writer::new(&mut scratch);
    w.u8(v.phase as u8)?;
    w.u64(v.seal_height)?;
    match v.input {
        Presence::Absent => w.put(&[0; 32])?,
        Presence::Present(d) => w.put(d.as_bytes())?,
    }
    w.u16(v.cursor)?;
    w.u16(v.report_count as u16)?;
    for i in 0..v.report_count {
        let row = v.report(i)?;
        w.put(row.evaluator.as_bytes())?;
        w.put(row.report.as_bytes())?;
    }
    w.u16(v.output_count as u16)?;
    for i in 0..v.output_count {
        write_worker(&mut w, v.output(i)?)?;
    }
    w.u64(v.running_weight)?;
    w.presence(&v.root, |w, d| w.put(d.as_bytes()))?;
    decode_current(&scratch[..n], binding, workers)?;
    capacity(out, n, CURRENT_MAX_BYTES)?;
    out[..n].copy_from_slice(&scratch[..n]);
    Ok(n)
}
pub fn decode_current(
    bytes: &[u8],
    binding: FrozenBinding,
    workers: &[WorkerRosterEntry],
) -> CodecResult<DecodedCurrent> {
    if bytes.len() > CURRENT_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(bytes);
    let phase = match r.u8()? {
        0 => AggregationPhase::Unsealed,
        1 => AggregationPhase::Processing,
        2 => AggregationPhase::Terminal,
        _ => return Err(NON_CANONICAL),
    };
    let seal_height = r.u64()?;
    let raw = r.fixed()?;
    let input = if raw == [0; 32] {
        Presence::Absent
    } else {
        Presence::Present(Digest32::new(raw)?)
    };
    let cursor = r.u16()?;
    let report_count = usize::from(r.u16()?);
    if report_count > 8 {
        return Err(CAPACITY);
    }
    let mut reports = [None; 8];
    let mut last = None;
    for slot in &mut reports[..report_count] {
        let row = ReportCommitment {
            evaluator: EvaluatorId::new(r.fixed()?)?,
            report: ReportDigest::new(r.fixed()?)?,
        };
        if last.is_some_and(|p| p >= row.evaluator) {
            return Err(NON_CANONICAL);
        }
        last = Some(row.evaluator);
        *slot = Some(row);
    }
    let output_count = usize::from(r.u16()?);
    if output_count > 32 || workers.len() > 32 {
        return Err(CAPACITY);
    }
    if output_count != usize::from(cursor) || output_count > workers.len() {
        return Err(NON_CANONICAL);
    }
    let mut outputs = [None; 32];
    let mut sum = 0u64;
    if workers.windows(2).any(|p| p[0].worker >= p[1].worker) {
        return Err(NON_CANONICAL);
    }
    for (i, slot) in outputs[..output_count].iter_mut().enumerate() {
        let output = read_worker(&mut r)?;
        if output.worker != workers[i].worker
            || output.generation != workers[i].generation
            || usize::from(output.support) > report_count
        {
            return Err(F05_REPORT_INVARIANT);
        }
        sum = sum
            .checked_add(u64::from(output.weight))
            .ok_or(ARITHMETIC)?;
        *slot = Some(output);
    }
    let running_weight = r.u64()?;
    let root = r.presence(|r| Digest32::new(r.fixed()?))?;
    r.finish()?;
    if sum != running_weight || sum > MAX_TOTAL_WEIGHT {
        return Err(NON_CANONICAL);
    }
    if phase == AggregationPhase::Unsealed {
        if seal_height != 0
            || input != Presence::Absent
            || cursor != 0
            || report_count != 0
            || output_count != 0
            || running_weight != 0
            || root != Presence::Absent
        {
            return Err(NON_CANONICAL);
        }
    } else {
        let mut preimage = [0; INPUT_PREIMAGE_MAX_BYTES];
        let mut w = Writer::new(&mut preimage);
        write_binding(&mut w, binding)?;
        w.u64(seal_height)?;
        w.u16(report_count as u16)?;
        for row in reports[..report_count].iter().flatten() {
            w.put(row.evaluator.as_bytes())?;
            w.put(row.report.as_bytes())?;
        }
        let n = w.len();
        if input != Presence::Present(domain_hash("PAXAI/aggregation-input/v1", &preimage[..n])?) {
            return Err(CONFLICT);
        }
        if phase == AggregationPhase::Processing && root != Presence::Absent {
            return Err(NON_CANONICAL);
        }
        if phase == AggregationPhase::Terminal {
            if output_count != workers.len() {
                return Err(NON_CANONICAL);
            }
            let digest = match input {
                Presence::Present(d) => d,
                _ => return Err(NON_CANONICAL),
            };
            let mut preimage = [0; OUTPUT_PREIMAGE_MAX_BYTES];
            let mut w = Writer::new(&mut preimage);
            write_binding(&mut w, binding)?;
            w.put(digest.as_bytes())?;
            w.u16(output_count as u16)?;
            for output in outputs[..output_count].iter().flatten() {
                write_worker(&mut w, *output)?;
            }
            w.u64(running_weight)?;
            let n = w.len();
            if root
                != Presence::Present(domain_hash("PAXAI/aggregation-output/v1", &preimage[..n])?)
            {
                return Err(CONFLICT);
            }
        }
    }
    Ok(DecodedCurrent {
        phase,
        seal_height,
        input,
        cursor,
        reports,
        report_count,
        outputs,
        output_count,
        running_weight,
        root,
    })
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct HistorySummary {
    pub epoch: u64,
    pub config: Version,
    pub roster: RosterDigest,
    pub input: Digest32,
    pub root: Digest32,
}
fn check_history(history: &[HistorySummary]) -> CodecResult<()> {
    if history.len() > 32 {
        return Err(CAPACITY);
    }
    if history.windows(2).any(|p| p[0].epoch >= p[1].epoch) {
        return Err(NON_CANONICAL);
    }
    Ok(())
}
pub fn encode_history(history: &[HistorySummary], out: &mut [u8]) -> CodecResult<usize> {
    check_history(history)?;
    capacity(out, 2 + history.len() * 112, HISTORY_MAX_BYTES)?;
    let mut w = Writer::new(out);
    w.u16(history.len() as u16)?;
    for row in history {
        w.u64(row.epoch)?;
        w.u64(row.config.get())?;
        w.put(row.roster.as_bytes())?;
        w.put(row.input.as_bytes())?;
        w.put(row.root.as_bytes())?;
    }
    Ok(w.len())
}
#[derive(Clone, Debug)]
pub struct DecodedHistory {
    entries: [Option<HistorySummary>; 32],
    count: usize,
}
impl DecodedHistory {
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn entry(&self, index: usize) -> CodecResult<HistorySummary> {
        self.entries
            .get(index)
            .copied()
            .flatten()
            .ok_or(NON_CANONICAL)
    }
}
pub fn decode_history(bytes: &[u8]) -> CodecResult<DecodedHistory> {
    if bytes.len() > HISTORY_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(bytes);
    let count = usize::from(r.u16()?);
    if count > 32 {
        return Err(CAPACITY);
    }
    let mut entries = [None; 32];
    let mut last = None;
    for slot in &mut entries[..count] {
        let row = HistorySummary {
            epoch: r.u64()?,
            config: Version::new(r.u64()?)?,
            roster: RosterDigest::new(r.fixed()?)?,
            input: Digest32::new(r.fixed()?)?,
            root: Digest32::new(r.fixed()?)?,
        };
        if last.is_some_and(|p| p >= row.epoch) {
            return Err(NON_CANONICAL);
        }
        last = Some(row.epoch);
        *slot = Some(row);
    }
    r.finish()?;
    Ok(DecodedHistory { entries, count })
}

/// Strict decoding of the exact output commitment preimage, not a new terminal
/// wire ABI. An externally supplied commitment is checked alongside all bindings.
#[derive(Clone, Debug)]
pub struct DecodedEpochAggregation {
    binding: FrozenBinding,
    input: Digest32,
    outputs: [Option<WorkerAggregate>; 32],
    count: usize,
    total: u64,
    root: Digest32,
}
impl DecodedEpochAggregation {
    pub fn binding(&self) -> FrozenBinding {
        self.binding
    }
    pub fn input(&self) -> Digest32 {
        self.input
    }
    pub fn root(&self) -> Digest32 {
        self.root
    }
    pub fn total_weight(&self) -> u64 {
        self.total
    }
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn output(&self, index: usize) -> CodecResult<WorkerAggregate> {
        self.outputs
            .get(index)
            .copied()
            .flatten()
            .ok_or(NON_CANONICAL)
    }
}
pub fn decode_epoch_preimage(
    bytes: &[u8],
    expected: FrozenBinding,
    workers: &[WorkerRosterEntry],
    expected_root: Digest32,
) -> CodecResult<DecodedEpochAggregation> {
    if bytes.len() > OUTPUT_PREIMAGE_MAX_BYTES || workers.len() > 32 {
        return Err(CAPACITY);
    }
    if workers.windows(2).any(|p| p[0].worker >= p[1].worker) {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(bytes);
    let binding = read_binding(&mut r)?;
    check_binding(binding, expected)?;
    let input = Digest32::new(r.fixed()?)?;
    let count = usize::from(r.u16()?);
    if count > 32 {
        return Err(CAPACITY);
    }
    if count != workers.len() {
        return Err(NON_CANONICAL);
    }
    let mut outputs = [None; 32];
    let mut sum = 0u64;
    for (i, slot) in outputs[..count].iter_mut().enumerate() {
        let output = read_worker(&mut r)?;
        if output.worker != workers[i].worker || output.generation != workers[i].generation {
            return Err(F05_REPORT_INVARIANT);
        }
        sum = sum
            .checked_add(u64::from(output.weight))
            .ok_or(ARITHMETIC)?;
        *slot = Some(output);
    }
    let total = r.u64()?;
    r.finish()?;
    if total != sum || sum > MAX_TOTAL_WEIGHT {
        return Err(NON_CANONICAL);
    }
    let root = domain_hash("PAXAI/aggregation-output/v1", bytes)?;
    if root != expected_root {
        return Err(CONFLICT);
    }
    Ok(DecodedEpochAggregation {
        binding,
        input,
        outputs,
        count,
        total,
        root,
    })
}
impl CurrentAggregation<'_> {
    /// Caller supplies frozen origin, never wall-clock time. Runtime additionally
    /// checks authenticated execution height at the actual input-seal transition.
    pub fn validate_seal_window(&self, binding: FrozenBinding, origin: u64) -> CodecResult<()> {
        if self.phase != AggregationPhase::Unsealed
            && self.seal_height < EpochWindows::new(origin, binding.epoch)?.settlement
        {
            return Err(WRONG_PHASE);
        }
        Ok(())
    }
}
/// Cross-record structural consistency. Retention high-watermark, safe pruning,
/// terminal height and economic atomicity remain common/F06-owned state.
pub fn validate_current_history(
    current: &CurrentAggregation<'_>,
    binding: FrozenBinding,
    workers: &[WorkerRosterEntry],
    history: &[HistorySummary],
) -> CodecResult<()> {
    current.validate(binding, workers)?;
    check_history(history)?;
    if history.iter().any(|r| r.epoch > binding.epoch) {
        return Err(WRONG_EPOCH);
    }
    let matching = history.iter().find(|r| r.epoch == binding.epoch);
    if current.phase == AggregationPhase::Terminal {
        let row = matching.ok_or(F05_REPORT_INVARIANT)?;
        if row.config != binding.config
            || row.roster != binding.roster
            || current.input != Presence::Present(row.input)
            || current.root != Presence::Present(row.root)
        {
            return Err(F05_REPORT_INVARIANT);
        }
    } else if matching.is_some() {
        return Err(F05_REPORT_INVARIANT);
    }
    Ok(())
}

#[derive(Clone, Debug)]
pub struct DecodedInputCommitment {
    binding: FrozenBinding,
    seal_height: u64,
    rows: [Option<ReportCommitment>; 8],
    count: usize,
    digest: Digest32,
}
impl DecodedInputCommitment {
    pub fn binding(&self) -> FrozenBinding {
        self.binding
    }
    pub fn seal_height(&self) -> u64 {
        self.seal_height
    }
    pub fn digest(&self) -> Digest32 {
        self.digest
    }
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn row(&self, index: usize) -> CodecResult<ReportCommitment> {
        self.rows.get(index).copied().flatten().ok_or(NON_CANONICAL)
    }
}
pub fn decode_input_preimage(
    bytes: &[u8],
    expected: FrozenBinding,
    expected_digest: Digest32,
) -> CodecResult<DecodedInputCommitment> {
    if bytes.len() > INPUT_PREIMAGE_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(bytes);
    let binding = read_binding(&mut r)?;
    check_binding(binding, expected)?;
    let seal_height = r.u64()?;
    let count = usize::from(r.u16()?);
    if count > 8 {
        return Err(CAPACITY);
    }
    let mut rows = [None; 8];
    let mut last = None;
    for slot in &mut rows[..count] {
        let row = ReportCommitment {
            evaluator: EvaluatorId::new(r.fixed()?)?,
            report: ReportDigest::new(r.fixed()?)?,
        };
        if last.is_some_and(|p| p >= row.evaluator) {
            return Err(NON_CANONICAL);
        }
        last = Some(row.evaluator);
        *slot = Some(row);
    }
    r.finish()?;
    let digest = domain_hash("PAXAI/aggregation-input/v1", bytes)?;
    if digest != expected_digest {
        return Err(CONFLICT);
    }
    Ok(DecodedInputCommitment {
        binding,
        seal_height,
        rows,
        count,
        digest,
    })
}
impl DecodedCurrent {
    pub fn seal_height(&self) -> u64 {
        self.seal_height
    }
    pub fn input(&self) -> Presence<Digest32> {
        self.input
    }
    pub fn root(&self) -> Presence<Digest32> {
        self.root
    }
    pub fn running_weight(&self) -> u64 {
        self.running_weight
    }
    pub fn report_count(&self) -> usize {
        self.report_count
    }
    pub fn output_count(&self) -> usize {
        self.output_count
    }
    /// Use after decoding with the same frozen binding. Does not authenticate the
    /// structural producer view or turn persisted phase2 into economic authority.
    pub fn validate_inputs(&self, view: &AggregationInputView<'_>) -> CodecResult<()> {
        if self.phase == AggregationPhase::Unsealed {
            return Ok(());
        }
        if self.report_count != view.reports.len() || self.output_count > view.workers().len() {
            return Err(F05_REPORT_INVARIANT);
        }
        let mut preimage = [0; INPUT_PREIMAGE_MAX_BYTES];
        let mut w = Writer::new(&mut preimage);
        write_binding(&mut w, view.binding)?;
        w.u64(self.seal_height)?;
        w.u16(self.report_count as u16)?;
        for (i, report) in view.reports.iter().enumerate() {
            let row = self.report(i)?;
            if row.evaluator != report.binding.evaluator || row.report != report_digest(report)? {
                return Err(F05_REPORT_INVARIANT);
            }
            w.put(row.evaluator.as_bytes())?;
            w.put(row.report.as_bytes())?;
        }
        let n = w.len();
        if self.input
            != Presence::Present(domain_hash("PAXAI/aggregation-input/v1", &preimage[..n])?)
        {
            return Err(CONFLICT);
        }
        for i in 0..self.output_count {
            let output = self.output(i)?;
            let worker = view.workers()[i];
            if output.worker != worker.worker
                || output.generation != worker.generation
                || output.support != view.support(worker.worker)?
            {
                return Err(F05_REPORT_INVARIANT);
            }
        }
        Ok(())
    }
}

/// Canonical per-worker input projection in ascending stable evaluator identity.
/// Absence creates no vote; an explicit Score(0) remains a real entry.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EvaluatorVote {
    pub evaluator: EvaluatorId,
    pub score: Score,
}
#[derive(Clone, Debug)]
pub struct WorkerVotes {
    worker: WorkerId,
    generation: Version,
    entries: [Option<EvaluatorVote>; 8],
    count: usize,
}
impl WorkerVotes {
    pub fn worker(&self) -> WorkerId {
        self.worker
    }
    pub fn generation(&self) -> Version {
        self.generation
    }
    pub fn len(&self) -> usize {
        self.count
    }
    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
    pub fn vote(&self, index: usize) -> CodecResult<EvaluatorVote> {
        self.entries
            .get(index)
            .copied()
            .flatten()
            .ok_or(NON_CANONICAL)
    }
}
impl AggregationInputView<'_> {
    pub fn worker_votes(&self, worker: WorkerId, generation: Version) -> CodecResult<WorkerVotes> {
        let frozen = self
            .roster
            .workers
            .iter()
            .find(|w| w.worker == worker)
            .ok_or(F05_REPORT_INVARIANT)?;
        if frozen.generation != generation {
            return Err(F05_REPORT_INVARIANT);
        }
        let mut entries = [None; 8];
        let mut count = 0usize;
        for report in self.reports {
            for cell in report.scores.entries() {
                let cell = cell?;
                if cell.worker == worker {
                    if count >= 8 {
                        return Err(CAPACITY);
                    }
                    entries[count] = Some(EvaluatorVote {
                        evaluator: report.binding.evaluator,
                        score: cell.score,
                    });
                    count += 1;
                }
            }
        }
        Ok(WorkerVotes {
            worker,
            generation,
            entries,
            count,
        })
    }
}
