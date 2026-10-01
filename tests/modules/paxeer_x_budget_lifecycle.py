#!/usr/bin/env python3
"""Build or qualify budget partial defunding and revocation Activities."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
TARGET = ROOT / "build/paxeer-x-3.1"
BINARY = TARGET / "tests/test_budget_close"
MANIFEST = TARGET / "budget-lifecycle.json"
SOURCES = (
    "include/layerx/lx_budget.h",
    "src/modules/budget/lx_budget_module.c",
    "src/modules/budget/lx_budget_close.c",
    "src/modules/budget/lx_budget_codec.c",
    "src/protocol/lxp_kernel.c",
    "tests/modules/test_budget_close.c",
    "tests/modules/paxeer_x_budget_lifecycle.py",
)
REQUIRED = {
    "helper-path", "codec-defund", "codec-defund-length", "codec-defund-version",
    "codec-revoke", "codec-revoke-zero-sequence", "activity-ids-distinct",
    "defund-unauthorized", "revoke-unauthorized", "defund-overdraw",
    "defund-u128-max", "defund-zero", "close-distinct-payload",
    "defund-rejects-revoke-payload", "defund-partial", "defund-duplicate",
    "spend-after-defund-clamped", "revoke-stale-sequence", "spend-after-rollover",
    "revoke", "revoke-duplicate", "revoke-after-revoke", "spend-after-revoke",
    "defund-after-revoke", "fund-after-revoke", "record-durable-roundtrip",
    "replica-replay-identical",
}
COMMAND = ["timeout", "1800s", "python3", "tests/modules/paxeer_x_budget_lifecycle.py"]


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def candidate():
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True,
                              capture_output=True, text=True).stdout.strip()
    dirty = subprocess.run(["git", "status", "--porcelain", "--", *SOURCES], cwd=ROOT,
                           check=True, capture_output=True, text=True).stdout.splitlines()
    return revision, {name: digest(ROOT / name) for name in SOURCES}, dirty


def build():
    revision, sources, _ = candidate()
    TARGET.mkdir(parents=True, exist_ok=True)
    MANIFEST.unlink(missing_ok=True)
    command = ["make", "-j5", "BUILD_DIR=build/paxeer-x-3.1", f"LXP_REVISION={revision}",
               "build/paxeer-x-3.1/tests/test_budget_close"]
    print("BUILD " + json.dumps(command), flush=True)
    subprocess.run(command, cwd=ROOT, check=True)
    if candidate()[1] != sources:
        raise RuntimeError("candidate sources changed during build")
    MANIFEST.write_text(json.dumps({"revision": revision, "sources": sources,
        "binary_sha256": digest(BINARY), "command": command}, indent=2) + "\n")


def qualify():
    evidence = Path(os.environ["PAXEER_X_EVIDENCE_DIR"]) if "PAXEER_X_EVIDENCE_DIR" in os.environ \
        else Path(tempfile.mkdtemp(prefix="paxeer-x-budget-lifecycle-"))
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(evidence, 0o700)
    log_path = evidence / "budget-lifecycle.log"
    result = {"task": "3.1", "tests": 0, "skipped": 0, "cases": [], "command": COMMAND}
    code = 1
    try:
        revision, sources, dirty = candidate()
        result["revision"] = revision
        result["uncommitted_sources"] = dirty
        if not BINARY.is_file() or not MANIFEST.is_file():
            raise RuntimeError("missing prebuilt test_budget_close or its manifest; run --build first")
        manifest = json.loads(MANIFEST.read_text())
        if manifest.get("sources") != sources or manifest.get("binary_sha256") != digest(BINARY):
            raise RuntimeError("prebuilt binary does not match the current candidate sources")
        with log_path.open("w") as log:
            completed = subprocess.run([str(BINARY)], cwd=ROOT, stdout=log,
                                       stderr=subprocess.STDOUT, timeout=600, check=False)
        result["binary_exit_code"] = completed.returncode
        result["log"] = str(log_path)
        output = log_path.read_text()
        cases = {}
        for line in output.splitlines():
            if line.startswith("BUDGET_CASE "):
                _, name, status = line.split(" ")
                if name in cases:
                    raise RuntimeError(f"duplicate case {name}")
                cases[name] = status
        result["cases"] = [{"name": name, "status": status} for name, status in cases.items()]
        result["tests"] = len(cases)
        if completed.returncode != 0:
            raise RuntimeError(f"test_budget_close exited {completed.returncode}; see {log_path}")
        if any(status != "ok" for status in cases.values()):
            raise RuntimeError("a lifecycle case failed")
        missing = REQUIRED - set(cases)
        if missing:
            raise RuntimeError("missing required cases: " + ", ".join(sorted(missing)))
        if f"BUDGET_LIFECYCLE cases={len(cases)} skipped=0" not in output.splitlines():
            raise RuntimeError("missing or inconsistent case accounting")
        result["artifact_manifest"] = manifest
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
        print(str(error), file=sys.stderr)
    result["exit_code"] = code
    destination = evidence / "budget-lifecycle.json"
    destination.write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(destination, 0o600)
    print(f"revision={result.get('revision', 'unknown')} command={' '.join(COMMAND)} exit_code={code}")
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0")
    print(f"Evidence: {destination}")
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build", action="store_true",
                        help="build the declared test_budget_close target only")
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.build:
        build()
        return 0
    return qualify()


if __name__ == "__main__":
    sys.exit(main())
