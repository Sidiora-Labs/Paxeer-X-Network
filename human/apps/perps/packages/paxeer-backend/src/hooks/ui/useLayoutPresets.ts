import { useState, useCallback, useEffect } from "react";

export interface LayoutPreset {
  id: string;
  label: string;
  watchlistWidth: number;
  orderbookWidth: number;
  chartHeight: number;
  positionsHeight: number;
  watchlistCollapsed: boolean;
}

const PRESETS: LayoutPreset[] = [
  {
    id: "default",
    label: "Default",
    watchlistWidth: 220,
    orderbookWidth: 260,
    chartHeight: 520,
    positionsHeight: 300,
    watchlistCollapsed: false,
  },
  {
    id: "pro",
    label: "Pro",
    watchlistWidth: 280,
    orderbookWidth: 340,
    chartHeight: 600,
    positionsHeight: 360,
    watchlistCollapsed: false,
  },
  {
    id: "compact",
    label: "Compact",
    watchlistWidth: 160,
    orderbookWidth: 280,
    chartHeight: 400,
    positionsHeight: 200,
    watchlistCollapsed: false,
  },
  {
    id: "chart-focus",
    label: "Chart Focus",
    watchlistWidth: 160,
    orderbookWidth: 280,
    chartHeight: 700,
    positionsHeight: 160,
    watchlistCollapsed: false,
  },
];

const STORAGE_KEY = "sidiora-layout-preset";

function loadSavedPreset(): string {
  if (typeof window === "undefined") return "default";
  try {
    return localStorage.getItem(STORAGE_KEY) || "default";
  } catch {
    return "default";
  }
}

export function useLayoutPresets() {
  const [activeId, setActiveId] = useState<string>(loadSavedPreset);

  const activePreset = PRESETS.find((p) => p.id === activeId) ?? PRESETS[0]!;

  const applyPreset = useCallback((id: string) => {
    setActiveId(id);
    try {
      localStorage.setItem(STORAGE_KEY, id);
    } catch {}
  }, []);

  return {
    presets: PRESETS,
    activePreset,
    activeId,
    applyPreset,
  };
}
