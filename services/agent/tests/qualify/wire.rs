use std::collections::BTreeSet;
use std::path::Path;
use std::process::Command;

use layerx_types::vectors::{coverage_report, Corpus};

fn reported_case_count(stdout: &str) -> Result<usize, String> {
    let line = stdout
        .lines()
        .find(|line| line.starts_with("wire parity passed:"))
        .ok_or_else(|| "differential harness omitted its result line".to_owned())?;
    line.split_whitespace()
        .nth(3)
        .ok_or_else(|| "differential harness omitted its case count".to_owned())?
        .parse()
        .map_err(|_| "differential harness emitted an invalid case count".to_owned())
}

/// Runs and audits the process-isolated Rust/C differential wire gate.
///
/// # Errors
///
/// Returns the harness's first-divergence evidence or a named coverage gap.
pub fn agent_qualify_wire_gate(
    repository: &Path,
    c_reference: &Path,
    harness: &Path,
) -> Result<String, String> {
    if !c_reference.is_file() || !harness.is_file() {
        return Err("wire qualification requires built C and Rust harnesses".to_owned());
    }
    let output = Command::new(harness)
        .env("LAYERX_REPOSITORY_ROOT", repository)
        .env("LAYERX_C_REFERENCE", c_reference)
        .output()
        .map_err(|error| format!("could not start differential harness: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "differential harness failed: status={} stderr={}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    let stdout = String::from_utf8(output.stdout)
        .map_err(|_| "differential harness emitted non-UTF-8 output".to_owned())?;
    let observed_cases = reported_case_count(&stdout)?;
    let corpus = Corpus::load(repository).map_err(|error| format!("vector corpus: {error:?}"))?;
    let coverage =
        coverage_report(&corpus).map_err(|error| format!("vector taxonomy coverage: {error:?}"))?;
    if !coverage.unused.is_empty() {
        return Err(format!("unexercised vector classes: {:?}", coverage.unused));
    }
    let expected_cases = corpus.valid_codec.len()
        + corpus.adversarial_codec.len()
        + corpus.replay.canonical_activities.len()
        + corpus.replay.activity_types.len()
        + 8;
    if observed_cases != expected_cases {
        return Err(format!(
            "differential case coverage mismatch: observed={observed_cases} expected={expected_cases}"
        ));
    }
    let activity_types = corpus
        .replay
        .activity_types
        .iter()
        .map(|activity_type| activity_type.value())
        .collect::<BTreeSet<_>>();
    if activity_types.is_empty() {
        return Err("no activity type was exercised".to_owned());
    }
    let rejection_codes = corpus
        .adversarial_codec
        .iter()
        .map(|vector| vector.expected_result.raw())
        .collect::<BTreeSet<_>>();
    if rejection_codes.len() < 4 || rejection_codes.contains(&0) {
        return Err(format!(
            "rejection taxonomy coverage is incomplete: {rejection_codes:?}"
        ));
    }
    Ok(format!(
        "agent_qualify_wire_gate passed cases={observed_cases} vector_classes={} activity_types={} rejection_codes={:?} payload_boundaries=8",
        coverage.exercised.len(),
        activity_types.len(),
        rejection_codes
    ))
}
