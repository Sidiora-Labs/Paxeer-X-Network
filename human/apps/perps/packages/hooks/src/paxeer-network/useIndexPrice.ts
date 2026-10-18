import { useRef } from "react";
import useSWRSubscription from "swr/subscription";
import { useWS } from "../useWS";

export const useIndexPrice = (symbol: string) => {
  const symbolRef = useRef<string>(symbol);
  symbolRef.current = symbol;

  const ws = useWS();
  return useSWRSubscription(`${symbol}@indexprice`, (key, { next }) => {
    const unsubscribe = ws.subscribe(`${symbol}@indexprice`, {
      onMessage: (message: any) => {
        // when current symbol is not the same as the ws symbol, skip update data and auto unsubscribe old symbol ws
        if (message.symbol !== symbolRef.current) {
          unsubscribe?.();
          return;
        }
        next(null, message.price);
      },
    });

    return () => {
      unsubscribe?.();
    };
  });
};
