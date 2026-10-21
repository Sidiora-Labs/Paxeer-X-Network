"use client";

import * as React from "react";
import { cn } from "../lib/utils";
import { Badge } from "./badge";

export interface BankCardData {
  holder: string;
  /** Masked number, e.g. "6464 XXXX XXXX 9980". */
  number: string;
  kind?: string; // "Virtual card"
  balanceLabel?: string;
  balance?: string;
  expiry?: string;
  brand?: string; // "VISA"
  status?: { label: string; tone?: "success" | "neutral" | "destructive" };
  theme?: "light" | "dark";
}

/** Payment-card visual from the wallet/cards screens. */
export function BankCard({ data, className }: { data: BankCardData; className?: string }) {
  const dark = data.theme === "dark";
  return (
    <div
      className={cn(
        "relative flex aspect-[8/5] w-full flex-col justify-between overflow-hidden rounded-lg p-5 shadow-card",
        dark
          ? "bg-[#101418] text-white"
          : "bg-[linear-gradient(135deg,#eef3fa_0%,#e2eaf5_55%,#dbe5f2_100%)] text-foreground",
        className,
      )}
    >
      {/* subtle texture */}
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute inset-0",
          dark
            ? "bg-[radial-gradient(120%_90%_at_80%_0%,rgb(255_255_255/0.08),transparent_60%)]"
            : "bg-[radial-gradient(120%_90%_at_80%_0%,rgb(255_255_255/0.7),transparent_60%)]",
        )}
      />
      <div className="relative flex items-start justify-between gap-3">
        <span className="text-[13px] font-bold tracking-[0.12em] uppercase">{data.holder}</span>
        {data.status && (
          <Badge variant={data.status.tone ?? "success"} size="sm" className="bg-surface/80">
            {data.status.label}
          </Badge>
        )}
      </div>
      <div className="relative flex items-end justify-between gap-4">
        <div className="flex flex-col gap-1">
          <span className={cn("text-xs", dark ? "text-white/60" : "text-muted-foreground")}>
            {data.kind ?? "Virtual card"}
          </span>
          <span className="text-[15px] font-semibold tracking-[0.06em] tabular-nums">
            {data.number}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1">
          <span className={cn("text-xs", dark ? "text-white/60" : "text-muted-foreground")}>
            {data.balance ? (data.balanceLabel ?? "Balance") : "Expiry"}
          </span>
          <span className="text-[15px] font-semibold tabular-nums">
            {data.balance ?? data.expiry}
          </span>
        </div>
      </div>
      <div className="relative flex items-center justify-between">
        <span className={cn("text-lg font-black tracking-tight", dark ? "text-white" : "text-foreground")}>
          ≋
        </span>
        <span className="text-sm font-black tracking-[0.08em] uppercase italic">
          {data.brand ?? "VISA"}
        </span>
      </div>
    </div>
  );
}

/* --------------------------------------------------------- CardCarousel */

/** Horizontal card pager with dot indicator, as on the Cards screen. */
export function CardCarousel({
  cards,
  renderCard,
  className,
}: {
  cards: BankCardData[];
  renderCard?: (card: BankCardData, index: number) => React.ReactNode;
  className?: string;
}) {
  const [active, setActive] = React.useState(0);
  const trackRef = React.useRef<HTMLDivElement>(null);

  const onScroll = () => {
    const el = trackRef.current;
    if (!el) return;
    const i = Math.round(el.scrollLeft / el.clientWidth);
    setActive(Math.max(0, Math.min(cards.length - 1, i)));
  };

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <div
        ref={trackRef}
        onScroll={onScroll}
        className="lx-scroll flex snap-x snap-mandatory gap-3 overflow-x-auto"
      >
        {cards.map((c, i) => (
          <div key={i} className="w-full shrink-0 snap-center">
            {renderCard ? renderCard(c, i) : <BankCard data={c} />}
          </div>
        ))}
      </div>
      {cards.length > 1 && (
        <div className="flex items-center justify-center gap-1.5" aria-hidden>
          {cards.map((_, i) => (
            <span
              key={i}
              className={cn(
                "size-1.5 rounded-full transition-colors",
                i === active ? "bg-foreground" : "bg-border-strong",
              )}
            />
          ))}
        </div>
      )}
    </div>
  );
}
