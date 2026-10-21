from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import time

from evidence_policy import validate_phase2, validate_phase3, PHASE3_SCENARIOS

_module = importlib.util.spec_from_file_location('persisted_state', Path(__file__).with_name('persisted-state.py'))
persistence = importlib.util.module_from_spec(_module)
_module.loader.exec_module(persistence)


class JourneyError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise JourneyError(message)


def write(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with path.open('x', encoding='utf-8') as output:
        json.dump(value, output, indent=2, sort_keys=True)
        output.write('\n')
    os.chmod(path, 0o600)


def bound_document(configuration, key):
    return persistence.load_bound(configuration[key + '_file'], configuration[key + '_sha256'])


def load_configuration(path, digest, source):
    c = persistence.load_bound(path, digest)
    require(c.get('schema_version') == 2 and c.get('realm') == 'private-anvil125' and c.get('allow_fixture_mutation') is True, 'actual isolated phase2 realm required')
    require(c.get('source_sha256') == source and re.fullmatch('[0-9a-f]{64}', source) is not None, 'current source binding required')
    require(re.fullmatch('kd-[0-9a-f]{24}', c.get('namespace', '')) is not None, 'coordinator namespace required')
    require(re.fullmatch('[0-9a-f-]{36}', c.get('attempt', '')) is not None, 'current attempt required')
    binding = c.get('binding', {})
    require(set(binding) == {'baseline', 'candidate', 'interface', 'layout', 'topology'}, 'complete source/interface/topology binding required')
    require(binding['candidate'] == source and all(re.fullmatch('[0-9a-f]{64}', item) for item in binding.values()), 'invalid digest bindings')
    gate = bound_document(c, 'prior_gate')
    require(gate.get('task') == '1.99' and gate.get('qualified') is True and gate.get('canonical_integrated') is True and gate.get('canonical_sha256')==binding['baseline'] and gate.get('output_interface_sha256')==binding['interface'], 'actual qualified1.99 result required; source implementation is insufficient')
    chain = bound_document(c, 'chain')
    require(chain.get('baselineTokenBalanceRaw') == '0' and chain.get('account', '').lower() == c['wallet'] and chain.get('token', '').lower() == c['token'] and chain.get('pool', '').lower() == c['pool'], 'actual zero-inventory fixture baseline and accounting partition required')
    require(chain.get('productionActivation') is False and chain['manifest']['chainId'] == 125, 'owned actual chain125 fixture required')
    for key in ('frontend_registry', 'backend_registry'):
        registry = bound_document(c, key)
        require(registry['deploymentVersion'] == chain['manifest']['deploymentVersion'], 'registry belongs to different deployed fixture')
    require(set(c['faults']) == {'duplicate_delivery', 'commit_before_ack', 'arm_candle', 'crash_candle', 'recover_candle'}, 'every task fault control is required')
    require(isinstance(c.get('marker'),str) and re.fullmatch('phase2-[0-9a-f-]{36}', c['marker']), 'unique bounded actual message marker required')
    require(c['wallet'].lower() == c['wallet'] and re.fullmatch('0x[0-9a-f]{40}', c['wallet']), 'normalized wallet required')
    require(c['pool'].lower() == c['pool'] and re.fullmatch('0x[0-9a-f]{40}', c['pool']), 'normalized pool required')
    return c


class Journey:
    def __init__(self, configuration, configuration_path, output):
        self.c = configuration
        self.configuration_path = str(Path(configuration_path).resolve())
        self.output = Path(output).resolve()
        require(not self.output.exists(), 'fresh phase2 output directory required')
        self.output.mkdir(parents=True, mode=0o700)
        self.started = time.time()
        self.database = persistence.PostgreSQL(configuration['postgres'])
        self.receipts = []
        self.deadline = time.monotonic() + 1200

    def command(self, name, argv, env=None, timeout=120):
        require(time.monotonic() < self.deadline, 'phase2 scenario budget exhausted')
        require(isinstance(argv, list) and argv and all(isinstance(v, str) and v and '\x00' not in v for v in argv), 'explicit argv required')
        tool = Path(argv[0])
        expected = self.c['tools'].get(str(tool))
        require(tool.is_absolute() and tool.is_file() and not tool.is_symlink() and expected == hashlib.sha256(tool.read_bytes()).hexdigest(), 'command executable is not coordinator bound')
        log = self.output / (name + '.log')
        before = time.time()
        with log.open('xb') as stream:
            process = subprocess.Popen(argv, env={**os.environ, **(env or {})}, stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                code = process.wait(timeout=min(timeout, max(1, self.deadline-time.monotonic())))
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, 15)
                try:
                    process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, 9)
                    process.wait(timeout=2)
                raise JourneyError('actual scenario timed out: ' + name)
            finally:
                try:
                    os.killpg(process.pid, 15)
                except ProcessLookupError:
                    pass
        os.chmod(log, 0o600)
        receipt = {'name': name, 'command': argv, 'exit_code': code, 'started_epoch': before, 'finished_epoch': time.time(), 'log': log.name, 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest()}
        self.receipts.append(receipt)
        require(code == 0, 'actual scenario failed: ' + name + '; exit=' + str(code) + '; log=' + str(log))
        return receipt

    def browser(self, phase):
        folder = self.output / phase
        require(not folder.exists(), 'browser namespace already exists')
        env = {'PHASE2_CONFIG_FILE': self.configuration_path, 'PHASE2_OUTPUT_DIR': str(folder), 'PHASE2_BROWSER_PHASE': phase}
        self.command(phase, [self.c['node'], str(Path(__file__).with_name('browser-journeys.mjs'))], env, 180)
        result_path = folder / 'result.json'
        require(result_path.is_file() and not result_path.is_symlink() and result_path.stat().st_mtime >= self.started, 'actual fresh browser result absent')
        return json.loads(result_path.read_text())

    def fault(self, name):
        control = self.c['faults'][name]
        require(control['source_sha256'] == self.c['source_sha256'], 'fault control source is stale')
        for source, digest in control['inputs'].items():
            source = Path(source)
            require(source.is_file() and not source.is_symlink() and hashlib.sha256(source.read_bytes()).hexdigest() == digest, 'fault control implementation changed')
        require(control['inputs'], 'fault control native source identity absent')
        self.command(name, control['argv'], {'PHASE2_ATTEMPT': self.c['attempt'], 'PHASE2_NAMESPACE': self.c['namespace'], 'PHASE2_OUTPUT_DIR': str(self.output)}, 120)

    def snapshot(self, name):
        result = self.database.snapshot(self.c['wallet'], self.c['pool'], self.c['marker'])
        require(result['database'] == self.c['postgres']['database'], 'read connected to a different database')
        write(self.output / (name + '.json'), result)
        return result

    def run(self):
        scenarios = []
        registry = self.browser('registry')
        require(registry['enabled_contracts'], 'no actual enabled contract code checked')
        scenarios.append({'id': 'deployment-registry', 'passed': True, 'checks': len(registry['enabled_contracts'])})
        browser = self.browser('journeys')
        target = {item['transactionHash'].lower() for item in browser['transactions'] if item['journey']=='trade'}
        deadline = time.monotonic()+60
        while True:
            before = self.database.snapshot(self.c['wallet'], self.c['pool'], self.c['marker'])
            indexed = {fill['position']['transactionHash'].lower() for fill in before['fills']}
            if target <= indexed and before['watchlist'] and before['chat'] and len(before['stats']) >= len(before['fills']) and len(before['charts']) >= 7:
                break
            require(time.monotonic() < deadline, 'actual joined projections/persistence did not catch up')
            time.sleep(.25)
        write(self.output / 'journeys-persisted.json', before)
        correlation = persistence.assert_persistence(before, browser)
        correlation['accounting'] = persistence.assert_accounting(before, self.c['wallet'], self.c['token'])
        write(self.output / 'correlation.json', correlation)
        scenarios.append({'id': 'persisted-browser-journeys', 'passed': True, 'checks': 4})
        self.fault('commit_before_ack')
        self.fault('duplicate_delivery')
        transport = json.loads((self.output / 'duplicate-delivery.json').read_text())
        require(transport['attempt'] == self.c['attempt'] and transport['namespace'] == self.c['namespace'], 'duplicate transport identity differs')
        require(transport['source_sha256'] == self.c['source_sha256'] and transport['actual_consumer_deliveries'] == 10, 'ten actual consumer deliveries required; broker deduplication alone is insufficient')
        require(transport['crash_after_commit_before_ack'] is True and transport['redelivered_after_crash'] is True, 'actual commit/ack crash and redelivery evidence required')
        after = self.snapshot('after-ten-deliveries')
        persistence.assert_duplicate_invariant(before, after)
        scenarios.append({'id': 'ten-duplicate-redelivery', 'passed': True, 'checks': 9})
        reverted = self.browser('receipt-withheld-reverted')
        require(reverted['transactions'][0]['pendingElapsedMs'] > 2000 and reverted['transactions'][0]['status'] == '0x0', 'real pending then reverted receipt required')
        scenarios.append({'id': 'receipt-withheld-reverted', 'passed': True, 'checks': 3})
        candle_before = self.snapshot('candle-before')
        armed = False
        try:
            self.fault('arm_candle')
            armed = True
            fills = self.browser('concurrent-candle-fills')
            blocked = self.snapshot('candle-precommit')
            self.fault('crash_candle')
        finally:
            if armed:
                self.fault('recover_candle')
        recovered = self.snapshot('candle-recovered')
        persistence.assert_candle_recovery(candle_before, blocked, recovered, fills)
        persistence.assert_accounting(recovered, self.c['wallet'], self.c['token'])
        scenarios.append({'id': 'concurrent-candle-precommit-crash', 'passed': True, 'checks': 4})
        artifacts = {}
        for path in sorted(self.output.rglob('*')):
            if path.is_file():
                require(not path.is_symlink(), 'artifact symlink forbidden')
                artifacts[path.relative_to(self.output).as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
        result = {'schema_version': 2, 'task': '2.49', 'binding': self.c['binding'], 'attempt': self.c['attempt'], 'namespace': self.c['namespace'], 'started_epoch': self.started, 'finished_epoch': time.time(), 'scenarios': scenarios, 'executions': self.receipts, 'artifacts': artifacts, 'phase2_qualified': True, 'production_qualified': False, 'capacity_qualified': False, 'disaster_recovery_qualified': False}
        validate_phase2(self.output, result, self.c['binding'], self.started)
        write(self.output / 'result.json', result)
        return result



PHASE3_FAULTS = frozenset({'observe_topology', 'exhaust_database_pool', 'release_database_pool',
                         'introduce_rpc_disagreement', 'restore_rpc', 'rollback_rehearsal',
                         'arm_consumer_commit_ack', 'crash_consumer', 'recover_consumer',
                         'redeliver_consumer', 'observe_consumer'})


def load_phase3_configuration(path, digest, source):
    from adapters import RecoveryPermit
    c = persistence.load_bound(path, digest)
    require(c.get('schema_version') == 3 and c.get('realm') == 'private-anvil125' and c.get('allow_fixture_mutation') is True, 'actual isolated phase3 realm required')
    require(re.fullmatch('[0-9a-f]{64}', source or '') and c.get('source_sha256') == source, 'current phase3 source required')
    require(re.fullmatch('kd-[0-9a-f]{24}', c.get('namespace','')) and re.fullmatch('[0-9a-f-]{36}', c.get('attempt','')), 'exact isolated current lease required')
    binding = c.get('binding',{})
    require(set(binding)=={'baseline','candidate','interface','layout','topology'} and binding['candidate']==source and all(re.fullmatch('[0-9a-f]{64}', v) for v in binding.values()), 'complete current candidate binding required')
    prior = bound_document(c,'prior_gate')
    require(prior.get('task')=='2.99' and prior.get('qualified') is True and prior.get('canonical_integrated') is True and prior.get('canonical_sha256')==binding['baseline'] and prior.get('output_interface_sha256')==binding['interface'], 'actual qualified2.99 output required')
    chain = bound_document(c,'chain')
    require(chain.get('productionActivation') is False and chain['manifest']['chainId']==125 and chain.get('baselineTokenBalanceRaw')=='0', 'actual owned chain125 and zero inventory baseline required')
    for key in ('wallet','pool','token'):
        require(re.fullmatch('0x[0-9a-f]{40}', c.get(key,'')) and chain.get({'wallet':'account','pool':'pool','token':'token'}[key],'').lower()==c[key], 'actual normalized accounting partition required')
    require(re.fullmatch('phase3-[0-9a-f-]{36}',c.get('marker','')), 'unique phase3 durable marker required')
    for key in ('frontend_registry','backend_registry'):
        require(bound_document(c,key)['deploymentVersion']==chain['manifest']['deploymentVersion'], 'registry does not belong to owned deployment')
    topology = bound_document(c,'recovery_topology')
    require(c['recovery_topology_sha256']==binding['topology'] and topology.get('namespace')==c['namespace'] and topology.get('attempt')==c['attempt'], 'exact actual topology/lease required')
    require(topology.get('postgres_failover')=='synchronous' and topology.get('jetstream_replicas')==3, 'synchronous PG and actual NATS R3 topology required')
    resources = topology.get('resources',[])
    permits = [RecoveryPermit(r['container_id'],c['namespace'],c['attempt'],r['role'],r['failure_domain'],tuple(r['volumes']),tuple(r['actions'])) for r in resources]
    require(len({p.container_id for p in permits})==len(permits), 'duplicate recovery resource')
    az = topology.get('lost_failure_domain')
    victims = [p for p in permits if p.failure_domain==az]
    require({p.role for p in victims}>={'app','postgres','nats'}, 'AZ removal must include application, database and broker')
    for role in ('postgres','nats'):
        nodes=[p for p in permits if p.role==role]
        require(len({p.failure_domain for p in nodes})>=3, 'three independent '+role+' failure domains required')
    require(set(c.get('faults',{}))==PHASE3_FAULTS, 'actual source-bound fault controls incomplete')
    consumers=c.get('durable_consumers',[])
    require(consumers and len({v['id'] for v in consumers})==len(consumers), 'complete unique durable consumer inventory required')
    require({v['schema'] for v in consumers}=={'indexer','stats','candles','pnl','users','chat','metadata','livestream','ranking'}, 'every durable consumer schema required')
    for v in consumers:
        require(re.fullmatch('[a-zA-Z0-9_.-]{1,128}',v['id']) and v['container_id'] in {p.container_id for p in permits if p.role=='app'}, 'consumer must own exact application target')
    require(type(c.get('recovery_timeout_seconds')) is int and 1<=c['recovery_timeout_seconds']<=120 and type(c.get('detection_threshold_seconds')) is int and 1<=c['detection_threshold_seconds']<=5, 'bounded fault detection/recovery required')
    return c


class Phase3Journey(Journey):
    def __init__(self, configuration, configuration_path, output):
        from adapters import RecoveryPermit, RecoveryFaults
        super().__init__(configuration, configuration_path, output)
        self.topology=bound_document(configuration,'recovery_topology')
        docker=Path(configuration['docker'])
        require(docker.is_absolute() and docker.is_file() and not docker.is_symlink() and configuration['tools'].get(str(docker))==hashlib.sha256(docker.read_bytes()).hexdigest(), 'bound Docker executable required')
        permits=[RecoveryPermit(r['container_id'],configuration['namespace'],configuration['attempt'],r['role'],r['failure_domain'],tuple(r['volumes']),tuple(r['actions'])) for r in self.topology['resources']]
        self.resources=RecoveryFaults(permits,configuration['namespace'],configuration['attempt'],str(docker))
        self.actions=[]

    def browser(self, phase):
        folder=self.output/phase
        require(not folder.exists(),'fresh phase3 browser namespace required')
        self.command(phase,[self.c['node'],str(Path(__file__).with_name('browser-journeys.mjs'))],{'PHASE3_CONFIG_FILE':self.configuration_path,'PHASE3_OUTPUT_DIR':str(folder),'PHASE3_BROWSER_PHASE':phase},180)
        result_path=folder/'result.json'
        require(result_path.is_file() and not result_path.is_symlink() and result_path.stat().st_mtime>=self.started,'actual fresh browser result absent')
        return json.loads(result_path.read_text())

    def control(self, name, suffix='', consumer=None):
        control=self.c['faults'][name]
        require(control['source_sha256']==self.c['source_sha256'] and control['inputs'],'current native fault control sources required')
        for path,digest in control['inputs'].items():
            path=Path(path)
            require(path.is_absolute() and path.is_file() and not path.is_symlink() and hashlib.sha256(path.read_bytes()).hexdigest()==digest,'native control source changed')
        execution=name+suffix
        env={'PHASE3_ATTEMPT':self.c['attempt'],'PHASE3_NAMESPACE':self.c['namespace'],'PHASE3_OUTPUT_DIR':str(self.output),'PHASE3_CONTROL_RESULT':str(self.output/(execution+'.json'))}
        if consumer is not None:
            env.update({'PHASE3_CONSUMER_ID':consumer['id'],'PHASE3_CONSUMER_SCHEMA':consumer['schema'],'PHASE3_CONSUMER_CONTAINER':consumer['container_id']})
        self.command(execution,control['argv'],env,120)
        result_path=self.output/(execution+'.json')
        require(result_path.is_file() and not result_path.is_symlink() and result_path.stat().st_mtime>=self.started,'fresh native fault observation absent: '+execution)
        result=json.loads(result_path.read_text())
        require(result.get('attempt')==self.c['attempt'] and result.get('namespace')==self.c['namespace'] and result.get('source_sha256')==self.c['source_sha256'],'native fault observation identity differs')
        return result

    def action(self, cid, action):
        result=self.resources.apply(cid,action)
        self.actions.append(result)
        write(self.output/('resource-action-'+str(len(self.actions))+'.json'),result)

    def security_snapshot(self,name):
        result=persistence.adversarial_snapshot(self.database,self.c['wallet'])
        require(result['database']==self.c['postgres']['database'],'adversarial snapshot used different database')
        write(self.output/(name+'.json'),result)
        return result

    def reconcile(self,name,before):
        deadline=time.monotonic()+self.c['recovery_timeout_seconds']
        while True:
            after=self.database.snapshot(self.c['wallet'],self.c['pool'],self.c['marker'])
            try:
                persistence.assert_acknowledged_preserved(before,after)
                persistence.assert_accounting(after,self.c['wallet'],self.c['token'])
                break
            except persistence.PersistenceError:
                require(time.monotonic()<deadline,'acknowledged state/replay did not recover within bound')
                time.sleep(.25)
        write(self.output/(name+'.json'),after)
        return after

    def run(self):
        scenarios=[]
        registry=self.browser('registry')
        require(registry['enabled_contracts'],'actual registry check absent')
        browser=self.browser('journeys')
        deadline=time.monotonic()+60
        while True:
            before=self.database.snapshot(self.c['wallet'],self.c['pool'],self.c['marker'])
            try:
                persistence.assert_persistence(before,browser)
                persistence.assert_accounting(before,self.c['wallet'],self.c['token'])
                break
            except persistence.PersistenceError:
                require(time.monotonic()<deadline,'actual persisted browser journeys missing')
                time.sleep(.25)
        write(self.output/'acknowledged-baseline.json',before)
        self.browser('scanner-baseline')
        media_before=self.security_snapshot('media-before')
        scanners=[p for p in self.resources.permits.values() if p.role=='scanner']
        require(scanners,'actual scanner resource required')
        disrupted=[]
        try:
            for p in scanners:
                self.action(p.container_id,'pause'); disrupted.append(p.container_id)
            denied=self.browser('scanner-outage')
            require(denied['scanner_rejected'] is True,'scanner outage became clean upload')
        finally:
            for cid in reversed(disrupted): self.action(cid,'unpause')
        media_after=self.security_snapshot('scanner-after')
        persistence.assert_webhook_unchanged(media_before,media_after)
        attacks=self.browser('media-auth-adversarial')
        require(attacks['unauthenticated_webhook_denied'] and attacks['authorization_denied'] and attacks['unsafe_uploads_denied'],'actual security deny assertions absent')
        webhook_after=self.security_snapshot('webhook-after')
        persistence.assert_webhook_unchanged(media_after,webhook_after)
        self.browser('webhook-valid')
        accepted=self.security_snapshot('webhook-accepted')
        require(accepted['stream_inbox']!=webhook_after['stream_inbox'],'genuine signed callback did not reach actual durable inbox')
        self.browser('webhook-replay')
        replayed=self.security_snapshot('webhook-replayed')
        persistence.assert_webhook_unchanged(accepted,replayed)
        scenarios.append({'id':'scanner-webhook-auth','passed':True,'checks':len(attacks['requests'])+3})
        for index,consumer in enumerate(self.c['durable_consumers']):
            suffix='-'+str(index)
            baseline=self.snapshot('consumer-before'+suffix)
            durable_before=persistence.consumer_snapshot(self.database,consumer)
            write(self.output/('consumer-effects-before'+suffix+'.json'),durable_before)
            armed=False
            try:
                arm=self.control('arm_consumer_commit_ack',suffix,consumer);armed=True
                require(arm.get('consumer_id')==consumer['id'],'wrong consumer fault armed')
                self.control('redeliver_consumer',suffix,consumer)
                observation=self.control('observe_consumer',suffix,consumer)
                require(observation.get('consumer_id')==consumer['id'] and observation.get('committed_before_ack') is True and observation.get('acknowledged') is False,'actual commit-before-ACK boundary absent')
                self.control('crash_consumer',suffix,consumer)
            finally:
                if armed:self.control('recover_consumer',suffix,consumer)
            redelivery=self.control('redeliver_consumer','-recovered'+suffix,consumer)
            require(redelivery.get('consumer_id')==consumer['id'] and redelivery.get('actual_consumer_deliveries',0)>=10 and redelivery.get('redelivered_after_crash') is True,'actual durable redelivery missing')
            after=self.reconcile('consumer-after'+suffix,baseline)
            persistence.assert_duplicate_invariant(baseline,after)
            durable_after=persistence.consumer_snapshot(self.database,consumer)
            write(self.output/('consumer-effects-after'+suffix+'.json'),durable_after)
            require(durable_before==durable_after,'duplicate/crash changed actual consumer business effects, inbox or outbox')
        scenarios.append({'id':'all-consumer-commit-ack','passed':True,'checks':len(self.c['durable_consumers'])})
        topology=self.control('observe_topology','-before-az')
        require(topology.get('topology_sha256')==self.c['binding']['topology'] and topology.get('jetstream_replicas')==3 and topology.get('postgres_synchronous') is True,'observed live durability topology differs')
        az_before=self.snapshot('az-before');victims=[]
        try:
            for permit in self.resources.permits.values():
                self.resources.inspect(permit.container_id)
                if permit.failure_domain==self.topology['lost_failure_domain']:
                    self.action(permit.container_id,'stop');victims.append(permit.container_id)
            self.reconcile('az-surviving',az_before)
            probe=self.browser('az-surviving')
            require(probe['persisted_visible'] is True,'AZ client replay did not restore acknowledged state')
            surviving=self.control('observe_topology','-during-az')
            require(surviving.get('postgres_primary_available') is True and surviving.get('jetstream_quorum_available') is True,'actual single-AZ quorum/failover unavailable')
        finally:
            for cid in reversed(victims):self.action(cid,'start')
        self.reconcile('az-recovered',az_before)
        scenarios.append({'id':'az-loss-ledger','passed':True,'checks':4})
        for role,scenario in [('redis','redis-outage'),('nats','nats-quorum-loss'),('object-fixture','object-storage-failure'),('app','application-restart')]:
            baseline=self.snapshot(scenario+'-before');paused=[]
            targets=[p for p in self.resources.permits.values() if p.role==role]
            require(targets,'exact actual dependency absent: '+role)
            try:
                for p in targets:
                    self.action(p.container_id,'pause' if role!='app' else 'stop');paused.append(p.container_id)
                if role!='app':
                    denied=self.browser(scenario)
                    require(denied['mutation_rejected'] is True and denied['explicit_unavailable'] is True,'dependency failure acknowledged volatile mutation or concealed unavailable state')
            finally:
                for cid in reversed(paused):self.action(cid,'unpause' if role!='app' else 'start')
            self.reconcile(scenario+'-recovered',baseline)
            if role=='app':
                replay=self.browser('application-recovered')
                require(replay['persisted_visible'] is True,'actual client restart replay missing')
            scenarios.append({'id':scenario,'passed':True,'checks':2})
        baseline=self.snapshot('pool-exhaustion-before');armed=False
        try:
            self.control('exhaust_database_pool');armed=True
            result=self.browser('database-pool-exhaustion')
            require(result['mutation_rejected'] and result['explicit_unavailable'],'database exhaustion did not reject bounded mutation')
        finally:
            if armed:self.control('release_database_pool')
        self.reconcile('pool-exhaustion-recovered',baseline)
        scenarios.append({'id':'database-pool-exhaustion','passed':True,'checks':2})
        heads=self.security_snapshot('rpc-before');armed=False
        try:
            conflict=self.control('introduce_rpc_disagreement');armed=True
            require(conflict.get('actual_rpc_responses_file') and conflict.get('conflicting_height') is not None,'raw actual RPC disagreement absent')
            blocked=self.security_snapshot('rpc-blocked')
            persistence.assert_rpc_quarantine(heads,blocked,conflict['affected_stream'],conflict['conflicting_height'])
            state=self.browser('rpc-quarantine')
            require(state['explicit_unavailable'],'client concealed canonical integrity incident')
        finally:
            if armed:self.control('restore_rpc')
        scenarios.append({'id':'rpc-integrity-quarantine','passed':True,'checks':3})
        rollback=self.control('rollback_rehearsal')
        require(rollback.get('rollback_denied') is True and rollback.get('schema_compatible') is False and rollback.get('registry_compatible') is False and rollback.get('production_changed') is False and rollback.get('validator_changed') is False,'incompatible rollback was not denied before isolated activation')
        scenarios.append({'id':'incompatible-rollback-denial','passed':True,'checks':4})
        require({s['id'] for s in scenarios}==PHASE3_SCENARIOS,'phase3 scenario inventory differs')
        artifacts={p.relative_to(self.output).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(self.output.rglob('*')) if p.is_file() and '.private' not in p.relative_to(self.output).parts}
        result={'schema_version':3,'task':'3.49','binding':self.c['binding'],'attempt':self.c['attempt'],'namespace':self.c['namespace'],'started_epoch':self.started,'finished_epoch':time.time(),'scenarios':scenarios,'executions':self.receipts,'artifacts':artifacts,'resource_actions':self.actions,'durable_consumers':self.c['durable_consumers'],'phase3_qualified':True,'production_qualified':False,'capacity_qualified':False,'disaster_recovery_qualified':False}
        validate_phase3(self.output,result,self.c['binding'],self.started)
        write(self.output/'result.json',result)
        return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=('2','3'), default='2')
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--config-sha256', required=True)
    parser.add_argument('--source-sha256', required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    loader = load_phase3_configuration if args.phase=='3' else load_configuration
    runner = Phase3Journey if args.phase=='3' else Journey
    c = loader(args.config, args.config_sha256, args.source_sha256)
    result = runner(c, args.config, args.output_dir).run()
    key = 'phase3_qualified' if args.phase=='3' else 'phase2_qualified'
    print(json.dumps({'task': result['task'], key: result[key], 'checks': sum(s['checks'] for s in result['scenarios']), 'production_qualified': False}))


if __name__ == '__main__':
    main()
