#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
definition = importlib.util.spec_from_file_location(
    'bindings_producer', Path(__file__).with_name('program-bindings-producer.py'))
producer = importlib.util.module_from_spec(definition)
definition.loader.exec_module(producer)
require = producer.require


def unchanged(saved):
    require(producer.artifact(saved['path'], allow_empty=saved['bytes'] == 0) == saved, 'artifact changed: ' + saved['path'])


def rust_count(output, required=None):
    passed = re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', output, re.M)
    summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', output, re.M)
    require(len(summaries) == 1 and len(passed) == len(set(passed)), 'missing/duplicate Rust results')
    count, failed, ignored = map(int, summaries[0])
    require(count == len(passed) and count > 0 and failed == ignored == 0,
            'required Rust cases failed, were empty, or were ignored')
    if required is not None:
        require(set(passed) == required, 'Rust execution differs from exact inventory')
    return count


def runtime_command(row, root):
    argv = producer.expand(row['run'], root)
    path = Path(argv[0])
    if path.is_absolute() and root in path.resolve().parents:
        require(str(path.resolve()) in {value['path'] for value in row['built_artifacts']},
                'native runtime is not a recorded prebuilt artifact')
    else:
        require(path.name in {'node', 'python3', 'java', 'dotnet', 'mono'},
                'gate refuses compiler or unknown runtime command')
        if path.name == 'dotnet':
            require(len(argv) > 1 and argv[1].endswith('.dll'), 'dotnet gate only executes prebuilt DLLs')
        if path.name == 'python3':
            require(not any(value in argv for value in ['-c', '-m']), 'Python gate only executes generated source')
        if path.name == 'java':
            require(not any(value.endswith('.java') for value in argv), 'Java source-file launch would compile')
    return argv


def verify(args):
    manifest_path = Path(args.manifest).absolute()
    directory = producer.private_directory(manifest_path.parent)
    manifest = producer.load_private(manifest_path)
    remaining = manifest['deadline_epoch'] - time.time()
    require(0 < remaining <= 1800 and 0 < manifest['deadline_epoch'] - manifest['started_epoch'] <= 1800,
            'original producer-plus-gate deadline expired or invalid')
    deadline = time.monotonic() + remaining
    source = producer.identity()
    require(manifest['schema'] == producer.SCHEMA and manifest['source'] == source,
            'artifact provenance does not match clean candidate')
    require(manifest['languages'] == sorted(producer.LANGUAGES)
            and manifest['required_cases'] == sorted(producer.CASES), 'incomplete language/case inventory')
    require(set(manifest['artifacts']) == {'layerx_program_sdk', 'bindgen_conformance', 'layerx'},
            'missing current SDK/CLI candidate artifact')
    saved_files = list(manifest['artifacts'].values()) + manifest['sdk_outputs'] + manifest['consumer_sources']
    saved_files += [manifest['consumer_plan'], manifest['compile_results'], manifest['fixtures']['result']]
    for command in manifest['commands']:
        require(command['exit_code'] == 0, 'producer command was not successful')
        saved_files.append(command['log'])
    for fixture in manifest['fixtures']['cases']:
        saved_files += fixture['files'] + fixture['originals']
    for saved in saved_files:
        unchanged(saved)
    require({f['case'] for f in manifest['fixtures']['cases']} == producer.FIXTURES,
            'incomplete immutable ABI/domain inventory')
    require(manifest['fixtures']['result']['sha256'] == producer.PREREQUISITE_RESULT_SHA256,
            'unrecognized immutable prerequisite record')
    consumers = directory / 'consumers'
    require([producer.artifact(path, allow_empty=True) for path in sorted(consumers.rglob('*')) if path.is_file()]
            == manifest['consumer_sources'], 'consumer runtime file closure differs from producer')
    plan = producer.load_private(consumers / 'consumer-plan.json')
    expected = producer.validate_plan(plan, consumers)
    compiled = producer.load_private(consumers / 'compile-results.json')
    require(compiled['schema'] == 1, 'invalid compiler evidence schema')
    actual = compiled['consumers']
    require([row['id'] for row in actual] == [row['id'] for row in expected],
            'compiler evidence omitted or duplicated a consumer')
    run_dir = Path(tempfile.mkdtemp(prefix='gate-', dir=directory))
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1',
                       PAXEER_X_BINDINGS_ARTIFACTS=str(consumers),
                       PAXEER_X_BINDINGS_FIXTURES=str(directory / 'fixtures'))
    environment.pop('PAXEER_X_BINDINGS_EMIT_DIR', None)
    records = []
    cases = []
    def run(argv, name, cwd=producer.ROOT):
        record = producer.run(argv, run_dir / (name + '.log'), environment, cwd, deadline)
        records.append(record)
        return record, Path(record['log']['path']).read_text(errors='replace')
    for planned, row in zip(expected, actual):
        for key, value in planned.items():
            if key in {'compile', 'cwd'}:
                continue
            require(row.get(key) == value, 'compiler evidence changed plan field: ' + key)
        require(row['command'] == producer.expand(planned['compile'], consumers), 'compiler argv mismatch')
        expected_cwd = planned.get('cwd', '{root}').replace('{root}', str(consumers))
        require(Path(row['cwd']).resolve() == Path(expected_cwd).resolve(), 'compiler cwd mismatch')
        unchanged(row['log'])
        require(row.get('compiler') is not None, 'compiler executable was absent')
        unchanged(row['compiler'])
        for saved in row['source_artifacts'] + row['built_artifacts']:
            unchanged(saved)
        require(row['source_artifacts'] == [producer.artifact(producer.inside(consumers, path))
                                          for path in row['sources']], 'compiler inputs mismatch')
        if row['expect_success']:
            require(row['exit_code'] == 0, 'consumer did not compile')
            require(row['built_artifacts'] == [producer.artifact(producer.inside(consumers, path))
                                              for path in row['artifacts']], 'compiled output inventory mismatch')
            argv = runtime_command(row, consumers)
            cwd = Path(row.get('cwd', '{root}').replace('{root}', str(consumers)))
            record, output = run(argv, 'consumer-' + row['id'], cwd)
            producer.successful(record)
            markers = re.findall(r'^BINDING_CASE ([a-zA-Z0-9_]+)$', output, re.M)
            require(len(markers) == len(set(markers)) and set(markers) == set(row['cases']),
                    'executed case inventory differs: ' + row['id'])
        else:
            require(row['exit_code'] > 0 and row['exit_code'] not in {124, 127},
                    'compiler absence/timeout/success cannot prove a refusal')
            require(re.search(row['diagnostic'], Path(row['log']['path']).read_text(errors='replace')),
                    'wrong compiler refusal diagnostic')
        cases += [row['language'] + ':' + row['id'] + ':' + case for case in row['cases']]
    for name in ['layerx_program_sdk', 'bindgen_conformance']:
        binary = manifest['artifacts'][name]['path']
        listing, output = run([binary, '--list', '--format=terse'], name + '-inventory')
        producer.successful(listing)
        inventory = set(re.findall(r'^([A-Za-z0-9_:]+): test$', output, re.M))
        argv = [binary, '--test-threads=1']
        if name == 'bindgen_conformance':
            require('emit_generated_consumers' in inventory, 'missing source generation entry')
            inventory.remove('emit_generated_consumers')
            argv += ['--skip', 'emit_generated_consumers']
        result, output = run(argv, name)
        producer.successful(result)
        rust_count(output, inventory)
        cases += [name + ':' + case for case in sorted(inventory)]
    binary = manifest['artifacts']['layerx']['path']
    for fixture in manifest['fixtures']['cases']:
        case = fixture['case']
        generated = run_dir / ('cli-' + case)
        argv = producer.cli_command(binary, fixture, generated)
        result, _ = run(argv, 'cli-' + case)
        producer.successful(result)
        cli_manifest = json.loads((generated / 'bindings.json').read_text())
        require(cli_manifest['interface_digest'] == fixture['digest']
                and cli_manifest['code_hash'] == fixture['code_hash']
                and cli_manifest['evidence_scope'] == 'historical-deployment',
                'CLI metadata does not bind verified immutable deployment')
        require(cli_manifest['deployment_proof_sha256'] == producer.digest(Path(fixture['path']) / 'deployment-proof.bin'),
                'CLI proof provenance differs')
        for filename in producer.OUTPUTS:
            producer.artifact(generated / filename)
        cases.append('cli:' + case + ':verified_generation')
        if case == 'abi2-dynamic':
            for sdk_file, cli_file in zip(producer.SDK_OUTPUTS, producer.OUTPUTS):
                require(producer.digest(directory / 'sdk-build-bindings' / sdk_file)
                        == producer.digest(generated / cli_file), 'normal SDK build output differs: ' + sdk_file)
                cases.append('sdk_build:' + sdk_file)
        for option, name in [('--digest', 'stale_digest'), ('--code-hash', 'wrong_code_hash')]:
            refused_dir = run_dir / (case + '-' + name)
            bad = producer.cli_command(binary, fixture, refused_dir)
            bad[bad.index(option) + 1] = '00' * 32
            refusal, _ = run(bad, case + '-' + name)
            require(refusal['exit_code'] > 0 and refusal['exit_code'] not in {124, 127}
                    and not refused_dir.exists(), 'CLI accepted or rendered mismatched bindings')
            cases.append('cli:' + case + ':' + name)
        proof = Path(fixture['path']) / 'deployment-proof.bin'
        corrupt = run_dir / (case + '-tampered-proof.bin')
        data = bytearray(proof.read_bytes())
        data[-1] ^= 1
        corrupt.write_bytes(data)
        refused_dir = run_dir / (case + '-tampered-proof')
        bad = producer.cli_command(binary, fixture, refused_dir)
        bad[bad.index('--deployment-proof') + 1] = str(corrupt)
        refusal, _ = run(bad, case + '-tampered-proof')
        require(refusal['exit_code'] > 0 and refusal['exit_code'] not in {124, 127}
                and not refused_dir.exists(), 'CLI rendered unverified proof')
        cases.append('cli:' + case + ':tampered_proof')
        mismatched = run_dir / (case + '-wrong-interface.bin')
        canonical = bytearray((Path(fixture['path']) / 'interface.bin').read_bytes())
        hash_offset = canonical.index(0) + 1
        canonical[hash_offset] ^= 1
        mismatched.write_bytes(canonical)
        refused_dir = run_dir / (case + '-wrong-interface')
        bad = producer.cli_command(binary, fixture, refused_dir)
        bad[bad.index('--interface') + 1] = str(mismatched)
        bad[bad.index('--digest') + 1] = producer.digest(mismatched)
        bad[bad.index('--code-hash') + 1] = canonical[hash_offset:hash_offset + 32].hex()
        refusal, _ = run(bad, case + '-wrong-interface')
        require(refusal['exit_code'] > 0 and refusal['exit_code'] not in {124, 127}
                and not refused_dir.exists(), 'caller-asserted hashes bypassed genuine deployment binding')
        cases.append('cli:' + case + ':wrong_interface_matching_caller_hashes')
        refused_dir = run_dir / (case + '-missing-proof')
        bad = producer.cli_command(binary, fixture, refused_dir)
        proof_index = bad.index('--deployment-proof')
        del bad[proof_index:proof_index + 2]
        refusal, _ = run(bad, case + '-missing-proof')
        require(refusal['exit_code'] > 0 and refusal['exit_code'] not in {124, 127}
                and not refused_dir.exists(), 'CLI allowed generation without verified deployment authority')
        cases.append('cli:' + case + ':missing_proof')

    require(producer.identity() == source, 'candidate source changed during gate')
    for saved in saved_files:
        unchanged(saved)
    require([producer.artifact(path, allow_empty=True) for path in sorted(consumers.rglob('*')) if path.is_file()]
            == manifest['consumer_sources'], 'consumer runtime file closure changed during execution')
    producer.write_private(run_dir / 'result.json', {
        'schema': 'paxeer-x.program-bindings-result.v1', 'source': source,
        'manifest': producer.artifact(manifest_path), 'cases': cases, 'tests': len(cases),
        'skipped': 0, 'commands': records, 'prerequisite': manifest['fixtures']['result'],
        'proof_scope': 'immutable historical deployment fixtures; not current live head',
    })
    print(f'PAXEER_X_GATE tests={len(cases)} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    args = parser.parse_args()
    try:
        verify(args)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'bindings gate refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
