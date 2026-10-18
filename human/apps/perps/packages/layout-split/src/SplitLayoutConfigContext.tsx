/**
 * Context for split layout configuration (panels, layout, breakpoint, callbacks).
 * Provides configuration to SplitNodeRenderer without prop drilling.
 */
import React, { createContext, useContext, useMemo } from "react";
import type { PanelRegistry } from "@paxeer-network/layout-core";
import type { SplitLayoutClassNames, SplitLayoutModel } from "./types";

export interface SplitLayoutConfigValue {
  /** Registry of panel components (id -> React.ReactNode). */
  panels: PanelRegistry;
  /** Current layout model with breakpoint roots. */
  layout: SplitLayoutModel;
  /** Current breakpoint key (e.g. "md", "sm"). */
  breakpoint: keyof SplitLayoutModel["layouts"];
  /** Callback when layout model changes. */
  onLayoutChange: (layout: SplitLayoutModel) => void;
  /** Callback when panel sizes change at a path. */
  onSizeChange: (path: number[], sizes: string[]) => void;
  /** Callback when panel sizes should be persisted (e.g., on resize end). */
  onSizePersist?: (path: number[], sizes: string[]) => void;
  /** Optional classNames for panel group, panel, and handle (from plugin options). */
  classNames?: SplitLayoutClassNames;
  /** Optional gap between panels in px (from plugin options). */
  gap?: number;
  /** Set of collapsed panel IDs. */
  collapsedPanels: Set<string>;
  /** Toggle collapse state for a panel. */
  togglePanelCollapse: (panelId: string) => void;
  /** Check if a panel is collapsed. */
  isPanelCollapsed: (panelId: string) => boolean;
  /** Map of panel IDs that are collapsible. */
  collapsiblePanels: Map<string, boolean>;
  /** Check if a panel is collapsible. */
  isPanelCollapsible: (panelId: string) => boolean;
}

const SplitLayoutConfigContext = createContext<SplitLayoutConfigValue | null>(
  null,
);

export interface SplitLayoutConfigProviderProps {
  /** Registry of panel components. */
  panels: PanelRegistry;
  /** Current layout model. */
  layout: SplitLayoutModel;
  /** Current breakpoint key. */
  breakpoint: keyof SplitLayoutModel["layouts"];
  /** Callback when layout model changes. */
  onLayoutChange: (layout: SplitLayoutModel) => void;
  /** Callback when panel sizes change. */
  onSizeChange: (path: number[], sizes: string[]) => void;
  /** Callback when panel sizes should be persisted. */
  onSizePersist?: (path: number[], sizes: string[]) => void;
  /** Optional classNames for panel group, panel, and handle. */
  classNames?: SplitLayoutClassNames;
  /** Optional gap between panels in px. */
  gap?: number;
  /** Set of collapsed panel IDs. */
  collapsedPanels: Set<string>;
  /** Toggle collapse state for a panel. */
  togglePanelCollapse: (panelId: string) => void;
  /** Check if a panel is collapsed. */
  isPanelCollapsed: (panelId: string) => boolean;
  /** Map of panel IDs that are collapsible. */
  collapsiblePanels: Map<string, boolean>;
  /** Check if a panel is collapsible. */
  isPanelCollapsible: (panelId: string) => boolean;
  children: React.ReactNode;
}

/**
 * Provides layout configuration to SplitNodeRenderer.
 * Use at the root of the split layout renderer.
 */
export function SplitLayoutConfigProvider({
  panels,
  layout,
  breakpoint,
  onLayoutChange,
  onSizeChange,
  onSizePersist,
  classNames,
  gap,
  collapsedPanels,
  togglePanelCollapse,
  isPanelCollapsed,
  collapsiblePanels,
  isPanelCollapsible,
  children,
}: SplitLayoutConfigProviderProps): React.ReactElement {
  const value = useMemo<SplitLayoutConfigValue>(
    () => ({
      panels,
      layout,
      breakpoint,
      onLayoutChange,
      onSizeChange,
      onSizePersist,
      classNames,
      gap,
      collapsedPanels,
      togglePanelCollapse,
      isPanelCollapsed,
      collapsiblePanels,
      isPanelCollapsible,
    }),
    [
      panels,
      layout,
      breakpoint,
      onLayoutChange,
      onSizeChange,
      onSizePersist,
      classNames,
      gap,
      collapsedPanels,
      togglePanelCollapse,
      isPanelCollapsed,
      collapsiblePanels,
      isPanelCollapsible,
    ],
  );

  return (
    <SplitLayoutConfigContext.Provider value={value}>
      {children}
    </SplitLayoutConfigContext.Provider>
  );
}

/**
 * Hook to read layout config from context.
 * Throws if used outside SplitLayoutConfigProvider.
 */
// Hook is co-located with its provider (library pattern).
/* eslint-disable-next-line react-refresh/only-export-components */
export function useSplitLayoutConfig(): SplitLayoutConfigValue {
  const ctx = useContext(SplitLayoutConfigContext);
  if (!ctx) {
    throw new Error(
      "useSplitLayoutConfig must be used within SplitLayoutConfigProvider",
    );
  }
  return ctx;
}
