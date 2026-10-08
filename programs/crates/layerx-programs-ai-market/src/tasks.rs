//! F01 task-set lifecycle over the complete committed shared state value: `ADMIT_TASK`,
//! `ACCEPT_TASK`, `CANCEL_TASK`, `COMMIT_TASK_RESULT` and the permissionless explicit
//! `SEAL_TASK_SET`.
//!
//! The task region of the F01 section is `task_count:u16 || seal presence:u8 || [seal32] ||
//! task_count * TaskBinding(233)` in strictly ascending `TaskId` order. Every transition binds
//! the currently opened epoch: F08 `current_epoch`, the frozen config and policy of the F01
//! section and the core roster digest of the F06 epoch row. Each success commits exactly one
//! revision increment with `header.state_revision` equal to the shared revision; every refusal
//! leaves `current` unchanged and the outputs must be discarded.
//!
//! Readings chosen over the producers present in the tree:
//! - No producer stores per-epoch frozen worker metadata or keys. The frozen worker set is the
//!   F06 epoch row (worker and frozen recipient); the frozen roster binding of an admission is
//!   proven by recomputing the core roster digest from that set, the current F02 attributes
//!   with the claimed metadata for the named worker, and the F03 snapshot of the epoch. A
//!   rostered worker whose F02 attributes changed since opening therefore blocks admission
//!   with `WRONG_ROSTER` until the next opening.
//! - Task records carry no epoch field. A record belongs to the clock epoch of
//!   `deadline - 1`, which admission keeps inside the admitting epoch's Work window. Until an
//!   opening resets the region with [`region_after_open`], a sealed region of an earlier epoch
//!   reads as the empty set of the opened epoch and an unsealed nonempty one refuses
//!   `WRONG_PHASE`.
use core::cmp::Ordering;

use sha2::{Digest as _, Sha256};

use crate::{
    admission::{AdmissionTable, Participant},
    codec::{self, derive_market, domain_hash, Envelope, EventCommon, Reader, Writer},
    codec::{Roster, ValidatedEnvelope},
    dispatch::{self, Operation},
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F01_LIFECYCLE_CLOSED, F01_NO_TASK_CAPACITY,
        F01_POLICY_MISMATCH, F01_PRINCIPAL_MISMATCH, F01_STALE_REVISION, F01_TASK_ALREADY_ACCEPTED,
        F01_TASK_CONFLICT, F01_TASK_EXPIRED, F01_TASK_NOT_FOUND, F01_UNKNOWN_WORKER,
        F01_WRONG_LIFECYCLE, F01_WRONG_WORKER, F02_DELEGATE_REVOKED,
        F09_EVIDENCE_TASK_SET_UNSEALED, KEY_MISMATCH, NON_CANONICAL, NOT_FOUND, REVOKED,
        UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE,
        WRONG_ROSTER,
    },
    evaluators::{
        authority::{self, split_identity_section},
        codec::verify_digest,
        model::VerificationError,
    },
    registry::{check_f01_capacity, market_clock, MarketHeader},
    registry_ops::{
        CallContext, PolicySection, ACTIVE, CLOSED, EMPTY_TASK_REGION, RECEIPT_MAX_BYTES,
        WINDING_DOWN,
    },
    rewards::{decode_reward_state, RewardState, REWARD_STATE_BYTES},
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, ReplayDecision, ReplayRequest,
        RetainedResult, Section, SharedState,
    },
    types::{
        Authentication, Digest32, EpochPhase, EpochWindows, EvaluatorRosterEntry, MarketId,
        MetadataDigest, PolicyDigest, Presence, PrincipalId, PublicKey32, RequestId, ResultDigest,
        RosterDigest, Signature64, TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{check_new_admission, WorkerCurrent, WorkerState, WorkerTable},
    MAX_EVALUATORS, MAX_TASKS, MAX_WORKERS,
};

/// Canonical `TaskBinding` length.
pub const TASK_BINDING_BYTES: usize = 233;
/// Canonical `ADMIT_TASK` payload length.
pub const ADMIT_PAYLOAD_BYTES: usize = 248;
/// Task count, seal presence and seal of a full region.
const REGION_HEADER_BYTES: usize = 35;
/// Largest canonical task region: a sealed set of `MAX_TASKS` bindings.
pub const TASK_REGION_MAX_BYTES: usize = REGION_HEADER_BYTES + MAX_TASKS * TASK_BINDING_BYTES;
const POLICY_CAP: usize = Section::PolicyLifecycle.payload_cap();
const CONTROL_CAP: usize = Section::Control.payload_cap();
/// Caller scratch for [`apply`]: the next task region, F01 section and control payloads.
pub const SCRATCH_BYTES: usize = TASK_REGION_MAX_BYTES + POLICY_CAP + CONTROL_CAP;
/// `SEAL_TASK_SET` event suffix: the sealed digest and task count.
pub const SEAL_SUFFIX_BYTES: usize = 34;
/// `ADMIT_TASK` event suffix: the admitted binding and the nonce it does not store.
pub const ADMIT_SUFFIX_BYTES: usize = TASK_BINDING_BYTES + 32;
const ADMISSION_DOMAIN: &str = "PAXAI/task-admission/v1";
const TASK_SET_DOMAIN: &[u8] = b"PAXAI/task-set/v1";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TaskStatus {
    Admitted = 1,
    Accepted = 2,
    ResultCommitted = 3,
    Cancelled = 4,
}
impl TaskStatus {
    /// Decodes a stored status byte.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` outside 1..=4.
    pub fn from_u8(value: u8) -> CodecResult<Self> {
        Ok(match value {
            1 => Self::Admitted,
            2 => Self::Accepted,
            3 => Self::ResultCommitted,
            4 => Self::Cancelled,
            _ => return Err(NON_CANONICAL),
        })
    }
}

/// One compact task record; absent acknowledgement and result encode as zero.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TaskBinding {
    pub task: TaskId,
    pub requester: PrincipalId,
    pub worker: WorkerId,
    pub input: Digest32,
    pub deadline: u64,
    pub status: TaskStatus,
    pub acknowledgement: Option<Digest32>,
    pub result: Option<Digest32>,
    pub admission: Digest32,
}
impl TaskBinding {
    /// Status/acknowledgement/result invariant.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the digests present do not match the status.
    pub fn validate(&self) -> CodecResult<()> {
        let valid = match self.status {
            TaskStatus::Admitted | TaskStatus::Cancelled => {
                self.acknowledgement.is_none() && self.result.is_none()
            }
            TaskStatus::Accepted => self.acknowledgement.is_some() && self.result.is_none(),
            TaskStatus::ResultCommitted => self.acknowledgement.is_some() && self.result.is_some(),
        };
        if valid {
            Ok(())
        } else {
            Err(NON_CANONICAL)
        }
    }
    /// Canonical 233 bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a record that fails [`TaskBinding::validate`].
    pub fn encode(&self) -> CodecResult<[u8; TASK_BINDING_BYTES]> {
        self.validate()?;
        let mut out = [0; TASK_BINDING_BYTES];
        let mut w = Writer::new(&mut out);
        w.put(self.task.as_bytes())?;
        w.put(self.requester.as_bytes())?;
        w.put(self.worker.as_bytes())?;
        w.put(self.input.as_bytes())?;
        w.u64(self.deadline)?;
        w.u8(self.status as u8)?;
        w.put(&self.acknowledgement.map_or([0; 32], Digest32::bytes))?;
        w.put(&self.result.map_or([0; 32], Digest32::bytes))?;
        w.put(self.admission.as_bytes())?;
        Ok(out)
    }
    /// Strict decode of exactly 233 bytes.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a short or long input, a zero identity, input or admission
    /// digest, an unknown status or a status/digest mismatch.
    pub fn decode(input: &[u8]) -> CodecResult<Self> {
        let mut r = Reader::new(input);
        let value = Self {
            task: TaskId::new(r.fixed()?)?,
            requester: PrincipalId::new(r.fixed()?)?,
            worker: WorkerId::new(r.fixed()?)?,
            input: Digest32::new(r.fixed()?)?,
            deadline: r.u64()?,
            status: TaskStatus::from_u8(r.u8()?)?,
            acknowledgement: Digest32::new(r.fixed()?).ok(),
            result: Digest32::new(r.fixed()?).ok(),
            admission: Digest32::new(r.fixed()?)?,
        };
        r.finish()?;
        value.validate()?;
        Ok(value)
    }
}

/// Borrowed, validated view of a task region.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TaskSet<'a> {
    seal: Option<Digest32>,
    records: &'a [u8],
}
const EMPTY_SET: TaskSet<'static> = TaskSet {
    seal: None,
    records: &[],
};
impl<'a> TaskSet<'a> {
    /// Strictly decodes a task region.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` for a count above `MAX_TASKS`, a bad presence flag, a zero seal,
    /// short or trailing bytes, an invalid record or records not strictly ascending by `TaskId`.
    pub fn decode(region: &'a [u8]) -> CodecResult<Self> {
        let mut r = Reader::new(region);
        let count = usize::from(r.u16()?);
        let seal = match r.u8()? {
            0 => None,
            1 => Some(Digest32::new(r.fixed()?)?),
            _ => return Err(NON_CANONICAL),
        };
        if count > MAX_TASKS {
            return Err(NON_CANONICAL);
        }
        let records = r.take(count * TASK_BINDING_BYTES)?;
        r.finish()?;
        let set = Self { seal, records };
        let mut previous = None;
        for binding in set.bindings() {
            let task = binding?.task;
            if previous.is_some_and(|p| p >= task) {
                return Err(NON_CANONICAL);
            }
            previous = Some(task);
        }
        Ok(set)
    }
    #[must_use]
    pub fn len(&self) -> usize {
        self.records.len() / TASK_BINDING_BYTES
    }
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.records.is_empty()
    }
    /// The materialized task-set digest, once sealed.
    #[must_use]
    pub const fn seal(&self) -> Option<Digest32> {
        self.seal
    }
    /// The records in ascending `TaskId` order.
    pub fn bindings(&self) -> impl Iterator<Item = CodecResult<TaskBinding>> + 'a {
        let records = self.records;
        records
            .chunks_exact(TASK_BINDING_BYTES)
            .map(TaskBinding::decode)
    }
    /// Index of `task`, or of its ordered insertion point, and the record when present.
    ///
    /// # Errors
    /// Propagates record decode refusals.
    pub fn position(&self, task: TaskId) -> CodecResult<(usize, Option<TaskBinding>)> {
        for (index, binding) in self.bindings().enumerate() {
            let binding = binding?;
            match binding.task.cmp(&task) {
                Ordering::Less => {}
                Ordering::Equal => return Ok((index, Some(binding))),
                Ordering::Greater => return Ok((index, None)),
            }
        }
        Ok((self.len(), None))
    }
}

/// The immutable binding every task of one opened epoch inherits.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SetBinding {
    pub market: MarketId,
    pub epoch: u64,
    pub config: Version,
    pub policy: PolicyDigest,
    pub roster: RosterDigest,
}

/// R044 `task_set_digest` over the records of `set` in their canonical order.
///
/// # Errors
/// Returns `ARITHMETIC` when the count does not fit `u16`; `NON_CANONICAL` for a zero digest.
pub fn task_set_digest(binding: &SetBinding, set: &TaskSet<'_>) -> CodecResult<Digest32> {
    let count = u16::try_from(set.len()).map_err(|_| ARITHMETIC)?;
    let mut h = Sha256::new();
    h.update(TASK_SET_DOMAIN);
    h.update([0]);
    h.update(binding.market.as_bytes());
    h.update(binding.epoch.to_be_bytes());
    h.update(binding.config.get().to_be_bytes());
    h.update(binding.policy.as_bytes());
    h.update(binding.roster.as_bytes());
    h.update(count.to_be_bytes());
    h.update(set.records);
    Digest32::new(h.finalize().into())
}

/// The F01 receipt: market, committed revision, request id, action, config and policy digest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Receipt {
    bytes: [u8; RECEIPT_MAX_BYTES],
    pub digest: ResultDigest,
}
impl Receipt {
    fn new(
        binding: &SetBinding,
        revision: u64,
        request: RequestId,
        operation: Operation,
    ) -> CodecResult<Self> {
        let mut bytes = [0; RECEIPT_MAX_BYTES];
        let mut w = Writer::new(&mut bytes);
        w.put(binding.market.as_bytes())?;
        w.u64(revision)?;
        w.put(request.as_bytes())?;
        w.u16(operation.selector())?;
        w.u64(binding.config.get())?;
        w.presence(&Presence::Present(binding.policy), |out, policy| {
            out.put(policy.as_bytes())
        })?;
        if w.len() != RECEIPT_MAX_BYTES {
            return Err(NON_CANONICAL);
        }
        Ok(Self {
            bytes,
            digest: codec::result_digest(&bytes)?,
        })
    }
    #[must_use]
    pub const fn bytes(&self) -> &[u8; RECEIPT_MAX_BYTES] {
        &self.bytes
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    /// One revision increment was composed into `next` and its event into `event`.
    Applied {
        receipt: Receipt,
        revision: u64,
        state_len: usize,
        event_len: usize,
    },
    /// Object-local repeat (byte-identical admission, cancelled task, same seal); nothing
    /// was written. `subject` is the task id or the sealed digest.
    AlreadyApplied { subject: Digest32 },
    /// Exact retry of a worker request already applied under its role sequence.
    Retained(RetainedResult),
}

/// Committed F01 section of `state`; its header revision must equal the shared revision.
fn committed<'a>(state: &SharedState<'a>) -> CodecResult<PolicySection<'a>> {
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    Ok(section)
}

fn rewards<'a>(state: &SharedState<'a>) -> CodecResult<RewardState<'a>> {
    let section = state.feature_sections[Section::SettlementClaims.index()];
    decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(WRONG_EPOCH)?)
}

/// Binding of the currently opened epoch.
fn set_binding(state: &SharedState<'_>, section: &PolicySection<'_>) -> CodecResult<SetBinding> {
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
    let epoch = admission.current_epoch().ok_or(WRONG_EPOCH)?;
    let row = match rewards(state)?.row(epoch) {
        Err(NOT_FOUND) => return Err(WRONG_EPOCH),
        row => row?,
    };
    Ok(SetBinding {
        market: section.header.market_id,
        epoch,
        config: Version::new(section.header.active_config_version)?,
        policy: section.current.digest()?,
        roster: row.roster,
    })
}

/// Clock epoch of the records of `set`, which must all agree.
fn records_epoch(set: &TaskSet<'_>, origin: u64) -> CodecResult<Option<u64>> {
    let mut epoch = None;
    for binding in set.bindings() {
        let last = binding?.deadline.checked_sub(1).ok_or(NON_CANONICAL)?;
        let clock = market_clock(origin, last)?.epoch;
        if epoch.is_some_and(|e| e != clock) {
            return Err(NON_CANONICAL);
        }
        epoch = Some(clock);
    }
    Ok(epoch)
}

/// The task set of the opened epoch over the stored region.
fn current_view<'a>(
    region: &'a [u8],
    origin: u64,
    binding: &SetBinding,
) -> CodecResult<TaskSet<'a>> {
    let set = TaskSet::decode(region)?;
    let Some(epoch) = records_epoch(&set, origin)? else {
        return Ok(match set.seal {
            Some(seal) if seal != task_set_digest(binding, &EMPTY_SET)? => EMPTY_SET,
            _ => set,
        });
    };
    match epoch.cmp(&binding.epoch) {
        Ordering::Equal => Ok(set),
        Ordering::Less if set.seal.is_some() => Ok(EMPTY_SET),
        Ordering::Less => Err(WRONG_PHASE),
        Ordering::Greater => Err(NON_CANONICAL),
    }
}

/// Task region an `OpenEpoch` composer writes for the newly opened epoch: the empty region,
/// replacing a sealed or empty previous set. The epoch opening calls
/// `next_policy.task_region = tasks::region_after_open(selected.task_region)?;`.
///
/// # Errors
/// Propagates region decode refusals; returns `WRONG_PHASE` for an unsealed nonempty set.
pub fn region_after_open(region: &[u8]) -> CodecResult<&'static [u8]> {
    let set = TaskSet::decode(region)?;
    if set.is_empty() || set.seal.is_some() {
        Ok(&EMPTY_TASK_REGION)
    } else {
        Err(WRONG_PHASE)
    }
}

/// Binding and task set of `epoch`, which must be the currently opened epoch.
fn epoch_view<'a>(
    state: &SharedState<'a>,
    epoch: u64,
) -> CodecResult<(SetBinding, Option<TaskSet<'a>>)> {
    let section = committed(state)?;
    let binding = set_binding(state, &section)?;
    if binding.epoch != epoch {
        return Err(WRONG_EPOCH);
    }
    match current_view(section.task_region, section.header.origin_height, &binding) {
        Err(WRONG_PHASE) => Ok((binding, None)),
        view => Ok((binding, Some(view?))),
    }
}

fn verified_seal(binding: &SetBinding, set: &TaskSet<'_>) -> CodecResult<Option<Digest32>> {
    match set.seal {
        Some(seal) if task_set_digest(binding, set)? != seal => Err(NON_CANONICAL),
        seal => Ok(seal),
    }
}

/// The explicitly sealed task-set digest of the opened `epoch`, for F09 evidence. Never
/// seals and never changes state.
///
/// # Errors
/// Returns `F09_EVIDENCE_TASK_SET_UNSEALED` when the set of `epoch` has not been sealed;
/// `WRONG_EPOCH` for another epoch; `NON_CANONICAL` for an inconsistent state or a stored seal
/// that does not match its records.
pub fn sealed_task_set(state: &SharedState<'_>, epoch: u64) -> CodecResult<Digest32> {
    let (binding, set) = epoch_view(state, epoch)?;
    let set = set.ok_or(F09_EVIDENCE_TASK_SET_UNSEALED)?;
    verified_seal(&binding, &set)?.ok_or(F09_EVIDENCE_TASK_SET_UNSEALED)
}

/// The task-set digest F05 terminalization of the opened `epoch` binds: the explicit seal,
/// or the pure digest of an empty unsealed set. Never seals and never changes state.
///
/// # Errors
/// Returns `WRONG_PHASE` for a nonempty unsealed set; `WRONG_EPOCH` for another epoch;
/// `NON_CANONICAL` for an inconsistent state or a stored seal that does not match its records.
pub fn terminal_task_set(state: &SharedState<'_>, epoch: u64) -> CodecResult<Digest32> {
    let (binding, set) = epoch_view(state, epoch)?;
    let set = set.ok_or(WRONG_PHASE)?;
    match verified_seal(&binding, &set)? {
        Some(seal) => Ok(seal),
        None if set.is_empty() => task_set_digest(&binding, &set),
        None => Err(WRONG_PHASE),
    }
}

struct Opened<'a> {
    state: SharedState<'a>,
    section: PolicySection<'a>,
    binding: SetBinding,
    set: TaskSet<'a>,
}
impl Opened<'_> {
    fn header(&self) -> &MarketHeader {
        &self.section.header
    }
}

fn opened(current: &[u8]) -> CodecResult<Opened<'_>> {
    let state = decode_shared_state(current)?;
    let section = committed(&state)?;
    let binding = set_binding(&state, &section)?;
    let set = current_view(section.task_region, section.header.origin_height, &binding)?;
    Ok(Opened {
        state,
        section,
        binding,
        set,
    })
}

struct Call<'c> {
    ctx: &'c CallContext,
    envelope: &'c ValidatedEnvelope<'c>,
    opened: &'c Opened<'c>,
}
impl Call<'_> {
    fn e(&self) -> &Envelope<'_> {
        &self.envelope.envelope
    }
    /// Domain, expiry, market and the opened epoch's envelope binding.
    fn check_binding(&self) -> CodecResult<()> {
        let e = self.e();
        let market = derive_market(self.ctx.chain, self.ctx.program)?;
        e.check_domain(self.ctx.chain, self.ctx.program, market)?;
        e.check_expiry(self.ctx.height)?;
        let binding = &self.opened.binding;
        if self.opened.header().market_id != market {
            return Err(WRONG_MARKET);
        }
        if e.epoch != binding.epoch {
            return Err(WRONG_EPOCH);
        }
        if e.config != binding.config.get() {
            return Err(WRONG_CONFIG);
        }
        if e.roster != Presence::Present(binding.roster) {
            return Err(WRONG_ROSTER);
        }
        Ok(())
    }
    /// Execution height inside the opened epoch's Work window.
    fn check_work(&self) -> CodecResult<EpochWindows> {
        let clock = market_clock(self.opened.header().origin_height, self.ctx.height)?;
        if clock.epoch != self.opened.binding.epoch {
            return Err(WRONG_PHASE);
        }
        if clock.phase != EpochPhase::Work {
            return Err(WRONG_PHASE);
        }
        Ok(clock.windows)
    }
    fn check_open_lifecycle(&self) -> CodecResult<()> {
        if self.opened.header().lifecycle == CLOSED {
            Err(F01_LIFECYCLE_CLOSED)
        } else {
            Ok(())
        }
    }
}

struct Buffers<'b> {
    next: &'b mut [u8],
    region: &'b mut [u8],
    policy: &'b mut [u8],
    control: &'b mut [u8],
    event: &'b mut [u8],
}

/// Writes `count:u16 || seal presence || [seal] || parts` into `out`.
fn write_region(seal: Option<Digest32>, parts: [&[u8]; 3], out: &mut [u8]) -> CodecResult<usize> {
    let bytes = parts.iter().map(|p| p.len()).sum::<usize>();
    if bytes % TASK_BINDING_BYTES != 0 || bytes > MAX_TASKS * TASK_BINDING_BYTES {
        return Err(CAPACITY);
    }
    let mut w = Writer::new(out);
    w.u16(u16::try_from(bytes / TASK_BINDING_BYTES).map_err(|_| ARITHMETIC)?)?;
    w.presence(&seal.map_or(Presence::Absent, Presence::Present), |w, d| {
        w.put(d.as_bytes())
    })?;
    for part in parts {
        w.put(part)?;
    }
    Ok(w.len())
}

/// Region of `set` with `binding` at `index`, inserted or replacing the record there.
fn write_binding(
    set: &TaskSet<'_>,
    index: usize,
    binding: &[u8; TASK_BINDING_BYTES],
    replace: bool,
    out: &mut [u8],
) -> CodecResult<usize> {
    if set.seal.is_some() {
        return Err(WRONG_PHASE);
    }
    let at = index.checked_mul(TASK_BINDING_BYTES).ok_or(ARITHMETIC)?;
    let resume = if replace {
        at.checked_add(TASK_BINDING_BYTES).ok_or(ARITHMETIC)?
    } else {
        at
    };
    let before = set.records.get(..at).ok_or(NON_CANONICAL)?;
    let after = set.records.get(resume..).ok_or(NON_CANONICAL)?;
    write_region(None, [before, binding, after], out)
}

/// Commits the region in `buffers.region[..region_len]` at the next revision, records a role
/// request when present, and emits the event.
fn finish(
    call: &Call<'_>,
    region_len: usize,
    replay: Option<&ReplayRequest>,
    suffix: &[u8],
    buffers: Buffers<'_>,
) -> CodecResult<Outcome> {
    let Buffers {
        next,
        region,
        policy,
        control,
        event,
    } = buffers;
    let opened = call.opened;
    let e = call.e();
    let revision = opened.state.revision.checked_add(1).ok_or(ARITHMETIC)?;
    let mut section = opened.section;
    section.task_region = region.get(..region_len).ok_or(CAPACITY)?;
    section.header.state_revision = revision;
    let policy_len = section.encode(policy)?;
    let policy = policy.get(..policy_len).ok_or(CAPACITY)?;
    let mut candidate = opened
        .state
        .replace_section(Section::PolicyLifecycle, policy)?;
    let receipt = Receipt::new(&opened.binding, revision, e.request, e.operation)?;
    match replay {
        Some(request) => {
            if candidate.record_success(request, call.ctx.height, receipt.digest)?
                != ReplayDecision::Apply
            {
                return Err(NON_CANONICAL);
            }
        }
        None => candidate.revision = revision,
    }
    if candidate.revision != revision {
        return Err(NON_CANONICAL);
    }
    check_f01_capacity(policy_len, candidate.encoded_len()?)?;
    let state_len = encode_shared_state(&candidate, next, control)?;
    let event_len = codec::encode_event_frame(
        e.operation,
        &EventCommon {
            market: opened.binding.market,
            epoch: opened.binding.epoch,
            config: opened.binding.config,
            revision,
            request: call.envelope.request_digest()?,
            result: receipt.digest,
        },
        suffix,
        event,
    )?;
    Ok(Outcome::Applied {
        receipt,
        revision,
        state_len,
        event_len,
    })
}

struct AdmitBody {
    epoch: u64,
    config: u64,
    policy: PolicyDigest,
    roster: RosterDigest,
    requester: PrincipalId,
    worker: WorkerId,
    metadata: MetadataDigest,
    nonce: [u8; 32],
    input: Digest32,
    deadline: u64,
    digest: Digest32,
}

fn parse_admit(payload: &[u8]) -> CodecResult<AdmitBody> {
    let mut r = Reader::new(payload);
    let body = AdmitBody {
        epoch: r.u64()?,
        config: r.u64()?,
        policy: PolicyDigest::new(r.fixed()?)?,
        roster: RosterDigest::new(r.fixed()?)?,
        requester: PrincipalId::new(r.fixed()?)?,
        worker: WorkerId::new(r.fixed()?)?,
        metadata: MetadataDigest::new(r.fixed()?)?,
        nonce: r.fixed()?,
        input: Digest32::new(r.fixed()?)?,
        deadline: r.u64()?,
        digest: domain_hash(ADMISSION_DOMAIN, payload)?,
    };
    r.finish()?;
    Ok(body)
}

fn evaluator_roster(
    identity: &[u8],
    epoch: u64,
) -> CodecResult<([EvaluatorRosterEntry; MAX_EVALUATORS], usize)> {
    let region = authority::evaluator_region(identity)?;
    let snapshot = region
        .snapshot()
        .filter(|s| s.epoch == epoch)
        .ok_or(NON_CANONICAL)?;
    let first = snapshot.entries().next().ok_or(NON_CANONICAL)?.entry;
    let mut roster = [first; MAX_EVALUATORS];
    for (slot, frozen) in roster.iter_mut().zip(snapshot.entries()) {
        *slot = frozen.entry;
    }
    Ok((roster, snapshot.len()))
}

/// R041 frozen-worker binding: the worker is frozen in the opened epoch, its operating
/// authority is current and unrevoked, and the claimed metadata reproduces the frozen roster.
fn check_frozen_worker(
    state: &SharedState<'_>,
    binding: &SetBinding,
    body: &AdmitBody,
    height: u64,
) -> CodecResult<()> {
    let rewards = rewards(state)?;
    let row = rewards.row(binding.epoch)?;
    let dictionary = rewards.dictionary();
    let mut frozen = None;
    for entry in row.entries() {
        let slot = dictionary.slot(entry.slot)?;
        if slot.worker == body.worker {
            frozen = Some(slot);
        }
    }
    let frozen = frozen.ok_or(F01_UNKNOWN_WORKER)?;
    let identity = state.feature_sections[Section::IdentityRoster.index()];
    let live = WorkerTable::decode(split_identity_section(identity)?.0)?;
    let record = live.get(body.worker).ok_or(NON_CANONICAL)?;
    let target = WorkerRosterEntry {
        worker: body.worker,
        owner: record.owner,
        recipient: frozen.recipient,
        generation: Version::new(record.generation)?,
        key_version: Version::new(record.key_version)?,
        public_key: record.delegate,
        metadata: body.metadata,
    };
    check_new_admission(&record, &target, height)?;
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
    if admission
        .get(Participant::Worker(body.worker))
        .is_none_or(|m| m.revoked())
    {
        return Err(REVOKED);
    }
    let mut workers = [target; MAX_WORKERS];
    let mut count = 0;
    for entry in row.entries() {
        let slot = dictionary.slot(entry.slot)?;
        if slot.worker != body.worker {
            let other = live.get(slot.worker).ok_or(NON_CANONICAL)?;
            *workers.get_mut(count).ok_or(CAPACITY)? = WorkerRosterEntry {
                worker: slot.worker,
                owner: other.owner,
                recipient: slot.recipient,
                generation: Version::new(other.generation)?,
                key_version: Version::new(other.key_version)?,
                public_key: other.delegate,
                metadata: other.metadata,
            };
        }
        count += 1;
    }
    let workers = workers.get_mut(..count).ok_or(CAPACITY)?;
    workers.sort_unstable_by_key(|w| w.worker);
    let (evaluators, evaluator_count) = evaluator_roster(identity, binding.epoch)?;
    let digest = codec::roster_digest(&Roster {
        market: binding.market,
        epoch: binding.epoch,
        config: binding.config,
        workers,
        evaluators: evaluators.get(..evaluator_count).ok_or(CAPACITY)?,
    })?;
    if digest == binding.roster {
        Ok(())
    } else {
        Err(WRONG_ROSTER)
    }
}

/// `ADMIT_TASK` (0x010C): object-local by the requester principal.
fn admit(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = call.e();
    let body = parse_admit(e.payload)?;
    codec::compare_native_principal(e, call.ctx.principal)?;
    if body.requester != e.actor {
        return Err(F01_PRINCIPAL_MISMATCH);
    }
    call.check_binding()?;
    let opened = call.opened;
    let binding = &opened.binding;
    if body.epoch != binding.epoch {
        return Err(WRONG_EPOCH);
    }
    let task = codec::derive_task(binding.market, binding.epoch, body.requester, body.nonce)?;
    let (index, existing) = opened.set.position(task)?;
    if let Some(existing) = existing {
        return if existing.admission == body.digest {
            Ok(Outcome::AlreadyApplied {
                subject: Digest32::new(task.bytes())?,
            })
        } else {
            Err(F01_TASK_CONFLICT)
        };
    }
    match opened.header().lifecycle {
        ACTIVE => {}
        WINDING_DOWN | CLOSED => return Err(F01_LIFECYCLE_CLOSED),
        _ => return Err(F01_WRONG_LIFECYCLE),
    }
    if body.config != binding.config.get() || body.policy != binding.policy {
        return Err(F01_POLICY_MISMATCH);
    }
    if body.roster != binding.roster {
        return Err(WRONG_ROSTER);
    }
    let windows = call.check_work()?;
    let height = call.ctx.height;
    check_frozen_worker(&opened.state, binding, &body, height)?;
    let policy = &opened.section.current;
    if opened.set.len() >= usize::from(policy.max_tasks_per_epoch) {
        return Err(F01_NO_TASK_CAPACITY);
    }
    let latest = height
        .checked_add(u64::from(policy.task_timeout_heights))
        .ok_or(ARITHMETIC)?
        .min(windows.commit);
    if body.deadline <= height || body.deadline > latest {
        return Err(F01_TASK_EXPIRED);
    }
    let record = TaskBinding {
        task,
        requester: body.requester,
        worker: body.worker,
        input: body.input,
        deadline: body.deadline,
        status: TaskStatus::Admitted,
        acknowledgement: None,
        result: None,
        admission: body.digest,
    }
    .encode()?;
    let region_len = write_binding(&opened.set, index, &record, false, buffers.region)?;
    let mut suffix = [0; ADMIT_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.put(&record)?;
    w.put(&body.nonce)?;
    finish(call, region_len, None, &suffix, buffers)
}

fn task_id(payload: &[u8]) -> CodecResult<TaskId> {
    let mut r = Reader::new(payload);
    let task = TaskId::new(r.fixed()?)?;
    r.finish()?;
    Ok(task)
}

/// The opened epoch's record of `task`.
fn find(call: &Call<'_>, task: TaskId) -> CodecResult<(usize, TaskBinding)> {
    match call.opened.set.position(task)? {
        (index, Some(binding)) => Ok((index, binding)),
        (_, None) => Err(F01_TASK_NOT_FOUND),
    }
}

/// `CANCEL_TASK` (0x010E): object-local by the task's requester.
fn cancel(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = call.e();
    let task = task_id(e.payload)?;
    codec::compare_native_principal(e, call.ctx.principal)?;
    call.check_binding()?;
    let (index, record) = find(call, task)?;
    if record.requester != e.actor {
        return Err(UNAUTHORIZED);
    }
    match record.status {
        TaskStatus::Admitted => {}
        TaskStatus::Cancelled => {
            return Ok(Outcome::AlreadyApplied {
                subject: Digest32::new(task.bytes())?,
            })
        }
        TaskStatus::Accepted | TaskStatus::ResultCommitted => {
            return Err(F01_TASK_ALREADY_ACCEPTED)
        }
    }
    call.check_open_lifecycle()?;
    if call.ctx.height >= record.deadline {
        return Err(F01_TASK_EXPIRED);
    }
    call.check_work()?;
    let record = TaskBinding {
        status: TaskStatus::Cancelled,
        ..record
    }
    .encode()?;
    let region_len = write_binding(&call.opened.set, index, &record, true, buffers.region)?;
    finish(call, region_len, None, &record, buffers)
}

fn verify(key: PublicKey32, signature: Signature64, digest: Digest32) -> CodecResult<()> {
    verify_digest(key, signature, digest.bytes()).map_err(|e| match e {
        VerificationError::Application(a) => a,
        #[cfg(target_arch = "wasm32")]
        VerificationError::Host(_) => crate::errors::HOST_CAPABILITY,
    })
}

/// The task's worker principal natively, or its current enrolled signing delegate.
fn authenticate_worker(call: &Call<'_>, worker: WorkerId) -> CodecResult<WorkerCurrent> {
    let e = call.e();
    let identity = call.opened.state.feature_sections[Section::IdentityRoster.index()];
    let record = WorkerTable::decode(split_identity_section(identity)?.0)?
        .get(worker)
        .ok_or(F01_WRONG_WORKER)?;
    match e.authentication {
        Authentication::Native => {
            codec::compare_native_principal(e, call.ctx.principal)?;
            if e.actor != record.owner {
                return Err(F01_WRONG_WORKER);
            }
        }
        Authentication::Delegate { key, signature } => {
            if e.actor != record.owner {
                return Err(F01_WRONG_WORKER);
            }
            if record.state == WorkerState::Revoked {
                return Err(F02_DELEGATE_REVOKED);
            }
            if key != record.delegate {
                return Err(KEY_MISMATCH);
            }
            verify(
                key,
                signature,
                Digest32::new(call.envelope.request_digest()?.bytes())?,
            )?;
        }
    }
    Ok(record)
}

/// Role replay request under the worker's actor slot.
fn worker_request(call: &Call<'_>, record: &WorkerCurrent) -> CodecResult<ReplayRequest> {
    let slot = ActorSlot::worker(usize::from(record.slot))?;
    let actor = call
        .opened
        .state
        .control
        .replay
        .actor(slot)
        .ok_or(NOT_FOUND)?;
    if actor.principal != record.owner {
        return Err(UNAUTHORIZED);
    }
    ReplayRequest::from_envelope(slot, actor.authority_version, call.envelope)
}

/// R042/R043 status step of an accepted or committed result.
fn advance_status(
    operation: Operation,
    record: &TaskBinding,
    digest: Digest32,
) -> CodecResult<TaskBinding> {
    match (operation, record.status) {
        (dispatch::ACCEPT_TASK, TaskStatus::Admitted) => Ok(TaskBinding {
            status: TaskStatus::Accepted,
            acknowledgement: Some(digest),
            ..*record
        }),
        (dispatch::ACCEPT_TASK, TaskStatus::Accepted | TaskStatus::ResultCommitted) => {
            Err(F01_TASK_ALREADY_ACCEPTED)
        }
        (dispatch::COMMIT_TASK_RESULT, TaskStatus::Accepted) => Ok(TaskBinding {
            status: TaskStatus::ResultCommitted,
            result: Some(digest),
            ..*record
        }),
        _ => Err(F01_TASK_CONFLICT),
    }
}

/// `ACCEPT_TASK` (0x010D) and `COMMIT_TASK_RESULT` (0x010F): role-sequenced by the task's
/// worker or its delegate.
fn worker_step(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = call.e();
    let mut r = Reader::new(e.payload);
    let expected = r.u64()?;
    let task = TaskId::new(r.fixed()?)?;
    let digest = Digest32::new(r.fixed()?)?;
    r.finish()?;
    call.check_binding()?;
    let (index, record) = find(call, task)?;
    let worker = authenticate_worker(call, record.worker)?;
    let request = worker_request(call, &worker)?;
    let replay = &call.opened.state.control.replay;
    if let ReplayDecision::AlreadyApplied(retained) = replay.check(&request, call.ctx.height)? {
        return Ok(Outcome::Retained(retained));
    }
    if expected != call.opened.state.revision {
        return Err(F01_STALE_REVISION);
    }
    call.check_open_lifecycle()?;
    if call.ctx.height >= record.deadline {
        return Err(F01_TASK_EXPIRED);
    }
    let next = advance_status(e.operation, &record, digest)?.encode()?;
    call.check_work()?;
    let region_len = write_binding(&call.opened.set, index, &next, true, buffers.region)?;
    finish(call, region_len, Some(&request), &next, buffers)
}

/// `SEAL_TASK_SET` (0x0110): permissionless, object-local, at or after Work end.
fn seal(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = call.e();
    let mut r = Reader::new(e.payload);
    let epoch = r.u64()?;
    let config = r.u64()?;
    let expected = Digest32::new(r.fixed()?)?;
    r.finish()?;
    codec::compare_native_principal(e, call.ctx.principal)?;
    call.check_binding()?;
    let opened = call.opened;
    let binding = &opened.binding;
    if epoch != binding.epoch {
        return Err(WRONG_EPOCH);
    }
    if config != binding.config.get() {
        return Err(WRONG_CONFIG);
    }
    call.check_open_lifecycle()?;
    let windows = EpochWindows::new(opened.header().origin_height, binding.epoch)?;
    if call.ctx.height < windows.commit {
        return Err(WRONG_PHASE);
    }
    if let Some(stored) = opened.set.seal {
        return if stored == expected {
            Ok(Outcome::AlreadyApplied { subject: stored })
        } else {
            Err(CONFLICT)
        };
    }
    let digest = task_set_digest(binding, &opened.set)?;
    if digest != expected {
        return Err(CONFLICT);
    }
    let region_len = write_region(Some(digest), [opened.set.records, &[], &[]], buffers.region)?;
    let mut suffix = [0; SEAL_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.put(digest.as_bytes())?;
    w.u16(u16::try_from(opened.set.len()).map_err(|_| ARITHMETIC)?)?;
    finish(call, region_len, None, &suffix, buffers)
}

/// Applies one F01 task-set request to the committed `current` state, writing the whole next
/// state into `next` (at least `MAX_STATE_BYTES`) and its event into `event`; `scratch` holds
/// at least [`SCRATCH_BYTES`]. Dispatch arm: `dispatch::ADMIT_TASK | dispatch::ACCEPT_TASK |
/// dispatch::CANCEL_TASK | dispatch::COMMIT_TASK_RESULT | dispatch::SEAL_TASK_SET =>
/// tasks::apply(&ctx, &envelope, current, next, scratch, event)`.
///
/// # Errors
/// `UNKNOWN_OPERATION`; `NON_CANONICAL` for a malformed payload or zero digest, or an
/// inconsistent committed state; envelope principal, domain and expiry refusals;
/// `WRONG_MARKET`; `WRONG_EPOCH`, `WRONG_CONFIG` and `WRONG_ROSTER` for a binding other than
/// the opened epoch's; `F01_PRINCIPAL_MISMATCH`; `F01_TASK_CONFLICT`; `F01_WRONG_LIFECYCLE`
/// and `F01_LIFECYCLE_CLOSED`; `F01_POLICY_MISMATCH`; `WRONG_PHASE` outside Work, for a seal
/// before Work end, or over an unsealed set of an earlier epoch; `F01_UNKNOWN_WORKER`; F02
/// `check_new_admission` refusals and `REVOKED`; `F01_NO_TASK_CAPACITY`; `F01_TASK_EXPIRED`;
/// `F01_TASK_NOT_FOUND`; `UNAUTHORIZED` for a cancel by another principal;
/// `F01_WRONG_WORKER`, `F02_DELEGATE_REVOKED`, `KEY_MISMATCH` and `BAD_SIGNATURE`; common
/// replay refusals; `F01_STALE_REVISION`; `F01_TASK_ALREADY_ACCEPTED`; `CONFLICT` for a seal
/// whose expected digest differs; `F01_CAPACITY_UNAVAILABLE` and `CAPACITY`. On any error
/// `current` is unchanged and the outputs must be discarded.
pub fn apply(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let operation = envelope.envelope.operation;
    if !matches!(
        operation,
        dispatch::ADMIT_TASK
            | dispatch::ACCEPT_TASK
            | dispatch::CANCEL_TASK
            | dispatch::COMMIT_TASK_RESULT
            | dispatch::SEAL_TASK_SET
    ) {
        return Err(UNKNOWN_OPERATION);
    }
    let opened = opened(current)?;
    let (region, rest) = scratch
        .split_at_mut_checked(TASK_REGION_MAX_BYTES)
        .ok_or(CAPACITY)?;
    let (policy, control) = rest.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let call = Call {
        ctx,
        envelope,
        opened: &opened,
    };
    let buffers = Buffers {
        next,
        region,
        policy,
        control,
        event,
    };
    match operation {
        dispatch::ADMIT_TASK => admit(&call, buffers),
        dispatch::CANCEL_TASK => cancel(&call, buffers),
        dispatch::SEAL_TASK_SET => seal(&call, buffers),
        _ => worker_step(&call, buffers),
    }
}
