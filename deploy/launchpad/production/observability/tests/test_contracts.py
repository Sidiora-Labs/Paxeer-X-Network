import copy,json,pathlib,sys,unittest
P=pathlib.Path(__file__).resolve().parents[1];sys.path.insert(0,str(P));import contracts as c
class Contracts(unittest.TestCase):
 def setUp(self):self.config=json.loads((P/'config.json').read_text())
 def test_strict_configuration_and_missing_dependencies(self):
  c.validate(self.config)
  for mutate in [lambda x:x.update(extra=True),lambda x:x.pop('exporter'),lambda x:x.update(series_per_instance=20001),lambda x:x['alerts'][0].update(owner=''),lambda x:x['capacity'].update(status='qualified'),lambda x:x['probes'][0].update(economic_write=True),lambda x:x.update(revision=True),lambda x:x.update(success_sample_percent=True),lambda x:x['alerts'][0].update(owner={'bad':1})]:
   x=copy.deepcopy(self.config);mutate(x)
   with self.assertRaises((c.Invalid,KeyError)):c.validate(x)
 def test_million_distinct_pool_labels_constant_cardinality(self):
  series=set();drops=0
  for i in range(1000000):
   labels,n=c.safe_labels({'service':'gateway','route':'browse','pool':f'0x{i:040x}','wallet':f'0x{i:040x}','raw_url':f'/pool/{i}'},self.config);series.add(tuple(sorted(labels.items())));drops+=n
  self.assertEqual(len(series),1);self.assertEqual(drops,3000000)
 def test_million_read_denominator_independent_reconstruction(self):
  totals={'eligible':0,'good':0,'fast150':0,'fast400':0}
  # Deterministic arithmetic workload only: no real access-log or throughput claim.
  for i in range(1000000):
   for k,v in c.eligible_read(503 if i%1000==0 else 200,.9 if i%100==0 else .1).items():totals[k]+=v
  self.assertEqual(totals,dict(eligible=1000000,good=999000,fast150=990000,fast400=990000))
  self.assertFalse(c.eligible_read(200,.1,'synthetic')['eligible'])
 def test_failed_commit_never_successful(self):
  for mask in range(16):self.assertEqual(c.mutation_good(*[bool(mask&(1<<i)) for i in range(4)]),mask==15)
 def test_burn_multiwindow_denominator_guard(self):
  w={k:dict(total=100000,bad=720) for k in ['5m','1h','30m','6h']}
  self.assertEqual(c.burn(w,.9995),'critical');w['1h']['bad']=0
  self.assertEqual(c.burn(w,.9995),'warning');w['6h']['bad']=0
  self.assertEqual(c.burn(w,.9995),'healthy');w['5m']['total']=10;w['5m']['bad']=1
  self.assertEqual(c.burn(w,.9995),'insufficient_traffic')
 def test_privacy_marker_absent_and_arbitrary_values_dropped(self):
  secret='DO_NOT_EXPORT_UNIQUE_SECRET'
  record=dict(authorization=secret,body=secret,signature=secret,stream_key=secret,service='gateway',error_code='timeout',zone=secret,stage=secret)
  self.assertNotIn(secret,json.dumps(c.sanitize_record(record)))
  self.assertEqual(c.safe_labels({'error_code':secret,'route':'/pool/0x123','service':'gateway'},self.config),({'service':'gateway'},2))
 def test_trace_interval_clock_skew_and_900ms_dependency(self):
  v=c.delivery_intervals(1000,1400,1500,2200)
  self.assertEqual(sum(v['seconds'].values()),v['total_seconds']);self.assertFalse(v['ingest_violation']);self.assertFalse(v['delivery_violation'])
  self.assertTrue(c.delivery_intervals(1000,1900,2000,3001)['ingest_violation'])
  with self.assertRaises(c.Invalid):c.delivery_intervals(1000,900,1500,2000)
 def test_state_activation_requires_validated_revision_actor(self):
  with self.assertRaises(c.Invalid):c.transition('proposed','activate','operator',1)
  with self.assertRaises(c.Invalid):c.transition('proposed','validate','operator',1)
  with self.assertRaises(c.Invalid):c.transition('validated','activate','operator',1)
 def test_capacity_evidence_rejects_empty_and_missing_costs(self):
  with self.assertRaises(c.Invalid):c.capacity_evidence({})
  e=dict(version='1.0',deployment_digest='a'*64,started_at=1,ended_at=2,resources={'cpu_cores':2,'memory_bytes':1024,'disk_bytes':4096},workload={'reads':1000,'events':1000,'peak_sockets':1,'video_viewer_seconds':1},measured={k:1 for k in ['million_reads','million_events','thousand_sockets','video_viewer_hour']},invoice_units={k:1 for k in ['baseline_reservation','burst_ceiling','storage_growth','cdn_egress']},retention={'logs_days':14,'traces_days':7,'metrics_days':14},artifacts={'telemetry.json':'b'*64})
  c.capacity_evidence(e);del e['invoice_units']['cdn_egress']
  with self.assertRaises(c.Invalid):c.capacity_evidence(e)
 def test_resources_own_every_alert_and_dashboard(self):
  rules=json.loads((P/'prometheus-rules.json').read_text())['groups'][0]['rules'];alerts=[r for r in rules if 'alert' in r]
  self.assertEqual({a['alert'] for a in alerts},{a['name'] for a in self.config['alerts']})
  self.assertGreater(len(json.loads((P/'grafana-dashboard.json').read_text())['panels']),len(alerts))
  for a in alerts:self.assertTrue((P/a['annotations']['runbook_url']).is_file())
if __name__=='__main__':unittest.main(verbosity=2)
