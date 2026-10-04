#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.32.4"
SCHEMA = "paxeer-x.program-fee-governance-build.v1"
MAKEFILE = "tools/paxeer-x/build/104.32.4.mk"
NATIVE_CASES = (
    "native_pending_and_history", "native_occupancy_up", "native_occupancy_down",
    "native_occupancy_target", "native_occupancy_full_width",
    "native_activation_and_authority_refusals",
    "native_signed_governance_producer_consumer",
)
FEE_CASES = (
    "recorded_schedule_prices_real_meter_without_current_head_fallback",
    "occupancy_movement_is_monotonic_bounded_and_preserves_other_prices",
    "governance_observations_append_only_effective_versions",
    "invalid_coefficients_and_nonconsecutive_history_refuse_without_mutation",
    "invalid_demand_policies_and_wrong_next_version_refuse",
    "full_width_occupancy_and_fractional_rounding_are_exact",
    "version_exhaustion_keeps_history_intact",
)
REPLAY_CASES = (
    "governed_fee_history_reprices_each_recorded_version_exactly",
    "mixed_v1_v2_history_selects_each_recorded_abi_and_fee_schedule",
)
SOURCE_PATHS = (
    "Makefile", "tools/build/sanitizers.mk", "platform/Makefile.inc",
    "src", "include", "contracts/config", "programs/Cargo.toml", "programs/Cargo.lock",
    "programs/crates", "programs/sdk/rust", "programs/.cargo", "programs/vendor",
    ".cargo", "rust-toolchain.toml", "tests/programs/test_fee_governance.c",
    "tools/qualification/paxeer-x/program_fee_governance.py", MAKEFILE,
    "tools/paxeer-x/gates/104.32.4.sh",
)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def run(command, **kwargs):
    print("COMMAND " + json.dumps(command), flush=True)
    return subprocess.run(command, cwd=ROOT, check=True, stdin=subprocess.DEVNULL, **kwargs)


def source():
    revision = run(["git", "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
    dirty = run(["git", "status", "--porcelain=v1", "--untracked-files=normal"],
                capture_output=True, text=True).stdout
    if dirty:
        raise RuntimeError("source checkout must be clean before build and verification")
    names = run(["git", "ls-files", "-z", "--", *SOURCE_PATHS], capture_output=True).stdout.split(b"\0")
    paths = sorted(os.fsdecode(name) for name in names
                   if name and not Path(os.fsdecode(name)).name.startswith(".env"))
    if not paths:
        raise RuntimeError("empty source dependency inventory")
    return {"revision": revision, "inputs": {name: digest(ROOT / name) for name in paths}}


def evidence_dir():
    raw = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if not raw:
        raise RuntimeError("PAXEER_X_EVIDENCE_DIR is required")
    path = Path(raw).resolve(strict=True)
    info = path.stat()
    if (ROOT == path or ROOT in path.parents or not stat.S_ISDIR(info.st_mode)
            or info.st_uid != os.geteuid() or info.st_mode & 0o077):
        raise RuntimeError("evidence directory must be private, caller-owned and outside checkout")
    return path


def write_json(path, value):
    with path.open("w", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
    path.chmod(0o600)


def path_record(path):
    path = Path(path).resolve(strict=True)
    try:
        name = str(path.relative_to(ROOT))
    except ValueError:
        name = str(path)
    return {"path": name, "sha256": digest(path)}


def checked_artifact(record):
    path = ROOT / record["path"]
    if not path.is_file() or digest(path) != record["sha256"]:
        raise RuntimeError("missing or changed prebuilt artifact: " + str(path))
    return path


def build(arguments, evidence):
    manifest_path = evidence / ("task-" + TASK + "-build.json")
    manifest_path.unlink(missing_ok=True)
    candidate = source()
    build_dir = Path(arguments.build_dir)
    if not build_dir.is_absolute():
        build_dir = ROOT / build_dir
    build_dir = build_dir.resolve()
    target = Path(os.environ.get("CARGO_TARGET_DIR", str(ROOT / "programs/target"))).resolve()
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target))
    toolchain = run(["rustc", "+1.91.1", "-vV"], capture_output=True, text=True).stdout
    if not toolchain.startswith("rustc 1.91.1 "):
        raise RuntimeError("required Rust 1.91.1 toolchain unavailable")
    compiler = run([*shlex.split(arguments.cc), "--version"], capture_output=True, text=True).stdout
    commands = []

    def produce(command, **kwargs):
        commands.append(command)
        return run(command, env=environment, **kwargs)

    produce(["cargo", "+1.91.1", "build", "--locked", "--manifest-path",
             "programs/Cargo.toml", "-p", "layerx-programs-sandbox", "--features", "host-ffi"])
    sandbox = target / "debug/liblayerx_programs_sandbox.a"
    binary = build_dir / "tests/programs_fee_governance_104_32_4"
    binary.unlink(missing_ok=True)
    produce(["make", "-j5", "-f", "Makefile", "-f", MAKEFILE,
             "BUILD_DIR=" + str(build_dir), "CC=" + arguments.cc,
             "LXP_REVISION=" + candidate["revision"], "PAXEER_FEE_RUNTIME_LIB=" + str(sandbox),
             "paxeer-x-native-104.32.4"])
    command = ["cargo", "+1.91.1", "test", "--locked", "--manifest-path", "programs/Cargo.toml",
               "--no-run", "-p", "layerx-programs-runtime", "--test", "fee_governance",
               "--test", "replay", "--message-format=json"]
    try:
        completed = produce(command, capture_output=True, text=True)
    except subprocess.CalledProcessError as error:
        (evidence / ("task-" + TASK + "-cargo.jsonl")).write_text(error.stdout or "")
        sys.stderr.write(error.stderr or "")
        raise
    (evidence / ("task-" + TASK + "-cargo.jsonl")).write_text(completed.stdout)
    sys.stderr.write(completed.stderr)
    executables = {name: set() for name in ("fee_governance", "replay")}
    for line in completed.stdout.splitlines():
        item = json.loads(line)
        name = item.get("target", {}).get("name")
        if (item.get("reason") == "compiler-artifact" and name in executables
                and item.get("profile", {}).get("test") is True and item.get("executable")):
            executables[name].add(item["executable"])
    if any(len(paths) != 1 for paths in executables.values()):
        raise RuntimeError("producer did not emit exactly one executable for each required Rust target")
    headers = sorted((build_dir / "generated").glob("*.h"))
    if not headers:
        raise RuntimeError("native producer emitted no required generated headers")
    if source() != candidate:
        raise RuntimeError("source changed during producer execution")
    write_json(manifest_path, {
        "schema": SCHEMA, "task": TASK, "source": candidate,
        "toolchain": toolchain, "compiler": compiler,
        "features": {"sandbox": ["host-ffi"], "runtime_tests": []}, "commands": commands,
        "native_cases": list(NATIVE_CASES), "fee_cases": list(FEE_CASES), "replay_cases": list(REPLAY_CASES),
        "native": path_record(binary), "sandbox": path_record(sandbox),
        "fee": path_record(executables["fee_governance"].pop()),
        "replay": path_record(executables["replay"].pop()),
        "library": path_record(build_dir / "liblayerx.a"),
        "headers": [path_record(path) for path in headers],
    })
    print("Build provenance: " + str(manifest_path))


def qualify(evidence):
    manifest = json.loads((evidence / ("task-" + TASK + "-build.json")).read_text())
    candidate = source()
    if (manifest.get("schema") != SCHEMA or manifest.get("task") != TASK
            or manifest.get("source") != candidate or manifest.get("native_cases") != list(NATIVE_CASES)
            or manifest.get("fee_cases") != list(FEE_CASES) or manifest.get("replay_cases") != list(REPLAY_CASES)
            or manifest.get("features") != {"sandbox": ["host-ffi"], "runtime_tests": []}
            or not manifest.get("toolchain", "").startswith("rustc 1.91.1 ")):
        raise RuntimeError("producer provenance does not bind this source and required inventory")
    artifacts = [manifest[name] for name in ("native", "fee", "replay", "sandbox", "library")]
    if not manifest["headers"]:
        raise RuntimeError("producer artifact inventory is incomplete")
    artifacts += manifest["headers"]
    for item in artifacts:
        checked_artifact(item)
    native = checked_artifact(manifest["native"])
    inventory = run([str(native), "--list-cases"], capture_output=True, text=True).stdout.splitlines()
    if inventory != list(NATIVE_CASES):
        raise RuntimeError("native acceptance inventory differs from declared cases")
    rust_inventory = run([str(checked_artifact(manifest["fee"])), "--list"],
                         capture_output=True, text=True).stdout
    if sorted(re.findall(r"^(\S+): test$", rust_inventory, re.M)) != sorted(FEE_CASES):
        raise RuntimeError("focused fee acceptance inventory differs from declared cases")
    stamp = str(time.time_ns())
    results = []
    cases = [("native", name) for name in NATIVE_CASES]
    cases += [("fee", name) for name in FEE_CASES] + [("replay", name) for name in REPLAY_CASES]
    result_path = evidence / ("task-" + TASK + "-" + stamp + "-result.json")
    for index, (kind, name) in enumerate(cases):
        binary = checked_artifact(manifest[kind])
        command = ([str(binary), "--case", name] if kind == "native" else
                   [str(binary), "--exact", name, "--nocapture", "--test-threads=1"])
        log_path = evidence / ("task-" + TASK + "-" + stamp + "-" + str(index) + ".log")
        with log_path.open("w") as log:
            completed = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                                       stdout=log, stderr=subprocess.STDOUT, timeout=300)
        output = log_path.read_text()
        results.append({"case": name, "command": command, "exit_code": completed.returncode,
                        "log": str(log_path)})
        write_json(result_path, {"source": candidate, "cases": results, "complete": False})
        if completed.returncode:
            raise RuntimeError(name + " failed; log: " + str(log_path))
        if kind == "native":
            if re.findall(r"^PASS (\S+)$", output, re.M) != [name]:
                raise RuntimeError("missing, duplicate or unexpected native success: " + name)
        elif (re.findall(r"^test (\S+) \.\.\. ok$", output, re.M) != [name]
              or not re.search(r"^test result: ok\. 1 passed; 0 failed; 0 ignored;", output, re.M)):
            raise RuntimeError("retained Rust case was absent, ignored or failed: " + name)
    unknown = subprocess.run([str(native), "--case", "unknown_fee_governance_case"], cwd=ROOT,
                             stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
    if unknown.returncode != 2 or "PASS " in unknown.stdout:
        raise RuntimeError("native unknown selector did not refuse without executing a case")
    if source() != candidate:
        raise RuntimeError("source changed during qualification")
    for item in artifacts:
        checked_artifact(item)
    write_json(result_path, {"source": candidate, "cases": results,
                            "unknown_selector_exit_code": unknown.returncode, "complete": True})
    print("PAXEER_X_GATE tests=" + str(len(results) + 1) + " skipped=0")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--build-dir", default="build")
    parser.add_argument("--cc", default=os.environ.get("CC", "cc"))
    arguments = parser.parse_args()
    os.umask(0o077)
    try:
        evidence = evidence_dir()
        if arguments.build:
            build(arguments, evidence)
        else:
            qualify(evidence)
        return 0
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
        print("fee-governance: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
