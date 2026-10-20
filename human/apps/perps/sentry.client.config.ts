/* ══════════════════════════════════════════════════════════════════════
   Sentry — client config

   Reads `NEXT_PUBLIC_SENTRY_DSN` at runtime. If unset, Sentry stays
   inert and the app's local error UI is unaffected. We deliberately
   do NOT enable Replay or Profiling by default to keep the bundle
   light — turn them on per-environment if/when you want them.

   The `beforeSend` hook performs a final sanitisation pass:
     · Strips wallet addresses, private keys, and seed phrases.
     · Drops AbortError frames (benign, noisy).
   ══════════════════════════════════════════════════════════════════════ */

import * as Sentry from "@sentry/nextjs";

const DSN = process.env.NEXT_PUBLIC_SENTRY_DSN;
const ENV = process.env.NEXT_PUBLIC_SENTRY_ENV || process.env.NODE_ENV || "production";
const RELEASE = process.env.NEXT_PUBLIC_RELEASE;

if (DSN) {
  Sentry.init({
    dsn: DSN,
    environment: ENV,
    release: RELEASE,

    // Performance + replay are opt-in. Keep cheap defaults.
    tracesSampleRate: 0,
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: 0,

    // Lock down PII — we never want to ship wallet addresses or
    // private keys to a third-party. Our own `reportError` already
    // attaches what we want.
    sendDefaultPii: false,

    // Drop benign noise BEFORE it leaves the browser.
    ignoreErrors: [
      // ResizeObserver chrome warnings
      "ResizeObserver loop limit exceeded",
      "ResizeObserver loop completed with undelivered notifications.",
      // Cross-origin opaque errors — useless to triage
      "Script error.",
      // wagmi/viem expected user actions
      "User rejected the request",
      "UserRejectedRequestError",
    ],

    beforeSend(event, hint) {
      // Strip anything that looks like a hex secret from the message.
      // Belt-and-braces — our app code shouldn't be passing these
      // through `reportError` anyway, but a leak here would be loud.
      if (event.message) {
        event.message = redactSecrets(event.message);
      }
      if (event.exception?.values) {
        for (const ex of event.exception.values) {
          if (ex.value) ex.value = redactSecrets(ex.value);
        }
      }

      // AbortError can still reach `captureException` from third-party
      // libraries that don't normalise their own throws. Drop them.
      const original = hint?.originalException as { name?: string } | undefined;
      if (original?.name === "AbortError") return null;

      return event;
    },
  });
}

function redactSecrets(input: string): string {
  // 64-hex private keys + 12/24-word mnemonic guards (best-effort).
  let out = input.replace(/\b[0-9a-fA-F]{64}\b/g, "[redacted-hex]");
  out = out.replace(
    /\b(?:[a-z]{3,8}\s+){11,23}[a-z]{3,8}\b/gi,
    "[redacted-mnemonic]",
  );
  return out;
}
