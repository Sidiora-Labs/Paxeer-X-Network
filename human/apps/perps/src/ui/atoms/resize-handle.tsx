/**
 * High-performance resize handle — zero React re-renders during drag.
 *
 * Instead of calling setState every frame, this component directly
 * mutates DOM element styles via targetRef during the drag. Only on
 * mouseup / touchend does it call onResizeEnd with the final pixel
 * value so the parent can commit to React state once.
 *
 * Props:
 *   - direction: "horizontal" | "vertical"
 *   - targetRef: ref to the DOM element whose width/height to resize
 *   - cssProp: the CSS property to set ("width" | "height" | "flex-basis")
 *   - min / max: pixel bounds
 *   - onResizeEnd(finalPx): called once on drag end with the clamped value
 *   - invert: if true, delta is subtracted (for right-side panels)
 */
import { useCallback, useRef, useEffect, type RefObject } from "react";
import { cn } from "@/ui/utils/cn";

interface ResizeHandleProps {
  direction: "horizontal" | "vertical";
  targetRef: RefObject<HTMLElement | null>;
  cssProp?: "width" | "height" | "flexBasis";
  min?: number;
  max?: number;
  onResizeEnd?: (finalPx: number) => void;
  invert?: boolean;
  className?: string;
}

export function ResizeHandle({
  direction,
  targetRef,
  cssProp = direction === "horizontal" ? "width" : "height",
  min = 0,
  max = 9999,
  onResizeEnd,
  invert = false,
  className,
}: ResizeHandleProps) {
  const handleRef = useRef<HTMLDivElement>(null);
  const isDragging = useRef(false);
  const startPos = useRef(0);
  const startSize = useRef(0);
  const rafId = useRef(0);
  const pendingPos = useRef(0);
  const onEndRef = useRef(onResizeEnd);
  onEndRef.current = onResizeEnd;

  const clamp = useCallback((v: number) => Math.min(max, Math.max(min, v)), [min, max]);

  const applySize = useCallback(() => {
    const el = targetRef.current;
    if (!el || !isDragging.current) return;
    const delta = invert
      ? startPos.current - pendingPos.current
      : pendingPos.current - startPos.current;
    const newSize = clamp(startSize.current + delta);
    el.style[cssProp] = `${newSize}px`;
  }, [targetRef, cssProp, clamp, invert]);

  const startDrag = useCallback(
    (clientPos: number) => {
      const el = targetRef.current;
      if (!el) return;
      isDragging.current = true;
      startPos.current = clientPos;
      pendingPos.current = clientPos;
      const rect = el.getBoundingClientRect();
      startSize.current = direction === "horizontal" ? rect.width : rect.height;
      document.body.style.cursor = direction === "horizontal" ? "col-resize" : "row-resize";
      document.body.style.userSelect = "none";
      document.body.style.webkitUserSelect = "none";
      // Block iframes from stealing pointer events during drag
      document.querySelectorAll("iframe").forEach((f) => {
        (f as HTMLIFrameElement).style.pointerEvents = "none";
      });
      handleRef.current?.classList.add("resize-active");
    },
    [direction, targetRef]
  );

  const onMove = useCallback(
    (e: MouseEvent | TouchEvent) => {
      if (!isDragging.current) return;
      let pos: number;
      if ("touches" in e) {
        const t = e.touches[0] || e.changedTouches[0];
        pos = direction === "horizontal" ? t.clientX : t.clientY;
      } else {
        pos = direction === "horizontal" ? e.clientX : e.clientY;
      }
      pendingPos.current = pos;
      if (!rafId.current) {
        rafId.current = requestAnimationFrame(() => {
          rafId.current = 0;
          applySize();
        });
      }
    },
    [direction, applySize]
  );

  const endDrag = useCallback(() => {
    if (!isDragging.current) return;
    isDragging.current = false;
    if (rafId.current) {
      cancelAnimationFrame(rafId.current);
      rafId.current = 0;
    }
    // Apply final position synchronously
    applySize();
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
    document.body.style.webkitUserSelect = "";
    document.querySelectorAll("iframe").forEach((f) => {
      (f as HTMLIFrameElement).style.pointerEvents = "";
    });
    handleRef.current?.classList.remove("resize-active");
    const el = targetRef.current;
    if (el) {
      const rect = el.getBoundingClientRect();
      const finalPx = direction === "horizontal" ? rect.width : rect.height;
      onEndRef.current?.(Math.round(finalPx));
    }
  }, [direction, targetRef, applySize]);

  useEffect(() => {
    const el = handleRef.current;
    if (!el) return;

    const onMouseDown = (e: MouseEvent) => {
      e.preventDefault();
      startDrag(direction === "horizontal" ? e.clientX : e.clientY);
    };
    const onTouchStart = (e: TouchEvent) => {
      const t = e.touches[0];
      startDrag(direction === "horizontal" ? t.clientX : t.clientY);
    };

    el.addEventListener("mousedown", onMouseDown);
    el.addEventListener("touchstart", onTouchStart, { passive: true });
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", endDrag);
    window.addEventListener("touchmove", onMove, { passive: true });
    window.addEventListener("touchend", endDrag);
    window.addEventListener("touchcancel", endDrag);

    return () => {
      el.removeEventListener("mousedown", onMouseDown);
      el.removeEventListener("touchstart", onTouchStart);
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", endDrag);
      window.removeEventListener("touchmove", onMove);
      window.removeEventListener("touchend", endDrag);
      window.removeEventListener("touchcancel", endDrag);
      if (rafId.current) cancelAnimationFrame(rafId.current);
    };
  }, [direction, startDrag, onMove, endDrag]);

  return (
    <div
      ref={handleRef}
      className={cn(
        "flex-shrink-0 relative touch-none select-none group",
        direction === "horizontal"
          ? "w-[4px] cursor-col-resize"
          : "h-[4px] cursor-row-resize",
        className
      )}
    >
      {/* Extended invisible hit area — 20px grab zone */}
      <div
        className={cn(
          "absolute z-10",
          direction === "horizontal"
            ? "top-0 bottom-0 -left-[8px] -right-[8px] cursor-col-resize"
            : "left-0 right-0 -top-[8px] -bottom-[8px] cursor-row-resize"
        )}
      />
      {/* Visible indicator line */}
      <div
        className={cn(
          "absolute transition-all duration-100",
          "bg-border/40 group-hover:bg-primary/40",
          direction === "horizontal"
            ? "w-px h-6 left-1/2 -translate-x-1/2 top-1/2 -translate-y-1/2 group-hover:h-10 group-hover:w-[2px]"
            : "h-px w-6 top-1/2 -translate-y-1/2 left-1/2 -translate-x-1/2 group-hover:w-10 group-hover:h-[2px]"
        )}
      />
    </div>
  );
}
