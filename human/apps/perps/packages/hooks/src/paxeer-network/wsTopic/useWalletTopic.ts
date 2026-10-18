import { useEffect } from "react";
import { useAccount } from "../../useAccount";
import { useWS } from "../../useWS";

export const useWalletTopic = (options: { onMessage: (data: any) => void }) => {
  const ws = useWS();
  const { state } = useAccount();

  useEffect(() => {
    if (!state.accountId) return;

    const unsubscribe = ws.privateSubscribe(
      {
        id: "wallet",
        event: "subscribe",
        topic: "wallet",
        ts: Date.now(),
      },
      {
        onMessage: (data: any) => {
          options.onMessage(data);
        },
      },
    );

    return () => unsubscribe?.();
  }, [state.accountId]);
};
