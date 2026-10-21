/* ══════════════════════════════════════════════════════════════════════
   Report feedback modal

   Brand-matching feedback dialog that lets users add context to a
   classified error before it ships to Sentry. Imperatively-driven so
   any callsite can call `openReportModal(err)` without wiring state.
   ══════════════════════════════════════════════════════════════════════ */

import * as React from "react";
import { Send, ShieldCheck } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/ui/atoms/dialog";
import { Button } from "@/ui/atoms/button";
import { Input } from "@/ui/atoms/input";
import { cn } from "@/ui/utils/cn";
import { reportError } from "@/core/errors/report";
import type { UserFacingError } from "@/core/errors/types";

interface ModalState {
  open: boolean;
  error: UserFacingError | null;
}

const listeners = new Set<(s: ModalState) => void>();
let state: ModalState = { open: false, error: null };

function setState(next: ModalState) {
  state = next;
  for (const cb of listeners) cb(state);
}

/** Imperative entrypoint — call from anywhere to open the report modal. */
export function openReportModal(error: UserFacingError): void {
  setState({ open: true, error });
}

export function closeReportModal(): void {
  setState({ open: false, error: null });
}

/**
 * Mounted once at the app shell so any `openReportModal()` call from
 * a toast or panel renders against the same singleton instance.
 */
export function ReportFeedbackModalHost() {
  const [s, setS] = React.useState<ModalState>(state);
  React.useEffect(() => {
    listeners.add(setS);
    return () => {
      listeners.delete(setS);
    };
  }, []);

  if (!s.open || !s.error) return null;
  return <ReportFeedbackModalView error={s.error} onClose={closeReportModal} />;
}

function ReportFeedbackModalView({
  error,
  onClose,
}: {
  error: UserFacingError;
  onClose: () => void;
}) {
  const [comments, setComments] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [submitted, setSubmitted] = React.useState(false);

  const handleSubmit = React.useCallback(async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await reportError(error, {
        feedback: {
          email: email.trim() || undefined,
          comments: comments.trim() || `(no comment) — ${error.title}`,
        },
        tags: { reported_by_user: "true" },
      });
      setSubmitted(true);
      // Close after a short success beat so the user sees the confirmation.
      window.setTimeout(() => {
        onClose();
        setSubmitted(false);
        setComments("");
        setEmail("");
      }, 1400);
    } finally {
      setSubmitting(false);
    }
  }, [comments, email, error, onClose, submitting]);

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-[color:var(--brand)]" />
            Report this issue
          </DialogTitle>
          <DialogDescription>
            Help us track this down. We&apos;ll receive your message along with technical
            details about what happened — no private data, no transactions.
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-[var(--radius-md)] border border-white/[0.06] bg-white/[0.02] p-3 text-xs">
          <div className="font-medium text-foreground">{error.title}</div>
          <div className="mt-0.5 text-muted-foreground">{error.message}</div>
          <div className="mt-2 font-mono text-[10px] uppercase tracking-wide text-muted-foreground/70">
            Code: {error.code}
            {error.context ? ` · ${error.context}` : ""}
          </div>
        </div>

        <div className="flex flex-col gap-3">
          <div>
            <label
              htmlFor="report-comments"
              className="text-xs font-medium text-foreground"
            >
              What were you doing when this happened?{" "}
              <span className="text-muted-foreground">(optional)</span>
            </label>
            <textarea
              id="report-comments"
              value={comments}
              onChange={(e) => setComments(e.target.value)}
              placeholder="e.g. I tried to open a long position on PAX/USD and the form just spun"
              rows={4}
              className={cn(
                "mt-1.5 w-full rounded-[var(--radius-md)] border border-white/[0.08]",
                "bg-white/[0.02] px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/60",
                "focus:border-[color:var(--brand)] focus:outline-none focus:ring-1 focus:ring-[color:var(--brand)]/40",
                "resize-none transition-colors",
              )}
              disabled={submitting || submitted}
            />
          </div>

          <div>
            <label htmlFor="report-email" className="text-xs font-medium text-foreground">
              Email <span className="text-muted-foreground">(optional, only if you want a reply)</span>
            </label>
            <Input
              id="report-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              className="mt-1.5 h-9 text-sm"
              disabled={submitting || submitted}
            />
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            disabled={submitting}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            onClick={handleSubmit}
            disabled={submitting || submitted}
            className="gap-1.5"
          >
            {submitted ? (
              <>
                <ShieldCheck className="h-3.5 w-3.5" />
                Sent — thanks
              </>
            ) : (
              <>
                <Send className="h-3.5 w-3.5" />
                {submitting ? "Sending…" : "Send report"}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
