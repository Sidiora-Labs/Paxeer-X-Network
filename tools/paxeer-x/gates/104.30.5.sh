#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - "$@" <<'PYTHON'
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import time

ROOT = Path.cwd().resolve()
SCHEMA = "paxeer-x.registry-multiasset-winddown.v1"
TEST = "lifecycle::account_upgrades::registry_multiasset_winddown_consumes_verified_native_state"
REQUIRED = {
    "two-assets-funded", "complete-exits", "missing-exit-refused",
    "current-proof-read", "proof-substitution-refused", "native-restart",
    "tombstone-history", "exit-conservation", "historical-replay",
    "wrong-asset-refused", "wrong-owner-refused",
}
DEADLINE = time.monotonic() + 1200
COUNT = 0


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def git(*arguments):
    return subprocess.check_output(["git", *arguments], cwd=ROOT, text=True).strip()


def private_directory(name):
    raw = os.environ.get(name)
    require(raw, "explicit private evidence directory required: " + name)
    path = Path(raw).absolute()
    require(path.resolve() == path and path != ROOT and ROOT not in path.parents,
            "canonical private evidence outside source required")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o700, "owned private0700 evidence required")
    return path


def document(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and info.st_nlink == 1 and not info.st_mode & 0o077
                and info.st_size <= 4194304, "protected bounded artifact document required")
        return json.load(stream)


def write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def sources():
    require(not git("status", "--porcelain", "--untracked-files=all"),
            "whole source must be published clean before qualification")
    paths = git("ls-files", "-z", "Makefile", "src", "include", "cmd/layerxd",
                "cmd/layerx-genesis", "programs", "agent/crates", "agent/Cargo.toml",
                "agent/Cargo.lock", "platform/hosted/agent-boundary", "platform/Cargo.toml",
                "platform/Cargo.lock", "contracts/config", "tests/bridge", "tests/support",
                "tests/fixtures/fee-params-v3.bin", "tests/fixtures/fee-params-v4.bin",
                "tools/paxeer-x/build/6.6.mk", "tools/build", "platform/Makefile.inc",
                "tools/paxeer-x/gates/104.30.5.sh", "rust-toolchain.toml").split("\0")
    result = {}
    for relative in paths:
        if not relative or any(part.startswith(".env") for part in Path(relative).parts):
            continue
        path = ROOT / relative
        require(path.is_file() and not path.is_symlink(), "regular candidate source required: " + relative)
        result[relative] = digest(path)
    return {"revision": git("rev-parse", "HEAD"), "tree": git("rev-parse", "HEAD^{tree}"),
            "files": result}


def run(command, log, environment=None):
    require(time.monotonic() < DEADLINE, "task qualification deadline elapsed")
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write((json.dumps(command) + "\n").encode())
        stream.flush()
        child = subprocess.Popen(command, cwd=ROOT, env=environment,
                                 stdout=stream, stderr=subprocess.STDOUT,
                                 stdin=subprocess.DEVNULL, start_new_session=True)
        try:
            code = child.wait(timeout=max(1, DEADLINE - time.monotonic()))
        finally:
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=3)
    print("COMMAND " + json.dumps(command) + " EXIT " + str(code) + " LOG " + str(log), flush=True)
    require(code == 0, "actual command refused: " + str(code) + ": " + str(log))
    return Path(log).read_text(errors="replace")


def artifact(path):
    path = Path(path).absolute()
    require(path.resolve() == path and path.is_file() and not path.is_symlink(),
            "canonical actual artifact required")
    return {"path": str(path), "sha256": digest(path), "bytes": path.stat().st_size}


def executable(output, target):
    result = set()
    for line in output.splitlines():
        if line.startswith("{"):
            event = json.loads(line)
            if event.get("reason") == "compiler-artifact" and event.get("executable") and event.get("target", {}).get("name") == target:
                result.add(event["executable"])
    require(len(result) == 1, "exact compiled executable required: " + target)
    return next(iter(result))


def build(evidence, manifest):
    before = sources()
    env = dict(os.environ, CARGO_BUILD_JOBS="2")
    runtime_target = Path(os.environ.get("PAXEER_X_REGISTRY_RUNTIME_TARGET", "/root/lx-target/programs")).absolute()
    platform_target = Path(os.environ.get("PAXEER_X_REGISTRY_PLATFORM_TARGET", "/root/lx-target/platform")).absolute()
    require(ROOT not in runtime_target.parents and ROOT not in platform_target.parents,
            "private warm Rust targets required")
    cargo = ["/root/.cargo/bin/cargo"]
    env["CARGO_TARGET_DIR"] = str(ROOT / "programs/sdk/rust/examples/escrow/target")
    run(cargo + ["build", "--locked", "--release", "--target", "wasm32-unknown-unknown",
                 "--manifest-path", "programs/sdk/rust/examples/escrow/Cargo.toml"], evidence / "guest-build.log", env)
    env["CARGO_TARGET_DIR"] = str(runtime_target)
    run(["flock", "/root/lx-cargo/native-build.lock"] + cargo + ["build", "--locked", "--manifest-path",
        "programs/Cargo.toml", "-p", "layerx-programs-sandbox", "--lib", "--features", "host-ffi"],
        evidence / "runtime-build.log", env)
    runtime = runtime_target / "debug/liblayerx_programs_sandbox.a"
    flags = shlex.split(env.get("EXTRA_LDFLAGS", ""))
    if "-lssl" not in flags:
        flags.append("-lssl")
    run(["flock", "/root/lx-cargo/native-build.lock", "make", "-j2", "-o", "programs-build",
         "BUILD_DIR=build", "LXP_REVISION=" + before["revision"], "EXTRA_LDFLAGS=" + shlex.join(flags),
         "PAXEER_X_PROFILE2_RUNTIME_LIB=" + str(runtime), "PROGRAMS_RUNTIME_LIB=" + str(runtime),
         "paxeer-x-profile2-native", "build/tests/bridge/sign-credit", "build/tests/bridge/test-credit"],
        evidence / "native-build.log", env)
    output = run(["flock", "/root/lx-cargo/native-build.lock"] + cargo + ["test", "--locked",
        "--manifest-path", "programs/Cargo.toml", "-p", "layerx-programs-registry", "--test", "winddown",
        "--no-run", "--message-format=json"], evidence / "registry-build.log", env)
    registry = executable(output, "winddown")
    env["CARGO_TARGET_DIR"] = str(platform_target)
    output = run(["flock", "/root/lx-cargo/platform-build.lock"] + cargo + ["test", "--locked",
        "--manifest-path", "platform/Cargo.toml", "-p", "layerx-platform-agent-boundary",
        "--test", "real_node", "--no-run", "--message-format=json"], evidence / "consumer-build.log", env)
    actual = executable(output, "real_node")
    boundary = executable(output, "layerx-agent-boundary")
    os.chmod(boundary, 0o755)
    require(sources() == before, "source changed during actual compilation")
    write(manifest, {"schema": SCHEMA, "source": before, "test": TEST,
                    "required": sorted(REQUIRED), "artifacts": {
        "layerxd": artifact(ROOT / "build/bin/layerxd"),
        "genesis": artifact(ROOT / "build/bin/layerx-genesis-build"),
        "sign-credit": artifact(ROOT / "build/tests/bridge/sign-credit"),
        "test-credit": artifact(ROOT / "build/tests/bridge/test-credit"),
        "runtime": artifact(runtime), "registry": artifact(registry),
        "consumer": artifact(actual), "boundary": artifact(boundary),
        "guest": artifact(ROOT / "programs/sdk/rust/examples/escrow/target/wasm32-unknown-unknown/release/layerx_reference_escrow.wasm")}})


def verify(evidence, manifest):
    global COUNT
    value = document(manifest)
    require(set(value) == {"schema", "source", "test", "required", "artifacts"}
            and value["schema"] == SCHEMA and value["source"] == sources()
            and value["test"] == TEST and value["required"] == sorted(REQUIRED),
            "actual immutable registry candidate binding required")
    artifacts = value["artifacts"]
    require(set(artifacts) == {"layerxd", "genesis", "sign-credit", "test-credit", "runtime",
                             "registry", "consumer", "boundary", "guest"}, "whole actual producer artifact set required")
    for row in artifacts.values():
        require(artifact(row["path"]) == row, "actual compiled artifact changed")
    require(os.geteuid() == 0, "real isolated node identity provisioning requires root")
    fixture = os.environ.get("LAYERX_MULTI_ASSET_CUSTODY_FIXTURE")
    require(fixture, "genuine registered multiasset custody fixture is required")
    run_directory = evidence / ("run-" + str(time.time_ns()))
    run_directory.mkdir(mode=0o700)
    env = dict(os.environ, PAXEER_X_PROFILE2_ACCOUNTS_EVIDENCE=str(run_directory),
               LAYERX_TEST_NATIVE_BIN_DIR=str(Path(artifacts["layerxd"]["path"]).parent))
    output = run([artifacts["consumer"]["path"], "--exact", TEST, "--nocapture", "--test-threads=1"],
                 run_directory / "consumer.log", env)
    cases = re.findall(r"^REGISTRY_MULTI_ASSET_CASE ([a-z0-9_-]+)$", output, re.M)
    require(set(cases) == REQUIRED and len(cases) == len(REQUIRED),
            "missing or duplicated genuine multiasset acceptance evidence")
    require(re.findall(r"^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;", output, re.M)
            == [("1", "0", "0")], "actual fixture absent, failed or skipped")
    require(list(run_directory.glob("*.receipt")) and list(run_directory.glob("*.LXPS2")),
            "actual signed receipts and verified account proofs required")
    COUNT = len(cases)
    output = run([artifacts["registry"]["path"], "--nocapture", "--test-threads=1"],
                 run_directory / "retained-registry.log", env)
    summary = re.findall(r"^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;", output, re.M)
    require(len(summary) == 1 and int(summary[0][0]) > 0 and summary[0][1:] == ("0", "0"),
            "retained registry assertions failed or skipped")
    COUNT += int(summary[0][0])
    require(sources() == value["source"], "source changed during genuine verification")
    for row in artifacts.values():
        require(artifact(row["path"]) == row, "artifact changed during genuine verification")
    write(run_directory / "result.json", {"revision": value["source"]["revision"], "exit_code": 0,
                                         "cases": cases, "tests": COUNT, "skipped": 0})


os.umask(0o077)
code = 0
try:
    require(sys.argv[1:] in ([], ["--build-artifacts"]), "unknown or unimplemented task mode")
    evidence = private_directory("PAXEER_X_REGISTRY_WINDDOWN_EVIDENCE")
    raw_manifest = os.environ.get("PAXEER_X_REGISTRY_WINDDOWN_ARTIFACTS")
    require(raw_manifest, "explicit protected actual artifact manifest required")
    manifest = Path(raw_manifest).absolute()
    require(manifest.parent == evidence and manifest.resolve() == manifest,
            "manifest must belong to exact private evidence directory")
    if sys.argv[1:]:
        build(evidence, manifest)
    else:
        verify(evidence, manifest)
except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
    print("registry-multiasset-winddown: refusal: " + str(error), file=sys.stderr)
    code = 78
finally:
    print("PAXEER_X_GATE tests=" + str(COUNT) + " skipped=0", flush=True)
raise SystemExit(code)
PYTHON
