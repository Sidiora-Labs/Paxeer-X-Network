#!/usr/bin/env python3
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import sys
import time
import zipfile

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.deterministic-execution.v1'
WORKFLOW = '.github/workflows/programs-conformance.yml'
SUITES = ('determinism', 'replay', 'validation', 'execution', 'activity_budget')
MATRIX = {
    'linux-x86_64-debug': ('Linux', 'x86_64', 'debug', 'ubuntu-24.04'),
    'linux-x86_64-release': ('Linux', 'x86_64', 'release', 'ubuntu-24.04'),
    'linux-arm64-release': ('Linux', 'aarch64', 'release', 'ubuntu-24.04-arm'),
    'macos-arm64-release': ('Darwin', 'aarch64', 'release', 'macos-14'),
    'windows-x86_64-release': ('Windows', 'x86_64', 'release', 'windows-2022'),
}
MAX_BYTES = 64 * 1024 * 1024
START = time.monotonic()


def require(value, message):
    if not value:
        raise ValueError(message)


def remaining():
    seconds = 1740 - int(time.monotonic() - START)
    require(seconds > 0, 'phase time limit reached')
    return seconds


def run(argv, cwd=ROOT, env=None, log=None):
    command = [str(x) for x in argv]
    if log is None:
        result = subprocess.run(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=min(remaining(), 90), check=False)
        require(result.returncode == 0, 'command failed: ' + command[0])
        require(len(result.stdout) <= MAX_BYTES, 'command output exceeds bound')
        return result.stdout
    with log.open('xb') as stream:
        stream.write(('COMMAND ' + json.dumps(command) + '\n').encode())
        stream.flush()
        result = subprocess.run(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=stream, stderr=subprocess.STDOUT,
                                timeout=remaining(), check=False)
        stream.write(('\nEXIT ' + str(result.returncode) + '\n').encode())
    require(result.returncode == 0,
            'command exited ' + str(result.returncode) + '; log=' + str(log))
    return result.returncode


def git(*argv):
    return run(['git', *argv]).decode().strip()


def identity():
    require(not git('status', '--porcelain=v1', '--untracked-files=normal'),
            'candidate source must be clean')
    return {'revision': git('rev-parse', 'HEAD^{commit}'),
            'tree': git('rev-parse', 'HEAD^{tree}')}


def machine():
    arch = platform.machine().lower()
    arch = {'amd64': 'x86_64', 'arm64': 'aarch64'}.get(arch, arch)
    return {'os': platform.system(), 'architecture': arch}


def digest_bytes(value):
    return hashlib.sha256(value).hexdigest()


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def artifact(path):
    path = path.resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= MAX_BYTES * 16,
            'invalid artifact file')
    return {'path': str(path), 'sha256': digest(path), 'bytes': info.st_size}


def checked_artifact(record):
    path = Path(record['path'])
    require(not path.is_symlink() and artifact(path) == record,
            'artifact identity changed')
    return path


def directory(path, fresh=False):
    path = Path(path).absolute()
    require(not path.is_symlink(), 'symlink output directory refused')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = path.resolve(strict=True)
    require(path != ROOT and ROOT not in path.parents,
            'outputs must be outside source checkout')
    if os.name != 'nt':
        info = path.stat()
        require(info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                'private owned output directory required')
    require(not fresh or not any(path.iterdir()), 'fresh empty output directory required')
    return path


def duplicates(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, 'duplicate JSON key')
        value[key] = item
    return value


def decode(data):
    require(len(data) <= MAX_BYTES, 'JSON bound exceeded')
    return json.loads(data, object_pairs_hook=duplicates)


def load(path):
    require(not path.is_symlink() and path.is_file(), 'regular manifest required')
    require(not any(x == '.env' or x.startswith('.env.') for x in path.parts),
            'environment files forbidden')
    require(path.stat().st_size <= MAX_BYTES, 'manifest bound exceeded')
    return decode(path.read_bytes())


def write(path, data):
    with path.open('x', encoding='utf-8', newline='\n') as stream:
        json.dump(data, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o600)


def expected_tests(suite):
    source = (ROOT / 'programs/crates/layerx-programs-runtime/tests' / (suite + '.rs')).read_text()
    names = re.findall(r'#\[test\]\s*fn\s+(\w+)\s*\(', source)
    require(names and len(names) == len(set(names)), 'missing or duplicate source tests')
    require('#[ignore' not in source, 'ignored scoped test refused')
    return sorted(names)


def parse_test_log(log, expected):
    text = log.read_text(encoding='utf-8')
    passed = re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', text, re.M)
    summaries = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored; (\d+) measured; (\d+) filtered out;', text)
    require(sorted(passed) == expected and len(summaries) == 1,
            'scoped case inventory missing, duplicate or failed')
    require(tuple(map(int, summaries[0])) == (len(expected), 0, 0, 0, 0),
            'scoped tests were skipped or filtered')
    return len(passed)


def build(args):
    source = identity()
    out = directory(args.output, fresh=True)
    only_matrix = args.matrix_key is not None
    if only_matrix:
        expected = MATRIX[args.matrix_key]
        require(machine() == {'os': expected[0], 'architecture': expected[1]},
                'matrix runner OS/architecture mismatch')
        profile = expected[2]
    else:
        require(machine()['os'] == 'Linux', 'native scoped producer requires Linux')
        profile = 'debug'
    env = dict(os.environ)
    for name in tuple(env):
        if name in ('RUSTFLAGS', 'CARGO_ENCODED_RUSTFLAGS', 'RUSTC', 'RUSTC_WRAPPER',
                    'RUSTC_WORKSPACE_WRAPPER', 'CARGO_BUILD_TARGET') or name.startswith('CARGO_PROFILE_'):
            del env[name]
    target = Path(env.get('CARGO_TARGET_DIR', str(out / 'cargo-target'))).absolute()
    env.update(CARGO_TARGET_DIR=str(target), CARGO_INCREMENTAL='0',
               CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0')
    jobs = int(env.get('PAXEER_X_DETERMINISM_BUILD_JOBS', '5'))
    require(1 <= jobs <= 16, 'build jobs outside bounds')
    env['CARGO_BUILD_JOBS'] = str(jobs)
    suites = ('determinism',) if only_matrix else SUITES
    cargo = ['cargo', '+1.91.1']
    command = cargo + ['test', '--locked', '-p', 'layerx-programs-runtime']
    for suite in suites:
        command += ['--test', suite]
    command += ['--no-run', '--message-format=json']
    if profile == 'release':
        command.append('--release')
    log = out / 'rust-build.log'
    toolchain = run(['rustc', '+1.91.1', '-vV']).decode().strip()
    require(toolchain.startswith('rustc 1.91.1 '), 'wrong Rust toolchain')
    run(command, cwd=ROOT / 'programs', env=env, log=log)
    binaries = {}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        entry = decode(line)
        if entry.get('reason') == 'build-finished':
            finished = entry.get('success') is True
        if (entry.get('reason') == 'compiler-artifact' and entry.get('executable')
                and entry.get('profile', {}).get('test') is True):
            name = entry.get('target', {}).get('name')
            if name in suites:
                require(entry['profile']['opt_level'] == ('3' if profile == 'release' else '0')
                        and not entry.get('features'), 'unexpected compile profile or features')
                require(name not in binaries, 'duplicate compiler test artifact')
                executable = out / (name + ('.exe' if os.name == 'nt' else ''))
                shutil.copyfile(entry['executable'], executable)
                executable.chmod(0o700)
                binaries[name] = artifact(executable)
    require(finished and set(binaries) == set(suites), 'missing compiled test executables')
    logs = [artifact(log)]
    build_commands = [{'argv': command, 'cwd': str(ROOT / 'programs'), 'exit_code': 0}]
    native = None
    if not only_matrix:
        native_dir = out / 'native-build'
        library = native_dir / 'liblayerx.a'
        header = native_dir / 'generated/lxp_checkpoint_settlement.h'
        staticlib = target / 'debug/liblayerx_programs_sandbox.a'
        commands = [
            (['make', '--no-print-directory', '-j' + str(jobs),
              'BUILD_DIR=' + str(native_dir), 'LXP_REVISION=' + source['revision'],
              str(library), str(header)], ROOT),
            (cargo + ['build', '--locked', '-p', 'layerx-programs-sandbox',
                      '--features', 'host-ffi'], ROOT / 'programs'),
            (['cc', '-Iinclude', '-I' + str(header.parent), '-std=c17', '-pedantic',
              '-Werror', '-Wall', '-Wextra', '-Wconversion', '-Wshadow', '-Wvla',
              '-fno-strict-aliasing', '-ffp-contract=off', '-O2',
              'tests/programs/test_metered_call.c', '-Wl,--start-group',
              str(library), str(staticlib), '-Wl,--end-group',
              '-lcrypto', '-lsqlite3', '-pthread', '-ldl', '-lm',
              '-o', str(out / 'programs_metered_call')], ROOT),
        ]
        for index, (argv, cwd) in enumerate(commands):
            log = out / ('native-build-' + str(index + 1) + '.log')
            run(argv, cwd=cwd, env=env, log=log)
            logs.append(artifact(log))
            build_commands.append({'argv': argv, 'cwd': str(cwd), 'exit_code': 0})
        native = artifact(out / 'programs_metered_call')
    require(identity() == source, 'source changed during compile phase')
    write(out / 'manifest.json', {
        'schema': SCHEMA, 'phase': 'build', 'source': source, 'machine': machine(),
        'profile': profile, 'matrix_key': args.matrix_key, 'toolchain': toolchain,
        'binaries': binaries, 'native': native, 'logs': logs,
        'commands': build_commands,
        'profiles': {'dev_opt_level': 0, 'release_opt_level': 3, 'incremental': False},
        'tests': {suite: expected_tests(suite) for suite in suites},
        'producer_sha256': digest(Path(__file__)),
    })


def vector_records(golden, properties):
    require(isinstance(golden, list) and len(golden) == 1, 'missing golden execution')
    require(isinstance(properties, list) and len(properties) == 2048,
            'missing actual property executions')
    records = golden + properties
    require(all(isinstance(row, dict) for row in records), 'vector object required')
    require([x.get('case') for x in records] == ['golden-add'] +
            ['property-' + str(i) for i in range(2048)], 'wrong vector identities/order')
    for row in records:
        require(set(row) == {'case', 'left', 'right', 'evidence'}, 'wrong vector fields')
        require(type(row['left']) is int and type(row['right']) is int and
                -(2**31) <= row['left'] < 2**31 and -(2**31) <= row['right'] < 2**31,
                'invalid actual guest arguments')
        require(isinstance(row['evidence'], str) and
                re.fullmatch(r'(?:[0-9a-f]{2})+', row['evidence']), 'invalid canonical evidence')
    expected = (ROOT / 'programs/crates/layerx-programs-runtime/vectors/execution-v2.hex').read_text().strip()
    require(golden[0]['evidence'] == expected and golden[0]['left'] == 19 and
            golden[0]['right'] == 23, 'golden evidence changed')
    return records


def vectors(path):
    return vector_records(decode((path / 'golden.json').read_bytes()),
                          decode((path / 'properties.json').read_bytes()))


def execute(args):
    source = identity()
    manifest = load(Path(args.manifest))
    require(manifest['schema'] == SCHEMA and manifest['phase'] == 'build'
            and manifest['source'] == source and manifest['machine'] == machine()
            and manifest['producer_sha256'] == digest(Path(__file__)),
            'wrong source, producer or executable host')
    key = manifest['matrix_key']
    require(key is not None or args.phase == 'gate', 'matrix run requires matrix build')
    if key is not None:
        expected = MATRIX[key]
        require(manifest['profile'] == expected[2] and machine() ==
                {'os': expected[0], 'architecture': expected[1]}, 'matrix identity mismatch')
    out = directory(args.output, fresh=True)
    vector_dir = out / 'vectors'
    vector_dir.mkdir(mode=0o700)
    suites = ('determinism',) if key is not None else SUITES
    require(set(manifest['binaries']) == set(suites), 'wrong scoped executable set')
    counts = {}
    logs = {}
    executions = []
    raw_logs = {}
    for suite in suites:
        expected = expected_tests(suite)
        require(manifest['tests'][suite] == expected, 'source test inventory changed')
        executable = checked_artifact(manifest['binaries'][suite])
        log = out / (suite + '.log')
        env = dict(os.environ)
        env.pop('PAXEER_X_DETERMINISM_VECTOR_DIR', None)
        if suite == 'determinism':
            env['PAXEER_X_DETERMINISM_VECTOR_DIR'] = str(vector_dir)
        run([executable, '--test-threads=1', '--nocapture'], env=env, log=log)
        counts[suite] = parse_test_log(log, expected)
        logs[suite] = artifact(log)
        raw_logs[suite] = log.read_text()
        executions.append({'argv': [str(executable), '--test-threads=1', '--nocapture'],
                           'cwd': str(ROOT), 'exit_code': 0})
    records = vectors(vector_dir)
    if key is None:
        native = checked_artifact(manifest['native'])
        log = out / 'native.log'
        run([native], log=log)
        counts['native_metered_call_executable'] = 1
        logs['native'] = artifact(log)
        raw_logs['native'] = log.read_text()
        executions.append({'argv': [str(native)], 'cwd': str(ROOT), 'exit_code': 0})
    require(identity() == source, 'source changed during execution')
    report = {'schema': SCHEMA, 'phase': 'execution', 'source': source,
              'matrix_key': key, 'machine': machine(), 'profile': manifest['profile'],
              'toolchain': manifest['toolchain'], 'build_manifest': artifact(Path(args.manifest)),
              'build': manifest, 'executions': executions, 'raw_logs': raw_logs,
              'binaries': manifest['binaries'], 'logs': logs, 'tests': counts,
              'skipped': 0, 'exit_code': 0,
              'vectors_sha256': digest_bytes(json.dumps(records, sort_keys=True, separators=(',', ':')).encode()),
              'vectors': records}
    if key is not None:
        required = ('GITHUB_REPOSITORY', 'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_JOB', 'RUNNER_OS', 'RUNNER_ARCH')
        require(os.environ.get('GITHUB_ACTIONS') == 'true' and all(os.environ.get(x) for x in required),
                'matrix execution must run under GitHub Actions provenance')
        report['ci'] = {x: os.environ[x] for x in required}
        require(report['ci']['GITHUB_JOB'] == 'deterministic-execution', 'unexpected CI job')
    write(out / 'report.json', report)
    return report


def api(repository, suffix):
    require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repository), 'invalid repository')
    return run(['gh', 'api', '--hostname', 'github.com', '-H',
                'Accept: application/vnd.github+json', 'repos/' + repository + suffix])


def pages(repository, suffix, field):
    rows = []
    for page in range(1, 21):
        value = decode(api(repository, suffix + ('&' if '?' in suffix else '?') +
                           'per_page=100&page=' + str(page)))
        batch = value[field]
        require(isinstance(batch, list), 'invalid GitHub collection')
        rows.extend(batch)
        if len(batch) < 100:
            return rows
    raise ValueError('GitHub collection exceeds page bound')


def matrix_evidence(args, source):
    require(args.repository and args.run_id and re.fullmatch(r'[1-9][0-9]*', args.run_id),
            'real GitHub repository and workflow run ID required')
    origin = git('remote', 'get-url', 'origin')
    match = re.fullmatch(r'(?:https://github.com/|git@github.com:)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?', origin)
    require(match is not None and match[1].lower() == args.repository.lower(),
            'evidence repository differs from checkout origin')
    run_record = decode(api(args.repository, '/actions/runs/' + args.run_id))
    require(run_record['head_sha'] == source['revision'] and
            run_record['path'] == WORKFLOW and
            run_record['repository']['full_name'].lower() == args.repository.lower() and
            run_record['head_repository']['full_name'].lower() == args.repository.lower(),
            'workflow provenance/source mismatch')
    require(run_record['event'] in ('push', 'workflow_dispatch', 'schedule', 'pull_request'),
            'unsupported workflow event')
    if args.phase == 'gate':
        require(run_record['status'] == 'completed', 'matrix workflow is not finished')
    attempt = int(run_record['run_attempt'])
    jobs = pages(args.repository, '/actions/runs/' + args.run_id + '/attempts/' + str(attempt) + '/jobs', 'jobs')
    artifacts = pages(args.repository, '/actions/runs/' + args.run_id + '/artifacts', 'artifacts')
    reports = {}
    bindings = []
    common = None
    for key, (system, arch, profile, runner_label) in MATRIX.items():
        selected_jobs = [x for x in jobs if x['name'] == 'determinism-' + key]
        require(len(selected_jobs) == 1 and selected_jobs[0]['status'] == 'completed'
                and selected_jobs[0]['conclusion'] == 'success', 'missing successful platform job: ' + key)
        job = selected_jobs[0]
        require(runner_label in job['labels'], 'job runner label mismatch: ' + key)
        steps = {x['name']: x for x in job['steps']}
        for step in ('Build deterministic execution vectors', 'Execute deterministic execution vectors', 'Upload deterministic execution evidence'):
            require(steps.get(step, {}).get('conclusion') == 'success', 'missing executed CI phase: ' + step)
        name = 'determinism-' + key + '-' + str(attempt)
        found = [x for x in artifacts if x['name'] == name]
        require(len(found) == 1 and not found[0]['expired'], 'missing platform artifact: ' + name)
        item = found[0]
        require(item.get('workflow_run', {}).get('head_sha') == source['revision'],
                'artifact source binding mismatch')
        require(0 < item['size_in_bytes'] <= MAX_BYTES, 'artifact archive exceeds bound')
        payload = api(args.repository, '/actions/artifacts/' + str(item['id']) + '/zip')
        require(item.get('digest') == 'sha256:' + digest_bytes(payload), 'GitHub artifact digest mismatch')
        with zipfile.ZipFile(io.BytesIO(payload)) as archive:
            entries = archive.infolist()
            require(len(entries) == 1 and entries[0].filename == 'report.json'
                    and 0 < entries[0].file_size <= MAX_BYTES, 'unexpected artifact files')
            report = decode(archive.read(entries[0]))
        require(report['schema'] == SCHEMA and report['phase'] == 'execution'
                and report['source'] == source and report['matrix_key'] == key
                and report['machine'] == {'os': system, 'architecture': arch}
                and report['profile'] == profile and report['exit_code'] == 0
                and report['skipped'] == 0 and report['tests'] == {'determinism': len(expected_tests('determinism'))},
                'platform execution report mismatch')
        ci = report['ci']
        require(ci['GITHUB_REPOSITORY'].lower() == args.repository.lower()
                and ci['GITHUB_RUN_ID'] == args.run_id
                and ci['GITHUB_RUN_ATTEMPT'] == str(attempt)
                and ci['GITHUB_JOB'] == 'deterministic-execution', 'CI execution binding mismatch')
        require(report['toolchain'].startswith('rustc 1.91.1 '), 'wrong matrix Rust version')
        require(report['build']['source'] == source
                and report['build']['matrix_key'] == key
                and report['build']['machine'] == report['machine']
                and report['build']['profile'] == profile
                and report['build']['toolchain'] == report['toolchain']
                and report['build']['binaries'] == report['binaries']
                and report['build']['producer_sha256'] == digest(Path(__file__))
                and report['build']['tests'] == {'determinism': expected_tests('determinism')}
                and len(report['executions']) == 1
                and report['executions'][0]['exit_code'] == 0,
                'missing exact producer/build/execution binding')
        require(ci['RUNNER_OS'] == system.replace('Darwin', 'macOS')
                and ci['RUNNER_ARCH'] == ('ARM64' if arch == 'aarch64' else 'X64'),
                'runner environment identity mismatch')
        expected_argv = ['cargo', '+1.91.1', 'test', '--locked', '-p',
                         'layerx-programs-runtime', '--test', 'determinism',
                         '--no-run', '--message-format=json']
        if profile == 'release':
            expected_argv.append('--release')
        commands = report['build']['commands']
        require(len(commands) == 1 and commands[0]['argv'] == expected_argv
                and commands[0]['exit_code'] == 0
                and report['executions'][0]['argv'] ==
                [report['binaries']['determinism']['path'], '--test-threads=1', '--nocapture'],
                'actual matrix recipe differs')
        text = report['raw_logs']['determinism']
        require(sorted(re.findall(r'^test ([A-Za-z0-9_:]+) \.\.\. ok$', text, re.M))
                == expected_tests('determinism'), 'remote test case inventory differs')
        summaries = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored; (\d+) measured; (\d+) filtered out;', text)
        require(len(summaries) == 1 and tuple(map(int, summaries[0]))
                == (len(expected_tests('determinism')), 0, 0, 0, 0),
                'remote test skips or incomplete execution')
        require(isinstance(report['vectors'], list), 'invalid remote vector corpus')
        vector_records(report['vectors'][:1], report['vectors'][1:])
        canonical = json.dumps(report['vectors'], sort_keys=True, separators=(',', ':')).encode()
        require(digest_bytes(canonical) == report['vectors_sha256'] and len(report['vectors']) == 2049,
                'matrix corpus identity mismatch')
        if common is None:
            common = canonical
        require(canonical == common, 'cross-platform canonical guest evidence diverged')
        reports[key] = report
        bindings.append({'matrix_key': key, 'job_id': job['id'], 'artifact_id': item['id'],
                         'artifact_digest': item['digest'], 'run_attempt': attempt})
    return {'source': source, 'repository': args.repository, 'run_id': args.run_id,
            'bindings': bindings, 'vectors_sha256': digest_bytes(common),
            'tests': sum(sum(x['tests'].values()) for x in reports.values()),
            'skipped': 0}


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=('build', 'run', 'collect', 'gate'), default='build')
    parser.add_argument('--output', required=True)
    parser.add_argument('--manifest')
    parser.add_argument('--matrix-key', choices=tuple(MATRIX))
    parser.add_argument('--repository')
    parser.add_argument('--run-id')
    args = parser.parse_args()
    try:
        if args.phase == 'build':
            build(args)
        elif args.phase == 'run':
            require(args.manifest is not None, 'prebuilt manifest required')
            execute(args)
        else:
            source = identity()
            matrix = matrix_evidence(args, source)
            if args.phase == 'collect':
                out = directory(args.output, fresh=True)
                write(out / 'matrix.json', matrix)
            else:
                require(args.manifest is not None, 'prebuilt manifest required')
                require(load(Path(args.manifest))['matrix_key'] is None,
                        'gate requires complete scoped native build')
                local = execute(args)
                require(local['vectors_sha256'] == matrix['vectors_sha256'],
                        'local execution differs from platform corpus')
                write(Path(args.output) / 'matrix.json', matrix)
                count = sum(local['tests'].values()) + matrix['tests']
                print('PAXEER_X_GATE tests=' + str(count) + ' skipped=0')
    except (OSError, ValueError, KeyError, TypeError, AttributeError, IndexError,
            subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print('deterministic execution refused: ' + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
