use std::path::Path;
use std::process::Command;

/// Runs repeated, concurrent and post-restart idempotency qualification.
///
/// # Errors
///
/// Fails if any scenario is absent or produces more than one receipt/effect.
pub fn agent_exactly_once_suite(repository: &Path) -> Result<String, String> {
    let path = repository.join("services/agent/crates/layerx-agentd/tests/idempotency.rs");
    let source = std::fs::read_to_string(&path)
        .map_err(|error| format!("could not read {}: {error}", path.display()))?;
    for required in [
        "repeated_key_returns_original_and_changed_body_conflicts",
        "concurrent_duplicates_produce_exactly_one_economic_effect",
        "post_restart_retry_reuses_original_result_and_pending_bytes",
    ] {
        if !source.contains(&format!("fn {required}()")) {
            return Err(format!("exactly-once scenario {required} is missing"));
        }
    }
    let output = Command::new("cargo")
        .args([
            "test",
            "--manifest-path",
            "services/agent/Cargo.toml",
            "--locked",
            "-p",
            "layerx-agentd",
            "--test",
            "idempotency",
        ])
        .current_dir(repository)
        .output()
        .map_err(|error| format!("could not run idempotency suite: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "idempotency suite failed: status={} stderr={}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    Ok("agent_exactly_once_suite passed retries=repeated,concurrent,post_restart receipts=1 economic_effects=1".to_owned())
}
