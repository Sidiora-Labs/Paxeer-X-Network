#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
ROOT=Path(__file__).resolve().parents[3]
EVIDENCE=Path(os.environ.get('PAXEER_X_PROGRAM_REPLAY_EVIDENCE','/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043524'))
DECLARED=['programs/crates/layerx-programs-runtime/src/replay_record.rs','programs/crates/layerx-programs-runtime/tests/program_replay.rs','tests/daemon/lxp_test_program_replay.c','tools/paxeer-x/build/104.35.20.mk','tools/qualification/paxeer-x/program-replay-record.py']
def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()
def sources():
    names=subprocess.run(['git','ls-files','-z','--','src','include','cmd/layerxd','programs','Makefile','tests/programs/test_call_activity.c','tests/daemon/lxp_test_arbiter_admission.c','tests/bridge/files.h','contracts/config'],cwd=ROOT,check=True,capture_output=True).stdout.split(b'\0')
    paths={os.fsdecode(p) for p in names if p}|set(DECLARED)
    return {p:sha(ROOT/p) for p in sorted(paths) if Path(p).suffix in {'.c','.h','.rs','.toml','.lock','.json','.inc','.py','.mk'} or p=='Makefile'}
def execute(command,log,environment=None):
    with log.open('w') as stream:
        result=subprocess.run(command,cwd=ROOT,env=environment,stdout=stream,stderr=subprocess.STDOUT,timeout=540)
    print('exit='+str(result.returncode)+' log='+str(log),flush=True)
    if result.returncode: raise subprocess.CalledProcessError(result.returncode,command)
    return log.read_text()
def prepare(path):
    source=(ROOT/'tests/daemon/lxp_test_arbiter_admission.c').read_text()
    start=source.index('static int maintenance_publish(')
    end=source.index('static int maintenance_noncall(',start)
    publish=source[start:end]
    publish=publish.replace('static int maintenance_publish(', 'static int replay_publish(' ,1)
    publish=publish.replace('size_t count, uint64_t batch_number)', 'size_t count, uint64_t batch_number, lxp_result expected_result, bool serial)',1)
    assertion='CHECK(decoded[i].result_code == (f->arbiter_terminal ? LXP_ERR_IDENTITY_FROZEN : LXP_OK));'
    if publish.count(assertion)!=1: raise RuntimeError('genuine receipt assertion boundary changed')
    publish=publish.replace(assertion,'CHECK(decoded[i].result_code == expected_result);')
    publish=publish.replace('} else if (type == LX_PROGRAMS_CALL) {','} else if (type == LX_PROGRAMS_CALL && !serial) {')
    owner=source.index('static int transport_owner_open(')
    reopen=source.index('static int transport_reopen(',owner)
    main=source.index('int main(int argc, char **argv)',reopen)
    insert=source.index('#undef lxp_kernel_prepare_serial_activity_batch_with_arbiter_prestate',end)
    source=source[:insert]+publish+source[insert:owner]+source[reopen:main]
    source=re.sub(r'#include "(\.\./[^"\n]+)"',lambda m:'#include "'+str((ROOT/'tests/daemon'/m.group(1)).resolve())+'"',source)
    path=Path(path);path.parent.mkdir(parents=True,exist_ok=True);path.write_text(source)
def checked_replay(fixture,capture,admission):
    proof=(fixture/capture['metadata_proof']).read_bytes()
    key_length=int.from_bytes(proof[4:8],'big')
    key=proof[8:8+key_length]
    cursor=8+key_length
    value_length=int.from_bytes(proof[cursor:cursor+4],'big')
    metadata=proof[cursor+4:cursor+4+value_length]
    domain=b'LXP/program-replay-native/v1\0'
    if value_length!=394 or not metadata.startswith(domain) or len(metadata)!=394:
        raise RuntimeError('canonical native replay metadata missing')
    body=metadata[len(domain):]
    if key!=b'progreplay/v1/'+body[6:38] or int.from_bytes(body[2:6],'big')!=7:
        raise RuntimeError('metadata activity/network binding mismatch')
    blob=(fixture/capture['witness']).read_bytes()
    if hashlib.sha256(blob).digest()!=body[-32:]:
        raise RuntimeError('native witness digest mismatch')
    def field(data,offset):
        if offset+4>len(data): raise RuntimeError('replay length truncated')
        length=int.from_bytes(data[offset:offset+4],'big');offset+=4
        if length==0 or length>len(data)-offset: raise RuntimeError('replay field bounds')
        return data[offset:offset+length],offset+length
    native_domain=b'LXP/program-replay-native-blob/v1\0'
    if not blob.startswith(native_domain): raise RuntimeError('native replay domain mismatch')
    runtime,cursor=field(blob,len(native_domain))
    authority,cursor=field(blob,cursor)
    hosts,cursor=field(blob,cursor)
    if cursor!=len(blob) or hashlib.sha256(authority).digest()!=body[201:233] or hashlib.sha256(hosts).digest()!=body[233:265]:
        raise RuntimeError('actual native authority/host facts root mismatch')
    authority_domain=b'LXP/program-replay-authority/v1\0'
    if not authority.startswith(authority_domain) or not hosts.startswith(b'LXP/program-replay-hosts/v1\0'):
        raise RuntimeError('native checked facts domain mismatch')
    signed_activity,_=field(authority,len(authority_domain))
    if signed_activity!=Path(admission['activity_path']).read_bytes():
        raise RuntimeError('checked authority does not bind the signed additive request')
    runtime_domain=b'LXP/program-replay-record/v1\0'
    if not runtime.startswith(runtime_domain): raise RuntimeError('runtime replay domain mismatch')
    record=runtime[len(runtime_domain):]
    for source,destination,length in [(0,0,2),(2,110,32),(34,142,32),(66,174,12),(78,188,8),(86,196,5),(91,265,64)]:
        if record[source:source+length]!=body[destination:destination+length]:
            raise RuntimeError('runtime/native metadata binding mismatch')
    witness,cursor=field(record,155)
    if cursor!=len(record) or hashlib.sha256(witness).digest()!=body[297:329]:
        raise RuntimeError('canonical boundary witness digest mismatch')
    witness_domain=b'LXP/program-replay-witness/v1\0'
    if not witness.startswith(witness_domain): raise RuntimeError('boundary witness domain mismatch')
    cursor=len(witness_domain)
    count=int.from_bytes(witness[cursor:cursor+4],'big');cursor+=4
    if count!=int.from_bytes(body[197:201],'big') or not 0<count<=4096:
        raise RuntimeError('boundary count mismatch')
    hashes=[]
    for index in range(count):
        leaf,cursor=field(witness,cursor)
        if not leaf.startswith(b'LXP/program-replay-boundary/v1\0'):
            raise RuntimeError('actual engine boundary encoding missing')
        hashes.append(hashlib.sha256(b'LXP/program-replay-leaf/v1\0'+index.to_bytes(4,'big')+len(leaf).to_bytes(4,'big')+leaf).digest())
    if cursor!=len(witness): raise RuntimeError('trailing boundary witness bytes')
    while len(hashes)>1:
        hashes=[hashlib.sha256(b'LXP/program-replay-node/v1\0'+hashes[index]+hashes[index+1 if index+1<len(hashes) else index]).digest() for index in range(0,len(hashes),2)]
    if hashes[0]!=body[265:297]: raise RuntimeError('actual engine boundary Merkle root mismatch')
def exported_inputs(fixture):
    admission=json.loads((fixture/'inputs.json').read_text())
    replay=json.loads((fixture/'replay-inputs.json').read_text())
    if admission['network_id']!=7 or replay['admission_inputs']!='inputs.json':
        raise RuntimeError('actual network/admission binding missing')
    captures=replay['captures']
    if [(c['batch'],c['receipt_index']) for c in captures]!=[(3,0),(4,0),(4,1),(6,0)]:
        raise RuntimeError('serial/scheduled/trap replay corpus missing')
    admissions={Path(c['receipt_path']).name:c for c in admission['captures']}
    required=('v3_path','v2_path','v1_path','receipt_path','proof_path','activity_path','signing_preimage_path','authority_key_path','header_path','header_signature_path','maintenance_path','maintenance_proof_path')
    outputs={}
    for capture in captures:
        receipt=capture['receipt']
        if receipt not in admissions: raise RuntimeError('replay has no signed admission evidence')
        checked_replay(fixture,capture,admissions[receipt])
        for key in ('metadata_proof','witness','root'):
            path=(fixture/capture[key]).resolve(strict=True)
            if path.parent!=fixture.resolve() or path.stat().st_size==0:
                raise RuntimeError('portable replay evidence missing')
            outputs[str(path)]=sha(path)
        for key in required:
            path=Path(admissions[receipt][key]).resolve(strict=True)
            if path.parent!=fixture.resolve() or path.stat().st_size==0:
                raise RuntimeError('genuine receipt/admission evidence missing')
            outputs[str(path)]=sha(path)
        for receipt_path in admissions[receipt]['receipts']:
            path=Path(receipt_path).resolve(strict=True)
            if path.parent!=fixture.resolve(): raise RuntimeError('receipt chain escaped private fixture')
            outputs[str(path)]=sha(path)
    return outputs
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--prepare-fixture');parser.add_argument('--build',action='store_true');parser.add_argument('--native');parser.add_argument('--cargo',default='/root/.cargo/bin/cargo');parser.add_argument('--rust-target',default='/root/lx-target/arbiter-prestate/rust');args=parser.parse_args()
    if args.prepare_fixture: prepare(args.prepare_fixture);return 0
    EVIDENCE.mkdir(parents=True,mode=0o700,exist_ok=True)
    if EVIDENCE.stat().st_mode&0o077: raise RuntimeError('private evidence required')
    if args.build:
        source=sources();environment=dict(os.environ,CARGO_TARGET_DIR=args.rust_target,CARGO_BUILD_JOBS='4')
        output=execute([*shlex.split(args.cargo),'test','--locked','--manifest-path','programs/Cargo.toml','-p','layerx-programs-runtime','--test','program_replay','--no-run','--message-format=json'],EVIDENCE/'build-runtime-test.log',environment)
        artifacts=[json.loads(line) for line in output.splitlines() if line.startswith('{')]
        binaries={item['executable'] for item in artifacts if item.get('reason')=='compiler-artifact' and item.get('executable') and item.get('target',{}).get('name')=='program_replay'}
        if len(binaries)!=1 or source!=sources(): raise RuntimeError('whole source freeze and genuine test artifact required')
        native=Path(args.native).resolve(strict=True);rust=Path(binaries.pop()).resolve(strict=True)
        (EVIDENCE/'artifacts.json').write_text(json.dumps({'inputs':source,'native':str(native),'native_sha':sha(native),'rust':str(rust),'rust_sha':sha(rust)},indent=2));return 0
    manifest=json.loads((EVIDENCE/'artifacts.json').read_text())
    if sources()!=manifest['inputs'] or sha(manifest['native'])!=manifest['native_sha'] or sha(manifest['rust'])!=manifest['rust_sha']: raise RuntimeError('source/artifact mismatch')
    fixture=Path(tempfile.mkdtemp(prefix='native-',dir=EVIDENCE))
    output=execute([manifest['native'],str(fixture)],EVIDENCE/'verify-native.log')
    if 'PROGRAM_REPLAY real-signed-serial-scheduled-trap-proof-reopen-refusal' not in output: raise RuntimeError('native producer coverage incomplete')
    outputs=exported_inputs(fixture)
    execute([manifest['rust'],'--nocapture','--test-threads=1'],EVIDENCE/'verify-runtime.log')
    if sources()!=manifest['inputs']: raise RuntimeError('source changed during qualification')
    (EVIDENCE/'result.json').write_text(json.dumps({'command':'timeout 10m python3 tools/qualification/paxeer-x/program-replay-record.py','exit':0,'native_log':str(EVIDENCE/'verify-native.log'),'runtime_log':str(EVIDENCE/'verify-runtime.log'),'fixture':str(fixture),'inputs':outputs}));return 0
if __name__=='__main__':
    try: sys.exit(main())
    except (OSError,ValueError,RuntimeError,subprocess.SubprocessError) as error: print(error,file=sys.stderr);sys.exit(1)
