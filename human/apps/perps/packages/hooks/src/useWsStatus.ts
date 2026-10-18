import { useEffect, useRef, useState } from "react";
import { useWS } from "./useWS";

export enum WsNetworkStatus {
  Connected = "connected",
  Unstable = "unstable",
  Disconnected = "disconnected",
}

export function useWsStatus() {
  const ws = useWS();
  const [wsStatus, setWsStatus] = useState<WsNetworkStatus>(
    ws.client.public.readyState
      ? WsNetworkStatus.Connected
      : WsNetworkStatus.Disconnected,
  );

  const connectCount = useRef(0);

  useEffect(() => {
    ws.on("status:change", (status: any) => {
      const { type, isPrivate } = status;
      if (!isPrivate) {
        switch (type) {
          case "open":
            connectCount.current = 0;
            setWsStatus(WsNetworkStatus.Connected);
            break;
          case "close":
            connectCount.current = 0;
            setWsStatus(WsNetworkStatus.Disconnected);
            break;
          case "reconnecting":
            connectCount.current++;
            if (connectCount.current >= 3) {
              setWsStatus(WsNetworkStatus.Unstable);
            }
            break;
        }
      }
    });
    return () => {
      ws.off("status:change", () => {});
    };
  }, []);

  return wsStatus;
}
