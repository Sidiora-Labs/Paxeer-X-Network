'use client';

import { useState, useEffect, useRef } from 'react';

/**
 * Returns a debounced version of `value`.
 * The debounced value only updates after `delay` ms of inactivity.
 */
export function useDebouncedValue<T>(value: T, delay = 250): T {
  const [debounced, setDebounced] = useState(value);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    timer.current = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer.current);
  }, [value, delay]);

  return debounced;
}
