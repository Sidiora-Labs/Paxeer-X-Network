from __future__ import annotations

import hashlib
import hmac
import json
import math
from pathlib import Path
import time
import sqlite3
from urllib.parse import quote

from contracts import canonical, digest, inventory, load, number, require, REQUIREMENTS, sha, endpoint
from runtime import prometheus_samples


class Evidence:
    def __init__(self, path, binding, config, attestation_key, now=None):
        require(isinstance(attestation_key,bytes) and len(attestation_key)>=32, 'evidence verifier key unavailable')
        supplied=Path(path).absolute()
        require(all(not component.is_symlink() for component in (supplied,*supplied.parents)), 'evidence manifest symlink denied')
        self.path=supplied.resolve();self.root=self.path.parent;self.binding=binding;self.config=config
        self.document=load(self.path);document=self.document
        keys=('version','candidate_sha256','input_interface_sha256','deployment_digest','configuration_revision','started_at','ended_at','artifacts','provider_inventory','scenario_artifacts','native_validation','restore','database_effects','signature')
        inventory.exact(document,keys+(('phase3',) if document.get('version')=='3.0' else ()))
        require(document['version'] in ('2.0','3.0'),'evidence version')
        for key in ('candidate_sha256','input_interface_sha256','deployment_digest','configuration_revision'):
            require(document[key]==binding[key], 'evidence identity mismatch')
        current=time.time() if now is None else now
        number(document['started_at'],1,current);number(document['ended_at'],document['started_at']+1,current)
        require(current-document['ended_at']<=1800,'evidence expired')
        unsigned={key:value for key,value in document.items() if key!='signature'}
        expected=hmac.new(attestation_key,canonical(unsigned),hashlib.sha256).hexdigest()
        require(isinstance(document['signature'],str) and hmac.compare_digest(document['signature'],expected),'untrusted scenario attestation')
        require(set(document['scenario_artifacts'])==set(REQUIREMENTS),'all twelve scenario inputs required')
        require(isinstance(document['provider_inventory'],dict) and document['provider_inventory'], 'native provider inventory missing')
        self.files={}
        require(isinstance(document['artifacts'],dict) and document['artifacts'],'no native artifacts')
        for name,metadata in document['artifacts'].items():
            require(isinstance(name,str) and not Path(name).is_absolute() and '..' not in Path(name).parts,'artifact path escapes evidence')
            inventory.exact(metadata,('sha256','provider','observed_at','kind'))
            digest(metadata['sha256']);number(metadata['observed_at'],document['started_at'],document['ended_at'])
            require(metadata['provider'] in document['provider_inventory'],'unowned artifact provider')
            require(metadata['kind'] in ('origin_log','metrics','postgres','rpc','broker','socket','collector','alertmanager','resource','invoice','operator','fault_control'),'non-native evidence kind')
            artifact=self.root/name
            require(not artifact.is_symlink() and artifact.is_file() and artifact.resolve().is_relative_to(self.root),'artifact unavailable')
            hasher=hashlib.sha256()
            with artifact.open('rb') as stream:
                for block in iter(lambda:stream.read(1024*1024),b''):hasher.update(block)
            require(hasher.hexdigest()==metadata['sha256'],'native artifact changed')
            self.files[name]=artifact
        validation=self.read(document['native_validation'],'operator')
        require(validation.get('passed') is True and len(validation.get('checks',[]))==3,'native configuration validation missing')
        for key in ('candidate_sha256','input_interface_sha256','deployment_digest','configuration_revision'):
            require(validation.get(key)==binding[key],'native build identity differs')
        require(validation.get('binding_sha256')==sha(canonical(binding)) and validation.get('configuration_sha256')==sha(canonical(config)),'native validated configuration differs')
        templates=validation.get('source_templates')
        require(isinstance(templates,dict) and set(templates)=={'prometheus-rules.json','otel-collector.yaml'},'native validation template inventory differs')
        for name,expected_hash in templates.items():
            require(sha((Path(__file__).resolve().parents[1]/name).read_bytes())==expected_hash,'native validated source template differs')
        require(set(validation.get('tool_hashes',{}))=={'collector','promtool'},'native validation tool inventory differs')
        for value in validation['tool_hashes'].values():digest(value)
        for check in validation['checks']:
            require(type(check.get('exit_code')) is int and check['exit_code']==0 and check.get('command'),'native configuration validation failed')
            digest(check.get('log_sha256'))
            require(any(meta['sha256']==check['log_sha256'] for meta in document['artifacts'].values()),'native validation log unavailable')
        number(validation.get('checked_at'),document['started_at'],document['ended_at'])
        require(document['restore']['completed'] is True and document['restore']['reconciled'] is True and document['restore']['artifact'] in self.files,'failed or unproven restore')
        effects=document['database_effects'];require(isinstance(effects,list) and effects,'database reconciliation missing')
        seen=set()
        for effect in effects:
            inventory.exact(effect,('identity','expected','actual','artifact'))
            require(isinstance(effect['identity'],str) and effect['identity'] and effect['identity'] not in seen and type(effect['expected']) is int and type(effect['actual']) is int and effect['expected']>=0 and effect['expected']==effect['actual'] and effect['artifact'] in self.files,'duplicate or divergent database effects')
            seen.add(effect['identity'])

    def read(self, name, kind=None):
        require(name in self.files,'missing scenario artifact')
        if kind:require(self.document['artifacts'][name]['kind']==kind,'wrong native artifact kind')
        return load(self.files[name],64*1024*1024)

    def scenario(self, requirement):
        return self.read(self.document['scenario_artifacts'][requirement])

    def metric_delta(self, before_name, after_name, metric):
        def series(name):
            require(self.document['artifacts'][name]['kind']=='metrics','counter source must be native metrics')
            samples=prometheus_samples(self.files[name].read_bytes(),self.config)
            values={labels:v for (n,labels),v in samples.items() if n==metric}
            require(values,'required metric absent')
            return values
        before,after=series(before_name),series(after_name)
        require(set(before)==set(after),'counter series changed during scenario')
        require(all(after[labels]>=value for labels,value in before.items()),'counter reset during scenario')
        return sum(after[labels]-value for labels,value in before.items())


def reads(e,s):
    inventory.exact(s,('access_log','metrics_before','metrics_after','denominator','metric_timeline'))
    require(s['denominator']==1000000,'one million read denominator required')
    path=e.files[s['access_log']];require(e.document['artifacts'][s['access_log']]['kind']=='origin_log','origin log source required')
    totals={'eligible':0,'good':0,'fast150':0,'fast400':0};failures=slow=0;durations=[]
    with path.open() as stream:
        for line in stream:
            row=json.loads(line)
            inventory.exact(row,('timestamp','route','status','duration_seconds','exclusion'))
            number(row['timestamp'],e.document['started_at'],e.document['ended_at'])
            require(row['route'] in inventory.ROUTES,'unbounded origin route')
            observed=inventory.eligible_read(row['status'],row['duration_seconds'],row['exclusion'])
            for key,value in observed.items():totals[key]+=value
            if observed['eligible']:
                failures+=row['status']>=500;slow+=row['duration_seconds']>.4;durations.append(row['duration_seconds'])
    require(totals['eligible']==1000000 and failures>0 and slow>0,'read scenario injection absent')
    for key,name in [('eligible','sli_read_eligible_requests_total'),('good','sli_read_good_requests_total'),('fast150','sli_read_fast150_requests_total'),('fast400','sli_read_fast400_requests_total')]:
        require(e.metric_delta(s['metrics_before'],s['metrics_after'],name)==totals[key],'origin counter reconstruction mismatch')
    durations.sort()
    return {'counts':totals,'p95_seconds':durations[math.ceil(.95*len(durations))-1],'p99_seconds':durations[math.ceil(.99*len(durations))-1]}


def mutations(e,s):
    rows=e.read(s['transactions'],'postgres');require(rows,'mutation records missing')
    failed=[r for r in rows if r['validated'] is True and r['commit_error'] is True]
    require(failed,'commit failure injection absent')
    for row in failed:
        require(row['authoritative_commit'] is False and row['outbox_commit'] is False and row['acknowledged'] is False and row['response_status']>=500,'failed commit claimed success')
    for row in rows:
        require(type(row['commit_error']) is bool and type(row['response_status']) is int and 100<=row['response_status']<=599, 'mutation outcome invalid')
        inventory.mutation_good(row['validated'],row['authoritative_commit'],row['outbox_commit'],row['acknowledged'])
    good=sum(inventory.mutation_good(r['validated'],r['authoritative_commit'],r['outbox_commit'],r['acknowledged']) for r in rows)
    require(e.metric_delta(s['metrics_before'],s['metrics_after'],'sli_mutation_good_requests_total')==good,'mutation good counter differs')
    require(e.metric_delta(s['metrics_before'],s['metrics_after'],'sli_mutation_eligible_requests_total')==sum(r['validated'] for r in rows),'mutation denominator differs')
    return {'failed_commits':len(failed),'good':good}


def quotes(e,s):
    rows=e.read(s['requests'],'origin_log');require(rows,'quote workload absent')
    durations=[];impaired=0
    for row in rows:
        require(row['dependency']=='healthy','healthy RPC impairment scenario absent')
        for key in ('duration_seconds','queue_seconds','dependency_seconds'):number(row[key])
        require(row['duration_seconds']>=row['queue_seconds']+row['dependency_seconds'],'quote stages exceed origin duration')
        if row['dependency_seconds']>=.9:
            impaired+=1;require(row['duration_seconds']>.75 and row['fast'] is False,'slow quote classified fast')
        durations.append(row['duration_seconds'])
    durations.sort();p99=durations[math.ceil(.99*len(durations))-1]
    require(impaired and p99>.75,'quote p99 impairment unobserved')
    alerts=e.read(s['alerts'],'alertmanager');require(any(a['name']=='quote_latency' and a['state']=='firing' for a in alerts),'quote alert absent')
    require(s['peak_rps']>=2000 and s['burst_rps']>=4000,'quote capacity evidence absent')
    return {'p99_seconds':p99,'impaired':impaired}


def delivery(e,s):
    row=e.read(s['trace'],'socket')
    require(row['chain_id']==125 and row['event_identity'] and row['browser_ack_id'],'committed event/browser ACK absent')
    require(row['durable_publish_ack'] is True and row['projection_commit_ack'] is True,'delivery path not durable')
    intervals=inventory.delivery_intervals(row['committed_ms'],row['ingested_ms'],row['published_ms'],row['browser_ack_ms'])
    require(abs(sum(intervals['seconds'].values())-intervals['total_seconds'])<1e-9,'trace interval sum differs')
    return intervals


def cardinality(e,s):
    require(e.document['artifacts'][s['requests']]['kind']=='origin_log','pool workload origin logs missing')
    pools=set();count=0
    with e.files[s['requests']].open() as stream:
        for line in stream:
            row=json.loads(line);pool=row['pool']
            require(isinstance(pool,str) and len(pool)==42 and pool.startswith('0x'),'invalid workload pool')
            int(pool[2:],16);pools.add(pool.lower());count+=1
    require(count==1000000 and len(pools)==1000000,'million distinct pool workload absent')
    samples=prometheus_samples(e.files[s['series']].read_bytes(),e.config)
    grouped={}
    for (_,labels),_value in samples.items():
        labels=dict(labels);identity=tuple(labels.get(k) for k in ('service','environment','zone'))
        grouped[identity]=grouped.get(identity,0)+1
    require(grouped and max(grouped.values())<=20000,'metric cardinality exceeded')
    require(e.metric_delta(s['metrics_before'],s['metrics_after'],'telemetry_dropped_labels_total')>0,'unsafe label dropping unobserved')
    return {'series_per_identity':list(grouped.values())}


def privacy(e,s):
    marker=e.read(s['marker'],'fault_control')['marker'];require(isinstance(marker,str) and len(marker)>=24,'privacy canary missing')
    exports=s['exports'];require(isinstance(exports,list) and exports,'native collector exports missing')
    kinds=set()
    for name in exports:
        require(e.document['artifacts'][name]['kind']=='collector','export must come from native collector')
        raw=e.files[name].read_bytes();require(marker.encode() not in raw,'private marker exported')
        document=json.loads(raw);kinds.add(document['kind'])
        def contains_marker(value):
            if isinstance(value,str):return marker in value
            if isinstance(value,list):return any(contains_marker(item) for item in value)
            if isinstance(value,dict):return any(contains_marker(key) or contains_marker(item) for key,item in value.items())
            return False
        require(not contains_marker(document),'encoded private marker exported')
        require(document['safe_stage_count']>0 and document['safe_error_count']>0,'safe trace diagnosis lost')
    require(kinds=={'logs','spans','metrics'},'incomplete export privacy observation')
    resource=e.read(s['resources'],'resource')
    require(0<resource['memory_limit_bytes']<=512*1024*1024 and resource['routine_sample_percent']==1 and resource['business_commits_blocked']==0,'sampling or backpressure boundary violated')
    return {'export_kinds':sorted(kinds),'memory_limit_bytes':resource['memory_limit_bytes']}


def burn(e,s):
    windows=e.read(s['windows'],'metrics');require(inventory.burn(windows,.9995)=='critical','multiwindow burn threshold absent')
    alerts=e.read(s['alerts'],'alertmanager')
    matches=[a for a in alerts if a['name']=='read_burn_critical' and a['state']=='firing']
    require(matches,'critical burn alert absent')
    for alert in matches:
        require(alert['deployment_digest']==e.binding['deployment_digest'] and alert['denominator']>=100 and alert['route'] in inventory.ROUTES and alert['runbook']=='runbooks/availability.md','alert diagnosis incomplete')
    return {'critical_alerts':len(matches)}


def storage(e,s):
    rows=e.read(s['timeline'],'broker');require(rows,'storage telemetry absent')
    for row in rows:
        number(row['capacity_bytes'],1);number(row['used_bytes'],0,row['capacity_bytes']);number(row['oldest_consumer_seconds'])
        for key in ('growth_bytes_per_second','growth_horizon_seconds','retained_records_before','retained_records_after','replay_rps'):number(row[key])
        if row['spare_capacity_rps'] is not None:number(row['spare_capacity_rps'])
    warning=[r for r in rows if .8<=r['used_bytes']/r['capacity_bytes']<.9 and r['oldest_consumer_seconds']>60]
    critical=[r for r in rows if r['used_bytes']/r['capacity_bytes']>=.9]
    require(warning and critical,'storage boundary scenario absent')
    for row in warning:
        require(row['warning'] is True and row['growth_bytes_per_second']>0 and row['growth_horizon_seconds']>0,'growth horizon absent')
        require(math.isclose(row['growth_horizon_seconds'],(row['capacity_bytes']-row['used_bytes'])/row['growth_bytes_per_second'],rel_tol=.01),'growth horizon does not reconstruct')
    for row in critical:require(row['critical'] is True and row['unsafe_admission_allowed'] is False,'storage safety admission not enforced')
    for row in rows:
        require(row['retained_records_before']==row['retained_records_after'],'durable records lost')
        require(row['replay_rps']==0 if row['spare_capacity_rps'] is None else row['replay_rps']<.25*row['spare_capacity_rps'],'unsafe replay allocation')
    return {'warning_samples':len(warning),'critical_samples':len(critical)}


def integrity(e,s):
    nodes=e.read(s['nodes'],'rpc');require(len(nodes)==2 and nodes[0]['height']==nodes[1]['height'] and nodes[0]['block_hash']!=nodes[1]['block_hash'],'committed node divergence absent')
    require(all(n['chain_id']==125 and n['healthy'] is True for n in nodes),'node integrity scenario differs')
    controls=e.read(s['controls'],'fault_control')
    require(controls['quotes_allowed'] is False and controls['market_submissions_allowed'] is False and controls['reason']=='integrity','integrity control failed open')
    require(type(controls['historical_height']) is int and controls['historical_age_seconds']>=0,'historical diagnosis absent')
    return {'verified_height':controls['historical_height'],'quarantined':True}


def synthetics(e,s):
    rows=e.read(s['observations'],'origin_log');journeys={r['journey'] for r in rows}
    require(journeys==set(inventory.ROUTES[:5]),'synthetic inventory incomplete')
    failed=[r for r in rows if r['route_disabled'] is True]
    require(failed and all(r['health_status']==200 and r['success']==0 and 0<=r['alert_at']-r['disabled_at']<=120 and r['alert_journey']==r['journey'] for r in failed),'green-health dead-route incident missed')
    require(all(r['economic_writes']==0 and r['timeout_seconds']<=5 and r['interval_seconds']<=30 for r in rows),'unsafe synthetic workload')
    return {'journeys':sorted(journeys),'failed_route_alerts':len(failed)}


def capacity(e,s):
    resource=e.read(s['resources'],'resource');invoice=e.read(s['invoice'],'invoice')
    inventory.capacity_evidence(resource)
    require(all(value>0 for value in resource['measured'].values()),'measured capacity demand absent')
    require(resource['deployment_digest']==e.binding['deployment_digest'],'capacity release differs')
    require(resource['started_at']==e.document['started_at'] and resource['ended_at']==e.document['ended_at'],'capacity interval differs')
    for path,expected in resource['artifacts'].items():require(path in e.files and sha(e.files[path].read_bytes())==expected,'capacity artifact unavailable')
    for key in ('reads','events','peak_sockets','video_viewer_seconds'):number(resource['workload'][key],1)
    require(set(invoice)==set(resource['invoice_units']) and invoice==resource['invoice_units'],'invoice units differ')
    workload=resource['workload']
    return {'invoice_units':invoice,'denominators':{'million_reads':workload['reads']/1e6,'million_events':workload['events']/1e6,'thousand_sockets':workload['peak_sockets']/1e3,'video_viewer_hour':workload['video_viewer_seconds']/3600},'guaranteed_price':False}


def ownership(e,s):
    rehearsals=e.read(s['rehearsals'],'operator');mapped={r['alert']:r for r in rehearsals}
    require(len(mapped)==len(rehearsals),'duplicate incident rehearsal')
    for alert in e.config['alerts']:
        if alert['severity']!='critical':continue
        require(alert['name'] in mapped,'critical rehearsal absent')
        row=mapped[alert['name']]
        require(row['responder']==e.binding['responders'][alert['owner']] and row['commander']==e.binding['responders'][alert['commander']],'unowned critical alert')
        number(row['fired_at'],e.document['started_at'],e.document['ended_at']);number(row['acknowledged_at'],row['fired_at'],e.document['ended_at']);number(row['max_update_gap_seconds'],0,900)
        require(0<=row['acknowledged_at']-row['fired_at']<=300 and row['max_update_gap_seconds']<=900 and row['runbook']==alert['runbook'],'incident timing or runbook violated')
    rejected=e.read(s['unowned_attempt'],'operator')
    require(rejected['state']=='rejected' and rejected['reason']=='unowned_alert','unowned alert activation accepted')
    return {'critical_rehearsals':len(mapped)}


CHECKS=dict(zip(REQUIREMENTS,(reads,mutations,quotes,delivery,cardinality,privacy,burn,storage,integrity,synthetics,capacity,ownership)))
TIMELINES=dict(zip(REQUIREMENTS,('sli_read_good_requests_total','sli_mutation_good_requests_total','quote_duration_seconds','event_delivery_stage_duration_seconds','telemetry_dropped_labels_total','telemetry_redaction_failures_total','slo_error_budget_burn_rate','durable_storage_free_bytes','chain_integrity_incidents_total','synthetic_journey_success','capacity_cost_estimate_revision','incident_acknowledgment_seconds')))


def evaluate(evidence):
    results=[]
    for requirement,check in CHECKS.items():
        try:
            scenario=evidence.scenario(requirement)
            timeline=scenario.get('metric_timeline')
            require(timeline in evidence.files and evidence.document['artifacts'][timeline]['kind']=='metrics','timestamped metric timeline missing')
            samples=prometheus_samples(evidence.files[timeline].read_bytes(),evidence.config)
            expected=TIMELINES[requirement]
            require(any(name==expected or name in {expected+'_bucket',expected+'_sum',expected+'_count'} for name,_labels in samples),'required scenario metric absent')
            observation=check(evidence,scenario);passed=True;reason=None
        except (ValueError,KeyError,TypeError,OSError,ZeroDivisionError,OverflowError):
            observation=None;passed=False;reason='missing_or_invalid_native_scenario_evidence'
        results.append({'requirement':requirement,'criteria':[requirement+'.'+str(i) for i in range(1,6)],'passed':passed,'reason':reason,'observation':observation})
    return {'version':'2.0','candidate_sha256':evidence.binding['candidate_sha256'],'input_interface_sha256':evidence.binding['input_interface_sha256'],'deployment_digest':evidence.binding['deployment_digest'],'configuration_revision':evidence.binding['configuration_revision'],'ended_at':evidence.document['ended_at'],'evidence_sha256':sha(evidence.path.read_bytes()),'results':results,'passed':all(r['passed'] for r in results)}


def native_journal(e, name):
    require(e.document['artifacts'][name]['kind']=='operator', 'control journal provider differs')
    path=e.files[name]
    db=sqlite3.connect('file:'+quote(str(path))+'?mode=ro&immutable=1',uri=True)
    try:
        require(db.execute('PRAGMA integrity_check').fetchone()==('ok',), 'native control journal corrupt')
        settings=dict(db.execute('SELECT name,value FROM control_settings'))
        require(settings.get('authentication')=='required', 'native control authority not secured')
        rows=db.execute('SELECT sequence,revision,state,actor,at,binding_sha256,configuration_sha256,evidence_sha256,previous_sha256,record_sha256,payload FROM transitions ORDER BY sequence').fetchall()
    finally:
        db.close()
    records=[];previous='0'*64
    fields=('sequence','revision','state','actor','at','binding_sha256','configuration_sha256','evidence_sha256','previous_sha256')
    for sequence,row in enumerate(rows,1):
        payload=json.loads(row[10])
        require(row[0]==sequence and all(payload.get(key)==value for key,value in zip(fields,row[:9])) and payload['previous_sha256']==previous and sha(canonical(payload))==row[9], 'native control journal does not reconcile')
        records.append(payload);previous=row[9]
    return {'settings':settings,'transitions':records}


def authoritative_snapshot(e, name):
    kind=e.document['artifacts'][name]['kind']
    require(kind in ('postgres','broker','operator'), 'recovery needs authoritative state capture')
    if kind=='operator':
        journal=native_journal(e,name)
        return {'operator_authority':sha(canonical(journal))}
    snapshot=e.read(name)
    inventory.exact(snapshot, ('snapshot_id','rows'))
    require(isinstance(snapshot['snapshot_id'],str) and snapshot['snapshot_id'] and isinstance(snapshot['rows'],list) and snapshot['rows'], 'empty authoritative state capture')
    records={}
    for row in snapshot['rows']:
        inventory.exact(row, ('identity','value'))
        require(isinstance(row['identity'],str) and row['identity'] and row['identity'] not in records, 'duplicate authoritative recovery identity')
        records[row['identity']]=sha(canonical(row['value']))
    return records


def preserved_state(e, before, after):
    require(e.document['artifacts'][before]['observed_at']<=e.document['artifacts'][after]['observed_at'], 'recovery captures out of order')
    original=authoritative_snapshot(e,before)
    require(original==authoritative_snapshot(e,after), 'authoritative recovery state diverged')
    return sha(canonical(original))


def phase3_providers(e, phase):
    require(e.binding['environment']=='qualification', 'adversaries require isolated qualification infrastructure')
    require(isinstance(phase['namespace'],str) and inventory.SAFE.fullmatch(phase['namespace']), 'isolated namespace missing')
    roles=set()
    for provider in e.document['provider_inventory'].values():
        inventory.exact(provider, ('role','environment','namespace','endpoint'))
        require(provider['environment']=='qualification' and provider['namespace']==phase['namespace'], 'provider outside isolated rehearsal')
        require(provider['role'] in ('origin','postgres','rpc','broker','socket','collector','alertmanager','resource','invoice','operator','fault_control','prometheus'), 'native provider role unknown')
        endpoint(provider['endpoint'],'qualification')
        roles.add(provider['role'])
    require({'origin','postgres','rpc','broker','socket','collector','alertmanager','resource','invoice','operator','fault_control','prometheus'} <= roles, 'native isolated provider inventory incomplete')
    required_roles={'origin_log':'origin','metrics':'prometheus','postgres':'postgres','rpc':'rpc','broker':'broker','socket':'socket','collector':'collector','alertmanager':'alertmanager','resource':'resource','invoice':'invoice','operator':'operator','fault_control':'fault_control'}
    for artifact in e.document['artifacts'].values():
        require(e.document['provider_inventory'][artifact['provider']]['role']==required_roles[artifact['kind']], 'artifact provider role differs')


def phase3_recovery(e, phase, requirement):
    recovery=e.read(phase['recoveries'][requirement],'operator')
    inventory.exact(recovery, ('fault','before','after','incident_timeline','resources'))
    fault=e.read(recovery['fault'],'fault_control')
    inventory.exact(fault, ('namespace','environment','operation','target_provider','started_at','ended_at'))
    require(fault['namespace']==phase['namespace'] and fault['environment']=='qualification', 'fault target outside isolation')
    require(isinstance(fault['operation'],str) and inventory.SAFE.fullmatch(fault['operation']), 'fault operation missing')
    require(fault['target_provider'] in e.document['provider_inventory'], 'fault target unowned')
    number(fault['started_at'],e.document['started_at'],e.document['ended_at'])
    number(fault['ended_at'],fault['started_at'],e.document['ended_at'])
    require(e.document['artifacts'][recovery['before']]['observed_at']<=fault['started_at'] and e.document['artifacts'][recovery['after']]['observed_at']>=fault['ended_at'], 'authoritative state captures do not bound fault interval')
    state=preserved_state(e,recovery['before'],recovery['after'])
    events=e.read(recovery['incident_timeline'],'operator')
    require(isinstance(events,list) and len(events)>=3, 'incident timeline missing')
    previous=fault['started_at']
    for event in events:
        inventory.exact(event, ('at','stage','deployment_digest'))
        number(event['at'],previous,e.document['ended_at']);previous=event['at']
        require(event['stage'] in ('injected','detected','contained','restored','reconciled') and event['deployment_digest']==e.binding['deployment_digest'], 'incident timeline identity differs')
    require(events[0]['stage']=='injected' and events[-1]['stage']=='reconciled' and any(row['stage']=='restored' and row['at']>=fault['ended_at'] for row in events), 'incident recovery timeline incomplete')
    resources=e.read(recovery['resources'],'resource')
    inventory.exact(resources, ('cpu_cores','memory_bytes','disk_bytes'))
    for value in resources.values():number(value,1)
    return {'state_sha256':state,'fault_started_at':fault['started_at'],'fault_ended_at':fault['ended_at'],'reconciled_at':events[-1]['at']}


def phase3_control(e, phase):
    denied=e.read(phase['control_adversaries'],'operator')
    require(isinstance(denied,list) and denied, 'denied control actions missing')
    actions=set()
    for attempt in denied:
        inventory.exact(attempt, ('action','actor','reason','before','after','exports','at'))
        require(attempt['action'] in ('propose','validate','activate','reject') and attempt['reason']=='unauthorized', 'control denial outcome differs')
        require(isinstance(attempt['actor'],str) and attempt['actor'] not in e.binding['responders'].values(), 'denied actor is assigned authority')
        number(attempt['at'],e.document['started_at'],e.document['ended_at'])
        preserved_state(e,attempt['before'],attempt['after'])
        require(isinstance(attempt['exports'],list) and attempt['exports'], 'denial privacy exports missing')
        for name in attempt['exports']:
            exported=e.read(name,'operator')
            require(set(exported) <= {'action','decision','error_code','sequence','configuration_revision'}, 'denied action exposed authority or credentials')
            require(exported.get('decision')=='denied' and exported.get('error_code')=='unauthorized', 'denied action diagnosis missing')
        actions.add(attempt['action'])
    require(actions=={'propose','validate','activate','reject'}, 'denied action coverage incomplete')
    stale=e.read(phase['stale_actor'],'operator')
    inventory.exact(stale, ('old_epoch','current_epoch','accepted_effects','before','after'))
    require(type(stale['old_epoch']) is int and type(stale['current_epoch']) is int and 0<=stale['old_epoch']<stale['current_epoch'] and type(stale['accepted_effects']) is int and stale['accepted_effects']==0, 'stale authority not fenced')
    preserved_state(e,stale['before'],stale['after'])
    interrupted=e.read(phase['interrupted_activation'],'operator')
    inventory.exact(interrupted, ('before','after','recovered','active_binding_sha256','activation_records','bundle_sha256','journal','bundle_manifest'))
    require(interrupted['recovered'] is True and type(interrupted['activation_records']) is int and interrupted['activation_records']==1 and interrupted['active_binding_sha256']==sha(canonical(e.binding)), 'interrupted activation not reconciled')
    digest(interrupted['bundle_sha256']);preserved_state(e,interrupted['before'],interrupted['after'])
    journal=native_journal(e,interrupted['journal'])
    activations=[row for row in journal['transitions'] if row['state']=='activated' and row['revision']==e.config['revision']]
    require(len(activations)==1 and journal['transitions'][-1]==activations[0] and activations[0]['binding_sha256']==sha(canonical(e.binding)), 'native activation journal does not reconcile')
    number(activations[0]['at'],e.document['started_at'],e.document['ended_at'])
    bundle=e.read(interrupted['bundle_manifest'],'operator')
    require(sha(canonical(bundle))==interrupted['bundle_sha256']==activations[0]['render_manifest_sha256'], 'native activation bundle digest differs')
    require(bundle['binding_sha256']==sha(canonical(e.binding)) and bundle['configuration_sha256']==sha(canonical(e.config)), 'native activation bundle scope differs')
    zone=e.read(phase['zone_loss'],'fault_control')
    inventory.exact(zone, ('namespace','environment','zone','before','after','recovered','old_epoch','new_epoch','stale_acceptances','backlog_after'))
    require(zone['namespace']==phase['namespace'] and zone['environment']=='qualification' and zone['zone'] in e.config['zones'], 'zone fault outside isolation')
    require(zone['recovered'] is True and type(zone['old_epoch']) is int and type(zone['new_epoch']) is int and 0<=zone['old_epoch']<zone['new_epoch'] and type(zone['stale_acceptances']) is int and zone['stale_acceptances']==0 and type(zone['backlog_after']) is int and zone['backlog_after']==0, 'zone recovery authority or backlog diverged')
    preserved_state(e,zone['before'],zone['after'])
    restore=e.read(e.document['restore']['artifact'],'postgres')
    inventory.exact(restore, ('before','after','completed_at'))
    preserved_state(e,restore['before'],restore['after'])
    number(restore['completed_at'],e.document['started_at'],e.document['ended_at'])
    for effect in e.document['database_effects']:
        capture=e.read(effect['artifact'],'postgres')
        inventory.exact(capture, ('identity','committed_rows','outbox_rows'))
        require(capture['identity']==effect['identity'] and isinstance(capture['committed_rows'],list) and isinstance(capture['outbox_rows'],list), 'database effect capture missing')
        require(len(capture['committed_rows'])==len(capture['outbox_rows'])==effect['expected'], 'database or outbox effects do not reconcile')
        for rows in (capture['committed_rows'],capture['outbox_rows']):
            require(len({sha(canonical(row)) for row in rows})==len(rows), 'duplicate database effect persisted')
    return {'denied_actions':sorted(actions),'zone':zone['zone'],'activation_records':interrupted['activation_records']}


def evaluate_phase3(evidence):
    result=evaluate(evidence)
    result['version']='3.0';result['phase']=3
    try:
        require(evidence.document['version']=='3.0', 'phase3 native evidence required')
        phase=evidence.document['phase3']
        inventory.exact(phase, ('namespace','recoveries','control_adversaries','zone_loss','interrupted_activation','stale_actor'))
        require(isinstance(phase['recoveries'],dict) and set(phase['recoveries'])==set(REQUIREMENTS), 'all twelve recovery records required')
        phase3_providers(evidence,phase)
        result['adversaries']=phase3_control(evidence,phase)
    except (ValueError,KeyError,TypeError,OSError,ZeroDivisionError,OverflowError,sqlite3.Error):
        result['adversaries']=None
        for row in result['results']:
            row.update(passed=False,reason='missing_or_invalid_native_adversarial_evidence')
    else:
        for row in result['results']:
            try:
                recovery=phase3_recovery(evidence,phase,row['requirement'])
                scenario=evidence.scenario(row['requirement'])
                prometheus_samples(evidence.files[scenario['metric_timeline']].read_bytes(), evidence.config, (evidence.document['started_at'], evidence.document['ended_at']))
                row['recovery']=recovery
            except (ValueError,KeyError,TypeError,OSError,ZeroDivisionError,OverflowError,sqlite3.Error):
                row.update(passed=False,reason='missing_or_invalid_native_recovery_evidence')
    result['passed']=all(row['passed'] for row in result['results'])
    return result
