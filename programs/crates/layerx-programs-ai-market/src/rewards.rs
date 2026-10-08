//! F06 reward ledger, recipient dictionary and epoch records with strict
//! big-endian encodings. These records move no funds; native transfer, grant
//! and finality authority stay with the real Program activity.
use crate::{
    codec::{domain_hash, Reader, Writer},
    errors::{
        CodecResult, ACCOUNT_BINDING, ARITHMETIC, CAPACITY, F06_AGGREGATION_MISMATCH,
        F06_CLAIM_EXPIRED, F06_CLAIM_NOT_READY, F06_CONTRIBUTION_CONSENT_REQUIRED,
        F06_EPOCH_ALREADY_RESERVED, F06_EPOCH_NOT_RESERVED, F06_EPOCH_TERMINAL,
        F06_FUNDING_POLICY_MISMATCH, F06_INVALID_AMOUNT, F06_LEDGER_INVARIANT_VIOLATION,
        F06_NOTHING_TO_CLAIM, F06_REFUND_RECIPIENT_MISMATCH, F06_UNKNOWN_WORKER_ENTITLEMENT,
        F06_WRONG_CLAIM_AMOUNT, F06_WRONG_CLAIM_RECIPIENT, INSUFFICIENT_FREE, NON_CANONICAL,
        NOT_FOUND, RETENTION_FULL, STALE_CURSOR, UNAUTHORIZED, WRONG_PHASE, WRONG_ROSTER,
    },
    reward_math::{claim_expiry, conservation_holds, Allocation},
    state::{ActorSlot, ReplayDecision, ReplayRequest, ReplayTable},
    types::{
        AccountId, Amount, AssetId, Digest32, FrozenBinding, Presence, PrincipalId, RequestDigest,
        ResultDigest, RosterDigest, WorkerId, WorkerRosterEntry,
    },
    MAX_PAYOUT_IDENTITIES, MAX_RETAINED_EPOCHS, MAX_WORKERS, SCHEMA_VERSION,
};

pub const FUNDING_POLICY_VERSION: u64 = 1;
pub const LEDGER_BYTES: usize = 206;
pub const SLOT_BYTES: usize = 67;
pub const DICTIONARY_BYTES: usize = MAX_PAYOUT_IDENTITIES * SLOT_BYTES;
pub const EPOCH_HEADER_BYTES: usize = 174;
pub const ENTRY_BYTES: usize = 19;
pub const EPOCH_MAX_BYTES: usize = EPOCH_HEADER_BYTES + MAX_WORKERS * ENTRY_BYTES;
pub const MAX_SLOT_REFERENCES: u16 = 1056;
pub const ALLOCATION_PREIMAGE_MAX_BYTES: usize = 228 + MAX_WORKERS * 80;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum EpochStatus {
    Reserved = 1,
    Terminal = 2,
    Expired = 3,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum RewardOutcome {
    Undecided = 0,
    Allocated = 1,
    NoEligibleScore = 2,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum Disposition {
    Unclaimed = 0,
    Claimed = 1,
    Expired = 2,
}
impl EpochStatus {
    /// # Errors
    /// `NON_CANONICAL` for any value other than 1, 2 or 3.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            1 => Ok(Self::Reserved),
            2 => Ok(Self::Terminal),
            3 => Ok(Self::Expired),
            _ => Err(NON_CANONICAL),
        }
    }
}
impl RewardOutcome {
    /// # Errors
    /// `NON_CANONICAL` for any value other than 0, 1 or 2.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            0 => Ok(Self::Undecided),
            1 => Ok(Self::Allocated),
            2 => Ok(Self::NoEligibleScore),
            _ => Err(NON_CANONICAL),
        }
    }
}
impl Disposition {
    /// # Errors
    /// `NON_CANONICAL` for any value other than 0, 1 or 2.
    pub fn decode(value: u8) -> CodecResult<Self> {
        match value {
            0 => Ok(Self::Unclaimed),
            1 => Ok(Self::Claimed),
            2 => Ok(Self::Expired),
            _ => Err(NON_CANONICAL),
        }
    }
}

fn checked_sum(mut values: impl Iterator<Item = Amount>) -> CodecResult<Amount> {
    values.try_fold(0u128, |acc, v| acc.checked_add(v).ok_or(ARITHMETIC))
}
fn digest_bytes(value: Presence<Digest32>) -> [u8; 32] {
    match value {
        Presence::Present(d) => d.bytes(),
        Presence::Absent => [0; 32],
    }
}
fn read_digest(r: &mut Reader<'_>) -> CodecResult<Presence<Digest32>> {
    let bytes: [u8; 32] = r.fixed()?;
    if bytes == [0; 32] {
        Ok(Presence::Absent)
    } else {
        Ok(Presence::Present(Digest32::new(bytes)?))
    }
}

/// `RewardLedgerV1`: immutable bindings plus the D,P,X,F,R,C counters.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RewardLedger {
    pub asset: AssetId,
    pub account: AccountId,
    pub refund_recipient: AccountId,
    pub tracked_deposits: Amount,
    pub total_claimed: Amount,
    pub tracked_refunds: Amount,
    pub free: Amount,
    pub reserved: Amount,
    pub liability: Amount,
    pub recipient_count: u16,
    pub retained_epochs: u8,
    pub active_reserve: bool,
}
impl RewardLedger {
    /// # Errors
    /// `F06_REFUND_RECIPIENT_MISMATCH` when the refund recipient is the reserve account.
    pub fn new(
        asset: AssetId,
        account: AccountId,
        refund_recipient: AccountId,
    ) -> CodecResult<Self> {
        let ledger = Self {
            asset,
            account,
            refund_recipient,
            tracked_deposits: 0,
            total_claimed: 0,
            tracked_refunds: 0,
            free: 0,
            reserved: 0,
            liability: 0,
            recipient_count: 0,
            retained_epochs: 0,
            active_reserve: false,
        };
        ledger.validate()?;
        Ok(ledger)
    }
    /// # Errors
    /// `F06_REFUND_RECIPIENT_MISMATCH`, `NON_CANONICAL` for out-of-range counts, `ARITHMETIC` or `F06_LEDGER_INVARIANT_VIOLATION` when conservation or the active-reserve flag fails.
    pub fn validate(&self) -> CodecResult<()> {
        if self.refund_recipient == self.account {
            return Err(F06_REFUND_RECIPIENT_MISMATCH);
        }
        if usize::from(self.recipient_count) > MAX_PAYOUT_IDENTITIES
            || usize::from(self.retained_epochs) > MAX_RETAINED_EPOCHS
        {
            return Err(NON_CANONICAL);
        }
        if !conservation_holds(
            self.tracked_deposits,
            self.total_claimed,
            self.tracked_refunds,
            self.free,
            self.reserved,
            self.liability,
        )? || self.active_reserve != (self.reserved != 0)
        {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        Ok(())
    }
    /// Counter effect of one accepted deposit: D and F rise by amount.
    ///
    /// # Errors
    /// `F06_INVALID_AMOUNT` for zero, `ARITHMETIC` on overflow, or any ledger validation error.
    pub fn deposit(&self, amount: Amount) -> CodecResult<Self> {
        self.validate()?;
        if amount == 0 {
            return Err(F06_INVALID_AMOUNT);
        }
        let mut next = *self;
        next.tracked_deposits = self
            .tracked_deposits
            .checked_add(amount)
            .ok_or(ARITHMETIC)?;
        next.free = self.free.checked_add(amount).ok_or(ARITHMETIC)?;
        next.validate()?;
        Ok(next)
    }
    /// Counter effect of reserving one epoch budget: F falls, R rises.
    ///
    /// # Errors
    /// `F06_INVALID_AMOUNT`, `F06_EPOCH_ALREADY_RESERVED`, `INSUFFICIENT_FREE`, or any ledger validation error.
    pub fn reserve(&self, budget: Amount) -> CodecResult<Self> {
        self.validate()?;
        if budget == 0 {
            return Err(F06_INVALID_AMOUNT);
        }
        if self.active_reserve {
            return Err(F06_EPOCH_ALREADY_RESERVED);
        }
        let mut next = *self;
        next.free = self.free.checked_sub(budget).ok_or(INSUFFICIENT_FREE)?;
        next.reserved = budget;
        next.active_reserve = true;
        next.validate()?;
        Ok(next)
    }
    /// Counter effect of terminalization. ALLOCATED moves B from R to C and
    /// returns its expiry; `NO_ELIGIBLE_SCORE` releases B from R to F (expiry 0).
    /// The expiry is computed first, so a height overflow leaves R untouched.
    ///
    /// # Errors
    /// `F06_EPOCH_NOT_RESERVED`, `F06_LEDGER_INVARIANT_VIOLATION` for a budget other than R, `RETENTION_FULL`, `ARITHMETIC` on height or counter overflow, or `F06_AGGREGATION_MISMATCH` for an undecided outcome.
    pub fn terminalize(
        &self,
        budget: Amount,
        outcome: RewardOutcome,
        terminal_height: u64,
    ) -> CodecResult<(Self, u64)> {
        self.validate()?;
        if !self.active_reserve {
            return Err(F06_EPOCH_NOT_RESERVED);
        }
        if budget != self.reserved {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        let mut next = *self;
        next.reserved = 0;
        next.active_reserve = false;
        next.retained_epochs = self.retained_epochs.checked_add(1).ok_or(RETENTION_FULL)?;
        if usize::from(next.retained_epochs) > MAX_RETAINED_EPOCHS {
            return Err(RETENTION_FULL);
        }
        let expiry = match outcome {
            RewardOutcome::Allocated => {
                let expiry = claim_expiry(terminal_height)?;
                next.liability = self.liability.checked_add(budget).ok_or(ARITHMETIC)?;
                expiry
            }
            RewardOutcome::NoEligibleScore => {
                next.free = self.free.checked_add(budget).ok_or(ARITHMETIC)?;
                0
            }
            RewardOutcome::Undecided => return Err(F06_AGGREGATION_MISMATCH),
        };
        next.validate()?;
        Ok((next, expiry))
    }
    /// Aggregate counters must exactly agree with retained rows before any
    /// mutation commits: R = sum RESERVED budgets, C = sum unclaimed allocated
    /// entitlements, one active reserve at most, ascending epoch rows.
    ///
    /// # Errors
    /// `CAPACITY`, `ARITHMETIC`, any row validation error, or `F06_LEDGER_INVARIANT_VIOLATION` when the counters disagree with the rows.
    pub fn check_rows(&self, epochs: &[RewardEpoch]) -> CodecResult<()> {
        self.check_row_iter(epochs.iter())
    }
    fn check_row_iter<'a, I>(&self, epochs: I) -> CodecResult<()>
    where
        I: Iterator<Item = &'a RewardEpoch> + Clone,
    {
        self.validate()?;
        let total = epochs.clone().count();
        if total > MAX_RETAINED_EPOCHS + 1 {
            return Err(CAPACITY);
        }
        let mut previous: Option<u64> = None;
        let mut active = 0usize;
        let mut reserved = 0u128;
        let mut liability = 0u128;
        for epoch in epochs {
            if previous.is_some_and(|p| p >= epoch.epoch) {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
            previous = Some(epoch.epoch);
            epoch.validate()?;
            if epoch.status == EpochStatus::Reserved {
                active += 1;
            }
            reserved = reserved
                .checked_add(epoch.reserved_budget())
                .ok_or(ARITHMETIC)?;
            liability = liability
                .checked_add(epoch.unclaimed_sum()?)
                .ok_or(ARITHMETIC)?;
        }
        if active > 1
            || (active == 1) != self.active_reserve
            || total - active != usize::from(self.retained_epochs)
            || reserved != self.reserved
            || liability != self.liability
        {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        Ok(())
    }
}
/// # Errors
/// Any ledger validation error, or `CAPACITY` when `out` is too short.
pub fn encode_ledger(v: &RewardLedger, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    let mut w = Writer::new(out);
    w.put(v.asset.as_bytes())?;
    w.put(v.account.as_bytes())?;
    w.put(v.refund_recipient.as_bytes())?;
    w.u64(FUNDING_POLICY_VERSION)?;
    for counter in [
        v.tracked_deposits,
        v.total_claimed,
        v.tracked_refunds,
        v.free,
        v.reserved,
        v.liability,
    ] {
        w.u128(counter)?;
    }
    w.u16(v.recipient_count)?;
    w.u8(v.retained_epochs)?;
    w.boolean(v.active_reserve)?;
    w.u16(0)?;
    Ok(w.len())
}
/// # Errors
/// `NON_CANONICAL` for malformed bytes, `F06_FUNDING_POLICY_MISMATCH`, or any ledger validation error.
pub fn decode_ledger(bytes: &[u8]) -> CodecResult<RewardLedger> {
    let mut r = Reader::new(bytes);
    let asset = AssetId::new(r.fixed()?)?;
    let account = AccountId::new(r.fixed()?)?;
    let refund_recipient = AccountId::new(r.fixed()?)?;
    if r.u64()? != FUNDING_POLICY_VERSION {
        return Err(F06_FUNDING_POLICY_MISMATCH);
    }
    let v = RewardLedger {
        asset,
        account,
        refund_recipient,
        tracked_deposits: r.u128()?,
        total_claimed: r.u128()?,
        tracked_refunds: r.u128()?,
        free: r.u128()?,
        reserved: r.u128()?,
        liability: r.u128()?,
        recipient_count: r.u16()?,
        retained_epochs: r.u8()?,
        active_reserve: r.boolean()?,
    };
    r.reserved(2)?;
    r.finish()?;
    v.validate()?;
    Ok(v)
}

/// One occupied (worker, frozen recipient) pair with its retained references.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RecipientSlot {
    pub worker: WorkerId,
    pub recipient: AccountId,
    pub references: u16,
}
/// Fixed 256-slot dictionary; an unused slot is entirely zero on the wire.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RecipientDictionary {
    pub slots: [Option<RecipientSlot>; MAX_PAYOUT_IDENTITIES],
}
impl RecipientDictionary {
    pub const EMPTY: Self = Self {
        slots: [None; MAX_PAYOUT_IDENTITIES],
    };
    #[must_use]
    pub const fn new() -> Self {
        Self::EMPTY
    }
    /// # Errors
    /// `NON_CANONICAL` for an index past 255, `F06_LEDGER_INVARIANT_VIOLATION` for an unused slot.
    pub fn slot(&self, index: u16) -> CodecResult<RecipientSlot> {
        self.slots
            .get(usize::from(index))
            .ok_or(NON_CANONICAL)?
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)
    }
    #[must_use]
    pub fn occupied(&self) -> usize {
        self.slots.iter().filter(|s| s.is_some()).count()
    }
    /// # Errors
    /// `ACCOUNT_BINDING` for a reserve-account recipient, `NON_CANONICAL` for a bad reference count or a duplicate pair.
    pub fn validate(&self, reserve: AccountId) -> CodecResult<()> {
        for (i, slot) in self.slots.iter().enumerate() {
            let Some(slot) = slot else { continue };
            if slot.recipient == reserve {
                return Err(ACCOUNT_BINDING);
            }
            if slot.references == 0 || slot.references > MAX_SLOT_REFERENCES {
                return Err(NON_CANONICAL);
            }
            if self.slots[i + 1..]
                .iter()
                .flatten()
                .any(|o| o.worker == slot.worker && o.recipient == slot.recipient)
            {
                return Err(NON_CANONICAL);
            }
        }
        Ok(())
    }
}
impl Default for RecipientDictionary {
    fn default() -> Self {
        Self::new()
    }
}
/// # Errors
/// Any dictionary validation error, or `CAPACITY` when `out` is too short.
pub fn encode_dictionary(
    v: &RecipientDictionary,
    reserve: AccountId,
    out: &mut [u8],
) -> CodecResult<usize> {
    v.validate(reserve)?;
    let mut w = Writer::new(out);
    for slot in &v.slots {
        match slot {
            Some(s) => {
                w.u8(1)?;
                w.put(s.worker.as_bytes())?;
                w.put(s.recipient.as_bytes())?;
                w.u16(s.references)?;
            }
            None => w.put(&[0; SLOT_BYTES])?,
        }
    }
    Ok(w.len())
}
/// # Errors
/// `NON_CANONICAL` for malformed bytes, or any dictionary validation error.
pub fn decode_dictionary(bytes: &[u8], reserve: AccountId) -> CodecResult<RecipientDictionary> {
    if bytes.len() != DICTIONARY_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(bytes);
    let mut v = RecipientDictionary::new();
    for slot in &mut v.slots {
        if r.boolean()? {
            *slot = Some(RecipientSlot {
                worker: WorkerId::new(r.fixed()?)?,
                recipient: AccountId::new(r.fixed()?)?,
                references: r.u16()?,
            });
        } else {
            r.reserved(SLOT_BYTES - 1)?;
        }
    }
    r.finish()?;
    v.validate(reserve)?;
    Ok(v)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RewardEntry {
    pub slot: u16,
    pub entitlement: Amount,
    pub disposition: Disposition,
}
impl RewardEntry {
    pub const EMPTY: Self = Self {
        slot: 0,
        entitlement: 0,
        disposition: Disposition::Unclaimed,
    };
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ClaimDecision {
    Payable { index: usize, amount: Amount },
    AlreadyApplied(Digest32),
}

/// `RewardEpochV1`: 174-byte header followed by `entry_count` 19-byte entries.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RewardEpoch {
    pub epoch: u64,
    pub status: EpochStatus,
    pub outcome: RewardOutcome,
    pub terminal_height: u64,
    pub expiry_height: u64,
    pub budget: Amount,
    pub paid_sum: Amount,
    pub expired_sum: Amount,
    pub roster: RosterDigest,
    pub aggregation: Presence<Digest32>,
    pub allocation: Presence<Digest32>,
    pub entry_count: u16,
    pub entries: [RewardEntry; MAX_WORKERS],
}
impl RewardEpoch {
    /// Immutable RESERVED row: frozen dictionary slots, zero entitlements.
    ///
    /// # Errors
    /// `CAPACITY` for more than 32 slots, `ARITHMETIC`, or any row validation error.
    pub fn reserved(
        epoch: u64,
        budget: Amount,
        roster: RosterDigest,
        slots: &[u16],
    ) -> CodecResult<Self> {
        if slots.len() > MAX_WORKERS {
            return Err(CAPACITY);
        }
        let mut entries = [RewardEntry::EMPTY; MAX_WORKERS];
        for (entry, slot) in entries.iter_mut().zip(slots) {
            entry.slot = *slot;
        }
        let row = Self {
            epoch,
            status: EpochStatus::Reserved,
            outcome: RewardOutcome::Undecided,
            terminal_height: 0,
            expiry_height: 0,
            budget,
            paid_sum: 0,
            expired_sum: 0,
            roster,
            aggregation: Presence::Absent,
            allocation: Presence::Absent,
            entry_count: u16::try_from(slots.len()).map_err(|_| ARITHMETIC)?,
            entries,
        };
        row.validate()?;
        Ok(row)
    }
    #[must_use]
    pub fn entries(&self) -> &[RewardEntry] {
        &self.entries[..usize::from(self.entry_count).min(MAX_WORKERS)]
    }
    #[must_use]
    pub fn reserved_budget(&self) -> Amount {
        if self.status == EpochStatus::Reserved {
            self.budget
        } else {
            0
        }
    }
    /// Live liability: unclaimed entitlements of a TERMINAL ALLOCATED row.
    ///
    /// # Errors
    /// `ARITHMETIC` on overflow.
    pub fn unclaimed_sum(&self) -> CodecResult<Amount> {
        if self.status != EpochStatus::Terminal {
            return Ok(0);
        }
        checked_sum(
            self.entries()
                .iter()
                .filter(|e| e.disposition == Disposition::Unclaimed)
                .map(|e| e.entitlement),
        )
    }
    fn sum_of(&self, disposition: Disposition) -> CodecResult<Amount> {
        checked_sum(
            self.entries()
                .iter()
                .filter(|e| e.disposition == disposition)
                .map(|e| e.entitlement),
        )
    }
    /// # Errors
    /// `CAPACITY`, `F06_INVALID_AMOUNT` for a zero budget, `ARITHMETIC`, or `NON_CANONICAL` for an inconsistent row.
    pub fn validate(&self) -> CodecResult<()> {
        let count = usize::from(self.entry_count);
        if count > MAX_WORKERS {
            return Err(CAPACITY);
        }
        if self.budget == 0 {
            return Err(F06_INVALID_AMOUNT);
        }
        let entries = self.entries();
        if entries.iter().any(|e| e.slot >= 256)
            || self.entries[count..]
                .iter()
                .any(|e| *e != RewardEntry::EMPTY)
        {
            return Err(NON_CANONICAL);
        }
        let ok = match (self.status, self.outcome) {
            (EpochStatus::Reserved, RewardOutcome::Undecided) => {
                count > 0
                    && self.terminal_height == 0
                    && self.expiry_height == 0
                    && self.paid_sum == 0
                    && self.expired_sum == 0
                    && self.aggregation == Presence::Absent
                    && self.allocation == Presence::Absent
                    && entries
                        .iter()
                        .all(|e| e.entitlement == 0 && e.disposition == Disposition::Unclaimed)
            }
            (EpochStatus::Terminal, RewardOutcome::NoEligibleScore) => {
                count == 0
                    && self.expiry_height == 0
                    && self.paid_sum == 0
                    && self.expired_sum == 0
                    && self.aggregation != Presence::Absent
                    && self.allocation != Presence::Absent
            }
            (EpochStatus::Terminal | EpochStatus::Expired, RewardOutcome::Allocated) => {
                let expired = self.status == EpochStatus::Expired;
                count > 0
                    && self.expiry_height == claim_expiry(self.terminal_height)?
                    && self.aggregation != Presence::Absent
                    && self.allocation != Presence::Absent
                    && checked_sum(entries.iter().map(|e| e.entitlement))? == self.budget
                    && self.paid_sum == self.sum_of(Disposition::Claimed)?
                    && self.expired_sum
                        == if expired {
                            self.sum_of(Disposition::Expired)?
                        } else {
                            0
                        }
                    && entries.iter().all(|e| match e.disposition {
                        Disposition::Unclaimed => !expired,
                        Disposition::Claimed => e.entitlement > 0,
                        Disposition::Expired => expired,
                    })
            }
            _ => false,
        };
        if ok {
            Ok(())
        } else {
            Err(NON_CANONICAL)
        }
    }
    /// Entries must name occupied slots ordered strictly by `worker_id` bytes.
    ///
    /// # Errors
    /// Any row validation error, a dictionary slot error, or `F06_LEDGER_INVARIANT_VIOLATION` for unordered workers.
    pub fn check_dictionary(&self, dictionary: &RecipientDictionary) -> CodecResult<()> {
        self.validate()?;
        let mut previous: Option<WorkerId> = None;
        for entry in self.entries() {
            let worker = dictionary.slot(entry.slot)?.worker;
            if previous.is_some_and(|p| p >= worker) {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
            previous = Some(worker);
        }
        Ok(())
    }
    /// Exact-match claim check against the immutable entry; no field is coerced.
    ///
    /// # Errors
    /// `F06_CLAIM_NOT_READY`, `F06_CLAIM_EXPIRED`, `F06_WRONG_CLAIM_RECIPIENT`, `F06_WRONG_CLAIM_AMOUNT`, `F06_NOTHING_TO_CLAIM`, `F06_UNKNOWN_WORKER_ENTITLEMENT`, or a row/dictionary validation error.
    pub fn check_claim(
        &self,
        dictionary: &RecipientDictionary,
        worker: WorkerId,
        recipient: AccountId,
        amount: Amount,
        height: u64,
    ) -> CodecResult<ClaimDecision> {
        self.check_dictionary(dictionary)?;
        match self.status {
            EpochStatus::Reserved => return Err(F06_CLAIM_NOT_READY),
            EpochStatus::Expired => return Err(F06_CLAIM_EXPIRED),
            EpochStatus::Terminal => {}
        }
        let Presence::Present(allocation) = self.allocation else {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        };
        if self.outcome == RewardOutcome::Allocated && height >= self.expiry_height {
            return Err(F06_CLAIM_EXPIRED);
        }
        for (index, entry) in self.entries().iter().enumerate() {
            let slot = dictionary.slot(entry.slot)?;
            if slot.worker != worker {
                continue;
            }
            if slot.recipient != recipient {
                return Err(F06_WRONG_CLAIM_RECIPIENT);
            }
            if entry.entitlement != amount {
                return Err(F06_WRONG_CLAIM_AMOUNT);
            }
            if amount == 0 {
                return Err(F06_NOTHING_TO_CLAIM);
            }
            return match entry.disposition {
                Disposition::Unclaimed => Ok(ClaimDecision::Payable { index, amount }),
                Disposition::Claimed => Ok(ClaimDecision::AlreadyApplied(entitlement_id(
                    allocation, worker,
                )?)),
                Disposition::Expired => Err(F06_CLAIM_EXPIRED),
            };
        }
        Err(F06_UNKNOWN_WORKER_ENTITLEMENT)
    }
}
/// # Errors
/// Any row validation error, or `CAPACITY` when `out` is too short.
pub fn encode_epoch(v: &RewardEpoch, out: &mut [u8]) -> CodecResult<usize> {
    v.validate()?;
    let mut w = Writer::new(out);
    w.u64(v.epoch)?;
    w.u8(v.status as u8)?;
    w.u8(v.outcome as u8)?;
    w.u16(0)?;
    w.u64(v.terminal_height)?;
    w.u64(v.expiry_height)?;
    w.u128(v.budget)?;
    w.u128(v.paid_sum)?;
    w.u128(v.expired_sum)?;
    w.put(v.roster.as_bytes())?;
    w.put(&digest_bytes(v.aggregation))?;
    w.put(&digest_bytes(v.allocation))?;
    w.u16(v.entry_count)?;
    for e in v.entries() {
        w.u16(e.slot)?;
        w.u128(e.entitlement)?;
        w.u8(e.disposition as u8)?;
    }
    Ok(w.len())
}
/// # Errors
/// `CAPACITY`, `NON_CANONICAL` for malformed bytes, or any row validation error.
pub fn decode_epoch(bytes: &[u8]) -> CodecResult<RewardEpoch> {
    if bytes.len() > EPOCH_MAX_BYTES {
        return Err(CAPACITY);
    }
    let mut r = Reader::new(bytes);
    let epoch = r.u64()?;
    let status = EpochStatus::decode(r.u8()?)?;
    let outcome = RewardOutcome::decode(r.u8()?)?;
    r.reserved(2)?;
    let mut v = RewardEpoch {
        epoch,
        status,
        outcome,
        terminal_height: r.u64()?,
        expiry_height: r.u64()?,
        budget: r.u128()?,
        paid_sum: r.u128()?,
        expired_sum: r.u128()?,
        roster: RosterDigest::new(r.fixed()?)?,
        aggregation: read_digest(&mut r)?,
        allocation: read_digest(&mut r)?,
        entry_count: r.u16()?,
        entries: [RewardEntry::EMPTY; MAX_WORKERS],
    };
    let count = usize::from(v.entry_count);
    if count > MAX_WORKERS {
        return Err(CAPACITY);
    }
    for entry in &mut v.entries[..count] {
        let slot = r.u16()?;
        if slot >= 256 {
            return Err(NON_CANONICAL);
        }
        *entry = RewardEntry {
            slot,
            entitlement: r.u128()?,
            disposition: Disposition::decode(r.u8()?)?,
        };
    }
    r.finish()?;
    v.validate()?;
    Ok(v)
}

/// `H(PAXAI/reward-allocation/v1, ...)`: each hashed entry is
/// `worker_id32 || recipient32 || entitlement:u128` with the frozen roster
/// recipient. Dictionary indexes and claim dispositions never enter the digest.
///
/// # Errors
/// `WRONG_ROSTER`, `F06_UNKNOWN_WORKER_ENTITLEMENT` for a worker outside the roster, `ARITHMETIC`, or a hashing error.
pub fn allocation_digest(
    binding: &FrozenBinding,
    aggregation: Digest32,
    asset: AssetId,
    allocation: &Allocation,
    roster: &[WorkerRosterEntry],
) -> CodecResult<Digest32> {
    if !allocation.is_empty() && roster.len() != allocation.len() {
        return Err(WRONG_ROSTER);
    }
    let mut bytes = [0u8; ALLOCATION_PREIMAGE_MAX_BYTES];
    let mut w = Writer::new(&mut bytes);
    w.u16(SCHEMA_VERSION)?;
    w.put(binding.chain.as_bytes())?;
    w.put(binding.program.as_bytes())?;
    w.put(binding.market.as_bytes())?;
    w.u64(binding.epoch)?;
    w.u64(binding.config.get())?;
    w.put(binding.roster.as_bytes())?;
    w.put(aggregation.as_bytes())?;
    w.put(asset.as_bytes())?;
    w.u128(allocation.budget())?;
    w.u16(u16::try_from(allocation.len()).map_err(|_| ARITHMETIC)?)?;
    for i in 0..allocation.len() {
        let (worker, amount) = allocation.entitlement(i)?;
        let recipient = roster
            .iter()
            .find(|e| e.worker == worker)
            .ok_or(F06_UNKNOWN_WORKER_ENTITLEMENT)?
            .recipient;
        w.put(worker.as_bytes())?;
        w.put(recipient.as_bytes())?;
        w.u128(amount)?;
    }
    let n = w.len();
    domain_hash("PAXAI/reward-allocation/v1", &bytes[..n])
}
/// # Errors
/// A hashing error.
pub fn entitlement_id(allocation: Digest32, worker: WorkerId) -> CodecResult<Digest32> {
    let mut bytes = [0u8; 64];
    bytes[..32].copy_from_slice(allocation.as_bytes());
    bytes[32..].copy_from_slice(worker.as_bytes());
    domain_hash("PAXAI/reward-entitlement/v1", &bytes)
}

pub const FUND_PAYLOAD_BYTES: usize = 57;
pub const CLAIM_PAYLOAD_BYTES: usize = 80;
pub const REFUND_PAYLOAD_BYTES: usize = 64;
pub const LAST_REFUND_BYTES: usize = 96;
pub const EPOCH_SPAN_HEIGHTS: u64 = 128;
pub const MAX_EPOCH_ROWS: usize = MAX_RETAINED_EPOCHS + 1;

/// Fund payload: `amount || fixed_refund_recipient32 || policy:u64 || consent:u8`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FundRequest {
    pub amount: Amount,
    pub refund_recipient: AccountId,
    pub policy_version: u64,
    pub consent: bool,
}
impl FundRequest {
    /// # Errors
    /// `CAPACITY` when `out` is too short.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let mut w = Writer::new(out);
        w.u128(self.amount)?;
        w.put(self.refund_recipient.as_bytes())?;
        w.u64(self.policy_version)?;
        w.boolean(self.consent)?;
        Ok(w.len())
    }
    /// # Errors
    /// `NON_CANONICAL` for a length other than 57 bytes, a zero recipient or a consent byte other than 0 or 1.
    pub fn decode(bytes: &[u8]) -> CodecResult<Self> {
        if bytes.len() != FUND_PAYLOAD_BYTES {
            return Err(NON_CANONICAL);
        }
        let mut r = Reader::new(bytes);
        let v = Self {
            amount: r.u128()?,
            refund_recipient: AccountId::new(r.fixed()?)?,
            policy_version: r.u64()?,
            consent: r.boolean()?,
        };
        r.finish()?;
        Ok(v)
    }
}
/// Claim payload: `worker_id32 || fixed_recipient32 || exact_entitlement:u128`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ClaimRequest {
    pub worker: WorkerId,
    pub recipient: AccountId,
    pub amount: Amount,
}
impl ClaimRequest {
    /// # Errors
    /// `CAPACITY` when `out` is too short.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let mut w = Writer::new(out);
        w.put(self.worker.as_bytes())?;
        w.put(self.recipient.as_bytes())?;
        w.u128(self.amount)?;
        Ok(w.len())
    }
    /// # Errors
    /// `NON_CANONICAL` for a length other than 80 bytes or a zero identity.
    pub fn decode(bytes: &[u8]) -> CodecResult<Self> {
        if bytes.len() != CLAIM_PAYLOAD_BYTES {
            return Err(NON_CANONICAL);
        }
        let mut r = Reader::new(bytes);
        let v = Self {
            worker: WorkerId::new(r.fixed()?)?,
            recipient: AccountId::new(r.fixed()?)?,
            amount: r.u128()?,
        };
        r.finish()?;
        Ok(v)
    }
}
/// `RefundFree` payload: `expected_refunded_total || amount || fixed_refund_recipient32`.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RefundRequest {
    pub expected_refunded: Amount,
    pub amount: Amount,
    pub recipient: AccountId,
}
impl RefundRequest {
    /// # Errors
    /// `CAPACITY` when `out` is too short.
    pub fn encode(&self, out: &mut [u8]) -> CodecResult<usize> {
        let mut w = Writer::new(out);
        w.u128(self.expected_refunded)?;
        w.u128(self.amount)?;
        w.put(self.recipient.as_bytes())?;
        Ok(w.len())
    }
    /// # Errors
    /// `NON_CANONICAL` for a length other than 64 bytes or a zero recipient.
    pub fn decode(bytes: &[u8]) -> CodecResult<Self> {
        if bytes.len() != REFUND_PAYLOAD_BYTES {
            return Err(NON_CANONICAL);
        }
        let mut r = Reader::new(bytes);
        let v = Self {
            expected_refunded: r.u128()?,
            amount: r.u128()?,
            recipient: AccountId::new(r.fixed()?)?,
        };
        r.finish()?;
        Ok(v)
    }
}

/// The single retained refund cursor record (96 bytes plus presence byte).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LastRefund {
    pub prior_refunded: Amount,
    pub amount: Amount,
    pub request: RequestDigest,
    pub result: ResultDigest,
}
/// # Errors
/// `CAPACITY` when `out` is too short.
pub fn encode_last_refund(v: Option<&LastRefund>, out: &mut [u8]) -> CodecResult<usize> {
    let mut w = Writer::new(out);
    match v {
        Some(v) => {
            w.u8(1)?;
            w.u128(v.prior_refunded)?;
            w.u128(v.amount)?;
            w.put(v.request.as_bytes())?;
            w.put(v.result.as_bytes())?;
        }
        None => w.put(&[0; 1 + LAST_REFUND_BYTES])?,
    }
    Ok(w.len())
}
/// # Errors
/// `NON_CANONICAL` for malformed bytes, a zero digest or a zero amount.
pub fn decode_last_refund(bytes: &[u8]) -> CodecResult<Option<LastRefund>> {
    let mut r = Reader::new(bytes);
    let v = if r.boolean()? {
        let v = LastRefund {
            prior_refunded: r.u128()?,
            amount: r.u128()?,
            request: RequestDigest::new(r.fixed()?)?,
            result: ResultDigest::new(r.fixed()?)?,
        };
        if v.amount == 0 {
            return Err(NON_CANONICAL);
        }
        Some(v)
    } else {
        r.reserved(LAST_REFUND_BYTES)?;
        None
    };
    r.finish()?;
    Ok(v)
}

/// Immutable F01 funding principals: the owner and the optional treasury.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FundingAuthority {
    pub owner: PrincipalId,
    pub treasury: Presence<PrincipalId>,
}
impl FundingAuthority {
    /// Principal identity comes from the authenticated replay request, never
    /// from payload bytes; only the owner slot or the distinct creation-time
    /// treasury slot may fund.
    ///
    /// # Errors
    /// `UNAUTHORIZED` for any principal or slot other than the owner or the distinct present treasury.
    pub fn check(&self, request: &ReplayRequest) -> CodecResult<()> {
        let ok = if request.slot == ActorSlot::OWNER {
            request.principal == self.owner
        } else if request.slot == ActorSlot::TREASURY {
            matches!(self.treasury, Presence::Present(t) if t == request.principal && t != self.owner)
        } else {
            false
        };
        if ok {
            Ok(())
        } else {
            Err(UNAUTHORIZED)
        }
    }
}

/// Role-sequence replay inputs of one Fund request: the shared replay table,
/// the authenticated request, the execution height, the state revision and
/// the result digest retained on success.
#[derive(Debug)]
pub struct FundReplay<'a> {
    pub table: &'a mut ReplayTable,
    pub request: &'a ReplayRequest,
    pub height: u64,
    pub revision: &'a mut u64,
    pub result: ResultDigest,
}

/// Market lifecycle as seen by the reward ledger.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FundingPhase {
    Accepting,
    Closing,
}

/// The single native effect a committed transition stages; the real Program
/// activity applies it in the same atomic commit as the returned state.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RewardEffect {
    NoTransfer,
    Deposit {
        principal: PrincipalId,
        amount: Amount,
    },
    Payout {
        recipient: AccountId,
        amount: Amount,
    },
    Released(Amount),
    Pruned(Presence<Digest32>),
    AlreadyApplied(Digest32),
    ReplayedFund(ResultDigest),
    RepeatedRefund(ResultDigest),
}

/// Complete F06 reward state: ledger header, frozen recipient dictionary,
/// ascending retained epoch rows and the refund cursor record.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RewardState {
    pub ledger: RewardLedger,
    pub dictionary: RecipientDictionary,
    pub epochs: [Option<RewardEpoch>; MAX_EPOCH_ROWS],
    pub last_refund: Option<LastRefund>,
}
impl RewardState {
    const NO_ROWS: [Option<RewardEpoch>; MAX_EPOCH_ROWS] = [None; MAX_EPOCH_ROWS];
    /// # Errors
    /// Any state invariant error.
    pub fn new(ledger: RewardLedger) -> CodecResult<Self> {
        let state = Self {
            ledger,
            dictionary: RecipientDictionary::new(),
            epochs: Self::NO_ROWS,
            last_refund: None,
        };
        state.check()?;
        Ok(state)
    }
    pub fn rows(&self) -> impl Iterator<Item = &RewardEpoch> + Clone {
        self.epochs.iter().flatten()
    }
    /// # Errors
    /// `NOT_FOUND` for an unknown or pruned epoch.
    pub fn row(&self, epoch: u64) -> CodecResult<&RewardEpoch> {
        self.rows().find(|r| r.epoch == epoch).ok_or(NOT_FOUND)
    }
    fn row_index(&self, epoch: u64) -> CodecResult<usize> {
        self.epochs
            .iter()
            .position(|r| r.is_some_and(|r| r.epoch == epoch))
            .ok_or(NOT_FOUND)
    }
    fn completed(&self) -> usize {
        self.rows()
            .filter(|r| r.status != EpochStatus::Reserved)
            .count()
    }
    /// Full invariant: compact ascending rows, ledger counters equal to the
    /// rows, every entry naming an occupied slot, and each slot reference
    /// count equal to the retained entries that name it.
    ///
    /// # Errors
    /// `F06_LEDGER_INVARIANT_VIOLATION` when rows, counters, dictionary references or the refund record disagree, or any ledger, row or dictionary validation error.
    pub fn check(&self) -> CodecResult<()> {
        let rows = self.rows().count();
        if self.epochs[rows..].iter().any(Option::is_some) {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        self.ledger.check_row_iter(self.rows())?;
        self.dictionary.validate(self.ledger.account)?;
        if self.dictionary.occupied() != usize::from(self.ledger.recipient_count) {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        let mut references = [0u16; MAX_PAYOUT_IDENTITIES];
        for row in self.rows() {
            row.check_dictionary(&self.dictionary)?;
            for entry in row.entries() {
                let count = &mut references[usize::from(entry.slot)];
                *count = count.checked_add(1).ok_or(ARITHMETIC)?;
            }
        }
        for (slot, count) in self.dictionary.slots.iter().zip(references) {
            if slot.map_or(0, |s| s.references) != count {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
        }
        if let Some(last) = self.last_refund {
            let end = last
                .prior_refunded
                .checked_add(last.amount)
                .ok_or(ARITHMETIC)?;
            if last.amount == 0 || end > self.ledger.tracked_refunds {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
        }
        Ok(())
    }
    fn commit(self, effect: RewardEffect) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        Ok((self, effect))
    }
    fn release(&mut self, slot: u16) -> CodecResult<()> {
        let entry = self
            .dictionary
            .slots
            .get_mut(usize::from(slot))
            .ok_or(NON_CANONICAL)?;
        let mut current = entry.ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        current.references = current
            .references
            .checked_sub(1)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        if current.references == 0 {
            *entry = None;
            self.ledger.recipient_count = self
                .ledger
                .recipient_count
                .checked_sub(1)
                .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        } else {
            *entry = Some(current);
        }
        Ok(())
    }
    fn admit(&mut self, worker: WorkerId, recipient: AccountId) -> CodecResult<u16> {
        if recipient == self.ledger.account {
            return Err(ACCOUNT_BINDING);
        }
        if let Some(i) = self
            .dictionary
            .slots
            .iter()
            .position(|s| s.is_some_and(|s| s.worker == worker && s.recipient == recipient))
        {
            let slot = self.dictionary.slots[i].as_mut().ok_or(NON_CANONICAL)?;
            if slot.references >= MAX_SLOT_REFERENCES {
                return Err(CAPACITY);
            }
            slot.references += 1;
            return u16::try_from(i).map_err(|_| ARITHMETIC);
        }
        let i = self
            .dictionary
            .slots
            .iter()
            .position(Option::is_none)
            .ok_or(CAPACITY)?;
        self.dictionary.slots[i] = Some(RecipientSlot {
            worker,
            recipient,
            references: 1,
        });
        self.ledger.recipient_count = self
            .ledger
            .recipient_count
            .checked_add(1)
            .ok_or(ARITHMETIC)?;
        u16::try_from(i).map_err(|_| ARITHMETIC)
    }
    fn remove_oldest(&mut self) -> CodecResult<Presence<Digest32>> {
        let oldest = self.epochs[0].ok_or(NOT_FOUND)?;
        let prunable = match oldest.status {
            EpochStatus::Reserved => false,
            EpochStatus::Expired => true,
            EpochStatus::Terminal => oldest.unclaimed_sum()? == 0,
        };
        if !prunable {
            return Err(WRONG_PHASE);
        }
        for entry in oldest.entries() {
            self.release(entry.slot)?;
        }
        self.epochs.copy_within(1.., 0);
        self.epochs[MAX_EPOCH_ROWS - 1] = None;
        self.ledger.retained_epochs = self
            .ledger
            .retained_epochs
            .checked_sub(1)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        Ok(oldest.allocation)
    }

    /// Fund (0x0601): authenticated owner/treasury, policy 1, explicit
    /// consent and the immutable refund recipient. The replay record and
    /// revision move only together with D and F; an accepted repeat stages
    /// no deposit.
    ///
    /// # Errors
    /// `UNAUTHORIZED`, `F06_FUNDING_POLICY_MISMATCH`,
    /// `F06_CONTRIBUTION_CONSENT_REQUIRED`, `F06_REFUND_RECIPIENT_MISMATCH`,
    /// `WRONG_PHASE`, `F06_INVALID_AMOUNT`, `ARITHMETIC` and the common replay
    /// refusals; on any error neither the state nor the replay table changes.
    pub fn fund(
        &self,
        authority: &FundingAuthority,
        phase: FundingPhase,
        payload: &FundRequest,
        replay: &mut FundReplay<'_>,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        authority.check(replay.request)?;
        if payload.policy_version != FUNDING_POLICY_VERSION {
            return Err(F06_FUNDING_POLICY_MISMATCH);
        }
        if !payload.consent {
            return Err(F06_CONTRIBUTION_CONSENT_REQUIRED);
        }
        if payload.refund_recipient != self.ledger.refund_recipient {
            return Err(F06_REFUND_RECIPIENT_MISMATCH);
        }
        if let ReplayDecision::AlreadyApplied(last) =
            replay.table.check(replay.request, replay.height)?
        {
            return Ok((self.clone(), RewardEffect::ReplayedFund(last.result_digest)));
        }
        if phase != FundingPhase::Accepting {
            return Err(WRONG_PHASE);
        }
        let mut next = self.clone();
        next.ledger = self.ledger.deposit(payload.amount)?;
        next.check()?;
        let mut table = replay.table.clone();
        let mut revision = *replay.revision;
        table.record_success(replay.request, replay.height, &mut revision, replay.result)?;
        *replay.table = table;
        *replay.revision = revision;
        Ok((
            next,
            RewardEffect::Deposit {
                principal: replay.request.principal,
                amount: payload.amount,
            },
        ))
    }

    /// `ReserveEpoch`: internal to `OpenEpoch`. Prunes the oldest completed row
    /// only when the ring is full and that row holds no unpaid liability,
    /// preflights every frozen recipient into the dictionary (`WorkerId`s
    /// ascending), then moves budget from F to R in one row.
    ///
    /// # Errors
    /// `F06_EPOCH_ALREADY_RESERVED`, `WRONG_ROSTER`, `WRONG_PHASE` for a non-increasing epoch, `ARITHMETIC` when the eventual expiry height overflows, `INSUFFICIENT_FREE`, `F06_INVALID_AMOUNT`, `RETENTION_FULL` when the oldest completed row still holds liability, `ACCOUNT_BINDING`, `CAPACITY` when the dictionary is full, or any state invariant error.
    pub fn reserve_epoch(
        &self,
        epoch: u64,
        budget: Amount,
        roster_digest: RosterDigest,
        roster: &[WorkerRosterEntry],
        opening_height: u64,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        if self.ledger.active_reserve {
            return Err(F06_EPOCH_ALREADY_RESERVED);
        }
        if roster.is_empty() || roster.len() > MAX_WORKERS {
            return Err(WRONG_ROSTER);
        }
        if roster.windows(2).any(|p| p[0].worker >= p[1].worker) {
            return Err(WRONG_ROSTER);
        }
        if self.rows().last().is_some_and(|r| r.epoch >= epoch) {
            return Err(WRONG_PHASE);
        }
        claim_expiry(
            opening_height
                .checked_add(EPOCH_SPAN_HEIGHTS)
                .ok_or(ARITHMETIC)?,
        )?;
        if budget > self.ledger.free {
            return Err(INSUFFICIENT_FREE);
        }
        let mut next = self.clone();
        let mut effect = RewardEffect::NoTransfer;
        if next.completed() >= MAX_RETAINED_EPOCHS {
            effect = RewardEffect::Pruned(next.remove_oldest().map_err(|e| {
                if e == WRONG_PHASE {
                    RETENTION_FULL
                } else {
                    e
                }
            })?);
        }
        let mut slots = [0u16; MAX_WORKERS];
        for (slot, entry) in slots.iter_mut().zip(roster) {
            *slot = next.admit(entry.worker, entry.recipient)?;
        }
        next.ledger = next.ledger.reserve(budget)?;
        let index = next.rows().count();
        next.epochs[index] = Some(RewardEpoch::reserved(
            epoch,
            budget,
            roster_digest,
            &slots[..roster.len()],
        )?);
        next.commit(effect)
    }

    /// `TerminalizeRewards`: internal to `FinalizeAggregation`, bound to the
    /// immutable F05 terminal result and the frozen roster. ALLOCATED moves B
    /// from R to C; `NO_ELIGIBLE_SCORE` releases B to F and the slot references.
    ///
    /// # Errors
    /// `NOT_FOUND`, `F06_EPOCH_TERMINAL`, `WRONG_ROSTER`, `F06_AGGREGATION_MISMATCH`, `ARITHMETIC` on height overflow with R preserved, `RETENTION_FULL`, or any state invariant error.
    pub fn terminalize(
        &self,
        binding: &FrozenBinding,
        aggregation: Digest32,
        allocation: &Allocation,
        roster: &[WorkerRosterEntry],
        height: u64,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        let index = self.row_index(binding.epoch)?;
        let row = self.epochs[index].ok_or(NOT_FOUND)?;
        if row.status != EpochStatus::Reserved {
            return Err(F06_EPOCH_TERMINAL);
        }
        if binding.roster != row.roster || roster.len() != row.entries().len() {
            return Err(WRONG_ROSTER);
        }
        for (entry, member) in row.entries().iter().zip(roster) {
            let slot = self.dictionary.slot(entry.slot)?;
            if slot.worker != member.worker || slot.recipient != member.recipient {
                return Err(WRONG_ROSTER);
            }
        }
        if allocation.budget() != row.budget {
            return Err(F06_AGGREGATION_MISMATCH);
        }
        let outcome = allocation.outcome();
        let digest =
            allocation_digest(binding, aggregation, self.ledger.asset, allocation, roster)?;
        let mut next = self.clone();
        let (ledger, expiry) = self.ledger.terminalize(row.budget, outcome, height)?;
        next.ledger = ledger;
        let mut terminal = row;
        terminal.status = EpochStatus::Terminal;
        terminal.outcome = outcome;
        terminal.terminal_height = height;
        terminal.expiry_height = expiry;
        terminal.aggregation = Presence::Present(aggregation);
        terminal.allocation = Presence::Present(digest);
        match outcome {
            RewardOutcome::Allocated => {
                if allocation.len() != row.entries().len() {
                    return Err(F06_AGGREGATION_MISMATCH);
                }
                for (i, entry) in terminal
                    .entries
                    .iter_mut()
                    .take(allocation.len())
                    .enumerate()
                {
                    let (worker, amount) = allocation.entitlement(i)?;
                    if worker != roster[i].worker {
                        return Err(F06_AGGREGATION_MISMATCH);
                    }
                    entry.entitlement = amount;
                }
            }
            RewardOutcome::NoEligibleScore => {
                for entry in row.entries() {
                    next.release(entry.slot)?;
                }
                terminal.entry_count = 0;
                terminal.entries = [RewardEntry::EMPTY; MAX_WORKERS];
            }
            RewardOutcome::Undecided => return Err(F06_AGGREGATION_MISMATCH),
        }
        next.epochs[index] = Some(terminal);
        next.commit(RewardEffect::NoTransfer)
    }

    /// `DeclinePendingEpoch`: only an epoch that never opened may be declined;
    /// it spends nothing and an opened epoch can never be cancelled.
    ///
    /// # Errors
    /// `WRONG_PHASE` once the epoch has opened, or any state invariant error.
    pub fn decline_pending_epoch(&self, epoch: u64) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        if self.rows().any(|r| r.epoch >= epoch) {
            return Err(WRONG_PHASE);
        }
        Ok((self.clone(), RewardEffect::NoTransfer))
    }

    /// Claim (0x0602): exact fixed recipient and amount; pays at most once.
    ///
    /// # Errors
    /// `NOT_FOUND` for an unknown or pruned epoch, the claim refusals of `RewardEpoch::check_claim`, `ARITHMETIC`, or any state invariant error.
    pub fn claim(
        &self,
        epoch: u64,
        request: &ClaimRequest,
        height: u64,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        let index = self.row_index(epoch)?;
        let row = self.epochs[index].ok_or(NOT_FOUND)?;
        match row.check_claim(
            &self.dictionary,
            request.worker,
            request.recipient,
            request.amount,
            height,
        )? {
            ClaimDecision::AlreadyApplied(id) => {
                Ok((self.clone(), RewardEffect::AlreadyApplied(id)))
            }
            ClaimDecision::Payable {
                index: entry,
                amount,
            } => {
                let mut next = self.clone();
                let mut paid = row;
                paid.entries[entry].disposition = Disposition::Claimed;
                paid.paid_sum = row.paid_sum.checked_add(amount).ok_or(ARITHMETIC)?;
                next.epochs[index] = Some(paid);
                next.ledger.liability = self
                    .ledger
                    .liability
                    .checked_sub(amount)
                    .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
                next.ledger.total_claimed = self
                    .ledger
                    .total_claimed
                    .checked_add(amount)
                    .ok_or(ARITHMETIC)?;
                next.commit(RewardEffect::Payout {
                    recipient: request.recipient,
                    amount,
                })
            }
        }
    }

    /// `ExpireEpochClaims` (0x0603): at or after expiry, every unpaid entry
    /// becomes EXPIRED and exactly their sum moves from C to F.
    ///
    /// # Errors
    /// `NOT_FOUND`, `WRONG_PHASE` for a row that is not allocated or not yet at expiry, `ARITHMETIC`, or any state invariant error.
    pub fn expire_epoch_claims(
        &self,
        epoch: u64,
        height: u64,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        let index = self.row_index(epoch)?;
        let row = self.epochs[index].ok_or(NOT_FOUND)?;
        if row.outcome != RewardOutcome::Allocated {
            return Err(WRONG_PHASE);
        }
        if row.status == EpochStatus::Expired {
            let Presence::Present(digest) = row.allocation else {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            };
            return Ok((self.clone(), RewardEffect::AlreadyApplied(digest)));
        }
        if height < row.expiry_height {
            return Err(WRONG_PHASE);
        }
        let released = row.unclaimed_sum()?;
        let mut expired = row;
        expired.status = EpochStatus::Expired;
        for entry in expired
            .entries
            .iter_mut()
            .take(usize::from(row.entry_count))
        {
            if entry.disposition == Disposition::Unclaimed {
                entry.disposition = Disposition::Expired;
            }
        }
        expired.expired_sum = released;
        let mut next = self.clone();
        next.epochs[index] = Some(expired);
        next.ledger.liability = self
            .ledger
            .liability
            .checked_sub(released)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        next.ledger.free = self.ledger.free.checked_add(released).ok_or(ARITHMETIC)?;
        next.commit(RewardEffect::Released(released))
    }

    /// `RefundFree` (0x0604): permissionless while closing, monotonic X cursor,
    /// fixed recipient, never touching R or C.
    ///
    /// # Errors
    /// `WRONG_PHASE` outside closing, `F06_REFUND_RECIPIENT_MISMATCH`, `STALE_CURSOR`, `F06_INVALID_AMOUNT`, `INSUFFICIENT_FREE`, `ARITHMETIC`, or any state invariant error.
    pub fn refund_free(
        &self,
        phase: FundingPhase,
        request: &RefundRequest,
        request_digest: RequestDigest,
        result: ResultDigest,
    ) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        if phase != FundingPhase::Closing {
            return Err(WRONG_PHASE);
        }
        if request.recipient != self.ledger.refund_recipient {
            return Err(F06_REFUND_RECIPIENT_MISMATCH);
        }
        if let Some(last) = self.last_refund {
            if last.prior_refunded == request.expected_refunded && last.amount == request.amount {
                return Ok((self.clone(), RewardEffect::RepeatedRefund(last.result)));
            }
        }
        if request.expected_refunded != self.ledger.tracked_refunds {
            return Err(STALE_CURSOR);
        }
        if request.amount == 0 {
            return Err(F06_INVALID_AMOUNT);
        }
        let mut next = self.clone();
        next.ledger.free = self
            .ledger
            .free
            .checked_sub(request.amount)
            .ok_or(INSUFFICIENT_FREE)?;
        next.ledger.tracked_refunds = self
            .ledger
            .tracked_refunds
            .checked_add(request.amount)
            .ok_or(ARITHMETIC)?;
        next.last_refund = Some(LastRefund {
            prior_refunded: request.expected_refunded,
            amount: request.amount,
            request: request_digest,
            result,
        });
        next.commit(RewardEffect::Payout {
            recipient: self.ledger.refund_recipient,
            amount: request.amount,
        })
    }

    /// `PruneEpoch` (0x0605): only the oldest completed row without unpaid
    /// liability; releases its dictionary references, no counter change.
    ///
    /// # Errors
    /// `NOT_FOUND`, `WRONG_PHASE` for a younger row, a reserved row or a row with unpaid liability, or any state invariant error.
    pub fn prune_epoch(&self, epoch: u64) -> CodecResult<(Self, RewardEffect)> {
        self.check()?;
        if self.row_index(epoch)? != 0 {
            return Err(WRONG_PHASE);
        }
        let mut next = self.clone();
        let digest = next.remove_oldest()?;
        next.commit(RewardEffect::Pruned(digest))
    }
}
