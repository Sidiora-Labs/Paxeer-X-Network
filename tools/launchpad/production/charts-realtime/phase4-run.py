from __future__ import annotations

import argparse
import asyncio
from collections import Counter, defaultdict
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time
import sqlite3
import tempfile

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/production/load'))
from phase2_feeder import PostgresObserver, owned_command
from phase2_types import Bindings, bound_document, canonical, digest, unique, validate_v2

HEX = re.compile(r'^[0-9a-f]{64}$')
TOPIC = re.compile(r'^market\.candles\.(0x[0-9a-f]{40})\.(1m|5m|15m|1h|4h|1d|1w)$')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def integer(value, minimum=0):
    require(type(value) is int and value >= minimum, 'Exact nonnegative integer measurement required')
    return value


def stamp(value):
    require(type(value) in (str, int) and re.fullmatch(r'[1-9][0-9]{0,29}', str(value)), 'Exact measured UTC timestamp required')
    return int(value)


def hash_file(path):
    hasher = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1048576), b''):
            hasher.update(chunk)
    return hasher.hexdigest()


def bound_path(reference, maximum):
    path = Path(reference['path'])
    require(path.is_absolute() and not path.is_symlink() and path.is_file(), 'Immutable absolute artifact path required')
    require(path.stat().st_size <= maximum and HEX.fullmatch(reference['sha256']), 'Raw capture bound or hash missing')
    require(hash_file(path) == reference['sha256'], 'Immutable raw capture digest differs')
    return path


def rows(reference, maximum):
    path = bound_path(reference, maximum)
    with path.open('rb') as source:
        for line in source:
            require(len(line) <= 1048576 and line.endswith(b'\n'), 'Bounded complete raw capture row required')
            value = json.loads(line, object_pairs_hook=unique)
            require(type(value) is dict, 'Observed object row required')
            yield value


def semantic_candle(value):
    validate_v2('Candle', value)
    return digest(json.dumps({k: v for k, v in value.items() if k != 'snapshotId'}, sort_keys=True, separators=(',', ':'), allow_nan=False).encode())


def source_manifest(reference):
    manifest = bound_document(reference['path'], reference['sha256'], maximum_bytes=67108864)
    require(type(manifest.get('files')) is dict and manifest['files'], 'Source manifest must bind actual file hashes')
    for relative, expected in manifest['files'].items():
        path = ROOT / relative
        require(not Path(relative).is_absolute() and '..' not in Path(relative).parts and not path.is_symlink(), 'Exact source-relative path required')
        require(HEX.fullmatch(expected) and hash_file(path) == expected, 'Current candidate source differs')
    return manifest


def configuration(reference, mode):
    config = bound_document(reference['path'], reference['sha256'])
    from jsonschema import Draft202012Validator
    schema = json.loads((Path(__file__).parent / 'phase4-config.schema.json').read_text())
    Draft202012Validator(schema).validate(config)
    if mode == 'release':
        require(all(name in config for name in ('shared_manifest', 'browser_captures', 'clock_calibrations', 'expected_ledger')), 'Required final performance/browser evidence absent')
    Bindings(**config['bindings'])
    current_source = source_manifest(config['candidate_manifest'])
    candidate_digest = digest(json.dumps(current_source['files'], sort_keys=True, separators=(',', ':')).encode())
    require(config['bindings']['candidate'] == candidate_digest, 'Current candidate source manifest binding differs')
    authoritative_candidate = os.environ.get('QUALIFICATION_CANDIDATE_SHA256')
    if authoritative_candidate is not None:
        require(authoritative_candidate == candidate_digest, 'Qualification candidate differs from chart measurement source')
    for name in ('shared_schema', 'interface_manifest', 'topology_manifest', 'metric_mapping'):
        bound_path(config[name], 67108864)
    require(config['bindings']['interface'] == config['interface_manifest']['sha256'], 'Interface binding differs')
    require(config['bindings']['topology'] == config['topology_manifest']['sha256'], 'Topology binding differs')
    harness_files = sorted(Path(__file__).parent.glob('phase4-*')) + [ROOT / 'client/apps/web/src/widgets/trading/udf-datafeed.ts']
    harness_digest = digest(json.dumps({str(p.relative_to(ROOT)): hash_file(p) for p in harness_files if p.is_file()}, sort_keys=True, separators=(',', ':')).encode())
    require(config['harness_sha256'] == harness_digest, 'Current chart measurement harness binding differs')
    return config


async def prepare(config):
    browser = bound_document(config['browser_configuration']['path'], config['browser_configuration']['sha256'])
    require(browser['bindings'] == config['bindings'] and browser['run_id'] == config['run_id'], 'Actual browser configuration binding differs')
    require(browser['pool_address'] in config['pools'], 'Browser pool outside admitted shared pool inventory')
    for name in ('playwright_entry', 'browser_executable', 'storage_state'):
        bound_path(browser[name], 67108864 if name != 'browser_executable' else 1073741824)
    observer = PostgresObserver(**config['postgres'], pools=config['pools'])
    readiness = await observer.readiness()
    topology = bound_document(config['topology_manifest']['path'], config['topology_manifest']['sha256'])
    mapping = bound_document(config['metric_mapping']['path'], config['metric_mapping']['sha256'])
    require(topology.get('replicas') and type(topology['replicas']) is list, 'Actual replica topology/sizes required')
    require(type(mapping.get('metrics')) is dict and mapping['metrics'], 'Source-bound native telemetry mapping required')
    authority = bound_document(mapping['authority']['path'], mapping['authority']['sha256'])
    require(authority['bindings'] == config['bindings'] and authority['boundary'] == 'edge_socket_write', 'Native authority differs from chart candidate/shared boundary')
    for field, value in mapping['metrics'].items():
        require(authority['metrics'][field] == value['exporter_name'], 'Mapped metric differs from native shared exporter authority')
        require(type(value) is dict and 'source_path' in value and 'source_sha256' in value and 'exporter_name' in value, 'Actual native exporter mapping required')
        relative = Path(value['source_path'])
        require(not relative.is_absolute() and '..' not in relative.parts and relative.suffix in ('.go', '.py', '.ts', '.mjs'), 'Exact authored exporter source path required')
        source = ROOT / relative
        require(source.is_file() and not source.is_symlink() and hash_file(source) == value['source_sha256'], 'Native metric source binding differs')
        require(value['exporter_name'] in source.read_text(), 'Metric exporter name absent from bound actual source')
    return {'harness_inputs_ready': True, 'postgres': readiness, 'capacity_qualified': False, 'final_capacity_task': '9.1'}


async def capture(config):
    await prepare(config)
    executable = bound_path(config['node'], 1073741824)
    browser = bound_document(config['browser_configuration']['path'], config['browser_configuration']['sha256'])
    raw = await owned_command([str(executable), str(Path(__file__).parent / 'phase4-browser.mjs'), config['browser_configuration']['path'], config['browser_configuration']['sha256']], environment=dict(os.environ), timeout=browser['duration_seconds'] + 120, maximum_bytes=1048576)
    return json.loads(raw, object_pairs_hook=unique)


def browser_evidence(config, shared, faults):
    expected = {}
    for row in rows(config['expected_ledger'], config['raw_artifact_max_bytes']):
        key = (row['receiver_id'], row['event_id'])
        require(key not in expected and TOPIC.fullmatch(row['topic']), 'Distinct expected chart browser delivery required')
        shared_validate(config, 'ExpectedBrowserDelivery', row)
        require(row['run_id'] == config['run_id'] and row['source_manifest_sha256'] == config['candidate_manifest']['sha256'], 'Expected delivery binding differs')
        require(len(expected) < 1000000, 'Expected browser denominator exceeds declared bounded million-sample inventory')
        require(stamp(row['scheduled_utc_ns']) <= stamp(row['deadline_utc_ns']), 'Expected browser sampling deadline invalid')
        row = dict(row)
        row['healthy'] = not any(start <= stamp(row['scheduled_utc_ns']) <= recovery for start, end, recovery in faults)
        expected[key] = row
    require(expected, 'Actual browser expected-delivery denominator missing')
    clocks = {}
    for reference in config['clock_calibrations']:
        clock = bound_document(reference['path'], reference['sha256'])
        require(clock['run_id'] == config['run_id'] and clock['bindings'] == config['bindings'], 'Clock calibration binding differs')
        require(type(clock['offset_ns']) is int and 0 <= integer(clock['uncertainty_ns']) <= 5000000, 'Independently calibrated clock uncertainty<=5ms required')
        require(clock['receiver_id'] not in clocks, 'Duplicate receiver clock calibration')
        clocks[clock['receiver_id']] = clock
    accepted, states, comparisons, spans = {}, {}, [], {}
    histogram = Counter()
    source_ids = set()
    for reference in config['browser_captures']:
        for row in rows(reference, config['raw_artifact_max_bytes']):
            require(row['bindings'] == config['bindings'] and row['run_id'] == config['run_id'], 'Browser observation binding differs')
            receiver = row['receiver_id']
            if row['kind'] == 'candle_delivery':
                event = validate_v2('CandleEvent', row['event'])
                key = (receiver, event['eventId'])
                require(key in expected, 'Browser delivery absent from measured sampling ledger')
                require(key not in accepted, 'Duplicate browser sample would bias latency denominator')
                require(event['topic'] == expected[key]['topic'] and TOPIC.fullmatch(event['topic']), 'Cross-pool browser candle')
                match = TOPIC.fullmatch(event['topic'])
                require(event['payload']['poolAddress'].lower() == match.group(1) and event['payload']['interval'] == match.group(2), 'Cross-pool or interval candle payload')
                require(event['watermark'] is not None and event['watermark']['integrity'] == 'healthy', 'Sampled candle watermark unavailable')
                require(event['revision'] == event['payload']['revision'] and row['source_event_id'] == event['payload']['close']['source']['sourceEventId'], 'Chart delivery revision/source identity differs')
                require(row['source_event_id'] == expected[key]['source_event_id'], 'Sampled browser source identity differs from expected boundary')
                require(stamp(row['received_utc_ns']) <= stamp(expected[key]['deadline_utc_ns']), 'Browser sample missed independently declared delivery deadline')
                source_ids.add(row['source_event_id']); accepted[key] = row
            elif row['kind'] == 'replay_state':
                require(0 <= integer(row['pending_bytes']) <= 256 * 1024 and TOPIC.fullmatch(row['topic']), 'Unbounded/cross-pool browser replay state')
                require(not (row['state'] == 'live' and row['replay_complete'] is not True), 'Live browser with unresolved replay gap')
                states[(receiver, row['topic'])] = row
            elif row['kind'] == 'end_state_comparison':
                require(TOPIC.fullmatch(row['topic']), 'End-state topic scope invalid')
                require(semantic_candle(row['browser_payload']) == semantic_candle(row['authoritative_payload']), 'Browser/REST canonical candle digest differs')
                require(row['authoritative_request_id'], 'Authoritative REST request identity missing')
                comparisons.append({'receiver_id': receiver, 'topic': row['topic'], 'digest': semantic_candle(row['browser_payload']), 'request_id': row['authoritative_request_id']})
            elif row['kind'] == 'browser_started':
                require(receiver not in spans, 'Duplicate browser lifecycle start')
                spans[receiver] = {'start': stamp(row['started_utc_ns'])}
            elif row['kind'] == 'browser_finished':
                require(receiver in spans and 'end' not in spans[receiver], 'Browser finish without unique actual start')
                spans[receiver]['end'] = stamp(row['ended_utc_ns'])
                require(HEX.fullmatch(row['screenshot_sha256']) and HEX.fullmatch(row['trace_sha256']), 'Required browser visual artifacts missing')
                capture_parent = Path(reference['path']).parent
                require(hash_file(capture_parent / 'chart-end-state.png') == row['screenshot_sha256'] and hash_file(capture_parent / 'chart-browser-trace.zip') == row['trace_sha256'], 'Actual browser visual artifact digest differs')
            elif row['kind'] != 'candle_state':
                raise ValueError('Unknown chart browser capture kind')
    require(comparisons and spans and states, 'Required browser correctness/lifecycle/replay capture missing')
    require(all('end' in span and span['end'] - span['start'] >= 86400 * 10**9 for span in spans.values()), 'End-state sample requires actual24h browser soak')
    require(all(row['replay_complete'] is True and row['pending_bytes'] == 0 for row in states.values()), 'Unresolved final revision gaps')
    healthy_expected = sum(row['healthy'] is True for row in expected.values())
    healthy_received = sum(expected[key]['healthy'] is True for key in accepted)
    require(set(spans) <= set(clocks), 'Browser receiver lacks independently calibrated clock')
    for receiver, span in spans.items():
        require(any(comparison['receiver_id'] == receiver for comparison in comparisons), 'Soaked browser receiver has no end-state digest sample')
    require(healthy_received == healthy_expected and set(accepted) == set(expected), 'Required browser delivery missing; tail samples stay in denominator')
    return expected, accepted, clocks, comparisons, histogram, source_ids


async def chart_latency(config, expected, accepted, clocks, source_ids):
    observer = PostgresObserver(**config['postgres'], pools=config['pools'])
    await observer.readiness()
    commits = {}
    after = None
    for _ in range(200000):
        page = await observer.committed(after=after)
        for row in page:
            if row['event_id'] in source_ids:
                commits[row['event_id']] = row
        if len(page) < 5000:
            break
        after = page[-1]['event_id']
    else:
        raise ValueError('Durable commit observation pagination budget exceeded')
    require(set(commits) == source_ids, 'Chart source fill lacks actual durable commit authority')
    bins = Counter()
    for key, row in accepted.items():
        clock = clocks[row['receiver_id']]
        received = stamp(row['received_utc_ns'])
        require(stamp(clock['valid_from_utc_ns']) <= received <= stamp(clock['valid_to_utc_ns']), 'Browser sample outside independently calibrated clock interval')
        committed = commits[row['source_event_id']]
        require(committed['timestamp_authority'] == 'postgres_transaction_commit', 'Raw durable transaction commit timestamp required')
        latency = received - clock['offset_ns'] - stamp(committed['durable_commit_utc_ns'])
        require(latency >= -clock['uncertainty_ns'], 'Browser delivery predates committed source beyond clock bound')
        if expected[key]['healthy']:
            bins[(max(0, latency) + clock['uncertainty_ns'] + 999999) // 1000000] += 1
    count = sum(bins.values()); require(count > 0, 'Healthy chart latency samples absent')
    percentiles = {}
    for fraction in (.5, .95, .99, .999, 1):
        threshold = math.ceil(count * fraction); seen = 0
        for bucket, frequency in sorted(bins.items()):
            seen += frequency
            if seen >= threshold:
                percentiles[str(fraction)] = bucket * 1000000; break
    require(percentiles['0.99'] <= 1000000000, 'Healthy durable-ingest-to-browser p99 exceeds1000ms')
    return {'denominator': count, 'missing': 0, 'latency_ns_upper_bound': percentiles, 'histogram_ms_upper_bound': dict(sorted(bins.items())), 'timestamp_authority': 'postgres_transaction_commit', 'source_commits': list(commits.values())}


def shared_validate(config, name, value):
    from jsonschema import Draft202012Validator
    definitions = bound_document(config['shared_schema']['path'], config['shared_schema']['sha256'])
    Draft202012Validator({**definitions, '$ref': '#/$defs/' + name}).validate(value)
    return value


class NativeSamples:
    def __init__(self, output):
        self.database = None
        self.directory = tempfile.TemporaryDirectory(prefix='native-index-', dir=output)
        self.database = sqlite3.connect(str(Path(self.directory.name) / 'samples.sqlite'))
        self.database.execute('PRAGMA cache_size=-32768')
        self.database.execute('CREATE TABLE samples(replica TEXT, digest TEXT, payload TEXT, PRIMARY KEY(replica,digest))')
        self.count = 0

    def add(self, key, sample):
        encoded = canonical(sample).decode()
        old = self.database.execute('SELECT payload FROM samples WHERE replica=? AND digest=?', key).fetchone()
        if old is not None:
            require(old[0] == encoded, 'Native sample identity conflict')
            return
        require(self.count < 2000000, 'Native interval inventory exceeds bounded sample budget')
        self.database.execute('INSERT INTO samples VALUES(?,?,?)', (*key, encoded))
        self.count += 1

    def get(self, key):
        row = self.database.execute('SELECT payload FROM samples WHERE replica=? AND digest=?', key).fetchone()
        return None if row is None else json.loads(row[0], object_pairs_hook=unique)

    def close(self):
        if self.database is not None:
            self.database.close(); self.database = None
            self.directory.cleanup()

    def __del__(self):
        self.close()


def shared_capture(config):
    from capture import read_manifest, gateway_summary, iter_artifact
    require(config['shared_schema']['path'] == str(ROOT / 'tools/production/load/final-shared-capture.schema.json'), 'Actual shared capture interface required')
    manifest = read_manifest(config['shared_manifest']['path'], config['shared_manifest']['sha256'], bindings=Bindings(**config['bindings']))
    require(manifest['run_id'] == config['run_id'] and manifest['scope'] == 'final_shared', 'Isolated evidence cannot substitute for final shared run')
    require('L12' in manifest['required_lane_ids'] and manifest['final_gate'] == '9.1', 'Chart lane must participate in final shared gate')
    require(stamp(manifest['ended_utc_ns']) - stamp(manifest['started_utc_ns']) >= 86400 * 10**9, 'Actual shared24h peak soak absent')
    mapping = bound_document(config['metric_mapping']['path'], config['metric_mapping']['sha256'])
    require(manifest['native_runtime_authority_sha256'] == mapping['authority']['sha256'], 'Actual shared metric authority differs')
    summary = gateway_summary(manifest)
    final_lag = {}
    largest_lag = 0
    raw_native = NativeSamples(config['output'])
    for native in iter_artifact(manifest, 'native_provider'):
        if 'before' not in native or 'after' not in native:
            continue
        for stage in ('before', 'after'):
            sample = native[stage]
            fingerprint = digest(canonical(sample))
            key = (native['replica_id'], fingerprint)
            raw_native.add(key, sample)
    for observed in iter_artifact(manifest, 'gateway_egress', record='GatewayInterval'):
        from capture import GatewaySampler
        left_key = (observed['replica_id'], observed['native_sample_before_sha256'])
        right_key = (observed['replica_id'], observed['native_sample_after_sha256'])
        left, right = raw_native.get(left_key), raw_native.get(right_key)
        require(left is not None and right is not None, 'Gateway interval lacks bound native before/after samples')
        require(str(left['observed_utc_ns']) == observed['interval_start_utc_ns'] and str(right['observed_utc_ns']) == observed['interval_end_utc_ns'], 'Native raw observation time differs from gateway interval')
        for field, right_value in right['values'].items():
            measured = right_value - left['values'][field] if field in GatewaySampler.COUNTERS else right_value
            if field in ('source_cursor', 'durable_watermark'):
                measured = str(measured)
            require(observed[field] == measured, 'Gateway frame/byte/resource math differs from actual native sample')
        require(observed['metrics_authority_sha256'] == mapping['authority']['sha256'], 'Gateway metric observation authority differs')
        final_lag[observed['replica_id']] = observed['oldest_consumer_lag_ns']
        largest_lag = max(largest_lag, observed['oldest_consumer_lag_ns'])
    require(final_lag and all(value == 0 for value in final_lag.values()), 'Source backlog never recovered by end of shared capture')
    seconds = {int(key): row for key, row in summary['seconds'].items()}
    require(set(seconds) == set(range(86400)) or (len(seconds) > 86400 and set(range(max(seconds) + 1)) == set(seconds)), 'Shared gateway interval capture has silent gaps')
    totals = summary['totals']
    require(totals['chart_delivered_frames'] > 0 and totals['chart_delivered_frames'] <= totals['delivered_frames'], 'Chart share absent or counted independently from global frames')
    require(totals['payload_bytes'] <= 400 * totals['delivered_frames'], 'Measured global mean application payload exceeds400-byte target')
    require(totals['network_bytes'] > totals['tls_bytes'] > totals['websocket_bytes'] > totals['payload_bytes'], 'Actual transport overhead evidence unavailable')
    faults, replica_losses, reconnects = [], [], []
    for row in iter_artifact(manifest, 'fault_control', record='FaultInterval'):
        require(row['run_id'] == manifest['run_id'] and row['outcome'] == 'recovered', 'Native fault not recovered under same run')
        start, end, recovery = (stamp(row[name]) for name in ('observed_start_utc_ns', 'observed_end_utc_ns', 'recovery_utc_ns'))
        require(stamp(manifest['started_utc_ns']) <= start <= end <= recovery <= stamp(manifest['ended_utc_ns']), 'Fault/recovery interval outside actual shared capture')
        faults.append((start, end, recovery))
        if row['action'] == 'replica_loss':
            require(len(row['affected_replica_ids']) == 1, 'Chart scenario requires one actual replica loss')
            second = (start - stamp(manifest['started_utc_ns'])) // 10**9
            require(seconds[second]['connections'] >= 200000 or (second > 0 and seconds[second - 1]['connections'] >= 200000), 'Replica loss did not overlap actual200k socket surge')
            replica_losses.append(row)
        if row['action'] == 'reconnect':
            reconnects.append(row)
    require(replica_losses and reconnects, 'Actual replica-loss/reconnect-storm captures missing')
    surge_run = surge_longest = burst_run = burst_longest = 0
    peak_seconds = 0
    for second, row in sorted(seconds.items()):
        require(row['connections'] >= 100000, 'Shared peak below actual100k accepted sockets')
        require(row['subscriptions'] == 5 * row['connections'], 'Actual shared mean5 topics not observed')
        require(row['slow_readers'] * 100 >= row['connections'], 'Actual one-percent stalled reader cohort absent')
        require(row['chart_frames'] > 0, 'Chart traffic absent from shared mixed workload interval')
        if row['connections'] >= 200000:
            surge_run += 1; surge_longest = max(surge_longest, surge_run)
        else:
            surge_run = 0; peak_seconds += 1
        if row['frames'] >= 1000000:
            burst_run += 1; burst_longest = max(burst_longest, burst_run)
        else:
            burst_run = 0
    require(surge_longest >= 600, 'Actual200k socket surge was not sustained ten minutes')
    steady = [row for row in seconds.values() if row['connections'] < 200000]
    require(steady and min(row['frames'] for row in steady) >= 250000, 'Global shared250kframes/s steady workload was not actually delivered')
    require(peak_seconds > 0 and burst_longest > 0, 'Measured steady peak or global1m frame burst missing')
    arrivals = defaultdict(set)
    all_source_ids = set()
    for row in iter_artifact(manifest, 'source_commits', record='SourceCommit'):
        event_id = row['event_id']; require(event_id not in all_source_ids, 'Source burst identities double counted')
        all_source_ids.add(event_id)
        second = (stamp(row['durable_commit_utc_ns']) - stamp(manifest['started_utc_ns'])) // 10**9
        if second >= 0:
            arrivals[second].add(event_id)
    source_run = source_longest = 0
    for second in range(max(seconds) + 1):
        if len(arrivals[second]) >= 10000:
            source_run += 1; source_longest = max(source_longest, source_run)
        else:
            source_run = 0
    require(source_longest >= 300, 'Actual source10000events/s five-minute burst missing')
    require(totals['source_rejected_events'] == 0, 'Source ingest failures must remain explicit')
    manifest_digests = {item['sha256'] for item in manifest['artifacts']}
    require(config['expected_ledger']['sha256'] in manifest_digests, 'Expected browser denominator not bound into shared manifest')
    require(all(item['sha256'] in manifest_digests for item in config['browser_captures']), 'Chart browser captures absent from final shared manifest')
    raw_native.close()
    return manifest, summary, faults, {'boundary': 'edge_socket_write', 'chart_frame_share': totals['chart_delivered_frames'] / totals['delivered_frames'], 'global_mean_payload_bytes': totals['payload_bytes'] / totals['delivered_frames'], 'chart_mean_payload_bytes': totals['chart_payload_bytes'] / totals['chart_delivered_frames'], 'websocket_overhead_bytes': totals['websocket_bytes'] - totals['payload_bytes'], 'tls_overhead_bytes': totals['tls_bytes'] - totals['websocket_bytes'], 'network_overhead_bytes': totals['network_bytes'] - totals['tls_bytes'], 'max_source_backlog_ns': largest_lag, 'final_source_backlog_ns': final_lag, 'surge_seconds_contiguous': surge_longest, 'global_burst_seconds_contiguous': burst_longest, 'source_burst_seconds_contiguous': source_longest, 'replica_loss': replica_losses, 'reconnect_storm': reconnects, 'totals': totals}


async def release(config):
    await prepare(config)
    manifest, gateway, faults, accounting = shared_capture(config)
    expected, accepted, clocks, comparisons, histogram, source_ids = browser_evidence(config, manifest, faults)
    latency = await chart_latency(config, expected, accepted, clocks, source_ids)
    return {'chart_measurement_passed': True, 'capacity_qualified': False, 'final_capacity_task': '9.1', 'shared_manifest_sha256': config['shared_manifest']['sha256'], 'bindings': config['bindings'], 'run_id': config['run_id'], 'frame_egress_accounting': accounting, 'latency': latency, 'browser_correctness_samples': comparisons, 'pending_replay_gaps': 0, 'qualification_boundary': 'Chart lane measurements only; final shared acceptance belongs to9.1'}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--mode', choices=('prepare', 'capture', 'release'), required=True)
    parser.add_argument('--config', required=True)
    parser.add_argument('--config-sha256', required=True)
    args = parser.parse_args()
    result = {'task': '4.12', 'mode': args.mode, 'capacity_qualified': False, 'passed': False}
    try:
        config = configuration({'path': args.config, 'sha256': args.config_sha256}, args.mode)
        output = Path(config['output']).resolve()
        require(output.is_relative_to((ROOT / 'evidence/production-system/L12/phase4').resolve()) and not output.exists(), 'Fresh chart-owned measurement output required')
        output.mkdir(parents=True, mode=0o700)
        callback = {'prepare': prepare, 'capture': capture, 'release': release}[args.mode]
        result.update(asyncio.run(callback(config)))
        result['passed'] = True
        result['chart_harness_sha256'] = config['harness_sha256']
        result['configuration_sha256'] = args.config_sha256
        target = output / 'result.json'
        target.write_text(json.dumps(result, indent=2, allow_nan=False) + '\n')
        print(json.dumps({'task': '4.12', 'mode': args.mode, 'passed': True, 'capacity_qualified': False, 'result': str(target), 'sha256': hash_file(target)}))
        return 0
    except Exception as error:
        result['error'] = type(error).__name__
        result['observed'] = str(error)[:512] if type(error) is ValueError else 'Bound schema or actual provider operation failed; credential-bearing input values are not retained'
        if 'output' in locals() and output.exists():
            target = output / 'result.json'
            if not target.exists():
                target.write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps({'task': '4.12', 'mode': args.mode, 'passed': False, 'capacity_qualified': False, 'error': type(error).__name__}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
