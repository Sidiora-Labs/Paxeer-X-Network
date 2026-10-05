#!/usr/bin/env python3
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
TASK = "7.2"
SCHEMA = "paxeer-x.programs-market-resolution.v1"
SOURCE_PATHS = (
    "programs",
    "tools/qualification/paxeer-x/programs_market_resolution.py",
)
PROGRAMS = ROOT / "programs"
MANIFEST = ["--locked", "--manifest-path", "Cargo.toml"]
SUITES = {
    "sdk": {
        "package": "layerx-program-sdk",
        "target": "layerx_program_sdk",
        "filter": "arbiter::tests::",
        "required": {
            "arbiter::tests::market_step_request_and_outcome_round_trip_and_refuse_malformed_framing",
        },
    },
    "market": {
        "package": "layerx-programs-market",
        "target": "layerx_programs_market",
        "filter": None,
        "required": {
            "dispute::tests::opening_requires_a_frozen_claim_challenged_now_with_a_bounded_trace",
            "dispute::tests::bisection_collapses_to_one_authenticated_transition",
            "dispute::tests::agreeing_to_every_boundary_judges_the_terminal_state",
            "dispute::tests::a_wrong_initial_state_or_unopenable_boundary_loses_for_the_provider",
            "dispute::tests::absent_parties_lose_by_the_bisection_rules_and_only_after_the_deadline",
            "dispute::tests::every_dispute_settles_within_a_bounded_number_of_moves",
            "dispute::tests::transcript_absorbs_every_move_in_order",
            "dispute::tests::dispute_rows_are_strictly_decoded",
            "stake::tests::provider_proven_wrong_is_partially_slashed_to_the_challenger",
            "stake::tests::challenger_proven_wrong_compensates_the_provider",
            "stake::tests::slashing_one_of_several_concurrent_leases_keeps_the_others_locked",
            "stake::tests::dispute_heavy_histories_conserve_value",
            "settle::tests::both_arbiter_outcomes_conserve_escrow_and_challenge_stake",
            "settle::tests::dispute_requires_stake_and_contradiction_and_freezes_settlement",
        },
    },
    "runtime": {
        "package": "layerx-programs-runtime",
        "target": "layerx_programs_runtime",
        "filter": "execute::market_tests::",
        "required": {
            "execute::market_tests::compiled_market_funding_expiry_public_settlement_and_close",
            "execute::market_tests::compiled_market_refuses_unfunded_and_unauthorized_payments_atomically",
            "execute::market_tests::compiled_market_dispute_honest_provider_defeats_a_lying_challenger",
            "execute::market_tests::compiled_market_dispute_honest_challenger_slashes_a_lying_provider",
            "execute::market_tests::compiled_market_dispute_timeouts_follow_the_bisection_rules",
            "execute::market_tests::compiled_market_dispute_refuses_forged_wrong_replayed_unauthorized_and_premature_moves",
            "execute::market_tests::compiled_market_dispute_resumes_after_restart_without_duplicate_effects",
            "execute::market_tests::compiled_market_concurrent_leases_are_not_over_slashed_and_the_offer_closes_after_disputes",
        },
    },
}


def fail(message):
    print("FAIL " + message, file=sys.stderr, flush=True)
    sys.exit(1)


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, check=True, timeout=120).stdout


def main():
    if len(sys.argv) != 1:
        fail("usage: programs_market_resolution.py (no arguments)")
    cargo = shlex.split(os.environ.get("PROGRAMS_CARGO", "cargo"))
    configured = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if configured:
        evidence = Path(configured)
        evidence.mkdir(parents=True, exist_ok=True)
        if any(evidence.iterdir()):
            fail("evidence directory must start empty: " + str(evidence))
    else:
        evidence = Path(tempfile.mkdtemp(prefix="programs-market-resolution-"))
    revision = git("rev-parse", "HEAD").strip()
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        fail("candidate revision is not a commit")
    dirty = git("status", "--porcelain", "--", *SOURCE_PATHS)
    if dirty.strip():
        fail("candidate sources differ from revision " + revision + ":\n" + dirty)
    commands = []
    results = {}

    def execute(command, name, timeout, environment=None, cwd=ROOT):
        log = evidence / (name + ".log")
        env = dict(os.environ)
        env.update(environment or {})
        with log.open("w") as output:
            try:
                result = subprocess.run(command, cwd=cwd, stdin=subprocess.DEVNULL, stdout=output,
                                        stderr=subprocess.STDOUT, timeout=timeout, env=env)
            except subprocess.TimeoutExpired:
                fail(name + " timed out; log=" + str(log))
        commands.append({"name": name, "command": command, "cwd": str(cwd), "exit": result.returncode,
                         "log": str(log), "environment": environment or {}})
        print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
        if result.returncode:
            fail(name + " exited " + str(result.returncode) + "; log=" + str(log))
        return log.read_text()

    execute(cargo + ["build", *MANIFEST, "-p", "layerx-programs-market",
                     "--target", "wasm32-unknown-unknown", "--release"], "build-market-guest", 1500, cwd=PROGRAMS)
    metadata = json.loads(subprocess.run(
        cargo + ["metadata", "--format-version", "1", "--no-deps", *MANIFEST],
        cwd=PROGRAMS, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=True,
        timeout=300).stdout)
    guest = Path(metadata["target_directory"]) / "wasm32-unknown-unknown/release/layerx_programs_market.wasm"
    if guest.is_symlink() or not guest.is_file() or guest.read_bytes()[:4] != b"\0asm":
        fail("production Market guest was not produced: " + str(guest))
    guest_bytes = guest.read_bytes()
    if b"layerx_v5" not in guest_bytes or b"market_step_adjudicate" not in guest_bytes:
        fail("production Market guest does not import the ABI V5 market-step adjudication")
    candidate = evidence / "layerx_programs_market.wasm"
    candidate.write_bytes(guest_bytes)
    guest_digest = hashlib.sha256(guest_bytes).hexdigest()

    total = 0
    for suite, spec in SUITES.items():
        built = execute(cargo + ["test", *MANIFEST, "-p", spec["package"], "--lib", "--no-run",
                                 "--message-format=json"], "build-" + suite + "-tests", 2400, cwd=PROGRAMS)
        executables = []
        for line in built.splitlines():
            if not line.startswith("{"):
                continue
            entry = json.loads(line)
            if (entry.get("reason") == "compiler-artifact" and entry.get("executable")
                    and entry.get("target", {}).get("name") == spec["target"]
                    and entry.get("profile", {}).get("test")):
                executables.append(entry["executable"])
        if len(executables) != 1:
            fail("exactly one " + suite + " unit test executable required, found " + str(len(executables)))
        binary = Path(executables[0])
        if binary.is_symlink() or not binary.is_file() or not os.access(binary, os.X_OK):
            fail("invalid " + suite + " unit test executable: " + str(binary))
        inventory = execute([str(binary), "--list"], suite + "-inventory", 240)
        declared = set(re.findall(r"^(.+): test$", inventory, re.M))
        selected = {name for name in declared if spec["filter"] is None or name.startswith(spec["filter"])}
        missing = sorted(spec["required"] - declared)
        if missing:
            fail(suite + " executable omits required cases: " + ", ".join(missing))
        run = [str(binary), "--test-threads=1"]
        if spec["filter"] is not None:
            run.append(spec["filter"])
        output = execute(run, suite + "-cases", 3600,
                         {"LAYERX_MARKET_WASM": str(candidate)} if suite == "runtime" else None)
        counts = re.findall(r"test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;", output)
        if len(counts) != 1 or int(counts[0][0]) != len(selected) or counts[0][1:] != ("0", "0"):
            fail(suite + " corpus was incomplete, failed or ignored: " + repr(counts))
        passed = set(re.findall(r"^test (\S+) \.\.\. ok$", output, re.M))
        if passed != selected:
            fail(suite + " cases did not all pass: " + ", ".join(sorted(selected - passed)))
        results[suite] = {"executable": str(binary), "cases": {name: "ok" for name in sorted(passed)}}
        total += len(passed)

    manifest = {
        "schema": SCHEMA,
        "task": TASK,
        "revision": revision,
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "candidate": {"path": str(candidate), "built_from": str(guest), "sha256": guest_digest},
        "commands": commands,
        "results": results,
        "evidence_directory": str(evidence),
        "tests": total,
        "skipped": 0,
    }
    path = evidence / "manifest.json"
    path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print("manifest=" + str(path), flush=True)
    print("PAXEER_X_GATE tests=" + str(total) + " skipped=0", flush=True)


if __name__ == "__main__":
    main()
