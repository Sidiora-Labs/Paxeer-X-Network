use std::path::Path;
use std::process::Command;

const COMPLETE: &str =
    "MIGRATION_FUNDED_OPERATOR_COMPLETE ethereum=1 solana=1 funded=2 pending=1 skipped=0";

fn refusal(arguments: &[&str], code: &str) {
    let output = Command::new(env!("CARGO_BIN_EXE_layerx-migrate"))
        .args(arguments)
        .output()
        .unwrap_or_else(|_| panic!("real migration operator unavailable"));
    assert_eq!(output.status.code(), Some(1));
    let response: serde_json::Value = serde_json::from_slice(&output.stdout)
        .unwrap_or_else(|_| panic!("operator refusal is not typed JSON"));
    assert_eq!(response["ok"], false);
    assert_eq!(response["error"]["code"], code);
    assert!(output.stderr.is_empty());
    assert!(!String::from_utf8_lossy(&output.stdout).contains("private-fixture-marker"));
}

#[test]
fn funded_operator_refuses_malformed_order_before_source_io() {
    refusal(
        &["migrate-asset", "--order-digest", "private-fixture-marker"],
        "invalid_evidence",
    );
}

#[test]
fn funded_operator_refuses_duplicate_parameters() {
    refusal(
        &["migrate-asset", "--chain", "ethereum", "--chain", "solana"],
        "configuration_invalid",
    );
}

#[test]
fn funded_operator_refuses_caller_supplied_customer_identity() {
    for field in ["--principal", "--identity"] {
        refusal(
            &["migrate-asset", field, "private-fixture-marker"],
            "configuration_invalid",
        );
    }
}

#[test]
fn funded_operator_refuses_receipt_and_credit_authority_overrides() {
    for field in ["--receipt", "--layerx-receipt", "--layerx-credit-verified"] {
        refusal(
            &["migrate-asset", field, "private-fixture-marker"],
            "configuration_invalid",
        );
    }
}

#[test]
fn funded_operator_refuses_missing_required_contract() {
    refusal(&["migrate-asset"], "configuration_invalid");
}

#[test]
fn funded_operator_runs_real_gateway_source_and_independent_funded_receipts() {
    let fixture = std::env::var_os("LAYERX_MIGRATION_FUNDED_OPERATOR_FIXTURE")
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| panic!("genuine protected funded operator fixture required"));
    assert!(
        Path::new(&fixture).is_absolute(),
        "absolute owner fixture required"
    );
    let root = Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap_or_else(|| panic!("repository root unavailable"));
    let output = Command::new("python3")
        .arg(root.join("tools/qualification/paxeer-x/migration-funded-operator.py"))
        .arg("--corpus")
        .current_dir(root)
        .env("LAYERX_MIGRATION_FUNDED_OPERATOR_FIXTURE", fixture)
        .env("LAYERX_MIGRATE_BIN", env!("CARGO_BIN_EXE_layerx-migrate"))
        .env("LAYERX_MIGRATION_FUNDED_OPERATOR_RUST_LAUNCH", "1")
        .output()
        .unwrap_or_else(|_| panic!("real funded operator corpus unavailable"));
    println!(
        "MIGRATION_FUNDED_OPERATOR_SUBPROCESS exit={}",
        output
            .status
            .code()
            .map_or_else(|| "signal".to_owned(), |code| code.to_string())
    );
    assert_eq!(
        output.status.code(),
        Some(0),
        "real funded operator corpus refused"
    );
    let report = std::str::from_utf8(&output.stdout)
        .unwrap_or_else(|_| panic!("funded operator report encoding refused"));
    assert_eq!(
        report.lines().filter(|line| *line == COMPLETE).count(),
        1,
        "complete genuine Ethereum/Solana funded coverage required"
    );
    let records: Vec<&str> = report
        .lines()
        .filter(|line| line.starts_with("PAXEER_X_GATE "))
        .collect();
    assert_eq!(records.len(), 1, "one actual coverage record required");
    let count = records[0]
        .strip_prefix("PAXEER_X_GATE tests=")
        .and_then(|value| value.strip_suffix(" skipped=0"))
        .and_then(|value| value.parse::<u64>().ok())
        .filter(|value| *value >= 21)
        .unwrap_or_else(|| panic!("complete nonzero real cases with no skips required"));
    println!("{COMPLETE}");
    println!("MIGRATION_FUNDED_OPERATOR_CORPUS tests={count} skipped=0");
}
