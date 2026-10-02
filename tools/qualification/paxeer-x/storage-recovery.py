#!/usr/bin/env python3
import argparse
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
definition = importlib.util.spec_from_file_location(
    'storage_recovery_producer', Path(__file__).with_name('storage-recovery-producer.py'))
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
require = producer.require


def evidence_directory():
    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(raw, 'PAXEER_X_EVIDENCE_DIR is not set')
    return producer.private_directory(raw)


def run(name, command, run_dir):
    log = run_dir / (name + '.log')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('COMMAND ' + ' '.join(command) + '\n').encode())
        stream.flush()
        try:
            exit_code = subprocess.run(command, cwd=producer.ROOT, stdin=subprocess.DEVNULL,
                                       stdout=stream, stderr=subprocess.STDOUT,
                                       env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'),
                                       timeout=900).returncode
        except subprocess.TimeoutExpired:
            exit_code = 124
        stream.write(f'\nEXIT {exit_code}\n'.encode())
    print(log.read_text(errors='replace'), end='', flush=True)
    return {'name': name, 'command': command, 'exit': exit_code,
            'log': str(log), 'log_sha256': producer.digest(log)}


def verify(manifest):
    path = Path(manifest).absolute()
    directory = producer.private_directory(path.parent)
    value = producer.load_private(path)
    require(value.get('schema') == producer.SCHEMA, 'unsupported storage recovery artifact schema')
    source = producer.identity()
    require({key: value.get(key) for key in source} == source,
            'artifacts do not bind this clean candidate')
    require(value.get('exit') == 0, 'producer build did not succeed')
    command = value.get('command')
    require(isinstance(command, list) and command[:2] == ['make', '-j3']
            and command[-3:] == ['build/tests/' + name for name in producer.TESTS],
            'producer command is not the declared make invocation')
    require(value.get('sources') == producer.sources(),
            'scoped sources differ from the producer record')
    require(value.get('log') == producer.LOG
            and producer.digest(directory / producer.LOG) == value.get('log_sha256'),
            'build log differs from the producer record')
    artifacts = value.get('artifacts')
    require(isinstance(artifacts, dict) and set(artifacts) == set(producer.TESTS),
            'incomplete artifact inventory')
    for name in producer.TESTS:
        require(artifacts[name] == producer.artifact(directory, 'bin/' + name),
                'artifact differs from producer: ' + name)
        require(os.access(directory / 'bin' / name, os.X_OK), 'artifact is not executable: ' + name)
    run_dir = Path(tempfile.mkdtemp(prefix='storage-recovery-', dir=evidence_directory()))
    suites = [('order', ['sh', producer.ORDER_CHECK])]
    suites += [(name, [str(directory / 'bin' / name)]) for name in producer.TESTS]
    results = []
    for name, suite in suites:
        results.append(run(name, suite, run_dir))
    passed = [result['name'] for result in results if result['exit'] == 0]
    failed = [result['name'] + '=' + str(result['exit']) for result in results
              if result['exit'] != 0]
    unchanged = producer.identity() == source and producer.sources() == value['sources'] and all(
        artifacts[name] == producer.artifact(directory, 'bin/' + name) for name in producer.TESTS)
    producer.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.storage-recovery-result.v1', 'source': source,
        'manifest': str(path), 'manifest_sha256': producer.digest(path),
        'suites': results, 'tests': len(passed), 'skipped': 0, 'failed': failed,
        'unchanged': unchanged})
    print('STORAGE_RECOVERY_EVIDENCE ' + str(run_dir), flush=True)
    require(not failed, 'suites failed: ' + ', '.join(failed))
    require(unchanged, 'source or artifacts changed during gate')
    require(len(passed) == len(suites) and passed, 'suite execution was empty or incomplete')
    print(f'PAXEER_X_GATE tests={len(passed)} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    args = parser.parse_args()
    try:
        verify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'storage recovery gate refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
