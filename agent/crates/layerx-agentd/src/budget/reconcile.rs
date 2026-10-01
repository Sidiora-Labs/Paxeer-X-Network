//! Receipt- and protocol-state-authoritative budget reconciliation.

use std::cmp::Ordering;

use crate::protocol_evidence::{
    EvidenceAuthority, RawReceiptEvidence, RawStateEvidence, ReceiptReplayError,
};

/// State inclusion candidate for a protocol budget.
///
/// The verified leaf must carry the core budget module record stored under
/// [`budget_state_key`]; any other leaf leaves reconciliation unavailable.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProtocolBudgetState {
    pub evidence: RawStateEvidence,
}

pub use layerx_client::budget::{
    budget_state_key, ProtocolBudgetRecord, ReconcileError, BUDGET_MODULE_ID,
};

/// Raw receipt evidence applied to one declared protocol budget window.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SpendReceiptEvidence {
    pub expected_activity_id: [u8; 32],
    pub evidence: RawReceiptEvidence,
}

/// Rebuildable local cache, never authority.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LocalAccounting {
    pub consumed: u128,
    pub window_start_sequence: u64,
    pub last_receipt: Option<[u8; 32]>,
}

/// Opaque reconciliation result issued only after protocol evidence succeeds.
///
/// No public constructor exists; a value is issued only after the verified leaf
/// decoded as one canonical core budget record.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReconciliationState {
    last_verified_receipt: Option<[u8; 32]>,
    protocol_consumed: u128,
    local_before: u128,
    local_after: u128,
    divergence: Option<i128>,
    window_start_sequence: u64,
    window_end_sequence: u64,
    remaining: u128,
    observed_head_sequence: u64,
}

impl ReconciliationState {
    /// Returns the protocol remaining amount from this opaque verified result.
    #[must_use]
    pub const fn remaining(&self) -> u128 {
        self.remaining
    }

    /// Returns the signed head sequence which anchored this result.
    #[must_use]
    pub const fn observed_head_sequence(&self) -> u64 {
        self.observed_head_sequence
    }

    /// Returns the corrected local amount from this opaque verified result.
    #[must_use]
    pub const fn local_after(&self) -> u128 {
        self.local_after
    }

    pub(crate) const fn last_verified_receipt(&self) -> Option<[u8; 32]> {
        self.last_verified_receipt
    }

    pub(crate) const fn protocol_consumed(&self) -> u128 {
        self.protocol_consumed
    }

    pub(crate) const fn local_before(&self) -> u128 {
        self.local_before
    }

    pub(crate) const fn divergence(&self) -> Option<i128> {
        self.divergence
    }

    pub(crate) const fn window_start_sequence(&self) -> u64 {
        self.window_start_sequence
    }

    pub(crate) const fn window_end_sequence(&self) -> u64 {
        self.window_end_sequence
    }
}

/// Verifies receipt identity and state inclusion, then rebuilds the local cache
/// from the decoded canonical budget record.
pub(crate) fn reconcile_state(
    local: &mut LocalAccounting,
    protocol: &ProtocolBudgetState,
    receipts: &[SpendReceiptEvidence],
    verifier: &EvidenceAuthority,
) -> Result<ReconciliationState, ReconcileError> {
    let mut replay = EvidenceAuthority::receipt_replay_guard();
    let mut last_verified_receipt = local.last_receipt;
    for receipt in receipts {
        let verified_receipt = verifier
            .verify_receipt(&receipt.evidence)
            .map_err(|_| ReconcileError::UnverifiedReceipt)?;
        if verified_receipt.activity_id() != receipt.expected_activity_id {
            return Err(ReconcileError::ReceiptActivityMismatch);
        }
        replay.admit(&verified_receipt).map_err(map_replay_error)?;
        last_verified_receipt = Some(verified_receipt.activity_id());
    }
    let verified_state = verifier
        .verify_state(&protocol.evidence)
        .map_err(|_| ReconcileError::UnverifiedProtocolState)?;
    let record = ProtocolBudgetRecord::decode(verified_state.canonical_state())
        .map_err(|_| ReconcileError::ProtocolStateSchemaUnavailable)?;
    let protocol_consumed = record.spent_this_period;
    let local_before = local.consumed;
    let divergence = match local_before.cmp(&protocol_consumed) {
        Ordering::Equal => None,
        Ordering::Greater => {
            Some(i128::try_from(local_before - protocol_consumed).unwrap_or(i128::MAX))
        }
        Ordering::Less => Some(
            i128::try_from(protocol_consumed - local_before)
                .map_or(i128::MIN, |difference| -difference),
        ),
    };
    let state = ReconciliationState {
        last_verified_receipt,
        protocol_consumed,
        local_before,
        local_after: protocol_consumed,
        divergence,
        window_start_sequence: record.period_start,
        window_end_sequence: record.window_end_sequence(),
        remaining: record.remaining(),
        observed_head_sequence: verified_state.observed_head_sequence(),
    };
    local.consumed = protocol_consumed;
    local.window_start_sequence = record.period_start;
    local.last_receipt = last_verified_receipt;
    Ok(state)
}

const fn map_replay_error(error: ReceiptReplayError) -> ReconcileError {
    match error {
        ReceiptReplayError::DuplicateReceipt => ReconcileError::DuplicateReceipt,
        ReceiptReplayError::DuplicateActivity => ReconcileError::DuplicateActivity,
    }
}
