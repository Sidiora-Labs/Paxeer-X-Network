"use client";

import {
  LayoutDashboard,
  Briefcase,
  ClipboardList,
  Receipt,
  Wallet,
  Percent,
  KeyRound,
  Settings as SettingsIcon,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/ui/utils/cn";
import type { PortfolioNavKey } from "./PortfolioSideNav";

/**
 * Horizontal pill nav for the /portfolio page on mobile. Same key set as
 * `PortfolioSideNav`, just rendered as a scrollable row rather than a
 * collapsible vertical list.
 */

const MOBILE_NAV: Array<{ key: PortfolioNavKey; label: string; Icon: LucideIcon }> = [
  { key: "overview", label: "Overview", Icon: LayoutDashboard },
  { key: "positions", label: "Positions", Icon: Briefcase },
  { key: "orders", label: "Orders", Icon: ClipboardList },
  { key: "settlements", label: "Settlements", Icon: Receipt },
  { key: "assets", label: "Assets", Icon: Wallet },
  { key: "feeTier", label: "Fees", Icon: Percent },
  { key: "apiKeys", label: "API", Icon: KeyRound },
  { key: "settings", label: "Settings", Icon: SettingsIcon },
];

export interface PortfolioMobileNavProps {
  active: PortfolioNavKey;
  onChange: (k: PortfolioNavKey) => void;
}

export function PortfolioMobileNav({ active, onChange }: PortfolioMobileNavProps) {
  return (
    <div className="flex gap-1.5 overflow-x-auto -mx-4 px-4 scrollbar-hide">
      {MOBILE_NAV.map((item) => {
        const isActive = item.key === active;
        const { Icon } = item;
        return (
          <button
            key={item.key}
            type="button"
            aria-pressed={isActive}
            onClick={() => onChange(item.key)}
            className={cn(
              "shrink-0 inline-flex items-center gap-1.5 h-9 px-3 rounded-[var(--radius-pill)] text-xs font-medium transition-colors",
              "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
              isActive
                ? "bg-[var(--brand-blue)]/15 text-[var(--brand-blue)] border border-[var(--brand-blue)]/30"
                : "bg-surface-1/80 text-muted-foreground hover:text-foreground border border-transparent",
            )}
          >
            <Icon className="h-3.5 w-3.5" />
            {item.label}
          </button>
        );
      })}
    </div>
  );
}
