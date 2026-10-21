type ErrorContext = Record<string, unknown> | string;

function toContext(ctx?: ErrorContext): Record<string, unknown> | undefined {
  if (!ctx) return undefined;
  if (typeof ctx === 'string') return { area: ctx };
  return ctx;
}

/**
 * 4.5: Error reporting with pluggable provider.
 *
 * Supports three modes:
 * 1. Sentry — set NEXT_PUBLIC_SENTRY_DSN (requires @sentry/nextjs to be installed)
 * 2. Beacon — POST errors to /api/errors in production (zero-dependency fallback)
 * 3. Console — dev-mode only, logs to console.warn
 *
 * To enable Sentry: `pnpm add @sentry/nextjs` and set the DSN env var.
 */

const SENTRY_DSN = process.env.NEXT_PUBLIC_SENTRY_DSN;

export function reportError(error: unknown, context?: ErrorContext): void {
  const extra = toContext(context);

  if (process.env.NODE_ENV !== 'production') {
    console.warn('[Sidiora]', extra, error);
  }

  // Sentry (lazy dynamic import — only resolves if @sentry/nextjs is installed)
  if (SENTRY_DSN) {
    import('@sentry/nextjs')
      .then((Sentry) => {
        Sentry.captureException(error, { extra });
      })
      .catch(() => {
        sendBeacon(error, extra);
      });
    return;
  }

  sendBeacon(error, extra);
}

function sendBeacon(error: unknown, extra?: Record<string, unknown>): void {
  if (process.env.NODE_ENV !== 'production') return;
  if (typeof window === 'undefined') return;
  try {
    const body = {
      message: error instanceof Error ? error.message : String(error),
      stack: error instanceof Error ? error.stack : undefined,
      context: extra,
      url: window.location.href,
      timestamp: Date.now(),
    };
    navigator.sendBeacon('/api/errors', JSON.stringify(body));
  } catch { /* best-effort */ }
}
