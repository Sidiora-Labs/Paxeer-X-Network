#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import py_compile
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
PRIVATE = Path('/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1041317')
TS = ROOT / 'agent/sdk/typescript'
PYTHON = ROOT / 'agent/sdk/python'
SOURCES = (
    'agent/sdk/typescript/src/native-effect.ts',
    'agent/sdk/typescript/src/agent-http.ts',
    'agent/sdk/typescript/src/index.ts',
    'agent/sdk/typescript/test/native-effect.test.ts',
    'agent/sdk/python/layerx_sdk/native_effect.py',
    'agent/sdk/python/layerx_sdk/agent_http.py',
    'agent/sdk/python/layerx_sdk/agent_http.pyi',
    'agent/sdk/python/layerx_sdk/__init__.py',
    'agent/sdk/python/layerx_sdk/__init__.pyi',
    'tests/agent/sdk/python/test_native_effect.py',
    'agent/crates/layerx-sdk/src/agent_envelope.rs',
    'agent/crates/layerx-sdk/src/native_effect.rs',
    'agent/crates/layerx-sdk/src/lib.rs',
    'tools/qualification/paxeer-x/native-effect-sdk.py',
    'agent/crates/layerx-crypto/tests/fixtures/payments/native-1-5.hex',
)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def fingerprints():
    return {path: digest(ROOT / path) for path in SOURCES}


def save(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')
    path.chmod(0o600)


def run(command, cwd, environment, log, timeout):
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=cwd, env=environment, stdin=subprocess.DEVNULL,
                                stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
    log.chmod(0o600)
    print('EXIT', result.returncode, 'LOG', log, flush=True)
    if result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command)


def build(environment):
    manifest = PRIVATE / 'artifacts.json'
    manifest.unlink(missing_ok=True)
    source = fingerprints()
    run(['npm', 'run', 'build'], TS, environment, PRIVATE / 'build-typescript.log', 300)
    environment['PYTHONPYCACHEPREFIX'] = str(PRIVATE / 'python-cache')
    sys.pycache_prefix = environment['PYTHONPYCACHEPREFIX']
    artifacts = []
    for relative in SOURCES:
        if (relative.startswith('agent/sdk/python/') or relative == 'tests/agent/sdk/python/test_native_effect.py') and relative.endswith('.py'):
            compiled = py_compile.compile(str(ROOT / relative), doraise=True)
            artifacts.append(Path(compiled))
    print('EXIT 0 LOG Python codecs compiled', flush=True)
    cargo = ['/root/.cargo/bin/cargo', 'test', '--locked', '--manifest-path', 'agent/Cargo.toml',
             '-p', 'layerx-sdk', '--lib', 'native_effect::tests', '--no-run', '--message-format=json']
    run(cargo, ROOT, environment, PRIVATE / 'build-rust.log', 900)
    binaries = []
    for line in (PRIVATE / 'build-rust.log').read_text().splitlines():
        if not line.startswith('{'):
            continue
        item = json.loads(line)
        if item.get('reason') == 'compiler-artifact' and item.get('target', {}).get('name') == 'layerx_sdk' and item.get('profile', {}).get('test') and item.get('executable'):
            binaries.append(Path(item['executable']).resolve(strict=True))
    if len(set(binaries)) != 1:
        raise RuntimeError('exact actual Rust focused test artifact required')
    binary = binaries[0]
    artifacts += [binary, TS / 'dist/test/native-effect.test.js', TS / 'dist/src/native-effect.js', TS / 'dist/src/agent-http.js', TS / 'dist/src/index.js']
    if source != fingerprints():
        raise RuntimeError('authored source changed during build')
    save(manifest, {'sources': source, 'rust': str(binary),
                    'artifacts': {str(path): digest(path) for path in artifacts}})


def verify(environment):
    manifest = json.loads((PRIVATE / 'artifacts.json').read_text())
    if manifest['sources'] != fingerprints():
        raise RuntimeError('declared final source differs from built codecs')
    for path, expected in manifest['artifacts'].items():
        if digest(path) != expected:
            raise RuntimeError('prebuilt actual codec artifact changed: ' + path)
    rust = PRIVATE / 'canonical-rust.json'
    typescript = PRIVATE / 'canonical-typescript.json'
    python = PRIVATE / 'canonical-python.json'
    for path in (rust, typescript, python):
        path.unlink(missing_ok=True)
    environment.update(NATIVE_EFFECT_RUST_CANONICAL_OUTPUT=str(rust),
                       NATIVE_EFFECT_CROSS_LANGUAGE_REQUEST=str(rust),
                       NATIVE_EFFECT_TYPESCRIPT_CANONICAL_OUTPUT=str(typescript),
                       NATIVE_EFFECT_PYTHON_CANONICAL_OUTPUT=str(python),
                       PYTHONPYCACHEPREFIX=str(PRIVATE / 'python-cache'),
                       PYTHONPATH=str(PYTHON))
    run([manifest['rust'], 'native_effect::tests', '--test-threads=1'], ROOT, environment,
        PRIVATE / 'verify-rust.log', 120)
    summary = (PRIVATE / 'verify-rust.log').read_text()
    if '3 passed; 0 failed; 0 ignored' not in summary:
        raise RuntimeError('all three focused actual Rust codec cases must execute')
    run(['node', 'dist/test/native-effect.test.js'], TS, environment,
        PRIVATE / 'verify-typescript.log', 120)
    run(['python3', '-m', 'unittest', 'discover', '-s', str(ROOT / 'tests/agent/sdk/python'), '-p', 'test_native_effect.py'],
        PYTHON, environment, PRIVATE / 'verify-python.log', 120)
    original = rust.read_bytes()
    for path in (typescript, python):
        if path.read_bytes() != original:
            raise RuntimeError('native canonical cross-language codec mismatch: ' + str(path))
    if manifest['sources'] != fingerprints():
        raise RuntimeError('authored source changed during qualification')
    for path, expected in manifest['artifacts'].items():
        if digest(path) != expected:
            raise RuntimeError('built codec artifact changed during qualification')
    save(PRIVATE / 'result.json', {'task': '104.13.17', 'exit': 0,
        'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
        'command': 'timeout 10m python3 tools/qualification/paxeer-x/native-effect-sdk.py',
        'logs': [str(PRIVATE / ('verify-' + language + '.log')) for language in ('rust', 'typescript', 'python')],
        'sources': manifest['sources'], 'canonical_sha256': hashlib.sha256(original).hexdigest()})


def main():
    arguments = argparse.ArgumentParser()
    arguments.add_argument('--build', action='store_true')
    mode = arguments.parse_args()
    PRIVATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = PRIVATE.stat()
    if ROOT in PRIVATE.resolve().parents or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise RuntimeError('private caller-owned evidence outside checkout required')
    environment = dict(os.environ, CARGO_TARGET_DIR='/root/lx-target/agent', CARGO_BUILD_JOBS='4')
    if mode.build:
        build(environment)
    else:
        verify(environment)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, RuntimeError, py_compile.PyCompileError, subprocess.SubprocessError) as error:
        print('FAILED', str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
