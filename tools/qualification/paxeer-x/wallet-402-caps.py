#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import signal
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
STATE = None
PROCESSES = []
PG_STOP = None
CA = None
CASES = []


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def private(path):
    path = Path(path)
    info = path.lstat()
    require(path.is_file() and not path.is_symlink() and info.st_mode & 0o077 == 0,
            'protected fixture file required')
    require(path.name != '.env' and not path.name.startswith('.env.'), 'environment files are not fixture artifacts')
    return path


def local(url):
    value = urllib.parse.urlparse(url)
    require(value.hostname in ('localhost', '127.0.0.1', '::1') and value.scheme in ('http', 'https'),
            'isolated loopback service required')
    return url


def port():
    with socket.socket() as stream:
        stream.bind(('127.0.0.1', 0))
        return stream.getsockname()[1]


def command(argv, env=None):
    subprocess.run(argv, env=env, check=True, timeout=90, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def start(argv, env, name):
    with (STATE / (name + '.log')).open('ab', buffering=0) as log:
        process = subprocess.Popen(argv, env=env, cwd=ROOT, stdout=log, stderr=log, start_new_session=True)
    PROCESSES.append(process)
    return process


def stop(process):
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)


def http(url, token=None, body=None):
    local(url)
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers=headers)
    try:
        response = urllib.request.urlopen(request, context=CA, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        data = response.read(4 * 1024 * 1024 + 1)
        require(len(data) <= 4 * 1024 * 1024, 'bounded HTTP response required')
        return response.status, json.loads(data)


def rpc(url, token, method, params):
    status, value = http(url, token, {'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params})
    require(status == 200 and value.get('jsonrpc') == '2.0' and value.get('id') == 1, 'real unified RPC response required')
    return value


def wait_ready(process, url):
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        require(process.poll() is None, 'real service exited before readiness')
        try:
            if http(url)[0] == 200:
                return
        except (OSError, ValueError, urllib.error.URLError):
            pass
        time.sleep(0.25)
    raise RuntimeError('real service readiness deadline')


def snapshot(url, account):
    result = rpc(url, account['token'], 'lx_getWalletCaps', [{'address': account['address'], 'chain_id': account['chain_id']}])
    require('result' in result and 'error' not in result, 'verified caps read failed')
    value = result['result']
    require(value['did'] == account['did'] and value['account_id'] == account['account_id'], 'caps identity substitution')
    require(value['context']['address'] == account['address'] and value['context']['chain_id'] == account['chain_id'], 'caps connection substitution')
    require(value['observation']['verification'] in ('state_proven', 'checkpoint_finalised', 'settlement_anchored'), 'caps verification required')
    require(all(row['owner'] == account['account_id'] for row in value['budgets'] + value['grants']), 'foreign prefix data disclosed')
    require((value['state'] == 'empty') == (not value['budgets'] and not value['grants']), 'unverified empty state')
    return value


def expect_error(value, code):
    require(value.get('error', {}).get('code') == code and 'result' not in value, 'required refusal was not observed')


def exact(value, expected):
    for kind in ('budgets', 'grants'):
        actual = {row['id']: row for row in value[kind]}
        require(set(actual) == set(expected[kind]), 'caps record omission or foreign disclosure')
        for identifier, fields in expected[kind].items():
            require(all(actual[identifier].get(key) == wanted for key, wanted in fields.items()), 'verified cap differs from signed fixture operation')


SDK_CASES = r'''
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
const c = JSON.parse(await readFile(process.argv[2], 'utf8'));
const { PaxeerProvider, readWalletCaps, decodeWalletCaps } = await import(pathToFileURL(c.sdk));
let token = c.owner.token;
let announce = null;
const provider = new PaxeerProvider({ gatewayUrl: c.gateway_url, rpcUrl: c.rpc_url, token: () => token,
  fetch: (...args) => {
    const pending = fetch(...args);
    if (args[1]?.body && JSON.parse(args[1].body).method === 'lx_getWalletCaps' && announce) { announce(); announce = null; }
    return pending;
  } });
await provider.request({ method: 'eth_requestAccounts' });
const observed = await readWalletCaps(provider, c.owner.address);
assert.equal(observed.account_id, c.owner.account_id);
assert.equal(observed.state, 'ready');
for (const kind of ['budgets', 'grants']) {
  assert.deepEqual(observed[kind].map(v => v.id).sort(), Object.keys(c.expected[kind]).sort());
  for (const row of observed[kind]) for (const [key, value] of Object.entries(c.expected[kind][row.id])) assert.equal(row[key], value);
}
for (const change of [
  v => { v.context.address = c.empty.address; },
  v => { v.context.chain_id += 1; },
  v => { v.context.expires_at = '1'; },
  v => { v.budgets[0].owner = c.empty.account_id; },
  v => { v.budgets[0].limit = (1n << 128n).toString(); },
  v => { v.grants[0].allowance = 1; },
  v => { v.state = 'empty'; },
  v => { v.observation.verification = 'node_claimed'; },
  v => { v.grants.push(v.grants[0]); },
]) {
  const changed = structuredClone(observed); change(changed);
  assert.throws(() => decodeWalletCaps(changed, c.owner.address, c.owner.chain_id));
}
const entered = new Promise(resolve => { announce = resolve; });
const pending = readWalletCaps(provider, c.owner.address);
const refused = assert.rejects(pending);
await entered;
token = c.empty.token;
provider.invalidateCapsSession();
await refused;
await provider.request({ method: 'eth_requestAccounts' });
const empty = await readWalletCaps(provider, c.empty.address);
assert.equal(empty.state, 'empty');
assert.equal(empty.account_id, c.empty.account_id);
assert.notEqual(empty.context.principal, observed.context.principal);
assert.notEqual(empty.context.session_id, observed.context.session_id);
provider.disconnect();
await assert.rejects(readWalletCaps(provider, c.empty.address));
console.log(JSON.stringify(['typed-populated', 'decoder-bounds-and-foreign-refusals', 'inflight-session-discard', 'account-switch-empty', 'disconnect-refused']));
'''

BROWSER_CASES = r'''
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
const c = JSON.parse(await readFile(process.argv[2], 'utf8'));
const { chromium } = await import(pathToFileURL(c.playwright));
const browser = await chromium.launch({ executablePath: c.chromium, headless: true });
try {
  const context = await browser.newContext({ storageState: c.owner.storage_state });
  const page = await context.newPage();
  await page.addInitScript(() => {
    window.__capsStates = [];
    new MutationObserver(() => {
      const state = document.querySelector('[data-caps-state]')?.getAttribute('data-caps-state');
      if (state && window.__capsStates.at(-1) !== state) window.__capsStates.push(state);
    }).observe(document, { childList: true, subtree: true, attributes: true });
  });
  await page.goto(c.ui_url);
  const caps = page.locator('[data-caps-state]');
  await caps.locator('[data-budget="' + Object.keys(c.expected.budgets)[0] + '"]').waitFor();
  await caps.locator('[data-grant="' + Object.keys(c.expected.grants)[0] + '"]').waitFor();
  assert.equal(await caps.getAttribute('data-caps-state'), 'ready');
  assert.match(await caps.locator('[data-role="caps-account"]').innerText(), new RegExp(c.owner.account_id));
  assert.equal(await caps.locator('[data-role="caps-empty"]').count(), 0);
  assert.equal(await page.evaluate(() => window.__capsStates.includes('loading')), true);
  await writeFile(c.control + '/ui-ready', 'ready');
  let changed;
  const deadline = Date.now() + 150_000;
  while (Date.now() < deadline) {
    try { changed = JSON.parse(await readFile(c.control + '/mutation-complete.json', 'utf8')); break; }
    catch (error) { if (error.code !== 'ENOENT') throw error; await new Promise(resolve => setTimeout(resolve, 100)); }
  }
  assert.ok(changed);
  for (const [id, fields] of Object.entries(changed.budgets)) {
    await page.waitForFunction(({ id, limit }) => document.querySelector('[data-budget="' + id + '"] [data-role="budget-cap"]')?.textContent.includes('Period cap: ' + limit + ' units'), { id, limit: fields.limit });
  }
  for (const [id, fields] of Object.entries(changed.grants)) {
    if (fields.revoked) await page.waitForFunction(id => document.querySelector('[data-grant="' + id + '"]')?.textContent.includes('Revoked'), id);
  }
  const other = JSON.parse(await readFile(c.empty.storage_state, 'utf8'));
  const origin = new URL(c.ui_url).origin;
  const entries = other.origins.find(v => v.origin === origin)?.localStorage;
  assert.ok(entries && entries.length);
  const auth = entries.find(v => /^sb-.*-auth-token$/.test(v.name));
  assert.ok(auth);
  await page.evaluate(({ entries, auth }) => {
    for (const row of entries) localStorage.setItem(row.name, row.value);
    const channel = new BroadcastChannel(auth.name);
    channel.postMessage({ event: 'SIGNED_IN', session: JSON.parse(auth.value) });
    channel.close();
  }, { entries, auth });
  await page.waitForFunction(() => !document.querySelector('[data-budget], [data-grant]'));
  assert.notEqual(await caps.getAttribute('data-caps-state'), 'empty');
  await page.reload();
  await page.locator('[data-caps-state="empty"] [data-role="caps-empty"]').waitFor();
  assert.equal(await page.locator('[data-budget], [data-grant]').count(), 0);
  assert.match(await page.locator('[data-role="caps-account"]').innerText(), new RegExp(c.empty.account_id));
  await context.close();
  console.log(JSON.stringify(['ui-loading', 'ui-verified-values', 'ui-mutation-refresh', 'ui-session-invalidated', 'ui-reload-fresh-empty']));
} finally { await browser.close(); }
'''


def main():
    global STATE, PG_STOP, CA
    os.umask(0o077)
    argument = os.environ.get('WALLET_CAPS_FIXTURE_BUNDLE')
    require(argument, 'prerequisite: protected WALLET_CAPS_FIXTURE_BUNDLE with real services, sessions and custody-signed activities')
    bundle = Path(argument).resolve()
    require(bundle.is_dir() and bundle.stat().st_mode & 0o077 == 0, 'protected fixture bundle directory required')
    require(not any(p.name == '.env' or p.name.startswith('.env.') for p in bundle.rglob('*')), 'environment files are not fixture artifacts')
    config = json.loads(private(bundle / 'wallet-caps-fixture.json').read_text())
    require(config.get('version') == 1 and config.get('isolated') is True, 'isolated fixture version 1 required')
    require(config['revision'] == subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(), 'source-bound prebuilt revision required')
    require(config['source_tree'] == subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip(), 'source-bound prebuilt tree required')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'source changed since bounded build')
    artifacts = config['artifacts']
    for artifact in artifacts.values():
        path = Path(artifact['path'])
        require(path.is_absolute() and path.is_file() and not path.is_symlink() and '.env' not in path.name, 'prebuilt artifact missing')
        require(hashlib.sha256(path.read_bytes()).hexdigest() == artifact['sha256'], 'prebuilt artifact digest mismatch')
    required = {'node', 'wallet_gateway', 'wallet_sdk', 'wallet_ui', 'core', 'gateway', 'identity', 'clock', 'layerxd', 'playwright', 'chromium'}
    require(required <= set(artifacts), 'missing actual wallet/native/service/browser prebuilt artifacts')
    STATE = Path(tempfile.mkdtemp(prefix='wallet-402-caps-', dir=os.environ.get('WALLET_CAPS_EVIDENCE_ROOT')))
    STATE.chmod(0o700)
    copied = STATE / 'fixture'
    shutil.copytree(bundle, copied)
    expand = lambda text: str(text).replace('{state}', str(STATE)).replace('{fixture}', str(copied))
    env = {key: value for key, value in os.environ.items() if not key.startswith(('DATABASE_', 'ATTESTOR_', 'WALLET_', 'SUPABASE_', 'HYPERPAXEER_', 'RPC_', 'LAYER'))}
    pg = Path(config['postgres_bin_dir'])
    require(all((pg / name).is_file() for name in ('initdb', 'pg_ctl', 'pg_restore')), 'prebuilt PostgreSQL prerequisite')
    pgdir = STATE / 'postgres'; pgdir.mkdir(mode=0o700)
    prefix = []
    if os.getuid() == 0:
        owner = pwd.getpwnam('postgres')
        os.chown(STATE, owner.pw_uid, owner.pw_gid); os.chown(pgdir, owner.pw_uid, owner.pw_gid)
        prefix = ['runuser', '-u', 'postgres', '--']
    pgport = port()
    command(prefix + [str(pg / 'initdb'), '-D', str(pgdir / 'data'), '-A', 'trust', '-U', 'postgres'])
    command(prefix + [str(pg / 'pg_ctl'), '-D', str(pgdir / 'data'), '-l', str(pgdir / 'server.log'), '-o', f'-p {pgport} -h 127.0.0.1 -k {pgdir}', '-w', 'start'])
    PG_STOP = prefix + [str(pg / 'pg_ctl'), '-D', str(pgdir / 'data'), '-m', 'fast', '-w', 'stop']
    env['DATABASE_URL'] = f'postgres://postgres@127.0.0.1:{pgport}/postgres'
    command([str(pg / 'pg_restore'), '--exit-on-error', '--no-owner', '--dbname', env['DATABASE_URL'], str(copied / config['postgres_dump'])])
    CA = ssl.create_default_context(cafile=expand(config['ca_file']))
    env['NODE_EXTRA_CA_CERTS'] = expand(config['ca_file'])
    services = config['services']
    kinds = [item['kind'] for item in services]
    for kind in ('chain', 'supabase', 'layerxd', 'clock', 'identity', 'core', 'gateway', 'wallet_gateway', 'wallet_ui'):
        require(kinds.count(kind) == 1, 'one real service required: ' + kind)
    require(kinds.count('attestor') == 5, 'five actual attestors with unchanged threshold required')
    running = {}
    def launch(item):
        executable = artifacts[item['artifact']]['path']
        process_env = dict(env)
        process_env.update({key: expand(value) for key, value in item.get('env', {}).items()})
        if item['kind'] == 'wallet_gateway':
            require(process_env.get('ATTESTOR_QUORUM') == '3', 'production custody threshold must remain three')
        process = start([executable, *[expand(v) for v in item['argv']]], process_env, item['name'])
        if item.get('ready_url'):
            wait_ready(process, expand(item['ready_url']))
        return process
    for item in services:
        require(item['name'] not in running, 'duplicate service name')
        running[item['name']] = launch(item)
    url = local(expand(config['rpc_url']))
    gateway_url = local(expand(config['wallet_gateway_url']))
    accounts = {}
    for name in ('owner', 'empty', 'foreign'):
        entry = config['accounts'][name]
        token = private(copied / entry['token_file']).read_text().strip()
        status, provision = http(gateway_url + '/v1/wallet/provision', token, {})
        require(status == 200 and provision.get('identityBinding') and provision.get('wallet', {}).get('binding_state') == 'bound', 'actual authenticated wallet provisioning prerequisite')
        status, me = http(gateway_url + '/v1/wallet/me', token)
        require(status == 200 and me.get('identityBinding') and me.get('capsContext'), 'actual signed wallet identity bridge required')
        ctx = me['capsContext']
        require(ctx['did'] == entry['did'] and ctx['account_id'] == entry['account_id'], 'signed activity fixture must belong to actual provisioned wallet')
        accounts[name] = dict(entry, token=token, address=ctx['address'], chain_id=ctx['chain_id'], storage_state=str(copied / entry['storage_state']))
    require(len({a['did'] for a in accounts.values()}) == 3, 'three distinct real bound principals required')
    require(snapshot(url, accounts['empty'])['state'] == 'empty', 'confirmed-empty fixture must prove no caps')
    CASES.append('confirmed-empty')
    expected = config['expected']
    def mutate(name, account_name, wanted):
        operation = config['mutations'][name]
        request = json.loads(private(copied / operation['request_file']).read_text())
        require(request['method'] == 'lx_sendActivity' and isinstance(request['params'], list), 'canonical signed submission input required')
        authorization = private(copied / operation['gateway_key_file']).read_text().strip()
        before = snapshot(url, accounts[account_name])
        result = rpc(url, authorization, request['method'], request['params'])
        require('result' in result and 'error' not in result, 'actual native signed mutation refused')
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            value = snapshot(url, accounts[account_name])
            try:
                exact(value, wanted)
                require(int(value['observation']['sequence']) > int(before['observation']['sequence']), 'mutation must advance proven observation')
                require(value['observation']['state_root'] != before['observation']['state_root'], 'mutation must change proven state root')
                return value
            except RuntimeError:
                time.sleep(0.25)
        raise RuntimeError('signed mutation did not reach required verified state: ' + name)
    for name, who in [('owner-budget', 'owner'), ('owner-grant', 'owner'), ('foreign-budget', 'foreign'), ('foreign-grant', 'foreign')]:
        mutate(name, who, expected[name])
    populated = snapshot(url, accounts['owner']); exact(populated, expected['owner-grant'])
    require(populated['budgets'] and populated['grants'], 'real populated budgets and grants required')
    CASES.extend(['signed-native-population', 'foreign-prefix-filtered'])
    owner, empty = accounts['owner'], accounts['empty']
    args = [{'address': owner['address'], 'chain_id': owner['chain_id']}]
    expect_error(rpc(url, None, 'lx_getWalletCaps', args), -32002)
    expect_error(rpc(url, empty['token'], 'lx_getWalletCaps', args), -32002)
    expect_error(rpc(url, owner['token'], 'lx_getWalletCaps', [{'address': owner['address'], 'chain_id': owner['chain_id'] + 1}]), -32002)
    expect_error(rpc(url, owner['token'], 'lx_getWalletCaps', [{'did': owner['did']}]), -32602)
    expired = private(copied / config['expired_token_file']).read_text().strip()
    expect_error(rpc(url, expired, 'lx_getWalletCaps', args), -32002)
    code, _ = http(local(expand(config['core_url'])) + '/internal/v1/wallet-caps', body={'did': owner['did'], 'account_id': owner['account_id'], 'network_id': populated['network_id']})
    require(code == 401, 'direct anonymous selector must be refused before native read')
    CASES.extend(['anonymous-refused', 'cross-principal-refused', 'wrong-chain-refused', 'selector-refused', 'expired-session-refused', 'private-core-token-required'])
    runtime = dict(owner=owner, empty=empty, expected=expected['owner-grant'], sdk=artifacts['wallet_sdk']['path'],
                   gateway_url=gateway_url, rpc_url=url, ui_url=local(expand(config['ui_url'])),
                   playwright=artifacts['playwright']['path'], chromium=artifacts['chromium']['path'], control=str(STATE))
    runtime_path = STATE / 'runtime.json'; runtime_path.write_text(json.dumps(runtime))
    for name, source, required_cases in [('sdk', SDK_CASES, 5)]:
        script = STATE / (name + '.mjs'); script.write_text(source)
        result = subprocess.run([artifacts['node']['path'], str(script), str(runtime_path)], env=env, cwd=ROOT,
                                capture_output=True, timeout=120)
        (STATE / (name + '-cases.log')).write_bytes(result.stdout + result.stderr)
        require(result.returncode == 0, 'real ' + name + ' boundary failed; retained private log')
        executed = json.loads(result.stdout.decode().strip().splitlines()[-1])
        require(len(executed) == required_cases and len(set(executed)) == required_cases, 'incomplete actual boundary cases')
        CASES.extend(executed)
    browser_script = STATE / 'browser.mjs'; browser_script.write_text(BROWSER_CASES)
    browser_log = (STATE / 'browser-cases.log').open('wb')
    browser = subprocess.Popen([artifacts['node']['path'], str(browser_script), str(runtime_path)], env=env, cwd=ROOT,
                               stdout=browser_log, stderr=browser_log, start_new_session=True)
    PROCESSES.append(browser)
    deadline = time.monotonic() + 60
    while not (STATE / 'ui-ready').exists():
        require(browser.poll() is None and time.monotonic() < deadline, 'actual browser did not reach verified owner caps')
        time.sleep(0.1)
    updated = mutate('owner-budget-change', 'owner', expected['owner-budget-change'])
    revoked = mutate('owner-grant-revoke', 'owner', expected['owner-grant-revoke'])
    require(any(g['revoked'] for g in revoked['grants']), 'real grant revocation required')
    CASES.extend(['mutation-refresh', 'grant-revocation-refresh'])
    (STATE / 'mutation-complete.json').write_text(json.dumps(expected['owner-grant-revoke']))
    require(browser.wait(timeout=120) == 0, 'actual browser state/refresh/isolation case failed')
    browser_log.close()
    executed = json.loads((STATE / 'browser-cases.log').read_text().strip().splitlines()[-1])
    require(set(executed) == {'ui-loading', 'ui-verified-values', 'ui-mutation-refresh', 'ui-session-invalidated', 'ui-reload-fresh-empty'} and len(executed) == 5, 'incomplete actual browser cases')
    CASES.extend(executed)
    core = next(item for item in services if item['kind'] == 'core')
    stop(running[core['name']])
    expect_error(rpc(url, owner['token'], 'lx_getWalletCaps', args), -32001)
    CASES.append('unavailable-not-empty')
    running[core['name']] = launch(core)
    fresh = snapshot(url, owner); exact(fresh, expected['owner-grant-revoke'])
    require(int(fresh['observation']['sequence']) >= int(updated['observation']['sequence']), 'restart cannot regress evidence')
    CASES.append('restart-fresh-verified-state')
    require(len(CASES) == 23 and len(set(CASES)) == 23, 'every declared focused case must execute')
    require(all(process.poll() is None for process in running.values()), 'owned real service died during qualification')
    (STATE / 'result.json').write_text(json.dumps({'revision': config['revision'], 'command': 'timeout 15m python3 tools/qualification/paxeer-x/wallet-402-caps.py',
        'exit_code': 0, 'cases': CASES, 'log_path': str(STATE), 'skipped': 0}, indent=2))
    print(json.dumps({'exit_code': 0, 'log_path': str(STATE)}))


if __name__ == '__main__':
    def interrupted(*_):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupted)
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        reason = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        if STATE:
            (STATE / 'failure.json').write_text(json.dumps({'exit_code': 1, 'reason': reason, 'cases': CASES}))
        print(json.dumps({'exit_code': 1, 'log_path': str(STATE) if STATE else None, 'reason': reason}))
        raise SystemExit(1)
    finally:
        for process in reversed(PROCESSES):
            stop(process)
        if PG_STOP:
            try:
                command(PG_STOP)
            except Exception:
                pass
