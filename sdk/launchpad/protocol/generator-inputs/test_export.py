import importlib.util, json, pathlib, unittest, copy
spec = importlib.util.spec_from_file_location('export', pathlib.Path(__file__).with_name('export.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class ExportTests(unittest.TestCase):

    def test_real_frozen_artifact_determinism_and_drift(self):
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        root = pathlib.Path('/root/mk-falcon/contracts')
        first = m.export(reviewed, root)
        second = m.export(reviewed, root)
        self.assertEqual(m.canonical(first), m.canonical(second))
        self.assertEqual(len(first['bundles']), 24)
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['abi'][0]['type'] = 'event'
        with self.assertRaisesRegex(ValueError, 'ABI differs'):
            m.export(changed, root)
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['artifactPath'] = '../../etc/passwd'
        with self.assertRaisesRegex(ValueError, 'escapes'):
            m.export(changed, root)

    def test_selector_inputs_and_identity_validation(self):
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        root = pathlib.Path('/root/mk-falcon/contracts')
        exported = m.export(reviewed, root)
        for bundle in exported['bundles']:
            source = next((c for c in reviewed['contracts'] if c['name'] == bundle['name']))
            self.assertEqual({e['signature']: e['topic0'] for e in bundle['events']}, {e['signature']: e['topic0'] for e in source['events']})
            self.assertEqual({f['signature']: f['selector'] for f in bundle['functions']}, {f['signature']: f['selector'] for f in source['functions']})
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['abiSha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'ABI hash drift'):
            m.export(changed, root)
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['events'].pop()
        with self.assertRaisesRegex(ValueError, 'unmapped event signature'):
            m.export(changed, root)
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['functions'][0]['selector'] = '0x1234'
        with self.assertRaisesRegex(ValueError, 'unmapped function signature'):
            m.export(changed, root)

    def test_versioned_inputs_are_deterministic_and_require_authentic_history(self):
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        root = pathlib.Path('/root/mk-falcon/contracts')
        first = m.export_revisions(reviewed, root, [], ['EventEmitter', 'SidioraFactory'])
        second = m.export_revisions(reviewed, root, [], ['SidioraFactory', 'EventEmitter'])
        self.assertEqual(m.canonical(first), m.canonical(second))
        self.assertEqual(m.revision_diff(first, second), [])
        changed = copy.deepcopy(second)
        event = next(event for bundle in changed['bundles'] for event in bundle['events'] if event['inputs'])
        event['inputs'][0]['indexed'] = not event['inputs'][0]['indexed']
        diff = m.revision_diff(first, changed)
        self.assertEqual(len(diff), 1)
        self.assertNotEqual(diff[0]['before'], diff[0]['after'])
        with self.assertRaisesRegex(ValueError, 'unmapped enabled'):
            m.export_revisions(reviewed, root, [], ['UnregisteredFamily'])
        raw = m.canonical(reviewed)
        with self.assertRaisesRegex(ValueError, 'unreviewed historical'):
            m.export_revisions(reviewed, root, [(raw, '0' * 64)], [])
        with self.assertRaisesRegex(ValueError, 'only reviewed EventEmitter'):
            m.export_revisions(reviewed, root, [(raw, m.sha(raw))], [])

    def test_real_optical_registry_review_preserves_compiler_and_source_identity(self):
        root = pathlib.Path('/root/mk-falcon/contracts')
        path = root / 'artifacts/contracts/opticals/OpticalRegistry.sol/OpticalRegistry.json'
        artifact_raw = path.read_bytes()
        artifact = json.loads(artifact_raw)
        debug = json.loads(path.with_suffix('.dbg.json').read_bytes())
        build_raw = (path.parent / debug['buildInfo']).read_bytes()
        build = json.loads(build_raw)
        source = 'contracts/opticals/OpticalRegistry.sol'
        bundle = {'name': 'OpticalRegistry', 'sourceName': source,
                  'artifactPath': path.relative_to(root).as_posix(),
                  'artifactSha256': m.sha(artifact_raw), 'abiSha256': m.sha(m.canonical(artifact['abi'])),
                  'sourceSha256': m.sha(build['input']['sources'][source]['content'].encode()),
                  'buildInfoSha256': m.sha(build_raw), 'compilerVersion': build['solcLongVersion'], 'abi': artifact['abi']}
        review = {'schemaVersion': 1, 'kind': 'reviewed-optical-registry', 'bundle': bundle}
        self.assertEqual(m.canonical(m.export_optical_registry(review, root)), m.canonical(review))
        changed = copy.deepcopy(review)
        changed['bundle']['sourceSha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'compiled source drift'):
            m.export_optical_registry(changed, root)
        changed = copy.deepcopy(review)
        changed['bundle']['artifactPath'] = '../../outside.json'
        with self.assertRaisesRegex(ValueError, 'wrong optical source identity'):
            m.export_optical_registry(changed, root)
    def test_signature_hashes_are_verified_against_real_current_artifacts(self):
        self.assertEqual(m.keccak256(b'Transfer(address,address,uint256)').hex(),
                         'ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef')
        self.assertEqual(m.keccak256(b'').hex(),
                         'c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470')
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        root = pathlib.Path('/root/mk-falcon/contracts')
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['events'][0]['topic0'] = '0x' + '1' * 64
        with self.assertRaisesRegex(ValueError, 'unmapped event signature'):
            m.export(changed, root)
        changed = copy.deepcopy(reviewed)
        changed['contracts'][0]['functions'][0]['selector'] = '0x12345678'
        with self.assertRaisesRegex(ValueError, 'unmapped function signature'):
            m.export(changed, root)
        changed = copy.deepcopy(reviewed)
        changed['contracts'].append(copy.deepcopy(changed['contracts'][0]))
        with self.assertRaisesRegex(ValueError, 'duplicate reviewed contract'):
            m.export(changed, root)

    def test_generation_measurement_records_real_artifact_identities_without_runtime_claims(self):
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        roots = pathlib.Path(__file__).resolve().parents[3] / 'contracts'
        result = m.generation_measurement(reviewed, roots, ['EventEmitter', 'SidioraERC20'])
        self.assertEqual(result['firstExportSha256'], result['secondExportSha256'])
        self.assertTrue(result['byteIdentical'])
        self.assertFalse(result['releaseReady'])
        self.assertGreater(result['elapsedNanoseconds'], 0)
        self.assertTrue(result['coverageMatrix'])
        self.assertTrue(all(row['runtimeCoverage'] == 'unmeasured' for row in result['coverageMatrix']))
        with self.assertRaisesRegex(ValueError, 'explicit unique'):
            m.generation_measurement(reviewed, roots, [])
        with self.assertRaisesRegex(ValueError, 'unmapped enabled'):
            m.generation_measurement(reviewed, roots, ['MissingEnabledFamily'])

    def test_binding_measurement_rejects_missing_independent_generations(self):
        reviewed = json.loads((pathlib.Path(__file__).resolve().parents[1] / 'catalog/reviewed.json').read_text())
        with self.assertRaisesRegex(ValueError, 'two independently reviewed'):
            m.measure_generated_bindings(reviewed, '0' * 64, ['EventEmitter'], [])

if __name__ == '__main__':
    unittest.main()
