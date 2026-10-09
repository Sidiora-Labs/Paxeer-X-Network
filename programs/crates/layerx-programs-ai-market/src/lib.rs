//! PAXAI schema-1 common contracts and the direct native Programs entrypoint. No codec
//! value establishes host authority or finality.
#![cfg_attr(target_arch = "wasm32", no_std)]

pub mod admission;
pub mod aggregation;
pub mod aggregation_codec;
pub mod codec;
pub mod commit_reveal;
pub mod dispatch;
pub mod epoch;
pub mod errors;
pub mod evidence;
pub mod evaluators {
    pub mod admission;
    pub mod authority;
    pub mod codec;
    pub mod model;
}
pub mod host_adapter;
pub mod policy;
pub mod queries;
pub mod registry;
pub mod registry_ops;
pub mod reputation;
pub mod reputation_codec;
pub mod reputation_transition;
pub mod reward_math;
pub mod rewards;
pub mod roster;
pub mod state;
pub mod tasks;
pub mod types;
pub mod workers;

#[cfg(target_arch = "wasm32")]
mod guest {
    use crate::dispatch::{self, Buffers, Routed, SCRATCH_BYTES};
    use crate::host_adapter::{AdapterError, ApplicationEvent, DirectCall};
    use crate::registry_ops::CallContext;
    use crate::types::{ChainDomain, Presence};
    use crate::{codec, ApplicationError, MAX_EVENT_BYTES, MAX_RESULT_BYTES, MAX_STATE_BYTES};
    use layerx_program_sdk::{
        CallResult, EntryResponse, ProgramRefusal, RefusalClass, RefusalReason,
    };

    layerx_program_sdk::trap_on_panic!();
    layerx_program_sdk::failure_entrypoint!(handle);

    const CHAIN_DOMAIN: Option<[u8; 32]> = match option_env!("PAXAI_CHAIN_DOMAIN") {
        Some(hex) => parse_domain(hex.as_bytes()),
        None => None,
    };

    const fn nibble(byte: u8) -> Option<u8> {
        match byte {
            b'0'..=b'9' => Some(byte - b'0'),
            b'a'..=b'f' => Some(byte - b'a' + 10),
            _ => None,
        }
    }

    const fn parse_domain(hex: &[u8]) -> Option<[u8; 32]> {
        if hex.len() != 64 {
            return None;
        }
        let mut out = [0; 32];
        let mut i = 0;
        while i < 32 {
            let (Some(high), Some(low)) = (nibble(hex[2 * i]), nibble(hex[2 * i + 1])) else {
                return None;
            };
            out[i] = (high << 4) | low;
            i += 1;
        }
        Some(out)
    }

    struct Arena {
        current: [u8; MAX_STATE_BYTES],
        next: [u8; MAX_STATE_BYTES],
        scratch: [u8; SCRATCH_BYTES],
        event: [u8; MAX_EVENT_BYTES],
        result: [u8; MAX_RESULT_BYTES],
    }

    static ARENA: spin::Mutex<Arena> = spin::Mutex::new(Arena {
        current: [0; MAX_STATE_BYTES],
        next: [0; MAX_STATE_BYTES],
        scratch: [0; SCRATCH_BYTES],
        event: [0; MAX_EVENT_BYTES],
        result: [0; MAX_RESULT_BYTES],
    });

    const EMPTY_REFUSAL: ProgramRefusal<'static> = match RefusalReason::new(&[]) {
        Ok(reason) => match ProgramRefusal::new(RefusalClass::Rejected, reason) {
            Ok(refusal) => refusal,
            Err(_) => panic!("Rejected is guest publishable"),
        },
        Err(_) => panic!("an empty reason is within the ABI bound"),
    };

    fn rejected(reason: &'static [u8]) -> ProgramRefusal<'static> {
        RefusalReason::new(reason)
            .and_then(|r| ProgramRefusal::new(RefusalClass::Rejected, r))
            .unwrap_or(EMPTY_REFUSAL)
    }

    fn failure(
        error: ApplicationError,
        result: &'static mut [u8; MAX_RESULT_BYTES],
    ) -> ProgramRefusal<'static> {
        match codec::ApplicationResult::failure(error, Presence::Absent, 0)
            .and_then(|value| codec::encode_result(&value, result))
        {
            Ok(n) => rejected(&result[..n]),
            Err(_) => EMPTY_REFUSAL,
        }
    }

    fn handle(input: &[u8]) -> Result<EntryResponse<'static>, ProgramRefusal<'static>> {
        let arena = spin::MutexGuard::leak(ARENA.try_lock().ok_or(EMPTY_REFUSAL)?);
        let Arena {
            current,
            next,
            scratch,
            event,
            result,
        } = arena;
        let call = match DirectCall::read_context() {
            Ok(call) => call,
            Err(AdapterError::Application(error)) => return Err(failure(error, result)),
            Err(AdapterError::Host(_)) => return Err(EMPTY_REFUSAL),
        };
        let Some(chain) = CHAIN_DOMAIN.and_then(|bytes| ChainDomain::new(bytes).ok()) else {
            return Err(failure(crate::errors::WRONG_DOMAIN, result));
        };
        let ctx = CallContext {
            chain,
            program: call.program(),
            principal: call.principal(),
            height: call.height(),
        };
        let current_len = match call.read_state(current) {
            Ok(Some(state)) => match state.encoded_len() {
                Ok(n) => Some(n),
                Err(error) => return Err(failure(error, result)),
            },
            Ok(None) => None,
            Err(AdapterError::Application(error)) => return Err(failure(error, result)),
            Err(AdapterError::Host(_)) => return Err(EMPTY_REFUSAL),
        };
        let routed = dispatch::route(
            &ctx,
            input,
            current_len.map(|n| &current[..n]),
            Buffers {
                next,
                scratch,
                event,
                result,
            },
        );
        match routed {
            Ok(Routed::Applied {
                operation,
                state_len,
                event_len,
                result_len,
                ..
            }) => {
                let events = [ApplicationEvent {
                    operation,
                    body: &event[..event_len],
                }];
                match call.stage_state_and_events(&next[..state_len], &events) {
                    Ok(()) => Ok(EntryResponse::new(CallResult::OK, &result[..result_len])),
                    Err(AdapterError::Application(error)) => Err(failure(error, result)),
                    Err(AdapterError::Host(_)) => Err(EMPTY_REFUSAL),
                }
            }
            Ok(Routed::Unchanged { result_len }) => {
                Ok(EntryResponse::new(CallResult::OK, &result[..result_len]))
            }
            Ok(Routed::Refused { result_len }) => Err(rejected(&result[..result_len])),
            Err(error) => Err(failure(error, result)),
        }
    }
}

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
