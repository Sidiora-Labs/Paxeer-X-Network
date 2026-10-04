#!/usr/bin/env bash
set -euo pipefail
umask 077
if (($#)); then
    printf 'agents gate accepts no arguments\n' >&2
    exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" <<'PY_GATE'
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys

root = Path(sys.argv[1])
web = root / 'human/apps/web'
spec = importlib.util.spec_from_file_location('human_production_build', root / 'tools/paxeer-x/builders/104.13.1.py')
if spec is None or spec.loader is None:
    raise SystemExit('agents gate refused: production build producer unavailable')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
filename = os.environ.get('HUMAN_E2E_SETTINGS_ARTIFACTS')
if not filename:
    raise SystemExit('agents gate refused: separately completed source-bound Human production build required')
manifest = builder.verify_artifacts(filename)
environment = builder.production_environment(builder.output_directory())
if manifest['origin'] != environment['LAYERX_HUMAN_WEB_ORIGIN'] or manifest['service'] != environment['LAYERX_HUMAN_SERVICE_URL']:
    raise SystemExit('agents gate refused: production build endpoint binding differs')
for name in ('HUMAN_E2E_AGENT_MONTHLY_LIMIT', 'HUMAN_E2E_AGENT_FEE_PER_ACTION',
             'HUMAN_E2E_AGENT_FEE_TOTAL', 'HUMAN_E2E_AGENT_FEE_PER_PERIOD'):
    value = os.environ.get(name)
    if not value:
        raise SystemExit('agents gate refused: explicit real agent limits required: ' + name)
    environment[name] = value
browser = web / 'e2e/browser/agents.spec.ts'
if not browser.is_file():
    raise SystemExit('agents gate refused: production lifecycle cases unavailable')
lines = []
for command in (["node", "--test", "e2e/agents.spec.ts"],
                ["node", "node_modules/@playwright/test/cli.js", "test", "e2e/browser/agents.spec.ts",
                 "--project=mobile-shell", "--project=desktop-shell", "--workers=1", "--retries=0"]):
    process = subprocess.Popen(command, cwd=web, env=environment, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    for line in process.stdout:
        sys.stdout.write(line)
        sys.stdout.flush()
        lines.append(line)
    code = process.wait()
    if code:
        raise SystemExit(code)
if builder.source_binding() != manifest['source']:
    raise SystemExit('agents gate refused: source changed during qualification')
output = re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', ''.join(lines)).replace('\r', '\n')
expected = re.findall(r'^test\("([^"\n]+)"', (web / 'e2e/agents.spec.ts').read_text(), re.M)
executed = re.findall(r'^# Subtest: (.+)$', output, re.M)
if not expected or executed != expected:
    raise SystemExit('agents gate refused: incomplete retained Node corpus')
counts = {}
for key in ('tests', 'pass', 'fail', 'cancelled', 'skipped', 'todo'):
    values = re.findall(r'^# ' + key + r' (\d+)$', output, re.M)
    if len(values) != 1:
        raise SystemExit('agents gate refused: ambiguous Node count ' + key)
    counts[key] = int(values[0])
if counts['tests'] != len(expected) or counts['pass'] != len(expected) or any(counts[key] for key in ('fail', 'cancelled', 'skipped', 'todo')):
    raise SystemExit('agents gate refused: retained Node coverage failed or skipped')
browser_expected = re.findall(r'^test\("(@agents[^"\n]+)"', browser.read_text(), re.M)
passed = re.findall(r'^\s*(\d+) passed \(', output, re.M)
if not browser_expected or len(passed) != 1 or int(passed[0]) != len(browser_expected) * 2:
    raise SystemExit('agents gate refused: incomplete production lifecycle coverage for both shells')
if re.search(r'\b[1-9]\d* (?:failed|flaky|skipped|did not run)\b', output):
    raise SystemExit('agents gate refused: unsuccessful, retried or skipped browser case')
for shell in ('mobile-shell', 'desktop-shell'):
    for title in browser_expected:
        if not re.search(r'\[' + shell + r'\].*agents\.spec\.ts.*' + re.escape(title), output):
            raise SystemExit('agents gate refused: missing lifecycle case ' + shell + ': ' + title)
print('PAXEER_X_GATE tests=' + str(counts['tests'] + int(passed[0])) + ' skipped=0')
PY_GATE
