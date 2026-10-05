#!/usr/bin/env bash
set -euo pipefail
umask 077
if (($#)); then
    printf 'performance gate accepts no arguments\n' >&2
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


def refuse(reason):
    raise SystemExit('performance gate refused: ' + reason)


spec = importlib.util.spec_from_file_location('human_production_build', root / 'tools/paxeer-x/builders/104.13.1.py')
if spec is None or spec.loader is None:
    refuse('production build producer unavailable')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
filename = os.environ.get('HUMAN_E2E_SETTINGS_ARTIFACTS')
if not filename:
    refuse('separately completed source-bound Human production build required (tools/paxeer-x/builders/104.13.1.py)')
manifest = builder.verify_artifacts(filename)
environment = builder.production_environment(builder.output_directory())
if manifest['origin'] != environment['LAYERX_HUMAN_WEB_ORIGIN'] or manifest['service'] != environment['LAYERX_HUMAN_SERVICE_URL']:
    refuse('production build endpoint binding differs')
suite = web / 'e2e/perf.spec.ts'
expected = re.findall(r'^test\("([^"\n]+)"', suite.read_text(), re.M)
if not expected:
    refuse('retained performance corpus unavailable')
command = ['node', 'node_modules/@playwright/test/cli.js', 'test', '--config', 'playwright.perf.config.ts',
           '--project=desktop-shell', '--workers=1', '--retries=0']
process = subprocess.Popen(command, cwd=web, env=environment, stdin=subprocess.DEVNULL,
                           stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
lines = []
for line in process.stdout:
    sys.stdout.write(line)
    sys.stdout.flush()
    lines.append(line)
code = process.wait()
if code:
    raise SystemExit(code)
if builder.source_binding() != manifest['source']:
    refuse('source changed during qualification')
output = re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', ''.join(lines)).replace('\r', '\n')
passed = re.findall(r'^\s*(\d+) passed \(', output, re.M)
if len(passed) != 1 or int(passed[0]) != len(expected):
    refuse('incomplete production performance coverage')
if re.search(r'\b[1-9]\d* (?:failed|flaky|skipped|did not run)\b', output):
    refuse('unsuccessful, retried or skipped performance case')
for title in expected:
    if not re.search(r'\[desktop-shell\].*perf\.spec\.ts.*' + re.escape(title), output):
        refuse('missing performance case: ' + title)
print('PAXEER_X_GATE tests=' + passed[0] + ' skipped=0')
PY_GATE
