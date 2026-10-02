#!/usr/bin/env python3
"""Program-authorised transfer legs (task 104.30.2).

Runs the prebuilt native kernel settlement test and the Rust transfer-law and
real-guest monetary-law executables recorded by the scoped producer manifest.
Nothing is compiled here; the manifest must bind the exact checkout.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TASK = "104.30.2"
MAINLINE = os.environ.get("PAXEER_X_MAINLINE", "")
TOUCHES = {
    "programs/crates/layerx-programs-runtime/src/transfer.rs",
    "programs/crates/layerx-programs-runtime/tests/monetary_law.rs",
    "tools/paxeer-x/gates/104.30.2.sh",
    "tools/qualification/paxeer-x/program_transfer_authority.py",
    "tools/paxeer-x/build/104.30.2.mk",
    "tests/programs/test_monetary_law.c",
}
INPUTS = sorted({"tests/programs/test_monetary_law.c", "programs/Cargo.lock", "rust-toolchain.toml",
                 "programs/crates/layerx-programs-runtime/src/transfer.rs",
                 "programs/crates/layerx-programs-runtime/tests/monetary_law.rs",
                 "tools/paxeer-x/build/104.30.2.mk", "tools/paxeer-x/gates/104.30.2.sh",
                 "tools/qualification/paxeer-x/program_transfer_authority.py",
                 "tests/programs/test_call_activity.c", "agent/Cargo.toml", "agent/Cargo.lock",
                 "programs/Cargo.toml", "contracts/config/checkpoint-settlement.json", "Makefile"})
BINARIES = ("native_monetary_law", "runtime_unit", "monetary_law", "sandbox_staticlib")
UNIT = [
    "real_guest_mixed_effects_seal_exact_dual_authority_kernel_set",
    "program_authority_recomputes_the_exact_owner_seed_and_source",
    "mixed_principal_and_program_sources_share_one_v2_set_and_kernel_root",
    "owner_frame_and_cumulative_program_spend_boundaries_are_closed",
    "canonical_decoder_rejects_inner_and_trailing_event_malleability",
    "explicit_v2_principal_authority_preserves_kernel_legs_and_legacy_decoding",
    "transfer_set_is_bound_to_invocation_authority_and_exact_order",
    "program_authority_refuses_wrong_seed_program_and_source_typed",
    "program_authority_refuses_callee_frame_and_ungranted_authority_and_keeps_program_spend_grants",
    "program_authority_cumulative_bound_refuses_without_partial_set",
    "program_authority_canonical_encoding_rejects_trailing_and_malleable_bytes",
    "child_transfer_requires_a_reachable_call_graph_edge",
    "disconnected_and_forged_nested_call_staging_is_rejected_as_invariant_one",
    "forged_program_or_principal_is_an_invariant_one_violation",
    "empty_invalid_and_overflowing_sets_are_refused_before_core",
]
GUEST = [
    "real_wasm_cumulative_program_legs_refuse_one_past_grant_atomically",
    "candidate_program_transfer_host_issues_exact_owner_frame_authority",
    "real_wasm_budgeted_preparation_seals_transfer_or_zero_transfer_without_kernel",
    "real_wasm_program_leg_from_underivable_account_is_refused",
    "real_wasm_mixed_principal_and_program_legs_seal_one_canonical_set",
    "real_wasm_program_leg_staged_by_callee_frame_is_refused",
]
NATIVE = [
    "real_mixed_program_authority_kernel_case",
    "mixed_source_kernel_law",
    "program_leg_atomic_rollback_after_first_leg",
    "program_leg_insufficient_balance_refusal_receipt_fees_sequence",
    "program_leg_cumulative_bound_refusal",
    "kernel_primitive_sole_balance_mutation",
]
RUST_LINE = re.compile(r"^test (\S+) \.\.\. (ok|FAILED|ignored)\s*$", re.M)
RUST_SUMMARY = re.compile(r"^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored", re.M)


class Fail(Exception):
    pass


def git(*args):
    return subprocess.run(["git", "--no-optional-locks", "-C", str(ROOT), *args], check=True,
                          capture_output=True, text=True, timeout=60).stdout.strip()


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def load_manifest():
    build_dir = os.environ.get("BUILD_DIR")
    if not build_dir:
        raise Fail("BUILD_DIR is not set; the scoped producer manifest cannot be located")
    path = Path(build_dir) / "paxeer-x-104.30.2" / "artifacts.json"
    if not path.is_file():
        raise Fail("producer manifest missing: run the scoped build stage first")
    data = json.loads(path.read_text(encoding="utf-8"))
    head = git("rev-parse", "HEAD")
    if data.get("task") != TASK or data.get("source_revision") != head:
        raise Fail("producer manifest is not bound to HEAD " + head)
    if data.get("source_dirty") is not False or git("status", "--porcelain=v1"):
        raise Fail("source tree or producer build was dirty")
    for key in ("rustc", "cargo", "cc", "commands", "features", "env"):
        if not data.get(key):
            raise Fail("producer manifest lacks " + key)
    if "layerx-programs-sandbox/host-ffi" not in data["features"]:
        raise Fail("sandbox staticlib was not built with host-ffi")
    inputs = data.get("inputs") or {}
    required_inputs = set(INPUTS)
    required_inputs.update(p for p in git("ls-files", "--", "include", "src").splitlines()
                           if p.endswith((".c", ".h", ".inc")))
    required_inputs.update(p for p in git("ls-files", "--", "programs/crates", "programs/sdk/rust", "agent/crates").splitlines()
                           if p.endswith(".rs") or p.endswith("/Cargo.toml"))
    for rel in required_inputs:
        if rel not in inputs:
            raise Fail("producer manifest omits input " + rel)
    for rel, expected in inputs.items():
        if digest(ROOT / rel) != expected:
            raise Fail("input changed since build: " + rel)
    for key, argv in (("rustc", ["rustc", "-vV"]), ("cargo", ["cargo", "-V"]),
                      ("cc", [data.get("cc_command") or "cc", "--version"])):
        actual = subprocess.run(argv, check=True, capture_output=True, text=True, timeout=60).stdout.strip()
        if actual != data[key]:
            raise Fail("toolchain changed since build: " + key)
    library = data.get("test_library") or {}
    if not library.get("path") or digest(library["path"]) != library.get("sha256"):
        raise Fail("testing kernel library missing or changed since build")
    binaries = data.get("binaries") or {}
    sums = data.get("binary_sha256") or {}
    for name in BINARIES:
        p = binaries.get(name)
        if not p or not Path(p).is_file():
            raise Fail("producer binary missing: " + name)
        if digest(p) != sums.get(name):
            raise Fail("producer binary changed since build: " + name)
    return path, data


def scope():
    if not MAINLINE:
        raise Fail("PAXEER_X_MAINLINE is not set")
    changed = set(filter(None, git("diff", "--name-only", MAINLINE, "HEAD").splitlines()))
    outside = sorted(changed - TOUCHES)
    if outside:
        raise Fail("candidate changes paths outside the transfer-law scope: " + ", ".join(outside))
    return sorted(changed)


def run(argv, log):
    result = subprocess.run(argv, cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=900, env=dict(os.environ, RUST_BACKTRACE="1"))
    output = result.stdout + result.stderr
    log.write("$ " + " ".join(argv) + "\n" + output + "\nexit " + str(result.returncode) + "\n")
    log.flush()
    return result.returncode, output


def rust(binary, names, prefix, module, log):
    argv = [binary, "--test-threads=1"]
    if module:
        argv.append(module)
    code, output = run(argv, log)
    seen = {name: status for name, status in RUST_LINE.findall(output)}
    summary = RUST_SUMMARY.findall(output)
    cases = []
    for n in names:
        status = seen.get(prefix + n)
        if status != "ok":
            raise Fail(binary + ": required case " + prefix + n + " " + (status or "absent"))
        cases.append(prefix + n)
    if code or not summary or any(s[0] != "ok" or s[2] != "0" or s[3] != "0" for s in summary):
        raise Fail(binary + " exited " + str(code) + " or reported failed/ignored cases")
    return cases, sum(int(s[1]) for s in summary)


def native(binary, log):
    code, output = run([binary], log)
    if code:
        raise Fail("native monetary law exited " + str(code))
    for n in NATIVE:
        if not re.search(r"^CASE " + re.escape(n) + r" ok$", output, re.M):
            raise Fail("native case " + n + " absent or failed")
    passed = re.findall(r"^PASSED (\d+)$", output, re.M)
    if not passed or int(passed[-1]) < len(NATIVE):
        raise Fail("native run did not report PASSED for every case")
    return ["native:" + n for n in NATIVE], int(passed[-1])


def main():
    evidence = Path(os.environ.get("PAXEER_X_EVIDENCE_DIR") or os.environ.get("TASK_LOG_DIR") or "")
    if not evidence.is_dir():
        print("program_transfer_authority: no private evidence directory", file=sys.stderr)
        return 2
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    log_path = evidence / ("104.30.2-cases-" + stamp + ".log")
    record = {"task": TASK, "started_at": stamp, "result": "fail"}
    try:
        with open(log_path, "x", encoding="utf-8") as log:
            manifest_path, data = load_manifest()
            record["producer_manifest"] = {"path": str(manifest_path), "sha256": digest(manifest_path)}
            record["scope"] = scope()
            unit, unit_run = rust(data["binaries"]["runtime_unit"], UNIT, "transfer::tests::", "transfer::tests::", log)
            guest, guest_run = rust(data["binaries"]["monetary_law"], GUEST, "", None, log)
            kernel, kernel_run = native(data["binaries"]["native_monetary_law"], log)
        cases = unit + guest + kernel
        record.update(result="pass", cases=cases,
                      executed={"runtime_unit": unit_run, "monetary_law": guest_run,
                                "native_monetary_law": kernel_run},
                      log={"path": str(log_path), "sha256": digest(log_path)})
        print("program_transfer_authority: " + str(len(cases)) + " required cases passed")
        print("PAXEER_X_GATE tests=" + str(len(cases)) + " skipped=0")
        return 0
    except (Fail, subprocess.SubprocessError, OSError, ValueError, KeyError) as error:
        record["error"] = str(error)
        print("program_transfer_authority: FAIL " + str(error), file=sys.stderr)
        return 1
    finally:
        out = evidence / ("104.30.2-transfer-authority-" + stamp + ".json")
        with open(out, "x", encoding="utf-8") as stream:
            json.dump(record, stream, indent=2, sort_keys=True)
        os.chmod(out, 0o600)
        print("evidence: " + str(out))


if __name__ == "__main__":
    sys.exit(main())
