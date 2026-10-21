import * as Sentry from '@sentry/nextjs';

Sentry.init({
  dsn: process.env.NEXT_PUBLIC_SENTRY_DSN,
  environment: process.env.NODE_ENV,

  // Capture 10 % of sessions in prod; 100 % in dev so you see everything locally.
  tracesSampleRate: process.env.NODE_ENV === 'production' ? 0.1 : 1.0,

  // Session replays — 5 % of all sessions, 100 % of sessions with errors.
  replaysSessionSampleRate: 0.05,
  replaysOnErrorSampleRate: 1.0,

  integrations: [
    Sentry.replayIntegration({
      maskAllText: true,
      maskAllInputs: true,
      blockAllMedia: true,
    }),
  ],

  // Only enable when a DSN is configured — avoids noise in local dev without one.
  enabled: !!process.env.NEXT_PUBLIC_SENTRY_DSN,
});
