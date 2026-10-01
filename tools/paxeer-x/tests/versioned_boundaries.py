#!/usr/bin/env python3
"""Versioned sequence, refusal and expiry boundary contract for the native
kernel, checked against the vectors emitted by the real terminal rejection
test program (tests/protocol/lxp_test_terminal_rejection.c).

The table below is the published contract for every accepted protocol
version. Unknown versions are refused. A behaviour change to any row needs a
new protocol version with its own rows; historical rows stay as written."""
import os
import re
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
BUILD_DIR = os.environ.get("BUILD_DIR", "build")
PROGRAM = os.path.join(ROOT, BUILD_DIR, "tests", "lxp_test_terminal_rejection")
RESULT_HEADER = os.path.join(ROOT, "include", "layerx", "lxp_result.h")

ACCEPTED_VERSIONS = (1, 2, 3)
UNKNOWN_VERSIONS = (0, 4)

# Admission, ordered execution, durable terminal refusal and replay, per
# accepted version. "global"/"actor" are sequence deltas, "fee" the charged
# units, "events"/"effects" the emitted counts, "receipt" whether a canonical
# receipt is stored, "idempotency" what a duplicate of the same key returns.
TRANSITIONS = {
    version: {
        "admission_refusal": {
            "where": "lxp_admit_activity before ordering",
            "global": 0, "actor": 0, "fee": 0, "effects": 0, "events": 0,
            "receipt": "none", "idempotency": "key not recorded",
        },
        "ordered_success": {
            "where": "lxp_kernel_execute_activity",
            "global": 1, "actor": 1, "fee": "metered, <= fee_limit",
            "effects": "module effects", "events": "module events",
            "receipt": "stored under the activity idempotency key",
            "idempotency": "IDEMPOTENT_REPLAY returning the first receipt",
        },
        "ordered_execution_failure": {
            "where": "lxp_kernel_execute_activity with a module refusal after fee admission",
            "global": 1, "actor": "per fee policy", "fee": "per fee policy",
            "effects": 0, "events": 0,
            "receipt": "stored with the refusal result code",
            "idempotency": "IDEMPOTENT_REPLAY returning the first receipt",
        },
        "terminal_refusal": {
            "where": "lxp_kernel_terminal_rejection / lxp_kernel_prepare_terminal_rejection",
            "eligible_domains": "CODEC ENVELOPE AUTHORITY SEQUENCING LEDGER ARITHMETIC METERING MODULE",
            "global": 1, "actor": 0, "fee": 0, "effects": 0, "events": 0,
            "receipt": "stored with the refusal result code, protocol_version = activity version",
            "state_root": ("v3 binds the receipt into the committed state root after binding; "
                           "v1/v2 compute the root before receipt build"),
            "idempotency": "IDEMPOTENT_REPLAY returning the first refusal receipt",
        },
        "fee_only_failure": {
            "where": "terminal refusal of a METERING-domain result (LXP_ERR_FEE_LIMIT)",
            "global": 1, "actor": 0, "fee": 0, "effects": 0, "events": 0,
            "receipt": "stored with LXP_ERR_FEE_LIMIT",
            "idempotency": "IDEMPOTENT_REPLAY returning the first refusal receipt",
        },
    }
    for version in ACCEPTED_VERSIONS
}

# Expiry equality per accepted version. None of the comparators takes a
# protocol version input, so every accepted version has the same semantics.
EXPIRY = {
    version: {
        "activity": "inclusive: batch_ts > not_after -> EXPIRED; batch_ts < not_before -> NOT_YET_VALID",
        "grant": ("exclusive record end: batch_ts >= not_after -> AUTH_EXPIRED; an owner grant "
                  "stores activity not_after + 1, so it is live through the activity bound"),
        "send": "inclusive: batch_ts > expires_at -> EXPIRED",
        "receive": "inclusive: batch_ts > grant.expiration -> GRANT_EXPIRED",
    }
    for version in ACCEPTED_VERSIONS
}

# General clause versus operation-specific clauses, resolved without changing
# any check. Every entry must be resolved for a release verdict.
CONFLICTS = [
    {
        "id": "expiry-grant-exclusive-vs-operation-inclusive",
        "resolved": True,
        "resolution": ("Grant records keep an exclusive end and activity/SEND/RECEIVE keep an "
                       "inclusive bound; the owner grant derivation stores not_after + 1, so "
                       "equality at the activity bound is admitted by both (vectors "
                       "expiry.grant.owner_equal and expiry.activity.equal)."),
    },
    {
        "id": "terminal-refusal-policy-v1-v3",
        "resolved": True,
        "resolution": ("Versions 1-3 keep the current eligible-domain classification: global +1, "
                       "actor +0, fee 0, zero effects, replay returns the refusal receipt. A "
                       "different refusal policy requires a new protocol version with its own rows."),
    },
    {
        "id": "unknown-version",
        "resolved": True,
        "resolution": ("Versions outside 1-3 are refused at admission with "
                       "LXP_ERR_VERSION_UNSUPPORTED, and a terminal refusal for them is "
                       "LXP_ERR_NON_CANONICAL with no sequence, receipt or state change."),
    },
]


def result_codes():
    codes = {}
    with open(RESULT_HEADER, encoding="utf-8") as handle:
        for name, value in re.findall(r"X\((LXP_[A-Z0-9_]+),\s*(-?\d+)\)", handle.read()):
            codes[name[4:]] = int(value)
    return codes


def expected_vectors(code):
    vectors = {f"version.supported.{v}": int(v in ACCEPTED_VERSIONS) for v in range(5)}
    vectors.update({
        "expiry.activity.before": code["OK"],
        "expiry.activity.equal": code["OK"],
        "expiry.activity.after": code["ERR_EXPIRED"],
        "expiry.activity.not_before_equal": code["OK"],
        "expiry.activity.not_before_below": code["ERR_NOT_YET_VALID"],
        "expiry.grant.owner_not_after_stored": 101,
        "expiry.grant.owner_unbounded": code["ERR_MALFORMED_ENVELOPE"],
        "expiry.grant.owner_before": code["OK"],
        "expiry.grant.owner_equal": code["OK"],
        "expiry.grant.owner_after": code["ERR_AUTH_EXPIRED"],
        "expiry.grant.record_before": code["OK"],
        "expiry.grant.record_equal": code["ERR_AUTH_EXPIRED"],
        "expiry.grant.record_after": code["ERR_AUTH_EXPIRED"],
        "expiry.send.before": code["ERR_CONTEXT_MISMATCH"],
        "expiry.send.equal": code["ERR_CONTEXT_MISMATCH"],
        "expiry.send.after": code["ERR_EXPIRED"],
        "expiry.receive.before": code["ERR_GRANT_SCOPE_VIOLATION"],
        "expiry.receive.equal": code["ERR_GRANT_SCOPE_VIOLATION"],
        "expiry.receive.after": code["ERR_GRANT_EXPIRED"],
        "expiry.receive.balance_unchanged": 1,
    })
    refusals = {"authority": "ERR_IDENTITY_FROZEN", "module": "ERR_INSUFFICIENT_BALANCE",
                "fee": "ERR_FEE_LIMIT", "expired": "ERR_EXPIRED"}
    for version in ACCEPTED_VERSIONS:
        row = TRANSITIONS[version]["terminal_refusal"]
        for label, refusal in refusals.items():
            prefix = f"terminal.v{version}.{label}."
            vectors.update({prefix + key: value for key, value in {
                "envelope": code["OK"],
                "status": code["OK"],
                "global_delta": row["global"],
                "actor_delta": row["actor"],
                "actor_balance": 1000, "recipient_balance": 0, "treasury_balance": 0,
                "root_unchanged": 0,
                "receipt_result": code[refusal],
                "receipt_version": version,
                "receipt_sequence_offset": 0,
                "receipt_fee": row["fee"],
                "receipt_effects": row["effects"],
                "receipt_root_is_kernel_root": 1,
                "restart_status": code["OK"],
                "restart_receipt_identical": 1,
                "restart_root_identical": 1,
                "duplicate_status": code["ERR_IDEMPOTENT_REPLAY"],
                "duplicate_result": code[refusal],
                "duplicate_sequence_offset": 0,
                "duplicate_global_delta": row["global"],
                "duplicate_actor_delta": row["actor"],
            }.items()})
    for version in UNKNOWN_VERSIONS:
        prefix = f"terminal.v{version}.authority."
        vectors.update({prefix + key: value for key, value in {
            "envelope": code["ERR_VERSION_UNSUPPORTED"],
            "status": code["ERR_NON_CANONICAL"],
            "global_delta": 0, "actor_delta": 0,
            "actor_balance": 1000, "recipient_balance": 0, "treasury_balance": 0,
            "root_unchanged": 1,
        }.items()})
    return vectors


def main():
    unresolved = [c["id"] for c in CONFLICTS if not c["resolved"]]
    if unresolved:
        print("unresolved boundary conflicts block release: " + ", ".join(unresolved))
        return 1
    if set(EXPIRY) != set(ACCEPTED_VERSIONS) or set(TRANSITIONS) != set(ACCEPTED_VERSIONS):
        print("contract table does not cover every accepted version")
        return 1
    if not os.access(PROGRAM, os.X_OK):
        print(f"missing test program {PROGRAM}; build it with make {BUILD_DIR}/tests/lxp_test_terminal_rejection")
        return 1
    expected = expected_vectors(result_codes())
    run = subprocess.run([PROGRAM], cwd=ROOT, capture_output=True, text=True, timeout=540)
    sys.stdout.write(run.stdout)
    sys.stderr.write(run.stderr)
    observed = {}
    for name, value in re.findall(r"^VECTOR ([a-z0-9_.]+)=(-?\d+)$", run.stdout, re.M):
        if name in observed:
            print(f"duplicate vector {name}")
            return 1
        observed[name] = int(value)
    failures = 0
    for name, value in expected.items():
        if observed.get(name) != value:
            failures += 1
            print(f"FAIL {name}: expected {value}, observed {observed.get(name)}")
    for name in sorted(set(observed) - set(expected)):
        failures += 1
        print(f"FAIL {name}: observed {observed[name]} has no contract row")
    if run.returncode != 0 or "terminal rejection tests passed" not in run.stdout:
        failures += 1
        print(f"FAIL program exit {run.returncode}")
    print(f"versioned boundary vectors: {len(expected) - failures if failures <= len(expected) else 0}"
          f"/{len(expected)} matched, {failures} failures")
    print(f"VECTORS tests={len(expected)} failures={failures}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
