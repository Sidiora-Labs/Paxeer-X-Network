import copy
import hashlib
import json
import pathlib
import tempfile
import unittest
import recovery as r

ROOT=pathlib.Path(__file__).parent

def config():
 return json.loads((ROOT/"config.example.json").read_text())
def checkpoint():
 return dict(chain_id=125,height=100,block_hash="a"*64,epoch=3,source_sha256="b"*64,deployment_sha256="c"*64,interface_sha256="d"*64)
def manifest():
 m=dict(version=1,environment="qualification",region="region-primary",backup_id="backup-one",postgres_system_id="12345",postgres_major=17,
        timeline=1,start_lsn="0/100",end_lsn="0/200",created_unix=100,snapshot_id="snapshot-one",checkpoint=checkpoint(),
        tables={k:dict(count=1,sha256="a"*64,snapshot_id="snapshot-one") for k in r.REQUIRED_TABLES},
        objects=[dict(key="backup/base",version_id="version-one",sha256="a"*64,size=10,kind="base"),dict(key="backup/wal",version_id="version-two",sha256="b"*64,size=10,kind="wal")],
        encryption_key="kms://primary/qualification-backups",wal_complete_through_unix=200)
 m["inventory_sha256"]=r.sha({"tables":m["tables"],"objects":m["objects"]})
 return m

class Contracts(unittest.TestCase):
 def test_configuration_and_review_only_plan(self):
  c=config();m=manifest();r.configuration(c);r.manifest(m)
  p=r.restore_plan(c,m,r.sha(m),150,"12345")
  self.assertFalse(p["traffic_cutover_allowed"]);self.assertEqual(p["new_epoch"],4)
  self.assertEqual(p["postgres_recovery"]["recovery_target_action"],"pause")
 def test_failclosed_configuration(self):
  for k,v in [("primary_region","region-recovery"),("replica_bucket","kindle-backups-primary"),("restore_principal","backup-writer"),("replica_kms_key","kms://primary/qualification-backups"),("rpo_seconds",301),("rto_seconds",1801),("wal_age_seconds",61),("daily_points",True),("postgres_major",15)]:
   with self.subTest(k=k):
    c=config();c[k]=v
    with self.assertRaises(r.Invalid):r.configuration(c)
  c=config();del c["archive_bucket"]
  with self.assertRaises(r.Invalid):r.configuration(c)
 def test_json_duplicate_nonfinite_size_and_boolean_rejected(self):
  for b in (b'{"x":1,"x":2}',b'{"x":NaN}',b'x'*(r.MAX_JSON+1),b'\xff'):
   with self.assertRaises(r.Invalid):r.load(b)
  with self.assertRaises(r.Invalid):r.integer(True)
 def test_missing_mixed_snapshot_or_untrusted_manifest(self):
  for mutate in [lambda m:m["tables"].pop("inbox"),lambda m:m["tables"]["outbox"].update(snapshot_id="snapshot-two"),lambda m:m.update(chain_id=1),lambda m:m.update(start_lsn="0/300"),lambda m:m["checkpoint"].update(chain_id=31337),lambda m:m["objects"][0].update(key="../escape")]:
   m=manifest();mutate(m)
   with self.assertRaises(r.Invalid):r.manifest(m)
  with self.assertRaises(r.Invalid):r.restore_plan(config(),manifest(),"f"*64,150,"12345")
  for t,s in [(99,"12345"),(201,"12345"),(150,"999")]:
   with self.assertRaises(r.Invalid):r.restore_plan(config(),manifest(),r.sha(manifest()),t,s)
 def test_real_filesystem_object_replica_corruption_missing_and_symlink(self):
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d);(root/"objects").mkdir();(root/"objects/one").write_bytes(b"actual-object")
   o=dict(key="objects/one",sha256=hashlib.sha256(b"actual-object").hexdigest(),size=13)
   self.assertEqual(r.verify_objects(root,[o])["verified_objects"],1)
   (root/"objects/one").write_bytes(b"bad--object--")
   with self.assertRaisesRegex(r.Invalid,"checksum.*objects/one"):r.verify_objects(root,[o])
   (root/"objects/one").unlink()
   with self.assertRaisesRegex(r.Invalid,"missing object"):r.verify_objects(root,[o])
   (root/"elsewhere").write_bytes(b"actual-object");(root/"objects/one").symlink_to(root/"elsewhere")
   with self.assertRaisesRegex(r.Invalid,"symlink"):r.verify_objects(root,[o])
 def test_raw_identity_order_and_chain(self):
  record=dict(chain_id=125,height=10,transaction_index=0,log_index=0,block_hash="a"*64,transaction_hash="b"*64,payload_sha256="c"*64)
  second={**record,"log_index":1}
  self.assertEqual(r.raw_partition([record,second])["count"],2)
  for rows in ([second,record],[record,record],[{**record,"chain_id":1}],[record,{**second,"height":11,"log_index":0}]):
   with self.assertRaises(r.Invalid):r.raw_partition(rows)
 def test_exact_reconciliation_no_float_or_zero_substitution(self):
  a=dict(checkpoint=checkpoint(),snapshot_id="snapshot-one",counts={"users":12,"outbox":1},amounts_raw={"usdl":"123456789012345678901234567890"},event_identities_sha256="e"*64)
  self.assertTrue(r.reconcile(a,copy.deepcopy(a))["reconciled"])
  b=copy.deepcopy(a);b["amounts_raw"]["usdl"]="0"
  with self.assertRaises(r.Invalid):r.reconcile(a,b)
  b=copy.deepcopy(a);b["amounts_raw"]["usdl"]=1.0
  with self.assertRaises(r.Invalid):r.reconcile(a,b)
  for key in ("height","block_hash","source_sha256","deployment_sha256","interface_sha256","epoch"):
   b=copy.deepcopy(a);b["checkpoint"][key]=101 if key=="height" else 4 if key=="epoch" else "f"*64
   with self.subTest(checkpoint_field=key):
    with self.assertRaises(r.Invalid):r.reconcile(a,b)
  b=copy.deepcopy(a);b["counts"]["outbox"]=2
  with self.assertRaises(r.Invalid):r.reconcile(a,b)
 def test_measured_evidence_missing_timeline_and_unbound_metric(self):
  e=dict(version=1,requirement="D14_032",deployment_sha256="a"*64,source_sha256="b"*64,inputs_sha256="c"*64,inventory_sha256="d"*64,
         started_unix=100,ended_unix=200,failure_start_unix=120,failure_end_unix=130,metric="dr_recovery_elapsed_seconds",
         samples=[dict(unix=t,value=t-100,service="recovery",environment="qualification",zone="region-one") for t in [100,200]],outcome="blocked",limitations=["Cross-region drill not run"])
  r.measured_evidence(e)
  for mutate in [lambda x:x.update(samples=[]),lambda x:x.update(metric="unrelated"),lambda x:x.update(failure_end_unix=999),lambda x:x["samples"][1].update(unix=100),lambda x:x["samples"][0].update(incident="unbounded-label")]:
   b=copy.deepcopy(e);mutate(b)
   with self.assertRaises(r.Invalid):r.measured_evidence(b)
 def test_declarative_resources_cover_owned_requirements_without_claiming_activation(self):
  d=json.loads((ROOT/"resources.json").read_text())
  r.resources(d)
  self.assertEqual(d["activation"],"disabled-until-coordinator-review")
  self.assertFalse(d["resources"]["drills"]["capacity_qualified"])
  self.assertFalse(d["resources"]["cutover"]["automatic_failback"])
  self.assertEqual(len(r.METRICS),10)
  self.assertIn("backup:Delete",d["iam"]["application-runtime"]["deny"])

 def test_resource_validation_fails_closed_for_every_requirement_group(self):
  source=json.loads((ROOT/"resources.json").read_text())
  mutations=[lambda d:d.update(version=2),lambda d:d.update(activation="active"),
    lambda d:d["resources"]["postgres"].update(archive_mode="off"),
    lambda d:d["resources"]["object_storage"].update(replication="same-region"),
    lambda d:d["resources"]["broker"].update(ack_contract="local-only"),
    lambda d:d["resources"]["broker"].update(rebuild_precedence=[]),
    lambda d:d["resources"]["object_storage"].update(object_lock="none"),
    lambda d:d["resources"]["drills"].update(queries=[]),
    lambda d:d["iam"]["application-runtime"].update(allow=["backup:Delete"]),
    lambda d:d["resources"]["object_storage"].update(versioning=False),
    lambda d:d["resources"]["cutover"].update(automatic_failback=True),
    lambda d:d["resources"]["drills"].update(capacity_qualified=True),
    lambda d:d["command_contracts"].update(base_backup=["sh","-c","unsafe"])]
  for mutate in mutations:
   d=copy.deepcopy(source);mutate(d)
   with self.assertRaises(r.Invalid):r.resources(d)
  for group in source["resources"]:
   d=copy.deepcopy(source);del d["resources"][group]
   with self.assertRaises(r.Invalid):r.resources(d)

def adversarial_inputs():
 return {
  'D14_031': dict(backup_verified=True,paused=True,marker_before='a'*64,marker_restored='a'*64,
                 marker_committed_unix=100,target_unix=110,deletion_unix=120,wal_archive_age_seconds=59,cross_region_lag_seconds=300),
  'D14_032': dict(loss_unix=1000,verified_read_unix=1100,last_acknowledged_unix=999,last_restored_unix=800,
                 expected_read_sha256='a'*64,actual_read_sha256='a'*64,writable_regions=1,
                 capacity_samples=[dict(unix=1050,verified_reads=10,failed_reads=0),dict(unix=1100,verified_reads=20,failed_reads=0)],
                 primary_principal='primary-reader',recovery_principal='recovery-reader'),
  'D14_033': dict(stale_write_sqlstate='55000',stale_ack_error='authentication-revoked',before_sha256='a'*64,after_sha256='a'*64,epoch_before=3,epoch_after=4),
  'D14_034': dict(pending_before=1,pending_after=0,republished_event_id='event-original',original_event_id='event-original',
                 effects_before=10,effects_after=10,inbox_before=10,inbox_after=10,delivery_attempts=2),
  'D14_035': dict(disks_removed=3,source='raw-chain-and-postgresql-outbox',target_watermark=100,consumer_watermark=100,
                 expected_identities_sha256='a'*64,actual_identities_sha256='a'*64,replayed_events=10,fabricated_events=0,old_epoch_events=0),
  'D14_036': dict(object_key='archive/object',reported_object_key='archive/object',partition_halted=True,expected_sha256='a'*64,
                 corrupt_sha256='b'*64,replacement_sha256='a'*64,independent_principal='independent-reader',archive_principal='archive-reader',
                 effects_before=10,effects_after=10),
  'D14_037': dict(dataset_first_unix=100,dataset_last_unix=100+72*3600,classification='reviewed-anonymized',fixed_watermark=100,restored_watermark=100,
                 expected_counts={'users':10},actual_counts={'users':10},expected_amounts_raw={'usdl':'-123456789012345678901234567890'},
                 actual_amounts_raw={'usdl':'-123456789012345678901234567890'},expected_identities_sha256='a'*64,actual_identities_sha256='a'*64),
  'D14_038': dict(principal='application-runtime',delete_audit_principal='application-runtime',decrypt_audit_principal='application-runtime',
                 delete_error='AccessDenied',decrypt_error='AccessDeniedException',object_before_sha256='a'*64,object_after_sha256='a'*64,
                 plaintext_bytes=0,delete_request_id='request-delete-123',decrypt_request_id='request-decrypt-123'),
  'D14_039': dict(missing_before=1,missing_after=0,missing_keys=['media/object'],false_healthy=False,restored_keys=['media/object'],
                 explicitly_unavailable_keys=[],replication_lag_seconds=300),
  'D14_040': dict(connectivity_restored_unix=100,old_primary_read_only=True,old_primary_write_sqlstate='25006',
                 old_primary_publish_error='authorization-violation',writable_regions=1,epoch_before_return=4,epoch_after_return=4,
                 before_sha256='a'*64,after_sha256='a'*64,automatic_failback=False)
 }

class AdversarialResultContracts(unittest.TestCase):
 def test_all_ten_native_result_shapes(self):
  inputs=adversarial_inputs()
  self.assertEqual(set(inputs),set(r.METRICS))
  for rid,value in inputs.items():
   with self.subTest(requirement=rid):self.assertEqual(r.adversarial_scenario(rid,value),value)
 def test_missing_fields_and_unknown_assertions_never_qualify(self):
  for rid,value in adversarial_inputs().items():
   for field in value:
    missing=copy.deepcopy(value);del missing[field]
    with self.subTest(requirement=rid,field=field),self.assertRaises(r.Invalid):r.adversarial_scenario(rid,missing)
   extra={**value,'healthy':True}
   with self.assertRaises(r.Invalid):r.adversarial_scenario(rid,extra)
 def test_each_scenario_adversary_has_an_exact_failing_outcome(self):
  changes={'D14_031':{'marker_restored':'b'*64},'D14_032':{'verified_read_unix':2801},
           'D14_033':{'stale_ack_error':'connection-timeout'},'D14_034':{'effects_after':11},
           'D14_035':{'actual_identities_sha256':'b'*64},'D14_036':{'partition_halted':False},
           'D14_037':{'actual_amounts_raw':{'usdl':'0'}},'D14_038':{'delete_error':'NoSuchBucket'},
           'D14_039':{'false_healthy':True},'D14_040':{'writable_regions':2}}
  for rid,value in adversarial_inputs().items():
   with self.subTest(requirement=rid),self.assertRaises(r.Invalid):r.adversarial_scenario(rid,{**value,**changes[rid]})
 def test_absent_capacity_and_monotonic_region_samples_are_required(self):
  original=adversarial_inputs()['D14_032']
  for samples in ([],[original['capacity_samples'][0]],list(reversed(original['capacity_samples']))):
   with self.assertRaises(r.Invalid):r.adversarial_scenario('D14_032',{**original,'capacity_samples':samples})
 def test_denied_credentials_and_implicit_failback_do_not_hide_mutations(self):
  inputs=adversarial_inputs()
  for change in ({'delete_audit_principal':'other-service'},{'plaintext_bytes':1},{'object_after_sha256':'b'*64},
                 {'delete_request_id':''},{'decrypt_error':'InvalidCiphertextException'}):
   with self.assertRaises(r.Invalid):r.adversarial_scenario('D14_038',{**inputs['D14_038'],**change})
  for change in ({'automatic_failback':True},{'epoch_after_return':5},{'old_primary_publish_error':'connection-timeout'}):
   with self.assertRaises(r.Invalid):r.adversarial_scenario('D14_040',{**inputs['D14_040'],**change})

if __name__=="__main__":unittest.main(verbosity=2)
