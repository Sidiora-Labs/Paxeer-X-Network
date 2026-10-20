import { useState } from "react";
import dynamic from "next/dynamic";
import { type SpotMarket } from "@/core/spot/config";
import { ChartSkeleton, OrderbookSkeleton, PositionsSkeleton } from "@/ui/atoms/skeleton-panel";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotMarketView — SPOT mirror of MobileMarketView.

   Primary tab row: Chart | Order Book | Trades.
   The dynamic centre view embeds the existing SPOT widgets (compact
   variants for the order-book and the trades tape).

   Below the centre view, the SpotPositionsPanel is embedded as the
   secondary panel — its built-in tab strip already covers the
   `Holdings | Open Orders | Order History | Trade History | Closed Legs`
   surface that the perps SecondaryTabs equivalent provides for perps.
   ══════════════════════════════════════════════════════════════════════ */

const Chart = dynamic(
  () => import("@/widgets/trading/Chart").then((m) => ({ default: m.Chart })),
  { ssr: false, loading: () => <ChartSkeleton /> },
);
const SpotOrderbookWidget = dynamic(
  () => import("@/widgets/spot/SpotOrderbook").then((m) => ({ default: m.SpotOrderbook })),
  { ssr: false, loading: () => <OrderbookSkeleton /> },
);
const SpotTradesTapeWidget = dynamic(
  () => import("@/widgets/spot/SpotTradesTape").then((m) => ({ default: m.SpotTradesTape })),
  { ssr: false, loading: () => null },
);
const SpotPositionsPanelWidget = dynamic(
  () =>
    import("@/widgets/spot/SpotPositionsPanel").then((m) => ({ default: m.SpotPositionsPanel })),
  { ssr: false, loading: () => <PositionsSkeleton /> },
);

type SubTab = "chart" | "orderbook" | "trades";

const SUB_TABS: { key: SubTab; label: string }[] = [
  { key: "chart", label: "Chart" },
  { key: "orderbook", label: "Order Book" },
  { key: "trades", label: "Trades" },
];

interface MobileSpotMarketViewProps {
  market: SpotMarket;
}

export function MobileSpotMarketView({ market }: MobileSpotMarketViewProps) {
  const [activeTab, setActiveTab] = useState<SubTab>("chart");

  return (
    <div className="flex flex-col md:hidden">
      {/* Primary tab row — Chart | Order Book | Trades */}
      <div
        className="flex border-b"
        style={{ borderColor: "var(--border-soft)" }}
      >
        {SUB_TABS.map((tab) => {
          const isActive = activeTab === tab.key;
          return (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key)}
              className="flex-1 py-2.5 text-center transition-colors relative"
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                letterSpacing: "0.06em",
                color: isActive ? "var(--accent-solid)" : "var(--fg-muted)",
                fontWeight: isActive ? 600 : 400,
              }}
            >
              {tab.label}
              {isActive && (
                <div
                  className="absolute bottom-0 left-1/4 right-1/4 h-[2px] rounded-full"
                  style={{ background: "var(--accent-solid)" }}
                />
              )}
            </button>
          );
        })}
      </div>

      {/* Dynamic centre view */}
      <div style={{ background: "var(--bg)" }}>
        {activeTab === "chart" && (
          <div style={{ height: 432 }}>
            <Chart
              selectedPair={`${market.symbol}/USD`}
              height={432}
              onHeightChange={() => {}}
              positions={[]}
            />
          </div>
        )}
        {activeTab === "orderbook" && (
          <div style={{ height: 440 }}>
            <SpotOrderbookWidget symbol={market.symbol} height={440} compact />
          </div>
        )}
        {activeTab === "trades" && (
          <div style={{ height: 440 }}>
            <SpotTradesTapeWidget symbol={market.symbol} compact />
          </div>
        )}
      </div>

      {/* Secondary panel — SpotPositionsPanel handles its own tab strip */}
      <div
        className="border-t"
        style={{ borderColor: "var(--border-soft)", minHeight: 360 }}
      >
        <SpotPositionsPanelWidget />
      </div>
    </div>
  );
}
