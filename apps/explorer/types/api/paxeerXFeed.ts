// Price feed submissions are native CometBFT transactions; prices are 18-decimal integers carried
// as decimal strings and must never pass through a float.

export interface PriceObservation {
  market_id: number;
  symbol: string | null;
  index_price: string;
  mark_price: string;
  source_timestamp_ms: number;
  sequence: number;
}

export interface PriceUpdateSummary {
  feeder_bech32: string;
  observation_count: number;
  observations: Array<PriceObservation>;
}

export interface PriceUpdateItem {
  hash: string;
  block_number: number;
  block_hash: string;
  timestamp: string;
  index: number;
  from: { hash: string; is_contract: boolean };
  to: null;
  value: string;
  fee: { type: 'actual'; value: string };
  gas_used: string;
  gas_limit: string;
  status: 'ok' | 'error';
  result: 'success' | 'failed';
  method: 'submitPrices';
  transaction_types: Array<'price_update'>;
  type: 127;
  confirmations: number;
  price_update: PriceUpdateSummary;
}

export interface FeedMarketLatest {
  index_price: string;
  mark_price: string;
  source_timestamp_ms: number;
  sequence: number;
  update_hash: string;
  block_number: number;
  timestamp: string;
}

export interface FeedMarket {
  market_id: number;
  symbol: string;
  chainflow_symbol: string;
  latest: FeedMarketLatest | null;
}

export interface FeedMarketsResponse {
  items: Array<FeedMarket>;
  chain_head: number;
}

export interface MarketUpdate {
  update_hash: string;
  block_number: number;
  timestamp: string;
  index_price: string;
  mark_price: string;
  source_timestamp_ms: number;
  sequence: number;
}

export interface MarketUpdatesResponse {
  items: Array<MarketUpdate>;
  next_page_params: {
    block_number: number;
    index: number;
  } | null;
}
