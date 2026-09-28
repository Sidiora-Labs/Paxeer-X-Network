'use client';

// ── Capacitor Provider ──────────────────────────────────────────────────────
// Initializes native Capacitor plugins when running inside the Android shell.
// No-ops gracefully in browser. Handles biometric lock on app resume,
// FCM push registration, and app state management.

import { createContext, useContext, useEffect, useState, useCallback, useRef } from 'react';
import {
  isNativeApp,
  getNativePlatform,
  initCapacitor,
  checkNativeBiometric,
  authenticateNativeBiometric,
  initNativePush,
  initAppStateListener,
  onAppStateChange,
  openNativeBrowser,
} from '@/lib/capacitor';

interface CapacitorContextValue {
  isNative: boolean;
  platform: 'android' | 'ios' | 'web';
  biometricAvailable: boolean;
  authenticateBiometric: () => Promise<boolean>;
  openBrowser: (url: string) => Promise<void>;
  fcmToken: string | null;
}

const CapacitorContext = createContext<CapacitorContextValue>({
  isNative: false,
  platform: 'web',
  biometricAvailable: false,
  authenticateBiometric: async () => false,
  openBrowser: async () => {},
  fcmToken: null,
});

export function useCapacitor() {
  return useContext(CapacitorContext);
}

interface Props {
  children: React.ReactNode;
  onBiometricLockRequired?: () => void;
}

export function CapacitorProvider({ children, onBiometricLockRequired }: Props) {
  const [biometricAvailable, setBiometricAvailable] = useState(false);
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

      // Check biometric availability
      const bioInfo = await checkNativeBiometric();
      setBiometricAvailable(bioInfo.available);

      // Init push notifications
      await initNativePush(
        (token) => setFcmToken(token),
        (_data) => {
          // Push payload handled by notification service; do not log in production
        },
      );

      // App state listener for biometric lock on resume
      initAppStateListener();
    })();
  }, [native]);

  // Biometric lock on app resume
  useEffect(() => {
    if (!native || !biometricAvailable) return;

    const cleanup = onAppStateChange((isActive) => {
      if (isActive && onBiometricLockRequired) {
        onBiometricLockRequired();
      }
    });

    return cleanup;
  }, [native, biometricAvailable, onBiometricLockRequired]);

  const authenticateBiometric = useCallback(async () => {
    if (!native || !biometricAvailable) return false;
    return authenticateNativeBiometric();
  }, [native, biometricAvailable]);

  const openBrowser = useCallback(
    async (url: string) => {
      await openNativeBrowser(url);
    },
    [],
  );

  return (
    <CapacitorContext.Provider
      value={{
        isNative: native,
        platform,
        biometricAvailable,
        authenticateBiometric,
        openBrowser,
        fcmToken,
      }}
    >
      {children}
    </CapacitorContext.Provider>
  );
}
