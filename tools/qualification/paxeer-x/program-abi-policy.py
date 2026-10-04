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
BASE = Path(__file__).parent
spec = importlib.util.spec_from_file_location('abi_interface_producer', BASE / 'program-interface-producer.py')
producer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(producer)
spec = importlib.util.spec_from_file_location('abi_interface_gate', BASE / 'program-interface.py')
interface = importlib.util.module_from_spec(spec)
spec.loader.exec_module(interface)
require = producer.require
ROOT = producer.ROOT
SCHEMA = 'paxeer-x.program-abi-policy-artifacts.v1'
RUNTIME_CASES = {
    'abi_upgrades_are_monotonic_and_historical_versions_remain_admitted',
    'every_supported_abi_transition_preserves_lifecycle_history',
}
REGISTRY_CASES = {
    'registry_replay_refuses_downgrade_without_rewriting_historical_version',
    'registry_replay_applies_every_supported_abi_transition_without_rewriting_history',
    'registry_resolves_historical_and_latest_code_only_from_protocol_evidence',
    'historical_deployments_replay_across_an_explicit_sequencer_rotation',
    'forged_batch_journal_and_stale_evidence_never_create_deployment_authority',
}
INTERFACE_CASES = {
    'interface::conformance_vectors::central_abi_policy_admits_every_supported_version_and_refuses_unknown',
    'interface::conformance_vectors::upgrade_is_monotonic_across_supported_abi_versions',
    'interface::conformance_vectors::every_supported_abi_transition_binds_the_recorded_interface_version',
    'interface::conformance_vectors::every_abi_refuses_substituted_domains_and_noncanonical_capabilities',
}
RUNTIME_POLICY_CASES = {
    'abi_policy::tests::every_admitted_abi_version_carries_one_capability_encoding',
    'abi_policy::tests::a_validated_revision_and_its_recorded_version_select_the_same_encoding',
}


def executables(log, required):
    found = {}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if (event.get('reason') == 'compiler-artifact'
                and event.get('profile', {}).get('test') is True and event.get('executable')):
            name = event.get('target', {}).get('name')
            if name in required:
                require(name not in found and not event.get('features'),
                        'duplicate or feature-substituted Rust executable')
                found[name] = Path(event['executable'])
    require(finished and set(found) == required, 'missing successfully compiled Rust executables')
    return found


def build(output):
    source = producer.identity()
    directory = producer.private_directory(output, create=True)
    require(not any(directory.iterdir()), 'fresh empty build output is required')
    environment = dict(os.environ, CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
                       CARGO_INCREMENTAL='0', PYTHONDONTWRITEBYTECODE='1')
    cargo_dir = Path(os.environ.get('CARGO_TARGET_DIR', str(directory / 'cargo-target'))).absolute()
    environment['CARGO_TARGET_DIR'] = str(cargo_dir)
    interface_dir = directory / 'interface'
    commands = [[sys.executable, str(BASE / 'program-interface-producer.py'),
                 '--output', str(interface_dir)]]
    logs = [directory / 'build-interface.log']
    producer.build_step(commands[0], logs[0], environment, ROOT)
    inherited_path = interface_dir / 'manifest.json'
    inherited = producer.load_private(inherited_path)
    require(inherited['source'] == source, 'interface build source mismatch')
    artifacts = inherited['artifacts']
    native = directory / 'registration'
    compile_command = [
        'cc', '-Iinclude', '-I' + str(Path(artifacts['generated-header']['path']).parent),
        '-std=c17', '-pedantic', '-Werror', '-Wall', '-Wextra', '-Wconversion',
        '-Wshadow', '-Wvla', '-fno-strict-aliasing', '-ffp-contract=off', '-O2',
        'tests/programs/test_registration.c', '-Wl,--start-group',
        artifacts['native-library']['path'], artifacts['sandbox-staticlib']['path'],
        '-Wl,--end-group', '-lssl', '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm',
        '-o', str(native),
    ]
    cargo = ['cargo', '+1.91.1', 'test', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml')]
    commands.extend([
        compile_command,
        cargo + ['-p', 'layerx-programs-runtime', '--lib', '--test', 'lifecycle',
                 '--no-run', '--message-format=json'],
        cargo + ['-p', 'layerx-programs-registry', '--test', 'registry',
                 '--no-run', '--message-format=json'],
    ])
    for index, command in enumerate(commands[1:], start=1):
        log = directory / f'build-{index}.log'
        producer.build_step(command, log, environment, ROOT)
        logs.append(log)
    runtime = executables(logs[2], {'layerx_programs_runtime', 'lifecycle'})
    registry = executables(logs[3], {'registry'})
    built = {'native': producer.artifact(native)}
    for name, path in {**runtime, **registry}.items():
        target = directory / name
        shutil.copyfile(path, target)
        target.chmod(0o700)
        built[name] = producer.artifact(target)
    require(producer.identity() == source, 'source changed during build')
    producer.write_private(directory / 'manifest.json', {
        'schema': SCHEMA, 'source': source, 'commands': commands,
        'interface_manifest': producer.artifact(inherited_path),
        'artifacts': built, 'logs': [producer.artifact(log) for log in logs],
        'required_runtime_cases': sorted(RUNTIME_CASES),
        'required_registry_cases': sorted(REGISTRY_CASES),
        'required_interface_cases': sorted(INTERFACE_CASES),
        'required_runtime_policy_cases': sorted(RUNTIME_POLICY_CASES),
    })
    print('ABI_POLICY_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def verify(manifest):
    path = Path(manifest).absolute()
    directory = producer.private_directory(path.parent)
    value = producer.load_private(path)
    source = producer.identity()
    require(value.get('schema') == SCHEMA and value.get('source') == source,
            'ABI policy artifacts do not bind this clean candidate')
    for field, required in (
        ('required_runtime_cases', RUNTIME_CASES), ('required_registry_cases', REGISTRY_CASES),
        ('required_interface_cases', INTERFACE_CASES),
        ('required_runtime_policy_cases', RUNTIME_POLICY_CASES),
    ):
        require(value.get(field) == sorted(required), 'required ABI case inventory mismatch')
    artifacts = value['artifacts']
    require(set(artifacts) == {'native', 'layerx_programs_runtime', 'lifecycle', 'registry'},
            'ABI policy executable inventory mismatch')
    require(len(value['logs']) == 4 and len(value['commands']) == 4, 'missing build evidence')
    saved_artifacts = list(artifacts.values()) + value['logs'] + [value['interface_manifest']]
    for saved in saved_artifacts:
        require(producer.artifact(saved['path']) == saved, 'artifact changed: ' + saved['path'])
    for saved in artifacts.values():
        require(os.access(saved['path'], os.X_OK), 'artifact is not executable')
    inherited_path = value['interface_manifest']['path']
    inherited = producer.load_private(inherited_path)
    require(inherited.get('source') == source, 'inherited interface source mismatch')
    run_dir = Path(tempfile.mkdtemp(prefix='abi-gate-', dir=directory))
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    logs = []

    def run(command, name):
        log = run_dir / (name + '.log')
        output = interface.run(command, log, environment)
        logs.append(log)
        return output

    output = run([sys.executable, str(BASE / 'program-interface.py'), '--manifest', inherited_path],
                 'authenticated-interface')
    counts = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=(\d+)$', output, re.M)
    require(len(counts) == 1 and int(counts[0][0]) > 0 and int(counts[0][1]) == 0,
            'authenticated interface qualification did not run')
    count = int(counts[0][0])
    passed = set(re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', output, re.M))
    require(INTERFACE_CASES <= passed, 'inherited interface gate omitted ABI transition cases')
    output = run([artifacts['native']['path']], 'native-registration')
    counts = re.findall(r'^ABI_POLICY_NATIVE tests=(\d+) skipped=(\d+)$', output, re.M)
    require(counts == [('32', '0')], 'native ABI matrix did not execute all 32 checks')
    count += 32
    for artifact_name, required, filter_name in (
        ('layerx_programs_runtime', RUNTIME_POLICY_CASES, 'abi_policy::tests::'),
        ('lifecycle', RUNTIME_CASES, 'abi_'),
    ):
        command = [artifacts[artifact_name]['path']]
        if filter_name:
            command.append(filter_name)
        command.append('--test-threads=1')
        output = run(command, artifact_name)
        count += interface.rust_count(output, required)
    for name in sorted(REGISTRY_CASES):
        output = run([artifacts['registry']['path'], '--exact', name, '--test-threads=1'],
                     'registry-' + name)
        count += interface.rust_count(output, {name})
    require(producer.identity() == source, 'source changed during gate')
    for saved in saved_artifacts:
        require(producer.artifact(saved['path']) == saved, 'artifact changed during gate')
    producer.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.program-abi-policy-result.v1', 'source': source,
        'manifest': producer.artifact(path), 'tests': count, 'skipped': 0,
        'logs': [producer.artifact(log) for log in logs],
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
        print('ABI policy qualification refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
