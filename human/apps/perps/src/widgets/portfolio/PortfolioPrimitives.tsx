"use client";

import * as React from "react";
import { Button } from "@/ui/atoms/button";

/**
 * Shared card / empty-state primitives used by every Portfolio tab panel.
 *
 * Kept in one file because they're tiny, semantically related, and always
 * imported together.
 */

// ── SectionCard ──────────────────────────────────────────────────────

export interface SectionCardProps {
  title: string;
  /** Optional badge count rendered next to the title (hidden when 0). */
  count?: number;
  children: React.ReactNode;
}

export function SectionCard({ title, count, children }: SectionCardProps) {
  return (
    <section className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] p-4 md:p-5">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-foreground">
          {title}
          {typeof count === "number" && count > 0 ? (
            <span className="ml-2 inline-flex items-center justify-center h-5 min-w-[20px] px-1.5 rounded-[var(--radius-pill)] text-[10px] font-mono bg-white/[0.06] text-muted-foreground">
              {count}
            </span>
          ) : null}
        </h3>
      </div>
      {children}
    </section>
  );
}

// ── EmptyState ───────────────────────────────────────────────────────

export interface EmptyStateProps {
  title: string;
  description: string;
}

export function EmptyState({ title, description }: EmptyStateProps) {
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-dashed border-white/[0.08] p-8 text-center">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <p className="mt-1 text-xs text-muted-foreground max-w-md mx-auto">
        {description}
      </p>
    </div>
  );
}

// ── PlaceholderPanel ─────────────────────────────────────────────────

export interface PlaceholderPanelProps {
  title: string;
  /** Bold label inside the dashed box (e.g. "Open orders"). */
  subtitle: string;
  /** Smaller helper text under the subtitle. */
  hint: string;
}

export function PlaceholderPanel({ title, subtitle, hint }: PlaceholderPanelProps) {
  return (
    <SectionCard title={title}>
      <div className="rounded-[var(--radius-md)] bg-surface-0 border border-dashed border-white/[0.08] p-8 text-center">
        <div className="text-sm font-semibold text-foreground">{subtitle}</div>
        <p className="mt-1 text-xs text-muted-foreground max-w-md mx-auto">
          {hint}
        </p>
      </div>
    </SectionCard>
  );
}

// ── ConnectGate ──────────────────────────────────────────────────────

export interface ConnectGateProps {
  isConnected: boolean;
  onConnect: () => void;
}

export function ConnectGate({ isConnected, onConnect }: ConnectGateProps) {
  if (isConnected) return null;
  return (
    <div className="rounded-[var(--radius-md)] bg-surface-0 border border-dashed border-white/[0.08] p-8 text-center">
      <div className="text-sm font-semibold text-foreground">
        Wallet not connected
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        Connect your wallet to view your portfolio.
      </p>
      <Button
        onClick={onConnect}
        className="mt-4 h-9 rounded-[var(--radius-md)]"
      >
        Connect wallet
      </Button>
    </div>
  );
}
