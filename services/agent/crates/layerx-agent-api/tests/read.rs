use std::collections::BTreeMap;
use std::error::Error;

use layerx_agent_api::availability::{
    AvailabilityClass, AvailabilityCompletion, AvailabilityReport, ClassReport, ProviderRef,
    ProviderReport,
};
use layerx_agent_api::export::{FactRef, OfflineExport};
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::proof::ProofBundle;
use layerx_agent_api::read::{
    AccountRef, AccountValue, BalanceValue, BatchRef, BatchValue, CheckpointRef, CheckpointValue,
    Freshness, HistoryValue, ModuleStateValue, ProjectionResult, RelativeTo, VerifiedRead,
};
use layerx_agent_api::verify::Level;
use layerx_agent_api::{Amount, Sequence};
use layerx_types::json::{self, JsonValue};

const SCHEMA: &str = include_str!("../../../schema/agent-api/read.kvx");

fn required<T>(result: Result<T, layerx_agent_api::identity::ContractError>) -> T {
    result.unwrap_or_else(|error| panic!("valid contract value: {error:?}"))
}

fn bytes(value: u8) -> CanonicalBytes {
    required(CanonicalBytes::new(vec![value]))
}

fn freshness() -> Freshness {
    Freshness {
        chain_head: Sequence(50),
        latest_sealed_batch: required(BatchRef::new("batch-4")),
        latest_finalised_checkpoint: required(CheckpointRef::new("checkpoint-3")),
        value_sequence: Sequence(47),
        relative_to: RelativeTo::Batch(required(BatchRef::new("batch-4"))),
    }
}

fn assert_verified<T: layerx_agent_api::read::CoreProduced>(value: &VerifiedRead<T>) {
    assert_eq!(value.achieved_verification_level, Level::StateProven);
    assert_eq!(value.freshness.chain_head, Sequence(50));
    assert_eq!(value.freshness.latest_sealed_batch.as_str(), "batch-4");
    assert_eq!(
        value.freshness.latest_finalised_checkpoint.as_str(),
        "checkpoint-3"
    );
}

#[test]
fn every_authoritative_read_shape_carries_level_and_freshness() {
    let level = Level::StateProven;
    let values = (
        VerifiedRead::new(AccountValue(bytes(1)), level, freshness()),
        VerifiedRead::new(ModuleStateValue(bytes(2)), level, freshness()),
        VerifiedRead::new(
            HistoryValue {
                records: vec![bytes(3)],
                next_cursor: None,
            },
            level,
            freshness(),
        ),
        VerifiedRead::new(BatchValue(bytes(4)), level, freshness()),
        VerifiedRead::new(CheckpointValue(bytes(5)), level, freshness()),
        VerifiedRead::new(
            ProofBundle {
                target: bytes(6),
                proofs: vec![bytes(7)],
            },
            level,
            freshness(),
        ),
    );
    assert_verified(&values.0);
    assert_verified(&values.1);
    assert_verified(&values.2);
    assert_verified(&values.3);
    assert_verified(&values.4);
    assert_verified(&values.5);
    assert!(SCHEMA.contains("required = [\"value\",\"achieved_verification_level\",\"freshness\"]"));
}

#[test]
fn balance_preserves_identity_amount_sequence_and_reference() {
    let balance = VerifiedRead::new(
        BalanceValue {
            account: required(AccountRef::new("account-1")),
            asset: required(layerx_agent_api::identity::Asset::new("LXP")),
            amount: Amount(9_007_199_254_740_993),
            canonical_state: bytes(8),
        },
        Level::StateProven,
        freshness(),
    );
    assert_eq!(balance.value.amount, Amount(9_007_199_254_740_993));
    assert_eq!(balance.freshness.value_sequence, Sequence(47));
    assert_verified(&balance);
}

#[test]
fn availability_reports_complete_provider_or_attributed_partials() {
    let provider = required(ProviderRef::new("provider-a"));
    let class = ClassReport {
        class: AvailabilityClass::Recovery,
        complete: false,
        verified_chunks: 2,
        verified_bytes: 128,
        failure: Some("withheld final chunk".to_owned()),
    };
    let report = AvailabilityReport {
        completion: AvailabilityCompletion::Partial,
        classes: vec![class.clone()],
        providers: vec![ProviderReport {
            provider,
            classes: vec![class],
            failure: Some("incomplete".to_owned()),
        }],
    };
    let read = VerifiedRead::new(report, Level::BatchIncluded, freshness());
    assert!(matches!(
        read.value.completion,
        AvailabilityCompletion::Partial
    ));
    assert_eq!(read.value.providers.len(), 1);
    assert!(SCHEMA.contains("bytes are never merged across providers"));
}

#[test]
fn offline_export_is_self_contained_and_projection_is_distinct() {
    let export = OfflineExport {
        facts: vec![required(FactRef::new("fact-1"))],
        receipts: vec![bytes(9)],
        proofs: vec![bytes(10)],
        certificates: vec![bytes(11)],
        headers: vec![bytes(12)],
    }
    .validate()
    .unwrap_or_else(|error| panic!("offline export: {error:?}"));
    let verified = VerifiedRead::new(export, Level::CheckpointFinalised, freshness());
    assert_eq!(verified.value.facts.len(), 1);

    let projection = ProjectionResult::new(Amount(22), "fee estimate only", freshness())
        .unwrap_or_else(|error| panic!("projection: {error:?}"));
    assert_eq!(projection.projected, Amount(22));
    assert!(SCHEMA.contains("forbidden_response = \"VerifiedRead\""));
    assert!(SCHEMA.contains("layerx-proof_without_daemon_or_network"));
}

fn schema_declarations() -> Result<BTreeMap<String, JsonValue>, Box<dyn Error>> {
    let mut section = "";
    let mut entries = BTreeMap::new();
    for raw in SCHEMA.lines() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|value| value.strip_suffix(']'))
        {
            section = name;
            continue;
        }
        assert!(!section.is_empty(), "schema declaration outside a section");
        let (key, value) = line
            .split_once('=')
            .ok_or("schema declaration missing equals")?;
        let path = format!("{section}.{}", key.trim());
        let value = json::parse(value.trim())?;
        assert!(
            entries.insert(path.clone(), value).is_none(),
            "duplicate schema declaration {path}"
        );
    }
    Ok(entries)
}

fn schema_list<'a>(entries: &'a BTreeMap<String, JsonValue>, key: &str) -> Vec<&'a str> {
    let JsonValue::Array(values) = &entries[key] else {
        panic!("schema declaration {key} must be an array");
    };
    values
        .iter()
        .map(|value| {
            let JsonValue::String(text) = value else {
                panic!("schema declaration {key} must contain strings");
            };
            text.as_str()
        })
        .collect()
}

fn schema_text<'a>(entries: &'a BTreeMap<String, JsonValue>, key: &str) -> &'a str {
    let JsonValue::String(value) = &entries[key] else {
        panic!("schema declaration {key} must be text");
    };
    value
}

#[test]
fn history_cursor_triple() -> Result<(), Box<dyn Error>> {
    let entries = schema_declarations()?;
    assert_eq!(entries["module.contract_major"], JsonValue::Integer(2));
    assert_eq!(
        schema_list(&entries, "type.HistoryCursor.fields"),
        ["blockHeight", "txIndex", "logIndex"]
    );
    assert_eq!(
        schema_text(&entries, "type.HistoryCursor.encoding"),
        "ordered_json_array"
    );
    assert_eq!(
        schema_text(&entries, "type.HistoryCursor.integer_encoding"),
        "canonical_decimal_string"
    );
    assert_eq!(
        schema_text(&entries, "type.HistoryCursor.integer_type"),
        "u64"
    );
    assert_eq!(entries["type.HistoryCursor.arity"], JsonValue::Integer(3));
    assert_eq!(entries["type.HistoryCursor.minimum"], JsonValue::Integer(0));
    assert_eq!(
        schema_text(&entries, "type.HistoryCursor.maximum").parse::<u64>()?,
        u64::MAX
    );

    let response = json::parse(r#"{"cursor":["9007199254740993","18446744073709551615","0"]}"#)?;
    assert_eq!(response.array_at("cursor")?.len(), 3);
    let decoded: Vec<u64> = (0..3)
        .map(|index| {
            Ok(response
                .str_at(&format!("cursor.{index}"))?
                .parse::<u64>()?)
        })
        .collect::<Result<_, Box<dyn Error>>>()?;
    assert_eq!(decoded, [9_007_199_254_740_993, u64::MAX, 0]);

    for key in [
        "operation.read.history.cursor_text",
        "operation.read.history.response_cursor",
    ] {
        assert!(
            schema_text(&entries, key).contains("(blockHeight, txIndex, logIndex)"),
            "{key}"
        );
        assert!(!schema_text(&entries, key).contains("112"), "{key}");
    }
    assert_eq!(
        schema_list(&entries, "type.HistoryCursor.refused"),
        [
            "legacy_kernel_cursor",
            "wrong_arity",
            "negative",
            "fraction",
            "exponent",
            "leading_zero",
            "embedded_whitespace",
            "overflow",
        ]
    );
    Ok(())
}

#[test]
fn proof_bundle_v2_fields() -> Result<(), Box<dyn Error>> {
    let entries = schema_declarations()?;
    let common = [
        "format",
        "verificationLevel",
        "rawTransaction",
        "signedReceipt",
        "receiptDigest",
        "receiptKey",
    ];
    let committed = ["blockHeader", "receiptInclusion", "commit", "validatorSet"];
    assert_eq!(
        schema_text(&entries, "type.ProofBundle.format"),
        "layerx-receipt-proof-v2"
    );
    assert_eq!(schema_list(&entries, "type.ProofBundle.required"), common);
    assert_eq!(
        schema_list(&entries, "operation.read.proof_bundle.response_required"),
        common
    );
    assert_eq!(
        schema_list(&entries, "type.ProofBundle.required_committed"),
        committed
    );
    assert_eq!(
        schema_list(
            &entries,
            "operation.read.proof_bundle.response_committed_required"
        ),
        committed
    );
    assert_eq!(
        schema_list(&entries, "type.ProofBundle.optional"),
        ["accountProof"]
    );
    assert_eq!(
        schema_list(&entries, "type.ReceiptKey.required"),
        ["keyId", "publicKey", "firstHeight", "lastHeight"]
    );
    for field in ["blockHeader", "commit", "validatorSet"] {
        assert!(schema_text(&entries, &format!("type.ProofBundle.{field}"))
            .contains("CometBFT protobuf"));
    }
    assert!(schema_text(&entries, "type.ProofBundle.receiptInclusion")
        .contains("EVM receipt-trie proof"));
    assert!(schema_text(&entries, "type.ProofBundle.rawTransaction").contains("keccak256"));
    assert!(
        schema_text(&entries, "type.ProofBundle.signedReceipt").contains("byte-exact re-encode")
    );
    assert!(!entries
        .keys()
        .any(|key| key.starts_with("type.ProofBundleRecord.")));
    assert!(!SCHEMA.contains("PB1"));
    Ok(())
}

#[test]
fn proof_bundle_requires_verified_rungs_and_independent_authority() -> Result<(), Box<dyn Error>> {
    let entries = schema_declarations()?;
    assert_eq!(
        schema_text(
            &entries,
            "operation.read.proof_bundle.requested_verification_level"
        ),
        "Rung"
    );
    assert_eq!(
        schema_list(&entries, "operation.read.proof_bundle.requested_rungs"),
        ["submitted", "receipted", "committed"]
    );
    assert_eq!(
        schema_list(&entries, "operation.read.proof_bundle.achieved_rungs"),
        ["receipted", "committed"]
    );
    assert_eq!(
        schema_list(&entries, "type.ProofBundle.verificationLevel"),
        ["receipted", "committed"]
    );
    assert!(
        schema_text(&entries, "operation.read.proof_bundle.level_rule")
            .contains("refused, never downgraded")
    );
    assert!(
        schema_text(&entries, "operation.read.proof_bundle.authority")
            .contains("independently pinned ReceiptKeys")
    );
    assert!(
        schema_text(&entries, "operation.read.proof_bundle.authority")
            .contains("never an independent trust anchor")
    );
    assert!(
        schema_text(&entries, "operation.read.proof_bundle.ownership")
            .contains("before and after acquisition")
    );
    assert!(
        schema_text(&entries, "operation.read.proof_bundle.ownership")
            .contains("public knowledge of a transaction hash grants no access")
    );
    assert!(
        schema_text(&entries, "type.ProofBundle.commit").contains("strictly more than two thirds")
    );
    assert!(
        schema_text(&entries, "type.ProofBundle.consumer_verification")
            .contains("status-0 receipt is a receipted revert")
    );
    let bound = schema_text(&entries, "operation.read.proof_bundle.size_bound");
    assert!(bound.contains("262144"));
    assert!(bound.contains("1048576"));
    for removed in [
        "SequencerSigned",
        "BatchIncluded",
        "StateProven",
        "CheckpointFinalised",
        "SettlementAnchored",
    ] {
        assert!(
            !SCHEMA.contains(removed),
            "removed level remains: {removed}"
        );
    }
    Ok(())
}

#[test]
fn reads_carry_height() -> Result<(), Box<dyn Error>> {
    let entries = schema_declarations()?;
    for operation in [
        "read.balance",
        "read.account",
        "read.module_state",
        "read.history",
        "read.batch",
        "read.checkpoint",
        "availability.fetch",
        "export.offline",
    ] {
        let prefix = format!("operation.{operation}");
        assert_eq!(
            schema_text(&entries, &format!("{prefix}.height")),
            "canonical_decimal_string u64",
            "{operation}"
        );
        assert!(
            schema_list(&entries, &format!("{prefix}.response_required")).contains(&"height"),
            "{operation}"
        );
    }
    assert_eq!(
        schema_list(&entries, "type.Freshness.required"),
        ["chain_head", "value_height", "relative_to"]
    );
    assert_eq!(
        schema_text(&entries, "type.Freshness.value_height"),
        "canonical_decimal_string u64 block height"
    );
    assert_eq!(
        schema_text(&entries, "operation.project.height"),
        "observed_freshness.value_height"
    );
    assert!(schema_list(&entries, "type.HistoryItem.required").contains(&"height"));
    assert!(schema_list(&entries, "type.HistoryItem.required").contains(&"txIndex"));
    assert!(schema_list(&entries, "type.HistoryItem.required").contains(&"logIndex"));
    assert!(!entries.keys().any(|key| key.ends_with("sequence")));
    assert!(!SCHEMA.contains("value_sequence"));
    assert!(!SCHEMA.contains("global_sequence"));
    assert!(!SCHEMA.contains("snapshot_sequence"));
    Ok(())
}

#[test]
fn fee_projection_names_payer_denom_and_snapshot_height() -> Result<(), Box<dyn Error>> {
    let entries = schema_declarations()?;
    let fields = schema_list(&entries, "type.FeeProjection.required");
    for field in ["fee", "gas_units", "denom", "snapshot_height"] {
        assert!(fields.contains(&field), "projection missing {field}");
    }
    assert_eq!(
        schema_text(&entries, "operation.project.fee_denom"),
        "projected.denom"
    );
    assert_eq!(
        schema_text(&entries, "type.FeeProjection.fee"),
        "decimal_string u128, exact"
    );
    assert_eq!(
        schema_text(&entries, "type.FeeProjection.gas_units"),
        "canonical_decimal_string u64"
    );
    assert!(schema_text(&entries, "type.FeeProjection.denom").contains("sponsor denomination"));
    assert!(schema_text(&entries, "type.FeeProjection.snapshot_height")
        .contains("observed_freshness.value_height"));
    assert_eq!(
        schema_text(&entries, "operation.project.verification_status"),
        "A projection claims no transaction rung."
    );
    assert_eq!(
        schema_text(&entries, "operation.project.forbidden_response"),
        "VerifiedRead"
    );
    Ok(())
}
