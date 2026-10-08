//! F08 deterministic roster rollover, inactivity selection and evaluator health.
//! Selection never consults quality history (F07); it uses activity epochs and IDs only.
use crate::{
    admission::{AdmissionMeta, AdmissionTable, POLICY_V1, ROLE_EVALUATOR},
    codec::{self, Writer},
    errors::*,
    types::*,
};

/// Unique prunable member minimizing (A(x), admitted_epoch, participant_id).
pub fn prune_candidate(table: &AdmissionTable, role: u8, epoch: u64) -> CodecResult<AdmissionMeta> {
    table
        .iter()
        .filter(|m| {
            m.role == role
                && m.admitted()
                && !m.draining()
                && m.pending_exit_epoch.is_none()
                && m.admitted_epoch.is_some_and(|a| epoch > a)
                && epoch > m.immunity_until_epoch
                && m.complete_missed_opened_epochs >= POLICY_V1.missed_epoch_threshold
                && m.last_heartbeat_epoch != Some(epoch)
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
pub fn health(table: &AdmissionTable) -> Health {
    let mut roster = 0u8;
    let mut eligible = 0u8;
    for m in table
        .iter()
        .filter(|m| m.role == ROLE_EVALUATOR && m.admitted() && m.admitted_epoch.is_some())
    {
        roster += 1;
        if !m.revoked() && !m.draining() {
            eligible += 1;
        }
    }
    Health {
        roster_evaluators: roster,
        eligible_evaluators: eligible,
        quorum_ready: eligible >= POLICY_V1.score_quorum,
    }
}

/// New evaluation-dependent task admission gate.
pub fn require_quorum(table: &AdmissionTable) -> CodecResult<()> {
    if health(table).quorum_ready {
        Ok(())
    } else {
        Err(F08_QUORUM_UNAVAILABLE)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Rollover {
    pub epoch: u64,
    pub removed: u8,
    pub installed: u8,
    pub members: u8,
    pub digest: Digest32,
    pub health: Health,
}

/// Retired current records are discarded by the caller only after its own
/// retention prerequisites; `retained` refuses removal of a member with
/// unresolved obligations (RetentionBlocked) instead of erasing them.
pub fn open_epoch(
    table: &mut AdmissionTable,
    epoch: u64,
    retained: &dyn Fn(&AdmissionMeta) -> bool,
) -> CodecResult<Rollover> {
    if table.epoch_present && epoch <= table.current_epoch {
        return Err(WRONG_EPOCH);
    }
    let previous = table.epoch_present.then_some(table.current_epoch);
    let mut next = *table;
    let mut removed = 0u8;
    let mut exiting = [[0u8; 32]; crate::admission::MAX_MEMBERS];
    for m in table.iter() {
        if m.pending_exit_epoch.is_some_and(|e| e <= epoch) {
            if retained(m) {
                return Err(F08_RETENTION_BLOCKED);
            }
            exiting[usize::from(removed)] = m.participant;
            removed += 1;
        }
    }
    for id in &exiting[..usize::from(removed)] {
        next.remove(*id)?;
    }
    let mut installed = 0u8;
    let snapshot = next;
    for m in snapshot.iter() {
        let mut m = *m;
        if !m.admitted() {
            continue;
        }
        match m.admitted_epoch {
            None if m.effective_epoch <= epoch => {
                m.admitted_epoch = Some(epoch);
                m.immunity_until_epoch = epoch;
                m.complete_missed_opened_epochs = 0;
                installed += 1;
            }
            None => continue,
            Some(_) => {
                let p = previous.ok_or(NON_CANONICAL)?;
                let a = m.last_activity().ok_or(NON_CANONICAL)?;
                m.complete_missed_opened_epochs = if p > a {
                    (m.complete_missed_opened_epochs + 1).min(2)
                } else {
                    0
                };
            }
        }
        if m.pending_exit_epoch.is_none() && !m.revoked() {
            m.membership_flags &= !crate::admission::FLAG_DRAINING;
        }
        next.replace(m)?;
    }
    next.epoch_present = true;
    next.current_epoch = epoch;
    next.enrollments_this_epoch = 0;
    let digest = roster_digest(&next, epoch)?;
    let members = u8::try_from(next.iter().filter(|m| m.admitted_epoch.is_some()).count())
        .map_err(|_| ARITHMETIC)?;
    let h = health(&next);
    *table = next;
    Ok(Rollover {
        epoch,
        removed,
        installed,
        members,
        digest,
        health: h,
    })
}

/// Digest over the frozen roster sorted by role then stable participant ID.
pub fn roster_digest(table: &AdmissionTable, epoch: u64) -> CodecResult<Digest32> {
    let mut buf = [0u8; 8 + crate::admission::MAX_MEMBERS * 89];
    let mut w = Writer::new(&mut buf);
    w.u64(epoch)?;
    for m in table.iter().filter(|m| m.admitted_epoch.is_some()) {
        w.u8(m.role)?;
        w.put(&m.participant)?;
        w.put(m.owner.as_bytes())?;
        w.u64(m.membership_generation)?;
        w.u64(m.delegate_generation)?;
    }
    let n = w.len();
    codec::domain_hash("PAXAI/admission-roster/v1", &buf[..n])
}
