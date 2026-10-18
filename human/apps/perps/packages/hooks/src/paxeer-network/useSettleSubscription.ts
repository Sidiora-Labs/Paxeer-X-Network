import useSWRSubscription from "swr/subscription";
import { getTimestamp } from "@paxeer-network/utils";
import { useAccount } from "../useAccount";
import { useWS } from "../useWS";

export const useSettleSubscription = (options?: {
  onMessage?: (data: any) => void;
}) => {
  const ws = useWS();
  const { state } = useAccount();

  return useSWRSubscription(
    state.accountId ? ["settle", state.accountId] : null,
    (_, { next }) => {
      const unsubscribe = ws.privateSubscribe(
        {
          id: "settle",
          event: "subscribe",
          topic: "settle",
          ts: getTimestamp(),
        },
        {
          onMessage: (data: any) => {
            //
            options?.onMessage?.(data);
            next(data);
          },
        },
      );

      return () => unsubscribe();
    },
  );
};
