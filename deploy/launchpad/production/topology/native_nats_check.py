#!/usr/bin/env python3
"""Parse generated NATS config in an isolated, preinstalled pinned broker image.

Creates only a temporary self-signed TEST certificate and a --network none,
--rm parser process. Does not connect to or alter any broker or production service.
"""
import json
import os
import subprocess
import tempfile
import uuid
from pathlib import Path
import topology
ROOT=Path(__file__).resolve().parent
IMAGE='nats:2.11.9-alpine@sha256:777787bbb4c7c4083ec2213b0b8b14151617610a3e0d85776bd995cd46599c73'
def run(argv,**kwargs):
    r=subprocess.run(argv,check=False,capture_output=True,text=True,timeout=45,**kwargs)
    if r.returncode:
        print(json.dumps({'passed':False,'exit':r.returncode,'stdout':r.stdout,'stderr':r.stderr}))
        r.check_returncode()
    return r
def require_native_validity(stdout,stderr):
    combined=stdout+stderr
    topology.need('configuration file' in combined.lower() and 'is valid' in combined.lower() and 'flag provided but not defined' not in combined,'native parser did not report actual configuration validity')
def main():
    run(['docker','image','inspect',IMAGE]) # no implicit pulls
    c=topology.load(ROOT/'fixtures/synthetic-config.json');plan=topology.compile_plan(c)
    conf=next(x['data']['nats.conf'] for x in plan['resources']['items'] if x['kind']=='ConfigMap' and x['metadata']['name']=='nats-config')
    with tempfile.TemporaryDirectory(prefix='kindle-l42-nats-parser-') as tmp:
        d=Path(tmp);(d/'config').mkdir();(d/'tls').mkdir();(d/'auth').mkdir()
        (d/'config/nats.conf').write_text(conf)
        # A local test identity only; real per-service NKey/account permissions
        # remain an externally provisioned deployment prerequisite.
        (d/'auth/accounts.conf').write_text('accounts { FIXTURE { jetstream: enabled; users: [{user: "fixture", password: "public-test-only"}] } }\n')
        run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=kindle-l42-parser-fixture','-keyout',str(d/'tls/tls.key'),'-out',str(d/'tls/tls.crt')])
        (d/'tls/ca.crt').write_bytes((d/'tls/tls.crt').read_bytes())
        for p in d.rglob('*'):
            if p.is_file():p.chmod(0o644)
            else:p.chmod(0o755)
        d.chmod(0o755)
        container_name='kindle-l42-parser-'+uuid.uuid4().hex
        try:
            result=run(['docker','run','--name',container_name,'--rm','--pull=never','--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','--user','10001:10001','--label','kindle.test.owner=L42','--env','POD_NAME=nats-0','--mount',f'type=bind,src={d}/config,dst=/config,readonly','--mount',f'type=bind,src={d}/tls,dst=/tls,readonly','--mount',f'type=bind,src={d}/auth,dst=/auth,readonly',IMAGE,'--config','/config/nats.conf','-t'])
        finally:
            subprocess.run(['docker','rm','--force',container_name],capture_output=True,timeout=15)
        require_native_validity(result.stdout,result.stderr)
        print(json.dumps({'check':'native_pinned_nats_configuration_parser','passed':True,'image':IMAGE,'network':'none','configurationDigest':topology.digest(c),'stdout':result.stdout.strip(),'stderr':result.stderr.strip(),'limitations':'Syntax/TLS-file loading only; no quorum, disk, accounts authorization or capacity proof.'}))
if __name__=='__main__':main()
