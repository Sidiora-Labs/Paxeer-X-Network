import { useEffect, useRef } from "react";
import { useThemeAttribute } from "../../hooks";

export function useWrap(deps: any[]) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const themeAttribute = useThemeAttribute();

  useEffect(() => {
    if (!wrapRef.current) return;

    const bgColor = window.getComputedStyle(wrapRef.current).backgroundColor;

    wrapRef.current.style.setProperty("--oui-table-background-color", bgColor);
  }, [...deps, themeAttribute]);

  return wrapRef;
}
