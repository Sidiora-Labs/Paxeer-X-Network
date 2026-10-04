use layerx_program_sdk::arbiter::{MarketBillingCommitment, MarketSandboxProfile};
use layerx_program_sdk::{AccountId, Amount, Field, ProgramError, Reason};

use crate::{ComputeLease, LeaseStatus, Offer, VerificationModel};

pub const AUTHORIZE_SANDBOX_PROFILE: u8 = 13;
pub const COMMIT_SANDBOX_BILLING: u8 = 14;
pub const PROFILE_PREFIX: &[u8] = b"lx.market.arbitration-profile/";
pub const BILLING_PREFIX: &[u8] = b"lx.market.arbitration-billing/";
pub const TOPIC_PROFILE: &[u8] = b"lx.market.arbitration-profile";
pub const TOPIC_BILLING: &[u8] = b"lx.market.arbitration-billing";
pub const NAMESPACE_DOMAIN: &[u8] = b"LayerX/programs/sandbox/namespace/v1\0";

pub fn authorize_profile(
    offer: &Offer<'_>,
    lease: &ComputeLease<'_>,
    profile: MarketSandboxProfile,
    principal: AccountId,
    market_program: [u8; 32],
    sealed_input_commitment: [u8; 32],
    expected_namespace: [u8; 32],
    height: u64,
) -> Result<MarketSandboxProfile, ProgramError> {
    profile.validate()?;
    if principal != lease.tenant
        || profile.tenant != lease.tenant.bytes()
        || profile.provider != lease.provider.bytes()
        || profile.market_program != market_program
        || profile.lease_id != lease.id
        || profile.offer_id != offer.id
        || lease.offer_id != offer.id
        || lease.provider != offer.provider
        || lease.status != LeaseStatus::Funded
        || lease.verification != VerificationModel::FraudProvable
        || offer.verification != VerificationModel::FraudProvable
        || height < lease.opened_at
        || height > profile.interval_start
        || profile.response_deadline >= lease.expires_at
        || profile.attested_input_commitment != sealed_input_commitment
        || profile.namespace != expected_namespace
        || Amount::from_integer(profile.fee_budget) > lease.funded
    {
        return Err(malformed());
    }
    Ok(profile)
}

pub fn billing_claim(
    offer: Offer<'_>,
    lease: &ComputeLease<'_>,
    profile: &MarketSandboxProfile,
    billing: &MarketBillingCommitment,
    actual_profile_digest: [u8; 32],
    principal: AccountId,
    height: u64,
) -> Result<(crate::settle::UsageClaim, crate::settle::ChallengeWindow), ProgramError> {
    profile.validate()?;
    billing.validate()?;
    if principal != lease.provider
        || profile.provider != principal.bytes()
        || profile.tenant != lease.tenant.bytes()
        || profile.lease_id != lease.id
        || profile.offer_id != offer.id
        || profile.provider != offer.provider.bytes()
        || lease.verification != VerificationModel::FraudProvable
        || billing.profile_digest != actual_profile_digest
        || billing.boundary_count > profile.maximum_boundaries
        || height < profile.interval_end
        || height >= profile.response_deadline
        || height.checked_add(billing.challenge_window_batches) != Some(profile.response_deadline)
        || billing.usage[0] > profile.limits.cpu_fuel
        || u128::from(billing.usage[1])
            > u128::from(profile.limits.memory_bytes)
                * u128::from(profile.interval_end - profile.interval_start)
        || billing.usage[2] > profile.limits.storage_read_bytes
        || billing.usage[3] > profile.limits.storage_write_bytes
        || billing.usage[4] > u64::from(profile.maximum_bytes)
        || billing.usage[5] > profile.limits.output_bytes
    {
        return Err(malformed());
    }
    crate::settle::commit_usage(
        offer,
        lease,
        crate::settle::ProviderCommitment {
            id: profile.claim_id,
            lease_id: profile.lease_id,
            input_commitment: profile.attested_input_commitment,
            output_digest: billing.output_digest,
            execution_state_root: billing.final_execution_state_root,
            usage: crate::settle::MeteredUsageClaim {
                compute_units: billing.usage[0],
                memory_byte_batches: billing.usage[1],
                storage_read_bytes: billing.usage[2],
                storage_written_bytes: billing.usage[3],
                ingress_bytes: billing.usage[4],
                egress_bytes: billing.usage[5],
            },
            payable: Amount::from_integer(billing.payable),
            challenger_stake: Amount::from_integer(billing.challenger_stake),
            challenge_window_batches: billing.challenge_window_batches,
        },
        principal,
        height,
    )
}

pub fn matches_claim(
    profile: &MarketSandboxProfile,
    billing: &MarketBillingCommitment,
    claim: &crate::settle::UsageClaim,
) -> Result<(), ProgramError> {
    if claim.id != profile.claim_id
        || claim.lease_id != profile.lease_id
        || claim.provider.bytes() != profile.provider
        || claim.input_commitment != profile.attested_input_commitment
        || claim.execution_state_root != billing.final_execution_state_root
        || claim.output_digest != billing.output_digest
        || claim.challenge_deadline != profile.response_deadline
        || claim.payable != Amount::from_integer(billing.payable)
        || claim.challenger_stake != Amount::from_integer(billing.challenger_stake)
        || [
            claim.usage.compute_units,
            claim.usage.memory_byte_batches,
            claim.usage.storage_read_bytes,
            claim.usage.storage_written_bytes,
            claim.usage.ingress_bytes,
            claim.usage.egress_bytes,
        ] != billing.usage
    {
        return Err(malformed());
    }
    Ok(())
}

fn malformed() -> ProgramError {
    ProgramError::value(Field::CallInput, Reason::Malformed)
}
