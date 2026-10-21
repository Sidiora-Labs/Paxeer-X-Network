import { useState, useMemo } from "react";
import dynamic from "next/dynamic";
import { Book, BookX } from "lucide-react";
import { type PerpsMarket } from "@/core/perps/config";
import { useSidPaxOrderbook } from "@/hooks/perps/useSidPaxMarketStream";
import { useVenueBook } from "@/core/net/venueStream";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { MobileBalancePanel } from "./MobileBalancePanel";
import { MobileSecondaryTabs } from "./MobileSecondaryTabs";
import { MobilePanelToolbar } from "./MobilePanelToolbar";
import { MobileSecondaryTabContent } from "./MobileSecondaryTabContent";
import { OrderFormSkeleton } from "@/ui/atoms/skeleton-panel";

const PerpsOrderForm = dynamic(
  () => import("@/widgets/trading/PerpsOrderForm").then((m) => ({ default: m.PerpsOrderForm })),
  { ssr: false, loading: () => <OrderFormSkeleton /> }
);

interface MobileTradeViewProps {
  market: PerpsMarket;
}

export function MobileTradeView({ market }: MobileTradeViewProps) {
  const [secondaryTab, setSecondaryTab] = useState("Balances");
  const [sortValue, setSortValue] = useState("Market");
  const [showBook, setShowBook] = useState(true);

  return (
    <div className="flex flex-col md:hidden" style={{ background: "var(--bg)" }}>
      {/* Balance summary row */}
      <MobileBalancePanel />

      {/* Hide/show orderbook toggle */}
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
      <div className="flex items-stretch" style={{ minHeight: 420 }}>
        {/* Left: Order Entry Form */}
        <div className="min-w-0" style={{ flex: showBook ? "0 0 72%" : "1 1 100%" }}>
          <PerpsOrderForm market={market} compact />
        </div>
        {/* Right: Compact Orderbook */}
        {showBook && (
          <div
            className="shrink-0 overflow-hidden"
            style={{
              flex: "0 0 28%",
              borderLeft: "1px solid var(--border-soft)",
            }}
          >
            <CompactOrderbook symbol={market.symbol} />
          </div>
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

/* ── Compact orderbook: asks + mid-price banner + bids + tick-size dropdown ── */

interface MiniLevel {
  price: number;
  size: number;
}

function fmtPrice(price: number): string {
  if (price >= 10000) return price.toFixed(2);
  if (price >= 1) return price.toFixed(4);
  return price.toFixed(6);
}

function fmtSize(size: number): string {
  if (size >= 1000) return size.toFixed(1);
  if (size >= 1) return size.toFixed(3);
  return size.toFixed(4);
}

function CompactOrderbook({ symbol }: { symbol: string }) {
  const venueBook = useVenueBook(symbol);
  const referenceBook = useSidPaxOrderbook(venueBook ? undefined : symbol);
  const { prices } = useRealtimePrices();
  const [tickSize, setTickSize] = useState("0.1");
  const book = useMemo<{ asks: MiniLevel[]; bids: MiniLevel[] } | null>(() => {
    const source = venueBook ?? referenceBook;
    if (!source) return null;
    return { asks: source.asks.slice(0, 8), bids: source.bids.slice(0, 8) };
  }, [venueBook, referenceBook]);

  const midPrice = useMemo(() => {
    if (!book) return prices[symbol] ?? 0;
    const bestBid = book.bids[0]?.price ?? 0;
    const bestAsk = book.asks[0]?.price ?? 0;
    if (bestBid > 0 && bestAsk > 0) return (bestBid + bestAsk) / 2;
    return prices[symbol] ?? 0;
  }, [book, prices, symbol]);

  const asks = book?.asks ?? [];
  const bids = book?.bids ?? [];

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div
        className="flex justify-between px-2 py-1.5"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 8,
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "var(--fg-subtle)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span>Price</span>
        <span>Size</span>
      </div>

      {/* Asks (reversed so lowest ask is at bottom) */}
      <div className="flex flex-col justify-end flex-1">
        {[...asks].reverse().map((level, i) => (
          <div
            key={`ask-${i}`}
            className="flex justify-between px-2 py-[3px]"
            style={{ fontFamily: "var(--font-mono)", fontSize: 10 }}
          >
            <span className="tabular-nums" style={{ color: "var(--danger)" }}>
              {fmtPrice(level.price)}
            </span>
            <span className="tabular-nums" style={{ color: "var(--fg-muted)" }}>
              {fmtSize(level.size)}
            </span>
          </div>
        ))}
      </div>

      {/* Mid-market price banner */}
      <div
        className="flex items-center justify-center py-1.5"
        style={{
          background: "linear-gradient(90deg, rgba(0, 76, 237, 0.10), transparent)",
          borderTop: "1px solid var(--border-soft)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 13,
            fontWeight: 500,
            color: "var(--accent-solid)",
          }}
        >
          {midPrice > 0 ? `$${fmtPrice(midPrice)}` : "—"}
        </span>
      </div>

      {/* Bids */}
      <div className="flex flex-col flex-1">
        {bids.map((level, i) => (
          <div
            key={`bid-${i}`}
            className="flex justify-between px-2 py-[3px]"
            style={{ fontFamily: "var(--font-mono)", fontSize: 10 }}
          >
            <span className="tabular-nums" style={{ color: "var(--success)" }}>
              {fmtPrice(level.price)}
            </span>
            <span className="tabular-nums" style={{ color: "var(--fg-muted)" }}>
              {fmtSize(level.size)}
            </span>
          </div>
        ))}
      </div>

      {/* Tick-size dropdown at bottom */}
      <div
        className="px-2 py-1.5"
        style={{ borderTop: "1px solid var(--border-soft)" }}
      >
        <select
          value={tickSize}
          onChange={(e) => setTickSize(e.target.value)}
          className="rounded px-1 py-0.5 text-[9px] outline-none w-full"
          style={{
            fontFamily: "var(--font-mono)",
            color: "var(--fg)",
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
          }}
        >
          <option value="0.01">0.01</option>
          <option value="0.1">0.1</option>
          <option value="1">1</option>
          <option value="10">10</option>
        </select>
      </div>
    </div>
  );
}
