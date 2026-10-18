import { useState, RefObject } from "react";
import {
  getScrollLeftForOffsetFromStart,
  getScrollOffsetFromStart,
} from "./useScroll";

export function useDrag(containerRef: RefObject<HTMLDivElement>) {
  const [isDragging, setIsDragging] = useState(false);
  const [startX, setStartX] = useState(0);
  const [scrollOffsetFromStart, setScrollOffsetFromStart] = useState(0);

  const handleMouseDown = (e: React.MouseEvent) => {
    if (!containerRef.current) return;

    setIsDragging(true);
    setStartX(e.pageX - containerRef.current.offsetLeft);
    setScrollOffsetFromStart(getScrollOffsetFromStart(containerRef.current));
  };

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!isDragging || !containerRef.current) return;

    e.preventDefault();
    const x = e.pageX - containerRef.current.offsetLeft;
    const walk = x - startX;
    containerRef.current.scrollLeft = getScrollLeftForOffsetFromStart(
      containerRef.current,
      scrollOffsetFromStart - walk,
    );
  };

  const handleMouseUp = () => {
    setIsDragging(false);
  };

  return {
    handleMouseDown,
    handleMouseMove,
    handleMouseUp,
    isDragging,
  };
}
