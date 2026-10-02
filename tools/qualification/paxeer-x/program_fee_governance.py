#!/usr/bin/env python3
"""Qualify governed, versioned program fee schedules against prebuilt native and Rust artifacts."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.32.4"
BUILD_DIR = Path(os.environ.get("BUILD_DIR", "build"))
if not BUILD_DIR.is_absolute():
    BUILD_DIR = ROOT / BUILD_DIR
OUT = BUILD_DIR / "paxeer-x-104.32.4"
MANIFEST = OUT / "artifacts.json"
SOURCES = (
    "programs/crates/layerx-programs-runtime/src/meter.rs",
    "src/modules/programs/fees.c",
    "tools/paxeer-x/gates/104.32.4.sh",
    "tools/qualification/paxeer-x/program_fee_governance.py",
    "tools/paxeer-x/build/104.32.4.mk",
    "tests/programs/test_fee_governance.c",
    "programs/crates/layerx-programs-runtime/tests/fee_governance.rs",
    "include/layerx/programs.h",
    "Makefile",
    "programs/Cargo.lock",
    "rust-toolchain.toml",
)
BINARIES = ("native_fee_governance", "rust_fee_governance", "rust_replay")
CRITERIA = {
    "versioned protocol-state schedule includes every named coefficient/unit": (
        ("native", "schedule-state-named-coefficients"),
        ("rust_fee_governance", "versioned_schedule_names_every_coefficient_and_unit"),
    ),
    "governance proposal accepted only with real verified governance receipt; wrong module, "
    "unsuccessful receipt, missing proof, wrong proposal binding, reused receipt refused": (
        ("native", "governance-accepts-verified-receipt"),
        ("native", "governance-refuses-wrong-module"),
        ("native", "governance-refuses-unsuccessful-receipt"),
        ("native", "governance-refuses-missing-proof"),
        ("native", "governance-refuses-wrong-proposal"),
        ("native", "governance-refuses-reused-receipt"),
    ),
    "pending governed state visible before activation; exact activation boundary and no "
    "retroactive mutation": (
        ("native", "pending-visible-before-activation"),
        ("native", "activation-exact-boundary"),
        ("native", "no-retroactive-mutation"),
        ("rust_fee_governance",
         "pending_schedule_visible_before_exact_activation_without_retroactive_change"),
    ),
    "execution receipt records version and immutable historical replay selects recorded "
    "version, refuses unknown version": (
        ("rust_fee_governance", "receipt_version_selects_historical_schedule_and_refuses_unknown"),
        ("native", "history-replay-recorded-version"),
        ("native", "history-refuses-unknown-version"),
        ("rust_replay", "governed_fee_history_reprices_each_recorded_version_exactly"),
    ),
    "occupancy-derived demand price uses protocol data only; zero/target/high/full-range "
    "occupancy, bounded up/down movement, cap/floor and integer overflow refusal": (
        ("native", "occupancy-zero"),
        ("native", "occupancy-target"),
        ("native", "occupancy-high"),
        ("native", "occupancy-full"),
        ("native", "occupancy-bounded-up"),
        ("native", "occupancy-bounded-down"),
        ("native", "occupancy-cap-floor"),
        ("native", "occupancy-overflow-refused"),
        ("rust_fee_governance", "demand_price_from_occupancy_is_bounded_per_batch"),
        ("rust_fee_governance", "demand_price_cap_floor_and_overflow_are_refused"),
    ),
    "multi-schedule replay preserves historical charges and canonical evidence": (
        ("native", "multi-schedule-replay-preserves-charges"),
        ("rust_fee_governance", "multi_schedule_history_replay_reprices_every_activity"),
        ("rust_replay", "mixed_v1_v2_history_selects_each_recorded_abi_and_fee_schedule"),
    ),
    "retain every existing pending/history and occupancy vector": (
        ("native", "seeded-pending-history"),
        ("native", "occupancy-100-110-90-100"),
        ("rust_replay", "recorded_v1_replays_identically_after_a_simulated_upgrade"),
    ),
}
NATIVE_CASES = {name for cases in CRITERIA.values() for suite, name in cases if suite == "native"}
RUST_TESTS = {suite: tuple(dict.fromkeys(name for cases in CRITERIA.values()
                                         for owner, name in cases if owner == suite))
              for suite in ("rust_fee_governance", "rust_replay")}
CASE = re.compile(r"^FEE_CASE (\{.*\})$", re.M)
SUMMARY = re.compile(r"^FEE_GOVERNANCE cases=(\d+) skipped=(\d+)$", re.M)


def git(*arguments):
    return subprocess.run(["git", *arguments], cwd=ROOT, check=True,
                          capture_output=True, text=True).stdout.strip()


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def candidate():
    if git("status", "--porcelain", "--untracked-files=all", "--", *SOURCES):
        raise RuntimeError("declared sources differ from HEAD")
    return git("rev-parse", "HEAD"), {name: digest(ROOT / name) for name in SOURCES}


def artifacts(revision, sources):
    if not MANIFEST.is_file():
        raise RuntimeError("missing producer artifact manifest; run the declared build stage first")
    manifest = json.loads(MANIFEST.read_text())
    if manifest.get("revision") != revision:
        raise RuntimeError("artifact manifest revision is not HEAD")
    if manifest.get("sources") != sources:
        raise RuntimeError("artifact manifest source digests do not match the checkout")
    for field in ("toolchain", "features", "env", "commands"):
        if not manifest.get(field):
            raise RuntimeError("artifact manifest lacks " + field)
    if "layerx-programs-sandbox/host-ffi" not in manifest["features"]:
        raise RuntimeError("sandbox staticlib was not built with host-ffi")
    paths = {}
    for name in BINARIES:
        entry = manifest.get("binaries", {}).get(name)
        if not entry:
            raise RuntimeError("artifact manifest lacks binary " + name)
        path = Path(entry["path"])
        if not path.is_file() or not os.access(path, os.X_OK):
            raise RuntimeError("declared binary is not an executable file: " + name)
        if digest(path) != entry["sha256"]:
            raise RuntimeError("declared binary digest mismatch: " + name)
        paths[name] = path
    return manifest, paths


def execute(command, log):
    with log.open("w") as stream:
        completed = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=stream,
                                   stderr=subprocess.STDOUT, timeout=900, check=False)
    return completed.returncode, log.read_text(errors="replace")


def native(binary, evidence):
    code, output = execute([str(binary)], evidence / "native_fee_governance.log")
    cases = [json.loads(line) for line in CASE.findall(output)]
    names = [case.get("name") for case in cases]
    failures = []
    if code != 0:
        failures.append(f"native fee governance exited {code}")
    if len(names) != len(set(names)):
        failures.append("duplicate native case name")
    if SUMMARY.findall(output) != [(str(len(cases)), "0")]:
        failures.append("native case accounting missing, skipped or inconsistent")
    missing = sorted(NATIVE_CASES - set(names))
    if missing:
        failures.append("missing native cases: " + ", ".join(missing))
    failed = sorted(str(case.get("name")) for case in cases if case.get("result") != "pass")
    if failed:
        failures.append("failed native cases: " + ", ".join(failed))
    return [{"suite": "native", "name": case.get("name"), "result": case.get("result")}
            for case in cases], failures


def rust(name, binary, test, evidence):
    command = [str(binary), "--exact", test, "--test-threads=1"]
    code, output = execute(command, evidence / f"{name}.{test}.log")
    passed = (code == 0 and re.search(rf"^test {re.escape(test)} \.\.\. ok$", output, re.M)
              and re.search(r"test result: ok\. 1 passed; 0 failed; 0 ignored;", output))
    return {"suite": name, "name": test, "command": command, "exit_code": code,
            "result": "pass" if passed else "fail"}


def main():
    os.umask(0o077)
    root = Path(os.environ["PAXEER_X_EVIDENCE_DIR"])
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    result = {"task": TASK, "tests": 0, "skipped": 0, "cases": [], "failures": [],
              "command": ["timeout", "30m", "python3",
                          "tools/qualification/paxeer-x/program_fee_governance.py"],
              "scope": "focused task gate; complete programs-test remains a release gate"}
    evidence = root
    code = 1
    try:
        revision, sources = candidate()
        result["revision"] = revision
        evidence = root / f"fee-governance-{revision[:12]}-{stamp}"
        evidence.mkdir(mode=0o700)
        manifest, paths = artifacts(revision, sources)
        result["artifact_manifest"] = manifest
        cases, failures = native(paths["native_fee_governance"], evidence)
        for name, tests in RUST_TESTS.items():
            for test in tests:
                case = rust(name, paths[name], test, evidence)
                cases.append(case)
                if case["result"] != "pass":
                    failures.append(f"{name} {test} exited {case['exit_code']}")
        passed = {(case["suite"], case["name"]) for case in cases if case["result"] == "pass"}
        result["criteria"] = {criterion: [{"suite": suite, "case": name,
                                           "result": "pass" if (suite, name) in passed else "fail"}
                                          for suite, name in required]
                              for criterion, required in CRITERIA.items()}
        for criterion, required in CRITERIA.items():
            if not all(case in passed for case in required):
                failures.append("criterion not exercised by passing cases: " + criterion)
        if candidate() != (revision, sources):
            failures.append("candidate changed during qualification")
        result["cases"] = cases
        result["tests"] = len(cases)
        result["failures"] = failures
        code = 1 if failures or not cases else 0
    except (OSError, ValueError, KeyError, TypeError, RuntimeError,
            subprocess.SubprocessError) as error:
        result["failures"].append(str(error))
    for failure in result["failures"]:
        print(failure, file=sys.stderr)
    result["exit_code"] = code
    destination = evidence / "fee-governance-result.json"
    if destination.exists():
        destination = evidence / f"fee-governance-result-{stamp}.json"
    destination.write_text(json.dumps(result, indent=2) + "\n")
    print(f"PAXEER_X_GATE tests={result['tests']} skipped=0")
    print(f"Evidence: {destination}")
    return code


if __name__ == "__main__":
    sys.exit(main())
