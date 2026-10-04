#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
TEST = "maintenance_head_tests::retained_maintenance_head_is_verified_and_survives_restart"
SOURCES = {
    "platform/emulator/src/main.rs",
    "platform/emulator/core/emulator_core.c",
    "platform/emulator/core/emulator_core.h",
    "tests/platform/emulator_maintenance_head.py",
}
CASES = {
    "unavailable-before-seal", "current-signed-maintenance", "stale-activity-receipt",
    "tamper-/receipt_hex", "tamper-/batch_evidence/header_hex",
    "tamper-/batch_evidence/header_signature", "tamper-/batch_evidence/receipt_proof_hex",
    "tamper-/state_root", "foreign-authority", "restart", "snapshot-tamper", "stale-root",
}


def digest(path):
    hasher = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            hasher.update(block)
    return hasher.hexdigest()


def require(condition, detail):
    if not condition:
        raise RuntimeError(detail)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate artifact manifest key")
        result[key] = value
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", default=os.environ.get("PAXEER_X_EMULATOR_MAINTENANCE_ARTIFACTS"))
    supplied = parser.parse_args().artifacts
    require(supplied, "actual prebuilt emulator maintenance artifacts required")
    manifest_path = Path(supplied).resolve(strict=True)
    require(manifest_path.stat().st_size <= 65536, "artifact manifest exceeds bound")
    manifest = json.loads(manifest_path.read_text(), object_pairs_hook=unique_object)
    require(set(manifest) == {"schema", "source_revision", "source_files", "executable"}, "artifact manifest fields")
    require(manifest["schema"] == "layerx.emulator.maintenance-artifacts.v1", "artifact manifest schema")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    require(manifest["source_revision"] == revision, "artifact revision mismatch")
    require(set(manifest["source_files"]) == SOURCES, "artifact source inventory mismatch")
    for relative, expected in manifest["source_files"].items():
        require(digest(ROOT / relative) == expected, "source hash mismatch: " + relative)
    executable = manifest["executable"]
    require(set(executable) == {"path", "sha256"}, "executable fields")
    binary = Path(executable["path"]).resolve(strict=True)
    require(binary.is_file() and os.access(binary, os.X_OK), "prebuilt test executable missing")
    require(digest(binary) == executable["sha256"], "prebuilt test executable hash mismatch")
    environment = dict(os.environ)
    for key in list(environment):
        if key.startswith("LAYERX_") or key.startswith("PAXEER_X_"):
            environment.pop(key)
    result = subprocess.run([str(binary), "--exact", TEST, "--nocapture", "--test-threads=1"],
                            cwd=ROOT, env=environment, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=600, check=False)
    sys.stdout.write(result.stdout)
    require(result.returncode == 0, "actual production inclusion test failed: " + str(result.returncode))
    require("1 passed; 0 failed; 0 ignored;" in result.stdout, "focused test count mismatch")
    observed = [line.split("MAINTENANCE_HEAD_CASE ", 1)[1].strip()
                for line in result.stdout.splitlines() if "MAINTENANCE_HEAD_CASE " in line]
    require(len(observed) == len(CASES) and set(observed) == CASES, "actual maintenance case inventory mismatch")
    require(digest(binary) == executable["sha256"], "artifact changed during qualification")
    for relative, expected in manifest["source_files"].items():
        require(digest(ROOT / relative) == expected, "source changed during qualification: " + relative)
    print("emulator maintenance head: 12 real cases passed")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("emulator maintenance head refused: " + str(error), file=sys.stderr)
        sys.exit(1)
