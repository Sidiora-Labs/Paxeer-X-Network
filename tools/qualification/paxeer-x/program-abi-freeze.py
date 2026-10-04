#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
BASE = Path(__file__).parent
spec = importlib.util.spec_from_file_location('freeze_producer', BASE / 'program-interface-producer.py')
producer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(producer)
spec = importlib.util.spec_from_file_location('freeze_consumer', BASE / 'program-interface.py')
consumer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(consumer)
ROOT = producer.ROOT
require = producer.require
SCHEMA = 'paxeer-x.program-abi-freeze-artifacts.v1'
CASES = {
    'layerx_programs_runtime': {
        'qualification::tests::dispatcher_keeps_v1_replay_stable_after_a_real_v2_revision_is_present',
        'qualification::tests::dispatcher_refuses_unknown_runtime_and_unsupported_abi',
    },
    'isolation': {
        'abi_v1_manifest_matches_typed_declarations_and_golden',
        'abi_v2_manifest_matches_typed_declarations_and_golden',
    },
    'abi_linker': {
        'every_frozen_import_instantiates_against_its_revision_linker',
        'wrong_signatures_duplicates_and_extra_imports_are_rejected',
    },
    'replay': {
        'governed_fee_history_reprices_each_recorded_version_exactly',
        'recorded_v1_replays_identically_after_a_simulated_upgrade',
        'mixed_v1_v2_history_selects_each_recorded_abi_and_fee_schedule',
        'unknown_runtime_and_abi_artifacts_are_preserved_without_execution',
        'mixed_history_preserves_recorded_abi_and_refuses_version_substitution',
    },
}


def inventory():
    paths = [ROOT / 'programs/abi-frozen.sha256']
    paths += [ROOT / f'programs/tests/vectors/abi-v{version}.hex' for version in range(1, 5)]
    return [producer.artifact(path) for path in paths]


def build(output):
    source = producer.identity()
    directory = producer.private_directory(output, create=True)
    require(not any(directory.iterdir()), 'fresh empty build output required')
    environment = dict(os.environ, CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
                       CARGO_INCREMENTAL='0', PYTHONDONTWRITEBYTECODE='1')
    commands = [
        [sys.executable, 'scripts/programs/generate-abi-vectors.py', '--check'],
        ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
         '-p', 'layerx-programs-runtime', '--lib', '--test', 'replay', '--test', 'isolation',
         '--test', 'abi_linker', '--no-run', '--message-format=json'],
    ]
    logs = []
    frozen = inventory()
    producer.write_private(directory / 'build-inputs.json', {
        'schema': SCHEMA, 'source': source, 'commands': commands, 'frozen': frozen,
        'required_cases': {name: sorted(cases) for name, cases in CASES.items()},
    })
    for index, command in enumerate(commands):
        log = directory / f'build-{index + 1}.log'
        producer.build_step(command, log, environment, ROOT)
        logs.append(log)
    binaries = {}
    finished = False
    for line in logs[-1].read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if (event.get('reason') == 'compiler-artifact' and event.get('profile', {}).get('test')
                and event.get('executable')):
            name = event.get('target', {}).get('name')
            if name in CASES:
                require(name not in binaries and not event.get('features'),
                        'duplicate or feature-substituted executable')
                binaries[name] = event['executable']
    require(finished and set(binaries) == set(CASES), 'missing compiled ABI freeze executables')
    artifacts = {}
    for name, binary in binaries.items():
        target = directory / name
        shutil.copyfile(binary, target)
        target.chmod(0o700)
        artifacts[name] = producer.artifact(target)
    require(producer.identity() == source and inventory() == frozen, 'build source or vectors changed')
    producer.write_private(directory / 'manifest.json', {
        'schema': SCHEMA, 'source': source, 'commands': commands,
        'required_cases': {name: sorted(cases) for name, cases in CASES.items()},
        'artifacts': artifacts, 'frozen': frozen, 'logs': [producer.artifact(log) for log in logs],
        'abi_drift_check_exit': 0,
    })
    print('ABI_FREEZE_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def verify(manifest):
    path = Path(manifest).absolute()
    directory = producer.private_directory(path.parent)
    value = producer.load_private(path)
    source = producer.identity()
    require(value.get('schema') == SCHEMA and value.get('source') == source,
            'ABI freeze artifacts do not bind the clean candidate')
    require(value.get('required_cases') == {name: sorted(cases) for name, cases in CASES.items()},
            'ABI freeze case inventory differs')
    require(value.get('abi_drift_check_exit') == 0 and len(value['logs']) == 2,
            'frozen vector build check did not pass')
    require(value.get('commands') == [
        [sys.executable, 'scripts/programs/generate-abi-vectors.py', '--check'],
        ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
         '-p', 'layerx-programs-runtime', '--lib', '--test', 'replay', '--test', 'isolation',
         '--test', 'abi_linker', '--no-run', '--message-format=json'],
    ], 'build command identity differs')
    artifacts = value['artifacts']
    require(set(artifacts) == set(CASES) and value['frozen'] == inventory(),
            'executable or frozen-vector inventory differs')
    saved = list(artifacts.values()) + value['logs'] + value['frozen']
    for artifact in saved:
        require(producer.artifact(artifact['path']) == artifact, 'artifact differs from build')
    require(value['logs'][0]['path'] and '\nBUILD_EXIT 0\n' in Path(value['logs'][0]['path']).read_text(),
            'ABI drift build log has no successful command exit')
    run_dir = Path(tempfile.mkdtemp(prefix='abi-freeze-gate-', dir=directory))
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    count = 0
    logs = []
    for name, cases in CASES.items():
        require(os.access(artifacts[name]['path'], os.X_OK), 'artifact is not executable')
        for case in sorted(cases):
            log = run_dir / (name + '-' + case.replace('::', '-') + '.log')
            output = consumer.run([artifacts[name]['path'], '--exact', case, '--test-threads=1'],
                                  log, environment)
            count += consumer.rust_count(output, {case})
            logs.append(log)
    require(producer.identity() == source, 'source changed during gate')
    for artifact in saved:
        require(producer.artifact(artifact['path']) == artifact, 'artifact changed during gate')
    producer.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.program-abi-freeze-result.v1', 'source': source,
        'manifest': producer.artifact(path), 'tests': count, 'skipped': 0,
        'logs': [producer.artifact(log) for log in logs], 'abi_drift_build_check': value['logs'][0],
    })
    print(f'PAXEER_X_GATE tests={count} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest='mode', required=True)
    sub.add_parser('build').add_argument('--output', required=True)
    sub.add_parser('verify').add_argument('--manifest', required=True)
    args = parser.parse_args()
    try:
        if args.mode == 'build':
            build(args.output)
        else:
            verify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('ABI freeze refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
