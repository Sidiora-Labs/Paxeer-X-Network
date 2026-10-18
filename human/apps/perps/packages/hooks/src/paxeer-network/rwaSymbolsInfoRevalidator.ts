import { API } from "@paxeer-network/types";

export const RWA_SYMBOLS_INFO_QUERY_KEY = "/v1/public/rwa/info";

export const getExpiredRwaSymbolsInfoRefreshKeys = (
  rwaSymbolsInfo: Record<string, API.RwaSymbol>,
  currentTime: number,
  triggeredRefreshKeys: Record<string, true>,
) => {
  return Object.entries(rwaSymbolsInfo).reduce<string[]>(
    (keys, [symbol, rwaSymbol]) => {
      if (!isValidTimestamp(rwaSymbol.next_close)) {
        return keys;
      }

      if (currentTime < rwaSymbol.next_close) {
        return keys;
      }

      const key = createRwaSymbolsInfoRefreshKey(symbol, rwaSymbol);
      if (!triggeredRefreshKeys[key]) {
        keys.push(key);
      }

      return keys;
    },
    [],
  );
};

export const getRwaSymbolsInfoRefreshState = (
  rwaSymbolsInfo: Record<string, API.RwaSymbol>,
  currentTime: number,
  triggeredRefreshKeys: Record<string, true>,
) => {
  const expiredRefreshKeys = getExpiredRwaSymbolsInfoRefreshKeys(
    rwaSymbolsInfo,
    currentTime,
    triggeredRefreshKeys,
  );

  if (expiredRefreshKeys.length === 0) {
    return {
      shouldRefresh: false,
      triggeredRefreshKeys,
    };
  }

  const nextTriggeredRefreshKeys = { ...triggeredRefreshKeys };
  expiredRefreshKeys.forEach((key) => {
    nextTriggeredRefreshKeys[key] = true;
  });

  return {
    shouldRefresh: true,
    triggeredRefreshKeys: nextTriggeredRefreshKeys,
  };
};

const createRwaSymbolsInfoRefreshKey = (
  symbol: string,
  rwaSymbol: API.RwaSymbol,
) => {
  return `${symbol}:${rwaSymbol.status}:${rwaSymbol.next_open}:${rwaSymbol.next_close}`;
};

const isValidTimestamp = (timestamp?: number): timestamp is number => {
  return typeof timestamp === "number" && timestamp > 0;
};
