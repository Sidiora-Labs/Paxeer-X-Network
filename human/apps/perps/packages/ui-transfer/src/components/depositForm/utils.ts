import { API, isNativeTokenChecker } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

export const fetcher = (url: string) => fetch(url).then((res) => res.json());

export function mergeTokens(
  paxeerNetworkTokens: API.TokenInfo[],
  swapTokens: API.TokenInfo[],
) {
  const tokens = paxeerNetworkTokens.map((item) => {
    return {
      ...item,
      swap_enable: swapTokens.some((swapToken) => {
        return swapToken.address?.toLowerCase() === item.address?.toLowerCase();
      }),
    } as API.TokenInfo;
  });

  swapTokens.forEach((swapToken) => {
    const paxeerNetworkToken = paxeerNetworkTokens.find((item) => {
      return (
        item.address?.toLowerCase() === swapToken.address?.toLowerCase() ||
        item.symbol === swapToken.symbol
      );
    });

    if (!paxeerNetworkToken) {
      tokens.push(swapToken);
    }
  });

  return tokens;
}

export function filterAndSortTokens(
  paxeerNetworkTokens: API.TokenInfo[] = [],
  swapTokens: API.TokenInfo[] = [],
  tokenBalances: Record<string, string> = {},
  getIndexPrice: (token: string) => number,
) {
  const mergedTokens = mergeTokens(paxeerNetworkTokens, swapTokens);

  const filteredTokens = mergedTokens
    .map((item) => {
      const quantity = tokenBalances[item.symbol!];
      const amount = new Decimal(quantity || 0)
        .todp(item.precision || 6, Decimal.ROUND_DOWN)
        .toNumber();

      return {
        ...item,
        quantity,
        amount,
      };
    })
    .filter((item) => {
      return item.is_collateral || item.amount > 0;
    });

  const list = filteredTokens.map((item) => {
    const indexPrice = getIndexPrice(item.symbol!);

    const balance = new Decimal(item.amount || 0)
      .mul(indexPrice || 1)
      .todp(item.precision || 2)
      .toNumber();

    return {
      ...item,
      balance,
      isNativeToken: isNativeTokenChecker(item.address!),
    };
  });

  const STABLECOIN_ORDER: Record<string, number> = {
    USDC: 0,
    "USDC.e": 1,
    USDT: 2,
  };
  const getStablecoinRank = (symbol: string | undefined) =>
    symbol !== undefined && symbol in STABLECOIN_ORDER
      ? STABLECOIN_ORDER[symbol]
      : Number.MAX_SAFE_INTEGER;

  return list.sort((a, b) => {
    // 1. paxeer-network supported collateral first
    if (a.is_collateral !== b.is_collateral) {
      return a.is_collateral ? -1 : 1;
    }

    // 2. Among collateral: stablecoins (USDC, USDC.e, USDT) first
    if (a.is_collateral && b.is_collateral) {
      const rankA = getStablecoinRank(a.symbol);
      const rankB = getStablecoinRank(b.symbol);
      if (rankA !== rankB) return rankA - rankB;

      // 3. Gas/native token (only when also collateral) after stablecoins
      if (a.isNativeToken !== b.isNativeToken) {
        return a.isNativeToken ? -1 : 1;
      }
    }

    // 4. Alphabetically A–Z
    return (a.symbol || "").localeCompare(b.symbol || "");
  });
}
