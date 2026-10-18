import { useMemo, useCallback, useState, useEffect } from "react";
import { useConfig, useStorageChain } from "@paxeer-network/hooks";
import {
  useChains,
  useWalletConnector,
  useLocalStorage,
} from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import {
  isWalletChainChangePendingResult,
  NetworkId,
  type WalletChainChangeState,
} from "@paxeer-network/types";
import { usePaxeerNetworkTheme } from "@paxeer-network/ui";
import { ChainType, TChainItem } from "./type";

const KEY = "paxeerNetwork_selected_chains";
const MAX_RECENT_CHAINS = 6;

export type UseChainSelectorScriptReturn = ReturnType<
  typeof useChainSelectorScript
>;

export type ChainSelectorResult = {
  result: true;
  wrongNetwork: boolean;
  chainId: number;
};

export type UseChainSelectorScriptOptions = {
  networkId?: NetworkId;
  bridgeLessOnly?: boolean;
  close?: () => void;
  resolve?: (result: ChainSelectorResult) => void;
  reject?: (reason?: unknown) => void;
  onChainChangeBefore?: (
    chainId: number,
    state: {
      isTestnet: boolean;
    },
  ) => void;
  onChainChangeAfter?: (chainId: number, state: WalletChainChangeState) => void;
};

export const useChainSelectorScript = (
  options: UseChainSelectorScriptOptions,
) => {
  const { networkId, bridgeLessOnly } = options || {};
  const { setStorageChain } = useStorageChain();

  const config = useConfig();
  const resolvedNetworkId = networkId ?? config.get<NetworkId>("networkId");
  const [_chains, { checkChainSupport }] = useChains();
  const { setChain, connectedChain } = useWalletConnector();

  const { onChainChanged, currentChainId, setCurrentChainId, wrongNetwork } =
    useAppContext();

  const [selectChainId, setSelectChainId] = useState<number | undefined>(
    currentChainId,
  );

  const { getComponentTheme } = usePaxeerNetworkTheme();

  const chains = useMemo(() => {
    const bridgeLessChains = bridgeLessOnly
      ? _chains.mainnet.filter((chain) => chain.network_infos.bridgeless)
      : _chains.mainnet;

    return {
      mainnet: bridgeLessChains.map((chain) => ({
        name: chain.network_infos.name,
        id: chain.network_infos.chain_id,
        lowestFee: chain.network_infos.bridgeless,
        isTestnet: false,
      })),
      testnet: _chains.testnet.map((chain) => ({
        name: chain.network_infos.name,
        id: chain.network_infos.chain_id,
        lowestFee: chain.network_infos.bridgeless,
        isTestnet: true,
      })),
    };
  }, [_chains, bridgeLessOnly]);

  const showTestnet = useMemo(() => {
    const chainSelectorOverrides = getComponentTheme("chainSelector", {
      showTestnet: true,
    });

    if (
      chainSelectorOverrides.showTestnet === false ||
      !chains.testnet.length
    ) {
      return false;
    }

    return true;
  }, [chains.testnet]);

  const showMainnet = chains.mainnet.length > 0;

  const { recentChains, saveRecentChain } = useRecentChains(chains);

  const onChainChange = async (chain: TChainItem) => {
    if (connectedChain) {
      const result = await setChain({ chainId: chain.id });

      if (isWalletChainChangePendingResult(result)) return result;
      if (!result) return result;

      return {
        result,
        wrongNetwork: !checkChainSupport(chain.id, resolvedNetworkId),
        chainId: chain.id,
      };
    }

    setStorageChain(chain.id);

    setCurrentChainId(chain.id);
    return {
      result: true as const,
      wrongNetwork: false,
      chainId: chain.id,
    };
    // return Promise.reject("No connected chain");
  };

  const changedCallback = (
    chain: TChainItem,
    isWalletConnected: boolean,
    isWalletConnectionPending = false,
  ) => {
    const params = {
      isTestnet: chain.isTestnet,
      isWalletConnected,
      isWalletConnectionPending,
    };
    options.onChainChangeAfter?.(chain.id, params);
    onChainChanged?.(chain.id, params);
  };

  const onChainClick = async (chain: TChainItem) => {
    setSelectChainId(chain.id);
    options.onChainChangeBefore?.(chain.id, { isTestnet: chain.isTestnet });

    let complete: Awaited<ReturnType<typeof onChainChange>>;
    try {
      complete = await onChainChange(chain);
    } catch (err) {
      setSelectChainId(undefined);
      changedCallback(chain, false);
      return;
    }

    if (isWalletChainChangePendingResult(complete)) {
      setSelectChainId(undefined);
      options.reject?.(complete);
      options.close?.();
      saveRecentChain(chain);
      changedCallback(chain, false, true);
    } else if (complete) {
      options.resolve?.(complete);
      options.close?.();
      saveRecentChain(chain);
      changedCallback(chain, true);
    } else {
      setSelectChainId(undefined);
      changedCallback(chain, false);
    }
  };

  const { selectedTab, onTabChange } = useChainTab(
    chains,
    currentChainId,
    wrongNetwork,
    showMainnet,
    showTestnet,
  );

  return {
    recentChains,
    chains,
    selectChainId,
    onChainClick,
    selectedTab,
    onTabChange,
    showMainnet,
    showTestnet,
  };
};

function useChainTab(
  chains: Record<NetworkId, TChainItem[]>,
  currentChainId?: number,
  wrongNetwork?: boolean,
  showMainnet?: boolean,
  showTestnet?: boolean,
) {
  const [selectedTab, setSelectedTab] = useState<ChainType>(ChainType.Mainnet);

  const onTabChange = (tab: ChainType) => {
    setSelectedTab(tab);
  };

  useEffect(() => {
    if (!showMainnet) {
      if (showTestnet) {
        setSelectedTab(ChainType.Testnet);
      }
      return;
    }

    if (!showTestnet) {
      setSelectedTab(ChainType.Mainnet);
      return;
    }

    if (currentChainId) {
      const isMainnet = chains.mainnet?.some(
        (chain) => chain.id === currentChainId,
      );
      if (isMainnet) {
        setSelectedTab(wrongNetwork ? ChainType.Testnet : ChainType.Mainnet);
        return;
      }

      const isTestnet = chains.testnet?.some(
        (chain) => chain.id === currentChainId,
      );
      if (isTestnet) {
        setSelectedTab(wrongNetwork ? ChainType.Mainnet : ChainType.Testnet);
        return;
      }
    }
  }, [currentChainId, chains, wrongNetwork, showMainnet, showTestnet]);

  return { selectedTab, onTabChange };
}

function useRecentChains(chains: Record<NetworkId, TChainItem[]>) {
  const [recentChainsIds, setRecentChainsIds] = useLocalStorage<string[]>(
    KEY,
    [],
  );

  const recentChains = useMemo<TChainItem[]>(() => {
    return recentChainsIds
      ?.map((id: string) =>
        chains.mainnet?.find((item) => item.id === parseInt(id)),
      )
      .filter((chains: TChainItem) => !!chains);
  }, [chains, recentChainsIds]);

  const saveRecentChain = useCallback(
    (chain: TChainItem) => {
      // only mainnet can save chain id to local storage
      if (chain.isTestnet) {
        return;
      }
      let ids = recentChainsIds?.filter((id: number) => id !== chain.id);
      ids = [chain.id, ...ids].slice(0, MAX_RECENT_CHAINS);
      setRecentChainsIds(ids);
    },
    [recentChainsIds],
  );

  return { recentChains, saveRecentChain };
}
