import argparse
import base64
import copy
import contextlib
import hashlib
import os
import pathlib
import shutil
import sys
import subprocess
import tempfile
import time
import urllib.request
import urllib.parse
from ci_tools import verified_executable
from controller import GitHubChecks, admit
from confinement import verify as confinement
from policy import Rejected, admission, canonical, digest, require
from provenance import Tool, make_statement, read_document, write_document
from reproduce import reproduce
from sbom import generate, inventory_projection
from scanner import scan


def run_failure_fixture(config, output):
    require(config.get('fixture_write_authorized') is True, 'fixture branch writes not authorized')
    require(config['branch'].startswith('qualification/') and config['repository'] == config['approved_fixture_repository'], 'fixture repository or branch not independently approved')
    token = os.environ.get('GITHUB_FIXTURE_TOKEN')
    require(type(token) is str and token, 'isolated fixture repository token unavailable')
    authority = GitHubChecks(config['repository'], token)
    relative = pathlib.PurePosixPath(config['failing_unit_source'])
    require(not relative.is_absolute() and '..' not in relative.parts and str(relative).endswith('_test.go'), 'fixture must name an existing Go unit test')
    base = authority.get('/git/ref/heads/' + urllib.parse.quote(config['base_branch'], safe=''))
    commit = base['object']['sha']
    require(commit == config['approved_base_commit'], 'fixture base source changed')
    unit = authority.get('/contents/' + urllib.parse.quote(str(relative), safe='/') + '?ref=' + commit)
    require(unit.get('type') == 'file' and unit.get('encoding') == 'base64', 'missing real fixture unit source')
    source = base64.b64decode(unit['content'])
    require(b'"testing"' in source and b'TestReleaseGateIntentionalFailure' not in source, 'unsupported or previously mutated unit fixture')
    addition = b'\nfunc TestReleaseGateIntentionalFailure(t *testing.T) { t.Fatal("release publication qualification deliberate unit failure") }\n'
    def write(method, endpoint, value):
        data = canonical(value)
        request = urllib.request.Request('https://api.github.com/repos/' + config['repository'] + endpoint, data=data, method=method, headers={'Accept': 'application/vnd.github+json', 'Authorization': 'Bearer ' + token, 'X-GitHub-Api-Version': '2022-11-28', 'Content-Type': 'application/json', 'User-Agent': 'KindleLaunch-release-fixture'})
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, hdrs, newurl):
                return None
        with urllib.request.build_opener(NoRedirect).open(request, timeout=15) as response:
            from policy import load
            require(response.status in (200, 201), 'fixture authority rejected write')
            return load(response.read(4 * 1024 * 1024 + 1))
    write('POST', '/git/refs', {'ref': 'refs/heads/' + config['branch'], 'sha': commit})
    changed = write('PUT', '/contents/' + urllib.parse.quote(str(relative), safe='/'), {'message': 'Exercise publication admission with a deliberate failing unit fixture', 'branch': config['branch'], 'sha': unit['sha'], 'content': base64.b64encode(source + addition).decode()})
    candidate = changed['commit']['sha']
    deadline = time.monotonic() + 600
    while time.monotonic() < deadline:
        response = authority.get('/actions/runs?event=push&head_sha=' + candidate + '&per_page=100')
        matches = [run for run in response.get('workflow_runs', []) if run.get('head_sha') == candidate and run.get('head_branch') == config['branch'] and run.get('path') == config['workflow_path']]
        require(len(matches) <= 1, 'ambiguous fixture workflow execution')
        if matches and matches[0].get('status') == 'completed':
            run = matches[0]
            require(run.get('conclusion') == 'failure', 'deliberate failed unit did not fail fixture pipeline')
            jobs = authority.get('/actions/runs/' + str(run['id']) + '/jobs?per_page=100')
            require(jobs.get('total_count', 101) <= 100, 'fixture job inventory too large')
            failed = [job for job in jobs['jobs'] if job.get('name') == config['failed_unit_job_name']]
            publication = [job for job in jobs['jobs'] if job.get('name') == config['publication_job_name']]
            require(len(failed) == 1 and len(publication) == 1, 'missing exact fixture required jobs')
            receipt = {**config, 'run_id': str(run['id']), 'commit': candidate, 'failed_unit_job_id': str(failed[0]['id']), 'publication_job_id': str(publication[0]['id'])}
            require(failed[0].get('conclusion') == 'failure' and publication[0].get('conclusion') == 'skipped', 'publication survived an actual failed unit job')
            write_document(output, receipt)
            return receipt
        time.sleep(5)
    raise Rejected('fixture pipeline did not complete within600seconds; branch and commit retained for diagnosis')


def failed_pipeline(config):
    authority = GitHubChecks(config['repository'], os.environ.get('GITHUB_TOKEN'))
    run_id = config['run_id']
    require(type(run_id) is str and run_id.isdecimal(), 'invalid failing fixture run')
    run = authority.get('/actions/runs/' + run_id)
    require(run.get('head_sha') == config['commit'] and run.get('head_branch') == config['branch'] and run.get('head_repository', {}).get('full_name') == config['repository'], 'failing fixture branch mismatch')
    require(config['branch'].startswith('qualification/') and run.get('event') == 'push' and run.get('status') == 'completed' and run.get('conclusion') == 'failure', 'fixture was not a real failed pipeline')
    response = authority.get('/actions/runs/' + run_id + '/jobs?per_page=100')
    require(type(response.get('jobs')) is list and response.get('total_count', 101) <= 100, 'fixture job inventory exceeds bound')
    jobs = {str(job['id']): job for job in response['jobs']}
    failed, publish = jobs.get(config['failed_unit_job_id']), jobs.get(config['publication_job_id'])
    require(failed is not None and failed.get('conclusion') == 'failure' and failed.get('name') == config['failed_unit_job_name'], 'unit failure not observed')
    require(publish is not None and publish.get('conclusion') == 'skipped' and publish.get('name') == config['publication_job_name'], 'publication was not skipped')
    annotation = authority.get('/check-runs/' + str(failed['check_run_url'].rsplit('/', 1)[1]) + '/annotations?per_page=100')
    require(type(annotation) is list and any(item.get('path') == config['failing_unit_source'] and item.get('annotation_level') == 'failure' for item in annotation), 'missing actual unit-source failure annotation')
    return {'run_id': run_id, 'commit': config['commit'], 'failed_unit_job_id': str(failed['id']), 'publication_job_id': str(publish['id']), 'publication_result': 'skipped'}


def tool_tamper(pin):
    tool = Tool.parse(pin)
    executable = pathlib.Path(verified_executable(tool.path, tool.sha256))
    with tempfile.TemporaryDirectory(prefix='tool-checksum-negative-') as temporary:
        copy_path = pathlib.Path(temporary) / executable.name
        shutil.copy2(executable, copy_path)
        before = subprocess.check_output([str(copy_path), '--version'], timeout=10)
        with copy_path.open('ab') as target:
            target.write(b'\nchecksum-negative-same-version\n')
        after = subprocess.check_output([str(copy_path), '--version'], timeout=10)
        require(before == after, 'tamper fixture changed tool version output')
        try:
            verified_executable(copy_path, tool.sha256)
        except Rejected as error:
            require('checksum' in str(error), 'unexpected tamper rejection')
            return {'version_sha256': digest(before), 'rejected': True, 'reason': str(error)}
        raise Rejected('modified tool was accepted')


def exception_expiry(document, controller, release_path, bundle_path):
    high = [finding for finding in document['release']['vulnerabilities'] if finding['severity'] == 'high']
    require(high, 'missing real independently approved high finding for expiry scenario')
    for finding in high:
        exception = finding['exception']
        try:
            admit(controller, release_path, bundle_path, exception['expires'])
        except Rejected as error:
            require(exception['id'] in str(error), 'expiry did not identify exact exception: ' + exception['id'])
            return {'exception_id': exception['id'], 'clock': exception['expires'], 'rejected': True, 'reason': str(error)}
    raise Rejected('expired exception was accepted')


def prepare(config, root, output):
    output = pathlib.Path(output)
    require(not output.exists(), 'release preparation output exists')
    output.mkdir(parents=True)
    builds, _ = read_document(config['build_receipt_path'], config['build_receipt_sha256'])
    require(builds.get('reproducible') is True and len(builds.get('builds', [])) == 2 and builds['builds'][0]['binary_sha256'] == builds['builds'][1]['binary_sha256'], 'missing paired clean build proof')
    expected = config['controller']['expected']
    require(builds['source'] == expected['source'] and builds['binary_sha256'] == config['controller']['approved_binary_sha256'], 'paired build source mismatch')
    now = int(time.time())
    sbom = generate(config['sbom'], expected['image'], pathlib.Path(root) / 'launchpad/client', output / 'sbom')
    documents = []
    for artifact in sbom['artifacts']:
        document, _ = read_document(artifact['path'], artifact['sha256'])
        documents.append({'scope': artifact['scope'], 'sha256': digest(canonical(document)), 'document': document})
    require(digest(canonical(inventory_projection([item['document'] for item in documents]))) == expected['sbom'], 'SBOM differs from trusted expected inventory')
    scanned = scan(config['scan'], expected['image'], root, output / 'scan', now)
    build = {**builds['builds'][0], 'image': expected['image']}
    statement = make_statement(expected, build, expected['materials'], now)
    release = {key: expected[key] for key in ('source', 'workflow', 'image', 'sbom', 'repository', 'builder', 'run_id', 'ref')}
    release.update(issued=now, expires=now + 3600, event='push', fork=False, scanner_status=scanned['scanner_status'], vulnerabilities=scanned['vulnerabilities'], checks=config['checks'])
    document = {'schema_version': 2, 'release': release, 'provenance': statement, 'sboms': documents, 'scan': scanned, 'build': build}
    admission(release, expected, now)
    write_document(output / 'release.json', document)
    return {'release_path': str(output / 'release.json'), 'source': expected['source'], 'sbom': expected['sbom'], 'image': expected['image']}


def verify_all(config, output):
    output = pathlib.Path(output)
    require(not output.exists(), 'phase2 evidence output already exists')
    output.mkdir(parents=True)
    started = int(time.time())
    scenarios = {}
    try:
        scenarios['failed_pipeline'] = failed_pipeline(config['failed_pipeline'])
        scenarios['tool_tamper'] = tool_tamper(config['tamper_tool'])
        builds, _ = read_document(config['build_receipt_path'], config['build_receipt_sha256'])
        require(builds.get('reproducible') is True and len(builds['builds']) == 2, 'missing real clean build pair')
        first, second = builds['builds']
        require(first['worker'] != second['worker'] and first['inputs'] == second['inputs'] and first['binary_sha256'] == second['binary_sha256'], 'clean build comparison failed')
        require(builds['source'] == config['controller']['expected']['source'] and first['binary_sha256'] == config['controller']['approved_binary_sha256'], 'reproducibility candidate mismatch')
        scenarios['reproducibility'] = {'binary_sha256': first['binary_sha256'], 'workers': [first['worker'], second['worker']], 'source': builds['source']}
        require(config['confinement']['approved_binary_sha256'] == config['controller']['approved_binary_sha256'], 'confinement uses different application binary')
        scenarios['confinement'] = confinement(config['confinement'], config['confinement']['image'])
        admitted, document = admit(config['controller'], config['release_path'], config['bundle_path'], started)
        scenarios['admission'] = admitted
        changed = copy.deepcopy(config['controller'])
        image = changed['expected']['image']
        changed['expected']['image'] = image.rsplit('@sha256:', 1)[0] + '@sha256:' + hashlib.sha256(image.encode()).hexdigest()
        try:
            admit(changed, config['release_path'], config['bundle_path'], started)
        except Rejected as error:
            require('mismatch' in str(error), 'unrelated candidate replacement rejection')
            scenarios['digest_replacement'] = {'rejected': True, 'reason': str(error)}
        else:
            raise Rejected('replaced image digest was admitted')
        scenarios['exception_expiry'] = exception_expiry(document, config['controller'], config['release_path'], config['bundle_path'])
    except Exception as error:
        write_document(output / 'phase-2.json', {'schema_version': 2, 'qualified': False, 'started': started, 'ended': int(time.time()), 'completed_scenarios': scenarios, 'failure': {'type': type(error).__name__, 'observed': str(error)}})
        raise
    result = {'schema_version': 2, 'qualified': True, 'started': started, 'ended': int(time.time()), 'source': config['controller']['expected']['source'], 'image': config['controller']['expected']['image'], 'scenarios': scenarios}
    write_document(output / 'phase-2.json', result)
    return result


def verify_adversarial(config, output):
    output = pathlib.Path(output)
    require(not output.exists(), 'phase3 evidence output already exists')
    output.mkdir(parents=True)
    started = int(time.time())
    scenarios = {}
    def record():
        write_document(output / 'phase-3.json', {'schema_version': 3, 'qualified': False, 'started': started, 'source': config['controller']['expected']['source'], 'image': config['controller']['expected']['image'], 'completed_scenarios': scenarios})
    record()
    try:
        baseline = verify_all(config, output / 'baseline')
        scenarios.update(baseline['scenarios'])
        record()
        controller = config['controller']
        tool = Tool.parse(config['tamper_tool'])
        from ci_tools import execution_snapshot
        with tempfile.TemporaryDirectory(prefix='tool-path-replacement-') as temporary:
            copied = pathlib.Path(temporary) / 'tool'
            shutil.copy2(tool.path, copied)
            with execution_snapshot(copied, tool.sha256) as (executable, descriptor):
                before = subprocess.check_output([executable, '--version'], pass_fds=(descriptor,), timeout=10)
                with copied.open('ab') as target:
                    target.write(b'changed after verified snapshot')
                after = subprocess.check_output([executable, '--version'], pass_fds=(descriptor,), timeout=10)
                require(before == after, 'verified execution changed after path replacement')
            try:
                Tool(str(copied), tool.sha256).run(['--version'], timeout=10)
            except Rejected as error:
                require('checksum' in str(error), 'unrelated tool replacement rejection')
                scenarios['immutable_execution'] = {'rejected_changed_path': True, 'executed_sha256': tool.sha256, 'output_sha256': digest(after)}
            else:
                raise Rejected('changed tool path executed')
        record()
        from controller import verify_federated
        _, original = read_document(config['release_path'])
        with tempfile.TemporaryDirectory(prefix='signed-release-tamper-') as temporary:
            tampered = pathlib.Path(temporary) / 'release.json'
            tampered.write_bytes(original + b' ')
            try:
                verify_federated(controller['cosign'], tampered, config['bundle_path'])
            except Rejected as error:
                scenarios['signed_byte_tamper'] = {'rejected': True, 'reason': str(error)}
            else:
                raise Rejected('changed signed bytes accepted')
        revoked = copy.deepcopy(controller['cosign'])
        revoked['certificate_identity'] += '-revoked'
        try:
            verify_federated(revoked, config['release_path'], config['bundle_path'])
        except Rejected as error:
            scenarios['revoked_signing_identity'] = {'rejected': True, 'reason': str(error)}
        else:
            raise Rejected('revoked signing identity accepted')
        require(config.get('rotation_controller') is not None and config.get('rotation_release_path') and config.get('rotation_bundle_path'), 'missing independently reviewed signing rotation authority')
        rotation = config['rotation_controller']
        for field in ('source', 'image', 'sbom'):
            require(rotation['expected'][field] == controller['expected'][field], 'signing rotation changes released artifact: ' + field)
        require(rotation['cosign']['certificate_identity'] != controller['cosign']['certificate_identity'], 'signing authority was not rotated')
        rotated, _ = admit(rotation, config['rotation_release_path'], config['rotation_bundle_path'], int(time.time()))
        try:
            verify_federated(rotation['cosign'], config['release_path'], config['bundle_path'])
        except Rejected as error:
            scenarios['signing_rotation'] = {'new_authority_admitted': rotated, 'old_authority_rejected': True, 'reason': str(error)}
        else:
            raise Rejected('previous signing authority survives rotation')
        record()
        scenarios['promotion_recovery'] = promotion_recovery(config, output / 'promotion')
    except Exception as error:
        write_document(output / 'phase-3.json', {'schema_version': 3, 'qualified': False, 'started': started, 'ended': int(time.time()), 'completed_scenarios': scenarios, 'failure': {'type': type(error).__name__, 'observed': str(error)}})
        raise
    result = {'schema_version': 3, 'qualified': True, 'started': started, 'ended': int(time.time()), 'source': config['controller']['expected']['source'], 'image': config['controller']['expected']['image'], 'scenarios': scenarios}
    write_document(output / 'phase-3.json', result)
    return result


@contextlib.contextmanager
def promotion_python(config):
    if config.get('python') is not None:
        from ci_tools import execution_snapshot
        pin = Tool.parse(config['python'])
        with execution_snapshot(pin.path, pin.sha256) as (executable, descriptor):
            yield executable, (descriptor,)
    else:
        yield sys.executable, ()


def promotion_recovery(config, output):
    import signal
    from controller import promote, promotion_lock
    require(config.get('promotion_write_authorized') is True, 'isolated registry promotion writes not authorized')
    require(config['controller']['expected']['image'].startswith(config['approved_fixture_registry_prefix'] + '@sha256:'), 'promotion outside independently approved fixture registry')
    directory = pathlib.Path(output)
    directory.mkdir(parents=True)
    result_path = directory / 'promotion.json'
    authority = directory / 'authority.json'
    write_document(authority, config['controller'])
    command = [sys.executable, str(pathlib.Path(__file__).with_name('controller.py')), 'promote', '--authority', str(authority), '--authority-sha256', digest(authority.read_bytes()), '--release', config['release_path'], '--bundle', config['bundle_path'], '--layout', config['promotion_layout'], '--output', str(result_path)]
    state_path = result_path.with_name(result_path.name + '.state.json')
    with promotion_python(config) as (python, descriptors), (directory / 'interrupted.log').open('wb') as log:
        command[0] = python
        process = subprocess.Popen(command, pass_fds=descriptors, stdout=log, stderr=log, start_new_session=True)
        interrupted = False
        deadline = time.monotonic() + 600
        try:
            while process.poll() is None and time.monotonic() < deadline:
                if state_path.is_file():
                    state, _ = read_document(state_path)
                    if 'copied' in state['completed'] and not state['promoted']:
                        try:
                            with promotion_lock(result_path):
                                raise Rejected('parallel controller did not retain promotion lease')
                        except Rejected as error:
                            require('duplicate promotion' in str(error), 'unrelated competing controller failure')
                        os.killpg(process.pid, signal.SIGTERM)
                        interrupted = True
                        break
                time.sleep(0.05)
            require(interrupted, 'could not interrupt actual promotion after durable OCI copy')
            code = process.wait(timeout=30)
            require(code != 0, 'interrupted controller reported success')
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
    state, _ = read_document(state_path)
    require(state['promoted'] is False and 'copied' in state['completed'], 'interruption lost durable incomplete promotion state')
    recovered = promote(config['controller'], config['release_path'], config['bundle_path'], config['promotion_layout'], result_path)
    require(recovered['promoted'] is True and recovered['registry_attachments_verified'] is True, 'recovery did not verify real registry attachments')
    completed, _ = read_document(state_path)
    duplicate = promote(config['controller'], config['release_path'], config['bundle_path'], config['promotion_layout'], result_path)
    require(duplicate['promoted'] is True and read_document(state_path)[0] == completed, 'duplicate promotion changed completed recovery state')
    return {'interrupted_exit_code': code, 'log_path': str(directory / 'interrupted.log'), 'resumed': recovered, 'duplicate_idempotent': True, 'recovery_state_sha256': digest(state_path.read_bytes())}


RELEASE_STAGES = ('toolchain', 'failed_pipeline', 'tool_tamper', 'reproducibility', 'preparation', 'signing', 'confinement', 'admission', 'digest_replacement', 'exception_expiry', 'signing_rotation', 'promotion_recovery')
RELEASE_REQUIREMENTS = {
    'D15_015': ('failed_pipeline', 'admission', 'promotion_recovery'),
    'D15_016': ('toolchain', 'tool_tamper'),
    'D15_017': ('reproducibility',),
    'D15_018': ('confinement',),
    'D15_019': ('preparation', 'signing', 'admission', 'digest_replacement', 'signing_rotation', 'promotion_recovery'),
    'D15_020': ('preparation', 'exception_expiry', 'admission'),
}


def candidate_snapshot(root):
    import importlib.util
    root = pathlib.Path(root).resolve()
    path = root / 'tools' / 'launchpad' / 'production' / 'qualify.py'
    require(path.is_file() and not path.is_symlink(), 'current-source authority is absent')
    definition = importlib.util.spec_from_file_location('supplychain_current_source_authority', path)
    module = importlib.util.module_from_spec(definition)
    definition.loader.exec_module(module)
    return module.snapshot(root)


def finite_number(value):
    import math
    return type(value) in (int, float) and math.isfinite(value)


def nearest_rank(values, percentile):
    import math
    require(type(values) is list and values and all(finite_number(value) and value >= 0 for value in values), 'empty or invalid latency observations')
    require(type(percentile) is int and 1 <= percentile <= 100, 'invalid latency percentile')
    return sorted(values)[max(0, math.ceil(len(values) * percentile / 100) - 1)]


def validate_release_measurement(config, root):
    from ci_tools import read_pins
    from provenance import source_manifest
    from policy import sha
    require(type(config) is dict and config.get('schema_version') == 4, 'unsupported release measurement authority')
    require(sha(config.get('candidate_sha256')), 'missing approved current-candidate identity')
    require(os.environ.get('QUALIFICATION_CANDIDATE_SHA256') == config['candidate_sha256'], 'qualification candidate authority absent or mismatched')
    snapshot = candidate_snapshot(root)
    require(snapshot['sha256'] == config['candidate_sha256'], 'release measurement source differs from current candidate')
    phase2 = config['phase2']
    require(phase2.get('schema_version') == 2, 'release measurement requires existing v2 provider authority')
    build = phase2['build']
    expected = phase2['controller']['expected']
    inputs = source_manifest(root, build['source_paths'])
    require(inputs['sha256'] == build['source_sha256'] == expected['source'], 'release build/source authority mismatch')
    require(type(expected.get('materials')) is dict and all(expected['materials'].get(name) == identity for name, identity in inputs['files'].items()), 'provenance omits or changes actual source-file inputs')
    for image in (build['builder'], build['runtime'], build['buildkit_image']):
        require(expected['materials'].get(image) == image.rsplit('@sha256:', 1)[-1], 'provenance omits reviewed build image material')
    require(expected['materials'].get(build['buildx']['path']) == build['buildx']['sha256'], 'provenance omits reviewed buildx executable')
    require(build['service'] == expected['service'] and build['binary'] == expected['binary'] and build['platform'] == 'linux/' + expected['architecture'], 'release target authority mismatch')
    required = {'deploy/launchpad/Dockerfile.svc.dockerignore', 'launchpad/.github/workflows/ci.yml', 'launchpad/.github/workflows/release.yml', 'deploy/launchpad/Dockerfile.svc'}
    required.update(path.relative_to(root).as_posix() for path in (pathlib.Path(root) / 'services' / 'launchpad' / 'security' / 'supplychain').glob('*.py'))
    required.add('services/launchpad/security/supplychain/phase2.schema.json')
    require(required <= set(build['source_paths']), 'release source inputs omit owned supply-chain implementation')
    require(digest((pathlib.Path(root) / 'launchpad/.github/workflows/ci.yml').read_bytes()) == expected['workflow'], 'release workflow revision is not current')
    require(phase2['failed_pipeline']['approved_base_commit'] == expected['commit'], 'failed pipeline is not based on approved candidate commit')
    require(phase2['confinement']['approved_binary_sha256'] == phase2['controller']['approved_binary_sha256'], 'runtime/build binary authority mismatch')
    require(phase2['confinement']['image'] == config['approved_confinement_image'], 'confinement image authority mismatch')
    from policy import pinned_image
    pinned_image(config['approved_confinement_image'])
    toolchain = config['toolchain']
    pins = read_pins(toolchain['manifest'], toolchain['sha256'])
    require(toolchain.get('materialize_pnpm') is True, 'release authority must explicitly require managed executable pnpm bundle materialization')
    bundle = pins['pnpm']
    python = Tool.parse(config['python'])
    verified_executable(python.path, python.sha256)
    measured = config['measurement']
    require(type(measured) is dict and set(measured) == {'total_timeout_seconds', 'stage_timeouts_seconds', 'stage_budgets', 'admission_requests', 'admission_concurrency', 'admission_budget'}, 'incomplete measurement contract')
    require(type(measured['total_timeout_seconds']) is int and 1 <= measured['total_timeout_seconds'] <= 1800, 'release measurement time budget must be bounded by1800seconds')
    require(type(measured['stage_timeouts_seconds']) is dict and set(measured['stage_timeouts_seconds']) == set(RELEASE_STAGES), 'missing stage execution budgets')
    require(all(type(value) is int and 1 <= value <= measured['total_timeout_seconds'] for value in measured['stage_timeouts_seconds'].values()), 'invalid stage execution budget')
    require(type(measured['stage_budgets']) is dict and set(measured['stage_budgets']) == set(RELEASE_STAGES), 'missing reviewed stage measurement bounds')
    for name, budget in measured['stage_budgets'].items():
        require(type(budget) is dict and set(budget) == {'wall_ms_max', 'controller_cpu_ms_max', 'single_process_peak_rss_bytes_max', 'artifact_bytes_max'}, 'incomplete stage measurement bound: ' + name)
        require(all(finite_number(value) and value > 0 for value in budget.values()), 'invalid stage measurement bound: ' + name)
    require(type(measured['admission_requests']) is int and 1 <= measured['admission_requests'] <= 100 and type(measured['admission_concurrency']) is int and 1 <= measured['admission_concurrency'] <= min(8, measured['admission_requests']), 'unbounded admission workload')
    admission_budget = measured['admission_budget']
    require(type(admission_budget) is dict and set(admission_budget) == {'p95_ms_max', 'p99_ms_max', 'requests_per_second_min', 'failure_rate_max'}, 'incomplete admission capacity contract')
    require(all(finite_number(value) and value > 0 for key, value in admission_budget.items() if key != 'failure_rate_max') and admission_budget['failure_rate_max'] == 0, 'release admission workload requires zero failures and finite bounds')
    return snapshot, pins, bundle


def artifact_inventory(directory, excluded=()):
    directory = pathlib.Path(directory)
    files = {}
    total = 0
    for path in sorted(directory.rglob('*')):
        require(not path.is_symlink(), 'measurement artifact is indirect')
        if not path.is_file() or path.relative_to(directory).as_posix() in excluded:
            continue
        with path.open('rb') as source:
            identity = hashlib.file_digest(source, 'sha256').hexdigest()
        length = path.stat().st_size
        total += length
        files[path.relative_to(directory).as_posix()] = {'sha256': identity, 'bytes': length}
    return {'files': files, 'total_bytes': total}


def admission_capacity(config, release_path, bundle_path, output):
    from concurrent.futures import ThreadPoolExecutor, as_completed
    import threading
    measured = config['measurement']
    controller = config['phase2']['controller']
    samples = []
    lock = threading.Lock()
    started = time.monotonic_ns()
    raw = pathlib.Path(output) / 'admission-samples.jsonl'
    def request(number):
        before = time.monotonic_ns()
        stamp = time.time_ns()
        record = {'request_id': number, 'started_epoch_ns': stamp, 'started_monotonic_ns': before, 'source': controller['expected']['source'], 'image': controller['expected']['image']}
        try:
            result, _ = admit(controller, release_path, bundle_path, int(time.time()))
            require(result['admitted'] is True and result['image'] == record['image'] and result['source'] == record['source'], 'admission response candidate mismatch')
            record.update(admitted=True, release_sha256=result['release_sha256'])
        except Exception as error:
            record.update(admitted=False, failure={'type': type(error).__name__, 'observed': str(error)})
        record['ended_monotonic_ns'] = time.monotonic_ns()
        record['latency_ms'] = (record['ended_monotonic_ns'] - before) / 1000000
        with lock:
            with raw.open('ab') as target:
                target.write(canonical(record) + b'\n')
                target.flush()
                os.fsync(target.fileno())
        return record
    with ThreadPoolExecutor(max_workers=measured['admission_concurrency']) as pool:
        futures = [pool.submit(request, number) for number in range(measured['admission_requests'])]
        for future in as_completed(futures):
            samples.append(future.result())
    elapsed = time.monotonic_ns() - started
    require(elapsed > 0 and len(samples) == measured['admission_requests'], 'incomplete admission workload')
    failed = sum(sample['admitted'] is not True for sample in samples)
    metrics = {'requests': len(samples), 'concurrency': measured['admission_concurrency'], 'elapsed_ns': elapsed, 'failures': failed, 'failure_rate': failed / len(samples), 'requests_per_second': (len(samples) - failed) * 1000000000 / elapsed, 'p95_ms': nearest_rank([sample['latency_ms'] for sample in samples], 95), 'p99_ms': nearest_rank([sample['latency_ms'] for sample in samples], 99), 'raw_sha256': digest(raw.read_bytes())}
    write_document(pathlib.Path(output) / 'admission-capacity.json', metrics)
    budget = measured['admission_budget']
    require(metrics['failure_rate'] <= budget['failure_rate_max'], 'admission capacity included failed requests')
    require(metrics['p95_ms'] <= budget['p95_ms_max'] and metrics['p99_ms'] <= budget['p99_ms_max'] and metrics['requests_per_second'] >= budget['requests_per_second_min'], 'admission capacity budget exceeded')
    return metrics


def phase4_stage(config, root, output, name):
    require(name in RELEASE_STAGES, 'unknown release measurement stage')
    phase2 = copy.deepcopy(config['phase2'])
    measurement_root = pathlib.Path(output)
    stage = measurement_root / name
    stage.mkdir(parents=True, exist_ok=True)
    build_receipt = measurement_root / 'reproducibility' / 'builds' / 'reproducibility.json'
    release_path = measurement_root / 'preparation' / 'release' / 'release.json'
    bundle_path = measurement_root / 'signing' / 'bundle.json'
    if name == 'toolchain':
        from ci_tools import read_pins, verify_materialized_tools
        toolchain = config['toolchain']
        require(toolchain.get('materialize_pnpm') is True, 'managed executable pnpm materialization not authorized')
        return verify_materialized_tools(read_pins(toolchain['manifest'], toolchain['sha256']))
    if name == 'failed_pipeline':
        return failed_pipeline(phase2['failed_pipeline'])
    if name == 'tool_tamper':
        return tool_tamper(phase2['tamper_tool'])
    if name == 'reproducibility':
        result = reproduce(phase2['build'], root, stage / 'builds')
        require(result['binary_sha256'] == phase2['controller']['approved_binary_sha256'], 'fresh clean build differs from approved binary')
        return result
    if name == 'preparation':
        phase2.update(build_receipt_path=str(build_receipt), build_receipt_sha256=digest(build_receipt.read_bytes()))
        return prepare(phase2, root, stage / 'release')
    if name == 'signing':
        from controller import sign_federated
        return sign_federated(phase2['controller']['cosign'], release_path, bundle_path)
    if name == 'confinement':
        return confinement(phase2['confinement'], phase2['confinement']['image'])
    if name == 'admission':
        return admission_capacity(config, release_path, bundle_path, stage)
    if name == 'digest_replacement':
        changed = copy.deepcopy(phase2['controller'])
        image = changed['expected']['image']
        changed['expected']['image'] = image.rsplit('@sha256:', 1)[0] + '@sha256:' + digest(image.encode())
        try:
            admit(changed, release_path, bundle_path, int(time.time()))
        except Rejected as error:
            require('mismatch' in str(error), 'unrelated image replacement rejection')
            return {'rejected': True, 'reason': str(error)}
        raise Rejected('unapproved image digest was admitted')
    if name == 'exception_expiry':
        document, _ = read_document(release_path)
        return exception_expiry(document, phase2['controller'], release_path, bundle_path)
    if name == 'signing_rotation':
        from controller import verify_federated
        rotation = phase2['rotation_controller']
        for field in ('source', 'image', 'sbom'):
            require(rotation['expected'][field] == phase2['controller']['expected'][field], 'rotated authority changes candidate: ' + field)
        require(rotation['cosign']['certificate_identity'] != phase2['controller']['cosign']['certificate_identity'], 'signing identity was not rotated')
        result, _ = admit(rotation, phase2['rotation_release_path'], phase2['rotation_bundle_path'], int(time.time()))
        try:
            verify_federated(rotation['cosign'], release_path, bundle_path)
        except Rejected as error:
            return {'rotated_admission': result, 'prior_identity_rejected': True, 'reason': str(error)}
        raise Rejected('old signing identity survived rotation')
    phase2.update(release_path=str(release_path), bundle_path=str(bundle_path), python=config['python'])
    return promotion_recovery(phase2, stage / 'registry')


def phase4_worker(config, root, output, name):
    import resource
    import signal
    def interrupted(number, frame):
        raise InterruptedError('measurement worker interrupted by signal ' + str(number))
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    started = time.monotonic_ns()
    before_self = resource.getrusage(resource.RUSAGE_SELF)
    before_children = resource.getrusage(resource.RUSAGE_CHILDREN)
    stage = pathlib.Path(output) / name
    stage.mkdir(parents=True, exist_ok=True)
    result = {'stage': name, 'status': 'blocked', 'started_epoch_ns': time.time_ns(), 'candidate_sha256': config['candidate_sha256']}
    exit_code = 1
    try:
        result['result'] = phase4_stage(config, root, output, name)
        result['status'] = 'passed'
        exit_code = 0
    except Exception as error:
        result['failure'] = {'type': type(error).__name__, 'observed': str(error)}
    finally:
        after_self = resource.getrusage(resource.RUSAGE_SELF)
        after_children = resource.getrusage(resource.RUSAGE_CHILDREN)
        result['metrics'] = {'wall_ns': time.monotonic_ns() - started, 'controller_user_cpu_seconds': after_self.ru_utime - before_self.ru_utime, 'controller_system_cpu_seconds': after_self.ru_stime - before_self.ru_stime, 'child_user_cpu_seconds': after_children.ru_utime - before_children.ru_utime, 'child_system_cpu_seconds': after_children.ru_stime - before_children.ru_stime, 'controller_peak_rss_kib': after_self.ru_maxrss, 'child_peak_rss_kib': after_children.ru_maxrss, 'resource_scope': 'controller process and local child processes; excludes remote Docker/registry/GitHub providers'}
        write_document(stage / 'worker-result.json', result)
    return exit_code


def measured_release_stage(config, authority_path, root, output, name, remaining_seconds):
    import signal
    from ci_tools import execution_snapshot
    stage = pathlib.Path(output) / name
    stage.mkdir()
    pin = Tool.parse(config['python'])
    timeout = min(config['measurement']['stage_timeouts_seconds'][name], remaining_seconds)
    require(timeout > 0, 'release measurement deadline exhausted')
    started = time.monotonic_ns()
    timed_out = False
    with execution_snapshot(pin.path, pin.sha256) as (executable, descriptor), (stage / 'stdout.log').open('wb') as stdout, (stage / 'stderr.log').open('wb') as stderr:
        arguments = [str(pathlib.Path(__file__).resolve()), '_phase4-worker', '--authority', str(authority_path), '--authority-sha256', digest(pathlib.Path(authority_path).read_bytes()), '--source-root', str(root), '--output', str(output), '--stage', name]
        process = subprocess.Popen([executable, *arguments], pass_fds=(descriptor,), cwd=root, stdout=stdout, stderr=stderr, start_new_session=True)
        try:
            code = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(process.pid, signal.SIGTERM)
            try:
                code = process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                code = process.wait(timeout=2)
        except BaseException:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=2)
            raise
    elapsed = time.monotonic_ns() - started
    observation = {'stage': name, 'status': 'blocked', 'candidate_sha256': config['candidate_sha256'], 'python_sha256': pin.sha256, 'exit_code': code, 'timed_out': timed_out, 'started_monotonic_ns': started, 'ended_monotonic_ns': started + elapsed, 'wall_ms': elapsed / 1000000}
    worker_path = stage / 'worker-result.json'
    if worker_path.is_file():
        worker, _ = read_document(worker_path)
        require(worker['candidate_sha256'] == config['candidate_sha256'] and worker['stage'] == name, 'worker evidence candidate/stage mismatch')
        observation['worker'] = worker
        metrics = worker['metrics']
        observation['controller_cpu_ms'] = (metrics['controller_user_cpu_seconds'] + metrics['controller_system_cpu_seconds'] + metrics['child_user_cpu_seconds'] + metrics['child_system_cpu_seconds']) * 1000
        observation['single_process_peak_rss_bytes'] = max(metrics['controller_peak_rss_kib'], metrics['child_peak_rss_kib']) * 1024
        require(finite_number(observation['controller_cpu_ms']) and observation['controller_cpu_ms'] >= 0 and type(observation['single_process_peak_rss_bytes']) is int and observation['single_process_peak_rss_bytes'] > 0, 'invalid measured process resources')
        observation['worker_result_sha256'] = digest(worker_path.read_bytes())
    observation['artifacts'] = artifact_inventory(stage, excluded=('measurement.json',))
    budget = config['measurement']['stage_budgets'][name]
    checks = {'worker_success': code == 0 and not timed_out and observation.get('worker', {}).get('status') == 'passed', 'wall_time': observation['wall_ms'] <= budget['wall_ms_max'], 'cpu_time': 'controller_cpu_ms' in observation and observation['controller_cpu_ms'] <= budget['controller_cpu_ms_max'], 'single_process_memory': 'single_process_peak_rss_bytes' in observation and observation['single_process_peak_rss_bytes'] <= budget['single_process_peak_rss_bytes_max'], 'artifact_bytes': observation['artifacts']['total_bytes'] <= budget['artifact_bytes_max']}
    observation['budget_checks'] = checks
    observation['status'] = 'passed' if all(checks.values()) else 'blocked'
    write_document(stage / 'measurement.json', observation)
    return observation


def release_capacity(config, root, output, authority_sha256):
    output = pathlib.Path(output).resolve()
    root = pathlib.Path(root).resolve()
    require(not output.exists(), 'release measurement output already exists')
    require(not output.is_relative_to(root) or 'evidence' in output.relative_to(root).parts, 'release output must be outside current source or under an excluded evidence directory')
    output.mkdir(parents=True, mode=0o700)
    started_monotonic = time.monotonic()
    report = {'schema_version': 4, 'task': '4.46', 'qualified': False, 'qualification_scope': 'supplychain-release-measurements', 'global_capacity_accepted': False, 'final_shared_load_restore_gate': '9.1', 'started_epoch_ns': time.time_ns(), 'authority_sha256': authority_sha256, 'stages': {name: {'status': 'blocked', 'observed': 'not executed'} for name in RELEASE_STAGES}, 'requirements': {f'{name}.{clause}': {'status': 'blocked', 'required_stages': list(stages)} for name, stages in RELEASE_REQUIREMENTS.items() for clause in range(1, 6)}}
    def publish():
        for requirement, item in report['requirements'].items():
            item['status'] = 'passed' if all(report['stages'][stage]['status'] == 'passed' for stage in item['required_stages']) else 'blocked'
        write_document(output / 'phase-4.json', report)
    publish()
    try:
        snapshot, _, _ = validate_release_measurement(config, root)
        report.update(candidate_sha256=snapshot['sha256'], source=config['phase2']['controller']['expected']['source'], image=config['phase2']['controller']['expected']['image'])
        write_document(output / 'source-before.json', snapshot)
        with tempfile.TemporaryDirectory(prefix='release-measurement-authority-') as temporary:
            authority = pathlib.Path(temporary) / 'authority.json'
            write_document(authority, config)
            deadline = started_monotonic + config['measurement']['total_timeout_seconds']
            for name in RELEASE_STAGES:
                require(candidate_snapshot(root) == snapshot, 'source changed before release stage: ' + name)
                observation = measured_release_stage(config, authority, root, output, name, deadline - time.monotonic())
                report['stages'][name] = observation
                publish()
                require(observation['status'] == 'passed', 'release stage failed or exceeded its reviewed budget: ' + name)
        final = candidate_snapshot(root)
        write_document(output / 'source-after.json', final)
        require(final == snapshot, 'source changed during release measurements')
        report['raw_artifacts'] = artifact_inventory(output, excluded=('phase-4.json',))
        report['qualified'] = all(item['status'] == 'passed' for item in report['requirements'].values())
        require(report['qualified'], 'release requirement evidence is incomplete')
    except Exception as error:
        report['qualified'] = False
        report['failure'] = {'type': type(error).__name__, 'observed': str(error)}
        raise
    finally:
        report['ended_epoch_ns'] = time.time_ns()
        report['wall_seconds'] = time.monotonic() - started_monotonic
        if config.get('measurement') and report['wall_seconds'] > config['measurement'].get('total_timeout_seconds', 0):
            report['qualified'] = False
            report['deadline_exceeded'] = True
        publish()
    require(report['qualified'], 'release measurement did not qualify within the reviewed deadline')
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['build', 'prepare', 'verify', 'verify-adversarial', 'run-fixture', 'release-capacity', '_phase4-worker'])
    parser.add_argument('--authority', required=True)
    parser.add_argument('--authority-sha256', required=True)
    parser.add_argument('--source-root', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--stage', choices=RELEASE_STAGES)
    args = parser.parse_args()
    config, _ = read_document(args.authority, args.authority_sha256)
    if args.operation == 'release-capacity':
        release_capacity(config, args.source_root, args.output, args.authority_sha256)
        return
    if args.operation == '_phase4-worker':
        require(config.get('schema_version') == 4 and args.stage is not None, 'invalid release measurement worker authority')
        raise SystemExit(phase4_worker(config, args.source_root, args.output, args.stage))
    require(config.get('schema_version') == 2, 'unsupported phase2 authority')
    if args.operation == 'build':
        reproduce(config['build'], args.source_root, args.output)
    elif args.operation == 'prepare':
        prepare(config, args.source_root, args.output)
    elif args.operation == 'run-fixture':
        run_failure_fixture(config['failed_pipeline'], args.output)
    elif args.operation == 'verify-adversarial':
        verify_adversarial(config, args.output)
    else:
        verify_all(config, args.output)


if __name__ == '__main__':
    main()
