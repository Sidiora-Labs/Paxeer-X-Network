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

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
RECORD = Path('/root/lx-ops/paxeer-x-integration-2026-10-03/task-104.13.24-artifacts.json')
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


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def source_hashes():
    return {source: digest(ROOT / source) for source in SOURCES}


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
    RECORD.parent.mkdir(parents=True, exist_ok=True)
    descriptor = os.open(RECORD, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        os.fchmod(output.fileno(), 0o600)
        json.dump(record, output, indent=2)
        output.write('\n')
    print('compiled genuine service and focused browser-boundary target')
    return 0


def verify():
    info = RECORD.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 65536
            and info.st_mode & 0o077 == 0, 'protected compiler artifact record required')
    record = json.loads(RECORD.read_text())
    require(record.get('schema') == 'paxeer-x.human-service-contract.v1'
            and record.get('sources') == source_hashes(), 'artifact record does not bind current sources')
    require(set(record.get('artifacts', {})) == {'service', 'boundary'}, 'closed artifact profile required')
    for artifact in record['artifacts'].values():
        path = Path(artifact['path'])
        require(path.is_file() and os.access(path, os.X_OK)
                and digest(path) == artifact['sha256'], 'compiler artifact was changed or unavailable')
    result = subprocess.run([record['artifacts']['boundary']['path'], '--test-threads=1'],
                            cwd=ROOT, capture_output=True, timeout=480, check=False)
    sys.stdout.buffer.write(result.stdout)
    sys.stderr.buffer.write(result.stderr)
    require(result.returncode == 0 and re.search(
        rb'test result: ok\. 6 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out',
        result.stdout), 'all six existing boundary cases must execute and pass')
    require(record['sources'] == source_hashes(), 'source changed during the focused verification')
    return 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    args = parser.parse_args()
    return build() if args.build else verify()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print('human service contract refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
