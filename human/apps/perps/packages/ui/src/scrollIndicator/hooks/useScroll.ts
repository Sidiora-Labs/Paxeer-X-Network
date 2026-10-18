import { useCallback, useEffect, useRef, useState } from "react";

export type ScrollIndicatorDirection = "leading" | "tailing";

type RtlScrollType = "default" | "negative" | "reverse";

let cachedRtlScrollType: RtlScrollType | undefined;

function getRtlScrollType(): RtlScrollType {
  if (cachedRtlScrollType) {
    return cachedRtlScrollType;
  }

  if (typeof document === "undefined") {
    cachedRtlScrollType = "negative";
    return cachedRtlScrollType;
  }

  const outer = document.createElement("div");
  const inner = document.createElement("div");

  outer.dir = "rtl";
  outer.style.width = "4px";
  outer.style.height = "1px";
  outer.style.overflow = "scroll";
  outer.style.position = "absolute";
  outer.style.top = "-1000px";
  inner.style.width = "8px";
  inner.style.height = "1px";

  outer.appendChild(inner);
  document.body.appendChild(outer);

  if (outer.scrollLeft > 0) {
    cachedRtlScrollType = "default";
  } else {
    outer.scrollLeft = 1;
    cachedRtlScrollType = outer.scrollLeft === 0 ? "negative" : "reverse";
  }

  document.body.removeChild(outer);
  return cachedRtlScrollType;
}

function getMaxScrollLeft(container: HTMLDivElement) {
  return Math.max(container.scrollWidth - container.clientWidth, 0);
}

export function getScrollOffsetFromStart(container: HTMLDivElement) {
  const maxScrollLeft = getMaxScrollLeft(container);

  if (getComputedStyle(container).direction !== "rtl") {
    return container.scrollLeft;
  }

  switch (getRtlScrollType()) {
    case "negative":
      return -container.scrollLeft;
    case "default":
      return maxScrollLeft - container.scrollLeft;
    case "reverse":
      return container.scrollLeft;
  }
}

export function getScrollLeftForOffsetFromStart(
  container: HTMLDivElement,
  offsetFromStart: number,
) {
  const maxScrollLeft = getMaxScrollLeft(container);
  const nextOffset = Math.min(Math.max(offsetFromStart, 0), maxScrollLeft);

  if (getComputedStyle(container).direction !== "rtl") {
    return nextOffset;
  }

  switch (getRtlScrollType()) {
    case "negative":
      return -nextOffset;
    case "default":
      return maxScrollLeft - nextOffset;
    case "reverse":
      return nextOffset;
  }
}

export function useScroll() {
  const [leadingVisible, setLeadingVisible] = useState(false);
  const [tailingVisible, setTailingVisible] = useState(false);
  const [isRTL, setIsRTL] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);

  const updateScrollVisibility = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;

    const maxScrollLeft = getMaxScrollLeft(container);
    const offsetFromStart = getScrollOffsetFromStart(container);
    const nextIsRTL = getComputedStyle(container).direction === "rtl";

    setIsRTL(nextIsRTL);
    setLeadingVisible(offsetFromStart > 1);
    setTailingVisible(offsetFromStart < maxScrollLeft - 1);
  }, []);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    container.addEventListener("scroll", updateScrollVisibility);

    const intersectionObserver = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          updateScrollVisibility();
        }
      });
    });

    const resizeObserver =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(updateScrollVisibility)
        : undefined;

    intersectionObserver.observe(container);
    resizeObserver?.observe(container);
    updateScrollVisibility();

    return () => {
      container.removeEventListener("scroll", updateScrollVisibility);
      intersectionObserver.disconnect();
      resizeObserver?.disconnect();
    };
  }, [updateScrollVisibility]);

  const onScoll = (direction: ScrollIndicatorDirection) => {
    const container = containerRef.current;
    if (!container) return;

    const currentOffset = getScrollOffsetFromStart(container);
    const delta = direction === "leading" ? -100 : 100;
    const left = getScrollLeftForOffsetFromStart(
      container,
      currentOffset + delta,
    );

    container.scrollTo({ left, behavior: "smooth" });
  };

  return { containerRef, leadingVisible, tailingVisible, isRTL, onScoll };
}
