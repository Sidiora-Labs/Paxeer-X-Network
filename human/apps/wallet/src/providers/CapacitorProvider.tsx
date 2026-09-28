'use client';

// ── Capacitor Provider ──────────────────────────────────────────────────────
// Initializes native Capacitor plugins when running inside the Android shell.
// No-ops gracefully in browser. Handles FCM push registration and app state
// management.

import { createContext, useContext, useEffect, useRef, useState } from 'react';
import {
  isNativeApp,
  getNativePlatform,
  initCapacitor,
  initNativePush,
  initAppStateListener,
} from '@/lib/capacitor';

interface CapacitorContextValue {
  isNative: boolean;
  platform: 'android' | 'ios' | 'web';
  fcmToken: string | null;
}

const CapacitorContext = createContext<CapacitorContextValue>({
  isNative: false,
  platform: 'web',
  fcmToken: null,
});

export function useCapacitor() {
  return useContext(CapacitorContext);
}

interface Props {
  children: React.ReactNode;
}

export function CapacitorProvider({ children }: Props) {
  const [fcmToken, setFcmToken] = useState<string | null>(null);
  const native = isNativeApp();
  const platform = getNativePlatform();
  const initializedRef = useRef(false);

  // Initialize Capacitor on mount
  useEffect(() => {
    if (!native || initializedRef.current) return;
    initializedRef.current = true;

    (async () => {
      await initCapacitor();

      // Init push notifications
      await initNativePush(
        (token) => setFcmToken(token),
        (_data) => {
          // Push payload handled by notification service; do not log in production
        },
      );

      initAppStateListener();
    })();
  }, [native]);

  return (
    <CapacitorContext.Provider
      value={{
        isNative: native,
        platform,
        fcmToken,
      }}
    >
      {children}
    </CapacitorContext.Provider>
  );
}
