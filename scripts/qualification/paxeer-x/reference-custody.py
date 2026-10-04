#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
TASK = '104.30.7'
SCHEMA = 'paxeer-x.reference-custody-build.v1'
GUESTS = ('escrow', 'vault')
spec = importlib.util.spec_from_file_location('spending_evidence',
    ROOT / 'tools/qualification/paxeer-x/program_spend_composition.py')
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)
evidence.ROOT = ROOT


def source():
    record = evidence.source()
    paths = evidence.capture(['git', 'ls-files', '--',
        'scripts/qualification/paxeer-x/reference-custody.py',
        'tests/programs/test_reference_custody.c',
        'tools/paxeer-x/gates/104.30.7.sh',
        'platform/docs/content/guide/programs.md', 'programs/porting']).splitlines()
    for path in paths:
        evidence.require(not any(p.startswith('.env') for p in Path(path).parts),
            'credential input is forbidden')
        record['inputs'][path] = evidence.digest(ROOT / path)
    return record


def build(args, directory):
    candidate = source()
    run = directory / ('reference-custody-build-' + str(time.time_ns()))
    run.mkdir(mode=0o700)
    target = Path(os.environ.get('CARGO_TARGET_DIR', str(run / 'cargo'))).absolute()
    environment = dict(os.environ, CARGO_TARGET_DIR=str(target), CARGO_BUILD_JOBS='4',
        CARGO_INCREMENTAL='0', CARGO_PROFILE_DEV_DEBUG='0', PYTHONDONTWRITEBYTECODE='1')
    deadline = time.monotonic() + 1800
    record = {'schema': SCHEMA, 'source': candidate, 'completed': False,
        'toolchain': evidence.capture(['rustc', '+1.91.1', '-vV']),
        'steps': [], 'artifacts': {}}
    try:
        for guest in GUESTS:
            command = ['cargo', '+1.91.1', 'build', '--locked', '--release',
                '--target', 'wasm32-unknown-unknown', '--manifest-path',
                'programs/sdk/rust/examples/' + guest + '/Cargo.toml']
            record['steps'].append(evidence.launch(command, run, environment,
                deadline, guest))
            artifact = target / ('wasm32-unknown-unknown/release/layerx_reference_' + guest + '.wasm')
            record['artifacts'][guest] = evidence.artifact(artifact)
        record['steps'].append(evidence.launch(['cargo', '+1.91.1', 'build', '--locked',
            '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-programs-sandbox',
            '--features', 'host-ffi'], run, environment, deadline, 'sandbox'))
        sandbox = target / 'debug/liblayerx_programs_sandbox.a'
        record['artifacts']['sandbox'] = evidence.artifact(sandbox)
        record['steps'].append(evidence.launch(['cargo', '+1.91.1', 'build', '--locked',
            '--manifest-path', 'programs/Cargo.toml', '-p', 'layerx-program-lint',
            '--bin', 'layerx-program-lint'], run, environment, deadline, 'lint-compiler'))
        record['artifacts']['lint'] = evidence.artifact(target / 'debug/layerx-program-lint')
        native_dir = run / 'native'
        generated = native_dir / 'generated'
        generated.mkdir(parents=True, mode=0o700)
        fixture = (ROOT / 'tests/programs/test_spend_capability_composition.c').read_text()
        original = 'int main(int argc, char **argv)'
        replacement = 'int custody_composition_reference_main(int argc, char **argv)'
        evidence.require(fixture.count(original) == 1,
            'actual fixture entrypoint projection must match exactly once')
        projection = generated / 'reference-custody-fixture-base.inc'
        fd = os.open(projection, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            stream.write(replacement + ';\n' + fixture.replace(original, replacement, 1))
        record['artifacts']['fixture_projection'] = evidence.artifact(projection)
        record['steps'].append(evidence.launch(['make', '-j4',
            'BUILD_DIR=' + str(native_dir), 'CC=' + args.cc,
            'LXP_REVISION=' + candidate['revision'],
            'PAXEER_REFERENCE_RUNTIME_LIB=' + str(sandbox),
            'PROGRAMS_RUNTIME_LIB=' + str(sandbox),
            'PROGRAMS_TARGET_DIR=' + str(target),
            str(native_dir / 'tests/programs_reference_custody')], run,
            environment, deadline, 'native'))
        record['artifacts']['native'] = evidence.artifact(native_dir / 'tests/programs_reference_custody')
        record['artifacts']['library'] = evidence.artifact(native_dir / 'liblayerx.a')
        evidence.require(source() == candidate, 'source changed during declared build')
        record['completed'] = True
    finally:
        error = sys.exc_info()[1]
        if isinstance(error, evidence.GateProcessError):
            record['steps'].append(error.step)
        evidence.write_private(directory / ('task-' + TASK + '-build.json'), record)


def qualify(directory):
    manifest = json.loads((directory / ('task-' + TASK + '-build.json')).read_text())
    candidate = source()
    evidence.require(manifest['schema'] == SCHEMA and manifest['completed'] is True
        and manifest['source'] == candidate, 'complete source-bound custody build required')
    artifacts = manifest['artifacts']
    native = evidence.checked(artifacts['native'], elf=True)
    lint = evidence.checked(artifacts['lint'], elf=True)
    for artifact in artifacts.values():
        evidence.checked(artifact)
    run = directory / ('reference-custody-verify-' + str(time.time_ns()))
    run.mkdir(mode=0o700)
    result = {'schema': 'paxeer-x.reference-custody-result.v1',
        'source': candidate, 'steps': [], 'passed': False,
        'unqualified_criteria': ['escrow-successful-release', 'escrow-successful-refund',
            'escrow-duplicate-settlement', 'authenticated-source-verification']}
    deadline = time.monotonic() + 1800
    try:
        for guest in GUESTS:
            result['steps'].append(evidence.launch([str(lint),
                str(ROOT / ('programs/sdk/rust/examples/' + guest)),
                artifacts[guest]['path']], run, dict(os.environ), deadline,
                guest + '-determinism'))
        output = run / 'native-evidence'
        output.mkdir(mode=0o700)
        result['steps'].append(evidence.launch([str(native), artifacts['escrow']['path'],
            artifacts['vault']['path'], str(output)], run, dict(os.environ),
            deadline, 'actual-native-custody'))
        result['native_evidence'] = [evidence.artifact(p) for p in sorted(output.iterdir())
            if p.is_file()]
        evidence.require(result['native_evidence'], 'actual custody receipt evidence missing')
        evidence.require(source() == candidate, 'source changed during qualification')
        result['native_passed'] = True
    finally:
        error = sys.exc_info()[1]
        if isinstance(error, evidence.GateProcessError):
            result['steps'].append(error.step)
        output = run / 'native-evidence'
        if output.is_dir():
            result['native_evidence'] = [evidence.artifact(p) for p in sorted(output.iterdir())
                if p.is_file()]
        evidence.write_private(directory / ('task-' + TASK + '-result.json'), result)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--cc', default='cc')
    args = parser.parse_args()
    try:
        directory = evidence.private_directory()
        if args.build:
            build(args, directory)
        else:
            qualify(directory)
            print('custody native evidence recorded; registered source verification remains unqualified',
                file=sys.stderr)
            return 2
    except (OSError, ValueError, KeyError, TypeError) as error:
        print('reference custody qualification refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
