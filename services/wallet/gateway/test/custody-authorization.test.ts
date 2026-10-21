import assert from 'node:assert/strict';
import { constants, closeSync, fstatSync, openSync, readFileSync } from 'node:fs';
import { Pool, type PoolClient } from 'pg';
import { keccak256, type Hex } from 'viem';
import { it } from 'vitest';
import type { SignResult } from '../src/attestor/client.js';
import {
  completeCustodyAuthorization, readCustodyAuthorization, retainCustodyAuthorization,
  type CustodyAuthorizationIdentity,
} from '../src/custody/authorization.js';

interface Fixture {
  isolated_database: true;
  database_url: string;
  signed: { identity: CustodyAuthorizationIdentity; signature: Hex; evidence: SignResult };
  unknown: { identity: CustodyAuthorizationIdentity };
  foreign_user_id: string;
}

function fixture(): Fixture {
  const path = process.env.WALLET_CUSTODY_AUTHORIZATION_FIXTURE;
  assert(path, 'genuine custody authorization fixture is required');
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const metadata = fstatSync(descriptor);
    assert(metadata.isFile() && metadata.nlink === 1 && (metadata.mode & 0o777) === 0o600
      && metadata.uid === process.getuid?.() && metadata.size > 0 && metadata.size <= 131072,
    'custody authorization fixture must be an owner-only regular file');
    const loaded = JSON.parse(readFileSync(descriptor, 'utf8')) as Fixture;
    assert.equal(loaded.isolated_database, true, 'only an explicitly isolated qualification database is admitted');
    const url = new URL(loaded.database_url);
    assert(['postgres:', 'postgresql:'].includes(url.protocol)
      && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
      && /^\/wallet_custody_qualification_[a-zA-Z0-9_]+$/.test(url.pathname),
    'qualification requires a dedicated local custody database');
    assert.notEqual(loaded.signed.identity.id, loaded.unknown.identity.id);
    assert.notEqual(loaded.foreign_user_id, loaded.signed.identity.userId);
    return loaded;
  } finally {
    closeSync(descriptor);
  }
}

async function transaction<T>(pool: Pool, work: (client: PoolClient) => Promise<T>): Promise<T> {
  const client = await pool.connect();
  try {
    await client.query('begin');
    const result = await work(client);
    await client.query('commit');
    return result;
  } catch (error) {
    await client.query('rollback');
    throw error;
  } finally {
    client.release();
  }
}

it('retains actual custody evidence across lost replies, restart, replay and strict identity refusals', async () => {
  const supplied = fixture();
  let pool = new Pool({ connectionString: supplied.database_url, connectionTimeoutMillis: 10000 });
  const { identity, signature, evidence } = supplied.signed;
  const unknown = supplied.unknown.identity;
  const ids = [identity.id, unknown.id];
  let ownsRows = false;
  try {
    await pool.query(readFileSync(new URL('../migrations/013_wallet_custody_authorizations.sql', import.meta.url), 'utf8'));
    const existing = await pool.query('select id from wallet_custody_authorizations where id=any($1::text[])', [ids]);
    assert.equal(existing.rowCount, 0, 'genuine fixture identities must be unused in this isolated store');
    ownsRows = true;
    const retained = await transaction(pool, client => retainCustodyAuthorization(client, identity));
    assert.equal(retained.attempt, true);
    assert.equal(retained.record.state, 'signing_unknown');
    assert.equal(retained.record.signature, null);
    assert.equal(retained.record.evidence, null);
    await pool.end();
    pool = new Pool({ connectionString: supplied.database_url, connectionTimeoutMillis: 10000 });
    const resumed = await transaction(pool, client => retainCustodyAuthorization(client, identity));
    assert.equal(resumed.attempt, false, 'ambiguous outcome must never create a fresh signing attempt');
    assert.equal(resumed.record.state, 'signing_unknown');
    const saved = await transaction(pool, client => completeCustodyAuthorization(client, identity, signature, evidence));
    assert.equal(saved.state, 'signed');
    assert.equal(saved.signature, signature);
    assert.deepEqual(saved.evidence, evidence);
    await pool.end();
    pool = new Pool({ connectionString: supplied.database_url, connectionTimeoutMillis: 10000 });
    const recovered = await transaction(pool, client => readCustodyAuthorization(client, identity));
    assert.equal(recovered?.signature, signature, 'response loss and reconnect recover the original signature');
    assert.deepEqual(recovered?.evidence, evidence);
    const replayed = await transaction(pool, client => retainCustodyAuthorization(client, identity));
    assert.equal(replayed.attempt, false);
    assert.equal(replayed.record.signature, signature);
    assert.deepEqual(await transaction(pool, client => completeCustodyAuthorization(client, identity, signature, evidence)), recovered);

    for (const changed of [
      { ...identity, userId: supplied.foreign_user_id },
      { ...identity, address: '0x0000000000000000000000000000000000000001' as Hex },
      { ...identity, chainId: identity.chainId + 1 },
      { ...identity, walletId: supplied.foreign_user_id },
      { ...identity, keyId: identity.keyId + '-changed' },
      { ...identity, custody: '0x01' as Hex },
      { ...identity, id: keccak256('0x01') },
    ]) {
      await assert.rejects(transaction(pool, client => readCustodyAuthorization(client, changed)),
        { code: 'custody_authorization_mismatch' });
      await assert.rejects(transaction(pool, client => retainCustodyAuthorization(client, changed)),
        { code: 'custody_authorization_mismatch' });
    }
    await assert.rejects(transaction(pool, client => completeCustodyAuthorization(client, identity, signature,
      { ...evidence, sessionId: '00000000-0000-4000-8000-000000000001' })),
    { code: 'custody_authorization_proof_changed' });
    await assert.rejects(transaction(pool, client => completeCustodyAuthorization(client, identity, signature,
      { ...evidence, signedBytes: keccak256('0x01') })), { code: 'custody_authorization_proof_invalid' });
    await assert.rejects(transaction(pool, client => client.query(
      'update wallet_custody_authorizations set user_id=$2 where id=$1', [identity.id, supplied.foreign_user_id])));
    await assert.rejects(transaction(pool, client => client.query(
      'update wallet_custody_authorizations set evidence=$2::jsonb where id=$1', [identity.id, '{}'])));
    await assert.rejects(transaction(pool, client => client.query(
      "update wallet_custody_authorizations set state='signing_unknown',signature=null,evidence=null,completed_at=null where id=$1", [identity.id])));

    const ambiguous = await transaction(pool, client => retainCustodyAuthorization(client, unknown));
    assert.equal(ambiguous.attempt, true);
    await assert.rejects(transaction(pool, client => completeCustodyAuthorization(client, unknown, signature, evidence)),
      { code: 'custody_authorization_proof_invalid' });
    const unchanged = await transaction(pool, client => retainCustodyAuthorization(client, unknown));
    assert.equal(unchanged.attempt, false);
    assert.equal(unchanged.record.state, 'signing_unknown');
    assert.equal(unchanged.record.signature, null);
    assert.equal(unchanged.record.evidence, null);

    await transaction(pool, async client => {
      await client.query('delete from wallet_custody_authorizations where id=$1', [unknown.id]);
      await client.query(`insert into wallet_custody_authorizations
        (id,user_id,wallet_id,address,chain_id,attestor_key_id,custody,state,signature,evidence,completed_at)
        values($1,$2,$3,$4,$5,$6,$7,'signed',$8,'{}'::jsonb,now())`,
      [unknown.id, unknown.userId, unknown.walletId, unknown.address.toLowerCase(), unknown.chainId,
        unknown.keyId, unknown.custody.toLowerCase(), signature]);
    });
    await assert.rejects(transaction(pool, client => readCustodyAuthorization(client, unknown)),
      { code: 'custody_authorization_proof_invalid' });
  } finally {
    if (ownsRows) await pool.query('delete from wallet_custody_authorizations where id=any($1::text[])', [ids]);
    await pool.end();
  }
}, 90000);
