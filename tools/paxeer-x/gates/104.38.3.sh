#!/usr/bin/env bash
# paxeer-x-services: human human-kms
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" <<'PY'
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys

root = Path(sys.argv[1])
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('human_boundary', root / 'tools/qualification/paxeer-x/human-api-boundary.py')
if spec is None or spec.loader is None:
    raise RuntimeError('actual production Human boundary owner is missing')
owner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(owner)
evidence = owner.private(os.environ['PAXEER_X_EVIDENCE_DIR'], directory=True)
manifest_path = os.environ.get('PAXEER_X_HUMAN_COMPONENT_ARTIFACT_MANIFEST')
if not manifest_path or not Path(manifest_path).is_file():
    print('missing prerequisite: source-bound prebuilt Human component artifacts', file=sys.stderr)
    sys.exit(78)
manifest = owner.load(manifest_path)
owner.require(manifest.get('schema') == 'layerx-human-components-artifacts.v1', 'Human component artifact schema')
revision, source = owner.identity()
owner.require(manifest.get('revision') == revision and manifest.get('source_digest') == source,
              'Human component artifact candidate binding')
rows = manifest.get('artifacts', {})
owner.require(set(rows) == {'lib-tests', 'identity-boundary', 'browser-boundary', 'components'},
              'complete real Human component artifact inventory')
artifacts = {name: owner.executable(row, revision, source) for name, row in rows.items()}
tests = 0

def execute(argv, name, timeout=180):
    log = evidence / ('104.38.3-' + name + '.log')
    with log.open('x') as stream:
        os.chmod(log, 0o600)
        result = subprocess.run([str(arg) for arg in argv], cwd=root, stdin=subprocess.DEVNULL,
                                stdout=stream, stderr=subprocess.STDOUT,
                                timeout=min(timeout, owner.remaining()))
    print('exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
    if result.returncode:
        sys.exit(result.returncode)
    return log.read_text()

for name, selector in (('lib-tests', 'server::component'), ('identity-boundary', ''), ('browser-boundary', '')):
    binary = artifacts[name]
    inventory = execute([binary, selector, '--list'], name + '-inventory')
    declared = set(re.findall(r'^(.+): test$', inventory, re.M))
    owner.require(declared, 'required real component corpus is empty: ' + name)
    output = execute(['sh', root / 'tools/runtime/run-with-clock.sh', binary, selector,
                      '--nocapture', '--test-threads=1'], name)
    results = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
    owner.require(len(results) == 1 and int(results[0][0]) == len(declared) and results[0][1:] == ('0', '0'),
                  'complete real component corpus must pass without ignored cases: ' + name)
    tests += int(results[0][0])

fixture_path = os.environ.get('PAXEER_X_HUMAN_API_MANIFEST')
if not fixture_path or not Path(fixture_path).is_file():
    print('missing prerequisite: genuine production Human API process manifest', file=sys.stderr)
    sys.exit(78)
fixture = owner.load(fixture_path)
owner.require(fixture.get('schema') == 'layerx-human-api-boundary.v1', 'actual Human process fixture schema')
component = owner.executable(fixture['artifacts']['components'], revision, source)
owner.require(component == artifacts['components'], 'production component process must use the declared candidate binary')
output = execute(['python3', root / 'tools/qualification/paxeer-x/human-api-boundary.py',
                  '--manifest', fixture_path], 'production-boundary', timeout=1700)
results = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=(\d+)$', output, re.M)
owner.require(len(results) == 1 and int(results[0][0]) > 0 and results[0][1] == '0',
              'complete real production Human API service contract required')
tests += int(results[0][0])
print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=0', flush=True)
PY
