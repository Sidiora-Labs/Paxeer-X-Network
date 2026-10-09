//! F08 deterministic roster rollover, inactivity selection and evaluator health.
//! Selection never consults quality history (F07); it uses activity epochs and IDs only.
use crate::{
    admission::{
        AdmissionMeta, AdmissionTable, Participant, Role, FLAG_DRAINING, MAX_MEMBERS, POLICY_V1,
        TABLE_MAX_BYTES,
    },
    codec::{self, Writer},
    errors::{
        CodecResult, ACCOUNT_BINDING, ARITHMETIC, CAPACITY, F08_MARKET_PAUSED,
        F08_NO_PRUNABLE_MEMBER, F08_QUORUM_UNAVAILABLE, F08_RETENTION_BLOCKED, F08_STALE_STATE,
        NON_CANONICAL, WRONG_EPOCH, WRONG_PHASE,
    },
    evaluators::authority::split_identity_section,
    registry::{market_clock, MarketHeader},
    registry_ops::{PolicySection, ACTIVE, REGISTERED, SUSPENDED},
    rewards::{decode_reward_state, Disposition, EpochStatus, RewardState, REWARD_STATE_BYTES},
    state::{self, ActorSlot, Control, Section, SharedState},
    types::{Amount, Digest32, EpochPhase, WorkerId},
    workers::{WorkerState, WorkerTable},
};

/// Bytes per frozen member in the roster digest preimage.
const ROSTER_MEMBER_BYTES: usize = 33 + 32 + 8 + 8;

/// Unique prunable member minimizing (A(x), `admitted_epoch`, participant).
///
/// # Errors
/// `F08_NO_PRUNABLE_MEMBER` when no active member of `role` has two complete misses.
pub fn prune_candidate(
    table: &AdmissionTable,
    role: Role,
    epoch: u64,
) -> CodecResult<AdmissionMeta> {
    table
        .iter()
        .filter(|m| {
            m.participant.role() == role
                && m.admitted()
                && !m.draining()
                && !m.revoked()
                && m.pending_exit.is_none()
                && m.admitted_epoch.is_some_and(|a| epoch > a)
                && m.complete_missed_opened_epochs >= POLICY_V1.missed_epoch_threshold
        })
        .min_by_key(|m| (m.last_activity(), m.admitted_epoch, m.participant))
        .copied()
        .ok_or(F08_NO_PRUNABLE_MEMBER)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Health {
    pub roster_evaluators: u8,
    pub eligible_evaluators: u8,
    pub quorum_ready: bool,
}

/// Frozen evaluator count and current eligibility overlay (revoked/draining excluded).
///
/// # Errors
/// `ARITHMETIC` only if a decoded table could exceed `u8` members, which capacity forbids.
pub fn health(table: &AdmissionTable) -> CodecResult<Health> {
    let frozen = || {
        table
            .iter()
            .filter(|m| m.participant.role() == Role::Evaluator && m.admitted_epoch.is_some())
    };
    let roster = u8::try_from(frozen().count()).map_err(|_| ARITHMETIC)?;
    let eligible = u8::try_from(frozen().filter(|m| !m.revoked() && !m.draining()).count())
        .map_err(|_| ARITHMETIC)?;
    Ok(Health {
        roster_evaluators: roster,
        eligible_evaluators: eligible,
        quorum_ready: eligible >= POLICY_V1.score_quorum,
    })
}

/// New evaluation-dependent task admission gate.
///
/// # Errors
/// `F08_QUORUM_UNAVAILABLE` below three eligible frozen evaluators.
pub fn require_quorum(table: &AdmissionTable) -> CodecResult<()> {
    if health(table)?.quorum_ready {
        Ok(())
    } else {
        Err(F08_QUORUM_UNAVAILABLE)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Rollover {
    pub epoch: u64,
    pub removed: u8,
    pub expired: u8,
    pub installed: u8,
    pub rotated: u8,
    pub members: u8,
    pub digest: Digest32,
    pub health: Health,
}

fn bump(counter: &mut u8) -> CodecResult<()> {
    *counter = counter.checked_add(1).ok_or(ARITHMETIC)?;
    Ok(())
}

/// Shared `OpenEpoch` rollover. `retained` reports unresolved obligations of a member
/// whose exit is due; such a member refuses `F08_RETENTION_BLOCKED` instead of being
/// erased. Unconsumed approvals for an epoch before `epoch` can no longer be admitted
/// and are discarded. Refusal leaves the table unchanged.
///
/// # Errors
/// `WRONG_EPOCH` unless `epoch` advances, `F08_RETENTION_BLOCKED`, `NON_CANONICAL` and
/// `ARITHMETIC`.
pub fn open_epoch(
    table: &mut AdmissionTable,
    epoch: u64,
    retained: impl Fn(&AdmissionMeta) -> bool,
) -> CodecResult<Rollover> {
    roll(
        table,
        epoch,
        |m| Ok(retained(m)),
        |m| Ok(Some(m.delegate_generation)),
    )
}

/// Rollover kernel. `authorized` returns the delegate generation the current identity
/// record authorizes at this snapshot, or `None` while that record is not yet (or no
/// longer) valid: a staged member then stays staged and a frozen member keeps its frozen
/// generation, so pending, current and frozen versions never merge early.
fn roll(
    table: &mut AdmissionTable,
    epoch: u64,
    retained: impl Fn(&AdmissionMeta) -> CodecResult<bool>,
    authorized: impl Fn(&AdmissionMeta) -> CodecResult<Option<u64>>,
) -> CodecResult<Rollover> {
    let previous = table.current_epoch();
    if previous.is_some_and(|p| epoch <= p) {
        return Err(WRONG_EPOCH);
    }
    let mut next = *table;
    let (mut removed, mut expired, mut installed, mut rotated) = (0u8, 0u8, 0u8, 0u8);
    for m in table.iter() {
        if m.pending_exit.is_some_and(|exit| exit.epoch <= epoch) {
            if retained(m)? {
                return Err(F08_RETENTION_BLOCKED);
            }
            next.remove(m.participant)?;
            bump(&mut removed)?;
            continue;
        }
        if let Some(approval) = m.approval {
            if approval.effective_epoch < epoch {
                next.remove(m.participant)?;
                bump(&mut expired)?;
            }
            continue;
        }
        let mut m = *m;
        let generation = authorized(&m)?;
        if generation.is_some_and(|g| g < m.delegate_generation) {
            return Err(NON_CANONICAL);
        }
        match (m.admitted_epoch, previous) {
            (None, _) => {
                let Some(g) = generation.filter(|_| m.immunity_until_epoch <= epoch) else {
                    continue;
                };
                m.admitted_epoch = Some(epoch);
                m.immunity_until_epoch = epoch;
                m.delegate_generation = g;
                bump(&mut installed)?;
            }
            (Some(_), None) => return Err(NON_CANONICAL),
            (Some(_), Some(p)) => {
                let a = m.last_activity().ok_or(NON_CANONICAL)?;
                m.complete_missed_opened_epochs = if p > a {
                    m.complete_missed_opened_epochs
                        .saturating_add(1)
                        .min(POLICY_V1.missed_epoch_threshold)
                } else {
                    0
                };
                if let Some(g) = generation.filter(|g| *g != m.delegate_generation) {
                    m.delegate_generation = g;
                    bump(&mut rotated)?;
                }
            }
        }
        if m.pending_exit.is_none() && !m.revoked() {
            m.membership_flags &= !FLAG_DRAINING;
        }
        next.replace(m)?;
    }
    next.begin_epoch(epoch);
    let digest = roster_digest(&next, epoch)?;
    let members = u8::try_from(next.iter().filter(|m| m.admitted_epoch.is_some()).count())
        .map_err(|_| ARITHMETIC)?;
    let health = health(&next)?;
    *table = next;
    Ok(Rollover {
        epoch,
        removed,
        expired,
        installed,
        rotated,
        members,
        digest,
        health,
    })
}

/// Caller scratch for [`rollover_roster`]: the next admission table, the next identity
/// section (F02 worker table followed by the F03 region) and the next control section.
pub const ROLLOVER_SCRATCH_BYTES: usize =
    TABLE_MAX_BYTES + Section::IdentityRoster.payload_cap() + Section::Control.payload_cap();

/// `RosterOpened` summary of one complete rollover over the shared state value.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RosterOpened {
    pub rollover: Rollover,
    pub previous: Option<u64>,
    pub skipped: u64,
    pub workers: u8,
    pub evaluators: u8,
    pub preserved_entitlements: u16,
    pub preserved_amount: Amount,
    pub state_len: usize,
}

/// Committed F06 reward state at the head of the joint F05/F06 section, bound to the
/// market reserve; an empty section means rewards were never initialized.
fn reward_state<'a>(
    section: &'a [u8],
    market: &MarketHeader,
) -> CodecResult<Option<RewardState<'a>>> {
    if section.is_empty() {
        return Ok(None);
    }
    let rewards = decode_reward_state(section.get(..REWARD_STATE_BYTES).ok_or(NON_CANONICAL)?)?;
    let ledger = rewards.ledger()?;
    if ledger.asset != market.funding_asset || ledger.account != market.rewards_account {
        return Err(ACCOUNT_BINDING);
    }
    if ledger.active_reserve {
        return Err(WRONG_PHASE);
    }
    Ok(Some(rewards))
}

/// Unclaimed TERMINAL entitlements still owed to `worker` through its frozen recipient.
fn outstanding(rewards: &RewardState<'_>, worker: WorkerId) -> CodecResult<(u16, Amount)> {
    let dictionary = rewards.dictionary();
    let (mut count, mut amount) = (0u16, 0);
    for row in rewards.rows().records() {
        let row = row?;
        if row.status != EpochStatus::Terminal {
            continue;
        }
        for entry in row.entries() {
            if entry.disposition == Disposition::Unclaimed
                && entry.entitlement > 0
                && dictionary.slot(entry.slot)?.worker == worker
            {
                count = count.checked_add(1).ok_or(ARITHMETIC)?;
                amount = Amount::checked_add(amount, entry.entitlement).ok_or(ARITHMETIC)?;
            }
        }
    }
    Ok((count, amount))
}

/// Clock epoch of `height` for a live market, inside its Work window.
fn opening_epoch(market: &MarketHeader, height: u64) -> CodecResult<u64> {
    match market.lifecycle {
        REGISTERED | ACTIVE => {}
        SUSPENDED => return Err(F08_MARKET_PAUSED),
        _ => return Err(WRONG_PHASE),
    }
    if height < market.origin_height {
        return Err(WRONG_EPOCH);
    }
    let clock = market_clock(market.origin_height, height)?;
    if clock.phase != EpochPhase::Work {
        return Err(WRONG_PHASE);
    }
    Ok(clock.epoch)
}

/// Delegate generation the member's current F02 record authorizes for `epoch`; an
/// evaluator keeps its F08 generation.
fn authorized(
    workers: &WorkerTable,
    m: &AdmissionMeta,
    height: u64,
    epoch: u64,
) -> CodecResult<Option<u64>> {
    let Participant::Worker(worker) = m.participant else {
        return Ok(Some(m.delegate_generation));
    };
    let record = workers.get(worker).ok_or(NON_CANONICAL)?;
    if record.owner != m.owner {
        return Err(NON_CANONICAL);
    }
    let serving = matches!(record.state, WorkerState::Enrolled | WorkerState::Available)
        && height < record.expiry;
    let version_due =
        record.key_version == m.delegate_generation || record.effective_epoch <= epoch;
    Ok((serving && version_due).then_some(record.key_version))
}

/// `RolloverRoster` over the complete committed shared state value at authenticated
/// `height`, writing the whole next value into `next`. The epoch is the clock epoch of
/// `height`, which must lie in its Work window; skipped epochs add no samples. The
/// previous epoch must be terminal (no active F06 reserve). Due exits leave the F08
/// table, and a departing worker's F02 record and actor replay slot are retired unless
/// that slot still retains an unexpired result (`F08_RETENTION_BLOCKED`); F06 rows and
/// recipient dictionary are carried over byte for byte, so every outstanding entitlement
/// stays claimable under the old stable ID, and so is the F03 evaluator region that
/// follows the worker table in the identity section. A staged worker is installed at its F08
/// effective epoch only while its F02 record is ENROLLED/AVAILABLE with an unexpired
/// manifest; a changed F02 key version is frozen only from its F02 effective epoch, so a
/// pending rotation never enters an earlier snapshot. The revision is not advanced: the
/// enclosing `OpenEpoch` commits it together with its replay record and event. Refusal
/// writes nothing the caller may commit.
///
/// # Errors
/// `F08_STALE_STATE`, `F08_MARKET_PAUSED`, `WRONG_PHASE` for a closing market, a height
/// outside the Work window or a non-terminal previous epoch, `WRONG_EPOCH` before origin
/// or for a non-advancing epoch, `ACCOUNT_BINDING`, `F08_RETENTION_BLOCKED`,
/// `NON_CANONICAL` for inconsistent F02/F08 records, `CAPACITY` for short buffers or an
/// oversized state, `ARITHMETIC` and every section decoding error.
pub fn rollover_roster(
    current: &[u8],
    height: u64,
    expected_revision: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<RosterOpened> {
    Ok(rollover_roster_with(current, height, expected_revision, next, scratch)?.0)
}

/// [`rollover_roster`] that also returns the joint F05/F06 section of `current`, which the
/// next value carries byte for byte, and its validated reward state when initialized.
pub(crate) fn rollover_roster_with<'c>(
    current: &'c [u8],
    height: u64,
    expected_revision: u64,
    next: &mut [u8],
    scratch: &mut [u8],
) -> CodecResult<(RosterOpened, &'c [u8], Option<RewardState<'c>>)> {
    let state = state::decode_shared_state(current)?;
    if state.revision != expected_revision {
        return Err(F08_STALE_STATE);
    }
    let sections = state.feature_sections;
    let market = PolicySection::decode(sections[Section::PolicyLifecycle.index()])?.header;
    let epoch = opening_epoch(&market, height)?;
    let mut admission = AdmissionTable::decode(sections[Section::ReputationAdmission.index()])?;
    let previous = admission.current_epoch();
    let skipped = match previous {
        Some(p) if epoch <= p => return Err(WRONG_EPOCH),
        Some(p) => epoch - p - 1,
        None => epoch,
    };
    let rewards = reward_state(sections[Section::SettlementClaims.index()], &market)?;
    let (worker_bytes, region) = split_identity_section(sections[Section::IdentityRoster.index()])?;
    let mut workers = WorkerTable::decode(worker_bytes)?;
    let replay = &state.control.replay;
    let before = admission;
    let rollover = roll(
        &mut admission,
        epoch,
        |m| {
            let Participant::Worker(worker) = m.participant else {
                return Ok(false);
            };
            let record = workers.get(worker).ok_or(NON_CANONICAL)?;
            let slot = ActorSlot::worker(usize::from(record.slot))?;
            Ok(replay
                .actor(slot)
                .and_then(|actor| actor.last)
                .is_some_and(|last| last.expiry_height > height))
        },
        |m| authorized(&workers, m, height, epoch),
    )?;
    let mut control = Control {
        replay: state.control.replay.clone(),
        feature_bytes: state.control.feature_bytes,
    };
    let (mut preserved_entitlements, mut preserved_amount) = (0u16, 0);
    let mut workers_changed = false;
    for m in before
        .iter()
        .filter(|m| m.pending_exit.is_some_and(|exit| exit.epoch <= epoch))
    {
        let Participant::Worker(worker) = m.participant else {
            continue;
        };
        let record = workers.remove(worker)?;
        control
            .replay
            .retire(ActorSlot::worker(usize::from(record.slot))?)?;
        workers_changed = true;
        if let Some(rewards) = &rewards {
            let (count, amount) = outstanding(rewards, worker)?;
            preserved_entitlements = preserved_entitlements
                .checked_add(count)
                .ok_or(ARITHMETIC)?;
            preserved_amount = Amount::checked_add(preserved_amount, amount).ok_or(ARITHMETIC)?;
        }
    }
    let (table_out, rest) = scratch
        .split_at_mut_checked(TABLE_MAX_BYTES)
        .ok_or(CAPACITY)?;
    let (identity_out, control_out) = rest
        .split_at_mut_checked(Section::IdentityRoster.payload_cap())
        .ok_or(CAPACITY)?;
    let mut feature_sections = sections;
    let table_len = admission.encode(table_out)?;
    feature_sections[Section::ReputationAdmission.index()] = &table_out[..table_len];
    if workers_changed {
        let workers_len = workers.encode(identity_out)?;
        let identity_len = workers_len.checked_add(region.len()).ok_or(ARITHMETIC)?;
        identity_out
            .get_mut(workers_len..identity_len)
            .ok_or(CAPACITY)?
            .copy_from_slice(region);
        feature_sections[Section::IdentityRoster.index()] = &identity_out[..identity_len];
    }
    let opened = SharedState {
        revision: state.revision,
        feature_sections,
        control,
    };
    let state_len = state::encode_shared_state(&opened, next, control_out)?;
    let roster = RosterOpened {
        rollover,
        previous,
        skipped,
        workers: rollover
            .members
            .checked_sub(rollover.health.roster_evaluators)
            .ok_or(ARITHMETIC)?,
        evaluators: rollover.health.roster_evaluators,
        preserved_entitlements,
        preserved_amount,
        state_len,
    };
    Ok((roster, sections[Section::SettlementClaims.index()], rewards))
}

/// Digest over the frozen roster sorted by role then stable participant ID.
///
/// # Errors
/// Propagates codec and host hash failures.
pub fn roster_digest(table: &AdmissionTable, epoch: u64) -> CodecResult<Digest32> {
    let mut buf = [0u8; 8 + MAX_MEMBERS * ROSTER_MEMBER_BYTES];
    let mut w = Writer::new(&mut buf);
    w.u64(epoch)?;
    for m in table.iter().filter(|m| m.admitted_epoch.is_some()) {
        w.u8(m.participant.role() as u8)?;
        w.put(&m.participant.bytes())?;
        w.put(m.owner.as_bytes())?;
        w.u64(m.membership_generation)?;
        w.u64(m.delegate_generation)?;
    }
    let n = w.len();
    codec::domain_hash("PAXAI/admission-roster/v1", &buf[..n])
}
