import { useEffect, useState } from "react";
import { API, SDKError } from "@paxeer-network/types";
import { getTimestamp } from "@paxeer-network/utils";
import { useWS } from "../useWS";

export interface MarketTradeStreamOptions {
  limit?: number;
}

const tradeKey = (trade: API.Trade & { trade_id?: string }) =>
  trade.trade_id ??
  `${trade.ts}:${trade.side}:${trade.executed_price}:${trade.executed_quantity}`;

export const useMarketTradeStream = (
  symbol: string,
  options: MarketTradeStreamOptions = {},
) => {
  if (!symbol) {
    throw new SDKError("Symbol is required");
  }

  const [trades, setTrades] = useState<API.Trade[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(false);

  const { limit = 50 } = options;

  const ws = useWS();

  useEffect(() => {
    let active = true;
    setIsLoading(true);
    setTrades([]);
    const unsubscribeHistory: unknown = ws.onceSubscribe(
      {
        id: `${symbol}@trade`,
        event: "request",
        params: {
          type: "trade",
          symbol: symbol,
          limit,
        },
      },
      {
        onMessage: (data: any) => {
          if (!active || !Array.isArray(data)) return;
          setIsLoading(false);
          setTrades(
            data.filter((trade) => trade.symbol === symbol).slice(0, limit),
          );
        },
      },
    );
    const unsubscribe = ws.subscribe(
      {
        id: `${symbol}@trade`,
        event: "subscribe",
        topic: `${symbol}@trade`,
        ts: getTimestamp(),
      },
      {
        onMessage: (data: any) => {
          if (!active || data?.symbol !== symbol) return;
          setIsLoading(false);
          setTrades((prev) =>
            [
              data,
              ...prev.filter((trade) => tradeKey(trade) !== tradeKey(data)),
            ].slice(0, limit),
          );
        },
      },
    );

    return () => {
      active = false;
      if (typeof unsubscribeHistory === "function") unsubscribeHistory();
      unsubscribe?.();
    };
  }, [symbol, limit, ws]);

  return { data: trades, isLoading };
};
