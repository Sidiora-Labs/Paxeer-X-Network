#[path = "support/send_authorization.rs"]
mod send_authorization;

use std::future::Future;
use std::pin::pin;
use std::sync::Arc;
use std::task::{Context, Poll, Wake, Waker};

use layerx_agentd::prepare::{
    prepare_activity, CorePreparationBoundary, CorePreparationState, CoreStateError,
    PreparationDefaults, PrepareRequest, Prepared,
};
use layerx_agentd::sign::{attach_external_signature, verify_before_submit, SigningError};
use layerx_crypto::local::LocalSigner;
use layerx_crypto::signer::{sign_disclosed, Signer};
use layerx_types::activity::{Authority, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_wire::encode::Encoder;

struct NoopWake;

impl Wake for NoopWake {
    fn wake(self: Arc<Self>) {}
}

fn ready<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let waker = Waker::from(Arc::new(NoopWake));
    let mut context = Context::from_waker(&waker);
    match future.as_mut().poll(&mut context) {
        Poll::Ready(value) => value,
        Poll::Pending => panic!("local signing unexpectedly blocked"),
    }
}

struct RecordedCore(CorePreparationState);

impl CorePreparationBoundary for RecordedCore {
    fn preparation_state(&mut self, _actor: &Did) -> Result<CorePreparationState, CoreStateError> {
        Ok(self.0.clone())
    }
}

fn activity_type() -> ActivityType {
    ActivityType::new(ModuleId::Asset, 5).unwrap_or_else(|error| panic!("activity: {error:?}"))
}

fn registry() -> ModuleRegistry {
    ModuleRegistry::new(
        &[ModuleRegistration::new(ModuleId::Asset, &[activity_type()])
            .unwrap_or_else(|error| panic!("registration: {error:?}"))],
    )
    .unwrap_or_else(|error| panic!("registry: {error:?}"))
}

fn send_payload() -> Vec<u8> {
    let mut encoder = Encoder::new(512);
    encoder
        .u16(0x5301)
        .unwrap_or_else(|error| panic!("tag: {error:?}"));
    encoder
        .u16(10)
        .unwrap_or_else(|error| panic!("fields: {error:?}"));
    encoder
        .fixed(&[0x11; 32])
        .unwrap_or_else(|error| panic!("from: {error:?}"));
    encoder
        .fixed(&[0x22; 32])
        .unwrap_or_else(|error| panic!("to: {error:?}"));
    encoder
        .fixed(&[0x33; 32])
        .unwrap_or_else(|error| panic!("asset: {error:?}"));
    encoder
        .u128(25)
        .unwrap_or_else(|error| panic!("amount: {error:?}"));
    encoder
        .u64(5)
        .unwrap_or_else(|error| panic!("sequence: {error:?}"));
    encoder
        .fixed(&[4; 32])
        .unwrap_or_else(|error| panic!("idempotency: {error:?}"));
    encoder
        .u64(1_010)
        .unwrap_or_else(|error| panic!("expiry: {error:?}"));
    encoder
        .fixed(&[0x55; 32])
        .unwrap_or_else(|error| panic!("context: {error:?}"));
    encoder
        .u8(0)
        .unwrap_or_else(|error| panic!("conditions: {error:?}"));
    encoder
        .u8(1)
        .unwrap_or_else(|error| panic!("authority kind: {error:?}"));
    encoder
        .fixed(&[0x11; 32])
        .unwrap_or_else(|error| panic!("controller: {error:?}"));
    encoder
        .fixed(&[0x66; 32])
        .unwrap_or_else(|error| panic!("payload key: {error:?}"));
    encoder
        .fixed(&[0x77; 64])
        .unwrap_or_else(|error| panic!("payload signature: {error:?}"));
    encoder
        .fixed(&[0x55; 32])
        .unwrap_or_else(|error| panic!("signed context: {error:?}"));
    encoder
        .u32(17)
        .unwrap_or_else(|error| panic!("network: {error:?}"));
    encoder
        .u16(layerx_wire::limits::PROTOCOL_VERSION)
        .unwrap_or_else(|error| panic!("version: {error:?}"));
    send_authorization::sign(encoder.finish())
}

fn owner_bound(owner: &LocalSigner) -> Prepared {
    let mut core = RecordedCore(CorePreparationState {
        network_id: 17,
        account_sequence: 5,
        protocol_timestamp: 1_000,
        observed_head_sequence: 88,
        module_registry: registry(),
    });
    prepare_activity(
        &mut core,
        PreparationDefaults {
            timestamp_span: 30,
            fee_limit: Amount::from_u128(12),
            maximum_payload_bytes: 1_024,
        },
        PrepareRequest {
            actor: Did::new(b"did:layerx:owner-binding")
                .unwrap_or_else(|error| panic!("DID: {error:?}")),
            authority: Authority::owner(&owner.public_key())
                .unwrap_or_else(|error| panic!("authority: {error:?}")),
            activity_type: activity_type(),
            expected_account_sequence: Some(5),
            timestamp_bound: Some(
                TimestampBound::new(995, 1_010)
                    .unwrap_or_else(|error| panic!("timestamp: {error:?}")),
            ),
            fee_limit: Some(Amount::from_u128(7)),
            idempotency_key: IdempotencyKey::new([4; 32]),
            payload: send_payload(),
            declared_payload_limit: 1_024,
        },
    )
    .unwrap_or_else(|error| panic!("prepare: {error:?}"))
}

fn signed_by(prepared: &Prepared, signer: &LocalSigner) -> Vec<u8> {
    let signature = ready(sign_disclosed(
        signer,
        &prepared.canonical_bytes,
        &prepared.disclosure,
        &registry(),
    ))
    .unwrap_or_else(|error| panic!("sign: {error:?}"));
    attach_external_signature(prepared, *signature.as_bytes())
        .unwrap_or_else(|error| panic!("attach: {error:?}"))
}

#[test]
fn a_foreign_key_signature_over_the_owner_preimage_is_refused() {
    let owner = LocalSigner::new([0xa5; 32]);
    let foreign = LocalSigner::new([0x5a; 32]);
    assert_ne!(owner.public_key(), foreign.public_key());
    let prepared = owner_bound(&owner);
    let signed_bytes = signed_by(&prepared, &foreign);
    assert_eq!(
        verify_before_submit(&signed_bytes, &prepared, &foreign.public_key(), &registry()),
        Err(SigningError::AuthorityMismatch)
    );
    assert_eq!(
        verify_before_submit(&signed_bytes, &prepared, &owner.public_key(), &registry()),
        Err(SigningError::SignatureInvalid)
    );
}

#[test]
fn the_bound_owner_key_still_verifies() {
    let owner = LocalSigner::new([0xa5; 32]);
    let prepared = owner_bound(&owner);
    let signed_bytes = signed_by(&prepared, &owner);
    let verified = verify_before_submit(&signed_bytes, &prepared, &owner.public_key(), &registry())
        .unwrap_or_else(|error| panic!("verify: {error:?}"));
    assert_eq!(verified.exact_bytes(), signed_bytes);
    assert_ne!(verified.activity_id(), [0; 32]);
}
