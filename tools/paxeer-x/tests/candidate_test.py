#!/usr/bin/env python3
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
PROGRAM = ROOT / 'tools/paxeer-x/candidate.py'
SHELL = ROOT / 'tools/paxeer-x/runtime-inventory.sh'
SPEC = ROOT / 'spec/paxeer-x/spec.kvx'
loader = importlib.util.spec_from_file_location('candidate', PROGRAM)
candidate = importlib.util.module_from_spec(loader)
loader.loader.exec_module(candidate)


class CandidateTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='candidate-test-')
        self.addCleanup(self.directory.cleanup)
        self.base = Path(self.directory.name)
        self.repo = self.base / 'repo'
        self.repo.mkdir()
        self.git('init', '-b', 'main')
        self.git('config', 'user.email', 'candidate-test@example.invalid')
        self.git('config', 'user.name', 'Candidate Test')
        (self.repo / 'source.txt').write_text('candidate source\n')
        self.git('add', 'source.txt')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Initial source')
        self.manifest_path = self.base / 'candidate.json'
        result = self.cli('create', '--repo', self.repo, '--spec', SPEC,
                          '--output', self.manifest_path)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.manifest = candidate.load_private(self.manifest_path)
        self.roster = candidate.catalogue(SPEC)

    def git(self, *args):
        result = subprocess.run(['git', '-C', str(self.repo), *args],
                                capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip()

    def cli(self, *args):
        return subprocess.run([sys.executable, str(PROGRAM), *map(str, args)],
                              capture_output=True, text=True, timeout=30)

    def refused(self, manifest):
        with self.assertRaises(candidate.Invalid):
            candidate.validate(manifest, self.roster)

    def contract_vector(self):
        manifest = copy.deepcopy(self.manifest)
        revision = self.git('rev-parse', 'HEAD')
        digest = 'sha256:' + hashlib.sha256(PROGRAM.read_bytes()).hexdigest()
        for service in manifest['services']:
            for key in service['bindings']:
                service['bindings'][key] = (revision if key == 'source_revision' else
                                             digest if key.endswith('digest') else
                                             'secure://contract/' + service['id'] + '/' + key)
            service['dependencies_ref'] = 'secure://contract/dependencies'
            row = candidate.observation(service['id'], 'running', digest,
                                        'registry-manifest', 'secure://contract/observation')
            row.update(source_revision=revision, config_digest=digest,
                       readiness='ready', role_identity='secure://contract/role')
            service['observations'] = [row]
            service['action'] = 'preserved'
        return manifest

    def test_real_git_identity_complete_catalogue_private_output(self):
        self.assertEqual(len(self.manifest['services']), 31)
        self.assertEqual(self.manifest['source']['revision'], self.git('rev-parse', 'HEAD'))
        self.assertEqual(self.manifest['source']['tree'], self.git('rev-parse', 'HEAD^{tree}'))
        self.assertTrue(self.manifest['source']['integrated'])
        self.assertFalse(self.manifest['source']['dirty'])
        self.assertEqual(self.manifest_path.stat().st_mode & 0o777, 0o600)
        self.assertTrue(all(s['action'] == 'unknown' and not s['mutation_allowed']
                            for s in self.manifest['services']))
        result = self.cli('validate', self.manifest_path, '--repo', self.repo, '--spec', SPEC)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.cli('validate', self.manifest_path, '--repo', self.repo,
                                 '--spec', SPEC, '--require-ready').returncode, 2)

    def test_actual_branch_and_dirty_source_receive_no_credit(self):
        self.git('checkout', '-b', 'feature')
        (self.repo / 'change.txt').write_text('branch implementation\n')
        self.git('add', 'change.txt')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Branch change')
        identity = candidate.source_identity(self.repo, 'refs/heads/main')
        self.assertFalse(identity['integrated'])
        self.assertFalse(identity['release_credit'])
        (self.repo / 'change.txt').write_text('uncommitted implementation\n')
        self.assertTrue(candidate.source_identity(self.repo, 'refs/heads/main')['dirty'])
        forged = copy.deepcopy(self.manifest)
        forged['source'] = identity
        forged['source']['mainline_revision'] = identity['revision']
        forged['source']['integrated'] = True
        with self.assertRaises(candidate.Invalid):
            candidate.validate(forged, self.roster, self.repo)

    def test_missing_duplicate_and_extra_services_refuse(self):
        for action in ('missing', 'duplicate', 'extra'):
            with self.subTest(action=action):
                manifest = copy.deepcopy(self.manifest)
                if action == 'missing':
                    manifest['services'].pop()
                elif action == 'duplicate':
                    manifest['services'].append(copy.deepcopy(manifest['services'][0]))
                else:
                    manifest['services'][0]['id'] = 'unlisted'
                self.refused(manifest)

    def test_foundation_and_source_claim_changes_refuse(self):
        for field, value in (('chain_id', 1), ('nodes', 'deploy'), ('relocation', True),
                             ('wallet_authentication', 'different'), ('rpc_records', ['api1'])):
            manifest = copy.deepcopy(self.manifest)
            manifest['foundation'][field] = value
            self.refused(manifest)
        manifest = copy.deepcopy(self.manifest)
        manifest['source']['release_credit'] = True
        self.refused(manifest)

    def test_contract_vectors_preserve_configure_update_deploy(self):
        manifest = self.contract_vector()
        candidate.validate(manifest, self.roster)
        service = manifest['services'][0]
        service['observations'][0]['config_digest'] = 'sha256:' + '0' * 64
        service['action'] = 'configure'
        candidate.validate(manifest, self.roster)
        service['observations'][0]['image_digest'] = 'sha256:' + '1' * 64
        service['action'] = 'update'
        candidate.validate(manifest, self.roster)
        service['observations'][0].update(state='absent', readiness='unknown')
        service['action'] = 'deploy'
        candidate.validate(manifest, self.roster)

    def test_every_unknown_binding_blocks_mutation_decisions(self):
        for field in candidate.BINDING_FIELDS:
            with self.subTest(field=field):
                manifest = self.contract_vector()
                manifest['services'][0]['bindings'][field] = 'unknown'
                self.refused(manifest)
                manifest['services'][0]['action'] = 'unknown'
                candidate.validate(manifest, self.roster)
        manifest = self.contract_vector()
        manifest['services'][0]['mutation_allowed'] = True
        self.refused(manifest)

    def test_image_without_running_role_is_not_readiness(self):
        for state in ('created', 'stopped', 'unknown'):
            manifest = self.contract_vector()
            manifest['services'][0]['observations'][0]['state'] = state
            self.refused(manifest)
        manifest = self.contract_vector()
        manifest['services'][0]['observations'][0]['role_identity'] = 'unknown'
        self.refused(manifest)
        manifest = self.contract_vector()
        manifest['services'][0]['observations'][0]['image_digest'] = 'image:latest'
        self.refused(manifest)

    def test_unresolved_dependency_and_cycles_refuse(self):
        manifest = self.contract_vector()
        first, second = manifest['services'][:2]
        first['dependency_ids'] = [second['id']]
        second['bindings']['membership_ref'] = 'unknown'
        second['action'] = 'unknown'
        self.refused(manifest)
        first['action'] = 'unknown'
        candidate.validate(manifest, self.roster)
        second['dependency_ids'] = [first['id']]
        self.refused(manifest)

    def test_branch_only_pending_changes_cannot_claim_integration(self):
        manifest = copy.deepcopy(self.manifest)
        manifest['branch_changes'] = [{'revision': self.git('rev-parse', 'HEAD'),
                                      'evidence_ref': 'secure://source/branches',
                                      'integration': 'not-credited', 'pending_tasks': ['24.1']}]
        candidate.validate(manifest, self.roster)
        manifest['branch_changes'][0]['integration'] = 'integrated'
        self.refused(manifest)

    def test_secret_fields_and_inline_values_refuse_without_echo(self):
        for key in ('token', 'password', 'environment', 'private_key'):
            manifest = copy.deepcopy(self.manifest)
            manifest['services'][0]['bindings'][key] = 'forbidden-input-marker'
            self.refused(manifest)
        manifest = copy.deepcopy(self.manifest)
        manifest['services'][0]['bindings']['authority_ref'] = 'https://name:forbidden-input-marker@host/'
        candidate.write_private(self.base / 'unsafe.json', manifest)
        result = self.cli('validate', self.base / 'unsafe.json', '--repo', self.repo, '--spec', SPEC)
        self.assertEqual(result.returncode, 2)
        self.assertNotIn('forbidden-input-marker', result.stdout + result.stderr)

    def test_private_loader_refuses_symlinks_hardlinks_public_and_env_files(self):
        public = self.base / 'public.json'
        public.write_text('{}')
        public.chmod(0o644)
        symbolic = self.base / 'symbolic.json'
        symbolic.symlink_to(public)
        hardlink = self.base / 'hardlink.json'
        os.link(self.manifest_path, hardlink)
        for path in (public, symbolic, hardlink, self.base / '.env'):
            with self.subTest(kind=path.name), self.assertRaises((OSError, candidate.Invalid)):
                candidate.load_private(path)

    def test_duplicate_json_and_bounded_input_refuse(self):
        path = self.base / 'duplicate.json'
        path.write_text('{"schema":1,"schema":2}')
        path.chmod(0o600)
        with self.assertRaises(candidate.Invalid):
            candidate.load_private(path)
        large = self.base / 'large.json'
        with large.open('wb') as stream:
            stream.truncate(candidate.MAX_BYTES + 1)
        large.chmod(0o600)
        with self.assertRaises(candidate.Invalid):
            candidate.load_private(large)

    def test_no_overwrite_and_real_subprocess_deadline(self):
        before = self.manifest_path.read_bytes()
        result = self.cli('create', '--repo', self.repo, '--spec', SPEC,
                          '--output', self.manifest_path)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(before, self.manifest_path.read_bytes())
        with self.assertRaises(candidate.Invalid):
            candidate.run_bounded(['sleep', '2'], timeout=0.05)

    def test_production_cli_preserves_absent_pending_and_null_inventory(self):
        inventory = self.base / 'fly-input.json'
        apps = [
            {'app': 'absent-app', 'deployment_state': 'absent_from_authenticated_app_list',
             'observed_app': None, 'machines': None, 'releases': None},
            {'app': 'pending-app', 'deployment_state': 'pending',
             'observed_app': {'status': 'pending'},
             'machines': {'exit_code': 0, 'machines': []}, 'releases': None},
            {'app': 'unknown-app', 'deployment_state': 'deployed',
             'observed_app': {'status': 'deployed'}, 'machines': None},
            {'app': 'null-list-app', 'deployment_state': 'deployed',
             'machines': {'exit_code': 0, 'machines': None}},
            {'app': 'missing-metadata-app', 'deployment_state': 'deployed'},
            {'app': 'running-app', 'deployment_state': 'deployed',
             'machines': {'exit_code': 0, 'machines': [
                 {'id': 'machine-one', 'state': 'started', 'image_ref': None}]}},
        ]
        candidate.write_private(inventory, {
            'schema': 'paxeer-x.fly-inventory.v1', 'read_only': True, 'apps': apps,
            'services': [{'service': 'kernel', 'expected_apps_from_source':
                          [app['app'] for app in apps] + ['unlisted-app']}],
        })
        output = self.base / 'collector-candidate.json'
        result = self.cli('create', '--repo', self.repo, '--spec', SPEC,
                          '--inventory', inventory, '--output', output)
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = candidate.load_private(output)
        service = next(row for row in manifest['services'] if row['id'] == 'kernel')
        rows = {row['identity']: row for row in service['observations']}
        self.assertEqual(rows['absent-app']['state'], 'absent')
        self.assertEqual(rows['pending-app']['state'], 'pending')
        for name in ('unknown-app', 'null-list-app', 'missing-metadata-app', 'unlisted-app'):
            self.assertEqual(rows[name]['state'], 'unknown')
        self.assertEqual(rows['running-app/machine-one']['state'], 'started')
        self.assertTrue(all(row['readiness'] == 'unknown' and row['image_digest'] == 'unknown'
                            for row in rows.values()))
        self.assertEqual(service['action'], 'unknown')
        self.assertFalse(service['mutation_allowed'])

    def test_real_shell_selector_and_unknown_input_refusal(self):
        inventory = self.base / 'inventory.json'
        candidate.write_private(inventory, {'schema': 'paxeer-x.runtime-selection.v1',
                                            'read_only': True, 'services': []})
        output = self.base / 'selected.json'
        result = subprocess.run(['bash', str(SHELL), '--input', str(inventory),
                                 '--spec', str(SPEC), '--service', 'kernel',
                                 '--output', str(output)], capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        selected = candidate.load_private(output)
        self.assertEqual(selected['services'], [{'id': 'kernel', 'observations': []}])
        invalid = self.base / 'invalid.json'
        candidate.write_private(invalid, {'schema': 'unrecognized'})
        with self.assertRaises(candidate.Invalid):
            candidate.select_inventory([invalid], self.roster)


if __name__ == '__main__':
    unittest.main(verbosity=2)
