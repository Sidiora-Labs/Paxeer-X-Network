import { useRef, useCallback } from "react";

interface LongPressHandlers {
  onMouseDown: (e: React.MouseEvent) => void;
  onMouseUp: () => void;
  onMouseMove: (e: React.MouseEvent) => void;
  onMouseLeave: () => void;
  onContextMenu: (e: React.MouseEvent) => void;
  onTouchStart: (e: React.TouchEvent) => void;
  onTouchEnd: () => void;
  onTouchMove: (e: React.TouchEvent) => void;
  onTouchCancel: () => void;
}

export function useLongPress(
  callback: () => void,
  longPressTime = 600, // milliseconds
): LongPressHandlers {
  const timeoutRef = useRef<NodeJS.Timeout | null>(null);
  const targetRef = useRef<EventTarget | null>(null);
  const startPositionRef = useRef<{ x: number; y: number } | null>(null);
  const isLongPressTriggeredRef = useRef(false);

  const clearLongPress = useCallback(() => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
    targetRef.current = null;
    startPositionRef.current = null;
    isLongPressTriggeredRef.current = false;
  }, []);

  const startLongPress = useCallback(
    (e: React.MouseEvent | React.TouchEvent) => {
      // Clear previous timer
      clearLongPress();

      // Prevent default behavior (disable browser default menu)
      if ("touches" in e) {
        e.preventDefault();
      }

      // Record target element and start position
      targetRef.current = e.currentTarget;
      const clientX = "touches" in e ? e.touches[0]?.clientX : e.clientX;
      const clientY = "touches" in e ? e.touches[0]?.clientY : e.clientY;

      startPositionRef.current = { x: clientX, y: clientY };
      isLongPressTriggeredRef.current = false;

      // Set long press timer
      timeoutRef.current = setTimeout(() => {
        if (targetRef.current && startPositionRef.current) {
          isLongPressTriggeredRef.current = true;
          callback();
        }
        clearLongPress();
      }, longPressTime);
    },
    [callback, longPressTime, clearLongPress],
  );

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      // Only handle left button click
      if (e.button !== 0) return;
      startLongPress(e);
    },
    [startLongPress],
  );

  const handleMouseUp = useCallback(() => {
    clearLongPress();
  }, [clearLongPress]);

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      // If long press is already triggered, ignore movement
      if (isLongPressTriggeredRef.current) return;

      // Check if movement exceeds threshold (5px)
      if (startPositionRef.current) {
        const deltaX = Math.abs(e.clientX - startPositionRef.current.x);
        const deltaY = Math.abs(e.clientY - startPositionRef.current.y);
        const threshold = 5;

        if (deltaX > threshold || deltaY > threshold) {
          clearLongPress();
        }
      }
    },
    [clearLongPress],
  );

  const handleMouseLeave = useCallback(() => {
    clearLongPress();
  }, [clearLongPress]);

  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    // If long press is in progress, prevent context menu
    if (timeoutRef.current || isLongPressTriggeredRef.current) {
      e.preventDefault();
      e.stopPropagation();
    }
  }, []);

  const handleTouchStart = useCallback(
    (e: React.TouchEvent) => {
      startLongPress(e);
    },
    [startLongPress],
  );

  const handleTouchEnd = useCallback(() => {
    clearLongPress();
  }, [clearLongPress]);

  const handleTouchMove = useCallback(
    (e: React.TouchEvent) => {
      // If long press is in progress, prevent default behavior (disable long press menu)
      if (timeoutRef.current) {
        e.preventDefault();
      }

      // If long press is already triggered, ignore movement
      if (isLongPressTriggeredRef.current) return;

      // Check if movement exceeds threshold (5px)
      if (startPositionRef.current && e.touches[0]) {
        const deltaX = Math.abs(
          e.touches[0].clientX - startPositionRef.current.x,
        );
        const deltaY = Math.abs(
          e.touches[0].clientY - startPositionRef.current.y,
        );
        const threshold = 5;

        if (deltaX > threshold || deltaY > threshold) {
          clearLongPress();
        }
      }
    },
    [clearLongPress],
  );

  const handleTouchCancel = useCallback(() => {
    clearLongPress();
  }, [clearLongPress]);

  return {
    onMouseDown: handleMouseDown,
    onMouseUp: handleMouseUp,
    onMouseMove: handleMouseMove,
    onMouseLeave: handleMouseLeave,
    onContextMenu: handleContextMenu,
    onTouchStart: handleTouchStart,
    onTouchEnd: handleTouchEnd,
    onTouchMove: handleTouchMove,
    onTouchCancel: handleTouchCancel,
  };
}
