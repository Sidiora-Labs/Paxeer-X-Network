import { useEffect, useRef } from "react";
import { useGetRwaSymbolInfo } from "../../paxeer-network/paxeerNetworkHooks";

export const useRwaLeverageSync = (
  symbol: string,
  refreshSymbolLeverages: () => Promise<unknown> | unknown,
) => {
  const { isRwa, open: rwaOpen } = useGetRwaSymbolInfo(symbol);
  const lastRwaOpenRef = useRef<{
    symbol?: string;
    open?: boolean;
  }>({});

  useEffect(() => {
    const previous = lastRwaOpenRef.current;

    if (!isRwa || typeof rwaOpen !== "boolean") {
      lastRwaOpenRef.current = { symbol };
      return;
    }

    if (previous.symbol !== symbol) {
      lastRwaOpenRef.current = { symbol, open: rwaOpen };
      return;
    }

    if (typeof previous.open === "boolean" && previous.open !== rwaOpen) {
      Promise.resolve(refreshSymbolLeverages()).catch(() => undefined);
    }

    lastRwaOpenRef.current = { symbol, open: rwaOpen };
  }, [isRwa, refreshSymbolLeverages, rwaOpen, symbol]);
};
