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
EVIDENCE=Path(os.environ.get('PAXEER_X_PROGRAM_REPLAY_EVIDENCE','/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task1043520'))
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
    source=source[:source.index('int main(int argc, char **argv)')]
    source=re.sub(r'#include "(\.\./[^"\n]+)"',lambda m:'#include "'+str((ROOT/'tests/daemon'/m.group(1)).resolve())+'"',source)
    path=Path(path);path.parent.mkdir(parents=True,exist_ok=True);path.write_text(source)
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
    execute([manifest['rust'],'--nocapture','--test-threads=1'],EVIDENCE/'verify-runtime.log')
    if sources()!=manifest['inputs']: raise RuntimeError('source changed during qualification')
    (EVIDENCE/'result.json').write_text(json.dumps({'command':'timeout 10m python3 tools/qualification/paxeer-x/program-replay-record.py','exit':0,'native_log':str(EVIDENCE/'verify-native.log'),'runtime_log':str(EVIDENCE/'verify-runtime.log')}));return 0
if __name__=='__main__':
    try: sys.exit(main())
    except (OSError,ValueError,RuntimeError,subprocess.SubprocessError) as error: print(error,file=sys.stderr);sys.exit(1)
