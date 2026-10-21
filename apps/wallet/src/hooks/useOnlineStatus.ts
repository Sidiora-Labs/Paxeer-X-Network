'use client';

import { useEffect, useState } from 'react';

/**
 * Returns true when the browser has network connectivity.
 * Listens to the `online` / `offline` window events so the value
 * updates in real-time without polling.
 *
 * Always returns `true` during SSR (no window object).
 */
export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState(() =>
    typeof navigator !== 'undefined' ? navigator.onLine : true,
  );

  useEffect(() => {
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  return online;
}
