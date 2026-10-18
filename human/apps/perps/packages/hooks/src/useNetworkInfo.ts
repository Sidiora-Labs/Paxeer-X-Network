import { NetworkId } from "@paxeer-network/types";
import { Chain, useChains } from "./";

export type CurrentChain = {
  id: number;
  info?: Chain;
};

export const useNetworkInfo = (networkId: NetworkId) => {
  const [, { findByChainId }] = useChains(networkId, {
    pick: "network_infos",
    filter: (chain: any) =>
      chain.network_infos?.bridge_enable || chain.network_infos?.bridgeless,
  });

  return (chainId: number) => {
    const chain = findByChainId(chainId);
    return {
      id: chainId,
      info: chain,
    } as CurrentChain;
  };
};
