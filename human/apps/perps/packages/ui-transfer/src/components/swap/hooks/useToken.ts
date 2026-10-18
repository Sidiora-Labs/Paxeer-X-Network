import { useCallback, useEffect, useState } from "react";
import { API } from "@paxeer-network/types";
import { CurrentChain } from "../../../types";
import { getUSDCToken } from "../../../utils";

type Options = {
  currentChain: CurrentChain | null;
  tokensFilter?: (chainInfo: API.Chain) => API.TokenInfo[];
};

export function useToken(options: Options) {
  const { currentChain, tokensFilter } = options;
  const [token, setToken] = useState<API.TokenInfo>();
  const [tokens, setTokens] = useState<API.TokenInfo[]>([]);

  // when chain changed and chain data ready then call this function
  const onChainInited = useCallback(
    (chainInfo?: API.Chain) => {
      if (chainInfo && chainInfo?.token_infos?.length > 0) {
        const tokens =
          typeof tokensFilter === "function"
            ? tokensFilter(chainInfo)
            : chainInfo.token_infos;

        setTokens(tokens);

        const newToken = getUSDCToken(tokens);

        if (!newToken) return;

        setToken(newToken);
      }
    },
    [tokensFilter],
  );

  useEffect(() => {
    onChainInited(currentChain?.info);
    // if settingChain is true, the currentChain will change, so use currentChain.id
  }, [currentChain?.id, onChainInited]);

  return { token, tokens, onTokenChange: setToken };
}
