#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" "$@" <<'PYGATE'
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(ROOT / "tools/paxeer-x"))
from candidate import load_private, source_identity

TASK = "104.38.2"
GATE = "tools/paxeer-x/gates/104.38.2.sh"
CASES = {
    "TestResolutionSendReturnsHash",
    "TestResolutionSequencerReceiptRoute",
    "TestSendSequencerReturnsAfterReceipt",
    "TestSendSequencerUsesExecutedGas",
    "TestSendSequencerSkipsLatestSimulation",
    "TestPendingSimulateAccessListOnOpenBlock",
    "TestPendingSimulateRefusedOffSequencer",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def private(path, directory=False):
    path = Path(path)
    info = path.lstat()
    require(
        not path.is_symlink()
        and info.st_uid == os.geteuid()
        and not info.st_mode & 0o077
        and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
        "private caller-owned fixture or evidence required",
    )
    return path.resolve(strict=True)


def identity():
    return source_identity(ROOT, os.environ.get("PAXEER_X_MAINLINE", "refs/heads/main"))


def save(path, value):
    if path.exists():
        private(path)
    descriptor = os.open(
        path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600
    )
    with os.fdopen(descriptor, "w") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")


def run(argv, path):
    remaining = deadline - time.time()
    require(remaining > 0, "bounded task deadline exhausted")
    descriptor = os.open(
        path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600
    )
    with os.fdopen(descriptor, "w") as stream:
        result = subprocess.run(
            [str(arg) for arg in argv],
            cwd=ROOT / "rpc",
            stdin=subprocess.DEVNULL,
            stdout=stream,
            stderr=subprocess.STDOUT,
            timeout=min(600, remaining),
        )
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return path.read_text()


def build():
    before = identity()
    require(not before["dirty"], "clean published-source build required")
    binary = directory / "rpc-transport.test"
    require(not binary.is_symlink(), "build artifact symlink refused")
    argv = ["go", "test", "-c", "-o", str(binary), "."]
    run(argv, directory / "build.log")
    require(
        not binary.is_symlink() and binary.is_file(),
        "genuine RPC build artifact required",
    )
    binary.chmod(0o700)
    private(binary)
    require(
        os.access(binary, os.X_OK) and identity() == before,
        "source-matched actual RPC artifact required",
    )
    save(
        directory / "build-manifest.json",
        {
            "schema": "paxeer-x.rpc-transport-build.v1",
            "source": before,
            "gate_sha256": digest(ROOT / GATE),
            "build_exit_code": 0,
            "command": argv,
            "harness": {"path": str(binary), "sha256": digest(binary)},
        },
    )


def qualify():
    built = load_private(directory / "build-manifest.json")
    current = identity()
    require(
        built.get("schema") == "paxeer-x.rpc-transport-build.v1"
        and built["build_exit_code"] == 0
        and built["source"] == current
        and not current["dirty"]
        and built["gate_sha256"] == digest(ROOT / GATE),
        "successful exact published-source build required",
    )
    row = built["harness"]
    binary = Path(row["path"])
    require(
        binary.is_absolute() and binary == directory / "rpc-transport.test",
        "private source-matched RPC executable required",
    )
    private(binary)
    require(
        os.access(binary, os.X_OK) and digest(binary) == row["sha256"],
        "genuine unchanged prebuilt executable required",
    )
    selector = "^(" + "|".join(sorted(CASES)) + ")$"
    argv = [
        binary,
        "-test.v",
        "-test.count=1",
        "-test.timeout=10m",
        "-test.run=" + selector,
    ]
    output = run(argv, directory / "production.log")
    passed = re.findall(r"^--- PASS: (\S+)", output, re.M)
    require(
        len(passed) == len(CASES)
        and set(passed) == CASES
        and not re.search(r"^\s*--- (?:FAIL|SKIP):", output, re.M),
        "every raw-transaction, signed-receipt and call-view case must pass exactly once",
    )
    require(
        identity() == current
        and digest(binary) == row["sha256"]
        and digest(ROOT / GATE) == built["gate_sha256"],
        "compiled source or harness changed during qualification",
    )
    save(
        directory / "result.json",
        {
            "task": TASK,
            "revision": current["revision"],
            "command": "tools/paxeer-x/verify-task.sh 104.38.2",
            "exit_code": 0,
            "executed_cases": passed,
            "skipped": 0,
            "log_path": str(directory / "production.log"),
            "release_qualification": "UNRUN",
        },
    )
    print("PAXEER_X_GATE tests=" + str(len(CASES)) + " skipped=0", flush=True)


try:
    directory = Path(os.environ["PAXEER_X_RPC_TRANSPORT_EVIDENCE"])
    require(directory.is_absolute(), "absolute private evidence directory required")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory = private(directory, True)
    require(
        ROOT not in directory.parents and directory != ROOT,
        "private evidence must be outside checkout",
    )
    deadline = float(
        os.environ.get("PAXEER_X_TASK_DEADLINE_EPOCH", str(time.time() + 1800))
    )
    args = sys.argv[2:]
    require(args in ([], ["--build"]), "104.38.2 accepts only --build or no arguments")
    if args:
        build()
    else:
        qualify()
except subprocess.CalledProcessError as error:
    print("104.38.2 actual command exit=" + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print("104.38.2 bounded task deadline exhausted", file=sys.stderr)
    sys.exit(124)
except (OSError, ValueError, KeyError, TypeError) as error:
    print(
        "104.38.2 genuine fixture or provenance refused: " + type(error).__name__,
        file=sys.stderr,
    )
    sys.exit(78)
PYGATE
