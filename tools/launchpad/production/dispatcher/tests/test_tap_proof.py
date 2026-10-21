import json,pathlib,shutil,subprocess,sys,tempfile,unittest
PRODUCTION=pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0,str(PRODUCTION))
from dispatcher import evidence
from dispatcher.workspace import run_process

class TapProof(unittest.TestCase):
 def native(self,body):
  with tempfile.TemporaryDirectory(prefix='tap-native-proof-') as directory:
   path=pathlib.Path(directory)/'actual.test.cjs';path.write_text("const {test}=require('node:test');const assert=require('node:assert/strict');\n"+body)
   result=run_process(['node','--test','--test-reporter=tap',str(path)],directory,10)
   receipt={'stdout':result['stdout'],'stderr':result['stderr'],'exit_code':result['exit_code'],'timed_out':result['timed_out']}
   return receipt
 def valid(self,text=None):
  return {'stdout':text or 'TAP version 13\nok 1 - actual assertion\n1..1\n# tests 1\n# suites 0\n# pass 1\n# fail 0\n# cancelled 0\n# skipped 0\n# todo 0\n','stderr':'','exit_code':0,'timed_out':False}
 def test_real_node_all_pass_zero_todo_summary(self):
  receipt=self.native("test('actual addition',()=>assert.equal(2+2,4));")
  self.assertEqual(receipt['exit_code'],0);self.assertIn('# todo 0',receipt['stdout']);self.assertEqual(evidence._proof(receipt,'node-tap')['positive_count'],1)
 def test_real_node_skip_rejected(self):
  receipt=self.native("test('actual addition',()=>assert.equal(2+2,4));test.skip('deferred',()=>{});")
  self.assertEqual(receipt['exit_code'],0);self.assertIn('# skipped 1',receipt['stdout'])
  with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
 def test_real_node_todo_rejected(self):
  receipt=self.native("test('actual addition',()=>assert.equal(2+2,4));test.todo('deferred');")
  self.assertEqual(receipt['exit_code'],0);self.assertIn('# todo 1',receipt['stdout'])
  with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
 def test_real_node_cancel_rejected(self):
  receipt=self.native("test('actual addition',()=>assert.equal(2+2,4));test('bounded cancellation',{timeout:20},()=>new Promise(resolve=>setTimeout(resolve,100)));")
  self.assertNotEqual(receipt['exit_code'],0);self.assertIn('# cancelled 1',receipt['stdout'])
  with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
  receipt['exit_code']=0 # A forged exit status cannot hide the actual cancellation summary.
  with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
 def test_real_node_failure_rejected(self):
  receipt=self.native("test('actual failure',()=>assert.equal(2+2,5));")
  self.assertNotEqual(receipt['exit_code'],0)
  with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
 def test_spoofed_zero_summary_cannot_hide_directive(self):
  for line in ['ok 2 - skipped # SKIP reason','ok 2 - future # TODO 0','    ok 2 - nested # todo pending','not ok 2 - failure # TODO pending','    not ok 2 - nested failure']:
   with self.subTest(line=line),self.assertRaises(evidence.EvidenceError):evidence._proof(self.valid(line+'\n'+self.valid()['stdout']),'node-tap')
 def test_contradictory_and_duplicate_summaries_rejected(self):
  for suffix in ['# pass 1\n','# fail 0\n','# skipped 1\n','# todo 1\n','# cancelled 1\n']:
   with self.subTest(suffix=suffix),self.assertRaises(evidence.EvidenceError):evidence._proof(self.valid(self.valid()['stdout']+suffix),'node-tap')
 def test_no_positive_missing_summary_missing_named_test_rejected(self):
  for text in ['TAP version 13\n1..0\n# pass 0\n# fail 0\n# todo 0\n','ok 1 - assertion\n# todo 0\n','# pass 1\n# fail 0\n# todo 0\n']:
   with self.subTest(text=text),self.assertRaises(evidence.EvidenceError):evidence._proof(self.valid(text),'node-tap')
 def test_real_node_nested_suite_all_pass(self):
  receipt=self.native("const {describe,it}=require('node:test');describe('suite',()=>{it('child1',()=>assert.equal(2+2,4));it('child2',()=>assert.equal(3+3,6));});")
  self.assertEqual(receipt['exit_code'],0);self.assertEqual(evidence._proof(receipt,'node-tap')['positive_count'],2)
 def test_truncated_plan_and_spoofed_totals_rejected(self):
  for text in ['ok 1 - fake\n# pass 1\n# fail 0\n',self.valid()['stdout'].replace('1..1\n',''),self.valid()['stdout'].replace('# tests 1','# tests 2'),self.valid()['stdout'].replace('ok 1 -','ok 2 -'),self.valid()['stdout']+'1..1\n']:
   with self.subTest(text=text),self.assertRaises(evidence.EvidenceError):evidence._proof(self.valid(text),'node-tap')
 def test_timeout_and_nonzero_exit_rejected(self):
  for change in [{'timed_out':True},{'exit_code':1}]:
   receipt=self.valid();receipt.update(change)
   with self.assertRaises(evidence.EvidenceError):evidence._proof(receipt,'node-tap')
 def test_comment_and_escaped_hash_are_not_test_directives(self):
  receipt=self.valid(self.valid()['stdout'].replace('actual assertion','title with \\# TODO escaped')+'# TODO diagnostic comment\n# SKIP diagnostic comment\n')
  self.assertEqual(evidence._proof(receipt,'node-tap')['positive_count'],1)

if __name__=='__main__':unittest.main()
