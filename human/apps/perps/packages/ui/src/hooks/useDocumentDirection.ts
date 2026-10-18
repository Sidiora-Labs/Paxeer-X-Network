import { useCallback, useSyncExternalStore } from "react";

type Direction = "ltr" | "rtl";

function subscribeDocumentDir(listener: () => void) {
  if (typeof document === "undefined") {
    return () => {};
  }
  const observer = new MutationObserver(listener);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["dir"],
  });
  return () => observer.disconnect();
}

function getDocumentDirection(): Direction {
  if (typeof document === "undefined") {
    return "ltr";
  }
  return document.documentElement.dir === "rtl" ? "rtl" : "ltr";
}

export function useDocumentDirection(): Direction {
  return useSyncExternalStore(
    subscribeDocumentDir,
    getDocumentDirection,
    () => "ltr" as Direction,
  );
}
