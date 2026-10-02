#!/usr/bin/env python3
"""Produce and consume source-bound native execution-context qualification artifacts."""
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
TASK = "104.31.1"
SCHEMA = "paxeer-x.program-execution-context-build.v1"
MAKEFILE = "tools/qualification/paxeer-x/program_execution_context.mk"
NATIVE_CASES = (
    "native_context_fields",
    "native_context_refusals",
    "native_context_frame_restore",
    "native_context_reentry",
    "native_context_depth",
    "native_context_edges",
    "native_context_fanout",
    "native_context_visits",
    "native_context_metering",
    "native_context_exhaustion",
    "native_context_replay",
)
RUST_CASES = (
    "abi::context::tests::frozen_field_ids_and_encodings_are_canonical",
    "abi::context::tests::zero_protocol_fields_never_authenticate",
    "calls::context_tests::immediate_caller_is_owned_by_each_active_edge",
    "host::context::tests::actual_guest_without_authenticated_context_refuses_every_field",
    "host::context::tests::actual_graph_boundary_preserves_context_and_refuses_edge_sixty_five",
)
SOURCE_PATHS = (
    "Makefile", "tools/build/sanitizers.mk", "platform/Makefile.inc",
    "src", "include", "contracts/config", "programs/Cargo.toml",
    "programs/Cargo.lock", "programs/crates", "programs/sdk/rust", "programs/.cargo",
    "programs/vendor", "agent/Cargo.toml", "agent/Cargo.lock", "agent/crates",
    ".cargo", "rust-toolchain.toml", "tests/programs",
    "tools/qualification/paxeer-x/program_execution_context.py", MAKEFILE,
    "tools/paxeer-x/gates/104.31.1.sh",
)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def run(command, **kwargs):
    print("COMMAND " + json.dumps(command), flush=True)
    return subprocess.run(command, cwd=ROOT, check=True, stdin=subprocess.DEVNULL,
                          **kwargs)


def source():
    revision = run(["git", "rev-parse", "HEAD"], capture_output=True,
                   text=True).stdout.strip()
    dirty = run(["git", "status", "--porcelain=v1", "--untracked-files=normal"],
                capture_output=True, text=True).stdout
    if dirty:
        raise RuntimeError("source checkout must be clean before build and verification")
    names = run(["git", "ls-files", "-z", "--", *SOURCE_PATHS],
                capture_output=True).stdout.split(b"\0")
    paths = [os.fsdecode(name) for name in names if name]
    if not paths:
        raise RuntimeError("empty source dependency inventory")
    return {"revision": revision,
            "inputs": {name: digest(ROOT / name) for name in sorted(paths)}}


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
    toolchain = run(["rustc", "+1.91.1", "-vV"], capture_output=True,
                    text=True).stdout
    if not toolchain.startswith("rustc 1.91.1 "):
        raise RuntimeError("required Rust 1.91.1 toolchain unavailable")
    compiler = run([*shlex.split(arguments.cc), "--version"], capture_output=True,
                   text=True).stdout
    commands = []

    def produce(command, **kwargs):
        commands.append(command)
        return run(command, env=environment, **kwargs)

    produce(["cargo", "+1.91.1", "build", "--locked", "--manifest-path",
             "programs/Cargo.toml", "-p", "layerx-programs-sandbox", "--features", "host-ffi"])
    sandbox = target / "debug/liblayerx_programs_sandbox.a"
    binary = build_dir / "tests/programs_execution_context"
    binary.unlink(missing_ok=True)
    produce(["make", "-j5", "-f", "Makefile", "-f", MAKEFILE,
             "BUILD_DIR=" + str(build_dir), "CC=" + arguments.cc,
             "LXP_REVISION=" + candidate["revision"],
             "PAXEER_CONTEXT_RUNTIME_LIB=" + str(sandbox),
             "paxeer-x-native-104.31.1"])
    rust_command = ["cargo", "+1.91.1", "test", "--locked", "--manifest-path",
                    "programs/Cargo.toml", "--no-run", "-p", "layerx-programs-runtime",
                    "--lib", "--message-format=json"]
    try:
        completed = produce(rust_command, capture_output=True, text=True)
    except subprocess.CalledProcessError as error:
        (evidence / ("task-" + TASK + "-cargo.jsonl")).write_text(error.stdout or "")
        sys.stderr.write(error.stderr or "")
        raise
    (evidence / ("task-" + TASK + "-cargo.jsonl")).write_text(completed.stdout)
    sys.stderr.write(completed.stderr)
    executables = set()
    for line in completed.stdout.splitlines():
        item = json.loads(line)
        if (item.get("reason") == "compiler-artifact"
                and item.get("target", {}).get("name") == "layerx_programs_runtime"
                and item.get("profile", {}).get("test") is True and item.get("executable")):
            executables.add(item["executable"])
    if len(executables) != 1:
        raise RuntimeError("producer did not emit exactly one runtime library test executable")
    guests = build_dir / ("paxeer-x-104.31.1-wasm-" + str(time.time_ns()))
    guests.mkdir(parents=True, exist_ok=False)
    produce([str(binary), "--emit-wasm", str(guests)])
    guest_paths = sorted(guests.glob("*.wasm"))
    if not guest_paths or any(path.read_bytes()[:8] != b"\0asm\x01\0\0\0" for path in guest_paths):
        raise RuntimeError("producer emitted no valid Wasm fixture inventory")
    headers = sorted((build_dir / "generated").glob("*.h"))
    if not headers:
        raise RuntimeError("native producer emitted no required generated headers")
    if source() != candidate:
        raise RuntimeError("source changed during producer execution")
    write_json(manifest_path, {
        "schema": SCHEMA, "task": TASK, "source": candidate,
        "toolchain": toolchain, "compiler": compiler,
        "features": {"sandbox": ["host-ffi"], "runtime_tests": []},
        "commands": commands, "native_cases": list(NATIVE_CASES),
        "rust_cases": list(RUST_CASES), "native": path_record(binary),
        "rust": path_record(executables.pop()), "sandbox": path_record(sandbox),
        "library": path_record(build_dir / "liblayerx.a"),
        "headers": [path_record(path) for path in headers],
        "guests": [path_record(path) for path in guest_paths],
    })
    print("Build provenance: " + str(manifest_path))


def qualify(evidence):
    manifest_path = evidence / ("task-" + TASK + "-build.json")
    manifest = json.loads(manifest_path.read_text())
    candidate = source()
    if (manifest.get("schema") != SCHEMA or manifest.get("task") != TASK
            or manifest.get("source") != candidate
            or manifest.get("native_cases") != list(NATIVE_CASES)
            or manifest.get("rust_cases") != list(RUST_CASES)
            or manifest.get("features") != {"sandbox": ["host-ffi"], "runtime_tests": []}
            or not manifest.get("toolchain", "").startswith("rustc 1.91.1 ")):
        raise RuntimeError("producer provenance does not bind this exact source and required inventory")
    native = checked_artifact(manifest["native"])
    rust = checked_artifact(manifest["rust"])
    checked_artifact(manifest["sandbox"])
    checked_artifact(manifest["library"])
    if not manifest["headers"] or not manifest["guests"]:
        raise RuntimeError("producer artifact inventory is incomplete")
    for item in manifest["headers"]:
        checked_artifact(item)
    guests = [checked_artifact(item) for item in manifest["guests"]]
    guest_directory = guests[0].parent
    if (any(path.parent != guest_directory for path in guests)
            or set(guest_directory.glob("*.wasm")) != set(guests)):
        raise RuntimeError("prebuilt guest inventory changed")
    inventory = run([str(native), "--list-cases"], capture_output=True, text=True).stdout.splitlines()
    if inventory != list(NATIVE_CASES):
        raise RuntimeError("native executable inventory does not exactly match acceptance cases")
    stamp = str(time.time_ns())
    results = []
    for index, name in enumerate(NATIVE_CASES + RUST_CASES):
        if index < len(NATIVE_CASES):
            command = [str(native), "--case", name, "--wasm-dir", str(guest_directory)]
        else:
            command = [str(rust), "--exact", name, "--nocapture", "--test-threads=1"]
        log_path = evidence / ("task-" + TASK + "-" + stamp + "-" + str(index) + ".log")
        with log_path.open("w") as log:
            completed = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                                       stdout=log, stderr=subprocess.STDOUT, timeout=300)
        output = log_path.read_text()
        results.append({"case": name, "command": command,
                        "exit_code": completed.returncode, "log": str(log_path)})
        write_json(evidence / ("task-" + TASK + "-" + stamp + "-result.json"),
                   {"source": candidate, "cases": results, "complete": False})
        if completed.returncode:
            raise RuntimeError(name + " failed; log: " + str(log_path))
        if index < len(NATIVE_CASES):
            if re.findall(r"^PASS (\S+)$", output, re.M) != [name]:
                raise RuntimeError("missing, duplicate or unexpected native success: " + name)
        elif (re.findall(r"^test (\S+) \.\.\. ok$", output, re.M) != [name]
              or not re.search(r"^test result: ok\. 1 passed; 0 failed; 0 ignored;", output, re.M)):
            raise RuntimeError("retained unit case was absent, ignored or failed: " + name)
    if source() != candidate:
        raise RuntimeError("source changed during qualification")
    for item in [manifest["native"], manifest["rust"], manifest["sandbox"],
                 manifest["library"], *manifest["headers"], *manifest["guests"]]:
        checked_artifact(item)
    write_json(evidence / ("task-" + TASK + "-" + stamp + "-result.json"),
               {"source": candidate, "cases": results, "complete": True})
    print("PAXEER_X_GATE tests=" + str(len(results)) + " skipped=0")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
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
    except (OSError, ValueError, KeyError, TypeError, RuntimeError,
            subprocess.SubprocessError) as error:
        print("execution-context: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
