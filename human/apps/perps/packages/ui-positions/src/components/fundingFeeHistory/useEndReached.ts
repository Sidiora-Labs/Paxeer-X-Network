import { useEffect, useRef, MutableRefObject } from "react";

/**
 * Listen for the specified element to scroll to the bottom
 */
export function useEndReached(
  sentinelRef: MutableRefObject<HTMLDivElement | null>,
  onEndReached?: () => void,
) {
  const observer = useRef<IntersectionObserver>();
  const cb = useRef(onEndReached);

  cb.current = onEndReached;

  useEffect(() => {
    const options: IntersectionObserverInit = {
      root: null,
      rootMargin: "0px",
      threshold: 0,
    };

    const handleObserver = (entries: IntersectionObserverEntry[]) => {
      console.log(">>>>>>>>handleObserver", entries);
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          cb.current?.();
        }
      });
    };

    observer.current = new IntersectionObserver(handleObserver, options);

    return () => {
      // console.log("------->disconnect");
      observer.current?.disconnect();
    };
  }, []);

  useEffect(() => {
    observer.current?.observe(sentinelRef.current!);
  }, []);
}
