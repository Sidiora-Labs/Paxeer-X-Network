/* ══════════════════════════════════════════════════════════════════════
   Error toast

   Single function `showErrorToast(ufe, opts)` that renders a
   classified `UserFacingError` through the existing toast queue. It
   ALWAYS includes:

     · A short title and a one-line plain-English description.
     · A suggestion for a possible fix when one exists.
     · A "Retry" button when the error is retryable AND a callback
       is supplied by the caller.
     · A "Report" button for any error flagged `isReportable`,
       opening the brand-matching feedback modal.

   This is the user-facing replacement for `toast({ variant: 'destructive' })`
   anywhere in the app — never call the raw `toast()` for errors again.
   ══════════════════════════════════════════════════════════════════════ */

import * as React from "react";
import { AlertCircle, AlertTriangle, Info, RefreshCw, Send, XOctagon } from "lucide-react";
import { toast } from "@/ui/atoms/use-toast";
import { ToastAction, type ToastActionElement } from "./ToastActionBridge";
import { openReportModal } from "./ReportFeedbackModal";
import { classifyError, isUserFacingError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";
import type { ErrorContext, UserFacingError } from "@/core/errors/types";

export interface ShowErrorToastOptions {
  /** Wired to the toast's "Retry" action when the error is retryable. */
  onRetry?: () => void | Promise<void>;
  /** Override the default classification → variant mapping (rare). */
  variant?: "default" | "destructive";
  /**
   * Optional caller-supplied title override (e.g. "Failed to add size").
   * The catalog's user-friendly `message` and `suggestedFix` are still
   * shown — this only replaces the headline so callers can preserve
   * their domain copy. Defaults to the catalog's `title`.
   */
  titleOverride?: React.ReactNode;
}

/**
 * Map severity to icon + visual treatment. Severity drives the variant:
 * - fatal/error → destructive (red)
 * - warning/info → default (subtle, brand-tinted)
 */
function severityIcon(severity: UserFacingError["severity"]): React.ReactElement {
  switch (severity) {
    case "fatal":
    case "error":
      return <XOctagon className="h-4 w-4 shrink-0 text-[color:var(--destructive-foreground)]" />;
    case "warning":
      return <AlertTriangle className="h-4 w-4 shrink-0 text-amber-400" />;
    case "info":
    default:
      return <Info className="h-4 w-4 shrink-0 text-[color:var(--brand)]" />;
  }
}

function severityVariant(severity: UserFacingError["severity"]): "default" | "destructive" {
  return severity === "fatal" || severity === "error" ? "destructive" : "default";
}

/**
 * Render a `UserFacingError` as a toast. Returns a handle the caller
 * can use to dismiss it manually if needed.
 */
export function showErrorToast(
  ufe: UserFacingError,
  opts: ShowErrorToastOptions = {},
): { id: string; dismiss: () => void } {
  const variant = opts.variant ?? severityVariant(ufe.severity);

  const titleNode: React.ReactElement = (
    <div className="flex items-center gap-2">
      {severityIcon(ufe.severity)}
      <span>{opts.titleOverride ?? ufe.title}</span>
    </div>
  );

  const descriptionNode: React.ReactElement = (
    <div className="flex flex-col gap-1">
      <span>{ufe.message}</span>
      {ufe.suggestedFix ? (
        <span className="text-[11px] text-muted-foreground/90">
          <span className="font-medium text-foreground/80">Try this: </span>
          {ufe.suggestedFix}
        </span>
      ) : null}
    </div>
  );

  // Build the action slot: optional Retry + optional Report. We collapse
  // them into a single horizontal row that fits inside the toast's right
  // gutter without overflowing. The toast's `action` slot accepts a single
  // ReactElement, so we wrap multiples in a div.
  const actions: React.ReactElement[] = [];

  if (ufe.isRetryable && opts.onRetry) {
    actions.push(
      <ToastAction
        key="retry"
        altText="Retry the failed action"
        onClick={() => {
          // Fire-and-forget — the caller's onRetry is responsible for
          // surfacing any subsequent errors through this same pipeline.
          try {
            void opts.onRetry?.();
          } catch {
            /* swallowed — global trap will catch it */
          }
        }}
        className="gap-1"
      >
        <RefreshCw className="h-3 w-3" />
        Retry
      </ToastAction>,
    );
  }

  if (ufe.isReportable) {
    actions.push(
      <ToastAction
        key="report"
        altText="Report this issue"
        onClick={() => openReportModal(ufe)}
        className="gap-1"
      >
        <Send className="h-3 w-3" />
        Report
      </ToastAction>,
    );
  }

  let actionElement: ToastActionElement | undefined;
  if (actions.length === 1) {
    actionElement = actions[0];
  } else if (actions.length > 1) {
    actionElement = <div className="flex items-center gap-1.5">{actions}</div>;
  }

  return toast({
    title: titleNode,
    description: descriptionNode,
    variant,
    action: actionElement,
  });
}

/**
 * Re-export `AlertCircle` for any caller that wants to render a
 * matching glyph next to inline error states (e.g. form fields).
 */
export { AlertCircle as ErrorGlyph };

/* ── Standalone helper for non-hook callsites ─────────────────────── */

export interface ToastErrorOptions extends ShowErrorToastOptions {
  /** Free-form context tag (e.g. `"deposit/approve"`). */
  context?: ErrorContext;
  /** Suppress the toast — still classifies + reports. */
  silent?: boolean;
}

/**
 * Drop-in replacement for the `toast({ variant: "destructive", ... })`
 * pattern used across the codebase. Classifies the error, renders a
 * brand `ErrorToast`, and reports to Sentry. Use this from non-React
 * code (hooks, services, plain functions) where `useErrorHandler`
 * isn't available.
 */
export function toastError(err: unknown, opts: ToastErrorOptions = {}): void {
  const ufe = isUserFacingError(err) ? err : classifyError(err, opts.context);
  if (!opts.silent) {
    showErrorToast(ufe, { onRetry: opts.onRetry, variant: opts.variant });
  }
  void reportError(ufe, { context: opts.context });
}
