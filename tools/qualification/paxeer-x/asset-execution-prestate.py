#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import shlex
import shutil
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.13.15"
SCHEMA = "layerx.asset-execution-prestate.v1"
TEST = "real_native_asset_execution_prestate_transport"
DECLARED = (
    "tests/daemon/lxp_test_asset_execution_prestate.c",
    "agent/crates/layerx-client/tests/asset_execution_prestate.rs",
    "agent/crates/layerx-client/src/execution_prestate.rs",
    "agent/crates/layerx-client/src/evidence/execution_prestate.rs",
    "tools/qualification/paxeer-x/asset-execution-prestate.py",
)


def digest(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def evidence_directory():
    directory = Path(os.environ.get("PAXEER_X_ASSET_EXECUTION_PRESTATE_EVIDENCE",
        "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1041315"))
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory = directory.resolve(strict=True)
    info = directory.stat()
    if ROOT == directory or ROOT in directory.parents or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError("private caller-owned evidence outside checkout required")
    return directory


def inputs():
    inventory = subprocess.run(["git", "ls-files", "-z", "--", "Makefile", "src", "include",
        "cmd/layerxd", "cmd/layerx-guarantor", "agent/Cargo.toml", "agent/Cargo.lock",
        "agent/crates/layerx-client", "agent/crates/layerx-proof", "agent/crates/layerx-wire",
        "agent/crates/layerx-types", "agent/crates/layerx-crypto", "agent/crates/layerx-paxeer-verifier",
        "agent/schema/lni", "programs/Cargo.toml", "programs/Cargo.lock", "programs/crates",
        "programs/sdk/rust", "tests/daemon/lxp_test_arbiter_prestate.c",
        "tests/programs/test_call_activity.c", "tests/bridge/files.h", "contracts/config",
        "tools/paxeer-x/build", ".cargo", "rust-toolchain.toml"], cwd=ROOT,
        check=True, capture_output=True).stdout.split(b"\0")
    paths = {os.fsdecode(path) for path in inventory if path} | set(DECLARED)
    return {path: digest(ROOT / path) for path in sorted(paths)
        if Path(path).suffix in {".c", ".h", ".rs", ".toml", ".lock", ".kvx", ".json", ".mk", ".py"}
        or path == "Makefile"}


def save(path, value):
    with path.open("w") as output:
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")
    path.chmod(0o600)


def artifact(path):
    path = Path(path).resolve(strict=True)
    return {"path": str(path), "sha256": digest(path)}


def checked(value):
    path = Path(value["path"])
    if not path.is_file() or digest(path) != value["sha256"]:
        raise RuntimeError("missing or changed declared artifact")
    return path


def build(arguments, evidence):
    manifest = evidence / "artifacts.json"
    manifest.unlink(missing_ok=True)
    source = inputs()
    native_make = evidence / "build-native.mk"
    native_make.write_text('''.PHONY: paxeer-x-asset-execution-prestate-build
paxeer-x-asset-execution-prestate-build: layerxd $(BUILD_DIR)/tests/lxp_test_asset_execution_prestate
$(BUILD_DIR)/tests/lxp_test_asset_execution_prestate: tests/daemon/lxp_test_asset_execution_prestate.c tests/programs/test_call_activity.c $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
\t@mkdir -p $(@D)
\t$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd $< $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
''')
    native_make.chmod(0o600)
    native_command = ["make", "-f", "Makefile", "-f", str(native_make), "-j4",
        "BUILD_DIR=/root/lx-target/arbiter-prestate/native",
        "PROGRAMS_TARGET_DIR=/root/lx-target/arbiter-prestate/rust",
        "PROGRAMS_RUNTIME_LIB=/root/lx-target/arbiter-prestate/rust/debug/liblayerx_programs_sandbox.a",
        "paxeer-x-asset-execution-prestate-build"]
    with (evidence / "build-native.log").open("w") as output:
        result = subprocess.run(native_command, cwd=ROOT, stdin=subprocess.DEVNULL,
            stdout=output, stderr=subprocess.STDOUT, timeout=1100)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, native_command)
    arguments.native = "/root/lx-target/arbiter-prestate/native/tests/lxp_test_asset_execution_prestate"
    arguments.daemon = "/root/lx-target/arbiter-prestate/native/bin/layerxd"
    command = [*shlex.split(arguments.cargo), "test", "--locked", "--manifest-path", "agent/Cargo.toml",
        "-p", "layerx-client", "--test", "asset_execution_prestate", "--no-run", "--message-format=json"]
    environment = dict(os.environ, CARGO_TARGET_DIR="/root/lx-target/agent", CARGO_BUILD_JOBS="4")
    log = evidence / "build-client.log"
    with log.open("w") as output:
        result = subprocess.run(command, cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, timeout=1100)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    executables = set()
    for line in log.read_text().splitlines():
        if not line.startswith("{"):
            continue
        value = json.loads(line)
        if value.get("reason") == "compiler-artifact" and value.get("executable") and value.get("target", {}).get("name") == "asset_execution_prestate":
            executables.add(value["executable"])
    if len(executables) != 1 or inputs() != source:
        raise RuntimeError("one declared client artifact and unchanged build source required")
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT,
        check=True, capture_output=True, text=True).stdout.strip()
    save(manifest, {"schema": SCHEMA, "task": TASK, "revision": revision,
        "inputs": source, "native": artifact(arguments.native), "daemon": artifact(arguments.daemon),
        "rust": artifact(executables.pop())})
    print("ARTIFACTS " + str(manifest), flush=True)


def become_client():
    os.setgroups([])
    os.setgid(65534)
    os.setuid(65534)


def qualify(evidence):
    if os.geteuid() != 0:
        raise RuntimeError("distinct owner and admitted service UID qualification requires root")
    manifest = json.loads((evidence / "artifacts.json").read_text())
    if manifest.get("schema") != SCHEMA or manifest.get("task") != TASK or manifest.get("inputs") != inputs():
        raise RuntimeError("declared artifacts do not bind current source")
    native, rust = checked(manifest["native"]), checked(manifest["rust"])
    checked(manifest["daemon"])
    fixture = Path(tempfile.mkdtemp(prefix="lxp-arbiter-transport-"))
    os.chown(fixture, 0, 65534)
    fixture.chmod(0o750)
    executable = fixture / "client-probe"
    shutil.copyfile(rust, executable)
    os.chown(executable, 0, 65534)
    executable.chmod(0o750)
    if digest(executable) != manifest["rust"]["sha256"]:
        raise RuntimeError("client executable copy changed")
    events = queue.Queue()
    native_log = evidence / "verify-native.log"
    stream = native_log.open("w")
    process = subprocess.Popen([str(native), str(fixture)], cwd=ROOT,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)

    def capture():
        for line in process.stdout:
            stream.write(line)
            stream.flush()
            if line.startswith("ASSET_PRESTATE_TRANSPORT_READY "):
                events.put(line.strip())
        events.put("EXIT")

    reader = threading.Thread(target=capture, daemon=True)
    reader.start()
    try:
        for phase in ("before", "after"):
            if phase == "after":
                process.stdin.write("reopen\n")
                process.stdin.flush()
            marker = events.get(timeout=120)
            if marker != "ASSET_PRESTATE_TRANSPORT_READY " + phase:
                raise RuntimeError("genuine native boundary failed before " + phase + ": " + str(native_log))
            captures = json.loads((fixture / "inputs.json").read_text()).get("captures", [])
            if {item.get("name") for item in captures} != {"asset0", "asset1"}:
                raise RuntimeError("complete genuine serial/scheduled/terminal corpus required")
            for path in fixture.iterdir():
                if path.is_file() and not path.is_socket():
                    os.chown(path, 0, 65534)
                    path.chmod(0o750 if path == executable else 0o640)
            command = [str(executable), TEST, "--exact", "--nocapture", "--test-threads=1"]
            environment = {"LAYERX_ASSET_EXECUTION_PRESTATE_INPUTS": str(fixture / "inputs.json"),
                "LAYERX_ASSET_EXECUTION_PRESTATE_PHASE": phase, "RUST_BACKTRACE": "0"}
            log = evidence / ("verify-client-" + phase + ".log")
            with log.open("w") as output:
                result = subprocess.run(command, cwd=fixture, env=environment, preexec_fn=become_client,
                    stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, timeout=120)
            if result.returncode:
                raise subprocess.CalledProcessError(result.returncode, command)
            if "test result: ok. 1 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out" not in log.read_text():
                raise RuntimeError("genuine client assertions skipped or incomplete")
        process.stdin.write("stop\n")
        process.stdin.flush()
        if process.wait(timeout=30) != 0:
            raise RuntimeError("native shutdown failed: " + str(native_log))
        reader.join(timeout=5)
        if "ASSET_PRESTATE_TRANSPORT native-retained-reopened-authenticated-boundary" not in native_log.read_text():
            raise RuntimeError("native capture/reopen boundary did not complete")
        if inputs() != manifest["inputs"]:
            raise RuntimeError("source changed during verification")
        save(evidence / "result.json", {"task": TASK, "revision": manifest["revision"],
            "command": "timeout 10m python3 tools/qualification/paxeer-x/asset-execution-prestate.py",
            "exit": 0, "phases": ["before", "after"], "fixture": str(fixture),
            "native_log": str(native_log)})
        print("VERIFIED " + str(evidence / "result.json"), flush=True)
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        reader.join(timeout=5)
        stream.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--native")
    parser.add_argument("--daemon")
    parser.add_argument("--cargo", default="cargo")
    arguments = parser.parse_args()
    evidence = evidence_directory()
    if arguments.build:
        build(arguments, evidence)
    else:
        qualify(evidence)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, queue.Empty, subprocess.SubprocessError) as error:
        print("FAILED " + str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
