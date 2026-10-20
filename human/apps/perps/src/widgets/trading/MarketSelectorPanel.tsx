import React, { useState, useMemo, forwardRef } from "react";
import { cn } from "@/ui/utils/cn";
import { PERPS_MARKETS, coinLogoUrl } from "@/core/perps/config";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { IcoSearch, IcoClock } from "@/ui/atoms/custom-icon";

interface MarketWatchlistProps {
  /** Visual chrome: "rail" = side-panel with width + collapse toggle; "popover" = chromeless body for popover host. */
  chrome?: "rail" | "popover";
  width?: number;
  collapsed?: boolean;
  onToggle?: () => void;
  onSelect: (symbol: string) => void;
  selectedSymbol: string;
}

type FilterTab = "all" | "rwa" | "new";

const NEW_MARKET_IDS = [5, 6, 7, 8, 9, 10];
const RWA_MARKET_IDS = [5, 6, 7, 8, 9, 10];

function formatPrice(price: number): string {
  if (price >= 10000) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (price >= 1) return price.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return price.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

function fmtVol(v: number): string {
  if (v >= 1_000_000_000) return `$${(v / 1_000_000_000).toFixed(1)}B`;
  if (v >= 1_000_000) return `$${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `$${(v / 1_000).toFixed(0)}K`;
  return `$${v.toFixed(0)}`;
}

export const MarketWatchlist = forwardRef<HTMLDivElement, MarketWatchlistProps>(function MarketWatchlist({ chrome = "rail", width, collapsed, onToggle, onSelect, selectedSymbol }, ref) {
  const { prices, tickers } = useRealtimePrices();
  const [search, setSearch] = useState("");
  const [tab, setTab] = useState<FilterTab>("all");

  const filtered = useMemo(() => {
    let list = [...PERPS_MARKETS];
    if (tab === "rwa") list = list.filter((m) => RWA_MARKET_IDS.includes(m.marketId));
    else if (tab === "new") list = list.filter((m) => NEW_MARKET_IDS.includes(m.marketId));
    if (search) {
      const q = search.toLowerCase();
      list = list.filter((m) => m.symbol.toLowerCase().includes(q) || m.name.toLowerCase().includes(q));
    }
    return list;
  }, [tab, search]);

  const handleSelect = (symbol: string) => {
    onSelect(symbol);
    try {
      const recent = JSON.parse(localStorage.getItem("recentMarkets") || "[]");
      const updated = [symbol, ...recent.filter((s: string) => s !== symbol)].slice(0, 10);
      localStorage.setItem("recentMarkets", JSON.stringify(updated));
    } catch {}
  };

  // Collapsed state — just show toggle button (rail chrome only)
  if (chrome === "rail" && collapsed) {
    return (
      <div
        className="hidden md:flex flex-col items-center shrink-0 h-full"
        style={{
          width: 32,
          background: "var(--bg-subtle)",
          borderRight: "1px solid var(--border-soft)",
        }}
      >
        <button
          onClick={onToggle}
          title="Open watchlist"
          style={{
            padding: 6,
            marginTop: 8,
            color: "var(--fg-muted)",
            background: "transparent",
            border: "none",
            cursor: "pointer",
            transition: "color 150ms",
          }}
        >
          <ChevronRight className="h-4 w-4" />
        </button>
        {/* Mini market icons */}
        <div className="flex flex-col items-center gap-1.5 mt-2 overflow-y-auto scrollbar-hide flex-1 py-1">
          {PERPS_MARKETS.slice(0, 8).map((m) => {
            const isSelected = selectedSymbol === m.symbol;
            return (
              <button
                key={m.symbol}
                onClick={() => handleSelect(m.symbol)}
                title={`${m.symbol}/USD`}
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: 999,
                  border: "none",
                  padding: 0,
                  background: "transparent",
                  cursor: "pointer",
                  opacity: isSelected ? 1 : 0.6,
                  boxShadow: isSelected
                    ? "0 0 0 2px var(--accent-solid), 0 0 0 3px var(--bg)"
                    : "none",
                  transition: "all 150ms",
                }}
              >
                <img
                  src={coinLogoUrl(m.symbol)}
                  alt={m.symbol}
                  className="w-6 h-6 rounded-full object-cover"
                />
              </button>
            );
          })}
        </div>
      </div>
    );
  }

  const isPopover = chrome === "popover";

  return (
    <div
      ref={ref}
      className={cn(
        "flex flex-col overflow-hidden h-full",
        isPopover ? "w-full" : "hidden md:flex shrink-0",
      )}
      style={
        isPopover
          ? { background: "var(--bg-subtle)" }
          : {
              width: `${width ?? 220}px`,
              background: "var(--bg-subtle)",
              borderRight: "1px solid var(--border-soft)",
            }
      }
    >
      {/* Header */}
      <div
        className="flex items-center justify-between"
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          {isPopover ? "Select market" : "Watchlist"}
        </span>
        {!isPopover && (
          <button
            onClick={onToggle}
            style={{
              color: "var(--fg-muted)",
              background: "transparent",
              border: "none",
              cursor: "pointer",
              padding: 2,
              transition: "color 150ms",
            }}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {/* Search */}
      <div style={{ padding: "10px 10px 6px" }}>
        <div style={{ position: "relative" }}>
          <IcoSearch
            size={12}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 opacity-60"
          />
          <input
            placeholder="Search markets…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{
              width: "100%",
              padding: "6px 8px 6px 28px",
              borderRadius: 999,
              background: "var(--bg)",
              border: "1px solid var(--border-soft)",
              color: "var(--fg)",
              fontFamily: "var(--font-body)",
              fontSize: 11,
              outline: "none",
            }}
          />
        </div>
      </div>

      {/* Filter tabs — pill row */}
      <div
        style={{
          padding: "0 10px 8px",
        }}
      >
        <div
          style={{
            display: "flex",
            gap: 2,
            padding: 2,
            background: "var(--bg)",
            border: "1px solid var(--border-soft)",
            borderRadius: 999,
            width: "fit-content",
          }}
        >
          {([
            { key: "all", label: "All" },
            { key: "rwa", label: "RWA" },
            { key: "new", label: "New" },
          ] as { key: FilterTab; label: string }[]).map((t) => {
            const active = tab === t.key;
            return (
              <button
                key={t.key}
                onClick={() => setTab(t.key)}
                style={{
                  padding: "3px 10px",
                  borderRadius: 999,
                  border: "none",
                  background: active ? "var(--bg-subtle)" : "transparent",
                  color: active ? "var(--fg)" : "var(--fg-muted)",
                  fontFamily: "var(--font-mono)",
                  fontSize: 9,
                  letterSpacing: "0.12em",
                  textTransform: "uppercase",
                  cursor: "pointer",
                  transition: "all 150ms",
                }}
              >
                {t.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Column headers */}
      <div
        className="flex items-center"
        style={{
          padding: "8px 12px",
          fontFamily: "var(--font-mono)",
          fontSize: 9,
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
          borderTop: "1px solid var(--border-soft)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span className="flex-1">Market</span>
        <span className="w-14 text-right">Price</span>
        <span className="w-12 text-right">24h</span>
      </div>

      {/* Market list */}
      <div className="flex-1 overflow-y-auto scrollbar-custom">
        {filtered.map((m) => {
          const price = prices[m.symbol] ?? 0;
          const ticker = tickers[m.symbol];
          const open24 = ticker?.["24h_open"] ?? 0;
          const chg = open24 > 0 ? ((price - open24) / open24) * 100 : null;
          const vol = ticker?.["24h_volume"] ?? 0;
          const isSelected = selectedSymbol === m.symbol;
          const chgColor =
            chg === null ? "var(--fg-subtle)" : chg >= 0 ? "var(--success)" : "var(--danger)";
          const chgArrow = chg === null ? "" : chg >= 0 ? "▲" : "▼";
          return (
            <button
              key={m.symbol}
              onClick={() => handleSelect(m.symbol)}
              className="w-full flex items-center text-left"
              style={{
                padding: "8px 12px",
                gap: 8,
                background: isSelected
                  ? "rgba(var(--accent-solid-rgb), 0.10)"
                  : "transparent",
                borderLeft: isSelected
                  ? "2px solid var(--accent-solid)"
                  : "2px solid transparent",
                borderBottom: "1px solid var(--border-soft)",
                cursor: "pointer",
                transition: "background 150ms",
              }}
            >
              <img
                src={coinLogoUrl(m.symbol)}
                alt={m.symbol}
                className="w-5 h-5 rounded-full object-cover shrink-0"
              />
              <div className="flex-1 min-w-0" style={{ display: "flex", flexDirection: "column", gap: 1 }}>
                <div className="flex items-center" style={{ gap: 5 }}>
                  <span
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 12,
                      fontWeight: 500,
                      color: isSelected ? "var(--color-blue-200)" : "var(--fg)",
                    }}
                  >
                    {m.symbol}
                  </span>
                  {NEW_MARKET_IDS.includes(m.marketId) && (
                    <span
                      style={{
                        padding: "1px 4px",
                        borderRadius: 3,
                        background: "rgba(255, 158, 44, 0.15)",
                        color: "var(--warning)",
                        fontFamily: "var(--font-mono)",
                        fontSize: 8,
                        letterSpacing: "0.1em",
                        fontWeight: 500,
                      }}
                    >
                      NEW
                    </span>
                  )}
                </div>
                <span
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: 9,
                    color: "var(--fg-subtle)",
                    letterSpacing: "0.04em",
                    lineHeight: 1.2,
                  }}
                >
                  {vol > 0 ? `Vol ${fmtVol(vol)}` : m.name}
                </span>
              </div>
              <div
                className="w-14 text-right shrink-0"
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 11,
                  color: "var(--fg)",
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                {price > 0 ? `$${formatPrice(price)}` : "—"}
              </div>
              <div
                className="w-12 text-right shrink-0"
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 10,
                  color: chgColor,
                  fontVariantNumeric: "tabular-nums",
                  letterSpacing: "0.02em",
                }}
              >
                {chg !== null ? (
                  <>
                    <span style={{ fontSize: 8, marginRight: 2 }}>{chgArrow}</span>
                    {Math.abs(chg).toFixed(2)}%
                  </>
                ) : (
                  "—"
                )}
              </div>
            </button>
          );
        })}
        {filtered.length === 0 && (
          <div
            className="text-center"
            style={{
              padding: "24px 12px",
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              letterSpacing: "0.14em",
              textTransform: "uppercase",
              color: "var(--fg-subtle)",
            }}
          >
            No markets
          </div>
        )}
      </div>
    </div>
  );
});

// Keep old export name for backward compat (used in PairHeader onOpenMarketPanel)
export { MarketWatchlist as MarketSelectorPanel };
