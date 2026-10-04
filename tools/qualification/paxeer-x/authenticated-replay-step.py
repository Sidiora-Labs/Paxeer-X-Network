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
EVIDENCE = Path(os.environ.get("LAYERX_AUTHENTICATED_REPLAY_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043525"))
DECLARED = (
    "programs/crates/layerx-programs-runtime/src/portable_replay.rs",
    "programs/crates/layerx-programs-arbiter/Cargo.toml",
    "programs/crates/layerx-programs-arbiter/src/lib.rs",
    "programs/crates/layerx-programs-arbiter/src/step.rs",
    "programs/crates/layerx-programs-arbiter/tests/verified_step.rs",
    "tools/qualification/paxeer-x/authenticated-replay-step.py",
)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    result = subprocess.run(
        ["git", "ls-files", "-z", "--", "src", "include", "cmd/layerxd", "programs", "Makefile", "tests/daemon/lxp_test_program_replay.c", "tests/daemon/lxp_test_arbiter_admission.c", "tests/programs/test_call_activity.c", "tests/bridge/files.h", "tools/paxeer-x/build/104.35.20.mk", "tools/qualification/paxeer-x/program-replay-record.py"],
        cwd=ROOT, check=True, capture_output=True,
    )
    names = {os.fsdecode(p) for p in result.stdout.split(b"\0") if p} | set(DECLARED)
    return {p: sha(ROOT / p) for p in sorted(names) if Path(p).suffix in {".rs", ".c", ".h", ".toml", ".lock", ".mk", ".py", ".json", ".inc"} or p == "Makefile"}


def execute(command, log, environment, timeout):
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
    log.with_suffix(log.suffix + ".exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def artifact(output, name):
    paths = set()
    for line in output.splitlines():
        if line.startswith("{"):
            item = json.loads(line)
            if item.get("reason") == "compiler-artifact" and item.get("target", {}).get("name") == name and item.get("executable"):
                paths.add(item["executable"])
    if len(paths) != 1:
        raise RuntimeError("required genuine artifact missing: " + name)
    return str(Path(paths.pop()).resolve(strict=True))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    args = parser.parse_args()
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    if EVIDENCE.stat().st_mode & 0o077:
        raise RuntimeError("private evidence required")
    environment = dict(os.environ)
    environment["PATH"] = "/root/.cargo/bin:" + environment.get("PATH", "")
    environment["CARGO_BUILD_JOBS"] = "4"
    environment["CARGO_TARGET_DIR"] = "/root/lx-target/arbiter-prestate/rust"
    environment["PAXEER_X_PROGRAM_REPLAY_EVIDENCE"] = str(EVIDENCE / "producer")
    if args.build:
        inputs = sources()
        execute([
            "flock", "/root/lx-cargo/native-build.lock", "make", "-j4",
            "BUILD_DIR=/root/lx-target/arbiter-prestate/native",
            "PROGRAMS_TARGET_DIR=/root/lx-target/arbiter-prestate/rust",
            "PROGRAMS_RUNTIME_LIB=/root/lx-target/arbiter-prestate/rust/debug/liblayerx_programs_sandbox.a",
            "paxeer-x-program-replay-build",
        ], EVIDENCE / "build-native.log", environment, 900)
        output = execute([
            "/root/.cargo/bin/cargo", "test", "--locked", "--manifest-path", "programs/Cargo.toml",
            "-p", "layerx-programs-arbiter", "--test", "verified_step", "--no-run", "--message-format=json",
        ], EVIDENCE / "build-arbiter.log", environment, 900)
        producer = json.loads((EVIDENCE / "producer/artifacts.json").read_text())
        binaries = {"native": producer["native"], "runtime": producer["rust"], "arbiter": artifact(output, "verified_step")}
        if inputs != sources():
            raise RuntimeError("source changed during the single task build")
        (EVIDENCE / "artifacts.json").write_text(json.dumps({"inputs": inputs, "binaries": {key: {"path": path, "sha256": sha(path)} for key, path in binaries.items()}}, indent=2))
        return 0
    manifest = json.loads((EVIDENCE / "artifacts.json").read_text())
    if manifest["inputs"] != sources():
        raise RuntimeError("source mismatch after build")
    for value in manifest["binaries"].values():
        if sha(value["path"]) != value["sha256"]:
            raise RuntimeError("genuine artifact changed")
    fixture = Path(tempfile.mkdtemp(prefix="native-", dir=EVIDENCE))
    output = execute([manifest["binaries"]["native"]["path"], str(fixture)], EVIDENCE / "verify-native.log", environment, 360)
    if "PROGRAM_REPLAY real-signed-serial-scheduled-trap-proof-reopen-refusal" not in output:
        raise RuntimeError("native serial/scheduled/trap/refusal/reopen corpus incomplete")
    environment["LAYERX_AUTHENTICATED_REPLAY_INPUTS"] = str(fixture / "replay-inputs.json")
    environment["LAYERX_ARBITER_ADMISSION_INPUTS"] = str(fixture / "inputs.json")
    execute([manifest["binaries"]["runtime"]["path"], "--nocapture", "--test-threads=1"], EVIDENCE / "verify-runtime.log", environment, 120)
    execute([manifest["binaries"]["arbiter"]["path"], "--nocapture", "--test-threads=1"], EVIDENCE / "verify-arbiter.log", environment, 360)
    if manifest["inputs"] != sources():
        raise RuntimeError("source changed during qualification")
    (EVIDENCE / "result.json").write_text(json.dumps({"command": "timeout 10m python3 tools/qualification/paxeer-x/authenticated-replay-step.py", "exit": 0, "fixture": str(fixture), "native_log": str(EVIDENCE / "verify-native.log"), "runtime_log": str(EVIDENCE / "verify-runtime.log"), "arbiter_log": str(EVIDENCE / "verify-arbiter.log")}, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
