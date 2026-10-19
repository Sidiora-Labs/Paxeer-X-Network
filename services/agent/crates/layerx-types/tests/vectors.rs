use std::fs;
use std::path::{Path, PathBuf};

use layerx_types::vectors::{
    coverage_report, Corpus, CorpusError, DERIVED_PROTOCOL_VERSION, TYPE_PROTOCOL_VERSIONS,
};

fn repository_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../..")
}

fn copy_corpora(target: &Path) {
    let source = repository_root().join("tests/vectors");
    assert!(fs::create_dir_all(target.join("tests/vectors/codec")).is_ok());
    for relative in [
        "codec/valid.lxv",
        "codec/adversarial.lxv",
        "replay_corpus.lxb",
        "qualification_replay_10m.digest",
    ] {
        assert!(fs::copy(
            source.join(relative),
            target.join("tests/vectors").join(relative)
        )
        .is_ok());
    }
}

fn temporary_repository(name: &str) -> PathBuf {
    let path = std::env::temp_dir().join(format!("layerx-vectors-{name}-{}", std::process::id()));
    let _ = fs::remove_dir_all(&path);
    copy_corpora(&path);
    path
}

#[test]
fn repository_corpora_are_loaded_and_fully_accounted() {
    let Ok(corpus) = Corpus::load(&repository_root()) else {
        panic!("published corpora failed to load");
    };
    assert_eq!(corpus.valid_codec.len(), 4);
    assert_eq!(corpus.adversarial_codec.len(), 7);
    assert_eq!(corpus.replay.protocol_version, DERIVED_PROTOCOL_VERSION);
    assert_eq!(corpus.replay.activity_count, 53);
    assert_eq!(corpus.replay.activity_types.len(), 53);
    assert_eq!(corpus.qualification_digests.len(), 9);
    assert!(TYPE_PROTOCOL_VERSIONS
        .iter()
        .all(|(_, version)| *version == DERIVED_PROTOCOL_VERSION));

    let Ok(report) = coverage_report(&corpus) else {
        panic!("published vector class was silently unused");
    };
    assert_eq!(report.exercised.len(), 10);
    assert!(report.unused.is_empty());
    assert_eq!(report.render().lines().count(), 10);
}

#[test]
fn unsupported_corpus_version_is_build_breaking() {
    let repository = temporary_repository("version");
    let replay_path = repository.join("tests/vectors/replay_corpus.lxb");
    let Ok(mut replay) = fs::read(&replay_path) else {
        panic!("copied replay corpus unreadable");
    };
    replay[11] = 2;
    assert!(fs::write(&replay_path, replay).is_ok());
    let result = Corpus::load(&repository);
    let _ = fs::remove_dir_all(&repository);
    assert!(matches!(
        result,
        Err(CorpusError::UnsupportedVersion {
            declared: 2,
            supported: DERIVED_PROTOCOL_VERSION,
            ..
        })
    ));
}

#[test]
fn unknown_vector_class_names_the_missing_capability() {
    let repository = temporary_repository("class");
    let valid_path = repository.join("tests/vectors/codec/valid.lxv");
    let Ok(mut valid) = fs::read_to_string(&valid_path) else {
        panic!("copied valid corpus unreadable");
    };
    valid.push_str("future-structure|future-case|00|-1|-\n");
    assert!(fs::write(&valid_path, valid).is_ok());
    let result = Corpus::load(&repository);
    let _ = fs::remove_dir_all(&repository);
    assert_eq!(
        result,
        Err(CorpusError::MissingCapability {
            vector: "future-case".to_owned(),
            capability: "future-structure".to_owned(),
        })
    );
}
