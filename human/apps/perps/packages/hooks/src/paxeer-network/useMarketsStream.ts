import { useMemo } from "react";
import useSWRSubscription from "swr/subscription";
import { WSMessage } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { useWS } from "../useWS";
import { useMarketList } from "./useMarket/market.store";

export const useMarketsStream = () => {
  const ws = useWS();
  const futures = useMarketList();

  const topic = "tickers";

  const { data: tickers } = useSWRSubscription("tickers", (_, { next }) => {
    const unsubscribe = ws.subscribe(topic, {
      onMessage: (message: any) => {
        next(null, message);
      },
    });

    return () => {
      unsubscribe?.();
    };
  });

  const value = useMemo(() => {
    if (!futures) return null;
    if (!tickers) return futures;

    return futures.map((item) => {
      const ticker = tickers.find(
        (t: WSMessage.Ticker) => t.symbol === item.symbol,
      );
      if (ticker) {
        const data = {
          ...item,
          ["24h_close"]: ticker.close,
          ["24h_open"]: ticker.open,
          /**
           * @deprecated
           * spelling mistake, use 24h_volume to instead, will be remove next version
           */
          ["24h_volumn"]: ticker.volume,
          ["24h_volume"]: ticker.volume,
          ["24h_amount"]: ticker.amount,
          change: 0,
        };

        if (ticker.close !== undefined && ticker.open !== undefined) {
          data["change"] = new Decimal(ticker.close)
            .minus(ticker.open)
            .div(ticker.open)
            .toNumber();
        }

        return data;
      }
      return item;
    });
  }, [futures, tickers]);

  return { data: value as unknown as WSMessage.Ticker[] };
};
