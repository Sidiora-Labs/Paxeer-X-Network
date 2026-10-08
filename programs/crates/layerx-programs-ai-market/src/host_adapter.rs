use crate::errors::ApplicationError;
use layerx_program_sdk::ProgramError;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AdapterError {
    Application(ApplicationError),
    Host(ProgramError),
}
impl From<ApplicationError> for AdapterError {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<ProgramError> for AdapterError {
    fn from(error: ProgramError) -> Self {
        Self::Host(error)
    }
}
pub type AdapterResult<T> = Result<T, AdapterError>;

pub const READ_CAPABILITIES: [layerx_program_sdk::Capability; 1] =
    [layerx_program_sdk::Capability::SharedStorageRead];
pub const MUTATION_CAPABILITIES: [layerx_program_sdk::Capability; 3] = [
    layerx_program_sdk::Capability::SharedStorageRead,
    layerx_program_sdk::Capability::SharedStorageWrite,
    layerx_program_sdk::Capability::EmitEvent,
];

#[cfg(target_arch = "wasm32")]
mod wasm {
    use super::*;
    use crate::{
        codec, dispatch::Operation, errors::*, state, types::*, MAX_EVENTS, MAX_STATE_BYTES,
        SHARED_STATE_KEY,
    };
    use layerx_program_sdk::{
        crypto,
        storage::shared::{self, SharedStorageKey},
    };
    use layerx_program_sdk::{Context, EventData, EventTopic, StorageValue};

    pub struct DirectCall {
        program: ProgramId,
        principal: PrincipalId,
        height: u64,
        activity_sequence: u64,
        abi_version: u16,
        runtime_version: u16,
        fee_schedule_version: u32,
    }
    pub struct ApplicationEvent<'a> {
        pub operation: Operation,
        pub body: &'a [u8],
    }

    impl DirectCall {
        pub fn read_context() -> AdapterResult<Self> {
            if Context::immediate_caller()?.is_some() {
                return Err(UNAUTHORIZED.into());
            }
            Ok(Self {
                program: ProgramId::new(Context::executing_program()?.bytes())?,
                principal: PrincipalId::new(Context::invoking_principal()?.bytes())?,
                height: Context::batch_height()?,
                activity_sequence: Context::activity_sequence()?,
                abi_version: Context::abi_version()?,
                runtime_version: Context::runtime_version()?,
                fee_schedule_version: Context::fee_schedule_version()?,
            })
        }
        pub const fn program(&self) -> ProgramId {
            self.program
        }
        pub const fn principal(&self) -> PrincipalId {
            self.principal
        }
        pub const fn height(&self) -> u64 {
            self.height
        }
        pub const fn activity_sequence(&self) -> u64 {
            self.activity_sequence
        }
        pub const fn abi_version(&self) -> u16 {
            self.abi_version
        }
        pub const fn runtime_version(&self) -> u16 {
            self.runtime_version
        }
        pub const fn fee_schedule_version(&self) -> u32 {
            self.fee_schedule_version
        }
        pub fn check_native_actor(&self, envelope: &codec::Envelope<'_>) -> AdapterResult<()> {
            if envelope.program != self.program {
                return Err(WRONG_PROGRAM.into());
            }
            if envelope.expiry == 0 {
                return Err(NON_CANONICAL.into());
            }
            if self.height >= envelope.expiry {
                return Err(EXPIRED.into());
            }
            codec::compare_native_principal(envelope, self.principal)?;
            Ok(())
        }
        pub fn read_state<'a>(
            &self,
            output: &'a mut [u8],
        ) -> AdapterResult<Option<state::SharedState<'a>>> {
            let bounded_len = output.len().min(MAX_STATE_BYTES);
            let length = shared::read(
                SharedStorageKey::new(SHARED_STATE_KEY)?,
                &mut output[..bounded_len],
            )?;
            match length {
                None => Ok(None),
                Some(length) => Ok(Some(state::decode_shared_state(&output[..length])?)),
            }
        }
        pub fn hash_domain(
            &self,
            domain: &str,
            body: &[u8],
            scratch: &mut [u8],
        ) -> AdapterResult<Digest32> {
            if domain.is_empty() || !domain.is_ascii() || domain.as_bytes().contains(&0) {
                return Err(NON_CANONICAL.into());
            }
            let n = domain
                .len()
                .checked_add(1)
                .and_then(|n| n.checked_add(body.len()))
                .ok_or(ARITHMETIC)?;
            if n > crypto::MAX_HASH_INPUT_BYTES || scratch.len() < n {
                return Err(CAPACITY.into());
            }
            let mut w = codec::Writer::new(&mut scratch[..n]);
            w.put(domain.as_bytes())?;
            w.u8(0)?;
            w.put(body)?;
            let digest = crypto::hash(
                crypto::HashAlgorithm::Sha256,
                crypto::HashInput::new(&scratch[..n])?,
            )?;
            Ok(Digest32::new(digest)?)
        }
        pub fn request_digest(
            &self,
            envelope: &codec::ValidatedEnvelope<'_>,
            scratch: &mut [u8],
        ) -> AdapterResult<RequestDigest> {
            Ok(RequestDigest::new(
                self.hash_domain("PAXAI/request/v1", envelope.unsigned_bytes(), scratch)?
                    .bytes(),
            )?)
        }
        pub fn verify_digest(
            &self,
            digest: Digest32,
            key: PublicKey32,
            signature: Signature64,
        ) -> AdapterResult<()> {
            let message = digest.bytes();
            crypto::ed25519_verify(crypto::Ed25519Message::new(&message)?, &key.0, &signature.0)?;
            Ok(())
        }
        pub fn stage_state_and_events(
            self,
            encoded_state: &[u8],
            events: &[ApplicationEvent<'_>],
        ) -> AdapterResult<()> {
            let state = state::decode_shared_state(encoded_state)?;
            if events.len() > MAX_EVENTS {
                return Err(CAPACITY.into());
            }
            let mut topics = [[0u8; 64]; MAX_EVENTS];
            let mut lengths = [0usize; MAX_EVENTS];
            let mut previous = None;
            for (i, event) in events.iter().enumerate() {
                lengths[i] = codec::event_topic(event.operation, &mut topics[i])?;
                let (_, common, _) =
                    codec::decode_event_frame(&topics[i][..lengths[i]], event.body)?;
                if common.revision != state.revision {
                    return Err(NON_CANONICAL.into());
                }
                if previous.is_some_and(|p| p != common) {
                    return Err(NON_CANONICAL.into());
                }
                previous = Some(common);
                EventTopic::new(&topics[i][..lengths[i]])?;
                EventData::new(event.body)?;
            }
            let key = SharedStorageKey::new(SHARED_STATE_KEY)?;
            let value = StorageValue::new(encoded_state)?;
            shared::write(key, value)?;
            for (i, event) in events.iter().enumerate() {
                layerx_program_sdk::event::emit(
                    EventTopic::new(&topics[i][..lengths[i]])?,
                    EventData::new(event.body)?,
                )?;
            }
            Ok(())
        }
    }
}
#[cfg(target_arch = "wasm32")]
pub use wasm::{ApplicationEvent, DirectCall};
