import CryptoJS from 'crypto-js';
import { WalletError } from '../types/errors';

const LEGACY_WALLET_SALT = 'paxeer_wallet_salt_2024';
const LEGACY_ITERATIONS = 10_000;

interface LegacyPinRecord {
  hash: string;
  salt: string;
}

function constantTimeEqual(left: string, right: string): boolean {
  if (left.length !== right.length) return false;
  let difference = 0;
  for (let i = 0; i < left.length; i++) {
    difference |= left.charCodeAt(i) ^ right.charCodeAt(i);
  }
  return difference === 0;
}

export class LegacyCryptoJsReader {
  verifyPin(pin: string, rawRecord: string): boolean {
    let record: LegacyPinRecord;
    try {
      const parsed: unknown = JSON.parse(rawRecord);
      if (
        !parsed
        || typeof parsed !== 'object'
        || Array.isArray(parsed)
        || typeof (parsed as Record<string, unknown>).hash !== 'string'
        || typeof (parsed as Record<string, unknown>).salt !== 'string'
      ) {
        return false;
      }
      record = parsed as LegacyPinRecord;
    } catch {
      return false;
    }

    const candidate = CryptoJS.PBKDF2(pin, record.salt, {
      keySize: 512 / 32,
      iterations: LEGACY_ITERATIONS,
    }).toString();
    return constantTimeEqual(candidate, record.hash);
  }

  decryptWalletSecret(pin: string, ciphertext: string): string {
    if (!ciphertext || typeof ciphertext !== 'string') {
      throw WalletError.migrationFailed('Legacy ciphertext is malformed');
    }
    const walletKey = CryptoJS.PBKDF2(pin, LEGACY_WALLET_SALT, {
      keySize: 256 / 32,
      iterations: LEGACY_ITERATIONS,
    }).toString();
    try {
      const plaintext = CryptoJS.AES.decrypt(ciphertext, walletKey).toString(
        CryptoJS.enc.Utf8,
      );
      if (!plaintext) throw new Error('empty plaintext');
      return plaintext;
    } catch {
      throw WalletError.migrationFailed('Legacy decryption failed');
    }
  }
}
