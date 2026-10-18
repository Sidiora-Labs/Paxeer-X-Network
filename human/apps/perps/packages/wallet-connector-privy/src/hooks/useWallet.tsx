import { useCallback, useEffect, useMemo, useState } from "react";
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
  ConnectorKey,
  SolanaChains,
  defaultMainnetChains,
  defaultTestnetChains,
  TrackerEventName,
  WALLET_CHAIN_CHANGE_PENDING_RESULT,
  type WalletChainChangeResult,
} from "@paxeer-network/types";
import {
  getWalletConnectErrorMessage,
  isWalletConnectCancellation,
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
  WALLET_CONNECT_PROVIDER_START,
} from "../connectEvents";
import { useWalletConnectorPrivy } from "../provider";
import { useAbstractWallet } from "../providers/abstractWallet/abstractWalletProvider";
import { usePrivyWallet } from "../providers/privy/privyWalletProvider";
import { useSolanaWallet } from "../providers/solana/solanaWalletProvider";
import { useWagmiWallet } from "../providers/wagmi/wagmiWalletProvider";
import { ConnectProps, WalletConnectType, WalletType } from "../types";
import { getChainType } from "../util";
import {
  selectAggregatedWallet,
  shouldPreservePrivyEvmStorageChain,
} from "../walletSelection";

export function useWallet() {
  const { track } = useTrack();
  const ee = useEventEmitter();
  const { walletChainTypeConfig, mainnetChains, testnetChains, network } =
    useWalletConnectorPrivy();
  const [connectorKey, setConnectorKey] = useLocalStorage(ConnectorKey, "");
  const {
    disconnect: disconnectEVM,
    connect: connectEVM,
    wallet: walletEVM,
    setChain: setChainEvm,
    isConnected: isConnectedEVM,
  } = useWagmiWallet();
  const {
    disconnect: disconnectSOL,
    connect: connectSOL,
    wallet: walletSOL,
    isConnected: isConnectedSOL,
  } = useSolanaWallet();
  const {
    disconnect: disconnectPrivy,
    connect: connectPrivy,
    walletSOL: privyWalletSOL,
    walletEVM: privyWalletEVM,
    walletSOLReady: privyWalletSOLReady,
    walletEVMReady: privyWalletEVMReady,
    switchChain: setChainPrivy,
    isConnected: isConnectedPrivy,
  } = usePrivyWallet();

  const {
    connect: connectAbstract,
    wallet: walletAbstract,
    isConnected: isConnectedAbstract,
    disconnect: disconnectAbstract,
  } = useAbstractWallet();
  const [wallet, setWallet] = useState<WalletState | null>(null);
  const [namespace, setNamespace] = useState<ChainNamespace | null>(null);
  const [walletType, setWalletType] = useState<WalletConnectType | null>(null);
  const [pendingFallbackType, setPendingFallbackType] =
    useState<WalletConnectType | null>(null);
  const { storageChain, setStorageChain } = useStorageChain();
  const { setOpenConnectDrawer, targetWalletType, setTargetWalletType } =
    useWalletConnectorPrivy();

  const activeChains = network === "mainnet" ? mainnetChains : testnetChains;

  const supportedEvmChainIds = useMemo(() => {
    const ids = activeChains
      ?.map((c) => c.id)
      .filter((id) => !SolanaChains.has(id) && !AbstractChains.has(id));
    return new Set<number>(ids ?? []);
  }, [activeChains]);

  const preferredEvmChainId = useMemo(() => {
    const preferredOrder =
      network === "mainnet" ? defaultMainnetChains : defaultTestnetChains;
    return preferredOrder
      .map((c) => c.id)
      .find((id) => supportedEvmChainIds.has(id));
  }, [network, supportedEvmChainIds]);

  const defaultEvmChainId = useMemo(() => {
    for (const chain of activeChains ?? []) {
      if (!SolanaChains.has(chain.id) && !AbstractChains.has(chain.id)) {
        return chain.id;
      }
    }
    return undefined;
  }, [activeChains]);

  const fallbackEvmChainId = preferredEvmChainId ?? defaultEvmChainId;

  const connect = (params: ConnectProps) => {
    setTargetWalletType(undefined);
    ee.emit(WALLET_CONNECT_PROVIDER_START, {
      walletType: params.walletType,
      previousConnectorKey: connectorKey,
      previousChainId: storageChain?.chainId,
    });

    try {
      if (params.walletType === WalletConnectType.EVM) {
        setConnectorKey(WalletConnectType.EVM);
        connectEVM({ connector: params.connector! });
      }
      if (params.walletType === WalletConnectType.SOL) {
        setConnectorKey(WalletConnectType.SOL);
        connectSOL(params.walletAdapter!.name).catch((err: Error) => {
          if (isWalletConnectCancellation(err)) {
            ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
              walletType: WalletConnectType.SOL,
            });
            return;
          }

          if (err.name === "WalletAccountError") {
            return;
          }

          ee.emit(WALLET_CONNECT_ERROR, {
            walletType: WalletConnectType.SOL,
            message: getWalletConnectErrorMessage(
              err,
              "Please switch to a wallet with Solana address.",
            ),
          });
        });
      }
      if (params.walletType === WalletConnectType.PRIVY) {
        setConnectorKey(WalletConnectType.PRIVY);
        connectPrivy(params);
      }
      if (params.walletType === WalletConnectType.ABSTRACT) {
        setConnectorKey(WalletConnectType.ABSTRACT);
        connectAbstract();
      }
    } catch (e) {
      if (isWalletConnectCancellation(e)) {
        ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
          walletType: params.walletType,
        });
        return;
      }

      ee.emit(WALLET_CONNECT_ERROR, {
        walletType: params.walletType,
        message: getWalletConnectErrorMessage(
          e,
          "Failed to connect to the wallet.",
        ),
      });
    }
  };

  const isPrivy = useMemo(() => {
    if (connectorKey === WalletConnectType.PRIVY) {
      return true;
    }
    return false;
  }, [connectorKey]);

  const [connectedChain, setConnectedChain] = useState<any>();

  const setChain = async (chain: {
    chainId: number | string;
  }): Promise<WalletChainChangeResult> => {
    const chainType = getChainType(parseInt(chain.chainId as string));

    if (isPrivy) {
      if (chainType === WalletType.EVM) {
        if (!walletChainTypeConfig.hasEvm) {
          return Promise.reject(new Error("No evm wallet found"));
        }
        return setChainPrivy(parseInt(chain.chainId as string))
          .then((res) => {
            track(TrackerEventName.switchNetworkSuccess, {
              from_chain: storageChain?.chainId,
              to_chain: chain.chainId,
            });
            setStorageChain(parseInt(chain.chainId as string));

            return Promise.resolve(true);
          })
          .catch((e) => {
            console.log("xxxx switch network failed", {
              error: e,
            });
            return Promise.reject(e);
          });
      }
      // current privy not support abstract chain
      if (chainType === WalletType.ABSTRACT) {
        setOpenConnectDrawer(true);
        setTargetWalletType(WalletType.ABSTRACT);
        return WALLET_CHAIN_CHANGE_PENDING_RESULT;
      }

      if (chainType === WalletType.SOL) {
        if (!walletChainTypeConfig.hasSol) {
          return Promise.reject(new Error("No solana wallet found"));
        }
        if (privyWalletSOL) {
          setStorageChain(parseInt(chain.chainId as string));
          return Promise.resolve(true);
        } else {
          setOpenConnectDrawer(true);
          setTargetWalletType(WalletType.SOL);
          return WALLET_CHAIN_CHANGE_PENDING_RESULT;
        }
      }
    } else {
      // if current namespace is evm, switch chain
      if (chainType === WalletType.EVM) {
        if (isConnectedEVM && walletEVM) {
          await setChainEvm(parseInt(chain.chainId as string));
          setConnectorKey(WalletConnectType.EVM);
          setStorageChain(parseInt(chain.chainId as string));
          track(TrackerEventName.switchNetworkSuccess, {
            from_chain: storageChain?.chainId,
            to_chain: chain.chainId,
          });
        } else {
          setOpenConnectDrawer(true);
          setTargetWalletType(WalletType.EVM);
          return WALLET_CHAIN_CHANGE_PENDING_RESULT;
        }

        return Promise.resolve(true);
      }
      if (chainType === WalletType.SOL) {
        if (isConnectedSOL && walletSOL) {
          setConnectorKey(WalletConnectType.SOL);
          setStorageChain(parseInt(chain.chainId as string));
          return Promise.resolve(true);
        } else {
          setOpenConnectDrawer(true);
          setTargetWalletType(WalletType.SOL);
          return WALLET_CHAIN_CHANGE_PENDING_RESULT;
        }
      }
      if (chainType === WalletType.ABSTRACT) {
        if (isConnectedAbstract && walletAbstract) {
          setConnectorKey(WalletConnectType.ABSTRACT);
          setStorageChain(parseInt(chain.chainId as string));
          return Promise.resolve(true);
        } else {
          setOpenConnectDrawer(true);
          // TODO need send abstract type
          setTargetWalletType(WalletType.ABSTRACT);
          return WALLET_CHAIN_CHANGE_PENDING_RESULT;
        }
      }
    }
  };

  const switchWallet = (
    walletType: WalletType,
    selectedWallet?: WalletState,
  ) => {
    const fromWallet = wallet?.accounts[0].address;
    let toWallet: string | undefined;
    const selectedChain = selectedWallet?.chains[0];

    if (selectedWallet && selectedChain) {
      const selectedChainId = Number(selectedChain.id);
      const nextChainId =
        isPrivy &&
        walletType === WalletType.EVM &&
        !supportedEvmChainIds.has(selectedChainId)
          ? fallbackEvmChainId
          : selectedChainId;
      if (typeof nextChainId === "number") {
        setStorageChain(nextChainId);
      }
      toWallet = selectedWallet.accounts[0]?.address;
      track(TrackerEventName.clickSwitchWallet, {
        fromWallet,
        toWallet,
      });
      return;
    }

    if (isPrivy) {
      switch (walletType) {
        case WalletType.EVM:
          if (privyWalletEVM) {
            const desired = privyWalletEVM.chain.id;
            const nextChainId = supportedEvmChainIds.has(desired)
              ? desired
              : fallbackEvmChainId;
            if (typeof nextChainId === "number") {
              setStorageChain(nextChainId);
            }
            toWallet = privyWalletEVM.accounts[0].address;
          }
          break;
        case WalletType.SOL:
          if (privyWalletSOL) {
            setStorageChain(privyWalletSOL.chain.id);
            toWallet = privyWalletSOL.accounts[0].address;
          }
          break;
        case WalletType.ABSTRACT:
          // privy don't support abstract wallet
          break;
      }
    } else {
      switch (walletType) {
        case WalletType.EVM:
          if (walletEVM) {
            setConnectorKey(WalletConnectType.EVM);
            setStorageChain(walletEVM.chain.id);
            toWallet = walletEVM.accounts[0].address;
          }
          break;
        case WalletType.SOL:
          if (walletSOL) {
            setConnectorKey(WalletConnectType.SOL);
            setStorageChain(walletSOL.chain.id);
            toWallet = walletSOL.accounts[0].address;
          }
          break;
        case WalletType.ABSTRACT:
          if (walletAbstract) {
            setConnectorKey(WalletConnectType.ABSTRACT);
            setStorageChain(walletAbstract.chain!.id);
            toWallet = walletAbstract.accounts[0].address;
          }
          break;
      }
    }
    track(TrackerEventName.clickSwitchWallet, {
      fromWallet,
      toWallet,
    });
  };

  const getFallbackWallet = useCallback(
    (disconnectedType: WalletConnectType) => {
      const preferredPrivyWallet =
        storageChain?.namespace === ChainNamespace.solana
          ? (privyWalletSOL ?? privyWalletEVM)
          : (privyWalletEVM ?? privyWalletSOL);
      const candidates = [
        {
          walletType: WalletConnectType.EVM,
          wallet: isConnectedEVM ? walletEVM : null,
        },
        {
          walletType: WalletConnectType.SOL,
          wallet: isConnectedSOL ? walletSOL : null,
        },
        {
          walletType: WalletConnectType.ABSTRACT,
          wallet: isConnectedAbstract ? walletAbstract : null,
        },
        {
          walletType: WalletConnectType.PRIVY,
          wallet: isConnectedPrivy ? preferredPrivyWallet : null,
        },
      ];

      return candidates.find(
        (candidate) =>
          candidate.walletType !== disconnectedType && candidate.wallet,
      );
    },
    [
      isConnectedAbstract,
      isConnectedEVM,
      isConnectedPrivy,
      isConnectedSOL,
      privyWalletEVM,
      privyWalletSOL,
      storageChain?.namespace,
      walletAbstract,
      walletEVM,
      walletSOL,
    ],
  );

  const hasInitializingFallback = useCallback(
    (disconnectedType: WalletConnectType) =>
      (disconnectedType !== WalletConnectType.EVM &&
        isConnectedEVM &&
        !walletEVM) ||
      (disconnectedType !== WalletConnectType.SOL &&
        isConnectedSOL &&
        !walletSOL) ||
      (disconnectedType !== WalletConnectType.ABSTRACT &&
        isConnectedAbstract &&
        !walletAbstract) ||
      (disconnectedType !== WalletConnectType.PRIVY &&
        isConnectedPrivy &&
        (!privyWalletEVMReady || !privyWalletSOLReady)),
    [
      isConnectedAbstract,
      isConnectedEVM,
      isConnectedPrivy,
      isConnectedSOL,
      privyWalletEVMReady,
      privyWalletSOLReady,
      walletAbstract,
      walletEVM,
      walletSOL,
    ],
  );

  const activateFallbackWallet = useCallback(
    (disconnectedType: WalletConnectType, allowInactive = false) => {
      if (
        !allowInactive &&
        connectorKey !== disconnectedType &&
        walletType !== disconnectedType
      ) {
        return;
      }

      const fallback = getFallbackWallet(disconnectedType);
      const fallbackChain = fallback?.wallet?.chains[0];
      if (!fallback || !fallbackChain) {
        setConnectorKey("");
        setPendingFallbackType(
          hasInitializingFallback(disconnectedType) ? disconnectedType : null,
        );
        return;
      }

      const fallbackChainId = Number(fallbackChain.id);
      const nextChainId =
        fallback.walletType === WalletConnectType.PRIVY &&
        fallbackChain.namespace === ChainNamespace.evm &&
        !supportedEvmChainIds.has(fallbackChainId)
          ? (fallbackEvmChainId ?? fallbackChainId)
          : fallbackChainId;
      setPendingFallbackType(null);
      setConnectorKey(fallback.walletType);
      if (typeof nextChainId === "number") {
        setStorageChain(nextChainId);
      }
    },
    [
      connectorKey,
      fallbackEvmChainId,
      getFallbackWallet,
      hasInitializingFallback,
      setConnectorKey,
      setStorageChain,
      supportedEvmChainIds,
      walletType,
    ],
  );

  useEffect(() => {
    if (!pendingFallbackType) {
      return;
    }

    if (connectorKey) {
      setPendingFallbackType(null);
      return;
    }

    activateFallbackWallet(pendingFallbackType, true);
  }, [activateFallbackWallet, connectorKey, pendingFallbackType]);

  const disconnect = async (disconnectedType: WalletConnectType) => {
    let result: unknown;
    switch (disconnectedType) {
      case WalletConnectType.PRIVY:
        result = await disconnectPrivy();
        break;
      case WalletConnectType.EVM:
        result = await disconnectEVM();
        break;
      case WalletConnectType.SOL:
        result = await disconnectSOL();
        break;
      case WalletConnectType.ABSTRACT:
        result = await disconnectAbstract();
        break;
    }

    activateFallbackWallet(disconnectedType);
    return result;
  };

  const restoreConnectorState = useCallback(
    (previousConnectorKey?: string, previousChainId?: number) => {
      setConnectorKey(previousConnectorKey ?? "");
      if (typeof previousChainId === "number") {
        setStorageChain(previousChainId);
      }
    },
    [setConnectorKey, setStorageChain],
  );

  const onDisconnect = (params: any): Promise<any> => {
    const disconnectedType = params?.walletType ?? walletType;
    if (!disconnectedType) {
      throw new Error("No wallet type found");
    }
    return new Promise((resolve, reject) => {
      disconnect(disconnectedType)
        .then(() => {
          resolve(true);
        })
        .catch((e) => {
          reject(e);
        });
    });
  };

  const setNullWalletStatus = () => {
    setWallet(null);
    setWalletType(null);
    setConnectedChain(null);
    setNamespace(null);
  };

  useEffect(() => {
    const selection = selectAggregatedWallet({
      connectorKey,
      targetWalletType,
      storageChain,
      privyWalletEVM,
      privyWalletSOL,
      privyWalletEVMReady,
      privyWalletSOLReady,
      walletEVM: walletEVM ?? null,
      walletSOL: walletSOL ?? null,
      walletAbstract,
      isConnectedEVM,
      isConnectedSOL,
      isConnectedAbstract,
    });

    const nextChain = selection?.wallet.chains[0];
    if (!selection || !nextChain) {
      setNullWalletStatus();
      return;
    }

    setWallet(selection.wallet);
    setWalletType(selection.walletType);
    setConnectedChain(nextChain);
    setNamespace(nextChain.namespace);

    const selectedWalletType =
      nextChain.namespace === ChainNamespace.solana
        ? WalletType.SOL
        : WalletType.EVM;
    if (
      connectorKey === WalletConnectType.PRIVY &&
      targetWalletType === selectedWalletType
    ) {
      setTargetWalletType(undefined);
    }

    const preservePrivyEvmStorageChain = shouldPreservePrivyEvmStorageChain({
      connectorKey,
      storageChain,
      nextChain: {
        chainId: Number(nextChain.id),
        namespace: nextChain.namespace,
      },
      supportedEvmChainIds,
    });
    if (
      storageChain?.chainId !== nextChain.id &&
      !preservePrivyEvmStorageChain
    ) {
      setStorageChain(Number(nextChain.id));
    }
  }, [
    connectorKey,
    isConnectedAbstract,
    isConnectedEVM,
    isConnectedSOL,
    privyWalletEVM,
    privyWalletEVMReady,
    privyWalletSOL,
    privyWalletSOLReady,
    setStorageChain,
    storageChain,
    supportedEvmChainIds,
    targetWalletType,
    setTargetWalletType,
    walletAbstract,
    walletEVM,
    walletSOL,
  ]);

  return {
    connect,
    wallet,
    walletType,
    connectedChain,
    setChain,
    namespace,
    switchWallet,
    disconnect,
    onDisconnect,
    restoreConnectorState,
  };
}
