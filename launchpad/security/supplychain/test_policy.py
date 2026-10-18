import unittest, copy, tempfile, subprocess, json, sys, shutil
from pathlib import Path
from policy import *
from build import validate, validate_context
from ci_tools import read_pins, verified_executable

class ReleasePolicy(unittest.TestCase):

    def setUp(self):
        self.sbom = canonical({'spdxVersion': 'SPDX-2.3', 'packages': [{'name': k, 'versionInfo': '1', 'licenseConcluded': 'NOASSERTION', 'kind': k} for k in ['go', 'javascript', 'os']]})
        self.expected = {'source': '1' * 64, 'workflow': '2' * 64, 'sbom': digest(self.sbom), 'image': 'registry.example/app@sha256:' + '3' * 64, 'repository': 'owner/repository', 'builder': 'trusted-ci', 'run_id': '123', 'ref': 'refs/heads/main', 'exception_approvers': ['security']}
        self.doc = {k: v for k, v in self.expected.items() if k not in ['exception_approvers']}
        self.doc.update(issued=100, expires=200, event='push', fork=False, scanner_status='success', vulnerabilities=[], checks={k: {'id': k + '-123', 'result': 'success', 'source': '1' * 64} for k in ['verify', 'contracts', 'frontend', 'policy', 'scan', 'signing']})

    def test_valid_admission(self):
        self.assertTrue(admission(self.doc, self.expected, 101)['admitted'])

    def test_each_required_failure_cancel_skip(self):
        for check in self.doc['checks']:
            for result in ['failure', 'cancelled', 'skipped', 'pending', '']:
                with self.subTest(check=check, result=result):
                    doc = copy.deepcopy(self.doc)
                    doc['checks'][check]['result'] = result
                    with self.assertRaises(Rejected):
                        admission(doc, self.expected, 101)

    def test_missing_check_and_reused_identity(self):
        del self.doc['checks']['scan']
        with self.assertRaises(Rejected):
            admission(self.doc, self.expected, 101)

    def test_binding_fields(self):
        for field in ['source', 'workflow', 'image', 'sbom', 'repository', 'builder', 'run_id', 'ref']:
            doc = copy.deepcopy(self.doc)
            doc[field] = 'forged'
            with self.subTest(field=field), self.assertRaises(Rejected):
                admission(doc, self.expected, 101)

    def test_stale_and_future(self):
        for now in [99, 200, 201]:
            with self.subTest(now=now), self.assertRaises(Rejected):
                admission(self.doc, self.expected, now)

    def test_fork_and_pull_request(self):
        for field, value in [('fork', True), ('event', 'pull_request'), ('scanner_status', 'unavailable')]:
            doc = copy.deepcopy(self.doc)
            doc[field] = value
            with self.subTest(field=field), self.assertRaises(Rejected):
                admission(doc, self.expected, 101)

    def test_stale_check(self):
        self.doc['checks']['verify']['source'] = '9' * 64
        with self.assertRaises(Rejected):
            admission(self.doc, self.expected, 101)

    def test_critical_reachability(self):
        self.doc['vulnerabilities'] = [{'id': 'CVE-test', 'severity': 'critical', 'reachable': True, 'analysis': 'network parser reachable'}]
        with self.assertRaisesRegex(Rejected, 'CVE-test'):
            admission(self.doc, self.expected, 101)
        self.doc['vulnerabilities'][0]['reachable'] = False
        admission(self.doc, self.expected, 101)

    def test_high_exception_expiry(self):
        self.doc['vulnerabilities'] = [{'id': 'CVE-high', 'severity': 'high', 'reachable': True, 'analysis': 'exposure', 'exception': {'id': 'EX-1', 'issued': 100, 'expires': 150, 'approved_by': 'security', 'controls': 'isolate egress'}}]
        e = self.doc['vulnerabilities'][0]['exception']
        self.expected['approved_exception_digests'] = [digest(canonical({'finding': 'CVE-high', 'source': self.expected['source'], 'image': self.expected['image'], 'exception': e}))]
        admission(self.doc, self.expected, 101)
        with self.assertRaisesRegex(Rejected, 'EX-1'):
            admission(self.doc, self.expected, 150)

    def test_high_exception_unapproved_and_too_long(self):
        for e in [{'id': 'EX-2', 'issued': 100, 'expires': 800000, 'approved_by': 'security', 'controls': 'isolate'}, {'id': 'EX-3', 'issued': 100, 'expires': 150, 'approved_by': 'attacker', 'controls': 'isolate'}]:
            self.doc['vulnerabilities'] = [{'id': 'CVE-high', 'severity': 'high', 'reachable': True, 'analysis': 'exposure', 'exception': e}]
            with self.assertRaises(Rejected):
                admission(self.doc, self.expected, 101)

    def test_duplicate_json(self):
        with self.assertRaises(Rejected):
            load(b'{"image":1,"image":2}')

    def test_sbom_coverage_and_tamper(self):
        validate_sbom(self.sbom, self.expected['sbom'])
        with self.assertRaises(Rejected):
            validate_sbom(self.sbom + b' ', self.expected['sbom'])
        data = canonical({'spdxVersion': 'SPDX-2.3', 'packages': [{'name': 'one', 'versionInfo': '1', 'licenseConcluded': 'MIT', 'kind': 'go'}]})
        with self.assertRaises(Rejected):
            validate_sbom(data, digest(data))

    def test_target_and_digest(self):
        valid = 'registry.example/base@sha256:' + '1' * 64
        validate('core/indexer', 'indexerd', valid, valid)
        for service, binary, image in [('../../etc', 'indexerd', valid), ('core/indexer', 'sh', valid), ('media/metadata', 'metadatad', valid), ('core/indexer', 'indexerd', 'golang:latest')]:
            with self.subTest(service=service, binary=binary, image=image), self.assertRaises(Rejected):
                validate(service, binary, image, valid)

    def test_actual_ed25519_and_forgery(self):
        with tempfile.TemporaryDirectory() as temp:
            d = Path(temp)
            subprocess.run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(d / 'private')], check=True, capture_output=True)
            subprocess.run(['openssl', 'pkey', '-in', str(d / 'private'), '-pubout', '-out', str(d / 'public')], check=True, capture_output=True)
            raw = canonical(self.doc)
            (d / 'doc').write_bytes(raw)
            subprocess.run(['openssl', 'pkeyutl', '-sign', '-inkey', str(d / 'private'), '-rawin', '-in', str(d / 'doc'), '-out', str(d / 'sig')], check=True, capture_output=True)
            key = (d / 'public').read_bytes()
            sig = (d / 'sig').read_bytes()
            self.assertTrue(admit_signed(raw, sig, key, digest(key), self.sbom, self.expected, 101)['admitted'])
            for data, signature, pin in [(raw + b' ', sig, digest(key)), (raw, sig, '0' * 64), (raw, b'x' * 64, digest(key))]:
                with self.subTest(pin=pin), self.assertRaises(Rejected):
                    admit_signed(data, signature, key, pin, self.sbom, self.expected, 101)

    def test_runtime_confinement(self):
        profile = {'runAsNonRoot': True, 'uid': 65532, 'readOnlyRootFilesystem': True, 'privileged': False, 'allowPrivilegeEscalation': False, 'hostNetwork': False, 'hostPID': False, 'hostIPC': False, 'capabilities': {'drop': ['ALL'], 'add': []}, 'seccomp': 'RuntimeDefault', 'egressPolicyDigest': '1' * 64, 'limits': {'cpu': 2, 'memory': 100000000, 'pids': 128}, 'tmpfs': [{'path': '/tmp', 'bytes': 1000}]}
        runtime(profile)
        for field, value in [('uid', 0), ('privileged', True), ('readOnlyRootFilesystem', False), ('hostNetwork', True), ('seccomp', 'Unconfined'), ('limits', {}), ('capabilities', {'add': ['NET_RAW'], 'drop': []})]:
            bad = copy.deepcopy(profile)
            bad[field] = value
            with self.subTest(field=field), self.assertRaises(Rejected):
                runtime(bad)

    def signed_document(self, doc):
        with tempfile.TemporaryDirectory() as temp:
            d = Path(temp)
            subprocess.run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(d / 'private')], check=True, capture_output=True)
            subprocess.run(['openssl', 'pkey', '-in', str(d / 'private'), '-pubout', '-out', str(d / 'public')], check=True, capture_output=True)
            raw = canonical(doc)
            (d / 'doc').write_bytes(raw)
            subprocess.run(['openssl', 'pkeyutl', '-sign', '-inkey', str(d / 'private'), '-rawin', '-in', str(d / 'doc'), '-out', str(d / 'sig')], check=True, capture_output=True)
            key = (d / 'public').read_bytes()
            return admit_signed(raw, (d / 'sig').read_bytes(), key, digest(key), self.sbom, self.expected, 101)

    def test_signed_builder_cannot_forge_exception_approval(self):
        self.doc['vulnerabilities'] = [{'id': 'CVE-high', 'severity': 'high', 'reachable': True, 'analysis': 'network exposure', 'exception': {'id': 'EX-forged', 'issued': 100, 'expires': 150, 'approved_by': 'security', 'controls': 'isolation'}}]
        with self.assertRaisesRegex(Rejected, 'independent approval'):
            self.signed_document(self.doc)
        e = self.doc['vulnerabilities'][0]['exception']
        self.expected['approved_exception_digests'] = [digest(canonical({'finding': 'CVE-high', 'source': self.expected['source'], 'image': self.expected['image'], 'exception': e}))]
        self.assertTrue(self.signed_document(self.doc)['admitted'])
        e['expires'] = 151
        with self.assertRaisesRegex(Rejected, 'independent approval'):
            self.signed_document(self.doc)

    def test_signed_missing_scan_list_fails(self):
        del self.doc['vulnerabilities']
        with self.assertRaisesRegex(Rejected, 'vulnerability list'):
            self.signed_document(self.doc)

    def test_scan_list_shape_and_bound(self):
        for value in [None, {}, 'clean', [{}], [{'id': 'CVE', 'severity': 'low', 'reachable': False, 'analysis': 'checked', 'extra': True}], [{}] * 10001]:
            self.doc['vulnerabilities'] = value
            with self.subTest(value_type=type(value).__name__), self.assertRaises(Rejected):
                admission(self.doc, self.expected, 101)

    def test_context_negations_and_nested_secret_exclusions(self):
        safe = '\n'.join(['.env', '.env.*', '**/.env*', '**/node_modules', '**/.git', 'evidence', '**/*.pem', '**/*.key', '.codify', '.cg', '.codex', '**/token.json', '**/coordinator.key'])
        validate_context(safe)
        for bad in [safe + '\n!.env', safe + '\n!**/*.key', safe.replace('**/.env*\n', ''), safe.replace('.codify\n', '')]:
            with self.subTest(bad=bad), self.assertRaises(Rejected):
                validate_context(bad)

    def test_tool_checksum_before_execution(self):
        data = b'reviewed executable fixture'
        self.assertEqual(verified_tool_bytes(data, digest(data)), data)
        with self.assertRaisesRegex(Rejected, 'checksum'):
            verified_tool_bytes(data + b'modified', digest(data))

    def test_reviewed_manifest_binds_all_tool_paths(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'pins.json'
            binary = Path(sys.executable).resolve()
            pin = {'path': str(binary), 'sha256': digest(binary.read_bytes())}
            raw = canonical({'schema_version': 1, 'tools': {name: pin for name in ['go', 'node', 'pnpm']}})
            path.write_bytes(raw)
            self.assertEqual(read_pins(str(path), digest(raw))['go'], pin)
            path.write_bytes(raw + b' ')
            with self.assertRaisesRegex(Rejected, 'checksum'):
                read_pins(str(path), digest(raw))
            incomplete = canonical({'schema_version': 1, 'tools': {'go': pin}})
            path.write_bytes(incomplete)
            with self.assertRaisesRegex(Rejected, 'incomplete'):
                read_pins(str(path), digest(incomplete))

    def test_modified_real_executable_is_rejected_despite_same_version(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'python'
            shutil.copy2(Path(sys.executable).resolve(), path)
            trusted = digest(path.read_bytes())
            expected = subprocess.check_output([verified_executable(path, trusted), '--version'])
            with path.open('ab') as out:
                out.write(b'modified tool bytes')
            self.assertEqual(subprocess.check_output([str(path), '--version']), expected)
            with self.assertRaisesRegex(Rejected, 'checksum'):
                verified_executable(path, trusted)

    def test_workflow_publication_bypass_removed(self):
        text = (Path(__file__).resolve().parents[2] / '.github/workflows/ci.yml').read_text()
        self.assertNotIn('!cancelled()', text)
        self.assertNotIn('packages: write', text)
        self.assertNotIn('docker push', text)
        self.assertIn('needs: [verify, contracts, frontend, policy]', text)
        for line in text.splitlines():
            if 'uses:' in line:
                self.assertRegex(line, '@[0-9a-f]{40} #')
    def test_actual_signing_authority_rotation_revokes_old_key(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            raw = canonical(self.doc)
            (directory / 'document').write_bytes(raw)
            keys = []
            for name in ('previous', 'current'):
                private = directory / (name + '.private')
                public = directory / (name + '.public')
                signature = directory / (name + '.signature')
                subprocess.run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(private)], check=True, capture_output=True)
                subprocess.run(['openssl', 'pkey', '-in', str(private), '-pubout', '-out', str(public)], check=True, capture_output=True)
                subprocess.run(['openssl', 'pkeyutl', '-sign', '-inkey', str(private), '-rawin', '-in', str(directory / 'document'), '-out', str(signature)], check=True, capture_output=True)
                keys.append((public.read_bytes(), signature.read_bytes()))
            previous, current = keys
            self.assertTrue(admit_signed(raw, previous[1], previous[0], digest(previous[0]), self.sbom, self.expected, 101)['admitted'])
            with self.assertRaisesRegex(Rejected, 'untrusted signing identity'):
                admit_signed(raw, previous[1], previous[0], digest(current[0]), self.sbom, self.expected, 101)
            with self.assertRaisesRegex(Rejected, 'signature verification failed'):
                admit_signed(raw, previous[1], current[0], digest(current[0]), self.sbom, self.expected, 101)
            self.assertTrue(admit_signed(raw, current[1], current[0], digest(current[0]), self.sbom, self.expected, 101)['admitted'])

    def test_scanner_outage_recovers_only_with_current_verified_receipt(self):
        for status in ('unavailable', 'error', 'timeout', 'success-with-no-results'):
            changed = copy.deepcopy(self.doc)
            changed['scanner_status'] = status
            with self.subTest(status=status), self.assertRaisesRegex(Rejected, 'scanner unavailable'):
                admission(changed, self.expected, 101)
        self.assertTrue(admission(self.doc, self.expected, 101)['admitted'])
        changed = copy.deepcopy(self.doc)
        changed['checks']['scan']['source'] = '0' * 64
        with self.assertRaisesRegex(Rejected, 'stale check: scan'):
            admission(changed, self.expected, 101)

    def test_exception_rotation_requires_renewed_independent_exact_approval(self):
        exception = {'id': 'EX-rotation-1', 'issued': 100, 'expires': 150, 'approved_by': 'security', 'controls': 'network isolation'}
        finding = {'id': 'CVE-high', 'severity': 'high', 'reachable': True, 'analysis': 'reachable parser', 'exception': exception}
        self.doc['vulnerabilities'] = [finding]
        def approve():
            self.expected['approved_exception_digests'] = [digest(canonical({'finding': finding['id'], 'source': self.expected['source'], 'image': self.expected['image'], 'exception': exception}))]
        approve()
        self.assertTrue(admission(self.doc, self.expected, 101)['admitted'])
        with self.assertRaisesRegex(Rejected, 'EX-rotation-1'):
            admission(self.doc, self.expected, 150)
        exception.update(id='EX-rotation-2', issued=150, expires=190)
        with self.assertRaisesRegex(Rejected, 'independent approval: EX-rotation-2'):
            admission(self.doc, self.expected, 151)
        approve()
        self.assertTrue(admission(self.doc, self.expected, 151)['admitted'])

if __name__ == '__main__':
    unittest.main()
