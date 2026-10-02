#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SCENARIOS = ('prepared', 'broadcast', 'dropped', 'replacement-prepared',
             'replacement-broadcast', 'finality', 'unreachable', 'divergent')


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def protected(path):
    path = Path(path)
    require(path.is_absolute() and not any(p == '.env' or p.startswith('.env.') for p in path.parts),
            'absolute non-environment material path required')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and info.st_nlink == 1 and not info.st_mode & 0o077, 'private single-link material required')
    require(info.st_size <= 16 * 1024 * 1024, 'material exceeds bound')
    return path


def document(path):
    def unique(pairs):
        result = {}
        for name, value in pairs:
            require(name not in result, 'duplicate material field')
            result[name] = value
        return result
    return json.loads(protected(path).read_text(), object_pairs_hook=unique)


def digest(path):
    with Path(path).open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def hexbytes(value):
    return '0x' + bytes(value).hex()


def integer(value):
    return int.from_bytes(bytes(value), 'big')


def local_url(url, secure=False):
    parsed = urllib.parse.urlsplit(url)
    require(parsed.scheme == ('https' if secure else 'http') and parsed.hostname
            and not parsed.username and not parsed.password and not parsed.fragment
            and not parsed.query, 'isolated local endpoint required')
    addresses = socket.getaddrinfo(parsed.hostname, parsed.port or (443 if secure else 80))
    require(addresses and all(ipaddress.ip_address(row[4][0]).is_loopback for row in addresses),
            'qualification refuses non-loopback chain and service endpoints')


def post(url, value, timeout=3):
    request = urllib.request.Request(url, json.dumps(value).encode(), {'Content-Type': 'application/json'})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(request, timeout=timeout) as response:
            return response.status, json.loads(response.read(1_048_577))
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read(1_048_577))


def rpc(endpoints, method, params):
    votes = {}
    for endpoint in endpoints:
        status, reply = post(endpoint, {'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params})
        require(status == 200 and reply.get('id') == 1 and 'error' not in reply
                and 'result' in reply, 'chain observation refused')
        key = json.dumps(reply['result'], sort_keys=True)
        votes[key] = votes.get(key, 0) + 1
    winner = [value for value, count in votes.items() if count > len(endpoints) // 2]
    require(len(winner) == 1, 'chain observations lack strict majority')
    return json.loads(winner[0])


def journal(path):
    raw = path.read_bytes()
    require(not raw or raw.endswith(b'\n'), 'torn journal')
    return [json.loads(line) for line in raw.splitlines()]


def identities(entries):
    quotes = [e['quote'] for e in entries if e['kind'] == 'quoted']
    require(quotes, 'scenario has no genuine durable quote')
    return [(q, {'sponsor': hexbytes(q['key']['sponsor']), 'quoteNonce': str(integer(q['key']['quote_nonce'])),
                 'account': hexbytes(q['account']), 'relayerSignature': hexbytes(q['signature'])}) for q in quotes]


def submitted(entries, key, kind='prepared'):
    field = 'submission' if kind == 'prepared' else 'replacement'
    values = [e[field] for e in entries if e['kind'] == kind and e.get('key') == key]
    require(len(values) == 1, 'exactly one durable transaction identity required')
    return values[0]


def start(binary, config, state, log):
    return subprocess.Popen([str(binary), '--config', str(config), '--journal', str(state)],
                            cwd=ROOT, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                            start_new_session=True)


def stop(process):
    if process.poll() is None:
        process.kill()
    process.wait(timeout=5)


def ready(process, base, identity):
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        require(process.poll() is None, 'station exited before readiness')
        try:
            status, body = post(base + '/status', identity)
            if status == 200:
                return body
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.1)
    raise RuntimeError('station readiness exceeded startup recovery bound')


def canonical_receipt(endpoints, transaction_hash):
    receipt = rpc(endpoints, 'eth_getTransactionReceipt', [transaction_hash])
    require(receipt and receipt['transactionHash'].lower() == transaction_hash.lower(), 'exact receipt missing')
    block = rpc(endpoints, 'eth_getBlockByNumber', [receipt['blockNumber'], False])
    final = rpc(endpoints, 'eth_getBlockByNumber', ['finalized', False])
    require(block['hash'] == receipt['blockHash'] and block['number'] == receipt['blockNumber']
            and int(final['number'], 16) >= int(receipt['blockNumber'], 16), 'receipt is not canonical and finalized')
    return receipt


def prove_completion(endpoints, entries, quote, status, keccak):
    original = submitted(entries, quote['key'])
    outcome = status['completion']
    require(outcome and outcome['outcome'] in ('included', 'cancelled', 'reverted'), 'ambiguous completion cannot qualify')
    tx = original if outcome['outcome'] != 'cancelled' else submitted(entries, quote['key'], 'replaced')
    tx_hash = hexbytes(tx['hash'])
    require(outcome['transactionHash'] == tx_hash and keccak(bytes(tx['raw'])).hex() == tx_hash[2:],
            'completion changed durable transaction identity')
    receipt = canonical_receipt(endpoints, tx_hash)
    observed = [e for e in entries if e['kind'] == 'receipt_observed' and hexbytes(e['hash']) == tx_hash]
    require(len(observed) == 1 and observed[0]['receipt'] == receipt, 'exact finalized receipt was not retained')
    require(int(rpc(endpoints, 'eth_getTransactionCount', [hexbytes(quote['key']['sponsor']), 'finalized']), 16)
            > tx['nonce'], 'sponsor nonce remains unfinalized')
    require(receipt['from'].lower() == hexbytes(quote['key']['sponsor']), 'receipt sponsor differs')
    if outcome['outcome'] == 'included':
        require(receipt['to'].lower() == hexbytes(quote['account']) and int(receipt['status'], 16) == 1,
                'sponsored execution failed')
        topic = '0x' + keccak(b'Transfer(address,address,uint256)').hex()
        topics = [topic, '0x' + ('00' * 12) + hexbytes(quote['account'])[2:],
                  '0x' + ('00' * 12) + hexbytes(quote['key']['sponsor'])[2:]]
        transfer_logs = [row for row in receipt['logs'] if row['address'].lower() == hexbytes(quote['token'])
                         and row['topics'] == topics and not row.get('removed', False)]
        require(sum(int(row['data'], 16) for row in transfer_logs) == integer(quote['amount'])
                == int(outcome['sidCollected']), 'SID transfer amount differs')
        logs = rpc(endpoints, 'eth_getLogs', [{'address': hexbytes(quote['account']),
                   'fromBlock': receipt['blockNumber'], 'toBlock': 'finalized',
                   'topics': ['0x' + keccak(b'Sponsored(address,address,uint256,uint256)').hex()]}])
        matches = [row for row in logs if row['data'].lower() == hexbytes(quote['amount'] + quote['key']['quote_nonce'])]
        require(len(matches) == 1 and matches[0]['transactionHash'] == tx_hash, 'duplicate sponsorship or missing event')
    elif outcome['outcome'] == 'cancelled':
        require(receipt['to'].lower() == hexbytes(quote['key']['sponsor']) and int(receipt['status'], 16) == 1,
                'cancellation failed')
        require(rpc(endpoints, 'eth_getTransactionReceipt', [hexbytes(original['hash'])]) is None,
                'cancellation coexists with original execution')
    else:
        require(int(receipt['status'], 16) == 0, 'revert receipt disagrees')


def run_case(name, row, binary, evidence, keccak):
    require(set(row) == {'config', 'journal', 'base_url', 'observation_endpoints', 'expired_submission'}, 'scenario fields differ')
    config_path = protected(row['config']); config = document(config_path)
    local_url(row['base_url'])
    for endpoint in config['endpoints'] + row['observation_endpoints']:
        local_url(endpoint, secure=True)
    require(os.environ.get(config['relayer_key_env']), 'ephemeral sponsor key is absent')
    source = protected(row['journal']); entries = journal(source)
    require(all(e['kind'] not in ('completed', 'cancelled', 'released') for e in entries), 'scenario is already terminal or released')
    all_identities = identities(entries)
    identities_ = [(q, identity) for q, identity in all_identities
                   if any(e['kind'] == 'prepared' and e['key'] == q['key'] for e in entries)]
    require(identities_, 'no prepared identities')
    expired_request = document(row['expired_submission'])
    expired = [(q, identity) for q, identity in all_identities if not any(
        e['kind'] == 'prepared' and e['key'] == q['key'] for e in entries)]
    require(len(expired) == 1 and expired[0][0]['deadline'] < int(time.time()),
            'unsubmitted expired authorization case absent')
    require(expired_request['relayerSignature'] == expired[0][1]['relayerSignature']
            and expired_request['batch']['quote']['quoteNonce'] == expired[0][1]['quoteNonce'],
            'expired authorization is not the durable quote')
    quote, identity = identities_[0]
    original = submitted(entries, quote['key'])
    require(len(identities_) >= 2, 'scenario must also carry the next sponsor nonce')
    successor = submitted(entries, identities_[1][0]['key'])
    require(identities_[1][0]['key']['sponsor'] == quote['key']['sponsor']
            and successor['nonce'] == original['nonce'] + 1, 'next safe nonce case absent')
    endpoints = row['observation_endpoints']
    require(endpoints, 'independent observation endpoints absent')
    require(int(rpc(config['endpoints'], 'eth_chainId', []), 16) == config['chain_id'],
            'station RPC chain identity differs')
    if name in ('unreachable', 'divergent'):
        observations = []
        for endpoint in config['endpoints']:
            try:
                code, reply = post(endpoint, {'jsonrpc': '2.0', 'id': 1,
                    'method': 'eth_getTransactionReceipt', 'params': [hexbytes(original['hash'])]})
                if code == 200 and reply.get('id') == 1 and 'result' in reply and 'error' not in reply:
                    observations.append(json.dumps(reply['result'], sort_keys=True))
            except (OSError, urllib.error.URLError):
                pass
        if name == 'unreachable':
            require(not observations, 'unreachable scenario answered receipt queries')
        else:
            require(len(set(observations)) > 1 and all(observations.count(v) <= len(config['endpoints']) // 2
                    for v in observations), 'divergent scenario has no actual conflicting observations')
    if name in ('prepared', 'dropped'):
        require(rpc(endpoints, 'eth_getTransactionByHash', [hexbytes(original['hash'])]) is None,
                'unbroadcast boundary was not established')
    if name == 'broadcast':
        require(rpc(endpoints, 'eth_getTransactionByHash', [hexbytes(original['hash'])]) is not None,
                'broadcast boundary was not established')
    if name.startswith('replacement-'):
        replacement = submitted(entries, quote['key'], 'replaced')
        seen = rpc(endpoints, 'eth_getTransactionByHash', [hexbytes(replacement['hash'])])
        require((seen is None) == (name == 'replacement-prepared'), 'replacement boundary differs')
    if name == 'finality':
        canonical_receipt(endpoints, hexbytes(original['hash']))
    if name == 'dropped':
        require(quote['deadline'] < int(time.time()), 'dropped case must exercise expiry cancellation')
    directory = evidence / name; directory.mkdir(mode=0o700)
    state = directory / 'journal.jsonl'; state.write_bytes(source.read_bytes()); state.chmod(0o600)
    log_path = directory / 'process.log'
    with log_path.open('wb') as log:
        process = start(binary, config_path, state, log)
        try:
            if name == 'divergent':
                require(process.wait(timeout=45) != 0, 'divergent startup did not refuse')
                require('startup recovery' in log_path.read_text() and 'Divergence' in log_path.read_text(),
                        'startup refused for an unrelated reason')
                require(state.read_bytes() == source.read_bytes(), 'divergent startup mutated journal')
                return
            initial = ready(process, row['base_url'], identity)
            require(post(row['base_url'] + '/submit', expired_request) == (422, {'error': 'expired_quote'}),
                    'fresh expired authorization was not refused at expiry boundary')
            require(post(row['base_url'] + '/status', expired[0][1])[1]['state'] == 'quoted',
                    'expired authorization created a submission')
            competing = start(binary, config_path, state, log)
            try:
                require(competing.wait(timeout=10) != 0, 'competing journal writer was admitted')
                require('Locked' in log_path.read_text(), 'competing writer failed for an unrelated reason')
            finally:
                stop(competing)
            wrong = dict(identity, relayerSignature='0x' + '00' * 65)
            require(post(row['base_url'] + '/status', wrong)[0] == 422, 'unauthenticated status admitted')
            if name == 'unreachable':
                require(initial['completion'] is None, 'unreachable observation completed liability')
                time.sleep(1)
                require(post(row['base_url'] + '/status', identity)[1]['completion'] is None, 'unreachable liability lost')
                require(state.read_bytes() == source.read_bytes(), 'unreachable observation mutated liability')
                return
            stop(process)
            durable = state.read_bytes()
            process = start(binary, config_path, state, log)
            ready(process, row['base_url'], identity)
            deadline = time.monotonic() + 100
            statuses = []
            while time.monotonic() < deadline:
                require(process.poll() is None, 'restarted station exited')
                statuses = [post(row['base_url'] + '/status', ident)[1] for _, ident in identities_]
                if all(status.get('state') == 'completed' for status in statuses):
                    break
                time.sleep(0.2)
            require(statuses and all(s.get('state') == 'completed' for s in statuses), 'autonomous recovery did not finalize every nonce')
            require(state.read_bytes().startswith(durable), 'restart rewrote durable bytes')
            recovered = journal(state)
            for (saved_quote, ident), status in zip(identities_, statuses):
                prove_completion(endpoints, recovered, saved_quote, status, keccak)
                require(submitted(recovered, saved_quote['key']) == submitted(entries, saved_quote['key']), 'prepared bytes changed')
                require(post(row['base_url'] + '/retry', ident) == (200, status), 'same-identity terminal retry changed result')
        finally:
            stop(process)
    corrupt = directory / 'corrupt.jsonl'; corrupt.write_bytes(state.read_bytes() + b'{'); corrupt.chmod(0o600)
    with log_path.open('ab') as log:
        refused = start(binary, config_path, corrupt, log)
        try:
            require(refused.wait(timeout=10) != 0, 'corrupt journal started')
            require('Corrupt' in log_path.read_text(), 'corrupt journal failed for an unrelated reason')
        finally:
            stop(refused)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['station-autonomous-recovery'])
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location('candidate', ROOT / 'tools/paxeer-x/candidate.py')
    candidate = importlib.util.module_from_spec(spec); spec.loader.exec_module(candidate)
    manifest = candidate.load_private(args.candidate_manifest)
    candidate.validate(manifest, candidate.catalogue(ROOT / 'spec/paxeer-x/spec.kvx'), ROOT)
    require(not manifest['source']['dirty'], 'clean candidate required')
    material = document(os.environ['PAXEER_X_STATION_RECOVERY_MATERIAL'])
    require(material['source_revision'] == manifest['source']['revision'] and material['build_exit'] == 0,
            'source-bound successful binary build required')
    binary = protected(material['binary'])
    require(os.access(binary, os.X_OK) and digest(binary) == material['binary_sha256'], 'binary identity mismatch')
    require(set(material['scenarios']) == set(SCENARIOS), 'required restart/refusal scenario missing')
    evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR']).resolve()
    info = evidence.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
            'private evidence directory required')
    from eth_hash.auto import keccak
    count = 0
    for name in SCENARIOS:
        run_case(name, material['scenarios'][name], binary, evidence, keccak)
        count += 1
        print('passed ' + name, flush=True)
    print(f'PAXEER_X_GATE tests={count} skipped=0')


if __name__ == '__main__':
    os.umask(0o077)
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print('station recovery qualification refused: ' + type(error).__name__, file=sys.stderr)
        sys.exit(1)
