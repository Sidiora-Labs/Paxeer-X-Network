import { chainsInfoMap } from "@paxeer-network/types";

export interface NetworkInterface {
  name: string;
  logo: string;
  chainId: string;
  label: string;
  token: string;
  requestRpc: string;
  explorerUrls: string[];
}

export const getChainsArray = () => {
  return Array.from(chainsInfoMap.values()).map((chain) => {
    return {
      id: chain.chainInfo.chainId,
      token: chain.chainInfo.nativeCurrency.symbol,
      label: chain.chainInfo.chainName,
      rpcUrl: chain.chainInfo.rpcUrls[0],
      blockExplorerUrl: chain.chainInfo.blockExplorerUrls[0],
    };
  });
};
