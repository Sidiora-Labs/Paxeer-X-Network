#!/usr/bin/env python3
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

SOURCE_NAMES = ('advanced-provider.ts', 'best-route-swap.tsx', 'capability-gates.test.tsx', 'connected-journeys.test.tsx', 'custom-route.tsx', 'domain.test.ts', 'domain.ts', 'liquidity-position.tsx', 'measurement-harness.ts', 'measurement-harness.test.ts', 'open-orders-list.tsx', 'order-form.tsx', 'run-connected-chain.py', 'token-universe.ts', 'vitest.l28.config.ts', 'write-gates.test.tsx')


def digest(path):
    if not path.is_file() or path.is_symlink():
        raise ValueError(f'Missing regular source/input: {path}')
    hasher = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1048576), b''):
            hasher.update(chunk)
    return hasher.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--fixture', required=True, type=Path)
    parser.add_argument('--fixture-sha256', required=True)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--measure', action='store_true')
    parser.add_argument('--timeout-seconds', type=int, default=300)
    args = parser.parse_args()
    if args.timeout_seconds < 1 or args.timeout_seconds > 1800:
        raise ValueError('Native execution cap must be1..1800seconds')
    if digest(args.fixture) != args.fixture_sha256:
        raise ValueError('Advanced fixture descriptor digest mismatch')
    fixture = json.loads(args.fixture.read_text())
    if fixture.get('realm') != 'private_fixture':
        raise ValueError('Only an admitted private fixture may run funded qualification')
    if args.measure and (not isinstance(fixture.get('measurement'), dict) or type(fixture['measurement'].get('durationMs')) is not int or not 0 < fixture['measurement']['durationMs'] <= 120000):
        raise ValueError('Measurement requires explicit funded accounts and1..120000ms workload within the preserved240second lifecycle gate')
    source = Path(__file__).resolve().parent
    web = source.parents[2]
    root = web.parents[2]
    files = [source / name for name in SOURCE_NAMES] + [web / 'src/core/clients/meta-ag.ts', web / 'src/widgets/trading/transaction-controls/recovery.ts', web / 'src/widgets/trading/transaction-controls/domain.ts', web / 'src/core/network/deployment.ts', web / 'src/core/network/abis/MetaAGRouter.json', web / 'src/core/network/abis/MetaAGQuoter.json', web / 'src/core/network/abis/PECOROrders.json', web / 'vitest.config.ts', root / 'client/pnpm-lock.yaml']
    sources = {str(path.relative_to(root)): digest(path) for path in files}
    manifest_file = Path(fixture['manifestFile'])
    manifest_sha = digest(manifest_file)
    if manifest_sha != fixture['manifestSha256']:
        raise ValueError('Advanced deployment manifest digest mismatch')
    run_id = str(uuid.uuid4())
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    run = args.output / run_id
    run.mkdir(mode=0o700)
    source_manifest = run / 'source-manifest.json'
    source_manifest.write_text(json.dumps({'version': 1, 'runId': run_id, 'sourceRoot': str(root), 'scope': 'lane_and_direct_dependency_sources', 'files': sources}, indent=2) + '\n')
    source_manifest_sha = digest(source_manifest)
    command = ['pnpm', 'exec', 'vitest', 'run', 'src/widgets/meta-ag/connected-journeys.test.tsx', 'src/widgets/meta-ag/domain.test.ts', 'src/widgets/meta-ag/write-gates.test.tsx', 'src/widgets/meta-ag/capability-gates.test.tsx', 'src/widgets/meta-ag/measurement-harness.test.ts', '--config', 'src/widgets/meta-ag/vitest.l28.config.ts', '--reporter=json', '--outputFile', str((run / 'native.json').resolve())]
    env = os.environ.copy()
    env.update({'L28_CHAIN_FIXTURE_JSON': str(args.fixture.resolve()), 'L28_OBSERVATION_DIRECTORY': str(run.resolve()), 'L28_SOURCE_MANIFEST_JSON': str(source_manifest.resolve()), 'L28_MEASURE': '1' if args.measure else '0', 'L28_PRIVATE_CACHE_DIRECTORY': str((run / 'cache').resolve())})
    started_at = time.time_ns()
    native_exit = None
    failure = None
    with (run / 'native.log').open('xb') as log:
        try:
            completed = subprocess.run(command, cwd=web, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=args.timeout_seconds, check=False)
            native_exit = completed.returncode
        except subprocess.TimeoutExpired:
            failure = 'native_timeout'
    finished_at = time.time_ns()
    native_valid = False
    try:
        report = json.loads((run / 'native.json').read_text())
        assertions = [item for suite in report['testResults'] for item in suite['assertionResults']]
        total = report['numTotalTests']
        native_valid = (native_exit == 0 and type(total) is int and total > 0 and report['success'] is True
                        and report['numPassedTests'] == total and len(assertions) == total
                        and all(item['status'] == 'passed' and item.get('fullName') for item in assertions)
                        and all(suite['status'] == 'passed' for suite in report['testResults'])
                        and all(report.get(name, 0) == 0 for name in ('numFailedTests', 'numPendingTests', 'numTodoTests')))
    except (OSError, ValueError, KeyError, TypeError):
        failure = failure or 'invalid_native_result'
    if {str(path.relative_to(root)): digest(path) for path in files} != sources or digest(manifest_file) != manifest_sha or digest(args.fixture) != args.fixture_sha256:
        failure = 'source_or_authority_changed_during_run'
        native_valid = False
    seen_rpc = set()
    seen_operations = set()
    operation_groups = {}
    observed_scenarios = set()
    actual_writes = 0
    broadcast_buckets = {}
    rpc_calls = 0
    required_raw = ['rpc.jsonl', 'scenarios.jsonl'] + (['operations.jsonl', 'measurement.json'] if args.measure else [])
    raw_valid = True
    for name in required_raw:
        path = run / name
        if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
            failure = failure or f'missing_actual_observation:{name}'
            raw_valid = False
            continue
        if name.endswith('.jsonl'):
            count = 0
            with path.open() as raw_capture:
              for line in raw_capture:
                  value = json.loads(line)
                  if value.get('runId') != run_id or value.get('manifestSha256') != manifest_sha or value.get('sourceManifestSha256') != source_manifest_sha or value.get('chainId') != 125:
                      raise ValueError('Observation identity mismatch')
                  if name == 'rpc.jsonl':
                      sequence = value.get('sequence')
                      if type(sequence) is not int or sequence in seen_rpc or sequence <= 0 or value.get('finishedAtMs', -1) < value.get('startedAtMs', 0):
                          raise ValueError('Duplicated or malformed actual RPC capture')
                      seen_rpc.add(sequence)
                      request = json.loads(value['requestBody'])
                      if request.get('id') != sequence or request.get('method') != value.get('method'):
                          raise ValueError('Actual RPC request identity mismatch')
                      if value.get('responseBody') is not None:
                          response = json.loads(value['responseBody'])
                          if response.get('id') != sequence:
                              raise ValueError('Actual RPC response identity mismatch')
                      rpc_calls += 1
                      if request['method'] == 'eth_sendRawTransaction':
                          actual_writes += 1
                          second = int(value['startedAtUnixMs'] // 1000)
                          broadcast_buckets[second] = broadcast_buckets.get(second, 0) + 1
                  if name == 'operations.jsonl' and value.get('type') == 'operation':
                      sequence = value.get('sequence')
                      if type(sequence) is not int or sequence < 0 or sequence in seen_operations:
                          raise ValueError('Duplicate actual operation observation')
                      seen_operations.add(sequence)
                      group = operation_groups.setdefault(value['kind'], {'offered': 0, 'admitted': 0, 'verified': 0, 'latencyMs': []})
                      group['offered'] += 1
                      planned, started, finished = value['plannedAtMs'], value['startedAtMs'], value['finishedAtMs']
                      if not all(type(item) in (int,float) and math.isfinite(item) for item in (planned,finished)) or finished < planned:
                          raise ValueError('Invalid actual operation interval')
                      if value['outcome'] == 'dropped':
                          if started is not None:
                              raise ValueError('Dropped request falsely admitted')
                      else:
                          if type(started) not in (int,float) or not math.isfinite(started) or started < planned or finished < started:
                              raise ValueError('Invalid admitted operation interval')
                          if value['outcome'] not in ('verified','rejected','reverted','unknown','no-route','unsupported'):
                              raise ValueError('Unknown actual operation outcome')
                          group['admitted'] += 1
                          group['verified'] += value['outcome'] == 'verified'
                          group['latencyMs'].append(finished - planned)
                  if name == 'scenarios.jsonl':
                      observed_scenarios.add(value.get('scenario'))
                  count += 1
            if count == 0:
                raw_valid = False
    for required in ('scenario_1','scenario_2','scenario_3_eligible_degraded','scenario_3_later_canonical_fill','scenario_4_cancel_wins','scenario_4_fill_wins','scenario_5','scenario_6','scenario_7','scenario_8_actual_race','scenario_8_cancel_concurrency'):
        if required not in observed_scenarios:
            failure = failure or f'missing_actual_scenario:{required}'
            raw_valid = False
    if args.measure and raw_valid:
        measured = json.loads((run / 'measurement.json').read_text())
        if measured.get('runId') != run_id or measured.get('manifestSha256') != manifest_sha or measured.get('sourceManifestSha256') != source_manifest_sha or measured.get('globalCapacityAccepted') is not False or measured.get('quoteApiCapacityAccepted') is not False or measured.get('finalAcceptanceOwner') != 'serial9.1':
            raise ValueError('Measured summary authority mismatch')
        profile = fixture['measurement']['profile']
        if profile not in ('peak','burst') or measured.get('profile') != profile:
            raise ValueError('Measured global workload profile mismatch')
        write_limit = 2000 if profile == 'peak' else 4000
        if any(count > write_limit for count in broadcast_buckets.values()):
            failure = 'lane_actual_broadcast_subset_exceeds_global_write_budget'
            raw_valid = False
        duration = measured['durationMs']
        if type(duration) not in (int,float) or not math.isfinite(duration) or duration <= 0:
            raise ValueError('Missing actual workload duration')
        if set(operation_groups) != set(measured['operations']):
            raise ValueError('Incomplete raw operation mix')
        for kind, observed in operation_groups.items():
            summary = measured['operations'][kind]
            for count_name in ('offered','admitted','verified'):
                if summary[count_name] != observed[count_name]:
                    raise ValueError('Measured summary disagrees with raw counts')
            latencies = sorted(observed['latencyMs'])
            for field, quantile in (('p95Ms', .95), ('p99Ms', .99)):
                expected = latencies[math.ceil(len(latencies) * quantile) - 1] if latencies else None
                if summary[field] != expected:
                    raise ValueError('Measured percentile disagrees with actual raw timings')
            if not math.isclose(summary['verifiedPerSecond'], observed['verified'] * 1000 / duration, rel_tol=1e-12, abs_tol=1e-12):
                raise ValueError('Verified throughput counts failures or ignores elapsed time')
    artifacts = [{'path': str(path.relative_to(run)), 'sha256': digest(path), 'bytes': path.stat().st_size} for path in sorted(run.iterdir()) if path.is_file() and not path.is_symlink()]
    exit_code = 0 if native_valid and raw_valid and failure is None else 1
    result = {'version': 1, 'runId': run_id, 'command': command, 'exit_code': exit_code, 'native_exit_code': native_exit,
              'fixture_sha256': args.fixture_sha256, 'manifest_sha256': manifest_sha, 'source_manifest_sha256': source_manifest_sha,
              'started_at_unix_ns': started_at, 'finished_at_unix_ns': finished_at, 'native_assertions_verified': native_valid,
              'raw_observations_bound': raw_valid, 'rpc_calls': rpc_calls, 'actual_transaction_broadcast_requests': actual_writes, 'broadcasts_by_unix_second': broadcast_buckets, 'observed_scenarios': sorted(observed_scenarios), 'budget_scope': 'component RPC calls and real broadcasts only; no API/global double counting', 'failure': failure, 'artifacts': artifacts,
              'log_path': str((run / 'native.log').resolve()), 'native_result': str((run / 'native.json').resolve()),
              'production_qualified': False, 'global_acceptance_owner': 'serial9.1',
              'release_blockers': ['Current deployment/device/vendor authority and mounted UI proof required', 'Final shared load/restore24hsoak and200000socket surge evidence deferred to serial9.1']}
    (run / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(str(run / 'result.json'))
    return exit_code


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f'Advanced measurement failed input: {error}', file=sys.stderr)
        sys.exit(1)
