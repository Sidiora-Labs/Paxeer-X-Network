//! Digest-bound approval disclosure rendering.

use std::fmt::{Display, Formatter, Write as _};

use layerx_agent_api::verify::Level;
use layerx_crypto::disclosure::{
    AmountRole, CounterpartyRole, DisclosedEvmPayoutBinding, Disclosure, DisclosureError,
};
use layerx_types::payload::{ActivityType, ModuleId};
use sha2::{Digest as _, Sha256};

use super::VerifiedBudgetAfter;

const MOVE_MONEY_KEY: &str = "approval.activity.move_money";
const ADD_MONEY_KEY: &str = "approval.activity.add_money";
const WITHDRAWAL_WALLET_KEY: &str = "approval.activity.withdrawal_wallet";
const UNRENDERABLE_KEY: &str = "approval.activity.unrenderable";
const MOVE_MONEY_TEMPLATE: &str =
    "Move {amount} {asset} to {counterparty}. Fees can be up to {fee}.";
const ADD_MONEY_TEMPLATE: &str = "Add {amount} {asset} to {counterparty}. Fees can be up to {fee}.";
const WITHDRAWAL_WALLET_TEMPLATE: &str =
    "Use {counterparty} for withdrawals. Fees can be up to {fee}.";
const UNRENDERABLE_COPY: &str = "This request cannot be reviewed here.";

/// V1 activity classes for which the agent disclosure authority can currently
/// produce a complete, byte-bound human disclosure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ApprovalActivityClass {
    MoveMoney,
    AddMoney,
    WithdrawalWallet,
}

impl ApprovalActivityClass {
    pub const ALL: [Self; 3] = [Self::MoveMoney, Self::AddMoney, Self::WithdrawalWallet];

    const fn from_activity_type(activity_type: ActivityType) -> Option<Self> {
        match (activity_type.module(), activity_type.ordinal()) {
            (ModuleId::Asset, 5) => Some(Self::MoveMoney),
            (ModuleId::Bridge, 1) => Some(Self::AddMoney),
            (ModuleId::Governance, 4) => Some(Self::WithdrawalWallet),
            _ => None,
        }
    }

    #[must_use]
    pub const fn copy_key(self) -> &'static str {
        match self {
            Self::MoveMoney => MOVE_MONEY_KEY,
            Self::AddMoney => ADD_MONEY_KEY,
            Self::WithdrawalWallet => WITHDRAWAL_WALLET_KEY,
        }
    }

    #[must_use]
    pub const fn copy_template(self) -> &'static str {
        match self {
            Self::MoveMoney => MOVE_MONEY_TEMPLATE,
            Self::AddMoney => ADD_MONEY_TEMPLATE,
            Self::WithdrawalWallet => WITHDRAWAL_WALLET_TEMPLATE,
        }
    }
}

/// Exact counterparty representation taken from the validated disclosure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RenderedCounterparty {
    Account([u8; 32]),
    EvmAddress([u8; 20]),
}

impl RenderedCounterparty {
    fn plain(self) -> String {
        match self {
            Self::Account(account) => hex(&account, false),
            Self::EvmAddress(address) => hex(&address, true),
        }
    }
}

/// Approval facts copied only from the disclosure that passed re-encoding.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RenderedApprovalFacts {
    pub amount: Option<u128>,
    pub counterparty: RenderedCounterparty,
    pub asset: Option<[u8; 32]>,
    pub fee_limit: u128,
    pub expires_at: u64,
}

/// Plain approval content plus separately-labelled verified budget context.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RenderedApproval {
    pub class: ApprovalActivityClass,
    pub copy_key: &'static str,
    pub plain_copy: String,
    pub facts: RenderedApprovalFacts,
    pub budget_after: VerifiedBudgetAfter,
    pub disclosure_digest: [u8; 32],
}

/// Safe presentation for an activity class this plane does not understand.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct UnrenderableApproval {
    pub activity_type: u32,
    pub copy_key: &'static str,
    pub plain_copy: &'static str,
}

/// Total renderer outcome. Only a validated known class is approvable.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ApprovalPresentation {
    Rendered(Box<RenderedApproval>),
    Unrenderable(UnrenderableApproval),
}

impl ApprovalPresentation {
    #[must_use]
    pub const fn can_approve(&self) -> bool {
        matches!(self, Self::Rendered(_))
    }
}

/// Stateless digest and disclosure gate for approval content.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct DisclosureRenderer;

impl DisclosureRenderer {
    /// Re-encodes and digest-checks a known disclosure before deriving any
    /// approvable content. Unknown classes return safe plain copy without an
    /// approve capability.
    ///
    /// # Errors
    ///
    /// Returns a typed defect for changed disclosure fields, a mismatched held
    /// digest, malformed class facts, or budget context without verified agent
    /// evidence.
    pub fn render(
        disclosure: &Disclosure,
        held_digest: [u8; 32],
        budget_after: VerifiedBudgetAfter,
    ) -> Result<ApprovalPresentation, DisclosureRenderError> {
        let Some(class) = ApprovalActivityClass::from_activity_type(disclosure.activity_type)
        else {
            return Ok(ApprovalPresentation::Unrenderable(UnrenderableApproval {
                activity_type: disclosure.activity_type.value(),
                copy_key: UNRENDERABLE_KEY,
                plain_copy: UNRENDERABLE_COPY,
            }));
        };

        let canonical = disclosure
            .reencode()
            .map_err(DisclosureRenderError::DefectiveDisclosure)?;
        let observed_digest: [u8; 32] = Sha256::digest(&canonical).into();
        if observed_digest != held_digest {
            return Err(DisclosureRenderError::DigestMismatch);
        }
        if budget_after.level == Level::Unverified || budget_after.evidence_digest == [0; 32] {
            return Err(DisclosureRenderError::UnverifiedBudget);
        }

        let facts = facts(class, disclosure)?;
        let plain_copy = plain_copy(class, facts);
        Ok(ApprovalPresentation::Rendered(Box::new(RenderedApproval {
            class,
            copy_key: class.copy_key(),
            plain_copy,
            facts,
            budget_after,
            disclosure_digest: held_digest,
        })))
    }
}

fn facts(
    class: ApprovalActivityClass,
    disclosure: &Disclosure,
) -> Result<RenderedApprovalFacts, DisclosureRenderError> {
    match class {
        ApprovalActivityClass::MoveMoney | ApprovalActivityClass::AddMoney => {
            let [payer, recipient] = disclosure.counterparties.as_slice() else {
                return Err(DisclosureRenderError::MalformedFacts);
            };
            let [amount] = disclosure.amounts.as_slice() else {
                return Err(DisclosureRenderError::MalformedFacts);
            };
            if payer.role != CounterpartyRole::Payer
                || recipient.role != CounterpartyRole::Recipient
                || amount.role != AmountRole::Transfer
                || amount.value == 0
            {
                return Err(DisclosureRenderError::MalformedFacts);
            }
            Ok(RenderedApprovalFacts {
                amount: Some(amount.value),
                counterparty: RenderedCounterparty::Account(recipient.account),
                asset: Some(disclosure.asset),
                fee_limit: disclosure.fee_limit,
                expires_at: disclosure.expiry.payload_expires_at,
            })
        }
        ApprovalActivityClass::WithdrawalWallet => {
            let Some(binding) = disclosure.evm_payout_binding else {
                return Err(DisclosureRenderError::MalformedFacts);
            };
            wallet_facts(disclosure, binding)
        }
    }
}

fn wallet_facts(
    disclosure: &Disclosure,
    binding: DisclosedEvmPayoutBinding,
) -> Result<RenderedApprovalFacts, DisclosureRenderError> {
    if !disclosure.counterparties.is_empty()
        || !disclosure.amounts.is_empty()
        || disclosure.asset != [0; 32]
    {
        return Err(DisclosureRenderError::MalformedFacts);
    }
    Ok(RenderedApprovalFacts {
        amount: None,
        counterparty: RenderedCounterparty::EvmAddress(binding.payout_address),
        asset: None,
        fee_limit: disclosure.fee_limit,
        expires_at: disclosure.expiry.payload_expires_at,
    })
}

fn plain_copy(class: ApprovalActivityClass, facts: RenderedApprovalFacts) -> String {
    let counterparty = facts.counterparty.plain();
    let amount = facts
        .amount
        .map_or_else(String::new, |value| value.to_string());
    let asset = facts
        .asset
        .map_or_else(String::new, |value| hex(&value, false));
    class
        .copy_template()
        .replace("{amount}", &amount)
        .replace("{asset}", &asset)
        .replace("{counterparty}", &counterparty)
        .replace("{fee}", &facts.fee_limit.to_string())
}

fn hex(bytes: &[u8], prefix: bool) -> String {
    let mut output = String::with_capacity(bytes.len().saturating_mul(2) + usize::from(prefix) * 2);
    if prefix {
        output.push_str("0x");
    }
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

/// Typed refusal before approval content becomes approvable.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DisclosureRenderError {
    DefectiveDisclosure(DisclosureError),
    DigestMismatch,
    UnverifiedBudget,
    MalformedFacts,
}

impl Display for DisclosureRenderError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::DefectiveDisclosure(error) => {
                write!(formatter, "held disclosure is defective: {error}")
            }
            Self::DigestMismatch => {
                formatter.write_str("held disclosure does not match its digest")
            }
            Self::UnverifiedBudget => {
                formatter.write_str("budget context is not an agent-verified read")
            }
            Self::MalformedFacts => {
                formatter.write_str("held disclosure cannot supply its approval facts")
            }
        }
    }
}

impl std::error::Error for DisclosureRenderError {}
