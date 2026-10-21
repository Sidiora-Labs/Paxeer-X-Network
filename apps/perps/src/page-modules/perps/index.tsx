import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import dynamic from "next/dynamic";
import { useRouter } from "next/router";
import { cn } from "@/ui/utils/cn";
import { PairHeader } from "@/widgets/trading/PairHeader";
import { MarketWatchlist } from "@/widgets/trading/MarketSelectorPanel";
import { MobileAppLayout } from "@/widgets/mobile/MobileAppLayout";
import { ResizeHandle } from "@/ui/atoms/resize-handle";
import { PERPS_MARKETS, type PerpsMarket } from "@/core/perps/config";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { useLayoutPresets } from "@/hooks/ui/useLayoutPresets";
import { ErrorBoundary } from "@/app/shell/ErrorBoundary";
import { ChartSkeleton, OrderbookSkeleton, PositionsSkeleton, OrderFormSkeleton } from "@/ui/atoms/skeleton-panel";
import { IcoWidget } from "@/ui/atoms/custom-icon";
import { Header } from "@/app/shell/Header";
import { MobileTradeHeader } from "@/app/shell/MobileTradeHeader";

const Chart = dynamic(
  () => import("@/widgets/trading/Chart").then((m) => ({ default: m.Chart })),
  { ssr: false, loading: () => <ChartSkeleton /> }
);
const Orderbook = dynamic(
  () => import("@/widgets/trading/Orderbook").then((m) => ({ default: m.Orderbook })),
  { ssr: false, loading: () => <OrderbookSkeleton /> }
);
const DepthPanel = dynamic(
  () => import("@/widgets/trading/Orderbook").then((m) => ({ default: m.DepthPanel })),
  { ssr: false, loading: () => <OrderbookSkeleton /> }
);
const PerpsOrderForm = dynamic(
  () => import("@/widgets/trading/PerpsOrderForm").then((m) => ({ default: m.PerpsOrderForm })),
  { ssr: false, loading: () => <OrderFormSkeleton /> }
);
const PerpsPositionsPanel = dynamic(
  () => import("@/widgets/trading/PerpsPositionsPanel").then((m) => ({ default: m.PerpsPositionsPanel })),
  { ssr: false, loading: () => <PositionsSkeleton /> }
);

const WATCHLIST_MIN = 160;
const WATCHLIST_MAX = 320;
const WATCHLIST_DEFAULT = 220;
const ORDERBOOK_MIN = 180;
const ORDERBOOK_MAX = 400;
const ORDERBOOK_DEFAULT = 260;
const CHART_MIN = 280;
const CHART_MAX = 900;
const CHART_DEFAULT = 520;
const POSITIONS_MIN = 160;
const POSITIONS_MAX = 800;
const POSITIONS_DEFAULT = 300;
const WORKSPACE_STORAGE_KEY = "sidiora-trading-workspace";

interface TradingWorkspaceState {
  orderbookWidth: number;
  chartHeight: number;
  positionsHeight: number;
}

function loadWorkspaceState(): TradingWorkspaceState {
  if (typeof window === "undefined") {
    return {
      orderbookWidth: ORDERBOOK_DEFAULT,
      chartHeight: CHART_DEFAULT,
      positionsHeight: POSITIONS_DEFAULT,
    };
  }
  try {
    const raw = localStorage.getItem(WORKSPACE_STORAGE_KEY);
    if (!raw) {
      return {
        orderbookWidth: ORDERBOOK_DEFAULT,
        chartHeight: CHART_DEFAULT,
        positionsHeight: POSITIONS_DEFAULT,
      };
    }
    const parsed = JSON.parse(raw) as Partial<TradingWorkspaceState>;
    return {
      orderbookWidth: Math.min(ORDERBOOK_MAX, Math.max(ORDERBOOK_MIN, parsed.orderbookWidth ?? ORDERBOOK_DEFAULT)),
      chartHeight: Math.min(CHART_MAX, Math.max(CHART_MIN, parsed.chartHeight ?? CHART_DEFAULT)),
      positionsHeight: Math.min(POSITIONS_MAX, Math.max(POSITIONS_MIN, parsed.positionsHeight ?? POSITIONS_DEFAULT)),
    };
  } catch {
    return {
      orderbookWidth: ORDERBOOK_DEFAULT,
      chartHeight: CHART_DEFAULT,
      positionsHeight: POSITIONS_DEFAULT,
    };
  }
}

export default function TradingInterface() {
  const router = useRouter();
  const { address } = usePerpsWallet();
  const { prices } = useRealtimePrices();
  const isMobile = useIsMobile();
  const { presets, activePreset, activeId, applyPreset } = useLayoutPresets();
  const [layoutMenuOpen, setLayoutMenuOpen] = useState(false);

  const selectedSymbol = (router.query.market as string)?.toUpperCase() || "PAX";
  const selectedMarket = useMemo<PerpsMarket>(
    () => PERPS_MARKETS.find((m) => m.symbol === selectedSymbol) ?? PERPS_MARKETS[0]!,
    [selectedSymbol]
  );

  const selectMarket = useCallback(
    (symbol: string) => {
      router.push(`/perps?market=${symbol}`, undefined, { shallow: true });
    },
    [router]
  );

  // Refs for direct DOM resize (zero re-renders during drag)
  const orderbookRef = useRef<HTMLDivElement>(null);
  const chartRowRef = useRef<HTMLDivElement>(null);
  const positionsPanelRef = useRef<HTMLDivElement>(null);
  const lastTitleWriteRef = useRef<number>(0);

  const [initialWorkspace] = useState(loadWorkspaceState);
  const [chartHeight, setChartHeight] = useState<number>(initialWorkspace.chartHeight);
  const [positionsHeight, setPositionsHeight] = useState<number>(initialWorkspace.positionsHeight);
  const [orderbookWidth, setOrderbookWidth] = useState<number>(initialWorkspace.orderbookWidth);

  const persistWorkspaceState = useCallback((next: TradingWorkspaceState) => {
    try {
      localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(next));
    } catch {}
  }, []);

  const applyLayoutPreset = useCallback((id: string) => {
    const preset = presets.find((p) => p.id === id);
    if (!preset) return;
    applyPreset(id);
    setChartHeight(preset.chartHeight);
    setPositionsHeight(preset.positionsHeight);
    setOrderbookWidth(preset.orderbookWidth);
  }, [applyPreset, presets]);

  useEffect(() => {
    if (orderbookRef.current) orderbookRef.current.style.width = `${orderbookWidth}px`;
    if (chartRowRef.current) chartRowRef.current.style.height = `${chartHeight}px`;
    if (positionsPanelRef.current) positionsPanelRef.current.style.height = `${positionsHeight}px`;
  }, [orderbookWidth, chartHeight, positionsHeight]);

  useEffect(() => {
    persistWorkspaceState({
      orderbookWidth,
      chartHeight,
      positionsHeight,
    });
  }, [orderbookWidth, chartHeight, positionsHeight, persistWorkspaceState]);

  // Sticky sidebar height: viewport minus header(50) + pairheader(~50) + footer(~30)
  const stickyH = "calc(100dvh - 130px)";

  // Title updates: depend on the selected market's price only (not the whole
  // prices map) and throttle writes to 1 Hz max to avoid per-tick tab repaints.
  const marketPrice = prices[selectedMarket.symbol];
  useEffect(() => {
    const formattedPrice = marketPrice
      ? new Intl.NumberFormat("en-US", {
          maximumFractionDigits: 2,
          minimumFractionDigits: 2,
        }).format(marketPrice)
      : "...";
    const nextTitle = `HyperPax | ${selectedMarket.symbol}/USD $${formattedPrice}`;
    const now = Date.now();
    const sinceLast = now - lastTitleWriteRef.current;
    if (sinceLast >= 1000) {
      lastTitleWriteRef.current = now;
      document.title = nextTitle;
      return;
    }
    const handle = window.setTimeout(() => {
      lastTitleWriteRef.current = Date.now();
      document.title = nextTitle;
    }, 1000 - sinceLast);
    return () => window.clearTimeout(handle);
  }, [selectedMarket.symbol, marketPrice]);

  // ═══════════════════ MOBILE ═══════════════════
  // Shell renders MobileTradeHeader (Component.shell === "trade"). The page
  // body just renders the mobile trading layout.
  if (isMobile) {
    return <MobileAppLayout market={selectedMarket} onSelectMarket={selectMarket} />;
  }

  // ═══════════════════ DESKTOP LAYOUT ═══════════════════
  // Single page scroll model:
  //   - PairHeader sticky at top (shell provides Header above this)
  //   - Right order-form sidebar sticky so it pins while center scrolls
  //   - Market selector lives inside PairHeader as a popover (no left rail)
  //   - Center column has natural height: chart row + positions panel
  //   - Resizing bottom panel changes page depth → page scrolls
  return (
    <div className="bg-surface-0 text-foreground">
      {/* Sticky pair-header band — sits below shell's <Header>. */}
      <div className="sticky top-0 z-30">
        <div className="flex items-center">
          <div className="flex-1 min-w-0 border-b-0">
            <PairHeader
              selectedPair={`${selectedMarket.symbol}/USD`}
              onPairChange={(pair: string) => selectMarket(pair.split("/")[0]!)}
            />
          </div>
          {/* Layout preset selector */}
          <div className="relative shrink-0 px-2">
            <button
              className="flex items-center gap-1 px-2 py-1 rounded text-[10px] font-medium text-muted-foreground hover:text-foreground hover:bg-muted/30 transition-colors"
              onClick={() => setLayoutMenuOpen((o) => !o)}
              title="Layout presets"
            >
              <IcoWidget size={14} />
              <span className="hidden lg:inline">{activePreset.label}</span>
            </button>
            {layoutMenuOpen && (
              <div className="absolute right-0 top-full mt-1 w-36 rounded-[var(--radius-lg)] bg-surface-2 shadow-lg z-50 py-1">
                {presets.map((p) => (
                  <button
                    key={p.id}
                    className={cn(
                      "w-full text-left px-3 py-1.5 text-xs transition-colors",
                      activeId === p.id ? "bg-primary/10 text-primary font-medium" : "hover:bg-muted/30 text-foreground"
                    )}
                    onClick={() => { applyLayoutPreset(p.id); setLayoutMenuOpen(false); }}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* PRO Mode — Resizable Panels (no left rail; market selector is in PairHeader popover). */}
      <div className="flex relative gap-1">
        {/* Center: Chart row + Positions — natural height, drives page scroll */}
        <div className="flex-1 min-w-0">
          {/* Chart + Orderbook row */}
          <div
            ref={chartRowRef}
            className="flex"
            style={{ height: `${chartHeight}px` }}
          >
            <div className="flex-1 min-w-0">
              <ErrorBoundary fallbackLabel="Chart">
                <Chart
                  selectedPair={`${selectedMarket.symbol}/USD`}
                  height={chartHeight}
                  onHeightChange={setChartHeight}
                  positions={[]}
                />
              </ErrorBoundary>
            </div>

            <ResizeHandle
              direction="horizontal"
              targetRef={orderbookRef}
              min={ORDERBOOK_MIN}
              max={ORDERBOOK_MAX}
              invert
              onResizeEnd={(w) => setOrderbookWidth(w)}
            />

            <div
              ref={orderbookRef}
              className="shrink-0 overflow-hidden"
              style={{ width: `${orderbookWidth}px` }}
            >
              <ErrorBoundary fallbackLabel="Orderbook">
                <DepthPanel symbol={selectedMarket.symbol} height={chartHeight} />
              </ErrorBoundary>
            </div>
          </div>

          {/* Vertical resize handle between chart and positions */}
          <ResizeHandle
            direction="vertical"
            targetRef={chartRowRef}
            cssProp="height"
            min={CHART_MIN}
            max={CHART_MAX}
            onResizeEnd={(h) => setChartHeight(h)}
          />

          {/* Bottom panel — resizable height, NO inner scroll, drives page depth */}
          <div
            ref={positionsPanelRef}
            className="overflow-hidden"
            style={{ height: `${positionsHeight}px` }}
          >
            <ErrorBoundary fallbackLabel="Positions">
              <PerpsPositionsPanel />
            </ErrorBoundary>
          </div>

          {/* Resize handle below positions to drag panel taller/shorter */}
          <ResizeHandle
            direction="vertical"
            targetRef={positionsPanelRef}
            cssProp="height"
            min={POSITIONS_MIN}
            max={POSITIONS_MAX}
            onResizeEnd={(h) => setPositionsHeight(h)}
          />
        </div>

        {/* Right: Order form sidebar — sticky */}
        <aside
          className="sticky top-[88px] self-start w-[340px] min-w-[340px] max-w-[340px] bg-surface-1 overflow-y-auto z-10 rounded-[var(--radius-lg)]"
          style={{ height: stickyH }}
        >
          <ErrorBoundary fallbackLabel="Order Form">
            <PerpsOrderForm market={selectedMarket} />
          </ErrorBoundary>
        </aside>
      </div>
    </div>
  );
}

// Re-enable AppShell so the universal DesktopSidebar renders here too.
// Mark this page as "trade" so the shell renders MobileTradeHeader on mobile.
(TradingInterface as { shell?: string }).shell = "trade";
