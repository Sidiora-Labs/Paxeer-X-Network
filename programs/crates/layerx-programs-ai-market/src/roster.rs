//! F08 deterministic roster rollover, inactivity selection and evaluator health.
//! Selection never consults quality history (F07); it uses activity epochs and IDs only.
use crate::{
    admission::{AdmissionMeta, AdmissionTable, Role, FLAG_DRAINING, MAX_MEMBERS, POLICY_V1},
    codec::{self, Writer},
    errors::{
        CodecResult, ARITHMETIC, F08_NO_PRUNABLE_MEMBER, F08_QUORUM_UNAVAILABLE,
        F08_RETENTION_BLOCKED, NON_CANONICAL, WRONG_EPOCH,
    },
    types::Digest32,
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
    let previous = table.current_epoch();
    if previous.is_some_and(|p| epoch <= p) {
        return Err(WRONG_EPOCH);
    }
    let mut next = *table;
    let (mut removed, mut expired, mut installed) = (0u8, 0u8, 0u8);
    for m in table.iter() {
        if m.pending_exit.is_some_and(|exit| exit.epoch <= epoch) {
            if retained(m) {
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
        match (m.admitted_epoch, previous) {
            (None, _) if m.immunity_until_epoch <= epoch => {
                m.admitted_epoch = Some(epoch);
                m.immunity_until_epoch = epoch;
                bump(&mut installed)?;
            }
            (None, _) => continue,
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
        members,
        digest,
        health,
    })
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
