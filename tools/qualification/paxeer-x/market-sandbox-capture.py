#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import sys
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[3]
TASK = '104.35.34'
TEST = 'genuine_signed_market_profile_claim_steps_and_cryptographic_refusals'
DECLARED = ('tests/daemon/lxp_test_market_sandbox_profile.c',
    'programs/crates/layerx-programs-arbiter/examples/market_sandbox_capture.rs',
    'tools/qualification/paxeer-x/market-sandbox-capture.py')
TARGET = Path(os.environ.get('LAYERX_MARKET_CAPTURE_TARGET', '/root/lx-target/task1043534'))
EVIDENCE = Path(os.environ.get('LAYERX_MARKET_CAPTURE_EVIDENCE',
    '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043534'))
DEADLINE = float(os.environ.get('PAXEER_X_TASK_DEADLINE_EPOCH', str(time.time() + 1200)))


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    output = subprocess.check_output(['git', 'ls-files', '-z', '--', 'src', 'include',
        'cmd/layerxd', 'cmd/layerx-guarantor', 'programs', 'agent/crates/layerx-client',
        'agent/crates/layerx-proof', 'agent/crates/layerx-wire', 'agent/crates/layerx-types',
        'agent/crates/layerx-crypto', 'tests/daemon/lxp_test_arbiter_admission.c',
        'tests/programs/test_call_activity.c', 'tests/bridge/files.h', 'Makefile',
        '.cargo', 'tools/qualification/paxeer-x/program-replay-record.py',
        'tools/qualification/paxeer-x/replay-authority.py'], cwd=ROOT)
    names = {os.fsdecode(value) for value in output.split(b'\0') if value} | set(DECLARED)
    return {name: sha(ROOT / name) for name in sorted(names)
        if Path(name).suffix in {'.c', '.h', '.rs', '.toml', '.lock', '.json', '.py', '.mk', '.inc'} or name == 'Makefile'}


def bound(seconds):
    remaining = min(seconds, DEADLINE - time.time())
    if remaining <= 0:
        raise RuntimeError('original task cutoff exhausted')
    return remaining


def environment():
    return dict(os.environ, PATH='/root/.cargo/bin:' + os.environ.get('PATH', ''),
        CARGO_TARGET_DIR=str(TARGET / 'rust'), CARGO_BUILD_JOBS='8')


def run(command, name, env=None, seconds=1100):
    log = EVIDENCE / (name + '.log')
    duration = bound(seconds)
    with log.open('w') as stream:
        process = subprocess.Popen(command, cwd=ROOT, env=env or environment(),
            stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
            start_new_session=True)
        try:
            code = process.wait(timeout=duration)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            log.with_suffix('.log.exit').write_text('124\n')
            raise
    log.with_suffix('.log.exit').write_text(str(code) + '\n')
    if code:
        raise subprocess.CalledProcessError(code, command)
    return log.read_text()


def row(path):
    path = Path(path).resolve(strict=True)
    return {'path': str(path), 'sha256': sha(path)}


def executable(output, name):
    found = set()
    for line in output.splitlines():
        if line.startswith('{'):
            value = json.loads(line)
            if value.get('reason') == 'compiler-artifact' and value.get('target', {}).get('name') == name and value.get('executable'):
                found.add(value['executable'])
    if len(found) != 1:
        raise RuntimeError('one genuine declared compiler executable required: ' + name)
    return row(found.pop())


def checked(value):
    path = Path(value['path'])
    if not path.is_file() or path.is_symlink() or sha(path) != value['sha256']:
        raise RuntimeError('source-bound artifact missing or changed')
    return path


def build():
    inputs = sources()
    generated = TARGET / 'native/generated/replay-authority-fixture-base.inc'
    module_path = ROOT / 'tools/qualification/paxeer-x/replay-authority.py'
    spec = importlib.util.spec_from_file_location('genuine_native_authority', module_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.prepare_fixture(generated)
    fragment = EVIDENCE / 'build-native.mk'
    fragment.write_text('''.PHONY: market-capture-build
market-capture-build: $(BUILD_DIR)/tests/lxp_test_market_sandbox_profile
$(BUILD_DIR)/tests/lxp_test_market_sandbox_profile: tests/daemon/lxp_test_market_sandbox_profile.c $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) | programs-build
\t@mkdir -p $(@D)
\t$(CC) $(CPPFLAGS) $(CFLAGS) -Icmd/layerxd -I$(BUILD_DIR)/generated $< $(filter-out $(BUILD_DIR)/obj/cmd/layerxd/main.o,$(LAYERXD_OBJECTS)) $(LIBRARY) $(PROGRAMS_RUNTIME_LIB) $(LIBRARY) $(EXTRA_LDFLAGS) -lssl -lcrypto -lsqlite3 -pthread -ldl -lm -o $@
''')
    native = TARGET / 'native/tests/lxp_test_market_sandbox_profile'
    run(['make', '-f', 'Makefile', '-f', str(fragment), '-j8',
        'BUILD_DIR=' + str(TARGET / 'native'), 'PROGRAMS_TARGET_DIR=' + str(TARGET / 'rust'),
        'PROGRAMS_RUNTIME_LIB=' + str(TARGET / 'rust/debug/liblayerx_programs_sandbox.a'),
        'market-capture-build'], 'build-native')
    cargo = '/root/.cargo/bin/cargo'
    run([cargo, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
        '--target', 'wasm32-unknown-unknown', '--release', '-p', 'layerx-programs-market'], 'build-market-wasm')
    provider = executable(run([cargo, 'build', '--locked', '--manifest-path', 'programs/Cargo.toml',
        '-p', 'layerx-programs-arbiter', '--example', 'market_sandbox_capture', '--message-format=json'],
        'build-provider'), 'market_sandbox_capture')
    verifier = executable(run([cargo, 'test', '--locked', '--manifest-path', 'programs/Cargo.toml',
        '-p', 'layerx-programs-arbiter', '--test', 'market_sandbox_profile', '--no-run', '--message-format=json'],
        'build-verifier'), 'market_sandbox_profile')
    if inputs != sources():
        raise RuntimeError('source changed during bounded build')
    manifest = {'task': TASK, 'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
        'inputs': inputs, 'build_exit': 0, 'native': row(native), 'provider': provider, 'verifier': verifier,
        'market_wasm': row(TARGET / 'rust/wasm32-unknown-unknown/release/layerx_programs_market.wasm')}
    (EVIDENCE / 'artifacts.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print('ARTIFACTS ' + str(EVIDENCE / 'artifacts.json'), flush=True)


def qualify():
    manifest = json.loads((EVIDENCE / 'artifacts.json').read_text())
    if manifest.get('task') != TASK or manifest.get('build_exit') != 0 or manifest['inputs'] != sources():
        raise RuntimeError('genuine source-bound build required')
    native, provider, verifier, wasm = (checked(manifest[key]) for key in ('native', 'provider', 'verifier', 'market_wasm'))
    fixture = Path(tempfile.mkdtemp(prefix='lxp-market-capture-'))
    run([str(provider), 'modules', str(fixture)], 'produce-modules', seconds=120)
    events = queue.Queue()
    stream = (EVIDENCE / 'produce-native.log').open('w')
    process = subprocess.Popen([str(native), str(fixture), str(wasm)], cwd=ROOT, env=environment(),
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
    def capture():
        for line in process.stdout:
            stream.write(line); stream.flush()
            if line.startswith('MARKET_CAPTURE_READY '): events.put(line.strip())
        events.put('EXIT')
    reader = threading.Thread(target=capture, daemon=True); reader.start()
    try:
        if events.get(timeout=bound(120)) != 'MARKET_CAPTURE_READY setup':
            raise RuntimeError('real Market setup failed: ' + str(EVIDENCE / 'produce-native.log'))
        run([str(provider), 'prepare', str(fixture), str(fixture / 'setup.json')], 'produce-runtime', seconds=120)
        process.stdin.write('continue\n'); process.stdin.flush()
        if process.wait(timeout=bound(120)) != 0:
            raise RuntimeError('genuine signed Market capture failed')
        reader.join(timeout=5)
        run([str(provider), 'finish', str(fixture)], 'produce-manifest', seconds=120)
        inputs = fixture / 'market-inputs.json'
        env = dict(environment(), LAYERX_MARKET_SANDBOX_INPUTS=str(inputs))
        output = run([str(verifier), TEST, '--exact', '--nocapture', '--test-threads=1'], 'verify', env, 540)
        if 'test result: ok. 1 passed; 0 failed; 0 ignored;' not in output:
            raise RuntimeError('genuine signed Market profile case missing or skipped')
        if manifest['inputs'] != sources(): raise RuntimeError('source changed during verification')
        result = {'task': TASK, 'revision': manifest['revision'], 'command': 'timeout 10m python3 tools/qualification/paxeer-x/market-sandbox-capture.py',
            'exit': 0, 'fixture_manifest': str(inputs), 'fixture_sha256': sha(inputs), 'log': str(EVIDENCE / 'verify.log')}
        (EVIDENCE / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
        print('VERIFIED ' + str(EVIDENCE / 'result.json'), flush=True)
    finally:
        if process.poll() is None:
            process.terminate()
            try: process.wait(timeout=5)
            except subprocess.TimeoutExpired: process.kill(); process.wait()
        reader.join(timeout=5); stream.close()


def main():
    parser = argparse.ArgumentParser(); parser.add_argument('--build', action='store_true'); args = parser.parse_args()
    EVIDENCE.mkdir(mode=0o700, parents=True, exist_ok=True)
    if EVIDENCE.stat().st_mode & 0o077: raise RuntimeError('private evidence directory required')
    if args.build: build()
    else: qualify()


if __name__ == '__main__':
    try: main()
    except (OSError, ValueError, RuntimeError, KeyError, queue.Empty, subprocess.SubprocessError) as error:
        print('FAILED ' + str(error), file=sys.stderr)
        sys.exit(error.returncode if isinstance(error, subprocess.CalledProcessError) else 1)
