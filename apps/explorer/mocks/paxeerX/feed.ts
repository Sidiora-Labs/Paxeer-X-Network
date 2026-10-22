import type { FeedMarketsResponse, MarketUpdate, MarketUpdatesResponse, PriceUpdateItem } from 'types/api/paxeerXFeed';

export const updateHash = '0x7d3c1f0e9b8a76543210fedcba9876543210fedcba9876543210fedcba987654';
export const olderUpdateHash = '0x1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f809';
export const oldestUpdateHash = '0x0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0';
export const feederHash = '0x7c2e4b1a9d3f5e6c8b0a1d2e3f4a5b6c7d8e9f01';
export const feederBech32 = 'pax10shykjsa8a07dj6q5850706tdm7ca8cpe6ws6x';

// 67,012.5 and 67,020.25 at 18 decimals, beyond what a float carries exactly.
export const btcIndexPrice = '67012500000000000000000';
export const btcMarkPrice = '67020250000000000000000';
export const ethIndexPrice = '3456789012345678901234';
export const ethMarkPrice = '3457000000000000000000';

export const priceUpdate: PriceUpdateItem = {
  hash: updateHash,
  block_number: 34800123,
  block_hash: '0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd',
  timestamp: '2026-01-02T03:04:05.000000Z',
  index: 0,
  from: { hash: feederHash, is_contract: false },
  to: null,
  value: '0',
  fee: { type: 'actual', value: '0' },
  gas_used: '84211',
  gas_limit: '200000',
  status: 'ok',
  result: 'success',
  method: 'submitPrices',
  transaction_types: [ 'price_update' ],
  type: 127,
  confirmations: 12,
  price_update: {
    feeder_bech32: feederBech32,
    observation_count: 2,
    observations: [
      { market_id: 1, symbol: 'ETH-USD', index_price: ethIndexPrice, mark_price: ethMarkPrice, source_timestamp_ms: 1767323044500, sequence: 9001 },
      { market_id: 0, symbol: 'BTC-USD', index_price: btcIndexPrice, mark_price: btcMarkPrice, source_timestamp_ms: 1767323044400, sequence: 9002 },
    ],
  },
};

export const failedPriceUpdate: PriceUpdateItem = {
  ...priceUpdate,
  hash: olderUpdateHash,
  status: 'error',
  result: 'failed',
};

export const markets: FeedMarketsResponse = {
  items: [
    {
      market_id: 0,
      symbol: 'BTC-USD',
      chainflow_symbol: 'BTCUSDT',
      latest: {
        index_price: btcIndexPrice,
        mark_price: btcMarkPrice,
        source_timestamp_ms: 1767323044400,
        sequence: 9002,
        update_hash: updateHash,
        block_number: 34800123,
        timestamp: '2026-01-02T03:04:05.000000Z',
      },
    },
    {
      market_id: 17,
      symbol: 'SOL-USD',
      chainflow_symbol: 'SOLUSDT',
      latest: null,
    },
  ],
  chain_head: 34800135,
};

export const marketUpdates: Array<MarketUpdate> = [
  {
    update_hash: updateHash,
    block_number: 34800123,
    timestamp: '2026-01-02T03:04:05.000000Z',
    index_price: '67012500000000000000000',
    mark_price: btcMarkPrice,
    source_timestamp_ms: 1767323044400,
    sequence: 9002,
  },
  {
    update_hash: olderUpdateHash,
    block_number: 34800122,
    timestamp: '2026-01-02T03:04:04.000000Z',
    index_price: '67100000000000000000000',
    mark_price: '67105000000000000000000',
    source_timestamp_ms: 1767323043400,
    sequence: 9001,
  },
  {
    update_hash: oldestUpdateHash,
    block_number: 34800121,
    timestamp: '2026-01-02T03:04:03.000000Z',
    index_price: '67000000000000000000000',
    mark_price: '67001000000000000000000',
    source_timestamp_ms: 1767323042400,
    sequence: 9000,
  },
];

export const marketUpdatesPage: MarketUpdatesResponse = {
  items: marketUpdates,
  next_page_params: null,
};
