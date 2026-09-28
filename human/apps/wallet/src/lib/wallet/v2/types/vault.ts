import type { AeadEnvelopeV1, PasswordKeySlotV1 } from './crypto';

export interface VaultManifestV2 {
  readonly schema: 2;
  readonly vaultId: string;
  readonly revision: number;
  readonly createdAt: number;
  readonly updatedAt: number;
  readonly keySlots: readonly PasswordKeySlotV1[];
  readonly verifier: AeadEnvelopeV1;
  readonly payload: AeadEnvelopeV1;
}

export interface WalletPayloadV2 {
  readonly schema: 2;
  readonly mnemonic: string;
  readonly derivation: {
    readonly curve: 'secp256k1';
    readonly standard: 'bip44';
    readonly basePath: "m/44'/60'/0'/0";
  };
  readonly accounts: readonly VaultAccountV2[];
  readonly activeAccountId: string | null;
  readonly nextAccountIndex: number;
  readonly createdAt: number;
}

export type VaultAccountV2 =
  | {
      readonly id: string;
      readonly kind: 'derived';
      readonly address: string;
      readonly name: string;
      readonly derivationPath: string;
      readonly accountIndex: number;
    }
  | {
      readonly id: string;
      readonly kind: 'imported';
      readonly address: string;
      readonly name: string;
      readonly privateKey: string;
    };
