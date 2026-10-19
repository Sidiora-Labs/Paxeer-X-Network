#![no_main]

mod support;

use layerx_wire::activity::{decode_signed, decode_unsigned, encode_signed, encode_unsigned};
use layerx_wire::receipt::{
    decode, decode_batch_header, decode_checkpoint, decode_merkle_proof, encode,
    encode_batch_header, encode_checkpoint, encode_merkle_proof,
};
use libfuzzer_sys::fuzz_target;

pub fn fuzz_target_roundtrip(data: &[u8]) {
    let materialized = support::input_bytes(data);
    let Some((&selector, bytes)) = materialized.split_first() else {
        return;
    };
    match selector % 6 {
        0 => {
            let Some(registry) = support::registry() else {
                return;
            };
            if let Ok(value) = decode_signed(bytes, &registry) {
                assert_eq!(encode_signed(&value).as_deref(), Ok(bytes));
            }
        }
        1 => {
            let Some(registry) = support::registry() else {
                return;
            };
            if let Ok(value) = decode_unsigned(bytes, &registry) {
                assert_eq!(encode_unsigned(&value).as_deref(), Ok(bytes));
            }
        }
        2 => {
            if let Ok(value) = decode(bytes) {
                assert_eq!(encode(&value).as_deref(), Ok(bytes));
            }
        }
        3 => {
            if let Ok(value) = decode_merkle_proof(bytes) {
                assert_eq!(encode_merkle_proof(&value).as_deref(), Ok(bytes));
            }
        }
        4 => {
            if let Ok(value) = decode_batch_header(bytes) {
                assert_eq!(encode_batch_header(&value).as_deref(), Ok(bytes));
            }
        }
        _ => {
            if let Ok(value) = decode_checkpoint(bytes) {
                assert_eq!(encode_checkpoint(&value).as_deref(), Ok(bytes));
            }
        }
    }
}

fuzz_target!(|data: &[u8]| fuzz_target_roundtrip(data));
