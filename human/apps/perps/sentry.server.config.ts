/* ══════════════════════════════════════════════════════════════════════
   Sentry — server config

   Mirrors the client config but for the Next.js node runtime
   (API routes, getServerSideProps, etc.). Server traces are off by
   default; only exception capture is wired.
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
