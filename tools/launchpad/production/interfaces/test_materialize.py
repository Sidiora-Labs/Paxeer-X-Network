import copy,importlib.util,json,pathlib,tomllib,unittest
from jsonschema import Draft202012Validator,FormatChecker
HERE=pathlib.Path(__file__).resolve().parent
loader=importlib.util.spec_from_file_location('materialize',HERE/'materialize.py');m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
ROOT=HERE.parents[3];OUT=ROOT/'launchpad/spec/production-system/integration/contracts/v1'
class MaterializeTests(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.spec=tomllib.load(open(ROOT/'launchpad/spec/production-system/spec.kvx','rb'));cls.supp=json.load(open(OUT/'inputs/supplementary.json'));cls.files=m.generate(cls.spec,cls.supp);cls.doc=json.loads(cls.files['schema-definitions.json']);cls.api=json.loads(cls.files['openapi.json']);cls.asyncapi=json.loads(cls.files['asyncapi.json']);cls.map=json.loads(cls.files['catalog-map.json'])
 def valid(self,name,value):
  doc={'$schema':m.DIALECT,'$ref':'#/$defs/'+name,'$defs':self.doc['$defs']};return Draft202012Validator(doc,format_checker=FormatChecker()).is_valid(value)
 def test_all_catalog_entries_mapped(self):
  self.assertEqual((len(self.map['schemas']),len(self.map['endpoints']),len(self.map['streams']),len(self.map['errors'])),(59,108,12,16))
  for id,e in self.map['endpoints'].items():
   for field in ['request','data','response']:self.assertIn(e[field],self.doc['$defs'],id)
 def test_regeneration_deterministic(self):self.assertEqual(self.files,m.generate(self.spec,self.supp))
 def test_all_local_refs_resolve(self):
  for name in ['schema-definitions.json','openapi.json','asyncapi.json']:m.validate_refs(json.loads(self.files[name]))
 def test_uint256_exact_boundary(self):
  for v in ['0',str(2**256-1)]:self.assertTrue(self.valid('UInt256',v))
  for v in [str(2**256),'01','-0','+1','1e18',' 1','1.0',1]:self.assertFalse(self.valid('UInt256',v),str(v))
 def test_aggregate_and_price_have_distinct_caps(self):
  self.assertTrue(self.valid('AggregateUInt','9'*156));self.assertFalse(self.valid('AggregateUInt','9'*157));self.assertTrue(self.valid('PriceComponent','9'*512));self.assertFalse(self.valid('PriceComponent','9'*513));self.assertFalse(self.valid('PositivePriceComponent','0'))
 def test_unknown_type_fails(self):
  spec=copy.deepcopy(self.spec);spec['schema']['IF_POOL']['fields']+='; invented:InventedAnyThing'
  with self.assertRaisesRegex(m.ContractError,'Unknown field type'):m.generate(spec,self.supp)
 def test_duplicate_fields_fail(self):
  spec=copy.deepcopy(self.spec);spec['schema']['IF_POOL']['fields']+='; poolAddress:hex20'
  with self.assertRaisesRegex(m.ContractError,'Duplicate field'):m.generate(spec,self.supp)
 def test_generic_type_fails(self):
  spec=copy.deepcopy(self.spec);spec['schema']['IF_POOL']['fields']+='; invented:object'
  with self.assertRaisesRegex(m.ContractError,'Unresolved generic'):m.generate(spec,self.supp)
 def test_recursive_strict_wallet_proof_and_real_union(self):
  a='0x'+'11'*20
  eoa={'kind':'eoa_eip191','wallet':a,'signature':'0x'+'22'*65};erc={'kind':'erc1271','wallet':a,'signature':'0x1234'}
  self.assertTrue(self.valid('WalletProof',eoa));self.assertTrue(self.valid('WalletProof',erc))
  eoa['signature']='0x12';self.assertFalse(self.valid('WalletProof',eoa))
  request={'challengeId':'00000000-0000-4000-8000-000000000001','proof':erc,'deviceLabel':'test'}
  self.assertTrue(self.valid('LoginBody',request));request['proof']['unexpected']='bad';self.assertFalse(self.valid('LoginBody',request))
 def test_required_nullable_envelope_and_additive_output(self):
  env={'requestId':'00000000-0000-4000-8000-000000000001','data':{'items':[]},'watermark':None,'revision':None,'quality':'complete','nextCursor':None,'generatedAt':'2026-10-09T00:00:00.000Z'}
  self.assertTrue(self.valid('PoolListResponse',env));env['newOptional']='future';self.assertTrue(self.valid('PoolListResponse',env))
  del env['watermark'];self.assertFalse(self.valid('PoolListResponse',env))
 def test_cookie_header_and_sse_contracts(self):
  refresh=self.api['paths']['/v1/auth/sessions:refresh']['post'];self.assertNotIn('requestBody',refresh);self.assertIn(('sidiora_refresh','cookie'),[(x['name'],x['in']) for x in refresh['parameters']])
  deletion=self.api['paths']['/v1/me/deletion-requests/{jobId}']['get'];self.assertIn(('X-Recovery-Receipt','header'),[(x['name'],x['in']) for x in deletion['parameters']]);self.assertNotIn('requestBody',deletion)
  sse=next(v for k,v in self.map['endpoints'].items() if k=='IF_EVENTS_SSE');op=self.api['paths'][sse['path']]['get'];self.assertIn('text/event-stream',op['responses']['200']['content']);self.assertEqual(sse['wireMode'],'sse')
 def test_subscribe_rejects_unknown_and_openapi_parameters_match_strict_request(self):
  frame={'op':'subscribe','subscriptionId':'s1','topics':['market.price.0x'+'11'*20],'afterCursor':None}
  self.assertTrue(self.valid('Subscribe',frame));frame['unknown']=True;self.assertFalse(self.valid('Subscribe',frame))
  for id,entry in self.map['endpoints'].items():
   operation=self.api['paths'][entry['path']][entry['method'].lower()]
   request=self.doc['$defs'][entry['request']]
   for parameter in operation['parameters']:
    if parameter['name'] in request.get('properties',{}):
     self.assertEqual(parameter['schema'],m.replace_refs(request['properties'][parameter['name']],'#/components/schemas/'),id)
 def test_semantic_identity_ignores_task_lifecycle_but_detects_norms(self):
  changed=copy.deepcopy(self.spec);changed.setdefault('task',{}).setdefault('0.2',{})['status']='done';changed.setdefault('meta',{})['status']='different'
  self.assertEqual(m.semantic_digest(self.spec),m.semantic_digest(changed));self.assertEqual(self.files,m.generate(changed,self.supp))
  changed['schema']['IF_POOL']['status']='implemented';self.assertEqual(m.semantic_digest(self.spec),m.semantic_digest(changed));self.assertEqual(self.files,m.generate(changed,self.supp))
  changed['schema']['IF_CONVENTIONS']['time']+=' changed normative';self.assertNotEqual(m.semantic_digest(self.spec),m.semantic_digest(changed))
 def test_utc_clock_range_and_calendar(self):
  field=self.doc['$defs']['Session']['properties']['accessExpiresAt']
  checker=Draft202012Validator(field,format_checker=FormatChecker())
  for value in ['2026-10-09T23:59:59.000Z','0001-01-01T00:00:00.000Z','9999-12-31T23:59:59.999Z','2000-02-29T00:00:00.000Z','2024-02-29T00:00:00.000Z']:self.assertTrue(checker.is_valid(value),value)
  for value in ['2026-12-31T23:59:60.000Z','2026-02-30T00:00:00.000Z','0000-01-01T00:00:00.000Z','1900-02-29T00:00:00.000Z','2100-02-29T00:00:00.000Z','2026-04-31T00:00:00.000Z','2026-13-01T00:00:00.000Z','2026-10-09T24:00:00.000Z','2026-10-09T00:00:00Z']:
   self.assertFalse(checker.is_valid(value),value)
 def test_https_origin_has_authority_and_no_credentials_or_path(self):
  origin=self.doc['$defs']['ChallengeBody']['properties']['origin'];validator=Draft202012Validator(origin,format_checker=FormatChecker())
  self.assertTrue(validator.is_valid('https://example.invalid:443'))
  for value in ['https://','https:///path','https://user:password@example.invalid','https://example.invalid/path','https://example.invalid?token=x']:
   self.assertFalse(validator.is_valid(value),value)
 def test_no_unconstrained_json_schema_or_unknown_array(self):
  def walk(v):
   if isinstance(v,dict):
    if 'properties' in v:
     for shape in v['properties'].values():self.assertNotEqual(shape,{})
    if v.get('type')=='array':self.assertIn('items',v);self.assertNotEqual(v['items'],{})
    for value in v.values():walk(value)
   elif isinstance(v,list):
    for value in v:walk(value)
  walk(self.doc)
if __name__=='__main__':unittest.main()
