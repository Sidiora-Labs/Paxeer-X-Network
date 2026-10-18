import type { WalletState } from "@paxeer-network/hooks";
import { AbstractChains, ChainNamespace } from "@paxeer-network/types";
import { WalletConnectType, WalletType } from "./types";

type StorageChain = {
  chainId: number;
  namespace: ChainNamespace;
};

export type AggregatedWalletSelection = {
  wallet: WalletState;
  walletType: WalletConnectType;
};

export const shouldPreservePrivyEvmStorageChain = (options: {
  connectorKey: string;
  storageChain?: StorageChain;
  nextChain: StorageChain;
  supportedEvmChainIds: ReadonlySet<number>;
}) => {
  const { connectorKey, storageChain, nextChain, supportedEvmChainIds } =
    options;

  return (
    connectorKey === WalletConnectType.PRIVY &&
    storageChain?.namespace === ChainNamespace.evm &&
    nextChain.namespace === ChainNamespace.evm &&
    supportedEvmChainIds.has(storageChain.chainId) &&
    storageChain.chainId !== nextChain.chainId
  );
};

export const selectAggregatedWallet = (options: {
  connectorKey: string;
  targetWalletType?: WalletType;
  storageChain?: StorageChain;
  privyWalletEVM: WalletState | null;
  privyWalletSOL: WalletState | null;
  privyWalletEVMReady: boolean;
  privyWalletSOLReady: boolean;
  walletEVM: WalletState | null;
  walletSOL: WalletState | null;
  walletAbstract: WalletState | null;
  isConnectedEVM: boolean;
  isConnectedSOL: boolean;
  isConnectedAbstract: boolean;
}): AggregatedWalletSelection | null => {
  const {
    connectorKey,
    targetWalletType,
    storageChain,
    privyWalletEVM,
    privyWalletSOL,
    privyWalletEVMReady,
    privyWalletSOLReady,
    walletEVM,
    walletSOL,
    walletAbstract,
    isConnectedEVM,
    isConnectedSOL,
    isConnectedAbstract,
  } = options;

  if (connectorKey === WalletConnectType.PRIVY) {
    if (targetWalletType === WalletType.EVM) {
      return privyWalletEVM
        ? { wallet: privyWalletEVM, walletType: WalletConnectType.PRIVY }
        : null;
    }

    if (targetWalletType === WalletType.SOL) {
      return privyWalletSOL
        ? { wallet: privyWalletSOL, walletType: WalletConnectType.PRIVY }
        : null;
    }

    if (targetWalletType === WalletType.ABSTRACT) {
      return null;
    }

    if (
      storageChain?.namespace === ChainNamespace.evm &&
      AbstractChains.has(storageChain.chainId)
    ) {
      // Privy embedded wallets don't support Abstract chains — stay
      // disconnected so the AGW connect flow can take over.
      return null;
    }

    if (storageChain?.namespace === ChainNamespace.evm) {
      if (privyWalletEVM) {
        return { wallet: privyWalletEVM, walletType: WalletConnectType.PRIVY };
      }
      if (!privyWalletEVMReady) {
        return null;
      }
      return privyWalletSOL
        ? { wallet: privyWalletSOL, walletType: WalletConnectType.PRIVY }
        : null;
    }

    if (storageChain?.namespace === ChainNamespace.solana) {
      if (privyWalletSOL) {
        return { wallet: privyWalletSOL, walletType: WalletConnectType.PRIVY };
      }
      if (!privyWalletSOLReady) {
        return null;
      }
      return privyWalletEVM
        ? { wallet: privyWalletEVM, walletType: WalletConnectType.PRIVY }
        : null;
    }

    const wallet = privyWalletEVM ?? privyWalletSOL;
    return wallet ? { wallet, walletType: WalletConnectType.PRIVY } : null;
  }

  if (connectorKey === WalletConnectType.EVM) {
    return isConnectedEVM && walletEVM
      ? { wallet: walletEVM, walletType: WalletConnectType.EVM }
      : null;
  }

  if (connectorKey === WalletConnectType.SOL) {
    return isConnectedSOL && walletSOL
      ? { wallet: walletSOL, walletType: WalletConnectType.SOL }
      : null;
  }

  if (connectorKey === WalletConnectType.ABSTRACT) {
    return isConnectedAbstract && walletAbstract
      ? { wallet: walletAbstract, walletType: WalletConnectType.ABSTRACT }
      : null;
  }

  if (
    storageChain?.namespace === ChainNamespace.evm &&
    AbstractChains.has(storageChain.chainId) &&
    isConnectedAbstract &&
    walletAbstract
  ) {
    return {
      wallet: walletAbstract,
      walletType: WalletConnectType.ABSTRACT,
    };
  }

  if (
    storageChain?.namespace === ChainNamespace.evm &&
    isConnectedEVM &&
    walletEVM
  ) {
    return { wallet: walletEVM, walletType: WalletConnectType.EVM };
  }

  if (
    storageChain?.namespace === ChainNamespace.solana &&
    isConnectedSOL &&
    walletSOL
  ) {
    return { wallet: walletSOL, walletType: WalletConnectType.SOL };
  }

  return null;
};
