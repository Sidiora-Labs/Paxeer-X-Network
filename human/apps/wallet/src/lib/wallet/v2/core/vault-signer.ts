import { ethers } from 'ethers';
import type { WalletCoreV2 } from './wallet-core';

export class VaultSigner extends ethers.AbstractSigner {
  constructor(
    private readonly wallet: WalletCoreV2,
    private readonly address: string,
    provider: null | ethers.Provider = null,
    private readonly chainId: number = 125,
  ) {
    super(provider);
  }

  connect(provider: null | ethers.Provider): VaultSigner {
    return new VaultSigner(this.wallet, this.address, provider, this.chainId);
  }

  async getAddress(): Promise<string> {
    return this.address;
  }

  async signTransaction(transaction: ethers.TransactionRequest): Promise<string> {
    return this.wallet.signTransaction({
      approvalId: crypto.randomUUID(),
      accountAddress: this.address,
      chainId: Number(transaction.chainId ?? this.chainId),
      transaction,
    });
  }

  async signMessage(message: string | Uint8Array): Promise<string> {
    return this.wallet.signMessage({
      approvalId: crypto.randomUUID(),
      accountAddress: this.address,
      chainId: this.chainId,
      message,
    });
  }

  async signTypedData(
    domain: ethers.TypedDataDomain,
    types: Record<string, ethers.TypedDataField[]>,
    value: Record<string, unknown>,
  ): Promise<string> {
    return this.wallet.signTypedData({
      approvalId: crypto.randomUUID(),
      accountAddress: this.address,
      chainId: Number(domain.chainId ?? this.chainId),
      domain,
      types,
      value,
    });
  }
}
