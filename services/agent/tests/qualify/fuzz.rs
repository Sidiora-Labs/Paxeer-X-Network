use std::fs::{self, File};
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

const FUZZ_TARGETS: &[&str] = &[
    "primitive_decode",
    "envelope_decode",
    "payload_decode",
    "receipt_decode",
    "proof_decode",
    "roundtrip",
    "lni_frame",
    "contract_request",
    "policy_loader",
    "tenant_key",
];
const MAX_LOG_TAIL: u64 = 65_536;
const MAX_CORPUS_ENTRY: u64 = 2_097_157;

#[derive(Clone, Copy)]
struct CorpusStats {
    entries: usize,
    bytes: u64,
}

struct CommandEvidence {
    stdout: String,
    stderr: String,
}

fn read_tail(path: &Path) -> Result<String, String> {
    let mut file = File::open(path)
        .map_err(|error| format!("could not open command log {}: {error}", path.display()))?;
    let length = file
        .metadata()
        .map_err(|error| format!("could not inspect command log {}: {error}", path.display()))?
        .len();
    if length > MAX_LOG_TAIL {
        file.seek(SeekFrom::Start(length - MAX_LOG_TAIL))
            .map_err(|error| format!("could not seek command log {}: {error}", path.display()))?;
    }
    let mut bytes = Vec::with_capacity(length.min(MAX_LOG_TAIL) as usize);
    file.read_to_end(&mut bytes)
        .map_err(|error| format!("could not read command log {}: {error}", path.display()))?;
    Ok(String::from_utf8_lossy(&bytes).into_owned())
}

fn run_logged(
    repository: &Path,
    label: &str,
    command: &mut Command,
) -> Result<CommandEvidence, String> {
    let log_root = repository.join("build/qualification/fuzz");
    fs::create_dir_all(&log_root)
        .map_err(|error| format!("could not create {}: {error}", log_root.display()))?;
    let stdout_path = log_root.join(format!("{label}.stdout.log"));
    let stderr_path = log_root.join(format!("{label}.stderr.log"));
    let stdout = File::create(&stdout_path)
        .map_err(|error| format!("could not create {}: {error}", stdout_path.display()))?;
    let stderr = File::create(&stderr_path)
        .map_err(|error| format!("could not create {}: {error}", stderr_path.display()))?;
    let status = command
        .current_dir(repository)
        .stdout(Stdio::from(stdout))
        .stderr(Stdio::from(stderr))
        .status()
        .map_err(|error| format!("could not start {label}: {error}"))?;
    let evidence = CommandEvidence {
        stdout: read_tail(&stdout_path)?,
        stderr: read_tail(&stderr_path)?,
    };
    if status.success() {
        Ok(evidence)
    } else {
        Err(format!(
            "{label} failed: status={status} stdout_tail={} stderr_tail={}",
            evidence.stdout.trim(),
            evidence.stderr.trim()
        ))
    }
}

fn corpus_stats(root: &Path) -> Result<CorpusStats, String> {
    let mut total = CorpusStats {
        entries: 0,
        bytes: 0,
    };
    for target in FUZZ_TARGETS {
        let directory = root.join(target);
        let entries = fs::read_dir(&directory)
            .map_err(|error| format!("could not read corpus {}: {error}", directory.display()))?;
        let mut target_entries = 0_usize;
        for entry in entries {
            let entry = entry.map_err(|error| {
                format!(
                    "could not enumerate corpus {}: {error}",
                    directory.display()
                )
            })?;
            let metadata = entry.metadata().map_err(|error| {
                format!(
                    "could not inspect corpus {}: {error}",
                    entry.path().display()
                )
            })?;
            if !metadata.is_file() {
                continue;
            }
            if metadata.len() == 0 || metadata.len() > MAX_CORPUS_ENTRY {
                return Err(format!(
                    "corpus entry {} has invalid size {}",
                    entry.path().display(),
                    metadata.len()
                ));
            }
            target_entries += 1;
            total.entries += 1;
            total.bytes = total
                .bytes
                .checked_add(metadata.len())
                .ok_or_else(|| "corpus byte count overflowed".to_owned())?;
        }
        if target_entries == 0 {
            return Err(format!("fuzz target {target} has no corpus entries"));
        }
    }
    Ok(total)
}

fn audit_targets(repository: &Path) -> Result<(), String> {
    let fuzz_root = repository.join("services/agent/fuzz");
    let manifest = fs::read_to_string(fuzz_root.join("Cargo.toml"))
        .map_err(|error| format!("could not read fuzz manifest: {error}"))?;
    for target in FUZZ_TARGETS {
        if !manifest.contains(&format!("name = \"{target}\""))
            || !fuzz_root
                .join(format!("fuzz_targets/{target}.rs"))
                .is_file()
        {
            return Err(format!("fuzz target {target} is not fully declared"));
        }
    }
    for (target, parser) in [
        ("lni_frame", "decode_frame"),
        ("contract_request", "layerx_mcp::validate"),
        ("policy_loader", "load_policy_source"),
    ] {
        let path = fuzz_root.join(format!("fuzz_targets/{target}.rs"));
        let source = fs::read_to_string(&path)
            .map_err(|error| format!("could not read {}: {error}", path.display()))?;
        if !source.contains(parser) {
            return Err(format!("fuzz target {target} no longer reaches {parser}"));
        }
    }
    Ok(())
}

fn run_fuzzers(repository: &Path, minimized_root: &Path) -> Result<(), String> {
    let mut fuzz = Command::new("make");
    fuzz.args([
        "--no-print-directory",
        "AGENT_FUZZ_RUNS=256",
        "AGENT_FUZZ_MAX_LEN=1048576",
        "AGENT_FUZZ_RSS_MB=512",
        "AGENT_FUZZ_TIMEOUT=2",
        "agent-fuzz",
    ]);
    run_logged(repository, "bounded-fuzz", &mut fuzz)?;

    let minimized = format!("AGENT_FUZZ_MINIMIZED_ROOT={}", minimized_root.display());
    let mut minimise = Command::new("make");
    minimise.args([
        "--no-print-directory",
        minimized.as_str(),
        "AGENT_FUZZ_MAX_LEN=1048576",
        "AGENT_FUZZ_RSS_MB=512",
        "AGENT_FUZZ_TIMEOUT=2",
        "agent-fuzz-minimize",
    ]);
    run_logged(repository, "corpus-minimisation", &mut minimise)?;
    Ok(())
}

/// Runs every committed parser corpus under bounded libFuzzer and audits minimisation.
///
/// # Errors
///
/// Fails on any crash, timeout, RSS breach, missing target, empty corpus or ineffective expansion.
pub fn agent_qualify_fuzz_gate(repository: &Path, minimized_root: &Path) -> Result<String, String> {
    audit_targets(repository)?;
    let committed = corpus_stats(&repository.join("services/agent/fuzz/corpus"))?;
    run_fuzzers(repository, minimized_root)?;
    let minimized = corpus_stats(minimized_root)?;
    if minimized.entries > committed.entries || minimized.bytes > committed.bytes {
        return Err(format!(
            "corpus minimisation expanded input: committed={}/{} minimized={}/{}",
            committed.entries, committed.bytes, minimized.entries, minimized.bytes
        ));
    }
    Ok(format!(
        "agent_qualify_fuzz_gate passed targets={} committed_entries={} committed_bytes={} minimized_entries={} minimized_bytes={} runs_per_target=256 timeout_seconds=2 rss_limit_mb=512 max_input_bytes=1048576",
        FUZZ_TARGETS.len(),
        committed.entries,
        committed.bytes,
        minimized.entries,
        minimized.bytes
    ))
}

fn unsafe_exception_count(repository: &Path) -> Result<usize, String> {
    let path = repository.join("services/agent/unsafe-allowlist.toml");
    let source = fs::read_to_string(&path)
        .map_err(|error| format!("could not read {}: {error}", path.display()))?;
    let exceptions = source
        .lines()
        .filter(|line| line.trim() == "[[exceptions]]")
        .count();
    if exceptions == 0 {
        return Err("unsafe allowlist contains no justified exception".to_owned());
    }
    Ok(exceptions)
}

/// Runs the workspace under supported sanitizers and enforces the unsafe allowlist.
///
/// # Errors
///
/// Fails on an ASan/TSan report, an unexplained sanitizer probe failure or unsafe-code drift.
pub fn agent_qualify_sanitizer_gate(repository: &Path) -> Result<String, String> {
    let mut sanitizers = Command::new("sh");
    sanitizers.arg("services/agent/tools/run-sanitizers.sh");
    let sanitizer_evidence = run_logged(repository, "sanitizers", &mut sanitizers)?;
    let sanitizer_output = format!(
        "{}\n{}",
        sanitizer_evidence.stdout, sanitizer_evidence.stderr
    );
    let thread = if sanitizer_output
        .contains("thread sanitizer unavailable: pinned standard library is not TSan-instrumented")
    {
        "unsupported-pinned-stdlib"
    } else {
        "passed"
    };

    let mut boundary = Command::new("cargo");
    boundary.args([
        "run",
        "--manifest-path",
        "services/agent/tools/boundary-check/Cargo.toml",
        "--locked",
        "--quiet",
        "--",
        "agent",
    ]);
    let boundary_evidence = run_logged(repository, "unsafe-allowlist", &mut boundary)?;
    if !boundary_evidence
        .stdout
        .contains("agent boundary purity passed")
    {
        return Err("unsafe checker omitted its success evidence".to_owned());
    }
    let exceptions = unsafe_exception_count(repository)?;
    Ok(format!(
        "agent_qualify_sanitizer_gate passed address=passed thread={thread} unsafe_allowlist=passed justified_exceptions={exceptions}"
    ))
}
