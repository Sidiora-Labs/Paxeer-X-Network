import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { privateKeyToAccount } from 'viem/accounts';
import { verifyMessage, type Hex } from 'viem';
import {
  LIVESTREAM_SIGNATURE_HEADER, OPERATION_ENVELOPE_HEADER, OPERATION_SIGNATURE_HEADER,
  buildOperationEnvelope, canonicalOperationJSON, encodeOperationEnvelope, operationHeaders, operationStatement, signOperation,
  type OperationEnvelope, type OperationMethod, type OperationVersion,
} from './operation-envelope';

/**
 * Vectors produced by the production Go code: test-fixtures/operation-envelope/main.go
 * (`GOWORK=off go run .` in that directory) builds each envelope with
 * shared/auth OperationEnvelope, takes Statement(), signs it with go-ethereum
 * crypto.Sign(accounts.TextHash(statement)) and self-checks the result through
 * the same decode / Statement / VerifyEOA / Validate steps userd runs.
 */
interface GoVector { name: string; body: string; envelope: OperationEnvelope; statement: string; encoded: string; signature: Hex }
const fixture = JSON.parse(readFileSync(fileURLToPath(new URL('./test-fixtures/operation-envelope/go-vectors.json', import.meta.url)), 'utf8')) as { privateKey: Hex; actor: string; vectors: GoVector[] };
const account = privateKeyToAccount(fixture.privateKey);

describe('operation envelope against the Go production signer', () => {
  it('uses the key the Go generator used', () => {
    expect(account.address.toLowerCase()).toBe(fixture.actor);
    expect(fixture.vectors.length).toBe(7);
  });

  for (const vector of fixture.vectors) {
    it(`${vector.name}: envelope, statement, header and signature match byte for byte`, async () => {
      const { envelope } = vector;
      const built = await buildOperationEnvelope({
        actor: account.address, action: envelope.action, method: envelope.method as OperationMethod, path: envelope.path,
        body: vector.body, version: envelope.version as OperationVersion, environment: envelope.environment,
        lifetimeSeconds: envelope.expiresAt - envelope.issuedAt,
      }, envelope.nonce, envelope.issuedAt);
      expect(built).toEqual(envelope);
      expect(operationStatement(built)).toBe(vector.statement);
      expect(encodeOperationEnvelope(built)).toBe(vector.encoded);
      const signature = await account.signMessage({ message: operationStatement(built) });
      expect(signature).toBe(vector.signature);
      expect(await verifyMessage({ address: account.address, message: vector.statement, signature: vector.signature })).toBe(true);
    });
  }

  it('escapes HTML-significant characters the way encoding/json does', () => {
    const vector = fixture.vectors.find(entry => entry.name === 'html-escaped-path')!;
    expect(canonicalOperationJSON(vector.envelope)).toContain('"path":"/v1/a\\u0026b=c@d"');
    expect(vector.statement).toContain('\\u0026');
  });
});

describe('signOperation', () => {
  it('signs a fresh v2 envelope that verifies for the actor and names the production binding', async () => {
    const body = JSON.stringify({ alertEnabled: false });
    const path = '/v1/me/watchlist/0xce20da50f92d21d8bee497910c782e9aa25b6263';
    let checks = 0;
    const signed = await signOperation({ actor: account.address, action: 'watchlist.update', method: 'PUT', path, body }, { signMessage: message => account.signMessage({ message }), assertCurrent: () => { checks++; } });
    expect(checks).toBe(2);
    expect(signed.envelope).toMatchObject({ version: 2, chainId: '125', environment: 'production', actor: fixture.actor, action: 'watchlist.update', method: 'PUT', path });
    expect(signed.envelope.expiresAt - signed.envelope.issuedAt).toBe(300);
    expect(Math.abs(signed.envelope.issuedAt - Date.now() / 1000)).toBeLessThan(5);
    expect(signed.envelope.nonce).toMatch(/^[0-9a-f]{64}$/);
    const decoded = JSON.parse(Buffer.from(signed.encoded, 'base64url').toString('utf8')) as OperationEnvelope;
    expect(decoded).toEqual(signed.envelope);
    expect(signed.encoded).not.toMatch(/[=+/]/);
    expect(await verifyMessage({ address: account.address, message: signed.statement, signature: signed.signature as Hex })).toBe(true);
    expect(operationHeaders(signed)).toEqual({ [OPERATION_ENVELOPE_HEADER]: signed.encoded, [OPERATION_SIGNATURE_HEADER]: signed.signature });
    expect(Object.keys(operationHeaders(signed, LIVESTREAM_SIGNATURE_HEADER))).toEqual(['X-Kindle-Operation', 'X-Kindle-Signature']);
  });

  it('hashes the empty body of a DELETE (BodyDigestV2)', async () => {
    const signed = await signOperation({ actor: account.address, action: 'watchlist.remove', method: 'DELETE', path: '/v1/me/watchlist/0xce20da50f92d21d8bee497910c782e9aa25b6263', body: '' }, { signMessage: message => account.signMessage({ message }) });
    expect(signed.envelope.bodyHash).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
  });

  it('refuses what the Go Statement() refuses', async () => {
    const request = { actor: account.address, action: 'profile.update', method: 'PATCH' as const, path: '/v3/me/preferences', body: '{"a":1}' };
    const nonce = 'ab'.repeat(32);
    await expect(buildOperationEnvelope({ ...request, path: '/v3/me/../preferences' }, nonce, 1)).rejects.toThrow('canonical');
    await expect(buildOperationEnvelope({ ...request, path: '/v3/me/preferences?x=1' }, nonce, 1)).rejects.toThrow('canonical');
    await expect(buildOperationEnvelope({ ...request, path: '/v1/a<b' }, nonce, 1)).rejects.toThrow('canonical');
    await expect(buildOperationEnvelope({ ...request, path: '/v3/me/preferences/' }, nonce, 1)).rejects.toThrow('canonical');
    await expect(buildOperationEnvelope({ ...request, lifetimeSeconds: 301 }, nonce, 1)).rejects.toThrow('lifetime');
    await expect(buildOperationEnvelope({ ...request, body: '' }, nonce, 1)).rejects.toThrow('needs a body');
    await expect(buildOperationEnvelope({ ...request, environment: 'prod env' }, nonce, 1)).rejects.toThrow('Invalid operation envelope');
    await expect(buildOperationEnvelope({ ...request, actor: '0x0000000000000000000000000000000000000000' }, nonce, 1)).rejects.toThrow('normalized wallet');
    await expect(buildOperationEnvelope(request, 'AB'.repeat(32), 1)).rejects.toThrow('nonce');
  });

  it('rejects a malformed wallet signature', async () => {
    await expect(signOperation({ actor: account.address, action: 'profile.update', method: 'PATCH', path: '/v3/me/preferences', body: '{}' }, { signMessage: async () => 'not-a-signature' })).rejects.toThrow('invalid signature');
  });
});
