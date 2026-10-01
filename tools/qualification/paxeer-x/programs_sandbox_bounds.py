#!/usr/bin/env python3
"""Build or qualify the real registered native sandbox decoder with ASan."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
TARGET = ROOT / "build/paxeer-x-6.1"
BINARY = TARGET / "programs_registration"
MANIFEST = TARGET / "candidate.json"
SOURCES = (
    "src/modules/programs/sandbox.c",
    "src/modules/programs/registration.c",
    "include/layerx/programs.h",
    "tests/programs/test_registration.c",
    "tools/qualification/paxeer-x/programs_sandbox_bounds.py",
    "tools/paxeer-x/gates/6.1.sh",
)


def run(command, **kwargs):
    return subprocess.run(command, cwd=ROOT, check=True, **kwargs)


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def candidate():
    revision = run(["git", "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
    run(["git", "diff", "--quiet", "HEAD", "--", *SOURCES])
    return revision, {name: digest(ROOT / name) for name in SOURCES}


def build():
    revision, sources = candidate()
    TARGET.mkdir(parents=True, exist_ok=True)
    MANIFEST.unlink(missing_ok=True)
    commands = [
        ["cargo", "build", "--locked", "--manifest-path", "programs/Cargo.toml",
         "-p", "layerx-programs-sandbox", "--features", "host-ffi"],
        ["make", "-j5", "BUILD_DIR=build/paxeer-x-6.1", "OPT_LEVEL=-O1",
         f"LXP_REVISION={revision}",
         "EXTRA_CFLAGS=-g -fsanitize=address -fno-omit-frame-pointer",
         "build/paxeer-x-6.1/liblayerx.a"],
        [os.environ.get("CC", "cc"), "-std=c17", "-pedantic", "-Werror", "-Wall",
         "-Wextra", "-Wconversion", "-Wshadow", "-Wvla", "-O1", "-g",
         "-fsanitize=address", "-fno-omit-frame-pointer", "-Iinclude",
         "-Ibuild/paxeer-x-6.1/generated", "tests/programs/test_registration.c",
         "-Wl,--start-group", "build/paxeer-x-6.1/liblayerx.a",
         "programs/target/debug/liblayerx_programs_sandbox.a", "-Wl,--end-group",
         "-lcrypto", "-pthread", "-ldl", "-lm", "-o", str(BINARY)],
    ]
    environment = dict(os.environ, CARGO_TARGET_DIR=str(ROOT / "programs/target"))
    for command in commands:
        print("BUILD " + json.dumps(command), flush=True)
        run(command, env=environment)
    if candidate() != (revision, sources):
        raise RuntimeError("candidate changed during build")
    MANIFEST.write_text(json.dumps({"revision": revision, "sources": sources,
        "binary_sha256": digest(BINARY), "commands": commands,
        "detector": "address-sanitizer", "target": "programs_registration"}, indent=2) + "\n")


def qualify():
    evidence = Path(os.environ["PAXEER_X_EVIDENCE_DIR"]) if "PAXEER_X_EVIDENCE_DIR" in os.environ else Path(tempfile.mkdtemp(prefix="paxeer-x-sandbox-bounds-"))
    evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(evidence, 0o700)
    result = {"task": "6.1", "tests": 0, "skipped": 0, "cases": [],
              "command": ["timeout", "30m", "python3", "tools/qualification/paxeer-x/programs_sandbox_bounds.py"],
              "boundary": "kernel-registered native module decoder",
              "authority": "not applicable to decode-only acceptance; no validation/execution invoked",
              "receipts": "not applicable; decoder must stage no state and emit no effects"}
    code = 1
    try:
        revision, sources = candidate()
        result["revision"] = revision
        if not BINARY.is_file() or not MANIFEST.is_file():
            raise RuntimeError("missing prebuilt ASan candidate binary or provenance manifest; build first")
        manifest = json.loads(MANIFEST.read_text())
        if (manifest.get("revision") != revision or manifest.get("sources") != sources or
                manifest.get("binary_sha256") != digest(BINARY) or
                manifest.get("detector") != "address-sanitizer"):
            raise RuntimeError("prebuilt artifact does not match the exact candidate and detector")
        command = [str(BINARY), "--sandbox-bounds"]
        result["binary_command"] = command
        environment = dict(os.environ, ASAN_OPTIONS="abort_on_error=1:detect_leaks=1:halt_on_error=1")
        with (evidence / "decoder.log").open("w") as log:
            completed = subprocess.run(command, cwd=ROOT, env=environment,
                stdout=log, stderr=subprocess.STDOUT, timeout=300, check=False)
        result["decoder_exit_code"] = completed.returncode
        result["decoder_log"] = str(evidence / "decoder.log")
        output = (evidence / "decoder.log").read_text()
        cases = [json.loads(line.removeprefix("SANDBOX_CASE ")) for line in output.splitlines()
                 if line.startswith("SANDBOX_CASE ")]
        result["cases"] = cases
        result["tests"] = len(cases)
        if completed.returncode != 0:
            raise RuntimeError(f"registered decoder exited {completed.returncode}; see decoder.log")
        if "AddressSanitizer:" in output or "LeakSanitizer:" in output:
            raise RuntimeError("memory-safety detector reported a failure")
        summaries = re.findall(r"^SANDBOX_BOUNDS detector=address-sanitizer cases=(\d+) skipped=0$", output, re.M)
        if summaries != [str(len(cases))] or not cases:
            raise RuntimeError("missing or inconsistent nonzero case accounting")
        for abi in (3, 4):
            for operation, length in ((1, 236), (2, 248), (3, 76)):
                observed = [case["length"] for case in cases if case["abi"] == abi and case["name"] == f"prefix-{operation}"]
                if observed != list(range(length)):
                    raise RuntimeError("missing, duplicate or skipped truncated-prefix case")
            required = {"execute-canonical", "activate-canonical", "fund-canonical",
                        "execute-maximum-nested-fields", "fund-maximum-transfer",
                        "invalid-header", "nested-call-prefix", "nested-transfer-prefix",
                        "execute-trailing", "fund-trailing", "activate-trailing",
                        "fund-lifecycle-overflow", "execute-length-overflow",
                        "execute-zero-identifier", "execute-zero-fee-version",
                        "nested-call-invalid-entrypoint", "nested-call-over-limit"}
            if not required.issubset({case["name"] for case in cases if case["abi"] == abi}):
                raise RuntimeError("missing required canonical or refusal family")
        if any(case["result"] != case["expected"] or case["staged"] != 0 or case["effects"] != 0 for case in cases):
            raise RuntimeError("case outcome or no-effect invariant failed")
        result["artifact_manifest"] = manifest
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        result["failure"] = str(error)
        print(str(error), file=sys.stderr)
    result["exit_code"] = code
    destination = evidence / "result.json"
    destination.write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(destination, 0o600)
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0")
    print(f"Evidence: {destination}")
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build", action="store_true", help="build only the declared native ASan target and its production dependencies")
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.build:
        build()
        return 0
    return qualify()


if __name__ == "__main__":
    sys.exit(main())
