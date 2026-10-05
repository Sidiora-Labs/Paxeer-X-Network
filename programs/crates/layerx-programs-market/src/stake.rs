use layerx_program_sdk::{AccountId, Amount, AssetId, Field, ProgramError, Reason};

use crate::settle::{ClaimStatus, SettlementPlan, UsageChallenge, UsageClaim};
use crate::{ComputeLease, LeaseStatus, Offer, OfferStatus};

#[cfg(target_arch = "wasm32")]
use layerx_program_sdk::{transfer, ProgramAccountPayment, ProgramAccountSeed};

pub const STAKE_PREFIX: &[u8] = b"lx.market.stake/";
pub const STAKE_CAPACITY: usize = 177;
pub const MAX_TRANSFERS: usize = 3;
const VERSION: u8 = 1;

/// Provider stake held in the offer's program-owned stake account.
///
/// `posted` is the value the stake account holds, `locked` the part of it bound to leases whose
/// escrow is still at risk, and `slashed` the cumulative value moved out by arbiter verdicts, so
/// `posted + slashed` always equals the stake originally deposited.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Stake {
    pub offer_id: [u8; 32],
    pub provider: AccountId,
    pub account: AccountId,
    pub asset: AssetId,
    pub posted: Amount,
    pub locked: Amount,
    pub slashed: Amount,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Transfer<'a> {
    pub source: AccountId,
    pub seed: &'a [u8],
    pub destination: AccountId,
    pub amount: Amount,
}

/// Ordinary program-account transfers applied together by the kernel within one call.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TransferSet<'a> {
    pub asset: AssetId,
    transfers: [Option<Transfer<'a>>; MAX_TRANSFERS],
}

impl<'a> TransferSet<'a> {
    const fn new(asset: AssetId) -> Self {
        Self {
            asset,
            transfers: [None; MAX_TRANSFERS],
        }
    }

    fn push(
        &mut self,
        source: AccountId,
        seed: &'a [u8],
        destination: AccountId,
        amount: Amount,
    ) -> Result<(), ProgramError> {
        if amount.is_zero() {
            return Ok(());
        }
        let slot = self
            .transfers
            .iter_mut()
            .find(|slot| slot.is_none())
            .ok_or_else(malformed)?;
        *slot = Some(Transfer {
            source,
            seed,
            destination,
            amount,
        });
        Ok(())
    }

    pub fn iter(&self) -> impl Iterator<Item = &Transfer<'a>> {
        self.transfers.iter().flatten()
    }

    /// # Errors
    /// Returns an error if the debited total overflows.
    pub fn debited(&self, account: AccountId) -> Result<Amount, ProgramError> {
        self.iter()
            .filter(|transfer| transfer.source == account)
            .try_fold(Amount::ZERO, |total, transfer| {
                total.checked_add(transfer.amount)
            })
    }

    /// # Errors
    /// Returns an error if the credited total overflows.
    pub fn credited(&self, account: AccountId) -> Result<Amount, ProgramError> {
        self.iter()
            .filter(|transfer| transfer.destination == account)
            .try_fold(Amount::ZERO, |total, transfer| {
                total.checked_add(transfer.amount)
            })
    }

    /// # Errors
    /// Returns an error if the moved total overflows.
    pub fn total(&self) -> Result<Amount, ProgramError> {
        self.iter().try_fold(Amount::ZERO, |total, transfer| {
            total.checked_add(transfer.amount)
        })
    }
}

/// Stake a provider must hold locked for a lease: the full escrow the provider could wrongly
/// collect, so the bond is proportional to the value at risk.
#[must_use]
pub const fn value_at_risk(lease: &ComputeLease<'_>) -> Amount {
    lease.funded
}

impl Stake {
    /// # Errors
    /// Returns an error unless the offer is open with a nonzero stake deposit.
    pub fn post(offer: &Offer<'_>) -> Result<Self, ProgramError> {
        if offer.status != OfferStatus::Open || offer.stake.is_zero() {
            return Err(malformed());
        }
        Ok(Self {
            offer_id: offer.id,
            provider: offer.provider,
            account: offer.stake_account,
            asset: offer.asset,
            posted: offer.stake,
            locked: Amount::ZERO,
            slashed: Amount::ZERO,
        })
    }

    /// # Errors
    /// Returns an error if locked stake exceeds posted stake or the unlocked total overflows.
    pub fn unlocked(self) -> Result<Amount, ProgramError> {
        self.posted.checked_sub(self.locked)
    }

    fn bound(self, offer: &Offer<'_>) -> Result<(), ProgramError> {
        if self.offer_id != offer.id
            || self.provider != offer.provider
            || self.account != offer.stake_account
            || self.asset != offer.asset
        {
            return Err(malformed());
        }
        Ok(())
    }

    fn holds(self, lease: &ComputeLease<'_>) -> Result<(), ProgramError> {
        if self.offer_id != lease.offer_id
            || self.provider != lease.provider
            || self.asset != lease.asset
            || self.account == lease.escrow_account
        {
            return Err(malformed());
        }
        Ok(())
    }

    /// Locks stake proportional to the lease's value at risk when the lease opens.
    ///
    /// # Errors
    /// Returns an error for an unrelated or unfunded lease, a height outside the lease or
    /// insufficient unlocked stake.
    pub fn lock(
        mut self,
        offer: &Offer<'_>,
        lease: &ComputeLease<'_>,
        height: u64,
    ) -> Result<Self, ProgramError> {
        self.bound(offer)?;
        self.holds(lease)?;
        let locked = self.locked.checked_add(value_at_risk(lease))?;
        if offer.status != OfferStatus::Open
            || lease.status != LeaseStatus::Funded
            || height < lease.opened_at
            || height >= lease.expires_at
            || locked > self.posted
        {
            return Err(malformed());
        }
        self.locked = locked;
        Ok(self)
    }

    /// Unlocks a lease's stake once its challenge window has closed unrefuted, or once an
    /// unclaimed lease has expired and refunded its tenant.
    ///
    /// # Errors
    /// Returns an error for an unrelated lease or claim, a frozen or unsettled claim, an open
    /// challenge window or an unexpired lease.
    pub fn release(
        mut self,
        lease: &ComputeLease<'_>,
        claim: Option<&UsageClaim>,
        height: u64,
    ) -> Result<Self, ProgramError> {
        self.holds(lease)?;
        let closed = match claim {
            None => lease.status == LeaseStatus::ExpiredRefunded && height >= lease.expires_at,
            Some(claim) => {
                claim.lease_id == lease.id
                    && claim.provider == lease.provider
                    && lease.status == LeaseStatus::Settled
                    && claim.status == ClaimStatus::Finalized
                    && height > claim.challenge_deadline
            }
        };
        if !closed {
            return Err(malformed());
        }
        self.locked = self.locked.checked_sub(value_at_risk(lease))?;
        Ok(self)
    }

    /// Returns the remaining stake to the provider once no lease holds any of it.
    ///
    /// # Errors
    /// Returns an error unless the provider withdraws from its own closed offer with nothing locked.
    pub fn withdraw<'a>(
        mut self,
        offer: &Offer<'a>,
        principal: AccountId,
    ) -> Result<(Self, TransferSet<'a>), ProgramError> {
        self.bound(offer)?;
        if principal != self.provider
            || offer.status != OfferStatus::Closed
            || !self.locked.is_zero()
        {
            return Err(malformed());
        }
        let mut set = TransferSet::new(self.asset);
        set.push(self.account, offer.stake_seed, self.provider, self.posted)?;
        if set.debited(self.account)? != self.posted {
            return Err(malformed());
        }
        self.posted = Amount::ZERO;
        Ok((self, set))
    }
}

/// Settles a resolved dispute as one ordinary transfer set.
///
/// A provider proven wrong forfeits exactly the stake locked for the disputed lease to the
/// challenger, the tenant recovers the whole escrow and the challenger recovers its own stake.
/// A challenger proven wrong forfeits its stake to the provider's payout account, the escrow pays
/// the provider's claim and refunds the remainder, and the provider's lock is released. Every
/// debited unit is credited to exactly one account.
///
/// # Errors
/// Returns an error for unrelated or unresolved dispute state, a plan that does not match the
/// verdict, overlapping program accounts, or any non-conserving movement.
pub fn slash<'a>(
    mut stake: Stake,
    offer: &Offer<'a>,
    lease: &ComputeLease<'a>,
    claim: &UsageClaim,
    challenge: &UsageChallenge<'a>,
    plan: SettlementPlan,
) -> Result<(Stake, TransferSet<'a>), ProgramError> {
    stake.bound(offer)?;
    stake.holds(lease)?;
    if lease.offer_id != offer.id
        || lease.status != LeaseStatus::Settled
        || claim.lease_id != lease.id
        || claim.provider != lease.provider
        || challenge.claim_id != claim.id
        || challenge.lease_id != lease.id
        || challenge.offer_id != offer.id
        || challenge.provider != lease.provider
        || challenge.tenant != lease.tenant
        || challenge.stake != claim.challenger_stake
        || challenge.stake_account == lease.escrow_account
        || challenge.stake_account == stake.account
    {
        return Err(malformed());
    }
    plan.conserves(lease.funded, challenge.stake)?;
    let lock = value_at_risk(lease);
    stake.locked = stake.locked.checked_sub(lock)?;
    let mut set = TransferSet::new(lease.asset);
    let forfeited = match claim.status {
        ClaimStatus::ChallengerWon => {
            if plan
                != (SettlementPlan {
                    provider: Amount::ZERO,
                    tenant: lease.funded,
                    challenger: challenge.stake,
                    stake_for_provider: Amount::ZERO,
                })
            {
                return Err(malformed());
            }
            set.push(
                lease.escrow_account,
                lease.escrow_seed,
                lease.tenant_refund,
                plan.tenant,
            )?;
            set.push(
                challenge.stake_account,
                challenge.stake_seed,
                challenge.challenger,
                plan.challenger,
            )?;
            set.push(stake.account, offer.stake_seed, challenge.challenger, lock)?;
            lock
        }
        ClaimStatus::ProviderWon => {
            if plan
                != (SettlementPlan {
                    provider: claim.payable,
                    tenant: lease.funded.checked_sub(claim.payable)?,
                    challenger: Amount::ZERO,
                    stake_for_provider: challenge.stake,
                })
            {
                return Err(malformed());
            }
            set.push(
                lease.escrow_account,
                lease.escrow_seed,
                lease.provider_payout,
                plan.provider,
            )?;
            set.push(
                lease.escrow_account,
                lease.escrow_seed,
                lease.tenant_refund,
                plan.tenant,
            )?;
            set.push(
                challenge.stake_account,
                challenge.stake_seed,
                lease.provider_payout,
                plan.stake_for_provider,
            )?;
            Amount::ZERO
        }
        _ => return Err(malformed()),
    };
    stake.posted = stake.posted.checked_sub(forfeited)?;
    stake.slashed = stake.slashed.checked_add(forfeited)?;
    if set.debited(lease.escrow_account)? != lease.funded
        || set.debited(challenge.stake_account)? != challenge.stake
        || set.debited(stake.account)? != forfeited
        || set.total()?
            != lease
                .funded
                .checked_add(challenge.stake)?
                .checked_add(forfeited)?
        || stake.locked > stake.posted
    {
        return Err(malformed());
    }
    Ok((stake, set))
}

#[cfg(target_arch = "wasm32")]
/// # Errors
/// Returns an error for invalid payment fields or a refused host transfer, which rolls back the
/// whole call.
pub fn execute(set: &TransferSet<'_>) -> Result<(), ProgramError> {
    for movement in set.iter() {
        transfer::pay_from_program_account(ProgramAccountPayment::new(
            ProgramAccountSeed::new(movement.seed)?,
            movement.source,
            set.asset,
            movement.destination,
            movement.amount,
        )?)?;
    }
    Ok(())
}

/// # Errors
/// Returns an error for inconsistent stake state or a short output buffer.
pub fn encode_stake(stake: &Stake, output: &mut [u8]) -> Result<usize, ProgramError> {
    if stake.offer_id == [0; 32] || stake.locked > stake.posted || output.len() < STAKE_CAPACITY {
        return Err(malformed());
    }
    let mut offset = 0;
    crate::append(output, &mut offset, &[VERSION])?;
    for bytes in [
        stake.offer_id,
        stake.provider.bytes(),
        stake.account.bytes(),
        stake.asset.bytes(),
    ] {
        crate::append(output, &mut offset, &bytes)?;
    }
    for amount in [stake.posted, stake.locked, stake.slashed] {
        crate::append(output, &mut offset, &amount.to_be_bytes())?;
    }
    Ok(offset)
}

/// # Errors
/// Returns an error for a wrong version, truncated or trailing bytes, or inconsistent amounts.
pub fn decode_stake(input: &[u8]) -> Result<Stake, ProgramError> {
    let mut cursor = crate::Cursor::new(input);
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    let stake = Stake {
        offer_id: cursor.array()?,
        provider: cursor.account()?,
        account: cursor.account()?,
        asset: cursor.asset()?,
        posted: cursor.amount()?,
        locked: cursor.amount()?,
        slashed: cursor.amount()?,
    };
    cursor.finish()?;
    if stake.offer_id == [0; 32] || stake.locked > stake.posted {
        return Err(malformed());
    }
    Ok(stake)
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::CallInput, Reason::Malformed)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::settle::{
        challenge, commit_usage, finalize_unchallenged, resolve, ArbiterResolution, ArbiterVerdict,
        ChallengeRequest, ContradictingCommitment, MeteredUsageClaim, ProviderCommitment,
    };
    use crate::{close, expire, open, register, OpenLease, RegisterOffer, VerificationModel};
    use std::collections::BTreeMap;

    fn ok<T>(result: Result<T, ProgramError>) -> T {
        result.unwrap_or_else(|error| panic!("stake fixture: {error}"))
    }
    fn account(tag: u8, index: u64) -> AccountId {
        let mut bytes = [tag; 32];
        bytes[24..].copy_from_slice(&index.to_be_bytes());
        ok(AccountId::new(bytes))
    }
    fn amount(value: u64) -> Amount {
        Amount::from_integer(value)
    }
    fn asset() -> AssetId {
        ok(AssetId::new([9; 32]))
    }
    fn provider() -> AccountId {
        account(1, 0)
    }
    fn offer(stake: u64) -> Offer<'static> {
        ok(register(
            RegisterOffer {
                id: [1; 32],
                provider: provider(),
                payout: account(2, 0),
                asset: asset(),
                stake_account: account(3, 0),
                stake_seed: b"stake/offer-1",
                stake: amount(stake),
                unit_price: amount(4),
                capacity: 100,
                minimum_units: 2,
                maximum_units: 20,
                expires_at: 1_000_000,
                verification: VerificationModel::FraudProvable,
            },
            provider(),
            1,
        ))
    }
    fn lease(
        offer: Offer<'static>,
        index: u64,
        units: u64,
        height: u64,
    ) -> Result<(Offer<'static>, ComputeLease<'static>), ProgramError> {
        let mut id = [5; 32];
        id[24..].copy_from_slice(&index.to_be_bytes());
        let tenant = account(4, index);
        open(
            offer,
            OpenLease {
                id,
                offer_id: offer.id,
                tenant,
                refund: tenant,
                escrow_account: account(6, index),
                escrow_seed: b"escrow",
                units,
                funded: amount(units * 4),
                expires_at: height + 50,
            },
            tenant,
            height,
        )
    }
    fn claim(
        offer: Offer<'static>,
        lease: &ComputeLease<'static>,
        units: u64,
        challenger_stake: u64,
        height: u64,
    ) -> UsageClaim {
        let mut id = lease.id;
        id[0] = 7;
        ok(commit_usage(
            offer,
            lease,
            ProviderCommitment {
                id,
                lease_id: lease.id,
                input_commitment: [8; 32],
                output_digest: [9; 32],
                execution_state_root: [10; 32],
                usage: MeteredUsageClaim {
                    compute_units: units,
                    memory_byte_batches: 1,
                    storage_read_bytes: 2,
                    storage_written_bytes: 3,
                    ingress_bytes: 4,
                    egress_bytes: 5,
                },
                payable: amount(units * 4),
                challenger_stake: amount(challenger_stake),
                challenge_window_batches: 10,
            },
            provider(),
            height,
        ))
        .0
    }
    fn dispute(
        offer: Offer<'static>,
        lease: &ComputeLease<'static>,
        claim: UsageClaim,
        height: u64,
    ) -> (UsageClaim, UsageChallenge<'static>) {
        let mut index = [0; 8];
        index.copy_from_slice(&lease.id[24..]);
        let index = u64::from_be_bytes(index);
        let mut id = lease.id;
        id[0] = 12;
        ok(challenge(
            offer,
            lease,
            claim,
            &ChallengeRequest {
                challenge_id: id,
                challenger: account(13, index),
                stake_account: account(14, index),
                stake_seed: b"challenge",
                stake: claim.challenger_stake,
                contradictory: ContradictingCommitment {
                    input_commitment: claim.input_commitment,
                    output_digest: [11; 32],
                    execution_state_root: claim.execution_state_root,
                    usage: claim.usage,
                },
            },
            height,
        ))
    }
    fn verdict(
        offer: Offer<'static>,
        lease: ComputeLease<'static>,
        frozen: UsageClaim,
        dispute: &UsageChallenge<'static>,
        verdict: ArbiterVerdict,
    ) -> (
        Offer<'static>,
        ComputeLease<'static>,
        UsageClaim,
        SettlementPlan,
    ) {
        ok(resolve(
            offer,
            lease,
            frozen,
            dispute,
            ArbiterResolution {
                claim_id: frozen.id,
                challenge_id: dispute.id,
                dispute_commitment: [15; 32],
                verdict,
            },
        ))
    }

    struct Ledger(BTreeMap<AccountId, u128>);

    impl Ledger {
        fn balance(&self, account: AccountId) -> u128 {
            self.0.get(&account).copied().unwrap_or(0)
        }
        fn supply(&self) -> u128 {
            self.0.values().sum()
        }
        fn mint(&mut self, account: AccountId, value: Amount) {
            *self.0.entry(account).or_insert(0) += value.value();
        }
        fn pay(&mut self, source: AccountId, destination: AccountId, value: Amount) {
            let available = self.balance(source);
            assert!(available >= value.value(), "overdraft of {source:?}");
            self.0.insert(source, available - value.value());
            *self.0.entry(destination).or_insert(0) += value.value();
        }
        fn apply(&mut self, set: &TransferSet<'_>) {
            assert_eq!(set.asset, asset());
            for movement in set.iter() {
                assert!(!movement.amount.is_zero());
                self.pay(movement.source, movement.destination, movement.amount);
            }
        }
    }

    #[test]
    fn stake_is_proportional_to_value_at_risk_and_locked_in_the_program_account() {
        let offer = offer(100);
        let stake = ok(Stake::post(&offer));
        assert_eq!(stake.account, offer.stake_account);
        assert_eq!(stake.posted, offer.stake);
        assert_eq!(stake.locked, Amount::ZERO);
        let (offer, small) = ok(lease(offer, 1, 5, 2));
        let (offer, large) = ok(lease(offer, 2, 15, 2));
        assert_eq!(value_at_risk(&small), amount(20));
        assert_eq!(value_at_risk(&large), amount(60));
        let stake = ok(stake.lock(&offer, &small, 2));
        assert_eq!(stake.locked, amount(20));
        let stake = ok(stake.lock(&offer, &large, 2));
        assert_eq!(stake.locked, amount(80));
        assert_eq!(ok(stake.unlocked()), amount(20));
        let (offer, over) = ok(lease(offer, 3, 6, 2));
        assert!(stake.lock(&offer, &over, 2).is_err());
        let (offer, exact) = ok(lease(offer, 4, 5, 2));
        let full = ok(stake.lock(&offer, &exact, 2));
        assert_eq!(full.locked, full.posted);
        assert!(stake.lock(&offer, &exact, 1).is_err());
        assert!(stake.lock(&offer, &exact, exact.expires_at).is_err());
        let foreign = Offer {
            id: [2; 32],
            ..offer
        };
        assert!(stake.lock(&foreign, &exact, 2).is_err());
        let shared = ComputeLease {
            escrow_account: offer.stake_account,
            ..exact
        };
        assert!(stake.lock(&offer, &shared, 2).is_err());
        let closed = Offer {
            status: OfferStatus::Closed,
            ..offer
        };
        assert!(Stake::post(&closed).is_err());
        assert!(full.withdraw(&closed, provider()).is_err());
    }

    #[test]
    fn lock_holds_through_the_challenge_window_and_unlocks_at_close() {
        let offer = offer(200);
        let stake = ok(Stake::post(&offer));
        let (offer, open_lease) = ok(lease(offer, 1, 10, 2));
        let stake = ok(stake.lock(&offer, &open_lease, 2));
        let pending = claim(offer, &open_lease, 6, 25, 3);
        assert!(stake.release(&open_lease, Some(&pending), 3).is_err());
        assert!(stake
            .release(&open_lease, Some(&pending), pending.challenge_deadline + 1)
            .is_err());
        assert!(stake
            .release(&open_lease, None, open_lease.expires_at)
            .is_err());
        let (frozen, _) = dispute(offer, &open_lease, pending, pending.challenge_deadline);
        assert!(stake
            .release(&open_lease, Some(&frozen), open_lease.expires_at)
            .is_err());
        let (offer, settled, finalized, plan) = ok(finalize_unchallenged(
            offer,
            open_lease,
            pending,
            pending.challenge_deadline + 1,
        ));
        assert_eq!(plan.provider, amount(24));
        assert!(stake
            .release(&settled, Some(&finalized), finalized.challenge_deadline)
            .is_err());
        let released =
            ok(stake.release(&settled, Some(&finalized), finalized.challenge_deadline + 1));
        assert_eq!(released.locked, Amount::ZERO);
        assert_eq!(released.posted, amount(200));
        assert!(released
            .release(&settled, Some(&finalized), finalized.challenge_deadline + 1)
            .is_err());

        let (offer, unclaimed) = ok(lease(offer, 2, 4, 60));
        let relocked = ok(released.lock(&offer, &unclaimed, 60));
        assert!(relocked.withdraw(&offer, provider()).is_err());
        assert!(relocked
            .release(&unclaimed, None, unclaimed.expires_at)
            .is_err());
        assert!(expire(offer, unclaimed, unclaimed.expires_at - 1).is_err());
        let (offer, refunded) = ok(expire(offer, unclaimed, unclaimed.expires_at));
        assert!(relocked
            .release(&refunded, None, refunded.expires_at - 1)
            .is_err());
        let unlocked = ok(relocked.release(&refunded, None, refunded.expires_at));
        assert_eq!(unlocked.locked, Amount::ZERO);
        assert!(unlocked.withdraw(&offer, provider()).is_err());
        let closed = ok(close(offer, provider()));
        assert!(unlocked.withdraw(&closed, account(2, 0)).is_err());
        let (empty, set) = ok(unlocked.withdraw(&closed, provider()));
        assert_eq!(empty.posted, Amount::ZERO);
        assert_eq!(ok(set.debited(closed.stake_account)), amount(200));
        assert_eq!(ok(set.credited(provider())), amount(200));
        assert_eq!(set.iter().count(), 1);
    }

    #[test]
    fn provider_proven_wrong_is_partially_slashed_to_the_challenger() {
        let offer = offer(300);
        let stake = ok(Stake::post(&offer));
        let (offer, disputed) = ok(lease(offer, 1, 10, 2));
        let stake = ok(stake.lock(&offer, &disputed, 2));
        let pending = claim(offer, &disputed, 6, 25, 3);
        let (frozen, challenge) = dispute(offer, &disputed, pending, 5);
        let (offer, settled, resolved, plan) = verdict(
            offer,
            disputed,
            frozen,
            &challenge,
            ArbiterVerdict::Challenger,
        );
        let (slashed, set) = ok(slash(stake, &offer, &settled, &resolved, &challenge, plan));
        assert_eq!(slashed.locked, Amount::ZERO);
        assert_eq!(slashed.posted, amount(260));
        assert_eq!(slashed.slashed, amount(40));
        assert_eq!(ok(slashed.posted.checked_add(slashed.slashed)), offer.stake);
        assert_eq!(ok(set.debited(offer.stake_account)), amount(40));
        assert_eq!(ok(set.debited(settled.escrow_account)), settled.funded);
        assert_eq!(ok(set.debited(challenge.stake_account)), challenge.stake);
        assert_eq!(ok(set.credited(settled.tenant_refund)), settled.funded);
        assert_eq!(ok(set.credited(challenge.challenger)), amount(65));
        assert_eq!(ok(set.credited(settled.provider_payout)), Amount::ZERO);
        assert_eq!(ok(set.total()), amount(105));
        assert!(stake
            .release(&settled, Some(&resolved), resolved.challenge_deadline + 1)
            .is_err());
        let skewed = SettlementPlan {
            tenant: ok(plan.tenant.checked_sub(amount(1))),
            provider: amount(1),
            ..plan
        };
        assert!(slash(stake, &offer, &settled, &resolved, &challenge, skewed).is_err());
        let provider_plan = SettlementPlan {
            provider: resolved.payable,
            tenant: ok(settled.funded.checked_sub(resolved.payable)),
            challenger: Amount::ZERO,
            stake_for_provider: challenge.stake,
        };
        assert!(slash(
            stake,
            &offer,
            &settled,
            &resolved,
            &challenge,
            provider_plan
        )
        .is_err());
        assert!(slash(stake, &offer, &settled, &frozen, &challenge, plan).is_err());
        assert!(slash(stake, &offer, &disputed, &resolved, &challenge, plan).is_err());
        let unlocked = Stake {
            locked: Amount::ZERO,
            ..stake
        };
        assert!(slash(unlocked, &offer, &settled, &resolved, &challenge, plan).is_err());
        let overlapping = UsageChallenge {
            stake_account: offer.stake_account,
            ..challenge
        };
        assert!(slash(stake, &offer, &settled, &resolved, &overlapping, plan).is_err());
        let restaked = UsageChallenge {
            stake: amount(24),
            ..challenge
        };
        assert!(slash(stake, &offer, &settled, &resolved, &restaked, plan).is_err());
    }

    #[test]
    fn challenger_proven_wrong_compensates_the_provider() {
        let offer = offer(300);
        let stake = ok(Stake::post(&offer));
        let (offer, disputed) = ok(lease(offer, 1, 10, 2));
        let stake = ok(stake.lock(&offer, &disputed, 2));
        let pending = claim(offer, &disputed, 6, 25, 3);
        let (frozen, challenge) = dispute(offer, &disputed, pending, 5);
        let (offer, settled, resolved, plan) = verdict(
            offer,
            disputed,
            frozen,
            &challenge,
            ArbiterVerdict::Provider,
        );
        let (kept, set) = ok(slash(stake, &offer, &settled, &resolved, &challenge, plan));
        assert_eq!(kept.locked, Amount::ZERO);
        assert_eq!(kept.posted, offer.stake);
        assert_eq!(kept.slashed, Amount::ZERO);
        assert_eq!(ok(set.debited(offer.stake_account)), Amount::ZERO);
        assert_eq!(ok(set.debited(challenge.stake_account)), amount(25));
        assert_eq!(ok(set.credited(challenge.challenger)), Amount::ZERO);
        assert_eq!(ok(set.credited(settled.provider_payout)), amount(49));
        assert_eq!(ok(set.credited(settled.tenant_refund)), amount(16));
        assert_eq!(ok(set.total()), amount(65));
        let mut ledger = Ledger(BTreeMap::new());
        ledger.mint(settled.escrow_account, settled.funded);
        ledger.mint(challenge.stake_account, challenge.stake);
        ledger.mint(offer.stake_account, offer.stake);
        let supply = ledger.supply();
        ledger.apply(&set);
        assert_eq!(ledger.supply(), supply);
        assert_eq!(ledger.balance(challenge.challenger), 0);
        assert_eq!(ledger.balance(challenge.stake_account), 0);
        assert_eq!(ledger.balance(settled.escrow_account), 0);
        assert_eq!(ledger.balance(offer.stake_account), kept.posted.value());
        let wrong_plan = SettlementPlan {
            provider: Amount::ZERO,
            tenant: settled.funded,
            challenger: challenge.stake,
            stake_for_provider: Amount::ZERO,
        };
        assert!(slash(stake, &offer, &settled, &resolved, &challenge, wrong_plan).is_err());
    }

    #[test]
    fn slashing_one_of_several_concurrent_leases_keeps_the_others_locked() {
        let offer = offer(200);
        let mut stake = ok(Stake::post(&offer));
        let mut current = offer;
        let mut leases = Vec::new();
        for index in 1..=4 {
            let (next, lease) = ok(lease(current, index, 10, 2));
            stake = ok(stake.lock(&next, &lease, 2));
            current = next;
            leases.push(lease);
        }
        assert_eq!(stake.locked, amount(160));
        let (next, fifth) = ok(lease(current, 5, 11, 2));
        assert!(stake.lock(&next, &fifth, 2).is_err());
        let claims: Vec<_> = leases
            .iter()
            .map(|lease| claim(current, lease, 6, 25, 3))
            .collect();
        let (frozen, challenge) = dispute(current, &leases[1], claims[1], 5);
        let (offer, settled, resolved, plan) = verdict(
            current,
            leases[1],
            frozen,
            &challenge,
            ArbiterVerdict::Challenger,
        );
        let (mut stake, set) = ok(slash(stake, &offer, &settled, &resolved, &challenge, plan));
        assert_eq!(stake.locked, amount(120));
        assert_eq!(stake.posted, amount(160));
        assert_eq!(stake.slashed, amount(40));
        assert_eq!(ok(set.debited(offer.stake_account)), amount(40));
        assert_eq!(ok(stake.unlocked()), amount(40));
        let mut current = offer;
        for (index, (lease, claim)) in leases.iter().zip(&claims).enumerate() {
            if index == 1 {
                continue;
            }
            assert!(stake
                .release(lease, Some(claim), claim.challenge_deadline + 1)
                .is_err());
            let (next, settled, finalized, _) = ok(finalize_unchallenged(
                current,
                *lease,
                *claim,
                claim.challenge_deadline + 1,
            ));
            stake = ok(stake.release(&settled, Some(&finalized), claim.challenge_deadline + 1));
            current = next;
        }
        assert_eq!(stake.locked, Amount::ZERO);
        assert_eq!(stake.posted, amount(160));
        let closed = ok(close(current, provider()));
        let (empty, set) = ok(stake.withdraw(&closed, provider()));
        assert_eq!(ok(set.credited(provider())), amount(160));
        assert_eq!(ok(empty.posted.checked_add(empty.slashed)), amount(40));
    }

    #[test]
    fn stake_state_is_strictly_decoded() {
        let offer = offer(200);
        let (offer, lease) = ok(lease(offer, 1, 10, 2));
        let stake = ok(ok(Stake::post(&offer)).lock(&offer, &lease, 2));
        let mut bytes = [0; STAKE_CAPACITY];
        assert_eq!(encode_stake(&stake, &mut bytes), Ok(STAKE_CAPACITY));
        assert_eq!(decode_stake(&bytes), Ok(stake));
        for length in 0..STAKE_CAPACITY {
            assert!(decode_stake(&bytes[..length]).is_err());
        }
        let mut trailing = bytes.to_vec();
        trailing.push(0);
        assert!(decode_stake(&trailing).is_err());
        let mut version = bytes;
        version[0] = 2;
        assert!(decode_stake(&version).is_err());
        let overlocked = Stake {
            locked: amount(201),
            ..stake
        };
        assert!(encode_stake(&overlocked, &mut [0; STAKE_CAPACITY]).is_err());
        let mut forged = bytes;
        forged[STAKE_CAPACITY - 32..STAKE_CAPACITY - 16]
            .copy_from_slice(&amount(201).to_be_bytes());
        assert!(decode_stake(&forged).is_err());
        let mut short = [0xa5; STAKE_CAPACITY - 1];
        assert!(encode_stake(&stake, &mut short).is_err());
        assert_eq!(short, [0xa5; STAKE_CAPACITY - 1]);
    }

    struct Random(u64);

    impl Random {
        fn below(&mut self, bound: u64) -> u64 {
            self.0 = self
                .0
                .wrapping_mul(6_364_136_223_846_793_005)
                .wrapping_add(1_442_695_040_888_963_407);
            (self.0 >> 33) % bound
        }
    }

    #[test]
    fn dispute_heavy_histories_conserve_value() {
        let mut disputes = 0;
        let mut slashes = 0;
        for seed in 0..64 {
            let mut random = Random(seed);
            let mut ledger = Ledger(BTreeMap::new());
            let mut offer = offer(150 + random.below(400));
            ledger.mint(provider(), offer.stake);
            let mut minted = offer.stake.value();
            let mut stake = ok(Stake::post(&offer));
            ledger.pay(provider(), offer.stake_account, offer.stake);
            let mut index = 0;
            for round in 0..12u64 {
                let base = 100 + round * 100;
                let mut active = Vec::new();
                for _ in 0..=random.below(5) {
                    index += 1;
                    let units = 2 + random.below(19);
                    let Ok((next, lease)) = lease(offer, index, units, base) else {
                        continue;
                    };
                    ledger.mint(lease.tenant, lease.funded);
                    minted += lease.funded.value();
                    let Ok(locked) = stake.lock(&next, &lease, base) else {
                        assert!(ok(stake.locked.checked_add(value_at_risk(&lease))) > stake.posted);
                        continue;
                    };
                    ledger.pay(lease.tenant, lease.escrow_account, lease.funded);
                    stake = locked;
                    offer = next;
                    active.push(lease);
                }
                assert_eq!(ledger.supply(), minted);
                for lease in active {
                    let outcome = random.below(5);
                    if outcome == 0 {
                        let (next, refunded) = ok(expire(offer, lease, lease.expires_at));
                        ledger.pay(lease.escrow_account, lease.tenant_refund, lease.funded);
                        stake = ok(stake.release(&refunded, None, lease.expires_at));
                        offer = next;
                    } else {
                        let used = 1 + random.below(lease.units);
                        let committed = claim(offer, &lease, used, 1 + random.below(30), base + 1);
                        if outcome == 1 {
                            let at = committed.challenge_deadline + 1;
                            let (next, settled, finalized, plan) =
                                ok(finalize_unchallenged(offer, lease, committed, at));
                            ledger.pay(lease.escrow_account, lease.provider_payout, plan.provider);
                            ledger.pay(lease.escrow_account, lease.tenant_refund, plan.tenant);
                            stake = ok(stake.release(&settled, Some(&finalized), at));
                            offer = next;
                        } else {
                            disputes += 1;
                            let (frozen, challenge) = dispute(offer, &lease, committed, base + 2);
                            ledger.mint(challenge.challenger, challenge.stake);
                            ledger.pay(
                                challenge.challenger,
                                challenge.stake_account,
                                challenge.stake,
                            );
                            minted += challenge.stake.value();
                            let side = if outcome == 2 {
                                ArbiterVerdict::Challenger
                            } else {
                                ArbiterVerdict::Provider
                            };
                            let before = stake;
                            let (next, settled, resolved, plan) =
                                verdict(offer, lease, frozen, &challenge, side);
                            let (after, set) =
                                ok(slash(stake, &next, &settled, &resolved, &challenge, plan));
                            let moved = ledger.supply();
                            ledger.apply(&set);
                            assert_eq!(ledger.supply(), moved);
                            assert_eq!(ledger.balance(lease.escrow_account), 0);
                            assert_eq!(ledger.balance(challenge.stake_account), 0);
                            let forfeited = ok(before.posted.checked_sub(after.posted));
                            if side == ArbiterVerdict::Challenger {
                                slashes += 1;
                                assert_eq!(forfeited, value_at_risk(&lease));
                                assert_eq!(
                                    ledger.balance(challenge.challenger),
                                    ok(challenge.stake.checked_add(forfeited)).value()
                                );
                            } else {
                                assert_eq!(forfeited, Amount::ZERO);
                                assert_eq!(ledger.balance(challenge.challenger), 0);
                            }
                            stake = after;
                            offer = next;
                        }
                        assert_eq!(ledger.balance(lease.escrow_account), 0);
                    }
                    assert!(stake.locked <= stake.posted);
                    assert_eq!(ledger.balance(offer.stake_account), stake.posted.value());
                    assert_eq!(ok(stake.posted.checked_add(stake.slashed)), offer.stake);
                    assert_eq!(ledger.supply(), minted);
                }
                assert_eq!(stake.locked, Amount::ZERO);
            }
            let closed = ok(close(offer, provider()));
            let (empty, set) = ok(stake.withdraw(&closed, provider()));
            ledger.apply(&set);
            assert_eq!(ledger.supply(), minted);
            assert_eq!(ledger.balance(closed.stake_account), 0);
            assert_eq!(empty.posted, Amount::ZERO);
            assert_eq!(
                ledger.balance(provider()) + empty.slashed.value(),
                offer.stake.value()
            );
        }
        assert!(disputes > 100);
        assert!(slashes > 50);
    }
}
