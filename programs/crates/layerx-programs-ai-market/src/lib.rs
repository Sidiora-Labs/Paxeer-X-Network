//! PAXAI schema-1 common contracts. This is a codec library, not an executable
//! Programs entrypoint. No codec value establishes host authority or finality.
#![no_std]

pub mod aggregation_codec;
pub mod codec;
pub mod commit_reveal {
    pub mod commitment;
}
pub mod dispatch;
pub mod errors;
pub mod evaluators {
    pub mod codec;
    pub mod model;
}
pub mod host_adapter;
pub mod policy;
pub mod registry;
pub mod state;
pub mod types;

pub use errors::{ApplicationError, CodecResult};
pub use types::*;

pub const SCHEMA_VERSION: u16 = 1;
pub const SHARED_STATE_KEY: &[u8] = b"paxai/state/v1";
pub const REWARDS_ACCOUNT_SEED: &[u8] = b"paxai/rewards/v1";
pub const MAX_ENVELOPE_BYTES: usize = 16_384;
pub const MAX_PAYLOAD_BYTES: usize = 15_965;
pub const MAX_RESULT_BYTES: usize = 16_384;
pub const RESULT_HEADER_BYTES: usize = 82;
pub const MAX_RESULT_PAYLOAD_BYTES: usize = 16_302;
pub const MAX_STATE_BYTES: usize = 196_608;
pub const MAX_EVENT_BYTES: usize = 2_048;
pub const MAX_EVENTS: usize = 2;
pub const MAX_WORKERS: usize = 32;
pub const MAX_EVALUATORS: usize = 8;
pub const MAX_TASKS: usize = 64;
pub const MAX_RETAINED_EPOCHS: usize = 32;
pub const MAX_PAYOUT_IDENTITIES: usize = 256;
pub const MAX_CHUNK_BYTES: usize = 8_192;
