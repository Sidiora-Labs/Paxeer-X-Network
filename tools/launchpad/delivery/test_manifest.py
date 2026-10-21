import copy
import json
import unittest
import subprocess
import sys
from pathlib import Path
from validate_manifest import validate


class ManifestTest(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads(Path(__file__).with_name('stream-manifest.json').read_text())

    def test_manifest_constraints(self):
        self.assertEqual(validate(self.manifest)['live_three_az_operation'], 'not_tested')

    def test_invalid_retention_replication_and_zone_placement_rejected(self):
        for mutate in [
            lambda m: m['stream'].update(max_age=3600 * 10**9),
            lambda m: m['stream'].update(num_replicas=1),
            lambda m: m['stream'].update(discard='old'),
            lambda m: m['server_requirements']['replicas'][2].update(tags=['az:a', 'workload:durable-events']),
            lambda m: m['stream'].update(max_bytes=1000),
        ]:
            m = copy.deepcopy(self.manifest)
            mutate(m)
            with self.assertRaises(ValueError):
                validate(m)

    def test_optimized_python_still_rejects_invalid_manifest(self):
        manifest=copy.deepcopy(self.manifest)
        manifest['stream']['num_replicas']=1
        code="import json,sys; from validate_manifest import validate; validate(json.load(sys.stdin))"
        result=subprocess.run([sys.executable,'-O','-c',code],input=json.dumps(manifest),text=True,capture_output=True,cwd=Path(__file__).parent,timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('ValueError',result.stderr)


if __name__ == '__main__':
    unittest.main()
