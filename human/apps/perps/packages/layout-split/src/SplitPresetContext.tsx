/**
 * Context for split layout configuration.
 * Provides showIndicator for sort indicator visibility and classNames.
 * Note: The preset system has been deprecated in favor of fixed layout approach.
 */
import React, { createContext, useContext } from "react";
import type { SplitLayoutClassNames } from "./types";

export interface SplitPresetContextValue {
  /** Optional classNames for panel group, panel, and handle (from plugin options). */
  classNames?: SplitLayoutClassNames;
  /** Optional gap between panels in px (similar to Tailwind gap-*; applied to handle margin). */
  gap?: number;
  /** When true, show drag handle on sortable panels; when false, hide it. */
  showIndicator?: boolean;
}

const SplitPresetContext = createContext<SplitPresetContextValue | null>(null);

export interface SplitPresetProviderProps {
  /** Optional classNames for panel group, panel, and handle (from plugin options). */
  classNames?: SplitLayoutClassNames;
  /** Optional gap between panels in px (similar to Tailwind gap-*). */
  gap?: number;
  /** Optional showIndicator for sort indicator visibility. */
  showIndicator?: boolean;
  children: React.ReactNode;
}

/**
 * Provides basic split layout configuration.
 * Note: The preset system has been deprecated.
 */
export function SplitPresetProvider({
  classNames,
  gap,
  showIndicator,
  children,
}: SplitPresetProviderProps): React.ReactElement {
  const value: SplitPresetContextValue = {
    classNames: classNames ?? undefined,
    gap: gap ?? undefined,
    showIndicator,
  };

  return (
    <SplitPresetContext.Provider value={value}>
      {children}
    </SplitPresetContext.Provider>
  );
}

/* eslint-disable-next-line react-refresh/only-export-components -- hook co-located with provider */
export function useSplitPresetContext(): SplitPresetContextValue | null {
  return useContext(SplitPresetContext);
}
