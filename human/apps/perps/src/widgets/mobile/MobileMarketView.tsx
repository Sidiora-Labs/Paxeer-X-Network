import { useState } from "react";
import dynamic from "next/dynamic";
import { ExternalLink } from "lucide-react";
import { type PerpsMarket } from "@/core/perps/config";
import {
  hasDedicatedMarketStream,
  useSidPaxTrades,
} from "@/hooks/perps/useSidPaxMarketStream";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { ChartSkeleton, OrderbookSkeleton } from "@/ui/atoms/skeleton-panel";
import { MobileSecondaryTabs } from "./MobileSecondaryTabs";
import { MobilePanelToolbar } from "./MobilePanelToolbar";
import { MobileSecondaryTabContent } from "./MobileSecondaryTabContent";

const Chart = dynamic(
  () => import("@/widgets/trading/Chart").then((m) => ({ default: m.Chart })),
  { ssr: false, loading: () => <ChartSkeleton /> }
);
const Orderbook = dynamic(
  () => import("@/widgets/trading/Orderbook").then((m) => ({ default: m.Orderbook })),
  { ssr: false, loading: () => <OrderbookSkeleton /> }
);

type SubTab = "chart" | "orderbook" | "trades";

const SUB_TABS: { key: SubTab; label: string }[] = [
  { key: "chart", label: "Chart" },
  { key: "orderbook", label: "Order Book" },
  { key: "trades", label: "Trades" },
];

interface MobileMarketViewProps {
  market: PerpsMarket;
}

export function MobileMarketView({ market }: MobileMarketViewProps) {
  const [activeTab, setActiveTab] = useState<SubTab>("chart");
  const [secondaryTab, setSecondaryTab] = useState("Balances");
  const [sortValue, setSortValue] = useState("Market");

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

      {/* Dynamic center view */}
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
            <Orderbook symbol={market.symbol} height={440} />
          </div>
        )}
        {activeTab === "trades" && (
          <MobileTradesTab symbol={market.symbol} />
        )}
      </div>

      {/* Secondary tab row */}
      <MobileSecondaryTabs active={secondaryTab} onChange={setSecondaryTab} />

      {/* Panel toolbar */}
      <MobilePanelToolbar sortValue={sortValue} onSortChange={setSortValue} />

      {/* Secondary tab content */}
      <MobileSecondaryTabContent activeTab={secondaryTab} />
    </div>
  );
}

/* ── Trades sub-tab ─────────────────────────────────────── */

function fmtPrice(price: number): string {
  if (price >= 10000) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

function MobileTradesTab({ symbol }: { symbol: string }) {
  const { tickers } = useRealtimePrices();
  const isDedicated = hasDedicatedMarketStream(symbol);
  const dedicatedTrades = useSidPaxTrades(isDedicated ? symbol : undefined);
  const ticker = tickers[symbol];
  const tickerTrades = (ticker as any)?.recent_trades as
    | { time: string; price: number; qty: number; side: string }[]
    | undefined;

  const trades: { time: string; price: number; qty: number; side: string }[] | undefined =
    isDedicated
      ? dedicatedTrades.map((t) => ({
          time: new Date(t.trade_time).toISOString().slice(11, 19),
          price: t.price,
          qty: t.size,
          side: t.side === "BUY" ? "buy" : "sell",
        }))
      : tickerTrades;

  return (
    <div className="flex flex-col" style={{ minHeight: 300 }}>
      {/* Column headers: Price | Size | Time */}
      <div
        className="grid grid-cols-[1fr_1fr_auto] px-4 py-2 border-b"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 9,
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
          borderColor: "var(--border-soft)",
        }}
      >
        <span>Price</span>
        <span className="text-right">Size</span>
        <span className="text-right w-16">Time</span>
      </div>
      <div className="overflow-y-auto" style={{ maxHeight: 350 }}>
        {trades && trades.length > 0 ? (
          trades.map((t, i) => (
            <div
              key={i}
              className="grid grid-cols-[1fr_1fr_auto] items-center px-4 py-1.5"
              style={{ fontFamily: "var(--font-mono)", fontSize: 11 }}
            >
              <span
                className="tabular-nums"
                style={{ color: t.side === "buy" ? "var(--success)" : "var(--danger)" }}
              >
                {fmtPrice(t.price)}
              </span>
              <span className="tabular-nums text-right" style={{ color: "var(--fg)" }}>
                {t.qty.toFixed(4)}
              </span>
              <span className="tabular-nums text-right flex items-center gap-1 w-16 justify-end" style={{ color: "var(--fg-muted)" }}>
                {t.time}
                <ExternalLink className="w-2.5 h-2.5 shrink-0" style={{ color: "var(--fg-subtle)" }} />
              </span>
            </div>
          ))
        ) : (
          <div
            className="flex items-center justify-center py-12"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              color: "var(--fg-subtle)",
              letterSpacing: "0.1em",
              textTransform: "uppercase",
            }}
          >
            Loading trades...
          </div>
        )}
      </div>
    </div>
  );
}
