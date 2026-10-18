import { useCallback, useEffect, useRef, useState } from "react";
import { Chain, ConnectedChain, useConfig } from "@paxeer-network/hooks";
import { NetworkId, type API } from "@paxeer-network/types";
import { getUSDCToken } from "../../../utils";

export type CurrentChain = Pick<ConnectedChain, "namespace"> & {
  id: number;
  info?: Chain;
};

export const useToken = (
  currentChain?: CurrentChain | null,
  filter: (token: API.TokenInfo) => boolean = () => true,
) => {
  const [sourceToken, setSourceToken] = useState<API.TokenInfo>();
  const [targetToken, setTargetToken] = useState<API.TokenInfo>();

  const [sourceTokens, setSourceTokens] = useState<API.TokenInfo[]>([]);
  const [targetTokens, setTargetTokens] = useState<API.TokenInfo[]>([]);

  const sourceTokenUpdatedRef = useRef(false);

  const networkId = useConfig("networkId") as NetworkId;

  // when chain changed and chain data ready then call this function init tokens
  const onChainInited = useCallback((chainInfo?: API.Chain) => {
    if (chainInfo && chainInfo?.token_infos?.length > 0) {
      // const tokens = chainInfo.token_infos.filter((i) => i.is_collateral);
      // all tokens available in the chain, include swap tokens
      const tokens = chainInfo.token_infos?.filter(filter);

      if (tokens?.length) {
        // sort tokens, USDC should be the first
        tokens.sort((a, b) => {
          if (a.symbol === "USDC") return -1;
          if (b.symbol === "USDC") return 1;
          return 0;
        });
      }

      const usdcToken = getUSDCToken(tokens);
      if (!usdcToken) {
        return;
      }
      setSourceToken(usdcToken);
      setTargetToken(usdcToken);

      setSourceTokens(tokens);
      setTargetTokens([usdcToken]);
    }
  }, []);

  useEffect(() => {
    onChainInited(currentChain?.info);
    // if settingChain is true, the currentChain will change, so use currentChain.id
    // TODO:  confirm currentChain data is correct
  }, [currentChain, onChainInited]);

  useEffect(() => {
    if (!sourceToken || !sourceTokens.length) {
      return;
    }

    // USDC => USDC
    if (sourceToken.symbol === "USDC") {
      setTargetToken(sourceToken);
      setTargetTokens([sourceToken]);
      return;
    }

    const usdc = sourceTokens.find((t) => t.symbol === "USDC")!;

    // if is_collateral
    if (sourceToken.is_collateral) {
      // mainnet and swap_enable: [token] => [USDC,token]
      if (networkId === "mainnet" && sourceToken.swap_enable) {
        setTargetToken(sourceToken);
        setTargetTokens([sourceToken, usdc]);
      } else {
        // testnet: [token] => [token]
        setTargetToken(sourceToken);
        setTargetTokens([sourceToken]);
      }
      return;
    }

    // if swap token: [token] => [USDC]
    setTargetToken(usdc);
    setTargetTokens([usdc]);
  }, [networkId, sourceToken, sourceTokens]);

  const onSourceTokenChange = useCallback((token: API.TokenInfo) => {
    sourceTokenUpdatedRef.current = true;
    setSourceToken(token);
  }, []);

  useEffect(() => {
    sourceTokenUpdatedRef.current = false;
  }, [currentChain?.id]);

  return {
    sourceToken,
    targetToken,

    sourceTokens,
    targetTokens,

    onSourceTokenChange,
    setSourceToken,
    onTargetTokenChange: setTargetToken,
    sourceTokenUpdatedRef,
  };
};
