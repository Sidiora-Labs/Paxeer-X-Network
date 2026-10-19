//! Canonical core budget fund and revoke identity and verified confirmation.

use layerx_crypto::disclosure::{self, BudgetStateContext, DisclosedNativeOperation};
use layerx_types::payload::ModuleRegistry;
use sha2::{Digest as _, Sha256};

use super::accounting::{ProtocolBudgetRecord, ProtocolBudgetState};
use super::create::{BudgetCreationError, CoreBudgetReceipt};
use crate::protocol_evidence::EvidenceAuthority;
use crate::sign::VerifiedSubmission;

const STATE_DIGEST_DOMAIN: &[u8] = b"layerx:budget-state-context:v1\0";

/// One decoded core budget mutation (fund 0x00030002 or revoke 0x00030009).
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BudgetMutation {
    Fund {
        budget_id: [u8; 32],
        amount: u128,
        encoding_version: u16,
        source_sequence: Option<u64>,
    },
    Revoke {
        budget_id: [u8; 32],
        revocation_sequence: u64,
    },
}

impl BudgetMutation {
    #[must_use]
    pub const fn budget_id(&self) -> [u8; 32] {
        match self {
            Self::Fund { budget_id, .. } | Self::Revoke { budget_id, .. } => *budget_id,
        }
    }
}

/// Builds the disclosure context from a verified, decoded core budget record.
///
/// `verified_state` must be the canonical state bytes the evidence authority verified for
/// `record`, and `balance` the verified budget-account balance. A version-1 budget funds from
/// the owner's main account, the source the native create disclosure binds for version 1.
///
/// # Errors
///
/// Returns `ContextMismatch` when the state bytes do not decode to `record`, and
/// `BudgetNotLive` for a closed record.
pub fn budget_state_context(
    record: &ProtocolBudgetRecord,
    verified_state: &[u8],
    observed_head_sequence: u64,
    balance: u128,
) -> Result<BudgetStateContext, BudgetCreationError> {
    let decoded = ProtocolBudgetRecord::decode(verified_state)
        .map_err(|_| BudgetCreationError::ContextMismatch)?;
    if &decoded != record {
        return Err(BudgetCreationError::ContextMismatch);
    }
    if record.closed {
        return Err(BudgetCreationError::BudgetNotLive);
    }
    let state_digest: [u8; 32] = Sha256::new()
        .chain_update(STATE_DIGEST_DOMAIN)
        .chain_update(verified_state)
        .finalize()
        .into();
    Ok(BudgetStateContext {
        budget_id: record.budget_id,
        owner: record.owner,
        budget_account: record.budget_account,
        asset: record.asset_id,
        source_account: record.source_account.unwrap_or(record.owner),
        native_source: record.source_account.is_some(),
        revocation_sequence: record.revocation_sequence,
        balance,
        state_digest,
        observed_head_sequence,
        purpose_hash: record.purpose_hash,
    })
}

/// Decodes a signed canonical budget fund or revoke activity through the shared
/// context-bound native disclosure.
///
/// # Errors
///
/// Returns `NotBudgetCreation` when the bytes are not a canonical signed activity, and
/// `ContextMismatch` when the payload is not a fund or revoke consistent with `context`.
pub fn budget_mutation_identity(
    canonical_activity: &[u8],
    registry: &ModuleRegistry,
    context: &BudgetStateContext,
) -> Result<BudgetMutation, BudgetCreationError> {
    let activity = layerx_wire::activity::decode_signed(canonical_activity, registry)
        .map_err(|_| BudgetCreationError::NotBudgetCreation)?;
    let unsigned = layerx_wire::activity::encode_unsigned(&activity)
        .map_err(|_| BudgetCreationError::NotBudgetCreation)?;
    let bound = disclosure::bind_budget_mutation(&unsigned, registry, context)
        .map_err(|_| BudgetCreationError::ContextMismatch)?;
    match bound.native_operation {
        Some(DisclosedNativeOperation::BudgetFund(fund)) => Ok(BudgetMutation::Fund {
            budget_id: fund.budget_id,
            amount: fund.amount,
            encoding_version: fund.encoding_version,
            source_sequence: fund.source_sequence,
        }),
        Some(DisclosedNativeOperation::BudgetRevoke(revoke)) => {
            if revoke.revocation_sequence <= context.revocation_sequence {
                return Err(BudgetCreationError::StaleRevocation);
            }
            Ok(BudgetMutation::Revoke {
                budget_id: revoke.budget_id,
                revocation_sequence: revoke.revocation_sequence,
            })
        }
        _ => Err(BudgetCreationError::ContextMismatch),
    }
}

/// Exact verified-submission transport and proven-state read for budget mutations.
pub trait BudgetMutationPipeline {
    /// Submits the byte-identical verifier-bound activity and returns raw core evidence.
    ///
    /// # Errors
    ///
    /// Returns `Submission` when the exact signed activity does not reach core and produce a
    /// receipt; the outcome is then unknown and must be reconciled under the same key.
    fn submit_verified(
        &mut self,
        submission: &VerifiedSubmission,
    ) -> Result<CoreBudgetReceipt, BudgetCreationError>;

    /// Reads the proven budget module state stored under `budget_state_key(budget_id)`.
    ///
    /// # Errors
    ///
    /// Returns `CreatedBudgetUnconfirmed` when the node serves no proven state for the key.
    fn budget_state(
        &mut self,
        budget_id: [u8; 32],
    ) -> Result<ProtocolBudgetState, BudgetCreationError>;

    /// Reads the state-proven balance of `account` in `asset` (the verified client balance
    /// read); its amount is the `balance` passed to `budget_state_context`.
    ///
    /// # Errors
    ///
    /// Returns `CreatedBudgetUnconfirmed` when no state-proven balance is served.
    fn budget_balance(
        &mut self,
        account: [u8; 32],
        asset: [u8; 32],
    ) -> Result<u128, BudgetCreationError>;
}

/// A budget mutation confirmed by its verified receipt and proven resulting state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ConfirmedBudgetMutation {
    mutation: BudgetMutation,
    record: ProtocolBudgetRecord,
    activity_id: [u8; 32],
    receipt_bytes: Vec<u8>,
    observed_head_sequence: u64,
}

impl ConfirmedBudgetMutation {
    #[must_use]
    pub const fn mutation(&self) -> &BudgetMutation {
        &self.mutation
    }

    #[must_use]
    pub const fn record(&self) -> &ProtocolBudgetRecord {
        &self.record
    }

    #[must_use]
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }

    #[must_use]
    pub fn receipt_bytes(&self) -> &[u8] {
        &self.receipt_bytes
    }

    #[must_use]
    pub const fn observed_head_sequence(&self) -> u64 {
        self.observed_head_sequence
    }
}

/// Submits one verified fund or revoke activity and confirms it from proven core state.
///
/// # Errors
///
/// Refuses an activity whose idempotency key is not the mutation key, a payload that does not
/// bind to `context`, an unverified or substituted receipt, a core rejection, and a resulting
/// state that is not the expected effect: a fund must leave the same live budget identity, a
/// revoke must leave the budget revoked at the signed counter with its limit set to the amount
/// already spent. `Submission` means the outcome is unknown and nothing may be released.
pub fn confirm_budget_mutation(
    pipeline: &mut dyn BudgetMutationPipeline,
    verifier: &EvidenceAuthority,
    submission: &VerifiedSubmission,
    registry: &ModuleRegistry,
    context: &BudgetStateContext,
    mutation_key: [u8; 32],
) -> Result<ConfirmedBudgetMutation, BudgetCreationError> {
    if submission.idempotency_key() != mutation_key {
        return Err(BudgetCreationError::ActivityBindingMismatch);
    }
    let mutation = budget_mutation_identity(submission.exact_bytes(), registry, context)?;
    let receipt = pipeline.submit_verified(submission)?;
    let verified_receipt = verifier
        .verify_receipt(&receipt.evidence)
        .map_err(|_| BudgetCreationError::UnverifiedReceipt)?;
    if verified_receipt.activity_id() != submission.activity_id() {
        return Err(BudgetCreationError::ReceiptActivityMismatch);
    }
    if verified_receipt.result_code() != 0 {
        return Err(BudgetCreationError::CoreRejected);
    }
    let state = pipeline.budget_state(mutation.budget_id())?;
    let verified_state = verifier
        .verify_state(&state.evidence)
        .map_err(|_| BudgetCreationError::CreatedBudgetUnconfirmed)?;
    let record = ProtocolBudgetRecord::decode(verified_state.canonical_state())
        .map_err(|_| BudgetCreationError::CreatedBudgetUnconfirmed)?;
    let same_identity = record.budget_id == context.budget_id
        && record.owner == context.owner
        && record.budget_account == context.budget_account
        && record.asset_id == context.asset
        && record.source_account.is_some() == context.native_source
        && !record.closed;
    let effect = match mutation {
        BudgetMutation::Fund { .. } => {
            !record.revoked && record.revocation_sequence == context.revocation_sequence
        }
        BudgetMutation::Revoke {
            revocation_sequence,
            ..
        } => {
            record.revoked
                && record.revocation_sequence == revocation_sequence
                && record.per_period_limit == record.spent_this_period
        }
    };
    if !same_identity || !effect {
        return Err(BudgetCreationError::CreatedBudgetUnconfirmed);
    }
    Ok(ConfirmedBudgetMutation {
        mutation,
        activity_id: submission.activity_id(),
        receipt_bytes: verified_receipt.canonical_receipt().to_vec(),
        observed_head_sequence: verified_state.observed_head_sequence(),
        record,
    })
}
