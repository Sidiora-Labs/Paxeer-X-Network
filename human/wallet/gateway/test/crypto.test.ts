import { describe, expect, it } from 'vitest';
import { randomBytes } from 'node:crypto';
import {
  CryptoError,
  decrypt,
  encrypt,
  loadMasterKey,
  safeEqual,
  _resetMasterKeyCacheForTests,
} from '../src/crypto.js';

const MASTER_KEY = randomBytes(32);

describe('crypto envelope (AES-256-GCM)', () => {
  it('round-trips a plaintext private key', () => {
    const pk = randomBytes(32); // simulates an EVM private key
    const { ciphertext, version } = encrypt(pk, MASTER_KEY);
    expect(version).toBe(1);
    expect(ciphertext.length).toBeGreaterThan(0);
    const decrypted = decrypt(ciphertext, MASTER_KEY);
    expect(decrypted.equals(pk)).toBe(true);
  });

  it('produces different ciphertexts for the same plaintext (random IV)', () => {
    const pk = randomBytes(32);
    const a = encrypt(pk, MASTER_KEY).ciphertext;
    const b = encrypt(pk, MASTER_KEY).ciphertext;
    expect(a).not.toBe(b);
  });

  it('rejects a wrong master key', () => {
    const pk = randomBytes(32);
    const { ciphertext } = encrypt(pk, MASTER_KEY);
    const wrong = randomBytes(32);
    expect(() => decrypt(ciphertext, wrong)).toThrow(CryptoError);
  });

  it('rejects a tampered ciphertext (auth tag check)', () => {
    const pk = randomBytes(32);
    const { ciphertext } = encrypt(pk, MASTER_KEY);
    const buf = Buffer.from(ciphertext, 'base64');
    // flip a bit in the encrypted region (after version + iv + tag = 1 + 12 + 16 = 29 bytes)
    buf[buf.length - 1] ^= 0x01;
    const tampered = buf.toString('base64');
    expect(() => decrypt(tampered, MASTER_KEY)).toThrow(CryptoError);
  });

  it('rejects a master key of the wrong length', () => {
    const pk = randomBytes(32);
    expect(() => encrypt(pk, randomBytes(31))).toThrow(/master key must be exactly 32 bytes/);
  });

  it('rejects truncated envelopes', () => {
    expect(() => decrypt('AAA=', MASTER_KEY)).toThrow(/envelope too short|unsupported/i);
  });

  it('rejects an unsupported version byte', () => {
    const pk = randomBytes(32);
    const { ciphertext } = encrypt(pk, MASTER_KEY);
    const buf = Buffer.from(ciphertext, 'base64');
    buf[0] = 99; // invalid version
    expect(() => decrypt(buf.toString('base64'), MASTER_KEY)).toThrow(/unsupported envelope version/);
  });
});

describe('safeEqual', () => {
  it('returns true for equal buffers', () => {
    expect(safeEqual('abc', 'abc')).toBe(true);
  });
  it('returns false for unequal buffers of same length', () => {
    expect(safeEqual('abc', 'abd')).toBe(false);
  });
  it('returns false for different lengths', () => {
    expect(safeEqual('abc', 'abcd')).toBe(false);
  });
});

describe('loadMasterKey', () => {
  it('loads and caches a 32-byte key', () => {
    _resetMasterKeyCacheForTests();
    const b64 = randomBytes(32).toString('base64');
    const k1 = loadMasterKey(b64);
    const k2 = loadMasterKey(b64);
    expect(k1).toBe(k2); // cached, same buffer
    expect(k1.byteLength).toBe(32);
  });

  it('rejects keys of the wrong length', () => {
    _resetMasterKeyCacheForTests();
    expect(() => loadMasterKey(randomBytes(16).toString('base64'))).toThrow(
      /master key must be exactly 32 bytes/,
    );
  });
});
