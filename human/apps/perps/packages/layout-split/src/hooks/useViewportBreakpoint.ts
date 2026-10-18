import { useState, useEffect } from "react";
import { BREAKPOINT_KEYS, VIEWPORT_BREAKPOINTS } from "../constants";
import type { ViewportBreakpointKey } from "../constants";

/**
 * Returns the current viewport breakpoint key (largest breakpoint whose max width >= viewport width).
 */
export function getViewportBreakpointKey(
  viewportWidth: number,
  breakpoints = VIEWPORT_BREAKPOINTS,
): ViewportBreakpointKey {
  for (const bp of BREAKPOINT_KEYS) {
    // console.log("-------->>>>>>", viewportWidth, breakpoints[bp], bp);
    if (viewportWidth >= breakpoints[bp]) return bp;
  }
  return "xs";
}

export interface UseViewportBreakpointOptions {
  /** Breakpoint map; defaults to VIEWPORT_BREAKPOINTS. */
  breakpoints?: typeof VIEWPORT_BREAKPOINTS;
  /** Initial width when ref not yet mounted. */
  fallbackWidth?: number;
}

/**
 * Observes viewport width and returns current breakpoint key.
 * Uses ResizeObserver on document.documentElement; before mount uses fallbackWidth.
 *
 * @param options - Optional breakpoints and fallbackWidth
 * @returns Current breakpoint key (lg | md | sm | xs)
 */
export function useViewportBreakpoint(
  options: UseViewportBreakpointOptions = {},
): ViewportBreakpointKey {
  const {
    breakpoints = VIEWPORT_BREAKPOINTS,
    fallbackWidth = typeof window !== "undefined" ? window.innerWidth : 1440,
  } = options;
  const [width, setWidth] = useState(fallbackWidth);

  useEffect(() => {
    const element = document.documentElement;
    if (!element) return;

    setWidth(window.innerWidth);

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setWidth(entry.contentRect.width);
      }
    });

    resizeObserver.observe(element);

    return () => resizeObserver.disconnect();
  }, []);

  const bp = getViewportBreakpointKey(width, breakpoints);
  return bp;
}
