import type { AeadEnvelopeV1, PasswordKeySlotV1, PasswordKdfV1 } from '../types/crypto';
import type { VaultManifestV2, WalletPayloadV2, VaultAccountV2 } from '../types/vault';
import { WalletError } from '../types/errors';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { HDKey } from '@scure/bip32';
import { ethers } from 'ethers';

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const CANONICAL_BASE64URL_RE = /^[A-Za-z0-9_-]*$/;
const ETH_ADDRESS_RE = /^0x[0-9a-fA-F]{40}$/;
const DERIVATION_PATH_RE = /^m\/44'\/60'\/0'\/0\/\d+$/;

const MAX_VAULT_ID_LENGTH = 64;
const MAX_SLOT_ID_LENGTH = 64;
const MAX_NAME_LENGTH = 128;
const MAX_ACCOUNTS = 256;
const MAX_NEXT_INDEX = 2147483647;
const MAX_REVISION = 2147483647;
const MAX_TIMESTAMP = 32503680000000; // year 3000
const MIN_TIMESTAMP = 1700000000000; // ~2023
const MAX_ENVELOPE_CIPHERTEXT_BYTES = 10 * 1024 * 1024; // 10MB
const MIN_PBKDF2_ITERATIONS = 600_000;
const MAX_PBKDF2_ITERATIONS = 100_000_000;
const MAX_KEY_SLOTS = 16;
const AES_GCM_IV_LENGTH = 12;

export function assertString(value: unknown, field: string): asserts value is string {
  if (typeof value !== 'string') {
    throw WalletError.invalidInput(field, 'must be a string');
  }
}

export function assertNumber(value: unknown, field: string): asserts value is number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw WalletError.invalidInput(field, 'must be a finite number');
  }
}

export function assertInteger(value: unknown, field: string): asserts value is number {
  assertNumber(value, field);
  if (!Number.isInteger(value)) {
    throw WalletError.invalidInput(field, 'must be an integer');
  }
}

export function assertObject(value: unknown, field: string): asserts value is Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw WalletError.invalidInput(field, 'must be a non-null object');
  }
}

export function assertArray(value: unknown, field: string): asserts value is unknown[] {
  if (!Array.isArray(value)) {
    throw WalletError.invalidInput(field, 'must be an array');
  }
}

function rejectUnknownFields(obj: Record<string, unknown>, knownKeys: ReadonlySet<string>, prefix: string): void {
  for (const key of Object.keys(obj)) {
    if (!knownKeys.has(key)) {
      throw WalletError.invalidInput(`${prefix}.${key}`, 'unknown field');
    }
  }
}

export function validateVaultId(value: unknown): string {
  assertString(value, 'vaultId');
  if (value.length === 0 || value.length > MAX_VAULT_ID_LENGTH) {
    throw WalletError.invalidInput('vaultId', `length must be 1-${MAX_VAULT_ID_LENGTH}`);
  }
  if (!UUID_RE.test(value)) {
    throw WalletError.invalidInput('vaultId', 'must be a valid UUID v4');
  }
  return value;
}

export function validateSlotId(value: unknown): string {
  assertString(value, 'slotId');
  if (value.length === 0 || value.length > MAX_SLOT_ID_LENGTH) {
    throw WalletError.invalidInput('slotId', `length must be 1-${MAX_SLOT_ID_LENGTH}`);
  }
  if (!UUID_RE.test(value)) {
    throw WalletError.invalidInput('slotId', 'must be a valid UUID v4');
  }
  return value;
}

export function validateRevision(value: unknown): number {
  assertInteger(value, 'revision');
  if ((value as number) < 0 || (value as number) > MAX_REVISION) {
    throw WalletError.invalidInput('revision', `must be 0-${MAX_REVISION}`);
  }
  return value as number;
}

export function validateTimestamp(value: unknown, field: string): number {
  assertInteger(value, field);
  const n = value as number;
  if (n < MIN_TIMESTAMP || n > MAX_TIMESTAMP) {
    throw WalletError.invalidInput(field, `must be ${MIN_TIMESTAMP}-${MAX_TIMESTAMP}`);
  }
  return n;
}

export function validateCanonicalBase64Url(value: unknown, field: string, maxBytes?: number): string {
  assertString(value, field);
  if (!CANONICAL_BASE64URL_RE.test(value)) {
    throw WalletError.invalidInput(field, 'must be canonical unpadded base64url');
  }
  if (maxBytes !== undefined) {
    const approxBytes = Math.ceil((value.length * 3) / 4);
    if (approxBytes > maxBytes) {
      throw WalletError.invalidInput(field, `exceeds maximum size of ${maxBytes} bytes`);
    }
  }
  return value;
}

// Backward-compatible alias
export const validateBase64Url = validateCanonicalBase64Url;

export function validateEnvelope(value: unknown, field: string): AeadEnvelopeV1 {
  assertObject(value, field);
  const obj = value as Record<string, unknown>;

  // Strictly reject unknown fields in envelope
  const ENVELOPE_KNOWN_KEYS = new Set(['version', 'algorithm', 'iv', 'ciphertext']);
  rejectUnknownFields(obj, ENVELOPE_KNOWN_KEYS, field);

  if (obj.version !== 1) {
    throw WalletError.invalidInput(`${field}.version`, 'must be 1');
  }
  if (obj.algorithm !== 'AES-256-GCM') {
    throw WalletError.invalidInput(`${field}.algorithm`, 'must be AES-256-GCM');
  }

  const iv = validateCanonicalBase64Url(obj.iv, `${field}.iv`, AES_GCM_IV_LENGTH + 4);
  const ciphertext = validateCanonicalBase64Url(obj.ciphertext, `${field}.ciphertext`, MAX_ENVELOPE_CIPHERTEXT_BYTES);

  // Decode and check exact IV length
  const ivDecoded = base64UrlDecode(iv);
  if (ivDecoded.length !== AES_GCM_IV_LENGTH) {
    throw WalletError.invalidInput(`${field}.iv`, `must decode to exactly ${AES_GCM_IV_LENGTH} bytes`);
  }

  // Ciphertext must be at least 16 bytes (GCM tag)
  const ctDecoded = base64UrlDecode(ciphertext);
  if (ctDecoded.length < 16) {
    throw WalletError.invalidInput(`${field}.ciphertext`, 'too short (must include auth tag)');
  }

  return { version: 1, algorithm: 'AES-256-GCM', iv, ciphertext };
}

export function validateKdf(value: unknown, field: string): PasswordKdfV1 {
  assertObject(value, field);
  const obj = value as Record<string, unknown>;

  // Strictly reject unknown fields in KDF
  const KDF_KNOWN_KEYS = new Set(['algorithm', 'salt', 'iterations']);
  rejectUnknownFields(obj, KDF_KNOWN_KEYS, field);

  if (obj.algorithm !== 'PBKDF2-HMAC-SHA-256') {
    throw WalletError.invalidInput(`${field}.algorithm`, 'must be PBKDF2-HMAC-SHA-256');
  }

  const salt = validateCanonicalBase64Url(obj.salt, `${field}.salt`, 1024);
  const saltDecoded = base64UrlDecode(salt);
  if (saltDecoded.length < 16) {
    throw WalletError.invalidInput(`${field}.salt`, 'must be at least 16 bytes');
  }

  assertInteger(obj.iterations, `${field}.iterations`);
  const iterations = obj.iterations as number;
  if (iterations < MIN_PBKDF2_ITERATIONS || iterations > MAX_PBKDF2_ITERATIONS) {
    throw WalletError.invalidInput(`${field}.iterations`, `must be ${MIN_PBKDF2_ITERATIONS}-${MAX_PBKDF2_ITERATIONS}`);
  }

  return { algorithm: 'PBKDF2-HMAC-SHA-256', salt, iterations };
}

export function validateKeySlot(value: unknown, field: string): PasswordKeySlotV1 {
  assertObject(value, field);
  const obj = value as Record<string, unknown>;

  // Strictly reject unknown fields in key slot
  const SLOT_KNOWN_KEYS = new Set(['version', 'id', 'type', 'kdf', 'wrappedVaultKey', 'createdAt']);
  rejectUnknownFields(obj, SLOT_KNOWN_KEYS, field);

  if (obj.version !== 1) {
    throw WalletError.invalidInput(`${field}.version`, 'must be 1');
  }

  const id = validateSlotId(obj.id);

  if (obj.type !== 'password') {
    throw WalletError.invalidInput(`${field}.type`, 'must be password');
  }

  const kdf = validateKdf(obj.kdf, `${field}.kdf`);
  const wrappedVaultKey = validateEnvelope(obj.wrappedVaultKey, `${field}.wrappedVaultKey`);
  const createdAt = validateTimestamp(obj.createdAt, `${field}.createdAt`);

  return { version: 1, id, type: 'password', kdf, wrappedVaultKey, createdAt };
}

export function validateManifest(value: unknown): VaultManifestV2 {
  assertObject(value, 'manifest');
  const obj = value as Record<string, unknown>;

  if (obj.schema !== 2) {
    if (typeof obj.schema === 'number' && obj.schema > 2) {
      throw WalletError.unsupportedVersion(obj.schema);
    }
    throw WalletError.invalidInput('manifest.schema', 'must be 2');
  }

  const vaultId = validateVaultId(obj.vaultId);
  const revision = validateRevision(obj.revision);
  const createdAt = validateTimestamp(obj.createdAt, 'manifest.createdAt');
  const updatedAt = validateTimestamp(obj.updatedAt, 'manifest.updatedAt');

  if (updatedAt < createdAt) {
    throw WalletError.invalidInput('manifest.updatedAt', 'must not be before createdAt');
  }

  assertArray(obj.keySlots, 'manifest.keySlots');
  const keySlots = obj.keySlots as unknown[];
  if (keySlots.length === 0) {
    throw WalletError.invalidInput('manifest.keySlots', 'must have at least one key slot');
  }
  if (keySlots.length > MAX_KEY_SLOTS) {
    throw WalletError.invalidInput('manifest.keySlots', `must not exceed ${MAX_KEY_SLOTS}`);
  }

  const slotIds = new Set<string>();
  const validatedSlots: PasswordKeySlotV1[] = [];
  for (let i = 0; i < keySlots.length; i++) {
    const slot = validateKeySlot(keySlots[i], `manifest.keySlots[${i}]`);
    if (slotIds.has(slot.id)) {
      throw WalletError.invalidInput(`manifest.keySlots[${i}].id`, 'duplicate slot id');
    }
    slotIds.add(slot.id);
    validatedSlots.push(slot);
  }

  const verifier = validateEnvelope(obj.verifier, 'manifest.verifier');
  const payload = validateEnvelope(obj.payload, 'manifest.payload');

  // Reject unknown mandatory fields
  const knownKeys = new Set(['schema', 'vaultId', 'revision', 'createdAt', 'updatedAt', 'keySlots', 'verifier', 'payload']);
  rejectUnknownFields(obj, knownKeys, 'manifest');

  return {
    schema: 2,
    vaultId,
    revision,
    createdAt,
    updatedAt,
    keySlots: validatedSlots,
    verifier,
    payload,
  };
}

export function validateEthAddress(value: unknown, field: string): string {
  assertString(value, field);
  if (!ETH_ADDRESS_RE.test(value)) {
    throw WalletError.invalidInput(field, 'must be a valid checksummed Ethereum address (0x + 40 hex chars)');
  }
  let checksummed: string;
  try {
    checksummed = ethers.getAddress(value);
  } catch {
    throw WalletError.invalidInput(field, 'must be a valid EVM address');
  }
  if (checksummed !== value) {
    throw WalletError.invalidInput(field, 'must use the canonical checksum');
  }
  return checksummed;
}

export function validateDerivationPath(value: unknown, field: string): string {
  assertString(value, field);
  if (!DERIVATION_PATH_RE.test(value)) {
    throw WalletError.invalidInput(field, 'must match m/44\'/60\'/0\'/0/<index>');
  }
  return value;
}

export function validateAccountName(value: unknown, field: string): string {
  assertString(value, field);
  if (value.length === 0 || value.length > MAX_NAME_LENGTH) {
    throw WalletError.invalidInput(field, `length must be 1-${MAX_NAME_LENGTH}`);
  }
  if (value !== value.trim() || /[\u0000-\u001f\u007f]/.test(value)) {
    throw WalletError.invalidInput(field, 'must be trimmed and contain no control characters');
  }
  return value;
}

export function validateAccount(value: unknown, field: string): VaultAccountV2 {
  assertObject(value, field);
  const obj = value as Record<string, unknown>;

  assertString(obj.id, `${field}.id`);
  if (!UUID_RE.test(obj.id as string)) {
    throw WalletError.invalidInput(`${field}.id`, 'must be a valid UUID v4');
  }
  const id = obj.id as string;

  const address = validateEthAddress(obj.address, `${field}.address`);
  const name = validateAccountName(obj.name, `${field}.name`);

  if (obj.kind === 'derived') {
    const derivationPath = validateDerivationPath(obj.derivationPath, `${field}.derivationPath`);
    assertInteger(obj.accountIndex, `${field}.accountIndex`);
    const accountIndex = obj.accountIndex as number;
    if (accountIndex < 0 || accountIndex > MAX_NEXT_INDEX) {
      throw WalletError.invalidInput(`${field}.accountIndex`, 'out of range');
    }

    const expectedPath = `m/44'/60'/0'/0/${accountIndex}`;
    if (derivationPath !== expectedPath) {
      throw WalletError.invalidInput(`${field}.derivationPath`, `does not match accountIndex (expected ${expectedPath})`);
    }

    const knownKeys = new Set(['id', 'kind', 'address', 'name', 'derivationPath', 'accountIndex']);
    rejectUnknownFields(obj, knownKeys, field);

    return { id, kind: 'derived', address, name, derivationPath, accountIndex };
  } else if (obj.kind === 'imported') {
    assertString(obj.privateKey, `${field}.privateKey`);
    const privateKey = obj.privateKey as string;
    if (!/^0x[0-9a-fA-F]{64}$/.test(privateKey)) {
      throw WalletError.invalidInput(`${field}.privateKey`, 'must be 0x + 64 hex characters');
    }
    let derivedAddress: string;
    try {
      derivedAddress = new ethers.Wallet(privateKey).address;
    } catch {
      throw WalletError.invalidInput(`${field}.privateKey`, 'is not a valid secp256k1 key');
    }
    if (derivedAddress !== address) {
      throw WalletError.invalidInput(
        `${field}.address`,
        'does not match the imported private key',
      );
    }

    const knownKeys = new Set(['id', 'kind', 'address', 'name', 'privateKey']);
    rejectUnknownFields(obj, knownKeys, field);

    return { id, kind: 'imported', address, name, privateKey };
  } else {
    throw WalletError.invalidInput(`${field}.kind`, 'must be derived or imported');
  }
}

export function validatePayload(value: unknown): WalletPayloadV2 {
  assertObject(value, 'payload');
  const obj = value as Record<string, unknown>;

  if (obj.schema !== 2) {
    if (typeof obj.schema === 'number' && obj.schema > 2) {
      throw WalletError.unsupportedVersion(obj.schema);
    }
    throw WalletError.invalidInput('payload.schema', 'must be 2');
  }

  assertString(obj.mnemonic, 'payload.mnemonic');
  const mnemonic = obj.mnemonic as string;
  if (
    mnemonic !== mnemonic.trim().toLowerCase().replace(/\s+/g, ' ')
    || !bip39.validateMnemonic(mnemonic, wordlist)
  ) {
    throw WalletError.invalidInput('payload.mnemonic', 'must be a canonical valid BIP39 phrase');
  }

  // Validate derivation block
  assertObject(obj.derivation, 'payload.derivation');
  const derivation = obj.derivation as Record<string, unknown>;

  // Strictly reject unknown fields in derivation
  const DERIVATION_KNOWN_KEYS = new Set(['curve', 'standard', 'basePath']);
  rejectUnknownFields(derivation, DERIVATION_KNOWN_KEYS, 'payload.derivation');

  if (derivation.curve !== 'secp256k1') {
    throw WalletError.invalidInput('payload.derivation.curve', 'must be secp256k1');
  }
  if (derivation.standard !== 'bip44') {
    throw WalletError.invalidInput('payload.derivation.standard', 'must be bip44');
  }
  if (derivation.basePath !== "m/44'/60'/0'/0") {
    throw WalletError.invalidInput('payload.derivation.basePath', "must be m/44'/60'/0'/0");
  }

  assertArray(obj.accounts, 'payload.accounts');
  const accounts = obj.accounts as unknown[];
  if (accounts.length > MAX_ACCOUNTS) {
    throw WalletError.invalidInput('payload.accounts', `must not exceed ${MAX_ACCOUNTS}`);
  }

  const accountIds = new Set<string>();
  const accountAddresses = new Set<string>();
  const derivedIndexes = new Set<number>();
  const validatedAccounts: VaultAccountV2[] = [];

  for (let i = 0; i < accounts.length; i++) {
    const acc = validateAccount(accounts[i], `payload.accounts[${i}]`);

    if (accountIds.has(acc.id)) {
      throw WalletError.invalidInput(`payload.accounts[${i}].id`, 'duplicate account id');
    }
    accountIds.add(acc.id);

    const addrLower = acc.address.toLowerCase();
    if (accountAddresses.has(addrLower)) {
      throw WalletError.invalidInput(`payload.accounts[${i}].address`, 'duplicate address');
    }
    accountAddresses.add(addrLower);

    if (acc.kind === 'derived') {
      if (derivedIndexes.has(acc.accountIndex)) {
        throw WalletError.invalidInput(`payload.accounts[${i}].accountIndex`, 'duplicate derivation index');
      }
      derivedIndexes.add(acc.accountIndex);
    }

    validatedAccounts.push(acc);
  }

  assertInteger(obj.nextAccountIndex, 'payload.nextAccountIndex');
  const nextAccountIndex = obj.nextAccountIndex as number;
  if (nextAccountIndex < 0 || nextAccountIndex > MAX_NEXT_INDEX) {
    throw WalletError.invalidInput('payload.nextAccountIndex', 'out of range');
  }

  for (const idx of derivedIndexes) {
    if (nextAccountIndex <= idx) {
      throw WalletError.invalidInput('payload.nextAccountIndex', 'must exceed all existing derived account indexes');
    }
  }

  const root = HDKey.fromMasterSeed(bip39.mnemonicToSeedSync(mnemonic));
  for (let i = 0; i < validatedAccounts.length; i++) {
    const account = validatedAccounts[i];
    if (account.kind !== 'derived') continue;
    const derived = root.derive(account.derivationPath);
    if (!derived.privateKey) {
      throw WalletError.invalidInput(
        `payload.accounts[${i}].derivationPath`,
        'did not produce a private key',
      );
    }
    const expectedAddress = new ethers.Wallet(ethers.hexlify(derived.privateKey)).address;
    if (expectedAddress !== account.address) {
      throw WalletError.invalidInput(
        `payload.accounts[${i}].address`,
        'does not match mnemonic derivation',
      );
    }
  }

  let activeAccountId: string | null;
  if (obj.activeAccountId === null) {
    activeAccountId = null;
  } else {
    assertString(obj.activeAccountId, 'payload.activeAccountId');
    activeAccountId = obj.activeAccountId as string;
    if (!accountIds.has(activeAccountId)) {
      throw WalletError.invalidInput(
        'payload.activeAccountId',
        'must reference an existing account',
      );
    }
  }
  if (validatedAccounts.length > 0 && activeAccountId === null) {
    throw WalletError.invalidInput(
      'payload.activeAccountId',
      'must be set when accounts exist',
    );
  }

  const createdAt = validateTimestamp(obj.createdAt, 'payload.createdAt');

  // Reject unknown fields
  const knownKeys = new Set([
    'schema',
    'mnemonic',
    'derivation',
    'accounts',
    'activeAccountId',
    'nextAccountIndex',
    'createdAt',
  ]);
  rejectUnknownFields(obj, knownKeys, 'payload');

  return {
    schema: 2,
    mnemonic,
    derivation: { curve: 'secp256k1', standard: 'bip44', basePath: "m/44'/60'/0'/0" },
    accounts: validatedAccounts,
    activeAccountId,
    nextAccountIndex,
    createdAt,
  };
}

function base64UrlDecode(encoded: string): Uint8Array {
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
