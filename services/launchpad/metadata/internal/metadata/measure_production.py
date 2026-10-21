#!/usr/bin/env python3
import argparse
from functools import lru_cache
import concurrent.futures
from decimal import Decimal
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import re
import ssl
import subprocess
import sys
import threading
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[5]
sys.path.insert(0, str(ROOT / 'tools/launchpad/production/load'))
sys.path.insert(0, str(ROOT / 'tools/launchpad/production'))
from phase2_types import Bindings, canonical, digest, unique
from workload import MIX, VERSION
from dispatcher.evidence import source_digest as candidate_digest
from qualify import interface_bundle
from calibration import linux_cpu_snapshot, cpu_fraction, nic_fraction

HEX = re.compile(r'^[a-f0-9]{64}$')
ADDRESS = re.compile(r'^0x[a-f0-9]{40}$')
MAX_RESPONSE = 6 * 1024 * 1024
SENSITIVE = {'accesstoken', 'refreshtoken', 'authorization', 'cookie', 'password', 'privatekey', 'secretaccesskey', 'signature'}


def require(condition, code):
    if not condition:
        raise ValueError(code)


def integer(value, minimum, maximum, name):
    require(type(value) is int and minimum <= value <= maximum, name)
    return value


def ordinary(path):
    path = Path(path)
    require(path.is_absolute() and path.is_file(), 'absolute_file_required')
    require(not any(p.is_symlink() for p in (path, *path.parents)), 'symlink_denied')
    return path


def file_hash(path):
    hasher = hashlib.sha256()
    with ordinary(path).open('rb') as source:
        for block in iter(lambda: source.read(1048576), b''):
            hasher.update(block)
    return hasher.hexdigest()


def reference(value, maximum=8 * 1024 * 1024):
    require(set(value) == {'path', 'sha256'} and HEX.fullmatch(value['sha256']), 'file_reference')
    path = ordinary(value['path'])
    require(path.stat().st_size <= maximum and file_hash(path) == value['sha256'], 'input_digest')
    return path


def document(value):
    return json.loads(reference(value).read_bytes(), object_pairs_hook=unique)


def redact_check(value):
    if isinstance(value, dict):
        require(not any(key.lower() in SENSITIVE for key in value), 'credential_in_capture')
        for item in value.values():
            redact_check(item)
    elif isinstance(value, list):
        for item in value:
            redact_check(item)


def checked_url(value, *, origin=False):
    u = urlsplit(value)
    require(u.scheme == 'https' and u.hostname and not u.username and not u.password and not u.fragment and not u.query, 'https_endpoint_required')
    if origin:
        require(u.path == '' and value == value.rstrip('/'), 'exact_origin_required')
    return u


class Capture:
    def __init__(self, directory, bindings):
        requested = Path(directory).absolute()
        require('..' not in requested.parts and not any(parent.is_symlink() for parent in (requested, *requested.parents)), 'capture_path_scope')
        self.directory = requested.resolve()
        require(self.directory.is_relative_to(ROOT / 'launchpad/evidence/production-system/L39/phase4'), 'lane_capture_directory')
        require(not self.directory.exists(), 'capture_already_exists')
        self.directory.mkdir(parents=True, mode=0o700)
        self.bindings = bindings
        self.lock = threading.Lock()
        self.rows = (self.directory / 'requests.ndjson').open('x')
        self.count = 0

    def append(self, row):
        redact_check(row)
        with self.lock:
            self.rows.write(canonical(row).decode() + '\n')
            self.count += 1

    def write(self, name, value):
        require(re.fullmatch(r'[a-z0-9-]+\.json', name), 'artifact_name')
        redact_check(value)
        with (self.directory / name).open('x') as target:
            target.write(canonical(value).decode() + '\n')

    def finish(self):
        self.rows.flush()
        os.fsync(self.rows.fileno())
        self.rows.close()
        self.write('artifact-sha256.json', {p.name: file_hash(p) for p in self.directory.iterdir() if p.is_file()})


def env_headers(entries):
    headers = {}
    for key, variable in entries.items():
        require(re.fullmatch(r'[A-Za-z][A-Za-z0-9-]{0,63}', key) and key.lower() not in {'host', 'content-length', 'origin'}, 'header_scope')
        require(re.fullmatch(r'[A-Z][A-Z0-9_]{0,127}', variable), 'credential_environment_reference')
        value = os.environ.get(variable)
        require(value is not None and 0 < len(value) <= 4096 and not any(c in value for c in '\r\n'), 'missing_credential_input')
        headers[key] = value
    return headers


@lru_cache(maxsize=4)
def response_validator(name):
    from jsonschema import Draft202012Validator, FormatChecker
    definitions = json.loads((ROOT / 'launchpad/spec/production-system/integration/contracts/v2/schema-definitions.json').read_bytes(), object_pairs_hook=unique)
    require(name in definitions['$defs'], 'known_response_contract')
    return Draft202012Validator({**definitions, '$ref': '#/$defs/' + name}, format_checker=FormatChecker())


def request(capture, url, category, sequence, scheduled_ns, *, method='GET', body=None, headers=None, schema=None, expected=(200,), body_sha256=None):
    parsed = checked_url(url)
    started = time.monotonic_ns()
    row = dict(category=category, sequence=sequence, scheduled_ns=scheduled_ns, started_ns=started, started_utc_ns=time.time_ns(), method=method, url=url, request_bytes=len(body or b''), request_sha256=digest(body or b''), expected_status=list(expected))
    connection = http.client.HTTPSConnection(parsed.hostname, parsed.port, timeout=10, context=ssl.create_default_context())
    value = None
    try:
        connection.request(method, parsed.path or '/', body=body, headers=headers or {})
        response = connection.getresponse()
        remaining = MAX_RESPONSE + 1
        chunks = []
        while remaining:
            elapsed = (time.monotonic_ns() - started) / 1e9
            require(elapsed < 10, 'response_deadline')
            if connection.sock is not None:
                connection.sock.settimeout(10 - elapsed)
            chunk = response.read1(min(65536, remaining))
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b''.join(chunks)
        require(len(raw) <= MAX_RESPONSE, 'response_byte_limit')
        row.update(status=response.status, response_bytes=len(raw), response_sha256=digest(raw), etag=response.getheader('ETag'), content_type=response.getheader('Content-Type'))
        require(response.status in expected, 'unexpected_http_status')
        if body_sha256 is not None:
            require(digest(raw) == body_sha256, 'cdn_content_digest')
        if schema:
            require(len(raw) <= 1048576, 'json_byte_limit')
            value = json.loads(raw, object_pairs_hook=unique)
            response_validator(schema).validate(value)
            redact_check(value)
            if schema == 'MetadataGetResponse':
                token = value['data']['token']
                require(token['address'] == parsed.path.split('/')[3] and token['sourcePosition']['chainId'] == '125', 'metadata_response_identity')
            if schema == 'AssetGetResponse':
                asset = value['data']['asset']
                require(asset['assetId'] == parsed.path.split('/')[-1] and asset['sha256'] == '0x' + digest(body) and asset['bytes'] == len(body), 'upload_response_identity')
                require((asset['state'] == 'approved') == (asset['publicUrl'] is not None), 'public_asset_approval_state')
            row['response'] = value
        row['success'] = True
    except Exception as error:
        row['success'] = False
        row['error_class'] = type(error).__name__
        if isinstance(error, ValueError):
            row['reason'] = str(error)[:80]
    finally:
        connection.close()
        row['ended_ns'] = time.monotonic_ns()
        row['latency_ns'] = row['ended_ns'] - scheduled_ns
        row['service_ns'] = row['ended_ns'] - started
        row['queue_ns'] = started - scheduled_ns
        capture.append(row)
    return row, value


def percentile(values, fraction):
    require(bool(values), 'empty_latency_population')
    return sorted(values)[math.ceil(len(values) * fraction) - 1]


def approved_probe(config, phase, capture, bindings):
    executable = reference(config['executable'], 512 * 1024 * 1024)
    require(os.access(executable, os.X_OK), 'native_probe_not_executable')
    require(config['kind'] == 'native_provider_capture' and config['owned_scope'], 'native_provider_scope')
    argv = config['argv']
    require(isinstance(argv, list) and all(isinstance(x, str) and len(x) <= 8192 and '\x00' not in x for x in argv), 'native_probe_arguments')
    environment = dict(os.environ)
    for name in config['required_environment']:
        require(os.environ.get(name), 'missing_native_provider_credential')
    input_data = canonical(dict(phase=phase, bindings=bindings, owned_scope=config['owned_scope'], observed_utc_ns=str(time.time_ns())))
    result = subprocess.run([str(executable), *argv], input=input_data, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=30, env=environment)
    require(result.returncode == 0 and 0 < len(result.stdout) <= 8 * 1024 * 1024, 'native_probe_failed')
    value = json.loads(result.stdout, object_pairs_hook=unique)
    redact_check(value)
    require(value['bindings'] == bindings and value['owned_scope'] == config['owned_scope'] and value['phase'] == phase, 'native_capture_binding')
    require(abs(time.time_ns() - integer(value['observed_utc_ns'], 1, 2**63 - 1, 'native_capture_time')) <= 30 * 10**9, 'stale_native_capture')
    capture.write('native-' + phase + '.json', value)
    return value


def check_inventory(before, after):
    for operation, old in before['acknowledged_operations'].items():
        require(operation in after['acknowledged_operations'] and old == after['acknowledged_operations'][operation], 'acknowledged_data_loss')
    for asset, old in before['approved_assets'].items():
        require(asset in after['approved_assets'] and old == after['approved_assets'][asset], 'approved_asset_lost')
    for object_key, content_hash in after['sql_object_inventory'].items():
        require(after['provider_object_inventory'].get(object_key) == content_hash, 'sql_provider_inventory_mismatch')
    require(integer(after['duplicate_external_allocations'], 0, 0, 'actual_duplicate_allocation_count') == 0, 'duplicate_external_allocation')


def check_provider(value, budget):
    costs = Decimal('0')
    provider_ids = set()
    require(re.fullmatch(r'[A-Z]{3}', budget['currency']), 'budget_currency')
    require(value['accounts'], 'provider_account_inventory_empty')
    for provider in value['accounts']:
        require(provider['account_id'] and provider['provider'] and provider['region'], 'provider_account_identity')
        identity = (provider['provider'], provider['account_id'])
        require(identity not in provider_ids, 'duplicate_provider_account')
        provider_ids.add(identity)
        require(HEX.fullmatch(provider['quota_receipt_sha256']) and HEX.fullmatch(provider['price_receipt_sha256']), 'provider_receipt_required')
        checked_url(provider['quota_receipt_url'])
        checked_url(provider['price_receipt_url'])
        for receipt in (provider['quota_receipt'], provider['price_receipt']):
            reference(receipt)
        require(provider['quota_receipt']['sha256'] == provider['quota_receipt_sha256'] and provider['price_receipt']['sha256'] == provider['price_receipt_sha256'], 'provider_receipt_binding')
        require(integer(provider['quota_remaining'], 0, 2**63 - 1, 'actual_quota_remaining') >= integer(provider['planned_peak_allocation'], 1, 2**63 - 1, 'planned_provider_allocation'), 'provider_quota_insufficient')
        require(provider['currency'] == budget['currency'], 'budget_currency_mismatch')
        require(integer(provider['retention_seconds'], 86400, 2**63 - 1, 'actual_retention') >= 86400 and provider['quarantine_public'] is False, 'provider_retention_policy')
        for item in provider['cost_items']:
            units, unit_price = Decimal(str(item['projected_daily_units'])), Decimal(str(item['quote_price_per_unit']))
            require(units.is_finite() and unit_price.is_finite() and units >= 0 and unit_price >= 0 and item['unit'] and item['usage_authority'], 'provider_cost_units')
            costs += units * unit_price
    approved = Decimal(str(budget['approved_daily']))
    require(approved.is_finite() and approved > 0 and budget['approval_id'], 'approved_operating_budget_required')
    with_contingency = costs * Decimal('1.30')
    require(with_contingency <= approved, 'approved_budget_exceeded')
    return dict(currency=budget['currency'], projected_daily=str(costs), contingency_daily=str(with_contingency), approved_daily=str(approved), approval_id=budget['approval_id'])


def measure_resources(parent, stop, capture):
    parent = Path(parent)
    require(parent.is_absolute() and parent.is_dir() and not parent.is_symlink(), 'delegated_cgroup_parent_required')
    require((parent / 'cgroup.controllers').is_file(), 'actual_cgroup_v2_required')
    mounts = [line.split(' - ', 1) for line in Path('/proc/self/mountinfo').read_text().splitlines()]
    require(any(fields[1].split()[0] == 'cgroup2' and parent.is_relative_to(Path(fields[0].split()[4])) for fields in mounts), 'resource_capture_requires_real_cgroup_filesystem')
    sequence = 0
    while not stop.is_set():
        for group in parent.glob('metadata-raster-*'):
            try:
                maximum = int((group / 'memory.max').read_text())
                current = int((group / 'memory.current').read_text())
                peak = int((group / 'memory.peak').read_text())
                swap = int((group / 'memory.swap.max').read_text())
            except FileNotFoundError:
                continue
            require(0 < maximum <= 268435456 and swap == 0 and current <= maximum and peak <= maximum, 'worker_resource_limit')
            capture.append(dict(category='worker_resources', sequence=sequence, observed_utc_ns=time.time_ns(), cgroup=group.name, memory_limit_bytes=maximum, memory_current_bytes=current, memory_peak_bytes=peak, memory_swap_limit_bytes=swap))
            sequence += 1
        stop.wait(0.05)
    return sequence


def mixed_load(config, capture, resource_future):
    duration = integer(config['duration_seconds'], 60, 600, 'measurement_duration')
    warmup = integer(config['warmup_seconds'], 0, duration - 1, 'warmup')
    allocation = config['allocation']
    require(allocation['workload_version'] == VERSION and allocation['global_peak_http_rps'] == 10000 and allocation['metadata_peak_rps'] == MIX['metadata_media'], 'shared_workload_allocation')
    read_rate = integer(allocation['origin_read_rps'], 1, 500, 'origin_read_allocation')
    upload_rate = integer(allocation['upload_rps'], 1, 499, 'upload_allocation')
    require(read_rate + upload_rate == 500, 'media_is_subset_of_global_total')
    require(len(config['uploads']) == duration * upload_rate, 'unique_actual_upload_inputs_required')
    tokens = config['tokens']
    require(tokens and all(ADDRESS.fullmatch(token) for token in tokens), 'actual_token_identity_input')
    origin = config['origin']
    checked_url(origin, origin=True)
    workers = integer(config['generator_workers'], 1, 256, 'bounded_generator_workers')
    response_validator('MetadataGetResponse')
    response_validator('AssetGetResponse')
    started = time.monotonic_ns()
    samples = []
    in_flight = set()
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        total = duration * 500
        upload_index = 0
        for index in range(total):
            if resource_future.done():
                resource_future.result()
                raise ValueError('resource_sampler_ended_before_workload')
            scheduled = started + index * 1000000000 // 500
            wait = (scheduled - time.monotonic_ns()) / 1e9
            if wait > 0:
                time.sleep(wait)
            for done in list(in_flight):
                if done.done():
                    samples.append({k: v for k, v in done.result()[0].items() if k != 'response'})
                    in_flight.remove(done)
            if len(in_flight) >= workers * 2:
                capture.append(dict(category='generator_overflow', sequence=index, scheduled_ns=scheduled, observed_utc_ns=time.time_ns(), success=False))
                raise ValueError('generator_capacity_exhausted_no_dropped_samples')
            if index % 500 < upload_rate:
                upload = config['uploads'][upload_index]
                upload_index += 1
                path = reference(upload['content'], 6291456)
                body = path.read_bytes()
                require(body and upload['license_receipt'] and upload['source_receipt'], 'actual_test_content_provenance')
                reference(upload['license_receipt'])
                reference(upload['source_receipt'])
                require(ADDRESS.fullmatch(upload['token']) and re.fullmatch(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}', upload['operation_id']), 'upload_identity')
                headers = env_headers(upload['headers_environment'])
                headers.update({'Content-Type': upload['mime'], 'Idempotency-Key': upload['operation_id']})
                future = pool.submit(request, capture, origin + '/v2/tokens/' + upload['token'] + '/assets/' + upload['operation_id'], 'upload', index, scheduled, method='PUT', body=body, headers=headers, schema='AssetGetResponse', expected=(200, 202))
            else:
                token = tokens[index % len(tokens)]
                future = pool.submit(request, capture, origin + '/v2/tokens/' + token + '/metadata', 'origin_read', index, scheduled, schema='MetadataGetResponse')
            in_flight.add(future)
            for done in list(in_flight):
                if done.done():
                    samples.append({k: v for k, v in done.result()[0].items() if k != 'response'})
                    in_flight.remove(done)
        for future in in_flight:
            samples.append({k: v for k, v in future.result()[0].items() if k != 'response'})
    require(len(samples) == duration * 500, 'scheduled_received_population_mismatch')
    steady = [s for s in samples if s['scheduled_ns'] >= started + warmup * 10**9]
    reads = [s for s in steady if s['category'] == 'origin_read']
    require(reads and all(s['success'] for s in reads), 'origin_read_failure')
    require(percentile([s['queue_ns'] for s in steady], .99) <= 10000000, 'generator_scheduler_delay')
    latencies = [s['latency_ns'] for s in reads]
    p95, p99 = percentile(latencies, .95), percentile(latencies, .99)
    require(p95 <= 150000000 and p99 <= 400000000, 'origin_latency_slo')
    require(sum(not s['success'] for s in steady) / len(steady) <= .001, 'valid_operation_failure_rate')
    return dict(scheduled=len(samples), observed=len(samples), steady_read_count=len(reads), warmup_excluded_seconds=warmup, duration_seconds=duration, assigned_media_rps=500, p95_ns=p95, p99_ns=p99, corrected_for_queueing=True, max_queue_ns=max(s['queue_ns'] for s in samples), upload_count=sum(s['category'] == 'upload' for s in samples), acknowledged_uploads={s['url'].rsplit('/', 1)[-1]: s['request_sha256'] for s in samples if s['category'] == 'upload' and s['success']})


def browser_measurement(config, capture, bindings):
    node = reference(config['node'], 512 * 1024 * 1024)
    script = ROOT / 'apps/launchpad/web/src/features/metadata-media/measure.browser.mjs'
    require(file_hash(script) == config['script_sha256'], 'browser_source_digest')
    browser_input = dict(config['inputs'], bindings=bindings, output=str(capture.directory / 'browser.json'))
    try:
        result = subprocess.run([str(node), str(script)], input=canonical(browser_input), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)
        require(result.returncode == 0, 'actual_browser_measurement_failed')
    finally:
        approved_probe(config['inputs']['native_probe'], 'browser-metadata-restore', capture, bindings)
    value = json.loads((capture.directory / 'browser.json').read_bytes(), object_pairs_hook=unique)
    require(value['bindings'] == bindings and value['network_interruption_recovered'] and value['axe_violations'] == 0 and value['layout_shift'] <= .1 and value['keyboard_upload_passed'] and value['keyboard_cancel_passed'], 'browser_acceptance_failed')
    return value


def generator_snapshot(interface):
    require(re.fullmatch(r'[A-Za-z0-9_-]{1,32}', interface), 'generator_network_interface')
    directory = Path('/sys/class/net') / interface
    return dict(cpu=linux_cpu_snapshot(), tx=int((directory / 'statistics/tx_bytes').read_text()), rx=int((directory / 'statistics/rx_bytes').read_text()), speed_bps=integer(int((directory / 'speed').read_text()), 1, 1000000, 'actual_nic_speed') * 1000000, monotonic_ns=time.monotonic_ns(), observed_utc_ns=time.time_ns())


def adversarial_capture(config, capture, bindings):
    record = approved_probe(config['native_probe'], 'adversarial', capture, bindings)
    cases = record['cases']
    require(set(cases) == {str(i) for i in range(1, 12)}, 'complete_adversarial_case_population')
    for case in cases.values():
        require(case['raw_receipts'], 'adversarial_raw_receipts_missing')
        for raw in case['raw_receipts']:
            reference(raw)
        require(case['candidate'] == bindings['candidate'], 'adversarial_candidate_mismatch')
    observed = {key: value['observed'] for key, value in cases.items()}
    require(observed['1']['status'] == 403 and observed['1']['foreign_rows_changed'] == 0, 'signed_cross_pool_replay')
    require(observed['2']['status'] == 422 and observed['2']['active_script_elements'] == 0, 'unsafe_or_oversized_metadata')
    require(observed['3']['status'] == 415 and observed['3']['public_objects_written'] == 0, 'mime_confusion_published')
    require(integer(observed['4']['memory_peak_bytes'], 1, 268435456, 'actual_bomb_memory_peak') <= 268435456 and integer(observed['4']['worker_wall_ns'], 1, 5000000000, 'actual_bomb_worker_wall') <= 5000000000 and observed['4']['published_derivatives'] == 0, 'dimension_bomb')
    require(observed['5']['scanner_verdict'] == 'UNAVAILABLE' and HEX.fullmatch(observed['5']['prior_image_hash']) and observed['5']['prior_image_hash'] == observed['5']['active_image_hash'] and observed['5']['asset_state'] in ('QUARANTINED', 'VALIDATING'), 'scanner_interruption')
    require(observed['6']['active_asset_revision_count'] == 1 and observed['6']['duplicate_external_allocations'] == 0, 'promotion_interruption')
    require(observed['7']['accepted'] == 1 and observed['7']['conflicts'] == 1 and observed['7']['revision'] == '8', 'revision_seven_race')
    require(observed['8']['metadata_address_http_requests'] == 0 and observed['8']['import_blocked'] is True, 'ssrf_redirect')
    require(observed['9']['token_visible'] is True and observed['9']['trading_visible'] is True and observed['9']['metadata_error_visible'] is True, 'metadata_outage_view')
    require(observed['10']['asset_blocked'] is True and observed['10']['old_allow_ignored'] is True, 'old_moderation_allow')
    require(observed['11']['approved_revisions'] == 1 and observed['11']['pending_cleared'] is True and observed['11']['same_operation_reused'] is True, 'interrupted_upload_retry')
    return dict(case_count=len(cases), raw_receipt_count=sum(len(case['raw_receipts']) for case in cases.values()))


def run(config, capture, bindings):
    before = approved_probe(config['native_probe'], 'before', capture, bindings)
    require(before['deployment_source_sha256'] == bindings['candidate'] and re.fullmatch(r'sha256:[a-f0-9]{64}', before['deployment_image_digest']), 'running_candidate_deployment_binding')
    reference(before['deployment_receipt'])
    require(before['generator_origin_region'] == before['application_origin_region'] and before['generator_outside_application_cluster'], 'origin_region_generator_scope')
    budget = check_provider(before, config['budget'])
    operation_ids = [upload['operation_id'] for upload in config['uploads']]
    require(len(set(operation_ids)) == len(operation_ids), 'duplicate_mixed_workload_operation')
    require(all(upload['token'] in config['tokens'] for upload in config['uploads']), 'upload_token_outside_owned_workload')
    generator_before = generator_snapshot(config['generator_network_interface'])
    stop = threading.Event()
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as resource_pool:
        resource_future = resource_pool.submit(measure_resources, config['raster_cgroup_parent'], stop, capture)
        try:
            load = mixed_load(config, capture, resource_future)
        finally:
            stop.set()
        resources = resource_future.result()
    require(resources > 0, 'no_actual_decode_resource_samples')
    generator_after = generator_snapshot(config['generator_network_interface'])
    require(generator_before['speed_bps'] == generator_after['speed_bps'], 'generator_nic_changed')
    generator_cpu = cpu_fraction(generator_before['cpu'], generator_after['cpu'])
    generator_nic = nic_fraction(tx_before=generator_before['tx'], rx_before=generator_before['rx'], tx_after=generator_after['tx'], rx_after=generator_after['rx'], duration_ns=generator_after['monotonic_ns'] - generator_before['monotonic_ns'], speed_bps=generator_before['speed_bps'])
    require(generator_cpu <= .7 and generator_nic <= .7, 'generator_saturation')
    capture.write('generator-resources.json', dict(before=generator_before, after=generator_after, cpu_fraction=generator_cpu, nic_fraction=generator_nic))
    cdn_samples = []
    for index, content in enumerate(config['cdn_content']):
        reference(content['source_receipt'])
        require(HEX.fullmatch(content['sha256']) and '/approved/' in checked_url(content['url']).path, 'approved_cdn_content_required')
        row, _ = request(capture, content['url'], 'cdn_delivery', index, time.monotonic_ns(), body_sha256=content['sha256'])
        require(row['success'], 'actual_cdn_delivery_failed')
        cdn_samples.append(row)
    require(cdn_samples, 'cdn_delivery_population_empty')
    before_fault = approved_probe(config['native_probe'], 'before-fault', capture, bindings)
    require(load['acknowledged_uploads'] and all(before_fault['acknowledged_operations'].get(operation) == content_hash for operation, content_hash in load['acknowledged_uploads'].items()), 'acknowledged_upload_not_durable')
    require(config['native_probe']['fault_authorization_id'], 'explicit_owned_fault_authorization_required')
    fault_started = False
    try:
        fault_started = True
        outage = approved_probe(config['native_probe'], 'dependency-loss', capture, bindings)
        require(outage['dependency_loss_observed'] and outage['dependency'] in {'scanner', 'object_storage', 'database'}, 'documented_dependency_loss_missing')
        check_inventory(before_fault, outage)
        require(outage['unapproved_public_objects'] == 0 and outage['pending_operations_preserved'], 'unsafe_dependency_loss')
    finally:
        if fault_started:
            approved_probe(config['native_probe'], 'restore-dependency', capture, bindings)
    recovered = approved_probe(config['native_probe'], 'after-recovery', capture, bindings)
    check_inventory(before_fault, recovered)
    require(recovered['pending_operations'] == 0 and recovered['approved_revisions_per_operation_max'] == 1, 'pending_recovery_not_complete')
    retention = approved_probe(config['native_probe'], 'retention', capture, bindings)
    require(retention['expired_unheld_rejected_objects'] == 0 and retention['held_objects_preserved'] and retention['measured_retention_seconds'] >= 86400, 'actual_24h_retention_proof_required')
    browser_config = dict(config['browser'], inputs=dict(config['browser']['inputs'], native_probe=config['native_probe']))
    browser = browser_measurement(browser_config, capture, bindings)
    adversarial = adversarial_capture(config, capture, bindings)
    final = approved_probe(config['native_probe'], 'after', capture, bindings)
    check_inventory(before, final)
    return dict(bindings=bindings, task='4.39', workload_version=VERSION, lane_measurements=load, generator_cpu_fraction=generator_cpu, generator_nic_fraction=generator_nic, adversarial=adversarial, worker_resource_samples=resources, cdn_delivery=dict(count=len(cdn_samples), p95_ns=percentile([s['service_ns'] for s in cdn_samples], .95)), browser=browser, cost=budget, dependency_loss_recovered=True, acknowledged_data_loss=0, final_shared_load_task='9.1', final_shared_load_qualified=False)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--input', required=True)
    parser.add_argument('--input-sha256', required=True)
    parser.add_argument('--signature', required=True)
    parser.add_argument('--authority-key', required=True)
    parser.add_argument('--authority-key-sha256', required=True)
    parser.add_argument('--openssl', required=True)
    parser.add_argument('--openssl-sha256', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    capture = None
    try:
        key = reference({'path': args.authority_key, 'sha256': args.authority_key_sha256})
        openssl = reference({'path': args.openssl, 'sha256': args.openssl_sha256}, 512 * 1024 * 1024)
        source = reference({'path': args.input, 'sha256': args.input_sha256})
        signed = subprocess.run([str(openssl), 'pkeyutl', '-verify', '-pubin', '-inkey', str(key), '-rawin', '-in', str(source), '-sigfile', str(ordinary(args.signature))], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        require(signed.returncode == 0, 'approved_measurement_input_signature')
        config = json.loads(source.read_bytes(), object_pairs_hook=unique)
        bindings = config['bindings']
        Bindings(**bindings)
        require(bindings['generator'] == file_hash(Path(__file__).absolute()), 'current_harness_source_binding')
        for name, expected in config['candidate_files'].items():
            path = ROOT / name
            require(not Path(name).is_absolute() and '..' not in Path(name).parts and file_hash(path) == expected, 'candidate_source_digest')
        require(candidate_digest(ROOT) == bindings['candidate'], 'candidate_manifest_digest')
        require(interface_bundle(document(config['interface_bundle']), ROOT)['sha256'] == bindings['interface'], 'current_interface_binding')
        require(reference(config['topology']).read_bytes() and config['topology']['sha256'] == bindings['topology'], 'topology_binding')
        capture = Capture(args.output, bindings)
        capture.write('measurement-input-binding.json', dict(input_sha256=args.input_sha256, authority_key_sha256=args.authority_key_sha256, bindings=bindings))
        result = run(config, capture, bindings)
        require(candidate_digest(ROOT) == bindings['candidate'], 'candidate_changed_during_measurement')
        capture.write('measurement.json', result)
        return 0
    except Exception as error:
        diagnostic = dict(task='4.39', failure_class=type(error).__name__, reason=str(error)[:160] if isinstance(error, ValueError) else 'actual_measurement_failed', qualified=False)
        sys.stderr.write(canonical(diagnostic).decode() + '\n')
        if capture:
            capture.write('failure.json', dict(task='4.39', failure_class=type(error).__name__, reason=str(error)[:160] if isinstance(error, ValueError) else 'actual_measurement_failed', qualified=False))
        return 1
    finally:
        if capture:
            capture.finish()


if __name__ == '__main__':
    sys.exit(main())
