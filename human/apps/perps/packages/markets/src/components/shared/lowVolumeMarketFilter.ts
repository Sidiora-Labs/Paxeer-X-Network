import { useLocalStorage } from "@paxeer-network/hooks";

const STORAGE_KEY = "paxeerNetwork_markets_show_low_volume";

export type LowVolumeMarketFilterState = {
  showLowVolume: boolean;
  setShowLowVolume: (value: boolean) => void;
};

/**
 * Global show-low-volume preference backed by localStorage.
 * No Provider needed — all callers share the same key and sync via storage events.
 */
export const useLowVolumeMarketFilter = (): LowVolumeMarketFilterState => {
  const [showLowVolume, setShowLowVolume] = useLocalStorage<boolean>(
    STORAGE_KEY,
    true,
  );

  return { showLowVolume, setShowLowVolume };
};
