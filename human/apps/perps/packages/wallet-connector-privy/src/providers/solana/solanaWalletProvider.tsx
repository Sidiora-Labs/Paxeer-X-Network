import React, { createContext, useContext, useEffect, useMemo } from "react";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";
import { useWallet } from "@solana/wallet-adapter-react";
import { useStorageLedgerAddress } from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import { useWalletConnectorPrivy } from "../../provider";
import { useSolanaWalletStore } from "../../stores/solanaWalletStore";
import { SolanaChainsMap } from "../../types";

interface SolanaWalletContextValue {
  wallets: any[];
  connectedChain: { id: number; namespace: ChainNamespace } | null;
  connect: (walletName: string) => Promise<any>;
  wallet: any;
  disconnect: () => void;
  isConnected: boolean;
}

const disabledSolanaWalletValue: SolanaWalletContextValue = {
  wallets: [],
  connectedChain: null,
  connect: () =>
    Promise.reject(new Error("Solana wallet connector is disabled")),
  wallet: null,
  disconnect: () => Promise.resolve(),
  isConnected: false,
};

const SolanaWalletContext = createContext<SolanaWalletContextValue | null>(
  null,
);

export const SolanaWalletProvider: React.FC<{
  children: React.ReactNode;
  disabled: boolean;
}> = ({ children, disabled }) => {
  useEffect(() => {
    if (disabled) {
      useSolanaWalletStore.setState({
        wallet: null,
        isConnecting: false,
        error: null,
        isManual: false,
        walletMethods: null,
        pendingWalletName: null,
      });
    }
  }, [disabled]);

  if (disabled) {
    return (
      <SolanaWalletContext.Provider value={disabledSolanaWalletValue}>
        {children}
      </SolanaWalletContext.Provider>
    );
  }

  return <EnabledSolanaWalletProvider>{children}</EnabledSolanaWalletProvider>;
};

const EnabledSolanaWalletProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const { syncLedgerAddress } = useStorageLedgerAddress();
  const { network, solanaInfo } = useWalletConnectorPrivy();

  const {
    wallets,
    select,
    connect: connectSolana,
    wallet: walletSolana,
    publicKey,
    connecting,
    signMessage,
    signTransaction,
    sendTransaction,
    disconnect: disconnectSolana,
  } = useWallet();

  const {
    wallet,
    connect,
    disconnect: disconnectWallet,
    setWallet,
    setWalletMethods,
    isManual,
  } = useSolanaWalletStore();

  useEffect(() => {
    if (!walletSolana || isManual) return;

    if (!publicKey) {
      return;
    }

    const newWallet = {
      label: walletSolana.adapter.name,
      icon: "",
      provider: {
        rpcUrl: solanaInfo?.rpcUrl ?? null,
        network: solanaInfo?.network ?? WalletAdapterNetwork.Devnet,
        signMessage,
        signTransaction,
        sendTransaction,
      },
      accounts: [
        {
          address: publicKey.toBase58(),
        },
      ],
      chains: [
        {
          id: SolanaChainsMap.get(network)!,
          namespace: ChainNamespace.solana,
        },
      ],
      chain: {
        id: SolanaChainsMap.get(network)!,
        namespace: ChainNamespace.solana,
      },
    };

    setWallet(newWallet);
  }, [
    publicKey,
    walletSolana,
    signMessage,
    signTransaction,
    sendTransaction,
    solanaInfo,
    isManual,
  ]);

  const ledgerAddress = publicKey?.toBase58();
  const ledgerAdapterName = walletSolana?.adapter.name;

  useEffect(() => {
    if (!ledgerAddress || !ledgerAdapterName) {
      return;
    }

    syncLedgerAddress(ledgerAddress, ledgerAdapterName);
  }, [ledgerAddress, ledgerAdapterName, syncLedgerAddress]);

  useEffect(() => {
    setWalletMethods({
      select,
      connectSolana,
      walletSolana,
      publicKey,
      connecting,
      signMessage,
      signTransaction,
      sendTransaction,
      disconnectSolana,
      network,
      solanaInfo,
      syncLedgerAddress,
    });
  }, [
    select,
    connectSolana,
    walletSolana,
    publicKey,
    connecting,
    signMessage,
    signTransaction,
    sendTransaction,
    disconnectSolana,
    network,
    solanaInfo,
    syncLedgerAddress,
  ]);

  const dedupedWallets = useMemo(() => {
    const seen = new Set<string>();
    return wallets.filter((w: any) => {
      const name = w?.adapter?.name?.toLowerCase?.();
      if (!name || seen.has(name)) return !name;
      seen.add(name);
      return true;
    });
  }, [wallets]);

  const value = useMemo(
    () => ({
      wallets: dedupedWallets,
      connectedChain: publicKey
        ? {
            id: SolanaChainsMap.get(network)!,
            namespace: ChainNamespace.solana,
          }
        : null,
      connect,
      wallet,
      disconnect: disconnectWallet,
      isConnected: !!publicKey,
    }),
    [dedupedWallets, publicKey, wallet, network],
  );

  return (
    <SolanaWalletContext.Provider value={value}>
      {children}
    </SolanaWalletContext.Provider>
  );
};

export function useSolanaWallet() {
  const context = useContext(SolanaWalletContext);
  if (!context) {
    throw new Error(
      "useSolanaWallet must be used within a SolanaWalletProvider",
    );
  }
  return context;
}
