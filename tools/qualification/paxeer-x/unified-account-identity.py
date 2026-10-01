#!/usr/bin/env python3
import json
import os
from pathlib import Path
import re
import ssl
import stat
import subprocess
import sys
import urllib.parse
import urllib.request

COUNT = 0
ROOT = Path(__file__).resolve().parents[3]

def require(value, message):
    if not value:
        raise ValueError(message)

def private_json(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600,
            'input must be an owned private regular file')
    require(info.st_size <= 1024 * 1024, 'input exceeds bound')
    return json.loads(path.read_text())

def rpc(target, method, params):
    global COUNT
    url = urllib.parse.urlsplit(target['url'])
    require(url.scheme == 'https' and url.hostname in ('localhost', '127.0.0.1', '::1') and not url.username and not url.password,
            'fixture must use an isolated local TLS endpoint')
    context = ssl.create_default_context(cafile=target['ca_file'])
    if 'certificate_file' in target:
        context.load_cert_chain(target['certificate_file'], target['key_file'])
    request = urllib.request.Request(target['url'], data=json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params}).encode(),
                                     headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(request, context=context, timeout=8) as response:
        raw = response.read(8 * 1024 * 1024 + 1)
    require(len(raw) <= 8 * 1024 * 1024, 'response exceeds bound')
    answer = json.loads(raw)
    require(answer.get('jsonrpc') == '2.0' and answer.get('id') == 1, 'response identity mismatch')
    COUNT += 1
    return answer

def run():
    bundle_path = os.environ.get('PAXEER_X_IDENTITY_BUNDLE')
    fixture_path = os.environ.get('PAXEER_X_IDENTITY_FIXTURE')
    require(bundle_path and fixture_path, 'prebuilt identity bundle and real isolated identity fixture are required')
    bundle, fixture = private_json(bundle_path), private_json(fixture_path)
    revision = subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    require(bundle['revision'] == revision == fixture['revision'], 'source provenance mismatch')
    require(fixture['schema'] == 'paxeer-x.unified-identity-fixture.v1', 'unsupported fixture schema')
    for name in ('client','explorer','router','keeper','precompile'):
        binary = Path(bundle['binaries'][name])
        require(binary.is_file() and os.access(binary,os.X_OK), 'missing prebuilt executable: '+name)
    evm, did, native = fixture['evm'], fixture['did'], fixture['native_account']
    require(re.fullmatch(r'0x[0-9a-f]{40}',evm) and re.fullmatch(r'did:layerx:[0-9a-f]{64}',did) and re.fullmatch(r'[0-9a-f]{64}',native), 'invalid real identity selectors')
    healthy = fixture['targets']['healthy']
    require(rpc(healthy,'eth_chainId',[])['result'] == '0x7d', 'wrong chain')
    expected = None
    for selector in (evm,did,evm,did):
        answer = rpc(healthy,'px_resolveAccount',[selector])
        require('error' not in answer, 'real binding unavailable')
        document = answer['result']
        require(document['evm_address'] == evm and document['layerx_did'] == did and document['layerx_account'] == native and document['bound'] is True, 'conflicting real binding')
        require(expected is None or expected == document, 'repeated lookup changed owner')
        expected = document
        for method in ('px_getAccount','px_getBalances','px_getUnifiedHistory'):
            joined = rpc(healthy,method,[selector])
            require('error' not in joined and joined['result']['account'] == document, 'account/balance/history join changed owner')
    for selector in (native,did.removeprefix('did:layerx:')):
        refusal = rpc(healthy,'px_resolveAccount',[selector])
        require(refusal.get('error',{}).get('data',{}).get('code') == 'native_account_reverse_resolution_unsupported', 'bare native account entered DID lookup')
    for target in ('wrong_network','unavailable_binding','conflicting_binding'):
        refusal = rpc(fixture['targets'][target],'px_resolveAccount',[did])
        require(refusal.get('error',{}).get('code') == -32001, 'required refusal did not occur: '+target)
        require(refusal['error'].get('data',{}).get('code') == fixture['refusal_codes'][target], 'wrong typed refusal: '+target)
    groups = {'client':['identity_selector_contract'], 'explorer':['unified::tests::'], 'router':['paxeer::'],
              'keeper':['-test.run=^TestLayerX(Bind|DoubleBind|Unbind|BindingGenesis)','-test.v'],
              'precompile':['-test.run=^TestLayerX','-test.v']}
    for name,args in groups.items():
        env = dict(os.environ, SSL_CERT_FILE=healthy['ca_file'])
        completed = subprocess.run([bundle['binaries'][name],*args],cwd=ROOT,env=env,capture_output=True,text=True,timeout=120)
        require(completed.returncode == 0, 'prebuilt test failed: '+name)
        require(('test result: ok.' in completed.stdout and '0 passed;' not in completed.stdout) or '\nPASS\n' in completed.stdout, 'no actual tests: '+name)
        print('ok identity production tests '+name,flush=True)

if __name__ == '__main__':
    try:
        run()
    except (ValueError,KeyError,OSError,subprocess.SubprocessError) as error:
        print('identity: refusal: '+(str(error) if isinstance(error,ValueError) else type(error).__name__),file=sys.stderr)
        print(f'PAXEER_X_GATE tests={COUNT} skipped=0')
        sys.exit(1)
    print(f'PAXEER_X_GATE tests={COUNT} skipped=0')
