import React, { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/router";
import { ErrorBoundary } from "@/app/shell/ErrorBoundary";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { useFavoriteMarkets } from "@/hooks/ui/useFavoriteMarkets";
import {
  MarketsHeaderTabs,
  MarketsOverviewStats,
  MarketsHighlights,
  MarketsFilterBar,
  MarketsTable,
  useMarketsRows,
  filterAndSort,
  type MarketsCategoryFilter,
  type MarketsTab,
  type MarketsMode,
  type SortDir,
  type SortKey,
} from "@/widgets/markets";
import { SPOT_MARKETS, spotCoinLogoUrl } from "@/core/spot/config";
import { useSpotStats } from "@/hooks/spot/useSpotMarketStream";
import { FUNDING_LABEL_1H, formatFundingRatePct } from "@/core/perps/fundingRate";

/* ══════════════════════════════════════════════════════════════════════
   /markets page — live, widget-composed layout.

   Data flows from a single `/ws/markets` feed via `useMarketsRows`
   (which internally subscribes through `useMarketsStream`). The page
   owns only UI state (active tab, category filter, search, sort,
   pagination) and delegates every render decision to a widget.
   ══════════════════════════════════════════════════════════════════════ */

const PAGE_SIZE = 25;

export default function MarketsPage() {
  const router = useRouter();
  const isMobile = useIsMobile();
  const { rows, hasAnyLive } = useMarketsRows();
  const { favorites, toggle: toggleFavorite } = useFavoriteMarkets();

  // ── Page-local state ──
  const [mode, setMode] = useState<MarketsMode>("perps");
  const [activeTab, setActiveTab] = useState<MarketsTab>("markets");
  const [category, setCategory] = useState<MarketsCategoryFilter>("all");
  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("volume24h");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const [page, setPage] = useState(0);

  // ── Deep-link category via `?tab=favorites|rwa|…` so the mobile drawer
  //    can hand users straight into a filtered view. ──
  useEffect(() => {
    const q = router.query.tab;
    if (typeof q !== "string") return;
    const allowed: MarketsCategoryFilter[] = ["favorites", "all", "crypto", "rwa", "new"];
    if ((allowed as string[]).includes(q)) {
      setCategory(q as MarketsCategoryFilter);
      setPage(0);
    }
  }, [router.query.tab]);

  const filteredRows = useMemo(
    () => filterAndSort(rows, { category, search, favorites, sortKey, sortDir }),
    [rows, category, search, favorites, sortKey, sortDir],
  );

  // Keep the page index in-bounds when filters shrink the dataset.
  useEffect(() => {
    const lastPage = Math.max(0, Math.ceil(filteredRows.length / PAGE_SIZE) - 1);
    if (page > lastPage) setPage(lastPage);
  }, [filteredRows.length, page]);

  const pageRows = useMemo(
    () => filteredRows.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE),
    [filteredRows, page],
  );
  const totalPages = Math.max(1, Math.ceil(filteredRows.length / PAGE_SIZE));

  const handleToggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "symbol" ? "asc" : "desc");
    }
  };

  return (
    <div className="flex flex-col min-h-full bg-background text-foreground">
      <main className="flex-1 min-h-0 min-w-0 w-full px-4 md:px-8 xl:px-12 py-4 md:py-6 space-y-4 md:space-y-6">
        <MarketsHeaderTabs
          activeTab={activeTab}
          onChangeTab={(t) => { setActiveTab(t); setPage(0); }}
          mode={mode}
          onChangeMode={(m) => { setMode(m); setActiveTab("markets"); setPage(0); }}
        />

        {mode === "spot" ? (
          <ErrorBoundary fallbackLabel="Spot Markets">
            <SpotMarketsPanel />
          </ErrorBoundary>
        ) : activeTab === "markets" ? (
          <ErrorBoundary fallbackLabel="Markets overview">
            {/* ── Stats + highlights band ── */}
            {isMobile ? (
              <div className="space-y-3">
                <MarketsOverviewStats rows={rows} layout="row" />
                <MarketsHighlights rows={rows} hasAnyLive={hasAnyLive} variant="tabs" />
              </div>
            ) : (
              <div className="grid grid-cols-1 lg:grid-cols-[280px_minmax(0,1fr)] gap-4">
                <MarketsOverviewStats rows={rows} layout="column" />
                <MarketsHighlights rows={rows} hasAnyLive={hasAnyLive} variant="columns" />
              </div>
            )}

            {/* ── Filters + table ── */}
            <div className="space-y-3 md:space-y-4">
              <MarketsFilterBar
                category={category}
                onChangeCategory={(c) => { setCategory(c); setPage(0); }}
                search={search}
                onChangeSearch={(v) => { setSearch(v); setPage(0); }}
                favoriteCount={favorites.size}
              />

              <MarketsTable
                rows={pageRows}
                loading={!hasAnyLive}
                favorites={favorites}
                onToggleFavorite={toggleFavorite}
                sortKey={sortKey}
                sortDir={sortDir}
                onToggleSort={handleToggleSort}
                emptyMessage={
                  category === "favorites" && favorites.size === 0
                    ? "No favorites yet"
                    : search
                      ? `No markets match "${search}"`
                      : undefined
                }
              />

              {filteredRows.length > PAGE_SIZE ? (
                <PagerBar
                  page={page}
                  totalPages={totalPages}
                  pageSize={PAGE_SIZE}
                  total={filteredRows.length}
                  onPrev={() => setPage((p) => Math.max(0, p - 1))}
                  onNext={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                />
              ) : null}
            </div>
          </ErrorBoundary>
        ) : (
          <FundingTabPanel rows={rows} hasAnyLive={hasAnyLive} />
        )}
      </main>
    </div>
  );
}

// ── Pagination bar ────────────────────────────────────────────────────

function PagerBar({
  page,
  totalPages,
  pageSize,
  total,
  onPrev,
  onNext,
}: {
  page: number;
  totalPages: number;
  pageSize: number;
  total: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  const first = page * pageSize + 1;
  const last = Math.min((page + 1) * pageSize, total);
  return (
    <div className="flex items-center justify-between gap-3 px-1 text-xs text-muted-foreground">
      <span className="font-mono tabular-nums">
        {first}–{last} of {total}
      </span>
      <div className="flex items-center gap-1">
        <PagerButton disabled={page === 0} onClick={onPrev} label="Prev" />
        <span className="font-mono tabular-nums text-foreground px-2">
          {page + 1} / {totalPages}
        </span>
        <PagerButton
          disabled={page >= totalPages - 1}
          onClick={onNext}
          label="Next"
        />
      </div>
    </div>
  );
}

function PagerButton({
  disabled,
  onClick,
  label,
}: {
  disabled: boolean;
  onClick: () => void;
  label: string;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="h-7 px-3 rounded-[var(--radius-pill)] bg-surface-1 border border-white/[0.06] text-xs text-foreground transition-colors enabled:hover:bg-surface-2 disabled:opacity-40 disabled:cursor-not-allowed"
    >
      {label}
    </button>
  );
}

// ── Spot markets panel ───────────────────────────────────────────────

function SpotMarketsPanel() {
  const router = useRouter();

  return (
    <div className="space-y-4">
      <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
        <div
          className="hidden md:grid items-center px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground border-b border-white/[0.04]"
          style={{ gridTemplateColumns: "minmax(180px,2fr) 1fr 1fr 1fr 1fr" }}
        >
          <span>Market</span>
          <span className="text-right">Last</span>
          <span className="text-right">24h Change</span>
          <span className="text-right">24h High</span>
          <span className="text-right">24h Volume</span>
        </div>
        <div className="divide-y divide-white/[0.04]">
          {SPOT_MARKETS.map((market) => (
            <SpotMarketRow key={market.symbol} market={market} onClick={() => router.push(`/spot?market=${market.symbol}`)} />
          ))}
        </div>
      </div>
    </div>
  );
}

function SpotMarketRow({ market, onClick }: { market: import("@/core/spot/config").SpotMarket; onClick: () => void }) {
  const stats = useSpotStats(market.symbol);
  const change = stats?.change_24h ?? 0;
  const positive = change >= 0;

  return (
    <button
      type="button"
      onClick={onClick}
      className="w-full grid items-center gap-3 px-4 py-3 text-left hover:bg-white/[0.03] transition-colors"
      style={{ gridTemplateColumns: "minmax(180px,2fr) 1fr 1fr 1fr 1fr" }}
    >
      <div className="flex items-center gap-2 min-w-0">
        <img
          src={spotCoinLogoUrl(market.symbol)}
          alt={market.symbol}
          className="w-6 h-6 rounded-full"
          onError={(e) => { (e.target as HTMLImageElement).style.display = "none"; }}
        />
        <span className="text-sm font-semibold text-foreground truncate">
          {market.pair}
        </span>
        <span className="text-[10px] text-muted-foreground truncate">{market.name}</span>
      </div>
      <span className="text-xs font-mono tabular-nums text-foreground text-right">
        {stats?.last_price
          ? `$${stats.last_price.toLocaleString("en-US", { maximumFractionDigits: 4 })}`
          : "—"}
      </span>
      <span className={`text-xs font-mono tabular-nums text-right ${positive ? "text-emerald-400" : "text-red-400"}`}>
        {change !== 0 ? `${positive ? "+" : ""}${(change * 100).toFixed(2)}%` : "—"}
      </span>
      <span className="text-xs font-mono tabular-nums text-muted-foreground text-right">
        {stats?.high_24h
          ? `$${stats.high_24h.toLocaleString("en-US", { maximumFractionDigits: 4 })}`
          : "—"}
      </span>
      <span className="text-xs font-mono tabular-nums text-muted-foreground text-right">
        {stats?.volume_24h && stats.volume_24h > 0
          ? `$${(stats.volume_24h / 1e6).toFixed(2)}M`
          : "—"}
      </span>
    </button>
  );
}

// ── Funding-only tab ──────────────────────────────────────────────────

function FundingTabPanel({
  rows,
  hasAnyLive,
}: {
  rows: readonly import("@/widgets/markets").MarketRow[];
  hasAnyLive: boolean;
}) {
  const router = useRouter();
  const sorted = useMemo(
    () => [...rows].filter((r) => r.hasLiveData).sort((a, b) => {
      const abs = (v: bigint | null) => (v === null ? -1n : v < 0n ? -v : v);
      const x = abs(a.funding8h);
      const y = abs(b.funding8h);
      return x === y ? 0 : x < y ? 1 : -1;
    }),
    [rows],
  );

  if (!hasAnyLive) {
    return (
      <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] p-6">
        <div className="h-5 w-32 rounded bg-white/[0.05] animate-pulse mb-3" />
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="h-8 rounded bg-white/[0.03] animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
      <div
        className="hidden md:grid items-center px-4 py-2.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground border-b border-white/[0.04]"
        style={{ gridTemplateColumns: "minmax(180px,2fr) 1fr 1fr 1fr 1fr" }}
      >
        <span>Market</span>
        <span className="text-right">Last</span>
        <span className="text-right">8h funding</span>
        <span className="text-right">{FUNDING_LABEL_1H}</span>
        <span className="text-right">24h vol</span>
      </div>
      <div className="divide-y divide-white/[0.04]">
        {sorted.map((row) => {
          const fp = row.funding8h === null || row.funding8h >= 0n;
          const ep = row.fundingHourly === null || row.fundingHourly >= 0n;
          return (
            <button
              key={row.marketId}
              type="button"
              onClick={() => router.push(`/perps?market=${row.symbol}`)}
              className="w-full grid items-center gap-3 px-4 py-3 text-left hover:bg-white/[0.03] transition-colors"
              style={{ gridTemplateColumns: "minmax(180px,2fr) 1fr 1fr 1fr 1fr" }}
            >
              <div className="flex items-center gap-2 min-w-0">
                <span className="text-sm font-semibold text-foreground truncate">
                  {row.symbol}
                </span>
                <span className="text-[10px] text-muted-foreground truncate">{row.name}</span>
              </div>
              <span className="text-xs font-mono tabular-nums text-foreground text-right">
                {row.price > 0 ? `$${row.price.toLocaleString("en-US", { maximumFractionDigits: 4 })}` : "—"}
              </span>
              <span className={`text-xs font-mono tabular-nums text-right ${fp ? "text-emerald-400" : "text-red-400"}`}>
                {row.funding8h === null ? "—" : formatFundingRatePct(row.funding8h)}
              </span>
              <span className={`text-xs font-mono tabular-nums text-right ${ep ? "text-emerald-400" : "text-red-400"}`}>
                {row.fundingHourly === null ? "—" : formatFundingRatePct(row.fundingHourly)}
              </span>
              <span className="text-xs font-mono tabular-nums text-muted-foreground text-right">
                {row.volume24h > 0 ? `$${(row.volume24h / 1e6).toFixed(2)}M` : "—"}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
