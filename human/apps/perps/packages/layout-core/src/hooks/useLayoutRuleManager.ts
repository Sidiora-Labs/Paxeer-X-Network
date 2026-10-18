/**
 * @file useLayoutRuleManager.ts
 * @description React hook that syncs LayoutRuleManager with component state for presets, selection, and reset.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import type {
  LayoutPreset,
  LayoutRuleManager,
} from "../utils/LayoutRuleManager";

export interface UseLayoutRuleManagerResult<TRule> {
  presets: readonly LayoutPreset<TRule>[];
  selectedPresetId: string;
  setSelectedPresetId: (id: string) => void;
  layoutStorageKey: string;
  reset: () => void;
  getSelectedPreset: () => LayoutPreset<TRule> | undefined;
  hasPersistedLayout: () => boolean;
}

/**
 * Binds a LayoutRuleManager to React state so selection and reset trigger re-renders.
 * setSelectedPresetId updates both the manager (localStorage) and local state.
 *
 * @param manager - LayoutRuleManager instance (e.g. from LayoutRuleManager<SplitLayoutRule>)
 * @returns Presets, selectedPresetId, setSelectedPresetId, layoutStorageKey, reset, getSelectedPreset, hasPersistedLayout
 */
export function useLayoutRuleManager<TRule>(
  manager: LayoutRuleManager<TRule>,
): UseLayoutRuleManagerResult<TRule> {
  const [selectedPresetId, setSelectedPresetIdState] = useState<string>(() =>
    manager.getSelectedPresetId(),
  );

  /** Sync state when manager instance changes (e.g. presets changed). */
  useEffect(() => {
    setSelectedPresetIdState(manager.getSelectedPresetId());
  }, [manager]);

  const setSelectedPresetId = useCallback(
    (id: string) => {
      manager.setSelectedPresetId(id);
      setSelectedPresetIdState(id);
    },
    [manager],
  );

  const reset = useCallback(() => {
    manager.reset();
    // Optionally force re-read selected id (no change) so consumers can react to reset
    setSelectedPresetIdState((prev) => prev);
  }, [manager]);

  const layoutStorageKey = useMemo(
    () => manager.getLayoutStorageKey(),
    [manager, selectedPresetId],
  );

  const getSelectedPreset = useCallback(
    () => manager.getSelectedPreset(),
    [manager],
  );

  const hasPersistedLayout = useCallback(
    () => manager.hasPersistedLayout(),
    [manager],
  );

  return useMemo(
    () => ({
      presets: manager.presets,
      selectedPresetId,
      setSelectedPresetId,
      layoutStorageKey,
      reset,
      getSelectedPreset,
      hasPersistedLayout,
    }),
    [
      manager,
      selectedPresetId,
      setSelectedPresetId,
      layoutStorageKey,
      reset,
      getSelectedPreset,
      hasPersistedLayout,
    ],
  );
}
