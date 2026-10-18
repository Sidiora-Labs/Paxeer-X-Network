import { useState, useEffect, useCallback } from "react";
import {
  SPLIT_BREAKPOINT_ORDER,
  DEFAULT_SPLIT_BREAKPOINTS,
} from "../constants";
import type { SplitLayoutBreakpointKey } from "../types";

/**
 * Returns the current breakpoint key for a given width (largest breakpoint whose min width <= width).
 */
export function getBreakpointKeyForWidth(
  width: number,
  breakpoints = DEFAULT_SPLIT_BREAKPOINTS,
): SplitLayoutBreakpointKey {
  for (const bp of SPLIT_BREAKPOINT_ORDER) {
    if (width >= breakpoints[bp]) return bp;
  }
  return "xs";
}

export interface UseBreakpointFromWidthOptions {
  /** Breakpoint map; defaults to DEFAULT_SPLIT_BREAKPOINTS. */
  breakpoints?: typeof DEFAULT_SPLIT_BREAKPOINTS;
  /** Initial width when ref not yet mounted (e.g. window.innerWidth). */
  fallbackWidth?: number;
}

/**
 * Observes container width and returns current breakpoint key.
 * Uses ResizeObserver on containerRef.current; before mount or when ref is null uses fallbackWidth.
 *
 * @param containerRef - Ref to the layout container element
 * @param options - Optional breakpoints and fallbackWidth
 * @returns Current breakpoint key (lg | md | sm | xs)
 */
export function useBreakpointFromWidth(
  containerRef: React.RefObject<HTMLElement | null>,
  options: UseBreakpointFromWidthOptions = {},
): SplitLayoutBreakpointKey {
  const { breakpoints = DEFAULT_SPLIT_BREAKPOINTS, fallbackWidth = 1200 } =
    options;
  const [width, setWidth] = useState(fallbackWidth);

  const updateWidth = useCallback(() => {
    const el = containerRef.current;
    setWidth(el ? el.offsetWidth : fallbackWidth);
  }, [containerRef, fallbackWidth]);

  useEffect(() => {
    const el = containerRef.current;
    // Initial measurement + observer: width must sync to DOM after mount.
    /* eslint-disable-next-line react-hooks/set-state-in-effect -- ResizeObserver needs baseline width before subscribe */
    updateWidth();
    if (!el) {
      return;
    }
    const ro = new ResizeObserver(() => updateWidth());
    ro.observe(el);
    return () => ro.disconnect();
  }, [containerRef, updateWidth]);

  const bp = getBreakpointKeyForWidth(width, breakpoints);
  return bp;
}
