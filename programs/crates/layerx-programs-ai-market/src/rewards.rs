//! F06 reward ledger, recipient dictionary and epoch records with strict
//! big-endian encodings. These records move no funds; native transfer, grant
//! and finality authority stay with the real Program activity.
use crate::{
    codec::{domain_hash, Reader, Writer},
    errors::*,
    reward_math::{claim_expiry, conservation_holds, Allocation},
    types::*,
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

/// RewardLedgerV1: immutable bindings plus the D,P,X,F,R,C counters.
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
    /// returns its expiry; NO_ELIGIBLE_SCORE releases B from R to F (expiry 0).
    /// The expiry is computed first, so a height overflow leaves R untouched.
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
    pub fn check_rows(&self, epochs: &[RewardEpoch]) -> CodecResult<()> {
        self.validate()?;
        if epochs.len() > MAX_RETAINED_EPOCHS + 1 {
            return Err(CAPACITY);
        }
        if epochs.windows(2).any(|p| p[0].epoch >= p[1].epoch) {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        let mut active = 0usize;
        for epoch in epochs {
            epoch.validate()?;
            if epoch.status == EpochStatus::Reserved {
                active += 1;
            }
        }
        let reserved = checked_sum(epochs.iter().map(RewardEpoch::reserved_budget))?;
        let mut liability = 0u128;
        for epoch in epochs {
            liability = liability
                .checked_add(epoch.unclaimed_sum()?)
                .ok_or(ARITHMETIC)?;
        }
        if active > 1
            || (active == 1) != self.active_reserve
            || epochs.len() - active != usize::from(self.retained_epochs)
            || reserved != self.reserved
            || liability != self.liability
        {
            return Err(F06_LEDGER_INVARIANT_VIOLATION);
        }
        Ok(())
    }
}
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
    pub const fn new() -> Self {
        Self {
            slots: [None; MAX_PAYOUT_IDENTITIES],
        }
    }
    pub fn slot(&self, index: u16) -> CodecResult<RecipientSlot> {
        self.slots
            .get(usize::from(index))
            .ok_or(NON_CANONICAL)?
            .ok_or(F06_LEDGER_INVARIANT_VIOLATION)
    }
    pub fn occupied(&self) -> usize {
        self.slots.iter().filter(|s| s.is_some()).count()
    }
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
pub fn decode_dictionary(bytes: &[u8], reserve: AccountId) -> CodecResult<RecipientDictionary> {
    if bytes.len() != DICTIONARY_BYTES {
        return Err(NON_CANONICAL);
    }
    let mut r = Reader::new(bytes);
    let mut v = RecipientDictionary::new();
    for slot in v.slots.iter_mut() {
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

/// RewardEpochV1: 174-byte header followed by entry_count 19-byte entries.
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
    pub fn entries(&self) -> &[RewardEntry] {
        &self.entries[..usize::from(self.entry_count).min(MAX_WORKERS)]
    }
    pub fn reserved_budget(&self) -> Amount {
        if self.status == EpochStatus::Reserved {
            self.budget
        } else {
            0
        }
    }
    /// Live liability: unclaimed entitlements of a TERMINAL ALLOCATED row.
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
    /// Entries must name occupied slots ordered strictly by worker_id bytes.
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
    for entry in v.entries[..count].iter_mut() {
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

/// H(PAXAI/reward-allocation/v1, ...): each hashed entry is worker_id32 ||
/// frozen roster recipient32 || entitlement:u128. Dictionary indexes and claim
/// dispositions never enter the digest.
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
pub fn entitlement_id(allocation: Digest32, worker: WorkerId) -> CodecResult<Digest32> {
    let mut bytes = [0u8; 64];
    bytes[..32].copy_from_slice(allocation.as_bytes());
    bytes[32..].copy_from_slice(worker.as_bytes());
    domain_hash("PAXAI/reward-entitlement/v1", &bytes)
}
