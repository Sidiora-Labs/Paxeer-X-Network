//! Constructor limits copied from the normative protocol headers.

/// Every consensus identifier and digest is 32 bytes.
pub const IDENTIFIER_BYTES: usize = 32;
/// Maximum canonical DID byte length (`LXP_MAX_DID_LENGTH`).
pub const MAX_DID_BYTES: usize = 255;
/// Maximum canonical account namespace length (`LX_ACCOUNT_NAME_MAX`).
pub const MAX_ACCOUNT_NAME_BYTES: usize = 512;
/// Maximum canonical authority byte length (`LXP_MAX_PAYLOAD_BYTES`).
pub const MAX_AUTHORITY_BYTES: usize = 524_288;
/// Maximum canonical module payload byte length (`LXP_MAX_PAYLOAD_BYTES`).
pub const MAX_PAYLOAD_BYTES: usize = 524_288;
/// Maximum canonical activity signature byte length.
pub const MAX_SIGNATURE_BYTES: usize = 128;
/// Maximum activity types declared by one module registration.
pub const MAX_MODULE_ACTIVITY_TYPES: usize = 64;
/// Maximum receipt effects (`LXP_MAX_EFFECTS`).
pub const MAX_EFFECTS: usize = 512;
/// Maximum effect body bytes.
pub const MAX_EFFECT_BODY_BYTES: usize = 256;
/// Maximum Merkle proof depth (`LXP_MERKLE_MAX_DEPTH`).
pub const MAX_MERKLE_DEPTH: usize = 32;
/// Maximum guarantor attestations in a certificate.
pub const MAX_GUARANTOR_ATTESTATIONS: usize = 32;
/// Maximum checkpoint validity-proof bytes.
pub const MAX_VALIDITY_PROOF_BYTES: usize = 1_048_576;
/// Maximum Paxeer settlement-reference bytes.
pub const MAX_SETTLEMENT_REFERENCE_BYTES: usize = 1_024;
