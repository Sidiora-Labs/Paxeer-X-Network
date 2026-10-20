import { fetchWithPolicy as fetch } from "@/core/net/fetch";
import { activePoll } from "@/core/net/realtime";
/**
 * UDF-compatible datafeed for TradingView — HyperPax / Sidiora candle stack.
 *
 * Historical bars (`getBars`) read the 18 per-symbol UDF REST endpoints
 * directly through data.feed.chainflowtrading.com, using the browser Origin.
 *
 * Live bars (`subscribeBars`) come over WebSocket whenever the symbol has
 * a dedicated gateway WS (all 18 markets): we multiplex the
 * `<SYMBOL>@kline_<interval>` topic onto the same socket already used for
 * the orderbook + trade tape (`useSidPaxMarketStream.ts`). Symbols
 * without a dedicated WS and resolutions outside
 * the candle server's supported set (e.g. `1M`) fall back to a 2 s REST
 * poll on the same `/history` endpoint.
 */

import type {
  IBasicDataFeed,
  LibrarySymbolInfo,
  ResolutionString,
  Bar,
  PeriodParams,
  HistoryCallback,
  DatafeedErrorCallback,
  OnReadyCallback,
  ResolveCallback,
  SubscribeBarsCallback,
  SearchSymbolsCallback,
} from "../../../public/static/charting_library/charting_library";
import {
  hasDedicatedKlineStream,
  subscribeKline,
  tvResolutionToKlineInterval,
  type SidPaxCandle,
} from "@/hooks/perps/useSidPaxMarketStream";

import { candleFeedUrl, FEED_SYMBOLS } from "@/core/feeds";

// Read the public gateway directly. App-domain /api routing must not be a
// dependency for charts. Browser Origin authorizes these keyless requests.

interface BarSubscription {
  /** Tear down the subscription — either clears the poll interval or
   *  unsubscribes from the WS kline topic. */
  cleanup: () => void;
}

function candleToBar(c: SidPaxCandle): Bar {
  return {
    time: c.time * 1000, // unix s → ms (TV expects ms)
    open: c.open,
    high: c.high,
    low: c.low,
    close: c.close,
    volume: c.volume,
  };
}

export class PerpsDatafeed implements IBasicDataFeed {
  private lastBarCache: Record<string, Bar> = {};
  private subscribers: Record<string, BarSubscription> = {};

  onReady(callback: OnReadyCallback): void {
    // Matches the per-symbol candle servers' `/config.supported_resolutions`.
    // Kept in sync with `INTERVALS` in `Chart.tsx` and
    // `tvResolutionToKlineInterval` in `useSidPaxMarketStream.ts`.
    const DEFAULT_RESOLUTIONS = [
      "1", "2", "3", "5", "15", "30", "60", "240", "480", "D", "W",
    ] as ResolutionString[];

    fetch(candleFeedUrl("config"))
      .then((r) => r.json())
      .then((cfg) => {
        setTimeout(() => callback({
          supported_resolutions: cfg.supported_resolutions ?? DEFAULT_RESOLUTIONS,
          supports_marks: cfg.supports_marks ?? false,
          supports_time: cfg.supports_time ?? true,
          supports_timescale_marks: cfg.supports_timescale_marks ?? false,
        }), 0);
      })
      .catch(() => {
        setTimeout(
          () =>
            callback({
              supported_resolutions: DEFAULT_RESOLUTIONS,
              supports_marks: false,
              supports_time: true,
              supports_timescale_marks: false,
            }),
          0
        );
      });
  }

  searchSymbols(
    userInput: string,
    _exchange: string,
    _symbolType: string,
    onResult: SearchSymbolsCallback
  ): void {
    const matches = FEED_SYMBOLS.filter(symbol => symbol.includes(userInput.toUpperCase())).map(symbol => ({
      symbol, ticker: symbol, full_name: symbol, description: symbol, exchange: "HyperPax", type: "crypto",
    }));
    setTimeout(() => onResult(matches), 0);
  }

  resolveSymbol(
    symbolName: string,
    onResolve: ResolveCallback,
    onError: DatafeedErrorCallback
  ): void {
    let url: string;
    try { url = candleFeedUrl("symbols", symbolName); }
    catch (error) { setTimeout(() => onError(String(error)), 0); return; }
    fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`Symbol not found: ${symbolName}`);
        return r.json();
      })
      .then((data) => {
        const symbolInfo: LibrarySymbolInfo = {
          name: data.name || symbolName,
          ticker: data.ticker || symbolName,
          description: data.description || symbolName,
          type: data.type || "crypto",
          session: data.session || "24x7",
          exchange: data.exchange || "HyperPax",
          listed_exchange: data.listed_exchange || "HyperPax",
          timezone: (data.timezone || "Etc/UTC") as LibrarySymbolInfo["timezone"],
          format: data.format || "price",
          pricescale: data.pricescale || 100,
          minmov: data.minmov || 1,
          has_intraday: data.has_intraday !== false,
          has_daily: data.has_daily !== false,
          has_weekly_and_monthly: data.has_weekly_and_monthly !== false,
          supported_resolutions: data.supported_resolutions || (["1", "5", "15", "30", "60", "240", "D", "W"] as ResolutionString[]),
          data_status: data.data_status || "streaming",
        };
        onResolve(symbolInfo);
      })
      .catch((err) => onError(err.message));
  }

  getBars(
    symbolInfo: LibrarySymbolInfo,
    resolution: ResolutionString,
    periodParams: PeriodParams,
    onResult: HistoryCallback,
    onError: DatafeedErrorCallback
  ): void {
    const { from, to, countBack } = periodParams;
    const qs = new URLSearchParams({
      symbol: symbolInfo.ticker ?? symbolInfo.name,
      resolution: String(resolution),
      from: String(from),
      to: String(to),
    });
    if (countBack) qs.set("countback", String(countBack));

    let url: string;
    try { url = candleFeedUrl("history", symbolInfo.ticker ?? symbolInfo.name, qs); }
    catch (error) { setTimeout(() => onError(String(error)), 0); return; }
    fetch(url)
      .then((r) => r.json())
      .then((data) => {
        if (data.s === "no_data") {
          onResult([], { noData: true, nextTime: data.nextTime });
          return;
        }
        if (data.s !== "ok" && data.s !== undefined) {
          onError(data.s || "Error loading data");
          return;
        }

        const bars: Bar[] = [];
        const len = data.t?.length ?? 0;
        for (let i = 0; i < len; i++) {
          bars.push({
            time: data.t[i] * 1000,
            open: data.o[i],
            high: data.h[i],
            low: data.l[i],
            close: data.c[i],
            volume: data.v?.[i],
          });
        }

        if (bars.length > 0) {
          const lastBar = bars[bars.length - 1];
          if (lastBar) {
            this.lastBarCache[symbolInfo.ticker ?? symbolInfo.name] = lastBar;
          }
        }

        onResult(bars, { noData: bars.length === 0 });
      })
      .catch((err) => onError(err.message));
  }

  subscribeBars(
    symbolInfo: LibrarySymbolInfo,
    resolution: ResolutionString,
    onTick: SubscribeBarsCallback,
    listenerGuid: string,
    onResetCacheNeededCallback: () => void
  ): void {
    const symbolKey = symbolInfo.ticker ?? symbolInfo.name;
    // Strip `PERP_…_USDC` wrapping that some symbol names carry; the
    // market proxy maps lowercased bare tickers (e.g. "btc").
    const bareSymbol = symbolKey.toUpperCase().replace(/^PERP_/, "").replace(/_USDC$/, "");

    // Helper — cache + forward a candle to TradingView, ignoring any
    // out-of-order frames. Shared by both WS and REST paths.
    const pushBar = (bar: Bar) => {
      const cached = this.lastBarCache[symbolKey];
      if (!cached || bar.time >= cached.time) {
        this.lastBarCache[symbolKey] = bar;
        onTick(bar);
      }
    };

    // Prefer the WS kline stream when the symbol has a dedicated market-
    // proxy upstream AND the resolution maps to a supported interval.
    // Fall back to REST polling for stocks (no per-symbol WS) and for
    // exotic resolutions (e.g. "1M") that the candle servers don't
    // produce.
    const klineInterval = tvResolutionToKlineInterval(String(resolution));
    if (klineInterval && hasDedicatedKlineStream(bareSymbol)) {
      const unsubscribe = subscribeKline(
        bareSymbol,
        klineInterval,
        {
          onGap: () => { delete this.lastBarCache[symbolKey]; onResetCacheNeededCallback(); },
          onSnapshot: (candles) => {
            // Only forward the most recent (possibly-in-progress) bar.
            // TradingView already has the historical rows from getBars;
            // the snapshot just warms `lastBarCache` + closes the gap
            // between getBars finishing and the first live frame.
            if (candles.length > 0) {
              pushBar(candleToBar(candles[candles.length - 1]));
            }
          },
          onUpdate: (candle) => {
            pushBar(candleToBar(candle));
          },
        },
        1000,
      );
      this.subscribers[listenerGuid] = { cleanup: unsubscribe };
      return;
    }

    // REST polling fallback — same logic as before, just factored
    // through `pushBar` so the bar-cache semantics are identical.
    const poll = () => {
      const now = Math.floor(Date.now() / 1000);
      const qs = new URLSearchParams({
        symbol: symbolKey,
        resolution: String(resolution),
        from: String(now - 300),
        to: String(now + 1),
        countback: "1",
      });

      return fetch(candleFeedUrl("history", symbolKey, qs))
        .then((r) => r.json())
        .then((data) => {
          if (data.s !== "ok" || !data.t?.length) return;

          const idx = data.t.length - 1;
          pushBar({
            time: data.t[idx] * 1000,
            open: data.o[idx],
            high: data.h[idx],
            low: data.l[idx],
            close: data.c[idx],
            volume: data.v?.[idx],
          });
        })
        .catch(() => {});
    };

    const stopPoll = activePoll(poll, 2000);
    this.subscribers[listenerGuid] = {
      cleanup: stopPoll,
    };
  }

  unsubscribeBars(listenerGuid: string): void {
    const sub = this.subscribers[listenerGuid];
    if (sub) {
      sub.cleanup();
      delete this.subscribers[listenerGuid];
    }
  }
}
