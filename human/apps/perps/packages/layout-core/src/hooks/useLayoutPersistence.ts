import { useMemo, useCallback, useState } from "react";
import type { LayoutModel, LayoutStrategy } from "../types";

function canUseLocalStorage(): boolean {
  try {
    return (
      typeof window !== "undefined" &&
      typeof window.localStorage !== "undefined"
    );
  } catch {
    return false;
  }
}

/** Native persisted state is scoped to the selected strategy and preset. */
export function useLayoutPersistence<TLayout extends LayoutModel>(
  strategy: LayoutStrategy<TLayout>,
  storageKey?: string,
  dep?: unknown,
): [TLayout | undefined, (layout: TLayout) => void] {
  const stored = useMemo(() => {
    if (!storageKey || !canUseLocalStorage()) return undefined;
    try {
      const value = localStorage.getItem(storageKey);
      return value ? strategy.deserialize(value) : undefined;
    } catch (error) {
      console.warn(`Failed to deserialize layout from ${storageKey}:`, error);
      return undefined;
    }
  }, [strategy, storageKey, dep]);
  const [current, setCurrent] = useState<{
    strategy: LayoutStrategy<TLayout>;
    key: string;
    dependency: unknown;
    layout: TLayout;
  }>();
  const layout =
    current?.strategy === strategy &&
    current.key === storageKey &&
    Object.is(current.dependency, dep)
      ? current.layout
      : stored;
  const setLayout = useCallback(
    (next: TLayout) => {
      if (!storageKey) return;
      // Update the live source of truth as well as storage. Otherwise a panel
      // registration rerender can reset the visible grid to its initial preset.
      setCurrent({ strategy, key: storageKey, dependency: dep, layout: next });
      if (!canUseLocalStorage()) return;
      try {
        localStorage.setItem(storageKey, strategy.serialize(next));
      } catch (error) {
        console.warn(`Failed to serialize layout to ${storageKey}:`, error);
      }
    },
    [strategy, storageKey, dep],
  );
  return [layout, setLayout];
}
