#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))
from candidate import SCHEMA, UNKNOWN, BINDING_FIELDS, Invalid, load_private, write_private

PASS_GATE = '''#!/bin/sh
# paxeer-x-services: svc-01
if [ -n "${GATE_MARKER:-}" ]; then echo ran >> "$GATE_MARKER"; fi
echo "PAXEER_X_GATE tests=3 skipped=0"
'''
SERVICES = ''.join('[service.svc-%02d]\npurpose = "purpose"\nroute = "route"\ndependencies = "none"\n\n' % i
                   for i in range(1, 32))
TASKS = {'1.1': ['1'], '1.2': ['1'], '1.3': ['3'], '1.4': ['3'], '1.5': ['3'], '1.6': ['3'],
         '1.9': ['2']}


class GateDispatchTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix='gate-dispatch-test-')
        self.addCleanup(directory.cleanup)
        self.base = Path(directory.name)
        self.repo = self.base / 'repo'
        self.tools = self.repo / 'tools/paxeer-x'
        (self.tools / 'gates').mkdir(parents=True)
        for name in ('candidate.py', 'verify-task.sh', 'release.sh'):
            shutil.copy2(TOOLS / name, self.tools / name)
        self.spec = self.repo / 'spec/paxeer-x/spec.kvx'
        self.spec.parent.mkdir(parents=True)
        self.spec.write_text('[meta]\nstatus = "active"\n\n' + SERVICES + ''.join(
            '[task.%s]\ntitle = "task %s"\nstatus = "pending"\nreqs = %s\n\n' % (task, task, json.dumps(reqs))
            for task, reqs in TASKS.items()))
        self.gate('1.1', PASS_GATE)
        self.gate('1.2', PASS_GATE)
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.email', 'gate-dispatch-test@example.invalid')
        self.git('config', 'user.name', 'Gate Dispatch Test')
        self.evidence = self.base / 'evidence'
        self.evidence.mkdir(mode=0o700)
        self.private = self.base / 'private'
        self.private.mkdir(mode=0o700)
        self.marker = self.base / 'marker'
        self.manifests = 0
        self.commit()

    def gate(self, task, text, mode=0o755):
        path = self.tools / 'gates' / (task + '.sh')
        path.write_text(text)
        path.chmod(mode)
        return path

    def git(self, *args):
        result = subprocess.run(['git', '-C', str(self.repo), *args],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip()

    def commit(self):
        self.git('add', '-A')
        self.git('-c', 'core.hooksPath=/dev/null', 'commit', '-q', '-m', 'Change')
        self.manifest = self.make_manifest()

    def make_manifest(self):
        self.manifests += 1
        path = self.private / ('candidate-%d.json' % self.manifests)
        result = subprocess.run([sys.executable, str(self.tools / 'candidate.py'), 'create',
                                 '--repo', str(self.repo), '--spec', str(self.spec),
                                 '--output', str(path)],
                                capture_output=True, text=True, timeout=60,
                                env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
        self.assertEqual(result.returncode, 0, result.stderr)
        return path

    def environment(self, **extra):
        env = {key: value for key, value in os.environ.items() if not key.startswith('PAXEER_X_')}
        env.update(PAXEER_X_EVIDENCE_DIR=str(self.evidence), PAXEER_X_CANDIDATE_MANIFEST=str(self.manifest),
                   GATE_MARKER=str(self.marker), PYTHONDONTWRITEBYTECODE='1')
        env.update(extra)
        return {key: value for key, value in env.items() if value is not None}

    def tool(self, name, *args, **extra):
        return subprocess.run([str(self.tools / name), *map(str, args)], cwd=self.repo,
                              capture_output=True, text=True, timeout=120, env=self.environment(**extra))

    def verify(self, *args, **extra):
        return self.tool('verify-task.sh', *args, **extra)

    def evidence_of(self, result):
        lines = [line for line in result.stdout.splitlines() if line.startswith('evidence: ')]
        self.assertTrue(lines, result.stdout + result.stderr)
        return Path(lines[-1][len('evidence: '):])

    def runs(self):
        return len(self.marker.read_text().splitlines()) if self.marker.exists() else 0

    def test_pass_writes_private_bound_evidence(self):
        result = self.verify('1.1')
        self.assertEqual(result.returncode, 0, result.stderr)
        path = self.evidence_of(result)
        self.assertEqual(path.parent, self.evidence.resolve())
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        record = load_private(path)
        manifest = load_private(self.manifest)
        gate = self.tools / 'gates/1.1.sh'
        self.assertEqual(record['result'], 'pass')
        self.assertIs(record['credited'], True)
        self.assertEqual(record['source'], {'revision': self.git('rev-parse', 'HEAD'),
                                            'tree': self.git('rev-parse', 'HEAD^{tree}'), 'dirty': False})
        self.assertEqual(record['gate']['sha256'], hashlib.sha256(gate.read_bytes()).hexdigest())
        self.assertEqual(record['command']['argv'], [str(gate.resolve())])
        self.assertEqual(record['command']['cwd'], str(self.repo.resolve()))
        self.assertIs(record['command']['executed'], True)
        self.assertEqual(record['command']['dispatcher_argv'], ['1.1'])
        self.assertEqual(record['candidate']['schema'], SCHEMA)
        self.assertEqual(record['candidate']['sha256'], hashlib.sha256(json.dumps(
            manifest, sort_keys=True, separators=(',', ':')).encode()).hexdigest())
        self.assertEqual(record['candidate']['source_revision'], manifest['source']['revision'])
        bindings = next(s for s in manifest['services'] if s['id'] == 'svc-01')['bindings']
        self.assertEqual(record['candidate']['services'],
                         {'svc-01': {field: bindings[field] for field in BINDING_FIELDS}})
        self.assertEqual(record['candidate']['uncredited_fields'],
                         ['svc-01.' + field for field in BINDING_FIELDS if bindings[field] == UNKNOWN])
        self.assertEqual(len(record['candidate']['uncredited_fields']), len(BINDING_FIELDS))
        self.assertEqual((record['exit_code'], record['tests'], record['skipped']), (0, 3, 0))
        log = Path(record['log']['path'])
        self.assertEqual(log.stat().st_mode & 0o777, 0o600)
        self.assertEqual(record['log']['sha256'], hashlib.sha256(log.read_bytes()).hexdigest())
        self.assertEqual(self.runs(), 1)
        checked = self.verify('--check', path)
        self.assertEqual(checked.returncode, 0, checked.stderr)
        self.assertEqual(json.loads(checked.stdout)['identity'], record['gate']['identity'])
        identity = self.verify('--identity', '1.1')
        self.assertEqual(identity.returncode, 0, identity.stderr)
        self.assertEqual(json.loads(identity.stdout)['identity'], record['gate']['identity'])
        self.assertEqual(self.runs(), 1)

    def test_unknown_and_malformed_selectors_refuse(self):
        for args in (('9.9',), ('all',), ('../1.1',), ('1',), (), ('1.1', 'extra')):
            with self.subTest(args=args):
                result = self.verify(*args)
                self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(self.runs(), 0)
        self.assertEqual(list(self.evidence.iterdir()), [])

    def test_absent_gate_refuses(self):
        self.assertEqual(self.verify('1.9').returncode, 3)
        self.assertEqual(self.verify('--identity', '1.9').returncode, 3)

    def test_non_executable_gate_and_missing_interpreter_refuse(self):
        self.gate('1.3', PASS_GATE, mode=0o644)
        self.gate('1.4', '#!/nonexistent/interpreter\necho "PAXEER_X_GATE tests=1 skipped=0"\n')
        self.commit()
        self.assertEqual(self.verify('1.3').returncode, 4)
        self.assertEqual(self.verify('1.4').returncode, 4)
        self.assertEqual(self.runs(), 0)

    def test_empty_corpus_refuses(self):
        self.gate('1.3', '#!/bin/sh\necho "PAXEER_X_GATE tests=0 skipped=0"\n')
        self.gate('1.4', '#!/bin/sh\necho "ran nothing"\n')
        self.commit()
        for task in ('1.3', '1.4'):
            result = self.verify(task)
            self.assertEqual(result.returncode, 9, result.stderr)
            record = load_private(self.evidence_of(result))
            self.assertEqual((record['result'], record['credited']), ('empty', False))
            self.assertEqual(self.verify('--check', self.evidence_of(result)).returncode, 9)

    def test_skipped_requirements_and_failures_refuse(self):
        self.gate('1.3', '#!/bin/sh\necho "PAXEER_X_GATE tests=2 skipped=1"\n')
        self.gate('1.4', '#!/bin/sh\necho "PAXEER_X_GATE tests=2 skipped=0"\nexit 77\n')
        self.gate('1.5', '#!/bin/sh\necho "PAXEER_X_GATE tests=2 skipped=0"\nexit 1\n')
        self.commit()
        for task, code, result in (('1.3', 10, 'skipped'), ('1.4', 10, 'skipped'), ('1.5', 8, 'failed')):
            with self.subTest(task=task):
                run = self.verify(task)
                self.assertEqual(run.returncode, code, run.stderr)
                record = load_private(self.evidence_of(run))
                self.assertEqual((record['result'], record['credited']), (result, False))

    def test_stale_evidence_refuses(self):
        path = self.evidence_of(self.verify('1.1'))
        (self.repo / 'later.txt').write_text('later change\n')
        self.commit()
        self.assertEqual(self.verify('--check', path).returncode, 12)

    def test_dirty_source_refuses_credit(self):
        (self.repo / 'later.txt').write_text('uncommitted\n')
        self.assertEqual(self.verify('1.1').returncode, 6)
        self.manifest = self.make_manifest()
        result = self.verify('1.1')
        self.assertEqual(result.returncode, 7, result.stderr)
        record = load_private(self.evidence_of(result))
        self.assertEqual((record['result'], record['credited'], record['source']['dirty']), ('dirty', False, True))
        self.assertIs(record['command']['executed'], False)
        self.assertEqual(self.runs(), 0)
        self.assertEqual(self.verify('--identity', '1.1').returncode, 7)

    def test_gate_that_dirties_source_refuses_credit(self):
        self.gate('1.3', '#!/bin/sh\necho changed > dirtied.txt\necho "PAXEER_X_GATE tests=1 skipped=0"\n')
        self.commit()
        result = self.verify('1.3')
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assertEqual(load_private(self.evidence_of(result))['result'], 'dirty')

    def test_mismatched_manifest_identities_refuse(self):
        earlier = self.manifest
        path = self.evidence_of(self.verify('1.1'))
        (self.repo / 'later.txt').write_text('later change\n')
        self.commit()
        self.assertEqual(self.verify('1.1', '--manifest', earlier).returncode, 6)
        self.assertEqual(self.verify('1.1', PAXEER_X_CANDIDATE_MANIFEST=None).returncode, 6)
        self.git('reset', '-q', '--hard', 'HEAD~1')
        changed = load_private(earlier)
        changed['services'][0]['bindings']['config_ref'] = 'secure://other/config'
        other = self.private / 'changed.json'
        write_private(other, changed)
        self.assertEqual(self.verify('--check', path, '--manifest', earlier).returncode, 0)
        self.assertEqual(self.verify('--check', path, '--manifest', other).returncode, 6)
        changed['foundation']['chain_id'] = 1
        forged = self.private / 'forged.json'
        write_private(forged, changed)
        self.assertEqual(self.verify('1.1', '--manifest', forged).returncode, 6)
        self.gate('1.3', PASS_GATE.replace('svc-01', 'svc-99'))
        self.commit()
        self.assertEqual(self.verify('1.3').returncode, 6)

    def test_fabricated_evidence_refuses(self):
        path = self.evidence_of(self.verify('1.1'))
        record = load_private(path)
        log = Path(record['log']['path'])
        forged = dict(record, tests=99)
        target = self.evidence / 'forged-count.json'
        write_private(target, forged)
        self.assertEqual(self.verify('--check', target).returncode, 11)
        unsigned = {key: value for key, value in record.items() if key != 'hmac'}
        target = self.evidence / 'unsigned.json'
        write_private(target, unsigned)
        self.assertEqual(self.verify('--check', target).returncode, 11)
        target = self.evidence / 'public.json'
        target.write_text(json.dumps(record))
        target.chmod(0o644)
        self.assertEqual(self.verify('--check', target).returncode, 11)
        log.write_text('PAXEER_X_GATE tests=3 skipped=0\nedited\n')
        self.assertEqual(self.verify('--check', path).returncode, 11)
        (self.evidence / '.dispatch-key').unlink()
        self.assertEqual(self.verify('--check', path).returncode, 11)

    def test_unsafe_evidence_directory_refuses(self):
        inside = self.repo / 'evidence'
        inside.mkdir(mode=0o700)
        public = self.base / 'public'
        public.mkdir()
        public.chmod(0o755)
        for value in (str(inside), str(public), None, str(self.base / 'missing')):
            with self.subTest(value=value):
                self.assertEqual(self.verify('1.1', PAXEER_X_EVIDENCE_DIR=value).returncode, 5)
        self.assertEqual(self.runs(), 0)

    def test_release_deduplicates_identical_identities(self):
        scope = self.base / 'scope.txt'
        scope.write_text('# release scope\n1.1\nreq.1\n')
        result = self.tool('release.sh', scope, '1.2')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.runs(), 1)
        line = [l for l in result.stdout.splitlines() if l.startswith('release-evidence: ')][-1]
        path = Path(line.split()[1])
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        release = load_private(path)
        self.assertEqual(release['result'], 'pass')
        self.assertEqual(sorted(release['selectors']), ['1.1', '1.2'])
        self.assertEqual(len(set(release['selectors'].values())), 1)
        self.assertEqual(len(release['gates']), 1)
        gate = release['gates'][0]
        self.assertEqual((gate['selectors'], gate['exit_code']), (['1.1', '1.2'], 0))
        self.assertEqual(gate['evidence_sha256'], hashlib.sha256(Path(gate['evidence']).read_bytes()).hexdigest())
        self.assertEqual(release['source']['revision'], self.git('rev-parse', 'HEAD'))

    def test_release_runs_distinct_identities_and_reports_failure(self):
        self.gate('1.3', PASS_GATE.replace('tests=3', 'tests=4'))
        self.gate('1.4', '#!/bin/sh\necho "PAXEER_X_GATE tests=1 skipped=0"\nexit 1\n')
        self.commit()
        result = self.tool('release.sh', '1.1', '1.3')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.runs(), 2)
        result = self.tool('release.sh', '1.1', '1.4')
        self.assertEqual(result.returncode, 8, result.stderr)
        line = [l for l in result.stdout.splitlines() if l.startswith('release-evidence: ')][-1]
        self.assertEqual(load_private(Path(line.split()[1]))['result'], 'refused')

    def test_release_refuses_empty_scope_and_incomplete_coverage(self):
        empty = self.base / 'empty.txt'
        empty.write_text('# nothing\n')
        self.assertEqual(self.tool('release.sh').returncode, 2)
        self.assertEqual(self.tool('release.sh', empty).returncode, 2)
        self.assertEqual(self.tool('release.sh', '1.1', '9.9').returncode, 2)
        self.assertEqual(self.tool('release.sh', '1.1', '1.9').returncode, 3)
        self.assertEqual(self.tool('release.sh', '1.1', 'req.2').returncode, 3)
        self.assertEqual(self.tool('release.sh', 'req.7').returncode, 3)
        self.assertEqual(self.runs(), 0)
        self.assertEqual(list(self.evidence.iterdir()), [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
