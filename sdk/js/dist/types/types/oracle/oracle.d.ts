export interface Params {
    /** The number of blocks per voting window, at the end of the vote period, the oracle votes are assessed and exchange rates are calculated. If the vote period is 1 this is equivalent to having oracle votes assessed and exchange rates calculated in each block. */
    vote_period: number;
    vote_threshold: string;
    reward_band: string;
    whitelist: Denom[];
    slash_fraction: string;
    /** The interval in blocks at which the oracle module will assess validator penalty counters, and penalize validators with too poor performance. */
    slash_window: number;
    /** The minimum percentage of voting windows for which a validator must have `success`es in order to not be penalized at the end of the slash window. */
    min_valid_per_window: string;
    lookback_duration: number;
}
export interface Denom {
    name: string;
}
export interface AggregateExchangeRateVote {
    exchange_rate_tuples: ExchangeRateTuple[];
    voter: string;
}
export interface ExchangeRateTuple {
    denom: string;
    exchange_rate: string;
}
export interface OracleExchangeRate {
    exchange_rate: string;
    last_update: string;
    last_update_timestamp: number;
}
export interface PriceSnapshotItem {
    denom: string;
    oracle_exchange_rate?: OracleExchangeRate;
}
export interface PriceSnapshot {
    snapshot_timestamp: number;
    price_snapshot_items: PriceSnapshotItem[];
}
export interface OracleTwap {
    denom: string;
    twap: string;
    lookback_seconds: number;
}
export interface VotePenaltyCounter {
    miss_count: number;
    abstain_count: number;
    success_count: number;
}
