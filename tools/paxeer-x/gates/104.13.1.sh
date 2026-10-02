#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
test "$#" -eq 0 || { printf 'settings gate accepts no arguments\n' >&2; exit 2; }
exec python3 - "$root" <<'PY_GATE'
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys

root = Path(sys.argv[1])
spec = importlib.util.spec_from_file_location("settings_build", root / "tools/paxeer-x/builders/104.13.1.py")
if spec is None or spec.loader is None:
    raise SystemExit("settings gate refused: build producer is unavailable")
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
filename = os.environ.get("HUMAN_E2E_SETTINGS_ARTIFACTS")
if not filename:
    raise SystemExit("settings gate refused: first run the separately authorized tools/paxeer-x/builders/104.13.1.py build producer and supply HUMAN_E2E_SETTINGS_ARTIFACTS")
manifest = builder.verify_artifacts(filename)
output_directory = builder.output_directory()
environment = builder.production_environment(output_directory)
if (manifest["origin"] != environment["LAYERX_HUMAN_WEB_ORIGIN"]
        or manifest["service"] != environment["LAYERX_HUMAN_SERVICE_URL"]):
    raise SystemExit("settings gate refused: production environment differs from the build")
lines = []
for command in (["node", "--test", "e2e/settings.spec.ts"],
                ["node", "node_modules/@playwright/test/cli.js", "test", "--grep", "@settings"]):
    process = subprocess.Popen(
        command, cwd=root / "human/apps/web", env=environment,
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True,
    )
    for line in process.stdout:
        sys.stdout.write(line)
        sys.stdout.flush()
        lines.append(line)
    code = process.wait()
    if code:
        raise SystemExit(code)
if builder.source_binding() != manifest["source"]:
    raise SystemExit("settings gate refused: source changed during qualification")
output = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", "".join(lines)).replace("\r", "\n")

def refuse(reason):
    raise SystemExit("settings gate refused: " + reason)

source = (root / "human/apps/web/e2e/settings.spec.ts").read_text()
expected = re.findall(r'^test\("([^"\n]+)"', source, re.M)
executed = re.findall(r"^# Subtest: (.+)$", output, re.M)
if not expected or executed != expected:
    refuse("the complete retained settings Node corpus did not execute")
counts = {}
for key in ("tests", "pass", "fail", "cancelled", "skipped", "todo"):
    values = re.findall(r"^# " + key + r" (\d+)$", output, re.M)
    if len(values) != 1:
        refuse("missing or ambiguous Node " + key + " count")
    counts[key] = int(values[0])
if counts["tests"] != len(expected) or counts["pass"] != len(expected):
    refuse("incomplete Node results")
if any(counts[key] for key in ("fail", "cancelled", "skipped", "todo")):
    refuse("Node settings contains unsuccessful or skipped cases")
passed = re.findall(r"^\s*(\d+) passed \(", output, re.M)
if len(passed) != 1 or int(passed[0]) < 2:
    refuse("missing real browser results for both shells")
if re.search(r"\b[1-9]\d* (?:failed|flaky|skipped|did not run)\b", output):
    refuse("browser settings contains unsuccessful, retried or skipped cases")
browser_source = (root / "human/apps/web/e2e/browser/settings.spec.ts").read_text()
browser_expected = re.findall(r'^test\("(@settings[^"\n]+)"', browser_source, re.M)
if not browser_expected or int(passed[0]) != len(browser_expected) * 2:
    refuse("the complete retained browser settings corpus did not execute in both shells")
for shell in ("mobile-shell", "desktop-shell"):
    if not re.search(r"\[" + shell + r"\].*settings\.spec\.ts.*@settings", output):
        refuse("missing browser execution for " + shell)
    for title in browser_expected:
        if not re.search(r"\[" + shell + r"\].*settings\.spec\.ts.*" + re.escape(title), output):
            refuse("missing browser case for " + shell + ": " + title)
print("PAXEER_X_GATE tests=" + str(counts["tests"] + int(passed[0])) + " skipped=0")
PY_GATE
