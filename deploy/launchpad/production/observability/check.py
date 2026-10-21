"""Fresh source-bound checks. Requires explicit reviewed native promtool3.2.1."""
import argparse,hashlib,json,os,pathlib,subprocess,sys
p=argparse.ArgumentParser();p.add_argument('--promtool',required=True);p.add_argument('--output',required=True);a=p.parse_args()
root=pathlib.Path(__file__).resolve().parent
version=subprocess.run([a.promtool,'--version'],capture_output=True,text=True,check=True)
if '3.2.1' not in version.stdout: raise SystemExit('promtool3.2.1 required')
checks=[]
for argv,cwd in [([sys.executable,'-m','unittest','discover','-s',str(root/'tests'),'-v'],root),([sys.executable,str(root/'contracts.py'),str(root/'config.json')],root),([a.promtool,'check','rules',str(root/'prometheus-rules.json')],root),([a.promtool,'test','rules','prometheus.test.yml'],root/'tests')]:
 r=subprocess.run(argv,cwd=cwd,capture_output=True,text=True,timeout=180)
 checks.append(dict(argv=argv,exit=r.returncode,stdout=r.stdout,stderr=r.stderr))
 if r.returncode:break
result=dict(version='1.0',checks=checks,passed=len(checks)==4 and all(x['exit']==0 for x in checks),capacity_qualified=False,collector_export_privacy_qualified=False,activation_allowed=False,source_hashes={str(f.relative_to(root)):hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted(root.rglob('*')) if f.is_file() and '__pycache__' not in f.parts},promtool_sha256=hashlib.sha256(pathlib.Path(a.promtool).read_bytes()).hexdigest())
out=pathlib.Path(a.output);out.parent.mkdir(parents=True,exist_ok=True);out.write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({k:result[k] for k in ('passed','capacity_qualified','collector_export_privacy_qualified','activation_allowed')}))
raise SystemExit(0 if result['passed'] else 1)
