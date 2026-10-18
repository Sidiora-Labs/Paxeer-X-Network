"""Real local adapters; authority is explicit and never inferred from a name prefix."""
from dataclasses import dataclass
import http.client
import ipaddress
import json
import os
import re
import signal
import subprocess
import time
import threading
import socket
import selectors
from urllib.parse import urlsplit

class BoundaryError(ValueError): pass

class LocalHTTP:
    def __init__(self,origin,allowed_ports,timeout=3,max_bytes=1048576):
        u=urlsplit(origin)
        if u.scheme!="http" or u.username or u.password or u.path not in ("", "/") or u.query or u.fragment:
            raise BoundaryError("plain isolated loopback origin required")
        try: loopback=ipaddress.ip_address(u.hostname).is_loopback
        except ValueError: loopback=False
        if not loopback or u.port not in allowed_ports or not 0<timeout<=10 or not 1<=max_bytes<=1048576:
            raise BoundaryError("origin outside allocated loopback ports")
        self.host=u.hostname; self.port=u.port; self.timeout=timeout; self.max_bytes=max_bytes
    def request(self,method,path,body=None):
        if method not in ("GET","POST") or not path.startswith("/") or path.startswith("//") or any(c in path for c in "\r\n"):
            raise BoundaryError("invalid frozen-interface request")
        raw=None if body is None else json.dumps(body,separators=(",",":"),allow_nan=False).encode()
        if raw is not None and len(raw)>self.max_bytes: raise BoundaryError("request bound")
        c=http.client.HTTPConnection(self.host,self.port,timeout=self.timeout)
        sockets=[]
        expired=threading.Event()
        def abort():
            expired.set()
            for sock in sockets:
                try: sock.shutdown(socket.SHUT_RDWR)
                except OSError: pass
                sock.close()
        timer=threading.Timer(self.timeout,abort); timer.daemon=True; timer.start()
        try:
            c.connect(); sockets.append(c.sock)
            if expired.is_set(): raise TimeoutError("HTTP total deadline")
            c.request(method,path,body=raw,headers={"Content-Type":"application/json"})
            r=c.getresponse(); data=r.read(self.max_bytes+1)
            if expired.is_set(): raise TimeoutError("HTTP total deadline")
            if len(data)>self.max_bytes: raise BoundaryError("response bound")
            if 300<=r.status<400: raise BoundaryError("redirects forbidden")
            return r.status,data
        finally: timer.cancel(); c.close()

class OwnedProcess:
    def __init__(self,argv,cwd,stdout):
        if not argv or not os.path.isabs(argv[0]): raise BoundaryError("explicit executable required")
        self.stopped=False
        self.child=subprocess.Popen(argv,cwd=cwd,stdout=stdout,stderr=subprocess.STDOUT,start_new_session=True,env={**os.environ,"PYTHONUNBUFFERED":"1"})
    def alive(self): return self.child.poll() is None
    def stop(self):
        if self.stopped: return
        self.stopped=True
        # The group remains ours even after its leader exits. Never accept a PID
        # supplied by a caller, and never skip descendants on a quick leader exit.
        for sig,delay in ((signal.SIGTERM,.2),(signal.SIGKILL,0)):
            try: os.killpg(self.child.pid,sig)
            except ProcessLookupError: break
            if delay: time.sleep(delay)
        self.child.wait(timeout=3)
    def __enter__(self): return self
    def __exit__(self,*args): self.stop()

@dataclass(frozen=True)
class FaultPermit:
    container_id: str
    namespace: str
    attempt: str
    role: str
    actions: tuple
    def __post_init__(self):
        if not re.fullmatch("[0-9a-f]{64}",self.container_id): raise BoundaryError("full immutable container ID required")
        if not re.fullmatch("kd-[0-9a-f]{24}",self.namespace): raise BoundaryError("allocated namespace required")
        if not re.fullmatch("[0-9a-f-]{36}",self.attempt) or self.role not in ("app","postgres","redis","nats","object-fixture","rpc-fixture"):
            raise BoundaryError("explicit attempt/role required")
        if type(self.actions) is not tuple or not self.actions or any(a not in ("pause","unpause","stop","start") for a in self.actions):
            raise BoundaryError("unsupported fault action")

class DockerFaults:
    """Coordinator provisions exact resources and labels. No compose/global resets,
    deletion, shell exec, live validator actions, name resolution or wildcard IDs.
    Docker inspect followed by exact immutable ID closes name replacement races.
    """
    def __init__(self,permits,expected_namespace,expected_attempt,docker="/usr/bin/docker"):
        permits=tuple(permits)
        if any(p.namespace!=expected_namespace or p.attempt!=expected_attempt for p in permits):
            raise BoundaryError("permit outside current coordinator lease")
        self.permits={p.container_id:p for p in permits}
        if len(self.permits)!=len(permits): raise BoundaryError("duplicate target permit")
        if not self.permits: raise BoundaryError("empty allowlist")
        self.docker=docker
    @staticmethod
    def validate_inspect(permit,obj):
        labels=obj.get("Config",{}).get("Labels",{}) or {}
        expected={"kindle.assurance.namespace":permit.namespace,"kindle.assurance.attempt":permit.attempt,"kindle.assurance.role":permit.role,"kindle.assurance.isolated":"true"}
        if obj.get("Id")!=permit.container_id or any(labels.get(k)!=v for k,v in expected.items()):
            raise BoundaryError("container identity/ownership mismatch")
        # Host namespaces, privileged containers, or host mounts cannot be fault targets.
        host=obj.get("HostConfig",{})
        if host.get("Privileged") or host.get("NetworkMode")=="host" or host.get("PidMode")=="host" or obj.get("Mounts"):
            raise BoundaryError("resource is not isolated")
    def apply(self,container_id,action):
        permit=self.permits.get(container_id)
        if permit is None or action not in permit.actions: raise BoundaryError("fault outside exact allowlist")
        obj=json.loads(bounded_command([self.docker,"inspect",container_id],5))
        if not isinstance(obj,list) or len(obj)!=1: raise BoundaryError("ambiguous container")
        self.validate_inspect(permit,obj[0])
        args=[self.docker,action]
        if action=="stop": args += ["--time","3"]
        bounded_command(args+[container_id],10)
        return {"container_id":container_id,"action":action,"observed_monotonic_ns":time.monotonic_ns()}


@dataclass(frozen=True)
class RecoveryPermit:
    container_id: str
    namespace: str
    attempt: str
    role: str
    failure_domain: str
    volumes: tuple
    actions: tuple

    def __post_init__(self):
        if not re.fullmatch('[0-9a-f]{64}', self.container_id) or not re.fullmatch('kd-[0-9a-f]{24}', self.namespace) or not re.fullmatch('[0-9a-f-]{36}', self.attempt):
            raise BoundaryError('exact recovery lease identity required')
        if self.role not in ('app','postgres','nats','redis','scanner','object-fixture','rpc-fixture') or not re.fullmatch('[a-z0-9-]{1,64}', self.failure_domain):
            raise BoundaryError('explicit recovery role/failure domain required')
        if type(self.volumes) is not tuple or len(set(self.volumes))!=len(self.volumes) or any(not re.fullmatch(self.namespace+'-[a-z0-9-]{1,64}', v) for v in self.volumes):
            raise BoundaryError('exact namespace owned named volumes required')
        if type(self.actions) is not tuple or not self.actions or any(action not in ('pause','unpause','stop','start') for action in self.actions):
            raise BoundaryError('unsupported recovery action')


class RecoveryFaults:
    def __init__(self, permits, namespace, attempt, docker):
        permits=tuple(permits)
        if not permits or len({p.container_id for p in permits})!=len(permits) or any(p.namespace!=namespace or p.attempt!=attempt for p in permits):
            raise BoundaryError('recovery resources outside current lease')
        if not os.path.isabs(docker): raise BoundaryError('explicit Docker executable required')
        self.permits={p.container_id:p for p in permits}; self.docker=docker

    @staticmethod
    def validate_resource(permit, obj, volumes):
        labels=obj.get('Config',{}).get('Labels',{}) or {}
        expected={'kindle.assurance.namespace':permit.namespace,'kindle.assurance.attempt':permit.attempt,'kindle.assurance.role':permit.role,'kindle.assurance.isolated':'true','kindle.assurance.failure-domain':permit.failure_domain}
        host=obj.get('HostConfig',{})
        if obj.get('Id')!=permit.container_id or any(labels.get(k)!=v for k,v in expected.items()) or host.get('Privileged') or host.get('NetworkMode')=='host' or host.get('PidMode')=='host' or host.get('Devices') or host.get('CapAdd'):
            raise BoundaryError('recovery target is not exact isolated resource')
        mounts=obj.get('Mounts',[])
        if any(m.get('Type')!='volume' or m.get('Driver')!='local' or not m.get('Destination','').startswith('/') for m in mounts) or {m.get('Name') for m in mounts}!=set(permit.volumes) or len(mounts)!=len(permit.volumes):
            raise BoundaryError('unexpected recovery mount')
        if len(volumes)!=len(permit.volumes) or {v.get('Name') for v in volumes}!=set(permit.volumes):
            raise BoundaryError('ambiguous recovery volume inventory')
        for volume in volumes:
            owned=volume.get('Labels') or {}
            if volume.get('Driver')!='local' or volume.get('Options') or any(owned.get(k)!=v for k,v in expected.items()):
                raise BoundaryError('volume outside exact isolated recovery lease')

    def inspect(self, container_id):
        permit=self.permits.get(container_id)
        if permit is None: raise BoundaryError('resource outside recovery allowlist')
        objects=json.loads(bounded_command([self.docker,'inspect',container_id],5))
        if not isinstance(objects,list) or len(objects)!=1: raise BoundaryError('ambiguous recovery container')
        volumes=[]
        for name in permit.volumes:
            result=json.loads(bounded_command([self.docker,'volume','inspect',name],5))
            if not isinstance(result,list) or len(result)!=1: raise BoundaryError('ambiguous recovery volume')
            volumes.extend(result)
        self.validate_resource(permit,objects[0],volumes)
        return objects[0]

    def apply(self, container_id, action):
        permit=self.permits.get(container_id)
        if permit is None or action not in permit.actions: raise BoundaryError('recovery action outside allowlist')
        before=self.inspect(container_id)
        argv=[self.docker,action]+(['--time','3'] if action=='stop' else [])+[container_id]
        bounded_command(argv,10)
        after=self.inspect(container_id)
        state=after.get('State',{})
        if action=='pause' and state.get('Paused') is not True or action=='unpause' and state.get('Paused') is not False or action=='stop' and state.get('Running') is not False or action=='start' and state.get('Running') is not True:
            raise BoundaryError('fault action did not change actual resource state')
        return {'container_id':container_id,'role':permit.role,'failure_domain':permit.failure_domain,'action':action,'before_state':before.get('State'),'after_state':state,'observed_epoch':time.time()}


def bounded_command(argv,timeout,max_bytes=1048576):
    """Bound both pipes during execution and kill the exact owned process group.
    stdout is returned; stderr shares the same byte budget and cannot fill a pipe.
    """
    child=subprocess.Popen(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
    selector=selectors.DefaultSelector(); data=bytearray(); total=0
    for stream in (child.stdout,child.stderr):
        os.set_blocking(stream.fileno(),False); selector.register(stream,selectors.EVENT_READ)
    deadline=time.monotonic()+timeout
    try:
        while selector.get_map():
            remaining=deadline-time.monotonic()
            if remaining<=0: raise TimeoutError("owned command deadline")
            for key,_ in selector.select(min(remaining,.1)):
                chunk=os.read(key.fileobj.fileno(),65536)
                if not chunk: selector.unregister(key.fileobj); continue
                total+=len(chunk)
                if total>max_bytes: raise BoundaryError("command output bound")
                if key.fileobj is child.stdout: data.extend(chunk)
        code=child.wait(timeout=max(.001,deadline-time.monotonic()))
        if code: raise subprocess.CalledProcessError(code,argv)
        return bytes(data)
    finally:
        selector.close()
        for sig in (signal.SIGTERM,signal.SIGKILL):
            try: os.killpg(child.pid,sig)
            except ProcessLookupError: pass
        child.wait(timeout=3); child.stdout.close(); child.stderr.close()
