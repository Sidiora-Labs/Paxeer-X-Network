import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import time
import unittest

MODULE=Path(__file__).resolve().parents[1]/'graph.py'
spec=importlib.util.spec_from_file_location('dispatcher_graph',MODULE)
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
Graph,GraphError=module.Graph,module.GraphError
ROOT=Path('/root/mk-falcon/KindleLaunch')
SPEC=ROOT/'spec/production-system/spec.kvx'
PLAN=ROOT/'spec/production-system/execution-plan.json'

class GraphTests(unittest.TestCase):
    def tiny(self,body):
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup)
        path=Path(tmp.name)/'spec.kvx';path.write_text(body);return path
    def test_full_graph_is_bounded(self):
        start=time.monotonic();g=Graph(SPEC,PLAN)
        for _ in range(100):g.status();g.ready()
        self.assertLess(time.monotonic()-start,30)
        self.assertEqual(len(g.tasks),208)
        self.assertEqual(len(g.layout_digest),64)
    def test_fifty_ready_overlay_is_isolated(self):
        g=Graph(SPEC,PLAN);before=g.status()
        from dispatcher.qualification_fixture import initial_states
        ready=g.ready(initial_states(g.tasks))
        self.assertEqual(len(ready),50)
        self.assertTrue(all(g.tasks[x]['wave']==1 for x in ready))
        self.assertEqual(before,g.status())
    def test_only_done_dependency_qualifies(self):
        g=Graph(SPEC)
        for state in ('pending','in_progress','implemented','blocked','failed','paused'):
            self.assertFalse(any(g.tasks[x]['wave']==1 for x in g.ready({'0.1':'done','0.2':'done','0.3':state})))
    def test_implemented_peers_do_not_open_gate(self):
        g=Graph(SPEC,PLAN)
        overlay={'0.1':'done','0.2':'done','0.3':'done'}
        overlay.update({k:'implemented' for k,t in g.tasks.items() if t['wave']==1})
        self.assertEqual(g.ready(overlay),[])
        overlay.update({k:'done' for k,t in g.tasks.items() if t['wave']==1})
        self.assertTrue(g.ready(overlay))
        self.assertTrue(all(g.tasks[k]['wave']==2 for k in g.ready(overlay)))
    def test_unknown_overlay_rejected(self):
        g=Graph(SPEC)
        for overlay in ({'bogus':'done'},{'0.3':'success'},[]):
            with self.assertRaises(GraphError):g.ready(overlay)
    def test_lowest_wave(self):
        p=self.tiny('[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=[]\n[task.1.1]\ntitle="b"\nwave=1\nstatus="pending"\nrequires=[]\n')
        g=Graph(p);self.assertEqual(g.ready(),['0.1']);self.assertEqual(g.ready({'0.1':'done'}),['1.1'])
    def test_unknown_dependency(self):
        p=self.tiny('[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=["0.2"]\n')
        with self.assertRaisesRegex(GraphError,'unknown dependency'):Graph(p)
    def test_cycle(self):
        p=self.tiny('[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=["0.2"]\n[task.0.2]\ntitle="b"\nwave=0\nstatus="pending"\nrequires=["0.1"]\n')
        with self.assertRaisesRegex(GraphError,'cycle'):Graph(p)
    def test_bad_toml(self):
        p=self.tiny('[task.0.1]\nwave=0\nwave=1\n')
        with self.assertRaises(GraphError):Graph(p)
    def test_future_wave_dependency(self):
        p=self.tiny('[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=["1.1"]\n[task.1.1]\ntitle="b"\nwave=1\nstatus="pending"\nrequires=[]\n')
        with self.assertRaisesRegex(GraphError,'wave violation'):Graph(p)
    def test_projection_hash_and_definition(self):
        with tempfile.TemporaryDirectory() as tmp:
            p=Path(tmp)/'plan.json';q=json.loads(PLAN.read_text());q['spec_sha256']='0'*64;p.write_text(json.dumps(q))
            with self.assertRaises(GraphError):Graph(SPEC,p)
            q=json.loads(PLAN.read_text());q['tasks']['0.1']['title']='bad';p.write_text(json.dumps(q))
            with self.assertRaises(GraphError):Graph(SPEC,p)
    def test_ownership_overlap(self):
        p=self.tiny('[lane.L01]\nwrite_paths=["a/**"]\n[lane.L02]\nwrite_paths=["a/b/**"]\n[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=[]\n')
        with self.assertRaisesRegex(GraphError,'overlap'):Graph(p)
    def test_unowned_touch(self):
        p=self.tiny('[lane.L01]\nwrite_paths=["a/**"]\n[task.1.01]\ntitle="a"\nwave=1\nphase=1\nkind="parallel_lane"\nlane="L01"\nstatus="pending"\nrequires=[]\ntouches=["b/x"]\n')
        with self.assertRaisesRegex(GraphError,'unowned'):Graph(p)
    def test_reserved_ownership(self):
        p=self.tiny('[execution.reserved]\npaths=["go.work"]\n[lane.L01]\nwrite_paths=["go.work"]\n[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=[]\n')
        with self.assertRaisesRegex(GraphError,'reserved'):Graph(p)
    def test_traversal_ownership(self):
        p=self.tiny('[lane.L01]\nwrite_paths=["../a/**"]\n[task.0.1]\ntitle="a"\nwave=0\nstatus="pending"\nrequires=[]\n')
        with self.assertRaises(GraphError):Graph(p)

if __name__=='__main__':unittest.main()
