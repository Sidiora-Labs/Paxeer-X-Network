#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.program-bindings-artifacts.v1'
PREREQUISITE_RESULT_SHA256 = '7a8c0e0330cbcaa8a63bc400b17d3601b861b55f90b361e40b1cf77a8d74e6f2'
LANGUAGES = {'rust', 'typescript', 'python', 'go', 'java', 'kotlin', 'swift', 'csharp'}
TYPES = {'u8', 'u16', 'u32', 'u64', 'u128', 'u256', 'i8', 'i16', 'i32', 'i64', 'i128',
         'bytes', 'fixed', 'variable', 'option', 'union', 'evm'}
CASES = {'roundtrip_' + name for name in TYPES} | {
    'typed_failure', 'malformed_call', 'stale_digest', 'wrong_code_hash'}
FIXTURES = {'abi1', 'abi2', 'abi2-dynamic', 'abi3', 'abi3-dynamic', 'abi4', 'abi4-dynamic'}
OUTPUTS = ['client.rs', 'client.ts', 'guest.rs', 'client.go', 'ProgramBindings.java',
           'client.kt', 'client.py', 'client.swift', 'Client.cs']
SDK_OUTPUTS = ['layerx_client.rs', 'layerx_client.ts', 'layerx_guest.rs',
               'client.go', 'ProgramBindings.java', 'client.kt',
               'client.py', 'client.swift', 'Client.cs']
COMPILERS = {'rust': {'rustc'}, 'rust_guest': {'rustc'}, 'typescript': {'tsc'},
             'python': {'mypy', 'python3'}, 'go': {'go'}, 'java': {'javac'},
             'kotlin': {'kotlinc'}, 'swift': {'swiftc'}, 'csharp': {'csc', 'dotnet'}}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifact(path, allow_empty=False):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and (allow_empty or info.st_size > 0), 'missing artifact: ' + str(path))
    return {'path': str(path), 'sha256': digest(path), 'bytes': info.st_size}


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, text=True, timeout=60).strip()


def identity():
    require(not capture(['git', 'status', '--porcelain=v1', '--untracked-files=normal']),
            'candidate source is dirty')
    return {'revision': capture(['git', 'rev-parse', 'HEAD^{commit}']),
            'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']), 'dirty': False}


def private_directory(path, create=False):
    path = Path(path).absolute()
    require(not path.is_symlink(), 'artifact directory is a symlink')
    if create:
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    info = path.stat()
    require(ROOT != path and ROOT not in path.parents, 'artifacts must be outside repository')
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077, 'private caller-owned directory required')
    return path


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def load_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private caller-owned JSON required')
        return json.load(stream)


def run(command, log, environment, cwd, deadline):
    remaining = deadline - time.monotonic()
    require(remaining > 0, 'original producer/gate time limit exhausted')
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    code = 127
    with os.fdopen(fd, 'wb') as stream:
        stream.write(('COMMAND ' + json.dumps(command) + '\nCWD ' + str(cwd) + '\n').encode())
        stream.flush()
        try:
            result = subprocess.run(command, cwd=cwd, env=environment,
                                    stdin=subprocess.DEVNULL, stdout=stream,
                                    stderr=subprocess.STDOUT, timeout=remaining)
            code = result.returncode
        except subprocess.TimeoutExpired:
            code = 124
        except OSError as error:
            stream.write(str(error).encode())
        stream.write(f'\nEXIT {code}\n'.encode())
    return {'command': command, 'cwd': str(cwd), 'exit_code': code, 'log': artifact(log)}


def successful(record):
    require(record['exit_code'] == 0,
            f"command exited {record['exit_code']}; log={record['log']['path']}")


def inside(root, path):
    candidate = (root / path).resolve()
    require(candidate != root and root in candidate.parents, 'consumer path escapes artifact root')
    return candidate


def expand(values, root):
    require(isinstance(values, list) and values and all(isinstance(v, str) for v in values),
            'missing argv inventory')
    return [value.replace('{root}', str(root)).replace('{repo}', str(ROOT)) for value in values]


def validate_plan(plan, root):
    require(plan.get('schema') == 1, 'unsupported consumer plan')
    rows = plan['consumers']
    ids = set()
    cases = {language: set() for language in LANGUAGES | {'rust_guest'}}
    negative = set()
    for row in rows:
        name, language = row['id'], row['language']
        require(re.fullmatch(r'[a-zA-Z0-9_-]+', name) and name not in ids, 'duplicate/invalid consumer id')
        ids.add(name)
        require(language in COMPILERS, 'unknown consumer language: ' + language)
        require(isinstance(row['expect_success'], bool), 'missing compiler expectation')
        require(row['sources'] and row['cases'], 'empty source or case inventory: ' + name)
        require(len(row['cases']) == len(set(row['cases'])), 'duplicate cases: ' + name)
        for source in row['sources']:
            artifact(inside(root, source))
        command = expand(row['compile'], root)
        require(Path(command[0]).name in COMPILERS[language], 'unexpected compiler: ' + name)
        require(not any(v in command for v in ['run', 'test', 'install', 'restore']),
                'compiler command may not run tests, consumers or installs')
        if Path(command[0]).name == 'dotnet':
            require('-p:RestoreSources=' + str(root / 'offline-nuget') in command
                    and '-p:NuGetAudit=false' in command, 'C# compilation must use empty offline package source')
        if row['expect_success']:
            require(row['run'] and row['artifacts'], 'successful consumer has no runnable artifact')
        else:
            require(row['diagnostic'] and not row.get('run'), 'refusal lacks diagnostic or invokes runtime')
            negative.add(language)
        cases[language].update(row['cases'])
    require(set(language for language in cases if cases[language]) == LANGUAGES | {'rust_guest'},
            'missing required generated language or guest')
    for language in LANGUAGES:
        require(CASES <= cases[language], 'missing cases for ' + language + ': ' + ','.join(sorted(CASES - cases[language])))
    require(LANGUAGES | {'rust_guest'} <= negative, 'missing real compiler/type-checker refusal')
    require('guest_missing_entry' in cases['rust_guest'], 'missing guest entry-point refusal')
    return rows


def proof_encoding(path):
    fields = {}
    for line in path.read_text().splitlines():
        key, value = line.split('=', 1)
        require(key not in fields, 'duplicate fixture evidence field')
        fields[key] = value
    def raw(key):
        return bytes.fromhex(fields[key])
    def sized(data):
        return len(data).to_bytes(4, 'big') + data
    def state(prefix):
        siblings = fields[prefix + '.siblings']
        values = [] if not siblings else [bytes.fromhex(v) for v in siblings.split(',')]
        require(len(values) <= 32 and all(len(v) == 32 for v in values), 'bad fixture proof siblings')
        return (int(fields[prefix + '.leaf_index']).to_bytes(4, 'big')
                + int(fields[prefix + '.leaf_count']).to_bytes(4, 'big')
                + bytes([len(values)]) + b''.join(values))
    def witness(prefix):
        return sized(raw(prefix + '.key')) + sized(raw(prefix + '.value')) + state(prefix + '.proof')
    def optional(prefix):
        return b'\0' if fields.get(prefix) == 'absent' else b'\1' + witness(prefix)
    return (b'LayerX/programs/deployment-proof/v1\0' + sized(raw('activity'))
            + sized(b'\1' + state('activity_proof')) + sized(raw('receipt'))
            + sized(b'\1' + state('receipt_proof')) + sized(raw('header'))
            + raw('header_signature') + raw('programs_root') + state('programs_root_proof')
            + witness('program_record') + b'\0' + optional('lifecycle_lower') + optional('lifecycle_upper'))


def fixture_inputs(result_path, directory):
    result_path = Path(result_path).resolve(strict=True)
    require(digest(result_path) == PREREQUISITE_RESULT_SHA256, 'unrecognized immutable prerequisite result')
    record = load_private(result_path)
    require(record['schema'] == 'paxeer-x.program-interface-result.v1'
            and record['source']['revision'] == 'a33d608d82c7b8d6915bf2a90085a99f6e5a31ff'
            and record['source']['dirty'] is False and record['skipped'] == 0
            and record['tests'] == 22, 'not the qualified immutable interface result')
    source_files = {Path(row['path']): row for row in record['fixtures']}
    root = directory / 'fixtures'
    root.mkdir(mode=0o700)
    values = []
    for case in sorted(FIXTURES):
        target = root / case
        target.mkdir(mode=0o700)
        provenance = []
        for name in ['interface.bin', 'module.wasm', 'evidence.kvx', 'trust.bin']:
            original = result_path.parent / 'fixtures' / case / name
            require(original in source_files and artifact(original) == source_files[original],
                    'immutable prerequisite fixture mismatch: ' + str(original))
            provenance.append(source_files[original])
            shutil.copyfile(original, target / name)
            (target / name).chmod(0o600)
        (target / 'deployment-proof.bin').write_bytes(proof_encoding(target / 'evidence.kvx'))
        interface = (target / 'interface.bin').read_bytes()
        domain_end = interface.index(0) + 1
        require(interface[:domain_end] in [b'LayerX/program-interface/v1\0', b'LayerX/program-interface/v2\0',
                                               b'LayerX/program-interface/v3\0', b'LayerX/program-interface/v4\0'],
                'unsupported canonical interface domain')
        values.append({'case': case, 'path': str(target), 'digest': digest(target / 'interface.bin'),
                       'code_hash': interface[domain_end:domain_end + 32].hex(),
                       'originals': provenance,
                       'files': [artifact(p) for p in sorted(target.iterdir())]})
    return {'result': artifact(result_path), 'source': record['source'], 'cases': values}


def cli_command(binary, fixture, output):
    path = Path(fixture['path'])
    return [str(binary), 'program', 'bindings', '--interface', str(path / 'interface.bin'),
            '--digest', fixture['digest'], '--code-hash', fixture['code_hash'],
            '--deployment-proof', str(path / 'deployment-proof.bin'),
            '--trust-history', str(path / 'trust.bin'), '--historical', '--output', str(output)]


def cargo_artifacts(log, names):
    found = {}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        value = json.loads(line)
        if value.get('reason') == 'build-finished':
            finished = value.get('success') is True
        if value.get('reason') == 'compiler-artifact' and value.get('executable'):
            name = value.get('target', {}).get('name')
            if name in names:
                require(name not in found, 'duplicate candidate artifact: ' + name)
                found[name] = value['executable']
    require(finished and set(found) == set(names), 'missing current compiled candidate artifacts')
    return found


def build(args):
    started = time.time()
    deadline_epoch = args.deadline_epoch if args.deadline_epoch is not None else started + 1800
    require(started < deadline_epoch <= started + 1800, 'producer deadline must retain the original bounded task window')
    deadline = time.monotonic() + deadline_epoch - started
    source = identity()
    directory = private_directory(args.output, create=True)
    require(not any(directory.iterdir()), 'producer requires fresh empty output')
    fixture = fixture_inputs(args.interface_result, directory)
    selected = next(row for row in fixture['cases'] if row['case'] == 'abi2-dynamic')
    sdk_dir = directory / 'sdk-build-bindings'
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1', CARGO_INCREMENTAL='0',
                       CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
                       CARGO_TARGET_DIR=str(directory / 'cargo-target'), CARGO_NET_OFFLINE='true',
                       GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off',
                       DOTNET_SKIP_FIRST_TIME_EXPERIENCE='1', DOTNET_CLI_TELEMETRY_OPTOUT='1',
                       CARGO_BUILD_JOBS=os.environ.get('PAXEER_X_BINDINGS_BUILD_JOBS', '4'),
                       LAYERX_INTERFACE_PATH=str(Path(selected['path']) / 'interface.bin'),
                       LAYERX_INTERFACE_DIGEST=selected['digest'],
                       LAYERX_PROGRAM_CODE_HASH=selected['code_hash'], LAYERX_BINDINGS_DIR=str(sdk_dir))
    require(1 <= int(environment['CARGO_BUILD_JOBS']) <= 16, 'invalid build job bound')
    record = {'schema': SCHEMA, 'source': source, 'producer_root': str(ROOT), 'deadline_epoch': deadline_epoch,
              'started_epoch': started, 'fixtures': fixture, 'languages': sorted(LANGUAGES), 'required_cases': sorted(CASES),
              'commands': [], 'artifacts': {}}
    write_private(directory / 'build-inputs.json', record)
    installed_rust = capture(['rustup', 'toolchain', 'list'])
    require(any(line.split()[0].startswith('1.91.1-') for line in installed_rust.splitlines() if line.split()),
            'provisioned Rust 1.91.1 toolchain absent; installation forbidden')
    record['installed_rust_toolchains'] = installed_rust
    cargo = ['cargo', '+1.91.1']
    commands = [cargo + ['test', '--locked', '--manifest-path', str(ROOT / 'programs/Cargo.toml'),
                         '-p', 'layerx-program-sdk', '--lib', '--test', 'bindgen_conformance',
                         '--no-run', '--message-format=json'],
                cargo + ['build', '--locked', '--manifest-path', str(ROOT / 'platform/Cargo.toml'),
                         '-p', 'layerx-platform-cli', '--bin', 'layerx', '--message-format=json']]
    for index, command in enumerate(commands):
        item = run(command, directory / f'build-{index}.log', environment, ROOT, deadline)
        record['commands'].append(item)
        successful(item)
        names = ['layerx_program_sdk', 'bindgen_conformance'] if index == 0 else ['layerx']
        for name, original in cargo_artifacts(Path(item['log']['path']), names).items():
            target = directory / name
            shutil.copyfile(original, target)
            target.chmod(0o700)
            record['artifacts'][name] = artifact(target)
    record['sdk_outputs'] = [artifact(sdk_dir / name) for name in SDK_OUTPUTS]
    consumers = directory / 'consumers'
    consumers.mkdir(mode=0o700)
    (consumers / 'offline-nuget').mkdir(mode=0o700)
    environment['PAXEER_X_BINDINGS_EMIT_DIR'] = str(consumers)
    environment['PAXEER_X_BINDINGS_FIXTURES'] = str(directory / 'fixtures')
    emission = run([record['artifacts']['bindgen_conformance']['path'], '--exact',
                    'emit_generated_consumers', '--test-threads=1', '--nocapture'],
                   directory / 'emit.log', environment, ROOT, deadline)
    successful(emission)
    record['commands'].append(emission)
    plan = load_private(consumers / 'consumer-plan.json')
    rows = validate_plan(plan, consumers)
    compile_results = {'schema': 1, 'consumers': []}
    for row in rows:
        command = expand(row['compile'], consumers)
        cwd = Path(row.get('cwd', '{root}').replace('{root}', str(consumers)))
        require(cwd.resolve() == consumers or consumers in cwd.resolve().parents, 'compiler cwd escapes consumers')
        item = run(command, directory / ('compile-' + row['id'] + '.log'), environment, cwd, deadline)
        saved = dict(row, **item)
        compiler_path = shutil.which(command[0])
        saved['compiler'] = artifact(compiler_path) if compiler_path else None
        saved['source_artifacts'] = [artifact(inside(consumers, path)) for path in row['sources']]
        saved['built_artifacts'] = []
        compile_results['consumers'].append(saved)
        if row['expect_success']:
            successful(item)
            saved['built_artifacts'] = [artifact(inside(consumers, path)) for path in row['artifacts']]
        else:
            require(item['exit_code'] not in [0, 124, 127] and item['exit_code'] > 0,
                    'expected actual compiler/type-checker refusal; log=' + item['log']['path'])
            require(re.search(row['diagnostic'], Path(item['log']['path']).read_text(errors='replace')),
                    'compiler refusal was unrelated; log=' + item['log']['path'])
        write_private(directory / ('compile-' + row['id'] + '.json'), saved)
    write_private(consumers / 'compile-results.json', compile_results)
    record['consumer_plan'] = artifact(consumers / 'consumer-plan.json')
    record['compile_results'] = artifact(consumers / 'compile-results.json')
    record['consumer_sources'] = [artifact(path, allow_empty=True) for path in sorted(consumers.rglob('*')) if path.is_file()]
    require(identity() == source, 'source changed during explicit producer invocation')
    write_private(directory / 'manifest.json', record)
    print('BINDINGS_BUILD_MANIFEST ' + str(directory / 'manifest.json'))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    parser.add_argument('--interface-result', required=True)
    parser.add_argument('--deadline-epoch', type=float)
    args = parser.parse_args()
    try:
        build(args)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'bindings producer refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
