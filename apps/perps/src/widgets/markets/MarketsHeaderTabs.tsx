import React from "react";
import { cn } from "@/ui/utils/cn";
import { isSpotOpen } from "@/core/nav";
import { useMarketsStreamConnection } from "@/hooks/perps/useMarketsStream";

export type MarketsTab = "markets" | "funding";
export type MarketsMode = "perps" | "spot";

export function marketsModes(spotOpen: boolean = isSpotOpen()): MarketsMode[] {
  return spotOpen ? ["perps", "spot"] : ["perps"];
}

const MODE_LABELS: Record<MarketsMode, string> = { perps: "Perps", spot: "Spot" };

const MODE_ACTIVE_CLASS: Record<MarketsMode, string> = {
  perps: "bg-[var(--brand-blue)]/20 text-[var(--brand-blue)] shadow-sm",
  spot: "bg-emerald-500/20 text-emerald-400 shadow-sm",
};

interface MarketsHeaderTabsProps {
  activeTab: MarketsTab;
  onChangeTab: (tab: MarketsTab) => void;
  /** Active market mode (perps vs spot). */
  mode?: MarketsMode;
  onChangeMode?: (mode: MarketsMode) => void;
  /** Optional — when set, appears inline next to the tabs (desktop only). */
  rightSlot?: React.ReactNode;
}

/**
 * Top-level page tabs: `Markets | Funding`. Mirrors the reference design
 * where the "Funding" tab is present but empty (placeholder hook for a
 * future funding-rate-only view).
 */
export function MarketsHeaderTabs({
  activeTab,
  onChangeTab,
  mode = "perps",
  onChangeMode,
  rightSlot,
}: MarketsHeaderTabsProps) {
  const connection = useMarketsStreamConnection();

  return (
    <div className="flex items-center justify-between gap-3 mb-4 md:mb-6">
      <div className="flex items-center gap-1">
        {onChangeMode ? (
          <ModeToggle mode={mode} onChange={onChangeMode} />
        ) : null}
        {onChangeMode ? (
          <span className="mx-1 h-5 w-px bg-white/[0.08]" />
        ) : null}
        <TabButton
          label="Markets"
          active={activeTab === "markets"}
          onClick={() => onChangeTab("markets")}
        />
        {mode === "perps" ? (
          <TabButton
            label="Funding"
            active={activeTab === "funding"}
            onClick={() => onChangeTab("funding")}
          />
        ) : null}
        <LiveIndicator state={connection} />
      </div>
      {rightSlot ? (
        <div className="hidden md:flex items-center">{rightSlot}</div>
      ) : null}
    </div>
  );
}

function TabButton({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "relative px-3 md:px-4 py-2 text-sm md:text-base font-semibold tracking-tight transition-colors",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 rounded-[var(--radius-md)]",
        active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
      <span
        aria-hidden
        className={cn(
          "absolute left-3 right-3 -bottom-[1px] h-[2px] rounded-full transition-opacity",
          active ? "opacity-100 bg-[var(--brand-blue)]" : "opacity-0 bg-transparent",
        )}
      />
    </button>
  );
}

function ModeToggle({
  mode,
  onChange,
}: {
  mode: MarketsMode;
  onChange: (mode: MarketsMode) => void;
}) {
  return (
    <div className="flex items-center gap-0.5 p-0.5 rounded-[var(--radius-pill)] bg-surface-1 border border-white/[0.06]">
      {marketsModes().map((m) => (
        <button
          key={m}
          type="button"
          onClick={() => onChange(m)}
          className={cn(
            "px-3 py-1 text-xs font-semibold rounded-[var(--radius-pill)] transition-all",
            mode === m ? MODE_ACTIVE_CLASS[m] : "text-muted-foreground hover:text-foreground",
          )}
        >
          {MODE_LABELS[m]}
        </button>
      ))}
    </div>
  );
}

function LiveIndicator({ state }: { state: string }) {
  const open = state === "open";
  const connecting = state === "connecting" || state === "idle";
  return (
    <span
      className={cn(
        "ml-1 hidden sm:inline-flex items-center gap-1.5 px-2 py-[3px] rounded-[var(--radius-pill)] text-[10px] font-mono uppercase tracking-widest",
        open
          ? "bg-emerald-500/10 text-emerald-400"
          : connecting
            ? "bg-amber-500/10 text-amber-400"
            : "bg-red-500/10 text-red-400",
      )}
      title={`Feed ${state}`}
    >
      <span
        className={cn(
          "inline-block h-1.5 w-1.5 rounded-full",
          open
            ? "bg-emerald-400 animate-pulse"
            : connecting
              ? "bg-amber-400 animate-pulse"
              : "bg-red-400",
        )}
      />
      {open ? "Live" : connecting ? "Connecting" : "Offline"}
    </span>
  );
}
