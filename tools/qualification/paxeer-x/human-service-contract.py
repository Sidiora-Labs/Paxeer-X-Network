#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
RECORD = Path('/root/lx-ops/paxeer-x-integration-2026-10-03/task-104.13.24-artifacts.json')
CLOCK_RECORD = RECORD.with_name('task-104.13.25-artifacts.json')
V1_GATE_DIGEST = 'c13cff140997c8cbde2a6602221ce92df850ab8736c47fbc9576f92678673549'
SOURCES = (
    'human/crates/layerx-human-service/src/server/agent_runtime.rs',
    'human/crates/layerx-human-service/src/server/production_rotation.rs',
    'human/crates/layerx-human-service/src/server/production_components.rs',
    'human/crates/layerx-human-service/src/server/http.rs',
    'human/crates/layerx-human-service/tests/server_browser_boundary.rs',
    'human/crates/layerx-human-service/Cargo.toml',
    'human/Cargo.toml', 'human/Cargo.lock',
    'tools/qualification/paxeer-x/human-service-contract.py',
)
CLOCK_SOURCES = (
    'platform/hosted/runtime-clock/src/main.rs',
    'platform/hosted/runtime-clock/src/server.rs',
    'platform/hosted/runtime-clock/Cargo.toml',
    'platform/Cargo.toml', 'platform/Cargo.lock',
    'agent/crates/layerx-types/src/clock.rs',
    'agent/crates/layerx-types/src/clock_protocol.rs',
    'agent/crates/layerx-client/src/runtime_clock.rs',
    'tools/runtime/run-with-clock.sh',
)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def source_hashes():
    return {source: digest(ROOT / source) for source in SOURCES}


def clock_source_hashes():
    return {source: digest(ROOT / source) for source in (*SOURCES, *CLOCK_SOURCES)}


def protected_record(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65536
            and info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
            'protected compiler artifact record required')
    return json.loads(path.read_text())


def check_artifacts(artifacts, names):
    require(set(artifacts) == names, 'closed artifact profile required')
    for artifact in artifacts.values():
        path = Path(artifact['path'])
        require(path.is_absolute() and path.is_file() and os.access(path, os.X_OK)
                and digest(path) == artifact['sha256'], 'compiler artifact was changed or unavailable')


def save_record(path, record):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        os.fchmod(output.fileno(), 0o600)
        json.dump(record, output, indent=2)
        output.write('\n')


def build():
    hashes = source_hashes()
    command = [os.environ.get('CARGO', '/root/.cargo/bin/cargo'), 'build', '--locked',
               '--manifest-path', str(ROOT / 'human/Cargo.toml'),
               '-p', 'layerx-human-service', '--bin', 'layerx-human-service',
               '--test', 'server_browser_boundary', '--message-format=json-render-diagnostics']
    env = dict(os.environ)
    env.setdefault('CARGO_TARGET_DIR', '/root/lx-target/human')
    env.setdefault('CARGO_BUILD_JOBS', '4')
    result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True,
                            timeout=1100, check=False)
    sys.stderr.buffer.write(result.stderr)
    artifacts = {}
    for line in result.stdout.splitlines():
        value = json.loads(line)
        if value.get('reason') == 'compiler-message':
            rendered = value.get('message', {}).get('rendered')
            if rendered:
                print(rendered, file=sys.stderr, end='')
        if value.get('reason') != 'compiler-artifact' or not value.get('executable'):
            continue
        target = value['target']
        if target['name'] == 'layerx-human-service' and 'bin' in target['kind']:
            artifacts['service'] = value['executable']
        elif target['name'] == 'server_browser_boundary' and 'test' in target['kind']:
            artifacts['boundary'] = value['executable']
    if result.returncode:
        return result.returncode
    require(hashes == source_hashes(), 'source changed during the declared build')
    require(set(artifacts) == {'service', 'boundary'}, 'actual service and boundary artifacts required')
    for path in artifacts.values():
        require(Path(path).is_file() and os.access(path, os.X_OK), 'compiler artifact unavailable')
    record = {'schema': 'paxeer-x.human-service-contract.v1', 'sources': hashes,
              'artifacts': {name: {'path': path, 'sha256': digest(Path(path))}
                            for name, path in artifacts.items()}}
    save_record(RECORD, record)
    print('compiled genuine service and focused browser-boundary target')
    return 0


def build_clock():
    inherited = protected_record(RECORD)
    require(inherited.get('schema') == 'paxeer-x.human-service-contract.v1',
            'genuine prior service compiler record required')
    expected = source_hashes()
    if inherited.get('sources', {}).get('tools/qualification/paxeer-x/human-service-contract.py') == V1_GATE_DIGEST:
        expected['tools/qualification/paxeer-x/human-service-contract.py'] = V1_GATE_DIGEST
    require(inherited.get('sources') == expected,
            'prior service and boundary sources changed; clock-only build cannot replace them')
    check_artifacts(inherited.get('artifacts', {}), {'service', 'boundary'})
    predecessor = digest(RECORD)
    hashes = clock_source_hashes()
    env = dict(os.environ)
    env.setdefault('CARGO_TARGET_DIR', '/root/lx-target/human')
    env.setdefault('CARGO_BUILD_JOBS', '4')
    command = [os.environ.get('CARGO', '/root/.cargo/bin/cargo'), 'build', '--locked',
               '--manifest-path', str(ROOT / 'platform/Cargo.toml'),
               '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock',
               '--message-format=json-render-diagnostics']
    result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True, timeout=540, check=False)
    sys.stderr.buffer.write(result.stderr)
    clock = None
    for line in result.stdout.splitlines():
        value = json.loads(line)
        if value.get('reason') == 'compiler-message':
            rendered = value.get('message', {}).get('rendered')
            if rendered:
                print(rendered, file=sys.stderr, end='')
        if value.get('reason') == 'compiler-artifact' and value.get('executable'):
            target = value['target']
            if target['name'] == 'layerx-runtime-clock' and 'bin' in target['kind']:
                require(clock is None, 'one genuine clock compiler artifact required')
                clock = value['executable']
    if result.returncode:
        return result.returncode
    require(hashes == clock_source_hashes() and predecessor == digest(RECORD),
            'source or predecessor record changed during clock build')
    check_artifacts(inherited['artifacts'], {'service', 'boundary'})
    require(clock and Path(clock).is_file() and os.access(clock, os.X_OK),
            'actual clock compiler artifact required')
    artifacts = dict(inherited['artifacts'])
    artifacts['clock'] = {'path': clock, 'sha256': digest(Path(clock))}
    save_record(CLOCK_RECORD, {'schema': 'paxeer-x.human-service-contract.v2',
                'sources': hashes, 'artifacts': artifacts,
                'predecessor_digest': predecessor})
    print('compiled genuine runtime clock; unchanged service and boundary artifacts retained')
    return 0


def verify():
    record = protected_record(CLOCK_RECORD)
    require(record.get('schema') == 'paxeer-x.human-service-contract.v2'
            and record.get('sources') == clock_source_hashes(),
            'clock-supervised artifact record does not bind current sources')
    require(record.get('predecessor_digest') == digest(RECORD), 'service predecessor record changed')
    check_artifacts(record.get('artifacts', {}), {'service', 'boundary', 'clock'})
    env = {key: value for key, value in os.environ.items()
           if not key.startswith('LAYERX_RUNTIME_CLOCK_')}
    env['LAYERX_RUNTIME_CLOCK_BIN'] = record['artifacts']['clock']['path']
    with tempfile.TemporaryDirectory(prefix='lx-human-contract-', dir='/tmp') as private:
        os.chmod(private, 0o700)
        env['LAYERX_RUNTIME_CLOCK_DIRECTORY'] = private
        result = subprocess.run(['sh', str(ROOT / 'tools/runtime/run-with-clock.sh'),
                                 record['artifacts']['boundary']['path'], '--test-threads=1'],
                                cwd=ROOT, env=env, capture_output=True, timeout=480, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and re.search(
        rb'test result: ok\. 6 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out',
        result.stdout), 'all six existing boundary cases must execute and pass')
    require(record['sources'] == clock_source_hashes(), 'source changed during the focused verification')
    check_artifacts(record['artifacts'], {'service', 'boundary', 'clock'})
    return 0


def main():
    parser = argparse.ArgumentParser()
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--build', action='store_true')
    modes.add_argument('--build-clock', action='store_true')
    args = parser.parse_args()
    if args.build:
        return build()
    return build_clock() if args.build_clock else verify()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print('human service contract refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
