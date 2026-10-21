"""Actual PostgreSQL logical snapshot and epoch tests; NOT physical WAL/PITR proof."""
import concurrent.futures
import json
import pathlib
import re
import subprocess
import unittest
import uuid

ROOT=pathlib.Path(__file__).parent
CONTAINER="kindle-bootstrap-local-postgres-1"
PREFIX="kd_4c08a94d878d708d1132a6d9"

def process(args, data=None, timeout=20):
 return subprocess.run(args,input=data,capture_output=True,text=True,timeout=timeout,check=False)
def verify_fixture():
 p=process(["docker","inspect","--format",'{{json .Config.Labels}}',CONTAINER])
 if p.returncode:raise RuntimeError("authorized local PostgreSQL fixture unavailable")
 labels=json.loads(p.stdout)
 if labels.get("com.docker.compose.project")!="kindle-bootstrap-local" or labels.get("com.docker.compose.service")!="postgres":
  raise RuntimeError("fixture identity mismatch")
def sql(query,ok=True):
 p=process(["docker","exec","-i",CONTAINER,"psql","-X","-qAt","-v","ON_ERROR_STOP=1","-U","kindle_fixture","-d","kindle_fixture"],query)
 if ok and p.returncode:raise AssertionError(p.stderr)
 return p

class PostgreSQLRecovery(unittest.TestCase):
 def setUp(self):
  verify_fixture();self.schema=PREFIX+"_"+uuid.uuid4().hex[:12];self.token=uuid.uuid4().hex
  assert re.fullmatch(r"kd_[a-z0-9_]{1,59}",self.schema)
  s=self.schema
  sql(f"CREATE SCHEMA {s}; CREATE TABLE {s}.fixture_identity(token text PRIMARY KEY); INSERT INTO {s}.fixture_identity VALUES ('{self.token}');")
  sql((ROOT/"epoch.sql").read_text().replace("__SCHEMA__",s))
  sql(f"INSERT INTO {s}.recovery_control VALUES(true,3,'region-primary',1,'{'a'*64}');")
 def tearDown(self):
  verify_fixture()
  found=sql(f"SELECT token FROM {self.schema}.fixture_identity").stdout.strip()
  if found!=self.token:raise RuntimeError("cleanup refused: isolated schema ownership mismatch")
  sql(f"DROP SCHEMA {self.schema} CASCADE;")
 def proposal(self,name="proposal-one",proof=True):
  s=self.schema;p="'"+"b"*64+"'" if proof else "NULL"
  sql(f"INSERT INTO {s}.recovery_proposals(proposal_id,expected_epoch,next_region,revision,checkpoint_sha256,fence_proof_sha256,state,actor) VALUES ('{name}',3,'region-recovery',2,'{'a'*64}',{p},'validated','coordinator');")
 def test_real_postgres_epoch_activation_and_stale_write_rejection(self):
  s=self.schema;self.proposal()
  self.assertEqual(sql(f"SELECT {s}.activate('proposal-one','coordinator')").stdout.strip(),"4")
  sql(f"CREATE TABLE {s}.effects(id text PRIMARY KEY);")
  rejected=sql(f"BEGIN; SELECT {s}.assert_epoch(3,'region-primary'); INSERT INTO {s}.effects VALUES('stale'); COMMIT;",ok=False)
  self.assertNotEqual(rejected.returncode,0)
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.effects").stdout.strip(),"0")
  sql(f"BEGIN; SELECT {s}.assert_epoch(4,'region-recovery'); INSERT INTO {s}.effects VALUES('new'); COMMIT;")
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.recovery_audit WHERE state='activated' AND epoch=4").stdout.strip(),"1")
  self.assertNotEqual(sql(f"SELECT {s}.assert_epoch(4,'region-primary')",ok=False).returncode,0)
 def test_no_fence_proof_or_wrong_actor_cannot_activate(self):
  s=self.schema;self.proposal(proof=False)
  self.assertNotEqual(sql(f"SELECT {s}.activate('proposal-one','coordinator')",ok=False).returncode,0)
  sql(f"UPDATE {s}.recovery_proposals SET fence_proof_sha256='{'b'*64}' WHERE proposal_id='proposal-one'")
  self.assertNotEqual(sql(f"SELECT {s}.activate('proposal-one','attacker')",ok=False).returncode,0)
  self.assertEqual(sql(f"SELECT epoch FROM {s}.recovery_control").stdout.strip(),"3")
 def test_concurrent_cutover_has_exactly_one_activation(self):
  s=self.schema;self.proposal("proposal-one");self.proposal("proposal-two")
  with concurrent.futures.ThreadPoolExecutor(max_workers=2) as e:
   results=list(e.map(lambda n:sql(f"SELECT {s}.activate('{n}','coordinator')",ok=False),["proposal-one","proposal-two"]))
  self.assertEqual(sum(p.returncode==0 for p in results),1)
  self.assertEqual(sql(f"SELECT epoch||':'||(SELECT count(*) FROM {s}.recovery_audit) FROM {s}.recovery_control").stdout.strip(),"4:1")
 def test_audit_failure_rolls_back_authority(self):
  s=self.schema;self.proposal()
  sql(f"ALTER TABLE {s}.recovery_audit ADD CONSTRAINT injected_failure CHECK(epoch < 4);")
  self.assertNotEqual(sql(f"SELECT {s}.activate('proposal-one','coordinator')",ok=False).returncode,0)
  self.assertEqual(sql(f"SELECT epoch FROM {s}.recovery_control").stdout.strip(),"3")
  self.assertEqual(sql(f"SELECT state FROM {s}.recovery_proposals").stdout.strip(),"validated")
 def test_guard_lock_blocks_epoch_switch_until_effect_transaction_finishes(self):
  s=self.schema;self.proposal()
  # Actual row lock: a guarded transaction holds FOR SHARE for 1 second.
  import time
  with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
   future=pool.submit(sql,f"BEGIN; SELECT {s}.assert_epoch(3,'region-primary'); SELECT pg_sleep(1); COMMIT;")
   # Wait for the actual lock to exist rather than guessing a sleep deadline.
   deadline=time.monotonic()+5
   while time.monotonic()<deadline:
    q=sql(f"SELECT count(*) FROM pg_locks WHERE relation='{s}.recovery_control'::regclass AND mode='RowShareLock'")
    if int(q.stdout.strip())>0:break
   else:raise AssertionError("guard lock not observed")
   started=time.monotonic();sql(f"SELECT {s}.activate('proposal-one','coordinator')")
   elapsed=time.monotonic()-started;future.result()
   self.assertGreater(elapsed,0.2)
 def test_actual_logical_snapshot_restore_business_outbox_inbox_and_watermark(self):
  s=self.schema
  sql(f"CREATE TABLE {s}.business(id text PRIMARY KEY,amount numeric(78,0) NOT NULL); CREATE TABLE {s}.outbox(event_id text PRIMARY KEY,published boolean NOT NULL); CREATE TABLE {s}.inbox(event_id text PRIMARY KEY); CREATE TABLE {s}.watermarks(height bigint,hash text,source_sha256 text,deployment_sha256 text,interface_sha256 text); INSERT INTO {s}.business VALUES('marker',123456789012345678901234567890); INSERT INTO {s}.outbox VALUES('event-unpublished',false); INSERT INTO {s}.inbox VALUES('event-delivered'); INSERT INTO {s}.watermarks VALUES(100,'{'a'*64}','{'b'*64}','{'c'*64}','{'d'*64}');")
  p=process(["docker","exec",CONTAINER,"pg_dump","-U","kindle_fixture","-d","kindle_fixture","--schema="+s,"--no-owner","--no-privileges"],timeout=30)
  self.assertEqual(p.returncode,0,p.stderr);self.assertLess(len(p.stdout),1024*1024)
  sql(f"DELETE FROM {s}.business WHERE id='marker';")
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.business").stdout.strip(),"0")
  # Drop ONLY after verifying fixture owner identity, then restore its exact dump.
  self.assertEqual(sql(f"SELECT token FROM {s}.fixture_identity").stdout.strip(),self.token)
  sql(f"DROP SCHEMA {s} CASCADE;");sql(p.stdout)
  self.assertEqual(sql(f"SELECT amount FROM {s}.business WHERE id='marker'").stdout.strip(),"123456789012345678901234567890")
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.outbox WHERE NOT published").stdout.strip(),"1")
  self.assertEqual(sql(f"INSERT INTO {s}.inbox VALUES('event-delivered') ON CONFLICT DO NOTHING; SELECT count(*) FROM {s}.inbox").stdout.strip(),"1")
  self.assertEqual(sql(f"SELECT height||':'||hash||':'||source_sha256||':'||deployment_sha256||':'||interface_sha256 FROM {s}.watermarks").stdout.strip(),"100:"+"a"*64+":"+"b"*64+":"+"c"*64+":"+"d"*64)
  self.assertEqual(sql(f"SELECT epoch||':'||checkpoint_sha256 FROM {s}.recovery_control").stdout.strip(),"3:"+"a"*64)
  # No publish/provider call is faked: durable pending row is the measured result.

 def test_v3_actor_spoofing_cannot_change_authority(self):
  s=self.schema;self.proposal()
  before=sql(f"SELECT row_to_json(c) FROM {s}.recovery_control c").stdout.strip()
  denied=sql(f"SELECT {s}.activate_fenced_v3('proposal-one','{'c'*64}','{'d'*64}')",ok=False)
  self.assertNotEqual(denied.returncode,0)
  self.assertIn('authenticated recovery principal required',denied.stderr)
  self.assertEqual(sql(f"SELECT row_to_json(c) FROM {s}.recovery_control c").stdout.strip(),before)
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.recovery_audit").stdout.strip(),'0')
 def test_v3_changed_validation_expiry_and_unfenced_activation_are_denied(self):
  s=self.schema;self.proposal()
  sql(f"UPDATE {s}.recovery_proposals SET actor=session_user WHERE proposal_id='proposal-one'; INSERT INTO {s}.recovery_validations_v3(proposal_id,observation_sha256,scenarios_sha256,actor,valid_until) VALUES('proposal-one','{'c'*64}','{'d'*64}',session_user,clock_timestamp()+interval '2 minutes');")
  for observation,scenarios in [('e'*64,'d'*64),('c'*64,'e'*64),('c'*64,'d'*64)]:
   denied=sql(f"SELECT {s}.activate_fenced_v3('proposal-one','{observation}','{scenarios}')",ok=False)
   self.assertNotEqual(denied.returncode,0)
   self.assertEqual(sql(f"SELECT epoch FROM {s}.recovery_control").stdout.strip(),'3')
   self.assertEqual(sql(f"SELECT count(*) FROM {s}.recovery_audit").stdout.strip(),'0')
  sql(f"UPDATE {s}.recovery_validations_v3 SET validated_at=clock_timestamp()-interval '10 minutes',valid_until=clock_timestamp()-interval '1 minute';")
  denied=sql(f"SELECT {s}.activate_fenced_v3('proposal-one','{'c'*64}','{'d'*64}')",ok=False)
  self.assertNotEqual(denied.returncode,0)
  self.assertIn('fresh exact recovery validation required',denied.stderr)
 def test_v3_interrupted_validation_is_not_activation_authority(self):
  s=self.schema;self.proposal()
  sql(f"UPDATE {s}.recovery_proposals SET actor=session_user WHERE proposal_id='proposal-one'; BEGIN; INSERT INTO {s}.recovery_validations_v3(proposal_id,observation_sha256,scenarios_sha256,actor,valid_until) VALUES('proposal-one','{'c'*64}','{'d'*64}',session_user,clock_timestamp()+interval '2 minutes'); ROLLBACK;")
  self.assertEqual(sql(f"SELECT count(*) FROM {s}.recovery_validations_v3").stdout.strip(),'0')
  denied=sql(f"SELECT {s}.activate_fenced_v3('proposal-one','{'c'*64}','{'d'*64}')",ok=False)
  self.assertNotEqual(denied.returncode,0)
  self.assertEqual(sql(f"SELECT epoch||':'||region FROM {s}.recovery_control").stdout.strip(),'3:region-primary')
 def test_v3_public_activation_and_control_table_grants_are_absent(self):
  s=self.schema
  self.assertEqual(sql(f"SELECT count(*) FROM pg_proc p,aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid='{s}.activate_fenced_v3(text,text,text)'::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE'").stdout.strip(),'0')
  self.assertEqual(sql(f"SELECT count(*) FROM pg_class c,aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a WHERE c.oid='{s}.recovery_validations_v3'::regclass AND a.grantee=0").stdout.strip(),'0')

if __name__=="__main__":unittest.main(verbosity=2)
