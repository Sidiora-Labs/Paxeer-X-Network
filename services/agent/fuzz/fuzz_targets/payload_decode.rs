#![no_main]

mod support;

use layerx_types::payload::{ActivityType, Payload};
use layerx_wire::decode::Decoder;
use layerx_wire::encode::Encoder;
use libfuzzer_sys::fuzz_target;

pub fn fuzz_target_payload_decode(data: &[u8]) {
    let materialized = support::input_bytes(data);
    let Some(registry) = support::registry() else {
        return;
    };
    let mut decoder = Decoder::new(&materialized, support::MAX_FUZZ_INPUT);
    let Ok(raw_type) = decoder.u32() else {
        return;
    };
    let Ok(bytes) = decoder.bytes_owned(support::MAX_FUZZ_INPUT / 2) else {
        return;
    };
    if decoder.finish().is_err() {
        return;
    }
    let Ok(activity_type) = ActivityType::from_u32(raw_type) else {
        return;
    };
    let Ok(payload) = Payload::new(&registry, activity_type, &bytes) else {
        return;
    };
    let mut encoder = Encoder::new(support::MAX_FUZZ_INPUT);
    if encoder.u32(payload.activity_type().value()).is_ok()
        && encoder
            .bytes(payload.as_bytes(), support::MAX_FUZZ_INPUT / 2)
            .is_ok()
    {
        assert_eq!(encoder.finish(), materialized.as_ref());
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_payload_decode(data));
