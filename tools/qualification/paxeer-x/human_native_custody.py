#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('human_boundary', ROOT / 'tools/qualification/paxeer-x/human-api-boundary.py')
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)
B.DEADLINE = time.monotonic() + 1180
if os.environ.get('PAXEER_X_TASK_DEADLINE_UNIX'):
    B.DEADLINE = min(B.DEADLINE, time.monotonic() + float(os.environ['PAXEER_X_TASK_DEADLINE_UNIX']) - time.time())


def inventory():
    roots = (ROOT / 'human', ROOT / 'agent', ROOT / 'crates', ROOT / 'platform')
    paths = {ROOT / 'tools/qualification/paxeer-x/human_native_custody.py',
             ROOT / 'human/apps/web/e2e/software-authenticator.ts'}
    for root in roots:
        for directory, children, files in os.walk(root):
            children[:] = [name for name in children if name not in ('target', 'node_modules', '.git', '.next')]
            for name in files:
                if name.endswith(('.rs', '.go', '.proto')) or name in ('Cargo.toml', 'Cargo.lock', 'go.mod', 'go.sum', 'build.rs'):
                    paths.add(Path(directory) / name)
    return {str(path.relative_to(ROOT)): B.digest(path) for path in sorted(paths) if not path.is_symlink()}


def source_digest(files):
    value = hashlib.sha256()
    for name, digest in sorted(files.items()):
        value.update(name.encode() + b'\0' + digest.encode() + b'\n')
    return value.hexdigest()


def run(command, log, cwd, environment):
    with log.open('xb') as output:
        os.chmod(log, 0o600)
        result = subprocess.run(command, cwd=cwd, env=environment, stdin=subprocess.DEVNULL,
                                stdout=output, stderr=subprocess.STDOUT, timeout=B.remaining())
    print('command=' + json.dumps(command) + ' exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
    B.require(result.returncode == 0, 'bounded native custody command failed')


def main():
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--build', action='store_true')
    mode.add_argument('--candidate-manifest')
    args = parser.parse_args()
    destination = Path(os.environ.get('PAXEER_X_NATIVE_CUSTODY_ARTIFACT_DIRECTORY',
                                      '/root/lx-ops/paxeer-x-integration-2026-10-03/native-custody-artifacts'))
    if args.build:
        destination.mkdir(mode=0o700, parents=True, exist_ok=False)
        B.private(str(destination), True)
        files = inventory()
        revision = B.git('rev-parse', 'HEAD').decode().strip()
        source = source_digest(files)
        environment = dict(os.environ, CARGO_BUILD_JOBS='4', CARGO_TARGET_DIR='/root/lx-target/human')
        run(['go', 'build', '-mod=readonly', '-o', str(destination / 'attestor'), './cmd/attestor'],
            destination / 'go-daemon-build.log', ROOT / 'human/wallet/attestor', environment)
        run(['go', 'test', '-mod=readonly', '-c', '-o', str(destination / 'native-server.test'), './internal/server'],
            destination / 'go-corpus-build.log', ROOT / 'human/wallet/attestor', environment)
        command = ['/root/.cargo/bin/cargo', 'test', '--locked', '--manifest-path', 'human/Cargo.toml',
                   '-p', 'layerx-human-kms', '--test', 'attestor_native_profile', '--no-run', '--message-format=json']
        run(command, destination / 'rust-corpus-build.log', ROOT, environment)
        executables = []
        for line in (destination / 'rust-corpus-build.log').read_text().splitlines():
            try:
                row = json.loads(line)
            except ValueError:
                continue
            if row.get('reason') == 'compiler-artifact' and row.get('target', {}).get('name') == 'attestor_native_profile' and row.get('executable'):
                executables.append(Path(row['executable']))
        B.require(len(executables) == 1, 'one real focused Rust executable')
        B.require(files == inventory(), 'source remained frozen through the bounded build')
        artifacts = {}
        for role, path in [('attestor', destination / 'attestor'), ('go_corpus', destination / 'native-server.test'), ('rust_corpus', executables[0])]:
            artifacts[role] = dict(path=str(path), sha256=B.digest(path), source_revision=revision, source_digest=source, build_exit=0)
        document = dict(schema='layerx-human-native-custody-build.v1', source_files=files,
                        source_revision=revision, source_digest=source, artifacts=artifacts)
        manifest = destination / 'build.json'
        with manifest.open('x') as output:
            os.chmod(manifest, 0o600)
            json.dump(document, output)
        print('native-custody build manifest=' + str(manifest))
        return
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(args.candidate_manifest)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    material = B.load(os.environ.get('PAXEER_X_NATIVE_CUSTODY_BUILD_MANIFEST', str(destination / 'build.json')))
    B.require(material['schema'] == 'layerx-human-native-custody-build.v1', 'closed protected real-process build authority')
    files = inventory()
    revision = B.git('rev-parse', 'HEAD').decode().strip()
    source = source_digest(files)
    B.require(material['source_files'] == files and material['source_digest'] == source
              and material['source_revision'] == revision and candidate['source']['revision'] == revision,
              'actual frozen candidate source binding')
    artifacts = {name: B.executable(row, revision, source) for name, row in material['artifacts'].items()}
    B.require(set(artifacts) == {'attestor', 'go_corpus', 'rust_corpus'}, 'actual compiled production daemon and focused real consumers')
    environment = dict(os.environ, PAXEER_X_NATIVE_CUSTODY_RUST_TEST=str(artifacts['rust_corpus']))
    log = destination / 'native-custody-verify.log'
    run([str(artifacts['go_corpus']), '-test.run=^TestNativeCustody', '-test.count=1', '-test.v',
         '-test.timeout=' + str(max(1, int(B.remaining()))) + 's'], log, ROOT / 'human/wallet/attestor', environment)
    observed = log.read_text()
    B.require('--- PASS: TestNativeCustodyRealProfiles' in observed and 'native_provider_and_sdk_real_cluster ... ok' in observed
              and 'native custody genuine passkey issuance and grant consent verified' in observed
              and 'native custody principal digest forgery ceremony-replay and consumed-evidence refusals verified' in observed
              and '\nPASS\n' in observed and 'SKIP' not in observed,
              'both real Go corpus and actual Rust custody/provider consumer ran without skips')
    print('human-native-custody: PASS revision=' + revision)


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        print('human-native-custody: FAIL ' + str(error), file=sys.stderr)
        sys.exit(1)
