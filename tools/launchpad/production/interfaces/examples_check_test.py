"""Real JSONSchema instance and example-manifest regression checks."""
import copy
import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import tomllib
import unittest
from jsonschema import ValidationError

HERE=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('examples_check',HERE/'examples_check.py')
e=importlib.util.module_from_spec(spec);spec.loader.exec_module(e)
ROOT=HERE.parents[3]
CONTRACTS=pathlib.Path(os.environ.get('EXAMPLE_CONTRACTS',ROOT/'launchpad/spec/production-system/integration/contracts/v1'))
SPEC=pathlib.Path(os.environ.get('EXAMPLE_SPEC',ROOT/'launchpad/spec/production-system/spec.kvx'))
MATERIALIZER=pathlib.Path(os.environ.get('EXAMPLE_MATERIALIZER',HERE/'materialize.py'))

class ExamplesTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog=tomllib.loads(SPEC.read_text())
        cls.generated=e.generate(cls.catalog,CONTRACTS,e.load_materializer(MATERIALIZER))
        cls.samples=e.Samples(json.loads((CONTRACTS/'schema-definitions.json').read_bytes()))
        cls.openapi=json.loads((CONTRACTS/'openapi.json').read_bytes())
    def test_exact_108_coverage_and_proofs(self):
        self.assertEqual(len(self.generated['endpoints']),108)
        self.assertEqual(set(self.generated['endpoints']),set(self.catalog['endpoint']))
        for name in ('IF_LOGIN','IF_WALLET_LINK_VERIFY'):
            variants=self.generated['endpoints'][name]['requestVariants']
            self.assertEqual({v['request']['body']['proof']['kind'] for v in variants},{'eoa_eip191','erc1271'})
    def test_missing_and_unknown_ids_fail(self):
        for unknown in (False,True):
            bad=copy.deepcopy(self.generated)
            if unknown:bad['endpoints']['IF_UNKNOWN']=bad['endpoints']['IF_LOGIN']
            else:del bad['endpoints']['IF_LOGIN']
            with self.assertRaises(e.ExampleError):e.check(bad,self.generated)
    def test_stale_hash_and_missing_instance_fail(self):
        for field in ('inputs','endpoints'):
            bad=copy.deepcopy(self.generated)
            if field=='inputs':bad[field]['semanticCatalogSha256']='0'*64
            else:bad[field]['IF_LOGIN']['requestVariants']=[]
            with self.assertRaises(e.ExampleError):e.check(bad,self.generated)
    def test_missing_required_rejected_by_actual_schema(self):
        value=self.samples.sample({'$ref':'#/$defs/LoginBody'})
        del value['challengeId']
        with self.assertRaises(ValidationError):self.samples.validate({'$ref':'#/$defs/LoginBody'},value)
    def test_amount_number_rejected_by_actual_schema(self):
        value=self.samples.sample({'$ref':'#/$defs/QuoteRequest'})
        value['inputRaw']=9007199254740993
        with self.assertRaises(ValidationError):self.samples.validate({'$ref':'#/$defs/QuoteRequest'},value)
    def test_unknown_mutation_and_nested_proof_fields_rejected(self):
        for nested in (False,True):
            value=self.samples.sample({'$ref':'#/$defs/LoginBody'})
            (value['proof'] if nested else value)['unknownMutationField']=True
            with self.assertRaises(ValidationError):self.samples.validate({'$ref':'#/$defs/LoginBody'},value)
    def test_additive_response_accepted_by_actual_schema(self):
        value=self.samples.sample({'$ref':'#/$defs/PoolListResponse'})
        value['futureEnvelopeField']='compatible'
        value['data']['futureDomainField']='compatible'
        self.samples.validate({'$ref':'#/$defs/PoolListResponse'},value)
        e.reject_pagination_duplicate(value)
    def test_pagination_duplicate_schema_vs_semantic_distinction(self):
        value=self.samples.sample({'$ref':'#/$defs/PoolListResponse'})
        value['data']['nextCursor']='legacy.cursor'
        self.samples.validate({'$ref':'#/$defs/PoolListResponse'},value)
        with self.assertRaises(e.ExampleError):e.reject_pagination_duplicate(value)
    def test_misplaced_request_parameter_fails(self):
        endpoint=self.generated['endpoints']['IF_CANDLES']
        request=copy.deepcopy(endpoint['requestVariants'][0]['request'])
        value=request['path'].pop('poolAddress');request['query']['poolAddress']=value
        operation=self.openapi['paths'][endpoint['path']][endpoint['method'].lower()]
        with self.assertRaises(e.ExampleError):e.validate_placement(self.samples,operation,request)
    def test_sse_frames_and_empty204(self):
        seen_empty=0
        for endpoint in self.generated['endpoints'].values():
            for success in endpoint['successes']:
                if success['status']==204:
                    seen_empty+=1;self.assertIsNone(success['body']);self.assertEqual(success['bodyBytes'],0)
                if success.get('contentType')=='text/event-stream':
                    frame=success['frame'];self.assertTrue(frame.endswith('\n\n'))
                    line=next(line for line in frame.splitlines() if line.startswith('data: '))
                    self.assertEqual(json.loads(line[6:]),success['jsonData'])
        self.assertGreater(seen_empty,0)
        self.assertEqual(len(self.generated['endpoints']['IF_EVENTS_SSE']['successes']),13)
    def test_unknown_schema_or_pattern_has_no_fallback(self):
        for shape in ({},{'type':'object'},{'type':'string','pattern':'^never-generic-fallback$'}):
            with self.assertRaises(e.ExampleError):self.samples.sample(shape)
    def test_task_lifecycle_excluded_normative_change_included(self):
        materializer=e.load_materializer(MATERIALIZER)
        digest=materializer.semantic_digest(self.catalog)
        other=copy.deepcopy(self.catalog);other['task']['lifecycle_test']={'status':'done'}
        self.assertEqual(digest,materializer.semantic_digest(other))
        other['endpoint']['IF_LOGIN']['success_status']=299
        self.assertNotEqual(digest,materializer.semantic_digest(other))
    def test_check_read_only_and_rejects_mutated_bytes(self):
        with tempfile.TemporaryDirectory() as temp:
            path=pathlib.Path(temp)/'examples.json';path.write_bytes(e.encode(self.generated));before=path.read_bytes()
            cmd=[sys.executable,str(HERE/'examples_check.py'),'--check','--spec',str(SPEC),'--contracts',str(CONTRACTS),'--materializer',str(MATERIALIZER),'--examples',str(path)]
            result=subprocess.run(cmd,capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(path.read_bytes(),before)
            path.write_bytes(before.replace(b'"IF_LOGIN":',b'"IF_LOGIN_CHANGED":',1));bad=path.read_bytes()
            result=subprocess.run(cmd,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(path.read_bytes(),bad)

if __name__=='__main__':unittest.main()
