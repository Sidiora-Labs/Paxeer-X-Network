#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.custody-provider-build.v1'
SERVICE = ROOT / 'human/crates/layerx-human-service'
KMS = ROOT / 'human/crates/layerx-human-kms'


def refuse(message):
    raise RuntimeError(message)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def capture(argv, env=None):
    result = subprocess.run(argv, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)
    print(result.stdout, end='', flush=True)
    if result.returncode:
        refuse('command failed (%d): %s' % (result.returncode, ' '.join(map(str, argv))))
    return result.stdout


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args], text=True).strip()


def source():
    if git('status', '--porcelain', '--untracked-files=normal'):
        refuse('source must be committed and clean')
    return {'revision': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}')}


def environment(phase):
    env = dict(os.environ)
    if env.get('RUSTC_BOOTSTRAP') or env.get('RUSTDOCFLAGS') or env.get('RUSTFLAGS'):
        refuse('undeclared compiler overrides')
    for key in list(env):
        if key.startswith(('LAYERX_HUMAN_KMS_', 'LAYERX_ATTESTOR_', 'LAYERX_RUNTIME_CLOCK_')):
            del env[key]
    env.update(RUSTC_WRAPPER=str(Path(__file__).resolve()),
               PAXEER_X_CUSTODY_PHASE=phase, CARGO_BUILD_JOBS='2',
               CARGO_PROFILE_DEV_DEBUG='0', CARGO_PROFILE_TEST_DEBUG='0',
               CARGO_INCREMENTAL='0', CARGO_NET_OFFLINE='true')
    return env


def wrapper():
    compiler, *args = sys.argv[1:]
    if os.environ.get('PAXEER_X_CUSTODY_PHASE') == 'build':
        os.execv(compiler, [compiler, *args])
    if os.environ.get('PAXEER_X_CUSTODY_PHASE') != 'gate':
        refuse('undeclared compiler phase')
    # Cargo probes compiler metadata even when every package artifact is fresh.
    probe = ['-', '--crate-name', '___', '--print=file-names']
    for kind in ['bin', 'rlib', 'dylib', 'cdylib', 'staticlib', 'proc-macro']:
        probe.extend(['--crate-type', kind])
    probe.extend(['--print=sysroot', '--print=split-debuginfo', '--print=crate-name', '--print=cfg'])
    if args != ['-vV'] and args != probe:
        refuse('ordinary Cargo compilation forbidden during gate: ' + repr(args))
    os.execv(compiler, [compiler, *args])


def artifact(path):
    path = Path(path).resolve()
    if not path.is_file():
        refuse('missing artifact: ' + str(path))
    return {'path': str(path), 'sha256': digest(path)}


def build(manifest):
    initial = source()
    env = environment('build')
    version = capture(['rustc', '+1.91.1', '--version'], env).strip()
    if not version.startswith('rustc 1.91.1 '):
        refuse('wrong Rust toolchain')
    tests = []
    expected = []
    for package, extra, package_root in [
        ('layerx-human-service', [], SERVICE),
        ('layerx-human-kms', ['--test', 'provider'], KMS),
    ]:
        metadata = subprocess.check_output(['cargo', '+1.91.1', 'metadata', '--frozen',
            '--no-deps', '--format-version', '1', '--manifest-path', 'human/Cargo.toml'],
            cwd=ROOT, env=env, text=True)
        packages = json.loads(metadata)['packages']
        target_package = next(p for p in packages if p['name'] == package)
        targets = [t for t in target_package['targets'] if t['test'] and
                   (package == 'layerx-human-service' or t['name'] == 'provider') and
                   any(k in ['lib', 'bin', 'test'] for k in t['kind'])]
        expected.extend((package, t['name'], tuple(t['kind'])) for t in targets)
        output = capture(['cargo', '+1.91.1', 'test', '--frozen', '--manifest-path',
            'human/Cargo.toml', '-p', package, *extra, '--no-run', '--message-format=json'], env)
        for line in output.splitlines():
            try:
                item = json.loads(line)
            except ValueError:
                continue
            if (item.get('reason') != 'compiler-artifact' or not item.get('executable') or
                    not item.get('profile', {}).get('test') or
                    not Path(item['target']['src_path']).is_relative_to(package_root)):
                continue
            tests.append(dict(artifact(item['executable']), package=package,
                              name=item['target']['name'], kind=item['target']['kind']))
    actual = {(t['package'], t['name'], tuple(t['kind'])) for t in tests}
    if actual != set(expected) or len(actual) != len(tests):
        refuse('compiled test inventory differs from complete package targets')
    capture(['cargo', '+1.91.1', 'build', '--frozen', '--manifest-path',
             'platform/Cargo.toml', '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock'], env)
    target = Path(env['CARGO_TARGET_DIR']) / 'debug'
    result = dict(schema=SCHEMA, source=initial, toolchain=version, tests=tests,
                  kms=artifact(target / 'layerx-human-kms'),
                  clock=artifact(target / 'layerx-runtime-clock'),
                  wrapper=artifact(Path(__file__)), expected_doc_tests=3,
                  target_dir=env['CARGO_TARGET_DIR'])
    if source() != initial:
        refuse('source changed during build')
    manifest.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(manifest, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(result, stream, indent=2)
        stream.write('\n')


def counts(output):
    found = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored; (\d+) measured; (\d+) filtered out;', output)
    if not found:
        refuse('missing genuine test result')
    total = 0
    for passed, failed, ignored, measured, filtered in found:
        if any(int(v) for v in [failed, ignored, measured, filtered]):
            refuse('incomplete test coverage')
        total += int(passed)
    return total


def verify(manifest):
    info = manifest.stat()
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        refuse('build manifest must be private and owned')
    data = json.loads(manifest.read_text())
    initial = source()
    if data['schema'] != SCHEMA or data['source'] != initial:
        refuse('candidate build identity mismatch')
    if data['target_dir'] != os.environ.get('CARGO_TARGET_DIR'):
        refuse('build and gate target path mismatch')
    for item in [*data['tests'], data['kms'], data['clock'], data['wrapper']]:
        if artifact(item['path']) != {k: item[k] for k in ['path', 'sha256']}:
            refuse('artifact identity mismatch')
    if data['wrapper']['path'] != str(Path(__file__).resolve()):
        refuse('wrapper path mismatch')
    env = environment('gate')
    if capture(['rustc', '+1.91.1', '--version'], env).strip() != data['toolchain']:
        refuse('compiler identity mismatch')
    env['LAYERX_RUNTIME_CLOCK_BIN'] = data['clock']['path']
    run = ['sh', str(ROOT / 'tools/runtime/run-with-clock.sh')]
    total = 0
    for item in data['tests']:
        print('FULL TEST TARGET', item['package'], item['name'], flush=True)
        total += counts(capture([*run, item['path'], '--test-threads=1'], env))
    docs = capture([*run, 'cargo', '+1.91.1', 'test', '--frozen', '--manifest-path',
                    'human/Cargo.toml', '-p', 'layerx-human-service', '--doc'], env)
    doc_total = counts(docs)
    if doc_total != data['expected_doc_tests'] or doc_total != 3:
        refuse('all three original rustdoc examples are mandatory')
    if source() != initial:
        refuse('source changed during verification')
    print('PAXEER_X_GATE tests=%d skipped=0' % (total + doc_total))


if __name__ == '__main__':
    try:
        if len(sys.argv) > 1 and not sys.argv[1].startswith('--'):
            wrapper()
        if len(sys.argv) != 2 or sys.argv[1] not in ['--build', '--verify']:
            refuse('expected --build or --verify')
        manifest = Path(os.environ['PAXEER_X_CUSTODY_PROVIDER_MANIFEST'])
        (build if sys.argv[1] == '--build' else verify)(manifest)
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print('custody qualification refused:', error, file=sys.stderr)
        sys.exit(1)
