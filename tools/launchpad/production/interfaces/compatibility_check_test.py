"""Negative freeze checks against actual frozen repository files, not fake adapter proof."""
import copy
import json
import os
import tempfile
import unittest
from pathlib import Path
from compatibility_check import check

ROOT=Path(os.environ.get('COMPAT_SOURCE_ROOT','/root/mk-falcon/KindleLaunch'))
MANIFEST=Path(__file__).resolve().parents[4]/'launchpad/spec/production-system/integration/contracts/v1/compatibility.json'
SCHEMAS=os.environ.get('COMPAT_SCHEMAS')

class CompatibilityFreezeTests(unittest.TestCase):
    def setUp(self): self.data=json.loads(MANIFEST.read_text())
    def validate(self,data):
        with tempfile.TemporaryDirectory(prefix='compat-negative-') as folder:
            p=Path(folder)/'manifest.json';p.write_text(json.dumps(data));return check(ROOT,p,SCHEMAS)
    def test_actual_inventory_and_generated_refs(self):self.assertEqual(self.validate(self.data),[])
    def test_unmapped_contract_rejected(self):
        self.data['contracts'].pop();self.assertTrue(self.validate(self.data))
    def test_new_unmapped_field_rejected(self):
        c=self.data['contracts'][0];c['field_mappings'].pop();self.assertTrue(self.validate(self.data))
    def test_source_drift_rejected(self):
        self.data['contracts'][0]['sources'][0]['sha256']='0'*64;self.assertTrue(self.validate(self.data))
    def test_nonexistent_route_rejected(self):
        self.data['contracts'][0]['routes'][0]['literal']='/nonexistent-frozen-route';self.assertTrue(self.validate(self.data))
    def test_runtime_claim_rejected(self):
        self.data['contracts'][0]['runtime_qualified']=True;self.assertTrue(self.validate(self.data))
    def test_unknown_generated_field_rejected(self):
        if not SCHEMAS:self.fail('COMPAT_SCHEMAS required to test materialized references')
        c=self.data['contracts'][1];c['field_mappings'][0].update(target_schema_ref='schema-definitions.json#/$defs/PoolStats',target_field='fabricatedField');self.assertTrue(self.validate(self.data))
if __name__=='__main__':unittest.main()
