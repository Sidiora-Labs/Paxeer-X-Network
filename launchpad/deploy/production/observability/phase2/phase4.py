from __future__ import annotations

import argparse
from collections import defaultdict
import dataclasses
import hashlib
import json
import math
import os
from pathlib import Path
import sys
import time

from contracts import canonical, digest, inventory, load, number, require, REQUIREMENTS, safe_diagnostics, sha, validate_binding
from control import read_key
from evidence import Evidence, evaluate_phase3
from probes import run_once_concurrent
from render import atomic_write, safe_directory
from runtime import COUNTERS, prometheus_samples, scrape_concurrent

ROOT=Path(__file__).resolve().parents[4]
LOAD=ROOT/'tools/production/load'
METADATA_LIMIT=64*1024*1024
sys.path.insert(0,str(LOAD))
from capture import read_manifest, iter_artifact, gateway_summary
from phase2_types import Bindings, bound_document, source_digest
from workload import ledger, provisioned_budget
from jsonschema import Draft202012Validator, ValidationError


def source_sha256():
    root=Path(__file__).resolve().parents[1]
    files={str(path.relative_to(root)):sha(path.read_bytes()) for path in sorted(root.rglob('*')) if path.is_file() and path.suffix in ('.py','.json','.yaml','.yml') and '__pycache__' not in path.parts}
    return sha(canonical(files))


def bound(path, expected, limit=4*1024*1024):
    digest(expected)
    path=Path(path).absolute()
    require(all(not component.is_symlink() for component in (path,*path.parents)), 'bound input symlink denied')
    require(path.is_file() and path.stat().st_size<=limit and sha(path.read_bytes())==expected, 'bound input unavailable or changed')
    return load(path,limit)


def validate_plan(plan,binding,config):
    validate_binding(binding,config)
    Draft202012Validator(load(Path(__file__).resolve().parents[1]/'config-v4.schema.json')).validate(plan)
    inventory.exact(plan,('version','binding_sha256','configuration_sha256','source_sha256','d16_bindings','run_id','namespace','sample_interval_seconds','duration_seconds','max_capture_bytes','budget_path','budget_sha256'))
    require(plan['version']=='4.0' and binding['environment']=='qualification', 'isolated measurement plan required')
    require(plan['binding_sha256']==sha(canonical(binding)) and plan['configuration_sha256']==sha(canonical(config)) and plan['source_sha256']==source_sha256(), 'measurement plan differs from current source or configuration')
    inventory.exact(plan['d16_bindings'],('candidate','interface','topology','generator'))
    bindings=Bindings(**plan['d16_bindings'])
    require(bindings.candidate==binding['candidate_sha256'] and bindings.interface==binding['input_interface_sha256'] and bindings.generator==source_digest(), 'D16 candidate/interface/generator differs')
    require(isinstance(plan['run_id'],str) and 1<=len(plan['run_id'])<=256, 'measurement run identity missing')
    require(isinstance(plan['namespace'],str) and inventory.SAFE.fullmatch(plan['namespace']), 'measurement isolation namespace missing')
    require(type(plan['sample_interval_seconds']) is int and plan['sample_interval_seconds']==30, 'bounded probe interval required')
    require(type(plan['duration_seconds']) is int and 60<=plan['duration_seconds']<=86400 and plan['duration_seconds']%30==0, 'genuine bounded measurement duration required')
    require(type(plan['max_capture_bytes']) is int and 1<=plan['max_capture_bytes']<=2**53, 'approved capture byte budget required')
    budget=bound(plan['budget_path'],plan['budget_sha256'])
    allocations=validate_budget(budget,bindings,config)
    require(plan['max_capture_bytes']+2*METADATA_LIMIT<=sum(row['disk_bytes'] for row in allocations.values()), 'capture and metadata budgets exceed admitted disk allocation')
    return bindings,budget,allocations


def validate_budget(budget,bindings,config):
    inventory.exact(budget,('version','bindings','limits','retention_seconds','cost','allocations'))
    require(budget['version']=='4.0' and budget['bindings']==dataclasses.asdict(bindings), 'allocation topology differs')
    result=provisioned_budget(budget)
    require(result['final_run_permitted'], 'storage/network/operating budget insufficient')
    require(isinstance(budget['allocations'],list) and budget['allocations'], 'actual allocation inventory unavailable')
    allocations={}
    for row in budget['allocations']:
        inventory.exact(row,('service','instance','zone','cpu_cores','memory_bytes','disk_bytes','provider_revision'))
        require(row['service'] in inventory.SERVICES and row['zone'] in config['zones'], 'allocation outside admitted inventory')
        require(isinstance(row['instance'],str) and 1<=len(row['instance'])<=128 and row['instance'] not in allocations, 'duplicate or absent allocated instance')
        require(isinstance(row['provider_revision'],str) and row['provider_revision'], 'allocation provider revision missing')
        for field in ('cpu_cores','memory_bytes','disk_bytes'):
            number(row[field]);require(row[field]>0, 'positive actual allocation required')
        for field in ('memory_bytes','disk_bytes'):require(type(row[field]) is int, 'exact allocation bytes required')
        allocations[row['instance']]=row
    require({row['service'] for row in allocations.values()}==set(inventory.SERVICES), 'twelve-role allocation inventory incomplete')
    return allocations


def collect(plan,binding,config,output,root):
    validate_plan(plan,binding,config)
    output=safe_directory(output)
    require(not output.exists(), 'capture output must be a fresh private directory')
    output.mkdir(parents=True,mode=0o700)
    expected=plan['duration_seconds']//30+1
    manifest={'version':'4.0','binding_sha256':plan['binding_sha256'],'configuration_sha256':plan['configuration_sha256'],'source_sha256':plan['source_sha256'],'plan_sha256':sha(canonical(plan)),'run_id':plan['run_id'],'started_at':time.time(),'ended_at':None,'expected_cycles':expected,'completed_cycles':0,'cycles':[],'failures':[],'artifacts':{},'capacity_qualified':False,'monthly_availability_qualified':False,'final_gate':'9.1'}
    consumed=0;start=time.monotonic()
    def checkpoint(name):
        raw=canonical(manifest)
        require(len(raw)<=METADATA_LIMIT, 'capture metadata budget exhausted')
        atomic_write(output/name,raw)
    def persist(name,raw,metadata):
        nonlocal consumed
        require(consumed+len(raw)<=plan['max_capture_bytes'], 'approved capture byte budget exhausted')
        atomic_write(output/name,raw);consumed+=len(raw)
        manifest['artifacts'][name]={'sha256':sha(raw),'bytes':len(raw),**metadata}
    try:
        for index in range(expected):
            deadline=start+index*30
            if index:time.sleep(max(0,deadline-time.monotonic()))
            require(time.monotonic()-deadline<=5, 'measurement schedule gap exceeded')
            cycle={'index':index,'started_at':time.time(),'ended_at':None,'metrics':[],'probes':None}
            manifest['cycles'].append(cycle)
            for sample in scrape_concurrent(binding,config):
                name='cycle-%05d-%s.prom'%(index,sample['service'])
                persist(name,sample['raw'],{key:sample[key] for key in ('service','zone','observed_at','duration_seconds')} | {'kind':'metrics'})
                cycle['metrics'].append(name)
            if index<expected-1:
                probes=run_once_concurrent(binding,config,root)
                name='cycle-%05d-probes.json'%index
                persist(name,canonical(probes),{'kind':'probes','observed_at':time.time()})
                cycle['probes']=name
                if any(row['success']!=1 for row in probes):manifest['failures'].append({'cycle':index,'reason':'native_route_probe_failed'})
            cycle['ended_at']=time.time();manifest['completed_cycles']+=1
            checkpoint('capture-progress.json')
    except (ValueError,OSError,TimeoutError,KeyError,TypeError):
        manifest['failures'].append({'cycle':len(manifest['cycles'])-1,'reason':'native_capture_unavailable_or_budget_exceeded'})
    finally:
        manifest['ended_at']=time.time()
        checkpoint('measurement.json')
    return manifest


def load_measurement(path,expected,plan,binding,config):
    manifest=bound(path,expected,64*1024*1024);root=Path(path).absolute().parent
    inventory.exact(manifest,('version','binding_sha256','configuration_sha256','source_sha256','plan_sha256','run_id','started_at','ended_at','expected_cycles','completed_cycles','cycles','failures','artifacts','capacity_qualified','monthly_availability_qualified','final_gate'))
    for field in ('binding_sha256','configuration_sha256','source_sha256','run_id'):require(manifest[field]==plan[field], 'native measurement identity differs')
    require(manifest['version']=='4.0' and manifest['plan_sha256']==sha(canonical(plan)) and manifest['capacity_qualified'] is False and manifest['monthly_availability_qualified'] is False and manifest['final_gate']=='9.1', 'native measurement authority differs')
    require(manifest['expected_cycles']==plan['duration_seconds']//30+1 and manifest['completed_cycles']==manifest['expected_cycles']==len(manifest['cycles']) and not manifest['failures'], 'native capture incomplete or failed')
    number(manifest['started_at'],1,time.time());number(manifest['ended_at'],manifest['started_at']+plan['duration_seconds'],time.time())
    require(time.time()-manifest['ended_at']<=1800, 'native measurement expired')
    previous=None;deltas=defaultdict(float);seen=set();total_bytes=0
    for index,cycle in enumerate(manifest['cycles']):
        inventory.exact(cycle,('index','started_at','ended_at','metrics','probes'))
        require(cycle['index']==index and isinstance(cycle['metrics'],list) and len(cycle['metrics'])==len(binding['services']), 'native cycle inventory differs')
        number(cycle['started_at'],manifest['started_at']+index*30-5,manifest['started_at']+index*30+5)
        number(cycle['ended_at'],cycle['started_at'],cycle['started_at']+30)
        snapshot={}
        for name,service in zip(cycle['metrics'],binding['services']):
            require(isinstance(name,str) and Path(name).name==name and name not in seen, 'duplicate or escaping metric capture')
            metadata=manifest['artifacts'][name]
            inventory.exact(metadata,('sha256','bytes','service','zone','observed_at','duration_seconds','kind'))
            require(metadata['kind']=='metrics' and metadata['service']==service['service'] and metadata['zone']==service['zone'], 'metric capture provider differs')
            path=root/name;require(not path.is_symlink() and path.is_file(), 'native metric capture missing')
            require(path.stat().st_size==metadata['bytes'] and sha(path.read_bytes())==metadata['sha256'], 'native metric capture bytes changed')
            number(metadata['observed_at'],cycle['started_at'],cycle['ended_at']);number(metadata['duration_seconds'],0,5)
            parsed=prometheus_samples(path.read_bytes(),config)
            require(len(parsed)<=config['series_per_instance'], 'metric series budget exceeded')
            for (metric,labels),value in parsed.items():
                dimensions=dict(labels)
                require(dimensions.get('service')==service['service'] and dimensions.get('zone')==service['zone'] and dimensions.get('environment')==binding['environment'], 'captured metric runtime identity differs')
                require((metric,labels) not in snapshot, 'captured metric series duplicated')
                snapshot[(metric,labels)]=value
            seen.add(name);total_bytes+=metadata['bytes']
        if index<len(manifest['cycles'])-1:
            name=cycle['probes'];require(isinstance(name,str) and Path(name).name==name and name not in seen, 'native probe capture missing')
            metadata=manifest['artifacts'][name];inventory.exact(metadata,('sha256','bytes','kind','observed_at'))
            require(metadata['kind']=='probes', 'native probe capture provider differs')
            probes=bound(root/name,metadata['sha256']);require((root/name).stat().st_size==metadata['bytes'], 'native probe capture bytes changed')
            require(isinstance(probes,list) and [row['journey'] for row in probes]==list(inventory.ROUTES[:5]), 'native route probe inventory differs')
            for row in probes:
                inventory.exact(row,('journey','environment','observed_at','duration_seconds','success','error_code'))
                require(row['environment']==binding['environment'] and type(row['success']) is int and row['success']==1 and row['error_code']=='none', 'native user journey probe failed')
                number(row['observed_at'],cycle['started_at'],cycle['ended_at']);number(row['duration_seconds'],0,5)
            seen.add(name);total_bytes+=metadata['bytes']
        else:require(cycle['probes'] is None, 'unexpected final-cycle probe')
        if previous is not None:
            for key,value in counter_deltas((previous,snapshot)).items():deltas[key]+=value
        previous=snapshot
    require(set(manifest['artifacts'])==seen and total_bytes<=plan['max_capture_bytes'], 'capture artifact inventory or byte budget differs')
    return manifest,dict(deltas)


def counter_deltas(samples):
    result=defaultdict(float)
    for before,after in zip(samples,samples[1:]):
        selected={key for key in before if key[0] in COUNTERS or key[0].endswith(('_bucket','_count','_sum'))}
        require(selected=={key for key in after if key[0] in COUNTERS or key[0].endswith(('_bucket','_count','_sum'))}, 'counter/histogram identity appeared or disappeared')
        for key in selected:
            require(after[key]>=before[key], 'native series reset during measurement')
            result[key]+=after[key]-before[key]
    return dict(result)


def total(deltas,name,where=None):
    selected=[value for (metric,labels),value in deltas.items() if metric==name and all(dict(labels).get(key)==value for key,value in (where or {}).items())]
    require(selected, 'required native SLO counter absent')
    return sum(selected)


def histogram_quantile(deltas,name,fraction,where=None):
    buckets=defaultdict(float)
    for (metric,labels),value in deltas.items():
        dimensions=dict(labels)
        if metric==name+'_bucket' and all(dimensions.get(key)==value for key,value in (where or {}).items()):buckets[float(dimensions['le'])]+=value
    require(buckets and math.inf in buckets and buckets[math.inf]>=100, 'native histogram or sufficient denominator absent')
    require(all(limit>=0 for limit in buckets), 'negative native duration histogram boundary')
    denominator=total(deltas,name+'_count',where)
    require(buckets[math.inf]==denominator, 'native histogram count differs from terminal bucket')
    previous=0
    for limit,count in sorted(buckets.items()):
        require(count>=previous and count<=denominator, 'native histogram buckets are not cumulative')
        previous=count
    for limit,count in sorted(buckets.items()):
        if count>=math.ceil(fraction*denominator):return limit
    raise ValueError('native histogram quantile unavailable')


def slo_measurement(deltas):
    read=total(deltas,'sli_read_eligible_requests_total');good=total(deltas,'sli_read_good_requests_total')
    fast150=total(deltas,'sli_read_fast150_requests_total');fast400=total(deltas,'sli_read_fast400_requests_total')
    mutations=total(deltas,'sli_mutation_eligible_requests_total');mutation_good=total(deltas,'sli_mutation_good_requests_total')
    require(read>=100 and mutations>=100 and 0<=good<=read and 0<=fast150<=fast400<=read and 0<=mutation_good<=mutations, 'native SLO denominator or numerator invalid')
    quote95=histogram_quantile(deltas,'quote_duration_seconds',.95,{'dependency':'healthy'})
    quote99=histogram_quantile(deltas,'quote_duration_seconds',.99,{'dependency':'healthy'})
    ingest99=histogram_quantile(deltas,'event_delivery_stage_duration_seconds',.99,{'stage':'committed_to_ingested'})
    delivery99=histogram_quantile(deltas,'event_ingest_to_browser_duration_seconds',.99)
    observations={'read_eligible':read,'read_good':good,'read_availability':good/read,'read_fast150_fraction':fast150/read,'read_fast400_fraction':fast400/read,'mutation_eligible':mutations,'mutation_good':mutation_good,'mutation_availability':mutation_good/mutations,'quote_p95_seconds_upper_bound':quote95 if math.isfinite(quote95) else None,'quote_p99_seconds_upper_bound':quote99 if math.isfinite(quote99) else None,'ingest_p99_seconds_upper_bound':ingest99 if math.isfinite(ingest99) else None,'delivery_p99_seconds_upper_bound':delivery99 if math.isfinite(delivery99) else None}
    passed={'D14_019':good/read>=.9995 and fast150/read>=.95 and fast400/read>=.99,'D14_020':mutation_good/mutations>=.999,'D14_021':quote95<=.25 and quote99<=.75,'D14_022':ingest99<=.5 and delivery99<=1}
    return observations,passed


def resource_measurement(shared,allocations):
    seen=set();peaks={};coverage={}
    started=int(shared['started_utc_ns']);ended=int(shared['ended_utc_ns'])
    for row in iter_artifact(shared,'resource_pool'):
        instance=row['instance'];require(instance in allocations, 'resource capture outside actual allocation')
        stamp=row['observed_utc_ns'];require(type(stamp) is int and started<=stamp<=ended, 'resource capture outside shared interval')
        identity=(instance,stamp);require(identity not in seen, 'duplicate native resource capture');seen.add(identity)
        if instance in coverage:
            require(coverage[instance][1]<stamp<=coverage[instance][1]+30*10**9, 'native resource capture gap or ordering failure')
            coverage[instance][1]=stamp
        else:coverage[instance]=[stamp,stamp]
        allocation=allocations[instance];process=row['process']
        for field in ('memory_bytes','memory_limit_bytes','cpu_fraction','oom_kills_total'):
            sample=process[field];number(sample['value']);number(sample['source_timestamp'],stamp/1e9-5,stamp/1e9+5)
            require(sample['metric'].get('instance')==instance, 'resource metric instance differs')
        require(process['memory_limit_bytes']['value']==allocation['memory_bytes'] and process['memory_bytes']['value']<allocation['memory_bytes'] and process['cpu_fraction']['value']<=.7 and process['oom_kills_total']['value']==0, 'measured allocation headroom failed')
        peak=peaks.setdefault(instance,{'memory_bytes':0,'cpu_fraction':0,'allocated':allocation})
        peak['memory_bytes']=max(peak['memory_bytes'],process['memory_bytes']['value']);peak['cpu_fraction']=max(peak['cpu_fraction'],process['cpu_fraction']['value'])
    require(set(peaks)==set(allocations), 'resource measurements omit allocated runtime instances')
    require(all(first<=started+30*10**9 and last>=ended-30*10**9 for first,last in coverage.values()), 'native resource observations do not cover shared workload interval')
    return peaks


def failed_result(binding,reason):
    diagnostics=None
    if binding is not None:
        try:
            for field in ('candidate_sha256','input_interface_sha256','deployment_digest'):digest(binding[field])
            diagnostics=safe_diagnostics(binding)
        except (ValueError,KeyError,TypeError):binding=None
    identity={field:binding[field] if binding is not None else None for field in ('candidate_sha256','input_interface_sha256','deployment_digest','configuration_revision')}
    return {'version':'4.0','phase':4,**identity,'passed':False,'results':[{'requirement':requirement,'criteria':[requirement+'.'+str(i) for i in range(1,6)],'passed':False,'reason':reason,'observation':None} for requirement in REQUIREMENTS],'capacity_qualified':False,'monthly_availability_qualified':False,'production_qualified':False,'final_shared_acceptance':'deferred_to_9.1','final_gate':'9.1','diagnostics':diagnostics}


def write_result(result,output):
    output=safe_directory(output)
    for row in result['results']:atomic_write(output/(row['requirement']+'.json'),canonical({'identity':result['diagnostics'],**row}))
    atomic_write(output/'result.json',canonical(result))


def release(plan,binding,config,measurement_path,measurement_sha256,shared_path,shared_sha256,scenario_path,scenario_sha256,key,output):
    result=failed_result(binding,'missing_or_invalid_native_release_input')
    try:
        bindings,budget,allocations=validate_plan(plan,binding,config)
        measurement,deltas=load_measurement(measurement_path,measurement_sha256,plan,binding,config)
        shared=read_manifest(shared_path,shared_sha256,bindings=bindings)
        require(shared['run_id']==plan['run_id'] and 'L43' in shared['required_lane_ids'], 'shared run or lane admission differs')
        require(int(shared['started_utc_ns'])/1e9<=measurement['started_at'] and int(shared['ended_utc_ns'])/1e9>=measurement['ended_at'], 'shared capture does not cover measurement interval')
        native_hashes={row['sha256'] for row in shared['artifacts']}
        require(shared['native_runtime_authority_sha256'] in native_hashes and shared['clock_calibration_sha256'] in native_hashes, 'native shared authority/calibration capture unavailable')
        if shared['scope']=='final_shared':require(int(shared['ended_utc_ns'])-int(shared['started_utc_ns'])>=86400*10**9 and plan['duration_seconds']==86400, 'final shared peak soak is shorter than 24 hours')
        bound(scenario_path,scenario_sha256)
        native=Evidence(scenario_path,binding,config,key)
        require(native.document['phase3']['namespace']==plan['namespace'], 'native adversaries outside measurement namespace')
        scenarios=evaluate_phase3(native)
        require(scenarios['passed'], 'native failure/security/recovery scenario failed')
        gateway=gateway_summary(shared)
        whole_seconds=max(1,(int(shared['ended_utc_ns'])-int(shared['started_utc_ns']))//10**9)
        require(set(range(whole_seconds)) <= {int(second) for second in gateway['seconds']}, 'native gateway observation interval incomplete')
        require(set(shared['gateway_replica_ids']) <= set(allocations), 'gateway replica allocation missing')
        resources=resource_measurement(shared,allocations)
        observations,slo_passed=slo_measurement(deltas)
        duration=measurement['ended_at']-measurement['started_at']
        observations['healthy_quote_rps']=total(deltas,'quote_duration_seconds_count',{'dependency':'healthy'})/duration
        observations['eligible_origin_rps']=(observations['read_eligible']+observations['mutation_eligible'])/duration
        if shared['scope']=='final_shared':
            demand=ledger()
            require(observations['read_eligible']+observations['mutation_eligible']>=demand['requests_24h'] and observations['mutation_eligible']>=demand['writes_24h'] and total(deltas,'quote_duration_seconds_count',{'dependency':'healthy'})>=86400*demand['peak_mix']['quotes'], 'final shared peak origin workload absent')
            require(gateway['totals']['source_accepted_events']>=demand['events_24h'] and gateway['totals']['delivered_frames']>=demand['frames_24h'], 'final shared source/delivery workload absent')
            require(all(row['connections']>=demand['sockets_peak'] for row in gateway['seconds'].values()), 'final shared peak socket inventory absent')
        result.update(passed=True,results=scenarios['results'],measurement_sha256=measurement_sha256,shared_capture_sha256=shared_sha256,scenario_evidence_sha256=scenario_sha256,allocations_sha256=plan['budget_sha256'],run_id=plan['run_id'],scope=shared['scope'],observed_duration_seconds=measurement['ended_at']-measurement['started_at'],actual_allocations=list(allocations.values()),resource_peaks=resources,slo_observations=observations,gateway_observations=gateway['totals'],operating_budget=provisioned_budget(budget),limitations=['Finite soak observations do not qualify monthly availability.','Final shared load, restore, all burst scenarios and production acceptance require serial9.1.'])
        for row in result['results']:
            row['phase']=4;row['measurement_sha256']=measurement_sha256
            if row['requirement'] in slo_passed and not slo_passed[row['requirement']]:row.update(passed=False,reason='measured_origin_slo_target_failed')
        result['passed']=all(row['passed'] for row in result['results'])
    except (ValueError,KeyError,TypeError,OSError,IndexError,ZeroDivisionError,OverflowError,ValidationError):
        result=failed_result(binding,'missing_or_invalid_native_release_input')
    write_result(result,output)
    return result


def main():
    parser=argparse.ArgumentParser();parser.add_argument('action',choices=('collect','evaluate'));parser.add_argument('--plan',required=True);parser.add_argument('--binding',required=True);parser.add_argument('--config',required=True);parser.add_argument('--output',required=True);parser.add_argument('--root',default=str(ROOT));parser.add_argument('--measurement');parser.add_argument('--measurement-sha256');parser.add_argument('--shared-capture');parser.add_argument('--shared-capture-sha256');parser.add_argument('--scenario-evidence');parser.add_argument('--scenario-evidence-sha256');parser.add_argument('--attestation-key-file');args=parser.parse_args()
    binding=None
    try:binding=load(args.binding);config=load(args.config);plan=load(args.plan)
    except (ValueError,KeyError,TypeError,OSError,ValidationError):
        result=failed_result(binding,'missing_or_invalid_native_identity_or_plan');write_result(result,args.output)
        print(json.dumps({'phase':4,'passed':False,'reason':'missing_or_invalid_native_identity_or_plan','output':args.output}))
        raise SystemExit(1)
    if args.action=='collect':
        try:result=collect(plan,binding,config,args.output,args.root)
        except (ValueError,KeyError,TypeError,OSError,ValidationError):
            result=failed_result(binding,'missing_or_invalid_native_measurement_input');write_result(result,args.output)
        passed=result.get('completed_cycles')==result.get('expected_cycles') and 'completed_cycles' in result and not result['failures']
    else:
        try:key=read_key(args.attestation_key_file) if args.attestation_key_file else None
        except (ValueError,OSError):key=None
        result=release(plan,binding,config,args.measurement,args.measurement_sha256,args.shared_capture,args.shared_capture_sha256,args.scenario_evidence,args.scenario_evidence_sha256,key,args.output);passed=result['passed']
    print(json.dumps({'phase':4,'passed':passed,'capacity_qualified':False,'production_qualified':False,'final_gate':'9.1','output':args.output}))
    raise SystemExit(0 if passed else 1)


if __name__=='__main__':main()
