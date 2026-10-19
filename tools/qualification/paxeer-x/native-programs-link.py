#!/usr/bin/env python3
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import queue
import shlex
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.35.28"
SCHEMA = "layerx.native-programs-link.v1"
DECLARED = "tools/qualification/paxeer-x/native-programs-link.py"
PRODUCTION = (
    "layerxd",
    "layerx-genesis-build",
    "layerx-verify",
    "layerx-module-registry",
    "layerx-archive-codec",
    "layerx-custody-proof",
)


def catalogue():
    path = ROOT / "tools/qualification/paxeer-x/replay-catalogue.py"
    spec = importlib.util.spec_from_file_location("layerx_replay_catalogue", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def inputs(gate):
    source = gate.inputs()
    inventory = subprocess.run(
        ["git", "ls-files", "-z", "--", "cmd", "*.go", "go.mod", "go.sum"],
        cwd=ROOT,
        check=True,
        capture_output=True,
    ).stdout.split(b"\0")
    paths = {os.fsdecode(path) for path in inventory if path} | {DECLARED}
    source.update(
        {
            path: gate.digest(ROOT / path)
            for path in sorted(paths)
            if Path(path).suffix
            in {
                ".c",
                ".h",
                ".rs",
                ".go",
                ".mod",
                ".sum",
                ".toml",
                ".lock",
                ".kvx",
                ".json",
                ".mk",
                ".py",
            }
        }
    )
    return source


def evidence_directory(gate):
    directory = Path(
        os.environ.get(
            "PAXEER_X_NATIVE_PROGRAMS_LINK_EVIDENCE",
            "/root/lx-ops/qualification/native-programs-link",
        )
    )
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory = directory.resolve(strict=True)
    info = directory.stat()
    if (
        ROOT == directory
        or ROOT in directory.parents
        or info.st_uid != os.geteuid()
        or info.st_mode & 0o077
    ):
        raise RuntimeError("private caller-owned evidence outside checkout required")
    return directory


def commit_proof():
    configured = os.environ.get("PAXEER_X_NATIVE_PROGRAMS_COMMIT_PROOF")
    if not configured:
        raise RuntimeError("protected genuine chain commit and state proof required")
    path = Path(configured)
    info = path.lstat()
    if (
        not stat.S_ISREG(info.st_mode)
        or not 0 < info.st_size <= 128 * 1024 * 1024
        or info.st_uid != os.geteuid()
        or info.st_mode & 0o077
    ):
        raise RuntimeError(
            "chain commit proof must be caller-owned, protected and bounded"
        )
    path = path.resolve(strict=True)
    if path == ROOT or ROOT in path.parents:
        raise RuntimeError("chain commit proof must remain outside the checkout")
    return path


def verify_commit(gate, evidence, manifest):
    proof = commit_proof()
    if gate.checked(manifest["commit_proof"]) != proof:
        raise RuntimeError("chain commit proof differs from the declared build input")
    with proof.open("rb") as source:
        request = source.read(128 * 1024 * 1024 + 1)
    if not 0 < len(request) <= 128 * 1024 * 1024:
        raise RuntimeError("chain commit proof size")
    if hashlib.sha256(request).hexdigest() != manifest["commit_proof"]["sha256"]:
        raise RuntimeError("chain commit proof changed while being read")
    command = [str(gate.checked(manifest["production"]["layerx-custody-proof"]))]
    result = subprocess.run(
        command, cwd=ROOT, input=request, capture_output=True, check=False, timeout=120
    )
    log = evidence / "verify-chain-commit.log"
    log.write_bytes(result.stdout + result.stderr)
    log.chmod(0o600)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    decoded = json.loads(request)
    verified = json.loads(result.stdout)
    if (
        not isinstance(decoded, dict)
        or not isinstance(verified, dict)
        or verified.get("version") != "paxeer-custody-state-v3"
        or verified.get("version") != decoded["bundle"]["version"]
        or verified.get("state_height") != decoded["bundle"]["state_height"]
        or verified.get("finalized_height") != decoded["bundle"]["finalized_height"]
        or verified.get("module_sha256") != decoded["expected"]["module_sha256"].lower()
        or verified.get("deposit_id", "") != decoded["expected"].get("deposit_id", "")
    ):
        raise RuntimeError(
            "verified chain commit identity differs from the declared proof"
        )
    gate.checked(manifest["commit_proof"])
    return gate.artifact(log)


def build(arguments, gate, evidence):
    manifest = evidence / "native-programs-artifacts.json"
    manifest.unlink(missing_ok=True)
    (evidence / "artifacts.json").unlink(missing_ok=True)
    source = inputs(gate)
    proof = gate.artifact(commit_proof())
    catalogue_source = gate.inputs()
    native_dir = Path(arguments.native_dir).resolve()
    programs_dir = Path(arguments.programs_dir).resolve()
    agent_dir = Path(arguments.agent_dir).resolve()
    native_make = evidence / "build-native.mk"
    targets = " ".join(
        "$(BUILD_DIR)/bin/" + name
        for name in PRODUCTION
        if name != "layerx-custody-proof"
    )
    native_make.write_text(
        ".PHONY: paxeer-x-native-programs-link-build\n"
        "paxeer-x-native-programs-link-build: "
        + targets
        + " custody-proof-build $(BUILD_DIR)/tests/lxp_test_replay_catalogue\n"
        "$(BUILD_DIR)/tests/lxp_test_replay_catalogue: tests/daemon/lxp_test_replay_catalogue.c tests/programs/test_call_activity.c $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build\n"
        "\t@mkdir -p $(@D)\n"
        "\t$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd $< $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) $(PROGRAMS_NATIVE_LDLIBS) -lcrypto -lsqlite3 -pthread -ldl -lm -o $@\n"
    )
    native_make.chmod(0o600)
    command = [
        "make",
        "-f",
        "Makefile",
        "-f",
        str(native_make),
        "-j4",
        "-W",
        str(programs_dir / "debug/liblayerx_programs_sandbox.a"),
        "BUILD_DIR=" + str(native_dir),
        "PROGRAMS_TARGET_DIR=" + str(programs_dir),
        "PROGRAMS_RUNTIME_LIB="
        + str(programs_dir / "debug/liblayerx_programs_sandbox.a"),
        "paxeer-x-native-programs-link-build",
    ]
    environment = dict(
        os.environ, PATH="/root/.cargo/bin:" + os.environ.get("PATH", "")
    )
    lock_path = Path("/root/lx-cargo/native-build.lock")
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        with (evidence / "build-native.log").open("w") as output:
            result = subprocess.run(
                command,
                cwd=ROOT,
                env=environment,
                stdin=subprocess.DEVNULL,
                stdout=output,
                stderr=subprocess.STDOUT,
                timeout=1100,
            )
        if result.returncode:
            raise subprocess.CalledProcessError(result.returncode, command)
    command = [
        *shlex.split(arguments.cargo),
        "test",
        "--locked",
        "--manifest-path",
        "services/agent/Cargo.toml",
        "-p",
        "layerx-client",
        "--test",
        "replay_catalogue",
        "--no-run",
        "--message-format=json",
    ]
    environment.update(CARGO_TARGET_DIR=str(agent_dir), CARGO_BUILD_JOBS="4")
    log = evidence / "build-client.log"
    with log.open("w") as output:
        result = subprocess.run(
            command,
            cwd=ROOT,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            timeout=1100,
        )
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    executables = set()
    for line in log.read_text().splitlines():
        if not line.startswith("{"):
            continue
        value = json.loads(line)
        if (
            value.get("reason") == "compiler-artifact"
            and value.get("executable")
            and value.get("target", {}).get("name") == "replay_catalogue"
        ):
            executables.add(value["executable"])
    if len(executables) != 1 or inputs(gate) != source:
        raise RuntimeError(
            "one declared client artifact and unchanged complete source required"
        )
    revision = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    production = {name: gate.artifact(native_dir / "bin" / name) for name in PRODUCTION}
    gate.checked(proof)
    transport = {
        "schema": gate.SCHEMA,
        "task": gate.TASK,
        "revision": revision,
        "inputs": catalogue_source,
        "native": gate.artifact(native_dir / "tests/lxp_test_replay_catalogue"),
        "daemon": production["layerxd"],
        "rust": gate.artifact(executables.pop()),
    }
    gate.save(evidence / "artifacts.json", transport)
    gate.save(
        manifest,
        {
            "schema": SCHEMA,
            "task": TASK,
            "revision": revision,
            "inputs": source,
            "production": production,
            "commit_proof": proof,
            "transport_manifest": gate.artifact(evidence / "artifacts.json"),
        },
    )
    print("ARTIFACTS " + str(manifest), flush=True)


def qualify(gate, evidence):
    manifest = json.loads((evidence / "native-programs-artifacts.json").read_text())
    if (
        manifest.get("schema") != SCHEMA
        or manifest.get("task") != TASK
        or manifest.get("inputs") != inputs(gate)
    ):
        raise RuntimeError("production artifacts do not bind current complete source")
    if set(manifest.get("production", {})) != set(PRODUCTION):
        raise RuntimeError("every Programs-linked production executable required")
    gate.checked(manifest["transport_manifest"])
    dependencies = {}
    with (evidence / "verify-production-links.log").open("w") as output:
        for name in PRODUCTION:
            path = gate.checked(manifest["production"][name])
            with path.open("rb") as binary:
                if binary.read(4) != b"\x7fELF":
                    raise RuntimeError("actual native ELF executable required: " + name)
            command = ["readelf", "--dynamic", str(path)]
            result = subprocess.run(
                command, check=True, capture_output=True, text=True, timeout=30
            )
            output.write(result.stdout)
            dependencies[name] = [
                line.strip()
                for line in result.stdout.splitlines()
                if "(NEEDED)" in line
            ]
            command = ["ldd", "-r", str(path)]
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            output.write(result.stdout)
            output.write(result.stderr)
            output.flush()
            if result.returncode:
                raise subprocess.CalledProcessError(result.returncode, command)
            if (
                "not found" in result.stdout + result.stderr
                or "undefined symbol:" in result.stdout + result.stderr
            ):
                raise RuntimeError("unresolved production native dependency: " + name)
    if not any("libssl.so" in line for line in dependencies["layerxd"]):
        raise RuntimeError("daemon Programs-to-Client TLS dependency missing")
    chain_commit = verify_commit(gate, evidence, manifest)
    gate.qualify(evidence)
    if inputs(gate) != manifest["inputs"]:
        raise RuntimeError("source changed during focused native qualification")
    for value in manifest["production"].values():
        gate.checked(value)
    gate.checked(manifest["transport_manifest"])
    gate.checked(manifest["commit_proof"])
    transport_result = evidence / "result.json"
    gate.save(
        evidence / "native-programs-result.json",
        {
            "task": TASK,
            "revision": manifest["revision"],
            "command": "timeout 10m python3 " + DECLARED,
            "exit": 0,
            "production_dependencies": dependencies,
            "chain_commit": chain_commit,
            "transport_result": gate.artifact(transport_result),
        },
    )
    print("VERIFIED " + str(evidence / "native-programs-result.json"), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--cargo", default="/root/.cargo/bin/cargo")
    parser.add_argument(
        "--native-dir", default="/root/lx-target/arbiter-prestate/native"
    )
    parser.add_argument(
        "--programs-dir", default="/root/lx-target/arbiter-prestate/rust"
    )
    parser.add_argument("--agent-dir", default="/root/lx-target/agent")
    arguments = parser.parse_args()
    gate = catalogue()
    evidence = evidence_directory(gate)
    if arguments.build:
        build(arguments, gate, evidence)
    else:
        qualify(gate, evidence)


if __name__ == "__main__":
    try:
        main()
    except (
        OSError,
        ValueError,
        RuntimeError,
        queue.Empty,
        subprocess.SubprocessError,
    ) as error:
        print("FAILED " + str(error), file=sys.stderr)
        sys.exit(
            error.returncode if isinstance(error, subprocess.CalledProcessError) else 1
        )
