#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
LANGUAGES = ("c", "rust", "assemblyscript")
CASES = ("basic", "oracle", "web", "refusal", "call", "account", "denied")
FIELDS = ("result_code", "response_hex", "refusal_hex", "effects_hex", "events_hex", "calldata_hex")


def sha(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    path.chmod(0o600)


def inventory():
    paths = subprocess.run(["git", "ls-files", "-z", "--", "Makefile", "src", "include",
        "programs", "tests/programs", "tests/test_programs_oracle_read.c", "tests/test_programs_web_read.c",
        "tools/qualification/paxeer-x/programs_language_abi.py"], cwd=ROOT,
        check=True, capture_output=True).stdout.split(b"\0")
    return {os.fsdecode(p): sha(ROOT / os.fsdecode(p)) for p in paths if p}


def run(command, evidence, name, deadline, stdout=None):
    remaining = int(deadline - time.time())
    if remaining <= 0:
        raise TimeoutError("task cutoff reached")
    with (evidence / (name + ".log")).open("wb") as log:
        process = subprocess.run(["timeout", "--signal=TERM", "--kill-after=5s", str(remaining),
            *map(str, command)], cwd=ROOT, stdout=stdout or log, stderr=log)
    (evidence / (name + ".exit")).write_text(str(process.returncode) + "\n")
    if process.returncode:
        raise subprocess.CalledProcessError(process.returncode, command)


def compiler(name, override):
    value = os.environ.get(override) or shutil.which(name)
    if not value or not Path(value).is_file():
        raise FileNotFoundError("missing genuine toolchain: " + name)
    return Path(value).absolute()


def manifest_parity():
    rows = []
    for line in (ROOT / "programs/sdk/abi-manifest.tsv").read_text().splitlines():
        version, module, name, signature = line.split("\t")
        version = int(version)
        if version not in range(1, 5) or module != "layerx_v" + str(version):
            raise RuntimeError("noncanonical shared ABI namespace")
        rows.append((version, module, name, signature))
    if len(rows) != 28 or len({(row[1], row[2]) for row in rows}) != len(rows):
        raise RuntimeError("shared ABI table is incomplete or duplicated")
    for version in range(1, 5):
        encoded = version.to_bytes(2, "big")
        previous = None
        for introduced, module, name, signature in rows:
            if introduced > version:
                continue
            if module != previous:
                encoded += module.encode() + b"\0"
                previous = module
            encoded += (name + signature).encode() + b"\0"
        frozen = bytes.fromhex((ROOT / f"programs/tests/vectors/abi-v{version}.hex").read_text())
        if encoded != frozen:
            raise RuntimeError("shared ABI table differs from frozen runtime manifest")


def rejected_artifacts(path, evidence):
    original = Path(path).read_bytes()
    if original[:8] != b"\0asm\1\0\0\0":
        raise RuntimeError("compiled WASM artifact required")
    def leb(cursor):
        value = 0
        for shift in range(0, 35, 7):
            byte = original[cursor]
            cursor += 1
            value |= (byte & 127) << shift
            if not byte & 128:
                return value, cursor
        raise RuntimeError("malformed compiled WASM length")
    sections = {}
    cursor = 8
    while cursor < len(original):
        identifier = original[cursor]
        length, start = leb(cursor + 1)
        end = start + length
        if end > len(original):
            raise RuntimeError("compiled WASM section exceeds artifact")
        sections[identifier] = (start, end)
        cursor = end
    start, end = sections[2]
    import_offset = original.find(b"oracle_read", start, end)
    if import_offset < 0:
        raise RuntimeError("compiled guest does not exercise committed oracle import")
    wrong_import = bytearray(original)
    wrong_import[import_offset + 5] = ord("f")
    start, end = sections[1]
    type_offset = next((offset for offset in range(start, end) if original[offset] in (0x7f, 0x7e)), None)
    if type_offset is None:
        raise RuntimeError("compiled guest has no integer function type")
    wrong_signature = bytearray(original)
    wrong_signature[type_offset] = 0x7e if original[type_offset] == 0x7f else 0x7f
    floating = bytearray(original)
    floating[type_offset] = 0x7d
    for name, value in [("unknown-import", wrong_import), ("wrong-signature", wrong_signature), ("floating-point", floating)]:
        artifact = evidence / (name + ".wasm")
        artifact.write_bytes(value)
        yield name, artifact


def refuse(command, evidence, name, deadline):
    remaining = int(deadline - time.time())
    if remaining <= 0:
        raise TimeoutError("task cutoff reached")
    with (evidence / (name + ".log")).open("wb") as log:
        result = subprocess.run(["timeout", str(remaining), *map(str, command)], cwd=ROOT, stdout=log, stderr=log)
    (evidence / (name + ".exit")).write_text(str(result.returncode) + "\n")
    if result.returncode == 0 or result.returncode in (124, 127):
        raise RuntimeError("required refusal accepted, or gate unavailable")


def build(evidence, target, deadline):
    source = inventory()
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT,
        check=True, capture_output=True, text=True).stdout.strip()
    clang = compiler("clang", "PAXEER_X_PROGRAMS_CLANG")
    node = compiler("node", "PAXEER_X_PROGRAMS_NODE")
    cargo = compiler("cargo", "PAXEER_X_PROGRAMS_CARGO")
    rustc = compiler("rustc", "PAXEER_X_PROGRAMS_RUSTC")
    asc = Path(os.environ.get("PAXEER_X_PROGRAMS_ASC", str(ROOT / "programs/sdk/assemblyscript/node_modules/assemblyscript/bin/asc.js")))
    if not asc.is_file():
        raise FileNotFoundError("missing genuine AssemblyScript 0.27.31 compiler")
    target.mkdir(parents=True, exist_ok=True)
    manifest_parity()
    run([cargo, "build", "--offline", "--locked", "--manifest-path", ROOT / "programs/Cargo.toml",
        "-p", "layerx-program-sdk", "--target", "wasm32-unknown-unknown", "--target-dir", target / "rust"],
        evidence, "build-rust-sdk", deadline)
    rlibs = list((target / "rust/wasm32-unknown-unknown/debug/deps").glob("liblayerx_program_sdk-*.rlib"))
    if len(rlibs) != 1:
        raise RuntimeError("exact freshly built SDK artifact required")
    run([rustc, "--edition=2021", "--target", "wasm32-unknown-unknown", "--crate-type", "cdylib",
        "-C", "opt-level=2", "-C", "panic=abort", "--extern", "layerx_program_sdk=" + str(rlibs[0]),
        "-L", "dependency=" + str(rlibs[0].parent), ROOT / "programs/sdk/rust/tests/fixtures/language_abi.rs",
        "-o", target / "rust.wasm"], evidence, "build-rust-guest", deadline)
    run([clang, "--target=wasm32-unknown-unknown", "-std=c17", "-Oz", "-ffreestanding", "-fno-builtin",
        "-nostdlib", "-I", ROOT / "programs/sdk/c/include", "-Wl,--no-entry", "-Wl,--export-memory",
        "-Wl,--gc-sections", *sorted((ROOT / "programs/sdk/c/src").glob("*.c")),
        ROOT / "programs/sdk/c/tests/language_abi.c", "-o", target / "c.wasm"], evidence, "build-c-guest", deadline)
    run([node, asc, ROOT / "programs/sdk/assemblyscript/tests/language_abi.ts", "--config",
        ROOT / "programs/sdk/assemblyscript/asconfig.json", "--outFile", target / "assemblyscript.wasm"],
        evidence, "build-assemblyscript-guest", deadline)
    run(["cc", "-std=c17", "-O2", ROOT / "programs/sdk/c/tools/determinism_lint.c", "-o", target / "c-lint"],
        evidence, "build-c-linter", deadline)
    run([cargo, "build", "--offline", "--locked", "--manifest-path", ROOT / "programs/Cargo.toml",
        "-p", "layerx-program-lint", "--bin", "layerx-program-lint", "--target-dir", target / "runtime"],
        evidence, "build-rust-linter", deadline)
    fragment = evidence / "native.mk"
    fragment.write_text(""".PHONY: programs-language-abi-build
programs-language-abi-build: $(BUILD_DIR)/tests/programs_language_abi
$(BUILD_DIR)/tests/programs_language_abi: tests/programs/test_language_abi.c $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
\t@mkdir -p $(@D)
\t$(CC) $(CPPFLAGS) $(CFLAGS) $< $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) $(PROGRAMS_NATIVE_LDLIBS) -lssl -lcrypto -pthread -ldl -lm -o $@
""")
    run(["flock", "/root/lx-cargo/native-build.lock", "make", "-j6", "-f", "Makefile", "-f", fragment,
        "BUILD_DIR=" + str(target / "native"), "PROGRAMS_TARGET_DIR=" + str(target / "runtime"),
        "PROGRAMS_RUNTIME_LIB=" + str(target / "runtime/debug/liblayerx_programs_sandbox.a"),
        "programs-language-abi-build"], evidence, "build-native", deadline)
    if inventory() != source:
        raise RuntimeError("candidate source changed during build")
    artifacts = {language: {"path": str(target / (language + ".wasm")), "sha256": sha(target / (language + ".wasm"))}
        for language in LANGUAGES}
    for name, path in [("native", target / "native/tests/programs_language_abi"), ("c_lint", target / "c-lint"),
        ("rust_lint", target / "runtime/debug/layerx-program-lint")]:
        artifacts[name] = {"path": str(path), "sha256": sha(path)}
    save(evidence / "artifacts.json", {"revision": revision, "source": source, "artifacts": artifacts,
        "abi_version": 4, "compiler": {"clang": str(clang), "rustc": str(rustc), "asc": str(asc)}})


def verify(evidence, deadline):
    manifest = json.loads((evidence / "artifacts.json").read_text())
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT,
        check=True, capture_output=True, text=True).stdout.strip()
    if manifest["revision"] != revision or manifest["source"] != inventory() or manifest["abi_version"] != 4:
        raise RuntimeError("candidate source identity mismatch")
    artifacts = manifest["artifacts"]
    for row in artifacts.values():
        if sha(row["path"]) != row["sha256"]:
            raise RuntimeError("candidate artifact missing or changed")
    shared = ROOT / "programs/sdk/abi-manifest.tsv"
    manifest_parity()
    for language in LANGUAGES:
        run([artifacts["c_lint"]["path"], "--abi-version", "4", "--abi-manifest", shared, artifacts[language]["path"]],
            evidence, "lint-c-" + language, deadline)
        run(["node", ROOT / "programs/sdk/assemblyscript/tools/determinism-lint.mjs", "--abi-version", "4",
            "--abi-manifest", shared, artifacts[language]["path"]], evidence, "lint-as-" + language, deadline)
        run([artifacts["rust_lint"]["path"], "--abi-version", "4", "--artifact", artifacts[language]["path"]],
            evidence, "lint-rust-" + language, deadline)
        for version in (0, 1, 2, 3, 5):
            for label, command in [("c", [artifacts["c_lint"]["path"], "--abi-version", str(version),
                "--abi-manifest", shared, artifacts[language]["path"]]), ("assemblyscript", ["node",
                ROOT / "programs/sdk/assemblyscript/tools/determinism-lint.mjs", "--abi-version", str(version),
                "--abi-manifest", shared, artifacts[language]["path"]]), ("rust", [artifacts["rust_lint"]["path"],
                "--abi-version", str(version), "--artifact", artifacts[language]["path"]])]:
                refuse(command, evidence, f"refuse-{label}-{language}-v{version}", deadline)
    for name, artifact in rejected_artifacts(artifacts["rust"]["path"], evidence):
        for label, command in [("c", [artifacts["c_lint"]["path"], "--abi-version", "4", "--abi-manifest", shared, artifact]),
            ("assemblyscript", ["node", ROOT / "programs/sdk/assemblyscript/tools/determinism-lint.mjs",
                "--abi-version", "4", "--abi-manifest", shared, artifact]),
            ("rust", [artifacts["rust_lint"]["path"], "--abi-version", "4", "--artifact", artifact])]:
            refuse(command, evidence, "refuse-" + label + "-" + name, deadline)
    with (evidence / "native-cases.json").open("wb") as output:
        run([artifacts["native"]["path"], *[artifacts[language]["path"] for language in LANGUAGES]],
            evidence, "native-cases", deadline, stdout=output)
    result = json.loads((evidence / "native-cases.json").read_text())
    if result.get("schema") != "layerx.program-language-abi.v1" or set(result.get("languages", {})) != set(LANGUAGES):
        raise RuntimeError("missing genuine cross-language corpus")
    reference = None
    for language in LANGUAGES:
        cases = result["languages"][language]["cases"]
        if set(cases) != set(CASES):
            raise RuntimeError("required behavior cases missing or skipped")
        for case in cases.values():
            if set(case) != set(FIELDS) | {"terminal_hex"} or type(case["result_code"]) is not int:
                raise RuntimeError("incomplete actual canonical outcome")
            for name in FIELDS[1:]:
                bytes.fromhex(case[name])
            if not bytes.fromhex(case["terminal_hex"]):
                raise RuntimeError("missing actual receipt-visible terminal evidence")
        semantic = {name: {field: cases[name][field] for field in FIELDS} for name in CASES}
        if reference is not None and semantic != reference:
            raise RuntimeError("cross-language canonical behavior differs")
        reference = semantic
    save(evidence / "qualification.json", {"revision": revision, "command": "timeout 30m python3 tools/qualification/paxeer-x/programs_language_abi.py",
        "exit_code": 0, "cases": result, "log_path": str(evidence / "native-cases.log")})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    arguments = parser.parse_args()
    evidence = Path(os.environ.get("PAXEER_X_PROGRAMS_LANGUAGE_EVIDENCE", "/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task7.3"))
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = evidence.stat()
    if info.st_uid != os.geteuid() or info.st_mode & 0o077 or ROOT in evidence.resolve().parents:
        raise RuntimeError("protected evidence outside source checkout required")
    deadline = min(float(os.environ.get("PAXEER_X_TASK_DEADLINE_EPOCH", str(time.time() + 1800))), time.time() + 1800)
    target = Path(os.environ.get("PAXEER_X_PROGRAMS_LANGUAGE_TARGET", "/root/lx-target/task7.3"))
    try:
        if arguments.build:
            build(evidence, target, deadline)
        else:
            verify(evidence, deadline)
    except FileNotFoundError as error:
        print(str(error), file=sys.stderr)
        return 78
    except subprocess.CalledProcessError as error:
        return error.returncode
    except TimeoutError as error:
        print(str(error), file=sys.stderr)
        return 124
    except (RuntimeError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
