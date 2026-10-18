/* ══════════════════════════════════════════════════════════════════════
   Error reporting

   Single entry point for sending errors to Sentry. Lazily imports
   `@sentry/nextjs` so that builds where Sentry is disabled (no DSN
   set) ship zero of its runtime cost. Falls back to `console.error`
   if Sentry isn't initialised — the user-facing UI is unaffected.

   Set `NEXT_PUBLIC_SENTRY_DSN` to enable in production. Without it,
   classification + UI keep working; reports are just no-ops.
   ══════════════════════════════════════════════════════════════════════ */

import { classifyError, isUserFacingError } from "./classify";
import type { UserFacingError, ReportErrorOptions } from "./types";

let sentryReady = false;
let sentryModule: typeof import("@sentry/nextjs") | null = null;

async function getSentry(): Promise<typeof import("@sentry/nextjs") | null> {
  if (sentryReady) return sentryModule;
  // We mark this ready synchronously so racing callers don't double-init.
  sentryReady = true;
  if (typeof window === "undefined") {
    // Server-side: Sentry is initialised by the dedicated `sentry.server.config.ts`.
  }
  try {
    sentryModule = await import("@sentry/nextjs");
  } catch {
    sentryModule = null;
  }
  return sentryModule;
}

function severityToSentryLevel(
  severity: UserFacingError["severity"],
): "info" | "warning" | "error" | "fatal" {
  return severity;
}

/**
 * Report a normalised error to Sentry. Safe to call on every error
 * — it self-rate-limits via the `isReportable` flag in the catalog
 * (e.g. wallet rejections never hit Sentry).
 *
 * If `err` is a raw thrown value (not a `UserFacingError`), it will
 * be classified first.
 */
export async function reportError(
  err: unknown,
  opts: ReportErrorOptions = {},
): Promise<void> {
  const ufe: UserFacingError = isUserFacingError(err) ? err : classifyError(err, opts.context);
  if (!ufe.isReportable) return;

  // Always log locally — useful in dev and during incident triage.
  // eslint-disable-next-line no-console
  console.error(`[error:${ufe.code}]`, ufe.title, ufe.cause ?? ufe.message, opts.extra ?? "");

  const Sentry = await getSentry();
  if (!Sentry) return;

  Sentry.withScope((scope) => {
    scope.setLevel(severityToSentryLevel(ufe.severity));
    scope.setTag("error.code", ufe.code);
    if (ufe.context) scope.setTag("error.context", ufe.context);
    if (opts.tags) {
      for (const [k, v] of Object.entries(opts.tags)) scope.setTag(k, v);
    }
    if (ufe.fingerprint && ufe.fingerprint.length) scope.setFingerprint(ufe.fingerprint);

    scope.setContext("user_facing_error", {
      code: ufe.code,
      title: ufe.title,
      message: ufe.message,
      suggestedFix: ufe.suggestedFix ?? null,
      severity: ufe.severity,
    });

    if (opts.extra) {
      for (const [k, v] of Object.entries(opts.extra)) scope.setExtra(k, v);
    }

    const captured = ufe.cause instanceof Error
      ? Sentry.captureException(ufe.cause)
      : Sentry.captureMessage(`${ufe.code}: ${ufe.title}`);

    if (opts.feedback?.comments && captured) {
      try {
        // Sentry v8: `captureFeedback` is the modern API.
        // It accepts the eventId returned by the capture call.
        const fn = (Sentry as unknown as {
          captureFeedback?: (f: {
            name?: string;
            email?: string;
            message: string;
            associatedEventId?: string;
          }) => void;
        }).captureFeedback;
        if (typeof fn === "function") {
          fn({
            name: opts.feedback.name,
            email: opts.feedback.email,
            message: opts.feedback.comments,
            associatedEventId: typeof captured === "string" ? captured : undefined,
          });
        }
      } catch {
        // Feedback is best-effort — never let it break the host action.
      }
    }
  });
}

/**
 * Lightweight breadcrumb helper — useful from hooks/widgets that
 * want to leave a trail before doing risky work.
 */
export async function leaveBreadcrumb(
  category: string,
  message: string,
  data?: Record<string, unknown>,
): Promise<void> {
  const Sentry = await getSentry();
  if (!Sentry) return;
  try {
    Sentry.addBreadcrumb({ category, message, data, level: "info" });
  } catch {
    // ignore
  }
}
