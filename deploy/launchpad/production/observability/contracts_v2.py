"""Explicit v2 telemetry inventory for the selected twelve runtime roles."""
from __future__ import annotations
import argparse, hashlib, json, math, re
from pathlib import Path

SERVICES = ('apid_rest','apid_sockets','indexerd','chartsd','statsd','pnld','rankingd','mediagatewayd','userd','sociald','metadatad','livestreamd')
LABELS = {'service','environment','zone','route','status_class','error_code','stage','dependency'}
ROUTES = ('browse','quote','wallet_challenge','metadata','websocket_replay','account_mutation','other')
STAGES = ('committed_to_ingested','ingested_to_published','published_to_browser')
ERRORS = ('none','unavailable','timeout','integrity','validation','commit_failed','unauthorized','rate_limited','other')
SAFE = re.compile(r'^[a-z][a-z0-9_.-]{0,63}$')
DIGEST = re.compile(r'^[a-f0-9]{64}$')

class Invalid(ValueError): pass

def exact(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys): raise Invalid('unknown or missing fields')

def integer(value, lo, hi):
    if type(value) is not int or not lo <= value <= hi: raise Invalid('integer out of bounds')

def validate(c):
    exact(c, ('version','revision','environment','zones','services','series_per_instance','success_sample_percent','failure_queue_mib','retention_days','exporter','probes','alerts','capacity'))
    if c['version'] != '2.0': raise Invalid('unsupported version')
    integer(c['revision'],1,2**31-1)
    if c['environment'] not in ('qualification','production'): raise Invalid('environment')
    if not isinstance(c['zones'],list) or not 1 <= len(c['zones']) <= 3 or any(not isinstance(z,str) or z not in ('zone-a','zone-b','zone-c') for z in c['zones']) or len(set(c['zones'])) != len(c['zones']): raise Invalid('zones')
    if c['services'] != list(SERVICES): raise Invalid('service inventory')
    integer(c['series_per_instance'],1,20000)
    if type(c['success_sample_percent']) is not int or type(c['failure_queue_mib']) is not int or c['success_sample_percent'] != 1 or c['failure_queue_mib'] != 512: raise Invalid('sampling bounds')
    integer(c['retention_days'],1,30)
    exact(c['exporter'],('endpoint_env','ca_env','cert_env','key_env'))
    if c['exporter'] != dict(endpoint_env='OTEL_EXPORTER_OTLP_ENDPOINT',ca_env='OTEL_CA_FILE',cert_env='OTEL_CERT_FILE',key_env='OTEL_KEY_FILE'): raise Invalid('exporter environment contract')
    if not isinstance(c['probes'],list) or len(c['probes']) != 5: raise Invalid('probe inventory')
    for p,journey in zip(c['probes'],ROUTES[:5]):
        exact(p,('journey','interval_seconds','timeout_seconds','economic_write','identity'))
        if p['journey'] != journey or p['interval_seconds'] != 30 or p['timeout_seconds'] != 5 or p['economic_write'] is not False or p['identity'] != 'nonprivileged-synthetic': raise Invalid('probe boundary')
    if not isinstance(c['alerts'],list) or len(c['alerts']) != 15: raise Invalid('alert inventory bounds')
    names=set()
    for a in c['alerts']:
        exact(a,('name','severity','owner','commander','runbook','ack_seconds','update_seconds'))
        if not isinstance(a['name'],str) or a['name'] in names or not SAFE.fullmatch(a['name']): raise Invalid('alert name')
        names.add(a['name'])
        if a['severity'] not in ('warning','critical') or not isinstance(a['owner'],str) or not SAFE.fullmatch(a['owner']) or not isinstance(a['commander'],str) or not SAFE.fullmatch(a['commander']) or a['ack_seconds'] != 300 or a['update_seconds'] != 900 or a['runbook'] not in ('availability','storage','integrity','synthetic','telemetry'): raise Invalid('unowned alert')
    required={'read_burn_critical','read_burn_warning','mutation_burn_critical','mutation_burn_warning','read_traffic_insufficient','mutation_traffic_insufficient','storage_warning','storage_critical','backlog_warning','rpc_integrity','synthetic_failed','quote_latency','ingest_latency','delivery_latency','telemetry_cardinality'}
    if names != required: raise Invalid('alert inventory')
    exact(c['capacity'],('status','target_dau','peak_sockets','surge_sockets','evidence'))
    if c['capacity']['status'] != 'unqualified' or c['capacity']['evidence'] is not None or c['capacity']['target_dau'] != 500000 or c['capacity']['peak_sockets'] != 100000 or c['capacity']['surge_sockets'] != 200000: raise Invalid('unproven capacity claim')
    return c

def safe_labels(labels, config):
    """Drop every unrecognized dimension; normalize values to finite vocabularies."""
    validate(config)
    result={}; dropped=0
    vocab={'service':SERVICES,'environment':(config['environment'],),'zone':config['zones'],'route':ROUTES,'status_class':('2xx','3xx','4xx','5xx'),'error_code':ERRORS,'stage':STAGES,'dependency':('healthy','degraded','unavailable')}
    for k,v in labels.items():
        if k not in LABELS or not isinstance(v,str) or v not in vocab[k]: dropped+=1
        else: result[k]=v
    return result,dropped

def sanitize_record(record):
    """Positive allowlist: arbitrary body/header/attribute content never exports."""
    safe={}
    for k in ('service','environment','zone','stage','error_code'):
        v=record.get(k)
        allowed={'service':SERVICES,'environment':('qualification','production'),'stage':STAGES,'error_code':ERRORS}.get(k)
        if isinstance(v,str) and ((allowed is not None and v in allowed) or (k=='zone' and v in ('zone-a','zone-b','zone-c'))): safe[k]=v
    for k in ('duration_seconds','timestamp_unix_seconds'):
        v=record.get(k)
        if type(v) in (int,float) and math.isfinite(v) and 0 <= v <= 2**53: safe[k]=v
    return safe

def eligible_read(status, duration, exclusion=None):
    if type(status) is not int or not 100<=status<=599 or type(duration) not in (int,float) or not math.isfinite(duration) or duration<0: raise Invalid('invalid observation')
    if exclusion not in (None,'synthetic','health','client_cancel','invalid_request'): raise Invalid('exclusion reason')
    return {'eligible':exclusion is None,'good':exclusion is None and status < 500,'fast150':exclusion is None and duration <= .150,'fast400':exclusion is None and duration <= .400}

def mutation_good(validated, authoritative_commit, outbox_commit, acknowledged):
    if any(type(x) is not bool for x in (validated,authoritative_commit,outbox_commit,acknowledged)): raise Invalid('boolean acknowledgments required')
    return validated and authoritative_commit and outbox_commit and acknowledged

def burn(windows, target):
    if target not in (.9995,.999): raise Invalid('target')
    exact(windows,('5m','1h','30m','6h'))
    rates={}; sufficient={}
    for k,v in windows.items():
        exact(v,('total','bad')); integer(v['total'],0,2**53); integer(v['bad'],0,v['total'])
        sufficient[k]=v['total'] >= 100
        rates[k]=v['bad']/v['total']/(1-target) if v['total'] else 0
    if sufficient['5m'] and sufficient['1h'] and min(rates['5m'],rates['1h']) >= 14.4-1e-9: return 'critical'
    if sufficient['30m'] and sufficient['6h'] and min(rates['30m'],rates['6h']) >= 6-1e-9: return 'warning'
    return 'healthy' if all(sufficient.values()) else 'insufficient_traffic'

def delivery_intervals(committed,ingested,published,browser):
    points=(committed,ingested,published,browser)
    if any(type(x) is not int or not 0<=x<=2**53 for x in points) or list(points)!=sorted(points): raise Invalid('invalid trace ordering or clock skew')
    intervals=dict(zip(STAGES,((ingested-committed)/1000,(published-ingested)/1000,(browser-published)/1000)))
    return {'seconds':intervals,'total_seconds':(browser-committed)/1000,'ingest_violation':ingested-committed>500,'delivery_violation':browser-ingested>1000}

def capacity_evidence(v):
    """Shape-only ingestion check; explicitly NEVER capacity/release qualification."""
    exact(v,('version','deployment_digest','started_at','ended_at','resources','workload','measured','invoice_units','retention','artifacts'))
    if v['version'] != '1.0' or not isinstance(v['deployment_digest'],str) or not DIGEST.fullmatch(v['deployment_digest']): raise Invalid('evidence identity')
    integer(v['started_at'],1,2**53); integer(v['ended_at'],v['started_at']+1,2**53)
    schemas={
      'resources':('cpu_cores','memory_bytes','disk_bytes'),
      'workload':('reads','events','peak_sockets','video_viewer_seconds'),
      'measured':('million_reads','million_events','thousand_sockets','video_viewer_hour'),
      'invoice_units':('baseline_reservation','burst_ceiling','storage_growth','cdn_egress'),
      'retention':('logs_days','traces_days','metrics_days'),
    }
    for key,fields in schemas.items():
        exact(v[key],fields)
        for value in v[key].values():
            if type(value) not in (int,float) or not math.isfinite(value) or not 0 <= value <= 2**53: raise Invalid('numeric evidence field')
    if any(value <= 0 for value in v['resources'].values()): raise Invalid('resource inventory')
    for value in v['retention'].values(): integer(value,1,30)
    if not isinstance(v['artifacts'],dict) or not v['artifacts']: raise Invalid('missing artifact bindings')
    for path,digest in v['artifacts'].items():
        if not isinstance(path,str) or not path or path.startswith('/') or '..' in Path(path).parts or not isinstance(digest,str) or not DIGEST.fullmatch(digest): raise Invalid('artifact digest')
    return v

def transition(current, action, actor, revision, validated=False, activation_evidence=None):
    if current not in ('proposed','validated','activated','rejected') or not isinstance(actor,str) or not SAFE.fullmatch(actor): raise Invalid('state/actor')
    integer(revision,1,2**31-1)
    allowed={('proposed','validate'):'validated',('proposed','reject'):'rejected',('validated','activate'):'activated',('validated','reject'):'rejected'}
    if (current,action) not in allowed or (action=='validate' and not validated): raise Invalid('invalid transition')
    if action == 'activate':
        # Unwired declarations have no native exporter privacy proof or staffed rehearsal.
        raise Invalid('activation blocked: native exporter privacy, responders and runtime rehearsal unqualified')
    return {'state':allowed[current,action],'actor':actor,'revision':revision}

def main():
    p=argparse.ArgumentParser();p.add_argument('config');a=p.parse_args()
    raw=Path(a.config).read_bytes()
    if len(raw)>65536: raise Invalid('config too large')
    def unique(pairs):
        out={}
        for k,v in pairs:
            if k in out: raise Invalid('duplicate key')
            out[k]=v
        return out
    validate(json.loads(raw,object_pairs_hook=unique));print(json.dumps({'contract_version':'2.0','validated':True,'sha256':hashlib.sha256(raw).hexdigest(),'capacity_qualified':False,'activation_qualified':False}))
if __name__=='__main__': main()
