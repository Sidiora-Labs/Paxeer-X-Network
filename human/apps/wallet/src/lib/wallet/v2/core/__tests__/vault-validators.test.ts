import { describe, it, expect } from 'vitest';
import { webcrypto } from 'node:crypto';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import {
  validateManifest,
  validatePayload,
  validateEnvelope,
  validateKeySlot,
  validateKdf,
  validateAccount,
  validateVaultId,
  validateSlotId,
  validateRevision,
  validateTimestamp,
  validateBase64Url,
  validateEthAddress,
  validateDerivationPath,
  validateAccountName,
} from '../../core/vault-validators';
import { WalletError } from '../../types/errors';
import { WebCryptoAdapter } from '../../adapters/web-crypto-adapter';

Object.defineProperty(globalThis, 'crypto', { value: webcrypto });

const adapter = new WebCryptoAdapter();
const TEST_MNEMONIC = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);
const DERIVED_ADDRESS_0 = '0x9858EfFD232B4033E47d90003D41EC34EcaEda94';
const DERIVED_ADDRESS_1 = '0x6Fac4D18c912343BF86fa7049364Dd4E424Ab9C0';
const DERIVED_ADDRESS_5 = '0xA40cFBFc8534FFC84E20a7d8bBC3729B26a35F6f';
const IMPORTED_PRIVATE_KEY = `0x${'ab'.repeat(32)}`;
const IMPORTED_ADDRESS = '0xe239cdc5fbe977a8a141B72194D3CF8c41bC5BC6';

function makeValidEnvelope() {
  return {
    version: 1,
    algorithm: 'AES-256-GCM',
    iv: adapter.encodeBase64Url(adapter.generateRandomBytes(12)),
    ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(48)),
  };
}

function makeValidSlot(id?: string) {
  return {
    version: 1,
    id: id ?? '12345678-1234-4abc-9abc-123456789abc',
    type: 'password',
    kdf: {
      algorithm: 'PBKDF2-HMAC-SHA-256',
      salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
      iterations: 600_000,
    },
    wrappedVaultKey: makeValidEnvelope(),
    createdAt: 1710000000000,
  };
}

function makeValidManifest() {
  return {
    schema: 2,
    vaultId: 'abcdef01-2345-4678-9abc-def012345678',
    revision: 1,
    createdAt: 1710000000000,
    updatedAt: 1710000000000,
    keySlots: [makeValidSlot()],
    verifier: makeValidEnvelope(),
    payload: makeValidEnvelope(),
  };
}

function makeValidPayload() {
  return {
    schema: 2,
    mnemonic: TEST_MNEMONIC,
    derivation: { curve: 'secp256k1', standard: 'bip44', basePath: "m/44'/60'/0'/0" },
    accounts: [],
    activeAccountId: null,
    nextAccountIndex: 0,
    createdAt: 1710000000000,
  };
}

describe('vault-validators', () => {
  describe('validateVaultId', () => {
    it('accepts valid UUID v4', () => {
      expect(validateVaultId('abcdef01-2345-4678-9abc-def012345678')).toBe('abcdef01-2345-4678-9abc-def012345678');
    });

    it('rejects non-string', () => {
      expect(() => validateVaultId(123)).toThrow(/must be a string/);
    });

    it('rejects empty string', () => {
      expect(() => validateVaultId('')).toThrow(/length/);
    });

    it('rejects non-UUID format', () => {
      expect(() => validateVaultId('not-a-uuid')).toThrow(/UUID/);
    });

    it('rejects UUID with wrong version digit', () => {
      expect(() => validateVaultId('abcdef01-2345-1678-9abc-def012345678')).toThrow(/UUID/);
    });

    it('rejects UUID with wrong variant', () => {
      expect(() => validateVaultId('abcdef01-2345-4678-cabc-def012345678')).toThrow(/UUID/);
    });
  });

  describe('validateSlotId', () => {
    it('accepts valid UUID v4', () => {
      expect(validateSlotId('12345678-1234-4abc-9abc-123456789abc')).toBe('12345678-1234-4abc-9abc-123456789abc');
    });

    it('rejects null', () => {
      expect(() => validateSlotId(null)).toThrow(/must be a string/);
    });
  });

  describe('validateRevision', () => {
    it('accepts 0', () => { expect(validateRevision(0)).toBe(0); });
    it('accepts positive integer', () => { expect(validateRevision(100)).toBe(100); });
    it('accepts MAX_REVISION (2147483647)', () => { expect(validateRevision(2147483647)).toBe(2147483647); });
    it('rejects negative', () => { expect(() => validateRevision(-1)).toThrow(); });
    it('rejects float', () => { expect(() => validateRevision(1.5)).toThrow(/integer/); });
    it('rejects too large (above 2147483647)', () => { expect(() => validateRevision(2147483648)).toThrow(); });
    it('rejects NaN', () => { expect(() => validateRevision(NaN)).toThrow(); });
    it('rejects Infinity', () => { expect(() => validateRevision(Infinity)).toThrow(); });
  });

  describe('validateTimestamp', () => {
    it('accepts valid timestamp', () => {
      expect(validateTimestamp(1710000000000, 'ts')).toBe(1710000000000);
    });
    it('rejects too early', () => {
      expect(() => validateTimestamp(1000000000000, 'ts')).toThrow();
    });
    it('rejects too late', () => {
      expect(() => validateTimestamp(33000000000000, 'ts')).toThrow();
    });
    it('rejects non-integer', () => {
      expect(() => validateTimestamp(1710000000000.5, 'ts')).toThrow(/integer/);
    });
  });

  describe('validateBase64Url (canonical unpadded)', () => {
    it('accepts valid base64url', () => {
      const val = adapter.encodeBase64Url(adapter.generateRandomBytes(32));
      expect(validateBase64Url(val, 'field')).toBe(val);
    });
    it('rejects padding characters (=)', () => {
      expect(() => validateBase64Url('abc=', 'field')).toThrow(/canonical unpadded base64url/);
    });
    it('rejects standard base64 chars (+)', () => {
      expect(() => validateBase64Url('abc+def', 'field')).toThrow(/canonical unpadded base64url/);
    });
    it('rejects standard base64 chars (/)', () => {
      expect(() => validateBase64Url('abc/def', 'field')).toThrow(/canonical unpadded base64url/);
    });
    it('rejects oversized data', () => {
      const big = adapter.encodeBase64Url(adapter.generateRandomBytes(100));
      expect(() => validateBase64Url(big, 'field', 10)).toThrow(/exceeds/);
    });
    it('accepts empty string', () => {
      expect(validateBase64Url('', 'field')).toBe('');
    });
    it('rejects whitespace', () => {
      expect(() => validateBase64Url('abc def', 'field')).toThrow(/canonical unpadded base64url/);
    });
    it('rejects newlines', () => {
      expect(() => validateBase64Url('abc\ndef', 'field')).toThrow(/canonical unpadded base64url/);
    });
  });

  describe('validateEnvelope', () => {
    it('accepts valid envelope', () => {
      const env = makeValidEnvelope();
      const result = validateEnvelope(env, 'env');
      expect(result.version).toBe(1);
      expect(result.algorithm).toBe('AES-256-GCM');
    });

    it('rejects wrong version', () => {
      const env = { ...makeValidEnvelope(), version: 2 };
      expect(() => validateEnvelope(env, 'env')).toThrow(/version/);
    });

    it('rejects wrong algorithm', () => {
      const env = { ...makeValidEnvelope(), algorithm: 'AES-128-GCM' };
      expect(() => validateEnvelope(env, 'env')).toThrow(/algorithm/);
    });

    it('rejects short IV', () => {
      const env = { ...makeValidEnvelope(), iv: adapter.encodeBase64Url(adapter.generateRandomBytes(8)) };
      expect(() => validateEnvelope(env, 'env')).toThrow(/iv/);
    });

    it('rejects long IV', () => {
      const env = { ...makeValidEnvelope(), iv: adapter.encodeBase64Url(adapter.generateRandomBytes(16)) };
      expect(() => validateEnvelope(env, 'env')).toThrow(/iv/);
    });

    it('rejects ciphertext shorter than auth tag', () => {
      const env = { ...makeValidEnvelope(), ciphertext: adapter.encodeBase64Url(adapter.generateRandomBytes(8)) };
      expect(() => validateEnvelope(env, 'env')).toThrow(/ciphertext/);
    });

    it('rejects null', () => {
      expect(() => validateEnvelope(null, 'env')).toThrow(/object/);
    });

    it('rejects array', () => {
      expect(() => validateEnvelope([], 'env')).toThrow(/object/);
    });

    it('rejects unknown fields in envelope', () => {
      const env = { ...makeValidEnvelope(), extraField: 'surprise' };
      expect(() => validateEnvelope(env, 'env')).toThrow(/unknown field/);
    });

    it('rejects tag field in envelope', () => {
      const env = { ...makeValidEnvelope(), tag: 'value' };
      expect(() => validateEnvelope(env, 'env')).toThrow(/unknown field/);
    });

    it('rejects non-canonical base64url in IV (padded)', () => {
      const env = { ...makeValidEnvelope(), iv: adapter.encodeBase64Url(adapter.generateRandomBytes(12)) + '=' };
      expect(() => validateEnvelope(env, 'env')).toThrow(/canonical unpadded base64url/);
    });
  });

  describe('validateKdf', () => {
    it('accepts valid KDF params', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 600_000,
      };
      const result = validateKdf(kdf, 'kdf');
      expect(result.algorithm).toBe('PBKDF2-HMAC-SHA-256');
    });

    it('rejects wrong algorithm', () => {
      const kdf = {
        algorithm: 'scrypt',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 600_000,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/algorithm/);
    });

    it('rejects short salt', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(8)),
        iterations: 600_000,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/salt/);
    });

    it('rejects iterations below minimum', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 100_000,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/iterations/);
    });

    it('rejects iterations above maximum', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 200_000_000,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/iterations/);
    });

    it('rejects unknown fields in KDF', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 600_000,
        memory: 65536,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/unknown field/);
    });

    it('rejects parallelism field in KDF', () => {
      const kdf = {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: adapter.encodeBase64Url(adapter.generateRandomBytes(32)),
        iterations: 600_000,
        parallelism: 4,
      };
      expect(() => validateKdf(kdf, 'kdf')).toThrow(/unknown field/);
    });
  });

  describe('validateKeySlot', () => {
    it('accepts valid slot', () => {
      const slot = makeValidSlot();
      const result = validateKeySlot(slot, 'slot');
      expect(result.type).toBe('password');
    });

    it('rejects wrong version', () => {
      const slot = { ...makeValidSlot(), version: 2 };
      expect(() => validateKeySlot(slot, 'slot')).toThrow(/version/);
    });

    it('rejects non-password type', () => {
      const slot = { ...makeValidSlot(), type: 'biometric' };
      expect(() => validateKeySlot(slot, 'slot')).toThrow(/type/);
    });

    it('rejects invalid slot id', () => {
      const slot = { ...makeValidSlot(), id: 'bad-id' };
      expect(() => validateKeySlot(slot, 'slot')).toThrow(/UUID/);
    });

    it('rejects unknown fields in key slot', () => {
      const slot = { ...makeValidSlot(), verifier: 'something' };
      expect(() => validateKeySlot(slot, 'slot')).toThrow(/unknown field/);
    });

    it('rejects lastUsed field in key slot', () => {
      const slot = { ...makeValidSlot(), lastUsed: 1710000000000 };
      expect(() => validateKeySlot(slot, 'slot')).toThrow(/unknown field/);
    });
  });

  describe('validateManifest', () => {
    it('accepts valid manifest', () => {
      const m = makeValidManifest();
      const result = validateManifest(m);
      expect(result.schema).toBe(2);
      expect(result.vaultId).toBe('abcdef01-2345-4678-9abc-def012345678');
    });

    it('rejects unsupported future version', () => {
      const m = { ...makeValidManifest(), schema: 3 };
      expect(() => validateManifest(m)).toThrow();
      try { validateManifest(m); } catch (e: any) {
        expect(e.code).toBe('UNSUPPORTED_VERSION');
      }
    });

    it('rejects schema version 1', () => {
      const m = { ...makeValidManifest(), schema: 1 };
      expect(() => validateManifest(m)).toThrow(/schema/);
    });

    it('rejects updatedAt before createdAt', () => {
      const m = { ...makeValidManifest(), updatedAt: 1709000000000 };
      expect(() => validateManifest(m)).toThrow(/updatedAt/);
    });

    it('rejects empty keySlots', () => {
      const m = { ...makeValidManifest(), keySlots: [] };
      expect(() => validateManifest(m)).toThrow(/at least one/);
    });

    it('rejects too many keySlots', () => {
      const m = { ...makeValidManifest(), keySlots: Array(17).fill(null).map((_, i) => makeValidSlot(`${i.toString().padStart(8, '0')}-1234-4abc-9abc-123456789abc`)) };
      expect(() => validateManifest(m)).toThrow(/exceed/);
    });

    it('rejects duplicate slot ids', () => {
      const slotId = '12345678-1234-4abc-9abc-123456789abc';
      const m = { ...makeValidManifest(), keySlots: [makeValidSlot(slotId), makeValidSlot(slotId)] };
      expect(() => validateManifest(m)).toThrow(/duplicate/);
    });

    it('rejects unknown mandatory fields', () => {
      const m = { ...makeValidManifest(), extraField: 'surprise' };
      expect(() => validateManifest(m)).toThrow(/unknown/);
    });

    it('rejects null input', () => {
      expect(() => validateManifest(null)).toThrow(/object/);
    });

    it('rejects array input', () => {
      expect(() => validateManifest([])).toThrow(/object/);
    });
  });

  describe('validateEthAddress', () => {
    it('accepts valid address', () => {
      expect(validateEthAddress(DERIVED_ADDRESS_0, 'addr')).toBe(DERIVED_ADDRESS_0);
    });
    it('rejects without 0x prefix', () => {
      expect(() => validateEthAddress('1234567890abcdef1234567890abcdef12345678', 'addr')).toThrow();
    });
    it('rejects too short', () => {
      expect(() => validateEthAddress('0x1234', 'addr')).toThrow();
    });
    it('rejects too long', () => {
      expect(() => validateEthAddress('0x1234567890abcdef1234567890abcdef123456789', 'addr')).toThrow();
    });
    it('rejects invalid hex chars', () => {
      expect(() => validateEthAddress('0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ', 'addr')).toThrow();
    });
  });

  describe('validateDerivationPath', () => {
    it('accepts valid path', () => {
      expect(validateDerivationPath("m/44'/60'/0'/0/0", 'p')).toBe("m/44'/60'/0'/0/0");
      expect(validateDerivationPath("m/44'/60'/0'/0/99", 'p')).toBe("m/44'/60'/0'/0/99");
    });
    it('rejects wrong base path', () => {
      expect(() => validateDerivationPath("m/44'/0'/0'/0/0", 'p')).toThrow();
    });
    it('rejects missing index', () => {
      expect(() => validateDerivationPath("m/44'/60'/0'/0/", 'p')).toThrow();
    });
  });

  describe('validateAccountName', () => {
    it('accepts valid name', () => {
      expect(validateAccountName('Account 1', 'n')).toBe('Account 1');
    });
    it('rejects empty', () => {
      expect(() => validateAccountName('', 'n')).toThrow(/length/);
    });
    it('rejects too long', () => {
      expect(() => validateAccountName('x'.repeat(129), 'n')).toThrow(/length/);
    });
  });

  describe('validateAccount', () => {
    it('accepts valid derived account', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'derived',
        address: DERIVED_ADDRESS_0,
        name: 'Account 1',
        derivationPath: "m/44'/60'/0'/0/0",
        accountIndex: 0,
      };
      const result = validateAccount(acc, 'acc');
      expect(result.kind).toBe('derived');
    });

    it('accepts valid imported account', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'imported',
        address: IMPORTED_ADDRESS,
        name: 'Imported',
        privateKey: IMPORTED_PRIVATE_KEY,
      };
      const result = validateAccount(acc, 'acc');
      expect(result.kind).toBe('imported');
    });

    it('rejects derived account with path/index mismatch', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'derived',
        address: DERIVED_ADDRESS_0,
        name: 'Account 1',
        derivationPath: "m/44'/60'/0'/0/5",
        accountIndex: 3,
      };
      expect(() => validateAccount(acc, 'acc')).toThrow(/does not match/);
    });

    it('rejects imported account with invalid private key', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'imported',
        address: DERIVED_ADDRESS_0,
        name: 'Imported',
        privateKey: '0xshort',
      };
      expect(() => validateAccount(acc, 'acc')).toThrow(/privateKey/);
    });

    it('rejects unknown kind', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'hardware',
        address: DERIVED_ADDRESS_0,
        name: 'HW',
      };
      expect(() => validateAccount(acc, 'acc')).toThrow(/kind/);
    });

    it('rejects unknown extra field on derived account', () => {
      const acc = {
        id: 'abcdef01-2345-4678-9abc-def012345678',
        kind: 'derived',
        address: DERIVED_ADDRESS_0,
        name: 'Account 1',
        derivationPath: "m/44'/60'/0'/0/0",
        accountIndex: 0,
        extraField: true,
      };
      expect(() => validateAccount(acc, 'acc')).toThrow(/unknown/);
    });

    it('rejects account with invalid UUID id', () => {
      const acc = {
        id: 'not-a-uuid',
        kind: 'derived',
        address: DERIVED_ADDRESS_0,
        name: 'Account 1',
        derivationPath: "m/44'/60'/0'/0/0",
        accountIndex: 0,
      };
      expect(() => validateAccount(acc, 'acc')).toThrow(/UUID/);
    });
  });

  describe('validatePayload', () => {
    it('accepts valid empty payload', () => {
      const p = makeValidPayload();
      const result = validatePayload(p);
      expect(result.schema).toBe(2);
    });

    it('rejects unsupported future version', () => {
      const p = { ...makeValidPayload(), schema: 3 };
      try { validatePayload(p); } catch (e: any) {
        expect(e.code).toBe('UNSUPPORTED_VERSION');
      }
    });

    it('rejects invalid mnemonic word count', () => {
      const p = { ...makeValidPayload(), mnemonic: 'one two three' };
      expect(() => validatePayload(p)).toThrow(/BIP39/);
    });

    it('rejects wrong derivation curve', () => {
      const p = { ...makeValidPayload(), derivation: { curve: 'ed25519', standard: 'bip44', basePath: "m/44'/60'/0'/0" } };
      expect(() => validatePayload(p)).toThrow(/secp256k1/);
    });

    it('rejects wrong derivation basePath', () => {
      const p = { ...makeValidPayload(), derivation: { curve: 'secp256k1', standard: 'bip44', basePath: "m/44'/0'/0'/0" } };
      expect(() => validatePayload(p)).toThrow(/basePath/);
    });

    it('rejects unknown fields in derivation block', () => {
      const p = {
        ...makeValidPayload(),
        derivation: { curve: 'secp256k1', standard: 'bip44', basePath: "m/44'/60'/0'/0", coinType: 60 },
      };
      expect(() => validatePayload(p)).toThrow(/unknown field/);
    });

    it('rejects duplicate account ids', () => {
      const id = 'abcdef01-2345-4678-9abc-def012345678';
      const p = {
        ...makeValidPayload(),
        nextAccountIndex: 2,
        accounts: [
          { id, kind: 'derived', address: DERIVED_ADDRESS_0, name: 'A', derivationPath: "m/44'/60'/0'/0/0", accountIndex: 0 },
          { id, kind: 'derived', address: DERIVED_ADDRESS_1, name: 'B', derivationPath: "m/44'/60'/0'/0/1", accountIndex: 1 },
        ],
      };
      expect(() => validatePayload(p)).toThrow(/duplicate account id/);
    });

    it('rejects duplicate addresses', () => {
      const addr = DERIVED_ADDRESS_0;
      const p = {
        ...makeValidPayload(),
        nextAccountIndex: 2,
        accounts: [
          { id: 'abcdef01-2345-4678-9abc-def012345678', kind: 'derived', address: addr, name: 'A', derivationPath: "m/44'/60'/0'/0/0", accountIndex: 0 },
          { id: 'bbcdef01-2345-4678-9abc-def012345678', kind: 'derived', address: addr, name: 'B', derivationPath: "m/44'/60'/0'/0/1", accountIndex: 1 },
        ],
      };
      expect(() => validatePayload(p)).toThrow(/duplicate address/);
    });

    it('rejects duplicate derivation indexes', () => {
      const p = {
        ...makeValidPayload(),
        nextAccountIndex: 2,
        accounts: [
          { id: 'abcdef01-2345-4678-9abc-def012345678', kind: 'derived', address: DERIVED_ADDRESS_0, name: 'A', derivationPath: "m/44'/60'/0'/0/0", accountIndex: 0 },
          { id: 'bbcdef01-2345-4678-9abc-def012345678', kind: 'derived', address: DERIVED_ADDRESS_1, name: 'B', derivationPath: "m/44'/60'/0'/0/0", accountIndex: 0 },
        ],
      };
      expect(() => validatePayload(p)).toThrow(/duplicate derivation index/);
    });

    it('rejects nextAccountIndex less than derived indexes', () => {
      const p = {
        ...makeValidPayload(),
        nextAccountIndex: 0,
        accounts: [
          { id: 'abcdef01-2345-4678-9abc-def012345678', kind: 'derived', address: DERIVED_ADDRESS_5, name: 'A', derivationPath: "m/44'/60'/0'/0/5", accountIndex: 5 },
        ],
      };
      expect(() => validatePayload(p)).toThrow(/nextAccountIndex/);
    });

    it('rejects unknown fields in payload', () => {
      const p = { ...makeValidPayload(), unknownField: 'hi' };
      expect(() => validatePayload(p)).toThrow(/unknown/);
    });

    it('rejects too many accounts', () => {
      const accounts = Array.from({ length: 257 }, (_, i) => ({
        id: `${i.toString(16).padStart(8, '0')}-1234-4abc-9abc-123456789abc`,
        kind: 'derived',
        address: `0x${i.toString(16).padStart(40, '0')}`,
        name: `Account ${i}`,
        derivationPath: `m/44'/60'/0'/0/${i}`,
        accountIndex: i,
      }));
      const p = { ...makeValidPayload(), accounts, nextAccountIndex: 257 };
      expect(() => validatePayload(p)).toThrow(/exceed/);
    });
  });
});
