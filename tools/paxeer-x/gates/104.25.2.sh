#!/usr/bin/env bash
# paxeer-x-services: mirror
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "${root}" "$@" <<'PY'
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

ROOT = Path(sys.argv[1])
BUILD = sys.argv[2:] == ['--build']
if sys.argv[2:] and not BUILD:
    raise SystemExit('usage: 104.25.2.sh [--build]')
EVIDENCE = Path(os.environ.get('PAXEER_X_MIRROR_EVIDENCE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104252'))
DEADLINE = int(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(int(time.time()) + 1800)))
TESTS = ['verify::tests::native_fixture_receipt_verifies_and_altered_bytes_are_refused']


def refuse(reason, code=78):
    print('mirror gate refused: ' + reason, file=sys.stderr)
    raise SystemExit(code)


def private_directory(path):
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    resolved = path.resolve()
    info = resolved.stat()
    if (resolved == ROOT or ROOT in resolved.parents or not stat.S_ISDIR(info.st_mode)
            or info.st_uid != os.geteuid() or info.st_mode & 0o077):
        refuse('evidence directory is not private and outside source')
    return resolved


def private_json(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077 or info.st_size > 4 * 1024 * 1024:
            refuse('protected evidence is not a bounded private owner file')
        return json.load(stream)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args], text=True).strip()


def source_identity():
    names = git('ls-files').splitlines()
    selected = [n for n in names if n.endswith('.rs') or Path(n).name in ('Cargo.toml', 'Cargo.lock')]
    selected += ['scripts/qualify-mirror-verification-live.sh', 'tools/paxeer-x/gates/104.25.2.sh', 'human/apps/web/src/explorer/components.tsx', 'human/apps/web/copy/catalog.ts', 'human/apps/web/copy/messages.generated.ts', 'human/apps/web/next.config.mjs', 'tools/paxeer-x/route-catalogue.json']
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'hashes': {n: digest(ROOT / n) for n in sorted(set(selected))}}


def remaining(maximum):
    value = min(maximum, DEADLINE - time.time())
    if value <= 0:
        refuse('task deadline elapsed', 124)
    return value


def execute(argv, log, maximum, environment=None):
    with log.open('wb') as stream:
        os.chmod(log, 0o600)
        try:
            code = subprocess.run(argv, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=stream,
                                  stderr=subprocess.STDOUT, timeout=remaining(maximum), env=environment).returncode
        except subprocess.TimeoutExpired:
            code = 124
    print('command=' + json.dumps(argv) + ' exit=' + str(code) + ' log=' + str(log))
    if code:
        raise SystemExit(code if code > 0 else 1)


def build(directory):
    before = source_identity()
    lock_path = Path('/root/lx-cargo/interop-build.lock')
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                remaining(1)
                time.sleep(1)
        command = ['/root/.cargo/bin/cargo', 'build', '--locked', '--manifest-path', 'interop/Cargo.toml',
                   '-p', 'layerx-mirror', '--tests', '--bin', 'layerx-mirror-verify', '--message-format=json']
        execute(command, directory / 'build.log', 1200)
    with Path('/root/lx-cargo/platform-build.lock').open('a') as platform_lock:
        while True:
            try:
                fcntl.flock(platform_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                remaining(1)
                time.sleep(1)
        execute(['/root/.cargo/bin/cargo', 'build', '--locked', '--manifest-path', 'platform/Cargo.toml', '-p', 'layerx-platform-gateway', '--bin', 'layerx-gateway'], directory / 'gateway-build.log', 600,
                dict(os.environ, CARGO_TARGET_DIR='/root/lx-target/platform'))
    if source_identity() != before:
        refuse('source changed during compilation')
    artifacts = {}
    for line in (directory / 'build.log').read_text(errors='replace').splitlines():
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if item.get('reason') != 'compiler-artifact' or not item.get('executable'):
            continue
        target = item['target']['name']
        if (target == 'layerx_mirror' and item.get('profile', {}).get('test')) or (target == 'layerx-mirror-verify' and not item.get('profile', {}).get('test')):
            path = Path(item['executable']).resolve()
            artifacts[item['target']['name']] = {'path': str(path), 'sha256': digest(path)}
    if set(artifacts) != {'layerx_mirror', 'layerx-mirror-verify'}:
        refuse('actual compiler did not emit native test and production verifier executables')
    tsc = Path(os.environ.get('PAXEER_X_MIRROR_WEB_TSC', '/root/Layerx-protocol/human/apps/web/node_modules/typescript/bin/tsc'))
    node = Path('/root/lx-toolchains/node24/bin/node')
    execute([str(node), str(tsc), '--noEmit', '--incremental', 'false', '--project', str(ROOT / 'human/apps/web/tsconfig.json')], directory / 'web-typecheck.log', 300)
    if source_identity() != before:
        refuse('consumer source changed during focused typecheck')
    manifest = {'schema': 'layerx.mirror-verification-build.v1', 'source': before, 'artifacts': artifacts,
                'command': command, 'exit_code': 0, 'log': str(directory / 'build.log')}
    fd = os.open(directory / 'build.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(manifest, stream, sort_keys=True)
        stream.write('\n')


def live_verification(directory, verifier):
    required = [
        'LAYERX_MIRROR_VERIFY_CONFIG', 'LAYERX_MIRROR_CANONICAL_REQUEST', 'LAYERX_MIRROR_FAILOVER_REQUEST',
        'LAYERX_MIRROR_DIVERGENCE_REQUEST', 'LAYERX_MIRROR_TAMPER_REQUEST', 'LAYERX_MIRROR_STATE_REQUEST',
        'LAYERX_MIRROR_STATE_TAMPER_REQUEST', 'LAYERX_MIRROR_EXPLORER_VERIFY_URL',
        'LAYERX_MIRROR_TS_CONFORMANCE', 'LAYERX_MIRROR_PYTHON_CONFORMANCE', 'LAYERX_MIRROR_GO_CONFORMANCE',
        'LAYERX_MIRROR_JVM_CONFORMANCE', 'LAYERX_MIRROR_SWIFT_CONFORMANCE', 'LAYERX_MIRROR_DOTNET_CONFORMANCE',
    ]
    missing = [name for name in required if not os.environ.get(name)]
    if missing:
        refuse('genuine independent mirror receipt/state/browser/SDK fixture unavailable: ' + ','.join(missing))
    for name in required[:7]:
        path = Path(os.environ[name])
        if not path.is_absolute() or '.env' in str(path).lower():
            refuse('mirror authority fixture must be an explicit protected path, never an environment file')
        private_json(path)
    if any(os.environ.get(name) for name in ('LAYERX_NODE_URL', 'LAYERX_GATEWAY_URL', 'LAYERX_EXPLORER_API_ORIGIN')):
        refuse('mirror-only qualification must not use LayerX node/gateway/explorer authority')
    os.environ['LAYERX_MIRROR_VERIFY_BIN'] = verifier
    execute(['bash', 'scripts/qualify-mirror-verification-live.sh', 'task-104.25.2'], directory / 'mirror-only-live.log', 900)
    return 18


def verify(directory):
    build_manifest = private_json(directory / 'build.json')
    if build_manifest.get('schema') != 'layerx.mirror-verification-build.v1' or build_manifest.get('exit_code') != 0 or build_manifest.get('source') != source_identity():
        refuse('focused compilation is not bound to this published source')
    artifacts = build_manifest['artifacts']
    for artifact in artifacts.values():
        if digest(Path(artifact['path'])) != artifact['sha256']:
            refuse('compiled focused artifact changed')
    tests = 0
    for index, name in enumerate(TESTS):
        log = directory / ('case-' + str(index) + '.log')
        execute([artifacts['layerx_mirror']['path'], '--exact', name, '--nocapture'], log, 60)
        if not re.search(r'test result: ok\. 1 passed; 0 failed;', log.read_text()):
            refuse('focused runtime case did not actually execute')
        tests += 1
    try:
        tests += live_verification(directory, artifacts['layerx-mirror-verify']['path'])
    finally:
        print('PAXEER_X_GATE tests=' + str(tests) + ' skipped=0')



try:
    directory = private_directory(EVIDENCE)
    if BUILD:
        build(directory)
    else:
        verify(directory)
except (OSError, ValueError, KeyError, TypeError, AttributeError) as error:
    refuse('required protected evidence/artifact unavailable or malformed: ' + type(error).__name__)
PY
