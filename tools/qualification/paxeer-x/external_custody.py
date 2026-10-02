#!/usr/bin/env python3
"""External-custody labelling qualification for task 104.26.2.

--build   one build phase: Clippy, prebuilt harnesses and binaries, locked web dependencies,
          then a private manifest at $PAXEER_X_EXTERNAL_CUSTODY_MANIFEST.
--verify  one qualification phase over that manifest: no compilation and no installs.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.external-custody-build.v1'
MANIFEST_ENV = 'PAXEER_X_EXTERNAL_CUSTODY_MANIFEST'
WEB = 'human/apps/web'
COPY_LINT = 'human/tools/copy-lint/Cargo.toml'
PLATFORM = 'platform/Cargo.toml'
TOOLKIT_DIR = 'platform/ramps/toolkit'
UI_DIST = WEB + '/packages/layerx-ui/dist'
FIXTURE = 'platform/hosted/gateway/tests/fixtures/maintained-authority.json'
SOURCES = ['human/apps/web/copy/catalog.ts', 'human/tools/copy-lint/src/main.rs',
           'tools/paxeer-x/gates/104.26.2.sh', 'tools/qualification/paxeer-x/external_custody.py']
LOCKS = ['human/tools/copy-lint/Cargo.lock', 'platform/Cargo.lock', WEB + '/package-lock.json']
NODE_FILES = 20
LINT_PASS = 'human copy catalog and source lint passed'
REQUIRED_LIB = ['clients::maintained_consumer_tests::',
                'clients::authority_shape_tests::real_authority_shape_selects_attachment_without_null_or_unknown_fallback',
                'clients::gateway_receipt_envelope_tests::real_gateway_envelope_carries_the_receipt_and_the_authority_it_verified']
REQUIRED_CONTRACTS = ['done_requires_both_verified_legs_and_external_label']
LIBTEST = re.compile(r'^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored; '
                     r'(\d+) measured; (\d+) filtered out', re.M)
NODE = re.compile(r'^(?:ℹ|#) (tests|pass|fail|cancelled|skipped|todo) (\d+)\s*$', re.M)


class Fail(Exception):
    pass


def now():
    return datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def tree_digest(directory, skip=()):
    base = ROOT / directory
    files = sorted(p for p in base.rglob('*')
                   if p.is_file() and not set(p.relative_to(base).parts) & set(skip))
    if not files:
        raise Fail('empty directory ' + directory)
    lines = [str(p.relative_to(base)) + ' ' + digest(p) for p in files]
    return hashlib.sha256('\n'.join(lines).encode()).hexdigest()


def private_dir(path):
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat()
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise Fail('directory is not private: ' + str(path))
    if path == ROOT or ROOT in path.resolve().parents:
        raise Fail('private directory is inside the repository: ' + str(path))
    return path


def git(*args):
    out = subprocess.run(['git', '--no-optional-locks', '-C', str(ROOT), *args],
                         stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60)
    if out.returncode:
        raise Fail('git ' + ' '.join(args) + ' exited ' + str(out.returncode))
    return out.stdout.strip()


def source():
    if git('status', '--porcelain=v1', '--untracked-files=normal'):
        raise Fail('source tree is not clean')
    return {'revision': git('rev-parse', '--verify', 'HEAD^{commit}'),
            'tree': git('rev-parse', '--verify', 'HEAD^{tree}')}


def run(logs, name, argv, cwd=ROOT, env=None):
    out = subprocess.run(argv, cwd=cwd, stdin=subprocess.DEVNULL, capture_output=True, env=env)
    record = {'argv': [str(a) for a in argv], 'cwd': str(cwd), 'exit': out.returncode}
    for stream, data in (('stdout', out.stdout), ('stderr', out.stderr)):
        path = logs / (name + '.' + stream)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(data)
        record[stream] = {'path': str(path), 'sha256': hashlib.sha256(data).hexdigest()}
    print(name + ': exit ' + str(out.returncode) + ' ' + ' '.join(record['argv']) + ' log ' + str(logs / name) + '.*',
          flush=True)
    if out.returncode:
        raise Fail(name + ' failed: exit ' + str(out.returncode) + ': ' + ' '.join(record['argv'])
                   + ' (logs ' + str(logs / name) + '.stdout/.stderr)')
    return record, out.stdout.decode('utf-8', 'replace'), out.stderr.decode('utf-8', 'replace')


def version(logs, name, argv):
    return run(logs, 'version-' + name, argv)[1].strip()


def artifacts(stdout, manifest, kind, name, test):
    manifest = str((ROOT / manifest).resolve())
    found, finished = [], None
    for line in stdout.splitlines():
        if not line.startswith('{'):
            continue
        record = json.loads(line)
        if record.get('reason') == 'build-finished':
            finished = record.get('success')
        if (record.get('reason') == 'compiler-artifact' and record.get('manifest_path') == manifest
                and record['target']['kind'] == [kind] and record['target']['name'] == name
                and record['profile']['test'] is test and record.get('executable')):
            found.append(record['executable'])
    if finished is not True:
        raise Fail('cargo did not report a successful build for ' + name)
    found = sorted(set(found))
    if len(found) != 1:
        raise Fail('expected one executable for ' + kind + ' ' + name + ', found ' + str(len(found)))
    return {'path': found[0], 'sha256': digest(found[0])}


def ui_sources():
    return tree_digest(WEB + '/packages/layerx-ui', ('dist', 'node_modules'))


def build():
    target = os.environ.get(MANIFEST_ENV)
    if not target or not os.path.isabs(target):
        raise Fail(MANIFEST_ENV + ' must name a private absolute path')
    target = Path(target)
    if target.exists():
        raise Fail('manifest already exists; refusing to overwrite ' + str(target))
    private_dir(target.parent)
    logs = private_dir(target.parent / ('external-custody-build-' + now()))
    src = source()
    for path in [FIXTURE, *SOURCES, *LOCKS]:
        if not (ROOT / path).is_file():
            raise Fail('missing required file ' + path)
    node = version(logs, 'node', ['node', '--version'])
    if int(node.lstrip('v').split('.')[0]) < 24:
        raise Fail('Node >= 24 required, found ' + node)
    tools = {'rustc': version(logs, 'rustc', ['rustc', '--version']),
             'cargo': version(logs, 'cargo', ['cargo', '--version']),
             'clippy': version(logs, 'clippy', ['cargo', 'clippy', '--version']),
             'node': node, 'npm': version(logs, 'npm', ['npm', '--version'])}
    clippy = run(logs, 'clippy', ['cargo', 'clippy', '--manifest-path', COPY_LINT, '--locked',
                                  '--all-targets', '--', '-D', 'warnings'])[0]
    out = run(logs, 'copy-lint-test-build', ['cargo', 'test', '--manifest-path', COPY_LINT, '--locked',
                                             '--no-run', '--message-format=json'])[1]
    lint_test = artifacts(out, COPY_LINT, 'bin', 'layerx-human-copy-lint', True)
    out = run(logs, 'copy-lint-build', ['cargo', 'build', '--manifest-path', COPY_LINT, '--locked',
                                        '--message-format=json'])[1]
    lint_bin = artifacts(out, COPY_LINT, 'bin', 'layerx-human-copy-lint', False)
    out = run(logs, 'toolkit-test-build', ['cargo', 'test', '--manifest-path', PLATFORM, '--locked',
                                           '-p', 'layerx-ramp-toolkit', '--lib', '--test', 'contracts',
                                           '--no-run', '--message-format=json'])[1]
    toolkit = TOOLKIT_DIR + '/Cargo.toml'
    toolkit_lib = artifacts(out, toolkit, 'lib', 'layerx_ramp_toolkit', True)
    contracts = artifacts(out, toolkit, 'test', 'contracts', True)
    out = run(logs, 'runtime-clock-build', ['cargo', 'build', '--manifest-path', PLATFORM, '--locked',
                                            '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock',
                                            '--message-format=json'])[1]
    clock = clock_artifact(out)
    npm = {}
    if not (ROOT / WEB / 'node_modules').is_dir():
        npm['ci'] = run(logs, 'npm-ci', ['npm', '--prefix', WEB, 'ci'])[0]
    npm['ls'] = run(logs, 'npm-ls', ['npm', '--prefix', WEB, 'ls', '--all'])[0]
    hidden = ROOT / WEB / 'node_modules/.package-lock.json'
    if not hidden.is_file():
        raise Fail('installed dependency record missing: ' + str(hidden.relative_to(ROOT)))
    ui = {'built': False, 'sources_sha256': ui_sources()}
    if not (ROOT / UI_DIST).is_dir():
        ui['build'] = run(logs, 'build-ui', ['npm', '--prefix', WEB, 'run', 'build:ui'])[0]
        ui['built'] = True
    ui['dist_sha256'] = tree_digest(UI_DIST)
    if source() != src:
        raise Fail('source changed or became dirty during the build phase')
    manifest = {
        'schema': SCHEMA, 'root': str(ROOT), 'source': src, 'created_at': now(), 'logs': str(logs),
        'sources': {p: digest(ROOT / p) for p in SOURCES},
        'locks': {p: digest(ROOT / p) for p in LOCKS},
        'fixtures': {FIXTURE: digest(ROOT / FIXTURE)},
        'node_modules': {'record': str(hidden.relative_to(ROOT)), 'sha256': digest(hidden), **npm},
        'ui': ui, 'toolchain': tools, 'clippy': clippy,
        'executables': {'copy_lint_tests': lint_test, 'copy_lint': lint_bin, 'toolkit_lib_tests': toolkit_lib,
                        'toolkit_contracts_tests': contracts, 'runtime_clock': clock},
    }
    staging = target.with_name(target.name + '.partial')
    fd = os.open(staging, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as handle:
        json.dump(manifest, handle, indent=2, sort_keys=True)
        handle.write('\n')
        handle.flush()
        os.fsync(handle.fileno())
    os.link(staging, target)
    os.unlink(staging)
    print('manifest: ' + str(target) + ' sha256 ' + digest(target))


def clock_artifact(stdout):
    found = set()
    for line in stdout.splitlines():
        if line.startswith('{'):
            record = json.loads(line)
            if (record.get('reason') == 'compiler-artifact' and record['target']['name'] == 'layerx-runtime-clock'
                    and record['target']['kind'] == ['bin'] and record['profile']['test'] is False
                    and record.get('executable')):
                found.add(record['executable'])
    if len(found) != 1:
        raise Fail('expected one layerx-runtime-clock executable, found ' + str(len(found)))
    path = found.pop()
    return {'path': path, 'sha256': digest(path)}


def load():
    raw = os.environ.get(MANIFEST_ENV)
    if not raw:
        raise Fail(MANIFEST_ENV + ' is not set; build evidence missing')
    fd = os.open(raw, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'r', encoding='utf-8') as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise Fail('manifest is not a private regular file')
        data = json.load(handle)
    if data.get('schema') != SCHEMA:
        raise Fail('unsupported manifest schema')
    return data


def stale(data):
    if data['root'] != str(ROOT):
        raise Fail('manifest was built in ' + data['root'] + ', not this checkout')
    if source() != data['source']:
        raise Fail('manifest does not bind the current HEAD and tree')
    for group in ('sources', 'locks', 'fixtures'):
        if set(data[group]) != set({'sources': SOURCES, 'locks': LOCKS, 'fixtures': [FIXTURE]}[group]):
            raise Fail('manifest ' + group + ' coverage differs from the declared set')
        for path, value in data[group].items():
            if not (ROOT / path).is_file() or digest(ROOT / path) != value:
                raise Fail('stale or missing ' + path)
    clippy = data['clippy']
    if clippy['exit'] != 0 or clippy['argv'][:2] != ['cargo', 'clippy'] or '-D' not in clippy['argv']:
        raise Fail('Clippy record is not a successful -D warnings run')
    for stream in ('stdout', 'stderr'):
        if digest(clippy[stream]['path']) != clippy[stream]['sha256']:
            raise Fail('Clippy log changed since the build')
    for name, exe in data['executables'].items():
        if not os.access(exe['path'], os.X_OK) or digest(exe['path']) != exe['sha256']:
            raise Fail('prebuilt executable ' + name + ' is missing or changed')
    if digest(ROOT / data['node_modules']['record']) != data['node_modules']['sha256']:
        raise Fail('installed web dependencies changed since the build')
    if tree_digest(UI_DIST) != data['ui']['dist_sha256'] or ui_sources() != data['ui']['sources_sha256']:
        raise Fail('UI package dist or sources changed since the build')


def inventory(logs, name, exe, cwd, env):
    out = run(logs, name + '-list', [exe, '--list'], cwd=cwd, env=env)[1]
    tests = [line[:-len(': test')] for line in out.splitlines() if line.endswith(': test')]
    if not tests:
        raise Fail(name + ' compiled inventory is empty')
    return tests


def libtest(logs, name, argv, listed, cwd, env):
    out = run(logs, name, argv, cwd=cwd, env=env)[1]
    results = LIBTEST.findall(out)
    if len(results) != 1:
        raise Fail(name + ' printed ' + str(len(results)) + ' libtest result lines')
    state, passed, failed, ignored, measured, filtered = results[0][0], *map(int, results[0][1:])
    if state != 'ok' or failed or ignored or measured or filtered or passed != len(listed):
        raise Fail(name + ' result ' + state + ' passed=' + str(passed) + ' failed=' + str(failed)
                   + ' ignored=' + str(ignored) + ' filtered=' + str(filtered) + ' listed=' + str(len(listed)))
    return {'listed': len(listed), 'passed': passed, 'ignored': ignored}


def require(name, listed, wanted):
    for entry in wanted:
        hit = [t for t in listed if t.startswith(entry)] if entry.endswith('::') else [t for t in listed if t == entry]
        if not hit:
            raise Fail(name + ' inventory lacks ' + entry)


def verify():
    data = load()
    evidence = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if not evidence:
        raise Fail('PAXEER_X_EVIDENCE_DIR is not set')
    logs = private_dir(Path(evidence).resolve() / ('external-custody-' + data['source']['revision'][:12] + '-' + now()))
    stale(data)
    exe = {k: v['path'] for k, v in data['executables'].items()}
    env = {k: v for k, v in os.environ.items() if k != 'LAYERX_RUNTIME_CLOCK_SOCKET'}
    env['LAYERX_RUNTIME_CLOCK_BIN'] = exe['runtime_clock']
    summary = {'manifest': os.environ[MANIFEST_ENV], 'source': data['source'], 'checks': {}}

    listed = inventory(logs, 'copy-lint-tests', exe['copy_lint_tests'], ROOT, env)
    summary['checks']['copy_lint_tests'] = libtest(
        logs, 'copy-lint-tests', ['sh', 'tools/runtime/run-with-clock.sh', exe['copy_lint_tests']], listed, ROOT, env)

    _, out, err = run(logs, 'copy-lint', [exe['copy_lint'], WEB], env=env)
    if out.strip() != LINT_PASS or err.strip():
        raise Fail('copy linter did not report a clean pass over ' + WEB)
    summary['checks']['copy_lint'] = {'passed': True}

    script = json.loads((ROOT / WEB / 'package.json').read_text(encoding='utf-8'))['scripts']['test']
    files = re.findall(r'e2e/\S+', script)
    if len(files) != NODE_FILES or not all((ROOT / WEB / f).is_file() for f in files):
        raise Fail('web test script does not name the ' + str(NODE_FILES) + ' existing Node test files')
    run(logs, 'web-typecheck', ['npm', '--prefix', WEB, 'run', 'typecheck'], env=env)
    summary['checks']['web_typecheck'] = {'passed': True}
    out = run(logs, 'web-test', ['npm', '--prefix', WEB, 'test'], env=env)[1]
    counts = {}
    for key, value in NODE.findall(out):
        counts[key] = int(value)
    if set(counts) != {'tests', 'pass', 'fail', 'cancelled', 'skipped', 'todo'}:
        raise Fail('node --test summary is incomplete: ' + json.dumps(counts))
    if (counts['fail'] or counts['cancelled'] or counts['skipped'] or counts['todo']
            or counts['pass'] != counts['tests'] or counts['tests'] < 1):
        raise Fail('node --test result ' + json.dumps(counts))
    summary['checks']['web_test'] = dict(counts, files=len(files))

    toolkit = ROOT / TOOLKIT_DIR
    if not (ROOT / FIXTURE).is_file():
        raise Fail('missing fixture ' + FIXTURE)
    listed = inventory(logs, 'toolkit-lib', exe['toolkit_lib_tests'], toolkit, env)
    require('toolkit lib', listed, REQUIRED_LIB)
    summary['checks']['toolkit_lib'] = libtest(logs, 'toolkit-lib', [exe['toolkit_lib_tests']], listed, toolkit, env)
    listed = inventory(logs, 'toolkit-contracts', exe['toolkit_contracts_tests'], toolkit, env)
    require('toolkit contracts', listed, REQUIRED_CONTRACTS)
    summary['checks']['toolkit_contracts'] = libtest(
        logs, 'toolkit-contracts', [exe['toolkit_contracts_tests']], listed, toolkit, env)

    if source() != data['source']:
        raise Fail('source changed or became dirty during qualification')
    checks = summary['checks']
    tests = (checks['copy_lint_tests']['passed'] + checks['web_test']['pass']
             + checks['toolkit_lib']['passed'] + checks['toolkit_contracts']['passed'])
    skipped = (checks['copy_lint_tests']['ignored'] + checks['web_test']['skipped'] + checks['web_test']['todo']
               + checks['toolkit_lib']['ignored'] + checks['toolkit_contracts']['ignored'])
    summary.update(tests=tests, skipped=skipped)
    fd = os.open(logs / 'summary.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as handle:
        json.dump(summary, handle, indent=2, sort_keys=True)
        handle.write('\n')
    print('evidence: ' + str(logs))
    print('PAXEER_X_GATE tests=%d skipped=%d' % (tests, skipped))


def main(argv):
    if argv not in (['--build'], ['--verify']):
        print('usage: external_custody.py --build | --verify', file=sys.stderr)
        return 2
    try:
        build() if argv == ['--build'] else verify()
    except (Fail, OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as error:
        print('external-custody: refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
