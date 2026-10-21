import assert from 'node:assert/strict';
import { createPrivateKey, createPublicKey, randomBytes, randomUUID, sign } from 'node:crypto';
import { closeSync, constants, fstatSync, openSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { request as httpsRequest } from 'node:https';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Pool } from 'pg';
import { createPublicClient, http, keccak256, parseTransaction, serializeTransaction, recoverMessageAddress, recoverTransactionAddress, type Hex } from 'viem';

type Fixture = {
  fixture_dir: string; evidence_dir: string; database_url: string; gateway_url: string;
  mismatch_url: string; control_url: string; real_rpc_url: string;
  foreign_owner_token_file: string; owner_token_file: string; agent_private_key_file: string; agent_read_token_file: string;
  owner_subject: string; owner_address: Hex; agent_did: string; agent_address: Hex;
  recipient: Hex; chain_id: number; fund_amount_wei: string; attestor_policy_refusal_value_wei: string;
  membership_mismatch_verified: boolean; inventory_files: string[]; inventory_public_key_file: string;
  operator_inventory_private_key_file: string; operator_client_cert_file: string; operator_client_key_file: string;
  attestor_ca_file: string; attestor_endpoints: string[];
  provision_agent: { did: string; private_key_file: string };
  durable: { token: Hex; amount: string; spender: Hex; contract: Hex; method: string; args: string[] };
};
const fixture: Fixture = JSON.parse(readFileSync(process.env.WALLET_CUSTODY_RUNTIME!, 'utf8'));
const fixtureFile = (name: string) => {
  const path = resolve(fixture.fixture_dir, name);
  assert(path.startsWith(resolve(fixture.fixture_dir) + '/'), 'fixture path escapes isolated bundle');
  return readFileSync(path, 'utf8').trim();
};
const ownerToken = fixtureFile(fixture.owner_token_file);
const foreignOwnerToken = fixtureFile(fixture.foreign_owner_token_file);
const readToken = fixtureFile(fixture.agent_read_token_file);
const agentKey = createPrivateKey(fixtureFile(fixture.agent_private_key_file));
const agentPublicKey = createPublicKey(agentKey).export({ format: 'der', type: 'spki' }).subarray(-32).toString('hex');
const pool = new Pool({ connectionString: fixture.database_url });
const chain = createPublicClient({ transport: http(fixture.real_rpc_url) });
const completed: string[] = [];
const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
const digestModule = await import(pathToFileURL(resolve('dist/agent/verify.js')).href);
const walletsModule = await import(pathToFileURL(resolve('dist/db/wallets.js')).href);
const dbModule = await import(pathToFileURL(resolve('dist/db/pool.js')).href);
const agentRequestDigest = digestModule.agentRequestDigest as (args: {
  method: string; keyId: string; nonce: Buffer; expiry: bigint; body: Buffer;
}) => Buffer;

async function request(path: string, body?: unknown, headers: Record<string, string> = {}, base = fixture.gateway_url) {
  const response = await fetch(base + path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
    signal: AbortSignal.timeout(180_000),
  });
  return { status: response.status, body: await response.json() as Record<string, any> };
}
const owner = { authorization: `Bearer ${ownerToken}` };
const reader = { authorization: `Bearer ${readToken}` };

function proof(method: string, keyId: string, body: Buffer, expiry = Math.floor(Date.now() / 1000) + 240, key = agentKey) {
  const nonce = randomBytes(16);
  const signature = sign(null, agentRequestDigest({ method, keyId, nonce, expiry: BigInt(expiry), body }), key).toString('hex');
  return { nonce: nonce.toString('hex'), expiry, signature };
}

function signedHeaders(path: string, raw: string, challenge?: string, did = fixture.agent_did, key = agentKey) {
  const original = proof(`POST ${path}`, did, Buffer.from(raw), undefined, key);
  const headers: Record<string, string> = {
    'x-agent-key': did, 'x-agent-nonce': original.nonce,
    'x-agent-expires': String(original.expiry), 'x-agent-signature': original.signature,
  };
  if (challenge) {
    const wire = JSON.parse(challenge);
    assert.equal(wire.origin.method, `POST ${path}`);
    assert.equal(wire.origin.did, did);
    assert.equal(wire.origin.body, Buffer.from(raw).toString('base64'));
    wire.origin = { method: `POST ${path}`, did, body: Buffer.from(raw).toString('base64'), ...original };
    const exact = JSON.stringify(wire);
    const final = proof('/v1/sign', wire.key_id, Buffer.from(exact), undefined, key);
    headers['x-agent-attestor-authorization'] = JSON.stringify({ body: exact, ...final });
  }
  return headers;
}

async function signedRequest(path: string, body: unknown, challenge?: string, did = fixture.agent_did, key = agentKey) {
  const raw = JSON.stringify(body);
  const headers = signedHeaders(path, raw, challenge, did, key);
  if ((headers['x-agent-attestor-authorization']?.length ?? 0) > 8_000) {
    const authorization = JSON.parse(headers['x-agent-attestor-authorization']!);
    const uploaded = await signedRequest('/v1/agent/signing-authorizations', authorization, undefined, did, key);
    assert.equal(uploaded.status, 201, JSON.stringify(uploaded.body));
    assert.match(uploaded.body.authorization_id, /^[0-9a-f]{64}$/);
    delete headers['x-agent-attestor-authorization'];
    headers['x-agent-attestor-authorization-id'] = uploaded.body.authorization_id;
  }
  return request(path, raw, headers);
}

async function poll<T>(read: () => Promise<T>, satisfied: (value: T) => boolean, timeout = 180_000): Promise<T> {
  const deadline = Date.now() + timeout;
  for (;;) {
    const value = await read();
    if (satisfied(value)) return value;
    assert(Date.now() < deadline, 'production transition deadline exceeded');
    await pause(250);
  }
}

function operatorInventorySigner() {
  const path = resolve(fixture.fixture_dir, fixture.operator_inventory_private_key_file);
  assert(path.startsWith(resolve(fixture.fixture_dir) + '/'));
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const metadata = fstatSync(descriptor);
    assert(metadata.isFile() && metadata.nlink === 1 && (metadata.mode & 0o777) === 0o600
      && metadata.uid === process.getuid?.() && metadata.size > 0 && metadata.size <= 16_384,
      'prerequisite: protected operator approval key');
    const key = createPrivateKey(readFileSync(descriptor));
    assert.equal(key.asymmetricKeyType, 'ec');
    assert.equal(key.asymmetricKeyDetails?.namedCurve, 'prime256v1');
    const expected = createPublicKey(readFileSync(fixture.inventory_public_key_file)).export({ type: 'spki', format: 'der' });
    assert.deepEqual(createPublicKey(key).export({ type: 'spki', format: 'der' }), expected,
      'supplied approval key must match the configured independent operator pin');
    return key;
  } finally { closeSync(descriptor); }
}
const inventorySigner = operatorInventorySigner();
let inventory = JSON.parse(Buffer.from(readFileSync(fixture.inventory_files[0]!, 'utf8').trim().split('.')[1]!, 'base64url').toString('utf8'));
function writeInventoryToken(token: string) {
  for (const path of fixture.inventory_files) {
    assert(resolve(path).startsWith(resolve(fixture.evidence_dir) + '/'));
    const temporary = path + '.qualification-next';
    writeFileSync(temporary, token, { mode: 0o600 });
    renameSync(temporary, path);
  }
}
function approveInventory(next: typeof inventory) {
  const now = Math.floor(Date.now() / 1000);
  next = { ...next, sequence: (BigInt(inventory.sequence) + 1n).toString(), iat: now, exp: now + 3600 };
  const header = Buffer.from(JSON.stringify({ alg: 'ES256', typ: 'wallet-custody-inventory+jwt' })).toString('base64url');
  const body = Buffer.from(JSON.stringify(next)).toString('base64url');
  const input = `${header}.${body}`;
  const signature = sign('sha256', Buffer.from(input), { key: inventorySigner, dsaEncoding: 'ieee-p1363' }).toString('base64url');
  writeInventoryToken(`${input}.${signature}`);
  inventory = next;
}
async function describeKey(endpoint: string, keyId: string): Promise<Record<string, any>> {
  const body = JSON.stringify({ session_id: randomUUID(), key_id: keyId });
  return new Promise((resolveReply, reject) => {
    const request = httpsRequest(new URL('/v1/keys/describe', endpoint), {
      method: 'POST', ca: readFileSync(fixture.attestor_ca_file),
      cert: fixtureFile(fixture.operator_client_cert_file), key: fixtureFile(fixture.operator_client_key_file),
      rejectUnauthorized: true, headers: { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) },
      timeout: 30_000,
    }, (response) => {
      const chunks: Buffer[] = [];
      response.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
      response.on('end', () => {
        try {
          assert.equal(response.statusCode, 200);
          resolveReply(JSON.parse(Buffer.concat(chunks).toString('utf8')));
        } catch (error) { reject(error); }
      });
    });
    request.on('error', reject); request.on('timeout', () => request.destroy(new Error('describe timeout')));
    request.end(body);
  });
}

async function wrongSubjectAtAttestor(endpoint: string, wire: Record<string, unknown>) {
  const raw = JSON.stringify(wire);
  const result = await new Promise<{ status: number; body: Record<string, any> }>((resolveReply, reject) => {
    const call = httpsRequest(new URL('/v1/sign', endpoint), {
      method: 'POST', ca: readFileSync(fixture.attestor_ca_file),
      cert: readFileSync(process.env.ATTESTOR_CLIENT_CERT_FILE!), key: readFileSync(process.env.ATTESTOR_CLIENT_KEY_FILE!),
      rejectUnauthorized: true, headers: { 'content-type': 'application/json', 'content-length': Buffer.byteLength(raw),
        authorization: `Bearer ${foreignOwnerToken}` }, timeout: 30_000,
    }, (response) => {
      const chunks: Buffer[] = [];
      response.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
      response.on('end', () => {
        try { resolveReply({ status: response.statusCode!, body: JSON.parse(Buffer.concat(chunks).toString('utf8')) }); }
        catch (error) { reject(error); }
      });
    });
    call.on('error', reject); call.on('timeout', () => call.destroy(new Error('attestor admission timeout')));
    call.end(raw);
  });
  assert(result.status === 401 || result.status === 403);
  assert.equal(result.body.code, 'token_not_owner');
}

async function assertAudit(subject: string, minimum: number) {
  const result = await pool.query(`select attestor_audit from signing_audit
    where client_subject = $1 and path = 'attestor' and decision = 'signed' order by id desc limit $2`, [subject, minimum]);
  assert.equal(result.rows.length, minimum);
  for (const row of result.rows) {
    assert.equal(row.attestor_audit.length, 3);
    assert.equal(new Set(row.attestor_audit.map((entry: any) => entry.node_id)).size, 3);
    assert(row.attestor_audit.every((entry: any) => Number(entry.audit_sequence) > 0));
  }
}

try {
  assert.equal(await chain.getChainId(), fixture.chain_id);
  const ready = await poll(() => request('/readyz'), (value) => value.status === 200);
  assert.equal(ready.body.components.attestors.required, 3);
  assert.equal(ready.body.components.attestors.healthy, 5);
  const principal = (await pool.query('select * from agent_principals where did = $1', [fixture.agent_did])).rows[0];
  assert(principal, 'approved registered external principal fixture required');
  assert.equal(principal.public_key, agentPublicKey);
  assert.equal(principal.owner_user_id, fixture.owner_subject);
  assert.equal(principal.is_frozen, false);
  const wallets = await pool.query('select * from wallets where lower(address) = any($1::text[])',
    [[fixture.owner_address.toLowerCase(), fixture.agent_address.toLowerCase()]]);
  assert.equal(wallets.rows.length, 2);
  assert(wallets.rows.every((wallet) => wallet.migrated_at && wallet.attestor_key_id));
  assert.equal((await pool.query('select count(*)::int as count from agent_actions where did = $1', [fixture.agent_did])).rows[0].count, 0,
    'isolated fixture must begin without actions for this principal');
  completed.push('real-five-member-threshold-three-readiness');

  const newAgent = fixture.provision_agent;
  const newKey = createPrivateKey(fixtureFile(newAgent.private_key_file));
  const newPublic = createPublicKey(newKey).export({ format: 'der', type: 'spki' }).subarray(-32).toString('hex');
  const newPrincipal = (await pool.query('select * from agent_principals where did = $1', [newAgent.did])).rows[0];
  assert(newPrincipal && newPrincipal.wallet_id === null && newPrincipal.public_key === newPublic && !newPrincipal.is_frozen,
    'fresh provisioning requires registered external identity and no wallet');
  const newUserId = walletsModule.agentWalletUserId(newAgent.did);
  assert.equal((await pool.query('select count(*)::int as count from wallets where user_id = $1', [newUserId])).rows[0].count, 0);
  const newKeyId = `wallet:${newUserId}:agent:secp256k1:0`;
  assert(!inventory.keys.some((entry: any) => entry.key_id === newKeyId), 'fresh generation key must not already exist in fixture inventory');
  approveInventory({ ...inventory, keys: [...inventory.keys, {
    key_id: newKeyId, epoch: 0, curve: 'secp256k1', public_key: '', owner: `agent:${newPublic}`, operations: ['generate'],
  }] });
  const pending = await signedRequest('/v1/agent/provision', {}, undefined, newAgent.did, newKey);
  assert.equal(pending.status, 202, JSON.stringify(pending.body));
  assert.equal(pending.body.provision.awaiting, 'agent_signature');
  const descriptions = await Promise.all(fixture.attestor_endpoints.map((endpoint) => describeKey(endpoint, newKeyId)));
  assert.equal(new Set(descriptions.map((reply) => reply.node_id)).size, 5);
  for (const description of descriptions) {
    assert.equal(description.key_id, newKeyId);
    assert.equal(description.owner, `agent:${newPublic}`);
    assert.equal(description.public_key, descriptions[0]!.public_key);
    assert.equal(description.epoch, descriptions[0]!.epoch);
    assert.deepEqual([...description.participants].sort(), inventory.members.map((member: any) => member.id).sort());
  }
  approveInventory({ ...inventory, keys: inventory.keys.map((entry: any) => entry.key_id === newKeyId ? {
    ...entry, public_key: descriptions[0]!.public_key, epoch: descriptions[0]!.epoch, operations: ['sign', 'refresh'],
  } : entry) });
  const bindBody = { bind_signature: sign(null, Buffer.from(pending.body.provision.bindMessage.slice(2), 'hex'), newKey).toString('hex') };
  const bindingChallenge = await signedRequest('/v1/agent/provision', bindBody, undefined, newAgent.did, newKey);
  assert.equal(bindingChallenge.status, 409, JSON.stringify(bindingChallenge.body));
  assert.equal(bindingChallenge.body.error, 'agent_reauthorization_required');
  const bound = await signedRequest('/v1/agent/provision', bindBody, bindingChallenge.body.signing_request, newAgent.did, newKey);
  assert.equal(bound.status, 200, JSON.stringify(bound.body));
  assert.equal(bound.body.provision.state, 'active');
  const boundWallet = (await pool.query('select * from wallets where id = $1', [bound.body.provision.walletId])).rows[0];
  assert.equal(boundWallet.encrypted_private_key, null);
  assert.equal(boundWallet.layerx_key_id, null);
  assert.equal(boundWallet.did, `did:layerx:${newPublic}`);
  assert.equal(boundWallet.binding_state, 'bound');
  assert.equal((await pool.query('select public_key from agent_principals where did = $1', [newAgent.did])).rows[0].public_key, newPublic);
  completed.push('fresh-agent-real-threshold-generation-operator-admission-and-registered-identity-binding');


  const ownerWallet = wallets.rows.find((wallet) => wallet.address.toLowerCase() === fixture.owner_address.toLowerCase())!;
  const agentWallet = wallets.rows.find((wallet) => wallet.address.toLowerCase() === fixture.agent_address.toLowerCase())!;
  await pool.query('update wallets set encrypted_private_key = $2 where id = any($1::uuid[])',
    [[ownerWallet.id, agentWallet.id], 'invalid-retired-envelope-must-never-be-decrypted']);
  for (const wallet of [ownerWallet, agentWallet]) {
    await assert.rejects(walletsModule.getSigningAccountForRow(wallet), /wallet_migrated/);
  }
  const message = 'production-custody-owner-' + randomBytes(12).toString('hex');
  const ownerSigned = await request('/v1/wallet/sign-message', { message }, owner);
  assert.equal(ownerSigned.status, 200, JSON.stringify(ownerSigned.body));
  assert.equal((await recoverMessageAddress({ message, signature: ownerSigned.body.signature })).toLowerCase(), fixture.owner_address.toLowerCase());
  await assertAudit(fixture.owner_subject, 1);
  completed.push('migrated-owner-refuses-retired-envelope-and-signs-via-attestors');
  const wrongOwner = await request(`/v1/agents/${encodeURIComponent(fixture.agent_did)}/fund`,
    { amount: fixture.fund_amount_wei }, { authorization: `Bearer ${foreignOwnerToken}` });
  assert.equal(wrongOwner.status, 403);
  assert.equal(wrongOwner.body.error, 'forbidden');
  completed.push('foreign-supabase-owner-cannot-fund-or-authorize-agent');


  await pool.query('update wallets set encrypted_private_key = null, migrated_at = null where id = $1', [ownerWallet.id]);
  const nullMessage = message + '-null';
  const nullSigned = await request('/v1/wallet/sign-message', { message: nullMessage }, owner);
  assert.equal(nullSigned.status, 200, JSON.stringify(nullSigned.body));
  assert.equal((await recoverMessageAddress({ message: nullMessage, signature: nullSigned.body.signature })).toLowerCase(), fixture.owner_address.toLowerCase());
  await pool.query('update wallets set migrated_at = $2 where id = $1', [ownerWallet.id, ownerWallet.migrated_at]);
  completed.push('null-envelope-is-real-attestor-custody');

  for (const route of ['fund', 'sweep']) {
    const response = await request(`/v1/agents/${encodeURIComponent(fixture.agent_did)}/${route}`, { amount: fixture.fund_amount_wei }, owner);
    assert.equal(response.status, 200, JSON.stringify(response.body));
    const receipt = await chain.waitForTransactionReceipt({ hash: response.body.tx_hash, confirmations: 1, timeout: 120_000 });
    assert.equal(receipt.status, 'success');
  }
  completed.push('owner-fund-and-sweep-through-migrated-custody');
  const concurrent = await Promise.all([0, 1].map(() => request(`/v1/agents/${encodeURIComponent(fixture.agent_did)}/fund`,
    { amount: fixture.fund_amount_wei }, owner)));
  assert(concurrent.every((response) => response.status === 200), JSON.stringify(concurrent));
  const concurrentTxs = await Promise.all(concurrent.map(async (response) => {
    const receipt = await chain.waitForTransactionReceipt({ hash: response.body.tx_hash, confirmations: 1, timeout: 120_000 });
    assert.equal(receipt.status, 'success');
    return chain.getTransaction({ hash: response.body.tx_hash });
  }));
  const nonces = concurrentTxs.map((tx) => tx.nonce).sort((a, b) => a - b);
  assert.equal(nonces[1], nonces[0]! + 1);
  completed.push('concurrent-owner-funding-retains-one-wallet-nonce-lock');
  const policyRefusal = await request('/v1/wallet/sign', { tx: {
    to: fixture.recipient, value: fixture.attestor_policy_refusal_value_wei, gas: '21000',
    maxFeePerGas: '2000000000', maxPriorityFeePerGas: '1000000000', chainId: fixture.chain_id,
  } }, owner);
  assert.equal(policyRefusal.status, 403, JSON.stringify(policyRefusal.body));
  assert.equal(policyRefusal.body.error, 'attestor_policy_refused');
  completed.push('independent-attestor-policy-refusal');
  await pool.query("update wallets set kind = 'funded' where id = $1", [ownerWallet.id]);
  const retiredFunded = (await pool.query('select * from wallets where id = $1', [ownerWallet.id])).rows[0];
  await assert.rejects(walletsModule.getMigrationAwareSigningAccountForRow(retiredFunded,
    { scheme: 'supabase_jwt', token: ownerToken }), /wallet custody is unavailable/);
  await assert.rejects(walletsModule.getSigningAccountForRow(retiredFunded), /wallet custody is unavailable/);
  await assert.rejects(walletsModule.getMigrationAwareSigningAccountForRow(ownerWallet,
    { scheme: 'supabase_jwt', token: ownerToken }), /wallet custody is unavailable/);
  await pool.query("update wallets set kind = 'standard' where id = $1", [ownerWallet.id]);
  completed.push('retired-funded-lane-refuses-real-row-and-stale-row-signing');



  const agentBody = { tx: { to: fixture.recipient, value: '0', chainId: fixture.chain_id } };
  const readWrite = await request('/v1/agent/sign', agentBody, reader);
  assert.equal(readWrite.status, 403);
  assert.equal(readWrite.body.error, 'agent_token_read_only');
  const raw = JSON.stringify(agentBody);
  const badSignature = signedHeaders('/v1/agent/sign', raw);
  badSignature['x-agent-signature'] = '00'.repeat(64);
  assert.equal((await request('/v1/agent/sign', raw, badSignature)).status, 401);
  const expired = proof('POST /v1/agent/sign', fixture.agent_did, Buffer.from(raw), Math.floor(Date.now() / 1000) - 1);
  assert.equal((await request('/v1/agent/sign', raw, {
    'x-agent-key': fixture.agent_did, 'x-agent-nonce': expired.nonce,
    'x-agent-expires': String(expired.expiry), 'x-agent-signature': expired.signature,
  })).body.error, 'agent_request_expired');
  const replayHeaders = signedHeaders('/v1/agent/sign', raw);
  const challenge = await request('/v1/agent/sign', raw, replayHeaders);
  assert.equal(challenge.status, 409, JSON.stringify(challenge.body));
  assert.equal(challenge.body.error, 'agent_reauthorization_required');
  const wrongOwnerWire = { ...JSON.parse(challenge.body.signing_request), session_id: randomUUID(), key_id: ownerWallet.attestor_key_id };
  await wrongSubjectAtAttestor(fixture.attestor_endpoints[0]!, wrongOwnerWire);
  completed.push('attestor-independently-refuses-foreign-supabase-subject');

  assert.equal((await request('/v1/agent/sign', raw, replayHeaders)).body.error, 'agent_nonce_replayed');
  const agentSigned = await signedRequest('/v1/agent/sign', agentBody, challenge.body.signing_request);
  assert.equal(agentSigned.status, 200, JSON.stringify(agentSigned.body));
  assert.equal((await recoverTransactionAddress({ serializedTransaction: agentSigned.body.signed_tx })).toLowerCase(), fixture.agent_address.toLowerCase());
  await assertAudit(fixture.agent_did, 1);
  completed.push('agent-original-and-exact-transaction-authority-with-replay-expiry-read-token-refusals');
  const largeMessageBody = { message: 'm'.repeat(10_000) };
  const largeChallenge = await signedRequest('/v1/agent/sign-message', largeMessageBody);
  assert.equal(largeChallenge.status, 409, JSON.stringify(largeChallenge.body));
  const largeSigned = await signedRequest('/v1/agent/sign-message', largeMessageBody, largeChallenge.body.signing_request);
  assert.equal(largeSigned.status, 200, JSON.stringify(largeSigned.body));
  assert.equal((await recoverMessageAddress({ message: largeMessageBody.message, signature: largeSigned.body.signature })).toLowerCase(), fixture.agent_address.toLowerCase());
  completed.push('supported-10000-character-message-uses-real-authorization-upload-handle');


  assert.equal(fixture.membership_mismatch_verified, true);
  completed.push('membership-mismatch-refuses-signing');
  const authorityModule = await import(pathToFileURL(resolve('dist/agent/authority.js')).href);
  for (const badScope of [{ protocol: 'xweb' }, { protocol: 'bridge' }, { threshold: 2 }]) {
    const priorInventory = structuredClone(inventory);
    approveInventory({ ...inventory, ...badScope });
    assert.throws(() => authorityModule.loadWalletInventory(), { code: 'custody_authority_unavailable' });
    const refused = await request('/v1/wallet/sign-message', { message: message + '-bad-scope' }, owner);
    assert(refused.status >= 400, 'foreign signer set or changed threshold must refuse');
    approveInventory(priorInventory);
    authorityModule.loadWalletInventory();
  }
  const validToken = readFileSync(fixture.inventory_files[0]!, 'utf8').trim();
  for (const path of fixture.inventory_files) renameSync(path, path + '.qualification-missing');
  assert.throws(() => authorityModule.loadWalletInventory(), { code: 'custody_authority_unavailable' });
  assert((await request('/v1/wallet/sign-message', { message: message + '-missing-inventory' }, owner)).status >= 400);
  for (const path of fixture.inventory_files) renameSync(path + '.qualification-missing', path);
  const corruptParts = validToken.split('.');
  const corruptSignature = Buffer.from(corruptParts[2]!, 'base64url');
  corruptSignature[0] = corruptSignature[0]! ^ 1;
  corruptParts[2] = corruptSignature.toString('base64url');
  writeInventoryToken(corruptParts.join('.'));
  assert.throws(() => authorityModule.loadWalletInventory(), { code: 'custody_authority_unavailable' });
  assert((await request('/v1/wallet/sign-message', { message: message + '-bad-inventory-signature' }, owner)).status >= 400);
  writeInventoryToken(validToken);
  approveInventory({ ...inventory });
  authorityModule.loadWalletInventory();
  const advancedToken = readFileSync(fixture.inventory_files[0]!, 'utf8').trim();
  writeInventoryToken(validToken);
  assert.throws(() => authorityModule.loadWalletInventory(), { code: 'custody_authority_unavailable' });
  assert((await request('/v1/wallet/sign-message', { message: message + '-inventory-rollback' }, owner)).status >= 400);
  writeInventoryToken(advancedToken);
  authorityModule.loadWalletInventory();
  completed.push('missing-invalid-signature-and-rollback-inventory-refusals');
  const validEpochInventory = structuredClone(inventory);
  approveInventory({ ...inventory, keys: inventory.keys.map((entry: any) => entry.key_id === ownerWallet.attestor_key_id
    ? { ...entry, epoch: entry.epoch + 1 } : entry) });
  const epochRefusal = await request('/v1/wallet/sign-message', { message: message + '-bad-epoch' }, owner);
  assert(epochRefusal.status >= 400, 'approved epoch must match held shares before signing');
  approveInventory(validEpochInventory);
  const epochRecovered = await request('/v1/wallet/sign-message', { message: message + '-restored-epoch' }, owner);
  assert.equal(epochRecovered.status, 200, JSON.stringify(epochRecovered.body));
  completed.push('wallet-xweb-bridge-threshold-and-share-epoch-mismatches-refuse');


  const actionPath = '/v1/agent/actions/allowance-and-call';
  const actionBody = { ...fixture.durable, idempotency_key: 'custody-' + randomBytes(12).toString('hex') };
  const submitted = await signedRequest(actionPath, actionBody);
  assert.equal(submitted.status, 202, JSON.stringify(submitted.body));
  const actionId = submitted.body.action_id;
  const readAction = () => request(`/v1/agent/actions/${actionId}`, undefined, reader);
  let action = await poll(readAction, (value) => value.body.error?.code === 'AGENT_REAUTHORIZATION_REQUIRED');
  const draftBefore = (await pool.query('select request from agent_actions where id = $1', [actionId])).rows[0].request._execution;
  const reservations = (await pool.query('select * from custody_signing_reservations where action_id = $1', [actionId])).rows;
  assert.equal(reservations.length, 1);
  const contested = await signedRequest('/v1/agent/send', agentBody);
  assert(contested.status >= 400, 'direct routes must refuse another action nonce reservation');
  const changed = JSON.parse(action.body.error.cause.signing_request);
  changed.session_id += '-changed';
  const tampered = await signedRequest(actionPath, actionBody, JSON.stringify(changed));
  assert.equal(tampered.status, 409, 'durable reauthorization may not retarget the retained session/transaction');
  for (const field of ['nonce', 'maxFeePerGas', 'maxPriorityFeePerGas'] as const) {
    const altered = JSON.parse(action.body.error.cause.signing_request);
    const parsed = parseTransaction(`0x${altered.transaction}`);
    const modified = field === 'nonce'
      ? { ...parsed, nonce: parsed.nonce! + 1 }
      : { ...parsed, [field]: (parsed[field] ?? 0n) + 1n };
    altered.transaction = serializeTransaction(modified as never).slice(2);
    const refused = await signedRequest(actionPath, actionBody, JSON.stringify(altered));
    assert.equal(refused.status, 409, `durable final authorization cannot change ${field}`);
  }

  await request('/control/arm', {}, {}, fixture.control_url);
  const authorized = await signedRequest(actionPath, actionBody, action.body.error.cause.signing_request);
  assert.equal(authorized.status, 200, JSON.stringify(authorized.body));
  await poll(() => request('/control/checkpoint', undefined, {}, fixture.control_url), (value) => value.body.intercepted === true);
  const beforeCrash = (await pool.query('select * from agent_actions where id = $1', [actionId])).rows[0];
  const leg = beforeCrash.call_tx_hash ? 'call' : 'approval';
  const retained = beforeCrash.request._execution[leg].current;
  assert(retained?.raw && retained?.hash, 'raw transaction must be durable before the RPC send');
  assert.equal(keccak256(retained.raw), retained.hash);
  assert.equal(retained.hash, beforeCrash[`${leg}_tx_hash`]);
  assert.equal((await pool.query('select signed_hash from custody_signing_reservations where action_id = $1', [actionId])).rows[0].signed_hash, retained.hash);
  assert.equal(parseTransaction(retained.raw).nonce, beforeCrash[`${leg}_nonce`]);
  assert.deepEqual(retained.draft, draftBefore[leg].draft);
  const restart = await request('/control/restart', {}, {}, fixture.control_url);
  assert.equal(restart.body.restarted, true);
  const receipt = await chain.waitForTransactionReceipt({ hash: retained.hash, confirmations: 1, timeout: 180_000 });
  assert.equal(receipt.status, 'success');
  for (let remaining = 0; remaining < 6; remaining++) {
    action = await poll(readAction, (value) => value.body.terminal === true || value.body.error?.code === 'AGENT_REAUTHORIZATION_REQUIRED');
    if (action.body.terminal) break;
    const next = await signedRequest(actionPath, actionBody, action.body.error.cause.signing_request);
    assert.equal(next.status, 200, JSON.stringify(next.body));
  }
  assert.equal(action.body.status, 'confirmed', JSON.stringify(action.body));
  const final = (await pool.query('select * from agent_actions where id = $1', [actionId])).rows[0];
  assert.equal(final.request._execution[leg].current.hash, retained.hash);
  const actions = await pool.query('select id from agent_actions where did = $1 and idempotency_key = $2', [fixture.agent_did, actionBody.idempotency_key]);
  assert.deepEqual(actions.rows.map((row) => row.id), [actionId]);
  const replay = await signedRequest(actionPath, actionBody);
  assert.equal(replay.body.action_id, actionId);
  assert.equal(replay.body.status, 'confirmed');
  const counts = await request('/control/counts', undefined, {}, fixture.control_url);
  assert(counts.body.counts.every((count: number) => count === 1), 'the real RPC must see each signed transaction only once');
  assert.equal((await pool.query('select count(*)::int as count from custody_signing_reservations where action_id = $1', [actionId])).rows[0].count, 0);
  await assertAudit(fixture.agent_did, 2);
  completed.push('durable-exact-reauthorization-persist-before-send-process-kill-restart-no-duplicate-nonce-or-broadcast');

  const frozen = await request(`/v1/agents/${encodeURIComponent(fixture.agent_did)}/freeze`, {}, owner);
  assert.equal(frozen.status, 200, JSON.stringify(frozen.body));
  const denied = await signedRequest('/v1/agent/sign', agentBody);
  assert.equal(denied.status, 403);
  assert.equal(denied.body.error, 'agent_frozen');
  completed.push('owner-freeze-refusal');
  const twoStopped = await request('/control/stop-two', {}, {}, fixture.control_url);
  assert.equal(twoStopped.body.stopped, 2);
  await poll(() => request('/readyz'), (response) => response.status === 200 && response.body.components.attestors.healthy === 3);
  const thresholdMessage = message + '-three-of-five';
  const thresholdSignature = await request('/v1/wallet/sign-message', { message: thresholdMessage }, owner);
  assert.equal(thresholdSignature.status, 200, JSON.stringify(thresholdSignature.body));
  assert.equal((await recoverMessageAddress({ message: thresholdMessage, signature: thresholdSignature.body.signature })).toLowerCase(), fixture.owner_address.toLowerCase());
  await assertAudit(fixture.owner_subject, 1);
  completed.push('three-approved-members-sign-with-two-offline');
  const quorumStopped = await request('/control/stop-quorum', {}, {}, fixture.control_url);
  assert.equal(quorumStopped.body.stopped, 3);
  await poll(() => request('/readyz'), (response) => response.status === 503 && response.body.components.attestors.healthy < 3);
  const noQuorum = await request('/v1/wallet/sign-message', { message: message + '-no-quorum' }, owner);
  assert.equal(noQuorum.status, 503, JSON.stringify(noQuorum.body));
  completed.push('insufficient-members-refuse-without-legacy-fallback');

  writeFileSync(resolve(fixture.evidence_dir, 'cases-result.json'), JSON.stringify({ complete: true, cases: completed }, null, 2), { mode: 0o600 });
} finally {
  await pool.end();
  await dbModule.closePool();

}
