use std::process::Command;

fn refusal(arguments: &[&str], code: &str) {
    let output = Command::new(env!("CARGO_BIN_EXE_layerx-migrate"))
        .args(arguments)
        .output()
        .unwrap_or_else(|error| panic!("real CLI invocation failed: {error}"));
    assert_eq!(output.status.code(), Some(1));
    let response: serde_json::Value = serde_json::from_slice(&output.stdout)
        .unwrap_or_else(|error| panic!("typed CLI refusal is malformed: {error}"));
    assert_eq!(response["ok"], false);
    assert_eq!(response["error"]["code"], code);
    assert!(output.stderr.is_empty());
    assert!(!String::from_utf8_lossy(&output.stdout).contains("private-fixture-marker"));
}

#[test]
fn operator_refuses_unknown_commands_before_opening_source_configuration() {
    refusal(
        &[
            "mint",
            "--chain",
            "ethereum",
            "--config",
            "/private-fixture-marker",
        ],
        "configuration_invalid",
    );
}

#[test]
fn operator_refuses_duplicate_parameters_without_network_io() {
    refusal(
        &["verify-asset", "--chain", "ethereum", "--chain", "solana"],
        "configuration_invalid",
    );
}

#[test]
fn operator_refuses_receipt_or_credit_substitution_flags() {
    refusal(
        &["verify-asset", "--receipt", "private-fixture-marker"],
        "configuration_invalid",
    );
    refusal(
        &["verify-asset", "--principal", "private-fixture-marker"],
        "configuration_invalid",
    );
}

#[test]
fn operator_refuses_unknown_source_network() {
    refusal(
        &["verify-asset", "--chain", "private-fixture-marker"],
        "invalid_network",
    );
}
