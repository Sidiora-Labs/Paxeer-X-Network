"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useSpotOrderbook } from "@/hooks/spot/useSpotMarketStream";
import { useSpotVenueBook } from "@/core/net/venueStream";

export interface SpotOrderbookLevel {
  price: number;
  size: number;
  total: number;
}

interface SpotOrderbookData {
  bids: SpotOrderbookLevel[];
  asks: SpotOrderbookLevel[];
  spread: number;
  spreadPct: number;
}

interface SpotOrderbookProps {
  symbol: string;
  height?: number;
  onPriceClick?: (price: number) => void;
  /** Compact density mode — used by mobile trade view (smaller fonts, fewer levels). */
  compact?: boolean;
}

const MAX_LEVELS = 33;
const ROW_H_PX = 23;
const ROW_H_PX_COMPACT = 18;
const CHROME_H_PX = 95;
const MIN_LEVELS_PER_SIDE = 4;

function buildBook(
  rawAsks: { price: number; size: number }[],
  rawBids: { price: number; size: number }[],
): SpotOrderbookData {
  const processLevels = (
    levels: { price: number; size: number }[],
    isBids: boolean,
  ): SpotOrderbookLevel[] => {
    if (!Array.isArray(levels)) return [];
    const sorted = [...levels].sort((a, b) =>
      isBids ? b.price - a.price : a.price - b.price,
    );
    let total = 0;
    return sorted.slice(0, MAX_LEVELS).map((level) => {
      total += level.size;
      return { price: level.price, size: level.size, total };
    });
  };

  const bids = processLevels(rawBids, true);
  const asks = processLevels(rawAsks, false);

  const bestBid = bids[0]?.price ?? 0;
  const bestAsk = asks[0]?.price ?? 0;
  const spread = bestAsk > 0 && bestBid > 0 ? bestAsk - bestBid : 0;
  const mid = (bestAsk + bestBid) / 2;
  const spreadPct = mid > 0 ? (spread / mid) * 100 : 0;

  return { bids, asks, spread, spreadPct };
}

export function SpotOrderbook({
  symbol,
  height = 580,
  onPriceClick,
  compact = false,
}: SpotOrderbookProps) {
  const [book, setBook] = useState<SpotOrderbookData | null>(null);
  const pendingBookRef = useRef<SpotOrderbookData | null>(null);
  const rafIdRef = useRef<number | null>(null);

  const scheduleBookUpdate = useCallback((next: SpotOrderbookData) => {
    pendingBookRef.current = next;
    if (rafIdRef.current !== null) return;
    if (typeof window === "undefined" || typeof window.requestAnimationFrame !== "function") {
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

  // The spot venue book of the gateway; the Chainflow reference book while the
  // gateway socket is not open or the spot venue does not list the symbol.
  const venueBook = useSpotVenueBook(symbol);
  const referenceBook = useSpotOrderbook(venueBook ? undefined : symbol);
  const spotBook = venueBook ?? referenceBook;

  useEffect(() => {
    if (!spotBook) {
      pendingBookRef.current = null;
      if (rafIdRef.current !== null && typeof window !== "undefined") {
        window.cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      setBook(null);
      return;
    }
    const asks = spotBook.asks.map((l) => ({ price: l.price, size: l.size }));
    const bids = spotBook.bids.map((l) => ({ price: l.price, size: l.size }));
    scheduleBookUpdate(buildBook(asks, bids));
  }, [spotBook, scheduleBookUpdate]);

  const maxTotal = useMemo(() => {
    if (!book) return 1;
    const maxBid = book.bids[book.bids.length - 1]?.total ?? 0;
    const maxAsk = book.asks[book.asks.length - 1]?.total ?? 0;
    return Math.max(maxBid, maxAsk, 1);
  }, [book]);

  const formatPrice = useCallback((price: number) => {
    if (price >= 10000) return price.toFixed(2);
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
        <div className="animate-pulse">Connecting&hellip;</div>
      </div>
    );
  }

  const rowH = compact ? ROW_H_PX_COMPACT : ROW_H_PX;
  const chromeH = compact ? Math.round(CHROME_H_PX * 0.7) : CHROME_H_PX;
  const halfLevels = Math.max(
    MIN_LEVELS_PER_SIDE,
    Math.min(MAX_LEVELS, Math.floor((height - chromeH) / 2 / rowH)),
  );
  const displayAsks = book.asks.slice(0, halfLevels).reverse();
  const displayBids = book.bids.slice(0, halfLevels);

  const bestBid = book.bids[0];

  const renderRow = (level: SpotOrderbookLevel, kind: "ask" | "bid", i: number) => {
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
          gridTemplateColumns: "1fr 1fr 1fr",
          padding: compact ? "2.5px 10px" : "5px 12px",
          fontFamily: "var(--font-mono)",
          fontSize: compact ? 10 : 12,
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
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr 1fr",
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
        <span style={{ textAlign: "right" }}>Size</span>
        <span style={{ textAlign: "right" }}>Total</span>
      </div>

      <div className="flex-1 overflow-hidden flex flex-col justify-end">
        {displayAsks.map((level, i) => renderRow(level, "ask", i))}
      </div>

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
            {bestBid ? `$${formatPrice(bestBid.price)}` : "\u2014"}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 9,
              color: "var(--fg-subtle)",
              letterSpacing: "0.14em",
              textTransform: "uppercase",
            }}
          >
            Mid Price
          </div>
        </div>
        <div style={{ textAlign: "right" }}>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-muted)",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {book.spread > 0 ? formatPrice(book.spread) : "\u2014"}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 9,
              color: "var(--fg-subtle)",
              letterSpacing: "0.14em",
              textTransform: "uppercase",
            }}
          >
            Spread ({book.spreadPct.toFixed(2)}%)
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-hidden">
        {displayBids.map((level, i) => renderRow(level, "bid", i))}
      </div>
    </div>
  );
}
