// Legacy service-worker retirement shim. next-pwa owns /sw.js registration.
//
// Loaded by <Script src="/scripts/register-sw.js"> in `_app.tsx` instead
// of `dangerouslySetInnerHTML` so we can keep a strict CSP without a
// blanket `'unsafe-inline'` on script-src. The file is short by design —
// this only removes the obsolete /service-worker.js registration and cache.
(function () {
  if (typeof window === "undefined") return;
  if (!("serviceWorker" in navigator)) return;
  window.addEventListener("load", function () {
    navigator.serviceWorker.getRegistrations()
      .then(function (registrations) {
        return Promise.all(registrations.filter(function (registration) {
          var worker = registration.active || registration.waiting || registration.installing;
          return worker && new URL(worker.scriptURL).pathname === "/service-worker.js";
        }).map(function (registration) { return registration.unregister(); }));
      })
      .then(function () { return caches.delete("unidex-v1"); })
      .catch(function () {
        /* swallowed; SW failure must not break the trading UI */
      });
  });
})();
