#!/usr/bin/env python3
"""Paxeer X program-registry contract qualification.

Each case runs the registry's own tests through the production types and
persistence paths, and fails when a required input, execution or test case is
absent, failed or ignored.
"""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.candidate.v1'
RESULT = re.compile(r'^test (\S+) \.\.\. (\S+)$', re.M)

CASES = {
    'durable-verification-idempotency': {
        'command': ['cargo', 'test', '--offline', '--locked',
                    '--manifest-path', 'platform/Cargo.toml',
                    '-p', 'layerx-platform-registry', '--lib', 'durable_verification'],
        'tests': {
            'verified::tests::durable_verification_replays_after_restart_and_refuses_changed_requests':
                'a completed scope replays its recorded response after restart; a changed request is 409 and leaves the record unchanged',
            'verified::tests::durable_verification_isolates_principals_and_refuses_foreign_records':
                'principals own distinct scopes; a record bound to another principal fails closed',
            'verified::tests::durable_verification_recovers_a_crashed_rebuild_as_a_new_attempt':
                'a rebuild without a live owner recovers as the next attempt and stays bound to its request',
            'verified::tests::durable_verification_recovers_a_persisted_publication_without_duplicate_events':
                'a persisted verification recovers its publication without a rebuild or a second outbox event',
            'verified::tests::durable_verification_retention_preserves_live_identities':
                'retention evicts only settled identities and refuses new ones when every retained identity is live',
            'verified::tests::durable_verification_fails_closed_on_corrupt_uncertain_and_unavailable_storage':
                'corrupt and future-version records fail closed and stay intact; interrupted writes are preserved; unavailable storage refuses',
            'verified::tests::durable_verification_concurrent_worker_process_has_one_build_owner':
                'a second worker process cannot take the journal while the build owner holds it and then replays its response',
        },
    },
}


def fail(message):
    print('registry-contract: FAIL: ' + message, flush=True)
    print('registry-contract: exit code 1', flush=True)
    sys.exit(1)


def manifest(path):
    if not path:
        fail('--candidate-manifest is required')
    try:
        data = Path(path).read_bytes()
        document = json.loads(data)
    except (OSError, ValueError) as error:
        fail('candidate manifest unreadable: ' + str(error))
    if not isinstance(document, dict) or document.get('schema') != SCHEMA:
        fail('candidate manifest schema is not ' + SCHEMA)
    return hashlib.sha256(data).hexdigest()


def run(name):
    case = CASES[name]
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    result = subprocess.run(case['command'], cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    outcomes = dict(RESULT.findall(result.stdout))
    expected = case['tests']
    missing = sorted(set(expected) - set(outcomes))
    unexpected = sorted(set(outcomes) - set(expected))
    failed = sorted(test for test in expected if outcomes.get(test, 'ok') != 'ok')
    for test, assertion in expected.items():
        print('registry-contract: %s %s: %s' % (outcomes.get(test, 'absent'), test, assertion))
    if missing:
        fail('cases did not execute: ' + ', '.join(missing))
    if unexpected:
        fail('unexpected cases executed: ' + ', '.join(unexpected))
    if failed:
        fail('cases did not pass: ' + ', '.join(failed))
    if result.returncode:
        fail('%s exited %d' % (' '.join(case['command']), result.returncode))
    return len(expected)


SERVED_FIELDS = ('binary', 'environment', 'url', 'ca', 'cert', 'key', 'publication_token_file',
                 'program', 'source_uri', 'source_digest', 'changed_source_digest')


def served_inputs():
    """Real served prerequisites: the source-bound registry artifact manifest
    and the served environment (node, receipt authority, TLS, tokens, program)."""
    artifacts = os.environ.get('PAXEER_X_REGISTRY_ARTIFACT_MANIFEST', '')
    served = os.environ.get('PAXEER_X_REGISTRY_SERVED_ENVIRONMENT', '')
    if not artifacts or not Path(artifacts).is_file():
        fail('served registry artifact manifest absent: PAXEER_X_REGISTRY_ARTIFACT_MANIFEST=%r' % artifacts)
    if not served or not Path(served).is_file():
        fail('served registry environment absent: PAXEER_X_REGISTRY_SERVED_ENVIRONMENT=%r' % served)
    try:
        document = json.loads(Path(served).read_bytes())
        artifact_text = Path(artifacts).read_text()
    except (OSError, ValueError) as error:
        fail('served inputs unreadable: ' + str(error))
    absent = [field for field in SERVED_FIELDS if not document.get(field)]
    if absent:
        fail('served environment lacks: ' + ', '.join(absent))
    binary = Path(document['binary'])
    try:
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    except OSError as error:
        fail('served registry binary unreadable: ' + str(error))
    if digest not in artifact_text:
        fail('served registry binary %s is not bound by the artifact manifest' % digest)
    return document


class Served:
    def __init__(self, inputs, log):
        self.inputs = inputs
        self.log = log
        self.process = None
        self.context = ssl.create_default_context(cafile=inputs['ca'])
        self.context.load_cert_chain(inputs['cert'], inputs['key'])
        self.token = Path(inputs['publication_token_file']).read_text().strip()
        environment = inputs['environment']
        self.state = Path(environment.get('LAYERX_REGISTRY_STATE', '/var/lib/layerx-registry'))
        self.verified = Path(environment.get('LAYERX_REGISTRY_VERIFIED', str(self.state / 'verified')))

    def start(self):
        environment = dict(os.environ, **self.inputs['environment'])
        self.process = subprocess.Popen([self.inputs['binary']], env=environment, stdin=subprocess.DEVNULL,
                                        stdout=self.log, stderr=self.log)
        limit = time.monotonic() + 180
        while time.monotonic() < limit:
            if self.process.poll() is not None:
                fail('served registry exited during startup with %d' % self.process.returncode)
            try:
                if self.request('GET', '/healthz')[0] == 200:
                    return
            except OSError:
                pass
            time.sleep(0.1)
        fail('served registry startup deadline exceeded')

    def stop(self, crash=False):
        if self.process and self.process.poll() is None:
            self.process.send_signal(signal.SIGKILL if crash else signal.SIGTERM)
            self.process.wait(timeout=60)

    def request(self, method, path, key=None, digest=None):
        headers = {'Authorization': 'Bearer ' + self.token, 'Content-Type': 'application/json'}
        data = None
        if key is not None:
            headers['Idempotency-Key'] = key
            data = json.dumps({'source_uri': self.inputs['source_uri'],
                               'source_digest': digest or self.inputs['source_digest']}).encode()
        request = urllib.request.Request(self.inputs['url'] + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, context=self.context, timeout=1800) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def verify(self, key, digest=None):
        return self.request('POST', '/v1/programs/registry/%s/source' % self.inputs['program'], key, digest)

    def verified_state(self):
        digest = hashlib.sha256()
        for path in sorted(self.verified.rglob('*')):
            if path.is_file():
                digest.update(str(path.relative_to(self.verified)).encode() + b'\0' + path.read_bytes())
        return digest.hexdigest()


def code(body):
    try:
        return json.loads(body).get('error', {}).get('code') if body else None
    except (ValueError, AttributeError):
        return None


def served(evidence):
    inputs = served_inputs()
    assertions = []

    def check(name, condition, observed):
        assertions.append({'assertion': name, 'ok': bool(condition), 'observed': observed})
        print('registry-contract: %s served::%s: %s' % ('ok' if condition else 'FAILED', name, observed), flush=True)

    stamp = '%d' % time.time_ns()
    with open(evidence / 'served-registry.log', 'ab') as log:
        registry = Served(inputs, log)
        registry.start()
        try:
            key = 'served-replay-' + stamp
            first = registry.verify(key)
            check('terminal_response_is_recorded', first[0] == 200, first[0])
            before = registry.verified_state()
            registry.stop()
            registry.start()
            replay = registry.verify(key)
            check('retry_after_terminal_response_replays_across_restart', replay == first, replay[0])
            check('replay_does_not_rerun_the_build', registry.verified_state() == before, 'verified state unchanged')
            conflict = registry.verify(key, inputs['changed_source_digest'])
            check('changed_request_is_409', conflict[0] == 409 and code(conflict[1]) == 'idempotency_conflict',
                  conflict[0])
            check('conflict_leaves_verified_state_unchanged', registry.verified_state() == before,
                  'verified state unchanged')

            key = 'served-concurrent-' + stamp
            with concurrent.futures.ThreadPoolExecutor(4) as pool:
                results = list(pool.map(lambda _: registry.verify(key), range(4)))
            settled = [result for result in results if result[0] == 200]
            pending = [result for result in results if result[0] == 503 and code(result[1]) == 'verification_pending']
            check('concurrent_identical_requests_have_one_outcome',
                  settled and len(settled) + len(pending) == len(results) and len(set(settled)) == 1,
                  [result[0] for result in results])

            key = 'served-crash-' + stamp
            with concurrent.futures.ThreadPoolExecutor(1) as pool:
                inflight = pool.submit(registry.verify, key)
                time.sleep(1)
                registry.stop(crash=True)
                try:
                    interrupted = inflight.result()[0]
                except OSError as error:
                    interrupted = type(error).__name__
            registry.start()
            recovered = registry.verify(key)
            check('worker_restart_mid_verification_recovers_an_explicit_state',
                  recovered[0] == 200 or (recovered[0] == 503 and code(recovered[1]) == 'verification_pending'),
                  [interrupted, recovered[0]])
            limit = time.monotonic() + 900
            while recovered[0] == 503 and time.monotonic() < limit:
                time.sleep(1)
                recovered = registry.verify(key)
            settled_state = registry.verified_state()
            again = registry.verify(key)
            check('outbox_replay_after_crash_settles_once', recovered[0] == 200 and again == recovered
                  and registry.verified_state() == settled_state, [recovered[0], again[0]])
        finally:
            registry.stop()
    (evidence / 'served-assertions.json').write_text(json.dumps(assertions, indent=2))
    failed = [entry['assertion'] for entry in assertions if not entry['ok']]
    if failed:
        fail('served assertions failed: ' + ', '.join(failed))
    return len(assertions)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--case', required=True, choices=sorted(CASES))
    parser.add_argument('--candidate-manifest', required=True)
    arguments = parser.parse_args()
    revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, capture_output=True, text=True).stdout.strip()
    print('registry-contract: revision ' + revision)
    print('registry-contract: command ' + ' '.join(sys.argv))
    directory = os.environ.get('PAXEER_X_EVIDENCE_DIR', '')
    if not directory:
        fail('PAXEER_X_EVIDENCE_DIR is required')
    evidence = Path(directory) / ('registry-contract-' + arguments.case)
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    print('registry-contract: evidence ' + str(evidence))
    digest = manifest(arguments.candidate_manifest)
    print('registry-contract: candidate manifest sha256 ' + digest)
    count = run(arguments.case)
    count += served(evidence)
    print('registry-contract: case %s cases=%d passed=%d' % (arguments.case, count, count))
    print('registry-contract: exit code 0')
    print('PAXEER_X_GATE tests=%d skipped=0' % count)


if __name__ == '__main__':
    main()
