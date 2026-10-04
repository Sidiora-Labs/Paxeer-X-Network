#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
BASE = ROOT / 'tools/qualification/paxeer-x'
definition = importlib.util.spec_from_file_location('terminal_v5_producer', BASE / 'program-interface-producer.py')
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
definition = importlib.util.spec_from_file_location('terminal_v5_consumer', BASE / 'program-interface.py')
consumer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(consumer)
SCHEMA = 'paxeer-x.program-terminal-v5-artifacts.v1'
PIN = 'b4f05aee172965774743f4cd7de4c3621c9e36fd77af7139aafec25eb3fb3360'
CASES = {f'abi{abi}-{outcome}' for abi in (3, 4)
    for outcome in ('success', 'failure', 'resource', 'callback', 'settlement')}
RUST_CASES = ('terminal::tests::execution_v5_closed_profile_and_abi_binding',
    'terminal::tests::current_candidate_v4_success_decodes_exactly')


def build(output):
    source = producer.identity()
    directory = producer.private_directory(output, create=True)
    producer.require(not any(directory.iterdir()), 'fresh empty build output required')
    native_dir = directory / 'native-build'
    generated = native_dir / 'generated'
    generated.mkdir(parents=True, mode=0o700)
    original = (ROOT / 'tests/programs/test_language_abi.c').read_text()
    entry = 'int main(int argc,char **argv)'
    renamed = 'int terminal_language_reference_main(int argc,char **argv)'
    producer.require(original.count(entry) == 1, 'exact native fixture entrypoint required')
    projection = generated / 'terminal-v5-language-fixture.inc'
    projection.write_text(renamed + ';\n' + original.replace(entry, renamed, 1))
    cargo_dir = Path(os.environ['CARGO_TARGET_DIR']).absolute()
    environment = dict(os.environ, CARGO_BUILD_JOBS='3', CARGO_PROFILE_DEV_DEBUG='0',
        CARGO_PROFILE_TEST_DEBUG='0', CARGO_INCREMENTAL='0', PYTHONDONTWRITEBYTECODE='1')
    library = native_dir / 'liblayerx.a'
    header = generated / 'lxp_checkpoint_settlement.h'
    sandbox = cargo_dir / 'debug/liblayerx_programs_sandbox.a'
    native = directory / 'program-terminal-v5-native'
    commands = [
        ['make', '--no-print-directory', '-j3', 'BUILD_DIR=' + str(native_dir),
            'LXP_REVISION=' + source['revision'], str(library), str(header)],
        ['cargo', '+1.91.1', 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
            '-p', 'layerx-programs-sandbox', '--features', 'host-ffi'],
        ['cc', '-Iinclude', '-Itests/programs', '-I' + str(generated), '-std=c17',
            '-pedantic', '-Werror', '-Wall', '-Wextra', '-Wconversion', '-Wshadow', '-Wvla',
            '-fno-strict-aliasing', '-ffp-contract=off', '-O2', 'tests/programs/test_terminal_v5.c',
            '-Wl,--start-group', str(library), str(sandbox), '-Wl,--end-group',
            '-lssl', '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm', '-o', str(native)],
        ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
            '-p', 'layerx-programs-runtime', '--lib', '--no-run', '--message-format=json'],
    ]
    logs = []
    for index, command in enumerate(commands):
        log = directory / f'build-{index}.log'
        producer.build_step(command, log, environment, ROOT)
        logs.append(log)
    binaries = []
    finished = False
    for line in logs[-1].read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if (event.get('reason') == 'compiler-artifact' and event.get('profile', {}).get('test')
                and event.get('executable') and event.get('target', {}).get('name') == 'layerx_programs_runtime'):
            binaries.append(event['executable'])
    producer.require(finished and len(binaries) == 1, 'actual runtime unit artifact required')
    producer.require(producer.identity() == source, 'source changed during declared build')
    producer.write_private(directory / 'manifest.json', {'schema': SCHEMA, 'source': source,
        'commands': commands, 'logs': [producer.artifact(log) for log in logs],
        'artifacts': {'native': producer.artifact(native), 'runtime': producer.artifact(binaries[0]),
            'sandbox': producer.artifact(sandbox), 'library': producer.artifact(library),
            'projection': producer.artifact(projection)}, 'cases': sorted(CASES),
        'rust_cases': list(RUST_CASES)})
    print(directory / 'manifest.json')


def verify(path):
    value = producer.load_private(Path(path).absolute())
    source = producer.identity()
    producer.require(value.get('schema') == SCHEMA and value.get('source') == source
        and value.get('cases') == sorted(CASES) and value.get('rust_cases') == list(RUST_CASES),
        'complete current source-bound artifact contract required')
    for artifact in list(value['artifacts'].values()) + value['logs']:
        producer.require(producer.artifact(artifact['path']) == artifact, 'artifact changed')
    directory = producer.private_directory(Path(path).absolute().parent)
    environment = dict(os.environ, PAXEER_X_MAINLINE=source['revision'])
    process = subprocess.run([value['artifacts']['native']['path'], '--dump-corpus'], cwd=ROOT,
        env=environment, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=600)
    producer.write_private(directory / 'native-result.json', {'exit_code': process.returncode,
        'stdout': process.stdout, 'stderr': process.stderr})
    try:
        corpus = json.loads(process.stdout)
    except json.JSONDecodeError as error:
        raise ValueError('native signed corpus was not emitted') from error
    producer.require(corpus.get('source_revision') == source['revision']
        and corpus.get('trusted_sequencer_public_key_hex') == PIN, 'native authority or revision differs')
    producer.write_private(directory / 'terminal-v5-corpus.json', corpus)
    print('PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS=' + str(directory / 'terminal-v5-corpus.json'))
    names = [case['name'] for case in corpus['cases']]
    if process.returncode == 78 or set(names) != CASES or len(names) != len(CASES) or corpus.get('missing'):
        print('genuine terminal-v5 native cases missing: ' + repr(corpus.get('missing')), file=sys.stderr)
        return 78
    producer.require(process.returncode == 0, 'native signed producer failed')
    for name in RUST_CASES:
        log = directory / (name.replace('::', '-') + '.log')
        output = consumer.run([value['artifacts']['runtime']['path'], '--exact', name,
            '--test-threads=1'], log, environment)
        producer.require(re.search(r'test result: ok\. 1 passed; 0 failed;', output), 'exact runtime case did not pass')
    producer.require(producer.identity() == source, 'source changed during qualification')
    print('PAXEER_X_GATE tests=12 skipped=0')
    return 0


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--output')
    parser.add_argument('--manifest', default=os.environ.get('PAXEER_X_PROGRAM_TERMINAL_V5_ARTIFACTS'))
    args = parser.parse_args()
    try:
        if args.build:
            producer.require(args.output, 'private build output required')
            build(args.output)
            return 0
        producer.require(args.manifest, 'explicit prebuilt terminal-v5 artifact manifest required')
        return verify(args.manifest)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('terminal-v5 qualification refused: ' + str(error), file=sys.stderr)
        return 78


if __name__ == '__main__':
    sys.exit(main())
