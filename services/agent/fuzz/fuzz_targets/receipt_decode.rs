#![no_main]

mod support;

use layerx_wire::receipt::{decode, encode};
use libfuzzer_sys::fuzz_target;

pub fn fuzz_target_receipt_decode(data: &[u8]) {
    let materialized = support::input_bytes(data);
    if let Ok(receipt) = decode(&materialized) {
        assert_eq!(encode(&receipt).as_deref(), Ok(materialized.as_ref()));
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_receipt_decode(data));
