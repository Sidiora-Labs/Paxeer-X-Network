//! The published finality contract of a real file-backed store: local
//! reversible-depth stability is reported with its evidence source and never
//! as LayerX settlement; without verified checkpoint evidence bound to a
//! receipt every row stays settlement-unverified, through depth advances,
//! a zero challenge window, rollback and a database reopen.

use std::path::{Path, PathBuf};

use layerx_indexer::config::finality_depth_from_challenge_window;
use layerx_indexer::layerx::{decode_batch, CHAIN};
use layerx_indexer::store::{
    Store, Unit, SETTLEMENT_UNAVAILABLE_REASON, SETTLEMENT_UNVERIFIED, STABILITY_SOURCE,
};
use serde_json::Value;

struct Scratch(PathBuf);

impl Scratch {
    fn new(name: &str) -> Self {
        let path = std::env::temp_dir().join(format!(
            "layerx-indexer-finality-contract-{}-{name}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&path);
        std::fs::create_dir_all(&path)
            .unwrap_or_else(|error| panic!("{}: {error}", path.display()));
        Self(path)
    }

    fn database(&self) -> PathBuf {
        self.0.join("index.sqlite")
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

fn relay() -> Value {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("fixtures")
        .join("relay_archive_batches.json");
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|error| panic!("{}: {error}", path.display()));
    serde_json::from_str(&text).unwrap_or_else(|error| panic!("{}: {error}", path.display()))
}

fn batch(relay: &Value, view: &str, number: u64) -> Unit {
    decode_batch(&relay[view]["batches"][number.to_string()])
        .unwrap_or_else(|error| panic!("{view} batch {number}: {error}"))
}

fn transfers(store: &Store) -> Vec<Value> {
    store
        .transfers(CHAIN)
        .unwrap_or_else(|error| panic!("{error}"))
}

fn boundary(store: &Store) -> Option<u64> {
    store
        .cursor(CHAIN)
        .unwrap_or_else(|error| panic!("{error}"))
        .and_then(|cursor| cursor.finalized_boundary)
}

/// Every row publishes depth stability from the local finality boundary and
/// an unverified settlement level; returns the stable flags in row order.
fn assert_contract(store: &Store) -> Vec<bool> {
    let boundary = boundary(store);
    transfers(store)
        .iter()
        .map(|row| {
            let position: u64 = row["height_or_seq"]
                .as_str()
                .and_then(|text| text.parse().ok())
                .unwrap_or_else(|| panic!("row position: {row}"));
            let stable = boundary.is_some_and(|boundary| position <= boundary);
            assert_eq!(row["final"], stable, "{row}");
            assert_eq!(row["final_basis"], STABILITY_SOURCE, "{row}");
            assert_eq!(row["stability"]["source"], STABILITY_SOURCE, "{row}");
            assert_eq!(
                row["stability"]["level"],
                if stable { "depth_stable" } else { "reversible" },
                "{row}"
            );
            assert_eq!(
                row["stability"]["finalized_boundary"],
                boundary.map_or(Value::Null, |boundary| Value::from(boundary.to_string())),
                "{row}"
            );
            assert_eq!(row["settlement"]["level"], SETTLEMENT_UNVERIFIED, "{row}");
            assert_eq!(row["settlement"]["source"], Value::Null, "{row}");
            assert_eq!(
                row["settlement"]["reason"], SETTLEMENT_UNAVAILABLE_REASON,
                "{row}"
            );
            stable
        })
        .collect()
}

#[test]
fn depth_and_challenge_window_never_label_settlement() {
    let relay = relay();
    let windowed = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    let depth = finality_depth_from_challenge_window(0, 1_000);
    for number in 1..=3 {
        windowed
            .commit(&batch(&relay, "canonical", number), depth)
            .unwrap_or_else(|error| panic!("{error}"));
        assert_contract(&windowed);
    }
    let head = assert_contract(&windowed);
    assert!(!head.is_empty(), "the fixture indexes transfer rows");
    assert!(
        head.iter().all(|stable| !*stable),
        "head rows stay reversible"
    );

    let immediate = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    for number in 1..=3 {
        immediate
            .commit(&batch(&relay, "canonical", number), 0)
            .unwrap_or_else(|error| panic!("{error}"));
    }
    let stable = assert_contract(&immediate);
    assert!(!stable.is_empty());
    assert!(
        stable.iter().all(|stable| *stable),
        "depth 0 is locally stable"
    );
}

#[test]
fn rollback_and_restart_keep_provenance_without_stronger_claims() {
    let relay = relay();
    let scratch = Scratch::new("rollback");
    let store = Store::open(&scratch.database()).unwrap_or_else(|error| panic!("{error}"));
    for number in 1..=3 {
        store
            .commit(&batch(&relay, "canonical", number), 1)
            .unwrap_or_else(|error| panic!("{error}"));
    }
    let before = assert_contract(&store);
    assert!(!before.is_empty(), "the fixture indexes transfer rows");
    assert!(
        before.iter().all(|stable| !*stable),
        "head rows stay reversible"
    );
    let boundary_before = boundary(&store);

    store
        .rollback(CHAIN, Some(2))
        .unwrap_or_else(|error| panic!("{error}"));
    let after_rollback = assert_contract(&store);
    assert_eq!(boundary(&store), boundary_before);
    assert!(after_rollback.len() <= before.len());
    assert!(
        store.rollback(CHAIN, Some(0)).is_err(),
        "rollback below the local finality boundary is refused"
    );

    for number in [3, 4] {
        store
            .commit(&batch(&relay, "reorg", number), 1)
            .unwrap_or_else(|error| panic!("{error}"));
    }
    let replaced = assert_contract(&store);
    let rows = transfers(&store);
    drop(store);

    let reopened = Store::open(&scratch.database()).unwrap_or_else(|error| panic!("{error}"));
    assert_eq!(transfers(&reopened), rows);
    assert_eq!(assert_contract(&reopened), replaced);
}
