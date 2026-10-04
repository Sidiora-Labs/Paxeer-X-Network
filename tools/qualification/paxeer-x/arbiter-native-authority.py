#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
EVIDENCE = Path(os.environ.get("LAYERX_NATIVE_AUTHORITY_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043535"))
TARGET = Path(os.environ.get("LAYERX_NATIVE_AUTHORITY_TARGET", "/root/lx-target/task1043535"))
DECLARED = (
    "include/layerx/programs.h", "include/layerx/lxp_receipt.h", "include/layerx/lxp_module.h",
    "src/modules/programs/call.c", "src/protocol/lxp_receipt.c", "src/protocol/lxp_module_ctx.c",
    "cmd/layerxd/lxp_daemon_protocol.c", "programs/crates/layerx-programs-runtime/src/ffi_call.rs",
    "programs/crates/layerx-programs-runtime/src/lib.rs", "tests/daemon/lxp_test_arbiter_native_authority.c",
    "tools/paxeer-x/build/104.35.35.mk", "tools/qualification/paxeer-x/arbiter-native-authority.py",
)
MARKER = "ARBITER_NATIVE_AUTHORITY real-signed-owner-index-call-frame-247-reopen-refusal"
REQUIRED_CASES = {
    "owner-publication", "serial", "scheduled", "historical-handover", "reopen",
    "new-index-missing", "wrong-network", "wrong-batch", "wrong-digest", "wrong-root",
    "bad-header-signature", "inactive-view", "view-bounds", "old-lookup-fallback", "old-receipt-view",
}


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    output = subprocess.run([
        "git", "ls-files", "-z", "--", "src", "include", "cmd/layerxd", "programs", "Makefile",
        "tests/daemon/lxp_test_arbiter_admission.c", "tests/daemon/lxp_test_replay_authority.c",
        "tests/programs/test_call_activity.c", "tests/bridge/files.h", "tools/paxeer-x/build",
        "tools/qualification/paxeer-x/replay-authority.py", "tools/qualification/paxeer-x/program-replay-record.py",
    ], cwd=ROOT, check=True, capture_output=True).stdout
    names = {os.fsdecode(value) for value in output.split(b"\0") if value} | set(DECLARED)
    return {name: sha(ROOT / name) for name in sorted(names)
            if Path(name).suffix in {".c", ".h", ".rs", ".toml", ".lock", ".mk", ".py", ".inc"} or name == "Makefile"}


def execute(command, log, environment, timeout):
    with log.open("w") as stream:
        try:
            result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream,
                                    stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired:
            log.with_suffix(".exit").write_text("124\n")
            print("exit=124 log=" + str(log), flush=True)
            raise
    log.with_suffix(".exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def exported_inputs(directory):
    manifest = json.loads((directory / "native-authority.json").read_text())
    if manifest["network_id"] != 7 or manifest["view_bytes"] != 247:
        raise RuntimeError("native authority version/bounds/network mismatch")
    if set(manifest["cases"]) != REQUIRED_CASES:
        raise RuntimeError("required genuine owner/handover/reopen/refusal corpus incomplete")
    outputs = {}
    phases = set()
    for item in manifest["views"]:
        path = (directory / item["view"]).resolve(strict=True)
        if path.parent != directory.resolve():
            raise RuntimeError("native projection escaped private fixture")
        data = path.read_bytes()
        if len(data) != 247 or data[:2] != b"\0\1" or int.from_bytes(data[2:6], "big") != 7:
            raise RuntimeError("actual fixed native projection missing")
        if data[55:87].hex() != item["receipt_digest"]:
            raise RuntimeError("native receipt identity projection mismatch")
        batch = int.from_bytes(data[15:23], "big")
        first = int.from_bytes(data[39:47], "big")
        last = int.from_bytes(data[47:55], "big")
        if not first <= batch <= last or data[183:215] == bytes(32) or data[215:247] == bytes(32):
            raise RuntimeError("actual native-pinned authorization absent")
        phases.add(item["phase"])
        outputs[str(path)] = sha(path)
    if not {"before", "after"} <= phases:
        raise RuntimeError("real protected reopen projection missing")
    outputs[str(directory / "native-authority.json")] = sha(directory / "native-authority.json")
    return outputs


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    args = parser.parse_args()
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    if EVIDENCE.stat().st_mode & 0o077:
        raise RuntimeError("private native authority evidence required")
    environment = dict(os.environ)
    environment["PATH"] = "/root/.cargo/bin:" + environment.get("PATH", "")
    environment["CARGO_BUILD_JOBS"] = "4"
    if args.build:
        inputs = sources()
        execute([
            "flock", "/root/lx-cargo/native-build.lock", "make", "-f", "Makefile", "-f",
            "tools/paxeer-x/build/104.35.35.mk", "-j4", "BUILD_DIR=" + str(TARGET / "native"),
            "PROGRAMS_TARGET_DIR=" + str(TARGET / "rust"),
            "PROGRAMS_RUNTIME_LIB=" + str(TARGET / "rust/debug/liblayerx_programs_sandbox.a"),
            "paxeer-x-arbiter-native-authority-build",
        ], EVIDENCE / "build.log", environment, 1140)
        binary = (TARGET / "native/tests/lxp_test_arbiter_native_authority").resolve(strict=True)
        archive = (TARGET / "rust/debug/liblayerx_programs_sandbox.a").resolve(strict=True)
        if inputs != sources():
            raise RuntimeError("source changed during sole native authority build")
        (EVIDENCE / "artifacts.json").write_text(json.dumps({
            "revision": subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True,
                                       capture_output=True, text=True).stdout.strip(),
            "inputs": inputs,
            "binary": {"path": str(binary), "sha256": sha(binary)},
            "archive": {"path": str(archive), "sha256": sha(archive)},
        }, indent=2))
        return 0
    manifest = json.loads((EVIDENCE / "artifacts.json").read_text())
    if manifest["inputs"] != sources():
        raise RuntimeError("native authority source mismatch")
    for key in ("binary", "archive"):
        if sha(manifest[key]["path"]) != manifest[key]["sha256"]:
            raise RuntimeError("native authority artifact changed")
    directory = Path(tempfile.mkdtemp(prefix="native-", dir=EVIDENCE))
    output = execute([manifest["binary"]["path"], str(directory)], EVIDENCE / "verify.log", environment, 540)
    if MARKER not in output:
        raise RuntimeError("genuine native authority corpus marker missing")
    outputs = exported_inputs(directory)
    if manifest["inputs"] != sources():
        raise RuntimeError("native authority source changed during verify")
    (EVIDENCE / "result.json").write_text(json.dumps({
        "revision": manifest["revision"], "command": "timeout 10m python3 tools/qualification/paxeer-x/arbiter-native-authority.py",
        "exit": 0, "log": str(EVIDENCE / "verify.log"), "fixture": str(directory), "outputs": outputs,
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
