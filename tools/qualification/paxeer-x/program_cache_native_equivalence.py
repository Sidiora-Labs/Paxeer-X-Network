#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.program-cache-artifacts.v1'
REQUIRED = {
    'only_exact_hash_and_versions_construct_an_artifact',
    'disabled_miss_hit_and_eviction_have_identical_execution_observations',
    'eviction_is_canonical_and_capacity_is_never_crossed',
    'variable_weight_admission_skips_an_oversized_key_and_considers_later_keys',
    'same_key_is_recompiled_and_replaced_for_different_engine_limits',
    'runtime_abi_and_upgrade_invalidation_are_explicit',
    'large_activity_mix_is_identical_with_cache_disabled_and_enabled',
}
GUEST_NAMES = {f'guest-{index:02d}.wasm' for index in range(12)}
OWN = [
    'Makefile', 'rust-toolchain.toml',
    'tests/programs/test_cache_native_equivalence.c',
    'tools/qualification/paxeer-x/program_cache_native_equivalence.py',
    'tools/paxeer-x/build/104.32.1.mk', 'tools/paxeer-x/gates/104.32.1.sh',
]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def capture(command):
    return subprocess.check_output(command, cwd=ROOT, text=True).strip()


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def manifest_path():
    raw = os.environ.get('PAXEER_X_CACHE_ARTIFACT_MANIFEST')
    require(raw, 'PAXEER_X_CACHE_ARTIFACT_MANIFEST is required')
    path = Path(raw).absolute()
    require(not path.is_symlink(), 'artifact manifest must not be a symlink')
    directory = path.parent.resolve(strict=True)
    require(directory != ROOT and ROOT not in directory.parents,
            'artifact manifest must be outside the repository')
    info = directory.stat()
    require(info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'artifact manifest directory must be private and owned by caller')
    return directory / path.name


def load_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, 'private artifact record required')
        return json.load(stream)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def sources():
    tracked = subprocess.check_output([
        'git', 'ls-files', '-z', '--', 'src', 'include', 'programs',
        'agent/crates', 'agent/Cargo.toml', 'agent/Cargo.lock',
        'contracts/config/checkpoint-settlement.json', 'tests/programs', '.cargo',
    ], cwd=ROOT).decode().split('\0')
    names = set(OWN)
    for name in tracked:
        if name and (Path(name).suffix in {'.c', '.h', '.rs', '.toml', '.lock',
                                          '.json', '.wat', '.wasm'}):
            names.add(name)
    return {name: digest(ROOT / name) for name in sorted(names)}


def identity():
    return {'revision': capture(['git', 'rev-parse', 'HEAD']), 'sources': sources()}


def artifact(raw):
    path = Path(raw).resolve(strict=True)
    require(path.is_file() and path.stat().st_size > 0, f'empty artifact: {path}')
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def snapshot(args):
    path = manifest_path()
    require(not path.exists(), 'refusing to replace retained producer manifest')
    value = identity()
    value['schema'] = SCHEMA
    value['producer_root'] = str(ROOT)
    value['native_flags'] = {'cppflags': args.cppflags, 'cflags': args.cflags,
                             'ldflags': args.ldflags}
    value['commands'] = [
        'cargo build --locked --manifest-path programs/Cargo.toml -p layerx-programs-sandbox --features host-ffi',
        'cargo test --locked --manifest-path programs/Cargo.toml -p layerx-programs-runtime --lib --no-run --message-format=json cache::tests',
        'make -f Makefile -f tools/paxeer-x/build/104.32.1.mk paxeer-x-build-104.32.1',
    ]
    value['features'] = {'sandbox': ['host-ffi'], 'runtime-tests': []}
    value['toolchains'] = {
        'cc': capture(shlex.split(args.compiler) + ['--version']),
        'cargo': capture(shlex.split(args.cargo) + ['--version']),
        'rustc': capture(['rustc', '-vV']),
    }
    write_private(path.with_suffix(path.suffix + '.inputs'), value)


def record(args):
    path = manifest_path()
    value = load_private(path.with_suffix(path.suffix + '.inputs'))
    current = identity()
    require(value['revision'] == current['revision'] and value['sources'] == current['sources'],
            'source changed during artifact production')
    candidates = set()
    completed = False
    with Path(args.cargo_json).open() as stream:
        for line in stream:
            event = json.loads(line)
            if event.get('reason') == 'build-finished':
                completed = event.get('success') is True
            if (event.get('reason') == 'compiler-artifact'
                    and event.get('target', {}).get('name') == 'layerx_programs_runtime'
                    and event.get('profile', {}).get('test') is True
                    and event.get('executable')):
                require(event.get('features') == [],
                        'runtime test producer enabled unexpected features')
                candidates.add(event['executable'])
    require(completed and len(candidates) == 1,
            'expected one successfully compiled runtime library test binary')
    value['artifacts'] = {
        'native': artifact(args.native), 'native-library': artifact(args.library),
        'sandbox-staticlib': artifact(args.staticlib),
        'runtime-tests': artifact(candidates.pop()),
        'cargo-json': artifact(args.cargo_json),
        'generated-header': artifact(args.generated_header),
    }
    guests = Path(args.guests).resolve(strict=True)
    value['guest_directory'] = str(guests)
    value['guests'] = {file.name: artifact(file) for file in sorted(guests.glob('*.wasm'))}
    require(set(value['guests']) == GUEST_NAMES, 'producer guest inventory is incomplete')
    value['required_cache_cases'] = sorted(REQUIRED)
    write_private(path, value)
    print(f'CACHE_BUILD_MANIFEST {path}')


def run_logged(command, log):
    result = subprocess.run(command, cwd=ROOT, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, text=True, timeout=900)
    fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        stream.write(result.stdout)
    print(result.stdout, end='')
    require(result.returncode == 0, f'command exited {result.returncode}; log={log}')
    return result.stdout


def verify(_args):
    path = manifest_path()
    value = load_private(path)
    require(value.get('schema') == SCHEMA, 'unsupported producer manifest')
    current = identity()
    require(value.get('revision') == current['revision'], 'producer revision differs from candidate')
    require(value.get('sources') == current['sources'], 'producer source hashes differ from candidate')
    require(value.get('required_cache_cases') == sorted(REQUIRED), 'incomplete case inventory')
    require(value.get('features') == {'sandbox': ['host-ffi'], 'runtime-tests': []},
            'producer features differ from required features')
    require(set(value['artifacts']) == {'native', 'native-library', 'sandbox-staticlib',
                                      'runtime-tests', 'cargo-json', 'generated-header'},
            'incomplete artifact inventory')
    require(set(value.get('guests', {})) == GUEST_NAMES, 'incomplete guest inventory')
    for saved in list(value['artifacts'].values()) + list(value['guests'].values()):
        require(artifact(saved['path']) == saved, f'artifact changed: {saved["path"]}')
    guest_directory = Path(value['guest_directory'])
    require(set(value['guests']) == {file.name for file in guest_directory.glob('*.wasm')},
            'guest inventory changed')
    for name, saved in value['guests'].items():
        require(artifact(guest_directory / name) == saved,
                f'guest path differs from producer: {name}')
    native = run_logged([value['artifacts']['native']['path'], '--guest-dir', str(guest_directory)],
                        path.parent / 'cache-native.log')
    counts = re.findall(r'^CACHE_NATIVE_EQUIVALENCE calls=(\d+) receipts=(\d+) hits=(\d+) compilations=(\d+) evictions=(\d+) invalidations=(\d+)$',
                        native, re.M)
    require(len(counts) == 1, 'native fixture did not report one complete result')
    calls, receipts, hits, compilations, evictions, invalidations = map(int, counts[0])
    require(calls == 1024 and receipts == 1072 and hits > 0 and compilations > 0
            and evictions > 0 and invalidations > 0,
            'native activity mix is empty, partial or did not exercise cache')
    output = run_logged([value['artifacts']['runtime-tests']['path'], 'cache::tests',
                         '--nocapture', '--test-threads=1'], path.parent / 'cache-runtime.log')
    passed = set(re.findall(r'^test cache::tests::([A-Za-z0-9_]+) \.\.\. ok$', output, re.M))
    require(REQUIRED <= passed, 'required focused runtime cache case did not pass')
    summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;',
                           output, re.M)
    require(len(summaries) == 1, 'missing focused runtime result count')
    count, failures, ignored = map(int, summaries[0])
    require(count == len(passed) and count >= len(REQUIRED) and failures == 0 and ignored == 0,
            'partial or skipped focused runtime cache cases')
    require(identity() == current, 'source changed during qualification')
    print(f'PAXEER_X_GATE tests={count + 1} skipped=0')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest='operation', required=True)
    before = sub.add_parser('snapshot')
    before.add_argument('--compiler', required=True)
    before.add_argument('--cargo', required=True)
    before.add_argument('--cppflags', required=True)
    before.add_argument('--cflags', required=True)
    before.add_argument('--ldflags', required=True)
    before.set_defaults(run=snapshot)
    producer = sub.add_parser('record')
    for name in ['native', 'library', 'staticlib', 'cargo-json', 'guests', 'generated-header']:
        producer.add_argument('--' + name, required=True)
    producer.set_defaults(run=record)
    gate = sub.add_parser('verify')
    gate.set_defaults(run=verify)
    args = parser.parse_args()
    try:
        args.run(args)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'cache qualification refused: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
