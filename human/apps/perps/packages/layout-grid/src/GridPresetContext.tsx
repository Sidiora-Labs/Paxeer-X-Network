/** Preset list + selected id (LayoutRuleManager); used by plugin for getInitialLayout/storageKey. */
import React, { createContext, useContext, useMemo } from "react";
import {
  LayoutRuleManager,
  useLayoutRuleManager,
} from "@paxeer-network/layout-core";
import {
  GRID_PRESET_ID_STORAGE_KEY,
  GRID_LAYOUT_STORAGE_KEY_PREFIX,
} from "./constants";
import type { GridLayoutPreset } from "./types";

export interface GridPresetContextValue {
  presets: GridLayoutPreset[];
  selectedPresetId: string;
  setSelectedPresetId: (id: string) => void;
  layoutStorageKey: string;
  reset: () => void;
  rowHeight?: number;
}

const GridPresetContext = createContext<GridPresetContextValue | null>(null);

export interface GridPresetProviderProps {
  presets: GridLayoutPreset[];
  persistLayout?: boolean;
  children: React.ReactNode;
}

export function GridPresetProvider({
  presets,
  persistLayout = true,
  children,
}: GridPresetProviderProps): React.ReactElement {
  const manager = useMemo(
    () =>
      new LayoutRuleManager(presets, {
        presetIdStorageKey: GRID_PRESET_ID_STORAGE_KEY,
        layoutStorageKeyPrefix: GRID_LAYOUT_STORAGE_KEY_PREFIX,
        persist: persistLayout,
      }),
    [presets, persistLayout],
  );

  const ruleState = useLayoutRuleManager(manager);

  const value = useMemo<GridPresetContextValue>(() => {
    const selectedPreset = (ruleState.presets as GridLayoutPreset[]).find(
      (p) => p.id === ruleState.selectedPresetId,
    );
    return {
      presets: ruleState.presets as GridLayoutPreset[],
      selectedPresetId: ruleState.selectedPresetId,
      setSelectedPresetId: ruleState.setSelectedPresetId,
      layoutStorageKey: ruleState.layoutStorageKey,
      reset: ruleState.reset,
      rowHeight: selectedPreset?.rowHeight,
    };
  }, [
    ruleState.presets,
    ruleState.selectedPresetId,
    ruleState.setSelectedPresetId,
    ruleState.layoutStorageKey,
    ruleState.reset,
  ]);

  return (
    <GridPresetContext.Provider value={value}>
      {children}
    </GridPresetContext.Provider>
  );
}

export function useGridPresetContext(): GridPresetContextValue | null {
  return useContext(GridPresetContext);
}
