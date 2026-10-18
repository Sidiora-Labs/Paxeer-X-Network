import copy
import importlib.util
import json
import hashlib
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('kindle_topology',ROOT/'topology.py')
t=importlib.util.module_from_spec(spec);spec.loader.exec_module(t)

def config():return t.load(ROOT/'fixtures/synthetic-config.json')
def evidence(c,p):
    end=datetime(2026,10,10,0,0,tzinfo=timezone.utc);start=end-timedelta(days=3)
    e={'schemaVersion':1,'classification':'production_shaped_measured','configurationDigest':p['configurationDigest'],'deploymentDigest':p['deploymentDigest'],'sourceDigest':p['sourceDigest'],'interfaceDigest':p['interfaceDigest'],'attestationDigest':'a'*64,'inventoryDigest':'b'*64,'startedAt':start.isoformat(),'finishedAt':end.isoformat(),'scenarios':[{'requirement':r,'passed':True,'startedAt':start.isoformat(),'finishedAt':end.isoformat(),'inputDigest':'1'*64,'artifactDigest':'2'*64,'reconciliationDigest':'3'*64,'metric':t.METRICS[r],'sampleCount':2} for r in t.REQS],'measurements':{'apiRpsPerReplicaZoneLoss':625,'socketConnectionsPerReplicaZoneLoss':5000,'peakDynamicRps':10000,'burstDynamicRps':20000,'peakConnectedClients':100000,'surgeConnectedClients':200000,'framePayloadBytesPerSecond':100000000,'burstFramePayloadBytesPerSecond':400000000,'postgresApplicationConnections':480,'activePostgresWriters':1,'duplicateDatabaseEffects':0,'lostAcknowledgedWrites':0,'ingestP99Ms':500,'drainSeconds':30,'probeTimeoutMs':2000,'recoverySeconds':900,'mediaCdnBitsPerSecond':40000000000,'apiMediaPayloadBytes':0,'brokerOverheadPermille':100,'postgresDerivedBytesPerMember':200*1024**3,'walBytesPerMember':128*1024**3,'vacuumBytesPerMember':128*1024**3,'soakSeconds':86400,'replayRetentionSeconds':259200},'measuredStreams':[dict(x,retainedLogicalBytes=x['qualificationEventsPerSecond']*259200*x['envelopeBytes']) for x in c['streams']]}
    return e,end

class TopologyTests(unittest.TestCase):
    def test_exact_integer_workload_storage_ledgers(self):
        x=t.capacity(config());n=x['broker']['normal'];q=x['broker']['qualification'];pg=x['postgres']
        self.assertEqual((n['rawLogicalBytes'],n['rawR3Bytes']),(36000000000,108000000000))
        self.assertEqual((q['rawLogicalBytes'],q['rawR3Bytes'],q['rawR3HeadroomBytes']),(622080000000,1866240000000,3732480000000))
        self.assertEqual(x['peak24hRawBytes'],207360000000)
        self.assertEqual((pg['thirtyDayRawBytes'],pg['rowIndexAmplifiedBytesPerMember'],pg['rowIndexAmplifiedBytesThreeCopies']),(555360000000,1666080000000,4998240000000))
        self.assertEqual(x['dynamicRequestsPerDay'],50000000)
        self.assertEqual(x['averageConcurrentSessions'],{'numerator':31250,'denominator':3,'decimalRounded2':'10416.67'})
        self.assertEqual(q['derivedLogicalBytes'],207360000000)
        self.assertEqual(q['requiredBytesWithTwofoldHeadroom'],5474304000000)
        self.assertEqual(q['provisionedBytes'],(2*1024**4*9//10)*3)
    def test_postgres_provisional_2tib_rejected_with_all_workspace(self):
        c=config();c['storage']['postgresBytesPerMember']=2*t.TIB
        with self.assertRaisesRegex(t.Invalid,'PostgreSQL storage requires expansion'):t.validate(c)
    def test_broker_effective_90_percent_allocation_rejects_near_boundary(self):
        c=config();c['storage']['brokerOverheadPermille']=250
        # 6.2208TB fits full6.597TB PVCs but not5.937TB configured broker limit.
        with self.assertRaisesRegex(t.Invalid,'broker storage exceeds'):t.validate(c)
    def test_missing_provider_inputs_reject_template(self):
        with self.assertRaises(t.Invalid):t.validate(t.load(ROOT/'production-input.template.json'))
    def test_closed_fields_required_and_unknown(self):
        c=config()
        for key in list(c):
            with self.subTest(missing=key):
                x=copy.deepcopy(c);del x[key]
                with self.assertRaises(t.Invalid):t.validate(x)
        c['ignoredPrivilege']=True
        with self.assertRaises(t.Invalid):t.validate(c)
    def test_bools_floats_and_unbounded_fields_rejected(self):
        for group,key in [('connections','poolerReplicas'),('admission','totalRps'),('admission','quoteRps'),('admission','socketConnectionsPerReplica'),('release','canaryPercent'),('release','canarySeconds'),('storage','brokerOverheadPermille')]:
            for v in [True,float(config()[group][key]),-1,2**64]:
                with self.subTest(group=group,key=key,value=v):
                    c=config();c[group][key]=v
                    with self.assertRaises(t.Invalid):t.validate(c)
    def test_distinct_zones_streams_and_principals(self):
        cases=[]
        c=config();c['zones'][2]=c['zones'][1];cases.append(c)
        c=config();c['streams'].append(copy.deepcopy(c['streams'][0]));cases.append(c)
        c=config();c['streams'][0]['name']='raw-chain';cases.append(c)
        c=config();c['secretRefs']['runtime']['api']=c['secretRefs']['runtime']['indexer'];cases.append(c)
        c=config();c['secretRefs']['runtime']['api']=c['secretRefs']['postgresBootstrap'];cases.append(c)
        for c in cases:
            with self.assertRaises(t.Invalid):t.validate(c)
    def test_mutable_images_credentials_rpc_and_pool_exhaustion_rejected(self):
        cases=[]
        c=config();c['images']['api']='example.invalid/api:latest';cases.append(c)
        c=config();c['rpcEndpoints'][0]='https://user:password@rpc.example.invalid';cases.append(c)
        c=config();c['rpcEndpoints'][0]='https://rpc.example.invalid/?apiKey=secret';cases.append(c)
        c=config();c['connections']['otherDirectConnections']=181;cases.append(c)
        c=config();c['connections']['serverConnectionsPerPooler']=141;cases.append(c)
        for c in cases:
            with self.assertRaises(t.Invalid):t.validate(c)
    def test_duplicate_json_oversized_and_nonfinite_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'c.json'
            for payload in ['{"revision":1,"revision":2}','{"x":NaN}',' '*((2*1024*1024)+1)]:
                p.write_text(payload)
                with self.assertRaises(t.Invalid):t.load(p)
    def test_real_cli_render_and_exact_plan_tamper_checks(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'plan.json'
            base=[sys.executable,str(ROOT/'topology.py')]
            result=subprocess.run(base+['render','--config',str(ROOT/'fixtures/synthetic-config.json'),'--output',str(p)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            plan=t.load(p);self.assertEqual(plan,t.compile_plan(config()))
            result=subprocess.run(base+['verify-plan','--config',str(ROOT/'fixtures/synthetic-config.json'),'--plan',str(p)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            svc=next(x for x in plan['resources']['items'] if x['kind']=='Service' and x['metadata']['name']=='nats');svc['spec']['type']='LoadBalancer'
            plan['deploymentDigest']=t.digest(plan['resources']);p.write_text(json.dumps(plan))
            result=subprocess.run(base+['verify-plan','--config',str(ROOT/'fixtures/synthetic-config.json'),'--plan',str(p)],capture_output=True,text=True)
            self.assertEqual(result.returncode,1);self.assertIn('public service forbidden',result.stdout)
    def test_rendered_nested_drift_cannot_be_hidden_by_rehash(self):
        p=t.compile_plan(config());dep=next(x for x in p['resources']['items'] if x['kind']=='Deployment');dep['spec']['replicas']=1;p['deploymentDigest']=t.digest(p['resources'])
        with self.assertRaisesRegex(t.Invalid,'differs from reviewed'):t.assert_exact_plan(p,config())
    def test_declarations_zone_replicas_security_and_shutdown(self):
        p=t.compile_plan(config());items=p['resources']['items'];deps=[x for x in items if x['kind']=='Deployment'];self.assertEqual(len(deps),7)
        for d in deps:
            n=d['metadata']['name'];self.assertEqual(d['spec']['replicas'],t.SERVICES[n][0]);pod=d['spec']['template']['spec'];self.assertEqual(pod['terminationGracePeriodSeconds'],45)
            self.assertEqual(pod['topologySpreadConstraints'][0]['minDomains'],3);self.assertEqual(pod['containers'][0]['readinessProbe']['timeoutSeconds'],2)
        pg=next(x for x in items if x['kind']=='Cluster');self.assertEqual(pg['spec']['instances'],3);self.assertEqual(pg['spec']['postgresql']['synchronous'],{'method':'any','number':1,'dataDurability':'required','failoverQuorum':True})
        self.assertEqual(len(p['requirements']),18)
    def test_explicit_jetstream_stream_replication_retention_and_discard(self):
        p=t.compile_plan(config());cm=next(x for x in p['resources']['items'] if x['kind']=='ConfigMap' and x['metadata']['name']=='jetstream-stream-contracts');streams=json.loads(cm['data']['streams.json'])
        self.assertEqual(len(streams),2)
        for s in streams:self.assertEqual((s['num_replicas'],s['storage'],s['max_age'],s['discard']),(3,'file',259200*10**9,'new'))
        self.assertEqual(streams[0]['max_bytes'],1368576000000)
    def test_official_pinned_cnpg_crd_structural_validation(self):
        # This is genuine offline validation against checked-in upstream schemas,
        # not API admission/controller/failover evidence.
        import yaml,jsonschema
        p=t.compile_plan(config());provenance=t.load(ROOT/'fixtures/crd-provenance.json')
        for v,kind in zip(provenance,['Cluster','Pooler']):
            data=(ROOT/v['path']).read_bytes();self.assertEqual(hashlib.sha256(data).hexdigest(),v['sha256'])
            crd=yaml.safe_load(data);schema=next(x for x in crd['spec']['versions'] if x['name']=='v1')['schema']['openAPIV3Schema']
            resource=next(x for x in p['resources']['items'] if x['kind']==kind)
            jsonschema.Draft7Validator(schema).validate(resource)
            # Detect unknown fields that a permissive OpenAPI JSON validator would omit.
            def closed(value,s):
                if isinstance(value,dict) and 'properties' in s:
                    extra=set(value)-set(s['properties']);self.assertFalse(extra,(kind,extra))
                    for k,v in value.items():closed(v,s['properties'][k])
                elif isinstance(value,list) and 'items' in s:
                    for v in value:closed(v,s['items'])
            closed(resource['spec'],schema['properties']['spec'])
    def test_measured_evidence_schema_requires_actual_external_attestation(self):
        c=config();p=t.compile_plan(c);e,now=evidence(c,p);out=t.validate_evidence(e,p,c,now)
        self.assertTrue(out['notDeploymentAuthorization']);self.assertEqual(out['status'],'evidence_structurally_valid_external_signature_required')
    def test_missing_changed_failed_expired_and_synthetic_evidence_rejected(self):
        c=config();p=t.compile_plan(c);e,now=evidence(c,p)
        edits=[lambda x:x.update(schemaVersion=True),lambda x:x.update(schemaVersion=1.0),lambda x:x.update(classification='synthetic'),lambda x:x.update(deploymentDigest='9'*64),lambda x:x['scenarios'].pop(),lambda x:x['scenarios'][0].update(passed=False),lambda x:x['scenarios'][0].update(sampleCount=0),lambda x:x.update(measuredStreams=[])]
        for change in edits:
            x=copy.deepcopy(e);change(x)
            with self.assertRaises(t.Invalid):t.validate_evidence(x,p,c,now)
        with self.assertRaisesRegex(t.Invalid,'expired'):t.validate_evidence(e,p,c,now+timedelta(days=2))
    def test_measured_lost_writes_duplicate_effects_dual_primary_and_soak_lie_rejected(self):
        c=config();p=t.compile_plan(c);e,now=evidence(c,p)
        for key,value in [('lostAcknowledgedWrites',1),('duplicateDatabaseEffects',1),('activePostgresWriters',2),('apiRpsPerReplicaZoneLoss',624),('apiMediaPayloadBytes',1),('brokerOverheadPermille',101),('postgresDerivedBytesPerMember',201*1024**3)]:
            with self.subTest(key=key):
                x=copy.deepcopy(e);x['measurements'][key]=value
                with self.assertRaises(t.Invalid):t.validate_evidence(x,p,c,now)
        e['startedAt']=(now-timedelta(seconds=1)).isoformat()
        for s in e['scenarios']:s['startedAt']=e['startedAt']
        with self.assertRaisesRegex(t.Invalid,'claimed soak'):t.validate_evidence(e,p,c,now)
    def test_measured_derived_budget_overrun_rejected(self):
        c=config();p=t.compile_plan(c);e,now=evidence(c,p)
        for k in ('normalEventsPerDay','qualificationEventsPerSecond','envelopeBytes','retainedLogicalBytes'):
            x=copy.deepcopy(e);x['measuredStreams'][0][k]+=1
            with self.assertRaises(t.Invalid):t.validate_evidence(x,p,c,now)
    def test_native_parser_usage_exit_zero_is_not_validity_proof(self):
        sys.path.insert(0,str(ROOT))
        import native_nats_check as native
        with self.assertRaises(t.Invalid):
            try:native.require_native_validity('Usage: nats-server [options]','flag provided but not defined: -test')
            except native.topology.Invalid as ex:raise t.Invalid(str(ex))
        native.require_native_validity('','nats-server: configuration file /config/nats.conf is valid (sha256:123)')
    def test_python_optimized_mode_still_fails_closed(self):
        r=subprocess.run([sys.executable,'-O',str(ROOT/'topology.py'),'validate','--config',str(ROOT/'production-input.template.json')],capture_output=True,text=True)
        self.assertEqual(r.returncode,1);self.assertIn('rejected',r.stdout)

if __name__=='__main__':unittest.main(verbosity=2)
