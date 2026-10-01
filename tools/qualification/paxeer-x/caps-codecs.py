#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]


def main():
    output = Path(os.environ['CAPS_BUILD_DIR']).resolve()
    manifest = json.loads((output / 'manifest.json').read_text())
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if manifest['revision'] != revision:
        raise RuntimeError('prebuilt artifact revision mismatch')
    for tree, expected in manifest['source_trees'].items():
        actual = subprocess.check_output(['git', 'rev-parse', 'HEAD:' + tree], cwd=ROOT, text=True).strip()
        if actual != expected:
            raise RuntimeError('prebuilt source tree mismatch: ' + tree)
        subprocess.run(['git', 'diff', '--exit-code', 'HEAD', '--', tree], cwd=ROOT, check=True)
    for artifact in manifest['artifacts'].values():
        with open(artifact['path'], 'rb') as file:
            actual = hashlib.file_digest(file, 'sha256').hexdigest()
        if actual != artifact['sha256']:
            raise RuntimeError('prebuilt executable digest mismatch')
    vectors = output / 'native-vectors.json'
    with vectors.open('w') as target:
        subprocess.run([manifest['artifacts']['native']['path']], cwd=ROOT, stdout=target, check=True, timeout=60)
    native = json.loads(vectors.read_text())
    if native.get('producer') != 'native-budget-codec-and-grant-save':
        raise RuntimeError('unexpected native producer contract')
    env = dict(os.environ, LAYERX_CAPS_VECTORS=str(vectors))
    counts = {}
    for name in ['client', 'agentd']:
        result = subprocess.run([manifest['artifacts'][name]['path'], '--test-threads=1'], cwd=ROOT / 'agent', env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=600)
        (output / (name + '-gate.log')).write_text(result.stdout)
        print(result.stdout, end='')
        if result.returncode:
            raise RuntimeError(name + ' test exit ' + str(result.returncode))
        match = re.search(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored;', result.stdout)
        if not match or int(match[1]) == 0 or (name == 'client' and int(match[1]) != 4):
            raise RuntimeError('missing cases or skipped tests: ' + name)
        counts[name] = int(match[1])
    record = {'revision': revision, 'exit_code': 0, 'tests': sum(counts.values()), 'test_groups': counts, 'native_vectors': 6, 'skipped': 0, 'claim': 'canonical codecs and existing reconciliation compatibility only'}
    path = output / 'gate-result.json'
    path.write_text(json.dumps(record, indent=2) + '\n')
    path.chmod(0o600)
    print(json.dumps(record))


if __name__ == '__main__':
    try:
        main()
    except (OSError, KeyError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print('caps-codecs: ' + str(error), file=sys.stderr)
        sys.exit(1)
