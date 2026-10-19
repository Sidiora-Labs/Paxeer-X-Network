use std::cell::Cell;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use layerx_agentd::audit::{verify_chain, AuditError, ChainIssue, Log};
use layerx_agentd::store::TenantId;

const HEADER_BYTES: usize = 40;
const FRAME_FIXED_BYTES: usize = 80;
static NEXT_DIRECTORY: AtomicU64 = AtomicU64::new(1);

fn root(label: &str) -> PathBuf {
    let sequence = NEXT_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "layerx-audit-{label}-{}-{sequence}",
        std::process::id()
    ))
}

fn tenant() -> TenantId {
    TenantId::new("tenant/with/path-like-input").unwrap_or_else(|error| panic!("tenant: {error}"))
}

fn populated(label: &str, payloads: &[&[u8]]) -> (PathBuf, PathBuf) {
    let root = root(label);
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("open: {error}"));
    for payload in payloads {
        log.before_operation(payload, || ())
            .unwrap_or_else(|error| panic!("append: {error}"));
    }
    let path = log.path().to_path_buf();
    (root, path)
}

fn frame_ranges(bytes: &[u8]) -> Vec<std::ops::Range<usize>> {
    let mut ranges = Vec::new();
    let mut offset = HEADER_BYTES;
    while offset < bytes.len() {
        let length_offset = offset + 4 + 8 + 32;
        let length = u32::from_be_bytes(
            bytes[length_offset..length_offset + 4]
                .try_into()
                .unwrap_or_else(|_| panic!("payload length")),
        ) as usize;
        let end = offset + FRAME_FIXED_BYTES + length;
        ranges.push(offset..end);
        offset = end;
    }
    ranges
}

fn failure(path: &Path) -> (u64, ChainIssue) {
    match verify_chain(path) {
        Err(AuditError::Invalid(failure)) => (failure.entry, failure.issue),
        other => panic!("expected chain failure, got {other:?}"),
    }
}

#[test]
fn durable_entry_exists_before_the_operation_runs() {
    let root = root("ordering");
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("open: {error}"));
    let path = log.path().to_path_buf();
    let observed = Cell::new(0);
    let (receipt, ()) = log
        .before_operation(b"mutation-attempt", || {
            let verified =
                verify_chain(&path).unwrap_or_else(|error| panic!("verify in operation: {error}"));
            observed.set(verified.entries);
        })
        .unwrap_or_else(|error| panic!("audited operation: {error}"));
    assert_eq!(receipt.sequence, 0);
    assert_eq!(observed.get(), 1);
    drop(log);
    assert_eq!(
        verify_chain(&path)
            .unwrap_or_else(|error| panic!("verify after restart: {error}"))
            .entries,
        1
    );
    let _ = fs::remove_dir_all(root);
}

#[test]
fn tampering_reports_the_first_changed_entry() {
    let (root, path) = populated("tamper", &[b"first", b"second", b"third"]);
    let mut bytes = fs::read(&path).unwrap_or_else(|error| panic!("read: {error}"));
    let ranges = frame_ranges(&bytes);
    bytes[ranges[1].start + 48] ^= 0x40;
    fs::write(&path, bytes).unwrap_or_else(|error| panic!("tamper: {error}"));
    assert_eq!(failure(&path), (1, ChainIssue::EntryHashMismatch));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn removing_the_tail_is_detected_by_the_durable_anchor() {
    let (root, path) = populated("remove", &[b"first", b"second", b"third"]);
    let mut bytes = fs::read(&path).unwrap_or_else(|error| panic!("read: {error}"));
    let ranges = frame_ranges(&bytes);
    bytes.drain(ranges[2].clone());
    fs::write(&path, bytes).unwrap_or_else(|error| panic!("remove: {error}"));
    assert_eq!(failure(&path), (2, ChainIssue::AnchorMismatch));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn removing_the_whole_log_cannot_reset_an_existing_anchor() {
    let (root, path) = populated("remove-whole", &[b"first"]);
    fs::remove_file(&path).unwrap_or_else(|error| panic!("remove log: {error}"));
    assert!(matches!(
        Log::open(&root, &tenant()),
        Err(AuditError::Invalid(failure)) if failure.issue == ChainIssue::AnchorMismatch
    ));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn reordering_reports_the_first_out_of_sequence_entry() {
    let (root, path) = populated("reorder", &[b"same1", b"same2", b"same3"]);
    let bytes = fs::read(&path).unwrap_or_else(|error| panic!("read: {error}"));
    let ranges = frame_ranges(&bytes);
    let mut reordered = bytes[..HEADER_BYTES].to_vec();
    reordered.extend_from_slice(&bytes[ranges[1].clone()]);
    reordered.extend_from_slice(&bytes[ranges[0].clone()]);
    reordered.extend_from_slice(&bytes[ranges[2].clone()]);
    fs::write(&path, reordered).unwrap_or_else(|error| panic!("reorder: {error}"));
    assert_eq!(failure(&path), (0, ChainIssue::SequenceMismatch));
    let _ = fs::remove_dir_all(root);
}

#[test]
fn audit_write_failure_refuses_the_operation() {
    let (root, path) = populated("write-failure", &[b"initial"]);
    let mut log = Log::open(&root, &tenant()).unwrap_or_else(|error| panic!("reopen: {error}"));
    let backup = path.with_file_name("audit.backup");
    fs::rename(&path, &backup).unwrap_or_else(|error| panic!("rename: {error}"));
    fs::create_dir(&path).unwrap_or_else(|error| panic!("blocking directory: {error}"));
    let operation_ran = Cell::new(false);
    let result = log.before_operation(b"must-not-run", || operation_ran.set(true));
    assert!(matches!(result, Err(AuditError::Io(_))));
    assert!(!operation_ran.get());
    let _ = fs::remove_dir_all(root);
}
