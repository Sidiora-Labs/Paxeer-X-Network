#!/usr/bin/env python3
"""Paxeer X versioned sequence and fee policy gate.

Runs the prebuilt terminal rejection program, which exercises the production
kernel, fee policy and queue disposition codec, and compares the two-class
table it prints against the archived rejection policy (req 26 ac_6 of the
layerx-protocol spec before 0209f71cc). Old-version rows must keep the
historical terminal refusal; new-version rows must follow the archived
policy. Any missing prerequisite fails the gate."""
import json
import os
import re
import subprocess
import sys
import tempfile

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BUILD_DIR = os.environ.get("BUILD_DIR", "build")
PROGRAM = os.path.join(ROOT, BUILD_DIR, "tests", "lxp_test_terminal_rejection")
RESULT_HEADER = os.path.join(ROOT, "include", "layerx", "lxp_result.h")
FEE_HEADER = os.path.join(ROOT, "include", "layerx", "lxp_fee.h")
ARCHIVE = "0209f71cc^:spec/layerx-protocol/spec.kvx"

STAGES = {1: "submission", 2: "ordering", 3: "execution"}
RECEIPT_QUEUE_DISPOSITION, RECEIPT_REFUSAL, RECEIPT_FAILURE, RECEIPT_SUCCESS = 1, 2, 3, 4
ARCHIVED_CLAUSES = (
    "SHALL NOT be assigned a global sequence",
    "SHALL NOT consume the account sequence",
    "SHALL NOT be charged any fee",
    "SHALL consume its account sequence exactly once",
    "SHALL be charged the computed fee up to fee_limit",
    "durable failure receipt carrying its result code and zero module effects",
)
ADMISSION_CASES = ("malformed", "wrong_network", "bad_signature", "sequence_gap",
                   "sequence_reused", "expired", "fee_unpayable", "late_revocation",
                   "late_expiry", "late_balance_loss")
CASE_LINES = (
    ["two-class table case passed", "two-class terminal case passed"]
    + ["two-class admission %s passed" % name for name in ADMISSION_CASES]
    + ["two-class failure case passed", "two-class disposition case passed",
       "terminal rejection tests passed"]
)


def fail(message):
    print("FAIL: " + message, file=sys.stderr)
    sys.exit(1)


def git(*args):
    try:
        return subprocess.run(["git", "-C", ROOT] + list(args), check=True,
                              capture_output=True, text=True).stdout
    except (OSError, subprocess.CalledProcessError) as error:
        fail("git %s unavailable: %s" % (" ".join(args), error))


def archived_policy():
    text = git("show", ARCHIVE)
    block = re.search(r"^\[req\.26\]\n(.*?)(?=^\[)", text, re.S | re.M)
    if block is None:
        fail("archived req.26 missing from " + ARCHIVE)
    clause = re.search(r'^ac_6 = "(.*)"$', block.group(1), re.M)
    if clause is None:
        fail("archived req.26 ac_6 missing")
    for phrase in ARCHIVED_CLAUSES:
        if phrase not in clause.group(1):
            fail("archived req.26 ac_6 lacks: " + phrase)
    return clause.group(1)


def result_codes():
    if not os.path.isfile(RESULT_HEADER):
        fail("missing " + RESULT_HEADER)
    with open(RESULT_HEADER, encoding="utf-8") as handle:
        codes = {int(value): name for name, value in
                 re.findall(r"X\((LXP_\w+),\s*(-?\d+)\)", handle.read())}
    if not codes:
        fail("no result codes in " + RESULT_HEADER)
    return codes


def activation_version():
    if not os.path.isfile(FEE_HEADER):
        fail("missing " + FEE_HEADER)
    with open(FEE_HEADER, encoding="utf-8") as handle:
        found = re.search(r"LXP_FEE_TWO_CLASS_PARAMETER_VERSION\s*=\s*(\d+)", handle.read())
    if found is None:
        fail("activation parameter version missing from " + FEE_HEADER)
    return int(found.group(1))


def check_rows(stdout, codes, activation):
    rows = {}
    for line in stdout.splitlines():
        if not line.startswith("TWO_CLASS_ROW "):
            continue
        fields = dict(item.split("=", 1) for item in line.split()[1:])
        key = (int(fields["version"]), int(fields["stage"]), int(fields["result"]))
        if key in rows:
            fail("duplicate table row %r" % (key,))
        rows[key] = {name: int(value) for name, value in fields.items()}
    versions = (1, activation)
    expected = {(v, s, c) for v in versions for s in STAGES for c in codes}
    if set(rows) != expected:
        fail("table is not exhaustive: %d rows, %d expected" % (len(rows), len(expected)))
    for (version, stage, code), row in sorted(rows.items()):
        name = codes[code]
        where = "version %d %s %s" % (version, STAGES[stage], name)
        fatal = name.startswith("LXP_FATAL_")
        if fatal or (stage == 3 and name == "LXP_ERR_IDEMPOTENT_REPLAY"):
            if row["canonical"] != 0:
                fail(where + " must be non-canonical")
            continue
        if stage == 1:
            if row["canonical"] != 1 or (row["actor"], row["global"], row["fee"],
                                         row["effects"], row["receipt"]) != (0, 0, 0, 0, 0):
                fail(where + " consumes sequence or fee before admission")
        elif stage == 2:
            if row["canonical"] == 0:
                continue
            if version >= activation:
                if (row["actor"], row["global"], row["fee"], row["effects"],
                        row["receipt"]) != (0, 0, 0, 0, RECEIPT_QUEUE_DISPOSITION):
                    fail(where + " violates archived admission refusal policy")
            elif (row["actor"], row["global"], row["fee"], row["effects"],
                  row["receipt"]) != (0, 1, 0, 0, RECEIPT_REFUSAL):
                fail(where + " changed the historical terminal refusal")
        else:
            success = code == 0
            want = (1, 1, 1, 1 if success else 0,
                    RECEIPT_SUCCESS if success else RECEIPT_FAILURE)
            if row["canonical"] != 1 or (row["actor"], row["global"], row["fee"],
                                         row["effects"], row["receipt"]) != want:
                fail(where + " violates archived ordered execution policy")
    for code, name in codes.items():
        old, new = rows[(1, 2, code)], rows[(activation, 2, code)]
        if old["canonical"] != new["canonical"] and name != "LXP_ERR_IDEMPOTENT_REPLAY":
            fail("ordering eligibility differs across versions for " + name)
    return len(rows)


def main():
    revision = git("rev-parse", "HEAD").strip()
    if not revision:
        fail("revision unavailable")
    policy = archived_policy()
    codes = result_codes()
    activation = activation_version()
    if activation <= 1:
        fail("activation parameter version must exceed the historical version 1")
    if not os.path.isfile(PROGRAM) or not os.access(PROGRAM, os.X_OK):
        fail("missing prebuilt program " + PROGRAM)
    command = [PROGRAM]
    evidence = tempfile.mkdtemp(prefix="paxeer-x-rejection-policy-")
    os.chmod(evidence, 0o700)
    completed = subprocess.run(command, cwd=ROOT, capture_output=True, text=True,
                               timeout=1500)
    with open(os.path.join(evidence, "stdout.log"), "w", encoding="utf-8") as handle:
        handle.write(completed.stdout)
    with open(os.path.join(evidence, "stderr.log"), "w", encoding="utf-8") as handle:
        handle.write(completed.stderr)
    summary = {"revision": revision, "command": command,
               "exit_code": completed.returncode, "archived_policy": policy}
    print("revision: " + revision)
    print("command: " + " ".join(command))
    print("exit code: %d" % completed.returncode)
    print("evidence: " + evidence)
    if completed.returncode != 0:
        sys.stderr.write(completed.stderr[-4000:])
        fail("program exited %d" % completed.returncode)
    for line in CASE_LINES:
        if line not in completed.stdout.splitlines():
            fail("missing case output: " + line)
    summary["table_rows"] = check_rows(completed.stdout, codes, activation)
    with open(os.path.join(evidence, "summary.json"), "w", encoding="utf-8") as handle:
        json.dump(summary, handle, indent=2)
    print("table rows: %d" % summary["table_rows"])
    print("paxeer x rejection policy passed")


if __name__ == "__main__":
    main()
