import React from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";

interface LegalLayoutProps {
  title: string;
  lastUpdated: string;
  children: React.ReactNode;
}

/**
 * Shared layout for Privacy Policy, Terms of Service, and Cookie Policy pages.
 * Renders a centred prose column with a back-to-home link.
 */
export function LegalLayout({ title, lastUpdated, children }: LegalLayoutProps) {
  return (
    <div className="min-h-full bg-background px-4 py-10">
      <div className="mx-auto max-w-2xl">
        {/* Back link */}
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors mb-8"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          Back to HyperPax
        </Link>

        {/* Masthead */}
        <header className="mb-10 pb-6 border-b border-border">
          <p className="text-[10px] uppercase tracking-widest font-mono text-muted-foreground mb-2">
            Legal
          </p>
          <h1 className="text-2xl font-bold text-foreground">{title}</h1>
          <p className="text-xs text-muted-foreground mt-1">Last updated: {lastUpdated}</p>
        </header>

        {/* Prose */}
        <div className="prose prose-sm prose-invert max-w-none space-y-6 text-[13px] leading-relaxed text-muted-foreground [&_h2]:text-base [&_h2]:font-semibold [&_h2]:text-foreground [&_h2]:mt-8 [&_h2]:mb-3 [&_p]:mb-4 [&_ul]:list-disc [&_ul]:pl-5 [&_ul]:space-y-1 [&_a]:text-foreground [&_a]:underline [&_a]:underline-offset-2 [&_strong]:text-foreground [&_strong]:font-medium">
          {children}
        </div>

        {/* Footer nav */}
        <div className="mt-12 pt-6 border-t border-border flex flex-wrap gap-4 text-[11px] text-muted-foreground">
          <Link href="/privacy" className="hover:text-foreground transition-colors">Privacy Policy</Link>
          <Link href="/terms"   className="hover:text-foreground transition-colors">Terms of Service</Link>
          <Link href="/cookies" className="hover:text-foreground transition-colors">Cookie Policy</Link>
        </div>
      </div>
    </div>
  );
}
