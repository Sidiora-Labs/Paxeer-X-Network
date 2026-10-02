#!/usr/bin/env python3
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import socket
import stat
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[3]
TASK = "104.35.7"
SCHEMA = "paxeer-x.market-attestation-artifacts.v1"
SOURCE_PATHS = (
    "Makefile", "rust-toolchain.toml", "programs", "agent/Cargo.toml",
    "agent/Cargo.lock", "agent/unsafe-allowlist.toml", "agent/deny.toml", "agent/crates", "src", "include", "tests/programs",
    "tests/support", "contracts/config/checkpoint-settlement.json",
    "tools/qualification/paxeer-x/program_market_attestation.py",
    "tools/paxeer-x/build/104.35.7.mk", "tools/paxeer-x/gates/104.35.7.sh",
)
UNIT_CASES = {
    "attest::tests::empty_input_plan_seal_preserves_zero_commitments",
    "attest::tests::empty_input_plan_refuses_wrong_tenant_reseal_and_late_input",
    "attest::tests::real_source_commitment_is_sealed_before_work_and_labeled_attested",
    "attest::tests::policy_and_hardware_input_have_canonical_bounded_round_trips",
    "attest::tests::unnamed_attester_and_malformed_policy_are_refused",
    "attest::tests::runtime_authenticated_tenant_is_required_for_commit_and_seal",
    "attest::tests::statement_binds_policy_source_payload_and_observation",
    "attest::tests::production_attestation_wire_refuses_malformed_signature_framing",
    "attest::tests::input_order_replay_missing_and_exact_frozen_root_are_explicit",
    "attest::tests::statement_binds_every_input_and_policy_field",
    "attest::tests::policy_commitment_binds_tenant_lease_revision_and_named_keys",
    "attest::tests::attestation_wire_rejects_every_truncation_trailing_and_invalid_source",
    "attest::tests::named_policy_rejects_duplicate_sparse_empty_and_zero_key_entries",
    "attest::tests::refused_input_commitments_leave_policy_unchanged",
    "attest::tests::input_limit_rejects_overflow_after_canonical_commit_sequence",
    "attest::tests::input_decoder_refuses_verified_execution_relabelling",
}
NATIVE_CASES = {
    "empty-bonded-no-policy-usage-refused",
    "empty-bonded-configure-accepted",
    "empty-bonded-unsealed-usage-refused",
    "empty-bonded-wrong-tenant-seal-refused",
    "empty-bonded-seal-accepted",
    "empty-bonded-frozen-state-valid",
    "empty-bonded-reseal-refused",
    "empty-bonded-late-input-refused",
    "empty-bonded-external-admission-refused",
    "empty-bonded-zero-root-usage-refused",
    "empty-bonded-mutated-root-usage-refused",
    "empty-bonded-swapped-root-usage-refused",
    "empty-bonded-restored-state-root-equal",
    "empty-bonded-restored-usage-exact-root-accepted",
    "empty-bonded-usage-exact-root-accepted",
    "empty-bonded-input-state-unchanged-after-refusals",
    "empty-attested-no-policy-usage-refused",
    "empty-attested-configure-accepted",
    "empty-attested-unsealed-usage-refused",
    "empty-attested-wrong-tenant-seal-refused",
    "empty-attested-seal-accepted",
    "empty-attested-frozen-state-valid",
    "empty-attested-reseal-refused",
    "empty-attested-late-input-refused",
    "empty-attested-external-admission-refused",
    "empty-attested-zero-root-usage-refused",
    "empty-attested-mutated-root-usage-refused",
    "empty-attested-swapped-root-usage-refused",
    "empty-attested-restored-state-root-equal",
    "empty-attested-restored-usage-exact-root-accepted",
    "empty-attested-usage-exact-root-accepted",
    "empty-attested-input-state-unchanged-after-refusals",
    "empty-fraudprovable-no-policy-usage-refused",
    "empty-fraudprovable-configure-accepted",
    "empty-fraudprovable-unsealed-usage-refused",
    "empty-fraudprovable-wrong-tenant-seal-refused",
    "empty-fraudprovable-seal-accepted",
    "empty-fraudprovable-frozen-state-valid",
    "empty-fraudprovable-reseal-refused",
    "empty-fraudprovable-late-input-refused",
    "empty-fraudprovable-external-admission-refused",
    "empty-fraudprovable-zero-root-usage-refused",
    "empty-fraudprovable-mutated-root-usage-refused",
    "empty-fraudprovable-swapped-root-usage-refused",
    "empty-fraudprovable-restored-state-root-equal",
    "empty-fraudprovable-restored-usage-exact-root-accepted",
    "empty-fraudprovable-usage-exact-root-accepted",
    "empty-fraudprovable-input-state-unchanged-after-refusals",
    "native-deploy", "register-offer", "open-lease", "configure-named-attester",
    "register-offer-model-1", "register-offer-model-3",
    "open-lease-model-1", "open-lease-model-3",
    "commit-input", "commit-second-input", "second-attester-accepted",
    "seal-inputs", "bound-domain-refused", "wrong-caller-refused",
    "usage-before-admission-refused", "restored-state-root",
    "named-attester-accepted", "evidence-attested",
    "unnamed-attester-refused", "wrong-named-key-refused", "bad-signature-refused",
    "bound-policy-refused", "bound-revision-refused", "bound-lease-refused",
    "bound-input-refused", "bound-payload-refused", "bound-length-refused",
    "bound-source-refused", "bound-locator-refused", "bound-observation-refused",
    "bound-name-refused", "malformed-truncated-refused", "malformed-trailing-refused",
    "invalid-source-refused", "invalid-name-refused", "duplicate-attester-refused",
    "attester-bound-refused", "input-bound-refused", "zero-commitment-refused",
    "empty-policy-refused", "unsealed-input-refused", "late-commit-refused",
    "uncommitted-input-refused", "mismatched-precommit-refused", "future-observation-refused",
    "preseal-observation-refused", "input-order-refused", "duplicate-input-refused",
    "same-activity-replay-refused", "new-activity-replay-refused", "restored-replay-refused",
    "wrong-tenant-refused", "wrong-provider-refused", "unfunded-lease-refused",
    "expire-funded-lease",
    "partial-admission-settlement-refused", "usage-exact-root-accepted",
    "usage-zero-root-refused", "omitted-root-settlement-refused",
    "usage-swapped-root-refused", "usage-mutated-root-refused",
    "signed-native-receipts-verified", "committed-state-root-verified",
    "refusal-preserves-business-state", "deterministic-admission",
    "bonded-external-input-accepted", "fraudprovable-external-input-accepted",
    "bonded-uncommitted-settlement-refused", "fraud-provable-uncommitted-settlement-refused",
}


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def run(command, **kwargs):
    return subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, check=True, **kwargs)


def git(*arguments):
    return run(["git", "--no-optional-locks", *arguments], capture_output=True, text=True).stdout


def source_identity():
    paths = sorted(set(git("ls-files", "-z", "--", *SOURCE_PATHS).split("\0")) - {""})
    paths = [name for name in paths if ".env" not in Path(name).name and
             (Path(name).suffix in {".rs", ".c", ".h", ".toml", ".lock", ".json", ".py", ".sh", ".mk", ".wat", ".wasm", ".bin", ".kvx", ".hex", ".vec", ".inc", ".def"}
              or Path(name).name in {"Makefile", "build.rs"})]
    required = ["programs/crates/layerx-programs-market/src/attest.rs",
                "programs/crates/layerx-programs-market/src/lib.rs",
                "tests/programs/test_market_attestation.c", *SOURCE_PATHS[-3:]]
    if not set(required).issubset(paths):
        raise RuntimeError("required task sources are not tracked in the candidate")
    if git("status", "--porcelain=v1", "--untracked-files=normal", "--", *SOURCE_PATHS).strip():
        raise RuntimeError("producer source closure is dirty or contains untracked inputs")
    for name in paths:
        if (ROOT / name).is_symlink():
            raise RuntimeError("source closure contains a symbolic link: " + name)
    return {"revision": git("rev-parse", "HEAD").strip(),
            "tree": git("rev-parse", "HEAD^{tree}").strip(),
            "sources": {name: digest(ROOT / name) for name in paths}}


def artifact_base():
    return Path(os.environ.get("PAXEER_X_ATTEST_ARTIFACT_DIR",
                               str(ROOT / "build/paxeer-x-104.35.7"))).resolve()


def write_json(path, value):
    with path.open("x", encoding="utf-8") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")
    path.chmod(0o600)


def active_build():
    base = artifact_base()
    active = json.loads((base / "active.json").read_text())
    identifier = active["build_id"]
    if not re.fullmatch(r"[0-9a-f]{32}", identifier):
        raise RuntimeError("invalid build identity")
    return base / identifier


def begin_build():
    builder = os.environ.get("PAXEER_X_BUILDER_ID", "").strip()
    if not builder:
        raise RuntimeError("PAXEER_X_BUILDER_ID must identify the authorized build host")
    identity = source_identity()
    base = artifact_base()
    base.mkdir(parents=True, exist_ok=True, mode=0o700)
    identifier = uuid.uuid4().hex
    directory = base / identifier
    directory.mkdir(mode=0o700)
    write_json(directory / "start.json", {
        "schema": SCHEMA, "task": TASK, "build_id": identifier,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "builder": builder, "hostname": socket.gethostname(), "source": identity,
        "entrypoint": "make -f Makefile -f tools/paxeer-x/build/104.35.7.mk paxeer-x-build-104.35.7",
    })
    staging = base / ("active." + identifier + ".json")
    write_json(staging, {"build_id": identifier})
    staging.replace(base / "active.json")
    print("Artifact build: " + str(directory), flush=True)


def build(arguments):
    directory = active_build()
    manifest = json.loads((directory / "start.json").read_text())
    if manifest["source"] != source_identity():
        raise RuntimeError("source changed while native library prerequisites were built")
    if manifest["builder"] != os.environ.get("PAXEER_X_BUILDER_ID"):
        raise RuntimeError("build identity changed during production")
    cargo = shlex.split(arguments.cargo)
    cc = shlex.split(arguments.cc)
    target = Path(arguments.target_dir).resolve()
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target))
    native_output = Path(arguments.native_output).resolve()
    native_output.parent.mkdir(parents=True, exist_ok=True)
    native_library = Path(arguments.native_library).resolve()
    runtime_library = target / "debug/liblayerx_programs_sandbox.a"
    commands = [
        ("guest", cargo + ["build", "--locked", "--manifest-path", "programs/Cargo.toml",
         "--target", "wasm32-unknown-unknown", "--release", "-p", "layerx-programs-market"]),
        ("sandbox", cargo + ["build", "--locked", "--manifest-path", "programs/Cargo.toml",
         "-p", "layerx-programs-sandbox", "--features", "host-ffi"]),
        ("native", cc + shlex.split(arguments.cppflags) + shlex.split(arguments.cflags) +
         ["tests/programs/test_market_attestation.c", str(native_library), str(runtime_library),
          str(native_library)] + shlex.split(arguments.ldflags) +
         ["-lcrypto", "-pthread", "-ldl", "-lm", "-o", str(native_output)]),
        ("unit", cargo + ["test", "--locked", "--manifest-path", "programs/Cargo.toml",
         "-p", "layerx-programs-market", "--lib", "--no-run", "--message-format=json"]),
    ]
    manifest["commands"] = {name: command for name, command in commands}
    manifest["features"] = {"sandbox": ["host-ffi"], "market": []}
    manifest["compiler_environment"] = {name: environment.get(name, "") for name in
                                        ("RUSTFLAGS", "CARGO_ENCODED_RUSTFLAGS", "RUSTC", "RUSTC_WRAPPER",
                                         "CARGO_BUILD_RUSTFLAGS", "CARGO_BUILD_TARGET", "AR")}
    manifest["toolchain"] = {}
    for name, command in (("cargo", cargo + ["--version"]), ("rustc", ["rustc", "-vV"]),
                          ("cc", cc + ["--version"])):
        manifest["toolchain"][name] = run(command, capture_output=True, text=True).stdout
    write_json(directory / "producer.json", manifest)
    for name, command in commands:
        print("BUILD " + json.dumps(command), flush=True)
        with (directory / (name + ".stdout.log")).open("x") as output, \
                (directory / (name + ".stderr.log")).open("x") as errors:
            completed = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                       stdout=output, stderr=errors, check=False)
        print("BUILD_RESULT " + name + " " + str(completed.returncode), flush=True)
        if completed.returncode:
            write_json(directory / "failure.json", {"producer": name, "exit_code": completed.returncode,
                       "stdout": str(directory / (name + ".stdout.log")),
                       "stderr": str(directory / (name + ".stderr.log"))})
            raise RuntimeError("producer failed: " + name + "; logs: " + str(directory))
    unit_binaries = set()
    for line in (directory / "unit.stdout.log").read_text().splitlines():
        item = json.loads(line)
        if (item.get("reason") == "compiler-artifact" and item.get("profile", {}).get("test") and
                item.get("target", {}).get("name") == "layerx_programs_market" and item.get("executable")):
            unit_binaries.add(item["executable"])
    if len(unit_binaries) != 1:
        raise RuntimeError("unit producer did not identify exactly one market test executable")
    products = {"guest": target / "wasm32-unknown-unknown/release/layerx_programs_market.wasm",
                "native": native_output, "unit": Path(unit_binaries.pop())}
    manifest["products"] = {}
    for name, origin in products.items():
        destination = directory / {"guest": "market.wasm", "native": "native", "unit": "unit"}[name]
        shutil.copyfile(origin, destination)
        destination.chmod(0o600 if name == "guest" else 0o700)
        manifest["products"][name] = {"file": destination.name, "sha256": digest(destination),
                                      "producer_path": str(origin)}
    manifest["linked_libraries"] = {str(path): digest(path) for path in (native_library, runtime_library)}
    if manifest["source"] != source_identity():
        raise RuntimeError("source changed during artifact production")
    manifest["required_native_cases"] = sorted(NATIVE_CASES)
    manifest["required_unit_cases"] = sorted(UNIT_CASES)
    manifest["completed_at"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    write_json(directory / "artifacts.json", manifest)
    print("Artifacts: " + str(directory / "artifacts.json"), flush=True)


def evidence_directory():
    raw = os.environ.get("PAXEER_X_EVIDENCE_DIR")
    if not raw:
        raise RuntimeError("PAXEER_X_EVIDENCE_DIR is required")
    base = Path(raw).resolve()
    information = base.stat()
    if ROOT == base or ROOT in base.parents or not stat.S_ISDIR(information.st_mode) or \
            information.st_uid != os.geteuid() or information.st_mode & 0o077:
        raise RuntimeError("evidence must be a caller-owned private directory outside the checkout")
    directory = base / ("market-attestation-" + uuid.uuid4().hex)
    directory.mkdir(mode=0o700)
    return directory


def execute(command, directory, name, timeout):
    path = directory / (name + ".log")
    code = None
    try:
        with path.open("x") as output:
            process = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=output,
                                     stderr=subprocess.STDOUT, timeout=timeout, check=False)
            code = process.returncode
    except subprocess.TimeoutExpired:
        code = 124
    return code, path.read_text(errors="replace"), str(path)


def qualify():
    directory = evidence_directory()
    result = {"task": TASK, "tests": 0, "skipped": 0, "failures": [],
              "required_native_cases": sorted(NATIVE_CASES), "required_unit_cases": sorted(UNIT_CASES),
              "release_prerequisite": "make programs-test once per merged wave revision; not run here",
              "capacity_contract": {
                  "input-bound-refused": "Real native kernel blob capacity refusal at 512; unchanged policy and input state required",
                  "attest::tests::input_limit_rejects_overflow_after_canonical_commit_sequence":
                      "Supplementary source unit: 1024 canonical guest commitments, then 1025th refused; not native execution proof",
              }}
    try:
        artifacts = active_build()
        manifest = json.loads((artifacts / "artifacts.json").read_text())
        result["artifact_manifest"] = manifest
        if manifest.get("schema") != SCHEMA or manifest.get("task") != TASK or \
                manifest.get("build_id") != artifacts.name or manifest.get("source") != source_identity():
            raise RuntimeError("artifact provenance does not match the current candidate")
        if not manifest.get("builder") or manifest["builder"] != os.environ.get("PAXEER_X_BUILDER_ID"):
            raise RuntimeError("artifact builder does not match PAXEER_X_BUILDER_ID")
        if manifest.get("required_native_cases") != sorted(NATIVE_CASES) or \
                manifest.get("required_unit_cases") != sorted(UNIT_CASES):
            raise RuntimeError("artifact case manifest differs from the gate contract")
        paths = {}
        for name, filename in (("guest", "market.wasm"), ("native", "native"), ("unit", "unit")):
            item = manifest["products"][name]
            path = artifacts / filename
            if item["file"] != filename or path.is_symlink() or digest(path) != item["sha256"]:
                raise RuntimeError("artifact hash or path mismatch: " + name)
            if name != "guest" and not os.access(path, os.X_OK):
                raise RuntimeError("artifact is not executable: " + name)
            paths[name] = path
        if paths["guest"].read_bytes()[:8] != b"\x00asm\x01\x00\x00\x00":
            raise RuntimeError("guest is not a WebAssembly version 1 module")
        native_command = [str(paths["native"]), str(paths["guest"])]
        native_code, output, native_log = execute(native_command, directory, "native", 900)
        result["native"] = {"command": native_command, "exit_code": native_code, "log": native_log}
        cases = []
        for line in output.splitlines():
            if line.startswith('{"case":'):
                cases.append(json.loads(line))
        result["native"]["cases"] = cases
        result["tests"] += len(cases)
        names = [case.get("case") for case in cases]
        if set(names) != NATIVE_CASES or len(names) != len(NATIVE_CASES):
            result["failures"].append({"native_case_inventory": {
                "missing": sorted(NATIVE_CASES - set(names)), "unexpected": sorted(set(names) - NATIVE_CASES),
                "duplicates": sorted({name for name in names if names.count(name) > 1})}})
        if native_code != 0 or any(case.get("passed") is not True or
                                   type(case.get("result_code")) is not int for case in cases):
            result["failures"].append("native case suite failed; preserve its exact refusal and receipt evidence")
        list_code, listing, list_log = execute([str(paths["unit"]), "--list", "--format", "terse"],
                                               directory, "unit-list", 60)
        discovered = {line[:-6] for line in listing.splitlines()
                      if line.startswith("attest::tests::") and line.endswith(": test")}
        result["unit_list"] = {"exit_code": list_code, "log": list_log, "cases": sorted(discovered)}
        if list_code != 0 or not UNIT_CASES.issubset(discovered):
            result["failures"].append("precompiled binary is missing required attestation unit tests")
        unit_command = [str(paths["unit"]), "attest::tests::", "--nocapture", "--test-threads=1"]
        unit_code, unit_output, unit_log = execute(unit_command, directory, "unit", 300)
        result["unit"] = {"command": unit_command, "exit_code": unit_code, "log": unit_log}
        executed = re.findall(r"^test (attest::tests::\S+) \.\.\. (ok|FAILED|ignored)$", unit_output, re.M)
        result["tests"] += sum(status != "ignored" for _, status in executed)
        result["skipped"] += sum(status == "ignored" for _, status in executed)
        if unit_code != 0 or {name for name, _ in executed} != discovered or \
                len(executed) != len(discovered) or any(status != "ok" for _, status in executed):
            result["failures"].append("required precompiled attestation units failed or were skipped")
        if manifest["source"] != source_identity():
            result["failures"].append("candidate source changed during qualification")
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
        result["failures"].append(str(error))
    result["exit_code"] = int(bool(result["failures"]))
    write_json(directory / "result.json", result)
    print(f"PAXEER_X_GATE tests={result['tests']} skipped={result['skipped']}")
    print("Evidence: " + str(directory / "result.json"))
    for failure in result["failures"]:
        print(json.dumps(failure), file=sys.stderr)
    return result["exit_code"]


def main():
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--begin-build", action="store_true")
    mode.add_argument("--build", action="store_true")
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--target-dir", default=str(ROOT / "programs/target"))
    parser.add_argument("--cc", default="cc")
    parser.add_argument("--cppflags", default="")
    parser.add_argument("--cflags", default="")
    parser.add_argument("--ldflags", default="")
    parser.add_argument("--native-library", default="build/liblayerx.a")
    parser.add_argument("--native-output", default="build/tests/programs_market_attestation")
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.begin_build:
        begin_build()
        return 0
    if arguments.build:
        build(arguments)
        return 0
    return qualify()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as failure:
        print(str(failure), file=sys.stderr)
        sys.exit(1)
