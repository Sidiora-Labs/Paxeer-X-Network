#![cfg_attr(target_arch = "wasm32", no_std)]

pub mod settle;
pub use settle::{decode_claim as decode_usage_claim, ChallengeWindow, UsageClaim};

use layerx_program_sdk::{AccountId, Amount, AssetId, Field, ProgramError, Reason};

pub mod arbitration;
pub mod attest;
pub mod dispute;
pub mod stake;
pub use stake::{slash, Stake};

#[cfg(target_arch = "wasm32")]
use layerx_program_sdk::{
    context::Context, event, storage::shared, transfer, CallResult, EventData, EventTopic,
    ProgramAccountPayment, ProgramAccountSeed, ProgramDeposit, StorageValue,
};

#[cfg(target_arch = "wasm32")]
layerx_program_sdk::trap_on_panic!();

const VERSION: u8 = 1;
#[cfg(target_arch = "wasm32")]
const REGISTER_OFFER: u8 = 1;
#[cfg(target_arch = "wasm32")]
const OPEN_LEASE: u8 = 2;
#[cfg(target_arch = "wasm32")]
const SETTLE_LEASE: u8 = 3;
#[cfg(target_arch = "wasm32")]
const EXPIRE_LEASE: u8 = 4;
#[cfg(target_arch = "wasm32")]
const CLOSE_OFFER: u8 = 5;
#[cfg(target_arch = "wasm32")]
const CONFIGURE_ATTESTERS: u8 = 6;
#[cfg(target_arch = "wasm32")]
const COMMIT_EXTERNAL_INPUT: u8 = 7;
#[cfg(target_arch = "wasm32")]
const SEAL_EXTERNAL_INPUTS: u8 = 8;
#[cfg(target_arch = "wasm32")]
const SUBMIT_ATTESTATION: u8 = 9;
#[cfg(target_arch = "wasm32")]
const COMMIT_USAGE: u8 = 10;
#[cfg(target_arch = "wasm32")]
const CHALLENGE_USAGE: u8 = 11;
#[cfg(target_arch = "wasm32")]
const FINALIZE_USAGE: u8 = 12;
pub const OFFER_PREFIX: &[u8] = b"lx.market.offer/";
pub const LEASE_PREFIX: &[u8] = b"lx.market.lease/";
pub const CLAIM_PREFIX: &[u8] = b"lx.market.claim/";
pub const SETTLEMENT_PREFIX: &[u8] = b"lx.market.settlement/";
pub const SETTLEMENT_CAPACITY: usize = 282;
#[cfg(target_arch = "wasm32")]
const CHALLENGE_PREFIX: &[u8] = b"lx.market.challenge/";
#[cfg(target_arch = "wasm32")]
const CLAIM_ID_PREFIX: &[u8] = b"lx.market.claim-id/";
#[cfg(target_arch = "wasm32")]
const CHALLENGE_ID_PREFIX: &[u8] = b"lx.market.challenge-id/";
#[cfg(target_arch = "wasm32")]
const TOPIC_OFFER: &[u8] = b"lx.market.offer";
#[cfg(target_arch = "wasm32")]
const TOPIC_LEASE: &[u8] = b"lx.market.lease";
#[cfg(target_arch = "wasm32")]
const TOPIC_CLAIM: &[u8] = b"lx.market.claim";
#[cfg(target_arch = "wasm32")]
const TOPIC_CHALLENGE: &[u8] = b"lx.market.challenge";
#[cfg(target_arch = "wasm32")]
const TOPIC_SETTLEMENT: &[u8] = b"lx.market.settlement";
const ID_BYTES: usize = 32;
const MAX_SEED_BYTES: usize = layerx_program_sdk::MAX_PROGRAM_ACCOUNT_SEED_BYTES;
#[cfg(target_arch = "wasm32")]
const OFFER_CAPACITY: usize = 263 + MAX_SEED_BYTES;
#[cfg(target_arch = "wasm32")]
const LEASE_CAPACITY: usize = 308 + MAX_SEED_BYTES;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum VerificationModel {
    Bonded = 1,
    Attested = 2,
    FraudProvable = 3,
}

impl VerificationModel {
    fn decode(value: u8) -> Result<Self, ProgramError> {
        match value {
            1 => Ok(Self::Bonded),
            2 => Ok(Self::Attested),
            3 => Ok(Self::FraudProvable),
            _ => Err(malformed()),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum OfferStatus {
    Open = 1,
    Closed = 2,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum LeaseStatus {
    Funded = 1,
    Settled = 2,
    ExpiredRefunded = 3,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Offer<'a> {
    pub id: [u8; ID_BYTES],
    pub provider: AccountId,
    pub payout: AccountId,
    pub asset: AssetId,
    pub stake_account: AccountId,
    pub stake_seed: &'a [u8],
    pub stake: Amount,
    pub unit_price: Amount,
    pub total_capacity: u64,
    pub available_capacity: u64,
    pub minimum_units: u64,
    pub maximum_units: u64,
    pub expires_at: u64,
    pub verification: VerificationModel,
    pub status: OfferStatus,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ComputeLease<'a> {
    pub id: [u8; ID_BYTES],
    pub offer_id: [u8; ID_BYTES],
    pub provider: AccountId,
    pub tenant: AccountId,
    pub provider_payout: AccountId,
    pub tenant_refund: AccountId,
    pub asset: AssetId,
    pub escrow_account: AccountId,
    pub escrow_seed: &'a [u8],
    pub units: u64,
    pub funded: Amount,
    pub opened_at: u64,
    pub expires_at: u64,
    pub verification: VerificationModel,
    pub status: LeaseStatus,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SettlementRecord {
    pub lease_id: [u8; ID_BYTES],
    pub offer_id: [u8; ID_BYTES],
    pub claim_id: [u8; ID_BYTES],
    pub escrow_account: AccountId,
    pub asset: AssetId,
    pub provider_payout: AccountId,
    pub tenant_refund: AccountId,
    pub funded: Amount,
    pub provider_paid: Amount,
    pub tenant_paid: Amount,
    pub settled_at: u64,
    pub status: LeaseStatus,
}

impl SettlementRecord {
    pub fn new(
        lease: &ComputeLease<'_>,
        claim_id: [u8; ID_BYTES],
        provider_paid: Amount,
        tenant_paid: Amount,
        settled_at: u64,
    ) -> Result<Self, ProgramError> {
        let record = Self {
            lease_id: lease.id,
            offer_id: lease.offer_id,
            claim_id,
            escrow_account: lease.escrow_account,
            asset: lease.asset,
            provider_payout: lease.provider_payout,
            tenant_refund: lease.tenant_refund,
            funded: lease.funded,
            provider_paid,
            tenant_paid,
            settled_at,
            status: lease.status,
        };
        record.validate()?;
        if settled_at < lease.opened_at
            || (lease.status == LeaseStatus::ExpiredRefunded && settled_at < lease.expires_at)
        {
            return Err(malformed());
        }
        Ok(record)
    }

    fn validate(&self) -> Result<(), ProgramError> {
        if self.lease_id == [0; ID_BYTES]
            || self.offer_id == [0; ID_BYTES]
            || self.funded.is_zero()
            || self.provider_paid.checked_add(self.tenant_paid)? != self.funded
            || match self.status {
                LeaseStatus::Funded => true,
                LeaseStatus::Settled => self.claim_id == [0; ID_BYTES],
                LeaseStatus::ExpiredRefunded => {
                    self.claim_id != [0; ID_BYTES] || !self.provider_paid.is_zero()
                }
            }
        {
            return Err(malformed());
        }
        Ok(())
    }
}

pub fn encode_settlement(
    record: &SettlementRecord,
    output: &mut [u8],
) -> Result<usize, ProgramError> {
    record.validate()?;
    if output.len() < SETTLEMENT_CAPACITY {
        return Err(malformed());
    }
    let mut offset = 0;
    append(output, &mut offset, &[VERSION, record.status as u8])?;
    for bytes in [
        record.lease_id,
        record.offer_id,
        record.claim_id,
        record.escrow_account.bytes(),
        record.asset.bytes(),
        record.provider_payout.bytes(),
        record.tenant_refund.bytes(),
    ] {
        append(output, &mut offset, &bytes)?;
    }
    for amount in [record.funded, record.provider_paid, record.tenant_paid] {
        append(output, &mut offset, &amount.to_be_bytes())?;
    }
    append(output, &mut offset, &record.settled_at.to_be_bytes())?;
    Ok(offset)
}

pub fn decode_settlement(bytes: &[u8]) -> Result<SettlementRecord, ProgramError> {
    let mut cursor = Cursor::new(bytes);
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    let status = match cursor.byte()? {
        2 => LeaseStatus::Settled,
        3 => LeaseStatus::ExpiredRefunded,
        _ => return Err(malformed()),
    };
    let record = SettlementRecord {
        lease_id: cursor.array()?,
        offer_id: cursor.array()?,
        claim_id: cursor.array()?,
        escrow_account: cursor.account()?,
        asset: cursor.asset()?,
        provider_payout: cursor.account()?,
        tenant_refund: cursor.account()?,
        funded: cursor.amount()?,
        provider_paid: cursor.amount()?,
        tenant_paid: cursor.amount()?,
        settled_at: cursor.u64()?,
        status,
    };
    cursor.finish()?;
    record.validate()?;
    Ok(record)
}

#[derive(Clone, Copy)]
pub struct RegisterOffer<'a> {
    pub id: [u8; ID_BYTES],
    pub provider: AccountId,
    pub payout: AccountId,
    pub asset: AssetId,
    pub stake_account: AccountId,
    pub stake_seed: &'a [u8],
    pub stake: Amount,
    pub unit_price: Amount,
    pub capacity: u64,
    pub minimum_units: u64,
    pub maximum_units: u64,
    pub expires_at: u64,
    pub verification: VerificationModel,
}

#[derive(Clone, Copy)]
pub struct OpenLease<'a> {
    pub id: [u8; ID_BYTES],
    pub offer_id: [u8; ID_BYTES],
    pub tenant: AccountId,
    pub refund: AccountId,
    pub escrow_account: AccountId,
    pub escrow_seed: &'a [u8],
    pub units: u64,
    pub funded: Amount,
    pub expires_at: u64,
}

/// # Errors
/// Returns an error for invalid offer identity, principal, capacity, expiry, stake, price or seed.
pub fn register(
    request: RegisterOffer<'_>,
    principal: AccountId,
    height: u64,
) -> Result<Offer<'_>, ProgramError> {
    if request.id == [0; ID_BYTES]
        || request.provider != principal
        || request.capacity == 0
        || request.minimum_units == 0
        || request.maximum_units < request.minimum_units
        || request.maximum_units > request.capacity
        || request.expires_at <= height
        || request.stake.is_zero()
        || request.unit_price.is_zero()
        || request.stake_seed.is_empty()
        || request.stake_seed.len() > MAX_SEED_BYTES
    {
        return Err(malformed());
    }
    Ok(Offer {
        id: request.id,
        provider: request.provider,
        payout: request.payout,
        asset: request.asset,
        stake_account: request.stake_account,
        stake_seed: request.stake_seed,
        stake: request.stake,
        unit_price: request.unit_price,
        total_capacity: request.capacity,
        available_capacity: request.capacity,
        minimum_units: request.minimum_units,
        maximum_units: request.maximum_units,
        expires_at: request.expires_at,
        verification: request.verification,
        status: OfferStatus::Open,
    })
}

/// # Errors
/// Returns an error for invalid lease terms, funding, principal, offer state or arithmetic overflow.
pub fn open<'a>(
    offer: Offer<'a>,
    request: OpenLease<'a>,
    principal: AccountId,
    height: u64,
) -> Result<(Offer<'a>, ComputeLease<'a>), ProgramError> {
    let expected = offer
        .unit_price
        .checked_mul(Amount::from_integer(request.units))?;
    if request.id == [0; ID_BYTES]
        || request.offer_id != offer.id
        || request.tenant != principal
        || offer.status != OfferStatus::Open
        || height >= offer.expires_at
        || request.expires_at <= height
        || request.expires_at > offer.expires_at
        || request.units < offer.minimum_units
        || request.units > offer.maximum_units
        || request.units > offer.available_capacity
        || request.funded != expected
        || request.escrow_seed.is_empty()
        || request.escrow_seed.len() > MAX_SEED_BYTES
    {
        return Err(malformed());
    }
    let mut updated = offer;
    updated.available_capacity = updated
        .available_capacity
        .checked_sub(request.units)
        .ok_or_else(malformed)?;
    let lease = ComputeLease {
        id: request.id,
        offer_id: offer.id,
        provider: offer.provider,
        tenant: request.tenant,
        provider_payout: offer.payout,
        tenant_refund: request.refund,
        asset: offer.asset,
        escrow_account: request.escrow_account,
        escrow_seed: request.escrow_seed,
        units: request.units,
        funded: request.funded,
        opened_at: height,
        expires_at: request.expires_at,
        verification: offer.verification,
        status: LeaseStatus::Funded,
    };
    Ok((updated, lease))
}

/// # Errors
/// Returns an error for an unrelated, unfunded or unexpired lease, or invalid released capacity.
pub fn expire<'a>(
    offer: Offer<'a>,
    mut lease: ComputeLease<'a>,
    height: u64,
) -> Result<(Offer<'a>, ComputeLease<'a>), ProgramError> {
    if lease.offer_id != offer.id
        || lease.status != LeaseStatus::Funded
        || height < lease.expires_at
    {
        return Err(malformed());
    }
    let mut updated = offer;
    updated.available_capacity = updated
        .available_capacity
        .checked_add(lease.units)
        .filter(|capacity| *capacity <= updated.total_capacity)
        .ok_or_else(malformed)?;
    lease.status = LeaseStatus::ExpiredRefunded;
    Ok((updated, lease))
}

/// # Errors
/// Returns an error unless the provider closes an open offer with all capacity available.
pub fn close(mut offer: Offer<'_>, principal: AccountId) -> Result<Offer<'_>, ProgramError> {
    if offer.provider != principal
        || offer.status != OfferStatus::Open
        || offer.available_capacity != offer.total_capacity
    {
        return Err(malformed());
    }
    offer.status = OfferStatus::Closed;
    Ok(offer)
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::CallInput, Reason::Malformed)
}

#[cfg(any(test, target_arch = "wasm32"))]
fn decode_operation(cursor: &mut Cursor<'_>) -> Result<u8, ProgramError> {
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    match cursor.byte()? {
        operation @ (1 | 2 | 4..=17) => Ok(operation),
        _ => Err(malformed()),
    }
}

struct Cursor<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Cursor<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }
    fn take(&mut self, length: usize) -> Result<&'a [u8], ProgramError> {
        let end = self.offset.checked_add(length).ok_or_else(malformed)?;
        let value = self.bytes.get(self.offset..end).ok_or_else(malformed)?;
        self.offset = end;
        Ok(value)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ProgramError> {
        self.take(N)?.try_into().map_err(|_| malformed())
    }
    fn byte(&mut self) -> Result<u8, ProgramError> {
        Ok(self.take(1)?[0])
    }
    fn u32(&mut self) -> Result<u32, ProgramError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, ProgramError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn amount(&mut self) -> Result<Amount, ProgramError> {
        Ok(Amount::from_be_bytes(self.array()?))
    }
    fn account(&mut self) -> Result<AccountId, ProgramError> {
        AccountId::new(self.array()?)
    }
    fn asset(&mut self) -> Result<AssetId, ProgramError> {
        AssetId::new(self.array()?)
    }
    fn seed(&mut self) -> Result<&'a [u8], ProgramError> {
        let length = usize::from(u16::from_be_bytes(self.array()?));
        if length == 0 {
            return Err(malformed());
        }
        self.take(length)
    }
    fn remainder(self) -> &'a [u8] {
        &self.bytes[self.offset..]
    }
    fn finish(self) -> Result<(), ProgramError> {
        if self.offset == self.bytes.len() {
            Ok(())
        } else {
            Err(malformed())
        }
    }
}

fn append(output: &mut [u8], offset: &mut usize, value: &[u8]) -> Result<(), ProgramError> {
    let end = offset.checked_add(value.len()).ok_or_else(malformed)?;
    output
        .get_mut(*offset..end)
        .ok_or_else(malformed)?
        .copy_from_slice(value);
    *offset = end;
    Ok(())
}

fn append_seed(output: &mut [u8], offset: &mut usize, seed: &[u8]) -> Result<(), ProgramError> {
    let length = u16::try_from(seed.len()).map_err(|_| malformed())?;
    append(output, offset, &length.to_be_bytes())?;
    append(output, offset, seed)
}

pub fn encode_offer(offer: Offer<'_>, output: &mut [u8]) -> Result<usize, ProgramError> {
    let mut offset = 0;
    append(
        output,
        &mut offset,
        &[VERSION, offer.status as u8, offer.verification as u8],
    )?;
    append(output, &mut offset, &offer.id)?;
    append(output, &mut offset, &offer.provider.bytes())?;
    append(output, &mut offset, &offer.payout.bytes())?;
    append(output, &mut offset, &offer.asset.bytes())?;
    append(output, &mut offset, &offer.stake_account.bytes())?;
    append_seed(output, &mut offset, offer.stake_seed)?;
    append(output, &mut offset, &offer.stake.to_be_bytes())?;
    append(output, &mut offset, &offer.unit_price.to_be_bytes())?;
    for value in [
        offer.total_capacity,
        offer.available_capacity,
        offer.minimum_units,
        offer.maximum_units,
        offer.expires_at,
    ] {
        append(output, &mut offset, &value.to_be_bytes())?;
    }
    Ok(offset)
}

pub fn decode_offer(input: &[u8]) -> Result<Offer<'_>, ProgramError> {
    let mut cursor = Cursor::new(input);
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    let status = match cursor.byte()? {
        1 => OfferStatus::Open,
        2 => OfferStatus::Closed,
        _ => return Err(malformed()),
    };
    let verification = VerificationModel::decode(cursor.byte()?)?;
    let offer = Offer {
        id: cursor.array()?,
        provider: cursor.account()?,
        payout: cursor.account()?,
        asset: cursor.asset()?,
        stake_account: cursor.account()?,
        stake_seed: cursor.seed()?,
        stake: cursor.amount()?,
        unit_price: cursor.amount()?,
        total_capacity: cursor.u64()?,
        available_capacity: cursor.u64()?,
        minimum_units: cursor.u64()?,
        maximum_units: cursor.u64()?,
        expires_at: cursor.u64()?,
        verification,
        status,
    };
    cursor.finish()?;
    Ok(offer)
}

pub fn encode_lease(lease: &ComputeLease<'_>, output: &mut [u8]) -> Result<usize, ProgramError> {
    let mut offset = 0;
    append(
        output,
        &mut offset,
        &[VERSION, lease.status as u8, lease.verification as u8],
    )?;
    append(output, &mut offset, &lease.id)?;
    append(output, &mut offset, &lease.offer_id)?;
    for account in [
        lease.provider,
        lease.tenant,
        lease.provider_payout,
        lease.tenant_refund,
    ] {
        append(output, &mut offset, &account.bytes())?;
    }
    append(output, &mut offset, &lease.asset.bytes())?;
    append(output, &mut offset, &lease.escrow_account.bytes())?;
    append_seed(output, &mut offset, lease.escrow_seed)?;
    append(output, &mut offset, &lease.units.to_be_bytes())?;
    append(output, &mut offset, &lease.funded.to_be_bytes())?;
    append(output, &mut offset, &lease.opened_at.to_be_bytes())?;
    append(output, &mut offset, &lease.expires_at.to_be_bytes())?;
    Ok(offset)
}

pub fn decode_lease(input: &[u8]) -> Result<ComputeLease<'_>, ProgramError> {
    let mut cursor = Cursor::new(input);
    if cursor.byte()? != VERSION {
        return Err(malformed());
    }
    let status = match cursor.byte()? {
        1 => LeaseStatus::Funded,
        2 => LeaseStatus::Settled,
        3 => LeaseStatus::ExpiredRefunded,
        _ => return Err(malformed()),
    };
    let verification = VerificationModel::decode(cursor.byte()?)?;
    let lease = ComputeLease {
        id: cursor.array()?,
        offer_id: cursor.array()?,
        provider: cursor.account()?,
        tenant: cursor.account()?,
        provider_payout: cursor.account()?,
        tenant_refund: cursor.account()?,
        asset: cursor.asset()?,
        escrow_account: cursor.account()?,
        escrow_seed: cursor.seed()?,
        units: cursor.u64()?,
        funded: cursor.amount()?,
        opened_at: cursor.u64()?,
        expires_at: cursor.u64()?,
        verification,
        status,
    };
    cursor.finish()?;
    Ok(lease)
}

#[cfg(target_arch = "wasm32")]
fn state_key(prefix: &[u8], id: [u8; ID_BYTES]) -> Result<([u8; 64], usize), ProgramError> {
    let mut key = [0; 64];
    let end = prefix.len().checked_add(ID_BYTES).ok_or_else(malformed)?;
    key[..prefix.len()].copy_from_slice(prefix);
    key[prefix.len()..end].copy_from_slice(&id);
    Ok((key, end))
}

#[cfg(target_arch = "wasm32")]
fn read_state<'a>(
    prefix: &[u8],
    id: [u8; 32],
    output: &'a mut [u8],
) -> Result<&'a [u8], ProgramError> {
    let (key, length) = state_key(prefix, id)?;
    let written = shared::read(shared::SharedStorageKey::new(&key[..length])?, output)?
        .ok_or_else(|| ProgramError::value(Field::StorageValue, Reason::Malformed))?;
    output.get(..written).ok_or_else(malformed)
}

#[cfg(target_arch = "wasm32")]
fn read_optional_state<'a>(
    prefix: &[u8],
    id: [u8; 32],
    output: &'a mut [u8],
) -> Result<Option<&'a [u8]>, ProgramError> {
    let (key, length) = state_key(prefix, id)?;
    match shared::read(shared::SharedStorageKey::new(&key[..length])?, output)? {
        Some(written) => Ok(Some(output.get(..written).ok_or_else(malformed)?)),
        None => Ok(None),
    }
}

#[cfg(target_arch = "wasm32")]
fn write_state(prefix: &[u8], id: [u8; 32], value: &[u8]) -> Result<(), ProgramError> {
    let (key, length) = state_key(prefix, id)?;
    shared::write(
        shared::SharedStorageKey::new(&key[..length])?,
        StorageValue::new(value)?,
    )
}

#[cfg(target_arch = "wasm32")]
fn absent(prefix: &[u8], id: [u8; 32], scratch: &mut [u8]) -> Result<(), ProgramError> {
    let (key, length) = state_key(prefix, id)?;
    if shared::read(shared::SharedStorageKey::new(&key[..length])?, scratch)?.is_some() {
        return Err(ProgramError::value(Field::StorageValue, Reason::Duplicate));
    }
    Ok(())
}

#[cfg(target_arch = "wasm32")]
fn principal() -> Result<AccountId, ProgramError> {
    AccountId::new(Context::invoking_principal()?.bytes())
}

#[cfg(target_arch = "wasm32")]
fn emit(topic: &[u8], bytes: &[u8]) -> Result<(), ProgramError> {
    event::emit(EventTopic::new(topic)?, EventData::new(bytes)?)
}

#[cfg(target_arch = "wasm32")]
fn publish_settlement(record: SettlementRecord) -> Result<(), ProgramError> {
    let mut bytes = [0; SETTLEMENT_CAPACITY];
    absent(SETTLEMENT_PREFIX, record.lease_id, &mut bytes)?;
    let written = encode_settlement(&record, &mut bytes)?;
    write_state(SETTLEMENT_PREFIX, record.lease_id, &bytes[..written])?;
    emit(TOPIC_SETTLEMENT, &bytes[..written])
}

#[cfg(target_arch = "wasm32")]
fn sandbox_commitments(
    lease_id: [u8; ID_BYTES],
) -> Result<
    (
        layerx_program_sdk::arbiter::MarketSandboxProfile,
        layerx_program_sdk::arbiter::MarketBillingCommitment,
    ),
    ProgramError,
> {
    use layerx_program_sdk::arbiter::{
        MarketBillingCommitment, MarketSandboxProfile, MARKET_SANDBOX_BILLING_CAPACITY,
        MARKET_SANDBOX_PROFILE_CAPACITY,
    };
    let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
    let profile = MarketSandboxProfile::decode(read_state(
        arbitration::PROFILE_PREFIX,
        lease_id,
        &mut profile_bytes,
    )?)?;
    let mut billing_bytes = [0; MARKET_SANDBOX_BILLING_CAPACITY];
    let billing = MarketBillingCommitment::decode(read_state(
        arbitration::BILLING_PREFIX,
        lease_id,
        &mut billing_bytes,
    )?)?;
    Ok((profile, billing))
}

/// Forwards one step of the committed trace to the host arbiter with the market's own stored
/// profile and billing rows, so the verdict is authenticated by the host and never chosen by the
/// caller.
#[cfg(target_arch = "wasm32")]
fn adjudicate(
    lease_id: [u8; ID_BYTES],
    mode: u8,
    position: u32,
    evidence: &[u8],
) -> Result<layerx_program_sdk::arbiter::MarketStepOutcome, ProgramError> {
    use layerx_program_sdk::arbiter::{
        MarketStepRequest, MARKET_SANDBOX_BILLING_CAPACITY, MARKET_SANDBOX_PROFILE_CAPACITY,
    };
    let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
    let profile = read_state(arbitration::PROFILE_PREFIX, lease_id, &mut profile_bytes)?;
    let mut billing_bytes = [0; MARKET_SANDBOX_BILLING_CAPACITY];
    let billing = read_state(arbitration::BILLING_PREFIX, lease_id, &mut billing_bytes)?;
    layerx_program_sdk::arbiter::adjudicate_market_step(
        &MarketStepRequest {
            mode,
            position,
            profile,
            billing,
        },
        evidence,
    )
}

#[cfg(target_arch = "wasm32")]
fn absorb(
    mut current: dispute::Dispute,
    record: &dispute::Move,
) -> Result<dispute::Dispute, ProgramError> {
    current.transcript = layerx_program_sdk::crypto::hash(
        layerx_program_sdk::crypto::HashAlgorithm::Sha256,
        layerx_program_sdk::crypto::HashInput::new(&current.transcript_preimage(record))?,
    )?;
    Ok(current)
}

/// Persists one dispute move and, once the bisection has settled, settles the lease, the claim,
/// the challenge stake and the provider stake atomically in the same call.
#[cfg(target_arch = "wasm32")]
fn advance(
    lease: &ComputeLease<'_>,
    previous: dispute::Dispute,
    record: &dispute::Move,
    height: u64,
) -> Result<CallResult, ProgramError> {
    let current = absorb(previous, record)?;
    let mut dispute_bytes = [0; dispute::DISPUTE_CAPACITY];
    if let Some(verdict) = current.verdict() {
        let mut offer_bytes = [0; OFFER_CAPACITY];
        let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
        let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
        let claim = settle::decode_claim(read_state(CLAIM_PREFIX, lease.id, &mut claim_bytes)?)?;
        let mut challenge_bytes = [0; settle::CHALLENGE_CAPACITY];
        let challenge = settle::decode_challenge(read_state(
            CHALLENGE_PREFIX,
            lease.id,
            &mut challenge_bytes,
        )?)?;
        let mut stake_bytes = [0; stake::STAKE_CAPACITY];
        let posted =
            stake::decode_stake(read_state(stake::STAKE_PREFIX, offer.id, &mut stake_bytes)?)?;
        let (offer, settled, claim, plan) = settle::resolve(
            offer,
            *lease,
            claim,
            &challenge,
            settle::ArbiterResolution {
                claim_id: claim.id,
                challenge_id: challenge.id,
                dispute_commitment: current.transcript,
                verdict,
            },
        )?;
        let (stake, movements) = stake::slash(posted, &offer, &settled, &claim, &challenge, plan)?;
        stake::execute(&movements)?;
        publish_settlement(SettlementRecord::new(
            &settled,
            claim.id,
            plan.provider,
            plan.tenant,
            height,
        )?)?;
        let mut offer_output = [0; OFFER_CAPACITY];
        let mut lease_output = [0; LEASE_CAPACITY];
        let offer_written = encode_offer(offer, &mut offer_output)?;
        let lease_written = encode_lease(&settled, &mut lease_output)?;
        let claim_written = settle::encode_claim(&claim, &mut claim_bytes)?;
        let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
        write_state(OFFER_PREFIX, offer.id, &offer_output[..offer_written])?;
        write_state(LEASE_PREFIX, settled.id, &lease_output[..lease_written])?;
        write_state(CLAIM_PREFIX, settled.id, &claim_bytes[..claim_written])?;
        write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
        emit(TOPIC_OFFER, &offer_output[..offer_written])?;
        emit(TOPIC_LEASE, &lease_output[..lease_written])?;
        emit(TOPIC_CLAIM, &claim_bytes[..claim_written])?;
    }
    let written = dispute::encode_dispute(&current, &mut dispute_bytes)?;
    write_state(dispute::DISPUTE_PREFIX, lease.id, &dispute_bytes[..written])?;
    emit(dispute::TOPIC_DISPUTE, &dispute_bytes[..written])?;
    Ok(CallResult::OK)
}

#[cfg(target_arch = "wasm32")]
fn invoke(input: &[u8]) -> Result<CallResult, ProgramError> {
    let mut cursor = Cursor::new(input);
    let operation = decode_operation(&mut cursor)?;
    let height = Context::batch_height()?;
    let caller = principal()?;
    match operation {
        REGISTER_OFFER => {
            let request = RegisterOffer {
                id: cursor.array()?,
                provider: cursor.account()?,
                payout: cursor.account()?,
                asset: cursor.asset()?,
                stake_account: cursor.account()?,
                stake_seed: cursor.seed()?,
                stake: cursor.amount()?,
                unit_price: cursor.amount()?,
                capacity: cursor.u64()?,
                minimum_units: cursor.u64()?,
                maximum_units: cursor.u64()?,
                expires_at: cursor.u64()?,
                verification: VerificationModel::decode(cursor.byte()?)?,
            };
            cursor.finish()?;
            let mut scratch = [0; OFFER_CAPACITY];
            absent(OFFER_PREFIX, request.id, &mut scratch)?;
            let offer = register(request, caller, height)?;
            transfer::fund_program_account(ProgramDeposit::new(
                ProgramAccountSeed::new(offer.stake_seed)?,
                offer.stake_account,
                offer.asset,
                offer.stake,
            )?)?;
            let mut stake_bytes = [0; stake::STAKE_CAPACITY];
            absent(stake::STAKE_PREFIX, offer.id, &mut stake_bytes)?;
            let stake_written = stake::encode_stake(&Stake::post(&offer)?, &mut stake_bytes)?;
            let written = encode_offer(offer, &mut scratch)?;
            write_state(OFFER_PREFIX, offer.id, &scratch[..written])?;
            write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
            emit(TOPIC_OFFER, &scratch[..written])?;
            Ok(CallResult::OK)
        }
        OPEN_LEASE => {
            let request = OpenLease {
                id: cursor.array()?,
                offer_id: cursor.array()?,
                tenant: cursor.account()?,
                refund: cursor.account()?,
                escrow_account: cursor.account()?,
                escrow_seed: cursor.seed()?,
                units: cursor.u64()?,
                funded: cursor.amount()?,
                expires_at: cursor.u64()?,
            };
            cursor.finish()?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(
                OFFER_PREFIX,
                request.offer_id,
                &mut offer_bytes,
            )?)?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            absent(LEASE_PREFIX, request.id, &mut lease_bytes)?;
            let (offer, lease) = open(offer, request, caller, height)?;
            let mut stake_bytes = [0; stake::STAKE_CAPACITY];
            let stake =
                stake::decode_stake(read_state(stake::STAKE_PREFIX, offer.id, &mut stake_bytes)?)?
                    .lock(&offer, &lease, height)?;
            transfer::fund_program_account(ProgramDeposit::new(
                ProgramAccountSeed::new(lease.escrow_seed)?,
                lease.escrow_account,
                lease.asset,
                lease.funded,
            )?)?;
            let mut offer_output = [0; OFFER_CAPACITY];
            let offer_written = encode_offer(offer, &mut offer_output)?;
            let lease_written = encode_lease(&lease, &mut lease_bytes)?;
            let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
            write_state(OFFER_PREFIX, offer.id, &offer_output[..offer_written])?;
            write_state(LEASE_PREFIX, lease.id, &lease_bytes[..lease_written])?;
            write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
            emit(TOPIC_LEASE, &lease_bytes[..lease_written])?;
            Ok(CallResult::OK)
        }
        SETTLE_LEASE => Err(malformed()),
        arbitration::AUTHORIZE_SANDBOX_PROFILE => {
            use layerx_program_sdk::arbiter::{
                MarketSandboxProfile, MARKET_SANDBOX_PROFILE_CAPACITY,
            };
            let profile = MarketSandboxProfile::decode(cursor.remainder())?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(
                LEASE_PREFIX,
                profile.lease_id,
                &mut lease_bytes,
            )?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
            absent(arbitration::PROFILE_PREFIX, lease.id, &mut profile_bytes)?;
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            absent(CLAIM_PREFIX, lease.id, &mut claim_bytes)?;
            let mut namespace_preimage = [0; 128];
            let mut offset = 0;
            append(
                &mut namespace_preimage,
                &mut offset,
                arbitration::NAMESPACE_DOMAIN,
            )?;
            append(
                &mut namespace_preimage,
                &mut offset,
                &profile.sandbox_program,
            )?;
            append(&mut namespace_preimage, &mut offset, &profile.lease_id)?;
            let namespace = layerx_program_sdk::crypto::hash(
                layerx_program_sdk::crypto::HashAlgorithm::Sha256,
                layerx_program_sdk::crypto::HashInput::new(&namespace_preimage[..offset])?,
            )?;
            let profile = arbitration::authorize_profile(
                &offer,
                &lease,
                profile,
                caller,
                Context::executing_program()?.bytes(),
                attest::require_ready_commitment(lease.id)?,
                namespace,
                height,
            )?;
            let length = profile.encode(&mut profile_bytes)?;
            write_state(
                arbitration::PROFILE_PREFIX,
                lease.id,
                &profile_bytes[..length],
            )?;
            emit(arbitration::TOPIC_PROFILE, &profile_bytes[..length])?;
            Ok(CallResult::OK)
        }
        arbitration::COMMIT_SANDBOX_BILLING => {
            use layerx_program_sdk::arbiter::{
                MarketBillingCommitment, MarketSandboxProfile, MARKET_SANDBOX_BILLING_CAPACITY,
                MARKET_SANDBOX_PROFILE_CAPACITY,
            };
            let lease_id = cursor.array()?;
            let billing = MarketBillingCommitment::decode(cursor.remainder())?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            let mut profile_bytes = [0; MARKET_SANDBOX_PROFILE_CAPACITY];
            let profile_encoding =
                read_state(arbitration::PROFILE_PREFIX, lease.id, &mut profile_bytes)?;
            let profile = MarketSandboxProfile::decode(profile_encoding)?;
            if profile.market_program != Context::executing_program()?.bytes()
                || profile.attested_input_commitment != attest::require_ready_commitment(lease.id)?
            {
                return Err(malformed());
            }
            let profile_digest = layerx_program_sdk::crypto::hash(
                layerx_program_sdk::crypto::HashAlgorithm::Sha256,
                layerx_program_sdk::crypto::HashInput::new(profile_encoding)?,
            )?;
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            absent(CLAIM_PREFIX, lease.id, &mut claim_bytes)?;
            let mut identity = [0; ID_BYTES];
            absent(CLAIM_ID_PREFIX, profile.claim_id, &mut identity)?;
            let mut billing_bytes = [0; MARKET_SANDBOX_BILLING_CAPACITY];
            absent(arbitration::BILLING_PREFIX, lease.id, &mut billing_bytes)?;
            let (claim, _) = arbitration::billing_claim(
                offer,
                &lease,
                &profile,
                &billing,
                profile_digest,
                caller,
                height,
            )?;
            arbitration::matches_claim(&profile, &billing, &claim)?;
            let claim_length = settle::encode_claim(&claim, &mut claim_bytes)?;
            let billing_length = billing.encode(&mut billing_bytes)?;
            write_state(CLAIM_PREFIX, lease.id, &claim_bytes[..claim_length])?;
            write_state(CLAIM_ID_PREFIX, claim.id, &lease.id)?;
            write_state(
                arbitration::BILLING_PREFIX,
                lease.id,
                &billing_bytes[..billing_length],
            )?;
            emit(TOPIC_CLAIM, &claim_bytes[..claim_length])?;
            emit(arbitration::TOPIC_BILLING, &billing_bytes[..billing_length])?;
            Ok(CallResult::OK)
        }
        COMMIT_USAGE => {
            let commitment = settle::ProviderCommitment {
                id: cursor.array()?,
                lease_id: cursor.array()?,
                input_commitment: cursor.array()?,
                output_digest: cursor.array()?,
                execution_state_root: cursor.array()?,
                usage: settle::MeteredUsageClaim {
                    compute_units: cursor.u64()?,
                    memory_byte_batches: cursor.u64()?,
                    storage_read_bytes: cursor.u64()?,
                    storage_written_bytes: cursor.u64()?,
                    ingress_bytes: cursor.u64()?,
                    egress_bytes: cursor.u64()?,
                },
                payable: cursor.amount()?,
                challenger_stake: cursor.amount()?,
                challenge_window_batches: cursor.u64()?,
            };
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(
                LEASE_PREFIX,
                commitment.lease_id,
                &mut lease_bytes,
            )?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            if attest::require_ready_commitment(lease.id)? != commitment.input_commitment {
                return Err(malformed());
            }
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            absent(CLAIM_PREFIX, lease.id, &mut claim_bytes)?;
            let mut identity = [0; ID_BYTES];
            absent(CLAIM_ID_PREFIX, commitment.id, &mut identity)?;
            let (claim, _) = settle::commit_usage(offer, &lease, commitment, caller, height)?;
            let written = settle::encode_claim(&claim, &mut claim_bytes)?;
            write_state(CLAIM_PREFIX, lease.id, &claim_bytes[..written])?;
            write_state(CLAIM_ID_PREFIX, claim.id, &lease.id)?;
            emit(TOPIC_CLAIM, &claim_bytes[..written])?;
            Ok(CallResult::OK)
        }
        CHALLENGE_USAGE => {
            let lease_id = cursor.array()?;
            let challenge_id = cursor.array()?;
            let stake_account = cursor.account()?;
            let stake_seed = cursor.seed()?;
            let stake = cursor.amount()?;
            let contradictory = settle::ContradictingCommitment {
                input_commitment: cursor.array()?,
                output_digest: cursor.array()?,
                execution_state_root: cursor.array()?,
                usage: settle::MeteredUsageClaim {
                    compute_units: cursor.u64()?,
                    memory_byte_batches: cursor.u64()?,
                    storage_read_bytes: cursor.u64()?,
                    storage_written_bytes: cursor.u64()?,
                    ingress_bytes: cursor.u64()?,
                    egress_bytes: cursor.u64()?,
                },
            };
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            let claim =
                settle::decode_claim(read_state(CLAIM_PREFIX, lease.id, &mut claim_bytes)?)?;
            let mut challenge_bytes = [0; settle::CHALLENGE_CAPACITY];
            absent(CHALLENGE_PREFIX, lease.id, &mut challenge_bytes)?;
            let mut identity = [0; ID_BYTES];
            absent(CHALLENGE_ID_PREFIX, challenge_id, &mut identity)?;
            let (claim, challenge) = settle::challenge(
                offer,
                &lease,
                claim,
                &settle::ChallengeRequest {
                    challenge_id,
                    challenger: caller,
                    stake_account,
                    stake_seed,
                    stake,
                    contradictory,
                },
                height,
            )?;
            let (profile, billing) = sandbox_commitments(lease.id)?;
            arbitration::matches_claim(&profile, &billing, &claim)?;
            let mut dispute_bytes = [0; dispute::DISPUTE_CAPACITY];
            absent(dispute::DISPUTE_PREFIX, lease.id, &mut dispute_bytes)?;
            let (opened, record) = dispute::Dispute::open(
                &claim,
                &challenge,
                billing.provider_trace_root,
                billing.boundary_count,
                height,
            )?;
            let opened = absorb(opened, &record)?;
            settle::fund_challenge(challenge, lease.asset)?;
            let claim_written = settle::encode_claim(&claim, &mut claim_bytes)?;
            let challenge_written = settle::encode_challenge(&challenge, &mut challenge_bytes)?;
            let dispute_written = dispute::encode_dispute(&opened, &mut dispute_bytes)?;
            write_state(
                dispute::DISPUTE_PREFIX,
                lease.id,
                &dispute_bytes[..dispute_written],
            )?;
            emit(dispute::TOPIC_DISPUTE, &dispute_bytes[..dispute_written])?;
            write_state(CLAIM_PREFIX, lease.id, &claim_bytes[..claim_written])?;
            write_state(
                CHALLENGE_PREFIX,
                lease.id,
                &challenge_bytes[..challenge_written],
            )?;
            write_state(CHALLENGE_ID_PREFIX, challenge.id, &lease.id)?;
            emit(TOPIC_CLAIM, &claim_bytes[..claim_written])?;
            emit(TOPIC_CHALLENGE, &challenge_bytes[..challenge_written])?;
            Ok(CallResult::OK)
        }
        FINALIZE_USAGE => {
            let lease_id = cursor.array()?;
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            let claim =
                settle::decode_claim(read_state(CLAIM_PREFIX, lease.id, &mut claim_bytes)?)?;
            let (offer, lease, claim, plan) =
                settle::finalize_unchallenged(offer, lease, claim, height)?;
            let mut stake_bytes = [0; stake::STAKE_CAPACITY];
            let stake =
                stake::decode_stake(read_state(stake::STAKE_PREFIX, offer.id, &mut stake_bytes)?)?
                    .release(&lease, Some(&claim), height)?;
            settle::execute_settlement(&lease, None, plan)?;
            let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
            write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
            publish_settlement(SettlementRecord::new(
                &lease,
                claim.id,
                plan.provider,
                plan.tenant,
                height,
            )?)?;
            let mut offer_output = [0; OFFER_CAPACITY];
            let mut lease_output = [0; LEASE_CAPACITY];
            let offer_written = encode_offer(offer, &mut offer_output)?;
            let lease_written = encode_lease(&lease, &mut lease_output)?;
            let claim_written = settle::encode_claim(&claim, &mut claim_bytes)?;
            write_state(OFFER_PREFIX, offer.id, &offer_output[..offer_written])?;
            write_state(LEASE_PREFIX, lease.id, &lease_output[..lease_written])?;
            write_state(CLAIM_PREFIX, lease.id, &claim_bytes[..claim_written])?;
            emit(TOPIC_OFFER, &offer_output[..offer_written])?;
            emit(TOPIC_LEASE, &lease_output[..lease_written])?;
            emit(TOPIC_CLAIM, &claim_bytes[..claim_written])?;
            Ok(CallResult::OK)
        }
        EXPIRE_LEASE => {
            let lease_id = cursor.array()?;
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut offer_bytes = [0; OFFER_CAPACITY];
            let offer = decode_offer(read_state(OFFER_PREFIX, lease.offer_id, &mut offer_bytes)?)?;
            let mut claim_bytes = [0; settle::CLAIM_CAPACITY];
            if let Some(bytes) = read_optional_state(CLAIM_PREFIX, lease.id, &mut claim_bytes)? {
                let claim = settle::decode_claim(bytes)?;
                let (offer, lease, claim, plan) =
                    settle::finalize_unchallenged(offer, lease, claim, height)?;
                let mut stake_bytes = [0; stake::STAKE_CAPACITY];
                let stake = stake::decode_stake(read_state(
                    stake::STAKE_PREFIX,
                    offer.id,
                    &mut stake_bytes,
                )?)?
                .release(&lease, Some(&claim), height)?;
                settle::execute_settlement(&lease, None, plan)?;
                let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
                write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
                publish_settlement(SettlementRecord::new(
                    &lease,
                    claim.id,
                    plan.provider,
                    plan.tenant,
                    height,
                )?)?;
                let mut offer_output = [0; OFFER_CAPACITY];
                let mut lease_output = [0; LEASE_CAPACITY];
                let offer_written = encode_offer(offer, &mut offer_output)?;
                let lease_written = encode_lease(&lease, &mut lease_output)?;
                let claim_written = settle::encode_claim(&claim, &mut claim_bytes)?;
                write_state(OFFER_PREFIX, offer.id, &offer_output[..offer_written])?;
                write_state(LEASE_PREFIX, lease.id, &lease_output[..lease_written])?;
                write_state(CLAIM_PREFIX, lease.id, &claim_bytes[..claim_written])?;
                emit(TOPIC_OFFER, &offer_output[..offer_written])?;
                emit(TOPIC_LEASE, &lease_output[..lease_written])?;
                emit(TOPIC_CLAIM, &claim_bytes[..claim_written])?;
            } else {
                let (offer, lease) = expire(offer, lease, height)?;
                let mut stake_bytes = [0; stake::STAKE_CAPACITY];
                let stake = stake::decode_stake(read_state(
                    stake::STAKE_PREFIX,
                    offer.id,
                    &mut stake_bytes,
                )?)?
                .release(&lease, None, height)?;
                let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
                write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
                transfer::pay_from_program_account(ProgramAccountPayment::new(
                    ProgramAccountSeed::new(lease.escrow_seed)?,
                    lease.escrow_account,
                    lease.asset,
                    lease.tenant_refund,
                    lease.funded,
                )?)?;
                publish_settlement(SettlementRecord::new(
                    &lease,
                    [0; ID_BYTES],
                    Amount::ZERO,
                    lease.funded,
                    height,
                )?)?;
                let mut offer_output = [0; OFFER_CAPACITY];
                let mut lease_output = [0; LEASE_CAPACITY];
                let offer_written = encode_offer(offer, &mut offer_output)?;
                let lease_written = encode_lease(&lease, &mut lease_output)?;
                write_state(OFFER_PREFIX, offer.id, &offer_output[..offer_written])?;
                write_state(LEASE_PREFIX, lease.id, &lease_output[..lease_written])?;
                emit(TOPIC_OFFER, &offer_output[..offer_written])?;
                emit(TOPIC_LEASE, &lease_output[..lease_written])?;
            }
            Ok(CallResult::OK)
        }
        CLOSE_OFFER => {
            let offer_id = cursor.array()?;
            cursor.finish()?;
            let mut bytes = [0; OFFER_CAPACITY];
            let offer = close(
                decode_offer(read_state(OFFER_PREFIX, offer_id, &mut bytes)?)?,
                caller,
            )?;
            let mut stake_bytes = [0; stake::STAKE_CAPACITY];
            let (stake, withdrawal) =
                stake::decode_stake(read_state(stake::STAKE_PREFIX, offer.id, &mut stake_bytes)?)?
                    .withdraw(&offer, caller)?;
            stake::execute(&withdrawal)?;
            let stake_written = stake::encode_stake(&stake, &mut stake_bytes)?;
            let mut output = [0; OFFER_CAPACITY];
            let written = encode_offer(offer, &mut output)?;
            write_state(OFFER_PREFIX, offer.id, &output[..written])?;
            write_state(stake::STAKE_PREFIX, offer.id, &stake_bytes[..stake_written])?;
            emit(TOPIC_OFFER, &output[..written])?;
            Ok(CallResult::OK)
        }
        dispute::REVEAL_BOUNDARY => {
            let lease_id = cursor.array()?;
            let position = cursor.u32()?;
            let evidence = cursor.remainder();
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            if caller != lease.provider {
                return Err(malformed());
            }
            let mut dispute_bytes = [0; dispute::DISPUTE_CAPACITY];
            let current = dispute::decode_dispute(read_state(
                dispute::DISPUTE_PREFIX,
                lease.id,
                &mut dispute_bytes,
            )?)?;
            let (profile, _) = sandbox_commitments(lease.id)?;
            let opened = adjudicate(
                lease.id,
                layerx_program_sdk::arbiter::MARKET_STEP_OPEN,
                position,
                evidence,
            )?;
            let (next, record) = current.reveal(
                height,
                position,
                &opened,
                profile.initial_execution_state_root,
            )?;
            advance(&lease, next, &record, height)
        }
        dispute::RESPOND_BOUNDARY => {
            let lease_id = cursor.array()?;
            let agree = match cursor.byte()? {
                0 => false,
                1 => true,
                _ => return Err(malformed()),
            };
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut challenge_bytes = [0; settle::CHALLENGE_CAPACITY];
            let challenge = settle::decode_challenge(read_state(
                CHALLENGE_PREFIX,
                lease.id,
                &mut challenge_bytes,
            )?)?;
            if caller != challenge.challenger {
                return Err(malformed());
            }
            let mut dispute_bytes = [0; dispute::DISPUTE_CAPACITY];
            let current = dispute::decode_dispute(read_state(
                dispute::DISPUTE_PREFIX,
                lease.id,
                &mut dispute_bytes,
            )?)?;
            let (next, record) = current.respond(height, agree)?;
            advance(&lease, next, &record, height)
        }
        dispute::RESOLVE_DISPUTE => {
            let lease_id = cursor.array()?;
            let evidence = cursor.remainder();
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            let mut dispute_bytes = [0; dispute::DISPUTE_CAPACITY];
            let current = dispute::decode_dispute(read_state(
                dispute::DISPUTE_PREFIX,
                lease.id,
                &mut dispute_bytes,
            )?)?;
            let (next, record) = if height > current.deadline {
                if !evidence.is_empty() {
                    return Err(malformed());
                }
                current.timeout(height)?
            } else {
                let (mode, position) = current.adjudication()?;
                let judged = adjudicate(lease.id, mode, position, evidence)?;
                current.adjudicate(height, &judged)?
            };
            advance(&lease, next, &record, height)
        }
        CONFIGURE_ATTESTERS => {
            let lease_id = cursor.array()?;
            let revision = cursor.u64()?;
            let count = usize::from(cursor.byte()?);
            if count == 0 || count > attest::MAX_ATTESTERS {
                return Err(malformed());
            }
            let mut entries = [None; attest::MAX_ATTESTERS];
            for entry in entries.iter_mut().take(count) {
                let name_length = usize::from(cursor.byte()?);
                let name = cursor.take(name_length)?;
                *entry = Some(attest::Attester {
                    name,
                    ed25519_key: cursor.array()?,
                });
            }
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            if lease.tenant != caller || lease.status != LeaseStatus::Funded {
                return Err(malformed());
            }
            let mut scratch = [0; attest::ATTESTER_SET_CAPACITY];
            attest::configure(lease_id, caller, revision, entries, &mut scratch)?;
            Ok(CallResult::OK)
        }
        COMMIT_EXTERNAL_INPUT => {
            let commitment = attest::InputCommitment {
                lease_id: cursor.array()?,
                input_id: cursor.array()?,
                payload_digest: cursor.array()?,
                payload_length: cursor.u64()?,
                source: match cursor.byte()? {
                    1 => attest::ExternalInputSource::HttpsApi,
                    2 => attest::ExternalInputSource::HardwareSensor,
                    3 => attest::ExternalInputSource::ConfidentialCompute,
                    4 => attest::ExternalInputSource::HumanOperator,
                    _ => return Err(malformed()),
                },
                source_locator_digest: cursor.array()?,
            };
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(
                LEASE_PREFIX,
                commitment.lease_id,
                &mut lease_bytes,
            )?)?;
            if lease.tenant != caller || lease.status != LeaseStatus::Funded {
                return Err(malformed());
            }
            attest::commit(commitment, caller)?;
            Ok(CallResult::OK)
        }
        SEAL_EXTERNAL_INPUTS => {
            let lease_id = cursor.array()?;
            cursor.finish()?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(LEASE_PREFIX, lease_id, &mut lease_bytes)?)?;
            if lease.tenant != caller || lease.status != LeaseStatus::Funded {
                return Err(malformed());
            }
            attest::seal(lease_id, caller, height)?;
            Ok(CallResult::OK)
        }
        SUBMIT_ATTESTATION => {
            let attestation = attest::decode_attestation(cursor.remainder())?;
            let mut lease_bytes = [0; LEASE_CAPACITY];
            let lease = decode_lease(read_state(
                LEASE_PREFIX,
                attestation.input.lease_id,
                &mut lease_bytes,
            )?)?;
            if lease.provider != caller || lease.status != LeaseStatus::Funded {
                return Err(malformed());
            }
            attest::admit(attestation, height)?;
            Ok(CallResult::OK)
        }
        _ => Err(malformed()),
    }
}

#[cfg(target_arch = "wasm32")]
layerx_program_sdk::entrypoint!(invoke);

#[cfg(test)]
mod tests {
    use super::*;

    fn account(value: u8) -> AccountId {
        AccountId::new([value; 32]).unwrap_or_else(|error| panic!("account: {error}"))
    }
    fn asset() -> AssetId {
        AssetId::new([9; 32]).unwrap_or_else(|error| panic!("asset: {error}"))
    }
    fn offer(provider: AccountId, seed: &[u8]) -> Offer<'_> {
        register(
            RegisterOffer {
                id: [1; 32],
                provider,
                payout: account(2),
                asset: asset(),
                stake_account: account(3),
                stake_seed: seed,
                stake: Amount::from_integer(500u64),
                unit_price: Amount::from_integer(4u64),
                capacity: 100,
                minimum_units: 2,
                maximum_units: 20,
                expires_at: 50,
                verification: VerificationModel::FraudProvable,
            },
            provider,
            1,
        )
        .unwrap_or_else(|error| panic!("offer: {error}"))
    }

    #[test]
    fn funded_lease_holds_capacity_until_expiry_without_direct_settlement() {
        let provider = account(1);
        let tenant = account(4);
        let offer = offer(provider, b"stake/offer-1");
        let request = OpenLease {
            id: [5; 32],
            offer_id: offer.id,
            tenant,
            refund: tenant,
            escrow_account: account(6),
            escrow_seed: b"lease/5",
            units: 10,
            funded: Amount::from_integer(40u64),
            expires_at: 20,
        };
        let (offer, lease) =
            open(offer, request, tenant, 2).unwrap_or_else(|error| panic!("open: {error}"));
        assert_eq!(offer.available_capacity, 90);
        let (offer, lease) =
            expire(offer, lease, 20).unwrap_or_else(|error| panic!("expire: {error}"));
        assert_eq!(offer.available_capacity, 100);
        assert_eq!(lease.status, LeaseStatus::ExpiredRefunded);
    }

    #[test]
    fn provider_absence_refunds_expired_lease() {
        let provider = account(1);
        let tenant = account(4);
        let offer = offer(provider, b"stake/offer-1");
        let request = OpenLease {
            id: [5; 32],
            offer_id: offer.id,
            tenant,
            refund: tenant,
            escrow_account: account(6),
            escrow_seed: b"lease/5",
            units: 10,
            funded: Amount::from_integer(40u64),
            expires_at: 20,
        };
        let (offer, lease) =
            open(offer, request, tenant, 2).unwrap_or_else(|error| panic!("open: {error}"));
        let (offer, lease) =
            expire(offer, lease, 20).unwrap_or_else(|error| panic!("expire: {error}"));
        assert_eq!(offer.available_capacity, 100);
        assert_eq!(lease.status, LeaseStatus::ExpiredRefunded);
    }

    #[test]
    fn unfunded_and_mid_work_expiry_are_refused() {
        let provider = account(1);
        let tenant = account(4);
        let offer = offer(provider, b"stake/offer-1");
        let request = OpenLease {
            id: [5; 32],
            offer_id: offer.id,
            tenant,
            refund: tenant,
            escrow_account: account(6),
            escrow_seed: b"lease/5",
            units: 10,
            funded: Amount::from_integer(39u64),
            expires_at: 20,
        };
        assert!(open(offer, request, tenant, 2).is_err());
        let funded = OpenLease {
            funded: Amount::from_integer(40u64),
            ..request
        };
        let (offer, lease) =
            open(offer, funded, tenant, 2).unwrap_or_else(|error| panic!("open: {error}"));
        assert!(expire(offer, lease, 19).is_err());
    }

    fn funded_lease() -> (Offer<'static>, ComputeLease<'static>) {
        let provider = account(1);
        let tenant = account(4);
        let offer = offer(provider, b"stake/offer-1");
        open(
            offer,
            OpenLease {
                id: [5; 32],
                offer_id: offer.id,
                tenant,
                refund: tenant,
                escrow_account: account(6),
                escrow_seed: b"lease/5",
                units: 10,
                funded: Amount::from_integer(40u64),
                expires_at: 20,
            },
            tenant,
            2,
        )
        .unwrap_or_else(|error| panic!("funded lease: {error}"))
    }

    #[test]
    fn canonical_market_state_is_public_and_strictly_decoded() {
        let (offer, lease) = funded_lease();
        let mut offer_bytes = [0; 263 + MAX_SEED_BYTES];
        let mut lease_bytes = [0; 308 + MAX_SEED_BYTES];
        let offer_length = encode_offer(offer, &mut offer_bytes)
            .unwrap_or_else(|error| panic!("offer encoding: {error}"));
        let lease_length = encode_lease(&lease, &mut lease_bytes)
            .unwrap_or_else(|error| panic!("lease encoding: {error}"));
        assert_eq!(decode_offer(&offer_bytes[..offer_length]), Ok(offer));
        assert_eq!(decode_lease(&lease_bytes[..lease_length]), Ok(lease));
        assert_eq!(
            decode_lease(&lease_bytes[..lease_length])
                .unwrap_or_else(|error| panic!("lease: {error}"))
                .verification,
            VerificationModel::FraudProvable
        );
        for length in 0..offer_length {
            assert!(decode_offer(&offer_bytes[..length]).is_err());
        }
        for length in 0..lease_length {
            assert!(decode_lease(&lease_bytes[..length]).is_err());
        }
        assert!(decode_offer(&offer_bytes[..offer_length + 1]).is_err());
        assert!(decode_lease(&lease_bytes[..lease_length + 1]).is_err());
        for index in 0..3 {
            let mut changed = offer_bytes;
            changed[index] = 255;
            assert!(decode_offer(&changed[..offer_length]).is_err());
            let mut changed = lease_bytes;
            changed[index] = 255;
            assert!(decode_lease(&changed[..lease_length]).is_err());
        }
    }

    #[test]
    fn delivered_usage_settlement_conserves_escrow_and_is_readable() {
        let (offer, lease) = funded_lease();
        let (claim, window) = settle::commit_usage(
            offer,
            &lease,
            settle::ProviderCommitment {
                id: [7; 32],
                lease_id: lease.id,
                input_commitment: [11; 32],
                output_digest: [12; 32],
                execution_state_root: [13; 32],
                usage: settle::MeteredUsageClaim {
                    compute_units: 5,
                    memory_byte_batches: 0,
                    storage_read_bytes: 0,
                    storage_written_bytes: 0,
                    ingress_bytes: 0,
                    egress_bytes: 0,
                },
                payable: Amount::from_integer(20u64),
                challenger_stake: Amount::from_integer(50u64),
                challenge_window_batches: 2,
            },
            offer.provider,
            3,
        )
        .unwrap_or_else(|error| panic!("usage: {error}"));
        assert!(
            settle::finalize_unchallenged(offer, lease, claim, window.last_challenge_height)
                .is_err()
        );
        let (offer, lease, claim, plan) = settle::finalize_unchallenged(offer, lease, claim, 6)
            .unwrap_or_else(|error| panic!("settlement: {error}"));
        assert_eq!(offer.available_capacity, offer.total_capacity);
        assert_eq!(plan.total(), Ok(lease.funded));
        assert_eq!(plan.provider, Amount::from_integer(20u64));
        assert_eq!(plan.tenant, Amount::from_integer(20u64));
        assert!(settle::finalize_unchallenged(offer, lease, claim, 7).is_err());
        let record = SettlementRecord::new(&lease, claim.id, plan.provider, plan.tenant, 6)
            .unwrap_or_else(|error| panic!("record: {error}"));
        let mut bytes = [0; SETTLEMENT_CAPACITY];
        assert_eq!(
            encode_settlement(&record, &mut bytes),
            Ok(SETTLEMENT_CAPACITY)
        );
        assert_eq!(decode_settlement(&bytes), Ok(record));
        assert_eq!(record.escrow_account, lease.escrow_account);
        assert_eq!(record.provider_payout, lease.provider_payout);
        assert_eq!(record.tenant_refund, lease.tenant_refund);
        assert_eq!(record.asset, lease.asset);
    }

    #[test]
    fn absent_provider_expiry_records_exact_refund_once() {
        let (offer, lease) = funded_lease();
        assert!(expire(offer, lease, 19).is_err());
        let (offer, lease) =
            expire(offer, lease, 20).unwrap_or_else(|error| panic!("expiry: {error}"));
        assert!(expire(offer, lease, 21).is_err());
        let record = SettlementRecord::new(&lease, [0; 32], Amount::ZERO, lease.funded, 20)
            .unwrap_or_else(|error| panic!("refund: {error}"));
        assert_eq!(record.tenant_paid, Amount::from_integer(40u64));
        assert_eq!(record.provider_paid, Amount::ZERO);
        assert!(SettlementRecord::new(&lease, [0; 32], Amount::ZERO, lease.funded, 19).is_err());
        let closed = close(offer, offer.provider).unwrap_or_else(|error| panic!("close: {error}"));
        assert_eq!(closed.status, OfferStatus::Closed);
        assert!(close(closed, closed.provider).is_err());
    }

    #[test]
    fn renter_funding_and_provider_capacity_are_exact_obligations() {
        let (reserved, lease) = funded_lease();
        assert!(close(reserved, reserved.provider).is_err());
        let original = offer(account(1), b"stake/offer-1");
        let request = OpenLease {
            id: [5; 32],
            offer_id: original.id,
            tenant: account(4),
            refund: account(4),
            escrow_account: account(6),
            escrow_seed: b"lease/5",
            units: 10,
            funded: Amount::from_integer(40u64),
            expires_at: 20,
        };
        for funding in [0, 39, 41] {
            assert!(open(
                original,
                OpenLease {
                    funded: Amount::from_integer(funding as u64),
                    ..request
                },
                request.tenant,
                2
            )
            .is_err());
        }
        assert!(open(original, request, original.provider, 2).is_err());
        for units in [0, 1, 21, 101] {
            assert!(open(
                original,
                OpenLease {
                    units,
                    funded: original
                        .unit_price
                        .checked_mul(Amount::from_integer(units))
                        .unwrap_or_else(|error| panic!("price: {error}")),
                    ..request
                },
                request.tenant,
                2
            )
            .is_err());
        }
        assert!(open(
            original,
            OpenLease {
                expires_at: 51,
                ..request
            },
            request.tenant,
            2
        )
        .is_err());
        assert!(open(
            original,
            OpenLease {
                expires_at: 2,
                ..request
            },
            request.tenant,
            2
        )
        .is_err());
        assert_eq!(original.available_capacity, 100);
        assert_eq!(
            reserved.available_capacity + lease.units,
            original.total_capacity
        );
    }

    #[test]
    fn settlement_decoder_refuses_invalid_totals_status_and_noncanonical_bytes() {
        let (offer, lease) = funded_lease();
        let (_, lease) = expire(offer, lease, 20).unwrap_or_else(|error| panic!("expiry: {error}"));
        let record = SettlementRecord::new(&lease, [0; 32], Amount::ZERO, lease.funded, 20)
            .unwrap_or_else(|error| panic!("record: {error}"));
        let mut bytes = [0; SETTLEMENT_CAPACITY];
        encode_settlement(&record, &mut bytes).unwrap_or_else(|error| panic!("encoding: {error}"));
        for length in 0..SETTLEMENT_CAPACITY {
            assert!(decode_settlement(&bytes[..length]).is_err());
        }
        let mut trailing = bytes.to_vec();
        trailing.push(0);
        assert!(decode_settlement(&trailing).is_err());
        for index in [0, 1, 66, 241, 257, 273] {
            let mut changed = bytes;
            changed[index] ^= 1;
            assert!(decode_settlement(&changed).is_err(), "mutation {index}");
        }
        let mut short = [0xa5; SETTLEMENT_CAPACITY - 1];
        assert!(encode_settlement(&record, &mut short).is_err());
        assert_eq!(short, [0xa5; SETTLEMENT_CAPACITY - 1]);
        assert!(SettlementRecord::new(
            &lease,
            [0; 32],
            Amount::from_integer(1u64),
            lease.funded,
            20
        )
        .is_err());
        assert!(SettlementRecord::new(
            &lease,
            [0; 32],
            Amount::MAX,
            Amount::from_integer(1u64),
            20
        )
        .is_err());
    }

    #[test]
    fn marketplace_selector_refuses_legacy_unchecked_settlement_and_unknown_operations() {
        for operation in 0..=255 {
            let bytes = [VERSION, operation];
            let result = decode_operation(&mut Cursor::new(&bytes));
            assert_eq!(result.is_ok(), matches!(operation, 1 | 2 | 4..=17));
        }
        for bytes in [vec![], vec![VERSION], vec![0, 1], vec![2, 1], vec![255, 1]] {
            assert!(decode_operation(&mut Cursor::new(&bytes)).is_err());
        }
    }
}
