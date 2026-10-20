import { useState, useMemo } from "react";
import { X, Search, Star, StarOff } from "lucide-react";
import {
  SPOT_MARKETS,
  spotCoinLogoUrl,
  type SpotMarket,
} from "@/core/spot/config";
import { useSpotStats } from "@/hooks/spot/useSpotMarketStream";
import { useFavoriteMarkets } from "@/hooks/ui/useFavoriteMarkets";
import { MobileHeader } from "./MobileHeader";
import { MobileSpotTickerBar } from "./MobileSpotTickerBar";
import { MobileSpotMarketView } from "./MobileSpotMarketView";
import { MobileSpotTradeView } from "./MobileSpotTradeView";
import { MobileSpotAccountView } from "./MobileSpotAccountView";
import { MobileBottomNav, type MobileGlobalTab } from "./MobileBottomNav";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotAppLayout — SPOT mirror of MobileAppLayout.

   Same shell topology as the perps mobile layout:
     · Top                — global MobileHeader (wallet, nav, settings)
     · Below header       — MobileSpotTickerBar (markets / trade tabs)
     · Body               — MobileSpotMarketView | MobileSpotTradeView
                          | MobileSpotAccountView
     · Bottom             — MobileBottomNav (Markets / Trade / Account)
     · Overlay            — SpotMarketSelectorOverlay

   The perps and SPOT shells use the same MobileHeader and MobileBottomNav
   so the global navigation feels identical between the two trading
   modes. Only the views and ticker bar swap out.
   ══════════════════════════════════════════════════════════════════════ */

interface MobileSpotAppLayoutProps {
  market: SpotMarket;
  onSelectMarket: (symbol: string) => void;
}

export function MobileSpotAppLayout({
  market,
  onSelectMarket,
}: MobileSpotAppLayoutProps) {
  const [globalTab, setGlobalTab] = useState<MobileGlobalTab>("trade");
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
        <MobileSpotTickerBar
          market={market}
          onOpenSelector={() => setMarketSelectorOpen(true)}
        />
      )}

      {/* Main scrollable content area */}
      <div className="flex-1 min-h-0 overflow-y-auto" id="app-scroll">
        {globalTab === "markets" && <MobileSpotMarketView market={market} />}
        {globalTab === "trade" && <MobileSpotTradeView market={market} />}
        {globalTab === "account" && <MobileSpotAccountView />}
      </div>

      {/* Fixed Bottom Navigation — Markets | Trade | Account */}
      <MobileBottomNav active={globalTab} onChange={setGlobalTab} />

      {/* Market Selector Overlay */}
      {marketSelectorOpen && (
        <SpotMarketSelectorOverlay
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

/* ══════════════════════════════════════════════════════════════════════
   SpotMarketSelectorOverlay — fullscreen market picker.

   Mirrors the perps MarketSelectorOverlay. Adds favorites toggle so
   touch users can shortlist their preferred SPOT pairs. Stats are pulled
   per row via the SPOT stats stream singleton.
   ══════════════════════════════════════════════════════════════════════ */

function fmtPrice(price: number): string {
  if (price >= 10000) return price.toFixed(2);
  if (price >= 1) return price.toFixed(4);
  return price.toFixed(6);
}

function SpotMarketSelectorOverlay({
  currentSymbol,
  onSelect,
  onClose,
}: {
  currentSymbol: string;
  onSelect: (symbol: string) => void;
  onClose: () => void;
}) {
  const [search, setSearch] = useState("");
  const { favorites, toggle: toggleFavorite } = useFavoriteMarkets();

  const filtered = useMemo(() => {
    if (!search) return SPOT_MARKETS;
    const q = search.toLowerCase();
    return SPOT_MARKETS.filter(
      (m) =>
        m.symbol.toLowerCase().includes(q) ||
        m.name.toLowerCase().includes(q),
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
          Spot Markets
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
            placeholder="Search spot markets…"
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
        {filtered.map((m) => (
          <SpotMarketRow
            key={m.symbol}
            market={m}
            isSelected={currentSymbol === m.symbol}
            isFavorite={favorites.has(m.symbol.toUpperCase())}
            onSelect={() => onSelect(m.symbol)}
            onToggleFavorite={() => toggleFavorite(m.symbol)}
          />
        ))}
        {filtered.length === 0 && (
          <div
            className="flex items-center justify-center py-12"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--fg-subtle)",
            }}
          >
            No spot markets found
          </div>
        )}
      </div>
    </div>
  );
}

function SpotMarketRow({
  market,
  isSelected,
  isFavorite,
  onSelect,
  onToggleFavorite,
}: {
  market: SpotMarket;
  isSelected: boolean;
  isFavorite: boolean;
  onSelect: () => void;
  onToggleFavorite: () => void;
}) {
  const stats = useSpotStats(market.symbol);
  const price = stats?.last_price ?? 0;
  const changeFrac = stats?.change_24h ?? 0;
  const changePct = changeFrac * 100;
  const positive = changePct >= 0;

  return (
    <div
      className="w-full grid items-center gap-2 px-4 py-2.5 transition-colors"
      style={{
        gridTemplateColumns: "20px 1fr auto auto",
        borderBottom: "1px solid var(--border-soft)",
        background: isSelected ? "rgba(0, 76, 237, 0.10)" : undefined,
        borderLeft: isSelected
          ? "2px solid var(--accent-solid)"
          : "2px solid transparent",
      }}
    >
      <button
        type="button"
        onClick={onToggleFavorite}
        className="transition-colors"
        style={{
          color: isFavorite ? "#facc15" : "var(--fg-subtle)",
        }}
        aria-label={isFavorite ? "Unfavorite" : "Favorite"}
      >
        {isFavorite ? (
          <Star className="w-3.5 h-3.5" fill="currentColor" />
        ) : (
          <StarOff className="w-3.5 h-3.5" />
        )}
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="flex items-center gap-2.5 min-w-0 text-left"
      >
        <img
          src={spotCoinLogoUrl(market.symbol)}
          alt={market.symbol}
          className="w-7 h-7 rounded-full object-cover shrink-0"
        />
        <div className="flex flex-col items-start">
          <span
            className="text-sm font-semibold"
            style={{ color: "var(--fg)" }}
          >
            {market.pair}
          </span>
          <span
            className="text-[9px] font-mono"
            style={{ color: "var(--fg-subtle)" }}
          >
            {market.name}
          </span>
        </div>
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="text-xs font-mono text-right tabular-nums w-20"
        style={{ color: "var(--fg)" }}
      >
        {price > 0 ? `$${fmtPrice(price)}` : "—"}
      </button>
      <button
        type="button"
        onClick={onSelect}
        className="text-xs font-mono text-right tabular-nums w-14"
        style={{
          color:
            changePct === 0
              ? "var(--fg-subtle)"
              : positive
                ? "var(--success)"
                : "var(--danger)",
        }}
      >
        {changePct !== 0
          ? `${positive ? "+" : ""}${changePct.toFixed(2)}%`
          : "—"}
      </button>
    </div>
  );
}
