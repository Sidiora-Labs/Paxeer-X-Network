#!/usr/bin/env python3
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

root = Path(__file__).resolve().parents[4]
parser = argparse.ArgumentParser()
parser.add_argument('--phase', type=int, choices=(2, 3), default=2)
args = parser.parse_args()
evidence = root / f'evidence/production-system/L35/phase{args.phase}'
evidence.mkdir(parents=True, exist_ok=True)
required = {
    'TestConnectedOwnerPreferencesAndConsentExport',
    'TestConnectedRevisionInboxRejectsReplayAndResurrection',
    'TestConnectedInputAndTelemetryBoundaries',
    'TestConnectedEncryptedExportWorkerAndSingleUseDownload',
    'TestConnectedFrozenProfileAndVersionedExtensions',
    'TestConnectedGatewayRejectsForgedAssertion',
    'TestConnectedDeviceProofReassignmentAndRevocation',
}
if args.phase == 3:
    required.update({
        'TestConnectedPhase3ConcurrentReplayAndDatabaseInterruption',
        'TestConnectedPhase3ConsentConcurrentReplayAndPrivacy',
        'TestConnectedPhase3DeviceIdentityRevocation',
        'TestConnectedPhase3OwnedHTTPProfileAndWatchlist',
        'TestConnectedPhase3LegacyReplayRollbackAndQuarantine',
        'TestConnectedPhase3DeletionRetryAndImmutableBoundary',
        'TestConnectedPhase3LiveSocketRevocationAndHTTPWrites',
        'TestConnectedPhase3IdentityDependencyInterruption',
        'TestConnectedPhase3PreferenceFieldsAndOlderClients',
    })
command = ['go', '-C', str(root / 'media/user'), 'test', '-p', '1', '-count=1', '-json', './internal/account', '-run', '^(' + '|'.join(sorted(required)) + ')$']
started = time.time()
log = evidence / 'native-connected.jsonl'
with log.open('wb') as output:
    try:
        result = subprocess.run(command, cwd=root, stdout=output, stderr=subprocess.STDOUT, timeout=1740, check=False)
        code = result.returncode
    except subprocess.TimeoutExpired:
        code = 124
passed, failed = set(), []
outputs = {}
for line in log.read_text(errors='replace').splitlines():
    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        continue
    if event.get('Action') == 'output' and event.get('Test'):
        key = (event.get('Package'), event['Test'])
        outputs[key] = outputs.get(key, '') + event.get('Output', '')
    if event.get('Action') == 'pass' and event.get('Test'):
        passed.add(event['Test'])
    if event.get('Action') in ('fail', 'skip'):
        failed.append(event)
observations = []
for (_, test), output in outputs.items():
    for line in output.splitlines():
        for marker in ('L35_HTTP_EVIDENCE ', 'L35_PHASE3_EVIDENCE '):
            if marker in line:
                try:
                    observation = json.loads(line.split(marker, 1)[1])
                    observation['test'] = test
                    observations.append(observation)
                except json.JSONDecodeError:
                    failed.append({'Test': test, 'Action': 'malformed_observation'})
missing = sorted(required - passed)
status = code if code else (1 if failed or missing else 0)
report = {
    'phase': args.phase, 'command': command, 'exit_code': status, 'native_exit_code': code,
    'log_path': str(log), 'started_at': started, 'finished_at': time.time(),
    'passed_tests': sorted(passed), 'missing_required_tests': missing,
    'failures_or_skips': failed,
    'observations': observations,
    'observed_http_request_count': sum(v.get('requestCount', 0) for v in observations),
    'request_count_scope': 'Instrumented HTTP journeys only; store operations and uninstrumented historical adapter tests are not counted.',
    'requirement_observation_tests': {requirement: sorted({v['test'] for v in observations if requirement in v.get('requirements', [])}) for requirement in ('D11_019', 'D11_020', 'D11_021', 'D11_022', 'D11_023', 'D11_024', 'D11_025', 'D11_026', 'D11_027', 'D11_030')},
    'environment': {name: os.environ.get(name) for name in ('GOVERSION', 'GOOS', 'GOARCH', 'GOMAXPROCS')},
    'provider_configuration_present': {name: bool(os.environ.get(name)) for name in ('L35_TEST_POSTGRES', 'L35_TEST_OBJECT_ENDPOINT', 'L35_TEST_OBJECT_ACCESS_KEY', 'L35_TEST_OBJECT_SECRET_KEY', 'L35_TEST_OBJECT_BUCKET', 'L35_PHASE3_GATEWAY_URL', 'L35_PHASE3_STREAM_URL', 'L35_PHASE3_ORIGIN', 'L35_PHASE3_ACCESS_TOKEN', 'L35_PHASE3_WALLET_PRIVATE_KEY', 'L35_PHASE3_OPERATION_ENVIRONMENT')},
    'limitations': sorted({limitation for v in observations for limitation in v.get('limitations', [])}) + ['This focused service gate does not certify joined provider deployment, browser journeys, production capacity, or release.'],
}
(evidence / 'native-connected-result.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'exit_code': status, 'log_path': str(log)}))
sys.exit(status)
