use std::panic::{catch_unwind, AssertUnwindSafe};
use std::path::{Path, PathBuf};

use layerx_types::result::{KnownResult, ResultCode};
use layerx_types::vectors::{CodecVector, Corpus};
use layerx_wire::decode::Decoder;
use layerx_wire::encode::Encoder;
use layerx_wire::limits::MAX_MESSAGE_BYTES;
use layerx_wire::{check_ordered_keys, WireError};

fn repository_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../..")
}

fn run_vector(vector: &CodecVector) -> Result<(), WireError> {
    match vector.kind.as_str() {
        "u64" => {
            let mut decoder = Decoder::new(&vector.bytes, MAX_MESSAGE_BYTES);
            let value = decoder.u64()?;
            decoder.finish()?;
            let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
            encoder.u64(value)?;
            if encoder.as_bytes() != vector.bytes {
                return Err(WireError {
                    result: KnownResult::NonCanonical.into(),
                    offset: 0,
                });
            }
            Ok(())
        }
        "tag" => {
            let mut decoder = Decoder::new(&vector.bytes, MAX_MESSAGE_BYTES);
            let _ = decoder.tag(3)?;
            Ok(())
        }
        "bytes4" => {
            let mut decoder = Decoder::new(&vector.bytes, MAX_MESSAGE_BYTES);
            let _ = decoder.bytes(4)?;
            Ok(())
        }
        "seq" => {
            let mut decoder = Decoder::new(&vector.bytes, MAX_MESSAGE_BYTES);
            let first_length = usize::from(decoder.u8()?);
            let first = decoder.fixed(first_length)?;
            let second_length = usize::from(decoder.u8()?);
            let second = decoder.fixed(second_length)?;
            check_ordered_keys(&[first, second])
        }
        _ => Err(WireError {
            result: KnownResult::InvalidTag.into(),
            offset: 0,
        }),
    }
}

#[test]
fn published_primitive_vectors_match_exact_results() {
    let Ok(corpus) = Corpus::load(&repository_root()) else {
        panic!("published corpus failed to load");
    };
    for vector in corpus.valid_codec.iter().chain(&corpus.adversarial_codec) {
        let result = match run_vector(vector) {
            Ok(()) => ResultCode::from(KnownResult::Ok),
            Err(error) => error.result,
        };
        assert_eq!(result, vector.expected_result, "vector {}", vector.name);
    }
}

#[test]
fn generated_values_round_trip_byte_exactly() {
    let mut state = 0x9e37_79b9_7f4a_7c15_u64;
    for _ in 0..10_000 {
        state = state
            .wrapping_mul(6_364_136_223_846_793_005)
            .wrapping_add(1_442_695_040_888_963_407);
        let mut encoder = Encoder::new(128);
        assert!(encoder.u8(state.to_be_bytes()[0]).is_ok());
        assert!(encoder
            .u16(u16::from_be_bytes([
                state.to_be_bytes()[0],
                state.to_be_bytes()[1]
            ]))
            .is_ok());
        assert!(encoder
            .u32(u32::from_be_bytes(
                state.to_be_bytes()[0..4].try_into().unwrap_or([0; 4])
            ))
            .is_ok());
        assert!(encoder.u64(state).is_ok());
        assert!(encoder
            .u128(u128::from(state) << 64 | u128::from(!state))
            .is_ok());
        assert!(encoder
            .i32(i32::from_be_bytes(
                state.to_be_bytes()[0..4].try_into().unwrap_or([0; 4])
            ))
            .is_ok());
        let bytes = encoder.finish();
        let mut decoder = Decoder::new(&bytes, 0);
        assert_eq!(decoder.u8(), Ok(state.to_be_bytes()[0]));
        assert_eq!(
            decoder.u16(),
            Ok(u16::from_be_bytes([
                state.to_be_bytes()[0],
                state.to_be_bytes()[1]
            ]))
        );
        assert_eq!(
            decoder.u32(),
            Ok(u32::from_be_bytes(
                state.to_be_bytes()[0..4].try_into().unwrap_or([0; 4])
            ))
        );
        assert_eq!(decoder.u64(), Ok(state));
        assert_eq!(
            decoder.u128(),
            Ok(u128::from(state) << 64 | u128::from(!state))
        );
        assert_eq!(
            decoder.i32(),
            Ok(i32::from_be_bytes(
                state.to_be_bytes()[0..4].try_into().unwrap_or([0; 4])
            ))
        );
        assert!(decoder.finish().is_ok());
    }
}

#[test]
fn ordered_collections_and_limits_fail_closed() {
    assert!(check_ordered_keys(&[b"a", b"aa", b"b"]).is_ok());
    assert_eq!(
        check_ordered_keys(&[b"a", b"a"]),
        Err(WireError {
            result: KnownResult::UnsortedSequence.into(),
            offset: 0,
        })
    );
    let mut encoder = Encoder::new(64);
    assert!(encoder
        .ordered_map(&[(b"a", b"1"), (b"b", b"2")], 2, 1, 1)
        .is_ok());
    let mut decoder = Decoder::new(&[0, 0, 0, 5, 1, 2, 3, 4, 5], 4);
    assert_eq!(
        decoder.bytes_owned(8),
        Err(WireError {
            result: KnownResult::LengthLimit.into(),
            offset: 0,
        })
    );
    assert_eq!(decoder.allocated(), 0);
}

#[test]
fn arbitrary_input_never_unwinds() {
    let Ok(corpus) = Corpus::load(&repository_root()) else {
        panic!("published corpus failed to load");
    };
    for vector in corpus.valid_codec.iter().chain(&corpus.adversarial_codec) {
        for length in 0..=vector.bytes.len() {
            let input = &vector.bytes[..length];
            assert!(catch_unwind(AssertUnwindSafe(|| {
                let mut decoder = Decoder::new(input, 32);
                let _ = decoder.u8();
                let _ = decoder.u16();
                let _ = decoder.u32();
                let _ = decoder.u64();
                let _ = decoder.u128();
                let _ = decoder.i32();
                let _ = decoder.bytes_owned(16);
                let _ = decoder.finish();
            }))
            .is_ok());
        }
    }
}
