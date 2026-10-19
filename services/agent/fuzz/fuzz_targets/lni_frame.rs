#![no_main]

mod support;

use layerx_client::lni::framing::decode_frame;
use libfuzzer_sys::fuzz_target;

const MAXIMUM: usize = 1_048_576;

fuzz_target!(|data: &[u8]| {
    let materialized = support::input_bytes(data);
    let _ = decode_frame(&materialized, MAXIMUM);
});
