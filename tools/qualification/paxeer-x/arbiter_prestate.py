#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.35.11"
SCHEMA = "layerx.arbiter-prestate-build.v2"
DECLARED = (
    "src/protocol/lxp_kernel.c", "include/layerx/lxp_kernel.h",
    "agent/crates/layerx-client/src/evidence/arbiter_prestate.rs",
    "agent/crates/layerx-client/src/evidence.rs",
    "tests/daemon/lxp_test_arbiter_prestate.c",
    "agent/crates/layerx-client/tests/arbiter_prestate.rs",
    "tools/paxeer-x/build/104.35.11.mk",
    "tools/qualification/paxeer-x/arbiter_prestate.py",
)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def execute(command, log, environment=None):
    print("COMMAND " + json.dumps(command), flush=True)
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=ROOT, env=environment,
                                stdin=subprocess.DEVNULL, stdout=stream,
                                stderr=subprocess.STDOUT, timeout=1100)
    print("EXIT " + str(result.returncode) + " LOG " + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def evidence_dir():
    path = Path(os.environ.get("PAXEER_X_ARBITER_PRESTATE_EVIDENCE",
                "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043511"))
    path.mkdir(parents=True, mode=0o700, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    if ROOT == path or ROOT in path.parents or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError("private caller-owned evidence outside checkout required")
    return path


def inputs():
    names = subprocess.run(["git", "ls-files", "-z", "--", "Makefile", "src", "include",
                            "contracts/config", "programs/Cargo.toml", "programs/Cargo.lock",
                            "programs/crates", "programs/sdk/rust", "programs/vendor",
                            "agent/Cargo.toml", "agent/Cargo.lock",
                            "agent/crates/layerx-client", "agent/crates/layerx-proof",
                            "agent/crates/layerx-types", "agent/crates/layerx-wire",
                            "agent/crates/layerx-crypto", "agent/crates/layerx-paxeer-verifier",
                            "cmd/layerxd", "tests/storage/lxp_test_maintenance_publication.c",
                            "tests/programs/test_call_activity.c", "tests/bridge/files.h",
                            "rust-toolchain.toml", ".cargo"], cwd=ROOT, check=True,
                           capture_output=True).stdout.split(b"\0")
    paths = {os.fsdecode(name) for name in names if name}
    paths.update(DECLARED)
    paths = {name for name in paths if Path(name).suffix in
             {".c", ".h", ".rs", ".toml", ".lock", ".json", ".inc", ".mk", ".py"}
             or name == "Makefile"}
    return {name: digest(ROOT / name) for name in sorted(paths)}


def record(path):
    path = Path(path).resolve(strict=True)
    return {"path": str(path), "sha256": digest(path)}


def checked(recorded):
    path = Path(recorded["path"])
    if not path.is_file() or digest(path) != recorded["sha256"]:
        raise RuntimeError("missing or changed declared artifact: " + str(path))
    return path


def save(path, value):
    with path.open("w") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
    path.chmod(0o600)


def build(arguments, evidence):
    manifest_path = evidence / "artifacts.json"
    manifest_path.unlink(missing_ok=True)
    source = inputs()
    directory = Path(arguments.build_dir).resolve()
    native_build = directory / "native"
    rust_target = directory / "rust"
    native = native_build / "tests/lxp_test_arbiter_prestate"
    native.parent.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ, CARGO_TARGET_DIR=str(rust_target), CARGO_BUILD_JOBS="4")
    cargo = shlex.split(arguments.cargo)
    execute([*cargo, "build", "--locked", "--manifest-path", "programs/Cargo.toml",
             "-p", "layerx-programs-sandbox", "--lib", "--features", "host-ffi"],
            evidence / "build-sandbox.log", environment)
    library = native_build / "liblayerx.a"
    execute(["make", "-j4", "BUILD_DIR=" + str(native_build), "CC=" + arguments.cc,
             str(library)], evidence / "build-native-library.log", environment)
    sandbox = rust_target / "debug/liblayerx_programs_sandbox.a"
    compile_command = [*shlex.split(arguments.cc), "-I" + str(native_build / "generated"),
                       *shlex.split(arguments.cppflags), "-Icmd/layerxd",
                       *shlex.split(arguments.cflags), "tests/daemon/lxp_test_arbiter_prestate.c",
                       "cmd/layerxd/lxp_daemon_batch_wal.c",
                       "cmd/layerxd/lxp_daemon_receipt_authority.c",
                       "cmd/layerxd/lxp_daemon_evidence.c", str(library), str(sandbox),
                       str(library), *shlex.split(arguments.ldflags), "-lcrypto", "-pthread",
                       "-lsqlite3", "-ldl", "-lm", "-o", str(native)]
    execute(compile_command, evidence / "build-native-fixture.log", environment)
    output = execute([*cargo, "test", "--locked", "--manifest-path", "agent/Cargo.toml",
                      "-p", "layerx-client", "--test", "arbiter_prestate", "--no-run",
                      "--message-format=json"], evidence / "build-client-test.log", environment)
    executables = set()
    for line in output.splitlines():
        if not line.startswith("{"):
            continue
        item = json.loads(line)
        if (item.get("reason") == "compiler-artifact" and item.get("executable")
                and item.get("target", {}).get("name") == "arbiter_prestate"
                and item.get("profile", {}).get("test") is True):
            executables.add(item["executable"])
    if len(executables) != 1:
        raise RuntimeError("exactly one declared prestate test executable required")
    if inputs() != source:
        raise RuntimeError("source inputs changed during task build")
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True,
                              capture_output=True, text=True).stdout.strip()
    save(manifest_path, {"schema": SCHEMA, "task": TASK, "revision": revision,
                        "inputs": source, "native": record(native),
                        "rust": record(executables.pop()), "library": record(library),
                        "sandbox": record(sandbox), "compile_command": compile_command})
    print("ARTIFACTS " + str(manifest_path), flush=True)


def qualify(evidence):
    manifest = json.loads((evidence / "artifacts.json").read_text())
    if manifest.get("schema") != SCHEMA or manifest.get("task") != TASK or manifest.get("inputs") != inputs():
        raise RuntimeError("declared producer does not bind current source")
    native = checked(manifest["native"])
    rust = checked(manifest["rust"])
    checked(manifest["library"])
    checked(manifest["sandbox"])
    fixture = evidence / ("native-inputs-" + str(time.time_ns()))
    fixture.mkdir(mode=0o700, exist_ok=False)
    native_output = execute([str(native), str(fixture)], evidence / "verify-native.log")
    inventory = fixture / "inputs.json"
    if not inventory.is_file():
        raise RuntimeError("native producer did not emit its genuine input inventory")
    captures = json.loads(inventory.read_text()).get("captures", [])
    required = {"serial-empty-0", "scheduled-0", "scheduled-1", "terminal-0"}
    if len(captures) != len(required) or {item.get("name") for item in captures} != required:
        raise RuntimeError("complete serial, scheduled and terminal native fixture inventory required")
    for marker in ("ARBITER_CASE native-actual-boundaries-legacy-bytes-ownership-bounds",
                   "ARBITER_CASE native-serial-scheduled-terminal-maintained-signatures-wal-recovery"):
        if marker not in native_output:
            raise RuntimeError("native producer did not execute required boundary assertions")
    environment = dict(os.environ, LAYERX_ARBITER_PRESTATE_INPUTS=str(inventory))
    listed = execute([str(rust), "--list"], evidence / "verify-rust-list.log", environment)
    names = [line[:-6] for line in listed.splitlines() if line.endswith(": test")]
    if not names or any("ignored" in line and not line.startswith("0 ") for line in listed.splitlines()):
        raise RuntimeError("no complete real Rust fixture test inventory")
    output = execute([str(rust), "--nocapture", "--test-threads=1"], evidence / "verify-rust.log", environment)
    expected = "test result: ok. " + str(len(names)) + " passed; 0 failed; 0 ignored; 0 measured; 0 filtered out"
    if expected not in output:
        raise RuntimeError("declared real fixture assertions were skipped or not all executed")
    if inputs() != manifest["inputs"]:
        raise RuntimeError("source changed during task verification")
    save(evidence / "result.json", {"task": TASK, "revision": manifest["revision"],
                                   "command": "timeout 20m python3 tools/qualification/paxeer-x/arbiter_prestate.py",
                                   "exit": 0, "rust_tests": names,
                                   "native_log": str(evidence / "verify-native.log"),
                                   "rust_log": str(evidence / "verify-rust.log")})
    print("VERIFIED " + str(evidence / "result.json"), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--build-dir", default="/root/lx-target/arbiter-prestate")
    parser.add_argument("--cc", default="cc")
    parser.add_argument("--cppflags", default="-Iinclude")
    parser.add_argument("--cflags", default="-std=c17 -pedantic -Werror -Wall -Wextra -Wconversion -Wshadow -Wvla -O2")
    parser.add_argument("--ldflags", default="")
    parser.add_argument("--cargo", default="cargo")
    arguments = parser.parse_args()
    evidence = evidence_dir()
    if arguments.build:
        build(arguments, evidence)
    else:
        qualify(evidence)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("FAILED " + str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
