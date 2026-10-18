import { Bar, HistoryMetadata, LibrarySymbolInfo, PeriodParams } from "../type";
import {
  RequestParams,
  UdfErrorResponse,
  UdfOkResponse,
  UdfResponse,
  getErrorMessage,
} from "./helpers";
import { IRequester } from "./iRequester";

// tslint:disable: no-any
interface HistoryPartialDataResponse extends UdfOkResponse {
  t: any;
  c: any;
  o?: never;
  h?: never;
  l?: never;
  v?: never;
}

interface HistoryFullDataResponse extends UdfOkResponse {
  t: any;
  c: any;
  o: any;
  h: any;
  l: any;
  v: any;
}

// tslint:enable: no-any
interface HistoryNoDataResponse extends UdfResponse {
  s: "no_data";
  nextTime?: number;
}

type HistoryResponse =
  | HistoryFullDataResponse
  | HistoryPartialDataResponse
  | HistoryNoDataResponse;

export type PeriodParamsWithOptionalCountback = Omit<
  PeriodParams,
  "countBack"
> & { countBack?: number };

export interface GetBarsResult {
  bars: Bar[];
  meta: HistoryMetadata;
}

export type SymbolCreatedTimeResolver = (
  symbolInfo: LibrarySymbolInfo,
) => number | undefined;

interface BoundedKlineParams {
  params: RequestParams;
  clampedToCreateTime: boolean;
}

type KlineFallbackOutcome =
  | {
      status: "success";
      result: GetBarsResult;
    }
  | {
      status: "boundary" | "failed";
    };

const HISTORY_PATH = "tv/history";
const KLINE_HISTORY_PATH = "v1/tv/kline_history";

export interface LimitedResponseConfiguration {
  /**
   * Set this value to the maximum number of bars which
   * the data backend server can supply in a single response.
   * This doesn't affect or change the library behavior regarding
   * how many bars it will request. It just allows this Datafeed
   * implementation to correctly handle this situation.
   */
  maxResponseLength: number;
  /**
   * If the server can't return all the required bars in a single
   * response then `expectedOrder` specifies whether the server
   * will send the latest (newest) or earliest (older) data first.
   */
  expectedOrder: "latestFirst" | "earliestFirst";
}

export class HistoryProvider {
  private _datafeedUrl: string;
  private readonly _requester: IRequester;
  private readonly _limitedServerResponse?: LimitedResponseConfiguration;
  private readonly _getSymbolCreatedTime?: SymbolCreatedTimeResolver;

  private readonly _klinePreference = new Map<string, boolean>();
  private readonly _createTimeBoundaryCutoff = new Map<string, number>();

  /**
   * Static mapping table for resolution conversion
   * Maps TradingView resolution format to Kline History API resolution format
   * Key: TradingView resolution, Value: Kline History API resolution
   */
  private static readonly _RESOLUTION_MAP = new Map<string, string>([
    ["1", "1m"], // 1 minute
    ["3", "3m"], // 3 minutes
    ["5", "5m"], // 5 minutes
    ["15", "15m"], // 15 minutes
    ["30", "30m"], // 30 minutes
    ["60", "1h"], // 1 hour
    ["240", "4h"], // 4 hours
    ["720", "12h"], // 12 hours
    ["1D", "1d"], // 1 day
    ["1W", "1w"], // 1 week
    ["1M", "1mon"], // 1 month (mapped to 1m)
  ]);

  public constructor(
    datafeedUrl: string,
    requester: IRequester,
    limitedServerResponse?: LimitedResponseConfiguration,
    getSymbolCreatedTime?: SymbolCreatedTimeResolver,
  ) {
    this._datafeedUrl = datafeedUrl;
    this._requester = requester;
    this._limitedServerResponse = limitedServerResponse;
    this._getSymbolCreatedTime = getSymbolCreatedTime;
  }

  /**
   * Build request parameters for history API calls
   * @param symbolInfo - Symbol information
   * @param resolution - Resolution string
   * @param periodParams - Period parameters with optional countback
   * @returns Request parameters object
   */
  private _buildRequestParams(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    periodParams: PeriodParamsWithOptionalCountback,
  ): RequestParams {
    const requestParams: RequestParams = {
      symbol: symbolInfo.ticker || "",
      resolution: resolution,
      from: periodParams.from,
      to: periodParams.to,
    };

    const countBack = Math.min(periodParams.countBack ?? 0, 1000);

    if (periodParams.countBack !== undefined) {
      requestParams.countback = countBack;
    }

    if (symbolInfo.currency_code !== undefined) {
      requestParams.currencyCode = symbolInfo.currency_code;
    }

    if (symbolInfo.unit_id !== undefined) {
      requestParams.unitId = symbolInfo.unit_id;
    }

    return requestParams;
  }

  public getBars(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    periodParams: PeriodParamsWithOptionalCountback,
  ): Promise<GetBarsResult> {
    const requestParams = this._buildRequestParams(
      symbolInfo,
      resolution,
      periodParams,
    );

    const preferenceKey = this._getPreferenceKey(symbolInfo, resolution);
    const prefersKline = this._klinePreference.get(preferenceKey) === true;
    const countBack = Math.min(periodParams.countBack ?? 0, 1000);

    return new Promise(
      async (
        resolve: (result: GetBarsResult) => void,
        reject: (reason: string) => void,
      ) => {
        try {
          let result: GetBarsResult;
          let usedHistoryResult = false;

          if (
            this._shouldSkipRequestAtCreateTimeBoundary(
              symbolInfo,
              resolution,
              requestParams,
            )
          ) {
            resolve(this._createNoDataResult());
            return;
          }

          if (prefersKline) {
            const klineParams = this._buildBoundedKlineParams(
              symbolInfo,
              resolution,
              requestParams,
              countBack,
            );
            result =
              klineParams === null
                ? this._createNoDataResult()
                : await this._requestKlineHistory(klineParams.params);
          } else {
            const initialResponse = await this._requestHistory(requestParams);
            result = this._processHistoryResponse(initialResponse);
            usedHistoryResult = true;

            const reachedCreateTimeBoundary =
              this._markCreateTimeBoundaryIfReached(
                symbolInfo,
                resolution,
                result.bars,
              );

            const needsFallback =
              !reachedCreateTimeBoundary &&
              this._shouldFallbackToKline(initialResponse, countBack);

            if (needsFallback) {
              // Get the earliest time from history result to use as start time for kline request
              const earliestTime = this._getEarliestTime(result.bars);
              if (earliestTime !== null) {
                // Calculate remaining countBack by subtracting already fetched bars
                const remainingCountBack = Math.max(
                  0,
                  countBack - result.bars.length,
                );

                const fallbackOutcome = await this._tryKlineFallbackWithMerge(
                  symbolInfo,
                  resolution,
                  requestParams,
                  remainingCountBack,
                  earliestTime,
                  result,
                );
                if (fallbackOutcome.status === "success") {
                  result = fallbackOutcome.result;
                  usedHistoryResult = false;
                  this._klinePreference.set(preferenceKey, true);
                } else if (fallbackOutcome.status === "failed") {
                  this._klinePreference.set(preferenceKey, false);
                }
              } else {
                // If no bars in history result, fallback to original behavior
                const fallbackOutcome = await this._tryKlineFallback(
                  symbolInfo,
                  resolution,
                  requestParams,
                  countBack,
                );
                if (fallbackOutcome.status === "success") {
                  result = fallbackOutcome.result;
                  usedHistoryResult = false;
                  this._klinePreference.set(preferenceKey, true);
                } else if (fallbackOutcome.status === "failed") {
                  this._klinePreference.set(preferenceKey, false);
                }
              }
            } else {
              this._klinePreference.set(preferenceKey, false);
            }
          }

          this._markCreateTimeBoundaryIfReached(
            symbolInfo,
            resolution,
            result.bars,
          );

          if (usedHistoryResult && this._limitedServerResponse) {
            await this._processTruncatedResponse(result, { ...requestParams });
          }

          resolve(result);
        } catch (e: unknown) {
          const error =
            e instanceof Error ? e : typeof e === "string" ? e : undefined;
          const reasonString = getErrorMessage(error);
          // tslint:disable-next-line:no-console
          console.warn(
            `HistoryProvider: getBars() failed, error=${reasonString}`,
          );
          reject(reasonString || "Error");
        }
      },
    );
  }

  /**
   * Request kline history using KLINE_HISTORY_PATH endpoint
   * Handles rate limiting (429 errors) by waiting and retrying
   * @param requestParams - Request parameters
   * @param retryCount - Current retry attempt (internal use)
   * @returns Processed history response
   */
  private async _requestKlineHistory(
    requestParams: RequestParams,
    retryCount: number = 0,
  ): Promise<GetBarsResult> {
    const maxRetries = 5;
    const baseRetryDelay = 2500; // 2.5 seconds base delay (slightly more than 2s to avoid hitting limit)
    const maxRetryDelay = 10000; // Maximum 10 seconds delay

    // Build URL with query parameters
    const params: RequestParams = {
      ...requestParams,
      resolution: this._mapToKlineHistoryResolution(
        requestParams.resolution as string,
      ),
    };
    const urlPath = this._buildUrlWithParams(KLINE_HISTORY_PATH, params);

    // Make request with fetch to check HTTP status code
    const options: RequestInit = { credentials: "same-origin" };
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const requesterWithHeaders = this._requester as any;
    if (requesterWithHeaders._headers !== undefined) {
      options.headers = requesterWithHeaders._headers;
    }

    let response: Response;
    try {
      response = await fetch(`${this._datafeedUrl}/${urlPath}`, options);
    } catch (error) {
      // Network error, don't retry for rate limiting
      throw error;
    }

    // Check if we hit rate limit (429) - handle this BEFORE any JSON parsing
    if (response.status === 429) {
      if (retryCount >= maxRetries) {
        throw new Error(
          `Rate limit exceeded: Maximum retry attempts (${maxRetries}) reached`,
        );
      }

      // Calculate retry delay with exponential backoff
      const retryDelay = Math.min(
        baseRetryDelay * Math.pow(2, retryCount),
        maxRetryDelay,
      );

      // Check if response has Retry-After header
      const retryAfter = response.headers.get("Retry-After");
      const delay = retryAfter ? parseInt(retryAfter, 10) * 1000 : retryDelay;

      // IMPORTANT: Wait BEFORE retrying to avoid hitting rate limit again
      await new Promise((resolve) => setTimeout(resolve, delay));

      // Retry the request after waiting
      return this._requestKlineHistory(requestParams, retryCount + 1);
    }

    // Check if response is not OK (but not 429, which we already handled)
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({
        message: response.statusText,
      }));
      throw new Error(
        errorData.message || errorData.errmsg || response.statusText,
      );
    }

    // Parse response JSON
    let data: HistoryResponse | UdfErrorResponse;
    try {
      data = await response.json();
    } catch {
      throw new Error("Failed to parse response JSON");
    }

    // Process the response (this will handle errors if response.s === "error")
    return this._processHistoryResponse(data);
  }

  /**
   * Process truncated response by making follow-up requests if needed
   * @param result - Current result with bars
   * @param requestParams - Request parameters
   */
  private async _processTruncatedResponse(
    result: GetBarsResult,
    requestParams: RequestParams,
  ) {
    let lastResultLength = result.bars.length;
    try {
      while (
        this._limitedServerResponse &&
        this._limitedServerResponse.maxResponseLength > 0 &&
        this._limitedServerResponse.maxResponseLength === lastResultLength &&
        requestParams.from < requestParams.to
      ) {
        // adjust request parameters for follow-up request
        if (requestParams.countback) {
          requestParams.countback =
            (requestParams.countback as number) - lastResultLength;
        }
        if (this._limitedServerResponse.expectedOrder === "earliestFirst") {
          requestParams.from = Math.round(
            result.bars[result.bars.length - 1].time / 1000,
          );
        } else {
          requestParams.to = Math.round(result.bars[0].time / 1000);
        }

        const followupResponse =
          await this._requester.sendRequest<HistoryResponse>(
            this._datafeedUrl,
            HISTORY_PATH,
            requestParams,
          );
        const followupResult = this._processHistoryResponse(followupResponse);
        lastResultLength = followupResult.bars.length;
        // merge result with results collected so far
        if (this._limitedServerResponse.expectedOrder === "earliestFirst") {
          if (
            followupResult.bars[0].time ===
            result.bars[result.bars.length - 1].time
          ) {
            // Datafeed shouldn't include a value exactly matching the `to` timestamp but in case it does
            // we will remove the duplicate.
            followupResult.bars.shift();
          }
          result.bars.push(...followupResult.bars);
        } else {
          if (
            followupResult.bars[followupResult.bars.length - 1].time ===
            result.bars[0].time
          ) {
            // Datafeed shouldn't include a value exactly matching the `to` timestamp but in case it does
            // we will remove the duplicate.
            followupResult.bars.pop();
          }
          result.bars.unshift(...followupResult.bars);
        }
      }
    } catch (e: unknown) {
      /**
       * Error occurred during followup request. We won't reject the original promise
       * because the initial response was valid so we will return what we've got so far.
       */
      if (e instanceof Error || typeof e === "string") {
        const reasonString = getErrorMessage(e);
        // tslint:disable-next-line:no-console
        console.warn(
          `HistoryProvider: getBars() warning during followup request, error=${reasonString}`,
        );
      }
    }
  }

  private _processHistoryResponse(
    response: HistoryResponse | UdfErrorResponse,
  ) {
    if (response.s !== "ok" && response.s !== "no_data") {
      throw new Error(response.errmsg);
    }

    const bars: Bar[] = [];
    const meta: HistoryMetadata = {
      noData: false,
    };

    if (response.s === "no_data") {
      meta.noData = true;
      meta.nextTime = response.nextTime;
    } else {
      const volumePresent = response.v !== undefined;
      const ohlPresent = response.o !== undefined;

      for (let i = 0; i < response.t.length; ++i) {
        const barValue: Bar = {
          time: response.t[i] * 1000,
          close: parseFloat(response.c[i]),
          open: parseFloat(response.c[i]),
          high: parseFloat(response.c[i]),
          low: parseFloat(response.c[i]),
        };

        if (ohlPresent) {
          barValue.open = parseFloat(
            (response as HistoryFullDataResponse).o[i],
          );
          barValue.high = parseFloat(
            (response as HistoryFullDataResponse).h[i],
          );
          barValue.low = parseFloat((response as HistoryFullDataResponse).l[i]);
        }

        if (volumePresent) {
          const volume = parseFloat((response as HistoryFullDataResponse).v[i]);
          if (volume > 0) {
            barValue.volume = volume;
          } else {
            barValue.volume = 0.0000000001;
          }
        }

        bars.push(barValue);
      }

      // const firstBarTime = response.t[0] * 1000;

      // meta.nextTime = firstBarTime;
    }

    return {
      bars: bars,
      meta: meta,
    };
  }

  /**
   * Maps TradingView resolution format to Kline History API resolution format
   * @param resolution - TradingView resolution string (e.g., "1", "60", "1D")
   * @returns Kline History API resolution string (e.g., "1m", "1h", "1d")
   */
  private _mapToKlineHistoryResolution(resolution: string): string {
    return HistoryProvider._RESOLUTION_MAP.get(resolution) ?? resolution;
  }

  private _buildUrlWithParams(path: string, params?: RequestParams): string {
    if (!params || Object.keys(params).length === 0) {
      return path;
    }

    const searchParams = new URLSearchParams();
    Object.keys(params).forEach((key) => {
      const value = params[key];
      if (Array.isArray(value)) {
        value.forEach((item) => searchParams.append(key, item));
      } else {
        searchParams.append(key, value.toString());
      }
    });

    const queryString = searchParams.toString();
    return queryString ? `${path}?${queryString}` : path;
  }

  private _getPreferenceKey(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
  ): string {
    return `${symbolInfo.ticker ?? ""}|${resolution}`;
  }

  private _requestHistory(requestParams: RequestParams) {
    return this._requester.sendRequest<HistoryResponse>(
      this._datafeedUrl,
      HISTORY_PATH,
      requestParams,
    );
  }

  private _buildKlineParams(
    requestParams: RequestParams,
    countBack: number,
  ): RequestParams {
    const params: RequestParams = {
      ...requestParams,
    };

    delete params.countback;
    if (countBack > 0) {
      params.limit = countBack;
    } else {
      delete params.limit;
    }

    return params;
  }

  private _buildBoundedKlineParams(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    requestParams: RequestParams,
    countBack: number,
  ): BoundedKlineParams | null {
    return this._applyCreateTimeBoundaryToKlineParams(
      symbolInfo,
      resolution,
      this._buildKlineParams(requestParams, countBack),
    );
  }

  /**
   * Get the earliest time from bars array
   * @param bars - Array of bars
   * @returns Earliest timestamp in seconds, or null if bars array is empty
   */
  private _getEarliestTime(bars: Bar[]): number | null {
    if (bars.length === 0) {
      return null;
    }

    // Bars are typically sorted by time in ascending order (earliest first)
    // Find the minimum time to be safe
    return bars[0].time / 1000;
  }

  /**
   * Try kline fallback with merge logic
   * Requests kline data from the earliest time in history result to the original end time,
   * then merges the kline data with history data
   * @param requestParams - Original request parameters
   * @param countBack - Count back value
   * @param earliestTime - Earliest time from history result (in seconds)
   * @param historyResult - History result to merge with
   * @returns Merged result or null if kline request fails
   */
  private async _tryKlineFallbackWithMerge(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    requestParams: RequestParams,
    countBack: number,
    earliestTime: number,
    historyResult: GetBarsResult,
  ): Promise<KlineFallbackOutcome> {
    try {
      if (
        this._hasReachedCreateTimeBoundary(symbolInfo, resolution, earliestTime)
      ) {
        this._markCreateTimeBoundary(symbolInfo, resolution, earliestTime);
        return { status: "boundary" };
      }

      const klineParams: RequestParams = {
        ...requestParams,
        from: requestParams.from,
        to: earliestTime,
      };

      const boundedKlineParams = this._buildBoundedKlineParams(
        symbolInfo,
        resolution,
        klineParams,
        countBack,
      );
      if (boundedKlineParams === null) {
        return { status: "boundary" };
      }

      const klineResult = await this._requestKlineHistory(
        boundedKlineParams.params,
      );

      if (klineResult.bars.length === 0) {
        return { status: "failed" };
      }

      // Merge history and kline data
      return {
        status: "success",
        result: this._mergeBars(historyResult, klineResult),
      };
    } catch {
      return { status: "failed" };
    }
  }

  /**
   * Merge history bars with kline bars
   * Optimized for the case where klineBars are earlier data than historyBars
   * Uses direct concatenation when no overlap, otherwise uses two-pointer merge
   * Assumes both arrays are sorted by time (ascending, earliest first)
   * Kline data takes precedence when timestamps match
   * @param historyResult - History result (later time range)
   * @param klineResult - Kline result (earlier time range)
   * @returns Merged result with sorted bars
   */
  private _mergeBars(
    historyResult: GetBarsResult,
    klineResult: GetBarsResult,
  ): GetBarsResult {
    const historyBars = historyResult.bars;
    const klineBars = klineResult.bars;

    // Handle empty arrays
    if (historyBars.length === 0) {
      return klineResult;
    }
    if (klineBars.length === 0) {
      return historyResult;
    }

    // Check if arrays have time overlap
    // klineBars should be earlier data, so check if latest kline time < earliest history time
    const latestKlineTime = klineBars[klineBars.length - 1].time;
    const earliestHistoryTime = historyBars[0].time;

    let mergedBars: Bar[];

    if (latestKlineTime < earliestHistoryTime) {
      // No overlap: klineBars are all before historyBars
      // Direct concatenation is more efficient (klineBars first, then historyBars)
      mergedBars = [...klineBars, ...historyBars];
    } else if (latestKlineTime === earliestHistoryTime) {
      // Only one overlapping bar at the boundary: kline data takes precedence
      // Skip the first history bar to avoid duplicate
      mergedBars = [...klineBars, ...historyBars.slice(1)];
    } else {
      // Has overlap: use two-pointer merge to handle duplicates
      mergedBars = [];
      let historyIdx = 0;
      let klineIdx = 0;

      while (historyIdx < historyBars.length && klineIdx < klineBars.length) {
        const historyBar = historyBars[historyIdx];
        const klineBar = klineBars[klineIdx];

        if (historyBar.time < klineBar.time) {
          // History bar is earlier, add it
          mergedBars.push(historyBar);
          historyIdx++;
        } else if (historyBar.time > klineBar.time) {
          // Kline bar is earlier, add it
          mergedBars.push(klineBar);
          klineIdx++;
        } else {
          // Same timestamp: kline data takes precedence
          mergedBars.push(klineBar);
          historyIdx++;
          klineIdx++;
        }
      }

      // Append remaining bars from either array
      while (historyIdx < historyBars.length) {
        mergedBars.push(historyBars[historyIdx++]);
      }
      while (klineIdx < klineBars.length) {
        mergedBars.push(klineBars[klineIdx++]);
      }
    }

    // Use kline metadata if available, otherwise use history metadata
    const meta: HistoryMetadata = {
      noData: klineResult.meta.noData && historyResult.meta.noData,
      nextTime: klineResult.meta.nextTime ?? historyResult.meta.nextTime,
    };

    return {
      bars: mergedBars,
      meta: meta,
    };
  }

  private async _tryKlineFallback(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    requestParams: RequestParams,
    countBack: number,
  ): Promise<KlineFallbackOutcome> {
    try {
      const klineParams = this._buildBoundedKlineParams(
        symbolInfo,
        resolution,
        requestParams,
        countBack,
      );
      if (klineParams === null) {
        return { status: "boundary" };
      }

      const result = await this._requestKlineHistory(klineParams.params);
      if (result.bars.length === 0) {
        return { status: "failed" };
      }

      return {
        status: "success",
        result,
      };
    } catch {
      return { status: "failed" };
    }
  }

  private _shouldFallbackToKline(
    response: HistoryResponse | UdfErrorResponse,
    expectedCount: number,
  ): boolean {
    if (response.s !== "ok") {
      return false;
    }

    const barsCount = response.t.length;
    if (expectedCount > 0 && barsCount < expectedCount) {
      return true;
    }

    if (
      this._limitedServerResponse &&
      this._limitedServerResponse.maxResponseLength > 0 &&
      barsCount >= this._limitedServerResponse.maxResponseLength
    ) {
      return true;
    }

    return false;
  }

  private _applyCreateTimeBoundaryToKlineParams(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    params: RequestParams,
  ): BoundedKlineParams | null {
    const createdTimeSeconds = this._getSymbolCreatedTimeSeconds(symbolInfo);
    if (createdTimeSeconds === null) {
      return {
        params,
        clampedToCreateTime: false,
      };
    }

    const to = this._getNumericParam(params, "to");
    if (to !== null && to <= createdTimeSeconds) {
      this._markCreateTimeBoundary(symbolInfo, resolution, createdTimeSeconds);
      return null;
    }

    const from = this._getNumericParam(params, "from");
    if (from !== null && from < createdTimeSeconds) {
      return {
        params: {
          ...params,
          from: createdTimeSeconds,
        },
        clampedToCreateTime: true,
      };
    }

    return {
      params,
      clampedToCreateTime: false,
    };
  }

  private _shouldSkipRequestAtCreateTimeBoundary(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    params: RequestParams,
  ): boolean {
    const createdTimeSeconds = this._getSymbolCreatedTimeSeconds(symbolInfo);
    if (createdTimeSeconds === null) {
      return false;
    }

    const to = this._getNumericParam(params, "to");
    if (to === null) {
      return false;
    }

    if (to <= createdTimeSeconds) {
      return true;
    }

    const cutoff = this._createTimeBoundaryCutoff.get(
      this._getPreferenceKey(symbolInfo, resolution),
    );
    return cutoff !== undefined && to <= cutoff;
  }

  private _markCreateTimeBoundaryIfReached(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    bars: Bar[],
  ): boolean {
    const earliestTime = this._getEarliestTime(bars);
    if (
      earliestTime !== null &&
      this._hasReachedCreateTimeBoundary(symbolInfo, resolution, earliestTime)
    ) {
      this._markCreateTimeBoundary(symbolInfo, resolution, earliestTime);
      return true;
    }

    return false;
  }

  private _markCreateTimeBoundary(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    cutoffSeconds: number,
  ) {
    const key = this._getPreferenceKey(symbolInfo, resolution);
    const currentCutoff = this._createTimeBoundaryCutoff.get(key);

    if (currentCutoff === undefined || cutoffSeconds > currentCutoff) {
      this._createTimeBoundaryCutoff.set(key, cutoffSeconds);
    }
  }

  private _hasReachedCreateTimeBoundary(
    symbolInfo: LibrarySymbolInfo,
    resolution: string,
    earliestTimeSeconds: number,
  ): boolean {
    const createdTimeSeconds = this._getSymbolCreatedTimeSeconds(symbolInfo);
    if (createdTimeSeconds === null) {
      return false;
    }

    return (
      earliestTimeSeconds <=
      createdTimeSeconds + this._resolutionToSeconds(resolution)
    );
  }

  private _getSymbolCreatedTimeSeconds(
    symbolInfo: LibrarySymbolInfo,
  ): number | null {
    const symbolInfoWithCreateTime = symbolInfo as LibrarySymbolInfo & {
      created_time?: number;
    };
    const createdTime =
      symbolInfoWithCreateTime.created_time ??
      this._getSymbolCreatedTime?.(symbolInfo);

    if (typeof createdTime !== "number" || !Number.isFinite(createdTime)) {
      return null;
    }

    return Math.floor(createdTime / 1000);
  }

  private _getNumericParam(params: RequestParams, key: string): number | null {
    const value = params[key];
    if (Array.isArray(value)) {
      return null;
    }

    const numberValue = Number(value);
    return Number.isFinite(numberValue) ? numberValue : null;
  }

  private _resolutionToSeconds(resolution: string): number {
    const match = resolution.match(/^(\d*)([DWMY])?$/);
    if (!match) {
      return 60;
    }

    const amount = match[1] ? parseInt(match[1], 10) : 1;
    if (!Number.isFinite(amount) || amount <= 0) {
      return 60;
    }

    switch (match[2]) {
      case "D":
        return amount * 86400;
      case "W":
        return amount * 7 * 86400;
      case "M":
        return amount * 30 * 86400;
      case "Y":
        return amount * 365 * 86400;
      default:
        return amount * 60;
    }
  }

  private _createNoDataResult(): GetBarsResult {
    return {
      bars: [],
      meta: {
        noData: true,
      },
    };
  }
}
