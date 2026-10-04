#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
EVIDENCE = Path(os.environ.get("LAYERX_REPLAY_AUTHORITY_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043529"))
DECLARED = (
    "programs/crates/layerx-programs-arbiter/src/authority.rs",
    "programs/crates/layerx-programs-arbiter/src/step.rs",
    "programs/crates/layerx-programs-arbiter/src/lib.rs",
    "programs/crates/layerx-programs-arbiter/tests/authority_from_prestate.rs",
    "tests/daemon/lxp_test_replay_authority.c",
    "tools/paxeer-x/build/104.35.29.mk",
    "tools/qualification/paxeer-x/replay-authority.py",
)
SOURCE_PATHS = (
    "src", "include", "cmd/layerxd", "programs", "Makefile",
    "agent/crates/layerx-client", "agent/crates/layerx-proof",
    "agent/crates/layerx-types", "agent/crates/layerx-wire",
    "tests/daemon/lxp_test_arbiter_admission.c", "tests/daemon/lxp_test_program_replay.c",
    "tests/programs/test_call_activity.c", "tests/bridge/files.h",
    "tools/paxeer-x/build/104.35.20.mk", "tools/qualification/paxeer-x/program-replay-record.py",
)
NATIVE = Path("/root/lx-target/arbiter-prestate/native/tests/lxp_test_replay_authority")
MARKER = "REPLAY_AUTHORITY real-canonical-owner-signed-serial-scheduled-trap-proof-reopen-refusal"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    result = subprocess.run(["git", "ls-files", "-z", "--", *SOURCE_PATHS], cwd=ROOT, check=True, capture_output=True)
    names = {os.fsdecode(value) for value in result.stdout.split(b"\0") if value} | set(DECLARED)
    return {name: sha(ROOT / name) for name in sorted(names) if Path(name).suffix in {".rs", ".c", ".h", ".toml", ".lock", ".mk", ".py", ".json", ".inc"} or name == "Makefile"}


def prepare_fixture(path):
    module_path = ROOT / "tools/qualification/paxeer-x/program-replay-record.py"
    specification = importlib.util.spec_from_file_location("native_replay_producer", module_path)
    if specification is None or specification.loader is None:
        raise RuntimeError("actual native fixture generator unavailable")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    destination = Path(path)
    module.prepare(destination)
    source = destination.read_text()
    assignment = "executions[i].authority = &f->authority;"
    if source.count(assignment) != 2:
        raise RuntimeError("canonical authority resolver hook changed")
    source = source.replace(assignment, "CHECK(replay_authority_resolve(f, &activities[i], &executions[i], i) == 0);")
    destination.write_text(source)


def execute(command, log, environment, timeout):
    with log.open("w") as stream:
        try:
            result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
        except subprocess.TimeoutExpired:
            log.with_suffix(log.suffix + ".exit").write_text("124\n")
            print("exit=124 log=" + str(log), flush=True)
            raise
    log.with_suffix(log.suffix + ".exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def artifact(output):
    paths = set()
    for line in output.splitlines():
        if line.startswith("{"):
            item = json.loads(line)
            if item.get("reason") == "compiler-artifact" and item.get("target", {}).get("name") == "authority_from_prestate" and item.get("executable"):
                paths.add(item["executable"])
    if len(paths) != 1:
        raise RuntimeError("required genuine authority test artifact missing")
    return str(Path(paths.pop()).resolve(strict=True))


def exported_inputs(fixture):
    admission = json.loads((fixture / "inputs.json").read_text())
    replay = json.loads((fixture / "replay-inputs.json").read_text())
    if admission["network_id"] != 7 or replay["admission_inputs"] != "inputs.json":
        raise RuntimeError("actual signed network/admission binding missing")
    captures = replay["captures"]
    if len(captures) != 4 or [capture["receipt_index"] for capture in captures] != [0, 0, 1, 0]:
        raise RuntimeError("genuine canonical serial/scheduled/trap corpus incomplete")
    if captures[1]["batch"] != captures[2]["batch"] or not captures[0]["batch"] < captures[1]["batch"] < captures[3]["batch"]:
        raise RuntimeError("actual serial/scheduled/trap signed batch ordering missing")
    admissions = {Path(capture["receipt_path"]).name: capture for capture in admission["captures"]}
    required = ("v3_path", "v2_path", "v1_path", "receipt_path", "proof_path", "activity_path", "signing_preimage_path", "authority_key_path", "header_path", "header_signature_path", "maintenance_path", "maintenance_proof_path")
    outputs = {}
    for capture in captures:
        if capture["receipt"] not in admissions:
            raise RuntimeError("canonical replay has no genuine signed admission")
        paths = [fixture / capture[key] for key in ("metadata_proof", "witness", "root")]
        record = admissions[capture["receipt"]]
        paths.extend(Path(record[key]) for key in required)
        paths.extend(Path(value) for value in record["receipts"])
        for name in paths:
            path = name.resolve(strict=True)
            if path.parent != fixture.resolve() or path.stat().st_size == 0:
                raise RuntimeError("actual canonical evidence missing or escaped fixture")
            outputs[str(path)] = sha(path)
    return outputs


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--prepare-fixture")
    args = parser.parse_args()
    if args.prepare_fixture:
        if args.build:
            raise RuntimeError("fixture generation and build modes are exclusive")
        prepare_fixture(args.prepare_fixture)
        return 0
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    if EVIDENCE.stat().st_mode & 0o077:
        raise RuntimeError("private evidence required")
    environment = dict(os.environ)
    environment["PATH"] = "/root/.cargo/bin:" + environment.get("PATH", "")
    environment["CARGO_BUILD_JOBS"] = "4"
    environment["CARGO_TARGET_DIR"] = "/root/lx-target/arbiter-prestate/rust"
    if args.build:
        inputs = sources()
        execute([
            "flock", "/root/lx-cargo/native-build.lock", "make", "-j4",
            "BUILD_DIR=/root/lx-target/arbiter-prestate/native",
            "PROGRAMS_TARGET_DIR=/root/lx-target/arbiter-prestate/rust",
            "PROGRAMS_RUNTIME_LIB=/root/lx-target/arbiter-prestate/rust/debug/liblayerx_programs_sandbox.a",
            "paxeer-x-replay-authority-build",
        ], EVIDENCE / "build-native.log", environment, 900)
        output = execute([
            "/root/.cargo/bin/cargo", "test", "--locked", "--manifest-path", "programs/Cargo.toml",
            "-p", "layerx-programs-arbiter", "--test", "authority_from_prestate", "--no-run", "--message-format=json",
        ], EVIDENCE / "build-arbiter.log", environment, 900)
        binaries = {"native": str(NATIVE.resolve(strict=True)), "arbiter": artifact(output)}
        if inputs != sources():
            raise RuntimeError("source changed during single authority build")
        (EVIDENCE / "artifacts.json").write_text(json.dumps({
            "revision": subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True, capture_output=True, text=True).stdout.strip(),
            "inputs": inputs,
            "binaries": {name: {"path": path, "sha256": sha(path)} for name, path in binaries.items()},
        }, indent=2))
        return 0
    manifest = json.loads((EVIDENCE / "artifacts.json").read_text())
    if manifest["inputs"] != sources():
        raise RuntimeError("source mismatch after authority build")
    for value in manifest["binaries"].values():
        if sha(value["path"]) != value["sha256"]:
            raise RuntimeError("genuine authority artifact changed")
    fixture = Path(tempfile.mkdtemp(prefix="native-", dir=EVIDENCE))
    output = execute([manifest["binaries"]["native"]["path"], str(fixture)], EVIDENCE / "verify-native.log", environment, 240)
    if MARKER not in output:
        raise RuntimeError("genuine canonical authority corpus incomplete")
    inputs = exported_inputs(fixture)
    environment["LAYERX_AUTHENTICATED_REPLAY_INPUTS"] = str(fixture / "replay-inputs.json")
    environment["LAYERX_ARBITER_ADMISSION_INPUTS"] = str(fixture / "inputs.json")
    execute([manifest["binaries"]["arbiter"]["path"], "--nocapture", "--test-threads=1"], EVIDENCE / "verify-arbiter.log", environment, 300)
    if manifest["inputs"] != sources():
        raise RuntimeError("source changed during authority qualification")
    (EVIDENCE / "result.json").write_text(json.dumps({
        "revision": manifest["revision"],
        "command": "timeout 10m python3 tools/qualification/paxeer-x/replay-authority.py", "exit": 0,
        "fixture": str(fixture), "inputs": inputs,
        "native_log": str(EVIDENCE / "verify-native.log"), "arbiter_log": str(EVIDENCE / "verify-arbiter.log"),
    }, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
