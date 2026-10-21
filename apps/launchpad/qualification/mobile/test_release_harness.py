import importlib.util
from pathlib import Path
import json
import math
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('release_harness', Path(__file__).with_name('release_harness.py'))
harness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(harness)


class ReleaseHarnessBoundaryTests(unittest.TestCase):
    def test_nonfinite_measurements_are_never_admitted(self):
        for value in (math.nan, math.inf, -math.inf, True, '20', None):
            with self.assertRaises(ValueError):
                harness.finite(value, 'observed scalar')
        for values in ([], [0], [-1], [math.nan]):
            with self.assertRaises(ValueError):
                harness.percentile(values, .75)

    def test_percentile_uses_rank_without_mutating_input(self):
        numbers = list(range(20, 0, -1))
        self.assertEqual(harness.percentile(numbers, .75), 15)
        self.assertEqual(numbers[0], 20)

    def test_absent_native_os_measurements_are_blockers(self):
        for parser in (harness.android_frames, harness.android_heap):
            for text in ('', Path(__file__).read_text()):
                with self.assertRaises(ValueError):
                    parser(text)

    def test_hash_binding_uses_actual_source_bytes(self):
        own = Path(__file__).resolve()
        expected = harness.digest(own)
        item = {'path': own.name, 'sha256': expected}
        self.assertEqual(harness.reference(own.parent, item), own)
        with self.assertRaises(ValueError):
            harness.reference(own.parent, {'path': own.name, 'sha256': '0' * 64})
        with self.assertRaises(ValueError):
            harness.reference(own.parent, {'path': '../release_harness.py', 'sha256': expected})

    def test_duplicate_json_authority_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / 'invalid.json'
            for text in ('{"artifactDigest":"a","artifactDigest":"b"}', '{"heap":NaN}'):
                file.write_text(text)
                with self.assertRaises(ValueError):
                    harness.load(file)

    def test_source_binding_changes_when_genuine_source_inventory_changes(self):
        root = Path(__file__).resolve().parent
        first = harness.source_digest(root, ['release_harness.py'])
        second = harness.source_digest(root, ['release_harness.py', 'test_release_harness.py'])
        self.assertNotEqual(first, second)
        with self.assertRaises(ValueError):
            harness.source_digest(root, ['release_harness.py', 'release_harness.py'])


if __name__ == '__main__':
    unittest.main()
