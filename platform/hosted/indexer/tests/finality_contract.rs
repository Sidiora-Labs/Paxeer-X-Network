//! The published finality contract of a real file-backed store: local
//! reversible-depth stability is reported with its evidence source and never
//! as LayerX settlement; without verified checkpoint evidence bound to a
//! receipt every row stays settlement-unverified, through depth advances,
//! a zero challenge window, rollback and a database reopen. Settlement is
//! classified only through the canonical verifier: absent trust or evidence
//! is unverified, contradicting evidence is invalid, and rollback and reopen
//! never keep a stronger claim than the persisted record.

use std::path::{Path, PathBuf};

use layerx_client::evidence::{EvidenceError, FinalityEvidenceCandidate};
use layerx_client::handover::decode_finality_policy;
use layerx_indexer::api;
use layerx_indexer::config::{
    finality_depth_from_challenge_window, Config, SettlementTrust, SETTLEMENT_VARIABLES,
};
use layerx_indexer::layerx::{decode_batch, settle_batch, CHAIN};
use layerx_indexer::settlement::{SettlementLevel, SettlementRefusal};
use layerx_indexer::store::{
    settlement_reason, SettlementRecord, Store, Unit, SETTLEMENT_EVIDENCE_ABSENT,
    SETTLEMENT_EVIDENCE_MALFORMED, SETTLEMENT_OUTSIDE_AUTHORIZATION, SETTLEMENT_SOURCE,
    SETTLEMENT_TRUST_UNCONFIGURED, SETTLEMENT_UNAVAILABLE_REASON, SETTLEMENT_UNVERIFIED,
    SETTLEMENT_VERIFIED_REASON, STABILITY_SOURCE,
};
use layerx_indexer::IndexError;
use layerx_paxeer_verifier::{EndpointFailure, EndpointFault, PaxeerCheckpointVerifier};
use layerx_proof::inclusion::{InclusionError, SequencerAuthorization};
use layerx_proof::merkle::{build_proof, encode_proof, MerkleError};
use layerx_wire::receipt::decode_batch_header;
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

const FINALITY_VECTOR: &str = include_str!("../../../../tests/vectors/finality_evidence_v1.vec");

fn vector_field(name: &str) -> &'static str {
    FINALITY_VECTOR
        .lines()
        .filter_map(|line| line.split_once('='))
        .find(|(key, _)| *key == name)
        .map_or_else(
            || panic!("finality vector lacks {name}"),
            |(_, value)| value,
        )
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

/// The recorded CP1/CX1 pair, its certified header and that header's batch.
struct Recorded {
    checkpoint: String,
    context: String,
    header: Vec<u8>,
    batch_number: u64,
    sequencer_id: [u8; 32],
    protocol_version: u16,
    network_id: u32,
}

fn recorded() -> Recorded {
    let protocol_version: u16 = vector_field("protocol_version")
        .parse()
        .unwrap_or_else(|error| panic!("protocol version: {error}"));
    let network_id: u32 = vector_field("network_id")
        .parse()
        .unwrap_or_else(|error| panic!("network id: {error}"));
    let checkpoint = vector_field("checkpoint_payload").to_owned();
    let context = vector_field("finality_proof").to_owned();
    let decode = |text: &str| {
        (0..text.len())
            .step_by(2)
            .map(|index| {
                u8::from_str_radix(&text[index..index + 2], 16)
                    .unwrap_or_else(|error| panic!("vector hex: {error}"))
            })
            .collect::<Vec<u8>>()
    };
    let candidate = FinalityEvidenceCandidate::from_exact_bytes(
        decode(&checkpoint),
        decode(&context),
        protocol_version,
        network_id,
    )
    .unwrap_or_else(|error| panic!("recorded CP1/CX1 refused: {error:?}"));
    let header = candidate.canonical_header().to_vec();
    let decoded =
        decode_batch_header(&header).unwrap_or_else(|error| panic!("header refused: {error:?}"));
    Recorded {
        checkpoint,
        context,
        batch_number: decoded.batch_number(),
        sequencer_id: decoded.sequencer_id(),
        header,
        protocol_version,
        network_id,
    }
}

/// Settlement trust pinned to the recorded vector's network. Every case
/// below is refused by local checkpoint, header or authorisation checks
/// before the publication endpoint would be contacted.
fn vector_trust(recorded: &Recorded, first: u64, last: u64) -> SettlementTrust {
    let policy = format!(
        "version=1\nurl=http://127.0.0.1:18546\ntransport=local-emulator\ntrust_anchor_der=\nchain_id=125\nrequest_timeout_ms=8000\nregistry={}\nguarantor_bond={}\nprotocol_version={}\nnetwork_id={}\ncanonical_genesis_root={}\nconfirmations=2\n",
        "12".repeat(20),
        "34".repeat(20),
        recorded.protocol_version,
        recorded.network_id,
        "56".repeat(32)
    );
    let policy = decode_finality_policy(policy.as_bytes())
        .unwrap_or_else(|error| panic!("policy: {error:?}"));
    SettlementTrust {
        authorization: SequencerAuthorization::new(recorded.sequencer_id, [0x5a; 32], first, last),
        verifier: PaxeerCheckpointVerifier::new(policy)
            .unwrap_or_else(|error| panic!("verifier: {error:?}")),
    }
}

fn document(relay: &Value, view: &str, number: u64) -> Value {
    relay[view]["batches"][number.to_string()].clone()
}

/// Adds the recorded CP1/CX1 evidence and a canonically encoded single-leaf
/// proof to a recorded relay batch in the fields the relay serves them in.
/// `signed_header_hex` is set to the batch's 32-byte header identity, which
/// is not the header the checkpoint certifies. The proof is not an
/// inclusion proof for the receipt: every case using it is refused by the
/// checkpoint or header binding first, or by inclusion.
fn with_evidence(mut document: Value, recorded: &Recorded) -> Value {
    let identity = document["header_hex"].clone();
    document["signed_header_hex"] = identity;
    document["checkpoint_hex"] = Value::from(recorded.checkpoint.clone());
    document["context_hex"] = Value::from(recorded.context.clone());
    if let Some(activities) = document["activities"].as_array_mut() {
        for activity in activities {
            let receipt = activity["receipt_hex"]
                .as_str()
                .unwrap_or_default()
                .to_owned();
            let (proof, _) = build_proof(&[receipt.as_bytes()], 0)
                .unwrap_or_else(|error| panic!("proof: {error:?}"));
            activity["receipt_proof_hex"] = Value::from(hex(&encode_proof(&proof)));
        }
    }
    document
}

fn records(document: &Value, trust: Option<&SettlementTrust>) -> (Unit, Vec<SettlementRecord>) {
    let unit = decode_batch(document).unwrap_or_else(|error| panic!("{error}"));
    let records = settle_batch(document, &unit, trust);
    let mut transfers: Vec<&str> = unit
        .transfers
        .iter()
        .map(|row| row.tx_id.as_str())
        .collect();
    transfers.sort_unstable();
    transfers.dedup();
    assert_eq!(records.len(), transfers.len(), "one record per receipt");
    (unit, records)
}

/// The first recorded canonical batch that carries transfer rows.
fn transfer_batch(relay: &Value) -> u64 {
    (1..=3)
        .find(|number| !batch(relay, "canonical", *number).transfers.is_empty())
        .unwrap_or_else(|| panic!("the fixture indexes transfer rows"))
}

fn assert_all(records: &[SettlementRecord], level: SettlementLevel, reason: &str) {
    assert!(!records.is_empty());
    for record in records {
        assert_eq!(record.level(), level, "{record:?}");
        assert_eq!(record.reason(), reason, "{record:?}");
        assert!(record.source().is_none(), "{record:?}");
    }
}

fn get(store: &Store, target: &str) -> (u16, Value) {
    let response = api::route(store, "GET", target);
    let body = serde_json::from_str(&response.body).unwrap_or_else(|error| panic!("{error}"));
    (response.status, body)
}

/// Every row and its settlement document publish exactly the stored level.
fn assert_published(store: &Store, expected: &[SettlementRecord]) {
    for record in expected {
        let (status, body) = get(store, &format!("/v1/settlement/{}", record.tx_id()));
        assert_eq!(status, 200, "{body}");
        let settlement = &body["receipt"]["settlement"];
        assert_eq!(settlement["level"], record.level().as_str(), "{body}");
        assert_eq!(settlement["reason"], record.reason(), "{body}");
        assert_eq!(settlement["source"], Value::Null, "{body}");
        assert_eq!(body["receipt"]["stability"]["source"], STABILITY_SOURCE);
        for row in transfers(store)
            .iter()
            .filter(|row| row["tx_id"] == record.tx_id())
        {
            assert_eq!(&row["settlement"], settlement, "{row}");
            let (status, history) = get(
                store,
                &format!(
                    "/v1/history/{}",
                    row["account"].as_str().unwrap_or_default()
                ),
            );
            assert_eq!(status, 200);
            let items = history["items"].as_array().cloned().unwrap_or_default();
            assert!(items
                .iter()
                .any(|item| item["tx_id"] == record.tx_id() && item["settlement"] == *settlement));
        }
    }
}

#[test]
fn unconfigured_trust_and_absent_evidence_stay_unverified() {
    let relay = relay();
    let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    let (unit, unconfigured) =
        records(&document(&relay, "canonical", transfer_batch(&relay)), None);
    assert_all(
        &unconfigured,
        SettlementLevel::Unverified,
        SETTLEMENT_TRUST_UNCONFIGURED,
    );
    store
        .commit_settled(&unit, 0, &unconfigured)
        .unwrap_or_else(|error| panic!("{error}"));
    assert_published(&store, &unconfigured);

    let recorded = recorded();
    let trust = vector_trust(&recorded, 1, u64::MAX);
    let (_, absent) = records(
        &document(&relay, "canonical", transfer_batch(&relay)),
        Some(&trust),
    );
    assert_all(
        &absent,
        SettlementLevel::Unverified,
        SETTLEMENT_EVIDENCE_ABSENT,
    );

    let mut malformed = with_evidence(
        document(&relay, "canonical", transfer_batch(&relay)),
        &recorded,
    );
    malformed["signature_hex"] = Value::from("zz");
    let (_, malformed) = records(&malformed, Some(&trust));
    assert_all(
        &malformed,
        SettlementLevel::Unverified,
        SETTLEMENT_EVIDENCE_MALFORMED,
    );

    let outside = vector_trust(&recorded, 1_000_000, 1_000_001);
    let (_, outside) = records(
        &with_evidence(
            document(&relay, "canonical", transfer_batch(&relay)),
            &recorded,
        ),
        Some(&outside),
    );
    assert_all(
        &outside,
        SettlementLevel::Unverified,
        SETTLEMENT_OUTSIDE_AUTHORIZATION,
    );

    let (status, body) = get(&store, &format!("/v1/settlement/{}", "ab".repeat(32)));
    assert_eq!(
        (status, body["error"]["code"].clone()),
        (404, Value::from("settlement_not_found"))
    );
    let (status, _) = get(
        &store,
        &format!("/v1/settlement/{}?x=1", unconfigured[0].tx_id()),
    );
    assert_eq!(status, 400);
}

#[test]
fn contradicting_evidence_is_invalid_and_never_verified() {
    let relay = relay();
    let recorded = recorded();
    let trust = vector_trust(&recorded, 1, u64::MAX);
    let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    let evidenced = with_evidence(
        document(&relay, "canonical", transfer_batch(&relay)),
        &recorded,
    );
    let (unit, foreign_header) = records(&evidenced, Some(&trust));
    assert_all(
        &foreign_header,
        SettlementLevel::Invalid,
        "header_not_checkpointed",
    );

    let mut corrupt = evidenced.clone();
    let checkpoint = format!("ff{}", &recorded.checkpoint[2..]);
    corrupt["checkpoint_hex"] = Value::from(checkpoint);
    let (_, corrupt) = records(&corrupt, Some(&trust));
    assert_all(&corrupt, SettlementLevel::Invalid, "checkpoint_rejected");

    let mut context = evidenced.clone();
    context["context_hex"] = Value::from(format!("ff{}", &recorded.context[2..]));
    let (_, context) = records(&context, Some(&trust));
    assert_all(&context, SettlementLevel::Invalid, "checkpoint_rejected");

    let mut certified = evidenced;
    certified["signed_header_hex"] = Value::from(hex(&recorded.header));
    let (_, certified) = records(&certified, Some(&trust));
    let expected = if recorded.batch_number == unit.position {
        "receipt_not_included"
    } else {
        "batch_mismatch"
    };
    assert_all(&certified, SettlementLevel::Invalid, expected);

    store
        .commit_settled(&unit, 0, &foreign_header)
        .unwrap_or_else(|error| panic!("{error}"));
    assert_published(&store, &foreign_header);
    for row in transfers(&store) {
        assert_ne!(row["settlement"]["level"], "verified", "{row}");
        assert_eq!(
            row["stability"]["level"], "depth_stable",
            "depth never settles: {row}"
        );
    }
}

#[test]
fn every_refusal_publishes_its_level_and_reason() {
    let failure = EndpointFailure {
        url: "http://127.0.0.1:18546".to_owned(),
        fault: EndpointFault::Connect {
            detail: "refused".to_owned(),
        },
    };
    let cases = [
        (
            SettlementRefusal::Checkpoint(EvidenceError::Malformed),
            SettlementLevel::Invalid,
            "checkpoint_rejected",
        ),
        (
            SettlementRefusal::HeaderNotCheckpointed,
            SettlementLevel::Invalid,
            "header_not_checkpointed",
        ),
        (
            SettlementRefusal::BatchMismatch {
                indexed: 1,
                checkpointed: 2,
            },
            SettlementLevel::Invalid,
            "batch_mismatch",
        ),
        (
            SettlementRefusal::ProofEncoding(MerkleError::Encoding),
            SettlementLevel::Invalid,
            "receipt_proof_malformed",
        ),
        (
            SettlementRefusal::Inclusion(InclusionError::BatchNumber),
            SettlementLevel::Invalid,
            "receipt_not_included",
        ),
        (
            SettlementRefusal::ReceiptDecode,
            SettlementLevel::Invalid,
            "receipt_undecodable",
        ),
        (
            SettlementRefusal::ReceiptShape,
            SettlementLevel::Invalid,
            "receipt_not_protocol",
        ),
        (
            SettlementRefusal::ProtocolVersion,
            SettlementLevel::Invalid,
            "protocol_version_mismatch",
        ),
        (
            SettlementRefusal::SequenceRange,
            SettlementLevel::Invalid,
            "sequence_outside_header",
        ),
        (
            SettlementRefusal::PublicationUnconfigured,
            SettlementLevel::Unverified,
            "publication_verifier_unconfigured",
        ),
        (
            SettlementRefusal::Publication(failure),
            SettlementLevel::Unverified,
            "publication_unestablished",
        ),
        (
            SettlementRefusal::PublicationBinding(EvidenceError::Malformed),
            SettlementLevel::Invalid,
            "publication_binding_mismatch",
        ),
    ];
    let relay = relay();
    let unit = batch(&relay, "canonical", transfer_batch(&relay));
    let transfer = unit
        .transfers
        .first()
        .unwrap_or_else(|| panic!("fixture transfer"));
    for (refusal, level, reason) in cases {
        assert_eq!(refusal.level(), level, "{refusal:?}");
        assert_eq!(settlement_reason(&refusal), reason, "{refusal:?}");
        let record =
            SettlementRecord::refused(transfer.tx_id.clone(), transfer.height_or_seq, &refusal);
        assert_ne!(record.level(), SettlementLevel::Verified);
        let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
        store
            .commit_settled(&unit, 1, &[record.clone()])
            .unwrap_or_else(|error| panic!("{error}"));
        assert_published(&store, &[record]);
    }
    assert_eq!(SettlementLevel::Verified.as_str(), "verified");
    assert_ne!(SETTLEMENT_VERIFIED_REASON, SETTLEMENT_UNAVAILABLE_REASON);
    assert_ne!(SETTLEMENT_SOURCE, STABILITY_SOURCE);

    let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    let stray = SettlementRecord::unavailable(
        "cd".repeat(32),
        transfer.height_or_seq,
        SETTLEMENT_EVIDENCE_ABSENT,
    );
    assert!(
        store.commit_settled(&unit, 1, &[stray]).is_err(),
        "a record must name a receipt of the unit"
    );
    assert!(
        transfers(&store).is_empty(),
        "a refused commit writes nothing"
    );
}

#[test]
fn rollback_and_reopen_never_retain_a_stronger_settlement() {
    let relay = relay();
    let recorded = recorded();
    let trust = vector_trust(&recorded, 1, u64::MAX);
    let scratch = Scratch::new("settlement");
    let store = Store::open(&scratch.database()).unwrap_or_else(|error| panic!("{error}"));
    let mut committed = Vec::new();
    for number in 1..=3 {
        let (unit, settled) = records(
            &with_evidence(document(&relay, "canonical", number), &recorded),
            Some(&trust),
        );
        store
            .commit_settled(&unit, 1, &settled)
            .unwrap_or_else(|error| panic!("{error}"));
        committed.push((unit, settled));
    }
    let all: Vec<SettlementRecord> = committed
        .iter()
        .flat_map(|(_, settled)| settled.clone())
        .collect();
    assert_published(&store, &all);

    store
        .rollback(CHAIN, Some(2))
        .unwrap_or_else(|error| panic!("{error}"));
    let boundary = store
        .link(CHAIN, 2)
        .unwrap_or_else(|error| panic!("{error}"))
        .unwrap_or_else(|| panic!("link 2"))
        .boundary;
    let (kept, dropped): (Vec<_>, Vec<_>) = all
        .iter()
        .cloned()
        .partition(|record| record.height_or_seq() <= boundary);
    assert_published(&store, &kept);
    for record in &dropped {
        let (status, _) = get(&store, &format!("/v1/settlement/{}", record.tx_id()));
        assert_eq!(
            status, 404,
            "rollback drops the settlement with its receipt"
        );
    }

    let mut replaced = Vec::new();
    for number in [3, 4] {
        let (unit, settled) = records(&document(&relay, "reorg", number), None);
        assert_all(
            &settled,
            SettlementLevel::Unverified,
            SETTLEMENT_TRUST_UNCONFIGURED,
        );
        store
            .commit_settled(&unit, 1, &settled)
            .unwrap_or_else(|error| panic!("{error}"));
        replaced.extend(settled);
    }
    assert_published(&store, &replaced);
    let rows = transfers(&store);
    drop(store);

    let reopened = Store::open(&scratch.database()).unwrap_or_else(|error| panic!("{error}"));
    assert_eq!(transfers(&reopened), rows);
    assert_published(&reopened, &kept);
    assert_published(&reopened, &replaced);
    for row in &rows {
        assert_ne!(row["settlement"]["level"], "verified", "{row}");
    }
}

fn protected(scratch: &Scratch, name: &str, bytes: &[u8], mode: u32) -> String {
    use std::os::unix::fs::PermissionsExt as _;
    let path = scratch.0.join(name);
    std::fs::write(&path, bytes).unwrap_or_else(|error| panic!("{error}"));
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(mode))
        .unwrap_or_else(|error| panic!("{error}"));
    std::fs::canonicalize(&path)
        .unwrap_or_else(|error| panic!("{error}"))
        .display()
        .to_string()
}

fn configure(extra: &[(&'static str, String)]) -> Result<Config, IndexError> {
    let mut values = vec![
        ("LAYERX_INDEXER_DB", "/tmp/indexer.sqlite".to_owned()),
        (
            "LAYERX_INDEXER_RELAY_URL",
            "http://127.0.0.1:7000".to_owned(),
        ),
    ];
    values.extend_from_slice(extra);
    Config::from_lookup(&|name: &str| {
        values
            .iter()
            .find(|(key, _)| *key == name)
            .map(|(_, value)| value.clone())
    })
}

#[test]
fn settlement_trust_configuration_refuses_partial_or_unprotected_pins() {
    let config = configure(&[]).unwrap_or_else(|error| panic!("{error}"));
    assert!(config
        .layerx
        .unwrap_or_else(|| panic!("layerx"))
        .settlement
        .is_none());

    let scratch = Scratch::new("trust");
    let recorded = recorded();
    let policy = format!(
        "version=1\nurl=http://127.0.0.1:18546\ntransport=local-emulator\ntrust_anchor_der=\nchain_id=125\nrequest_timeout_ms=8000\nregistry={}\nguarantor_bond={}\nprotocol_version={}\nnetwork_id={}\ncanonical_genesis_root={}\nconfirmations=2\n",
        "12".repeat(20), "34".repeat(20), recorded.protocol_version, recorded.network_id, "56".repeat(32)
    );
    let policy_path = protected(&scratch, "finality.policy", policy.as_bytes(), 0o600);
    let open_policy = protected(&scratch, "open.policy", policy.as_bytes(), 0o644);
    let genesis = protected(
        &scratch,
        "genesis.trust",
        b"not a genesis trust artifact",
        0o600,
    );
    let full = |policy: &str, genesis: &str, id: String| {
        vec![
            (SETTLEMENT_VARIABLES[0], genesis.to_owned()),
            (SETTLEMENT_VARIABLES[1], policy.to_owned()),
            (SETTLEMENT_VARIABLES[2], id),
            (SETTLEMENT_VARIABLES[3], "5a".repeat(32)),
            (SETTLEMENT_VARIABLES[4], "1".to_owned()),
            (SETTLEMENT_VARIABLES[5], "100".to_owned()),
        ]
    };
    let derived = layerx_wire::handover::sequencer_id(&[0x5a; 32])
        .map_or_else(|_| panic!("sequencer id"), |id| hex(&id));
    let complete = full(&policy_path, &genesis, derived.clone());
    for missing in 0..SETTLEMENT_VARIABLES.len() {
        let mut partial = complete.clone();
        partial.remove(missing);
        let error = configure(&partial)
            .map(|_| ())
            .err()
            .unwrap_or_else(|| panic!("partial trust accepted"));
        assert!(
            error.to_string().contains(SETTLEMENT_VARIABLES[missing]),
            "{error}"
        );
    }
    for (label, values) in [
        (
            "underived sequencer id",
            full(&policy_path, &genesis, "77".repeat(32)),
        ),
        (
            "relative policy path",
            full("finality.policy", &genesis, derived.clone()),
        ),
        (
            "group-readable policy",
            full(&open_policy, &genesis, derived.clone()),
        ),
        (
            "unrecorded genesis artifact",
            full(&policy_path, &genesis, derived.clone()),
        ),
    ] {
        assert!(configure(&values).is_err(), "{label} accepted");
    }
}

/// The positive contrast: a real producer receipt with its inclusion proof,
/// signed header and CP1/CX1 checkpoint, verified against a live Paxeer
/// publication. Its inputs are the recorded producer batch document named
/// by `PAXEER_X_SETTLEMENT_PRODUCER_BATCH` and the six settlement trust
/// variables; both are required, never skipped.
#[test]
fn producer_receipt_with_published_checkpoint_is_verified() {
    let path = std::env::var("PAXEER_X_SETTLEMENT_PRODUCER_BATCH").unwrap_or_else(|_| {
        panic!("awaited input: PAXEER_X_SETTLEMENT_PRODUCER_BATCH (recorded producer batch with checkpoint_hex, context_hex and receipt_proof_hex)")
    });
    let text = std::fs::read_to_string(&path).unwrap_or_else(|error| panic!("{path}: {error}"));
    let producer: Value =
        serde_json::from_str(&text).unwrap_or_else(|error| panic!("{path}: {error}"));
    let trust = SettlementTrust::from_lookup(&|name: &str| std::env::var(name).ok())
        .unwrap_or_else(|error| panic!("{error}"))
        .unwrap_or_else(|| panic!("awaited input: {}", SETTLEMENT_VARIABLES.join(", ")));
    let (unit, settled) = records(&producer, Some(&trust));
    assert!(!settled.is_empty());
    let store = Store::open_in_memory().unwrap_or_else(|error| panic!("{error}"));
    store
        .commit_settled(&unit, u64::MAX, &settled)
        .unwrap_or_else(|error| panic!("{error}"));
    for record in &settled {
        assert_eq!(record.level(), SettlementLevel::Verified, "{record:?}");
        let source = record.source().unwrap_or_else(|| panic!("verified source"));
        assert_eq!(source.evidence, SETTLEMENT_SOURCE);
        assert_eq!(source.batch_number, unit.position.to_string());
        let (status, body) = get(&store, &format!("/v1/settlement/{}", record.tx_id()));
        assert_eq!(status, 200);
        let settlement = &body["receipt"]["settlement"];
        assert_eq!(settlement["level"], "verified", "{body}");
        assert_eq!(settlement["reason"], SETTLEMENT_VERIFIED_REASON, "{body}");
        assert_eq!(
            settlement["source"]["checkpoint_id"],
            source.checkpoint_id.as_str()
        );
        assert_eq!(
            body["receipt"]["stability"]["level"], "reversible",
            "depth stays separate: {body}"
        );
    }
}
