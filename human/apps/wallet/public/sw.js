// ── Paxeer Wallet Service Worker (no caching — push notifications only) ─────

// ── Install: activate immediately ───────────────────────────────────────────
self.addEventListener('install', () => {
  self.skipWaiting();
});

// ── Activate: purge ALL existing caches from previous versions ──────────────
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.map((k) => caches.delete(k))))
  );
  self.clients.claim();
});

// ── Push Notifications ──────────────────────────────────────────────────────
self.addEventListener('push', (event) => {
  let data = { title: 'Paxeer Wallet', body: 'You have a new notification.' };

  if (event.data) {
    try {
      data = event.data.json();
    } catch {
      data.body = event.data.text();
    }
  }

  const options = {
    body: data.body || '',
    icon: data.icon || '/icons/android/launchericon-192x192.png',
    badge: data.badge || '/icons/android/launchericon-96x96.png',
    image: data.image || undefined,
    data: { url: data.url || '/' },
    vibrate: [100, 50, 100],
    actions: data.actions || [],
    tag: data.tag || 'paxeer-default',
    renotify: !!data.tag,
    requireInteraction: data.requireInteraction || false,
  };

  event.waitUntil(
    self.registration.showNotification(data.title || 'Paxeer Wallet', options)
  );
});

// ── Notification click: open or focus the app ───────────────────────────────
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const rawTarget = event.notification.data?.url;
  let targetUrl = '/';
  if (typeof rawTarget === 'string' && rawTarget.length <= 2048) {
    try {
      const candidate = new URL(rawTarget, self.location.origin);
      if (
        candidate.origin === self.location.origin &&
        candidate.pathname === '/' &&
        !candidate.username &&
        !candidate.password
      ) {
        targetUrl = `${candidate.pathname}${candidate.search}`;
      }
    } catch {
      targetUrl = '/';
    }
  }

  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if (new URL(client.url).origin === self.location.origin && 'focus' in client) {
          client.postMessage({ type: 'PAXPORT_NAVIGATE', route: targetUrl });
          return client.focus();
        }
      }
      return self.clients.openWindow(targetUrl);
    })
  );
});

// ── Skip waiting message from client ────────────────────────────────────────
self.addEventListener('message', (event) => {
  if (event.data?.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

// ── Push subscription change (auto-resubscribe) ────────────────────────────
self.addEventListener('pushsubscriptionchange', (event) => {
  event.waitUntil(
    self.registration.pushManager.subscribe(event.oldSubscription.options).then((sub) => {
      // Notify the app about the new subscription
      self.clients.matchAll().then((clients) => {
        clients.forEach((client) => {
          client.postMessage({ type: 'PUSH_SUBSCRIPTION_CHANGED', subscription: sub.toJSON() });
        });
      });
    })
  );
});
