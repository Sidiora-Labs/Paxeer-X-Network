#!/usr/bin/env python3
"""Every advertised unified custody plan shape runs through its real journey.

--build compiles the layerx-human-service intent_submit test executable once
and records it in a private build manifest bound to the clean source revision.
Without arguments the gate never compiles: it checks its prerequisites and that
manifest, then runs every unified_shape_ case of the real executable against
disposable state, refuses skipped, missing or failing cases, and retains the
revision, command, case results and log path in a private evidence record.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.human-unified-custody-shapes.v1'
MANIFEST = Path(os.environ.get('PAXEER_X_UNIFIED_SHAPES_MANIFEST',
                               ROOT / 'human/target/paxeer-x/human-unified-custody-shapes.json'))
EVIDENCE = Path(os.environ.get('PAXEER_X_UNIFIED_SHAPES_EVIDENCE', '/root/lx-fleet/logs/10.4/evidence'))
SOURCES = ['human/Cargo.toml', 'human/Cargo.lock', 'human/crates', 'agent/crates', 'human/schema/human-api',
           'tools/qualification/paxeer-x/human_unified_custody_shapes.py']
FILTER = 'unified_shape_'
CASES = [
    'unified_shape_planner_and_submit_classifier_share_one_contract',
    'unified_shape_refuses_unsupported_reordered_and_discontinuous_legs',
    'unified_shape_deposit_forward_keeps_one_parent_and_gates_forwarding_on_the_credit',
    'unified_shape_withdraw_to_wallet_starts_one_parent_bound_to_the_signed_debit',
    'unified_shape_transfer_then_withdraw_resumes_one_parent_and_claims_each_receipt_once',
]
STALE = 6


class Stale(RuntimeError):
    pass


def refuse(reason):
    raise RuntimeError(reason)


def require(condition, reason):
    if not condition:
        refuse(reason)


def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args], text=True).strip()


def source():
    if git('status', '--porcelain', '--untracked-files=no', '--', *SOURCES):
        refuse('the qualified sources have uncommitted changes')
    return git('rev-parse', 'HEAD')


def private_write(path, document):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = path.with_suffix('.next')
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(document, stream, indent=2, sort_keys=True)
        stream.write('\n')
    os.replace(temporary, path)


def build():
    revision = source()
    command = ['cargo', 'test', '--locked', '--manifest-path', 'human/Cargo.toml', '-p', 'layerx-human-service',
               '--test', 'intent_submit', '--no-run', '--message-format=json']
    result = subprocess.run(command, cwd=ROOT, text=True, stdout=subprocess.PIPE, stdin=subprocess.DEVNULL)
    found = []
    for line in result.stdout.splitlines():
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if item.get('reason') == 'compiler-message' and item['message'].get('rendered'):
            print(item['message']['rendered'], end='', file=sys.stderr)
        if (item.get('reason') == 'compiler-artifact' and item.get('executable')
                and item['target']['name'] == 'intent_submit' and item['profile']['test']):
            found.append(Path(item['executable']).resolve())
    if result.returncode:
        refuse('build failed (%d): %s' % (result.returncode, ' '.join(command)))
    require(len(found) == 1, 'the build did not produce exactly one intent_submit test executable')
    if source() != revision:
        refuse('sources changed during the build')
    private_write(MANIFEST, {'schema': SCHEMA, 'revision': revision, 'command': command,
                             'artifact': {'path': str(found[0]), 'sha256': digest(found[0])}})
    print('PAXEER_X_BUILD manifest=%s revision=%s' % (MANIFEST, revision))


def prerequisites():
    if not MANIFEST.is_file():
        raise Stale('no build manifest at %s; run this gate with --build first' % MANIFEST)
    info = MANIFEST.stat()
    require(info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'the build manifest must be private and owned by the gate user')
    data = json.loads(MANIFEST.read_text())
    revision = source()
    if data.get('schema') != SCHEMA or data.get('revision') != revision:
        raise Stale('the test executable was not built from this revision; run --build')
    path = Path(data['artifact']['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink() and os.access(path, os.X_OK),
            'the built test executable is missing')
    if digest(path) != data['artifact']['sha256']:
        raise Stale('the test executable changed since the build')
    for schema in ('intent.kvx', 'golden/intent.submit.response.json'):
        require((ROOT / 'human/schema/human-api' / schema).is_file(), 'missing contract: ' + schema)
    return revision, path


def qualify():
    revision, executable = prerequisites()
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    stamp = time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())
    log = EVIDENCE / ('unified-shapes-%s.log' % stamp)
    state = Path(tempfile.mkdtemp(prefix='unified-shapes-', dir=EVIDENCE))
    command = [str(executable), FILTER, '--test-threads=1']
    environment = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'TMPDIR': str(state), 'HOME': str(state),
                   'CARGO_MANIFEST_DIR': str(ROOT / 'human/crates/layerx-human-service'), 'RUST_BACKTRACE': '1'}
    descriptor = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        result = subprocess.run(command, cwd=ROOT / 'human/crates/layerx-human-service', env=environment,
                                stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT, timeout=1500)
    text = log.read_text()
    results = dict(re.findall(r'^test (\S+) \.\.\. (\w+)$', text, re.M))
    record = {'schema': SCHEMA + '.evidence', 'revision': revision, 'command': command,
              'exit': result.returncode, 'log': str(log), 'state': str(state), 'cases': results}
    private_write(EVIDENCE / ('unified-shapes-%s.json' % stamp), record)
    print('command=%s exit=%d log=%s' % (json.dumps(command), result.returncode, log), flush=True)
    missing = [case for case in CASES if case not in results]
    require(not missing, 'cases did not run: ' + ', '.join(missing))
    require(set(results) == set(CASES), 'unexpected cases ran: ' + ', '.join(sorted(set(results) - set(CASES))))
    failed = [case for case in CASES if results[case] != 'ok']
    require(not failed, 'cases did not pass: ' + ', '.join('%s=%s' % (case, results[case]) for case in failed))
    summary = re.findall(r'^test result: (\w+)\. (\d+) passed; (\d+) failed; (\d+) ignored;', text, re.M)
    require(summary == [('ok', str(len(CASES)), '0', '0')], 'the test summary is not %d passed with none ignored' % len(CASES))
    require(result.returncode == 0, 'the test executable exited %d' % result.returncode)
    print('human-unified-custody-shapes: PASS revision=%s cases=%d evidence=%s' % (revision, len(CASES), EVIDENCE))


def main():
    if sys.argv[1:] == ['--build']:
        build()
    elif not sys.argv[1:]:
        qualify()
    else:
        refuse('usage: human_unified_custody_shapes.py [--build]')


if __name__ == '__main__':
    try:
        main()
    except Stale as error:
        print('human-unified-custody-shapes: STALE ' + str(error), file=sys.stderr)
        sys.exit(STALE)
    except (OSError, RuntimeError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print('human-unified-custody-shapes: FAIL ' + str(error), file=sys.stderr)
        sys.exit(1)
