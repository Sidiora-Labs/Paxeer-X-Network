#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 - "$root" "$@" <<'PY'
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

os.umask(0o077)
ROOT = Path(sys.argv[1])
TASK = '103.6.13'
GATE = 'tools/paxeer-x/gates/103.6.13.sh'
SCHEMA = 'layerx.native-export-corpus-build.v1'
DEADLINE = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1800)))
MANDATORY = {
    'fact_grammar_accepts_each_canonical_form_in_request_order',
    'fact_grammar_refuses_every_noncanonical_form',
    'receipt_header_and_inclusion_records_round_trip_their_frozen_byte_layouts',
    'account_state_records_round_trip_both_variants_and_refuse_unknown_variants',
    'checkpoint_record_round_trips_and_refuses_partial_reordered_or_unknown_material',
    'record_size_is_capped_at_exactly_one_mebibyte',
    'artifact_container_round_trips_and_refuses_duplicate_or_malformed_buckets',
    'independent_trust_refuses_false_membership_threshold_and_network',
    'settlement_anchored_is_refused_without_an_offline_finality_proof',
    'rogue_sequencer_key_and_false_key_range_are_refused_against_independent_trust',
    'swapped_and_orphan_records_are_refused',
    'state_path_tamper_and_missing_maintenance_link_are_refused',
    'checkpoint_facts_refuse_wrong_domain_membership_and_incomplete_availability',
}
LAST = {'command': [], 'log_path': None}


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def private(path, directory=False):
    info = path.lstat()
    require(not path.is_symlink() and info.st_uid == os.geteuid() and not info.st_mode & 0o077
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
            'private caller-owned evidence required')


def save(path, value):
    if path.exists() or path.is_symlink():
        private(path)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def load(path):
    private(path)
    require(path.stat().st_size <= 32 * 1024 * 1024, 'bounded manifest required')
    return json.loads(path.read_text())


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def identity():
    names = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    paths = {os.fsdecode(name) for name in names if name} | {GATE}
    selected = sorted(path for path in paths
        if not any(part.startswith('.env') for part in Path(path).parts)
        and (Path(path).suffix in {'.rs', '.toml', '.lock'} or path == GATE
             or path.startswith('agent/crates/layerx-proof/tests/fixtures/'))
        and (ROOT / path).is_file())
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'hashes': {path: digest(ROOT / path) for path in selected}}


def remaining(bound):
    value = min(bound, DEADLINE - time.time())
    if value <= 0:
        raise subprocess.TimeoutExpired(LAST['command'], 0)
    return value


def command(argv, log, environment=None, bound=900):
    LAST.update(command=[str(value) for value in argv], log_path=str(log))
    with log.open('w') as stream:
        log.chmod(0o600)
        result = subprocess.run(LAST['command'], cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            timeout=remaining(bound))
    print('command=' + json.dumps(LAST['command']) + ' exit=' + str(result.returncode)
          + ' log=' + str(log), flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, argv)
    return log.read_text()


def checked(row):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and os.access(path, os.X_OK) and digest(path) == row['sha256'],
            'actual source-bound test executable missing or changed')
    return path


def build(directory):
    require(not (directory / 'build-manifest.json').exists(), 'task build already recorded')
    before = identity()
    environment = dict(os.environ, PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''),
        RUSTUP_TOOLCHAIN='1.91.1', CARGO_BUILD_JOBS='2',
        CARGO_TARGET_DIR=os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/agent'))
    lock_path = Path('/root/lx-cargo/agent-build.lock')
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                time.sleep(min(1, remaining(1)))
        text = command(['/root/.cargo/bin/cargo', 'test', '--locked', '--manifest-path',
                'agent/Cargo.toml', '-p', 'layerx-proof', '--test', 'export_complete',
                '--no-run', '--message-format=json'], directory / 'build.log', environment)
    artifacts = []
    for line in text.splitlines():
        if not line.startswith('{'):
            continue
        row = json.loads(line)
        if (row.get('reason') == 'compiler-artifact' and row['profile']['test']
                and row.get('executable') and row['target']['name'] == 'export_complete'
                and row['manifest_path'] == str(ROOT / 'agent/crates/layerx-proof/Cargo.toml')):
            artifacts.append(row)
    require(len(artifacts) == 1, 'exact genuine export_complete compiler artifact required')
    binary = Path(artifacts[0]['executable']).resolve(strict=True)
    artifact = {'path': str(binary), 'sha256': digest(binary)}
    checked(artifact)
    require(identity() == before, 'actual compiler dependency source changed')
    save(directory / 'build-manifest.json', {'schema': SCHEMA, 'task': TASK, 'identity': before,
         'build_exit_code': 0, 'artifact': artifact, 'compiler_artifact': artifacts[0],
         'deadline_epoch': DEADLINE, 'qualification': 'UNRUN'})
    print('ARTIFACTS ' + str(directory / 'build-manifest.json'), flush=True)


def qualify(directory):
    built = load(directory / 'build-manifest.json')
    require(built['schema'] == SCHEMA and built['task'] == TASK and built['build_exit_code'] == 0,
            'genuine successful focused build manifest required')
    require(built['identity'] == identity() and built['deadline_epoch'] == DEADLINE,
            'compiled revision/tree/dependency bytes or original deadline changed')
    binary = checked(built['artifact'])
    inventory = command([binary, '--list', '--format', 'terse'], directory / 'inventory.log', bound=30)
    cases = re.findall(r'^(.+): test$', inventory, re.M)
    require(cases and len(cases) == len(set(cases)) and MANDATORY <= set(cases),
            'every retained export trust/integrity case must be present')
    text = command([binary, '--test-threads=1', '--nocapture'], directory / 'verify.log')
    passed = re.findall(r'^test ([^\s]+) \.\.\. ok$', text, re.M)
    summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;[^\n]*$', text, re.M)
    require(len(passed) == len(set(passed)) and set(passed) == set(cases)
            and len(summaries) == 1 and tuple(map(int, summaries[0])) == (len(cases), 0, 0),
            'complete existing corpus must pass once without skipped cases')
    require(built['identity'] == identity(), 'compiled source changed during verification')
    checked(built['artifact'])
    save(directory / 'result.json', {'task': TASK, 'revision': built['identity']['revision'],
         'command': 'timeout 15m tools/paxeer-x/gates/103.6.13.sh', 'exit_code': 0,
         'log_path': str(directory / 'verify.log'), 'executed_tests': sorted(cases), 'skipped': 0})
    print('PAXEER_X_GATE tests=' + str(len(cases)) + ' skipped=0', flush=True)


DIRECTORY = None
try:
    args = sys.argv[2:]
    require(args in ([], ['--build']), '103.6.13 accepts only --build or no selector arguments')
    DIRECTORY = Path(os.environ.get('PAXEER_X_EXPORT_COMPLETE_EVIDENCE',
        '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task103613'))
    DIRECTORY.mkdir(parents=True, mode=0o700, exist_ok=True)
    private(DIRECTORY, True)
    DIRECTORY = DIRECTORY.resolve(strict=True)
    require(DIRECTORY != ROOT and ROOT not in DIRECTORY.parents, 'evidence must be outside checkout')
    build(DIRECTORY) if args else qualify(DIRECTORY)
except (RuntimeError, OSError, ValueError, KeyError, TypeError,
        subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
    code = error.returncode if isinstance(error, subprocess.CalledProcessError) else 124 if isinstance(error, subprocess.TimeoutExpired) else 78
    code = code if code > 0 else 1
    if DIRECTORY is not None:
        save(DIRECTORY / ('build-failure.json' if sys.argv[2:] == ['--build'] else 'failure.json'),
             {'task': TASK, 'revision': git('rev-parse', 'HEAD'), 'exit_code': code,
              **LAST, 'observed': str(error), 'qualification': 'FAILED'})
    print('103.6.13 refused: ' + str(error), file=sys.stderr)
    sys.exit(code)
PY
