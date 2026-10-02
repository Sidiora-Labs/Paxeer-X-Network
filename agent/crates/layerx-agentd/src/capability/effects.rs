use std::collections::{BTreeMap, BTreeSet};

use layerx_crypto::disclosure::{DisclosedNativeOperation, Disclosure};
use layerx_crypto::payments::{Grant, Payment};

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum AuthorizationKind {
    SpendingLimit,
    SupplyCap,
    PerDrawMaximum,
    GrantAllowance,
    ProgramTransferMaximum,
    ProgramSpendMaximum,
    ProgramExitRoute,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Effect {
    Transfer {
        from: [u8; 32],
        to: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    },
    Issuance {
        account: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    },
    Destruction {
        account: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    },
    Authorization {
        kind: AuthorizationKind,
        account: [u8; 32],
        asset: Option<[u8; 32]>,
        amount: u128,
    },
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct VerifiedInputs {
    pub revoke_balance: Option<u128>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EffectsError {
    Unsupported(&'static str),
    MissingRevokeBalance,
    UnexpectedRevokeBalance,
    RevokeBalanceMismatch,
    Overflow,
    InvalidProgramCapabilities,
    InvalidProgramDisclosure,
    ProgramExitContextRequired,
    UnboundedProgramExit,
    UnboundedLegacyProgramCall,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ProgramValueSource {
    Principal,
    Program {
        owner_program: [u8; 32],
        seed: Vec<u8>,
        source_account: [u8; 32],
    },
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProgramSpendBound {
    pub source: ProgramValueSource,
    pub asset: [u8; 32],
    pub destination: [u8; 32],
    pub maximum_amount: u128,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SemanticPlan {
    effects: Vec<Effect>,
    gross_per_asset: BTreeMap<[u8; 32], u128>,
    participants: BTreeSet<[u8; 32]>,
    rate_actions: u32,
    program_spend_bounds: Vec<ProgramSpendBound>,
}

impl SemanticPlan {
    #[must_use]
    pub fn program_spend_bounds(&self) -> &[ProgramSpendBound] {
        &self.program_spend_bounds
    }

    #[must_use]
    pub fn effects(&self) -> &[Effect] {
        &self.effects
    }

    #[must_use]
    pub const fn gross_per_asset(&self) -> &BTreeMap<[u8; 32], u128> {
        &self.gross_per_asset
    }

    #[must_use]
    pub const fn participants(&self) -> &BTreeSet<[u8; 32]> {
        &self.participants
    }

    #[must_use]
    pub const fn rate_actions(&self) -> u32 {
        self.rate_actions
    }

    fn write() -> Self {
        Self {
            effects: Vec::new(),
            gross_per_asset: BTreeMap::new(),
            participants: BTreeSet::new(),
            rate_actions: 1,
            program_spend_bounds: Vec::new(),
        }
    }

    fn gross(&mut self, asset: [u8; 32], amount: u128) -> Result<(), EffectsError> {
        let total = self.gross_per_asset.entry(asset).or_insert(0);
        *total = total.checked_add(amount).ok_or(EffectsError::Overflow)?;
        Ok(())
    }

    fn transfer(
        &mut self,
        from: [u8; 32],
        to: [u8; 32],
        asset: [u8; 32],
        amount: u128,
    ) -> Result<(), EffectsError> {
        self.gross(asset, amount)?;
        self.participants.insert(from);
        self.participants.insert(to);
        self.effects.push(Effect::Transfer {
            from,
            to,
            asset,
            amount,
        });
        Ok(())
    }

    fn authorize(
        &mut self,
        kind: AuthorizationKind,
        account: [u8; 32],
        asset: Option<[u8; 32]>,
        amount: u128,
    ) {
        self.participants.insert(account);
        self.effects.push(Effect::Authorization {
            kind,
            account,
            asset,
            amount,
        });
    }

    fn grant_limits(&mut self, grant: &Grant) {
        self.authorize(
            AuthorizationKind::PerDrawMaximum,
            grant.from,
            Some(grant.asset),
            grant.per_draw_maximum,
        );
        self.authorize(
            AuthorizationKind::GrantAllowance,
            grant.from,
            Some(grant.asset),
            grant.allowance,
        );
    }
}

/// # Errors
/// Returns `Unsupported` for an activity without a mappable typed payload, a revoke-balance refusal, or `Overflow`.
pub fn derive(
    disclosure: &Disclosure,
    verified: &VerifiedInputs,
) -> Result<SemanticPlan, EffectsError> {
    if matches!(disclosure.native_operation,
        Some(DisclosedNativeOperation::ProgramDeploy(_)
            | DisclosedNativeOperation::ProgramUpgrade(_)
            | DisclosedNativeOperation::ProgramCall(_)
            | DisclosedNativeOperation::ProgramWindDown(_)
            | DisclosedNativeOperation::LegacyProgramCall(_))) {
        disclosure.reencode().map_err(|_| EffectsError::InvalidProgramDisclosure)?;
    }
    if disclosure.payment.is_none() && disclosure.native_operation.is_none() {
        return Err(EffectsError::Unsupported(untyped_kind(disclosure)));
    }
    plan(
        disclosure.payment.as_ref(),
        disclosure.native_operation.as_ref(),
        verified,
    )
}

const fn untyped_kind(disclosure: &Disclosure) -> &'static str {
    if disclosure.withdrawal.is_some() {
        "Withdrawal"
    } else if disclosure.onboarding.is_some() {
        "Onboarding"
    } else if disclosure.authority_grant.is_some() {
        "AuthorityGrant"
    } else if disclosure.session_grant.is_some() {
        "SessionGrant"
    } else if disclosure.evm_payout_binding.is_some() {
        "EvmPayoutBinding"
    } else {
        "Send"
    }
}

fn plan(
    payment: Option<&Payment>,
    native: Option<&DisclosedNativeOperation>,
    verified: &VerifiedInputs,
) -> Result<SemanticPlan, EffectsError> {
    let revoke = matches!(native, Some(DisclosedNativeOperation::BudgetRevoke(_)));
    if !revoke && verified.revoke_balance.is_some() {
        return Err(EffectsError::UnexpectedRevokeBalance);
    }
    let mut plan = SemanticPlan::write();
    if let Some(payment) = payment {
        payment_effects(&mut plan, payment)?;
    } else if let Some(native) = native {
        native_effects(&mut plan, native, verified)?;
    } else {
        return Err(EffectsError::Unsupported("Send"));
    }
    Ok(plan)
}

fn payment_effects(plan: &mut SemanticPlan, payment: &Payment) -> Result<(), EffectsError> {
    match payment {
        Payment::Register(_) => return Err(EffectsError::Unsupported("Register")),
        Payment::OpenAccount { .. }
        | Payment::Pause { .. }
        | Payment::Unpause { .. }
        | Payment::RevokeGrant { .. }
        | Payment::ProgramAccount { .. } => {}
        Payment::Receive {
            from,
            to,
            asset,
            amount,
            payer_grant,
            ..
        } => {
            plan.transfer(*from, *to, *asset, *amount)?;
            plan.grant_limits(payer_grant);
        }
        Payment::IssueGrant(grant) => {
            plan.grant_limits(grant);
            plan.participants.insert(grant.recipient);
        }
        Payment::Mint { asset, to, amount } => {
            plan.gross(*asset, *amount)?;
            plan.participants.insert(*to);
            plan.effects.push(Effect::Issuance {
                account: *to,
                asset: *asset,
                amount: *amount,
            });
        }
        Payment::Burn {
            asset,
            from,
            amount,
        } => {
            plan.gross(*asset, *amount)?;
            plan.participants.insert(*from);
            plan.effects.push(Effect::Destruction {
                account: *from,
                asset: *asset,
                amount: *amount,
            });
        }
        Payment::ProgramTransfer { legs, .. } => {
            for leg in legs {
                plan.transfer(leg.from, leg.to, leg.asset, leg.amount)?;
            }
        }
    }
    Ok(())
}

fn native_effects(
    plan: &mut SemanticPlan,
    native: &DisclosedNativeOperation,
    verified: &VerifiedInputs,
) -> Result<(), EffectsError> {
    match native {
        DisclosedNativeOperation::ProgramDeploy(_)
        | DisclosedNativeOperation::ProgramUpgrade(_) => {}
        DisclosedNativeOperation::ProgramCall(call) => program_call_effects(plan, call)?,
        DisclosedNativeOperation::LegacyProgramCall(call) => {
            if call.capabilities().as_slice().iter().any(|capability| matches!(capability,
                layerx_types::intent::CapabilityRequest::Transfer
                | layerx_types::intent::CapabilityRequest::Compose)) {
                return Err(EffectsError::UnboundedLegacyProgramCall);
            }
        }
        DisclosedNativeOperation::ProgramWindDown(value) => {
            use layerx_crypto::disclosure::DisclosedProgramWindDownOperation;
            match &value.operation {
                DisclosedProgramWindDownOperation::Route { account, asset, destination, seed } => {
                    let program = layerx_programs_runtime::ProgramId::new(value.program_id.bytes())
                        .map_err(|_| EffectsError::InvalidProgramDisclosure)?;
                    if !layerx_programs_runtime::accounts::derive_program_account(program, seed)
                        .is_ok_and(|derived| derived.matches(account)) {
                        return Err(EffectsError::InvalidProgramDisclosure);
                    }
                    plan.participants.insert(*account);
                    plan.authorize(AuthorizationKind::ProgramExitRoute, *destination, Some(*asset), 0);
                }
                DisclosedProgramWindDownOperation::Deprecate { .. }
                | DisclosedProgramWindDownOperation::Tombstone => {}
                DisclosedProgramWindDownOperation::Exit { .. } =>
                    return Err(EffectsError::UnboundedProgramExit),
                DisclosedProgramWindDownOperation::BoundedExit { .. } =>
                    return Err(EffectsError::ProgramExitContextRequired),
            }
        }
        DisclosedNativeOperation::IdentityRegistration(_)
        | DisclosedNativeOperation::RecoveryPolicy(_)
        | DisclosedNativeOperation::OwnerRotation(_) => {}
        DisclosedNativeOperation::BudgetCreate(budget) => {
            plan.transfer(
                budget.source_account,
                budget.budget_account,
                budget.asset,
                budget.initial_amount,
            )?;
            plan.authorize(
                AuthorizationKind::SpendingLimit,
                budget.budget_account,
                Some(budget.asset),
                budget.per_period_limit,
            );
        }
        DisclosedNativeOperation::BudgetFund(fund) => plan.transfer(
            fund.context.source_account,
            fund.context.budget_account,
            fund.context.asset,
            fund.amount,
        )?,
        DisclosedNativeOperation::BudgetDefund(defund) => plan.transfer(
            defund.context.budget_account,
            defund.context.source_account,
            defund.context.asset,
            defund.amount,
        )?,
        DisclosedNativeOperation::BudgetRevoke(revoke) => {
            let balance = verified
                .revoke_balance
                .ok_or(EffectsError::MissingRevokeBalance)?;
            if balance != revoke.context.balance {
                return Err(EffectsError::RevokeBalanceMismatch);
            }
            plan.transfer(
                revoke.context.budget_account,
                revoke.context.source_account,
                revoke.context.asset,
                balance,
            )?;
        }
    }
    Ok(())
}

fn program_call_effects(
    plan: &mut SemanticPlan,
    call: &layerx_crypto::disclosure::DisclosedProgramCall,
) -> Result<(), EffectsError> {
    use layerx_programs_runtime::abi::{Capability, CapabilitySet};
    use layerx_programs_runtime::abi_policy::{capability_encoding, CapabilityEncoding};
    let grants = match capability_encoding(call.guest_abi)
        .map_err(|_| EffectsError::InvalidProgramCapabilities)? {
        CapabilityEncoding::V1 => CapabilitySet::decode_canonical(&call.capabilities),
        CapabilityEncoding::V2 => CapabilitySet::decode_v2_canonical(&call.capabilities),
    }.map_err(|_| EffectsError::InvalidProgramCapabilities)?;
    for grant in grants {
        let bound = match grant {
            Capability::Transfer402 { asset, to, maximum_amount } => {
                plan.authorize(AuthorizationKind::ProgramTransferMaximum, to, Some(asset), maximum_amount);
                ProgramSpendBound {
                    source: ProgramValueSource::Principal, asset, destination: to, maximum_amount,
                }
            }
            Capability::ProgramSpend { owner_program, seed, source_account, asset, to, maximum_amount } => {
                plan.participants.insert(source_account);
                plan.authorize(AuthorizationKind::ProgramSpendMaximum, to, Some(asset), maximum_amount);
                ProgramSpendBound {
                    source: ProgramValueSource::Program {
                        owner_program: owner_program.bytes(), seed, source_account,
                    },
                    asset, destination: to, maximum_amount,
                }
            }
            Capability::StorageRead | Capability::StorageWrite
            | Capability::SharedStorageRead | Capability::SharedStorageWrite
            | Capability::EmitEvent | Capability::Call { .. }
            | Capability::ReceiptRead { .. } | Capability::BalanceView { .. } => continue,
        };
        plan.gross(bound.asset, bound.maximum_amount)?;
        plan.program_spend_bounds.push(bound);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use layerx_crypto::disclosure::{
        BudgetStateContext, DisclosedNativeBudgetCreate, DisclosedNativeBudgetRevoke,
    };
    use layerx_crypto::payments::{ReceiverAuthorization, TransferLeg};

    use super::*;

    const PAYER: [u8; 32] = [1; 32];
    const PAYEE: [u8; 32] = [2; 32];
    const ASSET: [u8; 32] = [3; 32];
    const OTHER_ASSET: [u8; 32] = [4; 32];
    const BUDGET: [u8; 32] = [5; 32];

    fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        value.unwrap_or_else(|error| panic!("effects: {error:?}"))
    }

    fn none() -> VerifiedInputs {
        VerifiedInputs::default()
    }

    fn grant() -> Grant {
        Grant {
            id: [9; 32],
            from: PAYER,
            recipient: PAYEE,
            asset: ASSET,
            per_draw_maximum: 50,
            allowance: 500,
            recurring: false,
            window_length: 0,
            expiration: 1_000,
            purpose_hash: [10; 32],
            has_reference: false,
            reference_hash: [0; 32],
            revocation_sequence: 0,
            public_key: [11; 32],
            signature: [12; 64],
        }
    }

    fn context(balance: u128) -> BudgetStateContext {
        BudgetStateContext {
            budget_id: [13; 32],
            owner: [14; 32],
            budget_account: BUDGET,
            asset: ASSET,
            source_account: PAYER,
            native_source: true,
            revocation_sequence: 1,
            balance,
            state_digest: [15; 32],
            observed_head_sequence: 7,
        }
    }

    fn revoke(balance: u128) -> DisclosedNativeOperation {
        DisclosedNativeOperation::BudgetRevoke(Box::new(DisclosedNativeBudgetRevoke {
            budget_id: [13; 32],
            revocation_sequence: 1,
            context: context(balance),
        }))
    }

    #[test]
    fn receive_is_one_transfer_and_two_grant_authorizations() {
        let payment = Payment::Receive {
            from: PAYER,
            to: PAYEE,
            asset: ASSET,
            amount: 40,
            grant: [9; 32],
            sequence: 3,
            idempotency_key: [16; 32],
            context_hash: [17; 32],
            receiver_authorization: ReceiverAuthorization {
                kind: 1,
                controller: PAYEE,
                public_key: [18; 32],
                signature: [19; 64],
                signed_context_hash: [17; 32],
                network_id: 1,
                protocol_version: 3,
            },
            payer_grant: Box::new(grant()),
        };
        let plan = must(plan(Some(&payment), None, &none()));
        assert_eq!(
            plan.effects(),
            &[
                Effect::Transfer {
                    from: PAYER,
                    to: PAYEE,
                    asset: ASSET,
                    amount: 40,
                },
                Effect::Authorization {
                    kind: AuthorizationKind::PerDrawMaximum,
                    account: PAYER,
                    asset: Some(ASSET),
                    amount: 50,
                },
                Effect::Authorization {
                    kind: AuthorizationKind::GrantAllowance,
                    account: PAYER,
                    asset: Some(ASSET),
                    amount: 500,
                },
            ]
        );
        assert_eq!(plan.gross_per_asset(), &BTreeMap::from([(ASSET, 40)]));
        assert_eq!(plan.participants(), &BTreeSet::from([PAYER, PAYEE]));
        assert_eq!(plan.rate_actions(), 1);
    }

    #[test]
    fn program_transfer_counts_each_leg_with_its_own_asset() {
        let payment = Payment::ProgramTransfer {
            program: [20; 32],
            legs: vec![
                TransferLeg {
                    from: PAYER,
                    asset: ASSET,
                    to: PAYEE,
                    amount: 7,
                },
                TransferLeg {
                    from: PAYEE,
                    asset: OTHER_ASSET,
                    to: PAYER,
                    amount: 9,
                },
            ],
        };
        let plan = must(plan(Some(&payment), None, &none()));
        assert_eq!(plan.effects().len(), 2);
        assert_eq!(
            plan.gross_per_asset(),
            &BTreeMap::from([(ASSET, 7), (OTHER_ASSET, 9)])
        );
        assert!(!plan.gross_per_asset().contains_key(&[0; 32]));
        assert_eq!(plan.participants(), &BTreeSet::from([PAYER, PAYEE]));
        assert_eq!(plan.rate_actions(), 1);
    }

    #[test]
    fn budget_create_funds_the_budget_and_sets_its_spending_limit() {
        let create =
            DisclosedNativeOperation::BudgetCreate(Box::new(DisclosedNativeBudgetCreate {
                encoding_version: 1,
                budget_id: [13; 32],
                budget_account: BUDGET,
                asset: ASSET,
                purpose: [21; 32],
                per_period_limit: 30,
                carry_cap: 0,
                initial_amount: 100,
                period_length_ms: 60_000,
                period_start_ms: 0,
                expiry_ms: 600_000,
                revocation_sequence: 0,
                rollover: 0,
                source_account: PAYER,
                source_sequence: 4,
            }));
        let plan = must(plan(None, Some(&create), &none()));
        assert_eq!(
            plan.effects(),
            &[
                Effect::Transfer {
                    from: PAYER,
                    to: BUDGET,
                    asset: ASSET,
                    amount: 100,
                },
                Effect::Authorization {
                    kind: AuthorizationKind::SpendingLimit,
                    account: BUDGET,
                    asset: Some(ASSET),
                    amount: 30,
                },
            ]
        );
        assert_eq!(plan.gross_per_asset(), &BTreeMap::from([(ASSET, 100)]));
        assert_eq!(plan.rate_actions(), 1);
    }

    #[test]
    fn revoke_requires_the_verified_balance() {
        assert_eq!(
            plan(None, Some(&revoke(80)), &none()),
            Err(EffectsError::MissingRevokeBalance)
        );
        let verified = VerifiedInputs {
            revoke_balance: Some(80),
        };
        let plan_ok = must(plan(None, Some(&revoke(80)), &verified));
        assert_eq!(
            plan_ok.effects(),
            &[Effect::Transfer {
                from: BUDGET,
                to: PAYER,
                asset: ASSET,
                amount: 80,
            }]
        );
        assert_eq!(
            plan(Some(&Payment::Pause { asset: ASSET }), None, &verified),
            Err(EffectsError::UnexpectedRevokeBalance)
        );
    }

    #[test]
    fn mint_names_no_opposite_account() {
        let payment = Payment::Mint {
            asset: ASSET,
            to: PAYEE,
            amount: 25,
        };
        let plan = must(plan(Some(&payment), None, &none()));
        assert_eq!(
            plan.effects(),
            &[Effect::Issuance {
                account: PAYEE,
                asset: ASSET,
                amount: 25,
            }]
        );
        assert_eq!(plan.participants(), &BTreeSet::from([PAYEE]));
        assert_eq!(plan.gross_per_asset(), &BTreeMap::from([(ASSET, 25)]));
    }

    #[test]
    fn gross_overflow_is_refused() {
        let payment = Payment::ProgramTransfer {
            program: [20; 32],
            legs: vec![
                TransferLeg {
                    from: PAYER,
                    asset: ASSET,
                    to: PAYEE,
                    amount: u128::MAX,
                },
                TransferLeg {
                    from: PAYEE,
                    asset: ASSET,
                    to: PAYER,
                    amount: 1,
                },
            ],
        };
        assert_eq!(
            plan(Some(&payment), None, &none()),
            Err(EffectsError::Overflow)
        );
    }

    #[test]
    fn register_is_unsupported_not_empty() {
        let payment = Payment::Register(layerx_crypto::payments::Registration {
            asset: ASSET,
            salt: [22; 32],
            symbol: "LX".to_owned(),
            name: "Layer".to_owned(),
            decimals: 6,
            supply_cap: 1_000,
            issuer_kind: 1,
            custody_ref: Vec::new(),
        });
        assert_eq!(
            plan(Some(&payment), None, &none()),
            Err(EffectsError::Unsupported("Register"))
        );
    }
}
