import { useState, useMemo } from "react";
import { X, Search } from "lucide-react";
import { PERPS_MARKETS, coinLogoUrl, type PerpsMarket } from "@/core/perps/config";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { fmtPrice, fmtUsdCompact } from "@/ui/utils/format";
import { MobileHeader } from "./MobileHeader";
import { MobileTickerBar } from "./MobileTickerBar";
import { MobileMarketView } from "./MobileMarketView";
import { MobileTradeView } from "./MobileTradeView";
import { MobileAccountView } from "./MobileAccountView";
import { MobileBottomNav, type MobileGlobalTab } from "./MobileBottomNav";

interface MobileAppLayoutProps {
  market: PerpsMarket;
  onSelectMarket: (symbol: string) => void;
}

export function MobileAppLayout({ market, onSelectMarket }: MobileAppLayoutProps) {
  const [globalTab, setGlobalTab] = useState<MobileGlobalTab>("markets");
  const [marketSelectorOpen, setMarketSelectorOpen] = useState(false);

  return (
    <div
      className="flex flex-col h-[100dvh] md:hidden"
      style={{ background: "var(--bg)" }}
    >
      {/* Global top app bar */}
      <MobileHeader />

      {/* Market header strip (shown for Markets + Trade tabs) */}
      {(globalTab === "markets" || globalTab === "trade") && (
        <MobileTickerBar
          market={market}
          onOpenSelector={() => setMarketSelectorOpen(true)}
        />
      )}

      {/* Main scrollable content area */}
      <div className="flex-1 min-h-0 overflow-y-auto" id="app-scroll">
        {globalTab === "markets" && (
          <MobileMarketView market={market} />
        )}
        {globalTab === "trade" && (
          <MobileTradeView market={market} />
        )}
        {globalTab === "account" && <MobileAccountView />}
      </div>

      {/* Fixed Bottom Navigation — Markets | Trade | Account */}
      <MobileBottomNav active={globalTab} onChange={setGlobalTab} />

      {/* Market Selector Overlay */}
      {marketSelectorOpen && (
        <MarketSelectorOverlay
          currentSymbol={market.symbol}
          onSelect={(sym) => {
            onSelectMarket(sym);
            setMarketSelectorOpen(false);
          }}
          onClose={() => setMarketSelectorOpen(false)}
        />
      )}
    </div>
  );
}

/* ── Market Selector Overlay ─────────────────────────────────── */

function MarketSelectorOverlay({
  currentSymbol,
  onSelect,
  onClose,
}: {
  currentSymbol: string;
  onSelect: (symbol: string) => void;
  onClose: () => void;
}) {
  const [search, setSearch] = useState("");
  const { prices, tickers } = useRealtimePrices();

  const filtered = useMemo(() => {
    if (!search) return PERPS_MARKETS;
    const q = search.toLowerCase();
    return PERPS_MARKETS.filter(
      (m) =>
        m.symbol.toLowerCase().includes(q) ||
        m.name.toLowerCase().includes(q)
    );
  }, [search]);

  return (
    <div
      className="fixed inset-0 z-[100] flex flex-col overflow-hidden"
      style={{ background: "var(--bg)" }}
    >
      <div
        className="flex items-center justify-between px-4 border-b"
        style={{ height: 52, borderColor: "var(--border-soft)" }}
      >
        <span
          className="text-sm font-semibold"
          style={{ color: "var(--fg)" }}
        >
          Markets
        </span>
        <button
          onClick={onClose}
          className="p-1 transition-colors"
          style={{ color: "var(--fg-muted)" }}
          aria-label="Close"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      <div
        className="px-4 py-2.5 border-b"
        style={{ borderColor: "var(--border-soft)" }}
      >
        <div className="relative">
          <Search
            className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5"
            style={{ color: "var(--fg-subtle)" }}
          />
          <input
            placeholder="Search markets…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-full pl-8 pr-3 py-2 text-sm outline-none"
            style={{
              background: "var(--bg-subtle)",
              border: "1px solid var(--border-soft)",
              color: "var(--fg)",
            }}
            autoFocus
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {filtered.map((m) => {
          const mPrice = prices[m.symbol] ?? 0;
          const mTicker = tickers[m.symbol];
          const open24 = mTicker?.["24h_open"] ?? 0;
          const chg =
            open24 > 0 ? ((mPrice - open24) / open24) * 100 : null;
          const isSelected = currentSymbol === m.symbol;

          return (
            <button
              key={m.symbol}
              onClick={() => onSelect(m.symbol)}
              className="w-full grid grid-cols-[1fr_auto_auto] gap-2 px-4 py-2.5 items-center transition-colors"
              style={{
                borderBottom: "1px solid var(--border-soft)",
                background: isSelected
                  ? "rgba(0, 76, 237, 0.10)"
                  : undefined,
                borderLeft: isSelected
                  ? "2px solid var(--accent-solid)"
                  : "2px solid transparent",
              }}
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <img
                  src={coinLogoUrl(m.symbol)}
                  alt=""
                  className="w-7 h-7 rounded-full object-cover shrink-0"
                />
                <div className="flex flex-col items-start">
                  <span
                    className="text-sm font-semibold"
                    style={{ color: "var(--fg)" }}
                  >
                    {m.symbol}
                  </span>
                  <span
                    className="text-[9px] font-mono"
                    style={{ color: "var(--fg-subtle)" }}
                  >
                    {m.name}
                  </span>
                </div>
              </div>
              <span
                className="text-xs font-mono text-right tabular-nums w-20"
                style={{ color: "var(--fg)" }}
              >
                {mPrice > 0 ? fmtPrice(mPrice) : "\u2014"}
              </span>
              <span
                className="text-xs font-mono text-right tabular-nums w-14"
                style={{
                  color:
                    chg === null
                      ? "var(--fg-subtle)"
                      : chg >= 0
                        ? "var(--success)"
                        : "var(--danger)",
                }}
              >
                {chg !== null
                  ? `${chg >= 0 ? "+" : ""}${chg.toFixed(2)}%`
                  : "\u2014"}
              </span>
            </button>
          );
        })}
        {filtered.length === 0 && (
          <div
            className="flex items-center justify-center py-12"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-subtle)",
            }}
          >
            No markets found
          </div>
        )}
      </div>
    </div>
  );
}
