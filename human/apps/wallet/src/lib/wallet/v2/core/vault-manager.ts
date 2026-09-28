import type { CryptoPort } from '../ports/crypto-port';
import type { StoragePort } from '../ports/storage-port';
import type { AeadEnvelopeV1, PasswordKeySlotV1 } from '../types/crypto';
import type { VaultManifestV2, WalletPayloadV2 } from '../types/vault';
import type { StorageRecord } from '../types/storage';
import { WalletError } from '../types/errors';
import { validateManifest, validatePayload } from './vault-validators';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';

const VAULT_NAMESPACE = 'paxport-wallet-v2';
const STAGED_VAULT_NAMESPACE = 'paxport-wallet-v2-staged';
const VERIFIER_PLAINTEXT = new TextEncoder().encode('paxport-vault-verifier-v2');
const DEFAULT_ITERATIONS = 600_000;
const KDF_TARGET_MS = 250;
const MAX_REVISION = 2147483647;

export interface VaultCreateParams {
  password: string;
  mnemonic: string;
  iterations?: number;
}

export interface VaultUnlockResult {
  vaultKey: CryptoKey;
  manifest: VaultManifestV2;
  payload: WalletPayloadV2;
  authenticatedSlotId: string;
}

export interface VaultPasswordReplaceParams {
  currentPassword: string;
  newPassword: string;
  newIterations?: number;
}

export class VaultManager {
  constructor(
    private readonly crypto: CryptoPort,
    private readonly storage: StoragePort | null,
  ) {}

  async createVault(params: VaultCreateParams): Promise<{
    manifest: VaultManifestV2;
    vaultKey: CryptoKey;
    record: StorageRecord | null;
  }> {
    this.validatePassword(params.password);
    const mnemonic = params.mnemonic.trim().toLowerCase().replace(/\s+/g, ' ');
    if (!bip39.validateMnemonic(mnemonic, wordlist)) {
      throw WalletError.invalidInput('mnemonic', 'must be a valid BIP39 phrase');
    }

    const vaultId = this.generateUUID();
    const now = Date.now();
    const iterations = params.iterations ?? (
      this.storage
        ? await this.crypto.calibrateKdf(KDF_TARGET_MS, DEFAULT_ITERATIONS)
        : DEFAULT_ITERATIONS
    );

    const vekRaw = this.crypto.generateRandomBytes(32);
    let vekNonExtractable: CryptoKey;
    let salt: Uint8Array;
    let slotId: string;
    let wrappedVaultKey: AeadEnvelopeV1;
    try {
      const vekExtractable = await this.crypto.importAesGcmKey(vekRaw, true);
      salt = this.crypto.generateRandomBytes(32);
      const kek = await this.crypto.deriveKeyFromPassword(
        params.password,
        salt,
        iterations,
      );
      slotId = this.generateUUID();
      wrappedVaultKey = await this.crypto.wrapVaultKey(
        vekExtractable,
        kek,
        vaultId,
        slotId,
      );
      vekNonExtractable = await this.crypto.importAesGcmKey(vekRaw, false);
    } finally {
      vekRaw.fill(0);
    }

    // Encrypt verifier
    const verifierAad = this.crypto.buildAad(vaultId, 'vault-verifier');
    const verifier = await this.crypto.encrypt(vekNonExtractable, VERIFIER_PLAINTEXT, verifierAad, 'vault-verifier');

    // Build and encrypt the payload
    const payload = this.buildInitialPayload(mnemonic, now);
    const payloadBytes = new TextEncoder().encode(JSON.stringify(payload));
    const payloadAad = this.crypto.buildAad(vaultId, 'vault-payload');
    const encryptedPayload = await this.crypto.encrypt(vekNonExtractable, payloadBytes, payloadAad, 'vault-payload');

    const keySlot: PasswordKeySlotV1 = {
      version: 1,
      id: slotId,
      type: 'password',
      kdf: {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: this.crypto.encodeBase64Url(salt),
        iterations,
      },
      wrappedVaultKey,
      createdAt: now,
    };

    const manifest: VaultManifestV2 = {
      schema: 2,
      vaultId,
      revision: 1,
      createdAt: now,
      updatedAt: now,
      keySlots: [keySlot],
      verifier,
      payload: encryptedPayload,
    };

    validateManifest(manifest);

    let record: StorageRecord | null = null;
    if (this.storage) {
      const writeResult = await this.storage.write(
        VAULT_NAMESPACE,
        { vaultId, revision: 1, manifest },
        null,
      );
      if (!writeResult.ok) {
        throw writeResult.error === 'WRITE_CONFLICT'
          ? WalletError.writeConflict()
          : WalletError.storageUnavailable();
      }
      record = writeResult.record;

      await this.authenticateReadBack(vekNonExtractable, vaultId);
    }

    return { manifest, vaultKey: vekNonExtractable, record };
  }

  async unlockVault(password: string, manifestInput?: VaultManifestV2): Promise<VaultUnlockResult> {
    let manifest: VaultManifestV2;

    if (manifestInput) {
      manifest = validateManifest(manifestInput);
    } else if (this.storage) {
      const stored = await this.storage.read(VAULT_NAMESPACE);
      if (!stored) {
        throw WalletError.corruptVault('No vault found in storage');
      }
      manifest = validateManifest(stored.manifest);
    } else {
      throw WalletError.storageUnavailable();
    }

    // Try each password key slot, tracking which one authenticated
    let vaultKey: CryptoKey | null = null;
    let authenticatedSlotId: string | null = null;
    for (const slot of manifest.keySlots) {
      if (slot.type !== 'password') continue;
      try {
        const salt = this.crypto.decodeBase64Url(slot.kdf.salt);
        const kek = await this.crypto.deriveKeyFromPassword(password, salt, slot.kdf.iterations);
        vaultKey = await this.crypto.unwrapVaultKey(
          slot.wrappedVaultKey,
          kek,
          manifest.vaultId,
          slot.id,
        );
        authenticatedSlotId = slot.id;
        break;
      } catch {
        continue;
      }
    }

    if (!vaultKey || !authenticatedSlotId) {
      throw WalletError.authenticationFailed();
    }

    await this.verifyVaultVerifier(vaultKey, manifest);

    const payload = await this.decryptPayload(vaultKey, manifest);

    return { vaultKey, manifest, payload, authenticatedSlotId };
  }

  async replacePasswordSlot(
    params: VaultPasswordReplaceParams,
    manifestInput?: VaultManifestV2,
  ): Promise<{
    manifest: VaultManifestV2;
    vaultKey: CryptoKey;
    record: StorageRecord | null;
  }> {
    this.validatePassword(params.newPassword);

    // Unlock with current password (validates everything and tracks authenticated slot)
    const { vaultKey, manifest: currentManifest, authenticatedSlotId } = await this.unlockVault(
      params.currentPassword,
      manifestInput,
    );

    // Find the exact slot that authenticated
    const authenticatedSlot = currentManifest.keySlots.find(s => s.id === authenticatedSlotId);
    if (!authenticatedSlot) {
      throw WalletError.corruptVault('Authenticated slot not found in manifest');
    }

    const now = Date.now();
    const iterations = params.newIterations ?? DEFAULT_ITERATIONS;

    // Derive KEK from old password using the authenticating slot's params
    const oldSalt = this.crypto.decodeBase64Url(authenticatedSlot.kdf.salt);
    const oldKek = await this.crypto.deriveKeyFromPassword(
      params.currentPassword,
      oldSalt,
      authenticatedSlot.kdf.iterations,
    );

    // Derive new KEK from new password
    const newSalt = this.crypto.generateRandomBytes(32);
    const newKek = await this.crypto.deriveKeyFromPassword(params.newPassword, newSalt, iterations);

    // Create new slot via rewrapVaultKey (no extractable VEK exposed)
    const newSlotId = this.generateUUID();
    const newWrappedVaultKey = await this.crypto.rewrapVaultKey(
      authenticatedSlot.wrappedVaultKey,
      oldKek,
      currentManifest.vaultId,
      authenticatedSlot.id,
      newKek,
      currentManifest.vaultId,
      newSlotId,
    );

    const newSlot: PasswordKeySlotV1 = {
      version: 1,
      id: newSlotId,
      type: 'password',
      kdf: {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: this.crypto.encodeBase64Url(newSalt),
        iterations,
      },
      wrappedVaultKey: newWrappedVaultKey,
      createdAt: now,
    };

    // Check revision overflow before incrementing
    const newRevision = this.safeIncrementRevision(currentManifest.revision);

    // Build manifest with both slots (intermediate two-slot state for safety)
    const manifestWithBothSlots: VaultManifestV2 = {
      ...currentManifest,
      revision: newRevision,
      updatedAt: now,
      keySlots: [newSlot, ...currentManifest.keySlots],
    };

    if (this.storage) {
      // Write with both slots
      const writeResult = await this.storage.write(
        VAULT_NAMESPACE,
        { vaultId: currentManifest.vaultId, revision: newRevision, manifest: manifestWithBothSlots },
        currentManifest.revision,
      );
      if (!writeResult.ok) {
        throw writeResult.error === 'WRITE_CONFLICT'
          ? WalletError.writeConflict()
          : WalletError.storageUnavailable();
      }

      // Read back and authenticate with new slot before removing old
      const readBack = await this.storage.read(VAULT_NAMESPACE);
      if (!readBack) {
        throw WalletError.corruptVault('Read-back after password replacement failed');
      }
      const readBackManifest = validateManifest(readBack.manifest);

      // Verify new slot works
      const readBackSalt = this.crypto.decodeBase64Url(newSlot.kdf.salt);
      const readBackKek = await this.crypto.deriveKeyFromPassword(params.newPassword, readBackSalt, iterations);
      const readBackVek = await this.crypto.unwrapVaultKey(
        newSlot.wrappedVaultKey,
        readBackKek,
        readBackManifest.vaultId,
        newSlot.id,
      );
      await this.verifyVaultVerifier(readBackVek, readBackManifest);

      // Now commit with only the new slot (remove old)
      const finalRevision = this.safeIncrementRevision(newRevision);
      const finalManifest: VaultManifestV2 = {
        ...readBackManifest,
        revision: finalRevision,
        updatedAt: now,
        keySlots: [newSlot],
      };

      const finalWrite = await this.storage.write(
        VAULT_NAMESPACE,
        { vaultId: currentManifest.vaultId, revision: finalRevision, manifest: finalManifest },
        newRevision,
      );
      if (!finalWrite.ok) {
        // Old slot is still present in the two-slot state; user can still unlock
        throw finalWrite.error === 'WRITE_CONFLICT'
          ? WalletError.writeConflict()
          : WalletError.storageUnavailable();
      }

      // Final read-back and authenticate
      await this.authenticateReadBack(vaultKey, currentManifest.vaultId);

      return { manifest: finalManifest, vaultKey, record: finalWrite.record };
    }

    // No storage: just return the final manifest with new slot only
    const finalManifest: VaultManifestV2 = {
      ...currentManifest,
      revision: newRevision,
      updatedAt: now,
      keySlots: [newSlot],
    };

    validateManifest(finalManifest);
    return { manifest: finalManifest, vaultKey, record: null };
  }

  async decryptPayload(vaultKey: CryptoKey, manifest: VaultManifestV2): Promise<WalletPayloadV2> {
    const payloadAad = this.crypto.buildAad(manifest.vaultId, 'vault-payload');
    let plaintext: Uint8Array;
    try {
      plaintext = await this.crypto.decrypt(vaultKey, manifest.payload, payloadAad, 'vault-payload');
    } catch (e) {
      if (e instanceof WalletError) throw e;
      throw WalletError.corruptVault('Payload decryption failed');
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(new TextDecoder().decode(plaintext));
    } catch {
      throw WalletError.corruptVault('Payload is not valid JSON');
    }

    return validatePayload(parsed);
  }

  async commitPayload(
    vaultKey: CryptoKey,
    currentManifest: VaultManifestV2,
    payloadInput: WalletPayloadV2,
  ): Promise<{ manifest: VaultManifestV2; record: StorageRecord | null }> {
    const payload = validatePayload(payloadInput);
    const revision = this.safeIncrementRevision(currentManifest.revision);
    const payloadAad = this.crypto.buildAad(currentManifest.vaultId, 'vault-payload');
    const encryptedPayload = await this.crypto.encrypt(
      vaultKey,
      new TextEncoder().encode(JSON.stringify(payload)),
      payloadAad,
      'vault-payload',
    );
    const manifest: VaultManifestV2 = validateManifest({
      ...currentManifest,
      revision,
      updatedAt: Date.now(),
      payload: encryptedPayload,
    });

    if (!this.storage) return { manifest, record: null };

    const existingStage = await this.storage.read(STAGED_VAULT_NAMESPACE);
    const stagedWrite = await this.storage.write(
      STAGED_VAULT_NAMESPACE,
      { vaultId: manifest.vaultId, revision, manifest },
      existingStage?.revision ?? null,
    );
    if (!stagedWrite.ok) {
      throw stagedWrite.error === 'WRITE_CONFLICT'
        ? WalletError.writeConflict()
        : WalletError.storageUnavailable();
    }

    try {
      const staged = await this.storage.read(STAGED_VAULT_NAMESPACE);
      if (!staged) throw WalletError.corruptVault('Staged vault read-back failed');
      await this.verifyVaultVerifier(vaultKey, staged.manifest);
      await this.decryptPayload(vaultKey, staged.manifest);

      const committed = await this.storage.write(
        VAULT_NAMESPACE,
        { vaultId: manifest.vaultId, revision, manifest },
        currentManifest.revision,
      );
      if (!committed.ok) {
        throw committed.error === 'WRITE_CONFLICT'
          ? WalletError.writeConflict()
          : WalletError.storageUnavailable();
      }

      const readBack = await this.storage.read(VAULT_NAMESPACE);
      if (!readBack || readBack.revision !== revision) {
        throw WalletError.corruptVault('Committed vault read-back failed');
      }
      await this.verifyVaultVerifier(vaultKey, readBack.manifest);
      await this.decryptPayload(vaultKey, readBack.manifest);
      return { manifest: readBack.manifest, record: readBack };
    } finally {
      const staged = await this.storage.read(STAGED_VAULT_NAMESPACE);
      if (staged) {
        await this.storage.delete(STAGED_VAULT_NAMESPACE, staged.revision);
      }
    }
  }

  private async verifyVaultVerifier(vaultKey: CryptoKey, manifest: VaultManifestV2): Promise<void> {
    const verifierAad = this.crypto.buildAad(manifest.vaultId, 'vault-verifier');
    let plaintext: Uint8Array;
    try {
      plaintext = await this.crypto.decrypt(vaultKey, manifest.verifier, verifierAad, 'vault-verifier');
    } catch {
      throw WalletError.authenticationFailed();
    }

    if (plaintext.length !== VERIFIER_PLAINTEXT.length) {
      throw WalletError.corruptVault('Verifier content mismatch');
    }
    for (let i = 0; i < plaintext.length; i++) {
      if (plaintext[i] !== VERIFIER_PLAINTEXT[i]) {
        throw WalletError.corruptVault('Verifier content mismatch');
      }
    }
  }

  private async authenticateReadBack(vaultKey: CryptoKey, vaultId: string): Promise<void> {
    if (!this.storage) return;

    const stored = await this.storage.read(VAULT_NAMESPACE);
    if (!stored) {
      throw WalletError.corruptVault('Read-back failed: no record found');
    }
    const readManifest = validateManifest(stored.manifest);
    if (readManifest.vaultId !== vaultId) {
      throw WalletError.corruptVault('Read-back vaultId mismatch');
    }
    await this.verifyVaultVerifier(vaultKey, readManifest);
  }

  private safeIncrementRevision(revision: number): number {
    if (revision >= MAX_REVISION) {
      throw WalletError.corruptVault('Revision overflow: maximum revision reached');
    }
    return revision + 1;
  }

  private buildInitialPayload(mnemonic: string, createdAt: number): WalletPayloadV2 {
    return {
      schema: 2,
      mnemonic,
      derivation: {
        curve: 'secp256k1',
        standard: 'bip44',
        basePath: "m/44'/60'/0'/0",
      },
      accounts: [],
      activeAccountId: null,
      nextAccountIndex: 0,
      createdAt,
    };
  }

  private validatePassword(password: string): void {
    if (typeof password !== 'string') {
      throw WalletError.invalidInput('pin', 'must be a string');
    }
    if (!/^\d{6}$/.test(password)) {
      throw WalletError.invalidInput('pin', 'must be exactly 6 digits');
    }
  }

  private generateUUID(): string {
    const bytes = this.crypto.generateRandomBytes(16);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
  }
}
