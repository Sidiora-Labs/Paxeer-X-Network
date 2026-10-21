import os, pathlib, subprocess, time, urllib.request, json, socket, sys, argparse, hashlib, datetime, signal
root=pathlib.Path(__file__).resolve().parents[6]
owned=pathlib.Path(__file__).resolve().parent

if '--phase4' in sys.argv[1:]:
    parser=argparse.ArgumentParser(description='Measure actual creation browser and private chain candidate')
    parser.add_argument('--phase4',action='store_true',required=True)
    parser.add_argument('--config',type=pathlib.Path,required=True)
    parser.add_argument('--output',type=pathlib.Path,required=True)
    args=parser.parse_args()
    output=args.output.resolve()
    if output.exists() and any(output.iterdir()):
        raise SystemExit('Phase4 output must be a fresh directory; stale evidence is rejected')
    output.mkdir(parents=True,exist_ok=True,mode=0o700)
    environment={**os.environ,'L27_PHASE4':'1','L27_PHASE4_CONFIG':str(args.config.resolve()),'L27_SOURCE_ROOT':str(root.resolve()),'L27_PHASE4_OUTPUT':str(output)}
    started=datetime.datetime.now(datetime.timezone.utc).isoformat()
    try:
        with (output/'runner.log').open('wb') as phase4log:
            os.chmod(output/'runner.log',0o600)
            execution=subprocess.Popen(['node',str(owned/'creation-chain.fixture.cjs')],cwd=root,env=environment,stdout=phase4log,stderr=subprocess.STDOUT,start_new_session=True)
            try:
                code=execution.wait(timeout=1800)
            except subprocess.TimeoutExpired:
                os.killpg(execution.pid,signal.SIGTERM)
                try:
                    execution.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(execution.pid,signal.SIGKILL)
                    execution.wait()
                code=124
    except OSError:
        code=127
    result_path=output/'result.json'
    if code==0 and not result_path.is_file():
        code=1
    if result_path.is_file():
        result=json.loads(result_path.read_text())
        if result.get('status')!='observed_lane_checks_passed' or result.get('scenarioCount')!=8 or result.get('failures') or result.get('productionQualified') is not False:
            code=code or 1
    summary={'task':'4.27','startedAt':started,'finishedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'exitCode':code,'logPath':str(output/'runner.log'),'logSha256':hashlib.sha256((output/'runner.log').read_bytes()).hexdigest() if (output/'runner.log').is_file() else None,'resultPath':str(result_path) if result_path.is_file() else None,'harnessSha256':hashlib.sha256((owned/'creation-chain.fixture.cjs').read_bytes()).hexdigest(),'sourceOnly':False,'productionQualified':False,'sharedLoadRestore':'deferred_to_serial9.1'}
    (output/'runner-result.json').write_text(json.dumps(summary,indent=2)+'\n')
    os.chmod(output/'runner-result.json',0o600)
    raise SystemExit(code)

evidence=root/'evidence/production-system/L27'
evidence.mkdir(parents=True,exist_ok=True)
probe=socket.socket(socket.AF_INET,socket.SOCK_STREAM)
try:
    probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    probe.bind(('127.0.0.1',22700))
finally:
    probe.close()
log=open(evidence/'anvil.log','w')
child=subprocess.Popen(['/root/.foundry/bin/anvil','--chain-id','125','--port','22700','--host','127.0.0.1'],stdout=log,stderr=subprocess.STDOUT)
try:
    for _ in range(100):
        if child.poll() is not None: raise RuntimeError('Owned Anvil exited; refuse existing listener')
        try:
            request=urllib.request.Request('http://127.0.0.1:22700',data=b'{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}',headers={'Content-Type':'application/json'})
            if json.load(urllib.request.urlopen(request,timeout=1))['result']=='0x7d': break
        except Exception: time.sleep(.1)
    else: raise RuntimeError('Owned Anvil readiness timeout')
    time.sleep(.3)
    if child.poll() is not None: raise RuntimeError('Lost owned child before dispatch')
    env={**os.environ,'CONTRACT_SOURCE_ROOT':str(root.parent/'contracts'),'L27_OWNED_ANVIL_PID':str(child.pid),'FIXTURE_OUTPUT':str(evidence/'actual-factory-receipts.json')}
    phase2='--phase2' in sys.argv[1:]
    if phase2: env['L27_PHASE2']='1'
    result=subprocess.run(['node',str(owned/'creation-chain.fixture.cjs')],env=env,timeout=180)
    if result.returncode==0 and phase2:
        env['L27_PHASE2_FIXTURE']=env['FIXTURE_OUTPUT']
        result=subprocess.run(['pnpm','exec','vitest','run','src/widgets/create/creation-journey.integration.test.tsx'],cwd=root/'client/apps/web',env=env,timeout=180)
    if child.poll() is not None: raise RuntimeError('Lost owned child during checks')
    raise SystemExit(result.returncode)
finally:
    if child.poll() is None:
        child.terminate()
        try: child.wait(timeout=5)
        except subprocess.TimeoutExpired: child.kill();child.wait()
    log.close()
