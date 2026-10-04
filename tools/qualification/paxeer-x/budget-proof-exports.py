#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import ssl
import stat
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
MAXIMUM = 1_048_576
SOURCES = (
    'platform/hosted/authority/src/human/budget_state.rs',
    'platform/hosted/authority/src/human.rs',
    'platform/hosted/authority/src/human/dynamic.rs',
    'platform/hosted/authority/tests/budget_proof_exports.rs',
    'tools/qualification/paxeer-x/budget-proof-exports.py',
)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def protected(path, maximum):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= maximum
            and info.st_mode & 0o077 == 0, 'genuine fixture input must be protected and bounded')
    return path.read_bytes()


def required(name):
    value = os.environ.get(name)
    require(bool(value), name + ' is required; genuine authority fixture is unavailable')
    return value


def main():
    fixture = json.loads(protected(required('PAXEER_X_BUDGET_PROOF_FIXTURE'), 65_536))
    require(fixture.get('schema') == 'paxeer-x.budget-proof-fixture.v1', 'invalid real fixture profile')
    hashes = fixture.get('source_hashes', {})
    require(all(hashes.get(source) == hashlib.sha256((ROOT / source).read_bytes()).hexdigest()
                for source in SOURCES), 'fixture binaries do not bind the final producer and verifier sources')
    evidence = Path(required('PAXEER_X_EVIDENCE_DIR')).resolve()
    require(evidence != ROOT and ROOT not in evidence.parents, 'private evidence must be outside the checkout')
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    require(evidence.stat().st_mode & 0o077 == 0, 'evidence directory must be owner-only')
    binary = Path(fixture['test_binary'])
    require(binary.is_file() and os.access(binary, os.X_OK), 'prebuilt genuine corpus verifier is required')
    require(hashlib.sha256(binary.read_bytes()).hexdigest() == fixture['test_binary_sha256'],
            'corpus verifier artifact mismatch')
    endpoint = urllib.parse.urlsplit(fixture['base_url'])
    require(endpoint.scheme == 'https' and endpoint.hostname and not endpoint.username
            and not endpoint.password and not endpoint.query and not endpoint.fragment,
            'authority endpoint must use genuine authenticated TLS')
    for name in ('ca_file', 'client_cert', 'client_key', 'bearer_file'):
        protected(fixture[name], 65_536)
    context = ssl.create_default_context(cafile=fixture['ca_file'])
    context.load_cert_chain(fixture['client_cert'], fixture['client_key'])
    bearer = protected(fixture['bearer_file'], 4096).decode('utf-8').strip()
    require(bearer and '\r' not in bearer and '\n' not in bearer, 'invalid protected bearer format')
    query = dict(fixture['query'])
    require(set(('tenant', 'principal', 'budget_id')) <= query.keys(), 'principal and budget binding required')

    def request(parameters, authorization):
        url = fixture['base_url'].rstrip('/') + '/v1/agent/budget-proof?' + urllib.parse.urlencode(parameters)
        request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + authorization})
        try:
            response = urllib.request.urlopen(request, context=context, timeout=8)
        except urllib.error.HTTPError as refusal:
            response = refusal
        with response:
            raw = response.read(MAXIMUM + 1)
            require(len(raw) <= MAXIMUM, 'export exceeded its declared response bound')
            return response.status, raw

    status, raw = request(query, bearer)
    require(status == 200, 'genuine authenticated budget export did not succeed')
    document = json.loads(raw)
    require(document.get('schema') == 'layerx.human.budget-proof.v1', 'unexpected export schema')
    require(document.get('tenant') == query['tenant'] and document.get('principal') == query['principal'],
            'export principal mismatch')
    require(document['budget_state']['budget_id'] == query['budget_id'], 'export budget mismatch')
    response_path = evidence / 'budget-proof-export.json'
    with response_path.open('wb', opener=lambda path, flags: os.open(path, flags, 0o600)) as handle:
        handle.write(raw)
    os.chmod(response_path, 0o600)
    corpus = json.loads(protected(fixture['corpus'], 65_536))
    cases = corpus.get('cases')
    require(isinstance(cases, list) and cases, 'genuine native proof corpus is required')
    require(cases[0]['budget_id'] == query['budget_id'], 'corpus is for another budget')
    cases[0]['response'] = str(response_path)
    corpus_path = evidence / 'budget-proof-corpus.json'
    with corpus_path.open('w', opener=lambda path, flags: os.open(path, flags, 0o600)) as handle:
        json.dump(corpus, handle)
    os.chmod(corpus_path, 0o600)
    for changed, credential, expected in (
        (query, 'invalid', 401),
        ({**query, 'tenant': query['tenant'] + ':wrong'}, bearer, 403),
        ({**query, 'principal': query['principal'] + ':wrong'}, bearer, 403),
        ({key: value for key, value in query.items() if key != 'budget_id'}, bearer, 400),
    ):
        require(request(changed, credential)[0] == expected, 'budget export authentication or query refusal failed')
    environment = dict(os.environ, PAXEER_X_BUDGET_PROOF_CORPUS=str(corpus_path))
    result = subprocess.run([str(binary), '--exact', 'real_budget_exports_bind_each_selector_finality_and_tamper_refusals'],
                            env=environment, cwd=ROOT, timeout=120, check=False, capture_output=True)
    with (evidence / 'budget-proof-verifier.log').open('wb', opener=lambda path, flags: os.open(path, flags, 0o600)) as handle:
        handle.write(result.stdout + result.stderr)
    require(result.returncode == 0, 'genuine native certificate/selector/tamper verifier failed')
    require(b'running 1 test' in result.stdout and b'1 passed; 0 failed' in result.stdout,
            'native proof verifier did not execute exactly the declared test')
    print('budget proof export TLS, source binding and native proof corpus passed')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as error:
        print('budget proof export qualification refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired):
        print('budget proof export qualification refused: genuine source-bound authority inputs or required proof checks failed', file=sys.stderr)
        sys.exit(1)
