#!/usr/bin/env python3
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = ('beta-qualify.sh', 'beta-ledger-check.sh', 'beta-report.sh')


class FocusedSourceIdentity(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='focused-source-')
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.origin = self.base / 'origin'
        self.origin.mkdir()
        self.run_command(['git', 'init', '-b', 'main', str(self.origin)])
        self.git('config', 'user.email', 'source-test@example.invalid', origin=True)
        self.git('config', 'user.name', 'Source Identity Test', origin=True)
        (self.origin / 'scripts/ci').mkdir(parents=True)
        for name in SCRIPTS:
            shutil.copy2(ROOT / 'scripts/ci' / name, self.origin / 'scripts/ci' / name)
        (self.origin / 'source.txt').write_text('original source\n')
        (self.origin / 'operation.py').write_text('''import hashlib,json,os,pathlib,subprocess,sys
mode=sys.argv[1]
p=pathlib.Path('dependency/source.txt' if mode=='submodule-restore' else 'source.txt')
if mode=='syntax':
 for name in ('beta-qualify.sh','beta-ledger-check.sh','beta-report.sh'):
  subprocess.run(['bash','-n','scripts/ci/'+name],check=True)
elif mode=='persistent': p.write_text('changed source')
elif mode in ('restore','submodule-restore'):
 old=p.read_bytes();p.write_text('transient change');p.write_bytes(old)
elif mode=='commit':
 p.write_text('new committed source')
 subprocess.run(['git','add','source.txt'],check=True)
 subprocess.run(['git','-c','core.hooksPath=/dev/null','commit','-m','During command'],check=True)
elif mode=='failure': sys.exit(7)
elif mode=='runner':
 revision=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
 directory=pathlib.Path(os.environ['LAYERX_QUALIFICATION_ARTIFACT_DIR'])
 for kind in ('status','report'):
  (directory/(kind+'.json')).write_text(json.dumps({'schema':'layerx-qualification-'+kind+'-v1','source_revision':revision,'source_identity':hashlib.sha256(revision.encode()+b'\\0').hexdigest()}))
''')
        targets = ['syntax', 'persistent', 'restore', 'commit', 'failure', 'runner', 'submodule-restore']
        (self.origin / 'Makefile').write_text(''.join(
            f'source-{name}:\n\tpython3 operation.py {name}\n' for name in targets) +
            'platform-beta-cluster-up:\n\tpython3 operation.py failure\n')
        self.git('add', 'scripts', 'source.txt', 'operation.py', 'Makefile', origin=True)
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Real source identity corpus', origin=True)
        self.tree = self.base / 'worktree'
        self.git('worktree', 'add', '--detach', str(self.tree), 'HEAD', origin=True)
        self.revision = self.git('rev-parse', 'HEAD')
        self.private = self.base / 'evidence'
        self.private.mkdir(mode=0o700)
        self.ledger = self.private / 'qualification.kvx'
        self.spec = self.private / 'spec.kvx'
        self.contract = self.private / 'contract.md'
        self.contract.write_text('''## Identity
| Key | Value |
| --- | --- |
| readiness_claim | false |
## Surfaces and journeys
| Surface | Journey | Class | Required rung | Reached rung | Source |
| --- | --- | --- | --- | --- | --- |
| human-web | source evidence | functional | statically_coherent | source_present | scripts/ci |
## Artifact set
| Ecosystem | Registry | Surface | Packages | Publication job |
| --- | --- | --- | --- | --- |

| Key | Value |
| --- | --- |
| artifact_manifest_path | unavailable.json |
| artifact_manifest_status | unavailable |
| report_status | no-go |
| report_generator | scripts/ci/beta-report.sh |
''')

    def run_command(self, command, cwd=None, expected=0):
        result = subprocess.run(list(map(str, command)), cwd=cwd, capture_output=True,
                                text=True, timeout=60)
        if expected is not None:
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def git(self, *args, origin=False):
        return self.run_command(['git', '-C', self.origin if origin else self.tree, *args]).stdout.strip()

    def qualify(self, target='source-syntax', kind='file', expected=0):
        command = 'make ' + target
        self.spec.write_text('[req.209]\n' + ''.join(f'ac_{i} = "source criterion"\n' for i in range(1, 5)) +
                             '[task.25.1]\nreqs = ["209"]\nverify_cmd = ' + json.dumps(command) + '\n' +
                             '[task.5.4]\nreqs = ["209"]\nverify_cmd = ' + json.dumps(command) + '\n')
        result = self.run_command(['bash', self.tree / 'scripts/ci/beta-qualify.sh', '--spec', self.spec,
                                  '--task', '25.1', '--ledger', self.ledger, '--evidence-root', self.private,
                                  '--evidence-kind', kind, '--', command], expected=expected)
        record = {}
        for line in self.ledger.read_text().splitlines():
            if '=' in line:
                key, value = line.split('=', 1)
                record[key.strip()] = json.loads(value)
        self.record = record
        self.sidecar = Path(record['source_evidence'])
        self.document = json.loads(self.sidecar.read_text())
        return result

    def write_ledger(self, record=None, name='gate.25.1.1'):
        record = self.record if record is None else record
        self.ledger.write_text('[' + name + ']\n' + ''.join(key + ' = ' + json.dumps(value) + '\n'
                                                          for key, value in record.items()))
        self.ledger.chmod(0o600)

    def save_document(self, document):
        self.sidecar.write_text(json.dumps(document))

    def consumers(self, eligible, revision=None, mode='stdout'):
        revision = revision or self.revision
        ledger = self.run_command(['bash', self.tree / 'scripts/ci/beta-ledger-check.sh', '--ledger', self.ledger,
                                   '--spec', self.spec, '--evidence-root', self.private, '--candidate', revision],
                                  expected=0 if eligible else 1)
        args = ['bash', self.tree / 'scripts/ci/beta-report.sh', '--ledger', self.ledger, '--spec', self.spec,
                '--contract', self.contract, '--revision', revision, '--evidence-root', self.private]
        if mode == 'stdout': args.append('--stdout')
        else:
            args.extend(['--output', self.private / 'report.md'])
            if mode == 'check': args.append('--check')
        report = self.run_command(args, expected=0 if eligible else 1)
        if mode == 'stdout':
            self.assertIn('| eligible_release_gate_records | ' + ('1' if eligible else '0') + ' |', report.stdout)
            self.assertIn('| acceptance_criteria_covered | ' + ('4' if eligible else '0') + ' of 4 |', report.stdout)
        return report

    def test_clean_plain_log_and_private_output(self):
        self.qualify()
        self.assertTrue(self.document['release_eligible'])
        self.assertEqual(self.document['source_start'], self.document['source_end'])
        self.assertTrue(self.document['mutation_observation']['complete'])
        self.assertEqual(self.sidecar.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.git('status', '--porcelain'), '')
        self.consumers(True)

    def test_dirty_tracked_staged_untracked_and_symlink(self):
        for variant in ('tracked', 'staged', 'untracked', 'symlink', 'credential'):
            with self.subTest(variant=variant):
                if variant in ('tracked', 'staged'):
                    (self.tree / 'source.txt').write_text('dirty')
                    if variant == 'staged': self.git('add', 'source.txt')
                elif variant == 'symlink': (self.tree / 'new-link').symlink_to('source.txt')
                else: (self.tree / ('.env' if variant == 'credential' else 'new-source')).write_text('private-marker')
                self.qualify()
                self.assertEqual(self.document['development_outcome'], 'pass')
                self.assertFalse(self.document['release_eligible'])
                self.write_ledger()
                self.consumers(False)
                self.git('reset', '--hard', self.revision)
                for name in ('new-link', 'new-source', '.env'):
                    path = self.tree / name
                    if path.exists() or path.is_symlink(): path.unlink()
                self.ledger.unlink()

    def test_persistent_mutation_and_change_restore(self):
        for target in ('source-persistent', 'source-restore'):
            with self.subTest(target=target):
                self.qualify(target)
                self.assertEqual(self.document['command_exit_status'], 0)
                self.assertTrue(self.document['mutation_observation']['changed'])
                self.assertFalse(self.document['release_eligible'])
                self.write_ledger()
                self.consumers(False)
                self.git('reset', '--hard', self.revision)
                self.ledger.unlink()

    def test_mid_run_commit_reads_fresh_head(self):
        self.qualify('source-commit')
        self.assertNotEqual(self.document['source_start']['revision'], self.document['source_end']['revision'])
        self.assertFalse(self.document['release_eligible'])
        self.consumers(False)

    def test_other_candidate_cannot_reuse_clean_evidence(self):
        self.qualify()
        (self.tree / 'source.txt').write_text('later')
        self.git('add', 'source.txt')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Later candidate')
        later = self.git('rev-parse', 'HEAD')
        self.run_command(['bash', self.tree / 'scripts/ci/beta-ledger-check.sh', '--ledger', self.ledger,
                          '--spec', self.spec, '--evidence-root', self.private, '--candidate', later], expected=1)
        result = self.run_command(['bash', self.tree / 'scripts/ci/beta-report.sh', '--ledger', self.ledger,
                                  '--spec', self.spec, '--contract', self.contract, '--revision', later,
                                  '--evidence-root', self.private, '--stdout'])
        self.assertIn('| eligible_release_gate_records | 0 |', result.stdout)
        self.assertIn('| acceptance_criteria_covered | 0 of 4 |', result.stdout)

    def test_missing_malformed_duplicate_and_typed_sidecars(self):
        self.qualify()
        original = self.sidecar.read_text()
        for raw in ('', '{}', '{', '{"schema":1,"schema":2}'):
            with self.subTest(raw=raw):
                self.sidecar.write_text(raw)
                self.consumers(False)
        self.sidecar.write_text(original)
        cases = [('release_eligible', 'true'), ('command_exit_status', True), ('executed', 1),
                 ('state', 'running'), ('state', 'interrupted'), ('schema', 'unknown'),
                 ('environment', 'different'), ('command', 'make other'),
                 ('started_at', '2000-01-01T00:00:00Z')]
        for key, value in cases:
            with self.subTest(field=key, value=value):
                document = copy.deepcopy(self.document)
                document[key] = value
                self.save_document(document)
                self.consumers(False)
        self.sidecar.unlink()
        self.consumers(False)

    def test_changed_log_forged_clean_and_incomplete_monitor_refuse(self):
        self.qualify()
        for change in ('clean', 'monitor', 'tree', 'digest'):
            with self.subTest(change=change):
                value = copy.deepcopy(self.document)
                if change == 'clean': value['source_end']['clean'] = False
                elif change == 'monitor': value['mutation_observation']['complete'] = False
                elif change == 'tree':
                    value['source_start']['tree'] = '0' * 40
                    value['source_end']['tree'] = '0' * 40
                else: value['evidence']['sha256'] = '0' * 64
                self.save_document(value)
                self.consumers(False)
        self.save_document(self.document)
        Path(self.record['evidence']).write_text('changed retained log')
        self.consumers(False)

    def test_failed_and_blocked_command_outcomes(self):
        for target, outcome in (('source-failure', 'fail'), ('platform-beta-cluster-up', 'blocked')):
            self.qualify(target, expected=1)
            self.assertEqual(self.document['development_outcome'], outcome)
            self.assertEqual(self.document['executed'], outcome != 'blocked')
            self.assertFalse(self.document['release_eligible'])
            self.write_ledger()
            self.consumers(False)
            self.ledger.unlink()

    def test_directory_and_nested_runner_identity(self):
        self.qualify('source-runner', kind='directory')
        self.consumers(True)
        nested = Path(self.record['evidence']) / 'report.json'
        value = json.loads(nested.read_text())
        value['source_revision'] = '0' * 40
        nested.write_text(json.dumps(value))
        self.consumers(False)

    def test_owner_decision_requires_eligible_source_all_output_modes(self):
        (self.tree / 'source.txt').write_text('dirty owner evidence')
        self.qualify()
        self.record['task'] = '5.4'
        self.record['note'] = 'owner go decision: named owner'
        self.write_ledger(name='gate.5.4.1')
        report = self.consumers(False)
        self.assertIn('| owner_go_decision | not recorded |', report.stdout)
        self.assertIn('| beta_invitation | not permitted |', report.stdout)
        self.consumers(False, mode='render')
        self.consumers(False, mode='check')

    def test_conflicted_index_is_development_only(self):
        self.git('checkout', '-b', 'left')
        (self.tree / 'source.txt').write_text('left change')
        self.git('add', 'source.txt')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Left change')
        self.git('checkout', '-b', 'right', self.revision)
        (self.tree / 'source.txt').write_text('right change')
        self.git('add', 'source.txt')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Right change')
        self.revision = self.git('rev-parse', 'HEAD')
        self.run_command(['git', '-C', self.tree, 'merge', 'left'], expected=1)
        self.qualify()
        self.assertFalse(self.document['source_start']['clean'])
        self.consumers(False)

    def test_real_submodule_dirt_and_transient_mutation(self):
        self.git('-c', 'protocol.file.allow=always', 'submodule', 'add', str(self.origin), 'dependency')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-m', 'Real submodule')
        self.revision = self.git('rev-parse', 'HEAD')
        nested = self.tree / 'dependency/source.txt'
        original = nested.read_bytes()
        nested.write_text('dirty dependency')
        self.qualify()
        self.assertFalse(self.document['source_start']['clean'])
        self.consumers(False)
        nested.write_bytes(original)
        self.ledger.unlink()
        self.qualify('source-submodule-restore')
        self.assertTrue(self.document['source_start']['clean'])
        self.assertTrue(self.document['mutation_observation']['changed'])
        self.assertFalse(self.document['release_eligible'])
        self.consumers(False)

    def test_source_evidence_path_and_schema_binding(self):
        self.qualify()
        for field, replacement in (('source_evidence', str(self.base / 'outside.json')),
                                   ('evidence', str(self.private / 'absent.log'))):
            with self.subTest(field=field):
                original = self.record[field]
                self.record[field] = replacement
                self.write_ledger()
                self.consumers(False)
                self.record[field] = original
        self.write_ledger()
        public = self.private / 'public-source.json'
        public.write_text(self.sidecar.read_text())
        public.chmod(0o644)
        self.record['source_evidence'] = str(public)
        self.write_ledger()
        self.consumers(False)

    def test_duplicate_ledger_key_refuses_both_consumers(self):
        self.qualify()
        with self.ledger.open('a') as output: output.write('outcome = "pass"\n')
        self.consumers(False)


if __name__ == '__main__':
    unittest.main(verbosity=2)
