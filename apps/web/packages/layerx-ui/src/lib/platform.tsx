"use client";

import * as React from "react";

export type Platform = "mobile" | "desktop";
export type PlatformSetting = Platform | "auto";

const PlatformContext = React.createContext<PlatformSetting>("auto");
const ResolvedContext = React.createContext<Platform>("desktop");

/**
 * Controls how LayerX responsive patterns resolve their mobile/desktop
 * variants. "auto" (default) follows the viewport (mobile = <768px).
 * Wrap demos in a fixed value to force a variant regardless of viewport —
 * e.g. inside a phone frame on a desktop docs page.
 */
export function PlatformProvider({
  value = "auto",
  children,
}: {
  value?: PlatformSetting;
  children: React.ReactNode;
}) {
  return <PlatformContext.Provider value={value}>{children}</PlatformContext.Provider>;
}

/** SSR-safe media query hook (mobile = viewport < 768px). */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = React.useState(false);
  React.useEffect(() => {
    const mql = window.matchMedia(query);
    const onChange = () => setMatches(mql.matches);
    onChange();
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, [query]);
  return matches;
}

/**
 * Resolve the current platform. Priority:
 * explicit prop > nearest PlatformProvider > viewport media query.
 */
export function usePlatform(override?: PlatformSetting): Platform {
  const fromContext = React.useContext(PlatformContext);
  const setting = override ?? fromContext;
  const isMobileViewport = useMediaQuery("(max-width: 767px)");
  if (setting === "mobile") return "mobile";
  if (setting === "desktop") return "desktop";
  return isMobileViewport ? "mobile" : "desktop";
}

/** Renders one of two branches by platform. */
export function PlatformSwitch({
  mobile,
  desktop,
  platform,
}: {
  mobile: React.ReactNode;
  desktop: React.ReactNode;
  platform?: PlatformSetting;
}) {
  const resolved = usePlatform(platform);
  return <>{resolved === "mobile" ? mobile : desktop}</>;
}
