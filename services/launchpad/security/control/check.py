#!/usr/bin/env python3
"""Reconstruct an isolated module using only owned source and baseline manifests.
No network dependency install, no shared source edits. Starts its own chain125
Anvil, fails if the allocated port is occupied, and cleans only its child.
"""
import json,os,pathlib,shutil,socket,subprocess,tempfile,time,urllib.request,signal,sys
root=pathlib.Path(__file__).resolve().parents[4]
port=24700
with socket.socket() as sock:
 sock.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 try:sock.bind(('127.0.0.1',port))
 except OSError:raise SystemExit('owned Anvil port occupied; refusing to reuse or mutate it')
with tempfile.TemporaryDirectory(prefix='kindle-l47-check-') as tmp:
 tmp=pathlib.Path(tmp)
 for file in (root/'services/launchpad/security/control').glob('*.go'):shutil.copy2(file,tmp/file.name)
 mod=(root/'sdk/launchpad/shared/go.mod').read_text().replace('=> ../protocol','=> '+str(root/'sdk/launchpad/protocol'))
 (tmp/'go.mod').write_text(mod);shutil.copy2(root/'sdk/launchpad/shared/go.sum',tmp/'go.sum')
 ipc=tmp/'anvil.ipc'
 log=open(tmp/'anvil.log','wb')
 node=subprocess.Popen(['/root/.foundry/bin/anvil','--host','127.0.0.1','--port',str(port),'--chain-id','125','--ipc',str(ipc),'--silent'],stdout=log,stderr=log,start_new_session=True)
 try:
  for _ in range(100):
   if node.poll() is not None:raise RuntimeError('owned Anvil child died: '+(tmp/'anvil.log').read_text())
   if ipc.exists():break
   time.sleep(.05)
  else:raise RuntimeError('owned Anvil startup timed out')
  env=dict(os.environ,GOWORK='off',GOMAXPROCS='2',GOFLAGS='-mod=readonly',L47_ANVIL_URL=str(ipc),L47_SOURCE_ROOT=str(root))
  result=subprocess.run(['go','test','-race','-p','1','-count=1','-json','.'],cwd=tmp,env=env)
  if node.poll() is not None:raise RuntimeError('owned Anvil child exited during checks')
  sys.exit(result.returncode)
 finally:
  if node.poll() is None:
   os.killpg(node.pid,signal.SIGTERM)
   try:node.wait(timeout=3)
   except subprocess.TimeoutExpired:os.killpg(node.pid,signal.SIGKILL);node.wait()
  log.close()
