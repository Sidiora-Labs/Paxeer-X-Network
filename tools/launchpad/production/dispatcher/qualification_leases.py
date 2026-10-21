"""Real child-process lease expiry and bounded resource admission proofs."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from .store import Store, StoreError


_BIND = dict.fromkeys(('baseline', 'candidate', 'interface', 'layout'), 'a' * 64)
_CAP = dict(cpu=4, memory=1024, pids=100, disk=1024, executor=30, go_build=1, container_start=1)
_RES = dict(cpu=1, memory=1, pids=1, disk=1, executor=1, go_build=0, container_start=0)


def qualify_expiry(rootpath):
    root = Path(rootpath) / 'lease-expiry'
    root.mkdir(parents=True, exist_ok=True)
    db = root / ('ledger-' + uuid.uuid4().hex + '.sqlite')
    boot = 'expiry-' + uuid.uuid4().hex
    token_path, barrier = root / 'old-token.json', root / 'takeover-ready'
    token_path.unlink(missing_ok=True)
    barrier.unlink(missing_ok=True)
    store = Store(db, _CAP, boot)
    module_root = str(Path(__file__).resolve().parents[1])
    script = '''import json,os,sys,time
from pathlib import Path
from dispatcher.store import Store,StoreError
s=Store(sys.argv[1],json.loads(sys.argv[2]),sys.argv[3])
t=s.claim('expiry-task','L01','live-child',json.loads(sys.argv[4]),json.loads(sys.argv[5]),ttl=.3)
Path(sys.argv[6]).write_text(json.dumps({'token':t,'pid':os.getpid()}))
deadline=time.monotonic()+15
while not Path(sys.argv[7]).exists():
 if time.monotonic()>deadline: raise RuntimeError('takeover barrier timeout')
 time.sleep(.01)
rejected=[]
for name in ('heartbeat','release','complete','validate'):
 try:getattr(s,name)(t)
 except StoreError:rejected.append(name)
print(json.dumps({'pid':os.getpid(),'rejected':rejected}),flush=True)
s.close()
'''
    env = dict(os.environ)
    env['PYTHONPATH'] = module_root + os.pathsep + env.get('PYTHONPATH', '')
    child = subprocess.Popen([sys.executable, '-c', script, str(db), json.dumps(_CAP), boot,
                              json.dumps(_BIND), json.dumps(_RES), str(token_path), str(barrier)],
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
    try:
        deadline = time.monotonic() + 15
        while not token_path.exists():
            if child.poll() is not None or time.monotonic() > deadline:
                raise RuntimeError('child did not establish lease')
            time.sleep(.01)
        old = json.loads(token_path.read_text())
        while time.monotonic() <= old['token']['expires'] + .03:
            time.sleep(.01)
        if child.poll() is not None:
            raise RuntimeError('expired worker was not still alive')
        os.kill(child.pid, 0)
        successor = store.claim('expiry-task', 'L01', 'coordinator-successor', _BIND, _RES)
        if successor['fence'] <= old['token']['fence']:
            raise RuntimeError('takeover failed to advance fence')
        barrier.touch()
        stdout, stderr = child.communicate(timeout=15)
        if child.returncode != 0:
            raise RuntimeError('child rejection checks failed: ' + stderr)
        result = json.loads(stdout)
        if set(result['rejected']) != {'heartbeat', 'release', 'complete', 'validate'}:
            raise RuntimeError('stale live worker operation accepted')
        retained = store.validate(successor)
        store.release(successor)
        return {'passed': True, 'child_pid': child.pid, 'child_alive_after_expiry': True,
                'old_fence': old['token']['fence'], 'successor_fence': successor['fence'],
                'rejected': result['rejected'], 'successor_reservation_preserved': retained['resources'] == _RES,
                'ledger': str(db)}
    finally:
        if child.poll() is None:
            child.kill()
            child.communicate(timeout=10)
        store.close()


def qualify_resources(rootpath):
    root = Path(rootpath) / 'resource-admission'
    root.mkdir(parents=True, exist_ok=True)
    store = Store(root / ('ledger-' + uuid.uuid4().hex + '.sqlite'), _CAP, 'resources-' + uuid.uuid4().hex)
    checks = []
    try:
        for resource in ('cpu', 'memory', 'pids', 'disk', 'executor'):
            request = dict(_RES)
            request[resource] = _CAP[resource] + 1
            try:
                store.claim('overflow-' + resource, 'L01', 'proof', _BIND, request)
            except StoreError:
                checks.append(resource + '_overflow_rejected')
            else:
                raise RuntimeError(resource + ' overflow was admitted')
        for resource in ('go_build', 'container_start'):
            first = store.claim('held-' + resource, 'L01', 'proof', _BIND, dict(_RES, **{resource: 1}))
            try:
                store.claim('denied-' + resource, 'L02', 'proof', _BIND, dict(_RES, **{resource: 1}))
            except StoreError:
                checks.append(resource + '_contention_rejected')
            else:
                raise RuntimeError(resource + ' contention admitted')
            # A distinct class may proceed while this scarce class is occupied.
            other = 'container_start' if resource == 'go_build' else 'go_build'
            parallel = store.claim('parallel-' + resource, 'L02', 'proof', _BIND, dict(_RES, **{other: 1}))
            store.release(parallel)
            store.release(first)
        for capacity in (31, 50):
            try:
                Store(root / ('denied-' + str(capacity) + '.sqlite'), dict(_CAP, executor=capacity))
            except StoreError:
                checks.append('ai_executor_' + str(capacity) + '_rejected')
            else:
                raise RuntimeError('AI coordinator reservation violated')
        if store.active():
            raise RuntimeError('resources leaked')
        mem = {}
        for line in Path('/proc/meminfo').read_text().splitlines():
            parts = line.split()
            if len(parts) >= 2:
                mem[parts[0].rstrip(':')] = int(parts[1]) * 1024
        stat = os.statvfs(root)
        return {'passed': True, 'checks': checks, 'host_observation': {
            'available_memory_bytes': mem['MemAvailable'], 'cpu_affinity_count': len(os.sched_getaffinity(0)),
            'disk_available_bytes': stat.f_bavail * stat.f_frsize},
            'scope': 'ledger admission proof; measured host capacity does not certify service throughput'}
    finally:
        store.close()
