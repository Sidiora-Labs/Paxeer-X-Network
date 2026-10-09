//! F01 close journal over the complete committed shared state value: the owner
//! `REQUEST_CLOSE` (0x010A) and the permissionless object-local `ADVANCE_CLOSE` (0x010B).
//!
//! `REQUEST_CLOSE` enters winding down, cancels the scheduled activation and the pending policy
//! (appending its CANCELLED history header), revokes the operator grant and starts the journal
//! at phase 1, cursor 0. Each `ADVANCE_CLOSE` performs exactly one progress unit and increments
//! the cursor:
//! - phase 1 waits until the opened epoch's F06 row is no longer Reserved;
//! - phase 2 waits until the opened epoch's task set is sealed (or empty) and names its
//!   preserved identity;
//! - phase 3 refunds all F06 Free to the immutable creation-time recipient through
//!   `RefundFree`, staging one `RewardEffect::Payout`;
//! - phase 4 releases one epoch's unclaimed entitlements at or after their F06 expiry and
//!   returns to phase 3, returns to phase 3 for any other Free, and waits while Reserved or
//!   Liability remain;
//! - phase 5 requires Free = Reserved = Liability = 0 and freezes the Closed tombstone with
//!   the closing request digest.
//!
//! A waiting phase refuses `F01_OBLIGATIONS_OUTSTANDING`. Every success commits exactly one
//! revision increment with `header.state_revision` equal to the shared revision; every
//! refusal leaves `current` unchanged and the outputs must be discarded.
use crate::{
    admission::AdmissionTable,
    codec::{self, derive_market, EventCommon, Reader, ValidatedEnvelope, Writer},
    dispatch::{self, Operation},
    errors::{
        CodecResult, ARITHMETIC, CAPACITY, F01_ALREADY_CLOSING, F01_INVALID_REASON,
        F01_LIFECYCLE_CLOSED, F01_OBLIGATIONS_OUTSTANDING, F01_STALE_REVISION, F01_WRONG_LIFECYCLE,
        NON_CANONICAL, NOT_FOUND, STALE_CURSOR, UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG,
        WRONG_MARKET, WRONG_PHASE,
    },
    policy::{
        fold_policy_history, PendingPolicy, PolicyHistoryHeader, MAX_RECENT_POLICY_HEADERS,
        POLICY_HISTORY_HEADER_BYTES, TASK_POLICY_BYTES,
    },
    registry::{check_f01_capacity, market_clock, OperatorGrant, OPERATOR_GRANT_BYTES},
    registry_ops::{
        CallContext, PolicySection, ACTIVE, CANCELLED, CLOSED, RECEIPT_MAX_BYTES, REGISTERED,
        SUSPENDED, WINDING_DOWN,
    },
    rewards::{
        decode_reward_state, EpochStatus, FundingPhase, RefundRequest, RewardEffect, RewardOutcome,
        RewardState, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, ReplayDecision, ReplayRequest,
        RetainedResult, Section, SharedState,
    },
    tasks::{terminal_task_set, TaskSet},
    types::{Amount, Presence, RequestDigest, ResultDigest, Version},
};

const POLICY_CAP: usize = Section::PolicyLifecycle.payload_cap();
const SETTLEMENT_CAP: usize = Section::SettlementClaims.payload_cap();
const CONTROL_CAP: usize = Section::Control.payload_cap();
/// Caller scratch for [`apply`]: the next F01 section, the next joint F05/F06 section and the
/// control payload.
pub const SCRATCH_BYTES: usize = POLICY_CAP + SETTLEMENT_CAP + CONTROL_CAP;
/// `REQUEST_CLOSE` event suffix: lifecycle, config, closure height and reason digest.
pub const REQUEST_SUFFIX_BYTES: usize = 49;
/// `ADVANCE_CLOSE` event suffix: lifecycle, phase, cursor, amount and subject.
pub const ADVANCE_SUFFIX_BYTES: usize = 52;
const OWNER_AUTHORITY: u64 = 1;
const FIRST_PHASE: u8 = 1;
const TASKS_PHASE: u8 = 2;
const REFUND_PHASE: u8 = 3;
const SETTLE_PHASE: u8 = 4;
const FINAL_PHASE: u8 = 5;

/// The F01 receipt: market, committed revision, request id, action, config and the current
/// policy digest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Receipt {
    bytes: [u8; RECEIPT_MAX_BYTES],
    pub digest: ResultDigest,
}
impl Receipt {
    fn new(
        section: &PolicySection<'_>,
        revision: u64,
        envelope: &ValidatedEnvelope<'_>,
    ) -> CodecResult<Self> {
        let e = &envelope.envelope;
        let mut bytes = [0; RECEIPT_MAX_BYTES];
        let mut w = Writer::new(&mut bytes);
        w.put(section.header.market_id.as_bytes())?;
        w.u64(revision)?;
        w.put(e.request.as_bytes())?;
        w.u16(e.operation.selector())?;
        w.u64(section.header.active_config_version)?;
        w.presence(
            &Presence::Present(section.current.digest()?),
            |w, policy| w.put(policy.as_bytes()),
        )?;
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
    /// One revision increment was composed into `next` and its event into `event`. `effect`
    /// is the single F06 native effect the activity must apply atomically with the state:
    /// `Payout` for a phase 3 refund, `Released` for a phase 4 expiry, otherwise `NoTransfer`.
    Applied {
        receipt: Receipt,
        revision: u64,
        state_len: usize,
        event_len: usize,
        lifecycle: u8,
        phase: u8,
        cursor: u16,
        effect: RewardEffect,
    },
    /// Exact retry of an owner `REQUEST_CLOSE` already applied under its role sequence.
    Retained(RetainedResult),
}

struct Committed<'a> {
    state: SharedState<'a>,
    section: PolicySection<'a>,
}

fn committed(current: &[u8]) -> CodecResult<Committed<'_>> {
    let state = decode_shared_state(current)?;
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    Ok(Committed { state, section })
}

/// The F06 reward state and the F05 bytes after it; `None` before the first funding.
fn rewards<'a>(state: &SharedState<'a>) -> CodecResult<Option<(RewardState<'a>, &'a [u8])>> {
    let section = state.feature_sections[Section::SettlementClaims.index()];
    if section.is_empty() {
        return Ok(None);
    }
    let (head, tail) = section
        .split_at_checked(REWARD_STATE_BYTES)
        .ok_or(NON_CANONICAL)?;
    Ok(Some((decode_reward_state(head)?, tail)))
}

struct Buffers<'b> {
    next: &'b mut [u8],
    policy: &'b mut [u8],
    settlement: &'b mut [u8],
    control: &'b mut [u8],
    event: &'b mut [u8],
}

/// The progress of one call: the next journal position, the new reward state written to
/// the settlement buffer (if any) and the event amount and subject.
struct Progress {
    lifecycle: u8,
    phase: u8,
    cursor: u16,
    settlement_len: Option<usize>,
    effect: RewardEffect,
    amount: Amount,
    subject: [u8; 32],
}
impl Progress {
    /// A move to `lifecycle` and `phase` without any F06 change.
    const fn to(lifecycle: u8, phase: u8, subject: [u8; 32]) -> Self {
        Self {
            lifecycle,
            phase,
            cursor: 0,
            settlement_len: None,
            effect: RewardEffect::NoTransfer,
            amount: 0,
            subject,
        }
    }
}

struct Call<'c> {
    ctx: &'c CallContext,
    envelope: &'c ValidatedEnvelope<'c>,
    committed: &'c Committed<'c>,
}

impl Call<'_> {
    fn revision(&self) -> CodecResult<u64> {
        self.committed
            .state
            .revision
            .checked_add(1)
            .ok_or(ARITHMETIC)
    }

    /// Commits `section` (with the next revision bound) and the optional settlement bytes,
    /// records the owner request when present and emits the event.
    fn finish(
        &self,
        mut section: PolicySection<'_>,
        withdrawn: Option<&PendingPolicy>,
        replay: Option<&ReplayRequest>,
        progress: &Progress,
        suffix: &[u8],
        buffers: Buffers<'_>,
    ) -> CodecResult<Outcome> {
        let Buffers {
            next,
            policy,
            settlement,
            control,
            event,
        } = buffers;
        let e = &self.envelope.envelope;
        let revision = self.revision()?;
        section.header.state_revision = revision;
        let policy_len = match withdrawn {
            Some(pending) => encode_withdrawn(&section, pending, policy)?,
            None => section.encode(policy)?,
        };
        let policy = policy.get(..policy_len).ok_or(CAPACITY)?;
        PolicySection::decode(policy)?;
        let mut feature_sections = self.committed.state.feature_sections;
        feature_sections[Section::PolicyLifecycle.index()] = policy;
        if let Some(n) = progress.settlement_len {
            feature_sections[Section::SettlementClaims.index()] =
                settlement.get(..n).ok_or(CAPACITY)?;
        }
        let mut candidate = SharedState {
            revision: self.committed.state.revision,
            feature_sections,
            control: self.committed.state.control.clone(),
        };
        let receipt = Receipt::new(&section, revision, self.envelope)?;
        match replay {
            Some(request) => {
                if candidate.record_success(request, self.ctx.height, receipt.digest)?
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
                market: section.header.market_id,
                epoch: market_clock(section.header.origin_height, self.ctx.height)?.epoch,
                config: Version::new(section.header.active_config_version)?,
                revision,
                request: self.envelope.request_digest()?,
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
            lifecycle: progress.lifecycle,
            phase: progress.phase,
            cursor: progress.cursor,
            effect: progress.effect,
        })
    }
}

/// The section bytes of `section` with `pending` withdrawn: the pending record is absent and
/// its CANCELLED history header is appended, folding the oldest header into the root when the
/// recent list is full.
fn encode_withdrawn(
    section: &PolicySection<'_>,
    pending: &PendingPolicy,
    out: &mut [u8],
) -> CodecResult<usize> {
    let cancelled = PolicyHistoryHeader {
        config_version: pending.policy.config_version,
        digest: pending.digest,
        effective_epoch: pending.effective_epoch,
        disposition: CANCELLED,
    };
    let recent = section.recent();
    let (root, kept) = if recent.len() == MAX_RECENT_POLICY_HEADERS {
        (
            fold_policy_history(
                section.history_root,
                0,
                recent.get(..1).ok_or(NON_CANONICAL)?,
            )?,
            recent.get(1..).ok_or(NON_CANONICAL)?,
        )
    } else {
        (section.history_root, recent)
    };
    let h = section.header.encode(out)?;
    let mut w = Writer::new(out.get_mut(h..).ok_or(CAPACITY)?);
    w.presence(&section.operator, |w, grant: &OperatorGrant| {
        let mut b = [0; OPERATOR_GRANT_BYTES];
        grant.encode(&mut b)?;
        w.put(&b)
    })?;
    let mut policy = [0; TASK_POLICY_BYTES];
    section.current.encode(&mut policy)?;
    w.put(&policy)?;
    w.presence(&Presence::<PendingPolicy>::Absent, |_, _| Ok(()))?;
    w.u16(u16::try_from(kept.len() + 1).map_err(|_| ARITHMETIC)?)?;
    for header in kept.iter().chain(core::iter::once(&cancelled)) {
        let mut b = [0; POLICY_HISTORY_HEADER_BYTES];
        header.encode(&mut b)?;
        w.put(&b)?;
    }
    w.put(root.as_bytes())?;
    w.u32(u32::try_from(section.task_region.len()).map_err(|_| ARITHMETIC)?)?;
    w.put(section.task_region)?;
    h.checked_add(w.len()).ok_or(ARITHMETIC)
}

fn authenticate(call: &Call<'_>) -> CodecResult<()> {
    let e = &call.envelope.envelope;
    let ctx = call.ctx;
    codec::compare_native_principal(e, ctx.principal)?;
    let market = derive_market(ctx.chain, ctx.program)?;
    e.check_domain(ctx.chain, ctx.program, market)?;
    e.check_expiry(ctx.height)?;
    let header = &call.committed.section.header;
    if header.market_id != market {
        return Err(WRONG_MARKET);
    }
    if e.config != header.active_config_version {
        return Err(WRONG_CONFIG);
    }
    Ok(())
}

/// `REQUEST_CLOSE` (0x010A): owner only, from Registered, Active or Suspended.
fn request_close(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = &call.envelope.envelope;
    let mut r = Reader::new(e.payload);
    let expected = r.u64()?;
    let reason: [u8; 32] = r.fixed()?;
    r.finish()?;
    let committed = call.committed;
    let section = committed.section;
    if e.actor != section.header.owner_principal {
        return Err(UNAUTHORIZED);
    }
    let request = ReplayRequest::from_envelope(
        ActorSlot::OWNER,
        Version::new(OWNER_AUTHORITY)?,
        call.envelope,
    )?;
    if let ReplayDecision::AlreadyApplied(retained) = committed
        .state
        .control
        .replay
        .check(&request, call.ctx.height)?
    {
        return Ok(Outcome::Retained(retained));
    }
    if expected != committed.state.revision {
        return Err(F01_STALE_REVISION);
    }
    if reason == [0; 32] {
        return Err(F01_INVALID_REASON);
    }
    match section.header.lifecycle {
        REGISTERED | ACTIVE | SUSPENDED => {}
        WINDING_DOWN => return Err(F01_ALREADY_CLOSING),
        CLOSED => return Err(F01_LIFECYCLE_CLOSED),
        _ => return Err(F01_WRONG_LIFECYCLE),
    }
    rewards(&committed.state)?;
    let mut next = section;
    next.header.lifecycle = WINDING_DOWN;
    next.header.activation_scheduled = false;
    next.header.activation_epoch = 0;
    next.header.closure_requested_at = call.ctx.height;
    next.header.close_phase = FIRST_PHASE;
    next.header.close_cursor = 0;
    if let Presence::Present(grant) = section.operator {
        next.operator = Presence::Present(OperatorGrant {
            revoked: true,
            ..grant
        });
    }
    let withdrawn = match section.pending {
        Presence::Present(pending) => Some(pending),
        Presence::Absent => None,
    };
    let mut suffix = [0; REQUEST_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.u8(WINDING_DOWN)?;
    w.u64(section.header.active_config_version)?;
    w.u64(call.ctx.height)?;
    w.put(&reason)?;
    let progress = Progress::to(WINDING_DOWN, FIRST_PHASE, reason);
    call.finish(
        next,
        withdrawn.as_ref(),
        Some(&request),
        &progress,
        &suffix,
        buffers,
    )
}

/// Phase 1: the currently opened epoch's F06 row must no longer be Reserved.
fn epoch_terminal(state: &SharedState<'_>, rewards: Option<&RewardState<'_>>) -> CodecResult<bool> {
    let Some(rewards) = rewards else {
        return Ok(true);
    };
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
    let Some(epoch) = admission.current_epoch() else {
        return Ok(true);
    };
    match rewards.row(epoch) {
        Ok(row) => Ok(row.status != EpochStatus::Reserved),
        Err(NOT_FOUND) => Ok(true),
        Err(error) => Err(error),
    }
}

/// Phase 2: the preserved task-set identity of the opened epoch, or `None` while a nonempty
/// set is unsealed.
fn task_set_identity(
    state: &SharedState<'_>,
    section: &PolicySection<'_>,
) -> CodecResult<Option<[u8; 32]>> {
    let admission =
        AdmissionTable::decode(state.feature_sections[Section::ReputationAdmission.index()])?;
    match admission.current_epoch() {
        Some(epoch) => match terminal_task_set(state, epoch) {
            Ok(digest) => Ok(Some(digest.bytes())),
            Err(WRONG_PHASE) => Ok(None),
            Err(error) => Err(error),
        },
        None if TaskSet::decode(section.task_region)?.is_empty() => Ok(Some([0; 32])),
        None => Ok(None),
    }
}

/// Writes the reward state produced by `transition` followed by the unchanged F05 `tail`.
fn write_settlement(
    tail: &[u8],
    out: &mut [u8],
    transition: impl FnOnce(&mut [u8]) -> CodecResult<RewardEffect>,
) -> CodecResult<(usize, RewardEffect)> {
    let total = REWARD_STATE_BYTES
        .checked_add(tail.len())
        .ok_or(ARITHMETIC)?;
    let (head, rest) = out
        .get_mut(..total)
        .ok_or(CAPACITY)?
        .split_at_mut(REWARD_STATE_BYTES);
    let effect = transition(head)?;
    rest.copy_from_slice(tail);
    Ok((total, effect))
}

/// Phase 3: one `RefundFree` of all Free to the immutable recipient.
fn refund(
    call: &Call<'_>,
    rewards: Option<(RewardState<'_>, &[u8])>,
    receipt: ResultDigest,
    settlement: &mut [u8],
) -> CodecResult<Progress> {
    let mut progress = Progress::to(WINDING_DOWN, SETTLE_PHASE, [0; 32]);
    let Some((rewards, tail)) = rewards else {
        return Ok(progress);
    };
    let ledger = rewards.ledger()?;
    if ledger.free == 0 {
        return Ok(progress);
    }
    let request: RequestDigest = call.envelope.request_digest()?;
    let (len, effect) = write_settlement(tail, settlement, |head| {
        rewards
            .refund_free(
                FundingPhase::Closing,
                &RefundRequest {
                    expected_refunded: ledger.tracked_refunds,
                    amount: ledger.free,
                    recipient: ledger.refund_recipient,
                },
                request,
                receipt,
                head,
            )
            .map(|(_, effect)| effect)
    })?;
    let RewardEffect::Payout { recipient, amount } = effect else {
        return Err(NON_CANONICAL);
    };
    progress.settlement_len = Some(len);
    progress.effect = effect;
    progress.amount = amount;
    progress.subject = recipient.bytes();
    Ok(progress)
}

/// Phase 4: release one expired epoch, return to phase 3 for Free, wait for live
/// obligations, or move to phase 5.
fn settle(
    call: &Call<'_>,
    rewards: Option<(RewardState<'_>, &[u8])>,
    settlement: &mut [u8],
) -> CodecResult<Progress> {
    let mut progress = Progress::to(WINDING_DOWN, FINAL_PHASE, [0; 32]);
    let Some((rewards, tail)) = rewards else {
        return Ok(progress);
    };
    let ledger = rewards.ledger()?;
    if ledger.free != 0 {
        progress.phase = REFUND_PHASE;
        return Ok(progress);
    }
    let height = call.ctx.height;
    for row in rewards.rows().records() {
        let row = row?;
        if row.status != EpochStatus::Terminal
            || row.outcome != RewardOutcome::Allocated
            || row.unclaimed_sum()? == 0
            || height < row.expiry_height
        {
            continue;
        }
        let (len, effect) = write_settlement(tail, settlement, |head| {
            rewards
                .expire_epoch_claims(row.epoch, height, head)
                .map(|(_, effect)| effect)
        })?;
        let RewardEffect::Released(amount) = effect else {
            return Err(NON_CANONICAL);
        };
        progress.phase = REFUND_PHASE;
        progress.settlement_len = Some(len);
        progress.effect = effect;
        progress.amount = amount;
        let mut subject = [0; 32];
        subject[24..].copy_from_slice(&row.epoch.to_be_bytes());
        progress.subject = subject;
        return Ok(progress);
    }
    if ledger.reserved != 0 || ledger.liability != 0 {
        return Err(F01_OBLIGATIONS_OUTSTANDING);
    }
    Ok(progress)
}

/// `ADVANCE_CLOSE` (0x010B): anyone, while winding down, one progress unit.
fn advance_close(call: &Call<'_>, buffers: Buffers<'_>) -> CodecResult<Outcome> {
    let e = &call.envelope.envelope;
    let mut r = Reader::new(e.payload);
    let expected = r.u64()?;
    let phase = r.u8()?;
    let cursor = r.u16()?;
    r.finish()?;
    let committed = call.committed;
    let section = committed.section;
    let header = &section.header;
    if expected != committed.state.revision {
        return Err(F01_STALE_REVISION);
    }
    match header.lifecycle {
        WINDING_DOWN => {}
        CLOSED => return Err(F01_LIFECYCLE_CLOSED),
        _ => return Err(F01_WRONG_LIFECYCLE),
    }
    if phase != header.close_phase || cursor != header.close_cursor {
        return Err(STALE_CURSOR);
    }
    let state = &committed.state;
    let rewards = rewards(state)?;
    let Buffers {
        next,
        policy,
        settlement,
        control,
        event,
    } = buffers;
    let mut progress = match header.close_phase {
        FIRST_PHASE => {
            if !epoch_terminal(state, rewards.as_ref().map(|(r, _)| r))? {
                return Err(F01_OBLIGATIONS_OUTSTANDING);
            }
            Progress::to(WINDING_DOWN, TASKS_PHASE, [0; 32])
        }
        TASKS_PHASE => {
            let subject = task_set_identity(state, &section)?.ok_or(F01_OBLIGATIONS_OUTSTANDING)?;
            Progress::to(WINDING_DOWN, REFUND_PHASE, subject)
        }
        REFUND_PHASE => {
            let receipt = Receipt::new(&section, call.revision()?, call.envelope)?;
            refund(call, rewards, receipt.digest, settlement)?
        }
        SETTLE_PHASE => settle(call, rewards, settlement)?,
        FINAL_PHASE => {
            if let Some((rewards, _)) = rewards {
                let ledger = rewards.ledger()?;
                if ledger.free != 0 || ledger.reserved != 0 || ledger.liability != 0 {
                    return Err(F01_OBLIGATIONS_OUTSTANDING);
                }
            }
            Progress::to(CLOSED, FINAL_PHASE, call.envelope.request_digest()?.bytes())
        }
        _ => return Err(NON_CANONICAL),
    };
    progress.cursor = header.close_cursor.checked_add(1).ok_or(ARITHMETIC)?;
    let mut next_section = section;
    next_section.header.lifecycle = progress.lifecycle;
    next_section.header.close_phase = progress.phase;
    next_section.header.close_cursor = progress.cursor;
    if progress.lifecycle == CLOSED {
        next_section.header.closing_request_digest = progress.subject;
    }
    let mut suffix = [0; ADVANCE_SUFFIX_BYTES];
    let mut w = Writer::new(&mut suffix);
    w.u8(progress.lifecycle)?;
    w.u8(progress.phase)?;
    w.u16(progress.cursor)?;
    w.u128(progress.amount)?;
    w.put(&progress.subject)?;
    call.finish(
        next_section,
        None,
        None,
        &progress,
        &suffix,
        Buffers {
            next,
            policy,
            settlement,
            control,
            event,
        },
    )
}

/// Applies one F01 close request to the committed `current` state, writing the whole next
/// state into `next` (at least `MAX_STATE_BYTES`) and its event into `event`; `scratch` holds
/// at least [`SCRATCH_BYTES`]. Dispatch arm: `0x010A | 0x010B => closure::apply(ctx,
/// envelope, present(current)?, next, scratch, event)`, staging an `Applied` effect of
/// `RewardEffect::Payout` as the F06 native transfer in the same atomic commit.
///
/// # Errors
/// `UNKNOWN_OPERATION`; `NON_CANONICAL` for a malformed payload or an inconsistent committed
/// state; envelope principal, domain and expiry refusals; `WRONG_MARKET`; `WRONG_CONFIG`;
/// `UNAUTHORIZED` for a close by anyone but the owner; common replay refusals;
/// `F01_STALE_REVISION`; `F01_INVALID_REASON`; `F01_ALREADY_CLOSING`; `F01_LIFECYCLE_CLOSED`;
/// `F01_WRONG_LIFECYCLE`; `STALE_CURSOR` for another journal phase or cursor;
/// `F01_OBLIGATIONS_OUTSTANDING` while a phase waits; F06 ledger refusals;
/// `F01_CAPACITY_UNAVAILABLE`, `CAPACITY` and `ARITHMETIC`. On any error `current` is
/// unchanged and the outputs must be discarded.
pub fn apply(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Outcome> {
    let operation: Operation = envelope.envelope.operation;
    if !matches!(operation, dispatch::REQUEST_CLOSE | dispatch::ADVANCE_CLOSE) {
        return Err(UNKNOWN_OPERATION);
    }
    let committed = committed(current)?;
    let call = Call {
        ctx,
        envelope,
        committed: &committed,
    };
    authenticate(&call)?;
    let (policy, rest) = scratch.split_at_mut_checked(POLICY_CAP).ok_or(CAPACITY)?;
    let (settlement, control) = rest.split_at_mut_checked(SETTLEMENT_CAP).ok_or(CAPACITY)?;
    let buffers = Buffers {
        next,
        policy,
        settlement,
        control,
        event,
    };
    if operation == dispatch::REQUEST_CLOSE {
        request_close(&call, buffers)
    } else {
        advance_close(&call, buffers)
    }
}
