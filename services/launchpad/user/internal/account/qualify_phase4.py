#!/usr/bin/env python3
from __future__ import annotations

import argparse
import asyncio
import base64
import dataclasses
import hashlib
import json
import math
import os
from pathlib import Path
import re
import secrets
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[4]
LOAD = ROOT / 'tools/production/load'
sys.path.insert(0, str(LOAD))
from calibration import cpu_fraction, linux_cpu_snapshot, nic_fraction
from capture import gateway_summary, iter_artifact, read_manifest, sha_file
from phase2_accounts import WalletSigner, login
from phase2_feeder import owned_command
from phase2_http import HTTP, HTTPFailure
from phase2_receivers import connect_receiver
from phase2_types import Bindings, bound_document, canonical, digest, unique
from scheduler import percentile
from workload import MIX, VERSION, provisioned_budget

REQUIREMENTS = tuple('D11_' + code for code in ('019', '020', '021', '022', '023', '024', '025', '026', '027', '030'))
OPERATIONS = ('profile_read', 'preferences_read', 'profile_write', 'preferences_write')
PG_KEYS = {'PGHOST', 'PGPORT', 'PGUSER', 'PGDATABASE', 'PGPASSWORD', 'PGSSLMODE', 'PGSSLROOTCERT', 'PGSERVICE', 'PGSERVICEFILE', 'PGPASSFILE', 'PATH'}
PUBLIC_FIELDS = {'wallet', 'displayName', 'bio', 'avatarAssetId', 'bannerAssetId', 'website', 'revision', 'socials', 'creatorPools'}


def exact_integer(value, lower, upper, label):
    if type(value) is not int or not lower <= value <= upper:
        raise ValueError(label + ' requires an explicit bounded integer')
    return value


def checked_source(config):
    bindings = Bindings(**config['bindings'])
    for name in ('candidate', 'interface', 'topology'):
        document = config[name + '_authority']
        if document['sha256'] != getattr(bindings, name):
            raise ValueError(name + ' binding differs from admitted authority')
        bound_document(document['path'], document['sha256'])
    files = config['source_files']
    minimum = {'media/user/internal/account/qualify_phase4.py', 'media/user/internal/account/store.go', 'media/user/internal/account/preferences_http.go', 'media/user/internal/account/http.go', 'media/user/internal/account/devices_http.go', 'shared/auth/operation.go', 'shared/auth/ledger.go', 'tools/production/load/phase2_accounts.py', 'tools/production/load/phase2_http.py', 'tools/production/load/phase2_receivers.py', 'tools/production/load/capture.py', 'tools/production/load/calibration.py', 'tools/production/load/phase2_types.py', 'tools/production/load/phase2_feeder.py', 'tools/production/load/scheduler.py', 'tools/production/load/workload.py'}
    if not isinstance(files, dict) or not minimum <= files.keys():
        raise ValueError('complete current lane/authentication/generator source bindings required')
    for relative, expected in files.items():
        path = Path(relative)
        if path.is_absolute() or '..' in path.parts or not (ROOT / path).resolve().is_relative_to(ROOT.resolve()) or sha_file(ROOT / path) != expected:
            raise ValueError('current source digest differs')
    if digest(canonical(dict(sorted(files.items())))) != bindings.generator:
        raise ValueError('generator source manifest differs')
    return bindings


class Artifacts:
    def __init__(self, directory):
        allowed = (ROOT / 'evidence/production-system/L35/phase4').resolve()
        directory = Path(directory)
        if not directory.is_absolute() or directory.is_symlink() or not directory.resolve().is_relative_to(allowed):
            raise ValueError('exact lane-owned immutable run directory required')
        for parent in directory.parents:
            if parent.is_symlink():
                raise ValueError('artifact parent symlink denied')
        directory.mkdir(parents=True, exist_ok=False)
        self.directory = directory
        self.streams = {}

    def rows(self, name):
        if not re.fullmatch(r'[a-z0-9-]+\.ndjson', name) or name in self.streams:
            raise ValueError('unique exact raw capture name required')
        stream = os.fdopen(os.open(self.directory / name, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w')
        self.streams[name] = stream
        return stream

    def document(self, name, value):
        if not re.fullmatch(r'[a-z0-9-]+\.json', name):
            raise ValueError('exact artifact name required')
        with os.fdopen(os.open(self.directory / name, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as stream:
            stream.write(canonical(value).decode() + '\n')
            stream.flush()
            os.fsync(stream.fileno())

    def close(self):
        for stream in self.streams.values():
            if not stream.closed:
                stream.flush()
                os.fsync(stream.fileno())
                stream.close()

    def hashes(self):
        self.close()
        return {path.name: sha_file(path) for path in sorted(self.directory.iterdir()) if path.is_file()}


class Database:
    def __init__(self, config):
        self.psql = Path(config['executable'])
        if not self.psql.is_absolute() or sha_file(self.psql) != config['sha256']:
            raise ValueError('bound actual psql executable required')
        self.environment = {}
        mapping = config['environment']
        if not mapping or not set(mapping) <= PG_KEYS:
            raise ValueError('explicit private database connection environment required')
        for key, variable in mapping.items():
            if not isinstance(variable, str) or not re.fullmatch(r'[A-Z][A-Z0-9_]{1,127}', variable) or not os.environ.get(variable):
                raise ValueError('required private database input missing')
            self.environment[key] = os.environ[variable]
        self.schema = config['schema']
        if not re.fullmatch(r'[a-z][a-z0-9_]{0,62}', self.schema):
            raise ValueError('exact owning user schema required')

    async def query(self, sql):
        raw = await owned_command([str(self.psql), '--no-psqlrc', '--quiet', '--no-align', '--tuples-only', '--set=ON_ERROR_STOP=1', '--command', "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='3s'; " + sql + '; COMMIT;'], environment=self.environment, timeout=5)
        return [json.loads(line, object_pairs_hook=unique) for line in raw.decode().splitlines() if line.startswith('{')]

    async def ready(self):
        rows = await self.query("SELECT json_build_object('server_version_num',current_setting('server_version_num'),'track_commit_timestamp',current_setting('track_commit_timestamp'),'recovery',pg_is_in_recovery(),'database',current_database())")
        if len(rows) != 1 or rows[0]['track_commit_timestamp'] != 'on' or rows[0]['recovery']:
            raise ValueError('real primary PostgreSQL with commit timestamp authority required')
        return rows[0]

    async def snapshot(self, wallets):
        if not wallets or any(not re.fullmatch(r'0x[0-9a-f]{40}', wallet) for wallet in wallets) or len(set(wallets)) != len(wallets):
            raise ValueError('actual distinct authenticated wallets required')
        scope = ','.join("'" + wallet + "'" for wallet in wallets)
        schema = '"' + self.schema + '".'
        select = "SELECT account_id FROM " + schema + 'account_state WHERE wallet IN (' + scope + ')'
        statements = []
        for table, fields, order in (
            ('account_state', 'account_id,revision,deleted,profile,preferences', 'account_id'),
            ('account_watch', 'account_id,chain_id,pool,added_at,revision,alert_enabled', 'account_id,chain_id,pool'),
            ('account_consent', 'account_id,category,channel,enabled,revision,revoke_revision', 'account_id,category,channel'),
            ('account_endpoint', 'account_id,device_id,platform,provider,consent_revision,validated_at,revoked', 'account_id,device_id'),
            ('account_job', 'job_id,account_id,kind,state,requested_at,expires_at', 'account_id,job_id'),
            ('account_outbox', 'event_id,account_id,revision,kind,payload', 'account_id,revision,event_id'),
            ('account_revision_inbox', 'event_id,digest,account_id,revision', 'account_id,revision,event_id'),
        ):
            statements.append("SELECT json_build_object('table','" + table + "','row',row_to_json(x)) FROM (SELECT " + fields + ' FROM ' + schema + table + ' WHERE account_id IN (' + select + ') ORDER BY ' + order + ') x')
        statements.append("SELECT json_build_object('table','operation_ledger','row',row_to_json(x)) FROM (SELECT request_id::text AS request_id,body_digest AS body_sha256,response_digest AS response_sha256 FROM " + schema + 'operation_ledger WHERE actor IN (' + scope + ') ORDER BY request_id) x')
        rows = await self.query(';'.join(statements))
        grouped = {table: [] for table in ('account_state', 'account_watch', 'account_consent', 'account_endpoint', 'account_job', 'account_outbox', 'account_revision_inbox', 'operation_ledger')}
        for row in rows:
            if row['table'] not in grouped:
                raise ValueError('foreign observation table')
            grouped[row['table']].append(row['row'])
        if len(grouped['account_state']) != len(wallets):
            raise ValueError('authenticated user state missing')
        return grouped

    async def queue(self):
        schema = '"' + self.schema + '".'
        rows = await self.query("SELECT json_build_object('observed_utc_ns',(extract(epoch FROM clock_timestamp())*1000000000)::numeric(30,0)::text,'pending_exports',(SELECT count(*) FROM " + schema + "account_job WHERE kind='export' AND state NOT IN('completed','expired','ready')),'pending_deletions',(SELECT count(*) FROM " + schema + "account_job WHERE kind='deletion' AND state NOT IN('completed','expired')),'delivery_pending',(SELECT count(*) FROM " + schema + "delivery_outbox WHERE state='pending'),'delivery_oldest_seconds',coalesce((SELECT extract(epoch FROM clock_timestamp()-min(created_at)) FROM " + schema + "delivery_outbox WHERE state='pending'),0))")
        if len(rows) != 1:
            raise ValueError('actual service queue observation missing')
        return rows[0]

    async def revocation_commit(self, session_id):
        session = str(uuid.UUID(session_id))
        schema = '"' + self.schema + '".'
        rows = await self.query("SELECT json_build_object('revoked',revoked_at IS NOT NULL,'commit_utc_ns',(extract(epoch FROM pg_xact_commit_timestamp(xmin))*1000000000)::numeric(30,0)::text) FROM " + schema + "identity_sessions WHERE session_id='" + session + "'::uuid")
        if len(rows) != 1 or rows[0]['revoked'] is not True or rows[0]['commit_utc_ns'] is None:
            raise ValueError('authoritative revocation commit absent')
        return int(rows[0]['commit_utc_ns'])


    async def replay_state(self, scope):
        for name in ('consumer_id', 'generation'):
            if not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}', scope[name]):
                raise ValueError('explicit real replay consumer/generation required')
        if not re.fullmatch(r'[0-9a-f]{64}', scope['manifest_digest']):
            raise ValueError('actual canonical archive manifest digest required')
        schema = '"' + self.schema + '".'
        sql = "SELECT json_build_object('consumer_id',r.consumer_id,'generation',r.generation,'manifest_digest',r.manifest_digest,'status',r.status,'raw_count',r.raw_count,'manifest_raw_count',m.raw_count,'projection_digest',r.projection_digest,'active_generation',a.generation) FROM " + schema + 'delivery_rebuilds r JOIN ' + schema + 'delivery_archive_manifests m USING(manifest_digest) LEFT JOIN ' + schema + "delivery_read_aliases a USING(consumer_id) WHERE r.consumer_id='" + scope['consumer_id'] + "' AND r.generation='" + scope['generation'] + "' AND r.manifest_digest='" + scope['manifest_digest'] + "'"
        rows = await self.query(sql)
        if len(rows) != 1:
            raise ValueError('actual authorized replay job absent')
        return rows[0]


def snapshot_record(snapshot, phase, stamp):
    return {'phase': phase, 'observed_utc_ns': stamp, 'tables': {table: {'count': len(rows), 'sha256': digest(canonical(rows))} for table, rows in snapshot.items()}, 'accounts': [{'account_id': row['account_id'], 'revision': row['revision'], 'deleted': row['deleted'], 'profile_sha256': digest(canonical(row['profile'])), 'preferences_sha256': digest(canonical(row['preferences']))} for row in snapshot['account_state']]}


def require_public(value):
    profile = value['data']['profile']
    if not isinstance(profile, dict) or not {'wallet', 'revision', 'displayName', 'bio'} <= profile.keys() or not profile.keys() <= PUBLIC_FIELDS:
        raise ValueError('public profile schema minimization failed')
    return profile


async def signed_headers(signer, account, environment, action, method, path, body):
    now = int(time.time())
    envelope = {'version': 1, 'chainId': '125', 'environment': environment, 'actor': account.wallet, 'action': action, 'method': method, 'path': path, 'bodyHash': digest(canonical(body)), 'nonce': secrets.token_hex(32), 'issuedAt': now, 'expiresAt': now + 120}
    statement = 'KindleLaunch signed operation v1\n' + canonical(envelope).decode()
    signature = (await signer.call('sign', wallet=account.wallet, message=statement))['signature']
    return {'X-Request-ID': str(uuid.uuid4()), 'X-Kindle-Operation': base64.urlsafe_b64encode(canonical(envelope)).rstrip(b'=').decode(), 'X-Kindle-Operation-Signature': signature}


async def call(http, method, path, *, account=None, body=None, headers=None):
    try:
        value = await http.call(method, path, body, bearer=None if account is None else account.access_token, headers=headers)
        return 200, value
    except HTTPFailure as error:
        return error.status, None


def summarize(samples, *, timeout_ns):
    result = {}
    for operation in OPERATIONS:
        rows = [row for row in samples if row['operation'] == operation and not row['warmup']]
        if not rows:
            raise ValueError('every declared workload operation needs measured samples')
        valid = [row for row in rows if row['outcome'] == 'success']
        latencies = [row['completed_monotonic_ns'] - row['scheduled_monotonic_ns'] for row in rows if row['outcome'] != 'missed']
        corrected = sorted(latencies + [max(timeout_ns, row['completed_monotonic_ns'] - row['scheduled_monotonic_ns']) for row in rows if row['outcome'] == 'missed'])
        denominator = len(rows)
        result[operation] = {'scheduled': denominator, 'attempted': sum(row['started_monotonic_ns'] is not None for row in rows), 'successful': len(valid), 'missed': sum(row['outcome'] == 'missed' for row in rows), 'intentional_admission': sum(row['status'] == 429 for row in rows), 'unplanned_failures': sum(row['outcome'] != 'success' and row['status'] != 429 for row in rows), 'p95_ns': percentile(corrected, .95), 'p99_ns': percentile(corrected, .99), 'p99_censored': (denominator - len(valid)) > math.floor(denominator * .01), 'censored_count': sum(row['outcome'] == 'missed' for row in rows), 'scheduler_p99_ns': percentile([row['started_monotonic_ns'] - row['scheduled_monotonic_ns'] for row in rows if row['started_monotonic_ns'] is not None], .99), 'latency_denominator': denominator, 'measurement_boundary': 'scheduled_generator_arrival_to_validated_http_response', 'coordinated_omission_corrected': True, 'missed_latency_policy': 'right_censored; upper diagnostic >=request deadline; missing work cannot pass p99 acceptance'}
    return result


async def workload(http, signer, accounts, config, output, queue_output, database):
    rates = config['rates']
    if set(rates) != set(OPERATIONS) or any(type(rate) is not int or rate <= 0 for rate in rates.values()):
        raise ValueError('four positive explicit operation rates required')
    read_rate = rates['profile_read'] + rates['preferences_read']
    write_rate = rates['profile_write'] + rates['preferences_write']
    if read_rate > MIX['identity_profile'] or write_rate > MIX['writes'] or sum(rates.values()) > 10000:
        raise ValueError('lane subset exceeds frozen shared request accounting')
    duration = exact_integer(config['duration_seconds'], 60, 600, 'isolated duration')
    warmup = exact_integer(config['warmup_seconds'], 1, 120, 'warmup')
    max_inflight = exact_integer(config['max_inflight'], 1, 4096, 'inflight')
    timeout_ns = exact_integer(config['request_timeout_ms'], 1, 10000, 'request timeout') * 1000000
    rate = sum(rates.values())
    count = (duration + warmup) * rate
    if count > 1000000:
        raise ValueError('bounded raw sample budget exceeded')
    cycle = [operation for operation in OPERATIONS for _ in range(rates[operation])]
    state = []
    for account in accounts:
        status, value = await call(http, 'GET', '/v3/me/preferences', account=account)
        if status != 200:
            raise ValueError('real authenticated preference provisioning failed')
        state.append({'revision': value['data']['preferences']['revision'], 'profile': {}, 'preferences': {}, 'lock': asyncio.Lock()})
    samples, acknowledgements, pending = [], [], set()
    origin = time.monotonic_ns() + 10000000
    start_utc = time.time_ns()
    maximum_pending = 0
    queue_stop = asyncio.Event()
    queue_samples = []
    async def sample_queue():
        while not queue_stop.is_set():
            measured = await database.queue()
            queue_samples.append(measured)
            queue_output.write(canonical(measured).decode() + '\n')
            try:
                await asyncio.wait_for(queue_stop.wait(), 1)
            except TimeoutError:
                pass
    sampler = asyncio.create_task(sample_queue())
    async def request(row, account_index):
        account, current = accounts[account_index], state[account_index]
        row['started_monotonic_ns'] = time.monotonic_ns()
        row['request_id'] = str(uuid.uuid4())
        try:
            async def execute():
                operation = row['operation']
                if operation == 'profile_read':
                    status, value = await call(http, 'GET', '/v3/profiles/' + account.wallet, headers={'X-Request-ID': row['request_id']})
                    row['status'] = status
                    if status == 200 and value.get('requestId') != row['request_id']:
                        raise ValueError('public response trace identity differs')
                    if status == 200:
                        require_public(value)
                elif operation == 'preferences_read':
                    status, value = await call(http, 'GET', '/v3/me/preferences', account=account, headers={'X-Request-ID': row['request_id']})
                    row['status'] = status
                    if status == 200 and value.get('requestId') != row['request_id']:
                        raise ValueError('preference response trace identity differs')
                    if status == 200 and value['data']['preferences']['schemaVersion'] < 1:
                        raise ValueError('typed preference schema absent')
                else:
                    async with current['lock']:
                        if operation == 'profile_write':
                            path, action = '/v3/profiles/' + account.wallet, 'profile.update'
                            patch = {'expectedRevision': current['revision'], 'bio': 'load-' + row['request_id']}
                        else:
                            path, action = '/v3/me/preferences', 'preferences.update'
                            patch = {'expectedRevision': current['revision'], 'locale': 'fr' if row['id'] % 2 else 'en', 'reducedMotion': True}
                        headers = await signed_headers(signer, account, config['operation_environment'], action, 'PATCH', path, patch)
                        row['request_id'] = headers['X-Request-ID']
                        row['body_sha256'] = digest(canonical(patch))
                        status, value = await call(http, 'PATCH', path, account=account, body=patch, headers=headers)
                        row['status'] = status
                        if status == 200 and value.get('requestId') != row['request_id']:
                            raise ValueError('signed mutation response trace identity differs')
                        if status == 200:
                            response = value['data']['profile' if operation == 'profile_write' else 'preferences']
                            revision = response['revision']
                            if int(revision) != int(current['revision']) + 1:
                                raise ValueError('acknowledged revision does not advance once')
                            current['revision'] = revision
                            target = 'profile' if operation == 'profile_write' else 'preferences'
                            current[target].update({key: val for key, val in patch.items() if key != 'expectedRevision'})
                            acknowledgements.append({'account_index': account_index, 'revision': int(revision), 'request_id': row['request_id'], 'operation': operation, 'body_sha256': row['body_sha256'], 'acknowledged_utc_ns': time.time_ns()})
                row['status'] = status
                row['outcome'] = 'success' if status == 200 else ('admission' if status == 429 else 'error')
            await asyncio.wait_for(execute(), timeout_ns / 1e9)
        except TimeoutError:
            row['outcome'] = 'timeout'
        except Exception as error:
            row['outcome'] = 'error'
            row['error_category'] = type(error).__name__
        finally:
            row['completed_monotonic_ns'] = time.monotonic_ns()
            row['completed_utc_ns'] = time.time_ns()
            output.write(canonical(row).decode() + '\n')
    try:
        for index in range(count):
            scheduled = origin + index * 1000000000 // rate
            await asyncio.sleep(max(0, (scheduled - time.monotonic_ns()) / 1e9))
            row = {'arrival_id': str(uuid.uuid4()), 'lane': 'L35', 'run_id': config['run_id'], 'host_id': config['host_id'], 'id': index, 'operation': cycle[index % len(cycle)], 'scheduled_monotonic_ns': scheduled, 'started_monotonic_ns': None, 'completed_monotonic_ns': None, 'completed_utc_ns': None, 'status': None, 'outcome': 'missed', 'warmup': index < warmup * rate, 'inflight_at_arrival': len(pending), 'request_id': None}
            samples.append(row)
            if len(pending) >= max_inflight:
                row['completed_monotonic_ns'], row['completed_utc_ns'] = time.monotonic_ns(), time.time_ns()
                output.write(canonical(row).decode() + '\n')
                continue
            task = asyncio.create_task(request(row, index % len(accounts)))
            pending.add(task)
            task.add_done_callback(pending.discard)
            maximum_pending = max(maximum_pending, len(pending))
        if pending:
            await asyncio.gather(*tuple(pending))
    finally:
        queue_stop.set()
        try:
            await sampler
        finally:
            for task in pending:
                task.cancel()
            if pending:
                await asyncio.gather(*tuple(pending), return_exceptions=True)
    return {'samples': samples, 'acknowledgements': acknowledgements, 'state': state, 'summary': summarize(samples, timeout_ns=timeout_ns), 'queue_samples': queue_samples, 'max_inflight_observed': maximum_pending, 'started_utc_ns': start_utc, 'ended_utc_ns': time.time_ns(), 'duration_ns': time.monotonic_ns() - origin}


def reconcile(snapshot, accounts, measurements):
    by_id = {row['account_id']: row for row in snapshot['account_state']}
    by_wallet = {}
    for index, account in enumerate(accounts):
        matches = [row for row in by_id.values() if row['profile'].get('wallet') == account.wallet]
        if len(matches) != 1:
            raise ValueError('actual owner database mapping missing or ambiguous')
        by_wallet[index] = matches[0]
    events = {(row['account_id'], row['revision'], row['kind']) for row in snapshot['account_outbox']}
    if len({row['event_id'] for row in snapshot['account_outbox']}) != len(snapshot['account_outbox']):
        raise ValueError('duplicate durable event identity')
    ledger = {row['request_id']: row for row in snapshot['operation_ledger']}
    acknowledged = []
    for ack in measurements['acknowledgements']:
        row = by_wallet[ack['account_index']]
        if ack['request_id'] not in ledger or ledger[ack['request_id']]['body_sha256'] != ack['body_sha256']:
            raise ValueError('acknowledged signed request absent from real operation ledger')
        kind = 'profile.updated' if ack['operation'] == 'profile_write' else 'preferences.updated'
        if row['revision'] < ack['revision'] or (row['account_id'], ack['revision'], kind) not in events:
            raise ValueError('acknowledged business effect or outbox intent absent')
        acknowledged.append({**{key: val for key, val in ack.items() if key != 'account_index'}, 'account_id': row['account_id'], 'event_kind': kind})
    for index, current in enumerate(measurements['state']):
        row = by_wallet[index]
        if row['deleted'] or row['revision'] != int(current['revision']):
            raise ValueError('current business revision differs from acknowledged state')
        for target in ('profile', 'preferences'):
            if any(row[target].get(key) != val for key, val in current[target].items()):
                raise ValueError('acknowledged business field lost')
    return acknowledged


async def revocation(http, signer, database, account, config, clock, output):
    topics = ['private.notifications.' + account.wallet]
    socket, state = await connect_receiver(http, config['stream_origin'], topics, account, ca_file=config['ca_file'], owned_local=config['owned_local'])
    try:
        ready_deadline = time.monotonic() + 5
        while True:
            value = await asyncio.wait_for(socket.receive(), max(.001, ready_deadline - time.monotonic()))
            if value.get('code') == 'REPLAY_COMPLETE':
                break
            state.receive(value)
            if time.monotonic() >= ready_deadline:
                raise TimeoutError('private socket did not become live')
        started_utc = time.time_ns()
        status, _ = await call(http, 'POST', '/v2/auth/sessions:logout-all', account=account, body={})
        if status != 204:
            raise ValueError('real identity revocation request was not acknowledged')
        ack_utc = time.time_ns()
        commit = await database.revocation_commit(account.session_id)
        profile_status, profile = await call(http, 'GET', '/v3/profiles/' + account.wallet)
        if profile_status != 200:
            raise ValueError('real public profile unavailable during revocation')
        body = {'expectedRevision': require_public(profile)['revision'], 'bio': 'revoked-write-' + str(uuid.uuid4())}
        headers = await signed_headers(signer, account, config['operation_environment'], 'profile.update', 'PATCH', '/v3/profiles/' + account.wallet, body)
        status, _ = await call(http, 'PATCH', '/v3/profiles/' + account.wallet, account=account, body=body, headers=headers)
        http_enforced = time.time_ns()
        if status != 401:
            raise ValueError('revoked signed write did not fail at authoritative authentication')
        deadline = time.monotonic() + 5
        while True:
            value = await asyncio.wait_for(socket.receive(), max(.001, deadline - time.monotonic()))
            if value.get('code') in ('AUTH_REQUIRED', 'AUTH_EXPIRED'):
                socket_enforced = time.time_ns()
                break
            if 'family' in value:
                raise ValueError('private business frame followed acknowledged identity revocation')
            if time.monotonic() >= deadline:
                raise TimeoutError('revoked private socket remained live')
        offset, uncertainty = clock['clock_offset_ns'], clock['clock_uncertainty_ns']
        bounds = {'http': http_enforced - offset - commit + uncertainty, 'socket': socket_enforced - offset - commit + uncertainty}
        if any(value < -uncertainty or value > 5000000000 for value in bounds.values()):
            raise ValueError('revocation propagation exceeds five-second bound')
        row = {'kind': 'l35_revocation', 'request_id': headers['X-Request-ID'], 'request_started_utc_ns': started_utc, 'acknowledged_utc_ns': ack_utc, 'commit_utc_ns': commit, 'http_enforced_utc_ns': http_enforced, 'socket_enforced_utc_ns': socket_enforced, 'http_status': status, 'clock_offset_ns': offset, 'clock_uncertainty_ns': uncertainty, 'latency_ns_upper_bound': bounds}
        output.write(canonical(row).decode() + '\n')
        return row
    finally:
        await socket.close()


def read_rows(path):
    with Path(path).open('rb') as source:
        while True:
            raw = source.readline(1048577)
            if not raw:
                return
            if len(raw) > 1048576 or not raw.endswith(b'\n'):
                raise ValueError('complete bounded native observation row required')
            yield json.loads(raw, object_pairs_hook=unique)


def requirement_assertions(source, bindings):
    document = bound_document(source['path'], source['sha256'])
    expected = {requirement + '.' + str(index) for requirement in REQUIREMENTS for index in range(1, 6)}
    if document['bindings'] != dataclasses.asdict(bindings) or set(document['assertions']) != expected:
        raise ValueError('all fifty exact-candidate acceptance assertion artifacts required')
    failures, checksums = [], {}
    for assertion_id, reference in document['assertions'].items():
        if sha_file(reference['raw_path']) != reference['raw_sha256']:
            raise ValueError('acceptance raw artifact digest differs')
        checksums[assertion_id] = reference['raw_sha256']
        rows = [row for row in read_rows(reference['raw_path']) if row.get('assertion_id') == assertion_id]
        if len(rows) != 1 or rows[0].get('run_id') != document['run_id'] or rows[0].get('executed') is not True or rows[0].get('exit_code') != 0 or not rows[0].get('request_ids') or not rows[0].get('observed'):
            failures.append(assertion_id + ': missing/failed actual acceptance observation')
        else:
            for request_id in rows[0]['request_ids']:
                uuid.UUID(request_id)
    return {'assertion_count': len(expected), 'raw_sha256': checksums, 'failures': failures, 'capacity_qualified': False}


def shared_http_measurements(manifest, timeout_ns):
    samples = []
    identities = set()
    for row in iter_artifact(manifest, 'http_arrivals'):
        if row.get('lane') != 'L35':
            continue
        if row['run_id'] != manifest['run_id'] or row['host_id'] not in manifest['generator_hosts'] or row['operation'] not in OPERATIONS:
            raise ValueError('foreign lane HTTP measurement')
        key = (row['host_id'], str(uuid.UUID(row['arrival_id'])))
        if key in identities:
            raise ValueError('duplicate planned HTTP arrival')
        identities.add(key)
        exact_integer(row['id'], 0, 2**63-1, 'arrival identity')
        for name in ('scheduled_monotonic_ns', 'completed_monotonic_ns', 'completed_utc_ns'):
            exact_integer(row[name], 1, 2**63-1, 'actual request timestamp')
        if row['completed_monotonic_ns'] < row['scheduled_monotonic_ns'] or row['outcome'] not in ('success', 'admission', 'error', 'missed', 'timeout') or type(row['warmup']) is not bool:
            raise ValueError('invalid actual planned/completed request')
        if row['outcome'] == 'success' and (row['status'] != 200 or row['started_monotonic_ns'] is None or row['request_id'] is None):
            raise ValueError('successful valid request lacks actual HTTP acceptance')
        if row['started_monotonic_ns'] is not None and not row['scheduled_monotonic_ns'] <= row['started_monotonic_ns'] <= row['completed_monotonic_ns']:
            raise ValueError('HTTP monotonic clocks/regression differ')
        if not int(manifest['started_utc_ns']) <= row['completed_utc_ns'] <= int(manifest['ended_utc_ns']):
            raise ValueError('HTTP sample outside capture run')
        samples.append(row)
    if not samples:
        raise ValueError('actual signed account/profile lane HTTP samples absent')
    measured = summarize(samples, timeout_ns=timeout_ns)
    failures = []
    for operation, result in measured.items():
        if result['p99_censored'] or result['missed'] or result['unplanned_failures'] * 1000 > result['scheduled']:
            failures.append(operation + ': actual valid load missing or failed')
        if operation.endswith('_read') and (result['p95_ns'] > 150000000 or result['p99_ns'] > 400000000):
            failures.append(operation + ': actual root read latency bound failed')
    return {'sample_count': len(samples), 'measurements': measured, 'failures': failures, 'capacity_qualified': False}


def shared_recovery(manifest, topology):
    if manifest['scope'] != 'final_shared' or 'L35' not in manifest['required_lane_ids']:
        raise ValueError('actual final task9.1 capture and lane membership required')
    faults = list(iter_artifact(manifest, 'fault_control', record='FaultInterval'))
    zones = topology['availability_zones']
    if not isinstance(zones, dict) or len(zones) < 2 or any(not value or len(set(value)) != len(value) for value in zones.values()):
        raise ValueError('actual multi-AZ replica inventory required')
    accepted_faults = []
    for fault in faults:
        if fault['run_id'] != manifest['run_id']:
            raise ValueError('foreign fault capture')
        if fault['action'] != 'replica_loss':
            continue
        if fault['native_control_sha256'] not in {artifact['sha256'] for artifact in manifest['artifacts']} :
            raise ValueError('AZ-loss native provider control bytes are not bound by capture manifest')
        affected = set(fault['affected_replica_ids'])
        if not any(affected == set(replicas) for replicas in zones.values()):
            continue
        if fault['outcome'] != 'recovered' or not int(fault['observed_start_utc_ns']) < int(fault['observed_end_utc_ns']) <= int(fault['recovery_utc_ns']):
            raise ValueError('AZ recovery did not actually complete')
        accepted_faults.append(fault)
    if not accepted_faults:
        raise ValueError('genuine AZ-loss/recovery observation absent')
    providers = list(iter_artifact(manifest, 'native_provider'))
    snapshots = [row for row in providers if row.get('kind') == 'l35_account_snapshot']
    acknowledgements = [row for row in providers if row.get('kind') == 'l35_acknowledged_mutation']
    if not snapshots or not acknowledgements:
        raise ValueError('actual L35 snapshots and acknowledged mutations required')
    failures = []
    for fault in accepted_faults:
        before = [row for row in snapshots if row['run_id'] == manifest['run_id'] and row['phase'] == 'before_fault' and row['fault_id'] == fault['fault_id'] and int(row['observed_utc_ns']) <= int(fault['observed_start_utc_ns'])]
        after = [row for row in snapshots if row['run_id'] == manifest['run_id'] and row['phase'] == 'after_recovery' and row['fault_id'] == fault['fault_id'] and int(row['observed_utc_ns']) >= int(fault['recovery_utc_ns'])]
        if len(before) != 1 or len(after) != 1:
            raise ValueError('exact independent before/after fault account snapshots required')
        for table in ('account_state', 'account_watch', 'account_consent', 'account_endpoint', 'account_job', 'account_outbox', 'account_revision_inbox', 'operation_ledger'):
            left, right = before[0]['tables'][table], after[0]['tables'][table]
            if type(left['count']) is not int or left['count'] < 0 or type(right['count']) is not int or right['count'] < 0 or not re.fullmatch(r'[0-9a-f]{64}', left['sha256']) or not re.fullmatch(r'[0-9a-f]{64}', right['sha256']):
                raise ValueError('authoritative exact record count and SHA256 missing')
            if left != right:
                failures.append('AZ record counts/checksums differ: ' + table)
        committed = {(row['account_id'], row['revision'], row['event_kind'], row['request_id']) for row in after[0]['acknowledgements']}
        expected = [row for row in acknowledgements if row['run_id'] == manifest['run_id'] and int(row['acknowledged_utc_ns']) <= int(fault['observed_start_utc_ns'])]
        if not expected:
            raise ValueError('AZ loss drill has no independently acknowledged business writes')
        for row in expected:
            if (row['account_id'], row['revision'], row['event_kind'], row['request_id']) not in committed:
                failures.append('acknowledged mutation missing after AZ recovery')
    replay_before = [row for row in snapshots if row['run_id'] == manifest['run_id'] and row['phase'] == 'before_replay']
    replay_after = [row for row in snapshots if row['run_id'] == manifest['run_id'] and row['phase'] == 'after_replay']
    if not replay_before or len(replay_before) != len(replay_after):
        raise ValueError('real replay job observations and before/after snapshots required')
    for before in replay_before:
        matches = [row for row in replay_after if row['fault_id'] == before['fault_id']]
        if len(matches) != 1:
            raise ValueError('replay observation identity missing or duplicated')
        after = matches[0]
        left, right = before['replay_state'], after['replay_state']
        if any(left[key] != right[key] for key in ('consumer_id', 'generation', 'manifest_digest')) or left['status'] != 'building' or right['status'] != 'active' or right['active_generation'] != right['generation'] or type(right['raw_count']) is not int or right['raw_count'] <= 0 or right['raw_count'] != right['manifest_raw_count'] or not re.fullmatch(r'[0-9a-f]{64}', right['projection_digest'] or '') or int(after['observed_utc_ns']) <= int(before['observed_utc_ns']):
            raise ValueError('replay must genuinely process and activate the exact archive generation')
        if before['tables'] != after['tables']:
            failures.append('event replay altered existing business rows/counts/checksums')
    return {'fault_count': len(accepted_faults), 'acknowledged_mutation_count': len(acknowledgements), 'snapshot_count': len(snapshots), 'failures': failures, 'capacity_qualified': False}


def check_thresholds(measurements, limits, resources):
    failures = []
    for operation, result in measurements['summary'].items():
        if result['p99_censored']:
            failures.append(operation + ': p99 acceptance censored by failed/missing valid work')
        if result['missed'] or result['scheduler_p99_ns'] is None or result['scheduler_p99_ns'] > 10000000:
            failures.append(operation + ': generator arrival saturation')
        if result['unplanned_failures'] * 1000 > result['scheduled']:
            failures.append(operation + ': supported valid operation error rate >0.1percent')
        if operation.endswith('_read') and (result['p95_ns'] > 150000000 or result['p99_ns'] > 400000000):
            failures.append(operation + ': read p95/p99 exceeds root SLO')
    if resources['cpu_fraction'] > .7 or resources['nic_fraction'] > .7:
        failures.append('generator CPU/NIC saturation')
    if not measurements['queue_samples']:
        failures.append('native service queue samples absent')
    for sample in measurements['queue_samples']:
        if sample['delivery_pending'] > limits['delivery_pending_max'] or sample['delivery_oldest_seconds'] > limits['delivery_oldest_seconds_max'] or sample['pending_exports'] > limits['pending_exports_max'] or sample['pending_deletions'] > limits['pending_deletions_max']:
            failures.append('native service queue budget exceeded')
    return sorted(set(failures))


def nic_snapshot(name):
    if not re.fullmatch(r'[A-Za-z0-9_.:-]{1,64}', name):
        raise ValueError('actual generator NIC identity required')
    root = Path('/sys/class/net') / name / 'statistics'
    return {direction: int((root / (direction + '_bytes')).read_text()) for direction in ('tx', 'rx')}


async def run(config, bindings, artifacts):
    if config['mode'] not in ('isolated_component', 'final_shared_evaluation', 'recovery_observation'):
        raise ValueError('exact lane component or shared capture evaluation mode required')
    topology = bound_document(config['topology_authority']['path'], bindings.topology)
    if config['mode'] == 'recovery_observation':
        database = Database(config['database'])
        await database.ready()
        if config['snapshot_phase'] not in ('before_fault', 'after_recovery', 'before_replay', 'after_replay') or not config['fault_id'] or not config['run_id']:
            raise ValueError('explicit actual recovery observation identity required')
        snapshot = await database.snapshot(config['wallets'])
        expected_source = config['acknowledgements']
        if sha_file(expected_source['path']) != expected_source['sha256']:
            raise ValueError('acknowledged mutation source digest differs')
        expected = list(read_rows(expected_source['path']))
        observed = {(row['account_id'], row['revision'], row['kind']) for row in snapshot['account_outbox']}
        ledger = {row['request_id']: row for row in snapshot['operation_ledger']}
        actual = []
        ids = set()
        for row in expected:
            if row['kind'] != 'l35_acknowledged_mutation' or row['run_id'] != config['run_id'] or row['request_id'] in ids:
                raise ValueError('acknowledged mutation provenance differs')
            ids.add(row['request_id'])
            if row['request_id'] not in ledger or ledger[row['request_id']]['body_sha256'] != row['body_sha256']:
                raise ValueError('recovered mutation request absent from real signed operation ledger')
            if (row['account_id'], row['revision'], row['event_kind']) not in observed:
                raise ValueError('acknowledged effect absent from actual recovered database')
            actual.append(row)
        if not actual:
            raise ValueError('recovery observation requires real acknowledged business mutations')
        record = snapshot_record(snapshot, config['snapshot_phase'], time.time_ns())
        if config['snapshot_phase'] in ('before_replay', 'after_replay'):
            record['replay_state'] = await database.replay_state(config['replay'])
        record.update({'kind': 'l35_account_snapshot', 'run_id': config['run_id'], 'fault_id': config['fault_id'], 'acknowledgements': actual, 'acknowledgements_sha256': expected_source['sha256']})
        artifacts.document('recovery-snapshot.json', record)
        return {'mode': config['mode'], 'run_id': config['run_id'], 'bindings': dataclasses.asdict(bindings), 'failures': [], 'capacity_qualified': False, 'monthly_availability_qualified': False, 'final_gate': '9.1', 'observation_only': True}
    if config['mode'] == 'final_shared_evaluation':
        source = config['shared_capture']
        manifest = read_manifest(source['path'], source['sha256'], bindings=bindings)
        recovery = shared_recovery(manifest, topology)
        gateway = gateway_summary(manifest)
        http_measurements = shared_http_measurements(manifest, exact_integer(config['request_timeout_ms'], 1, 10000, 'request timeout') * 1000000)
        requirement_evidence = requirement_assertions(config['requirement_evidence'], bindings)
        duration_ns = int(manifest['ended_utc_ns']) - int(manifest['started_utc_ns'])
        if duration_ns < 86400000000000:
            raise ValueError('final shared capture does not cover the declared 24hour soak')
        cost_artifacts = [value for value in manifest['artifacts'] if value['kind'] == 'cost_budget']
        if len(cost_artifacts) != 1:
            raise ValueError('actual shared provision and approved operating budget required')
        cost_source = cost_artifacts[0]
        costs = bound_document(Path(manifest['artifact_root']) / cost_source['path'], cost_source['sha256'])
        if costs['bindings'] != dataclasses.asdict(bindings):
            raise ValueError('shared provision/cost budget differs from final candidate')
        budget_result = provisioned_budget(costs)
        artifacts.document('shared-recovery.json', recovery)
        artifacts.document('shared-gateway.json', gateway)
        artifacts.document('shared-http.json', http_measurements)
        return {'failures': recovery['failures'] + requirement_evidence['failures'] + budget_result['failures'] + http_measurements['failures'], 'requirement_evidence': requirement_evidence, 'approved_budget_evaluation': budget_result, 'mode': config['mode'], 'shared_capture_sha256': source['sha256'], 'requirements': list(REQUIREMENTS), 'capacity_qualified': False, 'monthly_availability_qualified': False, 'final_gate': '9.1'}
    if not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}', config['operation_environment']) or type(config['owned_local']) is not bool or config['outside_application_cluster'] is not True:
        raise ValueError('actual operation environment and external generator identity required')
    count = exact_integer(config['accounts'], 2, 64, 'authenticated accounts')
    if not config['host_id'] or not config['region']:
        raise ValueError('actual generator host and origin region required')
    if config['host_id'] not in topology['generator_hosts'] or config['host_id'] in topology['application_hosts']:
        raise ValueError('generator must be an actual admitted host outside the application cluster')
    http = HTTP(config['gateway_origin'], config['application_origin'], ca_file=config['ca_file'], owned_local=config['owned_local'], timeout=config['request_timeout_ms'] / 1000)
    database = Database(config['database'])
    clock_source = config['clock_calibration']
    clock = bound_document(clock_source['path'], clock_source['sha256'])
    if clock['bindings'] != dataclasses.asdict(bindings) or clock['host_id'] != config['host_id'] or type(clock['clock_offset_ns']) is not int or type(clock['clock_uncertainty_ns']) is not int or not 0 <= clock['clock_uncertainty_ns'] <= 5000000 or not 0 <= time.time_ns() - clock['observed_utc_ns'] <= 300000000000:
        raise ValueError('current independently measured clock calibration required')
    budget_source = config['queue_budget']
    budget = bound_document(budget_source['path'], budget_source['sha256'])
    if budget['bindings'] != dataclasses.asdict(bindings) or set(budget['limits']) != {'delivery_pending_max', 'delivery_oldest_seconds_max', 'pending_exports_max', 'pending_deletions_max'} or any(type(value) is not int or value < 0 for value in budget['limits'].values()):
        raise ValueError('actual approved queue budget authority required')
    signer = WalletSigner(config['signer']['node'], config['signer']['node_sha256'], config['signer']['ethers_module'])
    try:
        ready = await database.ready()
        accounts = [await login(http, signer) for _ in range(count)]
        for account in accounts:
            status, _ = await call(http, 'GET', '/v3/me/preferences', account=account)
            if status != 200:
                raise ValueError('actual account state initialization failed')
        before = await database.snapshot([account.wallet for account in accounts])
        artifacts.document('database-before.json', snapshot_record(before, 'before_load', time.time_ns()))
        resource_started_ns = time.monotonic_ns()
        cpu_before = linux_cpu_snapshot()
        nic_before = nic_snapshot(config['nic']['name'])
        raw = artifacts.rows('requests.ndjson')
        queue_raw = artifacts.rows('queues.ndjson')
        measured = await workload(http, signer, accounts, config, raw, queue_raw, database)
        cpu_after = linux_cpu_snapshot()
        nic_after = nic_snapshot(config['nic']['name'])
        resource_duration_ns = time.monotonic_ns() - resource_started_ns
        after = await database.snapshot([account.wallet for account in accounts])
        acknowledgements = reconcile(after, accounts, measured)
        acknowledged = artifacts.rows('acknowledged-mutations.ndjson')
        for row in acknowledgements:
            acknowledged.write(canonical({'kind': 'l35_acknowledged_mutation', 'run_id': config['run_id'], **row}).decode() + '\n')
        artifacts.document('database-after.json', snapshot_record(after, 'after_load', time.time_ns()))
        resources = {'cpu_before': cpu_before, 'cpu_after': cpu_after, 'cpu_fraction': cpu_fraction(cpu_before, cpu_after), 'nic_before': nic_before, 'nic_after': nic_after, 'nic_fraction': nic_fraction(tx_before=nic_before['tx'], rx_before=nic_before['rx'], tx_after=nic_after['tx'], rx_after=nic_after['rx'], duration_ns=resource_duration_ns, speed_bps=exact_integer(config['nic']['speed_bps'], 1, 10**13, 'NIC provision')), 'observed_duration_ns': resource_duration_ns}
        artifacts.document('generator-resources.json', resources)
        failures = check_thresholds(measured, budget['limits'], resources)
        revoked = artifacts.rows('revocation.ndjson')
        revocations = []
        for account in accounts:
            revocations.append(await revocation(http, signer, database, account, config, clock, revoked))
        artifacts.document('latencies.json', measured['summary'])
        return {'mode': config['mode'], 'run_id': config['run_id'], 'bindings': dataclasses.asdict(bindings), 'workload_version': VERSION, 'host_id': config['host_id'], 'region': config['region'], 'request_count': len(measured['samples']), 'acknowledged_mutations': len(acknowledgements), 'revocation_sample_count': len(revocations), 'revocation_p99_upper_ns': {kind: percentile([row['latency_ns_upper_bound'][kind] for row in revocations], .99) for kind in ('http', 'socket')}, 'readiness': ready, 'summary': measured['summary'], 'write_offered_rps': config['rates']['profile_write'] + config['rates']['preferences_write'], 'observed_successful_write_rps': sum(value['successful'] for operation,value in measured['summary'].items() if operation.endswith('_write')) / config['duration_seconds'], 'max_inflight_observed': measured['max_inflight_observed'], 'resources': resources, 'failures': failures, 'requirements': list(REQUIREMENTS), 'final_gate': '9.1', 'capacity_qualified': False, 'monthly_availability_qualified': False, 'limitations': ['Component load only; full mixed traffic, AZ-loss and restore must use final9.1 capture.', 'Write p95/p99 are measured; no mutation latency bound is invented beyond declared lane/root authority.', 'Full Phase3 scenario evidence, D07 consumer decisions and historical trade privacy are independently required and not inferred from a profile load.']}
    finally:
        signer.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--config-sha256', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    artifacts = Artifacts(args.output)
    report = {'failures': [], 'capacity_qualified': False, 'monthly_availability_qualified': False, 'final_gate': '9.1'}
    code = 1
    started = time.time_ns()
    try:
        config = bound_document(args.config, args.config_sha256)
        bindings = checked_source(config)
        report = asyncio.run(asyncio.wait_for(run(config, bindings, artifacts), timeout=1740))
        code = 1 if report['failures'] else 0
    except Exception as error:
        report['failures'].append({'category': type(error).__name__, 'reason': str(error) if isinstance(error, (ValueError, TimeoutError)) else 'Actual provider or required artifact unavailable; private native diagnostics required.'})
    finally:
        artifacts.close()
        report.update({'exit_code': code, 'started_utc_ns': started, 'ended_utc_ns': time.time_ns(), 'config_sha256': args.config_sha256, 'artifact_sha256': artifacts.hashes(), 'qualification_claim': False})
        artifacts.document('result.json', report)
        artifacts.document('artifact-sha256.json', artifacts.hashes())
    print(json.dumps({'exit_code': code, 'report_path': str(artifacts.directory / 'result.json')}))
    return code


if __name__ == '__main__':
    raise SystemExit(main())
