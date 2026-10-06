#!/usr/bin/env python3
"""Airdrop entitlement gate: runs the prebuilt launchpad keeper, precompile and
node test binaries of the current revision and requires every airdrop case."""
import os
import re
import subprocess
import sys
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
MODULE = "github.com/Sidiora-Labs/Paxeer-X-Network"

TARGETS = [
    ("launchpad-keeper.test", "modules/launchpad/keeper", [
        "TestAirdropSharesByHolding",
        "TestAirdropEntitlements",
        "TestAirdropEntitlements/basis",
        "TestAirdropEntitlements/transfer_after_claim",
        "TestAirdropEntitlements/mint_burn_after_boundary",
        "TestAirdropEntitlements/refusals_are_atomic",
        "TestAirdropEntitlements/older_epoch",
        "TestAirdropEntitlements/genesis_reload_and_legacy_epochs",
        "TestGenesisRoundTrip",
    ]),
    ("launchpad-precompile.test", "precompiles/launchpad", [
        "TestClaimAirdropForEpochThroughThePrecompile",
        "TestFeeRightsAndGuardianThroughThePrecompile",
        "TestRequiredGasIsPinned",
    ]),
    ("node-launchpad.test", "node", [
        "TestLaunchpadAirdropEntitlementsSurviveCommitAndReopen",
    ]),
]

RESULT = re.compile(r"^\s*--- (PASS|FAIL|SKIP): (\S+)")


def fail(message):
    print(f"FAIL: {message}", flush=True)
    print("PAXEER_X_GATE tests=0 skipped=0", flush=True)
    sys.exit(1)


def git(*args):
    return subprocess.run(["git", "-C", ROOT, *args], check=True, capture_output=True, text=True).stdout.strip()


def main():
    log_dir = os.environ.get("TASK_LOG_DIR")
    evidence_root = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if not log_dir or not evidence_root:
        fail("TASK_LOG_DIR and PAXEER_X_EVIDENCE_DIR must be set (source .task-env)")
    evidence_root = os.path.abspath(evidence_root)
    if evidence_root == ROOT or evidence_root.startswith(ROOT + os.sep):
        fail(f"evidence directory {evidence_root} is inside the source tree")
    revision = git("rev-parse", "HEAD")
    if git("status", "--porcelain", "--untracked-files=all"):
        fail("source tree is dirty; the prebuilt targets cannot be bound to a revision")
    committed = int(git("log", "-1", "--format=%ct", "HEAD"))
    bundle = os.path.join(log_dir, "bundle")
    evidence = os.path.join(evidence_root, f"task-4.1-{revision[:12]}-{int(time.time())}")
    os.makedirs(evidence, mode=0o700)
    print(f"revision {revision}", flush=True)
    print(f"evidence {evidence}", flush=True)

    tests = skipped = 0
    code = 0
    for binary, package, required in TARGETS:
        path = os.path.join(bundle, binary)
        if not os.path.isfile(path) or not os.access(path, os.X_OK):
            fail(f"missing prebuilt target {path}")
        if os.path.getmtime(path) < committed:
            fail(f"{path} predates revision {revision}")
        info = subprocess.run(["go", "version", "-m", path], capture_output=True, text=True)
        if info.returncode != 0 or f"{MODULE}/{package}.test" not in info.stdout:
            fail(f"{path} is not the test binary of {MODULE}/{package}")
        tops = sorted({name.split("/")[0] for name in required})
        command = [path, "-test.run", "^(" + "|".join(tops) + ")$", "-test.count=1", "-test.v", "-test.timeout=25m"]
        print("command " + " ".join(command), flush=True)
        run = subprocess.run(command, cwd=os.path.join(ROOT, package), capture_output=True, text=True)
        output = run.stdout + run.stderr
        log = os.path.join(evidence, binary + ".log")
        with open(os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as out:
            out.write(output)
        print(f"exit {run.returncode} log {log}", flush=True)
        results = {}
        for line in output.splitlines():
            match = RESULT.match(line)
            if match:
                results[match.group(2)] = match.group(1)
        tests += sum(1 for state in results.values() if state in ("PASS", "FAIL"))
        skipped += sum(1 for state in results.values() if state == "SKIP")
        if not results:
            print(f"{binary}: zero cases ran", flush=True)
            code = 1
        for name in required:
            if results.get(name) != "PASS":
                print(f"{binary}: required case {name} is {results.get(name, 'missing')}", flush=True)
                code = 1
        if run.returncode != 0:
            print(output[-4000:], flush=True)
            code = 1
    if tests == 0 or skipped:
        code = 1
    print(f"PAXEER_X_GATE tests={tests} skipped={skipped}", flush=True)
    print(f"result revision={revision} exit={code} evidence={evidence}", flush=True)
    sys.exit(code)


if __name__ == "__main__":
    main()
