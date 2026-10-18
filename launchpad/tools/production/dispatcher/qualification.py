#!/usr/bin/env python3
"""Actual isolated dispatcher qualification. No canonical task writes."""
from __future__ import annotations
import argparse, hashlib, json, os, pathlib, signal, statistics, subprocess, sys, tempfile, time, uuid
from datetime import datetime, timezone
sys.dont_write_bytecode=True
os.environ.setdefault("PYTHONDONTWRITEBYTECODE","1")
ROOT = pathlib.Path(__file__).resolve().parent
if str(ROOT.parent) not in sys.path: sys.path.insert(0, str(ROOT.parent))
from dispatcher.graph import Graph, GraphError
from dispatcher.store import Store, StoreError
from dispatcher.workspace import create_archive, create_workspace, run_process, WorkspaceError
from dispatcher.evidence import run_checks, publish, source_digest, EvidenceError
from dispatcher.patches import build_patch, stage_patches, PatchError
from dispatcher.qualification_paths import qualify_paths
from dispatcher.qualification_leases import qualify_expiry, qualify_resources
from dispatcher.qualification_timeout import qualify_timeout
from dispatcher.cli import host_limits
from dispatcher.qualification_cli import qualify_cli
from dispatcher.qualification_fixture import initial_states
SCENARIOS = ('whole-dag','fifty-process-leases','concurrent-control-benchmark','claim-race','expiry-takeover','stale-proof','overlap-and-path-safety','timeout-cancel-restart','resource-denial','false-success-rejection','barriers-and-integration')
CAPACITY = dict(cpu=50,memory=4*1024**3,pids=500,disk=1024**3,executor=50,go_build=2,container_start=2)
REQUEST = dict(cpu=.5,memory=64*1024**2,pids=2,disk=4*1024**2,executor=1,go_build=0,container_start=0)
def digest(data): return hashlib.sha256(data).hexdigest()
def write(path, value):
    path=pathlib.Path(path);path.parent.mkdir(parents=True,exist_ok=True);path.write_text(json.dumps(value,sort_keys=True,indent=2)+'\n'); return path

def expect_error(fn):
    try: fn()
    except (ValueError, RuntimeError, OSError): return
    raise AssertionError('Forbidden operation was accepted')
def store(path, capacity=None, boot='test'):
    return Store(path,capacity or CAPACITY,boot_id=boot,mode='process-harness')
def binding(candidate=None):
    return {k: candidate if k=='candidate' and candidate else digest(k.encode()) for k in ('baseline','candidate','interface','layout')}
def wait_files(paths,processes,timeout=30):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        failed=[(p.pid,p.poll()) for p in processes if p.poll() not in (None,0)]
        if failed: raise AssertionError(f'Workers failed: {failed}')
        if all(p.exists() for p in paths): return
        time.sleep(.02)
    raise TimeoutError('Worker barrier timed out')
def worker(config_path):
    c=json.loads(pathlib.Path(config_path).read_text()); d=pathlib.Path(c['workspace']); s=store(c['db'],boot=c['boot']); timings={}
    start=time.monotonic(); t=time.monotonic(); tok=s.claim(c['task'],c['lane'],f'pid-{os.getpid()}',c['binding'],REQUEST,ttl=15);timings['claim']=time.monotonic()-t
    import socket
    port_socket=socket.socket();port_socket.bind(('127.0.0.1',tok['allocation']['ports'][0]));port_socket.listen(1)
    write(d/'ready.json',{'pid':os.getpid(),'token':tok,'bound_port':port_socket.getsockname()[1]})
    while not pathlib.Path(c['start']).exists(): time.sleep(.01)
    started=time.monotonic();count=0;heartbeat=[]
    while time.monotonic()-started<1.6:
        t=time.monotonic();tok=s.heartbeat(tok,ttl=15);heartbeat.append(time.monotonic()-t);count+=1;time.sleep(.08)
    ended=time.monotonic();write(d/'heartbeats.json',{'count':count,'started':started,'ended':ended})
    while not pathlib.Path(c['finish']).exists():time.sleep(.01)
    t=time.monotonic();s.complete(tok);port_socket.close();timings['completion']=time.monotonic()-t;s.close()
    write(d/'result.json',{'pid':os.getpid(),'lane':c['lane'],'attempt':tok['attempt'],'fence':tok['fence'],'workspace':str(d),'heartbeat_count':count,'bound_port':tok['allocation']['ports'][0],'started_monotonic':started,'ended_monotonic':ended,'timings':timings,'heartbeat_seconds':heartbeat,'elapsed_seconds':time.monotonic()-start})
def race_worker(config_path):
    c=json.loads(pathlib.Path(config_path).read_text());s=store(c['db'],boot=c['boot'])
    while not pathlib.Path(c['barrier']).exists():time.sleep(.01)
    try: tok=s.claim('race','L01',f'pid-{os.getpid()}',binding(),REQUEST,ttl=15);v={'won':True,'token':tok}
    except StoreError as e:v={'won':False,'error':str(e)}
    write(c['output'],v);s.close()
class Harness:
    def __init__(self,args):
        self.args=args;self.evidence=pathlib.Path(args.evidence_dir).resolve();self.state=pathlib.Path(args.state_dir).resolve();self.evidence.mkdir(parents=True,exist_ok=True);self.state.mkdir(parents=True,exist_ok=True);self.graph=Graph(args.spec,args.plan);self.process_proof=None;self.bench=None
    def directory(self,name):
        p=self.state/name;p.mkdir(exist_ok=False);return p
    def whole_dag(self):
        start=time.monotonic();g=Graph(self.args.spec,self.args.plan);duration=time.monotonic()-start;assert len(g.tasks)==208 and duration<30
        states=initial_states(g.tasks);ready=g.ready(states);assert len(ready)==50
        return {'checks':3,'tasks':len(g.tasks),'edges':sum(len(x.get('requires',[])) for x in g.tasks.values()),'ready_tasks':ready,'graph_validation_seconds':duration,'duration_seconds':duration,'layout_digest':g.layout_digest,'dispatcher_version':'1.0.0','python_version':sys.version}

    def fifty_process_leases(self):
        d=self.directory('fifty');base=d/'baseline';base.mkdir();(base/'input.txt').write_text('immutable process scheduling fixture\n');archive=d/'baseline.tar';base_digest=create_archive(base,archive)
        host=host_limits(d)
        for key in ('cpu','memory','pids','disk'):assert host[key]>=REQUEST[key]*50, f'Insufficient measured host {key} for 50 fixture envelopes'
        boot='fifty-'+uuid.uuid4().hex;db=d/'leases.sqlite';s=store(db,boot=boot);states=initial_states(self.graph.tasks);tasks=self.graph.ready(states);assert len(tasks)==50
        startbarrier=d/'start';finish=d/'finish';children=[];paths=[];logs=[];t0=time.monotonic();b=binding(base_digest)
        try:
            for task in tasks:
                lane=self.graph.tasks[task]['lane'];w=d/lane;create_workspace(archive,w);c=write(d/(lane+'.json'),{'db':str(db),'boot':boot,'task':task,'lane':lane,'workspace':str(w),'binding':b,'start':str(startbarrier),'finish':str(finish)});log=open(d/(lane+'.log'),'w');logs.append(log);p=subprocess.Popen([sys.executable,str(ROOT/'qualification.py'),'--worker',str(c)],stdout=log,stderr=subprocess.STDOUT,start_new_session=True);children.append(p);paths.append(w)
            wait_files([p/'ready.json' for p in paths],children);active=s.active();assert len(active)==50;assert len({x['lane'] for x in active})==50
            measured={'status':[],'readiness':[]}
            for _ in range(50):
                t=time.monotonic();assert len(s.active())==50;measured['status'].append(time.monotonic()-t)
                t=time.monotonic();assert len(self.graph.ready(states))==50;measured['readiness'].append(time.monotonic()-t)
            observed_memory=[]
            for process in children:
                lines=pathlib.Path(f'/proc/{process.pid}/status').read_text().splitlines();rss=int(next(x.split()[1] for x in lines if x.startswith('VmHWM:')))*1024;assert rss<=REQUEST['memory'];observed_memory.append({'pid':process.pid,'observed_high_water_rss_bytes':rss})
            startbarrier.touch();wait_files([p/'heartbeats.json' for p in paths],children);assert len(s.active())==50;finish.touch()
            for p in children: assert p.wait(timeout=15)==0
            proof=[json.loads((p/'result.json').read_text()) for p in paths];overlap=min(x['ended_monotonic'] for x in proof)-max(x['started_monotonic'] for x in proof);assert overlap>=1 and len({x['pid'] for x in proof})==50 and all(x['heartbeat_count']>0 for x in proof)
            assert not s.active();s.close();self.process_proof=proof
            for name in ('claim','completion'):measured[name]=[x['timings'][name] for x in proof]
            measured['heartbeat']=[t for x in proof for t in x['heartbeat_seconds']];self.bench={name:{'count':len(v),'min_seconds':min(v),'max_seconds':max(v),'median_seconds':statistics.median(v),'p95_seconds':sorted(v)[int((len(v)-1)*.95)],'errors':0} for name,v in measured.items()}
            return {'checks':8,'processes':proof,'overlap_seconds':overlap,'actual_os_processes':50,'actual_ai_agents':0,'executor':'process-harness','elapsed_seconds':time.monotonic()-t0,'baseline_archive_sha256':base_digest,'host_admission':host,'per_process_resources':REQUEST,'observed_process_memory':observed_memory}
        finally:
            for p in children:
                if p.poll() is None:
                    os.killpg(p.pid,signal.SIGKILL);p.wait()
            for log in logs:log.close()
    def concurrent_control_benchmark(self):
        if self.bench is None:self.fifty_process_leases()
        assert set(self.bench)=={'status','readiness','claim','heartbeat','completion'} and all(v['count']>=50 and v['errors']==0 for v in self.bench.values())
        return {'checks':5,'operations':self.bench,'at_least_50_active_during_status_and_readiness':True}
    def claim_race(self):
        d=self.directory('race');db=d/'leases.sqlite';boot=uuid.uuid4().hex;s=store(db,boot=boot);barrier=d/'go';ps=[]
        for i in range(2):
            c=write(d/f'{i}.json',{'db':str(db),'boot':boot,'barrier':str(barrier),'output':str(d/f'result{i}.json')});ps.append(subprocess.Popen([sys.executable,str(ROOT/'qualification.py'),'--race-worker',str(c)]))
        barrier.touch()
        for p in ps:assert p.wait(timeout=15)==0
        results=[json.loads((d/f'result{i}.json').read_text()) for i in range(2)];assert sum(x['won'] for x in results)==1 and len(s.active())==1;s.release(next(x['token'] for x in results if x['won']));s.close();return {'checks':3,'results':results,'pids':[p.pid for p in ps]}
    def expiry_takeover(self):
        result=qualify_expiry(self.directory('expiry-real'));result['checks']=len(result['rejected'])+3;return result
    def evidence_fixture(self,name):
        d=self.directory(name);src=d/'source';src.mkdir();(src/'check.py').write_text("from pathlib import Path\nassert Path('input.txt').read_text() == 'good'\nprint('actual checker passed')\n");(src/'input.txt').write_text('good');s=store(d/'state.db');b=binding(source_digest(src));tok=s.claim(name,'L01','coordinator',b,REQUEST,ttl=30);checks=[{'id':'real-check','command':[sys.executable,'check.py'],'timeout':5,'inputs':['input.txt']}];secret=os.urandom(32);result=run_checks(checks,src,d/'receipts',b,tok,secret);return d,src,s,tok,checks,secret,result
    def stale_proof(self):
        d,src,s,tok,checks,secret,result=self.evidence_fixture('stale');verified=publish(result,secret,s,src,checks);assert verified
        (src/'input.txt').write_text('changed');expect_error(lambda:publish(result,secret,s,src,checks));(src/'input.txt').write_text('good')
        for key in ('baseline','candidate','interface','layout'):
            mutated=json.loads(json.dumps(result));mutated.setdefault('binding',{})[key]='0'*64;expect_error(lambda m=mutated:publish(m,secret,s,src,checks))
        s.release(tok);expect_error(lambda:publish(result,secret,s,src,checks));s.close();return {'checks':7,'rejected':['changed-input','baseline','candidate','interface','layout','expired-token']}
    def overlap_and_path_safety(self):
        return qualify_paths(self.directory('paths-real'))
    def timeout_cancel_restart(self):
        actual=qualify_timeout(self.directory('timeout-real'))
        d=self.directory('timeout');src=d/'src';src.mkdir();r=run_process([sys.executable,'-c','import subprocess,time; subprocess.Popen(["sleep","60"]); time.sleep(60)'],src,.2);assert r['timed_out'] and r['exit_code']!=0
        s=store(d/'state.db',boot='before');old=s.claim('restart','L01','old',binding(),REQUEST,ttl=15);s.close();new=store(d/'state.db',boot='after');expect_error(lambda:new.heartbeat(old));tok=new.claim('restart','L01','new',binding(),REQUEST,ttl=15);assert tok['fence']>old['fence'];new.release(tok);new.close();return {'checks':8,'signal_cleanup':actual,'timeout':r,'restart_old_fence':old['fence'],'restart_new_fence':tok['fence']}
    def resource_denial(self):
        result=qualify_resources(self.directory('resources-real'));result['check_ids']=result['checks'];result['checks']=len(result['check_ids']);return result
    def false_success_rejection(self):
        module_run=run_process([sys.executable,'-m','unittest','discover','-s',str(ROOT/'tests'),'-t',str(ROOT.parent),'-v'],ROOT.parent,90,env=dict(os.environ,PYTHONPATH=str(ROOT.parent),PYTHONDONTWRITEBYTECODE='1'))
        from dispatcher.evidence import _proof
        module_proof=_proof(module_run,'unittest');assert module_run['exit_code']==0 and module_run['cleanup'] and module_proof['positive_count']>0
        d,src,s,tok,checks,secret,result=self.evidence_fixture('false-success');expect_error(lambda:publish({'status':'passed'},secret,s,src,checks));expect_error(lambda:publish(result,os.urandom(32),s,src,checks));expect_error(lambda:publish(result,secret,s,src,[*checks,{'id':'missing','command':[sys.executable,'missing.py'],'timeout':1}]))
        expect_error(lambda:run_checks([],src,d/'empty',binding(source_digest(src)),tok,secret))
        bad=[{'id':'real-check','command':[sys.executable,'-c','raise SystemExit(7)'],'timeout':5}];failed=run_checks(bad,src,d/'failed',binding(source_digest(src)),tok,secret);expect_error(lambda:publish(failed,secret,s,src,bad));s.release(tok);s.close();return {'checks':6,'module_tests':module_run,'module_test_proof':module_proof,'rejected':['forged-json','wrong-hmac','missing-check','empty-scenarios','nonzero-exit']}
    def barriers_and_integration(self):
        states=initial_states(self.graph.tasks);ready=self.graph.ready(states);assert len(ready)==50
        for task in ready:states[task]='done'
        states[ready[-1]]='implemented';assert '1.99' not in self.graph.ready(states);states[ready[-1]]='done';assert '1.99' in self.graph.ready(states)
        actual_cli=qualify_cli(self.directory('coordinator-cli'),self.args.spec,self.args.plan)
        d=self.directory('integration');base=d/'base';base.mkdir();(base/'lane').mkdir();(base/'lane/a').write_text('old');import shutil;c=d/'candidate';shutil.copytree(base,c);(c/'lane/a').write_text('new');patch=build_patch(base,c,['lane/**'],d/'patch');ownership={patch['patch_digest']:['lane/**']}
        a=stage_patches(base,[patch],d/'stage1',verify=lambda _:True,ownership=ownership);b=stage_patches(base,[patch],d/'stage2',verify=lambda _:True,ownership=ownership);assert a['result_digest']==b['result_digest'];expect_error(lambda:stage_patches(base,[patch],d/'failed',verify=lambda _:False,ownership=ownership));assert (base/'lane/a').read_text()=='old';return {'checks':actual_cli['checks']+5,'coordinator_cli':actual_cli,'staged_digest':a['result_digest'],'canonical_untouched':True}
    def run(self):
        started=datetime.now(timezone.utc).isoformat();source_before=source_digest(ROOT);results=[];failed=False
        ids=SCENARIOS if self.args.case=='all' else (self.args.case,)
        for case in ids:
            t=time.monotonic()
            try:
                v=getattr(self,case.replace('-','_'))()
                if type(v.get('checks')) is not int or v['checks']<=0 or v.get('passed',True) is not True:raise AssertionError('Empty, skipped, malformed or failed scenario result')
                v={'id':case,'status':'passed',**v}
            except Exception as e:
                import traceback;v={'id':case,'status':'failed','checks':0,'error':str(e),'traceback':traceback.format_exc()};failed=True
            v['duration_seconds']=time.monotonic()-t;artifact=write(self.evidence/(case+'.json'),v);v['artifacts']={artifact.name:digest(artifact.read_bytes())};results.append(v)
        source_after=source_digest(ROOT)
        if source_after!=source_before:failed=True
        source=os.environ.get('QUALIFICATION_CANDIDATE_SHA256') or source_before
        output={'schema_version':1,'task':'0.3','status':'failed' if failed else 'passed','source_sha256':source,'dispatcher_source_before':source_before,'dispatcher_source_after':source_after,'binding_mode':'runner-candidate' if os.environ.get('QUALIFICATION_CANDIDATE_SHA256') else 'standalone-dispatcher-source','started_at':started,'scenarios':results,'limitations':['OS process scheduling proof is separate from AI-agent capacity.','Workflow controls do not sandbox malicious same-UID code.','No product feature or 500000-DAU qualification.','Fixture ledger only; canonical task statuses never modified.']};write(self.evidence/'result.json',output);print(json.dumps({'status':output['status'],'result':str(self.evidence/'result.json'),'scenarios':len(results)}));return int(failed)
def main():
    p=argparse.ArgumentParser();p.add_argument('--spec');p.add_argument('--plan');p.add_argument('--state-dir');p.add_argument('--evidence-dir');p.add_argument('--case',default='all',choices=('all',*SCENARIOS));p.add_argument('--worker');p.add_argument('--race-worker');a=p.parse_args()
    if a.worker:worker(a.worker);return 0
    if a.race_worker:race_worker(a.race_worker);return 0
    if not all((a.spec,a.plan,a.state_dir,a.evidence_dir)):p.error('--spec --plan --state-dir --evidence-dir required')
    return Harness(a).run()
if __name__=='__main__':raise SystemExit(main())
