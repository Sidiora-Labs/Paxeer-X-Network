import { createCipheriv, createDecipheriv, randomBytes, timingSafeEqual } from 'node:crypto';

/**
 * AES-256-GCM envelope encryption for wallet private keys.
 *
 * Storage format (base64):
 *   [ 1 byte version ][ 12 bytes IV ][ 16 bytes auth tag ][ N bytes ciphertext ]
 *
 * Why this shape:
 *   - Version byte lets us rotate master keys / algorithms without breaking
 *     decrypt of older rows. Bump when changing scheme.
 *   - 12-byte IV is the GCM-recommended size; randomized per record.
 *   - GCM provides authenticated encryption (no separate HMAC needed).
 *   - The whole envelope is base64'd so it's safe in a Postgres `text` column.
 *
 * The master key MUST be exactly 32 bytes (256 bits). Generate with:
 *   node -e "console.log(require('crypto').randomBytes(32).toString('base64'))"
 *
 * In production, swap `loadMasterKey()` to fetch from KMS / Vault. The wire
 * format and call-site API stay identical.
 */

const ALGO = 'aes-256-gcm';
const KEY_LEN = 32; // 256 bits
const IV_LEN = 12; // 96 bits (GCM standard)
const TAG_LEN = 16; // 128 bits
const VERSION_LEN = 1;

export const SUPPORTED_VERSIONS = new Set<number>([1]);

export interface EncryptResult {
  /** Self-describing base64 blob safe to persist. */
  ciphertext: string;
  /** Version byte that gated this encryption. */
  version: number;
}

export class CryptoError extends Error {
  constructor(message: string, public readonly code: string) {
    super(message);
    this.name = 'CryptoError';
  }
}

function assertKey(key: Buffer): void {
  if (!Buffer.isBuffer(key) || key.byteLength !== KEY_LEN) {
    throw new CryptoError(
      `master key must be exactly ${KEY_LEN} bytes; got ${key.byteLength}`,
      'BAD_KEY_LENGTH',
    );
  }
}

/**
 * Encrypt plaintext bytes with the supplied 32-byte master key.
 * Returns a base64 envelope ready to persist.
 */
export function encrypt(plaintext: Buffer, masterKey: Buffer, version = 1): EncryptResult {
  assertKey(masterKey);
  if (!SUPPORTED_VERSIONS.has(version)) {
    throw new CryptoError(`unsupported version: ${version}`, 'UNSUPPORTED_VERSION');
  }
  const iv = randomBytes(IV_LEN);
  const cipher = createCipheriv(ALGO, masterKey, iv);
  const enc = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  const tag = cipher.getAuthTag();

  const versionByte = Buffer.from([version]);
  const envelope = Buffer.concat([versionByte, iv, tag, enc]);

  return { ciphertext: envelope.toString('base64'), version };
}

/**
 * Decrypt a base64 envelope produced by `encrypt`.
 * Throws CryptoError on tag mismatch / format errors.
 */
export function decrypt(envelopeB64: string, masterKey: Buffer): Buffer {
  assertKey(masterKey);
  let envelope: Buffer;
  try {
    envelope = Buffer.from(envelopeB64, 'base64');
  } catch {
    throw new CryptoError('invalid base64 envelope', 'BAD_BASE64');
  }
  if (envelope.byteLength < VERSION_LEN + IV_LEN + TAG_LEN + 1) {
    throw new CryptoError('envelope too short', 'TRUNCATED');
  }
  const version = envelope[0];
  if (version === undefined || !SUPPORTED_VERSIONS.has(version)) {
    throw new CryptoError(`unsupported envelope version: ${version}`, 'UNSUPPORTED_VERSION');
  }
  const iv = envelope.subarray(VERSION_LEN, VERSION_LEN + IV_LEN);
  const tag = envelope.subarray(VERSION_LEN + IV_LEN, VERSION_LEN + IV_LEN + TAG_LEN);
  const enc = envelope.subarray(VERSION_LEN + IV_LEN + TAG_LEN);

  const decipher = createDecipheriv(ALGO, masterKey, iv);
  decipher.setAuthTag(tag);
  try {
    return Buffer.concat([decipher.update(enc), decipher.final()]);
  } catch (err) {
    throw new CryptoError(
      `decryption failed (likely tampered ciphertext or wrong master key): ${(err as Error).message}`,
      'DECRYPT_FAILED',
    );
  }
}

/**
 * Constant-time compare for token / hash comparisons elsewhere in the API.
 * Re-exported here so consumers don't have to reach into node:crypto directly.
 */
export function safeEqual(a: Buffer | string, b: Buffer | string): boolean {
  const bufA = Buffer.isBuffer(a) ? a : Buffer.from(a);
  const bufB = Buffer.isBuffer(b) ? b : Buffer.from(b);
  if (bufA.byteLength !== bufB.byteLength) return false;
  return timingSafeEqual(bufA, bufB);
}

let cachedMasterKey: Buffer | null = null;

/**
 * Load the master key from env. Cached after first call.
 *
 * Production deployments should replace this with a KMS-backed fetcher that
 * keeps the key out of the process address space (e.g. KMS Decrypt per-call,
 * AWS Nitro Enclave, etc.). The interface is intentionally narrow so a swap
 * is mechanical.
 */
export function loadMasterKey(masterKeyB64: string): Buffer {
  if (cachedMasterKey) return cachedMasterKey;
  const buf = Buffer.from(masterKeyB64, 'base64');
  assertKey(buf);
  cachedMasterKey = buf;
  return buf;
}

/** Test-only — clears the cached master key so tests can swap keys. */
export function _resetMasterKeyCacheForTests(): void {
  cachedMasterKey = null;
}
