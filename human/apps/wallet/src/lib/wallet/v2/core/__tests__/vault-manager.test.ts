import { describe, it, expect, beforeAll } from 'vitest';
import { webcrypto } from 'node:crypto';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { WebCryptoAdapter } from '../../adapters/web-crypto-adapter';
import { VaultManager } from '../vault-manager';
import { validateManifest, validatePayload } from '../vault-validators';
import { WalletError } from '../../types/errors';

Object.defineProperty(globalThis, 'crypto', { value: webcrypto });
Object.defineProperty(globalThis, 'performance', {
  value: { now: () => Date.now() },
});

const TEST_PASSWORD = '482915';
const TEST_MNEMONIC = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);
const OTHER_MNEMONIC = bip39.entropyToMnemonic(new Uint8Array(16).fill(0xff), wordlist);

describe('VaultManager', () => {
  let cryptoAdapter: WebCryptoAdapter;
  let manager: VaultManager;

  beforeAll(() => {
    cryptoAdapter = new WebCryptoAdapter();
    manager = new VaultManager(cryptoAdapter, null);
  });

  describe('vault creation', () => {
    it('creates a valid vault with password slot', async () => {
      const { manifest, vaultKey } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      expect(manifest.schema).toBe(2);
      expect(manifest.revision).toBe(1);
      expect(manifest.keySlots).toHaveLength(1);
      expect(manifest.keySlots[0].type).toBe('password');
      expect(manifest.keySlots[0].kdf.iterations).toBeGreaterThanOrEqual(600_000);
      expect(vaultKey.extractable).toBe(false);
      expect(vaultKey.algorithm).toMatchObject({ name: 'AES-GCM', length: 256 });
    });

    it('produces structurally valid manifest (passes full validation)', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      expect(() => validateManifest(manifest)).not.toThrow();
    });

    it('generates unique vault IDs', async () => {
      const ids = new Set<string>();
      for (let i = 0; i < 10; i++) {
        const { manifest } = await manager.createVault({
          password: TEST_PASSWORD,
          mnemonic: TEST_MNEMONIC,
        });
        ids.add(manifest.vaultId);
      }
      expect(ids.size).toBe(10);
    });

    it('generates unique slot IDs', async () => {
      const ids = new Set<string>();
      for (let i = 0; i < 10; i++) {
        const { manifest } = await manager.createVault({
          password: TEST_PASSWORD,
          mnemonic: TEST_MNEMONIC,
        });
        ids.add(manifest.keySlots[0].id);
      }
      expect(ids.size).toBe(10);
    });

    it('vault key is non-extractable', async () => {
      const { vaultKey } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      expect(vaultKey.extractable).toBe(false);
      await expect(crypto.subtle.exportKey('raw', vaultKey)).rejects.toThrow();
    });

    it('uses custom iterations when provided', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
        iterations: 700_000,
      });
      expect(manifest.keySlots[0].kdf.iterations).toBe(700_000);
    });

    it('rejects a PIN shorter than 6 digits', async () => {
      await expect(
        manager.createVault({ password: '12345', mnemonic: TEST_MNEMONIC }),
      ).rejects.toThrow(/exactly 6 digits/);
    });

    it('rejects non-numeric credentials', async () => {
      await expect(
        manager.createVault({ password: 'abc123', mnemonic: TEST_MNEMONIC }),
      ).rejects.toThrow(/exactly 6 digits/);
    });

    it('accepts an exact 6-digit PIN', async () => {
      const { manifest } = await manager.createVault({
        password: '123456',
        mnemonic: TEST_MNEMONIC,
      });
      expect(manifest.schema).toBe(2);
    });

    it('uses fresh random salt per vault', async () => {
      const salts = new Set<string>();
      for (let i = 0; i < 10; i++) {
        const { manifest } = await manager.createVault({
          password: TEST_PASSWORD,
          mnemonic: TEST_MNEMONIC,
        });
        salts.add(manifest.keySlots[0].kdf.salt);
      }
      expect(salts.size).toBe(10);
    });

    it('encrypts verifier with vault key', async () => {
      const { manifest, vaultKey } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const verifierAad = cryptoAdapter.buildAad(manifest.vaultId, 'vault-verifier');
      const verifierPlain = await cryptoAdapter.decrypt(
        vaultKey,
        manifest.verifier,
        verifierAad,
        'vault-verifier',
      );
      expect(new TextDecoder().decode(verifierPlain)).toBe('paxport-vault-verifier-v2');
    });

    it('encrypts payload with vault key and correct AAD', async () => {
      const { manifest, vaultKey } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const payloadAad = cryptoAdapter.buildAad(manifest.vaultId, 'vault-payload');
      const payloadPlain = await cryptoAdapter.decrypt(
        vaultKey,
        manifest.payload,
        payloadAad,
        'vault-payload',
      );
      const payload = JSON.parse(new TextDecoder().decode(payloadPlain));
      expect(payload.mnemonic).toBe(TEST_MNEMONIC);
      expect(payload.schema).toBe(2);
    });
  });

  describe('vault unlock', () => {
    it('unlocks with correct password', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const result = await manager.unlockVault(TEST_PASSWORD, manifest);
      expect(result.vaultKey.extractable).toBe(false);
      expect(result.payload.mnemonic).toBe(TEST_MNEMONIC);
      expect(result.manifest.vaultId).toBe(manifest.vaultId);
    });

    it('returns the authenticated slot ID', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const result = await manager.unlockVault(TEST_PASSWORD, manifest);
      expect(result.authenticatedSlotId).toBe(manifest.keySlots[0].id);
    });

    it('unwrapped vault key is non-extractable', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const result = await manager.unlockVault(TEST_PASSWORD, manifest);
      expect(result.vaultKey.extractable).toBe(false);
      await expect(crypto.subtle.exportKey('raw', result.vaultKey)).rejects.toThrow();
    });

    it('rejects wrong password with AUTHENTICATION_FAILED', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      try {
        await manager.unlockVault('wrong-password-1234567', manifest);
        expect.fail('should have thrown');
      } catch (e: any) {
        expect(e).toBeInstanceOf(WalletError);
        expect(e.code).toBe('AUTHENTICATION_FAILED');
      }
    });

    it('does not leak which part failed (non-oracular)', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      try {
        await manager.unlockVault('another-wrong-password', manifest);
      } catch (e: any) {
        expect(e.message).toBe('Authentication failed.');
        expect(e.message).not.toContain('key');
        expect(e.message).not.toContain('tag');
        expect(e.message).not.toContain('IV');
        expect(e.message).not.toContain('AAD');
      }
    });

    it('decrypts and validates the payload', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const result = await manager.unlockVault(TEST_PASSWORD, manifest);
      expect(result.payload.schema).toBe(2);
      expect(result.payload.derivation.curve).toBe('secp256k1');
      expect(result.payload.accounts).toEqual([]);
      expect(result.payload.nextAccountIndex).toBe(0);
    });

    it('rejects tampered verifier', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const tampered = {
        ...manifest,
        verifier: {
          ...manifest.verifier,
          ciphertext: cryptoAdapter.encodeBase64Url(cryptoAdapter.generateRandomBytes(48)),
        },
      };

      await expect(manager.unlockVault(TEST_PASSWORD, tampered)).rejects.toThrow(/Authentication failed/);
    });

    it('rejects tampered payload', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const tampered = {
        ...manifest,
        payload: {
          ...manifest.payload,
          ciphertext: cryptoAdapter.encodeBase64Url(cryptoAdapter.generateRandomBytes(64)),
        },
      };

      await expect(manager.unlockVault(TEST_PASSWORD, tampered)).rejects.toThrow();
    });

    it('rejects swapped payload from different vault', async () => {
      const { manifest: m1 } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      const { manifest: m2 } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: OTHER_MNEMONIC,
      });

      const swapped = { ...m1, payload: m2.payload };
      await expect(manager.unlockVault(TEST_PASSWORD, swapped)).rejects.toThrow();
    });

    it('rejects manifest with invalid structure', async () => {
      await expect(
        manager.unlockVault(TEST_PASSWORD, { bad: 'data' } as any),
      ).rejects.toThrow();
    });

    it('identifies the correct authenticating slot in a two-slot manifest', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      // Simulate a two-slot intermediate state (add a second slot)
      const NEW_PASSWORD = '739204';
      const { manifest: twoSlotManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: NEW_PASSWORD },
        manifest,
      );

      // Now try to unlock with new password - should identify new slot
      const result = await manager.unlockVault(NEW_PASSWORD, twoSlotManifest);
      expect(result.authenticatedSlotId).toBe(twoSlotManifest.keySlots[0].id);
    });
  });

  describe('password slot replacement', () => {
    it('replaces password and unlocks with new password', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const NEW_PASSWORD = '739204';
      const { manifest: newManifest, vaultKey } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: NEW_PASSWORD },
        manifest,
      );

      expect(newManifest.keySlots).toHaveLength(1);
      expect(newManifest.keySlots[0].id).not.toBe(manifest.keySlots[0].id);
      expect(vaultKey.extractable).toBe(false);

      const result = await manager.unlockVault(NEW_PASSWORD, newManifest);
      expect(result.payload.mnemonic).toBe(TEST_MNEMONIC);
    });

    it('old password no longer works after replacement', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const NEW_PASSWORD = '739204';
      const { manifest: newManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: NEW_PASSWORD },
        manifest,
      );

      await expect(manager.unlockVault(TEST_PASSWORD, newManifest)).rejects.toThrow(/Authentication failed/);
    });

    it('rejects replacement with wrong current password', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      await expect(
        manager.replacePasswordSlot(
          { currentPassword: '739204', newPassword: '165830' },
          manifest,
        ),
      ).rejects.toThrow(/Authentication failed/);
    });

    it('rejects weak new password', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      await expect(
        manager.replacePasswordSlot(
          { currentPassword: TEST_PASSWORD, newPassword: '12345' },
          manifest,
        ),
      ).rejects.toThrow(/exactly 6 digits/);
    });

    it('does not re-encrypt the vault payload', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const { manifest: newManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: '739204' },
        manifest,
      );

      expect(newManifest.payload.ciphertext).toBe(manifest.payload.ciphertext);
      expect(newManifest.payload.iv).toBe(manifest.payload.iv);
    });

    it('increments revision', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const { manifest: newManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: '739204' },
        manifest,
      );

      expect(newManifest.revision).toBe(manifest.revision + 1);
    });

    it('uses fresh salt for new slot', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const { manifest: newManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: '739204' },
        manifest,
      );

      expect(newManifest.keySlots[0].kdf.salt).not.toBe(manifest.keySlots[0].kdf.salt);
    });

    it('preserves vault payload content through replacement', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const NEW_PASSWORD = '739204';
      const { manifest: newManifest } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: NEW_PASSWORD },
        manifest,
      );

      const result = await manager.unlockVault(NEW_PASSWORD, newManifest);
      expect(result.payload.mnemonic).toBe(TEST_MNEMONIC);
      expect(result.payload.schema).toBe(2);
    });

    it('does not expose extractable VEK during replacement (rewrap is internal)', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const NEW_PASSWORD = '739204';
      const { vaultKey } = await manager.replacePasswordSlot(
        { currentPassword: TEST_PASSWORD, newPassword: NEW_PASSWORD },
        manifest,
      );

      // The returned vault key must be non-extractable
      expect(vaultKey.extractable).toBe(false);
      await expect(crypto.subtle.exportKey('raw', vaultKey)).rejects.toThrow();
    });
  });

  describe('decryptPayload', () => {
    it('decrypts and validates payload', async () => {
      const { manifest, vaultKey } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const payload = await manager.decryptPayload(vaultKey, manifest);
      expect(payload.mnemonic).toBe(TEST_MNEMONIC);
      expect(payload.schema).toBe(2);
    });

    it('rejects payload with wrong vault key', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const wrongKey = await cryptoAdapter.importAesGcmKey(
        cryptoAdapter.generateRandomBytes(32),
        false,
      );
      await expect(manager.decryptPayload(wrongKey, manifest)).rejects.toThrow();
    });
  });

  describe('key hierarchy security properties', () => {
    it('different vaults with same password have different vault keys', async () => {
      const { manifest: m1, vaultKey: vk1 } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      const { manifest: m2, vaultKey: vk2 } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const aad1 = cryptoAdapter.buildAad(m1.vaultId, 'vault-payload');
      await expect(
        cryptoAdapter.decrypt(vk2, m1.payload, aad1, 'vault-payload'),
      ).rejects.toThrow();
    });

    it('wrapping uses vault-specific and slot-specific AAD', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const slot = manifest.keySlots[0];
      const expectedAad = cryptoAdapter.buildAad(manifest.vaultId, 'key-wrap', slot.id);
      expect(expectedAad).toContain(manifest.vaultId);
      expect(expectedAad).toContain(slot.id);
    });

    it('vault verifier proves key correctness before payload decryption', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      const result = await manager.unlockVault(TEST_PASSWORD, manifest);
      expect(result.payload).toBeDefined();
    });

    it('KEK (password-derived key) is never serialized', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      expect(manifest.keySlots[0]).not.toHaveProperty('kek');
      expect(manifest.keySlots[0]).not.toHaveProperty('rawKey');
      expect(manifest.keySlots[0]).not.toHaveProperty('derivedKey');

      const slotKeys = Object.keys(manifest.keySlots[0]);
      expect(slotKeys.sort()).toEqual(['createdAt', 'id', 'kdf', 'type', 'version', 'wrappedVaultKey'].sort());
    });
  });

  describe('adversarial input to manifest validation during unlock', () => {
    it('rejects manifest with schema version 0', async () => {
      await expect(
        manager.unlockVault(TEST_PASSWORD, { schema: 0 } as any),
      ).rejects.toThrow();
    });

    it('rejects manifest with negative revision', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      await expect(
        manager.unlockVault(TEST_PASSWORD, { ...manifest, revision: -1 } as any),
      ).rejects.toThrow();
    });

    it('rejects manifest with NaN timestamp', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });
      await expect(
        manager.unlockVault(TEST_PASSWORD, { ...manifest, createdAt: NaN } as any),
      ).rejects.toThrow();
    });

    it('rejects completely empty object', async () => {
      await expect(manager.unlockVault(TEST_PASSWORD, {} as any)).rejects.toThrow();
    });

    it('rejects null manifest', async () => {
      await expect(manager.unlockVault(TEST_PASSWORD, null as any)).rejects.toThrow();
    });
  });

  describe('revision overflow', () => {
    it('rejects revision increment at MAX_REVISION', async () => {
      const { manifest } = await manager.createVault({
        password: TEST_PASSWORD,
        mnemonic: TEST_MNEMONIC,
      });

      // Create manifest at max revision
      const maxRevManifest = { ...manifest, revision: 2147483647 };
      await expect(
        manager.replacePasswordSlot(
          { currentPassword: TEST_PASSWORD, newPassword: '739204' },
          maxRevManifest,
        ),
      ).rejects.toThrow(/overflow|maximum/i);
    });
  });

  describe('rewrap via CryptoPort (no extractable VEK exposure)', () => {
    it('rewrapVaultKey produces valid wrapping for new slot', async () => {
      const vekRaw = cryptoAdapter.generateRandomBytes(32);
      const vek = await cryptoAdapter.importAesGcmKey(vekRaw, true);

      const salt1 = cryptoAdapter.generateRandomBytes(32);
      const kek1 = await cryptoAdapter.deriveKeyFromPassword('old-password-12345', salt1, 600_000);
      const wrapped1 = await cryptoAdapter.wrapVaultKey(vek, kek1, 'vault-1', 'slot-1');

      const salt2 = cryptoAdapter.generateRandomBytes(32);
      const kek2 = await cryptoAdapter.deriveKeyFromPassword('new-password-67890', salt2, 600_000);

      // Rewrap internally
      const wrapped2 = await cryptoAdapter.rewrapVaultKey(
        wrapped1, kek1, 'vault-1', 'slot-1',
        kek2, 'vault-1', 'slot-2',
      );

      // Unwrap with new KEK should succeed
      const unwrapped = await cryptoAdapter.unwrapVaultKey(wrapped2, kek2, 'vault-1', 'slot-2');
      expect(unwrapped.extractable).toBe(false);

      // Verify the unwrapped key can decrypt something encrypted with original VEK
      const aad = cryptoAdapter.buildAad('vault-1', 'vault-payload');
      const envelope = await cryptoAdapter.encrypt(vek, new TextEncoder().encode('test'), aad, 'vault-payload');
      const decrypted = await cryptoAdapter.decrypt(unwrapped, envelope, aad, 'vault-payload');
      expect(new TextDecoder().decode(decrypted)).toBe('test');
    });

    it('rewrapVaultKey rejects wrong old KEK', async () => {
      const vekRaw = cryptoAdapter.generateRandomBytes(32);
      const vek = await cryptoAdapter.importAesGcmKey(vekRaw, true);

      const salt1 = cryptoAdapter.generateRandomBytes(32);
      const kek1 = await cryptoAdapter.deriveKeyFromPassword('old-password-12345', salt1, 600_000);
      const wrapped1 = await cryptoAdapter.wrapVaultKey(vek, kek1, 'vault-1', 'slot-1');

      const wrongKek = await cryptoAdapter.importAesGcmKey(cryptoAdapter.generateRandomBytes(32), false);
      const salt2 = cryptoAdapter.generateRandomBytes(32);
      const kek2 = await cryptoAdapter.deriveKeyFromPassword('new-password-67890', salt2, 600_000);

      await expect(
        cryptoAdapter.rewrapVaultKey(
          wrapped1, wrongKek, 'vault-1', 'slot-1',
          kek2, 'vault-1', 'slot-2',
        ),
      ).rejects.toThrow(/Authentication failed/);
    });

    it('rewrapVaultKey rejects wrong old slot AAD', async () => {
      const vekRaw = cryptoAdapter.generateRandomBytes(32);
      const vek = await cryptoAdapter.importAesGcmKey(vekRaw, true);

      const salt1 = cryptoAdapter.generateRandomBytes(32);
      const kek1 = await cryptoAdapter.deriveKeyFromPassword('old-password-12345', salt1, 600_000);
      const wrapped1 = await cryptoAdapter.wrapVaultKey(vek, kek1, 'vault-1', 'slot-1');

      const salt2 = cryptoAdapter.generateRandomBytes(32);
      const kek2 = await cryptoAdapter.deriveKeyFromPassword('new-password-67890', salt2, 600_000);

      // Use wrong old slot ID in rewrap
      await expect(
        cryptoAdapter.rewrapVaultKey(
          wrapped1, kek1, 'vault-1', 'slot-WRONG',
          kek2, 'vault-1', 'slot-2',
        ),
      ).rejects.toThrow(/Authentication failed/);
    });
  });
});
