#!/usr/bin/env python3
import argparse, importlib.util, json, os, pathlib, subprocess, sys

def run_bounded(lifecycle, command, **kwargs):
    child=subprocess.Popen(command,start_new_session=True,**kwargs)
    try:
        return child.wait(timeout=600)
    finally:
        lifecycle.stop(child)


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--output-dir',type=pathlib.Path,required=True)
    parser.add_argument('--phase',type=int,choices=(2,3,4),default=2)
    parser.add_argument('--release-digest',required=True)
    parser.add_argument('--interface-digest',required=True)
    args=parser.parse_args()
    for value in (args.release_digest,args.interface_digest):
        if len(value)!=64 or any(c not in '0123456789abcdef' for c in value):
            raise RuntimeError('current release/interface SHA256 required')
    if args.phase == 4:
        config = os.environ.get('L18_MEASUREMENT_CONFIG')
        if not config:
            raise RuntimeError('actual private L18_MEASUREMENT_CONFIG required')
        output = args.output_dir.resolve()
        output.mkdir(parents=True,exist_ok=False,mode=0o700)
        location = pathlib.Path(__file__).resolve()
        spec = importlib.util.spec_from_file_location('l18_fixture_lifecycle',location.with_name('qualify.py'))
        lifecycle = importlib.util.module_from_spec(spec);spec.loader.exec_module(lifecycle)
        lifecycle.install_signal_handlers()
        env = os.environ.copy()
        env.update(L18_MEASUREMENT_OUTPUT=str(output/'measurement'),L18_RELEASE_DIGEST=args.release_digest,L18_INTERFACE_DIGEST=args.interface_digest)
        command = [sys.executable,str(location.with_name('measurement_test.py'))]
        code = None
        try:
            with (output/'measurement-tests.log').open('x') as log:
                code = run_bounded(lifecycle,command,cwd=location.parents[5],env=env,stdout=log,stderr=subprocess.STDOUT)
        finally:
            (output/'commands.json').write_text(json.dumps({'phase':4,'release_digest':args.release_digest,'interface_digest':args.interface_digest,'commands':[{'command':command,'exit':code}],'capacity_qualified':False,'payout_qualified':False,'global_qualification_owner':'serial9.1'},indent=2)+'\n')
        return code
    if not os.environ.get('L18_POSTGRES_DSN'):
        raise RuntimeError('root-admitted real PostgreSQL required; no fallback DSN')
    location=pathlib.Path(__file__).resolve()
    spec=importlib.util.spec_from_file_location('l18_fixture_lifecycle',location.with_name('qualify.py'))
    lifecycle=importlib.util.module_from_spec(spec);spec.loader.exec_module(lifecycle)
    lifecycle.install_signal_handlers();lifecycle.refuse_occupied_port()
    output=args.output_dir.resolve();output.mkdir(parents=True,exist_ok=False)
    commands=[];child=None
    try:
        with (output/'anvil.log').open('x') as log:
            child=subprocess.Popen(['anvil','--host','127.0.0.1','--port','21800','--chain-id','125','--silent'],stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
            lifecycle.wait_ready(child)
            env=os.environ.copy();env['L18_ANVIL_IDENTITY']=json.dumps({'pid':child.pid,'starttime':lifecycle.process_start(child.pid)})
            fixture=output/'fixture.json';lifecycle.assert_owned(child)
            with fixture.open('x') as out,(output/'fixture-error.log').open('x') as err:
                command=['node',str(location.with_name('phase2_fixture.cjs'))]
                code=run_bounded(lifecycle,command,cwd=location.parents[5],env=env,stdout=out,stderr=err);commands.append({'command':command,'exit':code})
            if code:raise RuntimeError('real contract fixture failed')
            env.update(GOWORK='off',GOMAXPROCS='2',GOFLAGS='-mod=readonly',L18_CHAIN_FIXTURE=str(fixture),L18_RELEASE_DIGEST=args.release_digest,L18_INTERFACE_DIGEST=args.interface_digest)
            command=['go','test','-p','1','-count=1','-json','-run','^TestPhase'+str(args.phase),'./internal/production/l18','./internal/referral'];lifecycle.assert_owned(child)
            with (output/'go.jsonl').open('x') as out:
                code=run_bounded(lifecycle,command,cwd=location.parents[3],env=env,stdout=out,stderr=subprocess.STDOUT);commands.append({'command':command,'exit':code})
            if code:raise RuntimeError('phase'+str(args.phase)+' focused behavior failed')
    finally:
        if child is not None:lifecycle.stop(child)
        (output/'commands.json').write_text(json.dumps({'phase':args.phase,'release_digest':args.release_digest,'interface_digest':args.interface_digest,'commands':commands,'capacity_qualified':False,'payout_qualified':False},indent=2)+'\n')
    capabilities=location.parents[5]/'spec/production-system/handoffs/L18/phase2/capabilities.json'
    blockers=json.loads(capabilities.read_text())['mandatory_release_blockers']
    if blockers:
        (output/'remaining-blockers.json').write_text(json.dumps(blockers,indent=2)+'\n')
        return 2
    return 0

if __name__=='__main__':sys.exit(main())
