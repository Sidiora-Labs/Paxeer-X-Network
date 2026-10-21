"use client";

import Link from "next/link";
import { X, Cookie } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { useCookieConsent } from "@/hooks/ui/useCookieConsent";

/**
 * Cookie consent banner — appears on first visit, bottom-centre of the viewport.
 * Disappears once the user accepts or declines, and never reappears.
 * Choice is persisted to localStorage via `useCookieConsent`.
 */
export function CookieBanner() {
  const { value, loading, accept, decline } = useCookieConsent();

  // Don't render during SSR hydration or once a decision has been made.
  if (loading || value !== null) return null;

  return (
    <div
      role="dialog"
      aria-label="Cookie consent"
      aria-modal="false"
      className={cn(
        "fixed bottom-6 left-1/2 -translate-x-1/2 z-[200]",
        "w-full max-w-[560px] mx-auto px-4",
      )}
    >
      <div className="rounded-xl border border-border bg-card/95 backdrop-blur-xl shadow-2xl shadow-black/40 p-5">
        {/* Header row */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-2">
            <Cookie className="h-4 w-4 text-muted-foreground shrink-0 mt-0.5" />
            <span className="text-sm font-semibold text-foreground">We use cookies</span>
          </div>
          {/* Dismiss = same as declining */}
          <button
            onClick={decline}
            aria-label="Dismiss cookie banner"
            className="text-muted-foreground hover:text-foreground transition-colors shrink-0 -mt-0.5"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <p className="text-xs text-muted-foreground leading-relaxed mb-4">
          HyperPax uses essential cookies required for the platform to function, and optional
          analytics cookies to help us understand how you use the app. We never sell your data
          or share it with advertisers. Read our{" "}
          <Link href="/cookies" className="text-foreground underline underline-offset-2 hover:text-foreground/80 transition-colors">
            Cookie Policy
          </Link>{" "}
          and{" "}
          <Link href="/privacy" className="text-foreground underline underline-offset-2 hover:text-foreground/80 transition-colors">
            Privacy Policy
          </Link>{" "}
          for details.
        </p>

        {/* Actions */}
        <div className="flex items-center gap-2 justify-end">
          <button
            onClick={decline}
            className="px-4 py-1.5 rounded-lg text-xs font-medium text-muted-foreground border border-border hover:border-foreground/30 hover:text-foreground transition-colors"
          >
            Essential only
          </button>
          <button
            onClick={accept}
            className="px-4 py-1.5 rounded-lg text-xs font-semibold text-white bg-[var(--brand-blue)] hover:bg-[#0046DC] transition-colors"
          >
            Accept all cookies
          </button>
        </div>
      </div>
    </div>
  );
}
