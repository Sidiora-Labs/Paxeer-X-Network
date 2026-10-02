#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import ssl
import stat
import subprocess
import sys
import urllib.parse
import urllib.request

COUNT = 0
SKIPPED = 0
ROOT = Path(__file__).resolve().parents[3]


def require(value, message):
    if not value:
        raise ValueError(message)


def private_json(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
            'input must be an owned private regular file')
    require(info.st_size <= 1024 * 1024, 'input exceeds bound')
    return json.loads(path.read_text())


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        raise ValueError('isolated fixture redirects are refused')


def rpc(target, method, params):
    global COUNT
    url = urllib.parse.urlsplit(target['url'])
    require(url.scheme == 'https' and url.hostname in ('localhost', '127.0.0.1', '::1')
            and not url.username and not url.password and not url.fragment,
            'fixture must use an isolated local TLS endpoint')
    context = ssl.create_default_context(cafile=target['ca_file'])
    if 'certificate_file' in target:
        context.load_cert_chain(target['certificate_file'], target['key_file'])
    request = urllib.request.Request(
        target['url'],
        data=json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}).encode(),
        headers={'Content-Type': 'application/json'},
    )
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=context),
    )
    with opener.open(request, timeout=8) as response:
        raw = response.read(8 * 1024 * 1024 + 1)
    require(len(raw) <= 8 * 1024 * 1024, 'response exceeds bound')
    answer = json.loads(raw)
    require(isinstance(answer, dict) and answer.get('jsonrpc') == '2.0'
            and answer.get('id') == 1 and (('result' in answer) != ('error' in answer)),
            'response identity mismatch')
    COUNT += 1
    return answer


def canonical_account(name):
    encoded = name.encode('ascii')
    return hashlib.sha256(b'LX:ACCOUNT:v1' + len(encoded).to_bytes(4, 'big') + encoded).hexdigest()


def run():
    global COUNT, SKIPPED
    bundle_path = os.environ.get('PAXEER_X_IDENTITY_BUNDLE')
    fixture_path = os.environ.get('PAXEER_X_IDENTITY_FIXTURE')
    evidence_path = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(bundle_path and fixture_path,
            'prebuilt identity bundle and real isolated identity fixture are required')
    require(evidence_path, 'private evidence directory is required')
    evidence = Path(evidence_path)
    info = evidence.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o700, 'evidence directory must be owned and private')
    bundle, fixture = private_json(bundle_path), private_json(fixture_path)
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    require(bundle['revision'] == revision == fixture['revision'], 'source provenance mismatch')
    require(fixture['schema'] == 'paxeer-x.unified-identity-fixture.v1', 'unsupported fixture schema')
    for name in ('client', 'explorer', 'router', 'keeper', 'precompile'):
        binary = Path(bundle['binaries'][name])
        require(binary.is_file() and os.access(binary, os.X_OK), 'missing prebuilt executable: ' + name)
    evm, did, native = fixture['evm'], fixture['did'], fixture['native_account']
    require(re.fullmatch(r'0x[0-9a-f]{40}', evm)
            and re.fullmatch(r'did:layerx:[0-9a-f]{64}', did)
            and re.fullmatch(r'[0-9a-f]{64}', native), 'invalid real identity selectors')
    require(native == canonical_account('agent:' + did + ':main'),
            'fixture native account is not the canonical DID main account')
    healthy = fixture['targets']['healthy']
    require(rpc(healthy, 'eth_chainId', [])['result'] == '0x7d', 'wrong chain')
    expected = None
    methods = ('px_resolveAccount', 'px_getAccount', 'px_getBalances', 'px_getUnifiedHistory')
    for selector in (evm, did, evm, did):
        answer = rpc(healthy, 'px_resolveAccount', [selector])
        require('error' not in answer, 'real binding unavailable')
        document = answer['result']
        require(document['evm_address'] == evm and document['layerx_did'] == did
                and document['layerx_account'] == native and document['bound'] is True,
                'conflicting real binding')
        require(expected is None or expected == document, 'repeated lookup changed owner')
        expected = document
        for method in methods[1:]:
            joined = rpc(healthy, method, [selector])
            require('error' not in joined and joined['result']['account'] == document,
                    'account/balance/history join changed owner')
            result = joined['result']
            if method == 'px_getAccount':
                require(result['paxeer']['address'] == evm
                        and result['layerx']['account_id'] == native
                        and result['layerx']['name'] == 'agent:' + did + ':main',
                        'native account read changed canonical owner')
            elif method == 'px_getBalances':
                require(isinstance(result['balances'], list) and result['balances'],
                        'real retained balance rows are required')
                native_rows = [row['layerx'] for row in result['balances'] if row.get('layerx') is not None]
                require(native_rows, 'real retained native balance row is required')
                for row in native_rows:
                    require(row['name'].startswith('agent:' + did + ':')
                            and row['account_id'] == canonical_account(row['name']),
                            'native balance row changed owner')
            else:
                keys = {(row['side'], row['account']) for row in result['accounts']}
                require(('paxeer', evm) in keys and ('layerx', native) in keys,
                        'unified history lost the resolved account')
                require(isinstance(result['items'], list) and result['items'],
                        'real retained history rows are required')
    for selector in (native, did.removeprefix('did:layerx:')):
        for method in methods:
            refusal = rpc(healthy, method, [selector])
            require(refusal.get('error', {}).get('code') == -32001
                    and refusal['error'].get('data', {}).get('code')
                    == 'native_account_reverse_resolution_unsupported',
                    'bare native account entered DID lookup')
    allowed_refusals = {
        'wrong_network': {'paxeer_wrong_network_or_unavailable'},
        'unavailable_binding': {'paxeer_call_refused', 'identity_binding_unavailable'},
        'conflicting_binding': {'conflicting_identity_binding'},
    }
    for target, codes in allowed_refusals.items():
        require(fixture['refusal_codes'][target] in codes, 'invalid expected refusal: ' + target)
        for method in methods:
            refusal = rpc(fixture['targets'][target], method, [did])
            require(refusal.get('error', {}).get('code') == -32001,
                    'required refusal did not occur: ' + target)
            require(refusal['error'].get('data', {}).get('code') == fixture['refusal_codes'][target],
                    'wrong typed refusal: ' + target)
        require(rpc(healthy, 'px_resolveAccount', [did]).get('result') == expected,
                'healthy lookup failed after refused identity join')
    declarations = json.loads((ROOT / 'platform/hosted/gateway/openrpc.json').read_text())
    require({method['name'] for method in declarations['methods']} >= set(methods),
            'OpenRPC is missing an identity lookup method')
    for method in declarations['methods']:
        if method['name'] not in methods:
            continue
        param = next(param for param in method['params'] if param['name'] == 'account')
        pattern = param['schema']['pattern']
        require(re.fullmatch(pattern, evm) and re.fullmatch(pattern, did)
                and not re.fullmatch(pattern, native), 'OpenRPC selector contract mismatch')
    groups = {
        'client': ['identity_selector_contract'],
        'explorer': ['unified::tests::'],
        'router': ['paxeer::'],
        'keeper': ['-test.run=^TestLayerX(Bind|DoubleBind|Unbind|BindingGenesis)', '-test.v'],
        'precompile': ['-test.run=^TestLayerX', '-test.v'],
    }
    required_cases = {
        'client': ('native_account_never_becomes_a_did_selector',
                   'real_repeated_account_and_balance_lookup_preserves_owner',
                   'retaining_lookup_refuses_incomplete_or_conflicting_joins'),
        'explorer': ('identity_selector_contract_event_keys_never_cross_identity_kinds',),
        'router': ('identical_hex_has_distinct_did_and_native_meanings',
                   'canonical_main_account_is_bound_to_the_exact_did',
                   'native_listing_refuses_another_owner_or_noncanonical_account'),
        'keeper': ('TestLayerXBind', 'TestLayerXBindReplayRefused',
                   'TestLayerXBindWrongChainIDRefused', 'TestLayerXDoubleBindRefused',
                   'TestLayerXUnbindThenRebind', 'TestLayerXBindingGenesisRoundTrip'),
        'precompile': ('TestLayerXBindThroughPrecompile',
                       'TestLayerXBindRefusalsThroughPrecompile',
                       'TestLayerXUnbindAndRebindThroughPrecompile'),
    }
    for name, args in groups.items():
        env = dict(os.environ, SSL_CERT_FILE=healthy['ca_file'])
        log = evidence / ('identity-' + name + '.log')
        with log.open('x', encoding='utf-8') as output:
            os.chmod(log, 0o600)
            completed = subprocess.run([bundle['binaries'][name], *args], cwd=ROOT, env=env,
                                       stdout=output, stderr=subprocess.STDOUT, text=True, timeout=120)
        text = log.read_text()
        require(completed.returncode == 0, 'prebuilt test failed: ' + name)
        if name in ('client', 'explorer', 'router'):
            summaries = re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', text)
            SKIPPED += sum(int(ignored) for _, _, ignored in summaries)
            require(summaries and all(int(passed) > 0 and int(failed) == 0 and int(ignored) == 0
                                     for passed, failed, ignored in summaries),
                    'missing, failed or skipped production cases: ' + name)
            for case in required_cases[name]:
                require(re.search(r'^test (?:\S+::)?' + re.escape(case) + r' \.\.\. ok$',
                                  text, re.MULTILINE), 'required production case absent: ' + case)
            COUNT += sum(int(passed) for passed, _, _ in summaries)
        else:
            passed = re.findall(r'^--- PASS: (\S+)', text, re.MULTILINE)
            SKIPPED += len(re.findall(r'^--- SKIP:', text, re.MULTILINE))
            require(passed and '\nPASS\n' in text and '--- SKIP:' not in text,
                    'missing or skipped production cases: ' + name)
            require(set(required_cases[name]) <= set(passed),
                    'required production binding case absent: ' + name)
            COUNT += len(passed)
        print('ok identity production tests ' + name, flush=True)


if __name__ == '__main__':
    try:
        run()
    except (ValueError, KeyError, TypeError, StopIteration, OSError, subprocess.SubprocessError) as error:
        print('identity: refusal: ' + (str(error) if isinstance(error, ValueError)
                                      else type(error).__name__), file=sys.stderr)
        print(f'PAXEER_X_GATE tests={COUNT} skipped={SKIPPED}')
        sys.exit(1)
    print(f'PAXEER_X_GATE tests={COUNT} skipped={SKIPPED}')
