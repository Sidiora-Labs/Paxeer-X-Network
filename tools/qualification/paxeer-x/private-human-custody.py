#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import stat
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
NODE = Path('/root/lx-toolchains/node24/bin/node')
NPM = Path('/root/lx-toolchains/node24/bin/npm')
WEB = ROOT / 'human/apps/web'
SDK = ROOT / 'human/wallet/sdk'
EVIDENCE = Path(os.environ.get('PRIVATE_HUMAN_CUSTODY_EVIDENCE_ROOT',
    '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1412'))
MANIFEST = EVIDENCE / 'build-manifest.json'


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    require(path.is_file() and not path.is_symlink(), 'regular source/artifact required: ' + str(path.relative_to(ROOT)))
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_paths():
    paths = set()
    for directory in (SDK / 'src', WEB / 'src', WEB / 'copy'):
        paths.update(path for path in directory.rglob('*') if path.is_file())
    paths.update((WEB / 'e2e/custody-evidence.test.ts', WEB / 'package.json', WEB / 'package-lock.json',
                  WEB / 'tsconfig.json', SDK / 'package.json', SDK / 'tsconfig.json',
                  ROOT / 'human/wallet/tsconfig.base.json', Path(__file__).resolve()))
    require(all(not any(part == '.env' or part.startswith('.env.') for part in path.parts) for path in paths),
            'credential files are not source artifacts')
    return sorted(paths)


def hashes(paths):
    return {str(path.relative_to(ROOT)): digest(path) for path in paths}


def artifact_paths():
    paths = set()
    for directory in (SDK / 'dist', WEB / '.next/server', WEB / '.next/static'):
        paths.update(path for path in directory.rglob('*') if path.is_file())
    paths.update((WEB / '.next/BUILD_ID', WEB / '.next/build-manifest.json', WEB / '.next/routes-manifest.json'))
    return sorted(paths)


def run(argv, name, timeout):
    log = EVIDENCE / (name + '.log')
    environment = dict(os.environ)
    environment['PATH'] = str(NODE.parent) + os.pathsep + environment.get('PATH', '')
    with log.open('wb') as output:
        log.chmod(0o600)
        result = subprocess.run(argv, cwd=ROOT, env=environment, stdout=output,
                                stderr=subprocess.STDOUT, timeout=timeout, check=False)
    print(json.dumps({'command': argv, 'exit_code': result.returncode, 'log_path': str(log)}), flush=True)
    return result.returncode


def protected_runtime(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_nlink == 1
            and info.st_uid == os.getuid() and info.st_mode & 0o077 == 0,
            'protected private runtime configuration required')
    config = json.loads(path.read_text())
    require(config.get('version') == 1 and config.get('isolated') is True
            and config.get('approved_custody_execution') is True,
            'explicitly approved version 1 disposable fixture required')
    return config


def upstream_sdk_manifest():
    supplied = os.environ.get('PRIVATE_HUMAN_CUSTODY_SDK_MANIFEST')
    if not supplied:
        print(json.dumps({'status': 'prerequisite-unavailable', 'exit_code': 78,
                          'reason': 'PRIVATE_HUMAN_CUSTODY_SDK_MANIFEST from the completed sole task 14.11 SDK build required'}))
        return None
    path = Path(supplied)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_uid == os.getuid()
            and info.st_nlink == 1 and info.st_mode & 0o077 == 0, 'protected genuine SDK build manifest required')
    manifest = json.loads(path.read_text())
    require(isinstance(manifest.get('sources'), dict) and isinstance(manifest.get('artifacts'), dict),
            'actual SDK source/artifact build identity required')
    for relative in ('human/wallet/sdk/src/provider.ts', 'human/wallet/sdk/src/modules/gas-station.ts'):
        require(manifest['sources'].get(relative) == digest(ROOT / relative), 'upstream SDK source changed: ' + relative)
    actual = hashes(sorted(path for path in (SDK / 'dist').rglob('*') if path.is_file()))
    expected = {relative: value for relative, value in manifest['artifacts'].items()
                if relative.startswith('human/wallet/sdk/dist/')}
    require(actual and actual == expected and (SDK / 'dist/index.d.ts').is_file(),
            'complete genuine prebuilt SDK artifact identity required')
    return {'manifest': str(path), 'manifest_sha256': hashlib.sha256(path.read_bytes()).hexdigest()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    options = parser.parse_args()
    EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(NODE.is_file() and NPM.is_file(), 'existing Node 24 toolchain required; no installation permitted')
    sources = hashes(source_paths())
    if options.build:
        sdk_owner = upstream_sdk_manifest()
        if sdk_owner is None:
            return 78
        generated = [WEB / relative for relative in ('next-env.d.ts', 'tsconfig.json',
                     'public/manifest.json', 'public/sw.js')]
        generated.extend((WEB / 'packages/layerx-ui/dist').rglob('*.map'))
        before = {path: path.read_bytes() if path.exists() else None for path in generated}
        try:
            code = run([str(NPM), '--prefix', str(WEB), 'run', 'build'], 'build-web', 720)
        finally:
            for path, content in before.items():
                after = path.read_bytes() if path.exists() else None
                if after != content:
                    if content is None:
                        path.unlink(missing_ok=True)
                    else:
                        path.write_bytes(content)
        if code != 0:
            return code
        require(hashes(source_paths()) == sources, 'web build changed frozen task source')
        artifacts = artifact_paths()
        require(artifacts and (SDK / 'dist/index.js').is_file(), 'real compiled wallet owner artifact required')
        require(upstream_sdk_manifest() == sdk_owner, 'upstream SDK build identity changed')
        MANIFEST.write_text(json.dumps({'version': 1, 'sources': sources, 'artifacts': hashes(artifacts),
                                       'sdk_owner': sdk_owner}, indent=2))
        MANIFEST.chmod(0o600)
        print(json.dumps({'status': 'built', 'manifest': str(MANIFEST)}))
        return 0
    require(MANIFEST.is_file(), 'prerequisite: complete bounded --build manifest required')
    manifest = json.loads(MANIFEST.read_text())
    require(manifest.get('version') == 1 and manifest.get('sources') == sources,
            'qualification source differs from complete built task source')
    require(upstream_sdk_manifest() == manifest.get('sdk_owner'), 'upstream SDK build identity changed')
    artifacts = artifact_paths()
    require(hashes(artifacts) == manifest['artifacts'], 'compiled SDK/web artifact mismatch')
    supplied = os.environ.get('PRIVATE_HUMAN_CUSTODY_RUNTIME')
    if supplied:
        protected_runtime(supplied)
    code = run([str(NODE), '--test', '--test-reporter=tap', str(WEB / 'e2e/custody-evidence.test.ts')], 'verify-custody', 780)
    require(hashes(source_paths()) == sources, 'qualification changed frozen task source')
    require(hashes(artifact_paths()) == manifest['artifacts'], 'qualification changed compiled artifacts')
    if code != 0:
        return code
    output = (EVIDENCE / 'verify-custody.log').read_text()
    expected_tests = 6 if supplied else 5
    for counter, expected in (('tests', expected_tests), ('pass', expected_tests), ('fail', 0), ('skipped', 0)):
        require(re.search(r'^# ' + counter + ' ' + str(expected) + r'$', output, re.MULTILINE),
                'complete unskipped custody corpus required: ' + counter)
    if not supplied:
        print(json.dumps({'status': 'prerequisite-unavailable', 'exit_code': 78,
                          'reason': 'PRIVATE_HUMAN_CUSTODY_RUNTIME with approved real disposable gateway/attestor/chain fixture required',
                          'log_path': str(EVIDENCE / 'verify-custody.log')}))
        return 78
    config = protected_runtime(supplied)
    result = Path(config['evidence_dir']) / 'private-human-custody-result.json'
    require(result.is_file(), 'real private custody runtime result absent')
    report = json.loads(result.read_text())
    expected = {'real-deposit-codec', 'owner-confirmation-refusal', 'retained-signed-proof', 'lost-broadcast-reply',
                'gateway-restart', 'same-hash-resume', 'single-broadcast', 'receipt-confirmation',
                'unresolved-signature-refusal', 'expiry-refusal', 'disconnected-owner-refusal',
                'retained-authority-refusal', 'real-quorum-unavailable', 'original-signature-recovery'}
    require(report.get('version') == 1 and set(report.get('cases', [])) == expected
            and len(report['cases']) == len(expected),
            'complete real custody case inventory required')
    print(json.dumps({'status': 'passed', 'exit_code': 0, 'log_path': str(EVIDENCE / 'verify-custody.log'),
                      'runtime_evidence': str(result)}))
    return 0


if __name__ == '__main__':
    started = time.monotonic()
    try:
        sys.exit(main())
    except subprocess.TimeoutExpired:
        print(json.dumps({'status': 'failed', 'exit_code': 124, 'reason': 'bounded command deadline exceeded'}))
        sys.exit(124)
    except (OSError, ValueError, KeyError, RuntimeError) as error:
        reason = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        print(json.dumps({'status': 'failed', 'exit_code': 2, 'reason': reason,
                          'elapsed_seconds': round(time.monotonic() - started, 2)}))
        sys.exit(2)
