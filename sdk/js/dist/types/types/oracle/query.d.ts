import type { OracleExchangeRate, OracleTwap, Params, PriceSnapshot, VotePenaltyCounter } from "./oracle";
export interface QueryExchangeRateRequest {
    /** denom defines the denomination to query for. */
    denom: string;
}
export interface QueryExchangeRateResponse {
    /** exchange_rate defines the exchange rate of Pax denominated in various Pax */
    oracle_exchange_rate?: OracleExchangeRate;
}
export type QueryExchangeRatesRequest = {};
export interface DenomOracleExchangeRatePair {
    denom: string;
    oracle_exchange_rate?: OracleExchangeRate;
}
export interface QueryExchangeRatesResponse {
    /** exchange_rates defines a list of the exchange rate for all whitelisted denoms. */
    denom_oracle_exchange_rate_pairs: DenomOracleExchangeRatePair[];
}
export type QueryActivesRequest = {};
export interface QueryActivesResponse {
    /** actives defines a list of the denomination which oracle prices aggreed upon. */
    actives: string[];
}
export type QueryVoteTargetsRequest = {};
export interface QueryVoteTargetsResponse {
    /**
     * vote_targets defines a list of the denomination in which everyone
     * should vote in the current vote period.
     */
    vote_targets: string[];
}
export type QueryPriceSnapshotHistoryRequest = {};
export interface QueryPriceSnapshotHistoryResponse {
    price_snapshots: PriceSnapshot[];
}
export interface QueryTwapsRequest {
    lookback_seconds: number;
}
export interface QueryTwapsResponse {
    oracle_twaps: OracleTwap[];
}
export interface QueryFeederDelegationRequest {
    /** validator defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryFeederDelegationResponse {
    /** feeder_addr defines the feeder delegation of a validator */
    feeder_addr: string;
}
export interface QueryVotePenaltyCounterRequest {
    /** validator defines the validator address to query for. */
    validator_addr: string;
}
export interface QueryVotePenaltyCounterResponse {
    vote_penalty_counter?: VotePenaltyCounter;
}
export type QuerySlashWindowRequest = {};
export interface QuerySlashWindowResponse {
    /**
     * window_progress defines the number of voting periods
     * since the last slashing event would have taken place.
     */
    window_progress: number;
}
export type QueryParamsRequest = {};
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: Params;
}
