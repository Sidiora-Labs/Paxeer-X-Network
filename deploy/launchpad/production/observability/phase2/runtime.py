from __future__ import annotations

import json
import math
import re
import ssl
import time
import http.client
from urllib import error, parse, request
from concurrent.futures import ThreadPoolExecutor

from contracts import Invalid, inventory, require

SAMPLE = re.compile(r'^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})?\s+([^\s]+)(?:\s+([0-9]+))?$')
COUNTERS = {'sli_read_eligible_requests_total','sli_read_good_requests_total','sli_read_fast150_requests_total','sli_read_fast400_requests_total','sli_mutation_eligible_requests_total','sli_mutation_good_requests_total','durable_transaction_commits_total','durable_transaction_rollbacks_total','telemetry_dropped_labels_total','telemetry_redaction_failures_total','chain_integrity_incidents_total'}
GAUGES = {'durable_storage_free_bytes','durable_storage_capacity_bytes','durable_storage_growth_bytes_per_second','durable_consumer_oldest_seconds','telemetry_active_series','capacity_cost_estimate_revision','incident_acknowledgment_seconds','slo_error_budget_burn_rate','synthetic_journey_success','synthetic_journey_expected','synthetic_journey_last_observed_timestamp_seconds'}
HISTOGRAMS = {'read_duration_seconds','quote_duration_seconds','quote_queue_duration_seconds','quote_dependency_duration_seconds','event_delivery_stage_duration_seconds','event_ingest_to_browser_duration_seconds'}
METRICS = COUNTERS | GAUGES | {name+suffix for name in HISTOGRAMS for suffix in ('_bucket','_sum','_count')}
LABEL = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\[\\"n])*)"(?:,|$)')


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise Invalid('telemetry endpoint redirect denied')


def fetch(url, timeout=5, limit=8*1024*1024, method='GET', body=None, headers=None):
    opener = request.build_opener(NoRedirect(), request.HTTPSHandler(context=ssl.create_default_context()))
    req = request.Request(url, data=body, headers=headers or {}, method=method)
    started = time.monotonic()
    try:
        response = opener.open(req, timeout=timeout)
    except error.HTTPError as response_error:
        response = response_error
    with response:
        raw = response.read(limit+1)
        require(len(raw) <= limit, 'telemetry response too large')
        return response.status, raw, time.monotonic()-started


def prometheus_samples(raw, config, interval=None):
    text = raw.decode('utf-8') if isinstance(raw, bytes) else raw
    result = {}
    for line in text.splitlines():
        if not line or line.startswith('#'):
            continue
        match = SAMPLE.fullmatch(line)
        require(match is not None, 'invalid Prometheus sample')
        name, labels_raw, value, timestamp = match.groups()
        require(name in METRICS,'metric name outside reviewed inventory')
        if interval is not None:
            require(timestamp is not None and interval[0]*1000 <= int(timestamp) <= interval[1]*1000, 'metric timestamp absent or outside scenario')
        labels = {}
        position = 0
        for label in LABEL.finditer(labels_raw or ''):
            require(label.start() == position and label[1] not in labels, 'duplicate or malformed metric label')
            labels[label[1]] = json.loads('"'+label[2]+'"')
            position = label.end()
        require(position == len(labels_raw or ''), 'malformed metric labels')
        safe, dropped = inventory.safe_labels({k:v for k,v in labels.items() if k not in ('le','journey','window')}, config)
        require(dropped == 0, 'unsafe metric dimension')
        if 'journey' in labels:
            require(name.startswith('synthetic_journey_') and labels['journey'] in inventory.ROUTES[:5],'unsafe journey label')
            safe['journey']=labels['journey']
        if 'window' in labels:
            require(name=='slo_error_budget_burn_rate' and labels['window'] in ('5m','1h','30m','6h'),'unsafe burn window label')
            safe['window']=labels['window']
        if 'le' in labels:
            require(name.endswith('_bucket') and name[:-7] in HISTOGRAMS, 'bucket label outside histogram')
            bound = float(labels['le'])
            require(math.isfinite(bound) or labels['le'] == '+Inf', 'invalid histogram bound')
            safe['le'] = labels['le']
        number = float(value)
        require(math.isfinite(number) and number >= 0, 'invalid metric value')
        if name in COUNTERS or name.endswith('_count'):
            require(number.is_integer() and number <= 2**53, 'counter observation must be exact integer')
        key = (name, tuple(sorted(safe.items())))
        require(key not in result, 'duplicate metric series')
        result[key] = number
    require(result, 'empty metric export')
    return result


def scrape(binding, config):
    captures = []
    for service in binding['services']:
        status, raw, duration = fetch(service['base_url'].rstrip('/')+'/metrics')
        require(status == 200, 'runtime metrics unavailable')
        samples = prometheus_samples(raw, config)
        for (_, labels), _value in samples.items():
            labels = dict(labels)
            require(labels.get('service') == service['service'] and labels.get('environment') == binding['environment'] and labels.get('zone') == service['zone'], 'runtime metric identity differs')
        captures.append({'service': service['service'], 'zone': service['zone'], 'observed_at': time.time(), 'duration_seconds': duration, 'raw': raw})
    return captures


def scrape_concurrent(binding, config):
    def capture(service):
        started=time.time()
        status,raw,duration=fetch_metrics(service['base_url'].rstrip('/')+'/metrics')
        require(status==200,'runtime metrics unavailable')
        samples=prometheus_samples(raw,config)
        for (_,labels),_value in samples.items():
            labels=dict(labels)
            require(labels.get('service')==service['service'] and labels.get('environment')==binding['environment'] and labels.get('zone')==service['zone'],'runtime metric identity differs')
        require(len(samples)<=config['series_per_instance'],'runtime metric cardinality exceeded')
        return {'service':service['service'],'zone':service['zone'],'started_at':started,'observed_at':time.time(),'duration_seconds':duration,'raw':raw}
    with ThreadPoolExecutor(max_workers=len(binding['services'])) as pool:
        return list(pool.map(capture,binding['services']))


def fetch_metrics(url):
    target=parse.urlsplit(url)
    require(target.scheme in ('https','http') and target.hostname and not target.username and not target.password and not target.fragment, 'invalid native metrics origin')
    connection=http.client.HTTPSConnection(target.hostname,target.port,timeout=5,context=ssl.create_default_context()) if target.scheme=='https' else http.client.HTTPConnection(target.hostname,target.port,timeout=5)
    started=time.monotonic();raw=bytearray()
    try:
        connection.request('GET',target.path or '/',headers={'Accept':'text/plain'})
        response=connection.getresponse()
        require(response.status==200,'native metrics origin response failed')
        while True:
            remaining=5-(time.monotonic()-started)
            require(remaining>0,'native metrics total deadline exceeded')
            if connection.sock is not None:connection.sock.settimeout(remaining)
            chunk=response.read1(min(65536,8*1024*1024+1-len(raw)))
            if not chunk:break
            raw.extend(chunk);require(len(raw)<=8*1024*1024,'native metric response too large')
        return response.status,bytes(raw),time.monotonic()-started
    finally:
        connection.close()


def query(binding, expression):
    query_string = parse.urlencode({'query': expression})
    status, raw, _ = fetch(binding['prometheus_url'].rstrip('/')+'/api/v1/query?'+query_string)
    require(status == 200, 'Prometheus query unavailable')
    result = json.loads(raw)
    require(result.get('status') == 'success' and isinstance(result.get('data', {}).get('result'), list), 'Prometheus result invalid')
    return result['data']['result']
