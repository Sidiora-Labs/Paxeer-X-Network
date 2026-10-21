"use client";

import { Percent } from "lucide-react";
import { FeesBreakdownCard } from "./FeesBreakdownCard";
import { SectionCard } from "./PortfolioPrimitives";

/**
 * Fee tier tab.
 *
 *   ┌─────────────────────────────────┐
 *   │ Current tier · taker/maker bps  │
 *   ├─────────────────────────────────┤
 *   │ Fees breakdown (PerfDB rollup)  │
 *   └─────────────────────────────────┘
 *
 * Tier values are placeholder until volume-based tiering ships. The
 * breakdown card reads real fee data from PerfDB when an address is
 * available.
 */

export interface PortfolioFeeTierPanelProps {
  address: string | undefined;
}

export function PortfolioFeeTierPanel({ address }: PortfolioFeeTierPanelProps) {
  return (
    <div className="space-y-4 md:space-y-6">
      <SectionCard title="Fee tier">
        <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] p-5 space-y-3">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-[var(--radius-md)] bg-[var(--brand-blue)]/15 text-[var(--brand-blue)] flex items-center justify-center">
              <Percent className="h-5 w-5" />
            </div>
            <div>
              <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                Current tier
              </div>
              <div className="text-base font-semibold text-foreground">
                Tier 1 · Taker 3 bps / Maker 0.5 bps rebate
              </div>
            </div>
          </div>
          <p className="text-xs text-muted-foreground leading-relaxed">
            Fee tiers are applied automatically based on 30d volume and PAX
            holdings. Volume-based upgrades and PoFQ rebates will unlock as
            the competition program rolls out.
          </p>
        </div>
      </SectionCard>

      {address ? <FeesBreakdownCard userAddress={address} /> : null}
    </div>
  );
}
