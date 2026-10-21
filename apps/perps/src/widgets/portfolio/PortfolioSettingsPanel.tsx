"use client";

import { Button } from "@/ui/atoms/button";
import { SectionCard } from "./PortfolioPrimitives";

/**
 * Settings tab — opens the existing global `SettingsModal` rather than
 * duplicating its theme/preferences UI inline.
 */

export interface PortfolioSettingsPanelProps {
  onOpenFullSettings: () => void;
}

export function PortfolioSettingsPanel({
  onOpenFullSettings,
}: PortfolioSettingsPanelProps) {
  return (
    <SectionCard title="Settings">
      <div className="rounded-[var(--radius-md)] bg-surface-0 border border-white/[0.04] p-5 space-y-4">
        <div>
          <div className="text-sm font-semibold text-foreground">
            Trading preferences
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Theme, chart defaults, slippage presets, notifications.
          </p>
        </div>
        <Button
          onClick={onOpenFullSettings}
          variant="outline"
          className="h-9 rounded-[var(--radius-md)]"
        >
          Open settings
        </Button>
      </div>
    </SectionCard>
  );
}
