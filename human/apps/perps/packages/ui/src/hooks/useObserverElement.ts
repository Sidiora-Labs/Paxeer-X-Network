import { useEffect } from "react";

/**
 * observe the element and call the callback when the element is changed
 * */
export function useObserverElement<T extends HTMLElement>(
  element: T | null,
  callback: (entry: ResizeObserverEntry) => void,
) {
  useEffect(() => {
    if (!element) {
      return;
    }

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        callback(entry);
      }
    });

    resizeObserver.observe(element);

    return () => {
      resizeObserver.unobserve(element);
    };
  }, [element]);
}
