// Retire the legacy worker without deleting caches owned by the current worker.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => {
  event.waitUntil(Promise.all([
    caches.delete('unidex-v1'),
    self.registration.unregister(),
  ]));
});
