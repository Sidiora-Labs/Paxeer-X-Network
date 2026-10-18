import { API } from "@paxeer-network/types";

export function checkChainSupport(
  chainId: number | string,
  chains: API.Chain[]
) {
  if (typeof chainId === "string") {
    chainId = parseInt(chainId);
  }
  return chains.some((chain) => {
    return chain.network_infos.chain_id === chainId;
  });
}
