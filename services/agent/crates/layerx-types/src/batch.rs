//! Exact batch-header commitments from `lxp_batch_header`.

/// The exact fifteen ordered commitments in a protocol batch header.
pub const BATCH_HEADER_FIELDS: [&str; 15] = [
    "protocol_version",
    "network_id",
    "epoch",
    "batch_number",
    "first_sequence",
    "last_sequence",
    "previous_state_root",
    "resulting_state_root",
    "activity_merkle_root",
    "receipt_merkle_root",
    "event_merkle_root",
    "data_availability_root",
    "oracle_root",
    "timestamp_ms",
    "sequencer_id",
];

/// The protocol batch header. Private fields ensure only canonical decoding can
/// construct it; the decoder is implemented by `layerx-wire`.
#[allow(dead_code)]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BatchHeader {
    protocol_version: u16,
    network_id: u32,
    epoch: u64,
    batch_number: u64,
    first_sequence: u64,
    last_sequence: u64,
    previous_state_root: [u8; 32],
    resulting_state_root: [u8; 32],
    activity_merkle_root: [u8; 32],
    receipt_merkle_root: [u8; 32],
    event_merkle_root: [u8; 32],
    data_availability_root: [u8; 32],
    oracle_root: [u8; 32],
    timestamp_ms: u64,
    sequencer_id: [u8; 32],
}
