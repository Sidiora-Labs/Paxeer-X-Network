// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIPromise, type APIResponseProps } from './api-promise';
import * as Errors from './error';
import { uuid4 } from './internal/utils/uuid';
import { validatePositiveInteger, isAbsoluteURL, safeJSON, isEmptyObj } from './internal/utils/values';
import { sleep } from './internal/utils/sleep';
import { castToError, isAbortError } from './internal/errors';
import { getPlatformHeaders } from './internal/detect-platform';
import * as Shims from './internal/shims';
import * as Opts from './internal/request-options';
import { readEnv } from './internal/utils/env';
import {
  formatRequestDetails,
  loggerFor,
  parseLogLevel,
  redactUrl,
  type LogLevel,
  type Logger,
} from './internal/utils/log';
export type { Logger, LogLevel } from './internal/utils/log';
import type { RequestInit, RequestInfo, BodyInit, Fetch } from './internal/builtin-types';
import { buildHeaders, type HeadersLike, type NullableHeaders } from './internal/headers';
import type { FinalRequestOptions, RequestOptions } from './internal/request-options';
import type { HTTPMethod, FinalizedRequestInit, MergedRequestInit, PromiseOrValue } from './internal/types';
import { stringifyQuery } from './internal/utils/query';
import { toFile } from './core/uploads';
import { VERSION } from './version';
import {
  Gateway,
  type WsToken,
  type WsScope,
  type IsoTimestamp,
  type GatewayListHealthResponse,
  type GatewayCreateWebSocketTokenParams,
} from './resources/gateway';
import {
  PriceFeeds,
  type Price,
  type Error as PriceFeedsError,
  type FeedHealth,
  type Stats,
  type PerpStats,
  type TradeTapeStatus,
  type SpotTrades,
  type PerpTrades,
  type Symbol as PriceFeedsSymbol,
  type SpotTrade,
  type PerpTrade,
  type PriceFeedListResponse,
  type PriceFeedRetrieveRecentSpotTradesParams,
  type PriceFeedRetrieveRecentPerpetualTradesParams,
} from './resources/price-feeds';
import {
  TradingView,
  type UdfConfig,
  type SymbolInfo,
  type UdfError,
  type SymbolSearchResult,
  type CandleHistory,
  type HistoryNoData,
  type Quotes,
  type UnixSeconds,
  type Quote,
  type MissingQuote,
  type QuoteValues,
  type TradingViewListFeedTimeResponse,
  type TradingViewResolveSymbolResponse,
  type TradingViewSearchResponse,
  type TradingViewListCandleHistoryResponse,
  type TradingViewListQuotesResponse,
  type TradingViewResolveSymbolParams,
  type TradingViewSearchParams,
  type TradingViewListCandleHistoryParams,
  type TradingViewListQuotesParams,
} from './resources/trading-view';
import {
  Markets,
  type MarketsHealth,
  type UnixMilliseconds,
  type MarketRecord,
  type Totals,
  type Ticker,
  type OpenInterest,
  type WsRuntimeStats,
  type MarketListResponse,
  type MarketRetrieveResponse,
  type MarketListTotalsResponse,
  type MarketListTickersResponse,
  type MarketRetrieveTickerResponse,
  type MarketListOpenInterestResponse,
} from './resources/markets';
import {
  Explorer,
  type ExplorerHealth,
  type Block,
  type Transaction,
  type ExplorerListBlocksResponse,
  type ExplorerListTransactionsResponse,
  type ExplorerListBlocksParams,
  type ExplorerListTransactionsParams,
} from './resources/explorer';
import {
  CoinGecko,
  type CoinListItem,
  type CoinMarket,
  type CoinDetail,
  type CoinSimpleQuote,
  type CoinID,
  type Market,
  type CoinGeckoListResponse,
  type CoinGeckoListMarketsResponse,
  type CoinGeckoListSimplePricesResponse,
  type CoinGeckoListParams,
  type CoinGeckoListMarketsParams,
  type CoinGeckoRetrieveParams,
  type CoinGeckoListSimplePricesParams,
} from './resources/coin-gecko';
import {
  CoinMarketCap,
  type CmcStatus,
  type CmcAsset,
  type CmcUsdQuote,
  type CoinMarketCapListQuotesResponse,
  type CoinMarketCapListListingsResponse,
  type CoinMarketCapListQuotesAPIAliasResponse,
  type CoinMarketCapListListingsAPIAliasResponse,
  type CoinMarketCapListQuotesParams,
  type CoinMarketCapListListingsParams,
  type CoinMarketCapListQuotesAPIAliasParams,
  type CoinMarketCapListListingsAPIAliasParams,
} from './resources/coin-market-cap';
import { WebSockets } from './resources/web-sockets/web-sockets';

export type AuthTokenProvider = () => string | Promise<string>;

export interface ClientOptions {
  /**
   * REST key; not the WS minting key.
   */
  restAPIKey?: string | AuthTokenProvider | undefined;

  /**
   * Separate WS key used ONLY to mint symbol-bound credentials.
   */
  wsMintKey?: string | AuthTokenProvider | null | undefined;

  /**
   * Opaque 300-second, scope-bound token from POST /auth/ws-token. Checked at handshake, not forcibly expired mid-session.
   */
  wsToken?: string | AuthTokenProvider | null | undefined;

  /**
   * Override the default base URL for the API, e.g., "https://api.example.com/v2/"
   *
   * Defaults to process.env["SIDIORALABS_BASE_URL"].
   */
  baseURL?: string | null | undefined;

  /**
   * The maximum amount of time (in milliseconds) that the client should wait for a response
   * from the server before timing out a single request.
   *
   * Note that request timeouts are retried by default, so in a worst-case scenario you may wait
   * much longer than this timeout before the promise succeeds or fails.
   *
   * @unit milliseconds
   */
  timeout?: number | undefined;

  /**
   * Additional `RequestInit` options to be passed to `fetch` calls.
   * Properties will be overridden by per-request `fetchOptions`.
   */
  fetchOptions?: MergedRequestInit | undefined;

  /**
   * Specify a custom `fetch` function implementation.
   *
   * If not provided, we expect that `fetch` is defined globally.
   */
  fetch?: Fetch | undefined;

  /**
   * The maximum number of times that the client will retry a request in case of a
   * temporary failure, like a network error or a 5XX error from the server.
   *
   * @default 2
   */
  maxRetries?: number | undefined;

  /**
   * Default headers to include with every request to the API.
   *
   * These can be removed in individual requests by explicitly setting the
   * header to `null` in request options.
   */
  defaultHeaders?: HeadersLike | undefined;

  /**
   * Default query parameters to include with every request to the API.
   *
   * These can be removed in individual requests by explicitly setting the
   * param to `undefined` in request options.
   */
  defaultQuery?: Record<string, string | undefined> | undefined;

  /**
   * Set the log level.
   *
   * Defaults to process.env["SIDIORALABS_LOG"] or 'warn' if it isn't set.
   */
  logLevel?: LogLevel | undefined;

  /**
   * Set the logger.
   *
   * Defaults to globalThis.console.
   */
  logger?: Logger | undefined;
}

export type ChainflowDataGatewayOptions = ClientOptions;

/**
 * API Client for interfacing with the ChainflowDataGateway API.
 */
export class ChainflowDataGateway {
  restAPIKey: string | AuthTokenProvider;
  wsMintKey: string | AuthTokenProvider | null;
  wsToken: string | AuthTokenProvider | null;

  baseURL: string;
  maxRetries: number;
  timeout: number;
  logger: Logger;
  logLevel: LogLevel | undefined;
  fetchOptions: MergedRequestInit | undefined;
  private fetch: Fetch;
  #encoder: Opts.RequestEncoder;
  protected idempotencyHeader?: string;
  private _baseURLOverridden: boolean;
  private _defaultBaseURL: string;
  private _options: ClientOptions;

  /**
   * API Client for interfacing with the ChainflowDataGateway API.
   *
   * @param {string | AuthTokenProvider | undefined} [opts.restAPIKey=process.env["REST_API_KEY"] ?? undefined]
   * @param {string | AuthTokenProvider | null | undefined} [opts.wsMintKey=process.env["WS_MINT_KEY"] ?? null]
   * @param {string | AuthTokenProvider | null | undefined} [opts.wsToken=process.env["WS_TOKEN"] ?? null]
   * @param {string} [opts.baseURL=process.env["SIDIORALABS_BASE_URL"] ?? https://data.feed.chainflowtrading.com] - Override the default base URL for the API.
   * @param {number} [opts.timeout=1 minute] - The maximum amount of time (in milliseconds) the client will wait for a response before timing out.
   * @param {MergedRequestInit} [opts.fetchOptions] - Additional `RequestInit` options to be passed to `fetch` calls.
   * @param {Fetch} [opts.fetch] - Specify a custom `fetch` function implementation.
   * @param {number} [opts.maxRetries=2] - The maximum number of times the client will retry a request.
   * @param {HeadersLike} opts.defaultHeaders - Default headers to include with every request to the API.
   * @param {Record<string, string | undefined>} opts.defaultQuery - Default query parameters to include with every request to the API.
   */
  constructor({
    baseURL = readEnv('SIDIORALABS_BASE_URL'),
    restAPIKey = readEnv('REST_API_KEY'),
    wsMintKey = readEnv('WS_MINT_KEY') ?? null,
    wsToken = readEnv('WS_TOKEN') ?? null,
    ...opts
  }: ClientOptions = {}) {
    if (restAPIKey === undefined) {
      throw new Errors.ChainflowDataGatewayError(
        "The REST_API_KEY environment variable is missing or empty; either provide it, or instantiate the ChainflowDataGateway client with an restAPIKey option, like new ChainflowDataGateway({ restAPIKey: 'My Rest API Key' }).",
      );
    }

    const options: ClientOptions = {
      restAPIKey,
      wsMintKey,
      wsToken,
      ...opts,
      baseURL: baseURL || 'https://data.feed.chainflowtrading.com',
    };
    const baseURLOverridden = baseURL !== null && baseURL !== undefined && baseURL !== '';
    const defaultBaseURL = 'https://data.feed.chainflowtrading.com';
    this.baseURL = options.baseURL || defaultBaseURL;
    this.timeout = options.timeout ?? ChainflowDataGateway.DEFAULT_TIMEOUT /* 1 minute */;
    this.logger = options.logger ?? console;
    const defaultLogLevel = 'warn';
    // Set default logLevel early so that we can log a warning in parseLogLevel.
    this.logLevel = defaultLogLevel;
    this.logLevel =
      parseLogLevel(options.logLevel, 'ClientOptions.logLevel', this) ??
      parseLogLevel(readEnv('SIDIORALABS_LOG'), 'process.env["SIDIORALABS_LOG"]', this) ??
      defaultLogLevel;
    this.fetchOptions = options.fetchOptions;
    this.maxRetries = options.maxRetries ?? 2;
    this.fetch = options.fetch ?? Shims.getDefaultFetch();
    this.#encoder = Opts.FallbackEncoder;

    const customHeadersEnv = readEnv('SIDIORALABS_CUSTOM_HEADERS');
    if (customHeadersEnv) {
      const parsed: Record<string, string> = {};
      for (const line of customHeadersEnv.split('\n')) {
        const colon = line.indexOf(':');
        if (colon >= 0) {
          parsed[line.substring(0, colon).trim()] = line.substring(colon + 1).trim();
        }
      }
      options.defaultHeaders = { ...parsed, ...options.defaultHeaders };
    }

    this._options = { ...options, baseURL: baseURLOverridden ? this.baseURL : undefined };
    this._baseURLOverridden = baseURLOverridden;
    this._defaultBaseURL = defaultBaseURL;

    this.restAPIKey = restAPIKey;
    this.wsMintKey = wsMintKey;
    this.wsToken = wsToken;
  }

  withOptions(options: Partial<ClientOptions>): this {
    const client = new (this.constructor as new (props: ClientOptions) => this)({
      ...this._options,
      ...(this.#baseURLOverridden() ? { baseURL: this.baseURL } : {}),
      maxRetries: this.maxRetries,
      timeout: this.timeout,
      logger: this.logger,
      logLevel: this.logLevel,
      fetch: this.fetch,
      fetchOptions: this.fetchOptions,
      restAPIKey: this.restAPIKey,
      wsMintKey: this.wsMintKey,
      wsToken: this.wsToken,
      ...options,
    });
    return client;
  }

  #baseURLOverridden(): boolean {
    // A named environment selects a default URL; only explicit overrides should bypass per-request defaults.
    return this._baseURLOverridden || this.baseURL !== this._defaultBaseURL;
  }

  protected defaultQuery(): Record<string, string | undefined> | undefined {
    return this._options.defaultQuery;
  }

  protected stringifyQuery(query: object | Record<string, unknown>): string {
    return stringifyQuery(query);
  }

  private getUserAgent(): string {
    return `ChainflowDataGateway/JS ${VERSION}`;
  }

  protected defaultIdempotencyKey(): string {
    return `scalar-node-retry-${uuid4()}`;
  }

  protected makeStatusError(
    status: number,
    error: object | undefined,
    message: string | undefined,
    headers: Headers,
  ): Errors.APIError {
    return Errors.APIError.generate(status, error, message, headers);
  }

  buildURL(
    path: string,
    query: Record<string, unknown> | null | undefined,
    defaultBaseURL?: string | undefined,
  ): string {
    const baseURL = (!this.#baseURLOverridden() && defaultBaseURL) || this.baseURL;
    // Guarantee exactly one "/" between baseURL and path so that bases without a trailing slash
    // and paths without a leading slash do not fuse into a malformed URL (e.g. ".../v1" + "widgets").
    const url = isAbsoluteURL(path)
      ? new URL(path)
      : new URL(
          (baseURL.endsWith('/') ? baseURL : baseURL + '/') + (path.startsWith('/') ? path.slice(1) : path),
        );

    const defaultQuery = this.defaultQuery();
    const pathQuery = Object.fromEntries(url.searchParams);
    if (!isEmptyObj(defaultQuery) || !isEmptyObj(pathQuery)) {
      query = { ...pathQuery, ...defaultQuery, ...query };
    }

    if (typeof query === 'object' && query && !Array.isArray(query)) {
      url.search = this.stringifyQuery(query);
    }

    return url.toString();
  }

  /**
   * Used as a callback for mutating the given `FinalRequestOptions` object.
   */
  protected async prepareOptions(options: FinalRequestOptions): Promise<void> {}

  /**
   * Used as a callback for mutating the given `RequestInit` object.
   *
   * This is useful for cases where you want to add certain headers based off of
   * the request properties, e.g. `method` or `url`.
   */
  protected async prepareRequest(
    request: RequestInit,
    { url, options }: { url: string; options: FinalRequestOptions },
  ): Promise<void> {}

  get<Rsp>(path: string, opts?: PromiseOrValue<RequestOptions>): APIPromise<Rsp> {
    return this.methodRequest('get', path, opts);
  }

  post<Rsp>(path: string, opts?: PromiseOrValue<RequestOptions>): APIPromise<Rsp> {
    return this.methodRequest('post', path, opts);
  }

  patch<Rsp>(path: string, opts?: PromiseOrValue<RequestOptions>): APIPromise<Rsp> {
    return this.methodRequest('patch', path, opts);
  }

  put<Rsp>(path: string, opts?: PromiseOrValue<RequestOptions>): APIPromise<Rsp> {
    return this.methodRequest('put', path, opts);
  }

  delete<Rsp>(path: string, opts?: PromiseOrValue<RequestOptions>): APIPromise<Rsp> {
    return this.methodRequest('delete', path, opts);
  }

  private methodRequest<Rsp>(
    method: HTTPMethod,
    path: string,
    opts?: PromiseOrValue<RequestOptions>,
  ): APIPromise<Rsp> {
    return this.request(
      Promise.resolve(opts).then((opts) => {
        return { method, path, ...opts } as FinalRequestOptions;
      }),
    );
  }

  request<Rsp>(
    options: PromiseOrValue<FinalRequestOptions>,
    remainingRetries: number | null = null,
  ): APIPromise<Rsp> {
    return new APIPromise(this, this.makeRequest(options, remainingRetries, undefined));
  }

  private async makeRequest(
    optionsInput: PromiseOrValue<FinalRequestOptions>,
    retriesRemaining: number | null,
    retryOfRequestLogID: string | undefined,
  ): Promise<APIResponseProps> {
    const options = await optionsInput;
    const maxRetries = options.maxRetries ?? this.maxRetries;
    if (retriesRemaining == null) {
      retriesRemaining = maxRetries;
    }

    await this.prepareOptions(options);

    const { req, url, timeout } = await this.buildRequest(options, {
      retryCount: maxRetries - retriesRemaining,
    });

    await this.prepareRequest(req, { url, options });

    /** Not an API request ID, just for correlating local log entries. */
    const requestLogID = 'log_' + ((Math.random() * (1 << 24)) | 0).toString(16).padStart(6, '0');
    const retryLogStr = retryOfRequestLogID === undefined ? '' : `, retryOf: ${retryOfRequestLogID}`;
    const startTime = Date.now();

    loggerFor(this).debug(
      `[${requestLogID}] sending request`,
      formatRequestDetails({
        retryOfRequestLogID,
        method: options.method,
        url,
        options,
        headers: req.headers,
      }),
    );

    if (options.signal?.aborted) {
      throw new Errors.APIUserAbortError();
    }

    const controller = new AbortController();
    const response = await this.fetchWithTimeout(url, req, timeout, controller).catch(castToError);
    const headersTime = Date.now();

    if (response instanceof globalThis.Error) {
      const retryMessage = `retrying, ${retriesRemaining} attempts remaining`;
      if (options.signal?.aborted) {
        throw new Errors.APIUserAbortError();
      }
      // detect native connection timeout errors
      // deno throws "TypeError: error sending request for url (https://example/): client error (Connect): tcp connect error: Operation timed out (os error 60): Operation timed out (os error 60)"
      // undici throws "TypeError: fetch failed" with cause "ConnectTimeoutError: Connect Timeout Error (attempted address: example:443, timeout: 1ms)"
      // others do not provide enough information to distinguish timeouts from other connection errors
      const isTimeout =
        isAbortError(response) ||
        /timed? ?out/i.test(String(response) + ('cause' in response ? String(response.cause) : ''));
      if (retriesRemaining) {
        loggerFor(this).info(
          `[${requestLogID}] connection ${isTimeout ? 'timed out' : 'failed'} - ${retryMessage}`,
        );
        loggerFor(this).debug(
          `[${requestLogID}] connection ${isTimeout ? 'timed out' : 'failed'} (${retryMessage})`,
          formatRequestDetails({
            retryOfRequestLogID,
            url,
            durationMs: headersTime - startTime,
            message: response.message,
          }),
        );
        return this.retryRequest(options, retriesRemaining, retryOfRequestLogID ?? requestLogID);
      }
      loggerFor(this).info(
        `[${requestLogID}] connection ${isTimeout ? 'timed out' : 'failed'} - error; no more retries left`,
      );
      loggerFor(this).debug(
        `[${requestLogID}] connection ${isTimeout ? 'timed out' : 'failed'} (error; no more retries left)`,
        formatRequestDetails({
          retryOfRequestLogID,
          url,
          durationMs: headersTime - startTime,
          message: response.message,
        }),
      );
      if (isTimeout) {
        throw new Errors.APIConnectionTimeoutError();
      }
      throw new Errors.APIConnectionError({ cause: response });
    }

    const responseInfo = `[${requestLogID}${retryLogStr}] ${req.method} ${redactUrl(url)} ${
      response.ok ? 'succeeded' : 'failed'
    } with status ${response.status} in ${headersTime - startTime}ms`;

    if (!response.ok) {
      const shouldRetry = await this.shouldRetry(response);
      if (retriesRemaining && shouldRetry) {
        const retryMessage = `retrying, ${retriesRemaining} attempts remaining`;

        // We don't need the body of this response.
        await Shims.CancelReadableStream(response.body);
        loggerFor(this).info(`${responseInfo} - ${retryMessage}`);
        loggerFor(this).debug(
          `[${requestLogID}] response error (${retryMessage})`,
          formatRequestDetails({
            retryOfRequestLogID,
            url: response.url,
            status: response.status,
            headers: response.headers,
            durationMs: headersTime - startTime,
          }),
        );
        return this.retryRequest(
          options,
          retriesRemaining,
          retryOfRequestLogID ?? requestLogID,
          response.headers,
        );
      }

      const retryMessage = shouldRetry ? `error; no more retries left` : `error; not retryable`;

      loggerFor(this).info(`${responseInfo} - ${retryMessage}`);

      const errText = await response.text().catch((err: any) => castToError(err).message);
      const errJSON = safeJSON(errText) as any;
      const errMessage = errJSON ? undefined : errText;

      loggerFor(this).debug(
        `[${requestLogID}] response error (${retryMessage})`,
        formatRequestDetails({
          retryOfRequestLogID,
          url: response.url,
          status: response.status,
          headers: response.headers,
          message: errMessage,
          durationMs: Date.now() - startTime,
        }),
      );

      const err = this.makeStatusError(response.status, errJSON, errMessage, response.headers);
      throw err;
    }

    loggerFor(this).info(responseInfo);
    loggerFor(this).debug(
      `[${requestLogID}] response start`,
      formatRequestDetails({
        retryOfRequestLogID,
        url: response.url,
        status: response.status,
        headers: response.headers,
        durationMs: headersTime - startTime,
      }),
    );

    return { response, options, controller, requestLogID, retryOfRequestLogID, startTime };
  }

  async fetchWithTimeout(
    url: RequestInfo,
    init: RequestInit | undefined,
    ms: number,
    controller: AbortController,
  ): Promise<Response> {
    const { signal, method, ...options } = init || {};
    const abort = this._makeAbort(controller);
    if (signal?.aborted) abort();
    else if (signal) signal.addEventListener('abort', abort, { once: true });

    const timeout = setTimeout(abort, ms);

    const isReadableBody =
      ((globalThis as any).ReadableStream && options.body instanceof (globalThis as any).ReadableStream) ||
      (typeof options.body === 'object' && options.body !== null && Symbol.asyncIterator in options.body);

    const fetchOptions: RequestInit = {
      signal: controller.signal as any,
      ...(isReadableBody ? { duplex: 'half' } : {}),
      method: 'GET',
      ...options,
    };
    if (method) {
      // Custom methods like 'patch' need to be uppercased
      // See https://github.com/nodejs/undici/issues/2294
      fetchOptions.method = method.toUpperCase();
    }

    try {
      // use undefined this binding; fetch errors if bound to something else in browsers and edge runtimes / workers
      return await this.fetch.call(undefined, url, fetchOptions);
    } finally {
      clearTimeout(timeout);
    }
  }

  private async shouldRetry(response: Response): Promise<boolean> {
    // Note this is not a standard header.
    const shouldRetryHeader = response.headers.get('x-should-retry');

    // If the server explicitly says whether or not to retry, obey.
    if (shouldRetryHeader === 'true') return true;
    if (shouldRetryHeader === 'false') return false;

    // Retry on request timeouts.
    if (response.status === 408) return true;

    // Retry on lock timeouts.
    if (response.status === 409) return true;

    // Retry on rate limits.
    if (response.status === 429) return true;

    // Retry internal errors.
    if (response.status >= 500) return true;

    return false;
  }

  private async retryRequest(
    options: FinalRequestOptions,
    retriesRemaining: number,
    requestLogID: string,
    responseHeaders?: Headers | undefined,
  ): Promise<APIResponseProps> {
    let timeoutMillis: number | undefined;

    // Note the `retry-after-ms` header may not be standard, but is a good idea and we'd like proactive support for it.
    const retryAfterMillisHeader = responseHeaders?.get('retry-after-ms');
    if (retryAfterMillisHeader) {
      const timeoutMs = parseFloat(retryAfterMillisHeader);
      if (!Number.isNaN(timeoutMs)) {
        timeoutMillis = timeoutMs;
      }
    }

    // About the Retry-After header: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After
    const retryAfterHeader = responseHeaders?.get('retry-after');
    if (retryAfterHeader && !timeoutMillis) {
      const timeoutSeconds = parseFloat(retryAfterHeader);
      if (!Number.isNaN(timeoutSeconds)) {
        timeoutMillis = timeoutSeconds * 1000;
      } else {
        timeoutMillis = Date.parse(retryAfterHeader) - Date.now();
      }
    }

    // If the API asks us to wait a certain amount of time, just do what it says,
    // but cap server-provided delays at 60s so an oversized or malformed Retry-After
    // (e.g. `retry-after-ms: 999999999`, a past HTTP-date, or a value that Date.parse
    // failed on) cannot block retries for an unbounded amount of time. Otherwise fall
    // back to the default exponential-backoff calculation.
    const maxRetryAfterMillis = 60 * 1000;
    if (
      timeoutMillis === undefined ||
      !Number.isFinite(timeoutMillis) ||
      timeoutMillis <= 0 ||
      timeoutMillis > maxRetryAfterMillis
    ) {
      const maxRetries = options.maxRetries ?? this.maxRetries;
      timeoutMillis = this.calculateDefaultRetryTimeoutMillis(retriesRemaining, maxRetries);
    }
    await sleep(timeoutMillis);

    return this.makeRequest(options, retriesRemaining - 1, requestLogID);
  }

  private calculateDefaultRetryTimeoutMillis(retriesRemaining: number, maxRetries: number): number {
    const initialRetryDelay = 0.5;
    const maxRetryDelay = 8.0;

    const numRetries = maxRetries - retriesRemaining;

    // Apply exponential backoff, but not more than the max.
    const sleepSeconds = Math.min(initialRetryDelay * Math.pow(2, numRetries), maxRetryDelay);

    // Apply some jitter, take up to at most 25 percent of the retry time.
    const jitter = 1 - Math.random() * 0.25;

    return sleepSeconds * jitter * 1000;
  }

  async buildRequest(
    inputOptions: FinalRequestOptions,
    { retryCount = 0 }: { retryCount?: number } = {},
  ): Promise<{ req: FinalizedRequestInit; url: string; timeout: number }> {
    const options = { ...inputOptions };
    const { method, path, query, defaultBaseURL } = options;

    const url = this.buildURL(
      path!,
      { ...(await this.authQueryAsync()), ...((query as Record<string, unknown>) ?? {}) },
      defaultBaseURL,
    );
    if ('timeout' in options) validatePositiveInteger('timeout', options.timeout);
    options.timeout = options.timeout ?? this.timeout;
    const { bodyHeaders, body } = this.buildBody({ options });
    // Headers read the caller's own options, not the copy defaulted above: `X-Scalar-Timeout`
    // reports an explicit per-request timeout, and the idempotency key written back here has to
    // land where the retry can see it.
    const reqHeaders = await this.buildHeaders({
      options: inputOptions,
      method,
      bodyHeaders,
      retryCount,
      url,
    });

    const req: FinalizedRequestInit = {
      method,
      headers: reqHeaders,
      ...(options.signal && { signal: options.signal }),
      ...((globalThis as any).ReadableStream &&
        body instanceof (globalThis as any).ReadableStream && { duplex: 'half' }),
      // `buildBody` already collapses no-body into `undefined`; here we only need to drop that
      // sentinel. A truthiness spread would also strip an intentional empty-string body.
      ...(body !== undefined && { body }),
      ...((this.fetchOptions as any) ?? {}),
      ...((options.fetchOptions as any) ?? {}),
    };
    return { req, url, timeout: options.timeout };
  }

  private async buildHeaders({
    options,
    method,
    bodyHeaders,
    retryCount,
    url,
  }: {
    options: FinalRequestOptions;
    method: HTTPMethod;
    bodyHeaders: HeadersLike;
    retryCount: number;
    url: string;
  }): Promise<Headers> {
    let idempotencyHeaders: HeadersLike = {};
    if (this.idempotencyHeader && method !== 'get') {
      if (!options.idempotencyKey) options.idempotencyKey = this.defaultIdempotencyKey();
      idempotencyHeaders[this.idempotencyHeader] = options.idempotencyKey;
    }

    const headers = buildHeaders([
      idempotencyHeaders,
      {
        Accept: 'application/json',
        'User-Agent': this.getUserAgent(),
        'X-Scalar-Retry-Count': String(retryCount),
        ...(options.timeout ? { 'X-Scalar-Timeout': String(Math.trunc(options.timeout / 1000)) } : {}),
        ...getPlatformHeaders(),
      },
      await this.authHeaders(options),
      this._options.defaultHeaders,
      bodyHeaders,
      options.headers,
    ]);
    appendAuthCookies(headers.values, await this.authCookiesAsync());

    this.validateAuth(url, headers.values, options);

    return headers.values;
  }

  private _makeAbort(controller: AbortController) {
    // note: we can't just inline this method inside `fetchWithTimeout()` because then the closure
    //       would capture all request options, and cause a memory leak.
    return () => controller.abort();
  }

  private buildBody({ options: { body, headers: rawHeaders } }: { options: FinalRequestOptions }): {
    bodyHeaders: HeadersLike;
    body: BodyInit | undefined;
  } {
    // Skip only `null`/`undefined` so an intentional empty-string (or 0/false) payload still
    // reaches the encoder. A plain `!body` check would silently drop those falsy-but-valid bodies,
    // and `null` must be excluded here too because the iterator check below uses `in`, which
    // throws on null.
    if (body == null) {
      return { bodyHeaders: undefined, body: undefined };
    }
    // A `content-type` from either header bag says how the body is already encoded; the request's
    // own wins over the client-wide default, as it does on the wire.
    const headers = buildHeaders([this._options.defaultHeaders, rawHeaders]);
    if (
      // Pass raw type verbatim
      ArrayBuffer.isView(body) ||
      body instanceof ArrayBuffer ||
      body instanceof DataView ||
      // A string is only already-encoded when something has said what it is encoded as.
      // Generated call sites state the declared request media type, so a `text/plain` or
      // ndjson payload reaches the wire byte-for-byte. A string with no `content-type` came
      // from a body the document declared as JSON — `{ "type": "string" }` — and encoding it
      // below is what puts the quotes the server parses for around it.
      (typeof body === 'string' && headers.values.has('content-type')) ||
      // `Blob` is superset of `File`
      ((globalThis as any).Blob && body instanceof (globalThis as any).Blob) ||
      // `FormData` -> `multipart/form-data`
      body instanceof FormData ||
      // `URLSearchParams` -> `application/x-www-form-urlencoded`
      body instanceof URLSearchParams ||
      // Send chunked stream (each chunk has own `length`)
      ((globalThis as any).ReadableStream && body instanceof (globalThis as any).ReadableStream)
    ) {
      return { bodyHeaders: undefined, body: body as BodyInit };
    } else if (
      typeof body === 'object' &&
      (Symbol.asyncIterator in body ||
        (Symbol.iterator in body && 'next' in body && typeof body.next === 'function'))
    ) {
      return { bodyHeaders: undefined, body: Shims.ReadableStreamFrom(body as AsyncIterable<Uint8Array>) };
    } else if (
      typeof body === 'object' &&
      headers.values.get('content-type') === 'application/x-www-form-urlencoded'
    ) {
      return {
        bodyHeaders: { 'content-type': 'application/x-www-form-urlencoded' },
        body: this.stringifyQuery(body),
      };
    } else {
      return this.#encoder({ body, headers });
    }
  }

  protected validateAuth(url: string, headers: Headers, options: FinalRequestOptions): void {
    if (headers.has('X-API-Key')) return;
    if (headerExplicitlyOmitted(options.headers, 'X-API-Key')) return;
    if (new URL(url).searchParams.has('token')) return;
    throw new Errors.AuthenticationError(
      401,
      undefined,
      'Could not resolve authentication method. Expected either restAPIKey, wsMintKey or wsToken to be set. Or for the "X-API-Key" headers to be explicitly omitted',
      headers,
    );
  }

  authHeadersSync(): Record<string, string> {
    const headers: Record<string, string> = {};
    const restAPIKey = this.resolveAuthOptionSync('restAPIKey', this.restAPIKey);
    if (restAPIKey) headers['X-API-Key'] = restAPIKey;
    const wsMintKey = this.resolveAuthOptionSync('wsMintKey', this.wsMintKey);
    if (wsMintKey) headers['X-API-Key'] = wsMintKey;
    return headers;
  }

  webSocketAuthHeaders(): Record<string, string> {
    const restAPIKey = this.resolveAuthOptionSync('restAPIKey', this.restAPIKey);
    if (restAPIKey) return { 'X-API-Key': restAPIKey };
    return {};
  }

  webSocketAuthQuery(): Record<string, string> {
    const wsToken = this.resolveAuthOptionSync('wsToken', this.wsToken);
    if (wsToken) return { token: wsToken };
    return {};
  }

  protected async authHeaders(opts: FinalRequestOptions): Promise<NullableHeaders | undefined> {
    return buildHeaders([await this.restAPIKeyAuth(opts), await this.wsMintKeyAuth(opts)]);
  }

  protected async restAPIKeyAuth(opts: FinalRequestOptions): Promise<NullableHeaders | undefined> {
    const restAPIKey = await this.resolveAuthOption('restAPIKey', this.restAPIKey);
    if (restAPIKey == null) {
      return undefined;
    }
    return buildHeaders([{ 'X-API-Key': restAPIKey }]);
  }

  protected async wsMintKeyAuth(opts: FinalRequestOptions): Promise<NullableHeaders | undefined> {
    const wsMintKey = await this.resolveAuthOption('wsMintKey', this.wsMintKey);
    if (wsMintKey == null) {
      return undefined;
    }
    return buildHeaders([{ 'X-API-Key': wsMintKey }]);
  }

  private async authQueryAsync(): Promise<Record<string, string>> {
    const query: Record<string, string> = {};
    const wsToken = await this.resolveAuthOption('wsToken', this.wsToken);
    if (wsToken) query['token'] = wsToken;
    return query;
  }

  private async authCookiesAsync(): Promise<Record<string, string>> {
    const cookies: Record<string, string> = {};
    return cookies;
  }

  private async resolveAuthOption(
    optionName: string,
    value: string | AuthTokenProvider | null | undefined,
  ): Promise<string | undefined> {
    if (value == null) return undefined;
    const token = typeof value === 'function' ? await value() : value;
    if (!token)
      throw new Errors.ChainflowDataGatewayError(
        `Expected '${optionName}' to resolve to a non-empty string.`,
      );
    return token;
  }

  private resolveAuthOptionSync(
    optionName: string,
    value: string | AuthTokenProvider | null | undefined,
  ): string | undefined {
    if (value == null) return undefined;
    const token = typeof value === 'function' ? value() : value;
    if (typeof token !== 'string' || !token)
      throw new Errors.ChainflowDataGatewayError(
        `Expected '${optionName}' to resolve to a non-empty string.`,
      );
    return token;
  }

  static ChainflowDataGateway = this;
  static DEFAULT_TIMEOUT = 60000; // 1 minute

  static ChainflowDataGatewayError = Errors.ChainflowDataGatewayError;
  static APIError = Errors.APIError;
  static APIConnectionError = Errors.APIConnectionError;
  static APIConnectionTimeoutError = Errors.APIConnectionTimeoutError;
  static APIUserAbortError = Errors.APIUserAbortError;
  static NotFoundError = Errors.NotFoundError;
  static ConflictError = Errors.ConflictError;
  static RateLimitError = Errors.RateLimitError;
  static BadRequestError = Errors.BadRequestError;
  static AuthenticationError = Errors.AuthenticationError;
  static InternalServerError = Errors.InternalServerError;
  static PermissionDeniedError = Errors.PermissionDeniedError;
  static UnprocessableEntityError = Errors.UnprocessableEntityError;

  static toFile = toFile;

  gateway: Gateway = new Gateway(this);
  priceFeeds: PriceFeeds = new PriceFeeds(this);
  tradingView: TradingView = new TradingView(this);
  markets: Markets = new Markets(this);
  explorer: Explorer = new Explorer(this);
  coinGecko: CoinGecko = new CoinGecko(this);
  coinMarketCap: CoinMarketCap = new CoinMarketCap(this);
  webSockets: WebSockets = new WebSockets(this);
}

ChainflowDataGateway.Gateway = Gateway;
ChainflowDataGateway.PriceFeeds = PriceFeeds;
ChainflowDataGateway.TradingView = TradingView;
ChainflowDataGateway.Markets = Markets;
ChainflowDataGateway.Explorer = Explorer;
ChainflowDataGateway.CoinGecko = CoinGecko;
ChainflowDataGateway.CoinMarketCap = CoinMarketCap;
ChainflowDataGateway.WebSockets = WebSockets;

export declare namespace ChainflowDataGateway {
  export type RequestOptions = Opts.RequestOptions;
  export {
    Gateway as Gateway,
    type WsToken as WsToken,
    type WsScope as WsScope,
    type IsoTimestamp as IsoTimestamp,
    type GatewayListHealthResponse as GatewayListHealthResponse,
    type GatewayCreateWebSocketTokenParams as GatewayCreateWebSocketTokenParams,
  };

  export {
    PriceFeeds as PriceFeeds,
    type Price as Price,
    type PriceFeedsError as Error,
    type FeedHealth as FeedHealth,
    type Stats as Stats,
    type PerpStats as PerpStats,
    type TradeTapeStatus as TradeTapeStatus,
    type SpotTrades as SpotTrades,
    type PerpTrades as PerpTrades,
    type PriceFeedsSymbol as Symbol,
    type SpotTrade as SpotTrade,
    type PerpTrade as PerpTrade,
    type PriceFeedListResponse as PriceFeedListResponse,
    type PriceFeedRetrieveRecentSpotTradesParams as PriceFeedRetrieveRecentSpotTradesParams,
    type PriceFeedRetrieveRecentPerpetualTradesParams as PriceFeedRetrieveRecentPerpetualTradesParams,
  };

  export {
    TradingView as TradingView,
    type UdfConfig as UdfConfig,
    type SymbolInfo as SymbolInfo,
    type UdfError as UdfError,
    type SymbolSearchResult as SymbolSearchResult,
    type CandleHistory as CandleHistory,
    type HistoryNoData as HistoryNoData,
    type Quotes as Quotes,
    type UnixSeconds as UnixSeconds,
    type Quote as Quote,
    type MissingQuote as MissingQuote,
    type QuoteValues as QuoteValues,
    type TradingViewListFeedTimeResponse as TradingViewListFeedTimeResponse,
    type TradingViewResolveSymbolResponse as TradingViewResolveSymbolResponse,
    type TradingViewSearchResponse as TradingViewSearchResponse,
    type TradingViewListCandleHistoryResponse as TradingViewListCandleHistoryResponse,
    type TradingViewListQuotesResponse as TradingViewListQuotesResponse,
    type TradingViewResolveSymbolParams as TradingViewResolveSymbolParams,
    type TradingViewSearchParams as TradingViewSearchParams,
    type TradingViewListCandleHistoryParams as TradingViewListCandleHistoryParams,
    type TradingViewListQuotesParams as TradingViewListQuotesParams,
  };

  export {
    Markets as Markets,
    type MarketsHealth as MarketsHealth,
    type UnixMilliseconds as UnixMilliseconds,
    type MarketRecord as MarketRecord,
    type Totals as Totals,
    type Ticker as Ticker,
    type OpenInterest as OpenInterest,
    type WsRuntimeStats as WsRuntimeStats,
    type MarketListResponse as MarketListResponse,
    type MarketRetrieveResponse as MarketRetrieveResponse,
    type MarketListTotalsResponse as MarketListTotalsResponse,
    type MarketListTickersResponse as MarketListTickersResponse,
    type MarketRetrieveTickerResponse as MarketRetrieveTickerResponse,
    type MarketListOpenInterestResponse as MarketListOpenInterestResponse,
  };

  export {
    Explorer as Explorer,
    type ExplorerHealth as ExplorerHealth,
    type Block as Block,
    type Transaction as Transaction,
    type ExplorerListBlocksResponse as ExplorerListBlocksResponse,
    type ExplorerListTransactionsResponse as ExplorerListTransactionsResponse,
    type ExplorerListBlocksParams as ExplorerListBlocksParams,
    type ExplorerListTransactionsParams as ExplorerListTransactionsParams,
  };

  export {
    CoinGecko as CoinGecko,
    type CoinListItem as CoinListItem,
    type CoinMarket as CoinMarket,
    type CoinDetail as CoinDetail,
    type CoinSimpleQuote as CoinSimpleQuote,
    type CoinID as CoinID,
    type Market as Market,
    type CoinGeckoListResponse as CoinGeckoListResponse,
    type CoinGeckoListMarketsResponse as CoinGeckoListMarketsResponse,
    type CoinGeckoListSimplePricesResponse as CoinGeckoListSimplePricesResponse,
    type CoinGeckoListParams as CoinGeckoListParams,
    type CoinGeckoListMarketsParams as CoinGeckoListMarketsParams,
    type CoinGeckoRetrieveParams as CoinGeckoRetrieveParams,
    type CoinGeckoListSimplePricesParams as CoinGeckoListSimplePricesParams,
  };

  export {
    CoinMarketCap as CoinMarketCap,
    type CmcStatus as CmcStatus,
    type CmcAsset as CmcAsset,
    type CmcUsdQuote as CmcUsdQuote,
    type CoinMarketCapListQuotesResponse as CoinMarketCapListQuotesResponse,
    type CoinMarketCapListListingsResponse as CoinMarketCapListListingsResponse,
    type CoinMarketCapListQuotesAPIAliasResponse as CoinMarketCapListQuotesAPIAliasResponse,
    type CoinMarketCapListListingsAPIAliasResponse as CoinMarketCapListListingsAPIAliasResponse,
    type CoinMarketCapListQuotesParams as CoinMarketCapListQuotesParams,
    type CoinMarketCapListListingsParams as CoinMarketCapListListingsParams,
    type CoinMarketCapListQuotesAPIAliasParams as CoinMarketCapListQuotesAPIAliasParams,
    type CoinMarketCapListListingsAPIAliasParams as CoinMarketCapListListingsAPIAliasParams,
  };

  export { WebSockets as WebSockets };
}

const headerExplicitlyOmitted = (source: HeadersLike | undefined, name: string): boolean => {
  if (!source || Array.isArray(source) || source instanceof Headers) return false;
  const target = name.toLowerCase();
  return Object.entries(source).some(([key, value]) => key.toLowerCase() === target && value === null);
};

const appendAuthCookies = (headers: Headers, cookies: Record<string, string>): void => {
  for (const [name, value] of Object.entries(cookies)) {
    if (cookieHeaderHas(headers.get('Cookie'), name)) continue;
    const cookie = encodeURIComponent(name) + '=' + encodeURIComponent(value);
    const existing = headers.get('Cookie');
    headers.set('Cookie', existing ? existing + '; ' + cookie : cookie);
  }
};

const cookieHeaderHas = (value: string | null, name: string): boolean => {
  if (!value) return false;
  const target = encodeURIComponent(name) + '=';
  return value.split(';').some((cookie) => cookie.trim().startsWith(target));
};
