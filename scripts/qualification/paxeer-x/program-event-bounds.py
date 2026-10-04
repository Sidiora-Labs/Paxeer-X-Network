#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
BASE = ROOT / 'tools/qualification/paxeer-x'
definition = importlib.util.spec_from_file_location('event_bounds_producer', BASE / 'program-interface-producer.py')
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
definition = importlib.util.spec_from_file_location('event_bounds_consumer', BASE / 'program-interface.py')
consumer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(consumer)
require = producer.require
SCHEMA = 'paxeer-x.program-event-bounds-artifacts.v1'
NATIVE_CASES = {'event-count64', 'event-count65', 'event-byte-exact', 'event-byte-exhaustion', 'capability-transport65535'}
RUST_CASES = {
    'layerx_programs_runtime': {
        'abi::event_tests::event_count_boundary_refuses_before_a_sixty_fifth_stage',
        'abi::event_tests::event_bytes_are_charged_before_staging_and_exhaust_atomically',
        'abi::event_tests::maximum_largest_grant_set_fits_the_transport_ceiling',
        'abi::event_tests::inherited_event_count_refuses_before_metering_or_staging',
        'abi::event_tests::event_exhaustion_and_invalid_lengths_never_stage_or_charge_bytes',
    },
    'layerx_program_sdk': {
        'payments::transport_boundary_tests::maximum_program_spend_set_fits_the_canonical_transport',
        'capability::parity_vectors::mixed_v1_encoding_matches_shared_sdk_fixture',
    },
}


def build(output):
    source = producer.identity()
    directory = producer.private_directory(output, create=True)
    require(not any(directory.iterdir()), 'fresh empty build output required')
    native_dir = directory / 'native-build'
    cargo_dir = Path(os.environ['CARGO_TARGET_DIR']).absolute()
    environment = dict(os.environ, CARGO_BUILD_JOBS='3', CARGO_PROFILE_DEV_DEBUG='0',
        CARGO_PROFILE_TEST_DEBUG='0', CARGO_INCREMENTAL='0', PYTHONDONTWRITEBYTECODE='1')
    library = native_dir / 'liblayerx.a'
    header = native_dir / 'generated/lxp_checkpoint_settlement.h'
    staticlib = cargo_dir / 'debug/liblayerx_programs_sandbox.a'
    native = directory / 'program-event-bounds-native'
    commands = [
        ['make', '--no-print-directory', '-j3', 'BUILD_DIR=' + str(native_dir),
         'LXP_REVISION=' + source['revision'], str(library), str(header)],
        ['cargo', '+1.91.1', 'build', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
         '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
        ['cc', '-Iinclude', '-I' + str(header.parent), '-std=c17', '-pedantic', '-Werror',
         '-Wall', '-Wextra', '-Wconversion', '-Wshadow', '-Wvla', '-fno-strict-aliasing',
         '-ffp-contract=off', '-O2', 'tests/programs/test_call_activity.c', '-Wl,--start-group',
         str(library), str(staticlib), '-Wl,--end-group', '-lcrypto', '-lsqlite3', '-pthread',
         '-ldl', '-lm', '-o', str(native)],
        ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
         '-p', 'layerx-programs-runtime', '-p', 'layerx-program-sdk', '--lib', '--no-run', '--message-format=json'],
    ]
    producer.write_private(directory / 'build-inputs.json', {'schema': SCHEMA, 'source': source,
        'commands': commands, 'native_cases': sorted(NATIVE_CASES),
        'rust_cases': {name: sorted(cases) for name, cases in RUST_CASES.items()}})
    logs = []
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
        name = event.get('target', {}).get('name')
        if (event.get('reason') == 'compiler-artifact' and event.get('profile', {}).get('test')
                and event.get('executable') and name in RUST_CASES):
            require(name not in binaries and not event.get('features'), 'duplicate or feature-substituted test binary')
            binaries[name] = event['executable']
    require(finished and set(binaries) == set(RUST_CASES), 'missing actual compiled Rust executables')
    artifacts = {'native': producer.artifact(native), 'native-library': producer.artifact(library),
        'generated-header': producer.artifact(header), 'sandbox-staticlib': producer.artifact(staticlib)}
    for name, binary in binaries.items():
        target = directory / name
        shutil.copyfile(binary, target)
        target.chmod(0o700)
        artifacts[name] = producer.artifact(target)
    require(producer.identity() == source, 'source changed during build')
    producer.write_private(directory / 'manifest.json', {'schema': SCHEMA, 'source': source,
        'commands': commands, 'artifacts': artifacts, 'logs': [producer.artifact(log) for log in logs],
        'native_cases': sorted(NATIVE_CASES), 'rust_cases': {name: sorted(cases) for name, cases in RUST_CASES.items()}})
    print('EVENT_BOUNDS_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def verify(path):
    path = Path(path).absolute()
    directory = producer.private_directory(path.parent)
    value = producer.load_private(path)
    source = producer.identity()
    require(value.get('schema') == SCHEMA and value.get('source') == source, 'artifacts do not bind clean source')
    require(value.get('native_cases') == sorted(NATIVE_CASES)
        and value.get('rust_cases') == {name: sorted(cases) for name, cases in RUST_CASES.items()}, 'case inventory differs')
    artifacts = value['artifacts']
    require(set(artifacts) == {'native', 'native-library', 'generated-header', 'sandbox-staticlib', *RUST_CASES},
        'artifact inventory differs')
    require(len(value['logs']) == 4, 'missing explicit build logs')
    saved = list(artifacts.values()) + value['logs']
    for artifact in saved:
        require(producer.artifact(artifact['path']) == artifact, 'build artifact changed')
    run_dir = Path(tempfile.mkdtemp(prefix='event-bounds-gate-', dir=directory))
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    logs = []
    log = run_dir / 'native.log'
    output = consumer.run([artifacts['native']['path'], '--event-capability-bounds'], log, environment)
    markers = re.findall(r'^PROGRAM_EVENT_BOUNDS_CASE name=([a-z0-9-]+)$', output, re.M)
    require(len(markers) == len(NATIVE_CASES) and set(markers) == NATIVE_CASES, 'native case inventory incomplete or duplicated')
    logs.append(log)
    count = len(markers)
    for name, cases in RUST_CASES.items():
        for case in sorted(cases):
            log = run_dir / (name + '-' + case.replace('::', '-') + '.log')
            output = consumer.run([artifacts[name]['path'], '--exact', case, '--test-threads=1'], log, environment)
            count += consumer.rust_count(output, {case})
            logs.append(log)
    require(producer.identity() == source, 'source changed during gate')
    for artifact in saved:
        require(producer.artifact(artifact['path']) == artifact, 'artifact changed during gate')
    producer.write_private(run_dir / 'result.json', {'source': source, 'tests': count, 'skipped': 0,
        'manifest': producer.artifact(path), 'logs': [producer.artifact(log) for log in logs]})
    print(f'PAXEER_X_GATE tests={count} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest='mode', required=True)
    sub.add_parser('build').add_argument('--output', required=True)
    sub.add_parser('verify').add_argument('--manifest', required=True)
    args = parser.parse_args()
    try:
        build(args.output) if args.mode == 'build' else verify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('Event bounds refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
