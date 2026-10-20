import { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { cn } from "@/ui/utils/cn";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useSidPaxOrderbook } from "@/hooks/perps/useSidPaxMarketStream";
import { isVenueSymbol, useVenueBook, useVenueFills } from "@/core/net/venueStream";

export interface OrderbookLevel {
  price: number;
  size: number;
  total: number;
}

interface OrderbookData {
  bids: OrderbookLevel[];
  asks: OrderbookLevel[];
  spread: number;
  spreadPct: number;
  lastUpdateTime: number;
}

interface OrderbookProps {
  symbol: string;
  height?: number;
  onPriceClick?: (price: number) => void;
  /** Mobile compact mode: 2 columns (Price + Total), no Size column */
  compact?: boolean;
}

// Max levels kept in memory per side. Higher cap lets tall containers render
// deeper books. The actual visible row count is dynamic (see halfLevels below).
const MAX_LEVELS = 33;
// Actual row height in the UI — `padding: 5px 12px` + `fontSize: 12` with
// default line-height renders each row at ~22-24px. Using the measured value
// lets the orderbook fill its container without over-reserving per row.
const ROW_H_PX = 23;
// Fixed chrome in the orderbook shell: column header (~29px) + mid/spread
// band (~60px). Updated from the original formula which assumed 40px of
// chrome + 40px rows — both wrong, which is why the book looked sparse.
const CHROME_H_PX = 95;
// Always render at least this many levels per side so the book never collapses
// to 1-2 rows on very short containers (e.g. a narrow pane after resize).
const MIN_LEVELS_PER_SIDE = 4;

function buildBook(
  rawAsks: { price: number; quantity: number }[],
  rawBids: { price: number; quantity: number }[]
): OrderbookData {
  const processLevels = (levels: { price: number; quantity: number }[], isBids: boolean): OrderbookLevel[] => {
    if (!Array.isArray(levels)) return [];
    const sorted = [...levels].sort((a, b) =>
      isBids ? b.price - a.price : a.price - b.price
    );
    let total = 0;
    return sorted.slice(0, MAX_LEVELS).map((level) => {
      total += level.quantity;
      return { price: level.price, size: level.quantity, total };
    });
  };

  const bids = processLevels(rawBids, true);
  const asks = processLevels(rawAsks, false);

  const bestBid = bids[0]?.price ?? 0;
  const bestAsk = asks[0]?.price ?? 0;
  const spread = bestAsk > 0 && bestBid > 0 ? bestAsk - bestBid : 0;
  const mid = (bestAsk + bestBid) / 2;
  const spreadPct = mid > 0 ? (spread / mid) * 100 : 0;

  return { bids, asks, spread, spreadPct, lastUpdateTime: Date.now() };
}

export function Orderbook({ symbol, height = 580, onPriceClick, compact = false }: OrderbookProps) {
  const [book, setBook] = useState<OrderbookData | null>(null);
  // F-19: coalesce hot orderbook updates onto a single rAF tick.
  //
  // The WS can deliver multiple full-book frames within the same
  // animation frame (Sidiora pushes deltas at >60 Hz during volatility
  // spikes). React 18 batches state updates but the *prior* state is
  // still rebuilt for every frame, which costs O(MAX_LEVELS) work and
  // forces a layout per update. We collapse N updates inside one
  // frame down to one render of the *latest* book by holding the
  // pending value in a ref and only calling `setBook` from rAF.
  const pendingBookRef = useRef<OrderbookData | null>(null);
  const rafIdRef = useRef<number | null>(null);

  const scheduleBookUpdate = useCallback((next: OrderbookData) => {
    pendingBookRef.current = next;
    if (rafIdRef.current !== null) return;
    if (typeof window === "undefined" || typeof window.requestAnimationFrame !== "function") {
      // SSR / non-browser fallback — apply synchronously.
      setBook(next);
      pendingBookRef.current = null;
      return;
    }
    rafIdRef.current = window.requestAnimationFrame(() => {
      rafIdRef.current = null;
      const pending = pendingBookRef.current;
      pendingBookRef.current = null;
      if (pending) setBook(pending);
    });
  }, []);

  useEffect(() => {
    return () => {
      if (rafIdRef.current !== null && typeof window !== "undefined") {
        window.cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      pendingBookRef.current = null;
    };
  }, []);
  // The venue book of the gateway; the Chainflow reference book while the
  // gateway socket is not open or the venue does not list the symbol.
  const venueBook = useVenueBook(symbol);
  const referenceBook = useSidPaxOrderbook(venueBook ? undefined : symbol);
  const sourceBook = venueBook ?? referenceBook;

  useEffect(() => {
    if (!sourceBook) { pendingBookRef.current = null; setBook(null); return; }
    const asks = sourceBook.asks.map((l) => ({ price: l.price, quantity: l.size }));
    const bids = sourceBook.bids.map((l) => ({ price: l.price, quantity: l.size }));
    scheduleBookUpdate(buildBook(asks, bids));
  }, [sourceBook, scheduleBookUpdate]);

  const maxTotal = useMemo(() => {
    if (!book) return 1;
    const maxBid = book.bids[book.bids.length - 1]?.total ?? 0;
    const maxAsk = book.asks[book.asks.length - 1]?.total ?? 0;
    return Math.max(maxBid, maxAsk, 1);
  }, [book]);

  const formatPrice = useCallback((price: number) => {
    if (price >= 10000) return price.toFixed(2);
    if (price >= 100) return price.toFixed(2);
    if (price >= 1) return price.toFixed(4);
    return price.toFixed(6);
  }, []);

  const formatSize = useCallback((size: number) => {
    if (size >= 1000) return size.toFixed(1);
    if (size >= 1) return size.toFixed(3);
    return size.toFixed(4);
  }, []);

  if (!book) {
    return (
      <div
        className="flex flex-col h-full w-full items-center justify-center overflow-hidden"
        style={{
          height,
          background: "var(--bg-subtle)",
          border: "1px solid var(--border-soft)",
          borderRadius: 12,
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
        }}
      >
        <div className="animate-pulse">Connecting…</div>
      </div>
    );
  }

  // Dynamically derive the number of levels rendered per side from the
  // available height, using the measured row height + chrome constants.
  // Clamp between MIN_LEVELS_PER_SIDE and MAX_LEVELS so we never clip
  // visibly or over-request data the backend didn't supply.
  const halfLevels = Math.max(
    MIN_LEVELS_PER_SIDE,
    Math.min(MAX_LEVELS, Math.floor((height - CHROME_H_PX) / 2 / ROW_H_PX)),
  );
  const displayAsks = book.asks.slice(0, halfLevels).reverse();
  const displayBids = book.bids.slice(0, halfLevels);

  const bestBid = book.bids[0];

  const renderRow = (level: OrderbookLevel, kind: "ask" | "bid", i: number) => {
    const color = kind === "ask" ? "var(--danger)" : "var(--success)";
    const bg = kind === "ask" ? "rgba(255, 90, 101, 0.08)" : "rgba(5, 193, 104, 0.08)";
    const pct = (level.total / maxTotal) * 100;
    return (
      <div
        key={`${kind}-${i}`}
        onClick={() => onPriceClick?.(level.price)}
        className="hover:bg-white/[0.04] transition-colors"
        style={{
          display: "grid",
          gridTemplateColumns: compact ? "1fr 1fr" : "1fr 1fr 1fr",
          padding: compact ? "5px 8px" : "5px 12px",
          fontFamily: "var(--font-mono)",
          fontSize: 12,
          position: "relative",
          cursor: "pointer",
        }}
      >
        <div
          aria-hidden
          style={{
            position: "absolute",
            top: 0,
            bottom: 0,
            right: 0,
            width: `${pct}%`,
            background: bg,
            transition: "width 600ms cubic-bezier(0.4, 0, 0.2, 1)",
            pointerEvents: "none",
          }}
        />
        <span style={{ color, position: "relative", fontVariantNumeric: "tabular-nums" }}>
          {formatPrice(level.price)}
        </span>
        {!compact && (
          <span
            style={{
              textAlign: "right",
              color: "var(--fg)",
              position: "relative",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {formatSize(level.size)}
          </span>
        )}
        <span
          style={{
            textAlign: "right",
            color: "var(--fg-muted)",
            position: "relative",
            fontVariantNumeric: "tabular-nums",
          }}
        >
          {formatSize(level.total)}
        </span>
      </div>
    );
  };

  return (
    <div
      className="trading-panel flex flex-col h-full w-full overflow-hidden"
      style={{
        height,
        background: "var(--bg-subtle)",
        border: "1px solid var(--border-soft)",
        borderRadius: 12,
      }}
    >
      {/* Column header — mono caps */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: compact ? "1fr 1fr" : "1fr 1fr 1fr",
          padding: compact ? "10px 8px" : "10px 12px",
          borderBottom: "1px solid var(--border-soft)",
          fontFamily: "var(--font-mono)",
          fontSize: 9,
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
          flexShrink: 0,
        }}
      >
        <span>Price</span>
        {!compact && <span style={{ textAlign: "right" }}>Size</span>}
        <span style={{ textAlign: "right" }}>Total</span>
      </div>

      {/* Asks — lowest ask at bottom, abutting mid band */}
      <div className="flex-1 overflow-hidden flex flex-col justify-end">
        {displayAsks.map((level, i) => renderRow(level, "ask", i))}
      </div>

      {/* Mid / spread band — blue gradient */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "8px 12px",
          background: "linear-gradient(90deg, rgba(0, 76, 237, 0.12), transparent)",
          borderTop: "1px solid var(--border-soft)",
          borderBottom: "1px solid var(--border-soft)",
          flexShrink: 0,
        }}
      >
        <div>
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 18,
              fontWeight: 500,
              letterSpacing: "-0.01em",
              color: "var(--color-blue-200)",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {bestBid ? formatPrice(bestBid.price) : "—"}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 9,
              color: "var(--fg-subtle)",
              letterSpacing: "0.1em",
              textTransform: "uppercase",
              marginTop: 1,
            }}
          >
            mid · spread {book.spread.toFixed(2)} ({book.spreadPct.toFixed(3)}%)
          </div>
        </div>
      </div>

      {/* Bids — highest bid at top, abutting mid band */}
      <div className="flex-1 overflow-hidden flex flex-col">
        {displayBids.map((level, i) => renderRow(level, "bid", i))}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════════
   Recent Trades sub-panel (for DepthPanel "Trades" tab)
   ═══════════════════════════════════════════════════════════════════════ */
function RecentTradesPanel({ symbol, height }: { symbol: string; height: number }) {
  const { tickers } = useRealtimePrices();
  const isDedicated = isVenueSymbol(symbol);
  const venueFills = useVenueFills(isDedicated ? symbol : undefined);
  const ticker = tickers[symbol];
  const tickerTrades = (ticker as any)?.recent_trades as
    | { time: string; price: number; qty: number; side: string }[]
    | undefined;

  const fmtPrice = (price: number) => {
    if (price >= 10000) return price.toFixed(2);
    if (price >= 100) return price.toFixed(2);
    if (price >= 1) return price.toFixed(4);
    return price.toFixed(6);
  };

  const fmtQty = (val: number) => {
    if (val >= 1_000) return `${(val / 1_000).toFixed(2)}K`;
    return val.toFixed(val < 1 ? 4 : 2);
  };

  return (
    <div
      className="trading-panel flex flex-col h-full overflow-hidden"
      style={{
        height,
        background: "var(--bg-subtle)",
        border: "1px solid var(--border-soft)",
        borderRadius: 12,
      }}
    >
      {/* Column header */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: isDedicated ? "1fr 1fr 1fr 1fr" : "1fr 1fr 1fr",
          padding: "10px 12px",
          borderBottom: "1px solid var(--border-soft)",
          fontFamily: "var(--font-mono)",
          fontSize: 9,
          letterSpacing: "0.14em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
          flexShrink: 0,
        }}
      >
        <span>Price</span>
        {isDedicated && <span style={{ textAlign: "right" }}>Notional</span>}
        <span style={{ textAlign: "right" }}>Ctrs</span>
        <span style={{ textAlign: "right" }}>Time</span>
      </div>

      {/* Trades tape */}
      <div className="flex-1 overflow-y-auto scrollbar-custom">
        {isDedicated ? (
          venueFills && venueFills.length > 0 ? (
            venueFills.map((t, i) => {
              const color = t.side === "BUY" ? "var(--success)" : "var(--danger)";
              const timeStr = new Date(t.trade_time).toISOString().slice(11, 19);
              return (
                <div
                  key={t.id ?? `${t.trade_time}-${i}`}
                  style={{
                    display: "grid",
                    gridTemplateColumns: "1fr 1fr 1fr 1fr",
                    padding: "5px 12px",
                    fontFamily: "var(--font-mono)",
                    fontSize: 11,
                    position: "relative",
                  }}
                >
                  {/* Price + direction arrow */}
                  <span style={{ color, display: "inline-flex", alignItems: "center", gap: 4, fontVariantNumeric: "tabular-nums" }}>
                    <span style={{ fontSize: 9 }}>{t.side === "BUY" ? "▲" : "▼"}</span>
                    {fmtPrice(t.price)}
                    {t.liquidation && (
                      <span style={{ fontSize: 8, padding: "1px 4px", borderRadius: 4, background: "rgba(255,158,44,0.15)", color: "var(--brand-orange)", letterSpacing: "0.08em" }}>
                        LIQ
                      </span>
                    )}
                  </span>
                  {/* Notional */}
                  <span style={{ textAlign: "right", color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
                    {t.notional > 0
                      ? (t.notional >= 1_000 ? `$${(t.notional / 1_000).toFixed(1)}K` : `$${t.notional.toFixed(2)}`)
                      : "—"}
                  </span>
                  {/* Contracts */}
                  <span style={{ textAlign: "right", color: "var(--fg-muted)", fontVariantNumeric: "tabular-nums" }}>
                    {fmtQty(t.contracts)}
                  </span>
                  <span style={{ textAlign: "right", color: "var(--fg-subtle)" }}>{timeStr}</span>
                </div>
              );
            })
          ) : (
            <div className="flex flex-col items-center justify-center h-full min-h-[120px]" style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.14em", textTransform: "uppercase", color: "var(--fg-subtle)" }}>
              <span className="animate-pulse">Loading trades…</span>
            </div>
          )
        ) : tickerTrades && tickerTrades.length > 0 ? (
          tickerTrades.map((t, i) => {
            const color = t.side === "buy" ? "var(--success)" : "var(--danger)";
            return (
              <div
                key={i}
                style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", padding: "6px 12px", fontFamily: "var(--font-mono)", fontSize: 12 }}
              >
                <span style={{ color, display: "inline-flex", alignItems: "center", gap: 4, fontVariantNumeric: "tabular-nums" }}>
                  <span style={{ fontSize: 9 }}>{t.side === "buy" ? "▲" : "▼"}</span>
                  {fmtPrice(t.price)}
                </span>
                <span style={{ textAlign: "right", color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
                  {t.qty.toFixed(4)}
                </span>
                <span style={{ textAlign: "right", color: "var(--fg-subtle)" }}>{t.time}</span>
              </div>
            );
          })
        ) : (
          <div className="flex flex-col items-center justify-center h-full min-h-[120px]" style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.14em", textTransform: "uppercase", color: "var(--fg-subtle)" }}>
            <span className="animate-pulse">Loading trades…</span>
          </div>
        )}
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════════════
   DepthPanel — Tabbed wrapper: Order Book | Trades
   Matches trading-ui DepthPanel structure with accent-underline tabs
   ═══════════════════════════════════════════════════════════════════════ */
type DepthPanelTab = "orderbook" | "trades";

interface DepthPanelProps {
  symbol: string;
  height: number;
  onPriceClick?: (price: number) => void;
}

export function DepthPanel({ symbol, height, onPriceClick }: DepthPanelProps) {
  const [activeTab, setActiveTab] = useState<DepthPanelTab>("orderbook");

  const tabs: { value: DepthPanelTab; label: string }[] = [
    { value: "orderbook", label: "Order Book" },
    { value: "trades", label: "Trades" },
  ];

  const tabBarHeight = 37;
  const contentHeight = height - tabBarHeight;

  return (
    <div className="flex flex-col h-full overflow-hidden" style={{ height }}>
      {/* Tab Header — paxeer mono caps with accent underline.
          No outer chrome on this wrapper; inner panels own their own border +
          rounded corners so they also look right when rendered standalone. */}
      <div className="flex shrink-0" style={{ paddingBottom: 6 }}>
        {tabs.map((tab) => {
          const active = activeTab === tab.value;
          return (
            <button
              key={tab.value}
              onClick={() => setActiveTab(tab.value)}
              style={{
                position: "relative",
                padding: "10px 16px",
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                letterSpacing: "0.14em",
                textTransform: "uppercase",
                color: active ? "var(--fg)" : "var(--fg-subtle)",
                background: "transparent",
                border: "none",
                cursor: "pointer",
                transition: "color 150ms ease",
              }}
            >
              {tab.label}
              {active && (
                <span
                  aria-hidden
                  style={{
                    position: "absolute",
                    bottom: 0,
                    left: 12,
                    right: 12,
                    height: 2,
                    background: "var(--accent-solid)",
                    borderRadius: 999,
                  }}
                />
              )}
            </button>
          );
        })}
      </div>

      <div className="flex-1 overflow-hidden">
        {activeTab === "orderbook" && (
          <Orderbook symbol={symbol} height={contentHeight} onPriceClick={onPriceClick} />
        )}
        {activeTab === "trades" && (
          <RecentTradesPanel symbol={symbol} height={contentHeight} />
        )}
      </div>
    </div>
  );
}
