import { useEffect, useMemo, useState } from "react";
import { useMainTokenStore } from "@paxeer-network/hooks";
import type { API } from "@paxeer-network/types";

const splitTokenBySymbol = <T extends { token?: string }>(items: T[]) => {
  return items.reduce<Record<"usdc" | "others", T[]>>(
    (result, item) => {
      if (item.token?.toUpperCase() === "USDC") {
        result.usdc.push(item);
      } else {
        result.others.push(item);
      }
      return result;
    },
    { usdc: [], others: [] },
  );
};

interface Options {
  defaultValue?: string;
  reverse?: boolean;
}

type ConvertTokenInfo = API.Token & {
  precision: number;
  symbol: string;
};

export const getConvertTokenInfo = (
  tokenInfo: API.Token,
): ConvertTokenInfo => ({
  ...tokenInfo,
  symbol: tokenInfo.token,
  precision: tokenInfo.decimals ?? 6,
});

export const useToken = (options: Options) => {
  const { defaultValue, reverse = false } = options;

  const [sourceToken, setSourceToken] = useState<ConvertTokenInfo>();
  const [targetToken, setTargetToken] = useState<ConvertTokenInfo>();
  const [sourceTokens, setSourceTokens] = useState<ConvertTokenInfo[]>([]);

  const tokensInfo = useMainTokenStore((state) => state.data);

  // Tokens with on_chain_swap=false route through CeFi swap on the backend,
  // so every /v1/public/token entry is a valid convert source.
  const newTokensInfo = useMemo(() => {
    return (tokensInfo ?? []).reduce<ConvertTokenInfo[]>((result, item) => {
      result.push(getConvertTokenInfo(item));

      return result;
    }, []);
  }, [tokensInfo]);

  useEffect(() => {
    const { usdc, others } = splitTokenBySymbol(newTokensInfo);
    setSourceToken(() => {
      if (defaultValue) {
        const defaultToken = others.find(({ token }) => token === defaultValue);
        return defaultToken ? defaultToken : others[0];
      }
      return others[0];
    });
    setSourceTokens(others);
    setTargetToken(usdc[0]);
  }, [defaultValue, newTokensInfo]);

  const reverseSource = tokensInfo?.find((item) => item.token === "USDC");
  const reverseTarget = tokensInfo?.find((item) => item.token === "USDG");
  return {
    sourceToken: reverse
      ? reverseSource
        ? getConvertTokenInfo(reverseSource)
        : undefined
      : sourceToken,
    sourceTokens: reverse ? [] : sourceTokens,
    onSourceTokenChange: setSourceToken,
    targetToken: reverse
      ? reverseTarget
        ? getConvertTokenInfo(reverseTarget)
        : undefined
      : targetToken,
  };
};
