#!/usr/bin/env bash
set -euo pipefail
exec python3 - <<'PY'
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True
root = Path.cwd().resolve()
sys.path.insert(0, str(root / 'tools/paxeer-x'))
from candidate import load_private, source_identity

expected = {
    'modules/layerxanchor': 'layerxanchor-module.test',
    'modules/layerxanchor/keeper': 'layerxanchor-keeper.test',
    'modules/launchpad/keeper': 'launchpad-keeper.test',
    'modules/launchpad/types': 'launchpad-types.test',
}
required = {'TestMsgUpdateParamsGovernanceRoute', 'TestMsgUpdateParamsCodecAndSigners', 'TestMsgUpdateParamsRefusals'}
try:
    manifest_path = Path(os.environ['PAXEER_X_106_4_9_BUNDLE']).resolve()
    if root == manifest_path.parent or root in manifest_path.parents:
        raise ValueError('bundle must be private and outside source')
    bundle = load_private(manifest_path)
    identity = source_identity(root, os.environ.get('PAXEER_X_MAINLINE', 'refs/heads/main'))
    if set(bundle) != {'source', 'targets'} or bundle['source'] != identity or identity['dirty']:
        raise ValueError('bundle source identity mismatch')
    targets = bundle['targets']
    if set(targets) != set(expected):
        raise ValueError('bundle package corpus mismatch')
    paths = {}
    for package, name in expected.items():
        row = targets[package]
        if set(row) != {'binary', 'sha256'} or row['binary'] != name:
            raise ValueError('bundle target mismatch')
        binary = manifest_path.parent / name
        if binary.is_symlink() or not binary.is_file() or not os.access(binary, os.X_OK):
            raise ValueError('prebuilt test binary absent')
        if hashlib.sha256(binary.read_bytes()).hexdigest() != row['sha256']:
            raise ValueError('prebuilt test binary digest mismatch')
        paths[package] = binary
    tests = skipped = 0
    for package, binary in paths.items():
        print('package: ' + package, flush=True)
        result = subprocess.run([str(binary), '-test.v', '-test.count=1', '-test.timeout=10m'],
                                cwd=root / package, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        output = result.stdout.decode('utf-8', errors='replace')
        print(output, end='', flush=True)
        passed = set(re.findall(r'^--- PASS: (\S+)', output, re.M))
        skipped += len(re.findall(r'^\s*--- SKIP:', output, re.M))
        tests += len(re.findall(r'^\s*--- PASS:', output, re.M))
        if result.returncode or not passed or (package.endswith('/keeper') and not required <= passed):
            raise ValueError('prebuilt test failure or missing required case: ' + package)
    print(f'PAXEER_X_GATE tests={tests} skipped={skipped}')
    if skipped:
        raise ValueError('required corpus contains skipped tests')
except (OSError, ValueError, KeyError, TypeError) as error:
    print('106.4.9 refused: ' + str(error), file=sys.stderr)
    sys.exit(1)
PY
