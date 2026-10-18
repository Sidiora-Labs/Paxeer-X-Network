import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from dispatcher.graph import Graph, GraphError
from dispatcher.qualification_fixture import normalize_fixture

class FixtureTests(unittest.TestCase):
 def test_live_status_variants_are_private_and_projection_consistent(self):
  root=Path(os.environ.get('PRODUCTION_DISPATCHER_SPEC_ROOT','/root/mk-falcon/KindleLaunch'))
  original=(root/'spec/production-system/spec.kvx').read_text()
  original_plan=json.loads((root/'spec/production-system/execution-plan.json').read_text())
  import re
  for status in ('pending','in_progress','done'):
   with self.subTest(status=status),tempfile.TemporaryDirectory() as tmp:
    text=re.sub(r'(\[task\.1\.01\]\n.*?^status = )"[^"]+"',lambda m:m[1]+'"'+status+'"',original,flags=re.M|re.S)
    plan=copy.deepcopy(original_plan);plan['tasks']['1.01']['status']=status
    saved=copy.deepcopy(plan); normalized,fixture=normalize_fixture(text,plan)
    self.assertEqual(plan,saved)
    path=Path(tmp);(path/'spec.kvx').write_text(normalized);(path/'plan.json').write_text(json.dumps(fixture))
    graph=Graph(path/'spec.kvx',path/'plan.json');self.assertEqual(len(graph.ready()),50)
    self.assertEqual(graph.tasks['1.01']['status'],'pending')
    blocked={i:t['status'] for i,t in graph.tasks.items()};blocked['0.3']='in_progress';self.assertEqual(graph.ready(blocked),[])
    self.assertTrue(all(t['status']=='pending' for lane in fixture['lanes'] for t in lane['tasks']))
    # Current status projection validation remains enforced by Graph.
    fixture['tasks']['1.01']['status']='done';(path/'plan.json').write_text(json.dumps(fixture))
    with self.assertRaises(GraphError):Graph(path/'spec.kvx',path/'plan.json')
  self.assertEqual((root/'spec/production-system/spec.kvx').read_text(),original)
