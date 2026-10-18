#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time

import concurrent.futures
import math
import re
import ssl
import threading
import urllib.error
import urllib.parse
import urllib.request
import uuid


class ReleaseInputError(Exception):
    pass


def required(condition, code):
    if not condition:
        raise ReleaseInputError(code)


def strict_document(raw):
    def pairs(items):
        out = {}
        for key, value in items:
            required(key not in out, 'duplicate_json_field')
            out[key] = value
        return out
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda _: (_ for _ in ()).throw(ReleaseInputError('nonfinite_json')))


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, separators=(',', ':'), sort_keys=True, ensure_ascii=False).encode()


def utc_after(seconds):
    import datetime
    stamp = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=seconds)).isoformat(timespec='microseconds').replace('+00:00', 'Z')
    return re.sub(r'0+Z$', 'Z', re.sub(r'\.0+Z$', 'Z', stamp))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class OperatorReleaseHarness:
    def __init__(self, config, output, config_digest):
        self.c = config
        self.output = output
        self.config_digest = config_digest
        self.config_path = None
        self.lock = threading.Lock()
        self.samples = []
        self.sequence = 0
        self.marker = 'l47-' + str(uuid.uuid4())
        self.started = time.time_ns()
        self.deadline = time.monotonic() + 1500
        self.scenarios = {}
        self.roles = {}
        self.schema = ''
        self.workload_metrics = None
        self.resources = []
        self.failures = []
        self.source_hashes = {str(f.relative_to(root)): sha(f.read_bytes()) for f in sorted((root / 'security/control').iterdir()) if f.suffix in ('.go', '.py', '.ts', '.cjs')}
        self.source_digest = sha(canonical(self.source_hashes))
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context()))

    def bounded(self):
        required(time.monotonic() < self.deadline, 'lane_execution_budget_exhausted')

    def artifact(self, name, raw):
        path = self.output / name
        required(not path.exists(), 'artifact_overwrite')
        with path.open('xb') as stream:
            stream.write(raw)
        return {'path': name, 'sha256': sha(raw), 'bytes': len(raw)}

    def endpoint(self, value):
        parsed = urllib.parse.urlsplit(value)
        required(parsed.scheme in ('https', 'http') and parsed.hostname and not parsed.username and not parsed.password and not parsed.query and not parsed.fragment, 'invalid_endpoint')
        required(parsed.scheme == 'https' or parsed.hostname in ('127.0.0.1', 'localhost', '::1'), 'cleartext_nonloopback_endpoint')
        return value.rstrip('/')

    def http(self, method, path, role=None, body=None, base=None, scheduled_ns=None, category='scenario', timeout=5):
        self.bounded()
        required(path.startswith('/') and not path.startswith('//'), 'invalid_request_path')
        base = self.endpoint(base or self.c['control_url'])
        payload = canonical(body) if body is not None else None
        headers = {'Accept': 'application/json', 'Content-Type': 'application/json'}
        if role is not None:
            headers['Authorization'] = 'Bearer ' + self.roles[role]
        request = urllib.request.Request(base + path, data=payload, headers=headers, method=method)
        began = time.perf_counter_ns()
        status, raw, failure = 0, b'', None
        try:
            try:
                response = self.opener.open(request, timeout=timeout)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                status = response.status
                raw = response.read(1048577)
                required(len(raw) <= 1048576, 'http_response_exceeds_bound')
        except (OSError, urllib.error.URLError, TimeoutError):
            failure = 'transport_unavailable'
        ended = time.perf_counter_ns()
        required(not any(token.encode() in raw for token in self.roles.values()), 'credential_disclosed_in_response')
        with self.lock:
            self.sequence += 1
            index = self.sequence
            sample = {'request': index, 'method': method, 'path': path, 'role': role, 'category': category, 'scheduled_ns': scheduled_ns or began, 'started_ns': began, 'finished_ns': ended, 'elapsed_ns': ended-began, 'corrected_elapsed_ns': ended-(scheduled_ns or began), 'status': status, 'failure': failure, 'response_sha256': sha(raw), 'request_body': body}
            self.samples.append(sample)
            self.artifact(f'http-{index:06d}.response', raw)
        if failure:
            return status, {}, sample
        value = strict_document(raw)
        required(isinstance(value, dict), 'http_object_required')
        return status, value, sample

    def post(self, operation, role, body, expected=200):
        status, value, _ = self.http('POST', '/v2/control/' + operation, role, body)
        required(status == expected, 'unexpected_' + operation + '_status_' + str(status))
        return value.get('data') if expected == 200 else value

    def sql(self, statement, label):
        self.bounded()
        env = dict(os.environ)
        env['PGDATABASE'] = os.environ[self.c['observer_dsn_env']]
        env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=3000 -c lock_timeout=1000'
        result = subprocess.run([self.c['psql_path'], '--no-psqlrc', '--no-align', '--tuples-only', '--set', 'ON_ERROR_STOP=1', '--command', statement], env=env, capture_output=True, timeout=5)
        required(result.returncode == 0, 'observer_query_failed_' + label)
        required(len(result.stdout) <= 1048576, 'observer_result_exceeds_bound')
        with self.lock:
            self.sequence += 1
            self.artifact(f'sql-{self.sequence:06d}-{label}.json', result.stdout)
        return strict_document(result.stdout)

    def obj(self, kind, key):
        required(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.:-]{0,159}', key) is not None, 'invalid_object_key')
        required(kind in ('action', 'job', 'policy', 'budget', 'reservation', 'inventory', 'bounty', 'settlement', 'payout_attempt', 'incident', 'incident_outbox', 'receipt', 'quarantine'), 'invalid_object_kind')
        return self.sql(f"SELECT COALESCE((SELECT body FROM {self.schema}.objects WHERE kind='{kind}' AND id='{key}'),'null'::jsonb)", kind)

    def external(self, name):
        entry = self.c.get('external_actions', {}).get(name)
        required(isinstance(entry, dict), 'missing_real_external_action_' + name)
        argv = entry.get('argv')
        required(isinstance(argv, list) and argv and all(isinstance(v, str) for v in argv), 'invalid_external_action_' + name)
        bindings = entry.get('file_sha256')
        required(isinstance(bindings, dict) and argv[0] in bindings, 'unbound_external_executable_' + name)
        for path, expected in bindings.items():
            required(Path(path).is_absolute() and Path(path).is_file() and sha(Path(path).read_bytes()) == expected, 'external_action_digest_mismatch_' + name)
        required(not any(token in argument for token in self.roles.values() for argument in argv), 'credential_in_external_argument')
        result = subprocess.run(argv, env=dict(os.environ, L47_RUN_MARKER=self.marker), capture_output=True, timeout=30)
        required(not any(token.encode() in result.stdout or token.encode() in result.stderr for token in self.roles.values()), 'credential_disclosed_by_external_action')
        self.artifact('external-' + name + '.stdout', result.stdout)
        self.artifact('external-' + name + '.stderr', result.stderr)
        required(result.returncode == 0, 'external_action_failed_' + name)

    def rpc(self, method, params):
        status, value, _ = self.http('POST', '/', body={'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}, base=self.c['rpc_url'])
        required(status == 200 and value.get('id') == 1 and 'error' not in value and 'result' in value, 'chain_rpc_failed_' + method)
        return value['result']

    def resources_now(self):
        self.bounded()
        rows = []
        for entry in self.c['processes']:
            pid = entry['pid']
            required(isinstance(pid, int) and pid > 1, 'invalid_observed_pid')
            process = Path('/proc') / str(pid)
            required(sha((process / 'exe').read_bytes()) == entry['binary_sha256'], 'running_binary_digest_mismatch')
            stat = (process / 'stat').read_text()
            fields = stat[stat.rfind(')') + 2:].split()
            status = (process / 'status').read_text()
            rss = re.search(r'^VmRSS:\s+(\d+) kB$', status, re.M)
            required(rss is not None, 'process_rss_unavailable')
            rows.append({'pid': pid, 'binary_sha256': entry['binary_sha256'], 'cpu_ticks': int(fields[11])+int(fields[12]), 'start_ticks': int(fields[19]), 'rss_bytes': int(rss.group(1))*1024, 'monotonic_ns': time.perf_counter_ns(), 'clock_ticks_per_second': os.sysconf('SC_CLK_TCK')})
        self.resources.append(rows)
        return rows

    def preflight(self):
        c = self.c
        required(c.get('schema_version') == 4 and c.get('realm') == 'isolated-chain125' and c.get('allow_mutations') is True, 'explicit_isolated_mutation_realm_required')
        required(c.get('candidate_source_digest') == self.source_digest, 'candidate_source_digest_mismatch')
        for name in ('artifact_digest', 'configuration_revision', 'interface_digest', 'topology_sha256'):
            required(isinstance(c.get(name), str) and re.fullmatch(r'[0-9a-f]{64}', c[name]), 'missing_binding_' + name)
        topology_path = Path(c['topology_path'])
        required(topology_path.is_file() and sha(topology_path.read_bytes()) == c['topology_sha256'], 'topology_digest_mismatch')
        topology = strict_document(topology_path.read_bytes())
        required(topology['artifact_digest'] == c['artifact_digest'] and topology['configuration_revision'] == c['configuration_revision'] and topology['candidate_source_digest'] == self.source_digest and topology['interface_digest'] == c['interface_digest'], 'topology_candidate_mismatch')
        for name in ('control_url', 'market_url', 'rpc_url'):
            required(topology[name] == c[name], 'topology_endpoint_mismatch_' + name)
            self.endpoint(c[name])
        required(c['workload'] == topology['workload'], 'frozen_lane_workload_mismatch')
        required(c['actors'] == topology['actors'], 'admitted_actor_bindings_mismatch')
        required(c['processes'] == topology['processes'] and len(c['processes']) > 0, 'runtime_process_binding_required')
        required(re.fullmatch(r'[a-z][a-z0-9_]{0,62}', c['control_schema']) is not None, 'invalid_control_schema')
        self.schema = '"' + c['control_schema'] + '"'
        required(c['psql_path'] == shutil.which('psql'), 'actual_psql_required')
        required(c['observer_dsn_env'] in os.environ, 'observer_credentials_missing')
        required(len(set(c['actors'].values())) == len(c['actors']), 'distinct_role_actor_identities_required')
        for role in ('operator', 'approver', 'finance', 'moderator', 'support', 'owner', 'compromised'):
            token_env = c['credential_env'][role]
            required(token_env in os.environ and 32 <= len(os.environ[token_env]) <= 4096, 'missing_credential_' + role)
            required(re.fullmatch(r'[0-9a-f-]{36}', c['actors'][role]), 'invalid_actor_' + role)
            self.roles[role] = os.environ[token_env]
        required(len(set(self.roles.values())) == len(self.roles), 'distinct_credentials_required')
        observer = self.sql(f"SELECT json_build_object('role',current_user,'read_only',current_setting('transaction_read_only'),'objects_read',has_table_privilege(current_user,'{self.schema}.objects','SELECT'),'objects_write',has_table_privilege(current_user,'{self.schema}.objects','INSERT,UPDATE,DELETE,TRUNCATE'),'audit_read',has_table_privilege(current_user,'{self.schema}.audit','SELECT'),'audit_write',has_table_privilege(current_user,'{self.schema}.audit','INSERT,UPDATE,DELETE,TRUNCATE'))", 'observer-role')
        required(observer['read_only'] == 'on' and observer['objects_read'] and observer['audit_read'] and not observer['objects_write'] and not observer['audit_write'], 'strict_read_only_observer_required')
        required(self.rpc('eth_chainId', []) == '0x7d', 'chain125_required')
        required(c['asset'] == {'Chain': 125, 'Address': '0x85fcd13735f4309833a503ee804ea32395851479', 'Decimals': 6}, 'exact_usdl_asset_required')
        for name in ('funding_source', 'beneficiary'):
            required(re.fullmatch(r'0x[0-9a-f]{40}', c[name]) and int(c[name][2:], 16) != 0, 'invalid_' + name)
        _, view, _ = self.http('GET', '/v2/control/settlement-capability')
        self.settlement_capability = view['data']
        required(self.settlement_capability['creationProtocolFeeRaw'] == '1000000' and self.settlement_capability['feeAsset'] == c['asset'] and self.settlement_capability['gasIncluded'] is False, 'fee_or_asset_unit_disclosure_mismatch')
        self.resources_now()
        self.artifact('source-binding.json', canonical({'source_sha256': self.source_hashes, 'source_digest': self.source_digest, 'config_sha256': self.config_digest, 'artifact_digest': c['artifact_digest'], 'configuration_revision': c['configuration_revision'], 'interface_digest': c['interface_digest'], 'topology_sha256': c['topology_sha256']}))

    def transition(self, key, expected, next_state, role='operator'):
        return self.post('transition', role, {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' ' + next_state, 'next': next_state, 'expectedRevision': expected})

    def replay(self):
        key = self.marker + '.replay'
        proposal = {'key': key, 'scope': self.c['scope'], 'reason': self.marker, 'expiresAt': utc_after(240), 'payload': self.c['replay_payload']}
        malformed = dict(proposal)
        malformed['reason'] = ''
        self.post('replays', 'operator', malformed, 400)
        action = self.post('replays', 'operator', proposal)
        self.post('transition', 'operator', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' no approval', 'next': 'executing', 'expectedRevision': action['Revision']}, 403)
        self.post('transition', 'approver', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' stale approval', 'next': 'approved', 'expectedRevision': action['Revision']+1}, 409)
        self.post('transition', 'operator', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' self approval', 'next': 'approved', 'expectedRevision': action['Revision']}, 403)
        action = self.transition(key, action['Revision'], 'approved', 'approver')
        with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
            futures = [executor.submit(self.transition, key, action['Revision'], 'executing') for _ in range(10)]
            replies = [future.result() for future in futures]
        jobs = {reply['JobID'] for reply in replies}
        required(len(jobs) == 1 and '' not in jobs, 'replay_duplicate_effect')
        count = self.sql(f"SELECT count(*) FROM {self.schema}.objects WHERE kind='job' AND body->>'actionId'='{action['ID']}'", 'replay-count')
        required(count == 1, 'replay_job_count_mismatch')
        return {'responses': len(replies), 'job_id': next(iter(jobs)), 'jobs': count}

    def caps(self):
        _, before, _ = self.http('GET', '/v2/control/caps/pool_creation')
        revision = int(before['data']['revision'])
        cap = {'Capability': 'pool_creation', 'Version': 1, 'Limit': 0, 'Effective': utc_after(-1), 'Expires': utc_after(240)}
        began = time.perf_counter_ns()
        self.post('caps', 'operator', {'scope': 'platform', 'reason': self.marker + ' emergency cap', 'approvalKey': '', 'cap': cap, 'expectedRevision': revision})
        status, warning, _ = self.http('GET', '/v2/control/caps/pool_creation')
        required(status == 200 and warning['data']['allowed'] is False and warning['data']['stale'] is False, 'disabled_cap_not_propagated')
        elapsed = (time.perf_counter_ns()-began)/1e9
        required(elapsed <= 60, 'cap_propagation_over_60_seconds')
        status, _, _ = self.http('GET', self.c['market_read_path'], base=self.c['market_url'])
        required(status == 200, 'market_reads_paused_with_creation')
        browser = self.c.get('browser')
        required(isinstance(browser, dict), 'genuine_browser_configuration_missing')
        result_path = Path(browser['result_path']).resolve()
        required(result_path.parent == self.output and not result_path.exists(), 'fresh_owned_browser_output_required')
        required(browser['node_path'] == shutil.which('node'), 'actual_node_runtime_required')
        process = subprocess.run([browser['node_path'], str(root / 'security/control/phase4-browser.cjs'), '--config', str(self.config_path), '--config-sha256', self.config_digest, '--output', str(result_path)], env=dict(os.environ, L47_RUN_MARKER=self.marker), capture_output=True, timeout=45)
        required(not any(token.encode() in process.stdout or token.encode() in process.stderr for token in self.roles.values()), 'browser_disclosed_credentials')
        self.artifact('browser.stdout', process.stdout)
        self.artifact('browser.stderr', process.stderr)
        required(process.returncode == 0, 'actual_browser_cap_accessibility_failed')
        result = strict_document(result_path.read_bytes())
        required(result['run_marker'] == self.marker and result['candidate_source_digest'] == self.source_digest and result['wallet_requests'] == 0 and result['cap_disabled'] is True and result['market_visible'] is True and result['warning_visible'] is True and result['axe_violations'] == [], 'browser_cap_accessibility_failed')
        required(int(result['started_at_unix_ns']) >= self.started and int(result['finished_at_unix_ns']) <= time.time_ns(), 'browser_evidence_not_current')
        screenshot = Path(result['screenshot']['path'])
        required(screenshot == Path(str(result_path) + '.png') and sha(screenshot.read_bytes()) == result['screenshot']['sha256'], 'actual_browser_screenshot_binding_failed')
        self.artifact('browser-cap.png', screenshot.read_bytes())
        self.artifact('browser-result.json', canonical(result))
        raised = {'Capability': 'pool_creation', 'Version': 1, 'Limit': 1, 'Effective': utc_after(-1), 'Expires': utc_after(240), 'Revision': 0}
        current_revision = int(warning['data']['revision'])
        self.post('caps', 'operator', {'scope': 'platform', 'reason': self.marker + ' denied exposure increase', 'approvalKey': '', 'cap': raised, 'expectedRevision': current_revision}, 403)
        approval_key = self.marker + '.cap-approval'
        approval = self.post('actions', 'operator', {'key': approval_key, 'kind': 'cap_change', 'scope': 'platform', 'reason': self.marker + ' explicit cap increase', 'payloadHash': sha(json.dumps(raised, separators=(',', ':')).encode()), 'expiresAt': utc_after(120)})
        self.post('transition', 'approver', {'key': approval_key, 'scope': 'platform', 'reason': self.marker + ' approve cap increase', 'next': 'approved', 'expectedRevision': approval['Revision']})
        self.post('caps', 'operator', {'scope': 'platform', 'reason': self.marker + ' dual approved cap increase', 'approvalKey': approval_key, 'cap': raised, 'expectedRevision': current_revision})
        raised['Limit'] = 0
        self.post('caps', 'operator', {'scope': 'platform', 'reason': self.marker + ' restore emergency disabled', 'approvalKey': '', 'cap': raised, 'expectedRevision': current_revision+1})
        audits = self.sql(f"SELECT count(*) FROM {self.schema}.audit WHERE operation='caps' AND reason LIKE '{self.marker}%' AND result='ok'", 'cap-audits')
        required(audits == 3, 'cap_mutations_missing_immutable_audits')
        return {'cap_propagation_seconds': elapsed, 'browser': result, 'audited_cap_changes': audits, 'dual_approval': True}

    def recommendations(self):
        owner = self.c['actors']['owner']
        old_budget = self.obj('budget', owner + '.usdl')
        used = old_budget['Used'] if old_budget else 0
        budget = {'Owner': owner, 'Asset': self.c['asset'], 'Requests': used+1, 'Spend': '100000000', 'Expires': utc_after(240)}
        self.post('budget', 'owner', {'reason': self.marker + ' explicit opt in', 'budget': budget, 'expectedRevision': old_budget['Revision'] if old_budget else 0})
        policies = [str(uuid.uuid4()), str(uuid.uuid4())]
        for policy in policies:
            self.post('policies', 'owner', {'reason': self.marker + ' disclosed policy', 'policy': {'ID': policy, 'Owner': owner, 'Mode': 'disclosed_assistant', 'Disclosure': 'Automated recommendation only', 'Asset': self.c['asset'], 'Instruments': [self.c['instrument']], 'Requests': 20, 'Spend': '100000000', 'QuoteAge': 60000000000, 'Expires': utc_after(240)}})
            self.post('policy-state', 'owner', {'key': policy, 'reason': self.marker + ' enable', 'next': 'enabled', 'expectedRevision': 1})
        def reserve(index):
            return self.http('POST', '/v2/control/recommendations', 'owner', {'policyId': policies[index%2], 'key': self.marker + '.request' + str(index), 'instrument': self.c['instrument'], 'reason': self.marker + ' recommend', 'quoteAt': utc_after(-1)})
        with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
            rows = list(executor.map(reserve, range(10)))
        accepted = [row[1]['data'] for row in rows if row[0] == 200]
        required(len(accepted) == 1 and all(row[0] == 200 or (row[0] == 503 and row[1].get('rule') == 'budget_exceeded') for row in rows), 'aggregate_budget_race_failed')
        suggestion = accepted[0]
        winning_index = next(index for index, row in enumerate(rows) if row[0] == 200)
        status, history, _ = self.http('GET', '/v2/control/recommendations/' + self.marker + '.request' + str(winning_index), 'owner')
        required(status == 200 and history['data']['organicRankingEligible'] is False and history['data']['origin'] == 'automated_recommendation', 'real_automation_disclosure_missing')
        required(suggestion['Amount'] == '0' and suggestion['State'] == 'suggestion' and suggestion['Disclosure'] and not any('hash' in key.lower() or 'balance' in key.lower() for key in suggestion), 'fabricated_recommendation_effect')
        status, campaign, _ = self.http('GET', '/v2/control/campaigns/' + suggestion['PolicyID'], 'owner')
        required(status == 200 and campaign['data']['state'] == 'blocked' and 'delegated_execution_unavailable' in campaign['data']['capabilityBlockers'], 'unsupported_delegation_reported_enabled')
        current = self.obj('budget', owner + '.usdl')
        required(current['Used'] == used+1 and current['Reserved'] == '0', 'durable_budget_mismatch')
        for policy in policies:
            record = self.obj('policy', policy)
            self.post('policy-state', 'owner', {'key': policy, 'reason': self.marker + ' owner revoke', 'next': 'revoked', 'expectedRevision': record['Revision']})
        status, denial, _ = reserve(11)
        required(status == 403 and denial.get('rule') == 'scope_denied', 'queued_recommendation_survived_revocation')
        return {'durable_reservations': 1, 'budget_used_delta': 1, 'suggestion': suggestion, 'recommendation_view': history['data']}

    def bounty(self, key, raw):
        return {'ID': key, 'Title': 'Qualification funded work', 'Rules': self.c['bounty_rules'], 'FundingSource': self.c['funding_source'], 'RefundDisposition': self.c['refund_disposition'], 'Amount': raw, 'Asset': self.c['asset'], 'Deadline': utc_after(600)}

    def bounty_state(self, key, expected, state, evidence='', destination=''):
        return self.post('bounty-state', 'finance', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' ' + state, 'next': state, 'expectedRevision': expected, 'evidence': evidence, 'destination': destination})

    def unfunded(self):
        inventory = self.obj('inventory', 'usdl')
        required(inventory is None or inventory['Confirmed'] == '0', 'zero_confirmed_inventory_prerequisite_missing')
        key = str(uuid.uuid4())
        self.post('bounties', 'finance', {'scope': self.c['scope'], 'reason': self.marker, 'bounty': self.bounty(key, '100000000')})
        result = self.post('bounty-state', 'finance', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' unfunded activate', 'next': 'active', 'expectedRevision': 1}, 503)
        required(result['rule'] == 'funding_required' and self.obj('bounty', key)['State'] == 'draft', 'unfunded_bounty_activated')
        return {'bounty_id': key, 'funding_required': True}

    def funded(self):
        self.external('prepare_real_100_usdl_inventory')
        self.post('inventory', 'finance', {'scope': 'platform', 'reason': self.marker + ' independent funding reconciliation', 'asset': self.c['asset']})
        inventory = self.obj('inventory', 'usdl')
        required(inventory['Confirmed'] == '100000000' and inventory['Reserved'] == '0' and inventory['Pending'] == '0' and inventory['Frozen'] is False and inventory['Source'] == self.c['funding_source'], 'exact_real_100_inventory_required')
        chain_balance = self.rpc('eth_call', [{'to': self.c['asset']['Address'], 'data': '0x70a08231' + self.c['funding_source'][2:].zfill(64)}, 'latest'])
        required(int(chain_balance, 16) == 100000000, 'independent_funding_balance_mismatch')
        required(self.rpc('eth_getCode', [self.c['asset']['Address'], 'latest']) not in ('0x', '0x0'), 'actual_usdl_contract_missing')
        keys = [str(uuid.uuid4()), str(uuid.uuid4())]
        for key in keys:
            self.post('bounties', 'finance', {'scope': self.c['scope'], 'reason': self.marker, 'bounty': self.bounty(key, '80000000')})
        def fund(key):
            return key, self.http('POST', '/v2/control/bounty-state', 'finance', {'key': key, 'scope': self.c['scope'], 'reason': self.marker + ' reserve eighty', 'next': 'funded', 'expectedRevision': 1})
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(fund, keys))
        winners = [key for key, response in results if response[0] == 200]
        required(len(winners) == 1 and all(response[0] == 200 or (response[0] == 503 and response[1].get('rule') == 'funding_required') for _, response in results), 'competing_reward_reservations_failed')
        inventory = self.obj('inventory', 'usdl')
        required(inventory['Reserved'] == '80000000' and inventory['Pending'] == '80000000', 'reward_inventory_overreserved')
        cancel_key = str(uuid.uuid4())
        self.post('bounties', 'finance', {'scope': self.c['scope'], 'reason': self.marker, 'bounty': self.bounty(cancel_key, '20000000')})
        self.bounty_state(cancel_key, 1, 'funded')
        self.bounty_state(cancel_key, 2, 'cancelled')
        cancelled_inventory = self.obj('inventory', 'usdl')
        required(cancelled_inventory['Reserved'] == '80000000' and cancelled_inventory['Pending'] == '80000000' and cancelled_inventory['Reversed'] == '20000000', 'published_cancellation_disposition_not_durable')
        self.winning_bounty = winners[0]
        return {'awards': 2, 'accepted_reservations': 1, 'reserved_raw': inventory['Reserved']}

    def settlement(self):
        required(self.settlement_capability['available'] is True, 'genuine_settlement_authority_missing')
        required(hasattr(self, 'winning_bounty'), 'actual_funded_bounty_prerequisite_missing')
        key = self.winning_bounty
        document = self.c.get('submission')
        required(isinstance(document, dict), 'real_submission_artifact_missing')
        self.external('prepare_real_submission_artifact')
        self.bounty_state(key, 2, 'active')
        submission = str(uuid.uuid4())
        self.post('bounty-submissions', 'owner', {'scope': self.c['scope'], 'reason': self.marker + ' submit actual artifact', 'submission': {'ID': submission, 'BountyID': key, 'Author': self.c['actors']['owner'], 'Wallet': self.c['beneficiary'], 'Body': document['body'], 'EvidenceURLs': document['evidence_urls']}})
        self.bounty_state(key, 3, 'submitted', submission)
        self.bounty_state(key, 4, 'adjudicating', submission)
        from_block = self.rpc('eth_blockNumber', [])
        self.external('enable_real_payout_response_timeout')
        try:
            self.bounty_state(key, 5, 'awarded', submission, self.c['beneficiary'])
            settlement_key = 'bounty.' + key
            self.http('POST', '/v2/control/settlements', 'finance', {'scope': self.c['scope'], 'key': settlement_key, 'reason': self.marker + ' accepted payout response lost'})
            observed = self.obj('settlement', settlement_key)
            required(observed['State'] == 'unknown' and observed['SettlementReference'] == '', 'timeout_masqueraded_as_confirmed_payout')
            required(self.obj('payout_attempt', settlement_key)['Submitted'] is True, 'timeout_submission_intent_not_durable')
        finally:
            self.external('disable_real_payout_response_timeout')
        until = time.monotonic() + 60
        while True:
            self.http('POST', '/v2/control/settlements', 'finance', {'scope': self.c['scope'], 'key': settlement_key, 'reason': self.marker + ' reconcile original payout'})
            observed = self.obj('settlement', settlement_key)
            if observed['State'] == 'settled':
                break
            required(time.monotonic() < until, 'actual_payout_reconciliation_unavailable')
            time.sleep(0.2)
        reference = observed['SettlementReference']
        receipt = self.rpc('eth_getTransactionReceipt', [reference])
        required(receipt and receipt['status'] == '0x1' and receipt['transactionHash'].lower() == reference.lower(), 'successful_chain125_receipt_required')
        block = self.rpc('eth_getBlockByNumber', [receipt['blockNumber'], False])
        required(block and block['hash'].lower() == receipt['blockHash'].lower(), 'payout_receipt_not_in_committed_block')
        transfer = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef'
        to_block = self.rpc('eth_blockNumber', [])
        required(int(to_block, 16)-int(from_block, 16) <= 1000, 'payout_log_range_exceeds_bound')
        logs = self.rpc('eth_getLogs', [{'address': self.c['asset']['Address'], 'fromBlock': from_block, 'toBlock': to_block, 'topics': [transfer, '0x'+self.c['funding_source'][2:].zfill(64), '0x'+self.c['beneficiary'][2:].zfill(64)]}])
        payments = [log for log in logs if int(log['data'], 16) == 80000000 and not log.get('removed', False)]
        required(len(payments) == 1 and payments[0]['transactionHash'].lower() == reference.lower(), 'duplicate_or_unlinked_external_payout')
        for _ in range(9):
            self.post('settlements', 'finance', {'scope': self.c['scope'], 'key': settlement_key, 'reason': self.marker + ' payout retry'})
        inventory = self.obj('inventory', 'usdl')
        required(inventory['Pending'] == '0' and inventory['Reserved'] == '0' and inventory['Settled'] == '80000000', 'settled_reward_ledger_mismatch')
        self.artifact('confirmed-payout-receipt.json', canonical(receipt))
        self.external('create_real_inventory_drift')
        self.post('inventory', 'finance', {'scope': 'platform', 'reason': self.marker + ' reconcile actual drift', 'asset': self.c['asset']})
        required(self.obj('inventory', 'usdl')['Frozen'] is True, 'actual_inventory_drift_did_not_freeze_rewards')
        return {'settlement_key': settlement_key, 'confirmed_transaction': reference, 'external_payments': 1, 'settled_raw': '80000000', 'gas_included': self.settlement_capability['gasIncluded']}

    def incident(self):
        key = self.marker + '.compromise'
        began = time.perf_counter_ns()
        self.post('incidents', 'operator', {'scope': self.c['scope'], 'key': key, 'reason': self.marker + ' credential compromise', 'identity': self.c['actors']['compromised'], 'evidence': self.c['incident_detection_reference']})
        status, _, _ = self.http('POST', '/v2/control/replays', 'compromised', {'key': self.marker + '.denied', 'scope': self.c['scope'], 'reason': self.marker, 'expiresAt': utc_after(120), 'payload': self.c['replay_payload']})
        required(status in (401, 403) and (time.perf_counter_ns()-began)/1e9 <= 60, 'compromised_credential_not_contained')
        containment_seconds = (time.perf_counter_ns()-began)/1e9
        while True:
            outbox = self.obj('incident_outbox', key)
            if outbox['state'] == 'completed':
                break
            required((time.perf_counter_ns()-began)/1e9 <= 60, 'genuine_secret_cache_revocation_missing')
            time.sleep(0.2)
        revocation_seconds = (time.perf_counter_ns()-began)/1e9
        payload = {'incident': key, 'rotation': outbox['rotation'], 'integrity': outbox['integrity']}
        approval = self.post('actions', 'operator', {'key': key + '.recovery', 'kind': 'incident_recovery', 'scope': self.c['scope'], 'reason': self.marker + ' approved recovery', 'payloadHash': sha(canonical(payload)), 'expiresAt': utc_after(120)})
        self.transition(key + '.recovery', approval['Revision'], 'approved', 'approver')
        self.post('recover-incident', 'operator', {'scope': self.c['scope'], 'key': key, 'reason': self.marker + ' recover verified integrity', 'approvalKey': key + '.recovery', 'rotation': outbox['rotation'], 'integrity': outbox['integrity'], 'expectedRevision': 1})
        required(self.obj('incident', key)['State'] == 'recovered', 'approved_recovery_not_durable')
        return {'containment_seconds': containment_seconds, 'secret_cache_revocation_seconds': revocation_seconds, 'revocation_acknowledged': True, 'recovery': 'recovered'}

    def denied_treasury(self):
        before = self.obj('inventory', 'usdl')
        reason = self.marker + ' denied treasury adjustment'
        self.post('inventory', 'moderator', {'scope': 'platform', 'reason': reason, 'asset': self.c['asset']}, 403)
        after = self.obj('inventory', 'usdl')
        required(before == after, 'moderator_modified_treasury')
        audit = self.sql(f"SELECT COALESCE(json_agg(row_to_json(a)),'[]'::json) FROM {self.schema}.audit a WHERE actor='{self.c['actors']['moderator']}' AND reason='{reason}' AND operation='inventory' AND result='scope_denied'", 'immutable-denial')
        required(len(audit) == 1, 'immutable_denial_audit_missing')
        triggers = self.sql(f"SELECT COALESCE(json_agg(pg_get_triggerdef(t.oid)),'[]'::json) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='{self.c['control_schema']}' AND c.relname='audit' AND NOT t.tgisinternal AND t.tgenabled IN ('O','A')", 'audit-triggers')
        required(any(all(operation in trigger for operation in ('UPDATE', 'DELETE', 'TRUNCATE')) for trigger in triggers), 'append_only_audit_trigger_missing')
        for role, dataset in [('support', 'private_messages'), ('support', 'payment_secrets'), ('moderator', 'finance_liabilities'), ('operator', 'finance_liabilities'), ('finance', 'infra_health')]:
            self.post('access', role, {'scope': 'platform', 'reason': self.marker + ' role minimization', 'dataset': dataset, 'approvalKey': '', 'bulk': False}, 403)
        approval_key = self.marker + '.bulk'
        export_payload = {'dataset': 'finance_liabilities', 'scope': 'platform'}
        approval = self.post('actions', 'operator', {'key': approval_key, 'kind': 'bulk_export', 'scope': 'platform', 'reason': self.marker + ' export approval', 'payloadHash': sha(canonical(export_payload)), 'expiresAt': utc_after(120)})
        self.post('transition', 'approver', {'key': approval_key, 'scope': 'platform', 'reason': self.marker + ' second export operator', 'next': 'approved', 'expectedRevision': approval['Revision']})
        self.post('access', 'finance', {'scope': 'platform', 'reason': self.marker + ' approved liability export', 'dataset': 'finance_liabilities', 'approvalKey': approval_key, 'bulk': True})
        self.post('access', 'finance', {'scope': 'platform', 'reason': self.marker + ' rejected export reuse', 'dataset': 'finance_liabilities', 'approvalKey': approval_key, 'bulk': True}, 403)
        self.denial_audit = audit[0]
        return {'http_status': 403, 'denial_audit_seq': audit[0]['seq'], 'treasury_unchanged': True}

    def workload(self):
        spec = self.c['workload']
        duration, warmup, reads, writes, workers = (spec[k] for k in ('duration_seconds', 'warmup_seconds', 'market_read_rps', 'control_write_rps', 'max_inflight'))
        required(all(type(v) is int for v in (duration, warmup, reads, writes, workers)) and 1 <= duration <= 600 and 0 <= warmup <= 60 and 1 <= reads <= 10000 and 0 <= writes <= 2000 and 1 <= workers <= 64 and reads+writes <= 10000, 'invalid_lane_workload_allocation')
        required(spec['global_read_write_budget_is_shared'] is True, 'lane_budget_cannot_add_to_global_total')
        before = self.resources_now()
        def execute(index, due, stage):
            if index % (reads+writes) < reads:
                return self.http('GET', self.c['market_read_path'], base=self.c['market_url'], scheduled_ns=due, category=stage + '_market_read')
            return self.http('POST', '/v2/control/access', 'finance', {'scope': 'platform', 'reason': self.marker + ' measured liabilities access', 'dataset': 'finance_liabilities', 'approvalKey': '', 'bulk': False}, scheduled_ns=due, category=stage + '_control_write')
        dropped = []
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
            for stage, seconds in (('warmup', warmup), ('measured', duration)):
                base = time.perf_counter_ns()
                active = set()
                for index in range((reads+writes)*seconds):
                    self.bounded()
                    due = base + index*1000000000//(reads+writes)
                    delay = (due-time.perf_counter_ns())/1e9
                    if delay > 0:
                        time.sleep(delay)
                    finished = {future for future in active if future.done()}
                    for future in finished:
                        future.result()
                    active -= finished
                    if len(active) >= workers:
                        dropped.append({'stage': stage, 'index': index, 'scheduled_ns': due, 'reason': 'generator_inflight_exhausted'})
                    else:
                        active.add(executor.submit(execute, index, due, stage))
                for future in active:
                    future.result()
        after = self.resources_now()
        measured = [row for row in self.samples if row['category'].startswith('measured_')]
        metrics = {}
        for category, target in (('market_read', reads), ('control_write', writes)):
            rows = [row for row in measured if row['category'] == 'measured_' + category]
            accepted = [row for row in rows if row['status'] == 200 and row['failure'] is None]
            latencies = sorted(row['corrected_elapsed_ns']/1e6 for row in accepted)
            p95 = latencies[math.ceil(len(latencies)*0.95)-1] if latencies else None
            p99 = latencies[math.ceil(len(latencies)*0.99)-1] if latencies else None
            metrics[category] = {'planned': target*duration, 'attempted': len(rows), 'accepted': len(accepted), 'admission_denied': sum(row['status'] in (429, 503) for row in rows), 'unplanned_errors': sum(row['failure'] is not None or row['status'] >= 500 for row in rows), 'accepted_rps': len(accepted)/duration, 'corrected_p95_ms': p95, 'corrected_p99_ms': p99}
        self.workload_metrics = {'duration_seconds': duration, 'warmup_seconds_excluded': warmup, 'generator_dropped': len(dropped), 'metrics': metrics, 'latency_scope': 'scheduled generator to HTTP response including actual transport latency', 'coordinated_omission': 'latency begins at scheduled open-loop launch', 'resources_before': before, 'resources_after': after, 'shared_global_acceptance': 'deferred_serial9.1'}
        self.artifact('generator-dropped.json', canonical(dropped))
        required(not dropped, 'generator_capacity_insufficient')
        for category, values in metrics.items():
            required(values['attempted'] == values['planned'] and values['accepted'] == values['planned'], 'allocated_lane_traffic_not_admitted_' + category)
        read = metrics['market_read']
        required(read['corrected_p95_ms'] <= 150 and read['corrected_p99_ms'] <= 400, 'measured_read_latency_exceeds_frozen_slo')
        required(all(a['start_ticks'] == b['start_ticks'] for a, b in zip(before, after)), 'observed_process_restarted_during_workload')
        return self.workload_metrics

    def run(self):
        self.preflight()
        handlers = [(1, self.replay), (2, self.caps), (3, self.recommendations), (4, None), (5, self.unfunded), (6, self.funded), (7, self.settlement), (8, self.incident), (9, self.denied_treasury)]
        for number, handler in handlers:
            if number == 4:
                self.scenarios['4'] = dict(self.scenarios['3'])
                continue
            try:
                result = handler()
                self.scenarios[str(number)] = {'status': 'passed', 'observed': result, 'at_unix_ns': time.time_ns()}
            except (ReleaseInputError, KeyError, OSError, ValueError, subprocess.SubprocessError) as error:
                code = str(error) if isinstance(error, ReleaseInputError) else type(error).__name__
                self.scenarios[str(number)] = {'status': 'blocked' if isinstance(error, (KeyError, ReleaseInputError)) and ('missing' in code or 'required' in code or 'unavailable' in code) else 'failed', 'code': code, 'at_unix_ns': time.time_ns()}
                self.failures.append({'scenario': number, 'code': code})
        try:
            self.workload()
        except (ReleaseInputError, KeyError, OSError, ValueError, subprocess.SubprocessError) as error:
            self.failures.append({'scenario': 'capacity', 'code': str(error) if isinstance(error, ReleaseInputError) else type(error).__name__})
        if hasattr(self, 'denial_audit'):
            persisted = self.sql(f"SELECT row_to_json(a) FROM {self.schema}.audit a WHERE seq={self.denial_audit['seq']}", 'retained-denial')
            required(persisted == self.denial_audit, 'immutable_denial_audit_changed')
        self.resources_now()

    def finish(self):
        self.artifact('raw-http-samples.jsonl', b''.join(canonical(row)+b'\n' for row in self.samples))
        self.artifact('resource-observations.json', canonical(self.resources))
        unchanged = {str(f.relative_to(root)): sha(f.read_bytes()) for f in sorted((root / 'security/control').iterdir()) if f.suffix in ('.go', '.py', '.ts', '.cjs')}
        if unchanged != self.source_hashes:
            self.failures.append({'scenario': 'binding', 'code': 'source_changed_during_execution'})
        passed = len(self.scenarios) == 9 and all(row['status'] == 'passed' for row in self.scenarios.values()) and not self.failures
        groups = {'D15_021': '1', 'D15_022': '2', 'D15_023': '3', 'D15_024': '4', 'D15_025': '5', 'D15_026': '6', 'D15_027': '7', 'D15_028': '8', 'D15_030': '9'}
        criteria = {f'{group}.{index}': {'scenario': scenario, 'status': self.scenarios.get(scenario, {'status': 'blocked'})['status']} for group, scenario in groups.items() for index in range(1, 6)}
        dependencies = {'D15_025.2': ['6'], 'D15_025.3': ['6'], 'D15_025.4': ['7'], 'D15_025.5': ['6'], 'D15_026.3': ['6', '7'], 'D15_026.4': ['7'], 'D15_026.5': ['7']}
        for criterion, needed in dependencies.items():
            criteria[criterion]['status'] = 'passed' if all(self.scenarios.get(number, {}).get('status') == 'passed' for number in needed) else 'blocked'
            criteria[criterion]['additional_scenarios'] = needed
        criteria['D15_023.3']['status'] = 'blocked'
        criteria['D15_023.3']['reason'] = 'Supported delegated execution remains unavailable; recommendation disclosure proves denial, not delegation authority.'
        criteria['D15_023.5']['status'] = 'blocked'
        criteria['D15_023.5']['reason'] = 'Real affected discovery surface registration and label journey remain a serial integration input.'
        criteria['D15_026.4']['status'] = 'blocked'
        criteria['D15_026.4']['reason'] = 'Individual reconciliation is observed; sustained scheduled 60-second reconciliation evidence belongs to the real serial9.1 workload/restore run.'
        passed = passed and all(item['status'] == 'passed' for item in criteria.values())
        files = {path.name: {'sha256': sha(path.read_bytes()), 'bytes': path.stat().st_size} for path in sorted(self.output.iterdir()) if path.is_file()}
        report = {'task': '4.47', 'run_marker': self.marker, 'started_at_unix_ns': self.started, 'finished_at_unix_ns': time.time_ns(), 'config_sha256': self.config_digest, 'candidate_source_digest': self.source_digest, 'artifact_digest': self.c.get('artifact_digest'), 'configuration_revision': self.c.get('configuration_revision'), 'interface_digest': self.c.get('interface_digest'), 'topology_sha256': self.c.get('topology_sha256'), 'scenarios': self.scenarios, 'criterion_scenario_coverage': criteria, 'lane_measurements_passed': passed, 'workload': self.workload_metrics, 'failures': self.failures, 'artifacts': files, 'task_qualified': False, 'production_qualified': False, 'global_capacity_qualified': False, 'shared_load_restore_acceptance': 'deferred_to_serial9.1'}
        self.artifact('phase-4.json', canonical(report)+b'\n')
        return 0 if passed else 1


def run_phase4(arguments):
    required(arguments.config and arguments.config_sha256 and arguments.output, 'phase4_config_digest_output_required')
    raw = Path(arguments.config).read_bytes()
    required(len(raw) <= 1048576 and sha(raw) == arguments.config_sha256, 'phase4_configuration_digest_mismatch')
    config = strict_document(raw)
    output = Path(arguments.output).resolve()
    allowed = (root / 'evidence/production-system/L47/phase4').resolve()
    required(output.parent == allowed and not output.exists() and not output.is_symlink(), 'fresh_owned_phase4_output_required')
    output.mkdir(parents=True)
    harness = OperatorReleaseHarness(config, output, arguments.config_sha256)
    harness.config_path = Path(arguments.config).resolve()
    try:
        harness.run()
    except (ReleaseInputError, KeyError, OSError, ValueError, subprocess.SubprocessError) as error:
        harness.failures.append({'scenario': 'preflight', 'code': str(error) if isinstance(error, ReleaseInputError) else type(error).__name__})
    code = harness.finish()
    print(json.dumps({'task': '4.47', 'exit_code': code, 'report_path': str(output / 'phase-4.json'), 'global_acceptance': 'deferred_serial9.1'}))
    return code


root = Path(__file__).resolve().parents[2]

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--config-sha256', required=True)
    parser.add_argument('--output', required=True)
    raise SystemExit(run_phase4(parser.parse_args()))
