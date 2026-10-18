import { useRef } from "react";
import useSWRSubscription from "swr/subscription";
import { useWS } from "../useWS";

export const useOpenInterest = (symbol: string) => {
  const ws = useWS();

  const symbolRef = useRef<string>(symbol);
  symbolRef.current = symbol;

  return useSWRSubscription(`${symbol}@openinterest`, (key, { next }) => {
    const unsubscribe = ws.subscribe(`${symbol}@openinterest`, {
      onMessage: (message: any) => {
        // when current symbol is not the same as the ws symbol, skip update data and auto unsubscribe old symbol ws
        if (message.symbol !== symbolRef.current) {
          unsubscribe?.();
          return;
        }
        next(null, message.openInterest);
      },
    });

    return () => {
      unsubscribe?.();
    };
  });
};
