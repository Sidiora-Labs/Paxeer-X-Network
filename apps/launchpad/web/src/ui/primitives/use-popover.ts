'use client';

import { useCallback, useEffect, useLayoutEffect, useState, type RefObject } from 'react';

export interface PopoverPosition {
  top: number;
  left: number;
  minWidth: number;
}

const useIsoLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect;

/** Fixed-position anchor placement for portalled layers. Flips above the anchor when it would overflow the viewport. */
export function usePopoverPosition(
  anchorRef: RefObject<HTMLElement | null>,
  layerRef: RefObject<HTMLElement | null>,
  open: boolean,
  align: 'start' | 'end' | 'center' = 'start',
  offset = 4,
): PopoverPosition | null {
  const [position, setPosition] = useState<PopoverPosition | null>(null);

  const measure = useCallback(() => {
    const anchor = anchorRef.current;
    if (!anchor) return;
    const rect = anchor.getBoundingClientRect();
    const layer = layerRef.current;
    const layerWidth = layer?.offsetWidth ?? rect.width;
    const layerHeight = layer?.offsetHeight ?? 0;
    const viewportW = window.innerWidth;
    const viewportH = window.innerHeight;
    let left = align === 'end' ? rect.right - layerWidth : align === 'center' ? rect.left + rect.width / 2 - layerWidth / 2 : rect.left;
    left = Math.max(8, Math.min(left, viewportW - layerWidth - 8));
    let top = rect.bottom + offset;
    if (layerHeight && top + layerHeight > viewportH - 8 && rect.top - offset - layerHeight > 8) {
      top = rect.top - offset - layerHeight;
    }
    setPosition({ top, left, minWidth: rect.width });
  }, [anchorRef, layerRef, align, offset]);

  useIsoLayoutEffect(() => {
    if (!open) {
      setPosition(null);
      return;
    }
    measure();
    const onChange = () => measure();
    window.addEventListener('resize', onChange);
    window.addEventListener('scroll', onChange, true);
    return () => {
      window.removeEventListener('resize', onChange);
      window.removeEventListener('scroll', onChange, true);
    };
  }, [open, measure]);

  return position;
}

export function useMounted(): boolean {
  const [mounted, setMounted] = useState(false);
  useEffect(() => { setMounted(true); }, []);
  return mounted;
}

/** Closes when a pointer event lands outside both refs, or on Escape. */
export function useDismiss(open: boolean, onDismiss: () => void, refs: Array<RefObject<HTMLElement | null>>) {
  useEffect(() => {
    if (!open) return;
    function onPointer(event: PointerEvent) {
      const target = event.target as Node | null;
      if (!target) return;
      if (refs.some(ref => ref.current?.contains(target))) return;
      onDismiss();
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.stopPropagation();
        onDismiss();
      }
    }
    document.addEventListener('pointerdown', onPointer, true);
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('pointerdown', onPointer, true);
      document.removeEventListener('keydown', onKey, true);
    };
  }, [open, onDismiss, refs]);
}

let idCounter = 0;
export function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}
