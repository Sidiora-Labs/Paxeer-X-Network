#!/usr/bin/env python3
import argparse
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
definition = importlib.util.spec_from_file_location(
    'interface_producer', Path(__file__).with_name('program-interface-producer.py'))
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
require = producer.require


def run(command, log, environment):
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        result = subprocess.run(command, cwd=producer.ROOT, env=environment,
                                stdin=subprocess.DEVNULL, stdout=stream,
                                stderr=subprocess.STDOUT, timeout=900)
    output = log.read_text(errors='replace')
    print(output, end='', flush=True)
    require(result.returncode == 0, f'command exited {result.returncode}; log={log}')
    return output


def rust_count(output, required):
    passed = re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', output, re.M)
    require(len(passed) == len(set(passed)) and required <= set(passed),
            'required Rust cases did not pass: ' + ', '.join(sorted(required - set(passed))))
    counts = re.findall(
        r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(counts) == 1, 'expected exactly one Rust test summary')
    count, failed, ignored = map(int, counts[0])
    require(count == len(passed) and count >= len(required) and failed == 0 and ignored == 0,
            'Rust execution was empty, incomplete or skipped')
    return count


def verify(manifest):
    path = Path(manifest).absolute()
    directory = producer.private_directory(path.parent)
    value = producer.load_private(path)
    require(value.get('schema') == producer.SCHEMA, 'unsupported interface artifact schema')
    source = producer.identity()
    require(value.get('source') == source, 'artifacts do not bind this clean candidate')
    require(value.get('required_native_cases') == sorted(producer.NATIVE_CASES)
            and value.get('required_protocol_cases') == sorted(producer.PROTOCOL_CASES)
            and value.get('required_legacy_cases') == sorted(producer.LEGACY_CASES),
            'incomplete required case inventory')
    require(value.get('profiles') == {'dev_debug': 0, 'test_debug': 0, 'incremental': False}
            and value.get('features') == {'sandbox': ['host-ffi'], 'registry-tests': []},
            'unexpected build profiles or features')
    artifacts = value['artifacts']
    require(set(artifacts) == {'native', 'native-library', 'sandbox-staticlib',
                               'generated-header', 'protocol-tests', 'legacy-tests'},
            'incomplete artifact inventory')
    require(len(value['logs']) == 4, 'missing explicit build command logs')
    for saved in list(artifacts.values()) + value['logs']:
        require(producer.artifact(saved['path']) == saved,
                'artifact differs from producer: ' + saved['path'])
    for name in ('native', 'protocol-tests', 'legacy-tests'):
        require(os.access(artifacts[name]['path'], os.X_OK), 'artifact is not executable: ' + name)
    run_dir = Path(tempfile.mkdtemp(prefix='gate-', dir=directory))
    fixtures = run_dir / 'fixtures'
    fixtures.mkdir(mode=0o700)
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1',
                       PAXEER_X_INTERFACE_INPUTS=str(fixtures),
                       PAXEER_X_INTERFACE_FIXTURES=str(fixtures))
    binary = artifacts['protocol-tests']['path']
    count = rust_count(run([binary, '--exact', 'emit_native_inputs', '--test-threads=1'],
                          run_dir / 'inputs.log', environment), {'emit_native_inputs'})
    native = run([artifacts['native']['path'], '--output', str(fixtures)],
                 run_dir / 'native.log', environment)
    cases = re.findall(r'^INTERFACE_CASE name=([A-Za-z0-9-]+)$', native, re.M)
    require(len(cases) == len(set(cases)) and set(cases) == producer.NATIVE_CASES,
            'native lifecycle case inventory is incomplete or duplicated')
    summaries = re.findall(r'^INTERFACE_NATIVE tests=(\d+) skipped=(\d+)$', native, re.M)
    require(len(summaries) == 1 and tuple(map(int, summaries[0])) == (len(cases), 0),
            'native execution count is missing or skipped')
    count += len(cases)
    for case in ('abi1', 'abi2', 'abi2-dynamic', 'abi3', 'abi3-dynamic', 'abi4', 'abi4-dynamic'):
        for name in ('module.wasm', 'interface.bin', 'evidence.kvx'):
            producer.artifact(fixtures / case / name)
    count += rust_count(run([binary, '--skip', 'emit_native_inputs', '--test-threads=1'],
                            run_dir / 'protocol.log', environment), producer.PROTOCOL_CASES)
    count += rust_count(run([artifacts['legacy-tests']['path'],
                            'interface::conformance_vectors::', '--test-threads=1'],
                           run_dir / 'legacy.log', environment), producer.LEGACY_CASES)
    require(producer.identity() == source, 'source changed during gate')
    for saved in artifacts.values():
        require(producer.artifact(saved['path']) == saved, 'artifact changed during gate')
    producer.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.program-interface-result.v1', 'source': source,
        'manifest': producer.artifact(path), 'tests': count, 'skipped': 0,
        'native_cases': cases,
        'logs': [producer.artifact(log) for log in sorted(run_dir.glob('*.log'))],
        'fixtures': [producer.artifact(file) for file in sorted(fixtures.rglob('*'))
                     if file.is_file()],
    })
    print(f'PAXEER_X_GATE tests={count} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    args = parser.parse_args()
    try:
        verify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'interface gate refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
