#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
exec python3 - "$root" "$@" <<'PY'
import collections
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(sys.argv[1]).resolve()
TASK = "103.6.3"
SCHEMA = "layerx.chain-proof-task-build.v2"
GATE = "tools/paxeer-x/gates/103.6.3.sh"
EVIDENCE_VARIABLE = "PAXEER_X_AUTHORITY_EVIDENCE"
SUITES = {
    "commit_header": (
        "custodyproof",
        (
            "TestRealStateCreditAndClockBounds",
            "TestRealStateCreditRefusesMissingAuthority",
            "TestJSONRequestRefusesAmbiguousFields",
        ),
    ),
    "receipt_keys": (
        "modules/sequencer/keeper",
        (
            "TestReceiptKeysRangeValidation",
            "TestReceiptKeysGovernanceOnly",
            "TestReceiptKeysRotationBoundary",
        ),
    ),
    "leaders": (
        "consensus/internal/consensus/types",
        (
            "TestLeaderPinRoundRobinFromActivation",
            "TestLeaderPinRoundRobin",
            "TestLeaderPinRefusesInvalidList",
            "TestLeaderPinSafeRoundStateAgrees",
            "TestLeaderPinIndependentOfRestartOrder",
        ),
    ),
    "standby": (
        "node/sequencer",
        (
            "TestStandbyNoAckNoReceipt",
            "TestStandbyReplayMatchesPrimary",
            "TestStandbyAckBeforeRelease",
            "TestResolutionStandbyReplay",
        ),
    ),
    "rpc": (
        "rpc",
        (
            "TestResolutionSendReturnsHash",
            "TestResolutionSequencerReceiptRoute",
            "TestPendingSimulateAccessListOnOpenBlock",
            "TestPendingSimulateRefusedOffSequencer",
        ),
    ),
}

RELEASE_REQUIRED = ["merged wave qualification", "activation and failover rehearsal"]


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require(
        (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
        and not path.is_symlink()
        and info.st_uid == os.geteuid()
        and not info.st_mode & 0o077,
        "private caller-owned evidence required",
    )


def evidence():
    base = Path.home() / ".local/state/paxeer-x/qualification"
    base.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(base, True)
    path = Path(os.environ.get(EVIDENCE_VARIABLE, str(base / TASK)))
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    private(path, True)
    path = path.resolve(strict=True)
    require(
        path != ROOT and ROOT not in path.parents,
        "evidence must remain outside checkout",
    )
    return path, base / "rpc-contract.lock"


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    with path.open("w") as output:
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")
    path.chmod(0o600)


def load(path):
    private(path)
    return json.loads(path.read_text())


def git(*arguments):
    return subprocess.check_output(["git", *arguments], cwd=ROOT, text=True).strip()


def source_snapshot():
    inventory = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).split(
        b"\0"
    )
    paths = {os.fsdecode(path) for path in inventory if path} | {GATE}
    suffixes = {
        ".go",
        ".mod",
        ".sum",
        ".c",
        ".h",
        ".s",
        ".S",
        ".syso",
        ".json",
        ".bin",
        ".wasm",
        ".profile",
        ".credit",
        ".sh",
    }
    return {
        path: digest(ROOT / path)
        for path in sorted(paths)
        if not any(part.startswith(".env") for part in Path(path).parts)
        and Path(path).suffix in suffixes
        and (ROOT / path).is_file()
    }


def checked(row, revision):
    path = Path(row["path"])
    require(
        path.is_absolute() and row["source_revision"] == revision,
        "compiled executable revision binding required",
    )
    private(path)
    require(
        os.access(path, os.X_OK) and digest(path) == row["sha256"],
        "compiled executable changed or missing",
    )
    return path


def command(argv, log, cwd, environment):
    if log.exists() or log.is_symlink():
        private(log)
    with log.open("w") as output:
        result = subprocess.run(
            [str(value) for value in argv],
            cwd=cwd,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            timeout=1800,
        )
    private(log)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def build(directory):
    record = directory / "build-manifest.json"
    if record.exists() or record.is_symlink():
        private(record)
        record.unlink()
    before = source_snapshot()
    revision, tree = git("rev-parse", "HEAD"), git("rev-parse", "HEAD^{tree}")
    go = os.environ.get("GO", "go")
    environment = dict(os.environ)
    artifacts, commands = {}, []
    for name, (package, _) in SUITES.items():
        executable = directory / (name + ".test")
        if executable.exists() or executable.is_symlink():
            private(executable)
            executable.unlink()
        argv = [
            go,
            "test",
            "-mod=readonly",
            "-p",
            "4",
            "-c",
            "-o",
            str(executable),
            "./" + package,
        ]
        command(argv, directory / (name + "-build.log"), ROOT, environment)
        executable.chmod(0o700)
        artifacts[name] = {
            "path": str(executable),
            "sha256": digest(executable),
            "source_revision": revision,
        }
        checked(artifacts[name], revision)
        commands.append(argv)
    require(source_snapshot() == before, "actual compiler dependency source changed")
    require(
        revision == git("rev-parse", "HEAD")
        and tree == git("rev-parse", "HEAD^{tree}"),
        "revision changed during compilation",
    )
    save(
        record,
        {
            "schema": SCHEMA,
            "task": TASK,
            "source_revision": revision,
            "source_tree": tree,
            "source_hashes": before,
            "artifacts": artifacts,
            "commands": commands,
            "build_exit_code": 0,
            "release_required": RELEASE_REQUIRED,
            "release_qualification": "UNRUN",
        },
    )
    print("ARTIFACTS " + str(record), flush=True)


def qualify(directory):
    built = load(directory / "build-manifest.json")
    require(
        built.get("schema") == SCHEMA
        and built.get("task") == TASK
        and built.get("build_exit_code") == 0,
        "successful scoped chain build required",
    )
    revision = git("rev-parse", "HEAD")
    require(
        built["source_revision"] == revision
        and built["source_tree"] == git("rev-parse", "HEAD^{tree}"),
        "build revision changed",
    )
    require(
        source_snapshot() == built["source_hashes"],
        "compiled dependency source changed",
    )
    require(
        set(built["artifacts"]) == set(SUITES), "complete compiled test suites required"
    )
    environment = dict(os.environ)
    go = os.environ.get("GO", "go")
    executed = {}
    for name, (package, cases) in SUITES.items():
        binary = checked(built["artifacts"][name], revision)
        pattern = "^(" + "|".join(re.escape(case) for case in cases) + ")$"
        inventory = command(
            [binary, "-test.list", pattern],
            directory / (name + "-inventory.log"),
            ROOT / package,
            environment,
        )
        declared = [
            line for line in inventory.splitlines() if re.fullmatch(pattern, line)
        ]
        require(
            collections.Counter(declared) == collections.Counter(cases),
            "each mandatory chain test must be declared exactly once: " + name,
        )
        save(
            directory / (name + "-inventory.json"),
            {
                "task_cases": sorted(cases),
                "release_required": RELEASE_REQUIRED,
                "release_qualification": "UNRUN",
            },
        )
        argv = [
            go,
            "tool",
            "test2json",
            "-p",
            name,
            binary,
            "-test.v=test2json",
            "-test.count=1",
            "-test.run",
            pattern,
            "-test.timeout=30m",
        ]
        log = directory / (name + "-contract.log")
        text = command(argv, log, ROOT / package, environment)
        events = [json.loads(line) for line in text.splitlines() if line]
        require(
            all(
                isinstance(event, dict) and event.get("Package") == name
                for event in events
            ),
            "chain test event identity required",
        )
        passed = [
            event["Test"]
            for event in events
            if event.get("Action") == "pass" and event.get("Test") in cases
        ]
        started = [
            event["Test"]
            for event in events
            if event.get("Action") == "run" and "/" not in event.get("Test", "")
        ]
        require(
            collections.Counter(started) == collections.Counter(cases)
            and collections.Counter(passed) == collections.Counter(cases),
            "each mandatory chain test must run and pass exactly once: " + name,
        )
        require(
            not any(event.get("Action") in {"fail", "skip"} for event in events)
            and len(
                [
                    event
                    for event in events
                    if event.get("Action") == "pass" and "Test" not in event
                ]
            )
            == 1,
            "complete non-skipped chain test result required",
        )
        for event in events:
            if event.get("Action") == "output":
                print(event.get("Output", ""), end="", flush=True)
        executed[name] = sorted(cases)
    require(
        source_snapshot() == built["source_hashes"],
        "source changed during qualification",
    )
    require(
        revision == git("rev-parse", "HEAD")
        and built["source_tree"] == git("rev-parse", "HEAD^{tree}"),
        "revision changed during qualification",
    )
    for row in built["artifacts"].values():
        checked(row, revision)
    save(
        directory / "result.json",
        {
            "task": TASK,
            "revision": revision,
            "command": "tools/paxeer-x/verify-task.sh " + TASK,
            "exit_code": 0,
            "log_path": str(directory),
            "executed_tests": executed,
            "skipped": 0,
            "release_required": RELEASE_REQUIRED,
            "release_qualification": "UNRUN",
        },
    )
    print(
        f"PAXEER_X_GATE tests={sum(map(len, executed.values()))} skipped=0", flush=True
    )


def main():
    os.umask(0o077)
    args = sys.argv[2:]
    require(args in ([], ["--build"]), TASK + " accepts only --build or no arguments")
    directory, lock_path = evidence()
    descriptor = os.open(lock_path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as lock:
        private(lock_path)
        fcntl.flock(lock, fcntl.LOCK_EX)
        result = directory / "result.json"
        if result.exists() or result.is_symlink():
            private(result)
            result.unlink()
        if args:
            build(directory)
        else:
            qualify(directory)


try:
    main()
except subprocess.CalledProcessError as error:
    print(TASK + " command exit=" + str(error.returncode), file=sys.stderr)
    sys.exit(error.returncode if error.returncode > 0 else 1)
except subprocess.TimeoutExpired:
    print(TASK + " bounded chain command timeout", file=sys.stderr)
    sys.exit(124)
except (RuntimeError, OSError, KeyError, ValueError, TypeError) as error:
    print(
        TASK + " prerequisite or artifact binding refused: " + str(error),
        file=sys.stderr,
    )
    sys.exit(78)
PY
