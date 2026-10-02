use layerx_types::account::AccountId;
use layerx_types::ids::Did;
use layerx_types::payload::ModuleId;

use super::{
    fixed, hash, Activity, AmountRole, Counterparty, CounterpartyRole, Decoder, DisclosedAmount,
    DisclosureError, DisclosureFields, Encoder, Expiry,
};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedNativeIdentity {
    pub did_id: [u8; 32],
    pub primary_key: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedRecoveryPolicy {
    pub did_id: [u8; 32],
    pub recovery_root: [u8; 32],
    pub threshold: u16,
    pub delay_bounds: Option<(u64, u64)>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedNativeBudgetCreate {
    pub encoding_version: u16,
    pub budget_id: [u8; 32],
    pub budget_account: [u8; 32],
    pub asset: [u8; 32],
    pub purpose: [u8; 32],
    pub per_period_limit: u128,
    pub carry_cap: u128,
    pub initial_amount: u128,
    pub period_length_ms: u64,
    pub period_start_ms: u64,
    pub expiry_ms: u64,
    pub revocation_sequence: u64,
    pub rollover: u8,
    pub source_account: [u8; 32],
    pub source_sequence: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum DisclosedNativeOperation {
    ProgramDeploy(Box<super::DisclosedProgramDeploy>),
    ProgramUpgrade(Box<super::DisclosedProgramUpgrade>),
    ProgramCall(Box<super::DisclosedProgramCall>),
    ProgramWindDown(Box<super::DisclosedProgramWindDown>),
    LegacyProgramCall(Box<layerx_types::intent::ProgramCall>),
    IdentityRegistration(DisclosedNativeIdentity),
    RecoveryPolicy(DisclosedRecoveryPolicy),
    OwnerRotation(Box<crate::rotation::OwnerRotation>),
    BudgetCreate(Box<DisclosedNativeBudgetCreate>),
    BudgetFund(Box<DisclosedNativeBudgetFund>),
    BudgetDefund(Box<DisclosedNativeBudgetDefund>),
    BudgetRevoke(Box<DisclosedNativeBudgetRevoke>),
}

/// Verified budget record context a fund, defund or revoke disclosure is bound to.
///
/// The caller supplies values it verified from proven module and account state.
/// The disclosure checks them against what the canonical bytes and the actor
/// derive, and every field is bound into the transport bytes and audit digest.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct BudgetStateContext {
    pub budget_id: [u8; 32],
    pub owner: [u8; 32],
    pub budget_account: [u8; 32],
    pub asset: [u8; 32],
    pub source_account: [u8; 32],
    pub native_source: bool,
    pub revocation_sequence: u64,
    pub balance: u128,
    pub state_digest: [u8; 32],
    pub observed_head_sequence: u64,
    pub purpose_hash: [u8; 32],
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedNativeBudgetFund {
    pub encoding_version: u16,
    pub budget_id: [u8; 32],
    pub amount: u128,
    pub source_sequence: Option<u64>,
    pub context: BudgetStateContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedNativeBudgetDefund {
    pub budget_id: [u8; 32],
    pub amount: u128,
    pub context: BudgetStateContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DisclosedNativeBudgetRevoke {
    pub budget_id: [u8; 32],
    pub revocation_sequence: u64,
    pub context: BudgetStateContext,
}

impl BudgetStateContext {
    #[must_use]
    pub fn purpose_matches(&self, supplied: &[u8; 32]) -> bool {
        crate::ct::eq_fixed(&self.purpose_hash, supplied)
    }
}

fn encode_context(
    encoder: &mut Encoder,
    context: &BudgetStateContext,
) -> Result<(), DisclosureError> {
    encoder.fixed(&context.budget_id)?;
    encoder.fixed(&context.owner)?;
    encoder.fixed(&context.budget_account)?;
    encoder.fixed(&context.asset)?;
    encoder.fixed(&context.source_account)?;
    encoder.u8(u8::from(context.native_source))?;
    encoder.u64(context.revocation_sequence)?;
    encoder.u128(context.balance)?;
    encoder.fixed(&context.state_digest)?;
    encoder.u64(context.observed_head_sequence)?;
    encoder.fixed(&context.purpose_hash)?;
    Ok(())
}

impl DisclosedNativeOperation {
    /// # Errors
    /// Refuses disclosure fields that exceed the canonical encoding limits.
    pub fn encode(&self) -> Result<Vec<u8>, DisclosureError> {
        let mut encoder = Encoder::new(2048);
        match self {
            Self::ProgramDeploy(value) => value.encode_audit(&mut encoder)?,
            Self::ProgramUpgrade(value) => value.encode_audit(&mut encoder)?,
            Self::ProgramCall(value) => value.encode_audit(&mut encoder)?,
            Self::ProgramWindDown(value) => value.encode_audit(&mut encoder)?,
            Self::LegacyProgramCall(value) => super::program::encode_legacy(value, &mut encoder)?,
            Self::IdentityRegistration(identity) => {
                encoder.u8(1)?;
                encoder.fixed(&identity.did_id)?;
                encoder.fixed(&identity.primary_key)?;
            }
            Self::OwnerRotation(rotation) => {
                encoder.u8(5)?;
                encoder.bytes(rotation.owner().as_bytes(), 512)?;
                if let crate::rotation::OwnerRotation::Consent(consent) = rotation.as_ref() {
                    encoder.fixed(&consent.pending_public_key)?;
                }
                encoder.bytes(
                    &rotation
                        .payload()
                        .map_err(|_| DisclosureError::MalformedPayload)?,
                    1024,
                )?;
            }
            Self::RecoveryPolicy(policy) => {
                encoder.u8(3)?;
                encoder.fixed(&policy.did_id)?;
                encoder.fixed(&policy.recovery_root)?;
                encoder.u16(policy.threshold)?;
                if let Some((minimum, maximum)) = policy.delay_bounds {
                    encoder.u8(1)?;
                    encoder.u64(minimum)?;
                    encoder.u64(maximum)?;
                } else {
                    encoder.u8(0)?;
                }
            }
            Self::BudgetCreate(budget) => {
                encoder.u8(4)?;
                encoder.u16(budget.encoding_version)?;
                encoder.fixed(&budget.budget_id)?;
                encoder.fixed(&budget.budget_account)?;
                encoder.fixed(&budget.asset)?;
                encoder.fixed(&budget.purpose)?;
                encoder.u128(budget.per_period_limit)?;
                encoder.u128(budget.carry_cap)?;
                encoder.u128(budget.initial_amount)?;
                encoder.u64(budget.period_length_ms)?;
                encoder.u64(budget.period_start_ms)?;
                encoder.u64(budget.expiry_ms)?;
                encoder.u64(budget.revocation_sequence)?;
                encoder.u8(budget.rollover)?;
                encoder.fixed(&budget.source_account)?;
                encoder.u64(budget.source_sequence)?;
            }
            Self::BudgetFund(fund) => {
                encoder.u8(6)?;
                encoder.u16(fund.encoding_version)?;
                encoder.fixed(&fund.budget_id)?;
                encoder.u128(fund.amount)?;
                match fund.source_sequence {
                    Some(sequence) => {
                        encoder.u8(1)?;
                        encoder.u64(sequence)?;
                    }
                    None => encoder.u8(0)?,
                }
                encode_context(&mut encoder, &fund.context)?;
            }
            Self::BudgetDefund(defund) => {
                encoder.u8(7)?;
                encoder.fixed(&defund.budget_id)?;
                encoder.u128(defund.amount)?;
                encode_context(&mut encoder, &defund.context)?;
            }
            Self::BudgetRevoke(revoke) => {
                encoder.u8(8)?;
                encoder.fixed(&revoke.budget_id)?;
                encoder.u64(revoke.revocation_sequence)?;
                encode_context(&mut encoder, &revoke.context)?;
            }
        }
        Ok(encoder.finish())
    }
}

fn account_id(name: &str) -> Result<[u8; 32], DisclosureError> {
    let account = AccountId::parse(name).map_err(|_| DisclosureError::MalformedPayload)?;
    hash::account_id_for_protocol(&account, 3).map_err(DisclosureError::from)
}

fn hex(value: &[u8; 32]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(64);
    for byte in value {
        encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
        encoded.push(char::from(DIGITS[usize::from(byte & 15)]));
    }
    encoded
}

fn identity(activity: &Activity) -> Result<DisclosedNativeIdentity, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let mut reader = Decoder::new(activity.payload(), 0);
    if reader.u16()? != 0x7101 || reader.u16()? != 2 {
        return Err(malformed());
    }
    let value = DisclosedNativeIdentity {
        did_id: fixed(&mut reader)?,
        primary_key: fixed(&mut reader)?,
    };
    reader.finish()?;
    let did = Did::new(activity.actor_did()).map_err(|_| malformed())?;
    if hash::did_id_for_protocol(&did, 3)? != value.did_id
        || activity.authority() != value.primary_key
    {
        return Err(malformed());
    }
    Ok(value)
}

fn recovery(activity: &Activity) -> Result<DisclosedRecoveryPolicy, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let mut reader = Decoder::new(activity.payload(), 0);
    if reader.u16()? != 0x7103 || reader.u8()? != 0 {
        return Err(malformed());
    }
    let fields = reader.u8()?;
    let did_id = fixed(&mut reader)?;
    let recovery_root = fixed(&mut reader)?;
    let threshold = reader.u16()?;
    let delay_bounds = match fields {
        3 => None,
        5 => Some((reader.u64()?, reader.u64()?)),
        _ => return Err(malformed()),
    };
    reader.finish()?;
    let did = Did::new(activity.actor_did()).map_err(|_| malformed())?;
    if hash::did_id_for_protocol(&did, 3)? != did_id
        || recovery_root == [0; 32]
        || threshold == 0
        || delay_bounds.is_some_and(|(minimum, maximum)| minimum == 0 || maximum < minimum)
    {
        return Err(malformed());
    }
    Ok(DisclosedRecoveryPolicy {
        did_id,
        recovery_root,
        threshold,
        delay_bounds,
    })
}

fn budget(activity: &Activity) -> Result<DisclosedNativeBudgetCreate, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let actor = std::str::from_utf8(activity.actor_did()).map_err(|_| malformed())?;
    let main = account_id(&format!("agent:{actor}:main"))?;
    let mut reader = Decoder::new(activity.payload(), 0);
    let encoding_version = reader.u16()?;
    if !matches!(encoding_version, 1 | 2) {
        return Err(malformed());
    }
    let mut value = DisclosedNativeBudgetCreate {
        encoding_version,
        budget_id: fixed(&mut reader)?,
        budget_account: fixed(&mut reader)?,
        asset: fixed(&mut reader)?,
        purpose: fixed(&mut reader)?,
        per_period_limit: reader.u128()?,
        carry_cap: reader.u128()?,
        initial_amount: reader.u128()?,
        period_length_ms: reader.u64()?,
        period_start_ms: reader.u64()?,
        expiry_ms: reader.u64()?,
        revocation_sequence: reader.u64()?,
        rollover: reader.u8()?,
        source_account: main,
        source_sequence: activity.account_sequence(),
    };
    if encoding_version == 2 {
        value.source_account = fixed(&mut reader)?;
        value.source_sequence = reader.u64()?;
    }
    reader.finish()?;
    let per_asset = account_id(&format!("agent:{actor}:asset:{}", hex(&value.asset)))?;
    let expected_budget = account_id(&format!("agent:{actor}:budget:{}", hex(&value.budget_id)))?;
    if value.budget_id == [0; 32]
        || value.asset == [0; 32]
        || value.budget_account != expected_budget
        || value.source_account == value.budget_account
        || (value.source_account != main && value.source_account != per_asset)
        || value.source_sequence == u64::MAX
        || value.initial_amount == 0
        || value.per_period_limit == 0
        || value.period_length_ms == 0
        || value.expiry_ms <= value.period_start_ms
        || value.expiry_ms <= activity.timestamp_bound().not_before
        || !matches!(value.rollover, 1 | 2)
        || (value.rollover == 1 && value.carry_cap != 0)
    {
        return Err(malformed());
    }
    Ok(value)
}

/// Checks a verified budget context against the identities the actor and the
/// payload budget identifier derive, by the rules core resolves them with.
fn bound_context(
    activity: &Activity,
    context: &BudgetStateContext,
    budget_id: [u8; 32],
) -> Result<(), DisclosureError> {
    if budget_id != context.budget_id {
        return Err(DisclosureError::FieldMismatch("budget_id"));
    }
    let malformed = || DisclosureError::MalformedPayload;
    let actor = std::str::from_utf8(activity.actor_did()).map_err(|_| malformed())?;
    let main = account_id(&format!("agent:{actor}:main"))?;
    let per_asset = account_id(&format!("agent:{actor}:asset:{}", hex(&context.asset)))?;
    let expected_budget = account_id(&format!("agent:{actor}:budget:{}", hex(&budget_id)))?;
    if budget_id == [0; 32]
        || context.owner != main
        || context.budget_account != expected_budget
        || context.asset == [0; 32]
        || context.source_account == [0; 32]
        || context.source_account == context.budget_account
        || (!context.native_source
            && context.source_account != main
            && context.source_account != per_asset)
    {
        return Err(malformed());
    }
    Ok(())
}

fn budget_fund(
    activity: &Activity,
    context: &BudgetStateContext,
) -> Result<DisclosedNativeBudgetFund, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let payload = activity.payload();
    let mut reader = Decoder::new(payload, 0);
    let encoding_version = reader.u16()?;
    if !matches!((payload.len(), encoding_version), (50, 1) | (58, 2)) {
        return Err(malformed());
    }
    let budget_id = fixed(&mut reader)?;
    let amount = reader.u128()?;
    let source_sequence = if encoding_version == 2 {
        Some(reader.u64()?)
    } else {
        None
    };
    reader.finish()?;
    bound_context(activity, context, budget_id)?;
    if amount == 0
        || source_sequence == Some(u64::MAX)
        || context.native_source != (encoding_version == 2)
    {
        return Err(malformed());
    }
    Ok(DisclosedNativeBudgetFund {
        encoding_version,
        budget_id,
        amount,
        source_sequence,
        context: *context,
    })
}

fn budget_defund(
    activity: &Activity,
    context: &BudgetStateContext,
) -> Result<DisclosedNativeBudgetDefund, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let payload = activity.payload();
    let mut reader = Decoder::new(payload, 0);
    if payload.len() != 50 || reader.u16()? != 1 {
        return Err(malformed());
    }
    let budget_id = fixed(&mut reader)?;
    let amount = reader.u128()?;
    reader.finish()?;
    bound_context(activity, context, budget_id)?;
    if amount == 0 || amount > context.balance {
        return Err(malformed());
    }
    Ok(DisclosedNativeBudgetDefund {
        budget_id,
        amount,
        context: *context,
    })
}

fn budget_revoke(
    activity: &Activity,
    context: &BudgetStateContext,
) -> Result<DisclosedNativeBudgetRevoke, DisclosureError> {
    let malformed = || DisclosureError::MalformedPayload;
    let payload = activity.payload();
    let mut reader = Decoder::new(payload, 0);
    if payload.len() != 42 || reader.u16()? != 1 {
        return Err(malformed());
    }
    let budget_id = fixed(&mut reader)?;
    let revocation_sequence = reader.u64()?;
    reader.finish()?;
    bound_context(activity, context, budget_id)?;
    if revocation_sequence == 0 || revocation_sequence <= context.revocation_sequence {
        return Err(malformed());
    }
    Ok(DisclosedNativeBudgetRevoke {
        budget_id,
        revocation_sequence,
        context: *context,
    })
}

fn transfer(
    counterparties: &mut Vec<Counterparty>,
    amounts: &mut Vec<DisclosedAmount>,
    payer: [u8; 32],
    recipient: [u8; 32],
    value: u128,
) {
    counterparties.extend([
        Counterparty {
            role: CounterpartyRole::Payer,
            account: payer,
        },
        Counterparty {
            role: CounterpartyRole::Recipient,
            account: recipient,
        },
    ]);
    amounts.push(DisclosedAmount {
        role: AmountRole::Transfer,
        value,
    });
}

pub(super) fn fields(
    activity: &Activity,
    context: Option<&BudgetStateContext>,
) -> Result<DisclosureFields, DisclosureError> {
    if activity.protocol_version() != 3 || activity.network_id() == 0 {
        return Err(DisclosureError::MalformedPayload);
    }
    let key: [u8; 32] = activity
        .authority()
        .try_into()
        .map_err(|_| DisclosureError::MalformedPayload)?;
    if !crate::ed25519::public_key_is_canonical(&key) {
        return Err(DisclosureError::MalformedPayload);
    }
    let mut counterparties = Vec::new();
    let mut amounts = Vec::new();
    let mut asset = [0; 32];
    let bound = activity.timestamp_bound();
    let mut expiry = bound.not_after;
    let operation = match (
        activity.activity_type().module(),
        activity.activity_type().ordinal(),
    ) {
        (ModuleId::Governance, 1) => {
            DisclosedNativeOperation::IdentityRegistration(identity(activity)?)
        }
        (ModuleId::Governance, 2) => DisclosedNativeOperation::OwnerRotation(Box::new(
            crate::rotation::OwnerRotation::from_activity(activity)
                .map_err(|_| DisclosureError::MalformedPayload)?,
        )),
        (ModuleId::Governance, 3) => DisclosedNativeOperation::RecoveryPolicy(recovery(activity)?),
        (ModuleId::Budget, 1) => {
            let value = budget(activity)?;
            counterparties.extend([
                Counterparty {
                    role: CounterpartyRole::Payer,
                    account: value.source_account,
                },
                Counterparty {
                    role: CounterpartyRole::Recipient,
                    account: value.budget_account,
                },
            ]);
            amounts.extend([
                DisclosedAmount {
                    role: AmountRole::Transfer,
                    value: value.initial_amount,
                },
                DisclosedAmount {
                    role: AmountRole::SpendingLimit,
                    value: value.per_period_limit,
                },
            ]);
            asset = value.asset;
            expiry = value.expiry_ms;
            DisclosedNativeOperation::BudgetCreate(Box::new(value))
        }
        (ModuleId::Budget, ordinal @ (2 | 8 | 9)) => {
            let Some(context) = context else {
                return Err(DisclosureError::UnsupportedActivity(
                    activity.activity_type().value(),
                ));
            };
            asset = context.asset;
            match ordinal {
                2 => {
                    let value = budget_fund(activity, context)?;
                    transfer(
                        &mut counterparties,
                        &mut amounts,
                        context.source_account,
                        context.budget_account,
                        value.amount,
                    );
                    DisclosedNativeOperation::BudgetFund(Box::new(value))
                }
                8 => {
                    let value = budget_defund(activity, context)?;
                    transfer(
                        &mut counterparties,
                        &mut amounts,
                        context.budget_account,
                        context.source_account,
                        value.amount,
                    );
                    DisclosedNativeOperation::BudgetDefund(Box::new(value))
                }
                _ => {
                    let value = budget_revoke(activity, context)?;
                    transfer(
                        &mut counterparties,
                        &mut amounts,
                        context.budget_account,
                        context.source_account,
                        context.balance,
                    );
                    DisclosedNativeOperation::BudgetRevoke(Box::new(value))
                }
            }
        }
        _ => {
            return Err(DisclosureError::UnsupportedActivity(
                activity.activity_type().value(),
            ))
        }
    };
    Ok(DisclosureFields {
        activity_type: activity.activity_type(),
        actor: activity.actor_did().to_vec(),
        authority: activity.authority().to_vec(),
        counterparties,
        amounts,
        asset,
        fee_limit: activity.fee_limit(),
        expiry: Expiry {
            not_before: bound.not_before,
            not_after: bound.not_after,
            payload_expires_at: expiry,
        },
        idempotency_key: activity.idempotency_key(),
        evm_payout_binding: None,
        withdrawal: None,
        payment: None,
        authority_grant: None,
        session_grant: None,
        onboarding: None,
        native_operation: Some(operation),
    })
}
