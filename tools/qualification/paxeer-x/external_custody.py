#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SCOPE = ['human/tools/copy-lint', 'human/apps/web/copy', 'human/apps/web/src',
         'human/apps/web/e2e/copy.test.ts', 'tools/paxeer-x/gates/104.26.2.sh',
         'tools/qualification/paxeer-x/external_custody.py']
DEADLINE = time.monotonic() + 1740
NODE = '/root/lx-toolchains/node24/bin/node'


def require(value, message):
    if not value:
        raise RuntimeError(message)


def git(*args):
    return subprocess.check_output(['git', '--no-optional-locks', '-C', str(ROOT), *args],
                                   text=True, timeout=30).strip()


def digest(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def private(path, directory=False):
    path = Path(path)
    info = path.lstat()
    require(path.is_absolute() and not path.is_symlink() and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077 and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)),
            'private owned evidence required')
    require(path != ROOT and ROOT not in path.resolve().parents, 'evidence must remain outside checkout')
    return path


def evidence():
    return private(os.environ['PAXEER_X_EVIDENCE_DIR'], directory=True)


def snapshot():
    names = subprocess.check_output(['git', '-C', str(ROOT), 'ls-files', '-z', '--', *SCOPE]).split(b'\0')
    paths = [os.fsdecode(name) for name in names if name]
    require(paths and not git('status', '--porcelain=v1', '--untracked-files=normal'), 'published clean source required')
    return {'revision': git('rev-parse', 'HEAD'), 'sources': {name: digest(ROOT / name) for name in paths}}


def execute(argv, name, environment=None):
    directory = evidence()
    log = directory / (name + '.log')
    require(not log.exists(), 'fresh command evidence required: ' + name)
    timeout = DEADLINE - time.monotonic()
    require(timeout > 0, 'bounded task deadline exceeded')
    with log.open('xb') as output:
        log.chmod(0o600)
        result = subprocess.run(argv, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                stdout=output, stderr=subprocess.STDOUT, timeout=timeout, check=False)
    print(json.dumps({'command': argv, 'exit_code': result.returncode, 'log_path': str(log)}), flush=True)
    require(result.returncode == 0, 'command failed; exact private log: ' + str(log))
    return log.read_text()


def build():
    directory = evidence()
    before = snapshot()
    destination = Path(os.environ['PAXEER_X_EXTERNAL_CUSTODY_BUILD_MANIFEST'])
    require(destination.is_absolute() and destination.parent == directory and not destination.exists(),
            'fresh build manifest in private evidence directory required')
    environment = dict(os.environ)
    environment['CARGO_BUILD_JOBS'] = '2'
    environment.setdefault('CARGO_TARGET_DIR', str(directory / 'copy-lint-target'))
    cargo = '/root/.cargo/bin/cargo'
    manifest = 'human/tools/copy-lint/Cargo.toml'
    output = execute([cargo, 'build', '--locked', '--manifest-path', manifest,
                      '--all-targets', '--message-format=json'], 'external-custody-build', environment)
    rows = []
    finished = []
    for line in output.splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished.append(event['success'])
        if event.get('reason') == 'compiler-artifact' and event.get('executable'):
            require(event['target']['name'] == 'layerx-human-copy-lint', 'unexpected compiled target')
            name = 'copy-lint-tests' if event['profile']['test'] else 'copy-lint'
            path = directory / name
            require(not path.exists(), 'fresh executable destination required')
            shutil.copyfile(event['executable'], path)
            path.chmod(0o700)
            rows.append({'kind': name, 'path': str(path), 'sha256': digest(path)})
    require(finished == [True] and {row['kind'] for row in rows} == {'copy-lint', 'copy-lint-tests'}
            and len(rows) == 2, 'complete successful standalone copy lint build required')
    require(snapshot() == before, 'source changed during build')
    destination.write_text(json.dumps({'schema': 'external-custody-build-v1', **before,
                                     'release_wave_clippy': 'UNRUN', 'artifacts': rows}, sort_keys=True))
    destination.chmod(0o600)
    print(json.dumps({'manifest': str(destination), 'exit_code': 0}))


def binary(row):
    path = private(row['path'])
    require(os.access(path, os.X_OK) and digest(path) == row['sha256'], 'actual executable identity mismatch')
    return str(path)


def verify_receipt_owner():
    supplied = os.environ.get('PAXEER_X_RAMP_BUILD_MANIFEST')
    require(supplied, 'prerequisite: actual ramp contract prebuilt manifest required; no hidden rebuild')
    manifest = json.loads(private(supplied).read_text())
    require(manifest.get('schema') == 'ramp-prebuilt-v1' and manifest.get('exit_code') == 0,
            'successful genuine ramp build required')
    owner_sources = manifest.get('sourcehashmap', {})
    require(owner_sources and all(digest(ROOT / relative) == expected for relative, expected in owner_sources.items()
                                 if relative.startswith('platform/ramps/toolkit/')),
            'actual receipt owner source differs from its build')
    for relative in ('platform/ramps/toolkit/src/lib.rs', 'platform/ramps/toolkit/src/journal.rs',
                     'platform/ramps/toolkit/tests/contracts.rs'):
        require(relative in owner_sources, 'complete real receipt owner source identity required')
    matches = [row for row in manifest.get('testbinaries', [])
               if row.get('package') == 'layerx-ramp-toolkit' and row.get('target') == 'contracts']
    require(len(matches) == 1, 'actual ramp contract test artifact required')
    row = matches[0]
    case = 'done_requires_both_verified_legs_and_external_label'
    require(case in row.get('expected_test_names', []), 'retained receipt/refusal contract absent')
    output = execute([binary(row), '--exact', case, '--test-threads=1'], 'external-custody-receipt-rule')
    require(re.search(r'test result: ok\. 1 passed; 0 failed; 0 ignored;', output),
            'real receipt refusal and custody presentation rule did not pass')
    return 1


def verify():
    before = snapshot()
    manifest = json.loads(private(os.environ['PAXEER_X_EXTERNAL_CUSTODY_BUILD_MANIFEST']).read_text())
    require(manifest.get('schema') == 'external-custody-build-v1' and manifest.get('release_wave_clippy') == 'UNRUN'
            and manifest.get('revision') == before['revision'] and manifest.get('sources') == before['sources'],
            'task qualification differs from successful frozen build')
    rows = manifest.get('artifacts', [])
    require(len(rows) == 2 and {row['kind'] for row in rows} == {'copy-lint', 'copy-lint-tests'},
            'complete genuine lint artifact inventory required')
    artifacts = {row['kind']: binary(row) for row in rows}
    output = execute([artifacts['copy-lint-tests'], '--test-threads=1'], 'external-custody-lint-tests')
    results = re.findall(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored; 0 measured; 0 filtered out;', output)
    require(len(results) == 1 and int(results[0]) > 0, 'full unfiltered copy lint corpus required')
    total = int(results[0])
    execute([artifacts['copy-lint'], 'human/apps/web'], 'external-custody-live-copy-lint')
    execute([NODE, 'human/apps/web/copy/generate-runtime.ts', '--check'], 'external-custody-runtime-copy')
    output = execute([NODE, '--test', '--test-reporter=tap', 'human/apps/web/e2e/copy.test.ts'],
                     'external-custody-ui-copy')
    require(re.search(r'^# tests 3$', output, re.M) and re.search(r'^# pass 3$', output, re.M)
            and re.search(r'^# skipped 0$', output, re.M), 'complete existing real UI copy corpus required')
    total += 5
    total += verify_receipt_owner()
    require(snapshot() == before, 'source changed during qualification')
    for row in rows:
        binary(row)
    print(f'PAXEER_X_GATE tests={total} skipped=0')


if __name__ == '__main__':
    try:
        require(sys.argv[1:] in ([], ['--build']), 'only one bounded build or focused verification supported')
        build() if sys.argv[1:] else verify()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error) if isinstance(error, RuntimeError) else type(error).__name__, file=sys.stderr)
        sys.exit(1)
