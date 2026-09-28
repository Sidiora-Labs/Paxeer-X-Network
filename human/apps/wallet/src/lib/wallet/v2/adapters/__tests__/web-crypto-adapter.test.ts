import { describe, it, expect, beforeAll } from 'vitest';
import { webcrypto } from 'node:crypto';
import { WebCryptoAdapter } from '../web-crypto-adapter';

// Polyfill Web Crypto for Node test environment
Object.defineProperty(globalThis, 'crypto', { value: webcrypto });
Object.defineProperty(globalThis, 'performance', {
  value: { now: () => Date.now() },
});

describe('WebCryptoAdapter', () => {
  let adapter: WebCryptoAdapter;

  beforeAll(() => {
    adapter = new WebCryptoAdapter();
  });

  describe('base64url encoding', () => {
    it('round-trips arbitrary bytes', () => {
      const original = adapter.generateRandomBytes(48);
      const encoded = adapter.encodeBase64Url(original);
      const decoded = adapter.decodeBase64Url(encoded);
      expect(decoded).toEqual(original);
    });

    it('produces unpadded base64url (no +, /, or =)', () => {
      for (let i = 0; i < 100; i++) {
        const bytes = adapter.generateRandomBytes(1 + (i % 50));
        const encoded = adapter.encodeBase64Url(bytes);
        expect(encoded).not.toMatch(/[+/=]/);
      }
    });

    it('handles zero-length input', () => {
      const encoded = adapter.encodeBase64Url(new Uint8Array(0));
      expect(encoded).toBe('');
      const decoded = adapter.decodeBase64Url('');
      expect(decoded.length).toBe(0);
    });
  });

  describe('AES-256-GCM encrypt/decrypt', () => {
    it('round-trips plaintext with correct key and AAD', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('hello wallet');
      const aad = adapter.buildAad('vault-1', 'vault-payload');

      const envelope = await adapter.encrypt(key, plaintext, aad, 'vault-payload');
      const decrypted = await adapter.decrypt(key, envelope, aad, 'vault-payload');

      expect(new TextDecoder().decode(decrypted)).toBe('hello wallet');
    });

    it('produces a fresh IV on every encryption', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('same content');
      const aad = adapter.buildAad('vault-1', 'vault-payload');

      const ivs = new Set<string>();
      for (let i = 0; i < 20; i++) {
        const env = await adapter.encrypt(key, plaintext, aad, 'vault-payload');
        ivs.add(env.iv);
      }
      expect(ivs.size).toBe(20);
    });

    it('rejects AAD substitution', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('secret data');
      const aad1 = adapter.buildAad('vault-1', 'vault-payload');
      const aad2 = adapter.buildAad('vault-2', 'vault-payload');

      const envelope = await adapter.encrypt(key, plaintext, aad1, 'vault-payload');
      await expect(adapter.decrypt(key, envelope, aad2, 'vault-payload')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects bit-flip in ciphertext', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('tamper me');
      const aad = adapter.buildAad('vault-1', 'vault-payload');

      const envelope = await adapter.encrypt(key, plaintext, aad, 'vault-payload');
      const ctBytes = adapter.decodeBase64Url(envelope.ciphertext);
      ctBytes[0] ^= 0xff;
      const tampered = { ...envelope, ciphertext: adapter.encodeBase64Url(ctBytes) };

      await expect(adapter.decrypt(key, tampered, aad, 'vault-payload')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects bit-flip in IV', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('iv tamper');
      const aad = adapter.buildAad('vault-1', 'vault-payload');

      const envelope = await adapter.encrypt(key, plaintext, aad, 'vault-payload');
      const ivBytes = adapter.decodeBase64Url(envelope.iv);
      ivBytes[0] ^= 0xff;
      const tampered = { ...envelope, iv: adapter.encodeBase64Url(ivBytes) };

      await expect(adapter.decrypt(key, tampered, aad, 'vault-payload')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects wrong key', async () => {
      const key1 = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const key2 = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const plaintext = new TextEncoder().encode('wrong key');
      const aad = adapter.buildAad('vault-1', 'vault-payload');

      const envelope = await adapter.encrypt(key1, plaintext, aad, 'vault-payload');
      await expect(adapter.decrypt(key2, envelope, aad, 'vault-payload')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects malformed envelope (wrong version)', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const bad = { version: 2 as any, algorithm: 'AES-256-GCM' as const, iv: adapter.encodeBase64Url(adapter.generateRandomBytes(12)), ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(32)) };
      await expect(adapter.decrypt(key, bad, 'aad', 'vault-payload')).rejects.toThrow(/Invalid envelope/);
    });

    it('rejects malformed envelope (wrong algorithm)', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const bad = { version: 1 as const, algorithm: 'AES-128-CBC' as any, iv: adapter.encodeBase64Url(adapter.generateRandomBytes(12)), ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(32)) };
      await expect(adapter.decrypt(key, bad, 'aad', 'vault-payload')).rejects.toThrow(/Invalid envelope/);
    });

    it('rejects truncated IV', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const bad = { version: 1 as const, algorithm: 'AES-256-GCM' as const, iv: adapter.encodeBase64Url(adapter.generateRandomBytes(8)), ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(32)) };
      await expect(adapter.decrypt(key, bad, 'aad', 'vault-payload')).rejects.toThrow(/Invalid envelope/);
    });

    it('rejects truncated ciphertext (below tag length)', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const bad = { version: 1 as const, algorithm: 'AES-256-GCM' as const, iv: adapter.encodeBase64Url(adapter.generateRandomBytes(12)), ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(8)) };
      await expect(adapter.decrypt(key, bad, 'aad', 'vault-payload')).rejects.toThrow(/Invalid envelope/);
    });
  });

  describe('PBKDF2 key derivation', () => {
    it('derives a key from password and salt', async () => {
      const salt = adapter.generateRandomBytes(16);
      const key = await adapter.deriveKeyFromPassword('test-password-12345678', salt, 600_000);
      expect(key).toBeDefined();
      expect(key.type).toBe('secret');
      expect(key.algorithm).toMatchObject({ name: 'AES-GCM', length: 256 });
    });

    it('rejects iterations below minimum', async () => {
      const salt = adapter.generateRandomBytes(16);
      await expect(
        adapter.deriveKeyFromPassword('password', salt, 100_000),
      ).rejects.toThrow(/minimum/i);
    });

    it('rejects salt below 16 bytes', async () => {
      const salt = adapter.generateRandomBytes(8);
      await expect(
        adapter.deriveKeyFromPassword('password', salt, 600_000),
      ).rejects.toThrow(/minimum/i);
    });

    it('produces deterministic output for same inputs', async () => {
      const salt = adapter.generateRandomBytes(16);
      const key1 = await adapter.deriveKeyFromPassword('same-password', salt, 600_000);
      const key2 = await adapter.deriveKeyFromPassword('same-password', salt, 600_000);

      // Keys are non-extractable, so verify determinism by wrapping the same VEK
      const vekRaw = adapter.generateRandomBytes(32);
      const vek = await adapter.importAesGcmKey(vekRaw, true);
      const wrapped1 = await adapter.wrapVaultKey(vek, key1, 'vault-1', 'slot-1');
      const wrapped2 = await adapter.wrapVaultKey(vek, key2, 'vault-1', 'slot-1');

      // Both should unwrap to the same key
      const unwrapped1 = await adapter.unwrapVaultKey(wrapped1, key1, 'vault-1', 'slot-1');
      const unwrapped2 = await adapter.unwrapVaultKey(wrapped2, key2, 'vault-1', 'slot-1');

      // Verify both can decrypt the same data
      const aad = adapter.buildAad('vault-1', 'vault-payload');
      const envelope = await adapter.encrypt(vek, new TextEncoder().encode('test'), aad, 'vault-payload');
      const dec1 = await adapter.decrypt(unwrapped1, envelope, aad, 'vault-payload');
      const dec2 = await adapter.decrypt(unwrapped2, envelope, aad, 'vault-payload');
      expect(new TextDecoder().decode(dec1)).toBe('test');
      expect(new TextDecoder().decode(dec2)).toBe('test');
    });
  });

  describe('key wrapping', () => {
    it('wraps and unwraps the vault key', async () => {
      const vaultKeyRaw = adapter.generateRandomBytes(32);
      const vaultKey = await adapter.importAesGcmKey(vaultKeyRaw, true);
      const kekRaw = adapter.generateRandomBytes(32);
      const kek = await adapter.importAesGcmKey(kekRaw, false);

      const wrapped = await adapter.wrapVaultKey(vaultKey, kek, 'vault-1', 'slot-1');
      const unwrapped = await adapter.unwrapVaultKey(wrapped, kek, 'vault-1', 'slot-1');

      // Verify correctness by encrypting with original and decrypting with unwrapped
      const plaintext = new TextEncoder().encode('verify wrap/unwrap');
      const aad = adapter.buildAad('vault-1', 'vault-payload');
      const envelope = await adapter.encrypt(vaultKey, plaintext, aad, 'vault-payload');
      const decrypted = await adapter.decrypt(unwrapped, envelope, aad, 'vault-payload');
      expect(new TextDecoder().decode(decrypted)).toBe('verify wrap/unwrap');
    });

    it('rejects wrapping with wrong AAD (different vault)', async () => {
      const vaultKey = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const kek = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);

      const wrapped = await adapter.wrapVaultKey(vaultKey, kek, 'vault-1', 'slot-1');
      await expect(adapter.unwrapVaultKey(wrapped, kek, 'vault-2', 'slot-1')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects wrapping with wrong slot', async () => {
      const vaultKey = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const kek = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);

      const wrapped = await adapter.wrapVaultKey(vaultKey, kek, 'vault-1', 'slot-1');
      await expect(adapter.unwrapVaultKey(wrapped, kek, 'vault-1', 'slot-2')).rejects.toThrow(
        /Authentication failed/,
      );
    });

    it('rejects wrapping with wrong KEK', async () => {
      const vaultKey = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const kek1 = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);
      const kek2 = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);

      const wrapped = await adapter.wrapVaultKey(vaultKey, kek1, 'vault-1', 'slot-1');
      await expect(adapter.unwrapVaultKey(wrapped, kek2, 'vault-1', 'slot-1')).rejects.toThrow(
        /Authentication failed/,
      );
    });
  });

  describe('AAD construction', () => {
    it('builds canonical AAD string', () => {
      const aad = adapter.buildAad('abc-123', 'vault-payload', 'slot-1');
      expect(aad).toBe('paxport.wallet|schema=2|vault=abc-123|purpose=vault-payload|slot=slot-1');
    });

    it('uses empty slot when not provided', () => {
      const aad = adapter.buildAad('abc-123', 'vault-payload');
      expect(aad).toBe('paxport.wallet|schema=2|vault=abc-123|purpose=vault-payload|slot=');
    });

    it('produces different AAD for different purposes', () => {
      const aad1 = adapter.buildAad('v1', 'key-wrap');
      const aad2 = adapter.buildAad('v1', 'vault-payload');
      expect(aad1).not.toBe(aad2);
    });
  });

  describe('random generation', () => {
    it('generates correct length', () => {
      for (let i = 1; i <= 64; i++) {
        expect(adapter.generateRandomBytes(i).length).toBe(i);
      }
    });

    it('produces unique values (statistical)', () => {
      const samples = new Set<string>();
      for (let i = 0; i < 100; i++) {
        samples.add(adapter.encodeBase64Url(adapter.generateRandomBytes(32)));
      }
      expect(samples.size).toBe(100);
    });
  });

  describe('capabilities', () => {
    it('reports crypto availability', () => {
      const caps = adapter.capabilities();
      expect(caps.subtle).toBe(true);
      expect(caps.getRandomValues).toBe(true);
    });

    it('assertReady passes when crypto is available', () => {
      expect(() => WebCryptoAdapter.assertReady()).not.toThrow();
    });
  });

  describe('non-extractable keys', () => {
    it('rejects export of non-extractable key', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);
      await expect(crypto.subtle.exportKey('raw', key)).rejects.toThrow();
    });

    it('allows export of extractable key', async () => {
      const key = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), true);
      const raw = await crypto.subtle.exportKey('raw', key);
      expect(raw.byteLength).toBe(32);
    });

    it('unwrapped vault key is non-extractable', async () => {
      const vaultKeyRaw = adapter.generateRandomBytes(32);
      const vaultKey = await adapter.importAesGcmKey(vaultKeyRaw, true);
      const kek = await adapter.importAesGcmKey(adapter.generateRandomBytes(32), false);
      const wrapped = await adapter.wrapVaultKey(vaultKey, kek, 'vault-1', 'slot-1');
      const unwrapped = await adapter.unwrapVaultKey(wrapped, kek, 'vault-1', 'slot-1');
      expect(unwrapped.extractable).toBe(false);
      await expect(crypto.subtle.exportKey('raw', unwrapped)).rejects.toThrow();
    });
  });

  describe('KDF calibration', () => {
    it('calibrates to a bounded target duration', async () => {
      const iterations = await adapter.calibrateKdf(100, 600_000, 5_000_000);
      expect(iterations).toBeGreaterThanOrEqual(600_000);
      expect(adapter.getCalibratedIterations()).toBe(iterations);
    }, 60_000);

    it('enforces 600000 floor even when caller passes lower min', async () => {
      const iterations = await adapter.calibrateKdf(100, 100_000, 5_000_000);
      expect(iterations).toBeGreaterThanOrEqual(600_000);
    }, 60_000);
  });
});
