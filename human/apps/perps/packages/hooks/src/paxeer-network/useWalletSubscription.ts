import useSWRSubscription from "swr/subscription";
import { useAccount } from "../useAccount";
import { useWS } from "../useWS";

// only first callball will be called
// TODO: refactor this, reference useBalanceTopic
export const useWalletSubscription = (options?: {
  onMessage?: (data: any) => void;
}) => {
  const ws = useWS();
  const { state } = useAccount();

  return useSWRSubscription(
    state.accountId ? ["wallet", state.accountId] : null,
    (_, { next }) => {
      const unsubscribe = ws.privateSubscribe(
        {
          id: "wallet",
          event: "subscribe",
          topic: "wallet",
          ts: Date.now(),
        },
        {
          onMessage: (data: any) => {
            //
            options?.onMessage?.(data);
            next(null, data);
          },
        },
      );

      return () => unsubscribe();
    },
  );
};
