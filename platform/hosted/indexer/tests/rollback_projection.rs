//! Rollback restores the canonical asset and account projections of a real
//! file-backed SQLite store: orphaned supply metadata and orphan-only
//! discovery disappear, configured records survive with their own
//! provenance, a replacement branch without a supply update derives the last
//! surviving canonical value, everything holds across a database reopen and
//! rollback below the finalized boundary stays refused.

use std::path::{Path, PathBuf};

use layerx_indexer::layerx::{decode_batch, CHAIN};
use layerx_indexer::store::{AssetRow, Store, TransferRow, Unit};
use layerx_indexer::IndexError;
use serde_json::{json, Value};

const SENDER: &str = "3aa29bcf27f39c8bcfe4017be09686802ed23412631c141902d715255ec8acba";
const RECEIVER: &str = "29b27231b5c9eb1fee8193f1334c6a44c34f68f20c38f905001fca9f8ce6b553";
const SEND_ASSET: &str = "cd4caf041d0f03a1f172d10218e890ede6e1f93dfa8702c43f25e56c4c2fcdb5";
const SEND_ACTIVITY: &str = "a1f5977a9aa9cb2d167a315c9a8769083dfa9a68225c757e7e96beef275662e4";
const ORPHAN_ACCOUNT: &str = "5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e";
const POINTER: &str = "0x00000000000000000000000000000000000000e5";

struct Scratch(PathBuf);

impl Scratch {
    fn new(name: &str) -> Self {
        let path = std::env::temp_dir().join(format!(
            "layerx-indexer-rollback-projection-{}-{name}",
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

fn open(path: &Path) -> Store {
    Store::open(path).unwrap_or_else(|error| panic!("{error}"))
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

fn pointer() -> AssetRow {
    AssetRow {
        asset: format!("evm:{POINTER}"),
        chain: "paxeer".to_owned(),
        kind: "pointer".to_owned(),
        address: Some(POINTER.to_owned()),
        denom: Some("upax".to_owned()),
        metadata: json!({}),
    }
}

fn asset(store: &Store, id: &str) -> Option<Value> {
    store.asset(id).unwrap_or_else(|error| panic!("{error}"))
}

fn listed(store: &Store) -> Vec<String> {
    store
        .assets(None, 500)
        .unwrap_or_else(|error| panic!("{error}"))
        .items
        .iter()
        .filter_map(|item| item["asset"].as_str().map(str::to_owned))
        .collect()
}

fn known(store: &Store, chain: &str, account: &str) -> bool {
    store
        .account_known(chain, account)
        .unwrap_or_else(|error| panic!("{error}"))
}

fn position(store: &Store, chain: &str) -> Option<(u64, String, Option<u64>)> {
    store
        .cursor(chain)
        .unwrap_or_else(|error| panic!("{error}"))
        .map(|cursor| (cursor.position, cursor.hash, cursor.finalized_position))
}

fn activity_count(store: &Store, tx_id: &str) -> (usize, usize) {
    let transfers = store
        .transfers(CHAIN)
        .unwrap_or_else(|error| panic!("{error}"));
    let events = store
        .events(CHAIN)
        .unwrap_or_else(|error| panic!("{error}"));
    (
        transfers.iter().filter(|row| row["tx_id"] == tx_id).count(),
        events.iter().filter(|row| row["tx_id"] == tx_id).count(),
    )
}

fn assert_configured_pointer(store: &Store) {
    let pointer = asset(store, &format!("evm:{POINTER}"))
        .unwrap_or_else(|| panic!("the configured pointer must survive"));
    assert_eq!(pointer["kind"], "pointer");
    assert_eq!(pointer["configured"], true);
    assert_eq!(pointer["configured_metadata"], json!({}));
    assert_eq!(pointer["denom"], "upax");
    assert!(store
        .is_pointer(POINTER)
        .unwrap_or_else(|error| panic!("{error}")));
}

/// The canonical LayerX fixture batch 3 carries the only receipt naming
/// `SEND_ASSET`, with a supply transition. Rolling it back must remove the
/// asset, its supply metadata and the account only that batch discovered,
/// while the configured pointer and the surviving accounts stay. The
/// replacement branch carries no supply update and must not resurrect it.
#[test]
fn rolling_back_a_supply_bearing_receipt_restores_the_canonical_projection() {
    let scratch = Scratch::new("orphan");
    let relay = relay();
    let store = open(&scratch.database());
    store
        .register_assets(&[pointer()])
        .unwrap_or_else(|error| panic!("{error}"));

    let mut orphan_document = relay["canonical"]["batches"]["3"].clone();
    orphan_document["activities"][0]["accounts"]
        .as_array_mut()
        .unwrap_or_else(|| panic!("batch 3 activity has accounts"))
        .push(json!(ORPHAN_ACCOUNT));
    let orphan = decode_batch(&orphan_document).unwrap_or_else(|error| panic!("{error}"));
    for unit in [
        batch(&relay, "canonical", 1),
        batch(&relay, "canonical", 2),
        orphan,
    ] {
        store
            .commit(&unit, 1)
            .unwrap_or_else(|error| panic!("{error}"));
    }

    let observed = asset(&store, SEND_ASSET).unwrap_or_else(|| panic!("asset discovered"));
    assert_eq!(observed["configured"], false);
    assert_eq!(observed["metadata_provenance"], "observed");
    assert_eq!(observed["metadata"]["supply_sequence"], "5");
    assert!(observed["metadata"]["supply"].is_string());
    assert_eq!(observed["observed_metadata"], observed["metadata"]);
    assert_eq!(
        observed["observed_at"],
        json!({"chain": CHAIN, "position": "3"})
    );
    assert_eq!(observed["configured_metadata"], Value::Null);
    assert!(known(&store, CHAIN, ORPHAN_ACCOUNT));
    let (legs, events) = activity_count(&store, SEND_ACTIVITY);
    assert_eq!(legs, 2);
    assert!(events >= 1);
    assert_configured_pointer(&store);

    store
        .rollback(CHAIN, Some(2))
        .unwrap_or_else(|error| panic!("{error}"));

    assert_eq!(asset(&store, SEND_ASSET), None);
    assert!(!listed(&store).contains(&SEND_ASSET.to_owned()));
    assert!(!known(&store, CHAIN, ORPHAN_ACCOUNT));
    assert!(known(&store, CHAIN, RECEIVER));
    assert!(known(&store, CHAIN, SENDER));
    assert_eq!(activity_count(&store, SEND_ACTIVITY), (0, 0));
    assert_configured_pointer(&store);
    let canonical_two = relay["canonical"]["batches"]["2"]["batch_id"]
        .as_str()
        .unwrap_or_default()
        .to_owned();
    assert_eq!(
        position(&store, CHAIN).map(|(position, hash, _)| (position, hash)),
        Some((2, canonical_two))
    );

    for number in [3, 4] {
        store
            .commit(&batch(&relay, "reorg", number), 1)
            .unwrap_or_else(|error| panic!("{error}"));
    }
    assert_eq!(asset(&store, SEND_ASSET), None);
    assert!(!known(&store, CHAIN, ORPHAN_ACCOUNT));
    let before_reopen = (
        listed(&store),
        store
            .transfers(CHAIN)
            .unwrap_or_else(|error| panic!("{error}")),
        store
            .events(CHAIN)
            .unwrap_or_else(|error| panic!("{error}")),
        position(&store, CHAIN),
        store
            .link(CHAIN, 4)
            .unwrap_or_else(|error| panic!("{error}")),
    );
    drop(store);

    let store = open(&scratch.database());
    let after_reopen = (
        listed(&store),
        store
            .transfers(CHAIN)
            .unwrap_or_else(|error| panic!("{error}")),
        store
            .events(CHAIN)
            .unwrap_or_else(|error| panic!("{error}")),
        position(&store, CHAIN),
        store
            .link(CHAIN, 4)
            .unwrap_or_else(|error| panic!("{error}")),
    );
    assert_eq!(before_reopen, after_reopen);
    let reorg_four = relay["reorg"]["batches"]["4"]["batch_id"]
        .as_str()
        .unwrap_or_default()
        .to_owned();
    assert_eq!(after_reopen.3, Some((4, reorg_four, Some(3))));
    assert_eq!(asset(&store, SEND_ASSET), None);
    assert!(!known(&store, CHAIN, ORPHAN_ACCOUNT));
    assert!(known(&store, CHAIN, RECEIVER));
    assert_configured_pointer(&store);

    for fork in [Some(2), None] {
        match store.rollback(CHAIN, fork) {
            Err(IndexError::ReorgBeyondFinality { source, .. }) => assert_eq!(source, CHAIN),
            other => panic!("rollback to {fork:?} below finality must be refused, got {other:?}"),
        }
    }
    assert_eq!(listed(&store), after_reopen.0);
    assert_eq!(
        store
            .transfers(CHAIN)
            .unwrap_or_else(|error| panic!("{error}")),
        after_reopen.1
    );
    assert_eq!(position(&store, CHAIN), after_reopen.3);
    assert!(known(&store, CHAIN, RECEIVER));
}

const MINTED: &str = "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a";
const ORPHAN_ASSET: &str = "9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b";
const HOLDER: &str = "1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c";

fn layerx_asset(asset: &str, supply: Option<(u64, u64)>) -> AssetRow {
    AssetRow {
        asset: asset.to_owned(),
        chain: CHAIN.to_owned(),
        kind: "layerx".to_owned(),
        address: None,
        denom: None,
        metadata: supply.map_or_else(
            || json!({}),
            |(after, sequence)| {
                json!({ "supply": after.to_string(), "supply_sequence": sequence.to_string() })
            },
        ),
    }
}

fn layerx_unit(
    position: u64,
    hash: &str,
    parent: &str,
    assets: Vec<AssetRow>,
    account: &str,
) -> Unit {
    Unit {
        chain: CHAIN.to_owned(),
        position,
        hash: hash.to_owned(),
        parent: parent.to_owned(),
        link: hash.to_owned(),
        boundary: position * 10,
        transfers: assets
            .iter()
            .map(|asset| TransferRow {
                height_or_seq: position * 10,
                kind: "lxp_credit".to_owned(),
                direction: "in",
                account: account.to_owned(),
                counterparty: None,
                asset: asset.asset.clone(),
                amount: "1".to_owned(),
                tx_id: format!("{hash}-{}", asset.asset),
                ordinal: 0,
                decoded: json!({}),
            })
            .collect(),
        assets,
        ..Unit::default()
    }
}

fn supply_of(store: &Store, id: &str) -> Value {
    asset(store, id).unwrap_or_else(|| panic!("{id} indexed"))["observed_metadata"].clone()
}

/// A configured asset whose supply units observe keeps configured metadata
/// apart from the observed value; the observed value follows the newest
/// surviving canonical unit through rollback, a supply-free replacement
/// branch and a reopen, and a full rollback leaves only the configured
/// record.
#[test]
fn a_supply_free_replacement_branch_derives_the_last_surviving_canonical_metadata() {
    let scratch = Scratch::new("replacement");
    let store = open(&scratch.database());
    let configured = AssetRow {
        metadata: json!({ "symbol": "MNT", "decimals": 6 }),
        ..layerx_asset(MINTED, None)
    };
    store
        .register_assets(&[configured])
        .unwrap_or_else(|error| panic!("{error}"));
    let declared = asset(&store, MINTED).unwrap_or_else(|| panic!("configured"));
    assert_eq!(declared["metadata_provenance"], "configured");
    assert_eq!(
        declared["metadata"],
        json!({ "symbol": "MNT", "decimals": 6 })
    );
    assert_eq!(declared["observed_metadata"], json!({}));
    assert_eq!(declared["observed_at"], Value::Null);

    let depth = 100;
    store
        .commit(
            &layerx_unit(
                1,
                "b1",
                "b0",
                vec![layerx_asset(MINTED, Some((100, 10)))],
                HOLDER,
            ),
            depth,
        )
        .unwrap_or_else(|error| panic!("{error}"));
    store
        .commit(
            &layerx_unit(
                2,
                "b2",
                "b1",
                vec![layerx_asset(MINTED, Some((150, 20)))],
                HOLDER,
            ),
            depth,
        )
        .unwrap_or_else(|error| panic!("{error}"));
    store
        .commit(
            &layerx_unit(
                3,
                "b3",
                "b2",
                vec![
                    layerx_asset(MINTED, Some((900, 30))),
                    layerx_asset(ORPHAN_ASSET, Some((5, 30))),
                ],
                ORPHAN_ACCOUNT,
            ),
            depth,
        )
        .unwrap_or_else(|error| panic!("{error}"));
    let orphaned = asset(&store, MINTED).unwrap_or_else(|| panic!("indexed"));
    assert_eq!(
        orphaned["metadata"],
        json!({"supply": "900", "supply_sequence": "30"})
    );
    assert_eq!(orphaned["metadata_provenance"], "observed");
    assert_eq!(
        orphaned["configured_metadata"],
        json!({ "symbol": "MNT", "decimals": 6 })
    );
    assert!(asset(&store, ORPHAN_ASSET).is_some());
    assert!(known(&store, CHAIN, ORPHAN_ACCOUNT));

    store
        .rollback(CHAIN, Some(2))
        .unwrap_or_else(|error| panic!("{error}"));
    let expected_two = json!({"supply": "150", "supply_sequence": "20"});
    assert_eq!(supply_of(&store, MINTED), expected_two);
    assert_eq!(asset(&store, ORPHAN_ASSET), None);
    assert!(!known(&store, CHAIN, ORPHAN_ACCOUNT));

    store
        .commit(
            &layerx_unit(3, "c3", "b2", vec![layerx_asset(MINTED, None)], HOLDER),
            depth,
        )
        .unwrap_or_else(|error| panic!("{error}"));
    let replaced = asset(&store, MINTED).unwrap_or_else(|| panic!("indexed"));
    assert_eq!(replaced["metadata"], expected_two);
    assert_eq!(
        replaced["observed_at"],
        json!({"chain": CHAIN, "position": "2"})
    );
    assert_eq!(replaced["transfer_legs"], "3");
    drop(store);

    let store = open(&scratch.database());
    let reopened = asset(&store, MINTED).unwrap_or_else(|| panic!("indexed"));
    assert_eq!(reopened["metadata"], expected_two);
    assert_eq!(reopened["configured"], true);
    assert_eq!(
        reopened["configured_metadata"],
        json!({ "symbol": "MNT", "decimals": 6 })
    );
    assert_eq!(reopened["transfer_legs"], "3");
    assert_eq!(asset(&store, ORPHAN_ASSET), None);
    assert_eq!(
        position(&store, CHAIN).map(|(p, h, _)| (p, h)),
        Some((3, "c3".to_owned()))
    );

    store
        .rollback(CHAIN, Some(1))
        .unwrap_or_else(|error| panic!("{error}"));
    assert_eq!(
        supply_of(&store, MINTED),
        json!({"supply": "100", "supply_sequence": "10"})
    );
    assert!(known(&store, CHAIN, HOLDER));

    store
        .rollback(CHAIN, None)
        .unwrap_or_else(|error| panic!("{error}"));
    let bare = asset(&store, MINTED).unwrap_or_else(|| panic!("configured assets survive"));
    assert_eq!(bare["observed_metadata"], json!({}));
    assert_eq!(bare["observed_at"], Value::Null);
    assert_eq!(bare["metadata_provenance"], "configured");
    assert_eq!(bare["metadata"], json!({ "symbol": "MNT", "decimals": 6 }));
    assert_eq!(bare["transfer_legs"], "0");
    assert!(!known(&store, CHAIN, HOLDER));
    assert_eq!(position(&store, CHAIN), None);
}
