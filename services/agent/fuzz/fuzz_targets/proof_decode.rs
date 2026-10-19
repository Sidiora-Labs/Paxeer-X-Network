#![no_main]

mod support;

use layerx_wire::receipt::{decode_merkle_proof, encode_merkle_proof};
use libfuzzer_sys::fuzz_target;

pub fn fuzz_target_proof_decode(data: &[u8]) {
    let materialized = support::input_bytes(data);
    if let Ok(proof) = decode_merkle_proof(&materialized) {
        assert_eq!(
            encode_merkle_proof(&proof).as_deref(),
            Ok(materialized.as_ref())
        );
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_proof_decode(data));
