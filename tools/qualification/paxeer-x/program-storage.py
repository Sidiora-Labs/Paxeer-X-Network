#!/usr/bin/env python3
import argparse
import importlib.util
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
definition = importlib.util.spec_from_file_location(
    'storage_producer', Path(__file__).with_name('program-storage-producer.py'))
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
support = producer.support
require = producer.require


def rust_count(output, required):
    passed = re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', output, re.M)
    require(len(passed) == len(set(passed)) and set(required) <= set(passed),
            'required cases did not pass: ' + ', '.join(sorted(set(required) - set(passed))))
    summaries = re.findall(
        r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(summaries) == 1, 'expected one Rust test summary')
    count, failed, ignored = map(int, summaries[0])
    require(count == len(passed) and count >= len(required) and count > 0
            and failed == 0 and ignored == 0, 'empty, incomplete or skipped runtime cases')
    return count


def balance_prerequisite(evidence, source, run_dir, deadline):
    path = Path(evidence).absolute()
    directory = support.private_directory(path.parent)
    record = support.load_private(path)
    require(record.get('schema') == 'paxeer-x.gate-evidence.v1'
            and record.get('selector') == '104.31.2'
            and record.get('source') == source, 'balance evidence does not bind this candidate')
    require(record.get('result') == 'pass' and record.get('credited') is True
            and record.get('exit_code') == 0 and record.get('skipped') == 0
            and record.get('tests') == 12 and record.get('count_reported') is True
            and record.get('command', {}).get('executed') is True,
            'balance authority prerequisite is unqualified')
    require((directory / '.dispatch-key').is_file(), 'missing original evidence authentication key')
    saved = support.artifact(path)
    environment = dict(os.environ, PAXEER_X_EVIDENCE_DIR=str(directory),
                       PYTHONDONTWRITEBYTECODE='1')
    check_log = producer.run(
        [str(producer.ROOT / 'tools/paxeer-x/verify-task.sh'), '--check', str(path)],
        run_dir / 'balance-evidence-check.log', environment, deadline)
    require(support.artifact(path) == saved, 'balance evidence changed during authentication')
    log = Path(record['log']['path'])
    require(log.parent == directory and support.digest(log) == record['log']['sha256'],
            'balance evidence log changed')
    output = log.read_text(errors='replace')
    cases = re.findall(r'^BALANCE_READ_CASE (BAL-[0-9]{2}) ok$', output, re.M)
    required = {f'BAL-{index:02d}' for index in range(1, 10)}
    require(len(cases) == len(required) and set(cases) == required,
            'native receipt-bound balance cases are incomplete')
    require(re.findall(r'^BALANCE_READ_COMPLETE cases=(\d+) skipped=(\d+)$', output, re.M)
            == [('9', '0')], 'native receipt authority evidence is partial or skipped')
    return {'selector': '104.31.2', 'evidence': saved, 'log': support.artifact(log),
            'authentication_log': check_log, 'tests_not_reexecuted': 12}


def verify(manifest, balance_evidence):
    deadline = time.monotonic() + 1800
    path = Path(manifest).absolute()
    directory = support.private_directory(path.parent)
    value = support.load_private(path)
    manifest_record = support.artifact(path)
    source = support.identity()
    require(value.get('schema') == producer.SCHEMA, 'unsupported storage artifact schema')
    require(value.get('source') == source, 'artifacts do not bind this clean candidate')
    require(value.get('producer_root') == str(producer.ROOT)
            and value.get('cwd') == str(producer.ROOT / 'programs')
            and value.get('command') == producer.command(), 'unexpected source or build command')
    require(value.get('profiles') == {'dev_debug': 0, 'test_debug': 0, 'incremental': False}
            and value.get('features') == [], 'unexpected build profiles or features')
    require(value.get('suites') == producer.SUITES, 'changed required case inventory')
    artifacts = value['artifacts']
    require(set(artifacts) == set(producer.TARGETS) | {'layerx_programs_runtime'},
            'incomplete runtime artifact inventory')
    for saved in list(artifacts.values()) + [value['build_log']]:
        artifact_path = Path(saved['path'])
        require(not artifact_path.is_symlink() and artifact_path.parent == directory,
                'artifact is not directly inside the private producer directory')
        info = artifact_path.stat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'artifact is not private and caller-owned')
        require(support.artifact(artifact_path) == saved,
                'artifact differs from producer: ' + str(artifact_path))
    require(set(producer.executables(Path(value['build_log']['path']))) == set(artifacts),
            'producer log lacks successful expected compilation')
    for name, saved in artifacts.items():
        require(os.access(saved['path'], os.X_OK), 'artifact is not executable: ' + name)
    run_dir = Path(tempfile.mkdtemp(prefix='gate-', dir=directory))
    prerequisite = balance_prerequisite(balance_evidence, source, run_dir, deadline)
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    records = []
    count = 0
    for name, suite in sorted(producer.SUITES.items()):
        argv = [artifacts[suite['binary']]['path']]
        if suite['filter'] is not None:
            argv.append(suite['filter'])
        argv.extend(['--test-threads=1', '--color=never'])
        log = run_dir / (name + '.log')
        saved_log = producer.run(argv, log, environment, deadline)
        output = log.read_text(errors='replace')
        print(output, end='', flush=True)
        tests = rust_count(output, suite['required'])
        count += tests
        records.append({'suite': name, 'command': argv, 'tests': tests,
                        'exit_code': 0, 'log': saved_log})
    require(support.identity() == source, 'source changed during gate')
    for saved in list(artifacts.values()) + [value['build_log']]:
        require(support.artifact(saved['path']) == saved, 'artifact changed during gate')
    require(support.artifact(path) == manifest_record, 'producer manifest changed during gate')
    for name in ('evidence', 'log'):
        saved = prerequisite[name]
        require(support.artifact(saved['path']) == saved, 'balance prerequisite changed during gate')
    support.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.program-storage-result.v1', 'source': source,
        'manifest': manifest_record, 'tests': count, 'skipped': 0,
        'records': records, 'balance_prerequisite': prerequisite,
    })
    print(f'PAXEER_X_GATE tests={count} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    parser.add_argument('--balance-evidence', required=True)
    args = parser.parse_args()
    try:
        verify(args.manifest, args.balance_evidence)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'storage gate refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
