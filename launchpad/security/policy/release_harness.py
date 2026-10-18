from __future__ import annotations

import argparse
import hashlib
import hmac
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from fractions import Fraction

ROOT = Path(__file__).resolve().parents[2]
LOAD = ROOT / 'tools/production/load'
sys.path.insert(0, str(LOAD))
from capture import read_manifest, iter_artifact, sha_file
from phase2_types import Bindings, bound_document, canonical, unique

HEX = re.compile(r'^[0-9a-f]{64}$')
PROFILES = {'steady': (10000, 2000, 2000), 'surge': (20000, 4000, 4000)}
SCENARIOS = {
    1: ('policy', 'TestPrivateBoundaryRejectsActorHeadersWithoutWorkload'),
    2: ('policy', 'TestRealDeadLetterDenialDuplicateReplayAndRedaction'),
    3: ('policy', 'TestRealDeadLetterDenialDuplicateReplayAndRedaction'),
    4: ('policy', 'TestRedisDistributedBudget'),
    5: ('policy', 'TestRealRedisAtomicExpiryCrashRecovery'),
    6: ('policy', 'TestRealRedisAtomicExpiryCrashRecovery'),
    7: ('policy', 'TestOversizeHTTPMutationHasZeroDatabaseWrites'),
    8: ('policy', 'TestRedirectAndTranslatedPrivateAddressesFailClosed'),
    9: ('secrets', 'TestProductionStartupRequiredSecrets'),
    10: ('policy', 'TestRealMountedRotationRetirementRevocationAndRecovery'),
    11: ('policy', 'TestRealStatsRoleDeniedIdentityInsert'),
    12: ('policy', 'TestActualSecurityLogSecretRedaction'),
    13: ('policy', 'TestV2PrivilegedMutationRollsBackOnRealAuditFailure'),
    14: ('policy', 'TestThreatRegistryBlocksUnclassifiedOrCriticalRoute'),
    15: ('policy', 'TestRealRedisAtomicExpiryCrashRecovery'),
}
RUNTIME_BLOCKERS = {
    1: 'Approved private-port ingress and running workload identity boundary.',
    2: 'Real user-session/operator gateway replay with a before/after job ledger.',
    3: 'Running public router denial and unchanged durable queue snapshot.',
    4: 'Running replicas, authenticated same-principal writes and exact budget policy.',
    5: 'Approved Redis fault controller and native counter recovery capture.',
    6: 'Actual 2000-write-per-second Redis-loss arrival stream and native admission ledger.',
    7: 'Actual production route/schema body limit and durable before/after state digest.',
    8: 'Vendor redirect journey with native connection-denial observations.',
    9: 'Actual service startup under unavailable configured secret provider.',
    10: 'Actual webhook traffic, version overlap/revocation and provider cache freshness.',
    11: 'Actual stats login role and unchanged identity/stats database snapshots.',
    12: 'Complete indexed log and trace exports covering every canary probe request.',
    13: 'Actual feature-cap/audit transaction interruption and durable rollback.',
    14: 'Actual complete release route/threat registry and authorization classification.',
    15: 'Shared Redis-loss stream containing replay attacks, cached reads and native admission facts.',
}


def exact(value, names):
    if not isinstance(value, dict) or set(value) != set(names):
        raise ValueError('exact record fields required')


def integer(value, minimum=0):
    if type(value) is not int or value < minimum:
        raise ValueError('exact bounded integer required')
    return value


def identifier(value):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}', value):
        raise ValueError('opaque bounded identifier required')
    return value


def percentile(values, numerator, denominator):
    if not values:
        raise ValueError('actual latency samples absent')
    ordered = sorted(values)
    return ordered[(len(ordered) * numerator + denominator - 1) // denominator - 1]


def ordinary(path):
    path = Path(path)
    if not path.is_absolute() or not path.is_file():
        raise ValueError('absolute ordinary source path required')
    for part in (path, *path.parents):
        if part.is_symlink():
            raise ValueError('symlink source denied')
    return path


def owned_output(path):
    path = Path(path)
    if not path.is_absolute():
        raise ValueError('absolute fresh output directory required')
    for parent in path.parents:
        if parent.is_symlink():
            raise ValueError('symlink output denied')
    path.mkdir(mode=0o700, parents=True, exist_ok=False)
    return path


def write_json(path, value):
    with Path(path).open('xb') as output:
        os.chmod(path, 0o600)
        output.write(canonical(value) + b'\n')
        output.flush()
        os.fsync(output.fileno())


def rows(path, expected_sha):
    path = ordinary(path)
    if not HEX.fullmatch(expected_sha) or sha_file(path) != expected_sha:
        raise ValueError('immutable raw artifact digest differs')
    with path.open('rb') as source:
        while True:
            line = source.readline(1048577)
            if not line:
                break
            if len(line) > 1048576 or not line.endswith(b'\n'):
                raise ValueError('bounded complete raw observation required')
            yield json.loads(line, object_pairs_hook=unique)
    if sha_file(path) != expected_sha:
        raise ValueError('raw artifact changed during measurement')


def config(path, checksum):
    value = bound_document(ordinary(path), checksum)
    exact(value, ('version', 'bindings', 'profile', 'manifest_path', 'manifest_sha256',
                  'timings_path', 'timings_sha256', 'resources_path', 'resources_sha256',
                  'redis_fault_id', 'redis_control_sha256', 'policy_version', 'join_key_env',
                  'runtime_scenarios_path', 'runtime_scenarios_sha256', 'authority_path', 'authority_sha256'))
    if value['version'] != 1 or value['profile'] not in PROFILES:
        raise ValueError('frozen profile and supported configuration required')
    value['bindings'] = Bindings(**value['bindings'])
    identifier(value['redis_fault_id'])
    identifier(value['policy_version'])
    if not HEX.fullmatch(value['redis_control_sha256']) or not re.fullmatch(r'[A-Z][A-Z0-9_]{0,127}', value['join_key_env']):
        raise ValueError('approved fault authority and join-secret reference required')
    key = os.environ.get(value['join_key_env'], '')
    if len(key.encode()) < 32:
        raise ValueError('native capture join credential unavailable')
    value['join_key'] = key.encode()
    authority = bound_document(ordinary(value['authority_path']), value['authority_sha256'])
    exact(authority, ('version', 'bindings', 'run_id', 'replica_ids', 'generator_hosts', 'middleware_boundary', 'scenario_ids'))
    if authority['version'] != 1 or authority['bindings'] != value['bindings'].__dict__ or authority['middleware_boundary'] != 'policy_native_decision' or sorted(authority['scenario_ids']) != list(range(1, 16)):
        raise ValueError('approved current runtime authority differs')
    if not authority['replica_ids'] or len(set(authority['replica_ids'])) != len(authority['replica_ids']) or len(set(authority['generator_hosts'])) < 4:
        raise ValueError('real replica and distributed generator inventory required')
    for name in [authority['run_id'], *authority['replica_ids'], *authority['generator_hosts']]:
        identifier(name)
    value['authority'] = authority
    return value


def timings(configuration, run_id, replica_ids):
    result = {}
    names = ('run_id', 'replica_id', 'candidate_sha256', 'policy_version', 'request_reference',
             'stage', 'observed_utc_ns', 'duration_ns', 'allowed', 'backend_unavailable')
    for row in rows(configuration['timings_path'], configuration['timings_sha256']):
        exact(row, names)
        if row['run_id'] != run_id or row['replica_id'] not in replica_ids or row['candidate_sha256'] != configuration['bindings'].candidate or row['policy_version'] != configuration['policy_version']:
            raise ValueError('foreign native middleware observation')
        if not HEX.fullmatch(row['request_reference']) or row['stage'] not in ('authorization', 'limiter') or type(row['allowed']) is not bool or type(row['backend_unavailable']) is not bool:
            raise ValueError('native middleware record malformed')
        integer(row['duration_ns'], 1)
        integer(row['observed_utc_ns'], 1)
        identity = (row['request_reference'], row['stage'])
        if identity in result:
            raise ValueError('duplicate native middleware decision')
        result[identity] = row
    if not result:
        raise ValueError('native authorization and limiter evidence absent')
    return result


def resources(configuration, manifest):
    samples = {}
    for row in rows(configuration['resources_path'], configuration['resources_sha256']):
        exact(row, ('run_id', 'replica_id', 'observed_utc_ns', 'cpu_cores', 'memory_bytes',
                    'memory_limit_bytes', 'oom_kills', 'generator_cpu_fraction', 'generator_nic_fraction'))
        if row['run_id'] != manifest['run_id'] or row['replica_id'] not in manifest['gateway_replica_ids']:
            raise ValueError('foreign native resource sample')
        stamp = integer(row['observed_utc_ns'], 1)
        if not int(manifest['started_utc_ns']) <= stamp < int(manifest['ended_utc_ns']):
            raise ValueError('resource observation outside capture')
        integer(row['cpu_cores'], 1)
        integer(row['memory_limit_bytes'], 1)
        if integer(row['memory_bytes']) >= row['memory_limit_bytes'] or integer(row['oom_kills']):
            raise ValueError('memory or OOM budget failed')
        for field in ('generator_cpu_fraction', 'generator_nic_fraction'):
            if type(row[field]) not in (int, float) or not math.isfinite(row[field]) or not 0 <= row[field] <= .7:
                raise ValueError('generator saturation prevents capacity qualification')
        identity = (row['replica_id'], (stamp-int(manifest['started_utc_ns'])) // 1000000000)
        if identity in samples:
            raise ValueError('duplicate replica resource interval')
        samples[identity] = row
    seconds = (int(manifest['ended_utc_ns'])-int(manifest['started_utc_ns']) + 999999999) // 1000000000
    expected = {(replica, second) for replica in manifest['gateway_replica_ids'] for second in range(seconds)}
    if set(samples) != expected:
        raise ValueError('every replica-second needs native resource evidence')
    return {'replicas': len(manifest['gateway_replica_ids']), 'sample_count': len(samples),
            'cpu_cores_max': sum(max(r['cpu_cores'] for (rep, _), r in samples.items() if rep == replica) for replica in manifest['gateway_replica_ids']),
            'memory_limit_bytes_max': sum(max(r['memory_limit_bytes'] for (rep, _), r in samples.items() if rep == replica) for replica in manifest['gateway_replica_ids'])}


def capacity(configuration):
    manifest = read_manifest(configuration['manifest_path'], configuration['manifest_sha256'], bindings=configuration['bindings'])
    authority = configuration['authority']
    if manifest['run_id'] != authority['run_id'] or set(manifest['gateway_replica_ids']) != set(authority['replica_ids']) or set(manifest['generator_hosts']) != set(authority['generator_hosts']):
        raise ValueError('capture runtime topology differs from approved authority')
    start, end = int(manifest['started_utc_ns']), int(manifest['ended_utc_ns'])
    if end-start < 60000000000:
        raise ValueError('at least sixty actual wallclock seconds required')
    matching_faults = [r for r in iter_artifact(manifest, 'fault_control', record='FaultInterval') if r['fault_id'] == configuration['redis_fault_id']]
    if len(matching_faults) != 1:
        raise ValueError('one exact approved Redis outage interval required')
    fault = matching_faults[0]
    left, right = int(fault['observed_start_utc_ns']), int(fault['observed_end_utc_ns'])
    if fault['run_id'] != manifest['run_id'] or fault['action'] != 'disconnect' or fault['outcome'] != 'recovered' or fault['native_control_sha256'] != configuration['redis_control_sha256'] or not start <= left < right < end or int(fault['recovery_utc_ns']) < right:
        raise ValueError('genuine recoverable Redis fault-control evidence required')
    native = timings(configuration, manifest['run_id'], manifest['gateway_replica_ids'])
    seen, consumed = set(), set()
    reads, auth, limiter = [], [], []
    admitted = writes = quotes = rejected_attacks = outage_writes = outage_reads = outage_attacks = 0
    per_second = {}
    fields = ('run_id', 'request_id', 'route_class', 'valid', 'admitted', 'http_status',
              'scheduled_utc_ns', 'started_utc_ns', 'ended_utc_ns', 'replica_id', 'cached_read', 'candidate_sha256')
    for row in iter_artifact(manifest, 'http_arrivals'):
        exact(row, fields)
        identity = identifier(row['request_id'])
        if identity in seen or row['run_id'] != manifest['run_id'] or row['candidate_sha256'] != configuration['bindings'].candidate or row['replica_id'] not in manifest['gateway_replica_ids']:
            raise ValueError('foreign or duplicate HTTP arrival')
        seen.add(identity)
        if row['route_class'] not in ('public-read', 'account-read', 'account-write', 'quote', 'operator', 'authentication') or any(type(row[k]) is not bool for k in ('valid', 'admitted', 'cached_read')):
            raise ValueError('unclassified actual request')
        scheduled, begun, finished = (integer(row[k], 1) for k in ('scheduled_utc_ns', 'started_utc_ns', 'ended_utc_ns'))
        if not start <= scheduled <= begun < finished or scheduled >= end or begun >= end or finished > end+10000000000:
            raise ValueError('actual arrival/deadline differs from capture')
        elapsed = finished-scheduled
        if begun-scheduled > 5000000:
            raise ValueError('generator missed open-loop schedule; coordinated omission prohibited')
        status = integer(row['http_status'], 200)
        if status > 599 or row['admitted'] and status >= 300 or not row['admitted'] and status < 400 or not row['valid'] and row['admitted']:
            raise ValueError('attack or HTTP error counted as admitted work')
        reference = hmac.new(configuration['join_key'], b'request-id\x00'+identity.encode(), hashlib.sha256).hexdigest()
        required_stages = (('limiter',) if row['route_class'] == 'public-read' else ('authorization',) if row['route_class']=='operator' else ('authorization', 'limiter'))
        measured = {}
        for stage in required_stages:
            key = (reference, stage)
            if key not in native:
                if stage == 'limiter' and status in (401, 403) and (reference, 'authorization') in native and not native[(reference, 'authorization')]['allowed']:
                    continue
                raise ValueError('HTTP arrival lacks native middleware decision')
            observation = native[key]
            if observation['replica_id'] != row['replica_id'] or not begun-5000000 <= observation['observed_utc_ns'] <= finished+5000000 or observation['duration_ns'] > elapsed+10000000:
                raise ValueError('native middleware timing cannot be joined to HTTP request')
            consumed.add(key)
            measured[stage] = observation
            (auth if stage == 'authorization' else limiter).append(observation['duration_ns'])
        if row['admitted'] and any(not observation['allowed'] for observation in measured.values()):
            raise ValueError('admitted HTTP request contradicts native admission')
        loss = left <= begun < right
        if loss:
            if row['route_class'] == 'account-write':
                outage_writes += 1
            if not row['valid'] and row['route_class'] == 'operator':
                outage_attacks += 1
            if row['route_class'] in ('account-write', 'quote', 'authentication') and (row['admitted'] or status not in (401, 403, 503)):
                raise ValueError('protected mutation did not fail closed during Redis loss')
            if row['admitted']:
                if row['route_class'] != 'public-read' or not row['cached_read'] or not measured['limiter']['backend_unavailable']:
                    raise ValueError('Redis-loss admission lacks cached-read lease boundary')
                outage_reads += 1
                key = (row['replica_id'], begun // 1000000000)
                per_second[key] = per_second.get(key, 0)+1
                if per_second[key] > 100:
                    raise ValueError('origin cached-read fallback exceeded 100 RPS')
        elif row['valid'] and row['admitted']:
            admitted += 1
            if row['route_class'] in ('public-read', 'account-read'):
                reads.append(elapsed)
            elif row['route_class'] == 'account-write':
                writes += 1
            elif row['route_class'] == 'quote':
                quotes += 1
            else:
                raise ValueError('unsupported work counted in API capacity')
        if not row['valid']:
            rejected_attacks += 1
    if consumed != set(native):
        raise ValueError('native admission ledger has unmatched/omitted requests')
    normal_ns, loss_ns = end-start-(right-left), right-left
    targets = PROFILES[configuration['profile']]
    for count, rps in zip((admitted, writes, quotes), targets):
        if Fraction(count*1000000000, normal_ns) < rps:
            raise ValueError('valid launch workload below frozen demand')
    if Fraction(outage_writes*1000000000, loss_ns) < 2000 or not outage_reads or not outage_attacks or not rejected_attacks:
        raise ValueError('Redis-loss write/attack/cached-read workload incomplete')
    measured = {'read_p95_ns': percentile(reads, 95, 100), 'read_p99_ns': percentile(reads, 99, 100),
                'authorization_p99_ns': percentile(auth, 99, 100), 'limiter_p99_ns': percentile(limiter, 99, 100)}
    if measured['read_p95_ns'] > 150000000 or measured['read_p99_ns'] > 400000000 or measured['authorization_p99_ns'] > 10000000 or measured['limiter_p99_ns'] > 10000000:
        raise ValueError('frozen read or middleware latency budget exceeded')
    for path_key, sha_key in (('manifest_path','manifest_sha256'),('timings_path','timings_sha256'),('resources_path','resources_sha256'),('authority_path','authority_sha256')):
        if sha_file(ordinary(configuration[path_key])) != configuration[sha_key]:
            raise ValueError('input changed during measurement')
    return {'profile': configuration['profile'], 'actual_requests': len(seen), 'valid_admitted': admitted,
            'valid_writes': writes, 'valid_quotes': quotes, 'rejected_attacks': rejected_attacks,
            'outage_write_attempts': outage_writes, 'outage_cached_reads': outage_reads,
            'outage_replay_attacks': outage_attacks, 'normal_duration_ns': normal_ns, 'outage_duration_ns': loss_ns,
            'latency_ns': measured, 'resources': resources(configuration, manifest),
            'scope': manifest['scope'], 'lane_measurements_passed': True, 'capacity_qualified': False, 'final_gate': '9.1'}


def equal_state(facts):
    if not HEX.fullmatch(facts['before_sha256']) or facts['before_sha256'] != facts['after_sha256']:
        raise ValueError('denied/interrupted operation changed durable state')


def scenario_facts(scenario, facts, raw_paths):
    if scenario in (1, 2, 3, 7, 13):
        required = {'before_sha256', 'after_sha256', 'http_status'}
        if not required.issubset(facts):
            raise ValueError('durable before/after and HTTP observations absent')
        equal_state(facts)
        statuses = {1: (401, 403), 2: (403,), 3: (401, 403), 7: (413,), 13: (503,)}
        if facts['http_status'] not in statuses[scenario]:
            raise ValueError('observed authorization/parsing/audit failure differs')
        if scenario == 2 and integer(facts['replay_jobs_delta']) != 0:
            raise ValueError('ordinary session created replay work')
        if scenario == 3 and integer(facts['queue_before']) != integer(facts['queue_after']):
            raise ValueError('unauthenticated drain changed queue cardinality')
        if scenario == 7 and integer(facts['body_bytes']) != 65537:
            raise ValueError('exact body boundary scenario absent')
        if scenario == 13 and integer(facts['audit_rows_delta']) != 0:
            raise ValueError('interrupted transaction retained an audit side effect')
    elif scenario == 4:
        events = facts['redis_admissions']
        if len(events) != 61 or facts['policy_class'] != 'account-write' or facts['cost'] != 1:
            raise ValueError('exact same-principal account-write budget scenario required')
        replicas = set()
        tokens = Fraction(10)
        previous = integer(events[0]['redis_ms'], 1)
        first = previous
        for event in events:
            exact(event, ('redis_ms', 'admitted', 'replica_id', 'principal_reference'))
            now = integer(event['redis_ms'], 1)
            if now < previous or now-first > 60000 or type(event['admitted']) is not bool or event['principal_reference'] != events[0]['principal_reference']:
                raise ValueError('actual Redis-time principal history differs')
            tokens = min(Fraction(10), tokens+Fraction(now-previous,1000))
            expected = tokens >= 1
            if event['admitted'] != expected:
                raise ValueError('replica bypasses exact token policy')
            if expected:
                tokens -= 1
            replicas.add(identifier(event['replica_id']))
            previous = now
        if len(replicas) < 2:
            raise ValueError('budget replica sharing not exercised')
    elif scenario == 5:
        if facts['redis_recovered'] is not True or integer(facts['permanent_keys']) != 0 or integer(facts['local_cache_entries']) > 10000 or not facts['counter_ttls_ms']:
            raise ValueError('Redis crash/recovery counter observations absent')
        if any(not 1 <= integer(ttl,1) <= 120000 for ttl in facts['counter_ttls_ms']):
            raise ValueError('counter expiration lost at crash boundary')
    elif scenario in (6, 15):
        if facts['native_arrivals_bound'] is not True:
            raise ValueError('shared arrival/admission loss evidence not bound')
    elif scenario == 8:
        if facts['redirect_target'] != '169.254.169.254' or facts['connected_to_target'] is not False or facts['typed_denial'] is not True:
            raise ValueError('SSRF redirect reached a prohibited destination')
    elif scenario == 9:
        if facts['provider'] != 'vault' or integer(facts['startup_exit_code']) == 0 or facts['ready'] is not False or facts['env_fallback'] is not False:
            raise ValueError('unsupported secret provider used a fallback')
    elif scenario == 10:
        if not 0 < integer(facts['overlap_ns'],1) <= 900000000000 or integer(facts['refresh_ns']) > 60000000000 or integer(facts['revoke_ns']) > 60000000000 or facts['old_accepted_after_retirement'] is not False or facts['unsigned_accepted'] is not False or facts['outgoing_version'] != facts['newest_active_version'] or not facts['audiences']:
            raise ValueError('rotation lifetime/revocation/verification boundary failed')
    elif scenario == 11:
        equal_state(facts)
        if facts['sqlstate'] != '42501' or facts['database_superuser'] is not False or facts['database_owner'] is not False or facts['stats_schema_unchanged'] is not True or facts['identity_schema_unchanged'] is not True:
            raise ValueError('actual runtime data credential crossed schema authority')
    elif scenario == 12:
        env = facts['canary_env']
        if not isinstance(env, list) or len(env) < 5 or len(set(env)) != len(env):
            raise ValueError('unique protected-header canary references required')
        canaries = []
        for name in env:
            if not re.fullmatch(r'[A-Z][A-Z0-9_]{0,127}',name) or not os.environ.get(name):
                raise ValueError('actual sent protected-header canary unavailable')
            canaries.append(os.environ[name].encode())
        if len(set(canaries)) != len(canaries):
            raise ValueError('header canaries must be unique')
        covered = set(facts['request_references'])
        if not covered or any(not HEX.fullmatch(value) for value in covered):
            raise ValueError('native sent-request references required')
        for kind in ('indexed_logs', 'indexed_traces'):
            selected = [p for artifact_kind,p in raw_paths if artifact_kind==kind]
            if not selected:
                raise ValueError('indexed log/trace export unavailable')
            observed = set()
            for path in selected:
                with path.open('rb') as source:
                    carry = b''
                    while True:
                        block = source.read(1048576)
                        if not block:
                            break
                        combined = carry+block
                        if any(canary in combined for canary in canaries):
                            raise ValueError('protected-header secret found in indexed telemetry')
                        observed.update(reference for reference in covered if reference.encode() in combined)
                        carry = combined[-max(max(map(len,canaries)),64):]
            if observed != covered:
                raise ValueError('indexed export does not cover every canary request')
    elif scenario == 14:
        if integer(facts['policy_validation_exit_code']) == 0 or facts['unclassified_endpoint'] is not True or facts['entered_release_manifest'] is not False:
            raise ValueError('unclassified endpoint entered release authority')
    else:
        raise ValueError('unregistered security scenario')


def runtime_scenarios(configuration):
    document = bound_document(ordinary(configuration['runtime_scenarios_path']), configuration['runtime_scenarios_sha256'])
    exact(document, ('version','run_id','bindings','scenarios'))
    if document['version'] != 1 or document['run_id'] != configuration['authority']['run_id'] or document['bindings'] != configuration['bindings'].__dict__:
        raise ValueError('runtime scenario identity differs from candidate')
    results = {}
    for row in document['scenarios']:
        exact(row, ('scenario_id','started_utc_ns','ended_utc_ns','raw_artifacts','facts'))
        number = integer(row['scenario_id'],1)
        if number not in SCENARIOS or number in results or integer(row['ended_utc_ns'],1) <= integer(row['started_utc_ns'],1):
            raise ValueError('duplicate/unregistered scenario or missing actual interval')
        if not isinstance(row['facts'],dict) or not row['raw_artifacts']:
            raise ValueError('scenario observations and raw provider artifacts required')
        paths,seen = [],set()
        for artifact in row['raw_artifacts']:
            exact(artifact, ('kind','path','sha256'))
            path = ordinary(artifact['path'])
            if path in seen or not HEX.fullmatch(artifact['sha256']) or sha_file(path) != artifact['sha256']:
                raise ValueError('raw scenario provider artifact differs')
            seen.add(path)
            paths.append((artifact['kind'],path))
        scenario_facts(number,row['facts'],paths)
        for artifact in row['raw_artifacts']:
            if sha_file(ordinary(artifact['path'])) != artifact['sha256']:
                raise ValueError('raw scenario artifact changed during validation')
        results[number] = {'scenario_id':number,'observed_assertions_passed':True,
                           'raw_sha256':[a['sha256'] for a in row['raw_artifacts']], 'release_qualified':False}
    if set(results) != set(SCENARIOS):
        raise ValueError('every linked runtime scenario requires passing raw evidence or blocking status')
    return [results[number] for number in sorted(results)]


def source_binding(path,checksum):
    value = bound_document(ordinary(path),checksum)
    exact(value, ('version','candidate_sha256','interface_sha256','files'))
    if value['version'] != 1 or any(not HEX.fullmatch(value[k]) for k in ('candidate_sha256','interface_sha256')) or not value['files']:
        raise ValueError('exact admitted candidate source binding required')
    for relative,expected in value['files'].items():
        if Path(relative).is_absolute() or '..' in Path(relative).parts or not HEX.fullmatch(expected) or sha_file(ordinary(ROOT / relative)) != expected:
            raise ValueError('native source differs from admitted candidate')
    required = {str(p.relative_to(ROOT)) for p in (ROOT/'security/policy').glob('*.go')}
    required |= {str(p.relative_to(ROOT)) for p in (ROOT/'shared/secrets').glob('*.go')}
    required |= {'security/policy/release_harness.py','security/policy/test_release_harness.py','security/go.mod','security/go.sum','shared/go.mod','shared/go.sum','go.work','tools/production/load/capture.py','tools/production/load/phase2_types.py','tools/production/load/workload.py','tools/production/load/final-shared-capture.schema.json'}
    required |= {str(p.relative_to(ROOT)) for p in (ROOT/'shared/auth').glob('*.go')}
    if not required.issubset(value['files']):
        raise ValueError('security implementation/test binding incomplete')
    return value


def components(binding_path,binding_sha,go_binary,go_sha,output):
    binding = source_binding(binding_path,binding_sha)
    program = ordinary(go_binary)
    if not HEX.fullmatch(go_sha) or sha_file(program) != go_sha or not os.access(program,os.X_OK):
        raise ValueError('exact native Go toolchain unavailable')
    if not os.environ.get('L45_DATABASE_URL'):
        raise ValueError('explicit real component PostgreSQL credential required')
    output = owned_output(output)
    groups = {'policy': ROOT/'security', 'secrets':ROOT/'shared'}
    summaries = {}
    deadline = time.monotonic()+1500
    for name,directory in groups.items():
        names = sorted({test for package,test in SCENARIOS.values() if package==name} | ({'TestNativeAuthorizationCaptureUsesRealPrivateIdentity','TestNativeLimiterCaptureUsesActualBackendOutcome','TestMeasuredAdmissionCaptureFailureDeniesRequest'} if name=='policy' else set()))
        command = [str(program),'test','-json','-count=1','./'+name,'-run','^('+'|'.join(names)+')$']
        log_path = output/(name+'-native.ndjson')
        started = time.time_ns()
        with log_path.open('xb') as log:
            os.chmod(log_path,0o600)
            process = subprocess.Popen(command,cwd=directory,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
            try:
                exit_code = process.wait(timeout=max(.001,deadline-time.monotonic()))
            except subprocess.TimeoutExpired:
                os.killpg(process.pid,signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid,signal.SIGKILL)
                    process.wait()
                exit_code = 124
            log.flush();os.fsync(log.fileno())
        ended = time.time_ns()
        observed = {}
        with log_path.open('rb') as log:
            for line in log:
                if len(line)>1048576:
                    raise ValueError('native output event bound exceeded')
                try:
                    event=json.loads(line,object_pairs_hook=unique)
                except (ValueError,json.JSONDecodeError):
                    continue
                if event.get('Test') in names and event.get('Action') in ('pass','fail','skip'):
                    if event['Test'] in observed:
                        raise ValueError('native test terminal event duplicated')
                    observed[event['Test']]=event['Action']
        summaries[name]={'exit_code':exit_code,'started_utc_ns':started,'ended_utc_ns':ended,
                         'log_path':str(log_path),'log_sha256':sha_file(log_path),'command':command,'tests':observed,
                         'native_tests_passed':exit_code==0 and set(observed)==set(names) and all(v=='pass' for v in observed.values())}
    source_binding(binding_path,binding_sha)
    if sha_file(program) != go_sha:
        raise ValueError('native toolchain changed during component execution')
    result={'version':1,'scope':'isolated_component','candidate_sha256':binding['candidate_sha256'],
            'interface_sha256':binding['interface_sha256'],'source_binding_sha256':binding_sha,
            'native_go_binary_sha256':go_sha,'native_commands':summaries,'scenarios':[{'scenario_id':number,
                'component_test':test,'component_test_passed':summaries[package]['tests'].get(test)=='pass',
                'release_runtime_status':'blocked','release_runtime_input':RUNTIME_BLOCKERS[number]}
                for number,(package,test) in SCENARIOS.items()],
            'component_checks_passed':all(summary['native_tests_passed'] for summary in summaries.values()),
            'capacity_qualified':False,'release_qualified':False,'final_gate':'9.1'}
    write_json(output/'component-result.json',result)
    return 0 if result['component_checks_passed'] else 1


def main(argv=None):
    parser=argparse.ArgumentParser()
    sub=parser.add_subparsers(dest='mode',required=True)
    analyze=sub.add_parser('analyze')
    analyze.add_argument('--config',required=True);analyze.add_argument('--config-sha256',required=True);analyze.add_argument('--output',required=True)
    component=sub.add_parser('components')
    component.add_argument('--binding',required=True);component.add_argument('--binding-sha256',required=True)
    component.add_argument('--go-binary',required=True);component.add_argument('--go-sha256',required=True);component.add_argument('--output',required=True)
    args=parser.parse_args(argv)
    if args.mode=='components':
        try:
            return components(args.binding,args.binding_sha256,args.go_binary,args.go_sha256,args.output)
        except (ValueError,OSError,KeyError,TypeError,ImportError,subprocess.SubprocessError):
            print('security component capture failed: real inputs or native execution unavailable',file=sys.stderr)
            return 1
    output=None
    try:
        output=owned_output(args.output)
        configuration=config(args.config,args.config_sha256)
        measurements=capacity(configuration)
        scenarios=runtime_scenarios(configuration)
        if sha_file(ordinary(args.config))!=args.config_sha256:
            raise ValueError('configuration changed during release measurement')
        write_json(output/'security-release-result.json',{'version':1,'scope':'lane_measurements',
                   'configuration_sha256':args.config_sha256,'bindings':configuration['bindings'].__dict__,
                   'measurements':measurements,'scenarios':scenarios,'observed_assertions_passed':True,
                   'capacity_qualified':False,'release_qualified':False,'final_gate':'9.1'})
        return 0
    except (ValueError,OSError,KeyError,TypeError,ImportError):
        if output is not None:
            write_json(output/'security-release-result.json',{'version':1,'scope':'lane_measurements',
                       'observed_assertions_passed':False,'capacity_qualified':False,'release_qualified':False,
                       'blocked_inputs':['Exact current authority, complete native capture, raw scenario evidence or frozen budget assertions unavailable.'],
                       'scenarios':[{'scenario_id':number,'status':'blocked','required_real_input':RUNTIME_BLOCKERS[number]} for number in SCENARIOS],
                       'final_gate':'9.1'})
        print('security release capture failed: required real inputs or frozen assertions unavailable',file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
