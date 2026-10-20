import { useEffect, useRef, useMemo, useState, useCallback, memo } from "react";
import type { ResolutionString } from "../../../public/static/charting_library/charting_library";
import { PerpsDatafeed } from "@/core/datafeed/perps";
import { useTheme } from "next-themes";
import { useOrderLineStore } from "@/hooks/perps/useOrderLineStore";
import { cn } from "@/ui/utils/cn";
import { BarChart2, Settings2, Maximize2 } from "lucide-react";
import { PaxSpinner } from "@/ui/atoms/PaxSpinner";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { loadTradingView } from "./tradingViewLoader";

// NOTE: previously this was a module-level singleton. On mobile we observed
// the TV widget's cleanup not always calling `unsubscribeBars` before the
// next widget is constructed, which left stale intervals and cache entries
// on the shared datafeed — causing subsequent widgets to get stuck in the
// "loading" state. Use a fresh instance per Chart mount instead (see
// `datafeedRef` inside the component).


declare global {
  interface Window {
    TradingView: any;
  }
}

const chartingLibraryPath = "/static/charting_library/";

const TOOLBAR_H = 34;

// Must stay in sync with `tvResolutionToKlineInterval` in
// `useSidPaxMarketStream.ts` and the UDF `supported_resolutions` list
// on the per-symbol candle servers: `1, 2, 3, 5, 15, 30, 60, 240, 480,
// D, W`. (Monthly is not produced by the candle servers.)
const INTERVALS: { label: string; value: string }[] = [
  { label: "1m",  value: "1" },
  { label: "3m",  value: "3" },
  { label: "5m",  value: "5" },
  { label: "15m", value: "15" },
  { label: "30m", value: "30" },
  { label: "1h",  value: "60" },
  { label: "4h",  value: "240" },
  { label: "8h",  value: "480" },
  { label: "1D",  value: "1D" },
  { label: "1W",  value: "1W" },
];

// ── Isolated toolbar component — renders independently of Chart state ──
interface ToolbarProps {
  currentInterval: string;
  onInterval: (v: string) => void;
  onIndicators: () => void;
  onSettings: () => void;
  onFullscreen: () => void;
}

const ChartToolbar = memo(function ChartToolbar({ currentInterval, onInterval, onIndicators, onSettings, onFullscreen }: ToolbarProps) {
  return (
    <div
      className="flex items-center px-1.5 border-b border-border bg-card shrink-0"
      style={{ height: TOOLBAR_H }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      {INTERVALS.map((iv) => (
        <button
          key={iv.value}
          onClick={() => onInterval(iv.value)}
          className={cn(
            "h-[26px] min-w-[28px] px-1.5 text-[11px] font-medium rounded transition-colors",
            currentInterval === iv.value
              ? "bg-primary/10 text-primary font-semibold"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
          )}
        >
          {iv.label}
        </button>
      ))}
      <div className="flex-1" />
      <button onClick={onIndicators} title="Indicators"
        className="h-[26px] w-[28px] flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded transition-colors">
        <BarChart2 className="h-3.5 w-3.5" />
      </button>
      <button onClick={onSettings} title="Chart settings"
        className="h-[26px] w-[28px] flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded transition-colors">
        <Settings2 className="h-3.5 w-3.5" />
      </button>
      <button onClick={onFullscreen} title="Fullscreen"
        className="h-[26px] w-[28px] flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded transition-colors">
        <Maximize2 className="h-3.5 w-3.5" />
      </button>
    </div>
  );
});

// Calculate time ranges
const now = Math.floor(Date.now() / 1000);

const getFormattedSymbol = (pair: string) => {
  return pair.split("/")[0];
};

function fmtLinePrice(p: number): string {
  if (p >= 10000) return p.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (p >= 1) return p.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
  return p.toLocaleString("en-US", { minimumFractionDigits: 4, maximumFractionDigits: 6 });
}

// Resolve the TradingView pane background color for the active app theme.
// NOTE: This color MUST also be re-applied via `widget.applyOverrides` inside
// `onChartReady` — `load_last_chart: true` restores a saved layout whose pane
// background (TV's default `#131722` navy) would otherwise overwrite the
// `overrides` object on symbol-change re-init.
function resolveChartBg(theme: string | undefined): string {
  switch (theme) {
    case "greenify": return "#0a0e18";
    case "light":    return "#ffffff";
    case "hotline":  return "#0f0c2c";
    case "oled":     return "#000000";
    default:         return "#18191d";
  }
}

interface ChartProps {
  selectedPair?: string;
  height: number;
  onHeightChange: (height: number) => void;
  /** @deprecated Unused. All current callers pass []. */
  positions?: unknown[];
}


export function Chart({ selectedPair = "ETH/USD", height, onHeightChange }: ChartProps) {
  const widgetRef = useRef<any>(null);
  // Per-instance datafeed. Lazy-init so SSR doesn't touch window.
  const datafeedRef = useRef<PerpsDatafeed | null>(null);
  if (datafeedRef.current === null) datafeedRef.current = new PerpsDatafeed();
  const isDragging = useRef(false);
  const startY = useRef(0);
  const startHeight = useRef(0);
  const [isChartReady, setIsChartReady] = useState(false);
  const isChartReadyRef = useRef(false);
  const [currentInterval, setCurrentInterval] = useState("15");
  const { theme } = useTheme();
  // Always-current ref so loadTradingView (called from a [selectedPair] effect
  // closure) reads the live theme even when next-themes hydrates after the
  // effect first fires — prevents the chart reverting to default TV styling.
  const themeRef = useRef(theme);
  themeRef.current = theme;

  // ── Draggable order lines (Entry / TP / SL) ──
  const entryLineRef = useRef<any>(null);
  const tpLineRef = useRef<any>(null);
  const slLineRef = useRef<any>(null);
  const olStore = useOrderLineStore;

  // Effect for theme changes.
  // Prefer the TradingView runtime API `widget.changeTheme()` when switching
  // between the built-in Light/Dark themes — this is ~50ms and does not blank
  // the chart. Only fall back to a full rebuild for custom themes
  // (greenify/hotline/oled) whose styling can't be expressed via the runtime
  // API, and in that case fade the container during the rebuild so the user
  // sees a smooth crossfade instead of a white flash.
  const lastThemeRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!widgetRef.current) { lastThemeRef.current = theme; return; }
    const prev = lastThemeRef.current;
    lastThemeRef.current = theme;
    const isBuiltIn = (t: string | undefined) => t === "light" || t === "dark";

    if (isBuiltIn(theme) && isBuiltIn(prev)) {
      try {
        widgetRef.current.changeTheme?.(theme === "dark" ? "Dark" : "Light");
        return;
      } catch {
        // fall through to rebuild
      }
    }

    // Fade-out → rebuild → fade-in
    const container = document.getElementById("tv_chart_container");
    if (container) {
      container.style.transition = "opacity 180ms ease-out";
      container.style.opacity = "0";
    }
    const rebuildTimeout = window.setTimeout(() => {
      if (widgetRef.current) {
        widgetRef.current.remove();
        widgetRef.current = null;
        isChartReadyRef.current = false;
      }
      loadTradingView().finally(() => {
        const c = document.getElementById("tv_chart_container");
        if (c) {
          c.style.transition = "opacity 220ms ease-in";
          c.style.opacity = "1";
        }
      });
    }, 180);
    return () => window.clearTimeout(rebuildTimeout);
  }, [theme]);

  // Main chart initialization effect
  const loadTradingViewWidget = async () => {
    try {
      if (typeof window === "undefined" || !window.TradingView) {
        return;
      }

      // Remove old widget if it exists
      if (widgetRef.current) {
        try { widgetRef.current.remove(); } catch {}
        widgetRef.current = null;
        isChartReadyRef.current = false;
      }

      const container = document.getElementById('tv_chart_container');
      if (!container) {
        return;
      }

      // Mobile / PWA defense: some TV builds leave iframe nodes behind after
      // `remove()`, which prevents a fresh widget from taking over the same
      // container id. Nuke any residual children before re-init.
      try {
        while (container.firstChild) container.removeChild(container.firstChild);
      } catch {}

      const widget = new window.TradingView.widget({
        container: "tv_chart_container",
        locale: "en",
        library_path: chartingLibraryPath,
        datafeed: datafeedRef.current,
        symbol: getFormattedSymbol(selectedPair),
        interval: "5" as ResolutionString,
        autosize: true,
        debug: false,
        enabled_features: [
          "show_exchange_logos",
          "side_toolbar_in_fullscreen_mode",
          "hide_resolution_in_legend",
          "items_favoriting",
          "iframe_loading_compatibility_mode",
        ],
        disabled_features: [
          "header_widget",
          "volume_force_overlay",
          "create_volume_indicator_by_default",
          "display_market_status",
          "show_interval_dialog_on_key_press",
          "popup_hints",
          "right_bar_stays_on_scroll",
          "symbol_info",
          "timeframes_toolbar",
        ],
        theme: "dark",
        overrides: {
          "paneProperties.backgroundType": "solid",
          "paneProperties.legendProperties.showBackground": false,
          // Hide grid lines
          "paneProperties.horzGridProperties.color": "rgba(0, 0, 0, 0)",
          "paneProperties.vertGridProperties.color": "rgba(0, 0, 0, 0)",
          "paneProperties.horzGridProperties.style": 0,
          "paneProperties.vertGridProperties.style": 0,
          "paneProperties.background": resolveChartBg(themeRef.current),
          "scalesProperties.backgroundColor": resolveChartBg(themeRef.current),
        },
        load_last_chart: true,
        saved_data: null,
        auto_save_delay: 0,
        max_bars: 300,
        range: "1D",
        custom_css_url: `../${themeRef.current}.css`,
        allow_symbol_change: false,
        favorites: {
          intervals: [
            "1",
            "5",
            "15",
            "30",
            "60",
            "240",
            "1D",
          ] as ResolutionString[],
        },
        loading_screen: { backgroundColor: "transparent" },
        visible_range: {
          from: now - 24 * 60 * 60,
          to: now,
        },
        auto_scale: true,
        initial_data: {
          resolution: "15" as ResolutionString,
          from: now - 24 * 60 * 60,
          to: now,
        },
      });

      widgetRef.current = widget;

      widget.onChartReady(() => {
        isChartReadyRef.current = true;
        setIsChartReady(true);

        // Re-assert background colors AFTER `load_last_chart: true` has
        // restored the saved layout. Without this, on symbol-change re-init
        // the saved layout's pane bg (TV's default navy `#131722`) wins
        // over the `overrides` object passed at construction time.
        try {
          const bg = resolveChartBg(themeRef.current);
          widget.applyOverrides({
            "paneProperties.backgroundType": "solid",
            "paneProperties.background": bg,
            "scalesProperties.backgroundColor": bg,
          });
        } catch (err) {
          console.warn("[Chart] Failed to re-apply background overrides:", err);
        }

        const chart = widget.activeChart();
        try { setCurrentInterval(chart.resolution()); } catch {}

        // Custom text watermark via IWatermarkApi
        try {
          const wm = chart.watermark();
          wm.setContentProvider(() => ({
            lines: [
              {
                text: "HYPERPAX",
                font: "bold 32px sans-serif",
                lineSpacing: 0,
                vertOffset: 0,
                color: "rgba(255, 255, 255, 0.12)",
              },
            ],
          }));
          try { wm.visibility.setValue(true); } catch {}
          try { wm.visibility.ticker?.setValue(true); } catch {}
        } catch {}

        chart.getSeries().setChartStyleProperties(1, {
          upColor: "#0ebc5f",
          downColor: "#ff4c45",
          borderUpColor: "#0ebc5f",
          borderDownColor: "#ff4c45",
          wickUpColor: "#0ebc5f",
          wickDownColor: "#ff4c45",
        });
        
      });
    } catch (error) {
      console.error("Error initializing TradingView:", error);
    }
  };

  useEffect(() => {
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    const MAX_RETRIES = 60; // ~12s at 200ms interval — handles slow mobile cold loads

    const tryLoad = (attempt: number) => {
      if (cancelled) return;
      const tvReady = typeof window !== "undefined" && !!window.TradingView;
      const containerReady = !!document.getElementById('tv_chart_container');
      if (tvReady && containerReady) {
        loadTradingViewWidget();
        return;
      }
      if (attempt >= MAX_RETRIES) {
        console.warn("[Chart] TradingView or container not ready after retries; giving up.");
        return;
      }
      retryTimer = setTimeout(() => tryLoad(attempt + 1), 200);
    };

    // F-04: kick the lazy script load (no-op if already cached) before
    // starting the retry probe. tryLoad still polls because the chart
    // container element mounts asynchronously inside this component.
    loadTradingView().catch((err) => {
      console.error("[Chart] Failed to load TradingView library:", err);
    });
    tryLoad(0);

    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      try { entryLineRef.current?.remove(); } catch {}
      try { tpLineRef.current?.remove(); } catch {}
      try { slLineRef.current?.remove(); } catch {}
      entryLineRef.current = null;
      tpLineRef.current = null;
      slLineRef.current = null;
      if (widgetRef.current) {
        try { widgetRef.current.remove(); } catch (error) {
          console.error("Error cleaning up TradingView widget:", error);
        }
        widgetRef.current = null;
        isChartReadyRef.current = false;
        setIsChartReady(false);
      }
      // Also nuke any leftover DOM inside the container as a safety net
      // for mobile / PWA where TV's `remove()` may not fully clean up.
      try {
        const leftover = document.getElementById('tv_chart_container');
        if (leftover) {
          while (leftover.firstChild) leftover.removeChild(leftover.firstChild);
        }
      } catch {}
    };
  }, [selectedPair]);

  // ── Reactive order line state from store ──
  const olOrderType = useOrderLineStore((s) => s.orderType);
  const olIsLong = useOrderLineStore((s) => s.isLong);
  const olEntryPrice = useOrderLineStore((s) => s.entryPrice);
  const olTpPrice = useOrderLineStore((s) => s.tpPrice);
  const olSlPrice = useOrderLineStore((s) => s.slPrice);
  const olTpslEnabled = useOrderLineStore((s) => s.tpslEnabled);

  // Order lines effect — runs when chart is ready AND store values change
  useEffect(() => {
    if (!widgetRef.current || !isChartReady) return;

    const chart = widgetRef.current.activeChart();
    if (!chart) return;

    const isLimitType = olOrderType === "limit" || olOrderType === "stop-limit";

    // ── ENTRY LINE ──
    if (isLimitType && olEntryPrice > 0) {
      if (entryLineRef.current) {
        // Update existing
        try {
          entryLineRef.current.setPrice(olEntryPrice);
          entryLineRef.current.setText(`Entry @ $${fmtLinePrice(olEntryPrice)}`);
        } catch { entryLineRef.current = null; }
      }
      if (!entryLineRef.current) {
        try {
          const lineColor = olIsLong ? "#0ebc5f" : "#ff4c45";
          const bgColor = olIsLong ? "rgba(5, 193, 104, 0.12)" : "rgba(255, 90, 101, 0.12)";
          const textColor = olIsLong ? "#0ebc5f" : "#ff4c45";

          const line = chart.createOrderLine()
            .setPrice(olEntryPrice)
            .setText(`Entry @ $${fmtLinePrice(olEntryPrice)}`)
            .setQuantity("")
            .setLineColor(lineColor)
            .setLineWidth(2)
            .setLineStyle(2)
            .setBodyBorderColor(lineColor)
            .setBodyBackgroundColor(bgColor)
            .setBodyTextColor(textColor)
            .setQuantityBorderColor(lineColor)
            .setQuantityBackgroundColor(bgColor)
            .setQuantityTextColor(textColor)
            .setEditable(true)
            .setModifyTooltip("Drag to adjust entry price")
            // eslint-disable-next-line @typescript-eslint/no-this-alias
            .onMove(function (this: any) {
              const p = Math.round(this.getPrice() * 100) / 100;
              this.setText(`Entry @ $${fmtLinePrice(p)}`);
              olStore.getState().onChartDragEntry(p);
            });
          entryLineRef.current = line;
        } catch (err) {
          console.warn("[Chart] Failed to create Entry order line:", err);
        }
      }
    } else {
      if (entryLineRef.current) {
        try { entryLineRef.current.remove(); } catch {}
        entryLineRef.current = null;
      }
    }

    // ── TP LINE ──
    if (olTpslEnabled && olTpPrice > 0) {
      if (tpLineRef.current) {
        try {
          tpLineRef.current.setPrice(olTpPrice);
          tpLineRef.current.setText(`TP @ $${fmtLinePrice(olTpPrice)}`);
        } catch { tpLineRef.current = null; }
      }
      if (!tpLineRef.current) {
        try {
          const line = chart.createOrderLine()
            .setPrice(olTpPrice)
            .setText(`TP @ $${fmtLinePrice(olTpPrice)}`)
            .setQuantity("")
            .setLineColor("#0ebc5f")
            .setLineWidth(1)
            .setLineStyle(2)
            .setBodyBorderColor("#0ebc5f")
            .setBodyBackgroundColor("rgba(5, 193, 104, 0.12)")
            .setBodyTextColor("#0ebc5f")
            .setQuantityBorderColor("#0ebc5f")
            .setQuantityBackgroundColor("rgba(5, 193, 104, 0.12)")
            .setQuantityTextColor("#0ebc5f")
            .setEditable(true)
            .setModifyTooltip("Drag to adjust take profit")
            // eslint-disable-next-line @typescript-eslint/no-this-alias
            .onMove(function (this: any) {
              const p = Math.round(this.getPrice() * 100) / 100;
              this.setText(`TP @ $${fmtLinePrice(p)}`);
              olStore.getState().onChartDragTp(p);
            });
          tpLineRef.current = line;
        } catch (err) {
          console.warn("[Chart] Failed to create TP order line:", err);
        }
      }
    } else {
      if (tpLineRef.current) {
        try { tpLineRef.current.remove(); } catch {}
        tpLineRef.current = null;
      }
    }

    // ── SL LINE ──
    if (olTpslEnabled && olSlPrice > 0) {
      if (slLineRef.current) {
        try {
          slLineRef.current.setPrice(olSlPrice);
          slLineRef.current.setText(`SL @ $${fmtLinePrice(olSlPrice)}`);
        } catch { slLineRef.current = null; }
      }
      if (!slLineRef.current) {
        try {
          const line = chart.createOrderLine()
            .setPrice(olSlPrice)
            .setText(`SL @ $${fmtLinePrice(olSlPrice)}`)
            .setQuantity("")
            .setLineColor("#ff4c45")
            .setLineWidth(1)
            .setLineStyle(2)
            .setBodyBorderColor("#ff4c45")
            .setBodyBackgroundColor("rgba(255, 90, 101, 0.12)")
            .setBodyTextColor("#ff4c45")
            .setQuantityBorderColor("#ff4c45")
            .setQuantityBackgroundColor("rgba(255, 90, 101, 0.12)")
            .setQuantityTextColor("#ff4c45")
            .setEditable(true)
            .setModifyTooltip("Drag to adjust stop loss")
            // eslint-disable-next-line @typescript-eslint/no-this-alias
            .onMove(function (this: any) {
              const p = Math.round(this.getPrice() * 100) / 100;
              this.setText(`SL @ $${fmtLinePrice(p)}`);
              olStore.getState().onChartDragSl(p);
            });
          slLineRef.current = line;
        } catch (err) {
          console.warn("[Chart] Failed to create SL order line:", err);
        }
      }
    } else {
      if (slLineRef.current) {
        try { slLineRef.current.remove(); } catch {}
        slLineRef.current = null;
      }
    }
  }, [isChartReady, olOrderType, olIsLong, olEntryPrice, olTpPrice, olSlPrice, olTpslEnabled]);

  // Cleanup order lines on unmount
  useEffect(() => {
    return () => {
      try { entryLineRef.current?.remove(); } catch {}
      try { tpLineRef.current?.remove(); } catch {}
      try { slLineRef.current?.remove(); } catch {}
      entryLineRef.current = null;
      tpLineRef.current = null;
      slLineRef.current = null;
    };
  }, []);

  const handleIntervalChange = useCallback((value: string) => {
    setCurrentInterval(value);
    if (!widgetRef.current) return;
    // No isChartReadyRef gate — try directly; works even when the flag
    // lied (e.g. PWA iframe where onChartReady fired before the React
    // ref was written).
    const trySet = () => {
      try {
        const chart = widgetRef.current.activeChart?.() ?? widgetRef.current.chart?.();
        chart?.setResolution(value as ResolutionString, () => {});
      } catch {}
    };
    trySet();
    // Retry once after 300 ms in case the chart wasn't ready on the first try.
    setTimeout(trySet, 300);
  }, []);

  const handleIndicators = useCallback(() => {
    if (!widgetRef.current) return;
    try { (widgetRef.current.activeChart?.() ?? widgetRef.current.chart?.())?.executeActionById("insertIndicator"); } catch {}
  }, []);

  const handleChartSettings = useCallback(() => {
    if (!widgetRef.current) return;
    try { (widgetRef.current.activeChart?.() ?? widgetRef.current.chart?.())?.executeActionById("chartProperties"); } catch {}
  }, []);

  const handleFullscreen = useCallback(() => {
    if (!widgetRef.current) return;
    try { widgetRef.current.startFullscreen?.(); } catch {}
  }, []);

  // Also restore the resize handlers
  const handleMouseDown = (e: React.MouseEvent) => {
    isDragging.current = true;
    startY.current = e.clientY;
    startHeight.current = height;
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
  };

  const handleMouseMove = (e: MouseEvent) => {
    if (!isDragging.current) return;
    const deltaY = e.clientY - startY.current;
    const newHeight = Math.max(300, Math.min(800, startHeight.current + deltaY));
    onHeightChange(newHeight);
    if (widgetRef.current) {
      setTimeout(() => window.dispatchEvent(new Event('resize')), 0);
    }
  };

  const handleMouseUp = () => {
    isDragging.current = false;
    document.removeEventListener('mousemove', handleMouseMove);
    document.removeEventListener('mouseup', handleMouseUp);
  };

  return (
    <div
      className="relative border-t border-b bg-[var(--deposit-card-background)] flex flex-col"
      style={{
        height: `${height}px`,
        overflow: 'hidden',
        minHeight: '300px',
        maxHeight: '800px',
        position: 'relative',
        zIndex: 10,
      }}
    >
      {/* ── Custom Interval Toolbar — isolated memo, never re-renders the chart ── */}
      <ChartToolbar
        currentInterval={currentInterval}
        onInterval={handleIntervalChange}
        onIndicators={handleIndicators}
        onSettings={handleChartSettings}
        onFullscreen={handleFullscreen}
      />

      {/* ── TradingView chart ── */}
      {/* No onMouseDown here — chart interactions must not trigger resize */}
      <div
        id="tv_chart_container"
        className="w-full flex-1 relative overflow-hidden"
        style={{ pointerEvents: 'auto' }}
      />

      <div
        className="absolute bottom-0 left-0 right-0 h-2 cursor-ns-resize bg-transparent"
        onMouseDown={handleMouseDown}
      />

      {/* Branded loading overlay — layered above the chart area, fades out when ready */}
      <ChartLoaderOverlay isChartReady={isChartReady} />
    </div>
  );
}

function ChartLoaderOverlay({ isChartReady }: { isChartReady: boolean }) {
  const [mounted, setMounted] = useState(true);
  const isMobile = useIsMobile();

  useEffect(() => {
    if (!isChartReady) {
      setMounted(true);
      return;
    }
    const t = window.setTimeout(() => setMounted(false), 400);
    return () => window.clearTimeout(t);
  }, [isChartReady]);

  // On mobile we don't render any chart loader. The splash screen's
  // WebGL PaxSpinner is the only branded loader on mobile — stacking a
  // second visual on top of the TV iframe caused the symptom where the
  // chart appeared stuck on market / tab switches (WebGL context
  // pressure and CPU contention with the iframe).
  if (isMobile) return null;

  // Safety: if the chart is ready we never render the overlay. Prevents a
  // stuck overlay if some edge case keeps `mounted` from flipping.
  if (isChartReady && !mounted) return null;
  if (!mounted) return null;

  return (
    <div
      aria-hidden="true"
      className="absolute left-0 right-0 flex items-center justify-center bg-[var(--deposit-card-background)] pointer-events-none"
      style={{
        top: TOOLBAR_H,
        bottom: 0,
        opacity: isChartReady ? 0 : 1,
        transition: 'opacity 350ms ease-out',
        zIndex: isChartReady ? -1 : 20,
      }}
    >
      <PaxSpinner
        active={!isChartReady}
        speed={0.7}
        style={{
          width: 'min(50%, 260px)',
          aspectRatio: '1 / 1',
        }}
      />
    </div>
  );
}

