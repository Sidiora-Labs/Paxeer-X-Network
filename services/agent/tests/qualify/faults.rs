use std::path::Path;
use std::process::Command;

fn require_test(repository: &Path, file: &str, test: &str) -> Result<(), String> {
    let path = repository.join(file);
    let source = std::fs::read_to_string(&path)
        .map_err(|error| format!("could not read {}: {error}", path.display()))?;
    if source.contains(&format!("fn {test}()")) {
        Ok(())
    } else {
        Err(format!("fault scenario {test} is missing from {file}"))
    }
}

fn run_test(repository: &Path, test: &str) -> Result<(), String> {
    let output = Command::new("cargo")
        .args([
            "test",
            "--manifest-path",
            "services/agent/Cargo.toml",
            "--locked",
            "-p",
            "layerx-agentd",
            "--test",
            test,
        ])
        .current_dir(repository)
        .output()
        .map_err(|error| format!("could not run fault suite {test}: {error}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!(
            "fault suite {test} failed: status={} stderr={}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        ))
    }
}

/// Runs stage loss, kill, delay, restart, clock and ownership fault injection.
///
/// # Errors
///
/// Fails when a named injection disappears or any durable invariant suite fails.
pub fn agent_fault_injection_suite(repository: &Path) -> Result<String, String> {
    for (file, test) in [
        (
            "services/agent/crates/layerx-agentd/tests/deadlines.rs",
            "disconnect_cancels_every_write_stage_before_transmission",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/deadlines.rs",
            "disconnect_mid_submission_transfers_ownership_to_receipt_resolution",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/deadlines.rs",
            "request_deadline_reports_unknown_but_does_not_cancel_its_resolver",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/recovery.rs",
            "restart_at_every_write_stage_preserves_exactly_one_recovery_action",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/recovery.rs",
            "restart_resolution_uses_receipts_without_duplicate_delivery",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/recovery.rs",
            "clock_regression_during_restarted_resolution_fails_closed",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/unknown.rs",
            "acknowledgement_loss_is_resolved_only_by_the_existing_receipt",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/unknown.rs",
            "lost_resend_response_keeps_budget_and_ceiling_held_and_reuses_exact_bytes",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/budget_unknown.rs",
            "process_loss_between_submission_and_receipt_preserves_unknown_hold",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/budget_reserve.rs",
            "terminal_and_expiry_release_are_deterministic_unknown_is_held",
        ),
        (
            "services/agent/crates/layerx-agentd/tests/delivery.rs",
            "loaded_seam_is_ordered_observable_and_has_no_duplicate",
        ),
    ] {
        require_test(repository, file, test)?;
    }
    for suite in [
        "deadlines",
        "recovery",
        "unknown",
        "budget_unknown",
        "budget_reserve",
        "outbox",
        "delivery",
    ] {
        run_test(repository, suite)?;
    }
    Ok("agent_fault_injection_suite passed scenarios=connection_loss_all_stages,kill_before_transmission,duplicate_delivery,delayed_ack,restart_resolution,clock_disturbance ownership=resolver_or_caller reservations=no_leak outbox=no_loss unknown=honest".to_owned())
}
