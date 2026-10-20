/* ══════════════════════════════════════════════════════════════════════
   Sentry — edge config

   Initialised in the Next.js edge runtime (middleware, edge route
   handlers). We don't currently run anything on the edge but Sentry
   needs this file to exist so its instrumentation hook can be wired.
   ══════════════════════════════════════════════════════════════════════ */

import * as Sentry from "@sentry/nextjs";

const DSN = process.env.SENTRY_DSN || process.env.NEXT_PUBLIC_SENTRY_DSN;
const ENV = process.env.SENTRY_ENV || process.env.NODE_ENV || "production";
const RELEASE = process.env.NEXT_PUBLIC_RELEASE;

if (DSN) {
  Sentry.init({
    dsn: DSN,
    environment: ENV,
    release: RELEASE,
    tracesSampleRate: 0,
    sendDefaultPii: false,
  });
}
