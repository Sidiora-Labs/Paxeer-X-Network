"""Actual coordinator CLI lifecycle against an explicitly isolated fixture plan."""
from __future__ import annotations
import hashlib,json,os,pathlib,subprocess,sys,time
from dispatcher.qualification_fixture import normalize_fixture

def qualify_cli(root,spec_path,plan_path):
    root=pathlib.Path(root);root.mkdir(parents=True,exist_ok=True);cli=pathlib.Path(__file__).with_name('cli.py');source=root/'source';source.mkdir();owned=source/'shared/chain/readpolicy';owned.mkdir(parents=True);(owned/'value.txt').write_text('baseline')
    (owned/'test_value.py').write_text("import unittest\nfrom pathlib import Path\nclass CandidateTest(unittest.TestCase):\n def test_value(self): self.assertEqual(Path('shared/chain/readpolicy/value.txt').read_text(),'implemented')\n")
    text,plan=normalize_fixture(pathlib.Path(spec_path).read_text(),json.loads(pathlib.Path(plan_path).read_text()));spec=root/'fixture-spec.kvx';spec.write_text(text);planpath=root/'fixture-plan.json';planpath.write_text(json.dumps(plan));state=root/'state';capacity=root/'capacity.json';resources=root/'resources.json';capacity.write_text(json.dumps(dict(cpu=1,memory=64*1024**2,pids=20,disk=64*1024**2,executor=1,go_build=1,container_start=1)));resources.write_text(json.dumps(dict(cpu=.5,memory=1024**2,pids=2,disk=1024**2,executor=1,go_build=0,container_start=0)));history=[]
    def call(*args,fail=False):
        command=[sys.executable,str(cli),'--state-dir',str(state),*map(str,args)];r=subprocess.run(command,capture_output=True,text=True,timeout=30,env=dict(os.environ,PYTHONDONTWRITEBYTECODE='1'));history.append({'argv':command,'exit_code':r.returncode,'stdout':r.stdout,'stderr':r.stderr})
        if fail:
            if r.returncode==0:raise AssertionError('Forbidden CLI action succeeded: '+str(args))
            return None
        if r.returncode:raise AssertionError('CLI failed: '+str(args)+' '+r.stderr)
        return json.loads(r.stdout)
    call('init','--spec',spec,'--plan',planpath,'--source',source,'--interface-digest','a'*64,'--capacity',capacity,'--available-ai-slots','1','--fixture')
    claim=call('claim','--task','1.01','--owner','fixture-first','--resources',resources);token=pathlib.Path(claim['token']);workspace=pathlib.Path(claim['lease']['workspace']);old=root/'old-token.json';old.write_bytes(token.read_bytes());(workspace/'shared/chain/readpolicy/value.txt').write_text('implemented')
    (workspace/'unowned.txt').write_text('outside');call('candidate','--token',token,fail=True);(workspace/'unowned.txt').unlink();call('candidate','--token',token);call('heartbeat','--token',old,fail=True);call('patch','--token',token)
    checks=[{'id':'owned-fixture-tests','command':[sys.executable,'-m','unittest','discover','-s','shared/chain/readpolicy','-p','test_*.py','-v'],'timeout':10,'proof':'unittest','inputs':['shared/chain/readpolicy/value.txt','shared/chain/readpolicy/test_value.py']}];checksfile=root/'checks.json';checksfile.write_text(json.dumps(checks));call('register-checks','--task','1.01','--checks',checksfile);qualified=call('qualify','--token',token);assert qualified['verified'] and qualified['proofs']['owned-fixture-tests']['positive_count']>0
    accepted=call('accept','--token',token);accepted_path=accepted['accepted_path'];call('heartbeat','--token',token,fail=True)
    # A second lane can be admitted after accepting/releasing the first, despite capacity1.
    call('observe-ai','--available-ai-slots','1');second=call('claim','--task','1.02','--owner','fixture-second','--resources',resources);call('release','--token',second['token'])
    combined=root/'combined.json';combined.write_text(json.dumps(checks));call('stage','--accepted',accepted_path,'--output',root/'incomplete-wave','--checks',combined,'--wave','1',fail=True)
    staged=call('stage','--accepted',accepted_path,'--output',root/'stage1','--checks',combined,'--wave','1','--fixture-partial');again=call('stage','--accepted',accepted_path,'--output',root/'stage2','--checks',combined,'--wave','1','--fixture-partial');assert staged['result_digest']==again['result_digest']
    wrong=[dict(checks[0],command=[sys.executable,'-m','unittest','discover','-s','.','-p','nonexistent_tests.py','-v'])];bad=root/'bad-checks.json';bad.write_text(json.dumps(wrong));call('stage','--accepted',accepted_path,'--output',root/'failed-stage','--checks',bad,'--wave','1','--fixture-partial',fail=True);assert not (root/'failed-stage').exists();assert (source/'shared/chain/readpolicy/value.txt').read_text()=='baseline'
    # Every lane remains pending in the fixture spec: acceptance is not canonical graph qualification.
    import tomllib;assert tomllib.loads(spec.read_text())['task']['1']['01']['status']=='pending'
    (root/'commands.json').write_text(json.dumps(history,indent=2)+'\n')
    return {'checks':15,'qualified_proofs':qualified['proofs'],'accepted_path':accepted_path,'result_digest':staged['result_digest'],'commands':len(history),'capacity_workers':1,'successive_distinct_lane_claims':2,'canonical_status_changed':False,'failed_combined_check_rejected':True}
