#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
NATIVE_PATHS = ['Makefile', 'src', 'include', 'cmd', 'programs', 'agent',
                'contracts/config/checkpoint-settlement.json']


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def protected(path, executable=False):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not info.st_mode & 0o077,
            'artifact must be a protected regular file')
    require(not executable or os.access(path, os.X_OK), 'artifact must be executable')


def document(variable):
    value = os.environ.get(variable)
    require(value, variable + ' is required')
    path = Path(value)
    protected(path)
    return json.loads(path.read_text())


def artifact(record, revision):
    require(record['source_revision'] == revision, 'artifact revision mismatch')
    path = Path(record['path'])
    require(path.is_absolute(), 'absolute artifact path required')
    protected(path, True)
    require(hashlib.file_digest(path.open('rb'), 'sha256').hexdigest() == record['sha256'],
            'artifact digest mismatch')
    return str(path)


def main():
    count = 0
    try:
        require(os.geteuid() == 0, 'isolated real replica harness requires root')
        require(not git('status', '--porcelain', '--untracked-files=no'), 'clean source required')
        revision = git('rev-parse', 'HEAD')
        build = document('PAXEER_X_AUTHORITY_BUILD_MANIFEST')
        require(build['source_revision'] == revision and
                build['source_tree'] == git('rev-parse', 'HEAD^{tree}') and
                build['build_exit'] == 0, 'source-bound successful scoped build required')
        binaries = {name: artifact(build['artifacts'][name], revision)
                    for name in ['gateway_tests', 'authority_tests', 'authority']}
        foundation = document('PAXEER_X_FOUNDATION_ARTIFACT_MANIFEST')
        require(foundation['version'] == 1 and foundation['build']['exit_code'] == 0,
                'successful real native artifact build required')
        native_revision = foundation['source_revision']
        require(foundation['source_tree'] == git('rev-parse', native_revision + '^{tree}'),
                'native source tree mismatch')
        require(not git('diff', native_revision, revision, '--', *NATIVE_PATHS),
                'native source differs from this candidate')
        native = {name: artifact(foundation['artifacts'][name], native_revision)
                  for name in ['layerxd', 'layerx-genesis-build']}
        directory = Path(native['layerxd']).parent
        require(Path(native['layerx-genesis-build']).parent == directory,
                'native executables must share the isolated bundle')
        env = os.environ.copy()
        env.update(LAYERX_TEST_NATIVE_BIN_DIR=str(directory),
                   PAXEER_X_GATEWAY_TEST_BIN=binaries['gateway_tests'],
                   PAXEER_X_AUTHORITY_BIN=binaries['authority'],
                   PAXEER_X_AUTHORITY_RETAIN_STATE='1')
        result = subprocess.run([binaries['authority_tests'],
            'router_authority_readiness_schema_restart_contract', '--exact', '--nocapture'],
            cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, timeout=840)
        print(result.stdout, end='')
        markers = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=0$', result.stdout, re.M)
        require(result.returncode == 0, 'real authority contract test failed')
        require('1 passed; 0 failed' in result.stdout and len(markers) == 1,
                'exactly one executed contract and one actual count required')
        count = int(markers[0])
        require(count > 0, 'zero cases cannot qualify')
        return 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print('router-authority-schema: refusal: ' + str(error), file=sys.stderr)
        print(f'PAXEER_X_GATE tests={count} skipped=0')
        return 1


if __name__ == '__main__':
    sys.exit(main())
