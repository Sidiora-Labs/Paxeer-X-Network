import { useMemo, useEffect } from "react";
import { mutate } from "swr";
import { create } from "zustand";
import { API } from "@paxeer-network/types";
import { createGetter } from "../utils/createGetter";
import { useAppStore } from "./appStore";
import {
  RWA_SYMBOLS_INFO_QUERY_KEY,
  getRwaSymbolsInfoRefreshState,
} from "./rwaSymbolsInfoRevalidator";

/**
 * Check if currently trading based on next_open/next_close timestamps
 * @param nextClose - Next close time timestamp
 * @param status - RWA status from API
 * @param currentTime - Current time timestamp
 * @param nextOpen - Next open time timestamp
 * @returns boolean - true if currently trading
 */
export const isCurrentlyTrading = (
  nextClose: number | undefined,
  status: "open" | "close",
  currentTime: number = Date.now(),
  nextOpen?: number,
): boolean => {
  const hasNextClose = isValidTimestamp(nextClose);

  if (status === "open") {
    return !hasNextClose || currentTime < nextClose;
  }

  const hasNextOpen = isValidTimestamp(nextOpen);

  return (
    hasNextOpen &&
    currentTime >= nextOpen &&
    (!hasNextClose || currentTime < nextClose)
  );
};

export const isCurrentlyClosed = (
  nextOpen: number,
  status: "open" | "close",
  currentTime: number = Date.now(),
  nextClose?: number,
): boolean => {
  return !isCurrentlyTrading(nextClose, status, currentTime, nextOpen);
};

const isValidTimestamp = (timestamp?: number): timestamp is number => {
  return typeof timestamp === "number" && timestamp > 0;
};

/**
 * Type alias for the return type of useSymbolsInfo hook
 */
export type RwaSymbolsInfo = ReturnType<typeof useRwaSymbolsInfo>;

/**
 * A hook that provides access to symbol information.
 *
 * @returns A getter object that provides access to symbol information.
 * The getter allows accessing symbol data either by symbol name directly,
 * or through a two-level access pattern (symbol and property).
 *
 * @example
 * ```typescript
 * const rwaSymbolsInfo = useRwaSymbolsInfo();
 *
 * // Get all info for a symbol
 * const ethInfo = rwaSymbolsInfo["PERP_ETH_USDC"]();
 * ```
 */
export const useRwaSymbolsInfo = () => {
  const symbolsInfo = useAppStore((state) => state.rwaSymbolsInfo);
  return useMemo(() => createGetter({ ...symbolsInfo }), [symbolsInfo]);
};

export const useRwaSymbolsInfoStore = () => {
  return useAppStore((state) => state.rwaSymbolsInfo);
};

/**
 * Return type definition for the hook
 *
 * - isRwa: true if the symbol is an RWA symbol
 * - open: true if the symbol is open for trading
 * - nextOpen: the next open time in milliseconds
 * - nextClose: the next close time in milliseconds
 * - closeTimeInterval: the time interval in seconds until the symbol closes (countdown format)
 * - openTimeInterval: the time interval in seconds until the symbol opens (countdown format)
 */
export interface RwaSymbolResult {
  isRwa: boolean;
  open?: boolean;
  nextOpen?: number;
  nextClose?: number;
  closeTimeInterval?: number;
  openTimeInterval?: number;
}

/**
 * Computed RWA symbol state
 */
interface ComputedRwaSymbolState {
  isRwa: boolean;
  open?: boolean;
  nextOpen?: number;
  nextClose?: number;
  closeTimeInterval?: number;
  openTimeInterval?: number;
}

/**
 * RWA symbols runtime store state
 */
interface RwaSymbolsRuntimeState {
  // Computed states for all symbols
  computedStates: Record<string, ComputedRwaSymbolState>;
  // Current timestamp
  currentTime: number;
  // Timer reference
  timerId?: NodeJS.Timeout;
  // Refresh keys that have already triggered a revalidation
  triggeredRefreshKeys: Record<string, true>;
  // Start the timer
  startTimer: () => void;
  // Stop the timer
  stopTimer: () => void;
  // Update computed states
  updateComputedStates: (rwaSymbolsInfo: Record<string, API.RwaSymbol>) => void;
}

/**
 * Compute the state for a single symbol
 */
const computeSymbolState = (
  rwaSymbol: API.RwaSymbol,
  currentTime: number,
): ComputedRwaSymbolState => {
  const { status, next_close, next_open } = rwaSymbol;

  // Use isCurrentlyTrading function to determine if currently tradeable
  const isOpen = isCurrentlyTrading(next_close, status, currentTime, next_open);

  let closeTimeInterval: number | undefined;
  let openTimeInterval: number | undefined;

  // Calculate countdown to closing time
  if (isOpen && isValidTimestamp(next_close) && next_close > currentTime) {
    closeTimeInterval = Math.max(
      0,
      Math.floor((next_close - currentTime) / 1000),
    );
  }

  // Calculate countdown to opening time
  if (!isOpen && isValidTimestamp(next_open) && next_open > currentTime) {
    openTimeInterval = Math.max(
      0,
      Math.floor((next_open - currentTime) / 1000),
    );
  }

  return {
    isRwa: true,
    open: isOpen,
    nextOpen: next_open || undefined,
    nextClose: next_close || undefined,
    closeTimeInterval,
    openTimeInterval,
  };
};

/**
 * Centralized RWA symbols runtime state management
 * Uses a single timer to compute all symbol states, avoiding performance waste from multiple timers
 */
const useRwaSymbolsRuntimeStore = create<RwaSymbolsRuntimeState>(
  (set, get) => ({
    computedStates: {},
    currentTime: Date.now(),
    timerId: undefined,
    triggeredRefreshKeys: {},

    startTimer: () => {
      const state = get();

      // Clear existing timer if present
      if (state.timerId) {
        clearInterval(state.timerId);
      }

      // Start new timer, update every second
      const timerId = setInterval(() => {
        const currentTime = Date.now();
        const rwaSymbolsInfo = useAppStore.getState().rwaSymbolsInfo;

        if (!rwaSymbolsInfo) {
          set({ currentTime });
          return;
        }

        updateRwaSymbolsRuntimeState(rwaSymbolsInfo, currentTime, get, set);
      }, 1000);

      set({ timerId });
    },

    stopTimer: () => {
      const state = get();
      if (state.timerId) {
        clearInterval(state.timerId);
        set({ timerId: undefined });
      }
    },

    updateComputedStates: (rwaSymbolsInfo: Record<string, API.RwaSymbol>) => {
      const currentTime = get().currentTime;
      updateRwaSymbolsRuntimeState(rwaSymbolsInfo, currentTime, get, set);
    },
  }),
);

const updateRwaSymbolsRuntimeState = (
  rwaSymbolsInfo: Record<string, API.RwaSymbol>,
  currentTime: number,
  get: () => RwaSymbolsRuntimeState,
  set: (
    partial:
      | Partial<RwaSymbolsRuntimeState>
      | ((state: RwaSymbolsRuntimeState) => Partial<RwaSymbolsRuntimeState>),
  ) => void,
) => {
  const state = get();
  const computedStates: Record<string, ComputedRwaSymbolState> = {};

  Object.entries(rwaSymbolsInfo).forEach(([symbol, rwaSymbol]) => {
    computedStates[symbol] = computeSymbolState(rwaSymbol, currentTime);
  });

  const refreshState = getRwaSymbolsInfoRefreshState(
    rwaSymbolsInfo,
    currentTime,
    state.triggeredRefreshKeys,
  );

  if (!refreshState.shouldRefresh) {
    set({ computedStates, currentTime });
    return;
  }

  set({
    computedStates,
    currentTime,
    triggeredRefreshKeys: refreshState.triggeredRefreshKeys,
  });

  try {
    Promise.resolve(mutate(RWA_SYMBOLS_INFO_QUERY_KEY)).catch(() => undefined);
  } catch {
    // Ignore refresh failures. The stale snapshot is marked as triggered by design.
  }
};

/**
 * Hook to initialize and manage the global timer
 * This hook should be called once at the top level of the application to start and manage the global timer
 */
export const useInitRwaSymbolsRuntime = () => {
  const rwaSymbolsInfo = useRwaSymbolsInfoStore();
  const { startTimer, stopTimer, updateComputedStates } =
    useRwaSymbolsRuntimeStore();

  useEffect(() => {
    // Start timer when rwaSymbolsInfo exists
    if (rwaSymbolsInfo && Object.keys(rwaSymbolsInfo).length > 0) {
      // Update state immediately on first run
      updateComputedStates(rwaSymbolsInfo);
      // Start the timer
      startTimer();
    }

    // Cleanup: stop the timer
    return () => {
      stopTimer();
    };
  }, [rwaSymbolsInfo, startTimer, stopTimer, updateComputedStates]);
};

/**
 * Hook to get current RWA symbol information with real-time updates
 * Retrieves the state of a specific symbol from the centralized store
 * @param symbol - The symbol to query
 * @returns RwaSymbolResult containing RWA status and countdown information
 */
export const useGetRwaSymbolInfo = (symbol: string): RwaSymbolResult => {
  // Subscribe to the computed state of a specific symbol
  const computedState = useRwaSymbolsRuntimeStore(
    (state) => state.computedStates[symbol],
  );

  return useMemo(() => {
    if (!computedState) {
      return { isRwa: false };
    }
    return computedState;
  }, [computedState]);
};

/**
 * Simplified hook to get RWA symbol open status with real-time updates
 * @param symbol - The symbol to query
 * @returns Object containing isRwa and open status
 */
export const useGetRwaSymbolOpenStatus = (
  symbol: string,
): { isRwa: boolean; open?: boolean } => {
  const { isRwa, open } = useGetRwaSymbolInfo(symbol);

  return useMemo(() => {
    return { isRwa, open };
  }, [isRwa, open]);
};

/**
 * Generic time interval filtering function
 */
const filterTimeInterval = (
  timeInterval: number | undefined,
  isRwa: boolean,
  thresholdMinutes: number,
): number | undefined => {
  if (!isRwa || !timeInterval) {
    return undefined;
  }

  // Return undefined if time interval exceeds threshold
  return timeInterval > thresholdMinutes * 60 ? undefined : timeInterval;
};

/**
 * Hook to get RWA symbol close time interval with filtering
 * @param symbol - The symbol to query
 * @param thresholdMinutes - Time threshold in minutes, defaults to 30
 * @returns Close time interval in seconds, or undefined if not within threshold
 */
export const useGetRwaSymbolCloseTimeInterval = (
  symbol: string,
  thresholdMinutes: number = 30,
): {
  isRwa: boolean;
  open?: boolean;
  closeTimeInterval?: number;
  nextClose?: number;
} => {
  const { isRwa, open, closeTimeInterval, nextClose } =
    useGetRwaSymbolInfo(symbol);

  return useMemo(() => {
    const filteredInterval = filterTimeInterval(
      closeTimeInterval,
      isRwa,
      thresholdMinutes,
    );
    return { isRwa, open, closeTimeInterval: filteredInterval, nextClose };
  }, [isRwa, open, closeTimeInterval, nextClose, thresholdMinutes]);
};

/**
 * Hook to get RWA symbol open time interval with filtering
 * @param symbol - The symbol to query
 * @param thresholdMinutes - Time threshold in minutes, defaults to 30
 * @returns Open time interval in seconds, or undefined if not within threshold
 */
export const useGetRwaSymbolOpenTimeInterval = (
  symbol: string,
  thresholdMinutes: number = 30,
): {
  isRwa: boolean;
  open?: boolean;
  openTimeInterval?: number;
  nextOpen?: number;
} => {
  const { isRwa, open, openTimeInterval, nextOpen } =
    useGetRwaSymbolInfo(symbol);

  return useMemo(() => {
    const filteredInterval = filterTimeInterval(
      openTimeInterval,
      isRwa,
      thresholdMinutes,
    );
    return { isRwa, open, openTimeInterval: filteredInterval, nextOpen };
  }, [isRwa, open, openTimeInterval, nextOpen, thresholdMinutes]);
};
