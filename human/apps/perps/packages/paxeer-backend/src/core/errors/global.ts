/* ══════════════════════════════════════════════════════════════════════
   Global error trap

   Installed once on the client at app boot via `installGlobalErrorHandlers()`.
   Catches anything that escapes a React boundary — uncaught synchronous
   throws and unhandled promise rejections — classifies them, and
   shows a user-safe toast through the same `ErrorToast` UI used
   everywhere else. This is the safety net that guarantees no raw
   error string ever reaches a user, even from un-migrated catch sites.
   ══════════════════════════════════════════════════════════════════════ */

import { classifyError } from "./classify";
import { reportError } from "./report";
import { showErrorToast } from "@paxeer-network/paxeer-backend/errors/ErrorToast";

let installed = false;

/**
 * Heuristic: filter noise that would otherwise spam the user.
 * - `RUNTIME_ABORTED` is benign (effects unmounting, AbortController).
 * - Errors originating outside our origin (cross-origin scripts) are
 *   useless to surface and unactionable.
 */
function shouldSuppress(reason: unknown, message: string): boolean {
  const lower = message.toLowerCase();
  if (lower === "script error.") return true; // cross-origin opaque error
  if (lower.includes("resizeobserver loop limit")) return true; // benign chrome warning
  if (lower.includes("resizeobserver loop completed")) return true;
  if ((reason as { name?: string } | null)?.name === "AbortError") return true;
  return false;
}

export function installGlobalErrorHandlers(): () => void {
  if (typeof window === "undefined" || installed) return () => {};
  installed = true;

  const onError = (event: ErrorEvent) => {
    const raw = event.error ?? event.message;
    const message = typeof event.message === "string" ? event.message : String(raw ?? "");
    if (shouldSuppress(raw, message)) return;
    const ufe = classifyError(raw, "window.onerror");
    showErrorToast(ufe);
    void reportError(ufe);
  };

  const onUnhandledRejection = (event: PromiseRejectionEvent) => {
    const raw = event.reason;
    const message = (raw && typeof raw === "object" && "message" in raw)
      ? String((raw as { message: unknown }).message)
      : String(raw ?? "");
    if (shouldSuppress(raw, message)) return;
    const ufe = classifyError(raw, "unhandledrejection");
    showErrorToast(ufe);
    void reportError(ufe);
  };

  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onUnhandledRejection);

  // Also bridge the `__reportErrorBoundary` hook the legacy
  // `ErrorBoundary` already calls into, so any boundary-caught error
  // routes through the same pipeline.
  (window as unknown as {
    __reportErrorBoundary?: (info: {
      label: string;
      error: Error;
      componentStack?: string | null;
    }) => void;
  }).__reportErrorBoundary = (info) => {
    const ufe = classifyError(info.error, `boundary:${info.label}`);
    void reportError(ufe, { extra: { componentStack: info.componentStack } });
  };

  return () => {
    window.removeEventListener("error", onError);
    window.removeEventListener("unhandledrejection", onUnhandledRejection);
    delete (window as unknown as { __reportErrorBoundary?: unknown }).__reportErrorBoundary;
    installed = false;
  };
}
