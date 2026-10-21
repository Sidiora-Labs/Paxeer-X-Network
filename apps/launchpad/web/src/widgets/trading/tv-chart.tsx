'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ChartingLibraryWidgetConstructor, ChartingLibraryWidgetOptions, IBasicDataFeed, IChartingLibraryWidget, ResolutionString, TimeFrameItem } from '../../../public/charting_library/charting_library';
import { CandlestickChart } from 'lucide-react';
import { Button, EmptyState, ErrorState, StateNotice, UnavailableState } from '@/ui/primitives';
import { transientRetryBudgetMs } from '@/core/transient-retry';
import { initialsOf } from '@/ui/primitives/avatar';
import { reportUserFacingError } from '@/core/user-facing-error';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useMarketTrades } from '@/page-modules/token/market-session-context';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { shortAddress } from '@/utils/format';
import { ChartToolbar, type ChartMarkerActor } from '@/widgets/coin/chart-toolbar';
import { ChartSplit } from '@/widgets/coin/chart-split';
import { CHART_DEFAULT_HEIGHT, CHART_MAX_HEIGHT, CHART_MIN_HEIGHT, isChartInterval, useChartSettings, type ChartInterval } from '@/widgets/coin/use-chart-settings';
import { browserStreamTransportV2 } from '@kindle/sdk';
import { useMarketClient } from '@/core/market/client';
import { createReplayUdfDatafeed, registerChartReplayTransport, type CanonicalSupply, type ChartMode, type ChartFreshness, type ChartAccessibleView, type ChartMarkPalette, type ChartMarkPresentation, type ValuationState, resolutionToSeconds, resolutionToWsTimeframe, VALUATION_HISTORY_OPERATION } from './udf-datafeed';

export interface TvChartProps {
  poolAddress: string;
  symbol?: string;
  /** Controlled chart height; when omitted the persisted height from chart settings is used. */
  height?: number;
  onHeightChange?: (height: number) => void;
}

declare global {
  interface Window {
    TradingView?: any;
  }
}

const TV_LIB_URL = '/charting_library/charting_library.standalone.js';
const TV_LIB_PATH = '/charting_library/';
const TV_THEME_URL = '/charting_library/sidiora-theme.css';

const TIME_FRAMES: TimeFrameItem[] = [
  { text: '1d', resolution: '5' as ResolutionString, description: '1 day', title: '1D' },
  { text: '5d', resolution: '60' as ResolutionString, description: '5 days', title: '5D' },
  { text: '1m', resolution: '240' as ResolutionString, description: '1 month', title: '1M' },
  { text: '3m', resolution: '1D' as ResolutionString, description: '3 months', title: '3M' },
  { text: '1y', resolution: '1W' as ResolutionString, description: '1 year', title: '1Y' },
];

/**
 * Wall time the canvas waits for the datafeed's first history answer before the library's
 * spinner is replaced by an unavailable chip. The history read is a plain fetch with no
 * deadline of its own; the longest the app keeps any read in its loading state while a
 * server-advised transient failure (retryable 503 / 429) is being retried is
 * transientRetryBudgetMs (15 s), so a read still silent after that budget is not a recovery
 * in progress. A late answer still clears the chip.
 */
export const CHART_ANSWER_BUDGET_MS = transientRetryBudgetMs;

/** Request the chart's first history read targets, kept as developer detail on the chip. */
const CANDLE_HISTORY_OPERATION = 'GET /v2/pools/{poolAddress}/candles';

type FeedAnswer = 'waiting' | 'answered' | 'stalled';

const initialFreshness: ChartFreshness = { sourceObservedAt: null, lastReceivedAt: null, lastSourceHeight: null, connectionStatus: 'connecting', lagReason: null };
const initialView: ChartAccessibleView = { rows: [], markers: [], annotationState: 'unavailable', annotationReason: 'Confirmed fill annotations have not loaded' };

type Palette = Partial<Record<'canvas' | 'surface' | 'text' | 'textFaint' | 'grid' | 'crosshair' | 'up' | 'down' | 'volumeUp' | 'volumeDown' | 'accent' | 'onAccent', string>>;

/** The library renders inside its own iframe, so the role tokens are read once from the host document and handed over as values. */
function readPalette(): Palette {
  if (typeof document === 'undefined') return {};
  const style = getComputedStyle(document.documentElement);
  const token = (name: string) => { const value = style.getPropertyValue(name).trim(); return value || undefined; };
  return {
    canvas: token('--sl-canvas'), surface: token('--sl-surface-1'), text: token('--sl-text-muted'), textFaint: token('--sl-text-faint'), grid: token('--sl-chart-grid'), crosshair: token('--sl-chart-crosshair'),
    up: token('--sl-candle-up'), down: token('--sl-candle-down'), volumeUp: token('--sl-volume-up'), volumeDown: token('--sl-volume-down'), accent: token('--sl-accent'), onAccent: token('--sl-on-accent'),
  };
}

function widgetOverrides(palette: Palette): ChartingLibraryWidgetOptions['overrides'] {
  const overrides: Record<string, string | boolean> = { 'paneProperties.backgroundType': 'solid', 'paneProperties.legendProperties.showSeriesOHLC': true, 'paneProperties.legendProperties.showBarChange': true, 'paneProperties.legendProperties.showVolume': true };
  if (palette.canvas) overrides['paneProperties.background'] = palette.canvas;
  if (palette.grid) { overrides['paneProperties.vertGridProperties.color'] = palette.grid; overrides['paneProperties.horzGridProperties.color'] = palette.grid; }
  if (palette.crosshair) overrides['paneProperties.crossHairProperties.color'] = palette.crosshair;
  if (palette.textFaint) overrides['scalesProperties.textColor'] = palette.textFaint;
  if (palette.grid) overrides['scalesProperties.lineColor'] = palette.grid;
  if (palette.up) { overrides['mainSeriesProperties.candleStyle.upColor'] = palette.up; overrides['mainSeriesProperties.candleStyle.wickUpColor'] = palette.up; overrides['mainSeriesProperties.candleStyle.borderUpColor'] = palette.up; }
  if (palette.down) { overrides['mainSeriesProperties.candleStyle.downColor'] = palette.down; overrides['mainSeriesProperties.candleStyle.wickDownColor'] = palette.down; overrides['mainSeriesProperties.candleStyle.borderDownColor'] = palette.down; }
  return overrides as ChartingLibraryWidgetOptions['overrides'];
}

function studiesOverrides(palette: Palette): ChartingLibraryWidgetOptions['studies_overrides'] {
  const overrides: Record<string, string | boolean> = { 'volume.show ma': false };
  if (palette.volumeDown) overrides['volume.volume.color.0'] = palette.volumeDown;
  if (palette.volumeUp) overrides['volume.volume.color.1'] = palette.volumeUp;
  return overrides;
}

/** Stable key so a refetched token record with the same supply does not rebuild the widget. */
function verifiedSupplyKey(token: { supplyQuality: string; totalSupplyRaw: string | null; decimalsQuality: string; decimals: number | null } | null): string | null {
  if (!token || token.supplyQuality !== 'verified' || token.totalSupplyRaw === null || token.decimalsQuality !== 'verified' || token.decimals === null) return null;
  return `${token.totalSupplyRaw}:${token.decimals}`;
}

export default function TvChart({ poolAddress, symbol, height: heightProp, onHeightChange }: TvChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const widgetRef = useRef<IChartingLibraryWidget | null>(null);
  const chartReadyRef = useRef(false);
  const datafeedRef = useRef<ReturnType<typeof createReplayUdfDatafeed> | null>(null);
  const retryRef = useRef<(() => void) | null>(null);
  const viewportRef = useRef<{ pool: string; from: number; to: number } | null>(null);
  const markerDigestRef = useRef('');
  const intervalRef = useRef<ChartInterval>('5');
  const accessibleIntervalRef = useRef<ChartInterval | null>(null);
  const presentationRef = useRef<ChartMarkPresentation>({ enabled: true });

  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [retryGeneration, setRetryGeneration] = useState(0);
  const [accessibleGeneration, setAccessibleGeneration] = useState(0);
  const [freshness, setFreshness] = useState<ChartFreshness>(initialFreshness);
  const [view, setView] = useState<ChartAccessibleView>(initialView);
  const [valuation, setValuation] = useState<ValuationState | null>(null);
  const [tableOpen, setTableOpen] = useState(false);
  const [answer, setAnswer] = useState<FeedAnswer>('waiting');

  // Live candles ride the public v2 stream core/api serves: GET /v2/snapshots on
  // the api origin, WS /v2/stream on the stream origin (market topics need no
  // ticket). Without this binding the datafeed only polls /v2/pools/{p}/candles.
  const marketClient = useMarketClient();
  useEffect(() => {
    // The SDK refuses a non-HTTPS page origin for a production client; the
    // chart then stays on its HTTP polling path.
    const transport = (() => { try { return browserStreamTransportV2(marketClient, { applicationOrigin: window.location.origin, snapshotPath: '/v2/snapshots' }, false); } catch { return null; } })();
    if (!transport) return;
    const authorization = new AbortController();
    const release = registerChartReplayTransport({ transport, signal: authorization.signal });
    return () => { release(); authorization.abort(); };
  }, [marketClient]);

  const { settings, update } = useChartSettings();
  const interval = settings.interval;
  intervalRef.current = interval;
  const height = heightProp ?? settings.height;

  const snapshot = usePoolSnapshot(poolAddress);
  const token = snapshot.data?.token ?? null;
  const supplyKey = verifiedSupplyKey(token);
  const verifiedSupply = useMemo<CanonicalSupply | undefined>(() => {
    if (supplyKey === null) return undefined;
    const separator = supplyKey.lastIndexOf(':');
    return { raw: supplyKey.slice(0, separator), decimals: Number(supplyKey.slice(separator + 1)) };
  }, [supplyKey]);
  const mcapDisabledReason = snapshot.isPending
    ? 'Loading...'
    : snapshot.isError
      ? "Market cap chart can't load right now"
      : !token
        ? 'Market cap chart is not available for this coin yet'
        : verifiedSupply
          ? null
          : 'Market cap chart is not available for this coin yet';
  const mode: ChartMode = settings.mode === 'mcap' && verifiedSupply ? 'mcap' : 'price';
  const feedSupply = mode === 'mcap' ? verifiedSupply : undefined;

  const actors = useMemo(() => [...new Set(view.markers.map(marker => marker.actor.toLowerCase()))], [view.markers]);
  const { identities } = useWalletIdentities(settings.markers ? actors : []);
  const markerActors = useMemo<ChartMarkerActor[]>(() => actors.map(wallet => {
    const identity = identities[wallet];
    return { wallet, name: identity?.displayName ?? shortAddress(wallet), avatarUrl: identity?.avatarUrl ?? null };
  }), [actors, identities]);

  const setHeight = useCallback((next: number) => { update({ height: next }); onHeightChange?.(next); }, [update, onHeightChange]);
  const setIntervalValue = useCallback((next: ChartInterval) => { update({ interval: next }); }, [update]);

  useEffect(() => {
    if (window.TradingView) { setLoaded(true); return; }
    let active = true;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    const maxAttempts = 3;
    let currentScript: HTMLScriptElement | null = null;
    function tryLoad() {
      if (!active) return;
      attempts++;
      if (currentScript?.parentNode) currentScript.parentNode.removeChild(currentScript);
      const script = document.createElement('script');
      script.src = TV_LIB_URL;
      script.async = true;
      script.onload = () => { if (active) { if (window.TradingView) setLoaded(true); else setError("Chart can't load right now"); } };
      script.onerror = () => {
        if (!active) return;
        if (attempts < maxAttempts) retryTimer = setTimeout(tryLoad, 1000 * attempts);
        else setError("Chart can't load right now");
      };
      currentScript = script;
      document.head.appendChild(script);
    }
    tryLoad();
    return () => {
      active = false;
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      if (currentScript) { currentScript.onload = null; currentScript.onerror = null; }
      if (currentScript?.parentNode) currentScript.parentNode.removeChild(currentScript);
    };
  }, [retryGeneration]);

  useEffect(() => {
    if (!containerRef.current) return;
    if (widgetRef.current) { try { widgetRef.current.remove(); } catch {} widgetRef.current = null; }
    chartReadyRef.current = false;

    let active = true;
    const startInterval = intervalRef.current;
    setView(initialView);
    setValuation(null);
    setAnswer('waiting');
    // Bounded wait for the first history answer (success or a reported failure); see CHART_ANSWER_BUDGET_MS.
    const stallTimer = setTimeout(() => { if (active) setAnswer(value => value === 'waiting' ? 'stalled' : value); }, CHART_ANSWER_BUDGET_MS);
    const datafeed = createReplayUdfDatafeed(
      poolAddress,
      mode,
      value => { if (active) setFreshness(value); },
      feedSupply,
      undefined,
      value => {
        if (!active) return;
        setView(value);
        const digest = JSON.stringify(value.markers);
        if (digest !== markerDigestRef.current) { markerDigestRef.current = digest; if (chartReadyRef.current) widgetRef.current?.activeChart().refreshMarks(); }
      },
      value => { if (active) setValuation(value); },
    );
    datafeedRef.current = datafeed;
    datafeed.setMarkPresentation(presentationRef.current);
    accessibleIntervalRef.current = null;
    const now = Math.floor(Date.now() / 1000);
    const range = viewportRef.current?.pool === poolAddress ? viewportRef.current : { from: Math.max(0, now - resolutionToSeconds(startInterval) * 50), to: now + 60 };
    datafeed.getBars({}, startInterval, { from: Math.floor(range.from), to: Math.ceil(range.to) }, () => { if (active) setAnswer('answered'); }, reason => { if (active) { setAnswer('answered'); setFreshness(value => ({ ...value, connectionStatus: 'degraded', lagReason: reason })); } });
    retryRef.current = datafeed.retry;
    const constructor: { widget: ChartingLibraryWidgetConstructor } | undefined = window.TradingView;
    if (!loaded || !constructor) {
      datafeed.subscribeBars({}, startInterval, () => {}, 'accessible-chart');
      accessibleIntervalRef.current = startInterval;
      return () => { active = false; clearTimeout(stallTimer); retryRef.current = null; datafeedRef.current = null; datafeed.dispose(); };
    }
    setError(null);

    try {
      const palette = readPalette();
      const feed: IBasicDataFeed = datafeed;
      const widget = new constructor.widget({
        container: containerRef.current,
        datafeed: feed,
        symbol: symbol || poolAddress,
        interval: startInterval as ResolutionString,
        library_path: TV_LIB_PATH,
        custom_css_url: TV_THEME_URL,
        locale: 'en',
        fullscreen: false,
        autosize: true,
        theme: 'dark',
        time_frames: TIME_FRAMES,
        toolbar_bg: palette.canvas,
        loading_screen: { backgroundColor: palette.canvas, foregroundColor: palette.accent },
        overrides: widgetOverrides(palette),
        studies_overrides: studiesOverrides(palette),
        disabled_features: [
          'header_widget',
          'header_symbol_search',
          'symbol_search_hot_key',
          'header_compare',
          'header_indicators',
          'header_screenshot',
          'header_undo_redo',
          'volume_force_overlay',
          'display_market_status',
          'use_localstorage_for_settings',
        ],
        enabled_features: [
          'move_logo_to_main_pane',
          'hide_left_toolbar_by_default',
          'timeframes_toolbar',
          'go_to_date',
          'timezone_menu',
          'two_character_bar_marks_labels',
          'create_volume_indicator_by_default',
        ],
      });
      widgetRef.current = widget;
      widget.onChartReady(() => {
        if (!active || widgetRef.current !== widget) return;
        chartReadyRef.current = true;
        const chart = widget.activeChart();
        const viewport = viewportRef.current;
        if (viewport?.pool === poolAddress) void chart.setVisibleRange({ from: viewport.from, to: viewport.to }).catch((reason: unknown) => { if (active) setFreshness(value => ({ ...value, lagReason: reportUserFacingError(reason, 'Chart zoom was reset', 'chart viewport') })); });
        chart.onIntervalChanged().subscribe(null, next => {
          if (!active) return;
          const resolution = String(next);
          if (isChartInterval(resolution) && resolution !== intervalRef.current) { intervalRef.current = resolution; update({ interval: resolution }); }
        });
        setAccessibleGeneration(value => value + 1);
      });
    } catch (err: unknown) {
      datafeed.subscribeBars({}, startInterval, () => {}, 'accessible-chart');
      accessibleIntervalRef.current = startInterval;
      setError(reportUserFacingError(err, "Chart can't load right now", 'chart init'));
    }

    return () => {
      active = false; clearTimeout(stallTimer); retryRef.current = null; datafeedRef.current = null;
      if (widgetRef.current && chartReadyRef.current) {
        try { const current = widgetRef.current.activeChart().getVisibleRange(); if (Number.isFinite(current.from) && Number.isFinite(current.to) && current.to > current.from) viewportRef.current = { pool: poolAddress, from: current.from, to: current.to }; } catch {}
      }
      chartReadyRef.current = false;
      datafeed.dispose();
      if (widgetRef.current) { try { widgetRef.current.remove(); } catch {} widgetRef.current = null; }
    };
  }, [loaded, poolAddress, symbol, mode, feedSupply, retryGeneration, update]);

  useEffect(() => {
    const widget = widgetRef.current;
    if (widget && chartReadyRef.current) {
      const chart = widget.activeChart();
      if (String(chart.resolution()) !== interval) void chart.setResolution(interval as ResolutionString);
      return;
    }
    const datafeed = datafeedRef.current;
    if (!datafeed || accessibleIntervalRef.current === null || accessibleIntervalRef.current === interval) return;
    datafeed.subscribeBars({}, interval, () => {}, 'accessible-chart');
    accessibleIntervalRef.current = interval;
  }, [interval, loaded, accessibleGeneration]);

  useEffect(() => {
    const palette = readPalette();
    const markPalette: ChartMarkPalette | undefined = palette.up && palette.down && palette.onAccent ? { buy: palette.up, sell: palette.down, label: palette.onAccent } : undefined;
    const byWallet = new Map(markerActors.map(actor => [actor.wallet, actor]));
    const presentation: ChartMarkPresentation = {
      enabled: settings.markers,
      palette: markPalette,
      decorate: marker => {
        const actor = byWallet.get(marker.actor.toLowerCase());
        const name = actor?.name ?? shortAddress(marker.actor);
        return { name, label: initialsOf(name), imageUrl: actor?.avatarUrl ?? null };
      },
    };
    presentationRef.current = presentation;
    const datafeed = datafeedRef.current;
    if (!datafeed) return;
    datafeed.setMarkPresentation(presentation);
    const widget = widgetRef.current;
    if (!widget || !chartReadyRef.current) return;
    if (settings.markers) widget.activeChart().refreshMarks();
    else widget.activeChart().clearMarks();
  }, [settings.markers, markerActors, accessibleGeneration]);

  const timeframe = resolutionToWsTimeframe(interval);
  // A pool with no recorded fill has no candles: its newest trade page is empty with no older cursor
  // and no candle has arrived. That is an empty chart, not a failure.
  const firstTrades = useMarketTrades(poolAddress);
  const noTrades = firstTrades.data !== undefined && firstTrades.data.data.items.length === 0 && firstTrades.data.nextCursor === null && view.rows.length === 0;
  const valuationBlocked = mode === 'mcap' && valuation !== null && valuation.state !== 'ready';
  const stalled = answer === 'stalled' && !error && !noTrades && !valuationBlocked;
  const retryChart = useCallback(() => { setError(null); setLoaded(false); setRetryGeneration(value => value + 1); }, []);

  return (
    <section aria-label="Price chart" className="flex w-full max-w-full flex-col overflow-hidden rounded-lg bg-surface-1">
      <ChartToolbar
        interval={interval}
        onIntervalChange={setIntervalValue}
        mode={mode}
        onModeChange={next => update({ mode: next })}
        mcapDisabledReason={mcapDisabledReason}
        markers={settings.markers}
        onMarkersChange={next => update({ markers: next })}
        markerActors={markerActors}
        freshness={freshness}
        onRetry={() => retryRef.current?.()}
      />
      <ChartSplit height={height} onHeightChange={setHeight} min={CHART_MIN_HEIGHT} max={CHART_MAX_HEIGHT} defaultHeight={CHART_DEFAULT_HEIGHT}>
        <div ref={containerRef} className="absolute inset-0 h-full w-full" />
        {!loaded && !error && !stalled ? (
          <div role="status" className="absolute inset-0 flex items-center justify-center bg-canvas text-sm text-text-faint">Loading chart...</div>
        ) : null}
        {noTrades ? (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-canvas">
            <EmptyState icon={CandlestickChart} title="No trades yet" body="The chart starts with the first trade." />
          </div>
        ) : null}
        {stalled ? (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-surface-1 p-4">
            <UnavailableState variant="chip" surface={1} wrap title="Chart data" operation={CANDLE_HISTORY_OPERATION} status={null} onRetry={retryChart} />
          </div>
        ) : null}
        {error && !noTrades ? (
          <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-surface-1 p-4 text-center">
            <ErrorState variant="chip" wrap message="Chart can't load right now" onRetry={retryChart} />
            <p className="text-xs text-text-faint">Prices are still listed below.</p>
          </div>
        ) : null}
        {valuationBlocked && !noTrades ? (
          <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-surface-1 p-4">
            {valuation.state === 'unsupported' ? (
              <UnavailableState variant="chip" surface={1} wrap title="Market cap history" operation={valuation.operation} status={valuation.status} message={valuation.message} onRetry={() => retryRef.current?.()} />
            ) : (
              // A genuine failure (not an unserved route): an error chip, with the operation kept as developer detail.
              <StateNotice kind="error" wrap detail={VALUATION_HISTORY_OPERATION} onRetry={() => retryRef.current?.()}>Market cap chart can&apos;t load right now</StateNotice>
            )}
            <Button variant="secondary" size="sm" onClick={() => update({ mode: 'price' })}>Show price instead</Button>
          </div>
        ) : null}
      </ChartSplit>
      <div className="flex flex-col gap-2 bg-surface-1 px-3 py-2">
        <Button variant="ghost" size="sm" aria-expanded={tableOpen || !!error} aria-controls="canonical-chart-table" onClick={() => setTableOpen(value => !value)} className="self-start">
          {tableOpen || !!error ? 'Hide price list' : 'Show price list'}
        </Button>
        {tableOpen || !!error ? (
          <div id="canonical-chart-table" className="flex flex-col gap-3 rounded-md bg-surface-2 p-3 text-xs text-text-muted">
            <p role="status">{view.annotationState === 'complete' ? 'Trade markers are up to date.' : view.annotationState === 'partial' ? 'Some trade markers are still loading.' : 'Trade markers are still loading.'}</p>
            <div tabIndex={0} role="region" aria-label="Prices and volume" className="overflow-x-auto">
              <table className="w-full text-left">
                <caption className="pb-2 text-left text-text-faint">Prices in USDL, every {timeframe}, times in UTC</caption>
                <thead><tr>{['Time', 'Open', 'High', 'Low', 'Close', 'Volume (USDL)', 'Volume (coin, raw)', 'Version'].map(label => <th key={label} scope="col" className="p-2 font-bold text-text-faint">{label}</th>)}</tr></thead>
                <tbody>{view.rows.filter(row => row.interval === timeframe).map(row => <tr key={row.start} className="odd:bg-surface-3"><th scope="row" className="p-2 font-semibold"><time dateTime={row.start}>{row.start}</time></th>{[row.open, row.high, row.low, row.close, row.volumeUsdl, row.volumeTokenRaw, row.revision].map((value, index) => <td key={index} data-numeric className="p-2 font-mono">{value}</td>)}</tr>)}</tbody>
              </table>
            </div>
            {view.rows.length === 0 ? <p>No trades in this range yet</p> : null}
            <div tabIndex={0} role="region" aria-label="Trades on the chart" className="overflow-x-auto">
              <table className="w-full text-left">
                <caption className="pb-2 text-left text-text-faint">Trades shown on the chart</caption>
                <thead><tr>{['Date', 'Type', 'Amount (raw)', 'USDL (raw)', 'Price', 'Transaction', 'Account', 'Source'].map(label => <th key={label} scope="col" className="p-2 font-bold text-text-faint">{label}</th>)}</tr></thead>
                <tbody>{view.markers.filter(marker => marker.interval === timeframe).map(marker => <tr key={marker.id} className="odd:bg-surface-3"><td className="p-2">{new Date(marker.time * 1000).toISOString()}</td>{[marker.side, marker.tokenAmountRaw, marker.usdlAmountRaw, marker.executionPrice, marker.transactionHash, marker.actor, marker.actorBasis].map((value, index) => <td key={index} data-numeric className="p-2 font-mono">{value}</td>)}</tr>)}</tbody>
              </table>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
