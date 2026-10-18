import React, { useMemo, useState, useEffect, useCallback } from "react";
import { useLayoutPersistence } from "./hooks/useLayoutPersistence";
import type {
  LayoutHostProps,
  LayoutModel,
  PanelRegistry,
  PanelRegistryEntry,
} from "./types";

/**
 * Check if a value is a Map instance (handles cross-realm scenarios)
 */
function isMap(value: unknown): value is Map<string, PanelRegistryEntry> {
  return (
    value !== null &&
    typeof value === "object" &&
    typeof (value as Map<string, PanelRegistryEntry>).get === "function" &&
    typeof (value as Map<string, PanelRegistryEntry>).keys === "function"
  );
}

/**
 * LayoutHost component
 * Provides a unified interface for rendering layouts using any strategy
 *
 * @example
 * ```tsx
 * <LayoutHost
 *   strategy={splitStrategy}
 *   panels={panelMap}
 *   storageKey="my_layout_state"
 *   onLayoutChange={(layout) => console.log('Layout changed', layout)}
 * />
 * ```
 */
export function LayoutHost<TLayout extends LayoutModel = LayoutModel>(
  props: LayoutHostProps<TLayout>,
) {
  const {
    strategy,
    panels: panelsInput,
    initialLayout,
    onLayoutChange,
    storageKey,
    className,
    style,
  } = props;

  // Convert panels input to PanelRegistry (Map)
  // Uses duck-typing check for cross-realm compatibility
  const panels: PanelRegistry = useMemo(() => {
    if (isMap(panelsInput)) {
      return panelsInput;
    }
    // Convert Record to Map
    const map = new Map<string, PanelRegistryEntry>();
    Object.entries(panelsInput).forEach(([id, entry]) => {
      // Handle both old format (ReactNode) and new format (PanelRegistryEntry)
      if (entry && typeof entry === "object" && "node" in entry) {
        map.set(id, entry as PanelRegistryEntry);
      } else {
        map.set(id, { node: entry as React.ReactNode, props: {} });
      }
    });
    return map;
  }, [panelsInput]);

  // Get panel IDs from registry
  const panelIds = useMemo(() => Array.from(panels.keys()), [panels]);

  // Initialize layout state from persistence
  const [persistedLayout, setPersistedLayout] = useLayoutPersistence(
    strategy,
    storageKey,
    strategy.id, // Re-read when strategy changes
  );

  // Compute the resolved layout: persisted > initial > default
  const resolvedLayout = useMemo<TLayout>(() => {
    if (persistedLayout) {
      return persistedLayout;
    }
    if (initialLayout) {
      return initialLayout;
    }
    return strategy.defaultLayout(panelIds) as TLayout;
  }, [persistedLayout, initialLayout, strategy, panelIds]);

  // Internal layout state - initialized with resolved layout
  const [currentLayout, setCurrentLayout] = useState<TLayout>(resolvedLayout);

  // Sync when resolved layout changes (strategy switch, initial load, etc.)
  useEffect(() => {
    setCurrentLayout(resolvedLayout);
  }, [resolvedLayout]);

  // Handle layout changes - only updates internal state, no persistence
  const handleLayoutChange = useCallback(
    (newLayout: TLayout) => {
      setCurrentLayout(newLayout);
      // Notify parent
      onLayoutChange?.(newLayout);
    },
    [onLayoutChange],
  );

  // Handle layout persist - persists layout to storage
  const handleLayoutPersist = useCallback(
    (newLayout: TLayout) => {
      // Persist if storage key is provided
      if (storageKey) {
        setPersistedLayout(newLayout);
      }
    },
    [storageKey, setPersistedLayout],
  );

  // Get the Renderer component from strategy
  const Renderer = strategy.Renderer;

  // Handle empty panels case
  if (panels.size === 0) {
    return (
      <div className={className} style={style}>
        {/* Empty state - no panels registered */}
      </div>
    );
  }

  return (
    <Renderer
      layout={currentLayout}
      panels={panels}
      onLayoutChange={handleLayoutChange}
      onLayoutPersist={handleLayoutPersist}
      className={className}
      style={style}
    />
  );
}
