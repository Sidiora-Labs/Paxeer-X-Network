// Sentry browser init. Next.js 15.3+ loads `instrumentation-client.ts`
// natively under both Turbopack (dev) and webpack (build), so client error
// tracking works without relying on the Sentry webpack plugin injecting
// `sentry.client.config.ts`. No-op unless NEXT_PUBLIC_SENTRY_DSN is set.
import * as Sentry from '@sentry/nextjs';

const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;

Sentry.init({
  dsn,
  enabled: Boolean(dsn),
  environment: process.env.NEXT_PUBLIC_SENTRY_ENV ?? process.env.NODE_ENV,
  release: process.env.NEXT_PUBLIC_SENTRY_RELEASE,
  tracesSampleRate: Number(process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE ?? 0.1),
  replaysSessionSampleRate: 0,
  replaysOnErrorSampleRate: Number(
    process.env.NEXT_PUBLIC_SENTRY_REPLAY_ERROR_SAMPLE_RATE ?? 0,
  ),
  debug: false,
});

// Capture App Router client navigations as Sentry transactions.
export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
