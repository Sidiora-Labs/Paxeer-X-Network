from __future__ import annotations

import argparse
import configparser
from contextlib import contextmanager
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import time

from adapters import RecoveryFaults, RecoveryPermit
from journeys import Journey, load_configuration, persistence, write

ROOT = Path(__file__).resolve().parents[4]
PROVIDERS = ROOT / 'deploy/launchpad/production/recovery/phase2'
SOURCES = ('replay_restore.py', 'persisted-state.py', 'evidence_policy.py', 'adapters.py', 'journeys.py', 'browser-journeys.mjs')
SCENARIOS = frozenset({'isolated-regressions', 'durable-replay-calibration', 'physical-pitr-checkpoint', 'recovery-journey-timestamps'})


class RecoveryError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise RecoveryError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def source_digest():
    return digest(canonical({name:digest(Path(__file__).with_name(name).read_bytes()) for name in SOURCES}))


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value


@contextmanager
def deployment_modules():
    previous = {name:sys.modules.get(name) for name in ('recovery', 'providers', 'render', 'operator')}
    old_path = list(sys.path)
    try:
        sys.path.insert(0, str(PROVIDERS))
        contracts = module('recovery', PROVIDERS.parent / 'recovery.py')
        provider = module('providers', PROVIDERS / 'providers.py')
        renderer = module('render', PROVIDERS / 'render.py')
        yield contracts, provider, renderer
    finally:
        sys.path[:] = old_path
        for name, value in previous.items():
            if value is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = value


def shared_modules():
    path = str(ROOT / 'tools/launchpad/production/load')
    if path not in sys.path:
        sys.path.insert(0, path)
    import capture
    import calibration
    import workload
    return capture, calibration, workload


def bound(c, name):
    return persistence.load_bound(c[name + '_file'], c[name + '_sha256'])


def load_configuration4(path, sha256, source):
    c = persistence.load_bound(path, sha256)
    require(c.get('schema_version') == 4 and c.get('realm') == 'private-anvil125' and c.get('allow_fixture_mutation') is True, 'actual isolated phase4 authority required')
    require(re.fullmatch('[0-9a-f]{64}', source or '') and c.get('source_sha256') == source, 'current phase4 candidate required')
    require(re.fullmatch('kd-[0-9a-f]{24}', c.get('namespace','')) and re.fullmatch('[0-9a-f-]{36}', c.get('attempt','')), 'current exact recovery resource lease required')
    binding = c.get('binding',{})
    require(set(binding) == {'baseline','candidate','interface','layout','topology'} and binding['candidate'] == source and all(re.fullmatch('[0-9a-f]{64}',v) for v in binding.values()), 'complete source/interface/topology binding required')
    gate = bound(c,'prior_gate')
    require(gate.get('task') == '3.99' and gate.get('qualified') is True and gate.get('canonical_integrated') is True and gate.get('canonical_sha256') == binding['baseline'] and gate.get('output_interface_sha256') == binding['interface'], 'actual qualified3.99 output required')
    require(c.get('harness_sha256') == source_digest(), 'recovery harness source changed')
    require(re.fullmatch('[0-9a-f]{64}',c.get('generator_sha256','')) and c['generator_sha256']!='0'*64, 'actual independent calibrated generator identity required')
    capture, calibration, workload = shared_modules()
    require(c.get('root_capacity_ledger_sha256') == digest(capture.canonical(workload.ledger())) and c.get('root_mix_sha256') == digest(capture.canonical(workload.MIX)), 'root capacity ledger/workload mix differs')
    manifest = Path(__file__).with_name('phase-4-scenario-manifest.json')
    require(c.get('scenario_manifest_sha256') == digest(manifest.read_bytes()), 'phase4 scenario manifest changed')
    require(c.get('scope') == 'isolated_component' and c.get('final_gate') == '9.1', 'phase4 isolated proof cannot run final global gate')
    require(set(c.get('provider_sources',{})) == {'deploy/launchpad/production/recovery/recovery.py','deploy/launchpad/production/recovery/phase2/providers.py','deploy/launchpad/production/recovery/phase2/render.py','deploy/launchpad/production/recovery/phase2/reconciliation.sql'}, 'actual recovery provider source inventory required')
    for relative, sha in c['provider_sources'].items():
        path = ROOT / relative
        require(path.is_file() and not path.is_symlink() and digest(path.read_bytes()) == sha, 'actual recovery provider source changed')
    regression = bound(c,'regression_configuration')
    require(regression.get('source_sha256') == source and regression.get('namespace') == c['namespace'] and regression.get('attempt') == c['attempt'], 'regression runtime belongs to another candidate/lease')
    require(all(c.get(key)==regression.get(key) for key in ('wallet','pool','token','marker')), 'same actual regression/recovery accounting partition required')
    require(type(c.get('replay_pause_seconds')) is int and 1 <= c['replay_pause_seconds'] <= 60 and type(c.get('replay_recovery_seconds')) is int and 1 <= c['replay_recovery_seconds'] <= 120, 'bounded isolated replay calibration required')
    tables = c.get('reconciliation_tables',[])
    persistence.recovery_snapshot_sql(tables,commit_timestamps=True)
    topology = bound(c,'recovery_topology')
    require(topology.get('attempt') == c['attempt'] and topology.get('namespace') == c['namespace'], 'actual recovery topology lease differs')
    resources = topology.get('resources',[])
    require(resources and len({r['container_id'] for r in resources}) == len(resources), 'unique exact recovery resource inventory required')
    for r in resources:
        RecoveryPermit(r['container_id'],c['namespace'],c['attempt'],r['role'],r['failure_domain'],tuple(r['volumes']),tuple(r['actions']))
    require(c.get('replay_consumers') and len({v['id'] for v in c['replay_consumers']}) == len(c['replay_consumers']), 'every actual replay consumer required')
    require({v['schema'] for v in c['replay_consumers']} >= {'stats','candles','pnl'}, 'actual statistics/candles/positions replay consumers required')
    app_ids = {r['container_id'] for r in resources if r['role'] == 'app'}
    require(all(v['container_id'] in app_ids and re.fullmatch('[a-zA-Z0-9_.-]{1,128}',v['id']) and re.fullmatch('[a-zA-Z0-9_-]{1,128}',v['stream']) and re.fullmatch('[a-zA-Z0-9_-]{1,128}',v['durable']) for v in c['replay_consumers']), 'exact resource-backed broker consumer inventory required')
    native = bound(c,'native_runtime_authority')
    require(native.get('candidate') == source and native.get('namespace') == c['namespace'] and native.get('attempt') == c['attempt'] and native.get('realm') == 'isolated-recovery-qualification', 'reviewed isolated recovery runtime authority absent')
    require(set(c.get('controls',{})) == {'capture_checkpoint','recovery_fence','recovery_login_read_write','archive_rebuild'}, 'actual capture/fence/recovery/archive controls required')
    require(re.fullmatch('[0-9a-f]{64}',c.get('clock_calibration_sha256','')), 'bound real clock calibration required')
    return c


def recovery_timestamps(*, loss_utc_ns, loss_monotonic_ns, verified_utc_ns, verified_monotonic_ns, last_acknowledged_utc_ns, last_restored_utc_ns):
    values = (loss_utc_ns,loss_monotonic_ns,verified_utc_ns,verified_monotonic_ns,last_acknowledged_utc_ns,last_restored_utc_ns)
    require(all(type(v) is int and v > 0 for v in values), 'actual integer recovery timestamps required')
    require(last_restored_utc_ns <= last_acknowledged_utc_ns <= loss_utc_ns <= verified_utc_ns and verified_monotonic_ns >= loss_monotonic_ns, 'recovery timestamps are out of order')
    rpo = loss_utc_ns - last_restored_utc_ns
    rto = verified_monotonic_ns - loss_monotonic_ns
    require(rpo <= 300*10**9 and rto <= 1800*10**9, 'measured recovery exceeds RPO5minutes or RTO30minutes')
    require(abs((verified_utc_ns-loss_utc_ns)-rto) <= 10_000_000, 'wall clock and monotonic recovery duration disagree')
    return {'loss_utc_ns':str(loss_utc_ns),'verified_login_read_write_utc_ns':str(verified_utc_ns),'last_acknowledged_utc_ns':str(last_acknowledged_utc_ns),'last_restored_utc_ns':str(last_restored_utc_ns),'rpo_ns':str(rpo),'rto_ns':str(rto),'utc_duration_ns':str(verified_utc_ns-loss_utc_ns),'scope':'isolated_component','final_gate':'9.1','final_integrated_recovery_qualified':False}


class Phase4Recovery(Journey):
    def __init__(self,c,configuration_path,output):
        super().__init__(c,configuration_path,output)
        self.started_ns = time.time_ns()
        self.topology = bound(c,'recovery_topology')
        permits = [RecoveryPermit(r['container_id'],c['namespace'],c['attempt'],r['role'],r['failure_domain'],tuple(r['volumes']),tuple(r['actions'])) for r in self.topology['resources']]
        docker = Path(c['docker'])
        require(c['tools'].get(str(docker)) == digest(docker.read_bytes()) and not docker.is_symlink(), 'current exact Docker executable required')
        self.resources = RecoveryFaults(permits,c['namespace'],c['attempt'],str(docker))
        self.action_receipts = []
        self.native_receipts = []
        self.tables = c['reconciliation_tables']
        self.deadline = time.monotonic()+1200

    def control(self,name):
        control = self.c['controls'][name]
        require(control.get('inputs') and control.get('source_sha256') == self.c['source_sha256'], 'actual current control implementation required')
        for path, sha in control['inputs'].items():
            path = Path(path)
            require(path.is_absolute() and path.is_file() and not path.is_symlink() and digest(path.read_bytes()) == sha, 'actual control source changed')
        output = self.output/(name+'.json')
        self.command(name,control['argv'],{'PHASE4_CONFIG_FILE':self.configuration_path,'PHASE4_OUTPUT_DIR':str(self.output),'PHASE4_CONTROL_RESULT':str(output),'PHASE4_NAMESPACE':self.c['namespace'],'PHASE4_ATTEMPT':self.c['attempt']},180)
        require(output.is_file() and not output.is_symlink() and output.stat().st_mtime_ns >= self.started_ns, 'fresh native control result absent')
        result = json.loads(output.read_text())
        require(result.get('candidate') == self.c['source_sha256'] and result.get('attempt') == self.c['attempt'] and result.get('namespace') == self.c['namespace'], 'native control belongs to another candidate/lease')
        require(result.get('raw_artifacts'), 'actual native provider raw observations missing')
        for relative,sha in result['raw_artifacts'].items():
            path = self.output/relative
            require(not Path(relative).is_absolute() and '..' not in Path(relative).parts and not path.is_symlink() and path.resolve().is_relative_to(self.output) and path.is_file() and digest(path.read_bytes()) == sha, 'native raw artifact changed or escaped')
        return result

    def action(self,cid,action):
        row = self.resources.apply(cid,action)
        self.action_receipts.append(row)
        write(self.output/('resource-'+str(len(self.action_receipts))+'.json'),row)

    def nats(self,name,arguments):
        require(all(isinstance(v,str) and v and '\x00' not in v for v in arguments), 'bounded actual NATS command required')
        credentials = Path(self.c['nats_credentials_file'])
        require(credentials.is_file() and not credentials.is_symlink() and credentials.stat().st_mode & 0o077 == 0 and digest(credentials.read_bytes()) == self.c['nats_credentials_sha256'], 'private source-bound broker credentials required')
        from urllib.parse import urlsplit
        target = urlsplit(self.c['nats_url'])
        require(target.scheme in ('nats','tls') and ipaddress.ip_address(target.hostname).is_loopback and target.username is None and target.password is None and target.port in self.c['allocated_nats_ports'], 'exact isolated broker origin required')
        self.command(name,[self.c['nats_cli'],'--server',self.c['nats_url'],'--creds',str(credentials),*arguments,'--json'],timeout=30)
        raw = (self.output/(name+'.log')).read_bytes()
        result = json.loads(raw)
        write(self.output/(name+'.json'),result)
        return result

    def replay(self):
        before = self.snapshot('replay-before')
        persistence.assert_accounting(before,self.c['wallet'],self.c['token'])
        cursors = []
        paused = []
        begin = time.monotonic_ns()
        try:
            for index,consumer in enumerate(self.c['replay_consumers']):
                stream = self.nats('stream-'+str(index),['stream','info',consumer['stream']])
                require(stream['config']['num_replicas'] == 3 and stream['config']['max_age'] >= 259200*10**9, 'actual broker lacks R3 or72hour hot retention')
                cursor = self.nats('cursor-before-'+str(index),['consumer','info',consumer['stream'],consumer['durable']])
                require(cursor['config']['durable_name'] == consumer['durable'], 'actual durable consumer differs')
                cursors.append(cursor)
                if consumer['container_id'] not in paused:
                    self.action(consumer['container_id'],'stop');paused.append(consumer['container_id'])
            path=self.c['regression_configuration_file']
            config=load_configuration(path,self.c['regression_configuration_sha256'],self.c['source_sha256'])
            producer=Journey(config,path,self.output/'replay-browser')
            produced=producer.browser('concurrent-candle-fills')
            require(len(produced['candle_fill_hashes'])==2, 'two actual mined source fills required while projection consumers are stopped')
            for index,consumer in enumerate(self.c['replay_consumers']):
                pending=self.nats('cursor-paused-'+str(index),['consumer','info',consumer['stream'],consumer['durable']])
                require(pending['num_pending']>0 or pending['num_ack_pending']>0, 'actual missed durable delivery was not observed during interruption')
            end = time.monotonic()+self.c['replay_pause_seconds']
            while time.monotonic() < end:
                require(time.monotonic()<self.deadline,'bounded replay budget exceeded')
                time.sleep(max(0,min(.25,end-time.monotonic())))
        finally:
            for cid in reversed(paused):self.action(cid,'start')
        deadline = time.monotonic()+self.c['replay_recovery_seconds']
        while True:
            after = self.database.snapshot(self.c['wallet'],self.c['pool'],self.c['marker'])
            try:
                persistence.assert_accounting(after,self.c['wallet'],self.c['token'])
                fills={row['sourceFillId']:row for row in after['fills']}
                require(all(fills.get(row['sourceFillId'])==row for row in before['fills']), 'replay lost or changed acknowledged source facts')
                require(set(produced['candle_fill_hashes']) <= {row['position']['transactionHash'].lower() for row in after['fills']}, 'actual disconnected-period source fills missing after replay')
                for key in ('stats_inbox','candles_inbox','pnl_inbox'):
                    require(len(after[key])>len(before[key]), 'consumer replay produced no committed inbox advance: '+key)
                break
            except persistence.PersistenceError:
                require(time.monotonic()<deadline,'durable cursor replay did not recover actual projections')
                time.sleep(.25)
        write(self.output/'replay-after.json',after)
        for index,consumer in enumerate(self.c['replay_consumers']):
            after_cursor = self.nats('cursor-after-'+str(index),['consumer','info',consumer['stream'],consumer['durable']])
            require(after_cursor['ack_floor']['stream_seq'] > cursors[index]['ack_floor']['stream_seq'] and after_cursor['num_ack_pending'] == 0, 'actual broker cursor did not advance over interrupted-period events or durable ACK recovery incomplete')
        archive = self.control('archive_rebuild')
        require(archive.get('source') == 'raw-chain-and-postgresql-outbox' and archive.get('expected_checksums') == archive.get('actual_checksums') and archive.get('expected_checksums') and archive.get('retention_gap_silently_skipped') is False and archive.get('old_epoch_events') == 0, 'immutable archive/PostgreSQL rebuild did not reconcile authoritative checkpoint')
        result = {'started_monotonic_ns':str(begin),'finished_monotonic_ns':str(time.monotonic_ns()),'pause_seconds':self.c['replay_pause_seconds'],'actual_elapsed72h_proven':False,'consumers':self.c['replay_consumers'],'checksum_sha256':digest(canonical({k:after[k] for k in ('stats','charts','positions','stats_inbox','candles_inbox','pnl_inbox')})),'capacity_qualified':False,'final_gate':'9.1'}
        write(self.output/'replay-calibration.json',result)
        return result

    def arm72h(self):
        inventory=[];stopped=[]
        try:
            for index,consumer in enumerate(self.c['replay_consumers']):
                stream=self.nats('72h-stream-'+str(index),['stream','info',consumer['stream']])
                require(stream['config']['num_replicas']==3 and stream['config']['max_age']>=259200*10**9, 'actual72hour R3 retention required before disconnect')
                cursor=self.nats('72h-cursor-'+str(index),['consumer','info',consumer['stream'],consumer['durable']])
                if consumer['container_id'] not in stopped:
                    self.action(consumer['container_id'],'stop');stopped.append(consumer['container_id'])
                inventory.append({'consumer':consumer,'cursor':cursor})
            journal={'schema_version':4,'operation':'72hour-replay-disconnected','candidate':self.c['source_sha256'],'interface':self.c['binding']['interface'],'topology':self.c['binding']['topology'],'namespace':self.c['namespace'],'attempt':self.c['attempt'],'disconnected_utc_ns':str(time.time_ns()),'consumers':inventory,'resource_actions':self.action_receipts,'actual_elapsed72h_proven':False,'final_gate':'9.1','final_integrated_gate_passed':False}
            write(self.output/'72hour-replay-journal.json',journal)
            return journal
        except BaseException:
            for cid in reversed(stopped):self.action(cid,'start')
            raise

    def resume72h(self,journal_path,journal_sha256):
        journal=persistence.load_bound(journal_path,journal_sha256)
        require(journal.get('operation')=='72hour-replay-disconnected' and journal.get('candidate')==self.c['source_sha256'] and journal.get('interface')==self.c['binding']['interface'] and journal.get('topology')==self.c['binding']['topology'] and journal.get('namespace')==self.c['namespace'] and journal.get('attempt')==self.c['attempt'], 'actual72hour disconnect journal belongs to another source/lease')
        disconnected=int(journal['disconnected_utc_ns'])
        resumed=time.time_ns()
        require(disconnected>0 and resumed-disconnected>=259200*10**9, 'elapsed72hours must be real UTC duration; no accelerated clock')
        require([row['consumer'] for row in journal['consumers']]==self.c['replay_consumers'], 'replay consumer inventory changed while disconnected')
        restored=set()
        for index,row in enumerate(journal['consumers']):
            consumer=row['consumer']
            state=self.resources.inspect(consumer['container_id'])
            require(state['State']['Running'] is False, 'consumer reconnected before recorded72hour interruption ended')
            stream=self.nats('72h-retention-'+str(index),['stream','info',consumer['stream']])
            first=stream['state']['first_seq']
            acknowledged=row['cursor']['ack_floor']['stream_seq']
            if first>acknowledged+1:
                archive=self.control('archive_rebuild')
                require(archive.get('source')=='raw-chain-and-postgresql-outbox' and archive.get('retention_gap_silently_skipped') is False and archive.get('expected_checksums') and archive.get('expected_checksums')==archive.get('actual_checksums'), 'expired cursor requires authoritative immutable archive/PostgreSQL rebuild')
            if consumer['container_id'] not in restored:
                self.action(consumer['container_id'],'start');restored.add(consumer['container_id'])
        deadline=min(self.deadline,time.monotonic()+self.c['replay_recovery_seconds'])
        while True:
            snapshot=self.database.snapshot(self.c['wallet'],self.c['pool'],self.c['marker'])
            try:
                accounting=persistence.assert_accounting(snapshot,self.c['wallet'],self.c['token'])
                cursors=[self.nats('72h-recovered-cursor-'+str(index),['consumer','info',consumer['stream'],consumer['durable']]) for index,consumer in enumerate(self.c['replay_consumers'])]
                require(all(cursor['num_ack_pending']==0 and cursor['num_pending']==0 for cursor in cursors), 'actual72hour durable replay still pending')
                break
            except persistence.PersistenceError:
                require(time.monotonic()<deadline,'actual72hour replay did not recover within bounded budget')
                time.sleep(.25)
        write(self.output/'72hour-replay-snapshot.json',snapshot)
        result={'schema_version':4,'operation':'72hour-replay-reconciled','candidate':self.c['source_sha256'],'disconnected_utc_ns':str(disconnected),'reconnected_utc_ns':str(resumed),'actual_elapsed_ns':str(resumed-disconnected),'actual_elapsed72h_proven':True,'accounting':accounting,'final_gate':'9.1','final_integrated_gate_passed':False,'capacity_qualified':False}
        write(self.output/'72hour-replay-result.json',result)
        return result

    def run_regressions(self):
        path = self.c['regression_configuration_file']
        config = load_configuration(path,self.c['regression_configuration_sha256'],self.c['source_sha256'])
        result = Journey(config,path,self.output/'isolated-regressions').run()
        require(result['phase2_qualified'] is True and result['production_qualified'] is False, 'actual isolated duplicate/PnL/receipt/candle regressions failed')
        return {'id':'isolated-regressions','passed':True,'checks':sum(v['checks'] for v in result['scenarios'])}

    def physical_restore(self):
        runtime=bound(self.c,'physical_provider_configuration')
        stopped=[]
        try:
            for item in self.topology['resources']:
                if item['role']=='app' and item.get('region')==runtime['primary_region']:
                    observed=self.resources.inspect(item['container_id'])
                    require(observed['Config']['Labels'].get('kindle.assurance.region')==runtime['primary_region'], 'quiesce exact isolated region only')
                    self.action(item['container_id'],'stop');stopped.append(item['container_id'])
            require(stopped, 'actual old-region application writers must quiesce before frozen checkpoint')
            return self._physical_restore(stopped)
        finally:
            for cid in reversed(stopped):self.action(cid,'start')

    def _physical_restore(self, stopped_apps):
        with deployment_modules() as (contracts,providers,renderer):
            c = self.c
            runtime = bound(c,'physical_provider_configuration')
            require(runtime.get('realm') == 'isolated-recovery-qualification' and runtime.get('namespace') == c['namespace'] and runtime.get('attempt') == c['attempt'], 'actual isolated physical provider runtime authority required')
            for key in ('primary','restored'):
                selected=runtime[key+'_postgres']
                service=Path(selected['service_file'])
                require(service.is_file() and not service.is_symlink() and service.stat().st_mode & 0o077 == 0 and digest(service.read_bytes())==runtime[key+'_service_sha256'], 'private bound physical PostgreSQL service authority changed')
                services=configparser.ConfigParser(interpolation=None);services.read(service)
                require(selected['service'] in services, 'actual isolated PostgreSQL service absent')
                entry=services[selected['service']]
                host=entry.get('host','')
                if host.startswith('/'):
                    require(Path(host)==Path(runtime['owned_directory'])/'restored'/'owned-socket' and key=='restored', 'restored UNIXsocket is outside exact owned destination')
                else:
                    require(ipaddress.ip_address(host).is_loopback and entry.get('port','').isdigit() and int(entry['port']) in runtime['allocated_postgres_ports'], 'physical PostgreSQL must use approved isolated loopback port')
                require(entry.get('dbname')==c['postgres']['database'] and entry.get('user')==selected['expected_role'], 'physical PostgreSQL database/principal authority differs')
            owner=self
            class CapturingTools(providers.BoundTools):
                def run(self,tool,args,**options):
                    index=len(owner.native_receipts)+1
                    started=time.time_ns()
                    try:
                        raw=super().run(tool,args,**options)
                    except providers.ProviderFailure as failure:
                        owner.native_receipts.append({'tool':tool,'tool_sha256':self.tools[tool]['sha256'],'argv':args,'started_utc_ns':str(started),'ended_utc_ns':str(time.time_ns()),'exit_code':failure.exit_code,'failed':True})
                        raise
                    name='provider-output-'+str(index)+'.bin'
                    path=owner.output/name
                    with path.open('xb') as stream:stream.write(raw)
                    os.chmod(path,0o600)
                    receipt={'tool':tool,'tool_sha256':self.tools[tool]['sha256'],'argv':args,'started_utc_ns':str(started),'ended_utc_ns':str(time.time_ns()),'exit_code':0,'raw_output_path':name,'raw_output_sha256':digest(raw)}
                    owner.native_receipts.append(receipt)
                    write(owner.output/('provider-invocation-'+str(index)+'.json'),receipt)
                    return raw
            tools = CapturingTools(runtime['tools'])
            primary = providers.PostgreSQL(tools,**runtime['primary_postgres'])
            restored = providers.PostgreSQL(tools,**runtime['restored_postgres'])
            clock = primary.document("SELECT json_build_object('server_utc_ns',((extract(epoch FROM clock_timestamp())*1000000000)::numeric(30,0))::text,'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),'timeline',(SELECT timeline_id FROM pg_control_checkpoint()),'commit_timestamps',current_setting('track_commit_timestamp'));")
            require(clock['commit_timestamps'] == 'on', 'actual transaction commit timestamps must be enabled; no timestamp substitution')
            expected = primary.document(persistence.recovery_snapshot_sql(self.tables,commit_timestamps=True))
            persistence.recovery_checksums(expected,self.tables)
            write(self.output/'checkpoint-expected.json',expected)
            checkpoint = self.control('capture_checkpoint')
            require(checkpoint.get('expected_checksums') == persistence.recovery_checksums(expected,self.tables) and checkpoint.get('postgres_system_identifier') == clock['system_identifier'], 'independent authoritative checkpoint disagrees with actual PostgreSQL')
            ownership = Path(runtime['owned_directory'])
            require(ownership.is_absolute() and ownership.resolve(strict=True) == ownership and ownership.is_dir() and ownership.stat().st_mode & 0o077 == 0 and ownership.name == c['namespace'], 'private exact namespace owned physical output required')
            base = ownership/'base-backup'
            backup = primary.physical_backup(base)
            write(self.output/'physical-base-backup.json',backup)
            raw_manifest=(base/'backup_manifest').read_bytes()
            require(len(raw_manifest)<=16*1024*1024 and digest(raw_manifest)==backup['manifest_sha256'], 'actual physical backup manifest differs')
            with (self.output/'postgres-backup-manifest.json').open('xb') as stream:stream.write(raw_manifest)
            os.chmod(self.output/'postgres-backup-manifest.json',0o600)
            target = primary.document("SELECT json_build_object('target_time',to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'),'target_utc_ns',((extract(epoch FROM clock_timestamp())*1000000000)::numeric(30,0))::text);")
            primary.query('SELECT pg_switch_wal(); CHECKPOINT;')
            write(self.output/'pitr-target.json',target)
            fenced = self.control('recovery_fence')
            require(fenced.get('old_primary_write_sqlstate') == '55000' and fenced.get('old_epoch_publish_rejected') is True and fenced.get('old_epoch_ack_rejected') is True and fenced.get('writable_regions') == 1, 'real old-writer/broker epoch fence missing')
            victims = []
            restoration = None
            loss_utc = time.time_ns(); loss_mono = time.monotonic_ns()
            try:
                for item in self.topology['resources']:
                    if item.get('region') == runtime['primary_region']:
                        observed = self.resources.inspect(item['container_id'])
                        require(observed['Config']['Labels'].get('kindle.assurance.region') == runtime['primary_region'], 'actual old-region resource label differs')
                        if item['container_id'] not in stopped_apps:
                            self.action(item['container_id'],'stop');victims.append(item['container_id'])
                require(victims and {self.resources.permits[v].role for v in victims+stopped_apps} >= {'app','postgres','nats'}, 'actual old-region loss must include application, database and broker')
                restoration = primary.restore_physical(base,ownership/'restored',target_time=target['target_time'],timeline=clock['timeline'],port=runtime['restore_port'],wal_command=runtime['wal_command'],expected_system_id=clock['system_identifier'],expected_manifest_sha256=backup['manifest_sha256'])
                write(self.output/'physical-restoration.json',restoration)
                deadline = min(self.deadline,time.monotonic()+300)
                while True:
                    actual_state = restored.document("SELECT json_build_object('in_recovery',pg_is_in_recovery(),'paused',pg_is_wal_replay_paused(),'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),'last_replayed_utc_ns',((extract(epoch FROM pg_last_xact_replay_timestamp())*1000000000)::numeric(30,0))::text);")
                    if actual_state['paused'] is True:break
                    require(time.monotonic()<deadline,'actual PITR target was not reached and paused')
                    time.sleep(.25)
                require(actual_state['in_recovery'] is True and actual_state['system_identifier'] == clock['system_identifier'], 'physical restored identity or pause differs')
                actual = restored.document(persistence.recovery_snapshot_sql(self.tables,commit_timestamps=False))
                write(self.output/'checkpoint-restored.json',actual)
                verified = persistence.assert_restored_checkpoint(expected,actual,self.tables)
                write(self.output/'checkpoint-reconciliation.json',verified)
                probe = self.control('recovery_login_read_write')
                require(probe.get('actual_login') is True and probe.get('actual_read') is True and probe.get('actual_write') is True and probe.get('browser_trace_path') and probe.get('new_epoch_persisted') is True and probe.get('production_changed') is False, 'real isolated recovered login/read/write and durable epoch proof absent')
                verified_utc = time.time_ns(); verified_mono = time.monotonic_ns()
                latest = max(int(row['committed_utc_ns']) for rows in expected.values() for row in rows)
                timestamps = recovery_timestamps(loss_utc_ns=loss_utc,loss_monotonic_ns=loss_mono,verified_utc_ns=verified_utc,verified_monotonic_ns=verified_mono,last_acknowledged_utc_ns=latest,last_restored_utc_ns=int(verified['last_restored_commit_utc_ns']))
                timestamps['postgres_system_identifier'] = clock['system_identifier']
                timestamps['timeline'] = clock['timeline']
                timestamps['actual_pitr_target_utc_ns'] = target['target_utc_ns']
                timestamps['acknowledged_rows_lost'] = verified['acknowledged_rows_lost']
                write(self.output/'recovery-timestamps.json',timestamps)
                return timestamps
            finally:
                if restoration is not None:
                    require(Path(restoration['directory']).parent == ownership, 'restored process no longer belongs to exact namespace')
                    tools.run('pg_ctl',['--pgdata='+restoration['directory'],'--wait','--timeout=30','stop'],timeout=45)
                elif (ownership/'restored'/'postmaster.pid').is_file():
                    target=ownership/'restored'
                    require(target.resolve()==target and target.parent==ownership, 'interrupted restored instance ownership differs')
                    control=tools.run('pg_controldata',[str(target)]).decode()
                    identifier=re.search(r'^Database system identifier:\s+([0-9]+)\s*$',control,re.M)
                    require(identifier and identifier.group(1)==clock['system_identifier'], 'interrupted physical instance belongs to another database')
                    tools.run('pg_ctl',['--pgdata='+str(target),'--wait','--timeout=30','stop'],timeout=45)
                for cid in reversed(victims):self.action(cid,'start')

    def run(self):
        capture,calibration,workload = shared_modules()
        reports = bound(self.c,'generator_calibration')
        measured = calibration.evaluate(reports,candidate=self.c['source_sha256'],topology=self.c['binding']['topology'],generator=self.c['generator_sha256'])
        require(measured['generator_calibrated'], 'actual independent generator/clock calibration failed: '+','.join(measured['failures']))
        require(digest(canonical(reports)) == self.c['clock_calibration_sha256'], 'actual clock calibration authority differs')
        write(self.output/'generator-calibration.json',{'reports':reports,'evaluation':measured})
        write(self.output/'workload-ledger.json',workload.ledger())
        write(self.output/'root-mix.json',workload.MIX)
        budget = workload.provisioned_budget(bound(self.c,'provisioned_budget'))
        write(self.output/'provisioned-budget.json',budget)
        scenarios = [self.run_regressions()]
        replay = self.replay()
        scenarios.append({'id':'durable-replay-calibration','passed':True,'checks':len(self.c['replay_consumers'])+1})
        self.physical_restore()
        scenarios += [{'id':'physical-pitr-checkpoint','passed':True,'checks':len(self.tables)}, {'id':'recovery-journey-timestamps','passed':True,'checks':4}]
        require({v['id'] for v in scenarios} == SCENARIOS, 'isolated recovery scenario inventory differs')
        finished_ns = time.time_ns()
        raw_artifacts = {p.relative_to(self.output).as_posix():digest(p.read_bytes()) for p in sorted(self.output.rglob('*')) if p.is_file() and '.private' not in p.relative_to(self.output).parts}
        result = {'schema_version':4,'task':'4.49','scope':'isolated_component','binding':self.c['binding'],'attempt':self.c['attempt'],'namespace':self.c['namespace'],'generator_sha256':self.c['generator_sha256'],'started_utc_ns':str(self.started_ns),'ended_utc_ns':str(finished_ns),'scenarios':scenarios,'executions':self.receipts,'provider_invocations':self.native_receipts,'resource_actions':self.action_receipts,'artifacts':raw_artifacts,'phase4_isolated_proof':True,'actual_elapsed72h_proven':False,'final_gate':'9.1','final_integrated_gate_passed':False,'production_qualified':False,'capacity_qualified':False,'disaster_recovery_qualified':False}
        write(self.output/'isolated-recovery-result.json',result)
        artifacts = [{'name':p.name,'path':p.name,'sha256':digest(p.read_bytes()),'kind':'calibration' if p.name=='generator-calibration.json' else 'native_provider'} for p in sorted(self.output.glob('*.json'))]
        manifest = {'schema_version':1,'capture_version':'kindle-shared-capture-v1','run_id':self.c['attempt'],'scope':'isolated_component','final_gate':'9.1','bindings':{'candidate':self.c['source_sha256'],'interface':self.c['binding']['interface'],'topology':self.c['binding']['topology'],'generator':self.c['generator_sha256']},'workload_version':workload.VERSION,'root_capacity_ledger_sha256':self.c['root_capacity_ledger_sha256'],'root_mix_sha256':self.c['root_mix_sha256'],'scenario_manifest_sha256':self.c['scenario_manifest_sha256'],'started_utc_ns':str(self.started_ns),'ended_utc_ns':str(finished_ns),'artifact_root':str(self.output),'artifacts':artifacts,'generator_hosts':[r['host_id'] for r in reports],'gateway_replica_ids':self.c['gateway_replica_ids'],'required_lane_ids':['L49'],'native_runtime_authority_sha256':self.c['native_runtime_authority_sha256'],'clock_calibration_sha256':self.c['clock_calibration_sha256'],'capacity_qualified':False,'monthly_availability_qualified':False}
        capture.validate_record('CaptureManifest',manifest)
        from evidence_policy import validate_phase4
        validate_phase4(self.output,result,self.c['binding'],self.started_ns)
        write(self.output/'capture-manifest.json',manifest)
        return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config',required=True)
    parser.add_argument('--config-sha256',required=True)
    parser.add_argument('--source-sha256',required=True)
    parser.add_argument('--output-dir',required=True)
    parser.add_argument('--mode',choices=('isolated','arm72h','resume72h'),default='isolated')
    parser.add_argument('--journal')
    parser.add_argument('--journal-sha256')
    args = parser.parse_args()
    c = load_configuration4(args.config,args.config_sha256,args.source_sha256)
    harness=Phase4Recovery(c,args.config,args.output_dir)
    if args.mode=='arm72h':result=harness.arm72h()
    elif args.mode=='resume72h':
        require(args.journal and args.journal_sha256, 'actual bound72hour journal required')
        result=harness.resume72h(args.journal,args.journal_sha256)
    else:result=harness.run()
    print(json.dumps({'task':'4.49','isolated_proof':result.get('phase4_isolated_proof',False),'operation':result.get('operation','isolated-proof'),'final_gate':'9.1','final_integrated_gate_passed':False,'capacity_qualified':False}))


if __name__ == '__main__':
    main()
