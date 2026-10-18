import { useEffect, useMemo, useState } from "react";
import {
  useHoldingStream,
  useMemoizedFn,
  useTokensInfo,
} from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { CurrentChain, WithdrawTo } from "../../../types";
import { getUSDCToken } from "../../../utils";
import { isRobinhoodChain } from "../../reverseConvert/policy";
import { useToken } from "../hooks/useToken";

export function useWithdrawToken(params: {
  currentChain?: CurrentChain | null;
  withdrawTo: WithdrawTo;
}) {
  const { currentChain, withdrawTo } = params;
  const [token, setToken] = useState<API.TokenInfo>();
  const [tokens, setTokens] = useState<API.TokenInfo[]>([]);

  const tokensInfo = useTokensInfo();
  const { data: holdings } = useHoldingStream();

  const { sourceToken, onSourceTokenChange, sourceTokens } = useToken(
    currentChain,
    (token) => token.symbol === "USDC" || token.is_collateral,
  );

  // Get user holding
  const allTokens = useMemo(() => {
    if (!tokensInfo?.length) return [];

    const holdingMap = new Map<string, API.Holding>();
    holdings?.forEach((item) => {
      holdingMap.set(item.token, item);
    });

    const mappedTokens = tokensInfo.map((item) => ({
      ...item,
      symbol: item.token,
      token_decimal: item.decimals,
      precision: item.decimals,
    }));

    const list = mappedTokens.filter((token) => {
      const holding = holdingMap.get(token.symbol!);
      if (!holdings) return true;
      return !!holding && holding.holding > 0;
    });

    // If user has no holdings, show USDC by default
    if (list.length === 0) {
      const usdcToken = mappedTokens.find((t) => t.symbol === "USDC");
      if (usdcToken) {
        return [usdcToken];
      }
    }
    // sort tokens, USDC should be the first
    list.sort((a, b) => {
      if (a.symbol === "USDC") return -1;
      if (b.symbol === "USDC") return 1;
      return 0;
    });

    return list;
  }, [tokensInfo, holdings]);

  useEffect(() => {
    if (!allTokens.length) return;

    const usdcToken = getUSDCToken(allTokens);
    setToken(usdcToken || allTokens?.[0]);
    setTokens(allTokens);
  }, [allTokens]);

  // Check if a token is supported on the current chain
  const isTokenSupportedOnChain = useMemoizedFn(
    (tokenSymbol: string): boolean => {
      if (!currentChain?.info?.token_infos) {
        return false;
      }
      return currentChain.info.token_infos.some(
        (token) => token.symbol === tokenSymbol,
      );
    },
  );

  const handleSourceTokenChange = useMemoizedFn((newToken: API.TokenInfo) => {
    if (withdrawTo === WithdrawTo.Wallet && currentChain && newToken.symbol) {
      const matchingToken = sourceTokens.find(
        (t) => t.symbol === newToken.symbol,
      );
      if (matchingToken) {
        onSourceTokenChange(matchingToken);
      } else {
        onSourceTokenChange(newToken);
      }
      return;
    }
    // Withdraw to account
    onSourceTokenChange(newToken);
  });

  const syncToken = useMemoizedFn(() => {
    if (withdrawTo === WithdrawTo.Account) {
      const findToken = tokens.find(
        (item) => item.symbol === sourceToken?.symbol,
      ) as API.TokenInfo;
      if (findToken) {
        setToken(findToken);
      }
    }
  });

  useEffect(() => {
    if (withdrawTo !== WithdrawTo.Wallet || !allTokens.length) return;

    const holdingSymbols = new Set(allTokens.map((t) => t.symbol));
    if (!sourceToken?.symbol || !holdingSymbols.has(sourceToken.symbol)) {
      const defaultToken = getUSDCToken(allTokens);
      if (defaultToken) {
        handleSourceTokenChange(defaultToken);
      }
    }
  }, [withdrawTo, allTokens, sourceToken?.symbol, handleSourceTokenChange]);

  useEffect(() => {
    syncToken();
  }, [
    withdrawTo,
    allTokens,
    sourceToken,
    sourceTokens,
    currentChain,
    syncToken,
  ]);

  const robinhoodTokens = useMemo(() => {
    const usdg = currentChain?.info?.token_infos?.find(
      (item) => item.symbol === "USDG",
    );
    return usdg ? [{ ...usdg, precision: usdg.token_decimal }] : [];
  }, [currentChain?.info?.token_infos]);

  if (withdrawTo === WithdrawTo.Account) {
    return {
      sourceToken: token,
      onSourceTokenChange: setToken,
      sourceTokens: tokens,
      isTokenSupportedOnChain,
    };
  }

  if (isRobinhoodChain(currentChain?.id)) {
    return {
      sourceToken: robinhoodTokens[0],
      sourceTokens: robinhoodTokens,
      onSourceTokenChange: handleSourceTokenChange,
      isTokenSupportedOnChain,
    };
  }

  return {
    sourceToken,
    onSourceTokenChange: handleSourceTokenChange,
    sourceTokens: allTokens,
    isTokenSupportedOnChain,
  };
}
