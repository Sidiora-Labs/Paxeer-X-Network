#!/usr/bin/env python3
"""Single-coordinator dispatcher adapter; never changes canonical task status."""
from __future__ import annotations
import argparse, fcntl, hashlib, json, os, pathlib, shutil, sys, tempfile, time, uuid
sys.dont_write_bytecode=True
os.environ.setdefault("PYTHONDONTWRITEBYTECODE","1")
HERE=pathlib.Path(__file__).resolve().parent
sys.path.insert(0,str(HERE.parent))
from dispatcher.graph import Graph
from dispatcher.store import Store
from dispatcher.workspace import create_archive,create_workspace,namespace
from dispatcher.evidence import source_digest,run_checks,publish
from dispatcher.patches import build_patch,stage_patches
from dispatcher.acceptance import accept_artifact,stage_accepted,verify_accepted

def load(path):return json.loads(pathlib.Path(path).read_text())
def save(path,data):
    p=pathlib.Path(path);p.parent.mkdir(parents=True,exist_ok=True);fd,tmp=tempfile.mkstemp(dir=p.parent,prefix='.dispatch-')
    try:
        with os.fdopen(fd,'w') as f:json.dump(data,f,sort_keys=True,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
        os.replace(tmp,p)
    finally:pathlib.Path(tmp).unlink(missing_ok=True)

def host_limits(root):
    """Current conservative upper bounds, not a load-capacity certification."""
    memory=next(int(x.split()[1])*1024 for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:'))
    cpu=float(len(os.sched_getaffinity(0)));pids=65536
    cgline=next((x.split(':',2)[2] for x in pathlib.Path('/proc/self/cgroup').read_text().splitlines() if x.startswith('0::')), '/')
    cg=pathlib.Path('/sys/fs/cgroup')/cgline.lstrip('/')
    for p in (cg,*cg.parents):
        if p==pathlib.Path('/sys/fs'):break
        if (p/'memory.max').exists():
            maximum=(p/'memory.max').read_text().strip()
            if maximum!='max':memory=min(memory,max(0,int(maximum)-int((p/'memory.current').read_text())))
        if (p/'pids.max').exists():
            maximum=(p/'pids.max').read_text().strip()
            if maximum!='max':pids=min(pids,max(0,int(maximum)-int((p/'pids.current').read_text())))
        if (p/'cpu.max').exists():
            quota,period=(p/'cpu.max').read_text().split()
            if quota!='max':cpu=min(cpu,int(quota)/int(period))
    return {'cpu':cpu,'memory':memory,'pids':pids,'disk':shutil.disk_usage(root).free}
def graph(config):return Graph(config['spec'],config['plan'])
def ledger(config):return Store(config['db'],config['capacity'],boot_id=config['boot'],mode=config['mode'],total_slots=config['total_slots'])
def config_path(a):return pathlib.Path(a.state_dir).resolve()/'config.json'
def check_registry(c,task):
    d=load(c['registry']);checks=d.get(task)
    if not isinstance(checks,list) or not checks:raise ValueError('No coordinator-reviewed checks registered for task; qualification fails closed')
    return checks

def main():
    p=argparse.ArgumentParser();p.add_argument('--state-dir',required=True);sp=p.add_subparsers(dest='command',required=True)
    init=sp.add_parser('init');init.add_argument('--spec',required=True);init.add_argument('--plan',required=True);init.add_argument('--source',required=True);init.add_argument('--interface-digest',required=True);init.add_argument('--capacity',required=True);init.add_argument('--mode',choices=('ai','process-harness'),default='ai');init.add_argument('--total-slots',type=int,default=31);init.add_argument('--available-ai-slots',type=int);init.add_argument('--fixture',action='store_true')
    sp.add_parser('status');sp.add_parser('graph');sp.add_parser('restart');obs=sp.add_parser('observe-ai');obs.add_argument('--available-ai-slots',type=int,required=True)
    cl=sp.add_parser('claim');cl.add_argument('--task',required=True);cl.add_argument('--owner',required=True);cl.add_argument('--resources',required=True);cl.add_argument('--ttl',type=float,default=1800)
    for name in ('heartbeat','release','candidate','patch','qualify','complete','accept'):
        x=sp.add_parser(name);x.add_argument('--token',required=True)
        if name=='heartbeat':x.add_argument('--ttl',type=float,default=1800)
    reg=sp.add_parser('register-checks');reg.add_argument('--task',required=True);reg.add_argument('--checks',required=True)
    st=sp.add_parser('stage');st.add_argument('--accepted',nargs='+',required=True);st.add_argument('--output',required=True);st.add_argument('--checks',required=True);st.add_argument('--wave',type=int,required=True);st.add_argument('--fixture-partial',action='store_true')
    a=p.parse_args();state=pathlib.Path(a.state_dir).resolve();cp=config_path(a)
    if a.command=='init':
        if cp.exists():raise ValueError('State already initialized')
        state.mkdir(parents=True,exist_ok=True);os.chmod(state,0o700);g=Graph(a.spec,a.plan);limits=host_limits(state);cap=load(a.capacity)
        if a.mode=='ai' and (a.total_slots!=31 or a.available_ai_slots is None or not 0<=a.available_ai_slots<=30 or cap.get('executor',100)>a.available_ai_slots):raise ValueError('AI admission requires current coordinator observation within actual31-total ceiling')
        for k,v in limits.items():
            if cap.get(k,float('inf'))>v:raise ValueError('Capacity exceeds current host headroom: '+k)
        if len(a.interface_digest)!=64 or any(x not in '0123456789abcdef' for x in a.interface_digest):raise ValueError('Invalid frozen interface digest')
        archive=state/'baseline.tar';archive_sha=create_archive(a.source,archive);base=state/'baseline';create_workspace(archive,base);c={'version':1,'fixture':a.fixture,'spec':str(pathlib.Path(a.spec).resolve()),'plan':str(pathlib.Path(a.plan).resolve()),'archive':str(archive),'baseline':str(base),'archive_sha256':archive_sha,'layout':g.layout_digest,'interface':a.interface_digest,'db':str(state/'ledger.sqlite'),'boot':uuid.uuid4().hex,'capacity':cap,'mode':a.mode,'total_slots':a.total_slots,'registry':str(state/'checks.json'),'host_at_admission':limits,'ai_available_observed':a.available_ai_slots,'ai_observed_at':time.time(),'ai_observed_monotonic':time.monotonic(),'observed_host_boot':pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip()};s=ledger(c);s.close();save(c['registry'],{});save(state/'attempts.json',{});(state/'coordinator.key').write_bytes(os.urandom(32));os.chmod(state/'coordinator.key',0o600);save(cp,c);print(json.dumps(c));return 0
    c=load(cp);g=graph(c)
    if g.layout_digest!=c['layout']:raise ValueError('Task layout changed; create reviewed next baseline')
    if a.command=='restart':c['boot']=uuid.uuid4().hex;save(cp,c)
    s=ledger(c)
    try:
        if a.command=='observe-ai':
            if c['mode']!='ai' or not 0<=a.available_ai_slots<=30:raise ValueError('Invalid actual AI-slot observation')
            c['ai_available_observed']=a.available_ai_slots;c['ai_observed_at']=time.time();c['ai_observed_monotonic']=time.monotonic();c['observed_host_boot']=pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip();save(cp,c);out={'ai_available_observed':a.available_ai_slots,'observed_at':c['ai_observed_at'],'observation_source':'trusted coordinator tool telemetry'}
        elif a.command in ('status','graph','restart'):out=g.status()|{'leases':s.active(),'mode':c['mode'],'maximum_ai_workers':c['total_slots']-1,'logical_lanes':50}
        elif a.command=='claim':
            if a.task not in g.ready() or g.tasks[a.task].get('kind')!='parallel_lane':raise ValueError('Task not currently dependency-qualified parallel lane')
            if c['mode']=='ai' and (time.time()<c['ai_observed_at'] or time.time()-c['ai_observed_at']>60 or time.monotonic()<c['ai_observed_monotonic'] or time.monotonic()-c['ai_observed_monotonic']>60 or c['observed_host_boot']!=pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip() or c['ai_available_observed']<=0):raise ValueError('Fresh available AI-slot observation required before claim')
            resources=load(a.resources);limits=host_limits(state)
            for k in limits:
                if sum(x['resources'][k] for x in s.active())+resources.get(k,0)>limits[k]:raise ValueError('Current host headroom exhausted: '+k)
            lane=g.tasks[a.task]['lane'];b={'baseline':source_digest(c['baseline']),'candidate':source_digest(c['baseline']),'interface':c['interface'],'layout':c['layout']};t=s.claim(a.task,lane,a.owner,b,resources,a.ttl);d=state/'attempts'/namespace(b,a.task,lane,t['attempt']);d.mkdir(parents=True);w=d/'workspace'
            try:
                actual_archive=create_workspace(c['archive'],w)
                if actual_archive!=c['archive_sha256']:raise ValueError('Immutable archive changed')
            except BaseException:s.release(t);raise
            t['workspace']=str(w);t['attempt_dir']=str(d);attempts=load(state/'attempts.json');attempts[t['attempt']]={'workspace':str(w),'attempt_dir':str(d),'task':t['task']};save(state/'attempts.json',attempts);save(d/'token.json',t)
            if c['mode']=='ai':c['ai_available_observed']-=1;save(cp,c)
            out={'token':str(d/'token.json'),'lease':t}
        elif a.command=='register-checks':
            if a.task not in g.tasks:raise ValueError('Unknown task')
            checks=load(a.checks)
            if not isinstance(checks,list) or not checks or any(not isinstance(x,dict) or not x.get('id') or not isinstance(x.get('command'),list) or not x['command'] or not isinstance(x.get('timeout'),(int,float)) or x['timeout']<=0 or x.get('proof') not in ('unittest','go-test-json','node-tap') for x in checks):raise ValueError('Positive actual command registry required')
            if len({x['id'] for x in checks})!=len(checks):raise ValueError('Duplicate check ID')
            registry=load(c['registry']);registry[a.task]=checks;save(c['registry'],registry);out={'task':a.task,'registered':len(checks),'qualification':False}
        elif a.command=='stage':
            checks=load(a.checks)
            if not isinstance(checks,list) or not checks or any(not isinstance(x,dict) or x.get('proof') not in ('unittest','go-test-json','node-tap') for x in checks):raise ValueError('Reviewed combined checks require positive proof policies')
            if a.fixture_partial and not c.get('fixture'):raise ValueError('Partial staging is isolated fixture-only, never production qualification')
            expected={task for task,d in g.tasks.items() if d.get('wave')==a.wave and d.get('kind')=='parallel_lane'}
            if not expected:raise ValueError('No parallel tasks in selected wave')
            declared=[load(pathlib.Path(path)/'accepted.json')['task'] for path in a.accepted]
            if not a.fixture_partial and (set(declared)!=expected or len(declared)!=len(expected)):raise ValueError('Integration requires exact complete wave task set')
            key=(state/'coordinator.key').read_bytes();receipt_dir=state/'integration-checks'/uuid.uuid4().hex
            def combined_verify(candidate):
                b={'baseline':source_digest(c['baseline']),'candidate':source_digest(candidate),'interface':c['interface'],'layout':c['layout']}
                result=run_checks(checks,candidate,receipt_dir,b,{'coordinator_integration':True,'bindings':b},key)
                if any(r['exit_code']!=0 or r['timed_out'] or not r.get('proof') or r['proof']['positive_count']<=0 for r in result['receipts']):raise ValueError('Unqualified combined check')
                return {'qualified':True,'exit_code':0}
            ownership={task:definition['touches'] for task,definition in g.tasks.items()}
            registry=load(c['registry'])
            for accepted_path in a.accepted:
                task=load(pathlib.Path(accepted_path)/'accepted.json')['task'];review_dir=state/'accepted-reviews'/uuid.uuid4().hex
                reviewed=verify_accepted(c['baseline'],accepted_path,ownership[task],registry[task],key,review_dir,store=s)
                try:
                    if reviewed['bindings']['interface']!=c['interface'] or reviewed['bindings']['layout']!=c['layout']:raise ValueError('Accepted interface/layout differs from current frozen baseline')
                    if task not in expected:raise ValueError('Accepted task belongs to different wave')
                finally:shutil.rmtree(review_dir,ignore_errors=True)
            out=stage_accepted(c['baseline'],a.accepted,ownership,registry,key,a.output,combined_verify,store=s)
            out['integration_evidence']=str(receipt_dir/'evidence.json');out['expected_wave_tasks']=sorted(expected);out['fixture_partial']=a.fixture_partial;out['canonical_integrated']=False;out['complete_wave']=not a.fixture_partial;out['qualification_scope']='complete-wave-staged' if not a.fixture_partial else 'fixture-partial-staged'
            if a.fixture_partial:out['qualified']=False
        else:
            t=load(a.token);s.validate(t)
            if load(state/'attempts.json').get(t['attempt'])!={k:t[k] for k in ('workspace','attempt_dir','task')}:raise ValueError('Attempt paths differ from coordinator allocation')
            extra={k:t[k] for k in ('workspace','attempt_dir')};attempt=pathlib.Path(t['attempt_dir'])
            if a.command=='heartbeat':t=s.heartbeat(t,a.ttl)|extra;save(a.token,t);out=t
            elif a.command=='release':s.release(t);out={'released':True,'canonical_status_changed':False}
            elif a.command=='candidate':
                build_patch(c['baseline'],t['workspace'],g.tasks[t['task']]['touches'],attempt/('review-patch-'+source_digest(t['workspace'])));t=s.bind_candidate(t,source_digest(t['workspace']))|extra;save(a.token,t);out=t
            elif a.command=='patch':out=build_patch(c['baseline'],t['workspace'],g.tasks[t['task']]['touches'],attempt/'patch')
            elif a.command=='qualify':
                checks=check_registry(c,t['task']);key=(state/'coordinator.key').read_bytes();e=run_checks(checks,t['workspace'],attempt/'checks',t['bindings'],t,key);out=publish(e,key,s,t['workspace'],checks);save(attempt/'verified.json',out)
            elif a.command=='accept':
                checks=check_registry(c,t['task']);out=accept_artifact(s,t,t['workspace'],c['baseline'],attempt/'patch'/'manifest.json',g.tasks[t['task']]['touches'],checks,(state/'coordinator.key').read_bytes(),state/'accepted');out['accepted_path']=out['path'];out['canonical_status_changed']=False
            elif a.command=='complete':
                e=load(attempt/'checks'/'evidence.json');out=publish(e,(state/'coordinator.key').read_bytes(),s,t['workspace'],check_registry(c,t['task']));s.complete(t);out|={'local_state':'implemented','canonical_status_changed':False,'canonical_done':False}
        print(json.dumps(out,sort_keys=True));return 0
    finally:s.close()
def locked_main():
    parser=argparse.ArgumentParser(add_help=False);parser.add_argument('--state-dir',required=True);args,_=parser.parse_known_args();root=pathlib.Path(args.state_dir).resolve();root.mkdir(parents=True,exist_ok=True)
    with open(root/'coordinator.lock','a') as lock:
        deadline=time.monotonic()+30
        while True:
            try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB);break
            except BlockingIOError:
                if time.monotonic()>deadline:raise ValueError('Coordinator lock timeout')
                time.sleep(.01)
        return main()
if __name__=='__main__':
    try:raise SystemExit(locked_main())
    except (ValueError,OSError,KeyError) as e:print(json.dumps({'status':'failed','error':str(e)}),file=sys.stderr);raise SystemExit(1)
