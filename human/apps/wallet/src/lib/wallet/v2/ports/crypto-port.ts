import type { CryptoCapabilities, AeadEnvelopeV1, AeadPurpose, Base64Url } from '../types/crypto';

export interface CryptoPort {
  capabilities(): CryptoCapabilities;

  calibrateKdf(
    targetMs: number,
    minIterations?: number,
    maxIterations?: number,
  ): Promise<number>;

  getCalibratedIterations(): number | null;

  generateRandomBytes(length: number): Uint8Array;

  importAesGcmKey(raw: Uint8Array, extractable: boolean): Promise<CryptoKey>;

  encrypt(
    key: CryptoKey,
    plaintext: Uint8Array,
    aad: string,
    purpose: AeadPurpose,
  ): Promise<AeadEnvelopeV1>;

  decrypt(
    key: CryptoKey,
    envelope: AeadEnvelopeV1,
    aad: string,
    purpose: AeadPurpose,
  ): Promise<Uint8Array>;

  deriveKeyFromPassword(
    password: string,
    salt: Uint8Array,
    iterations: number,
  ): Promise<CryptoKey>;

  wrapVaultKey(
    vaultKey: CryptoKey,
    kek: CryptoKey,
    vaultId: string,
    slotId: string,
  ): Promise<AeadEnvelopeV1>;

  unwrapVaultKey(
    envelope: AeadEnvelopeV1,
    kek: CryptoKey,
    vaultId: string,
    slotId: string,
  ): Promise<CryptoKey>;

  /**
   * Rewrap the VEK from an old KEK to a new KEK without exposing an extractable
   * CryptoKey handle. The operation internally unwraps the raw VEK bytes from the
   * old envelope using the old KEK, then immediately wraps them under the new KEK
   * with fresh IV and correct AAD. The raw VEK bytes never leave the adapter as a
   * CryptoKey handle marked extractable.
   */
  rewrapVaultKey(
    oldEnvelope: AeadEnvelopeV1,
    oldKek: CryptoKey,
    oldVaultId: string,
    oldSlotId: string,
    newKek: CryptoKey,
    newVaultId: string,
    newSlotId: string,
  ): Promise<AeadEnvelopeV1>;

  buildAad(
    vaultId: string,
    purpose: AeadPurpose,
    slotId?: string,
  ): string;

  validateEnvelope(envelope: AeadEnvelopeV1): boolean;

  encodeBase64Url(data: Uint8Array): Base64Url;

  decodeBase64Url(encoded: Base64Url): Uint8Array;
}
