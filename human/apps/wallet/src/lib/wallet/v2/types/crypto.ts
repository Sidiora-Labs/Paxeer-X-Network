export type Base64Url = string;

export interface AeadEnvelopeV1 {
  readonly version: 1;
  readonly algorithm: 'AES-256-GCM';
  readonly iv: Base64Url;
  readonly ciphertext: Base64Url;
}

export interface PasswordKdfV1 {
  readonly algorithm: 'PBKDF2-HMAC-SHA-256';
  readonly salt: Base64Url;
  readonly iterations: number;
}

export interface PasswordKeySlotV1 {
  readonly version: 1;
  readonly id: string;
  readonly type: 'password';
  readonly kdf: PasswordKdfV1;
  readonly wrappedVaultKey: AeadEnvelopeV1;
  readonly createdAt: number;
}

export type AeadPurpose =
  | 'key-wrap'
  | 'vault-payload'
  | 'vault-verifier'
  | 'migration-verifier';

export interface CryptoCapabilities {
  readonly subtle: boolean;
  readonly getRandomValues: boolean;
  readonly secureContext: boolean;
}
