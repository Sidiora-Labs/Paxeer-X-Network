from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import py_compile
import subprocess
import time

from contracts import canonical, digest, load, require, sha
from render import atomic_write, render


def build(binding,config,output,collector,collector_sha256,promtool,promtool_sha256):
    output=Path(output).resolve();output.mkdir(parents=True,exist_ok=True,mode=0o700)
    tools={}
    for name,path,expected in [('collector',collector,collector_sha256),('promtool',promtool,promtool_sha256)]:
        digest(expected);path=Path(path)
        require(path.is_file() and not path.is_symlink() and sha(path.read_bytes())==expected,'native tool identity differs')
        tools[name]=str(path.resolve())
    rendered=output/'configuration';manifest=render(binding,config,rendered)
    for path in sorted(Path(__file__).resolve().parent.rglob('*.py')):
        py_compile.compile(str(path),cfile=str(output/(sha(str(path).encode())+'.pyc')),doraise=True)
    commands=[
        [tools['collector'],'validate','--config',str(rendered/'otel-collector.yaml')],
        [tools['promtool'],'check','config',str(rendered/'prometheus.json')],
        [tools['promtool'],'check','rules',str(rendered/'prometheus-rules.json')]
    ]
    checks=[]
    for index,command in enumerate(commands):
        result=subprocess.run(command,cwd=rendered,capture_output=True,timeout=180,env=dict(os.environ))
        log=output/('native-%d.log'%index);atomic_write(log,result.stdout+b'\n'+result.stderr)
        checks.append({'command':command,'exit_code':result.returncode,'log_sha256':sha(log.read_bytes())})
        if result.returncode:break
    result={'version':'2.0','candidate_sha256':binding['candidate_sha256'],'input_interface_sha256':binding['input_interface_sha256'],'deployment_digest':binding['deployment_digest'],'configuration_revision':binding['configuration_revision'],'render_manifest_sha256':sha(canonical(manifest)),'binding_sha256':sha(canonical(binding)),'configuration_sha256':sha(canonical(config)),'source_templates':{name:sha((Path(__file__).resolve().parents[1]/name).read_bytes()) for name in ('prometheus-rules.json','otel-collector.yaml')},'tool_hashes':{'collector':collector_sha256,'promtool':promtool_sha256},'checked_at':time.time(),'checks':checks,'passed':len(checks)==3 and all(c['exit_code']==0 for c in checks)}
    atomic_write(output/'build.json',canonical(result))
    return result


def main():
    p=argparse.ArgumentParser();p.add_argument('--binding',required=True);p.add_argument('--config',required=True);p.add_argument('--output',required=True);p.add_argument('--collector',required=True);p.add_argument('--collector-sha256',required=True);p.add_argument('--promtool',required=True);p.add_argument('--promtool-sha256',required=True);a=p.parse_args()
    result=build(load(a.binding),load(a.config),a.output,a.collector,a.collector_sha256,a.promtool,a.promtool_sha256)
    print(json.dumps({'passed':result['passed'],'log':str(Path(a.output)/'build.json')}))
    raise SystemExit(0 if result['passed'] else 1)


if __name__=='__main__':
    main()
