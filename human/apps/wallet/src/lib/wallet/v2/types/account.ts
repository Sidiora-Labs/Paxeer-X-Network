export interface WalletAccount {
  readonly id: string;
  readonly address: string;
  readonly name: string;
  readonly derivationPath: string;
  readonly accountIndex: number;
  readonly kind: 'derived' | 'imported';
}
