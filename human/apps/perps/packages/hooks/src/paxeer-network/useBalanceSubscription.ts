import useSWRSubscription from "swr/subscription";
import { useAccount } from "../useAccount";
import { useWS } from "../useWS";

export const useBalanceSubscription = (options?: {
  onMessage?: (data: any) => void;
}) => {
  const ws = useWS();
  const { state } = useAccount();

  return useSWRSubscription(
    state.accountId ? ["balance", state.accountId] : null,
    (_, { next }) => {
      const unsubscribe = ws.privateSubscribe(
        {
          id: "balance",
          event: "subscribe",
          topic: "balance",
          ts: Date.now(),
        },
        {
          onMessage: (data: any) => {
            options?.onMessage?.(data);
            next(null, data);
          },
        },
      );

      return () => unsubscribe();
    },
  );
};
