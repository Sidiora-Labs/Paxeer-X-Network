#!/usr/bin/env bash
set -euo pipefail
exec python3 - <<'PY'
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
root = Path.cwd().resolve()
sys.path.insert(0, str(root / 'tools/paxeer-x'))
from candidate import load_private, source_identity

try:
    manifest = Path(os.environ['PAXEER_X_106_4_6_BUNDLE']).resolve()
    if root == manifest.parent or root in manifest.parents:
        raise ValueError('replay bundle must be private and outside source')
    bundle = load_private(manifest)
    identity = source_identity(root, os.environ.get('PAXEER_X_MAINLINE', 'refs/heads/main'))
    if set(bundle) != {'source', 'binary', 'sha256'} or bundle['source'] != identity or identity['dirty']:
        raise ValueError('replay bundle source mismatch')
    if bundle['binary'] != 'upgrade-replay':
        raise ValueError('unexpected replay binary')
    binary = manifest.parent / bundle['binary']
    if binary.is_symlink() or not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError('prebuilt replay executable absent')
    if hashlib.sha256(binary.read_bytes()).hexdigest() != bundle['sha256']:
        raise ValueError('prebuilt replay executable digest mismatch')
    with tempfile.TemporaryDirectory(prefix='replay-', dir=manifest.parent) as work:
        env = dict(os.environ, UPGRADE_REPLAY_BINARY=str(binary), UPGRADE_REPLAY_WORKDIR=work,
                   UPGRADE_REPLAY_LOGDIR=str(Path(work) / 'logs'), UPGRADE_REPLAY_IN_PLACE='0',
                   UPGRADE_REPLAY_KEEP_STORES='0')
        result = subprocess.run(['tools/chain/upgrade-replay/run.sh'], cwd=root, env=env,
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        output = result.stdout.decode('utf-8', errors='replace')
        print(output, end='', flush=True)
        if result.returncode or re.search(r'^  FAIL ', output, re.M):
            raise ValueError('real replay failed')
        claims = set(re.findall(r'^  ok   (.*)$', output, re.M))
        required = {
            'v6.10 voting period is one hour',
            'v6.10 expedited voting period is twenty minutes',
            'v6.10 carries four supplied web-search signers',
            'v6.10 web-search threshold is three',
            'v6.10 web-search is unpaused',
            'six-module proposal validation refuses a foreign message',
            'six-module governance handler refuses a foreign message',
            'v6.10 operating values and all six governance effects survive commit and reopen',
        }
        if not required <= claims:
            raise ValueError('required replay assertions absent')
        tests = len(re.findall(r'^  ok   ', output, re.M))
        if not tests:
            raise ValueError('empty replay corpus')
        print(f'PAXEER_X_GATE tests={tests} skipped=0')
except (OSError, ValueError, KeyError, TypeError) as error:
    print('106.4.6 refused: ' + str(error), file=sys.stderr)
    sys.exit(1)
PY
