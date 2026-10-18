#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import re
import signal
import subprocess
import sys
import time
import uuid

root = pathlib.Path(__file__).resolve().parents[4]
sdk = root / 'client/packages/sdk'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def reject_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('Duplicate input key')
        result[key] = value
    return result


def read_json(path):
    data = pathlib.Path(path).read_bytes()
    if len(data) > 64 * 1024 * 1024:
        raise ValueError('Input byte bound exceeded')
    return json.loads(data, object_pairs_hook=reject_duplicates), data


def exact_source(name):
    source = (root / name).resolve()
    if not source.is_relative_to(root) or not source.is_file():
        raise ValueError('Candidate input outside actual source root')
    return source


def bound_file(definition):
    value, data = read_json(definition['path'])
    if not re.fullmatch(r'[0-9a-f]{64}', definition['sha256']) or sha(data) != definition['sha256']:
        raise ValueError('Frozen input digest mismatch')
    return value, data


def provider(config, name):
    command = config.get('commands', {}).get(name)
    if not command or not command.get('argv') or not re.fullmatch(r'[0-9a-f]{64}', command.get('executableSha256', '')):
        raise ValueError('Actual reset/provider command is missing')
    if sha(pathlib.Path(command['argv'][0]).read_bytes()) != command['executableSha256']:
        raise ValueError('Provider executable changed')
    if not command.get('sourceInputs'):
        raise ValueError('Actual provider source inputs are not pinned')
    for source, expected in command['sourceInputs'].items():
        if sha(pathlib.Path(source).read_bytes()) != expected:
            raise ValueError('Actual provider source input changed')
    for argument in command['argv'][1:]:
        file = pathlib.Path(argument)
        if file.is_file() and sha(file.read_bytes()) != command['sourceInputs'].get(argument):
            raise ValueError('Provider file argument is not source bound')
    result = subprocess.run(command['argv'], cwd=root, env={**os.environ, **command.get('env', {})}, capture_output=True, timeout=180)
    if result.returncode:
        raise ValueError('Actual fixture provider failed')
    if len(result.stdout) > 64 * 1024 * 1024:
        raise ValueError('Provider output bound exceeded')
    return json.loads(result.stdout, object_pairs_hook=reject_duplicates)


def main():
    location = os.environ.get('SDK_PHASE4_CONFIG')
    fixture_location = os.environ.get('SDK_CONNECTED_CONFIG')
    if not location or not fixture_location:
        raise ValueError('SDK_PHASE4_CONFIG and actual SDK_CONNECTED_CONFIG are mandatory')
    config, config_bytes = read_json(location)
    fixture, fixture_bytes = read_json(fixture_location)
    if fixture.get('realm') != 'private_fixture' or config.get('sourceSha256') != fixture.get('sourceSha256') or not re.fullmatch(r'[0-9a-f]{64}', config.get('sourceSha256', '')):
        raise ValueError('Current actual source-bound private fixture required')
    candidate, candidate_bytes = bound_file(config['candidateManifest'])
    if candidate.get('sourceSha256') != config['sourceSha256'] or not candidate.get('files'):
        raise ValueError('Actual candidate manifest/source identity required')
    required_sources = {str(file.relative_to(root)) for file in (sdk / 'src').glob('*') if file.is_file()}
    required_sources.update(str(file.relative_to(root)) for file in (root / 'client/apps/web/src/core/realtime').glob('*.ts'))
    required_sources.update(str(file.relative_to(root)) for file in (sdk / 'test').glob('*') if file.is_file())
    required_sources.update(str(file.relative_to(root)) for file in (sdk / 'scripts').glob('*.py'))
    required_sources.update(str(file.relative_to(root)) for file in (sdk / 'examples').glob('*.mjs'))
    required_sources.add('client/packages/sdk/package.json')
    if not required_sources.issubset(candidate['files']):
        raise ValueError('Candidate manifest omits measured source paths')
    for name, expected in candidate['files'].items():
        if not re.fullmatch(r'[0-9a-f]{64}', expected) or sha(exact_source(name).read_bytes()) != expected:
            raise ValueError('Candidate source differs from frozen manifest')
    bundle_manifest, bundle_manifest_bytes = bound_file(config['browserBundle']['manifest'])
    browser_sources = {name for name in required_sources if name.startswith('client/packages/sdk/src/') or name.startswith('client/apps/web/src/core/realtime/')}
    browser_sources.add('client/packages/sdk/test/browser-measurement.ts')
    if bundle_manifest.get('sourceSha256') != config['sourceSha256'] or not browser_sources.issubset(bundle_manifest.get('inputs', {})):
        raise ValueError('Actual browser bundle lacks current source inputs')
    for name, expected in bundle_manifest['inputs'].items():
        if sha(exact_source(name).read_bytes()) != expected:
            raise ValueError('Measured browser source changed after bundle build')
    bundle_path = pathlib.Path(config['browserBundle']['path']).resolve()
    if sha(bundle_path.read_bytes()) != config['browserBundle']['sha256'] or bundle_manifest.get('bundleSha256') != config['browserBundle']['sha256']:
        raise ValueError('Actual measured browser bundle digest mismatch')
    sdk_manifest, sdk_manifest_bytes = bound_file(config['sdkBuildManifest'])
    if sdk_manifest.get('sourceSha256') != config['sourceSha256'] or not sdk_manifest.get('outputs') or not all(name in sdk_manifest.get('inputs', {}) for name in required_sources if name.startswith('client/packages/sdk/src/')):
        raise ValueError('Actual compiled SDK source binding is missing')
    for name, expected in {**sdk_manifest['inputs'], **sdk_manifest['outputs']}.items():
        if sha(exact_source(name).read_bytes()) != expected:
            raise ValueError('Actual compiled SDK artifact changed')
    device, device_bytes = bound_file(config['device'])
    workload, workload_bytes = bound_file(config['workload'])
    if device.get('classification') != 'midrange' or device.get('physicalDevice') is not True or not workload.get('frozen'):
        raise ValueError('Reviewed physical reference device and frozen actual workload required')
    for name in ['attestReferenceDevice', 'emitFrozenStreamWorkload', 'restartGateway', 'fillAfterGatewayRestart', 'readStreamReference', 'expireCursor73HoursAndRevokeSession', 'backgroundReferenceTab', 'foregroundReferenceTab', 'resetConnectedFixture']:
        if name not in config.get('commands', {}):
            raise ValueError('Missing actual Phase4 provider ' + name)
    run_id = str(uuid.uuid4())
    output = root / 'evidence/production-system/L24/phase4' / run_id
    output.mkdir(parents=True, exist_ok=False, mode=0o700)
    os.chmod(output, 0o700)
    environment = os.environ.copy()
    environment['SDK_PHASE4_RUN_DIR'] = str(output)
    records = []
    def archive(name, data):
        destination = output / name
        destination.write_bytes(data)
        os.chmod(destination, 0o600)
        return {'path': str(destination.relative_to(root)), 'sha256': sha(data)}
    archive('candidate-manifest.json', candidate_bytes)
    archive('bundle-manifest.json', bundle_manifest_bytes)
    archive('sdk-build-manifest.json', sdk_manifest_bytes)
    archive('device-profile.json', device_bytes)
    archive('frozen-workload.json', workload_bytes)
    commands = [
        [sys.executable, str(sdk / 'test/generation.test.py')],
        [sys.executable, str(sdk / 'test/compatibility.test.py')],
        ['node', '--test', str(sdk / 'test/primitives.test.mjs')],
        ['pnpm', '--dir', str(root / 'client/apps/web'), 'exec', 'vitest', 'run', '--config', str(sdk / 'test/vitest.phase2.config.mts')],
        ['node', str(sdk / 'examples/pagination.mjs')],
        ['node', str(sdk / 'examples/snapshot-resume.mjs')],
        ['node', '--test', str(sdk / 'test/connected-services.test.mjs')],
        ['node', str(sdk / 'test/measure-browser.mjs')],
    ]
    code = 0
    deadline = time.monotonic() + 1500
    try:
        for index, command in enumerate(commands):
            if command[-1] == str(sdk / 'test/measure-browser.mjs'):
                refreshed = provider(config, 'resetConnectedFixture')
                refreshed_fixture, refreshed_bytes = read_json(refreshed['fixturePath'])
                if sha(refreshed_bytes) != refreshed.get('fixtureSha256') or refreshed_fixture.get('sourceSha256') != config['sourceSha256'] or refreshed_fixture.get('realm') != 'private_fixture' or not refreshed.get('receiptRefs'):
                    raise ValueError('Fresh genuine fixture lacks source/durable reset evidence')
                for receipt in refreshed['receiptRefs']:
                    if receipt.get('sourceSha256') != config['sourceSha256'] or sha(pathlib.Path(receipt['path']).read_bytes()) != receipt['sha256']:
                        raise ValueError('Actual fixture reset receipt changed')
                environment['SDK_CONNECTED_CONFIG'] = refreshed['fixturePath']
                fixture = refreshed_fixture
                archive('fixture-reset-receipts.json', json.dumps({'sourceSha256': config['sourceSha256'], 'fixtureSha256': refreshed['fixtureSha256'], 'receiptRefs': refreshed['receiptRefs']}, sort_keys=True).encode())
            step_environment = environment.copy()
            if command[-1] == str(sdk / 'examples/snapshot-resume.mjs'):
                step_environment['SDK_RESUME'] = '1'
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ValueError('Lane qualification deadline exceeded')
            process = subprocess.Popen(command, cwd=root, env=step_environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
            try:
                stdout, stderr = process.communicate(timeout=min(900, remaining))
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    stdout, stderr = process.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    stdout, stderr = process.communicate(timeout=5)
                code = 1
            captured = stdout + b'\n' + stderr
            original_sha = sha(captured)
            leaked = False
            secrets = [fixture.get('accessToken'), os.environ.get('SDK_MEASUREMENT_BEARER')]
            for secret in secrets:
                if not secret:
                    continue
                encoded = secret.encode()
                if encoded in captured:
                    captured = captured.replace(encoded, b'[REDACTED]')
                    leaked = True
            artifact = archive(f'step-{index:02d}.log', captured)
            records.append({'command': command, 'exit_code': process.returncode, 'log': artifact, 'unredactedSha256': original_sha, 'credentialLeak': leaked})
            if process.returncode or leaked or code:
                code = process.returncode or 1
                break
    except (ValueError, OSError, KeyError, subprocess.TimeoutExpired):
        code = 1
    source_changed = any(sha(exact_source(name).read_bytes()) != expected for name, expected in candidate['files'].items())
    if source_changed:
        code = 1
    artifacts = [{'path': str(file.relative_to(root)), 'sha256': sha(file.read_bytes())} for file in sorted(output.iterdir()) if file.is_file()]
    manifest = {'task': '4.24', 'scope': 'lane_browser_stream_measurement', 'shared_load_acceptance_deferred_to': '9.1', 'sourceSha256': config['sourceSha256'], 'candidateManifestSha256': sha(candidate_bytes), 'configSha256': sha(config_bytes), 'fixtureSha256': sha(fixture_bytes), 'deviceProfileSha256': sha(device_bytes), 'workloadSha256': sha(workload_bytes), 'bundleManifestSha256': sha(bundle_manifest_bytes), 'sdkBuildManifestSha256': sha(sdk_manifest_bytes), 'sourceChangedDuringQualification': source_changed, 'commands': records, 'artifacts': artifacts, 'exit_code': code}
    archive('manifest.json', json.dumps(manifest, sort_keys=True, indent=2).encode() + b'\n')
    print(json.dumps({'task': '4.24', 'exit_code': code, 'manifest_path': str(output / 'manifest.json')}, sort_keys=True))
    return code


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, KeyError, json.JSONDecodeError):
        print('L24_PHASE4_ACTUAL_INPUT_VALIDATION_FAILED', file=sys.stderr)
        raise SystemExit(1)
