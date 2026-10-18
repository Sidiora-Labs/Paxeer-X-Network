#!/usr/bin/env python3
import argparse
import json
import math
import os
from pathlib import Path
import resource
import shutil
import time

GIB = 1024 ** 3
PROFILES = {
    'logical': {'cpu': 0.1, 'memory': 128 * 1024 ** 2, 'disk': 128 * 1024 ** 2, 'pids': 4},
    'go-build': {'cpu': 2, 'memory': 2 * GIB, 'disk': 2 * GIB, 'pids': 128},
    'container': {'cpu': 1, 'memory': GIB, 'disk': 4 * GIB, 'pids': 128},
}


def read(path):
    try:
        return Path(path).read_text().strip()
    except (OSError, UnicodeError):
        return None


def snapshot(workspace):
    mem = {}
    for line in Path('/proc/meminfo').read_text().splitlines():
        key, value = line.split(':', 1)
        mem[key] = int(value.strip().split()[0]) * 1024
    cpu = min(os.cpu_count() or 1, len(os.sched_getaffinity(0)))
    available = mem['MemAvailable']
    pids_remaining = None
    limits = []
    controllers = read('/proc/self/cgroup') or ''
    v2 = next((line[3:] for line in controllers.splitlines() if line.startswith('0::')), None)
    if v2 is None:
        raise RuntimeError('cgroup v2 required; cannot safely infer effective v1 limits')
    base = Path('/sys/fs/cgroup')
    current = base / v2.lstrip('/')
    if not current.is_dir() or '..' in Path(v2).parts:
        raise RuntimeError('current cgroup is not observable; admission denied')
    while True:
        entry = {'path': str(current)}
        quota = read(current / 'cpu.max')
        if quota:
            entry['cpu.max'] = quota
            q, period = quota.split()
            if q != 'max':
                cpu = min(cpu, int(q) / int(period))
        for name in ('memory', 'pids'):
            cap = read(current / (name + '.max'))
            usage = read(current / (name + '.current'))
            if cap:
                entry[name + '.max'] = cap
            if usage:
                entry[name + '.current'] = usage
            if cap and cap != 'max':
                if usage is None:
                    raise RuntimeError('bounded cgroup usage missing: ' + str(current))
                remaining = max(0, int(cap) - int(usage))
                if name == 'memory':
                    available = min(available, remaining)
                else:
                    pids_remaining = remaining if pids_remaining is None else min(pids_remaining, remaining)
        limits.append(entry)
        if current == base:
            break
        current = current.parent
    disk = shutil.disk_usage(workspace)
    return {'observed_unix_seconds': time.time(), 'workspace': str(Path(workspace).resolve()),
            'effective_cpu': cpu, 'memory_available_bytes': available,
            'host_memory_total_bytes': mem['MemTotal'], 'disk_free_bytes': disk.free,
            'disk_total_bytes': disk.total, 'pids_remaining': pids_remaining,
            'nofile_soft': resource.getrlimit(resource.RLIMIT_NOFILE)[0],
            'nproc_soft': resource.getrlimit(resource.RLIMIT_NPROC)[0],
            'load_average': list(os.getloadavg()), 'cgroup_ancestors': limits}


def evaluate(observed, profile, requested, executor_slots):
    if requested < 1 or executor_slots < 0:
        raise ValueError('requested must be positive and executor slots nonnegative')
    envelope = PROFILES[profile]
    cpu_free = max(0, observed['effective_cpu'] - max(2, observed['load_average'][0]))
    mem_free = max(0, observed['memory_available_bytes'] - max(GIB, observed['host_memory_total_bytes'] * 0.1))
    disk_free = max(0, observed['disk_free_bytes'] - max(10 * GIB, observed['disk_total_bytes'] * 0.05))
    ceilings = {'cpu': math.floor(cpu_free / envelope['cpu']),
                'memory': math.floor(mem_free / envelope['memory']),
                'disk': math.floor(disk_free / envelope['disk']),
                'executor_slots': max(0, executor_slots - 1)}
    if observed['pids_remaining'] is not None:
        ceilings['pids'] = max(0, (observed['pids_remaining'] - 256) // envelope['pids'])
    if observed['nofile_soft'] != resource.RLIM_INFINITY and observed['nofile_soft'] < 1024:
        ceilings['file_descriptors'] = 0
    if observed['nproc_soft'] != resource.RLIM_INFINITY:
        ceilings['nproc_upper_bound'] = max(0, (observed['nproc_soft'] - 256) // envelope['pids'])
    admitted = min(requested, *ceilings.values())
    return {'profile': profile, 'requested_workers': requested, 'executor_slots_including_coordinator': executor_slots,
            'resource_envelope_per_worker': envelope, 'ceilings': ceilings, 'admitted_workers': admitted,
            'admission_pass': admitted == requested,
            'limits': {'go_gomaxprocs': 2, 'go_package_parallelism': 1,
                       'max_concurrent_go_builds': min(8, max(0, math.floor(cpu_free / 2)), max(0, math.floor(mem_free / (2 * GIB)))),
                       'max_concurrent_container_starts': 2},
            'scope': 'Point-in-time resource estimate; not an executor allocator, capacity benchmark or reservation. Coordinator must enforce joint leases across profiles and refresh before dispatch.'}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--workspace', default='.')
    parser.add_argument('--profile', choices=PROFILES, default='logical')
    parser.add_argument('--workers', type=int, default=50)
    parser.add_argument('--executor-slots', type=int, default=0)
    args = parser.parse_args()
    try:
        observed = snapshot(args.workspace)
        result = evaluate(observed, args.profile, args.workers, args.executor_slots)
        print(json.dumps({'observed': observed, 'decision': result}, indent=2))
        return 0 if result['admission_pass'] else 2
    except (OSError, ValueError, RuntimeError) as error:
        print(json.dumps({'admission_pass': False, 'error': str(error)}))
        return 3


if __name__ == '__main__':
    raise SystemExit(main())
