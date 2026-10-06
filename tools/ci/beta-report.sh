#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'USAGE'
usage: tools/ci/beta-report.sh [--ledger PATH] [--check]

Reads the Paxeer X Network mainnet-beta evidence ledger
(spec/layerx-beta/qualification.kvx by default) and nothing else.

  --check        validate the ledger shape only and print a one-line summary;
                 needs no CI inputs, network or build output
  --ledger PATH  ledger to read instead of the default

Without --check the go/no-go report is rendered to stdout from the gate
records alone. Exit status: 0 valid, 1 invalid ledger, 2 usage or
environment error.
USAGE
}

root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
ledger=""
mode=render
while [ "$#" -gt 0 ]; do
    case $1 in
    --ledger)
        [ "$#" -ge 2 ] || { usage >&2; exit 2; }
        ledger=$2
        shift 2
        ;;
    --check)
        mode=check
        shift
        ;;
    -h | --help)
        usage
        exit 0
        ;;
    *)
        usage >&2
        exit 2
        ;;
    esac
done
ledger=${ledger:-$root/spec/layerx-beta/qualification.kvx}
command -v python3 >/dev/null 2>&1 || { echo "beta-report: python3 is required" >&2; exit 2; }
[ -f "$ledger" ] || { echo "beta-report: ledger not found: $ledger" >&2; exit 2; }

exec python3 - "$mode" "$ledger" <<'PY'
import json
import re
import sys
from datetime import datetime

mode, path = sys.argv[1], sys.argv[2]

KEYS = {
    "gate": ["task", "reqs", "revision", "command", "environment", "started_at", "outcome", "evidence", "note"],
    "observation": ["task", "file", "symbol", "observed", "assumption", "severity"],
}
OUTCOMES = ("pass", "fail", "blocked")
SEVERITIES = ("blocker", "suspect", "assumption", "note")
HEADER = re.compile(r"^\[(gate|observation)\.(.+)\.([1-9][0-9]*)\]$")
TASK = re.compile(r"^[0-9A-Za-z]+(\.[0-9A-Za-z]+)*$")
ASSIGN = re.compile(r"^([a-z_][a-z0-9_]*) = (.+)$")
HEX40 = re.compile(r"^[0-9a-f]{40}$")
STAMP = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
IPV4 = re.compile(r"(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])")

errors = []
records = []
current = None

def fail(lineno, message):
    errors.append(f"{path}:{lineno}: {message}")

with open(path, encoding="utf-8") as handle:
    lines = handle.read().split("\n")

for lineno, raw in enumerate(lines, 1):
    line = raw.rstrip()
    if line != raw:
        fail(lineno, "trailing whitespace")
    if not line or line.startswith("#"):
        continue
    if line.startswith("["):
        match = HEADER.match(line)
        if not match or not TASK.match(match.group(2)):
            fail(lineno, f"record header {line!r} is not [gate.<task>.<n>] or [observation.<task>.<n>]")
            current = None
            continue
        name = line[1:-1]
        if any(r["name"] == name for r in records):
            fail(lineno, f"duplicate record {name}")
        current = {"name": name, "kind": match.group(1), "task": match.group(2), "line": lineno, "values": {}}
        records.append(current)
        continue
    match = ASSIGN.match(line)
    if not match:
        fail(lineno, f"line is not a comment, record header or key = value: {line!r}")
        continue
    if current is None:
        fail(lineno, "key outside a record")
        continue
    key, text = match.groups()
    if key in current["values"]:
        fail(lineno, f"{current['name']}: duplicate key {key}")
        continue
    try:
        value = json.loads(text)
    except ValueError:
        fail(lineno, f"{current['name']}.{key}: value is not a quoted string or string list")
        continue
    if isinstance(value, list):
        if not value or not all(isinstance(v, str) and v for v in value):
            fail(lineno, f"{current['name']}.{key}: list must hold one or more non-empty strings")
            continue
    elif not isinstance(value, str) or not value:
        fail(lineno, f"{current['name']}.{key}: value must be a non-empty string")
        continue
    for item in value if isinstance(value, list) else [value]:
        if IPV4.search(item):
            fail(lineno, f"{current['name']}.{key}: carries a host address; the ledger is public")
    current["values"][key] = value

for record in records:
    name, kind, values, lineno = record["name"], record["kind"], record["values"], record["line"]
    expected = KEYS[kind]
    missing = [k for k in expected if k not in values]
    extra = [k for k in values if k not in expected]
    if missing:
        fail(lineno, f"{name}: missing keys {', '.join(missing)}")
    if extra:
        fail(lineno, f"{name}: keys not allowed on a {kind} record: {', '.join(extra)}")
    for key in expected:
        if key in values and isinstance(values[key], list) != (key == "reqs"):
            fail(lineno, f"{name}.{key}: wrong value type")
    if values.get("task") is not None and values.get("task") != record["task"]:
        fail(lineno, f"{name}: task key {values.get('task')!r} does not match the record name")
    if kind == "observation":
        if "severity" in values and values["severity"] not in SEVERITIES:
            fail(lineno, f"{name}.severity: {values['severity']!r} is not one of {', '.join(SEVERITIES)}")
        continue
    if isinstance(values.get("revision"), str) and not HEX40.match(values["revision"]):
        fail(lineno, f"{name}.revision: not a 40-hex commit identifier")
    if isinstance(values.get("outcome"), str) and values["outcome"] not in OUTCOMES:
        fail(lineno, f"{name}.outcome: {values['outcome']!r} is not one of {', '.join(OUTCOMES)}")
    stamp = values.get("started_at")
    if isinstance(stamp, str):
        try:
            if not STAMP.match(stamp):
                raise ValueError
            datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ")
        except ValueError:
            fail(lineno, f"{name}.started_at: not a UTC timestamp YYYY-MM-DDTHH:MM:SSZ")

if errors:
    for line in errors:
        print(f"beta-report: {line}", file=sys.stderr)
    print(f"beta-report: ledger invalid ({len(errors)} errors)", file=sys.stderr)
    sys.exit(1)

gates = [r for r in records if r["kind"] == "gate"]
observations = [r for r in records if r["kind"] == "observation"]
go_records = [
    g for g in gates
    if g["values"]["outcome"] == "pass" and g["values"]["note"].startswith("owner go decision:")
]
candidate = go_records[-1]["values"]["revision"] if go_records else None
on_candidate = [g for g in gates if g["values"]["revision"] == candidate]
blocking = [g for g in on_candidate if g["values"]["outcome"] != "pass"]
decision = "go" if candidate and not blocking else "no-go"

count = lambda items, key, vocab: ", ".join(f"{v} {sum(1 for i in items if i['values'][key] == v)}" for v in vocab)
summary = (
    f"beta-report: {decision}; {len(gates)} gate records ({count(gates, 'outcome', OUTCOMES)}); "
    f"{len(observations)} observations ({count(observations, 'severity', SEVERITIES)}); "
    f"candidate {candidate or 'undeclared'}"
)

if mode == "check":
    print(summary)
    sys.exit(0)

cell = lambda text: text.replace("|", "\\|").replace("\n", " ")
out = [
    "# Paxeer X Network mainnet-beta go/no-go report",
    "",
    f"Rendered by `tools/ci/beta-report.sh` from `spec/layerx-beta/qualification.kvx` gate records only.",
    "",
    f"Decision: **{decision}**",
    "",
    f"Release candidate: {candidate or 'undeclared (no owner go decision record)'}",
    "",
    "## Gate records",
    "",
]
if gates:
    out += ["| Record | Revision | Command | Outcome | Evidence |", "| --- | --- | --- | --- | --- |"]
    out += [
        f"| {g['name']} | {g['values']['revision'][:12]} | {cell(g['values']['command'])} | {g['values']['outcome']} | {cell(g['values']['evidence'])} |"
        for g in gates
    ]
else:
    out.append("No gate record exists; every surface stays at rung source_present.")
out += ["", "## Blocking records on the candidate", ""]
out += [f"- {g['name']}: {g['values']['outcome']}: {cell(g['values']['note'])}" for g in blocking] or ["None."]
out += ["", "## Observations", "", count(observations, "severity", SEVERITIES) + "."]
print("\n".join(out))
print(summary, file=sys.stderr)
PY
