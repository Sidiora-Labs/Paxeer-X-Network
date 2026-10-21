from __future__ import annotations

import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import release_harness as harness


class ReleaseHarnessSourceTests(unittest.TestCase):
    def test_actual_source_bytes_bound_and_changes_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'source-observations.ndjson'
            actual=Path(harness.__file__).resolve()
            row={'path':str(actual),'sha256':harness.sha_file(actual)}
            path.write_bytes(harness.canonical(row)+b'\n'+harness.canonical(row)+b'\n')
            checksum=harness.sha_file(path)
            observations=harness.rows(path,checksum)
            self.assertEqual(next(observations)['sha256'],harness.sha_file(actual))
            path.write_bytes(harness.canonical(row)+b'\n')
            with self.assertRaises(ValueError):
                list(observations)

    def test_duplicate_and_incomplete_raw_input_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'input.ndjson'
            for raw in (b'{"id":1,"id":2}\n',b'{"id":1}'):
                path.write_bytes(raw)
                with self.assertRaises(ValueError):
                    list(harness.rows(path,harness.sha_file(path)))

    def test_symlink_and_existing_output_denied(self):
        with tempfile.TemporaryDirectory() as directory:
            source=Path(directory)/'source'
            source.write_text('actual filesystem source')
            alias=Path(directory)/'alias'
            alias.symlink_to(source)
            with self.assertRaises(ValueError):
                harness.ordinary(alias)
            with self.assertRaises(FileExistsError):
                harness.owned_output(Path(directory))

    def test_incomplete_native_source_binding_cannot_run_components(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'binding.json'
            actual=Path(harness.__file__).resolve()
            interface=harness.ROOT/'spec/production-system/integration/interface-decisions.json'
            bound={'version':1,'candidate_sha256':harness.sha_file(actual),
                   'interface_sha256':harness.sha_file(interface),
                   'files':{str(actual.relative_to(harness.ROOT)):harness.sha_file(actual)}}
            path.write_bytes(harness.canonical(bound))
            with self.assertRaises(ValueError):
                harness.source_binding(path,harness.sha_file(path))

    def test_missing_native_authority_produces_explicit_failed_result(self):
        with tempfile.TemporaryDirectory() as directory:
            output=Path(directory)/'result'
            missing=Path(directory)/'missing.json'
            exit_code=harness.main(['analyze','--config',str(missing),'--config-sha256',hashlib.sha256(b'').hexdigest(),'--output',str(output)])
            self.assertEqual(exit_code,1)
            observed=json.loads((output/'security-release-result.json').read_bytes())
            self.assertFalse(observed['observed_assertions_passed'])
            self.assertFalse(observed['capacity_qualified'])
            self.assertFalse(observed['release_qualified'])
            self.assertEqual(observed['final_gate'],'9.1')
            self.assertTrue(observed['blocked_inputs'])

    def test_frozen_profile_has_no_mutable_override(self):
        self.assertEqual(harness.PROFILES,{'steady':(10000,2000,2000),'surge':(20000,4000,4000)})
        for missing in ({}, {'request_id':'unbound'}):
            with self.assertRaises(ValueError):
                harness.exact(missing,('run_id','request_id','candidate_sha256'))


if __name__=='__main__':
    unittest.main()
