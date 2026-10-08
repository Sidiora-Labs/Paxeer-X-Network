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
        self.check_row_iter(epochs.iter().copied().map(Ok))
    }
    fn check_row_iter<I>(&self, epochs: I) -> CodecResult<()>
    where
        I: Iterator<Item = CodecResult<RewardEpoch>> + Clone,
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
            let epoch = epoch?;
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
/// Borrowed view over the fixed 256-slot dictionary bytes. Slot `i` occupies
/// bytes `[67 * i, 67 * (i + 1))`; an unused slot is entirely zero.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RecipientDictionary<'a> {
    bytes: &'a [u8],
}
impl<'a> RecipientDictionary<'a> {
    /// Writes the empty layout (every slot unused) in place.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `out` is not exactly 17152 bytes.
    pub fn init_empty(out: &mut [u8]) -> CodecResult<()> {
        if out.len() != DICTIONARY_BYTES {
            return Err(NON_CANONICAL);
        }
        out.fill(0);
        Ok(())
    }
    /// Writes one slot in place: `occupied:u8 || worker32 || recipient32 ||
    /// references:u16`, or 67 zero bytes for an unused slot.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `out` is not exactly 17152 bytes or the index is past 255.
    pub fn write_slot(out: &mut [u8], index: u16, slot: Option<&RecipientSlot>) -> CodecResult<()> {
        if out.len() != DICTIONARY_BYTES {
            return Err(NON_CANONICAL);
        }
        let target = out
            .chunks_exact_mut(SLOT_BYTES)
            .nth(usize::from(index))
            .ok_or(NON_CANONICAL)?;
        target.fill(0);
        if let Some(s) = slot {
            let mut w = Writer::new(target);
            w.u8(1)?;
            w.put(s.worker.as_bytes())?;
            w.put(s.recipient.as_bytes())?;
            w.u16(s.references)?;
        }
        Ok(())
    }
    /// Structural view; slot contents are checked by `slot`, `get` and `validate`.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `bytes` is not exactly 17152 bytes.
    pub fn new(bytes: &'a [u8]) -> CodecResult<Self> {
        if bytes.len() != DICTIONARY_BYTES {
            return Err(NON_CANONICAL);
        }
        Ok(Self { bytes })
    }
    #[must_use]
    pub fn bytes(&self) -> &'a [u8] {
        self.bytes
    }
    fn raw(&self) -> impl Iterator<Item = (u16, &'a [u8])> + 'a {
        (0u16..).zip(self.bytes.chunks_exact(SLOT_BYTES))
    }
    /// # Errors
    /// `NON_CANONICAL` for an index past 255 or a malformed slot.
    pub fn get(&self, index: u16) -> CodecResult<Option<RecipientSlot>> {
        let bytes = self
            .bytes
            .chunks_exact(SLOT_BYTES)
            .nth(usize::from(index))
            .ok_or(NON_CANONICAL)?;
        let mut r = Reader::new(bytes);
        let slot = if r.boolean()? {
            Some(RecipientSlot {
                worker: WorkerId::new(r.fixed()?)?,
                recipient: AccountId::new(r.fixed()?)?,
                references: r.u16()?,
            })
        } else {
            r.reserved(SLOT_BYTES - 1)?;
            None
        };
        r.finish()?;
        Ok(slot)
    }
    /// # Errors
    /// `NON_CANONICAL` for an index past 255 or a malformed slot, `F06_LEDGER_INVARIANT_VIOLATION` for an unused slot.
    pub fn slot(&self, index: u16) -> CodecResult<RecipientSlot> {
        self.get(index)?.ok_or(F06_LEDGER_INVARIANT_VIOLATION)
    }
    #[must_use]
    pub fn occupied(&self) -> usize {
        self.raw().filter(|(_, s)| s[0] == 1).count()
    }
    /// Index of the occupied slot holding exactly this pair.
    ///
    /// # Errors
    /// `NON_CANONICAL` for a malformed slot.
    pub fn find(&self, worker: WorkerId, recipient: AccountId) -> CodecResult<Option<u16>> {
        for (index, _) in self.raw() {
            if self
                .get(index)?
                .is_some_and(|s| s.worker == worker && s.recipient == recipient)
            {
                return Ok(Some(index));
            }
        }
        Ok(None)
    }
    /// Lowest unused slot index.
    #[must_use]
    pub fn first_free(&self) -> Option<u16> {
        self.raw()
            .find(|(_, s)| s.iter().all(|b| *b == 0))
            .map(|(index, _)| index)
    }
    /// # Errors
    /// `NON_CANONICAL` for a malformed slot, a bad reference count or a duplicate pair, `ACCOUNT_BINDING` for a reserve-account recipient.
    pub fn validate(&self, reserve: AccountId) -> CodecResult<()> {
        for (index, bytes) in self.raw() {
            let Some(slot) = self.get(index)? else {
                continue;
            };
            if slot.recipient == reserve {
                return Err(ACCOUNT_BINDING);
            }
            if slot.references == 0 || slot.references > MAX_SLOT_REFERENCES {
                return Err(NON_CANONICAL);
            }
            if self
                .raw()
                .skip(usize::from(index) + 1)
                .any(|(_, other)| other[0] == 1 && other[1..65] == bytes[1..65])
            {
                return Err(NON_CANONICAL);
            }
        }
        Ok(())
    }
}
/// # Errors
/// Any dictionary validation error, or `CAPACITY` when `out` is too short.
pub fn encode_dictionary(
    v: &RecipientDictionary<'_>,
    reserve: AccountId,
    out: &mut [u8],
) -> CodecResult<usize> {
    v.validate(reserve)?;
    let mut w = Writer::new(out);
    w.put(v.bytes)?;
    Ok(w.len())
}
/// Borrowed, fully validated view over the dictionary bytes.
///
/// # Errors
/// `NON_CANONICAL` for malformed bytes, or any dictionary validation error.
pub fn decode_dictionary(bytes: &[u8], reserve: AccountId) -> CodecResult<RecipientDictionary<'_>> {
    let v = RecipientDictionary::new(bytes)?;
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
    pub fn check_dictionary(&self, dictionary: &RecipientDictionary<'_>) -> CodecResult<()> {
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
        dictionary: &RecipientDictionary<'_>,
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

pub const EPOCH_ROWS_BYTES: usize = MAX_EPOCH_ROWS * EPOCH_MAX_BYTES;
pub const LAST_REFUND_RECORD_BYTES: usize = 1 + LAST_REFUND_BYTES;
/// `ledger || dictionary || 33 epoch rows || last-refund record`: 43261 bytes,
/// inside F06's narrowed 49152-byte share of the joint F05/F06 section.
pub const REWARD_STATE_BYTES: usize =
    LEDGER_BYTES + DICTIONARY_BYTES + EPOCH_ROWS_BYTES + LAST_REFUND_RECORD_BYTES;
const DICTIONARY_AT: usize = LEDGER_BYTES;
const ROWS_AT: usize = DICTIONARY_AT + DICTIONARY_BYTES;
const LAST_REFUND_AT: usize = ROWS_AT + EPOCH_ROWS_BYTES;

fn unused(bytes: &[u8]) -> bool {
    bytes.iter().all(|b| *b == 0)
}

/// Borrowed view over the fixed 33-row epoch region. Row `i` occupies bytes
/// `[782 * i, 782 * (i + 1))`: an unused row is entirely zero, a used row is
/// its canonical `RewardEpochV1` encoding followed by zero padding.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct EpochRows<'a> {
    bytes: &'a [u8],
}
impl<'a> EpochRows<'a> {
    /// Writes the empty layout (every row unused) in place.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `out` is not exactly 25806 bytes.
    pub fn init_empty(out: &mut [u8]) -> CodecResult<()> {
        if out.len() != EPOCH_ROWS_BYTES {
            return Err(NON_CANONICAL);
        }
        out.fill(0);
        Ok(())
    }
    fn write(out: &mut [u8], index: usize, row: Option<&RewardEpoch>) -> CodecResult<()> {
        if out.len() != EPOCH_ROWS_BYTES {
            return Err(NON_CANONICAL);
        }
        let target = out
            .chunks_exact_mut(EPOCH_MAX_BYTES)
            .nth(index)
            .ok_or(CAPACITY)?;
        target.fill(0);
        if let Some(row) = row {
            encode_epoch(row, target)?;
        }
        Ok(())
    }
    /// Structural view; row contents are checked by `get`.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `bytes` is not exactly 25806 bytes.
    pub fn new(bytes: &'a [u8]) -> CodecResult<Self> {
        if bytes.len() != EPOCH_ROWS_BYTES {
            return Err(NON_CANONICAL);
        }
        Ok(Self { bytes })
    }
    /// Number of leading used rows.
    #[must_use]
    pub fn used(&self) -> usize {
        self.bytes
            .chunks_exact(EPOCH_MAX_BYTES)
            .take_while(|row| !unused(row))
            .count()
    }
    /// # Errors
    /// `NOT_FOUND` for an index past 32, `CAPACITY` or `NON_CANONICAL` for a malformed row, or any row validation error.
    pub fn get(&self, index: usize) -> CodecResult<Option<RewardEpoch>> {
        let row = self
            .bytes
            .chunks_exact(EPOCH_MAX_BYTES)
            .nth(index)
            .ok_or(NOT_FOUND)?;
        if unused(row) {
            return Ok(None);
        }
        let count = usize::from(u16::from_be_bytes([
            row[EPOCH_HEADER_BYTES - 2],
            row[EPOCH_HEADER_BYTES - 1],
        ]));
        if count > MAX_WORKERS {
            return Err(CAPACITY);
        }
        let end = EPOCH_HEADER_BYTES + count * ENTRY_BYTES;
        if !unused(&row[end..]) {
            return Err(NON_CANONICAL);
        }
        decode_epoch(&row[..end]).map(Some)
    }
    /// Decoded used rows in ascending storage order.
    pub fn records(&self) -> impl Iterator<Item = CodecResult<RewardEpoch>> + Clone + 'a {
        let rows = *self;
        (0..MAX_EPOCH_ROWS).map_while(move |index| rows.get(index).transpose())
    }
}

/// Borrowed, fully validated view over one complete F06 reward state:
/// ledger header, frozen recipient dictionary, ascending retained epoch rows
/// and the refund cursor record. Transitions never touch these bytes; each
/// writes the whole next state into a caller buffer of the same size.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RewardState<'a> {
    bytes: &'a [u8],
}
/// Borrowed, fully validated reward state view.
///
/// # Errors
/// `NON_CANONICAL` when `bytes` is not exactly 43261 bytes, or any state invariant error.
pub fn decode_reward_state(bytes: &[u8]) -> CodecResult<RewardState<'_>> {
    if bytes.len() != REWARD_STATE_BYTES {
        return Err(NON_CANONICAL);
    }
    let state = RewardState { bytes };
    state.check()?;
    Ok(state)
}

/// Next-state buffer of one transition: a copy of the committed bytes and the
/// working ledger, re-validated as a whole before it is returned.
struct Draft<'b> {
    ledger: RewardLedger,
    bytes: &'b mut [u8],
}
impl<'b> Draft<'b> {
    fn start(current: &RewardState<'_>, next: &'b mut [u8]) -> CodecResult<Self> {
        if next.len() != REWARD_STATE_BYTES {
            return Err(NON_CANONICAL);
        }
        next.copy_from_slice(current.bytes);
        Ok(Self {
            ledger: current.ledger()?,
            bytes: next,
        })
    }
    fn dictionary(&self) -> RecipientDictionary<'_> {
        RecipientDictionary {
            bytes: &self.bytes[DICTIONARY_AT..ROWS_AT],
        }
    }
    fn rows(&self) -> EpochRows<'_> {
        EpochRows {
            bytes: &self.bytes[ROWS_AT..LAST_REFUND_AT],
        }
    }
    fn set_slot(&mut self, index: u16, slot: Option<&RecipientSlot>) -> CodecResult<()> {
        RecipientDictionary::write_slot(&mut self.bytes[DICTIONARY_AT..ROWS_AT], index, slot)
    }
    fn set_row(&mut self, index: usize, row: Option<&RewardEpoch>) -> CodecResult<()> {
        EpochRows::write(&mut self.bytes[ROWS_AT..LAST_REFUND_AT], index, row)
    }
    fn commit(self, effect: RewardEffect) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        let Self { ledger, bytes } = self;
        encode_ledger(&ledger, &mut bytes[..DICTIONARY_AT])?;
        Ok((decode_reward_state(bytes)?, effect))
    }
    fn release(&mut self, index: u16) -> CodecResult<()> {
        let mut slot = self.dictionary().slot(index)?;
        slot.references = slot
            .references
            .checked_sub(1)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        if slot.references == 0 {
            self.set_slot(index, None)?;
            self.ledger.recipient_count = self
                .ledger
                .recipient_count
                .checked_sub(1)
                .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
            Ok(())
        } else {
            self.set_slot(index, Some(&slot))
        }
    }
    fn admit(&mut self, worker: WorkerId, recipient: AccountId) -> CodecResult<u16> {
        if recipient == self.ledger.account {
            return Err(ACCOUNT_BINDING);
        }
        if let Some(index) = self.dictionary().find(worker, recipient)? {
            let mut slot = self.dictionary().slot(index)?;
            if slot.references >= MAX_SLOT_REFERENCES {
                return Err(CAPACITY);
            }
            slot.references += 1;
            self.set_slot(index, Some(&slot))?;
            return Ok(index);
        }
        let index = self.dictionary().first_free().ok_or(CAPACITY)?;
        self.set_slot(
            index,
            Some(&RecipientSlot {
                worker,
                recipient,
                references: 1,
            }),
        )?;
        self.ledger.recipient_count = self
            .ledger
            .recipient_count
            .checked_add(1)
            .ok_or(ARITHMETIC)?;
        Ok(index)
    }
    fn remove_oldest(&mut self) -> CodecResult<Presence<Digest32>> {
        let oldest = self.rows().get(0)?.ok_or(NOT_FOUND)?;
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
        self.bytes[ROWS_AT..LAST_REFUND_AT].copy_within(EPOCH_MAX_BYTES.., 0);
        self.set_row(MAX_EPOCH_ROWS - 1, None)?;
        self.ledger.retained_epochs = self
            .ledger
            .retained_epochs
            .checked_sub(1)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        Ok(oldest.allocation)
    }
}

impl<'a> RewardState<'a> {
    /// Writes the initial state in place: the given ledger, an empty
    /// dictionary, no epoch rows and an absent refund record.
    ///
    /// # Errors
    /// `NON_CANONICAL` when `out` is not exactly 43261 bytes, or any state invariant error.
    pub fn init<'b>(ledger: &RewardLedger, out: &'b mut [u8]) -> CodecResult<RewardState<'b>> {
        if out.len() != REWARD_STATE_BYTES {
            return Err(NON_CANONICAL);
        }
        encode_ledger(ledger, &mut out[..DICTIONARY_AT])?;
        RecipientDictionary::init_empty(&mut out[DICTIONARY_AT..ROWS_AT])?;
        EpochRows::init_empty(&mut out[ROWS_AT..LAST_REFUND_AT])?;
        encode_last_refund(None, &mut out[LAST_REFUND_AT..])?;
        decode_reward_state(out)
    }
    #[must_use]
    pub fn bytes(&self) -> &'a [u8] {
        self.bytes
    }
    /// # Errors
    /// Any ledger decoding error.
    pub fn ledger(&self) -> CodecResult<RewardLedger> {
        decode_ledger(&self.bytes[..DICTIONARY_AT])
    }
    #[must_use]
    pub fn dictionary(&self) -> RecipientDictionary<'a> {
        RecipientDictionary {
            bytes: &self.bytes[DICTIONARY_AT..ROWS_AT],
        }
    }
    #[must_use]
    pub fn rows(&self) -> EpochRows<'a> {
        EpochRows {
            bytes: &self.bytes[ROWS_AT..LAST_REFUND_AT],
        }
    }
    /// # Errors
    /// Any refund record decoding error.
    pub fn last_refund(&self) -> CodecResult<Option<LastRefund>> {
        decode_last_refund(&self.bytes[LAST_REFUND_AT..])
    }
    fn locate(&self, epoch: u64) -> CodecResult<(usize, RewardEpoch)> {
        for (index, row) in self.rows().records().enumerate() {
            let row = row?;
            if row.epoch == epoch {
                return Ok((index, row));
            }
        }
        Err(NOT_FOUND)
    }
    /// # Errors
    /// `NOT_FOUND` for an unknown or pruned epoch.
    pub fn row(&self, epoch: u64) -> CodecResult<RewardEpoch> {
        Ok(self.locate(epoch)?.1)
    }
    /// Full invariant: compact ascending rows, ledger counters equal to the
    /// rows, every entry naming an occupied slot, and each slot reference
    /// count equal to the retained entries that name it.
    fn check(&self) -> CodecResult<()> {
        let ledger = self.ledger()?;
        let rows = self.rows();
        if !rows
            .bytes
            .chunks_exact(EPOCH_MAX_BYTES)
            .skip(rows.used())
            .all(unused)
        {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        ledger.check_row_iter(rows.records())?;
        let dictionary = self.dictionary();
        dictionary.validate(ledger.account)?;
        if dictionary.occupied() != usize::from(ledger.recipient_count) {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        let mut references = [0u16; MAX_PAYOUT_IDENTITIES];
        for row in rows.records() {
            let row = row?;
            row.check_dictionary(&dictionary)?;
            for entry in row.entries() {
                let count = &mut references[usize::from(entry.slot)];
                *count = count.checked_add(1).ok_or(ARITHMETIC)?;
            }
        }
        for (index, count) in (0u16..).zip(references) {
            if dictionary.get(index)?.map_or(0, |s| s.references) != count {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
        }
        if let Some(last) = self.last_refund()? {
            let end = last
                .prior_refunded
                .checked_add(last.amount)
                .ok_or(ARITHMETIC)?;
            if end > ledger.tracked_refunds {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            }
        }
        Ok(())
    }
    /// Accepted repeat: the next state is a byte copy of this one.
    fn keep<'b>(
        &self,
        next: &'b mut [u8],
        effect: RewardEffect,
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        if next.len() != REWARD_STATE_BYTES {
            return Err(NON_CANONICAL);
        }
        next.copy_from_slice(self.bytes);
        Ok((RewardState { bytes: next }, effect))
    }
    fn completed(&self) -> CodecResult<usize> {
        let mut completed = 0;
        for row in self.rows().records() {
            if row?.status != EpochStatus::Reserved {
                completed += 1;
            }
        }
        Ok(completed)
    }

    /// Fund (0x0601): authenticated owner/treasury, policy 1, explicit
    /// consent and the immutable refund recipient. The replay record and
    /// revision move only together with D and F; an accepted repeat stages
    /// no deposit.
    ///
    /// # Errors
    /// `UNAUTHORIZED`, `F06_FUNDING_POLICY_MISMATCH`,
    /// `F06_CONTRIBUTION_CONSENT_REQUIRED`, `F06_REFUND_RECIPIENT_MISMATCH`,
    /// `WRONG_PHASE`, `F06_INVALID_AMOUNT`, `ARITHMETIC`, `NON_CANONICAL` for a
    /// wrong-sized `next`, and the common replay refusals; on any error
    /// neither this state nor the replay table changes.
    pub fn fund<'b>(
        &self,
        authority: &FundingAuthority,
        phase: FundingPhase,
        payload: &FundRequest,
        replay: &mut FundReplay<'_>,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        authority.check(replay.request)?;
        let ledger = self.ledger()?;
        if payload.policy_version != FUNDING_POLICY_VERSION {
            return Err(F06_FUNDING_POLICY_MISMATCH);
        }
        if !payload.consent {
            return Err(F06_CONTRIBUTION_CONSENT_REQUIRED);
        }
        if payload.refund_recipient != ledger.refund_recipient {
            return Err(F06_REFUND_RECIPIENT_MISMATCH);
        }
        if let ReplayDecision::AlreadyApplied(last) =
            replay.table.check(replay.request, replay.height)?
        {
            return self.keep(next, RewardEffect::ReplayedFund(last.result_digest));
        }
        if phase != FundingPhase::Accepting {
            return Err(WRONG_PHASE);
        }
        let mut draft = Draft::start(self, next)?;
        draft.ledger = ledger.deposit(payload.amount)?;
        let mut table = replay.table.clone();
        let mut revision = *replay.revision;
        table.record_success(replay.request, replay.height, &mut revision, replay.result)?;
        let committed = draft.commit(RewardEffect::Deposit {
            principal: replay.request.principal,
            amount: payload.amount,
        })?;
        *replay.table = table;
        *replay.revision = revision;
        Ok(committed)
    }

    /// `ReserveEpoch`: internal to `OpenEpoch`. Prunes the oldest completed row
    /// only when the ring is full and that row holds no unpaid liability,
    /// preflights every frozen recipient into the dictionary (`WorkerId`s
    /// ascending), then moves budget from F to R in one row.
    ///
    /// # Errors
    /// `F06_EPOCH_ALREADY_RESERVED`, `WRONG_ROSTER`, `WRONG_PHASE` for a non-increasing epoch, `ARITHMETIC` when the eventual expiry height overflows, `INSUFFICIENT_FREE`, `F06_INVALID_AMOUNT`, `RETENTION_FULL` when the oldest completed row still holds liability, `ACCOUNT_BINDING`, `CAPACITY` when the dictionary is full, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn reserve_epoch<'b>(
        &self,
        epoch: u64,
        budget: Amount,
        roster_digest: RosterDigest,
        roster: &[WorkerRosterEntry],
        opening_height: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        let ledger = self.ledger()?;
        if ledger.active_reserve {
            return Err(F06_EPOCH_ALREADY_RESERVED);
        }
        if roster.is_empty() || roster.len() > MAX_WORKERS {
            return Err(WRONG_ROSTER);
        }
        if roster.windows(2).any(|p| p[0].worker >= p[1].worker) {
            return Err(WRONG_ROSTER);
        }
        if self
            .rows()
            .records()
            .last()
            .transpose()?
            .is_some_and(|r| r.epoch >= epoch)
        {
            return Err(WRONG_PHASE);
        }
        claim_expiry(
            opening_height
                .checked_add(EPOCH_SPAN_HEIGHTS)
                .ok_or(ARITHMETIC)?,
        )?;
        if budget > ledger.free {
            return Err(INSUFFICIENT_FREE);
        }
        let mut effect = RewardEffect::NoTransfer;
        let pruning = self.completed()? >= MAX_RETAINED_EPOCHS;
        let mut draft = Draft::start(self, next)?;
        if pruning {
            effect = RewardEffect::Pruned(draft.remove_oldest().map_err(|e| {
                if e == WRONG_PHASE {
                    RETENTION_FULL
                } else {
                    e
                }
            })?);
        }
        let mut slots = [0u16; MAX_WORKERS];
        for (slot, entry) in slots.iter_mut().zip(roster) {
            *slot = draft.admit(entry.worker, entry.recipient)?;
        }
        draft.ledger = draft.ledger.reserve(budget)?;
        let index = draft.rows().used();
        let row = RewardEpoch::reserved(epoch, budget, roster_digest, &slots[..roster.len()])?;
        draft.set_row(index, Some(&row))?;
        draft.commit(effect)
    }

    /// `TerminalizeRewards`: internal to `FinalizeAggregation`, bound to the
    /// immutable F05 terminal result and the frozen roster. ALLOCATED moves B
    /// from R to C; `NO_ELIGIBLE_SCORE` releases B to F and the slot references.
    ///
    /// # Errors
    /// `NOT_FOUND`, `F06_EPOCH_TERMINAL`, `WRONG_ROSTER`, `F06_AGGREGATION_MISMATCH`, `ARITHMETIC` on height overflow with R preserved, `RETENTION_FULL`, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn terminalize<'b>(
        &self,
        binding: &FrozenBinding,
        aggregation: Digest32,
        allocation: &Allocation,
        roster: &[WorkerRosterEntry],
        height: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        let (index, row) = self.locate(binding.epoch)?;
        if row.status != EpochStatus::Reserved {
            return Err(F06_EPOCH_TERMINAL);
        }
        if binding.roster != row.roster || roster.len() != row.entries().len() {
            return Err(WRONG_ROSTER);
        }
        let dictionary = self.dictionary();
        for (entry, member) in row.entries().iter().zip(roster) {
            let slot = dictionary.slot(entry.slot)?;
            if slot.worker != member.worker || slot.recipient != member.recipient {
                return Err(WRONG_ROSTER);
            }
        }
        if allocation.budget() != row.budget {
            return Err(F06_AGGREGATION_MISMATCH);
        }
        let ledger = self.ledger()?;
        let outcome = allocation.outcome();
        let digest = allocation_digest(binding, aggregation, ledger.asset, allocation, roster)?;
        let (terminal_ledger, expiry) = ledger.terminalize(row.budget, outcome, height)?;
        let mut draft = Draft::start(self, next)?;
        draft.ledger = terminal_ledger;
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
                for (i, (entry, member)) in terminal.entries.iter_mut().zip(roster).enumerate() {
                    let (worker, amount) = allocation.entitlement(i)?;
                    if worker != member.worker {
                        return Err(F06_AGGREGATION_MISMATCH);
                    }
                    entry.entitlement = amount;
                }
            }
            RewardOutcome::NoEligibleScore => {
                for entry in row.entries() {
                    draft.release(entry.slot)?;
                }
                terminal.entry_count = 0;
                terminal.entries = [RewardEntry::EMPTY; MAX_WORKERS];
            }
            RewardOutcome::Undecided => return Err(F06_AGGREGATION_MISMATCH),
        }
        draft.set_row(index, Some(&terminal))?;
        draft.commit(RewardEffect::NoTransfer)
    }

    /// `DeclinePendingEpoch`: only an epoch that never opened may be declined;
    /// it spends nothing and an opened epoch can never be cancelled.
    ///
    /// # Errors
    /// `WRONG_PHASE` once the epoch has opened, `NON_CANONICAL` for a wrong-sized `next`, or any row decoding error.
    pub fn decline_pending_epoch<'b>(
        &self,
        epoch: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        for row in self.rows().records() {
            if row?.epoch >= epoch {
                return Err(WRONG_PHASE);
            }
        }
        self.keep(next, RewardEffect::NoTransfer)
    }

    /// Claim (0x0602): exact fixed recipient and amount; pays at most once.
    ///
    /// # Errors
    /// `NOT_FOUND` for an unknown or pruned epoch, the claim refusals of `RewardEpoch::check_claim`, `ARITHMETIC`, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn claim<'b>(
        &self,
        epoch: u64,
        request: &ClaimRequest,
        height: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        let (index, row) = self.locate(epoch)?;
        match row.check_claim(
            &self.dictionary(),
            request.worker,
            request.recipient,
            request.amount,
            height,
        )? {
            ClaimDecision::AlreadyApplied(id) => self.keep(next, RewardEffect::AlreadyApplied(id)),
            ClaimDecision::Payable {
                index: entry,
                amount,
            } => {
                let mut draft = Draft::start(self, next)?;
                let mut paid = row;
                paid.entries[entry].disposition = Disposition::Claimed;
                paid.paid_sum = row.paid_sum.checked_add(amount).ok_or(ARITHMETIC)?;
                draft.set_row(index, Some(&paid))?;
                draft.ledger.liability = draft
                    .ledger
                    .liability
                    .checked_sub(amount)
                    .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
                draft.ledger.total_claimed = draft
                    .ledger
                    .total_claimed
                    .checked_add(amount)
                    .ok_or(ARITHMETIC)?;
                draft.commit(RewardEffect::Payout {
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
    /// `NOT_FOUND`, `WRONG_PHASE` for a row that is not allocated or not yet at expiry, `ARITHMETIC`, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn expire_epoch_claims<'b>(
        &self,
        epoch: u64,
        height: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        let (index, row) = self.locate(epoch)?;
        if row.outcome != RewardOutcome::Allocated {
            return Err(WRONG_PHASE);
        }
        if row.status == EpochStatus::Expired {
            let Presence::Present(digest) = row.allocation else {
                return Err(F06_LEDGER_INVARIANT_VIOLATION);
            };
            return self.keep(next, RewardEffect::AlreadyApplied(digest));
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
        let mut draft = Draft::start(self, next)?;
        draft.set_row(index, Some(&expired))?;
        draft.ledger.liability = draft
            .ledger
            .liability
            .checked_sub(released)
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)?;
        draft.ledger.free = draft.ledger.free.checked_add(released).ok_or(ARITHMETIC)?;
        draft.commit(RewardEffect::Released(released))
    }

    /// `RefundFree` (0x0604): permissionless while closing, monotonic X cursor,
    /// fixed recipient, never touching R or C.
    ///
    /// # Errors
    /// `WRONG_PHASE` outside closing, `F06_REFUND_RECIPIENT_MISMATCH`, `STALE_CURSOR`, `F06_INVALID_AMOUNT`, `INSUFFICIENT_FREE`, `ARITHMETIC`, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn refund_free<'b>(
        &self,
        phase: FundingPhase,
        request: &RefundRequest,
        request_digest: RequestDigest,
        result: ResultDigest,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        if phase != FundingPhase::Closing {
            return Err(WRONG_PHASE);
        }
        let ledger = self.ledger()?;
        if request.recipient != ledger.refund_recipient {
            return Err(F06_REFUND_RECIPIENT_MISMATCH);
        }
        if let Some(last) = self.last_refund()? {
            if last.prior_refunded == request.expected_refunded && last.amount == request.amount {
                return self.keep(next, RewardEffect::RepeatedRefund(last.result));
            }
        }
        if request.expected_refunded != ledger.tracked_refunds {
            return Err(STALE_CURSOR);
        }
        if request.amount == 0 {
            return Err(F06_INVALID_AMOUNT);
        }
        let mut draft = Draft::start(self, next)?;
        draft.ledger.free = ledger
            .free
            .checked_sub(request.amount)
            .ok_or(INSUFFICIENT_FREE)?;
        draft.ledger.tracked_refunds = ledger
            .tracked_refunds
            .checked_add(request.amount)
            .ok_or(ARITHMETIC)?;
        let record = LastRefund {
            prior_refunded: request.expected_refunded,
            amount: request.amount,
            request: request_digest,
            result,
        };
        encode_last_refund(Some(&record), &mut draft.bytes[LAST_REFUND_AT..])?;
        draft.commit(RewardEffect::Payout {
            recipient: ledger.refund_recipient,
            amount: request.amount,
        })
    }

    /// `PruneEpoch` (0x0605): only the oldest completed row without unpaid
    /// liability; releases its dictionary references, no counter change.
    ///
    /// # Errors
    /// `NOT_FOUND`, `WRONG_PHASE` for a younger row, a reserved row or a row with unpaid liability, `NON_CANONICAL` for a wrong-sized `next`, or any state invariant error.
    pub fn prune_epoch<'b>(
        &self,
        epoch: u64,
        next: &'b mut [u8],
    ) -> CodecResult<(RewardState<'b>, RewardEffect)> {
        if self.locate(epoch)?.0 != 0 {
            return Err(WRONG_PHASE);
        }
        let mut draft = Draft::start(self, next)?;
        let digest = draft.remove_oldest()?;
        draft.commit(RewardEffect::Pruned(digest))
    }
}
