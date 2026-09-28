// ── Capacitor Native Bridge ─────────────────────────────────────────────────
// Conditionally activates native Capacitor plugins when running inside the
// Android (or iOS) shell. Falls back to existing web APIs in browser.

import { Capacitor } from '@capacitor/core';
import { App, type URLOpenListenerEvent } from '@capacitor/app';
import { StatusBar, Style } from '@capacitor/status-bar';
import { SplashScreen } from '@capacitor/splash-screen';
import { PushNotifications } from '@capacitor/push-notifications';
import { parseRouteUrl, serializeRoute } from '@/domains/shell';
import { reportBackgroundFailure } from '@/platform/status/background-failures';
import { currentThemeColor, currentThemeScheme } from '@/theme/dom';

// ── Platform Detection ──────────────────────────────────────────────────────

export function isNativeApp(): boolean {
  return Capacitor.isNativePlatform();
}

export function getNativePlatform(): 'android' | 'ios' | 'web' {
  return Capacitor.getPlatform() as 'android' | 'ios' | 'web';
}

// ── Initialization ──────────────────────────────────────────────────────────

let _initialized = false;

export async function initCapacitor(): Promise<void> {
  if (_initialized || !isNativeApp()) return;
  _initialized = true;

  try {
    await SplashScreen.hide({ fadeOutDuration: 300 });
  } catch {
    reportBackgroundFailure({
      domain: 'platform',
      kind: 'unavailable',
      code: 'NATIVE_SPLASH_FAILED',
      message: 'The native launch screen could not be dismissed normally.',
      retryable: false,
    });
  }

  try {
    await StatusBar.setStyle({ style: currentThemeScheme() === 'light' ? Style.Light : Style.Dark });
    await StatusBar.setBackgroundColor({ color: currentThemeColor() });
  } catch {
    reportBackgroundFailure({
      domain: 'platform',
      kind: 'unavailable',
      code: 'NATIVE_STATUS_BAR_FAILED',
      message: 'The native status bar could not be configured.',
      retryable: false,
    });
  }

  // Deep link handling
  App.addListener('appUrlOpen', (event: URLOpenListenerEvent) => {
    try {
      const incoming = new URL(event.url);
      const allowed =
        incoming.protocol === 'web+paxeer:' ||
        (incoming.protocol === 'https:' &&
          (incoming.hostname === 'paxportwallet.com' ||
            incoming.hostname === 'www.paxportwallet.com'));
      if (!allowed || incoming.username || incoming.password) return;
      const normalized = new URL('/', 'https://paxportwallet.com');
      incoming.searchParams.forEach((value, key) => {
        normalized.searchParams.append(key, value);
      });
      if (
        incoming.protocol === 'web+paxeer:' &&
        !normalized.searchParams.has('screen') &&
        incoming.hostname
      ) {
        normalized.searchParams.set('screen', incoming.hostname);
      }
      const route = parseRouteUrl(normalized);
      if (route.ok) {
        window.dispatchEvent(
          new CustomEvent('paxeer:deeplink', {
            detail: { route: serializeRoute(route.value) },
          }),
        );
      }
    } catch {
      // Invalid deep links are ignored at the native boundary.
    }
  });

  // Back button handling (Android)
  App.addListener('backButton', ({ canGoBack }) => {
    if (canGoBack) {
      window.history.back();
    } else {
      App.minimizeApp();
    }
  });
}

// ── Native Push Notifications (FCM) ────────────────────────────────────────

export interface PushTokenResult {
  token: string;
}

let _pushInitialized = false;

export async function initNativePush(
  onToken?: (token: string) => void,
  onNotification?: (data: Record<string, unknown>) => void,
): Promise<void> {
  if (!isNativeApp() || _pushInitialized) return;
  _pushInitialized = true;

  const permResult = await PushNotifications.requestPermissions();
  if (permResult.receive !== 'granted') {
    console.warn('[Capacitor] Push permission denied');
    return;
  }

  await PushNotifications.register();

  PushNotifications.addListener('registration', (token) => {
    onToken?.(token.value);
  });

  PushNotifications.addListener('registrationError', () => {
    reportBackgroundFailure({
      domain: 'platform',
      kind: 'unavailable',
      code: 'NATIVE_PUSH_REGISTRATION_FAILED',
      message: 'Notifications could not be registered on this device.',
      retryable: true,
    });
  });

  PushNotifications.addListener(
    'pushNotificationReceived',
    (notification) => {
      onNotification?.(notification.data ?? {});
    },
  );

  PushNotifications.addListener(
    'pushNotificationActionPerformed',
    (action) => {
      const data = action.notification?.data;
      const route =
        typeof data?.route === 'string' ? parseRouteUrl(data.route) : null;
      if (route?.ok) {
        window.dispatchEvent(
          new CustomEvent('paxeer:push-navigate', {
            detail: { route: serializeRoute(route.value) },
          }),
        );
      }
    },
  );
}

// ── App State (Resume / Pause) ──────────────────────────────────────────────

type AppStateCallback = (isActive: boolean) => void;

const _appStateCallbacks = new Set<AppStateCallback>();

export function onAppStateChange(callback: AppStateCallback): () => void {
  if (!isNativeApp()) return () => {};

  _appStateCallbacks.add(callback);

  return () => {
    _appStateCallbacks.delete(callback);
  };
}

let _appStateListenerAdded = false;

export function initAppStateListener(): void {
  if (!isNativeApp() || _appStateListenerAdded) return;
  _appStateListenerAdded = true;

  App.addListener('appStateChange', ({ isActive }) => {
    _appStateCallbacks.forEach((cb) => cb(isActive));
  });
}
