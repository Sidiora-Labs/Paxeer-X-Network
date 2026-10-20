"use client";

import { ArrowDownToLine, ArrowUpFromLine } from "lucide-react";
import { Button } from "@/ui/atoms/button";
import type { PortfolioNavKey } from "./PortfolioSideNav";

/**
 * Sticky-feeling header strip rendered above the active tab panel.
 *
 *   PORTFOLIO                                   [Connect | Create vault | Deposit / Withdraw]
 *   Active tab title
 *
 * The CTA cluster on the right adapts to wallet/vault state so the user
 * always sees the next action they can actually take.
 */

const NAV_LABELS: Record<PortfolioNavKey, string> = {
  overview: "Overview",
  positions: "Positions",
  orders: "Orders",
  settlements: "Settlements",
  assets: "Assets",
  feeTier: "Fee tier",
  apiKeys: "API Keys",
  settings: "Settings",
};

export interface PortfolioTopBarProps {
  activeNav: PortfolioNavKey;
  isConnected: boolean;
  hasVault: boolean;
  /** True once the on-chain vault lookup has resolved. Suppresses CTA flicker. */
  vaultResolved: boolean;
  onDeposit: () => void;
  onWithdraw: () => void;
  onConnect: () => void;
}

export function PortfolioTopBar({
  activeNav,
  isConnected,
  hasVault,
  vaultResolved,
  onDeposit,
  onWithdraw,
  onConnect,
}: PortfolioTopBarProps) {
  return (
    <div className="flex items-center justify-between gap-3 flex-wrap">
      <div>
        <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
          Portfolio
        </div>
        <h1 className="mt-0.5 text-xl md:text-2xl font-semibold text-foreground tracking-tight">
          {NAV_LABELS[activeNav]}
        </h1>
      </div>

      <div className="flex items-center gap-2">
        {!isConnected ? (
          <Button onClick={onConnect} className="h-9 rounded-[var(--radius-md)]">
            Connect wallet
          </Button>
        ) : !vaultResolved ? (
          <div className="h-9 px-3 rounded-[var(--radius-md)] bg-surface-1 border border-white/[0.04] flex items-center text-xs text-muted-foreground">
            Resolving vault…
          </div>
        ) : !hasVault ? (
          <Button onClick={onDeposit} className="h-9 rounded-[var(--radius-md)]">
            Create vault
          </Button>
        ) : (
          <>
            <Button onClick={onDeposit} className="h-9 rounded-[var(--radius-md)]">
              <ArrowDownToLine className="h-4 w-4 mr-1.5" />
              Deposit
            </Button>
            <Button
              onClick={onWithdraw}
              variant="outline"
              className="h-9 rounded-[var(--radius-md)]"
            >
              <ArrowUpFromLine className="h-4 w-4 mr-1.5" />
              Withdraw
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
