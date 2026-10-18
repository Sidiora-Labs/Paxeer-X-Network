import { useEffect, useRef, useState } from "react";

const SCROLL_EPSILON = 1;
type RTLScrollType = "default" | "negative" | "reverse";

let cachedRTLScrollType: RTLScrollType | null = null;

export function useScroll(deps: any[]) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [showLeftShadow, setShowLeftShadow] = useState(false);
  const [showRightShadow, setShowRightShadow] = useState(false);
  const [isRTL, setIsRTL] = useState(false);

  useEffect(() => {
    if (!scrollRef.current) {
      return;
    }

    const target = scrollRef.current;
    const updateState = () => {
      const direction = window.getComputedStyle(target).direction;
      const rtl = direction === "rtl";
      setIsRTL(rtl);
      const scrollAble = hasHorizontalScroll(target);
      const { fromStart, fromEnd } = getHorizontalScrollOffset(target);
      setShowLeftShadow(scrollAble && fromStart > SCROLL_EPSILON);
      setShowRightShadow(scrollAble && fromEnd > SCROLL_EPSILON);
    };

    updateState();
    target.addEventListener("scroll", updateState);

    const nearestDirElement = target.closest("[dir]") as HTMLElement | null;
    const dirObserveTarget = nearestDirElement || document.documentElement;

    const mutationObserver = new MutationObserver(updateState);
    mutationObserver.observe(dirObserveTarget, {
      attributes: true,
      attributeFilter: ["dir"],
    });

    let resizeObserver: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(updateState);
      resizeObserver.observe(target);
    }

    return () => {
      target.removeEventListener("scroll", updateState);
      mutationObserver.disconnect();
      resizeObserver?.disconnect();
    };
  }, [scrollRef, ...deps]);

  return {
    scrollRef,
    showLeftShadow,
    showRightShadow,
    isRTL,
  };
}

function hasHorizontalScroll(element: HTMLDivElement) {
  return element.scrollWidth - element.clientWidth > SCROLL_EPSILON;
}

function getHorizontalScrollOffset(element: HTMLDivElement) {
  const max = element.scrollWidth - element.clientWidth;
  if (max <= 0) {
    return { fromStart: 0, fromEnd: 0 };
  }

  const direction = window.getComputedStyle(element).direction;

  if (direction !== "rtl") {
    return {
      fromStart: Math.max(0, element.scrollLeft),
      fromEnd: Math.max(0, max - element.scrollLeft),
    };
  }

  const scrollType = getRTLScrollType();
  const scrollLeft = element.scrollLeft;
  let fromStart = 0;

  if (scrollType === "negative") {
    // right(start)=0, move left -> negative values
    fromStart = -scrollLeft;
  } else if (scrollType === "reverse") {
    // right(start)=0, move left -> positive values
    fromStart = scrollLeft;
  } else {
    // right(start)=max, move left -> decreasing to 0
    fromStart = max - scrollLeft;
  }

  return {
    fromStart: Math.max(0, fromStart),
    fromEnd: Math.max(0, max - fromStart),
  };
}

function getRTLScrollType(): RTLScrollType {
  if (cachedRTLScrollType) {
    return cachedRTLScrollType;
  }

  if (typeof document === "undefined" || !document.body) {
    cachedRTLScrollType = "negative";
    return cachedRTLScrollType;
  }

  const outer = document.createElement("div");
  const inner = document.createElement("div");

  outer.style.width = "4px";
  outer.style.height = "1px";
  outer.style.overflow = "scroll";
  outer.style.visibility = "hidden";
  outer.style.position = "absolute";
  outer.style.top = "-9999px";
  outer.dir = "rtl";

  inner.style.width = "8px";
  inner.style.height = "1px";
  outer.appendChild(inner);
  document.body.appendChild(outer);

  let type: RTLScrollType;
  if (outer.scrollLeft > 0) {
    type = "default";
  } else {
    outer.scrollLeft = 1;
    type = outer.scrollLeft === 0 ? "negative" : "reverse";
  }

  document.body.removeChild(outer);
  cachedRTLScrollType = type;
  return type;
}
