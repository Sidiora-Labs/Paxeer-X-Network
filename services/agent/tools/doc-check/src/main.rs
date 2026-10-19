use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;

fn declarations(source: &str) -> Result<BTreeMap<String, String>, String> {
    let mut section = String::new();
    let mut values = BTreeMap::new();
    for (line_number, raw) in source.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with("//") {
            continue;
        }
        if let Some(name) = line
            .strip_prefix('[')
            .and_then(|value| value.strip_suffix(']'))
        {
            name.clone_into(&mut section);
            continue;
        }
        let (key, value) = line
            .split_once('=')
            .ok_or_else(|| format!("schema line {} is malformed", line_number + 1))?;
        if section.is_empty() {
            return Err(format!(
                "schema line {} is outside a section",
                line_number + 1
            ));
        }
        values.insert(
            format!("{}.{}", section, key.trim()),
            value.trim().to_owned(),
        );
    }
    Ok(values)
}

fn unquote(value: &str) -> Result<&str, String> {
    value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .ok_or_else(|| format!("expected quoted schema value, got {value}"))
}

fn schema_value<'a>(values: &'a BTreeMap<String, String>, key: &str) -> Result<&'a str, String> {
    values
        .get(key)
        .ok_or_else(|| format!("schema is missing {key}"))
        .and_then(|value| unquote(value))
}

/// Verifies that the published SDK tuple is the exact schema tuple.
///
/// # Errors
///
/// Fails when a version dimension or the offline verification contract drifts.
pub fn agent_sdk_compat_matrix(repository: &Path) -> Result<(), String> {
    let schema_path = repository.join("services/agent/schema/agent-api/v1.kvx");
    let schema = declarations(
        &fs::read_to_string(&schema_path)
            .map_err(|error| format!("could not read {}: {error}", schema_path.display()))?,
    )?;
    let sdk = schema_value(&schema, "compatibility.matrix.sdk")?;
    let contract = schema_value(&schema, "compatibility.matrix.contract")?;
    let node = schema_value(&schema, "compatibility.matrix.node_interface")?;
    let daemon = schema_value(&schema, "compatibility.matrix.daemon")?;
    let matrix_path = repository.join("services/agent/sdk/COMPATIBILITY.md");
    let matrix = fs::read_to_string(&matrix_path)
        .map_err(|error| format!("could not read {}: {error}", matrix_path.display()))?;
    let row = format!("| `{sdk}` | `{contract}` | `{node}` | `{daemon}` | supported |");
    if !matrix.lines().any(|line| line.trim() == row) {
        return Err(format!(
            "SDK compatibility matrix is missing schema row {row}"
        ));
    }
    for required in [
        "schema changes must be additive",
        "requires a contract major-version increment",
        "No daemon, node, network connection or local LayerX database is consulted",
        "layerx-proof --example offline_verify",
        "Verification fails closed",
    ] {
        if !matrix.contains(required) {
            return Err(format!(
                "SDK compatibility publication is missing: {required}"
            ));
        }
    }
    Ok(())
}

fn reject_dishonest_document(path: &Path, source: &str) -> Result<(), String> {
    let lower = source.to_ascii_lowercase();
    for forbidden in [
        "`daemonlimit` | `protocol_enforced`",
        "daemon-enforced restriction is a protocol guarantee",
        "daemon-enforced restrictions are protocol guarantees",
        "bypassing the daemon preserves this limit",
    ] {
        if lower.contains(forbidden) {
            return Err(format!(
                "{} overstates a daemon-only restriction: {forbidden}",
                path.display()
            ));
        }
    }
    Ok(())
}

fn example_set(directory: &Path) -> Result<BTreeSet<String>, String> {
    fs::read_dir(directory)
        .map_err(|error| format!("could not read {}: {error}", directory.display()))?
        .map(|entry| {
            let path = entry
                .map_err(|error| format!("could not read example entry: {error}"))?
                .path();
            if !path.is_file() {
                return Ok(None);
            }
            let extension = path.extension().and_then(|value| value.to_str());
            if !matches!(extension, Some("ts" | "py")) {
                return Ok(None);
            }
            let stem = path
                .file_stem()
                .and_then(|value| value.to_str())
                .ok_or_else(|| format!("example name is not UTF-8: {}", path.display()))?;
            Ok(Some(stem.replace('-', "_")))
        })
        .filter_map(|entry| entry.transpose())
        .collect()
}

/// Verifies enforcement wording and the regenerated cross-language example set.
///
/// # Errors
///
/// Fails on overstated daemon controls, divergent generated docs or missing examples.
pub fn agent_sdk_guarantee_doc_check(repository: &Path) -> Result<(), String> {
    let typescript_path = repository.join("services/agent/sdk/typescript/src/generated/guarantees.md");
    let python_path = repository.join("services/agent/sdk/python/layerx_sdk/generated/guarantees.md");
    let matrix_path = repository.join("services/agent/sdk/COMPATIBILITY.md");
    let typescript = fs::read_to_string(&typescript_path)
        .map_err(|error| format!("could not read {}: {error}", typescript_path.display()))?;
    let python = fs::read_to_string(&python_path)
        .map_err(|error| format!("could not read {}: {error}", python_path.display()))?;
    let matrix = fs::read_to_string(&matrix_path)
        .map_err(|error| format!("could not read {}: {error}", matrix_path.display()))?;
    if typescript != python {
        return Err("TypeScript and Python guarantee documents diverged".to_owned());
    }
    for (path, source) in [
        (&typescript_path, typescript.as_str()),
        (&python_path, python.as_str()),
        (&matrix_path, matrix.as_str()),
    ] {
        reject_dishonest_document(path, source)?;
    }
    if !typescript
        .contains("| `DaemonLimit` | `daemon_enforced` | Bypassing the daemon bypasses this limit.")
        || !typescript.contains("| `ProtocolBudget` | `protocol_enforced` |")
    {
        return Err("generated guarantee rows are incomplete or dishonest".to_owned());
    }
    if !matrix.contains("they are not protocol guarantees")
        || !matrix.contains("layerx_sdk::GUARANTEES")
    {
        return Err("cross-SDK enforcement documentation is incomplete".to_owned());
    }
    let expected = BTreeSet::from([
        "budget_constrained_spending".to_owned(),
        "http_402_settlement".to_owned(),
        "offline_receipt_verification".to_owned(),
        "payment_with_verification".to_owned(),
        "service_lifecycle".to_owned(),
    ]);
    let typescript_examples = example_set(&repository.join("services/agent/sdk/typescript/examples"))?;
    let python_examples = example_set(&repository.join("services/agent/sdk/python/examples"))?;
    if typescript_examples != expected || python_examples != expected {
        return Err(format!(
            "SDK example set drift: expected={expected:?} typescript={typescript_examples:?} python={python_examples:?}"
        ));
    }
    Ok(())
}

fn main() -> ExitCode {
    let mut arguments = std::env::args_os();
    let _program = arguments.next();
    let repository = arguments.next().map_or_else(
        || std::env::current_dir().unwrap_or_else(|_| PathBuf::from(".")),
        PathBuf::from,
    );
    if arguments.next().is_some() {
        eprintln!("usage: agent-sdk-doc-check [REPOSITORY]");
        return ExitCode::FAILURE;
    }
    if let Err(error) = agent_sdk_compat_matrix(&repository)
        .and_then(|()| agent_sdk_guarantee_doc_check(&repository))
    {
        eprintln!("SDK documentation check failed: {error}");
        return ExitCode::FAILURE;
    }
    println!("SDK compatibility, guarantees, offline path and examples are current");
    ExitCode::SUCCESS
}

#[cfg(test)]
mod tests {
    use super::reject_dishonest_document;
    use std::path::Path;

    #[test]
    fn dishonest_daemon_guarantees_are_release_blocking() {
        for source in [
            "| `DaemonLimit` | `protocol_enforced` | always holds |",
            "A daemon-enforced restriction is a protocol guarantee.",
            "Bypassing the daemon preserves this limit.",
        ] {
            assert!(reject_dishonest_document(Path::new("dishonest.md"), source).is_err());
        }
    }

    #[test]
    fn explicit_non_guarantee_wording_is_allowed() {
        assert!(reject_dishonest_document(
            Path::new("honest.md"),
            "Daemon-enforced controls are not protocol guarantees."
        )
        .is_ok());
    }
}
