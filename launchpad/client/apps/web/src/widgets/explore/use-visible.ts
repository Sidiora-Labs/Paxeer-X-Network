'use client';
import { useEffect, useState, useSyncExternalStore, type RefObject } from 'react';

/** True once the element has entered the viewport (plus margin); stays true afterwards so cached data is never dropped. */
export function useVisible<T extends Element>(ref: RefObject<T | null>, rootMargin = '200px'): boolean {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (visible) return;
    const node = ref.current;
    if (!node) return;
    if (typeof IntersectionObserver === 'undefined') { setVisible(true); return; }
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { setVisible(true); observer.disconnect(); }
    }, { rootMargin });
    observer.observe(node);
    return () => observer.disconnect();
  }, [ref, rootMargin, visible]);
  return visible;
}

function subscribeMedia(query: string, onChange: () => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => undefined;
  const media = window.matchMedia(query);
  media.addEventListener('change', onChange);
  return () => media.removeEventListener('change', onChange);
}

/** Viewport media query; the server snapshot assumes the wide layout so the table markup is what hydrates. */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    onChange => subscribeMedia(query, onChange),
    () => (typeof window !== 'undefined' && typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : true),
    () => true,
  );
}
