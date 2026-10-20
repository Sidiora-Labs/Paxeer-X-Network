import { useState, useEffect, useMemo, useCallback, useRef } from "react";
import dynamic from "next/dynamic";
import { useRouter } from "next/router";
import { cn } from "@/ui/utils/cn";
import { SpotPairHeader } from "@/widgets/spot/SpotPairHeader";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { ErrorBoundary } from "@/app/shell/ErrorBoundary";
import { ChartSkeleton, OrderbookSkeleton, PositionsSkeleton, OrderFormSkeleton } from "@/ui/atoms/skeleton-panel";
import { ResizeHandle } from "@/ui/atoms/resize-handle";
import { SPOT_MARKETS, DEFAULT_SPOT_SYMBOL, type SpotMarket } from "@/core/spot/config";
import { MobileSpotAppLayout } from "@/widgets/mobile/MobileSpotAppLayout";

const Chart = dynamic(
  () => import("@/widgets/trading/Chart").then((m) => ({ default: m.Chart })),
  { ssr: false, loading: () => <ChartSkeleton /> },
);
const SpotOrderbookWidget = dynamic(
  () => import("@/widgets/spot/SpotOrderbook").then((m) => ({ default: m.SpotOrderbook })),
  { ssr: false, loading: () => <OrderbookSkeleton /> },
);
const SpotOrderFormWidget = dynamic(
  () => import("@/widgets/spot/SpotOrderForm").then((m) => ({ default: m.SpotOrderForm })),
  { ssr: false, loading: () => <OrderFormSkeleton /> },
);
const SpotPositionsPanelWidget = dynamic(
  () => import("@/widgets/spot/SpotPositionsPanel").then((m) => ({ default: m.SpotPositionsPanel })),
  { ssr: false, loading: () => <PositionsSkeleton /> },
);

const ORDERBOOK_MIN = 180;
const ORDERBOOK_MAX = 400;
const ORDERBOOK_DEFAULT = 260;
const CHART_MIN = 280;
const CHART_MAX = 900;
const CHART_DEFAULT = 520;
const POSITIONS_MIN = 160;
const POSITIONS_MAX = 800;
const POSITIONS_DEFAULT = 300;
const WORKSPACE_STORAGE_KEY = "sidiora-spot-workspace";

interface SpotWorkspaceState {
  orderbookWidth: number;
  chartHeight: number;
  positionsHeight: number;
}

function loadWorkspaceState(): SpotWorkspaceState {
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
    const parsed = JSON.parse(raw) as Partial<SpotWorkspaceState>;
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

export default function SpotTradingInterface() {
  const router = useRouter();
  const { prices } = useRealtimePrices();
  const isMobileSnapshot = useIsMobile();

  // Two-pass mount: keep the first client render identical to SSR (always
  // the desktop branch) so React can hydrate without a structural
  // mismatch. After the first effect commits we flip `mounted` and
  // re-render — at that point `useIsMobile()` is trusted. This eliminates
  // React error #418 ("hydration failed") and #423 ("error during
  // hydration recovery") that previously surfaced on the SPOT page on
  // mobile devices.
  const [mounted, setMounted] = useState(false);
  useEffect(() => {
    setMounted(true);
  }, []);
  const isMobile = mounted && isMobileSnapshot;

  const selectedSymbol =
    (router.query.market as string)?.toUpperCase() || DEFAULT_SPOT_SYMBOL;
  const selectedMarket = useMemo<SpotMarket>(
    () => SPOT_MARKETS.find((m) => m.symbol === selectedSymbol) ?? SPOT_MARKETS[0]!,
    [selectedSymbol],
  );

  const selectMarket = useCallback(
    (symbol: string) => {
      router.push(`/spot?market=${symbol}`, undefined, { shallow: true });
    },
    [router],
  );

  const orderbookRef = useRef<HTMLDivElement>(null);
  const chartRowRef = useRef<HTMLDivElement>(null);
  const positionsPanelRef = useRef<HTMLDivElement>(null);
  const lastTitleWriteRef = useRef<number>(0);

  // SSR-safe workspace bootstrap: always start from the deterministic
  // defaults so the server-rendered HTML byte-matches the first client
  // render. Any persisted values from `localStorage` are loaded AFTER
  // mount via the effect below. This is what eliminates React #418
  // ("hydration failed") and #423 ("error during hydration recovery")
  // for users who previously customized their pane sizes — those custom
  // values were ending up in the inline `style={{ width: ... }}` /
  // `style={{ height: ... }}` attributes and diverging from SSR.
  const [chartHeight, setChartHeight] = useState<number>(CHART_DEFAULT);
  const [positionsHeight, setPositionsHeight] = useState<number>(POSITIONS_DEFAULT);
  const [orderbookWidth, setOrderbookWidth] = useState<number>(ORDERBOOK_DEFAULT);
  const [workspaceHydrated, setWorkspaceHydrated] = useState(false);

  useEffect(() => {
    const saved = loadWorkspaceState();
    setChartHeight(saved.chartHeight);
    setPositionsHeight(saved.positionsHeight);
    setOrderbookWidth(saved.orderbookWidth);
    setWorkspaceHydrated(true);
  }, []);

  const persistWorkspaceState = useCallback((next: SpotWorkspaceState) => {
    try {
      localStorage.setItem(WORKSPACE_STORAGE_KEY, JSON.stringify(next));
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    if (orderbookRef.current) orderbookRef.current.style.width = `${orderbookWidth}px`;
    if (chartRowRef.current) chartRowRef.current.style.height = `${chartHeight}px`;
    if (positionsPanelRef.current) positionsPanelRef.current.style.height = `${positionsHeight}px`;
  }, [orderbookWidth, chartHeight, positionsHeight]);

  useEffect(() => {
    // Skip persisting until we've loaded any saved state — otherwise the
    // first commit (which carries defaults) would overwrite the user's
    // persisted workspace before the load effect has a chance to run.
    if (!workspaceHydrated) return;
    persistWorkspaceState({ orderbookWidth, chartHeight, positionsHeight });
  }, [orderbookWidth, chartHeight, positionsHeight, persistWorkspaceState, workspaceHydrated]);

  const stickyH = "calc(100dvh - 130px)";

  const marketPrice = prices[selectedMarket.symbol];
  useEffect(() => {
    const formattedPrice = marketPrice
      ? new Intl.NumberFormat("en-US", {
          maximumFractionDigits: 2,
          minimumFractionDigits: 2,
        }).format(marketPrice)
      : "...";
    const nextTitle = `HyperPax | ${selectedMarket.symbol}/USD $${formattedPrice} [SPOT]`;
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
  // Mirrors the perps mobile shell exactly: dedicated MobileSpotAppLayout
  // owns the header / ticker / bottom nav and the three view tabs.
  if (isMobile) {
    return (
      <MobileSpotAppLayout
        market={selectedMarket}
        onSelectMarket={selectMarket}
      />
    );
  }

  // ═══════════════════ DESKTOP LAYOUT ═══════════════════
  return (
    <div className="bg-surface-0 text-foreground">
      <div className="sticky top-0 z-30">
        <div className="flex items-center">
          <div className="flex-1 min-w-0 border-b-0">
            <SpotPairHeader
              selectedPair={`${selectedMarket.symbol}/USD`}
              onPairChange={(pair: string) => selectMarket(pair.split("/")[0]!)}
            />
          </div>
        </div>
      </div>

      <div className="flex relative gap-1">
        <div className="flex-1 min-w-0">
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
                <SpotOrderbookWidget symbol={selectedMarket.symbol} height={chartHeight} />
              </ErrorBoundary>
            </div>
          </div>

          <ResizeHandle
            direction="vertical"
            targetRef={chartRowRef}
            cssProp="height"
            min={CHART_MIN}
            max={CHART_MAX}
            onResizeEnd={(h) => setChartHeight(h)}
          />

          <div
            ref={positionsPanelRef}
            className="overflow-hidden"
            style={{ height: `${positionsHeight}px` }}
          >
            <ErrorBoundary fallbackLabel="Positions">
              <SpotPositionsPanelWidget />
            </ErrorBoundary>
          </div>

          <ResizeHandle
            direction="vertical"
            targetRef={positionsPanelRef}
            cssProp="height"
            min={POSITIONS_MIN}
            max={POSITIONS_MAX}
            onResizeEnd={(h) => setPositionsHeight(h)}
          />
        </div>

        <aside
          className="sticky top-[88px] self-start w-[340px] min-w-[340px] max-w-[340px] bg-surface-1 overflow-y-auto z-10 rounded-[var(--radius-lg)]"
          style={{ height: stickyH }}
        >
          <ErrorBoundary fallbackLabel="Order Form">
            <SpotOrderFormWidget market={selectedMarket} />
          </ErrorBoundary>
        </aside>
      </div>
    </div>
  );
}

(SpotTradingInterface as { shell?: string }).shell = "trade";
