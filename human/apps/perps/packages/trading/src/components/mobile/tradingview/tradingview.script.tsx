import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { TradingviewWidgetProps } from "./tradingview.widget";

const MaxHeight = 354;
const MinHeight = 234;
const Key = "TRADINGVIEW_MOBILE_HEIGHT";

export function useTradingviewScript(props: TradingviewWidgetProps) {
  const [height, setHeight] = useLocalStorage(Key, MaxHeight);
  const [dragging, setDragging] = useState(false);
  const dragRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const [offsetY, setOffsetY] = useState(0);
  const topRef = useRef<number>(0);

  const handleTouchStart = useCallback((e: TouchEvent) => {
    if (!boxRef.current) {
      return;
    }
    setDragging(true);
    const event = e.touches[0];
    const rect = boxRef.current.getBoundingClientRect();
    e.stopPropagation();
    e.preventDefault();

    const offsetY = event.clientY - rect.bottom;
    topRef.current = rect.top;
    setOffsetY(offsetY);
  }, []);

  const handleTouchMove = useCallback(
    (e: TouchEvent) => {
      if (!boxRef.current) {
        return;
      }
      if (dragging) {
        e.stopPropagation();
        e.preventDefault();

        const event = e.touches[0];

        const newHeight = event.clientY - topRef.current - offsetY;
        setHeight(
          Math.min(Math.max(Math.round(newHeight), MinHeight), MaxHeight),
        );
        return false;
      }
    },
    [dragging, offsetY],
  );

  const handleTouchEnd = useCallback(() => {
    setDragging(false);
  }, []);

  useEffect(() => {
    const drag = dragRef.current;
    if (!drag) {
      return;
    }

    drag.addEventListener("touchstart", handleTouchStart);

    return () => {
      drag.removeEventListener("touchstart", handleTouchStart);
    };
  }, [handleTouchStart]);

  useEffect(() => {
    document.addEventListener("touchmove", handleTouchMove, { passive: false });
    document.addEventListener("touchend", handleTouchEnd);

    return () => {
      document.removeEventListener("touchmove", handleTouchMove);
      document.removeEventListener("touchend", handleTouchEnd);
    };
  }, [dragging, handleTouchMove, handleTouchEnd]);
  return {
    ...props,

    height,
    dragging,
    dragRef,
    boxRef,
  };
}

export type TradingviewState = ReturnType<typeof useTradingviewScript>;
