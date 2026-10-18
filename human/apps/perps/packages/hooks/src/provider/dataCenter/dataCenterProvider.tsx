import { useEffect, useMemo, useRef, type PropsWithChildren } from "react";
import { useSWRConfig } from "swr";
import {
  DataCenterContext,
  type DataCenterContextState,
  type getKeyFunction,
} from "./dataCenterContext";
import { NativeRuntime } from "../../native/NativeRuntime";
export function DataCenterProvider({ children }: PropsWithChildren) {
  const { mutate } = useSWRConfig();
  const handlers = useRef(new Map<string, getKeyFunction>());
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState !== "visible") return;
      for (const handler of handlers.current.values()) {
        const key = handler(0, null);
        if (key) void mutate(key);
      }
      void mutate(
        (key) =>
          typeof key === "string" &&
          /\/(?:orders|algo\/orders|trades|asset\/history|positions)(?:\?|$)/.test(
            key,
          ),
      );
    };
    const timer = setInterval(refresh, 15_000);
    window.addEventListener("paxeer-network:transaction", refresh);
    return () => {
      clearInterval(timer);
      window.removeEventListener("paxeer-network:transaction", refresh);
    };
  }, [mutate]);
  const value = useMemo<DataCenterContextState>(
    () => ({
      registerKeyHandler: (key, handler) => handlers.current.set(key, handler),
      unregisterKeyHandler: (key) => {
        handlers.current.delete(key);
      },
    }),
    [],
  );
  return (
    <DataCenterContext.Provider value={value}>
      <NativeRuntime />
      {children}
    </DataCenterContext.Provider>
  );
}
