// File generated from our OpenAPI spec by Scalar. See README.md for details.

// Smoke test: calls every generated operation once to confirm the SDK can reach each endpoint.
// Run it from this repo with `bun tests/smoke-test.ts`. Each case below calls one SDK method
// exactly the way the SDK exposes it (positional params, request body, pagination, streaming).
//
// Two environment variables tune a run:
//   - SCALAR_SMOKE_FILTER: comma-separated needles; only operations whose name or path contains
//     one of them run, so you can smoke-test a subset without editing this file.
//   - SCALAR_SMOKE_REPORT: a file path; when set, the run writes a JSON report there instead of
//     printing a table. The generator uses this to collect per-operation results.
import { writeFileSync } from 'node:fs';

// The package exports the client class. The client reads auth and the base URL from the
// environment, so it needs no constructor options to point at a server.
import ChainflowDataGateway from '@sidioralabs/chainflow-data-gateway';

// One shared client runs every case.
const client = new ChainflowDataGateway({ maxRetries: 2, timeout: 10_000 });

// The result of running one case, collected for the JSON report or the printed table.
type SmokeResult = {
  operation: string;
  method: string;
  path: string;
  label?: string;
  status: 'passed' | 'failed';
  durationMs: number;
  error?: string;
};

// One or two entries per generated operation: the first passes only the arguments the method
// requires, the second also fills every optional parameter and body property. `label` says which
// is which, and is absent when the operation has no optional argument and so has only one case.
// `run` performs the real SDK call; the other fields are metadata used for filtering and
// reporting. This list is generated, so it stays in sync with the SDK surface.
const cases: {
  operation: string;
  method: string;
  path: string;
  label?: string;
  run: () => Promise<unknown>;
}[] = [
  {
    operation: 'listHealth',
    method: 'GET',
    path: '/health',
    run: async () => {
      const response = await client.gateway.listHealth();
    },
  },

  {
    operation: 'createWebSocketToken',
    method: 'POST',
    path: '/auth/ws-token',
    run: async () => {
      const wsToken = await client.gateway.createWebSocketToken({ symbol: 'btc' });
    },
  },

  {
    operation: 'list',
    method: 'GET',
    path: '/api/{market}/price',
    run: async () => {
      const priceFeed = await client.priceFeeds.list('btc');
    },
  },

  {
    operation: 'listHealth',
    method: 'GET',
    path: '/api/{market}/health',
    run: async () => {
      const feedHealth = await client.priceFeeds.listHealth('btc');
    },
  },

  {
    operation: 'listSpotStyleStats',
    method: 'GET',
    path: '/api/{market}/stats',
    run: async () => {
      const stats = await client.priceFeeds.listSpotStyleStats('btc');
    },
  },

  {
    operation: 'listPerpetualStats',
    method: 'GET',
    path: '/api/{market}/perp_stats',
    run: async () => {
      const perpStats = await client.priceFeeds.listPerpetualStats('btc');
    },
  },

  {
    operation: 'listTradeTapeStatus',
    method: 'GET',
    path: '/api/{market}/trades',
    run: async () => {
      const tradeTapeStatus = await client.priceFeeds.listTradeTapeStatus('btc');
    },
  },

  {
    operation: 'retrieveRecentSpotTrades',
    method: 'GET',
    path: '/api/{market}/trades/{symbol}',
    run: async () => {
      const spotTrades = await client.priceFeeds.retrieveRecentSpotTrades('symbol', {
        market: 'btc',
        limit: 100,
      });
    },
  },

  {
    operation: 'retrieveRecentPerpetualTrades',
    method: 'GET',
    path: '/api/{market}/perp_trades/{symbol}',
    run: async () => {
      const perpTrades = await client.priceFeeds.retrieveRecentPerpetualTrades('symbol', {
        market: 'btc',
        limit: 100,
      });
    },
  },

  {
    operation: 'listDatafeedConfig',
    method: 'GET',
    path: '/api/{market}/config',
    run: async () => {
      const udfConfig = await client.tradingView.listDatafeedConfig('btc');
    },
  },

  {
    operation: 'listFeedTime',
    method: 'GET',
    path: '/api/{market}/time',
    run: async () => {
      const response = await client.tradingView.listFeedTime('btc');
    },
  },

  {
    operation: 'resolveSymbol',
    method: 'GET',
    path: '/api/{market}/symbols',
    run: async () => {
      const tradingView = await client.tradingView.resolveSymbol('btc', {
        symbol: 'BTC',
      });
    },
  },

  {
    operation: 'search',
    method: 'GET',
    path: '/api/{market}/search',
    run: async () => {
      const tradingView = await client.tradingView.search('btc', {
        query: '',
        limit: 30,
      });
    },
  },

  {
    operation: 'listCandleHistory',
    method: 'GET',
    path: '/api/{market}/history',
    run: async () => {
      const tradingView = await client.tradingView.listCandleHistory('btc', {
        symbol: 'BTC',
        resolution: '1',
        from: 1,
        to: 1,
        countback: 1000,
      });
    },
  },

  {
    operation: 'listQuotes',
    method: 'GET',
    path: '/api/{market}/quotes',
    label: 'required params',
    run: async () => {
      const tradingView = await client.tradingView.listQuotes('btc');
    },
  },

  {
    operation: 'listQuotes',
    method: 'GET',
    path: '/api/{market}/quotes',
    label: 'all params',
    run: async () => {
      const tradingView = await client.tradingView.listQuotes('btc', {
        symbols: ['BTC'],
      });
    },
  },

  {
    operation: 'listHealth',
    method: 'GET',
    path: '/api/markets/health',
    run: async () => {
      const marketsHealth = await client.markets.listHealth();
    },
  },

  {
    operation: 'list',
    method: 'GET',
    path: '/api/markets/markets',
    run: async () => {
      const market = await client.markets.list();
    },
  },

  {
    operation: 'retrieve',
    method: 'GET',
    path: '/api/markets/markets/{symbol}',
    run: async () => {
      const market = await client.markets.retrieve('BTC');
    },
  },

  {
    operation: 'listTotals',
    method: 'GET',
    path: '/api/markets/totals',
    run: async () => {
      const market = await client.markets.listTotals();
    },
  },

  {
    operation: 'listTickers',
    method: 'GET',
    path: '/api/markets/tickers',
    run: async () => {
      const market = await client.markets.listTickers();
    },
  },

  {
    operation: 'retrieveTicker',
    method: 'GET',
    path: '/api/markets/ticker/{symbol}',
    run: async () => {
      const market = await client.markets.retrieveTicker('symbol');
    },
  },

  {
    operation: 'listOpenInterest',
    method: 'GET',
    path: '/api/markets/open-interest',
    run: async () => {
      const market = await client.markets.listOpenInterest();
    },
  },

  {
    operation: 'listHealth',
    method: 'GET',
    path: '/api/explorer/health',
    run: async () => {
      const explorerHealth = await client.explorer.listHealth();
    },
  },

  {
    operation: 'listBlocks',
    method: 'GET',
    path: '/api/explorer/blocks',
    run: async () => {
      const explorer = await client.explorer.listBlocks({
        limit: 20,
      });
    },
  },

  {
    operation: 'listTransactions',
    method: 'GET',
    path: '/api/explorer/transactions',
    run: async () => {
      const explorer = await client.explorer.listTransactions({
        limit: 50,
      });
    },
  },

  {
    operation: 'list',
    method: 'GET',
    path: '/api/v3/coins/list',
    run: async () => {
      const coinGecko = await client.coinGecko.list({
        include_platform: false,
      });
    },
  },

  {
    operation: 'listMarkets',
    method: 'GET',
    path: '/api/v3/coins/markets',
    label: 'required params',
    run: async () => {
      const coinGecko = await client.coinGecko.listMarkets({
        vs_currency: 'usd',
        page: 1,
        per_page: 100,
        order: 'market_cap_desc',
        sparkline: false,
        precision: 'full',
        locale: 'en',
      });
    },
  },

  {
    operation: 'listMarkets',
    method: 'GET',
    path: '/api/v3/coins/markets',
    label: 'all params',
    run: async () => {
      const coinGecko = await client.coinGecko.listMarkets({
        vs_currency: 'usd',
        ids: ['bitcoin'],
        symbols: ['btc'],
        page: 1,
        per_page: 100,
        order: 'market_cap_desc',
        sparkline: false,
        price_change_percentage: ['1h'],
        precision: 'full',
        locale: 'en',
      });
    },
  },

  {
    operation: 'retrieve',
    method: 'GET',
    path: '/api/v3/coins/{id}',
    run: async () => {
      const coinDetail = await client.coinGecko.retrieve('bitcoin', {
        localization: false,
        tickers: false,
        community_data: false,
        developer_data: false,
        sparkline: false,
        market_data: true,
      });
    },
  },

  {
    operation: 'listSimplePrices',
    method: 'GET',
    path: '/api/v3/simple/price',
    label: 'required params',
    run: async () => {
      const coinGecko = await client.coinGecko.listSimplePrices({
        vs_currencies: 'usd',
        include_market_cap: false,
        include_24hr_vol: false,
        include_24hr_change: false,
        include_last_updated_at: false,
      });
    },
  },

  {
    operation: 'listSimplePrices',
    method: 'GET',
    path: '/api/v3/simple/price',
    label: 'all params',
    run: async () => {
      const coinGecko = await client.coinGecko.listSimplePrices({
        ids: ['bitcoin'],
        symbols: ['btc'],
        vs_currencies: 'usd',
        include_market_cap: false,
        include_24hr_vol: false,
        include_24hr_change: false,
        include_last_updated_at: false,
      });
    },
  },

  {
    operation: 'listQuotes',
    method: 'GET',
    path: '/v1/cryptocurrency/quotes/latest',
    label: 'required params',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listQuotes({
        convert: 'USD',
      });
    },
  },

  {
    operation: 'listQuotes',
    method: 'GET',
    path: '/v1/cryptocurrency/quotes/latest',
    label: 'all params',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listQuotes({
        convert: 'USD',
        symbol: ['BTC'],
        slug: ['bitcoin'],
      });
    },
  },

  {
    operation: 'listListings',
    method: 'GET',
    path: '/v1/cryptocurrency/listings/latest',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listListings({
        convert: 'USD',
        start: 1,
        limit: 100,
        sort: 'market_cap',
        sort_dir: 'desc',
      });
    },
  },

  {
    operation: 'listQuotesApiAlias',
    method: 'GET',
    path: '/api/v1/cryptocurrency/quotes/latest',
    label: 'required params',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listQuotesAPIAlias({
        convert: 'USD',
      });
    },
  },

  {
    operation: 'listQuotesApiAlias',
    method: 'GET',
    path: '/api/v1/cryptocurrency/quotes/latest',
    label: 'all params',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listQuotesAPIAlias({
        convert: 'USD',
        symbol: ['BTC'],
        slug: ['bitcoin'],
      });
    },
  },

  {
    operation: 'listListingsApiAlias',
    method: 'GET',
    path: '/api/v1/cryptocurrency/listings/latest',
    run: async () => {
      const coinMarketCap = await client.coinMarketCap.listListingsAPIAlias({
        convert: 'USD',
        start: 1,
        limit: 100,
        sort: 'market_cap',
        sort_dir: 'desc',
      });
    },
  },

  {
    operation: 'connectPriceFeed',
    method: 'GET',
    path: '/ws/{market}',
    run: async () => {
      const socket = client.webSockets.connectPriceFeed('btc');
      try {
        // Stop at the first proof a side works: the server accepted the upgrade (`open`) or sent a
        // payload (`message`/`raw`). Leaving the socket open would keep the process alive.
        for await (const message of socket) {
          if (message.type === 'open' || message.type === 'message' || message.type === 'raw') break;
        }
      } finally {
        socket.close();
      }
    },
  },

  {
    operation: 'connectMarkets',
    method: 'GET',
    path: '/ws/markets',
    run: async () => {
      const socket = client.webSockets.connectMarkets();
      try {
        // Stop at the first proof a side works: the server accepted the upgrade (`open`) or sent a
        // payload (`message`/`raw`). Leaving the socket open would keep the process alive.
        for await (const message of socket) {
          if (message.type === 'open' || message.type === 'message' || message.type === 'raw') break;
        }
      } finally {
        socket.close();
      }
    },
  },

  {
    operation: 'connectExplorer',
    method: 'GET',
    path: '/ws/explorer',
    run: async () => {
      const socket = client.webSockets.connectExplorer();
      try {
        // Stop at the first proof a side works: the server accepted the upgrade (`open`) or sent a
        // payload (`message`/`raw`). Leaving the socket open would keep the process alive.
        for await (const message of socket) {
          if (message.type === 'open' || message.type === 'message' || message.type === 'raw') break;
        }
      } finally {
        socket.close();
      }
    },
  },

  {
    operation: 'connectExplorerAlias',
    method: 'GET',
    path: '/explorer',
    run: async () => {
      const socket = client.webSockets.connectExplorerAlias();
      try {
        // Stop at the first proof a side works: the server accepted the upgrade (`open`) or sent a
        // payload (`message`/`raw`). Leaving the socket open would keep the process alive.
        for await (const message of socket) {
          if (message.type === 'open' || message.type === 'message' || message.type === 'raw') break;
        }
      } finally {
        socket.close();
      }
    },
  },
];

/**
 * How many cases run at once, capped at the number of cases there are.
 *
 * SCALAR_SMOKE_CONCURRENCY overrides the default; anything unparseable falls back to it.
 */
const smokeConcurrency = (caseCount: number): number => {
  const override = Number.parseInt(process.env['SCALAR_SMOKE_CONCURRENCY'] ?? '', 10);
  const limit = Number.isInteger(override) && override > 0 ? override : 32;
  return Math.min(limit, caseCount);
};

const main = async (): Promise<void> => {
  // SCALAR_SMOKE_FILTER (comma-separated) keeps only cases whose operation name or path matches
  // one of the needles, so a caller can smoke-test a subset. With no filter, every case runs.
  const filter = process.env['SCALAR_SMOKE_FILTER'];
  const needles = filter
    ? filter
        .split(',')
        .map((needle) => needle.trim())
        .filter(Boolean)
    : [];
  const selected =
    needles.length > 0
      ? cases.filter((testCase) =>
          needles.some((needle) => testCase.operation.includes(needle) || testCase.path.includes(needle)),
        )
      : cases;

  // Run the selected cases under a bounded worker pool rather than all at once. A large SDK has
  // hundreds of operations, and firing every request together exceeds what the client's transport
  // keeps connections for while the runner is already busy with other targets. Each worker pulls
  // the next index off a shared cursor and writes into a pre-sized array, so results stay in case
  // order however the workers interleave. The per-case body catches everything and never rejects,
  // so one failing operation still cannot block the others.
  const results: SmokeResult[] = new Array<SmokeResult>(selected.length);
  let cursor = 0;
  const runNext = async (): Promise<void> => {
    for (let index = cursor++; index < selected.length; index = cursor++) {
      const testCase = selected[index];
      if (!testCase) continue;
      const startedAt = Date.now();
      // `label` distinguishes the required-params run from the all-params run of the same
      // operation; it is omitted entirely when the operation contributed only one case.
      const identity = {
        operation: testCase.operation,
        method: testCase.method,
        path: testCase.path,
        ...(testCase.label ? { label: testCase.label } : {}),
      };
      try {
        await testCase.run();
        results[index] = { ...identity, status: 'passed', durationMs: Date.now() - startedAt };
      } catch (error) {
        // Prefer the stack so a failure points at the failing SDK call; fall back to the message.
        const message = error instanceof Error ? (error.stack ?? error.message) : String(error);
        results[index] = {
          ...identity,
          status: 'failed',
          durationMs: Date.now() - startedAt,
          error: message,
        };
      }
    }
  };
  await Promise.all(Array.from({ length: smokeConcurrency(selected.length) }, runNext));
  const failed = results.filter((result) => result.status === 'failed');

  // With SCALAR_SMOKE_REPORT set, write a machine-readable report; otherwise print a table.
  const reportPath = process.env['SCALAR_SMOKE_REPORT'];
  if (reportPath) {
    writeFileSync(reportPath, JSON.stringify({ total: results.length, failed: failed.length, results }));
  } else {
    for (const result of results) {
      const suffix = result.label ? ` [${result.label}]` : '';
      if (result.status === 'passed')
        console.log(
          `\u2714 ${result.operation}${suffix} (${result.method} ${result.path}) ${result.durationMs}ms`,
        );
      else
        console.error(
          `\u2718 ${result.operation}${suffix} (${result.method} ${result.path})\n${result.error ?? ''}`,
        );
    }
    if (results.length === 0) {
      console.error('No code samples ran (empty SDK or a SCALAR_SMOKE_FILTER that matched nothing).');
    } else {
      console.log(`\n${results.length - failed.length}/${results.length} samples passed`);
    }
  }

  // An empty run (no operations, or a filter that matched nothing) is a failure, not a vacuous pass.
  if (failed.length > 0 || results.length === 0) process.exitCode = 1;
};

void main();
