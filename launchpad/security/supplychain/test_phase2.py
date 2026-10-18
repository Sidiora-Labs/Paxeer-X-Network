import hashlib
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest
from ci_tools import verified_executable
from policy import Rejected, digest
from provenance import Tool, read_document, source_manifest, write_document


class SourceAndExecution(unittest.TestCase):
    def test_real_pinned_python_execution_and_tamper(self):
        executable = pathlib.Path(sys.executable).resolve()
        with executable.open('rb') as source:
            expected = hashlib.file_digest(source, 'sha256').hexdigest()
        tool = Tool(str(executable), expected)
        self.assertEqual(tool.run(['-c', 'import sys; print(sys.version_info.major)']).strip(), b'3')
        with tempfile.TemporaryDirectory() as temporary:
            copied = pathlib.Path(temporary) / 'python'
            shutil.copy2(executable, copied)
            before = subprocess.check_output([str(copied), '--version'])
            with copied.open('ab') as target:
                target.write(b'changed executable bytes')
            self.assertEqual(subprocess.check_output([str(copied), '--version']), before)
            with self.assertRaisesRegex(Rejected, 'checksum'):
                verified_executable(copied, expected)

    def test_current_owned_source_manifest_detects_changes(self):
        root = pathlib.Path(__file__).resolve().parents[2]
        actual = source_manifest(root, ['security/supplychain/policy.py', 'security/supplychain/controller.py'])
        self.assertEqual(len(actual['files']), 2)
        with tempfile.TemporaryDirectory() as temporary:
            target = pathlib.Path(temporary)
            for name in actual['files']:
                (target / name).parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(root / name, target / name)
            self.assertEqual(source_manifest(target, list(actual['files'])), actual)
            with (target / 'security/supplychain/controller.py').open('ab') as source:
                source.write(b'\n')
            self.assertNotEqual(source_manifest(target, list(actual['files']))['sha256'], actual['sha256'])

    def test_source_manifest_rejects_escape_symlink_duplicate(self):
        root = pathlib.Path(__file__).resolve().parents[2]
        for names in (['../policy.py'], ['/etc/passwd'], ['security/supplychain/policy.py'] * 2):
            with self.subTest(names=names), self.assertRaises(Rejected):
                source_manifest(root, names)
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary)
            (path / 'linked.py').symlink_to(root / 'security/supplychain/policy.py')
            with self.assertRaises(Rejected):
                source_manifest(path, ['linked.py'])

    def test_atomic_document_binds_exact_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / 'nested' / 'result.json'
            value = {'source': 'current-source', 'qualified': False}
            write_document(path, value)
            expected = digest(path.read_bytes())
            self.assertEqual(read_document(path, expected)[0], value)
            with path.open('ab') as target:
                target.write(b' ')
            with self.assertRaisesRegex(Rejected, 'digest'):
                read_document(path, expected)

    def test_failed_real_process_is_rejected(self):
        executable = pathlib.Path(sys.executable).resolve()
        tool = Tool(str(executable), digest(executable.read_bytes()))
        with self.assertRaisesRegex(Rejected, 'exited 17'):
            tool.run(['-c', 'raise SystemExit(17)'])


class AdversarialExecution(unittest.TestCase):
    def test_execution_uses_sealed_bytes_after_path_replacement(self):
        import fcntl
        import os
        from ci_tools import execution_snapshot
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / 'python'
            shutil.copy2(pathlib.Path(sys.executable).resolve(), path)
            expected = digest(path.read_bytes())
            with execution_snapshot(path, expected) as (executable, descriptor):
                path.write_bytes(b'replaced after approval')
                result = subprocess.check_output([executable, '--version'], pass_fds=(descriptor,))
                self.assertTrue(result.startswith(b'Python '))
                with self.assertRaises(PermissionError):
                    os.write(descriptor, b'changed')
                self.assertTrue(fcntl.fcntl(descriptor, fcntl.F_GET_SEALS) & fcntl.F_SEAL_WRITE)
            with self.assertRaisesRegex(Rejected, 'checksum'):
                Tool(str(path), expected).run(['--version'])

    def test_tool_interruption_remains_failure_and_retry_executes_real_tool(self):
        import signal
        tool = Tool(str(pathlib.Path(sys.executable).resolve()), digest(pathlib.Path(sys.executable).resolve().read_bytes()))
        with self.assertRaisesRegex(Rejected, 'exited -' + str(signal.SIGTERM)):
            tool.run(['-c', 'import os,signal; os.kill(os.getpid(),signal.SIGTERM)'])
        self.assertEqual(tool.run(['-c', 'print("recovered")']).strip(), b'recovered')
        with self.assertRaises(subprocess.TimeoutExpired):
            tool.run(['-c', 'import time; time.sleep(10)'], timeout=0.1)
        self.assertEqual(tool.run(['-c', 'print("after-timeout")']).strip(), b'after-timeout')

    def test_promotion_lock_rejects_real_competing_process_and_recovers_after_death(self):
        import os
        from controller import promotion_lock
        module_root = str(pathlib.Path(__file__).resolve().parent)
        with tempfile.TemporaryDirectory() as temporary:
            output = str(pathlib.Path(temporary) / 'promotion.json')
            command = [sys.executable, '-c', 'import sys,time; sys.path.insert(0,sys.argv[1]); from controller import promotion_lock;\nwith promotion_lock(sys.argv[2]):\n print("locked",flush=True); time.sleep(30)', module_root, output]
            child = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'})
            try:
                self.assertEqual(child.stdout.readline(), b'locked\n')
                with self.assertRaisesRegex(Rejected, 'duplicate promotion'):
                    with promotion_lock(output):
                        self.fail('concurrent promotion admitted')
            finally:
                child.terminate()
                child.communicate(timeout=5)
            with promotion_lock(output):
                self.assertTrue(pathlib.Path(output + '.lock').is_file())

    def test_durable_recovery_binds_authority_artifact_and_steps(self):
        from controller import promotion_state
        from provenance import write_document
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / 'promotion.json'
            binding = {'authority_sha256': '1' * 64, 'release_sha256': '2' * 64, 'bundle_sha256': '3' * 64, 'image': 'ghcr.io/owner/app@sha256:' + '4' * 64}
            state_path, state = promotion_state(output, binding)
            state['completed'] = ['copied', 'sbom:image']
            write_document(state_path, state)
            self.assertEqual(promotion_state(output, binding)[1], state)
            for field in binding:
                changed = dict(binding)
                changed[field] += 'changed'
                with self.subTest(field=field), self.assertRaisesRegex(Rejected, 'authority changed'):
                    promotion_state(output, changed)
            state['promoted'] = True
            write_document(state_path, state)
            with self.assertRaisesRegex(Rejected, 'completion'):
                promotion_state(output, binding)
            state['promoted'] = False
            state['completed'].append('copied')
            write_document(state_path, state)
            with self.assertRaisesRegex(Rejected, 'steps'):
                promotion_state(output, binding)

    def test_atomic_recovery_document_survives_writer_process_exit(self):
        module_root = str(pathlib.Path(__file__).resolve().parent)
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / 'state.json'
            code = 'import sys,os; sys.path.insert(0,sys.argv[1]); from provenance import write_document; write_document(sys.argv[2],{"completed":["copied"],"promoted":False}); os._exit(19)'
            child = subprocess.run([sys.executable, '-c', code, module_root, str(output)], capture_output=True, timeout=5)
            self.assertEqual(child.returncode, 19)
            self.assertEqual(read_document(output)[0], {'completed': ['copied'], 'promoted': False})


class OCIGraphAdversaries(unittest.TestCase):
    def layout(self, directory, extras=()):
        import io
        import tarfile
        from policy import canonical
        root = pathlib.Path(directory)
        (root / 'blobs' / 'sha256').mkdir(parents=True)
        layer = io.BytesIO()
        with tarfile.open(fileobj=layer, mode='w') as archive:
            binary = pathlib.Path(sys.executable).resolve().read_bytes()
            entry = tarfile.TarInfo('app/app')
            entry.size = len(binary)
            entry.mode = 0o555
            archive.addfile(entry, io.BytesIO(binary))
            for name in extras:
                entry = tarfile.TarInfo(name)
                entry.size = 0
                archive.addfile(entry, io.BytesIO())
        def blob(data, kind):
            identity = digest(data)
            (root / 'blobs' / 'sha256' / identity).write_bytes(data)
            return {'mediaType': kind, 'size': len(data), 'digest': 'sha256:' + identity}
        config = blob(canonical({'architecture': 'amd64', 'os': 'linux', 'config': {'User': '65532:65532'}}), 'application/vnd.oci.image.config.v1+json')
        layer = blob(layer.getvalue(), 'application/vnd.oci.image.layer.v1.tar')
        manifest = blob(canonical({'schemaVersion': 2, 'config': config, 'layers': [layer]}), 'application/vnd.oci.image.manifest.v1+json')
        write_document(root / 'oci-layout', {'imageLayoutVersion': '1.0.0'})
        write_document(root / 'index.json', {'schemaVersion': 2, 'manifests': [manifest]})
        return manifest, config, layer

    def test_actual_oci_graph_binds_every_blob_and_approved_digest(self):
        from reproduce import validate_layout
        with tempfile.TemporaryDirectory() as temporary:
            manifest, config, layer = self.layout(temporary)
            actual = validate_layout(temporary, manifest['digest'])
            self.assertEqual(actual['binary_sha256'], digest(pathlib.Path(sys.executable).resolve().read_bytes()))
            with self.assertRaisesRegex(Rejected, 'changed after approval'):
                validate_layout(temporary, 'sha256:' + '0' * 64)
            for descriptor in (manifest, config, layer):
                path = pathlib.Path(temporary) / 'blobs' / 'sha256' / descriptor['digest'][7:]
                original = path.read_bytes()
                path.write_bytes(original + b'tamper')
                with self.assertRaisesRegex(Rejected, 'checksum'):
                    validate_layout(temporary, manifest['digest'])
                path.write_bytes(original)
            self.assertEqual(validate_layout(temporary), actual)

    def test_whiteouts_duplicate_normalized_paths_and_path_escape_fail(self):
        from reproduce import validate_layout
        for extra in ('app/.wh.app', 'app/.wh..wh..opq', './app/app', '../app/app'):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as temporary:
                self.layout(temporary, [extra])
                with self.assertRaises(Rejected):
                    validate_layout(temporary)

    def test_indirect_and_missing_blob_fail_closed(self):
        from reproduce import validate_layout
        with tempfile.TemporaryDirectory() as temporary:
            manifest, config, layer = self.layout(temporary)
            path = pathlib.Path(temporary) / 'blobs' / 'sha256' / layer['digest'][7:]
            backup = pathlib.Path(temporary) / 'saved-layer'
            path.rename(backup)
            with self.assertRaisesRegex(Rejected, 'missing or indirect'):
                validate_layout(temporary)
            path.symlink_to(backup)
            with self.assertRaisesRegex(Rejected, 'missing or indirect'):
                validate_layout(temporary)


class VulnerabilityResponseAdversaries(unittest.TestCase):
    def test_real_response_authority_deadlines_and_rotation(self):
        from scanner import response_state
        identity = 'CVE-2030-1000:parser@1.0'
        findings = [{'id': identity, 'severity': 'critical'}]
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / 'response.json'
            record = {'discovered': 100, 'triaged': None, 'mitigated': None, 'owner': 'security', 'ticket': 'incident-17'}
            write_document(path, {identity: record})
            config = {'response_authority_path': str(path), 'response_authority_sha256': digest(path.read_bytes())}
            self.assertEqual(response_state(config, findings, 101)[identity], record)
            with self.assertRaisesRegex(Rejected, identity + ':triaged'):
                response_state(config, findings, 14500)
            record['triaged'] = 200
            record['mitigated'] = 300
            write_document(path, {identity: record})
            with self.assertRaisesRegex(Rejected, 'digest'):
                response_state(config, findings, 301)
            config['response_authority_sha256'] = digest(path.read_bytes())
            self.assertEqual(response_state(config, findings, 90000)[identity], record)
            record['mitigated'] = 150
            write_document(path, {identity: record})
            config['response_authority_sha256'] = digest(path.read_bytes())
            with self.assertRaisesRegex(Rejected, 'chronology'):
                response_state(config, findings, 301)

    def test_reports_require_exact_exposure_authority_and_reject_conflicts(self):
        from scanner import findings_from_trivy
        finding = {'VulnerabilityID': 'CVE-2030-1000', 'PkgName': 'parser', 'InstalledVersion': '1.0', 'Severity': 'HIGH'}
        identity = 'CVE-2030-1000:parser@1.0'
        report = {'SchemaVersion': 2, 'Results': [{'Target': 'application', 'Vulnerabilities': [finding]}]}
        with self.assertRaisesRegex(Rejected, 'missing finding exposure'):
            findings_from_trivy({'image': report}, {})
        analysis = {identity: {'reachable': True, 'analysis': 'untrusted network request reaches parser'}}
        self.assertEqual(findings_from_trivy({'image': report}, analysis)[0]['id'], identity)
        changed = {'SchemaVersion': 2, 'Results': [{'Target': 'application', 'Vulnerabilities': [{**finding, 'Severity': 'CRITICAL'}]}]}
        with self.assertRaisesRegex(Rejected, 'conflicting'):
            findings_from_trivy({'image': report, 'fs': changed}, analysis)
        with self.assertRaisesRegex(Rejected, 'unused or stale'):
            findings_from_trivy({'image': report}, {**analysis, 'unobserved': {'reachable': False, 'analysis': 'irrelevant'}})


class ProvenanceInputAdversaries(unittest.TestCase):
    def test_provenance_material_rotation_and_duplicate_are_rejected(self):
        import copy
        from provenance import make_statement, validate_statement
        root = pathlib.Path(__file__).resolve().parents[2]
        source = source_manifest(root, ['security/supplychain/controller.py', 'security/supplychain/policy.py'])
        workflow = digest((root / '.github/workflows/ci.yml').read_bytes())
        binary = digest(pathlib.Path(sys.executable).resolve().read_bytes())
        expected = {'source': source['sha256'], 'workflow': workflow, 'repository': 'owner/repository', 'ref': 'refs/heads/main', 'service': 'core/indexer', 'binary': 'indexerd', 'architecture': 'amd64', 'builder': 'reviewed-builder', 'run_id': '123', 'sbom': digest(b'approved inventory bytes'), 'image': 'registry.example/app@sha256:' + binary, 'materials': source['files']}
        build = {**expected, 'binary_sha256': binary}
        statement = make_statement(expected, build, source['files'], 100)
        self.assertEqual(validate_statement(statement, expected), statement)
        for field in ('source', 'workflow', 'repository', 'ref', 'service', 'binary', 'architecture'):
            changed = copy.deepcopy(statement)
            changed['predicate']['buildDefinition']['externalParameters'][field] += '-changed'
            with self.subTest(field=field), self.assertRaisesRegex(Rejected, 'input mismatch'):
                validate_statement(changed, expected)
        changed = copy.deepcopy(statement)
        dependencies = changed['predicate']['buildDefinition']['resolvedDependencies']
        dependencies.append(copy.deepcopy(dependencies[0]))
        with self.assertRaisesRegex(Rejected, 'duplicate provenance dependency'):
            validate_statement(changed, expected)
        changed = copy.deepcopy(statement)
        changed['predicate']['buildDefinition']['resolvedDependencies'][0]['digest']['sha256'] = '0' * 64
        with self.assertRaisesRegex(Rejected, 'material mismatch'):
            validate_statement(changed, expected)


class ArchiveSnapshotAdversaries(unittest.TestCase):
    def test_source_replacement_cannot_change_archive_under_analysis(self):
        from provenance import artifact_snapshot
        with tempfile.TemporaryDirectory() as temporary:
            source = pathlib.Path(temporary) / 'image.tar'
            original = pathlib.Path(__file__).read_bytes()
            source.write_bytes(original)
            with artifact_snapshot(source) as snapshot:
                source.write_bytes(original + b'changed during scan')
                self.assertEqual(snapshot.read_bytes(), original)
                self.assertEqual(snapshot.stat().st_mode & 0o777, 0o400)
            self.assertFalse(snapshot.exists())
            with tempfile.TemporaryDirectory() as nested:
                indirect = pathlib.Path(nested) / 'linked.tar'
                indirect.symlink_to(source)
                with self.assertRaisesRegex(Rejected, 'direct artifact'):
                    with artifact_snapshot(indirect):
                        self.fail('indirect archive accepted')


class ReleaseMeasurementMath(unittest.TestCase):
    def test_nearest_rank_and_invalid_observations(self):
        from check_phase2 import nearest_rank
        self.assertEqual(nearest_rank(list(range(1, 101)), 95), 95)
        self.assertEqual(nearest_rank(list(range(1, 101)), 99), 99)
        self.assertEqual(nearest_rank([8, 3, 1], 95), 8)
        for samples in ([], [float('nan')], [float('inf')], [-1], [True]):
            with self.subTest(samples=samples), self.assertRaises(Rejected):
                nearest_rank(samples, 95)
        for percentile in (0, 101, True, 95.0):
            with self.subTest(percentile=percentile), self.assertRaises(Rejected):
                nearest_rank([1], percentile)

    def test_real_artifact_inventory_detects_byte_change_and_indirect_files(self):
        from check_phase2 import artifact_inventory
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            source = root / 'capture.json'
            original = pathlib.Path(__file__).read_bytes()
            source.write_bytes(original)
            write_document(root / 'phase-4.json', {'qualified': False})
            observed = artifact_inventory(root, excluded=('phase-4.json',))
            self.assertEqual(observed['files'], {'capture.json': {'sha256': digest(original), 'bytes': len(original)}})
            self.assertEqual(observed['total_bytes'], len(original))
            source.write_bytes(original + b'changed')
            self.assertNotEqual(artifact_inventory(root, excluded=('phase-4.json',))['files'], observed['files'])
            (root / 'indirect').symlink_to(source)
            with self.assertRaisesRegex(Rejected, 'indirect'):
                artifact_inventory(root)

    def test_managed_materialization_keeps_exact_bytes_and_strict_execution_guard(self):
        from ci_tools import execution_snapshot, materialized_pnpm_bundle
        executable = pathlib.Path(sys.executable).resolve()
        original = executable.read_bytes()
        with tempfile.TemporaryDirectory() as temporary:
            source = pathlib.Path(temporary) / 'reviewed-bundle'
            source.write_bytes(original)
            source.chmod(0o400)
            pin = {'path': str(source), 'sha256': digest(original)}
            with self.assertRaisesRegex(Rejected, 'not executable'):
                with execution_snapshot(source, pin['sha256']):
                    self.fail('nonexecutable bytes passed unchanged strict guard')
            with materialized_pnpm_bundle(pin) as managed:
                target = pathlib.Path(managed['path'])
                self.assertEqual(target.read_bytes(), original)
                self.assertEqual(target.stat().st_mode & 0o777, 0o500)
                with execution_snapshot(target, managed['sha256']) as (snapshot, descriptor):
                    self.assertTrue(subprocess.check_output([snapshot, '--version'], pass_fds=(descriptor,)).startswith(b'Python '))
            self.assertFalse(target.exists())
            self.assertEqual(source.stat().st_mode & 0o777, 0o400)
            source.chmod(0o600)
            source.write_bytes(original + b'changed')
            with self.assertRaisesRegex(Rejected, 'checksum'):
                with materialized_pnpm_bundle(pin):
                    self.fail('changed bundle was materialized')

    def test_real_phase4_worker_capture_retains_process_metrics_and_raw_hashes(self):
        from check_phase2 import candidate_snapshot, measured_release_stage
        root = pathlib.Path(__file__).resolve().parents[2]
        executable = pathlib.Path(sys.executable).resolve()
        pin = {'path': str(executable), 'sha256': digest(executable.read_bytes())}
        config = {'schema_version': 4, 'candidate_sha256': candidate_snapshot(root)['sha256'], 'python': pin, 'phase2': {'tamper_tool': pin}, 'measurement': {'stage_timeouts_seconds': {'tool_tamper': 30}, 'stage_budgets': {'tool_tamper': {'wall_ms_max': 30000, 'controller_cpu_ms_max': 30000, 'single_process_peak_rss_bytes_max': 536870912, 'artifact_bytes_max': 10485760}}}}
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / 'measurement'
            output.mkdir()
            authority = pathlib.Path(temporary) / 'authority.json'
            write_document(authority, config)
            observed = measured_release_stage(config, authority, root, output, 'tool_tamper', 30)
            self.assertEqual(observed['exit_code'], 0)
            self.assertEqual(observed['status'], 'passed')
            self.assertTrue(observed['worker']['result']['rejected'])
            self.assertGreater(observed['wall_ms'], 0)
            self.assertGreater(observed['single_process_peak_rss_bytes'], 0)
            self.assertIn('excludes remote', observed['worker']['metrics']['resource_scope'])
            stage = output / 'tool_tamper'
            self.assertEqual(observed['worker_result_sha256'], digest((stage / 'worker-result.json').read_bytes()))
            for name, artifact in observed['artifacts']['files'].items():
                self.assertEqual(artifact['sha256'], digest((stage / name).read_bytes()))

    def test_current_candidate_binding_rejects_absent_authority_before_provider_calls(self):
        import os
        from check_phase2 import candidate_snapshot, validate_release_measurement
        root = pathlib.Path(__file__).resolve().parents[2]
        config = {'schema_version': 4, 'candidate_sha256': candidate_snapshot(root)['sha256']}
        previous = os.environ.pop('QUALIFICATION_CANDIDATE_SHA256', None)
        try:
            with self.assertRaisesRegex(Rejected, 'candidate authority absent'):
                validate_release_measurement(config, root)
            os.environ['QUALIFICATION_CANDIDATE_SHA256'] = '0' * 64
            with self.assertRaisesRegex(Rejected, 'candidate authority absent'):
                validate_release_measurement(config, root)
        finally:
            if previous is None:
                os.environ.pop('QUALIFICATION_CANDIDATE_SHA256', None)
            else:
                os.environ['QUALIFICATION_CANDIDATE_SHA256'] = previous

    def test_missing_real_provider_marks_every_unexecuted_requirement_blocked(self):
        from check_phase2 import release_capacity, RELEASE_REQUIREMENTS, RELEASE_STAGES
        root = pathlib.Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / 'report'
            with self.assertRaisesRegex(Rejected, 'unsupported release measurement authority'):
                release_capacity({'schema_version': 0}, root, output, digest(b'actual rejected authority'))
            report = read_document(output / 'phase-4.json')[0]
            self.assertFalse(report['qualified'])
            self.assertFalse(report['global_capacity_accepted'])
            self.assertEqual(report['final_shared_load_restore_gate'], '9.1')
            self.assertEqual(set(report['stages']), set(RELEASE_STAGES))
            self.assertEqual(len(report['requirements']), len(RELEASE_REQUIREMENTS) * 5)
            self.assertTrue(all(item['status'] == 'blocked' for item in report['requirements'].values()))


class ReleaseControllerMeasurementBinding(unittest.TestCase):
    def test_real_blocked_release_measurement_cannot_authorize_publication(self):
        from check_phase2 import release_capacity
        from controller import verify_release_measurement
        root = pathlib.Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / 'measurement'
            authority = digest(b'rejected unsupported authority')
            with self.assertRaises(Rejected):
                release_capacity({'schema_version': 0}, root, output, authority)
            report = output / 'phase-4.json'
            with self.assertRaisesRegex(Rejected, 'absent or unqualified'):
                verify_release_measurement({'expected': {}}, report, authority, digest(pathlib.Path(__file__).read_bytes()))
            for pin in (None, '0', 'bad authority'):
                with self.subTest(pin=pin), self.assertRaisesRegex(Rejected, 'independent release measurement authority'):
                    verify_release_measurement({'expected': {}}, report, pin, digest(pathlib.Path(__file__).read_bytes()))


if __name__ == '__main__':
    unittest.main()
