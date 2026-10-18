import { useEffect, useState } from "react";
import { API } from "@paxeer-network/types";

export function useNeedSwapAndCross(options: {
  srcToken?: API.TokenInfo;
  dstToken?: API.TokenInfo;
  srcChainId?: number;
  dstChainId?: number;
}) {
  const { srcToken, dstToken, srcChainId, dstChainId } = options;
  const [needSwap, setNeedSwap] = useState(false);
  const [needCrossSwap, setNeedCrossSwap] = useState(false);

  useEffect(() => {
    if (!srcChainId || !dstChainId || !srcToken || !dstToken) return;
    const isCrossChain = srcChainId !== dstChainId;

    if (
      (srcToken?.symbol === "USDC" && !isCrossChain) ||
      // if srcToken is collateral and srcToken.symbol is the same as dstToken.symbol, it will not need swap
      (srcToken.is_collateral && srcToken.symbol === dstToken?.symbol)
    ) {
      setNeedSwap(false);
      setNeedCrossSwap(false);
      return;
    }

    setNeedSwap(srcToken?.symbol !== "USDC");

    if (isCrossChain) {
      setNeedCrossSwap(true);
      setNeedSwap(true);
    } else {
      setNeedCrossSwap(false);
    }
  }, [srcToken, dstToken, srcChainId, dstChainId]);

  return { needSwap, needCrossSwap };
}
