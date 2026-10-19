use std::path::PathBuf;

use layerx_indexer::codec::unhex;
use layerx_indexer::layerx::{decode_batch, CHAIN};
use layerx_indexer::settlement::{SettlementFailure, SettlementSource};
use layerx_indexer::store::Store;
use serde_json::Value;

#[test]
fn real_receipt_settlement_preserves_depth_refusal_restart_and_rollback() {
    let path = std::env::var("LAYERX_INDEXER_FINALITY_BATCH")
        .expect("LAYERX_INDEXER_FINALITY_BATCH must name a genuine published archive batch with a transfer, matching the configured real LNI and Paxeer publication");
    let document: Value =
        serde_json::from_slice(&std::fs::read(path).expect("read real archive batch"))
            .expect("decode real archive batch JSON");
    let unit = decode_batch(&document).expect("production receipt decoder");
    let transfer = unit
        .transfers
        .first()
        .expect("real published batch must contain a transfer");
    let activity = document["activities"]
        .as_array()
        .expect("archive activities")
        .iter()
        .find(|activity| activity["activity_id"].as_str() == Some(transfer.tx_id.as_str()))
        .expect("transfer activity exists");
    let receipt = unhex(
        activity["receipt_hex"]
            .as_str()
            .expect("canonical archive receipt"),
    )
    .expect("decode canonical receipt hex");

    let depth = Store::open_in_memory().expect("real SQLite store");
    depth.commit(&unit, 0).expect("commit depth-only unit");
    let row = depth
        .transfers(CHAIN)
        .expect("read depth-only history")
        .into_iter()
        .find(|row| row["tx_id"] == transfer.tx_id)
        .expect("transfer indexed");
    assert_eq!(row["final"], true);
    assert_eq!(row["final_basis"], "local_finality_depth");
    assert_eq!(row["stability"]["level"], "depth_stable");
    assert_eq!(row["settlement"]["level"], "unverified");

    let mut source = SettlementSource::from_environment()
        .expect("canonical settlement configuration")
        .expect("real LNI and independently pinned Paxeer policy required");
    source
        .prepare_through(unit.position)
        .expect("authenticate contiguous real historical authority and complete availability");
    let verified = source.verify(&receipt, unit.position).expect(
        "real signed header, receipt inclusion, checkpoint context and independent publication",
    );
    assert!(source
        .verify(&receipt, unit.position.checked_add(1).expect("next batch"))
        .is_err());
    let mut malformed = receipt.clone();
    malformed.push(0);
    assert_eq!(
        source.verify(&malformed, unit.position).err(),
        Some(SettlementFailure::Invalid)
    );

    let database: PathBuf = std::env::temp_dir().join(format!(
        "layerx-indexer-finality-{}-{}.sqlite",
        std::process::id(),
        transfer.tx_id
    ));
    assert!(!database.exists(), "qualification database must be fresh");
    {
        let store = Store::open(&database).expect("file-backed real SQLite");
        store
            .commit(
                &unit,
                unit.position.checked_add(1).expect("reversible depth"),
            )
            .expect("commit reversible real batch");
        store
            .record_settlement(&verified)
            .expect("bind closed verified value to indexed receipt");
        let row = store
            .transfers(CHAIN)
            .expect("verified API history")
            .into_iter()
            .find(|row| row["tx_id"] == transfer.tx_id)
            .expect("transfer indexed");
        assert_eq!(row["final"], false);
        assert_eq!(row["settlement"]["level"], "settlement_verified");
        assert_eq!(
            row["settlement"]["source"],
            "native_lni_and_independent_paxeer"
        );
        assert!(row["settlement"]["checkpoint_id"]
            .as_str()
            .is_some_and(|value| value.len() == 64));
        assert!(store
            .record_settlement_failure(&malformed, SettlementFailure::Invalid)
            .is_err());
    }
    {
        let store = Store::open(&database).expect("restart actual SQLite");
        let row = store
            .transfers(CHAIN)
            .expect("restart provenance")
            .into_iter()
            .find(|row| row["tx_id"] == transfer.tx_id)
            .expect("transfer survives");
        assert_eq!(row["settlement"]["level"], "settlement_verified");
        store
            .record_settlement_failure(&receipt, SettlementFailure::Unavailable)
            .expect("unavailable evidence lowers claim");
        let row = store
            .transfers(CHAIN)
            .expect("unavailable claim")
            .into_iter()
            .find(|row| row["tx_id"] == transfer.tx_id)
            .expect("transfer survives");
        assert_eq!(row["settlement"]["level"], "unverified");
        assert_eq!(
            row["settlement"]["reason"],
            "settlement_evidence_unavailable"
        );
        assert_eq!(
            row["settlement"]["previously_verified_evidence"]["level"],
            "settlement_verified"
        );
        assert!(
            row["settlement"]["previously_verified_evidence"]["checkpoint_id"]
                .as_str()
                .is_some_and(|value| value.len() == 64)
        );
        store
            .record_settlement_failure(&receipt, SettlementFailure::Invalid)
            .expect("invalid evidence remains unverified");
        let row = store
            .transfers(CHAIN)
            .expect("invalid claim")
            .into_iter()
            .find(|row| row["tx_id"] == transfer.tx_id)
            .expect("transfer survives");
        assert_eq!(row["settlement"]["reason"], "invalid_settlement_evidence");
        store
            .record_settlement(&verified)
            .expect("restore genuine verified publication");
        store
            .rollback(CHAIN, None)
            .expect("rollback reversible history atomically");
        assert!(store
            .transfers(CHAIN)
            .expect("orphan transfers removed")
            .is_empty());
        assert!(
            store.record_settlement(&verified).is_err(),
            "orphaned evidence cannot be restored without its receipt"
        );
    }
    let store = Store::open(&database).expect("restart rolled-back database");
    assert!(store
        .transfers(CHAIN)
        .expect("rollback survives restart")
        .is_empty());
    drop(store);
    std::fs::remove_file(database).expect("remove owned qualification database");
}
