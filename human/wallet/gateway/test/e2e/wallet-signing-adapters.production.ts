import assert from 'node:assert/strict';
import { readFileSync, lstatSync, writeFileSync, openSync, closeSync, fsyncSync, renameSync } from 'node:fs';
import { request as httpsRequest } from 'node:https';
import { join, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';
import { randomUUID, createHash } from 'node:crypto';
import { encodeFunctionData, keccak256, recoverMessageAddress, recoverAddress, type Hex } from 'viem';
import { Pool } from 'pg';

interface Runtime {
  gateway_url: string; rpc_url: string; real_rpc_url: string; control_url: string;
  sdk_entry: string; evidence_dir: string; fixture_dir: string; database_url: string;
  owner_token_file: string; foreign_owner_token_file: string; owner_subject: string;
  account: Hex; main_account: string; chain_id: number; custody_pointer: Hex;
  custody_amount: string; sponsor: Hex; paymaster: Hex; quote_url: string;
  sponsor_call: { to: Hex; data: Hex; value: string }; sponsor_maximum: string; sponsor_gas_cost: string;
  attestor_key_id: string; attestors: { id: string; url: string }[];
  attestor_ca_file: string; gateway_cert_file: string; gateway_key_file: string;
}
const runtimePath = process.env.WALLET_SIGNING_ADAPTERS_RUNTIME;
assert(runtimePath, 'protected runtime configuration is required');
const runtime = JSON.parse(readFileSync(runtimePath, 'utf8')) as Runtime;
function secret(name: string): string {
  const path = resolve(runtime.fixture_dir, name);
  assert(path.startsWith(resolve(runtime.fixture_dir) + sep), 'fixture material must stay in the protected bundle');
  const stat = lstatSync(path);
  assert(stat.isFile() && !stat.isSymbolicLink() && stat.nlink === 1 && (stat.mode & 0o077) === 0);
  assert.equal(stat.uid, process.getuid!());
  return readFileSync(path, 'utf8').trim();
}
const ownerToken = secret(runtime.owner_token_file);
const foreignToken = secret(runtime.foreign_owner_token_file);
const sdk = await import(pathToFileURL(runtime.sdk_entry).href) as typeof import('../../../sdk/src/index.js');
const cases: string[] = [];
const pool = new Pool({ connectionString: runtime.database_url });
const submissionStore = {
  getItem(key: string): string | null {
    const path = join(runtime.evidence_dir, 'submission-' + createHash('sha256').update(key).digest('hex') + '.json');
    try {
      const info = lstatSync(path);
      assert(info.isFile() && !info.isSymbolicLink() && info.uid === process.getuid!() && (info.mode & 0o077) === 0);
      return readFileSync(path, 'utf8');
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') return null;
      throw error;
    }
  },
  setItem(key: string, value: string): void {
    const path = join(runtime.evidence_dir, 'submission-' + createHash('sha256').update(key).digest('hex') + '.json');
    const temporary = path + '.' + randomUUID();
    const fd = openSync(temporary, 'wx', 0o600);
    try { writeFileSync(fd, value); fsyncSync(fd); } finally { closeSync(fd); }
    renameSync(temporary, path);
    const directory = openSync(runtime.evidence_dir, 'r');
    try { fsyncSync(directory); } finally { closeSync(directory); }
  },
};
const custodyTo = '0x0000000000000000000000000000000000001013' as Hex;
const depositAbi = [{ type: 'function', name: 'depositToken', stateMutability: 'nonpayable', inputs: [
  { name: 'pointer', type: 'address' }, { name: 'amount', type: 'uint256' }, { name: 'beneficiary', type: 'bytes32' },
], outputs: [{ name: 'depositId', type: 'bytes32' }] }] as const;
const calldata = encodeFunctionData({ abi: depositAbi, functionName: 'depositToken', args: [
  runtime.custody_pointer, BigInt(runtime.custody_amount), `0x${runtime.main_account.replace(/^0x/, '')}` as Hex,
] });
let lastSponsorBody: Record<string, unknown> | undefined;
let lastSponsorResponse: Record<string, unknown> | undefined;
const realFetch: typeof fetch = async (input, init) => {
  const response = await fetch(input, init);
  if (String(input).endsWith('/v1/wallet/sponsored/submit')) {
    assert.equal(typeof init?.body, 'string');
    lastSponsorBody = JSON.parse(init!.body as string) as Record<string, unknown>;
    lastSponsorResponse = await response.clone().json() as Record<string, unknown>;
  }
  return response;
};
function provider() {
  return new sdk.PaxeerProvider({ gatewayUrl: runtime.gateway_url, rpcUrl: runtime.rpc_url,
    chainId: runtime.chain_id, token: () => ownerToken, fetch: realFetch,
    confirm: async request => {
      assert(request.method && Array.isArray(request.params));
      return true;
    } });
}
async function http(path: string, body?: unknown, token = ownerToken, method = 'POST') {
  const response = await fetch(runtime.gateway_url + path, { method,
    headers: { 'content-type': 'application/json', authorization: `Bearer ${token}` },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  return { status: response.status, body: await response.json() as Record<string, any> };
}
async function rpc(method: string, params: unknown[], real = false): Promise<any> {
  const response = await fetch(real ? runtime.real_rpc_url : runtime.rpc_url, { method: 'POST',
    headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: randomUUID(), method, params }) });
  const body = await response.json() as { error?: unknown; result?: unknown };
  assert(!body.error, 'real chain refused ' + method);
  return body.result;
}
async function control(action: string) {
  const response = await fetch(runtime.control_url + '/control/' + action, { method: 'POST', body: '{}' });
  assert(response.ok);
  return response.json() as Promise<Record<string, any>>;
}
async function eventually<T>(read: () => Promise<T>, accept: (value: T) => boolean): Promise<T> {
  const end = Date.now() + 90_000;
  while (Date.now() < end) {
    const value = await read();
    if (accept(value)) return value;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error('real production evidence deadline exceeded');
}
function replaceWord(bytes: Hex, index: number, value: string): Hex {
  const offset = 2 + new TextEncoder().encode('LX:CUSTODY:v2').length * 2 + index * 64;
  return (bytes.slice(0, offset) + value.padStart(64, '0') + bytes.slice(offset + 64)) as Hex;
}
async function attestor(node: Runtime['attestors'][number], body: unknown) {
  const url = new URL('/v1/sign', node.url);
  const data = Buffer.from(JSON.stringify(body));
  return await new Promise<{ status: number; body: Record<string, any> }>((resolve, reject) => {
    const request = httpsRequest(url, { method: 'POST', minVersion: 'TLSv1.3',
      ca: secret(runtime.attestor_ca_file), cert: secret(runtime.gateway_cert_file), key: secret(runtime.gateway_key_file),
      headers: { 'content-type': 'application/json', 'content-length': data.length, authorization: `Bearer ${ownerToken}` } }, response => {
      const chunks: Buffer[] = [];
      response.on('data', chunk => chunks.push(Buffer.from(chunk)));
      response.on('end', () => resolve({ status: response.statusCode!, body: JSON.parse(Buffer.concat(chunks).toString('utf8')) }));
      response.on('error', reject);
    });
    request.on('error', reject); request.end(data);
  });
}

try {
  assert.equal(runtime.attestors.length, 5);
  const p = provider();
  const wallet = new sdk.WalletInterface(p);
  const accounts = await wallet.accounts();
  assert.equal(accounts[0]!.toLowerCase(), runtime.account.toLowerCase());
  const custody = await p.request({ method: 'paxeer_prepareCustody', params: [{
    account: runtime.account, chainId: runtime.chain_id, to: custodyTo, value: '0x0', data: calldata,
  }] }) as Hex;
  const decoded = sdk.decodeCustodyAuthorization(custody);
  assert.equal(decoded.account.toLowerCase(), runtime.account.toLowerCase());
  assert.equal(decoded.chainId, BigInt(runtime.chain_id));
  assert.equal(decoded.to.toLowerCase(), custodyTo);
  assert.equal(decoded.data.toLowerCase(), calldata.toLowerCase());
  const malformed = await http('/v1/wallet/sign-custody', { custody: '0x4c583a435553544f44593a7631' + '00'.repeat(32) });
  assert.equal(malformed.status, 400);
  const foreign = await http('/v1/wallet/sign-custody', { custody }, foreignToken);
  assert([401, 403, 404].includes(foreign.status));
  const wrongNetwork = replaceWord(custody, 1, (BigInt(runtime.chain_id) + 1n).toString(16));
  const expired = replaceWord(custody, 6, '0');
  for (const value of [wrongNetwork, expired]) {
    const refused = await http('/v1/wallet/sign-custody', { custody: value });
    assert([400, 403].includes(refused.status), 'network or expiry changed without refusal');
  }
  const bare = await http('/v1/wallet/sign-digest', { digest: `0x${'01'.repeat(32)}` });
  assert.equal(bare.status, 400);
  const signers = runtime.attestors.slice(0, 3).map(node => node.id);
  const independent = await Promise.all(runtime.attestors.slice(0, 3).map(node => attestor(node, {
    session_id: randomUUID(), key_id: runtime.attestor_key_id, signers,
    kind: 'custody', message: wrongNetwork.slice(2),
  })));
  assert(independent.every(response => response.status === 403 && response.body.error?.policy_code === 'decode_error'), 'daemon did not independently reject wrong network custody');
  cases.push('mounted-authentication', 'canonical-custody-refusals', 'original-owner-binding', 'independent-daemon-network-refusal');

  const signature = await wallet.signCustody(custody);
  assert.equal((await recoverMessageAddress({ message: { raw: custody }, signature })).toLowerCase(), runtime.account.toLowerCase());
  const audit = await pool.query<{ attestor_audit: { node_id: string; audit_sequence: number }[] }>(
    `select attestor_audit from signing_audit where client_subject=$1 and route='/v1/wallet/sign-custody' and decision='signed' order by id desc limit 1`,
    [runtime.owner_subject]);
  assert.equal(audit.rows[0]?.attestor_audit.length, 3);
  assert(audit.rows[0]!.attestor_audit.every(entry => signers.includes(entry.node_id) || runtime.attestors.some(node => node.id === entry.node_id)));
  assert(audit.rows[0]!.attestor_audit.every(entry => entry.audit_sequence > 0));
  const changedCustodySend = await http('/v1/wallet/send', { tx: {
    chainId: Number(decoded.chainId), nonce: Number(decoded.nonce), to: decoded.to,
    value: decoded.value.toString(), data: decoded.data, gas: decoded.gas.toString(),
    maxFeePerGas: (decoded.maxFeePerGas + 1n).toString(), maxPriorityFeePerGas: decoded.maxPriorityFeePerGas.toString(),
  }, custody: { bytes: custody, signature } });
  assert.equal(changedCustodySend.status, 403);
  assert.equal(changedCustodySend.body.error, 'custody_construction_changed');
  cases.push('custody-exact-approved-fee-bound');
  await control('arm-custody');
  const hash = await wallet.sendTransaction({ to: custodyTo, value: 0n, data: calldata, chainId: runtime.chain_id });
  assert(/^0x[0-9a-fA-F]{64}$/.test(hash));
  assert.equal((await control('state')).dropped_reply, true);
  let state = await p.request({ method: 'paxeer_custodyStatus', params: [{ custody }] }) as Record<string, any>;
  assert.equal(state.status, 'pending');
  assert.equal(state.tx_hash.toLowerCase(), hash.toLowerCase());
  await control('restart');
  const resumed = provider();
  const resumedWallet = new sdk.WalletInterface(resumed);
  await resumedWallet.accounts();
  await resumed.request({ method: 'paxeer_restoreCustody', params: [{ custody, signature }] });
  const repeatedHash = await resumedWallet.sendTransaction({ to: custodyTo, value: 0n, data: calldata, chainId: runtime.chain_id });
  assert.equal(repeatedHash.toLowerCase(), hash.toLowerCase());
  state = await resumed.request({ method: 'paxeer_custodyStatus', params: [{ custody }] }) as Record<string, any>;
  assert.equal(state.status, 'pending');
  await control('receipts');
  state = await eventually(async () => resumed.request({ method: 'paxeer_custodyStatus', params: [{ custody }] }) as Promise<Record<string, any>>,
    answer => answer.status === 'confirmed' || answer.status === 'reverted');
  assert.equal(state.status, 'confirmed');
  const receipt = await rpc('eth_getTransactionReceipt', [hash], true);
  assert.equal(receipt.status, '0x1');
  assert.equal(receipt.from.toLowerCase(), runtime.account.toLowerCase());
  assert.equal(receipt.to.toLowerCase(), custodyTo);
  const journal = await pool.query('select tx_hash,raw_tx,state from wallet_custody_submissions where id=$1', [keccak256(custody)]);
  assert.equal(journal.rows.length, 1);
  assert.equal(journal.rows[0].tx_hash.toLowerCase(), hash.toLowerCase());
  assert(journal.rows[0].raw_tx && journal.rows[0].state === 'confirmed');
  cases.push('sdk-deposit-custody-handoff', 'actual-selected-quorum-audit', 'lost-broadcast-reply', 'retained-restart-idempotency', 'receipt-only-confirmation');

  const gas = sdk.gasStation(resumed, { chainId: BigInt(runtime.chain_id), sponsor: runtime.sponsor,
    paymaster: runtime.paymaster, quoteUrl: runtime.quote_url, gatewayUrl: runtime.gateway_url,
    accessToken: () => ownerToken, fetch: realFetch, submissionStore });
  const calls = [{ to: runtime.sponsor_call.to, value: BigInt(runtime.sponsor_call.value), data: runtime.sponsor_call.data }];
  const nonce = await gas.batchNonce(runtime.account);
  const signed = await gas.requestQuote({ account: runtime.account, nonce, calls,
    maxTokenAmount: BigInt(runtime.sponsor_maximum), gasCost: BigInt(runtime.sponsor_gas_cost) });
  const batch = { chainId: BigInt(runtime.chain_id), account: runtime.account, nonce, calls, quote: signed.quote };
  const fields = gas.construction(batch);
  const digest = gas.digest(batch);
  const digestSignature = await gas.sign(batch) as Hex;
  assert.equal((await recoverAddress({ hash: digest, signature: digestSignature })).toLowerCase(), runtime.account.toLowerCase());
  const tampered = JSON.parse(JSON.stringify(fields));
  tampered.account = '0x0000000000000000000000000000000000000001';
  const refusedOwner = await http('/v1/wallet/sign-digest', { construction: tampered });
  assert([400, 403].includes(refusedOwner.status));
  tampered.account = fields.account;
  tampered.quote.deadline = '0';
  const refusedExpiry = await http('/v1/wallet/sign-digest', { construction: tampered });
  assert([400, 403].includes(refusedExpiry.status));
  const changedDigest = await Promise.all(runtime.attestors.slice(0, 3).map(node => attestor(node, {
    session_id: randomUUID(), key_id: runtime.attestor_key_id, signers,
    kind: 'eth_sign_digest', digest: '00'.repeat(32), construction: fields,
  })));
  assert(changedDigest.every(answer => answer.status === 403 && answer.body.error?.policy_code === 'digest_mismatch'), 'daemon did not independently recompute the digest');
  let confirmedConsent = false;
  const sponsoredHash = await gas.submitFirstUse(batch, signed.relayerSignature, { confirm: async consent => {
    assert.equal(consent.batchDigest.toLowerCase(), digest.toLowerCase());
    confirmedConsent = true;
    return true;
  } });
  assert(confirmedConsent && /^0x[0-9a-fA-F]{64}$/.test(sponsoredHash));
  assert(lastSponsorBody?.authorization && lastSponsorBody?.quote_decimals === 6);
  assert.equal(lastSponsorResponse?.tx_hash, sponsoredHash);
  assert(['pending', 'confirmed'].includes(String(lastSponsorResponse?.status)));
  const exactSubmission = lastSponsorBody!;
  await control('restart');
  const restartedProvider = provider();
  await new sdk.WalletInterface(restartedProvider).accounts();
  const restartedGas = sdk.gasStation(restartedProvider, { chainId: BigInt(runtime.chain_id), sponsor: runtime.sponsor,
    paymaster: runtime.paymaster, quoteUrl: runtime.quote_url, gatewayUrl: runtime.gateway_url,
    accessToken: () => ownerToken, fetch: realFetch, submissionStore });
  const resumedSponsor = await restartedGas.resume(batch, signed.relayerSignature);
  assert.equal(resumedSponsor.tx_hash?.toLowerCase(), sponsoredHash.toLowerCase());
  const retry = await http('/v1/wallet/sponsored/submit', exactSubmission);
  assert([200, 202].includes(retry.status));
  assert.equal(String(retry.body.tx_hash).toLowerCase(), sponsoredHash.toLowerCase());
  const statusRequest = { account: runtime.account, sponsor: signed.quote.sponsor,
    quoteNonce: signed.quote.quoteNonce.toString(), relayerSignature: signed.relayerSignature };
  const station = await eventually(async () => (await http('/v1/wallet/sponsored/status', statusRequest)).body,
    answer => answer.status === 'confirmed' || answer.status === 'reverted');
  assert.equal(station.status, 'confirmed');
  const sponsoredReceipt = await rpc('eth_getTransactionReceipt', [sponsoredHash], true);
  assert.equal(sponsoredReceipt.status, '0x1');
  const changedSubmission = structuredClone(exactSubmission);
  changedSubmission.account_signature = '0x' + '00'.repeat(65);
  const refusal = await http('/v1/wallet/sponsored/submit', changedSubmission);
  assert([400, 403, 409, 422].includes(refusal.status));
  cases.push('sdk-complete-digest-construction', 'independent-daemon-digest-recompute', 'expired-consent-refusal',
    'actual-station-first-use-protocol', 'exact-sponsor-consent-idempotency', 'station-restart-status');

  await control('lose-quorum');
  await new Promise(resolve => setTimeout(resolve, 1500));
  const currentNonce = await rpc('eth_getTransactionCount', [runtime.account, 'pending'], true);
  const unavailableCustody = sdk.encodeCustodyAuthorization({ ...decoded, nonce: BigInt(currentNonce),
    deadline: BigInt(Math.floor(Date.now() / 1000) + 300) });
  const unavailable = await http('/v1/wallet/sign-custody', { custody: unavailableCustody });
  assert.equal(unavailable.status, 503);
  assert(!unavailable.body.signature, 'unavailable quorum used local fallback');
  cases.push('quorum-loss-no-local-fallback');
  writeFileSync(join(runtime.evidence_dir, 'cases-result.json'), JSON.stringify({ complete: true, cases }, null, 2), { mode: 0o600 });
} finally {
  await pool.end();
}
