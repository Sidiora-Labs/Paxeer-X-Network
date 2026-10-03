#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import socket
import signal
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



FIRST_USE_DRIVER = r"""
import { readFileSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
const input = JSON.parse(readFileSync(process.argv[2], 'utf8'));
const { PaxeerProvider } = await import(pathToFileURL(input.provider));
const { gasStation } = await import(pathToFileURL(input.wallet));
const sdk = await import(pathToFileURL(input.agent));
const require = (value, reason) => { if (!value) throw new Error(reason); };
const json = value => JSON.stringify(value, (_, item) => typeof item === 'bigint' ? item.toString() : item);
const clone = value => JSON.parse(JSON.stringify(value));
const post = async (path, body) => {
  const response = await fetch(input.base_url + path, { method: 'POST', headers: { 'content-type': 'application/json' },
    body: json(body), signal: AbortSignal.timeout(15000), redirect: 'error' });
  return [response.status, await response.json()];
};

if (input.mode === 'native') {
  const outcomes = [];
  for (const row of input.scenarios) {
    const observed = [];
    const request = async ({ method, params }) => {
      require(['eth_chainId', 'eth_getBlockByNumber', 'eth_call'].includes(method), 'native preference attempted a transaction');
      observed.push(method);
      const response = await fetch(row.rpc_url, { method: 'POST', headers: { 'content-type': 'application/json' },
        body: json({ jsonrpc: '2.0', id: 1, method, params }), signal: AbortSignal.timeout(15000), redirect: 'error' });
      const reply = await response.json(); require(response.ok && !reply.error && reply.id === 1, 'native real RPC refused');
      return reply.result;
    };
    const preference = await sdk.readNativeFeePreference(row.account, { restUrl: row.rest_url, request });
    require(preference.state === row.expected_state, 'native availability differs from actual governed state');
    if (preference.state === 'available') {
      require(preference.chainId === 125n && preference.decimals === 6 && preference.symbol === 'SID'
        && preference.denom === row.registered_denom && preference.call.to === '0x0000000000000000000000000000000000001018'
        && preference.call.value === 0n, 'native preference projection differs');
    } else { require(preference.reason === row.expected_reason, 'native inactive/unavailable reason differs'); }
    require(observed.every(method => method !== 'eth_sendTransaction'), 'native preference executed');
    outcomes.push(preference);
  }
  writeFileSync(input.output, json(outcomes), { mode: 0o600 }); process.exit(0);
}
if (input.mode === 'direct') {
  const provider = new PaxeerProvider({ gatewayUrl: input.gateway_url, rpcUrl: input.rpc_url, chainId: 125,
    token: () => process.env[input.token_env], confirm: ({ method }) => method === 'eth_sendTransaction' });
  const accounts = await provider.request({ method: 'eth_requestAccounts' });
  require(accounts.some(account => account.toLowerCase() === input.account.toLowerCase()), 'direct wallet custody differs');
  const transactionHash = await provider.request({ method: 'eth_sendTransaction', params: [{ from: input.account,
    to: input.account, value: '0x0', data: input.data }] });
  writeFileSync(input.output, json({ transactionHash }), { mode: 0o600 }); process.exit(0);
}

const token = process.env[input.token_env]; require(token, 'real wallet gateway session required');
let signs = 0; let approved = null; let sent = null; let expiryProbe = true;
const provider = new PaxeerProvider({ gatewayUrl: input.gateway_url, rpcUrl: input.rpc_url, chainId: 125,
  token: () => token, confirm: () => { require(approved !== null, 'signing preceded explicit SID consent'); signs++; return true; } });
const accounts = await provider.request({ method: 'eth_requestAccounts' });
require(accounts.includes(input.account.toLowerCase()) || accounts.includes(input.account), 'wallet custody account differs');
const config = { chainId: 125n, sponsor: input.sponsor, paymaster: input.paymaster, quoteUrl: input.base_url + '/quote' };
const refused = [];
const station = gasStation(provider, { ...config, fetch: async (url, options) => {
  if (String(url).endsWith('/submit')) {
    const body = JSON.parse(options.body);
    if (expiryProbe) {
      const wait = Number(BigInt(body.batch.quote.deadline) - BigInt(Math.floor(Date.now() / 1000)) + 1n);
      require(wait >= 0 && wait <= 25, 'qualification quote expiry exceeds bound');
      await new Promise(resolve => setTimeout(resolve, wait * 1000));
      const reply = await fetch(url, options); const payload = await reply.clone().json();
      require(reply.status === 422 && payload.error === 'expired_quote', 'expired fresh signature admitted');
      expiryProbe = false; refused.push('expired_quote'); return reply;
    }
    const variants = [];
    const vary = (name, change) => { const value = clone(body); change(value); variants.push([name, value]); };
    vary('wrong_chain', value => { value.authorization.chainId = '126'; });
    vary('wrong_delegate', value => { value.authorization.address = '0x0000000000000000000000000000000000000001'; });
    vary('wrong_account', value => { value.batch.account = '0x0000000000000000000000000000000000000001'; });
    vary('wrong_sponsor', value => { value.batch.quote.sponsor = '0x0000000000000000000000000000000000000001'; });
    vary('wrong_token', value => { value.batch.quote.token = '0x0000000000000000000000000000000000000001'; });
    vary('above_maximum', value => { value.batch.quote.maxTokenAmount = '0'; });
    vary('invalid_signature', value => { value.authorization.r = '0x' + '00'.repeat(32); });
    vary('consumed_authorization_nonce', value => { value.authorization.nonce = (BigInt(value.authorization.nonce) + 1n).toString(); });
    vary('high_s', value => {
      const order = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141n;
      value.authorization.s = '0x' + (order - BigInt(value.authorization.s)).toString(16).padStart(64, '0');
      value.authorization.yParity = 1 - value.authorization.yParity;
    });
    const before = readFileSync(input.journal, 'utf8');
    const sponsorNonce = await provider.request({ method: 'eth_getTransactionCount', params: [input.sponsor, 'pending'] });
    for (const [name, value] of variants) {
      const [status] = await post('/submit', value);
      require(status === 422 || status === 409, name + ' not refused'); refused.push(name);
    }
    require(readFileSync(input.journal, 'utf8') === before, 'refused requests changed durable liabilities');
    require(await provider.request({ method: 'eth_getTransactionCount', params: [input.sponsor, 'pending'] }) === sponsorNonce,
      'refused requests funded a transaction');
    sent = body;
  }
  return fetch(url, options);
} });
const nonce = await station.batchNonce(input.account);
require(nonce === BigInt(input.expected_nonce), 'retained replay nonce differs');
const calls = input.calls.map(call => ({ ...call, value: BigInt(call.value) }));
const request = { account: input.account, nonce, calls, maxTokenAmount: BigInt(input.maximum), gasCost: BigInt(input.gas_cost) };
const signed = await station.requestQuote(request);
const batch = { chainId: 125n, account: input.account, nonce, calls, quote: signed.quote };
const consent = value => {
  require(value.kind === 'sponsored-eip7702' && value.symbol === 'SID' && value.decimals === 6
    && value.amount > 0n && value.amount <= value.maximum && value.maximum === BigInt(input.maximum)
    && value.deadline === batch.quote.deadline && / SID$/.test(value.amountDisplay), 'SID consent projection differs');
  approved = value; return true;
};
let declined = false;
try { await station.submitFirstUse(batch, signed.relayerSignature, { confirm: () => false }); } catch (error) {
  declined = error.refusal?.field === 'consent';
}
require(declined && signs === 0, 'declined consent signed material');
try { await station.submitFirstUse(batch, signed.relayerSignature, { confirm: consent }); } catch (error) {
  require(error.refusal?.code === 'expired_quote' && !expiryProbe, 'expiry probe failed before real station');
}
require(!readFileSync(input.journal, 'utf8').split('\n').filter(Boolean).map(JSON.parse).some(entry => entry.kind === 'prepared'),
  'expired authorization prepared transaction');
const fresh = await station.requestQuote(request);
const next = { ...batch, quote: fresh.quote };
approved = null;
const transactionHash = await station.submitFirstUse(next, fresh.relayerSignature, { confirm: value => {
  require(value.amount === fresh.quote.tokenAmount && value.maximum === fresh.quote.maxTokenAmount
    && value.deadline === fresh.quote.deadline && value.batchDigest === station.digest(next), 'fresh consent differs');
  approved = value; return true;
} });
require(sent !== null && approved !== null && signs === 4, 'real wallet signature path incomplete');
const sdkConsent = sdk.sponsoredConsent(station.config, next);
require(sdkConsent.ok && sdkConsent.value.batchDigest === approved.batchDigest, 'agent and wallet consent digests differ');
writeFileSync(input.output, json({ transactionHash, request: sent, consent: approved, refused, nonce }), { mode: 0o600 });
"""


def first_use_material(material, manifest):
    require(material['source_revision'] == manifest['source']['revision'] and material['build_exit'] == 0,
            'source-bound successful build required')
    for name in ('binary', 'provider', 'wallet', 'agent'):
        path = Path(material[name])
        require(path.is_absolute() and path.is_file() and not path.is_symlink()
                and not any(part == '.env' or part.startswith('.env.') for part in path.parts)
                and digest(path) == material[name + '_sha256'], 'built artifact identity mismatch')
    require(os.access(material['binary'], os.X_OK), 'station binary not executable')
    require(set(material['scenarios']) == {'first-use', 'retained-storage', 'sid-transfer-revert', 'incompatible-delegation'},
            'first-use acceptance scenario missing')


def run_first_use(name, row, material, evidence, keccak):
    config_path = protected(row['config']); config = document(config_path)
    endpoints = row['observation_endpoints']
    require(len(endpoints) >= 3 and len(set(endpoints)) == len(endpoints), 'independent observation quorum required')
    for endpoint in config['endpoints'] + endpoints:
        local_url(endpoint, secure=True)
    local_url(row['base_url']); local_url(row['gateway_url'])
    local_url(row['rpc_url'], secure=True)
    require(row['rpc_url'] in config['endpoints'] and config['chain_id'] == 125, 'actual chain configuration differs')
    require(8 <= config['interval_seconds'] <= 10, 'bounded real expiry interval required')
    require(rpc(endpoints, 'eth_chainId', []) == '0x7d', 'chain 125 required')
    account = row['account'].lower(); sponsor = row['sponsor'].lower(); token = config['token'].lower()
    require(token == '0x21f7b20a555199fa73a238b1a91fd0f549068fee' and config['decimals'] == 6,
            'exact six-decimal SID required')
    code = rpc(endpoints, 'eth_getCode', [account, 'finalized'])
    require(code == '0x' if name != 'incompatible-delegation' else code not in ('0x', '0xef0100' + config['paymaster'][2:].lower()),
            'real delegation scenario precondition missing')
    require(int(rpc(endpoints, 'eth_getBalance', [account, 'finalized']), 16) == 0, 'first-use account must have no PAX')
    balance_call = {'to': token, 'data': '0x' + keccak(b'balanceOf(address)')[:4].hex() + '00' * 12 + account[2:]}
    balance = int(rpc(endpoints, 'eth_call', [balance_call, 'finalized']), 16)
    require(balance >= int(row['maximum']) > 0, 'real SID funding insufficient')
    require(row['probes'] and len(row['probes']) <= 16, 'authorized-call observations required')
    before = [rpc(endpoints, 'eth_call', [probe['call'], 'finalized']) for probe in row['probes']]
    directory = evidence / name; directory.mkdir(mode=0o700)
    state = directory / 'journal.jsonl'; state.touch(mode=0o600)
    driver = directory / 'driver.mjs'; driver.write_text(FIRST_USE_DRIVER)
    output = directory / 'sdk-result.json'; input_path = directory / 'input.json'
    request = dict(row, provider=material['provider'], wallet=material['wallet'], agent=material['agent'],
                   sponsor=row['sponsor'], paymaster=config['paymaster'], output=str(output), journal=str(state))
    input_path.write_text(json.dumps(request)); input_path.chmod(0o600)
    with (directory / 'process.log').open('wb') as log:
        process = start(Path(material['binary']), config_path, state, log)
        try:
            deadline = time.monotonic() + 45
            while time.monotonic() < deadline:
                require(process.poll() is None, 'station exited before readiness')
                try:
                    if post(row['base_url'] + '/status', {})[0] == 400:
                        break
                except (OSError, urllib.error.URLError):
                    pass
                time.sleep(0.1)
            else:
                raise RuntimeError('station readiness timeout')
            if name == 'incompatible-delegation':
                body = {'account': account, 'nonce': row['expected_nonce'], 'calls': row['calls'],
                        'maxTokenAmount': row['maximum'], 'gasCost': row['gas_cost'], 'chainId': '125',
                        'token': config['token'], 'decimals': 6}
                require(post(row['base_url'] + '/quote', body) == (422, {'error': 'incompatible_delegation'}),
                        'incompatible delegation admitted')
                require(state.read_bytes() == b'', 'incompatible delegation created liabilities')
                return
            with (directory / 'sdk.log').open('wb') as sdk_log:
                completed = subprocess.run(['node', str(driver), str(input_path)], cwd=ROOT, stdin=subprocess.DEVNULL,
                                           stdout=sdk_log, stderr=sdk_log, timeout=70, check=False)
            require(completed.returncode == 0, 'real wallet/agent first-use driver failed')
            answer = document(output)
            require(set(answer['refused']) == {'expired_quote', 'wrong_chain', 'wrong_delegate', 'wrong_account',
                    'wrong_sponsor', 'wrong_token', 'above_maximum', 'invalid_signature', 'consumed_authorization_nonce', 'high_s'},
                    'pre-funding refusal coverage incomplete')
            entries = journal(state)
            submitted_quotes = [(quote, identity) for quote, identity in identities(entries)
                                if any(entry['kind'] == 'prepared' and entry['key'] == quote['key'] for entry in entries)]
            require(len(submitted_quotes) == 1, 'exactly one real submitted quote required')
            quote, identity = submitted_quotes[0]
            original = submitted(entries, quote['key'])
            require(hexbytes(original['hash']) == answer['transactionHash'], 'SDK identity differs from journal')
            stop(process); durable = state.read_bytes()
            process = start(Path(material['binary']), config_path, state, log)
            ready(process, row['base_url'], identity)
            deadline = time.monotonic() + 100; status = None
            while time.monotonic() < deadline:
                require(process.poll() is None, 'station exited during recovery')
                http, status = post(row['base_url'] + '/status', identity)
                if http == 200 and status.get('state') == 'completed':
                    break
                time.sleep(0.2)
            require(status and status['state'] == 'completed', 'real sponsored transaction did not finalize')
            recovered = journal(state)
            require(state.read_bytes().startswith(durable) and submitted(recovered, quote['key']) == original,
                    'restart changed exact durable transaction')
            prove_completion(endpoints, recovered, quote, status, keccak)
            expected = 'reverted' if name == 'sid-transfer-revert' else 'included'
            require(status['completion']['outcome'] == expected, 'real execution outcome differs')
            require(post(row['base_url'] + '/retry', identity) == (200, status), 'terminal retry changed identity')
            after = [rpc(endpoints, 'eth_call', [probe['call'], 'finalized']) for probe in row['probes']]
            nonce_call = {'to': account, 'data': '0x' + keccak(b'nonce()')[:4].hex()}
            nonce = int(rpc(endpoints, 'eth_call', [nonce_call, 'finalized']), 16)
            initial_nonce = int(row['expected_nonce'])
            require(initial_nonce > 0 if name == 'retained-storage' else initial_nonce == 0, 'retained nonce precondition missing')
            require(nonce == initial_nonce + (expected == 'included'), 'batch replay nonce changed incorrectly')
            require(rpc(endpoints, 'eth_getCode', [account, 'finalized']).lower() == '0xef0100' + config['paymaster'][2:].lower(),
                    'actual EIP-7702 delegation missing')
            if expected == 'reverted':
                trace = rpc(endpoints, 'debug_traceTransaction', [answer['transactionHash'],
                            {'tracer': 'callTracer', 'timeout': '5s'}])
                require(trace.get('error') and trace.get('output', '').lower()
                        == '0x' + keccak(b'TokenTransferFailed()')[:4].hex(),
                        'canonical execution did not fail specifically on SID repayment')
                require(after == before and int(rpc(endpoints, 'eth_call', [balance_call, 'finalized']), 16) == balance,
                        'failed SID transfer did not atomically roll back calls and token state')
                require(len(row['calls']) >= 2 and row['calls'][-1]['to'].lower() == token,
                        'real repayment-failure scenario must exhaust SID after a prior authorized mutation')
                transfer = '0x' + keccak(b'transfer(address,uint256)')[:4].hex()
                data = row['calls'][-1]['data']
                require(data.startswith(transfer) and len(data) == 138 and int(data[-64:], 16) == balance,
                        'repayment-failure scenario must transfer the actual entire SID balance')
                require(all(int(call['value']) == 0 for call in row['calls']), 'rollback case requires zero-PAX calls')
                used_call = {'to': account, 'data': '0x' + keccak(b'usedQuoteNonces(address,uint256)')[:4].hex()
                             + '00' * 12 + sponsor[2:] + integer(quote['key']['quote_nonce']).to_bytes(32, 'big').hex()}
                require(int(rpc(endpoints, 'eth_call', [used_call, 'finalized']), 16) == 0,
                        'failed repayment consumed the quote nonce')
            else:
                require(after == [probe['expected'] for probe in row['probes']] and after != before,
                        'authorized batch calls did not execute exactly')
                replay = {'account': account, 'nonce': row['expected_nonce'], 'calls': row['calls'],
                          'maxTokenAmount': row['maximum'], 'gasCost': row['gas_cost'], 'chainId': '125',
                          'token': config['token'], 'decimals': 6}
                durable_completed = state.read_bytes()
                require(post(row['base_url'] + '/quote', replay) == (409, {'error': 'conflict'}),
                        'consumed batch nonce admitted for a fresh quote')
                require(state.read_bytes() == durable_completed, 'consumed nonce created another liability')
        finally:
            stop(process)


def run_sdk_observation(mode, request, material, evidence):
    directory = evidence / mode; directory.mkdir(mode=0o700)
    driver = directory / 'driver.mjs'; driver.write_text(FIRST_USE_DRIVER)
    output = directory / 'sdk-result.json'; inputs = directory / 'input.json'
    request = dict(request, mode=mode, provider=material['provider'], wallet=material['wallet'], agent=material['agent'], output=str(output))
    inputs.write_text(json.dumps(request)); inputs.chmod(0o600)
    with (directory / 'sdk.log').open('wb') as log:
        result = subprocess.run(['node', str(driver), str(inputs)], cwd=ROOT, stdin=subprocess.DEVNULL,
                                stdout=log, stderr=log, timeout=100, check=False)
    require(result.returncode == 0, 'real SDK observation failed: ' + mode)
    return document(output)


def direct_call(calls, keccak):
    require(0 < len(calls) <= 64, 'bounded actual direct batch required')
    word = lambda value: int(value).to_bytes(32, 'big')
    tuples = []
    for call in calls:
        address = bytes.fromhex(call['to'][2:]); data = bytes.fromhex(call['data'][2:])
        require(len(address) == 20 and len(data) <= 65_536 and int(call['value']) == 0, 'direct call bounds differ')
        tuples.append(b'\0' * 12 + address + word(call['value']) + word(96) + word(len(data))
                      + data + b'\0' * (-len(data) % 32))
    offset = len(tuples) * 32; offsets = []
    for item in tuples:
        offsets.append(word(offset)); offset += len(item)
    return '0x' + (keccak(b'execute((address,uint256,bytes)[])')[:4] + word(32) + word(len(tuples))
                   + b''.join(offsets) + b''.join(tuples)).hex()


def direct_execution(row, material, evidence, keccak):
    endpoints = row['observation_endpoints']
    require(len(endpoints) >= 3 and len(set(endpoints)) == len(endpoints), 'direct observation quorum required')
    for endpoint in endpoints + [row['rpc_url']]:
        local_url(endpoint, secure=True)
    local_url(row['gateway_url'])
    require(rpc(endpoints, 'eth_chainId', []) == '0x7d', 'direct chain differs')
    account = row['account']; nonce_call = {'to': account, 'data': '0x' + keccak(b'nonce()')[:4].hex()}
    require(rpc(endpoints, 'eth_getCode', [account, 'finalized']).lower() == '0xef0100' + row['paymaster'][2:].lower(),
            'direct path requires genuine existing delegation')
    require(int(rpc(endpoints, 'eth_getBalance', [account, 'finalized']), 16) > 0, 'direct payer requires real PAX')
    nonce = int(rpc(endpoints, 'eth_call', [nonce_call, 'finalized']), 16)
    require(row['probes'], 'direct authorized-call observations required')
    before = [rpc(endpoints, 'eth_call', [probe['call'], 'finalized']) for probe in row['probes']]
    data = direct_call(row['calls'], keccak)
    answer = run_sdk_observation('direct', dict(row, data=data), material, evidence)
    deadline = time.monotonic() + 100; receipt = None
    while time.monotonic() < deadline:
        receipt = rpc(endpoints, 'eth_getTransactionReceipt', [answer['transactionHash']])
        if receipt and int(rpc(endpoints, 'eth_getBlockByNumber', ['finalized', False])['number'], 16) >= int(receipt['blockNumber'], 16):
            break
        time.sleep(0.2)
    receipt = canonical_receipt(endpoints, answer['transactionHash'])
    transaction = rpc(endpoints, 'eth_getTransactionByHash', [answer['transactionHash']])
    require(transaction['from'].lower() == account.lower() == transaction['to'].lower()
            and transaction['input'].lower() == data and int(receipt['status'], 16) == 1,
            'canonical direct execution differs')
    require(int(rpc(endpoints, 'eth_call', [nonce_call, 'finalized']), 16) == nonce + 1, 'direct nonce differs')
    after = [rpc(endpoints, 'eth_call', [probe['call'], 'finalized']) for probe in row['probes']]
    require(after == [probe['expected'] for probe in row['probes']] and after != before, 'direct calls did not execute')
    require(not any(log['topics'] and log['topics'][0] == '0x' + keccak(b'Sponsored(address,address,uint256,uint256)').hex()
                    for log in receipt['logs']), 'direct execution unexpectedly charged a sponsored quote')


def native_preference_cases(rows, material, evidence):
    require(set(rows) == {'enabled', 'disabled', 'not-upgraded', 'stale-rate'}, 'real native activation coverage missing')
    expected = {'enabled': ('available', None), 'disabled': ('inactive', 'fee_token_disabled'),
                'not-upgraded': ('inactive', 'upgrade_not_applied'), 'stale-rate': ('inactive', 'stale_rate')}
    for name, row in rows.items():
        local_url(row['rpc_url'], secure=True); local_url(row['rest_url'])
        require(row['expected_state'] == expected[name][0] and row.get('expected_reason') == expected[name][1],
                'native expected state cannot be substituted')
    run_sdk_observation('native', {'scenarios': list(rows.values())}, material, evidence)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['station-autonomous-recovery', 'first-use-sponsorship'])
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location('candidate', ROOT / 'tools/paxeer-x/candidate.py')
    candidate = importlib.util.module_from_spec(spec); spec.loader.exec_module(candidate)
    manifest = candidate.load_private(args.candidate_manifest)
    candidate.validate(manifest, candidate.catalogue(ROOT / 'spec/paxeer-x/spec.kvx'), ROOT)
    require(not manifest['source']['dirty'], 'clean candidate required')
    if args.case == 'first-use-sponsorship':
        def interrupted(_number, _frame):
            raise KeyboardInterrupt()
        signal.signal(signal.SIGTERM, interrupted)
        material = document(os.environ['PAXEER_X_STATION_FIRST_USE_MATERIAL'])
        first_use_material(material, manifest)
        evidence = Path(os.environ['PAXEER_X_EVIDENCE_DIR']).resolve()
        info = evidence.stat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                'private evidence directory required')
        from eth_hash.auto import keccak
        for name, row in material['scenarios'].items():
            run_first_use(name, row, material, evidence, keccak)
            print('passed ' + name, flush=True)
        direct_execution(material['direct'], material, evidence, keccak)
        native_preference_cases(material['native'], material, evidence)
        print('PAXEER_X_GATE tests=6 skipped=0')
        return
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
