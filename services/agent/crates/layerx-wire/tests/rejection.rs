use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_types::result::{KnownResult, ResultCode};
use layerx_types::vectors::{CodecVector, Corpus};
use layerx_wire::activity::decode_signed;
use layerx_wire::decode::Decoder;
use layerx_wire::limits::enforce;
use layerx_wire::{check_ordered_keys, WireError};

fn repository_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../..")
}

fn run_vector(vector: &CodecVector) -> Result<(), WireError> {
    match vector.kind.as_str() {
        "u64" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            let _ = decoder.u64()?;
            decoder.finish()
        }
        "tag" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            let _ = decoder.tag(3)?;
            Ok(())
        }
        "bytes4" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            let _ = decoder.bytes(4)?;
            Ok(())
        }
        "seq" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
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

fn registry() -> ModuleRegistry {
    let Ok(kind) = ActivityType::new(ModuleId::Asset, 1) else {
        panic!("valid activity type rejected");
    };
    let Ok(registration) = ModuleRegistration::new(ModuleId::Asset, &[kind]) else {
        panic!("valid registration rejected");
    };
    let Ok(registry) = ModuleRegistry::new(&[registration]) else {
        panic!("valid registry rejected");
    };
    registry
}

/// Executes the published corpus and returns the exact rejection-code coverage.
///
/// # Panics
///
/// Panics when a repository vector is unreadable, accepted, or classified
/// differently from the core-declared result.
#[must_use]
pub fn rejection_corpus() -> BTreeSet<i32> {
    let Ok(corpus) = Corpus::load(&repository_root()) else {
        panic!("published corpus failed to load");
    };
    corpus
        .adversarial_codec
        .iter()
        .map(|vector| {
            let Err(error) = run_vector(vector) else {
                panic!("adversarial vector accepted: {}", vector.name);
            };
            assert_eq!(
                error.result, vector.expected_result,
                "vector {}",
                vector.name
            );
            error.result.raw()
        })
        .collect()
}

#[test]
fn published_rejection_corpus_matches_core_classes() {
    assert_eq!(rejection_corpus(), BTreeSet::from([-6, -5, -4, -2, -1]));
}

#[test]
fn generated_noncanonical_mutations_are_rejected() {
    let Ok(corpus) = Corpus::load(&repository_root()) else {
        panic!("published corpus failed to load");
    };
    let canonical = &corpus.valid_codec[0].bytes;
    let mut appended = canonical.clone();
    appended.push(0);
    let mut decoder = Decoder::new(&appended, 0);
    assert!(decoder.u64().is_ok());
    assert_eq!(
        decoder.finish().map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::TrailingBytes))
    );
    for length in 0..canonical.len() {
        let mut decoder = Decoder::new(&canonical[..length], 0);
        assert_eq!(
            decoder.u64().map_err(|error| error.result),
            Err(ResultCode::from(KnownResult::Truncated))
        );
    }
    assert_eq!(
        check_ordered_keys(&[b"b", b"a"]).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::UnsortedSequence))
    );
    assert_eq!(
        check_ordered_keys(&[b"a", b"a"]).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::UnsortedSequence))
    );
    let mut indefinite = Decoder::new(&[u8::MAX; 4], 0);
    assert_eq!(
        indefinite.bytes(1024).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::LengthLimit))
    );

    let template = &corpus.replay.canonical_activities[0];
    let mut unknown_version = template.clone();
    unknown_version[1] = 2;
    let mut unknown_activity = template.clone();
    unknown_activity[14..18].copy_from_slice(&0x0009_0001_u32.to_be_bytes());
    let mut unknown_field = template.clone();
    unknown_field[5] = 13;
    assert_eq!(
        decode_signed(&unknown_version, &registry()).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::VersionUnsupported))
    );
    assert_eq!(
        decode_signed(&unknown_activity, &registry()).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::UnknownActivity))
    );
    assert_eq!(
        decode_signed(&unknown_field, &registry()).map_err(|error| error.result),
        Err(ResultCode::from(KnownResult::UnknownField))
    );
}

#[test]
fn every_declared_bound_accepts_exactly_maximum_and_rejects_one_more() {
    for maximum in [32_usize, 64, 128, 255, 1024, 524_288, 1_048_576] {
        assert!(enforce(maximum, maximum, 7).is_ok());
        assert_eq!(
            enforce(maximum + 1, maximum, 7),
            Err(WireError {
                result: KnownResult::LengthLimit.into(),
                offset: 7,
            })
        );
    }
    for maximum in [4_usize, 32, 64, 256, 512, 65_535] {
        let prefix = u32::try_from(maximum).unwrap_or_default().to_be_bytes();
        let input: Vec<_> = prefix
            .into_iter()
            .chain(std::iter::repeat_n(0, maximum))
            .collect();
        let mut decoder = Decoder::new(&input, maximum);
        assert_eq!(
            decoder.bytes_owned(maximum).map(|bytes| bytes.len()),
            Ok(maximum)
        );

        let over = u32::try_from(maximum + 1).unwrap_or_default().to_be_bytes();
        let mut decoder = Decoder::new(&over, maximum);
        assert_eq!(
            decoder.bytes_owned(maximum).map_err(|error| error.result),
            Err(ResultCode::from(KnownResult::LengthLimit))
        );
        assert_eq!(decoder.allocated(), 0);
    }
}

#[test]
fn hostile_lengths_are_constant_work_and_allocation_bounded() {
    let cases = 100_000;
    let mut completed = 0;
    for _ in 0..cases {
        let mut decoder = Decoder::new(&[u8::MAX; 4], 16);
        assert!(decoder.bytes_owned(16).is_err());
        assert_eq!(decoder.allocated(), 0);
        assert_eq!(decoder.offset(), 4);
        completed += 1;
    }
    assert_eq!(completed, cases);
}

#[test]
fn rejection_taxonomy_coverage_has_no_silent_gap() {
    let mut covered = rejection_corpus();
    covered.extend([
        KnownResult::NonCanonical.raw(),
        KnownResult::UnknownField.raw(),
        KnownResult::VersionUnsupported.raw(),
        KnownResult::UnknownActivity.raw(),
    ]);
    let required = BTreeSet::from([
        KnownResult::Truncated.raw(),
        KnownResult::TrailingBytes.raw(),
        KnownResult::NonCanonical.raw(),
        KnownResult::UnsortedSequence.raw(),
        KnownResult::LengthLimit.raw(),
        KnownResult::InvalidTag.raw(),
        KnownResult::UnknownField.raw(),
        KnownResult::VersionUnsupported.raw(),
        KnownResult::UnknownActivity.raw(),
    ]);
    assert_eq!(covered, required);
}
