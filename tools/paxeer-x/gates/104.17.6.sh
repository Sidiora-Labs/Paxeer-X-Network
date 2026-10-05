#!/usr/bin/env bash
set -euo pipefail
exec python3 - "$@" <<'GATE'
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys

os.umask(0o077)
ROOT = Path.cwd()
OUT = Path(os.environ.get('PAXEER_X_INSTALL_ARTIFACTS',
                          '/root/lx-ops/paxeer-x-integration-2026-10-03/task-104.17.6-artifacts'))
CARGO = os.environ.get('PAXEER_X_CARGO', '/root/.cargo/bin/cargo')
TARGET = Path(os.environ.get('CARGO_TARGET_DIR', '/root/lx-target/platform-cli-install'))
PACKAGE = 'layerx-platform-cli'
SOURCES = (b'platform/', b'agent/crates/', b'programs/', b'tools/paxeer-x/gates/104.17.6.sh')
JOURNEY = ROOT / 'platform/cli/tests/install-journey.sh'
BOOTSTRAP = ROOT / 'platform/cli/tests/clean-bootstrap.sh'


class Missing(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def git(*args):
    return subprocess.check_output(['git', '--no-optional-locks', *args], cwd=ROOT, text=True).strip()


def sources():
    paths = subprocess.check_output(['git', 'ls-files', '-z'], cwd=ROOT).split(b'\0')
    return {os.fsdecode(path): sha(ROOT / os.fsdecode(path)) for path in paths
            if path and path.startswith(SOURCES) and (ROOT / os.fsdecode(path)).is_file()
            and not any(part.startswith(b'.env') for part in path.split(b'/'))}


def run(argv, label, timeout=900, env=None, cwd=None, expect=0):
    path = OUT / (label + '.log')
    with path.open('w') as log:
        result = subprocess.run(argv, cwd=cwd or ROOT, stdin=subprocess.DEVNULL, stdout=log,
                                stderr=subprocess.STDOUT, timeout=timeout, env=env)
    print('exit=' + str(result.returncode) + ' log=' + str(path), flush=True)
    if expect == 0:
        require(result.returncode == 0, label + ' exit ' + str(result.returncode) + '; ' + str(path))
    else:
        require(result.returncode != 0, label + ' unexpectedly succeeded; ' + str(path))
    return path.read_text(errors='replace')


def compile_artifacts(arguments, label, env):
    raw = run([CARGO, *arguments, '--offline', '--locked', '--manifest-path', 'platform/Cargo.toml',
               '-p', PACKAGE, '--message-format=json'], label, timeout=3600, env=env)
    rows = []
    for line in raw.splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        if (row.get('reason') == 'compiler-artifact' and row.get('executable')
                and PACKAGE in str(row.get('package_id'))):
            rows.append({'path': row['executable'], 'sha256': sha(row['executable']),
                         'name': row['target']['name'], 'kind': row['target']['kind'],
                         'test': bool(row['profile']['test'])})
    return rows


def build():
    require(not git('status', '--porcelain=v1'), 'published immutable source required')
    OUT.mkdir(mode=0o700, parents=True, exist_ok=True)
    before = sources()
    base = dict(os.environ, PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''))
    tests = compile_artifacts(['test', '--no-run', '--features', 'test-credential-store'], 'cargo-test-build',
                              dict(base, CARGO_TARGET_DIR=str(TARGET / 'test')))
    production = compile_artifacts(['build', '--bin', 'layerx'], 'cargo-production-build',
                                   dict(base, CARGO_TARGET_DIR=str(TARGET / 'production')))
    corpus = [row for row in tests if row['test']]
    helper = [row for row in tests if not row['test'] and row['name'] == 'layerx']
    binary = [row for row in production if not row['test'] and row['name'] == 'layerx']
    names = {row['name'] for row in corpus}
    require({'install', 'mcp_daemon_bound', 'layerx'} <= names, 'install and daemon-bound corpora required')
    require(len(helper) == 1 and len(binary) == 1, 'test-store and production layerx executables required')
    require(before == sources(), 'source changed during compilation')
    manifest = {'revision': git('rev-parse', 'HEAD'), 'sources': before, 'corpus': corpus,
                'test_store_binary': helper[0], 'production_binary': binary[0]}
    (OUT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print('manifest=' + str(OUT / 'manifest.json'), flush=True)


def private_directory(path):
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'artifact directory must be private and owned by the caller')


def journey_prerequisites(binary):
    contract = re.findall(r'^: "\$\{([A-Z0-9_]+):\?', JOURNEY.read_text(), re.M)
    require(contract, 'install journey prerequisite contract is missing')
    missing = [name for name in contract if name != 'LAYERX_BIN' and not os.environ.get(name)]
    missing += ['executable ' + tool for tool in ('jq', 'curl', 'openssl') if not shutil.which(tool)]
    try:
        import cryptography  # noqa: F401
    except ImportError:
        missing.append('python cryptography')
    if missing:
        raise Missing('genuine install journey fixture: ' + ', '.join(missing))
    origin = os.environ['LAYERX_GATEWAY_URL']
    require(origin.startswith('https://'), 'install journey requires the hosted HTTPS gateway')
    return dict(os.environ, LAYERX_BIN=binary)


def verify():
    private_directory(OUT)
    manifest = json.loads((OUT / 'manifest.json').read_text())
    require(manifest['revision'] == git('rev-parse', 'HEAD') and manifest['sources'] == sources(),
            'compiled artifacts do not belong to this published source')
    executables = [*manifest['corpus'], manifest['test_store_binary'], manifest['production_binary']]
    for row in executables:
        require(Path(row['path']).is_file() and sha(row['path']) == row['sha256'], 'compiled artifact changed')
    count = 0
    for script in (JOURNEY, BOOTSTRAP):
        run(['bash', '-n', str(script)], 'syntax-' + script.name)
        count += 1
    cli = ROOT / 'platform/cli'
    environment = dict(os.environ, CARGO_MANIFEST_DIR=str(cli))
    for name in ('LAYERX_CONFIG', 'LAYERX_INSTALL_ROOT', 'LAYERX_CREDENTIAL_STORE', 'LAYERX_GATEWAY_KEY_ID'):
        environment.pop(name, None)
    for row in manifest['corpus']:
        label = row['name'] + '-' + Path(row['path']).name
        inventory = run([row['path'], '--list'], label + '-inventory', env=environment, cwd=cli)
        declared = len(re.findall(r'^.+: test$', inventory, re.M))
        output = run([row['path'], '--test-threads=1'], label, env=environment, cwd=cli)
        result = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output)
        require(len(result) == 1 and int(result[0][0]) == declared and result[0][1:] == ('0', '0'),
                'retained CLI test corpus omitted, failed or ignored a case: ' + label)
        count += declared
    production = manifest['production_binary']['path']
    scratch = OUT / 'credential-refusal'
    shutil.rmtree(scratch, ignore_errors=True)
    scratch.mkdir(mode=0o700)
    refusal = run([production, '--json', 'key', 'create', 'production-refusal'], 'production-credential-refusal',
                  env=dict(environment, LAYERX_CONFIG=str(scratch / 'config.json'), LAYERX_CREDENTIAL_STORE='mock'),
                  expect=1)
    require('credential store override mock is unavailable in this binary' in refusal
            and not (scratch / 'config.json').exists(), 'production binary accepted a test credential store')
    count += 1
    run(['bash', str(BOOTSTRAP)], 'clean-bootstrap', timeout=1200, env=dict(environment, LAYERX_BIN=production))
    count += 1
    output = run(['bash', str(JOURNEY)], 'install-journey', timeout=1500, env=journey_prerequisites(production))
    records = re.findall(r'^INSTALL_JOURNEY_RESULT (.+)$', output, re.M)
    require(len(records) == 1, 'install journey produced no result record')
    record = json.loads(records[0])
    require(isinstance(record.get('tests'), int) and record['tests'] > 0 and record.get('skipped') == 0
            and re.fullmatch('[0-9a-f]{64}', record.get('mcp_activity', ''))
            and re.fullmatch('[0-9a-f]{64}', record.get('a2a_activity', '')),
            'install journey did not complete both receipt-backed payments')
    count += record['tests']
    print('PAXEER_X_GATE tests=' + str(count) + ' skipped=0', flush=True)


try:
    require(sys.argv[1:] in ([], ['--build']), 'unknown gate argument')
    build() if sys.argv[1:] == ['--build'] else verify()
except Missing as missing:
    print('missing prerequisite: ' + str(missing), file=sys.stderr)
    raise SystemExit(78)
except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
    print('install gate: ' + str(error), file=sys.stderr)
    raise SystemExit(1)
GATE
