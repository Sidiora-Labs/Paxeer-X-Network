#!/usr/bin/env python3
"""Validate declarative retention/topology constraints, never live durability."""
import json
from pathlib import Path

def validate(manifest):
    stream = manifest['stream']
    requirements = manifest['server_requirements']
    capacity = manifest['capacity']
    if not stream['storage'] == 'file':
        raise ValueError("invalid manifest constraint: stream['storage'] == 'file'")
    if not stream['retention'] == 'limits':
        raise ValueError("invalid manifest constraint: stream['retention'] == 'limits'")
    if not stream['discard'] == 'new':
        raise ValueError('capacity exhaustion must reject new publication')
    if not stream['num_replicas'] == 3:
        raise ValueError("invalid manifest constraint: stream['num_replicas'] == 3")
    if not stream['max_age'] >= 72 * 3600 * 10 ** 9:
        raise ValueError("invalid manifest constraint: stream['max_age'] >= 72 * 3600 * 10 ** 9")
    if not requirements['jetstream_unique_tag'] == 'az:':
        raise ValueError("invalid manifest constraint: requirements['jetstream_unique_tag'] == 'az:'")
    if not requirements['sync_interval'] == 'always':
        raise ValueError("invalid manifest constraint: requirements['sync_interval'] == 'always'")
    nodes = requirements['replicas']
    if not len(nodes) == 3:
        raise ValueError('invalid manifest constraint: len(nodes) == 3')
    zones = []
    for node in nodes:
        az = [tag for tag in node['tags'] if tag.startswith('az:')]
        if not len(az) == 1:
            raise ValueError('invalid manifest constraint: len(az) == 1')
        zones.extend(az)
        if not set(stream['placement']['tags']).issubset(node['tags']):
            raise ValueError("invalid manifest constraint: set(stream['placement']['tags']).issubset(node['tags'])")
    if not len(set(zones)) == 3:
        raise ValueError('invalid manifest constraint: len(set(zones)) == 3')
    if not (requirements['tls_required'] and requirements['subject_scoped_credentials']):
        raise ValueError("invalid manifest constraint: requirements['tls_required'] and requirements['subject_scoped_credentials']")
    if not requirements['separate_admin_identity']:
        raise ValueError("invalid manifest constraint: requirements['separate_admin_identity']")
    normal = capacity['normal_events_per_day'] * 3 * capacity['envelope_budget_floor_bytes']
    peak = capacity['peak_events_per_second'] * capacity['retention_seconds'] * capacity['envelope_budget_floor_bytes']
    if not capacity['normal_logical_bytes_floor'] == normal:
        raise ValueError("invalid manifest constraint: capacity['normal_logical_bytes_floor'] == normal")
    if not capacity['normal_replicated_bytes_floor'] == normal * 3:
        raise ValueError("invalid manifest constraint: capacity['normal_replicated_bytes_floor'] == normal * 3")
    if not capacity['peak_logical_bytes_floor'] == peak:
        raise ValueError("invalid manifest constraint: capacity['peak_logical_bytes_floor'] == peak")
    if not capacity['peak_replicated_bytes_floor'] == peak * 3:
        raise ValueError("invalid manifest constraint: capacity['peak_replicated_bytes_floor'] == peak * 3")
    if not stream['max_bytes'] >= peak * (100 + capacity['headroom_percent']) // 100:
        raise ValueError("invalid manifest constraint: stream['max_bytes'] >= peak * (100 + capacity['headroom_percent']) // 100")
    if not manifest['consumer_defaults']['ack_policy'] == 'explicit':
        raise ValueError("invalid manifest constraint: manifest['consumer_defaults']['ack_policy'] == 'explicit'")
    return {'configuration': 'pass', 'live_three_az_operation': 'not_tested', 'normal_logical_bytes_floor': normal, 'peak_logical_bytes_floor': peak}
if __name__ == '__main__':
    path = Path(__file__).with_name('stream-manifest.json')
    print(json.dumps(validate(json.loads(path.read_text())), sort_keys=True))


def measured_capacity(encoded_envelopes, events_per_second, retention_seconds, replicas, index_bytes_per_event, free_bytes_per_node, headroom_percent=25):
    if not encoded_envelopes or any(not isinstance(wire, bytes) or not wire for wire in encoded_envelopes):
        raise ValueError('actual serialized envelope samples required')
    if events_per_second < 2000 or retention_seconds < 72 * 3600 or replicas != 3 or index_bytes_per_event < 0 or headroom_percent < 25:
        raise ValueError('capacity constraints violated')
    if len(free_bytes_per_node) != replicas or any(value <= 0 for value in free_bytes_per_node):
        raise ValueError('actual node free bytes required')
    sizes = sorted(map(len, encoded_envelopes))
    p99 = sizes[(99 * len(sizes) + 99) // 100 - 1]
    logical = events_per_second * retention_seconds * (p99 + index_bytes_per_event)
    provisioned = (logical * (100 + headroom_percent) + 99) // 100
    if any(free < provisioned for free in free_bytes_per_node):
        raise ValueError('insufficient measured per-node retention capacity')
    return {'measured_samples': len(sizes), 'serialized_p99_bytes': p99, 'logical_bytes': logical,
            'replicated_bytes': logical * replicas, 'required_bytes_per_node': provisioned}
