'use client';

import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import {
  isPushSupported,
  isNotificationSupported,
  getNotificationPermission,
  requestNotificationPermission,
  subscribeToPush,
  unsubscribeFromPush,
  getCurrentSubscription,
} from '@/lib/push';
import { pwaDismissalsRepository } from '@/platform/storage/repositories';
import { initCurrencyService } from '@/lib/currency';

// ── Types ───────────────────────────────────────────────────────────────────

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

interface PWAState {
  isInstalled: boolean;
  isInstallable: boolean;
  isStandalone: boolean;
  pushSupported: boolean;
  notificationPermission: NotificationPermission | 'unsupported';
  isSubscribed: boolean;
  subscription: PushSubscription | null;
  updateAvailable: boolean;
}

interface PWAActions {
  promptInstall: () => Promise<boolean>;
  requestPushPermission: () => Promise<NotificationPermission>;
  subscribePush: () => Promise<PushSubscription | null>;
  unsubscribePush: () => Promise<boolean>;
  dismissInstallPrompt: () => void;
  dismissNotificationPrompt: () => void;
  applyUpdate: () => void;
}

interface PWAUIState {
  showInstallPrompt: boolean;
  showNotificationPrompt: boolean;
  showFloatingIcon: boolean;
}

type PWAContextValue = PWAState & PWAActions & PWAUIState;

const PWAContext = createContext<PWAContextValue | null>(null);

// ── Storage Keys ────────────────────────────────────────────────────────────

const INSTALL_DISMISS_DAYS = 7;
const NOTIF_DISMISS_DAYS = 3;

function isDismissed(
  kind: 'installAt' | 'notificationAt',
  days: number,
): boolean {
  const timestamp = pwaDismissalsRepository.read()[kind];
  if (timestamp === null) return false;
  return Date.now() - timestamp < days * 24 * 60 * 60 * 1000;
}

function setDismissed(kind: 'installAt' | 'notificationAt') {
  pwaDismissalsRepository.update((current) => ({
    ...current,
    [kind]: Date.now(),
  }));
}

// ── Provider ────────────────────────────────────────────────────────────────

export function PWAProvider({ children }: { children: React.ReactNode }) {
  const deferredPromptRef = useRef<BeforeInstallPromptEvent | null>(null);

  const [isInstalled, setIsInstalled] = useState(false);
  const [isInstallable, setIsInstallable] = useState(false);
  const [isStandalone, setIsStandalone] = useState(false);
  const [pushSupported, setPushSupported] = useState(false);
  const [notificationPermission, setNotificationPermission] = useState<NotificationPermission | 'unsupported'>('unsupported');
  const [isSubscribed, setIsSubscribed] = useState(false);
  const [subscription, setSubscription] = useState<PushSubscription | null>(null);

  const [showInstallPrompt, setShowInstallPrompt] = useState(false);
  const [showNotificationPrompt, setShowNotificationPrompt] = useState(false);
  const [showFloatingIcon, setShowFloatingIcon] = useState(false);
  const [updateAvailable, setUpdateAvailable] = useState(false);
  const waitingWorkerRef = useRef<ServiceWorker | null>(null);

  // ── Detect standalone / installed ──────────────────────────────────────
  useEffect(() => {
    const standalone =
      window.matchMedia('(display-mode: standalone)').matches ||
      (navigator as any).standalone === true;
    setIsStandalone(standalone);
    setIsInstalled(standalone);

    // Listen for display mode changes
    const mq = window.matchMedia('(display-mode: standalone)');
    const handler = (e: MediaQueryListEvent) => {
      setIsStandalone(e.matches);
      setIsInstalled(e.matches);
    };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  // ── Initialize currency conversion service ─────────────────────────────
  useEffect(() => {
    initCurrencyService();
  }, []);

  // ── Detect push support and current subscription ───────────────────────
  useEffect(() => {
    const supported = isPushSupported();
    setPushSupported(supported);
    setNotificationPermission(getNotificationPermission());

    if (supported) {
      getCurrentSubscription().then((sub) => {
        setSubscription(sub);
        setIsSubscribed(!!sub);
      });
    }
  }, []);

  // ── Listen for beforeinstallprompt ─────────────────────────────────────
  useEffect(() => {
    const handler = (e: Event) => {
      e.preventDefault();
      deferredPromptRef.current = e as BeforeInstallPromptEvent;
      setIsInstallable(true);

      // Show install banner after a short delay if not dismissed
      if (!isDismissed('installAt', INSTALL_DISMISS_DAYS) && !isStandalone) {
        setTimeout(() => setShowInstallPrompt(true), 3000);
      }
    };

    window.addEventListener('beforeinstallprompt', handler);

    // Detect successful install
    window.addEventListener('appinstalled', () => {
      setIsInstalled(true);
      setIsInstallable(false);
      setShowInstallPrompt(false);
      setShowFloatingIcon(false);
      deferredPromptRef.current = null;
    });

    return () => window.removeEventListener('beforeinstallprompt', handler);
  }, [isStandalone]);

  // ── Show notification prompt after install or if already installed ─────
  useEffect(() => {
    if (!pushSupported) return;
    if (notificationPermission !== 'default') return;
    if (isDismissed('notificationAt', NOTIF_DISMISS_DAYS)) return;

    // Show notification prompt after 10 seconds
    const timer = setTimeout(() => {
      setShowNotificationPrompt(true);
    }, 10_000);

    return () => clearTimeout(timer);
  }, [pushSupported, notificationPermission]);

  // ── Show floating icon when installable and not showing banner ─────────
  useEffect(() => {
    if (isInstallable && !isStandalone && !showInstallPrompt) {
      setShowFloatingIcon(true);
    } else {
      setShowFloatingIcon(false);
    }
  }, [isInstallable, isStandalone, showInstallPrompt]);

  // ── Actions ────────────────────────────────────────────────────────────

  const promptInstall = useCallback(async (): Promise<boolean> => {
    const prompt = deferredPromptRef.current;
    if (!prompt) return false;

    await prompt.prompt();
    const { outcome } = await prompt.userChoice;
    deferredPromptRef.current = null;
    setShowInstallPrompt(false);

    if (outcome === 'accepted') {
      setIsInstalled(true);
      setIsInstallable(false);
      return true;
    }
    return false;
  }, []);

  const requestPushPermission = useCallback(async (): Promise<NotificationPermission> => {
    const result = await requestNotificationPermission();
    setNotificationPermission(result);
    setShowNotificationPrompt(false);
    return result;
  }, []);

  const subscribePush = useCallback(async (): Promise<PushSubscription | null> => {
    const perm = await requestPushPermission();
    if (perm !== 'granted') return null;

    const sub = await subscribeToPush();
    setSubscription(sub);
    setIsSubscribed(!!sub);
    return sub;
  }, [requestPushPermission]);

  const unsubscribePush = useCallback(async (): Promise<boolean> => {
    const success = await unsubscribeFromPush();
    if (success) {
      setSubscription(null);
      setIsSubscribed(false);
    }
    return success;
  }, []);

  const dismissInstallPrompt = useCallback(() => {
    setShowInstallPrompt(false);
    setDismissed('installAt');
  }, []);

  const dismissNotificationPrompt = useCallback(() => {
    setShowNotificationPrompt(false);
    setDismissed('notificationAt');
  }, []);

  // ── Listen for SW messages (subscription change + update) ──────────
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return;
    const handler = (event: MessageEvent) => {
      if (event.data?.type === 'PUSH_SUBSCRIPTION_CHANGED') {
        getCurrentSubscription().then((sub) => {
          setSubscription(sub);
          setIsSubscribed(!!sub);
        });
      }
      if (event.data?.type === 'SW_UPDATED') {
        setUpdateAvailable(true);
      }
    };
    const controllerChangeHandler = () => {
      if (!refreshing) {
        refreshing = true;
        window.location.reload();
      }
    };
    let refreshing = false;
    navigator.serviceWorker.addEventListener('message', handler);
    navigator.serviceWorker.addEventListener('controllerchange', controllerChangeHandler);

    // Also detect waiting SW on initial load
    navigator.serviceWorker.ready.then((reg) => {
      if (reg.waiting) {
        waitingWorkerRef.current = reg.waiting;
        setUpdateAvailable(true);
      }
      reg.addEventListener('updatefound', () => {
        const newWorker = reg.installing;
        if (!newWorker) return;
        newWorker.addEventListener('statechange', () => {
          if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
            waitingWorkerRef.current = newWorker;
            setUpdateAvailable(true);
          }
        });
      });
    }).catch((error) => {
      console.warn('[PWA] Service worker readiness failed', error);
    });

    return () => {
      navigator.serviceWorker.removeEventListener('message', handler);
      navigator.serviceWorker.removeEventListener('controllerchange', controllerChangeHandler);
    };
  }, []);

  const applyUpdate = useCallback(() => {
    const waiting = waitingWorkerRef.current;
    if (waiting) {
      waiting.postMessage({ type: 'SKIP_WAITING' });
    } else {
      window.location.reload();
    }
  }, []);

  const value: PWAContextValue = {
    isInstalled,
    isInstallable,
    isStandalone,
    pushSupported,
    notificationPermission,
    isSubscribed,
    subscription,
    updateAvailable,
    showInstallPrompt,
    showNotificationPrompt,
    showFloatingIcon,
    promptInstall,
    requestPushPermission,
    subscribePush,
    unsubscribePush,
    dismissInstallPrompt,
    dismissNotificationPrompt,
    applyUpdate,
  };

  return <PWAContext.Provider value={value}>{children}</PWAContext.Provider>;
}

// ── Hooks ───────────────────────────────────────────────────────────────────

export function usePWA(): PWAContextValue {
  const ctx = useContext(PWAContext);
  if (!ctx) throw new Error('usePWA must be used within PWAProvider');
  return ctx;
}
