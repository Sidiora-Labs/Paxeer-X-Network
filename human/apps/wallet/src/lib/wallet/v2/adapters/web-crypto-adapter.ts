import type { CryptoPort } from '../ports/crypto-port';
import type { CryptoCapabilities, AeadEnvelopeV1, AeadPurpose, Base64Url } from '../types/crypto';
import { WalletError } from '../types/errors';

const AES_GCM_IV_LENGTH = 12;
const AES_GCM_KEY_LENGTH = 32;
const MIN_PBKDF2_ITERATIONS = 600_000;
const PBKDF2_HASH = 'SHA-256';
const SCHEMA_VERSION = 2;
const AAD_PREFIX = 'paxport.wallet';
const MAX_RANDOM_BYTES = 65_536;
const MAX_ENVELOPE_BYTES = 2 * 1024 * 1024;

// Canonical unpadded base64url: only [A-Za-z0-9_-], no padding, no whitespace, no standard base64 chars
const CANONICAL_BASE64URL_RE = /^[A-Za-z0-9_-]*$/;

function getSubtle(): SubtleCrypto {
  if (typeof globalThis.crypto !== 'undefined' && globalThis.crypto.subtle) {
    return globalThis.crypto.subtle;
  }
  throw WalletError.storageUnavailable(new Error('SubtleCrypto is not available'));
}

function getGlobalCrypto(): Crypto {
  if (
    typeof globalThis.crypto !== 'undefined'
    && typeof globalThis.crypto.getRandomValues === 'function'
  ) {
    return globalThis.crypto;
  }
  throw WalletError.storageUnavailable(new Error('Web Crypto is not available'));
}

function copyToArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
}

export class WebCryptoAdapter implements CryptoPort {
  private calibratedIterations: number | null = null;

  static assertReady(): void {
    const caps: CryptoCapabilities = {
      subtle: typeof globalThis.crypto !== 'undefined' && !!globalThis.crypto.subtle,
      getRandomValues:
        typeof globalThis.crypto !== 'undefined'
        && typeof globalThis.crypto.getRandomValues === 'function',
      secureContext: globalThis.isSecureContext !== false,
    };
    if (!caps.subtle || !caps.getRandomValues || !caps.secureContext) {
      throw WalletError.storageUnavailable(
        new Error('Web Crypto is not available. A secure context (HTTPS) is required.'),
      );
    }
  }

  capabilities(): CryptoCapabilities {
    return {
      subtle: typeof globalThis.crypto !== 'undefined' && !!globalThis.crypto.subtle,
      getRandomValues:
        typeof globalThis.crypto !== 'undefined'
        && typeof globalThis.crypto.getRandomValues === 'function',
      secureContext: globalThis.isSecureContext !== false,
    };
  }

  generateRandomBytes(length: number): Uint8Array {
    if (!Number.isInteger(length) || length < 1 || length > MAX_RANDOM_BYTES) {
      throw WalletError.invalidInput(
        'random byte length',
        `must be an integer between 1 and ${MAX_RANDOM_BYTES}`,
      );
    }
    const buf = new Uint8Array(length);
    getGlobalCrypto().getRandomValues(buf);
    return buf;
  }

  async importAesGcmKey(raw: Uint8Array, extractable: boolean): Promise<CryptoKey> {
    if (raw.length !== AES_GCM_KEY_LENGTH) {
      throw WalletError.invalidInput('key', `expected ${AES_GCM_KEY_LENGTH} bytes, got ${raw.length}`);
    }
    const rawBuffer = copyToArrayBuffer(raw);
    try {
      return await getSubtle().importKey(
        'raw',
        rawBuffer,
        { name: 'AES-GCM' },
        extractable,
        ['encrypt', 'decrypt', 'wrapKey', 'unwrapKey'],
      );
    } finally {
      new Uint8Array(rawBuffer).fill(0);
    }
  }

  async encrypt(
    key: CryptoKey,
    plaintext: Uint8Array,
    aad: string,
    _purpose: AeadPurpose,
  ): Promise<AeadEnvelopeV1> {
    this.assertPurposeBoundAad(aad, _purpose);
    if (plaintext.byteLength > MAX_ENVELOPE_BYTES) {
      throw WalletError.invalidInput('plaintext', `exceeds ${MAX_ENVELOPE_BYTES} bytes`);
    }
    const iv = this.generateRandomBytes(AES_GCM_IV_LENGTH);
    const aadBytes = new TextEncoder().encode(aad);
    const plaintextBuffer = copyToArrayBuffer(plaintext);

    let ciphertext: Uint8Array;
    try {
      ciphertext = new Uint8Array(
        await getSubtle().encrypt(
          {
            name: 'AES-GCM',
            iv: copyToArrayBuffer(iv),
            additionalData: copyToArrayBuffer(aadBytes),
            tagLength: 128,
          },
          key,
          plaintextBuffer,
        ),
      );
    } finally {
      new Uint8Array(plaintextBuffer).fill(0);
    }

    return {
      version: 1,
      algorithm: 'AES-256-GCM',
      iv: this.encodeBase64Url(iv),
      ciphertext: this.encodeBase64Url(ciphertext),
    };
  }

  async decrypt(
    key: CryptoKey,
    envelope: AeadEnvelopeV1,
    aad: string,
    _purpose: AeadPurpose,
  ): Promise<Uint8Array> {
    if (!this.validateEnvelope(envelope)) {
      throw WalletError.corruptVault('Invalid envelope structure');
    }
    this.assertPurposeBoundAad(aad, _purpose);

    const iv = this.decodeBase64Url(envelope.iv);
    const ciphertext = this.decodeBase64Url(envelope.ciphertext);
    const aadBytes = new TextEncoder().encode(aad);

    try {
      const plaintext = await getSubtle().decrypt(
        {
          name: 'AES-GCM',
          iv: copyToArrayBuffer(iv),
          additionalData: copyToArrayBuffer(aadBytes),
          tagLength: 128,
        },
        key,
        copyToArrayBuffer(ciphertext),
      );
      return new Uint8Array(plaintext);
    } catch {
      throw WalletError.authenticationFailed();
    }
  }

  async deriveKeyFromPassword(
    password: string,
    salt: Uint8Array,
    iterations: number,
  ): Promise<CryptoKey> {
    if (iterations < MIN_PBKDF2_ITERATIONS) {
      throw WalletError.invalidInput('iterations', `minimum ${MIN_PBKDF2_ITERATIONS}, got ${iterations}`);
    }
    if (salt.length < 16) {
      throw WalletError.invalidInput('salt', 'minimum 16 bytes');
    }

    const encoder = new TextEncoder();
    const passwordBytes = encoder.encode(password);
    const passwordBuffer = copyToArrayBuffer(passwordBytes);
    let passwordKey: CryptoKey;
    try {
      passwordKey = await getSubtle().importKey(
        'raw',
        passwordBuffer,
        'PBKDF2',
        false,
        ['deriveKey'],
      );
    } finally {
      passwordBytes.fill(0);
      new Uint8Array(passwordBuffer).fill(0);
    }

    return getSubtle().deriveKey(
      {
        name: 'PBKDF2',
        salt: copyToArrayBuffer(salt),
        iterations,
        hash: PBKDF2_HASH,
      },
      passwordKey,
      { name: 'AES-GCM', length: 256 },
      false,
      ['wrapKey', 'unwrapKey'],
    );
  }

  async wrapVaultKey(
    vaultKey: CryptoKey,
    kek: CryptoKey,
    vaultId: string,
    slotId: string,
  ): Promise<AeadEnvelopeV1> {
    const iv = this.generateRandomBytes(AES_GCM_IV_LENGTH);
    const aad = this.buildAad(vaultId, 'key-wrap', slotId);
    const aadBytes = new TextEncoder().encode(aad);

    const wrapped = new Uint8Array(
      await getSubtle().wrapKey(
        'raw',
        vaultKey,
        kek,
        {
          name: 'AES-GCM',
          iv: copyToArrayBuffer(iv),
          additionalData: copyToArrayBuffer(aadBytes),
          tagLength: 128,
        },
      ),
    );

    return {
      version: 1,
      algorithm: 'AES-256-GCM',
      iv: this.encodeBase64Url(iv),
      ciphertext: this.encodeBase64Url(wrapped),
    };
  }

  async unwrapVaultKey(
    envelope: AeadEnvelopeV1,
    kek: CryptoKey,
    vaultId: string,
    slotId: string,
  ): Promise<CryptoKey> {
    if (!this.validateEnvelope(envelope)) {
      throw WalletError.corruptVault('Invalid key-wrap envelope');
    }

    const iv = this.decodeBase64Url(envelope.iv);
    const wrapped = this.decodeBase64Url(envelope.ciphertext);
    const aad = this.buildAad(vaultId, 'key-wrap', slotId);
    const aadBytes = new TextEncoder().encode(aad);

    try {
      return await getSubtle().unwrapKey(
        'raw',
        copyToArrayBuffer(wrapped),
        kek,
        {
          name: 'AES-GCM',
          iv: copyToArrayBuffer(iv),
          additionalData: copyToArrayBuffer(aadBytes),
          tagLength: 128,
        },
        { name: 'AES-GCM', length: 256 },
        false,
        ['encrypt', 'decrypt'],
      );
    } catch {
      throw WalletError.authenticationFailed();
    }
  }

  async rewrapVaultKey(
    oldEnvelope: AeadEnvelopeV1,
    oldKek: CryptoKey,
    oldVaultId: string,
    oldSlotId: string,
    newKek: CryptoKey,
    newVaultId: string,
    newSlotId: string,
  ): Promise<AeadEnvelopeV1> {
    if (!this.validateEnvelope(oldEnvelope)) {
      throw WalletError.corruptVault('Invalid key-wrap envelope for rewrap');
    }

    // Unwrap via SubtleCrypto.unwrapKey (KEK has wrapKey/unwrapKey usages)
    const oldIv = this.decodeBase64Url(oldEnvelope.iv);
    const oldWrapped = this.decodeBase64Url(oldEnvelope.ciphertext);
    const oldAad = this.buildAad(oldVaultId, 'key-wrap', oldSlotId);
    const oldAadBytes = new TextEncoder().encode(oldAad);

    let rawVekBytes: Uint8Array;
    let tempKey: CryptoKey;
    try {
      tempKey = await getSubtle().unwrapKey(
        'raw',
        copyToArrayBuffer(oldWrapped),
        oldKek,
        {
          name: 'AES-GCM',
          iv: copyToArrayBuffer(oldIv),
          additionalData: copyToArrayBuffer(oldAadBytes),
          tagLength: 128,
        },
        { name: 'AES-GCM', length: 256 },
        true,
        ['encrypt', 'decrypt'],
      );
      const exported = await getSubtle().exportKey('raw', tempKey);
      rawVekBytes = new Uint8Array(exported);
    } catch {
      throw WalletError.authenticationFailed();
    }

    // Validate VEK length
    if (rawVekBytes.length !== AES_GCM_KEY_LENGTH) {
      rawVekBytes.fill(0);
      throw WalletError.corruptVault('Unwrapped VEK has invalid length');
    }

    // Import raw bytes as extractable CryptoKey, then wrapKey under new KEK
    const newIv = this.generateRandomBytes(AES_GCM_IV_LENGTH);
    const newAad = this.buildAad(newVaultId, 'key-wrap', newSlotId);
    const newAadBytes = new TextEncoder().encode(newAad);

    let newWrapped: Uint8Array;
    try {
      const tempVek = await getSubtle().importKey(
        'raw',
        copyToArrayBuffer(rawVekBytes),
        { name: 'AES-GCM' },
        true,
        ['encrypt', 'decrypt'],
      );
      const wrapped = await getSubtle().wrapKey(
        'raw',
        tempVek,
        newKek,
        {
          name: 'AES-GCM',
          iv: copyToArrayBuffer(newIv),
          additionalData: copyToArrayBuffer(newAadBytes),
          tagLength: 128,
        },
      );
      newWrapped = new Uint8Array(wrapped);
    } finally {
      // Zero the raw key material
      rawVekBytes.fill(0);
    }

    return {
      version: 1,
      algorithm: 'AES-256-GCM',
      iv: this.encodeBase64Url(newIv),
      ciphertext: this.encodeBase64Url(newWrapped),
    };
  }

  buildAad(vaultId: string, purpose: AeadPurpose, slotId?: string): string {
    if (!vaultId || vaultId.includes('|')) {
      throw WalletError.invalidInput('vaultId', 'must be non-empty and must not contain separators');
    }
    if (slotId?.includes('|')) {
      throw WalletError.invalidInput('slotId', 'must not contain separators');
    }
    return `${AAD_PREFIX}|schema=${SCHEMA_VERSION}|vault=${vaultId}|purpose=${purpose}|slot=${slotId ?? ''}`;
  }

  validateEnvelope(envelope: AeadEnvelopeV1): boolean {
    if (!envelope || typeof envelope !== 'object') return false;
    if (Object.keys(envelope).some(
      key => !['version', 'algorithm', 'iv', 'ciphertext'].includes(key),
    )) return false;
    if (envelope.version !== 1) return false;
    if (envelope.algorithm !== 'AES-256-GCM') return false;

    try {
      // Enforce canonical unpadded base64url
      if (!CANONICAL_BASE64URL_RE.test(envelope.iv)) return false;
      if (!CANONICAL_BASE64URL_RE.test(envelope.ciphertext)) return false;
      if (envelope.ciphertext.length > Math.ceil(MAX_ENVELOPE_BYTES * 4 / 3) + 24) {
        return false;
      }

      const iv = this.decodeBase64Url(envelope.iv);
      if (iv.length !== AES_GCM_IV_LENGTH) return false;

      const ciphertext = this.decodeBase64Url(envelope.ciphertext);
      if (ciphertext.length < 16) return false;

      return true;
    } catch {
      return false;
    }
  }

  encodeBase64Url(data: Uint8Array): Base64Url {
    let binary = '';
    for (let i = 0; i < data.length; i++) {
      binary += String.fromCharCode(data[i]);
    }
    return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }

  decodeBase64Url(encoded: Base64Url): Uint8Array {
    // Enforce canonical unpadded base64url before decoding
    if (!CANONICAL_BASE64URL_RE.test(encoded)) {
      throw WalletError.invalidInput('base64url', 'not canonical unpadded base64url');
    }
    let base64 = encoded.replace(/-/g, '+').replace(/_/g, '/');
    const pad = base64.length % 4;
    if (pad) base64 += '='.repeat(4 - pad);
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  async calibrateKdf(
    targetMs: number,
    minIterations: number = MIN_PBKDF2_ITERATIONS,
    maxIterations: number = 5_000_000,
  ): Promise<number> {
    const floor = Math.max(minIterations, MIN_PBKDF2_ITERATIONS);
    const ceiling = Math.max(maxIterations, floor);
    const salt = this.generateRandomBytes(16);
    const password = 'calibration-test-password-12345678';
    let iterations = floor;

    for (let attempt = 0; attempt < 8; attempt++) {
      const start = performance.now();
      await this.deriveKeyFromPassword(password, salt, iterations);
      const elapsed = performance.now() - start;

      if (elapsed >= targetMs * 0.9 && elapsed <= targetMs * 1.1) {
        this.calibratedIterations = iterations;
        return iterations;
      }

      if (elapsed < targetMs * 0.5) {
        iterations = Math.min(Math.floor(iterations * 3), ceiling);
      } else if (elapsed < targetMs * 0.9) {
        iterations = Math.min(Math.floor(iterations * 1.5), ceiling);
      } else {
        iterations = Math.max(Math.floor(iterations * 0.7), floor);
      }
    }

    this.calibratedIterations = Math.max(iterations, floor);
    return this.calibratedIterations;
  }

  getCalibratedIterations(): number | null {
    return this.calibratedIterations;
  }

  private assertPurposeBoundAad(aad: string, purpose: AeadPurpose): void {
    if (!aad.startsWith(`${AAD_PREFIX}|schema=${SCHEMA_VERSION}|`)) {
      throw WalletError.invalidInput('aad', 'invalid namespace or schema');
    }
    if (!aad.includes(`|purpose=${purpose}|`)) {
      throw WalletError.invalidInput('aad', 'purpose does not match the operation');
    }
  }
}
