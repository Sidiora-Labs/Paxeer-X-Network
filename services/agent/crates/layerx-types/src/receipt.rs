//! Decode-only activity and 402LXP receipt domain types.

use crate::amount::Amount;
use crate::checkpoint::{
    ActivityInclusionProof, CheckpointCertificate, PaxeerSettlementReference, StateInclusionProof,
};
use crate::ids::{ActivityId, AssetId, BatchId};
use crate::result::ResultCode;

/// Exact base activity-receipt fields produced by the execution kernel.
pub const ACTIVITY_RECEIPT_FIELDS: [&str; 15] = [
    "protocol_version",
    "activity_id",
    "global_sequence",
    "previous_state_root",
    "resulting_state_root",
    "activity_root",
    "result_code",
    "effects",
    "fee_charged",
    "batch_id",
    "module_id",
    "module_version",
    "parameter_version",
    "timestamp",
    "sequencer_signature",
];

/// Exact 402LXP financial receipt fields.
pub const LXP_RECEIPT_FIELDS: [&str; 21] = [
    "protocol_version",
    "transaction_id",
    "operation",
    "global_sequence",
    "asset",
    "amount",
    "from",
    "from_balance_before",
    "from_balance_after",
    "from_sequence",
    "to",
    "to_balance_before",
    "to_balance_after",
    "transfer_set_root",
    "authorization_hash",
    "context_hash",
    "previous_state_root",
    "resulting_state_root",
    "batch_id",
    "timestamp",
    "sequencer_signature",
];

/// The three and only three effect kinds emitted by core.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum EffectKind {
    /// Non-monetary state mutation.
    State,
    /// 402LXP transfer-set effect.
    Transfer,
    /// Ordered event emission.
    Event,
}

/// One exact effect record decoded from a receipt.
#[allow(dead_code)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Effect {
    module_id: u16,
    ordinal: u16,
    event_type: u16,
    kind: EffectKind,
    monetary: bool,
    transfer_set_root: [u8; 32],
    body: Box<[u8]>,
}

/// A core-produced activity receipt. There is deliberately no public value
/// constructor; `layerx-wire` supplies canonical byte decoding.
#[allow(dead_code)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ActivityReceipt {
    protocol_version: u16,
    activity_id: ActivityId,
    global_sequence: u64,
    previous_state_root: [u8; 32],
    resulting_state_root: [u8; 32],
    activity_root: [u8; 32],
    result_code: ResultCode,
    effects: Vec<Effect>,
    fee_charged: Amount,
    batch_id: BatchId,
    module_id: u16,
    module_version: u32,
    parameter_version: u32,
    timestamp: u64,
    sequencer_signature: [u8; 64],
}

/// A core-produced 402LXP receipt. Its evidence fields are private and it has
/// no local-value constructor, preventing callers from fabricating balances.
#[allow(dead_code)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LxpReceipt {
    protocol_version: u16,
    transaction_id: ActivityId,
    operation: u8,
    global_sequence: u64,
    asset: AssetId,
    amount: Amount,
    from: [u8; 32],
    from_balance_before: Amount,
    from_balance_after: Amount,
    from_sequence: u64,
    to: [u8; 32],
    to_balance_before: Amount,
    to_balance_after: Amount,
    transfer_set_root: [u8; 32],
    authorization_hash: [u8; 32],
    context_hash: [u8; 32],
    previous_state_root: [u8; 32],
    resulting_state_root: [u8; 32],
    batch_id: BatchId,
    timestamp: u64,
    sequencer_signature: [u8; 64],
}

/// A finalized 402LXP receipt with all proof material kept distinct from the
/// pre-checkpoint sequencer promise.
#[allow(dead_code)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CheckpointedLxpReceipt {
    receipt: LxpReceipt,
    canonical_activity: Box<[u8]>,
    state_leaf: Box<[u8]>,
    activity_inclusion_proof: ActivityInclusionProof,
    state_inclusion_proof: StateInclusionProof,
    checkpoint_id: [u8; 32],
    guarantor_certificate: CheckpointCertificate,
    paxeer_settlement_reference: PaxeerSettlementReference,
}
