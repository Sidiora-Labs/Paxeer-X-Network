#!/usr/bin/env python3
"""Qualify replay-safe receipt identity across sustained execution and recovery."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BINARY = ROOT / "build/tests/lxp_test_idempotency"
LIBRARY = ROOT / "build/liblayerx.a"
SOURCES = (
    "include/layerx/lxp_state.h",
    "src/state/lxp_idempotency.c",
    "src/state/lxp_journal.c",
    "src/state/lxp_snapshot.c",
    "src/state/lxp_state_root.c",
    "src/protocol/lxp_module_ctx.c",
    "src/protocol/lxp_kernel.c",
    "tests/state/lxp_test_idempotency.c",
    "tests/qualification/paxeer_x_idempotency_lifecycle.py",
    "Makefile",
)
CHECKPOINT_CASES = {
    "journal_single_entry", "replica_lockstep_600", "checkpoint_511",
    "checkpoint_512", "checkpoint_513", "sustained_4096", "checkpoint_4096",
    "oldest_replay_identical", "oldest_replay_changed", "distinct_actor",
    "epoch_checkpoint", "epoch_transition", "storage_boundary",
    "checkpoint_written",
}
RESTART_CASES = {
    "journal_single_entry", "restart_root", "restart_replay_changed",
    "restart_distinct_actor", "restart_refused_absent",
    "restart_epoch_transition",
}
COMMAND = ["timeout", "1800s", "python3", "tests/qualification/paxeer_x_idempotency_lifecycle.py"]
LIFECYCLE = [
    "producer: lxp_kernel_execute_activity stages one entry per durable receipt (success, durable failure, terminal rejection) through receipt_store -> lxp_idempotency_record; prepared program calls stage through the same journal",
    "commit: lxp_state_journal_commit appends the staged entry via lxp_idempotency_commit_staged after lxp_idempotency_can_commit; rollback discards it",
    "preview: lxp_module_ctx preview_state_root copies the live index into a private preview store to compute the receipt root; never published",
    "prepare/publication: lxp_state_snapshot_create/clone copy the index; lxp_state_transition_create records add/replace/delete deltas; apply_snapshot and publish_guarded install them",
    "recovery: lxp_state_snapshot_restore, lxp_snapshot_write/load and the node checkpoint store carry the full sorted index with unchanged format",
    "epoch: lxp_kernel_epoch_transition commits a journal with no idempotency record; no batch, epoch or checkpoint path retires entries",
    "inference: entries are never retired, so the former 512-entry embedded array was a cumulative lifetime cap reached after 512 receipts (the 513th refused with LXP_ERR_ARENA_EXHAUSTED)",
    "representation: one heap index per store, allocated once at full capacity on first use and never reallocated, so shallow store copies stay valid; capacity LXP_STATE_MAX_IDEMPOTENCY=8192",
    "overflow: at capacity the next activity refuses with LXP_ERR_ARENA_EXHAUSTED before any sequence, balance or index mutation; nothing is evicted",
]
MEMORY_BOUND = {
    "entry_bytes": 4132,
    "capacity": 8192,
    "index_bytes_per_store": 4132 * 8192,
    "note": "virtual reservation per live, snapshot, preview or prepared store; resident pages grow with committed entries",
}


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, check=True,
                          capture_output=True, text=True).stdout


def run_binary(arguments, log_path, expected):
    with log_path.open("w") as log:
        completed = subprocess.run([str(BINARY), *arguments], cwd=ROOT, stdout=log,
                                   stderr=subprocess.STDOUT, timeout=1500, check=False)
    output = log_path.read_text()
    cases = {}
    for line in output.splitlines():
        if line.startswith("IDEMPOTENCY_CASE "):
            _, name, status = line.split(" ")
            if name in cases:
                raise RuntimeError(f"duplicate case {name}")
            cases[name] = status
    if completed.returncode != 0:
        raise RuntimeError(f"{BINARY.name} {' '.join(arguments)} exited {completed.returncode}; see {log_path}")
    if any(status != "ok" for status in cases.values()):
        raise RuntimeError("an idempotency case failed")
    missing = expected - set(cases)
    if missing:
        raise RuntimeError("missing required cases: " + ", ".join(sorted(missing)))
    if f"IDEMPOTENCY_LIFECYCLE cases={len(cases)} skipped=0" not in output.splitlines():
        raise RuntimeError("missing or inconsistent case accounting")
    return completed.returncode, cases


def main():
    result = {"task": "1.1", "tests": 0, "skipped": 0, "cases": [], "command": COMMAND,
              "lifecycle": LIFECYCLE, "memory_bound": MEMORY_BOUND}
    code = 1
    destination = None
    try:
        if "PAXEER_X_EVIDENCE_DIR" not in os.environ:
            raise RuntimeError("PAXEER_X_EVIDENCE_DIR is not set")
        evidence = Path(os.environ["PAXEER_X_EVIDENCE_DIR"]).resolve()
        if evidence == ROOT or ROOT in evidence.parents:
            raise RuntimeError("evidence directory must be outside the source tree")
        evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(evidence, 0o700)
        destination = evidence / "idempotency-lifecycle.json"
        revision = git("rev-parse", "HEAD").strip()
        result["revision"] = revision
        dirty = git("status", "--porcelain", "--untracked-files=all").splitlines()
        if dirty:
            raise RuntimeError("source tree is not clean: " + "; ".join(dirty))
        commit_time = int(git("log", "-1", "--format=%ct", "HEAD").strip())
        for prerequisite in (BINARY, LIBRARY):
            if not prerequisite.is_file():
                raise RuntimeError(f"missing prebuilt {prerequisite.relative_to(ROOT)}; build the task targets first")
            if int(prerequisite.stat().st_mtime) < commit_time:
                raise RuntimeError(f"{prerequisite.relative_to(ROOT)} is older than HEAD {revision}")
        result["sources"] = {name: digest(ROOT / name) for name in SOURCES}
        result["binary_sha256"] = digest(BINARY)
        result["library_sha256"] = digest(LIBRARY)
        restart_dir = Path(tempfile.mkdtemp(prefix=f"restart-{revision[:12]}-", dir=evidence))
        os.chmod(restart_dir, 0o700)
        result["checkpoint_directory"] = str(restart_dir)
        runs = []
        for mode, expected in (("--checkpoint", CHECKPOINT_CASES), ("--restart", RESTART_CASES)):
            log_path = restart_dir / f"{mode.lstrip('-')}.log"
            exit_code, cases = run_binary([mode, str(restart_dir)], log_path, expected)
            runs.append({"mode": mode, "exit_code": exit_code, "log": str(log_path),
                         "cases": sorted(cases)})
            result["cases"].extend({"name": f"{mode.lstrip('-')}:{name}", "status": status}
                                   for name, status in cases.items())
        result["runs"] = runs
        result["tests"] = len(result["cases"])
        if git("status", "--porcelain", "--untracked-files=all").splitlines():
            raise RuntimeError("the gate wrote into the source tree")
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
        print(str(error), file=sys.stderr)
    result["exit_code"] = code
    if destination is not None:
        destination.write_text(json.dumps(result, indent=2) + "\n")
        os.chmod(destination, 0o600)
    print(f"revision={result.get('revision', 'unknown')} command={' '.join(COMMAND)} exit_code={code}")
    print(f"Evidence: {destination}")
    print(f"PAXEER_X_GATE tests={result['tests'] if code == 0 else 0} skipped=0")
    return code


if __name__ == "__main__":
    sys.exit(main())
