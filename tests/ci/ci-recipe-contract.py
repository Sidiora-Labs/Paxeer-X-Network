#!/usr/bin/env python3
import copy
import json
from pathlib import Path
import re
import shlex
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = 'spec/paxeer-x/spec.kvx'
CHECK = 'tools/bringup/check-live.sh'
CANARY = '.github/workflows/runner-canary.yml'
ARCHIVED = 'spec/paxeer-x-ci/spec.kvx#task.2.1'
EFFECTIVE = 'spec/paxeer-x-bringup/spec.kvx#task.4.13'


def parse_spec(text):
    records = {}
    section = None
    for number, raw in enumerate(text.splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith('#'):
            continue
        if line.startswith('[') and line.endswith(']'):
            section = line[1:-1]
            if section in records:
                raise ValueError(f'duplicate section at {number}')
            records[section] = {}
            continue
        key, separator, value = line.partition('=')
        key = key.strip()
        if not separator or section is None or key in records[section]:
            raise ValueError(f'invalid or duplicate field at {number}')
        records[section][key] = json.loads(value.strip())
    return records


def shell_function(source, name):
    match = re.search(r'^' + re.escape(name) + r'\(\) \{\n.*?^\}\n', source, re.M | re.S)
    if match is None:
        raise ValueError('missing production function ' + name)
    return match.group()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def validate_contract(records, sources):
    resolution = records['resolution.63']
    require(resolution['pending_task_source'] == SPEC, 'pending task authority')
    require(resolution['effective_source'] == EFFECTIVE, 'effective bringup reference')
    require(resolution['current_contract'] == SPEC + '#capability.109.2.1', 'current canary reference')
    require(resolution['supersedes'] == [f'spec/paxeer-x-ci/spec.kvx#task.{task}' for task in ('1.1', '1.2', '2.1')], 'precise supersession references')
    require('never executable pending tasks' in resolution['archival_policy'], 'archival policy')
    require(resolution['source_authorities'] == ['docker/flyci-runner/Dockerfile', 'docker/flyci-controller/Dockerfile', CHECK + '#check_ci'], 'source authorities')
    for kind, number in (('runner', '1.1'), ('controller', '1.2')):
        capability = records['capability.109.' + number]
        dockerfile = f'docker/flyci-{kind}/Dockerfile'
        command = f'docker build -f {dockerfile} -t paxeer-ci-{kind}:verify .'
        require(capability['build_context'] == '.', 'repository-root build context')
        require(capability['dockerfile'] == dockerfile, 'canonical Dockerfile')
        require(capability['build_command'] == command, 'canonical image command')
        require(command in capability['verification_contract'], 'image build missing from retained gate')
        require(dockerfile in capability['touches'], 'Dockerfile touch authority')
        require(capability['recipe_resolution'] == 'resolution.63', 'image supersession')
        require(f'dockerfile = "{dockerfile}"' in sources[f'tools/flyci/{kind}/fly.toml'], 'Fly image recipe mismatch')
        copied = []
        for line in sources[dockerfile].splitlines():
            if not line.startswith('COPY '):
                continue
            words = shlex.split(line)[1:]
            if any(word.startswith('--from=') for word in words):
                continue
            words = [word for word in words if not word.startswith('--')]
            require(len(words) >= 2, 'malformed COPY')
            for path in words[:-1]:
                require(not Path(path).is_absolute() and '..' not in Path(path).parts, 'COPY escapes root')
                require((ROOT / path).exists(), 'missing root-context COPY input ' + path)
                copied.append(path)
        require(bool(copied), 'missing COPY contract')
    canary = records['capability.109.2.1']
    require(canary['verification_contract'] == 'timeout 30m tools/bringup/check-live.sh ci', 'current canary command')
    require(canary['recipe_authority'] == CHECK + '#check_ci', 'check_ci authority')
    require(canary['supersedes'] == ARCHIVED and canary['effective_source'] == EFFECTIVE, 'canary supersession')
    for name, fields in records.items():
        executable = [value for key, value in fields.items() if key in ('verify_cmd', 'verification_contract', 'build_command') and isinstance(value, str)]
        for command in executable:
            require('runnerName' not in command, 'obsolete runner API field in ' + name)
            require(not re.search(r'docker build[^\n]*\s+tools/flyci/(?:runner|controller)(?:[\s\x27\x22;&]|$)', command), 'obsolete build context in ' + name)
            require('tools/flyci/runner/Dockerfile' not in command and 'tools/flyci/controller/Dockerfile' not in command, 'obsolete Dockerfile in ' + name)
    task = records['task.25.4']
    require(task['verify_cmd'] == 'timeout 10m python3 tests/ci/ci-recipe-contract.py', 'source gate command')
    require(task['source_only'] is True and task['recipe_resolution'] == 'resolution.63', 'source-only qualification')
    source = sources[CHECK]
    check = shell_function(source, 'check_ci')
    for token in ('wait=1500', '-F return_run_details=true', 'ci_dispatch_id', '-f ref="$branch"', 'rev-parse --verify HEAD', 'actions/runs/$run/jobs?per_page=100&filter=latest', '--paginate --slurp', 'ci_canary_evidence "$run" "$branch" "$candidate" "$workflow"', 'gh run watch "$run"', 'gh run cancel "$run"', 'gh variable get CI_LINUX_RUNNER'):
        require(token in check, 'missing canary contract ' + token)
    require('gh run list' not in check and 'runnerName' not in check, 'unbound or obsolete run discovery')
    require('gh variable set' not in check and 'gh variable delete' not in check, 'canary changes routing')
    require('workflow_run_id' in shell_function(source, 'ci_dispatch_id'), 'dispatch identity field')
    evidence = shell_function(source, 'ci_canary_evidence')
    for field in ('runner_name', 'runner_id', 'run_id', 'head_sha', 'head_branch', 'workflow_dispatch'):
        require(field in evidence, 'missing REST binding ' + field)
    require('workflow_dispatch:' in sources[CANARY] and 'vars.CI_LINUX_RUNNER' in sources[CANARY], 'declared canary source')


class CIRecipeContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.records = parse_spec((ROOT / SPEC).read_text())
        paths = [CHECK, CANARY, 'docker/flyci-runner/Dockerfile', 'docker/flyci-controller/Dockerfile', 'tools/flyci/runner/fly.toml', 'tools/flyci/controller/fly.toml']
        cls.sources = {path: (ROOT / path).read_text() for path in paths}
        cls.dispatch = shell_function(cls.sources[CHECK], 'ci_dispatch_id')
        cls.evidence = shell_function(cls.sources[CHECK], 'ci_canary_evidence')

    def parser(self, function, raw, args=()):
        source = self.dispatch if function == 'ci_dispatch_id' else self.evidence
        return subprocess.run(['bash', '-c', source + '\n' + function + ' "$@"', 'ci-recipe', *args], input=raw, text=True, capture_output=True, timeout=10)

    def documents(self):
        run = {'id': 1, 'event': 'workflow_dispatch', 'head_branch': 'main', 'head_sha': 'a' * 40, 'path': CANARY, 'status': 'completed', 'conclusion': 'success'}
        job = {'id': 399444496, 'run_id': 1, 'status': 'completed', 'conclusion': 'success', 'runner_id': 1, 'runner_name': 'fly-399444496'}
        return run, [{'total_count': 1, 'jobs': [job]}]

    def evidence_result(self, run, pages):
        return self.parser('ci_canary_evidence', json.dumps(run) + '\n' + json.dumps(pages), ('1', 'main', 'a' * 40, 'runner-canary.yml'))

    def test_current_source_contract(self):
        validate_contract(self.records, self.sources)

    def test_obsolete_build_context_and_api_commands_refused(self):
        for section, key, value in (
            ('capability.109.1.1', 'build_context', 'tools/flyci/runner'),
            ('capability.109.1.2', 'dockerfile', 'tools/flyci/controller/Dockerfile'),
            ('capability.109.1.1', 'build_command', 'docker build tools/flyci/runner'),
            ('task.25.4', 'verify_cmd', 'gh run view 1 --json jobs --jq .jobs[].runnerName'),
        ):
            with self.subTest(section=section, key=key):
                records = copy.deepcopy(self.records)
                records[section][key] = value
                with self.assertRaises(ValueError):
                    validate_contract(records, self.sources)

    def test_supersession_and_pending_authority_refused(self):
        for key, value in (('effective_source', ARCHIVED), ('supersedes', [ARCHIVED]), ('pending_task_source', 'spec/paxeer-x-ci/spec.kvx'), ('current_contract', EFFECTIVE), ('archival_policy', 'Run the archived tasks')):
            with self.subTest(key=key):
                records = copy.deepcopy(self.records)
                records['resolution.63'][key] = value
                with self.assertRaises(ValueError):
                    validate_contract(records, self.sources)

    def test_missing_source_and_copy_inputs_refused(self):
        for path, value in (('docker/flyci-runner/Dockerfile', 'COPY tools/flyci/absent-contract-input /runner\n'), ('docker/flyci-controller/Dockerfile', 'COPY ../go.mod /src\n'), ('tools/flyci/controller/fly.toml', 'dockerfile = "tools/flyci/controller/Dockerfile"')):
            with self.subTest(path=path):
                sources = dict(self.sources)
                sources[path] = value
                with self.assertRaises(ValueError):
                    validate_contract(self.records, sources)

    def test_production_dispatch_response_parser(self):
        response = {'workflow_run_id': 1, 'run_url': 'https://api.github.com/repos/octo-org/octo-repo/actions/runs/1', 'html_url': 'https://github.com/octo-org/octo-repo/actions/runs/1'}
        result = self.parser('ci_dispatch_id', json.dumps(response))
        self.assertEqual((result.returncode, result.stdout), (0, '1\n'))
        for raw in ('', '{', '{}', 'null', '[]', '{"workflow_run_id":true}', '{"workflow_run_id":0}', '{"workflow_run_id":-1}', '{"workflow_run_id":"1"}'):
            with self.subTest(raw=raw):
                self.assertNotEqual(self.parser('ci_dispatch_id', raw).returncode, 0)

    def test_production_completed_run_and_paginated_jobs(self):
        run, pages = self.documents()
        result = self.evidence_result(run, pages)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'completed success ' + 'a' * 40 + ' fly-399444496 1\n')
        pages[0]['total_count'] = 2
        other = copy.deepcopy(pages[0]['jobs'][0])
        other['id'] += 1
        pages.append({'total_count': 2, 'jobs': [other]})
        self.assertEqual(self.evidence_result(run, pages).returncode, 0)

    def test_foreign_run_branch_source_and_workflow_refused(self):
        for key, value in (('id', 2), ('id', True), ('head_branch', 'another'), ('head_sha', 'b' * 40), ('path', '.github/workflows/another.yml'), ('event', 'push'), ('status', 'in_progress'), ('conclusion', 'failure')):
            with self.subTest(key=key):
                run, pages = self.documents()
                run[key] = value
                self.assertNotEqual(self.evidence_result(run, pages).returncode, 0)

    def test_unknown_runner_and_foreign_job_refused(self):
        for key, value in (('runner_id', None), ('runner_id', True), ('runner_id', 0), ('runner_name', None), ('runner_name', 'Hosted Agent'), ('runner_name', 'fly-1\nother'), ('run_id', 2), ('run_id', True), ('status', 'in_progress'), ('conclusion', 'failure')):
            with self.subTest(key=key):
                run, pages = self.documents()
                pages[0]['jobs'][0][key] = value
                self.assertNotEqual(self.evidence_result(run, pages).returncode, 0)
        run, pages = self.documents()
        pages[0]['jobs'][0]['runnerName'] = pages[0]['jobs'][0].pop('runner_name')
        self.assertNotEqual(self.evidence_result(run, pages).returncode, 0)

    def test_truncated_duplicate_and_missing_pages_refused(self):
        for variant in ('missing', 'empty', 'truncated', 'duplicate', 'count_type', 'inconsistent'):
            with self.subTest(variant=variant):
                run, pages = self.documents()
                if variant == 'missing': pages = None
                elif variant == 'empty': pages = []
                elif variant == 'truncated': pages[0]['total_count'] = 2
                elif variant == 'duplicate': pages.append(copy.deepcopy(pages[0]))
                elif variant == 'count_type': pages[0]['total_count'] = True
                else: pages.append({'total_count': 2, 'jobs': []})
                self.assertNotEqual(self.evidence_result(run, pages).returncode, 0)
        for raw in ('', '{}', '{}\n{', 'null\n[]', '{}\n[]\n{}'):
            self.assertNotEqual(self.parser('ci_canary_evidence', raw, ('1', 'main', 'a' * 40, 'runner-canary.yml')).returncode, 0)

    def test_unbound_run_lookup_and_routing_mutation_refused(self):
        for token, replacement in (('-F return_run_details=true', ''), ('--paginate --slurp', ''), ('gh run watch "$run"', 'gh run list'), ('gh variable get CI_LINUX_RUNNER', 'gh variable set CI_LINUX_RUNNER')):
            with self.subTest(token=token):
                sources = dict(self.sources)
                sources[CHECK] = sources[CHECK].replace(token, replacement)
                with self.assertRaises(ValueError):
                    validate_contract(self.records, sources)

    def test_duplicate_spec_identity_refused(self):
        for raw in ('[task.25.4]\nx = 1\nx = 2\n', '[task.25.4]\n[task.25.4]\n', 'x = 1\n'):
            with self.assertRaises(ValueError):
                parse_spec(raw)


if __name__ == '__main__':
    unittest.main(verbosity=2)
