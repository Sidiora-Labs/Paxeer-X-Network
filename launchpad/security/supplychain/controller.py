import argparse
import contextlib
import fcntl
import shutil
import signal
import tempfile
import base64
import json
import os
import pathlib
import re
import time
import urllib.parse
import urllib.request
from policy import admission, canonical, digest, require, pinned_image, validate_sbom
from provenance import Tool, read_document, validate_statement, write_document


class GitHubChecks:
    def __init__(self, repository, token=None):
        require(type(repository) is str and re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repository), 'invalid repository')
        self.repository = repository
        self.token = token

    def get(self, endpoint):
        require(endpoint.startswith('/') and '..' not in endpoint, 'invalid Actions endpoint')
        headers = {'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'KindleLaunch-release-admission'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        request = urllib.request.Request('https://api.github.com/repos/' + self.repository + endpoint, headers=headers)
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, hdrs, newurl):
                return None
        with urllib.request.build_opener(NoRedirect).open(request, timeout=15) as response:
            require(response.status == 200, 'Actions authority unavailable')
            from policy import load
            return load(response.read(4 * 1024 * 1024 + 1))

    def verify(self, expectation, checks):
        run_id = expectation['run_id']
        require(type(run_id) is str and re.fullmatch(r'[0-9]+', run_id), 'invalid Actions run')
        run = self.get('/actions/runs/' + run_id)
        require(run.get('event') == 'push' and run.get('head_sha') == expectation['commit'] and run.get('head_branch') == expectation['ref'].removeprefix('refs/heads/'), 'Actions source or event mismatch')
        require(run.get('head_repository', {}).get('full_name') == self.repository and run.get('repository', {}).get('full_name') == self.repository, 'fork Actions authority')
        require(run.get('path') == expectation['workflow_path'] and run.get('run_attempt') == expectation['run_attempt'], 'Actions workflow/attempt mismatch')
        require(run.get('status') == 'completed' and run.get('conclusion') == 'success', 'Actions run is not successful')
        require(len({receipt['id'] for receipt in checks.values()}) == len(checks), 'reused Actions check identity')
        jobs = {}
        for page in range(1, 21):
            response = self.get('/actions/runs/' + run_id + '/attempts/' + str(expectation['run_attempt']) + '/jobs?per_page=100&page=' + str(page))
            require(type(response.get('jobs')) is list, 'missing Actions jobs')
            for job in response['jobs']:
                require(type(job.get('id')) is int and job['id'] not in jobs, 'duplicate Actions job')
                jobs[job['id']] = job
            if len(response['jobs']) < 100:
                break
        else:
            require(False, 'Actions job pagination exceeds bound')
        for name, receipt in checks.items():
            identity = receipt['id']
            require(type(identity) is str and identity.isdecimal(), 'invalid Actions check ID: ' + name)
            job = jobs.get(int(identity))
            require(job is not None and job.get('name') == expectation['job_names'][name], 'Actions check identity mismatch: ' + name)
            require(job.get('status') == 'completed' and job.get('conclusion') == 'success' and job.get('head_sha') == expectation['commit'], 'Actions check failed: ' + name)
        return {'run_id': run_id, 'run_attempt': expectation['run_attempt'], 'jobs': {name: value['id'] for name, value in checks.items()}}


def cosign_identity(config):
    identity, issuer = config['certificate_identity'], config['certificate_oidc_issuer']
    require(type(identity) is str and identity.startswith('https://github.com/') and '@refs/heads/' in identity, 'untrusted signing subject')
    require(issuer == 'https://token.actions.githubusercontent.com', 'untrusted signing issuer')
    require(type(config.get('workflow_repository')) is str and re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', config['workflow_repository']), 'missing certificate repository authority')
    require(type(config.get('workflow_commit')) is str and re.fullmatch(r'[0-9a-f]{40}', config['workflow_commit']), 'missing certificate commit authority')
    return ['--certificate-identity', identity, '--certificate-oidc-issuer', issuer, '--certificate-github-workflow-repository', config['workflow_repository'], '--certificate-github-workflow-sha', config['workflow_commit']]


def verify_federated(config, document, bundle):
    tool = Tool.parse(config['tool'])
    require(pathlib.Path(document).is_file() and pathlib.Path(bundle).is_file(), 'missing signed release artifact')
    output = tool.run(['verify-blob', '--bundle', str(bundle), *cosign_identity(config), str(document)], timeout=120)
    return {'tool_sha256': tool.sha256, 'bundle_sha256': digest(pathlib.Path(bundle).read_bytes()), 'verified_output_sha256': digest(output)}


def sign_federated(config, document, bundle):
    require(not pathlib.Path(bundle).exists(), 'signing bundle already exists')
    tool = Tool.parse(config['tool'])
    tool.run(['sign-blob', '--yes', '--bundle', str(bundle), str(document)], timeout=180)
    return verify_federated(config, document, bundle)


def exception_preflight(release, expected, now):
    require(type(release.get('vulnerabilities')) is list, 'missing vulnerability list')
    for finding in release['vulnerabilities']:
        if finding.get('severity') != 'high':
            continue
        exception = finding.get('exception')
        require(type(exception) is dict and set(exception) == {'id', 'issued', 'expires', 'approved_by', 'controls'}, 'invalid high exception')
        identity = exception['id']
        require(type(identity) is str and identity, 'missing high exception identity')
        binding = {'finding': finding['id'], 'source': expected['source'], 'image': expected['image'], 'exception': exception}
        require(digest(canonical(binding)) in expected.get('approved_exception_digests', []), 'exception lacks independent approval: ' + identity)
        require(type(exception['issued']) is int and type(exception['expires']) is int and exception['issued'] <= now < exception['expires'] <= exception['issued'] + 604800, 'expired or excessive exception: ' + identity)


def admit(config, release_path, bundle_path, now):
    _, release_bytes = read_document(release_path)
    _, bundle_bytes = read_document(bundle_path)
    with tempfile.TemporaryDirectory(prefix='immutable-release-admission-') as temporary:
        directory = pathlib.Path(temporary)
        release = directory / 'release.json'
        bundle = directory / 'bundle.json'
        release.write_bytes(release_bytes)
        bundle.write_bytes(bundle_bytes)
        release.chmod(0o400)
        bundle.chmod(0o400)
        return _admit(config, release, bundle, now)


def _admit(config, release_path, bundle_path, now):
    expected = config['expected']
    require(config.get('schema_version') == 2, 'unsupported release authority version')
    require(type(now) is int and now >= 0, 'invalid controller clock')
    require(type(expected.get('commit')) is str and re.fullmatch(r'[0-9a-f]{40}', expected['commit']), 'missing trusted commit')
    require(type(expected.get('run_attempt')) is int and expected['run_attempt'] > 0, 'missing trusted run attempt')
    require(type(expected.get('job_names')) is dict and set(expected['job_names']) == {'verify', 'contracts', 'frontend', 'policy', 'scan', 'signing'}, 'incomplete trusted job mapping')
    require(config['cosign']['workflow_repository'] == expected['repository'] and config['cosign']['workflow_commit'] == expected['commit'], 'signer source authority mismatch')
    signature = verify_federated(config['cosign'], release_path, bundle_path)
    document, raw = read_document(release_path)
    schema, _ = read_document(pathlib.Path(__file__).with_name('phase2.schema.json'))
    __import__('jsonschema').Draft202012Validator(schema).validate(document)
    require(type(document) is dict and set(document) == {'schema_version', 'release', 'provenance', 'sboms', 'scan', 'build'}, 'invalid signed release shape')
    require(document['schema_version'] == 2, 'unsupported signed release version')
    release = document['release']
    require(release.get('ref') == expected['ref'] and expected['ref'] in config['protected_refs'], 'unprotected release branch')
    require(document['build']['source'] == expected['source'] and document['build']['image'] == expected['image'], 'build input mismatch')
    require(document['build']['binary_sha256'] == config['approved_binary_sha256'], 'unapproved binary digest')
    validate_statement(document['provenance'], expected)
    require(document['provenance']['predicate']['buildDefinition']['internalParameters'].get('binary_sha256') == config['approved_binary_sha256'], 'provenance binary mismatch')
    for field in ('service', 'binary', 'architecture'):
        require(document['build'].get(field) == expected[field], 'build target mismatch: ' + field)
    exception_preflight(release, expected, now)
    artifacts = document['sboms']
    require(type(artifacts) is list and {item.get('scope') for item in artifacts} == {'image', 'javascript'} and len(artifacts) == 2, 'incomplete released SBOM inventory')
    from sbom import validate_spdx, inventory_projection
    schema, _ = read_document(config['spdx_schema_path'], config['spdx_schema_sha256'])
    inventories = []
    for artifact in artifacts:
        require(set(artifact) == {'scope', 'sha256', 'document'}, 'invalid released SBOM artifact')
        require(digest(canonical(artifact['document'])) == artifact['sha256'], 'released SBOM byte mismatch')
        inventories.append(validate_spdx(artifact['document'], schema))
    projection = canonical(inventory_projection(inventories))
    validate_sbom(projection, expected['sbom'])
    scan = document['scan']
    require(type(scan) is dict and scan.get('scanner_status') == 'success' and scan.get('image') == expected['image'], 'scanner unavailable or wrong image')
    require(scan.get('tool_sha256') == config['approved_scanner_sha256'] and scan.get('database_sha256') == config['approved_database_sha256'], 'unapproved scanner inputs')
    require(type(scan.get('scanned_at')) is int and 0 <= now - scan['scanned_at'] < config['max_scan_age_seconds'] <= 86400, 'stale scanner evidence')
    require(release.get('vulnerabilities') == scan.get('vulnerabilities') and release.get('scanner_status') == scan['scanner_status'], 'scanner findings changed')
    from scanner import response_state
    responses = response_state(config, release['vulnerabilities'], now)
    require(scan.get('critical_response', {}) == responses, 'critical response authority mismatch')
    result = admission(release, expected, now)
    checks = GitHubChecks(expected['repository'], os.environ.get('GITHUB_TOKEN')).verify(expected, release['checks'])
    return {**result, 'schema_version': 2, 'release_sha256': digest(raw), 'verified_at': now, 'checks': checks, 'signature': signature, 'sboms': [{'scope': item['scope'], 'sha256': item['sha256']} for item in artifacts]}, document


@contextlib.contextmanager
def promotion_lock(output):
    output = pathlib.Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    import stat
    metadata = output.parent.stat()
    require(not output.parent.is_symlink() and metadata.st_uid == os.getuid() and stat.S_ISDIR(metadata.st_mode) and metadata.st_mode & 0o022 == 0, 'promotion directory is not exclusively owned')
    lock = output.with_name(output.name + '.lock')
    descriptor = os.open(lock, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        import stat
        require(stat.S_ISREG(os.fstat(descriptor).st_mode), 'invalid promotion lock')
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            require(False, 'duplicate promotion already in progress')
        yield
    finally:
        os.close(descriptor)


def promotion_state(output, binding):
    path = pathlib.Path(output).with_name(pathlib.Path(output).name + '.state.json')
    if path.exists():
        state, _ = read_document(path)
        require(type(state) is dict and set(state) == {'schema_version', 'binding', 'completed', 'promoted'}, 'invalid promotion recovery state')
        require(state['schema_version'] == 1 and state['binding'] == binding, 'promotion recovery authority changed')
        require(type(state['completed']) is list and len(state['completed']) == len(set(state['completed'])) and all(step in {'copied', 'sbom:image', 'sbom:javascript', 'provenance', 'signed', 'verified'} for step in state['completed']), 'invalid promotion recovery steps')
        require(type(state['promoted']) is bool and (not state['promoted'] or 'verified' in state['completed']), 'invalid promotion completion state')
    else:
        state = {'schema_version': 1, 'binding': binding, 'completed': [], 'promoted': False}
        write_document(path, state)
    return path, state


def promote(config, release_path, bundle_path, layout, output):
    with promotion_lock(output):
        return _promote(config, release_path, bundle_path, layout, output)


def _promote(config, release_path, bundle_path, layout, output):
    result, document = admit(config, release_path, bundle_path, int(time.time()))
    image = config['expected']['image']
    pinned_image(image)
    layout = pathlib.Path(layout)
    from reproduce import validate_layout
    manifest_digest = image.rsplit('@', 1)[1]
    candidate = validate_layout(layout, manifest_digest)
    require(candidate['binary_sha256'] == config['approved_binary_sha256'], 'OCI application binary differs from approval')
    require(candidate['architecture'] == config['expected']['architecture'], 'OCI platform differs from approval')
    binding = {'authority_sha256': digest(canonical(config)), 'release_sha256': result['release_sha256'], 'bundle_sha256': result['signature']['bundle_sha256'], 'image': image}
    state_path, state = promotion_state(output, binding)
    oras = Tool.parse(config['oras'])
    cosign = Tool.parse(config['promotion_cosign']['tool'])
    def completed(step):
        if step not in state['completed']:
            state['completed'].append(step)
        write_document(state_path, state)
    result.update(promoted=False, registry_attachments_verified=False, promotion_pending_attachment_verification=True)
    write_document(output, result)
    with tempfile.TemporaryDirectory(prefix='release-attachments-') as temporary:
        directory = pathlib.Path(temporary)
        registry = config['registry']
        require(registry.get('host') == 'ghcr.io' and image.startswith('ghcr.io/'), 'unsupported registry credential authority')
        require(type(registry.get('username')) is str and registry['username'], 'missing registry identity')
        token = os.environ.get('GITHUB_TOKEN')
        require(type(token) is str and token, 'registry workload token unavailable')
        auth = directory / 'config.json'
        oras.run(['login', '--registry-config', str(auth), '--username', registry['username'], '--password-stdin', registry['host']], timeout=60, input_bytes=(token + '\n').encode())
        require(auth.is_file() and not auth.is_symlink(), 'registry credential exchange failed')
        auth.chmod(0o600)
        environment = {**os.environ, 'DOCKER_CONFIG': str(directory)}
        if not state['promoted']:
            snapshot = directory / 'layout'
            shutil.copytree(layout, snapshot, symlinks=True)
            require(validate_layout(snapshot, manifest_digest) == candidate, 'OCI candidate changed while staging promotion')
            if 'copied' not in state['completed']:
                oras.run(['cp', '--to-registry-config', str(auth), '--from-oci-layout', str(snapshot) + '@' + manifest_digest, image], timeout=600)
                completed('copied')
            for artifact in document['sboms']:
                step = 'sbom:' + artifact['scope']
                if step not in state['completed']:
                    path = directory / (artifact['scope'] + '.spdx.json')
                    write_document(path, artifact['document'])
                    cosign.run(['attest', '--yes', '--predicate', str(path), '--type', 'spdxjson', image], timeout=180, env=environment)
                    completed(step)
            if 'provenance' not in state['completed']:
                predicate = directory / 'provenance.json'
                write_document(predicate, document['provenance']['predicate'])
                cosign.run(['attest', '--yes', '--predicate', str(predicate), '--type', 'slsaprovenance1', image], timeout=180, env=environment)
                completed('provenance')
            if 'signed' not in state['completed']:
                cosign.run(['sign', '--yes', image], timeout=180, env=environment)
                completed('signed')
        remote_path = directory / 'registry-manifest.json'
        oras.run(['manifest', 'fetch', '--registry-config', str(auth), '--output', str(remote_path), image], timeout=120)
        require(remote_path.is_file() and not remote_path.is_symlink() and digest(remote_path.read_bytes()) == manifest_digest.removeprefix('sha256:'), 'registry manifest differs from approval')
        cosign.run(['verify', *cosign_identity(config['promotion_cosign']), image], timeout=120, env=environment)
        verify_attachments(config, document, env=environment)
        completed('verified')
        state['promoted'] = True
        write_document(state_path, state)
        result.update(registry_attachments_verified=True, promotion_pending_attachment_verification=False, promoted=True, recovery_state_sha256=digest(state_path.read_bytes()))
        write_document(output, result)
    return result


def verify_attachments(config, document, env=None):
    tool = Tool.parse(config['promotion_cosign']['tool'])
    image = config['expected']['image']
    for kind, predicate_type, expected_predicates in (
        ('spdxjson', 'https://spdx.dev/Document', [artifact['document'] for artifact in document['sboms']]),
        ('slsaprovenance1', 'https://slsa.dev/provenance/v1', [document['provenance']['predicate']]),
    ):
        output = tool.run(['verify-attestation', '--type', kind, *cosign_identity(config['promotion_cosign']), image], timeout=120, env=env)
        envelopes = []
        text = output.decode('utf-8')
        decoder = json.JSONDecoder()
        position = 0
        from policy import load
        while position < len(text):
            while position < len(text) and text[position].isspace():
                position += 1
            if position == len(text):
                break
            _, end = decoder.raw_decode(text, position)
            parsed = load(text[position:end].encode())
            envelopes.extend(parsed if type(parsed) is list else [parsed])
            position = end
        predicates = []
        for envelope in envelopes:
            require(type(envelope.get('payload')) is str, 'missing verified attestation payload')
            statement = load(base64.b64decode(envelope['payload'], validate=True))
            name, subject_digest = image.rsplit('@sha256:', 1)
            require(statement.get('subject') == [{'name': name, 'digest': {'sha256': subject_digest}}] and statement.get('predicateType') == predicate_type, 'registry attestation subject mismatch')
            predicates.append(statement['predicate'])
        require(all(any(canonical(value) == canonical(actual) for actual in predicates) for value in expected_predicates), 'registry attachment missing or mismatched: ' + kind)


def verify_release_measurement(config, path, authority_sha256, candidate_sha256):
    from policy import sha
    from check_phase2 import RELEASE_REQUIREMENTS, RELEASE_STAGES
    require(sha(authority_sha256) and sha(candidate_sha256), 'missing independent release measurement authority')
    document, raw = read_document(path)
    require(document.get('schema_version') == 4 and document.get('task') == '4.46' and document.get('qualified') is True, 'release measurement is absent or unqualified')
    require(document.get('authority_sha256') == authority_sha256 and document.get('candidate_sha256') == candidate_sha256, 'release measurement authority or candidate mismatch')
    require(document.get('source') == config['expected']['source'] and document.get('image') == config['expected']['image'], 'release measurement binds a different source or image')
    require(document.get('qualification_scope') == 'supplychain-release-measurements' and document.get('global_capacity_accepted') is False and document.get('final_shared_load_restore_gate') == '9.1', 'release measurement scope was changed')
    require(type(document.get('stages')) is dict and set(document['stages']) == set(RELEASE_STAGES), 'release measurement stage inventory is incomplete')
    for name, stage in document['stages'].items():
        require(stage.get('status') == 'passed' and stage.get('candidate_sha256') == candidate_sha256 and stage.get('exit_code') == 0 and stage.get('timed_out') is False, 'release measurement stage failed: ' + name)
        require(type(stage.get('budget_checks')) is dict and set(stage['budget_checks']) == {'worker_success', 'wall_time', 'cpu_time', 'single_process_memory', 'artifact_bytes'} and all(value is True for value in stage['budget_checks'].values()), 'release measurement budget failed: ' + name)
    requirements = document.get('requirements')
    require(type(requirements) is dict and set(requirements) == {f'{name}.{clause}' for name in RELEASE_REQUIREMENTS for clause in range(1, 6)}, 'release measurement requirement inventory is incomplete')
    require(all(value.get('status') == 'passed' and value.get('required_stages') == list(RELEASE_REQUIREMENTS[name.rsplit('.', 1)[0]]) for name, value in requirements.items()), 'release requirement measurement was skipped')
    inventory = document.get('raw_artifacts')
    require(type(inventory) is dict and type(inventory.get('files')) is dict and inventory['files'], 'release measurement raw artifacts are absent')
    root = pathlib.Path(path).parent.resolve()
    total = 0
    for name, entry in inventory['files'].items():
        relative = pathlib.PurePosixPath(name)
        require(type(name) is str and not relative.is_absolute() and '..' not in relative.parts, 'unsafe release measurement artifact')
        artifact = root / relative
        require(artifact.is_file() and not artifact.is_symlink() and artifact.resolve().is_relative_to(root), 'missing or indirect release measurement artifact')
        require(type(entry) is dict and set(entry) == {'sha256', 'bytes'} and sha(entry['sha256']) and type(entry['bytes']) is int and entry['bytes'] >= 0, 'invalid release measurement artifact identity')
        with artifact.open('rb') as source:
            import hashlib
            require(artifact.stat().st_size == entry['bytes'] and hashlib.file_digest(source, 'sha256').hexdigest() == entry['sha256'], 'release measurement raw artifact changed')
        total += entry['bytes']
    require(total == inventory.get('total_bytes'), 'release measurement artifact accounting changed')
    require({'source-before.json', 'source-after.json'} <= set(inventory['files']) and all(name + '/worker-result.json' in inventory['files'] and name + '/measurement.json' in inventory['files'] for name in RELEASE_STAGES), 'release measurement raw captures are incomplete')
    before, _ = read_document(root / 'source-before.json')
    after, _ = read_document(root / 'source-after.json')
    require(before == after and before.get('sha256') == candidate_sha256 and type(before.get('files')) is dict, 'release source captures changed or have wrong candidate')
    require(digest(canonical(before['files'])) == candidate_sha256, 'release source capture digest is invalid')
    return {'sha256': digest(raw), 'candidate_sha256': candidate_sha256, 'authority_sha256': authority_sha256}


def main():
    def interrupted(number, frame):
        raise InterruptedError('release controller interrupted by signal ' + str(number))
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['admit', 'sign', 'promote'])
    parser.add_argument('--authority', required=True)
    parser.add_argument('--authority-sha256', required=True)
    parser.add_argument('--release', required=True)
    parser.add_argument('--bundle', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--layout')
    parser.add_argument('--measurement')
    parser.add_argument('--measurement-authority-sha256')
    parser.add_argument('--candidate-sha256')
    args = parser.parse_args()
    try:
        config, _ = read_document(args.authority, args.authority_sha256)
        if args.operation == 'sign':
            result = sign_federated(config['cosign'], args.release, args.bundle)
            write_document(args.output, result)
        elif args.operation == 'admit':
            result, _ = admit(config, args.release, args.bundle, int(time.time()))
            write_document(args.output, result)
        else:
            require(args.layout is not None, 'missing OCI layout')
            if args.measurement is not None:
                verify_release_measurement(config, args.measurement, args.measurement_authority_sha256, args.candidate_sha256)
            else:
                require(args.measurement_authority_sha256 is None and args.candidate_sha256 is None, 'measurement authority requires actual report')
            promote(config, args.release, args.bundle, args.layout, args.output)
    except Exception as error:
        write_document(args.output, {'schema_version': 2, 'admitted': False, 'promoted': False, 'verified_at': int(time.time()), 'failure': {'type': type(error).__name__, 'observed': str(error)}, 'counters': {'release_gate_failures_total': 1}})
        raise


if __name__ == '__main__':
    main()
