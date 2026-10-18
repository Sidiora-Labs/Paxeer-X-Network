import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  useLogin,
  usePrivy,
  useSolanaWallets,
  useWallets,
} from "@privy-io/react-auth";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";
import {
  useEventEmitter,
  useLocalStorage,
  useStorageChain,
  useTrack,
  WalletState,
} from "@paxeer-network/hooks";
import {
  AbstractChains,
  ChainNamespace,
  TrackerEventName,
} from "@paxeer-network/types";
import {
  getWalletConnectErrorMessage,
  isWalletConnectCancellation,
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_OAUTH_RETURNED,
  WALLET_CONNECT_PROVIDER_CANCEL,
} from "../../connectEvents";
import {
  clearOAuthConnectIntent,
  isRedirectLoginMethod,
  markOAuthConnectIntent,
  markOAuthConnectIntentReturned,
} from "../../oauthConnectIntent";
import { useWalletConnectorPrivy } from "../../provider";
import { ConnectProps, SolanaChainsMap, WalletConnectType } from "../../types";
import { buildPrivyEvmWallet } from "./privyEvmWallet";

interface WalletStatePrivy extends WalletState {
  chain: {
    id: number;
    namespace: ChainNamespace;
  };
}

interface PrivyWalletContextValue {
  connect: (params?: ConnectProps) => void;
  walletEVM: WalletStatePrivy | null;
  walletSOL: WalletStatePrivy | null;
  walletEVMReady: boolean;
  walletSOLReady: boolean;
  allWalletsEVM: WalletStatePrivy[];
  allWalletsSOL: WalletStatePrivy[];
  isConnected: boolean;
  switchChain: (chainId: number) => Promise<any>;
  linkedAccount: { type: string; address: string | null } | null;
  exportWallet: (namespace: ChainNamespace, address?: string) => any;
  createEvmWallet: any;
  createSolanaWallet: any;
  selectWallet: (namespace: ChainNamespace, address: string) => void;
  disconnect: () => Promise<void>;
}

const defaultPrivyWalletContextValue: PrivyWalletContextValue = {
  connect: () => {},
  walletEVM: null,
  walletSOL: null,
  walletEVMReady: true,
  walletSOLReady: true,
  allWalletsEVM: [],
  allWalletsSOL: [],
  isConnected: false,
  switchChain: () => Promise.reject(new Error("Privy is disabled")),
  linkedAccount: null,
  exportWallet: () => Promise.reject(new Error("Privy is disabled")),
  createEvmWallet: () => Promise.reject(new Error("Privy is disabled")),
  createSolanaWallet: () => Promise.reject(new Error("Privy is disabled")),
  selectWallet: () => {},
  disconnect: () => Promise.resolve(),
};

const PrivyWalletContext = createContext<PrivyWalletContextValue | null>(null);

export const PrivyWalletProvider: React.FC<{
  children: React.ReactNode;
  disabled: boolean;
}> = ({ children, disabled }) => {
  if (disabled) {
    return (
      <PrivyWalletContext.Provider value={defaultPrivyWalletContextValue}>
        {children}
      </PrivyWalletContext.Provider>
    );
  }

  return <PrivyWalletProviderInner>{children}</PrivyWalletProviderInner>;
};

const PrivyWalletProviderInner: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const {
    network,
    mainnetChains,
    testnetChains,
    solanaInfo,
    setSolanaInfo,
    connectorWalletType,
  } = useWalletConnectorPrivy();
  const {
    logout,
    ready,
    authenticated,
    user,
    exportWallet: exportEvmWallet,
    createWallet: createEvmWallet,
  } = usePrivy();
  const ee = useEventEmitter();
  const { wallets: walletsEVM, ready: evmWalletsSourceReady } = useWallets();
  const connectedRef = useRef(false);
  const manualLoginRef = useRef(false);

  const finishManualLogin = useCallback(() => {
    manualLoginRef.current = false;
  }, []);

  const { login } = useLogin({
    onComplete: () => {
      const intent = markOAuthConnectIntentReturned();
      if (intent) {
        ee.emit(WALLET_CONNECT_OAUTH_RETURNED, { intentId: intent.id });
      }
      finishManualLogin();
    },
    onError: (error) => {
      const wasManual = manualLoginRef.current;
      clearOAuthConnectIntent();
      finishManualLogin();
      if (!wasManual) {
        return;
      }

      if (isWalletConnectCancellation(error)) {
        ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
          walletType: WalletConnectType.PRIVY,
        });
        return;
      }

      ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.PRIVY,
        message: getWalletConnectErrorMessage(
          error,
          "Failed to log in with Privy.",
        ),
      });
    },
  });

  const {
    ready: solanaReady,
    wallets: walletsSOL,
    createWallet: createSolanaWallet,
    exportWallet: exportSolanaWallet,
  } = useSolanaWallets();

  const [walletEVM, setWalletEVM] = useState<WalletStatePrivy | null>(null);
  const [walletSOL, setWalletSOL] = useState<WalletStatePrivy | null>(null);
  const [walletEVMReady, setWalletEVMReady] = useState(false);
  const [walletSOLReady, setWalletSOLReady] = useState(false);
  const [allWalletsEVM, setAllWalletsEVM] = useState<WalletStatePrivy[]>([]);
  const [allWalletsSOL, setAllWalletsSOL] = useState<WalletStatePrivy[]>([]);

  const [selectedEvmAddress, setSelectedEvmAddress] = useLocalStorage<string>(
    "privy_selected_evm_address",
    "",
  );
  const [selectedSolAddress, setSelectedSolAddress] = useLocalStorage<string>(
    "privy_selected_sol_address",
    "",
  );
  const { storageChain } = useStorageChain();

  const targetEvmChainId = useMemo(() => {
    if (
      storageChain?.namespace !== ChainNamespace.evm ||
      AbstractChains.has(storageChain.chainId)
    ) {
      return undefined;
    }

    const activeChains = network === "mainnet" ? mainnetChains : testnetChains;
    return activeChains.some((chain) => chain.id === storageChain.chainId)
      ? storageChain.chainId
      : undefined;
  }, [mainnetChains, network, storageChain, testnetChains]);

  // Keep a ref map from address → raw Privy EVM wallet for switchChain
  const rawEvmWalletsRef = useRef<Map<string, any>>(new Map());

  const { track } = useTrack();

  const linkedAccount = useMemo(() => {
    const account = user?.linkedAccounts
      ?.filter((item) => item.type !== "wallet" && item.type !== "smart_wallet")
      .sort(
        (a, b) =>
          (b.latestVerifiedAt?.getTime() ?? 0) -
          (a.latestVerifiedAt?.getTime() ?? 0),
      )[0];

    if (!account) {
      return null;
    }

    let address = null;
    if (account.type === "email") {
      address = account.address;
    } else if (account.type === "twitter_oauth") {
      address = `@${account.username}`;
    } else if (account.type === "google_oauth") {
      address = `@${account.name}`;
    } else if (account.type === "telegram") {
      address = `@${account.username}`;
    }

    return {
      type: account.type,
      address,
    };
  }, [user]);

  const switchChain = (chainId: number) => {
    // Use the currently selected EVM wallet's raw Privy object
    const selectedAddress = walletEVM?.accounts[0]?.address;
    const rawWallet = selectedAddress
      ? rawEvmWalletsRef.current.get(selectedAddress)
      : undefined;
    if (rawWallet) {
      return rawWallet.switchChain(chainId);
    }
    // Fallback: try first embedded wallet
    const fallback =
      walletsEVM.find((w) => w.connectorType === "embedded") ?? walletsEVM[0];
    if (fallback) {
      return fallback.switchChain(chainId);
    }
    return Promise.reject("no wallet");
  };

  const connect = useCallback(
    (params?: ConnectProps) => {
      manualLoginRef.current = true;
      const isOAuthRedirect = isRedirectLoginMethod(params?.extraType);
      clearOAuthConnectIntent();

      try {
        if (params?.extraType) {
          if (isOAuthRedirect) {
            markOAuthConnectIntent(params.extraType);
          }
          login({ loginMethods: [params.extraType as any] });
          return;
        }
        login();
      } catch (error) {
        if (isOAuthRedirect) {
          clearOAuthConnectIntent();
        }
        finishManualLogin();
        if (isWalletConnectCancellation(error)) {
          ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
            walletType: WalletConnectType.PRIVY,
          });
          return;
        }

        ee.emit(WALLET_CONNECT_ERROR, {
          walletType: WalletConnectType.PRIVY,
          message: getWalletConnectErrorMessage(
            error,
            "Failed to log in with Privy.",
          ),
        });
      }
    },
    [ee, finishManualLogin, login],
  );

  const disconnect = () => {
    return logout();
  };

  const exportWallet = (namespace: ChainNamespace, address?: string) => {
    if (namespace === ChainNamespace.evm) {
      track(TrackerEventName.clickExportPrivateKey, {
        type: "evm",
      });
      const addr = address ?? walletEVM?.accounts[0]?.address;
      return exportEvmWallet(addr ? { address: addr } : undefined);
    } else if (namespace === ChainNamespace.solana) {
      track(TrackerEventName.clickExportPrivateKey, {
        type: "solana",
      });
      const addr = address ?? walletSOL?.accounts[0]?.address;
      return exportSolanaWallet(addr ? { address: addr } : undefined);
    }
    return Promise.reject("no namespace");
  };

  const selectWallet = useCallback(
    (namespace: ChainNamespace, address: string) => {
      if (namespace === ChainNamespace.evm) {
        const found = allWalletsEVM.find(
          (w) => w.accounts[0]?.address === address,
        );
        if (found) {
          setWalletEVM(found);
          setSelectedEvmAddress(address);
        }
      } else if (namespace === ChainNamespace.solana) {
        const found = allWalletsSOL.find(
          (w) => w.accounts[0]?.address === address,
        );
        if (found) {
          setWalletSOL(found);
          setSelectedSolAddress(address);
        }
      }
    },
    [
      allWalletsEVM,
      allWalletsSOL,
      setSelectedEvmAddress,
      setSelectedSolAddress,
    ],
  );

  const isConnected = useMemo(() => {
    if (ready && authenticated) {
      return true;
    }
    return false;
  }, [ready, authenticated]);

  // Build all EVM wallets
  useEffect(() => {
    if (!authenticated || !evmWalletsSourceReady) {
      setAllWalletsEVM([]);
      setWalletEVM(null);
      setWalletEVMReady(false);
      return;
    }
    if (!walletsEVM || !walletsEVM[0]) {
      setAllWalletsEVM([]);
      setWalletEVM(null);
      setWalletEVMReady(true);
      return;
    }

    setWalletEVMReady(false);

    const embeddedWallets = walletsEVM.filter(
      (w) => w.connectorType === "embedded",
    );
    const wallets =
      embeddedWallets.length > 0 ? embeddedWallets : [walletsEVM[0]];

    // Update raw wallet ref
    const newMap = new Map<string, any>();
    for (const w of wallets) {
      newMap.set(w.address, w);
    }
    rawEvmWalletsRef.current = newMap;

    let cancelled = false;

    Promise.allSettled(
      wallets.map((wallet) => buildPrivyEvmWallet(wallet, targetEvmChainId)),
    ).then((results) => {
      if (cancelled) {
        return;
      }

      const builtWallets = results.flatMap((result) =>
        result.status === "fulfilled" ? [result.value] : [],
      );
      const failures = results.filter(
        (result): result is PromiseRejectedResult =>
          result.status === "rejected",
      );

      failures.forEach((result) => {
        console.warn("Failed to build an EVM wallet", result.reason);
      });

      if (builtWallets.length === 0) {
        setAllWalletsEVM([]);
        setWalletEVM(null);
        setWalletEVMReady(true);

        // A Privy user may still have a usable Solana wallet. Do not cancel
        // that successful login because an EVM wallet failed to initialize.
        if (solanaReady && !walletsSOL?.[0]) {
          ee.emit(WALLET_CONNECT_ERROR, {
            walletType: WalletConnectType.PRIVY,
            message: getWalletConnectErrorMessage(
              failures[0]?.reason,
              "Failed to initialize the Privy wallet.",
            ),
          });
        }
        return;
      }

      setAllWalletsEVM(builtWallets);

      // Pick selected wallet: persisted address > first wallet
      const preferred = selectedEvmAddress
        ? builtWallets.find(
            (w) => w.accounts[0]?.address === selectedEvmAddress,
          )
        : undefined;
      setWalletEVM(preferred ?? builtWallets[0] ?? null);
      setWalletEVMReady(true);
    });

    return () => {
      cancelled = true;
    };
  }, [
    authenticated,
    ee,
    evmWalletsSourceReady,
    selectedEvmAddress,
    solanaReady,
    targetEvmChainId,
    walletsEVM,
    walletsSOL,
  ]);

  // Build all SOL wallets
  useEffect(() => {
    if (!authenticated) {
      setAllWalletsSOL([]);
      setWalletSOL(null);
      setWalletSOLReady(false);
      return;
    }
    if (!solanaReady) {
      setWalletSOLReady(false);
      return;
    }
    if (!walletsSOL || !walletsSOL[0]) {
      setAllWalletsSOL([]);
      setWalletSOL(null);
      setWalletSOLReady(true);
      return;
    }

    const embeddedWallets = walletsSOL.filter(
      (w: any) => w.connectorType === "embedded",
    );
    const wallets =
      embeddedWallets.length > 0 ? embeddedWallets : [walletsSOL[0]];

    const builtWallets: WalletStatePrivy[] = wallets.map(
      (w: any): WalletStatePrivy => ({
        label: "privy",
        icon: "",
        provider: {
          signMessage: w.signMessage,
          signTransaction: w.signTransaction,
          sendTransaction: w.sendTransaction,
          network: solanaInfo?.network ?? WalletAdapterNetwork.Devnet,
          rpcUrl: solanaInfo?.rpcUrl ?? undefined,
        },
        accounts: [{ address: w.address }],
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
      }),
    );

    setAllWalletsSOL(builtWallets);

    // Pick selected wallet: persisted address > first wallet
    const preferred = selectedSolAddress
      ? builtWallets.find((w) => w.accounts[0]?.address === selectedSolAddress)
      : undefined;
    setWalletSOL(preferred ?? builtWallets[0] ?? null);
    setWalletSOLReady(true);
  }, [walletsSOL, authenticated, solanaReady, network, solanaInfo]);

  useEffect(() => {
    if (isConnected && linkedAccount) {
      if (connectedRef.current) {
        return;
      }
      connectedRef.current = true;
      track(TrackerEventName.socialLoginSuccess, {
        type: linkedAccount.type,
        address: linkedAccount.address,
      });
    }
  }, [isConnected, linkedAccount, connectedRef]);

  const value = useMemo(
    () => ({
      connect,
      walletEVM,
      walletSOL,
      walletEVMReady,
      walletSOLReady,
      allWalletsEVM,
      allWalletsSOL,
      isConnected,
      disconnect,
      switchChain,
      linkedAccount,
      exportWallet,
      createEvmWallet,
      createSolanaWallet,
      selectWallet,
    }),
    [
      connect,
      walletEVM,
      walletSOL,
      walletEVMReady,
      walletSOLReady,
      allWalletsEVM,
      allWalletsSOL,
      isConnected,
      disconnect,
      switchChain,
      linkedAccount,
      exportWallet,
      createEvmWallet,
      createSolanaWallet,
      selectWallet,
    ],
  );

  return (
    <PrivyWalletContext.Provider value={value}>
      {children}
    </PrivyWalletContext.Provider>
  );
};

export function usePrivyWallet() {
  const context = useContext(PrivyWalletContext);
  if (!context) {
    throw new Error("usePrivyWallet must be used within a PrivyWalletProvider");
  }
  return context;
}
