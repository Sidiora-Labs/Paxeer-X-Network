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
//! - F07 segment rollover at opening and the F01 task region reset belong to later tasks;
//!   both sections are carried unchanged.
use crate::{
    admission::{AdmissionMeta, AdmissionTable, Participant},
    codec::{
        self, derive_market, Envelope, EventCommon, Reader, Roster, ValidatedEnvelope, Writer,
    },
    dispatch,
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, CONFLICT, F01_ACTIVATION_NOT_READY,
        F01_ACTIVATION_TOO_EARLY, F01_ALREADY_ACTIVATED, F01_LIFECYCLE_CLOSED, F01_STALE_REVISION,
        F08_MARKET_PAUSED, INSUFFICIENT_FREE, NON_CANONICAL, NOT_FOUND, READINESS_BLOCKED,
        ROLE_CONFLICT, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE,
        WRONG_ROSTER,
    },
    evaluators::authority::{self, SnapshotContext},
    policy::TaskPolicyV1,
    registry::{check_f01_capacity, market_clock, MarketHeader},
    registry_ops::{
        activate_pending_policy, CallContext, PolicySection, ACTIVE, REGISTERED, SUSPENDED,
    },
    rewards::{decode_reward_state, EpochStatus, REWARD_STATE_BYTES},
    roster::{self, ROLLOVER_SCRATCH_BYTES},
    state::{decode_shared_state, encode_shared_state, Section, SharedState},
    types::{
        AccountId, Amount, EpochPhase, EvaluatorRosterEntry, PolicyDigest, Presence, PrincipalId,
        ResultDigest, RosterDigest, Version, WorkerRosterEntry,
    },
    workers::{WorkerState, WorkerTable},
    MAX_EVALUATORS, MAX_STATE_BYTES, MAX_WORKERS,
};

const POLICY_CAP: usize = Section::PolicyLifecycle.payload_cap();
const IDENTITY_CAP: usize = Section::IdentityRoster.payload_cap();
const SETTLEMENT_CAP: usize = Section::SettlementClaims.payload_cap();
const CONTROL_CAP: usize = Section::Control.payload_cap();

/// `EpochOpened` suffix: policy digest, roster digest, budget, worker and evaluator counts,
/// policy-activated flag, previous-epoch presence and value, skipped epochs.
pub const OPEN_SUFFIX_BYTES: usize = 100;
/// `MarketActivated` suffix: activation epoch, clock epoch and committed height.
pub const ACTIVATION_SUFFIX_BYTES: usize = 24;
/// Caller scratch for [`open_epoch`]: the rolled-over state, the rollover scratch and the
/// next identity, settlement, policy and control payloads.
pub const OPEN_SCRATCH_BYTES: usize = MAX_STATE_BYTES
    + ROLLOVER_SCRATCH_BYTES
    + IDENTITY_CAP
    + SETTLEMENT_CAP
    + POLICY_CAP
    + CONTROL_CAP;
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

/// F06 `ReserveEpoch` into `out`, carrying any F05 bytes after the reward state unchanged.
fn reserve(
    section: &[u8],
    policy: &TaskPolicyV1,
    epoch: u64,
    roster: RosterDigest,
    workers: &[WorkerRosterEntry],
    height: u64,
    out: &mut [u8],
) -> CodecResult<(Amount, usize)> {
    if section.is_empty() {
        return Err(READINESS_BLOCKED);
    }
    let rewards = decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NON_CANONICAL)?)?;
    let tail = section.get(REWARD_STATE_BYTES..).ok_or(NON_CANONICAL)?;
    let budget = epoch_budget(policy, rewards.ledger()?.free)?;
    let (head, rest) = out
        .split_at_mut_checked(REWARD_STATE_BYTES)
        .ok_or(CAPACITY)?;
    rewards.reserve_epoch(epoch, budget, roster, workers, height, head)?;
    rest.get_mut(..tail.len())
        .ok_or(CAPACITY)?
        .copy_from_slice(tail);
    Ok((budget, section.len()))
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
    let (policy_out, control_out) = rest.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let opened = roster::rollover_roster(current, height, state.revision, rolled, work)?;
    let rolled = decode_shared_state(rolled.get(..opened.state_len).ok_or(CAPACITY)?)?;
    let sections = rolled.feature_sections;
    let frozen = freeze(&sections, &selected, epoch, height, identity_out)?;
    let (budget, settlement_len) = reserve(
        sections[Section::SettlementClaims.index()],
        &selected.current,
        epoch,
        frozen.digest,
        frozen.workers(),
        height,
        settlement_out,
    )?;
    let revision = state.revision.checked_add(1).ok_or(ARITHMETIC)?;
    let mut next_policy = selected;
    next_policy.header.state_revision = revision;
    let policy_len = next_policy.encode(policy_out)?;
    let next_state = SharedState {
        revision,
        feature_sections: [
            &policy_out[..policy_len],
            &identity_out[..frozen.identity_len],
            sections[Section::CurrentReports.index()],
            &settlement_out[..settlement_len],
            sections[Section::ReputationAdmission.index()],
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
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
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
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
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
