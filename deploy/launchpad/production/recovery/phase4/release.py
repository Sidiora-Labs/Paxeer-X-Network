from __future__ import annotations

import argparse
import asyncio
from dataclasses import asdict
from decimal import Decimal, InvalidOperation
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import time
from urllib.parse import urlencode

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'phase2'))
sys.path.insert(0, str(ROOT / 'tools/production/load'))
from providers import BoundTools, PostgreSQL, ProviderFailure, Unavailable, file_sha, regular
from render import RecoveryIdentity
from phase2_types import Bindings
from phase2_http import HTTP
from capture import read_manifest, iter_artifact, gateway_summary
import recovery


def bound(binding, *, private=True):
    recovery.exact(binding, {'path', 'sha256'})
    recovery.digest(binding['sha256'])
    path = regular(binding['path'], recovery.MAX_JSON, private=private)
    raw = path.read_bytes()
    recovery.require(hashlib.sha256(raw).hexdigest() == binding['sha256'], 'changed release input')
    return recovery.load(raw)


def current_source(expected):
    spec = importlib.util.spec_from_file_location('recovery_release_source_binding', ROOT / 'tools/production/qualify.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    recovery.require(module.snapshot(ROOT)['sha256'] == expected, 'recovery release candidate source changed')


def quantity(value, *, cpu=False):
    recovery.require(isinstance(value, str) and bool(re.fullmatch(r'[0-9]+(?:\.[0-9]+)?(?:[EPTGMK]i|[EPTGMk]|m|u|n)?', value)), 'native resource quantity absent')
    match = re.fullmatch(r'([0-9]+(?:\.[0-9]+)?)(.*)', value)
    scales = {'': 1, 'm': Decimal('0.001'), 'u': Decimal('0.000001'), 'n': Decimal('0.000000001'),
              **{suffix: 1000**power for power, suffix in enumerate(('k','M','G','T','P','E'), 1)},
              **{suffix: 1024**power for power, suffix in enumerate(('Ki','Mi','Gi','Ti','Pi','Ei'), 1)}}
    result = Decimal(match[1]) * scales[match[2]]
    recovery.require(result.is_finite() and result > 0 and (cpu or result == result.to_integral_value()), 'invalid actual resource quantity')
    return str(result) if cpu else int(result)


def allocation_rows(pods, volumes, *, pod_uids, volume_uids, expected_images):
    recovery.require(isinstance(pods, dict) and isinstance(volumes, dict), 'native Kubernetes allocations absent')
    actual_pods = {row['metadata']['uid']: row for row in pods.get('items', [])}
    actual_volumes = {row['metadata']['uid']: row for row in volumes.get('items', [])}
    recovery.require(len(actual_pods) == len(pods.get('items', [])) and len(actual_volumes) == len(volumes.get('items', [])), 'duplicate resource identity')
    recovery.require(set(actual_pods) == set(pod_uids) and set(actual_volumes) == set(volume_uids), 'actual resource inventory changed')
    recovery.require(isinstance(expected_images, dict) and set(expected_images) == set(pod_uids), 'admitted recovery image inventory absent')
    allocations = []
    for uid, pod in actual_pods.items():
        recovery.require(pod['status'].get('phase') == 'Running' and pod['spec'].get('nodeName'), 'recovery pod not scheduled and running')
        status = {row['name']: row for row in pod['status'].get('containerStatuses', [])}
        recovery.require(set(expected_images[uid]) == {row['name'] for row in pod['spec'].get('containers', [])}, 'admitted container inventory changed')
        for container in pod['spec'].get('containers', []):
            name = container['name']; state = status.get(name, {})
            recovery.require(state.get('ready') is True and 'running' in state.get('state', {}) and
                             re.search(r'@sha256:[0-9a-f]{64}$', state.get('imageID', '')), 'ready immutable recovery container required')
            recovery.require(state['imageID'] == expected_images[uid][name], 'actual recovery image differs from admitted deployment')
            recovery.integer(state.get('restartCount'), 0)
            recovery.require(state.get('lastState', {}).get('terminated', {}).get('reason') != 'OOMKilled', 'recovery container OOM history present')
            resources = container.get('resources', {})
            limits, requests = resources.get('limits', {}), resources.get('requests', {})
            cpu_request = Decimal(quantity(requests.get('cpu'), cpu=True)); cpu_limit = Decimal(quantity(limits.get('cpu'), cpu=True))
            memory_request = quantity(requests.get('memory')); memory_limit = quantity(limits.get('memory'))
            recovery.require(cpu_request <= cpu_limit and memory_request <= memory_limit, 'resource request exceeds allocation')
            allocations.append(dict(pod_uid=uid, node=pod['spec']['nodeName'], container=name, image_id=state['imageID'],
                                    cpu_request_cores=str(cpu_request), cpu_limit_cores=str(cpu_limit),
                                    memory_request_bytes=memory_request, memory_limit_bytes=memory_limit,
                                    restart_count=state['restartCount']))
    recovery.require(bool(allocations), 'empty actual recovery allocation')
    storage = []
    for uid, volume in actual_volumes.items():
        recovery.require(volume['status'].get('phase') == 'Bound' and volume['spec'].get('volumeName'), 'recovery storage not bound')
        requested = quantity(volume['spec'].get('resources', {}).get('requests', {}).get('storage'))
        allocated = quantity(volume['status'].get('capacity', {}).get('storage'))
        recovery.require(allocated >= requested, 'storage allocation below request')
        storage.append(dict(uid=uid, volume_name=volume['spec']['volumeName'], requested_bytes=requested, allocated_bytes=allocated))
    recovery.require(bool(storage), 'actual recovery storage absent')
    return {'containers': allocations, 'volumes': storage}


def signed_scenario(binding, key_binding, *, identity, bindings, run_id, requirement, started_ns, ended_ns, tool_sha256):
    signed = bound(binding)
    recovery.exact(signed, {'payload', 'hmac_sha256'})
    recovery.exact(key_binding, {'path','sha256'}); recovery.digest(key_binding['sha256'])
    key = regular(key_binding['path'], 4096, private=True).read_bytes()
    recovery.require(hashlib.sha256(key).hexdigest() == key_binding['sha256'], 'independent attestation key identity changed')
    recovery.require(32 <= len(key) <= 4096, 'independent recovery attestation key required')
    import hmac
    signature = hmac.new(key, recovery.canonical(signed['payload']), hashlib.sha256).hexdigest()
    recovery.require(isinstance(signed['hmac_sha256'], str) and hmac.compare_digest(signature, signed['hmac_sha256']), 'untrusted release scenario')
    payload = signed['payload']
    recovery.exact(payload, {'version', 'realm', 'identity', 'bindings', 'run_id', 'requirement', 'started_utc_ns', 'ended_utc_ns',
                            'fault', 'native', 'expected', 'actual', 'inputs_sha256', 'limitations'})
    recovery.require(type(payload['version']) is int and payload['version'] == 4 and payload['realm'] == 'isolated-recovery-qualification' and
                     payload['identity'] == identity and payload['bindings'] == bindings and payload['run_id'] == run_id and
                     payload['requirement'] == requirement and payload['limitations'] == [], 'release scenario identity, realm or limitations differ')
    recovery.integer(payload['started_utc_ns'], started_ns, ended_ns)
    recovery.integer(payload['ended_utc_ns'], payload['started_utc_ns'] + 1, ended_ns)
    recovery.digest(payload['inputs_sha256']); recovery.require(payload['inputs_sha256'] != '0'*64, 'scenario inputs absent')
    fault = payload['fault']
    recovery.exact(fault, {'fault_id', 'started_utc_ns', 'ended_utc_ns', 'native_control_sha256'})
    recovery.require(isinstance(fault['fault_id'], str) and 1 <= len(fault['fault_id']) <= 256, 'fault identity absent')
    recovery.integer(fault['started_utc_ns'], payload['started_utc_ns'], payload['ended_utc_ns'])
    recovery.integer(fault['ended_utc_ns'], fault['started_utc_ns'], payload['ended_utc_ns'])
    recovery.digest(fault['native_control_sha256'])
    native = bound(payload['native'])
    recovery.exact(native, {'version', 'realm', 'requirement', 'identity', 'inputs_sha256', 'started_unix', 'ended_unix', 'tool_sha256', 'exit_code', 'result'})
    recovery.require(native['version'] == 3 and type(native['version']) is int and native['realm'] == payload['realm'] and
                     native['identity'] == identity and native['requirement'] == requirement and native['inputs_sha256'] == payload['inputs_sha256'],
                     'native recovery result identity differs')
    recovery.integer(native['exit_code'], 0, 0); recovery.digest(native['tool_sha256'])
    recovery.digest(tool_sha256); recovery.require(native['tool_sha256'] == tool_sha256 and tool_sha256 != '0'*64, 'native executor is not the admitted binary')
    recovery.require(native['started_unix']*10**9 <= fault['started_utc_ns'] and
                     (native['ended_unix']+1)*10**9 >= fault['ended_utc_ns'], 'native receipt does not bound injected fault')
    recovery.adversarial_scenario(requirement, native['result'])
    from render import load_operator
    load_operator().reconcile_v2(payload['expected'], payload['actual'])
    checkpoint = payload['actual']['checkpoint']
    recovery.require(all(checkpoint[field] == identity[field] for field in ('source_sha256', 'interface_sha256', 'deployment_sha256')),
                     'authoritative reconciliation candidate differs')
    recovery.require(native['tool_sha256'] != '0'*64 and fault['native_control_sha256'] != '0'*64, 'native provider authority absent')
    return {'fault': fault, 'native_sha256': payload['native']['sha256'], 'inputs_sha256': payload['inputs_sha256'],
            'reconciliation_sha256': recovery.sha(payload['actual']), 'native_result': native['result']}


class Journal:
    def __init__(self, directory):
        self.root = Path(directory)
        recovery.require(self.root.is_absolute() and self.root.parent.resolve(strict=True) == self.root.parent and not self.root.exists(), 'fresh owned release output directory required')
        self.root.mkdir(mode=0o700)
        self.artifacts = {}

    def write(self, name, value):
        recovery.require(bool(re.fullmatch(r'[a-z0-9-]+\.(json|ndjson)', name)), 'unsafe release artifact name')
        raw = recovery.canonical(value) + b'\n'
        fd = os.open(self.root / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as output:
            output.write(raw); output.flush(); os.fsync(output.fileno())
        self.artifacts[name] = hashlib.sha256(raw).hexdigest()
        return {'path': str(self.root / name), 'sha256': self.artifacts[name]}


class Collector:
    def __init__(self, config, journal):
        recovery.exact(config, {'version', 'realm', 'identity', 'bindings', 'run_id', 'tools', 'telemetry', 'allocation', 'postgres', 'dataset', 'duration_seconds', 'step_seconds'})
        recovery.require(type(config['version']) is int and config['version'] == 4 and config['realm'] == 'isolated-recovery-qualification', 'isolated version4 collection required')
        self.identity = RecoveryIdentity(**config['identity']).validate().diagnostic()
        recovery.require(self.identity['environment'] == 'qualification', 'production collection forbidden')
        self.bindings = asdict(Bindings(**config['bindings']))
        recovery.require(self.bindings['candidate'] == self.identity['source_sha256'] and self.bindings['interface'] == self.identity['interface_sha256'], 'recovery collector candidate differs')
        current_source(self.bindings['candidate'])
        dataset = bound(config['dataset'])
        recovery.exact(dataset, {'version', 'realm', 'identity', 'classification', 'review_sha256', 'dataset_sha256', 'event_hours'})
        recovery.require(type(dataset['version']) is int and dataset['version'] == 4 and dataset['realm'] == config['realm'] and
                         dataset['identity'] == self.identity and dataset['classification'] in {'synthetic', 'reviewed-anonymized'}, 'unreviewed or foreign recovery dataset')
        recovery.digest(dataset['review_sha256']); recovery.digest(dataset['dataset_sha256'])
        recovery.require(dataset['review_sha256'] != '0'*64 and dataset['dataset_sha256'] != '0'*64, 'dataset review authority absent')
        recovery.integer(dataset['event_hours'], 72, 8760)
        recovery.require(isinstance(config['run_id'], str) and 1 <= len(config['run_id']) <= 256, 'D16 run identity required')
        recovery.integer(config['duration_seconds'], 60, 900); recovery.integer(config['step_seconds'], 1, 60)
        recovery.require(config['duration_seconds'] % config['step_seconds'] == 0, 'exact bounded capture schedule required')
        telemetry = config['telemetry']
        recovery.exact(telemetry, {'origin', 'application_origin', 'owned_local', 'authority'})
        recovery.require(type(telemetry['owned_local']) is bool, 'explicit telemetry transport realm required')
        self.http = HTTP(telemetry['origin'], telemetry['application_origin'], owned_local=telemetry['owned_local'])
        authority = bound(telemetry['authority'])
        recovery.exact(authority, {'version', 'identity', 'bindings', 'metrics'})
        recovery.require(type(authority['version']) is int and authority['version'] == 4 and authority['identity'] == self.identity and authority['bindings'] == self.bindings, 'native recovery metric authority differs')
        recovery.exact(authority['metrics'], recovery.METRICS)
        for metric in authority['metrics'].values():
            recovery.exact(metric, {'name', 'zone'})
            recovery.require(isinstance(metric['name'], str) and re.fullmatch(r'[A-Za-z_:][A-Za-z0-9_:]{0,127}', metric['name']) and
                             metric['zone'] in {self.identity['primary_region'], self.identity['recovery_region']}, 'exact native metric selector required')
        self.metrics = authority['metrics']; self.authority_sha = telemetry['authority']['sha256']
        self.tools, self.journal, self.config = BoundTools(config['tools']), journal, config
        self.database = PostgreSQL(self.tools, **config['postgres'])

    def row(self, kind, payload):
        return dict(version=4, lane='L44', run_id=self.config['run_id'], bindings=self.bindings,
                    identity=self.identity, kind=kind, observed_utc_ns=time.time_ns(), payload=payload)

    def allocations(self):
        config = self.config['allocation']
        recovery.exact(config, {'context', 'namespace', 'namespace_uid', 'kubeconfig', 'kubeconfig_sha256', 'pod_uids', 'volume_uids', 'expected_images'})
        for name in ('context', 'namespace'):
            recovery.require(isinstance(config[name], str) and re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,127}', config[name]), 'admitted Kubernetes identity required')
        recovery.digest(config['kubeconfig_sha256'])
        recovery.require(file_sha(regular(config['kubeconfig'], 65536, private=True)) == config['kubeconfig_sha256'], 'Kubernetes authority changed')
        env = {'KUBECONFIG': config['kubeconfig']}
        args = ['--context', config['context'], '--namespace', config['namespace'], '--request-timeout=20s', 'get']
        namespace = recovery.load(self.tools.run('kubectl', [*args, 'namespace', config['namespace'], '-o', 'json'], env=env, timeout=30))
        recovery.require(namespace['metadata']['uid'] == config['namespace_uid'] and namespace['metadata'].get('labels',{}).get('kindle.io/environment') == 'qualification', 'actual Kubernetes realm differs')
        pods_raw = self.tools.run('kubectl', [*args, 'pods', '-o', 'json'], env=env, timeout=30)
        volumes_raw = self.tools.run('kubectl', [*args, 'pvc', '-o', 'json'], env=env, timeout=30)
        result = allocation_rows(recovery.load(pods_raw), recovery.load(volumes_raw), pod_uids=config['pod_uids'], volume_uids=config['volume_uids'], expected_images=config['expected_images'])
        result['native_pods_sha256'] = hashlib.sha256(pods_raw).hexdigest()
        result['native_volumes_sha256'] = hashlib.sha256(volumes_raw).hexdigest()
        return self.row('allocations', result)

    async def snapshot(self):
        async def query(requirement, metric):
            labels = {'service': self.identity['service'], 'environment': self.identity['environment'], 'zone': metric['zone']}
            expression = metric['name'] + '{' + ','.join(key+'="'+value+'"' for key,value in sorted(labels.items())) + '}'
            response = await self.http.call('GET', '/api/v1/query?' + urlencode({'query': expression}))
            for sample in response.get('data', {}).get('result', []):
                recovery.require(sample.get('metric', {}).get('__name__') == metric['name'] and
                                 all(sample.get('metric', {}).get(key) == value for key,value in labels.items()), 'native exporter returned a foreign selector')
            return requirement, response
        values = await asyncio.gather(*(query(rid, metric) for rid, metric in self.metrics.items()))
        return self.row('metrics', {'authority_sha256': self.authority_sha, 'queries': dict(values)})

    async def collect(self):
        start = time.time_ns(); schedule = time.monotonic_ns(); rows = []
        self.journal.write('allocation-before.json', self.allocations())
        reconciliation_sql = (Path(__file__).resolve().parents[1] / 'phase2/reconciliation.sql').read_text()
        self.journal.write('checkpoint-before.json', self.row('checkpoint', self.database.document(reconciliation_sql)))
        rows.append(await self.snapshot())
        fd = os.open(self.journal.root / 'recovery-metrics.ndjson', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as output:
            output.write(recovery.canonical(rows[-1]) + b'\n'); output.flush()
            for tick in range(1, self.config['duration_seconds']//self.config['step_seconds'] + 1):
                await asyncio.sleep(max(0, (schedule + tick*self.config['step_seconds']*10**9 - time.monotonic_ns())/10**9))
                rows.append(await self.snapshot()); output.write(recovery.canonical(rows[-1]) + b'\n'); output.flush()
            os.fsync(output.fileno())
        end = time.time_ns()
        self.journal.artifacts['recovery-metrics.ndjson'] = file_sha(self.journal.root / 'recovery-metrics.ndjson')
        self.journal.write('allocation-after.json', self.allocations())
        self.journal.write('checkpoint-after.json', self.row('checkpoint', self.database.document(reconciliation_sql)))
        samples = recovery.release_timeline(rows, identity=self.identity, run_id=self.config['run_id'], bindings=self.bindings,
                                            started_ns=start, ended_ns=end, step_seconds=self.config['step_seconds'], authority_sha256=self.authority_sha)
        return dict(version=4, task='4.44', lane='L44', identity=self.identity, bindings=self.bindings, run_id=self.config['run_id'],
                    mode='read-only-component-measurement', started_utc_ns=start, ended_utc_ns=end,
                    samples={rid: len(values) for rid, values in samples.items()}, artifacts=dict(self.journal.artifacts),
                    results=[dict(requirement=rid, criteria=[rid+'.'+str(i) for i in range(1,6)], outcome='observed', samples=len(values),
                                  final_acceptance=False, limitation='shared_restore_acceptance_deferred_to_9.1') for rid,values in samples.items()],
                    outcome='pass', capacity_qualified=False, monthly_availability_qualified=False, final_shared_gate='9.1')


def evaluate(config, journal):
    recovery.exact(config, {'version', 'realm', 'identity', 'bindings', 'capture', 'authority_sha256', 'step_seconds', 'scenarios', 'native_tool_sha256', 'attestation_key', 'budget', 'scenarios_windows'})
    recovery.require(type(config['version']) is int and config['version'] == 4 and config['realm'] == 'isolated-recovery-qualification', 'isolated final release evaluation required')
    identity = RecoveryIdentity(**config['identity']).validate().diagnostic()
    recovery.require(identity['environment'] == 'qualification', 'production release rehearsal forbidden')
    bindings = Bindings(**config['bindings'])
    current_source(bindings.candidate)
    shared = read_manifest(config['capture']['path'], config['capture']['sha256'], bindings=bindings)
    recovery.require(shared['scope'] == 'final_shared' and 'L44' in shared['required_lane_ids'], 'component capture cannot qualify recovery release')
    start, end = int(shared['started_utc_ns']), int(shared['ended_utc_ns'])
    recovery.require(end-start >= 86400*10**9 and 0 <= time.time_ns()-end <= 1800*10**9, 'shortened, future or stale final soak')
    recovery.require(bindings.candidate == identity['source_sha256'] and bindings.interface == identity['interface_sha256'], 'release source and interface differ')
    recovery.exact(config['scenarios'], recovery.METRICS)
    recovery.exact(config['native_tool_sha256'], recovery.METRICS)
    recovery.exact(config['scenarios_windows'], {'connection-surge', 'fanout-burst', 'event-surge'})
    for scenario, minimum in (('connection-surge',600), ('fanout-burst',600), ('event-surge',300)):
        window = config['scenarios_windows'][scenario]
        recovery.exact(window, {'started_utc_ns','ended_utc_ns'})
        recovery.integer(window['started_utc_ns'], start, end); recovery.integer(window['ended_utc_ns'], window['started_utc_ns']+minimum*10**9, end)
    gateway = gateway_summary(shared)
    event_counts = {}
    event_window = config['scenarios_windows']['event-surge']
    for interval in iter_artifact(shared, 'gateway_egress', record='GatewayInterval'):
        instant = int(interval['interval_start_utc_ns'])
        if event_window['started_utc_ns'] <= instant < event_window['ended_utc_ns']:
            second = (instant-event_window['started_utc_ns'])//10**9
            event_counts[second] = event_counts.get(second, 0) + interval['source_accepted_events']
    recovery.require(len(event_counts) >= 300 and all(count >= 10000 for count in event_counts.values()), 'shared canonical event surge not measured')
    for name, window in config['scenarios_windows'].items():
        left = (window['started_utc_ns']-start+10**9-1)//10**9
        right = (window['ended_utc_ns']-start)//10**9
        observed = [gateway['seconds'].get(str(second)) for second in range(left, right)]
        recovery.require(len(observed) >= (300 if name == 'event-surge' else 600) and all(observed), 'burst native gateway capture incomplete')
        if name == 'connection-surge':
            recovery.require(all(item['connections'] >= 200000 and item['authenticated'] == item['public'] and
                                 item['subscriptions'] == item['connections']*5 for item in observed), 'shared connection surge not measured')
        elif name == 'fanout-burst':
            recovery.require(all(item['frames'] >= 1000000 and item['payload_bytes'] >= 400000000 and
                                 item['wire_bytes'] >= item['payload_bytes'] for item in observed), 'shared fanout burst not measured')
    metric_rows, allocations = [], []
    for row in iter_artifact(shared, 'native_provider'):
        if row.get('lane') == 'L44' and row.get('kind') == 'metrics': metric_rows.append(row)
        if row.get('lane') == 'L44' and row.get('kind') == 'allocations':
            recovery.require(row['identity'] == identity and row['bindings'] == asdict(bindings) and row['run_id'] == shared['run_id'], 'foreign recovery allocation')
            recovery.integer(row['observed_utc_ns'], start, end)
            values = row['payload']
            recovery.require(isinstance(values.get('containers'), list) and values['containers'] and
                             isinstance(values.get('volumes'), list) and values['volumes'], 'missing actual recovery allocation')
            recovery.digest(values['native_pods_sha256']); recovery.digest(values['native_volumes_sha256'])
            for container in values['containers']:
                recovery.require(Decimal(container['cpu_request_cores']) > 0 and Decimal(container['cpu_limit_cores']) >= Decimal(container['cpu_request_cores']) and
                                 0 < container['memory_request_bytes'] <= container['memory_limit_bytes'] and
                                 bool(re.search(r'@sha256:[0-9a-f]{64}$',container['image_id'])), 'invalid measured container budget')
            for volume in values['volumes']:
                recovery.require(0 < volume['requested_bytes'] <= volume['allocated_bytes'], 'invalid measured storage allocation')
            allocations.append(row)
    recovery.require(len(allocations) >= 2 and min(row['observed_utc_ns'] for row in allocations) - start <= 60*10**9 and
                     end-max(row['observed_utc_ns'] for row in allocations) <= 60*10**9, 'allocation captures do not bound final soak')
    metrics = recovery.release_timeline(metric_rows, identity=identity, run_id=shared['run_id'], bindings=asdict(bindings),
                                        started_ns=start, ended_ns=end, step_seconds=config['step_seconds'], authority_sha256=config['authority_sha256'])
    checks = recovery.release_metric_checks(metrics, ended_ns=end)
    faults = {}
    for row in iter_artifact(shared, 'fault_control', record='FaultInterval'):
        recovery.require(row['fault_id'] not in faults, 'duplicate native fault identity')
        recovery.require(row['run_id'] == shared['run_id'] and start <= int(row['observed_start_utc_ns']) <=
                         int(row['observed_end_utc_ns']) <= int(row['recovery_utc_ns']) <= end, 'fault outside actual shared capture')
        faults[row['fault_id']] = row
    budget = bound(config['budget'])
    recovery.require(any(artifact['kind'] == 'cost_budget' and artifact['sha256'] == config['budget']['sha256'] for artifact in shared['artifacts']), 'operating budget not part of immutable shared capture')
    from workload import provisioned_budget
    budget_result = provisioned_budget(budget)
    recovery.require(budget_result['final_run_permitted'], 'actual storage/network/cost budget failed')
    results = []
    for rid in recovery.METRICS:
        try:
            scenario = signed_scenario(config['scenarios'][rid], config['attestation_key'], identity=identity, bindings=asdict(bindings),
                                       run_id=shared['run_id'], requirement=rid, started_ns=start, ended_ns=end, tool_sha256=config['native_tool_sha256'][rid])
            fault = faults.get(scenario['fault']['fault_id'])
            recovery.require(fault is not None and fault['run_id'] == shared['run_id'] and fault['outcome'] == 'recovered' and
                             fault['native_control_sha256'] == scenario['fault']['native_control_sha256'] and
                             int(fault['observed_start_utc_ns']) == scenario['fault']['started_utc_ns'] and
                             int(fault['observed_end_utc_ns']) == scenario['fault']['ended_utc_ns'], 'recovery fault differs from actual D16 control interval')
            if rid == 'D14_032':
                recovery.require(int(fault['recovery_utc_ns']) - int(fault['observed_start_utc_ns']) <= 1800*10**9 and
                                 abs(scenario['native_result']['loss_unix']*10**9-int(fault['observed_start_utc_ns'])) < 10**9,
                                 'actual regional recovery interval exceeds RTO or checkpoint differs')
            recovery.require(checks[rid]['passed'], 'owned recovery SLO failed')
            recovery.require(any(row['observed_utc_ns'] <= scenario['fault']['started_utc_ns'] for row in allocations) and
                             any(row['observed_utc_ns'] >= scenario['fault']['ended_utc_ns'] for row in allocations), 'allocation telemetry does not bound recovery fault')
            results.append(dict(requirement=rid, criteria=[rid+'.'+str(i) for i in range(1,6)], outcome='pass',
                                metric=checks[rid], reconciliation_sha256=scenario['reconciliation_sha256'], native_sha256=scenario['native_sha256']))
        except (ValueError, OSError, KeyError, TypeError) as exc:
            results.append(dict(requirement=rid, criteria=[rid+'.'+str(i) for i in range(1,6)], outcome='fail', limitation=type(exc).__name__))
    return dict(version=4, task='4.44', lane='L44', identity=identity, bindings=asdict(bindings), run_id=shared['run_id'],
                capture_sha256=config['capture']['sha256'], started_utc_ns=start, ended_utc_ns=end, results=results,
                budget=budget_result, actual_allocations=allocations, gateway_totals=gateway['totals'], outcome='pass' if all(row['outcome']=='pass' for row in results) else 'fail',
                capacity_qualified=False, monthly_availability_qualified=False, final_shared_gate='9.1', artifacts=dict(journal.artifacts))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--mode', choices=('collect', 'evaluate'), required=True)
    parser.add_argument('--configuration', required=True)
    parser.add_argument('--configuration-sha256', required=True)
    parser.add_argument('--output-directory', required=True)
    args = parser.parse_args()
    journal = Journal(args.output_directory)
    try:
        config = bound({'path': args.configuration, 'sha256': args.configuration_sha256})
        from jsonschema import Draft202012Validator, FormatChecker
        schema = recovery.load(Path(__file__).with_name('config.schema.json').read_bytes())
        Draft202012Validator(schema, format_checker=FormatChecker()).validate(config)
        result = asyncio.run(Collector(config, journal).collect()) if args.mode == 'collect' else evaluate(config, journal)
    except Exception as exc:
        journal.artifacts.update({path.name:file_sha(path) for path in journal.root.iterdir() if path.is_file()})
        result = dict(version=4, task='4.44', lane='L44', outcome='fail', limitation=type(exc).__name__,
                      results=[dict(requirement=rid, criteria=[rid+'.'+str(i) for i in range(1,6)], outcome='fail', limitation='missing_or_failed_release_input') for rid in recovery.METRICS],
                      artifacts=dict(journal.artifacts), capacity_qualified=False, monthly_availability_qualified=False, final_shared_gate='9.1')
    journal.write('release-result.json', result)
    journal.write('artifact-sha256.json', journal.artifacts)
    print(json.dumps({'outcome':result['outcome'], 'result_path':str(journal.root/'release-result.json'), 'capacity_qualified':False}, sort_keys=True))
    return 0 if result['outcome']=='pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
