#![no_main]

mod support;

use layerx_wire::decode::Decoder;
use layerx_wire::encode::Encoder;
use libfuzzer_sys::fuzz_target;

fn exact(decoder: Decoder<'_>, encoder: Encoder, bytes: &[u8]) {
    if decoder.finish().is_ok() {
        assert_eq!(encoder.finish(), bytes);
    }
}

pub fn fuzz_target_primitive_decode(data: &[u8]) {
    let materialized = support::input_bytes(data);
    let Some((&selector, bytes)) = materialized.split_first() else {
        return;
    };
    let mut decoder = Decoder::new(bytes, support::MAX_FUZZ_INPUT);
    let mut encoder = Encoder::new(support::MAX_FUZZ_INPUT);
    match selector % 11 {
        0 => {
            if let Ok(value) = decoder.u8() {
                if encoder.u8(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        1 => {
            if let Ok(value) = decoder.u16() {
                if encoder.u16(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        2 => {
            if let Ok(value) = decoder.u32() {
                if encoder.u32(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        3 => {
            if let Ok(value) = decoder.u64() {
                if encoder.u64(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        4 => {
            if let Ok(value) = decoder.u128() {
                if encoder.u128(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        5 => {
            if let Ok(value) = decoder.i32() {
                if encoder.i32(value).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        6 => {
            if let Ok(value) = decoder.bytes(4096) {
                if encoder.bytes(value, 4096).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        7 => {
            if let Ok(value) = decoder.text(4096) {
                if encoder.text(value, 4096).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        8 => {
            if let Ok(value) = decoder.sequence_length(512) {
                if encoder.sequence_length(value, 512).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        9 => {
            if let Ok(value) = decoder.tag(12) {
                if encoder.tag(value, 12).is_ok() {
                    exact(decoder, encoder, bytes);
                }
            }
        }
        _ => {
            if decoder.structure_header(0x1001).is_ok() && encoder.structure_header(0x1001).is_ok()
            {
                exact(decoder, encoder, bytes);
            }
        }
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_primitive_decode(data));
