import { useState, useMemo } from "react";
import Link from "next/link";
import { ChevronDown, Eye, EyeOff, X, Search } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { PERPS_MARKETS, coinLogoUrl } from "@/core/perps/config";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useEnrichedMarketStats } from "@/hooks/perps/useEnrichedStats";
import { useSidPaxOrderbook, useSidPaxTrades } from "@/hooks/perps/useSidPaxMarketStream";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useWalletStore } from "@/hooks/wallet/useWalletStore";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";
import { MobileNavTrigger, MobileNavDrawer, useMobileNav } from "@/app/shell/MobileNav";
import { DepositWithdrawModal } from "@/app/modals/DepositWithdrawModal";
import { fmtPrice as formatPrice, fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { HyperPaxWordmark } from "@/ui/atoms/HyperPaxWordmark";

const MKT_RWA_IDS = [5, 6, 7, 8, 9, 10];
const MKT_NEW_IDS = [5, 6, 7, 8, 9, 10];

function fmtChangePct(open: number, close: number): { text: string; positive: boolean } {
  if (open <= 0) return { text: "—", positive: true };
  const pct = ((close - open) / open) * 100;
  return { text: `${pct >= 0 ? "+" : ""}${pct.toFixed(2)}%`, positive: pct >= 0 };
}

interface MobileTradeHeaderProps {
  selectedPair: string;
  onOpenMarketPanel: () => void;
  onSelectMarket?: (symbol: string) => void;
}

export function MobileTradeHeader({ selectedPair, onOpenMarketPanel, onSelectMarket }: MobileTradeHeaderProps) {
  const [privacyOn, setPrivacyOn] = useState(true);
  const [isDepositOpen, setIsDepositOpen] = useState(false);
  const [isMarketOpen, setIsMarketOpen] = useState(false);
  const [mktSearch, setMktSearch] = useState("");
  const [mktTab, setMktTab] = useState<"all" | "rwa" | "new">("all");
  const { open: mobileNavOpen, openNav, closeNav } = useMobileNav();
  const { prices, tickers } = useRealtimePrices();
  const { isConnected, shortAddress, connect } = usePerpsWallet();
  const openConnectModal = useWalletStore((s) => s.openConnectModal);
  const stats = usePortfolioStats();

  const symbol = selectedPair.split("/")[0];
  const market = PERPS_MARKETS.find((m) => m.symbol === symbol) ?? PERPS_MARKETS[0]!;
  const { data: enriched } = useEnrichedMarketStats(market.marketId);

  // Same fallback pattern as PairHeader: realtime /tickers is primary, subgraph
  // enrichedMarketStats covers Paxeer-native markets (PAX / SID / …) that are
  // not tracked by the realtime feed.
  const pickNum = (...values: Array<number | null | undefined>): number => {
    for (const v of values) {
      if (typeof v === "number" && !Number.isNaN(v) && v !== 0) return v;
    }
    return 0;
  };

  const mktFiltered = useMemo(() => {
    let list = [...PERPS_MARKETS];
    if (mktTab === "rwa") list = list.filter((m) => MKT_RWA_IDS.includes(m.marketId));
    else if (mktTab === "new") list = list.filter((m) => MKT_NEW_IDS.includes(m.marketId));
    if (mktSearch) {
      const q = mktSearch.toLowerCase();
      list = list.filter((m) => m.symbol.toLowerCase().includes(q) || m.name.toLowerCase().includes(q));
    }
    return list;
  }, [mktTab, mktSearch]);

  const orderbook = useSidPaxOrderbook(market.symbol);
  const trades = useSidPaxTrades(market.symbol);
  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastTradePrice = trades[0]?.price ?? 0;

  const ticker = tickers[market.symbol];
  const et = enriched?.ticker;
  const markPrice = midPrice || lastTradePrice || pickNum(ticker?.mark_price, et?.mark_price, prices[market.symbol]);
  const open24h = pickNum(ticker?.["24h_open"], et?.h24_open);
  const close24h = pickNum(ticker?.["24h_close"], et?.h24_close) || markPrice;
  const change = fmtChangePct(open24h, close24h);

  return (
    <>
      <header className="flex items-center justify-between px-3 bg-[var(--brand-black)] border-b border-[var(--brand-border)]" style={{ height: "52px" }}>
        {/* Left: hamburger + logo */}
        <div className="flex items-center gap-2 shrink-0">
          <MobileNavTrigger onClick={openNav} />
          <Link href="/" className="flex items-center">
            <HyperPaxWordmark size="sm" />
          </Link>
        </div>

        {/* Center: pair selector with leverage badge + 24h change */}
        <button
          className="flex items-center gap-1.5 flex-1 mx-2 min-w-0 justify-start"
          onClick={() => { onOpenMarketPanel(); setIsMarketOpen(true); }}
        >
          <img
            src={coinLogoUrl(market.symbol)}
            alt={market.symbol}
            className="w-5 h-5 rounded-full object-cover shrink-0"
          />
          <div className="flex flex-col items-start min-w-0">
            <div className="flex items-center gap-1">
              <span className="text-sm font-semibold text-white">{market.symbol}</span>
              <span className="text-[9px] font-mono font-medium text-[#AFAFAF] bg-[var(--brand-border)] rounded px-1 py-[1px]">{market.maxLeverage}x</span>
              <ChevronDown className="w-3 h-3 text-[#828282]" />
            </div>
            <span className={cn("text-[10px] font-mono font-medium", change.positive ? "text-[var(--brand-green)]" : "text-[var(--brand-red)]")}>
              {change.text}
            </span>
          </div>
        </button>

        {/* Right: connect button + total value eye toggle */}
        <div className="flex flex-col items-end gap-0.5 shrink-0">
          {!isConnected ? (
            <button
              className="px-3 py-1.5 rounded-lg text-xs font-semibold text-white"
              style={{ backgroundColor: "var(--brand-blue)" }}
              onClick={connect}
            >
              Connect
            </button>
          ) : (
            <button
              className="px-2.5 py-1 rounded-lg text-[10px] font-mono font-semibold text-white border border-[var(--brand-border)] bg-[var(--brand-surface)]"
              onClick={openConnectModal}
            >
              {shortAddress}
            </button>
          )}
          <button
            className="flex items-center gap-1 text-[#828282]"
            onClick={() => setPrivacyOn((v) => !v)}
          >
            <span className="text-[9px] text-[#828282]">Total value</span>
            {privacyOn ? <EyeOff className="w-3 h-3" /> : <Eye className="w-3 h-3" />}
          </button>
        </div>
      </header>

      <MobileNavDrawer open={mobileNavOpen} onClose={closeNav} />
      <DepositWithdrawModal isOpen={isDepositOpen} onClose={() => setIsDepositOpen(false)} />

      {/* ── Full-screen market selector sheet ── */}
      {isMarketOpen && (
        <div className="fixed inset-0 z-[100] flex flex-col overflow-hidden" style={{ background: "var(--brand-black)" }}>
          {/* Header */}
          <div className="flex items-center justify-between px-4 border-b border-[var(--brand-border)]" style={{ height: 52 }}>
            <span className="text-sm font-semibold text-white">Markets</span>
            <button
              onClick={() => { setIsMarketOpen(false); setMktSearch(""); }}
              className="p-1 text-[#828282] hover:text-white transition-colors"
              aria-label="Close"
            >
              <X className="w-5 h-5" />
            </button>
          </div>

          {/* Search */}
          <div className="px-4 py-2.5 border-b border-[var(--brand-border)]">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-[#828282]" />
              <input
                placeholder="Search markets…"
                value={mktSearch}
                onChange={(e) => setMktSearch(e.target.value)}
                className="w-full rounded-full pl-8 pr-3 py-2 text-sm bg-[#111] border border-[var(--brand-border)] text-white outline-none placeholder-[#828282]"
                autoFocus
              />
            </div>
          </div>

          {/* Filter tabs */}
          <div className="flex items-center gap-1 px-4 py-2.5 border-b border-[var(--brand-border)]">
            {(["all", "rwa", "new"] as const).map((t) => (
              <button
                key={t}
                onClick={() => setMktTab(t)}
                className="px-3 py-1 rounded-full text-[10px] font-mono tracking-wider uppercase transition-colors"
                style={{
                  background: mktTab === t ? "#1A1A1A" : "transparent",
                  color: mktTab === t ? "#fff" : "#828282",
                  border: mktTab === t ? "1px solid #2A2A2A" : "1px solid transparent",
                }}
              >
                {t}
              </button>
            ))}
          </div>

          {/* Column headers */}
          <div className="grid grid-cols-[1fr_auto_auto] gap-2 px-4 py-2 border-b border-[var(--brand-border)] text-[9px] font-mono uppercase tracking-wider text-[#828282]">
            <span>Market</span>
            <span className="text-right w-20">Price</span>
            <span className="text-right w-14">24h</span>
          </div>

          {/* Market list */}
          <div className="flex-1 overflow-y-auto">
            {mktFiltered.map((m) => {
              const mPrice = prices[m.symbol] ?? 0;
              const mTicker = tickers[m.symbol];
              const open24 = mTicker?.["24h_open"] ?? 0;
              const chg = open24 > 0 ? ((mPrice - open24) / open24) * 100 : null;
              const isSelected = market.symbol === m.symbol;
              return (
                <button
                  key={m.symbol}
                  onClick={() => { onSelectMarket?.(m.symbol); setIsMarketOpen(false); setMktSearch(""); }}
                  className="w-full grid grid-cols-[1fr_auto_auto] gap-2 px-4 py-2.5 items-center border-b border-[#1A1A1A] transition-colors hover:bg-[#0D0D0D]"
                  style={{
                    background: isSelected ? "rgba(0, 76, 237, 0.10)" : undefined,
                    borderLeft: isSelected ? "2px solid var(--brand-blue)" : "2px solid transparent",
                  }}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <img src={coinLogoUrl(m.symbol)} alt="" className="w-7 h-7 rounded-full object-cover shrink-0" />
                    <div className="flex flex-col items-start">
                      <div className="flex items-center gap-1">
                        <span className="text-sm font-semibold text-white">{m.symbol}</span>
                        <span className="text-[9px] font-mono text-[#AFAFAF] bg-[var(--brand-border)] rounded px-1 py-[1px]">{m.maxLeverage}x</span>
                        {MKT_NEW_IDS.includes(m.marketId) && (
                          <span className="text-[8px] font-mono px-1 rounded" style={{ background: "rgba(255,158,44,0.15)", color: "var(--brand-orange)" }}>NEW</span>
                        )}
                      </div>
                      <span className="text-[9px] font-mono text-[#828282]">
                        {mTicker?.["24h_volume"] ? fmtUsd(mTicker["24h_volume"]) + " vol" : m.name}
                      </span>
                    </div>
                  </div>
                  <span className="text-xs font-mono text-right tabular-nums text-white w-20">
                    {mPrice > 0 ? formatPrice(mPrice) : "—"}
                  </span>
                  <span
                    className="text-xs font-mono text-right tabular-nums w-14"
                    style={{ color: chg === null ? "#828282" : chg >= 0 ? "var(--brand-green)" : "var(--brand-red)" }}
                  >
                    {chg !== null ? `${chg >= 0 ? "+" : ""}${chg.toFixed(2)}%` : "—"}
                  </span>
                </button>
              );
            })}
            {mktFiltered.length === 0 && (
              <div className="flex items-center justify-center py-12 text-[#828282] text-sm font-mono">
                No markets
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}
