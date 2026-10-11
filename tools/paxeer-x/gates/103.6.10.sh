#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$ROOT" "${1:---verify}" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys

os.umask(0o077)
root = Path(sys.argv[1])
mode = sys.argv[2]
if mode not in ("--build", "--verify"):
    raise SystemExit("103.6.10: expected --build or --verify")
directory_name = os.environ.get("LAYERX_FINALITY_QUALIFICATION_DIR", "")
if not directory_name:
    raise SystemExit("103.6.10: private LAYERX_FINALITY_QUALIFICATION_DIR required")
directory = Path(directory_name)
if (
    not directory.is_absolute()
    or directory.is_symlink()
    or directory.resolve().is_relative_to(root)
):
    raise SystemExit(
        "103.6.10: qualification directory must be private and outside checkout"
    )
directory.mkdir(mode=0o700, parents=True, exist_ok=True)
metadata = directory.stat()
if (
    not stat.S_ISDIR(metadata.st_mode)
    or metadata.st_mode & 0o077
    or metadata.st_uid != os.geteuid()
):
    raise SystemExit("103.6.10: qualification directory permissions must be private")
manifest = directory / "build.json"
SUITES = {
    "custodyproof": {
        "TestRealStateCreditAndClockBounds",
        "TestRealStateCreditRefusesMissingAuthority",
        "TestJSONRequestRefusesAmbiguousFields",
    },
    "node/sequencer": {
        "TestSequencerConfigRequiresWALAndKey",
        "TestStandbyNoAckNoReceipt",
        "TestStandbyReplayMatchesPrimary",
        "TestStandbyAckBeforeRelease",
        "TestStandbyTimeoutRefusesSend",
        "TestStandbyAppliesInSeqOrder",
        "TestStandbyRefusesSharedKeys",
        "TestReplicatorResendsAfterLostCommitted",
    },
    "modules/sequencer/keeper": {
        "TestSequenceBatchMustBeFirst",
        "TestSequenceBatchChainsFromStore",
        "TestSequenceBatchFeeFreeAndBlockOnly",
        "TestReceiptKeysRangeValidation",
        "TestReceiptKeysGovernanceOnly",
        "TestReceiptKeysRotationBoundary",
    },
}


def git(*arguments):
    return subprocess.check_output(
        ["git", "-C", str(root), *arguments], text=True
    ).strip()


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def source():
    if git("status", "--porcelain", "--untracked-files=normal"):
        raise SystemExit("103.6.10: frozen clean published checkout required")
    names = subprocess.check_output(["git", "-C", str(root), "ls-files", "-z"]).split(
        b"\0"
    )
    selected = sorted(os.fsdecode(name) for name in names if name)
    hashes = {
        name: digest(root / name)
        for name in selected
        if not any(part.startswith(".env") for part in Path(name).parts)
        and (
            Path(name).suffix in {".go", ".mod", ".sum", ".s", ".c", ".h"}
            or (
                Path(name).suffix == ".json"
                and not name.startswith("contracts/config/")
            )
            or name.startswith("tests/fixtures/custody/")
            or name.startswith("layerxproof/testvectors/")
            or name == "tools/paxeer-x/gates/103.6.10.sh"
        )
        and (root / name).is_file()
    }
    return {
        "revision": git("rev-parse", "HEAD"),
        "tree": git("rev-parse", "HEAD^{tree}"),
        "sources": hashes,
    }


def run(command, log, workdir=None):
    with log.open("w") as stream:
        completed = subprocess.run(
            command, cwd=workdir or root, stdout=stream, stderr=subprocess.STDOUT
        )
    if completed.returncode:
        raise SystemExit("103.6.10: exit=%d log=%s" % (completed.returncode, log))
    return log.read_text()


def checked(package, row):
    path = directory / (package.replace("/", "-") + ".test")
    if (
        path.is_symlink()
        or not path.is_file()
        or not os.access(path, os.X_OK)
        or row.get("path") != str(path.resolve())
        or row.get("sha256") != digest(path)
    ):
        raise SystemExit("103.6.10: exact source-bound compiled test artifact required")
    return path


if mode == "--build":
    if manifest.exists() or manifest.is_symlink():
        raise SystemExit("103.6.10: refusing to relabel an existing build manifest")
    binding = source()
    go = shutil.which("go")
    if not go:
        raise SystemExit("103.6.10: actual Go compiler required")
    artifacts = {}
    for package in SUITES:
        binary = directory / (package.replace("/", "-") + ".test")
        run(
            [
                go,
                "test",
                "-mod=readonly",
                "-p",
                "2",
                "-c",
                "-o",
                str(binary),
                "./" + package,
            ],
            directory / (package.replace("/", "-") + "-build.log"),
        )
        row = {"path": str(binary.resolve(strict=True)), "sha256": digest(binary)}
        checked(package, row)
        artifacts[package] = row
    if source() != binding:
        raise SystemExit("103.6.10: source changed during build")
    value = dict(
        binding, version=2, purpose="103.6.10-source-bound-build", artifacts=artifacts
    )
    with manifest.open("x") as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write("\n")
    print("103.6.10 build manifest=" + str(manifest))
else:
    metadata = manifest.lstat()
    if (
        not stat.S_ISREG(metadata.st_mode)
        or metadata.st_mode & 0o077
        or metadata.st_uid != os.geteuid()
    ):
        raise SystemExit("103.6.10: private build manifest required")
    if metadata.st_size > 64 * 1024 * 1024:
        raise SystemExit("103.6.10: bounded build manifest required")
    value = json.loads(manifest.read_text())
    binding = source()
    if (
        value.get("version") != 2
        or value.get("purpose") != "103.6.10-source-bound-build"
        or any(value.get(name) != item for name, item in binding.items())
    ):
        raise SystemExit("103.6.10: build manifest does not bind this exact source")
    if set(value.get("artifacts", {})) != set(SUITES):
        raise SystemExit("103.6.10: all declared compiled suites required")
    executed = {}
    for package, mandatory in SUITES.items():
        binary = checked(package, value["artifacts"][package])
        stem = package.replace("/", "-")
        pattern = "^(" + "|".join(re.escape(name) for name in sorted(mandatory)) + ")$"
        inventory = run(
            [str(binary), "-test.list=" + pattern],
            directory / (stem + "-inventory.log"),
            root / package,
        )
        cases = re.findall(r"^Test[^\s]+$", inventory, re.MULTILINE)
        if len(cases) != len(set(cases)) or set(cases) != mandatory:
            raise SystemExit(
                "103.6.10: exact commit, WAL, standby and governed-key case inventory required"
            )
        text = run(
            [
                str(binary),
                "-test.run=" + pattern,
                "-test.v",
                "-test.count=1",
                "-test.parallel=1",
            ],
            directory / (stem + "-verify.log"),
            root / package,
        )
        passed = re.findall(r"^--- PASS: (Test[^\s]+) \([^\n]*\)$", text, re.MULTILINE)
        if (
            len(passed) != len(set(passed))
            or set(passed) != mandatory
            or re.search(r"^[ \t]*--- (?:FAIL|SKIP):", text, re.MULTILINE)
            or not text.splitlines()
            or text.splitlines()[-1] != "PASS"
        ):
            raise SystemExit(
                "103.6.10: every declared case must pass once without skips"
            )
        checked(package, value["artifacts"][package])
        executed[package] = sorted(cases)
    if source() != binding:
        raise SystemExit("103.6.10: source changed during verification")
    for package, row in value["artifacts"].items():
        checked(package, row)
    with (directory / "result.json").open("x") as stream:
        json.dump(
            dict(binding, exit_code=0, executed=executed, skipped=0),
            stream,
            sort_keys=True,
        )
        stream.write("\n")
    print(
        "103.6.10 verified committed state proofs, sequencer WAL/standby and governed receipt keys"
    )
    print(
        "PAXEER_X_GATE tests=%d skipped=0"
        % sum(len(cases) for cases in executed.values())
    )
PY
