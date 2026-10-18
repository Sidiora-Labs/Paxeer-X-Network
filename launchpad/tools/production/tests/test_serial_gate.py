import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest

SOURCE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(SOURCE))
from dispatcher import acceptance,evidence,patches
from dispatcher.graph import Graph
from dispatcher.store import Store
loader=importlib.util.spec_from_file_location('serial_gate',Path(__file__).parents[1]/'serial_gate.py')
gate=importlib.util.module_from_spec(loader);loader.loader.exec_module(gate)
CAP=dict(cpu=8,memory=8192,pids=100,disk=10000,executor=30,go_build=2,container_start=1)
RES=dict(cpu=1,memory=100,pids=1,disk=10,executor=1,go_build=0,container_start=0)

def writejson(path,value): path.parent.mkdir(parents=True,exist_ok=True);path.write_text(json.dumps(value,sort_keys=True))

class SerialQualification(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
    cls.tmp=None if os.environ.get('SERIAL_KEEP_EVIDENCE')=='1' else tempfile.TemporaryDirectory(prefix='serial-gate-proof-')
    cls.basepath=Path(tempfile.mkdtemp(prefix='serial-gate-proof-') if cls.tmp is None else cls.tmp.name)
    print('Serial fixture artifacts: '+str(cls.basepath),file=sys.stderr,flush=True)
    cls.base=cls.basepath/'baseline';cls.base.mkdir()
    shutil.copytree(SOURCE/'dispatcher',cls.base/'tools/production/dispatcher',ignore=shutil.ignore_patterns('__pycache__'))
    cls.state=cls.basepath/'state';cls.state.mkdir(mode=0o700)
    cls.secret=b'private-test-only-coordinator-key-123456'
    (cls.state/'coordinator.key').write_bytes(cls.secret);(cls.state/'coordinator.key').chmod(0o600)
    cls.store=Store(cls.state/'leases.sqlite',CAP,'test-boot')
    lines=['[meta]','feature="production-system"','[task.0.3]','title="dispatcher"','status="done"','wave=0']
    for i in range(1,51):
      lane=f'L{i:02}';tid=f'1.{i:02}'
      lines += [f'[lane.{lane}]',f'write_paths=["service/{lane}/**"]',f'[task.{tid}]','title="actual lane"','status="done"','wave=1','phase=1','kind="parallel_lane"',f'lane="{lane}"','requires=["0.3"]',f'touches=["service/{lane}/**"]']
      p=cls.base/'service'/lane/'value';p.parent.mkdir(parents=True);p.write_text('old')
    lines+=['[task.1.99]','title="serial"','status="in_progress"','wave=2','kind="serial_gate"','requires='+json.dumps([f'1.{i:02}' for i in range(1,51)])]
    specfile=cls.base/'spec/production-system/spec.kvx';specfile.parent.mkdir(parents=True);specfile.write_text('\n'.join(lines))
    cls.graph=Graph(specfile)
    import tomllib
    cls.spec=tomllib.loads(specfile.read_text());cls.task=cls.graph.tasks['1.99']
    plan={'tasks':cls.graph.tasks,'spec_sha256':hashlib.sha256(specfile.read_bytes()).hexdigest(),'lanes':[dict(id=k,write_paths=v['write_paths'],tasks=[dict(id=t,**d) for t,d in cls.graph.tasks.items() if d.get('lane')==k]) for k,v in cls.spec['lane'].items()]}
    writejson(cls.base/'spec/production-system/execution-plan.json',plan)
    writejson(cls.base/'tools/production/bootstrap/check-registry.json',{})
    (cls.base/'native.py').write_text("import sys,unittest,os\nfrom pathlib import Path\nmode=sys.argv.pop()\nclass Value(unittest.TestCase):\n def test_actual_files(self):\n  self.assertNotEqual(os.environ.get('SERIAL_TEST_FAIL'),'1')\n  lanes=range(1,51) if mode=='all' else [int(mode)]\n  for i in lanes: self.assertEqual(Path(f'service/L{i:02}/value').read_text(),'new')\nunittest.main()\n")
    cls.baseline_digest=evidence.source_digest(cls.base);cls.registry={};cls.selected={};cls.paths=[]
    for i in range(1,51):
      lane=f'L{i:02}';tid=f'1.{i:02}';candidate=cls.basepath/f'candidate-{lane}'
      shutil.copytree(cls.base,candidate);(candidate/'service'/lane/'value').write_text('new')
      checks=[{'id':'actual-'+lane,'command':[sys.executable,'native.py',str(i)],'timeout':10,'proof':'unittest','inputs':[f'service/{lane}/value']}]
      cls.registry[tid]=checks
      binding={'baseline':cls.baseline_digest,'candidate':evidence.source_digest(candidate),'interface':'a'*64,'layout':cls.graph.layout_digest}
      token=cls.store.claim(tid,lane,'actual-test-worker',binding,RES)
      token['attempt_dir']=str(cls.basepath/'attempts'/lane)
      evidence.run_checks(checks,candidate,Path(token['attempt_dir'])/'checks',binding,token,cls.secret)
      patch=patches.build_patch(cls.base,candidate,[f'service/{lane}/**'],cls.basepath/'patches'/lane)
      accepted=acceptance.accept_artifact(cls.store,token,candidate,cls.base,patch,[f'service/{lane}/**'],checks,cls.secret,cls.state/'accepted')
      cls.selected[tid]=accepted['id'];cls.paths.append(accepted['path'])
    writejson(cls.state/'checks.json',cls.registry)
    config={'baseline':str(cls.base),'db':str(cls.state/'leases.sqlite'),'registry':str(cls.state/'checks.json'),'interface':'a'*64,'layout':cls.graph.layout_digest,'fixture':False}
    writejson(cls.state/'config.json',config)
    cls.combined=[{'id':'actual-all-fifty','command':[sys.executable,'native.py','all'],'timeout':10,'proof':'unittest','inputs':['native.py']}]
    cls.stageproof=cls.basepath/'prior-combined'
    def verify(stage):
      binding={'baseline':cls.baseline_digest,'candidate':evidence.source_digest(stage),'interface':'a'*64,'layout':cls.graph.layout_digest}
      evidence.run_checks(cls.combined,stage,cls.stageproof,binding,{'coordinator_integration':True,'bindings':binding},cls.secret)
      return True
    cls.stage=cls.basepath/'complete-stage'
    acceptance.stage_accepted(cls.base,cls.paths,{k:v['touches'] for k,v in cls.graph.tasks.items() if k in cls.selected},cls.registry,cls.secret,cls.stage,verify,store=cls.store)
    cls.canonical=cls.basepath/'canonical';shutil.copytree(cls.stage,cls.canonical)
    target=cls.canonical/'service/internal/app/app.go';target.parent.mkdir(parents=True);target.write_text('package app\n// all fifty registered\n')
    blob=hashlib.sha256(target.read_bytes()).hexdigest();artifact=cls.basepath/'serial-patch';(artifact/'blobs').mkdir(parents=True);(artifact/'blobs'/blob).write_bytes(target.read_bytes())
    serial_manifest={'version':1,'baseline_digest':gate.digest(patches._tree(cls.stage)),'entries':[{'path':'service/internal/app/app.go','before':None,'after':{'sha256':blob,'mode':0o644}}]}
    writejson(artifact/'manifest.json',serial_manifest)
    cls.policy={'control_plane':{'spec_sha256':hashlib.sha256((cls.canonical/'spec/production-system/spec.kvx').read_bytes()).hexdigest(),'plan_sha256':hashlib.sha256((cls.canonical/'spec/production-system/execution-plan.json').read_bytes()).hexdigest(),'registry_without_self_sha256':gate.digest({})},'task':'1.99','wave':1,'accepted_ids':cls.selected,'state_directory':str(cls.state),'config_sha256':hashlib.sha256((cls.state/'config.json').read_bytes()).hexdigest(),'lane_registry_sha256':hashlib.sha256((cls.state/'checks.json').read_bytes()).hexdigest(),'input_interface_sha256':'a'*64,'output_interface_sha256':'a'*64,'baseline_sha256':cls.baseline_digest,'combined_checks':cls.combined,'stage_evidence_path':str(cls.stageproof/'evidence.json'),'stage_evidence_sha256':hashlib.sha256((cls.stageproof/'evidence.json').read_bytes()).hexdigest(),'stage_outputs_directory':str(cls.stageproof),'serial_integration':{'artifact_directory':str(artifact),'manifest_sha256':hashlib.sha256((artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':['service/internal/app/app.go']}}
 @classmethod
 def tearDownClass(cls):
    cls.store.close()
    if cls.tmp is not None:cls.tmp.cleanup()
 def run_policy(self,policy=None,root=None):
    policy=policy or copy.deepcopy(self.policy)
    output=Path(tempfile.mkdtemp(prefix='gate-result-',dir=self.basepath));self.addCleanup(shutil.rmtree,output)
    return gate.qualify_serial_gate({'serial_gate_policy':policy,'policy_sha256':gate.digest(policy),'interface_bundle':{'expected_sha256':policy['output_interface_sha256']}},'1.99',self.task,self.spec,root or self.canonical,output)
 def test_real_fifty_acceptances_combined_and_canonical(self):
    result=self.run_policy();self.assertTrue(result['qualified']);self.assertEqual(len(result['accepted_tasks']),50);self.assertEqual(result['checks'][0]['native_proof']['positive_count'],1)
 def test_missing_acceptance_rejected(self):
    policy=copy.deepcopy(self.policy);policy['accepted_ids'].pop('1.50')
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_duplicate_acceptance_rejected(self):
    policy=copy.deepcopy(self.policy);policy['accepted_ids']['1.50']=policy['accepted_ids']['1.01']
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_unexplained_canonical_edit_rejected(self):
    root=self.basepath/'unexpected';shutil.copytree(self.canonical,root);self.addCleanup(shutil.rmtree,root)
    (root/'unexplained').write_text('unreviewed')
    with self.assertRaises(ValueError):self.run_policy(root=root)
 def test_missing_native_policy_rejected(self):
    policy=copy.deepcopy(self.policy);policy['combined_checks']=[]
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_noop_native_not_accepted_as_prior_proof(self):
    policy=copy.deepcopy(self.policy);policy['combined_checks']=[{'id':'fake','command':[sys.executable,'-c','pass'],'timeout':10,'proof':'unittest'}]
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_stale_stage_hash_rejected(self):
    policy=copy.deepcopy(self.policy);policy['stage_evidence_sha256']='0'*64
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_unreviewed_serial_path_rejected(self):
    policy=copy.deepcopy(self.policy);policy['serial_integration']['allowed_paths']=['other/path']
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_actual_fresh_native_failure_rejected(self):
    old=os.environ.get('SERIAL_TEST_FAIL');os.environ['SERIAL_TEST_FAIL']='1'
    try:
      with self.assertRaises(ValueError):self.run_policy()
    finally:
      if old is None:os.environ.pop('SERIAL_TEST_FAIL',None)
      else:os.environ['SERIAL_TEST_FAIL']=old
 def test_forged_signed_stage_proof_rejected(self):
    policy=copy.deepcopy(self.policy);proof=json.loads((self.stageproof/'evidence.json').read_text());proof['signature']='0'*64
    target=self.basepath/'forged-stage.json';writejson(target,proof);self.addCleanup(target.unlink)
    policy['stage_evidence_path']=str(target);policy['stage_evidence_sha256']=hashlib.sha256(target.read_bytes()).hexdigest()
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_traversal_acceptance_rejected(self):
    policy=copy.deepcopy(self.policy);policy['accepted_ids']['1.50']='../outside'
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_noop_serial_manifest_rejected(self):
    folder=self.basepath/'noop-serial';folder.mkdir();self.addCleanup(shutil.rmtree,folder)
    writejson(folder/'manifest.json',{'version':1,'baseline_digest':gate.digest(patches._tree(self.stage)),'entries':[]})
    with self.assertRaises(ValueError):gate.reconcile_serial_patch(self.stage,folder,{'manifest_sha256':hashlib.sha256((folder/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':['service/internal/app/app.go']},patches)
 def test_changed_control_metadata_rejected(self):
    policy=copy.deepcopy(self.policy);policy['control_plane']['registry_without_self_sha256']='0'*64
    with self.assertRaises(ValueError):self.run_policy(policy)
 def test_unregistered_future_gate_fails(self):
    with self.assertRaises(ValueError):gate.qualify_serial_gate({},'2.99',self.task,self.spec,self.canonical,self.basepath/'unused')


 def test_exact_v2_full_gate_retains_signed_fifty_and_candidate_equality(self):
    root=self.basepath/'exact-canonical';shutil.copytree(self.stage,root);self.addCleanup(shutil.rmtree,root)
    artifact=self.basepath/'exact-artifact';artifact.mkdir();self.addCleanup(shutil.rmtree,artifact)
    ownership=artifact/'ownership.json';ownership.write_bytes(EXACT_OWNERSHIP_BYTES)
    target=root/'shared/accounting/wire.go';target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(b'package accounting\n')
    identity={'sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'mode':target.stat().st_mode&0o777}
    (artifact/'blobs').mkdir();(artifact/'blobs'/identity['sha256']).write_bytes(target.read_bytes())
    source_policy={'version':2,'kind':gate.EXACT_SERIAL_POLICY_KIND,'task':'1.99','ownership_artifact':str(ownership),'ownership_sha256':gate.EXACT_SERIAL_OWNERSHIP_SHA256}
    writejson(artifact/'manifest.json',{'version':2,'baseline_digest':gate.digest(patches._tree(self.stage)),'source_policy_sha256':gate.digest(source_policy),'entries':[{'path':'shared/accounting/wire.go','before':None,'after':identity}]})
    policy=copy.deepcopy(self.policy)
    policy['serial_integration']={'artifact_directory':str(artifact),'manifest_sha256':hashlib.sha256((artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':['shared/accounting/wire.go'],'source_policy':source_policy}
    controls={}
    for path in sorted(gate.EXACT_SERIAL_CONTROL_PATHS):
      destination=root/path;destination.parent.mkdir(parents=True,exist_ok=True);destination.write_bytes((SOURCE.parent.parent/path).read_bytes())
      controls[path]={'before':patches._tree(self.stage).get(path),'after':patches._tree(root)[path]}
    policy['control_plane']['serial_source_controls']=controls
    result=self.run_policy(policy,root=root)
    self.assertTrue(result['qualified']);self.assertEqual(len(result['accepted_tasks']),50)
    self.assertEqual(set(result['control_paths']),gate.CONTROL_PATHS|gate.EXACT_SERIAL_CONTROL_PATHS)
    policy['control_plane']['serial_source_controls']['tools/production/serial_gate.py']['after']['sha256']='0'*64
    with self.assertRaises(ValueError):self.run_policy(policy,root=root)

import base64
import zlib
EXACT_OWNERSHIP_BYTES=zlib.decompress(base64.b85decode('c-qaKT~p&W7l7~kD>8e%jtQ_IyuHeTDR~Pqb&~CLIvtN<D~T4z@=9_-+UbAqkv~FMVnaZdwKEIBk<W7^9UUFXlKjt4oz6QREbtQfE6g~d@w@+Zdhg}sI}(TR$2+oIrUFeP?BjbB2%lp{P~;~_EPT3-;jRTq-etE4#P8%Sz3$mrekDs4&@>is{p951`y5CUEYsb>w;dEkG(ZB-9U4cQpF2Uy7>-4U)073cGsQE?aA$$|0`tx?<pM5skPry;4m)TjFq1+)9Cea(8j)O9_kqs=^(Xw%>HL%ZEG|n0x*-kjAV2dM5ZN8s*IBmY_oL$vRuEwnTho+8p~Y}OK=mx$zwaw#&WTv0Q+rL>-7KPOZU>~hSw@P)2N8k7u~Cw6dyS_yXMr7n_ICaSw<EH`TriB57D`B0@O|Lk2WuzY>iCV!B^D?|0#(sYtTM&xS!*<35-XtbjLfSz$JUfViDPE1!6LADHA>!W@$k+@saWWv<!SWsIOej=HwT<yBvOVSECdW8B*=yzf->OVH(@VT)ET3H<Jf{r2{?_0j5R@XY%OV+MxelgF0l=c6yI0o^`pfxV!^_~j8Vo92ZCRWqOw+@eZ)8rQUMfIGT&aYYZA^ew0yx(NaDGjhBHeR*$m^bqF|5CtlU9cD9E}5xbH>a1O<0csN7B%6l);eyUZt`1Hqw=$^_kS9Qv<)ZuX?85DU7RJFW(r6@Auq^6nhRmGItYNNqt1%#gsLMN^TaLY)p`li*x~3VT+-2Vc>6ITs9_#kzykREm%_MiOW@rKE1M*^B#R74cZfe5q@AOoN=2Ej{n+mAd*TEFBE6{+K6}sqH3{I9X;^+ufjwSnVL=mK`mGU-q1K;Vz}}c)C<4kHpXu#we2BUH<RR>0aTd){H<hi%Gw|#NdXO3FSn(q(eziI}mdhzbbgWo>j)M5P(re#O6&HUqGzeI15HDoHn1OFlBN)ri-Je?DE*m&!zb@a-T(TIlHnCXoz0+Kh1OH-*c9YSoIWho;6JvO<78(d9Nx*GEKv>-e*SHvN=*d#4Ubl@W;^1Ah_R#<o@co0{V&oem^W>WC^OSWW+@k42t$spccZ00hQLRvVOS8E<IF`%oud_IBKw-)-crL&DnS+UcGw=2@fa)&Ksq1S2li!w%?FUW$|1oQ%_9ygr>o7Jz>JYIKUi&uHE)gV^&8WtN_I{CIRS>kE-FDH-WeE^dYyO8b?Yu!WYlRoXUU<2N-&C`+JHaG9x&w0-jpiBvx^g5?-zGD1zn=y(znisQ?Z0ryyLZ&r0IRl5<aYihbb^a+UeRUAcOE;$ET#zqptCFllH<y^oR)C0jY-KKg7)DP=0f(As9z2sy&xTw!J*?7xG)#X`}XDmDHe1uL`uM}qa3|9+$f`{x1)ZpkOEn5p%7Pb4d=pU0B*R8R#|dkyW9vlh4$zEw5ZPgXADfJXarXsv%zM_ve#$Z=3U_Ea}Et-ygx{Dw1^m?f~cL#gI-T8Y+F(m(P;x<n*yi`!DBp?!)utDI%gKyE4H-&4$5FmfKFgfFOu4CfBQo{<PH@WvfUfvcNzdIhXlJ75J1#Dv;z`>AMU{q_%{^>o}-Pz^nI4f>#7^rIZkh7mr<{GWyMHA?*FhBivkIG&Rj+xcS1>~R1JY92kPg_i-ug2+jPjoJy|7ef7V2}*5st5qSNGTABZ>S>ch%R9tLL^t+Dwh|N^D(>=CplGpXAv<4XTC_WmFY_^vz+S7Cg}%DQ7Qmgc@RU*^fr(w>wL+moVWMJ@i%KG$a?Dm}N+O6cOMh6oPN`=_WQKzc)TbqIgqK*b&D)T3hGr;6R*3jwN)ZdS(kaM+;)@~(M<}aa!_oj6z5po;oN}l~Xm$<f8=77Iga;=ShEV+b{)`@5WkPF)<KS8E^Asd#LL)lg*yUOTn{Q&srvRV!oP8-Y?5!F*YXC`{q@pYEGKu68GOcQqAC$Lgh|y%+`Q{+4kfS8v%a;w$p^(=|*)zo_Le*>9cu-n}gHe9IWWHjMaAc7X150dP`ds%pQtd-zwYXB2AA)NMv1VvC_~M$O4C|}SEi?=EG&5VUB0=wP%v*uBlHKjPUA^2j(Pd2E)<PRdPYpIZy^up6_MxfmU?0m>*T?S2)zr=IBWkXhoq@Rgo0kh_%dJ!uhIr5}!$YC9O+YbukxeO7&$KGWkx&<DJ0jD~(^-2kj#&}8nZ}<X5~U2c#nqYV6~Sw0B-!Mfwifyrsz1BBn#~$d7nI)lb<y!FkmbfxwLRgkRCWE}R;s36@F`D6ANZ8Iy@EK$qSD|F*hTz8lK(IrQ`@jqlAr7MnqcY69F7uQdCO6L`Hi+bMtF{bO&g~3fYeLVmce{2@J8;Jso2EZ1T7oRYT)qz#UQv1%c<Oeq?po1-s)x}RC5~3AEyBUn9?BSL8iJ8rI4v9K>3lTBRomDy0!R^C9121OyWZw<jVod-?fH$S}q>go_g_m9*)C=!d2NQ=u8JuE}PKB(N>K0Pa#y25=K@Ky68aFK5$S8s`LIzIE^~E$E;@E8^=M)vUL?|oZGHE6|QY7kEclzZGr_Nv7Idps(LVf`0Am(jDYxpQfPwBViFpWV51PSKn#aFYX$8U#IvDXzEH7_tGBaq%#sb~P(V#%QzN`~{o5-E^6^gD{?$F+*_B?qG3t%7X@c{^ER>5cybX?5Rj%e0tBVTR;j}z>uR`>Z6;8p{<%wr66}Yo>=HY<LrsdZe<QpMNQXErK+4rAW+ce-2=7b+m{JZPBXoMSJU^y4dHg=2z51IUI=fA4j?+E~)<C5vEJmnifwcg^bG|EEEp4q*8Y!#`j^0vbm31WiOt#nogs94qaW&*<z+BBtpT@?9R;i{satu!UEPcBW4JM0CRZRI17H~8c%%$%VBb9;5Xb2?vzV|c~se0xDfP(oQlwBqE3XCJ%1q2#`&IK}zTD&dDNjx8i_^^ns{j09Yz@(B)hm$>o`7kIgQK9=(6`gvj&3h%^m-Qe5aBL|N^THh#%!4#lOw}&sh9bX>xDJ>K6O&rJromC!)d8iVgK)&po)U5N?qliEeom)$kn4m|yI?xaU0m>qVR#$gio)%@6a4X682DGXPzDLU>YS~VI3|ArEY*QT=pT%wako;&s2!m!MCi#e>eRa4!xb*wy=k8=U_W$pGF-E0p#~T}?%xL78rHt=6m&PbG8DE`W^{=m;;V+IsO1U@v@xUGW=lz@8{@|CPDXM*uZS4=Uv>8pl+zdt|C@xd<ItPPtCu4Ke8D3l)W8Ki)2E$u-G#HyyWA4q3Gc>2h+~HSH2MTCpluF*HZ&a;KypijfqK)Hu<|uPH8D0#AzxadU*dV(uwetFdYg2T(a{AZfD}OZX-;S<aV~Xn9>0dbB7q{<Sn4;2+GaB{H36pMID4@(A_0LW6O2fV<)%t2oK@HvU;Bs(ol<RgcFS9`4#u@i7`s2PadfiH!ay^sAldcDbZi+Iuu6u2gE~9aOJTgZiZ+z)q4_wpY+pS`w$<0mQ`@=ZB{K+l20b^X|t>;`io-;gm%&{$RaPAnTj6ZTGo=G~nmjiRgxwn(im4D%UHK^8po4DpQF|R-T6~+QEIx)u`6*?I=7|6n0W0Y~E*EUC+(XBfiIleRe>RdzkW{_f&8^^FY3eWl7paJE`F>2a&^xLFwPT7n-r+;IXHu>Plq#DcAl5>+|&zxaNp_TL1pqaPP<jd&X8+<XwJ&xU9ouN6(+z!r->-EQ8|H7F1`s&Jv-*a7KeA4govF+Hu9t?jqOeGkJxR>VWG(Nw&9*o9DMcSa1<K6T<bE<AIzPj-GP=j*tf7>^qqmx^E@Dj%YG40E;wp%`m_cCi<M)|OH!R{T&e^txNU$ed7F#Jio_|tarAKJ(F+QWZrH}{|0#h<l`KW?|(pS0WVPfpv%_u9oDw~Ie%58tlSJ!#kLoqTE+->%a=Y1iqVw(E3H+x2><?V4V@?e25y^j@p<<5uY>t<q0hrGIFZ{;^g1r&j5oTcg8=Sk&wyYMP&)v1k>q$FfjF5XUX*!?1jb7Ur$0kjvDpLHXLr?GZ|qTa=rbYATO8dUh*jvwXWaA)PCKNzV&($TC!Ua?mUCogH_y|LtP(tNgqo5XZB<gyLQ(+93XfyK_{)&N~l-1rF}2PUQS#6^fO!s?WX|vyQsme!bC3&G#4Uft9;<b!Kph;3EOI{WsPFlt%%}&A@Shb>?$>xCJ=Hs&-O`gr9%OTi!8ffIhwtqLh;r&ff<~BVX#kLCT+5@+w~;FL&=&_XWKw8NSCsnq5dD6z@9HI){*zrnRs4d3qPmD&3baJ$Weq%4A!8WZG>><j17>97p+kfU`2h36jrGI-Ja7c}@!A3u_AQBJ6d~&MX>7n@&;2IP7Ha5w+xdCp+>`jyQn!ojJvyJ9%8u32_Vx@az<DmUXY41o}A4Rycg$iD_pI9xXsD>`*qNEWq6?FYjW!&a1>nVTeN?z5_)ACXW)|b@)(WfBp3DPyYclR0(<'))


class ExactSerialSourcePolicy(unittest.TestCase):
 def setUp(self):
    self.tmp=tempfile.TemporaryDirectory(prefix='exact-serial-source-');self.addCleanup(self.tmp.cleanup)
    self.root=Path(self.tmp.name);self.stage=self.root/'stage';self.stage.mkdir()
    self.artifact=self.root/'artifact';(self.artifact/'blobs').mkdir(parents=True)
    self.ownership=self.artifact/'ownership.json';self.ownership.write_bytes(EXACT_OWNERSHIP_BYTES)
    self.source_policy={'version':2,'kind':gate.EXACT_SERIAL_POLICY_KIND,'task':'1.99','ownership_artifact':str(self.ownership),'ownership_sha256':gate.EXACT_SERIAL_OWNERSHIP_SHA256}
    self.path='shared/accounting/wire.go';self.content=b'package accounting\n'
    self.identity={'sha256':hashlib.sha256(self.content).hexdigest(),'mode':0o644}
    (self.artifact/'blobs'/self.identity['sha256']).write_bytes(self.content)
    self.manifest={'version':2,'baseline_digest':gate.digest(patches._tree(self.stage)),'source_policy_sha256':gate.digest(self.source_policy),'entries':[{'path':self.path,'before':None,'after':self.identity}]}
    self.review={'allowed_paths':[self.path],'source_policy':self.source_policy}
 def reconcile(self):
    self.manifest['source_policy_sha256']=gate.digest(self.source_policy)
    writejson(self.artifact/'manifest.json',self.manifest)
    self.review['manifest_sha256']=hashlib.sha256((self.artifact/'manifest.json').read_bytes()).hexdigest()
    return gate.reconcile_serial_patch(self.stage,self.artifact,self.review,patches)
 def reject(self):
    with self.assertRaises(ValueError):self.reconcile()
 def test_exact_adapter_path_applies_real_hash_and_mode(self):
    self.assertFalse(patches._reserved(self.path));self.reconcile()
    self.assertEqual((self.stage/self.path).read_bytes(),self.content)
    self.assertEqual((self.stage/self.path).stat().st_mode&0o777,0o644)
 def test_legacy_default_still_rejects_adapter_path(self):
    self.manifest.pop('source_policy_sha256');self.manifest['version']=1
    writejson(self.artifact/'manifest.json',self.manifest)
    review={'allowed_paths':[self.path],'manifest_sha256':hashlib.sha256((self.artifact/'manifest.json').read_bytes()).hexdigest()}
    with self.assertRaises(ValueError):gate.reconcile_serial_patch(self.stage,self.artifact,review,patches)
 def test_widened_path_rejected(self):
    self.review['allowed_paths']=['shared/accounting/unreviewed.go'];self.manifest['entries'][0]['path']=self.review['allowed_paths'][0];self.reject()
 def test_wrong_ownership_hash_rejected(self):
    self.source_policy['ownership_sha256']='0'*64;self.reject()
 def test_modified_ownership_bytes_rejected(self):
    self.ownership.write_bytes(EXACT_OWNERSHIP_BYTES+b' ');self.reject()
 def test_unknown_policy_version_rejected(self):
    self.source_policy['version']=3;self.reject()
 def test_boolean_policy_version_rejected(self):
    self.source_policy['version']=True;self.reject()
 def test_wrong_task_rejected(self):
    self.source_policy['task']='2.99';self.reject()
 def test_extra_policy_field_rejected(self):
    self.source_policy['allow_any_path']=True;self.reject()
 def test_wrong_manifest_version_rejected(self):
    self.manifest['version']=1;self.reject()
 def test_wrong_manifest_policy_binding_rejected(self):
    writejson(self.artifact/'manifest.json',{**self.manifest,'source_policy_sha256':'0'*64})
    self.review['manifest_sha256']=hashlib.sha256((self.artifact/'manifest.json').read_bytes()).hexdigest()
    with self.assertRaises(ValueError):gate.reconcile_serial_patch(self.stage,self.artifact,self.review,patches)
 def test_controls_rejected(self):
    for path in sorted(gate.CONTROL_PATHS|gate.EXACT_SERIAL_CONTROL_PATHS):
      self.review['allowed_paths']=[path];self.manifest['entries'][0]['path']=path;self.reject()
 def test_ignored_traversal_and_wildcards_rejected(self):
    for path in ['evidence/a','../shared/accounting/wire.go','shared/accounting/**','/shared/accounting/wire.go']:
      self.review['allowed_paths']=[path];self.manifest['entries'][0]['path']=path;self.reject()
 def test_duplicate_allowlist_rejected(self):
    self.review['allowed_paths'].append(self.path);self.reject()
 def test_duplicate_entries_rejected(self):
    self.manifest['entries'].append(copy.deepcopy(self.manifest['entries'][0]));self.reject()
 def test_unused_review_path_rejected(self):
    self.review['allowed_paths'].append('shared/accounting/validate.go');self.reject()
 def test_stale_before_rejected(self):
    self.manifest['entries'][0]['before']={'sha256':'0'*64,'mode':0o644};self.reject()
 def test_noop_rejected(self):
    self.manifest['entries'][0]['after']=None;self.reject()
 def test_wrong_blob_content_rejected(self):
    (self.artifact/'blobs'/self.identity['sha256']).write_bytes(b'changed');self.reject()
 def test_unsafe_mode_rejected(self):
    self.identity['mode']=0o4755;self.reject()
 def test_boolean_mode_rejected(self):
    self.identity['mode']=True;self.reject()
 def test_symlink_blob_rejected(self):
    blob=self.artifact/'blobs'/self.identity['sha256'];blob.unlink();blob.symlink_to(self.ownership);self.reject()
 def test_symlink_stage_ancestor_rejected(self):
    (self.stage/'shared').symlink_to(self.root);self.reject()
 def test_late_invalid_entry_does_not_partially_apply(self):
    self.manifest['entries'].append({'path':'shared/accounting/validate.go','before':None,'after':{'sha256':'0'*64,'mode':0o644}})
    self.review['allowed_paths'].append('shared/accounting/validate.go');self.reject()
    self.assertFalse((self.stage/self.path).exists())
 def test_wrong_baseline_rejected(self):
    self.manifest['baseline_digest']='0'*64;self.reject()
 def test_runner_controls_require_exact_paths_and_identities(self):
    root=self.root/'canonical';root.mkdir();controls={}
    for path in sorted(gate.EXACT_SERIAL_CONTROL_PATHS):
      target=root/path;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(b'reviewed runner source')
      controls[path]={'before':None,'after':patches._tree(root)[path]}
    policy={'task':'1.99','control_plane':{'serial_source_controls':controls}}
    self.assertEqual(gate.exact_serial_controls(policy,self.stage,root,patches),gate.CONTROL_PATHS|gate.EXACT_SERIAL_CONTROL_PATHS)
    controls['tools/production/qualify.py']={'before':None,'after':{'sha256':'0'*64,'mode':0o644}}
    with self.assertRaises(ValueError):gate.exact_serial_controls(policy,self.stage,root,patches)
    controls.pop('tools/production/qualify.py');controls['tools/production/serial_gate.py']['after']['mode']=0o600
    with self.assertRaises(ValueError):gate.exact_serial_controls(policy,self.stage,root,patches)


EXACT_EXTENSION_BYTES=zlib.decompress(base64.b85decode('c-pm<+m72f42JLf6oXth6Y4DOYwYD>5s(x$<HoVGmeT`@zWdUdX?xM4z+zJvFl?IoO?^D*^P7i<9buQ_{9%%%KM$8<e?}YP?vIC^tG+qr>IgF%_RP-LU<D0{F(Fh3m<xcT!rogJ8--mY@Qe+N?A~tjGOf!XT?PNr*N2DCx4VA8w#uUBuLd=p=PGLP7T$h0eh(-(TS5e|CB^_y6Qx#T!IqG0jya3bhlIg~q7iEJK-MSkg6AaMcCVsezN+s?+Puhr3Zf)Tr3m?$oyAHPP)rWqqA;ceK3S+xI7M_~EHw>QYVnjg2n75wh#UJ&`cLcj`uFQ7`*G$|-+gnr;QG9I@Hi;X170ruaGq%x<#0hh;gdYR>+|%Y{i^&wKwI!w0;MRHtmc?w%Un5E=g?X-zW7!(`PNFJC}`1X!hxa>wIY<efDWyiX1xUGhyb`iNt#((YQgL>B4w>YX-rgOPC@iJ8gRNyigkQ9&>w_;3uR7~d}{#6z65I|Td!M6wv<%S*P?A!gEJ`BGo|9AElr};D-#gkg)$pwv!&=fg(~1uk))(;sF0+zq`9{MCVMndHH4vA<<A;TiRyU|%CB(yq1V%IA)Pq4n6!?;yI34Cw_<AE3W;iEh#~mMHW9nV2DO(HmE0VK)~XkO7t*9dQD-KCwhsg8*r2B4A*WiRG*?_=pR{@*`DSw35R(orul<zCIQJ{k0DCRfWqvPpf4z(|)_og3uGd9=Lj5dtz{9o6au_B>IpO&7_KZ^<^%yv=($~kQ`Cj<Jq?SxFy0;c;Q+7%iT@GRbvTnG!CW)DJR<kLgd7a}BIFz`}m0uP5bREZ^@Gxb69-bClhk4pB@-*~y`Sc@+ei1$>wtD58XaCe!Ip3(fE<Svd=?CXPJK?eaGroe+``@=S?leqw(;Lp`>xwkWQ9dHCM}<1zc<l3a;mx+n)HkV@lW~jU$n$ktb#1trIzP)9kMA`J>ZfLNGq2cBo4$E0I$OWI`4=|I{Z9'))


class ExactSerialSourceExtension(unittest.TestCase):
 def setUp(self):
    self.tmp=tempfile.TemporaryDirectory(prefix='exact-serial-extension-');self.addCleanup(self.tmp.cleanup)
    self.root=Path(self.tmp.name)
    self.ownership=self.root/'ownership.json';self.ownership.write_bytes(EXACT_OWNERSHIP_BYTES)
    self.extension=self.root/'extension.json';self.extension.write_bytes(EXACT_EXTENSION_BYTES)
    self.policy={'version':3,'kind':gate.EXACT_SERIAL_EXTENSION_KIND,'task':'1.99','ownership_artifact':str(self.ownership),'ownership_sha256':gate.EXACT_SERIAL_OWNERSHIP_SHA256,'extension_artifact':str(self.extension),'extension_sha256':gate.EXACT_SERIAL_EXTENSION_SHA256}
 def paths(self):return gate.exact_serial_extension_paths({'source_policy':self.policy},patches)
 def test_original336_and_exact5_with_identity_bindings(self):
    paths,entries=self.paths();self.assertEqual(len(paths),341)
    self.assertEqual(set(entries),gate.EXACT_SERIAL_EXTENSION_PATHS)
    self.assertEqual(entries['media/user/internal/user/handlers_test.go']['before']['sha256'],'baf05d3a0149c0b0aebf2bf87d96ef3937c473fd074f1878c94b54c4a098b133')
    self.assertEqual(entries['media/user/internal/user/handlers_test.go']['after']['sha256'],'7c8f6789c043693bccf91d8d9a8bddc065546c27bc3fc1a42a7b98f3b5ffd4e4')
    self.assertIsNone(entries['spec/production-system/integration/migrations/330001.sql']['before'])
 def test_wrong_extension_hash_rejected(self):
    self.policy['extension_sha256']='0'*64
    with self.assertRaises(ValueError):self.paths()
 def test_wrong_original_hash_rejected(self):
    self.policy['ownership_sha256']='0'*64
    with self.assertRaises(ValueError):self.paths()
 def test_widened_artifact_rejected_even_with_declared_new_hash(self):
    data=json.loads(EXACT_EXTENSION_BYTES);data['entries'][0]['path']='tools/production/qualify.py'
    self.extension.write_text(json.dumps(data));self.policy['extension_sha256']=hashlib.sha256(self.extension.read_bytes()).hexdigest()
    with self.assertRaises(ValueError):self.paths()
 def test_changed_source_identity_artifact_rejected(self):
    data=json.loads(EXACT_EXTENSION_BYTES);data['entries'][0]['after']['sha256']='0'*64
    self.extension.write_text(json.dumps(data))
    with self.assertRaises(ValueError):self.paths()
 def test_unknown_version_or_kind_rejected(self):
    self.policy['version']=4
    with self.assertRaises(ValueError):self.paths()
    self.policy['version']=3;self.policy['kind']=gate.EXACT_SERIAL_POLICY_KIND
    with self.assertRaises(ValueError):self.paths()
 def test_v2_still_rejects_extension_fields(self):
    old={**self.policy,'version':2,'kind':gate.EXACT_SERIAL_POLICY_KIND}
    with self.assertRaises(ValueError):gate.exact_serial_paths({'source_policy':old},patches)
 def test_extension_source_identity_must_match_manifest(self):
    stage=self.root/'stage';stage.mkdir();artifact=self.root/'artifact';artifact.mkdir()
    path='spec/production-system/integration/migrations/330001.sql'
    manifest={'version':3,'baseline_digest':gate.digest(patches._tree(stage)),'source_policy_sha256':gate.digest(self.policy),'entries':[{'path':path,'before':None,'after':{'sha256':'0'*64,'mode':0o644}}]}
    writejson(artifact/'manifest.json',manifest)
    review={'source_policy':self.policy,'manifest_sha256':hashlib.sha256((artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':[path]}
    with self.assertRaisesRegex(ValueError,'extension source identity'):gate.reconcile_serial_patch(stage,artifact,review,patches)
 def test_v3_rejects_v2_manifest(self):
    stage=self.root/'stage';stage.mkdir();artifact=self.root/'artifact';artifact.mkdir()
    writejson(artifact/'manifest.json',{'version':2,'baseline_digest':gate.digest(patches._tree(stage)),'source_policy_sha256':gate.digest(self.policy),'entries':[]})
    review={'source_policy':self.policy,'manifest_sha256':hashlib.sha256((artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':[]}
    with self.assertRaises(ValueError):gate.reconcile_serial_patch(stage,artifact,review,patches)

PRELUDE_OWNERSHIP_BYTES=zlib.decompress(base64.b85decode('c-qaKTXW*L5`f?LS4`!3jk$%ud1DeX;cNm~AhWf#b;>2nGH7Bfk7OY1*8ca?@`aE|G6-Tzo~j8qtH0K*yCqBVKfiqXbmk&K9JUDw7YiCWVu70HowNUZI%|1tzlE{aa#0sIa2w-R+dFR()No0oaenT#>ZseR*IRB6x7$rjnjL~$9k<@b$gelZ*{@RkBMm(%jV_m~7`Xv)$P>b`BM7G`a3T~hp6O|l<U7n(VGIdd-HY9q0zUr*A3lBhC;1CsCrPDz&RA?kD>tBGK|G6wF`07|Qx+ydlZ&Mcd2J2P={#hdoF%{f`|CS0r_FN<2Ly$dyP|<-ae`??-jL@p{0R9eROXz<i<N6{7=QEwwh=a_we2!EApV$Q_%fbd-t=pa>!$*R;((Gcwow!bd&yiH5F3RaXVkMHjRzk$9DXD``)Fk!Yu^ypsm7+}qPScjWx}pKVdjw<3D*bPMax<;Zv`b1Kx-08gb3kBG$*xh0#qte{)V_m32%UK5+e`A$XYiK%N88V4|dERNhs{UlGncHaU#V3>|7M60Q^`y9mJno0bLUjbAp!23Sj81cKU;`_tK+?0<?@QzaZwRRai~S<zNE{w>BsUNPGltgXT+WVHWyyUOGx9u!Q$$qgA}nM@z5K#}m*@!3vbrN_>Li74(E%3jq-xMK=728CU8L1yX&^{wARXUqvzDDp~GWf*_6;7U7(6p@x@TJu^_kUa%#Y&(I@JO1V9)Qo*JNL0+rSKSF|IsR0U9p!Htr8L)+L606W~uEyO^Z%&~9$KZz`k-hSKOP04!h^J7zs&+gx-@=YKT#lvJlDPybjs>)e<LrAEQdn~WpHU31kgeiq6`#&d${#Pt5{Vi}HKs$njYz6N2?Y<9LRUFwD4oT+hb+4kk1N6pYYn3bS>0KterAJSU<Bqgf}WWdMlISFXM0|fpw#koY1C1u^LoY7!GN9>iiq*J$W{<W%jA;U79AY>C`grl9Kt($&T4RFq4LCPc~D;4@@a?yi%Wm8Bw&x05fk*C5k)RsjypIcOCo0YUz@Xo)!4A|U={)~#%a7&0YKHmI5vCHgmGesyGv)`6&I}hEO{#~=Qwn6^pss10XnG!$EQz8oS3qeEOLSL+`E;}-5c%eDQ2()@mv;cW~?>y^kg!vKTQ}-Sz5Vi;2=kYWy2CD>ocy>JU;4f(m8|dMS-U{BznbxHV8wcT@q*AR4K;8qs+HK5vNNig0+lT%cDRAdqP7UWiX{?ero-QIQi<if@J0(Fd{*D&3yyoWl^$tRr8=026|Lr1|yGcp}}P`2_h%U%aK`5(3%5I2waPx9c{PHEsR1IQViU5wg7%ngu(Tg2#78BJBVV#DhRxjp%DpPPSM{LMBKeA9hu$lIVPB|x2^9h6i}a%<c=PJXh4;hU2g9L=;kT6)&ihB!um*Bx?$=ZwROP^r{4Y4TtdiVC~cew6@@yRC=4t)=lWQ%6Z=7KIgV@}qQ{xNNj%!2buB3eu7_fIw{5+NdY&w|gGR@ei$ek$5+hLM{#zJ17hwddKf3|(=1Q4W>yj`ueT3-8LcyFB+Sh*+Tux_yLDAFO-$owW-PH<F#3&Y)TuwxIu_peN<Z|lgB}PvLy~8}(AYWLiqywGyeO!`iZ-`+8syv^jHf!<@coD~u9IU4z%Fiem#cw1ripd4`ZX&@TTG<yA%lFt4(a;pvP^R;FaC2ls8UMZ_+(082A&SI;m2XJWq_$53_(JHysZNZl`POWakcAhB$Hlts*P<2tcD(}wa&Vy`nIk!gZn@waJj5wc#O&Qx%cdZ<<+$!QxaG$S??I0a7&>x}zzfL1PWKbysqVW#xY(e`Ic8M#VKfQnFm$xjO$N!`GK5uUD%J0gyhqPtmj#s*HCrQ4Ctu~JufEe+slC!xng~<e3E`0_U|aht*<%65oYce`7u$C;M7oV5yA&`oL-PO{?p9&#(J8>a@wm%aEP;`o<F!J;r}0=}y<4+kfypL#3pHoOBp^!?^Q{Vtxq|RDa%lkZZDt9rbyIEcLZMXM$aKl6ysfRTe8I)~C`6Wr#KL8Wds;KiAT6}TAb_}!akaI31)<>wNVy;@0i6O&n$~<vgX<hy<4tM#k~_ziPeS~r{eKP4)7T_pFiIBROE(rVi&((sTRUF?W{YhIB^`tBI;LY4rN-X6ZW>0bxE7OT6v$<}TGhzks8sc#xEx||Ijks3j?zTjAzA4z5Qz541{Jop;g!(Brwm^bPFX@*x2Q*e6mNgFw&Xnyq)0bGX(K7m&V3x0r*}M(Y;V*HO-9&4GZaR>+G=wLP2y_JOarYb-34j{Y9zb6jS_mf8>*`@ecQy^K+3E5if|u=-<nG~&zp~nb+9kxmgHmiAf->Ty-z&0W)5!|f1j0p7WvkMd~2);sIvVkfV@a1iPUD&3&2rN7il;4)6LUadoXrcQ@JhD@evJHoS5S3%=DU)4Rn%ZVpUrYeF@c{U0vN~L)-;pkB(wstmw0Lhkq363HPOz<Og>MO<wRd?`R)*;FyT_IJ<9<5F{4*NY3*FjB>a7DEXMBy&r}h<6?_qX)DPE2_jv2OHoe#i?%!lWRCFGL_2*(>ZNJRV76L7ZC1T7S~6(Xz!P9+K$wY^qqg7j;}jElQ=RiDf^$Ot6r&fxFa=tn5z({ElH#EZpeY*qkyg@7%}NxvkL9IAg<5E|^F<=NlZ<{U^||QNRQaoN@B1i^gkHqpTiG+1uY)Mp!sy~?D@OGj9v>w|oUS2$(ScIFxA9Ssq6;|1===aI)xf=E9lfBiZh>l?t%+x?qJSaK%5USV0h3S14jB%bKtb?|C+k&;F{%6VRNR`{3bdV6v5KN#ix-H7c9KNprvC>Rm^v|I(Dlp3L@c0KWdTFIvA0Umk{#;}!UA#P?JYaC*APj2a$SZ(Gd&5Q*uzg3UhrMQ93t;v<u<Jx>B>9&PLRza!j*R+D=x6g<Y(56gliSqb<1<1*~?Qk-<9aO`602z3EW!^gaR-~%7yU}`N~bPXDw;A&>pP8oQj_n_)FR@p(q)|S)jJ>rj_kFkQ*;Nh*92Jp_~(loU`UzTV9-AYo{;TK+5h>W$E8fXz9{FO&sOb<V}llkAN&ImvtDHr6`pVAs$oJtpFbbD6GajiNHyKwq>cW9FRAxd<z#es|Ykwk*#>7-Qfr?m0VQ3xRa(5Egt8@<PgzbfHhHDHl}zCZzep6%YB52u-DDK19(dYWuYA7s<b1^EIZ}rs5|n<Shw#hvLfl<;+5ZBA*Tg7#iF=9Fd`y&f5EfZikK{4Y^GBlk94eWjD}G2P-447Urs_Mqldkix`w}r!kV7~K3`}rTP70Q5X&bw6nW<Lu}Xjf=~e)$xbdJoMPd}NxwS-574$Hx1M>(-g|Y~tGpn9iTbeRUNJ`WFB1SbKPiPqhhTG|nBM12$AcN|_kTx`JL(-#bA#U?&NYg=>d3A(6ymtDRm*a=g%=z#5b7fSz9ZY7GQD!<FR7)9WGPtgcLJzZ>%bWh~?O^m}P$8wpcm3ILJaR7kclZ6_mr+$z`z-s~pViW4`tbQ~IGsXqRYk8$Xi(#aS#{JIUENlubwhU>j_$|P;jBtEHom(XjH**(<Iz`82MTCfDU~MEex+*dVKN<0s-n$cGO3O-*AJts;pmGq9L*}!u5+y>{o!p@bh;VzZ)Z2obkx6}-i#|#RJViv)nM{@+@D-kMWwsJblR^@m~=OW0!r*r|FTL}Y1E%cwZ2xSphn}_@OpSzDbqc^zD@#xyTPn~)t~h%qu0G`Q{zdM#*^a-H1w({b3Y#6R!NuXtUsGpN1@5=dVD(^S1rEXDK>q$yX#N>tejrX!#$XR%9zai$>4f08H_Fm)oELk;pL!G$~e>U!=y?&jjxB*8Ry<VOmCd4!Pg4a+V2nJ>U6P5fAkxS1z>bi9dnfFRJp-G66RJ$nSr$0)zM~pKORj7&S3O)a0}sEg%o?Z8&qtLVlw!nLIcX_pi<Me)88NZ)hV0VWYE8>mNx0&s7f`Ks3qqnXOrp-OERqnUn?~8HhuU!y_^g`SH?Wf#=i|l)luetcv(5GKb!QgDpOxy$1>udjK`I+Nq@-Owln8;IQp$(D#1u(d|e%#W|ud&!|AM2kv3>GnB4Uz)v3DS?B;6HhZ>ZF|NDLw9G%<^MV2It5m#H5Rp2tp-NqZ2vyC8JX&I&Bu8YI;=gl5=qb~octX?k%-!hADo5i1-$2XMlDvw+~0p2v1_l|1OAAI)S3E+mge0R+@xt?;dt35LH1iZPzZ<;l>t(JMbxx#Omwe<<!aPFIC@hx+BvyQgaF^j)2i#O|XT0Nuqwpp9sR<?T0rW}mOhat%wb)*omIeXVO+vd%7_^xq!!zjIJl-@E*ZyTkb8>M%Q(l3nCyT<77CX5w#td+q}&tJ8&M<H4$physd`fwtjm`H?C6;e66r#17Z8}E*7Q`!$-96vu>?Wldf$DPcku7#aFuR-YDp`Qp)IN6Ic7F+3&w;3)uQ-OQR)E9|skLxEDV)Eouv4EWJDXol*k$4ofrB@&+Ic@FjCuBlJhS||R1)j(D6QKMlJ)aF~Q6w9I<EOaH%z9M7Z+Ib({8`Jp5|}79odSsItI!90*<#IL9UM?o=<NJQeR#oE91}<Nw85gVNsmn*&8fdt@4@rOpLi6_L-39AfEW{cK>BE5ge+xy4p>Fskw<0uNlaiu5yKr}PZRTfB1TbmJhQ?n_ZL>?i}HK$j0*+w-XT(ZkD~`o@S`5;?qHs+R%&|kj2>8io{P>5E)l$?(3k!udVunw9`cwNu;DuMxjXg@MlfXuVcCNxO11?esW*o_5xmk6|GacRUO==OzI}W9Zoc8~|D>n{1sC98CFq)@kMm}MBc%%v|5tMPyCo_g87YPS`p#vqb;(og6iAm!Ia#9xWF!4LMsxX$2@<S40^g>EbcXymDWYAO$<yNDbGupJm5f-R=6Oe!%?0lIor`V{*W0a5ujOLw_ZnW;>!GgedAQy=KW}$%^TNd~A2(3Di7wn;*KfJ!zVEe3`$aXKxx{CjD8pUk*U!BcsyEu8fLlkz^_#BWz3_S+;<tLO3*5eF`Ck2^?Kip?UEFKC=WX0Z^<LL)v|2yF1Lisf<}4F?(jn-HPG@pJDw8&9z24a`|NioS8a@)1'))
PRELUDE_CHECK_BYTES=zlib.decompress(base64.b85decode('c-rlKYj@*DuHg6kD|B=3Ig-+%UVgh*XXAFeliqPZ)^;W{+wH4EktLZiMRHh_ZB3H@zCb<r5-C+ta;KAUGHppDfO-Qc6bktCAC1N@@UPMM#`5QL)3d)hZG2)6;8C!Q?tE|Wn~r?Yi{?S^-tlag7|uL!$((qR89BaZ4Ew{wLBD^}do&-&pcgJYW8yD7o7jeRN38p9uuSL}M&SEy@_QQkUPPeq6h!3AiNA=FM<@M$`hD`9cn`^skN>>+;lumaAKu^mboKV;;?+NIF3vx^z8DP>kS&@Ogd{LSVnAn291q%NlK|>XJj)@}co=3{sZMs4+|mdfk5T-Y`rbE;<SnQCjqI7z#$ed*i-$qz`%`%I&BAk{h)~&a-#A{dh-i9PE!TmH^vocjy}3VeT+)Mq4}Hh(%|a7u+j|V*B?-k(L;URiY=%keDpP@)!bb!hra|m+Eo@*~_vVc7zA6NKB$M?}DlcizGI2DiLG61*3>bd$L_!UWIf+c$j7$v_=u&%Ov6*GTN(xOYqT8U-(Bc*EMzjJdg%v+HX?I4vs-;KEfN%npYVcsX4)h_>$Brm`pE|P*6%S1hR`$#=-I=d3P<oKq5_1k*G@@%8B4H<?|44DJMIkZgN+_F0SwgB0YK);?4d{H227VZ+K`TuT39U325Lj{Osbf2SXd14$@T|MQw2g;7)j<XJufseNc*U{G!1HcWgI8?(0;ZQr1H52vxIRezr8!T@`3R(bdfoMGhX!T@Gb`+o2L}Zo$%xQ(2-IJM78wV=>sU*~$_PmZHJNYDKfHVO_Wi|c1J?G!bkp(`Br?o50~oq7C*cf)@1q~y19giSlthaF>HXIEIuxXtzXR3i`wu^Txcb}3pH9=y-kts9<JE`r>(Ma%^v&hj_tzt1?%N|+#J2Cb%aYohC*SEE=te0eE~SPbBrsT#`WYnOQ_2~zV&Hdab<N05v2M@0Go953I7_CW46MGu2Jl%@G(|4^0|Oz9Aav$V<UGWBNrCFbTPW!A$=*%(M#1|r_Ch(3sI~l52HZe4X>(635OvU)bf&jr1rr1w0;Go}r;^qSM|0so-$uTbuJ>~-9ndRXf0lXVKadbOdGXp98s@|?%moZr=tN7UMIu`xOY5=WqL~?30<$`+R{+JQ5|9EpE>zcogdm#8Be8R`E`bw0*puA^Tbqrr@=Hn4d9ZaYk+86<nmd%jnkngP(3k)+@<W3UT8}(2oh!Y7%7L*4;{psC5G$=q(<P0_ymtpn9##zP!6brSjtFez@O^h~>){4>CdGA6=)#Q(w!=GROk3L$jCt}sw-tvcpuVu-O2MbfGpNEi6te*~INz3NLAZ?NBsNUgdQU+eGGf9SD|ypWc%B*dr(q~xIivV$R&w5km088LcJY;}<!l4F`lJV&F4tdTmlwRW%m6eDm#u|j-nC~W1dJGbsim*4mQ}dyH-nd}-9<RUs$|ebz?fRL%hq5{L;v3^c9txB(kVSN+EA+%*s(f-wFpDp<|x)hF!k&<z7!f(t}|7xiiFmveXy9omPQR0siNVvM;_*}n51hP4hwk=ZFpPnrM@(`v;2@qtNXL_Jd}o^XS%`|Rm{Nl@wjM?63sR^oD%O!HzZ$BFQ%B8erhPI%V#YftOhM$P4c+N{BWs_pjk{{K2gi`v=KCm;UAh>)xOk>1g|yH8d4QHUuLgVGoG)2@z})jZBqD(nfI{%P@h7^bYO7o(69iEjZ<~Tlq#(!$tkRxA~e-Cm&Wy`6);o_;@u=Zbm6rNu@`|l_{?{0;%Sx<l9@vzC8nFh&@2czsB4C_R)C&?4_)>T#~(jj-+X^{aXo&0{eBeqG@1eHKkbRekEhO4v<S&*_UAY-DSh-q`@6yLsN0AC2B*V={k>rh?SE71LeXSoJJg({PgcG%-3!!#Vh#w?%p#QfX?q(CdGhN9s&;OAzURP1EoPF3u;&y2hU3iuf$>Sv*V^hE*0mQ?R8cIl7|Qxc77ej_veL@v?5`fQYW?D&=}k=Qe(Jcc7Lra67GUYuGXAEi+a31eNz<kOayRnLN$+0^0-RX^HmO_)+}5)SPX*TvSd6CKrmKsy*Y7US0Qe_h>u9C{wSa`Q8hc-<DEC^xK4z9#S%E_Ka%2d&n5F0#wqUwUoS9R63LU>JwU+=y`5|`vaxHr3Q_5X~Hbbl<l=AVIJHo(BH^X|1ISGpmJXyfxt70ue-_3E#l3~?YI7f1Wma?M5W)D_8T{SeRQ>(Xo&!}d-zPNn*_lv84j&ClmZ@#del`Z@F{D+HoXQO+2e0b0w?C&2R5n>&a{=oq`7#^JLn`CVHP@LnXPW68Oa{-&Uz{|oC{&$@4Jqf7A00mC`rF?ZuNSG$X^#igNC?u}ATnizXUPex@z)1b6UX_1pBMdubpm(HnpYcDq(RjTGad}^e`B%CgBr(IZ%>YW+!YC8d>e;+-6`ePaqpU2)i1D{-SYP1K<g4-X;7UczBda)f91DkUL!OT*O_*%Ky&Q?0GLi#k6^cPu9d=_t>3f|4uFw2o&`!oz7q8!5kAJ+n1oj5=1+)nj{o!!$=%m-rQhI;(?&A96+4%+N2FCtyU>_aq4^H-m_Hb$s2YZK;y_2Ioqfj@PnDj0`dX@>jAoN>!TR7)V3|47_!KyPTGjLMISXNG%pH3M(`>PIFst>UG7f{Q-F5MmR_esN5U^dhR_#a(N%HOlUt28zbTCH&Gz}h<Kb1ht&T)!4`YZRI`-bAuss-(RGpIhf4-pujHT7~E0$+V(E{xRqq1Yh!6>zO_ZTAf<?zDLm{(4&uLFqXeo*@Yf{33n<Z!n>w7Svan(Q+O7yTA{34t6^<X;Fw+DGOgl6zcLDIjm!#2nib}lshZ}j$19hOH~~X6wAhYU^VVJP*1=bKlJ$GB*1ISQK%ZH6Xw~}h^_yRRaBxa5f4w5qcNaJ-evJ>aem$e@V%9q_?l|5K*{-NDvQN<aHP&PfQFBF0Y|)^~h-2xgS+V?uJ*|XPpb&1d`VI!WaTZGG6xgDx6DzDt8DkZ_Utmh#gu%TY@|RCldYvxk6QHd>Tmo@*s?_4(OWO9nW}bZQnE}1?qhHT`Z|XW$^y{_jKb{LElKr^$t$PB``NtLR=su#WF=O8Uci~6oS>(?h>(`5?04O-oN7D%@06sW)U*m6{|L78XMoM@)-t#eC96*DzB6i2xA1kbxM>?jWPbwk*iI$jBh?~Ljv&FbEtmHet`rg2P>ok1YX#{3;=Q@*4gDxh*tDw^W79ZeE<4!}G=01Ct3kp}`y@?a*9LMb(8%|piqRSm>C~2R5_8K;sHkhf9-@YF|IQ*CEjxB(pOnqxn;!YWO<I#!k;$ndmR?`fZubq%Em)}+p642;IO))+=-?0%!v)wRh!<KN2SR*8?m7@lY!j@=Z_al6^V!(Ez*J$!uF<H`VGfXj00QFNdA4JAOk3QgA3zyJdcj9<Gj^FGwVM4%PjJ)vZz!Jp^ZG4Jz>1E*Ef+>O75{lK_Wd%o64a;?=#9CU=GYOhuz+%9%bJb+8_$>tZ+U@rE|I5UxWE!F^T@ednF3Vs&Nx~89BYXn07MREz7^YG41%G`jIvwRdc6vOMy3j8z(WN-6o}$rBxPWP8nE_McIWh52tcelGoK6E~J*ezf)beQ;zB?gK*!LIzxEbGETwc7pxVif0`1RZCvsafFuSd;6v(3j3XH*xa7ATY#|AL9v5?V<}pga#96FUu{E}qiJh5}zCzepIa(80=OClPOTyX(*~PS;ku(`fQ}-uOG~ML7Z4RXcAc<`xbobPx7q*Y!=C7E?v3IL5wet+wecZ6TE|OqN!YE-dWiCKQi#loJ@`S_0%P<~JZ(TwI;BSwQfuM<dg-NQ+^18o-R(FH;cTqh~G&#p`}aH)jg`h;M9BVuvAew5cC9?#U9$faL{%Z%pVu0oY-fz$T%?QcrGt+vh)T`PyDnHjsu2cKRLoBI>!o@kn(Cj?NVuZm8PJ;*Q`*9TybN!k%!6Ct%vLKR5vTt&Be1#!Y2F*jNr-R)6+Z3@ON)1$J69V0{R6Oh|WOPC);p;p3OGO`T7-Wh()zoe*0Ym-rP{IDspxbPQ2i@%$3XVN4gU``(-rxpF~U@uC)69QI-!(AF<aj|m$oI~ZZwJmnKnzGSaFpaX9{J$*{Z-m+FbWm|Lm8!Ig@ou<CEpm;k5!{%6_6GWiv&jVcBiHPD{VwB@<Dq<p{7P8I&{%mB!nP@!C+fKqXi+E;=WwKsWV1ND2dgd3)^SSAGE!de)eCkBY(GcF1X`Xy645{d_$U4}UOgB=CMu$CDMG}_Be5)Xh^@v3eT!96|Cp}Eq|HqZc6uWVH#HT#!C?)Zo^c4c_)7#;o-s-NgeC~afmQNlS3<F|iykei-k0$hgCTt?I3KVmiJ%iUy-+Gy8Rmqvp>ONHktHf?tf+}~ofonz}pXU0AFr~)~rW$JM3`CtmZg6_GkHTR~biIzt(<r~tph;l6YH*MJs@UZgk`e;^9BZ;b`wp#!rPp4Lsy$n?XjO`=G*4Giw;SC!_|^6ls@VJYVnhD~BPwDTUQOftUOs3`ZLyZpcR!K7iFfth{%|@W!^30qsJ}lQSZ3co*|Y7FXOaK=wfs*BDc?oaxJ2;p8}j`#$bKYU<g1~Erz!tMGq@rj`1$)gO1L)O$8{!)Vwa1J*m}Y{#*<$vHaIY6=z&<N%G$y{qw<*WWEnYDMRY#Qx<Y6xtg2lc`VYiI8`d_sRV3Ap!dN79c5zE;PzS~MG8@^0-{XSR*C@n7hu#;yh*?mvcsno<(}<yVC*)$1n7@M3MX1giR#ElXQi2b2MpKb5z^z)i_8w~$wc-|Ex5Fi(es`^4>eU!0Uk_KAYdj2!JZskB;UB${@K1#bKENYrTT$FACA8XW=lLYS;a4XC0?#$jS?m6bUIz5sxf$J+891e--5nej%Yj1!tL=E8iB<H{Of`F@TJm#s{itA-tk_~yJThWu3Z-87YIs_OW;%H721^A$9)&H@w`c-$_fqY<IHE^MY!5BWDtmy#wYs}jz~F>dt+_ud-#)7O_R6|DD(1~c2D;}=eb?~~sTl5xL0e{Mvp<R}Hip+>E<RM$!{~SNOI18Aj^?@idZPSNg%ck2Oj|@PuZF%w$z|3u6<qjNnfo90*ac7-6Tcb-9<QcqpM^H9hFW?jLj{?;s%S>1AH)H17!TGr6|D=;qba+N{%C0{R^5Z8Q*g>B7~0v1-GUbj$Nuc;`|KX4Z=ws=UGC0=9#*b7A?~P|0eOK%b@9)BtKGdLPXJfuh$UKvF0pEp?YM_NQqB}ph;eUMbmL{!NIYE)+X<T;{{6PYou)?68^U9%!#~nbq*K;#pL=CF)5eM$)@f)ulr*l{?74VyqE$~VM0NeyDB~o`>(TDDSS?>PjhW9TnA;R3d47agiErv^NzEygb=TldTaE91>A9QsR%K(0bo_vLEjCa*Oaya<L(e+b@wjgze=OP$_3J){PDEPxzSL#xSY~C5<K&~u*siua*)e`Y$eCp=<_j06RFQc@kqW(n){)8-u6gpn_it{hQZS=6$37|qSPb0CBI%e%>Gi{E$kiq3@p_LD6#^vit-ETdLZ9tgKhrAMES*DXP61^)T6@7#E>|6y_kAS_Md7m4k+}||GxQ*JfDOYJ*SAbpZd<AlkYB3eRoO<vGM8zZN+R;8&=9Ro`OE4lF~_~^wpSm0Ydz|CS9;X&=!@u4l~>ey2`O|OV`W5Z(O8SdS~S+8u@;TBXsnb^@3^*%ms`e9elO=tEP6Nb0oU5uL({v*EBL0HiG%dq4l+<`+OWSa%0hXXht>ECarg!N@y3BUPuSOwEUPv9dhN_SVzaNW_^kYu-&lvmh>>*g$0c$_7MX$l{6E(p&5)8eu5WV6$<GfVdvG?P>1Y3sgg!3}`?y%KJ27O}ubv|Cz>3P&kMtO>I)ki@uDBjmyros~v<l^O0@$+0uC;0*rC4c|GMN+{mjqYbj2;vk*Vm#@t1#l(W}GVZqa=KAEbZb-9j27fs#KI&47CtNC^%6jt%z2o$Vg)V*9NTHEap$7m>+=n>-Wc^ka5y%R2=JL@m&l5VZU~06%&}4p;a6Qc-S}i+ub><#__foU3J%a1`YzmjkG-OIU4hO)J4+1kRb3%_j?=A*vDVUf${f`hIF@x9Jeeg60qve9){n^9L{yEN4wSj?8&|9C>80Yh0wj`MxH+$^myoqc!x_xc%ocfn4<VNf&(^zRI?+QTr<Pv@OjJ<n?pS7(>}%>4hoKOVeLPFDvig3cshs?Ie3^C84n*%SUn%M?~cVY{D^=qHfG4;7lX1#oq}S!$BuKGJtWCuT(F_(T0RVINQmg!1g4Xs$bw?DQV)8~ncsyDf&P?A2Xv_H>redTxcLI<ylD0W>}`8p{EwNBaXok(D8_+htd9YH5zH39iV#ZO=4}UN$|zN<4vr@LTZ!jU{ph%uXAg{cIE>6-sPpcaKpE%E0hH)inbONh6#!MxqsrD)wZnuzWlW1=EvZPKsvf>Ev$=nELqmqXswshmF25WOix&@%u!w)`t&$&L-FlPhbVDWs23C?~U?a3jQU#8JnOCf_N@@iLkFBR_fNB-MKgzYm06H7Hg73O)0zc=D=Kz<r^f6SbB%&OEoOFtP6N{jEe>2rU1-@jzSq52Oa`|0C(|%FjHVOUv4f&>8f0wtO-h`ma{CeXDj&<&bp@>cK>-i$2es~Q^6uEc`oN#$#&I1xU-t1Rl9lc&mcqlXsPmAH@=HvzgYp@F%^vts_k=M{)MQW=v>s|(^#o=c}K#TnK4Ljs}W6mAdA&7R(wm|R6jJcmt0l+E<Z*O3fVJ|cfKn29+W@-xW-=e{fgn~R!Z2xyLv)(gA3M-i*iEq6svTWYv;$y>V%)qL!5rJT_hN{7qru13^tWgzyGCSryCoLC3rW72~ZYIGypQ<8nHtQB)733n_0INehwH(wiSQwh(s5x2gO$q)}gHoySBQ1>Byb4nXS+3+2oiPd&n*v%gDr8_<_vVby9<}drvt71{^n!UDMwlgm5t~G$tmSmfcaGKfjX2_TL^0^E-D2UByO<D@$J+GSX1m>HTl@g`_hR?Ww!iJ}%=IU&=C?eY;{0dIVtWdwWJb8a8QJrtC-U{{zi}wmQF*}CE`tg)uWh;a=Cd`9SR9$K3ICQ6O!Z~DpHz0t`v#1h3~-4CO5>4Ccz|`iz?S@&Kn7RD=#N4&sRf^0gRX@cnp0z9M%G=GVpGUkcneC5>Q#QExN$ep$EN@>y~0w-$=8C3A9-<9010!Sn3R<Hn-rmNF+qDX;t`NxB<mSega9oz=cxbDjSWOtkS~vvgljr;1%;*FRDzU}dbXNlqYOj68cZaz`%FsE7>$=zL<L&ro7@PD^Ii^49Q$?C7)80wxa4pfuri@ni=`~GF#~wjS8r-klCYo@d_lwgok^KQW(ItpcH(<Wj6auNU&2~<q+?WP5xOY0g(p^ro0mU(S06sy6q@&cx_C9hm%Ww9`=31wES-*dk7DONY{Qs=mwidgpVQLIOlf@h*8p*gMM^s{{!ZEb)nY`8&kZ+{&Sb3Yi!k&<rU8h`v|ltofA*R$vf-oi&qM@$PFIv7lSh1bgbnFAI@-z<5#up>eT>H}%yx&xzdvO$tnupz|L`#4)*YxstOH|2f~u^*Oy!W^L*L~eEpasAPsl0zi{ZHKR;wAG(Kb7AaqT36HZLwUCLh@~s87uvQ+K*kTr}abhrzu~7zCH8A9p^}LsGPx2D>y2g+MnZ1+feb*bRnbGlT;MN?~q#Q8T$@mX!bc;_vT&yu6I{zLhtu?vc!Yf$G^9)idC?GtwG}xz=C@3P}s0Hm(bO<WQ!eVfPl#f~Jon*EqwbHV9~A@MA)t6w<IA8lXlXQVS$?zyp(ijk1fYd&NInP1K}Ld%fhWy!j&6N2o81Ce2?ye}k(#46M)JoPPdh*zJG*rt=xpGx#}xFLMHH!e;N_+n9ChTbscIW@yeSy!v$eTeEXVObo3y`lWf!_*i7zfP875Heva?;*6okj%a=pO~KBJkMLT#mcUOoC*XIy!nz0T_NOMF^Ud4QsM&uyvYV$9kdODABzdw(5H&trvwl5A#4dFS30mF$0WX%gRAm$vp7~%pE^h2a*QbdM`J;-X?ZQNEqzfw^Yq7?$1pF=9_6pRc3-%QV^2H2`^@*)=#sT9$?U$_hWJNzEtl(QSoKZe$GmXg9KFM>y8_3PKpaZl-kHB0^y?!)d8zIL^_BO9{YC+v_0A>{<b{hZQ!Tx)hr`YCfpNC6tp@5-`Z-boZ_@RSQ4_*AxYi1*=W3~oh+@U#YwPX3fnyh;wD|RQGd|^hM_}8c;8_r8!sZsm{PgLVfFfaM0D*cu};b+rW!hbbjmxc$Lvj!+)nBu1=#1pC?4Ge}w!Utl%gx4<0NF0a;J9cQG^8>+=6-T~ZHtbBq(@|5%dfAl?&_m{p=8IIO2;>~{bsNOgOO#1CZ(<xw>65)jE5VbCvTWrwu-P;zZS>1;j2hNRbV%ylSk&=_tm7|p63)mugZr(dqHHE)azyZPV^KYOvA)=p4dDR*aEX}gl5SE$+=$LEtRG>1oURm?9#8~gbURq!DaSgUn$YeJ_Q6MBn667)ht9>*x%u=H_elX}3u`O>@!N0hmz)g<?O^n&xN+$`No`@__?9n{Z`tfm(iL6$$f3Cz#A9EQL`GZ>!S8K_v_uFk(&^l$j_wOIE2&Y*({|)7Vru7aQ0KX%$oM0S7i$v#FiDp31rhYq{v0nA()K{@3UdEQhKQ;js)e{|;<<X_SB{7CnP&$2U!y*LFHhI=pCblW(q_?E)>H;3TZ%-dESpbUS<_plQuB2BK)TN}05QXncK)Asdwr$oy%Pk)?ggAiF9OUS)(I41Ua7U&fG~5;%auGnVoHbvKE>q&e~y-N9&kurUTF>>6ltqTfBthMljm$`C?jb*Hme$<H1k=Av{D5@ZF0GR#3aB$#Zi1Jt0w|r9iH+jeF{?Vl#QO8K<IK~%U(>95wU$TZ{N-{1eGTUlDZ9}#N|8e<y)wfNZAhU5qli8My_FgnEj{C6&hqzj%S5@OOCq+#%6`B7yd!n`AmB%N%qiGHg?I(w3hP31$f+eif4agX0S_blL|6g3EkyWQ^`;%a@K9ID>iRMcC37|C2HS~Eti<yI5EP|)jUfpF+J2xIl^IUmG7oDuEpt+5Ct<jLG8XzDhJ~;%q0sGkd6&?!ko(h!g8>!WWC~WTsl!vG3g-Jp?4-uzZ^^#Mq^G}Vkexgp0Iax0dIJ4*lK0W5SgO}<_RZ!1}97C7Z5TSKE-xsIMQt_x@>Tm-7PgBIP*Wj%=P?>xf8{ita6E}a0ZvxhfQql)KbHJw6?R@Akvy@Cm};Zf-bmt%uQ=kd~J%aP4TrUemhhAA10GoZHli=@wF+wHpSPb_}Ua-o8oIzd~J%aP4TrUzBa|zruf<vUz_58S5v&OqcW9k{D?d}8c!E&8H}kj^Y9bI071KyveA3hBF8Bk(jFqCLsDi)aEAx65dj*Mzf`>5F8;E_D2^)OIl5fm0{<uj5;{M?z)q@NeA<wPXWrS2yCTX_DP0D~nIizM;^vEr4fM>EzX+PQ*Z>%tb5s^T;6?ZXgZ*3j-j7iATSev*jJKZ^Cbsoi9WG_Z2QaCsrNaR5m$Cj?c(p&@5>Dn*ENBX9E)Q}fXjWzwk5~#eEp~1@B}8IAdQX=8YQL`7xIHbY1eZ%>3H9V>MvXN0H?!PZRxd3v82>P1-oA#--WHzw<e2WpE=ZVShGng`FMjt)^6cA=WZA2SMJecnH3}xkSVnp-6-+(JSO6)j(-ibOgDhIHxUI(h7HrRdyncN-wh2qigvua!aL6O@HObD|4JWQA%2y<`hW+##P*veEyeDXVX$qBK9NiIbj2a+vQ=sUNkqu2mnvdphxnW}6BWniwAYR9UG5#g|#CBeM0)%{eh?XUtAq0uC&?;RSO_APIa_}*h4G7}8jB0IoePWRzi2{RXXK##km!H4gRy<2PFj(AwLx>I`l%(XSaDw(|&gPC0Z=n^=ky26%`?dIxWj)#=y3<DDFx_-2boCRrRXVz8qNlenUmhJj>7nJ%N_2~*{E$@}FRPdcLv1(m=k7Li@(Rv2v=HJAADtWjo_JmAYt~K3ai~yNaUsw<;<}Q=WD!NIXP-2XpdgQ63|!wfT}@m?oJYq75lZFMji>OY#^v#(9MDF505Rc*#;oKNu@F}@5tG=l@G~r%9rhGbs89Ur6TkYz@Ao?KV>S_f?1y3>-upq+65!V#F2~m&&i{6C(}|yquP%PPzBqgR`l>T%zpM`o>jT63z_30rtPc$91H<~jus$%X4-D%A!}`FmJ}~?f9T<u<UU+SXpB?gqWCcfs>+bKYGml{G%-AMl@>sd-E`{$>nSm_1;ZvECsBxFoKe-GP-w#RjDyB0P-w}d#S&5ZBx#*GKa3-7D)^K>(KW6HaB{#XCA{Io+{^{)U^5TX^_`mvi&eH6x?r9jgW(F9(e)ws8{=?bZ_v3dz-kjaM{qTO&9Eb(H=~~sD@83h$J+T`Yu={o9EY7GQ`>Vh@`~=-5WnAtE505H65EiVH(_4g0Z_3<o8!z<W1qSFQ9z_dgJHf9P*TepB$e!^<!JfSb_LO=C9LaZC-aQt%40T#A{gI_#Iuo`CwwX_IX3|{{dH(5L@o?tQC@PKJd=hye*^-d6Menqx%G6tLa@|Bx)nv=!`Sz#_(%+mB1Ewfbc-0Nma;)b51k@3O(KGDOd?Xc!aO}YJhzoy1c05&`L{;R-H&uq9!o1%h!ClZQTuOR$ar$Mud^h!C2M8Hzo1@Ghiflc9of}4_5tvbt%3x+ABCiH&R2wyxW0mj^Y+-tK;{~un&U$3z;PrTCQQ5?JbpCCIw)!H?*IdYLZZNQZrrS^{PQtoHH*Q5l4;?3Et~u#hxp~nYC8yhlc#Kz0bwlm_5rf&*r+xKlUwyx?KCY{e>+0jW`naw>uB(sh>f^flxUN2~tB>pI<GT8|?wOD4q|stLH5tn<ywj&}nNP(YlDfh-+%LYuCnCX-2MkDOkpi0uno7h;f~c}I)>DOBXyteON^b&+R=gqP%6!DTGlM?+U$X!O*Tl*Vrd-UuJR~X)3z)}eFq^PN3}vwY<<7rA!HI!7uRpxgJ@qQJplyGebR@3&2yImkIG!f4zNk&0e^nD`ec_=tfz~F_+5}pgKx-3dZ33-LptT9KHi6bA(AorAn?P$5Xl(+mO`sYkP|$_Vu{j&VF3_Aft`jZU?FBBdFmMCaNID{oB1qIQXR8j|ML<c(EKmeZ{N{_{S=I8Ow>h<`2r-WGahy`8@>dtQ39*{ZF=sa1$_f8f?1aCrW9f$XaT1qm7RX~m5Hye{TA<?-+eiY@RERdjU&IwvHnYiwO_<Lq2ySA~M>9}2TyC6gdSt4&WUmaK9-8a?YvI^faTdwTQHeN{r-9o~!w+#&Yk<mDq2UbK04_M!$Bi&Lj{(yV<{l;59Z<A>^(Yjvj@rapn^?bQ6Kfo-Fq|#ew%GCH{wY=r9ngkA)AB6Rl3=*|X|p}7e9{F7*m$C$pq1PFNJ}5zWsI?Vvzz{eMgXj{)f_cDgZ8JvZ9BFQrchJ6Ag+F;-=%giZ2vfdDUh-OH)@M=ZBec*%C$wgwkX#Y<=UcLTa;^ya&1wrEy}e;xwa_(Q7p>BbTLNz(s=4y3p!>7yD?+*XjxUWC>I5r$!ze^mDHI^rJ3!9c&6uGR>heYn0XUuX6|Oul~=62Q^v252G5j`O_tngGA!<kDe@<uN&_)Wn4bwV3vaA!^984x@fTg`RW94dTn@v2^#O>Y5|(#V#*tSkHKu^>mL;jSRs4}pdNx5`2LMY0EyU=-x!Dytl~o(65jq6xQ09d!<=8DU3pp@|woLMF$5c-9r7%FYA@bb^%sf(1j)2J(RK;X|Vx)Q*ok{upF!;b3akp2nox_t9@g7@F229C-s0DS;;oAf=*%-_yOS=}`nnYoC<Bc?62kFd2)S3!a5X!p2dZQ8rmP^|)U4LdUZdHlMBkY0vN5!!5h|9jIhJBy8{={?*O3cu@Qw?fUfU1fl;_yuTD1r$LBczH*fm2M(PzMFOKVQl9cnhb8`ZM&pw34eGWh8h6g+8~jn=C@iDyAam)WBO!;Gk1#Ue;)!W|L;C<E=<h9e?(dUMe1yA?#JO&f8s|xYSPV^$|pU1W_MB)JG8Y5k!3iQ6E9nM-cT9M12HNA3@Yd5cLtnuAJIU;Lgz)&DIfmwA=oinfXnNwF5KPq6+7lOAY6C7Ta1jh?zWlyc~ncbYWUZxumb2IamvrEV6bgzU6s`@}HSkJP#tyzZ(Xj?^vbY;y%z1vX1A3<{ZyUBfZCd>4bXRFI}h6bR)bR48sxuCp1e)y0wyFey?v7kYxt?st)a$n&0_zqMF(@gXodRN_axYcdH?@ey)yLHl|ex6%O@oh$*^=v+<jtVb{2WWu)%ho<fXJo72RE{Wh6lSeMA0=;CsJdP^&SOA2uoI@*XK*C{F$Dsl0~OrVR*4BuLVi3bar0oBA^sjp&Cdbuuzb*1+XI8^VPS?PK=Xr|zQHF5(eQatxz^X2i});lJB<v;G6x*zPCq#{3CPwSFQqu$$y%y&ZQQtOh<8bVj5ch7S-Z%=C{{dDEb?xNlqJYPnRMRzuqobIc|@|w8rj;X?~7+&v~=rw2$t{Jk_<GUt}LJU)~?3gaA>sxQ?@0jjRCf1E(-IMT@8E#8x+uB@%dIQzyy<*4UyXY!O@ilI|w<ohB7AnbY+h0}jUz_x9;%g}sG431RN6wFIZJVwTWY{d;0iB;%4*Tl(ReSOWcC^#GUQBR2u~S|xVD5U~hyEhinjY(&T@vIUTak$et9RyvcDUVwC?xv;Yc<?8)r%+V4o@U^P508b?)eDqh~PDI$=P)^uXod>RhNk+i>8VGQn}Ys#CAbLFk@{}A{CZpvV$<(0y9rceFAr8WO#n$OqT{Fmfkh<n20~0qlIS4^W8B+tUeJm!ucU)&T}4sR&@$`n^lR;&&iF6Ss@4Ao$s<c_7zje2}6cu28OUxR?em8fR?9eTaXk&hu&9h0h-9E?oL0>=f=zv6B7@=s$$!OT2Oek`O8U&QGnj}7gbXE`GFzl0h-!111=PisYv0;m=aQ91w7uFE?GB;v%Z6(zZOpK>f-G6y9<n8ThW9!Cm2<QZes=H2?bXhzR`7YufN81QHzY^?}^uU?f>`}@qbjfI<YqpsV1B4?)=_%!d760r~`0sMS@-=WB;E0YS$i>wMS*`QCWLb)*h9$M`i6%S$kC09+kC6W$jT}dsNmQm9<CZb9hvK#~_6U%(W;=q922EeB82Jp+N0^o4ex68Ingcv?=r71UV)`43vkF;1~KU29XdBqG-b^iXkx=WF0zTxW};HXRe&gZc^^fc^eOIL9}5}>^Sj6TqP8gMRiEkIUAs2+XQ5L14GhII-aE;6JvtjlX@<bcfNm5@gg=lSUsY`FHD*mRz8?xC_F29FG}1;c0gN+glM}eF4~Z2598&Uk)97)+&!9(dV%NSKX&S2xF(ZWdf4<1R%m+6LB)P>$m4cYlv|%5V>2e2Ct+D^7&X(?L&mAe`fiS9;84BVVr~_z8O_csC8S*!k3_~JXoPoY6bd8#I>=Dd6wi<<pz}|I*iNK97C9Q#IuW+BB!s<3#RlMHkQK1G*Nz$i$tL(`l`+gxpbG8R3G|SX8s(=or6}p<sKv5KA<G5{1-c3a%(l34PN5|o)zMbNVEh%%Z%)dNZ%_-(U?xqDe<z#h$P9|-D?gs@G4lj;E$r5<h?d6F&mA;!T+0uG1x?a@7V0oVLmY(5LJW?krniWa&aTdXczbhke)Hp1`KhLWy}5Yz@$&5E!nk~Ue)0bLVl6OCfzWqseAb&R(C#-2O&D)wBxD$<&-Kpu!*{0VOtC|r=B_@GpUnreXU;H;kK>1?ghg-4GwFqKAMKWIVj@>1t=&-^v<oXWz`You8d$x@5MGi{{4~VR?$2hp11goO^r!>dotSF93fnt0u~L<eotP8iZp`{CfVrZ)3CM>n?Gqjrb~ES0C7>UD8%6R)xNAP)IAx5<*As#Hy20cLzgD2|WHI&9Ms)yx0K=V=G$4PXO1TFs;5BgxW~J!TM`s!-W4@ik#)LwNxbsD4wjg*5gVF_f2W`!;!x0Bk>w>c>5UxnYoBG`VcQDE*m*&E=?yjwn5U)$ow#*!ZmGM8KmET&jlAo-A|5}TUo+iHPR1Qq*9(rbTHz4PO(I{8fQgA8hM|UqPO=iq>c*+|oG8GgcDPvKakf%Um?a<9ySBZpHLZWQ+MPNhqnhIMSv&*ShG$b^Dl>$hazA4u5Q1vkqjV7hx4TgUr4R3n~omblpS&1T-orQ*nEE6ACcqCZsbG1HK>vOd}SL<`NK3D5=wLYh!&y`#3{)qZq9dWkykFNcrYyarlKf3mhuKlBH|LEF3y7rH*{iAFD=-NNJ_K&Xpqn|6{thETyn>lBx7*l@{T4aoYJ>Uhm)};1J|JAr6OxlGD3#K>&Hhj-rHUt4FkKN@<C@aK)nF1@lYe-jXF5<XfCb2n9!4`!vx-nTo4Y~YvJk%&r%!ST`MStxyJn{fQX5-F5!yKsLZi+Z+Kk>#+Lm$F(8hJodqz%b-l_^6EhFgF}0yMkX=kJ<a|77+8Q0jtZ6cW-}H4O;p4*%>pGY^#Mk-+1>FfBcZGmo}169^`Vu$<#JF26BgF6f@{i9LzWL41ji?zb4vmcRsK?6aHtI%Cm%$KznhGa!zee4;m);RhsXzM)!{yXH$lrpx{63!LoR`QBo}b&VVRrXo9{NU6vN@PgRmpUrU8oa~v$M+dfT4W|A5{iFRuGMEg^gM;be{=vcV{z3m>x^E8p`+LXMz?>5EWI8w?Cqwhd+8+{Yu-DAeOwU;$K0G|M52n+@$w?n%xOr$>dy`?`n)KoK-qGG*uRkOQ2Ycr5$U3n1$i6*=YS>4I6Jk!M{ljLmZ%N-45XP)7y)%aghpm=qe^dGBNG3aEOAeHZ?j3ouof+;*V1tqBpSD;8{sZw$SZT@Fuf0Pt6n7|kVlV{9Na*n0z{}Kw4YMHynbN8=99~TS(_fnDLAd_m4Em^fS~x<Trs$LvC}{g{g26YaeLoWRocX;0d>B|^?{AEO@B@RMn3Y+M9(cjL$75gfZuqX>JwELA$KSE@s~4D;@4KV!@T7Y<{*DEVeZio=8=Q29-NF8NvY1Xuh^yec{q6t|Q?K{Oi)d;b8xOD!KuaOt0padpwg&04`AiDj1#AN<H|D!E#TQ~)ahjrSS*#3vF9Y8cc7a&i{?rs^T%o~>70aw$_Pv1>hBx*-vy?Fs;u-(J6KxhK&?yUKCXwb_DzbI*+F^JvWWBeYHz;Q_pfbEVhZl_H!-TOHK<Jky0)^`S9ri#DyWG;`J=DxxsKH^!bgp=yH?||6Nnqr5>*K7mYd9Msr=wB~gCT;W7sgZ1yoddq|M-^ePEoI>7pNCv-Dz@@XWiLRbA3WsF4M<+5{$*N8-t=4MvEYCn$?uxVrCTzaNM=o&|9sP75w|Fv)7jw;~%dtuEsxncy}?%3G~dg+<eB(y6+(#Xt7Vs*zfav{H@n&e$TKQC*3_X0B2`vwF^**E+ZH&UVai?ZA@Ubz{E-1foj*2+V!M%J*iz!YS)w6^`v$^sa;Q=GuYpAxOBuW9L2sHpFMq5a?h}QKeX{=&<}+^$&U-l9WYWGm*(aui+jwKh7T4qO~eNh1}LG;4*#AU>@TTsm52}YlP6Na91OUIxyD!HnpNcD$%$GkAc*Svv#g1W*Q4DN+aq>Oj!h7^$xA|GE?}*keWXfiauc91)PC<bK>Z}<Jagp1#t7-KBXZW^9O_h>#a+kaR&oAVv>!v9GDAQM-<N8{d#Rj1;WCJPBk*Z7gL)Z{4!bG7IhWF$*pAslA@yi3S**<vgCLuW=#KsA&irI<ZUSaDW@U^dBN$-{f;Jv(`iM*Q@t~`9S{CD>;Hpr~YMsL3r_!o;RSOMGkFrxaZB(3b%2+=?Mmn&otx@4c;wp9Y6zSpZB;YKtLO^x+5eGG-L9q&*G)vV(Wg4rA@*Nn1>2~SATs;D?FUQ#9Y-|+@PxV})(DWV#J>-98;Hb5}l!|DM4RX!nfe_Y7^_kDFKc~vnb}G$C_RGEf@te2*Kx4u9<A<x8@vF1zi_vgzzkgf|bbbEA#k;fd$Eyo?GP<|NQ_~y_kNU@lM@L5oli{8@v`*|JGJIa|&|+2Mv$qS0zn_!%nzDV!WP5ed&ID{mw68ba@Apy?p9+!RE)>sOOXqz9*_YI@=aCCsz&Det?^0Hm%VxeG72;JM&1TtP8Up_Kq4RIHa0)nP3RKo%BfvPd7T1#`PB1tbY<Rt%giGuh2}@sN53VPGHWzbk9|env>sVugNA9ISSOzl!;32cf9y4(~bzquy7cT?`#T@;Ujkz@I<rrfM=aEb>P*Z7b#rrBEa=*H4*{>*F_j|;%10SXp%zqe)Wj-9c7OkB~>V0{fC%gv?!D#g9(8!z-+u&k~3EN7N>ENrn?vS2Rl`pP-|KX<(SAQEpPo<x|JNw7Ss}JYbqhb2#o6EEBuSa;IGs0tBbi_!D*MsrR#q|vc;Om?3uP&}fl8v7BMC-?5ah+y=GOw6N*#CZ)I9m@7_V<Q4wEs=1iyMoP?NIb8F6oK$yz;sL{+v9^1!4hSnvT<w3d1r;i*DxpmVT0*gWZ&7%tPW7zGF2h6iEkh47qrtC^n9GmV$UN`SCG38QZjJMEI2&$<MM4NHLOR3S+iLl+j8zfW_d#Zm0}jw%;p;F4+KOf$d2T*c33kj4ECP$$s6j3_{`NCT5`3`ktc@H&Ca~fyeUc+H!*X8BY`|(#7hDrh{IJiz;<L-lR|-D+I10F%Nnbg#$;NlZ<eAzJ1GIR64BZ(A2ja)BSQhdTM&u8HJqq(BPf}2_vqW_#7Q=gjWmY3Bz!sy(zBUD%RuXjLtk@C_?wp*Aaz;JUd9G3g3=~Iywstd1q<{EM=&$y+Mx#Zepksca%}l%y_p_vStP;hHuY}=nf~oegY|6xU$2Ziq}Sd?qYSl0;akKx=bdNpxBF$<=@st&@84zIIMxzEnqRx0@frit*rVOngz{4_rsG=J+XKww@|4KS;n76gT%JGr$69j@xifTct>>L1Tn#!=6OCpgDF=#2mz_d<4c5^Fm^G4Qh;7dwpy~)lC73(wPe!_X}4Ez`qr2PkGM(j_4WI&#v1sg_nPdB$nIP$oR4<4<BZvMoLD+tdM|J>qKCy@3spFB2D{AWwi!v4ABf*rd*=J7j4}1`5`nHI>nOYYD<8(kbZQIIvoe$8_-NrT%}}U0CDF3*)r0RqVTDZQxDt~oS#_)iWlm@%&^ii^Kw-M7GPEjxN`q@w2=@-$uUDNn;gl*k`!2>|1IW+#eODL0irM^lJ@Z>X{y{)YSG&NS91R}}p1U!5`l?9G)ZXAB12lF-c{-b&$v2WnrU(ntyG3QVt2Mg(F@Pd(Y&Al05)eUgjRXEhTCy^w2}}yfu&IrAE$8X)41Y(mHDQe*&IH?^8=`QD{?*&34xcTS;kMH*D}*X)uUk!^=UW^*Sn|8#KD%6}mD$jmrCM2%)SAbiUh{Z9MWCwQqk9SIX!s}GuT|Katu&F(nK_9}=2RKan)r$2#Sg>MOa><gqhg?A3oJ+I0tYWiGKVM-j{hc}VdKS`U7XI#dTnIrQ)1#?F~Q)K6ok}{Bg$xC_CG&VlyaMhRK*uP3y%Vp)p45!<!MzU1G5;G71^#XF%hN1p1B#`6Mo#oZtWO_6QtyB44bC>ugX=L6nL3XSY1f#%UBDqTS#cqDChjGf-p|_uX>5`6MhZ43eI-ru5s&Az^YPUshi(sq>b)=Ye?YrcWb#_XC=s0@hTLWXz{t5Pa83*Wn$FM?b=-8OS%DLZJFZE0II2==Y<Q36;;uuw69}TmCx#U0I$_K55x6cvr&;dk;eC!-#-lFn>^auWFa*cQMxP62zNqJ+<_-s6a8zQI1Kl{VCDv&=5P4ReLyz;<xjA@H$}e`v8ZPLn352-QSvI7IWrv(-g8B1%x6P7seQQa4xlcp6fvS!qEaOqe{<OENL9a?(e<0{;>-<kUWfSN;Bi06QjiW!7N+h8cKV%Are_FKIT=JqZ5Gp>hr@-r7%nrB)#N=t8o+KCuldnd`;#%a%|}@ixSwPs*xL?^>bDv*Ed~^$AQxwLtT4{laRD~QrHv_w0c_s3J4dO2uwfEjOFy{QiNV4d7_g9qJ}tOATTb<!r{43_d!BmFQ}21|Jx{&osrNkfo@cGWe($i(S6An&tMk>>`ReL?b#=bFI$vF#uddElSLds%^VQY)>gs%Tb-ubfUtOKAE_-6fY&m1TD;@J=hB5HEu`utawo0pcd-(Z~P+~qc*BI*Yyy&<nc@1oVS*jjdo|osgV$PNmc^`=7N<!avX`o>acIn^+_2E)|xKtl5)rU*<;Zl9LR39$YhfDS0(l#k+{sPxQF8BM{P{3dKzJ6T()y3<#*W(|rE}@bD1tqJLb+~tQ((4y@<TG9F=gZo~TP!S~ed;b3`%Tq=@mWC9(T>+abL&sx_F%tnaA=l0*Re^M#0_9wxtnV@Nv)Iuh-JcIA#SBOS`0BMSxpPh2exuqdSO9e^`2vScT0QZDpTKxQzl&3%>=oRs)VIepqQs(iy}A3TOn5|evP`h4$ZN3F+i9TYiSjR5mUme(mIyKJ{&WCqTRV!<m$!i_LNM6P;zN^9V%l!7_M(Dtd85_S5S!d`_&RE0NySxxB?lGR5zQE;;#~cvvz-}-Ct_=m)iZMc7LheUuyT4+Wn<=f2rMHYWJ7g{iSw)soh^{_m^jOf8lnN<VG~x+nb@2aY#|}d{ME$q8rS?mvw{L?@L`r-mT`=Xuc8Oi6=~L<F0MowT-*Bao0BP9oe}5Y6tLNBm<*xqWgkT>|qND607hX+eQ|?fl2UG9Qu9{;9jV>D6|u#+p<Vi3H6`XJipJc`28%v1x0N3;~FtPvTWGw_XoB=K4V%5dvHZe_X^L$*{_QyH2gWyTbVeUJ$=0dUD2_wd_UqPxGwCiFrX#-$+zCHZtXLhsjj}qp8v=igv4>bU97zf3}#dlfigvx8a6?G5iT_%{IB>>vLWDqRa`Dp;AKpA1&56#I^1YN>lOfuj!<2A%|afj4y-b9Hgi!)V^dNeL$e^p+H`Bj{%V4&7s01=4>t9)LS<kBhIT;#E_O$4zT|O~3*)@1XUg?n1xL3q{-KRfMVZK^<SO!$ESVdUap|@VHY=DJT^}PZ==o05w?CzxC*O0b4?F?UW5G&Kd3+hEU7k&#xy#gO^&%f;t3O-zkW4x|<wLQA85%IC7-cO^AQ{@RH|QF%vZiFAX{O_KuYnehdbDSG!3_50C^$oEJPf+SlWxDeH0N#wI;f~^Ne23XF<H;_ERvh$Dg@}#i|$C|SVnB3S0o|KQ1C&=t6Jz9#MG)~Bi6+uAs5d_l*?w*hW#DOXR4*A;kDzod{v4)&VLiuL`c7$aFxtsCFP$|9IW#F+0C`0?JsaVP%XzEi5~s%e(d?4oN_{8<smEsGo+b|aBD+WYR>BBt)eSR^{A6C?V{+C`CyWEK8$2xVF1N0>;Mqjis|XFI~a6N4AX_qB;V0Hb9iugdIbMFKAKD>`__@Q-#^|X)^Ir3oAf8glfnMrU~o7%g1Kq#k-mL=aCCGyu}@A8EPMa(;N-+Q+G`d>p@fpfK(Vr6thj|+^cBA64Qt-M;NPL`&D%CK`w>Pj+gz{J%#3jZPpz6APHP3!p^XRAMZc2*fcHLwnY!kzAv}rg#$*vOBR{)MK@!B>1k41HB{Q9|8H|F98ho18@~W0sUq)X2@#2h~{BC}hv6E#Yy<*SGVlnn%f*S^tC&W>dXHscpIzMH%xP^#=Lec4qEa*5~xMWN%SR|q2XO5eoL$j_fO*UHY{+`f`aSqK9tcX90_I9}lXdxIsuAeL;3}D>u-jOHUnc;z1yVKl19t@^~$^K;6KO7t%9UmX>4af;G35bB<z?{H;ho(85S|BM-tU=$NSbLKx{MkDg9v@Ex^VjwIxrDY@z8~7?^5lm`vu(y6H2j!Zh?7h|2!M94$tEH<{x)A~ssVm_M0>IPXAY0DUzyJaerqTG8T_FTAgbEJai41oTdND4c-HY^gW^yeK^nk#@8KEfemnNLwD2%F_40HD+xpZ*H#Z>P?^r1*Z>6MszO5*duS~Bo=(80<o1Ih<pZd2c)V#F+@Ha(ZA^C+5#b^yyQSGjOJU_p<zRqF|Z?jrV{zY+(3rX#jk=k2ZGd7->+W0}lZ^yL5Y;51)v3tSbTfXbE)i&mCS7WwZtmT;B2X#`&th5!E1iM{H36fmNNSGTt&31hYG^0uz`0Nr@(u#^#wFZWorm=z~vh(Q_H4DwD>6wNNoO<G$p{-e}9}(<VG1?myv@CLJkWd0q&9Z^LPFR!$;la!E#OVx8Oo3bODx(XNXhmeHVOB$j0RV+53iYba6~tr!xU0lcXY^7!W@hf`Dvg8w!5>a*gpKzzX^pGB4r&XUI#Z0KkoD~w5-{O&Ji}{zg%#9#$6~Q>ST05#E*Muri2<%wSqYf4J@RDYaBgG<F&+tgyjTA(R|#5vQLPnp7CWS;>J{QnbfVW)%V9``mD_yuu)iCBg#8|(I^3<!U^PLmRxRZ8pD)AC*8T@M=K^JKf}9sZ-nl@;{h-7OF^O{_cQ`QM^#Uba>Ui83l(PT{&X4&p_pI&YPQ-E${6hg`edH1|aPG;n^fk<J;$j1eAVe4RV4j)zgVSP=DhGWBGHS+77TrM-gCkyhTT$6AHP+W<!ufNr+szAT6=-!GmJupG<z3GZhBZT|8A8nveqTY3)4I%W?|)j%m+s@+zmetHBD&-5c(4R6aS3F9U7<_YM`rBgE_clH`|Zc}f9tS>y_zM|ETLuzUpq^PSN&V>9hplf+#+7^YKf+KJtnYUGl7~3{Nc}fpU-NyjkjVv_vs=C+$D>=QOrr0iRjyGKa@By9&*zkv4;l-gA*e~+Td$T$xFUFbE3P&q=z0hQ`di_J<BOH62HbUF(Er}r4QEnwiTHW3*jaU7Hwm>0nBp7csfcIAf*G_I5NW-i8f;hR<#jMSo~IiS;f-kzKQQgGz!h2hp#v?%nZsd8*AP|I+Q=MU8#Uq&X4}oX1>7}4XTxK@(4*TiWUn<L9B?_BT88C6Fm5y;=y;;-y)uG%&L!m7TuZaPg>1yg+yw$PbI1DBCs)ROGvs?=v~uwTVeBmrVGz%FeTyh=f@ZA&!1b(&NO{`54G#Q!vj}wgI$tq|L16UAg-HVvuSkk<V3AzNZ55jo8L7zERZKqZ*lpjVTaB%LRXBnz7Y9l^-|7{YALr*m^%Z8;1XU6r_in9D-SNxN+sE5aH%L=#jwl=a;3p(L~Iu@!qKOcKLk38v#a{}g@ubM%GQ0$D!~F{0D)XgfHSsOn{5`+0dF*77u`ESDg*#xwF^drhfNT1Bk%&Va6XVR00_DtdKUAJ2~BVGizxDjg%Zh-#}Ed&3XMiA*2r%!xj%<KHjAt98*96qSD^HISmF3Lr!Au(6m*NE1B`{2%;RxD*&Dk@#zlz3j9_>K7APh#t$R@I;>ZtTO5nKVrbL;$!I2?mZ|<OtUr%G)eK0&w(4FNXSSK1KjJb&Xxw#T8y<kB>@tZ*M9Ca0he-@~}c}T~zaH4I1@&gj0o7H-#kUW}U?&9_e5Ie+N4}yG(7HRi0zYBZ1qQ`VG|I0*~{5lol6{RVA#kHbFd~C?;fr*dMW(~NoE=9|<7|=37kc%5(YxP0~EqvJ(AlV+y7#Nbb3#C4{8(fbo{XKKH&o^O*e&k!et58V{N{?|lLIqehknF17W?1Z_70_y_4QTJ}oALFJuddIp-oCm}+e|Bwyn!k5ZhZ0gi}yD>r+N-6@#@3npFv^&GjXZZ2yV@+PIkkr(vwPsXQkmqq!zk<L8dVzGg8_JrxAUa>@HD<ugLw1NN6*`G?5rN-u&obv+0n&GOXwGbFv$&VqpppwFX$G=X=bdOx~8!!H#kM@b@DBOiWCqJu^%T<GJFUsg*0!l{nf#e>WVhpnfWhtpJT#7stuUUCVt6@Fnvp&b^~TL78h5;8tXTYba@R!XjD<jK6G#<!z{c0iN)DchA(D6@(;!t*-%{HNk7ChD|W=C(J2_P$QSNOP%a0xg}mqWfZgX$}oUCIn$-V&Q>#{p50t;nhNWK6wdvL<B}fChtPLyy!QZ&=skw;l7!-?A%1p$HpAkWfm&3crU8b-Bvgag<62l@vdVQ~KC0nbD3zBKWjj=c8q~g5#9;9nNvMGlXEIYqNvSf)CTR;)jQ=PDO;$h^d$aD0cvVYhaW)x&N;P12(D7=pHg-e{uNmcWL(_u=KQl~s=4%X;9wfHJoTG09UE2@|J3(VF&NUgOb2G~3QI?SEgBoL~R|7iVqbzK-6139fkkCqV0f7~Vo;tPzTQkEo7oK$&n6~k-r#h$+Mjz0a1TnngSY_aOH>trZwtYb{?!e{-c)=Vm7vAS?zO86IGUWqEiCA%5t0tuo`2CYI)rc$D=O5m^di(z3^@_a3+m9^Py=|b~42Uvikw#31K~lf{$L)Xof66vfIs'))
PRELUDE_BEFORE_BYTES=zlib.decompress(base64.b85decode('c-qBV>vPjc690_<ia}RU60v0`u(hlqTSpSYatmxO3B?DX!dM#HO7w~zv2)q{@7LWkBTKUU3Spt{Sa}{jJ->dpM9iHFJ`yZXMRJyjG;6iIAdZrZnXL!L;2MSucyOXH6Vr?ylj}H(y25c&j=x3dM6MT(y60&W;=nMVbzX#j2VNk$LGEW>%o7?9Mb;f>SzLamzwmTDh=;{Vlo5?IOMpJ{fotH|Xu<Qy%f|V@cA}swvav`+9&|@hr+DU$6Xjt>`&O-mi69EQV=>J$&tGc?!BATX3!Jgn#s6RZQky#mq@8X&n!*(RCypW??~#sqB3$^Nm$LHu6VLUcgm+GbGY%s^8hIk^{^^CTFW&JybjDq|vrAiRhpoLi&0yI`)B#<+KY&wt7uBZgfo>!r_q*<(n_l{kJ238pntsrF@!bUR*Y^;LFGk($I!24IS}ib%G-Gi-@IB|_@jJH54AkPZ-|aeXXiKhJxSj#jp-58(ikv$cyJ<Zb@KoR?soJdvs0<ALJW+~aAHiRs8z3El!%9r{zVC@JYdyFS*NoPa8QMpQcf~XDr?_rCm~h`08B?TLx67Qda4yn3VB7^g13sc50|KqvR%-}%WTBY+AvrWN_C%SOeQL2MnsMMbv;Po&EE3aF-1c`_Swi*)F*%CTYy>Bm87v~T#5*=DJXUjZp*v_ZktDdVWDK}YUf0y5V6iu(r4M&m==sPOFm}i@?hj2v+K@wg`s~q_L1bamEwaDw3m)dNiB~o)cEip<WN<c2M5%z0TT27p!0`!Am|2?{s0tmPrUE<*sKJ9VF2_M!X41ofh*M<kzqv?2q52>wX&1Bv(X-kxO(^t9j)(z_&?$0+IKi<atzrF{vf=o9K79p3=SiRKu$M2{PEU>Q@h}uH`b#m2HzRn#F%kSxo=HiL?PD?WQusw<sflo<$rUcvKAEmBk}KGOvoj=NodgeI0EH@~D1D>-MgSjDEzK388;KBAvM#kmWO)*@$4U=zkFuOT!_yTMh$x|k4>*u&919eQIQ~IC;kUFB7*f0f$}n>py^<zk!Vg;_58PXz`%@ETc3T*YIWEk@VyQO@E4I^SK{`?<XlYZdSrKfiBX)vxblkE}Aqf7#)3N#aD+Xc{NLp5#jcs6V1)j*z0x(sKvtMJ}7ACB;@KWF>aFcNNBKMjFJONYmQ^whUPX7A91~@nYzX=0Z-1l5s%z>a_5oiXZ6FXjL*bCs1glizVz%JwAwZ-HW>kuJ`fKEXi949gYLjjLJOIW|ce!RR<aAfoWBPkM+0~wnU04o^jPz_*^gozju4<!)sgwF<!8ehRf-~%n4IVntZ?DrBO6PK?Lh60m7Tp*M|vtGGTU8^Afh}<@dIfz2uzb%C$0koL;1nm~gI*lPfiFPqjgZ7Fi;B^w_MX;8;BB47FsnR8I#V(^XQ2K3LsqakeUly>?xwPmq2aZk6>)2K{yM<i0@zfQ7K|o@Oq*BMLiAQk>_d>?(6Ooz5KIW=n);WdH3tJ5y$6&@h^B}>a996TP##P=A#E?U#Dua%F5JB9zT*0XuEn*bhX&XWeh7yVJE8jQqOnXnFN8v~R)<LLH2+17~LB*gXB9HxuyN8}HN_oW46TZusiWoAN2zek_ZOoCFASA^YDZct6m84k-<-u<QFrJz=Vm{Gk9?p~~w`aZq6_%{7m>RMtvx|VXQa)vWq@kJ4LiBA^SQ5R|#=U$vM5PTT<PLO&feQF3@i4qSo|)#%S%DM{gM^6q2NRnXA0muq$%B;cQ0Se$%2E>A%M>>+whzl1G<HetiZaK3|MvZBsl+$8a=xm}2OeOPdZ@%e8%iwuBnCvYq4{kXB|!&;`GTS7cOPB8U_(Gx{rCpUZPF8bz(LDuIzgGs&@Sl;E<{udJAvEoCjw&n5saE+vg8H>DOrd^%NbIsm<XbRJI@N*BtF8@9NKsv>%|hf6I|F#_O`HD#j&)l7mLl<!4-^1Ci5P8v5TYM@5#?$?pyYC2GtB{*VsZUfuF`!VJL0(c#W{J#0#!)d2r`LP>KT}v_BK3X<0wq>$w7qvdOKX^@2BJP4+H*Aw^Zd_CR1>l9YU<Er-m<Kw#z}$-&a1lMD<s-G?QG1IwZd5n?W9hSHhn6pX3eWeYNT(?`8$y(wHIgfc*ph{F;3C{wyvoywKG7lNV_)Y<3_t?x5sacFPq2iszX4OHQnl8N=1L3+`ZGpP4{<&I6`%k=qgM<3pNNxp<%rrZ2WIOG=s7RtViHOod%K^7lD-!M=298s&|Zd8?0F7yCMKbi<xb;$;ceHy*PA#-fY^ceH{%s$RT)5v3&%s7eAwIJ{uZig~skPh+`JqH$6a|g>XgT9U=e<TT@YLY>tU-S9(2ZG6X1%VnA7$^3;sIQ|FGX}ZmgHp(;PhtR`P}NCA(amwM?TRb8=oGS5vSwQJZ$6`ZDts|YctBGIO=KfE1>87M;;KzF^{;K3x}`)z#bmM_8<Vp(n{xp(!`CpjRXVCuU@2uwxz3sjqs0+TIrP_`b;qY9k<v8S%_EnYk5bE+GX`Q+x<vR3^7|7ZTqgaoOQllEOecxbw39kx5pb{+S4_dD(455}u&`_lCAGYUjr^9&Vx)(C3W9+)wM&M#)9Y<p<$-OgT{I-yy`Hr(!JP`hoeIGnBnTAI)5Nh>H!9(j(p5y*D8V;kfdIh=-cej=C}m;I%c&F$%LT^AP}L14dAi4OyfhxV<RcqHY#iI4=pL%LMGDgP6^++P5+xW@W$OsogpfDkVFnLCt-il4cUMm+88X|I9RB?`ds3Uh+({aF{cKmRI-~4$S6-vKxq9A7<akh^+RDyrXVz!&T;&7m2WOf<ab|6E>0C&vOjsaOAW~H@6QI4*ob|Y8(#l{P$zFlF1IIK`(g%U5{*{2Y11lX|YlA)G89R7=vTqj%l=pt_ebv8TWyDdhSHEb5C)`k?T;{IVlQ1l#U-l{XECi^~nu5;&_k>~u?Tv-4ZSwqHZF0+?qmA#eP09Sd80YyK##suN&31ae$WB44VRDg)ov&$PB~ig;w@BO1b<SLh^_<#@s_uiVCB^37;s8J2q){TJJ5#8vl#peot<Eyq^^Og-Xo?(6FViv8Suao$|5foiRhd4N;?5lh@dh;MH+HRodeLqzCG#W?_^lYKL#)n3Dh0;)qiixt^H~^8n3zIUC#1<J*x@{TRTvv3%kq#_Q4F)~QSyT$4hcK8WsADzovU+qL)w!x6lgQ&U7=mgen5-MKvNQpxi9xd<BScHC}77s<X@9&i3EU}MP-L+yk-)fHM%sc)pS)+Octg1m<Lc6QC?Cjp^zjDZs6bKF;xSoP$-GaA#H+LRad^5t1Cen9EvTaf@Lnos14My+Lh^pla-HgnvUU+e|GS&B=Yf`hq@xcG46$_={RjXH^?(S@PU`PgNcMANfakL`s7j6(2Y|FD`+3ezS8T<91MWg&v_r&m8cx8Kz5Zj1zsBP%o!UEZgoKKWwcwp@;Dpe3lh(vZjG!FH}bMW9B3##4Wwu2+S3@=6ETPvRGn(7T6P}4Jv_uplP0a(jU2kX<syeZT<XBVJyacXO`od>mMfr&!*!_mnYrCrZm@=9SNzxYOCOyS%B2Z5NP2=Q;CpF?9a6~kyp&oML9{H4>X)js`sOKCP||q;9h8FV^)Dzt^96}gx8MhcWmQ!#NHH?#Bx(~61AH}BqLngK^}2L!i;SoMH}O?~`=mLX^@NJsZk83!E?Z3vH@Ovwu9g;Vk`p4^5I-mWI!R%{Z(}6II$<I|2au)4Hd=`u!L%G7N(m#WQN34M=%Y4~iC+?k)J%!^Su@-R0>BmjxOz#>))2-0=}OUxY!c@Jne^rh>B0dFGir#^J?79aJB>WNfQ&GtPYH@rN^;tXLjPL#-WGchY&2Ir$gjWtYOQREEOjb1?BpAtJDSyMF)ju_j;+Pb47xbhNOR4GlG$n5K*}u~?0}W9Z8He?x+IFLiEOzH)_H6-1E?1J={U-L$Oi<3Q^@f%7UsU+ln;2Je3P#7NBwcmGpfHAh&WMM%qmI2xg8#M6wP%#;U4znsjpuLtt2|tCXj442=R8lYdnBxO~vP3@3y$Js#2D3H;6Qz@y+^q_k`>(#kUNz*Ns2$!txV^{!OKGX7r7zaeMnSLC|Vl2I1*q5VY@E=U(mdHy%cB>Ovwv5MpSwXJ2<1fdSXQE4qVOJ+K72!zdYet}DWIkbAlsxoX2jYY|liOBJZ0COEr~w#&>}IQOA9&N{c=P;EB7XGL2WD9J|6rHuu?)|ElQflsJAQ05S?o^AhfKlNX6YgGYG?U6TJPyouAYW?@-?cwv^xUBr{MWL3IeILU#kMV<Z;l9UDZRtaj)hH8GYoVNnDIY=(c{SSDm^J_y$S(jGX11n*UkCZyOXLWW51_qka9<>uxosK0zr+o{fBQ$U|3RjV9|We}|3IyuZ9m-z@PS7|vJ-PGV#urX$Bzc+W1E>fFJJETtgUT&dAPQGfbN=a-eKiGfxLqKb(neC^=TBn<H=|(wO`^~W<`hgrBn-kZ(g}hTLBWh4*h$EyOw_Uo{Y?cap!#Go^p=0x-*sL55C|2ISEO*KTS2vFv>$P=iamLFHOpqeE$5oHEZ#&S*3i4vO{8Ljk`YE@UH6LS#)#`w)Dmg4y@Z{^4q31zG(=VOMMf1>ktjjq3)#DHuS0iZXgnxIoM-KG_i~C;eNz>vlr{VGfVLkk#%02>DMDRePM6Q{*bBunV7xDor_U|LNcv_*;L87Q#4!2C$H#{5q=W8`3BkgI#;r{pP_BHt^WhE)L!H'))
PRELUDE_AFTER_BYTES=zlib.decompress(base64.b85decode('c-qBV>vPjc690_<ia}RU60zk64he>A9Z7)QTVOd7iVr}Au{5@o$db?_c8<;ee%(DYvLwr~4Pl{BBF&?x=hyF%5^?8(j|IyT5uc?ZNgEAs8ijGn%*GvKcm+)c+&E#7ig`-6@l}+DZQ-~H$7c~Zq3Z?X_IVNp*f5&XI4{JXQ*SET)67r3h{x0(h_pRP)2Mh)KX7&3h>JyWm=cX7je$P#fn(s_Xu$p0ODEaTcEV{}q!ST~Y}y`&t^A%lPM8HL&09AXI;LUJo``vtdj3W;2!_f?7~o92HeQ#zmA-R8+G$7QIdtK#C=7kvM>65DaN)m~u;Ta=&-KEXw@!sK2|_;{dm?H7=>@JYUhyn&CT%&hO;an2Z9F+iVc1wy0bM_T3aj!qs!iJi-G~G3x7}eo`RY6F(6|k1`bO);4->?vA0ZT7jN9o|gce~n8ekGh%A#!Od(QiJuh;=IP>Yj6yY0AvExB&scm_}hB1sr1a^|G$TjS1<Cjuu))o$EDWnl2{fl>_nE&Kr80O<&9R${V`d`|>v<IaV+Vl<x2(0&_xmpm1JimS$*8TWmWGDVuT+sv5==OW3bjJu#`z(+V@K%jBmXpCTwED*EbC5L9p9w-yDPc8O9GY%Z5_V2=vL~L4$+ra@VO2~dAW^cnJ9m5J{3WG>3agQ|v->E)1&>c3Jh+`aBFa{haw`*!rFxVT>(7OjL@O<P87<<7}?vG4E+K@wY2JGIYL1balHL`!?3m#;ViCZ=;_Klr^$Y5>g2onJ<*OmsnhT{|LFw-V8P!(D{Nd$NnP=h-Y9FC1R%%q!R5hcjle=`w-LJdGr(k^HSqGvUsn@|{#91#O(p;crHae{4cwT2BA%7*Q)`TQ9OoyP;#V^5#5UPrYa@*oh<x|_G+&In#`Oay<JrBad;`<)nj2|Q6-Vj^5ge2Ig#Pp0jQ_!4Gd?*d5}C&5D)K%sIeO5bR|6u^g6OLL9r#v(wKtV%5wX%+|UzS2XSqbz5@aCHR*B1)*?4K^ei#~ei>j{lHPc$QWIO^R1Q8G25`XVOGWc(5h%z`X^!KQ~ck*SXP{liWNkmU!c!WIIhZO~%RuEp3VwD}qT?#7>Zoj$8IA1i{~VGBH1X#z1TWNy}=oi4DxH!4nx;0j7#^_H&He!i13)ZVLPaP7>~6=w7iYkHHlEgmLztlfT}uAvVsyZ$iT*_dS;eb08>K2%5oY#g-Qs_6m3;;R;ADu*-OGWih$MDntk(ppz2^$BB$U)09UaC9I!eK5pK~IWh)-krWBZfsD<V0xKBlPz|7wgozju4+Ri$g^vcd8lS;K;0+C3I4MkY?AH<@6Nj%6h60m7Tp^TUy<WLtRjVNX2;C-&IEX^-zbS<y0koL;0PPmcI*A}aiDuqWf%cNe;B{i=g)o-8BBnhMslq03!~vr;Q2K2gsrO9mf6ZZ`b!pJo3^+D5uM=C@><)6>#8uY-1_6o1l1de?Iv&MW*cUQppNP~njxbjZ($*=2UYKg|C;~I)sRs!j<*1tVBr5ZMEJhqMRT*^b;}GJ`*EO8F;VMSKoi-uFU?`CYKl6PP*R&5MdK7;2vkpSJLP+j_2r3385n1Gi+<oEsqL4?7JmI^HsfZzSiGWW9tF$>16NDshBgI$$q>|Jtp<MWnDHu;h8!?|~G7ozSl$#6RfC@`imv@cWgT+QbTPdHi-&4~<XCeC5DlCa!YU5!x8llooXXFlag@FqAA@(r5-CvmI!dZb74TFS;_)ljx4Ss<zngtJ1*h8*&dMh`Q&|W6rc|Ltq)S!t=YL}Ne_Uk`hKbK1U?OOJimHF5MY!VNZ7-&O@WuHWVXgV^#jKX-@LSa5(DEfnYU!SlMpsOCfz;K)N1aGj>vg%e)<}$Jix`G1{)!a^Cw@0ynnEn=8%?Vj@je!&_#G&B~sgz6vQNfvKIc*XjVQG$R+>iC-2D&pGSWoskw^_xpw5=zr&Dg;aj7Y}I9(uKlqvzM;=P>sz`Z|MZhNNxmpq0R*v6CB0lilASY~0`l*SI{`^9xXlV<2?25T<EaKi%uO0*tcGt)caTH)D17E`1?IRlxRGU|tdze5Ea$%=<uK;UUSv(x8(R3^h4|A-Mxf!wV5$E@uYPndlUZso6yfGCK2noqlHy8wsEcP$Z&Yj6TYgE>@><1@DER=md4q`i;g9n6fyuH}$}Fm|+7|Sf*fN17?t3v}F(K{Xn^6)A%&sfBb2_+xax_3%K?^%{wk!{L{S4<?l|Xa6w-m#`pK(>chNVrN3MA3}ZWw%I)0|T)Us<PG7EHowN5)(nfyzY7GwjIdFBIPjm5j1z$j~5A%2Xa^1(!_6TV0!I($z{1C2@TA$_t4;>r}YwTk?uJKsT?e;5cbkrKV3ah^43Hf^jefvGbS|snMpt|qDJh80t98oEIzp0|GSQmrd`Qc2+B2}J8*{9(vY%(XdtR}DwO6_-9U>aHElGlqv^u(yA9Fzf7oI&=?6Lhp#hb}EFqcVmDGL*NH09rEnKMVstzj{MjWjuqt2J9U>4!v-ogE=#XndgHI$*vD#2!TVDmH8P3$GNsEF6E$8Nbbq`(V&0x1?5xWi*d}S)MZddI+k6)jT6SMnnYdy>QdJ=<z*^`Ay;E#F56^F9&_O*HH=M_z3YlW8il1IO~vBTK@m;a^w*!&IH4qwP&c@ldoDBYC6=+|JBU>oLEs0JLnlJGOolI)D$9hKP8=plD{&@b%E7U?Vh%2t`YZ-<n57dhVR9Qb@>@JbKzewrLom>!X2DPOI-RanZ0J&{ry=QfI@Z<%dnJOs62Tr41d8aGqe!b8RlG{+N+PV4;7hSWfDkgiQ(R~$MPV(=sgOGq>#_HNDzi)q4ELkx#`M9ZkW!nGM3Mc0_My%Kkb<;*MdNuKhcTug@}v#egbZQEgA{InT6KO?&MvP|Fl2ThIsE%?_Mp;(sirjY>e{Xxbw<hlf!s!Ua(TUz(D9(2x0Rh&)~xp8y2=Ms7fv;Se9g+_!n%-ynlM0SjHIeyCP2GapCr0x(#l{O$zFllgJtR{>4m^l|BAueft3!gw85V7lpQ}mIkIyEigQ19KI`XaDRI>4R5x1U2`3aN7rE<nBn&I*7k!Go2mvY+Nx)}-dqOdS_C~_iHhF%lHo0Ta(#DV2reywBjPra0<1B>BdOJN|Wv8Ik(7DRQ&Nno%f~a7#JEZOBy2L2NdQPXXs!WC?FTu0Y)d7C_OiPKB_DrF&RB;!bwpz<*Ru8!7L@Q5O^)MYXoy`Iz@n06NQ<mvnDela15N|+}p7D?$sOP8Ch1!SY0ndt|D#U6{q>^Kd4@Hwvn$Lo8#>5<oU?ELL!4B)$>%!PLz9|n`7saqR8OJ|4;*hWtTb}4wymPtl`;hh|4F%f5d6#IHvmemnBG7c~g1ImE#*>td;&95|^~k@*<%$vjHS<Cks~*iH+-r2HS*z)~GX%0I#m6#$vgY*SO2vsJp>YfUCYLE2K#4*@WDaQ)^r|xa<x&|A%HU9JDb#mMF-A?G1HJ>9J~(M{8K-U;4*6#b7mGt5*SV`J5^UpMkeH6s#C4}x%7;Giva~Rfa3qO*Wk;_(4r>lC6~YSIyYd+B`PU2#fW|L*A9;vVT3UhZDs7s2$&{zg#HewrLwb#+-Rh;s*%;p^c@7<J$&z|2FFV49no8Y3dWJ65jiEgg!)Qg3uDYsKI#t71eLj*GR;tCybazixraf8B?>>^hvEuDR#oVn_=DpE!b=%pKbDWN>h_ON+V#N$&_hJ4%+zYt&^0`>?ZWCN2;;Vwo=*D0IuBsdyDz;UYX0aM8Ie&)=u|)`2<S82j8O489*Y(nIuIQRzjn>Uj9epoJ@o*dRPA{QTrfGOnNZ0Sf7q!+ytmdR61v)4N^_pfyDV(iH+S)lk&@3ywYH^HFN+)#Ncst;$wkj@_#Ojz*=h?`J3br#}1=|mr!^Lr4zT53G)Wv4YY3eq+BGFY+)NS%pWE*1n%-<wI&H1g3>R7YP<d+<>?CGMF=oTyy@TQQwk{VTyxpRHgCNlF20+E_25kISk`&a<D;vZKp$=L>?xIJCTPm$}yxk4tL<)S`!0Hc)7vS}Yn=og(v7F<Af8qilDd08hpZH0k<rOyFZdk}m_Q1&2udwbT}<Cz<sN(DRlvhF+0YP}d&gC9lK>Vp;9IM!=R2OkBq)3Sl|UpROgSHQN(Ao<YcSyWD{izT_vsms|&`QRa$gqaVyiGa`!^+w8q%=hbZ6EBb-L8x3-e^=F~vw#&Mj#WmrPExRLiw7-5b5l=vh_6%Vl}r`1g6Pzdg=Divh}X*}*h7fcR0x0X-d3OTs`Tm0H$)oO_+tIKeL^Oh;0vO~o8q6iVfl5*;9Kbw&lnhU<NEqnf}qv92*ShFAZXvS$-bJ!&vT5<+=ZfID8$HUE<VgLrUq>Pd(j;(YLgqFdlAM%&viwx333nDBUe8B(ON_m%8d$CQ=gpO#u3oMSva?$H_j%z-coJ0-DgQ#7%0hB2VGkWe4{Ocf&-sWcc9E6UiG`Xw^NJeyH?fc+#Y+Q6~(CRDc7Pe|9G+gn9IuVRut-nvhRJ6WD)+YfN)>qSJLzi(0Y^!s*O<2f`pGChrAqbZA@DL4CGe;3_UyZsb2;8%TweCk`JJLU~pf=soAxR-=5-x-@g1K*#9I`#&0<j?|-1y{qDo903UkPBu|Cji3sv4{oSeo`q*S<@9EQC$J*)Ai_DGX19Uh1XaYw56UZyrp9iUzUY&;FD;|$GQu`a6%cAJezLaXgAI&Q_IaYuKuSI`~^1Y?sttTVPV0?Eza!WbKMyE5S!ykOt{%aDF;#oP>FrzRFz??h%A1_Uco7~^uw-&D(Hmp+Kgy{=nXM?@=x7@4x6PcFI!EW4fjRR}9nfxrRiElju=F+(dy#Wb_=TLXjD<pdD0Xq;e^&B6vIGow}r;I=29p018-rptoxymLl@${=2oBlY$mghsJ`U^k%kUJOS7=>h7IkSn9b1Oe=C7-<JjEwM;*exHBZLV_#d;1mIcHQ_t2}9|F'))

class ApprovedSerialStagePrelude(unittest.TestCase):
 def setUp(self):
    self.tmp=tempfile.TemporaryDirectory(prefix='approved-serial-stage-');self.addCleanup(self.tmp.cleanup)
    self.root=Path(self.tmp.name);self.stage=self.root/'stage';self.stage.mkdir()
    self.artifact=self.root/'artifact';(self.artifact/'blobs').mkdir(parents=True)
    self.ownership=self.artifact/'ownership.json';self.ownership.write_bytes(PRELUDE_OWNERSHIP_BYTES)
    self.policy={'version':4,'kind':gate.PRELUDED_SERIAL_POLICY_KIND,'task':'1.99','ownership_artifact':str(self.ownership),'ownership_sha256':gate.PRELUDED_SERIAL_OWNERSHIP_SHA256}
    self.review={'source_policy':self.policy,'artifact_directory':str(self.artifact)}
    self.target=self.stage/gate.APPROVED_STAGE_PRELUDE['path'];self.target.parent.mkdir(parents=True);self.target.write_bytes(PRELUDE_BEFORE_BYTES);self.target.chmod(0o644)
    (self.artifact/'blobs'/gate.APPROVED_STAGE_PRELUDE['blob_sha256']).write_bytes(PRELUDE_AFTER_BYTES)
    self.checks=self.artifact/'mandatory-checks.json';self.checks.write_bytes(PRELUDE_CHECK_BYTES)
    self.stage_policy={'staged_checks_artifact':str(self.checks),'staged_checks_sha256':gate.MANDATORY_PRELUDED_STAGE_CHECKS_SHA256,'staged_checks':json.loads(PRELUDE_CHECK_BYTES)}
    self.registry={f'1.{i:02}':[] for i in range(1,51)}
    for check in self.stage_policy['staged_checks']:
      if check['id'].startswith('L'):self.registry['1.'+check['id'][1:3]].append(check)
 def test_387_exact_paths_and_only_approved_fixture_transform(self):
    paths,prelude=gate.exact_serial_prelude_paths(self.review,patches);self.assertEqual(len(paths),387)
    self.assertEqual(prelude,gate.APPROVED_STAGE_PRELUDE)
    old=patches._tree(self.stage);gate.apply_approved_stage_prelude(self.stage,self.review,patches)
    expected=dict(old);expected[prelude['path']]=prelude['after']
    self.assertEqual(patches._tree(self.stage),expected);self.assertEqual(self.target.read_bytes(),PRELUDE_AFTER_BYTES)
 def test_already_preluded_or_wrong_before_rejected(self):
    self.target.write_bytes(PRELUDE_AFTER_BYTES)
    with self.assertRaises(ValueError):gate.apply_approved_stage_prelude(self.stage,self.review,patches)
    self.target.write_bytes(b'wrong before')
    with self.assertRaises(ValueError):gate.apply_approved_stage_prelude(self.stage,self.review,patches)
 def test_wrong_before_mode_rejected(self):
    self.target.chmod(0o600)
    with self.assertRaises(ValueError):gate.apply_approved_stage_prelude(self.stage,self.review,patches)
 def test_wrong_blob_hash_rejected_without_source_change(self):
    (self.artifact/'blobs'/gate.APPROVED_STAGE_PRELUDE['blob_sha256']).write_bytes(b'wrong after')
    with self.assertRaises(ValueError):gate.apply_approved_stage_prelude(self.stage,self.review,patches)
    self.assertEqual(self.target.read_bytes(),PRELUDE_BEFORE_BYTES)
 def test_wrong_ownership_hash_rejected(self):
    self.policy['ownership_sha256']='0'*64
    with self.assertRaises(ValueError):gate.exact_serial_prelude_paths(self.review,patches)
 def test_mutated_prelude_or_widened_ownership_rejected(self):
    value=json.loads(PRELUDE_OWNERSHIP_BYTES);value['stage_prelude']['after']['sha256']='0'*64
    self.ownership.write_text(json.dumps(value))
    with self.assertRaises(ValueError):gate.exact_serial_prelude_paths(self.review,patches)
 def test_symlink_prelude_rejected(self):
    self.target.unlink();self.target.symlink_to(self.ownership)
    with self.assertRaises(ValueError):gate.apply_approved_stage_prelude(self.stage,self.review,patches)
 def test_exact70_checks_match_authentic68_lane_definitions(self):
    checks=gate.mandatory_preluded_stage_checks(self.stage_policy,self.registry,self.registry)
    self.assertEqual(len(checks),70);self.assertEqual(sum(len(v) for v in self.registry.values()),68)
 def test_missing_native_check_rejected(self):
    self.stage_policy['staged_checks'].pop()
    with self.assertRaises(ValueError):gate.mandatory_preluded_stage_checks(self.stage_policy,self.registry,self.registry)
 def test_mutated_native_check_rejected(self):
    self.stage_policy['staged_checks'][1]['command']=['true']
    with self.assertRaises(ValueError):gate.mandatory_preluded_stage_checks(self.stage_policy,self.registry,self.registry)
 def test_wrong_native_registry_definition_rejected(self):
    self.registry=copy.deepcopy(self.registry);self.registry['1.01'][0]['command']=['true']
    with self.assertRaises(ValueError):gate.mandatory_preluded_stage_checks(self.stage_policy,self.registry,self.registry)
 def test_wrong_native_artifact_hash_rejected(self):
    self.stage_policy['staged_checks_sha256']='0'*64
    with self.assertRaises(ValueError):gate.mandatory_preluded_stage_checks(self.stage_policy,self.registry,self.registry)
 def test_fixture_cannot_be_changed_again_by_v4_manifest(self):
    gate.apply_approved_stage_prelude(self.stage,self.review,patches)
    manifest={'version':4,'baseline_digest':gate.digest(patches._tree(self.stage)),'source_policy_sha256':gate.digest(self.policy),'entries':[{'path':gate.APPROVED_STAGE_PRELUDE['path'],'before':gate.APPROVED_STAGE_PRELUDE['after'],'after':None}]}
    writejson(self.artifact/'manifest.json',manifest)
    self.review.update({'manifest_sha256':hashlib.sha256((self.artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':[gate.APPROVED_STAGE_PRELUDE['path']]})
    with self.assertRaisesRegex(ValueError,'cannot change again'):gate.reconcile_serial_patch(self.stage,self.artifact,self.review,patches)
 def test_unpreluded_serial_baseline_rejected(self):
    manifest={'version':4,'baseline_digest':gate.digest(patches._tree(self.stage)),'source_policy_sha256':gate.digest(self.policy),'entries':[]}
    writejson(self.artifact/'manifest.json',manifest)
    self.review.update({'manifest_sha256':hashlib.sha256((self.artifact/'manifest.json').read_bytes()).hexdigest(),'allowed_paths':[]})
    with self.assertRaisesRegex(ValueError,'prelude not present'):gate.reconcile_serial_patch(self.stage,self.artifact,self.review,patches)

if __name__=='__main__':unittest.main()
