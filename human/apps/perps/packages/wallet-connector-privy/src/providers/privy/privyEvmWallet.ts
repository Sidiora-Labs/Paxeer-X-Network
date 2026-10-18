import type { WalletState } from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";

type RawPrivyEvmWallet = {
  address: string;
  chainId?: string | number | null;
  getEthereumProvider: () => Promise<WalletState["provider"]>;
  switchChain: (chainId: number) => Promise<unknown>;
};

export interface PrivyEvmWalletState extends WalletState {
  chain: {
    id: number;
    namespace: ChainNamespace;
  };
}

export const parsePrivyEvmChainId = (
  chainId?: string | number | null,
): number | null => {
  if (typeof chainId === "number") {
    return Number.isSafeInteger(chainId) && chainId > 0 ? chainId : null;
  }

  if (typeof chainId !== "string") {
    return null;
  }

  const value = chainId.trim();
  const match = /^(?:eip155:)?(\d+)$/.exec(value);
  if (!match) {
    return null;
  }

  const parsed = Number(match[1]);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
};

export const buildPrivyEvmWallet = async (
  wallet: RawPrivyEvmWallet,
  targetChainId?: number,
): Promise<PrivyEvmWalletState> => {
  let chainId = parsePrivyEvmChainId(wallet.chainId);

  if (targetChainId && chainId !== targetChainId) {
    try {
      await wallet.switchChain(targetChainId);
      chainId = targetChainId;
    } catch (error) {
      // Preserve the real chain so the existing wrong-network flow can recover.
      console.warn(
        `Failed to switch Privy wallet to chain ${targetChainId}`,
        error,
      );
    }
  }

  if (!chainId) {
    throw new Error(`Invalid Privy wallet chain ID: ${String(wallet.chainId)}`);
  }

  const provider = await wallet.getEthereumProvider();
  const chain = {
    id: chainId,
    namespace: ChainNamespace.evm,
  };

  return {
    label: "privy",
    icon: "",
    provider,
    accounts: [{ address: wallet.address }],
    chains: [chain],
    chain,
  };
};
