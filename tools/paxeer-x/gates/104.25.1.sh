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
    raise SystemExit('usage: 104.25.1.sh [--build]')
EVIDENCE = Path(os.environ.get('PAXEER_X_MIRROR_EVIDENCE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task104251'))
DEADLINE = int(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(int(time.time()) + 1800)))
TESTS = [
    'runtime::tests::worker_degradation_survives_progress_application_per_lane',
    'runtime::tests::checkpoint_refusal_latches_until_verified_evidence',
    'runtime::tests::clean_restart_resumes_after_the_contiguous_spool_prefix',
]


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
    selected += ['scripts/qualify-mirror-live.sh', 'tools/paxeer-x/gates/104.25.1.sh']
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'hashes': {n: digest(ROOT / n) for n in sorted(set(selected))}}


def remaining(maximum):
    value = min(maximum, DEADLINE - time.time())
    if value <= 0:
        refuse('task deadline elapsed', 124)
    return value


def execute(argv, log, maximum):
    with log.open('wb') as stream:
        os.chmod(log, 0o600)
        try:
            code = subprocess.run(argv, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=stream,
                                  stderr=subprocess.STDOUT, timeout=remaining(maximum)).returncode
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
        command = ['/root/.cargo/bin/cargo', 'test', '--locked', '--manifest-path', 'interop/Cargo.toml',
                   '-p', 'layerx-mirror', '--lib', '--test', 'checkpoint_vectors', '--no-run', '--message-format=json']
        execute(command, directory / 'build.log', 1200)
    if source_identity() != before:
        refuse('source changed during compilation')
    artifacts = {}
    for line in (directory / 'build.log').read_text(errors='replace').splitlines():
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if item.get('reason') != 'compiler-artifact' or not item.get('executable') or not item.get('profile', {}).get('test'):
            continue
        if item['target']['name'] in ('layerx_mirror', 'checkpoint_vectors'):
            path = Path(item['executable']).resolve()
            artifacts[item['target']['name']] = {'path': str(path), 'sha256': digest(path)}
    if set(artifacts) != {'layerx_mirror', 'checkpoint_vectors'}:
        refuse('actual compiler did not emit both focused test executables')
    manifest = {'schema': 'layerx.mirror-build.v1', 'source': before, 'artifacts': artifacts,
                'command': command, 'exit_code': 0, 'log': str(directory / 'build.log')}
    fd = os.open(directory / 'build.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(manifest, stream, sort_keys=True)
        stream.write('\n')


def chain(value):
    return isinstance(value, dict) and isinstance(value.get('latest_batch_mirrored'), int) and value['latest_batch_mirrored'] >= 0


def recovered(status):
    return (chain(status.get('ethereum')) and chain(status.get('solana'))
            and status['ethereum']['phase'] == status['solana']['phase'] == 'retrieved_verified'
            and status['ethereum']['latest_batch_mirrored'] == status['solana']['latest_batch_mirrored'])


def live_evidence():
    raw = os.environ.get('PAXEER_X_MIRROR_LIVE_EVIDENCE')
    if not raw:
        refuse('genuine both-chain testnet publication/retrieval/stall/reorg evidence is absent; no live publication was started')
    live = private_json(Path(raw))
    if live.get('schema') != 'layerx.mirror-live-evidence.v1' or live.get('exit_code') != 0:
        refuse('live owner evidence schema or exit does not attest a complete run')
    if live.get('revision') != git('rev-parse', 'HEAD') or live.get('tree') != git('rev-parse', 'HEAD^{tree}'):
        refuse('live evidence belongs to a different published source')
    expected = sorted((ROOT / 'interop/crates/layerx-mirror/src').rglob('*.rs')) + [ROOT / 'scripts/qualify-mirror-live.sh']
    if live.get('source_hashes') != {str(p.relative_to(ROOT)): digest(p) for p in expected}:
        refuse('live publisher source identity differs')
    for key in ('publisher_sha256', 'fault_controller_sha256'):
        if not re.fullmatch('[0-9a-f]{64}', live.get(key, '')):
            refuse('actual publisher/fault controller identity is absent')
    phases = live.get('phases', {})
    names = {'baseline', 'ethereum_stall', 'ethereum_restored', 'solana_stall', 'solana_restored',
             'ethereum_reorg', 'ethereum_recovered', 'solana_reorg', 'solana_recovered'}
    if set(phases) != names:
        refuse('live evidence does not cover both complete failure/recovery paths')
    baseline = phases['baseline']
    if not recovered(baseline):
        refuse('baseline has no actually retrieved matching archives')
    for phase in phases.values():
        if not isinstance(phase, dict) or not phase.get('node', {}).get('ready'):
            refuse('mirrors do not demonstrate independent live LayerX node acquisition')
        if any(phase.get(k) != baseline.get(k) for k in ('source_chain_id', 'native_network_id', 'native_protocol_version')):
            refuse('live source network changed between phases')
        for lane in ('ethereum', 'solana'):
            value = phase.get(lane)
            if not isinstance(value, dict) or not chain(value):
                refuse('a configured live archive lane has no genuine batch coordinate')
            required = ('latest_checkpoint_batch_mirrored', 'latest_checkpoint_id_mirrored', 'checkpoint_batch_lag',
                        'checkpoint_within_budget', 'freshness_budget_batches', 'freshness')
            if any(k not in value for k in required):
                refuse('live archive omitted explicit checkpoint/freshness availability')
            checkpoint = value['latest_checkpoint_batch_mirrored']
            if checkpoint is None:
                if value['latest_checkpoint_id_mirrored'] is not None:
                    refuse('unavailable checkpoint was replaced with fabricated coordinates')
            target = value.get('latest_checkpoint_batch_verified')
            expected_lag = None if target is None else max(0, target - (checkpoint or 0))
            if value['checkpoint_batch_lag'] != expected_lag:
                refuse('live freshness does not match genuine verified checkpoint coverage')
            if checkpoint is not None and (not isinstance(checkpoint, int) or checkpoint > value['latest_batch_mirrored']
                  or not re.fullmatch('[0-9a-f]{64}', value['latest_checkpoint_id_mirrored'] or '')):
                refuse('live checkpoint coordinates are malformed')
    for lane, other, origin in [('ethereum', 'solana', baseline), ('solana', 'ethereum', phases['ethereum_restored'])]:
        stalled = phases[lane + '_stall']
        if (stalled[lane].get('error_class') != 'rpc' or stalled[lane].get('ready') is not False
                or stalled[other]['latest_batch_mirrored'] <= origin[other]['latest_batch_mirrored']):
            refuse('a stalled mirror hid degradation or prevented the other lane advancing')
        if not recovered(phases[lane + '_restored']) or not recovered(phases[lane + '_recovered']):
            refuse('live archive recovery did not retrieve matching chain data')
        if phases[lane + '_reorg'][lane].get('reorgs_observed', 0) <= 0:
            refuse('the actual chain reorg was not observed')
    return len(phases)


def verify(directory):
    build_manifest = private_json(directory / 'build.json')
    if build_manifest.get('schema') != 'layerx.mirror-build.v1' or build_manifest.get('exit_code') != 0 or build_manifest.get('source') != source_identity():
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
    log = directory / 'checkpoint-vectors.log'
    execute([artifacts['checkpoint_vectors']['path'], '--exact', 'mirror_checkpoint_identity_and_freshness_follow_the_shared_vectors', '--nocapture'], log, 120)
    if not re.search(r'test result: ok\. 1 passed; 0 failed;', log.read_text()):
        refuse('real shared checkpoint vector case did not actually execute')
    tests += 1
    try:
        tests += live_evidence()
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
