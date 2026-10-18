from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import ssl
import struct
import time
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import urlsplit

from contracts import canonical, load, require, sha, validate_binding
from render import atomic_write
from runtime import fetch


def validate_response(root, probe, document):
    import jsonschema
    schema_path = (Path(root)/probe['schema_path']).resolve()
    require(schema_path.is_relative_to(Path(root).resolve()), 'probe schema escapes source')
    require(sha(schema_path.read_bytes()) == probe['schema_sha256'], 'probe schema changed')
    schema = load(schema_path)
    jsonschema.Draft202012Validator.check_schema(schema)
    jsonschema.Draft202012Validator(schema,format_checker=jsonschema.FormatChecker()).validate(document)


def read_exact(stream, size):
    raw = bytearray()
    while len(raw) < size:
        part = stream.read(size-len(raw))
        require(part, 'socket closed before replay completed')
        raw.extend(part)
    return bytes(raw)


def send_frame(sock, opcode, payload):
    require(len(payload) <= 65536, 'synthetic frame too large')
    mask = os.urandom(4)
    header = bytes((0x80|opcode, 0x80|len(payload))) if len(payload)<126 else bytes((0x80|opcode, 0x80|126))+struct.pack('!H',len(payload)) if len(payload)<=65535 else bytes((0x80|opcode,0x80|127))+struct.pack('!Q',len(payload))
    sock.sendall(header+mask+bytes(value^mask[i%4] for i,value in enumerate(payload)))


def replay(probe, root):
    u=urlsplit(probe['url'])
    token_path=Path(probe['token_file'])
    require(token_path.is_file() and not token_path.is_symlink() and token_path.stat().st_size<=8192 and token_path.stat().st_mode & 0o077 == 0, 'session mount must be private')
    token=token_path.read_text().strip()
    require(token and '\r' not in token and '\n' not in token, 'invalid session token')
    sock=socket.create_connection((u.hostname,u.port or (443 if u.scheme=='wss' else 80)),timeout=5)
    try:
        if u.scheme=='wss':
            sock=ssl.create_default_context().wrap_socket(sock,server_hostname=u.hostname)
        deadline=time.monotonic()+5
        sock.settimeout(5)
        key=base64.b64encode(os.urandom(16)).decode()
        headers={'Host':u.netloc,'Upgrade':'websocket','Connection':'Upgrade','Sec-WebSocket-Key':key,'Sec-WebSocket-Version':'13','Sec-WebSocket-Protocol':'sidiora.v2','Authorization':'Bearer '+token,**probe['headers']}
        sock.sendall(('GET '+(u.path or '/')+' HTTP/1.1\r\n'+''.join(k+': '+v+'\r\n' for k,v in headers.items())+'\r\n').encode())
        stream=sock.makefile('rb')
        try:
            status=stream.readline(4097)
            require(status.startswith(b'HTTP/1.1 101 '), 'socket upgrade failed')
            response_headers={};total=len(status)
            while True:
                line=stream.readline(4097);total+=len(line)
                require(line and total<=16384, 'socket handshake exceeds bounds')
                if line==b'\r\n':break
                k,separator,v=line.decode('ascii').partition(':')
                require(separator and k.lower() not in response_headers, 'invalid handshake header')
                response_headers[k.lower()]=v.strip()
            expected=base64.b64encode(hashlib.sha1((key+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
            require(response_headers.get('sec-websocket-accept')==expected and response_headers.get('sec-websocket-protocol')=='sidiora.v2', 'socket protocol mismatch')
            send_frame(sock,1,canonical(probe['frame']))
            subscribed=False
            while time.monotonic()<deadline:
                sock.settimeout(max(.001,deadline-time.monotonic()))
                first,second=read_exact(stream,2)
                require(first&0x80 and not first&0x70 and not second&0x80, 'unsupported fragmented or masked server frame')
                length=second&0x7f
                if length==126:length=struct.unpack('!H',read_exact(stream,2))[0]
                elif length==127:length=struct.unpack('!Q',read_exact(stream,8))[0]
                require(length<=65536, 'replay frame too large')
                payload=read_exact(stream,length);opcode=first&15
                if opcode==9:
                    require(length<=125, 'invalid ping');send_frame(sock,10,payload);continue
                require(opcode==1, 'replay terminated without checkpoint')
                document=json.loads(payload);validate_response(root,probe,document)
                if document.get('op')=='subscribed':subscribed=True
                if document.get('op')=='checkpoint':
                    require(subscribed and document.get('cursor'), 'replay lacks subscribed checkpoint')
                    return {'replayed':True,'checkpoint_sha256':sha(str(document['cursor']).encode())}
                require(document.get('op') not in ('error','reset_required'), 'replay rejected')
            raise TimeoutError('synthetic replay deadline')
        finally:
            stream.close()
    finally:
        sock.close()


def run_once(binding, config, root):
    validate_binding(binding,config)
    observations=[]
    for probe in binding['probes']:
        started=time.monotonic();observed=time.time()
        try:
            if probe['journey']=='websocket_replay':
                replay(probe,root)
            else:
                body=canonical(probe['body']) if probe['body'] is not None else None
                headers={**probe['headers'],'Content-Type':'application/json'}
                status,raw,_=fetch(probe['url'],method=probe['method'],body=body,headers=headers,timeout=5,limit=1024*1024)
                require(status in ((201,) if probe['journey']=='wallet_challenge' else (200,)), 'synthetic route failed')
                validate_response(root,probe,json.loads(raw))
            success=1;reason='none'
        except (ValueError, OSError, TimeoutError, KeyError, __import__('jsonschema').ValidationError) as failure:
            success=0;reason='timeout' if isinstance(failure,TimeoutError) else 'unavailable'
        observations.append({'journey':probe['journey'],'environment':binding['environment'],'observed_at':observed,'duration_seconds':time.monotonic()-started,'success':success,'error_code':reason})
    return observations


def run_once_concurrent(binding, config, root):
    validate_binding(binding,config)
    def observe(probe):
        started=time.monotonic();observed=time.time()
        try:
            if probe['journey']=='websocket_replay':replay(probe,root)
            else:
                body=canonical(probe['body']) if probe['body'] is not None else None
                status,raw,_=fetch(probe['url'],method=probe['method'],body=body,headers={**probe['headers'],'Content-Type':'application/json'},timeout=5,limit=1024*1024)
                require(status in ((201,) if probe['journey']=='wallet_challenge' else (200,)), 'synthetic route failed')
                validate_response(root,probe,json.loads(raw))
            require(time.monotonic()-started<=5,'synthetic total deadline exceeded')
            success=1;reason='none'
        except (ValueError,OSError,TimeoutError,KeyError,__import__('jsonschema').ValidationError) as failure:
            success=0;reason='timeout' if isinstance(failure,TimeoutError) else 'unavailable'
        return {'journey':probe['journey'],'environment':binding['environment'],'observed_at':observed,'duration_seconds':time.monotonic()-started,'success':success,'error_code':reason}
    with ThreadPoolExecutor(max_workers=len(binding['probes'])) as pool:
        return list(pool.map(observe,binding['probes']))


def exposition(binding, observations):
    rows=[]
    for probe in binding['probes']:
        labels='journey="'+probe['journey']+'",environment="'+binding['environment']+'",zone="zone-a"'
        rows.append('synthetic_journey_expected{'+labels+'} 1')
    for item in observations:
        labels='journey="'+item['journey']+'",environment="'+item['environment']+'",zone="zone-a"'
        rows.extend(['synthetic_journey_success{'+labels+'} '+str(item['success']),'synthetic_journey_last_observed_timestamp_seconds{'+labels+'} '+str(item['observed_at'])])
    return ('\n'.join(rows)+'\n').encode()


def main():
    p=argparse.ArgumentParser();p.add_argument('--binding',required=True);p.add_argument('--config',required=True);p.add_argument('--root',required=True);p.add_argument('--output',required=True);p.add_argument('--cycles',type=int,default=1);a=p.parse_args()
    require(1<=a.cycles<=60, 'probe cycle bound')
    binding,config=load(a.binding),load(a.config)
    for cycle in range(a.cycles):
        started=time.monotonic();observations=run_once(binding,config,a.root)
        atomic_write(Path(a.output)/'synthetic.prom',exposition(binding,observations))
        atomic_write(Path(a.output)/('cycle-%04d.json'%cycle),canonical(observations))
        if cycle+1<a.cycles:time.sleep(max(0,30-(time.monotonic()-started)))


if __name__=='__main__':
    main()
