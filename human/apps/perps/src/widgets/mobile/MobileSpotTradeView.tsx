import { useState } from "react";
import dynamic from "next/dynamic";
import { Book, BookX } from "lucide-react";
import { type SpotMarket } from "@/core/spot/config";
import { OrderbookSkeleton, OrderFormSkeleton, PositionsSkeleton } from "@/ui/atoms/skeleton-panel";
import { MobileSpotBalancePanel } from "./MobileSpotBalancePanel";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotTradeView — SPOT mirror of MobileTradeView.

     · Top      — MobileSpotBalancePanel (3-cell stables/holdings/realized)
     · Toolbar  — Hide / Show Book toggle
     · Body     — order form (≈72%) + compact orderbook (≈28%)
     · Below    — SpotPositionsPanel (handles its own tab strip)

   Layout proportions, motion, and fonts are identical to the perps
   trade view to preserve cross-mode muscle memory.
   ══════════════════════════════════════════════════════════════════════ */

const SpotOrderFormWidget = dynamic(
  () => import("@/widgets/spot/SpotOrderForm").then((m) => ({ default: m.SpotOrderForm })),
  { ssr: false, loading: () => <OrderFormSkeleton /> },
);
const SpotOrderbookWidget = dynamic(
  () => import("@/widgets/spot/SpotOrderbook").then((m) => ({ default: m.SpotOrderbook })),
  { ssr: false, loading: () => <OrderbookSkeleton /> },
);
const SpotPositionsPanelWidget = dynamic(
  () =>
    import("@/widgets/spot/SpotPositionsPanel").then((m) => ({ default: m.SpotPositionsPanel })),
  { ssr: false, loading: () => <PositionsSkeleton /> },
);

interface MobileSpotTradeViewProps {
  market: SpotMarket;
}

export function MobileSpotTradeView({ market }: MobileSpotTradeViewProps) {
  const [showBook, setShowBook] = useState(true);

  return (
    <div className="flex flex-col md:hidden" style={{ background: "var(--bg)" }}>
      {/* Balance summary row */}
      <MobileSpotBalancePanel />

      {/* Hide / Show Book toggle */}
      <div
        className="flex justify-end px-3 py-1"
        style={{ borderBottom: "1px solid var(--border-soft)" }}
      >
        <button
          type="button"
          onClick={() => setShowBook((v) => !v)}
          className="flex items-center gap-1 px-2 py-1 rounded transition-colors"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.08em",
            color: showBook ? "var(--accent-solid)" : "var(--fg-muted)",
            background: showBook ? "rgba(0,76,237,0.08)" : "transparent",
          }}
        >
          {showBook ? <BookX className="w-3 h-3" /> : <Book className="w-3 h-3" />}
          {showBook ? "Hide Book" : "Show Book"}
        </button>
      </div>

      {/* Two-column body: order form left + compact orderbook right */}
      <div className="flex items-stretch" style={{ minHeight: 480 }}>
        <div className="min-w-0" style={{ flex: showBook ? "0 0 72%" : "1 1 100%" }}>
          <SpotOrderFormWidget market={market} compact />
        </div>
        {showBook && (
          <div
            className="shrink-0 overflow-hidden"
            style={{
              flex: "0 0 28%",
              borderLeft: "1px solid var(--border-soft)",
            }}
          >
            <SpotOrderbookWidget symbol={market.symbol} height={480} compact />
          </div>
        )}
      </div>

      {/* Secondary panel — SpotPositionsPanel owns its own tab strip */}
      <div
        className="border-t"
        style={{ borderColor: "var(--border-soft)", minHeight: 360 }}
      >
        <SpotPositionsPanelWidget />
      </div>
    </div>
  );
}
