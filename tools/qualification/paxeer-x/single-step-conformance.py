#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
DECLARED = (
    "programs/crates/layerx-programs-runtime/src/calls.rs",
    "programs/crates/layerx-programs-runtime/tests/step_conformance.rs",
    "tools/qualification/paxeer-x/single-step-conformance.py",
    "tools/paxeer-x/gates/104.35.2.sh",
    "tools/paxeer-x/build/104.35.2.mk",
)
UNIT_PREFIX = "calls::single_step_conformance::"
UNIT_NAMES = {
    UNIT_PREFIX + "fixed_i32_integer_vectors_replay_every_comparison_and_arithmetic_step",
    UNIT_PREFIX + "fixed_i64_integer_vectors_replay_every_comparison_and_arithmetic_step",
    UNIT_PREFIX + "fixed_integer_width_and_sign_extension_vectors_replay_real_steps",
    UNIT_PREFIX + "fixed_integer_memory_width_growth_fill_and_copy_vectors_replay_real_steps",
    UNIT_PREFIX + "fixed_control_flow_local_global_and_internal_call_vectors_replay_real_steps",
    UNIT_PREFIX + "every_frozen_host_import_replays_its_real_bounds_or_authorization_refusal",
}
INTEGRATION_NAMES = {
    "fixed_unreachable_memory_division_overflow_and_indirect_call_traps_replay_exactly",
    "reference_operand_and_float_instructions_preserve_real_admission_refusals",
}
MISSING_COVERAGE = (
    "successful authenticated native fixtures for every host import",
    "complete permitted table and passive data/element instruction golden inventory",
    "bad-signature, stack-exhaustion and metered-fuel trap golden inventory",
)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    result = subprocess.run(["git", "ls-files", "-z", "--", "programs"],
        cwd=ROOT, capture_output=True, check=True)
    paths = {os.fsdecode(value) for value in result.stdout.split(b"\0") if value} | set(DECLARED)
    return {path: sha(ROOT / path) for path in sorted(paths)
        if Path(path).suffix in {".rs", ".toml", ".lock", ".sh", ".mk", ".py"}}


def execute(command, log, environment, timeout):
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream,
            stderr=subprocess.STDOUT, timeout=timeout)
    log.with_suffix(".exit").write_text(str(result.returncode) + "\n")
    print("exit=" + str(result.returncode) + " log=" + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)
    return log.read_text()


def inventory(output, expected):
    names = {match.group(1) for match in re.finditer(r"^(.+): test$", output, re.M)}
    if names != expected:
        raise RuntimeError("actual enabled task corpus differs from declared inventory")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--build", action="store_true")
    args = parser.parse_args()
    raw = os.environ.get("LAYERX_SINGLE_STEP_EVIDENCE")
    if not raw:
        raise RuntimeError("LAYERX_SINGLE_STEP_EVIDENCE requires a private external evidence directory")
    evidence = Path(raw).resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        raise RuntimeError("evidence directory must be outside source checkout")
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = evidence.stat()
    if info.st_mode & 0o077 or info.st_uid != os.geteuid():
        raise RuntimeError("evidence directory must be private and owned")
    environment = dict(os.environ)
    environment["PATH"] = "/root/.cargo/bin:" + environment.get("PATH", "")
    environment.setdefault("CARGO_BUILD_JOBS", "4")
    environment.setdefault("CARGO_TARGET_DIR", "/root/lx-target/single-step-conformance")
    if args.build:
        inputs = sources()
        output = execute(["/root/.cargo/bin/cargo", "test", "--locked", "--manifest-path",
            "programs/Cargo.toml", "-p", "layerx-programs-runtime", "--lib", "--test",
            "step_conformance", "--no-run", "--message-format=json"],
            evidence / "build.log", environment, 1200)
        binaries = {}
        for line in output.splitlines():
            if not line.startswith("{"):
                continue
            item = json.loads(line)
            if item.get("reason") != "compiler-artifact" or not item.get("executable"):
                continue
            target = item.get("target", {})
            name = target.get("name")
            if name == "step_conformance":
                key = "integration"
            elif name == "layerx_programs_runtime" and item.get("profile", {}).get("test"):
                key = "unit"
            else:
                continue
            if key in binaries:
                raise RuntimeError("ambiguous actual task executable")
            path = str(Path(item["executable"]).resolve(strict=True))
            binaries[key] = {"path": path, "sha256": sha(path)}
        if set(binaries) != {"unit", "integration"} or inputs != sources():
            raise RuntimeError("task build artifacts missing or source changed")
        revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT,
            capture_output=True, text=True, check=True).stdout.strip()
        (evidence / "artifacts.json").write_text(json.dumps({"revision": revision,
            "inputs": inputs, "binaries": binaries}, indent=2) + "\n")
        return 0
    manifest = json.loads((evidence / "artifacts.json").read_text())
    if manifest["inputs"] != sources():
        raise RuntimeError("source mismatch after task build")
    for artifact in manifest["binaries"].values():
        if sha(artifact["path"]) != artifact["sha256"]:
            raise RuntimeError("task executable changed after build")
    unit = manifest["binaries"]["unit"]["path"]
    integration = manifest["binaries"]["integration"]["path"]
    inventory(execute([unit, UNIT_PREFIX, "--list"], evidence / "unit-inventory.log", environment, 30), UNIT_NAMES)
    inventory(execute([integration, "--list"], evidence / "integration-inventory.log", environment, 30), INTEGRATION_NAMES)
    outputs = [
        (execute([unit, UNIT_PREFIX, "--nocapture", "--test-threads=1"],
            evidence / "unit-verify.log", environment, 600), len(UNIT_NAMES)),
        (execute([integration, "--nocapture", "--test-threads=1"],
            evidence / "integration-verify.log", environment, 600), len(INTEGRATION_NAMES)),
    ]
    for output, count in outputs:
        summary = re.search(r"test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;", output)
        if not summary or tuple(map(int, summary.groups())) != (count, 0, 0):
            raise RuntimeError("empty, skipped or incomplete actual task corpus")
    if manifest["inputs"] != sources():
        raise RuntimeError("source changed during task qualification")
    result = {"revision": manifest["revision"], "tests": len(UNIT_NAMES) + len(INTEGRATION_NAMES),
        "corpus_exit": 0, "task_complete": False, "missing_coverage": list(MISSING_COVERAGE),
        "log_paths": [str(evidence / "unit-verify.log"), str(evidence / "integration-verify.log")]}
    (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print("PAXEER_X_GATE tests=" + str(result["tests"]) + " skipped=0")
    print("Full task coverage refused: " + "; ".join(MISSING_COVERAGE), file=sys.stderr)
    return 3


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
