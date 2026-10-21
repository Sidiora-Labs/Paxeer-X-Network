import copy,importlib.util,json,pathlib,subprocess,tempfile,unittest
spec=importlib.util.spec_from_file_location('codegen',pathlib.Path(__file__).with_name('codegen.py'));cg=importlib.util.module_from_spec(spec);spec.loader.exec_module(cg)
DOC={'$schema':'https://json-schema.org/draft/2020-12/schema','$defs':{'Amount':{'type':'string','pattern':'^(0|[1-9][0-9]*)$'},'Payload':{'type':'object','additionalProperties':False,'properties':{'amount':{'$ref':'#/$defs/Amount'},'note':{'type':['string','null']}},'required':['amount','note']},'Proof':{'oneOf':[{'type':'object','additionalProperties':False,'properties':{'kind':{'const':'eoa','type':'string'},'signature':{'type':'string','minLength':132,'maxLength':132}},'required':['kind','signature']},{'type':'object','additionalProperties':False,'properties':{'kind':{'const':'contract','type':'string'},'signature':{'type':'string','maxLength':8194}},'required':['kind','signature']}]}}}
class CodegenTests(unittest.TestCase):
 def test_deterministic_and_concrete(self):
  a=cg.Generator(copy.deepcopy(DOC)).emit();b=cg.Generator(copy.deepcopy(DOC)).emit();self.assertEqual(a,b)
  self.assertNotIn('interface{}',a['sdk/launchpad/shared/interfaces/v1/types.gen.go']);self.assertNotIn(' any',a['sdk/launchpad/packages/production-contracts/src/types.gen.ts']);self.assertIn('Variant1',a['sdk/launchpad/shared/interfaces/v1/types.gen.go']);self.assertIn(' | null',a['sdk/launchpad/packages/production-contracts/src/types.gen.ts'])
 def test_check_never_writes_and_detects_drift(self):
  with tempfile.TemporaryDirectory() as directory:
   root=pathlib.Path(directory);source=root/'launchpad/spec/production-system/integration/contracts/v1/schema-definitions.json';source.parent.mkdir(parents=True);source.write_text(json.dumps(DOC));self.assertEqual(cg.main(['--root',str(root)]),0);before={p:p.read_bytes() for p in root.rglob('*') if p.is_file()};self.assertEqual(cg.main(['--root',str(root),'--check']),0);self.assertEqual(before,{p:p.read_bytes() for p in root.rglob('*') if p.is_file()});target=root/'shared/interfaces/v1/types.gen.go';target.write_text('stale');self.assertEqual(cg.main(['--root',str(root),'--check']),1);self.assertEqual(target.read_text(),'stale')
 def test_unresolved_or_generic_or_unsupported_fails(self):
  for invalid in ({'$ref':'#/$defs/Missing'},{'type':'object','additionalProperties':True},{'type':'string','pattern':'^(?=a)a$'},{'type':'array'},{'type':'string','inventedConstraint':1}):
   with self.subTest(invalid=invalid),self.assertRaises(cg.GenerationError):cg.validate_schema(invalid,DOC['$defs'],'test')
 def test_absent_schema_fails(self):
  with tempfile.TemporaryDirectory() as d:self.assertEqual(cg.main(['--root',d,'--check']),1)
if __name__=='__main__':unittest.main()
