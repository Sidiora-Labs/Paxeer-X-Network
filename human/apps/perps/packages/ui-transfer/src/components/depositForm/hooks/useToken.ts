import { useEffect, useState } from "react";
import { useConfig } from "@paxeer-network/hooks";
import { NetworkId, type API } from "@paxeer-network/types";

export const useToken = (paxeerNetworkTokens: API.TokenInfo[]) => {
  const [sourceToken, setSourceToken] = useState<API.TokenInfo>();
  const [targetToken, setTargetToken] = useState<API.TokenInfo>();

  const [sourceTokens, setSourceTokens] = useState<API.TokenInfo[]>([]);
  const [targetTokens, setTargetTokens] = useState<API.TokenInfo[]>([]);

  const networkId = useConfig("networkId") as NetworkId;

  useEffect(() => {
    const usdcToken = paxeerNetworkTokens[0];
    if (!usdcToken) {
      return;
    }
    setSourceToken(usdcToken);
    setTargetToken(usdcToken);

    setSourceTokens(paxeerNetworkTokens);
    setTargetTokens([usdcToken]);
  }, [paxeerNetworkTokens]);

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

    const collateralTokens = sourceTokens.filter((item) => item.is_collateral)!;
    const usdcToken = sourceTokens.find((item) => item.symbol === "USDC")!;

    // if is_collateral
    if (sourceToken.is_collateral) {
      // mainnet and swap_enable: [token] => [USDC,token]
      if (networkId === "mainnet" && sourceToken.swap_enable) {
        setTargetToken(sourceToken);
        setTargetTokens([
          sourceToken,
          ...collateralTokens.filter(
            (item) => item.symbol !== sourceToken.symbol,
          ),
        ]);
      } else {
        // testnet: [token] => [token]
        setTargetToken(sourceToken);
        setTargetTokens([sourceToken]);
      }
      return;
    }

    // if swap token: [token] => [USDC]
    setTargetToken(usdcToken || collateralTokens[0]);
    setTargetTokens(collateralTokens);
  }, [networkId, sourceToken, sourceTokens]);

  return {
    sourceToken,
    targetToken,

    sourceTokens,
    targetTokens,

    setSourceTokens,

    onSourceTokenChange: setSourceToken,
    onTargetTokenChange: setTargetToken,
  };
};
