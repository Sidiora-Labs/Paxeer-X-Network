// ── Native Push Notification Helpers ─────────────────────────────────────────
// Manages Web Push subscriptions without any third-party service.
// To send push from your backend, use the subscription object with web-push lib.

const VAPID_PUBLIC_KEY = process.env.NEXT_PUBLIC_VAPID_PUBLIC_KEY || '';
const PUSH_API_BASE = '/api/push';

// ── Helpers ─────────────────────────────────────────────────────────────────

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const rawData = window.atob(base64);
  const outputArray = new Uint8Array(rawData.length);
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i);
  }
  return outputArray;
}

// ── Feature Detection ───────────────────────────────────────────────────────

export function isPushSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  );
}

export function isNotificationSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window;
}

export function getNotificationPermission(): NotificationPermission | 'unsupported' {
  if (!isNotificationSupported()) return 'unsupported';
  return Notification.permission;
}

// ── Service Worker Registration ─────────────────────────────────────────────

export async function getServiceWorkerRegistration(): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null;
  try {
    const existing = await navigator.serviceWorker.getRegistration('/');
    const registration = existing || await navigator.serviceWorker.register('/sw.js');
    if (registration.active) return registration;
    return await navigator.serviceWorker.ready;
  } catch {
    return null;
  }
}

// ── Push Subscription ───────────────────────────────────────────────────────

export async function subscribeToPush(): Promise<PushSubscription | null> {
  if (!isPushSupported()) return null;

  const registration = await getServiceWorkerRegistration();
  if (!registration) return null;

  // Check existing subscription
  const existing = await registration.pushManager.getSubscription();
  if (existing) return existing;

  if (!VAPID_PUBLIC_KEY) {
    console.warn('[Push] No VAPID public key configured');
    return null;
  }

  try {
    const subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY) as BufferSource,
    });

    // Send subscription to backend
    await sendSubscriptionToServer(subscription);
    return subscription;
  } catch (err) {
    console.error('[Push] Subscribe failed:', err);
    return null;
  }
}

export async function unsubscribeFromPush(): Promise<boolean> {
  const registration = await getServiceWorkerRegistration();
  if (!registration) return false;

  const subscription = await registration.pushManager.getSubscription();
  if (!subscription) return true;

  try {
    const success = await subscription.unsubscribe();
    if (success) {
      await removeSubscriptionFromServer(subscription);
    }
    return success;
  } catch (err) {
    console.error('[Push] Unsubscribe failed:', err);
    return false;
  }
}

export async function getCurrentSubscription(): Promise<PushSubscription | null> {
  const registration = await getServiceWorkerRegistration();
  if (!registration) return null;
  return registration.pushManager.getSubscription();
}

// ── Notification Permission ─────────────────────────────────────────────────

export async function requestNotificationPermission(): Promise<NotificationPermission> {
  if (!isNotificationSupported()) return 'denied';
  const result = await Notification.requestPermission();
  return result;
}

// ── Local Notifications (no push server needed) ─────────────────────────────

export async function showLocalNotification(
  title: string,
  options?: NotificationOptions
): Promise<void> {
  const registration = await getServiceWorkerRegistration();
  if (!registration) return;

  await registration.showNotification(title, {
    icon: '/icons/android/launchericon-192x192.png',
    badge: '/icons/android/launchericon-96x96.png',
    ...options,
  } as NotificationOptions & Record<string, unknown>);
}

// ── Backend Communication ───────────────────────────────────────────────────

let _walletAddress: string | null = null;

/** Set the active wallet address for push subscription association. */
export function setWalletAddressForPush(address: string): void {
  _walletAddress = address;
}

async function sendSubscriptionToServer(subscription: PushSubscription): Promise<void> {
  try {
    const subJson = subscription.toJSON();
    await fetch(`${PUSH_API_BASE}/subscribe`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        endpoint: subJson.endpoint,
        keys: subJson.keys,
        walletAddress: _walletAddress || 'unknown',
      }),
    });
  } catch (err) {
    console.warn('[Push] Failed to send subscription to server:', err);
  }
}

async function removeSubscriptionFromServer(subscription: PushSubscription): Promise<void> {
  try {
    await fetch(`${PUSH_API_BASE}/subscribe`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint: subscription.endpoint }),
    });
  } catch (err) {
    console.warn('[Push] Failed to remove subscription from server:', err);
  }
}

/** Update the server that this subscription is still active (heartbeat). */
export async function heartbeat(): Promise<void> {
  const sub = await getCurrentSubscription();
  if (!sub || !_walletAddress) return;
  // Re-upsert refreshes lastActiveAt on the server
  await sendSubscriptionToServer(sub);
}
