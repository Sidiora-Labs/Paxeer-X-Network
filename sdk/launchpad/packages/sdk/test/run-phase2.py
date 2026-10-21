#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[4]
sdk = root / 'client/packages/sdk'
fixture_path = os.environ.get('SDK_CONNECTED_CONFIG')
if not fixture_path: raise SystemExit('SDK_CONNECTED_CONFIG actual coordinator fixture is required')
fixture = json.loads(pathlib.Path(fixture_path).read_bytes())
required_commands = ['armSnapshotAckBarrier','fillDuringSnapshotAck','armDelayedTokenA','restartGateway','fillAfterGatewayRestart','expireCursor73HoursAndRevokeSession','emit100Fills','armDelayedSnapshotTokenA','waitDelayedSnapshotTokenA','releaseDelayedSnapshotTokenA']
if fixture.get('realm') != 'private_fixture' or any(name not in fixture.get('commands',{}) for name in required_commands):
    raise SystemExit('Connected fixture lacks real qualification capabilities')
commands = [
    [sys.executable, str(sdk / 'test/generation.test.py')],
    [sys.executable, str(sdk / 'test/compatibility.test.py')],
    ['node','--test', str(sdk / 'test/primitives.test.mjs')],
    ['pnpm','--dir',str(root / 'client/apps/web'),'exec','vitest','run','--config',str(sdk / 'test/vitest.phase2.config.mts')],
    ['node', str(sdk / 'examples/pagination.mjs')],
    ['node', str(sdk / 'examples/snapshot-resume.mjs')],
    ['node','--test', str(sdk / 'test/connected-services.test.mjs')],
]
for command in commands:
    environment = os.environ.copy()
    if command == ['node', str(sdk / 'examples/snapshot-resume.mjs')]: environment['SDK_RESUME'] = '1'
    result = subprocess.run(command,cwd=root,env=environment,timeout=180)
    if result.returncode: raise SystemExit(result.returncode)
print(json.dumps({'task':'2.24','source_only':False,'fixtureSha256':hashlib.sha256(pathlib.Path(fixture_path).read_bytes()).hexdigest(),'commands':commands,'exit_code':0},sort_keys=True))
