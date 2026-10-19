import type { AggregateExchangeRateVote, ExchangeRateTuple, Params, PriceSnapshot, VotePenaltyCounter } from "./oracle";
export interface GenesisState {
    params?: Params;
    feeder_delegations: FeederDelegation[];
    exchange_rates: ExchangeRateTuple[];
    penalty_counters: PenaltyCounter[];
    aggregate_exchange_rate_votes: AggregateExchangeRateVote[];
    price_snapshots: PriceSnapshot[];
}
export interface FeederDelegation {
    feeder_address: string;
    validator_address: string;
}
export interface PenaltyCounter {
    validator_address: string;
    vote_penalty_counter?: VotePenaltyCounter;
}
