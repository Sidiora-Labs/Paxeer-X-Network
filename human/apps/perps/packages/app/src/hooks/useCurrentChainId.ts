import { useEffect, useState } from "react";
import {
  Chains,
  useChains,
  useConfig,
  useStorageChain,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { Chain, NetworkId } from "@paxeer-network/types";
type ReturnChain = Pick<Chain, "id"> & Partial<Omit<Chain, "id">>;

export type DefaultChain =
  | {
      mainnet?: ReturnChain;
      testnet?: ReturnChain;
    }
  | ((networkId: NetworkId, chains: Chains) => ReturnChain)
  | undefined;

export function useCurrentChainId(defaultChain?: DefaultChain) {
  const { storageChain, setStorageChain } = useStorageChain();
  const [currentChainId, setCurrentChainId] = useState<number | undefined>();

  const [chains] = useChains();
  const networkId = useConfig("networkId") as NetworkId;

  const { connectedChain } = useWalletConnector();

  useEffect(() => {
    if (connectedChain) {
      setCurrentChainId?.(
        typeof connectedChain.id === "number"
          ? connectedChain.id
          : parseInt(connectedChain.id)
      );
    } else {
      if (!!currentChainId) return;
      let fallbackChain: Partial<Chain> | undefined;

      const firstChain =
        networkId === "mainnet" ? chains.mainnet?.[0] : chains.testnet?.[0];

      if (typeof defaultChain === "function") {
        fallbackChain = defaultChain(networkId, chains);
      } else if (typeof defaultChain === "object") {
        fallbackChain =
          networkId === "mainnet"
            ? defaultChain?.mainnet
            : defaultChain?.testnet;
      }

      const chainId = fallbackChain?.id || firstChain?.network_infos?.chain_id;
      if (!chainId) return;

      if (storageChain) {
        setCurrentChainId?.(storageChain.chainId);
      } else {
        setStorageChain(chainId);
        setCurrentChainId?.(chainId);
      
      }
    }
  }, [
    connectedChain,
    chains,
    currentChainId,
    networkId,
    setCurrentChainId,
    defaultChain,
  ]);

  return [currentChainId, setCurrentChainId] as const;
}
