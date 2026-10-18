from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
BROWSER = ROOT / 'client/apps/web/src/widgets/voice-rooms/qualify-phase4.mjs'
FAULTS = ('safety_dependency_loss', 'single_node_loss', 'provider_unavailable', 'provider_create_db_crash', 'moderation_replay')
SCENARIOS = {str(i) for i in range(1, 13)}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1048576), b''):
            h.update(block)
    return h.hexdigest()


def pinned(entry):
    path = Path(entry['path']).resolve(strict=True)
    require(path.is_file() and not path.is_symlink(), 'regular pinned input required')
    require(re.fullmatch(r'[0-9a-f]{64}', entry['sha256']) is not None, 'exact SHA256 required')
    require(digest(path) == entry['sha256'], 'pinned input digest mismatch')
    return path


def source_snapshot():
    path = ROOT / 'tools/production/qualify.py'
    module_spec = importlib.util.spec_from_file_location('l38_source_binding', path)
    module = importlib.util.module_from_spec(module_spec)
    module_spec.loader.exec_module(module)
    return module.snapshot(ROOT)['sha256']


def histogram(samples):
    values = sorted(samples)
    require(values and all(math.isfinite(v) and v >= 0 for v in values), 'real nonnegative latency samples required')
    bounds = [1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000]
    buckets = [0] * (len(bounds) + 1)
    for value in values:
        index = next((i for i, bound in enumerate(bounds) if value <= bound), len(bounds))
        buckets[index] += 1
    return {'unit': 'milliseconds', 'count': len(values), 'sum': sum(values), 'minimum': values[0], 'maximum': values[-1], 'bounds': bounds, 'buckets': buckets, **{f'p{q}': values[math.ceil(q / 100 * len(values)) - 1] for q in (50, 95, 99)}}


class Runner:
    def __init__(self, config, output):
        self.config = config
        self.output = output
        self.output.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.events = []
        self.scenarios = {}
        self.active_faults = set()
        self.outage_interval = {}
        self.global_process = None
        self.raw = (output / 'observations.ndjson').open('x', encoding='utf8')
        self.sequence = 0
        self.started = time.monotonic_ns()
        self.started_unix_ns = time.time_ns()
        self.deadline = time.monotonic() + config['durationSeconds']

    def record(self, event):
        require(isinstance(event, dict), 'object observation required')
        self.sequence += 1
        value = dict(event, sequence=self.sequence, captured_monotonic_ns=time.monotonic_ns(), captured_unix_ns=time.time_ns(), environmentId=self.config['environmentId'], sourceSnapshot=self.config['sourceSnapshot'], buildDigest=self.config['buildDigest'])
        self.raw.write(json.dumps(value, separators=(',', ':'), allow_nan=False) + '\n')
        self.raw.flush()
        self.events.append(value)

    def command(self, entry, name, background=False):
        executable = pinned(entry['executable'])
        require(entry['argv'][0] == str(executable), 'command executable binding mismatch')
        require(entry['environmentId'] == self.config['environmentId'], 'fault command environment mismatch')
        log = (self.output / (name + '.log')).open('xb')
        process = subprocess.Popen(entry['argv'], cwd=ROOT, env=os.environ.copy(), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        if background:
            return process, log
        try:
            code = process.wait(timeout=min(entry['timeoutSeconds'], max(1, self.deadline - time.monotonic())))
            self.record({'kind': 'operation', 'name': name, 'exitCode': code, 'logSha256': digest(log.name)})
            require(code == 0, 'approved operation failed: ' + name)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=5)
            log.close()

    def sql(self, name, query):
        require(re.fullmatch(r'[a-z_][a-z0-9_]*', self.config['databaseSchema']) is not None, 'exact database schema required')
        query = 'SET default_transaction_read_only=on;SET search_path="' + self.config['databaseSchema'] + '"; ' + query
        result = subprocess.run([str(pinned(self.config['psql'])), '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-c', query], cwd=ROOT, env=os.environ.copy(), capture_output=True, text=True, timeout=10)
        require(result.returncode == 0, 'database reconciliation failed: ' + name)
        lines = [line for line in result.stdout.splitlines() if line and line != 'SET']
        require(len(lines) == 1, 'exact SQL observation required')
        value = json.loads(lines[0])
        self.record({'kind': 'database', 'name': name, 'value': value})
        return value

    def outage_counts(self, name):
        prefix = 'l38-' + self.config['runId'] + '-outage-'
        return self.sql(name, "SELECT json_build_object('messages',(SELECT count(*) FROM chat_messages WHERE client_id LIKE '" + prefix + "%'),'outbox',(SELECT count(*) FROM chat_outbox o JOIN chat_messages m ON m.id=o.message_id WHERE m.client_id LIKE '" + prefix + "%'))")

    def audit_snapshot(self, name):
        value = self.sql(name, "SELECT COALESCE(json_agg(json_build_object('id',id,'caseId',case_id,'actor',actor,'kind',kind,'revision',revision,'sessionId',session_id,'role',actor_role,'scope',actor_scope,'reason',reason,'policyVersion',policy_version,'evidenceHash',evidence_hash) ORDER BY id),'[]'::json) FROM (SELECT * FROM safety_actions ORDER BY id LIMIT 10001) bounded")
        require(len(value) <= 10000, 'bounded audit capture exceeded; no partial durability claim')
        return value

    def control(self, event):
        stage = event['stage']
        if stage.startswith('oracle:'):
            name, identifier = event['name'], event['id']
            require(stage == 'oracle:' + name + ':' + identifier, 'oracle correlation mismatch')
            if name == 'orphan-alarm':
                require(identifier == self.config['failureRooms']['createCrash']['roomId'], 'alarm must bind actual crash room')
                log_path = Path(self.config['orphanAlarmLogPath']).resolve(strict=True)
                require(log_path.is_file(), 'actual provider orphan alarm capture missing')
                records = [json.loads(line) for line in log_path.read_text().splitlines() if line]
                alarms = [row for row in records if row.get('msg') == 'voice reconciliation requires attention' and row.get('orphans', 0) > 0 and row.get('environmentId') == self.config['environmentId'] and row.get('buildDigest') == self.config['buildDigest'] and row.get('observedAtUnixNs', 0) >= self.started_unix_ns]
                require(alarms, 'orphan older than five minutes lacked actual runtime alarm')
                self.record({'kind': 'orphan-alarm', 'alarms': alarms, 'rawSha256': digest(log_path)})
                return {'alarmCount': len(alarms), 'rawSha256': digest(log_path)}
            if name in ('case-action', 'linked-action', 'ban-actor', 'voice-room'):
                require(re.fullmatch(r'[0-9a-f-]{36}', identifier) is not None, 'actual UUID oracle target required')
            elif name in ('reviewer-denial', 'reviewer-denial-before'):
                require(re.fullmatch(r'0x[0-9a-f]{40}', identifier) is not None, 'actual wallet oracle target required')
            queries = {
                'case-action': "SELECT json_build_object('count',count(*)) FROM safety_actions WHERE case_id='" + identifier + "' AND kind IN ('actioned','dismissed')",
                'linked-action': "SELECT json_build_object('actionId',action_id) FROM safety_action_bans WHERE ban_id='" + identifier + "'",
                'ban-actor': "SELECT json_build_object('actor',actor) FROM safety_bans WHERE id='" + identifier + "'",
                'voice-room': "SELECT json_build_object('state',state,'revision',revision,'providerSession',provider_session,'startKey',start_key) FROM voice_rooms WHERE id='" + identifier + "'",
                'reviewer-denial': "SELECT json_build_object('count',count(*),'actor','" + identifier + "') FROM safety_actions WHERE actor='" + identifier + "' AND kind='denied:ban'",
                'reviewer-denial-before': "SELECT json_build_object('count',count(*)) FROM safety_actions WHERE actor='" + identifier + "' AND kind='denied:ban'",
            }
            require(name in queries, 'unregistered database oracle')
            return self.sql(stage, queries[name])
        if stage.endswith(':begin'):
            fault = stage.removesuffix(':begin')
            require(fault in FAULTS, 'undeclared fault stage')
            if fault == 'single_node_loss':
                self.before_failure = self.audit_snapshot('acknowledged-safety-before-node-loss')
            if fault == 'safety_dependency_loss':
                self.before_outage = self.outage_counts('outage-write-count-before')
            self.command(self.config['faults'][fault]['begin'], fault + '-begin')
            self.active_faults.add(fault)
            if fault == 'safety_dependency_loss':
                self.outage_interval['begin'] = time.time_ns()
        elif stage.endswith(':end'):
            fault = stage.removesuffix(':end')
            require(fault in self.active_faults, 'unmatched fault recovery')
            if fault == 'safety_dependency_loss':
                self.outage_interval['end'] = time.time_ns()
            self.command(self.config['faults'][fault]['end'], fault + '-end')
            self.active_faults.remove(fault)
            if fault == 'single_node_loss':
                after = self.audit_snapshot('acknowledged-safety-after-node-loss')
                rows = {row['id']: row for row in after}
                require(self.before_failure and all(rows.get(row['id']) == row for row in self.before_failure), 'acknowledged immutable safety decision lost or changed')
            if fault == 'safety_dependency_loss':
                after = self.outage_counts('outage-write-count-after')
                require(after == self.before_outage, 'denied dependency-loss writes committed')
        else:
            raise ValueError('unsupported control stage')

    def run(self):
        expected = json.loads(pinned(self.config['expectedSchema']).read_text())
        require(expected['service'] == 'media/social', 'actual social schema attestation required')
        observed = self.sql('runtime-schema-readiness', "SELECT json_build_object('role',current_user,'registry_digest',registry_digest,'interface_digest',interface_digest,'reader_min_version',reader_min_version,'migrations',migrations) FROM migration_control.readiness('media/social')")
        require(observed['role'] == expected['runtime_role'] and observed['registry_digest'] == expected['registry_digest'] == self.config['databaseSchemaVersion'] and observed['interface_digest'] == expected['interface_digest'] and 1 <= observed['reader_min_version'] <= expected['reader_version'] and observed['migrations'] == expected['migrations'], 'actual runtime schema/role/migration binding disagrees')
        before_audit = self.audit_snapshot('initial-audit')
        self.global_process, self.global_log = self.command(self.config['globalMixedWorkload'], 'global-mixed-workload', True)
        process = subprocess.Popen([str(pinned(self.config['node'])), str(BROWSER), '--config', self.config['configPath']], cwd=ROOT, env=dict(os.environ, L38_ARTIFACT_DIR=str(self.output)), stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=(self.output / 'browser-stderr.log').open('xb'), text=False, bufsize=0, start_new_session=True)
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ)
        os.set_blocking(process.stdout.fileno(), False)
        buffered = bytearray()
        def consume(data):
            buffered.extend(data)
            require(len(buffered) <= 2097152, 'bounded observation buffer required')
            while b'\n' in buffered:
                line, _, remainder = buffered.partition(b'\n')
                buffered[:] = remainder
                require(len(line) <= 1048576, 'bounded observation required')
                event = json.loads(line)
                self.record(event)
                if event.get('kind') == 'control':
                    observed = self.control(event)
                    process.stdin.write((json.dumps({'stage': event['stage'], 'continue': True, 'value': observed}) + '\n').encode())
                    process.stdin.flush()
                if event.get('kind') == 'scenario':
                    identifier = str(event['scenario'])
                    require(identifier in SCENARIOS and identifier not in self.scenarios, 'duplicate or unregistered scenario')
                    require(event.get('passed') is True and event.get('observations', 0) > 0, 'scenario lacks real observations')
                    self.scenarios[identifier] = event
        try:
            while process.poll() is None:
                require(time.monotonic() < self.deadline, 'measurement deadline exceeded')
                require(self.global_process.poll() is None, 'mixed workload stopped before safety drill completed')
                for key, _ in selector.select(timeout=1):
                    consume(os.read(key.fileobj.fileno(), 65536))
            while True:
                try:
                    remaining = os.read(process.stdout.fileno(), 65536)
                except BlockingIOError:
                    break
                if not remaining:
                    break
                consume(remaining)
            require(not buffered, 'truncated final observation fails measurement')
            require(process.returncode == 0, 'browser/service measurement failed')
            require(set(self.scenarios) == SCENARIOS, 'missing scenario result fails voice release')
            after_audit = self.audit_snapshot('final-audit')
            for row in after_audit:
                require(re.fullmatch(r'0x[0-9a-f]{64}', row['evidenceHash']), 'audit evidence hash invalid')
                values = [row['id'],row['caseId'],row['actor'],row['sessionId'],row['role'],row['scope'],row['kind'],row['reason'],row['policyVersion'],row['revision']]
                serialized = json.dumps(values, ensure_ascii=False, separators=(',', ':')).replace('&', '\\u0026').replace('<', '\\u003c').replace('>', '\\u003e').replace('\u2028', '\\u2028').replace('\u2029', '\\u2029')
                require('0x' + hashlib.sha256(serialized.encode()).hexdigest() == row['evidenceHash'], 'immutable action audit digest disagrees with real recorded actor/scope/reason')
            baseline = {row['id']: row for row in before_audit}
            final = {row['id']: row for row in after_audit}
            require(all(final.get(key) == value for key, value in baseline.items()), 'immutable audit history changed')
            counts = self.sql('final-provider-database-reconciliation', "SELECT json_build_object('liveRooms',(SELECT count(*) FROM voice_rooms WHERE state='live'),'endingRooms',(SELECT count(*) FROM voice_rooms WHERE state='ending'),'pendingRevocations',(SELECT count(*) FROM voice_participants WHERE grant_state IN ('granting','revoking')),'pendingSafetyOutbox',(SELECT count(*) FROM safety_outbox WHERE published_at IS NULL))")
            require(counts['endingRooms'] == 0 and counts['pendingRevocations'] == 0, 'voice cleanup reconciliation incomplete')
            os.killpg(self.global_process.pid, signal.SIGINT)
            require(self.global_process.wait(timeout=10) == 0, 'global workload did not finalize complete raw observations')
            self.global_log.close()
            mixed_samples = []
            identities = set()
            with (self.output / 'global-mixed-workload.log').open() as mixed:
                for line in mixed:
                    require(len(line) <= 1048576, 'bounded raw mixed observation required')
                    sample = json.loads(line)
                    require(sample['kind'] == 'request' and sample['sourceSnapshot'] == self.config['sourceSnapshot'] and sample['buildDigest'] == self.config['buildDigest'] and sample['environmentId'] == self.config['environmentId'], 'real mixed request/source binding missing')
                    require(sample['requestId'] not in identities and isinstance(sample['observedAtUnixNs'], int) and self.started_unix_ns <= sample['observedAtUnixNs'] <= time.time_ns(), 'duplicate or unrelated mixed observation')
                    identities.add(sample['requestId'])
                    require(isinstance(sample['status'], int) and 100 <= sample['status'] <= 599 and sample['admitted'] == (200 <= sample['status'] < 300), 'mixed request acceptance cannot include denied writes')
                    require(sample['phase'] in ('normal', 'safety_dependency_loss') and math.isfinite(sample['latencyMs']) and sample['latencyMs'] >= 0, 'actual mixed latency/phase missing')
                    require(set(sample) <= {'kind','sourceSnapshot','buildDigest','environmentId','requestId','observedAtUnixNs','status','admitted','phase','latencyMs','method','domain'}, 'mixed raw capture must exclude credentials and private bodies')
                    if sample['phase'] == 'safety_dependency_loss':
                        require(self.outage_interval['begin'] <= sample['observedAtUnixNs'] <= self.outage_interval['end'], 'mixed loss-phase observation occurred outside actual fault interval')
                    mixed_samples.append(sample)
            require(mixed_samples and {sample['phase'] for sample in mixed_samples} == {'normal','safety_dependency_loss'}, 'mixed workload lacked actual dependency-loss overlap')
            mixed_histogram = histogram([sample['latencyMs'] for sample in mixed_samples])
            require(source_snapshot() == self.config['sourceSnapshot'], 'source changed during measurement')
            latency = [event['latencyMs'] for event in self.events if event.get('kind') == 'request']
            summary = {'schemaVersion': 4, 'lane': 'L38', 'task': '4.38', 'sourceSnapshot': self.config['sourceSnapshot'], 'buildDigest': self.config['buildDigest'], 'environmentId': self.config['environmentId'], 'databaseSchemaVersion': self.config['databaseSchemaVersion'], 'seed': self.config['seed'], 'scenarioResults': self.scenarios, 'latencyHistogram': histogram(latency), 'mixedLatencyHistogram': mixed_histogram, 'mixedRawSha256': digest(self.output / 'global-mixed-workload.log'), 'mixedRequests': len(mixed_samples), 'mixedAdmitted': sum(sample['admitted'] for sample in mixed_samples), 'databaseReconciliation': counts, 'rawSha256': digest(self.raw.name), 'lane_measurement_passed': True, 'production_qualified': False, 'shared_load_restore_acceptance': 'deferred_to_serial_9.1'}
            self.summary = summary
        finally:
            selector.close()
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=5)
            for fault in list(self.active_faults):
                self.command(self.config['faults'][fault]['end'], fault + '-emergency-restore')
            if self.global_process.poll() is None:
                os.killpg(self.global_process.pid, signal.SIGINT)
                self.global_process.wait(timeout=10)
            if not self.global_log.closed:
                self.global_log.close()
            self.command(self.config['cleanup'], 'owned-environment-cleanup')
            self.raw.close()
        require(source_snapshot() == self.config['sourceSnapshot'], 'source changed during operational cleanup')
        self.summary['rawSha256'] = digest(self.output / 'observations.ndjson')
        (self.output / 'result.json').write_text(json.dumps(self.summary, indent=2, allow_nan=False) + '\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    config_path = Path(args.config).resolve(strict=True)
    require(digest(config_path) == os.environ.get('L38_REVIEWED_MANIFEST_SHA256'), 'independently pinned reviewed measurement manifest required')
    config = json.loads(config_path.read_text())
    require(config['schemaVersion'] == 4 and config['isolatedTestEnvironment'] is True, 'isolated actual test environment required')
    require(config['sourceSnapshot'] == source_snapshot(), 'current candidate source binding failed')
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', config['buildDigest']) is not None, 'real immutable build digest required')
    require(re.fullmatch(r'[0-9a-f-]{36}', config['runId']) is not None, 'isolated exact run UUID required')
    require(config['databaseSchemaVersion'] and config['environmentId'] and isinstance(config['seed'], int), 'schema/environment/seed binding required')
    require(60 <= config['durationSeconds'] <= 1800, 'bounded lane measurement duration required')
    require(len(config['replicas']) == 4 and len({r['origin'] for r in config['replicas']}) == 4, 'exactly four actual replicas required')
    require(360 <= config['orphanHoldSeconds'] <= 600, 'real elapsed orphan alarm drill must exceed five minutes plus reconciliation interval')
    require(60 <= config['voiceHoldSeconds'] <= 600, 'actual simultaneous voice occupancy hold required')
    require(len(config['rooms']) == 100 and all(len(room['actors']) == 50 and len(set(room['actors'])) == 50 for room in config['rooms']), '100 actual rooms with 50 participants required')
    require(config['budget']['dau'] == 500000 and config['budget']['globalWritesPerSecond'] == 2000 and config['budget']['globalSockets'] == 200000 and config['budget']['voiceParticipants'] == 5000 and config['budget']['maxPublishersPerRoom'] == 10, 'frozen global and voice budgets required')
    require(config['budget']['socketAllocations']['voice'] == 5000 and config['budget']['videoEgressBitsPerSecond'] == 40000000000, 'voice must stay inside global socket allocation and separate video bandwidth model')
    require(sum(config['budget']['writeAllocations'].values()) == 2000 and sum(config['budget']['socketAllocations'].values()) == 200000, 'domain allocations must sum to frozen budgets')
    for name in ('provider', 'acl', 'retention', 'moderator', 'consent'):
        receipt = json.loads(pinned(config['releaseEvidence'][name]).read_text())
        require(receipt['sourceSnapshot'] == config['sourceSnapshot'] and receipt['buildDigest'] == config['buildDigest'] and receipt['environmentId'] == config['environmentId'], 'release evidence binding mismatch: ' + name)
        require(receipt['exitCode'] == 0 and receipt['provider'] == 'LiveKit Cloud' and receipt['rawArtifact']['sha256'] == digest(pinned(receipt['rawArtifact'])), 'real provider/release evidence missing: ' + name)
    for replica in config['replicas']:
        receipt = json.loads(pinned(replica['deploymentReceipt']).read_text())
        require(receipt['origin'] == replica['origin'] and receipt['buildDigest'] == config['buildDigest'] and receipt['sourceSnapshot'] == config['sourceSnapshot'], 'actual replica deployment receipt mismatch')
    for fault in FAULTS:
        require(fault in config['faults'], 'actual approved fault controller required: ' + fault)
        for boundary in ('begin', 'end'):
            operation = config['faults'][fault][boundary]
            pinned(operation['executable'])
            require(1 <= operation['timeoutSeconds'] <= 60 and operation['environmentId'] == config['environmentId'], 'bounded environment-bound operation required')
    for key in ('globalMixedWorkload', 'cleanup'):
        pinned(config[key]['executable'])
    for key in ('node', 'psql', 'playwrightModule', 'websocketModule', 'livekitBrowserBundle', 'axeScript', 'expectedSchema'):
        pinned(config[key])
    require(os.access(pinned(config['node']), os.X_OK) and os.access(pinned(config['psql']), os.X_OK), 'actual executable runtimes required')
    require(os.environ.get(config['provider']['apiKeyEnv']) and os.environ.get(config['provider']['apiSecretEnv']), 'actual supported provider observer credentials required')
    for account in config['actors']:
        pinned(account['storageState'])
        require(os.environ.get(account['csrfTokenEnv']), 'actual authenticated session CSRF missing')
    require(len({journey['name'] for journey in config['accessibilityJourneys']}) == len(config['accessibilityJourneys']) and all(journey['steps'] for journey in config['accessibilityJourneys']), 'actual distinct executable user journeys required')
    pinned(config['visibilityFixture']['originalEnvelope'])
    config['configPath'] = str(config_path)
    output = Path(args.output).resolve()
    require(not output.is_relative_to(ROOT), 'raw measurement artifacts must be outside source checkout')
    Runner(config, output).run()
    print(json.dumps({'exit_code': 0, 'log_path': str(output / 'result.json')}))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'exit_code': 1, 'error': str(error)}), file=sys.stderr)
        sys.exit(1)
