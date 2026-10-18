import { useCallback, useEffect, useState } from "react";
import { API } from "@paxeer-network/types";
import { CurrentChain } from "../../../types";
import { sortTokensWithUSDCFirst } from "../../../utils";

export const usePaxeerNetworkTokens = (currentChain?: CurrentChain | null) => {
  const [paxeerNetworkTokens, setPaxeerNetworkTokens] = useState<API.TokenInfo[]>([]);

  // when chain changed and chain data ready then call this function init tokens
  const onChainInited = useCallback((chainInfo?: API.Chain) => {
    if (chainInfo && chainInfo?.token_infos?.length > 0) {
      const paxeerNetworkTokens = chainInfo.token_infos;

      const tokens = sortTokensWithUSDCFirst(paxeerNetworkTokens);

      setPaxeerNetworkTokens(tokens);
    }
  }, []);

  useEffect(() => {
    onChainInited(currentChain?.info);
  }, [currentChain]);

  return paxeerNetworkTokens;
};
