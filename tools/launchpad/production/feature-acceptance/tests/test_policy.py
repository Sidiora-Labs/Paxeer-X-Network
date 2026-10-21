import base64
import copy
import hashlib
import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

DIR=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('feature_policy',DIR/'policy.py')
p=importlib.util.module_from_spec(spec);spec.loader.exec_module(p)

class PolicyTests(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.keys=tempfile.TemporaryDirectory();cls.keyroot=pathlib.Path(cls.keys.name)
  cls.public={}
  for name in ['producer','reviewer','outsider']:
   subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(cls.keyroot/name)],check=True,capture_output=True)
   cls.public[name]=subprocess.run(['openssl','pkey','-in',str(cls.keyroot/name),'-pubout'],check=True,capture_output=True).stdout.decode()
 @classmethod
 def tearDownClass(cls):cls.keys.cleanup()
 def sign(self,payload,signer):
  with tempfile.TemporaryDirectory() as tmp:
   msg=pathlib.Path(tmp)/'msg';msg.write_bytes(p.canonical(payload))
   sig=subprocess.run(['openssl','pkeyutl','-sign','-inkey',str(self.keyroot/signer),'-rawin','-in',str(msg)],check=True,capture_output=True).stdout
  return dict(payload=payload,signer=signer,signature=base64.b64encode(sig).decode())
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=pathlib.Path(self.tmp.name)
  self.inventory=json.loads((DIR/'inventory.json').read_text());self.feature=self.inventory['features'][0]['id'];self.item=self.inventory['features'][0]
  self.now=2000000000
  self.context={k:hashlib.sha256(k.encode()).hexdigest() for k in p.BINDINGS};self.context.update(environment='private-integration',implementation_principals=['implementer'],run_id='test-run',harness='private-policy-fixture-not-product-proof')
  self.context['inventory']=p.digest(self.inventory)
  self.trust={name:dict(public_key=self.public[name],roles=[role],features=[self.feature],principal=name,enabled=True) for name,role in [('producer','producer'),('reviewer','reviewer')]}
  self.dependencies={dep['id']:True for dep in self.item['prerequisites']}
  self.payload=dict(version=1,feature=self.feature,gate='feature-user-path-v1',bindings={k:self.context[k] for k in p.BINDINGS},environment=self.context['environment'],issued_at=self.now-60,expires_at=self.now+3600,status='passed',scenarios={},artifacts=[],harness='private-policy-fixture-not-product-proof',run_id='test-run')
  self.artifact('authority.json','state',b'{"fixture":"policy test authority, not product behavior"}')
  self.artifact('receipt.json','receipt',b'{"fixture":"policy receipt only"}')
  self.artifact('trace.json','trace',b'{"fixture":"policy trace only"}')
  for name,expected in self.item['scenarios'].items():
   native=dict(scenario=name,run_id='test-run',candidate=self.context['source'],tests=1,failed=0,skipped=0,exit_code=0,assertions_sha256=p.digest(expected))
   path=name+'.json';self.artifact(path,'native_result',p.canonical(native))
   self.payload['scenarios'][name]=dict(status='passed',expected_sha256=p.digest(expected),result_artifact=path,authority_artifact='authority.json',trace_artifact='trace.json')
 def artifact(self,path,kind,raw):
  (self.root/path).write_bytes(raw)
  self.payload['artifacts'].append(dict(path=path,kind=kind,sha256=hashlib.sha256(raw).hexdigest(),size=len(raw),object_version='fixture-version-1',retain_until=self.now+181*86400))
 def envelopes(self):
  evidence=self.sign(self.payload,'producer')
  review=dict(version=1,feature=self.feature,gate=self.payload['gate'],evidence_sha256=p.digest(evidence),issued_at=self.now-30,expires_at=self.now+1800,disposition='accepted',findings=[],artifact_hashes={a['path']:a['sha256'] for a in self.payload['artifacts']})
  return evidence,self.sign(review,'reviewer')
 def run_policy(self,evidence=None,review=None):
  if evidence is None:evidence,review=self.envelopes()
  return p.qualify(self.feature,'feature-user-path-v1',evidence,review,inventory=self.inventory,context=self.context,trust=self.trust,artifact_root=self.root,dependencies=self.dependencies,now=self.now)
 def reject(self,reason=None):
  with self.assertRaisesRegex(p.Rejected,reason or '.'):
   self.run_policy()
 def test_exact_inventory_and_reproducible_spec_materialization(self):
  mapping=p.validate_inventory(self.inventory);self.assertEqual(set(mapping),p.FEATURES)
  gs=importlib.util.spec_from_file_location('inventory_generator',DIR/'build_inventory.py');g=importlib.util.module_from_spec(gs);gs.loader.exec_module(g)
  self.assertEqual(self.inventory,g.build())
 def test_valid_real_crypto_policy_fixture_not_release(self):
  result=self.run_policy();self.assertTrue(result['qualified']);self.assertFalse(result['release_qualified'])
 def test_unknown_and_absent_gate(self):
  e,r=self.envelopes()
  for gate in ['', 'capacity',None]:
   with self.subTest(gate=gate),self.assertRaises(p.Rejected):p.qualify(self.feature,gate,e,r,inventory=self.inventory,context=self.context,trust=self.trust,artifact_root=self.root,dependencies=self.dependencies,now=self.now)
 def test_every_digest_and_environment_mismatch(self):
  for field in sorted(p.BINDINGS|{'environment'}):
   old=self.context[field];self.context[field]='changed'
   with self.subTest(field=field):self.reject()
   self.context[field]=old
 def test_expired_future_and_thirty_day_evidence(self):
  for issue,expiry in [(self.now-30*86400,self.now+60),(self.now+1,self.now+100),(self.now-60,self.now)]:
   self.payload.update(issued_at=issue,expires_at=expiry);self.reject('expired')
 def test_final_smoke_not_seven_days(self):
  e,r=self.envelopes();self.payload.update(gate='final-smoke-v1',issued_at=self.now-2*86400)
  e,r=self.envelopes()
  with self.assertRaisesRegex(p.Rejected,'expired'):p.qualify(self.feature,'final-smoke-v1',e,r,inventory=self.inventory,context=self.context,trust=self.trust,artifact_root=self.root,dependencies=self.dependencies,now=self.now)
 def test_missing_dependency_and_native_not_web_substitute(self):
  self.dependencies.clear();self.reject('blocked dependency')
  device=next(f for f in self.inventory['features'] if f['id']=='PF24');self.assertTrue(any(d['kind']=='device' for d in device['prerequisites']))
 def test_forged_untrusted_and_wrong_role(self):
  e,r=self.envelopes();e['payload']['run_id']='forged'
  with self.assertRaisesRegex(p.Rejected,'signature mismatch'):self.run_policy(e,r)
  e=self.sign(self.payload,'outsider')
  with self.assertRaisesRegex(p.Rejected,'untrusted'):self.run_policy(e,r)
  self.trust['producer']['roles']=['reviewer'];self.reject('scope/role')
 def test_revoked_and_wrong_feature_trust(self):
  self.trust['producer']['enabled']=False;self.reject('scope/role')
  self.trust['producer']['enabled']=True;self.trust['producer']['features']=['OC25'];self.reject('scope/role')
 def test_self_review_principal_alias_and_implementer(self):
  for principal in ['producer','implementer']:
   self.trust['reviewer']['principal']=principal;self.reject('self review')
 def test_stale_review_wrong_hash_unresolved_findings(self):
  e,r=self.envelopes()
  for key,val in [('expires_at',self.now),('evidence_sha256','0'*64),('findings',[{'id':'F1','disposition':'open'}])]:
   bad=copy.deepcopy(r['payload']);bad[key]=val
   with self.subTest(key=key),self.assertRaises(p.Rejected):self.run_policy(e,self.sign(bad,'reviewer'))
 def test_one_byte_tamper_and_missing_trace(self):
  (self.root/'trace.json').write_bytes(b'changed');self.reject('artifact mismatch')
  (self.root/'trace.json').unlink();self.reject('artifact missing')
 def test_symlink_and_path_traversal(self):
  (self.root/'trace.json').unlink();(self.root/'trace.json').symlink_to(self.root/'authority.json');self.reject('symlink')
  self.payload['artifacts'][0]['path']='../escape';self.reject('unsafe')
 def test_immutable_version_and_retention(self):
  self.payload['artifacts'][0]['object_version']='';self.reject('retention')
  self.payload['artifacts'][0]['object_version']='v1';self.payload['artifacts'][0]['retain_until']=self.now+180;self.reject('retention')
 def test_duplicate_json_and_nonfinite(self):
  for raw in [b'{"a":1,"a":2}',b'{"a":NaN}']:
   with self.assertRaises(p.Rejected):p.strict_json(raw)
 def test_noop_skipped_scope_mismatched_results(self):
  path=self.root/'success.json';original=json.loads(path.read_text());entry=next(a for a in self.payload['artifacts'] if a['path']=='success.json')
  for key,val in [('tests',0),('skipped',1),('failed',1),('exit_code',1),('candidate','0'*64),('run_id','wrong'),('scenario','other')]:
   native=dict(original);native[key]=val;raw=p.canonical(native);path.write_bytes(raw);entry.update(size=len(raw),sha256=hashlib.sha256(raw).hexdigest())
   with self.subTest(key=key):self.reject()
 def test_missing_scenario_and_assertion_binding(self):
  self.payload['scenarios']['success']['expected_sha256']='0'*64;self.reject('assertion')
  del self.payload['scenarios']['success'];self.reject('missing scenario')
 def test_incomplete_duplicate_and_false_coverage(self):
  for results in [[],[dict(feature='PF01',qualified=True,gate='feature-user-path-v1')]*55]:
   with self.assertRaises(p.Rejected):p.release_coverage(results)
  complete=[dict(feature=f,qualified=True,gate='feature-user-path-v1') for f in p.FEATURES]
  with self.assertRaises(p.Rejected):p.release_coverage(complete)
  complete[0]['qualified']=False
  with self.assertRaises(p.Rejected):p.release_coverage(complete)
 def test_inventory_duplicate_and_missing_owner(self):
  self.inventory['features'][1]=copy.deepcopy(self.inventory['features'][0]);self.reject('inventory feature')
 def test_strict_boolean_timestamp_and_zero_artifact(self):
  self.payload['issued_at']=True;self.reject('timestamps')

 def test_cross_run_and_harness_replay_rejected(self):
  for field in ['run_id','harness']:
   original=self.context[field];self.context[field]='different-current-run';self.reject('wrong run');self.context[field]=original
 def test_hardlink_artifact_rejected(self):
  import os
  os.link(self.root/'trace.json',self.root/'alias.json');self.reject('artifact missing')
 def test_duplicate_prerequisite_and_owner(self):
  self.item['prerequisites'].append(copy.deepcopy(self.item['prerequisites'][0]));self.reject('duplicate dependencies')
 def test_malformed_evidence_is_typed_rejection(self):
  e,r=self.envelopes();e['signature']=None
  with self.assertRaises(p.Rejected):self.run_policy(e,r)

 def test_inventory_mutation_not_accepted_by_signed_evidence(self):
  self.item['prerequisites'][0]['remediation']='altered';self.reject('inventory digest')
 def test_declared_provider_and_device_artifacts_mandatory(self):
  for kind in ['vendor','device']:
   self.item['prerequisites'][1]['kind']=kind
   self.context['inventory']=p.digest(self.inventory);self.payload['bindings']['inventory']=self.context['inventory']
   with self.subTest(kind=kind):self.reject('evidence missing')
 def test_zero_artifact_and_missing_authority(self):
  entry=self.payload['artifacts'][0];entry['size']=0;self.reject('artifact size')
  entry['size']=(self.root/entry['path']).stat().st_size
  self.payload['scenarios']['success']['authority_artifact']='missing';self.reject('authoritative state')
 def test_schema_matches_native_envelope_fields(self):
  schema=json.loads((DIR/'evidence.schema.json').read_text())
  self.assertEqual(set(schema['properties']['payload']['required']),set(self.payload))
  self.assertEqual(set(schema['properties']['payload']['properties']['bindings']['required']),p.BINDINGS)

 def test_wrong_public_key_algorithm(self):
  key=pathlib.Path(self.tmp.name)/'rsa.pem'
  subprocess.run(['openssl','genpkey','-algorithm','RSA','-pkeyopt','rsa_keygen_bits:1024','-out',str(key)],check=True,capture_output=True)
  self.trust['producer']['public_key']=subprocess.run(['openssl','pkey','-in',str(key),'-pubout'],check=True,capture_output=True).stdout.decode()
  self.reject('Ed25519')

 def audit_authority(self):
  if not any(a['path']=='browser.har' for a in self.payload['artifacts']):
   self.artifact('browser.har','trace',p.canonical({'log':{'version':'1.2','creator':{'name':'policy-negative-case','version':'3'},'entries':[]},'run_id':self.context['run_id']}))
  return dict(required_traces=['browser.har','trace.json'],findings=[],blockers=[],redactions=[],private_artifact_root=self.root)
 def audit_envelope(self,evidence,review,authority):
  payload=dict(version=3,feature=self.feature,evidence_sha256=p.digest(evidence),review_sha256=p.digest(review),bindings={k:self.context[k] for k in p.BINDINGS},environment=self.context['environment'],run_id=self.context['run_id'],harness=self.context['harness'],issued_at=self.now-10,expires_at=self.now+600,required_traces_sha256=p.digest(authority['required_traces']),findings_sha256=p.digest(authority['findings']),blockers_sha256=p.digest(authority['blockers']),redactions=authority['redactions'])
  return self.sign(payload,'reviewer')
 def run_audit(self,authority=None,audit=None):
  authority=self.audit_authority() if authority is None else authority
  evidence,review=self.envelopes();self.run_policy(evidence,review)
  audit=self.audit_envelope(evidence,review,authority) if audit is None else audit
  artifacts=p.verify_artifacts(self.payload['artifacts'],self.root,self.payload['issued_at'],self.now)
  return p.verify_audit(self.feature,evidence,review,audit,context=self.context,trust=self.trust,artifacts=artifacts,audit_authority=authority,now=self.now)
 def test_phase3_real_policy_adversarial_execution_preserves_original_objects(self):
  authority=self.audit_authority();evidence,review=self.envelopes();before={a['path']:(self.root/a['path']).read_bytes() for a in self.payload['artifacts']}
  result=p.audit_adversarial(self.feature,evidence,review,required_traces=authority['required_traces'],inventory=self.inventory,context=self.context,trust=self.trust,artifact_root=self.root,dependencies=self.dependencies,now=self.now)
  self.assertEqual({c['scenario'] for c in result['checks']},{'one_byte_tamper','missing_service_trace','reviewer_conflict','stale_thirty_day_evidence'})
  self.assertTrue(all(c['rejected'] for c in result['checks']))
  self.assertEqual(before,{a['path']:(self.root/a['path']).read_bytes() for a in self.payload['artifacts']})
  self.assertFalse(result['release_qualified'])
 def test_phase3_expected_trace_cannot_be_removed_by_resigning_inventory(self):
  authority=self.audit_authority();authority['required_traces'].append('service-required.har')
  with self.assertRaisesRegex(p.Rejected,'expected service trace missing: service-required.har'):self.run_audit(authority)
 def test_phase3_independent_audit_bound_to_review_and_candidate(self):
  self.assertEqual(self.run_audit()['audit_policy_version'],3)
  authority=self.audit_authority();evidence,review=self.envelopes();audit=self.audit_envelope(evidence,review,authority)
  for field,value in [('review_sha256','0'*64),('evidence_sha256','0'*64),('environment','other'),('expires_at',self.now),('required_traces_sha256','0'*64)]:
   payload=copy.deepcopy(audit['payload']);payload[field]=value
   with self.subTest(field=field),self.assertRaises(p.Rejected):self.run_audit(authority,self.sign(payload,'reviewer'))
 def test_phase3_unresolved_critical_high_and_missing_resolution_proof_reject(self):
  for severity in ['critical','high']:
   authority=self.audit_authority();authority['findings']=[dict(id='security-'+severity,feature=self.feature,severity=severity,disposition='open',resolution_artifact=None)]
   with self.subTest(severity=severity),self.assertRaisesRegex(p.Rejected,'unresolved critical/high'):self.run_audit(authority)
   authority['findings'][0].update(disposition='resolved',resolution_artifact='absent')
   with self.assertRaisesRegex(p.Rejected,'resolution proof missing'):self.run_audit(authority)
   authority['findings'][0]['resolution_artifact']='authority.json';self.run_audit(authority)
 def test_phase3_mandatory_feature_blocker_cannot_be_waived(self):
  authority=self.audit_authority();authority['blockers']=[dict(feature='PF21',owner='L25',dependency='funding_chain125',evidence=None,remediation='Admit real vendor receipts',review_date=self.now)]
  with self.assertRaisesRegex(p.Rejected,'mandatory feature blocker: PF21'):self.run_audit(authority)
 def test_phase3_duplicate_or_wrong_scope_finding_rejects(self):
  finding=dict(id='duplicate',feature=self.feature,severity='low',disposition='open',resolution_artifact=None)
  authority=self.audit_authority();authority['findings']=[finding,dict(finding)]
  with self.assertRaisesRegex(p.Rejected,'finding identity/scope'):self.run_audit(authority)
  authority['findings']=[dict(finding,feature='not-a-feature')]
  with self.assertRaisesRegex(p.Rejected,'finding identity/scope'):self.run_audit(authority)
 def test_phase3_secret_redaction_preserves_real_json_identity_and_exact_hashes(self):
  original={'transactionHash':'0x'+'1'*64,'blockHash':'0x'+'2'*64,'chainId':'0x7d','request':{'url':'https://evidence.invalid/path?access_token=ephemeral-test-credential&transactionHash=0x'+ '1'*64,'headers':[{'name':'Authorization','value':'Bearer ephemeral-test-credential'}],'cookies':[{'name':'session','value':'ephemeral-test-credential'}],'postData':{'mimeType':'application/json','text':json.dumps({'password':'ephemeral-test-credential','transactionHash':'0x'+'1'*64})}}}
  raw=p.canonical(original);redacted,identity=p.redact_json_artifact(raw)
  self.assertNotIn(b'ephemeral-test-credential',redacted)
  self.assertEqual(p.evidence_identities(original),p.evidence_identities(p.strict_json(redacted)))
  authority=self.audit_authority();private=self.root/'private';private.mkdir();(private/'original.har').write_bytes(raw)
  self.artifact('redacted.har','trace',redacted)
  descriptor=dict(path='original.har',kind='trace',sha256=hashlib.sha256(raw).hexdigest(),size=len(raw),object_version='private-test-version',retain_until=self.now+181*86400)
  authority.update(private_artifact_root=private,redactions=[dict(artifact_path='redacted.har',original=descriptor,identity_sha256=identity)])
  self.run_audit(authority)
  wrong=p.strict_json(redacted);wrong['transactionHash']='0x'+'3'*64;tampered=p.canonical(wrong);(self.root/'redacted.har').write_bytes(tampered)
  entry=next(a for a in self.payload['artifacts'] if a['path']=='redacted.har');entry.update(size=len(tampered),sha256=hashlib.sha256(tampered).hexdigest())
  with self.assertRaisesRegex(p.Rejected,'redaction changed authoritative'):self.run_audit(authority)
 def test_phase3_published_credential_and_unstructured_private_key_reject(self):
  for raw in [b'{"access_token":"ephemeral-test-credential"}',b'-----BEGIN PRIVATE KEY-----']:
   with self.subTest(raw=raw),self.assertRaises(p.Rejected):p.assert_secret_free(raw,'trace.har')
 def test_phase3_redaction_without_authoritative_identity_rejects(self):
  with self.assertRaisesRegex(p.Rejected,'authoritative identity'):p.redact_json_artifact(b'{"password":"ephemeral-test-credential"}')
