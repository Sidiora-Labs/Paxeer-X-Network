/* ══════════════════════════════════════════════════════════════════════
   Error panel

   Inline error UI for widgets that fail in-place (not as a toast).
   Used by:
     · `ErrorBoundary` fallback
     · Empty-state replacements in panels (orderbook, positions, chart)
     · Any data-fetching widget that wants to render an error block
       instead of a stale view.

   Anatomy: glyph · title · message · fix hint · retry/report buttons.
   ══════════════════════════════════════════════════════════════════════ */

import * as React from "react";
import { AlertTriangle, RefreshCw, Send, WifiOff, XOctagon } from "lucide-react";
import { Button } from "@/ui/atoms/button";
import { cn } from "@/ui/utils/cn";
import { openReportModal } from "./ReportFeedbackModal";
import type { UserFacingError } from "@/core/errors/types";

interface ErrorPanelProps {
  error: UserFacingError;
  /** Optional retry callback. Hidden if the error isn't retryable. */
  onRetry?: () => void | Promise<void>;
  /** Compact mode trims padding for narrow panels (orderbook, watchlist). */
  size?: "sm" | "md";
  /** Render only the inner block (no card chrome) — useful inside cards. */
  bare?: boolean;
  className?: string;
}

function panelGlyph(error: UserFacingError): React.ReactElement {
  if (error.code.startsWith("API_") || error.code.startsWith("WS_") || error.code.startsWith("RPC_")) {
    return <WifiOff className="h-5 w-5 text-muted-foreground" aria-hidden />;
  }
  if (error.severity === "fatal" || error.severity === "error") {
    return <XOctagon className="h-5 w-5 text-[color:var(--destructive-foreground)]" aria-hidden />;
  }
  return <AlertTriangle className="h-5 w-5 text-amber-400" aria-hidden />;
}

export function ErrorPanel({
  error,
  onRetry,
  size = "md",
  bare = false,
  className,
}: ErrorPanelProps) {
  const [retrying, setRetrying] = React.useState(false);

  const handleRetry = React.useCallback(async () => {
    if (!onRetry || retrying) return;
    setRetrying(true);
    try {
      await onRetry();
    } finally {
      setRetrying(false);
    }
  }, [onRetry, retrying]);

  const padding = size === "sm" ? "p-4" : "p-6";

  const inner = (
    <div className={cn("flex flex-col items-center justify-center gap-3 text-center", padding)}>
      <div className="rounded-full bg-white/[0.04] p-2.5">{panelGlyph(error)}</div>

      <div className="flex max-w-xs flex-col gap-1">
        <p className="text-sm font-medium text-foreground">{error.title}</p>
        <p className="text-xs text-muted-foreground">{error.message}</p>
        {error.suggestedFix ? (
          <p className="mt-1 text-[11px] text-muted-foreground/90">
            <span className="font-medium text-foreground/80">Try this: </span>
            {error.suggestedFix}
          </p>
        ) : null}
      </div>

      {(error.isRetryable && onRetry) || error.isReportable ? (
        <div className="mt-1 flex flex-wrap items-center justify-center gap-2">
          {error.isRetryable && onRetry ? (
            <Button
              variant="outline"
              size="sm"
              className="h-7 gap-1.5 text-xs"
              onClick={handleRetry}
              disabled={retrying}
            >
              <RefreshCw className={cn("h-3 w-3", retrying && "animate-spin")} />
              {retrying ? "Retrying…" : "Try again"}
            </Button>
          ) : null}

          {error.isReportable ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1.5 text-xs"
              onClick={() => openReportModal(error)}
            >
              <Send className="h-3 w-3" />
              Report
            </Button>
          ) : null}
        </div>
      ) : null}

      <p className="mt-1 font-mono text-[9px] uppercase tracking-wide text-muted-foreground/50">
        {error.code}
        {error.context ? ` · ${error.context}` : ""}
      </p>
    </div>
  );

  if (bare) return <div className={cn("h-full w-full", className)}>{inner}</div>;

  return (
    <div
      className={cn(
        "flex h-full min-h-[140px] w-full flex-col items-center justify-center",
        "rounded-[var(--radius-lg)] border border-white/[0.04] bg-card/50",
        className,
      )}
    >
      {inner}
    </div>
  );
}
