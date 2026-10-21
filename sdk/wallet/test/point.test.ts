import { describe, expect, it } from 'vitest';
import { ed25519, ED25519_TORSION_SUBGROUP } from '@noble/curves/ed25519.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { PaxeerWallet, PaxeerWalletError } from '../src/index.js';

const CANONICAL_Y3 = '0300000000000000000000000000000000000000000000000000000000000000';
const NONCANONICAL_Y3 = 'f0ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f';

function hex(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString('hex');
}

function uint(value: bigint | number, size: number): Uint8Array {
  const out = new Uint8Array(size);
  let rest = BigInt(value);
  for (let i = size - 1; i >= 0; i--) {
    out[i] = Number(rest & 0xffn);
    rest >>= 8n;
  }
  return out;
}

function field(tag: number, ...parts: Uint8Array[]): Uint8Array[] {
  return [uint(tag, 1), ...parts];
}

function sized(bytes: Uint8Array): Uint8Array[] {
  return [uint(bytes.length, 4), bytes];
}

function concat(parts: Uint8Array[]): Uint8Array {
  return Uint8Array.from(Buffer.concat(parts));
}

function activity(publicKey: string): string {
  const key = Uint8Array.from(Buffer.from(publicKey, 'hex'));
  const payload = new TextEncoder().encode('point-check');
  const payloadHash = sha256(concat([new TextEncoder().encode('LXP/v1/payload-hash\0'), payload]));
  return hex(concat([
    uint(1, 2), uint(0x1001, 2), uint(11, 1),
    ...field(1, uint(1, 2)),
    ...field(2, uint(1, 4)),
    ...field(3, uint(65_536 + 1, 4)),
    ...field(4, ...sized(new TextEncoder().encode(`did:layerx:${publicKey}`))),
    ...field(5, ...sized(key)),
    ...field(6, uint(1, 8)),
    ...field(7, uint(0, 8), uint(1, 8)),
    ...field(8, ...sized(new Uint8Array(32).fill(7))),
    ...field(9, uint(0, 16)),
    ...field(10, ...sized(payloadHash)),
    ...field(11, ...sized(payload)),
  ]));
}

function wallet(): PaxeerWallet {
  const retained = new Map<string, string>();
  return new PaxeerWallet({
    apiUrl: 'https://wallet.invalid',
    supabaseUrl: 'https://project.supabase.invalid',
    supabaseAnonKey: 'synthetic-publishable-key',
    fetch: () => Promise.reject(new Error('the point check must not reach the network')),
    lxActivity: {
      confirm: () => false,
      storage: {
        getItem: (name) => retained.get(name) ?? null,
        setItem: (name, value) => { retained.set(name, value); },
      },
    },
  });
}

async function refusal(publicKey: string): Promise<{ code: string; status: number | undefined }> {
  const error = await wallet().reviewLxActivity(activity(publicKey)).then(
    () => { throw new Error('review unexpectedly succeeded'); },
    (reason: unknown) => reason,
  );
  expect(error).toBeInstanceOf(PaxeerWalletError);
  const { code, status } = error as PaxeerWalletError;
  return { code, status };
}

describe('LX public key point check', () => {
  it('accepts a canonical non-small-order key and proceeds to authentication', async () => {
    const publicKey = hex(ed25519.getPublicKey(new Uint8Array(32).fill(0x2a)));
    const point = ed25519.ExtendedPoint.fromHex(publicKey);
    expect(point.isSmallOrder()).toBe(false);
    expect(hex(point.toRawBytes())).toBe(publicKey);
    expect(await refusal(publicKey)).toEqual({ code: 'NO_SESSION', status: 401 });
  });

  it('accepts the canonical encoding of a low y-coordinate point', async () => {
    const point = ed25519.ExtendedPoint.fromHex(CANONICAL_Y3);
    expect(point.isSmallOrder()).toBe(false);
    expect(hex(point.toRawBytes())).toBe(CANONICAL_Y3);
    expect(await refusal(CANONICAL_Y3)).toEqual({ code: 'NO_SESSION', status: 401 });
  });

  it('refuses every small-order key', async () => {
    expect(ED25519_TORSION_SUBGROUP).toHaveLength(8);
    const orderEight = ED25519_TORSION_SUBGROUP[1] as string;
    const point = ed25519.ExtendedPoint.fromHex(orderEight);
    expect(point.isSmallOrder()).toBe(true);
    expect(hex(point.toRawBytes())).toBe(orderEight);
    for (const publicKey of ED25519_TORSION_SUBGROUP) {
      expect(await refusal(publicKey)).toEqual({ code: 'LX_PUBLIC_KEY_INVALID', status: 400 });
    }
  });

  it('refuses a non-canonical encoding of a valid point', async () => {
    const y = BigInt(`0x${Buffer.from(NONCANONICAL_Y3, 'hex').reverse().toString('hex')}`);
    expect(y).toBe(2n ** 255n - 19n + 3n);
    expect(await refusal(NONCANONICAL_Y3)).toEqual({ code: 'LX_PUBLIC_KEY_INVALID', status: 400 });
  });
});
