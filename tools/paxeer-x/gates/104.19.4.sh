#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - "$@" <<'PY'
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time

ROOT = Path.cwd().resolve()
NATIVE = ("test_module_registration",)
REQUIRED = {"versioned_registration", "protocol2_execution", "protocol3_execution"}
SCHEMA = "paxeer-x.program-module-registration.v1"
DEADLINE = time.monotonic() + 1200


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def private_directory(name, default):
    path = Path(os.environ.get(name, default)).absolute()
    require(path.resolve() == path, f"symlinked private directory: {path}")
    require(ROOT != path and ROOT not in path.parents,
            f"private directory inside checkout: {path}")
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    info = path.stat()
    require(info.st_uid == os.getuid() and stat.S_ISDIR(info.st_mode),
            f"unowned private directory: {path}")
    require(stat.S_IMODE(info.st_mode) == 0o700,
            f"private directory must have mode 0700: {path}")
    return path


def identity():
    require(not git("status", "--porcelain", "--untracked-files=all"),
            "published clean checkout required")
    return {"revision": git("rev-parse", "HEAD"),
            "tree": git("rev-parse", "HEAD^{tree}")}


def inputs():
    paths = git("ls-files", "-z", "Makefile", "src", "include", "programs",
                "tests/programs", "tests/vectors", "platform/sdk/conformance/fixtures",
                "contracts", "tools/specgen",
                "tools/paxeer-x/gates/104.19.4.sh").split("\0")
    result = {}
    for relative in paths:
        if not relative:
            continue
        path = ROOT / relative
        if any(part == ".env" or part.startswith(".env.") for part in path.parts):
            continue
        require(path.is_file() and not path.is_symlink(),
                f"noncanonical source input: {relative}")
        result[relative] = digest(path)
    require(all(f"tests/programs/{name}.c" in result for name in NATIVE),
            "incomplete real module registration corpus")
    return result


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    path.chmod(0o600)


def run(command, log, env=None):
    remaining = DEADLINE - time.monotonic()
    require(remaining > 0, "bounded qualification deadline elapsed")
    with log.open("wb") as stream:
        log.chmod(0o600)
        stream.write((json.dumps(command) + "\n").encode())
        stream.flush()
        result = subprocess.run(command, cwd=ROOT, stdout=stream,
                                stderr=subprocess.STDOUT, env=env,
                                timeout=remaining, check=False)
    record = {"command": command, "exit_code": result.returncode,
              "log_path": str(log)}
    records.append(record)
    write_json(evidence / "commands.json", records)
    require(result.returncode == 0,
            f"command exited {result.returncode}: {log}")
    return log.read_text(errors="replace")


def artifact(path):
    path = Path(path).absolute()
    require(path.resolve() == path and path.is_file() and not path.is_symlink(),
            f"noncanonical artifact: {path}")
    require(ROOT not in path.parents and os.access(path, os.X_OK),
            f"nonexecutable or in-checkout artifact: {path}")
    with path.open("rb") as stream:
        require(stream.read(4) == b"\x7fELF", f"native ELF artifact required: {path}")
    return {"path": str(path), "sha256": digest(path)}


def build():
    source = identity()
    source_inputs = inputs()
    manifest_path = evidence / "artifacts.json"
    require(not manifest_path.is_symlink(), "symlinked artifact manifest")
    manifest_path.unlink(missing_ok=True)
    target = private_directory("PAXEER_X_MODULE_REGISTRATION_TARGET",
                               "/root/lx-target/task104194")
    rust_target = Path(os.environ.get("PAXEER_X_MODULE_REGISTRATION_RUST_TARGET",
                                     str(target / "rust"))).absolute()
    require(rust_target.resolve() == rust_target and ROOT not in rust_target.parents,
            "canonical external Rust target required")
    env = os.environ.copy()
    env["CARGO_TARGET_DIR"] = str(rust_target)
    run(["cargo", "build", "--locked", "--manifest-path", "programs/Cargo.toml",
         "-p", "layerx-programs-sandbox", "--features", "host-ffi"],
        evidence / "build-sandbox.log", env)
    archive = rust_target / "debug/liblayerx_programs_sandbox.a"
    require(archive.is_file() and not archive.is_symlink(), "missing real sandbox archive")
    native = target / "native"
    fragment = evidence / "module-registration.mk"
    lines = [".PHONY: paxeer-x-module-registration-build",
             "paxeer-x-module-registration-build: " + " ".join(
                 f"$(BUILD_DIR)/tests/task104194/{name}" for name in NATIVE)]
    for name in NATIVE:
        lines.extend([
            f"$(BUILD_DIR)/tests/task104194/{name}: tests/programs/{name}.c $(LIBRARY) $(PROGRAMS_RUNTIME_LIB)",
            "\t@mkdir -p $(@D)",
            "\t$(CC) $(CPPFLAGS) $(CFLAGS) $< -Wl,--start-group $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) -Wl,--end-group $(EXTRA_LDFLAGS) $(PROGRAMS_NATIVE_LDLIBS) -lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@",
        ])
    fragment.write_text("\n".join(lines) + "\n")
    fragment.chmod(0o600)
    run(["make", "-f", "Makefile", "-f", str(fragment), "-j4",
         f"BUILD_DIR={native}", f"PROGRAMS_TARGET_DIR={rust_target}",
         f"PROGRAMS_RUNTIME_LIB={archive}", f"LXP_REVISION={source['revision']}",
         "paxeer-x-module-registration-build"], evidence / "build-native.log", env)
    require(identity() == source and inputs() == source_inputs, "source changed during build")
    retained_archive = evidence / "liblayerx_programs_sandbox.a"
    require(not retained_archive.exists(), "fresh retained archive path required")
    shutil.copyfile(archive, retained_archive)
    retained_archive.chmod(0o600)
    require(digest(retained_archive) == digest(archive), "sandbox archive changed during capture")
    manifest = {"schema": SCHEMA, **source, "source_inputs": source_inputs,
                "required_cases": sorted(REQUIRED),
                "native": {name: artifact(native / "tests/task104194" / name)
                           for name in NATIVE},
                "sandbox_archive": {"path": str(retained_archive), "sha256": digest(retained_archive)},
                "core_archive": {"path": str(native / "liblayerx.a"),
                                 "sha256": digest(native / "liblayerx.a")},
                "build_commands": records}
    write_json(evidence / "artifacts.json", manifest)


def verify():
    path = evidence / "artifacts.json"
    require(path.is_file() and not path.is_symlink() and path.stat().st_uid == os.getuid()
            and stat.S_IMODE(path.stat().st_mode) == 0o600,
            "protected actual build manifest required")
    manifest = json.loads(path.read_text())
    require(manifest.get("schema") == SCHEMA, "wrong artifact schema")
    source = identity()
    require(all(manifest.get(key) == value for key, value in source.items()),
            "artifact source identity mismatch")
    require(manifest.get("source_inputs") == inputs(), "artifact source hash mismatch")
    require(manifest.get("required_cases") == sorted(REQUIRED), "wrong acceptance corpus")
    require(set(manifest.get("native", {})) == set(NATIVE), "incomplete native corpus")
    for entry in manifest["native"].values():
        require(artifact(entry["path"]) == entry, "executable identity mismatch")
    for key in ("sandbox_archive", "core_archive"):
        entry = manifest[key]
        require(digest(Path(entry["path"])) == entry["sha256"], "archive identity mismatch")
    output = run([manifest["native"]["test_module_registration"]["path"]],
                 evidence / "verify-native-module-registration.log")
    policy = re.findall(r"^ABI_POLICY_NATIVE tests=(\d+) skipped=0$", output, re.MULTILINE)
    require(policy == ["32"], "retained ABI1-4 transition matrix missing or skipped")
    passed = re.findall(r"^PROGRAM_MODULE_REGISTRATION_CASE name=([a-z0-9_]+)$",
                        output, re.MULTILINE)
    require(len(passed) == len(REQUIRED) and set(passed) == REQUIRED,
            "complete signed native registration/rollback/event-root corpus required")
    require(identity() == source and inputs() == manifest["source_inputs"],
            "source changed during verification")
    for entry in manifest["native"].values():
        require(artifact(entry["path"]) == entry, "artifact changed during verification")
    tests = int(policy[0]) + len(passed)
    write_json(evidence / "result.json", {"schema": SCHEMA, **source,
               "commands": records, "cases": passed, "tests": tests, "skipped": 0})
    print(f"PAXEER_X_GATE tests={tests} skipped=0")


records = []
try:
    require(sys.argv[1:] in ([], ["--build"]), "usage: 104.19.4.sh [--build]")
    evidence = private_directory("PAXEER_X_MODULE_REGISTRATION_ARTIFACTS",
                                 "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104194")
    if sys.argv[1:] == ["--build"]:
        lock = Path("/root/lx-cargo/native-build.lock")
        lock.parent.mkdir(parents=True, exist_ok=True)
        with lock.open("a") as stream:
            fcntl.flock(stream, fcntl.LOCK_EX)
            build()
    else:
        verify()
except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
    print(f"program module registration: {error}", file=sys.stderr)
    sys.exit(1)
PY
