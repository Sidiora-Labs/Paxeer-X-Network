import { ethers } from 'ethers';
import type { TransactionData } from '../../types';
import { WalletError } from '../types/errors';
import { VaultSigner } from './vault-signer';
import type { WalletCoreV2 } from './wallet-core';
import { getActiveRpcUrl } from '../../../constants';
import { resolveFeeOverrides } from '../../../fees';

const ERC20_TRANSFER_ABI = [
  'function transfer(address to, uint256 amount) returns (bool)',
];
const NATIVE_GAS_LIMIT = 210_000n;
const ERC20_GAS_LIMIT = 500_000n;

export class TransactionServiceV2 {
  constructor(
    private readonly wallet: WalletCoreV2,
    private readonly rpcUrl: string,
    private readonly chainId: number,
    private readonly transferGasPrice: bigint = 5n,
  ) {}

  getProvider(): ethers.JsonRpcProvider {
    return new ethers.JsonRpcProvider(getActiveRpcUrl(), {
      chainId: this.chainId,
      name: 'paxeer-network',
    });
  }

  getSigner(address: string): VaultSigner {
    return new VaultSigner(
      this.wallet,
      ethers.getAddress(address),
      this.getProvider(),
      this.chainId,
    );
  }

  async sendTransaction(from: string, tx: TransactionData): Promise<string> {
    if (!ethers.isAddress(tx.to)) {
      throw WalletError.invalidInput('transaction.to', 'must be a valid address');
    }
    if (tx.tokenAddress && !ethers.isAddress(tx.tokenAddress)) {
      throw WalletError.invalidInput(
        'transaction.tokenAddress',
        'must be a valid address',
      );
    }

    const signer = this.getSigner(from);
    const fees = await this.getFeeOverrides();
    const nonce = tx.nonce ?? await this.getProvider().getTransactionCount(from, 'pending');

    if (tx.tokenAddress) {
      const contract = new ethers.Contract(
        tx.tokenAddress,
        ERC20_TRANSFER_ABI,
        signer,
      );
      const sent = await contract.transfer(
        tx.to,
        ethers.parseUnits(tx.value, tx.decimals ?? 18),
        {
          gasLimit: ERC20_GAS_LIMIT,
          nonce,
          ...fees,
        },
      );
      return sent.hash;
    }

    const sent = await signer.sendTransaction({
      to: tx.to,
      value: ethers.parseEther(tx.value),
      gasLimit: tx.gasLimit ? BigInt(tx.gasLimit) : NATIVE_GAS_LIMIT,
      chainId: this.chainId,
      nonce,
      ...fees,
    });
    return sent.hash;
  }

  private async getFeeOverrides(): Promise<{
    maxFeePerGas: bigint;
    maxPriorityFeePerGas: bigint;
  }> {
    const resolved = await resolveFeeOverrides(this.getProvider());
    return {
      maxFeePerGas: resolved.maxFeePerGas,
      maxPriorityFeePerGas: resolved.maxPriorityFeePerGas,
    };
  }
}
