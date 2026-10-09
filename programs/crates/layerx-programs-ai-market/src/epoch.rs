//! F01 `OpenEpoch` composer and `ADVANCE_ACTIVATION` over the complete committed shared
//! state value.
//!
//! `OpenEpoch` is lazy: it opens only the clock epoch of the authenticated execution height,
//! only inside that epoch's Work window, and never backdates. Every window bound, including
//! `T + 128`, comes from the checked [`market_clock`] arithmetic; an unrepresentable epoch
//! refuses `ARITHMETIC` before anything is composed. One successful opening atomically:
//! selects the sole pending policy when `epoch >= effective_epoch` (F01-R028), runs the F08
//! roster rollover (which marks missed epochs skipped and requires the previous opened epoch
//! to be terminal), runs the F03 evaluator snapshot under the selected policy rubric, enforces
//! anti-self-dealing and minimum readiness, freezes the core roster hash, reserves the F06
//! epoch budget, and commits exactly one revision increment with `header.state_revision`
//! equal to the shared revision. Every refusal leaves the committed input untouched; `next`,
//! `scratch` and `event` must then be discarded.
//!
//! The frozen snapshot of an opened epoch lives in its producers: F08 holds epoch presence
//! (`current_epoch`), membership and liveness baselines; F03 holds the frozen evaluator
//! grant/key versions; F06 holds the budget, the core roster digest and the frozen recipient
//! slots; F01 holds the frozen config and policy, which only the next opening can replace.
//!
//! Producer gaps and the readings chosen here:
//! - F02 stores no payout recipient. The worker's frozen recipient is the native account of
//!   its owner principal, which is how the runtime names a principal's account
//!   (`visible_principal_account`). F06 still refuses a recipient equal to the reserve account.
//! - F02 and F03 declare no control identity beyond the owner principal, so the owner
//!   principal is the declared controlling identity. An evaluator owned by the market owner, by
//!   a worker owner, or signing with a worker delegate key is a `ROLE_CONFLICT`.
//! - The joint F05/F06 section starts with the F06 reward state (`REWARD_STATE_BYTES`); any
//!   F05 bytes after it are carried unchanged. Without an F05 terminal record, "previous epoch
//!   terminal" is the F06 ledger having no active reserve.
//! - The epoch budget is `min(epoch_budget_cap, Free)` and must reach
//!   `minimum_epoch_funding`; a market whose reward state was never initialized is not ready.
//! - `OPEN_EPOCH` has no specified payload; it must be empty. The envelope binds the epoch
//!   being opened, its frozen config and the roster digest the opening freezes (an absent
//!   roster is only representable for epoch 0).
//! - The F01 task region is reset through `tasks::region_after_open`: a sealed or empty set
//!   yields the empty region, a nonempty unsealed set refuses the opening with `WRONG_PHASE`.
//! - The joint F07/F08 section is `RP07 region || [HA07 history seal] || F08 table`
//!   (`reputation_transition::split_joint`). A market without the F07 region keeps the F08
//!   table alone and every F07 phase is skipped. With the region, the opening atomically rolls
//!   every live reputation segment over to the selected config/policy/model, retires the records
//!   of workers that left the F02 table, bootstraps frozen workers unobserved and releases the
//!   completed summaries F06 no longer retains; [`aggregate`] freezes `history_allowed` at the
//!   F05 seal and applies the completed epoch inside the F05 terminal transition; [`history`]
//!   runs `ResetHistory`/`SuspendHistory`/`ResumeHistory`; [`carry_reputation`] keeps the region
//!   byte-identical across every other producer, which reads the F08 table only.
use crate::{
    admission::{AdmissionMeta, AdmissionTable, Participant},
    aggregation::{self, Outcome as Aggregated},
    aggregation_codec::{decode_current, WORKER_AGGREGATE_BYTES},
    codec::{
        self, derive_market, Envelope, EventCommon, Reader, Roster, ValidatedEnvelope, Writer,
    },
    dispatch,
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F01_ACTIVATION_NOT_READY,
        F01_ACTIVATION_TOO_EARLY, F01_ALREADY_ACTIVATED, F01_LIFECYCLE_CLOSED, F01_STALE_REVISION,
        F07_BINDING_MISMATCH, F07_IDEMPOTENCY_CONFLICT, F07_IDENTITY_FROZEN, F07_UNKNOWN_WORKER,
        F08_MARKET_PAUSED, INSUFFICIENT_FREE, NON_CANONICAL, NOT_FOUND, READINESS_BLOCKED,
        REPLAY_CONFLICT, RETENTION_FULL, ROLE_CONFLICT, UNAUTHORIZED, UNKNOWN_OPERATION,
        WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE, WRONG_ROSTER,
    },
    evaluators::{
        authority::{self, split_identity_section, SnapshotContext},
        model::GrantStatus,
    },
    policy::TaskPolicyV1,
    registry::{check_f01_capacity, market_clock, MarketHeader},
    registry_ops::{
        activate_pending_policy, CallContext, PolicySection, ACTIVE, REGISTERED, SUSPENDED,
    },
    reputation::{ClosureReason, HistoryStatus, ReputationCurrent, SegmentKey, LIMIT},
    reputation_transition::{
        bound_record, check_expected, complete_epoch, encode_joint, open_segments, prune_released,
        replace_record, reset_segment, seal_history, set_status, split_joint, Region,
        SegmentBinding, WorkerObservation,
    },
    rewards::{decode_reward_state, EpochStatus, RewardEpoch, RewardState, REWARD_STATE_BYTES},
    roster::{self, ROLLOVER_SCRATCH_BYTES},
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, ReplayDecision, ReplayRequest,
        RetainedResult, Section, SharedState,
    },
    tasks,
    types::{
        AccountId, Amount, Digest32, EpochPhase, EvaluatorRosterEntry, FrozenBinding, PolicyDigest,
        Presence, PrincipalId, ResultDigest, RosterDigest, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerState, WorkerTable},
    MAX_EVALUATORS, MAX_STATE_BYTES, MAX_WORKERS,
};

const POLICY_CAP: usize = Section::PolicyLifecycle.payload_cap();
const IDENTITY_CAP: usize = Section::IdentityRoster.payload_cap();
const SETTLEMENT_CAP: usize = Section::SettlementClaims.payload_cap();
const CONTROL_CAP: usize = Section::Control.payload_cap();
const JOINT_OUT: usize = Section::ReputationAdmission.payload_cap();
const ADMISSION: usize = Section::ReputationAdmission.index();
/// Persisted bytes of one sealed F05 report commitment (evaluator and report digest).
const SEALED_REPORT_BYTES: usize = 64;

/// `EpochOpened` suffix: policy digest, roster digest, budget, worker and evaluator counts,
/// policy-activated flag, previous-epoch presence and value, skipped epochs.
pub const OPEN_SUFFIX_BYTES: usize = 100;
/// `MarketActivated` suffix: activation epoch, clock epoch and committed height.
pub const ACTIVATION_SUFFIX_BYTES: usize = 24;
/// Caller scratch for [`open_epoch`]: the rolled-over state, the rollover scratch, the next
/// identity, settlement, policy and control payloads, the state without its F07 region and the
/// next joint F07/F08 section.
pub const OPEN_SCRATCH_BYTES: usize = MAX_STATE_BYTES
    + ROLLOVER_SCRATCH_BYTES
    + IDENTITY_CAP
    + SETTLEMENT_CAP
    + POLICY_CAP
    + CONTROL_CAP
    + MAX_STATE_BYTES
    + JOINT_OUT;
/// Caller scratch of [`carry_reputation`] beside the wrapped producer's own buffers: the state
/// without its F07 region, the next joint section and a control payload.
pub const CARRY_SCRATCH_BYTES: usize = MAX_STATE_BYTES + JOINT_OUT + CONTROL_CAP;
/// Caller scratch for [`aggregate`]: the F05 scratch followed by the carry scratch.
pub const AGGREGATE_SCRATCH_BYTES: usize = aggregation::SCRATCH_BYTES + CARRY_SCRATCH_BYTES;
/// Caller scratch for [`history`]: the next policy payload, joint section and control payload.
pub const HISTORY_SCRATCH_BYTES: usize = POLICY_CAP + JOINT_OUT + CONTROL_CAP;
/// `HistoryReset` suffix: worker, old segment, new segment, reset generation and reason.
pub const RESET_SUFFIX_BYTES: usize = 105;
/// `HistoryStatus` suffix: worker, segment, reset generation, new status and reason.
pub const STATUS_SUFFIX_BYTES: usize = 74;
/// `ResetHistory`/`SuspendHistory`/`ResumeHistory` payload: worker, expected segment digest,
/// expected reset generation and reason.
pub const HISTORY_PAYLOAD_BYTES: usize = 73;
/// Caller scratch for [`advance_activation`]: the activated policy and control payloads, one
/// trial opening state and the opening scratch.
pub const ADVANCE_SCRATCH_BYTES: usize =
    POLICY_CAP + CONTROL_CAP + MAX_STATE_BYTES + OPEN_SCRATCH_BYTES;

/// The immutable binding one successful opening freezes.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Frozen {
    pub epoch: u64,
    pub previous: Option<u64>,
    pub skipped: u64,
    pub config: Version,
    pub policy: PolicyDigest,
    pub policy_activated: bool,
    pub roster: RosterDigest,
    pub budget: Amount,
    pub workers: u8,
    pub evaluators: u8,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    Opened {
        frozen: Frozen,
        revision: u64,
        result: ResultDigest,
        state_len: usize,
        event_len: usize,
    },
    /// The clock epoch is already open under exactly this binding; nothing was written.
    AlreadyApplied { epoch: u64, roster: RosterDigest },
}

/// Prerequisites an opening of `epoch` satisfies (or the opened epoch already satisfies).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Readiness {
    pub epoch: u64,
    pub workers: usize,
    pub evaluators: usize,
    pub budget: Amount,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Activated {
    pub readiness: Readiness,
    pub activation_epoch: u64,
    pub revision: u64,
    pub result: ResultDigest,
    pub state_len: usize,
    pub event_len: usize,
}

fn committed(current: &[u8]) -> CodecResult<(SharedState<'_>, PolicySection<'_>)> {
    let state = decode_shared_state(current)?;
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    Ok((state, section))
}

/// Clock epoch of `height` inside its Work window. `market_clock` computes every window
/// bound of that epoch with checked arithmetic, so an epoch whose `T + 128` is not
/// representable refuses `ARITHMETIC`.
fn work_epoch(header: &MarketHeader, height: u64) -> CodecResult<u64> {
    if height < header.origin_height {
        return Err(WRONG_EPOCH);
    }
    let clock = market_clock(header.origin_height, height)?;
    if clock.phase != EpochPhase::Work {
        return Err(WRONG_PHASE);
    }
    Ok(clock.epoch)
}

fn opening_epoch(header: &MarketHeader, height: u64) -> CodecResult<u64> {
    match header.lifecycle {
        REGISTERED | ACTIVE => work_epoch(header, height),
        SUSPENDED => Err(F08_MARKET_PAUSED),
        _ => Err(F01_LIFECYCLE_CLOSED),
    }
}

fn authenticate(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    header: &MarketHeader,
) -> CodecResult<()> {
    let e = &envelope.envelope;
    codec::compare_native_principal(e, ctx.principal)?;
    let market = derive_market(ctx.chain, ctx.program)?;
    e.check_domain(ctx.chain, ctx.program, market)?;
    e.check_expiry(ctx.height)?;
    if header.market_id != market {
        return Err(WRONG_MARKET);
    }
    Ok(())
}

/// F01-R028 selection: the sole pending policy once `epoch` reaches its effective epoch.
fn select_policy<'a>(
    section: &PolicySection<'a>,
    epoch: u64,
) -> CodecResult<(PolicySection<'a>, bool)> {
    match section.pending {
        Presence::Present(pending) if epoch >= pending.effective_epoch => {
            Ok((activate_pending_policy(section, epoch)?, true))
        }
        _ => Ok((*section, false)),
    }
}

/// Frozen core worker entry of an F08 member eligible at this opening.
fn worker_entry(
    m: &AdmissionMeta,
    workers: &WorkerTable,
    height: u64,
) -> CodecResult<Option<WorkerRosterEntry>> {
    let Participant::Worker(worker) = m.participant else {
        return Ok(None);
    };
    if !m.admitted() || m.admitted_epoch.is_none() || m.revoked() || m.draining() {
        return Ok(None);
    }
    let record = workers.get(worker).ok_or(NON_CANONICAL)?;
    if !matches!(record.state, WorkerState::Enrolled | WorkerState::Available)
        || height >= record.expiry
    {
        return Ok(None);
    }
    if record.owner != m.owner || record.key_version != m.delegate_generation {
        return Err(NON_CANONICAL);
    }
    Ok(Some(WorkerRosterEntry {
        worker,
        owner: record.owner,
        recipient: AccountId::new(record.owner.bytes())?,
        generation: Version::new(record.generation)?,
        key_version: Version::new(record.key_version)?,
        public_key: record.delegate,
        metadata: record.metadata,
    }))
}

fn worker_roster(
    admission: &AdmissionTable,
    workers: &WorkerTable,
    height: u64,
) -> CodecResult<([WorkerRosterEntry; MAX_WORKERS], usize)> {
    let mut found = [None; MAX_WORKERS];
    let mut count = 0usize;
    for m in admission.iter() {
        if let Some(entry) = worker_entry(m, workers, height)? {
            *found.get_mut(count).ok_or(CAPACITY)? = Some(entry);
            count += 1;
        }
    }
    let first = found[0].ok_or(READINESS_BLOCKED)?;
    let mut roster = [first; MAX_WORKERS];
    for (slot, entry) in roster.iter_mut().zip(found.iter().flatten()) {
        *slot = *entry;
    }
    roster[..count].sort_unstable_by_key(|e| e.worker);
    Ok((roster, count))
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
    let first = snapshot.entries().next().ok_or(READINESS_BLOCKED)?.entry;
    let mut roster = [first; MAX_EVALUATORS];
    for (slot, frozen) in roster.iter_mut().zip(snapshot.entries()) {
        *slot = frozen.entry;
    }
    Ok((roster, snapshot.len()))
}

/// F01-R015 at epoch opening over declared principals and keys.
fn check_independence(
    market_owner: PrincipalId,
    workers: &[WorkerRosterEntry],
    evaluators: &[EvaluatorRosterEntry],
) -> CodecResult<()> {
    for e in evaluators {
        if e.owner == market_owner
            || workers
                .iter()
                .any(|w| w.owner == e.owner || w.public_key == e.public_key)
        {
            return Err(ROLE_CONFLICT);
        }
    }
    Ok(())
}

fn check_readiness(policy: &TaskPolicyV1, workers: usize, evaluators: usize) -> CodecResult<()> {
    let workers_ready = (usize::from(policy.minimum_worker_count)
        ..=usize::from(policy.max_workers))
        .contains(&workers);
    let evaluators_ready = (usize::from(policy.minimum_evaluator_count)
        ..=usize::from(policy.max_evaluators))
        .contains(&evaluators);
    if workers_ready && evaluators_ready {
        Ok(())
    } else {
        Err(READINESS_BLOCKED)
    }
}

fn epoch_budget(policy: &TaskPolicyV1, free: Amount) -> CodecResult<Amount> {
    let budget = policy.epoch_budget_cap.min(free);
    if budget < policy.minimum_epoch_funding {
        Err(INSUFFICIENT_FREE)
    } else {
        Ok(budget)
    }
}

/// F06 `ReserveEpoch` of `rewards`, the validated reward state at the head of `section`, into
/// `out`, carrying any F05 bytes after the reward state unchanged; returns the budget, the
/// section length and the validated reserved state.
#[allow(clippy::too_many_arguments)]
fn reserve<'o>(
    section: &[u8],
    rewards: Option<RewardState<'_>>,
    policy: &TaskPolicyV1,
    epoch: u64,
    roster: RosterDigest,
    workers: &[WorkerRosterEntry],
    height: u64,
    out: &'o mut [u8],
) -> CodecResult<(Amount, usize, RewardState<'o>)> {
    let rewards = rewards.ok_or(READINESS_BLOCKED)?;
    let tail = section.get(REWARD_STATE_BYTES..).ok_or(NON_CANONICAL)?;
    let budget = epoch_budget(policy, rewards.ledger()?.free)?;
    let (head, rest) = out
        .split_at_mut_checked(REWARD_STATE_BYTES)
        .ok_or(CAPACITY)?;
    let (reserved, _) = rewards.reserve_epoch(epoch, budget, roster, workers, height, head)?;
    rest.get_mut(..tail.len())
        .ok_or(CAPACITY)?
        .copy_from_slice(tail);
    Ok((budget, section.len(), reserved))
}

/// The frozen worker and evaluator rosters of one opening and its core roster digest.
struct FrozenRoster {
    workers: [WorkerRosterEntry; MAX_WORKERS],
    worker_count: usize,
    evaluator_count: usize,
    identity_len: usize,
    digest: RosterDigest,
}
impl FrozenRoster {
    fn workers(&self) -> &[WorkerRosterEntry] {
        &self.workers[..self.worker_count]
    }
}

/// Freezes the rosters over the rolled-over sections: F03 snapshot into `identity_out`,
/// independence, readiness and the core roster hash.
fn freeze(
    sections: &[&[u8]; 5],
    selected: &PolicySection<'_>,
    epoch: u64,
    height: u64,
    identity_out: &mut [u8],
) -> CodecResult<FrozenRoster> {
    let admission = AdmissionTable::decode(sections[Section::ReputationAdmission.index()])?;
    let identity = sections[Section::IdentityRoster.index()];
    let workers = WorkerTable::decode(authority::split_identity_section(identity)?.0)?;
    let (worker_roster, worker_count) = worker_roster(&admission, &workers, height)?;
    let frozen_workers = &worker_roster[..worker_count];
    let snapshot = authority::open_epoch(
        identity,
        &SnapshotContext {
            market: &selected.header,
            epoch,
            approved_rubric: selected.current.commitments.rubric,
            workers: frozen_workers,
            admission: &admission,
        },
        identity_out,
    )?;
    let identity_next = identity_out.get(..snapshot.section_len).ok_or(CAPACITY)?;
    let (evaluators, evaluator_count) = evaluator_roster(identity_next, epoch)?;
    let frozen_evaluators = &evaluators[..evaluator_count];
    check_independence(
        selected.header.owner_principal,
        frozen_workers,
        frozen_evaluators,
    )?;
    check_readiness(&selected.current, worker_count, evaluator_count)?;
    let digest = codec::roster_digest(&Roster {
        market: selected.header.market_id,
        epoch,
        config: Version::new(selected.header.active_config_version)?,
        workers: frozen_workers,
        evaluators: frozen_evaluators,
    })?;
    Ok(FrozenRoster {
        workers: worker_roster,
        worker_count,
        evaluator_count,
        identity_len: snapshot.section_len,
        digest,
    })
}

/// The segment binding of `section`'s active config and policy.
fn segment_binding(section: &PolicySection<'_>) -> CodecResult<SegmentBinding> {
    Ok(SegmentBinding {
        config: Version::new(section.header.active_config_version)?,
        policy: Digest32::new(section.current.digest()?.bytes())?,
        model: section.current.commitments.model_artifact,
    })
}

/// The F07 phase of one opening: segment rollover from the committed binding to the selected
/// one, retirement of workers that left the F02 table, bootstrap of the frozen workers, and
/// release of the completed summaries the reserved F06 ring (`rewards`) no longer retains.
fn open_reputation(
    region: &Region,
    committed: &PolicySection<'_>,
    selected: &PolicySection<'_>,
    sections: &[&[u8]; 5],
    roster: &[WorkerRosterEntry],
    rewards: &RewardState<'_>,
    height: u64,
) -> CodecResult<Region> {
    // A seal lives only between the F05 seal and completion of the opened epoch, which is
    // terminal before any later opening.
    if region.seal != Presence::Absent {
        return Err(NON_CANONICAL);
    }
    let identity = sections[Section::IdentityRoster.index()];
    let workers = WorkerTable::decode(split_identity_section(identity)?.0)?;
    let departed = |worker: WorkerId| !matches!(workers.get(worker), Some(record) if record.state != WorkerState::Retired);
    let opened = open_segments(
        &region.state,
        segment_binding(committed)?,
        segment_binding(selected)?,
        roster,
        departed,
        height,
    )?;
    let state = prune_released(&opened, |epoch| match rewards.row(epoch) {
        Ok(_) => Ok(true),
        Err(NOT_FOUND) => Ok(false),
        Err(error) => Err(error),
    })?;
    if state.completed().count() == LIMIT {
        return Err(RETENTION_FULL);
    }
    Ok(Region {
        state,
        seal: Presence::Absent,
    })
}

struct Composed {
    frozen: Frozen,
    revision: u64,
    state_len: usize,
}

/// The whole opening of the clock epoch of `height` over `current`, written into `next`.
fn compose(
    current: &[u8],
    height: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<Composed> {
    let (state, section) = committed(current)?;
    let epoch = opening_epoch(&section.header, height)?;
    let (selected, policy_activated) = select_policy(&section, epoch)?;
    let (rolled, rest) = scratch
        .split_at_mut_checked(MAX_STATE_BYTES)
        .ok_or(CAPACITY)?;
    let (work, rest) = rest
        .split_at_mut_checked(ROLLOVER_SCRATCH_BYTES)
        .ok_or(CAPACITY)?;
    let (identity_out, rest) = rest.split_at_mut_checked(IDENTITY_CAP).ok_or(CAPACITY)?;
    let (settlement_out, rest) = rest.split_at_mut_checked(SETTLEMENT_CAP).ok_or(CAPACITY)?;
    let (policy_out, rest) = rest.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let (control_out, rest) = rest.split_at_mut_checked(CONTROL_CAP).ok_or(CAPACITY)?;
    let (stripped_out, joint_out) = rest.split_at_mut_checked(MAX_STATE_BYTES).ok_or(CAPACITY)?;
    let (region, table) = split_joint(state.feature_sections[ADMISSION])?;
    let source: &[u8] = if region.is_some() {
        let stripped = state.replace_section(Section::ReputationAdmission, table)?;
        let len = encode_shared_state(&stripped, stripped_out, control_out)?;
        stripped_out.get(..len).ok_or(CAPACITY)?
    } else {
        current
    };
    let (opened, settlement, rewards) =
        roster::rollover_roster_with(source, height, state.revision, rolled, work)?;
    let rolled = decode_shared_state(rolled.get(..opened.state_len).ok_or(CAPACITY)?)?;
    let sections = rolled.feature_sections;
    let frozen = freeze(&sections, &selected, epoch, height, identity_out)?;
    let (budget, settlement_len, reserved) = reserve(
        settlement,
        rewards,
        &selected.current,
        epoch,
        frozen.digest,
        frozen.workers(),
        height,
        settlement_out,
    )?;
    let admission_next = match region {
        None => sections[ADMISSION],
        Some(region) => {
            let reputation = open_reputation(
                &region,
                &section,
                &selected,
                &sections,
                frozen.workers(),
                &reserved,
                height,
            )?;
            let len = encode_joint(&reputation, sections[ADMISSION], joint_out)?;
            joint_out.get(..len).ok_or(CAPACITY)?
        }
    };
    let revision = state.revision.checked_add(1).ok_or(ARITHMETIC)?;
    let mut next_policy = selected;
    next_policy.task_region = tasks::region_after_open(selected.task_region)?;
    next_policy.header.state_revision = revision;
    let policy_len = next_policy.encode(policy_out)?;
    let next_state = SharedState {
        revision,
        feature_sections: [
            &policy_out[..policy_len],
            &identity_out[..frozen.identity_len],
            sections[Section::CurrentReports.index()],
            &settlement_out[..settlement_len],
            admission_next,
        ],
        control: rolled.control.clone(),
    };
    check_f01_capacity(policy_len, next_state.encoded_len()?)?;
    let state_len = encode_shared_state(&next_state, next, control_out)?;
    Ok(Composed {
        frozen: Frozen {
            epoch,
            previous: opened.previous,
            skipped: opened.skipped,
            config: Version::new(selected.header.active_config_version)?,
            policy: selected.current.digest()?,
            policy_activated,
            roster: frozen.digest,
            budget,
            workers: u8::try_from(frozen.worker_count).map_err(|_| ARITHMETIC)?,
            evaluators: u8::try_from(frozen.evaluator_count).map_err(|_| ARITHMETIC)?,
        },
        revision,
        state_len,
    })
}

/// The binding an `OPEN_EPOCH` at `height` would freeze over `current`, so a keeper can
/// name the frozen config and roster digest in its envelope. Nothing is committed; `next`
/// (at least `MAX_STATE_BYTES`) and `scratch` (at least [`OPEN_SCRATCH_BYTES`]) are working
/// buffers only.
///
/// # Errors
/// Every refusal [`open_epoch`] returns from the composed opening.
pub fn preview_open(
    current: &[u8],
    height: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<Frozen> {
    Ok(compose(current, height, next, scratch)?.frozen)
}

fn check_roster(
    claimed: Presence<RosterDigest>,
    epoch: u64,
    frozen: RosterDigest,
) -> CodecResult<()> {
    match claimed {
        Presence::Present(roster) if roster == frozen => Ok(()),
        Presence::Absent if epoch == 0 => Ok(()),
        _ => Err(WRONG_ROSTER),
    }
}

/// Object-local repeat: the clock epoch is already open with an F06 reservation.
fn already_opened(
    state: &SharedState<'_>,
    header: &MarketHeader,
    e: &Envelope<'_>,
    epoch: u64,
) -> CodecResult<Option<RosterDigest>> {
    let admission = AdmissionTable::decode(split_joint(state.feature_sections[ADMISSION])?.1)?;
    if admission.current_epoch() != Some(epoch) {
        return Ok(None);
    }
    let section = state.feature_sections[Section::SettlementClaims.index()];
    if section.is_empty() {
        return Err(WRONG_EPOCH);
    }
    let rewards = decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NON_CANONICAL)?)?;
    let row = match rewards.row(epoch) {
        Err(NOT_FOUND) => return Err(WRONG_EPOCH),
        row => row?,
    };
    if e.config != header.active_config_version {
        return Err(WRONG_CONFIG);
    }
    check_roster(e.roster, epoch, row.roster)?;
    Ok(Some(row.roster))
}

fn open_suffix(f: &Frozen) -> CodecResult<[u8; OPEN_SUFFIX_BYTES]> {
    let mut suffix = [0; OPEN_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.put(f.policy.as_bytes())?;
    w.put(f.roster.as_bytes())?;
    w.u128(f.budget)?;
    w.u8(f.workers)?;
    w.u8(f.evaluators)?;
    w.boolean(f.policy_activated)?;
    w.boolean(f.previous.is_some())?;
    w.u64(f.previous.unwrap_or(0))?;
    w.u64(f.skipped)?;
    Ok(suffix)
}

/// `OPEN_EPOCH` (0x0111): permissionless, object-local (`actor_sequence = 0`, no replay
/// record). Opens the clock epoch of the authenticated `ctx.height` over the committed
/// `current` state and writes the whole next state into `next` (at least
/// `MAX_STATE_BYTES`) and its event into `event`. `scratch` holds at least
/// [`OPEN_SCRATCH_BYTES`]. Dispatch arm:
/// `dispatch::OPEN_EPOCH => epoch::open_epoch(&ctx, &envelope, current, next, scratch, event)`.
///
/// # Errors
/// `UNKNOWN_OPERATION`; envelope principal, domain and expiry refusals; `WRONG_MARKET`;
/// `NON_CANONICAL` for a non-empty payload or an inconsistent committed state;
/// `F08_MARKET_PAUSED` while suspended, `F01_LIFECYCLE_CLOSED` once closing; `WRONG_EPOCH`
/// before origin or for an envelope epoch other than the clock epoch; `ARITHMETIC` when the
/// epoch windows overflow; `WRONG_PHASE` outside Work or while the previous epoch is not
/// terminal; `WRONG_CONFIG`/`WRONG_ROSTER` for an envelope that does not bind the frozen
/// config and roster; `ROLE_CONFLICT`; `READINESS_BLOCKED`; `INSUFFICIENT_FREE`;
/// `ACCOUNT_BINDING`; and every rollover, F03 snapshot, F06 reserve, capacity and codec
/// refusal. On any error `current` is unchanged and the outputs must be discarded.
pub fn open_epoch(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let e = &envelope.envelope;
    if e.operation != dispatch::OPEN_EPOCH {
        return Err(UNKNOWN_OPERATION);
    }
    let (state, section) = committed(current)?;
    authenticate(ctx, envelope, &section.header)?;
    if !e.payload.is_empty() {
        return Err(NON_CANONICAL);
    }
    let epoch = opening_epoch(&section.header, ctx.height)?;
    if e.epoch != epoch {
        return Err(WRONG_EPOCH);
    }
    if let Some(roster) = already_opened(&state, &section.header, e, epoch)? {
        return Ok(Outcome::AlreadyApplied { epoch, roster });
    }
    let composed = compose(current, ctx.height, next, scratch)?;
    let frozen = composed.frozen;
    if e.config != frozen.config.get() {
        return Err(WRONG_CONFIG);
    }
    check_roster(e.roster, epoch, frozen.roster)?;
    let suffix = open_suffix(&frozen)?;
    let result = codec::result_digest(&suffix)?;
    let event_len = codec::encode_event_frame(
        dispatch::OPEN_EPOCH,
        &EventCommon {
            market: section.header.market_id,
            epoch,
            config: frozen.config,
            revision: composed.revision,
            request: envelope.request_digest()?,
            result,
        },
        &suffix,
        event,
    )?;
    Ok(Outcome::Opened {
        frozen,
        revision: composed.revision,
        result,
        state_len: composed.state_len,
        event_len,
    })
}

/// Readiness of an epoch that is already open: its reserved F06 row and the F03 snapshot
/// without evaluators revoked since.
fn opened_readiness(
    state: &SharedState<'_>,
    policy: &TaskPolicyV1,
    epoch: u64,
) -> CodecResult<Readiness> {
    let section = state.feature_sections[Section::SettlementClaims.index()];
    let rewards = decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(READINESS_BLOCKED)?)?;
    let row = rewards.row(epoch)?;
    if row.status != EpochStatus::Reserved {
        return Err(READINESS_BLOCKED);
    }
    if row.budget < policy.minimum_epoch_funding {
        return Err(INSUFFICIENT_FREE);
    }
    let region =
        authority::evaluator_region(state.feature_sections[Section::IdentityRoster.index()])?;
    let snapshot = region
        .snapshot()
        .filter(|s| s.epoch == epoch)
        .ok_or(READINESS_BLOCKED)?;
    let evaluators = snapshot
        .entries()
        .filter(|f| !region.excluded(f.entry.evaluator))
        .count();
    let workers = row.entries().len();
    check_readiness(policy, workers, evaluators)?;
    Ok(Readiness {
        epoch,
        workers,
        evaluators,
        budget: row.budget,
    })
}

/// Writes the activated state (lifecycle ACTIVE, schedule cleared, one revision increment)
/// into `next`, then proves readiness against it: an already opened clock epoch must still
/// be ready, otherwise a complete trial opening must succeed. The trial is discarded.
fn probe(
    current: &[u8],
    height: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<(Readiness, u64, usize)> {
    let (state, section) = committed(current)?;
    let (policy_out, rest) = scratch.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let (control_out, rest) = rest.split_at_mut_checked(CONTROL_CAP).ok_or(CAPACITY)?;
    let (trial, open_scratch) = rest.split_at_mut_checked(MAX_STATE_BYTES).ok_or(CAPACITY)?;
    let revision = state.revision.checked_add(1).ok_or(ARITHMETIC)?;
    let mut active = section;
    active.header.lifecycle = ACTIVE;
    active.header.activation_scheduled = false;
    active.header.activation_epoch = 0;
    active.header.state_revision = revision;
    let policy_len = active.encode(policy_out)?;
    let mut feature_sections = state.feature_sections;
    feature_sections[Section::PolicyLifecycle.index()] = &policy_out[..policy_len];
    let candidate = SharedState {
        revision,
        feature_sections,
        control: state.control.clone(),
    };
    check_f01_capacity(policy_len, candidate.encoded_len()?)?;
    let state_len = encode_shared_state(&candidate, next, control_out)?;
    let epoch = work_epoch(&active.header, height)?;
    let admission = AdmissionTable::decode(split_joint(state.feature_sections[ADMISSION])?.1)?;
    let readiness = if admission.current_epoch() == Some(epoch) {
        opened_readiness(&state, &active.current, epoch)?
    } else {
        let candidate = next.get(..state_len).ok_or(CAPACITY)?;
        let trial = compose(candidate, height, trial, open_scratch)?.frozen;
        Readiness {
            epoch,
            workers: usize::from(trial.workers),
            evaluators: usize::from(trial.evaluators),
            budget: trial.budget,
        }
    };
    Ok((readiness, revision, state_len))
}

/// The stable reason behind `ActivationNotReady`: readiness of the clock epoch of `height`
/// as `ADVANCE_ACTIVATION` evaluates it. `next` (at least `MAX_STATE_BYTES`) and `scratch`
/// (at least [`ADVANCE_SCRATCH_BYTES`]) are working buffers only.
///
/// # Errors
/// `READINESS_BLOCKED`, `INSUFFICIENT_FREE`, `ROLE_CONFLICT`, `ACCOUNT_BINDING`,
/// `WRONG_PHASE` and every refusal of a trial opening.
pub fn activation_readiness(
    current: &[u8],
    height: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<Readiness> {
    Ok(probe(current, height, next, scratch)?.0)
}

fn parse_advance(payload: &[u8]) -> CodecResult<(u64, u64)> {
    let mut r = Reader::new(payload);
    let expected = (r.u64()?, r.u64()?);
    r.finish()?;
    Ok(expected)
}

/// `ADVANCE_ACTIVATION` (0x0105): anyone, object-local, payload
/// `expected_state_revision:u64 || expected_activation_epoch:u64`. Only during Work of a
/// clock epoch at or after the scheduled epoch; on success the lifecycle becomes ACTIVE at
/// this committed height with one revision increment. It opens no epoch and books no budget.
/// Dispatch arm: `dispatch::ADVANCE_ACTIVATION => epoch::advance_activation(&ctx,
/// &envelope, current, next, scratch, event)`.
///
/// # Errors
/// `UNKNOWN_OPERATION`; envelope refusals; `NON_CANONICAL` for a malformed payload;
/// `WRONG_MARKET`; `WRONG_CONFIG`; `F01_ALREADY_ACTIVATED`; `F01_LIFECYCLE_CLOSED`;
/// `NOT_FOUND` without a schedule; `F01_STALE_REVISION`; `CONFLICT` for another scheduled
/// epoch; `WRONG_EPOCH`/`ARITHMETIC`/`WRONG_PHASE` from the clock;
/// `F01_ACTIVATION_TOO_EARLY`; `F01_ACTIVATION_NOT_READY` for any failed prerequisite (see
/// [`activation_readiness`]). The schedule, funds and `current` are unchanged on refusal.
pub fn advance_activation(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Activated> {
    let e = &envelope.envelope;
    if e.operation != dispatch::ADVANCE_ACTIVATION {
        return Err(UNKNOWN_OPERATION);
    }
    let (expected_revision, expected_epoch) = parse_advance(e.payload)?;
    let (state, section) = committed(current)?;
    let header = section.header;
    authenticate(ctx, envelope, &header)?;
    if e.config != header.active_config_version {
        return Err(WRONG_CONFIG);
    }
    match header.lifecycle {
        REGISTERED | SUSPENDED => {}
        ACTIVE => return Err(F01_ALREADY_ACTIVATED),
        _ => return Err(F01_LIFECYCLE_CLOSED),
    }
    if !header.activation_scheduled {
        return Err(NOT_FOUND);
    }
    if expected_revision != state.revision {
        return Err(F01_STALE_REVISION);
    }
    if expected_epoch != header.activation_epoch {
        return Err(CONFLICT);
    }
    let epoch = work_epoch(&header, ctx.height)?;
    if epoch < header.activation_epoch {
        return Err(F01_ACTIVATION_TOO_EARLY);
    }
    let (readiness, revision, state_len) =
        probe(current, ctx.height, next, scratch).map_err(|_| F01_ACTIVATION_NOT_READY)?;
    let mut suffix = [0; ACTIVATION_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.u64(header.activation_epoch)?;
    w.u64(epoch)?;
    w.u64(ctx.height)?;
    let result = codec::result_digest(&suffix)?;
    let event_len = codec::encode_event_frame(
        dispatch::ADVANCE_ACTIVATION,
        &EventCommon {
            market: header.market_id,
            epoch,
            config: Version::new(header.active_config_version)?,
            revision,
            request: envelope.request_digest()?,
            result,
        },
        &suffix,
        event,
    )?;
    Ok(Activated {
        readiness,
        activation_epoch: header.activation_epoch,
        revision,
        result,
        state_len,
        event_len,
    })
}

/// Runs `produce` over `current` without its F07 region and joins `update`'s region back into
/// the produced state. `produce` receives the region-free state and `next`, and returns its
/// outcome with the written state length (`None` when it wrote nothing). Without an F07 region
/// `produce` runs over `current` unchanged. The returned length is the length in `next`.
fn with_region<T>(
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    produce: impl FnOnce(&[u8], &mut [u8]) -> CodecResult<(T, Option<usize>)>,
    update: impl FnOnce(&Region, &SharedState<'_>, &SharedState<'_>, &T) -> CodecResult<Region>,
) -> CodecResult<(T, Option<usize>)> {
    let state = decode_shared_state(current)?;
    let (region, table) = split_joint(state.feature_sections[ADMISSION])?;
    let Some(region) = region else {
        return produce(current, next);
    };
    let (stripped, rest) = scratch
        .split_at_mut_checked(MAX_STATE_BYTES)
        .ok_or(CAPACITY)?;
    let (joint, control) = rest.split_at_mut_checked(JOINT_OUT).ok_or(CAPACITY)?;
    let without = state.replace_section(Section::ReputationAdmission, table)?;
    let len = encode_shared_state(&without, stripped, control)?;
    let (value, written) = produce(stripped.get(..len).ok_or(CAPACITY)?, next)?;
    let Some(written) = written else {
        return Ok((value, None));
    };
    let produced = stripped.get_mut(..written).ok_or(CAPACITY)?;
    produced.copy_from_slice(next.get(..written).ok_or(CAPACITY)?);
    let produced = decode_shared_state(produced)?;
    let (again, table) = split_joint(produced.feature_sections[ADMISSION])?;
    if again.is_some() {
        return Err(NON_CANONICAL);
    }
    let region = update(&region, &state, &produced, &value)?;
    let joint_len = encode_joint(&region, table, joint)?;
    let candidate = produced.replace_section(
        Section::ReputationAdmission,
        joint.get(..joint_len).ok_or(CAPACITY)?,
    )?;
    check_f01_capacity(
        candidate.feature_sections[Section::PolicyLifecycle.index()].len(),
        candidate.encoded_len()?,
    )?;
    let state_len = encode_shared_state(&candidate, next, control)?;
    Ok((value, Some(state_len)))
}

/// Runs any producer that reads the F08 table only (F02, F03, F04, F06, F08, F09, F10, F01
/// tasks and registry operations) over `current` without its F07 region, then carries the
/// region byte-identical into the produced state. `produce(current, next)` returns its
/// outcome and the state length it wrote into `next` (`None` when it wrote nothing); the
/// returned length replaces it. `scratch` holds at least [`CARRY_SCRATCH_BYTES`] and must not
/// alias the producer's own scratch. Without an F07 region `produce` runs over `current`.
///
/// # Errors
/// Propagates `produce`'s refusals; `NON_CANONICAL` when the produced joint section carries an
/// F07 region; the joint split/encode, capacity and state codec refusals. On any error
/// `current` is unchanged and the outputs must be discarded.
pub fn carry_reputation<T>(
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    produce: impl FnOnce(&[u8], &mut [u8]) -> CodecResult<(T, Option<usize>)>,
) -> CodecResult<(T, Option<usize>)> {
    with_region(current, next, scratch, produce, |region, _, _, _| {
        Ok(region.clone())
    })
}

/// The ascending frozen worker roster of `epoch`'s F06 row, rebuilt like F05 does: the row's
/// worker and recipient with the live F02 attributes.
fn frozen_workers(
    state: &SharedState<'_>,
    rewards: &RewardState<'_>,
    row: &RewardEpoch,
) -> CodecResult<([WorkerRosterEntry; MAX_WORKERS], usize)> {
    let identity = state.feature_sections[Section::IdentityRoster.index()];
    let live = WorkerTable::decode(split_identity_section(identity)?.0)?;
    let dictionary = rewards.dictionary();
    let mut found = [None; MAX_WORKERS];
    for (slot, entry) in found.iter_mut().zip(row.entries()) {
        let frozen = dictionary.slot(entry.slot)?;
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
    let count = row.entries().len();
    let first = found[0].ok_or(WRONG_ROSTER)?;
    let mut roster = [first; MAX_WORKERS];
    for (slot, entry) in roster.iter_mut().zip(found.iter().flatten()) {
        *slot = *entry;
    }
    roster
        .get_mut(..count)
        .ok_or(CAPACITY)?
        .sort_unstable_by_key(|e| e.worker);
    Ok((roster, count))
}

/// The opened epoch's F06 row in `state`.
fn epoch_row<'a>(
    state: &SharedState<'a>,
    epoch: u64,
) -> CodecResult<(RewardState<'a>, RewardEpoch)> {
    let settlement = state.feature_sections[Section::SettlementClaims.index()];
    let rewards = decode_reward_state(settlement.get(..REWARD_STATE_BYTES).ok_or(WRONG_EPOCH)?)?;
    let row = rewards.row(epoch)?;
    Ok((rewards, row))
}

/// `history_allowed` frozen at the F05 seal of `epoch` from the sealed `produced` state.
fn seal_region(region: &Region, produced: &SharedState<'_>, epoch: u64) -> CodecResult<Region> {
    if region.seal != Presence::Absent {
        return Err(NON_CANONICAL);
    }
    let (rewards, row) = epoch_row(produced, epoch)?;
    let (roster, count) = frozen_workers(produced, &rewards, &row)?;
    let mut workers = [roster[0].worker; MAX_WORKERS];
    for (slot, entry) in workers.iter_mut().zip(roster.get(..count).ok_or(CAPACITY)?) {
        *slot = entry.worker;
    }
    let admission = AdmissionTable::decode(produced.feature_sections[ADMISSION])?;
    let identity =
        authority::evaluator_region(produced.feature_sections[Section::IdentityRoster.index()])?;
    let snapshot = identity
        .snapshot()
        .filter(|s| s.epoch == epoch)
        .ok_or(NON_CANONICAL)?;
    let eligible = snapshot
        .entries()
        .filter(|f| {
            let evaluator = f.entry.evaluator;
            identity
                .get(evaluator)
                .is_some_and(|r| r.grant.status != GrantStatus::Revoked)
                && !identity.excluded(evaluator)
                && admission
                    .get(Participant::Evaluator(evaluator))
                    .is_some_and(|m| !m.revoked())
        })
        .count();
    let seal = seal_history(
        &region.state,
        epoch,
        workers.get(..count).ok_or(CAPACITY)?,
        u8::try_from(eligible).map_err(|_| CAPACITY)?,
    )?;
    Ok(Region {
        state: region.state.clone(),
        seal: Presence::Present(seal),
    })
}

/// Byte length of the F05 current record at the start of `tail`.
fn f05_record_len(tail: &[u8]) -> CodecResult<usize> {
    let mut r = Reader::new(tail);
    r.u8()?;
    r.u64()?;
    r.take(32)?;
    r.u16()?;
    let reports = usize::from(r.u16()?);
    r.take(reports.checked_mul(SEALED_REPORT_BYTES).ok_or(ARITHMETIC)?)?;
    let outputs = usize::from(r.u16()?);
    r.take(
        outputs
            .checked_mul(WORKER_AGGREGATE_BYTES)
            .ok_or(ARITHMETIC)?,
    )?;
    r.u64()?;
    if r.boolean()? {
        r.take(32)?;
    }
    Ok(r.offset())
}

/// `ApplyCompletedEpoch` inside the F05 terminal transition: the frozen roster from the reserved
/// F06 row `before` terminalization (a no-score terminal releases its slots), every frozen
/// worker's sealed F05 output from the terminalized `produced` state under the sealed `history_allowed` bits, one
/// completed summary bound to the aggregate `root`, and the seal cleared.
fn complete_region(
    region: &Region,
    before: &SharedState<'_>,
    produced: &SharedState<'_>,
    epoch: u64,
    root: Digest32,
    height: u64,
) -> CodecResult<Region> {
    let section =
        PolicySection::decode(produced.feature_sections[Section::PolicyLifecycle.index()])?;
    let header = &section.header;
    let (rewards, row) = epoch_row(before, epoch)?;
    let frozen = FrozenBinding {
        chain: header.deployment_chain_domain,
        program: header.program_id,
        market: header.market_id,
        epoch,
        config: Version::new(header.active_config_version)?,
        roster: row.roster,
    };
    let (roster, count) = frozen_workers(before, &rewards, &row)?;
    let roster = roster.get(..count).ok_or(CAPACITY)?;
    let settlement = produced.feature_sections[Section::SettlementClaims.index()];
    let tail = settlement.get(REWARD_STATE_BYTES..).ok_or(NON_CANONICAL)?;
    let record = tail.get(..f05_record_len(tail)?).ok_or(NON_CANONICAL)?;
    let current = decode_current(record, frozen, roster)?;
    if current.output_count() != count {
        return Err(F07_BINDING_MISMATCH);
    }
    let mut observations = [WorkerObservation {
        median: Presence::Absent,
        support: 0,
    }; MAX_WORKERS];
    for (i, (slot, entry)) in observations.iter_mut().zip(roster).enumerate() {
        let output = current.output(i)?;
        if output.worker() != entry.worker {
            return Err(F07_BINDING_MISMATCH);
        }
        *slot = WorkerObservation {
            median: output.quality(),
            support: output.support(),
        };
    }
    let (state, _) = complete_epoch(
        &region.state,
        region.seal,
        frozen,
        segment_binding(&section)?,
        root,
        height,
        roster,
        observations.get(..count).ok_or(CAPACITY)?,
    )?;
    Ok(Region {
        state,
        seal: Presence::Absent,
    })
}

/// `BeginAggregation`/`ProcessAggregation`/`FinalizeAggregation` (0x0501-0x0503) with the F07
/// phases of the same atomic transition: Begin freezes `history_allowed` and the eligible
/// evaluator count beside the F05 seal; Process carries the region; Finalize runs
/// `ApplyCompletedEpoch` over the sealed F05 outputs in the F05/F06 terminal transition, so
/// a refused F07 phase refuses the whole terminalization. `scratch` holds at least
/// [`AGGREGATE_SCRATCH_BYTES`]. Dispatch arm: `dispatch::BeginAggregation |
/// dispatch::ProcessAggregation | dispatch::FinalizeAggregation => epoch::aggregate(&ctx,
/// &envelope, current, next, scratch, event)`. Without an F07 region this is
/// `aggregation::apply`.
///
/// # Errors
/// Every `aggregation::apply` refusal; `NON_CANONICAL` for a second seal of an open epoch;
/// `F07_EPOCH_NOT_SEALED` for a completion without its seal; `F07_BINDING_MISMATCH`,
/// `F07_UNKNOWN_WORKER`, `RETENTION_FULL`, `WRONG_EPOCH` and `F07_RESOURCE_LIMIT` from the F07
/// completion; capacity and codec refusals. On any error `current` is unchanged and the outputs
/// must be discarded.
pub fn aggregate(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Aggregated> {
    let operation = envelope.envelope.operation;
    let (inner, carry) = scratch
        .split_at_mut_checked(aggregation::SCRATCH_BYTES)
        .ok_or(CAPACITY)?;
    let (outcome, written) = with_region(
        current,
        next,
        carry,
        |current, next| {
            let outcome = aggregation::apply(ctx, envelope, current, next, inner, event)?;
            let written = match outcome {
                Aggregated::Applied { state_len, .. } => Some(state_len),
                Aggregated::AlreadyApplied { .. } => None,
            };
            Ok((outcome, written))
        },
        |region, before, produced, outcome| {
            let Aggregated::Applied { progress, .. } = *outcome else {
                return Err(NON_CANONICAL);
            };
            if operation == dispatch::BeginAggregation {
                seal_region(region, produced, progress.epoch)
            } else if operation == dispatch::FinalizeAggregation {
                let Presence::Present(root) = progress.root else {
                    return Err(NON_CANONICAL);
                };
                complete_region(region, before, produced, progress.epoch, root, ctx.height)
            } else {
                Ok(region.clone())
            }
        },
    )?;
    Ok(match (outcome, written) {
        (
            Aggregated::Applied {
                progress,
                response,
                revision,
                result,
                event_len,
                ..
            },
            Some(state_len),
        ) => Aggregated::Applied {
            progress,
            response,
            revision,
            result,
            state_len,
            event_len,
        },
        (outcome, _) => outcome,
    })
}

/// Outcome of one `ResetHistory`/`SuspendHistory`/`ResumeHistory`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum HistoryOutcome {
    /// One revision increment composed into `next`, the retained result recorded under the
    /// actor's sequence and the `HistoryReset`/`HistoryStatus` event written into `event`.
    Applied {
        record: ReputationCurrent,
        revision: u64,
        result: ResultDigest,
        state_len: usize,
        event_len: usize,
    },
    /// Exact repetition of an applied request: its retained result; nothing was written.
    Retained(RetainedResult),
}

/// `ResetHistory`/`SuspendHistory`/`ResumeHistory` payload.
struct HistoryRequest {
    worker: WorkerId,
    segment: Digest32,
    generation: u64,
    reason: u8,
}

fn parse_history(payload: &[u8]) -> CodecResult<HistoryRequest> {
    if payload.len() != HISTORY_PAYLOAD_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(payload);
    let request = HistoryRequest {
        worker: WorkerId::new(r.fixed()?)?,
        segment: Digest32::new(r.fixed()?)?,
        generation: r.u64()?,
        reason: r.u8()?,
    };
    r.finish()?;
    Ok(request)
}

/// The authorized principal and replay slot of a history request: the worker owner for
/// `OWNER_RESET`, the market owner otherwise.
fn history_authority(
    operation: dispatch::Operation,
    request: &HistoryRequest,
    header: &MarketHeader,
    identity: &[u8],
) -> CodecResult<(PrincipalId, ActorSlot)> {
    let market_owner = (header.owner_principal, ActorSlot::OWNER);
    if operation == dispatch::ResetHistory {
        if ClosureReason::decode(request.reason)? != ClosureReason::OwnerReset {
            return Ok(market_owner);
        }
        let workers = WorkerTable::decode(split_identity_section(identity)?.0)?;
        let record = workers.get(request.worker).ok_or(F07_UNKNOWN_WORKER)?;
        return Ok((record.owner, ActorSlot::worker(usize::from(record.slot))?));
    }
    let valid = if operation == dispatch::SuspendHistory {
        matches!(request.reason, 1 | 2)
    } else {
        request.reason == 1
    };
    if valid {
        Ok(market_owner)
    } else {
        Err(NON_CANONICAL)
    }
}

/// The authorized role-sequenced replay request of a history call, or the retained result of
/// its exact repetition.
fn history_replay(
    envelope: &ValidatedEnvelope<'_>,
    state: &SharedState<'_>,
    header: &MarketHeader,
    body: &HistoryRequest,
    height: u64,
) -> CodecResult<Result<ReplayRequest, RetainedResult>> {
    let e = &envelope.envelope;
    let identity = state.feature_sections[Section::IdentityRoster.index()];
    let (principal, slot) = history_authority(e.operation, body, header, identity)?;
    if e.actor != principal {
        return Err(UNAUTHORIZED);
    }
    let actor = state.control.replay.actor(slot).ok_or(UNAUTHORIZED)?;
    if actor.principal != principal {
        return Err(UNAUTHORIZED);
    }
    let request = ReplayRequest::from_envelope(slot, actor.authority_version, envelope)?;
    match state.control.replay.check(&request, height) {
        Ok(ReplayDecision::AlreadyApplied(retained)) => Ok(Err(retained)),
        Ok(ReplayDecision::Apply) => Ok(Ok(request)),
        Err(REPLAY_CONFLICT) => Err(F07_IDEMPOTENCY_CONFLICT),
        Err(error) => Err(error),
    }
}

/// Refuses a reset while the opened, nonterminal epoch's frozen roster binds `worker`.
fn check_not_frozen(state: &SharedState<'_>, table: &[u8], worker: WorkerId) -> CodecResult<()> {
    let Some(epoch) = AdmissionTable::decode(table)?.current_epoch() else {
        return Ok(());
    };
    let (rewards, row) = epoch_row(state, epoch)?;
    if row.status != EpochStatus::Reserved {
        return Ok(());
    }
    let dictionary = rewards.dictionary();
    for entry in row.entries() {
        if dictionary.slot(entry.slot)?.worker == worker {
            return Err(F07_IDENTITY_FROZEN);
        }
    }
    Ok(())
}

/// The record a checked history request produces and its event suffix written into `suffix`:
/// the reset closes the segment into the next generation under `binding`; suspension and
/// resumption change only the status.
fn history_transition(
    operation: dispatch::Operation,
    (record, key): (ReputationCurrent, SegmentKey),
    binding: SegmentBinding,
    body: &HistoryRequest,
    latest: Presence<u64>,
    height: u64,
    suffix: &mut [u8],
) -> CodecResult<(ReputationCurrent, usize)> {
    let mut w = Writer::new(suffix);
    if operation == dispatch::ResetHistory {
        let (_, updated) = reset_segment(
            &record,
            key,
            binding,
            ClosureReason::decode(body.reason)?,
            latest,
            height,
        )?;
        w.put(body.worker.as_bytes())?;
        w.put(record.segment.as_bytes())?;
        w.put(updated.segment.as_bytes())?;
        w.u64(updated.reset_generation.get())?;
        w.u8(body.reason)?;
        return Ok((updated, RESET_SUFFIX_BYTES));
    }
    let status = if operation == dispatch::SuspendHistory {
        HistoryStatus::Suspended
    } else {
        HistoryStatus::Active
    };
    let updated = set_status(&record, key, status, height)?;
    w.put(body.worker.as_bytes())?;
    w.put(updated.segment.as_bytes())?;
    w.u64(updated.reset_generation.get())?;
    w.u8(status as u8)?;
    w.u8(body.reason)?;
    Ok((updated, STATUS_SUFFIX_BYTES))
}

/// `ResetHistory` (0x0701), `SuspendHistory` (0x0702) and `ResumeHistory` (0x0703): role
/// sequenced, payload `worker_id32 || expected_segment_digest32 || expected_reset_generation:u64
/// || reason:u8`. `OWNER_RESET` is the worker owner's (its F02 actor slot); every other reset
/// reason, suspension and resumption are the market owner's. The expected reset generation is
/// checked before the segment digest. A reset refuses while the opened nonterminal epoch's
/// frozen roster binds the worker, closes the segment (closure digest), increments the reset
/// generation and zeroes quality, count and coverage under the current binding, retaining
/// status and owner; suspension and resumption change the status prospectively and never the
/// sealed `history_allowed` of an epoch in aggregation. Rewards, membership and F05 voting
/// are untouched. An exact repetition returns the retained result and writes nothing.
/// `scratch` holds at least [`HISTORY_SCRATCH_BYTES`]. Dispatch arm:
/// `dispatch::ResetHistory | dispatch::SuspendHistory | dispatch::ResumeHistory =>
/// epoch::history(&ctx, &envelope, current, next, scratch, event)`.
///
/// # Errors
/// `UNKNOWN_OPERATION`; envelope principal, domain and expiry refusals; `WRONG_MARKET`;
/// `WRONG_CONFIG`; `NON_CANONICAL` for a malformed payload, reason or committed state;
/// `F07_UNKNOWN_WORKER` without an F07 region or record; `UNAUTHORIZED` for any other actor
/// or a delegate; the replay refusals with `F07_IDEMPOTENCY_CONFLICT` for a reused sequence;
/// `F07_GENERATION_MISMATCH`; `F07_SEGMENT_MISMATCH`; `F07_IDENTITY_FROZEN`; `CONFLICT` for a
/// retired record or an unchanged status; `F07_BINDING_MISMATCH` for a model/policy reason
/// without that change; capacity and codec refusals. On any error `current` is unchanged and
/// the outputs must be discarded.
pub fn history(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<HistoryOutcome> {
    let e = &envelope.envelope;
    if e.operation != dispatch::ResetHistory
        && e.operation != dispatch::SuspendHistory
        && e.operation != dispatch::ResumeHistory
    {
        return Err(UNKNOWN_OPERATION);
    }
    let (state, mut section) = committed(current)?;
    authenticate(ctx, envelope, &section.header)?;
    if e.config != section.header.active_config_version {
        return Err(WRONG_CONFIG);
    }
    let body = parse_history(e.payload)?;
    let (region, table) = split_joint(state.feature_sections[ADMISSION])?;
    let region = region.ok_or(F07_UNKNOWN_WORKER)?;
    let request = match history_replay(envelope, &state, &section.header, &body, ctx.height)? {
        Ok(request) => request,
        Err(retained) => return Ok(HistoryOutcome::Retained(retained)),
    };
    let record = *region
        .state
        .records()
        .find(|r| r.worker == body.worker)
        .ok_or(F07_UNKNOWN_WORKER)?;
    check_expected(&record, body.segment, body.generation)?;
    let binding = segment_binding(&section)?;
    let (_, key) = bound_record(&region.state, body.worker, binding)?;
    if e.operation == dispatch::ResetHistory {
        if record.status == HistoryStatus::Retired {
            return Err(CONFLICT);
        }
        check_not_frozen(&state, table, body.worker)?;
    }
    let mut suffix = [0; RESET_SUFFIX_BYTES];
    let (updated, suffix_len) = history_transition(
        e.operation,
        (record, key),
        binding,
        &body,
        region.state.latest_completed(),
        ctx.height,
        &mut suffix,
    )?;
    let suffix = suffix.get(..suffix_len).ok_or(CAPACITY)?;
    let next_region = Region {
        state: replace_record(&region.state, updated)?,
        seal: region.seal,
    };
    let (policy_out, rest) = scratch.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let (joint_out, control_out) = rest.split_at_mut_checked(JOINT_OUT).ok_or(CAPACITY)?;
    let joint_len = encode_joint(&next_region, table, joint_out)?;
    let revision = state.revision.checked_add(1).ok_or(ARITHMETIC)?;
    section.header.state_revision = revision;
    let policy_len = section.encode(policy_out)?;
    let mut feature_sections = state.feature_sections;
    feature_sections[Section::PolicyLifecycle.index()] =
        policy_out.get(..policy_len).ok_or(CAPACITY)?;
    feature_sections[ADMISSION] = joint_out.get(..joint_len).ok_or(CAPACITY)?;
    let mut candidate = SharedState {
        revision: state.revision,
        feature_sections,
        control: state.control.clone(),
    };
    let result = codec::result_digest(suffix)?;
    if candidate.record_success(&request, ctx.height, result)? != ReplayDecision::Apply
        || candidate.revision != revision
    {
        return Err(NON_CANONICAL);
    }
    check_f01_capacity(policy_len, candidate.encoded_len()?)?;
    let state_len = encode_shared_state(&candidate, next, control_out)?;
    let event_len = codec::encode_event_frame(
        e.operation,
        &EventCommon {
            market: section.header.market_id,
            epoch: e.epoch,
            config: Version::new(section.header.active_config_version)?,
            revision,
            request: envelope.request_digest()?,
            result,
        },
        suffix,
        event,
    )?;
    Ok(HistoryOutcome::Applied {
        record: updated,
        revision,
        result,
        state_len,
        event_len,
    })
}
