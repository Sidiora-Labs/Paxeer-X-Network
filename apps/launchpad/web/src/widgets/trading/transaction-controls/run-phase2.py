#!/usr/bin/env python3
import argparse, hashlib, json, os, pathlib, subprocess, sys

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--fixture',required=True)
    parser.add_argument('--fixture-sha256',required=True)
    parser.add_argument('--rpc-url',required=True)
    parser.add_argument('--compiler',required=True)
    parser.add_argument('--compiler-sha256',required=True)
    parser.add_argument('--output-dir',required=True)
    args=parser.parse_args()
    root=pathlib.Path(__file__).resolve().parents[7]
    output=pathlib.Path(args.output_dir).resolve();output.mkdir(parents=True,exist_ok=False)
    fixture=pathlib.Path(args.fixture).resolve();compiler=pathlib.Path(args.compiler).resolve()
    for path,digest in [(fixture,args.fixture_sha256),(compiler,args.compiler_sha256)]:
        if hashlib.sha256(path.read_bytes()).hexdigest()!=digest:raise RuntimeError('Bound input hash mismatch: '+str(path))
    source=pathlib.Path(__file__).parent/'fixtures/ZeroResetToken.sol'
    compiler_input={'language':'Solidity','sources':{'ZeroResetToken.sol':{'content':source.read_text()}},'settings':{'outputSelection':{'*':{'*':['abi','evm.bytecode.object']}}}}
    compiled=subprocess.run([str(compiler),'--standard-json'],input=json.dumps(compiler_input),text=True,capture_output=True,timeout=60)
    (output/'compiler.log').write_text(compiled.stdout+'\n'+compiled.stderr)
    if compiled.returncode:return compiled.returncode
    result=json.loads(compiled.stdout)
    if any(error.get('severity')=='error' for error in result.get('errors',[])):raise RuntimeError('Actual fixture compilation failed')
    token=result['contracts']['ZeroResetToken.sol']['ZeroResetToken']
    artifact=output/'ZeroResetToken.json';artifact.write_text(json.dumps({'abi':token['abi'],'bytecode':'0x'+token['evm']['bytecode']['object'],'source_sha256':hashlib.sha256(source.read_bytes()).hexdigest()}))
    env={**os.environ,'L26_FIXTURE_JSON':str(fixture),'L26_FIXTURE_SHA256':args.fixture_sha256,'L26_RPC_URL':args.rpc_url,'L26_ZERO_RESET_ARTIFACT':str(artifact),'L26_CACHE_DIR':str(output/'vite-cache'),'WALLET_TEST_RPC_URL':args.rpc_url,'WALLET_TEST_ALLOW_CHAIN_MUTATION':'1'}
    web=root/'client/apps/web'
    command=['node',str(web/'node_modules/vitest/vitest.mjs'),'run','--config','src/widgets/trading/transaction-controls/vitest.phase2.config.ts','--reporter=tap','--reporter=json','--outputFile='+str(output/'vitest.json')]
    completed=subprocess.run(command,cwd=web,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=240)
    (output/'native.log').write_text(completed.stdout)
    sys.stdout.write(completed.stdout);sys.stdout.flush()
    code=completed.returncode
    (output/'result.json').write_text(json.dumps({'command':command,'exit_code':code,'fixture_sha256':args.fixture_sha256,'compiler_sha256':args.compiler_sha256,'native_log':str(output/'native.log')}))
    return code
if __name__=='__main__':sys.exit(main())
