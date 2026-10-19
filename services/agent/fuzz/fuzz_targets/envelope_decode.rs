#![no_main]

mod support;

use layerx_wire::activity::{decode_signed, decode_unsigned, encode_signed, encode_unsigned};
use libfuzzer_sys::fuzz_target;

pub fn fuzz_target_envelope_decode(data: &[u8]) {
    let materialized = support::input_bytes(data);
    let Some(registry) = support::registry() else {
        return;
    };
    if let Ok(activity) = decode_signed(&materialized, &registry) {
        assert_eq!(
            encode_signed(&activity).as_deref(),
            Ok(materialized.as_ref())
        );
    }
    if let Ok(activity) = decode_unsigned(&materialized, &registry) {
        assert_eq!(
            encode_unsigned(&activity).as_deref(),
            Ok(materialized.as_ref())
        );
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_envelope_decode(data));
