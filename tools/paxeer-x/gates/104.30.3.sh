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
import stat
import subprocess
import sys
import time

ROOT = Path.cwd().resolve()
NATIVE = (
    "test_accounts", "test_cache_native_equivalence", "test_call_activity",
    "test_call_builders", "test_interface_protocol", "test_market_attestation",
    "test_metered_call", "test_metering_schedule", "test_occupancy_batch",
    "test_winddown",
)
RUST_CASES = {
    "golden_preimage_layout_is_frozen",
    "golden_derivation_matches_frozen_preimage_hash",
    "derivation_is_byte_identical_across_repeated_computation",
    "derivation_outputs_are_fixed_width", "conformance_vectors_do_not_collide",
    "same_seed_under_distinct_programs_never_collides",
}
SCHEMA = "paxeer-x.program-account-integration.v1"
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
                "tests/programs", "tests/vectors", "contracts", "tools/specgen",
                "tools/paxeer-x/gates/104.30.3.sh").split("\0")
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
            "incomplete native account corpus")
    require("programs/crates/layerx-programs-runtime/tests/program_account_derivation.rs"
            in result, "missing canonical Rust derivation corpus")
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
    target = private_directory("PAXEER_X_PROGRAM_ACCOUNTS_TARGET",
                               "/root/lx-target/task104303")
    rust_target = Path(os.environ.get("PAXEER_X_PROGRAM_ACCOUNTS_RUST_TARGET",
                                     str(target / "rust"))).absolute()
    require(rust_target.resolve() == rust_target and ROOT not in rust_target.parents,
            "canonical external Rust target required")
    env = os.environ.copy()
    env["CARGO_TARGET_DIR"] = str(rust_target)
    cargo = ["cargo", "--locked", "--manifest-path", "programs/Cargo.toml"]
    run([cargo[0], "build", *cargo[1:], "-p", "layerx-programs-sandbox",
         "--features", "host-ffi"], evidence / "build-sandbox.log", env)
    output = run([cargo[0], "test", *cargo[1:], "-p", "layerx-programs-runtime",
                  "--test", "program_account_derivation", "--no-run",
                  "--message-format=json"], evidence / "build-derivation.log", env)
    executables = []
    for line in output.splitlines():
        if not line.startswith("{"):
            continue
        entry = json.loads(line)
        if (entry.get("reason") == "compiler-artifact"
                and entry.get("target", {}).get("name") == "program_account_derivation"
                and entry.get("executable")):
            executables.append(entry["executable"])
    require(len(executables) == 1, "exact Rust derivation executable required")
    output = run([cargo[0], "test", *cargo[1:], "-p", "layerx-programs-registry",
                  "--test", "interface_protocol", "--no-run", "--message-format=json"],
                 evidence / "build-interface-inputs.log", env)
    interface_executables = []
    for line in output.splitlines():
        if line.startswith("{"):
            entry = json.loads(line)
            if (entry.get("reason") == "compiler-artifact"
                    and entry.get("target", {}).get("name") == "interface_protocol"
                    and entry.get("executable")):
                interface_executables.append(entry["executable"])
    require(len(interface_executables) == 1, "exact interface input producer required")
    run([cargo[0], "build", *cargo[1:], "-p", "layerx-programs-market",
         "--target", "wasm32-unknown-unknown", "--release"],
        evidence / "build-market-guest.log", env)
    market_guest = rust_target / "wasm32-unknown-unknown/release/layerx_programs_market.wasm"
    require(market_guest.is_file() and not market_guest.is_symlink(), "missing real Market guest")
    archive = rust_target / "debug/liblayerx_programs_sandbox.a"
    require(archive.is_file() and not archive.is_symlink(), "missing real sandbox archive")
    native = target / "native"
    fragment = evidence / "program-accounts.mk"
    lines = [".PHONY: paxeer-x-program-accounts-build",
             "paxeer-x-program-accounts-build: " + " ".join(
                 f"$(BUILD_DIR)/tests/task104303/{name}" for name in NATIVE)]
    for name in NATIVE:
        lines.extend([
            f"$(BUILD_DIR)/tests/task104303/{name}: tests/programs/{name}.c $(LIBRARY) $(PROGRAMS_RUNTIME_LIB)",
            "\t@mkdir -p $(@D)",
            "\t$(CC) $(CPPFLAGS) $(CFLAGS) $< -Wl,--start-group $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) -Wl,--end-group $(EXTRA_LDFLAGS) $(PROGRAMS_NATIVE_LDLIBS) -lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@",
        ])
    fragment.write_text("\n".join(lines) + "\n")
    fragment.chmod(0o600)
    run(["make", "-f", "Makefile", "-f", str(fragment), "-j4",
         f"BUILD_DIR={native}", f"PROGRAMS_TARGET_DIR={rust_target}",
         f"PROGRAMS_RUNTIME_LIB={archive}", f"LXP_REVISION={source['revision']}",
         "paxeer-x-program-accounts-build"], evidence / "build-native.log", env)
    require(identity() == source and inputs() == source_inputs, "source changed during build")
    manifest = {"schema": SCHEMA, **source, "source_inputs": source_inputs,
                "native": {name: artifact(native / "tests/task104303" / name)
                           for name in NATIVE},
                "rust": artifact(executables[0]),
                "interface_input_producer": artifact(interface_executables[0]),
                "market_guest": {"path": str(market_guest), "sha256": digest(market_guest)},
                "sandbox_archive": {"path": str(archive), "sha256": digest(archive)},
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
    require(set(manifest.get("native", {})) == set(NATIVE), "incomplete native corpus")
    for entry in [*manifest["native"].values(), manifest["rust"],
                  manifest["interface_input_producer"]]:
        require(artifact(entry["path"]) == entry, "executable identity mismatch")
    for key in ("sandbox_archive", "core_archive", "market_guest"):
        entry = manifest[key]
        require(digest(Path(entry["path"])) == entry["sha256"], "archive identity mismatch")
    with Path(manifest["market_guest"]["path"]).open("rb") as stream:
        require(stream.read(8) == b"\x00asm\x01\x00\x00\x00", "actual Market Wasm required")
    cache_inputs = evidence / "cache-inputs"
    interface_inputs = evidence / "interface-inputs"
    interface_output = evidence / "interface-output"
    for directory in (cache_inputs, interface_inputs, interface_output):
        require(not directory.exists(), "fresh genuine fixture inputs required")
        directory.mkdir(mode=0o700)
    run([manifest["native"]["test_cache_native_equivalence"]["path"],
         "--emit-guests", str(cache_inputs)], evidence / "produce-cache-guests.log")
    require({path.name for path in cache_inputs.iterdir()}
            == {f"guest-{index:02d}.wasm" for index in range(12)},
            "complete actual cache guest inventory required")
    producer_env = os.environ.copy()
    producer_env["PAXEER_X_INTERFACE_INPUTS"] = str(interface_inputs)
    produced = run([manifest["interface_input_producer"]["path"], "--exact",
                    "emit_native_inputs", "--nocapture", "--test-threads=1"],
                   evidence / "produce-interface-inputs.log", producer_env)
    require(re.search(r"test result: ok\. 1 passed; 0 failed; 0 ignored;", produced),
            "real canonical interface input producer failed")
    require({path.name for path in interface_inputs.iterdir()} == {
        "abi1", "abi2", "abi2-dynamic", "abi3", "abi3-dynamic", "abi4",
        "abi4-dynamic", "abi2-widening", "abi2-narrowing"},
        "complete actual interface guest inventory required")
    guest_inputs = {str(path): digest(path) for directory in (cache_inputs, interface_inputs)
                    for path in directory.rglob("*") if path.is_file()}
    for name in NATIVE:
        command = [manifest["native"][name]["path"]]
        if name == "test_cache_native_equivalence":
            command.extend(["--guest-dir", str(cache_inputs)])
        elif name == "test_interface_protocol":
            command.extend(["--input", str(interface_inputs), "--output", str(interface_output)])
        elif name == "test_market_attestation":
            command.append(manifest["market_guest"]["path"])
        run(command, evidence / f"verify-{name}.log")
    output = run([manifest["rust"]["path"], "--nocapture", "--test-threads=1"],
                 evidence / "verify-derivation.log")
    passed = re.findall(r"^test ([a-z_]+) \.\.\. ok$", output, re.MULTILINE)
    require(len(passed) == len(RUST_CASES) and set(passed) == RUST_CASES,
            "complete real Rust derivation corpus required")
    require(re.search(r"test result: ok\. 6 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out;",
                      output), "Rust corpus skipped or incomplete")
    require(identity() == source and inputs() == manifest["source_inputs"],
            "source changed during verification")
    require(all(digest(Path(path)) == value for path, value in guest_inputs.items()),
            "actual producer inputs changed during verification")
    for entry in [*manifest["native"].values(), manifest["rust"],
                  manifest["interface_input_producer"]]:
        require(artifact(entry["path"]) == entry, "artifact changed during verification")
    write_json(evidence / "result.json", {"schema": SCHEMA, **source,
               "commands": records, "producer_inputs": guest_inputs,
               "tests": len(NATIVE) + len(passed) + 1, "skipped": 0})
    print(f"PAXEER_X_GATE tests={len(NATIVE) + len(passed) + 1} skipped=0")


records = []
try:
    require(sys.argv[1:] in ([], ["--build"]), "usage: 104.30.3.sh [--build]")
    evidence = private_directory("PAXEER_X_PROGRAM_ACCOUNTS_ARTIFACTS",
                                 "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104303")
    if sys.argv[1:] == ["--build"]:
        lock = Path("/root/lx-cargo/native-build.lock")
        lock.parent.mkdir(parents=True, exist_ok=True)
        with lock.open("a") as stream:
            fcntl.flock(stream, fcntl.LOCK_EX)
            build()
    else:
        verify()
except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
    print(f"program-account integration: {error}", file=sys.stderr)
    sys.exit(1)
PY
