import importlib.util
import pathlib
import unittest

path = pathlib.Path(__file__).resolve().parents[1] / 'scripts/compatibility.py'
spec = importlib.util.spec_from_file_location('compatibility', path)
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)

class Compatibility(unittest.TestCase):
    def test_money_field_removal_enum_and_type_are_breaking(self):
        before = {'$defs': {'Amount': {'type': 'object', 'properties': {'raw': {'type': 'string'}, 'side': {'enum': ['buy','sell']}}}}}
        after = {'$defs': {'Amount': {'type': 'object', 'properties': {'side': {'enum': ['buy']}}}}}
        changes = module.breaking_changes(before, after)
        self.assertTrue(any('field removed' in change for change in changes))
        self.assertTrue(any('enum narrowed' in change for change in changes))
        with self.assertRaises(ValueError): module.require_window(changes, 2, 2, None)
        with self.assertRaises(ValueError): module.require_window(changes, 1, 2, None)
    def test_unchanged_schema_is_compatible(self):
        schema = {'$defs': {'Amount': {'type': 'string', 'pattern': '^[0-9]+$'}}}
        self.assertEqual(module.breaking_changes(schema, schema), [])
    def test_explicit_major_window_required(self):
        module.require_window(['breaking'], 1, 2, {'fromMajor':1,'toMajor':2,'startsAt':'2026-10-12T00:00:00Z','endsAt':'2026-11-12T00:00:00Z','retainedVersions':[1,2]})

if __name__ == '__main__': unittest.main()
