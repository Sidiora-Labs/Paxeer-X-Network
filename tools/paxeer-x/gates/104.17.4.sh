#!/usr/bin/env bash
# paxeer-x-services: private-profile
set -euo pipefail
exec python3 - <<'PY'
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import urllib.parse

root = Path.cwd()
evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR'])
tests = 0

def execute(argv, name, timeout=180):
    path = evidence / ('104.17.4-' + name + '.log')
    with path.open('w') as log:
        result = subprocess.run(argv, cwd=root, stdout=log, stderr=subprocess.STDOUT, timeout=timeout)
    print('exit=' + str(result.returncode) + ' log=' + str(path), flush=True)
    if result.returncode:
        sys.exit(result.returncode)
    return path.read_text()

for name in ('hosted-boundary.sh', 'run-load.sh'):
    execute(['sh', '-n', str(root / 'platform/hosted/gateway/tests' / name)], 'syntax-' + name)
    tests += 1

for name in ('LAYERX_GATEWAY_LIB_TEST_BINARY', 'LAYERX_GATEWAY_TEST_BINARY'):
    binary = os.environ.get(name)
    if not binary or not Path(binary).is_absolute() or not Path(binary).is_file() or not os.access(binary, os.X_OK):
        print('missing prerequisite: genuine prebuilt ' + name, file=sys.stderr)
        sys.exit(78)
    inventory = execute([binary, '--list'], name.lower() + '-inventory')
    declared = set(re.findall(r'^(.+): test$', inventory, re.M))
    if not declared:
        raise RuntimeError('required real Rust test inventory is empty: ' + name)
    output = execute([binary, '--nocapture', '--test-threads=1'], name.lower())
    results = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
    if len(results) != 1 or int(results[0][0]) != len(declared) or results[0][1:] != ('0', '0'):
        raise RuntimeError('real Rust test corpus omitted a required case: ' + name)
    tests += int(results[0][0])

output = execute(['python3', 'platform/hosted/gateway/tests/key-store.py'], 'key-store')
records = re.findall(r'^GATEWAY_KEY_STORE_RESULT (.+)$', output, re.M)
if len(records) != 1:
    raise RuntimeError('real Redis key-store corpus evidence is missing')
record = json.loads(records[0])
if not isinstance(record.get('tests'), int) or record['tests'] <= 0 or record.get('skipped') != 0:
    raise RuntimeError('real Redis key-store corpus is empty or skipped')
tests += record['tests']

boundary = root / 'platform/hosted/gateway/tests/hosted-boundary.sh'
load = root / 'platform/hosted/gateway/tests/run-load.sh'
required = []
for script in (boundary, load):
    contract = re.findall(r'^: "\$\{([A-Z0-9_]+):\?', script.read_text(), re.M)
    if not contract:
        raise RuntimeError('actual hosted fixture prerequisite contract is missing: ' + script.name)
    required.extend(contract)
required = sorted(set(required))
missing = [name for name in required if not os.environ.get(name)]
for tool in ('curl', 'jq', 'k6'):
    if not shutil.which(tool):
        missing.append('executable ' + tool)
verifier = os.environ.get('LAYERX_RECEIPT_VERIFY_BIN')
if verifier and (not Path(verifier).is_file() or not os.access(verifier, os.X_OK)):
    missing.append('executable LAYERX_RECEIPT_VERIFY_BIN')
if missing:
    print('missing prerequisite: genuine private hosted gateway fixture: ' + ', '.join(missing), file=sys.stderr)
    sys.exit(78)
origin = urllib.parse.urlsplit(os.environ['LAYERX_GATEWAY_URL'])
if (origin.scheme != 'https' or not origin.hostname or origin.username is not None or origin.password is not None
    or origin.path not in ('', '/') or origin.query or origin.fragment
    or not (origin.hostname.endswith('.svc.cluster.local') or origin.hostname in ('localhost', '127.0.0.1', '::1'))):
    raise RuntimeError('hosted qualification requires genuine private HTTPS gateway origin')
for name in required:
    if name.endswith('_FILE') or name == 'LAYERX_GATEWAY_ACTIVITY_CORPUS':
        target = Path(os.environ[name])
        if not target.is_file() or target.is_symlink() or not os.access(target, os.R_OK):
            print('missing prerequisite: genuine hosted artifact ' + name, file=sys.stderr)
            sys.exit(78)
execute(['sh', str(boundary)], 'hosted-boundary', timeout=900)
tests += 1
execute(['sh', str(load)], 'hosted-load', timeout=1500)
tests += 1
print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=0', flush=True)
PY
