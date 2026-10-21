#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


def validate_archive_manifest(manifest):
    def require(condition):
        if not condition:
            raise ValueError('archive manifest integrity conflict')
    require(manifest['version'] == 1 and manifest['chainId'] == 125 and manifest['complete'] is True)
    for name in ('sourceStream', 'deployment'):
        require(re.fullmatch(r'[a-zA-Z0-9_-]{1,128}', manifest[name]) is not None)
    for name in ('decoderDigest', 'abiDigest', 'codeDigest'):
        require(re.fullmatch(r'[0-9a-f]{64}', manifest[name]) is not None)
    first, last = manifest['fromHeight'], manifest['toHeight']
    require(0 <= first <= last and last-first < 10000 and len(manifest['blocks']) == last-first+1)
    count = 0
    for height, block in enumerate(manifest['blocks'], first):
        require(block['height'] == height and 0 <= block['rawCount'] <= 100000 and 0 <= block['segmentBytes'] <= 64 << 20)
        require(re.fullmatch(r'0x[0-9a-f]{64}', block['blockHash']) is not None)
        require(re.fullmatch(r'[0-9a-f]{64}', block['segmentHash']) is not None)
        count += block['rawCount']
    require(count == manifest['rawCount'])
    return {'raw_count': count, 'final_height': last, 'final_block_hash': manifest['blocks'][-1]['blockHash']}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--phase', choices=(2, 3, 4), type=int, default=2)
    parser.add_argument('--prepare-harness', action='store_true')
    args = parser.parse_args()
    if args.prepare_harness and args.phase != 4:
        raise SystemExit('harness preparation requires phase 4')
    root = Path(__file__).resolve().parents[3]
    required = ['DELIVERY_TEST_DSN', 'DELIVERY_ARCHIVE_ENDPOINT', 'DELIVERY_ARCHIVE_BUCKET',
                'DELIVERY_ARCHIVE_ACCESS_KEY', 'DELIVERY_ARCHIVE_SECRET_KEY', 'DELIVERY_ARCHIVE_FROM',
                'DELIVERY_ARCHIVE_TO', 'DELIVERY_ARCHIVE_STREAM', 'DELIVERY_ARCHIVE_DEPLOYMENT',
                'DELIVERY_ARCHIVE_DECODER_SHA', 'DELIVERY_ARCHIVE_ABI_SHA', 'DELIVERY_ARCHIVE_CODE_SHA']
    if args.phase == 3:
        required = ['DELIVERY_TEST_DSN', 'DELIVERY_TEST_SCHEMA']
    if args.phase == 4:
        required = ['DELIVERY_PHASE4_LEDGER', 'DELIVERY_PHASE4_LEDGER_SHA256', 'DELIVERY_PHASE4_CANDIDATE_SHA256']
    missing = [key for key in required if not os.environ.get(key)]
    if missing:
        raise SystemExit('actual private runtime inputs unavailable: ' + ','.join(missing))
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    commands = [
        ['go', '-C', 'services/launchpad', 'test', '-count=1', '-timeout=180s', '-run', 'Test(WebhookTargetsResumeIndependentlyPostgresTLS|ArchiveActualCanonicalPostgresAndObjectStorage|ArchiveManifestRequiresEveryBlockIncludingEmpty|ShadowExactSetAndDuplicateRejection|MeasuredRunwayFailsUnavailableInputs)$', '../../sdk/launchpad/shared/delivery'],
        ['go', '-C', 'services/launchpad', 'test', '-count=1', '-timeout=60s', '-run', 'Test(CanonicalShadowUsesFrozenDurableIdentity|WebhookStagerRejectsMissingRealProviders)$', './indexer/internal/publisher']]
    if args.phase == 3:
        commands = [
            ['go', '-C', 'services/launchpad', 'test', '-count=1', '-timeout=180s', '-run', 'TestPhase3Actual(RelayCrashAndConsumerRestart|OrderedPoisonAndHundredDuplicates)$', '../../sdk/launchpad/shared/delivery/natsdriver'],
            ['go', '-C', 'services/launchpad', 'test', '-count=1', '-timeout=180s', '-run', 'Test(PostgresDelivery|OrderedPostgres|WebhookTargetsResumeIndependentlyPostgresTLS)$', '../../sdk/launchpad/shared/delivery']]
    timeout = 200
    if args.phase == 4:
        measurement = [sys.executable, 'tools/launchpad/delivery/measure_phase4.py', '--ledger', os.environ['DELIVERY_PHASE4_LEDGER'], '--ledger-sha256', os.environ['DELIVERY_PHASE4_LEDGER_SHA256'], '--candidate-sha256', os.environ['DELIVERY_PHASE4_CANDIDATE_SHA256'], '--output', str(args.output / 'measurement')]
        if args.prepare_harness:
            measurement.append('--prepare-harness')
        else:
            ledger = json.loads(Path(os.environ['DELIVERY_PHASE4_LEDGER']).read_bytes())
            timeout = ledger['budgets']['total_run_timeout_seconds']
            if type(timeout) is not int or timeout <= 0:
                raise SystemExit('approved total measurement timeout required')
        if not args.prepare_harness:
            timeout += ledger['budgets']['cleanup_seconds']
        commands = [[sys.executable, '-m', 'unittest', 'discover', '-s', 'tools/launchpad/delivery', '-p', 'test_measure_phase4.py'], measurement]
    results = []
    for index, command in enumerate(commands):
        log = args.output / f'native-{index}.log'
        with log.open('wb') as output:
            if args.phase == 4:
                os.chmod(log, 0o600)
            result = subprocess.run(command, cwd=root, stdout=output, stderr=subprocess.STDOUT, timeout=timeout, check=False)
        results.append({'command': command, 'exit_code': result.returncode, 'log_path': str(log), 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest()})
        if result.returncode:
            break
    report = {'task': f'{args.phase}.08', 'recorded_at': time.time(), 'native_results': results,
              'pass': len(results) == len(commands) and all(item['exit_code'] == 0 for item in results),
              'production_capacity_az_qualification': ('not_run_harness_preparation' if args.prepare_harness else 'actual_shared_measurement_requested') if args.phase == 4 else 'not_run_focused_task_only',
              'scenario_scope': 'measured_sustained_burst_AZ_and_replay' if args.phase == 4 else ('actual_process_interruptions_duplicates_poison_and_recovery' if args.phase == 3 else 'archive_webhook_shadow')}
    (args.output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    return 0 if report['pass'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
