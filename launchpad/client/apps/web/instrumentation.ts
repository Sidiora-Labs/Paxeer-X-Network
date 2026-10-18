// Next.js instrumentation hook. Wires Sentry on the server + edge runtimes and
// forwards nested React Server Component request errors to Sentry.
import * as Sentry from '@sentry/nextjs';

export async function register() {
  if (process.env.NEXT_RUNTIME === 'nodejs') {
    await import('./sentry.server.config');
    // Fail loudly at startup: a production build without its backend origins
    // serves only the configuration error page (src/app/layout.tsx).
    const { backendConfigurationProblems } = await import('./src/core/backend-origins');
    const problems = backendConfigurationProblems();
    if (problems.length > 0) console.error(`[sidiora] backend origins are not configured; every page renders the configuration error: ${problems.map(problem => problem.message).join('; ')}`);
  }
  if (process.env.NEXT_RUNTIME === 'edge') {
    await import('./sentry.edge.config');
  }
}

export const onRequestError = Sentry.captureRequestError;
